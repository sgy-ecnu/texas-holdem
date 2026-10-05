import { chips, potName } from '../format'
import type { ActionPlacement } from './ActionPopover'

interface Props {
  placement: ActionPlacement
  amount: number
  rank: string | null
  breakdown: string | null
  isMe: boolean
  sidePotIndex: number | null
  uncontested: boolean
  leaving: boolean
}

// 赢家标签：固定锚点 + 定位缩放分离 + pointer-events none（与 ActionPopover 同族规则）。
// 不复用 ActionKind 枚举 / 2.2s 生命周期 / 1.8 倍缩放 / 动作 FIFO。
// aria-hidden：语义宣读由桌下结算条（aria-live，只读一次）承担，视觉标签不重复播报。
export function WinnerTag({ placement, amount, rank, breakdown, isMe, sidePotIndex, uncontested, leaving }: Props) {
  const classes = ['winner-tag', `is-${placement}`]
  if (isMe) classes.push('is-me')
  if (leaving) classes.push('is-leaving')
  return (
    <div className={classes.join(' ')} aria-hidden="true">
      <span className="winner-amount">{isMe ? `你赢了 +${chips(amount)}` : `赢 +${chips(amount)}`}</span>
      {rank && (
        <span className="winner-rank">
          {rank}
          {breakdown ? ` · ${breakdown}` : ''}
        </span>
      )}
      {!rank && uncontested && <span className="winner-sub">其余玩家弃牌</span>}
      {isMe && sidePotIndex != null && <span className="winner-sub">赢得{potName(sidePotIndex)}</span>}
    </div>
  )
}
