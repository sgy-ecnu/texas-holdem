package service

import (
	"context"
	"errors"
	"sort"
	"sync"
	"testing"
	"time"

	"texas-holdem/internal/poker"
)

// fakeTableRepo 是服务层单测用的内存模板仓库。
type fakeTableRepo struct {
	mu     sync.Mutex
	tables map[string]Table
	fail   bool
}

func newFakeTableRepo() *fakeTableRepo { return &fakeTableRepo{tables: map[string]Table{}} }

func (r *fakeTableRepo) CreateTable(_ context.Context, t Table) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.fail {
		return ErrStorage
	}
	if _, ok := r.tables[t.ID]; ok {
		return ErrConflict
	}
	r.tables[t.ID] = clone(t)
	return nil
}

func (r *fakeTableRepo) GetTable(_ context.Context, id string) (Table, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.fail {
		return Table{}, ErrStorage
	}
	t, ok := r.tables[id]
	if !ok {
		return Table{}, ErrNotFound
	}
	return clone(t), nil
}

func (r *fakeTableRepo) ListTables(_ context.Context, cursor string, limit int) ([]Table, string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.fail {
		return nil, "", ErrStorage
	}
	all := make([]Table, 0, len(r.tables))
	for _, t := range r.tables {
		all = append(all, clone(t))
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].UpdatedAt.Equal(all[j].UpdatedAt) {
			return all[i].ID > all[j].ID
		}
		return all[i].UpdatedAt.After(all[j].UpdatedAt)
	})
	start := 0
	if cursor != "" {
		for i, t := range all {
			if t.ID == cursor {
				start = i + 1
				break
			}
		}
	}
	all = all[start:]
	next := ""
	if len(all) > limit {
		all = all[:limit]
		next = all[limit-1].ID
	}
	return all, next, nil
}

func (r *fakeTableRepo) UpdateTable(_ context.Context, expected uint64, t Table) (Table, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.fail {
		return Table{}, ErrStorage
	}
	cur, ok := r.tables[t.ID]
	if !ok {
		return Table{}, ErrNotFound
	}
	if cur.Revision != expected {
		return Table{}, ErrConflict
	}
	cur.Name = t.Name
	cur.Config = clone(t.Config)
	cur.Revision = expected + 1
	cur.UpdatedAt = time.Now().UTC()
	r.tables[t.ID] = cur
	return clone(cur), nil
}

func (r *fakeTableRepo) ClearTable(_ context.Context, expected uint64, id string) (Table, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.fail {
		return Table{}, ErrStorage
	}
	cur, ok := r.tables[id]
	if !ok {
		return Table{}, ErrNotFound
	}
	if cur.Revision != expected {
		return Table{}, ErrConflict
	}
	cur.Config = nil
	cur.Revision = expected + 1
	cur.UpdatedAt = time.Now().UTC()
	r.tables[id] = cur
	return clone(cur), nil
}

func (r *fakeTableRepo) DeleteTable(_ context.Context, expected uint64, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.fail {
		return ErrStorage
	}
	cur, ok := r.tables[id]
	if !ok {
		return ErrNotFound
	}
	if cur.Revision != expected {
		return ErrConflict
	}
	delete(r.tables, id)
	return nil
}

func tableConfigFixture() TableConfig {
	return TableConfig{
		Seats:        []TableSeat{{Seat: 0, Name: "A", Stack: 1000, ModelRef: "mock", Kind: SeatAI}, {Seat: 1, Name: "B", Stack: 1000, ModelRef: "mock", Kind: SeatAI}},
		Button:       0,
		SmallBlind:   5,
		BigBlind:     10,
		MaxHands:     3,
		Mode:         "mock",
		MaxLiveCalls: 0,
	}
}

