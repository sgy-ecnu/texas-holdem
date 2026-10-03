package service

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"texas-holdem/internal/agent"
	"texas-holdem/internal/poker"
	"time"
)

// 仅测试使用的内存 Repository；生产接线只接受 data.Repository。
type testRepo struct {
	mu        sync.Mutex
	records   map[string]Record
	hands     map[uint32]CompletedHandView
	actions   []ActionView
	fail      bool
	noCurrent bool
}

func newTestRepo() *testRepo {
	return &testRepo{records: map[string]Record{}, hands: map[uint32]CompletedHandView{}}
}
func clone[T any](v T) T { b, _ := json.Marshal(v); var out T; _ = json.Unmarshal(b, &out); return out }
func (r *testRepo) Create(_ context.Context, v Record) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.fail {
		return ErrStorage
	}
	for _, x := range r.records {
		if !terminal(x.View.Status) {
			return ErrConflict
		}
	}
	r.records[v.View.ID] = clone(v)
	return nil
}
func (r *testRepo) Get(_ context.Context, id string) (Record, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.fail {
		return Record{}, ErrStorage
	}
	v, ok := r.records[id]
	if !ok {
		return v, ErrNotFound
	}
	return clone(v), nil
}
func (r *testRepo) Current(_ context.Context) (Record, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.fail {
		return Record{}, ErrStorage
	}
	if r.noCurrent {
		return Record{}, ErrNotFound
	}
	for _, v := range r.records {
		if !terminal(v.View.Status) {
			return clone(v), nil
		}
	}
	return Record{}, ErrNotFound
}
func (r *testRepo) Commit(_ context.Context, c Commit) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.fail {
		return ErrStorage
	}
	old, ok := r.records[c.Record.View.ID]
	if !ok || old.View.Revision != c.ExpectedRevision || c.Record.View.Revision != c.ExpectedRevision+1 {
		return ErrConflict
	}
	r.records[c.Record.View.ID] = clone(c.Record)
	if c.Action != nil {
		r.actions = append(r.actions, clone(*c.Action))
	}
	if c.Hand != nil {
		r.hands[c.Hand.HandNo] = clone(*c.Hand)
	}
	return nil
}
func (r *testRepo) Hand(_ context.Context, _ string, n uint32) (CompletedHandView, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.hands[n]
	if !ok {
		return v, ErrNotFound
	}
	return clone(v), nil
}
func (r *testRepo) Health(context.Context) (HealthView, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.fail {
		return HealthView{MySQL: "unavailable", Redis: "available"}, ErrStorage
	}
	return HealthView{MySQL: "available", Redis: "available"}, nil
}

type deciderFunc func(context.Context, poker.Observation) (agent.Decision, error)

func (f deciderFunc) Decide(c context.Context, o poker.Observation) (agent.Decision, error) {
	return f(c, o)
}

type testFactory struct {
	d     agent.Decider
	calls atomic.Int32
}

func (f *testFactory) Models() []agent.ModelDescriptor {
	return []agent.ModelDescriptor{{Ref: "test/model", Label: "Test", Provider: "openai_compatible", Availability: "configured"}}
}
func (f *testFactory) NewDecider(string) (agent.Decider, error) { f.calls.Add(1); return f.d, nil }
func config() CreateGame {
	return CreateGame{Seats: []SeatConfig{{Seat: 0, Name: "A", Stack: 1000, ModelRef: "test/model"}, {Seat: 1, Name: "B", Stack: 1000, ModelRef: "test/model"}}, Button: 0, SmallBlind: 5, BigBlind: 10, MaxHands: 3, Mode: "mock"}
}
func engine(t *testing.T, r *testRepo, f agent.Factory, o Options) *Engine {
	t.Helper()
	e, err := New(context.Background(), r, f, o)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(e.Close)
	return e
}
func eventually(t *testing.T, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition not reached")
}

