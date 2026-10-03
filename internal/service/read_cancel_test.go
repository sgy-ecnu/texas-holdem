package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"texas-holdem/internal/agent"
	"texas-holdem/internal/poker"
)

type readStageKey struct{}
type cancellationRepo struct{ *testRepo }

func requestStageError(ctx context.Context, stage string) error {
	if ctx.Value(readStageKey{}) == stage && ctx.Err() != nil {
		return fmt.Errorf("TEST_PRIVATE_REPO_DETAIL: %w", ctx.Err())
	}
	return nil
}
func (r cancellationRepo) Get(ctx context.Context, id string) (Record, error) {
	if err := requestStageError(ctx, "get"); err != nil {
		return Record{}, err
	}
	return r.testRepo.Get(ctx, id)
}
func (r cancellationRepo) Current(ctx context.Context) (Record, error) {
	if err := requestStageError(ctx, "current"); err != nil {
		return Record{}, err
	}
	return r.testRepo.Current(ctx)
}
func (r cancellationRepo) Hand(ctx context.Context, id string, n uint32) (CompletedHandView, error) {
	if err := requestStageError(ctx, "hand"); err != nil {
		return CompletedHandView{}, err
	}
	return r.testRepo.Hand(ctx, id, n)
}
func (r cancellationRepo) Health(ctx context.Context) (HealthView, error) {
	if err := requestStageError(ctx, "health"); err != nil {
		return HealthView{MySQL: "available", Redis: "available"}, err
	}
	return r.testRepo.Health(ctx)
}
func canceledReadContext(stage string, deadline bool) (context.Context, context.CancelFunc) {
	base := context.WithValue(context.Background(), readStageKey{}, stage)
	if deadline {
		return context.WithDeadline(base, time.Now().Add(-time.Second))
	}
	ctx, cancel := context.WithCancel(base)
	cancel()
	return ctx, cancel
}

func TestReadCancellationKeepsWorker(t *testing.T) {
	for _, deadline := range []bool{false, true} {
		kind := "cancel"
		if deadline {
			kind = "deadline"
		}
		for _, operation := range []string{"get", "current", "hand_lookup", "hand", "get_health", "health"} {
			t.Run(kind+"/"+operation, func(t *testing.T) {
				r := newTestRepo()
				seeded(t, r, true)
				r.mu.Lock()
				v := r.records["seed"]
				v.View.HandsCompleted = 1
				r.records["seed"] = v
				r.hands[1] = CompletedHandView{GameID: "seed", HandNo: 1}
				r.mu.Unlock()
				entered, release := make(chan struct{}), make(chan struct{})
				var releaseOnce sync.Once
				unblock := func() { releaseOnce.Do(func() { close(release) }) }
				defer unblock()
				var calls atomic.Int32
				f := &testFactory{d: deciderFunc(func(ctx context.Context, _ poker.Observation) (agent.Decision, error) {
					calls.Add(1)
					close(entered)
					select {
					case <-release:
						return agent.Decision{Action: poker.Action{Kind: "check"}}, nil
					case <-ctx.Done():
						return agent.Decision{}, ctx.Err()
					}
				})}
				e, err := New(context.Background(), cancellationRepo{r}, f, Options{AllowPaidModels: true, DecisionTimeout: 5 * time.Second})
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(e.Close)
				if _, err := e.Control(context.Background(), "seed", "resume"); err != nil {
					t.Fatal(err)
				}
				select {
				case <-entered:
				case <-time.After(time.Second):
					t.Fatal("worker did not start")
				}
				stage := operation
				if operation == "hand_lookup" {
					stage = "get"
				}
				if operation == "get_health" {
					stage = "health"
				}
				ctx, cancel := canceledReadContext(stage, deadline)
				defer cancel()
				switch operation {
				case "get", "get_health":
					_, err = e.Get(ctx, "seed")
				case "current":
					_, err = e.Current(ctx)
				case "hand_lookup", "hand":
					_, err = e.GetHand(ctx, "seed", 1)
				case "health":
					var h HealthView
					h, err = e.Health(ctx)
					if h.MySQL == "unavailable" || h.StorageDegraded {
						t.Error("caller cancellation falsely reported storage failure")
					}
				}
				if err != ErrStorage {
					t.Errorf("read did not return fixed safe error: %v", err)
				}
				if _, err := e.Get(context.Background(), "seed"); err != nil {
					t.Error("subsequent Get faulted", err)
				}
				if _, err := e.Health(context.Background()); err != nil {
					t.Error("subsequent Health faulted", err)
				}
				// 当前计费决策必须仍在进行；取消轮询不能取消它或重占预算。
				e.mu.Lock()
				active := e.activeRequest != "" && !e.fault
				e.mu.Unlock()
				if !active {
					t.Error("read cancellation stopped active worker")
					return
				}
				// 释放后等待动作落库，额度 1 不允许第二次模型调用。
				unblock()
				eventually(t, func() bool { r.mu.Lock(); defer r.mu.Unlock(); return len(r.actions) == 1 })
				record, _ := r.Get(context.Background(), "seed")
				if calls.Load() != 1 || record.View.LiveCallsUsed != 1 {
					t.Error("poll cancellation caused extra charge or retry")
				}
			})
		}
	}
}

