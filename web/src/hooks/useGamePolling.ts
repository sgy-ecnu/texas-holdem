import { useCallback, useEffect, useRef, useState } from 'react'
import { ApiError, api, isAbort } from '../api'
import type { GameDTO } from '../types'

export type PollProblem = { kind: 'unknown' | 'error'; message: string } | null
// public = 公共观战视角（GET /games/{id}）；player = 真人私有视角（GET /games/{id}/player）。
export type PollMode = 'public' | 'player'

// 轮询频率：运行中每秒，暂停/已创建降低频率，终态停止。
function intervalFor(status: string | undefined): number {
  switch (status) {
    case 'running':
      return 1000
    case 'paused':
      return 4000
    case 'created':
      return 2500
    default:
      return 0
  }
}

export interface GamePolling {
  game: GameDTO | null
  problem: PollProblem
  updatedAt: number | null
  refresh: () => Promise<GameDTO>
  apply: (game: GameDTO) => void
}

export interface GamePollingOptions {
  // player 视角收到 403 时回调（真人 Cookie 缺失/不匹配）；调用方据此切换为公共观战。
  onUnauthorized?: () => void
}

export function useGamePolling(
  id: string,
  initial: GameDTO,
  mode: PollMode = 'public',
  options: GamePollingOptions = {},
): GamePolling {
  const [game, setGame] = useState<GameDTO | null>(initial)
  const [problem, setProblem] = useState<PollProblem>(null)
  const [updatedAt, setUpdatedAt] = useState<number | null>(Date.now())

  const gameRef = useRef<GameDTO | null>(initial)
  const revisionRef = useRef<number>(initial.revision)
  const optionsRef = useRef(options)
  optionsRef.current = options
  const status = game?.status

  const fetchGame = useCallback(
    (signal?: AbortSignal) => (mode === 'player' ? api.player(id, signal) : api.game(id, signal)),
    [id, mode],
  )

  // 只接受 revision 不小于当前值的响应，丢弃晚到的旧版本。
  const apply = useCallback((next: GameDTO) => {
    if (next.revision < revisionRef.current) return
    revisionRef.current = next.revision
    gameRef.current = next
    setGame(next)
    setProblem(null)
    setUpdatedAt(Date.now())
  }, [])

  // 统一错误归类：503/网络为「状态未知」，绝不解释成「无真人权限」；403 才切观战。
  const handleError = useCallback(
    (error: unknown): 'stop' | undefined => {
      if (isAbort(error)) return
      if (error instanceof ApiError) {
        if (mode === 'player' && error.status === 403) {
          optionsRef.current.onUnauthorized?.()
          return 'stop'
        }
        setProblem(
          error.status === 503
            ? { kind: 'unknown', message: '无法确认牌局状态：存储暂不可用，画面可能已过期。' }
            : { kind: 'error', message: error.message },
        )
        return
      }
      setProblem({ kind: 'error', message: '网络异常，暂时无法更新牌局状态。' })
    },
    [mode],
  )

  const refresh = useCallback(async () => {
    try {
      const next = await fetchGame()
      apply(next)
      return next
    } catch (error) {
      handleError(error)
      throw error
    }
  }, [fetchGame, apply, handleError])

  useEffect(() => {
    const first = intervalFor(status)
    if (first === 0) return

    let alive = true
    let controller: AbortController | null = null
    let timer = 0

    const run = async () => {
      controller = new AbortController()
      try {
        const next = await fetchGame(controller.signal)
        if (alive) apply(next)
      } catch (error) {
        if (!alive) return
        // 403 切观战由调用方重建轮询实例，本循环停止。
        if (handleError(error) === 'stop') return
      }
      if (!alive) return
      const delay = intervalFor(gameRef.current?.status)
      if (delay > 0) timer = window.setTimeout(run, delay)
    }

    timer = window.setTimeout(run, first)
    return () => {
      alive = false
      controller?.abort()
      window.clearTimeout(timer)
    }
    // mode 变化会重建轮询：清理旧实例、中止在途请求，避免上一视角晚到响应覆盖新视角。
  }, [fetchGame, status, apply, handleError])

  return { game, problem, updatedAt, refresh, apply }
}
