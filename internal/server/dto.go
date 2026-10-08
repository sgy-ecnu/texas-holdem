package server

import (
	"bytes"
	"encoding/json"
	"sort"
	"texas-holdem/internal/agent"
	"texas-holdem/internal/poker"
	"texas-holdem/internal/service"
	"time"
)

type SeatDTO struct {
	Seat     poker.Seat       `json:"seat"`
	Name     string           `json:"name"`
	ModelRef string           `json:"model_ref"`
	Stack    poker.Chips      `json:"stack"`
	Kind     service.SeatKind `json:"kind"`
}
type PlayerDTO struct {
	Seat      poker.Seat         `json:"seat"`
	Stack     poker.Chips        `json:"stack"`
	StreetBet poker.Chips        `json:"street_bet"`
	Committed poker.Chips        `json:"committed"`
	Status    poker.PlayerStatus `json:"status"`
}
type HandDTO struct {
	Number     uint32       `json:"number"`
	Street     poker.Street `json:"street"`
	Button     poker.Seat   `json:"button"`
	SmallBlind poker.Seat   `json:"small_blind"`
	BigBlind   poker.Seat   `json:"big_blind"`
	Actor      *poker.Seat  `json:"actor"`
	Board      []poker.Card `json:"board"`
	Pot        poker.Chips  `json:"pot"`
	CurrentBet poker.Chips  `json:"current_bet"`
	Players    []PlayerDTO  `json:"players"`
	// WaitingFor 运行中等待真人行动时为 "human_action"，其余为 null。
	WaitingFor *string `json:"waiting_for"`
}
type ActionDTO struct {
	HandNo       uint32           `json:"hand_no"`
	Sequence     uint64           `json:"sequence"`
	Seat         poker.Seat       `json:"seat"`
	Kind         poker.ActionKind `json:"kind"`
	To           poker.Chips      `json:"to"`
	Amount       poker.Chips      `json:"amount"`
	Source       string           `json:"source"`
	ErrorCode    *string          `json:"error_code"`
	InputTokens  *int64           `json:"input_tokens"`
	OutputTokens *int64           `json:"output_tokens"`
}
type GameDTO struct {
	ID              string       `json:"id"`
	Revision        uint64       `json:"revision"`
	Status          string       `json:"status"`
	Reason          *string      `json:"reason"`
	Mode            service.Mode `json:"mode"`
	MaxHands        uint32       `json:"max_hands"`
	HandsCompleted  uint32       `json:"hands_completed"`
	LiveCallsUsed   uint32       `json:"live_calls_used"`
	MaxLiveCalls    uint32       `json:"max_live_calls"`
	StorageDegraded bool         `json:"storage_degraded"`
	Seats           []SeatDTO    `json:"seats"`
	Hand            *HandDTO     `json:"hand"`
	LastAction      *ActionDTO   `json:"last_action"`
	// HumanSeat 是公共信息；Player 只在真人本人视角非空，公共响应恒为 null。
	HumanSeat *poker.Seat    `json:"human_seat"`
	Player    *PlayerDTOView `json:"player"`
}

// PlayerDTOView 是真人本人可见的私有对象；公共转换函数绝不填充它。
type PlayerDTOView struct {
	Seat           poker.Seat       `json:"seat"`
	HandNo         *uint32          `json:"hand_no"`
	HoleCards      []poker.Card     `json:"hole_cards"`
	LegalActions   *LegalActionsDTO `json:"legal_actions"`
	ActionDeadline *time.Time       `json:"action_deadline"`
	ServerTime     time.Time        `json:"server_time"`
}

type LegalActionsDTO struct {
	CanFold      bool         `json:"can_fold"`
	CanCheck     bool         `json:"can_check"`
	CanCall      bool         `json:"can_call"`
	CallAmount   poker.Chips  `json:"call_amount"`
	CanRaise     bool         `json:"can_raise"`
	MinRaiseTo   poker.Chips  `json:"min_raise_to"`
	MaxRaiseTo   poker.Chips  `json:"max_raise_to"`
	ShortAllInTo *poker.Chips `json:"short_all_in_to"`
}
type AmountDTO struct {
	Seat   poker.Seat  `json:"seat"`
	Amount poker.Chips `json:"amount"`
}
type StackDTO struct {
	Seat  poker.Seat  `json:"seat"`
	Stack poker.Chips `json:"stack"`
}
type HoleDTO struct {
	Seat  poker.Seat    `json:"seat"`
	Cards [2]poker.Card `json:"cards"`
}
type PotDTO struct {
	Amount   poker.Chips `json:"amount"`
	Eligible []int       `json:"eligible"`
	Winners  []int       `json:"winners"`
	Payouts  []AmountDTO `json:"payouts"`
}

