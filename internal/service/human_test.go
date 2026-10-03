package service

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"texas-holdem/internal/agent"
	"texas-holdem/internal/poker"
	"time"
)

// ---------- 测试夹具 ----------

func sessionHash(seed byte) PlayerSessionHash {
	var h PlayerSessionHash
	for i := range h {
		h[i] = seed
	}
	return h
}

func humanConfig() CreateGame {
	return CreateGame{
		Seats: []SeatConfig{
			{Seat: 0, Name: "Human", Stack: 1000, Kind: SeatHuman},
			{Seat: 1, Name: "AI", Stack: 1000, ModelRef: "test/model", Kind: SeatAI},
		},
		Button: 1, SmallBlind: 5, BigBlind: 10, MaxHands: 100, Mode: "mock",
	}
}

func humanView(s poker.State) GameView {
	seat := poker.Seat(0)
	return GameView{
		ID: "human-game", Revision: 1, Status: "created", Mode: "mock", MaxHands: 100,
		Seats: []SeatView{
			{Seat: 0, Name: "Human", Stack: 1000, Kind: SeatHuman},
			{Seat: 1, Name: "AI", Stack: 1000, ModelRef: "test/model", Kind: SeatAI},
		},
		HumanSeat: &seat,
		Hand:      &HandView{Number: 1, State: poker.Public(s)},
	}
}

