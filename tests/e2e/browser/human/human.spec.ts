import { expect, test, type Page, type Response } from '@playwright/test'
import fs from 'node:fs'

// 所有 HTTP 均通过真实 Chromium 页面的原生 fetch 或真实 UI 发出。
// 不 route/fulfill、不 APIRequestContext、不读取 cookies()/storageState()/document.cookie。
const created: string[] = []
const evidence: { scenario: string; facts: Record<string, unknown> }[] = []

function record(scenario: string, facts: Record<string, unknown>) {
  evidence.push({ scenario, facts })
  fs.writeFileSync(process.env.HUMAN_EVIDENCE_FILE!, JSON.stringify(evidence, null, 2))
  console.log(`EVIDENCE ${scenario}: ${JSON.stringify(facts)}`)
}

async function http(page: Page, path: string, body?: unknown) {
  return page.evaluate(async ({ path, body }) => {
    const response = await fetch(path, {
      credentials: 'same-origin',
      ...(body === undefined ? {} : {
        method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body),
      }),
    })
    return { status: response.status, data: await response.json() }
  }, { path, body })
}

const field = (page: Page, name: string) => page.locator('.form-grid .field').filter({ hasText: name })
const playerPath = (id: string) => `/api/v1/games/${id}/player`

async function create(page: Page, hands = 1) {
  await page.goto('/')
  await expect(page.getByRole('button', { name: '创建牌局', exact: true })).toBeVisible()
  await page.getByLabel('真人座位', { exact: true }).selectOption('0')
  const rows = page.locator('.seat-row')
  await expect(rows).toHaveCount(2)
  await expect(rows.nth(0).locator('.f-model select')).toHaveCount(0)
  // mock 模式：AI 模型字段固定为「Mock（免费）」，无下拉、不可编辑。
  await expect(rows.nth(1).locator('.f-model select')).toHaveCount(0)
  await expect(rows.nth(1).locator('.f-model')).toContainText('Mock（免费）')
  await field(page, '小盲').locator('input').fill('5')
  await field(page, '大盲').locator('input').fill('10')
  await field(page, '最多手数').locator('input').fill(String(hands))
  const responsePromise = page.waitForResponse(r =>
    r.request().method() === 'POST' && new URL(r.url()).pathname === '/api/v1/games')
  await page.getByRole('button', { name: '创建牌局', exact: true }).click()
  const response = await responsePromise
  expect(response.status()).toBe(201)
  const data = await response.json()
  created.push(data.id)
  // 创建请求只含公开座位配置，绝不检查请求头。
  const body = response.request().postDataJSON()
  expect(body.seats[0].kind).toBe('human')
  expect(body.seats[0]).not.toHaveProperty('model_ref')
  expect(body.seats[1]).toMatchObject({ kind: 'ai', model_ref: 'mock' })
  expect(data.seats[0]).toMatchObject({ kind: 'human', model_ref: '' })
  expect(data.mode).toBe('mock')
  expect(data.live_calls_used).toBe(0)
  await expect(page.locator('.view-player')).toHaveText('真人视角')
  return { id: data.id as string, response }
}

async function waitHuman(page: Page, id: string) {
  let view: any
  await expect.poll(async () => {
    const response = await http(page, playerPath(id))
    expect(response.status).toBe(200)
    view = response.data
    return view.hand?.waiting_for
  // 两次 AI mock 步进后才可能进入下一个真人等待点（run_human 默认 1.5s；run_turn 显式 800ms）。
  }, { timeout: 6_500, intervals: [25, 50, 100] }).toBe('human_action')
  expect(view.hand.actor).toBe(0)
  expect(view.player.hole_cards).toHaveLength(2)
  expect(view.player.legal_actions).not.toBeNull()
  const remaining = Date.parse(view.player.action_deadline) - Date.parse(view.player.server_time)
  expect(remaining).toBeGreaterThan(0)
  expect(remaining).toBeLessThanOrEqual(5_000)
  await expect(page.locator('.hand-dock[data-turn="me"] .action-panel.is-turn')).toBeVisible()
  await expect(page.locator('.hand-dock .countdown')).toHaveText(/[1-5]s/)
  return view
}

async function start(page: Page, id: string) {
  await page.getByRole('button', { name: '开始', exact: true }).click()
  return waitHuman(page, id)
}