// ShowdownDTO 是摊牌结算的每座位展示：牌型中文名与最佳五张牌。
// 仅 reason=="showdown" 时非空；弃牌结束（uncontested）不产生牌型。
type ShowdownDTO struct {
	Seat     poker.Seat   `json:"seat"`
	HandRank string       `json:"hand_rank"`
	BestFive []poker.Card `json:"best_five"`
}
type CompletedHandDTO struct {
	GameID      string        `json:"game_id"`
	HandNo      uint32        `json:"hand_no"`
	Reason      string        `json:"reason"`
	Button      poker.Seat    `json:"button"`
	Board       []poker.Card  `json:"board"`
	HoleCards   []HoleDTO     `json:"hole_cards"`
	Refunds     []AmountDTO   `json:"refunds"`
	Payouts     []AmountDTO   `json:"payouts"`
	Pots        []PotDTO      `json:"pots"`
	FinalStacks []StackDTO    `json:"final_stacks"`
	Showdown    []ShowdownDTO `json:"showdown"`
}
type ModelDTO struct {
	Ref          string             `json:"ref"`
	Label        string             `json:"label"`
	Provider     agent.ProviderKind `json:"provider"`
	Availability agent.Availability `json:"availability"`
	ErrorCode    *string            `json:"error_code"`
}
type ModelsDTO struct {
	Models []ModelDTO `json:"models"`
}
type HealthDTO struct {
	MySQL           string `json:"mysql"`
	Redis           string `json:"redis"`
	StorageDegraded bool   `json:"storage_degraded"`
	AllowPaidModels bool   `json:"allow_paid_models"`
	MaxLiveCalls    uint32 `json:"max_live_calls"`
}

// 请求用指针区分缺字段与合法零值，不允许浏览器提交内部状态。
// Kind/ModelRef 用 RawMessage 以区分「省略」与「显式 null」。
type CreateSeatDTO struct {
	Seat     *poker.Seat     `json:"seat"`
	Name     *string         `json:"name"`
	Stack    *poker.Chips    `json:"stack"`
	ModelRef json.RawMessage `json:"model_ref"`
	Kind     json.RawMessage `json:"kind"`
}

// HumanActionDTO 是真人动作请求体；expected_revision 必填且 >0。
type HumanActionDTO struct {
	ExpectedRevision *uint64           `json:"expected_revision"`
	Kind             *poker.ActionKind `json:"kind"`
	To               *poker.Chips      `json:"to"`
}
type CreateGameDTO struct {
	Seats        []CreateSeatDTO `json:"seats"`
	Button       *poker.Seat     `json:"button"`
	SmallBlind   *poker.Chips    `json:"small_blind"`
	BigBlind     *poker.Chips    `json:"big_blind"`
	MaxHands     *uint32         `json:"max_hands"`
	Mode         *service.Mode   `json:"mode"`
	MaxLiveCalls *uint32         `json:"max_live_calls"`
}
type CommandDTO struct {
	Command service.Command `json:"command"`
}
type ErrorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
type ErrorDTO struct {
	Error ErrorDetail `json:"error"`
}

