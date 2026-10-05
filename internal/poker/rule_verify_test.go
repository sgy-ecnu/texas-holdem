package poker

import (
	"reflect"
	"testing"
)

// 本文件按架构师规则矩阵第 10 节“优先验证顺序”补独立 case，不改动现有测试。
// 每个 case 都有明确输入与期望，独立断言；发现引擎违反德州规则则修引擎，
// 若只是断言/期望写错则修测试。

// finishStreet 只把当前街行动跑到换街（或终局），不会跨街继续行动。
func finishStreet(t *testing.T, s State) State {
	t.Helper()
	start := Public(s).Street
	for i := 0; i < 100; i++ {
		p := Public(s)
		if p.Actor == nil || p.Street != start {
			return s
		}
		l, err := Legal(s, *p.Actor)
		if err != nil {
			t.Fatal(err)
		}
		k := ActionKind("call")
		if l.CanCheck {
			k = "check"
		}
		s = act(t, s, k, 0)
	}
	t.Fatal("单街行动未结束")
	return s
}

// showdownCards 构造可控摊牌：指定各座累计投入、弃牌集合、按钮、公共牌与底牌，
// 再以 showdown 结算。用于独立验证池分层与不同牌力赢家。
func showdownCards(t *testing.T, bets []Chips, folded map[int]bool, button Seat, board []Card, holes map[int][2]Card) State {
	t.Helper()
	stacks := make([]Chips, len(bets))
	for i := range stacks {
		stacks[i] = 100
	}
	s := hand(t, stacks...)
	s.button = button
	s.board = append([]Card{}, board...)
	s.street = "river"
	s.total = 0
	for i, n := range bets {
		s.players[i].Stack = 0
		s.players[i].Committed = n
		s.players[i].StreetBet = n
		s.players[i].Status = "all_in"
		if folded[i] {
			s.players[i].Status = "folded"
		}
		s.total += n
	}
	for seat, hc := range holes {
		s.hole[seat] = hc
	}
	s.settle("showdown")
	return s
}

func containsSeat(seats []Seat, target Seat) bool {
	for _, s := range seats {
		if s == target {
			return true
		}
	}
	return false
}

func five(a, b, c, d, e string) [5]Card {
	return [5]Card{Card(a), Card(b), Card(c), Card(d), Card(e)}
}

// ---------------------------------------------------------------------------
// 优先级 1：有人 bet/raise 后，后位以及返回的前位 check 必须拒绝。
// 直接回应用户怀疑的“前面下注后面还能 check”。
// ---------------------------------------------------------------------------

