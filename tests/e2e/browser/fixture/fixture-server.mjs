// 真人上桌 Web 交互的本地 fixture 服务器（仅前端验证用，不是真实链路）。
// 职责：
//   - 静态服务 web/dist 构建产物；
//   - 用内存状态模拟契约 DTO 的 REST 行为（含游戏级 HttpOnly Cookie 与 403 语义）。
// 明确：本 fixture 不能作为「真实 Go 联调」完成证据。
import { createServer } from 'node:http'
import { readFile } from 'node:fs/promises'
import { randomBytes } from 'node:crypto'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const HERE = path.dirname(fileURLToPath(import.meta.url))
const DIST = path.resolve(HERE, '../../../../web/dist')
const PORT = Number(process.env.FIXTURE_PORT || 4181)

const MIME = {
  '.html': 'text/html; charset=utf-8',
  '.js': 'text/javascript; charset=utf-8',
  '.css': 'text/css; charset=utf-8',
  '.json': 'application/json; charset=utf-8',
  '.svg': 'image/svg+xml',
  '.ico': 'image/x-icon',
}

// ---- 内存牌局状态 ----
let game = null
let token = null
let deadline = null
// 动作序号：复刻后端 last_action 的 sequence 递增，供前端行动历史去重.
let seq = 0

function reset() {
  game = null
  token = null
  deadline = null
  seq = 0
}

function send(res, status, payload, headers = {}) {
  const body = payload === undefined ? '' : JSON.stringify(payload)
  res.writeHead(status, {
    'Content-Type': 'application/json; charset=utf-8',
    'Cache-Control': 'no-store',
    ...headers,
  })
  res.end(body)
}

function fail(res, status, code, message) {
  send(res, status, { error: { code, message } })
}

function readBody(req) {
  return new Promise((resolve) => {
    let raw = ''
    req.on('data', (c) => (raw += c))
    req.on('end', () => {
      try {
        resolve(raw ? JSON.parse(raw) : {})
      } catch {
        resolve(undefined)
      }
    })
  })
}

function cookieToken(req) {
  const header = req.headers.cookie || ''
  const parts = header.split(';').map((p) => p.trim())
  let found = null
  for (const p of parts) {
    const eq = p.indexOf('=')
    if (eq < 0) continue
    if (p.slice(0, eq) === 'poker_player') {
      if (found !== null) return '__duplicate__'
      found = p.slice(eq + 1)
    }
  }
  return found
}

function publicGame() {
  if (!game) return null
  return { ...game, player: null, hand: game.hand ? { ...game.hand, waiting_for: game.hand.waiting_for ?? null } : null }
}

function humanWaitActive() {
  return (
    game &&
    game.status === 'running' &&
    game.hand &&
    game.human_seat !== null &&
    game.hand.actor === game.human_seat &&
    game.hand.waiting_for === 'human_action'
  )
}

function playerGame() {
  const pub = publicGame()
  if (!pub) return null
  const wait = humanWaitActive()
  return {
    ...pub,
    player: {
      seat: game.human_seat,
      hand_no: game.hand ? game.hand.number : null,
      hole_cards: game.hand ? ['Ah', 'Kd'] : [],
      legal_actions: wait
        ? {
            can_fold: true,
            can_check: false,
            can_call: true,
            call_amount: 100,
            can_raise: true,
            min_raise_to: 200,
            max_raise_to: 10000,
            short_all_in_to: null,
          }
        : null,
      action_deadline: wait ? deadline : null,
      server_time: new Date().toISOString(),
    },
  }
}

function startHand() {
  game.status = 'running'
  game.revision += 1
  game.hand = {
    number: 1,
    street: 'preflop',
    button: 0,
    small_blind: 50,
    big_blind: 100,
    actor: game.human_seat,
    board: [],
    pot: 150,
    current_bet: 100,
    players: game.seats.map((s) => ({
      seat: s.seat,
      stack: s.stack,
      street_bet: 0,
      committed: 0,
      status: 'active',
    })),
    waiting_for: 'human_action',
  }
  deadline = new Date(Date.now() + 60_000).toISOString()
}

function createGame(body) {
  const seats = (body.seats || []).map((s, i) => ({
    seat: s.seat ?? i,
    name: s.name ?? `S${i}`,
    model_ref: s.kind === 'human' ? '' : (s.model_ref ?? 'mock'),
    stack: s.stack ?? 10000,
    kind: s.kind === 'human' ? 'human' : 'ai',
  }))
  const human = seats.find((s) => s.kind === 'human') || null
  const id = 'fixture-' + randomBytes(4).toString('hex')
  game = {
    id,
    revision: 1,
    status: 'created',
    reason: null,
    mode: body.mode === 'live' ? 'live' : 'mock',
    max_hands: body.max_hands ?? 100,
    hands_completed: 0,
    live_calls_used: 0,
    max_live_calls: body.max_live_calls ?? 0,
    storage_degraded: false,
    seats,
    hand: null,
    last_action: null,
    human_seat: human ? human.seat : null,
  }
  return human ? id : null
}

