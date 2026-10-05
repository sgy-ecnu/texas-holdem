import { expect, test } from '@playwright/test'
import { mkdir } from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

// 动作特效 V2 验收（fixture，非真实 Go 链路）：
//  1) 四种动作统一 scale(1.8)、总时长 2200ms；
//  2) 精确时间轴：0–80ms 放大淡入、80–320ms 保持 1.8、320–920ms 600ms 缓慢缩回、920–1980 停留、1980–2200 淡出；
//  3) 四色只区分动作（尺寸统一）；
//  4) 展开方向 transform-origin：左半 right bottom / 右半 left bottom / 顶部 center bottom / 真人 left center；
//  5) 移动端仍保持 scale 1.8（不降级）；
//  6) reduced-motion 静态高对比展示约 2200ms。
// 用 page.route 脚本化 last_action（不改 fixture-server.mjs）。首个观察到的 last_action 只作基线不弹。

const HERE = path.dirname(fileURLToPath(import.meta.url))
const ROOT = path.resolve(HERE, '../../../../..')
const OUT = process.env.SCREEN_DIR ? path.resolve(process.env.SCREEN_DIR) : path.join(ROOT, 'docs/action-popover')

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

// 4 座：human0→底部，seat1→左(L)、seat2→顶(T)、seat3→右(R)，一次覆盖四种展开方向。
async function fourSeatGame(page: import('@playwright/test').Page) {
  return createGame(page, [
    { seat: 0, name: '我', stack: 10000, kind: 'human' },
    ai(1, 'AI-2'),
    ai(2, 'AI-3'),
    ai(3, 'AI-4'),
  ])
}

async function popMetrics(page: import('@playwright/test').Page, seat: number) {
  return page.evaluate((s) => {
    const el = document.querySelector(`.seat-slot[data-seat="${s}"] .action-popover`) as HTMLElement
    if (!el) return null
    const cs = getComputedStyle(el)
    const origin = cs.transformOrigin.split(' ').map((v) => parseFloat(v))
    return {
      className: el.className,
      scaleVar: parseFloat(cs.getPropertyValue('--pop-scale')),
      originX: origin[0],
      originY: origin[1],
      width: el.offsetWidth,
      height: el.offsetHeight,
      animationName: cs.animationName,
      animationDuration: cs.animationDuration,
      color: cs.color,
      backgroundColor: cs.backgroundColor,
      borderColor: cs.borderTopColor,
      opacity: cs.opacity,
      transform: cs.transform,
      boxShadow: cs.boxShadow,
    }
  }, seat)
}

// 用 Web Animations API 把动画定格到指定毫秒，返回该帧的 scale（matrix a 分量）。
async function scaleAt(page: import('@playwright/test').Page, seat: number, ms: number) {
  return page.evaluate(
    ([s, t]) => {
      const el = document.querySelector(`.seat-slot[data-seat="${s}"] .action-popover`) as HTMLElement
      if (!el) return null
      const anims = el.getAnimations()
      const anim = anims[anims.length - 1]
      if (!anim) return null
      anim.pause()
      anim.currentTime = t as number
      const m = new DOMMatrixReadOnly(getComputedStyle(el).transform)
      return m.a
    },
    [seat, ms] as const,
  )
}

async function keyframesInfo(page: import('@playwright/test').Page) {
  return page.evaluate(() => {
    for (const sheet of Array.from(document.styleSheets)) {
      let rules: CSSRuleList
      try {
        rules = sheet.cssRules
      } catch {
        continue
      }
      for (const rule of Array.from(rules)) {
        if (rule instanceof CSSKeyframesRule && rule.name === 'action-pop') {
          const text = Array.from(rule.cssRules)
            .map((k) => (k as CSSKeyframeRule).style.cssText)
            .join(';')
          return {
            usesVar: text.includes('var(--pop-scale)'),
            hasUndershoot: text.includes('scale(0.97)') || text.includes('scale(0.9)'),
          }
        }
      }
    }
    return { usesVar: false, hasUndershoot: false }
  })
}

test.beforeEach(async ({ request }) => {
  await request.post('/__fixture/reset')
  await mkdir(OUT, { recursive: true })
})

