import { useEffect, useMemo, useRef, useState } from 'react'
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

// 视觉槽位：以椭圆半径的归一化偏移表示（dx/right、dy/down，范围 [-1,1]）。
// 圆心 (0,0) → stage (50,50)；半径 RX/RY 依实测 stage 尺寸自适应。
interface Slot {
  dx: number
  dy: number
}

// 命名槽位（按需组合成人数模板）。
const B: Slot = { dx: 0, dy: 1 } // 底部（6 点钟）
const T: Slot = { dx: 0, dy: -1 } // 顶部
const L: Slot = { dx: -1, dy: 0 } // 正左
const R: Slot = { dx: 1, dy: 0 } // 正右
const UL: Slot = { dx: -0.72, dy: -0.7 } // 左上
const UR: Slot = { dx: 0.72, dy: -0.7 } // 右上
const LL: Slot = { dx: -0.66, dy: 0.86 } // 左下
const LR: Slot = { dx: 0.66, dy: 0.86 } // 右下
const L2L: Slot = { dx: -0.98, dy: 0.42 } // 左列·下
const L2U: Slot = { dx: -0.9, dy: -0.42 } // 左列·上
const R2U: Slot = { dx: 0.9, dy: -0.42 } // 右列·上
const R2L: Slot = { dx: 0.98, dy: 0.42 } // 右列·下
const T2L: Slot = { dx: -0.36, dy: -1 } // 顶排·左
const T2R: Slot = { dx: 0.36, dy: -1 } // 顶排·右
const T3L: Slot = { dx: -0.46, dy: -0.95 }
const T3M: Slot = { dx: 0, dy: -1 }
const T3R: Slot = { dx: 0.46, dy: -0.95 }
const T4L: Slot = { dx: -0.64, dy: -0.9 }
const T4ML: Slot = { dx: -0.27, dy: -1 }
const T4MR: Slot = { dx: 0.27, dy: -1 }
const T4R: Slot = { dx: 0.64, dy: -0.9 }

// 人数模板：槽位按「从底部起、顺时针（θ 递增：底→左→顶→右）」排列。
// 座位号相邻关系由纯映射保留；弃牌/出局不改变模板（不重排，防跳动）。
const TEMPLATES: Record<number, Slot[]> = {
  2: [B, T],
  3: [B, UL, UR],
  4: [B, L, T, R],
  5: [B, L, UL, UR, R],
  6: [B, LL, UL, T, UR, LR],
  7: [B, L2L, L2U, T2L, T2R, R2U, R2L],
  8: [B, L2L, L2U, T3L, T3M, T3R, R2U, R2L],
  9: [B, L2L, L2U, T4L, T4ML, T4MR, T4R, R2U, R2L],
}

