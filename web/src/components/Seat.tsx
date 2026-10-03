import { chips, PLAYER_STATUS_LABEL } from '../format'
import type { ModelDTO, PlayerDTO, SeatDTO } from '../types'
import { PlayingCard } from './PlayingCard'

interface Props {
  seat: SeatDTO
  player: PlayerDTO | null
  model: ModelDTO | undefined
  isButton: boolean
  isSmallBlind: boolean
  isBigBlind: boolean
  isActor: boolean
  // 当前行动者座位上的醒目标签：「轮到我」/「思考中」；非行动者为 null。
  actorLabel: string | null
  // 有进行中的手牌且当前 actor 已确定时，非行动座位弱化，突出轮转位置。
  isDimmed: boolean
  isHuman: boolean
  // 真人本人座位在本手的两张底牌；其他座位恒为 undefined，只画牌背。
  holeCards?: string[]
  holeHidden: boolean
  index: number
}

// 座位卡：名称 / 模型 / 筹码 / 位置标记 / 行动状态。
// 手中以 hand.players[].stack 为实时余额，seat.stack 是最近结算余额。
export function Seat({
  seat,
  player,
  model,
  isButton,
  isSmallBlind,
  isBigBlind,
  isActor,
  actorLabel,
  isDimmed,
  isHuman,
  holeCards,
  holeHidden,
  index,
}: Props) {
  const live = player !== null
  const status = player?.status ?? 'active'
  const stack = live ? (player as PlayerDTO).stack : seat.stack
  const classes = ['seat']
  if (isActor) classes.push('is-actor')
  if (isHuman) classes.push('is-human')
  if (isDimmed) classes.push('is-dimmed')
  if (status === 'folded') classes.push('is-folded')
  if (status === 'out') classes.push('is-out')

  const hasOwnCards = holeCards !== undefined && holeCards.length > 0

  return (
    <div className={classes.join(' ')} style={{ animationDelay: `${index * 45}ms` }}>
      {isActor && actorLabel && (
        <span className="turn-badge" role="status">
          {actorLabel}
        </span>
      )}

      <div className="seat-tags">
        {isHuman && <span className="tag human">真人</span>}
        {isButton && <span className="tag dealer">D</span>}
        {isSmallBlind && <span className="tag blind">SB</span>}
        {isBigBlind && <span className="tag blind">BB</span>}
      </div>

      {hasOwnCards ? (
        <div className="hole hole-open" aria-label="我的底牌">
          {holeCards.map((c) => (
            <PlayingCard key={c} card={c} small />
          ))}
        </div>
      ) : (
        holeHidden && (
          <div className="hole" aria-hidden="true">
            <span className="mini-card" />
            <span className="mini-card back-2" />
          </div>
        )
      )}

      <div className="seat-head">
        <span className="seat-no">{seat.seat}</span>
        <span className="seat-name" title={seat.name}>
          {seat.name}
        </span>
      </div>
      <div className="seat-model" title={isHuman ? undefined : seat.model_ref}>
        {isHuman ? '真人玩家' : model ? model.label : seat.model_ref || '未绑定模型'}
      </div>
      {/* 醒目数字始终是「剩余筹码」（手中取 hand.players[].stack 实时余额，赛后取 seat.stack 结算余额）。 */}
      <div
        className="seat-stack"
        data-live={live ? 'true' : 'false'}
        title={live ? '剩余筹码 = 初始筹码 − 累计投入' : '最近结算筹码'}
      >
        {chips(stack)}
        <span className="unit">剩余</span>
      </div>

      {status !== 'active' && (
        <div style={{ marginTop: 6 }}>
          <span className={`tag status-${status === 'all_in' ? 'allin' : status}`}>
            {PLAYER_STATUS_LABEL[status]}
          </span>
        </div>
      )}

      {/* 手中同时标注「本轮下注」与「本手累计投入」：街切换会让本轮归零，累计不变，
          两者相加才是从初始筹码里被扣掉的部分，避免只看剩余产生「筹码没少」的错觉。 */}
      {live && player && (player.street_bet > 0 || player.committed > 0) && (
        <div className="bet" title="本轮下注 / 本手累计投入">
          <span className="mini-chip" />
          <span>
            本轮 {chips(player.street_bet)}
            <span className="bet-sep"> · </span>
            累计 {chips(player.committed)}
          </span>
        </div>
      )}
    </div>
  )
}
