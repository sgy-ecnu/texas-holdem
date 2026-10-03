import { expect, test, type Page } from '@playwright/test'
import path from 'node:path'

const ownGames: string[] = []
// 顶层 run_turn.py 显式注入 POKER_AI_TURN_DELAY=800ms；仅 mock 模式生效。
const delayMs = 800

function evidence(scenario: string, facts: Record<string, unknown>) {
  console.log(`TURN_EVIDENCE ${scenario}: ${JSON.stringify(facts)}`)
}

async function http(page: Page, id: string, player = false) {
  return page.evaluate(async ({ id, player }) => {
    const response = await fetch(`/api/v1/games/${id}${player ? '/player' : ''}`, {
      credentials: 'same-origin',
    })
    return { status: response.status, data: await response.json() }
  }, { id, player })
}

const field = (page: Page, label: string) => page.locator('.form-grid .field').filter({ hasText: label })

async function create(page: Page, human = false) {
  await page.goto('/')
  await expect(page.getByRole('button', { name: '创建牌局', exact: true })).toBeVisible()
  if (human) await page.getByLabel('真人座位', { exact: true }).selectOption('0')
  await field(page, '小盲').locator('input').fill('5')
  await field(page, '大盲').locator('input').fill('10')
  await field(page, '最多手数').locator('input').fill('10')
  const pending = page.waitForResponse(response => response.request().method() === 'POST' &&
    new URL(response.url()).pathname === '/api/v1/games')
  await page.getByRole('button', { name: '创建牌局', exact: true }).click()
  const response = await pending
  expect(response.status()).toBe(201)
  const game = await response.json()
  ownGames.push(game.id)
  expect(game.mode).toBe('mock')
  expect(game.live_calls_used).toBe(0)
  await expect(page.locator(human ? '.view-player' : '.view-public')).toBeVisible()
  return game.id as string
}

async function command(page: Page, id: string, label: string, kind: string) {
  const pending = page.waitForResponse(response => response.request().method() === 'POST' &&
    new URL(response.url()).pathname === `/api/v1/games/${id}/commands`)
  const began = performance.now()
  await page.getByRole('button', { name: label, exact: true }).click()
  const response = await pending
  expect(response.status()).toBe(200)
  expect(response.request().postDataJSON()).toEqual({ command: kind })
  return { elapsedMs: Math.round(performance.now() - began), view: await response.json() }
}

async function shot(page: Page, name: string) {
  const target = path.join(process.env.TURN_SCREEN_DIR!, name)
  await page.screenshot({ path: target, fullPage: true })
  return target
}

test.afterEach(async ({ page }) => {
  for (const id of ownGames.splice(0)) {
    const response = await http(page, id)
    expect(response.status).toBe(200)
    if (!['stopped', 'completed', 'failed'].includes(response.data.status)) {
      const status = await page.evaluate(async id => {
        const response = await fetch(`/api/v1/games/${id}/commands`, {
          method: 'POST', credentials: 'same-origin', headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ command: 'stop' }),
        })
        return response.status
      }, id)
      expect(status).toBe(200)
    }
  }
})

