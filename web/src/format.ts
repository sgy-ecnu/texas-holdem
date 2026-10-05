import type {
  ActionDTO,
  ActionKind,
  Availability,
  PlayerStatus,
  ProviderKind,
  Street,
} from './types'

export const STATUS_LABEL: Record<string, string> = {
  created: '已创建',
  running: '进行中',
  paused: '已暂停',
  stopped: '已停止',
  completed: '已完成',
  failed: '失败',
}

export const REASON_LABEL: Record<string, string> = {
  recovery: '崩溃恢复',
  cost_limit: '额度耗尽',
}

export const STREET_LABEL: Record<Street, string> = {
  preflop: '翻牌前',
  flop: '翻牌',
  turn: '转牌',
  river: '河牌',
  complete: '本手结束',
}

export const ACTION_LABEL: Record<ActionKind, string> = {
  fold: '弃牌',
  check: '过牌',
  call: '跟注',
  raise_to: '加注到',
}

export const PLAYER_STATUS_LABEL: Record<PlayerStatus, string> = {
  active: '参与',
  folded: '已弃牌',
  all_in: '全下',
  out: '出局',
}

export const SOURCE_LABEL: Record<string, string> = {
  mock: '模拟',
  model: '模型',
  fallback: '降级',
  human: '人工',
}

export const AVAILABILITY_LABEL: Record<Availability, string> = {
  mock: '模拟',
  configured: '已配置',
  unverified: '待验证',
  available: '可用',
  unavailable: '不可用',
  unsupported: '不支持',
}

export const PROVIDER_LABEL: Record<ProviderKind, string> = {
  mock: '模拟',
  openai_compatible: 'OpenAI 兼容',
  anthropic: 'Anthropic',
}

export const ERROR_CODE_LABEL: Record<string, string> = {
  MODEL_TIMEOUT: '模型超时',
  MODEL_AUTH_FAILED: '模型认证失败',
  MODEL_INVALID_ACTION: '模型动作非法',
  MODEL_PROTOCOL_ERROR: '模型协议错误',
  MODEL_CANCELED: '模型请求取消',
  MODEL_REQUEST_FAILED: '模型请求失败',
  MODEL_HTTP_ERROR: '模型服务响应异常',
  MODEL_BODY_READ_FAILED: '模型响应读取失败',
  MODEL_BODY_TOO_LARGE: '模型响应过大',
  MODEL_REQUEST_INVALID: '模型请求构建失败',
  MODEL_UNAVAILABLE: '模型不可用',
  UNSUPPORTED_PROVIDER: '不支持的服务商',
}

export const HAND_REASON_LABEL: Record<string, string> = {
  showdown: '摊牌比牌',
  uncontested: '无人跟注',
}

export function statusLabel(status: string): string {
  return STATUS_LABEL[status] ?? status
}

export function reasonLabel(reason: string | null): string | null {
  if (!reason) return null
  return REASON_LABEL[reason] ?? reason
}

export function errorCodeLabel(code: string): string {
  return ERROR_CODE_LABEL[code] ?? code
}

// 筹码只做展示格式化，不改变数值语义。
export function chips(n: number): string {
  return new Intl.NumberFormat('zh-CN').format(n)
}

// 座位旁临时动作标签的文案：
//   fold→「弃牌」、check→「过牌」、call→「跟注 {amount}」、raise_to→「加注到 {to}」。
// call 用本次实际投入额 amount；raise_to 用本街累计目标值 to。数值缺失时按 0 兜底，不显示 NaN。
export function actionPopoverLabel(action: ActionDTO): string {
  switch (action.kind) {
    case 'fold':
      return '弃牌'
    case 'check':
      return '过牌'
    case 'call':
      return `跟注 ${chips(action.amount ?? 0)}`
    case 'raise_to':
      return `加注到 ${chips(action.to)}`
  }
}

export function isTerminal(status: string): boolean {
  return status === 'stopped' || status === 'completed' || status === 'failed'
}

export const SUIT_GLYPH: Record<string, string> = {
  c: '♣',
  d: '♦',
  h: '♥',
  s: '♠',
}

export const SUIT_NAME: Record<string, string> = {
  c: '梅花',
  d: '方块',
  h: '红桃',
  s: '黑桃',
}

export interface ParsedCard {
  rank: string
  suit: string
  glyph: string
  red: boolean
}

// 牌面形如 "Ah" / "Ts" / "2c"；rank 取前段，suit 取末位。
export function parseCard(card: string): ParsedCard | null {
  if (card.length < 2) return null
  const suit = card.slice(-1).toLowerCase()
  const rank = card.slice(0, -1)
  const glyph = SUIT_GLYPH[suit]
  if (!glyph) return null
  return { rank, suit, glyph, red: suit === 'h' || suit === 'd' }
}

export function cardLabel(card: string): string {
  const parsed = parseCard(card)
  if (!parsed) return card
  return `${SUIT_NAME[parsed.suit] ?? parsed.suit}${parsed.rank}`
}