func TestCheckDeniedAfterBetOrRaise(t *testing.T) {
	t.Run("later_player_cannot_check_after_bet", func(t *testing.T) {
		s := finishStreet(t, hand(t, 100, 100, 100)) // 翻牌，actor=1
		if got := *Public(s).Actor; got != 1 {
			t.Fatalf("翻牌首位 actor got %d want 1", got)
		}
		s = act(t, s, "raise_to", 20) // 座1 下注 20
		l, err := Legal(s, 2)
		if err != nil {
			t.Fatal(err)
		}
		if l.CanCheck || !l.CanCall || l.CallAmount != 20 {
			t.Fatalf("后位面对下注不能 check 且应可 call20: %+v", l)
		}
		if _, e := Apply(s, 2, Action{Kind: "check"}); e == nil {
			t.Fatal("后位面对下注的 check 必须被拒绝")
		}
	})

	t.Run("returning_checker_cannot_check_after_later_bet", func(t *testing.T) {
		// 关键回归：A check → B bet20 → C call20 → 返回 A，A 不能因“之前 check 过”再次 check。
		s := finishStreet(t, hand(t, 100, 100, 100)) // 翻牌，actor=1
		s = act(t, s, "check", 0)                    // A=座1 check
		s = act(t, s, "raise_to", 20)                // B=座2 bet20
		s = act(t, s, "call", 0)                     // C=座0 call20
		p := Public(s)
		if p.Street != "flop" || p.Actor == nil || *p.Actor != 1 {
			t.Fatalf("应回到前位座1且仍在翻牌: %+v", p)
		}
		l, err := Legal(s, 1)
		if err != nil {
			t.Fatal(err)
		}
		if l.CanCheck || !l.CanCall || l.CallAmount != 20 {
			t.Fatalf("返回的前位欠注20，不能 check: %+v", l)
		}
		if _, e := Apply(s, 1, Action{Kind: "check"}); e == nil {
			t.Fatal("面对后来下注的前位 check 必须被拒绝")
		}
	})

	t.Run("check_denied_after_full_raise", func(t *testing.T) {
		// 已跟20后对手加注到50，返回时欠30，check 拒绝。
		s := finishStreet(t, hand(t, 100, 100, 100)) // 翻牌 actor=1
		s = act(t, s, "raise_to", 20)                // 座1 bet20
		s = act(t, s, "call", 0)                     // 座2 call20
		s = act(t, s, "raise_to", 50)                // 座0 raise50
		if *Public(s).Actor != 1 {
			t.Fatalf("应返回座1: %+v", Public(s))
		}
		l, err := Legal(s, 1)
		if err != nil {
			t.Fatal(err)
		}
		if l.CanCheck || l.CallAmount != 30 {
			t.Fatalf("欠30不能 check: %+v", l)
		}
		if _, e := Apply(s, 1, Action{Kind: "check"}); e == nil {
			t.Fatal("加注后的 check 必须被拒绝")
		}
	})

	t.Run("check_denied_after_short_all_in", func(t *testing.T) {
		// 已跟10后短全押到15，返回时欠5；无加注权但仍必须 call/fold，check 拒绝。
		s := hand(t, 200, 200, 15)
		s = act(t, s, "call", 0)
		s = act(t, s, "call", 0)
		s = act(t, s, "raise_to", 15)
		if *Public(s).Actor != 0 {
			t.Fatalf("应返回座0: %+v", Public(s))
		}
		l, err := Legal(s, 0)
		if err != nil {
			t.Fatal(err)
		}
		if l.CanCheck || l.CanRaise || l.CallAmount != 5 {
			t.Fatalf("短全押后欠5且无加注权: %+v", l)
		}
		if _, e := Apply(s, 0, Action{Kind: "check"}); e == nil {
			t.Fatal("短全押欠注者的 check 必须被拒绝")
		}
		if _, e := Apply(s, 0, Action{Kind: "fold"}); e != nil {
			t.Fatalf("欠注者 fold 应合法: %v", e)
		}
	})
}

// ---------------------------------------------------------------------------
// 优先级 2：前位 check → 后位完整 bet → 前位可 check-raise；
// 加注后其他人重新响应，不能提前换街。
// ---------------------------------------------------------------------------

func TestCheckRaiseAndReopen(t *testing.T) {
	s := finishStreet(t, hand(t, 100, 100, 100)) // 翻牌 actor=1
	s = act(t, s, "check", 0)                    // A=座1 check
	s = act(t, s, "raise_to", 20)                // B=座2 bet20
	s = act(t, s, "call", 0)                     // C=座0 call20
	// 返回 A，A 可 check-raise，最小目标 40。
	l, err := Legal(s, 1)
	if err != nil {
		t.Fatal(err)
	}
	if l.CanCheck || !l.CanRaise || l.MinRaiseTo != 40 || l.CallAmount != 20 {
		t.Fatalf("check-raise 合法动作: %+v", l)
	}
	if _, e := Apply(s, 1, Action{Kind: "raise_to", To: 39}); e == nil {
		t.Fatal("低于最小目标的 check-raise 应被拒绝")
	}
	s = act(t, s, "raise_to", 40) // A check-raise 到 40
	// 加注后 B、C 必须重新响应，仍在本街，不能提前换到转牌。
	p := Public(s)
	if p.Street != "flop" || p.Actor == nil || *p.Actor != 2 {
		t.Fatalf("加注后应回座2且仍在翻牌: %+v", p)
	}
	l, err = Legal(s, 2)
	if err != nil {
		t.Fatal(err)
	}
	if !l.CanRaise || l.CallAmount != 20 || l.MinRaiseTo != 60 {
		t.Fatalf("B 重新响应可再加注到60: %+v", l)
	}
}

// ---------------------------------------------------------------------------
// 优先级 3：短全押后的 call/fold 与加注权分开验证，含逐玩家累计重开。
// ---------------------------------------------------------------------------

