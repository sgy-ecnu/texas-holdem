package server

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"texas-holdem/internal/service"
)

// tableRepoStub 是 server 层单测用的内存模板仓库。
type tableRepoStub struct {
	mu     sync.Mutex
	tables map[string]service.Table
}

func newTableRepoStub() *tableRepoStub { return &tableRepoStub{tables: map[string]service.Table{}} }

func (r *tableRepoStub) CreateTable(_ context.Context, t service.Table) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tables[t.ID] = t
	return nil
}
func (r *tableRepoStub) GetTable(_ context.Context, id string) (service.Table, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	t, ok := r.tables[id]
	if !ok {
		return service.Table{}, service.ErrNotFound
	}
	return t, nil
}
func (r *tableRepoStub) ListTables(_ context.Context, _ string, limit int) ([]service.Table, string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	all := make([]service.Table, 0, len(r.tables))
	for _, t := range r.tables {
		all = append(all, t)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].UpdatedAt.After(all[j].UpdatedAt) })
	if len(all) > limit {
		all = all[:limit]
	}
	return all, "", nil
}
func (r *tableRepoStub) UpdateTable(_ context.Context, expected uint64, t service.Table) (service.Table, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	cur, ok := r.tables[t.ID]
	if !ok {
		return service.Table{}, service.ErrNotFound
	}
	if cur.Revision != expected {
		return service.Table{}, service.ErrConflict
	}
	cur.Name, cur.Config, cur.Revision, cur.UpdatedAt = t.Name, t.Config, expected+1, time.Now().UTC()
	r.tables[t.ID] = cur
	return cur, nil
}
func (r *tableRepoStub) ClearTable(_ context.Context, expected uint64, id string) (service.Table, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	cur, ok := r.tables[id]
	if !ok {
		return service.Table{}, service.ErrNotFound
	}
	if cur.Revision != expected {
		return service.Table{}, service.ErrConflict
	}
	cur.Config, cur.Revision, cur.UpdatedAt = nil, expected+1, time.Now().UTC()
	r.tables[id] = cur
	return cur, nil
}
func (r *tableRepoStub) DeleteTable(_ context.Context, expected uint64, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	cur, ok := r.tables[id]
	if !ok {
		return service.ErrNotFound
	}
	if cur.Revision != expected {
		return service.ErrConflict
	}
	delete(r.tables, id)
	return nil
}

func tableHandler(t *testing.T, m service.Manager, opts ...Options) http.Handler {
	t.Helper()
	return HandlerWithTables(m, service.NewTableService(newTableRepoStub()), nil, opts...)
}

const aiTemplateJSON = `{"name":"t","config":{"seats":[{"seat":0,"name":"A","stack":1000,"model_ref":"mock"},{"seat":1,"name":"B","stack":1000,"model_ref":"mock"}],"button":0,"small_blind":5,"big_blind":10,"max_hands":3,"max_live_calls":0}}`
const humanTemplateJSON = `{"name":"h","config":{"seats":[{"seat":0,"name":"H","stack":1000,"kind":"human"},{"seat":1,"name":"A","stack":1000,"model_ref":"mock"}],"button":0,"small_blind":5,"big_blind":10,"max_hands":3,"max_live_calls":0}}`

func createTable(t *testing.T, h http.Handler, body string) TableDTO {
	t.Helper()
	w := request(h, "POST", "/api/v1/tables", body, nil)
	if w.Code != http.StatusCreated {
		t.Fatalf("create status got %d body=%s", w.Code, w.Body.String())
	}
	var d TableDTO
	if err := json.Unmarshal(w.Body.Bytes(), &d); err != nil {
		t.Fatal(err)
	}
	return d
}

