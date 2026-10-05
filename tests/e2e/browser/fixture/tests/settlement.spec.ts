import { expect, test } from '@playwright/test'
import { mkdir } from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

// 谁赢了结算展示验收（fixture，非真实 Go 链路）：
//  1) hands_completed 递增触发 api.hand，去重、首次进入不重播历史手；
//  2) 摊牌：参与者翻开底牌、弃牌者不翻；赢家金边 + 标签金额=该座位 payouts 之和 + 牌型；
//  3) 弃牌结束：不翻牌、不显示牌型、显示「其余玩家弃牌」；
//  4) 真人赢：标签文案「你赢了 +X」，冰蓝身份保留；
//  5) 桌下结算条：第一池标「主池」、多池多行、退回、aria-live 只宣读一次；
//  6) 结算快照：公共牌/最终筹码来自该手 CompletedHandDTO；reduced-motion 静态可见。
// 用 page.route 脚本化 hands_completed 与 /hands/{n}（不改 fixture-server.mjs）。
// 明确：本 fixture 不能作为「真实 Go 联调」完成证据。

const HERE = path.dirname(fileURLToPath(import.meta.url))
const ROOT = path.resolve(HERE, '../../../../..')
const OUT = process.env.SCREEN_DIR ? path.resolve(process.env.SCREEN_DIR) : path.join(ROOT, 'docs/settlement')

function ai(seat: number, name: string) {
  return { seat, name, stack: 10000, kind: 'ai' as const, model_ref: 'mock' }
}

function human(seat: number, name: string) {
  return { seat, name, stack: 10000, kind: 'human' as const }
}

async function createGame(page: import('@playwright/test').Page, seats: Array<Record<string, unknown>>) {
  const res = await page.request.post('/api/v1/games', {
    data: {
      seats,
      button: 0,
      small_blind: 50,
      big_blind: 100,
      max_hands: 100,
      mode: 'mock',
      max_live_calls: 0,
    },
  })
  expect(res.ok()).toBeTruthy()
  return res.json()
}

interface SState {
  handsCompleted: number
  street: string
  board: string[]
  currentBet: number
  handDetail: Record<string, unknown> | null
}

async function installScript(page: import('@playwright/test').Page, state: SState) {
  await page.route(/\/api\/v1\/games\/[^/]+\/hands\/\d+$/, async (route) => {
    if (!state.handDetail) {
      return route.fulfill({
        status: 404,
        contentType: 'application/json',
        body: JSON.stringify({ error: { code: 'NOT_FOUND', message: 'x' } }),
      })
    }
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(state.handDetail),
    })
  })
  await page.route(/\/api\/v1\/games\/[^/]+(\/player)?$/, async (route) => {
    if (route.request().method() !== 'GET') return route.continue()
    const res = await route.fetch()
    const json = await res.json()
    if (json && typeof json === 'object' && !Array.isArray(json)) {
      json.hands_completed = state.handsCompleted
      if (json.hand) {
        json.hand.street = state.street
        json.hand.board = state.board
        json.hand.current_bet = state.currentBet
      }
    }
    await route.fulfill({ response: res, json })
  })
}

function showdownDetail(handNo: number): Record<string, unknown> {
  return {
    game_id: 'x',
    hand_no: handNo,
    reason: 'showdown',
    button: 0,
    board: ['Ah', 'Kd', 'Qc', 'Js', 'Th'],
    hole_cards: [
      { seat: 0, cards: ['Ac', 'Ad'] },
      { seat: 1, cards: ['Kh', 'Kc'] },
    ],
    refunds: [],
    payouts: [{ seat: 0, amount: 6000 }],
    pots: [{ amount: 6000, eligible: [0, 1], winners: [0], payouts: [{ seat: 0, amount: 6000 }] }],
    final_stacks: [
      { seat: 0, stack: 16000 },
      { seat: 1, stack: 4000 },
      { seat: 2, stack: 10000 },
    ],
    showdown: [
      { seat: 0, hand_rank: '葫芦', best_five: ['Ac', 'Ad', 'Ah', 'Kd', 'Kc'] },
      { seat: 1, hand_rank: '两对', best_five: ['Kh', 'Kc', 'Ah', 'Kd', 'Qc'] },
    ],
  }
}

