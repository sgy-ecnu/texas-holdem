package data

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/DATA-DOG/go-sqlmock"
	"testing"
	"texas-holdem/internal/service"
	"time"
)

var tableCols = []string{"id", "owner_user_id", "name", "revision", "config", "created_at", "updated_at"}

func tableConfigBytes(t *testing.T) []byte {
	t.Helper()
	b, err := json.Marshal(service.TableConfig{
		Seats:        []service.TableSeat{{Seat: 0, Name: "A", Stack: 1000, ModelRef: "mock", Kind: service.SeatAI}},
		Button:       0,
		SmallBlind:   5,
		BigBlind:     10,
		MaxHands:     3,
		Mode:         "mock",
		MaxLiveCalls: 0,
	})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestCreateTablePersists(t *testing.T) {
	r, m := sqlRepo(t)
	now := time.Now().UTC()
	owner := "user-1"
	cfg := service.TableConfig{Seats: []service.TableSeat{{Seat: 0}}, Button: 0, SmallBlind: 5, BigBlind: 10, MaxHands: 1, Mode: "mock"}
	tb := service.Table{
		ID:          "11111111-1111-4111-8111-111111111111",
		OwnerUserID: &owner,
		Name:        "t",
		Revision:    1,
		Config:      &cfg,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	m.ExpectExec("INSERT INTO table_templates").WillReturnResult(sqlmock.NewResult(0, 1))
	if err := r.CreateTable(context.Background(), tb); err != nil {
		t.Fatal(err)
	}
}

func TestGetTableRoundTrip(t *testing.T) {
	r, m := sqlRepo(t)
	now := time.Now().UTC()
	m.ExpectQuery("FROM table_templates WHERE id=").WillReturnRows(
		sqlmock.NewRows(tableCols).AddRow("id-1", nil, "t", uint64(1), nil, now, now))
	tb, err := r.GetTable(context.Background(), "id-1")
	if err != nil {
		t.Fatal(err)
	}
	if tb.Name != "t" || tb.Config != nil || tb.OwnerUserID != nil || tb.Revision != 1 {
		t.Fatalf("get mismatch: %+v", tb)
	}
	// 不存在
	m.ExpectQuery("FROM table_templates WHERE id=").WillReturnError(sql.ErrNoRows)
	if _, err = r.GetTable(context.Background(), "missing"); !errors.Is(err, service.ErrNotFound) {
		t.Fatalf("missing should be not found: %v", err)
	}
}

func TestUpdateTableRevisionCAS(t *testing.T) {
	r, m := sqlRepo(t)
	now := time.Now().UTC()
	updated := service.Table{ID: "id-1", Name: "t2", Revision: 2}
	// 成功：UPDATE 影响 1 行，随后回读
	m.ExpectExec("UPDATE table_templates SET name=").WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectQuery("FROM table_templates WHERE id=").WillReturnRows(
		sqlmock.NewRows(tableCols).AddRow("id-1", nil, "t2", uint64(2), nil, now, now))
	tb, err := r.UpdateTable(context.Background(), 1, updated)
	if err != nil || tb.Revision != 2 || tb.Name != "t2" {
		t.Fatalf("update mismatch: %+v %v", tb, err)
	}
	// 冲突：0 行 + 存在 -> ErrConflict
	m.ExpectExec("UPDATE table_templates SET name=").WillReturnResult(sqlmock.NewResult(0, 0))
	m.ExpectQuery("SELECT COUNT").WillReturnRows(sqlmock.NewRows([]string{"c"}).AddRow(1))
	if _, err = r.UpdateTable(context.Background(), 1, updated); !errors.Is(err, service.ErrConflict) {
		t.Fatalf("stale update should conflict: %v", err)
	}
	// 不存在：0 行 + 0 -> ErrNotFound
	m.ExpectExec("UPDATE table_templates SET name=").WillReturnResult(sqlmock.NewResult(0, 0))
	m.ExpectQuery("SELECT COUNT").WillReturnRows(sqlmock.NewRows([]string{"c"}).AddRow(0))
	if _, err = r.UpdateTable(context.Background(), 1, updated); !errors.Is(err, service.ErrNotFound) {
		t.Fatalf("absent update should be not found: %v", err)
	}
}

func TestClearTableSetsNullConfig(t *testing.T) {
	r, m := sqlRepo(t)
	now := time.Now().UTC()
	m.ExpectExec("UPDATE table_templates SET config=NULL").WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectQuery("FROM table_templates WHERE id=").WillReturnRows(
		sqlmock.NewRows(tableCols).AddRow("id-1", nil, "t", uint64(2), nil, now, now))
	tb, err := r.ClearTable(context.Background(), 1, "id-1")
	if err != nil || tb.Config != nil || tb.Revision != 2 {
		t.Fatalf("clear mismatch: %+v %v", tb, err)
	}
}

func TestDeleteTableCAS(t *testing.T) {
	r, m := sqlRepo(t)
	m.ExpectExec("DELETE FROM table_templates").WillReturnResult(sqlmock.NewResult(0, 1))
	if err := r.DeleteTable(context.Background(), 1, "id-1"); err != nil {
		t.Fatal(err)
	}
	m.ExpectExec("DELETE FROM table_templates").WillReturnResult(sqlmock.NewResult(0, 0))
	m.ExpectQuery("SELECT COUNT").WillReturnRows(sqlmock.NewRows([]string{"c"}).AddRow(1))
	if err := r.DeleteTable(context.Background(), 1, "id-1"); !errors.Is(err, service.ErrConflict) {
		t.Fatalf("stale delete should conflict: %v", err)
	}
}

func TestListTablesPagination(t *testing.T) {
	r, m := sqlRepo(t)
	base := time.Now().UTC()
	rows := sqlmock.NewRows(tableCols)
	for i := 0; i < 3; i++ {
		rows.AddRow("id-"+string(rune('a'+i)), nil, "t", uint64(1), tableConfigBytes(t), base.Add(-time.Duration(i)*time.Second), base.Add(-time.Duration(i)*time.Second))
	}
	// limit=2，仓库查询 limit+1=3 行 -> 截断为 2 并给 next cursor
	m.ExpectQuery("FROM table_templates ORDER BY").WillReturnRows(rows)
	items, next, err := r.ListTables(context.Background(), "", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || next == "" {
		t.Fatalf("pagination mismatch: items=%d next=%q", len(items), next)
	}
	if items[0].Config == nil {
		t.Fatal("config not decoded")
	}
	// 无更多：只返回 limit 行
	rows2 := sqlmock.NewRows(tableCols).AddRow("id-z", nil, "t", uint64(1), nil, base, base)
	m.ExpectQuery("FROM table_templates ORDER BY").WillReturnRows(rows2)
	items, next, err = r.ListTables(context.Background(), "", 2)
	if err != nil || len(items) != 1 || next != "" {
		t.Fatalf("last page mismatch: %d %q %v", len(items), next, err)
	}
}
