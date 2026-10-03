package service

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"os"
	"sort"
	"strings"
	"sync"
	"texas-holdem/internal/agent"
	"texas-holdem/internal/poker"
	"time"
	"unicode/utf8"
)

type Engine struct {
	mu            sync.Mutex
	repo          Repository
	factory       agent.Factory
	opts          Options
	ctx           context.Context
	cancel        context.CancelFunc
	wake          chan struct{}
	done          chan struct{}
	requestCancel context.CancelFunc
	activeRequest string
	// pausing 在 AI 步进停顿期间为 true，使 loop 不推进到下一个 actor；
	// pauseCancel 用于让 Control(pause/stop) 立即打断停顿。两者都在 e.mu 下访问。
	pausing     bool
	pauseCancel context.CancelFunc
	deciders    map[string]map[poker.Seat]agent.Decider
	fault       bool
	diag        func(code, stage, gameID string)
}

func New(ctx context.Context, repo Repository, factory agent.Factory, opts Options) (*Engine, error) {
	if opts.MaxLiveCalls == 0 {
		opts.MaxLiveCalls = 1000
	}
	if opts.DecisionTimeout <= 0 {
		opts.DecisionTimeout = 15 * time.Second
	}
	if opts.HumanActionTimeout < 0 {
		return nil, ErrInvalid
	}
	if opts.HumanActionTimeout == 0 {
		opts.HumanActionTimeout = 60 * time.Second
	}
	root, cancel := context.WithCancel(ctx)
	e := &Engine{repo: repo, factory: factory, opts: opts, ctx: root, cancel: cancel, wake: make(chan struct{}, 1), done: make(chan struct{}), deciders: make(map[string]map[poker.Seat]agent.Decider)}
	r, err := repo.Current(ctx)
	if err != nil && !errors.Is(err, ErrNotFound) {
		cancel()
		return nil, ErrStorage
	}
	if err == nil && r.View.Status == "running" {
		old := r.View.Revision
		r.View.Status = "paused"
		r.View.Reason = text("recovery")
		r.RequestID = ""
		// 原真人等待转为冻结等待：剩余时间取 max(0, 原截止时间 - 恢复时服务端时间)。
		if r.HumanWait != nil && r.HumanWait.Deadline != nil {
			rem := r.HumanWait.Deadline.Sub(time.Now())
			if rem < 0 {
				rem = 0
			}
			r.HumanWait.Deadline = nil
			r.HumanWait.Remaining = rem
		}
		r.View.Revision++
		if err = repo.Commit(ctx, Commit{Record: r, ExpectedRevision: old}); err != nil {
			cancel()
			return nil, ErrStorage
		}
	}
	go e.loop()
	return e, nil
}

func (e *Engine) Close() {
	e.cancel()
	e.mu.Lock()
	e.cancelRequest()
	e.cancelPauseLocked()
	e.mu.Unlock()
	<-e.done
}
func (e *Engine) notify() {
	select {
	case e.wake <- struct{}{}:
	default:
	}
}
func (e *Engine) cancelRequest() {
	if e.requestCancel != nil {
		e.requestCancel()
	}
	e.requestCancel = nil
	e.activeRequest = ""
}

// cancelPauseLocked 取消进行中的 AI 步进停顿并清除门闩，使控制命令立即生效；
// 调用方必须持有 e.mu。
func (e *Engine) cancelPauseLocked() {
	if e.pauseCancel != nil {
		e.pauseCancel()
		e.pauseCancel = nil
	}
	e.pausing = false
}

// 固定安全诊断码与阶段。绝不携带 provider 原始错误、SDK 文本或凭据。
const (
	diagStorage  = "BG_STORAGE_FAILED"
	diagTxn      = "BG_TXN_FAILED"
	diagSnapshot = "BG_SNAPSHOT_FAILED"
	diagEngine   = "BG_ENGINE_FAILED"
	diagRandom   = "BG_RANDOM_FAILED"
	diagModel    = "BG_MODEL_FAILED"
	diagAuth     = "BG_AUTH_DENIED"
)

