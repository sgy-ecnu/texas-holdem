import { test, expect, type Page } from '@playwright/test'
import fs from 'node:fs'
import path from 'node:path'

// 截图落在被忽略目录（wrapper 通过 SCREEN_DIR 注入），不进入版本库。
const SCREEN_DIR =
  process.env.SCREEN_DIR || path.resolve(process.cwd(), '../../web/node_modules/.cache/browser-verify')
fs.mkdirSync(SCREEN_DIR, { recursive: true })

async function shot(page: Page, name: string) {
  // 禁用动画后再截全页图，避免入场/发牌动画导致全页拼接错位。
  await page.screenshot({ path: path.join(SCREEN_DIR, name), fullPage: true, animations: 'disabled' })
}

const statusChip = (page: Page) => page.locator('.statusbar .chip-badge').first()
const fieldByLabel = (page: Page, text: string) =>
  page.locator('.form-grid .field').filter({ hasText: text })
const seatRows = (page: Page) => page.locator('.seat-row')

async function pickModels(page: Page, seats: number) {
  const res = await page.request.get('/api/v1/models')
  const { models } = (await res.json()) as { models: { ref: string }[] }
  const refs = models.map((m) => m.ref)
  expect(refs.length).toBeGreaterThanOrEqual(2)
  const rows = seatRows(page)
  for (let i = 0; i < seats; i++) {
    await rows.nth(i).locator('.f-model select').selectOption(refs[i % refs.length])
  }
  return refs
}

async function configureAndCreate(
  page: Page,
  opts: { seats?: number; sb?: string; bb?: string; maxHands: string; stacks?: string },
) {
  const seats = opts.seats ?? 2
  const count = fieldByLabel(page, '座位数').locator('select')
  if (seats !== 2) {
    await count.selectOption(String(seats))
    await expect(seatRows(page)).toHaveCount(seats)
  }
  await fieldByLabel(page, '小盲').locator('input').fill(opts.sb ?? '5')
  await fieldByLabel(page, '大盲').locator('input').fill(opts.bb ?? '10')
  await fieldByLabel(page, '最多手数').locator('input').fill(opts.maxHands)
  if (opts.stacks) {
    for (let i = 0; i < seats; i++) {
      await seatRows(page).nth(i).locator('.f-stack input').fill(opts.stacks)
    }
  }
  const [response] = await Promise.all([
    page.waitForResponse(
      (r) => r.request().method() === 'POST' && /\/api\/v1\/games$/.test(new URL(r.url()).pathname),
    ),
    page.getByRole('button', { name: '创建牌局' }).click(),
  ])
  const created = (await response.json()) as { id: string }
  await expect(statusChip(page)).toContainText('已创建', { timeout: 20000 })
  return created.id
}

// 每个用例结束后清理自己创建的牌局，避免下一个用例撞 409（仅清理由本用例产生的非终态局）。
test.afterEach(async ({ request }) => {
  try {
    const res = await request.get('/api/v1/games/current')
    if (!res.ok()) return
    const game = await res.json()
    if (!['stopped', 'completed', 'failed'].includes(game.status)) {
      await request.post(`/api/v1/games/${game.id}/commands`, { data: { command: 'stop' } })
    }
  } catch {
    // 清理失败不掩盖主断言结果。
  }
})

test('scenario 1 · 配置 2/9 人且每座选择不同模型', async ({ page }) => {
  await page.goto('/')
  await expect(page.locator('.section-title').first()).toHaveText('新建牌局')
  await expect(page.locator('.masthead .brand')).toContainText('观战台')

  // 默认 2 人
  await expect(seatRows(page)).toHaveCount(2)
  await expect(seatRows(page).nth(0).locator('.f-name input')).toHaveValue('AI-1')
  await expect(seatRows(page).nth(1).locator('.f-name input')).toHaveValue('AI-2')
  await expect(fieldByLabel(page, '小盲').locator('input')).toHaveValue('50')
  await expect(fieldByLabel(page, '大盲').locator('input')).toHaveValue('100')
  await expect(fieldByLabel(page, '最多手数').locator('input')).toHaveValue('100')

  // 扩到 9 人
  const count = fieldByLabel(page, '座位数').locator('select')
  await count.selectOption('9')
  await expect(seatRows(page)).toHaveCount(9)
  await expect(seatRows(page).nth(8).locator('.f-name input')).toHaveValue('AI-9')

  // 每座选不同模型
  const refs = await pickModels(page, 9)
  const values: string[] = []
  for (let i = 0; i < 9; i++) {
    values.push(await seatRows(page).nth(i).locator('.f-model select').inputValue())
  }
  expect(new Set(values).size).toBe(Math.min(9, refs.length))
  await shot(page, 's1-config-9seats.png')

  // 收回 2 人
  await count.selectOption('2')
  await expect(seatRows(page)).toHaveCount(2)
})

