package service

import (
	"context"
	"testing"
	"time"

	"texas-holdem/internal/agent"
)

// TestAITurnDelayInterruptibleByContext 证明「思考停顿」用 select + ctx.Done() +
// time.After 实现：ctx 取消时立即返回，不阻塞 pause/stop/Close 等控制命令。
func TestAITurnDelayInterruptibleByContext(t *testing.T) {
	t.Run("pre_canceled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		start := time.Now()
		thinkPause(ctx, time.Hour)
		if d := time.Since(start); d > time.Second {
			t.Fatalf("thinkPause blocked on canceled ctx for %s", d)
		}
	})
	t.Run("cancel_during_pause", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan time.Duration, 1)
		start := time.Now()
		go func() { thinkPause(ctx, 30*time.Second); done <- time.Since(start) }()
		time.Sleep(20 * time.Millisecond)
		cancel()
		select {
		case d := <-done:
			if d > time.Second {
				t.Fatalf("thinkPause not interrupted by cancel: %s", d)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("thinkPause did not return after cancel")
		}
	})
	t.Run("zero_delay_is_noop", func(t *testing.T) {
		start := time.Now()
		thinkPause(context.Background(), 0)
		if d := time.Since(start); d > 10*time.Millisecond {
			t.Fatalf("zero delay slept for %s", d)
		}
	})
}

// TestAITurnDelayPacesAIActions 证明 AI 动作提交后会等待步进停顿再推进下一个 actor，
// 停顿窗口内不会瞬时完成；Options.AITurnDelay 为 0 时（既有测试默认）不引入等待。
func TestAITurnDelayPacesAIActions(t *testing.T) {
	r := newTestRepo()
	f := &testFactory{d: agent.NewMockDecider()}
	const delay = 150 * time.Millisecond
	e := engine(t, r, f, Options{AITurnDelay: delay})
	c := config()
	c.MaxHands = 1
	v, err := e.Create(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = e.Control(context.Background(), v.ID, "start"); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { r.mu.Lock(); defer r.mu.Unlock(); return len(r.actions) >= 1 })
	// 第一个 AI 动作已落库；停顿窗口内不得推进下一个 actor。
	time.Sleep(40 * time.Millisecond)
	r.mu.Lock()
	n := len(r.actions)
	r.mu.Unlock()
	if n != 1 {
		t.Fatalf("AI advanced during think pause: %d actions", n)
	}
	// 停顿结束后轮转继续。
	eventually(t, func() bool { r.mu.Lock(); defer r.mu.Unlock(); return len(r.actions) >= 2 })
}

// TestAITurnDelaySkippedForLiveMode 证明 live 模式用真实模型耗时，不加人工步进停顿：
// 第一个动作提交后不得进入 pausing，且第二个动作应立即推进（远快于 AITurnDelay）。
func TestAITurnDelaySkippedForLiveMode(t *testing.T) {
	r := newTestRepo()
	f := &testFactory{d: agent.NewMockDecider()}
	const delay = 300 * time.Millisecond
	e := engine(t, r, f, Options{AllowPaidModels: true, MaxLiveCalls: 100, AITurnDelay: delay})
	c := config()
	c.Mode = "live"
	c.MaxLiveCalls = 100
	c.MaxHands = 1
	v, err := e.Create(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = e.Control(context.Background(), v.ID, "start"); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { r.mu.Lock(); defer r.mu.Unlock(); return len(r.actions) >= 1 })
	// 给 decide 落库后置门闩的时间窗：live 必须不进入步进停顿。
	time.Sleep(50 * time.Millisecond)
	e.mu.Lock()
	pausing := e.pausing
	e.mu.Unlock()
	if pausing {
		t.Fatal("live mode entered think pause; should use real model latency only")
	}
	// live 动作应立即推进到第二个，不等待 delay。
	eventually(t, func() bool { r.mu.Lock(); defer r.mu.Unlock(); return len(r.actions) >= 2 })
}
