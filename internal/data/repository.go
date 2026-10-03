// Package data 的 MySQL 是唯一权威。Redis 内容仅为公共 DTO，读取永远回源 MySQL。
package data

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/go-sql-driver/mysql"
	"github.com/redis/go-redis/v9"
	"os"
	"strings"
	"sync/atomic"
	"texas-holdem/internal/conf"
	"texas-holdem/internal/service"
	"time"
)

//go:embed migrations/001_init.sql
var migration string

type mysqlLog struct{}

func (mysqlLog) Print(...any) { fmt.Fprintln(os.Stderr, "MYSQL_DRIVER_ERROR") }

type redisLog struct{}

func (redisLog) Printf(context.Context, string, ...any) {
	fmt.Fprintln(os.Stderr, "REDIS_DRIVER_ERROR")
}

func init() { _ = mysql.SetLogger(mysqlLog{}); redis.SetLogger(redisLog{}) }

type publicCache interface {
	Ping(context.Context) error
	Set(context.Context, string, []byte, time.Duration) error
	Delete(context.Context, string) error
}
type redisCache struct{ client *redis.Client }

func (r redisCache) Ping(ctx context.Context) error { return r.client.Ping(ctx).Err() }
func (r redisCache) Set(ctx context.Context, k string, b []byte, ttl time.Duration) error {
	return r.client.Set(ctx, k, b, ttl).Err()
}
func (r redisCache) Delete(ctx context.Context, k string) error { return r.client.Del(ctx, k).Err() }

type Repository struct {
	db       *sql.DB
	lock     *sql.Conn
	redis    *redis.Client
	cache    publicCache
	encode   func(service.GameView) ([]byte, error)
	degraded atomic.Bool
}

func openMySQL(dsn string) (*sql.DB, error) {
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		return nil, service.ErrStorage
	}
	cfg.MultiStatements = false
	cfg.Timeout = 5 * time.Second
	cfg.ReadTimeout = 5 * time.Second
	cfg.WriteTimeout = 5 * time.Second
	connector, err := mysql.NewConnector(cfg)
	if err != nil {
		return nil, service.ErrStorage
	}
	db := sql.OpenDB(connector)
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(4)
	db.SetConnMaxLifetime(5 * time.Minute)
	return db, nil
}
func Open(ctx context.Context, c conf.Config, encode func(service.GameView) ([]byte, error)) (*Repository, error) {
	db, err := openMySQL(c.MySQLDSN)
	if err != nil {
		return nil, service.ErrStorage
	}
	r := &Repository{db: db, encode: encode}
	ok := false
	defer func() {
		if !ok {
			_ = r.Close()
		}
	}()
	if err = db.PingContext(ctx); err != nil {
		return nil, service.ErrStorage
	}
	r.lock, err = db.Conn(ctx)
	if err != nil {
		return nil, service.ErrStorage
	}
	var acquired sql.NullInt64
	if err = r.lock.QueryRowContext(ctx, "SELECT GET_LOCK('texas-holdem:scheduler', 0)").Scan(&acquired); err != nil || !acquired.Valid || acquired.Int64 != 1 {
		return nil, errors.New("INSTANCE_LOCK_UNAVAILABLE")
	}
	if err = validateSchema(ctx, db); err != nil {
		return nil, errors.New("SCHEMA_INVALID")
	}
	r.redis = redis.NewClient(&redis.Options{Addr: c.RedisAddress, Password: c.RedisPassword, DB: c.RedisDB, DialTimeout: 5 * time.Second, ReadTimeout: 3 * time.Second, WriteTimeout: 3 * time.Second, MaxRetries: -1})
	r.cache = redisCache{r.redis}
	if err = r.cache.Ping(ctx); err != nil {
		return nil, service.ErrStorage
	}
	if encode == nil {
		return nil, errors.New("PUBLIC_ENCODER_REQUIRED")
	}
	ok = true
	return r, nil
}

func validateSchema(ctx context.Context, db *sql.DB) error {
	var version int
	if err := db.QueryRowContext(ctx, "SELECT MAX(version) FROM schema_version").Scan(&version); err != nil || version != 1 {
		return service.ErrStorage
	}
	for _, q := range []string{"SELECT id,revision,status,active_slot,record FROM games LIMIT 0", "SELECT game_id,sequence,hand_no,record FROM actions LIMIT 0", "SELECT game_id,hand_no,record FROM hands LIMIT 0"} {
		rows, err := db.QueryContext(ctx, q)
		if err != nil {
			return service.ErrStorage
		}
		if err = rows.Close(); err != nil {
			return service.ErrStorage
		}
	}
	var tables int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.TABLES WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME IN ('games','hands','actions','schema_version') AND ENGINE='InnoDB'").Scan(&tables); err != nil || tables != 4 {
		return service.ErrStorage
	}
	var indexes int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.STATISTICS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='games' AND INDEX_NAME='one_active_game' AND NON_UNIQUE=0 AND COLUMN_NAME='active_slot'").Scan(&indexes); err != nil || indexes != 1 {
		return service.ErrStorage
	}
	return nil
}