func TestShortAllInRightsAndCumulativeReopen(t *testing.T) {
	t.Run("per_player_cumulative_reopen", func(t *testing.T) {
		// 5人，翻前顺序 3→4→0→1→2；筹码 [200,18,20,200,15]。
		// 3 call10、4 all-in15、0 call15、1 all-in18、2 all-in20 → 3 累计重开；
		// 3 call20 后，0 只欠5且不重开。
		s := hand(t, 200, 18, 20, 200, 15)
		if *Public(s).Actor != 3 {
			t.Fatalf("翻前首位 actor: %+v", Public(s))
		}
		s = act(t, s, "call", 0)      // 3 call10
		s = act(t, s, "raise_to", 15) // 4 all-in15
		s = act(t, s, "call", 0)      // 0 call15
		s = act(t, s, "raise_to", 18) // 1 all-in18
		s = act(t, s, "raise_to", 20) // 2 all-in20
		if *Public(s).Actor != 3 {
			t.Fatalf("应返回座3: %+v", Public(s))
		}
		l, err := Legal(s, 3)
		if err != nil {
			t.Fatal(err)
		}
		if !l.CanRaise || l.MinRaiseTo != 30 || l.CallAmount != 10 {
			t.Fatalf("累计增量达完整增量应重开: %+v", l)
		}
		s = act(t, s, "call", 0) // 3 call20
		if *Public(s).Actor != 0 {
			t.Fatalf("应轮到座0: %+v", Public(s))
		}
		l, err = Legal(s, 0)
		if err != nil {
			t.Fatal(err)
		}
		if l.CanCheck || l.CanRaise || l.CallAmount != 5 {
			t.Fatalf("座0只欠5且不重开: %+v", l)
		}
		if _, e := Apply(s, 0, Action{Kind: "raise_to", To: 30}); e == nil {
			t.Fatal("座0不应有加注权")
		}
		s = act(t, s, "call", 0)
		// 座0与座3仍有筹码，匹配后应进入下一街继续打，而不是立即结算。
		if Public(s).Street == "preflop" {
			t.Fatalf("匹配后应进入下一街: %+v", Public(s))
		}
		s = finishCalling(t, s)
		if _, ok := Result(s); !ok {
			t.Fatal("跑完后应有结算")
		}
	})

	t.Run("cumulative_9_no_reopen_10_reopen", func(t *testing.T) {
		// 4人保证仍有无筹码压力的响应者；翻前顺序 3→0→1→2。
		// 累计只到19（增9）不重开；恰好20（增10）才重开。
		s := hand(t, 200, 15, 19, 200)
		s = act(t, s, "call", 0)         // 3 call10
		s = act(t, s, "call", 0)         // 0 call10
		s = act(t, s, "raise_to", 15)    // 1 all-in15
		s = act(t, s, "raise_to", 19)    // 2 all-in19
		l, err := Legal(s, 3)
		if err != nil {
			t.Fatal(err)
		}
		if l.CanRaise || l.CallAmount != 9 {
			t.Fatalf("累计增9不足完整增量10，不重开: %+v", l)
		}
		s = hand(t, 200, 15, 20, 200)
		s = act(t, s, "call", 0)         // 3 call10
		s = act(t, s, "call", 0)         // 0 call10
		s = act(t, s, "raise_to", 15)    // 1 all-in15
		s = act(t, s, "raise_to", 20)    // 2 all-in20
		l, err = Legal(s, 3)
		if err != nil {
			t.Fatal(err)
		}
		if !l.CanRaise || l.CallAmount != 10 || l.MinRaiseTo != 30 {
			t.Fatalf("累计增10恰好重开: %+v", l)
		}
	})

	t.Run("single_short_all_in_call_or_fold_only", func(t *testing.T) {
		s := hand(t, 200, 200, 15)
		s = act(t, s, "call", 0)
		s = act(t, s, "call", 0)
		s = act(t, s, "raise_to", 15)
		l, err := Legal(s, 0)
		if err != nil {
			t.Fatal(err)
		}
		if l.CanRaise || l.CallAmount != 5 || l.CanCheck {
			t.Fatalf("单次短全押不重开: %+v", l)
		}
		if _, e := Apply(s, 0, Action{Kind: "raise_to", To: 30}); e == nil {
			t.Fatal("无加注权却接受加注")
		}
		if _, e := Apply(s, 0, Action{Kind: "fold"}); e != nil {
			t.Fatalf("call/fold 权应保留，fold 合法: %v", e)
		}
	})
}

