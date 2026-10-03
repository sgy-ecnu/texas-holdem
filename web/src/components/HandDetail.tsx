import { useEffect, useMemo, useState } from 'react'
import { ApiError, api, isAbort } from '../api'
import { chips, HAND_REASON_LABEL, cardLabel } from '../format'
import type { CompletedHandDTO, SeatDTO } from '../types'
import { PlayingCard } from './PlayingCard'

interface Props {
  gameId: string
  handsCompleted: number
  seats: SeatDTO[]
  initialHandNo: number
  onClose: () => void
}

// 已完成手牌详情：仅在用户主动打开且该手已结束时请求，接口返回底牌才展示。
export function HandDetail({ gameId, handsCompleted, seats, initialHandNo, onClose }: Props) {
  const [handNo, setHandNo] = useState(initialHandNo)
  const [data, setData] = useState<CompletedHandDTO | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const nameBySeat = useMemo(() => {
    const map = new Map<number, string>()
    for (const s of seats) map.set(s.seat, s.name)
    return map
  }, [seats])

  useEffect(() => {
    const controller = new AbortController()
    setLoading(true)
    setError(null)
    setData(null)
    api
      .hand(gameId, handNo, controller.signal)
      .then((res) => {
        setData(res)
        setLoading(false)
      })
      .catch((err: unknown) => {
        if (isAbort(err)) return
        if (err instanceof ApiError && err.status === 409) {
          setError('该手牌尚未完成，暂不能查看底牌。')
        } else if (err instanceof ApiError) {
          setError(err.message)
        } else {
          setError('加载手牌详情失败，请稍后重试。')
        }
        setLoading(false)
      })
    return () => controller.abort()
  }, [gameId, handNo])

  const winnerSeats = useMemo(() => {
    if (!data) return new Set<number>()
    const set = new Set<number>()
    for (const p of data.payouts) set.add(p.seat)
    for (const pot of data.pots) for (const w of pot.winners) set.add(w)
    return set
  }, [data])

  const handOptions = Array.from({ length: handsCompleted }, (_, i) => i + 1)

  return (
    <div className="overlay" role="dialog" aria-modal="true" aria-label="已完成手牌详情" onClick={onClose}>
      <div className="modal" onClick={(e) => e.stopPropagation()}>
        <div className="modal-head">
          <span className="title">手牌详情</span>
          <div className="grow" />
          <select
            className="select"
            style={{ width: 'auto' }}
            value={handNo}
            onChange={(e) => setHandNo(Number(e.target.value))}
            aria-label="选择手数"
          >
            {handOptions.map((n) => (
              <option key={n} value={n}>
                第 {n} 手
              </option>
            ))}
          </select>
          <button className="btn btn-ghost btn-sm" onClick={onClose}>
            关闭
          </button>
        </div>

        <div className="modal-body">
          {loading && (
            <div className="empty">
              <span className="spinner" /> 正在加载手牌详情…
            </div>
          )}
          {error && !loading && <div className="banner error">{error}</div>}

          {data && !loading && (
            <>
              <div className="kv">
                <b>结果</b>
                {HAND_REASON_LABEL[data.reason] ?? data.reason}
                <b>·</b>
                <b>按钮位</b>
                座位 {data.button}
              </div>

              <div className="detail-block">
                <div className="detail-title">公共牌</div>
                <div className="board">
                  {data.board.length === 0 && <span className="muted">本手未发出公共牌</span>}
                  {data.board.map((c) => (
                    <PlayingCard key={c} card={c} small />
                  ))}
                </div>
              </div>

              <div className="detail-block">
                <div className="detail-title">底牌（已结束手牌才揭示）</div>
                {data.hole_cards.length === 0 ? (
                  <span className="muted">无底牌记录</span>
                ) : (
                  <div className="hole-grid">
                    {data.hole_cards.map((h) => {
                      const isWinner = winnerSeats.has(h.seat)
                      return (
                        <div
                          className={`hole-item${isWinner ? ' is-winner' : ''}`}
                          key={h.seat}
                          title={h.cards.map(cardLabel).join(' ')}
                        >
                          <span className="hole-name">
                            {nameBySeat.get(h.seat) ?? `座位 ${h.seat}`}
                            {isWinner ? ' · 胜' : ''}
                          </span>
                          <span className="hole-cards">
                            {h.cards.map((c) => (
                              <PlayingCard key={c} card={c} small />
                            ))}
                          </span>
                        </div>
                      )
                    })}
                  </div>
                )}
              </div>

              <div className="detail-block">
                <div className="detail-title">底池分配</div>
                {data.pots.length === 0 ? (
                  <span className="muted">无底池记录</span>
                ) : (
                  data.pots.map((pot, i) => (
                    <div className="pot-item" key={i}>
                      <div className="pot-head">
                        <span>边池 {i + 1}</span>
                        <span className="amount">{chips(pot.amount)}</span>
                      </div>
                      <div className="kv">
                        <b>有资格</b>
                        {pot.eligible.length ? pot.eligible.map((s) => `座位 ${s}`).join('、') : '无'}
                        <b>·</b>
                        <b>获胜</b>
                        {pot.winners.length
                          ? pot.winners.map((s) => nameBySeat.get(s) ?? `座位 ${s}`).join('、')
                          : '无'}
                      </div>
                    </div>
                  ))
                )}
                {data.refunds.length > 0 && (
                  <div className="kv" style={{ marginTop: 8 }}>
                    <b>退款</b>
                    {data.refunds.map((r) => `${nameBySeat.get(r.seat) ?? `座位 ${r.seat}`} ${chips(r.amount)}`).join('、')}
                  </div>
                )}
                {data.payouts.length > 0 && (
                  <div className="kv" style={{ marginTop: 8 }}>
                    <b>总获奖</b>
                    {data.payouts.map((p) => `${nameBySeat.get(p.seat) ?? `座位 ${p.seat}`} ${chips(p.amount)}`).join('、')}
                  </div>
                )}
              </div>

              <div className="detail-block">
                <div className="detail-title">最终筹码</div>
                <div className="kv">
                  {data.final_stacks.map((s, i) => (
                    <span key={s.seat}>
                      {i > 0 && ' · '}
                      <b>{nameBySeat.get(s.seat) ?? `座位 ${s.seat}`}</b>
                      <span className="mono"> {chips(s.stack)}</span>
                    </span>
                  ))}
                </div>
              </div>

              <div className="muted" style={{ fontSize: 12 }}>
                说明：手牌进行中前端不展示任何底牌；只有本手完成后，这里才会请求并显示底牌。
              </div>
            </>
          )}
        </div>
      </div>
    </div>
  )
}