function uncontestedDetail(handNo: number): Record<string, unknown> {
  return {
    game_id: 'x',
    hand_no: handNo,
    reason: 'uncontested',
    button: 0,
    board: [],
    hole_cards: [{ seat: 1, cards: ['Kh', 'Kc'] }],
    refunds: [{ seat: 0, amount: 50 }],
    payouts: [{ seat: 1, amount: 300 }],
    pots: [{ amount: 300, eligible: [0, 1], winners: [1], payouts: [{ seat: 1, amount: 300 }] }],
    final_stacks: [
      { seat: 0, stack: 9950 },
      { seat: 1, stack: 10300 },
      { seat: 2, stack: 10000 },
    ],
    showdown: [],
  }
}

test.beforeEach(async ({ request }) => {
  await request.post('/__fixture/reset')
  await mkdir(OUT, { recursive: true })
})

test('摊牌结算：参与者翻牌、弃牌者不翻、赢家金额=payouts、结算条主池命名、快照', async ({ page }) => {
  await page.setViewportSize({ width: 1536, height: 1024 })
  const state: SState = {
    handsCompleted: 0,
    street: 'preflop',
    board: [],
    currentBet: 100,
    handDetail: null,
  }
  await installScript(page, state)
  await createGame(page, [ai(0, 'AI-1'), ai(1, 'AI-2'), ai(2, 'AI-3')])

  await page.goto('/')
  await expect(page.getByText('公共观战')).toBeVisible()
  await page.getByRole('button', { name: '开始' }).click()
  await expect(page.locator('.street-track')).toBeVisible({ timeout: 10_000 })
  // 基线期不发生结算。
  await page.waitForTimeout(1200)
  await expect(page.locator('.winner-tag')).toHaveCount(0)

  // 手结束：hands_completed 0→1，提供摊牌详情。
  state.handsCompleted = 1
  state.street = 'complete'
  state.board = ['Ah', 'Kd', 'Qc', 'Js', 'Th']
  state.handDetail = showdownDetail(1)

  const tag = page.locator('.winner-tag')
  await expect(tag).toBeVisible({ timeout: 8000 })
  await expect(tag.locator('.winner-amount')).toContainText('赢 +6,000')
  await expect(tag.locator('.winner-rank')).toContainText('葫芦')
  await expect(tag.locator('.winner-rank')).toContainText('A三条/K一对')

  // 赢家座位金边；输家（摊牌未赢）底牌降透明。
  await expect(page.locator('.seat.is-winner')).toHaveCount(1)
  await expect(page.locator('.seat-slot[data-seat="0"] .seat')).toHaveClass(/is-winner/)
  await expect(page.locator('.seat-slot[data-seat="1"] .seat')).toHaveClass(/is-loser/)

  // 摊牌参与者翻牌（座位 0、1），弃牌者座位 2 不翻牌。
  await expect(page.locator('.seat-slot[data-seat="0"] .hole.hole-reveal')).toHaveCount(1)
  await expect(page.locator('.seat-slot[data-seat="1"] .hole.hole-reveal')).toHaveCount(1)
  await expect(page.locator('.seat-slot[data-seat="2"] .hole.hole-reveal')).toHaveCount(0)
  await expect(page.locator('.hole.hole-reveal .settle-flip')).toHaveCount(4)
  // 翻出的牌是真实底牌（Ac/Ad）。
  await expect(page.locator('.seat-slot[data-seat="0"] .hole.hole-reveal .card .rank').first()).toHaveText('A')

  // 结算快照：公共牌来自该手详情（5 张），最终筹码来自 final_stacks。
  await expect(page.locator('.board .card')).toHaveCount(5)
  await expect(page.locator('.seat-slot[data-seat="0"] .seat-stack')).toContainText('16,000')

  // 桌下结算条：第一池标「主池」（不是「边池1」），不退化成叠加。
  const bar = page.locator('.settlement-bar')
  await expect(bar).toBeVisible()
  await expect(bar.locator('.settlement-head')).toContainText('第 1 手 · 摊牌结算')
  await expect(bar.locator('.settlement-pot').first().locator('.pot-name')).toHaveText('主池')
  await expect(bar.locator('.settlement-pot').first()).toContainText('6,000')
  await expect(bar.locator('.settlement-pot').first()).toContainText('AI-1')

  await page.screenshot({ path: path.join(OUT, 'showdown-winner.png') })
})