func seeded(t *testing.T, r *testRepo, check bool) {
	t.Helper()
	c := config()
	c.Mode = "live"
	c.MaxLiveCalls = 1
	s, err := poker.NewHand(poker.HandConfig{Players: []poker.PlayerInit{{Seat: 0, Stack: 1000}, {Seat: 1, Stack: 1000}}, Button: 0, SmallBlind: 5, BigBlind: 10}, poker.StandardDeck())
	if err != nil {
		t.Fatal(err)
	}
	if check {
		p := poker.Public(s)
		s, err = poker.Apply(s, *p.Actor, poker.Action{Kind: "call"})
		if err != nil {
			t.Fatal(err)
		}
	}
	b, err := poker.EncodeState(s)
	if err != nil {
		t.Fatal(err)
	}
	v := GameView{ID: "seed", Revision: 1, Status: "paused", Mode: "live", MaxHands: 3, MaxLiveCalls: 1, Seats: []SeatView{{Seat: 0, Stack: 1000}, {Seat: 1, Stack: 1000}}, Hand: &HandView{Number: 1, State: poker.Public(s)}}
	if err = r.Create(context.Background(), Record{Config: c, View: v, Snapshot: b}); err != nil {
		t.Fatal(err)
	}
}

func TestDecisionFallback(t *testing.T) {
	for _, kind := range []string{"timeout", "protocol", "illegal"} {
		for _, check := range []bool{false, true} {
			name := kind + "/fold"
			if check {
				name = kind + "/check"
			}
			t.Run(name, func(t *testing.T) {
				r := newTestRepo()
				seeded(t, r, check)
				f := &testFactory{d: deciderFunc(func(ctx context.Context, _ poker.Observation) (agent.Decision, error) {
					switch kind {
					case "timeout":
						<-ctx.Done()
						return agent.Decision{}, ctx.Err()
					case "protocol":
						return agent.Decision{}, errors.New("TEST_PROTOCOL_FAILURE")
					default:
						return agent.Decision{Action: poker.Action{Kind: "raise_to", To: -1}}, nil
					}
				})}
				e := engine(t, r, f, Options{AllowPaidModels: true, DecisionTimeout: 10 * time.Millisecond})
				if _, err := e.Control(context.Background(), "seed", "resume"); err != nil {
					t.Fatal(err)
				}
				eventually(t, func() bool { r.mu.Lock(); defer r.mu.Unlock(); return len(r.actions) > 0 })
				r.mu.Lock()
				a := clone(r.actions[0])
				r.mu.Unlock()
				want := poker.ActionKind("fold")
				if check {
					want = "check"
				}
				if a.Kind != want || a.Source != "fallback" || a.ErrorCode == nil {
					t.Fatalf("fallback mismatch: %+v", a)
				}
			})
		}
	}
}

