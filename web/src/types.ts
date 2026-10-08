// 与 internal/server/dto.go 严格一一对应的 JSON 契约类型。
// 字段名全部保持后端 snake_case，不做驼峰映射，避免契约漂移。

export type Mode = 'mock' | 'live'
export type Command = 'start' | 'pause' | 'resume' | 'stop'
export type ProviderKind = 'mock' | 'openai_compatible' | 'anthropic'
export type Availability =
  | 'mock'
  | 'configured'
  | 'unverified'
  | 'available'
  | 'unavailable'
  | 'unsupported'
export type PlayerStatus = 'active' | 'folded' | 'all_in' | 'out'
export type Street = 'preflop' | 'flop' | 'turn' | 'river' | 'complete'
export type ActionKind = 'fold' | 'check' | 'call' | 'raise_to'
// 座位类型：ai 为默认值，human 为本浏览器真人。见设计契约「二、座位契约」。
export type SeatKind = 'ai' | 'human'
// 当前手牌等待点；仅 human_action 一种，服务端等待真人提交动作。
export type WaitingFor = 'human_action'

export interface SeatDTO {
  seat: number
  name: string
  model_ref: string
  stack: number
  kind: SeatKind
}

export interface PlayerDTO {
  seat: number
  stack: number
  street_bet: number
  committed: number
  status: PlayerStatus
}

export interface HandDTO {
  number: number
  street: Street
  button: number
  small_blind: number
  big_blind: number
  actor: number | null
  board: string[]
  pot: number
  current_bet: number
  players: PlayerDTO[]
  // 公共与真人视角都会下发：服务端当前是否在等待真人动作。
  waiting_for: WaitingFor | null
}

// 真人视角合法动作集合；仅 GET /player 的本人响应中非 null。见契约「四、REST 与 JSON DTO」。
export interface LegalActionsDTO {
  can_fold: boolean
  can_check: boolean
  can_call: boolean
  call_amount: number
  can_raise: boolean
  min_raise_to: number
  max_raise_to: number
  short_all_in_to: number | null
}

// 真人私有视角：仅本浏览器持有游戏级 Cookie 时下发，绝不进入公共 GameDTO。
// 公共接口该字段为 null；本人未开手时 hole_cards=[]、hand_no=null。
export interface PlayerDTOView {
  seat: number
  hand_no: number | null
  hole_cards: string[]
  legal_actions: LegalActionsDTO | null
  action_deadline: string | null
  server_time: string
}

export interface ActionDTO {
  hand_no: number
  sequence: number
  seat: number
  kind: ActionKind
  to: number
  // 本次动作的实际投入额：fold/check 为 0，call 为本次补差跟注额，raise_to 为追加额。
  // 前端仅用于座位旁动作标签的「跟注 {amount}」文案，展示语义。
  amount: number
  source: string
  error_code: string | null
  input_tokens: number | null
  output_tokens: number | null
}

export interface GameDTO {
  id: string
  revision: number
  status: string
  reason: string | null
  mode: Mode
  max_hands: number
  hands_completed: number
  live_calls_used: number
  max_live_calls: number
  storage_degraded: boolean
  seats: SeatDTO[]
  hand: HandDTO | null
  last_action: ActionDTO | null
  // 真人座位号；全 AI 牌局为 null。公共接口也会下发，用于判断是否尝试真人视角。
  human_seat: number | null
  // 仅真人本人视角非 null；公共接口恒为 null。见契约「三、真人身份与私牌接口」。
  player: PlayerDTOView | null
}

export interface AmountDTO {
  seat: number
  amount: number
}

export interface StackDTO {
  seat: number
  stack: number
}

export interface HoleDTO {
  seat: number
  cards: [string, string]
}

export interface PotDTO {
  amount: number
  eligible: number[]
  winners: number[]
  payouts: AmountDTO[]
}

