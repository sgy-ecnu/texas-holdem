import { expect, test } from '@playwright/test'
import { mkdir } from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

// 牌桌布局验收（fixture，非真实 Go 链路）：
//  1) 三层分离：中央公共牌/底池/街名不被任何座位或桌沿徽章压住；
//  2) 座位卡固定两行（名字+标记 / 剩余筹码），高度不随状态变化；
//  3) 有真人时真人固定在正下方居中；
//  4) 人数模板 2-9：座位互不重叠、不侵入中央；
//  5) 牌桌比上一版更大；无横向溢出；移动端可点、不溢出。
// 截图证据落盘到 SCREEN_DIR（默认 docs/table-layout）。

const HERE = path.dirname(fileURLToPath(import.meta.url))
const ROOT = path.resolve(HERE, '../../../../..')
const OUT = process.env.SCREEN_DIR ? path.resolve(process.env.SCREEN_DIR) : path.join(ROOT, 'docs/table-layout')

interface Rect {
  x: number
  y: number
  width: number
  height: number
}

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

function overlaps(a: Rect, b: Rect, tol = 0): boolean {
  return !(
    a.x + a.width <= b.x + tol ||
    b.x + b.width <= a.x + tol ||
    a.y + a.height <= b.y + tol ||
    b.y + b.height <= a.y + tol
  )
}

async function seatBoxes(page: import('@playwright/test').Page): Promise<Rect[]> {
  const seats = page.locator('.seat')
  const n = await seats.count()
  const boxes: Rect[] = []
  for (let i = 0; i < n; i++) {
    const b = await seats.nth(i).boundingBox()
    if (b) boxes.push(b)
  }
  return boxes
}

// 返回与目标元素相交的座位卡数量（0 表示中央区未被覆盖）。
async function seatsOverlapping(page: import('@playwright/test').Page, target: string): Promise<number> {
  const tb = await page.locator(target).boundingBox()
  if (!tb) return 0
  return (await seatBoxes(page)).filter((b) => overlaps(b, tb)).length
}

async function assertClean(page: import('@playwright/test').Page) {
  for (const sel of ['.board', '.pot', '.street-label']) {
    expect(await seatsOverlapping(page, sel)).toBe(0)
  }
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)
  expect(overflow).toBeLessThanOrEqual(1)
}

async function assertSeatsDisjoint(page: import('@playwright/test').Page) {
  const boxes = await seatBoxes(page)
  for (let i = 0; i < boxes.length; i++) {
    for (let j = i + 1; j < boxes.length; j++) {
      expect(overlaps(boxes[i], boxes[j], -2), `seat ${i} 与 seat ${j} 重叠`).toBeFalsy()
    }
  }
}

test.beforeEach(async ({ request }) => {
  await request.post('/__fixture/reset')
  await mkdir(OUT, { recursive: true })
})

test('人数模板 2-9（真人局）：不重叠、不侵入中央、真人底部居中、两行固定', async ({ page }) => {
  await page.setViewportSize({ width: 1536, height: 1024 })
  for (let n = 2; n <= 9; n++) {
    await page.request.post('/__fixture/reset')
    const seats: Array<Record<string, unknown>> = []
    for (let s = 0; s < n; s++) seats.push(ai(s, `AI-${s + 1}`))
    // 真人放在最后一个座位号，验证「与座位号无关，真人恒在底部居中」。
    const humanNo = n - 1
    seats[humanNo] = { seat: humanNo, name: '我', stack: 10000, kind: 'human' }
    await createGame(page, seats)
    await page.goto('/')
    await expect(page.getByText('真人视角')).toBeVisible()
    await page.getByRole('button', { name: '开始' }).click()
    await expect(page.locator('.hand-dock[data-turn="me"]')).toBeVisible({ timeout: 15_000 })

    await expect(page.locator('.seat')).toHaveCount(n)
    // 两行结构：每个座位都有 row1 + row2。
    await expect(page.locator('.seat .seat-row1')).toHaveCount(n)
    await expect(page.locator('.seat .seat-row2')).toHaveCount(n)

    // 固定高度：所有座位卡等高，且落在 [50,70] 像素。
    const boxes = await seatBoxes(page)
    const h0 = boxes[0].height
    expect(h0).toBeGreaterThanOrEqual(50)
    expect(h0).toBeLessThanOrEqual(70)
    for (const b of boxes) expect(Math.abs(b.height - h0)).toBeLessThanOrEqual(2)

    await assertClean(page)
    await assertSeatsDisjoint(page)

    // 真人底部居中。
    const stage = await page.locator('.stage').boundingBox()
    const human = await page.locator('.seat.is-human').boundingBox()
    expect(stage && human).toBeTruthy()
    const stageCx = stage!.x + stage!.width / 2
    const humanCx = human!.x + human!.width / 2
    expect(Math.abs(humanCx - stageCx), `n=${n} 真人未居中`).toBeLessThan(12)
    expect(human!.y).toBeGreaterThan(stage!.y + stage!.height * 0.6)

    await page.screenshot({ path: path.join(OUT, `count-${n}.png`), fullPage: false })
  }
})

