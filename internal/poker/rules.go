package poker

import "errors"

const chipLimit Chips = 1_000_000_000_000

func StandardDeck() Deck {
	var d Deck
	i := 0
	for _, r := range "23456789TJQKA" {
		for _, s := range "cdhs" {
			d[i] = Card(string([]rune{r, s}))
			i++
		}
	}
	return d
}

func validDeck(d Deck) bool {
	seen := map[Card]bool{}
	for _, c := range d {
		if len(c) != 2 || rankOf(c[0]) == 0 || !isSuit(c[1]) || seen[c] {
			return false
		}
		seen[c] = true
	}
	return true
}
func isSuit(b byte) bool { return b == 'c' || b == 'd' || b == 'h' || b == 's' }
func rankOf(b byte) int {
	for i, r := range "23456789TJQKA" {
		if byte(r) == b {
			return i + 2
		}
	}
	return 0
}

func NewHand(cfg HandConfig, deck Deck) (State, error) {
	if len(cfg.Players) < 2 || len(cfg.Players) > 9 || cfg.SmallBlind <= 0 || cfg.SmallBlind >= cfg.BigBlind || cfg.BigBlind > chipLimit || !validDeck(deck) {
		return State{}, errors.New("INVALID_CONFIG")
	}
	s := State{valid: true, cfg: cfg, deck: deck, button: cfg.Button, street: "preflop", currentBet: cfg.BigBlind, lastFullRaise: cfg.BigBlind, board: []Card{}, history: []recordedAction{}}
	s.cfg.Players = append([]PlayerInit{}, cfg.Players...)
	funded := 0
	for _, v := range cfg.Players {
		if v.Seat > 8 || s.present[v.Seat] || v.Stack < 0 || v.Stack > chipLimit-s.total {
			return State{}, errors.New("INVALID_PLAYERS")
		}
		s.total += v.Stack
		s.present[v.Seat] = true
		s.players[v.Seat] = Player{Seat: v.Seat, Stack: v.Stack, Status: "out"}
		if v.Stack > 0 {
			funded++
			s.players[v.Seat].Status = "active"
			s.dealt[v.Seat] = true
			s.pending[v.Seat] = true
		}
	}
	if funded < 2 || cfg.Button > 8 || !s.dealt[cfg.Button] {
		return State{}, errors.New("INVALID_BUTTON")
	}
	if funded == 2 {
		s.sb = cfg.Button
	} else {
		s.sb = s.next(cfg.Button, func(i Seat) bool { return s.dealt[i] })
	}
	s.bb = s.next(s.sb, func(i Seat) bool { return s.dealt[i] })
	// 从button后开始，两轮逐座发牌；短盲仍然是参赛者。
	for round := 0; round < 2; round++ {
		seat := cfg.Button
		for n := 0; n < funded; n++ {
			seat = s.next(seat, func(i Seat) bool { return s.dealt[i] })
			s.hole[seat][round] = s.draw()
		}
	}
	s.pay(s.sb, minChips(cfg.SmallBlind, s.players[s.sb].Stack))
	s.pay(s.bb, minChips(cfg.BigBlind, s.players[s.bb].Stack))
	s.advance(s.bb)
	return s, nil
}

func minChips(a, b Chips) Chips {
	if a < b {
		return a
	}
	return b
}
func (s State) next(from Seat, predicate func(Seat) bool) Seat {
	for n := 1; n <= 9; n++ {
		i := Seat((int(from) + n) % 9)
		if predicate(i) {
			return i
		}
	}
	return from
}
func (s *State) draw() Card { c := s.deck[s.nextCard]; s.nextCard++; return c }
func (s *State) pay(seat Seat, n Chips) {
	p := &s.players[seat]
	p.Stack -= n
	p.StreetBet += n
	p.Committed += n
	if p.Stack == 0 {
		p.Status = "all_in"
		s.pending[seat] = false
	}
}

func Legal(s State, seat Seat) (LegalActions, error) {
	if !s.valid || seat > 8 || !s.hasActor || s.actor != seat || s.result != nil || s.players[seat].Status != "active" {
		return LegalActions{}, errors.New("NOT_ACTOR")
	}
	p := s.players[seat]
	owed := s.currentBet - p.StreetBet
	if owed < 0 {
		owed = 0
	}
	l := LegalActions{CanFold: true, CanCheck: owed == 0, CanCall: owed > 0, CallAmount: minChips(owed, p.Stack), MinRaiseTo: s.currentBet + s.lastFullRaise, MaxRaiseTo: p.StreetBet + p.Stack}
	// 保存玩家自己上次行动时的完整加注阈值，而非仅检查最后一次动作。
	right := !s.acted[seat] || s.currentBet-s.faced[seat] >= s.reopenSize[seat]
	responder := false
	for i, q := range s.players {
		if Seat(i) != seat && s.present[i] && q.Status == "active" && q.Stack+q.StreetBet > s.currentBet {
			responder = true
			break
		}
	}
	l.CanRaise = right && responder && l.MaxRaiseTo > s.currentBet
	if l.CanRaise && l.MaxRaiseTo < l.MinRaiseTo {
		n := l.MaxRaiseTo
		l.ShortAllInTo = &n
	}
	return l, nil
}