// 椭圆牌桌：中央公共牌/底池；桌沿徽章；外圈座位卡。
export function PokerTable({ game, models, player = null }: Props) {
  const hand = game.hand
  // 实测 stage 尺寸，用于把槽位映射到实际像素、避免窄屏溢出。
  const stageRef = useRef<HTMLDivElement | null>(null)
  const [stageSize, setStageSize] = useState({ w: 0, h: 0 })
  useEffect(() => {
    const el = stageRef.current
    if (!el) return
    const measure = () => setStageSize({ w: el.clientWidth, h: el.clientHeight })
    measure()
    const ro = new ResizeObserver(measure)
    ro.observe(el)
    return () => ro.disconnect()
  }, [])

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

  const actor = hand?.actor ?? null
  // 定位用的真人座位取公共字段 game.human_seat：观战视角也应把真人摆在底部居中。
  // 「本人」私牌/高亮另由 player.seat 决定，二者在真人视角下相同、观战视角下 player 为 null。
  const humanSeat = game.human_seat ?? null
  const viewerSeat = player?.seat ?? null
  const humanCards = player?.hole_cards ?? []
  // 有进行中的手牌且 actor 已确定时，非行动座位弱化，让轮转位置一眼可见。
  const activeHand = hand !== null && hand.street !== 'complete'
  const focusActor = activeHand && actor !== null

  // 座位 → 槽位：真人座位映射到底部 B，其余座位按座位号顺时针（纯旋转）依次占位。
  const layout = useMemo(() => {
    const seats = [...game.seats].sort((a, b) => a.seat - b.seat).map((s) => s.seat)
    const n = seats.length || 1
    const tpl = TEMPLATES[n] ?? [{ dx: 0, dy: 1 } as Slot]
    let ring: number[]
    if (humanSeat !== null && seats.includes(humanSeat)) {
      const h = seats.indexOf(humanSeat)
      ring = [...seats.slice(h), ...seats.slice(0, h)]
    } else {
      ring = seats // 全 AI：座位号升序，首座落底部，顺时针铺开。
    }
    const slotBySeat = new Map<number, Slot>()
    ring.forEach((seatNo, k) => slotBySeat.set(seatNo, tpl[k] ?? B))
    return slotBySeat
  }, [game.seats, humanSeat])

  // 槽位 → stage 百分比的坐标；半径依实测尺寸自适应（窄屏自动收，避免溢出/重叠）。
  const geometry = useMemo(() => {
    const clamp = (v: number, lo: number, hi: number) => Math.min(hi, Math.max(lo, v))
    // 座位卡半宽/半高（含少量余量），用于把卡心推到尽量靠外而不过度溢出 stage。
    const HALF_X = 72
    const HALF_Y = 42
    const MARGIN_X = 22 // 允许的横向溢出像素（落在容器左右内边距内，不产生横向滚动）
    const MARGIN_Y = 4 // 允许的纵向溢出像素（落在牌桌与操作条之间的间隙内）
    const RX =
      stageSize.w > 0 ? clamp(((stageSize.w / 2 - HALF_X + MARGIN_X) / stageSize.w) * 100, 30, 47) : 46
    const RY =
      stageSize.h > 0 ? clamp(((stageSize.h / 2 - HALF_Y + MARGIN_Y) / stageSize.h) * 100, 28, 45) : 44
    const seatAt = (s: Slot) => ({ cx: 50 + s.dx * RX, cy: 50 + s.dy * RY })
    // 桌沿徽章（本轮下注）比座位更靠里，落在护栏内侧外围带；底部槽位避开真人底牌再往里收。
    const rimAt = (s: Slot) => {
      const t = s.dy > 0.9 ? 0.62 : 0.85
      return { cx: 50 + s.dx * t * RX, cy: 50 + s.dy * t * RY }
    }
    return { seatAt, rimAt }
  }, [stageSize.w, stageSize.h])

  return (
    <div className="stage" ref={stageRef}>
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

      {/* 桌沿层：本街下注徽章（本轮 X + 次要的累计 Y），紧贴所属座位、护栏内侧外围带。 */}
      <div className="rim-marks" aria-hidden="true">
        {game.seats.map((seat) => {
          const slot = layout.get(seat.seat)
          const p = playerBySeat.get(seat.seat)
          if (!slot || !p || (p.street_bet <= 0 && p.committed <= 0)) return null
          const pos = geometry.rimAt(slot)
          return (
            <div
              className="rim-mark"
              key={`rim-${seat.seat}`}
              style={{ left: `${pos.cx}%`, top: `${pos.cy}%` }}
            >
              <span className="mini-chip" />
              <span className="rim-now">本轮 {chips(p.street_bet)}</span>
              {p.committed > 0 && <span className="rim-cum">累计 {chips(p.committed)}</span>}
            </div>
          )
        })}
      </div>

      <div className="seats">
        {game.seats.map((seat, i) => {
          const slot = layout.get(seat.seat) ?? B
          const pos = geometry.seatAt(slot)
          const playerState = playerBySeat.get(seat.seat) ?? null
          const isHuman = viewerSeat === seat.seat
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
              style={{ left: `${pos.cx}%`, top: `${pos.cy}%` }}
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
                flipHole={pos.cx > 55}
                index={i}
              />
            </div>
          )
        })}
      </div>
    </div>
  )
}
