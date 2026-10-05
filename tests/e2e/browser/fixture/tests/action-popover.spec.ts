import { expect, test } from '@playwright/test'
import { mkdir } from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

// 座位旁动作标签（popover）验收（fixture，非真实 Go 链路）：
//  1) 去重触发：首次 last_action 只作基线不弹、重复轮询不重播、只弹严格递增的新动作；
//  2) 同座位替换不堆叠、不同座位并存、进入下一手清空上手指；
//  3) 文案映射：call→跟注 {amount}、raise_to→加注到 {to}、fold/check 文案；
//  4) 弃牌后标签不变暗（与 .seat 同级）；布局不遮挡 turn-badge / 底牌 / 中央区；
//  5) 2/6/9 人 + 移动端无重叠 / 不溢出；reduced-motion 下降级为静态可见后由 timer 移除。
// 用 page.route 脚本化 last_action（不改 fixture-server.mjs），因此 amount 字段可控。
// 明确：本 fixture 不能作为「真实 Go 联调」完成证据。

const HERE = path.dirname(fileURLToPath(import.meta.url))
const ROOT = path.resolve(HERE, '../../../../..')
const OUT = process.env.SCREEN_DIR
  ? path.resolve(process.env.SCREEN_DIR)
  : path.join(ROOT, 'docs/action-popover')

interface Rect {
  x: number
  y: number
  width: number
  height: number
}

function ai(seat: number, name: string) {
  return { seat, name, stack: 10000, kind: 'ai' as const, model_ref: 'mock' }
}

