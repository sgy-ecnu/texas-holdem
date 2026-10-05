package poker

import "sort"

// handCategoryNames 是牌型等级 0-8 的中文名，与 score 的 category 一一对应。
var handCategoryNames = [9]string{"高牌", "一对", "两对", "三条", "顺子", "同花", "葫芦", "四条", "同花顺"}

func handCategoryName(category int) string {
	if category < 0 || category >= len(handCategoryNames) {
		return ""
	}
	return handCategoryNames[category]
}

// rankCards 返回一组牌的最高五张牌分值。保留为薄封装，胜负判定语义不变。
func rankCards(cards []Card) uint64 {
	value, _, _ := rankBest(cards)
	return value
}

// rankBest 枚举所有五张组合，返回最高分值、对应牌型等级与最佳五张牌。
// 相同分值时取枚举顺序中的第一组（组合按索引字典序稳定），保证结果可复现；
// 不改变 rankCards 的分值语义。
func rankBest(cards []Card) (uint64, int, [5]Card) {
	var best uint64
	var category int
	var bestCards [5]Card
	for a := 0; a < len(cards)-4; a++ {
		for b := a + 1; b < len(cards)-3; b++ {
			for c := b + 1; c < len(cards)-2; c++ {
				for d := c + 1; d < len(cards)-1; d++ {
					for e := d + 1; e < len(cards); e++ {
						hand := [5]Card{cards[a], cards[b], cards[c], cards[d], cards[e]}
						v, cat := evalFive(hand)
						if v > best {
							best, category, bestCards = v, cat, hand
						}
					}
				}
			}
		}
	}
	return best, category, bestCards
}

func score(category int, kickers ...int) uint64 {
	v := uint64(category)
	for i := 0; i < 5; i++ {
		v *= 15
		if i < len(kickers) {
			v += uint64(kickers[i])
		}
	}
	return v
}

// rankFive 返回五张牌的分值；保留为薄封装。
func rankFive(cards [5]Card) uint64 {
	v, _ := evalFive(cards)
	return v
}

// evalFive 计算五张牌的分值与牌型等级（0 高牌 .. 8 同花顺）。
func evalFive(cards [5]Card) (uint64, int) {
	var counts [15]int
	flush := true
	for _, c := range cards {
		counts[rankOf(c[0])]++
		if c[1] != cards[0][1] {
			flush = false
		}
	}
	ranks := []int{}
	for r := 14; r >= 2; r-- {
		for n := 0; n < counts[r]; n++ {
			ranks = append(ranks, r)
		}
	}
	straight := 0
	for top := 14; top >= 5; top-- {
		ok := true
		for offset := 0; offset < 5; offset++ {
			r := top - offset
			if r == 1 {
				r = 14
			}
			if counts[r] == 0 {
				ok = false
				break
			}
		}
		if ok {
			straight = top
			break
		}
	}
	if flush && straight > 0 {
		return score(8, straight), 8
	}
	four, three := 0, 0
	pairs := []int{}
	singles := []int{}
	for r := 14; r >= 2; r-- {
		switch counts[r] {
		case 4:
			four = r
		case 3:
			three = r
		case 2:
			pairs = append(pairs, r)
		case 1:
			singles = append(singles, r)
		}
	}
	if four > 0 {
		return score(7, four, singles[0]), 7
	}
	if three > 0 && len(pairs) > 0 {
		return score(6, three, pairs[0]), 6
	}
	if flush {
		return score(5, ranks...), 5
	}
	if straight > 0 {
		return score(4, straight), 4
	}
	if three > 0 {
		return score(3, append([]int{three}, singles...)...), 3
	}
	if len(pairs) == 2 {
		return score(2, pairs[0], pairs[1], singles[0]), 2
	}
	if len(pairs) == 1 {
		return score(1, append(pairs, singles...)...), 1
	}
	return score(0, ranks...), 0
}

func (s *State) settle(reason string) {
	r := HandResult{Reason: reason, Pots: []PotAward{}, Refunds: map[Seat]Chips{}, Payouts: map[Seat]Chips{}, HoleCards: map[Seat][2]Card{}, FinalStack: map[Seat]Chips{}, HandRanks: map[Seat]string{}, BestFive: map[Seat][]Card{}}
	levels := []Chips{}
	seen := map[Chips]bool{}
	var ranks [9]uint64
	for i, p := range s.players {
		if !s.present[i] {
			continue
		}
		if p.Committed > 0 && !seen[p.Committed] {
			seen[p.Committed] = true
			levels = append(levels, p.Committed)
		}
		// 弃牌保密：只有实际参与摊牌的 active/all_in 座位才公开底牌与牌型；
		// uncontested（弃牌结束）不产生任何公共牌型，也不记录任何他人底牌。
		if reason == "showdown" && (p.Status == "active" || p.Status == "all_in") {
			cards := append([]Card{}, s.board...)
			cards = append(cards, s.hole[i][:]...)
			value, category, best := rankBest(cards)
			ranks[i] = value
			r.HoleCards[Seat(i)] = s.hole[i]
			r.HandRanks[Seat(i)] = handCategoryName(category)
			r.BestFive[Seat(i)] = best[:]
		}
	}
	sort.Slice(levels, func(i, j int) bool { return levels[i] < levels[j] })
	var previous Chips
	for _, level := range levels {
		contributors := []Seat{}
		eligible := []Seat{}
		for i, p := range s.players {
			if s.present[i] && p.Committed >= level {
				contributors = append(contributors, Seat(i))
				if p.Status == "active" || p.Status == "all_in" {
					eligible = append(eligible, Seat(i))
				}
			}
		}
		amount := (level - previous) * Chips(len(contributors))
		previous = level
		// 最高的唯一出资层先退款；弃牌资金仍参与其余池。
		if len(contributors) == 1 {
			r.Refunds[contributors[0]] += amount
			continue
		}
		pot := PotAward{Amount: amount, Eligible: eligible, Winners: []Seat{}, Payouts: map[Seat]Chips{}}
		if reason == "uncontested" {
			for i, p := range s.players {
				if s.present[i] && (p.Status == "active" || p.Status == "all_in") {
					pot.Eligible = []Seat{Seat(i)}
					pot.Winners = []Seat{Seat(i)}
					break
				}
			}
		} else {
			var best uint64
			for _, seat := range eligible {
				if ranks[seat] > best {
					best = ranks[seat]
				}
			}
			for _, seat := range eligible {
				if ranks[seat] == best {
					pot.Winners = append(pot.Winners, seat)
				}
			}
		}
		share := amount / Chips(len(pot.Winners))
		remainder := amount % Chips(len(pot.Winners))
		for _, seat := range pot.Winners {
			pot.Payouts[seat] = share
		}
		// 每个池独立从button后顺时针发放余数。
		for offset := 1; offset <= 9 && remainder > 0; offset++ {
			seat := Seat((int(s.button) + offset) % 9)
			if _, ok := pot.Payouts[seat]; ok {
				pot.Payouts[seat]++
				remainder--
			}
		}
		for seat, n := range pot.Payouts {
			r.Payouts[seat] += n
		}
		r.Pots = append(r.Pots, pot)
	}
	for i := range s.players {
		if s.present[i] {
			p := &s.players[i]
			p.Stack += r.Payouts[Seat(i)] + r.Refunds[Seat(i)]
			p.StreetBet = 0
			p.Committed = 0
			r.FinalStack[Seat(i)] = p.Stack
		}
	}
	s.street = "complete"
	s.currentBet = 0
	s.hasActor = false
	s.pending = [9]bool{}
	s.result = &r
}