// 只从 Set-Cookie 检查名称/属性布尔值；不提取、保存、断言或输出 name=value 的凭据部分。
// Playwright header 数据留在本地测试进程，永不进入报告或 trace。
async function cookieAttributes(response: Response, id: string) {
  return {
    issued: /^poker_player=/.test((await response.headerValue('set-cookie')) ?? ''),
    httpOnly: /;\s*HttpOnly(?:;|$)/i.test((await response.headerValue('set-cookie')) ?? ''),
    strict: /;\s*SameSite=Strict(?:;|$)/i.test((await response.headerValue('set-cookie')) ?? ''),
    gamePath: ((await response.headerValue('set-cookie')) ?? '').includes(`; Path=/api/v1/games/${id};`),
  }
}

function assertPublic(view: any) {
  expect(view.player).toBeNull()
  function inspect(value: unknown) {
    if (Array.isArray(value)) value.forEach(inspect)
    else if (value && typeof value === 'object') {
      for (const [key, child] of Object.entries(value)) {
        // input_tokens/output_tokens 是契约允许的公开用量统计，不是会话凭据。
        expect(/hole|deck|snapshot|session|password|secret|credential|api_?key|access_?token|auth/i.test(key),
          `公共接口不得包含私有字段 ${key}`).toBe(false)
        inspect(child)
      }
    }
  }
  inspect(view)
}

test.afterEach(async ({ page }) => {
  for (const id of created.splice(0)) {
    const view = await http(page, `/api/v1/games/${id}`)
    expect(view.status).toBe(200)
    if (!['stopped', 'completed', 'failed'].includes(view.data.status)) {
      const stopped = await http(page, `/api/v1/games/${id}/commands`, { command: 'stop' })
      expect(stopped.status).toBe(200)
    }
  }
})

test('human_identity_privacy_refresh · a/b/c/d/g/h 真实创建、会话、私牌、匿名与刷新', async ({ page, browser }) => {
  // AI 模型必填在真实 HTTP 校验，失败创建不发真人凭据。
  await page.goto('/')
  const invalid = await http(page, '/api/v1/games', {
    seats: [{ seat: 0, name: 'human', stack: 10000, kind: 'human' },
      { seat: 1, name: 'ai', stack: 10000, kind: 'ai' }],
    button: 0, small_blind: 5, big_blind: 10, max_hands: 1, mode: 'mock', max_live_calls: 0,
  })
  expect(invalid.status).toBe(400)
  expect(invalid.data.error.code).toBe('INVALID_INPUT')
  const { id, response } = await create(page)
  const attrs = await cookieAttributes(response, id)
  expect(attrs).toEqual({ issued: true, httpOnly: true, strict: true, gamePath: true })
  record('a/b', { create: 201, human_model_omitted: true, ai_model_required: invalid.status,
    cookie: { name: 'poker_player', ...attrs }, provider_calls: 0 })
  const view = await start(page, id)
  await expect(page.locator('.hole-open .card')).toHaveCount(2)
  await expect(page.locator('.seat:not(.is-human) .hole-open')).toHaveCount(0)
  const publicView = await http(page, `/api/v1/games/${id}`)
  expect(publicView.status).toBe(200)
  assertPublic(publicView.data)
  const unfinished = await http(page, `/api/v1/games/${id}/hands/1`)
  // 原有 GetHand 对正在进行的手牌返回 409 INVALID_STATE；不存在的手牌才是 404。
  expect(unfinished.status).toBe(409)
  expect(unfinished.data.error.code).toBe('INVALID_STATE')
  const anonymous = await browser.newContext({ baseURL: process.env.POKER_BASE_URL })
  try {
    const spectator = await anonymous.newPage()
    await spectator.goto('/')
    const denied = await http(spectator, playerPath(id))
    expect(denied.status).toBe(403)
    expect(denied.data.error.code).toBe('PLAYER_NOT_AUTHORIZED')
    await expect(spectator.locator('.view-public')).toHaveText('公共观战')
    await expect(spectator.locator('.hole-open')).toHaveCount(0)
    record('c/g', { authenticated_player: 200, own_hole_count: view.player.hole_cards.length,
      legal_actions: true, anonymous_player: denied.status, anonymous_code: denied.data.error.code,
      public_player: publicView.data.player, public_private_keys: 0, unfinished_hand: unfinished.status,
      ai_open_holes: 0, anonymous_open_holes: 0 })
  } finally {
    await anonymous.close()
  }
  // 暂停冻结等待点后刷新，避免把页面加载耗时误当成身份丢失；仍由真实 UI 控制。
  await page.getByRole('button', { name: '暂停', exact: true }).click()
  await expect(page.locator('.statusbar').first()).toContainText('已暂停')
  await page.reload()
  await expect(page.locator('.view-player')).toHaveText('真人视角')
  await expect(page.locator('.hole-open .card')).toHaveCount(2)
  const recovered = await http(page, playerPath(id))
  expect(recovered.status).toBe(200)
  expect(recovered.data.player.hole_cards).toEqual(view.player.hole_cards)
  record('h', { page_reload: true, recovered_player: 200, same_own_holes: true, cookies_manually_added: 0 })
})

