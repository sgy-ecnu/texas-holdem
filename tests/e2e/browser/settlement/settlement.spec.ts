import { expect, test, type Page } from '@playwright/test'
import fs from 'node:fs'
import path from 'node:path'

// 谁赢了结算 + 街进度指示器 + 模型短名 · 真实链路验收。
//
// 证据边界（与 action-popover / chip-badge / human 同源的安全约定）：
// - 所有 HTTP 均由真实 Chromium 页面的原生 fetch 或真实 UI 按钮发出；
//   不 route/fulfill、不使用 APIRequestContext、不读取 cookies()/storageState()/document.cookie。
// - Cookie/Set-Cookie 原值一律不读取、不输出。
// - mock 目录 allow_paid_models=false，不产生任何真实 provider 请求。
//
// 真实产品行为（写入测试前提）：
// - 全 AI mock 决策器只会 check/call（internal/agent/mock.go），故 2 座全 AI 每手必然走到摊牌；
//   要产生 uncontested（弃牌结束）必须由真人主动弃牌。
// - 手间权威窗口：摊牌 12s、弃牌 8s（internal/service/worker.go + config 默认），窗口内不开下一手。

const OUT = process.env.SETTLE_SCREEN_DIR
  ? path.resolve(process.env.SETTLE_SCREEN_DIR)
  : path.resolve(process.cwd(), 'settlement-artifacts')
fs.mkdirSync(OUT, { recursive: true })

const created: string[] = []
const evidence: { scenario: string; facts: Record<string, unknown> }[] = []

function record(scenario: string, facts: Record<string, unknown>) {
  evidence.push({ scenario, facts })
  fs.writeFileSync(process.env.SETTLE_EVIDENCE_FILE!, JSON.stringify(evidence, null, 2))
  console.log(`EVIDENCE ${scenario}: ${JSON.stringify(facts)}`)
}

interface HttpResult {
  status: number
  data: any
}

async function http(page: Page, pathname: string, body?: unknown): Promise<HttpResult> {
  return page.evaluate(
    async ({ p, b }) => {
      const response = await fetch(p, {
        credentials: 'same-origin',
        ...(b === undefined
          ? {}
          : {
              method: 'POST',
              headers: { 'Content-Type': 'application/json' },
              body: JSON.stringify(b),
            }),
      })
      return { status: response.status, data: await response.json() }
    },
    { p: pathname, b: body },
  )
}

async function getJson(page: Page, pathname: string): Promise<any> {
  const result = await http(page, pathname)
  expect(result.status, `GET ${pathname} 应 200`).toBe(200)
  return result.data
}

const field = (page: Page, name: string) =>
  page.locator('.form-grid .field').filter({ hasText: name })
const gamePath = (id: string) => `/api/v1/games/${id}`
const playerPath = (id: string) => `/api/v1/games/${id}/player`
const handPath = (id: string, n: number) => `/api/v1/games/${id}/hands/${n}`

const HAND_CATEGORIES = ['高牌', '一对', '两对', '三条', '顺子', '同花', '葫芦', '四条', '同花顺']

interface CreateOptions {
  humanSeat?: number | null
  hands?: number
  count?: number
  sb?: number
  bb?: number
  button?: number
}

async function create(page: Page, options: CreateOptions = {}) {
  const { humanSeat = null, hands = 2, count = 2, sb = 5, bb = 10, button = 0 } = options
  await page.goto('/')
  await expect(page.getByRole('button', { name: '创建牌局', exact: true })).toBeVisible()
  if (count !== 2) {
    await field(page, '座位数').locator('select').selectOption(String(count))
  }
  if (humanSeat !== null) {
    await page.getByLabel('真人座位', { exact: true }).selectOption(String(humanSeat))
  }
  await field(page, '按钮位').locator('select').selectOption(String(button))
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
  await expect
    .poll(
      async () => {
        const response = await http(page, playerPath(id))
        expect(response.status).toBe(200)
        return response.data.hand?.waiting_for
      },
      { timeout: 20_000, intervals: [50, 100] },
    )
    .toBe('human_action')
}

// ---------------- 真实 DOM 采样器 ----------------