test('桌面：四种动作统一 scale 1.8 / 2200ms / 四色', async ({ page }) => {
  await page.setViewportSize({ width: 1536, height: 1024 })
  // 首个动作只作基线；随后切换才触发弹出。
  const state: ScriptState = { action: act(1, 1, 3, 'check'), handNo: 1 }
  await installScript(page, state)
  await fourSeatGame(page)

  await page.goto('/')
  await expect(page.getByText('真人视角')).toBeVisible()
  await page.waitForTimeout(500)

  const kf = await keyframesInfo(page)
  expect(kf.usesVar, 'keyframes 使用 --pop-scale').toBeTruthy()
  expect(kf.hasUndershoot, 'keyframes 不应含回弹下冲').toBeFalsy()

  // fold（左半座位 seat1）
  state.action = act(1, 2, 1, 'fold')
  const foldEl = page.locator('.seat-slot[data-seat="1"] .action-popover')
  await expect(foldEl).toHaveText('弃牌', { timeout: 4000 })
  const fold = await popMetrics(page, 1)
  expect(fold!.className).toContain('is-fold')
  expect(fold!.className).toContain('is-above-left')
  expect(fold!.scaleVar).toBeCloseTo(1.8, 2)
  expect(fold!.animationName).toBe('action-pop')
  expect(fold!.animationDuration).toBe('2.2s')
  expect(fold!.backgroundColor).toBe('rgb(48, 56, 61)')
  expect(fold!.color).toBe('rgb(243, 245, 247)')
  expect(fold!.borderColor).toBe('rgb(170, 180, 188)')

  // check（顶部座位 seat2）
  state.action = act(1, 3, 2, 'check')
  const checkEl = page.locator('.seat-slot[data-seat="2"] .action-popover')
  await expect(checkEl).toHaveText('过牌', { timeout: 4000 })
  const check = await popMetrics(page, 2)
  expect(check!.className).toContain('is-check')
  expect(check!.className).toContain('is-above-center')
  expect(check!.scaleVar).toBeCloseTo(1.8, 2)
  expect(check!.animationDuration).toBe('2.2s')
  expect(check!.backgroundColor).toBe('rgb(24, 61, 70)')
  expect(check!.color).toBe('rgb(224, 248, 255)')

  // call（右半座位 seat3）
  state.action = act(1, 4, 3, 'call', 0, 100)
  const callEl = page.locator('.seat-slot[data-seat="3"] .action-popover')
  await expect(callEl).toHaveText('跟注 100', { timeout: 4000 })
  const call = await popMetrics(page, 3)
  expect(call!.className).toContain('is-call')
  expect(call!.className).toContain('is-above-right')
  expect(call!.scaleVar).toBeCloseTo(1.8, 2)
  expect(call!.backgroundColor).toBe('rgb(89, 66, 22)')
  expect(call!.color).toBe('rgb(255, 240, 184)')

  // raise_to（真人座位 seat0 → 右侧）
  state.action = act(1, 5, 0, 'raise_to', 300)
  const raiseEl = page.locator('.seat-slot[data-seat="0"] .action-popover')
  await expect(raiseEl).toHaveText('加注到 300', { timeout: 4000 })
  const raise = await popMetrics(page, 0)
  expect(raise!.className).toContain('is-raise')
  expect(raise!.className).toContain('is-right')
  expect(raise!.scaleVar).toBeCloseTo(1.8, 2)
  expect(raise!.backgroundColor).toBe('rgb(242, 205, 103)')
  expect(raise!.color).toBe('rgb(36, 27, 7)')

  await page.screenshot({ path: path.join(OUT, 'v2-desktop-four-actions.png') })
})

test('桌面：精确时间轴（80/320/620/920/1980ms）', async ({ page }) => {
  await page.setViewportSize({ width: 1536, height: 1024 })
  const state: ScriptState = { action: act(1, 1, 3, 'check'), handNo: 1 }
  await installScript(page, state)
  await fourSeatGame(page)

  await page.goto('/')
  await expect(page.getByText('真人视角')).toBeVisible()
  await page.waitForTimeout(500)

  state.action = act(1, 2, 1, 'fold')
  const el = page.locator('.seat-slot[data-seat="1"] .action-popover')
  await expect(el).toHaveText('弃牌', { timeout: 4000 })

  // 0–80ms 淡入阶段 scale 仍 1.8；80–320ms 保持 1.8。
  expect(await scaleAt(page, 1, 40), '40ms').toBeCloseTo(1.8, 2)
  expect(await scaleAt(page, 1, 80), '80ms').toBeCloseTo(1.8, 2)
  expect(await scaleAt(page, 1, 200), '200ms').toBeCloseTo(1.8, 2)
  expect(await scaleAt(page, 1, 320), '320ms').toBeCloseTo(1.8, 2)
  // 320–920ms 缓慢缩回：620ms 处于 1.0 与 1.8 之间。
  const mid = await scaleAt(page, 1, 620)
  expect(mid, '620ms 应处于缩回过程中').toBeGreaterThan(1.02)
  expect(mid, '620ms 应尚未缩回到 1.0').toBeLessThan(1.8)
  // 920ms 到正常尺寸并停留到 1980ms。
  expect(await scaleAt(page, 1, 920), '920ms').toBeCloseTo(1, 2)
  expect(await scaleAt(page, 1, 1500), '1500ms').toBeCloseTo(1, 2)
  expect(await scaleAt(page, 1, 1980), '1980ms').toBeCloseTo(1, 2)

  await page.screenshot({ path: path.join(OUT, 'v2-desktop-timeline.png') })
})