async function clickAction(page: Page, id: string, name: string | RegExp, kind: string, to?: number) {
  const before = (await http(page, playerPath(id))).data
  const promise = page.waitForResponse(r => r.request().method() === 'POST' &&
    new URL(r.url()).pathname === `/api/v1/games/${id}/action`)
  await page.getByRole('button', { name, exact: typeof name === 'string' }).click()
  const response = await promise
  expect(response.status()).toBe(200)
  const request = response.request().postDataJSON()
  expect(request).toMatchObject({ expected_revision: before.revision, kind })
  if (to !== undefined) expect(request.to).toBe(to)
  const after = await response.json()
  expect(after.revision).toBeGreaterThan(before.revision)
  expect(after.last_action).toMatchObject({ kind, source: 'human', error_code: null })
  record(`e/${kind}`, { status: 200, request, before_revision: before.revision,
    after_revision: after.revision, source: after.last_action.source })
  return after
}

test('human_actions · d/e/g 真实 UI call/check/raise_to/fold、400、409 与完成揭牌', async ({ page }) => {
  const { id } = await create(page)
  const first = await start(page, id)
  const panel = page.locator('.action-panel')
  await expect(panel.getByRole('button', { name: '弃牌', exact: true })).toBeEnabled()
  await expect(panel.getByRole('button', { name: /^跟注/ })).toBeEnabled()
  await expect(panel.getByRole('button', { name: '加注', exact: true })).toBeEnabled()
  await expect(panel.getByRole('button', { name: '全押', exact: true })).toBeEnabled()
  expect(first.player.legal_actions.can_check).toBe(false)
  const illegal = await http(page, `/api/v1/games/${id}/action`, {
    expected_revision: first.revision, kind: 'check', to: 0,
  })
  expect(illegal.status).toBe(400)
  expect(illegal.data.error.code).toBe('ILLEGAL_ACTION')
  const unchanged = (await http(page, playerPath(id))).data
  expect(unchanged.revision).toBe(first.revision)
  expect(unchanged.player.action_deadline).toBe(first.player.action_deadline)
  const stale = await http(page, `/api/v1/games/${id}/action`, {
    expected_revision: first.revision - 1, kind: 'fold', to: 0,
  })
  expect(stale.status).toBe(409)
  expect(stale.data.error.code).toBe('STALE_ACTION')
  record('e/errors', { illegal: illegal.status, illegal_code: illegal.data.error.code,
    unchanged_revision: true, unchanged_deadline: true, stale: stale.status, stale_code: stale.data.error.code })
  await clickAction(page, id, /^跟注/, 'call')
  const flop = await waitHuman(page, id)
  expect(flop.hand.street).toBe('flop')
  expect(flop.player.legal_actions.can_check).toBe(true)
  await expect(panel.getByRole('button', { name: '过牌', exact: true })).toBeEnabled()
  record('d', { waiting_for: 'human_action', fold: true, call: true, check: true,
    raise: true, all_in: true, countdown_max_seconds: 5, buttons_follow_legal_actions: true })
  await clickAction(page, id, '过牌', 'check')
  const turn = await waitHuman(page, id)
  expect(turn.hand.street).toBe('turn')
  const to = turn.player.legal_actions.min_raise_to
  await page.getByLabel('加注到（本街累计）').fill(String(to))
  await clickAction(page, id, '加注', 'raise_to', to)
  const river = await waitHuman(page, id)
  expect(river.hand.street).toBe('river')
  await clickAction(page, id, '弃牌', 'fold')
  await expect.poll(async () => (await http(page, `/api/v1/games/${id}`)).data.status,
    { intervals: [25, 50, 100] }).toBe('completed')
  const complete = await http(page, `/api/v1/games/${id}/hands/1`)
  expect(complete.status).toBe(200)
  expect(complete.data.hole_cards).toHaveLength(2)
  expect(complete.data.hole_cards.every((hole: any) => hole.cards.length === 2)).toBe(true)
  record('g/completed', { completed_hand: 200, revealed_seats: complete.data.hole_cards.length,
    revealed_cards_per_seat: 2 })
})

