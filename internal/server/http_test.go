package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"texas-holdem/internal/agent"
	"texas-holdem/internal/poker"
	"texas-holdem/internal/service"
	"time"
)

type managerStub struct {
	view      service.GameView
	err       error
	complete  bool
	creates   int
	input     service.CreateGame
	health    service.HealthView
	pots      []poker.PotAward
	player    service.PlayerGameView
	playerErr error
	hashes    []service.PlayerSessionHash
}

func (m *managerStub) Models(context.Context) ([]agent.ModelDescriptor, error) {
	return []agent.ModelDescriptor{{Ref: "mock", Label: "Mock", Provider: "mock", Availability: "mock"}}, m.err
}
func (m *managerStub) Create(_ context.Context, c service.CreateGame) (service.GameView, error) {
	m.creates++
	m.input = c
	return m.view, m.err
}
func (m *managerStub) Current(context.Context) (service.GameView, error)     { return m.view, m.err }
func (m *managerStub) Get(context.Context, string) (service.GameView, error) { return m.view, m.err }
func (m *managerStub) Control(context.Context, string, service.Command) (service.GameView, error) {
	return m.view, m.err
}
func (m *managerStub) GetPlayer(context.Context, string, service.PlayerSessionHash) (service.PlayerGameView, error) {
	return m.player, m.playerErr
}
func (m *managerStub) Act(context.Context, string, service.PlayerSessionHash, service.HumanAction) (service.PlayerGameView, error) {
	return m.player, m.playerErr
}
func (m *managerStub) GetHand(context.Context, string, uint32) (service.CompletedHandView, error) {
	if !m.complete {
		return service.CompletedHandView{}, service.ErrConflict
	}
	return service.CompletedHandView{GameID: "game", HandNo: 1, Result: poker.HandResult{Pots: m.pots, HoleCards: map[poker.Seat][2]poker.Card{0: {"Ac", "Qs"}}, FinalStack: map[poker.Seat]poker.Chips{0: 100}}}, nil
}
func (m *managerStub) Health(context.Context) (service.HealthView, error) { return m.health, m.err }

const gameID = "00000000-0000-4000-8000-000000000001"
const createJSON = `{"seats":[{"seat":0,"name":"A","stack":1000,"model_ref":"mock"},{"seat":1,"name":"B","stack":1000,"model_ref":"mock"}],"button":0,"small_blind":5,"big_blind":10,"max_hands":2,"max_live_calls":0}`

func TestPort8081SecurityContract(t *testing.T) {
	if Address != "127.0.0.1:8081" {
		t.Errorf("listener address got %s want 127.0.0.1:8081", Address)
	}
	for _, tc := range []struct {
		name, host, origin string
		want               int
	}{
		{"loopback_8081_same_origin", "127.0.0.1:8081", "http://127.0.0.1:8081", 201},
		{"localhost_8081_same_origin", "localhost:8081", "http://localhost:8081", 201},
		{"old_loopback_host_8080", "127.0.0.1:8080", "", 403},
		{"old_localhost_host_8080", "localhost:8080", "", 403},
		{"neighbor_host_8082", "127.0.0.1:8082", "", 403},
		{"old_origin_8080", "127.0.0.1:8081", "http://127.0.0.1:8080", 403},
		{"neighbor_origin_8082", "127.0.0.1:8081", "http://127.0.0.1:8082", 403},
		{"hostname_mismatch", "127.0.0.1:8081", "http://localhost:8081", 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := &managerStub{}
			headers := map[string]string{"Host": tc.host}
			if tc.origin != "" {
				headers["Origin"] = tc.origin
			}
			w := request(Handler(m, nil), "POST", "/api/v1/games", createJSON, headers)
			if w.Code != tc.want {
				t.Errorf("status got %d want %d", w.Code, tc.want)
			}
			if tc.want == 403 && m.creates != 0 {
				t.Error("rejected origin reached service")
			}
		})
	}
}

