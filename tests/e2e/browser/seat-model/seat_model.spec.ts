import { expect, test, type APIRequestContext, type BrowserContext, type Page } from '@playwright/test'

// 「按座位选模型」真实链路验收（真实 Go 二进制 + 受控本地 provider server）：
//   1) mock 模式 AI 模型字段固定「Mock（免费）」、不可编辑、提交 model_ref="mock"；
//   2) live 全 mock：可开局，且真实 provider 出站请求数为 0（免费场景）；
//   3) live 混合座位：仅真实模型座位出站，LiveCallsUsed 增加；
//   4) 多浏览器 context：Cookie 隔离（他人 403）+ 各自配置独立。
// 受控 provider 只统计出站请求数，不是真实模型；不读取任何凭据。

const providerURL = process.env.PROVIDER_URL
if (!providerURL) throw new Error('PROVIDER_URL_REQUIRED')

async function providerCount(request: APIRequestContext): Promise<number> {
  const res = await request.get(`${providerURL}/__count`)
  expect(res.ok()).toBeTruthy()
  return ((await res.json()) as { count: number }).count
}

async function resetProvider(request: APIRequestContext): Promise<void> {
  await request.post(`${providerURL}/__reset`)
}

const field = (page: Page, label: string) =>
  page.locator('.form-grid .field').filter({ hasText: label })
const seatRows = (page: Page) => page.locator('.seat-row')

async function createGame(page: Page): Promise<Record<string, unknown>> {
  const pending = page.waitForResponse(
    (r) => r.request().method() === 'POST' && /\/api\/v1\/games$/.test(new URL(r.url()).pathname),
  )
  await page.getByRole('button', { name: '创建牌局', exact: true }).click()
  const response = await pending
  expect(response.status()).toBe(201)
  return response.json()
}

async function stopCurrent(request: APIRequestContext): Promise<void> {
  const res = await request.get('/api/v1/games/current')
  if (!res.ok()) return
  const game = (await res.json()) as { id: string; status: string }
  if (!['stopped', 'completed', 'failed'].includes(game.status)) {
    await request.post(`/api/v1/games/${game.id}/commands`, { data: { command: 'stop' } })
  }
}

async function waitStatus(
  request: APIRequestContext,
  id: string,
  status: string,
  timeout = 30_000,
): Promise<Record<string, unknown>> {
  const deadline = Date.now() + timeout
  let game: Record<string, unknown> = {}
  while (Date.now() < deadline) {
    const res = await request.get(`/api/v1/games/${id}`)
    if (res.ok()) {
      game = (await res.json()) as Record<string, unknown>
      if (game.status === status) return game
    }
    await new Promise((r) => setTimeout(r, 250))
  }
  throw new Error(`game ${id} did not reach ${status}: ${JSON.stringify(game)}`)
}

test.afterEach(async ({ request }) => {
  await stopCurrent(request)
})

test('mock 模式：模型字段固定不可改，全 mock 局真实 provider 出站为 0', async ({ page, request }) => {
  await resetProvider(request)
  await page.goto('/')
  await expect(page.getByRole('button', { name: '创建牌局', exact: true })).toBeVisible()

  // AI 座位模型字段固定显示「Mock（免费）」，无下拉、不可编辑。
  await expect(seatRows(page)).toHaveCount(2)
  for (const row of await seatRows(page).all()) {
    await expect(row.locator('.f-model select')).toHaveCount(0)
    await expect(row.locator('.f-model')).toContainText('Mock（免费）')
  }

  await field(page, '小盲').locator('input').fill('5')
  await field(page, '大盲').locator('input').fill('10')
  await field(page, '最多手数').locator('input').fill('1')
  const game = await createGame(page)
  expect(game.mode).toBe('mock')
  for (const seat of game.seats as Array<Record<string, unknown>>) {
    expect(seat.model_ref).toBe('mock')
  }

  await page.getByRole('button', { name: '开始', exact: true }).click()
  const done = await waitStatus(request, game.id as string, 'completed')
  expect(done.live_calls_used).toBe(0)
  expect(await providerCount(request)).toBe(0)
})