// Migrate 必须由显式命令调用；DDL 不能承诺 MySQL 事务回滚，失败后可安全重跑。
func Migrate(ctx context.Context, c conf.Config) error {
	db, err := openMySQL(c.MySQLDSN)
	if err != nil {
		return service.ErrStorage
	}
	defer db.Close()
	if err = db.PingContext(ctx); err != nil {
		return service.ErrStorage
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		return service.ErrStorage
	}
	defer conn.Close()
	var acquired sql.NullInt64
	if err = conn.QueryRowContext(ctx, "SELECT GET_LOCK('texas-holdem:scheduler', 0)").Scan(&acquired); err != nil || !acquired.Valid || acquired.Int64 != 1 {
		return errors.New("INSTANCE_LOCK_UNAVAILABLE")
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		var released sql.NullInt64
		if err := conn.QueryRowContext(cleanup, "SELECT RELEASE_LOCK('texas-holdem:scheduler')").Scan(&released); err != nil {
			fmt.Fprintln(os.Stderr, "INSTANCE_LOCK_RELEASE_FAILED")
		}
	}()
	for _, stmt := range strings.Split(migration, ";") {
		if strings.TrimSpace(stmt) == "" {
			continue
		}
		if _, err = conn.ExecContext(ctx, stmt); err != nil {
			return errors.New("MIGRATION_FAILED")
		}
	}
	return nil
}

func (r *Repository) Close() error {
	var failed bool
	if r.lock != nil {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		var released sql.NullInt64
		if err := r.lock.QueryRowContext(ctx, "SELECT RELEASE_LOCK('texas-holdem:scheduler')").Scan(&released); err != nil {
			failed = true
		}
		cancel()
		if err := r.lock.Close(); err != nil {
			failed = true
		}
	}
	if r.redis != nil {
		if err := r.redis.Close(); err != nil {
			failed = true
		}
	}
	if r.db != nil {
		if err := r.db.Close(); err != nil {
			failed = true
		}
	}
	if failed {
		return service.ErrStorage
	}
	return nil
}
func (r *Repository) guard(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if r.lock != nil {
		// 查询锁归属而非单纯 Ping：专用连接断开或锁失效后禁止继续推进。
		// MySQL 驱动取消查询会关闭连接；轮询不得取消持有实例锁的连接。
		// 仍只查询一次，独立短超时失败属于真实锁健康不确定，必须 fail-closed。
		lockCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		var owned sql.NullInt64
		if err := r.lock.QueryRowContext(lockCtx, "SELECT IS_USED_LOCK('texas-holdem:scheduler') = CONNECTION_ID()").Scan(&owned); err != nil {
			return fmt.Errorf("%w: %w", service.ErrStorage, service.ErrInstanceLock)
		}
		if !owned.Valid || owned.Int64 != 1 {
			return fmt.Errorf("%w: %w", service.ErrStorage, service.ErrInstanceLock)
		}
	}
	return ctx.Err()
}
func active(status string) any {
	if status == "stopped" || status == "completed" || status == "failed" {
		return nil
	}
	return 1
}
func dbError(err error) error {
	var me *mysql.MySQLError
	if errors.As(err, &me) && me.Number == 1062 {
		return service.ErrConflict
	}
	return service.ErrStorage
}

// 只读错误保留取消原因。不能用 ctx.Err() 替代真实 SQL/锁错误；写入结果仍保守处理。
func readError(err error) error {
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	return service.ErrStorage
}

func (r *Repository) Create(ctx context.Context, record service.Record) error {
	if err := r.guard(ctx); err != nil {
		return err
	}
	b, err := json.Marshal(record)
	if err != nil {
		return service.ErrStorage
	}
	_, err = r.db.ExecContext(ctx, "INSERT INTO games (id,revision,status,active_slot,record) VALUES (?,?,?,?,?)", record.View.ID, record.View.Revision, record.View.Status, active(record.View.Status), b)
	if err != nil {
		return dbError(err)
	}
	r.publish(ctx, record.View)
	return nil
}
func (r *Repository) load(ctx context.Context, q string, args ...any) (service.Record, error) {
	if err := r.guard(ctx); err != nil {
		return service.Record{}, err
	}
	var b []byte
	if err := r.db.QueryRowContext(ctx, q, args...).Scan(&b); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return service.Record{}, service.ErrNotFound
		}
		return service.Record{}, readError(err)
	}
	var record service.Record
	if err := json.Unmarshal(b, &record); err != nil || record.View.ID == "" {
		return service.Record{}, service.ErrStorage
	}
	return record, nil
}
func (r *Repository) Get(ctx context.Context, id string) (service.Record, error) {
	return r.load(ctx, "SELECT record FROM games WHERE id=?", id)
}
func (r *Repository) Current(ctx context.Context) (service.Record, error) {
	return r.load(ctx, "SELECT record FROM games WHERE active_slot=1")
}