// ---------------------------------------------------------------------------
// 优先级 4：raise_to 是累计目标（非追加额）；完整/短全押上下界逐一验证。
// ---------------------------------------------------------------------------

func TestRaiseToIsCumulativeTarget(t *testing.T) {
	t.Run("raise_to_is_target_not_increment", func(t *testing.T) {
		s := hand(t, 100, 100, 100)
		s = act(t, s, "raise_to", 35) // 座0 目标35
		p := Public(s)
		if p.Players[0].StreetBet != 35 || p.Players[0].Stack != 65 || p.Players[0].Committed != 35 {
			t.Fatalf("raise_to 应为本街累计目标: %+v", p.Players[0])
		}
		l, err := Legal(s, 1)
		if err != nil {
			t.Fatal(err)
		}
		if l.MinRaiseTo != 60 || l.MaxRaiseTo != 100 {
			t.Fatalf("min/max 应为绝对目标: %+v", l)
		}
		if _, e := Apply(s, 1, Action{Kind: "raise_to", To: 59}); e == nil {
			t.Fatal("低于最小完整目标应被拒绝")
		}
		s = act(t, s, "raise_to", 60) // 座1 目标60
		p = Public(s)
		// 座1 只追加 60-10=50，而不是扣60。
		if p.Players[1].StreetBet != 60 || p.Players[1].Stack != 40 || p.Players[1].Committed != 60 {
			t.Fatalf("raise_to 只追加目标与已有下注之差: %+v", p.Players[1])
		}
	})

	t.Run("all_in_target_only_adds_difference", func(t *testing.T) {
		// 座0 本街已投35、剩65，回到它时最大目标100；raise_to 100 只追加65。
		s := hand(t, 100, 100, 100)
		s = act(t, s, "raise_to", 35) // 0
		s = act(t, s, "raise_to", 70) // 1
		s = act(t, s, "call", 0)      // 2
		if *Public(s).Actor != 0 {
			t.Fatalf("应返回座0: %+v", Public(s))
		}
		l, err := Legal(s, 0)
		if err != nil {
			t.Fatal(err)
		}
		if !l.CanRaise || l.MaxRaiseTo != 100 || l.ShortAllInTo == nil || *l.ShortAllInTo != 100 {
			t.Fatalf("座0最大目标100的短全押: %+v", l)
		}
		s = act(t, s, "raise_to", 100)
		p := Public(s)
		if p.Players[0].StreetBet != 100 || p.Players[0].Stack != 0 || p.Players[0].Committed != 100 {
			t.Fatalf("raise_to 100 只追加65: %+v", p.Players[0])
		}
	})

	t.Run("short_open_all_in_below_bb", func(t *testing.T) {
		// 翻牌无下注，短筹码 7 只能短全押到 7；满筹码 raise_to 7 拒绝。
		s := finishStreet(t, hand(t, 100, 100, 100)) // 翻牌 actor=1
		short := s
		short.players[1].Stack = 7
		short.players[1].StreetBet = 0
		l, err := Legal(short, 1)
		if err != nil {
			t.Fatal(err)
		}
		// 无人下注，短筹码可以 check，也可以把不足BB的首注做成短全押。
		if !l.CanCheck || l.ShortAllInTo == nil || *l.ShortAllInTo != 7 {
			t.Fatalf("无人下注时短筹码可 check 或短全押到7: %+v", l)
		}
		if _, e := Apply(short, 1, Action{Kind: "raise_to", To: 7}); e != nil {
			t.Fatalf("合法短全押被拒: %v", e)
		}
		full := s
		if _, e := Apply(full, 1, Action{Kind: "raise_to", To: 7}); e == nil {
			t.Fatal("满筹码低于BB的开池应被拒绝")
		}
	})

	t.Run("short_all_in_bounds_14_rejected_15_accepted", func(t *testing.T) {
		// 面对 CurrentBet10、min20、max15：15 接受（全押），14 拒绝（非全押且低于最小）。
		s := hand(t, 100, 15, 100)
		s = act(t, s, "call", 0) // 座0 call10 → 座1(SB)
		l, err := Legal(s, 1)
		if err != nil {
			t.Fatal(err)
		}
		if !l.CanRaise || l.MaxRaiseTo != 15 || l.MinRaiseTo != 20 || l.ShortAllInTo == nil || *l.ShortAllInTo != 15 {
			t.Fatalf("短全押上下界: %+v", l)
		}
		if _, e := Apply(s, 1, Action{Kind: "raise_to", To: 14}); e == nil {
			t.Fatal("非全押的低于最小值目标应被拒绝")
		}
		s = act(t, s, "raise_to", 15)
		p := Public(s)
		if p.Players[1].Stack != 0 || p.Players[1].StreetBet != 15 || p.Players[1].Status != "all_in" {
			t.Fatalf("15 应为 all-in: %+v", p.Players[1])
		}
	})

	t.Run("full_raise_bound_59_rejected_60_accepted", func(t *testing.T) {
		s := hand(t, 100, 100, 100)
		s = act(t, s, "raise_to", 35) // lastFullRaise=25
		if _, e := Apply(s, 1, Action{Kind: "raise_to", To: 59}); e == nil {
			t.Fatal("59 低于最小完整加注目标 60")
		}
		s = act(t, s, "raise_to", 60)
		if Public(s).CurrentBet != 60 {
			t.Fatalf("CurrentBet: %+v", Public(s))
		}
	})

	t.Run("all_in_at_max_target_and_above_rejected", func(t *testing.T) {
		s := hand(t, 100, 100, 100)
		l, err := Legal(s, 0)
		if err != nil {
			t.Fatal(err)
		}
		if l.MaxRaiseTo != 100 {
			t.Fatalf("MaxRaiseTo: %+v", l)
		}
		if _, e := Apply(s, 0, Action{Kind: "raise_to", To: 101}); e == nil {
			t.Fatal("超过最大目标应被拒绝")
		}
		s = act(t, s, "raise_to", 100)
		p := Public(s)
		if p.Players[0].Stack != 0 || p.Players[0].StreetBet != 100 {
			t.Fatalf("最大目标应全押: %+v", p.Players[0])
		}
	})

	t.Run("short_sb_big_blind_option", func(t *testing.T) {
		// 短SB全押后 BB 仍有 check/raise 的 option。
		s := hand(t, 100, 3, 100)
		s = act(t, s, "call", 0)
		if *Public(s).Actor != 2 {
			t.Fatalf("应轮到 BB 座2: %+v", Public(s))
		}
		l, err := Legal(s, 2)
		if err != nil {
			t.Fatal(err)
		}
		if !l.CanCheck {
			t.Fatalf("BB 面对未加注应可 check: %+v", l)
		}
		if _, e := Apply(s, 2, Action{Kind: "check"}); e != nil {
			t.Fatalf("BB check 应合法: %v", e)
		}
	})

	t.Run("short_bb_call_baseline_min_raise_20", func(t *testing.T) {
		// 短BB全押不降低正常入池与完整加注下界。
		s := hand(t, 100, 100, 7)
		s = act(t, s, "call", 0)
		if *Public(s).Actor != 1 {
			t.Fatalf("应轮到 SB 座1: %+v", Public(s))
		}
		l, err := Legal(s, 1)
		if err != nil {
			t.Fatal(err)
		}
		if l.CallAmount != 5 || l.MinRaiseTo != 20 {
			t.Fatalf("短BB场景跟注基准10、完整加注下界20: %+v", l)
		}
	})
}

