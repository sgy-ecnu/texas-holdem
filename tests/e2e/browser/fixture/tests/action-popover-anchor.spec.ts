import { expect, test } from '@playwright/test'
import { mkdir } from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

// 动作标签「归属/贴边」验收（fixture，非真实 Go 链路）：
//  1) 内侧固定贴边：标签紧贴所属座位卡（间距 ≈ --pop-gap），不再浮在座位之间的同一片空隙；
//  2) 短尖角真正朝向对应卡片：below→border-bottom 指向卡片、right→border-right 指向左、left→border-left 指向右；
//  3) 5 人局 AI-4（UR，向下展开）与 AI-5（R，贴左侧）相邻过牌并存时，两标签分离、各自贴座、互不重叠；
//  4) 9 人密集 + 长「加注到」1.8 倍峰值：侧边标签紧凑化后不遮挡公共牌/底池；
//  5) 移动端列表布局：不显示贴边尖角；reduced-motion：静态保留贴边与尖角。
// 用 page.route 脚本化 last_action。首个观察到的 last_action 只作基线不弹。
// 明确：本 fixture 不能作为「真实 Go 联调」完成证据。

const HERE = path.dirname(fileURLToPath(import.meta.url))
const ROOT = path.resolve(HERE, '../../../../..')
const OUT = process.env.SCREEN_DIR
  ? path.resolve(process.env.SCREEN_DIR)
  : path.join(ROOT, 'docs/action-popover-anchor')

interface Rect {
  x: number
  y: number
  width: number
  height: number
}

interface ScriptState {
  action: Record<string, unknown> | null
  handNo: number
}

function ai(seat: number, name: string) {
  return { seat, name, stack: 10000, kind: 'ai' as const, model_ref: 'mock' }
}

function act(handNo: number, sequence: number, seat: number, kind: string, to = 0, amount = 0) {
  return {
    hand_no: handNo,
    sequence,
    seat,
    kind,
    to,
    amount,
    source: 'mock',
    error_code: null,
    input_tokens: null,
    output_tokens: null,
  }
}

async function createGame(page: import('@playwright/test').Page, seats: Array<Record<string, unknown>>) {
  const res = await page.request.post('/api/v1/games', {
    data: { seats, button: 0, small_blind: 50, big_blind: 100, max_hands: 100, mode: 'mock', max_live_calls: 0 },
  })
  expect(res.ok()).toBeTruthy()
  const game = await res.json()
  const start = await page.request.post(`/api/v1/games/${game.id}/commands`, { data: { command: 'start' } })
  expect(start.ok()).toBeTruthy()
  return game
}

async function installScript(page: import('@playwright/test').Page, state: ScriptState) {
  await page.route(/\/api\/v1\/games(\/.*)?$/, async (route) => {
    if (route.request().method() !== 'GET') return route.continue()
    const res = await route.fetch()
    const json = await res.json()
    if (json && typeof json === 'object') {
      json.last_action = state.action
      if (json.hand) json.hand.number = state.handNo
    }
    await route.fulfill({ response: res, json })
  })
}

function overlaps(a: Rect, b: Rect, tol = 0): boolean {
  return !(a.x + a.width <= b.x + tol || b.x + b.width <= a.x + tol || a.y + a.height <= b.y + tol || b.y + b.height <= a.y + tol)
}

// 把标签动画定格到指定毫秒：t=1000 落在 scale 1 的停留段，便于测真实贴边间距。
async function freezeAt(page: import('@playwright/test').Page, seat: number, ms: number) {
  await page.evaluate(
    ([s, t]) => {
      const el = document.querySelector(`.seat-slot[data-seat="${s}"] .action-popover`) as HTMLElement | null
      if (!el) return
      for (const a of el.getAnimations()) {
        a.pause()
        a.currentTime = t as number
      }
    },
    [seat, ms] as const,
  )
}

interface Pseudo {
  display: string
  borderTopColor: string
  borderBottomColor: string
  borderLeftColor: string
  borderRightColor: string
  borderTopWidth: string
  borderBottomWidth: string
  borderLeftWidth: string
  borderRightWidth: string
}