async function serveStatic(res, urlPath) {
  let rel = urlPath === '/' ? '/index.html' : urlPath
  // 无路由的 SPA：未知非资源路径一律回退 index.html。
  if (!path.extname(rel)) rel = '/index.html'
  const file = path.join(DIST, path.normalize(rel))
  if (!file.startsWith(DIST)) return send(res, 403, { error: { code: 'FORBIDDEN', message: 'x' } })
  try {
    const data = await readFile(file)
    res.writeHead(200, { 'Content-Type': MIME[path.extname(file)] || 'application/octet-stream' })
    res.end(data)
  } catch {
    try {
      const html = await readFile(path.join(DIST, 'index.html'))
      res.writeHead(200, { 'Content-Type': MIME['.html'] })
      res.end(html)
    } catch {
      send(res, 404, { error: { code: 'NOT_FOUND', message: 'dist missing' } })
    }
  }
}

const server = createServer(async (req, res) => {
  const url = new URL(req.url, `http://127.0.0.1:${PORT}`)
  const p = url.pathname

  if (p === '/__fixture/reset' && req.method === 'POST') {
    reset()
    return send(res, 200, { ok: true })
  }

  if (p === '/api/v1/health') {
    return send(res, 200, {
      mysql: 'available',
      redis: 'available',
      storage_degraded: false,
      allow_paid_models: false,
      max_live_calls: 0,
    })
  }

  if (p === '/api/v1/models') {
    return send(res, 200, {
      models: [{ ref: 'mock', label: 'Mock', provider: 'mock', availability: 'mock', error_code: null }],
    })
  }

  if (p === '/api/v1/games/current') {
    if (!game) return fail(res, 404, 'NOT_FOUND', '没有进行中的牌局')
    return send(res, 200, publicGame())
  }

  if (p === '/api/v1/games' && req.method === 'POST') {
    const body = await readBody(req)
    if (body === undefined) return fail(res, 400, 'INVALID_INPUT', '请求体非法')
    const humanId = createGame(body)
    const headers = {}
    if (humanId) {
      token = randomBytes(16).toString('hex')
      headers['Set-Cookie'] =
        `poker_player=${token}; Path=/api/v1/games/${humanId}; HttpOnly; SameSite=Strict`
    }
    return send(res, 200, publicGame(), headers)
  }

  const gameMatch = p.match(/^\/api\/v1\/games\/([^/]+)$/)
  if (gameMatch && req.method === 'GET') {
    if (!game || game.id !== gameMatch[1]) return fail(res, 404, 'NOT_FOUND', '牌局不存在')
    return send(res, 200, publicGame())
  }

  const playerMatch = p.match(/^\/api\/v1\/games\/([^/]+)\/player$/)
  if (playerMatch && req.method === 'GET') {
    if (!game || game.id !== playerMatch[1]) return fail(res, 404, 'NOT_FOUND', '牌局不存在')
    if (game.human_seat === null || token === null || cookieToken(req) !== token) {
      return fail(res, 403, 'PLAYER_NOT_AUTHORIZED', '不是本局真人浏览器')
    }
    return send(res, 200, playerGame())
  }

  const actionMatch = p.match(/^\/api\/v1\/games\/([^/]+)\/action$/)
  if (actionMatch && req.method === 'POST') {
    if (!game || game.id !== actionMatch[1]) return fail(res, 404, 'NOT_FOUND', '牌局不存在')
    if (game.human_seat === null || token === null || cookieToken(req) !== token) {
      return fail(res, 403, 'PLAYER_NOT_AUTHORIZED', '不是本局真人浏览器')
    }
    const body = await readBody(req)
    if (body === undefined) return fail(res, 400, 'INVALID_INPUT', '请求体非法')
    if (body.expected_revision !== game.revision) return fail(res, 409, 'STALE_ACTION', '状态已变化')
    if (!humanWaitActive()) return fail(res, 409, 'INVALID_STATE', '当前不是真人行动点')
    // 记录已公开动作，复刻后端 last_action；前端据此累积行动历史。
    seq += 1
    game.last_action = {
      hand_no: game.hand.number,
      sequence: seq,
      seat: game.human_seat,
      kind: body.kind,
      to: body.kind === 'raise_to' ? (body.to ?? 0) : 0,
      source: 'human',
      error_code: null,
      input_tokens: null,
      output_tokens: null,
    }
    // 应用动作后交给 AI：清空等待点，演示面板收起，进入「思考中」态。
    if (body.kind === 'fold') {
      const me = game.hand.players.find((x) => x.seat === game.human_seat)
      if (me) me.status = 'folded'
    }
    game.hand.waiting_for = null
    game.hand.actor = game.seats.find((s) => s.seat !== game.human_seat)?.seat ?? null
    game.revision += 1
    return send(res, 200, playerGame())
  }

  const cmdMatch = p.match(/^\/api\/v1\/games\/([^/]+)\/commands$/)
  if (cmdMatch && req.method === 'POST') {
    if (!game || game.id !== cmdMatch[1]) return fail(res, 404, 'NOT_FOUND', '牌局不存在')
    const body = await readBody(req)
    if (body === undefined) return fail(res, 400, 'INVALID_INPUT', '请求体非法')
    if (body.command === 'start' && game.status === 'created') startHand()
    else if (body.command === 'pause' && game.status === 'running') {
      game.status = 'paused'
      game.revision += 1
    } else if (body.command === 'resume' && game.status === 'paused') {
      game.status = 'running'
      game.revision += 1
    } else if (body.command === 'stop') {
      game.status = 'stopped'
      game.revision += 1
    }
    return send(res, 200, publicGame())
  }

  if (p.startsWith('/api/')) return fail(res, 404, 'NOT_FOUND', '未知接口')

  return serveStatic(res, p)
})

server.listen(PORT, '127.0.0.1', () => {
  console.log(`fixture server on http://127.0.0.1:${PORT} (dist=${DIST})`)
})