func defaultDiag(code, stage, gameID string) {
	fmt.Fprintf(os.Stderr, "POKER_BACKGROUND_FAILURE code=%s stage=%s game=%s\n", code, stage, safeIdent(gameID))
}

// safeIdent 仅保留 uuid 级别安全字符，避免日志注入或意外泄露非游戏标识。
func safeIdent(s string) string {
	if s == "" {
		return "-"
	}
	if len(s) > 64 {
		s = s[:64]
	}
	for _, r := range s {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' {
			continue
		}
		return "-"
	}
	return s
}

func (e *Engine) reportDiag(code, stage, gameID string) {
	if e.diag != nil {
		e.diag(code, stage, gameID)
	}
}

// 同一故障锁存只记录一次，避免后台循环重复产生日志。
func (e *Engine) fail() { e.failWith(diagStorage, "storage", "") }
func (e *Engine) failWith(code, stage, gameID string) {
	if !e.fault {
		e.reportDiag(code, stage, gameID)
	}
	e.fault = true
	e.cancelRequest()
}

// txnStage 区分实例锁失效与一般事务失败；两者都仍是 fail-closed。
func txnStage(err error) string {
	if errors.Is(err, ErrInstanceLock) {
		return "instance_lock"
	}
	return "txn"
}

func (e *Engine) save(ctx context.Context, r Record, old uint64, action *ActionView, hand *CompletedHandView) error {
	err := e.repo.Commit(ctx, Commit{Record: r, ExpectedRevision: old, Action: action, Hand: hand})
	if err != nil {
		if errors.Is(err, ErrConflict) {
			return ErrConflict
		}
		e.failWith(diagTxn, txnStage(err), r.View.ID)
		return ErrStorage
	}
	// 终态已提交，实例不再被调度，可安全回收其 decider，避免内存泄漏。
	if terminal(r.View.Status) {
		delete(e.deciders, r.View.ID)
	}
	return nil
}
func uuid() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	b[6] = (b[6] & 15) | 64
	b[8] = (b[8] & 63) | 128
	h := hex.EncodeToString(b)
	return h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:], nil
}
func shuffle() (poker.Deck, error) {
	d := poker.StandardDeck()
	for i := len(d) - 1; i > 0; i-- {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(i+1)))
		if err != nil {
			return d, err
		}
		j := int(n.Int64())
		d[i], d[j] = d[j], d[i]
	}
	return d, nil
}

