package data

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strings"
	"testing"
	"texas-holdem/internal/conf"
	"texas-holdem/internal/service"
	"time"
)

// TestMigrateIntegration 需要真实 MySQL。设置 POKER_TEST_MYSQL_DSN（无凭据的本地
// 测试库）后运行；覆盖新库安装、重复迁移、旧库升级、部分失败重跑与单活约束。
func TestMigrateIntegration(t *testing.T) {
	dsn := os.Getenv("POKER_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("POKER_TEST_MYSQL_DSN not set")
	}
	ctx := context.Background()
	c := conf.Config{MySQLDSN: dsn}
	db, err := openMySQL(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.PingContext(ctx); err != nil {
		t.Fatal(err)
	}
	dropAll := func() {
		for _, tb := range []string{"actions", "hands", "table_templates", "games", "schema_version"} {
			if _, err := db.ExecContext(ctx, "DROP TABLE IF EXISTS "+tb); err != nil {
				t.Fatalf("drop %s: %v", tb, err)
			}
		}
	}

	t.Run("fresh_install_and_repeat", func(t *testing.T) {
		dropAll()
		if err := Migrate(ctx, c); err != nil {
			t.Fatalf("first migrate: %v", err)
		}
		if err := Migrate(ctx, c); err != nil {
			t.Fatalf("repeat migrate: %v", err)
		}
		var count int
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_version WHERE version=?", currentSchemaVersion).Scan(&count); err != nil || count != 1 {
			t.Fatalf("version row: %d %v", count, err)
		}
		if err := validateSchema(ctx, db); err != nil {
			t.Fatalf("validate after migrate: %v", err)
		}
		for _, col := range []string{"owner_user_id", "template_id", "template_revision"} {
			var n int
			if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='games' AND COLUMN_NAME=?", col).Scan(&n); err != nil || n != 1 {
				t.Fatalf("games.%s missing: %d %v", col, n, err)
			}
		}
		// 单活唯一索引：第二个非终态局必须被拒绝。
		if _, err := db.ExecContext(ctx, "INSERT INTO games (id,revision,status,active_slot,record) VALUES ('aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa',1,'created',1,0x7b7d)"); err != nil {
			t.Fatalf("first active insert: %v", err)
		}
		if _, err := db.ExecContext(ctx, "INSERT INTO games (id,revision,status,active_slot,record) VALUES ('bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb',1,'created',1,0x7b7d)"); err == nil {
			t.Fatal("second active game accepted")
		}
	})

	t.Run("old_install_upgrade", func(t *testing.T) {
		dropAll()
		applyVersion(t, ctx, db, 1)
		if err := validateSchema(ctx, db); err == nil {
			t.Fatal("v1 should not satisfy v2 schema")
		}
		if err := Migrate(ctx, c); err != nil {
			t.Fatalf("upgrade migrate: %v", err)
		}
		if err := validateSchema(ctx, db); err != nil {
			t.Fatalf("validate after upgrade: %v", err)
		}
	})

	t.Run("partial_failure_rerun", func(t *testing.T) {
		dropAll()
		applyVersion(t, ctx, db, 1)
		// 只建表、不改 games、不记录版本，模拟 002 中途失败。
		if _, err := db.ExecContext(ctx, "CREATE TABLE IF NOT EXISTS table_templates (id CHAR(36) NOT NULL PRIMARY KEY, owner_user_id CHAR(36) NULL, name VARCHAR(128) NOT NULL, revision BIGINT UNSIGNED NOT NULL, config JSON NULL, created_at DATETIME(6) NOT NULL, updated_at DATETIME(6) NOT NULL, KEY templates_updated (updated_at, id)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4"); err != nil {
			t.Fatalf("partial create: %v", err)
		}
		if err := Migrate(ctx, c); err != nil {
			t.Fatalf("rerun after partial: %v", err)
		}
		if err := validateSchema(ctx, db); err != nil {
			t.Fatalf("validate after partial rerun: %v", err)
		}
	})
}

