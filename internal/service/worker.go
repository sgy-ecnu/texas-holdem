package service

import (
	"context"
	"errors"
	"texas-holdem/internal/agent"
	"texas-holdem/internal/poker"
	"time"
)

func (e *Engine) loop() {
	defer close(e.done)
	tick := time.NewTicker(25 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-e.ctx.Done():
			return
		case <-e.wake:
			e.step()
		case <-tick.C:
			e.step()
		}
	}
}
func (e *Engine) step() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.ctx.Err() != nil || e.fault || e.activeRequest != "" || e.pausing {
		return
	}
	r, err := e.repo.Current(e.ctx)
	if errors.Is(err, ErrNotFound) {
		return
	}
	if err != nil {
		e.failWith(diagStorage, txnStage(err), "")
		return
	}
	if r.View.Status != "running" {
		return
	}
	old := r.View.Revision
	var s poker.State
	if len(r.Snapshot) == 0 || r.View.Hand != nil && r.View.Hand.State.Street == "complete" {
		players := []poker.PlayerInit{}
		for _, p := range r.View.Seats {
			if p.Stack > 0 {
				players = append(players, poker.PlayerInit{Seat: p.Seat, Stack: p.Stack})
			}
		}
		if len(players) < 2 || r.View.HandsCompleted >= r.Config.MaxHands {
			r.View.Status = "completed"
			r.View.Revision++
			_ = e.save(e.ctx, r, old, nil, nil)
			return
		}
		button := r.Config.Button
		if r.View.Hand != nil {
			previous := r.View.Hand.State.Button
			button = players[0].Seat
			for _, p := range players {
				if p.Seat > previous {
					button = p.Seat
					break
				}
			}
		}
		deck, derr := shuffle()
		if derr != nil {
			e.failWith(diagRandom, "random", r.View.ID)
			return
		}
		s, err = poker.NewHand(poker.HandConfig{Players: players, Button: button, SmallBlind: r.Config.SmallBlind, BigBlind: r.Config.BigBlind}, deck)
		if err != nil {
			e.failWith(diagEngine, "engine", r.View.ID)
			return
		}
		r.View.Hand = &HandView{Number: r.View.HandsCompleted + 1, State: poker.Public(s)}
		r.Snapshot, err = poker.EncodeState(s)
		if err != nil {
			e.failWith(diagSnapshot, "snapshot", r.View.ID)
			return
		}
		r.View.Revision++
		if result, ok := poker.Result(s); ok {
			e.settle(&r, result)
			hand := completed(r, result)
			_ = e.save(e.ctx, r, old, nil, &hand)
			return
		}
		if e.save(e.ctx, r, old, nil, nil) != nil {
			return
		}
		old = r.View.Revision
	} else {
		s, err = poker.DecodeState(r.Snapshot)
		if err != nil {
			e.failWith(diagSnapshot, "snapshot", r.View.ID)
			return
		}
	}
	public := poker.Public(s)
	if public.Actor == nil {
		e.failWith(diagEngine, "engine", r.View.ID)
		return
	}
	// 真人分支必须先于 live 额度与模型逻辑：额度耗尽不能阻止真人完成已轮到的行动。
	if hs := humanSeat(r.Config); hs != nil && *public.Actor == *hs {
		if r.HumanWait != nil && r.HumanWait.HandNo == r.View.Hand.Number && r.HumanWait.Seat == *hs {
			if r.HumanWait.Deadline != nil && !time.Now().Before(*r.HumanWait.Deadline) {
				// 超时一律弃牌，不复用 AI fallback 的 check 选择。
				if err := e.submit(e.ctx, &r, old, *hs, poker.Action{Kind: "fold"}, "fallback", text("HUMAN_ACTION_TIMEOUT")); err != nil {
					return
				}
				e.notify()
			}
			// 尚未截止：不推进、不请求模型、不反复写库。
			return
		}
		timeout := r.HumanActionTimeout
		if timeout <= 0 {
			timeout = e.opts.HumanActionTimeout
		}
		deadline := time.Now().Add(timeout)
		r.HumanWait = &HumanWait{HandNo: r.View.Hand.Number, Seat: *hs, Deadline: &deadline}
		r.View.Hand.WaitingFor = text("human_action")
		r.View.Revision++
		_ = e.save(e.ctx, r, old, nil, nil)
		return
	}
	obs, err := poker.Observe(s, *public.Actor)
	if err != nil {
		e.failWith(diagEngine, "engine", r.View.ID)
		return
	}
	if r.Config.Mode == "live" && r.View.LiveCallsUsed >= r.Config.MaxLiveCalls {
		r.View.Status = "paused"
		r.View.Reason = text("cost_limit")
		r.View.Revision++
		r.RequestID = ""
		_ = e.save(e.ctx, r, old, nil, nil)
		return
	}
	d := agent.NewMockDecider()
	if r.Config.Mode == "live" {
		if !e.opts.AllowPaidModels {
			// 授权在运行中被关闭：记录安全码并暂停等待人工恢复，绝不锁存为全局存储故障。
			e.reportDiag(diagAuth, "authorization", r.View.ID)
			r.View.Status = "paused"
			r.View.Reason = text("unauthorized")
			r.View.Revision++
			r.RequestID = ""
			_ = e.save(e.ctx, r, old, nil, nil)
			return
		}
		if e.factory == nil {
			e.failWith(diagModel, "model", r.View.ID)
			return
		}
		if e.deciders[r.View.ID] == nil {
			e.deciders[r.View.ID] = map[poker.Seat]agent.Decider{}
		}
		d = e.deciders[r.View.ID][obs.Self]
		if d == nil {
			ref := ""
			for _, p := range r.Config.Seats {
				if p.Seat == obs.Self {
					ref = p.ModelRef
				}
			}
			d, err = e.factory.NewDecider(ref)
			if err != nil {
				e.failWith(diagModel, "model", r.View.ID)
				return
			}
			e.deciders[r.View.ID][obs.Self] = d
		}
		r.View.LiveCallsUsed++
	}
	id, err := uuid()
	if err != nil {
		e.failWith(diagRandom, "random", r.View.ID)
		return
	}
	r.RequestID = id
	r.View.Revision++
	if e.save(e.ctx, r, old, nil, nil) != nil {
		return
	}
	ctx, cancel := context.WithTimeout(e.ctx, e.opts.DecisionTimeout)
	e.requestCancel = cancel
	e.activeRequest = id
	go e.decide(ctx, d, obs, r.View.ID, r.View.Revision, id)
}