func (e *Engine) Models(ctx context.Context) ([]agent.ModelDescriptor, error) {
	if err := ctx.Err(); err != nil {
		return nil, ErrStorage
	}
	if e.factory == nil {
		return []agent.ModelDescriptor{{Ref: "mock", Label: "Mock", Provider: "mock", Availability: "mock"}}, nil
	}
	return e.factory.Models(), nil
}
func (e *Engine) validate(c *CreateGame) error {
	if c.Mode == "" {
		c.Mode = "mock"
	}
	if c.Mode != "mock" && c.Mode != "live" {
		return ErrInvalid
	}
	if c.Mode == "live" && !e.opts.AllowPaidModels {
		return ErrForbidden
	}
	if c.Mode == "mock" && c.MaxLiveCalls != 0 || c.Mode == "live" && (c.MaxLiveCalls == 0 || c.MaxLiveCalls > e.opts.MaxLiveCalls) {
		return ErrInvalid
	}
	if len(c.Seats) < 2 || len(c.Seats) > 9 || c.MaxHands == 0 || c.SmallBlind <= 0 || c.BigBlind <= c.SmallBlind || c.BigBlind > 1000000000000 {
		return ErrInvalid
	}
	seen := map[poker.Seat]bool{}
	total := poker.Chips(0)
	eligible := 0
	button := false
	humans := 0
	models := map[string]agent.ModelDescriptor{}
	if e.factory != nil {
		for _, m := range e.factory.Models() {
			models[m.Ref] = m
		}
	}
	for i := range c.Seats {
		s := &c.Seats[i]
		if s.Seat > 8 || seen[s.Seat] || !utf8.ValidString(s.Name) || strings.TrimSpace(s.Name) == "" || len(s.Name) > 128 || len(s.ModelRef) > 256 || s.Stack < 0 || s.Stack > 1000000000000-total {
			return ErrInvalid
		}
		// 旧记录/旧请求 Kind 为空按 ai 解释；显式未知值拒绝。
		if s.Kind == "" {
			s.Kind = SeatAI
		}
		switch s.Kind {
		case SeatHuman:
			humans++
			if humans > 1 || s.ModelRef != "" || s.Stack == 0 {
				return ErrInvalid
			}
		case SeatAI:
			if s.ModelRef == "" {
				return ErrInvalid
			}
			if c.Mode == "live" {
				m, ok := models[s.ModelRef]
				if !ok || m.Provider == "mock" || m.Availability == "unsupported" {
					return ErrInvalid
				}
			}
		default:
			return ErrInvalid
		}
		seen[s.Seat] = true
		total += s.Stack
		if s.Stack > 0 {
			eligible++
			if s.Seat == c.Button {
				button = true
			}
		}
	}
	if eligible < 2 || !button {
		return ErrInvalid
	}
	// 真人创建必须提供非零会话摘要；全 AI 创建不得提供。
	if humanSeat(*c) != nil {
		if c.PlayerSessionHash == nil || *c.PlayerSessionHash == (PlayerSessionHash{}) {
			return ErrInvalid
		}
	} else if c.PlayerSessionHash != nil {
		return ErrInvalid
	}
	sort.Slice(c.Seats, func(i, j int) bool { return c.Seats[i].Seat < c.Seats[j].Seat })
	return nil
}

// humanSeat 返回配置中唯一的真人座位；旧记录 Kind 为空按 ai 处理，因此不返回。
func humanSeat(c CreateGame) *poker.Seat {
	for i := range c.Seats {
		if c.Seats[i].Kind == SeatHuman {
			seat := c.Seats[i].Seat
			return &seat
		}
	}
	return nil
}
func (e *Engine) Create(ctx context.Context, c CreateGame) (GameView, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.fault {
		return GameView{}, ErrStorage
	}
	c.Seats = append([]SeatConfig(nil), c.Seats...)
	if err := e.validate(&c); err != nil {
		return GameView{}, err
	}
	id, err := uuid()
	if err != nil {
		return GameView{}, ErrStorage
	}
	v := GameView{ID: id, Revision: 1, Status: "created", Mode: c.Mode, MaxHands: c.MaxHands, MaxLiveCalls: c.MaxLiveCalls, Seats: []SeatView{}}
	for _, s := range c.Seats {
		v.Seats = append(v.Seats, SeatView{Seat: s.Seat, Name: s.Name, ModelRef: s.ModelRef, Stack: s.Stack, Kind: s.Kind})
	}
	if hs := humanSeat(c); hs != nil {
		v.HumanSeat = hs
	}
	if c.Mode == "live" {
		ds := make(map[poker.Seat]agent.Decider)
		for _, s := range c.Seats {
			// 真人不创建 Decider、不消费 live 额度、不更新模型可用性。
			if s.Kind == SeatHuman {
				continue
			}
			d, err := e.factory.NewDecider(s.ModelRef)
			if err != nil {
				return GameView{}, ErrInvalid
			}
			ds[s.Seat] = d
		}
		e.deciders[id] = ds
	}
	// 会话摘要规范化到私有 Record，并从创建输入中清除，避免 Config 重复保存。
	session := c.PlayerSessionHash
	c.PlayerSessionHash = nil
	rec := Record{Config: c, View: v, HumanActionTimeout: e.opts.HumanActionTimeout}
	if session != nil {
		h := *session
		rec.HumanSessionHash = &h
	}
	if err = e.repo.Create(ctx, rec); err != nil {
		delete(e.deciders, id)
		if errors.Is(err, ErrConflict) {
			return GameView{}, ErrConflict
		}
		e.failWith(diagTxn, txnStage(err), id)
		return GameView{}, ErrStorage
	}
	return e.view(ctx, v)
}
func (e *Engine) view(ctx context.Context, v GameView) (GameView, error) {
	h, err := e.repo.Health(ctx)
	if callerReadCanceled(ctx, err) && h.MySQL != "unavailable" {
		return GameView{}, ErrStorage
	}
	if h.MySQL != "available" || err != nil && h.Redis == "available" {
		e.fail()
		return GameView{}, ErrStorage
	}
	v.StorageDegraded = err != nil || h.StorageDegraded || h.Redis != "available"
	return v, nil
}
func (e *Engine) Get(ctx context.Context, id string) (GameView, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.fault {
		return GameView{}, ErrStorage
	}
	r, err := e.repo.Get(ctx, id)
	if err != nil {
		if !errors.Is(err, ErrNotFound) && !callerReadCanceled(ctx, err) {
			e.fail()
		}
		return GameView{}, safeRepo(err)
	}
	return e.view(ctx, r.View)
}
func safeRepo(err error) error {
	if errors.Is(err, ErrNotFound) {
		return ErrNotFound
	}
	if errors.Is(err, ErrConflict) {
		return ErrConflict
	}
	return ErrStorage
}

