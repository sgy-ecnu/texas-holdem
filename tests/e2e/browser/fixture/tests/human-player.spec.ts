import { expect, test } from '@playwright/test'

// fixture 交互验收：验证 Web 端真人上桌的配置、视角、斗地主式轮转体验（当前 actor 提示、
// 底部倒计时/进度条、行动历史滚动、底部操作区）与观战隔离。
// 明确：走本地 fixture，不是真实 Go 链路。

test.beforeEach(async ({ request }) => {
  await request.post('/__fixture/reset')
})

async function createHumanGame(page: import('@playwright/test').Page) {
  await page.goto('/')
  await expect(page.getByRole('heading', { name: '新建牌局' })).toBeVisible()
  await page.getByLabel('真人座位').selectOption('0')
  await page.getByRole('button', { name: '创建牌局' }).click()
  await expect(page.getByText('真人视角')).toBeVisible()
}

test('全 AI 创建走公共观战，不出现真人行动面板', async ({ page }) => {
  await page.goto('/')
  await expect(page.getByRole('heading', { name: '新建牌局' })).toBeVisible()
  await page.getByRole('button', { name: '创建牌局' }).click()
  await expect(page.getByText('公共观战')).toBeVisible()
  await expect(page.getByText('真人视角')).toHaveCount(0)
  await expect(page.locator('.action-panel')).toHaveCount(0)
  // 底部行动区仍在，但无手牌时处于 idle，无真人操作按钮。
  await expect(page.locator('.hand-dock[data-turn="idle"]')).toBeVisible()
  await expect(page.locator('.turn-badge')).toHaveCount(0)
})

test('真人座位：配置页隐藏模型下拉，创建后进入真人视角', async ({ page }) => {
  await page.goto('/')
  await expect(page.getByRole('heading', { name: '新建牌局' })).toBeVisible()

  await page.getByLabel('真人座位').selectOption('0')
  // 真人座位不要求模型：显示身份说明而非模型下拉
  await expect(page.getByText('真人 · 创建浏览器持有权限，无需模型')).toBeVisible()
  await expect(page.locator('.seat-row.is-human .f-model select')).toHaveCount(0)

  await page.getByRole('button', { name: '创建牌局' }).click()
  await expect(page.getByText('真人视角')).toBeVisible()
})

test('轮到真人：座位光圈与「轮到我」、底部倒计时进度条与操作按钮组', async ({ page }) => {
  await createHumanGame(page)
  await page.getByRole('button', { name: '开始' }).click()

  // 轮询 /player 后进入真人行动点
  await expect(page.locator('.hand-dock[data-turn="me"]')).toBeVisible({ timeout: 15_000 })
  await expect(page.locator('.dock-state')).toHaveText('轮到你出牌')
  await expect(page.locator('.countdown')).toBeVisible()
  await expect(page.locator('.countdown')).toHaveText(/[0-9]/)
  // 底部进度条填充存在
  await expect(page.locator('.dock-timer-fill')).toBeVisible()

  // 操作区集中在底部，按钮齐全
  const panel = page.locator('.action-panel.is-turn')
  await expect(panel).toBeVisible()
  await expect(panel.getByRole('button', { name: '弃牌' })).toBeVisible()
  await expect(panel.getByRole('button', { name: /跟注/ })).toBeVisible()
  await expect(panel.getByRole('button', { name: '全押' })).toBeVisible()

  // 当前 actor 座位醒目：光圈 + 「轮到我」标签；非当前参与座位弱化
  await expect(page.locator('.seat.is-actor')).toHaveCount(1)
  await expect(page.locator('.seat.is-actor .turn-badge')).toHaveText('轮到我')
  await expect(page.locator('.seat.is-dimmed')).toHaveCount(1)

  // 本人两张底牌面朝上渲染在真人座位
  await expect(page.locator('.seat.is-human .hole-open .card')).toHaveCount(2)

  await page.getByRole('button', { name: '弃牌' }).click()
  // 提交后服务端清空等待点，行动面板收起
  await expect(page.locator('.action-panel.is-turn')).toHaveCount(0, { timeout: 15_000 })
})

test('AI 轮转：底部显示「思考中…」与不确定进度，行动历史高亮新动作', async ({ page }) => {
  await createHumanGame(page)
  await page.getByRole('button', { name: '开始' }).click()
  await expect(page.locator('.hand-dock[data-turn="me"]')).toBeVisible({ timeout: 15_000 })

  await page.getByRole('button', { name: '弃牌' }).click()

  // 交给 AI：底部转为思考态
  await expect(page.locator('.hand-dock[data-turn="ai"]')).toBeVisible({ timeout: 15_000 })
  await expect(page.locator('.dock-state')).toHaveText('思考中…')
  await expect(page.locator('.dock-timer.is-thinking')).toBeVisible()
  // AI 座位成为当前 actor，显示「思考中」标签
  await expect(page.locator('.seat.is-actor .turn-badge')).toHaveText('思考中')

  // 行动历史滚动：出现真人弃牌记录，且最新一条高亮
  await expect(page.locator('.action-feed .feed-row')).toHaveCount(1)
  await expect(page.locator('.action-feed .feed-row').last()).toContainText('弃牌')
  await expect(page.locator('.action-feed .feed-row.is-new')).toContainText('弃牌')
})

test('无 Cookie 浏览器：人类牌局仅公共观战，看不到真人底牌与行动面板', async ({ page, request }) => {
  const res = await request.post('/api/v1/games', {
    data: {
      seats: [
        { seat: 0, name: '我', stack: 10000, kind: 'human' },
        { seat: 1, name: 'AI-2', stack: 10000, kind: 'ai', model_ref: 'mock' },
      ],
      button: 0,
      small_blind: 50,
      big_blind: 100,
      max_hands: 100,
      mode: 'mock',
      max_live_calls: 0,
    },
  })
  expect(res.ok()).toBeTruthy()
  const created = await res.json()
  expect(created.human_seat).toBe(0)

  await page.goto('/')
  // 尝试 /player 得 403 后退回公共观战
  await expect(page.getByText('公共观战')).toBeVisible({ timeout: 15_000 })
  await expect(page.getByText('真人视角')).toHaveCount(0)
  await expect(page.locator('.seat.is-human .hole-open')).toHaveCount(0)
  await expect(page.locator('.action-panel')).toHaveCount(0)
})

test('移动端 390 宽：轮转提示与底部操作区可点无遮挡', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 })
  await createHumanGame(page)
  await page.getByRole('button', { name: '开始' }).click()

  await expect(page.locator('.hand-dock[data-turn="me"]')).toBeVisible({ timeout: 15_000 })
  await expect(page.locator('.dock-state')).toHaveText('轮到你出牌')

  // 底部操作区落在视口内，可点
  const fold = page.locator('.action-panel').getByRole('button', { name: '弃牌' })
  await expect(fold).toBeVisible()
  const box = await fold.boundingBox()
  expect(box).not.toBeNull()
  expect(box!.x).toBeGreaterThanOrEqual(0)
  expect(box!.x + box!.width).toBeLessThanOrEqual(390)

  await expect(page.locator('.seat.is-actor .turn-badge')).toHaveText('轮到我')
  await fold.click()
  await expect(page.locator('.action-panel.is-turn')).toHaveCount(0, { timeout: 15_000 })
})
