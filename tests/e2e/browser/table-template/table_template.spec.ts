import {
  expect,
  test,
  type APIRequestContext,
  type BrowserContext,
  type Page,
} from '@playwright/test'

// 「牌桌配置模板」阶段 3 前端真实链路验收（真实 Go 二进制 + Chromium）。
// 场景：
//   1) UI 创建模板（含真人座位）+ 列表卡片展示人数/盲注/真人位置/mock/真实模型数量；
//   2) 编辑模板 PUT 全量替换 + 版本冲突 409 提示刷新；
//   3) 清空 = config=null 空态（确认文案区别于删除）+ 未配置不可开局 + 空态可「填入默认配置」；
//   4) 删除模板（独立确认文案）+ 删除后 404；
//   5) 从模板一键开局（创建 + start 两步）；
//   6) 单活冲突 409（离开观战页不等于 stop）；
//   7) start 失败重试：保留已创建 game_id、不重新创建；
//   8) 多浏览器 context：模板列表不泄漏 owner、Cookie 隔离。
// 不读取任何凭据；不 commit/push。

const baseURL = process.env.POKER_BASE_URL
if (!baseURL) throw new Error('POKER_BASE_URL_REQUIRED')

interface Seat {
  seat: number
  name: string
  stack: number
  kind: 'ai' | 'human'
  model_ref?: string
}
interface Config {
  seats: Seat[]
  button: number
  small_blind: number
  big_blind: number
  max_hands: number
  mode: 'mock' | 'live'
  max_live_calls: number
}
interface TableDTO {
  id: string
  name: string
  revision: number
  config: Config | null
}

function mockConfig(overrides: Partial<Config> = {}): Config {
  return {
    seats: [
      { seat: 0, name: 'A', stack: 1000, kind: 'ai', model_ref: 'mock' },
      { seat: 1, name: 'B', stack: 1000, kind: 'ai', model_ref: 'mock' },
    ],
    button: 0,
    small_blind: 5,
    big_blind: 10,
    max_hands: 1,
    mode: 'mock',
    max_live_calls: 0,
    ...overrides,
  }
}

function humanConfig(): Config {
  return {
    seats: [
      { seat: 0, name: '真人', stack: 1000, kind: 'human' },
      { seat: 1, name: 'A', stack: 1000, kind: 'ai', model_ref: 'mock' },
    ],
    button: 0,
    small_blind: 5,
    big_blind: 10,
    max_hands: 1,
    mode: 'mock',
    max_live_calls: 0,
  }
}

const createdTables: string[] = []

async function apiCreateTable(
  request: APIRequestContext,
  name: string,
  config: Config | null,
): Promise<TableDTO> {
  const res = await request.post('/api/v1/tables', { data: { name, config } })
  expect(res.status(), await res.text()).toBe(201)
  const dto = (await res.json()) as TableDTO
  createdTables.push(dto.id)
  return dto
}

async function apiDeleteTable(request: APIRequestContext, id: string): Promise<void> {
  const get = await request.get(`/api/v1/tables/${id}`)
  if (!get.ok()) return
  const dto = (await get.json()) as TableDTO
  await request.delete(`/api/v1/tables/${id}`, { data: { expected_revision: dto.revision } })
}

async function stopCurrent(request: APIRequestContext): Promise<void> {
  const res = await request.get('/api/v1/games/current')
  if (!res.ok()) return
  const game = (await res.json()) as { id: string; status: string }
  if (!['stopped', 'completed', 'failed'].includes(game.status)) {
    await request.post(`/api/v1/games/${game.id}/commands`, { data: { command: 'stop' } })
  }
}

let nameSeq = 0
function uniqueName(prefix: string): string {
  nameSeq += 1
  return `e2e-${prefix}-${Date.now()}-${nameSeq}`
}

const field = (page: Page, label: string) =>
  page.locator('.form-grid .field').filter({ hasText: label })
const card = (page: Page, name: string) =>
  page.locator('.table-card').filter({ hasText: name })

