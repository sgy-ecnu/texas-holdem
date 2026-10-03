import { useEffect, useMemo, useRef, useState } from 'react'
import { ApiError, api } from '../api'
import { chips } from '../format'
import { useCountdown } from '../hooks/useCountdown'
import { QUICK_BET_CHIPS, quickBetTarget } from '../quickbet'
import type { ActionKind, GameDTO, LegalActionsDTO } from '../types'

interface Props {
  game: GameDTO
  // 控制命令（start/pause/resume/stop）进行中时，动作面板一并禁用，避免与串行边界竞争。
  controlsBusy: boolean
  // 上报动作提交忙碌状态，让父组件在动作进行中禁用控制命令。
  onBusyChange: (busy: boolean) => void
  onApply: (game: GameDTO) => void
  onRefresh: () => Promise<GameDTO>
}

// 动作按钮选择：全押是 UI 操作，不新增引擎动作。见契约「四、全押按钮映射」。
type AllInPlan = { kind: 'raise_to'; to: number } | { kind: 'call' } | null

// 键盘快捷键焦点保护：输入/文本域/下拉/可编辑元素聚焦时不响应全局快捷键，
// 避免数字输入与（移动端）软键盘误触动作。
function isTypingTarget(el: EventTarget | null): boolean {
  if (!(el instanceof HTMLElement)) return false
  const tag = el.tagName
  return tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT' || el.isContentEditable
}