// PrivateHole 是真人视角读取自己底牌的窄接口：状态有效、座位合法且本手确实
// 发过牌时返回该座两张牌。它只返回请求座位自己的牌，绝不返回其他座位底牌、
// 牌堆或合法动作；不改变 Observe/Legal/Apply 的既有语义。
func PrivateHole(s State, seat Seat) ([2]Card, error) {
	if !s.valid || seat > 8 || !s.present[seat] || !s.dealt[seat] {
		return [2]Card{}, errors.New("HOLE_UNAVAILABLE")
	}
	return s.hole[seat], nil
}

func Apply(s State, seat Seat, action Action) (State, error) {
	l, err := Legal(s, seat)
	if err != nil {
		return State{}, err
	}
	if action.To < 0 || action.To > chipLimit || (action.Kind != "raise_to" && action.To != 0) {
		return State{}, errors.New("INVALID_ACTION")
	}
	switch action.Kind {
	case "fold":
		if !l.CanFold {
			return State{}, errors.New("ILLEGAL_FOLD")
		}
	case "check":
		if !l.CanCheck {
			return State{}, errors.New("ILLEGAL_CHECK")
		}
	case "call":
		if !l.CanCall {
			return State{}, errors.New("ILLEGAL_CALL")
		}
	case "raise_to":
		if !l.CanRaise || action.To <= s.currentBet || action.To > l.MaxRaiseTo || (action.To < l.MinRaiseTo && action.To != l.MaxRaiseTo) {
			return State{}, errors.New("ILLEGAL_RAISE")
		}
	default:
		return State{}, errors.New("INVALID_ACTION")
	}
	n := s.clone()
	n.pending[seat] = false
	switch action.Kind {
	case "fold":
		n.players[seat].Status = "folded"
	case "call":
		n.pay(seat, l.CallAmount)
	case "raise_to":
		size := action.To - n.currentBet
		if size >= n.lastFullRaise {
			n.lastFullRaise = size
		}
		n.pay(seat, action.To-n.players[seat].StreetBet)
		n.currentBet = action.To
		for i, p := range n.players {
			if Seat(i) != seat && n.present[i] && p.Status == "active" && p.StreetBet < n.currentBet {
				n.pending[i] = true
			}
		}
	}
	n.acted[seat] = true
	n.faced[seat] = n.currentBet
	n.reopenSize[seat] = n.lastFullRaise
	n.history = append(n.history, recordedAction{seat, action})
	n.advance(seat)
	return n, nil
}

func (s *State) advance(after Seat) {
	for {
		live, active := 0, 0
		var only Seat
		for i, p := range s.players {
			if !s.present[i] {
				continue
			}
			if p.Status == "active" || p.Status == "all_in" {
				live++
			}
			if p.Status == "active" {
				active++
				only = Seat(i)
			}
		}
		if live == 1 {
			s.settle("uncontested")
			return
		}
		// 一名可行动者只须处理尚欠下注，不对全押者做无意义check。
		if active <= 1 {
			if active == 1 && s.players[only].StreetBet < s.currentBet {
				s.actor = only
				s.hasActor = true
				s.pending[only] = true
				return
			}
			for s.street != "river" {
				s.dealStreet()
			}
			s.settle("showdown")
			return
		}
		found := false
		for i, p := range s.players {
			if s.present[i] && p.Status == "active" && s.pending[i] {
				found = true
				break
			}
		}
		if found {
			s.actor = s.next(after, func(i Seat) bool { return s.present[i] && s.players[i].Status == "active" && s.pending[i] })
			s.hasActor = true
			return
		}
		if s.street == "river" {
			s.settle("showdown")
			return
		}
		s.dealStreet()
		after = s.button
	}
}

func (s *State) dealStreet() {
	s.draw() // 每街烧一张。
	n := 1
	switch s.street {
	case "preflop":
		s.street = "flop"
		n = 3
	case "flop":
		s.street = "turn"
	case "turn":
		s.street = "river"
	}
	for i := 0; i < n; i++ {
		s.board = append(s.board, s.draw())
	}
	s.currentBet = 0
	s.lastFullRaise = s.cfg.BigBlind
	s.acted = [9]bool{}
	s.faced = [9]Chips{}
	s.reopenSize = [9]Chips{}
	s.pending = [9]bool{}
	for i := range s.players {
		if s.present[i] {
			s.players[i].StreetBet = 0
			s.pending[i] = s.players[i].Status == "active"
		}
	}
}
