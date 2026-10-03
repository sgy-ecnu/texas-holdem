import { useEffect, useRef, useState } from 'react'
import type { PlayerDTOView } from '../types'

export interface Countdown {
  // 服务端截止时间换算到本地时间轴后的剩余毫秒。
  remainingMs: number
  // 同一截止时间首次观测到的剩余总量；用于进度条百分比。
  totalMs: number
  active: boolean
  expired: boolean
}

export function formatRemaining(ms: number): string {
  const total = Math.max(0, Math.ceil(ms / 1000))
  const m = Math.floor(total / 60)
  const s = total % 60
  return m > 0 ? `${m}:${String(s).padStart(2, '0')}` : `${s}s`
}

// 倒计时以服务端 server_time/action_deadline 为权威，本地时钟只做推进；
// 前端绝不自动弃牌，归零后刷新等待服务端处理结果。见契约「七、行动面板」。
export function useCountdown(player: PlayerDTOView | null): Countdown {
  const deadlineStr = player?.action_deadline ?? null
  const serverStr = player?.server_time ?? null
  const [now, setNow] = useState(() => Date.now())
  const deadlineAtRef = useRef<number | null>(null)
  const totalRef = useRef(0)
  const seenDeadlineRef = useRef<string | null>(null)

  useEffect(() => {
    if (!deadlineStr) {
      deadlineAtRef.current = null
      totalRef.current = 0
      seenDeadlineRef.current = null
      return
    }
    const deadline = Date.parse(deadlineStr)
    const server = serverStr ? Date.parse(serverStr) : Date.now()
    if (Number.isNaN(deadline) || Number.isNaN(server)) {
      deadlineAtRef.current = null
      totalRef.current = 0
      return
    }
    const remaining = Math.max(0, deadline - server)
    deadlineAtRef.current = Date.now() + remaining
    // 同一截止时间只记录首次观测值作为进度总量，后续轮询的 server_time 前移不改总量。
    if (seenDeadlineRef.current !== deadlineStr) {
      seenDeadlineRef.current = deadlineStr
      totalRef.current = remaining
    }
    setNow(Date.now())
  }, [deadlineStr, serverStr])

  useEffect(() => {
    if (!deadlineStr) return
    const timer = window.setInterval(() => setNow(Date.now()), 250)
    return () => window.clearInterval(timer)
  }, [deadlineStr])

  const remainingMs = deadlineAtRef.current === null ? 0 : Math.max(0, deadlineAtRef.current - now)
  const expired = deadlineStr !== null && deadlineAtRef.current !== null && remainingMs <= 0
  return { remainingMs, totalMs: totalRef.current, active: deadlineStr !== null, expired }
}