async function pointerStyle(page: import('@playwright/test').Page, seat: number): Promise<Pseudo> {
  return page.evaluate((s) => {
    const el = document.querySelector(`.seat-slot[data-seat="${s}"] .action-popover`) as HTMLElement
    const cs = getComputedStyle(el, '::after')
    return {
      display: cs.display,
      borderTopColor: cs.borderTopColor,
      borderBottomColor: cs.borderBottomColor,
      borderLeftColor: cs.borderLeftColor,
      borderRightColor: cs.borderRightColor,
      borderTopWidth: cs.borderTopWidth,
      borderBottomWidth: cs.borderBottomWidth,
      borderLeftWidth: cs.borderLeftWidth,
      borderRightWidth: cs.borderRightWidth,
    }
  }, seat)
}

// 标签贴边间距（px）：沿各自锚定轴，标签贴边一侧到座位卡对应边的距离（scale 1 时 ≈ 6）。
async function anchorGap(page: import('@playwright/test').Page, seat: number, anchor: 'below' | 'right' | 'left') {
  await freezeAt(page, seat, 1000)
  const card = (await page.locator(`.seat-slot[data-seat="${seat}"] .seat`).boundingBox())!
  const pop = (await page.locator(`.seat-slot[data-seat="${seat}"] .action-popover`).boundingBox())!
  if (anchor === 'below') return pop.y - (card.y + card.height)
  if (anchor === 'right') return pop.x - (card.x + card.width)
  return card.x - (pop.x + pop.width)
}

test.beforeEach(async ({ request }) => {
  await request.post('/__fixture/reset')
  await mkdir(OUT, { recursive: true })
})

test('5 人：AI-4(UR 向下) 与 AI-5(R 贴左) 相邻过牌并存，各自贴座、尖角指向、互不重叠', async ({ page }) => {
  await page.setViewportSize({ width: 1536, height: 1024 })
  const state: ScriptState = { action: act(1, 1, 2, 'check'), handNo: 1 }
  await installScript(page, state)
  // human0→底部；seat3→UR（顶部右）、seat4→R（正右），即任务聚焦的相邻两座。
  await createGame(page, [
    { seat: 0, name: '我', stack: 10000, kind: 'human' },
    ai(1, 'AI-2'),
    ai(2, 'AI-3'),
    ai(3, 'AI-4'),
    ai(4, 'AI-5'),
  ])

  await page.goto('/')
  await expect(page.getByText('真人视角')).toBeVisible()
  await page.waitForTimeout(400)

  // 触发出相邻两座标签使其并存（1.6s 窗口内）。
  state.action = act(1, 2, 3, 'check')
  await expect(page.locator('.seat-slot[data-seat="3"] .action-popover')).toHaveText('过牌', { timeout: 4000 })
  state.action = act(1, 3, 4, 'check')
  await expect(page.locator('.action-popover')).toHaveCount(2, { timeout: 4000 })

  const seat3 = page.locator('.seat-slot[data-seat="3"] .action-popover')
  const seat4 = page.locator('.seat-slot[data-seat="4"] .action-popover')
  // 归属分派：AI-4(UR) 向下展开贴卡片下沿；AI-5(R) 贴卡片左侧。
  await expect(seat3).toHaveClass(/is-above-right/)
  await expect(seat3).toHaveClass(/anchor-below/)
  await expect(seat4).toHaveClass(/is-above-right/)
  await expect(seat4).toHaveClass(/anchor-left/)

  // 两标签不糊在一起。
  const b3 = (await seat3.boundingBox())!
  const b4 = (await seat4.boundingBox())!
  expect(overlaps(b3, b4), '5 人相邻两座标签不应重叠').toBeFalsy()

  // 各自贴住所属座位卡；尖角朝向正确。
  const gap3 = await anchorGap(page, 3, 'below')
  const gap4 = await anchorGap(page, 4, 'left')
  expect(gap3, 'AI-4 标签应贴卡片下沿（间距 0–10px）').toBeGreaterThanOrEqual(0)
  expect(gap3, 'AI-4 标签不应浮在座位间隙').toBeLessThanOrEqual(10)
  expect(gap4, 'AI-5 标签应贴卡片左侧（间距 0–10px）').toBeGreaterThanOrEqual(0)
  expect(gap4, 'AI-5 标签不应浮在座位间隙').toBeLessThanOrEqual(10)

  const p3 = await pointerStyle(page, 3)
  expect(p3.display).not.toBe('none')
  expect(p3.borderBottomColor, '向下标签尖角朝上指向卡片').toBe('rgb(130, 213, 232)')
  expect(p3.borderTopWidth).toBe('0px')
  const p4 = await pointerStyle(page, 4)
  expect(p4.display).not.toBe('none')
  expect(p4.borderLeftColor, '贴左标签尖角朝右指向卡片').toBe('rgb(130, 213, 232)')
  expect(p4.borderRightWidth).toBe('0px')

  await page.screenshot({ path: path.join(OUT, 'five-seat-ai4-ai5-adjacent.png') })

  // 底部真人座位：贴卡片右侧、尖角朝左。
  state.action = act(1, 4, 0, 'call', 0, 100)
  const human = page.locator('.seat-slot[data-seat="0"] .action-popover')
  await expect(human).toHaveText('跟注 100', { timeout: 4000 })
  await expect(human).toHaveClass(/anchor-right/)
  const humanGap = await anchorGap(page, 0, 'right')
  expect(humanGap).toBeGreaterThanOrEqual(0)
  expect(humanGap).toBeLessThanOrEqual(10)
  const ph = await pointerStyle(page, 0)
  expect(ph.borderRightColor, '底部真人标签尖角朝左指向卡片').toBe('rgb(233, 199, 104)')
  expect(ph.borderLeftWidth).toBe('0px')
  await page.screenshot({ path: path.join(OUT, 'five-seat-bottom-human.png') })
})

