import { expect, test, type Page } from '@playwright/test'
import fs from 'node:fs'

// ActionPopover 真实链路验收：ActionDTO.amount 下发语义 + 座位旁动作标签弹出动效。
//
// 证据边界（与 human.spec.ts 一致的安全约定）：
// - 所有 HTTP 均由真实 Chromium 页面的原生 fetch 或真实 UI 按钮发出；
//   不 route/fulfill、不使用 APIRequestContext、不读取 cookies()/storageState()/document.cookie。
// - Cookie/Set-Cookie 原值一律不读取、不输出。
// - mock 目录 allow_paid_models=false，不产生任何真实 provider 请求。
//
// 已知产品行为：SpectatorView 把「进入/切视角后观察到的第一个非空 last_action」只作基线、
// 不弹标签（防止进入时重播旧动作）。因此测试必须确保目标动作不是该基线动作。

const created: string[] = []
const evidence: { scenario: string; facts: Record<string, unknown> }[] = []

function record(scenario: string, facts: Record<string, unknown>) {
  evidence.push({ scenario, facts })
  fs.writeFileSync(process.env.AP_EVIDENCE_FILE!, JSON.stringify(evidence, null, 2))
  console.log(`EVIDENCE ${scenario}: ${JSON.stringify(facts)}`)
}

interface HttpResult {
  status: number
  data: any
}

async function http(page: Page, path: string, body?: unknown): Promise<HttpResult> {
  return page.evaluate(
    async ({ path, body }) => {
      const response = await fetch(path, {
        credentials: 'same-origin',
        ...(body === undefined
          ? {}
          : {
              method: 'POST',
              headers: { 'Content-Type': 'application/json' },
              body: JSON.stringify(body),
            }),
      })
      return { status: response.status, data: await response.json() }
    },
    { path, body },
  )
}

async function getJson(page: Page, path: string): Promise<any> {
  const result = await http(page, path)
  expect(result.status).toBe(200)
  return result.data
}

const field = (page: Page, name: string) =>
  page.locator('.form-grid .field').filter({ hasText: name })
const gamePath = (id: string) => `/api/v1/games/${id}`
const playerPath = (id: string) => `/api/v1/games/${id}/player`

// ---------------- 座位旁标签观测（真实 DOM MutationObserver） ----------------

interface PopEvent {
  event: 'add' | 'remove'
  id: number | null
  text: string
  seat: number | null
  t: number
}

// 在真实页面安装 MutationObserver：只记录 .action-popover 的增删与时间戳，
// 不触碰 React 状态、不注入 mock。id 用于把 add 与 remove 配对测存活时长。
async function installPopoverObserver(page: Page) {
  await page.evaluate(() => {
    const w = window as any
    if (w.__popoverInstalled) return
    w.__popoverInstalled = true
    w.__popoverEvents = []
    const meta = new WeakMap<Element, { id: number; text: string; seat: number | null }>()
    let next = 1
    const seatOf = (node: Element): number | null => {
      const slot = node.closest?.('.seat-slot')
      const raw = slot?.getAttribute('data-seat')
      return raw === null || raw === undefined ? null : Number(raw)
    }
    const observer = new MutationObserver((records) => {
      for (const record of records) {
        record.addedNodes.forEach((node) => {
          if (node.nodeType === 1 && (node as Element).classList.contains('action-popover')) {
            const el = node as Element
            const id = next++
            const text = el.textContent ?? ''
            const seat = seatOf(el)
            meta.set(el, { id, text, seat })
            w.__popoverEvents.push({ event: 'add', id, text, seat, t: performance.now() })
          }
        })
        record.removedNodes.forEach((node) => {
          if (node.nodeType === 1 && (node as Element).classList.contains('action-popover')) {
            const el = node as Element
            const stored = meta.get(el)
            w.__popoverEvents.push({
              event: 'remove',
              id: stored?.id ?? null,
              text: stored?.text ?? el.textContent ?? '',
              seat: stored?.seat ?? null,
              t: performance.now(),
            })
          }
        })
      }
    })
    observer.observe(document.body, { childList: true, subtree: true })
    w.__popoverObserver = observer
  })
}