// TestTableRepositoryIntegration 在真实 MySQL 上验证模板仓库 CRUD、CAS 与 keyset 分页。
func TestTableRepositoryIntegration(t *testing.T) {
	dsn := os.Getenv("POKER_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("POKER_TEST_MYSQL_DSN not set")
	}
	ctx := context.Background()
	c := conf.Config{MySQLDSN: dsn}
	if err := Migrate(ctx, c); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	db, err := openMySQL(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, "DELETE FROM table_templates"); err != nil {
		t.Fatal(err)
	}
	r := &Repository{db: db}
	now := time.Now().UTC().Truncate(time.Microsecond)
	cfg := service.TableConfig{Seats: []service.TableSeat{{Seat: 0, Name: "A", Stack: 1000, ModelRef: "mock", Kind: service.SeatAI}, {Seat: 1, Name: "B", Stack: 1000, ModelRef: "mock", Kind: service.SeatAI}}, Button: 0, SmallBlind: 5, BigBlind: 10, MaxHands: 3, Mode: "mock"}
	id := "11111111-1111-4111-8111-111111111111"
	owner := "user-1"
	if err := r.CreateTable(ctx, service.Table{ID: id, OwnerUserID: &owner, Name: "t", Revision: 1, Config: &cfg, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := r.GetTable(ctx, id)
	if err != nil || got.Name != "t" || got.Config == nil || len(got.Config.Seats) != 2 || got.OwnerUserID == nil || *got.OwnerUserID != owner {
		t.Fatalf("get mismatch: %+v %v", got, err)
	}
	if _, err := r.UpdateTable(ctx, 99, service.Table{ID: id, Name: "x", Config: &cfg}); !errors.Is(err, service.ErrConflict) {
		t.Fatalf("stale update should conflict: %v", err)
	}
	upd, err := r.UpdateTable(ctx, 1, service.Table{ID: id, Name: "t2", Config: &cfg})
	if err != nil || upd.Revision != 2 || upd.Name != "t2" {
		t.Fatalf("update mismatch: %+v %v", upd, err)
	}
	cleared, err := r.ClearTable(ctx, 2, id)
	if err != nil || cleared.Config != nil || cleared.Revision != 3 {
		t.Fatalf("clear mismatch: %+v %v", cleared, err)
	}
	// keyset 分页：再插两条，limit=2 首页 + cursor 次页。
	for i, tid := range []string{"22222222-2222-4222-8222-222222222222", "33333333-3333-4333-8333-333333333333"} {
		ts := now.Add(time.Duration(i+1) * time.Second)
		if err := r.CreateTable(ctx, service.Table{ID: tid, Name: tid, Revision: 1, CreatedAt: ts, UpdatedAt: ts}); err != nil {
			t.Fatalf("create %s: %v", tid, err)
		}
	}
	page1, next, err := r.ListTables(ctx, "", 2)
	if err != nil || len(page1) != 2 || next == "" {
		t.Fatalf("page1 mismatch: %d %q %v", len(page1), next, err)
	}
	page2, _, err := r.ListTables(ctx, next, 2)
	if err != nil || len(page2) != 1 {
		t.Fatalf("page2 mismatch: %d %v", len(page2), err)
	}
	if err := r.DeleteTable(ctx, 3, id); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := r.DeleteTable(ctx, 1, id); !errors.Is(err, service.ErrNotFound) {
		t.Fatalf("delete missing should be not found: %v", err)
	}
}

// applyVersion 在测试库中只应用指定版本的迁移语句，用于模拟旧库状态。
func applyVersion(t *testing.T, ctx context.Context, db *sql.DB, version int) {
	t.Helper()
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range migrations {
		if m.version != version {
			continue
		}
		for _, stmt := range strings.Split(m.sql, ";") {
			if strings.TrimSpace(stmt) == "" {
				continue
			}
			if _, err := db.ExecContext(ctx, stmt); err != nil {
				t.Fatalf("apply v%d stmt: %v", version, err)
			}
		}
		return
	}
	t.Fatalf("migration %d not found", version)
}