func TestDynamicListenerAndOriginWhitelist(t *testing.T) {
	opts := Options{Host: "127.0.0.1", Port: 8083, Timeout: 7 * time.Second}
	for _, tc := range []struct {
		name, host, origin string
		want               int
	}{
		{"new_port_same_origin", "127.0.0.1:8083", "http://127.0.0.1:8083", 201},
		{"new_port_localhost_same_origin", "localhost:8083", "http://localhost:8083", 201},
		{"old_port_8081_host_rejected", "127.0.0.1:8081", "", 403},
		{"old_port_8081_origin_rejected", "127.0.0.1:8083", "http://127.0.0.1:8081", 403},
		{"cross_origin_rejected", "127.0.0.1:8083", "http://evil.invalid", 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := &managerStub{}
			headers := map[string]string{"Host": tc.host}
			if tc.origin != "" {
				headers["Origin"] = tc.origin
			}
			w := request(Handler(m, nil, opts), "POST", "/api/v1/games", createJSON, headers)
			if w.Code != tc.want {
				t.Errorf("status got %d want %d", w.Code, tc.want)
			}
			if tc.want == 403 && m.creates != 0 {
				t.Error("rejected origin reached service")
			}
		})
	}
	if opts.address() != "127.0.0.1:8083" {
		t.Fatalf("configured address mismatch: %s", opts.address())
	}
	s := New(&managerStub{}, nil, opts)
	if s.Server.Addr != "127.0.0.1:8083" || s.Server.ReadTimeout != 7*time.Second || s.Server.WriteTimeout != 7*time.Second {
		t.Fatalf("listener/timeout not wired: addr=%s read=%s write=%s", s.Server.Addr, s.Server.ReadTimeout, s.Server.WriteTimeout)
	}
}