function act(
  handNo: number,
  sequence: number,
  seat: number,
  kind: string,
  to = 0,
  amount = 0,
): Record<string, unknown> {
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

// 用共享可变状态脚本化服务端 last_action：测试改 state 后，最多一个轮询周期即生效。
// 这样能精确控制「同一动作重复轮询」「新动作」「跨手」等时序，无需改 fixture。
interface ScriptState {
  action: Record<string, unknown> | null
  handNo: number
  foldSeat?: number
}

async function installScript(page: import('@playwright/test').Page, state: ScriptState) {
  await page.route(/\/api\/v1\/games(\/.*)?$/, async (route) => {
    if (route.request().method() !== 'GET') return route.continue()
    const res = await route.fetch()
    const json = await res.json()
    if (json && typeof json === 'object') {
      json.last_action = state.action
      if (json.hand) {
        json.hand.number = state.handNo
        if (state.foldSeat !== undefined && Array.isArray(json.hand.players)) {
          const p = json.hand.players.find((x: { seat: number }) => x.seat === state.foldSeat)
          if (p) p.status = 'folded'
        }
      }
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

async function startHumanGame(page: import('@playwright/test').Page, seats: Array<Record<string, unknown>>) {
  const game = await createGame(page, seats)
  const res = await page.request.post(`/api/v1/games/${game.id}/commands`, {
    data: { command: 'start' },
  })
  expect(res.ok()).toBeTruthy()
  return game
}

test.beforeEach(async ({ request }) => {
  await request.post('/__fixture/reset')
  await mkdir(OUT, { recursive: true })
})

test('去重触发 + 同座位 FIFO 不截断 + 不同座位并存 + 跨手不清 + 弃牌不变暗 + 标签定位', async ({ page }) => {
  await page.setViewportSize({ width: 1536, height: 1024 })
  const state: ScriptState = { action: act(1, 1, 2, 'call', 0, 300), handNo: 1 }
  await installScript(page, state)
  await startHumanGame(page, [
    { seat: 0, name: '我', stack: 10000, kind: 'human' },
    ai(1, 'AI-2'),
    ai(2, 'AI-3'),
  ])

  const popover = page.locator('.action-popover')
  await page.goto('/')
  await expect(page.getByText('真人视角')).toBeVisible()

  // 基线：首次 last_action 不弹；随后同一动作重复轮询也不弹。
  await page.waitForTimeout(400)
  await expect(popover).toHaveCount(0)
  await page.waitForTimeout(1400)
  await expect(popover).toHaveCount(0)

  // 新动作（hand1 seq2 seat1 过牌）→ 弹出（左半座位走「向外斜上」定位）。
  state.action = act(1, 2, 1, 'check')
  const seat1 = page.locator('.seat-slot[data-seat="1"] .action-popover')
  await expect(seat1).toHaveText('过牌', { timeout: 4000 })
  await expect(seat1).toHaveClass(/is-check/)
  await expect(seat1).toHaveClass(/is-above-left/)

  // 重复轮询同一动作 → 不堆叠、不重播（仍为 1 个）。
  await page.waitForTimeout(600)
  await expect(popover).toHaveCount(1)
  await expect(seat1).toHaveText('过牌')

  // 同座位 FIFO：当前动作未结束时新动作入队等待，不截断旧动作。
  state.action = act(1, 3, 1, 'raise_to', 600)
  await page.waitForTimeout(500)
  await expect(seat1, '同座位新动作不得截断旧动作').toHaveText('过牌')
  await expect(popover).toHaveCount(1)
  // 旧动作完整展示后，新动作接上。
  await expect(seat1).toHaveText('加注到 600', { timeout: 4000 })
  await expect(seat1).toHaveClass(/is-raise/)
  await expect(popover).toHaveCount(1)

  // 不同座位并存：seat2 跟注，与 seat1 并发。
  state.action = act(1, 4, 2, 'call', 0, 75)
  await expect(page.locator('.seat-slot[data-seat="2"] .action-popover')).toHaveText('跟注 75', {
    timeout: 4000,
  })
  await expect(popover).toHaveCount(2)

  // 跨手：hand_no 递增不立即清空已开始标签；本手新动作独立弹出。
  state.handNo = 2
  state.action = act(2, 1, 0, 'check')
  await expect(page.locator('.seat-slot[data-seat="0"] .action-popover')).toHaveText('过牌', {
    timeout: 4000,
  })

  // 弃牌：底部真人座位标签走右侧、且不因 .seat.is-folded 变暗。
  state.foldSeat = 0
  state.action = act(2, 2, 0, 'fold')
  const humanPop = page.locator('.seat-slot[data-seat="0"] .action-popover')
  await expect(humanPop).toHaveText('弃牌', { timeout: 6000 })
  await expect(humanPop).toHaveClass(/is-right/)
  await expect(page.locator('.seat.is-human.is-folded')).toHaveCount(1)
  await page.waitForTimeout(300) // 越过 80ms 淡入尾部，读取稳定不透明度。
  const opacity = Number(await humanPop.evaluate((el) => getComputedStyle(el).opacity))
  expect(opacity, '弃牌后标签不应变暗').toBeGreaterThan(0.9)

  // 不遮挡 turn-badge（本人）、底牌与中央公共牌。
  const popBoxes = await boxes(page, '.action-popover')
  for (const sel of ['.turn-badge', '.hole', '.board', '.pot']) {
    for (const pb of popBoxes) {
      for (const ob of await boxes(page, sel)) {
        expect(overlaps(pb, ob), `标签压住 ${sel}`).toBeFalsy()
      }
    }
  }

  // AI 座位新动作走「座位上沿外侧」定位。
  state.action = act(2, 3, 1, 'call', 0, 50)
  await expect(page.locator('.seat-slot[data-seat="1"] .action-popover')).toHaveText('跟注 50', {
    timeout: 4000,
  })
  await expect(page.locator('.seat-slot[data-seat="1"] .action-popover')).toHaveClass(/is-above/)

  await page.screenshot({ path: path.join(OUT, 'desktop-human-3-popover.png') })
})

test('reduced-motion：标签降级为静态可见，随后由 timer 移除', async ({ page }) => {
  await page.setViewportSize({ width: 1536, height: 1024 })
  await page.emulateMedia({ reducedMotion: 'reduce' })
  const state: ScriptState = { action: act(1, 1, 0, 'call', 0, 100), handNo: 1 }
  await installScript(page, state)
  await createGame(page, [ai(0, 'AI-1'), ai(1, 'AI-2')])

  await page.goto('/')
  await expect(page.getByText('公共观战')).toBeVisible()

  state.action = act(1, 2, 0, 'check')
  const popover = page.locator('.action-popover')
  await expect(popover).toHaveCount(1, { timeout: 4000 })
  const style = await popover.evaluate((el) => {
    const s = getComputedStyle(el)
    return { opacity: s.opacity, animationName: s.animationName }
  })
  expect(style.animationName, 'reduced-motion 下不施加动画').toBe('none')
  expect(Number(style.opacity), 'reduced-motion 下静态可见').toBeGreaterThan(0.9)

  // 静态可见应持续（不会被 0.01ms 动画瞬间抹掉）。
  await page.waitForTimeout(800)
  await expect(popover).toHaveCount(1)
  await page.screenshot({ path: path.join(OUT, 'reduced-motion-static.png') })

  // 仍由 React timer 在 ~1.6s 后移除。
  await expect(popover).toHaveCount(0, { timeout: 3000 })
})

test('人数模板 2/6/9（全 AI）：标签不压中央、不互相重叠、无横向溢出', async ({ page }) => {
  await page.setViewportSize({ width: 1536, height: 1024 })
  for (const n of [2, 6, 9]) {
    await page.request.post('/__fixture/reset')
    const state: ScriptState = { action: act(1, 1, 0, 'call', 0, 300), handNo: 1 }
    const seats = Array.from({ length: n }, (_, i) => ai(i, `AI-${i + 1}`))
    const game = await createGame(page, seats)
    // 启动后运行中轮询为 1s，两个标签才可能落在同一 1.6s 窗口内并存。
    await page.request.post(`/api/v1/games/${game.id}/commands`, { data: { command: 'start' } })
    // 重新安装脚本前先移除旧路由，避免多次累积。
    await page.unroute(/\/api\/v1\/games(\/.*)?$/)
    await installScript(page, state)

    await page.goto('/')
    await expect(page.getByText('公共观战')).toBeVisible()

    // 连续两个座位各弹一个标签（利用 1.6s 窗口使其并存）。
    state.action = act(1, 2, 0, 'check')
    await expect(page.locator('.seat-slot[data-seat="0"] .action-popover')).toHaveCount(1, {
      timeout: 4000,
    })
    state.action = act(1, 3, 1, 'call', 0, 50)
    await expect(page.locator('.action-popover')).toHaveCount(2, { timeout: 4000 })

    // 中央公共牌/底池/街名不被标签压住。
    const popBoxes = await boxes(page, '.action-popover')
    for (const sel of ['.board', '.pot', '.street-label']) {
      for (const pb of popBoxes) {
        for (const ob of await boxes(page, sel)) {
          expect(overlaps(pb, ob), `n=${n} 标签压住 ${sel}`).toBeFalsy()
        }
      }
    }
    // 标签之间不重叠。
    for (let i = 0; i < popBoxes.length; i++) {
      for (let j = i + 1; j < popBoxes.length; j++) {
        expect(overlaps(popBoxes[i], popBoxes[j]), `n=${n} 标签互相重叠`).toBeFalsy()
      }
    }
    const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)
    expect(overflow).toBeLessThanOrEqual(1)

    await page.screenshot({ path: path.join(OUT, `count-${n}-popover.png`) })
    await page.unroute(/\/api\/v1\/games(\/.*)?$/)
  }
})

test('移动端 390：标签落在预留行、不压 .bet、不溢出', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 })
  const state: ScriptState = { action: act(1, 1, 0, 'call', 0, 300), handNo: 1 }
  await installScript(page, state)
  await startHumanGame(page, [
    { seat: 0, name: '我', stack: 10000, kind: 'human' },
    ai(1, 'AI-2'),
    ai(2, 'AI-3'),
  ])

  await page.goto('/')
  await expect(page.getByText('真人视角')).toBeVisible()

  // 弹两个标签（不同座位）。
  state.action = act(1, 2, 0, 'check')
  await expect(page.locator('.seat-slot[data-seat="0"] .action-popover')).toHaveCount(1, {
    timeout: 4000,
  })
  state.action = act(1, 3, 1, 'call', 0, 50)
  await expect(page.locator('.action-popover')).toHaveCount(2, { timeout: 4000 })

  // 标签不压座位卡内的 .bet，也不横向溢出。
  const popBoxes = await boxes(page, '.action-popover')
  for (const pb of popBoxes) {
    for (const bb of await boxes(page, '.seat .bet')) {
      expect(overlaps(pb, bb), '移动端标签压住 .bet').toBeFalsy()
    }
  }
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)
  expect(overflow).toBeLessThanOrEqual(1)

  await page.screenshot({ path: path.join(OUT, 'mobile-390-popover.png'), fullPage: false })
})