func TestLiveGateAndBudget(t *testing.T) {
	t.Run("default_zero_network", func(t *testing.T) {
		r := newTestRepo()
		f := &testFactory{d: deciderFunc(func(context.Context, poker.Observation) (agent.Decision, error) {
			t.Error("unexpected live call")
			return agent.Decision{}, nil
		})}
		e := engine(t, r, f, Options{})
		c := config()
		v, err := e.Create(context.Background(), c)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = e.Control(context.Background(), v.ID, "start"); err != nil {
			t.Fatal(err)
		}
		eventually(t, func() bool { v, err = e.Get(context.Background(), v.ID); return err == nil && v.Status == "completed" })
		if f.calls.Load() != 0 || v.LiveCallsUsed != 0 || v.HandsCompleted != 3 {
			t.Fatal("mock invoked live or failed multihand")
		}
	})
	t.Run("dual_authorization", func(t *testing.T) {
		for _, allow := range []bool{false, true} {
			r := newTestRepo()
			f := &testFactory{d: agent.NewMockDecider()}
			e := engine(t, r, f, Options{AllowPaidModels: allow, MaxLiveCalls: 2})
			c := config()
			c.Mode = "live"
			c.MaxLiveCalls = 1
			_, err := e.Create(context.Background(), c)
			if !allow && !errors.Is(err, ErrForbidden) || allow && err != nil {
				t.Fatal("authorization mismatch")
			}
		}
	})
	t.Run("hard_cap_and_mock_budget", func(t *testing.T) {
		r := newTestRepo()
		f := &testFactory{d: agent.NewMockDecider()}
		e := engine(t, r, f, Options{AllowPaidModels: true, MaxLiveCalls: 2})
		for _, tc := range []struct {
			mode  Mode
			limit uint32
		}{{"live", 0}, {"live", 3}, {"mock", 1}} {
			c := config()
			c.Mode = tc.mode
			c.MaxLiveCalls = tc.limit
			if _, err := e.Create(context.Background(), c); !errors.Is(err, ErrInvalid) {
				t.Fatal("budget gate bypassed")
			}
		}
	})
	t.Run("failed_call_precharged_and_limit_pauses", func(t *testing.T) {
		r := newTestRepo()
		seeded(t, r, true)
		var network atomic.Int32
		f := &testFactory{d: deciderFunc(func(context.Context, poker.Observation) (agent.Decision, error) {
			v, err := r.Get(context.Background(), "seed")
			if err != nil || v.View.LiveCallsUsed != 1 || v.RequestID == "" {
				t.Error("request not durably reserved")
			}
			network.Add(1)
			return agent.Decision{}, errors.New("TEST_FAILURE")
		})}
		e := engine(t, r, f, Options{AllowPaidModels: true})
		_, err := e.Control(context.Background(), "seed", "resume")
		if err != nil {
			t.Fatal(err)
		}
		eventually(t, func() bool {
			v, _ := e.Get(context.Background(), "seed")
			return v.Status == "paused" && v.Reason != nil && *v.Reason == "cost_limit"
		})
		v, _ := e.Get(context.Background(), "seed")
		if network.Load() != 1 || v.LiveCallsUsed != 1 {
			t.Fatal("budget mismatch")
		}
		if _, err = e.Control(context.Background(), "seed", "resume"); !errors.Is(err, ErrConflict) {
			t.Fatal("exhausted budget resumed")
		}
	})
}

func TestPauseResumeStop(t *testing.T) {
	r := newTestRepo()
	seeded(t, r, false)
	// 增加额度，测试取消也计次。
	r.mu.Lock()
	v := r.records["seed"]
	v.Config.MaxLiveCalls = 5
	v.View.MaxLiveCalls = 5
	r.records["seed"] = v
	r.mu.Unlock()
	entered := make(chan poker.Observation, 3)
	release := make(chan struct{})
	canceled := make(chan struct{}, 3)
	f := &testFactory{d: deciderFunc(func(ctx context.Context, o poker.Observation) (agent.Decision, error) {
		entered <- o
		<-ctx.Done()
		canceled <- struct{}{}
		<-release
		return agent.Decision{Action: poker.Action{Kind: "call"}}, nil
	})}
	e := engine(t, r, f, Options{AllowPaidModels: true})
	if _, err := e.Control(context.Background(), "seed", "resume"); err != nil {
		t.Fatal(err)
	}
	var first poker.Observation
	select {
	case first = <-entered:
	case <-time.After(time.Second):
		t.Fatal("no request")
	}
	before, _ := r.Get(context.Background(), "seed")
	paused, err := e.Control(context.Background(), "seed", "pause")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("not canceled")
	}
	after, _ := r.Get(context.Background(), "seed")
	if string(before.Snapshot) != string(after.Snapshot) || after.Sequence != 0 || paused.Status != "paused" {
		t.Fatal("pause moved actor or applied fallback")
	}
	if _, err = e.Control(context.Background(), "seed", "resume"); err != nil {
		t.Fatal(err)
	}
	select {
	case second := <-entered:
		if first.Self != second.Self || first.Hole != second.Hole {
			t.Fatal("resume changed observation")
		}
	case <-time.After(time.Second):
		t.Fatal("no resumed request")
	}
	stopped, err := e.Control(context.Background(), "seed", "stop")
	if err != nil || stopped.Status != "stopped" {
		t.Fatal("stop failed")
	}
	close(release)
	if _, err = e.Control(context.Background(), "seed", "resume"); !errors.Is(err, ErrConflict) {
		t.Fatal("stop resumed")
	}
	eventually(t, func() bool { r.mu.Lock(); defer r.mu.Unlock(); return r.records["seed"].View.LiveCallsUsed == 2 })
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.actions) != 0 || len(r.hands) != 0 {
		t.Fatal("late result applied or stop faked settlement")
	}
}

