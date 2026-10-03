package server

import (
	"encoding/json"
	"strings"
	"testing"
	"texas-holdem/internal/poker"
	"texas-holdem/internal/service"
)

func ptr[T any](v T) *T { return &v }

func humanView() service.GameView {
	seat := poker.Seat(0)
	return service.GameView{
		ID: gameID, Revision: 3, Status: "running", Mode: "mock", MaxHands: 2,
		HumanSeat: &seat,
		Seats: []service.SeatView{
			{Seat: 0, Name: "Human", Stack: 1000, Kind: service.SeatHuman},
			{Seat: 1, Name: "AI", Stack: 1000, ModelRef: "test/model", Kind: service.SeatAI},
		},
		Hand: &service.HandView{Number: 1, WaitingFor: ptr("human_action"), State: poker.PublicState{Street: "preflop", Actor: &seat}},
	}
}

// TestHumanPublicDTOPrivacy 验证公共转换永远 player=null、human_seat 可见、无任何底牌字段。
func TestHumanPublicDTOPrivacy(t *testing.T) {
	b, err := MarshalGame(humanView())
	if err != nil {
		t.Fatal(err)
	}
	out := string(b)
	if strings.Contains(out, "hole") || strings.Contains(out, "snapshot") {
		t.Fatalf("public DTO leaked private fields: %s", out)
	}
	var m map[string]any
	if err = json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if m["player"] != nil {
		t.Fatalf("public player must be null: %v", m["player"])
	}
	if m["human_seat"] != float64(0) {
		t.Fatalf("human_seat missing: %v", m["human_seat"])
	}
	hand := m["hand"].(map[string]any)
	if hand["waiting_for"] != "human_action" {
		t.Fatalf("waiting_for missing: %v", hand["waiting_for"])
	}
	seats := m["seats"].([]any)
	if seats[0].(map[string]any)["kind"] != "human" || seats[1].(map[string]any)["kind"] != "ai" {
		t.Fatalf("seat kind mismatch: %v", seats)
	}
}

// TestHumanPlayerDTOPrivacy 验证本人视角只含自己的底牌，公共转换不泄露。
func TestHumanPlayerDTOPrivacy(t *testing.T) {
	pv := service.PlayerGameView{
		Game: humanView(),
		Player: service.PlayerView{
			Seat:         0,
			HoleCards:    []poker.Card{"Ac", "Qs"},
			LegalActions: &poker.LegalActions{CanFold: true, CanCall: true, CallAmount: 50, CanRaise: true, MinRaiseTo: 200, MaxRaiseTo: 10000},
		},
	}
	d := PlayerGame(pv)
	if d.Player == nil || len(d.Player.HoleCards) != 2 || d.Player.HoleCards[0] != "Ac" {
		t.Fatalf("player private view missing: %+v", d.Player)
	}
	b, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	// 只有本人（seat 0）底牌，绝无 AI 底牌；公共 Game() 不含 player。
	if strings.Count(string(b), "hole_cards") != 1 {
		t.Fatalf("unexpected hole_cards count: %s", b)
	}
	pub, _ := json.Marshal(Game(pv.Game))
	if strings.Contains(string(pub), "hole_cards") || !strings.Contains(string(pub), `"player":null`) {
		t.Fatalf("public conversion leaked player: %s", pub)
	}
}