interface Sample {
  t: number
  status: string | null
  nodes: { node: string; state: string; text: string }[]
  settlement: string | null
  bar: boolean
  barHead: string | null
  barPots: string[]
  barRefunds: string | null
  winnerTags: string[]
  winnerRanks: string[]
  reveal: number
  winners: number
  flyChips: number
  board: number
  handNoText: string | null
}

// 在真实页面安装采样器：每 80ms 记录一次关键 DOM 状态，不触碰 React 状态、不注入 mock。
async function installSampler(page: Page) {
  await page.evaluate(() => {
    const w = window as any
    if (w.__settleSampler) return
    const pick = (sel: string): string | null => document.querySelector(sel)?.textContent?.trim() ?? null
    const statusbar = (): string | null => {
      for (const b of Array.from(document.querySelectorAll('.statusbar .metric'))) {
        const t = b.textContent ?? ''
        if (t.includes('手数')) return t.trim()
      }
      return null
    }
    const sample = () => {
      const stage = document.querySelector('.stage')
      w.__settleSamples.push({
        t: performance.now(),
        status: pick('.street-status'),
        nodes: Array.from(document.querySelectorAll('.street-node')).map((n) => ({
          node: (n as HTMLElement).dataset.node ?? '',
          state: (n as HTMLElement).dataset.state ?? '',
          text: n.textContent?.trim() ?? '',
        })),
        settlement: stage?.getAttribute('data-settlement') ?? null,
        bar: !!document.querySelector('.settlement-bar'),
        barHead: pick('.settlement-bar .settlement-head'),
        barPots: Array.from(document.querySelectorAll('.settlement-bar .settlement-pot')).map((p) =>
          (p.textContent ?? '').replace(/\s+/g, ' ').trim(),
        ),
        barRefunds: pick('.settlement-bar .settlement-refunds'),
        winnerTags: Array.from(document.querySelectorAll('.winner-tag .winner-amount')).map(
          (e) => e.textContent?.trim() ?? '',
        ),
        winnerRanks: Array.from(document.querySelectorAll('.winner-tag .winner-rank')).map(
          (e) => e.textContent?.trim() ?? '',
        ),
        reveal: document.querySelectorAll('.hole-reveal').length,
        winners: document.querySelectorAll('.seat.is-winner').length,
        flyChips: document.querySelectorAll('.settle-chip').length,
        board: document.querySelectorAll('.board .card').length,
        handNoText: statusbar(),
      })
      if (w.__settleSamples.length > 6000) w.__settleSamples.shift()
    }
    w.__settleSamples = []
    w.__settleSampler = window.setInterval(sample, 80)
    sample()
  })
}

async function samples(page: Page): Promise<Sample[]> {
  return page.evaluate(() => (window as any).__settleSamples ?? [])
}

function runs(list: Sample[], predicate: (s: Sample) => boolean): { start: number; end: number; dur: number }[] {
  const out: { start: number; end: number; dur: number }[] = []
  let cur: { start: number; end: number } | null = null
  for (const s of list) {
    if (predicate(s)) {
      if (!cur) cur = { start: s.t, end: s.t }
      else cur.end = s.t
    } else if (cur) {
      out.push({ ...cur, dur: cur.end - cur.start })
      cur = null
    }
  }
  if (cur) out.push({ ...cur, dur: cur.end - cur.start })
  return out
}

