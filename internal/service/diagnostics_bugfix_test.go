package service

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"texas-holdem/internal/agent"
	"texas-holdem/internal/poker"
)

type diagEvent struct{ code, stage, game string }

// directEngine 构造不启动后台 loop 的引擎，保证诊断断言确定性。
func directEngine(r Repository, f agent.Factory) *Engine {
	return &Engine{repo: r, factory: f, opts: Options{AllowPaidModels: true}, ctx: context.Background(), deciders: make(map[string]map[poker.Seat]agent.Decider)}
}

type failingFactory struct{ err error }

func (f failingFactory) Models() []agent.ModelDescriptor {
	return []agent.ModelDescriptor{{Ref: "test/model", Label: "Test", Provider: "openai_compatible", Availability: "configured"}}
}
func (f failingFactory) NewDecider(string) (agent.Decider, error) { return nil, f.err }

type lockLostRepo struct{ *testRepo }

func (r lockLostRepo) Commit(context.Context, Commit) error {
	return fmt.Errorf("%w: %w", ErrStorage, ErrInstanceLock)
}

// Bug 4：后台故障必须在最终处理边界留下固定安全码 + 阶段 + 非敏感标识，
// 区分快照 / 模型构造 / 事务（实例锁）等类别，同时保持 fail-closed。
func TestBackgroundFailureDiagnostics(t *testing.T) {
	t.Run("snapshot_decode", func(t *testing.T) {
		r := newTestRepo()
		seeded(t, r, true)
		r.mu.Lock()
		v := r.records["seed"]
		v.Snapshot = []byte("not-a-snapshot")
		v.View.Status = "running"
		r.records["seed"] = v
		r.mu.Unlock()
		e := directEngine(r, &testFactory{d: agent.NewMockDecider()})
		var ev []diagEvent
		e.diag = func(code, stage, game string) { ev = append(ev, diagEvent{code, stage, game}) }
		e.step()
		if len(ev) != 1 || ev[0] != (diagEvent{"BG_SNAPSHOT_FAILED", "snapshot", "seed"}) {
			t.Fatalf("snapshot diagnostic mismatch: %+v", ev)
		}
		if !e.fault {
			t.Fatal("snapshot failure must remain fail-closed")
		}
	})

	t.Run("model_construction", func(t *testing.T) {
		r := newTestRepo()
		seeded(t, r, false)
		r.mu.Lock()
		v := r.records["seed"]
		v.View.Status = "running"
		r.records["seed"] = v
		r.mu.Unlock()
		e := directEngine(r, failingFactory{err: errors.New("TEST_PROVIDER_DETAIL")})
		var ev []diagEvent
		e.diag = func(code, stage, game string) { ev = append(ev, diagEvent{code, stage, game}) }
		e.step()
		if len(ev) != 1 || ev[0] != (diagEvent{"BG_MODEL_FAILED", "model", "seed"}) {
			t.Fatalf("model diagnostic mismatch: %+v", ev)
		}
		if !e.fault {
			t.Fatal("model construction failure must remain fail-closed")
		}
	})

	t.Run("instance_lock_txn", func(t *testing.T) {
		r := newTestRepo()
		seeded(t, r, false)
		e := directEngine(lockLostRepo{r}, &testFactory{d: agent.NewMockDecider()})
		e.deciders["seed"] = map[poker.Seat]agent.Decider{0: agent.NewMockDecider()}
		r.mu.Lock()
		v := r.records["seed"]
		v.View.Status = "running"
		r.records["seed"] = v
		r.mu.Unlock()
		var ev []diagEvent
		e.diag = func(code, stage, game string) { ev = append(ev, diagEvent{code, stage, game}) }
		e.step()
		if len(ev) != 1 || ev[0] != (diagEvent{"BG_TXN_FAILED", "instance_lock", "seed"}) {
			t.Fatalf("txn diagnostic mismatch: %+v", ev)
		}
		if !e.fault {
			t.Fatal("instance lock failure must remain fail-closed")
		}
	})

	t.Run("auth_denied_is_not_fault", func(t *testing.T) {
		r := newTestRepo()
		seeded(t, r, false)
		e := directEngine(r, &testFactory{d: agent.NewMockDecider()})
		e.opts.AllowPaidModels = false
		r.mu.Lock()
		v := r.records["seed"]
		v.View.Status = "running"
		r.records["seed"] = v
		r.mu.Unlock()
		var ev []diagEvent
		e.diag = func(code, stage, game string) { ev = append(ev, diagEvent{code, stage, game}) }
		e.step()
		if len(ev) != 1 || ev[0] != (diagEvent{"BG_AUTH_DENIED", "authorization", "seed"}) {
			t.Fatalf("auth diagnostic mismatch: %+v", ev)
		}
		if e.fault {
			t.Fatal("authorization denial must not latch global fault")
		}
	})
}