// TestHumanHTTPPlayerAndAction 验证 Cookie 会话与动作路由的 HTTP 边界。
func TestHumanHTTPPlayerAndAction(t *testing.T) {
	m := &managerStub{player: service.PlayerGameView{Game: humanView(), Player: service.PlayerView{Seat: 0, HoleCards: []poker.Card{"Ac", "Qs"}}}}
	h := Handler(m, nil)
	actionBody := `{"expected_revision":3,"kind":"raise_to","to":300}`

	t.Run("missing_cookie_403", func(t *testing.T) {
		w := request(h, "GET", "/api/v1/games/"+gameID+"/player", "", nil)
		if w.Code != 403 || !strings.Contains(w.Body.String(), "PLAYER_NOT_AUTHORIZED") {
			t.Fatalf("missing cookie: %d %s", w.Code, w.Body.String())
		}
	})
	t.Run("duplicate_cookie_403", func(t *testing.T) {
		w := request(h, "GET", "/api/v1/games/"+gameID+"/player", "", map[string]string{"Cookie": "poker_player=aa;poker_player=bb"})
		if w.Code != 403 {
			t.Fatalf("duplicate cookie accepted: %d", w.Code)
		}
	})
	t.Run("valid_cookie_200", func(t *testing.T) {
		cred := strings.Repeat("ab", 32)
		w := request(h, "GET", "/api/v1/games/"+gameID+"/player", "", map[string]string{"Cookie": "poker_player=" + cred})
		if w.Code != 200 || !strings.Contains(w.Body.String(), `"hole_cards":["Ac","Qs"]`) {
			t.Fatalf("valid cookie failed: %d %s", w.Code, w.Body.String())
		}
	})
	t.Run("action_valid_and_bad", func(t *testing.T) {
		cred := strings.Repeat("ab", 32)
		w := request(h, "POST", "/api/v1/games/"+gameID+"/action", actionBody, map[string]string{"Cookie": "poker_player=" + cred})
		if w.Code != 200 {
			t.Fatalf("valid action failed: %d %s", w.Code, w.Body.String())
		}
		// kind 非法。
		w = request(h, "POST", "/api/v1/games/"+gameID+"/action", `{"expected_revision":3,"kind":"all_in"}`, map[string]string{"Cookie": "poker_player=" + cred})
		if w.Code != 400 {
			t.Fatalf("bad kind got %d", w.Code)
		}
		// 缺 expected_revision。
		w = request(h, "POST", "/api/v1/games/"+gameID+"/action", `{"kind":"fold"}`, map[string]string{"Cookie": "poker_player=" + cred})
		if w.Code != 400 {
			t.Fatalf("missing revision got %d", w.Code)
		}
	})
	t.Run("error_mapping", func(t *testing.T) {
		for _, tc := range []struct {
			err  error
			code string
		}{
			{service.ErrIllegalAction, "ILLEGAL_ACTION"},
			{service.ErrStaleAction, "STALE_ACTION"},
			{service.ErrActionExpired, "ACTION_EXPIRED"},
			{service.ErrPlayerUnauthorized, "PLAYER_NOT_AUTHORIZED"},
			{service.ErrHumanLocalOnly, "HUMAN_LOCAL_ONLY"},
		} {
			mm := &managerStub{playerErr: tc.err}
			cred := strings.Repeat("ab", 32)
			w := request(Handler(mm, nil), "GET", "/api/v1/games/"+gameID+"/player", "", map[string]string{"Cookie": "poker_player=" + cred})
			if !strings.Contains(w.Body.String(), tc.code) {
				t.Fatalf("%v not mapped to %s: %s", tc.err, tc.code, w.Body.String())
			}
		}
	})
}

// TestHumanCreateSetsCookieAndLocalOnly 验证创建真人游戏设置 HttpOnly Cookie，非 loopback 拒绝。
func TestHumanCreateSetsCookieAndLocalOnly(t *testing.T) {
	humanCreate := `{"seats":[` +
		`{"seat":0,"name":"我","stack":10000,"kind":"human"},` +
		`{"seat":1,"name":"AI","stack":10000,"kind":"ai","model_ref":"mock"}],` +
		`"button":0,"small_blind":50,"big_blind":100,"max_hands":100,"max_live_calls":0}`

	t.Run("loopback_sets_cookie", func(t *testing.T) {
		m := &managerStub{view: humanView()}
		w := request(Handler(m, nil), "POST", "/api/v1/games", humanCreate, nil)
		if w.Code != 201 {
			t.Fatalf("create human game got %d %s", w.Code, w.Body.String())
		}
		if m.input.PlayerSessionHash == nil {
			t.Fatal("session hash not injected by HTTP layer")
		}
		cookies := w.Result().Cookies()
		var player *httpCookie
		for _, c := range cookies {
			if c.Name == playerCookieName {
				player = &httpCookie{value: c.Value, httpOnly: c.HttpOnly, path: c.Path, sameSite: c.SameSite}
			}
		}
		if player == nil || !player.httpOnly || player.path != "/api/v1/games/"+gameID {
			t.Fatalf("player cookie mismatch: %+v", player)
		}
		// 全 AI 创建不设 Cookie、不注入摘要。
		m2 := &managerStub{view: humanView()}
		w2 := request(Handler(m2, nil), "POST", "/api/v1/games", createJSON, nil)
		if w2.Code != 201 || m2.input.PlayerSessionHash != nil {
			t.Fatalf("all-AI create injected session: %d %+v", w2.Code, m2.input)
		}
		for _, c := range w2.Result().Cookies() {
			if c.Name == playerCookieName {
				t.Fatal("all-AI create set player cookie")
			}
		}
	})
	t.Run("non_loopback_human_rejected", func(t *testing.T) {
		m := &managerStub{view: humanView()}
		opts := Options{Host: "0.0.0.0", Port: 8081}
		w := request(Handler(m, nil, opts), "POST", "/api/v1/games", humanCreate, map[string]string{"Host": "0.0.0.0:8081"})
		if w.Code != 403 || !strings.Contains(w.Body.String(), "HUMAN_LOCAL_ONLY") {
			t.Fatalf("non-loopback human create: %d %s", w.Code, w.Body.String())
		}
		if m.creates != 0 {
			t.Fatal("non-loopback human create reached service")
		}
	})
}

type httpCookie struct {
	value    string
	httpOnly bool
	path     string
	sameSite interface{}
}