test('scenario 2 · 创建/开始至 max_hands 并核对结束详情数值数组', async ({ page, request }) => {
  await page.goto('/')
  const gameId = await configureAndCreate(page, { seats: 2, maxHands: '3' })
  await page.getByRole('button', { name: '开始' }).click()
  await expect(statusChip(page)).toContainText('已完成', { timeout: 45000 })

  await expect(page.locator('.statusbar .metric').filter({ hasText: '手数' })).toContainText('3 / 3')

  // 打开完成手牌详情（UI 操作）
  await page.getByRole('button', { name: '手牌详情' }).click()
  const modal = page.locator('.overlay .modal')
  await expect(modal).toBeVisible()
  await expect(modal.locator('.modal-head .title')).toHaveText('手牌详情')
  await expect(modal.locator('.detail-title', { hasText: '底池分配' })).toBeVisible()
  await expect(modal.locator('.detail-title', { hasText: '最终筹码' })).toBeVisible()
  const holes = modal.locator('.hole-item')
  await expect(holes.first()).toBeVisible({ timeout: 15000 })
  expect(await holes.count()).toBeGreaterThanOrEqual(2)
  await expect(holes.first().locator('.card')).toHaveCount(2)
  await shot(page, 's2-completed-detail.png')

  // API 交叉核对数值数组（终态局用 id 直取；current 只返回非终态局）
  const cur = await (await request.get(`/api/v1/games/${gameId}`)).json()
  expect(cur.status).toBe('completed')
  expect(cur.hands_completed).toBe(3)
  const h3 = await (await request.get(`/api/v1/games/${gameId}/hands/3`)).json()
  expect(h3.hand_no).toBe(3)
  expect(Array.isArray(h3.pots)).toBe(true)
  expect(Array.isArray(h3.payouts)).toBe(true)
  expect(Array.isArray(h3.final_stacks)).toBe(true)
  const total = (h3.final_stacks as { stack: number }[]).reduce((s, p) => s + p.stack, 0)
  expect(total).toBe(20000)
})

test('scenario 3 · 暂停 / 刷新找 current / 恢复 / 停止', async ({ page }) => {
  await page.goto('/')
  await configureAndCreate(page, { seats: 2, maxHands: '100000', stacks: '100000000' })
  await page.getByRole('button', { name: '开始' }).click()
  await expect(statusChip(page)).toContainText('进行中', { timeout: 20000 })

  await page.getByRole('button', { name: '暂停' }).click()
  await expect(statusChip(page)).toContainText('已暂停', { timeout: 20000 })

  // 真实浏览器刷新：应由 /games/current 找回该非终态局
  await page.reload()
  await expect(page.locator('.statusbar')).toBeVisible({ timeout: 20000 })
  await expect(statusChip(page)).toContainText('已暂停')
  await expect(page.getByRole('button', { name: '恢复' })).toBeVisible()
  await shot(page, 's3-paused-after-reload.png')

  await page.getByRole('button', { name: '恢复' }).click()
  await expect(statusChip(page)).toContainText('进行中', { timeout: 20000 })

  await page.getByRole('button', { name: '停止' }).click()
  await expect(statusChip(page)).toContainText('已停止', { timeout: 20000 })
  await expect(page.getByRole('button', { name: '新建牌局' })).toBeVisible()
  await shot(page, 's3-stopped.png')
})

