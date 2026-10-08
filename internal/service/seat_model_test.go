package service

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"texas-holdem/internal/agent"
	"texas-holdem/internal/poker"
	"time"
)

// 「按座位选模型」验收：
//   统一判定规则：真实调用 = mode==live 且当前座位 model_ref != 保留值 "mock"。
//   - live 混合座位（部分 mock 部分真实）validate 通过；
//   - mock 座位不增加 LiveCallsUsed，真实座位增加；
//   - 额度耗尽时 mock 座位继续决策、真实座位暂停 cost_limit；
//   - ActionView.Source 按实际执行器判定：mock 执行器标 "mock"，真实失败回退标 "fallback"；
//   - mock 模式下的 AI model_ref 规范化为 "mock"。

// countDecider 记录被调用次数并返回稳定合法的动作（能过牌就过牌，否则跟注）。
type countDecider struct {
	calls atomic.Int32
	err   error
}

func (d *countDecider) Decide(_ context.Context, obs poker.Observation) (agent.Decision, error) {
	d.calls.Add(1)
	if d.err != nil {
		return agent.Decision{}, d.err
	}
	if obs.Legal.CanCheck {
		return agent.Decision{Action: poker.Action{Kind: "check"}}, nil
	}
	return agent.Decision{Action: poker.Action{Kind: "call"}}, nil
}

// catalogFactory 暴露可控模型目录；NewDecider 对 mock 引用仍走 agent 层 MockDecider。
type catalogFactory struct {
	models []agent.ModelDescriptor
	d      agent.Decider
	made   atomic.Int32
}

func (f *catalogFactory) Models() []agent.ModelDescriptor { return f.models }
func (f *catalogFactory) NewDecider(ref string) (agent.Decider, error) {
	f.made.Add(1)
	if ref == "mock" {
		return agent.NewMockDecider(), nil
	}
	for _, m := range f.models {
		if m.Ref == ref {
			if m.Provider == "unsupported" {
				return nil, errors.New("MODEL_UNSUPPORTED")
			}
			return f.d, nil
		}
	}
	return nil, errors.New("MODEL_NOT_FOUND")
}

func seatModelFactory(d agent.Decider) *catalogFactory {
	return &catalogFactory{
		models: []agent.ModelDescriptor{
			{Ref: "test/model", Label: "Test", Provider: "openai_compatible", Availability: "configured"},
			{Ref: "vendor/mock", Label: "VendorMock", Provider: "mock", Availability: "mock"},
			{Ref: "bad/unsupported", Label: "Bad", Provider: "unsupported", Availability: "unsupported"},
		},
		d: d,
	}
}

func mixedLiveConfig() CreateGame {
	return CreateGame{
		Seats: []SeatConfig{
			{Seat: 0, Name: "Real", Stack: 1000, ModelRef: "test/model", Kind: SeatAI},
			{Seat: 1, Name: "Mock", Stack: 1000, ModelRef: "mock", Kind: SeatAI},
		},
		Button: 0, SmallBlind: 5, BigBlind: 10, MaxHands: 2, Mode: "live", MaxLiveCalls: 100,
	}
}

// seedMixed 直接写入一份可控的 live 记录；advance 时推进一个 actor（翻前座位 0 先行动）。
func seedMixed(t *testing.T, r *testRepo, c CreateGame, used, max uint32, advance bool) {
	t.Helper()
	players := make([]poker.PlayerInit, 0, len(c.Seats))
	seats := make([]SeatView, 0, len(c.Seats))
	for _, s := range c.Seats {
		if s.Stack > 0 {
			players = append(players, poker.PlayerInit{Seat: s.Seat, Stack: s.Stack})
		}
		seats = append(seats, SeatView{Seat: s.Seat, Name: s.Name, ModelRef: s.ModelRef, Stack: s.Stack, Kind: s.Kind})
	}
	s, err := poker.NewHand(poker.HandConfig{Players: players, Button: c.Button, SmallBlind: c.SmallBlind, BigBlind: c.BigBlind}, poker.StandardDeck())
	if err != nil {
		t.Fatal(err)
	}
	if advance {
		p := poker.Public(s)
		kind := poker.ActionKind("call")
		if lerr := func() error {
			l, e := poker.Legal(s, *p.Actor)
			if e == nil && l.CanCheck {
				kind = "check"
			}
			return e
		}(); lerr != nil {
			t.Fatal(lerr)
		}
		if s, err = poker.Apply(s, *p.Actor, poker.Action{Kind: kind}); err != nil {
			t.Fatal(err)
		}
	}
	b, err := poker.EncodeState(s)
	if err != nil {
		t.Fatal(err)
	}
	v := GameView{ID: "mixed", Revision: 1, Status: "paused", Mode: c.Mode, MaxHands: c.MaxHands,
		LiveCallsUsed: used, MaxLiveCalls: max, Seats: seats, Hand: &HandView{Number: 1, State: poker.Public(s)}}
	if err := r.Create(context.Background(), Record{Config: c, View: v, Snapshot: b}); err != nil {
		t.Fatal(err)
	}
}