test.afterEach(async ({ request }) => {
  await stopCurrent(request)
  for (const id of createdTables.splice(0)) {
    await apiDeleteTable(request, id)
  }
})

test('场景1 UI 创建模板（含真人座位）并列表展示人数/盲注/真人位置/模型数量', async ({ page, request }) => {
  const name = uniqueName('create')
  await page.goto('/')
  // 默认入口是牌桌列表。
  await expect(page.getByRole('heading', { name: '牌桌模板' })).toBeVisible()

  await page.getByRole('button', { name: '新建模板' }).click()
  await expect(page.getByLabel('模板名称')).toBeVisible()
  // config=null 空态：提示尚未配置，提供「填入默认配置」。
  await expect(page.locator('.empty-config')).toContainText('尚未配置')
  await page.getByLabel('模板名称').fill(name)
  await page.getByRole('button', { name: '填入默认配置' }).click()
  await expect(page.locator('.seat-row')).toHaveCount(2)

  await page.getByLabel('真人座位').selectOption('0')
  await field(page, '小盲').locator('input').fill('5')
  await field(page, '大盲').locator('input').fill('10')
  await field(page, '最多手数').locator('input').fill('3')

  const pending = page.waitForResponse(
    (r) => r.request().method() === 'POST' && new URL(r.url()).pathname === '/api/v1/tables',
  )
  await page.getByRole('button', { name: '创建模板' }).click()
  const response = await pending
  expect(response.status()).toBe(201)
  const created = (await response.json()) as TableDTO
  createdTables.push(created.id)
  expect(created.config?.seats.find((s) => s.kind === 'human')?.seat).toBe(0)

  // 回到列表，卡片展示摘要。
  const c = card(page, name)
  await expect(c).toBeVisible()
  await expect(c.locator('.table-card-meta')).toContainText('座位 0')
  await expect(c.locator('.table-card-meta')).toContainText('0 真实 / 1 mock')

  // 列表接口不暴露 owner/会话字段。
  const listText = await (await request.get('/api/v1/tables')).text()
  expect(listText).not.toContain('owner')
  expect(listText).not.toContain('user_id')
})

test('场景2 编辑模板 PUT 全量替换，版本冲突 409 提示刷新', async ({ page, request }) => {
  const name = uniqueName('edit')
  const t = await apiCreateTable(request, name, mockConfig())

  await page.goto('/')
  await card(page, name).getByRole('button', { name: '编辑' }).click()
  await expect(page.getByLabel('模板名称')).toHaveValue(name)

  await field(page, '小盲').locator('input').fill('20')
  await field(page, '大盲').locator('input').fill('40')

  const save = page.waitForResponse(
    (r) => r.request().method() === 'PUT' && new URL(r.url()).pathname === `/api/v1/tables/${t.id}`,
  )
  await page.getByRole('button', { name: '保存修改' }).click()
  expect((await save).status()).toBe(200)

  await expect(card(page, name).locator('.table-card-meta')).toContainText('20/40')

  // 模拟他处先改：进入编辑页后外部 bump revision，保存时 CAS 冲突。
  await card(page, name).getByRole('button', { name: '编辑' }).click()
  await expect(page.getByLabel('模板名称')).toHaveValue(name)
  const external = await request.put(`/api/v1/tables/${t.id}`, {
    data: { expected_revision: t.revision + 1, name, config: mockConfig({ small_blind: 30, big_blind: 60 }) },
  })
  expect(external.status()).toBe(200)

  const conflict = page.waitForResponse(
    (r) => r.request().method() === 'PUT' && new URL(r.url()).pathname === `/api/v1/tables/${t.id}`,
  )
  await page.getByRole('button', { name: '保存修改' }).click()
  expect((await conflict).status()).toBe(409)
  await expect(page.locator('.banner-title', { hasText: '版本冲突' })).toBeVisible()
  await expect(page.getByRole('button', { name: '刷新' })).toBeVisible()
})