test('turn_mock_cadence_feed · 纯 AI 光圈轮转、思考提示、逐条历史与自动滚动', async ({ page }) => {
  const id = await create(page)
  await command(page, id, '开始', 'start')
  await expect(page.locator('.hand-dock[data-turn="ai"] .dock-state')).toHaveText('思考中…')
  await expect(page.locator('.seat.is-actor .turn-badge')).toHaveText('思考中')
  await expect(page.locator('.seat.is-actor')).toHaveCount(1)
  const firstActor = await page.locator('.seat.is-actor .seat-no').innerText()
  const firstScreen = await shot(page, 'ai-first-actor.png')
  await expect.poll(() => page.locator('.seat.is-actor .seat-no').innerText(),
    { timeout: 5_000, intervals: [50, 100] }).not.toBe(firstActor)
  const secondActor = await page.locator('.seat.is-actor .seat-no').innerText()
  await expect(page.locator('.seat.is-actor .turn-badge')).toHaveText('思考中')
  const secondScreen = await shot(page, 'ai-next-actor.png')
  // DOM 用自然的每秒轮询更新；额外原生 fetch 只读时间，不 apply、刷新或推进 UI。
  const observed = await page.evaluate(async id => {
    const started = performance.now()
    const ui: { atMs: number; actor: string; rows: number }[] = []
    const actions: { atMs: number; hand: number; sequence: number; seat: number }[] = []
    let signature = ''
    let actionKey = ''
    let lastProbe = -1000
    let scroll = { scrollTop: 0, scrollHeight: 0, clientHeight: 0, bottomGap: 0 }
    while (performance.now() - started < 28_000) {
      const atMs = Math.round(performance.now() - started)
      const actor = document.querySelector('.seat.is-actor .seat-no')?.textContent?.trim() ?? ''
      const rows = document.querySelectorAll('.action-feed .feed-row').length
      const key = `${actor}/${rows}`
      if (key !== signature) {
        ui.push({ atMs, actor, rows })
        signature = key
      }
      const feed = document.querySelector<HTMLElement>('.action-feed')
      if (feed) scroll = { scrollTop: feed.scrollTop, scrollHeight: feed.scrollHeight,
        clientHeight: feed.clientHeight, bottomGap: feed.scrollHeight - feed.clientHeight - feed.scrollTop }
      if (atMs - lastProbe >= 200) {
        lastProbe = atMs
        const response = await fetch(`/api/v1/games/${id}`, { credentials: 'same-origin' })
        if (response.status !== 200) throw new Error(`PUBLIC_PROBE_HTTP_${response.status}`)
        const view = await response.json()
        const action = view.last_action
        if (action && `${action.hand_no}/${action.sequence}` !== actionKey) {
          actionKey = `${action.hand_no}/${action.sequence}`
          actions.push({ atMs: Math.round(performance.now() - started), hand: action.hand_no,
            sequence: action.sequence, seat: action.seat })
        }
      }
      if (rows >= 11 && actions.length >= 6 && scroll.scrollTop > 0 && scroll.bottomGap < 3) break
      await new Promise(resolve => setTimeout(resolve, 50))
    }
    return { ui, actions, scroll }
  }, id)
  // 每秒轮询偶尔会采到「两手之间无 actor」的过渡帧（actor 为空），只校验实际轮转到的座位。
  const observedActors = observed.ui.map(sample => sample.actor).filter(actor => actor !== '')
  expect(new Set(observedActors)).toEqual(new Set(['0', '1']))
  expect(observed.actions.length).toBeGreaterThanOrEqual(6)
  const gaps = observed.actions.slice(1).map((action, i) => action.atMs - observed.actions[i].atMs)
  // 首个 action 是启动观测时已存在的快照，其后的第一个间隔只是剩余窗口。
  // 800ms 步进 + 200ms 采样：相邻 last_action 间隔应稳定 > 300ms，证明不是瞬间跑完。
  expect(Math.min(...gaps.slice(1))).toBeGreaterThan(300)
  const additions = observed.ui.filter((sample, i) => i === 0 || sample.rows !== observed.ui[i - 1].rows)
  // UI 每秒轮询，800ms 步进下偶尔一次轮询跨越两条动作，故只断言历史单向逐条增长、确有间隔。
  expect(additions.length).toBeGreaterThanOrEqual(3)
  for (let i = 1; i < additions.length; i++) {
    expect(additions[i].rows).toBeGreaterThan(additions[i - 1].rows)
  }
  const rowDeltas = additions.slice(1).map((sample, i) => sample.rows - additions[i].rows)
  expect(Math.min(...rowDeltas)).toBeGreaterThanOrEqual(1)
  expect(rowDeltas.filter(delta => delta === 1).length).toBeGreaterThanOrEqual(2)
  if (additions.length > 2) {
    const addGaps = additions.slice(2).map((sample, i) => sample.atMs - additions[i + 1].atMs)
    expect(Math.max(...addGaps)).toBeGreaterThanOrEqual(300)
  }
  expect(observed.scroll.scrollHeight).toBeGreaterThan(observed.scroll.clientHeight)
  expect(observed.scroll.scrollTop).toBeGreaterThan(0)
  expect(observed.scroll.bottomGap).toBeLessThan(3)
  await expect(page.locator('.feed-row.is-new')).toHaveCount(1)
  await page.locator('.action-feed').scrollIntoViewIfNeeded()
  await expect(page.locator('.feed-row.is-new')).toBeInViewport()
  const feedScreen = await shot(page, 'ai-feed-scrolled.png')
  evidence('mock-cadence', { ai_delay_ms: delayMs, first_actor: firstActor, next_actor: secondActor,
    observed, action_gap_ms: gaps, ui_feed_additions: additions,
    screenshots: [firstScreen, secondScreen, feedScreen], ui_forced_refreshes: 0 })
})

