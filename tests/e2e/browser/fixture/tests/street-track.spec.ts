import { expect, test } from '@playwright/test'
import { mkdir } from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

// 街进度指示器（5 节点轨道 + 状态行）验收（fixture，非真实 Go 链路）：
//  1) 五节点按当前街标注 已到达/当前/未来；
//  2) 状态行：下注进行中（有/无当前注）、切街临时提示 1.5s、暂停、跳街不伪造；
//  3) 本手结束：第五节点高亮 + 「本手已结束」；无涉牌手时默认「结束」不写「摊牌」；
//  4) 停止/失败不画成正常结束。
// 通过 page.route 脚本化 hand.street / current_bet / board（不改 fixture-server.mjs）。
// 明确：本 fixture 不能作为「真实 Go 联调」完成证据。

const HERE = path.dirname(fileURLToPath(import.meta.url))
const ROOT = path.resolve(HERE, '../../../../..')
const OUT = process.env.SCREEN_DIR ? path.resolve(process.env.SCREEN_DIR) : path.join(ROOT, 'docs/settlement')

function ai(seat: number, name: string) {
  return { seat, name, stack: 10000, kind: 'ai' as const, model_ref: 'mock' }
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
  street: string
  board: string[]
  currentBet: number
  status?: string
  handsCompleted?: number
}

async function installScript(page: import('@playwright/test').Page, state: SState) {
  await page.route(/\/api\/v1\/games\/[^/]+(\/player)?$/, async (route) => {
    if (route.request().method() !== 'GET') return route.continue()
    const res = await route.fetch()
    const json = await res.json()
    if (json && typeof json === 'object' && !Array.isArray(json)) {
      json.hands_completed = state.handsCompleted ?? json.hands_completed
      if (state.status) json.status = state.status
      if (json.hand) {
        json.hand.street = state.street
        json.hand.board = state.board
        json.hand.current_bet = state.currentBet
      }
    }
    await route.fulfill({ response: res, json })
  })
}

function node(page: import('@playwright/test').Page, key: string) {
  return page.locator(`.street-node[data-node="${key}"]`)
}

test.beforeEach(async ({ request }) => {
  await request.post('/__fixture/reset')
  await mkdir(OUT, { recursive: true })
})

test('五节点轨道：当前街最亮、过去小勾、未来空心、当前注文案', async ({ page }) => {
  await page.setViewportSize({ width: 1536, height: 1024 })
  const state: SState = { street: 'turn', board: ['Ah', 'Kd', 'Qc', '2s'], currentBet: 200 }
  await installScript(page, state)
  await createGame(page, [ai(0, 'AI-1'), ai(1, 'AI-2'), ai(2, 'AI-3')])

  await page.goto('/')
  await expect(page.getByText('公共观战')).toBeVisible()
  await page.getByRole('button', { name: '开始' }).click()

  const track = page.locator('.street-track')
  await expect(track).toBeVisible({ timeout: 10_000 })
  await expect(page.locator('.street-node')).toHaveCount(5)
  // 轮询生效后（start 的即时响应是 preflop），当前街为 turn。
  await expect(node(page, 'turn')).toHaveAttribute('data-state', 'current', { timeout: 5000 })
  await expect(node(page, 'preflop')).toHaveAttribute('data-state', 'done')
  await expect(node(page, 'flop')).toHaveAttribute('data-state', 'done')
  await expect(node(page, 'river')).toHaveAttribute('data-state', 'future')
  await expect(node(page, 'complete')).toHaveAttribute('data-state', 'future')

  // 当前节点字面量、已到达小勾。
  await expect(page.locator('.street-node[data-node="turn"] .node-name')).toHaveText('转牌')
  await expect(page.locator('.street-node[data-node="preflop"] .node-dot')).toHaveText('✓')

  const status = page.locator('.street-status')
  await expect(status).toContainText('下注进行中 · 本街当前注 200')

  // current_bet=0：不显示金额，显示「尚无人下注」。
  state.currentBet = 0
  await expect(status).toContainText('下注进行中 · 本街尚无人下注', { timeout: 5000 })

  await page.screenshot({ path: path.join(OUT, 'street-track-states.png') })
})

