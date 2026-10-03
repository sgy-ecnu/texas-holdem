import { statusLabel } from '../format'
import { formatRemaining, useCountdown } from '../hooks/useCountdown'
import type { GameDTO } from '../types'
import { HumanActionPanel } from './HumanActionPanel'

interface Props {
  game: GameDTO
  isPlayerView: boolean
  controlsBusy: boolean
  onBusyChange: (busy: boolean) => void
  onApply: (game: GameDTO) => void
  onRefresh: () => Promise<GameDTO>
}

type TurnKind = 'idle' | 'me' | 'ai' | 'human'

// 斗地主式底部行动区：当前行动者提示 + 倒计时/进度条 +（真人视角）操作按钮组。
// 轮转状态完全由已有 DTO 推导：hand.actor 决定轮到谁，waiting_for 区分真人/思考，
// 无需后端新增字段；AI 的「思考中」即 actor 已定但尚未进入 human_action 等待点。
export function ActionDock({
  game,
  isPlayerView,
  controlsBusy,
  onBusyChange,
  onApply,
  onRefresh,
}: Props) {
  const hand = game.hand
  const actor = hand?.actor ?? null
  const player = game.player
  const seatName = (seat: number) =>
    game.seats.find((s) => s.seat === seat)?.name ?? `座位 ${seat}`

  const myTurn =
    isPlayerView &&
    player !== null &&
    hand !== null &&
    game.status === 'running' &&
    actor === player.seat &&
    hand.waiting_for === 'human_action'

  // 仅真人等待点有权威截止时间；其余情况倒计时 inactive，展示「思考中」。
  const { remainingMs, totalMs, active, expired } = useCountdown(player)

  const thinking =
    hand !== null &&
    hand.street !== 'complete' &&
    game.status === 'running' &&
    actor !== null &&
    !myTurn

  const turnKind: TurnKind = myTurn
    ? 'me'
    : thinking
      ? actor === game.human_seat
        ? 'human'
        : 'ai'
      : 'idle'

  function stateText(): string {
    if (!hand || hand.street === 'complete') {
      return game.status === 'running' ? '本手结算中…' : statusLabel(game.status)
    }
    if (game.status === 'paused') return '牌局已暂停'
    if (game.status !== 'running') return statusLabel(game.status)
    if (myTurn) return '轮到你出牌'
    if (actor === null) return '等待行动'
    if (actor === game.human_seat) return '等待真人行动'
    return '思考中…'
  }

  const pct = active && totalMs > 0 ? Math.max(0, Math.min(100, (remainingMs / totalMs) * 100)) : 0
  const urgent = expired || remainingMs <= 10_000
  // 轮到自己时以第二人称呈现，避免与座位名（可能也叫 AI-x）混淆。
  const who = myTurn ? '你' : actor === null ? '—' : seatName(actor)

  return (
    <section
      className="hand-dock"
      data-turn={turnKind}
      aria-live="polite"
      aria-label="当前行动者与操作区"
    >
      <div className="dock-turn">
        <span className={`turn-orb ${turnKind}`} aria-hidden="true" />
        <span className="turn-who">{who}</span>
        <span className="dock-state">{stateText()}</span>
        {myTurn && active && (
          <span className={`countdown${urgent ? ' urgent' : ''}`}>
            {expired ? '已截止' : formatRemaining(remainingMs)}
          </span>
        )}
      </div>

      {myTurn && active && (
        <div
          className="dock-timer"
          role="progressbar"
          aria-valuemin={0}
          aria-valuemax={100}
          aria-valuenow={Math.round(pct)}
          aria-label="真人行动剩余时间"
        >
          <div className={`dock-timer-fill${urgent ? ' urgent' : ''}`} style={{ width: `${pct}%` }} />
        </div>
      )}
      {thinking && (
        <div className="dock-timer is-thinking" aria-hidden="true">
          <div className="dock-timer-fill" />
        </div>
      )}

      {isPlayerView && (
        <HumanActionPanel
          game={game}
          controlsBusy={controlsBusy}
          onBusyChange={onBusyChange}
          onApply={onApply}
          onRefresh={onRefresh}
          key="human-action"
        />
      )}
    </section>
  )
}