test('turn_human_countdown · 真人光圈、底部按钮组、倒计时数字与进度条减少', async ({ page }) => {
  const id = await create(page, true)
  await command(page, id, '开始', 'start')
  const dock = page.locator('.hand-dock[data-turn="me"]')
  await expect(dock).toBeVisible()
  await expect(dock.locator('.dock-state')).toHaveText('轮到你出牌')
  await expect(page.locator('.seat.is-human.is-actor .turn-badge')).toHaveText('轮到我')
  await expect(page.locator('.seat.is-human.is-actor')).toBeInViewport()
  await dock.scrollIntoViewIfNeeded()
  await expect(dock).toBeInViewport()
  for (const label of ['弃牌', '加注', '全押']) {
    await expect(dock.getByRole('button', { name: label, exact: true })).toBeEnabled()
  }
  await expect(dock.getByRole('button', { name: /^跟注/ })).toBeEnabled()
  const countdown = dock.locator('.countdown')
  await expect(countdown).toHaveText(/[1-5]s/)
  const bar = dock.getByRole('progressbar', { name: '真人行动剩余时间' })
  const before = { text: await countdown.innerText(), percent: Number(await bar.getAttribute('aria-valuenow')) }
  const halo = await page.locator('.seat.is-human.is-actor').evaluate(el => {
    const style = getComputedStyle(el)
    return { shadow: style.boxShadow, animation: style.animationName }
  })
  expect(halo.shadow).not.toBe('none')
  expect(halo.animation).toContain('actor-pulse')
  const beforeScreen = await shot(page, 'human-countdown-before.png')
  const began = performance.now()
  await expect.poll(async () => Number((await countdown.innerText()).replace('s', '')),
    { timeout: 2_500, intervals: [100] }).toBeLessThan(Number(before.text.replace('s', '')))
  await expect.poll(async () => Number(await bar.getAttribute('aria-valuenow')),
    { timeout: 1000, intervals: [50] }).toBeLessThan(before.percent)
  const after = { text: await countdown.innerText(), percent: Number(await bar.getAttribute('aria-valuenow')),
    elapsedMs: Math.round(performance.now() - began) }
  const afterScreen = await shot(page, 'human-countdown-after.png')
  const action = page.waitForResponse(response => response.request().method() === 'POST' &&
    new URL(response.url()).pathname === `/api/v1/games/${id}/action`)
  await dock.getByRole('button', { name: /^跟注/ }).click()
  expect((await action).status()).toBe(200)
  await expect(page.locator('.hand-dock[data-turn="ai"]')).toBeVisible()
  await expect(page.locator('.seat.is-actor .turn-badge')).toHaveText('思考中')
  await expect(dock.getByRole('button', { name: '过牌', exact: true })).toBeEnabled({ timeout: 6500 })
  const view = await http(page, id, true)
  expect(view.data.hand.waiting_for).toBe('human_action')
  expect(view.data.player.legal_actions.can_check).toBe(true)
  evidence('human-countdown', { before, after, halo, call_http: 200, check_button_on_next_turn: true,
    dock_in_viewport: true, buttons: ['弃牌', '过牌', '跟注', '加注', '全押'],
    screenshots: [beforeScreen, afterScreen] })
})

async function nextAction(page: Page, id: string, previous: number) {
  let view: any
  await expect.poll(async () => {
    view = (await http(page, id)).data
    return view.last_action?.sequence ?? 0
  }, { timeout: 3500, intervals: [25, 50] }).toBeGreaterThan(previous)
  return view
}

test('turn_interrupt · AI 停顿窗口中 pause/stop 快速响应且状态不再推进', async ({ page }) => {
  const id = await create(page)
  await command(page, id, '开始', 'start')
  await expect(page.locator('.hand-dock[data-turn="ai"]')).toBeVisible()
  const baseline = (await http(page, id)).data
  // 紧跟一次真实 AI 提交发送命令，保证还处于新的一整个 1.5s 停顿窗口。
  const duringPause = await nextAction(page, id, baseline.last_action?.sequence ?? 0)
  const paused = await command(page, id, '暂停', 'pause')
  expect(paused.elapsedMs).toBeLessThan(600)
  expect(paused.view.status).toBe('paused')
  expect(paused.view.last_action.sequence).toBe(duringPause.last_action.sequence)
  await expect(page.locator('.hand-dock .dock-state')).toHaveText('牌局已暂停')
  await page.waitForTimeout(delayMs + 200)
  expect((await http(page, id)).data.revision).toBe(paused.view.revision)
  await command(page, id, '恢复', 'resume')
  const duringStop = await nextAction(page, id, paused.view.last_action.sequence)
  const stopped = await command(page, id, '停止', 'stop')
  expect(stopped.elapsedMs).toBeLessThan(600)
  expect(stopped.view.status).toBe('stopped')
  expect(stopped.view.last_action.sequence).toBe(duringStop.last_action.sequence)
  await page.waitForTimeout(delayMs + 200)
  expect((await http(page, id)).data.revision).toBe(stopped.view.revision)
  evidence('interrupt', { ai_delay_ms: delayMs, pause_response_ms: paused.elapsedMs,
    stop_response_ms: stopped.elapsedMs, pause_action_sequence: paused.view.last_action.sequence,
    stop_action_sequence: stopped.view.last_action.sequence, stable_after_wait_ms: delayMs + 200,
    paused_revision: paused.view.revision, stopped_revision: stopped.view.revision,
    no_action_advanced_during_control: true })
})