test('human_timeout · f 5 秒无人操作，即使可 check 也由服务端 fallback 弃牌并结算', async ({ page }) => {
  const { id } = await create(page)
  await start(page, id)
  await clickAction(page, id, /^跟注/, 'call')
  const waiting = await waitHuman(page, id)
  expect(waiting.player.legal_actions.can_check).toBe(true)
  let postedActions = 0
  page.on('request', r => {
    if (r.method() === 'POST' && new URL(r.url()).pathname.endsWith('/action')) postedActions++
  })
  const startAt = Date.now()
  let final: any
  await expect.poll(async () => {
    final = (await http(page, `/api/v1/games/${id}`)).data
    return final.status
  }, { timeout: 10_000, intervals: [50, 100, 200] }).toBe('completed')
  expect(final.last_action).toMatchObject({ seat: 0, kind: 'fold', source: 'fallback',
    error_code: 'HUMAN_ACTION_TIMEOUT', input_tokens: null, output_tokens: null })
  expect(final.hands_completed).toBe(1)
  expect(final.revision).toBeGreaterThan(waiting.revision)
  expect(final.live_calls_used).toBe(0)
  expect(postedActions).toBe(0)
  const hand = await http(page, `/api/v1/games/${id}/hands/1`)
  expect(hand.status).toBe(200)
  record('f', { configured_timeout_seconds: 5,
    remaining_at_observation_ms: Date.parse(waiting.player.action_deadline) - Date.parse(waiting.player.server_time),
    elapsed_ms: Date.now() - startAt, can_check_before_timeout: true, action: final.last_action,
    browser_timeout_action_posts: postedActions, status: final.status, hands_completed: final.hands_completed,
    before_revision: waiting.revision, after_revision: final.revision, completed_hand: hand.status,
    live_calls_used: final.live_calls_used })
})

test('human_running_refresh_next_hand · f/h 运行中刷新恢复、超时后继续下一手', async ({ page }) => {
  const { id } = await create(page, 2)
  const first = await start(page, id)
  let actionPosts = 0
  page.on('request', request => {
    if (request.method() === 'POST' && new URL(request.url()).pathname.endsWith('/action')) actionPosts++
  })
  await page.reload()
  await expect(page.locator('.view-player')).toHaveText('真人视角')
  await expect(page.locator('.hole-open .card')).toHaveCount(2)
  const recovered = await http(page, playerPath(id))
  expect(recovered.status).toBe(200)
  expect(recovered.data.status).toBe('running')
  expect(recovered.data.player.hole_cards).toEqual(first.player.hole_cards)
  expect(recovered.data.player.action_deadline).toBe(first.player.action_deadline)
  record('h/running', { status: 'running', player_http: 200, same_own_holes: true,
    same_deadline: true, manually_added_cookies: 0 })
  let next: any
  await expect.poll(async () => {
    next = (await http(page, playerPath(id))).data
    return next.hand?.number
  }, { timeout: 10_000, intervals: [50, 100, 200] }).toBe(2)
  expect(next.status).toBe('running')
  expect(next.hands_completed).toBe(1)
  expect(next.live_calls_used).toBe(0)
  expect(actionPosts).toBe(0)
  const completed = await http(page, `/api/v1/games/${id}/hands/1`)
  expect(completed.status).toBe(200)
  const publicView = await http(page, `/api/v1/games/${id}`)
  assertPublic(publicView.data)
  record('f/next-hand', { status: next.status, next_hand_number: next.hand.number,
    hands_completed: next.hands_completed, first_hand_persisted: completed.status,
    browser_action_posts: actionPosts, live_calls_used: next.live_calls_used,
    public_player_in_next_hand: publicView.data.player })
})