// ---------------------------------------------------------------------------
// 优先级 5：主池、边池由不同牌力玩家获胜（不是所有公共牌平局）。
// ---------------------------------------------------------------------------

func TestMainAndSidePotsDistinctWinners(t *testing.T) {
	board := cards("2c", "3d", "7h", "9s", "Qc")

	t.Run("main_pot_and_side_pot_distinct_winners", func(t *testing.T) {
		// 投入 [30,60,100,30]，座3弃牌。主池120由座0一对A赢，边池60由座1一对K赢，
		// 座2顶层的40退款。
		s := showdownCards(t, []Chips{30, 60, 100, 30}, map[int]bool{3: true}, 0, board, map[int][2]Card{
			0: {"As", "Ad"},
			1: {"Kh", "Kd"},
			2: {"Jh", "Jd"},
			3: {"Tc", "Td"},
		})
		r, ok := Result(s)
		if !ok || len(r.Pots) != 2 {
			t.Fatalf("应有两个池: %+v", r)
		}
		if r.Pots[0].Amount != 120 || !reflect.DeepEqual(r.Pots[0].Winners, []Seat{0}) || r.Pots[0].Payouts[0] != 120 {
			t.Fatalf("主池应由座0赢: %+v", r.Pots[0])
		}
		if r.Pots[1].Amount != 60 || !reflect.DeepEqual(r.Pots[1].Winners, []Seat{1}) || r.Pots[1].Payouts[1] != 60 {
			t.Fatalf("边池应由座1赢: %+v", r.Pots[1])
		}
		if r.Refunds[2] != 40 || r.Payouts[2] != 0 {
			t.Fatalf("座2顶层40应退款且无赔注: %+v", r)
		}
		assertConservation(t, s)
	})

	t.Run("folded_strongest_hand_not_eligible", func(t *testing.T) {
		// 弃牌座持有最强牌，仍不得进入 eligible / winners / 赔注。
		s := showdownCards(t, []Chips{30, 60, 100, 30}, map[int]bool{3: true}, 0, board, map[int][2]Card{
			0: {"Kh", "Kd"},
			1: {"Jh", "Jd"},
			2: {"Tc", "Td"},
			3: {"As", "Ad"},
		})
		r, _ := Result(s)
		for _, pot := range r.Pots {
			if containsSeat(pot.Eligible, 3) || containsSeat(pot.Winners, 3) {
				t.Fatalf("弃牌座不得参与底池: %+v", pot)
			}
		}
		if r.Payouts[3] != 0 {
			t.Fatalf("弃牌座赔注应为0: %+v", r.Payouts)
		}
		assertConservation(t, s)
	})

	t.Run("multi_level_amounts_and_eligibility", func(t *testing.T) {
		// 投入 [100,70,40,10]，均未弃牌；公共牌皇家同花顺使所有人平局。
		// 各层金额 40/90/60，座0 顶层退款30；eligible 逐层为 4/3/2 人。
		s := showdownCards(t, []Chips{100, 70, 40, 10}, nil, 0, cards("Tc", "Jc", "Qc", "Kc", "Ac"), nil)
		r, _ := Result(s)
		if len(r.Pots) != 3 {
			t.Fatalf("应为三个可争夺池: %+v", r.Pots)
		}
		wantAmount := []Chips{40, 90, 60}
		wantEligible := []int{4, 3, 2}
		for i := range wantAmount {
			if r.Pots[i].Amount != wantAmount[i] || len(r.Pots[i].Eligible) != wantEligible[i] {
				t.Fatalf("池%d: %+v", i, r.Pots[i])
			}
		}
		if r.Refunds[0] != 30 {
			t.Fatalf("座0顶层退款30: %+v", r.Refunds)
		}
		assertConservation(t, s)
	})
}