test('4 座：四种锚定边贴边间距与尖角方向（below/right/left）', async ({ page }) => {
  await page.setViewportSize({ width: 1536, height: 1024 })
  const state: ScriptState = { action: act(1, 1, 2, 'check'), handNo: 1 }
  await installScript(page, state)
  // human0→B(down,anchor right)、seat1→L(left,anchor right)、seat2→T(up,anchor below)、seat3→R(right,anchor left)。
  await createGame(page, [
    { seat: 0, name: '我', stack: 10000, kind: 'human' },
    ai(1, 'AI-2'),
    ai(2, 'AI-3'),
    ai(3, 'AI-4'),
  ])
  await page.goto('/')
  await expect(page.getByText('真人视角')).toBeVisible()
  await page.waitForTimeout(400)

  state.action = act(1, 2, 2, 'check')
  await expect(page.locator('.seat-slot[data-seat="2"] .action-popover')).toHaveText('过牌', { timeout: 4000 })
  const top = await pointerStyle(page, 2)
  expect(top.borderBottomColor, '顶部标签尖角朝上').toBe('rgb(130, 213, 232)')
  expect(await anchorGap(page, 2, 'below')).toBeLessThanOrEqual(10)

  state.action = act(1, 3, 1, 'check')
  await expect(page.locator('.seat-slot[data-seat="1"] .action-popover')).toHaveText('过牌', { timeout: 4000 })
  const left = await pointerStyle(page, 1)
  // 正左座位：标签贴卡片右侧、尖角朝左指向卡片 → 着色边为 border-right。
  expect(left.borderRightColor, '正左座位标签尖角朝左指向卡片').toBe('rgb(130, 213, 232)')
  expect(await anchorGap(page, 1, 'right')).toBeLessThanOrEqual(10)

  state.action = act(1, 4, 3, 'check')
  await expect(page.locator('.seat-slot[data-seat="3"] .action-popover')).toHaveText('过牌', { timeout: 4000 })
  const right = await pointerStyle(page, 3)
  // 正右座位：标签贴卡片左侧、尖角朝右指向卡片 → 着色边为 border-left。
  expect(right.borderLeftColor, '正右座位标签尖角朝右指向卡片').toBe('rgb(130, 213, 232)')
  expect(await anchorGap(page, 3, 'left')).toBeLessThanOrEqual(10)

  await page.screenshot({ path: path.join(OUT, 'four-seat-anchors.png') })
})