// submit 在锁内以统一链路提交一个（已确认合法的）动作：解码快照、Apply、
// 原子提交 revision/快照/动作/必要结算，并清除真人等待点。Apply 失败返回
// ErrIllegalAction 且不改变任何状态、不重置时限。
func (e *Engine) submit(ctx context.Context, r *Record, old uint64, seat poker.Seat, action poker.Action, source string, code *string) error {
	s, err := poker.DecodeState(r.Snapshot)
	if err != nil {
		e.failWith(diagSnapshot, "snapshot", r.View.ID)
		return ErrStorage
	}
	next, err := poker.Apply(s, seat, action)
	if err != nil {
		return ErrIllegalAction
	}
	r.Sequence++
	r.View.Revision++
	r.RequestID = ""
	r.HumanWait = nil
	if r.View.Hand != nil {
		r.View.Hand.WaitingFor = nil
	}
	a := ActionView{HandNo: r.View.Hand.Number, Sequence: r.Sequence, Seat: seat, Kind: action.Kind, To: action.To, Source: source, ErrorCode: code}
	r.View.LastAction = &a
	r.View.Hand.State = poker.Public(next)
	r.Snapshot, err = poker.EncodeState(next)
	if err != nil {
		e.failWith(diagSnapshot, "snapshot", r.View.ID)
		return ErrStorage
	}
	var hand *CompletedHandView
	if result, ok := poker.Result(next); ok {
		e.settle(r, result)
		h := completed(*r, result)
		hand = &h
	}
	return e.save(ctx, *r, old, &a, hand)
}

type decisionResult struct {
	decision agent.Decision
	err      error
}

func (e *Engine) decide(ctx context.Context, d agent.Decider, obs poker.Observation, gameID string, revision uint64, id string) {
	ch := make(chan decisionResult, 1)
	go func() { v, err := d.Decide(ctx, obs); ch <- decisionResult{v, err} }()
	var result decisionResult
	select {
	case <-ctx.Done():
		result.err = ctx.Err()
	case result = <-ch:
	}
	e.mu.Lock()
	saved, isMock := e.commitDecision(&result, obs, gameID, revision, id)
	// 只有落库的 mock 动作才进入步进停顿：live 用真实模型耗时，不额外加人工停顿。
	// 停顿不持锁，pause/stop 可立即取消。
	var pauseCtx context.Context
	if saved && isMock && e.opts.AITurnDelay > 0 {
		pauseCtx, e.pauseCancel = context.WithCancel(e.ctx)
		e.pausing = true
	}
	e.mu.Unlock()
	if !saved {
		return
	}
	if pauseCtx != nil {
		thinkPause(pauseCtx, e.opts.AITurnDelay)
		e.mu.Lock()
		e.cancelPauseLocked()
		e.mu.Unlock()
	}
	e.notify()
}

