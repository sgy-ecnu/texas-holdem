import { useMemo } from 'react'
import { chips, STREET_LABEL } from '../format'
import type { GameDTO, ModelDTO, PlayerDTO, PlayerDTOView } from '../types'
import { PlayingCard } from './PlayingCard'
import { Seat } from './Seat'

interface Props {
  game: GameDTO
  models: ModelDTO[]
  // 真人视角才传入；其中 hole_cards 仅属于 player.seat，其他座位绝不补牌。
  player?: PlayerDTOView | null
}

// 椭圆牌桌：座位按角度环绕，中心为公共牌与底池。
export function PokerTable({ game, models, player = null }: Props) {
  const hand = game.hand
  const modelByRef = useMemo(() => {
    const map = new Map<string, ModelDTO>()
    for (const m of models) map.set(m.ref, m)
    return map
  }, [models])

  const playerBySeat = useMemo(() => {
    const map = new Map<number, PlayerDTO>()
    if (hand) for (const p of hand.players) map.set(p.seat, p)
    return map
  }, [hand])

  const count = game.seats.length || 1
  const actor = hand?.actor ?? null
  const humanSeat = player?.seat ?? null
  const humanCards = player?.hole_cards ?? []
  // 有进行中的手牌且 actor 已确定时，非行动座位弱化，让轮转位置一眼可见。
  const activeHand = hand !== null && hand.street !== 'complete'
  const focusActor = activeHand && actor !== null

  return (
    <div className="stage">
      <div className="rail">
        <div className="felt">
          <div className="table-word">Texas Hold'em</div>
          <div className="center">
            <div className="board" aria-label="公共牌">
              {Array.from({ length: 5 }).map((_, i) => {
                const card = hand?.board[i]
                return card ? (
                  <PlayingCard key={card} card={card} />
                ) : (
                  <div className="board-slot" key={`slot-${i}`} aria-hidden="true" />
                )
              })}
            </div>
            <div className="pot">
              <span className="pot-chip" aria-hidden="true" />
              <span className="caption">底池</span>
              <span className="value">{chips(hand?.pot ?? 0)}</span>
            </div>
            <div className="street-label">
              {hand ? STREET_LABEL[hand.street] : '等待开始'}
              {hand && hand.current_bet > 0 ? ` · 当前注 ${chips(hand.current_bet)}` : ''}
            </div>
          </div>
        </div>
      </div>

      <div className="seats">
        {game.seats.map((seat, i) => {
          const angle = (i / count) * Math.PI * 2 - Math.PI / 2
          const cx = 50 + 43 * Math.cos(angle)
          const cy = 50 + 40 * Math.sin(angle)
          const playerState = playerBySeat.get(seat.seat) ?? null
          const isHuman = humanSeat === seat.seat
          const isActor = actor === seat.seat
          // 非当前行动者、且仍在参与本手的座位弱化；弃牌/出局已有更弱的既有样式。
          const isDimmed = focusActor && !isActor && playerState?.status === 'active'
          // 「轮到我」仅本浏览器本人座位；其他行动座位（含观战视角的真人座位）显示思考态。
          const actorLabel = isActor
            ? isHuman
              ? '轮到我'
              : seat.seat === game.human_seat
                ? '等待真人'
                : '思考中'
            : null
          // 只有真人本人座位可显示自己的两张底牌；其余座位一律牌背。
          const ownCards = isHuman && humanCards.length === 2 ? humanCards : []
          const hasOwn = ownCards.length === 2
          // 未弃牌且本手进行中时，仅展示牌背占位（公共接口不下发底牌）。
          const holeHidden =
            !hasOwn &&
            hand !== null &&
            hand.street !== 'complete' &&
            playerState !== null &&
            playerState.status !== 'folded' &&
            playerState.status !== 'out'
          return (
            <div
              className={`seat-slot${isActor ? ' is-actor-slot' : ''}`}
              key={seat.seat}
              style={{ left: `${cx}%`, top: `${cy}%` }}
            >
              <Seat
                seat={seat}
                player={playerState}
                model={modelByRef.get(seat.model_ref)}
                isButton={hand !== null && hand.button === seat.seat}
                isSmallBlind={hand !== null && hand.small_blind === seat.seat}
                isBigBlind={hand !== null && hand.big_blind === seat.seat}
                isActor={isActor}
                actorLabel={actorLabel}
                isDimmed={isDimmed}
                isHuman={isHuman}
                holeCards={hasOwn ? ownCards : undefined}
                holeHidden={holeHidden}
                index={i}
              />
            </div>
          )
        })}
      </div>
    </div>
  )
}
