package service

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"texas-holdem/internal/poker"
)

// 模板列表分页边界；接口层与存储层共用，避免无界查询。
const (
	defaultTableListLimit = 20
	maxTableListLimit     = 100
	maxTableNameLen       = 128
	// maxTableLiveCalls 是模板保存时对 live 预算的绝对上限（与服务配置上限同量级）。
	// 保存时不依赖服务当前授权/预算开关，只拒绝明显越界值；开局时再由 Engine 复核。
	maxTableLiveCalls = 1000000
)

// TableSeat 是模板中的纯座位配置：只描述座位，不承载任何用户身份或会话摘要。
type TableSeat struct {
	Seat     poker.Seat
	Name     string
	Stack    poker.Chips
	ModelRef string
	Kind     SeatKind
}

// TableConfig 是可复用的纯游戏配置；可覆盖全部开局输入，但不含任何会话/用户字段。
type TableConfig struct {
	Seats        []TableSeat
	Button       poker.Seat
	SmallBlind   poker.Chips
	BigBlind     poker.Chips
	MaxHands     uint32
	Mode         Mode
	MaxLiveCalls uint32
}

// Table 是牌桌配置模板领域对象。Config==nil 表示已清空的未配置空态。
type Table struct {
	ID          string
	OwnerUserID *string
	Name        string
	Revision    uint64
	Config      *TableConfig
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// TableRepository 是模板持久化接口，与游戏生命周期 Repository 分离。
type TableRepository interface {
	CreateTable(context.Context, Table) error
	GetTable(context.Context, string) (Table, error)
	ListTables(context.Context, string, int) ([]Table, string, error)
	UpdateTable(context.Context, uint64, Table) (Table, error)
	ClearTable(context.Context, uint64, string) (Table, error)
	DeleteTable(context.Context, uint64, string) error
}

// TableService 负责模板 CRUD、保存时校验与开局配置读取。
// 开局时复用 Engine.Create 复核授权/预算/模型可构造性，本服务不构造 decider。
type TableService struct {
	repo TableRepository
}

func NewTableService(repo TableRepository) *TableService {
	return &TableService{repo: repo}
}

// HumanSeat 返回模板中唯一的真人座位；无真人返回 nil。
func (c TableConfig) HumanSeat() *poker.Seat {
	for i := range c.Seats {
		if c.Seats[i].Kind == SeatHuman {
			seat := c.Seats[i].Seat
			return &seat
		}
	}
	return nil
}

// GameInput 把模板配置投影为一次性创建输入。模板不保存用户身份，因此不设置任何
// UserID/OwnerUserID/会话；这些由可信 HTTP 适配层在开局时注入。
func (c TableConfig) GameInput() CreateGame {
	g := CreateGame{Button: c.Button, SmallBlind: c.SmallBlind, BigBlind: c.BigBlind, MaxHands: c.MaxHands, Mode: c.Mode, MaxLiveCalls: c.MaxLiveCalls}
	g.Seats = make([]SeatConfig, 0, len(c.Seats))
	for _, s := range c.Seats {
		g.Seats = append(g.Seats, SeatConfig{Seat: s.Seat, Name: s.Name, Stack: s.Stack, ModelRef: s.ModelRef, Kind: s.Kind})
	}
	return g
}

// TableConfigFromGame 把已规范化的一次性配置投影为纯模板配置（丢弃用户/会话字段）。
func TableConfigFromGame(g CreateGame) TableConfig {
	c := TableConfig{Button: g.Button, SmallBlind: g.SmallBlind, BigBlind: g.BigBlind, MaxHands: g.MaxHands, Mode: g.Mode, MaxLiveCalls: g.MaxLiveCalls}
	c.Seats = make([]TableSeat, 0, len(g.Seats))
	for _, s := range g.Seats {
		c.Seats = append(c.Seats, TableSeat{Seat: s.Seat, Name: s.Name, Stack: s.Stack, ModelRef: s.ModelRef, Kind: s.Kind})
	}
	return c
}

func normalizeTableName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > maxTableNameLen || !utf8.ValidString(name) {
		return "", ErrInvalid
	}
	return name, nil
}

