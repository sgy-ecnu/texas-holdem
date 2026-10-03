import { useEffect, useState } from 'react'
import { ApiError, api } from '../api'
import {
  AVAILABILITY_LABEL,
  isTerminal,
  reasonLabel,
  statusLabel,
} from '../format'
import { useGamePolling, type PollMode } from '../hooks/useGamePolling'
import type { ActionDTO, Command, GameDTO, HealthDTO, ModelDTO } from '../types'
import { ActionDock } from './ActionDock'
import { ActionFeed } from './ActionFeed'
import { HandDetail } from './HandDetail'
import { PokerTable } from './PokerTable'

interface Props {
  initial: GameDTO
  models: ModelDTO[]
  health: HealthDTO | null
  onExit: () => void
}

interface StageProps extends Props {
  mode: PollMode
  onSwitchToPublic: () => void
}

// 单个视角的完整舞台。以 mode 作为 key 重建，切换视角时中止旧轮询、清空旧私牌。
function Stage({ initial, models, health, onExit, mode, onSwitchToPublic }: StageProps) {
  const { game, problem, updatedAt, refresh, apply } = useGamePolling(initial.id, initial, mode, {
    onUnauthorized: onSwitchToPublic,
  })
  const [pending, setPending] = useState<Command | null>(null)
  const [actionError, setActionError] = useState<string | null>(null)
  const [handOpen, setHandOpen] = useState(false)
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

  async function control(command: Command) {
    if (!game) return
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
    } catch (err) {
      setActionError(err instanceof ApiError ? err.message : '操作失败，请稍后重试。')
      if (err instanceof ApiError && err.status === 409) {
        // 状态冲突：拉取最新状态对齐，不臆测结果。
        void refresh().catch(() => undefined)
      }
    } finally {
      setPending(null)
    }
  }

  const stale = problem !== null

  return (
    <div className="container">
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
            <button
              className="btn btn-primary btn-sm"
              disabled={controlsBusy}
              onClick={() => control('start')}
            >
              {pending === 'start' ? '启动中…' : '开始'}
            </button>
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
            onClick={() => setHandOpen(true)}
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

      <div className={`table-wrap${stale ? ' is-stale' : ''}`}>
        <PokerTable game={game} models={models} player={game.player} />
        {stale && <div className="stale-tag">画面已过期 · 等待状态确认</div>}
      </div>

      <div className="lower-deck">
        <ActionDock
          game={game}
          isPlayerView={isPlayerView}
          controlsBusy={pending !== null}
          onBusyChange={setActing}
          onApply={apply}
          onRefresh={refresh}
        />

        <div className="play-grid">
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
            <div className="kv" style={{ flexDirection: 'column', gap: 8 }}>
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
            <div className="models-grid" style={{ marginTop: 12 }}>
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
      </div>

      {handOpen && (
        <HandDetail
          gameId={game.id}
          handsCompleted={game.hands_completed}
          seats={game.seats}
          initialHandNo={game.hands_completed}
          onClose={() => setHandOpen(false)}
        />
      )}
    </div>
  )
}

// 视角选择：创建浏览器持有真人 Cookie 时尝试 /player；403 则退回公共观战。
// 网络错误/503 保持真人视角并显示「状态未知」，绝不解释成「无真人权限」。
export function SpectatorView({ initial, models, health, onExit }: Props) {
  const [mode, setMode] = useState<PollMode>(() => (initial.human_seat !== null ? 'player' : 'public'))
  return (
    <Stage
      key={mode}
      initial={initial}
      models={models}
      health={health}
      onExit={onExit}
      mode={mode}
      onSwitchToPublic={() => setMode('public')}
    />
  )
}
