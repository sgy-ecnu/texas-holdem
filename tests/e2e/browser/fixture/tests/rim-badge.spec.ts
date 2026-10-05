import { expect, test } from '@playwright/test'
import { mkdir } from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

// 座位外侧下注附属条（.rim-mark）fixture 验收（非真实 Go 链路）：
//  1) 方向分派：|dx| > 1.25×|dy| → 左/右，否则上/下（临界优先上下）；只由槽位决定，稳定不随金额变化；
//  2) 整条在桌沿（.felt）外，不遮公共牌 / 底牌 / 座位卡 / 行动状态标签 / 动作特效；
//  3) 相邻徽章不叠压；2-9 人（尤其 9 人顶部 4 座）；
//  4) 桌面 / 窄桌面 / 移动端；最长金额。
// 截图落盘到 SCREEN_DIR（默认 docs/rim-badge）。

const HERE = path.dirname(fileURLToPath(import.meta.url))
const ROOT = path.resolve(HERE, '../../../../..')
const OUT = process.env.SCREEN_DIR ? path.resolve(process.env.SCREEN_DIR) : path.join(ROOT, 'docs/rim-badge')

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
    data: { seats, button: 0, small_blind: 50, big_blind: 100, max_hands: 100, mode: 'mock', max_live_calls: 0 },
  })
  expect(res.ok()).toBeTruthy()
  return res.json()
}

// 用 route 给公开状态注入本街下注（不改 fixture 数据）。
async function injectBets(
  page: import('@playwright/test').Page,
  amounts: (seat: number) => { street_bet: number; committed: number },
) {
  await page.route('**/api/v1/games/**', async (route) => {
    const res = await route.fetch()
    const json = await res.json()
    if (json?.hand?.players) {
      json.hand.players = json.hand.players.map((p: { seat: number }) => ({ ...p, ...amounts(p.seat) }))
    }
    await route.fulfill({ response: res, json })
  })
}

function overlaps(a: Rect, b: Rect, tol = 0): boolean {
  return !(
    a.x + a.width <= b.x + tol ||
    b.x + b.width <= a.x + tol ||
    a.y + a.height <= b.y + tol ||
    b.y + b.height <= a.y + tol
  )
}

async function boxes(page: import('@playwright/test').Page, sel: string): Promise<Rect[]> {
  const loc = page.locator(sel)
  const n = await loc.count()
  const out: Rect[] = []
  for (let i = 0; i < n; i++) {
    const b = await loc.nth(i).boundingBox()
    if (b) out.push(b)
  }
  return out
}

async function overflowX(page: import('@playwright/test').Page): Promise<number> {
  return page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)
}

async function assertBarsClear(page: import('@playwright/test').Page) {
  const bars = await boxes(page, '.rim-mark')
  expect(bars.length).toBeGreaterThan(0)
  const felt = await page.locator('.felt').boundingBox()
  const board = await page.locator('.board').boundingBox()
  const seats = await boxes(page, '.seat')
  const holes = await boxes(page, '.seat .hole')
  const badges = await boxes(page, '.turn-badge')
  for (let i = 0; i < bars.length; i++) {
    const b = bars[i]
    if (felt) expect(overlaps(b, felt), `bar ${i} 压住 felt（未在桌沿外）`).toBeFalsy()
    if (board) expect(overlaps(b, board), `bar ${i} 压住公共牌`).toBeFalsy()
    for (let j = 0; j < seats.length; j++) {
      expect(overlaps(b, seats[j]), `bar ${i} 压住座位卡 ${j}`).toBeFalsy()
    }
    for (let j = 0; j < holes.length; j++) {
      expect(overlaps(b, holes[j]), `bar ${i} 压住底牌 ${j}`).toBeFalsy()
    }
    for (let j = 0; j < badges.length; j++) {
      expect(overlaps(b, badges[j]), `bar ${i} 压住行动状态标签 ${j}`).toBeFalsy()
    }
    for (let j = i + 1; j < bars.length; j++) {
      expect(overlaps(b, bars[j], -1), `相邻 bar ${i}/${j} 叠压`).toBeFalsy()
    }
  }
  expect(await overflowX(page)).toBeLessThanOrEqual(1)
}

test.beforeEach(async ({ request }) => {
  await request.post('/__fixture/reset')
  await mkdir(OUT, { recursive: true })
})