// 仅匹配调用方取消的只读错误可免于锁存；独立依赖超时、存储错误仍 fail-closed。
func callerReadCanceled(ctx context.Context, err error) bool {
	cause := ctx.Err()
	return cause != nil && !errors.Is(err, ErrStorage) && errors.Is(err, cause)
}

func (e *Engine) Current(ctx context.Context) (GameView, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.fault {
		return GameView{}, ErrStorage
	}
	r, err := e.repo.Current(ctx)
	if err != nil {
		if !errors.Is(err, ErrNotFound) && !callerReadCanceled(ctx, err) {
			e.fail()
		}
		return GameView{}, safeRepo(err)
	}
	return e.view(ctx, r.View)
}
func (e *Engine) GetHand(ctx context.Context, id string, n uint32) (CompletedHandView, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.fault {
		return CompletedHandView{}, ErrStorage
	}
	r, err := e.repo.Get(ctx, id)
	if err != nil {
		if !errors.Is(err, ErrNotFound) && !callerReadCanceled(ctx, err) {
			e.fail()
		}
		return CompletedHandView{}, safeRepo(err)
	}
	if n == 0 {
		return CompletedHandView{}, ErrInvalid
	}
	if n > r.View.HandsCompleted {
		if r.View.Hand != nil && r.View.Hand.Number == n {
			return CompletedHandView{}, ErrConflict
		}
		return CompletedHandView{}, ErrNotFound
	}
	h, err := e.repo.Hand(ctx, id, n)
	if err != nil {
		if !errors.Is(err, ErrNotFound) && !callerReadCanceled(ctx, err) {
			e.fail()
		}
		return CompletedHandView{}, safeRepo(err)
	}
	return h, nil
}
func (e *Engine) Health(ctx context.Context) (HealthView, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	h, err := e.repo.Health(ctx)
	h.AllowPaidModels = e.opts.AllowPaidModels
	h.MaxLiveCalls = e.opts.MaxLiveCalls
	if callerReadCanceled(ctx, err) && h.MySQL != "unavailable" && !e.fault {
		return h, ErrStorage
	}
	if h.MySQL != "available" || err != nil && h.Redis == "available" {
		e.fail()
	}
	if e.fault {
		h.MySQL = "unavailable"
		h.StorageDegraded = true
		return h, ErrStorage
	}
	if err != nil || h.MySQL != "available" || h.Redis != "available" {
		return h, ErrStorage
	}
	return h, nil
}