func humanState(t *testing.T, aiAction *poker.Action) poker.State {
	t.Helper()
	s, err := poker.NewHand(poker.HandConfig{Players: []poker.PlayerInit{{Seat: 0, Stack: 1000}, {Seat: 1, Stack: 1000}}, Button: 1, SmallBlind: 5, BigBlind: 10}, poker.StandardDeck())
	if err != nil {
		t.Fatal(err)
	}
	if aiAction != nil {
		p := poker.Public(s)
		if s, err = poker.Apply(s, *p.Actor, *aiAction); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

// seedHuman 直接写入一份真人私有记录；status 通常为 created，由 Control start 驱动。
func seedHuman(t *testing.T, r *testRepo, status string, s poker.State, wait *HumanWait, timeout time.Duration) {
	t.Helper()
	b, err := poker.EncodeState(s)
	if err != nil {
		t.Fatal(err)
	}
	h := sessionHash(0xAB)
	v := humanView(s)
	v.Status = status
	if wait != nil {
		v.Hand.WaitingFor = text("human_action")
	}
	rec := Record{Config: humanConfig(), View: v, Snapshot: b, HumanSessionHash: &h, HumanActionTimeout: timeout, HumanWait: wait}
	if err = r.Create(context.Background(), rec); err != nil {
		t.Fatal(err)
	}
}

func recordOf(t *testing.T, r *testRepo, id string) Record {
	t.Helper()
	rec, err := r.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return rec
}

// ---------- 场景：全 AI 旧记录兼容 ----------

func TestHumanSeatBackwardCompatibility(t *testing.T) {
	r := newTestRepo()
	e := engine(t, r, nil, Options{})
	c := config() // Kind 全空
	v, err := e.Create(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	if v.HumanSeat != nil {
		t.Fatal("old-style AI game exposed a human seat")
	}
	for _, s := range v.Seats {
		if s.Kind != SeatAI {
			t.Fatalf("kind not normalized to ai: %q", s.Kind)
		}
	}
	// 旧 JSON 记录缺少新字段：解码后按 ai 解释且无真人会话。
	raw, err := json.Marshal(Record{Config: c, View: v})
	if err != nil {
		t.Fatal(err)
	}
	var back Record
	if err = json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	if humanSeat(back.Config) != nil || back.HumanSessionHash != nil {
		t.Fatal("legacy record decoded as human")
	}
}

// ---------- 场景：最多一个真人、模型引用校验 ----------

func TestHumanSeatValidation(t *testing.T) {
	valid := func() CreateGame {
		c := humanConfig()
		h := sessionHash(1)
		c.PlayerSessionHash = &h
		return c
	}
	cases := []struct {
		name string
		mut  func(*CreateGame)
	}{
		{"two_humans", func(c *CreateGame) { c.Seats[1].Kind = SeatHuman; c.Seats[1].ModelRef = "" }},
		{"human_with_model_ref", func(c *CreateGame) { c.Seats[0].ModelRef = "test/model" }},
		{"human_zero_stack", func(c *CreateGame) { c.Seats[0].Stack = 0 }},
		{"human_without_session", func(c *CreateGame) { c.PlayerSessionHash = nil }},
		{"ai_with_session", func(c *CreateGame) { c.Seats[0].Kind = SeatAI; c.Seats[0].ModelRef = "test/model" }},
		{"unknown_kind", func(c *CreateGame) { c.Seats[0].Kind = "robot" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newTestRepo()
			e := engine(t, r, nil, Options{})
			c := valid()
			tc.mut(&c)
			if _, err := e.Create(context.Background(), c); !errors.Is(err, ErrInvalid) {
				t.Fatalf("got %v want INVALID_INPUT", err)
			}
		})
	}
	r := newTestRepo()
	e := engine(t, r, nil, Options{})
	v, err := e.Create(context.Background(), valid())
	if err != nil {
		t.Fatalf("valid human game rejected: %v", err)
	}
	if v.HumanSeat == nil || *v.HumanSeat != 0 || v.Seats[0].Kind != SeatHuman || v.Seats[0].ModelRef != "" {
		t.Fatalf("human seat not normalized: %+v", v)
	}
	r.mu.Lock()
	rec := r.records[v.ID]
	r.mu.Unlock()
	if rec.Config.PlayerSessionHash != nil || rec.HumanSessionHash == nil || rec.HumanActionTimeout != 60*time.Second {
		t.Fatal("session hash not moved to private record / timeout not persisted")
	}
}

// ---------- 场景：本人授权与其他浏览器拒绝 ----------

func TestPlayerSessionAuthorization(t *testing.T) {
	r := newTestRepo()
	e := engine(t, r, nil, Options{})
	c := humanConfig()
	h := sessionHash(1)
	c.PlayerSessionHash = &h
	v, err := e.Create(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.GetPlayer(context.Background(), v.ID, h); err != nil {
		t.Fatalf("authorized read failed: %v", err)
	}
	if _, err := e.GetPlayer(context.Background(), v.ID, sessionHash(2)); !errors.Is(err, ErrPlayerUnauthorized) {
		t.Fatalf("wrong session got %v", err)
	}
	if _, err := e.GetPlayer(context.Background(), "00000000-0000-4000-8000-000000000000", h); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing game got %v", err)
	}
	// 全 AI 局不能通过真人口令读取。
	aiRepo := newTestRepo()
	ae := engine(t, aiRepo, nil, Options{})
	av, err := ae.Create(context.Background(), config())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ae.GetPlayer(context.Background(), av.ID, h); !errors.Is(err, ErrPlayerUnauthorized) {
		t.Fatalf("AI game accepted session: %v", err)
	}
}

// ---------- 场景：四种引擎动作 ----------

func TestHumanActions(t *testing.T) {
	cases := []struct {
		name      string
		aiAction  *poker.Action
		action    func(t *testing.T, s poker.State) poker.Action
		wantKind  poker.ActionKind
		wantCheck bool
	}{
		{"check", &poker.Action{Kind: "call"}, func(_ *testing.T, _ poker.State) poker.Action { return poker.Action{Kind: "check"} }, "check", true},
		{"fold", &poker.Action{Kind: "raise_to", To: 30}, func(_ *testing.T, _ poker.State) poker.Action { return poker.Action{Kind: "fold"} }, "fold", false},
		{"call", &poker.Action{Kind: "raise_to", To: 30}, func(_ *testing.T, _ poker.State) poker.Action { return poker.Action{Kind: "call"} }, "call", false},
		{"raise_to", &poker.Action{Kind: "raise_to", To: 30}, func(t *testing.T, s poker.State) poker.Action {
			l, err := poker.Legal(s, 0)
			if err != nil || !l.CanRaise {
				t.Fatalf("expected raise right: %v %+v", err, l)
			}
			return poker.Action{Kind: "raise_to", To: l.MinRaiseTo}
		}, "raise_to", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newTestRepo()
			s := humanState(t, tc.aiAction)
			deadline := time.Now().Add(time.Minute)
			seedHuman(t, r, "created", s, &HumanWait{HandNo: 1, Seat: 0, Deadline: &deadline}, time.Minute)
			e := engine(t, r, nil, Options{HumanActionTimeout: time.Minute})
			if _, err := e.Control(context.Background(), "human-game", "start"); err != nil {
				t.Fatal(err)
			}
			rec := recordOf(t, r, "human-game")
			ha := HumanAction{ExpectedRevision: rec.View.Revision, Action: tc.action(t, s)}
			pv, err := e.Act(context.Background(), "human-game", sessionHash(0xAB), ha)
			if err != nil {
				t.Fatalf("action %s rejected: %v", tc.name, err)
			}
			if pv.Player.Seat != 0 {
				t.Fatalf("player seat mismatch: %d", pv.Player.Seat)
			}
			r.mu.Lock()
			actions := append([]ActionView{}, r.actions...)
			after := r.records["human-game"]
			r.mu.Unlock()
			if len(actions) != 1 || actions[0].Kind != tc.wantKind || actions[0].Source != "human" || actions[0].ErrorCode != nil {
				t.Fatalf("human action record mismatch: %+v", actions)
			}
			if after.HumanWait != nil || after.View.Hand.WaitingFor != nil {
				t.Fatal("wait state not cleared after action")
			}
		})
	}
}

// ---------- 场景：非法动作不推进、不重置时限 ----------

func TestHumanIllegalAction(t *testing.T) {
	r := newTestRepo()
	s := humanState(t, &poker.Action{Kind: "raise_to", To: 30}) // 面对下注，不能 check
	deadline := time.Now().Add(time.Minute)
	seedHuman(t, r, "created", s, &HumanWait{HandNo: 1, Seat: 0, Deadline: &deadline}, time.Minute)
	e := engine(t, r, nil, Options{HumanActionTimeout: time.Minute})
	if _, err := e.Control(context.Background(), "human-game", "start"); err != nil {
		t.Fatal(err)
	}
	rec := recordOf(t, r, "human-game")
	before := rec.View.Revision
	_, err := e.Act(context.Background(), "human-game", sessionHash(0xAB), HumanAction{ExpectedRevision: before, Action: poker.Action{Kind: "check"}})
	if !errors.Is(err, ErrIllegalAction) {
		t.Fatalf("got %v want ILLEGAL_ACTION", err)
	}
	after := recordOf(t, r, "human-game")
	if after.View.Revision != before || after.HumanWait == nil || after.HumanWait.Deadline == nil || !after.HumanWait.Deadline.Equal(deadline) {
		t.Fatal("illegal action advanced state or reset deadline")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.actions) != 0 {
		t.Fatal("illegal action recorded")
	}
}

// ---------- 场景：能 check 时超时仍 fold ----------

func TestHumanTimeoutAlwaysFolds(t *testing.T) {
	r := newTestRepo()
	s := humanState(t, &poker.Action{Kind: "call"}) // human 可 check
	deadline := time.Now().Add(-time.Second)
	seedHuman(t, r, "created", s, &HumanWait{HandNo: 1, Seat: 0, Deadline: &deadline}, time.Minute)
	e := engine(t, r, nil, Options{HumanActionTimeout: time.Minute})
	if _, err := e.Control(context.Background(), "human-game", "start"); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { r.mu.Lock(); defer r.mu.Unlock(); return len(r.actions) > 0 })
	r.mu.Lock()
	a := clone(r.actions[0])
	r.mu.Unlock()
	if a.Kind != "fold" || a.Source != "fallback" || a.ErrorCode == nil || *a.ErrorCode != "HUMAN_ACTION_TIMEOUT" {
		t.Fatalf("timeout did not always fold: %+v", a)
	}
}

// ---------- 场景：截止、双击、跨手、动作/超时竞争 ----------

func TestHumanActionConcurrency(t *testing.T) {
	t.Run("expired_act_triggers_timeout_fold", func(t *testing.T) {
		r := newTestRepo()
		s := humanState(t, &poker.Action{Kind: "raise_to", To: 30})
		deadline := time.Now().Add(-time.Second)
		seedHuman(t, r, "created", s, &HumanWait{HandNo: 1, Seat: 0, Deadline: &deadline}, time.Minute)
		e := engine(t, r, nil, Options{HumanActionTimeout: time.Minute})
		if _, err := e.Control(context.Background(), "human-game", "start"); err != nil {
			t.Fatal(err)
		}
		// 让 worker 暂停后台推进，确定性地验证 Act 自行触发超时提交。
		r.mu.Lock()
		r.noCurrent = true
		r.mu.Unlock()
		rec := recordOf(t, r, "human-game")
		_, err := e.Act(context.Background(), "human-game", sessionHash(0xAB), HumanAction{ExpectedRevision: rec.View.Revision, Action: poker.Action{Kind: "call"}})
		if !errors.Is(err, ErrActionExpired) {
			t.Fatalf("got %v want ACTION_EXPIRED", err)
		}
		r.mu.Lock()
		actions := append([]ActionView{}, r.actions...)
		r.mu.Unlock()
		if len(actions) != 1 || actions[0].Kind != "fold" || *actions[0].ErrorCode != "HUMAN_ACTION_TIMEOUT" {
			t.Fatalf("timeout not committed exactly once: %+v", actions)
		}
	})

	t.Run("stale_and_double_submit", func(t *testing.T) {
		r := newTestRepo()
		s := humanState(t, &poker.Action{Kind: "raise_to", To: 30})
		deadline := time.Now().Add(time.Minute)
		seedHuman(t, r, "created", s, &HumanWait{HandNo: 1, Seat: 0, Deadline: &deadline}, time.Minute)
		e := engine(t, r, nil, Options{HumanActionTimeout: time.Minute})
		if _, err := e.Control(context.Background(), "human-game", "start"); err != nil {
			t.Fatal(err)
		}
		rev := recordOf(t, r, "human-game").View.Revision
		if _, err := e.Act(context.Background(), "human-game", sessionHash(0xAB), HumanAction{ExpectedRevision: rev, Action: poker.Action{Kind: "fold"}}); err != nil {
			t.Fatal(err)
		}
		// 双击/重复提交同一 revision。
		if _, err := e.Act(context.Background(), "human-game", sessionHash(0xAB), HumanAction{ExpectedRevision: rev, Action: poker.Action{Kind: "fold"}}); !errors.Is(err, ErrStaleAction) {
			t.Fatalf("double submit got %v want STALE_ACTION", err)
		}
		// 跨手/更低 revision。
		if _, err := e.Act(context.Background(), "human-game", sessionHash(0xAB), HumanAction{ExpectedRevision: rev - 1, Action: poker.Action{Kind: "fold"}}); !errors.Is(err, ErrStaleAction) {
			t.Fatalf("old revision got %v want STALE_ACTION", err)
		}
	})

	t.Run("concurrent_act_single_commit", func(t *testing.T) {
		r := newTestRepo()
		s := humanState(t, &poker.Action{Kind: "raise_to", To: 30})
		deadline := time.Now().Add(time.Minute)
		seedHuman(t, r, "created", s, &HumanWait{HandNo: 1, Seat: 0, Deadline: &deadline}, time.Minute)
		e := engine(t, r, nil, Options{HumanActionTimeout: time.Minute})
		if _, err := e.Control(context.Background(), "human-game", "start"); err != nil {
			t.Fatal(err)
		}
		rev := recordOf(t, r, "human-game").View.Revision
		var wg sync.WaitGroup
		results := make([]error, 2)
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				_, results[i] = e.Act(context.Background(), "human-game", sessionHash(0xAB), HumanAction{ExpectedRevision: rev, Action: poker.Action{Kind: "fold"}})
			}(i)
		}
		wg.Wait()
		r.mu.Lock()
		actions := len(r.actions)
		r.mu.Unlock()
		if actions != 1 {
			t.Fatalf("concurrent actions committed %d times", actions)
		}
		ok := 0
		for _, err := range results {
			if err == nil {
				ok++
			}
		}
		if ok != 1 {
			t.Fatalf("want exactly one success, got %v", results)
		}
	})
}