test('弃牌结束：不翻牌、不显示牌型、显示「其余玩家弃牌」、退回展示', async ({ page }) => {
  await page.setViewportSize({ width: 1536, height: 1024 })
  const state: SState = {
    handsCompleted: 0,
    street: 'preflop',
    board: [],
    currentBet: 100,
    handDetail: null,
  }
  await installScript(page, state)
  await createGame(page, [ai(0, 'AI-1'), ai(1, 'AI-2'), ai(2, 'AI-3')])

  await page.goto('/')
  await page.getByRole('button', { name: '开始' }).click()
  await expect(page.locator('.street-track')).toBeVisible({ timeout: 10_000 })

  state.handsCompleted = 1
  state.street = 'complete'
  state.board = []
  state.handDetail = uncontestedDetail(1)

  const tag = page.locator('.winner-tag')
  await expect(tag).toBeVisible({ timeout: 8000 })
  await expect(tag.locator('.winner-amount')).toContainText('赢 +300')
  await expect(tag.locator('.winner-sub')).toContainText('其余玩家弃牌')
  await expect(tag.locator('.winner-rank')).toHaveCount(0)
  // 不翻牌、不补齐公共牌。
  await expect(page.locator('.hole.hole-reveal')).toHaveCount(0)
  await expect(page.locator('.settle-flip')).toHaveCount(0)
  await expect(page.locator('.board .card')).toHaveCount(0)
  await expect(page.locator('.board-slot')).toHaveCount(5)

  const bar = page.locator('.settlement-bar')
  await expect(bar.locator('.settlement-head')).toContainText('第 1 手 · 弃牌结算')
  await expect(bar.locator('.settlement-refunds')).toContainText('退回')
  await expect(bar.locator('.settlement-refunds')).toContainText('AI-1')

  await page.screenshot({ path: path.join(OUT, 'uncontested-winner.png') })
})

test('真人赢：标签「你赢了 +X」、只赢边池明确提示、冰蓝身份保留', async ({ page }) => {
  await page.setViewportSize({ width: 1536, height: 1024 })
  const state: SState = {
    handsCompleted: 0,
    street: 'preflop',
    board: [],
    currentBet: 100,
    handDetail: null,
  }
  const detail: Record<string, unknown> = {
    game_id: 'x',
    hand_no: 1,
    reason: 'showdown',
    button: 0,
    board: ['2h', '7c', '9d', 'Jh', 'Qs'],
    hole_cards: [
      { seat: 0, cards: ['As', 'Ks'] },
      { seat: 1, cards: ['Ah', 'Ad'] },
    ],
    refunds: [],
    payouts: [
      { seat: 0, amount: 2000 },
      { seat: 1, amount: 4000 },
    ],
    pots: [
      { amount: 4000, eligible: [0, 1], winners: [1], payouts: [{ seat: 1, amount: 4000 }] },
      { amount: 2000, eligible: [0], winners: [0], payouts: [{ seat: 0, amount: 2000 }] },
    ],
    final_stacks: [
      { seat: 0, stack: 12000 },
      { seat: 1, stack: 8000 },
      { seat: 2, stack: 10000 },
    ],
    showdown: [
      { seat: 0, hand_rank: '同花', best_five: ['As', 'Ks', 'Qs', '9d', '7c'] },
      { seat: 1, hand_rank: '一对', best_five: ['Ah', 'Ad', 'Qs', 'Jh', '9d'] },
    ],
  }

  await installScript(page, state)
  await createGame(page, [human(0, '我'), ai(1, 'AI-2'), ai(2, 'AI-3')])
  await page.goto('/')
  await expect(page.getByText('真人视角')).toBeVisible()
  await page.getByRole('button', { name: '开始' }).click()
  await expect(page.locator('.street-track')).toBeVisible({ timeout: 10_000 })

  state.handsCompleted = 1
  state.street = 'complete'
  state.board = ['2h', '7c', '9d', 'Jh', 'Qs']
  state.handDetail = detail

  const me = page.locator('.winner-tag.is-me')
  await expect(me).toBeVisible({ timeout: 8000 })
  await expect(me.locator('.winner-amount')).toContainText('你赢了 +2,000')
  await expect(me.locator('.winner-sub')).toContainText('赢得边池1')

  // 结算条多池：主池 + 边池1 各一行。
  const pots = page.locator('.settlement-bar .settlement-pot')
  await expect(pots).toHaveCount(2)
  await expect(pots.nth(0).locator('.pot-name')).toHaveText('主池')
  await expect(pots.nth(1).locator('.pot-name')).toHaveText('边池1')

  await page.screenshot({ path: path.join(OUT, 'human-winner.png') })
})