test('9 人（真人 #4）：方向分派正确、外侧条在桌沿外、不遮中央/底牌/座位卡/行动标签、相邻不叠压', async ({ page }) => {
  await page.setViewportSize({ width: 1536, height: 1024 })
  const seats = Array.from({ length: 9 }, (_, i) => ai(i, `AI-${i + 1}`))
  seats[4] = { seat: 4, name: '我', stack: 10000, kind: 'human' }
  await injectBets(page, (seat) => ({ street_bet: 100 + seat * 50, committed: 250 + seat * 250 }))
  await createGame(page, seats)
  await page.goto('/')
  await expect(page.getByText('真人视角')).toBeVisible()
  await page.getByRole('button', { name: '开始' }).click()
  await expect(page.locator('.hand-dock[data-turn="me"]')).toBeVisible({ timeout: 15_000 })
  await expect(page.locator('.rim-mark').first()).toBeVisible({ timeout: 15_000 })

  // 方向分派（人类 #4 → ring 从 #4 起顺时针）：B/L2L/L2U/T4L/T4ML/T4MR/T4R/R2U/R2L
  const expected: Record<number, string> = {
    4: 'down', 5: 'left', 6: 'left', 7: 'up', 8: 'up', 0: 'up', 1: 'up', 2: 'right', 3: 'right',
  }
  const bars = page.locator('.rim-mark')
  const count = await bars.count()
  const stage = await page.locator('.stage').boundingBox()
  const felt = await page.locator('.felt').boundingBox()
  console.log(`[rim] stage=${stage?.width}x${stage?.height} felt=${Math.round(felt?.width ?? 0)}x${Math.round(felt?.height ?? 0)} bars=${count}`)
  for (let i = 0; i < count; i++) {
    const el = bars.nth(i)
    const seat = Number(await el.getAttribute('data-seat'))
    const dir = await el.getAttribute('data-dir')
    const b = await el.boundingBox()
    console.log(`[rim] seat=${seat} dir=${dir} box=${b ? `${Math.round(b.x)},${Math.round(b.y)} ${Math.round(b.width)}x${Math.round(b.height)}` : 'null'}`)
    expect(dir, `seat ${seat} 方向`).toBe(expected[seat])
  }
  await assertBarsClear(page)
  await page.screenshot({ path: path.join(OUT, 'desktop-9-bets.png'), fullPage: false })
})

test('9 人（真人 #0）：顶部 4 座方向同上、外侧条不叠压、不溢出', async ({ page }) => {
  await page.setViewportSize({ width: 1536, height: 1024 })
  const seats = Array.from({ length: 9 }, (_, i) => ai(i, `AI-${i + 1}`))
  seats[0] = { seat: 0, name: '我', stack: 10000, kind: 'human' }
  await injectBets(page, (seat) => ({ street_bet: 200, committed: 400 }))
  await createGame(page, seats)
  await page.goto('/')
  await page.getByRole('button', { name: '开始' }).click()
  await expect(page.locator('.hand-dock[data-turn="me"]')).toBeVisible({ timeout: 15_000 })
  await expect(page.locator('.rim-mark').first()).toBeVisible({ timeout: 15_000 })
  await assertBarsClear(page)
  await page.screenshot({ path: path.join(OUT, 'desktop-9-human0.png'), fullPage: false })
})

test('最长金额：整条仍在桌沿外、不产生横向滚动', async ({ page }) => {
  await page.setViewportSize({ width: 1536, height: 1024 })
  const seats = Array.from({ length: 9 }, (_, i) => ai(i, `AI-${i + 1}`))
  seats[4] = { seat: 4, name: '我', stack: 10_000_000, kind: 'human' }
  await injectBets(page, () => ({ street_bet: 9_999_999, committed: 10_000_000 }))
  await createGame(page, seats)
  await page.goto('/')
  await page.getByRole('button', { name: '开始' }).click()
  await expect(page.locator('.hand-dock[data-turn="me"]')).toBeVisible({ timeout: 15_000 })
  await expect(page.locator('.rim-mark').first()).toBeVisible({ timeout: 15_000 })
  await assertBarsClear(page)
  await page.screenshot({ path: path.join(OUT, 'desktop-max-amount.png'), fullPage: false })
})

test('窄桌面 900x800：空间不足提前切列表布局，外侧条隐藏、下注信息回卡内 .bet、无横向滚动', async ({ page }) => {
  await page.setViewportSize({ width: 900, height: 800 })
  const seats = Array.from({ length: 9 }, (_, i) => ai(i, `AI-${i + 1}`))
  seats[4] = { seat: 4, name: '我', stack: 10000, kind: 'human' }
  await injectBets(page, (seat) => ({ street_bet: 150 + seat * 25, committed: 300 + seat * 100 }))
  await createGame(page, seats)
  await page.goto('/')
  await page.getByRole('button', { name: '开始' }).click()
  await expect(page.locator('.hand-dock[data-turn="me"]')).toBeVisible({ timeout: 15_000 })
  await expect(page.locator('.hand-dock')).toBeVisible({ timeout: 15_000 })
  await expect(page.locator('.stage.is-list')).toHaveCount(1)
  await expect(page.locator('.rim-mark').first()).toBeHidden()
  await expect(page.locator('.seat .bet').first()).toBeVisible()
  expect(await overflowX(page)).toBeLessThanOrEqual(1)
  await page.screenshot({ path: path.join(OUT, 'narrow-900.png'), fullPage: false })
})

