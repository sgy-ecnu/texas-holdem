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
		// 权威手间停留窗口：上一手结束后、最早开始时间未到时不开新一手，
		// 也不进入后续真人等待分支创建新手倒计时。时间基于持久化的绝对时刻，
		// 因此 pause/resume 与服务重启都不会绕过剩余窗口。
		if r.NextHandAt != nil && time.Now().Before(*r.NextHandAt) {
			return
		}
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
		// 新手已开：清除上一手遗留的手间窗口。
		r.NextHandAt = nil
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
	// 统一判定：真实调用 = mode==live 且当前座位引用非保留值 "mock"。
	// mock 引用即使处于 live 局也不消耗额度、不因额度耗尽暂停、不触发付费授权门槛。
	realCall := isRealCall(r.Config, obs.Self)
	// 只有真实模型请求受额度约束：额度耗尽时轮到 mock 或真人仍继续决策，
	// 绝不把真实模型自动降级为 mock。check 真人分支已在上面优先后返回。
	if realCall && r.View.LiveCallsUsed >= r.Config.MaxLiveCalls {
		r.View.Status = "paused"
		r.View.Reason = text("cost_limit")
		r.View.Revision++
		r.RequestID = ""
		_ = e.save(e.ctx, r, old, nil, nil)
		return
	}
	// 保留 live 模式运行授权复核：授权在运行中被关闭则暂停等待人工恢复，
	// 绝不锁存为全局存储故障；与 Control 的 start/resume 授权复核一致。
	if r.Config.Mode == "live" && !e.opts.AllowPaidModels {
		e.reportDiag(diagAuth, "authorization", r.View.ID)
		r.View.Status = "paused"
		r.View.Reason = text("unauthorized")
		r.View.Revision++
		r.RequestID = ""
		_ = e.save(e.ctx, r, old, nil, nil)
		return
	}
	// 执行器：mock 引用直接 MockDecider；真实引用才走 factory，并在请求前预扣额度。
	d := agent.NewMockDecider()
	if realCall {
		if e.factory == nil {
			e.failWith(diagModel, "model", r.View.ID)
			return
		}
		if e.deciders[r.View.ID] == nil {
			e.deciders[r.View.ID] = map[poker.Seat]agent.Decider{}
		}
		d = e.deciders[r.View.ID][obs.Self]
		if d == nil {
			d, err = e.factory.NewDecider(effectiveModelRef(r.Config, obs.Self))
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

// actionAmount 计算动作本次的实际投入额，仅依据动作前的权威状态：
// fold/check 为 0；call 为本次补差跟注额（Legal 的 CallAmount）；
// raise_to 为本次追加额（action.To 减去该座位动作前本街 StreetBet）。
// 不改变 poker 引擎 Apply/Legal 语义。
func actionAmount(s poker.State, seat poker.Seat, action poker.Action) poker.Chips {
	switch action.Kind {
	case "call":
		if l, err := poker.Legal(s, seat); err == nil {
			return l.CallAmount
		}
		return 0
	case "raise_to":
		for _, p := range poker.Public(s).Players {
			if p.Seat == seat {
				return action.To - p.StreetBet
			}
		}
		return 0
	default:
		return 0
	}
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
	a := ActionView{HandNo: r.View.Hand.Number, Sequence: r.Sequence, Seat: seat, Kind: action.Kind, To: action.To, Amount: actionAmount(s, seat, action), Source: source, ErrorCode: code}
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
	// 只有落库的 mock 执行器动作才进入步进停顿：live 真实模型用真实耗时，不额外加人工停顿。
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

// commitDecision 在锁内校验并原子落库当前 AI 决策；返回动作是否已提交，以及本次实际执行器
// 是否为 mock（仅 mock 执行器需要模拟步进停顿，live 真实模型用真实耗时）。
// ActionView.Source 也按实际执行器判定：mock 执行器（含 live 局中的 mock 引用）标 "mock"，
// 仅真实模型请求标 "model"，真实请求失败回退标 "fallback" 且已消耗的额度保留。
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
	// 执行器语义：仅真实模型请求标 "model"；mock 座位（含 live 局中的 mock 引用）标 "mock"。
	source := "model"
	if !isRealCall(r.Config, obs.Self) {
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
	a := ActionView{HandNo: r.View.Hand.Number, Sequence: r.Sequence, Seat: obs.Self, Kind: action.Kind, To: action.To, Amount: actionAmount(s, obs.Self, action), Source: source, ErrorCode: code, InputTokens: result.decision.InputTokens, OutputTokens: result.decision.OutputTokens}
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
	return saved, saved && !isRealCall(r.Config, obs.Self)
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
	// 本手后不再开新手：终局不设置窗口，避免残留过期时间。
	if r.View.HandsCompleted >= r.Config.MaxHands || alive < 2 {
		r.View.Status = "completed"
		r.NextHandAt = nil
		return
	}
	// 设置权威手间停留窗口：摊牌/弃牌结束使用不同时长；未启用（<=0）时不留窗口。
	if d := e.handInterval(result.Reason); d > 0 {
		next := time.Now().Add(d)
		r.NextHandAt = &next
	} else {
		r.NextHandAt = nil
	}
}

// handInterval 返回本手结束后应停留的时长；零值表示不启用窗口。
func (e *Engine) handInterval(reason string) time.Duration {
	if reason == "showdown" {
		return e.opts.HandIntervalShowdown
	}
	return e.opts.HandIntervalUncontested
}
func completed(r Record, result poker.HandResult) CompletedHandView {
	return CompletedHandView{GameID: r.View.ID, HandNo: r.View.Hand.Number, Button: r.View.Hand.State.Button, Board: append([]poker.Card(nil), r.View.Hand.State.Board...), Result: result}
}
