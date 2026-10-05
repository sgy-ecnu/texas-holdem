import { chips, modelShortLabel, PLAYER_STATUS_LABEL } from '../format'
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
  // 右半侧座位把牌背镜像到右侧，保证底牌始终朝桌外生长。
  flipHole?: boolean
  // 结算模式：以 CompletedHandDTO 为准展示摊牌底牌/角色/最终筹码，关闭行动态与下注行。
  settling?: boolean
  // 摊牌要翻开的底牌（仅 showdown 参与者）；无则不渲染，弃牌者不翻牌。
  settlementCards?: string[]
  settlementRole?: 'winner' | 'loser' | null
  // 该座位翻牌起始延迟（ms），按座位顺序错峰。
  settlementDelayMs?: number
  // 结算期间以该手 CompletedHandDTO.final_stacks 覆盖显示筹码。
  finalStack?: number | null
  index: number
}

// 座位卡：压扁成固定两行。
//   第一行 = 座位号 + 名字 + 身份/状态/位置标记（真人 / D / SB / BB / 弃牌 / 全下 / 出局）
//   第二行 = 剩余筹码（模型名降级：默认只进 title，宽屏才占位显示）
// 「本轮/累计」已拆到桌沿徽章（见 PokerTable .rim-mark）；此处不再逐座位长出高度。
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
  flipHole = false,
  settling = false,
  settlementCards,
  settlementRole = null,
  settlementDelayMs = 0,
  finalStack = null,
  index,
}: Props) {
  const live = player !== null
  const status = player?.status ?? 'active'
  const stack = finalStack != null ? finalStack : live ? (player as PlayerDTO).stack : seat.stack
  const classes = ['seat']
  if (isActor) classes.push('is-actor')
  if (isHuman) classes.push('is-human')
  if (isDimmed) classes.push('is-dimmed')
  if (flipHole) classes.push('is-right')
  if (settling) classes.push('is-settling')
  if (settlementRole === 'winner') classes.push('is-winner')
  if (settlementRole === 'loser') classes.push('is-loser')
  if (status === 'folded') classes.push('is-folded')
  if (status === 'out') classes.push('is-out')

  const hasOwnCards = holeCards !== undefined && holeCards.length > 0
  const hasSettlementCards = settlementCards !== undefined && settlementCards.length > 0
  const modelLabel = isHuman ? '真人玩家' : model ? model.label : seat.model_ref || '未绑定模型'
  const modelRef = model?.ref ?? seat.model_ref ?? ''
  // 只显示已知模型的短名；未知模型返回 null（座位卡省略，完整信息留在 title）。
  const shortModel = isHuman ? null : modelShortLabel(modelRef)
  const statusKey = status !== 'active' ? (status === 'all_in' ? 'allin' : status) : null

  return (
    <div
      className={classes.join(' ')}
      style={{ animationDelay: `${index * 45}ms` }}
      title={`${seat.name} · ${modelLabel}`}
    >
      {!settling && isActor && actorLabel && (
        <span className="turn-badge" role="status">
          {actorLabel}
        </span>
      )}

      {hasSettlementCards ? (
        <div
          className={`hole hole-reveal is-${settlementRole ?? 'neutral'}`}
          aria-label={settlementRole === 'winner' ? '赢家底牌' : '摊牌底牌'}
        >
          {settlementCards!.map((c) => (
            <span className="settle-flip" key={`${seat.seat}-${c}`} style={{ animationDelay: `${settlementDelayMs}ms` }}>
              <span className="settle-flip-inner">
                <span className="settle-back card card-back" aria-hidden="true" />
                <span className="settle-face">
                  <PlayingCard card={c} small />
                </span>
              </span>
            </span>
          ))}
        </div>
      ) : hasOwnCards ? (
        <div className="hole hole-open" aria-label="我的底牌">
          {holeCards.map((c) => (
            <PlayingCard key={c} card={c} small />
          ))}
        </div>
      ) : (
        holeHidden &&
        !settling && (
          <div className="hole" aria-hidden="true">
            <span className="mini-card" />
            <span className="mini-card back-2" />
          </div>
        )
      )}

      <div className="seat-row1">
        <span className="seat-no">{seat.seat}</span>
        <span className="seat-name" title={seat.name}>
          {seat.name}
        </span>
        {/* 模型短名紧跟玩家名；未知/mock 等未收录模型整组不渲染（含括号），完整 label 见 title。 */}
        {shortModel && (
          <span className="seat-model" title={modelLabel}>
            ({shortModel})
          </span>
        )}
        <span className="seat-tags">
          {isHuman && <span className="tag human">真人</span>}
          {isButton && <span className="tag dealer">D</span>}
          {isSmallBlind && <span className="tag blind">SB</span>}
          {isBigBlind && <span className="tag blind">BB</span>}
          {statusKey && (
            <span className={`tag status-${statusKey}`}>{PLAYER_STATUS_LABEL[status]}</span>
          )}
        </span>
      </div>

      {/* 醒目数字始终是「剩余筹码」（手中取 hand.players[].stack 实时余额，赛后取 seat.stack 结算余额）。 */}
      <div className="seat-row2">
        <span
          className="seat-stack"
          data-live={live ? 'true' : 'false'}
          title={live ? '剩余筹码 = 初始筹码 − 累计投入' : '最近结算筹码'}
        >
          {chips(stack)}
          <span className="unit">剩余</span>
        </span>
      </div>

      {/* 移动端网格没有桌沿徽章：本轮/累计仅在此显示（桌面端用 .rim-mark）；结算期间下注行淡出。 */}
      {!settling && live && player && (player.street_bet > 0 || player.committed > 0) && (
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
