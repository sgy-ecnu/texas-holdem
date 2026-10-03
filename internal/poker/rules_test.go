package poker

import (
	"bytes"
	"encoding/json"
	"math/rand"
	"reflect"
	"sync"
	"testing"
)

// 所有随机测试使用固定种子；牌堆构造与行动均可复现。
func hand(t *testing.T, stacks ...Chips) State {
	t.Helper()
	cfg := HandConfig{Button: 0, SmallBlind: 5, BigBlind: 10}
	for i, n := range stacks {
		cfg.Players = append(cfg.Players, PlayerInit{Seat: Seat(i), Stack: n})
	}
	s, err := NewHand(cfg, StandardDeck())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func act(t *testing.T, s State, kind ActionKind, to Chips) State {
	t.Helper()
	p := Public(s)
	if p.Actor == nil {
		t.Fatal("没有行动者")
	}
	n, err := Apply(s, *p.Actor, Action{Kind: kind, To: to})
	if err != nil {
		t.Fatalf("seat=%d action=%s/%d: %v", *p.Actor, kind, to, err)
	}
	return n
}

func finishCalling(t *testing.T, s State) State {
	t.Helper()
	for i := 0; i < 100; i++ {
		p := Public(s)
		if p.Actor == nil {
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
	t.Fatal("行动未结束")
	return s
}

func TestBlindAndActionOrder(t *testing.T) {
	t.Run("heads_up", func(t *testing.T) {
		s := hand(t, 100, 100)
		p := Public(s)
		if p.SmallBlind != 0 || p.BigBlind != 1 || p.Actor == nil || *p.Actor != 0 {
			t.Fatal(p)
		}
		s = act(t, s, "call", 0)
		s = act(t, s, "check", 0)
		p = Public(s)
		if p.Street != "flop" || *p.Actor != 1 || len(p.Board) != 3 {
			t.Fatal(p)
		}
		o, _ := Observe(hand(t, 100, 100), 0)
		if o.Hole != [2]Card{"2d", "2s"} {
			t.Fatal("HU逐轮发牌顺序", o.Hole)
		}
	})
	t.Run("nine_seats", func(t *testing.T) {
		s := hand(t, 100, 100, 100, 100, 100, 100, 100, 100, 100)
		p := Public(s)
		if p.SmallBlind != 1 || p.BigBlind != 2 || *p.Actor != 3 {
			t.Fatal(p)
		}
		for Public(s).Street == "preflop" {
			l, _ := Legal(s, *Public(s).Actor)
			k := ActionKind("call")
			if l.CanCheck {
				k = "check"
			}
			s = act(t, s, k, 0)
		}
		if *Public(s).Actor != 1 {
			t.Fatal(Public(s))
		}
	})
	t.Run("sparse_and_out_seats", func(t *testing.T) {
		s, e := NewHand(HandConfig{Players: []PlayerInit{{7, 100}, {2, 100}, {4, 0}}, Button: 7, SmallBlind: 5, BigBlind: 10}, StandardDeck())
		if e != nil {
			t.Fatal(e)
		}
		p := Public(s)
		if p.SmallBlind != 7 || p.BigBlind != 2 || *p.Actor != 7 {
			t.Fatal(p)
		}
	})
}

func TestMinimumRaise(t *testing.T) {
	s := hand(t, 200, 200, 200)
	l, _ := Legal(s, *Public(s).Actor)
	if l.MinRaiseTo != 20 || l.MaxRaiseTo != 200 {
		t.Fatal(l)
	}
	if _, e := Apply(s, 0, Action{"raise_to", 19}); e == nil {
		t.Fatal("接受不足完整加注")
	}
	s = act(t, s, "raise_to", 35)
	l, _ = Legal(s, *Public(s).Actor)
	if l.MinRaiseTo != 60 {
		t.Fatal(l)
	}
	s = act(t, s, "call", 0)
	s = act(t, s, "call", 0)
	l, _ = Legal(s, *Public(s).Actor)
	if l.MinRaiseTo != 10 {
		t.Fatal("开池下界", l)
	}
	s = act(t, s, "raise_to", 15)
	l, _ = Legal(s, *Public(s).Actor)
	if l.MinRaiseTo != 30 {
		t.Fatal("开池增量", l)
	}
}

func TestShortAllInReopening(t *testing.T) {
	t.Run("single_does_not_reopen", func(t *testing.T) {
		s := hand(t, 200, 200, 15)
		s = act(t, s, "call", 0)
		s = act(t, s, "call", 0)
		l, _ := Legal(s, 2)
		if l.ShortAllInTo == nil || *l.ShortAllInTo != 15 {
			t.Fatal(l)
		}
		s = act(t, s, "raise_to", 15)
		p := Public(s)
		if *p.Actor != 0 {
			t.Fatal("欠注者未响应", p)
		}
		l, _ = Legal(s, 0)
		if l.CanRaise || l.CallAmount != 5 {
			t.Fatal(l)
		}
		if _, e := Apply(s, 0, Action{"raise_to", 30}); e == nil {
			t.Fatal("短全押错误重开")
		}
	})
	t.Run("cumulative_reopens", func(t *testing.T) {
		s := hand(t, 200, 15, 20, 200) // 翻前顺序3、0、1、2。
		s = act(t, s, "call", 0)
		s = act(t, s, "call", 0)
		s = act(t, s, "raise_to", 15)
		s = act(t, s, "raise_to", 20)
		l, _ := Legal(s, 3)
		if !l.CanRaise || l.MinRaiseTo != 30 || l.CallAmount != 10 {
			t.Fatal(l)
		}
		s = act(t, s, "raise_to", 30)
		if *Public(s).Actor != 0 {
			t.Fatal(Public(s))
		}
	})
	t.Run("unacted_retains_right", func(t *testing.T) {
		s := hand(t, 15, 200, 200)
		s = act(t, s, "raise_to", 15)
		l, _ := Legal(s, 1)
		if !l.CanRaise || l.MinRaiseTo != 25 {
			t.Fatal(l)
		}
	})
}

func TestNoRaiseWithoutResponder(t *testing.T) {
	s := hand(t, 100, 12)
	s = act(t, s, "raise_to", 20)
	l, _ := Legal(s, 1)
	if l.CanRaise || l.CallAmount != 2 {
		t.Fatal(l)
	}
	s = act(t, s, "call", 0)
	if _, ok := Result(s); !ok {
		t.Fatal("无决策未自动结算")
	}
	s = hand(t, 100, 10)
	l, _ = Legal(s, 0)
	if l.CanRaise {
		t.Fatal(l)
	}
	if _, e := Apply(s, 0, Action{"raise_to", 20}); e == nil {
		t.Fatal("允许对全押者加注")
	}
}

func TestBlindShortStack(t *testing.T) {
	s := hand(t, 100, 3, 7)
	p := Public(s)
	if p.CurrentBet != 10 || p.Pot != 10 || p.Players[1].Status != "all_in" || p.Players[2].Status != "all_in" {
		t.Fatal(p)
	}
	l, _ := Legal(s, 0)
	if l.CallAmount != 10 || l.CanRaise {
		t.Fatal(l)
	}
	s = act(t, s, "call", 0)
	r, ok := Result(s)
	if !ok || r.Refunds[0] != 3 {
		t.Fatal(r, ok)
	}
	s = hand(t, 3, 7)
	if _, ok := Result(s); !ok || len(Public(s).Board) != 5 {
		t.Fatal("双盲全押未跑牌")
	}
}

// 结算夹具只用于独立验证池分层；完整行动链另有守恒和边池测试。
func settlementFixture(t *testing.T, bets []Chips, folded map[int]bool, button Seat) State {
	t.Helper()
	stacks := make([]Chips, len(bets))
	for i := range stacks {
		stacks[i] = 100
	}
	s := hand(t, stacks...)
	s.button = button
	s.board = []Card{"Tc", "Jc", "Qc", "Kc", "Ac"}
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
	s.settle("showdown")
	return s
}

func TestSidePotsAndRefunds(t *testing.T) {
	t.Run("layers_folded_money_refund", func(t *testing.T) {
		s := settlementFixture(t, []Chips{100, 60, 30, 30}, map[int]bool{3: true}, 0)
		r, _ := Result(s)
		if len(r.Pots) != 2 || r.Pots[0].Amount != 120 || r.Pots[1].Amount != 60 || r.Refunds[0] != 40 {
			t.Fatal(r)
		}
		if !reflect.DeepEqual(r.Pots[0].Eligible, []Seat{0, 1, 2}) || !reflect.DeepEqual(r.Pots[1].Eligible, []Seat{0, 1}) {
			t.Fatal(r)
		}
		if r.Payouts[0] != 70 || r.Payouts[1] != 70 || r.Payouts[2] != 40 || r.Payouts[3] != 0 {
			t.Fatal(r)
		}
		assertConservation(t, s)
	})
	t.Run("actual_action_chain", func(t *testing.T) {
		s := hand(t, 100, 60, 30)
		s = act(t, s, "raise_to", 100)
		s = act(t, s, "call", 0)
		s = act(t, s, "call", 0)
		r, ok := Result(s)
		if !ok || r.Refunds[0] != 40 || len(r.Pots) != 2 || r.Pots[0].Amount != 90 || r.Pots[1].Amount != 60 {
			t.Fatal(r, ok)
		}
		assertConservation(t, s)
	})
}

func TestSplitPotOddChip(t *testing.T) {
	s := settlementFixture(t, []Chips{5, 5, 3, 3, 1}, map[int]bool{3: true, 4: true}, 0)
	r, _ := Result(s)
	if len(r.Pots) != 3 {
		t.Fatal(r)
	}
	// 主池5除3，button后1、2各取余数；第二池8除3同序；第三池4除2。
	if r.Pots[0].Payouts[0] != 1 || r.Pots[0].Payouts[1] != 2 || r.Pots[0].Payouts[2] != 2 || r.Pots[1].Payouts[0] != 2 || r.Pots[1].Payouts[1] != 3 || r.Pots[1].Payouts[2] != 3 {
		t.Fatal(r)
	}
	assertConservation(t, s)
	s = settlementFixture(t, []Chips{1, 1, 1}, map[int]bool{1: true}, 2)
	r, _ = Result(s)
	if r.Payouts[0] != 2 || r.Payouts[2] != 1 {
		t.Fatal("顺时针回绕", r)
	}
}

func cards(v ...string) []Card {
	a := make([]Card, len(v))
	for i := range v {
		a[i] = Card(v[i])
	}
	return a
}

func TestHandRanking(t *testing.T) {
	groups := [][]Card{
		cards("Ac", "Kd", "9h", "7s", "5c", "3d", "2h"),
		cards("Ac", "Ad", "9h", "7s", "5c", "3d", "2h"),
		cards("Ac", "Ad", "9h", "9s", "5c", "3d", "2h"),
		cards("Ac", "Ad", "Ah", "7s", "5c", "3d", "2h"),
		cards("Ac", "2d", "3h", "4s", "5c", "9d", "Th"),
		cards("Ac", "Jc", "9c", "7c", "5c", "3d", "2h"),
		cards("Ac", "Ad", "Ah", "7s", "7c", "3d", "2h"),
		cards("Ac", "Ad", "Ah", "As", "5c", "3d", "2h"),
		cards("Ac", "2c", "3c", "4c", "5c", "9d", "Th"),
	}
	for i := 1; i < len(groups); i++ {
		if rankCards(groups[i]) <= rankCards(groups[i-1]) {
			t.Fatalf("牌型顺序%d", i)
		}
	}
	if rankCards(cards("2c", "3d", "4h", "5s", "6c", "9d", "Th")) <= rankCards(groups[4]) {
		t.Fatal("wheel应低于六高顺")
	}
	if rankCards(cards("Ac", "Ad", "Kh", "Qs", "Jc", "3d", "2h")) <= rankCards(cards("Ac", "Ad", "Kh", "Qs", "Tc", "3d", "2h")) {
		t.Fatal("同牌力踢脚")
	}
	if rankCards(cards("Tc", "Jc", "Qc", "Kc", "Ac", "2d", "3h")) != rankCards(cards("Tc", "Jc", "Qc", "Kc", "Ac", "9d", "9h")) {
		t.Fatal("公共牌平局")
	}
}

func TestRunoutAndUncontested(t *testing.T) {
	s := hand(t, 10, 10)
	s = act(t, s, "call", 0)
	r, ok := Result(s)
	if !ok || r.Reason != "showdown" || len(Public(s).Board) != 5 || Public(s).Actor != nil {
		t.Fatal(r, Public(s))
	}
	s = hand(t, 100, 100, 100)
	s = act(t, s, "fold", 0)
	s = act(t, s, "fold", 0)
	r, ok = Result(s)
	if !ok || r.Reason != "uncontested" || len(Public(s).Board) != 0 || r.Refunds[2] != 5 || r.Payouts[2] != 10 || r.FinalStack[2] != 105 {
		t.Fatal(r, Public(s))
	}
	if _, e := Legal(s, 2); e == nil {
		t.Fatal("结束后有合法动作")
	}
	if _, e := Apply(s, 2, Action{"check", 0}); e == nil {
		t.Fatal("结束后可行动")
	}
	// 2人底牌4张，烧牌后公共牌依次是第5/6/7、9、11号牌。
	s = finishCalling(t, hand(t, 100, 100))
	d := StandardDeck()
	if !reflect.DeepEqual(Public(s).Board, []Card{d[5], d[6], d[7], d[9], d[11]}) {
		t.Fatal(Public(s).Board)
	}
}

func assertConservation(t *testing.T, s State) {
	t.Helper()
	p := Public(s)
	var stacks, committed Chips
	for _, v := range p.Players {
		stacks += v.Stack
		committed += v.Committed
		if v.Stack < 0 || v.StreetBet < 0 || v.Committed < v.StreetBet {
			t.Fatal(v)
		}
	}
	if p.Pot != committed || stacks+committed != s.total {
		t.Fatalf("不守恒: %+v total=%d", p, s.total)
	}
	if r, ok := Result(s); ok {
		var total Chips
		for _, n := range r.FinalStack {
			total += n
		}
		if total != s.total || committed != 0 {
			t.Fatal(r)
		}
	}
}

func TestChipConservationRandomSequences(t *testing.T) {
	rng := rand.New(rand.NewSource(20261003))
	for h := 0; h < 200; h++ {
		n := 2 + rng.Intn(8)
		cfg := HandConfig{SmallBlind: 5, BigBlind: 10, Button: Seat(rng.Intn(n))}
		for i := 0; i < n; i++ {
			cfg.Players = append(cfg.Players, PlayerInit{Seat(i), Chips(1 + rng.Intn(300))})
		}
		d := StandardDeck()
		rng.Shuffle(52, func(i, j int) { d[i], d[j] = d[j], d[i] })
		s, e := NewHand(cfg, d)
		if e != nil {
			t.Fatal(e)
		}
		for step := 0; step < 500; step++ {
			assertConservation(t, s)
			p := Public(s)
			if p.Actor == nil {
				break
			}
			l, e := Legal(s, *p.Actor)
			if e != nil {
				t.Fatal(e)
			}
			choices := []Action{{"fold", 0}}
			if l.CanCheck {
				choices = append(choices, Action{"check", 0})
			}
			if l.CanCall {
				choices = append(choices, Action{"call", 0})
			}
			if l.CanRaise {
				if l.MaxRaiseTo >= l.MinRaiseTo {
					choices = append(choices, Action{"raise_to", l.MinRaiseTo}, Action{"raise_to", l.MaxRaiseTo})
				} else if l.ShortAllInTo != nil {
					choices = append(choices, Action{"raise_to", *l.ShortAllInTo})
				}
			}
			before, _ := EncodeState(s)
			if step%5 == 0 {
				restored, err := DecodeState(before)
				if err != nil {
					t.Fatal(err)
				}
				observation, _ := Observe(s, *p.Actor)
				resumedObservation, _ := Observe(restored, *p.Actor)
				if !reflect.DeepEqual(observation, resumedObservation) {
					t.Fatal("随机序列快照丢失重开权或私牌")
				}
			}
			next, e := Apply(s, *p.Actor, choices[rng.Intn(len(choices))])
			if e != nil {
				t.Fatal(e)
			}
			after, _ := EncodeState(s)
			if !bytes.Equal(before, after) {
				t.Fatal("Apply修改输入")
			}
			s = next
			if step == 499 {
				t.Fatal("序列未结束")
			}
		}
		if _, ok := Result(s); !ok {
			t.Fatal("无结果")
		}
	}
}

func TestObservationPrivacy(t *testing.T) {
	s := hand(t, 100, 100, 100)
	o, e := Observe(s, 0)
	if e != nil {
		t.Fatal(e)
	}
	if _, e := Observe(s, 1); e == nil {
		t.Fatal("非actor可观察")
	}
	if o.Hole != s.hole[0] {
		t.Fatal(o.Hole)
	}
	b, _ := json.Marshal(o)
	var fields map[string]json.RawMessage
	json.Unmarshal(b, &fields)
	if len(fields) != 4 || fields["State"] == nil || fields["Self"] == nil || fields["Hole"] == nil || fields["Legal"] == nil {
		t.Fatal(string(b))
	}
	for _, forbidden := range []string{"HoleCards", "Deck", "History", "Result", "Refunds"} {
		if bytes.Contains(b, []byte(forbidden)) {
			t.Fatal("私牌泄漏", forbidden)
		}
	}
	// State的所有字段必须私有，公共类型不含其他私牌或历史揭示。
	typ := reflect.TypeOf(s)
	for i := 0; i < typ.NumField(); i++ {
		if typ.Field(i).PkgPath == "" {
			t.Fatal("State公开字段", typ.Field(i).Name)
		}
	}
	s = finishCalling(t, s)
	if _, e := Observe(s, 0); e == nil {
		t.Fatal("终局可观察")
	}
}

func TestOutputIsolation(t *testing.T) {
	cfg := HandConfig{Players: []PlayerInit{{0, 100}, {1, 100}}, Button: 0, SmallBlind: 5, BigBlind: 10}
	original, err := NewHand(cfg, StandardDeck())
	if err != nil {
		t.Fatal(err)
	}
	before, _ := EncodeState(original)
	cfg.Players[0].Stack = 999
	after, _ := EncodeState(original)
	if !bytes.Equal(before, after) {
		t.Fatal("NewHand共享输入配置")
	}
	s := hand(t, 100, 100)
	p := Public(s)
	p.Players[0].Stack = 999
	*p.Actor = 8
	if Public(s).Players[0].Stack == 999 || *Public(s).Actor == 8 {
		t.Fatal("Public共享")
	}
	s = act(t, s, "call", 0)
	s = act(t, s, "check", 0)
	p = Public(s)
	p.Board[0] = "As"
	if Public(s).Board[0] == "As" {
		t.Fatal("Board共享")
	}
	o, _ := Observe(s, *Public(s).Actor)
	o.State.Players[0].Stack = 999
	if Public(s).Players[0].Stack == 999 {
		t.Fatal("Observe共享")
	}
	short := hand(t, 15, 100, 100)
	l, _ := Legal(short, 0)
	*l.ShortAllInTo = 999
	l2, _ := Legal(short, 0)
	if *l2.ShortAllInTo == 999 {
		t.Fatal("Legal共享")
	}
	s = finishCalling(t, s)
	r, _ := Result(s)
	r.Pots[0].Eligible[0] = 8
	r.Pots[0].Winners[0] = 8
	r.Pots[0].Payouts[8] = 999
	r.Refunds[8] = 999
	r.Payouts[8] = 999
	r.HoleCards[0] = [2]Card{"As", "Ah"}
	r.FinalStack[0] = 999
	r2, _ := Result(s)
	if r2.Pots[0].Eligible[0] == 8 || r2.Pots[0].Winners[0] == 8 || r2.Pots[0].Payouts[8] != 0 || r2.Refunds[8] != 0 || r2.Payouts[8] != 0 || r2.FinalStack[0] == 999 || r2.HoleCards[0] == r.HoleCards[0] {
		t.Fatal("Result共享")
	}
}

func TestConcurrentBranchIsolation(t *testing.T) {
	s := hand(t, 100, 100, 100)
	before, _ := EncodeState(s)
	var wg sync.WaitGroup
	errors := make(chan error, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			branch := s
			for Public(branch).Actor != nil {
				p := Public(branch)
				l, err := Legal(branch, *p.Actor)
				if err != nil {
					errors <- err
					return
				}
				kind := ActionKind("call")
				if l.CanCheck {
					kind = "check"
				}
				branch, err = Apply(branch, *p.Actor, Action{Kind: kind})
				if err != nil {
					errors <- err
					return
				}
				// 修改各分支返回值，不能影响其他分支或原状态。
				view := Public(branch)
				view.Players[0].Stack = 999
			}
			r, _ := Result(branch)
			r.FinalStack[0] = 999
			r.Pots[0].Payouts[0] = 999
		}()
	}
	wg.Wait()
	close(errors)
	for err := range errors {
		t.Fatal(err)
	}
	after, _ := EncodeState(s)
	if !bytes.Equal(before, after) {
		t.Fatal("并发分支修改原状态")
	}
}

func TestInitializationValidation(t *testing.T) {
	d := StandardDeck()
	seen := map[Card]bool{}
	for _, c := range d {
		if seen[c] {
			t.Fatal(c)
		}
		seen[c] = true
	}
	if len(seen) != 52 {
		t.Fatal(len(seen))
	}
	base := HandConfig{Players: []PlayerInit{{0, 100}, {1, 100}}, Button: 0, SmallBlind: 5, BigBlind: 10}
	tests := []struct {
		name string
		cfg  HandConfig
		deck Deck
	}{
		{"one", HandConfig{Players: []PlayerInit{{0, 100}}, Button: 0, SmallBlind: 5, BigBlind: 10}, d},
		{"duplicate_seat", HandConfig{Players: []PlayerInit{{0, 100}, {0, 100}}, Button: 0, SmallBlind: 5, BigBlind: 10}, d},
		{"seat_range", HandConfig{Players: []PlayerInit{{0, 100}, {9, 100}}, Button: 0, SmallBlind: 5, BigBlind: 10}, d},
		{"negative", HandConfig{Players: []PlayerInit{{0, -1}, {1, 100}}, Button: 1, SmallBlind: 5, BigBlind: 10}, d},
		{"total_limit", HandConfig{Players: []PlayerInit{{0, 1e12}, {1, 1}}, Button: 0, SmallBlind: 5, BigBlind: 10}, d},
		{"overflow", HandConfig{Players: []PlayerInit{{0, 1 << 62}, {1, 1 << 62}}, Button: 0, SmallBlind: 5, BigBlind: 10}, d},
		{"only_one_funded", HandConfig{Players: []PlayerInit{{0, 100}, {1, 0}}, Button: 0, SmallBlind: 5, BigBlind: 10}, d},
		{"ineligible_button", HandConfig{Players: []PlayerInit{{0, 0}, {1, 100}, {2, 100}}, Button: 0, SmallBlind: 5, BigBlind: 10}, d},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, e := NewHand(tc.cfg, tc.deck); e == nil {
				t.Fatal("接受非法配置")
			}
		})
	}
	for _, blinds := range [][2]Chips{{0, 10}, {5, 5}, {10, 5}, {-1, 10}, {5, 1e12 + 1}} {
		cfg := base
		cfg.SmallBlind = blinds[0]
		cfg.BigBlind = blinds[1]
		if _, e := NewHand(cfg, d); e == nil {
			t.Fatal(blinds)
		}
	}
	for _, bad := range []Card{"", "10c", "ac", "AX", "As ", "1c"} {
		deck := d
		deck[0] = bad
		if _, e := NewHand(base, deck); e == nil {
			t.Fatal(bad)
		}
	}
	d[0] = d[1]
	if _, e := NewHand(base, d); e == nil {
		t.Fatal("重复牌")
	}
	cfg := base
	cfg.Players = nil
	for i := 0; i < 10; i++ {
		cfg.Players = append(cfg.Players, PlayerInit{Seat(i), 100})
	}
	if _, e := NewHand(cfg, StandardDeck()); e == nil {
		t.Fatal("10座位")
	}
	if _, e := NewHand(HandConfig{Players: []PlayerInit{{0, 5e11}, {1, 5e11}}, SmallBlind: 5, BigBlind: 10}, StandardDeck()); e != nil {
		t.Fatal("上限应可接受", e)
	}
}

