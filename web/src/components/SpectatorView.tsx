import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { ApiError, api } from '../api'
import {
  AVAILABILITY_LABEL,
  isTerminal,
  reasonLabel,
  statusLabel,
} from '../format'
import { useGamePolling, type PollMode } from '../hooks/useGamePolling'
import { useHandSettlement } from '../hooks/useHandSettlement'
import { usePrefersReducedMotion } from '../hooks/usePrefersReducedMotion'
import type { ActionDTO, Command, GameDTO, HealthDTO, ModelDTO } from '../types'
import { ActionDock } from './ActionDock'
import { ActionFeed } from './ActionFeed'
import { HINT_DURATION_MS, HINT_FALLBACK_MS, hintFromAction, type ActionHint } from './ActionPopover'
import { HandDetail } from './HandDetail'
import { PokerTable } from './PokerTable'
import { SettlementBar } from './SettlementBar'

interface Props {
  initial: GameDTO
  models: ModelDTO[]
  health: HealthDTO | null
  onExit: () => void
  // 进入观战时的附加提示（如「已创建、启动失败」）；成功进入为 null。
  notice?: string | null
}


interface StageProps extends Props {
  mode: PollMode
  onSwitchToPublic: () => void
}

// 单个视角的完整舞台。以 mode 作为 key 重建，切换视角时中止旧轮询、清空旧私牌。
function Stage({ initial, models, health, onExit, notice, mode, onSwitchToPublic }: StageProps) {
  const { game, problem, updatedAt, refresh, apply } = useGamePolling(initial.id, initial, mode, {
    onUnauthorized: onSwitchToPublic,
  })
  const [pending, setPending] = useState<Command | null>(null)
  const [actionError, setActionError] = useState<string | null>(null)
  const [handOpen, setHandOpen] = useState(false)
  // 详情弹层默认展示最近完成的一手；结算条「查看本手详情」可指定具体手数。
  const [detailHandNo, setDetailHandNo] = useState(1)
  const [acting, setActing] = useState(false)
  // 行动历史：后端只下发 last_action，前端按 revision/序列号累积成滚动序列（纯展示）。
  const [history, setHistory] = useState<ActionDTO[]>([])

  useEffect(() => {
    const action = game?.last_action
    if (!action) return
    setHistory((prev) => {
      const last = prev[prev.length - 1]
      if (last && last.hand_no === action.hand_no && last.sequence === action.sequence) {
        return prev
      }
      const next = [...prev, action]
      return next.length > 60 ? next.slice(next.length - 60) : next
    })
  }, [game?.revision, game?.last_action])

  // ---- 座位旁动作标签（临时提示：React 管生命周期，CSS 管动画） ----
  // 每座位一个 FIFO 队列：当前动作完整展示 2200ms，新动作按序等待；不同座位并行。
  // 同一动作 ID 不重复入队；不丢弃、不合并不同动作；跨手不截断已开始的标签。
  const [hints, setHints] = useState<Map<number, ActionHint>>(() => new Map())
  // 已观察到的最后一个动作序号，用于「只弹客户端新观察到、严格递增的动作」。
  const seenActionRef = useRef<{ handNo: number; sequence: number } | null>(null)
  const activeRef = useRef<Map<number, ActionHint>>(new Map()) // 每座位正在展示的动作
  const queueRef = useRef<Map<number, ActionHint[]>>(new Map()) // 每座位等待队列
  const timerRef = useRef<Map<number, number>>(new Map()) // 每座位兜底 timer
  const pumpRef = useRef<(seat: number) => void>(() => {})
  const reduced = usePrefersReducedMotion()
  // 正常动画由 animationend(~2200ms) 清理，此 timer 仅兜底；reduced-motion 无动画，按静态生命周期 2200ms 清理。
  const fallbackMs = reduced ? HINT_DURATION_MS : HINT_FALLBACK_MS

  // ---- 谁赢了结算：观察 hands_completed 变化，逐手拉取 CompletedHandDTO 展示 ----
  const settlement = useHandSettlement(game)
  const settlementView = useMemo(
    () =>
      settlement.hand
        ? { hand: settlement.hand, phase: settlement.phase, reduced }
        : null,
    [settlement.hand, settlement.phase, reduced],
  )
  const nameBySeat = useMemo(() => {
    const map = new Map<number, string>()
    for (const s of game?.seats ?? []) map.set(s.seat, s.name)
    return map
  }, [game?.seats])
  // 「收起」仅对当前这一手生效；下一手（新 key）自动重新展示。
  const [dismissedHand, setDismissedHand] = useState<string | null>(null)
  const settlementKey = settlement.hand ? `${settlement.hand.game_id}:${settlement.hand.hand_no}` : null
  const barVisible = settlement.hand !== null && settlementKey !== dismissedHand

  // 推进某座位：当前动作到点（或动画结束）后清掉，再从队列取下一个。
  const advance = useCallback((seat: number, id: string) => {
    const cur = activeRef.current.get(seat)
    if (!cur || cur.id !== id) return
    const t = timerRef.current.get(seat)
    if (t !== undefined) {
      window.clearTimeout(t)
      timerRef.current.delete(seat)
    }
    activeRef.current.delete(seat)
    setHints((prev) => {
      if (prev.get(seat)?.id !== id) return prev
      const next = new Map(prev)
      next.delete(seat)
      return next
    })
    pumpRef.current(seat)
  }, [])

  // 展示队列头部动作；该座位已有动作展示时不打断（严格 FIFO）。
  const pump = useCallback(
    (seat: number) => {
      if (activeRef.current.has(seat)) return
      const q = queueRef.current.get(seat)
      if (!q || q.length === 0) return
      const hint = q.shift() as ActionHint
      if (q.length === 0) queueRef.current.delete(seat)
      activeRef.current.set(seat, hint)
      setHints((prev) => {
        const next = new Map(prev)
        next.set(seat, hint)
        return next
      })
      const timer = window.setTimeout(() => advance(seat, hint.id), fallbackMs)
      timerRef.current.set(seat, timer)
    },
    [advance, fallbackMs],
  )
  useEffect(() => {
    pumpRef.current = pump
  }, [pump])

  // 入队某动作：同 ID 去重；已有动作展示时排队等待，不被截断。
  const popHint = useCallback(
    (hint: ActionHint) => {
      if (activeRef.current.get(hint.seat)?.id === hint.id) return
      const q = queueRef.current.get(hint.seat) ?? []
      if (q.some((h) => h.id === hint.id)) return
      q.push(hint)
      queueRef.current.set(hint.seat, q)
      pump(hint.seat)
    },
    [pump],
  )

  // 观察 last_action：首次（进入/切视角）只作基线不弹；仅 hand_no+sequence 严格递增才弹。
  // 不依赖 revision，重复轮询同一动作也不会重播。跨手不清空：已开始的标签完成剩余展示后自然推进。
  const gameId = game?.id
  const lastAction = game?.last_action ?? null
  useEffect(() => {
    if (!gameId || !lastAction) return
    const seen = seenActionRef.current
    const { hand_no: handNo, sequence } = lastAction
    if (seen === null) {
      seenActionRef.current = { handNo, sequence }
      return
    }
    const forward = handNo > seen.handNo || (handNo === seen.handNo && sequence > seen.sequence)
    if (!forward) return
    seenActionRef.current = { handNo, sequence }
    popHint(hintFromAction(gameId, lastAction))
  }, [gameId, lastAction, popHint])

  // 卸载时清掉所有未到期 timer，避免在已卸载组件上 setState。
  useEffect(() => {
    const timers = timerRef.current
    return () => {
      for (const t of timers.values()) window.clearTimeout(t)
      timers.clear()
    }
  }, [])

  if (!game) {
    return (
      <div className="container">
        <div className="empty">牌局数据不可用。</div>
      </div>
    )
  }

  const terminal = isTerminal(game.status)
  const canResume = game.status === 'paused' && game.reason !== 'cost_limit'
  const isPlayerView = mode === 'player' && game.player !== null
  // 控制命令与真人动作共用同一串行边界：任一进行中时都禁用另一侧，避免竞态。
  const controlsBusy = pending !== null || acting

  // 返回是否成功，供「先停止再返回」这类复合操作判断能否进入下一步。
  async function control(command: Command): Promise<boolean> {
    if (!game) return false
    setPending(command)
    setActionError(null)
    try {
      const next = await api.command(game.id, command)
      apply(next)
      // 命令返回公共状态；真人视角必须重新 GET /player 对齐私牌与合法动作，
      // 不得把旧私牌/旧 legal_actions 合并进来。见契约「七、App / useGamePolling」。
      if (mode === 'player') {
        try {
          await refresh()
        } catch {
          /* 下一轮轮询会继续对齐 */
        }
      }
      return true
    } catch (err) {
      setActionError(err instanceof ApiError ? err.message : '操作失败，请稍后重试。')
      if (err instanceof ApiError && err.status === 409) {
        // 状态冲突：拉取最新状态对齐，不臆测结果。
        void refresh().catch(() => undefined)
      }
      return false
    } finally {
      setPending(null)
    }
  }

  // 已创建未开始：先 stop 释放非终态锁，避免之后新建撞 409，成功后再返回配置页；
  // stop 失败则留在本页展示错误，不做二次跳转。
  async function discardAndExit() {
    if (await control('stop')) onExit()
  }

  const stale = problem !== null

  return (
    <div className="container container-play">
      {notice && (
        <div className="banner warn" role="alert">
          <span>
            <span className="banner-title">提示：</span>
            {notice}
          </span>
        </div>
      )}
      {problem && (
        <div className={`banner ${problem.kind === 'unknown' ? 'warn' : 'error'}`} role="alert">
          <span>
            <span className="banner-title">
              {problem.kind === 'unknown' ? '状态未知：' : '更新失败：'}
            </span>
            {problem.message}
          </span>
        </div>
      )}
      {actionError && (
        <div className="banner error" role="alert">
          {actionError}
        </div>
      )}
      {game.storage_degraded && (
        <div className="banner warn">
          存储降级：当前读取可能来自数据库回源，缓存不可信；最新状态以 revision 递增为准。
        </div>
      )}

      <div className="panel statusbar">
        <span className="chip-badge">
          <span className={`dot ${stale && problem?.kind === 'unknown' ? 'unknown' : game.status}`} />
          {stale && problem?.kind === 'unknown' ? '状态未知' : statusLabel(game.status)}
          {!stale && reasonLabel(game.reason) ? ` · ${reasonLabel(game.reason)}` : ''}
        </span>
        <span className={`chip-badge ${isPlayerView ? 'view-player' : 'view-public'}`}>
          {isPlayerView ? '真人视角' : '公共观战'}
        </span>
        <span className="chip-badge">{game.mode === 'mock' ? 'Mock 免费' : 'Live 付费'}</span>
        <span className="metric">
          手数 <b>{game.hands_completed}</b> / {game.max_hands}
        </span>
        {game.mode === 'live' && (
          <span className="metric">
            调用额度 <b>{game.live_calls_used}</b> / {game.max_live_calls}
          </span>
        )}
        <span className="metric">
          revision <b>{game.revision}</b>
        </span>
        {updatedAt && (
          <span className="metric nowrap">更新于 {new Date(updatedAt).toLocaleTimeString('zh-CN')}</span>
        )}
        <span className="grow" />
        <span className="controls">
          {game.status === 'created' && (
            <>
              <button
                className="btn btn-primary btn-sm"
                disabled={controlsBusy}
                onClick={() => control('start')}
              >
                {pending === 'start' ? '启动中…' : '开始'}
              </button>
              <button
                className="btn btn-danger btn-sm"
                disabled={controlsBusy}
                title="停止这个尚未开始的牌局并返回配置页"
                onClick={() => void discardAndExit()}
              >
                {pending === 'stop' ? '放弃中…' : '放弃并返回'}
              </button>
            </>
          )}
          {game.status === 'running' && (
            <>
              <button className="btn btn-sm" disabled={controlsBusy} onClick={() => control('pause')}>
                {pending === 'pause' ? '暂停中…' : '暂停'}
              </button>
              <button
                className="btn btn-danger btn-sm"
                disabled={controlsBusy}
                onClick={() => control('stop')}
              >
                {pending === 'stop' ? '停止中…' : '停止'}
              </button>
            </>
          )}
          {game.status === 'paused' && (
            <>
              <button
                className="btn btn-primary btn-sm"
                disabled={controlsBusy || !canResume}
                title={canResume ? undefined : '额度耗尽，本版无法追加预算，只能停止或另建牌局'}
                onClick={() => control('resume')}
              >
                {pending === 'resume' ? '恢复中…' : '恢复'}
              </button>
              <button
                className="btn btn-danger btn-sm"
                disabled={controlsBusy}
                onClick={() => control('stop')}
              >
                {pending === 'stop' ? '停止中…' : '停止'}
              </button>
            </>
          )}
          <button
            className="btn btn-sm"
            disabled={controlsBusy}
            onClick={() => void refresh().catch(() => undefined)}
          >
            刷新
          </button>
          <button
            className="btn btn-sm"
            disabled={game.hands_completed < 1}
            onClick={() => {
              setDetailHandNo(game.hands_completed)
              setHandOpen(true)
            }}
          >
            手牌详情
          </button>
          {terminal && (
            <button className="btn btn-sm" onClick={onExit}>
              新建牌局
            </button>
          )}
        </span>
      </div>

      {/* 牌桌居中；操作条紧贴牌桌正下方，一屏内无需下滑即可操作。 */}
      <div className="play-layout">
        <div className={`table-wrap${stale ? ' is-stale' : ''}`}>
          <PokerTable
            game={game}
            models={models}
            player={game.player}
            hints={hints}
            settlement={settlementView}
          />
          {stale && <div className="stale-tag">画面已过期 · 等待状态确认</div>}
        </div>

        {barVisible && settlement.hand && (
          <SettlementBar
            hand={settlement.hand}
            nameBySeat={nameBySeat}
            onView={() => {
              setDetailHandNo(settlement.hand!.hand_no)
              setHandOpen(true)
            }}
            onDismiss={() => setDismissedHand(settlementKey)}
          />
        )}

        <ActionDock
          game={game}
          isPlayerView={isPlayerView}
          controlsBusy={pending !== null}
          onBusyChange={setActing}
          onApply={apply}
          onRefresh={refresh}
        />
      </div>

      {/* 次要面板：行动历史与运行信息放在操作条之后，不抢占操作条的可见性。 */}
      <div className="play-meta">
        <div className="panel panel-pad">
          <h2 className="section-title">行动历史</h2>
          <p className="section-hint">
            按发生顺序滚动展示已公开动作，新动作会短暂高亮；不含任何未结束手牌的底牌信息。
          </p>
          <ActionFeed actions={history} seats={game.seats} />
        </div>

        <div className="panel panel-pad">
          <h2 className="section-title">运行信息</h2>
          <p className="section-hint">存储与模型可用性；接口失败时显示明确文案，不以前一帧冒充成功。</p>
          <div className="kv info-kv">
            <span>
              <b>牌局 ID</b>
              <span className="mono"> {game.id}</span>
            </span>
            <span>
              <b>真人座位</b>{' '}
              {game.human_seat === null ? '无（全 AI）' : `座位 ${game.human_seat}`}
            </span>
            <span>
              <b>存储</b>{' '}
              {game.storage_degraded ? '降级（缓存不可信，已回源数据库）' : '正常'}
              {health ? `（MySQL：${health.mysql} / Redis：${health.redis}）` : ''}
            </span>
            <span>
              <b>付费模型授权</b> {health?.allow_paid_models ? '允许' : '禁止'}
            </span>
          </div>
          <div className="models-grid info-models">
            {models.map((m) => (
              <div className="model-card" key={m.ref}>
                <div className="ref" title={m.ref}>
                  {m.ref}
                </div>
                <div className="meta">
                  <span className={`avail ${m.availability}`}>{AVAILABILITY_LABEL[m.availability]}</span>
                </div>
              </div>
            ))}
          </div>
        </div>
      </div>

      {handOpen && (
        <HandDetail
          gameId={game.id}
          handsCompleted={game.hands_completed}
          seats={game.seats}
          initialHandNo={detailHandNo}
          onClose={() => setHandOpen(false)}
        />
      )}
    </div>
  )
}

// 视角选择：创建浏览器持有真人 Cookie 时尝试 /player；403 则退回公共观战。
// 网络错误/503 保持真人视角并显示「状态未知」，绝不解释成「无真人权限」。
export function SpectatorView({ initial, models, health, onExit, notice }: Props) {
  const [mode, setMode] = useState<PollMode>(() => (initial.human_seat !== null ? 'player' : 'public'))
  return (
    <Stage
      key={mode}
      initial={initial}
      models={models}
      health={health}
      onExit={onExit}
      notice={notice}
      mode={mode}
      onSwitchToPublic={() => setMode('public')}
    />
  )
}