// ---------- 场景：暂停冻结、恢复、停止、晚请求 ----------

func TestHumanPauseResumeStop(t *testing.T) {
	r := newTestRepo()
	s := humanState(t, &poker.Action{Kind: "raise_to", To: 30})
	deadline := time.Now().Add(30 * time.Second)
	seedHuman(t, r, "created", s, &HumanWait{HandNo: 1, Seat: 0, Deadline: &deadline}, 60*time.Second)
	e := engine(t, r, nil, Options{HumanActionTimeout: 60 * time.Second})
	if _, err := e.Control(context.Background(), "human-game", "start"); err != nil {
		t.Fatal(err)
	}
	paused, err := e.Control(context.Background(), "human-game", "pause")
	if err != nil {
		t.Fatal(err)
	}
	if paused.Status != "paused" || paused.Hand.WaitingFor == nil {
		t.Fatalf("pause mismatch: %+v", paused)
	}
	rec := recordOf(t, r, "human-game")
	if rec.HumanWait == nil || rec.HumanWait.Deadline != nil || rec.HumanWait.Remaining <= 0 || rec.HumanWait.Remaining > 30*time.Second {
		t.Fatalf("remaining not frozen: %+v", rec.HumanWait)
	}
	// 暂停后提交动作被拒。
	if _, err := e.Act(context.Background(), "human-game", sessionHash(0xAB), HumanAction{ExpectedRevision: rec.View.Revision, Action: poker.Action{Kind: "fold"}}); !errors.Is(err, ErrConflict) {
		t.Fatalf("paused act got %v", err)
	}
	if _, err := e.Control(context.Background(), "human-game", "resume"); err != nil {
		t.Fatal(err)
	}
	rec = recordOf(t, r, "human-game")
	if rec.HumanWait == nil || rec.HumanWait.Deadline == nil {
		t.Fatal("resume did not restore deadline")
	}
	remaining := time.Until(*rec.HumanWait.Deadline)
	if remaining <= 0 || remaining > 30*time.Second {
		t.Fatalf("resume did not preserve remaining time: %s", remaining)
	}
	stopped, err := e.Control(context.Background(), "human-game", "stop")
	if err != nil || stopped.Status != "stopped" {
		t.Fatal("stop failed")
	}
	rec = recordOf(t, r, "human-game")
	if rec.HumanWait != nil || rec.View.Hand.WaitingFor != nil {
		t.Fatal("stop did not clear wait")
	}
}