async function popEvents(page: Page): Promise<PopEvent[]> {
  return page.evaluate(() => (window as any).__popoverEvents ?? [])
}

function pairedDurations(events: PopEvent[]): number[] {
  const adds = events.filter((e) => e.event === 'add')
  const removes = new Map(events.filter((e) => e.event === 'remove').map((e) => [e.id, e]))
  return adds
    .map((a) => {
      const r = removes.get(a.id)
      return r ? r.t - a.t : null
    })
    .filter((d): d is number => d !== null)
}

// ---------------- 创建 / 等待 ----------------

interface CreateOptions {
  humanSeat?: number | null
  hands?: number
  count?: number
  sb?: number
  bb?: number
  button?: number
}

async function create(page: Page, options: CreateOptions = {}) {
  const { humanSeat = null, hands = 1, count = 2, sb = 5, bb = 10, button = 0 } = options
  await page.goto('/')
  await expect(page.getByRole('button', { name: '创建牌局', exact: true })).toBeVisible()
  if (count !== 2) {
    await page
      .locator('.form-grid .field')
      .filter({ hasText: '座位数' })
      .locator('select')
      .selectOption(String(count))
  }
  if (humanSeat !== null) {
    await page.getByLabel('真人座位', { exact: true }).selectOption(String(humanSeat))
  }
  await page.locator('.form-grid .field').filter({ hasText: '按钮位' }).locator('select').selectOption(String(button))
  await field(page, '小盲').locator('input').fill(String(sb))
  await field(page, '大盲').locator('input').fill(String(bb))
  await field(page, '最多手数').locator('input').fill(String(hands))
  const responsePromise = page.waitForResponse(
    (r) => r.request().method() === 'POST' && new URL(r.url()).pathname === '/api/v1/games',
  )
  await page.getByRole('button', { name: '创建牌局', exact: true }).click()
  const response = await responsePromise
  expect(response.status()).toBe(201)
  const data = await response.json()
  created.push(data.id as string)
  return { id: data.id as string, data }
}

async function waitHuman(page: Page, id: string) {
  let view: any
  await expect
    .poll(
      async () => {
        const response = await http(page, playerPath(id))
        expect(response.status).toBe(200)
        view = response.data
        return view.hand?.waiting_for
      },
      { timeout: 12_000, intervals: [50, 100] },
    )
    .toBe('human_action')
  return view
}

// ---------------- API 时间线（用于按前置状态校验 amount） ----------------

interface Obs {
  revision: number
  seq: number
  handNo: number | null
  currentBet: number
  players: { seat: number; streetBet: number; stack: number }[]
}

function snapshot(g: any): Obs {
  return {
    revision: g.revision,
    seq: g.last_action?.sequence ?? 0,
    handNo: g.hand?.number ?? null,
    currentBet: g.hand?.current_bet ?? 0,
    players: (g.hand?.players ?? []).map((p: any) => ({
      seat: p.seat,
      streetBet: p.street_bet,
      stack: p.stack,
    })),
  }
}

// 以「同一手内」动作发生前的权威状态（seq = N-1 且 hand 相同）计算期望投入额：
// fold/check → 0；call → min(current_bet - seat.street_bet, seat.stack)；
// raise_to → to - seat.street_bet（追加额）。
function expectedAmount(before: Obs, action: any): number | null {
  if (action.kind === 'fold' || action.kind === 'check') return 0
  const p = before.players.find((x) => x.seat === action.seat)
  if (!p) return null
  if (action.kind === 'call') {
    const owed = Math.max(0, before.currentBet - p.streetBet)
    return Math.min(owed, p.stack)
  }
  if (action.kind === 'raise_to') return action.to - p.streetBet
  return null
}

test.afterEach(async ({ page }) => {
  for (const id of created.splice(0)) {
    const view = await http(page, gamePath(id))
    expect(view.status).toBe(200)
    if (!['stopped', 'completed', 'failed'].includes(view.data.status)) {
      const stopped = await http(page, `${gamePath(id)}/commands`, { command: 'stop' })
      expect(stopped.status).toBe(200)
    }
  }
})