test('scenario 4 · live 禁用与费用确认门槛', async ({ page, request }) => {
  // 硬前提：服务未授权付费模型（否则本用例的 UI 分支断言不成立）。
  const health = await (await request.get('/api/v1/health')).json()
  expect(health.allow_paid_models).toBe(false)

  await page.goto('/')
  const liveBtn = page.locator('.segmented button').filter({ hasText: 'Live' })
  const mockBtn = page.locator('.segmented button').filter({ hasText: 'Mock' })
  await expect(liveBtn).toBeDisabled()
  await expect(mockBtn).toHaveAttribute('aria-pressed', 'true')
  await expect(page.locator('.masthead .health')).toContainText('未授权')
  await expect(page.locator('.inline-note')).toContainText('未授权付费模型')
  await shot(page, 's4-live-disabled.png')

  // 费用确认 UI 分支：用拦截的 health 呈现 live 可用态（纯 UI，不创建牌局、不发生付费调用）。
  await page.route('**/api/v1/health', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        mysql: 'available',
        redis: 'available',
        storage_degraded: false,
        allow_paid_models: true,
        max_live_calls: 1000,
      }),
    }),
  )
  let createPosts = 0
  page.on('request', (req) => {
    if (req.method() === 'POST' && /\/api\/v1\/games$/.test(new URL(req.url()).pathname)) createPosts++
  })
  await page.reload()
  await expect(liveBtn).toBeEnabled()
  await liveBtn.click()
  await expect(page.locator('.checkline input[type="checkbox"]')).toBeVisible()
  await expect(page.locator('.checkline')).toContainText('确认')
  await page.getByRole('button', { name: '创建牌局' }).click()
  await expect(page.getByRole('alert')).toContainText('勾选确认')
  expect(createPosts).toBe(0) // 未确认时客户端拦截，未发创建请求
  await shot(page, 's4-live-confirm.png')
})

test('scenario 6 · 运行中无底牌 / 完成后详情可揭示', async ({ page, request }) => {
  await page.goto('/')
  await configureAndCreate(page, { seats: 2, maxHands: '100000', stacks: '100000000' })
  await page.getByRole('button', { name: '开始' }).click()
  await expect(statusChip(page)).toContainText('进行中', { timeout: 20000 })

  // 手中：公共牌可显示，但绝不出现底牌；仅牌背占位，且无详情弹层。
  await expect(page.locator('.mini-card').first()).toBeVisible({ timeout: 20000 })
  expect(await page.locator('.overlay').count()).toBe(0)
  expect(await page.locator('.hole-item').count()).toBe(0)
  await shot(page, 's6-running-nohole.png')

  // 等到至少完成一手，暂停后打开详情，底牌才可揭示。
  await expect
    .poll(async () => {
      const r = await request.get('/api/v1/games/current')
      if (!r.ok()) return -1
      return (await r.json()).hands_completed as number
    }, { timeout: 30000 })
    .toBeGreaterThanOrEqual(1)

  await page.getByRole('button', { name: '暂停' }).click()
  await expect(statusChip(page)).toContainText('已暂停', { timeout: 20000 })

  await page.getByRole('button', { name: '手牌详情' }).click()
  const modal = page.locator('.overlay .modal')
  await expect(modal).toBeVisible()
  const holes = modal.locator('.hole-item')
  await expect(holes.first()).toBeVisible({ timeout: 15000 })
  expect(await holes.count()).toBeGreaterThanOrEqual(2)
  await expect(holes.first().locator('.card')).toHaveCount(2)
  await shot(page, 's6-detail-reveal.png')

  await page.locator('.modal-head button', { hasText: '关闭' }).click()
  await page.getByRole('button', { name: '停止' }).click()
  await expect(statusChip(page)).toContainText('已停止', { timeout: 20000 })
})

test.describe('scenario 5 · 手机宽 390 可操作无遮挡', () => {
  test.use({ viewport: { width: 390, height: 844 } })

  test('390 宽配置与运行控制可用', async ({ page }) => {
    await page.goto('/')
    await expect(page.locator('.section-title').first()).toHaveText('新建牌局')
    // 无横向溢出
    const overflow = await page.evaluate(
      () => document.documentElement.scrollWidth - window.innerWidth,
    )
    expect(overflow).toBeLessThanOrEqual(1)
    await shot(page, 's5-mobile-config.png')

    // 长局：mock 很快，max_hands 必须足够大，才能稳定验证暂停/停止控制。
    await configureAndCreate(page, { seats: 2, maxHands: '100000', stacks: '100000000' })
    await page.getByRole('button', { name: '开始' }).click()
    await expect(statusChip(page)).toContainText('进行中', { timeout: 20000 })

    // 控制按钮在视口内且可点击
    const pauseBtn = page.getByRole('button', { name: '暂停' })
    await pauseBtn.scrollIntoViewIfNeeded()
    const box = await pauseBtn.boundingBox()
    expect(box).not.toBeNull()
    expect(box!.x).toBeGreaterThanOrEqual(0)
    expect(box!.x + box!.width).toBeLessThanOrEqual(391)
    await pauseBtn.click()
    await expect(statusChip(page)).toContainText('已暂停', { timeout: 20000 })
    await shot(page, 's5-mobile-controls.png')

    await page.getByRole('button', { name: '停止' }).click()
    await expect(statusChip(page)).toContainText('已停止', { timeout: 20000 })
  })
})