func TestTableServiceCreateGetAndEmpty(t *testing.T) {
	s := NewTableService(newFakeTableRepo())
	empty, err := s.Create(context.Background(), "  空桌  ", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if empty.Name != "空桌" || empty.Revision != 1 || empty.Config != nil || empty.OwnerUserID != nil {
		t.Fatalf("empty template mismatch: %+v", empty)
	}
	cfg := tableConfigFixture()
	full, err := s.Create(context.Background(), "满桌", &cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if full.Config == nil || len(full.Config.Seats) != 2 {
		t.Fatalf("full template config mismatch: %+v", full)
	}
	got, err := s.Get(context.Background(), full.ID)
	if err != nil || got.Name != "满桌" {
		t.Fatalf("get mismatch: %+v %v", got, err)
	}
	if _, err = s.Get(context.Background(), "22222222-2222-4222-8222-222222222222"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing template should be not found: %v", err)
	}
}

func TestTableServiceNameValidation(t *testing.T) {
	s := NewTableService(newFakeTableRepo())
	if _, err := s.Create(context.Background(), "   ", nil, nil); !errors.Is(err, ErrInvalid) {
		t.Fatal("blank name accepted")
	}
	long := ""
	for i := 0; i < 129; i++ {
		long += "a"
	}
	if _, err := s.Create(context.Background(), long, nil, nil); !errors.Is(err, ErrInvalid) {
		t.Fatal("overlong name accepted")
	}
}

func TestTableServiceRevisionCAS(t *testing.T) {
	s := NewTableService(newFakeTableRepo())
	cfg := tableConfigFixture()
	tb, err := s.Create(context.Background(), "t", &cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	// 冲突：错误版本
	if _, err = s.Update(context.Background(), tb.ID, 99, "t2", &cfg); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale update should conflict: %v", err)
	}
	// 正确版本
	upd, err := s.Update(context.Background(), tb.ID, 1, "t2", &cfg)
	if err != nil || upd.Revision != 2 || upd.Name != "t2" {
		t.Fatalf("update mismatch: %+v %v", upd, err)
	}
	// 再次使用旧版本 -> 冲突
	if _, err = s.Update(context.Background(), tb.ID, 1, "t3", &cfg); !errors.Is(err, ErrConflict) {
		t.Fatalf("replayed update should conflict: %v", err)
	}
	// PUT 必填 config
	if _, err = s.Update(context.Background(), tb.ID, 2, "t3", nil); !errors.Is(err, ErrInvalid) {
		t.Fatalf("nil config update should be invalid: %v", err)
	}
}

func TestTableServiceClearSemantics(t *testing.T) {
	s := NewTableService(newFakeTableRepo())
	cfg := tableConfigFixture()
	tb, err := s.Create(context.Background(), "t", &cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	cleared, err := s.Clear(context.Background(), tb.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if cleared.Config != nil || cleared.Revision != 2 {
		t.Fatalf("clear mismatch: %+v", cleared)
	}
	if _, err = s.Clear(context.Background(), tb.ID, 1); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale clear should conflict: %v", err)
	}
}

func TestTableServiceDelete(t *testing.T) {
	s := NewTableService(newFakeTableRepo())
	cfg := tableConfigFixture()
	tb, err := s.Create(context.Background(), "t", &cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Delete(context.Background(), tb.ID, 5); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale delete should conflict: %v", err)
	}
	if err = s.Delete(context.Background(), tb.ID, 1); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Get(context.Background(), tb.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted template should be gone: %v", err)
	}
}

func TestTableServiceStorageFailure(t *testing.T) {
	repo := newFakeTableRepo()
	repo.fail = true
	s := NewTableService(repo)
	if _, err := s.Create(context.Background(), "t", nil, nil); !errors.Is(err, ErrStorage) {
		t.Fatalf("storage failure should surface: %v", err)
	}
}

func TestValidateTableConfig(t *testing.T) {
	base := tableConfigFixture()
	cases := []struct {
		name   string
		mutate func(*TableConfig)
	}{
		{"ai_seat_missing_model", func(c *TableConfig) { c.Seats[0].ModelRef = "" }},
		{"two_humans", func(c *TableConfig) {
			c.Seats[0].Kind = SeatHuman
			c.Seats[0].ModelRef = ""
			c.Seats[1].Kind = SeatHuman
			c.Seats[1].ModelRef = ""
		}},
		{"human_with_model", func(c *TableConfig) { c.Seats[1].Kind = SeatHuman; c.Seats[1].ModelRef = "mock" }},
		{"human_zero_stack", func(c *TableConfig) { c.Seats[1].Kind = SeatHuman; c.Seats[1].ModelRef = ""; c.Seats[1].Stack = 0 }},
		{"mock_with_budget", func(c *TableConfig) { c.Mode = "mock"; c.MaxLiveCalls = 1 }},
		{"live_without_budget", func(c *TableConfig) { c.Mode = "live"; c.MaxLiveCalls = 0 }},
		{"live_over_ceiling", func(c *TableConfig) { c.Mode = "live"; c.MaxLiveCalls = maxTableLiveCalls + 1 }},
		{"bad_mode", func(c *TableConfig) { c.Mode = "sandbox" }},
		{"blinds_inverted", func(c *TableConfig) { c.BigBlind = c.SmallBlind }},
		{"button_on_empty_seat", func(c *TableConfig) { c.Button = 5 }},
		{"too_few_seats", func(c *TableConfig) { c.Seats = c.Seats[:1] }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := base
			c.Seats = append([]TableSeat(nil), base.Seats...)
			mutateSeatPtrs(&c)
			tc.mutate(&c)
			if _, err := validateTableConfig(c); !errors.Is(err, ErrInvalid) {
				t.Fatalf("config accepted: %+v", c)
			}
		})
	}
	// live + mock 引用且预算合法，保存时必须接受（不检查模型目录/授权）。
	live := tableConfigFixture()
	live.Mode = "live"
	live.MaxLiveCalls = 50
	if _, err := validateTableConfig(live); err != nil {
		t.Fatalf("valid live template rejected: %v", err)
	}
}

// mutateSeatPtrs 让用例的浅拷贝不会共享底层 SeatConfig 指针。
func mutateSeatPtrs(c *TableConfig) {
	seats := make([]TableSeat, len(c.Seats))
	copy(seats, c.Seats)
	c.Seats = seats
}

func TestTableConfigGameInputReservesUser(t *testing.T) {
	cfg := TableConfig{
		Seats:        []TableSeat{{Seat: 0, Name: "H", Stack: 1000, Kind: SeatHuman}, {Seat: 1, Name: "A", Stack: 1000, ModelRef: "mock", Kind: SeatAI}},
		Button:       0,
		SmallBlind:   5,
		BigBlind:     10,
		MaxHands:     2,
		Mode:         "mock",
		MaxLiveCalls: 0,
	}
	g := cfg.GameInput()
	if g.TemplateID != nil || g.TemplateRevision != nil || g.OwnerUserID != nil || g.PlayerSessionHash != nil {
		t.Fatal("GameInput must not invent source/session fields")
	}
	for _, s := range g.Seats {
		if s.UserID != nil {
			t.Fatalf("template seat must not carry UserID: %+v", s)
		}
	}
	if hs := cfg.HumanSeat(); hs == nil || *hs != 0 {
		t.Fatalf("HumanSeat mismatch: %v", hs)
	}
}

// 通过 Engine.Create 复核：AI 座位带 UserID 必须被拒绝。
func TestCreateRejectsAIUserField(t *testing.T) {
	r := newTestRepo()
	e := engine(t, r, nil, Options{})
	user := "user-1"
	c := config()
	c.Seats[0].UserID = &user
	if _, err := e.Create(context.Background(), c); !errors.Is(err, ErrInvalid) {
		t.Fatalf("AI seat UserID accepted: %v", err)
	}
}

func TestTableServiceListBounds(t *testing.T) {
	s := NewTableService(newFakeTableRepo())
	cfg := tableConfigFixture()
	for i := 0; i < 3; i++ {
		if _, err := s.Create(context.Background(), "t", &cfg, nil); err != nil {
			t.Fatal(err)
		}
	}
	items, next, err := s.List(context.Background(), "", 0)
	if err != nil || len(items) != 3 || next != "" {
		t.Fatalf("list default mismatch: %d %q %v", len(items), next, err)
	}
	items, _, err = s.List(context.Background(), "", 2)
	if err != nil || len(items) != 2 {
		t.Fatalf("bounded list mismatch: %d %v", len(items), err)
	}
}

func TestTableConfigFromGameDropsUser(t *testing.T) {
	user := "user-1"
	cfg := TableConfigFromGame(CreateGame{
		Seats:        []SeatConfig{{Seat: 0, Name: "H", Stack: 1000, Kind: SeatHuman, UserID: &user}},
		Button:       0,
		SmallBlind:   5,
		BigBlind:     10,
		MaxHands:     2,
		Mode:         "mock",
		MaxLiveCalls: 0,
		OwnerUserID:  &user,
	})
	if cfg.Seats[0].Kind != SeatHuman {
		t.Fatal("kind lost")
	}
	if cfg.Seats[0].Seat != poker.Seat(0) {
		t.Fatal("seat lost")
	}
}