// 截止后的 pause 必须先超时弃牌，不能救回行动。
func TestHumanPauseAfterExpiryFolds(t *testing.T) {
	r := newTestRepo()
	s := humanState(t, &poker.Action{Kind: "raise_to", To: 30})
	deadline := time.Now().Add(-time.Second)
	seedHuman(t, r, "created", s, &HumanWait{HandNo: 1, Seat: 0, Deadline: &deadline}, 60*time.Second)
	e := engine(t, r, nil, Options{HumanActionTimeout: 60 * time.Second})
	// 直接构造 running 记录（不经过 start），验证 Control 的截止前置超时。
	r.mu.Lock()
	rec := r.records["human-game"]
	rec.View.Status = "running"
	r.records["human-game"] = rec
	r.mu.Unlock()
	if _, err := e.Control(context.Background(), "human-game", "pause"); err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	actions := append([]ActionView{}, r.actions...)
	r.mu.Unlock()
	if len(actions) != 1 || actions[0].Kind != "fold" || *actions[0].ErrorCode != "HUMAN_ACTION_TIMEOUT" {
		t.Fatalf("expired pause did not fold first: %+v", actions)
	}
}

// ---------- 场景：重启恢复与过期等待 ----------

func TestHumanRecovery(t *testing.T) {
	t.Run("future_wait_frozen", func(t *testing.T) {
		r := newTestRepo()
		s := humanState(t, &poker.Action{Kind: "raise_to", To: 30})
		deadline := time.Now().Add(30 * time.Second)
		seedHuman(t, r, "running", s, &HumanWait{HandNo: 1, Seat: 0, Deadline: &deadline}, 60*time.Second)
		e := engine(t, r, nil, Options{HumanActionTimeout: 60 * time.Second})
		rec := recordOf(t, r, "human-game")
		if rec.View.Status != "paused" || rec.View.Reason == nil || *rec.View.Reason != "recovery" {
			t.Fatal("recovery did not pause")
		}
		if rec.HumanWait == nil || rec.HumanWait.Deadline != nil || rec.HumanWait.Remaining <= 0 || rec.HumanWait.Remaining > 30*time.Second {
			t.Fatalf("wait not frozen on recovery: %+v", rec.HumanWait)
		}
		_ = e
	})
	t.Run("expired_wait_zero_remaining", func(t *testing.T) {
		r := newTestRepo()
		s := humanState(t, &poker.Action{Kind: "raise_to", To: 30})
		deadline := time.Now().Add(-time.Second)
		seedHuman(t, r, "running", s, &HumanWait{HandNo: 1, Seat: 0, Deadline: &deadline}, 60*time.Second)
		e := engine(t, r, nil, Options{HumanActionTimeout: 60 * time.Second})
		rec := recordOf(t, r, "human-game")
		if rec.HumanWait == nil || rec.HumanWait.Remaining != 0 {
			t.Fatalf("expired wait remaining not 0: %+v", rec.HumanWait)
		}
		if _, err := e.Control(context.Background(), "human-game", "resume"); err != nil {
			t.Fatal(err)
		}
		eventually(t, func() bool { r.mu.Lock(); defer r.mu.Unlock(); return len(r.actions) > 0 })
		r.mu.Lock()
		a := clone(r.actions[0])
		r.mu.Unlock()
		if a.Kind != "fold" || *a.ErrorCode != "HUMAN_ACTION_TIMEOUT" {
			t.Fatalf("expired recovery not folded: %+v", a)
		}
	})
}

