import { useEffect, useRef } from 'react'
import { ACTION_LABEL, chips, errorCodeLabel, SOURCE_LABEL } from '../format'
import type { ActionDTO, SeatDTO } from '../types'

interface Props {
  // 由轮询累积的已公开动作序列（按发生顺序）。见 SpectatorView 的 history 累积。
  actions: ActionDTO[]
  seats: SeatDTO[]
}

function keyOf(action: ActionDTO): string {
  return `${action.hand_no}-${action.sequence}`
}

// 行动历史：清晰滚动展示每个玩家已公开的动作，新动作入场时短暂高亮。
// 只展示服务端已下发的 last_action 序列，不含任何未结束手牌的底牌信息。
export function ActionFeed({ actions, seats }: Props) {
  const listRef = useRef<HTMLDivElement>(null)
  const latest = actions.length > 0 ? actions[actions.length - 1] : null
  const latestKey = latest ? keyOf(latest) : null
  const nameBySeat = new Map(seats.map((s) => [s.seat, s.name]))

  // 新动作进入时滚到底部，保持「谁刚刚做了什么」始终可见。
  useEffect(() => {
    const el = listRef.current
    if (el) el.scrollTop = el.scrollHeight
  }, [actions.length])

  if (actions.length === 0) {
    return <div className="feed-empty">等待第一次行动…</div>
  }

  return (
    <div className="action-feed" role="log" aria-live="polite" aria-label="行动历史" ref={listRef}>
      {actions.map((action) => {
        const key = keyOf(action)
        const isNew = key === latestKey
        return (
          <div className={`feed-row${isNew ? ' is-new' : ''}`} key={key}>
            <span className="feed-seat">
              {nameBySeat.get(action.seat) ?? `座位 ${action.seat}`}
            </span>
            <span className="feed-act">
              {ACTION_LABEL[action.kind]}
              {action.kind === 'raise_to' && ` ${chips(action.to)}`}
            </span>
            <span className={`pill ${action.source}`}>
              {SOURCE_LABEL[action.source] ?? action.source}
            </span>
            {action.error_code && (
              <span className="pill fallback">{errorCodeLabel(action.error_code)}</span>
            )}
            <span className="feed-meta">
              第 {action.hand_no} 手 · #{action.sequence}
            </span>
          </div>
        )
      })}
    </div>
  )
}
