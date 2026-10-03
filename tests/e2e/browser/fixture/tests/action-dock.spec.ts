import { expect, test } from '@playwright/test'
import { mkdir } from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

// 底部操作区精致化验收（fixture，非真实 Go 链路）：
//  1) 桌面压缩成三行结构（状态行 / 操作行 / 快捷注行），总高显著下降；
//  2) 删除重复信息（「真人 · 座位」行、与占位重复的「范围」提示）；
//  3) 按钮 ≥40px 可点；快捷注仍为 6 个；
//  4) 移动端各行堆叠、不溢出、触控目标不缩小。
// 截图证据落盘到 SCREEN_DIR（默认 docs/action-dock）。

const HERE = path.dirname(fileURLToPath(import.meta.url))
const ROOT = path.resolve(HERE, '../../../../..')
const OUT = process.env.SCREEN_DIR ? path.resolve(process.env.SCREEN_DIR) : path.join(ROOT, 'docs/action-dock')

async function createHumanGame(page: import('@playwright/test').Page, aiCount = 1) {
  const seats: Array<Record<string, unknown>> = [{ seat: 0, name: '我', stack: 10000, kind: 'human' }]
  for (let i = 1; i <= aiCount; i++) {
    seats.push({ seat: i, name: `AI-${i}`, stack: 10000, kind: 'ai', model_ref: 'mock' })
  }
  const res = await page.request.post('/api/v1/games', {
    data: { seats, button: 0, small_blind: 50, big_blind: 100, max_hands: 100, mode: 'mock', max_live_calls: 0 },
  })
  expect(res.ok()).toBeTruthy()
  await page.goto('/')
  await expect(page.getByText('真人视角')).toBeVisible()
}

async function waitMyTurn(page: import('@playwright/test').Page) {
  await page.getByRole('button', { name: '开始' }).click()
  await expect(page.locator('.hand-dock[data-turn="me"]')).toBeVisible({ timeout: 15_000 })
  await expect(page.locator('.action-panel.is-turn')).toBeVisible()
}

test.beforeEach(async ({ request }) => {
  await request.post('/__fixture/reset')
  await mkdir(OUT, { recursive: true })
})

test('桌面 · 三行紧凑结构、总高下降、无重复信息、按钮≥40px', async ({ page }) => {
  await page.setViewportSize({ width: 1536, height: 1024 })
  await createHumanGame(page)
  await waitMyTurn(page)

  const dock = page.locator('.hand-dock')
  const dockBox = await dock.boundingBox()
  expect(dockBox).not.toBeNull()
  console.log(`[dock] desktop height=${Math.round(dockBox!.height)}`)
  // 目标 96-140px；留少量余量断言 ≤150，并且必须显著低于旧版 ~260px。
  expect(dockBox!.height).toBeLessThanOrEqual(150)
  expect(dockBox!.height).toBeGreaterThanOrEqual(96)

  // 三行结构存在，且纵向顺序为 状态行 < 操作行 < 快捷注行。
  const status = await page.locator('.dock-turn').boundingBox()
  const main = await page.locator('.action-main').boundingBox()
  const quick = await page.locator('.quick-bets').boundingBox()
  expect(status && main && quick).toBeTruthy()
  expect(status!.y).toBeLessThan(main!.y)
  expect(main!.y).toBeLessThan(quick!.y)

  // 进度条并入状态行（不再是独立一行）。
  await expect(page.locator('.dock-turn .dock-timer')).toHaveCount(1)

  // 删除重复信息。
  await expect(page.locator('.action-who')).toHaveCount(0)
  await expect(page.locator('.raise-hint')).toHaveCount(0)
  await expect(page.getByText('真人 · 座位')).toHaveCount(0)
  // 常驻范围短提示可见（不依赖 placeholder）。
  await expect(page.locator('.raise-range')).toBeVisible()
  // 加注输入仍可通过可访问名定位并填写（保持 human E2E 兼容）。
  await page.getByLabel('加注到（本街累计）').fill('300')
  await expect(page.locator('#raise-to')).toHaveValue('300')

  // 直接动作按钮与加注按钮高度 ≥40px。
  for (const sel of ['.action-buttons .btn', '.raise-control .btn']) {
    const btns = page.locator(sel)
    const n = await btns.count()
    expect(n).toBeGreaterThan(0)
    for (let i = 0; i < n; i++) {
      const b = await btns.nth(i).boundingBox()
      expect(b, `${sel}[${i}] 不可见`).not.toBeNull()
      expect(b!.height).toBeGreaterThanOrEqual(40)
    }
  }
  // 快捷注仍为 6 个，且高度 ≥40px（不可点性由既有 quick-bet 测试覆盖）。
  await expect(page.locator('.quick-bet')).toHaveCount(6)
  const qb = await page.locator('.quick-bet').first().boundingBox()
  expect(qb!.height).toBeGreaterThanOrEqual(40)

  await page.screenshot({ path: path.join(OUT, 'desktop-my-turn.png'), fullPage: false })
})

test('桌面 · 非我行动（思考中）时操作区仅剩状态行', async ({ page }) => {
  await page.setViewportSize({ width: 1536, height: 1024 })
  await createHumanGame(page)
  await waitMyTurn(page)
  await page.getByRole('button', { name: '弃牌' }).click()
  await expect(page.locator('.hand-dock[data-turn="ai"]')).toBeVisible({ timeout: 15_000 })
  await expect(page.locator('.dock-state')).toHaveText('思考中…')

  const dockBox = await page.locator('.hand-dock').boundingBox()
  console.log(`[dock] thinking height=${Math.round(dockBox!.height)}`)
  // 非我行动时操作区只剩状态行（无按钮/快捷注），显著矮于三行态。
  expect(dockBox!.height).toBeLessThanOrEqual(64)
  await page.screenshot({ path: path.join(OUT, 'desktop-thinking.png'), fullPage: false })
})

test('移动端 390 · 各行堆叠、触控目标≥40px、不溢出', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 })
  await createHumanGame(page)
  await waitMyTurn(page)

  const dockBox = await page.locator('.hand-dock').boundingBox()
  console.log(`[dock] mobile height=${Math.round(dockBox!.height)}`)
  expect(dockBox!.height).toBeLessThanOrEqual(215)

  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)
  expect(overflow).toBeLessThanOrEqual(1)

  for (const sel of ['.action-buttons .btn', '.raise-control .btn', '.quick-bet']) {
    const btns = page.locator(sel)
    const n = await btns.count()
    expect(n).toBeGreaterThan(0)
    for (let i = 0; i < n; i++) {
      const b = await btns.nth(i).boundingBox()
      expect(b, `${sel}[${i}] 不可见`).not.toBeNull()
      expect(b!.height).toBeGreaterThanOrEqual(40)
      expect(b!.x, `${sel}[${i}] 左溢出`).toBeGreaterThanOrEqual(0)
      expect(b!.x + b!.width, `${sel}[${i}] 右溢出`).toBeLessThanOrEqual(390)
    }
  }

  await page.screenshot({ path: path.join(OUT, 'mobile-my-turn.png'), fullPage: false })
})