// controlTarget 返回命令的目标状态与在当前记录下是否合法（幂等由状态相等判断）。
func controlTarget(cmd Command, v GameView) (string, bool) {
	switch cmd {
	case "start":
		return "running", v.Status == "created"
	case "resume":
		return "running", v.Status == "paused" && (v.Reason == nil || *v.Reason != "cost_limit")
	case "pause":
		return "paused", v.Status == "running"
	case "stop":
		return "stopped", !terminal(v.Status)
	default:
		return "", false
	}
}

// humanWaitExpired 报告当前是否为已到截止时间、尚未提交的真人等待点。
func humanWaitExpired(r Record) bool {
	return r.View.Status == "running" && humanSeat(r.Config) != nil && r.HumanWait != nil &&
		r.HumanWait.Deadline != nil && !time.Now().Before(*r.HumanWait.Deadline)
}

func (e *Engine) Control(ctx context.Context, id string, cmd Command) (GameView, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.fault {
		return GameView{}, ErrStorage
	}
	r, err := e.repo.Get(ctx, id)
	if err != nil {
		if !errors.Is(err, ErrNotFound) {
			e.fail()
		}
		return GameView{}, safeRepo(err)
	}
	switch cmd {
	case "start", "resume", "pause", "stop":
	default:
		return GameView{}, ErrInvalid
	}
	// 截止后的 pause/resume 不能救回已超时行动：先走统一超时弃牌链路，再按新状态处理。
	if (cmd == "pause" || cmd == "resume") && humanWaitExpired(r) {
		seat := *humanSeat(r.Config)
		if err := e.submit(ctx, &r, r.View.Revision, seat, poker.Action{Kind: "fold"}, "fallback", text("HUMAN_ACTION_TIMEOUT")); err != nil {
			return GameView{}, err
		}
	}
	target, valid := controlTarget(cmd, r.View)
	if r.View.Status == target {
		return e.view(ctx, r.View)
	}
	if !valid {
		return GameView{}, ErrConflict
	}
	// 运行授权可能在进程生命周期内被关闭；每次进入 running 前重新校验，
	// 不把授权变化当成存储故障，也不允许旧 live 局绕过开关。
	if (cmd == "start" || cmd == "resume") && r.View.Mode == "live" && !e.opts.AllowPaidModels {
		return GameView{}, ErrForbidden
	}
	e.cancelRequest()
	// 立即打断进行中的 AI 步进停顿，避免 pause/stop/resume 等待停顿自然结束。
	e.cancelPauseLocked()
	old := r.View.Revision
	if cmd == "pause" && r.HumanWait != nil && r.HumanWait.Deadline != nil {
		// 暂停冻结剩余时长：清空 Deadline，不弃牌、不结算。
		rem := r.HumanWait.Deadline.Sub(time.Now())
		if rem < 0 {
			rem = 0
		}
		r.HumanWait.Deadline = nil
		r.HumanWait.Remaining = rem
	}
	if cmd == "start" || cmd == "resume" {
		// 恢复冻结等待：以剩余时长生成新 Deadline，不重新赠送完整时长。
		if r.HumanWait != nil && r.HumanWait.Deadline == nil {
			d := time.Now().Add(r.HumanWait.Remaining)
			r.HumanWait.Deadline = &d
		}
	}
	if cmd == "stop" {
		r.HumanWait = nil
		if r.View.Hand != nil {
			r.View.Hand.WaitingFor = nil
		}
	}
	r.View.Revision++
	r.View.Status = target
	r.View.Reason = nil
	r.RequestID = ""
	if err = e.save(ctx, r, old, nil, nil); err != nil {
		return GameView{}, err
	}
	e.notify()
	return e.view(ctx, r.View)
}

// authorized 恒定时间比较会话摘要；游戏无真人或摘要为空一律拒绝。
func authorized(r Record, hash PlayerSessionHash) bool {
	if humanSeat(r.Config) == nil || r.HumanSessionHash == nil {
		return false
	}
	return subtle.ConstantTimeCompare(r.HumanSessionHash[:], hash[:]) == 1
}