// Game 只产生公共视角：Player 恒为 nil，绝不包含任何底牌。
func Game(v service.GameView) GameDTO {
	d := GameDTO{ID: v.ID, Revision: v.Revision, Status: v.Status, Reason: v.Reason, Mode: v.Mode, MaxHands: v.MaxHands, HandsCompleted: v.HandsCompleted, LiveCallsUsed: v.LiveCallsUsed, MaxLiveCalls: v.MaxLiveCalls, StorageDegraded: v.StorageDegraded, HumanSeat: v.HumanSeat, Seats: make([]SeatDTO, 0, len(v.Seats))}
	for _, s := range v.Seats {
		kind := s.Kind
		if kind == "" {
			kind = service.SeatAI
		}
		d.Seats = append(d.Seats, SeatDTO{Seat: s.Seat, Name: s.Name, ModelRef: s.ModelRef, Stack: s.Stack, Kind: kind})
	}
	if v.Hand != nil {
		s := v.Hand.State
		h := &HandDTO{Number: v.Hand.Number, Street: s.Street, Button: s.Button, SmallBlind: s.SmallBlind, BigBlind: s.BigBlind, Actor: s.Actor, Board: append([]poker.Card{}, s.Board...), Pot: s.Pot, CurrentBet: s.CurrentBet, WaitingFor: v.Hand.WaitingFor, Players: make([]PlayerDTO, 0, len(s.Players))}
		for _, p := range s.Players {
			h.Players = append(h.Players, PlayerDTO{Seat: p.Seat, Stack: p.Stack, StreetBet: p.StreetBet, Committed: p.Committed, Status: p.Status})
		}
		d.Hand = h
	}
	if v.LastAction != nil {
		a := v.LastAction
		d.LastAction = &ActionDTO{HandNo: a.HandNo, Sequence: a.Sequence, Seat: a.Seat, Kind: a.Kind, To: a.To, Amount: a.Amount, Source: a.Source, ErrorCode: a.ErrorCode, InputTokens: a.InputTokens, OutputTokens: a.OutputTokens}
	}
	return d
}

// PlayerGame 在公共视角基础上显式添加本人私有对象；只能用于会话校验后的响应。
func PlayerGame(v service.PlayerGameView) GameDTO {
	d := Game(v.Game)
	p := v.Player
	pd := &PlayerDTOView{Seat: p.Seat, HandNo: p.HandNo, HoleCards: append([]poker.Card{}, p.HoleCards...), ActionDeadline: p.ActionDeadline, ServerTime: v.ServerTime}
	if p.LegalActions != nil {
		l := p.LegalActions
		pd.LegalActions = &LegalActionsDTO{CanFold: l.CanFold, CanCheck: l.CanCheck, CanCall: l.CanCall, CallAmount: l.CallAmount, CanRaise: l.CanRaise, MinRaiseTo: l.MinRaiseTo, MaxRaiseTo: l.MaxRaiseTo, ShortAllInTo: l.ShortAllInTo}
	}
	d.Player = pd
	return d
}

func MarshalGame(v service.GameView) ([]byte, error) { return json.Marshal(Game(v)) }
func amounts(m map[poker.Seat]poker.Chips) []AmountDTO {
	d := make([]AmountDTO, 0, len(m))
	for seat, n := range m {
		d = append(d, AmountDTO{Seat: seat, Amount: n})
	}
	sort.Slice(d, func(i, j int) bool { return d[i].Seat < d[j].Seat })
	return d
}

// Seat 的底层类型是 uint8；HTTP 数组使用 int，避免 JSON 编码为 Base64。
func seatNumbers(seats []poker.Seat) []int {
	numbers := make([]int, len(seats))
	for i, seat := range seats {
		numbers[i] = int(seat)
	}
	return numbers
}