func TestActionValidation(t *testing.T) {
	s := hand(t, 100, 100)
	for _, a := range []Action{{"check", 0}, {"call", 1}, {"fold", 1}, {"raise_to", -1}, {"raise_to", 101}, {"raise_to", 10}, {"unknown", 0}} {
		if _, e := Apply(s, 0, a); e == nil {
			t.Fatal(a)
		}
	}
	if _, e := Apply(s, 1, Action{"call", 0}); e == nil {
		t.Fatal("非actor行动")
	}
	if _, e := Legal(s, 9); e == nil {
		t.Fatal("非法座位")
	}
	s = act(t, s, "call", 0)
	if _, e := Apply(s, 1, Action{"call", 0}); e == nil {
		t.Fatal("零欠注call")
	}
}

func TestSnapshotValidationAndResume(t *testing.T) {
	s := hand(t, 100, 100, 100)
	s = act(t, s, "raise_to", 25)
	b, e := EncodeState(s)
	if e != nil {
		t.Fatal(e)
	}
	restored, e := DecodeState(b)
	if e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(Public(s), Public(restored)) {
		t.Fatal("恢复状态不一致")
	}
	a := finishCalling(t, s)
	z := finishCalling(t, restored)
	ar, _ := Result(a)
	zr, _ := Result(z)
	if !reflect.DeepEqual(ar, zr) {
		t.Fatal("恢复结算不一致")
	}
	b, e = EncodeState(a)
	if e != nil {
		t.Fatal(e)
	}
	restored, e = DecodeState(b)
	if e != nil {
		t.Fatal(e)
	}
	rr, _ := Result(restored)
	if !reflect.DeepEqual(ar, rr) {
		t.Fatal("终局恢复不一致")
	}
	var doc map[string]json.RawMessage
	json.Unmarshal(b, &doc)
	doc["version"] = json.RawMessage("999")
	bad, _ := json.Marshal(doc)
	if _, e := DecodeState(bad); e == nil {
		t.Fatal("接受未知版本")
	}
	for _, bad := range [][]byte{nil, []byte("{}"), []byte("null"), []byte("{\"version\":1}"), append(append([]byte{}, b...), []byte(" {}")...)} {
		if _, e := DecodeState(bad); e == nil {
			t.Fatal("接受坏快照")
		}
	}
	doc = nil
	json.Unmarshal(b, &doc)
	doc["unknown"] = json.RawMessage("1")
	bad, _ = json.Marshal(doc)
	if _, e := DecodeState(bad); e == nil {
		t.Fatal("接受未知字段")
	}
	doc = nil
	json.Unmarshal(b, &doc)
	doc["actions"] = json.RawMessage(`[{"seat":8,"action":{"Kind":"raise_to","To":999}}]`)
	bad, _ = json.Marshal(doc)
	if _, e := DecodeState(bad); e == nil {
		t.Fatal("接受非法行动快照")
	}
	if _, e := EncodeState(State{}); e == nil {
		t.Fatal("接受零值State")
	}
	// Go的JSON数组解码会截断超长数组；快照必须额外检查恰好52张。
	doc = nil
	json.Unmarshal(b, &doc)
	var deck []Card
	json.Unmarshal(doc["deck"], &deck)
	deck = append(deck, "2c")
	doc["deck"], _ = json.Marshal(deck)
	bad, _ = json.Marshal(doc)
	if _, e := DecodeState(bad); e == nil {
		t.Fatal("接受53张牌快照")
	}
	doc = nil
	json.Unmarshal(b, &doc)
	delete(doc, "actions")
	bad, _ = json.Marshal(doc)
	if _, e := DecodeState(bad); e == nil {
		t.Fatal("接受缺少行动日志的快照")
	}
	doc = nil
	json.Unmarshal(b, &doc)
	doc["actions"] = json.RawMessage("null")
	bad, _ = json.Marshal(doc)
	if _, e := DecodeState(bad); e == nil {
		t.Fatal("接受null行动日志")
	}
	bad = append([]byte(`{"version":999,`), b[1:]...)
	if _, e := DecodeState(bad); e == nil {
		t.Fatal("接受重复版本键")
	}
}
