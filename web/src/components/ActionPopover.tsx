import { actionPopoverLabel } from '../format'
import type { ActionDTO, ActionKind } from '../types'

// 座位旁临时动作标签的数据：id 是动作唯一键（game.id + hand_no + sequence），
// 既作 React key 触发重播，也作旧 timer 清理时的归属校验；text 是展示文案；
// kind 只决定配色（四种动作尺寸 / 时长 / 时间轴完全一致），不解析中文文案来推断动作。
export interface ActionHint {
  id: string
  seat: number
  text: string
  kind: ActionKind
}

// 静态展示生命周期（reduced-motion 无动画时按此时长静态可读）。
export const HINT_DURATION_MS = 2200
// 动画兜底移除：正常动画 2200ms，animationend 正常清理；此值仅兜底，不得小于动画时长。
export const HINT_FALLBACK_MS = 2300

export function hintFromAction(gameId: string, action: ActionDTO): ActionHint {
  return {
    id: `${gameId}:${action.hand_no}:${action.sequence}`,
    seat: action.seat,
    text: actionPopoverLabel(action),
    kind: action.kind,
  }
}

// 四色动作类：颜色只区分动作，尺寸 / 时长 / 时间轴四种动作完全一致。
const KIND_CLASS: Record<ActionKind, string> = {
  fold: 'fold',
  check: 'check',
  call: 'call',
  raise_to: 'raise',
}

// 展开方向（不是简单 center bottom）：
//   above-left   左半桌  → origin right bottom，向上 / 向左展开
//   above-right  右半桌  → origin left bottom，向上 / 向右展开
//   above-center 正顶部单座 → origin center bottom，向上 / 左右对称
//   right        底部真人 → origin left center，向右展开
export type ActionPlacement = 'above-left' | 'above-right' | 'above-center' | 'right'

// 标签锚定边（内侧固定贴边）：明确标签贴住所属座位卡的哪条边，并决定短尖角朝向与缩放原点。
//   below → 贴卡片下沿居中，尖角朝上（顶部座位；向下朝桌内展开，避开上方常驻下注条）。
//   right → 贴卡片右侧、垂直居中，尖角朝左（正左/左下/正下座位；朝桌心一侧，避开上方底牌）。
//   left  → 贴卡片左侧、垂直居中，尖角朝右（正右/右下座位；镜像）。
// placement 仍保留给赢家标签与列表布局回退；动作标签定位改由 anchor 决定，避免标签浮在座位间隙。
export type ActionAnchor = 'below' | 'right' | 'left'

interface Props {
  hint: ActionHint
  placement: ActionPlacement
  anchor: ActionAnchor
  // 单次动画（2200ms）自然结束回调，供每座位 FIFO 队列推进；reduced-motion 下不触发，靠 timer。
  onDone?: (id: string) => void
}

// 纯展示：四种动作统一 scale(1.8)、总时长 2200ms、80ms 淡入、320ms 后 600ms 缓慢缩回、停留、淡出；
// 不循环、不闪烁、不回弹。贴边位置由 anchor 类决定、配色由 kind 类决定；placement 仅作列表布局回退。
// aria-hidden：语义宣读由 ActionFeed（role=log）承担，避免重复播报。
export function ActionPopover({ hint, placement, anchor, onDone }: Props) {
  return (
    <span
      className={`action-popover is-${KIND_CLASS[hint.kind]} is-${placement} anchor-${anchor}`}
      aria-hidden="true"
      onAnimationEnd={() => onDone?.(hint.id)}
    >
      {hint.text}
    </span>
  )
}
