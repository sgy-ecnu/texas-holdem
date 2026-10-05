import { potName } from './format'
import type { CompletedHandDTO } from './types'

// 结算展示的纯派生逻辑：不涉及 DOM/计时，便于单测与复用。

export type SettlementPhase = 'idle' | 'loading' | 'reveal' | 'award' | 'hold' | 'finished'

export interface SettlementView {
  hand: CompletedHandDTO
  phase: SettlementPhase
  reduced: boolean
}

// 展示状态机各阶段结束时刻（相对该手展示开始，毫秒）。
// 摊牌约 6.5s（0-250 收束、250-950 翻牌、950-1300 揭晓、1300-2800 派发、2800-6200 停留、6200-6500 收尾）；
// 弃牌约 3.8s（更短，但不虚构牌型）。
export interface SettlementSchedule {
  revealEnd: number
  awardEnd: number
  holdEnd: number
  finishEnd: number
}

export function settlementSchedule(reason: string | undefined): SettlementSchedule {
  if (reason === 'showdown') {
    return { revealEnd: 1300, awardEnd: 2800, holdEnd: 6200, finishEnd: 6500 }
  }
  return { revealEnd: 900, awardEnd: 1800, holdEnd: 3300, finishEnd: 3800 }
}

// 摊牌参与者：hole_cards 中的座位 ∩ 各池 eligible 并集（弃牌者不在此列）。
export function showdownParticipantSeats(hand: CompletedHandDTO): number[] {
  if (hand.reason !== 'showdown') return []
  const eligible = new Set<number>()
  for (const pot of hand.pots) for (const s of pot.eligible) eligible.add(s)
  const holeSeats = new Set(hand.hole_cards.map((h) => h.seat))
  return hand.hole_cards
    .map((h) => h.seat)
    .filter((s) => holeSeats.has(s) && eligible.has(s))
}

// 每个座位的总获奖（= 顶层 payouts 之和；不含 refunds）。
export function payoutTotalBySeat(hand: CompletedHandDTO): Map<number, number> {
  const map = new Map<number, number>()
  for (const p of hand.payouts) map.set(p.seat, (map.get(p.seat) ?? 0) + p.amount)
  return map
}

export function winnerSeats(hand: CompletedHandDTO): Set<number> {
  const totals = payoutTotalBySeat(hand)
  const set = new Set<number>()
  for (const [seat, amount] of totals) if (amount > 0) set.add(seat)
  for (const pot of hand.pots) for (const w of pot.winners) if ((totals.get(w) ?? 0) > 0) set.add(w)
  return set
}

export function handRankBySeat(hand: CompletedHandDTO): Map<number, string> {
  const map = new Map<number, string>()
  for (const s of hand.showdown) map.set(s.seat, s.hand_rank)
  return map
}

export function bestFiveBySeat(hand: CompletedHandDTO): Map<number, string[]> {
  const map = new Map<number, string[]>()
  for (const s of hand.showdown) map.set(s.seat, s.best_five)
  return map
}

export function finalStackBySeat(hand: CompletedHandDTO): Map<number, number> {
  const map = new Map<number, number>()
  for (const s of hand.final_stacks) map.set(s.seat, s.stack)
  return map
}

export interface PotWinnerRow {
  seat: number
  amount: number
  rank: string | null
}

export interface PotRow {
  index: number
  name: string
  amount: number
  winners: PotWinnerRow[]
}

// 每池 → 赢家（含个人该池 payout 与该座位牌型）。
export function potRows(hand: CompletedHandDTO): PotRow[] {
  const rank = handRankBySeat(hand)
  return hand.pots.map((pot, index) => {
    const winners = pot.payouts
      .filter((p) => p.amount > 0)
      .map((p) => ({ seat: p.seat, amount: p.amount, rank: rank.get(p.seat) ?? null }))
    // 无逐座位 payouts 时退化为 winners 列表（金额按 pool 计），绝不虚构个人金额。
    if (winners.length === 0 && pot.winners.length > 0) {
      for (const w of pot.winners) winners.push({ seat: w, amount: 0, rank: rank.get(w) ?? null })
    }
    return { index, name: potName(index), amount: pot.amount, winners }
  })
}

// 真人赢家只赢边池时，明确指出赢得了哪一边池。
export function humanSidePotIndex(hand: CompletedHandDTO, seat: number): number | null {
  const mainPayout = hand.pots[0]?.payouts.find((p) => p.seat === seat)?.amount ?? 0
  if (mainPayout > 0) return null
  for (let i = 1; i < hand.pots.length; i++) {
    if ((hand.pots[i].payouts.find((p) => p.seat === seat)?.amount ?? 0) > 0) return i
  }
  return null
}

const RANK_ORDER = '23456789TJQKA'

// 由 best_five 归并出「A三条/K一对」式的牌型构成；仅当存在成组（对子/三条/四条）时展示，否则返回 null。
export function bestFiveBreakdown(bestFive: string[]): string | null {
  if (bestFive.length === 0) return null
  const counts = new Map<string, number>()
  for (const card of bestFive) {
    const rank = card.length >= 2 ? card.slice(0, -1) : card
    counts.set(rank, (counts.get(rank) ?? 0) + 1)
  }
  const groups = [...counts.entries()].sort((a, b) => {
    if (b[1] !== a[1]) return b[1] - a[1]
    return RANK_ORDER.indexOf(b[0]) - RANK_ORDER.indexOf(a[0])
  })
  if (groups.every(([, n]) => n === 1)) return null
  const name = (n: number) => (n >= 4 ? '四条' : n === 3 ? '三条' : n === 2 ? '一对' : '单张')
  return groups.map(([rank, n]) => `${rank}${name(n)}`).join('/')
}

// 结算条 aria-live 摘要（只宣读一次；视觉标签 aria-hidden 避免重复）。
export function settlementAnnouncement(hand: CompletedHandDTO, nameBySeat: Map<number, string>): string {
  const title = hand.reason === 'showdown' ? '摊牌结算' : '弃牌结算'
  const parts: string[] = [`第 ${hand.hand_no} 手${title}`]
  const rows = potRows(hand)
  for (const row of rows) {
    const winners = row.winners.map((w) => `${nameBySeat.get(w.seat) ?? `座位${w.seat}`} ${w.amount}`).join('、')
    parts.push(`${row.name}${row.amount}${winners ? `→${winners}` : ''}`)
  }
  if (hand.refunds.length > 0) {
    parts.push(`退回 ${hand.refunds.map((r) => `${nameBySeat.get(r.seat) ?? `座位${r.seat}`} ${r.amount}`).join('、')}`)
  }
  return parts.join('，')
}