test('9 人密集 + 长「加注到」1.8 倍峰值：侧边标签紧凑化不遮挡公共牌/底池', async ({ page }) => {
  await page.setViewportSize({ width: 1536, height: 1024 })
  const state: ScriptState = { action: act(1, 1, 0, 'check'), handNo: 1 }
  await installScript(page, state)
  const seats = Array.from({ length: 9 }, (_, i) => ai(i, `AI-${i + 1}`))
  await createGame(page, seats)
  await page.goto('/')
  await expect(page.getByText('公共观战')).toBeVisible()
  await page.waitForTimeout(400)

  // seat1→L2L（正左偏下，anchor right），放最长金额，验证 1.8 倍峰值横向伸展不压公共牌。
  state.action = act(1, 2, 1, 'raise_to', 1234567)
  const pop = page.locator('.seat-slot[data-seat="1"] .action-popover')
  await expect(pop).toHaveText('加注到 1,234,567', { timeout: 4000 })
  await expect(pop).toHaveClass(/anchor-right/)
  // 定位到放大峰值（t=200ms 仍为 scale 1.8），检查与公共牌/底池/街名不重叠。
  await freezeAt(page, 1, 200)
  const pb = (await pop.boundingBox())!
  for (const sel of ['.board', '.pot', '.street-label']) {
    for (const ob of await page.locator(sel).all()) {
      const box = await ob.boundingBox()
      if (box) expect(overlaps(pb, box), `9 人峰值侧边标签压住 ${sel}`).toBeFalsy()
    }
  }
  await page.screenshot({ path: path.join(OUT, 'nine-seat-long-amount-peak.png') })
})

test('移动端 390 列表布局：不显示贴边尖角、不溢出', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 })
  const state: ScriptState = { action: act(1, 1, 0, 'check'), handNo: 1 }
  await installScript(page, state)
  await createGame(page, [ai(0, 'AI-1'), ai(1, 'AI-2'), ai(2, 'AI-3')])
  await page.goto('/')
  await expect(page.getByText('公共观战')).toBeVisible()
  await page.waitForTimeout(400)

  state.action = act(1, 2, 0, 'check')
  await expect(page.locator('.seat-slot[data-seat="0"] .action-popover')).toHaveText('过牌', { timeout: 4000 })
  const p = await pointerStyle(page, 0)
  expect(p.display, '列表布局不显示贴边尖角').toBe('none')
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)
  expect(overflow).toBeLessThanOrEqual(1)
  await page.screenshot({ path: path.join(OUT, 'mobile-list-no-arrow.png') })
})

test('reduced-motion：静态保留贴边与尖角，无动画', async ({ page }) => {
  await page.setViewportSize({ width: 1536, height: 1024 })
  await page.emulateMedia({ reducedMotion: 'reduce' })
  const state: ScriptState = { action: act(1, 1, 2, 'check'), handNo: 1 }
  await installScript(page, state)
  await createGame(page, [
    { seat: 0, name: '我', stack: 10000, kind: 'human' },
    ai(1, 'AI-2'),
    ai(2, 'AI-3'),
    ai(3, 'AI-4'),
  ])
  await page.goto('/')
  await expect(page.getByText('真人视角')).toBeVisible()
  await page.waitForTimeout(400)

  state.action = act(1, 2, 2, 'check')
  const pop = page.locator('.seat-slot[data-seat="2"] .action-popover')
  await expect(pop).toHaveText('过牌', { timeout: 4000 })
  const style = await pop.evaluate((el) => {
    const s = getComputedStyle(el)
    return { animationName: s.animationName, transform: s.transform, opacity: s.opacity }
  })
  expect(style.animationName, 'reduced-motion 不施加动画').toBe('none')
  expect(style.transform, 'reduced-motion 无缩放').toBe('none')
  expect(Number(style.opacity)).toBeGreaterThan(0.9)
  const p = await pointerStyle(page, 2)
  expect(p.display, 'reduced-motion 仍保留尖角').not.toBe('none')
  await expect(pop).toHaveClass(/anchor-below/)
  await page.screenshot({ path: path.join(OUT, 'reduced-motion-anchor.png') })
})
