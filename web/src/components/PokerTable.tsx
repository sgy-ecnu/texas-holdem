import { useEffect, useLayoutEffect, useMemo, useRef, useState, type CSSProperties } from 'react'
import { chips } from '../format'
import {
  bestFiveBreakdown,
  finalStackBySeat,
  humanSidePotIndex,
  payoutTotalBySeat,
  showdownParticipantSeats,
  winnerSeats,
  type SettlementPhase,
} from '../settlement'
import type { CompletedHandDTO, GameDTO, ModelDTO, PlayerDTO, PlayerDTOView } from '../types'
import { ActionPopover, type ActionHint, type ActionPlacement } from './ActionPopover'
import { PlayingCard } from './PlayingCard'
import { Seat } from './Seat'
import { StreetTrack } from './StreetTrack'
import { WinnerTag } from './WinnerTag'

// 结算展示（Task 2）：结算期间牌桌的公共牌/底牌/获奖/最终筹码全部来自该手 CompletedHandDTO。
export interface PokerTableSettlement {
  hand: CompletedHandDTO
  phase: SettlementPhase
  reduced: boolean
}

interface Props {
  game: GameDTO
  models: ModelDTO[]
  // 真人视角才传入；其中 hole_cards 仅属于 player.seat，其他座位绝不补牌。
  player?: PlayerDTOView | null
  // 按座位索引的临时动作标签（Stage 管理生命周期）；缺省为空，仅展示用。
  hints?: Map<number, ActionHint>
  // 单个动作标签动画自然结束回调（供 Stage 的每座位 FIFO 队列推进）。
  onHintDone?: (seat: number, id: string) => void
  // 谁赢了结算（非 null 时进入独立结算模式，抑制动作标签与本街下注徽章）。
  settlement?: PokerTableSettlement | null
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

// 下注徽章（.rim-mark）定位所需固定像素尺寸（与 styles.css 对齐，不随 stage 缩放）：
// 座位卡 140×58；真人明牌 .hole-open .card 宽 42、高约 58.8，底边在座位卡上方 7px。
const SEAT_HALF_W = 70
const SEAT_HALF_H = 29
// 真人明牌自座位中心向上的突出：29(卡半高) + 7(缝隙) + 58.8(明牌高) ≈ 95，取 96 留余量。
const OPEN_HOLE_UP = 96
const MARK_HALF_H = 12
const MARK_GAP = 6
// 未测到徽章实际宽度前的保守半宽；测得后按实际 offsetWidth/2 精确定位。
const MARK_HALF_W_DEFAULT = 90
// 护栏（.felt）椭圆半径：felt 宽 = 86.8% stage 宽；高 = 89% stage 高 − 2.2% stage 宽
// （.rail padding 百分比按包含块宽度计算），故半径分别为 0.434W、(0.445H − 0.011W)。
const FELT_RX_FRAC = 0.434

// 椭圆牌桌：中央公共牌/底池；桌沿徽章；外圈座位卡。
export function PokerTable({ game, models, player = null, hints, onHintDone, settlement = null }: Props) {
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

  // 下注徽章层与各座位徽章实测宽度：宽度决定水平推出距离，读一次真实 offsetWidth 即可确定性收敛。
  const marksRef = useRef<HTMLDivElement | null>(null)
  const [markWidths, setMarkWidths] = useState<Map<number, number>>(new Map())
  useLayoutEffect(() => {
    const root = marksRef.current
    if (!root) return
    const next = new Map<number, number>()
    root.querySelectorAll<HTMLElement>('.rim-mark').forEach((el) => {
      const raw = el.dataset.seat
      if (raw != null) next.set(Number(raw), el.offsetWidth)
    })
    setMarkWidths((prev) => {
      if (prev.size === next.size && [...next].every(([seat, w]) => prev.get(seat) === w)) return prev
      return next
    })
  })

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

  // ---- 结算展示：独立模式，数据全部取自该手 CompletedHandDTO ----
  const settled = settlement?.hand ?? null
  const settling = settled !== null
  const winnerSet = useMemo(() => (settled ? winnerSeats(settled) : new Set<number>()), [settled])
  const payoutMap = useMemo(() => (settled ? payoutTotalBySeat(settled) : new Map<number, number>()), [settled])
  const finalMap = useMemo(() => (settled ? finalStackBySeat(settled) : new Map<number, number>()), [settled])
  const participantOrder = useMemo(
    () => (settled && settled.reason === 'showdown' ? showdownParticipantSeats(settled) : []),
    [settled],
  )
  const participantSet = useMemo(() => new Set(participantOrder), [participantOrder])
  const holeBySeat = useMemo(() => {
    const map = new Map<number, string[]>()
    if (settled) for (const h of settled.hole_cards) map.set(h.seat, h.cards)
    return map
  }, [settled])
  const rankBySeat = useMemo(() => {
    const map = new Map<number, string>()
    if (settled) for (const s of settled.showdown) map.set(s.seat, s.hand_rank)
    return map
  }, [settled])
  const breakdownBySeat = useMemo(() => {
    const map = new Map<number, string | null>()
    if (settled) for (const s of settled.showdown) map.set(s.seat, bestFiveBreakdown(s.best_five))
    return map
  }, [settled])
  const leaving = settlement?.phase === 'finished'
  const showChips = settlement?.phase === 'award' || settlement?.phase === 'hold'
  // 有进行中的手牌且 actor 已确定时，非行动座位弱化，让轮转位置一眼可见；结算期间不弱化。
  const activeHand = hand !== null && hand.street !== 'complete'
  const focusActor = activeHand && actor !== null && !settling

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
    const W = stageSize.w
    const H = stageSize.h
    // 座位卡半宽/半高（含少量余量），用于把卡心推到尽量靠外而不过度溢出 stage。
    const HALF_X = 72
    const HALF_Y = 42
    const MARGIN_X = 22 // 允许的横向溢出像素（落在容器左右内边距内，不产生横向滚动）
    const MARGIN_Y = 4 // 允许的纵向溢出像素（落在牌桌与操作条之间的间隙内）
    const RX =
      W > 0 ? clamp(((W / 2 - HALF_X + MARGIN_X) / W) * 100, 30, 47) : 46
    const RY =
      H > 0 ? clamp(((H / 2 - HALF_Y + MARGIN_Y) / H) * 100, 28, 45) : 44
    const seatAt = (s: Slot) => ({ cx: 50 + s.dx * RX, cy: 50 + s.dy * RY })
    // 桌沿徽章（本轮下注）定位：从座位中心沿「指向桌心」的轴对齐方向推出，直到
    // 座位卡（含真人明牌）与徽章沿该轴完全分离，并确保护栏（felt）内侧。
    // 轴对齐（顶点/底点竖推、侧点横推）保证相邻徽章在另一轴的间距不被缩短、彼此不叠压。
    const rimAt = (s: Slot, openHole: boolean, halfW: number) => {
      // 尚未测得 stage 尺寸时的初始占位（首帧后即被实测值覆盖）。
      const t = s.dy > 0.9 ? 0.62 : 0.85
      if (W <= 0 || H <= 0) return { cx: 50 + s.dx * t * RX, cy: 50 + s.dy * t * RY }
      const sx = ((50 + s.dx * RX) / 100) * W
      const sy = ((50 + s.dy * RY) / 100) * H
      let ax = 0
      let ay = 0
      if (s.dy > 0.9) ay = -1 // 正下方 → 向上收
      else if (s.dy < -0.9) ay = 1 // 正上方 → 向下收
      else if (Math.abs(s.dx) > Math.abs(s.dy) * 1.25) ax = s.dx < 0 ? 1 : -1 // 偏侧 → 水平收
      else ay = s.dy < 0 ? 1 : -1 // 偏上/偏下 → 垂直收
      let obstacle = ax !== 0 ? SEAT_HALF_W : SEAT_HALF_H
      // 真人明牌在座位卡正上方：仅当向上推出时需额外越过明牌高度。
      if (openHole && ay < 0) obstacle = Math.max(obstacle, OPEN_HOLE_UP)
      const badgeHalf = ax !== 0 ? halfW : MARK_HALF_H
      let dist = obstacle + badgeHalf + MARK_GAP
      // 护栏内侧约束：把徽章中心（含自身半尺寸余量）推进 felt 椭圆内。
      const fx = FELT_RX_FRAC * W
      const fy = 0.445 * H - 0.011 * W
      if (ax !== 0) {
        const allowed = fx * Math.sqrt(Math.max(0, 1 - ((sy - H / 2) / fy) ** 2))
        dist = Math.max(dist, Math.abs(sx - W / 2) - (allowed - halfW))
      } else {
        const allowed = fy * Math.sqrt(Math.max(0, 1 - ((sx - W / 2) / fx) ** 2))
        dist = Math.max(dist, Math.abs(sy - H / 2) - (allowed - MARK_HALF_H))
      }
      return { cx: ((sx + ax * dist) / W) * 100, cy: ((sy + ay * dist) / H) * 100 }
    }
    return { seatAt, rimAt }
  }, [stageSize.w, stageSize.h, markWidths])

  const board = settled ? settled.board : (hand?.board ?? [])
  const potValue = settled ? settled.pots.reduce((sum, p) => sum + p.amount, 0) : (hand?.pot ?? 0)
  const trackStreet = settled ? 'complete' : (hand?.street ?? null)
  const trackBet = settled ? 0 : (hand?.current_bet ?? 0)
  const trackBoardLength = board.length
  const trackReason = settled ? settled.reason : null
  const trackHandNo = settled ? settled.hand_no : (hand?.number ?? null)

  return (
    <div className={`stage${settling ? ' is-settling' : ''}`} data-settlement={settlement?.phase ?? 'idle'} ref={stageRef}>
      <div className="rail">
        <div className="felt">
          <div className="table-word">Texas Hold'em</div>
          <div className="center">
            <div className="board" aria-label="公共牌">
              {Array.from({ length: 5 }).map((_, i) => {
                const card = board[i]
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
              <span className="value">{chips(potValue)}</span>
            </div>
            <div className="street-label">
              <StreetTrack
                street={trackStreet}
                boardLength={trackBoardLength}
                currentBet={trackBet}
                status={game.status}
                completedReason={trackReason}
                handNumber={trackHandNo}
              />
            </div>
          </div>
        </div>
      </div>

      {/* 桌沿层：本街下注徽章；结算期间整体淡出，只保留该手结算快照。 */}
      <div className={`rim-marks${settling ? ' is-settling' : ''}`} aria-hidden="true" ref={marksRef}>
        {!settling &&
          game.seats.map((seat) => {
          const slot = layout.get(seat.seat)
          const p = playerBySeat.get(seat.seat)
          if (!slot || !p || (p.street_bet <= 0 && p.committed <= 0)) return null
          // 本人座位且已发两张明牌时，徽章需越过明牌高度（观战视角无明牌，按普通座位处理）。
          const openHole = viewerSeat === seat.seat && humanCards.length === 2
          const halfW = (markWidths.get(seat.seat) ?? MARK_HALF_W_DEFAULT * 2) / 2
          const pos = geometry.rimAt(slot, openHole, halfW)
          return (
            <div
              className="rim-mark"
              key={`rim-${seat.seat}`}
              data-seat={seat.seat}
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
          const isActor = !settling && actor === seat.seat
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
          // 只有真人本人座位可显示自己的两张底牌；其余座位一律牌背。结算期间改由该手快照揭示。
          const ownCards = !settling && isHuman && humanCards.length === 2 ? humanCards : []
          const hasOwn = ownCards.length === 2
          // 未弃牌且本手进行中时，仅展示牌背占位（公共接口不下发底牌）；结算期间不画牌背。
          const holeHidden =
            !settling &&
            !hasOwn &&
            hand !== null &&
            hand.street !== 'complete' &&
            playerState !== null &&
            playerState.status !== 'folded' &&
            playerState.status !== 'out'
          // 座位旁临时动作标签：与 <Seat> 同级（不放进 .seat），避免被 is-dimmed/is-folded 降透明度。
          const hint = settling ? undefined : hints?.get(seat.seat)
          // 结算角色：摊牌参与者翻牌；赢家亮金、输家底牌降透明（姓名/筹码仍可读）。
          const isWinner = winnerSet.has(seat.seat)
          const isParticipant = participantSet.has(seat.seat)
          const settlementCards =
            settled && isParticipant ? holeBySeat.get(seat.seat) : undefined
          const settlementRole: 'winner' | 'loser' | null = settled
            ? isWinner
              ? 'winner'
              : isParticipant
                ? 'loser'
                : null
            : null
          const settlementDelayMs = isParticipant ? 250 + participantOrder.indexOf(seat.seat) * 60 : 0
          const payoutSum = payoutMap.get(seat.seat) ?? 0
          // 底部真人座位＝座位右侧动作；观战与真人视角同规则（按 game.human_seat，不按 viewer）。
          const isBottomHuman = humanSeat !== null && seat.seat === humanSeat
          const isBottomCenter = slot.dy > 0.9 && Math.abs(slot.dx) < 0.01
          const place: ActionPlacement =
            isBottomHuman || isBottomCenter
              ? 'right'
              : slot.dx < -0.001
                ? 'above-left'
                : slot.dx > 0.001
                  ? 'above-right'
                  : 'above-center'
          // 筹码向赢家飞：起点在牌桌中心，用相对座位的像素偏移表示。
          const flyX = stageSize.w > 0 ? ((50 - pos.cx) / 100) * stageSize.w : 0
          const flyY = stageSize.h > 0 ? ((50 - pos.cy) / 100) * stageSize.h : 0
          const showFly = settling && isWinner && payoutSum > 0 && showChips
          return (
            <div
              className={`seat-slot${isActor ? ' is-actor-slot' : ''}${hint ? ' has-hint' : ''}`}
              key={seat.seat}
              data-seat={seat.seat}
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
                settling={settling}
                settlementCards={settlementCards}
                settlementRole={settlementRole}
                settlementDelayMs={settlementDelayMs}
                finalStack={settled ? finalMap.get(seat.seat) ?? null : null}
                index={i}
              />
              {showFly && (
                <span
                  className="settle-chips"
                  aria-hidden="true"
                  style={{ '--fly-x': `${flyX}px`, '--fly-y': `${flyY}px` } as CSSProperties}
                >
                  {[0, 1, 2].map((k) => (
                    <span className="settle-chip" key={k} style={{ animationDelay: `${k * 90}ms` }} />
                  ))}
                </span>
              )}
              {settled && isWinner && payoutSum > 0 && (
                <WinnerTag
                  placement={place}
                  amount={payoutSum}
                  rank={rankBySeat.get(seat.seat) ?? null}
                  breakdown={breakdownBySeat.get(seat.seat) ?? null}
                  isMe={viewerSeat === seat.seat}
                  sidePotIndex={viewerSeat === seat.seat ? humanSidePotIndex(settled, seat.seat) : null}
                  uncontested={settled.reason !== 'showdown'}
                  leaving={leaving}
                />
              )}
              {hint && (
                <ActionPopover
                  key={hint.id}
                  hint={hint}
                  placement={place}
                  onDone={(id) => onHintDone?.(seat.seat, id)}
                />
              )}
            </div>
          )
        })}
      </div>
    </div>
  )
}