test('移动端 390：外侧条隐藏，下注信息在座位卡内 .bet，无横向溢出', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 })
  const seats = [ai(0, 'AI-1'), ai(1, 'AI-2'), { seat: 2, name: '我', stack: 10000, kind: 'human' }]
  await injectBets(page, () => ({ street_bet: 100, committed: 250 }))
  await createGame(page, seats)
  await page.goto('/')
  await page.getByRole('button', { name: '开始' }).click()
  await expect(page.locator('.hand-dock[data-turn="me"]')).toBeVisible({ timeout: 15_000 })
  await expect(page.locator('.rim-mark').first()).toBeHidden()
  await expect(page.locator('.seat .bet').first()).toBeVisible()
  expect(await overflowX(page)).toBeLessThanOrEqual(1)
  await page.screenshot({ path: path.join(OUT, 'mobile-390.png'), fullPage: false })
})

// ---- 徽章外观 + 与动作特效不撞位（Bug1/Bug2 回归） ----

// 同一 GET 路由同时注入本街下注与可切换的 last_action（避免多 route 互相 fulfill）。
interface ActionScript {
  action: Record<string, unknown> | null
}
async function installBetsAndAction(page: import('@playwright/test').Page, state: ActionScript) {
  await page.route(/\/api\/v1\/games(\/.*)?$/, async (route) => {
    if (route.request().method() !== 'GET') return route.continue()
    const res = await route.fetch()
    const json = await res.json()
    if (json && typeof json === 'object') {
      json.last_action = state.action
      if (json.hand) {
        json.hand.number = 1
        json.hand.players = json.hand.players.map((p: { seat: number }) => ({
          ...p,
          street_bet: 500,
          committed: 1500,
        }))
      }
    }
    await route.fulfill({ response: res, json })
  })
}

