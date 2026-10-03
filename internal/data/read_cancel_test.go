package data

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"texas-holdem/internal/service"
)

type pingErrorCache struct {
	cacheStub
	err   error
	pings int
}

func (c *pingErrorCache) Ping(context.Context) error { c.pings++; return c.err }

func TestRepositoryReadCancellation(t *testing.T) {
	for _, cause := range []error{context.Canceled, context.DeadlineExceeded} {
		for _, stage := range []string{"guard", "get", "current", "hand", "health_guard", "health_mysql", "health_redis"} {
			t.Run(cause.Error()+"/"+stage, func(t *testing.T) {
				r, m := sqlRepo(t)
				ctx := context.Background()
				wrapped := fmt.Errorf("TEST_PRIVATE_DRIVER_DETAIL: %w", cause)
				cache := &pingErrorCache{}
				r.cache = cache
				if stage == "guard" || stage == "health_guard" {
					conn, err := r.db.Conn(context.Background())
					if err != nil {
						t.Fatal(err)
					}
					t.Cleanup(func() { _ = conn.Close() })
					r.lock = conn
					var cancel context.CancelFunc
					if cause == context.DeadlineExceeded {
						ctx, cancel = context.WithDeadline(ctx, time.Now().Add(-time.Second))
					} else {
						ctx, cancel = context.WithCancel(ctx)
					}
					cancel()
				} else if stage == "health_mysql" {
					m.ExpectPing().WillReturnError(wrapped)
				} else if stage == "health_redis" {
					m.ExpectPing()
					cache.err = wrapped
				} else {
					q := "SELECT record FROM games"
					if stage == "hand" {
						q = "SELECT record FROM hands"
					}
					m.ExpectQuery(q).WillReturnError(wrapped)
				}
				var err error
				switch stage {
				case "current":
					_, err = r.Current(ctx)
				case "hand":
					_, err = r.Hand(ctx, "game", 1)
				case "health_guard", "health_mysql", "health_redis":
					var h service.HealthView
					h, err = r.Health(ctx)
					if h.MySQL == "unavailable" || h.Redis == "unavailable" || h.StorageDegraded || r.degraded.Load() {
						t.Error("request cancellation reported dependency failure or polluted degradation")
					}
					if stage != "health_redis" && cache.pings != 0 {
						t.Error("canceled health performed extra cache ping")
					}
				default:
					_, err = r.Get(ctx, "game")
				}
				if !errors.Is(err, cause) {
					t.Errorf("cancellation cause lost: got %v want %v", err, cause)
				}
				cache.err = nil
				if r.lock != nil {
					m.ExpectQuery("SELECT IS_USED_LOCK").WillReturnRows(sqlmock.NewRows([]string{"owned"}).AddRow(1))
				}
				b, _ := json.Marshal(service.Record{View: service.GameView{ID: "game"}})
				m.ExpectQuery("SELECT record FROM games").WillReturnRows(sqlmock.NewRows([]string{"record"}).AddRow(b))
				if _, err := r.Get(context.Background(), "game"); err != nil {
					t.Error("subsequent read failed", err)
				}
				if r.lock != nil {
					m.ExpectQuery("SELECT IS_USED_LOCK").WillReturnRows(sqlmock.NewRows([]string{"owned"}).AddRow(1))
				}
				m.ExpectPing()
				if _, err := r.Health(context.Background()); err != nil {
					t.Error("subsequent health failed", err)
				}
			})
		}
	}
}

func TestRepositoryCanceledContext(t *testing.T) {
	for _, deadline := range []bool{false, true} {
		name := "cancel"
		if deadline {
			name = "deadline"
		}
		t.Run(name, func(t *testing.T) {
			r, _ := sqlRepo(t)
			ctx, cancel := context.WithCancel(context.Background())
			if deadline {
				cancel()
				ctx, cancel = context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
			}
			cancel()
			if _, err := r.Get(ctx, "game"); !errors.Is(err, ctx.Err()) {
				t.Errorf("database/sql cancellation lost: %v", err)
			}
		})
	}
}