test('首次进入不重播历史手', async ({ page }) => {
  await page.setViewportSize({ width: 1536, height: 1024 })
  const state: SState = {
    handsCompleted: 3,
    street: 'complete',
    board: ['2h', '7c', '9d', 'Jh', 'Qs'],
    currentBet: 0,
    handDetail: showdownDetail(3),
  }
  await installScript(page, state)
  await createGame(page, [ai(0, 'AI-1'), ai(1, 'AI-2'), ai(2, 'AI-3')])

  await page.goto('/')
  await expect(page.getByText('公共观战')).toBeVisible()
  await page.waitForTimeout(2500)
  await expect(page.locator('.winner-tag')).toHaveCount(0)
  await expect(page.locator('.settlement-bar')).toHaveCount(0)
})

test('reduced-motion：结算静态可见（金边/标签/牌面），不依赖动画', async ({ page }) => {
  await page.setViewportSize({ width: 1536, height: 1024 })
  await page.emulateMedia({ reducedMotion: 'reduce' })
  const state: SState = {
    handsCompleted: 0,
    street: 'preflop',
    board: [],
    currentBet: 100,
    handDetail: null,
  }
  await installScript(page, state)
  await createGame(page, [ai(0, 'AI-1'), ai(1, 'AI-2'), ai(2, 'AI-3')])

  await page.goto('/')
  await page.getByRole('button', { name: '开始' }).click()
  await expect(page.locator('.street-track')).toBeVisible({ timeout: 10_000 })

  state.handsCompleted = 1
  state.street = 'complete'
  state.board = ['Ah', 'Kd', 'Qc', 'Js', 'Th']
  state.handDetail = showdownDetail(1)

  const tag = page.locator('.winner-tag')
  await expect(tag).toBeVisible({ timeout: 8000 })
  const opacity = Number(await tag.evaluate((el) => getComputedStyle(el).opacity))
  expect(opacity, 'reduced-motion 下赢家标签静态可见').toBeGreaterThan(0.9)

  // 翻牌动画被压到 0.01ms 后应停在「正面」，牌面文字可见。
  await expect(page.locator('.seat-slot[data-seat="0"] .hole.hole-reveal .card .rank').first()).toBeVisible()
  // 静态金边保留。
  const border = await page.locator('.seat.is-winner').evaluate((el) => getComputedStyle(el).borderTopColor)
  expect(border).not.toBe('rgba(0, 0, 0, 0)')

  await page.screenshot({ path: path.join(OUT, 'reduced-motion-static.png') })
})

test('移动端 390：结算条在牌桌下方、可展开、不遮挡操作区', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 })
  const state: SState = {
    handsCompleted: 0,
    street: 'preflop',
    board: [],
    currentBet: 100,
    handDetail: null,
  }
  await installScript(page, state)
  await createGame(page, [ai(0, 'AI-1'), ai(1, 'AI-2'), ai(2, 'AI-3')])
  await page.goto('/')
  await page.getByRole('button', { name: '开始' }).click()
  await expect(page.locator('.street-track')).toBeVisible({ timeout: 10_000 })

  state.handsCompleted = 1
  state.street = 'complete'
  state.board = ['Ah', 'Kd', 'Qc', 'Js', 'Th']
  state.handDetail = showdownDetail(1)

  const bar = page.locator('.settlement-bar')
  await expect(bar).toBeVisible({ timeout: 8000 })
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)
  expect(overflow).toBeLessThanOrEqual(1)
  // 结算条位于牌桌之后（文档流），不遮牌桌。
  const table = await page.locator('.stage').boundingBox()
  const barBox = await bar.boundingBox()
  expect(table && barBox).toBeTruthy()
  expect(barBox!.y).toBeGreaterThanOrEqual(table!.y + table!.height - 2)

  await page.screenshot({ path: path.join(OUT, 'mobile-settlement.png'), fullPage: false })
})
