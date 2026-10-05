import { useEffect, useMemo, useRef, useState, type CSSProperties } from 'react'
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
import { ActionPopover, type ActionAnchor, type ActionHint, type ActionPlacement } from './ActionPopover'
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

// 座位卡固定像素尺寸（与 styles.css 对齐，不随 stage 缩放）：140×58。
const SEAT_HALF_W = 70
const SEAT_HALF_H = 29
// 椭圆布局下座位卡的「整卡宽度」与「窄桌面紧凑宽度」。
// 顶部相邻座位横向间距小于整卡宽 → 卡会重叠：先切紧凑宽度（.is-narrow-seats），
// 紧凑后仍放不下才退化为列表；桌面宽度不再因「顶部下注条」而切列表。
const SEAT_W_FULL = SEAT_HALF_W * 2 // 140
const SEAT_W_COMPACT = 108
// 座位外侧下注附属条的外围包络（自座位卡边到条的「外缘」）。
// 侧向：AI 牌背外探 ~14px + 间隙 ~7px + 两行条宽上限 ~110px ≈ 131（含最长金额）。
// 纵向：行动状态标签/留白 ~19px + 单行条高 ~24px + 余量 ≈ 54。
// 用于把椭圆半径内收，给「座位卡+底牌+下注条+行动标签」的组合包络留出真实可测的外围空间。
const ENVELOPE_SIDE = 131
// 仅上/下向（无侧向座位）时的横向包络：座位卡边 + AI 牌背外探/间隙，无需为侧向两行条预留。
const ENVELOPE_SIDE_NONE = 21
const ENVELOPE_VERT = 54
// 横向可借用的 stage→视口余量上限（超过则触发紧凑化，不靠缩字号）。
const SLACK_X_MAX = 110
// 窄桌面紧凑化阈值（stage 实测宽度，px）。
const TIGHT_STAGE_W = 1000
// 顶部相邻「上向」下注条放不下一行「完整条（本轮 + 累计）」的阈值（约 160px）。
// 相邻间距由当前人数的实际槽位模板算出（见 minTopNeighborDx）：
// 人少时顶部座位稀疏、间距大，8/9 人顶部密集（T3/T4）才可能触发。
// 触发时只让顶部下注条自身紧凑化（隐藏累计、收字号/内边距），绝不据此切整个列表布局。
const TOP_GAP_MIN = 160
// 极窄 stage（移动端）直接退化为列表布局的阈值；此外仅当「椭圆座位卡无法避免重叠」
// （seatsCollide，见 geometry）才切列表。桌面宽度下的顶部下注条拥挤只做紧凑化，不切列表。
const LIST_MIN_STAGE_W = 520

// 外侧下注条方向：仅由槽位 dx/dy 决定（与金额/行动状态无关）。
// |dx| > 1.25×|dy| → 左/右；否则 → 上/下（临界优先上下）。
// 正上/左上/右上 → 上；正左/左 → 左；正右/右 → 右；左下/右下/正下 → 下。
type BarDir = 'up' | 'down' | 'left' | 'right'
function barDir(s: Slot): BarDir {
  if (Math.abs(s.dx) > Math.abs(s.dy) * 1.25) return s.dx < 0 ? 'left' : 'right'
  return s.dy < 0 ? 'up' : 'down'
}

// 当前座位集里「上向」下注条相邻槽位的最小水平间距（归一化 dx）。
// 顶部只有 0/1 个上向座位（2/4 人）时返回 Infinity（永不触发顶部拥挤退化）；
// 6 人 UL/T/UR 间距 0.72、7 人 T2L/T2R 0.72、8 人 T3 三座 0.46、9 人 T4 四座 0.37。
function minTopNeighborDx(slots: Slot[]): number {
  const dxs = slots
    .filter((s) => barDir(s) === 'up')
    .map((s) => s.dx)
    .sort((a, b) => a - b)
  let min = Infinity
  for (let i = 1; i < dxs.length; i++) min = Math.min(min, dxs[i] - dxs[i - 1])
  return min
}