test('live 全 mock：可开局且不产生真实模型调用', async ({ page, request }) => {
  await resetProvider(request)
  await page.goto('/')
  await page.locator('.segmented button').filter({ hasText: 'Live' }).click()
  // 全 mock 的 live 配置：明确提示不产生真实调用，不要求费用确认。
  await expect(page.locator('.inline-note').first()).toContainText('不产生真实模型调用')
  await expect(page.locator('.checkline')).toHaveCount(0)

  await field(page, '小盲').locator('input').fill('5')
  await field(page, '大盲').locator('input').fill('10')
  await field(page, '最多手数').locator('input').fill('1')
  const game = await createGame(page)
  expect(game.mode).toBe('live')
  for (const seat of game.seats as Array<Record<string, unknown>>) {
    expect(seat.model_ref).toBe('mock')
  }

  await page.getByRole('button', { name: '开始', exact: true }).click()
  const done = await waitStatus(request, game.id as string, 'completed')
  expect(done.live_calls_used).toBe(0)
  expect(await providerCount(request)).toBe(0)
})

test('live 混合座位：仅真实模型座位出站并消耗额度', async ({ page, request }) => {
  await resetProvider(request)
  await page.goto('/')
  await page.locator('.segmented button').filter({ hasText: 'Live' }).click()

  // 座位 1 选择真实模型；费用确认门槛出现。
  await seatRows(page).nth(1).locator('.f-model select').selectOption('local/real')
  await expect(page.locator('.checkline input[type="checkbox"]')).toBeVisible()
  await page.locator('.checkline input[type="checkbox"]').check()

  await field(page, '小盲').locator('input').fill('5')
  await field(page, '大盲').locator('input').fill('10')
  await field(page, '最多手数').locator('input').fill('1')
  const game = await createGame(page)
  const seats = game.seats as Array<Record<string, unknown>>
  const byModel = seats.map((s) => s.model_ref)
  expect(byModel).toContain('local/real')
  expect(byModel).toContain('mock')

  await page.getByRole('button', { name: '开始', exact: true }).click()
  // 真实模型座位被轮到后才发出请求；出站计数 > 0 且额度被消耗。
  await expect.poll(() => providerCount(request), { timeout: 30_000 }).toBeGreaterThan(0)
  const done = await waitStatus(request, game.id as string, 'completed')
  expect(done.live_calls_used as number).toBeGreaterThan(0)
})

test('多浏览器 context：Cookie 隔离且各自配置独立', async ({ browser, request }) => {
  const baseURL = process.env.POKER_BASE_URL
  const ctxA: BrowserContext = await browser.newContext({ baseURL })
  const ctxB: BrowserContext = await browser.newContext({ baseURL })
  try {
    const pageA = await ctxA.newPage()
    const pageB = await ctxB.newPage()

    // 配置独立：A 保持 mock，B 切到 live；互不影响。
    await pageA.goto('/')
    await pageB.goto('/')
    await expect(
      pageA.locator('.segmented button').filter({ hasText: 'Mock' }),
    ).toHaveAttribute('aria-pressed', 'true')
    await pageB.locator('.segmented button').filter({ hasText: 'Live' }).click()
    await expect(
      pageB.locator('.segmented button').filter({ hasText: 'Live' }),
    ).toHaveAttribute('aria-pressed', 'true')
    await expect(
      pageA.locator('.segmented button').filter({ hasText: 'Mock' }),
    ).toHaveAttribute('aria-pressed', 'true')
    await pageB.locator('.segmented button').filter({ hasText: 'Mock' }).click()

    // A 创建真人局，持有游戏级 Cookie；B 无 Cookie 不得读取 A 的私牌视角。
    async function createHuman(page: Page): Promise<string> {
      await page.goto('/')
      await page.getByLabel('真人座位', { exact: true }).selectOption('0')
      await field(page, '小盲').locator('input').fill('5')
      await field(page, '大盲').locator('input').fill('10')
      await field(page, '最多手数').locator('input').fill('1')
      const game = await createGame(page)
      return game.id as string
    }
    const idA = await createHuman(pageA)
    const playerA = await ctxA.request.get(`/api/v1/games/${idA}/player`)
    expect(playerA.status()).toBe(200)
    const crossA = await ctxB.request.get(`/api/v1/games/${idA}/player`)
    expect(crossA.status()).toBe(403)

    await ctxA.request.post(`/api/v1/games/${idA}/commands`, { data: { command: 'stop' } })

    // B 创建自己的真人局：B 可读，A 被拒。
    const idB = await createHuman(pageB)
    const playerB = await ctxB.request.get(`/api/v1/games/${idB}/player`)
    expect(playerB.status()).toBe(200)
    const crossB = await ctxA.request.get(`/api/v1/games/${idB}/player`)
    expect(crossB.status()).toBe(403)
  } finally {
    await ctxA.close()
    await ctxB.close()
    await stopCurrent(request)
  }
})