// commitDecision 在锁内校验并原子落库当前 AI 决策；返回动作是否已提交，以及本局是否为
// mock 模式（仅 mock 需要模拟步进停顿，live 用真实模型耗时）。
func (e *Engine) commitDecision(result *decisionResult, obs poker.Observation, gameID string, revision uint64, id string) (bool, bool) {
	if e.ctx.Err() != nil || e.fault || e.activeRequest != id {
		return false, false
	}
	e.cancelRequest()
	r, err := e.repo.Get(e.ctx, gameID)
	if err != nil {
		e.failWith(diagStorage, txnStage(err), gameID)
		return false, false
	}
	if r.View.Status != "running" || r.View.Revision != revision || r.RequestID != id {
		return false, false
	}
	s, err := poker.DecodeState(r.Snapshot)
	if err != nil {
		e.failWith(diagSnapshot, "snapshot", gameID)
		return false, false
	}
	action := result.decision.Action
	source := "model"
	if r.Config.Mode == "mock" {
		source = "mock"
	}
	var code *string
	var next poker.State
	if result.err == nil {
		if action.Kind != "raise_to" && action.To != 0 {
			err = ErrInvalid
		} else {
			next, err = poker.Apply(s, obs.Self, action)
		}
	} else {
		err = result.err
	}
	if err != nil {
		code = text("MODEL_INVALID_ACTION")
		if result.err != nil {
			code = text(modelErrorCode(result.err))
		}
		action = poker.Action{Kind: "fold"}
		if obs.Legal.CanCheck {
			action.Kind = "check"
		}
		source = "fallback"
		next, err = poker.Apply(s, obs.Self, action)
		if err != nil {
			e.failWith(diagEngine, "engine", gameID)
			return false, false
		}
	}
	if result.decision.InputTokens != nil && *result.decision.InputTokens < 0 {
		result.decision.InputTokens = nil
	}
	if result.decision.OutputTokens != nil && *result.decision.OutputTokens < 0 {
		result.decision.OutputTokens = nil
	}
	r.Sequence++
	r.View.Revision++
	r.RequestID = ""
	a := ActionView{HandNo: r.View.Hand.Number, Sequence: r.Sequence, Seat: obs.Self, Kind: action.Kind, To: action.To, Source: source, ErrorCode: code, InputTokens: result.decision.InputTokens, OutputTokens: result.decision.OutputTokens}
	r.View.LastAction = &a
	r.View.Hand.State = poker.Public(next)
	r.Snapshot, err = poker.EncodeState(next)
	if err != nil {
		e.failWith(diagSnapshot, "snapshot", gameID)
		return false, false
	}
	var hand *CompletedHandView
	if result, ok := poker.Result(next); ok {
		e.settle(&r, result)
		h := completed(r, result)
		hand = &h
	}
	saved := e.save(e.ctx, r, revision, &a, hand) == nil
	return saved, saved && r.Config.Mode == "mock"
}

// thinkPause 在 AI 动作提交之后、推进下一个 actor 之前插入可取消的「思考停顿」，
// 让轮转节奏对人眼可见。使用 select + ctx.Done() + time.After：ctx 取消
// （pause/stop/Close）时立即返回，绝不阻塞控制命令。delay<=0 时不等待，
// 因此测试与关闭节奏的部署不受影响。
func thinkPause(ctx context.Context, delay time.Duration) {
	if delay <= 0 {
		return
	}
	select {
	case <-ctx.Done():
	case <-time.After(delay):
	}
}

// 只允许固定安全码。绝不传播 SDK、网络或 provider 原始错误文本。
func modelErrorCode(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "MODEL_TIMEOUT"
	}
	switch err.Error() {
	case "MODEL_TIMEOUT", "MODEL_AUTH_FAILED", "MODEL_INVALID_ACTION", "MODEL_PROTOCOL_ERROR", "MODEL_CANCELED", "MODEL_REQUEST_FAILED", "MODEL_HTTP_ERROR", "MODEL_BODY_READ_FAILED", "MODEL_BODY_TOO_LARGE", "MODEL_REQUEST_INVALID":
		return err.Error()
	default:
		return "MODEL_PROTOCOL_ERROR"
	}
}
func (e *Engine) settle(r *Record, result poker.HandResult) {
	r.View.HandsCompleted++
	alive := 0
	for i := range r.View.Seats {
		p := &r.View.Seats[i]
		if stack, ok := result.FinalStack[p.Seat]; ok {
			p.Stack = stack
		}
		if p.Stack > 0 {
			alive++
		}
	}
	if r.View.HandsCompleted >= r.Config.MaxHands || alive < 2 {
		r.View.Status = "completed"
	}
}
func completed(r Record, result poker.HandResult) CompletedHandView {
	return CompletedHandView{GameID: r.View.ID, HandNo: r.View.Hand.Number, Button: r.View.Hand.State.Button, Board: append([]poker.Card(nil), r.View.Hand.State.Board...), Result: result}
}