// 是否存在侧向（左/右向）下注条座位：只有此时才需要为侧向两行条预留 131px 外侧包络，
// 否则该横向上限会无谓压缩椭圆半径（如 2/3/6 人，顶部/底部座位不需要横向包络）。
function hasSideBars(slots: Slot[]): boolean {
  return slots.some((s) => {
    const d = barDir(s)
    return d === 'left' || d === 'right'
  })
}

// 椭圆牌桌：中央公共牌/底池；桌沿徽章；外圈座位卡。
export function PokerTable({ game, models, player = null, hints, onHintDone, settlement = null }: Props) {
  const hand = game.hand
  // 实测 stage 尺寸，以及 stage 到视口左右边的真实余量（用于把外侧下注条放进容器留白，
  // 而非硬编码固定值；余量随视口真实变化）。
  const stageRef = useRef<HTMLDivElement | null>(null)
  const [stageSize, setStageSize] = useState({ w: 0, h: 0, slackX: 0 })
  useEffect(() => {
    const el = stageRef.current
    if (!el) return
    const measure = () => {
      const rect = el.getBoundingClientRect()
      const slackX = Math.max(0, Math.min(rect.left, window.innerWidth - rect.right))
      setStageSize((prev) =>
        prev.w === el.clientWidth && prev.h === el.clientHeight && prev.slackX === slackX
          ? prev
          : { w: el.clientWidth, h: el.clientHeight, slackX },
      )
    }
    measure()
    const ro = new ResizeObserver(measure)
    ro.observe(el)
    window.addEventListener('resize', measure)
    return () => {
      ro.disconnect()
      window.removeEventListener('resize', measure)
    }
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

  // 槽位 → stage 百分比的坐标；半径依实测尺寸自适应，并为外侧下注条预留外围包络。
  const geometry = useMemo(() => {
    const clamp = (v: number, lo: number, hi: number) => Math.min(hi, Math.max(lo, v))
    const W = stageSize.w
    const H = stageSize.h
    const slots = [...layout.values()]
    // 座位中心到组合包络外缘的距离：座位卡半尺寸 + 外侧下注条包络。
    // 侧向包络仅在存在侧向座位时计入，避免 2/3/6 人局被无谓压缩椭圆半径（桌子偏小）。
    const HALF_X = SEAT_HALF_W + (hasSideBars(slots) ? ENVELOPE_SIDE : ENVELOPE_SIDE_NONE)
    const HALF_Y = SEAT_HALF_H + ENVELOPE_VERT
    // 横向借用 stage 到视口边的真实余量（上限 SLACK_X_MAX），让桌子尽量大又不产生横向滚动。
    const slackX = W > 0 ? Math.min(stageSize.slackX, SLACK_X_MAX) : 0
    const MARGIN_Y = 4
    const RX = W > 0 ? clamp(((W / 2 - HALF_X + slackX) / W) * 100, 26, 47) : 44
    const RY = H > 0 ? clamp(((H / 2 - HALF_Y + MARGIN_Y) / H) * 100, 26, 45) : 42
    const seatAt = (s: Slot) => ({ cx: 50 + s.dx * RX, cy: 50 + s.dy * RY })
    // 顶部「上向」相邻槽位的水平像素间距（归一化 dx × 横向半径）。
    // 该间距同时决定两件事：下注条能否放下完整一行条、以及相邻座位卡是否会横向重叠。
    const RXpx = (RX / 100) * W
    const topDxPx = minTopNeighborDx(slots) * RXpx
    // (1) 顶部下注条放不下完整一行条（本轮 + 累计）→ 只紧凑化下注条（隐藏累计），绝不切列表。
    const topCramped = W > 0 && topDxPx < TOP_GAP_MIN
    // (2) 座位卡横向重叠是「真窄屏」才切列表的唯一几何依据（与下注条无关）：
    //     间距放不下整卡 → 先切紧凑宽度窗位；紧凑后仍放不下 → 退化为列表单列。
    const ovalSeatW = topDxPx < SEAT_W_FULL ? SEAT_W_COMPACT : SEAT_W_FULL
    const seatsCollide = W > 0 && topDxPx < ovalSeatW
    // 列表布局只由「真窄屏退化」触发：极窄 stage（移动端）或椭圆座位卡已无法避免重叠。
    // 桌面宽度（1000px+ 视口）8/9 人在紧凑座位卡下仍保持椭圆。
    const isList = W > 0 && (W < LIST_MIN_STAGE_W || seatsCollide)
    // 窄桌面紧凑座位卡：椭圆布局且需收窄才不重叠时启用（不改高度、不缩特效）。
    const narrowSeats = !isList && W > 0 && ovalSeatW === SEAT_W_COMPACT
    return { seatAt, stageW: W, isList, topCramped, narrowSeats }
  }, [stageSize.w, stageSize.h, stageSize.slackX, layout])

  const board = settled ? settled.board : (hand?.board ?? [])
  const potValue = settled ? settled.pots.reduce((sum, p) => sum + p.amount, 0) : (hand?.pot ?? 0)
  const trackStreet = settled ? 'complete' : (hand?.street ?? null)
  const trackBet = settled ? 0 : (hand?.current_bet ?? 0)
  const trackBoardLength = board.length
  const trackReason = settled ? settled.reason : null
  const trackHandNo = settled ? settled.hand_no : (hand?.number ?? null)

  return (
    <div
      className={`stage${settling ? ' is-settling' : ''}${geometry.isList ? ' is-list' : ''}${geometry.narrowSeats ? ' is-narrow-seats' : ''}${geometry.topCramped ? ' is-top-cramped' : ''}${stageSize.w > 0 && stageSize.w < TIGHT_STAGE_W ? ' is-tight' : ''}`}
      data-settlement={settlement?.phase ?? 'idle'}
      ref={stageRef}
    >
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

      <div className="seats">
        {game.seats.map((seat, i) => {
          const slot = layout.get(seat.seat) ?? B
          const pos = geometry.seatAt(slot)
          // 外侧下注条方向：仅由槽位 dx/dy 决定，不随金额/行动状态变化。
          const dir = barDir(slot)
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
          // 动作标签锚定边（内侧固定贴边）：顶部座位贴卡片下沿居中（向下朝桌内展开）；
          // 其余座位一律贴向桌心一侧，与外侧下注条方向相反——正左/左下/正下贴右侧、正右/右下贴左侧。
          // 这样标签紧贴所属座位卡、尖角指向卡片，不再落进座位之间的同一片空隙。
          const anchor: ActionAnchor = dir === 'up' ? 'below' : slot.dx > 0.001 ? 'left' : 'right'
          // 筹码向赢家飞：起点在牌桌中心，用相对座位的像素偏移表示。
          const flyX = stageSize.w > 0 ? ((50 - pos.cx) / 100) * stageSize.w : 0
          const flyY = stageSize.h > 0 ? ((50 - pos.cy) / 100) * stageSize.h : 0
          const showFly = settling && isWinner && payoutSum > 0 && showChips
          return (
            <div
              className={`seat-slot${isActor ? ' is-actor-slot' : ''}${hint ? ' has-hint' : ''}`}
              key={seat.seat}
              data-seat={seat.seat}
              data-dir={dir}
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
              {/* 座位外侧附属条：与 <Seat> 同级（不放进 .seat，避免跟随座位卡弱化透明度）。
                  方向由槽位决定（is-up/down/left/right），紧贴座位表达归属、不加引导线、不重复名字。 */}
              {!settling && playerState && (playerState.street_bet > 0 || playerState.committed > 0) && (
                <div
                  className={`rim-mark is-${dir}`}
                  data-seat={seat.seat}
                  data-dir={dir}
                  title={`本轮 ${chips(playerState.street_bet)} · 累计 ${chips(playerState.committed)}`}
                  aria-hidden="true"
                >
                  <span className="rim-now">本轮 {chips(playerState.street_bet)}</span>
                  {playerState.committed > 0 && <span className="rim-cum">累计 {chips(playerState.committed)}</span>}
                </div>
              )}
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
                  anchor={anchor}
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