// ---------- validate：live 混合座位通过、mock 规范化、非法引用拒绝 ----------

func TestSeatModelValidate(t *testing.T) {
	t.Run("live_mixed_seats_ok", func(t *testing.T) {
		r := newTestRepo()
		e := engine(t, r, seatModelFactory(agent.NewMockDecider()), Options{AllowPaidModels: true, MaxLiveCalls: 1000})
		v, err := e.Create(context.Background(), mixedLiveConfig())
		if err != nil {
			t.Fatalf("live mixed seats rejected: %v", err)
		}
		if v.Seats[0].ModelRef != "test/model" || v.Seats[1].ModelRef != "mock" {
			t.Fatalf("seat model refs not preserved: %+v", v.Seats)
		}
	})

	t.Run("live_rejects_bad_refs", func(t *testing.T) {
		for name, ref := range map[string]string{
			"unknown":       "nope/none",
			"unsupported":   "bad/unsupported",
			"mock_provider": "vendor/mock",
		} {
			t.Run(name, func(t *testing.T) {
				r := newTestRepo()
				e := engine(t, r, seatModelFactory(agent.NewMockDecider()), Options{AllowPaidModels: true, MaxLiveCalls: 1000})
				c := mixedLiveConfig()
				c.Seats[0].ModelRef = ref
				if _, err := e.Create(context.Background(), c); !errors.Is(err, ErrInvalid) {
					t.Fatalf("live ref %q accepted, got %v", ref, err)
				}
			})
		}
	})

	t.Run("live_real_ref_without_factory_rejected", func(t *testing.T) {
		r := newTestRepo()
		e := engine(t, r, nil, Options{AllowPaidModels: true, MaxLiveCalls: 1000})
		if _, err := e.Create(context.Background(), mixedLiveConfig()); !errors.Is(err, ErrInvalid) {
			t.Fatalf("real ref without factory accepted: %v", err)
		}
	})

	t.Run("live_all_mock_without_factory_ok", func(t *testing.T) {
		r := newTestRepo()
		e := engine(t, r, nil, Options{AllowPaidModels: true, MaxLiveCalls: 1000})
		c := mixedLiveConfig()
		c.Seats[0].ModelRef = "mock"
		v, err := e.Create(context.Background(), c)
		if err != nil {
			t.Fatalf("live all-mock without factory rejected: %v", err)
		}
		if v.Seats[0].ModelRef != "mock" {
			t.Fatalf("mock ref lost: %+v", v.Seats)
		}
	})

	t.Run("mock_mode_normalizes_refs", func(t *testing.T) {
		r := newTestRepo()
		e := engine(t, r, nil, Options{})
		c := config() // mock 模式，座位引用为真实模型
		v, err := e.Create(context.Background(), c)
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range v.Seats {
			if s.ModelRef != "mock" {
				t.Fatalf("mock mode ref not normalized: %+v", v.Seats)
			}
		}
		rec := recordOf(t, r, v.ID)
		for _, s := range rec.Config.Seats {
			if s.ModelRef != "mock" {
				t.Fatalf("persisted mock config not normalized: %+v", rec.Config.Seats)
			}
		}
	})
}

// ---------- 额度：live 全 mock 不消耗；live 混合仅真实座位消耗且 source 正确 ----------

func TestSeatModelBudgetIsolation(t *testing.T) {
	t.Run("live_all_mock_no_budget", func(t *testing.T) {
		r := newTestRepo()
		e := engine(t, r, nil, Options{AllowPaidModels: true, MaxLiveCalls: 1000})
		c := mixedLiveConfig()
		c.Seats[0].ModelRef = "mock"
		c.Seats[1].ModelRef = "mock"
		v, err := e.Create(context.Background(), c)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = e.Control(context.Background(), v.ID, "start"); err != nil {
			t.Fatal(err)
		}
		eventually(t, func() bool { v, err = e.Get(context.Background(), v.ID); return err == nil && v.Status == "completed" })
		if v.LiveCallsUsed != 0 {
			t.Fatalf("live all-mock consumed budget: %d", v.LiveCallsUsed)
		}
		if v.HandsCompleted != 2 {
			t.Fatalf("all-mock live did not run to completion: %d", v.HandsCompleted)
		}
	})

	t.Run("live_mixed_only_real_consumes", func(t *testing.T) {
		r := newTestRepo()
		d := &countDecider{}
		e := engine(t, r, seatModelFactory(d), Options{AllowPaidModels: true, MaxLiveCalls: 100})
		v, err := e.Create(context.Background(), mixedLiveConfig())
		if err != nil {
			t.Fatal(err)
		}
		if _, err = e.Control(context.Background(), v.ID, "start"); err != nil {
			t.Fatal(err)
		}
		eventually(t, func() bool { v, err = e.Get(context.Background(), v.ID); return err == nil && v.Status == "completed" })
		realCalls := d.calls.Load()
		if realCalls == 0 {
			t.Fatal("real decider never called")
		}
		if v.LiveCallsUsed != uint32(realCalls) {
			t.Fatalf("live budget %d != real calls %d", v.LiveCallsUsed, realCalls)
		}
		r.mu.Lock()
		actions := append([]ActionView(nil), r.actions...)
		r.mu.Unlock()
		bySeat := map[poker.Seat]string{}
		for _, a := range actions {
			bySeat[a.Seat] = a.Source
		}
		if bySeat[0] != "model" {
			t.Fatalf("real seat source got %q want model", bySeat[0])
		}
		if bySeat[1] != "mock" {
			t.Fatalf("mock seat in live game source got %q want mock", bySeat[1])
		}
	})
}

