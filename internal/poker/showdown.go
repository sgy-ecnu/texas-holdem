package poker

import "sort"

// 五张牌编码按牌型、逐个踢脚字典序排序；枚举七选五取最大值。
func rankCards(cards []Card) uint64 {
	var best uint64
	for a := 0; a < len(cards)-4; a++ {
		for b := a + 1; b < len(cards)-3; b++ {
			for c := b + 1; c < len(cards)-2; c++ {
				for d := c + 1; d < len(cards)-1; d++ {
					for e := d + 1; e < len(cards); e++ {
						v := rankFive([5]Card{cards[a], cards[b], cards[c], cards[d], cards[e]})
						if v > best {
							best = v
						}
					}
				}
			}
		}
	}
	return best
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
func rankFive(cards [5]Card) uint64 {
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
		return score(8, straight)
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
		return score(7, four, singles[0])
	}
	if three > 0 && len(pairs) > 0 {
		return score(6, three, pairs[0])
	}
	if flush {
		return score(5, ranks...)
	}
	if straight > 0 {
		return score(4, straight)
	}
	if three > 0 {
		return score(3, append([]int{three}, singles...)...)
	}
	if len(pairs) == 2 {
		return score(2, pairs[0], pairs[1], singles[0])
	}
	if len(pairs) == 1 {
		return score(1, append(pairs, singles...)...)
	}
	return score(0, ranks...)
}

func (s *State) settle(reason string) {
	r := HandResult{Reason: reason, Pots: []PotAward{}, Refunds: map[Seat]Chips{}, Payouts: map[Seat]Chips{}, HoleCards: map[Seat][2]Card{}, FinalStack: map[Seat]Chips{}}
	levels := []Chips{}
	seen := map[Chips]bool{}
	var ranks [9]uint64
	for i, p := range s.players {
		if !s.present[i] {
			continue
		}
		if s.dealt[i] {
			r.HoleCards[Seat(i)] = s.hole[i]
		}
		if p.Committed > 0 && !seen[p.Committed] {
			seen[p.Committed] = true
			levels = append(levels, p.Committed)
		}
		if reason == "showdown" && (p.Status == "active" || p.Status == "all_in") {
			cards := append([]Card{}, s.board...)
			cards = append(cards, s.hole[i][:]...)
			ranks[i] = rankCards(cards)
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