// ---------------------------------------------------------------------------
// 优先级 6：全部同级牌型比较及踢脚边界（用 rankFive 直接构造五张）。
// ---------------------------------------------------------------------------

func TestRankFiveSameCategoryBoundaries(t *testing.T) {
	t.Run("better_hand_wins", func(t *testing.T) {
		cases := []struct {
			name   string
			better [5]Card
			worse  [5]Card
		}{
			{"high_card_fifth_kicker", five("Ac", "Kd", "Qh", "Js", "9c"), five("Ad", "Kh", "Qs", "Jc", "8d")},
			{"pair_rank_A_over_K", five("Ac", "Ad", "2h", "3s", "4c"), five("Kc", "Kd", "Ah", "Qs", "Jc")},
			{"two_pair_high_pair", five("Kc", "Kd", "Qh", "Qs", "Ac"), five("Kh", "Ks", "Jc", "Jd", "Ah")},
			{"two_pair_fifth_kicker", five("Kc", "Kd", "Qh", "Qs", "Ac"), five("Kh", "Ks", "Qc", "Qd", "Jc")},
			{"trips_rank_over_kicker", five("Kc", "Kd", "Kh", "2s", "3c"), five("Qc", "Qd", "Qh", "As", "Kc")},
			{"trips_kicker_order", five("Kc", "Kd", "Kh", "As", "Qc"), five("Ks", "Kc", "Kd", "Ah", "Jc")},
			{"straight_six_over_wheel", five("2c", "3d", "4h", "5s", "6c"), five("Ac", "2d", "3h", "4s", "5c")},
			{"flush_fifth_card", five("Ac", "Jc", "9c", "7c", "5c"), five("Ad", "Jd", "9d", "7d", "4d")},
			{"full_house_trips_first", five("Ac", "Ad", "Ah", "2s", "2c"), five("Kc", "Kd", "Kh", "Qs", "Qc")},
			{"full_house_pair_second", five("Ac", "Ad", "Ah", "Ks", "Kc"), five("As", "Ac", "Ad", "Qh", "Qd")},
			{"quads_rank", five("Kc", "Kd", "Kh", "Ks", "2c"), five("Qc", "Qd", "Qh", "Qs", "Ac")},
			{"quads_kicker", five("Kc", "Kd", "Kh", "Ks", "Ac"), five("Kc", "Kd", "Kh", "Ks", "Qc")},
			{"straight_flush_six_over_wheel", five("2c", "3c", "4c", "5c", "6c"), five("Ac", "2c", "3c", "4c", "5c")},
			{"royal_over_nine_high_sf", five("Tc", "Jc", "Qc", "Kc", "Ac"), five("9c", "Tc", "Jc", "Qc", "Kc")},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				if rankFive(tc.better) <= rankFive(tc.worse) {
					t.Fatalf("better=%d worse=%d", rankFive(tc.better), rankFive(tc.worse))
				}
			})
		}
	})

	t.Run("suits_do_not_break_ties", func(t *testing.T) {
		if rankFive(five("Ac", "Kd", "Qh", "Js", "9c")) != rankFive(five("Ad", "Kh", "Qs", "Jc", "9d")) {
			t.Fatal("同点数高牌不应按花色分胜负")
		}
		if rankFive(five("Ac", "Jc", "9c", "7c", "5c")) != rankFive(five("Ad", "Jd", "9d", "7d", "5d")) {
			t.Fatal("同点数同花不应按花色分胜负")
		}
	})

	t.Run("non_straight_A_high", func(t *testing.T) {
		v, cat := evalFive(five("Qc", "Kd", "Ah", "2s", "3c"))
		if cat != 0 {
			t.Fatalf("QKA23 不是顺子，category got %d value %d", cat, v)
		}
		if _, cat := evalFive(five("Ac", "2d", "3h", "4s", "5c")); cat != 4 {
			t.Fatal("A2345 应为顺子")
		}
	})
}