function probeAct(sequence: number, seat: number, kind: string, to = 0, amount = 0) {
  return {
    hand_no: 1,
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

test('顶部座位：动作特效 scale 1.8→缩回全程不压顶部下注条、不向上溢出 stage', async ({ page }) => {
  await page.setViewportSize({ width: 1536, height: 1024 })
  const state: ActionScript = { action: probeAct(1, 2, 'call', 0, 100) }
  await installBetsAndAction(page, state)
  await createGame(page, [
    { seat: 0, name: '我', stack: 10000, kind: 'human' },
    ai(1, 'AI-2'),
    ai(2, 'AI-3'),
    ai(3, 'AI-4'),
  ])
  await page.goto('/')
  await page.getByRole('button', { name: '开始' }).click()
  const bar = page.locator('.seat-slot[data-seat="2"] .rim-mark')
  await expect(bar).toBeVisible({ timeout: 15_000 })
  await expect(bar).toHaveAttribute('data-dir', 'up')

  // 触发顶部座位动作（切换 sequence 才会弹出）。
  state.action = probeAct(2, 2, 'call', 0, 100)
  const pop = page.locator('.seat-slot[data-seat="2"] .action-popover')
  await expect(pop).toHaveText('跟注 100', { timeout: 4000 })

  const frames = await page.evaluate(() => {
    const barEl = document.querySelector('.seat-slot[data-seat="2"] .rim-mark') as HTMLElement
    const popEl = document.querySelector('.seat-slot[data-seat="2"] .action-popover') as HTMLElement
    const stage = document.querySelector('.stage')!.getBoundingClientRect()
    const anims = popEl.getAnimations()
    const anim = anims[anims.length - 1]
    const out: Array<Record<string, number | boolean>> = []
    for (const t of [0, 100, 320, 620, 920, 1980]) {
      if (anim) {
        anim.pause()
        anim.currentTime = t
      }
      const b = barEl.getBoundingClientRect()
      const r = popEl.getBoundingClientRect()
      const overlap = !(
        r.bottom <= b.top ||
        r.top >= b.bottom ||
        r.right <= b.left ||
        r.left >= b.right
      )
      out.push({
        t,
        barTop: b.top,
        barBottom: b.bottom,
        popTop: r.top,
        popBottom: r.bottom,
        overlap: overlap ? 1 : 0,
        aboveStageTop: r.top < stage.top ? 1 : 0,
      })
    }
    return out
  })
  for (const f of frames) {
    expect(f.overlap, `t=${f.t}ms 动作特效压住顶部下注条`).toBe(0)
    expect(f.aboveStageTop, `t=${f.t}ms 动作特效向上溢出 stage 顶部`).toBe(0)
    // 动作特效整体在徽章下方（完全垂直分离）。
    expect(Number(f.popTop), `t=${f.t}ms 特效未落到徽章下方`).toBeGreaterThanOrEqual(Number(f.barBottom))
  }
  expect(await overflowX(page)).toBeLessThanOrEqual(1)
  await page.screenshot({ path: path.join(OUT, 'top-action-no-collision.png'), fullPage: false })
  await expect(pop).toHaveCount(0, { timeout: 4000 })
})

test('徽章外观：浅色弱边框附属条（非深色胶囊），本轮强调、累计次级可读', async ({ page }) => {
  await page.setViewportSize({ width: 1536, height: 1024 })
  await injectBets(page, () => ({ street_bet: 300, committed: 900 }))
  const seats = Array.from({ length: 6 }, (_, i) => ai(i, `AI-${i + 1}`))
  seats[3] = { seat: 3, name: '我', stack: 10000, kind: 'human' }
  await createGame(page, seats)
  await page.goto('/')
  await page.getByRole('button', { name: '开始' }).click()
  const bar = page.locator('.rim-mark').first()
  await expect(bar).toBeVisible({ timeout: 15_000 })

  const style = await bar.evaluate((el) => {
    const cs = getComputedStyle(el)
    const parse = (s: string) => (s.match(/[\d.]+/g) ?? []).map(Number)
    const now = el.querySelector('.rim-now')
    const cum = el.querySelector('.rim-cum')
    return {
      bg: parse(cs.backgroundColor).slice(0, 3),
      border: parse(cs.borderTopColor),
      now: parse(now ? getComputedStyle(now).color : '').slice(0, 3),
      cum: parse(cum ? getComputedStyle(cum).color : '').slice(0, 3),
    }
  })
  // 浅色背景：三通道均高（>180），远离原深色 rgba(10,16,14,.72)。
  expect(Math.min(...style.bg)).toBeGreaterThan(180)
  // 边框不再是金色（金 R>G>B 且通道跨度大）：跨度应较小。
  expect(Math.max(...style.border.slice(0, 3)) - Math.min(...style.border.slice(0, 3))).toBeLessThan(
    40,
  )
  // 本轮 = 一级强调（近黑，可读性最强）；累计 = 次级中灰（比本轮浅、但可读）。
  expect(Math.max(...style.now)).toBeLessThan(90)
  expect(Math.min(...style.cum)).toBeGreaterThan(Math.max(...style.now))
  expect(Math.min(...style.cum)).toBeGreaterThan(80)
  await page.screenshot({ path: path.join(OUT, 'badge-light-appearance.png'), fullPage: false })
})

test('下/左/右方向无同座徽章与动作特效撞位（不回归）', async ({ page }) => {
  await page.setViewportSize({ width: 1536, height: 1024 })
  const state: ActionScript = { action: probeAct(1, 0, 'check') }
  await installBetsAndAction(page, state)
  await createGame(page, [
    { seat: 0, name: '我', stack: 10000, kind: 'human' },
    ai(1, 'AI-2'),
    ai(2, 'AI-3'),
    ai(3, 'AI-4'),
  ])
  await page.goto('/')
  await page.getByRole('button', { name: '开始' }).click()
  await expect(page.locator('.rim-mark').first()).toBeVisible({ timeout: 15_000 })

  // 4 座：seat0→下/右（真人底部），seat1→左，seat2→上，seat3→右。
  const targets: Array<{ seat: number; dir: string }> = [
    { seat: 0, dir: 'down' },
    { seat: 1, dir: 'left' },
    { seat: 3, dir: 'right' },
  ]
  let seq = 2
  for (const t of targets) {
    state.action = probeAct(seq++, t.seat, 'check')
    const pop = page.locator(`.seat-slot[data-seat="${t.seat}"] .action-popover`)
    await expect(pop).toHaveText('过牌', { timeout: 4000 })
    const overlap = await page.evaluate((seat) => {
      const barEl = document.querySelector(`.seat-slot[data-seat="${seat}"] .rim-mark`) as HTMLElement
      const popEl = document.querySelector(`.seat-slot[data-seat="${seat}"] .action-popover`) as HTMLElement
      const anims = popEl.getAnimations()
      const anim = anims[anims.length - 1]
      let bad = 0
      for (const tt of [0, 320, 920]) {
        if (anim) {
          anim.pause()
          anim.currentTime = tt
        }
        const b = barEl.getBoundingClientRect()
        const r = popEl.getBoundingClientRect()
        const ov = !(r.bottom <= b.top || r.top >= b.bottom || r.right <= b.left || r.left >= b.right)
        if (ov) bad++
      }
      return bad
    }, t.seat)
    expect(overlap, `seat ${t.seat}(${t.dir}) 动作特效压住同座徽章`).toBe(0)
    await expect(pop).toHaveCount(0, { timeout: 4000 })
  }
  await page.screenshot({ path: path.join(OUT, 'other-dirs-no-collision.png'), fullPage: false })
})

