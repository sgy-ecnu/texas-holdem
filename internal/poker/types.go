// Package poker 实现无IO、无随机源的德州扑克纯规则引擎。
package poker

type Chips int64
type Seat uint8
type Card string
type Deck [52]Card
type Street string
type PlayerStatus string
type ActionKind string

type Player struct {
	Seat      Seat
	Stack     Chips
	StreetBet Chips
	Committed Chips
	Status    PlayerStatus
}
type PlayerInit struct {
	Seat  Seat
	Stack Chips
}
type HandConfig struct {
	Players    []PlayerInit
	Button     Seat
	SmallBlind Chips
	BigBlind   Chips
}
type Action struct {
	Kind ActionKind
	To   Chips
}
type LegalActions struct {
	CanFold      bool
	CanCheck     bool
	CanCall      bool
	CallAmount   Chips
	CanRaise     bool
	MinRaiseTo   Chips
	MaxRaiseTo   Chips
	ShortAllInTo *Chips
}
type PublicState struct {
	Street     Street
	Button     Seat
	SmallBlind Seat
	BigBlind   Seat
	Actor      *Seat
	Board      []Card
	Pot        Chips
	CurrentBet Chips
	Players    []Player
}
type Observation struct {
	State PublicState
	Self  Seat
	Hole  [2]Card
	Legal LegalActions
}
type PotAward struct {
	Amount   Chips
	Eligible []Seat
	Winners  []Seat
	Payouts  map[Seat]Chips
}
type HandResult struct {
	Reason     string
	Pots       []PotAward
	Refunds    map[Seat]Chips
	Payouts    map[Seat]Chips
	HoleCards  map[Seat][2]Card
	FinalStack map[Seat]Chips
	// HandRanks 是每个实际参与摊牌玩家的牌型中文名（高牌..同花顺）；
	// BestFive 是对应的最佳五张牌。仅 reason=="showdown" 时非空，且只覆盖
	// active/all_in 的座位，弃牌者不公开底牌、不产生牌型。
	HandRanks map[Seat]string
	BestFive  map[Seat][]Card
}

// State的值拷贝仅在内部使用；公开操作通过clone切断所有可变引用。
type State struct {
	valid         bool
	cfg           HandConfig
	deck          Deck
	nextCard      int
	players       [9]Player
	present       [9]bool
	hole          [9][2]Card
	dealt         [9]bool
	button        Seat
	sb            Seat
	bb            Seat
	street        Street
	board         []Card
	actor         Seat
	hasActor      bool
	currentBet    Chips
	lastFullRaise Chips
	acted         [9]bool
	faced         [9]Chips
	reopenSize    [9]Chips
	pending       [9]bool
	total         Chips
	history       []recordedAction
	result        *HandResult
}

type recordedAction struct {
	Seat   Seat   `json:"seat"`
	Action Action `json:"action"`
}

func copyChips(src map[Seat]Chips) map[Seat]Chips {
	dst := make(map[Seat]Chips, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}
func copyResult(src HandResult) HandResult {
	dst := src
	dst.Refunds = copyChips(src.Refunds)
	dst.Payouts = copyChips(src.Payouts)
	dst.FinalStack = copyChips(src.FinalStack)
	dst.HoleCards = make(map[Seat][2]Card, len(src.HoleCards))
	for k, v := range src.HoleCards {
		dst.HoleCards[k] = v
	}
	dst.HandRanks = make(map[Seat]string, len(src.HandRanks))
	for k, v := range src.HandRanks {
		dst.HandRanks[k] = v
	}
	dst.BestFive = make(map[Seat][]Card, len(src.BestFive))
	for k, v := range src.BestFive {
		dst.BestFive[k] = append([]Card{}, v...)
	}
	dst.Pots = make([]PotAward, len(src.Pots))
	for i, p := range src.Pots {
		dst.Pots[i] = p
		dst.Pots[i].Eligible = append([]Seat{}, p.Eligible...)
		dst.Pots[i].Winners = append([]Seat{}, p.Winners...)
		dst.Pots[i].Payouts = copyChips(p.Payouts)
	}
	return dst
}
func (s State) clone() State {
	s.cfg.Players = append([]PlayerInit{}, s.cfg.Players...)
	s.board = append([]Card{}, s.board...)
	s.history = append([]recordedAction{}, s.history...)
	if s.result != nil {
		r := copyResult(*s.result)
		s.result = &r
	}
	return s
}

func Public(s State) PublicState {
	p := PublicState{Street: s.street, Button: s.button, SmallBlind: s.sb, BigBlind: s.bb, Board: append([]Card{}, s.board...), CurrentBet: s.currentBet, Players: []Player{}}
	if s.hasActor {
		a := s.actor
		p.Actor = &a
	}
	for i, v := range s.players {
		if s.present[i] {
			p.Players = append(p.Players, v)
			p.Pot += v.Committed
		}
	}
	return p
}
func Observe(s State, seat Seat) (Observation, error) {
	l, err := Legal(s, seat)
	if err != nil {
		return Observation{}, err
	}
	return Observation{State: Public(s), Self: seat, Hole: s.hole[seat], Legal: l}, nil
}
func Result(s State) (HandResult, bool) {
	if s.result == nil {
		return HandResult{}, false
	}
	return copyResult(*s.result), true
}