// ---------------- 场景 A：全 AI mock（仅 check/call） ----------------

test('action_popover_all_ai · amount 真实下发 + 标签弹出/约1.6s消失/去重', async ({ page }) => {
  test.setTimeout(180_000)
  const SB = 5
  const BB = 10
  const { id } = await create(page, { humanSeat: null, hands: 3, count: 2, sb: SB, bb: BB, button: 0 })
  await installPopoverObserver(page)
  await page.getByRole('button', { name: '开始', exact: true }).click()

  const observations: Obs[] = []
  const actions: any[] = []
  const seen = new Set<string>()
  const push = (g: any) => {
    observations.push(snapshot(g))
    const la = g.last_action
    if (!la) return
    const key = `${la.hand_no}:${la.sequence}`
    if (seen.has(key)) return
    seen.add(key)
    actions.push(la)
  }

  push(await getJson(page, gamePath(id)))
  const calls = () => actions.filter((a) => a.kind === 'call')
  const checks = () => actions.filter((a) => a.kind === 'check')
  const enough = () =>
    calls().length >= 2 && checks().length >= 2 && actions.some((a) => a.hand_no >= 2)

  const deadline = Date.now() + 120_000
  while (Date.now() < deadline && !enough()) {
    push(await getJson(page, gamePath(id)))
    await page.waitForTimeout(40)
  }
  expect(calls().length, 'mock AI 应产生多次 call（跨手）').toBeGreaterThanOrEqual(2)
  expect(checks().length, 'mock AI 应产生多次 check').toBeGreaterThanOrEqual(2)

  // 逐动作校验 amount 与 to 语义。
  const reconstructed: any[] = []
  for (const action of actions) {
    if (action.kind === 'fold' || action.kind === 'check') {
      expect(action.amount, `${action.kind} 的 amount 应为 0`).toBe(0)
      expect(action.to, `${action.kind} 的 to 应为 0`).toBe(0)
    } else if (action.kind === 'call') {
      expect(action.to, 'call 的 to 应为 0').toBe(0)
      // mock 决策器只会 check/call（从不加注），故每手唯一的 call 都是小盲翻前补差：
      // amount 应为 大盲 - 小盲。这直接验证「call 的 amount = 跟注额」。
      expect(action.amount, 'call 的 amount 应为大盲-小盲').toBe(BB - SB)
    } else if (action.kind === 'raise_to') {
      expect(action.to, 'raise_to 的 to 应是本街累计目标值 >0').toBeGreaterThan(0)
      expect(action.amount, 'raise_to 的 amount（追加额）应 >0').toBeGreaterThan(0)
      expect(action.amount).toBeLessThanOrEqual(action.to)
    } else {
      throw new Error('未知动作类型 ' + action.kind)
    }
    // 同手前置状态可确定时，进一步核对精确金额（跨手首个动作无同手前置，跳过）。
    const before = [...observations]
      .reverse()
      .find((o) => o.seq === action.sequence - 1 && o.handNo === action.hand_no)
    if (before) {
      const expected = expectedAmount(before, action)
      expect(expected, `无法计算 seq=${action.sequence} 的期望金额`).not.toBeNull()
      expect(action.amount, `seq=${action.sequence} ${action.kind} amount`).toBe(expected)
      reconstructed.push({
        hand: action.hand_no,
        seq: action.sequence,
        seat: action.seat,
        kind: action.kind,
        to: action.to,
        amount: action.amount,
        expected,
      })
    }
  }
  expect(reconstructed.length, '至少应有可确定前置状态的动作完成精确核对').toBeGreaterThan(0)
  expect(calls().every((a) => a.amount > 0), '所有 call 的实际投入额都应 >0').toBe(true)
  record('api/all_ai', {
    observed_actions: actions.length,
    kinds: [...new Set(actions.map((a) => a.kind))],
    calls: calls().map((a) => ({ hand: a.hand_no, seq: a.sequence, to: a.to, amount: a.amount })),
    checks_count: checks().length,
    reconstructed,
  })

  // 等浏览器轮询消化最新动作，再暂停：last_action 冻结，重复轮询不得重播。
  await page.waitForTimeout(2500)
  await page.getByRole('button', { name: '暂停', exact: true }).click()
  await expect(page.locator('.statusbar').first()).toContainText('已暂停')
  await page.waitForTimeout(3000)
  const addsBefore = (await popEvents(page)).filter((e) => e.event === 'add').length
  await page.waitForTimeout(9000)
  const events = await popEvents(page)
  const addsAfter = events.filter((e) => e.event === 'add').length
  expect(addsAfter, '暂停后重复轮询不应重播标签').toBe(addsBefore)

  // 标签文案 / 消失时长 / 与 amount 交叉核对。
  const adds = events.filter((e) => e.event === 'add')
  expect(adds.length).toBeGreaterThanOrEqual(2)
  expect(adds.every((a) => a.text === '过牌' || /^跟注 \d+$/.test(a.text))).toBe(true)
  const callTexts = adds.filter((a) => a.text.startsWith('跟注 ')).map((a) => a.text)
  expect(callTexts.length, '浏览器应至少弹出一次「跟注 X」').toBeGreaterThan(0)
  for (const text of callTexts) {
    expect(
      calls().some((a) => `跟注 ${a.amount}` === text),
      `标签「${text}」应与 API 的 call.amount 一致`,
    ).toBe(true)
  }
  const durations = pairedDurations(events)
  expect(durations.length).toBe(adds.length)
  expect(Math.min(...durations), '标签不应瞬时消失').toBeGreaterThan(400)
  expect(Math.max(...durations), '标签不应长期滞留').toBeLessThan(2600)
  expect(
    durations.some((d) => d >= 1400 && d <= 2100),
    '标签应存在约 1.6s 的常规存活时长',
  ).toBe(true)
  record('browser/all_ai', {
    adds: adds.length,
    texts: [...new Set(adds.map((a) => a.text))],
    call_texts: callTexts,
    add_remove_durations_ms: durations,
    dedup_adds_before_pause: addsBefore,
    dedup_adds_after_9s: addsAfter,
  })
})