test('桌面 · 真人 9 座：底部居中、中央清爽、座位卡两行、牌桌更大', async ({ page }) => {
  await page.setViewportSize({ width: 1536, height: 1024 })
  const seats = Array.from({ length: 9 }, (_, i) => ai(i, `AI-${i + 1}`))
  seats[4] = { seat: 4, name: '我', stack: 10000, kind: 'human' }
  await createGame(page, seats)
  await page.goto('/')
  await expect(page.getByText('真人视角')).toBeVisible()
  await page.getByRole('button', { name: '开始' }).click()
  await expect(page.locator('.hand-dock[data-turn="me"]')).toBeVisible({ timeout: 15_000 })

  await assertClean(page)
  // 本人底牌大于 AI 牌背（面朝上的两张明显更大）。
  const ownCard = await page.locator('.seat.is-human .hole-open .card').first().boundingBox()
  const back = await page.locator('.seat .hole .mini-card').first().boundingBox()
  expect(ownCard && back).toBeTruthy()
  expect(ownCard!.width).toBeGreaterThan(back!.width)

  const stage = await page.locator('.stage').boundingBox()
  const felt = await page.locator('.felt').boundingBox()
  console.log(`[layout] stage=${Math.round(stage!.width)}x${Math.round(stage!.height)} felt=${Math.round(felt!.width)}x${Math.round(felt!.height)}`)
  // 上一轮实测 ~1048；本轮目标 10-15% 放大。
  expect(felt!.width).toBeGreaterThan(1100)

  await page.screenshot({ path: path.join(OUT, 'desktop-human-9.png'), fullPage: false })
})

test('桌面 · 全 AI 9 座观战：环绕不重叠、中央清爽', async ({ page }) => {
  await page.setViewportSize({ width: 1536, height: 1024 })
  const seats = Array.from({ length: 9 }, (_, i) => ai(i, `AI-${i + 1}`))
  await createGame(page, seats)
  await page.goto('/')
  await expect(page.getByText('公共观战')).toBeVisible()
  await page.getByRole('button', { name: '开始' }).click()
  await expect(page.locator('.hand-dock')).toBeVisible()

  await assertClean(page)
  await assertSeatsDisjoint(page)
  await page.screenshot({ path: path.join(OUT, 'desktop-allai-9.png'), fullPage: false })
})

test('桌沿徽章：本街下注（本轮 + 累计）落在护栏内侧，不压中央', async ({ page }) => {
  await page.setViewportSize({ width: 1536, height: 1024 })
  // 用 route 给公开状态注入本街下注，验证 .rim-mark 的渲染与位置（不改 fixture 数据）。
  await page.route('**/api/v1/games/*', async (route) => {
    const res = await route.fetch()
    const json = await res.json()
    if (json?.hand?.players) {
      json.hand.players = json.hand.players.map((p: { seat: number }) => ({
        ...p,
        street_bet: p.seat % 2 === 0 ? 300 : 0,
        committed: p.seat % 2 === 0 ? 550 : 0,
      }))
    }
    await route.fulfill({ response: res, json })
  })
  const seats = Array.from({ length: 9 }, (_, i) => ai(i, `AI-${i + 1}`))
  await createGame(page, seats)
  await page.goto('/')
  await expect(page.getByText('公共观战')).toBeVisible()
  await page.getByRole('button', { name: '开始' }).click()

  await expect(page.locator('.rim-mark').first()).toBeVisible({ timeout: 15_000 })
  // 桌沿徽章也不得压住中央元素。
  const tb = await page.locator('.board').boundingBox()
  const marks = page.locator('.rim-mark')
  const mc = await marks.count()
  for (let i = 0; i < mc; i++) {
    const mb = await marks.nth(i).boundingBox()
    if (tb && mb) expect(overlaps(mb, tb), `rim-mark ${i} 压住公共牌`).toBeFalsy()
  }
  await page.screenshot({ path: path.join(OUT, 'desktop-rim-bets.png'), fullPage: false })
})

test('移动端 390：座位可点、不溢出、操作条在视口内', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 })
  const seats = [ai(0, 'AI-1'), ai(1, 'AI-2'), { seat: 2, name: '我', stack: 10000, kind: 'human' }]
  await createGame(page, seats)
  await page.goto('/')
  await expect(page.getByText('真人视角')).toBeVisible()
  await page.getByRole('button', { name: '开始' }).click()
  await expect(page.locator('.hand-dock[data-turn="me"]')).toBeVisible({ timeout: 15_000 })

  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)
  expect(overflow).toBeLessThanOrEqual(1)
  // 移动端不渲染桌沿徽章层。
  await expect(page.locator('.rim-marks')).toBeHidden()

  const fold = page.locator('.action-panel').getByRole('button', { name: '弃牌' })
  await expect(fold).toBeVisible()
  const box = await fold.boundingBox()
  expect(box).not.toBeNull()
  expect(box!.x).toBeGreaterThanOrEqual(0)
  expect(box!.x + box!.width).toBeLessThanOrEqual(390)

  await page.screenshot({ path: path.join(OUT, 'mobile-human-390.png'), fullPage: false })
})