func TestRecoveryPausesRunningGame(t *testing.T) {
	r := newTestRepo()
	seeded(t, r, false)
	r.mu.Lock()
	v := r.records["seed"]
	v.View.Status = "running"
	v.View.LiveCallsUsed = 1
	v.RequestID = "interrupted"
	r.records["seed"] = v
	r.mu.Unlock()
	f := &testFactory{d: agent.NewMockDecider()}
	e := engine(t, r, f, Options{AllowPaidModels: true})
	v, err := r.Get(context.Background(), "seed")
	if err != nil {
		t.Fatal(err)
	}
	if v.View.Status != "paused" || v.View.Reason == nil || *v.View.Reason != "recovery" || v.View.LiveCallsUsed != 1 || v.RequestID != "" || f.calls.Load() != 0 || len(r.hands) != 0 {
		t.Fatal("unsafe recovery")
	}
	_, _ = e.Current(context.Background())
}

func TestRepositoryCommitAtomicity(t *testing.T) {
	r := newTestRepo()
	c := config()
	v := Record{Config: c, View: GameView{ID: "cas", Revision: 1, Status: "created"}}
	if err := r.Create(context.Background(), v); err != nil {
		t.Fatal(err)
	}
	v.View.Revision = 2
	v.Snapshot = []byte("test-snapshot")
	commit := Commit{Record: v, ExpectedRevision: 1, Action: &ActionView{Sequence: 1, HandNo: 1}, Hand: &CompletedHandView{GameID: "cas", HandNo: 1}}
	if err := r.Commit(context.Background(), commit); err != nil {
		t.Fatal(err)
	}
	if err := r.Commit(context.Background(), commit); !errors.Is(err, ErrConflict) {
		t.Fatal("CAS double submit")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.actions) != 1 || len(r.hands) != 1 || string(r.records["cas"].Snapshot) != "test-snapshot" {
		t.Fatal("inconsistent atomic state")
	}
}

func TestZeroStacksAndStorageFailure(t *testing.T) {
	r := newTestRepo()
	e := engine(t, r, nil, Options{})
	c := config()
	c.Seats = append(c.Seats, SeatConfig{Seat: 2, Name: "Out", ModelRef: "mock", Stack: 0})
	c.MaxHands = 1
	v, err := e.Create(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = e.Control(context.Background(), v.ID, "start"); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { v, _ = e.Get(context.Background(), v.ID); return v.Status == "completed" })
	if len(v.Hand.State.Players) != 2 {
		t.Fatal("zero stack dealt in")
	}
	r.mu.Lock()
	r.fail = true
	r.mu.Unlock()
	if _, err = e.Create(context.Background(), config()); !errors.Is(err, ErrStorage) {
		t.Fatal("storage fallback")
	}
	if _, err = e.Health(context.Background()); !errors.Is(err, ErrStorage) {
		t.Fatal("fault not visible")
	}
}

func TestModelErrorCodeAllowlist(t *testing.T) {
	for _, code := range []string{"MODEL_AUTH_FAILED", "MODEL_TIMEOUT", "MODEL_INVALID_ACTION"} {
		if modelErrorCode(errors.New(code)) != code {
			t.Fatal("safe code lost")
		}
	}
	if modelErrorCode(errors.New("TEST_UNSAFE_PROVIDER_DETAIL")) != "MODEL_PROTOCOL_ERROR" {
		t.Fatal("unsafe error exposed")
	}
}