test('场景3 清空 = 未配置空态（确认文案区别于删除），未配置不可开局', async ({ page, request }) => {
  const name = uniqueName('clear')
  await apiCreateTable(request, name, mockConfig())

  await page.goto('/')
  const c = card(page, name)
  await c.getByRole('button', { name: '清空配置' }).click()
  // 清空确认文案明确「未配置空态、名称保留」。
  const clearConfirm = page.getByRole('alertdialog', { name: '确认清空配置' })
  await expect(clearConfirm).toContainText('未配置')
  await expect(clearConfirm).toContainText('名称保留')

  const cleared = page.waitForResponse(
    (r) => r.request().method() === 'POST' && /\/api\/v1\/tables\/[^/]+\/clear$/.test(new URL(r.url()).pathname),
  )
  await clearConfirm.getByRole('button', { name: '确认清空' }).click()
  expect((await cleared).status()).toBe(200)

  await expect(c.getByText('未配置', { exact: true })).toBeVisible()
  await expect(c.getByRole('button', { name: '开一局' })).toBeDisabled()

  // 空态编辑：显示「填入默认配置」，可恢复出座位。
  await c.getByRole('button', { name: '编辑' }).click()
  await expect(page.locator('.empty-config')).toContainText('尚未配置')
  await page.getByRole('button', { name: '填入默认配置' }).click()
  await expect(page.locator('.seat-row')).toHaveCount(2)
})

test('场景4 删除模板（独立确认文案）后列表移除且 GET 404', async ({ page, request }) => {
  const name = uniqueName('delete')
  const t = await apiCreateTable(request, name, mockConfig())
  createdTables.splice(createdTables.indexOf(t.id), 1) // 本场景自行删除，避免 afterEach 重复

  await page.goto('/')
  const c = card(page, name)
  await c.getByRole('button', { name: '删除' }).click()
  const delConfirm = page.getByRole('alertdialog', { name: '确认删除模板' })
  await expect(delConfirm).toContainText('不可恢复')
  await expect(delConfirm).not.toContainText('名称保留')

  const del = page.waitForResponse(
    (r) => r.request().method() === 'DELETE' && new URL(r.url()).pathname === `/api/v1/tables/${t.id}`,
  )
  await delConfirm.getByRole('button', { name: '确认删除' }).click()
  expect((await del).status()).toBe(204)
  await expect(c).toHaveCount(0)

  const after = await request.get(`/api/v1/tables/${t.id}`)
  expect(after.status()).toBe(404)
})

test('场景5 从模板一键开局（创建 + start 两步）', async ({ page, request }) => {
  const name = uniqueName('open')
  const t = await apiCreateTable(request, name, mockConfig({ max_hands: 1 }))

  await page.goto('/')
  await card(page, name).getByRole('button', { name: '开一局' }).click()

  // 进入当前牌局观战。
  await expect(page.getByText('公共观战')).toBeVisible({ timeout: 15_000 })

  // 后端确实用该模板开局（新局拥有模板座位数）。
  const current = await request.get('/api/v1/games/current')
  expect(current.ok()).toBeTruthy()
  const game = (await current.json()) as { id: string; seats: unknown[] }
  expect(game.seats.length).toBe(t.config!.seats.length)
})

test('场景6 单活冲突：已有进行中牌局时再开局 409，且离开观战页不等于 stop', async ({ page, request }) => {
  const first = uniqueName('active1')
  const second = uniqueName('active2')
  await apiCreateTable(request, first, mockConfig({ max_hands: 100 }))
  await apiCreateTable(request, second, mockConfig({ max_hands: 1 }))

  await page.goto('/')
  await card(page, first).getByRole('button', { name: '开一局' }).click()
  await expect(page.getByText('公共观战')).toBeVisible({ timeout: 15_000 })

  // 离开观战返回列表：牌局仍在（不 stop）。
  await page.getByRole('button', { name: '牌桌列表' }).click()
  await expect(page.getByRole('heading', { name: '牌桌模板' })).toBeVisible()
  const still = await request.get('/api/v1/games/current')
  expect(still.ok()).toBeTruthy()

  // 再开第二个模板：单活约束 409，显示错误且停留在列表。
  const conflict = page.waitForResponse(
    (r) => r.request().method() === 'POST' && /\/api\/v1\/tables\/[^/]+\/games$/.test(new URL(r.url()).pathname),
  )
  await card(page, second).getByRole('button', { name: '开一局' }).click()
  expect((await conflict).status()).toBe(409)
  await expect(page.locator('.banner.error')).toContainText('状态已变化')
  await expect(page.getByRole('heading', { name: '牌桌模板' })).toBeVisible()
})