func TestTableCRUDHTTP(t *testing.T) {
	m := &managerStub{}
	h := tableHandler(t, m)
	created := createTable(t, h, aiTemplateJSON)
	if created.Revision != 1 || created.Config == nil || len(created.Config.Seats) != 2 {
		t.Fatalf("create mismatch: %+v", created)
	}
	if strings.Contains(w0(created), "owner") {
		t.Fatal("owner leaked")
	}

	w := request(h, "GET", "/api/v1/tables", "", nil)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"tables":[`) || strings.Contains(w.Body.String(), "owner_user_id") {
		t.Fatalf("list mismatch: %d %s", w.Code, w.Body.String())
	}
	w = request(h, "GET", "/api/v1/tables/"+created.ID, "", nil)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), created.ID) {
		t.Fatalf("get mismatch: %d", w.Code)
	}

	// 冲突版本 PUT -> 409
	putBody := `{"expected_revision":99,"name":"t2","config":` + aiConfigOnly + `}`
	if w = request(h, "PUT", "/api/v1/tables/"+created.ID, putBody, nil); w.Code != http.StatusConflict {
		t.Fatalf("stale put status got %d", w.Code)
	}
	putBody = `{"expected_revision":1,"name":"t2","config":` + aiConfigOnly + `}`
	if w = request(h, "PUT", "/api/v1/tables/"+created.ID, putBody, nil); w.Code != http.StatusOK {
		t.Fatalf("put status got %d body=%s", w.Code, w.Body.String())
	}
	var updated TableDTO
	_ = json.Unmarshal(w.Body.Bytes(), &updated)
	if updated.Revision != 2 || updated.Name != "t2" {
		t.Fatalf("put mismatch: %+v", updated)
	}

	// 清空 -> config null, revision 3
	if w = request(h, "POST", "/api/v1/tables/"+created.ID+"/clear", `{"expected_revision":2}`, nil); w.Code != http.StatusOK {
		t.Fatalf("clear status got %d body=%s", w.Code, w.Body.String())
	}
	var cleared TableDTO
	_ = json.Unmarshal(w.Body.Bytes(), &cleared)
	if cleared.Config != nil || cleared.Revision != 3 {
		t.Fatalf("clear mismatch: %+v", cleared)
	}

	// 已清空不可开局
	if w = request(h, "POST", "/api/v1/tables/"+created.ID+"/games", `{"expected_revision":3}`, nil); w.Code != http.StatusBadRequest {
		t.Fatalf("open cleared status got %d", w.Code)
	}

	// 删除：错版本 409，正确 204，随后 404
	if w = request(h, "DELETE", "/api/v1/tables/"+created.ID, `{"expected_revision":1}`, nil); w.Code != http.StatusConflict {
		t.Fatalf("stale delete status got %d", w.Code)
	}
	if w = request(h, "DELETE", "/api/v1/tables/"+created.ID, `{"expected_revision":3}`, nil); w.Code != http.StatusNoContent {
		t.Fatalf("delete status got %d", w.Code)
	}
	if w = request(h, "GET", "/api/v1/tables/"+created.ID, "", nil); w.Code != http.StatusNotFound {
		t.Fatalf("get deleted status got %d", w.Code)
	}
}

const aiConfigOnly = `{"seats":[{"seat":0,"name":"A","stack":1000,"model_ref":"mock"},{"seat":1,"name":"B","stack":1000,"model_ref":"mock"}],"button":0,"small_blind":5,"big_blind":10,"max_hands":3,"max_live_calls":0}`

func w0(d TableDTO) string { b, _ := json.Marshal(d); return string(b) }

func TestTableCreateEmptyAndRejectUnknownUser(t *testing.T) {
	h := tableHandler(t, &managerStub{})
	w := request(h, "POST", "/api/v1/tables", `{"name":"empty"}`, nil)
	if w.Code != http.StatusCreated || !strings.Contains(w.Body.String(), `"config":null`) {
		t.Fatalf("empty config mismatch: %d %s", w.Code, w.Body.String())
	}
	// 座位带 user_id -> 严格解码拒绝
	userBody := `{"name":"x","config":{"seats":[{"seat":0,"name":"A","stack":1000,"model_ref":"mock","user_id":"u"},{"seat":1,"name":"B","stack":1000,"model_ref":"mock"}],"button":0,"small_blind":5,"big_blind":10,"max_hands":3,"max_live_calls":0}}`
	if w = request(h, "POST", "/api/v1/tables", userBody, nil); w.Code != http.StatusBadRequest {
		t.Fatalf("seat user_id accepted: %d", w.Code)
	}
}

func TestTableOpenFromTemplate(t *testing.T) {
	m := &managerStub{view: service.GameView{ID: gameID, Status: "created"}}
	h := tableHandler(t, m)
	created := createTable(t, h, aiTemplateJSON)
	w := request(h, "POST", "/api/v1/tables/"+created.ID+"/games", `{"expected_revision":1}`, nil)
	if w.Code != http.StatusCreated {
		t.Fatalf("open status got %d body=%s", w.Code, w.Body.String())
	}
	if m.creates != 1 || m.input.TemplateID == nil || *m.input.TemplateID != created.ID {
		t.Fatalf("template id not recorded: %+v", m.input)
	}
	if m.input.TemplateRevision == nil || *m.input.TemplateRevision != 1 {
		t.Fatalf("template revision not recorded: %+v", m.input)
	}
	if len(m.input.Seats) != 2 || m.input.PlayerSessionHash != nil || m.input.OwnerUserID != nil {
		t.Fatalf("open input mismatch: %+v", m.input)
	}
}

func TestTableOpenRevisionMismatch(t *testing.T) {
	m := &managerStub{view: service.GameView{ID: gameID, Status: "created"}}
	h := tableHandler(t, m)
	created := createTable(t, h, aiTemplateJSON)
	w := request(h, "POST", "/api/v1/tables/"+created.ID+"/games", `{"expected_revision":99}`, nil)
	if w.Code != http.StatusConflict || m.creates != 0 {
		t.Fatalf("revision mismatch not blocked: %d creates=%d", w.Code, m.creates)
	}
}

func TestTableOpenSingleActiveConflict(t *testing.T) {
	m := &managerStub{err: service.ErrConflict}
	h := tableHandler(t, m)
	created := createTable(t, h, aiTemplateJSON)
	w := request(h, "POST", "/api/v1/tables/"+created.ID+"/games", `{"expected_revision":1}`, nil)
	if w.Code != http.StatusConflict {
		t.Fatalf("single-active conflict status got %d", w.Code)
	}
}

func TestTableOpenHumanSetsSessionCookie(t *testing.T) {
	m := &managerStub{view: service.GameView{ID: gameID, Status: "created"}}
	h := tableHandler(t, m)
	created := createTable(t, h, humanTemplateJSON)
	w := request(h, "POST", "/api/v1/tables/"+created.ID+"/games", `{"expected_revision":1}`, nil)
	if w.Code != http.StatusCreated {
		t.Fatalf("human open status got %d body=%s", w.Code, w.Body.String())
	}
	cookies := w.Result().Cookies()
	found := false
	for _, c := range cookies {
		if c.Name == playerCookieName && c.HttpOnly && c.Value != "" {
			found = true
			if strings.Contains(w.Body.String(), c.Value) {
				t.Fatal("session credential leaked into response")
			}
		}
	}
	if !found {
		t.Fatal("human session cookie missing")
	}
	if m.input.PlayerSessionHash == nil || *m.input.PlayerSessionHash == (service.PlayerSessionHash{}) {
		t.Fatal("human session hash not injected")
	}
}

func TestTableOpenHumanLocalOnly(t *testing.T) {
	m := &managerStub{view: service.GameView{ID: gameID, Status: "created"}}
	h := tableHandler(t, m, Options{Host: "0.0.0.0", Port: 8081})
	// 非 loopback 监听下，请求 Host 必须匹配配置端口，才能通过来源校验进入处理器。
	headers := map[string]string{"Host": "0.0.0.0:8081"}
	w := request(h, "POST", "/api/v1/tables", humanTemplateJSON, headers)
	if w.Code != http.StatusCreated {
		t.Fatalf("create status got %d body=%s", w.Code, w.Body.String())
	}
	var created TableDTO
	_ = json.Unmarshal(w.Body.Bytes(), &created)
	w = request(h, "POST", "/api/v1/tables/"+created.ID+"/games", `{"expected_revision":1}`, headers)
	if w.Code != http.StatusForbidden || m.creates != 0 {
		t.Fatalf("non-loopback human open allowed: %d", w.Code)
	}
}