test('桌面：展开方向 transform-origin（左半/右半/顶部/真人）', async ({ page }) => {
  await page.setViewportSize({ width: 1536, height: 1024 })
  const state: ScriptState = { action: act(1, 1, 3, 'check'), handNo: 1 }
  await installScript(page, state)
  await fourSeatGame(page)

  await page.goto('/')
  await expect(page.getByText('真人视角')).toBeVisible()
  await page.waitForTimeout(500)

  // 左半 seat1（正左座位 L）：贴卡片右侧、垂直居中，尖角朝左指向卡片；origin left center。
  state.action = act(1, 2, 1, 'check')
  await expect(page.locator('.seat-slot[data-seat="1"] .action-popover')).toHaveText('过牌', { timeout: 4000 })
  const left = await popMetrics(page, 1)
  expect(left!.className).toContain('anchor-right')
  expect(left!.originX).toBeCloseTo(0, 0)
  expect(left!.originY).toBeCloseTo(left!.height / 2, 0)

  // 顶部 seat2：标签贴卡片下沿居中、尖角朝上（向下朝桌内展开，避免压上方常驻下注条），origin center top。
  state.action = act(1, 3, 2, 'check')
  await expect(page.locator('.seat-slot[data-seat="2"] .action-popover')).toHaveText('过牌', { timeout: 4000 })
  const top = await popMetrics(page, 2)
  expect(top!.className).toContain('anchor-below')
  expect(top!.originX).toBeCloseTo(top!.width / 2, 0)
  expect(top!.originY).toBeCloseTo(0, 0)

  // 右半 seat3（正右座位 R）：贴卡片左侧、垂直居中，尖角朝右指向卡片；origin right center。
  state.action = act(1, 4, 3, 'check')
  await expect(page.locator('.seat-slot[data-seat="3"] .action-popover')).toHaveText('过牌', { timeout: 4000 })
  const right = await popMetrics(page, 3)
  expect(right!.className).toContain('anchor-left')
  expect(right!.originX).toBeCloseTo(right!.width, 0)
  expect(right!.originY).toBeCloseTo(right!.height / 2, 0)

  // 真人 seat0（底部）：贴卡片右侧、垂直居中，尖角朝左指向卡片；origin left center。
  state.action = act(1, 5, 0, 'raise_to', 300)
  await expect(page.locator('.seat-slot[data-seat="0"] .action-popover')).toHaveText('加注到 300', { timeout: 4000 })
  const human = await popMetrics(page, 0)
  expect(human!.className).toContain('anchor-right')
  expect(human!.originX).toBeCloseTo(0, 0)
  expect(human!.originY).toBeCloseTo(human!.height / 2, 0)

  await page.screenshot({ path: path.join(OUT, 'v2-desktop-origins.png') })
})

test('移动端 390：scale 仍 1.8、不横向溢出', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 })
  const state: ScriptState = { action: act(1, 1, 3, 'check'), handNo: 1 }
  await installScript(page, state)
  await fourSeatGame(page)

  await page.goto('/')
  await expect(page.getByText('真人视角')).toBeVisible()
  await page.waitForTimeout(500)

  state.action = act(1, 2, 1, 'raise_to', 123456)
  const el = page.locator('.seat-slot[data-seat="1"] .action-popover')
  await expect(el).toHaveText('加注到 123,456', { timeout: 4000 })
  const m = await popMetrics(page, 1)
  // 移动端取消 scale 降级：仍是 1.8。
  expect(m!.scaleVar).toBeCloseTo(1.8, 2)
  expect(m!.animationDuration).toBe('2.2s')
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)
  expect(overflow).toBeLessThanOrEqual(1)

  await page.screenshot({ path: path.join(OUT, 'v2-mobile-raise.png'), fullPage: false })
})

test('reduced-motion：静态高对比展示约 2200ms', async ({ page }) => {
  await page.setViewportSize({ width: 1536, height: 1024 })
  await page.emulateMedia({ reducedMotion: 'reduce' })
  const state: ScriptState = { action: act(1, 1, 3, 'check'), handNo: 1 }
  await installScript(page, state)
  await fourSeatGame(page)

  await page.goto('/')
  await expect(page.getByText('真人视角')).toBeVisible()
  await page.waitForTimeout(500)

  state.action = act(1, 2, 1, 'raise_to', 900)
  const el = page.locator('.seat-slot[data-seat="1"] .action-popover')
  await expect(el).toHaveText('加注到 900', { timeout: 4000 })
  const m = await popMetrics(page, 1)
  expect(m!.animationName, 'reduced-motion 不施加动画').toBe('none')
  expect(m!.transform, 'reduced-motion 无缩放').toBe('none')
  expect(Number(m!.opacity)).toBeGreaterThan(0.9)
  expect(m!.className).toContain('is-raise')
  expect(m!.backgroundColor).toBe('rgb(242, 205, 103)')

  await page.waitForTimeout(1900)
  await expect(el, 'reduced-motion 静态可见至约 2200ms').toHaveCount(1)
  await page.screenshot({ path: path.join(OUT, 'v2-reduced-motion.png') })
  await expect(el).toHaveCount(0, { timeout: 2000 })
})