test('切街临时提示 1.5s（flop→turn），随后恢复常态', async ({ page }) => {
  await page.setViewportSize({ width: 1536, height: 1024 })
  const state: SState = { street: 'flop', board: ['Ah', 'Kd', 'Qc'], currentBet: 100 }
  await installScript(page, state)
  await createGame(page, [ai(0, 'AI-1'), ai(1, 'AI-2'), ai(2, 'AI-3')])

  await page.goto('/')
  await page.getByRole('button', { name: '开始' }).click()
  await expect(node(page, 'flop')).toHaveAttribute('data-state', 'current', { timeout: 5000 })

  state.street = 'turn'
  state.board = ['Ah', 'Kd', 'Qc', '2s']
  const status = page.locator('.street-status')
  await expect(status).toContainText('翻牌下注已结束 · 转牌下注进行中', { timeout: 5000 })
  // 1.5s 后恢复常态。
  await expect(status).toContainText('下注进行中 · 本街当前注 100', { timeout: 4000 })
  await expect(status).not.toContainText('下注已结束')
})

test('跳街（preflop→river）不伪造中间切街提示', async ({ page }) => {
  await page.setViewportSize({ width: 1536, height: 1024 })
  const state: SState = { street: 'preflop', board: [], currentBet: 100 }
  await installScript(page, state)
  await createGame(page, [ai(0, 'AI-1'), ai(1, 'AI-2'), ai(2, 'AI-3')])

  await page.goto('/')
  await page.getByRole('button', { name: '开始' }).click()
  await expect(node(page, 'preflop')).toHaveAttribute('data-state', 'current', { timeout: 5000 })

  state.street = 'river'
  state.board = ['Ah', 'Kd', 'Qc', '2s', '9h']
  const status = page.locator('.street-status')
  await expect(node(page, 'river')).toHaveAttribute('data-state', 'current', { timeout: 5000 })
  // 直接显示最新街常态，绝不弹出/短暂显示「下注已结束」文案。
  await page.waitForTimeout(1800)
  await expect(status).toContainText('下注进行中 · 本街当前注 100')
  await expect(status).not.toContainText('下注已结束')
})

test('暂停：状态行显示「牌局已暂停 · 本街尚未结束」', async ({ page }) => {
  await page.setViewportSize({ width: 1536, height: 1024 })
  const state: SState = { street: 'flop', board: ['Ah', 'Kd', 'Qc'], currentBet: 100 }
  await installScript(page, state)
  await createGame(page, [ai(0, 'AI-1'), ai(1, 'AI-2'), ai(2, 'AI-3')])

  await page.goto('/')
  await page.getByRole('button', { name: '开始' }).click()
  await expect(page.locator('.street-status')).toBeVisible()
  await page.getByRole('button', { name: '暂停' }).click()
  await expect(page.locator('.street-status')).toContainText('牌局已暂停 · 本街尚未结束', {
    timeout: 6000,
  })
})

test('停止：不把中止画成正常结束', async ({ page }) => {
  await page.setViewportSize({ width: 1536, height: 1024 })
  const state: SState = { street: 'flop', board: ['Ah', 'Kd', 'Qc'], currentBet: 100 }
  await installScript(page, state)
  await createGame(page, [ai(0, 'AI-1'), ai(1, 'AI-2'), ai(2, 'AI-3')])

  await page.goto('/')
  await page.getByRole('button', { name: '开始' }).click()
  await expect(page.locator('.street-status')).toBeVisible()
  await page.getByRole('button', { name: '停止', exact: true }).click()

  await expect(page.locator('.street-status')).toContainText('已停止', { timeout: 6000 })
  await expect(page.locator('.street-status')).not.toContainText('本手已结束')
  await expect(page.locator('.street-node.is-ended')).toHaveCount(0)
})