// ---------- 场景：真人不消耗 AI 调用额度 ----------

func TestHumanLiveBudgetIsolation(t *testing.T) {
	r := newTestRepo()
	// 真人 button=0，翻前先行动；弃牌即结束该手，AI 不会发起请求。
	c := CreateGame{
		Seats: []SeatConfig{
			{Seat: 0, Name: "Human", Stack: 1000, Kind: SeatHuman},
			{Seat: 1, Name: "AI", Stack: 1000, ModelRef: "test/model", Kind: SeatAI},
		},
		Button: 0, SmallBlind: 5, BigBlind: 10, MaxHands: 100, Mode: "live", MaxLiveCalls: 5,
	}
	h := sessionHash(9)
	c.PlayerSessionHash = &h
	f := &testFactory{d: agent.NewMockDecider()}
	e := engine(t, r, f, Options{AllowPaidModels: true, MaxLiveCalls: 5, HumanActionTimeout: time.Minute})
	v, err := e.Create(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	if f.calls.Load() != 1 {
		t.Fatalf("want only AI decider created, got %d", f.calls.Load())
	}
	if _, err := e.Control(context.Background(), v.ID, "start"); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool {
		rec, _ := r.Get(context.Background(), v.ID)
		return rec.HumanWait != nil
	})
	rec := recordOf(t, r, v.ID)
	if _, err := e.Act(context.Background(), v.ID, h, HumanAction{ExpectedRevision: rec.View.Revision, Action: poker.Action{Kind: "fold"}}); err != nil {
		t.Fatal(err)
	}
	after := recordOf(t, r, v.ID)
	if after.View.LiveCallsUsed != 0 {
		t.Fatalf("human consumed live budget: %d", after.View.LiveCallsUsed)
	}
	if f.calls.Load() != 1 {
		t.Fatalf("human created a decider: %d", f.calls.Load())
	}
}

