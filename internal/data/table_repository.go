package data

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"texas-holdem/internal/service"
	"time"
)

// 模板存储直接读写 MySQL（权威），不加 Redis 缓存。
const tableColumns = "id,owner_user_id,name,revision,config,created_at,updated_at"

func marshalTableConfig(c *service.TableConfig) (any, error) {
	if c == nil {
		return nil, nil
	}
	b, err := json.Marshal(c)
	if err != nil {
		return nil, err
	}
	return b, nil
}

func scanTable(scanner interface{ Scan(...any) error }) (service.Table, error) {
	var (
		id, name  string
		owner     sql.NullString
		revision  uint64
		config    []byte
		createdAt time.Time
		updatedAt time.Time
	)
	if err := scanner.Scan(&id, &owner, &name, &revision, &config, &createdAt, &updatedAt); err != nil {
		return service.Table{}, err
	}
	t := service.Table{ID: id, Name: name, Revision: revision, CreatedAt: createdAt.UTC(), UpdatedAt: updatedAt.UTC()}
	if owner.Valid {
		v := owner.String
		t.OwnerUserID = &v
	}
	if len(config) > 0 {
		var c service.TableConfig
		if err := json.Unmarshal(config, &c); err != nil {
			return service.Table{}, service.ErrStorage
		}
		t.Config = &c
	}
	return t, nil
}

// tableMutationMiss 在 CAS 更新/删除影响 0 行时区分「不存在」与「版本冲突」。
func (r *Repository) tableMutationMiss(ctx context.Context, id string) error {
	var exists int
	if err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM table_templates WHERE id=?", id).Scan(&exists); err != nil {
		return readError(err)
	}
	if exists == 0 {
		return service.ErrNotFound
	}
	return service.ErrConflict
}

func (r *Repository) CreateTable(ctx context.Context, t service.Table) error {
	if err := r.guard(ctx); err != nil {
		return err
	}
	config, err := marshalTableConfig(t.Config)
	if err != nil {
		return service.ErrStorage
	}
	_, err = r.db.ExecContext(ctx, "INSERT INTO table_templates ("+tableColumns+") VALUES (?,?,?,?,?,?,?)", t.ID, nullString(t.OwnerUserID), t.Name, t.Revision, config, t.CreatedAt, t.UpdatedAt)
	if err != nil {
		return dbError(err)
	}
	return nil
}

func (r *Repository) GetTable(ctx context.Context, id string) (service.Table, error) {
	if err := r.guard(ctx); err != nil {
		return service.Table{}, err
	}
	t, err := scanTable(r.db.QueryRowContext(ctx, "SELECT "+tableColumns+" FROM table_templates WHERE id=?", id))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return service.Table{}, service.ErrNotFound
		}
		return service.Table{}, readError(err)
	}
	return t, nil
}

func (r *Repository) ListTables(ctx context.Context, cursor string, limit int) ([]service.Table, string, error) {
	if err := r.guard(ctx); err != nil {
		return nil, "", err
	}
	query := "SELECT " + tableColumns + " FROM table_templates"
	var args []any
	if cursor != "" {
		updatedAt, id, ok := parseTableCursor(cursor)
		if !ok {
			return nil, "", service.ErrInvalid
		}
		query += " WHERE (updated_at < ?) OR (updated_at = ? AND id < ?)"
		args = append(args, updatedAt, updatedAt, id)
	}
	query += " ORDER BY updated_at DESC, id DESC LIMIT ?"
	args = append(args, limit+1)
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, "", readError(err)
	}
	defer rows.Close()
	items := make([]service.Table, 0, limit)
	for rows.Next() {
		t, err := scanTable(rows)
		if err != nil {
			return nil, "", readError(err)
		}
		items = append(items, t)
	}
	if err := rows.Err(); err != nil {
		return nil, "", readError(err)
	}
	next := ""
	if len(items) > limit {
		items = items[:limit]
		next = formatTableCursor(items[limit-1].UpdatedAt, items[limit-1].ID)
	}
	return items, next, nil
}

func (r *Repository) UpdateTable(ctx context.Context, expectedRevision uint64, t service.Table) (service.Table, error) {
	if err := r.guard(ctx); err != nil {
		return service.Table{}, err
	}
	config, err := marshalTableConfig(t.Config)
	if err != nil {
		return service.Table{}, service.ErrStorage
	}
	res, err := r.db.ExecContext(ctx, "UPDATE table_templates SET name=?, config=?, revision=revision+1, updated_at=? WHERE id=? AND revision=?", t.Name, config, time.Now().UTC(), t.ID, expectedRevision)
	if err != nil {
		return service.Table{}, dbError(err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return service.Table{}, service.ErrStorage
	}
	if n != 1 {
		return service.Table{}, r.tableMutationMiss(ctx, t.ID)
	}
	return r.GetTable(ctx, t.ID)
}

func (r *Repository) ClearTable(ctx context.Context, expectedRevision uint64, id string) (service.Table, error) {
	if err := r.guard(ctx); err != nil {
		return service.Table{}, err
	}
	res, err := r.db.ExecContext(ctx, "UPDATE table_templates SET config=NULL, revision=revision+1, updated_at=? WHERE id=? AND revision=?", time.Now().UTC(), id, expectedRevision)
	if err != nil {
		return service.Table{}, dbError(err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return service.Table{}, service.ErrStorage
	}
	if n != 1 {
		return service.Table{}, r.tableMutationMiss(ctx, id)
	}
	return r.GetTable(ctx, id)
}

func (r *Repository) DeleteTable(ctx context.Context, expectedRevision uint64, id string) error {
	if err := r.guard(ctx); err != nil {
		return err
	}
	res, err := r.db.ExecContext(ctx, "DELETE FROM table_templates WHERE id=? AND revision=?", id, expectedRevision)
	if err != nil {
		return dbError(err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return service.ErrStorage
	}
	if n != 1 {
		return r.tableMutationMiss(ctx, id)
	}
	return nil
}

// formatTableCursor 编码 (updated_at 微秒, id)，作为 keyset 分页游标；不含任何用户信息。
func formatTableCursor(updatedAt time.Time, id string) string {
	return strconv.FormatInt(updatedAt.UTC().UnixMicro(), 10) + ":" + id
}

func parseTableCursor(cursor string) (time.Time, string, bool) {
	raw, id, ok := strings.Cut(cursor, ":")
	if !ok || id == "" {
		return time.Time{}, "", false
	}
	micro, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return time.Time{}, "", false
	}
	return time.UnixMicro(micro).UTC(), id, true
}