// ---------------- 场景 B：真人（call/check/raise_to/fold） ----------------

test('action_popover_human · 真人四动作文案与金额 + 去重', async ({ page }) => {
  test.setTimeout(150_000)
  // 真人放在座位 1（小盲），AI 座位 0（按钮）先行动，保证前端基线由 AI 动作建立，
  // 真人四个动作都能真正触发标签。
  const { id } = await create(page, { humanSeat: 1, hands: 1, count: 3, sb: 5, bb: 10, button: 0 })
  await installPopoverObserver(page)
  await page.getByRole('button', { name: '开始', exact: true }).click()
  const panel = page.locator('.action-panel')
  const results: any[] = []

  await waitHuman(page, id)
  // 前端把第一个观察到的 last_action 作为基线（AI 按钮的动作），ActionFeed 渲染出该行动即证明基线已建立。
  await expect(page.locator('.action-feed .feed-row').first()).toBeVisible({ timeout: 12_000 })

  async function lastAddFor(seat: number): Promise<PopEvent> {
    const adds = (await popEvents(page)).filter((e) => e.event === 'add' && e.seat === seat)
    expect(adds.length, `座位 ${seat} 应已有标签`).toBeGreaterThan(0)
    return adds[adds.length - 1]
  }

  async function act(click: () => Promise<void>): Promise<any> {
    const promise = page.waitForResponse(
      (r) =>
        r.request().method() === 'POST' &&
        new URL(r.url()).pathname === `/api/v1/games/${id}/action`,
    )
    await click()
    const resp = await promise
    expect(resp.status()).toBe(200)
    return resp.json()
  }

  // 1) 翻牌前 call：amount = legal_actions.call_amount（本座补差额）。
  {
    const before = await getJson(page, playerPath(id))
    const callAmount = before.player.legal_actions.call_amount
    const prev = (await popEvents(page)).length
    const after = await act(() => panel.getByRole('button', { name: /^跟注/ }).click())
    expect(after.last_action).toMatchObject({ seat: 1, kind: 'call', to: 0, amount: callAmount, source: 'human' })
    await expect
      .poll(async () => (await popEvents(page)).length)
      .toBeGreaterThan(prev)
    const pop = await lastAddFor(1)
    expect(pop.text).toBe(`跟注 ${callAmount}`)
    results.push({ kind: 'call', to: after.last_action.to, amount: after.last_action.amount, expected: callAmount, text: pop.text })
  }

  // 2) 翻牌 check：amount = 0。
  await waitHuman(page, id)
  {
    const prev = (await popEvents(page)).length
    const after = await act(() => panel.getByRole('button', { name: '过牌', exact: true }).click())
    expect(after.last_action).toMatchObject({ seat: 1, kind: 'check', to: 0, amount: 0, source: 'human' })
    await expect
      .poll(async () => (await popEvents(page)).length)
      .toBeGreaterThan(prev)
    const pop = await lastAddFor(1)
    expect(pop.text).toBe('过牌')
    results.push({ kind: 'check', to: after.last_action.to, amount: after.last_action.amount, expected: 0, text: pop.text })
  }

  // 3) 转牌 raise_to：amount = to - 动作前本街投入；to 仍是本街累计目标值。
  await waitHuman(page, id)
  {
    const before = await getJson(page, playerPath(id))
    const to = before.player.legal_actions.min_raise_to
    const beforeStreet = before.hand.players.find((p: any) => p.seat === 1).street_bet
    const expected = to - beforeStreet
    const prev = (await popEvents(page)).length
    await page.getByLabel('加注到（本街累计）').fill(String(to))
    const after = await act(() => panel.getByRole('button', { name: '加注', exact: true }).click())
    expect(after.last_action).toMatchObject({ seat: 1, kind: 'raise_to', to, amount: expected, source: 'human' })
    expect(after.last_action.to, 'raise_to 的 to 应是本街累计目标值').toBe(to)
    expect(after.last_action.amount, 'raise_to 的 amount 应是追加额').toBe(expected)
    await expect
      .poll(async () => (await popEvents(page)).length)
      .toBeGreaterThan(prev)
    const pop = await lastAddFor(1)
    expect(pop.text).toBe(`加注到 ${to}`)
    results.push({ kind: 'raise_to', to: after.last_action.to, amount: after.last_action.amount, expected, text: pop.text })
  }

  // 4) 河牌 fold：amount = 0。
  await waitHuman(page, id)
  {
    const prev = (await popEvents(page)).length
    const after = await act(() => panel.getByRole('button', { name: '弃牌', exact: true }).click())
    expect(after.last_action).toMatchObject({ seat: 1, kind: 'fold', to: 0, amount: 0, source: 'human' })
    await expect
      .poll(async () => (await popEvents(page)).length)
      .toBeGreaterThan(prev)
    const pop = await lastAddFor(1)
    expect(pop.text).toBe('弃牌')
    results.push({ kind: 'fold', to: after.last_action.to, amount: after.last_action.amount, expected: 0, text: pop.text })
  }

  // 整局结束：座位 1 每个动作只弹一次（真人整局浏览器多次轮询同一 last_action）。
  await expect
    .poll(async () => (await getJson(page, gamePath(id))).status, { timeout: 12_000 })
    .toBe('completed')
  const events = await popEvents(page)
  const seat1Adds = events.filter((e) => e.event === 'add' && e.seat === 1)
  expect(seat1Adds.length, '座位 1 的 4 个动作各应只弹一次（重复轮询不重播）').toBe(4)
  expect(seat1Adds.map((a) => a.text)).toEqual([
    '跟注 5',
    '过牌',
    expect.stringMatching(/^加注到 \d+$/),
    '弃牌',
  ] as any)
  const seat1Durations = pairedDurations(events.filter((e) => e.seat === 1))
  expect(seat1Durations.length).toBe(4)
  for (const d of seat1Durations) {
    expect(d, '真人动作标签应约 1.6s 后消失').toBeGreaterThanOrEqual(1300)
    expect(d).toBeLessThanOrEqual(2300)
  }
  record('browser/human', {
    actions: results,
    seat1_popover_count: seat1Adds.length,
    seat1_texts: seat1Adds.map((a) => a.text),
    seat1_durations_ms: seat1Durations,
  })
})