// ---------- 场景：动作/快照/结果 CAS 原子提交 ----------

func TestHumanCommitAtomicity(t *testing.T) {
	r := newTestRepo()
	s := humanState(t, &poker.Action{Kind: "raise_to", To: 30})
	deadline := time.Now().Add(time.Minute)
	seedHuman(t, r, "created", s, &HumanWait{HandNo: 1, Seat: 0, Deadline: &deadline}, time.Minute)
	e := engine(t, r, nil, Options{HumanActionTimeout: time.Minute})
	if _, err := e.Control(context.Background(), "human-game", "start"); err != nil {
		t.Fatal(err)
	}
	before := recordOf(t, r, "human-game")
	rev := before.View.Revision
	if _, err := e.Act(context.Background(), "human-game", sessionHash(0xAB), HumanAction{ExpectedRevision: rev, Action: poker.Action{Kind: "call"}}); err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	if len(r.actions) != 1 || len(r.hands) != 0 {
		r.mu.Unlock()
		t.Fatal("action/hand atomicity mismatch")
	}
	updated := r.records["human-game"]
	r.mu.Unlock()
	if updated.View.Revision != rev+1 || updated.Sequence != 1 || string(updated.Snapshot) == string(before.Snapshot) {
		t.Fatal("snapshot/revision not atomically advanced")
	}
	// 以旧 revision 再次 CAS 必须冲突，不产生第二条动作。
	if err := r.Commit(context.Background(), Commit{Record: updated, ExpectedRevision: rev}); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale CAS got %v", err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.actions) != 1 {
		t.Fatal("stale CAS double committed")
	}
}