// validateTableConfig 是保存时的纯校验：只检查结构、数值范围与座位关系，
// 不要求真人会话、不检查模型目录、不构造 decider；开局时再由 Engine 复核。
func validateTableConfig(c TableConfig) (TableConfig, error) {
	if c.Mode == "" {
		c.Mode = "mock"
	}
	if c.Mode != "mock" && c.Mode != "live" {
		return TableConfig{}, ErrInvalid
	}
	if c.Mode == "mock" && c.MaxLiveCalls != 0 {
		return TableConfig{}, ErrInvalid
	}
	if c.Mode == "live" && (c.MaxLiveCalls == 0 || c.MaxLiveCalls > maxTableLiveCalls) {
		return TableConfig{}, ErrInvalid
	}
	if len(c.Seats) < 2 || len(c.Seats) > 9 || c.MaxHands == 0 || c.SmallBlind <= 0 || c.BigBlind <= c.SmallBlind || c.BigBlind > 1000000000000 {
		return TableConfig{}, ErrInvalid
	}
	seen := map[poker.Seat]bool{}
	total := poker.Chips(0)
	eligible := 0
	button := false
	humans := 0
	for i := range c.Seats {
		s := &c.Seats[i]
		if s.Seat > 8 || seen[s.Seat] || !utf8.ValidString(s.Name) || strings.TrimSpace(s.Name) == "" || len(s.Name) > 128 || len(s.ModelRef) > 256 || s.Stack < 0 || s.Stack > 1000000000000-total {
			return TableConfig{}, ErrInvalid
		}
		if s.Kind == "" {
			s.Kind = SeatAI
		}
		switch s.Kind {
		case SeatHuman:
			humans++
			if humans > 1 || s.ModelRef != "" || s.Stack == 0 {
				return TableConfig{}, ErrInvalid
			}
		case SeatAI:
			if s.ModelRef == "" {
				return TableConfig{}, ErrInvalid
			}
		default:
			return TableConfig{}, ErrInvalid
		}
		seen[s.Seat] = true
		total += s.Stack
		if s.Stack > 0 {
			eligible++
			if s.Seat == c.Button {
				button = true
			}
		}
	}
	if eligible < 2 || !button {
		return TableConfig{}, ErrInvalid
	}
	sort.Slice(c.Seats, func(i, j int) bool { return c.Seats[i].Seat < c.Seats[j].Seat })
	return c, nil
}

// Create 新建模板；name 非空、config 允许为 nil（空模板）。
func (s *TableService) Create(ctx context.Context, name string, config *TableConfig, owner *string) (Table, error) {
	if err := ctx.Err(); err != nil {
		return Table{}, ErrStorage
	}
	name, err := normalizeTableName(name)
	if err != nil {
		return Table{}, err
	}
	if config != nil {
		c, verr := validateTableConfig(*config)
		if verr != nil {
			return Table{}, verr
		}
		config = &c
	}
	id, err := uuid()
	if err != nil {
		return Table{}, ErrStorage
	}
	now := time.Now().UTC()
	t := Table{ID: id, OwnerUserID: cloneString(owner), Name: name, Revision: 1, Config: config, CreatedAt: now, UpdatedAt: now}
	if err := s.repo.CreateTable(ctx, t); err != nil {
		return Table{}, safeTableError(err)
	}
	return t, nil
}

func (s *TableService) Get(ctx context.Context, id string) (Table, error) {
	if !validTableID(id) {
		return Table{}, ErrInvalid
	}
	t, err := s.repo.GetTable(ctx, id)
	if err != nil {
		return Table{}, safeTableError(err)
	}
	return t, nil
}

// List 返回有界分页；limit 为 0 用默认值，超过上限截断到上限。
func (s *TableService) List(ctx context.Context, cursor string, limit int) ([]Table, string, error) {
	if limit <= 0 {
		limit = defaultTableListLimit
	}
	if limit > maxTableListLimit {
		limit = maxTableListLimit
	}
	items, next, err := s.repo.ListTables(ctx, cursor, limit)
	if err != nil {
		return nil, "", safeTableError(err)
	}
	return items, next, nil
}

// Update 全量替换 name 与 config（config 必填）；expectedRevision 是 CAS 前置版本。
func (s *TableService) Update(ctx context.Context, id string, expectedRevision uint64, name string, config *TableConfig) (Table, error) {
	if !validTableID(id) || expectedRevision == 0 || config == nil {
		return Table{}, ErrInvalid
	}
	name, err := normalizeTableName(name)
	if err != nil {
		return Table{}, err
	}
	c, err := validateTableConfig(*config)
	if err != nil {
		return Table{}, err
	}
	t, err := s.repo.UpdateTable(ctx, expectedRevision, Table{ID: id, Name: name, Config: &c, Revision: expectedRevision + 1})
	if err != nil {
		return Table{}, safeTableError(err)
	}
	return t, nil
}

// Clear 清空配置：config 置 nil、revision 递增、updated_at 更新；返回空态模板。
func (s *TableService) Clear(ctx context.Context, id string, expectedRevision uint64) (Table, error) {
	if !validTableID(id) || expectedRevision == 0 {
		return Table{}, ErrInvalid
	}
	t, err := s.repo.ClearTable(ctx, expectedRevision, id)
	if err != nil {
		return Table{}, safeTableError(err)
	}
	return t, nil
}

// Delete 按 CAS 删除模板；不影响任何历史牌局。
func (s *TableService) Delete(ctx context.Context, id string, expectedRevision uint64) error {
	if !validTableID(id) || expectedRevision == 0 {
		return ErrInvalid
	}
	if err := s.repo.DeleteTable(ctx, expectedRevision, id); err != nil {
		return safeTableError(err)
	}
	return nil
}

func cloneString(v *string) *string {
	if v == nil {
		return nil
	}
	s := *v
	return &s
}

// validTableID 与 UUID 形状一致（36 字符小写 hex + 连字符）。
func validTableID(id string) bool {
	if len(id) != 36 {
		return false
	}
	for i, c := range id {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return false
			}
			continue
		}
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// safeTableError 把存储错误归一化为对上层稳定的错误码，保留冲突/未找到语义。
func safeTableError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ErrNotFound), errors.Is(err, ErrConflict), errors.Is(err, ErrInvalid), errors.Is(err, ErrStorage):
		return err
	default:
		return ErrStorage
	}
}