func Completed(v service.CompletedHandView) CompletedHandDTO {
	r := v.Result
	d := CompletedHandDTO{GameID: v.GameID, HandNo: v.HandNo, Reason: r.Reason, Button: v.Button, Board: append([]poker.Card{}, v.Board...), HoleCards: make([]HoleDTO, 0, len(r.HoleCards)), Refunds: amounts(r.Refunds), Payouts: amounts(r.Payouts), Pots: make([]PotDTO, 0, len(r.Pots)), FinalStacks: make([]StackDTO, 0, len(r.FinalStack)), Showdown: make([]ShowdownDTO, 0, len(r.HandRanks))}
	for seat, cards := range r.HoleCards {
		d.HoleCards = append(d.HoleCards, HoleDTO{Seat: seat, Cards: cards})
	}
	sort.Slice(d.HoleCards, func(i, j int) bool { return d.HoleCards[i].Seat < d.HoleCards[j].Seat })
	for seat, rank := range r.HandRanks {
		d.Showdown = append(d.Showdown, ShowdownDTO{Seat: seat, HandRank: rank, BestFive: append([]poker.Card{}, r.BestFive[seat]...)})
	}
	sort.Slice(d.Showdown, func(i, j int) bool { return d.Showdown[i].Seat < d.Showdown[j].Seat })
	for _, p := range r.Pots {
		d.Pots = append(d.Pots, PotDTO{Amount: p.Amount, Eligible: seatNumbers(p.Eligible), Winners: seatNumbers(p.Winners), Payouts: amounts(p.Payouts)})
	}
	for seat, stack := range r.FinalStack {
		d.FinalStacks = append(d.FinalStacks, StackDTO{Seat: seat, Stack: stack})
	}
	sort.Slice(d.FinalStacks, func(i, j int) bool { return d.FinalStacks[i].Seat < d.FinalStacks[j].Seat })
	return d
}
func Models(v []agent.ModelDescriptor) ModelsDTO {
	d := ModelsDTO{Models: make([]ModelDTO, 0, len(v))}
	for _, m := range v {
		var code *string
		if m.Availability == "unavailable" {
			s := "MODEL_UNAVAILABLE"
			code = &s
		}
		if m.Availability == "unsupported" {
			s := "UNSUPPORTED_PROVIDER"
			code = &s
		}
		d.Models = append(d.Models, ModelDTO{Ref: m.Ref, Label: m.Label, Provider: m.Provider, Availability: m.Availability, ErrorCode: code})
	}
	return d
}
func Health(v service.HealthView) HealthDTO {
	return HealthDTO{MySQL: v.MySQL, Redis: v.Redis, StorageDegraded: v.StorageDegraded, AllowPaidModels: v.AllowPaidModels, MaxLiveCalls: v.MaxLiveCalls}
}
func (d CreateGameDTO) Input() (service.CreateGame, error) {
	if len(d.Seats) < 2 || len(d.Seats) > 9 || d.Button == nil || d.SmallBlind == nil || d.BigBlind == nil || d.MaxHands == nil || d.MaxLiveCalls == nil {
		return service.CreateGame{}, service.ErrInvalid
	}
	c := service.CreateGame{Button: *d.Button, SmallBlind: *d.SmallBlind, BigBlind: *d.BigBlind, MaxHands: *d.MaxHands, MaxLiveCalls: *d.MaxLiveCalls, Mode: "mock"}
	if d.Mode != nil {
		c.Mode = *d.Mode
		if c.Mode == "" {
			return c, service.ErrInvalid
		}
	}
	for _, s := range d.Seats {
		sc, err := s.input()
		if err != nil {
			return service.CreateGame{}, err
		}
		c.Seats = append(c.Seats, sc)
	}
	return c, nil
}

// input 解析单个座位；kind 省略按 ai，显式 null/空串/未知值拒绝。
// 真人 model_ref 只允许省略或 ""，显式 null 或非空拒绝。
func (s CreateSeatDTO) input() (service.SeatConfig, error) {
	if s.Seat == nil || s.Name == nil || s.Stack == nil {
		return service.SeatConfig{}, service.ErrInvalid
	}
	kind := service.SeatAI
	if raw := bytes.TrimSpace(s.Kind); len(raw) > 0 {
		if bytes.Equal(raw, []byte("null")) {
			return service.SeatConfig{}, service.ErrInvalid
		}
		var k service.SeatKind
		if json.Unmarshal(raw, &k) != nil || (k != service.SeatAI && k != service.SeatHuman) {
			return service.SeatConfig{}, service.ErrInvalid
		}
		kind = k
	}
	sc := service.SeatConfig{Seat: *s.Seat, Name: *s.Name, Stack: *s.Stack, Kind: kind}
	raw := bytes.TrimSpace(s.ModelRef)
	switch kind {
	case service.SeatHuman:
		if len(raw) == 0 {
			break
		}
		if bytes.Equal(raw, []byte("null")) {
			return service.SeatConfig{}, service.ErrInvalid
		}
		var ref string
		if json.Unmarshal(raw, &ref) != nil || ref != "" {
			return service.SeatConfig{}, service.ErrInvalid
		}
	case service.SeatAI:
		if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
			return service.SeatConfig{}, service.ErrInvalid
		}
		var ref string
		if json.Unmarshal(raw, &ref) != nil || ref == "" {
			return service.SeatConfig{}, service.ErrInvalid
		}
		sc.ModelRef = ref
	}
	return sc, nil
}

