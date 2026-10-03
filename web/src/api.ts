import type {
  Command,
  CompletedHandDTO,
  CreateGameRequest,
  ErrorDTO,
  GameDTO,
  HealthDTO,
  HumanActionRequest,
  ModelsDTO,
} from './types'

// 统一走同源相对路径：开发经 Vite proxy，生产由 Go 同源提供静态资源。
const BASE = ''

export class ApiError extends Error {
  readonly status: number
  readonly code: string

  constructor(status: number, code: string, message: string) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.code = code
  }
}

export function isAbort(error: unknown): boolean {
  return error instanceof DOMException
    ? error.name === 'AbortError'
    : error instanceof Error && error.name === 'AbortError'
}

async function parseBody(res: Response): Promise<unknown> {
  try {
    return await res.json()
  } catch {
    return null
  }
}

function toApiError(res: Response, body: unknown): ApiError {
  let code = 'UNKNOWN'
  let message = `请求失败（HTTP ${res.status}）`
  if (body && typeof body === 'object' && 'error' in body) {
    const detail = (body as ErrorDTO).error
    if (detail && typeof detail === 'object') {
      if (typeof detail.code === 'string' && detail.code) code = detail.code
      if (typeof detail.message === 'string' && detail.message) message = detail.message
    }
  }
  return new ApiError(res.status, code, message)
}

async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const headers = new Headers(init.headers)
  headers.set('Accept', 'application/json')
  if (init.body != null) headers.set('Content-Type', 'application/json')

  // 同源请求显式带上 Cookie：真人身份是 HttpOnly 游戏级会话，前端只依赖同源凭据，
  // 绝不读取 Cookie 值。见契约「三、真人身份与私牌接口」。
  const res = await fetch(BASE + path, { ...init, credentials: 'same-origin', headers })
  const body = await parseBody(res)
  if (!res.ok) throw toApiError(res, body)
  return body as T
}

export interface HealthResult {
  health: HealthDTO
  // false 表示后端以 503 返回但携带了可用字段（存储降级）。
  ok: boolean
}

async function getHealth(signal?: AbortSignal): Promise<HealthResult> {
  const res = await fetch(BASE + '/api/v1/health', {
    signal,
    credentials: 'same-origin',
    headers: { Accept: 'application/json' },
  })
  const body = await parseBody(res)
  if (res.ok) {
    if (!body) throw toApiError(res, body)
    return { health: body as HealthDTO, ok: true }
  }
  // 503 时后端仍返回健康字段，用于展示降级原因；无 body 才视为错误。
  if (res.status === 503 && body) {
    return { health: body as HealthDTO, ok: false }
  }
  throw toApiError(res, body)
}

export const api = {
  health: getHealth,
  models: (signal?: AbortSignal) => request<ModelsDTO>('/api/v1/models', { signal }),
  current: (signal?: AbortSignal) => request<GameDTO>('/api/v1/games/current', { signal }),
  game: (id: string, signal?: AbortSignal) =>
    request<GameDTO>(`/api/v1/games/${encodeURIComponent(id)}`, { signal }),
  // 真人私有视角：需本浏览器持有游戏级 HttpOnly Cookie；否则 403 PLAYER_NOT_AUTHORIZED。
  player: (id: string, signal?: AbortSignal) =>
    request<GameDTO>(`/api/v1/games/${encodeURIComponent(id)}/player`, { signal }),
  // 提交真人动作：body 携带 expected_revision 做并发校验，响应为该动作提交后的真人 GameDTO。
  action: (id: string, body: HumanActionRequest, signal?: AbortSignal) =>
    request<GameDTO>(`/api/v1/games/${encodeURIComponent(id)}/action`, {
      method: 'POST',
      body: JSON.stringify(body),
      signal,
    }),
  create: (body: CreateGameRequest, signal?: AbortSignal) =>
    request<GameDTO>('/api/v1/games', {
      method: 'POST',
      body: JSON.stringify(body),
      signal,
    }),
  command: (id: string, command: Command, signal?: AbortSignal) =>
    request<GameDTO>(`/api/v1/games/${encodeURIComponent(id)}/commands`, {
      method: 'POST',
      body: JSON.stringify({ command }),
      signal,
    }),
  hand: (id: string, handNo: number, signal?: AbortSignal) =>
    request<CompletedHandDTO>(
      `/api/v1/games/${encodeURIComponent(id)}/hands/${handNo}`,
      { signal },
    ),
}
