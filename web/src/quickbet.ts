import type { HandDTO, LegalActionsDTO } from './types'

// 快捷注按钮：ratio 为底池比例；allIn 表示全押，直接取合法上限。
export interface QuickBetChip {
  key: string
  short: string
  label: string
  ratio: number
  allIn: boolean
}

// 比例参考 PokerTH：1/3、1/2、2/3、3/4、1× 底池，以及全押。
export const QUICK_BET_CHIPS: QuickBetChip[] = [
  { key: '1/3', short: '1/3', label: '1/3 底池', ratio: 1 / 3, allIn: false },
  { key: '1/2', short: '1/2', label: '1/2 底池', ratio: 1 / 2, allIn: false },
  { key: '2/3', short: '2/3', label: '2/3 底池', ratio: 2 / 3, allIn: false },
  { key: '3/4', short: '3/4', label: '3/4 底池', ratio: 3 / 4, allIn: false },
  { key: '1', short: '1×', label: '1× 底池', ratio: 1, allIn: false },
  { key: 'allin', short: '全押', label: '全押', ratio: 0, allIn: true },
]

// 基准池 = 底池 + 当前注 + 需跟注额。
// 这是 PokerTH 式「跟注后底池」基准：加注目标按该基准乘以比例得到。
export function quickBetBase(hand: HandDTO | null, legal: LegalActionsDTO): number {
  const pot = hand ? hand.pot : 0
  const currentBet = hand ? hand.current_bet : 0
  return pot + currentBet + legal.call_amount
}

function clamp(n: number, lo: number, hi: number): number {
  return Math.min(hi, Math.max(lo, n))
}

// 计算快捷注的目标「本街累计投入值」，并 clamp 到合法区间。
// - 无加注权（can_raise=false）→ null，整组快捷注禁用。
// - 全押 → max_raise_to（短全押场景下 max_raise_to 即 short_all_in_to）。
// - 比例注在仅有短全押（max_raise_to < min_raise_to）时无意义 → null。
export function quickBetTarget(
  hand: HandDTO | null,
  legal: LegalActionsDTO,
  chip: QuickBetChip,
): number | null {
  if (!legal.can_raise) return null
  if (chip.allIn) return legal.max_raise_to
  if (legal.max_raise_to < legal.min_raise_to) return null
  const raw = Math.round(chip.ratio * quickBetBase(hand, legal))
  return clamp(raw, legal.min_raise_to, legal.max_raise_to)
}