func TestRepositoryRealFailureWinsOverCancellation(t *testing.T) {
	for _, stage := range []string{"lock_lost", "guard_database", "load_database", "mysql_then_cache_cancel"} {
		t.Run(stage, func(t *testing.T) {
			r, m := sqlRepo(t)
			if stage == "lock_lost" || stage == "guard_database" {
				conn, err := r.db.Conn(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = conn.Close() })
				r.lock = conn
				if stage == "lock_lost" {
					m.ExpectQuery("SELECT IS_USED_LOCK").WillReturnRows(sqlmock.NewRows([]string{"owned"}).AddRow(0))
				} else {
					m.ExpectQuery("SELECT IS_USED_LOCK").WillReturnError(errors.New("TEST_LOCK_DATABASE_FAILURE"))
				}
			} else if stage == "load_database" {
				m.ExpectQuery("SELECT record FROM games").WillReturnError(errors.New("TEST_DATABASE_FAILURE"))
			} else {
				m.ExpectPing().WillReturnError(errors.New("TEST_DATABASE_FAILURE"))
				r.cache = &pingErrorCache{err: context.Canceled}
			}
			var err error
			if stage == "mysql_then_cache_cancel" {
				var h service.HealthView
				h, err = r.Health(context.Background())
				if h.MySQL != "unavailable" || !h.StorageDegraded || r.degraded.Load() {
					t.Error("real MySQL failure hidden or cache cancellation polluted state")
				}
			} else {
				_, err = r.Get(context.Background(), "game")
			}
			if !errors.Is(err, service.ErrStorage) {
				t.Errorf("real failure got %v want fixed storage error", err)
			}
		})
	}
}

func TestRepositoryWriteCancellationRemainsConservative(t *testing.T) {
	for _, cause := range []error{context.Canceled, context.DeadlineExceeded} {
		for _, stage := range []string{"create", "begin", "update", "commit"} {
			t.Run(cause.Error()+"/"+stage, func(t *testing.T) {
				r, m := sqlRepo(t)
				c := commitRecord()
				c.Action, c.Hand = nil, nil
				if stage == "create" {
					m.ExpectExec("INSERT INTO games").WillReturnError(cause)
				} else if stage == "begin" {
					m.ExpectBegin().WillReturnError(cause)
				} else {
					m.ExpectBegin()
					if stage == "update" {
						m.ExpectExec("UPDATE games").WillReturnError(cause)
						m.ExpectRollback()
					} else {
						m.ExpectExec("UPDATE games").WillReturnResult(sqlmock.NewResult(0, 1))
						m.ExpectCommit().WillReturnError(cause)
					}
				}
				var err error
				if stage == "create" {
					err = r.Create(context.Background(), c.Record)
				} else {
					err = r.Commit(context.Background(), c)
				}
				if err != service.ErrStorage {
					t.Errorf("uncertain write got %v want fixed storage error", err)
				}
				if len(r.cache.(*cacheStub).value) != 0 {
					t.Error("uncertain write published cache")
				}
			})
		}
	}
}

func TestGuardPollingCancellationDoesNotCancelLockQuery(t *testing.T) {
	for _, deadline := range []bool{false, true} {
		name := "cancel"
		if deadline {
			name = "deadline"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			if deadline {
				cancel()
				ctx, cancel = context.WithTimeout(context.Background(), 10*time.Millisecond)
			}
			defer cancel()
			matcher := sqlmock.QueryMatcherFunc(func(expected, actual string) error {
				if expected == "SELECT IS_USED_LOCK" {
					if deadline {
						<-ctx.Done()
					} else {
						cancel()
					}
				}
				return sqlmock.QueryMatcherRegexp.Match(expected, actual)
			})
			db, m, err := sqlmock.New(sqlmock.QueryMatcherOption(matcher))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			conn, err := db.Conn(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			r := &Repository{db: db, lock: conn}
			m.ExpectQuery("SELECT IS_USED_LOCK").WillDelayFor(time.Millisecond).WillReturnRows(sqlmock.NewRows([]string{"owned"}).AddRow(1))
			if err := r.guard(ctx); !errors.Is(err, ctx.Err()) {
				t.Errorf("caller cancellation got %v want %v", err, ctx.Err())
			}
			// sqlmock 延迟查询会在传入 context 被取消时中断；成功消费行验证锁查询未被轮询取消。
			if err := m.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
			m.ExpectQuery("SELECT IS_USED_LOCK").WillReturnRows(sqlmock.NewRows([]string{"owned"}).AddRow(1))
			if err := r.guard(context.Background()); err != nil {
				t.Error("subsequent lock guard failed", err)
			}
			if err := m.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