type failingReadRepo struct {
	*testRepo
	err error
}

func (r failingReadRepo) Get(context.Context, string) (Record, error) { return Record{}, r.err }

func TestReadStorageFailureStillLatches(t *testing.T) {
	for _, cause := range []error{ErrStorage, errors.New("TEST_DATABASE_ERROR"), context.DeadlineExceeded} {
		t.Run(cause.Error(), func(t *testing.T) {
			r := newTestRepo()
			e, err := New(context.Background(), failingReadRepo{r, cause}, nil, Options{})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(e.Close)
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			// 调用方已取消也不能掩盖真实存储错误或不匹配的依赖 deadline。
			if _, err := e.Get(ctx, "game"); err != ErrStorage {
				t.Fatal("unsafe read error")
			}
			if _, err := e.Health(context.Background()); err != ErrStorage {
				t.Error("real dependency failure did not latch")
			}
		})
	}
}

type uncertainWriteRepo struct {
	*testRepo
	cause  error
	writes atomic.Int32
}

func (r *uncertainWriteRepo) Commit(ctx context.Context, c Commit) error {
	r.writes.Add(1)
	// 模拟预算预占已持久化，但提交响应取消/超时；禁止重新占用或调用模型。
	if err := r.testRepo.Commit(ctx, c); err != nil {
		return err
	}
	return r.cause
}

func TestUncertainBudgetWriteStillFaults(t *testing.T) {
	for _, cause := range []error{context.Canceled, context.DeadlineExceeded} {
		t.Run(cause.Error(), func(t *testing.T) {
			r := newTestRepo()
			seeded(t, r, false)
			var calls atomic.Int32
			f := &testFactory{d: deciderFunc(func(context.Context, poker.Observation) (agent.Decision, error) {
				calls.Add(1)
				return agent.Decision{Action: poker.Action{Kind: "fold"}}, nil
			})}
			wr := &uncertainWriteRepo{testRepo: r, cause: cause}
			// 手动推进同一 worker，避免定时器让故障前后计数不确定。
			e := &Engine{repo: wr, factory: f, opts: Options{AllowPaidModels: true}, ctx: context.Background(), deciders: make(map[string]map[poker.Seat]agent.Decider)}
			r.mu.Lock()
			record := r.records["seed"]
			record.View.Status = "running"
			r.records["seed"] = record
			r.mu.Unlock()
			e.step()
			e.step()
			record, _ = r.Get(context.Background(), "seed")
			if !e.fault || wr.writes.Load() != 1 || record.View.LiveCallsUsed != 1 || calls.Load() != 0 {
				t.Error("uncertain budget write retried, invoked model or did not fault")
			}
			if _, err := e.Get(context.Background(), "seed"); err != ErrStorage {
				t.Error("uncertain write fault not latched")
			}
		})
	}
}

type failingHealthRepo struct {
	*testRepo
	h   HealthView
	err error
}

func (r failingHealthRepo) Health(ctx context.Context) (HealthView, error) {
	if ctx.Value(readStageKey{}) == "health_failure" {
		return r.h, r.err
	}
	return r.testRepo.Health(ctx)
}

func TestHealthReadFailureStillLatches(t *testing.T) {
	for _, tc := range []struct {
		name  string
		err   error
		mysql string
	}{
		{"database", ErrStorage, "unavailable"},
		{"unmarked_database", errors.New("TEST_DATABASE_ERROR"), "available"},
		{"dependency_deadline", context.DeadlineExceeded, "available"},
		{"confirmed_mysql_failure_with_cancel", context.Canceled, "unavailable"},
	} {
		for _, operation := range []string{"get_health", "health"} {
			t.Run(tc.name+"/"+operation, func(t *testing.T) {
				r := newTestRepo()
				seeded(t, r, false)
				e, err := New(context.Background(), failingHealthRepo{r, HealthView{MySQL: tc.mysql, Redis: "available"}, tc.err}, nil, Options{})
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(e.Close)
				ctx, cancel := canceledReadContext("health_failure", false)
				defer cancel()
				if operation == "get_health" {
					_, err = e.Get(ctx, "seed")
				} else {
					_, err = e.Health(ctx)
				}
				if err != ErrStorage {
					t.Errorf("real health failure got %v want safe storage error", err)
				}
				if _, err := e.Health(context.Background()); err != ErrStorage {
					t.Error("real health failure not latched")
				}
			})
		}
	}
}
