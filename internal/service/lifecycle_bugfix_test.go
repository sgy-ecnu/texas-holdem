package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"texas-holdem/internal/agent"
	"texas-holdem/internal/poker"
)

// Bug 3：付费开关关闭后，旧 live 局不得再 start/resume；未授权必须返回 ErrForbidden，
// 且授权变化不能被当成全局存储故障锁存。
func TestLiveAuthorizationRecheckedAtRuntime(t *testing.T) {
	t.Run("start_forbidden", func(t *testing.T) {
		r := newTestRepo()
		c := config()
		c.Mode = "live"
		c.MaxLiveCalls = 1
		v := GameView{ID: "seed", Revision: 1, Status: "created", Mode: "live", MaxHands: c.MaxHands, MaxLiveCalls: 1,
			Seats: []SeatView{{Seat: 0, Name: "A", Stack: 1000, ModelRef: "test/model"}, {Seat: 1, Name: "B", Stack: 1000, ModelRef: "test/model"}}}
		if err := r.Create(context.Background(), Record{Config: c, View: v}); err != nil {
			t.Fatal(err)
		}
		e := engine(t, r, &testFactory{d: agent.NewMockDecider()}, Options{AllowPaidModels: false})
		if _, err := e.Control(context.Background(), "seed", "start"); !errors.Is(err, ErrForbidden) {
			t.Fatalf("start got %v want forbidden", err)
		}
		rec, _ := r.Get(context.Background(), "seed")
		if rec.View.Status != "created" {
			t.Fatalf("forbidden start mutated status to %q", rec.View.Status)
		}
		if _, err := e.Health(context.Background()); err != nil {
			t.Fatalf("authorization revoke latched global fault: %v", err)
		}
	})

	t.Run("resume_forbidden", func(t *testing.T) {
		r := newTestRepo()
		seeded(t, r, false)
		e := engine(t, r, &testFactory{d: agent.NewMockDecider()}, Options{AllowPaidModels: false})
		if _, err := e.Control(context.Background(), "seed", "resume"); !errors.Is(err, ErrForbidden) {
			t.Fatalf("resume got %v want forbidden", err)
		}
		rec, _ := r.Get(context.Background(), "seed")
		if rec.View.Status != "paused" {
			t.Fatalf("forbidden resume mutated status to %q", rec.View.Status)
		}
		if _, err := e.Get(context.Background(), "seed"); err != nil {
			t.Fatalf("authorization revoke latched global fault: %v", err)
		}
	})

	t.Run("running_worker_pauses_without_fault", func(t *testing.T) {
		r := newTestRepo()
		seeded(t, r, false)
		e := engine(t, r, &testFactory{d: agent.NewMockDecider()}, Options{AllowPaidModels: false})
		r.mu.Lock()
		v := r.records["seed"]
		v.View.Status = "running"
		r.records["seed"] = v
		r.mu.Unlock()
		e.step()
		rec, _ := r.Get(context.Background(), "seed")
		if rec.View.Status != "paused" || rec.View.Reason == nil || *rec.View.Reason != "unauthorized" {
			t.Fatalf("revoked live game not paused: %+v", rec.View)
		}
		e.mu.Lock()
		fault := e.fault
		e.mu.Unlock()
		if fault {
			t.Fatal("authorization revoke latched global fault")
		}
		if _, err := e.Health(context.Background()); err != nil {
			t.Fatalf("authorization revoke reported storage failure: %v", err)
		}
	})
}

// Bug 6：live 局进入终态后必须释放 decider，避免永久驻留内存。
func TestTerminalGameReleasesDeciders(t *testing.T) {
	t.Run("stop_releases", func(t *testing.T) {
		r := newTestRepo()
		seeded(t, r, true)
		f := &testFactory{d: deciderFunc(func(ctx context.Context, _ poker.Observation) (agent.Decision, error) {
			<-ctx.Done()
			return agent.Decision{}, ctx.Err()
		})}
		e := engine(t, r, f, Options{AllowPaidModels: true, DecisionTimeout: 10 * time.Second})
		if _, err := e.Control(context.Background(), "seed", "resume"); err != nil {
			t.Fatal(err)
		}
		eventually(t, func() bool {
			e.mu.Lock()
			defer e.mu.Unlock()
			return e.deciders["seed"] != nil
		})
		if _, err := e.Control(context.Background(), "seed", "stop"); err != nil {
			t.Fatal(err)
		}
		e.mu.Lock()
		_, ok := e.deciders["seed"]
		e.mu.Unlock()
		if ok {
			t.Fatal("decider retained after stop")
		}
	})

	t.Run("completion_releases", func(t *testing.T) {
		r := newTestRepo()
		seeded(t, r, false)
		e := engine(t, r, &testFactory{d: agent.NewMockDecider()}, Options{AllowPaidModels: true})
		e.mu.Lock()
		e.deciders["seed"] = map[poker.Seat]agent.Decider{0: agent.NewMockDecider()}
		e.mu.Unlock()
		r.mu.Lock()
		v := r.records["seed"]
		v.Snapshot = nil
		v.View.HandsCompleted = v.Config.MaxHands
		v.View.Status = "running"
		r.records["seed"] = v
		r.mu.Unlock()
		eventually(t, func() bool {
			rec, _ := r.Get(context.Background(), "seed")
			return rec.View.Status == "completed"
		})
		e.mu.Lock()
		_, ok := e.deciders["seed"]
		e.mu.Unlock()
		if ok {
			t.Fatal("decider retained after completion")
		}
	})
}