// 真人操作区：轮到自己时在底部行动区呈现「弃牌/过牌/跟注/加注/全押」。
// 标题与倒计时由外层 ActionDock 统一展示，这里只负责合法动作按钮与提交逻辑。
export function HumanActionPanel({ game, controlsBusy, onBusyChange, onApply, onRefresh }: Props) {
  const player = game.player
  const hand = game.hand
  const legal: LegalActionsDTO | null = player?.legal_actions ?? null
  const { expired } = useCountdown(player)

  const [raiseDraft, setRaiseDraft] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [notice, setNotice] = useState<string | null>(null)
  const refreshedForDeadline = useRef<string | null>(null)

  const me = useMemo(() => {
    if (!hand || !player) return null
    return hand.players.find((p) => p.seat === player.seat) ?? null
  }, [hand, player])

  const isMyTurn =
    !!player &&
    !!hand &&
    game.status === 'running' &&
    hand.actor === player.seat &&
    hand.waiting_for === 'human_action' &&
    legal !== null &&
    player.action_deadline !== null

  const canAct = isMyTurn && !controlsBusy && !submitting && !expired

  const allIn: AllInPlan = useMemo(() => {
    if (!legal || !me) return null
    if (legal.can_raise) return { kind: 'raise_to', to: legal.max_raise_to }
    if (legal.can_call && me.stack > 0 && legal.call_amount === me.stack) return { kind: 'call' }
    return null
  }, [legal, me])

  const raiseInputRef = useRef<HTMLInputElement | null>(null)

  // 快捷注目标：仅在拥有完整加注区间时按比例给出；无加注权时整组为 null（禁用）。
  const quickChips = useMemo(() => {
    if (!legal) return []
    return QUICK_BET_CHIPS.map((chip) => ({ chip, to: quickBetTarget(hand, legal, chip) }))
  }, [hand, legal])

  // 全局快捷键一次注册，用 ref 读取最新状态，避免闭包持有旧 legal/revision。
  const keyState = useRef({ canAct, legal, send })
  useEffect(() => {
    keyState.current = { canAct, legal, send }
  })

  useEffect(() => {
    function onKeyDown(e: KeyboardEvent) {
      // 组合键、长按重复、已被处理的按键一律不拦截。
      if (e.defaultPrevented || e.metaKey || e.ctrlKey || e.altKey || e.repeat) return
      const { canAct: act, legal: l, send: dispatch } = keyState.current
      if (!act || !l) return
      // 输入类元素聚焦时不响应，保证加注输入与移动端软键盘不被误触。
      if (isTypingTarget(e.target)) return
      // 弹层打开时不响应，避免在查看详情时误提交动作。
      if (document.querySelector('.overlay')) return
      const key = e.key.toLowerCase()
      if (key === 'f' && l.can_fold) {
        e.preventDefault()
        void dispatch('fold')
      } else if (key === 'c' && l.can_call) {
        e.preventDefault()
        void dispatch('call')
      } else if (key === 'k' && l.can_check) {
        e.preventDefault()
        void dispatch('check')
      } else if (key === 'r' && l.can_raise) {
        e.preventDefault()
        raiseInputRef.current?.focus()
        raiseInputRef.current?.select()
      }
    }
    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
  }, [])

  // 倒计时归零：禁用动作并刷新一次，等待服务端超时弃牌结果；浏览器绝不自动发弃牌。
  useEffect(() => {
    if (!player?.action_deadline || !isMyTurn || !expired) return
    const key = player.action_deadline
    if (refreshedForDeadline.current === key) return
    refreshedForDeadline.current = key
    setNotice('已到截止时间，正在刷新以获取服务端处理结果…')
    void onRefresh().catch(() => undefined)
  }, [isMyTurn, expired, player?.action_deadline, onRefresh])

  // 新的等待点（换手/换行动人）清除加注草稿与旧提示；同一行动点内不打断草稿。
  useEffect(() => {
    setRaiseDraft('')
    setError(null)
    setNotice(null)
    refreshedForDeadline.current = null
  }, [player?.hand_no, player?.action_deadline])

  async function send(kind: ActionKind, to?: number) {
    if (!game) return
    setSubmitting(true)
    onBusyChange(true)
    setError(null)
    setNotice(null)
    try {
      const next = await api.action(game.id, {
        expected_revision: game.revision,
        kind,
        ...(to !== undefined ? { to } : {}),
      })
      onApply(next)
      setRaiseDraft('')
    } catch (err) {
      if (err instanceof ApiError) {
        if (err.status === 409) {
          // STALE_ACTION / INVALID_STATE / ACTION_EXPIRED：以服务端权威状态为准，不重发 POST。
          setNotice('状态已变化，正在刷新最新状态…')
          try {
            await onRefresh()
          } catch {
            /* 刷新失败交由轮询兜底 */
          }
        } else {
          setError(err.message)
          if (err.status >= 500) {
            try {
              await onRefresh()
            } catch {
              /* 忽略，轮询会继续对齐 */
            }
          }
        }
      } else {
        setError('网络异常，正在刷新状态…')
        try {
          await onRefresh()
        } catch {
          /* 忽略 */
        }
      }
    } finally {
      setSubmitting(false)
      onBusyChange(false)
    }
  }

  function sendRaise() {
    if (!legal) return
    const to = Number(raiseDraft)
    if (!Number.isInteger(to) || to <= 0) {
      setError('加注到必须是正整数。')
      return
    }
    const inRange = legal.can_raise && to >= legal.min_raise_to && to <= legal.max_raise_to
    const isShortAllIn = legal.short_all_in_to !== null && to === legal.short_all_in_to
    if (!inRange && !isShortAllIn) {
      setError('加注到超出当前合法范围。')
      return
    }
    void send('raise_to', to)
  }

  // 快捷注：把目标值填入加注输入框并聚焦，不自动提交；由玩家确认「加注」或回车提交。
  // 选择「填入」而非「直接提交」，避免一次误点即下重注。
  function applyQuickTo(to: number) {
    setError(null)
    setRaiseDraft(String(to))
    const el = raiseInputRef.current
    if (el) {
      el.focus()
      el.select()
    }
  }

  if (!player) return null

  return (
    <section className={`action-panel${isMyTurn ? ' is-turn' : ''}`} aria-live="polite">
      <div className="action-who">真人 · 座位 {player.seat}</div>

      {notice && <div className="banner info">{notice}</div>}
      {error && (
        <div className="banner error" role="alert">
          {error}
        </div>
      )}

      {isMyTurn && legal && (
        <div className="action-body">
          <div className="action-buttons">
            {legal.can_fold && (
              <button
                className="btn btn-danger"
                aria-label="弃牌"
                aria-keyshortcuts="F"
                disabled={!canAct}
                onClick={() => void send('fold')}
              >
                弃牌 <kbd className="keyhint" aria-hidden="true">F</kbd>
              </button>
            )}
            {legal.can_check && (
              <button
                className="btn"
                aria-label="过牌"
                aria-keyshortcuts="K"
                disabled={!canAct}
                onClick={() => void send('check')}
              >
                过牌 <kbd className="keyhint" aria-hidden="true">K</kbd>
              </button>
            )}
            {legal.can_call && (
              <button
                className="btn btn-primary"
                aria-label={`跟注 ${chips(legal.call_amount)}`}
                aria-keyshortcuts="C"
                disabled={!canAct}
                onClick={() => void send('call')}
              >
                跟注 {chips(legal.call_amount)} <kbd className="keyhint" aria-hidden="true">C</kbd>
              </button>
            )}
            {/* 无加注权时，全押可能仍是唯一可提交动作（跟注即全下）；有加注权时由快捷注的「全押」承担。 */}
            {!legal.can_raise && (
              <button
                className="btn btn-allin"
                aria-label="全押"
                disabled={!canAct || allIn === null}
                title={allIn === null ? '当前无合法全押动作' : undefined}
                onClick={() => {
                  if (!allIn) return
                  void send(allIn.kind, allIn.kind === 'raise_to' ? allIn.to : undefined)
                }}
              >
                全押
              </button>
            )}
          </div>

          {legal.can_raise && (
            <div className="raise-control">
              <label htmlFor="raise-to">加注到（本街累计）</label>
              <input
                id="raise-to"
                ref={raiseInputRef}
                className="input"
                type="number"
                inputMode="numeric"
                value={raiseDraft}
                placeholder={`${legal.min_raise_to} – ${legal.max_raise_to}`}
                min={legal.min_raise_to}
                max={legal.max_raise_to}
                disabled={!canAct}
                onChange={(e) => setRaiseDraft(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === 'Enter' && canAct) {
                    e.preventDefault()
                    sendRaise()
                  }
                }}
              />
              <button
                className="btn btn-primary"
                aria-label="加注"
                aria-keyshortcuts="R"
                disabled={!canAct}
                onClick={sendRaise}
              >
                加注 <kbd className="keyhint" aria-hidden="true">R</kbd>
              </button>
              <span className="raise-hint">
                范围 {chips(legal.min_raise_to)} – {chips(legal.max_raise_to)}
                {legal.short_all_in_to !== null ? `（短全押 ${chips(legal.short_all_in_to)}）` : ''}
              </span>

              <div className="quick-bets" role="group" aria-label="快捷注">
                {quickChips.map(({ chip, to }) => (
                  <button
                    key={chip.key}
                    type="button"
                    className={`quick-bet${chip.allIn ? ' is-allin' : ''}`}
                    data-quick={chip.key}
                    aria-label={chip.label}
                    aria-pressed={to !== null && raiseDraft !== '' && Number(raiseDraft) === to}
                    title={to === null ? '当前无合法加注' : `加注到 ${chips(to)}`}
                    disabled={!canAct || to === null}
                    onClick={() => {
                      if (to !== null) applyQuickTo(to)
                    }}
                  >
                    <span className="qb-ratio" aria-hidden="true">{chip.short}</span>
                    <span className="qb-to" aria-hidden="true">{to === null ? '—' : chips(to)}</span>
                  </button>
                ))}
              </div>
            </div>
          )}
        </div>
      )}
    </section>
  )
}
