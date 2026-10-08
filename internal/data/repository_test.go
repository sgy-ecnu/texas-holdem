package data

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/DATA-DOG/go-sqlmock"
	"strings"
	"testing"
	"texas-holdem/internal/server"
	"texas-holdem/internal/service"
	"time"
)

type cacheStub struct {
	fail    bool
	deleted bool
	value   []byte
}

func (c *cacheStub) Ping(context.Context) error {
	if c.fail {
		return errors.New("TEST_CACHE_FAILURE")
	}
	return nil
}
func (c *cacheStub) Set(_ context.Context, _ string, b []byte, _ time.Duration) error {
	if c.fail {
		return errors.New("TEST_CACHE_FAILURE")
	}
	c.value = append([]byte(nil), b...)
	return nil
}
func (c *cacheStub) Delete(context.Context, string) error { c.deleted = true; return nil }
func sqlRepo(t *testing.T) (*Repository, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
		_ = db.Close()
	})
	return &Repository{db: db, cache: &cacheStub{}, encode: server.MarshalGame}, mock
}
func commitRecord() service.Commit {
	hash := service.PlayerSessionHash{}
	deadline := time.Now().Add(time.Minute)
	return service.Commit{
		Record: service.Record{
			View:             service.GameView{ID: "game", Revision: 2, Status: "running"},
			Snapshot:         []byte("TEST_PRIVATE_SNAPSHOT"),
			HumanSessionHash: &hash,
			HumanWait:        &service.HumanWait{HandNo: 1, Seat: 0, Deadline: &deadline},
		},
		ExpectedRevision: 1, Action: &service.ActionView{HandNo: 1, Sequence: 1}, Hand: &service.CompletedHandView{GameID: "game", HandNo: 1},
	}
}
func TestRepositoryCommitAtomicity(t *testing.T) {
	for _, scenario := range []string{"commit", "cas_conflict", "hand_failure", "commit_failure"} {
		t.Run(scenario, func(t *testing.T) {
			r, m := sqlRepo(t)
			c := commitRecord()
			m.ExpectBegin()
			if scenario == "cas_conflict" {
				m.ExpectExec("UPDATE games").WillReturnResult(sqlmock.NewResult(0, 0))
				m.ExpectRollback()
			} else {
				m.ExpectExec("UPDATE games").WillReturnResult(sqlmock.NewResult(0, 1))
				m.ExpectExec("INSERT INTO actions").WillReturnResult(sqlmock.NewResult(0, 1))
				if scenario == "hand_failure" {
					m.ExpectExec("INSERT INTO hands").WillReturnError(errors.New("TEST_INSERT_FAILURE"))
					m.ExpectRollback()
				} else {
					m.ExpectExec("INSERT INTO hands").WillReturnResult(sqlmock.NewResult(0, 1))
					if scenario == "commit_failure" {
						m.ExpectCommit().WillReturnError(errors.New("TEST_COMMIT_FAILURE"))
					} else {
						m.ExpectCommit()
					}
				}
			}
			err := r.Commit(context.Background(), c)
			if scenario == "commit" && err != nil || scenario == "cas_conflict" && !errors.Is(err, service.ErrConflict) || scenario != "commit" && scenario != "cas_conflict" && !errors.Is(err, service.ErrStorage) {
				t.Fatal("transaction outcome mismatch")
			}
			cache := r.cache.(*cacheStub)
			if scenario == "commit" {
				if len(cache.value) == 0 {
					t.Fatal("no public cache")
				}
				var v map[string]any
				if err = json.Unmarshal(cache.value, &v); err != nil {
					t.Fatal(err)
				}
				for _, private := range []string{"Snapshot", "HumanSessionHash", "HumanWait", "HumanActionTimeout", "human_session_hash"} {
					if _, ok := v[private]; ok {
						t.Fatalf("private field %q cached", private)
					}
				}
				if strings.Contains(string(cache.value), "Ac") {
					t.Fatal("hole cards cached")
				}
				if _, ok := v["revision"]; !ok {
					t.Fatal("cache not DTO")
				}
			} else if len(cache.value) != 0 {
				t.Fatal("cache published before successful commit")
			}
		})
	}
}
func TestRedisDegradation(t *testing.T) {
	r, m := sqlRepo(t)
	cache := &cacheStub{fail: true, value: []byte(`{"revision":1}`)}
	r.cache = cache
	c := commitRecord()
	c.Action = nil
	c.Hand = nil
	m.ExpectBegin()
	m.ExpectExec("UPDATE games").WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectCommit()
	if err := r.Commit(context.Background(), c); err != nil {
		t.Fatal("Redis failure rolled back MySQL")
	}
	if !r.degraded.Load() || !cache.deleted {
		t.Fatal("cache not invalidated")
	}
	b, _ := json.Marshal(c.Record)
	m.ExpectQuery("SELECT record FROM games WHERE id").WithArgs("game").WillReturnRows(sqlmock.NewRows([]string{"record"}).AddRow(b))
	v, err := r.Get(context.Background(), "game")
	if err != nil || v.View.Revision != 2 {
		t.Fatal("old cache used")
	}
	m.ExpectPing()
	h, err := r.Health(context.Background())
	if !errors.Is(err, service.ErrStorage) || h.MySQL != "available" || h.Redis != "unavailable" || !h.StorageDegraded {
		t.Fatal("degraded health mismatch")
	}
}
func TestSingleInstanceLockGuard(t *testing.T) {
	r, m := sqlRepo(t)
	conn, err := r.db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	r.lock = conn
	m.ExpectQuery("SELECT IS_USED_LOCK").WillReturnRows(sqlmock.NewRows([]string{"owned"}).AddRow(0))
	if _, err = r.Get(context.Background(), "game"); !errors.Is(err, service.ErrStorage) {
		t.Fatal("lost scheduler lock did not stop reads")
	}
}
func TestSchemaFailsClosed(t *testing.T) {
	r, m := sqlRepo(t)
	m.ExpectQuery("SELECT MAX").WillReturnRows(sqlmock.NewRows([]string{"version"}).AddRow(3))
	if err := validateSchema(context.Background(), r.db); !errors.Is(err, service.ErrStorage) {
		t.Fatal("unknown schema accepted")
	}
}