func request(h http.Handler, method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "http://127.0.0.1:8081"+path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		if k == "Host" {
			r.Host = v
		} else {
			r.Header.Set(k, v)
		}
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func TestPublicDTOPrivacyHTTP(t *testing.T) {
	m := &managerStub{view: service.GameView{ID: gameID, Status: "running", Hand: &service.HandView{Number: 1, State: poker.PublicState{Street: "preflop"}}}}
	h := Handler(m, nil)
	w := request(h, "GET", "/api/v1/games/"+gameID, "", nil)
	if w.Code != 200 || strings.Contains(w.Body.String(), "hole") || strings.Contains(w.Body.String(), "snapshot") {
		t.Fatal("private public DTO")
	}
	w = request(h, "GET", "/api/v1/games/"+gameID+"/hands/1", "", nil)
	if w.Code != 409 {
		t.Fatal("unfinished hand revealed")
	}
	m.complete = true
	w = request(h, "GET", "/api/v1/games/"+gameID+"/hands/1", "", nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"hole_cards":[{"seat":0,"cards":["Ac","Qs"]}]`) || !strings.Contains(w.Body.String(), `"refunds":[]`) {
		t.Fatal("completed hand conversion")
	}
}
func TestModelsWrapperAndHealth(t *testing.T) {
	m := &managerStub{health: service.HealthView{MySQL: "available", Redis: "available", MaxLiveCalls: 1000}}
	h := Handler(m, nil)
	w := request(h, "GET", "/api/v1/models", "", nil)
	if w.Code != 200 || w.Body.String() != `{"models":[{"ref":"mock","label":"Mock","provider":"mock","availability":"mock","error_code":null}]}`+"\n" {
		t.Fatal("models wrapper mismatch")
	}
	w = request(h, "GET", "/api/v1/health", "", nil)
	if w.Body.String() != `{"mysql":"available","redis":"available","storage_degraded":false,"allow_paid_models":false,"max_live_calls":1000}`+"\n" {
		t.Fatal("health contract mismatch")
	}
	m.err = service.ErrStorage
	m.health.Redis = "unavailable"
	m.health.StorageDegraded = true
	w = request(h, "GET", "/api/v1/health", "", nil)
	if w.Code != 503 || !strings.Contains(w.Body.String(), `"redis":"unavailable"`) {
		t.Fatal("health did not fail closed")
	}
}

func TestCompletedPotSeatArraysHTTP(t *testing.T) {
	for _, tc := range potSeatJSONCases {
		t.Run(tc.name, func(t *testing.T) {
			m := &managerStub{complete: true, pots: []poker.PotAward{{Eligible: tc.eligible, Winners: tc.winners}}}
			w := request(Handler(m, nil), "GET", "/api/v1/games/"+gameID+"/hands/1", "", nil)
			if w.Code != http.StatusOK {
				t.Fatalf("status got %d want 200", w.Code)
			}
			assertPotSeatJSON(t, w.Body.Bytes(), tc.wantEligible, tc.wantWinners)
		})
	}
}
func TestInputLimitsAndOrigins(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		headers    map[string]string
		status     int
	}{
		{"default_mock", createJSON, nil, 201},
		{"same_origin", createJSON, map[string]string{"Origin": "http://127.0.0.1:8081"}, 201},
		{"cross_origin", createJSON, map[string]string{"Origin": "http://evil.invalid"}, 403},
		{"dns_rebinding", createJSON, map[string]string{"Host": "evil.invalid:8081"}, 403},
		{"null_origin", createJSON, map[string]string{"Origin": "null"}, 403},
		{"fetch_cross_site", createJSON, map[string]string{"Sec-Fetch-Site": "cross-site"}, 403},
		{"oversized_body", strings.Repeat(" ", maxBody+1), nil, 400},
		{"oversized_headers", createJSON, map[string]string{"X-Test": strings.Repeat("x", maxHeaders+1)}, 431},
		{"unknown_field", strings.TrimSuffix(createJSON, "}") + `,"hole_cards":[]}`, nil, 400},
		{"missing_required", `{"seats":[]}`, nil, 400},
		{"duplicate_key", strings.TrimSuffix(createJSON, "}") + `,"button":1}`, nil, 400},
		{"trailing_value", createJSON + ` {}`, nil, 400},
		{"not_json", createJSON, map[string]string{"Content-Type": "text/plain"}, 400},
		{"null_mode", strings.TrimSuffix(createJSON, "}") + `,"mode":null}`, nil, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := &managerStub{}
			w := request(Handler(m, nil), "POST", "/api/v1/games", tc.body, tc.headers)
			if w.Code != tc.status {
				t.Fatalf("status got %d want %d", w.Code, tc.status)
			}
			if tc.status != 201 && m.creates != 0 {
				t.Fatal("invalid request reached service")
			}
			if tc.status == 201 && m.input.Mode != "mock" {
				t.Fatal("missing mode not mock")
			}
		})
	}
}
func TestSafeErrorsAndCurrent(t *testing.T) {
	for _, tc := range []struct {
		err    error
		status int
	}{{service.ErrInvalid, 400}, {service.ErrForbidden, 403}, {service.ErrNotFound, 404}, {service.ErrConflict, 409}, {service.ErrStorage, 503}, {errors.New("TEST_INTERNAL_DETAIL"), 500}} {
		m := &managerStub{err: tc.err}
		w := request(Handler(m, nil), "GET", "/api/v1/games/current", "", nil)
		if w.Code != tc.status || strings.Contains(w.Body.String(), "TEST_INTERNAL_DETAIL") {
			t.Fatal("unsafe error boundary")
		}
		var v ErrorDTO
		if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil || v.Error.Code == "" {
			t.Fatal("error schema")
		}
	}
}
func TestKratosHTTPConfigurationAndStatic(t *testing.T) {
	s := New(&managerStub{}, fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("test-only")}, "LICENSE": &fstest.MapFile{Data: []byte("test-only")}})
	if s.Server.MaxHeaderBytes != maxHeaders || s.Server.ReadHeaderTimeout <= 0 || s.Server.WriteTimeout <= 0 {
		t.Fatal("missing HTTP limits")
	}
	w := request(s, "GET", "/", "", nil)
	if w.Code != 200 || w.Body.String() != "test-only" {
		t.Fatal("Kratos handler not serving assets")
	}
	w = request(s, "GET", "/LICENSE", "", nil)
	if w.Code != 200 {
		t.Fatal("extensionless static asset failed")
	}
}