test('场景7 start 失败：保留已创建 game_id 并允许重试 start，不重新创建', async ({ page, request }) => {
  const name = uniqueName('retry')
  await apiCreateTable(request, name, mockConfig({ max_hands: 1 }))

  let openCalls = 0
  page.on('request', (r) => {
    if (r.method() === 'POST' && /\/api\/v1\/tables\/[^/]+\/games$/.test(new URL(r.url()).pathname)) {
      openCalls += 1
    }
  })

  // 只拦截第一条 start 命令，注入 500；后续放行。
  const commandsRoute = /\/api\/v1\/games\/[^/]+\/commands$/
  let startCalls = 0
  await page.route(commandsRoute, async (route) => {
    if (route.request().method() === 'POST') {
      startCalls += 1
      if (startCalls === 1) {
        await route.fulfill({
          status: 500,
          contentType: 'application/json',
          body: JSON.stringify({ error: { code: 'STORAGE', message: 'injected start failure' } }),
        })
        return
      }
    }
    await route.continue()
  })

  await page.goto('/')
  await card(page, name).getByRole('button', { name: '开一局' }).click()

  // 已创建但启动失败：进入该牌局并提示可重试。
  await expect(page.getByText('启动失败', { exact: false })).toBeVisible({ timeout: 15_000 })
  await expect(page.getByRole('button', { name: '开始' })).toBeVisible()
  expect(openCalls).toBe(1)

  // 放行后重试 start：同一牌局进入运行/结束，不再创建新局。
  await page.unroute(commandsRoute)
  await page.getByRole('button', { name: '开始' }).click()
  await expect(page.getByRole('button', { name: '开始' })).toHaveCount(0, { timeout: 15_000 })
  expect(openCalls).toBe(1)
})

test('场景8 多浏览器 context：模板列表不泄漏 owner、真人 Cookie 隔离', async ({
  browser,
  request,
}) => {
  const name = uniqueName('multi')
  const t = await apiCreateTable(request, name, humanConfig())

  const ctxA: BrowserContext = await browser.newContext({ baseURL })
  const ctxB: BrowserContext = await browser.newContext({ baseURL })
  try {
    const pageA = await ctxA.newPage()
    const pageB = await ctxB.newPage()
    await pageA.goto('/')
    await pageB.goto('/')

    // B 能看到模板（模板全局共享），但公共响应不含 owner 信息。
    await expect(card(pageB, name)).toBeVisible()
    const listText = await (await ctxB.request.get('/api/v1/tables')).text()
    expect(listText).not.toContain('owner')
    const detailText = await (await ctxB.request.get(`/api/v1/tables/${t.id}`)).text()
    expect(detailText).not.toContain('owner')

    // A 从真人模板开局：A 持有游戏级 Cookie，可读 /player；B 被拒 403。
    await card(pageA, name).getByRole('button', { name: '开一局' }).click()
    await expect(pageA.getByText('真人视角')).toBeVisible({ timeout: 15_000 })

    const current = await ctxA.request.get('/api/v1/games/current')
    expect(current.ok()).toBeTruthy()
    const game = (await current.json()) as { id: string }

    const playerA = await ctxA.request.get(`/api/v1/games/${game.id}/player`)
    expect(playerA.status()).toBe(200)
    const crossB = await ctxB.request.get(`/api/v1/games/${game.id}/player`)
    expect(crossB.status()).toBe(403)
  } finally {
    await ctxA.close()
    await ctxB.close()
    await stopCurrent(request)
  }
})
