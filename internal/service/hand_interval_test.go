package service

import (
	"context"
	"testing"
	"time"

	"texas-holdem/internal/agent"
	"texas-holdem/internal/poker"
)

// completedState 打出一条完整到摊牌的手牌状态，供重启恢复测试构造已结束的一手。
func completedState(t *testing.T) poker.State {
	t.Helper()
	s, err := poker.NewHand(poker.HandConfig{Players: []poker.PlayerInit{{Seat: 0, Stack: 1000}, {Seat: 1, Stack: 1000}}, Button: 0, SmallBlind: 5, BigBlind: 10}, poker.StandardDeck())
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		p := poker.Public(s)
		if p.Actor == nil {
			return s
		}
		l, err := poker.Legal(s, *p.Actor)
		if err != nil {
			t.Fatal(err)
		}
		kind := poker.ActionKind("call")
		if l.CanCheck {
			kind = "check"
		}
		if s, err = poker.Apply(s, *p.Actor, poker.Action{Kind: kind}); err != nil {
			t.Fatal(err)
		}
	}
	t.Fatal("hand did not complete")
	return poker.State{}
}

// TestHandIntervalGatesNextHand 验证结算后窗口内不推进、窗口过后才开新手。
func TestHandIntervalGatesNextHand(t *testing.T) {
	r := newTestRepo()
	const window = 300 * time.Millisecond
	e := engine(t, r, &testFactory{d: agent.NewMockDecider()}, Options{HandIntervalShowdown: window, HandIntervalUncontested: window})
	c := config()
	c.MaxHands = 3
	v, err := e.Create(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Control(context.Background(), v.ID, "start"); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool {
		rec, err := r.Get(context.Background(), v.ID)
		return err == nil && rec.View.HandsCompleted == 1 && rec.NextHandAt != nil
	})
	rec, _ := r.Get(context.Background(), v.ID)
	if rec.View.Hand.Number != 1 {
		t.Fatalf("hand number after first settlement got %d want 1", rec.View.Hand.Number)
	}
	// 窗口内不得开新手。
	time.Sleep(100 * time.Millisecond)
	rec, _ = r.Get(context.Background(), v.ID)
	if rec.View.Hand.Number != 1 || rec.View.HandsCompleted != 1 {
		t.Fatalf("next hand started inside window: hand=%d completed=%d", rec.View.Hand.Number, rec.View.HandsCompleted)
	}
	// 窗口过后应推进到第二手。
	eventually(t, func() bool {
		rec, err := r.Get(context.Background(), v.ID)
		return err == nil && rec.View.Hand != nil && rec.View.Hand.Number == 2
	})
}

// TestHandIntervalNotBypassedByPauseResume 验证暂停/恢复不能绕过剩余窗口：
// 在窗口内暂停、很快恢复后，剩余窗口仍需走完才开新手。
func TestHandIntervalNotBypassedByPauseResume(t *testing.T) {
	r := newTestRepo()
	const window = 600 * time.Millisecond
	e := engine(t, r, &testFactory{d: agent.NewMockDecider()}, Options{HandIntervalShowdown: window, HandIntervalUncontested: window})
	c := config()
	c.MaxHands = 3
	v, err := e.Create(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Control(context.Background(), v.ID, "start"); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool {
		rec, err := r.Get(context.Background(), v.ID)
		return err == nil && rec.View.HandsCompleted == 1 && rec.NextHandAt != nil
	})
	if _, err := e.Control(context.Background(), v.ID, "pause"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	if _, err := e.Control(context.Background(), v.ID, "resume"); err != nil {
		t.Fatal(err)
	}
	// 恢复后剩余窗口未走完：不得立即开新手。
	time.Sleep(100 * time.Millisecond)
	rec, _ := r.Get(context.Background(), v.ID)
	if rec.View.Hand.Number != 1 {
		t.Fatalf("resume bypassed hand interval: hand=%d", rec.View.Hand.Number)
	}
	eventually(t, func() bool {
		rec, err := r.Get(context.Background(), v.ID)
		return err == nil && rec.View.Hand != nil && rec.View.Hand.Number == 2
	})
}

// TestHandIntervalSurvivesRestart 验证窗口随记录持久化：重启恢复后仍保留原最早开始时间，
// 恢复运行不得提前开新手，窗口过后才推进。
func TestHandIntervalSurvivesRestart(t *testing.T) {
	r := newTestRepo()
	s := completedState(t)
	b, err := poker.EncodeState(s)
	if err != nil {
		t.Fatal(err)
	}
	c := config()
	c.MaxHands = 3
	next := time.Now().Add(700 * time.Millisecond)
	v := GameView{ID: "seed", Revision: 1, Status: "running", Mode: "mock", MaxHands: 3, HandsCompleted: 1,
		Seats: []SeatView{{Seat: 0, Stack: 1000}, {Seat: 1, Stack: 1000}},
		Hand:  &HandView{Number: 1, State: poker.Public(s)}}
	if err = r.Create(context.Background(), Record{Config: c, View: v, Snapshot: b, NextHandAt: &next}); err != nil {
		t.Fatal(err)
	}
	e := engine(t, r, nil, Options{HandIntervalShowdown: time.Second, HandIntervalUncontested: time.Second})
	got, err := r.Get(context.Background(), "seed")
	if err != nil {
		t.Fatal(err)
	}
	if got.View.Status != "paused" || got.NextHandAt == nil || !got.NextHandAt.Equal(next) {
		t.Fatalf("hand interval lost on restart: status=%q next=%v", got.View.Status, got.NextHandAt)
	}
	if _, err := e.Control(context.Background(), "seed", "resume"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	got, _ = r.Get(context.Background(), "seed")
	if got.View.Hand.Number != 1 {
		t.Fatalf("restart resume bypassed remaining interval: hand=%d", got.View.Hand.Number)
	}
	eventually(t, func() bool {
		got, err := r.Get(context.Background(), "seed")
		return err == nil && got.View.Hand != nil && got.View.Hand.Number == 2
	})
}

// TestHandIntervalLastHandStops 验证最后一手结束后不设置下一手窗口，游戏直接终局。
func TestHandIntervalLastHandStops(t *testing.T) {
	r := newTestRepo()
	e := engine(t, r, &testFactory{d: agent.NewMockDecider()}, Options{HandIntervalShowdown: 200 * time.Millisecond, HandIntervalUncontested: 200 * time.Millisecond})
	c := config()
	c.MaxHands = 1
	v, err := e.Create(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Control(context.Background(), v.ID, "start"); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool {
		rec, err := r.Get(context.Background(), v.ID)
		return err == nil && rec.View.Status == "completed"
	})
	rec, _ := r.Get(context.Background(), v.ID)
	if rec.NextHandAt != nil {
		t.Fatalf("last hand left a pending next-hand window: %v", rec.NextHandAt)
	}
	if rec.View.HandsCompleted != 1 {
		t.Fatalf("completed hands got %d want 1", rec.View.HandsCompleted)
	}
}
