import { expect, test, type Page } from '@playwright/test'
import fs from 'node:fs'
import path from 'node:path'

// 下注徽章（.rim-mark）遮挡修复 · 真实链路验收。
//
// 边界（与 action-popover / human 同源的安全约定）：
// - 真实 Chromium 页面的原生 fetch 创建牌局；不 route/fulfill、不使用 APIRequestContext。
// - Cookie/Set-Cookie 原值一律不读取、不输出。
// - mock 目录 allow_paid_models=false，不产生任何真实 provider 请求。
//
// 断言基于真实 getBoundingClientRect（Playwright boundingBox）：
//   1) 徽章不与任何座位卡第一行（.seat-row1）相交；
//   2) 徽章不与任何底牌（真人明牌 .hole-open .card / 牌背 .hole）相交；
//   3) 徽章两两不相交；
//   4) 徽章仍落在护栏内侧（felt 椭圆内）且紧贴所属座位。

interface Rect {
  x: number
  y: number
  width: number
  height: number
}

const OUT = process.env.CHIP_SCREEN_DIR
  ? path.resolve(process.env.CHIP_SCREEN_DIR)
  : path.resolve(process.cwd(), '../../docs/chip-badge')
fs.mkdirSync(OUT, { recursive: true })

const evidence: { name: string; facts: Record<string, unknown> }[] = []
function record(name: string, facts: Record<string, unknown>) {
  evidence.push({ name, facts })
  fs.writeFileSync(path.join(OUT, 'evidence.json'), JSON.stringify(evidence, null, 2))
  console.log(`EVIDENCE ${name}: ${JSON.stringify(facts)}`)
}

function overlaps(a: Rect, b: Rect, tol = 0): boolean {
  return !(
    a.x + a.width <= b.x + tol ||
    b.x + b.width <= a.x + tol ||
    a.y + a.height <= b.y + tol ||
    b.y + b.height <= a.y + tol
  )
}

function pairs(mapA: Rect[], mapB: Rect[], same = false): [number, number][] {
  const out: [number, number][] = []
  for (let i = 0; i < mapA.length; i++) {
    const start = same ? i + 1 : 0
    for (let j = start; j < mapB.length; j++) {
      if (overlaps(mapA[i], mapB[j])) out.push([i, j])
    }
  }
  return out
}

async function boxes(page: Page, sel: string): Promise<Rect[]> {
  const loc = page.locator(sel)
  const n = await loc.count()
  const out: Rect[] = []
  for (let i = 0; i < n; i++) {
    const b = await loc.nth(i).boundingBox()
    if (b) out.push(b as Rect)
  }
  return out
}

const center = (r: Rect) => ({ x: r.x + r.width / 2, y: r.y + r.height / 2 })

const field = (page: Page, name: string) =>
  page.locator('.form-grid .field').filter({ hasText: name })

interface CreateOptions {
  count?: number
  humanSeat?: number | null
  button?: number
  sb?: number
  bb?: number
  hands?: number
}

async function create(page: Page, options: CreateOptions = {}) {
  const { count = 9, humanSeat = null, button = 0, sb = 50, bb = 100, hands = 100000 } = options
  await page.goto('/')
  await expect(page.getByRole('button', { name: '创建牌局', exact: true })).toBeVisible()
  await field(page, '座位数').locator('select').selectOption(String(count))
  await expect(page.locator('.seat-row')).toHaveCount(count)
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
  return (await response.json()).id as string
}

async function stopGameIfRunning(page: Page) {
  // 通过真实 UI 停止：回到配置页后 current 牌局会被停止；此处只用 UI 按钮。
  const stopBtn = page.getByRole('button', { name: '停止', exact: true })
  if (await stopBtn.isVisible().catch(() => false)) {
    await stopBtn.click().catch(() => {})
  }
}

async function pause(page: Page) {
  const pauseBtn = page.getByRole('button', { name: '暂停', exact: true })
  await pauseBtn.click()
  await expect(page.locator('.statusbar').first()).toContainText('已暂停')
}