// playerView 从私有 Record 投影本人视角：公共 GameView + 自己的已发底牌 +
// 仅当前等待点提供合法动作与截止时间。绝不返回其他座位私牌。
func (e *Engine) playerView(ctx context.Context, r Record, seat poker.Seat) (PlayerGameView, error) {
	g, err := e.view(ctx, r.View)
	if err != nil {
		return PlayerGameView{}, err
	}
	pv := PlayerView{Seat: seat, HoleCards: []poker.Card{}}
	if r.View.Hand != nil {
		n := r.View.Hand.Number
		pv.HandNo = &n
	}
	if len(r.Snapshot) > 0 {
		s, derr := poker.DecodeState(r.Snapshot)
		if derr == nil {
			if cards, herr := poker.PrivateHole(s, seat); herr == nil {
				pv.HoleCards = []poker.Card{cards[0], cards[1]}
			}
			if r.View.Status == "running" && r.HumanWait != nil && r.HumanWait.Seat == seat && r.HumanWait.Deadline != nil {
				if l, lerr := poker.Legal(s, seat); lerr == nil {
					la := l
					pv.LegalActions = &la
					d := *r.HumanWait.Deadline
					pv.ActionDeadline = &d
				}
			}
		}
	}
	return PlayerGameView{Game: g, Player: pv, ServerTime: time.Now().UTC()}, nil
}

func (e *Engine) GetPlayer(ctx context.Context, id string, hash PlayerSessionHash) (PlayerGameView, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.fault {
		return PlayerGameView{}, ErrStorage
	}
	r, err := e.repo.Get(ctx, id)
	if err != nil {
		if !errors.Is(err, ErrNotFound) && !callerReadCanceled(ctx, err) {
			e.fail()
		}
		return PlayerGameView{}, safeRepo(err)
	}
	if !authorized(r, hash) {
		return PlayerGameView{}, ErrPlayerUnauthorized
	}
	return e.playerView(ctx, r, *humanSeat(r.Config))
}

// Act 在当前串行边界内校验会话、revision、等待点与截止时间后提交真人动作。
func (e *Engine) Act(ctx context.Context, id string, hash PlayerSessionHash, ha HumanAction) (PlayerGameView, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.fault {
		return PlayerGameView{}, ErrStorage
	}
	r, err := e.repo.Get(ctx, id)
	if err != nil {
		if !errors.Is(err, ErrNotFound) && !callerReadCanceled(ctx, err) {
			e.fail()
		}
		return PlayerGameView{}, safeRepo(err)
	}
	if !authorized(r, hash) {
		return PlayerGameView{}, ErrPlayerUnauthorized
	}
	hs := humanSeat(r.Config)
	if r.View.Status != "running" {
		return PlayerGameView{}, ErrConflict
	}
	if ha.ExpectedRevision == 0 {
		return PlayerGameView{}, ErrInvalid
	}
	// 超时/跨手/双击都会先在这里因 revision 过期被拒（超时提交后 revision 已 +1）。
	if ha.ExpectedRevision != r.View.Revision {
		return PlayerGameView{}, ErrStaleAction
	}
	if r.View.Hand == nil || r.HumanWait == nil || r.HumanWait.Seat != *hs || r.HumanWait.HandNo != r.View.Hand.Number || r.HumanWait.Deadline == nil {
		return PlayerGameView{}, ErrConflict
	}
	// 截止已过但 worker 尚未处理：以权威超时链路提交弃牌，拒绝该动作。
	if !time.Now().Before(*r.HumanWait.Deadline) {
		if err := e.submit(ctx, &r, r.View.Revision, *hs, poker.Action{Kind: "fold"}, "fallback", text("HUMAN_ACTION_TIMEOUT")); err != nil {
			return PlayerGameView{}, err
		}
		e.notify()
		return PlayerGameView{}, ErrActionExpired
	}
	if err := e.submit(ctx, &r, r.View.Revision, *hs, ha.Action, "human", nil); err != nil {
		return PlayerGameView{}, err
	}
	e.notify()
	return e.playerView(ctx, r, *hs)
}