// ---------- 额度耗尽：真实座位 cost_limit 暂停；mock 座位继续，绝不降级真实为 mock ----------

func TestSeatModelExhaustedBudget(t *testing.T) {
	t.Run("real_seat_pauses", func(t *testing.T) {
		r := newTestRepo()
		d := &countDecider{}
		c := mixedLiveConfig()
		c.MaxLiveCalls = 1
		seedMixed(t, r, c, 1, 1, false) // 翻前 actor=0（真实座位），额度已耗尽
		e := engine(t, r, seatModelFactory(d), Options{AllowPaidModels: true, MaxLiveCalls: 1})
		if _, err := e.Control(context.Background(), "mixed", "resume"); err != nil {
			t.Fatal(err)
		}
		eventually(t, func() bool {
			v, _ := e.Get(context.Background(), "mixed")
			return v.Status == "paused" && v.Reason != nil && *v.Reason == "cost_limit"
		})
		v, _ := e.Get(context.Background(), "mixed")
		if v.LiveCallsUsed != 1 || d.calls.Load() != 0 {
			t.Fatalf("real seat consumed beyond budget or requested: used=%d calls=%d", v.LiveCallsUsed, d.calls.Load())
		}
		r.mu.Lock()
		n := len(r.actions)
		r.mu.Unlock()
		if n != 0 {
			t.Fatalf("action committed while budget exhausted: %d", n)
		}
	})

	t.Run("mock_seat_continues", func(t *testing.T) {
		r := newTestRepo()
		d := &countDecider{}
		c := mixedLiveConfig()
		c.Seats[0].ModelRef = "mock" // actor=0 为 mock 座位
		c.Seats[1].ModelRef = "test/model"
		c.MaxLiveCalls = 1
		seedMixed(t, r, c, 1, 1, false)
		e := engine(t, r, seatModelFactory(d), Options{AllowPaidModels: true, MaxLiveCalls: 1})
		if _, err := e.Control(context.Background(), "mixed", "resume"); err != nil {
			t.Fatal(err)
		}
		eventually(t, func() bool { r.mu.Lock(); defer r.mu.Unlock(); return len(r.actions) >= 1 })
		r.mu.Lock()
		a := clone(r.actions[0])
		r.mu.Unlock()
		if a.Source != "mock" {
			t.Fatalf("mock seat source got %q want mock", a.Source)
		}
		v, _ := e.Get(context.Background(), "mixed")
		if v.LiveCallsUsed != 1 {
			t.Fatalf("mock seat changed budget: %d", v.LiveCallsUsed)
		}
		if d.calls.Load() != 0 {
			t.Fatalf("mock seat invoked real decider: %d", d.calls.Load())
		}
	})
}

// ---------- 真实请求失败：source=fallback 且已消耗额度保留 ----------

func TestSeatModelFallbackKeepsBudget(t *testing.T) {
	r := newTestRepo()
	d := &countDecider{err: errors.New("TEST_PROVIDER_FAILURE")}
	c := mixedLiveConfig()
	c.MaxHands = 100
	// 额度上限设为 1（Config 与 View 一致）：首个真实请求预扣后即稳定停在 cost_limit，
	// 与 manager_test.go 的 failed_call_precharged_and_limit_pauses 同一等待方式。
	c.MaxLiveCalls = 1
	seedMixed(t, r, c, 0, 1, false) // 翻前 actor=0 真实座位
	e := engine(t, r, seatModelFactory(d), Options{AllowPaidModels: true, MaxLiveCalls: 5, DecisionTimeout: time.Second})
	if _, err := e.Control(context.Background(), "mixed", "resume"); err != nil {
		t.Fatal(err)
	}
	// 必须等 worker 到达终态（额度耗尽暂停）后再断言：仅等首个动作落库会与后台
	// 继续发起的真实调用竞态，读取到中间计数（非 -race 下稳定多计、-race 下偶发）。
	eventually(t, func() bool {
		v, _ := e.Get(context.Background(), "mixed")
		return v.Status == "paused" && v.Reason != nil && *v.Reason == "cost_limit"
	})
	r.mu.Lock()
	if len(r.actions) < 1 {
		r.mu.Unlock()
		t.Fatal("no action committed before cost_limit")
	}
	a := clone(r.actions[0])
	r.mu.Unlock()
	if a.Source != "fallback" || a.ErrorCode == nil {
		t.Fatalf("real failure not marked fallback: %+v", a)
	}
	v, _ := e.Get(context.Background(), "mixed")
	if v.LiveCallsUsed != 1 {
		t.Fatalf("failed real request must retain budget: %d", v.LiveCallsUsed)
	}
}