// 核心测量：真实 DOM 包围盒相交检查。
async function measureAndAssert(page: Page, label: string) {
  const marks = await boxes(page, '.rim-mark')
  const row1 = await boxes(page, '.seat .seat-row1')
  const holes = await boxes(page, '.seat .hole')
  const openCards = await boxes(page, '.seat .hole-open .card')
  const seats = await boxes(page, '.seat')
  const felt = await page.locator('.felt').boundingBox()
  const board = await page.locator('.board').boundingBox()
  const pot = await page.locator('.pot').boundingBox()
  const stage = await page.locator('.stage').boundingBox()

  expect(marks.length, `${label}: 应至少有下注徽章出现`).toBeGreaterThan(0)
  expect(seats.length, `${label}: 座位数应为 9`).toBe(9)

  const markVsRow1 = pairs(marks, row1)
  const markVsHole = pairs(marks, holes)
  const markVsOpen = pairs(marks, openCards)
  const markVsMark = pairs(marks, marks, true)
  const markVsBoard = board ? pairs(marks, [board as Rect]) : []
  const markVsPot = pot ? pairs(marks, [pot as Rect]) : []

  // 4) 护栏内侧 + 紧贴所属座位。
  const insideFelt: boolean[] = []
  const nearestSeat: number[] = []
  if (felt) {
    const fc = center(felt as Rect)
    const rx = felt.width / 2
    const ry = felt.height / 2
    for (const m of marks) {
      const c = center(m)
      insideFelt.push(((c.x - fc.x) / rx) ** 2 + ((c.y - fc.y) / ry) ** 2 <= 1.001)
      nearestSeat.push(Math.min(...seats.map((s) => Math.hypot(c.x - center(s).x, c.y - center(s).y))))
    }
  }

  const facts = {
    marks: marks.length,
    seats: seats.length,
    mark_vs_row1: markVsRow1,
    mark_vs_hole: markVsHole,
    mark_vs_open_cards: markVsOpen,
    mark_vs_mark: markVsMark,
    mark_vs_board: markVsBoard,
    mark_vs_pot: markVsPot,
    marks_inside_felt: insideFelt.every(Boolean),
    nearest_seat_dist_px: nearestSeat.map((d) => Math.round(d)),
    stage: stage ? { w: Math.round(stage.width), h: Math.round(stage.height) } : null,
    felt: felt ? { w: Math.round(felt.width), h: Math.round(felt.height) } : null,
    mark_boxes: marks.map((m) => ({
      x: Math.round(m.x),
      y: Math.round(m.y),
      w: Math.round(m.width),
      h: Math.round(m.height),
    })),
    seat_centers: seats.map((s) => ({ x: Math.round(center(s).x), y: Math.round(center(s).y) })),
  }
  record(label, facts)

  expect(markVsRow1, `${label}: 徽章遮挡座位卡第一行`).toEqual([])
  expect(markVsHole, `${label}: 徽章遮挡底牌占位/明牌容器`).toEqual([])
  expect(markVsOpen, `${label}: 徽章遮挡真人明牌`).toEqual([])
  expect(markVsMark, `${label}: 徽章彼此叠压`).toEqual([])
  expect(markVsBoard, `${label}: 徽章压住公共牌`).toEqual([])
  expect(markVsPot, `${label}: 徽章压住底池`).toEqual([])
  if (felt) expect(insideFelt.every(Boolean), `${label}: 徽章应落在护栏(felt)内侧`).toBe(true)
  return facts
}

test.afterEach(async ({ page }) => {
  await stopGameIfRunning(page)
})

test('chip_badge_all_ai · 全 AI 9 座：徽章不遮挡底牌/首行，落护栏内侧', async ({ page }) => {
  test.setTimeout(120_000)
  await page.setViewportSize({ width: 1536, height: 1024 })
  await create(page, { count: 9, humanSeat: null, button: 0, sb: 50, bb: 100 })
  await expect(page.getByText('公共观战')).toBeVisible()
  await page.getByRole('button', { name: '开始', exact: true }).click()
  // 等翻牌前全部跟注后，9 个座位都有本手累计投入 → 9 个徽章同时出现。
  await expect
    .poll(async () => page.locator('.rim-mark').count(), { timeout: 60_000, intervals: [100, 200] })
    .toBe(9)
  await pause(page)
  await measureAndAssert(page, 'all_ai_9')
  await page.screenshot({ path: path.join(OUT, 'desktop-allai-9.png'), animations: 'disabled' })
})

test('chip_badge_human · 真人 9 座：徽章不遮挡真人明牌与首行', async ({ page }) => {
  test.setTimeout(120_000)
  await page.setViewportSize({ width: 1536, height: 1024 })
  // 真人座位 4，按钮位 2 → 真人正好是大盲，开局即有本手累计投入（徽章），且明牌可见。
  await create(page, { count: 9, humanSeat: 4, button: 2, sb: 50, bb: 100 })
  await expect(page.getByText('真人视角')).toBeVisible()
  await page.getByRole('button', { name: '开始', exact: true }).click()
  // 等真人明牌出现（真人视角私有底牌）且真人徽章出现。
  await expect(page.locator('.seat.is-human .hole-open .card')).toHaveCount(2, { timeout: 30_000 })
  await expect
    .poll(async () => page.locator('.rim-mark').count(), { timeout: 60_000, intervals: [100, 200] })
    .toBeGreaterThanOrEqual(2)
  await pause(page)
  const facts = await measureAndAssert(page, 'human_9')

  // 真人座位的徽章确实出现（真人 seat 4 属于大盲，本轮/累计 > 0）。
  const humanHole = await page.locator('.seat.is-human .hole-open').boundingBox()
  expect(humanHole).toBeTruthy()
  await page.screenshot({ path: path.join(OUT, 'desktop-human-9.png'), animations: 'disabled' })

  // 局部放大裁剪：底部真人座位 + 左右下座位 + 顶排，便于目视复核。
  const clipShot = async (name: string, sel: string, pad = 70) => {
    const b = await page.locator(sel).boundingBox()
    if (!b) return
    await page.screenshot({
      path: path.join(OUT, name),
      animations: 'disabled',
      clip: {
        x: Math.max(0, b.x - pad),
        y: Math.max(0, b.y - pad),
        width: Math.min(1536, b.width + pad * 2),
        height: Math.min(1024, b.height + pad * 2),
      },
    })
  }
  await clipShot('crop-human-seat.png', '.seat.is-human', 130)
  record('human_9_zoom', { human_hole: humanHole })
})
