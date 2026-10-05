import { chips, SETTLEMENT_TITLE } from '../format'
import { potRows, settlementAnnouncement } from '../settlement'
import type { CompletedHandDTO } from '../types'

interface Props {
  hand: CompletedHandDTO
  nameBySeat: Map<number, string>
  onView: () => void
  onDismiss: () => void
}

// 桌下结算条：放在牌桌之后、ActionDock 之前（正常文档流，不遮牌桌）。
// 第一池标「主池」（不是「边池1」）；复杂结果多行，移动端可换行/展开。
// 可访问性：sr-only aria-live 只宣读一次；视觉内容 aria-hidden，避免重复播报。
export function SettlementBar({ hand, nameBySeat, onView, onDismiss }: Props) {
  const name = (seat: number) => nameBySeat.get(seat) ?? `座位 ${seat}`
  const rows = potRows(hand)
  const title = SETTLEMENT_TITLE[hand.reason] ?? HAND_TITLE_FALLBACK(hand.reason)

  return (
    <section className="settlement-bar" aria-label="本手结算">
      <div className="sr-only" role="status" aria-live="polite">
        {settlementAnnouncement(hand, nameBySeat)}
      </div>
      <div className="settlement-content" aria-hidden="true">
        <div className="settlement-head">
          第 {hand.hand_no} 手 · {title}
        </div>
        <div className="settlement-pots">
          {rows.map((row) => (
            <div className="settlement-pot" key={row.index}>
              <span className="pot-name">{row.name}</span>
              <span className="pot-amount">{chips(row.amount)}</span>
              <span className="pot-winners">
                {row.winners
                  .map((w) => `${name(w.seat)} ${chips(w.amount)}${w.rank ? ` · ${w.rank}` : ''}`)
                  .join('、')}
              </span>
            </div>
          ))}
        </div>
        {hand.refunds.length > 0 && (
          <div className="settlement-refunds">
            退回：{hand.refunds.map((r) => `${name(r.seat)} ${chips(r.amount)}`).join('、')}
          </div>
        )}
        <div className="settlement-actions">
          <button type="button" className="btn btn-sm settlement-view" onClick={onView}>
            查看本手详情
          </button>
          <button type="button" className="btn btn-sm btn-ghost settlement-collapse" onClick={onDismiss}>
            收起
          </button>
        </div>
      </div>
    </section>
  )
}

function HAND_TITLE_FALLBACK(reason: string): string {
  return reason === 'showdown' ? '摊牌结算' : '弃牌结算'
}