// HumanInput 校验并转换真人动作请求；expected_revision 必填且 >0。
func (d HumanActionDTO) HumanInput() (service.HumanAction, error) {
	if d.ExpectedRevision == nil || *d.ExpectedRevision == 0 || d.Kind == nil {
		return service.HumanAction{}, service.ErrInvalid
	}
	ha := service.HumanAction{ExpectedRevision: *d.ExpectedRevision, Action: poker.Action{Kind: *d.Kind}}
	switch *d.Kind {
	case "fold", "check", "call":
		if d.To != nil && *d.To != 0 {
			return service.HumanAction{}, service.ErrInvalid
		}
	case "raise_to":
		if d.To == nil || *d.To <= 0 {
			return service.HumanAction{}, service.ErrInvalid
		}
		ha.Action.To = *d.To
	default:
		return service.HumanAction{}, service.ErrInvalid
	}
	return ha, nil
}

// ---- 牌桌模板 DTO ----
// 公共模板 DTO 绝不包含 owner_user_id 或任何会话信息；user 字段不进入响应。

// TableSeatDTO 是模板座位公共视图。
type TableSeatDTO struct {
	Seat     poker.Seat       `json:"seat"`
	Name     string           `json:"name"`
	ModelRef string           `json:"model_ref"`
	Stack    poker.Chips      `json:"stack"`
	Kind     service.SeatKind `json:"kind"`
}
type TableConfigDTO struct {
	Seats        []TableSeatDTO `json:"seats"`
	Button       poker.Seat     `json:"button"`
	SmallBlind   poker.Chips    `json:"small_blind"`
	BigBlind     poker.Chips    `json:"big_blind"`
	MaxHands     uint32         `json:"max_hands"`
	Mode         service.Mode   `json:"mode"`
	MaxLiveCalls uint32         `json:"max_live_calls"`
}
type TableDTO struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Revision  uint64          `json:"revision"`
	Config    *TableConfigDTO `json:"config"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}

// TableSummaryDTO 是列表摘要：不含完整 seats，不暴露 owner/会话。
type TableSummaryDTO struct {
	ID         string      `json:"id"`
	Name       string      `json:"name"`
	Revision   uint64      `json:"revision"`
	Configured bool        `json:"configured"`
	SeatCount  int         `json:"seat_count"`
	SmallBlind poker.Chips `json:"small_blind"`
	BigBlind   poker.Chips `json:"big_blind"`
	HumanSeat  *poker.Seat `json:"human_seat"`
	CreatedAt  time.Time   `json:"created_at"`
	UpdatedAt  time.Time   `json:"updated_at"`
}
type TableListDTO struct {
	Tables     []TableSummaryDTO `json:"tables"`
	NextCursor *string           `json:"next_cursor"`
}

// 请求 DTO：指针区分缺字段与零值，RawMessage 区分省略与显式 null。
type TableSeatInputDTO struct {
	Seat     *poker.Seat     `json:"seat"`
	Name     *string         `json:"name"`
	Stack    *poker.Chips    `json:"stack"`
	ModelRef json.RawMessage `json:"model_ref"`
	Kind     json.RawMessage `json:"kind"`
}
type TableConfigInputDTO struct {
	Seats        []TableSeatInputDTO `json:"seats"`
	Button       *poker.Seat         `json:"button"`
	SmallBlind   *poker.Chips        `json:"small_blind"`
	BigBlind     *poker.Chips        `json:"big_blind"`
	MaxHands     *uint32             `json:"max_hands"`
	Mode         *service.Mode       `json:"mode"`
	MaxLiveCalls *uint32             `json:"max_live_calls"`
}
type CreateTableDTO struct {
	Name   *string         `json:"name"`
	Config json.RawMessage `json:"config"`
}
type UpdateTableDTO struct {
	ExpectedRevision *uint64         `json:"expected_revision"`
	Name             *string         `json:"name"`
	Config           json.RawMessage `json:"config"`
}
type ExpectedRevisionDTO struct {
	ExpectedRevision *uint64 `json:"expected_revision"`
}

// Table 转换公共模板视图：Config=null 时输出 null，表示未配置空态。
func Table(v service.Table) TableDTO {
	d := TableDTO{ID: v.ID, Name: v.Name, Revision: v.Revision, CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt}
	if v.Config != nil {
		c := tableConfig(*v.Config)
		d.Config = &c
	}
	return d
}

func Tables(items []service.Table, next string) TableListDTO {
	d := TableListDTO{Tables: make([]TableSummaryDTO, 0, len(items))}
	for _, t := range items {
		s := TableSummaryDTO{ID: t.ID, Name: t.Name, Revision: t.Revision, Configured: t.Config != nil, CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt}
		if t.Config != nil {
			s.SeatCount = len(t.Config.Seats)
			s.SmallBlind = t.Config.SmallBlind
			s.BigBlind = t.Config.BigBlind
			s.HumanSeat = t.Config.HumanSeat()
		}
		d.Tables = append(d.Tables, s)
	}
	if next != "" {
		d.NextCursor = &next
	}
	return d
}

func tableConfig(c service.TableConfig) TableConfigDTO {
	d := TableConfigDTO{Button: c.Button, SmallBlind: c.SmallBlind, BigBlind: c.BigBlind, MaxHands: c.MaxHands, Mode: c.Mode, MaxLiveCalls: c.MaxLiveCalls, Seats: make([]TableSeatDTO, 0, len(c.Seats))}
	for _, s := range c.Seats {
		kind := s.Kind
		if kind == "" {
			kind = service.SeatAI
		}
		d.Seats = append(d.Seats, TableSeatDTO{Seat: s.Seat, Name: s.Name, ModelRef: s.ModelRef, Stack: s.Stack, Kind: kind})
	}
	return d
}

// tableConfigFromRaw 解析可空的 config 字段：缺省/null 表示空模板；否则严格解码。
func tableConfigFromRaw(raw json.RawMessage) (*service.TableConfig, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, nil
	}
	var in TableConfigInputDTO
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		return nil, service.ErrInvalid
	}
	c, err := in.Input()
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (d TableConfigInputDTO) Input() (service.TableConfig, error) {
	if len(d.Seats) < 2 || len(d.Seats) > 9 || d.Button == nil || d.SmallBlind == nil || d.BigBlind == nil || d.MaxHands == nil || d.MaxLiveCalls == nil {
		return service.TableConfig{}, service.ErrInvalid
	}
	c := service.TableConfig{Button: *d.Button, SmallBlind: *d.SmallBlind, BigBlind: *d.BigBlind, MaxHands: *d.MaxHands, MaxLiveCalls: *d.MaxLiveCalls, Mode: "mock"}
	if d.Mode != nil {
		c.Mode = *d.Mode
		if c.Mode == "" {
			return c, service.ErrInvalid
		}
	}
	for _, s := range d.Seats {
		sc, err := s.input()
		if err != nil {
			return service.TableConfig{}, err
		}
		c.Seats = append(c.Seats, sc)
	}
	return c, nil
}

// input 解析模板单座位；kind 省略按 ai，显式 null/未知拒绝；真人 model_ref 只允许省略或 ""。
func (s TableSeatInputDTO) input() (service.TableSeat, error) {
	if s.Seat == nil || s.Name == nil || s.Stack == nil {
		return service.TableSeat{}, service.ErrInvalid
	}
	kind := service.SeatAI
	if raw := bytes.TrimSpace(s.Kind); len(raw) > 0 {
		if bytes.Equal(raw, []byte("null")) {
			return service.TableSeat{}, service.ErrInvalid
		}
		var k service.SeatKind
		if json.Unmarshal(raw, &k) != nil || (k != service.SeatAI && k != service.SeatHuman) {
			return service.TableSeat{}, service.ErrInvalid
		}
		kind = k
	}
	sc := service.TableSeat{Seat: *s.Seat, Name: *s.Name, Stack: *s.Stack, Kind: kind}
	raw := bytes.TrimSpace(s.ModelRef)
	switch kind {
	case service.SeatHuman:
		if len(raw) == 0 {
			break
		}
		if bytes.Equal(raw, []byte("null")) {
			return service.TableSeat{}, service.ErrInvalid
		}
		var ref string
		if json.Unmarshal(raw, &ref) != nil || ref != "" {
			return service.TableSeat{}, service.ErrInvalid
		}
	case service.SeatAI:
		if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
			return service.TableSeat{}, service.ErrInvalid
		}
		var ref string
		if json.Unmarshal(raw, &ref) != nil || ref == "" {
			return service.TableSeat{}, service.ErrInvalid
		}
		sc.ModelRef = ref
	}
	return sc, nil
}
