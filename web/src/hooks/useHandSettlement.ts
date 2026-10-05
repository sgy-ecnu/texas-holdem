import { useCallback, useEffect, useRef, useState } from 'react'
import { api } from '../api'
import { settlementSchedule, type SettlementPhase } from '../settlement'
import type { CompletedHandDTO, GameDTO } from '../types'

export interface HandSettlement {
  hand: CompletedHandDTO | null
  phase: SettlementPhase
}

// 观察 game.hands_completed 从 N 增到 M：为 N+1..M 拉取 api.hand 并依次展示结算。
// 首次进入只记录基线、不重播历史手；切视角（Stage 以 mode 为 key 重建）同样以当前值作基线，
// 不会重播同一手。去重键 = gameId:handNo。
export function useHandSettlement(game: GameDTO | null): HandSettlement {
  const [hand, setHand] = useState<CompletedHandDTO | null>(null)
  const [phase, setPhase] = useState<SettlementPhase>('idle')

  const gameId = game?.id ?? null
  const completed = game?.hands_completed ?? 0

  const aliveRef = useRef(true)
  const gameIdRef = useRef<string | null>(null)
  const baselineRef = useRef<number | null>(null)
  const seenRef = useRef<Set<string>>(new Set())
  const queueRef = useRef<number[]>([])
  const playingRef = useRef(false)
  const timersRef = useRef<number[]>([])

  useEffect(() => {
    aliveRef.current = true
    return () => {
      aliveRef.current = false
      for (const t of timersRef.current) window.clearTimeout(t)
      timersRef.current = []
    }
  }, [])

  const clearTimers = useCallback(() => {
    for (const t of timersRef.current) window.clearTimeout(t)
    timersRef.current = []
  }, [])

  const pumpRef = useRef<() => void>(() => {})
  const pump = useCallback(() => {
    if (playingRef.current) return
    const id = gameIdRef.current
    if (!id) return
    const next = queueRef.current.shift()
    if (next === undefined) return
    playingRef.current = true
    setPhase('loading')
    api
      .hand(id, next)
      .then((res) => {
        if (!aliveRef.current) return
        setHand(res)
        setPhase('reveal')
        const s = settlementSchedule(res.reason)
        timersRef.current.push(
          window.setTimeout(() => aliveRef.current && setPhase('award'), s.revealEnd),
          window.setTimeout(() => aliveRef.current && setPhase('hold'), s.awardEnd),
          window.setTimeout(() => aliveRef.current && setPhase('finished'), s.holdEnd),
          window.setTimeout(() => {
            if (!aliveRef.current) return
            setPhase('idle')
            setHand(null)
            playingRef.current = false
            pumpRef.current()
          }, s.finishEnd),
        )
      })
      .catch(() => {
        // 拉取失败绝不伪造结算：跳过该手，继续队列。
        if (!aliveRef.current) return
        playingRef.current = false
        pumpRef.current()
      })
  }, [])
  useEffect(() => {
    pumpRef.current = pump
  }, [pump])

  useEffect(() => {
    if (!gameId) return
    // 新牌局或首次观察：记录基线，不重播。
    if (baselineRef.current === null || gameIdRef.current !== gameId) {
      clearTimers()
      gameIdRef.current = gameId
      baselineRef.current = completed
      seenRef.current = new Set()
      queueRef.current = []
      playingRef.current = false
      setPhase('idle')
      setHand(null)
      return
    }
    if (completed <= baselineRef.current) {
      baselineRef.current = completed
      return
    }
    for (let n = baselineRef.current + 1; n <= completed; n++) {
      const key = `${gameId}:${n}`
      if (seenRef.current.has(key)) continue
      seenRef.current.add(key)
      queueRef.current.push(n)
    }
    baselineRef.current = completed
    pumpRef.current()
  }, [gameId, completed, clearTimers])

  return { hand, phase }
}
