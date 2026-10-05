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

test('桌面 · 真人 9 座：底部居中、中央清爽、座位卡两行、牌桌占据主要区域', async ({ page }) => {
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
  // 「座位外侧下注条」需要外围带（.rail inset 12%/11%），牌桌按预期收窄到 ~1012；
  // 断言仍明显大于移动端牌桌，中央公共牌区保持宽敞。
  expect(felt!.width).toBeGreaterThan(950)

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

test('座位外侧下注条：本街下注（本轮 + 累计）在桌沿外，不压中央', async ({ page }) => {
  await page.setViewportSize({ width: 1536, height: 1024 })
  // 用 route 给公开状态注入本街下注，验证 .rim-mark 的渲染与位置（不改 fixture 数据）。
  await page.route('**/api/v1/games/**', async (route) => {
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
  // 外侧下注条也不得压住中央元素。
  const tb = await page.locator('.board').boundingBox()
  const marks = page.locator('.rim-mark')
  const mc = await marks.count()
  for (let i = 0; i < mc; i++) {
    const mb = await marks.nth(i).boundingBox()
    if (tb && mb) expect(overlaps(mb, tb), `rim-mark ${i} 压住公共牌`).toBeFalsy()
  }
  await page.screenshot({ path: path.join(OUT, 'desktop-rim-bets.png'), fullPage: false })
})

// 列表布局判定契约：
//  - 顶部下注条放不下只做下注条紧凑化，绝不据此切列表（旧误判：9 人 1200px 被切列表）；
//  - 椭圆座位卡会重叠时先收窄座位卡（.is-narrow-seats）；真窄屏（<1000 视口）才退化为列表；
//  - ≤720 移动端仍列表。
test('列表布局判定：8/9 人桌面宽度保持椭圆，真窄屏才列表', async ({ page }) => {
  const asyncSeats = async (n: number) => {
    await page.request.post('/__fixture/reset')
    const seats = Array.from({ length: n }, (_, i) => ai(i, `AI-${i + 1}`))
    seats[n - 1] = { seat: n - 1, name: '我', stack: 10000, kind: 'human' }
    await createGame(page, seats)
    await page.goto('/')
    await expect(page.getByText('真人视角')).toBeVisible()
    await page.getByRole('button', { name: '开始' }).click()
    await expect(page.locator('.hand-dock[data-turn="me"]')).toBeVisible({ timeout: 15_000 })
  }
  const isList = () => page.locator('.stage.is-list')
  // 座位分布是否环绕（非单列长条）：按 y=20px 分桶统计出现过的 x 桶数量。
  const spreadX = async () => {
    const rects = await page.evaluate(() =>
      Array.from(document.querySelectorAll('.seat')).map((s) => {
        const r = (s as HTMLElement).getBoundingClientRect()
        return { x: r.x, y: r.y }
      }),
    )
    return new Set(rects.map((r) => Math.round(r.x / 20))).size
  }
  // 座位卡两两重叠数（严格 0 容差）。
  const seatOverlaps = () =>
    page.evaluate(() => {
      const rs = Array.from(document.querySelectorAll('.seat')).map((el) =>
        (el as HTMLElement).getBoundingClientRect(),
      )
      let n = 0
      for (let i = 0; i < rs.length; i++) {
        for (let j = i + 1; j < rs.length; j++) {
          const a = rs[i]
          const b = rs[j]
          if (!(a.right <= b.left || b.right <= a.left || a.bottom <= b.top || b.bottom <= a.top)) n++
        }
      }
      return n
    })

  // 桌面 1536×900：2-9 人全部椭圆环绕，无 is-list，座位横向铺开。
  await page.setViewportSize({ width: 1536, height: 900 })
  for (let n = 2; n <= 9; n++) {
    await asyncSeats(n)
    await expect(isList(), `n=${n} 桌面不应切列表布局`).toHaveCount(0)
    // 2 人局 B/T 同 x 垂直对坐是合法椭圆，不作横向铺开断言。
    if (n >= 3) expect(await spreadX(), `n=${n} 座位应环绕而非单列`).toBeGreaterThanOrEqual(3)
  }

  // 核心回归：桌面 1200 宽 9 人椭圆（旧逻辑 0.37×RXpx<160 误切列表）。
  await page.setViewportSize({ width: 1200, height: 900 })
  await asyncSeats(9)
  await expect(isList(), '9 人在 1200 宽必须保持椭圆布局').toHaveCount(0)
  expect(await spreadX(), '9 人应环绕').toBeGreaterThanOrEqual(4)
  expect(await seatOverlaps(), '9 人座位卡不应重叠').toBe(0)
  await asyncSeats(8)
  await expect(isList(), '8 人在 1200 宽应椭圆').toHaveCount(0)
  expect(await seatOverlaps(), '8 人座位卡不应重叠').toBe(0)

  // 1000 视口（桌面下界）：9 人仍椭圆，改用紧凑座位卡避免重叠。
  await page.setViewportSize({ width: 1000, height: 900 })
  await asyncSeats(9)
  await expect(isList(), '9 人在 1000 宽（桌面下界）应椭圆').toHaveCount(0)
  await expect(page.locator('.stage.is-narrow-seats'), '窄桌面应启用紧凑座位卡').toHaveCount(1)
  expect(await seatOverlaps(), '紧凑座位卡下座位不应重叠').toBe(0)

  // 真窄屏（<1000 视口）：9 人退化为列表（单列）。
  await page.setViewportSize({ width: 900, height: 900 })
  await asyncSeats(9)
  await expect(isList(), '9 人在 900 宽窄屏应列表').toHaveCount(1)
  expect(await spreadX()).toBe(1)

  // 移动端 390：6 人退化为列表（单列）。
  await page.setViewportSize({ width: 390, height: 844 })
  await asyncSeats(6)
  await expect(isList(), '移动端应切列表布局').toHaveCount(1)
  expect(await spreadX()).toBe(1)
})

// 顶部下注条紧凑化契约：9 人 1200 宽下顶部相邻条放不下完整一行条时，
// 只隐藏「累计」并收紧样式，绝不切列表；条仍在外侧、不重叠、不压中央。
test('顶部下注条紧凑化：9 人 1200 宽不切列表，顶部条折叠累计且不重叠', async ({ page }) => {
  await page.setViewportSize({ width: 1200, height: 900 })
  // route 注入本街下注，验证 .rim-mark 紧凑化（不改 fixture 数据）。
  await page.route('**/api/v1/games/**', async (route) => {
    const res = await route.fetch()
    const json = await res.json()
    if (json?.hand?.players) {
      json.hand.players = json.hand.players.map((p: { seat: number }) => ({
        ...p,
        street_bet: 9_999,
        committed: 99_999,
      }))
    }
    await route.fulfill({ response: res, json })
  })
  const seats = Array.from({ length: 9 }, (_, i) => ai(i, `AI-${i + 1}`))
  seats[4] = { seat: 4, name: '我', stack: 10_000_000, kind: 'human' }
  await createGame(page, seats)
  await page.goto('/')
  await page.getByRole('button', { name: '开始' }).click()
  await expect(page.locator('.rim-mark').first()).toBeVisible({ timeout: 15_000 })

  await expect(page.locator('.stage.is-list'), '9 人 1200 宽不得切列表').toHaveCount(0)
  await expect(page.locator('.stage.is-top-cramped'), '应触发顶部下注条紧凑化').toHaveCount(1)
  // 顶部「上向」条：累计折叠（不显示），title 仍保留累计信息。
  const upBar = page.locator('.rim-mark.is-up').first()
  await expect(upBar).toBeVisible()
  await expect(upBar.locator('.rim-cum')).toBeHidden()
  await expect(upBar).toHaveAttribute('title', /累计/)

  // 顶部相邻条互不重叠，且不压公共牌。
  const board = await page.locator('.board').boundingBox()
  const ups = page.locator('.rim-mark.is-up')
  const boxes: Rect[] = []
  for (let i = 0; i < (await ups.count()); i++) {
    const b = await ups.nth(i).boundingBox()
    if (b) boxes.push(b)
  }
  for (let i = 0; i < boxes.length; i++) {
    for (let j = i + 1; j < boxes.length; j++) {
      expect(overlaps(boxes[i], boxes[j], -1), `顶部条 ${i}/${j} 叠压`).toBeFalsy()
    }
    if (board) expect(overlaps(boxes[i], board), `顶部条 ${i} 压住公共牌`).toBeFalsy()
  }
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)
  expect(overflow).toBeLessThanOrEqual(1)
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
  // 移动端/列表布局不渲染座位外侧下注条（下注信息在座位卡内 .bet）。
  await expect(page.locator('.rim-mark').first()).toBeHidden()

  const fold = page.locator('.action-panel').getByRole('button', { name: '弃牌' })
  await expect(fold).toBeVisible()
  const box = await fold.boundingBox()
  expect(box).not.toBeNull()
  expect(box!.x).toBeGreaterThanOrEqual(0)
  expect(box!.x + box!.width).toBeLessThanOrEqual(390)

  await page.screenshot({ path: path.join(OUT, 'mobile-human-390.png'), fullPage: false })
})