func (r *Repository) Commit(ctx context.Context, c service.Commit) error {
	if err := r.guard(ctx); err != nil {
		return err
	}
	if c.Record.View.Revision != c.ExpectedRevision+1 {
		return service.ErrConflict
	}
	b, err := json.Marshal(c.Record)
	if err != nil {
		return service.ErrStorage
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return service.ErrStorage
	}
	defer func() {
		err := tx.Rollback()
		if err != nil && !errors.Is(err, sql.ErrTxDone) {
			fmt.Fprintln(os.Stderr, "TRANSACTION_ROLLBACK_FAILED")
		}
	}()
	res, err := tx.ExecContext(ctx, "UPDATE games SET revision=?,status=?,active_slot=?,record=? WHERE id=? AND revision=?", c.Record.View.Revision, c.Record.View.Status, active(c.Record.View.Status), b, c.Record.View.ID, c.ExpectedRevision)
	if err != nil {
		return dbError(err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return service.ErrStorage
	}
	if n != 1 {
		return service.ErrConflict
	}
	if c.Action != nil {
		a, err := json.Marshal(c.Action)
		if err != nil {
			return service.ErrStorage
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO actions (game_id,sequence,hand_no,record) VALUES (?,?,?,?)", c.Record.View.ID, c.Action.Sequence, c.Action.HandNo, a); err != nil {
			return dbError(err)
		}
	}
	if c.Hand != nil {
		h, err := json.Marshal(c.Hand)
		if err != nil {
			return service.ErrStorage
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO hands (game_id,hand_no,record) VALUES (?,?,?)", c.Record.View.ID, c.Hand.HandNo, h); err != nil {
			return dbError(err)
		}
	}
	if err = tx.Commit(); err != nil {
		return service.ErrStorage
	}
	r.publish(ctx, c.Record.View)
	return nil
}
func (r *Repository) Hand(ctx context.Context, id string, n uint32) (service.CompletedHandView, error) {
	if err := r.guard(ctx); err != nil {
		return service.CompletedHandView{}, err
	}
	var b []byte
	if err := r.db.QueryRowContext(ctx, "SELECT record FROM hands WHERE game_id=? AND hand_no=?", id, n).Scan(&b); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return service.CompletedHandView{}, service.ErrNotFound
		}
		return service.CompletedHandView{}, readError(err)
	}
	var h service.CompletedHandView
	if err := json.Unmarshal(b, &h); err != nil {
		return h, service.ErrStorage
	}
	return h, nil
}
func (r *Repository) publish(ctx context.Context, v service.GameView) {
	if r.cache == nil || r.encode == nil {
		r.degraded.Store(true)
		return
	}
	v.StorageDegraded = false
	b, err := r.encode(v)
	if err == nil {
		err = r.cache.Set(ctx, "poker:game:"+v.ID+":public", b, 30*time.Second)
	}
	if err != nil {
		r.degraded.Store(true)
		cleanup, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := r.cache.Delete(cleanup, "poker:game:"+v.ID+":public"); err != nil {
			fmt.Fprintln(os.Stderr, "PUBLIC_CACHE_INVALIDATION_FAILED")
		}
		return
	}
	r.degraded.Store(false)
}
func (r *Repository) Health(ctx context.Context) (service.HealthView, error) {
	h := service.HealthView{MySQL: "available", Redis: "available", StorageDegraded: r.degraded.Load()}
	err := r.guard(ctx)
	if err == nil {
		if pingErr := r.db.PingContext(ctx); pingErr != nil {
			err = readError(pingErr)
		}
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return h, err
	}
	if err != nil {
		h.MySQL = "unavailable"
		h.StorageDegraded = true
	}
	cacheErr := service.ErrStorage
	if r.cache != nil {
		cacheErr = r.cache.Ping(ctx)
	}
	if errors.Is(cacheErr, context.Canceled) || errors.Is(cacheErr, context.DeadlineExceeded) {
		// 取消不能污染 Redis 降级状态，也不能掩盖此前已确认的 MySQL 故障。
		if err != nil {
			return h, service.ErrStorage
		}
		return h, readError(cacheErr)
	}
	if cacheErr != nil {
		h.Redis = "unavailable"
		h.StorageDegraded = true
		r.degraded.Store(true)
	}
	if h.MySQL != "available" || h.Redis != "available" {
		return h, service.ErrStorage
	}
	return h, nil
}