// ---------------------------------------------------------------------------
// 优先级 7：最佳五张边界（0/1/2 张底牌、双三条、三对、6张同花、wheel、
// 排列不变、board 皇家同花顺 split）。
// ---------------------------------------------------------------------------

func TestBestFiveBoundaries(t *testing.T) {
	t.Run("straight_uses_one_hole_card", func(t *testing.T) {
		// board=9c Tc Jd Qh 2s，底牌 Ks 3d → 最佳 9TJQK，只用 Ks 一张底牌。
		_, cat, best := rankBest(cards("9c", "Tc", "Jd", "Qh", "2s", "Ks", "3d"))
		if cat != 4 {
			t.Fatalf("应为顺子 category got %d", cat)
		}
		if !containsCard(best[:], "Ks") {
			t.Fatalf("最佳五张应使用 Ks: %v", best)
		}
		if containsCard(best[:], "3d") {
			t.Fatalf("最佳五张不应使用无效底牌 3d: %v", best)
		}
	})

	t.Run("straight_uses_two_hole_cards", func(t *testing.T) {
		// board=2c 7d 9h Js Qc，底牌 Kd Ts → 最佳 9TJQK，使用两张底牌。
		_, cat, best := rankBest(cards("2c", "7d", "9h", "Js", "Qc", "Kd", "Ts"))
		if cat != 4 {
			t.Fatalf("应为顺子 category got %d", cat)
		}
		if !containsCard(best[:], "Kd") || !containsCard(best[:], "Ts") {
			t.Fatalf("最佳五张应使用两张底牌: %v", best)
		}
	})

	t.Run("double_trips_pick_high_full_house", func(t *testing.T) {
		// 七张 Ac Ad Ah Kc Kd Kh 2s → 最佳 AAAKK，不能错选 KKKAA。
		_, cat, best := rankBest(cards("Ac", "Ad", "Ah", "Kc", "Kd", "Kh", "2s"))
		if cat != 6 {
			t.Fatalf("应为葫芦 category got %d", cat)
		}
		if !reflect.DeepEqual(sortedCards(best[:]), sortedCards(cards("Ac", "Ad", "Ah", "Kc", "Kd"))) {
			t.Fatalf("双三条应取 AAAKK: %v", best)
		}
	})

	t.Run("three_pairs_pick_top_two", func(t *testing.T) {
		// 七张 Ac Ad Kc Kd Qc Qd 2s → 最佳 AAKKQ。
		_, cat, best := rankBest(cards("Ac", "Ad", "Kc", "Kd", "Qc", "Qd", "2s"))
		if cat != 2 {
			t.Fatalf("应为两对 category got %d", cat)
		}
		if !reflect.DeepEqual(sortedCards(best[:]), sortedCards(cards("Ac", "Ad", "Kc", "Kd", "Qc"))) {
			t.Fatalf("三对应取 AAKKQ: %v", best)
		}
	})

	t.Run("six_flush_cards_drop_lowest", func(t *testing.T) {
		// 七张 Ac Kc Qc 9c 7c 3c 2d → 最佳同花 AKQ97，丢掉 3c。
		_, cat, best := rankBest(cards("Ac", "Kc", "Qc", "9c", "7c", "3c", "2d"))
		if cat != 5 {
			t.Fatalf("应为同花 category got %d", cat)
		}
		if !reflect.DeepEqual(sortedCards(best[:]), sortedCards(cards("Ac", "Kc", "Qc", "9c", "7c"))) {
			t.Fatalf("六张同花应丢最低 3c: %v", best)
		}
	})

	t.Run("six_high_straight_beats_wheel_with_duplicate", func(t *testing.T) {
		// 七张 Ac 2d 3h 4s 5c 6d 6h → 最佳六高顺，不取 wheel，不因重复6漏判。
		_, cat, best := rankBest(cards("Ac", "2d", "3h", "4s", "5c", "6d", "6h"))
		if cat != 4 {
			t.Fatalf("应为顺子 category got %d", cat)
		}
		if containsCard(best[:], "Ac") {
			t.Fatalf("应取六高顺而非 wheel: %v", best)
		}
	})

	t.Run("permutation_invariance", func(t *testing.T) {
		orig := cards("Ac", "2d", "3h", "4s", "5c", "6d", "6h")
		base := rankCards(orig)
		rot := append(append([]Card{}, orig[2:]...), orig[:2]...)
		rev := []Card{orig[6], orig[5], orig[4], orig[3], orig[2], orig[1], orig[0]}
		if rankCards(rot) != base || rankCards(rev) != base {
			t.Fatal("牌力应对排列不变")
		}
	})

	t.Run("board_royal_flush_split_zero_hole", func(t *testing.T) {
		// board 皇家同花顺，两个不同底牌的玩家都只用 0 张底牌，平分。
		royalBoard := cards("Tc", "Jc", "Qc", "Kc", "Ac")
		a := append(append([]Card{}, royalBoard...), "2d", "3h")
		b := append(append([]Card{}, royalBoard...), "9d", "9h")
		if rankCards(a) != rankCards(b) {
			t.Fatal("board 皇家同花顺应使不同底牌平局")
		}
		_, cat, best := rankBest(a)
		if cat != 8 || !reflect.DeepEqual(sortedCards(best[:]), sortedCards(royalBoard)) {
			t.Fatalf("最佳五张应为公共牌皇家同花顺: cat=%d %v", cat, best)
		}
	})

	t.Run("best_five_is_subset_of_input", func(t *testing.T) {
		pool := cards("Ac", "Ad", "Ah", "Kc", "Kd", "Kh", "2s")
		_, _, best := rankBest(pool)
		count := map[Card]int{}
		for _, c := range pool {
			count[c]++
		}
		for _, c := range best {
			count[c]--
			if count[c] < 0 {
				t.Fatalf("最佳五张不是输入的无重复子集: %v", best)
			}
		}
	})
}

func containsCard(pool []Card, target Card) bool {
	for _, c := range pool {
		if c == target {
			return true
		}
	}
	return false
}
