package service

import (
	"context"
	"errors"
	"texas-holdem/internal/agent"
	"texas-holdem/internal/poker"
	"time"
)

type Mode string
type Command string

// SeatKind 区分 AI 与真人座位；持久化旧记录中的空值按 ai 解释。
type SeatKind string

const (
	SeatAI    SeatKind = "ai"
	SeatHuman SeatKind = "human"
)

type SeatConfig struct {
	Seat     poker.Seat
	Name     string
	Stack    poker.Chips
	ModelRef string
	Kind     SeatKind
}
type CreateGame struct {
	Seats                []SeatConfig
	Button               poker.Seat
	SmallBlind, BigBlind poker.Chips
	MaxHands             uint32
	Mode                 Mode
	MaxLiveCalls         uint32
	// PlayerSessionHash 只由可信 HTTP 适配层注入，绝不来自 JSON 请求字段；
	// 全 AI 创建必须为 nil，真人创建必须提供非零摘要。
	PlayerSessionHash *PlayerSessionHash
}
type SeatView struct {
	Seat           poker.Seat
	Name, ModelRef string
	Stack          poker.Chips
	Kind           SeatKind
}
type HandView struct {
	Number uint32
	State  poker.PublicState
	// WaitingFor 在运行中等待真人行动时为 "human_action"，否则为 nil。
	WaitingFor *string
}
type ActionView struct {
	HandNo   uint32
	Sequence uint64
	Seat     poker.Seat
	Kind     poker.ActionKind
	To       poker.Chips
	// Amount 是本次动作的实际投入额：fold/check 为 0，call 为本次补差跟注额，
	// raise_to 为相对动作前本街投入的追加额。To 仍为本街累计投入目标值。
	Amount                    poker.Chips
	Source                    string
	ErrorCode                 *string
	InputTokens, OutputTokens *int64
}
type GameView struct {
	ID                                                    string
	Revision                                              uint64
	Status                                                string
	Reason                                                *string
	Mode                                                  Mode
	MaxHands, HandsCompleted, LiveCallsUsed, MaxLiveCalls uint32
	StorageDegraded                                       bool
	Seats                                                 []SeatView
	// HumanSeat 是游戏内唯一真人座位号；全 AI 局为 nil。它是公共信息。
	HumanSeat  *poker.Seat
	Hand       *HandView
	LastAction *ActionView
}

// PlayerSessionHash 是浏览器真人凭据的 SHA-256 摘要；原始凭据只存在于浏览器
// Cookie，服务端不持有原值，绝不进入 JSON、日志或公共缓存。
type PlayerSessionHash [32]byte

// HumanAction 是真人动作提交；ExpectedRevision 必须与锁内权威 revision 一致。
type HumanAction struct {
	ExpectedRevision uint64
	Action           poker.Action
}

// PlayerView 只包含请求者本人可见的私牌与行动信息。
type PlayerView struct {
	Seat           poker.Seat
	HandNo         *uint32
	HoleCards      []poker.Card
	LegalActions   *poker.LegalActions
	ActionDeadline *time.Time
}

// PlayerGameView 是公共视角 + 本人私有部分的组合；公共 GameView 永远不含私牌。
type PlayerGameView struct {
	Game       GameView
	Player     PlayerView
	ServerTime time.Time
}

// HumanWait 是私有等待记录；运行中 Deadline!=nil，暂停冻结时 Deadline==nil 且剩余>0。
type HumanWait struct {
	HandNo    uint32
	Seat      poker.Seat
	Deadline  *time.Time
	Remaining time.Duration
}

type CompletedHandView struct {
	GameID string
	HandNo uint32
	Button poker.Seat
	Board  []poker.Card
	Result poker.HandResult
}
type HealthView struct {
	MySQL, Redis                     string
	StorageDegraded, AllowPaidModels bool
	MaxLiveCalls                     uint32
}
type Manager interface {
	Models(context.Context) ([]agent.ModelDescriptor, error)
	Create(context.Context, CreateGame) (GameView, error)
	Current(context.Context) (GameView, error)
	Get(context.Context, string) (GameView, error)
	Control(context.Context, string, Command) (GameView, error)
	GetHand(context.Context, string, uint32) (CompletedHandView, error)
	Health(context.Context) (HealthView, error)
	// GetPlayer/Act 是真人视角接口，必须校验会话摘要，绝不返回其他座位私牌。
	GetPlayer(context.Context, string, PlayerSessionHash) (PlayerGameView, error)
	Act(context.Context, string, PlayerSessionHash, HumanAction) (PlayerGameView, error)
}

// Record 是持久化私有记录，禁止直接序列化到 HTTP 或 Redis。
type Record struct {
	Config    CreateGame
	View      GameView
	Snapshot  []byte
	RequestID string
	Sequence  uint64
	// 真人私有字段：会话摘要、本局有效超时、当前等待记录，均不得进入公共 DTO/Redis。
	HumanSessionHash   *PlayerSessionHash
	HumanActionTimeout time.Duration
	HumanWait          *HumanWait
}
type Commit struct {
	Record           Record
	ExpectedRevision uint64
	Action           *ActionView
	Hand             *CompletedHandView
}

// Repository.Commit 的实现必须原子执行 CAS、快照、动作和结算；Create 也须保证仅一非终态局。
type Repository interface {
	Create(context.Context, Record) error
	Get(context.Context, string) (Record, error)
	Current(context.Context) (Record, error)
	Commit(context.Context, Commit) error
	Hand(context.Context, string, uint32) (CompletedHandView, error)
	Health(context.Context) (HealthView, error)
}
type Options struct {
	AllowPaidModels bool
	MaxLiveCalls    uint32
	DecisionTimeout time.Duration
	// HumanActionTimeout 是真人在被轮到行动后等待动作的最长时间；零值按 60s。
	HumanActionTimeout time.Duration
	// AITurnDelay 是 AI 每次提交动作后、推进下一个 actor 前的思考停顿；
	// 零值或负值不等待（测试/快速部署），仅作用于 AI 座位，用于统一轮转节奏。
	AITurnDelay time.Duration
}

var (
	ErrInvalid   = errors.New("INVALID_INPUT")
	ErrForbidden = errors.New("LIVE_NOT_AUTHORIZED")
	ErrNotFound  = errors.New("NOT_FOUND")
	ErrConflict  = errors.New("INVALID_STATE")
	ErrStorage   = errors.New("STORAGE_UNAVAILABLE")
	// 真人会话与动作错误码，HTTP 层统一映射为固定状态与安全文案。
	ErrPlayerUnauthorized = errors.New("PLAYER_NOT_AUTHORIZED")
	ErrHumanLocalOnly     = errors.New("HUMAN_LOCAL_ONLY")
	ErrIllegalAction      = errors.New("ILLEGAL_ACTION")
	ErrStaleAction        = errors.New("STALE_ACTION")
	ErrActionExpired      = errors.New("ACTION_EXPIRED")
	// ErrInstanceLock 标记存储不可用源于调度实例锁失效。data 层会与 ErrStorage
	// 一同包装，因此 errors.Is(err, ErrStorage) 仍成立，fail-closed 语义不变。
	ErrInstanceLock = errors.New("INSTANCE_LOCK_UNAVAILABLE")
)

func terminal(status string) bool {
	return status == "stopped" || status == "completed" || status == "failed"
}
func text(s string) *string { return &s }