// 轮询到某条件成立，返回成立时刻（ms，perf.now 对齐）；超时抛出。
async function pollUntil(page: Page, id: string, predicate: (g: any) => boolean, timeoutMs: number): Promise<{ t: number; g: any }> {
  const deadline = Date.now() + timeoutMs
  let last: any = null
  while (Date.now() < deadline) {
    last = await getJson(page, gamePath(id))
    if (predicate(last)) {
      return { t: await page.evaluate(() => performance.now()), g: last }
    }
    await page.waitForTimeout(120)
  }
  throw new Error(`pollUntil 超时（${timeoutMs}ms），最后状态 hand=${last?.hand?.number} completed=${last?.hands_completed}`)
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

// ---------------- 场景 1：全 AI 摊牌（模型名/街轨道/摊牌结算/手间窗口） ----------------

test('settlement_showdown · 模型短名 + 5 节点街轨道 + 摊牌结算与手间窗口', async ({ page }) => {
  test.setTimeout(240_000)
  await page.setViewportSize({ width: 1440, height: 1000 })
  const { id } = await create(page, { humanSeat: null, hands: 2, count: 2, sb: 5, bb: 10, button: 0 })

  // ---- 模型列表：真实服务当前只提供 mock（短名未收录 → 座位卡省略短名，属预期） ----
  const models = await getJson(page, '/api/v1/models')
  expect(models.models.map((m: any) => m.ref), 'mock 目录应仅提供 mock 模型').toEqual(['mock'])

  await installSampler(page)
  await page.getByRole('button', { name: '开始', exact: true }).click()

  // ---- 街轨道：等翻牌前下注进行中并截图 ----
  await expect
    .poll(async () => page.locator('.street-status').textContent(), { timeout: 30_000, intervals: [100, 200] })
    .toContain('下注进行中')
  const nodes = await page.locator('.street-node').evaluateAll((els) =>
    els.map((e) => ({
      node: (e as HTMLElement).dataset.node,
      state: (e as HTMLElement).dataset.state,
      text: e.textContent?.trim() ?? '',
    })),
  )
  expect(nodes.map((n) => n.node), '街轨道应为 5 节点').toEqual(['preflop', 'flop', 'turn', 'river', 'complete'])
  expect(nodes[0].state, '翻牌前应为当前街').toBe('current')
  await page.locator('.street-wrap').screenshot({ path: path.join(OUT, 'street-preflop.png'), animations: 'disabled' })

  // ---- 进入翻牌：轨道推进 ----
  await page
    .locator('.street-node[data-node="flop"][data-state="current"]')
    .waitFor({ state: 'visible', timeout: 90_000 })
  await page.locator('.street-wrap').screenshot({ path: path.join(OUT, 'street-flop.png'), animations: 'disabled' })

  // ---- 切街提示：翻牌→转牌，约 1.5s，抓拍真实文案 ----
  await page.waitForFunction(
    () => /翻牌下注已结束 · 转牌下注进行中/.test(document.querySelector('.street-status')?.textContent ?? ''),
    undefined,
    { timeout: 90_000, polling: 100 },
  )
  const transitionText = await page.locator('.street-status').textContent()
  await page.locator('.street-wrap').screenshot({ path: path.join(OUT, 'street-transition.png'), animations: 'disabled' })

  // ---- 摊牌结算：等待结算条出现，并在 hold 阶段取稳定帧 ----
  await page.locator('.settlement-bar').waitFor({ state: 'visible', timeout: 150_000 })
  // 结算条出现的同一刻即本手完成；立即记录完成时刻，作为手间窗口计时起点。
  const done = await pollUntil(page, id, (g) => (g.hands_completed ?? 0) >= 1, 30_000)
  await page.waitForFunction(
    () => document.querySelector('.stage')?.getAttribute('data-settlement') === 'hold',
    undefined,
    { timeout: 30_000, polling: 50 },
  )
  await page.screenshot({ path: path.join(OUT, 'settlement-showdown.png'), animations: 'disabled' })
  await page.locator('.settlement-bar').screenshot({ path: path.join(OUT, 'settlement-bar-showdown.png'), animations: 'disabled' })
  const winnerLoc = page.locator('.winner-tag').first()
  if (await winnerLoc.isVisible().catch(() => false)) {
    const box = await winnerLoc.boundingBox()
    if (box) {
      await page.screenshot({
        path: path.join(OUT, 'winner-tag-crop.png'),
        animations: 'disabled',
        clip: {
          x: Math.max(0, box.x - 80),
          y: Math.max(0, box.y - 60),
          width: Math.min(1440, box.width + 160),
          height: Math.min(1000, box.height + 120),
        },
      })
    }
  }

  // ---- DOM 断言：翻牌 + 赢家亮金 + 「赢 +X」标签（含牌型）+ 筹码飞行 + 结算条 ----
  const barText = (await page.locator('.settlement-bar').textContent()) ?? ''
  expect(barText, '结算条应标「摊牌结算」').toContain('摊牌结算')
  expect(barText, '结算条应含主池').toContain('主池')
  expect(await page.locator('.hole-reveal').count(), '摊牌应翻开 2 家底牌').toBe(2)
  expect(await page.locator('.hole-reveal .settle-face .card').count(), '每家 2 张牌面').toBe(4)
  expect(await page.locator('.seat.is-winner').count(), '应至少 1 个赢家亮金').toBeGreaterThanOrEqual(1)
  expect(await page.locator('.winner-tag').count(), '应至少 1 个赢家标签').toBeGreaterThanOrEqual(1)
  const tagText = (await winnerLoc.textContent()) ?? ''
  expect(tagText, '赢家标签金额应形如「赢 +X」或「你赢了 +X」').toMatch(/(赢|你赢了) \+\d/)
  const rankText = (await page.locator('.winner-tag .winner-rank').first().textContent()) ?? ''
  expect(
    HAND_CATEGORIES.some((c) => rankText.includes(c)),
    `赢家标签应含中文牌型，实际「${rankText}」`,
  ).toBe(true)
  const flyCount = await page.locator('.settle-chip').count()
  expect(flyCount, '筹码应飞向赢家（每赢家 3 枚）').toBeGreaterThanOrEqual(3)
  const potWinnerText = (await page.locator('.settlement-bar .pot-winners').first().textContent()) ?? ''
  expect(potWinnerText.length, '结算条应列出各池赢家与牌型').toBeGreaterThan(0)
  // 结算显示期间取出文案快照；之后等 idle/下一手会清空结算条，不能再查询。
  const settlementHeadText = (await page.locator('.settlement-head').textContent())?.trim() ?? ''
  const settlementPots = await page.locator('.settlement-pot').allTextContents()
  const settlementRefundsCount = await page.locator('.settlement-refunds').count()

  // ---- 座位卡：模型短名现象 + 第二行「剩余」单行不换行 ----
  const seatFacts = await page.locator('.seat').evaluateAll((els) =>
    els.map((el) => {
      const row2 = el.querySelector('.seat-row2') as HTMLElement | null
      const stack = el.querySelector('.seat-stack') as HTMLElement | null
      const model = el.querySelector('.seat-model')
      return {
        title: el.getAttribute('title'),
        row1: el.querySelector('.seat-row1')?.textContent?.replace(/\s+/g, ' ').trim() ?? null,
        row2: row2?.textContent?.replace(/\s+/g, ' ').trim() ?? null,
        model_short: model?.textContent ?? null,
        row2_white_space: row2 ? getComputedStyle(row2).whiteSpace : null,
        stack_height: stack ? Math.round(stack.getBoundingClientRect().height) : null,
      }
    }),
  )
  for (const s of seatFacts) {
    expect(s.row2_white_space, '第二行应为 nowrap').toBe('nowrap')
    expect(s.stack_height ?? 0, `第二行「剩余」应单行不换行，实际高度 ${s.stack_height}`).toBeLessThan(30)
    expect(s.row2 ?? '', '第二行应显示剩余筹码').toContain('剩余')
  }
  const shortNames = seatFacts.map((s) => s.model_short).filter((x): x is string => x !== null)
  expect(shortNames, 'mock 模型未收录短名 → 座位卡省略短名（预期现象）').toEqual([])

  // ---- API 层：CompletedHandDTO.showdown 字段 ----
  const hand = await getJson(page, handPath(id, 1))
  expect(hand.reason, '全 AI 2 座应摊牌').toBe('showdown')
  expect(hand.showdown.length, 'showdown 应含 2 家').toBe(2)
  for (const s of hand.showdown) {
    expect(Object.keys(s).sort(), 'ShowdownDTO 字段').toEqual(['best_five', 'hand_rank', 'seat'])
    expect(HAND_CATEGORIES, `hand_rank「${s.hand_rank}」应为中文牌型`).toContain(s.hand_rank)
    expect(s.best_five.length, 'best_five 应 5 张').toBe(5)
  }
  expect(hand.hole_cards.length, '摊牌应公开 2 家底牌').toBe(2)
  for (const h of hand.hole_cards) expect(h.cards.length).toBe(2)
  expect(hand.board.length, '摊牌应有 5 张公共牌').toBe(5)
  expect(hand.pots.length, '应有至少 1 个池').toBeGreaterThanOrEqual(1)
  expect(hand.final_stacks.length, '应有全部座位的最终筹码').toBe(2)

  // 赢家座位与 API 一致：DOM 亮金座位 ∩ API payouts>0 一致。
  const payoutSeats = hand.payouts.filter((p: any) => p.amount > 0).map((p: any) => p.seat)
  const winnerSeatsDom = await page
    .locator('.seat-slot:has(.seat.is-winner)')
    .evaluateAll((els) => els.map((e) => Number((e as HTMLElement).dataset.seat)))
  expect([...new Set(winnerSeatsDom)].sort()).toEqual([...new Set(payoutSeats)].sort())

  // 结算期间座位最终筹码应等于 API final_stacks。
  const stackBySeatApi = new Map<number, number>(hand.final_stacks.map((s: any) => [s.seat, s.stack]))
  const stackDom = await page.locator('.seat-slot').evaluateAll((els) =>
    els.map((e) => {
      const slot = e as HTMLElement
      const text = slot.querySelector('.seat-stack')?.textContent?.replace(/[^0-9]/g, '') ?? ''
      return { seat: Number(slot.dataset.seat), stack: Number(text) }
    }),
  )
  for (const s of stackDom) expect(s.stack, `座位 ${s.seat} 结算筹码应等于 final_stacks`).toBe(stackBySeatApi.get(s.seat))

  // ---- 手间窗口：动画期间不得开下一手；摊牌窗口约 12s ----
  // 等结算展示完整结束（phase 回到 idle）再据采样评估窗口时长，避免在动画进行中截断采样。
  await page.waitForFunction(
    () => document.querySelector('.stage')?.getAttribute('data-settlement') === 'idle',
    undefined,
    { timeout: 20_000, polling: 50 },
  )
  const duringWindow = await samples(page)
  const settlementRun = runs(duringWindow, (s) => s.settlement !== null && s.settlement !== 'idle')
  const maxRun = settlementRun.reduce((m, r) => Math.max(m, r.dur), 0)
  const boardResetDuring = duringWindow.some(
    (s) => s.settlement !== null && s.settlement !== 'idle' && s.board < 5,
  )
  expect(maxRun, `结算展示窗口应持续约 6.5s（实际 ${Math.round(maxRun)}ms）`).toBeGreaterThanOrEqual(5500)
  expect(boardResetDuring, '结算动画期间公共牌不应被重置（不得开下一手）').toBe(false)
  // 切街提示真实停留时长：文案「X下注已结束 · Y下注进行中」应约 1.5s。
  const transitionRuns = runs(duringWindow, (s) => /下注已结束 · .+下注进行中/.test(s.status ?? '')).map((r) => Math.round(r.dur))
  expect(transitionRuns.length, '应至少观察到一次切街提示').toBeGreaterThanOrEqual(1)
  for (const d of transitionRuns) expect(d, `切街提示应约 1.5s，实测 ${d}ms`).toBeGreaterThanOrEqual(1000)
  for (const d of transitionRuns) expect(d, `切街提示不应长期滞留，实测 ${d}ms`).toBeLessThanOrEqual(2200)
  const next = await pollUntil(page, id, (g) => g.hand?.number === 2 || ['completed', 'stopped', 'failed'].includes(g.status), 25_000)
  const gap = next.t - done.t
  expect(gap, `摊牌手间窗口应约 12s（实测 ${Math.round(gap)}ms）`).toBeGreaterThanOrEqual(9000)

  record('showdown', {
    models: models.models.map((m: any) => ({ ref: m.ref, label: m.label })),
    seat_facts: seatFacts,
    short_names: shortNames,
    street_nodes: nodes,
    transition_text: transitionText?.trim(),
    settlement_head: settlementHeadText,
    settlement_pots: settlementPots,
    settlement_refunds: settlementRefundsCount,
    winner_tag: tagText.trim(),
    winner_rank: rankText.trim(),
    transition_runs_ms: transitionRuns,
    fly_chip_elements: flyCount,
    reveal_count: 2,
    winner_dom_seats: [...new Set(winnerSeatsDom)].sort(),
    api_showdown: hand.showdown,
    api_payout_seats: payoutSeats,
    api_final_stacks: hand.final_stacks,
    settlement_window_ms: Math.round(maxRun),
    hand_gap_ms: Math.round(gap),
    screenshots: ['street-preflop.png', 'street-flop.png', 'street-transition.png', 'settlement-showdown.png', 'settlement-bar-showdown.png', 'winner-tag-crop.png'],
  })
})

// ---------------- 场景 2：弃牌结束（uncontested）：弃牌者不翻 + 底牌保密 ----------------

test('settlement_uncontested · 真人弃牌：弃牌者不翻牌 + hole_cards 不含他人底牌', async ({ page }) => {
  test.setTimeout(180_000)
  await page.setViewportSize({ width: 1440, height: 1000 })
  // 真人座位 0（按钮/小盲），2 座；翻牌前真人先行动 → 直接弃牌 → AI 无人跟注获胜。
  const { id } = await create(page, { humanSeat: 0, hands: 2, count: 2, sb: 5, bb: 10, button: 0 })
  await installSampler(page)
  await expect(page.getByText('真人视角')).toBeVisible()
  await page.getByRole('button', { name: '开始', exact: true }).click()
  await waitHuman(page, id)

  const panel = page.locator('.action-panel')
  const respPromise = page.waitForResponse(
    (r) => r.request().method() === 'POST' && new URL(r.url()).pathname === `/api/v1/games/${id}/action`,
  )
  await panel.getByRole('button', { name: '弃牌', exact: true }).click()
  expect((await respPromise).status()).toBe(200)

  await page.locator('.settlement-bar').waitFor({ state: 'visible', timeout: 40_000 })
  // 结算条出现即本手完成；立即记录完成时刻作为手间窗口计时起点（弃牌窗口约 8s）。
  const done = await pollUntil(page, id, (g) => (g.hands_completed ?? 0) >= 1, 20_000)
  // 等进入 award/hold（筹码飞行阶段）；reveal 阶段尚无飞行动画。
  await page.waitForFunction(
    () => ['award', 'hold'].includes(document.querySelector('.stage')?.getAttribute('data-settlement') ?? ''),
    undefined,
    { timeout: 20_000, polling: 50 },
  )
  await page.screenshot({ path: path.join(OUT, 'settlement-uncontested.png'), animations: 'disabled' })
  await page.locator('.settlement-bar').screenshot({ path: path.join(OUT, 'settlement-bar-uncontested.png'), animations: 'disabled' })

  const barText = (await page.locator('.settlement-bar').textContent()) ?? ''
  expect(barText, '结算条应标「弃牌结算」').toContain('弃牌结算')
  expect(await page.locator('.hole-reveal').count(), '弃牌结束不得翻任何底牌').toBe(0)
  const tagText = (await page.locator('.winner-tag').first().textContent()) ?? ''
  expect(tagText, '赢家标签应形如「赢 +X」').toMatch(/赢 \+\d/)
  expect(tagText, '弃牌结束应提示其余玩家弃牌').toContain('其余玩家弃牌')
  expect(await page.locator('.winner-tag .winner-rank').count(), '弃牌结束不应展示牌型').toBe(0)
  const flyCount = await page.locator('.settle-chip').count()
  expect(flyCount, '筹码仍应飞向赢家').toBeGreaterThanOrEqual(3)
  // 结算显示期间取出文案快照；之后等下一手会清空结算条。
  const settlementHeadText = (await page.locator('.settlement-head').textContent())?.trim() ?? ''
  const settlementPots = await page.locator('.settlement-pot').allTextContents()

  // API：uncontested 保密断言。
  const hand = await getJson(page, handPath(id, 1))
  expect(hand.reason).toBe('uncontested')
  expect(hand.showdown, '弃牌结束 showdown 必须为空').toEqual([])
  expect(hand.hole_cards, '弃牌结束不得下发任何底牌（含弃牌者与他人）').toEqual([])
  expect(hand.board, '翻牌前弃牌结束公共牌为空').toEqual([])

  const next = await pollUntil(page, id, (g) => g.hand?.number === 2 || ['completed', 'stopped', 'failed'].includes(g.status), 20_000)
  const gap = next.t - done.t
  expect(gap, `弃牌手间窗口应约 8s（实测 ${Math.round(gap)}ms）`).toBeGreaterThanOrEqual(6000)

  record('uncontested', {
    settlement_head: settlementHeadText,
    settlement_pots: settlementPots,
    winner_tag: tagText.trim(),
    reveal_count: 0,
    fly_chip_elements: flyCount,
    api_reason: hand.reason,
    api_showdown: hand.showdown,
    api_hole_cards: hand.hole_cards,
    api_board: hand.board,
    hand_gap_ms: Math.round(gap),
    screenshots: ['settlement-uncontested.png', 'settlement-bar-uncontested.png'],
  })
})