// 摊牌结算的每座位展示：牌型中文名与最佳五张牌；仅 reason=="showdown" 时非空。
export interface ShowdownDTO {
  seat: number
  hand_rank: string
  best_five: string[]
}

export interface CompletedHandDTO {
  game_id: string
  hand_no: number
  reason: string
  button: number
  board: string[]
  hole_cards: HoleDTO[]
  refunds: AmountDTO[]
  payouts: AmountDTO[]
  pots: PotDTO[]
  final_stacks: StackDTO[]
  showdown: ShowdownDTO[]
}

export interface ModelDTO {
  ref: string
  label: string
  provider: ProviderKind
  availability: Availability
  error_code: string | null
}

export interface ModelsDTO {
  models: ModelDTO[]
}

export interface HealthDTO {
  mysql: string
  redis: string
  storage_degraded: boolean
  allow_paid_models: boolean
  max_live_calls: number
}

export interface ErrorDTO {
  error: { code: string; message: string }
}

// POST /api/v1/games 请求体；所有字段显式提交，不做隐式默认。
// 真人座位 kind=human 且省略 model_ref；AI 座位 kind=ai 且必须带 model_ref。
export interface CreateSeatRequest {
  seat: number
  name: string
  stack: number
  kind: SeatKind
  model_ref?: string
}

export interface CreateGameRequest {
  seats: CreateSeatRequest[]
  button: number
  small_blind: number
  big_blind: number
  max_hands: number
  mode: Mode
  max_live_calls: number
}

// POST /api/v1/games/{id}/action 请求体；raise_to 的 to 表示本街累计投入目标值。
export interface HumanActionRequest {
  expected_revision: number
  kind: ActionKind
  to?: number
}

// ---------------------------------------------------------------------------
// 牌桌配置模板（阶段 2 后端已实现，字段与 internal/server/dto.go 一一对应）。
// 模板是可复用配置：保存时只存「哪个位置是真人」，不绑定任何真人会话/Cookie。
// ---------------------------------------------------------------------------

// 模板座位公共视图；真人座位 model_ref 为空字符串，kind=human。
export interface TableSeatDTO {
  seat: number
  name: string
  model_ref: string
  stack: number
  kind: SeatKind
}

// 完整模板配置；对应后端 TableConfigDTO。
export interface TableConfigDTO {
  seats: TableSeatDTO[]
  button: number
  small_blind: number
  big_blind: number
  max_hands: number
  mode: Mode
  max_live_calls: number
}

// GET /api/v1/tables/{id} 与 PUT/POST/clear 响应。
export interface TableDTO {
  id: string
  name: string
  revision: number
  // null 表示「未配置」空态（清空后即此状态），不是默认 2 人局。
  config: TableConfigDTO | null
  created_at: string
  updated_at: string
}

// 列表摘要：不含完整 seats、不含 owner/会话。
export interface TableSummaryDTO {
  id: string
  name: string
  revision: number
  configured: boolean
  seat_count: number
  small_blind: number
  big_blind: number
  human_seat: number | null
  created_at: string
  updated_at: string
}

export interface TableListDTO {
  tables: TableSummaryDTO[]
  next_cursor: string | null
}

// 模板写入请求：真人座位省略 model_ref，AI 座位必须携带 model_ref。
export interface TableSeatInput {
  seat: number
  name: string
  stack: number
  kind: SeatKind
  model_ref?: string
}

// 完整模板配置请求体（PUT 全量替换）。与 CreateGameRequest 结构同构。
export interface TableConfigInput {
  seats: TableSeatInput[]
  button: number
  small_blind: number
  big_blind: number
  max_hands: number
  mode: Mode
  max_live_calls: number
}

export interface CreateTableRequest {
  name: string
  // 允许 null：创建「未配置」空模板。
  config: TableConfigInput | null
}

export interface UpdateTableRequest {
  expected_revision: number
  name: string
  config: TableConfigInput | null
}
