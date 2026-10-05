package poker

import (
	"reflect"
	"sort"
	"testing"
)

// sortedCards 返回与牌序无关的稳定表示，用于比较"最佳五张"的集合。
func sortedCards(a []Card) []Card {
	b := append([]Card{}, a...)
	sort.Slice(b, func(i, j int) bool { return b[i] < b[j] })
	return b
}

// TestHandCategoryNamesAndBestFive 覆盖 0-8 全部牌型：断言中文牌型名、最佳五张，
// 并证明 rankCards 的分值语义未被 rankBest 重构改变。
func TestHandCategoryNamesAndBestFive(t *testing.T) {
	cases := []struct {
		name     string
		cards    []Card
		wantName string
		wantBest []string
	}{
		{"high_card", cards("Ac", "Kd", "9h", "7s", "5c", "3d", "2h"), "高牌", []string{"Ac", "Kd", "9h", "7s", "5c"}},
		{"one_pair", cards("Ac", "Ad", "9h", "7s", "5c", "3d", "2h"), "一对", []string{"Ac", "Ad", "9h", "7s", "5c"}},
		{"two_pair", cards("Ac", "Ad", "9h", "9s", "5c", "3d", "2h"), "两对", []string{"Ac", "Ad", "9h", "9s", "5c"}},
		{"three_of_a_kind", cards("Ac", "Ad", "Ah", "7s", "5c", "3d", "2h"), "三条", []string{"Ac", "Ad", "Ah", "7s", "5c"}},
		{"straight", cards("Ac", "2d", "3h", "4s", "5c", "9d", "Th"), "顺子", []string{"Ac", "2d", "3h", "4s", "5c"}},
		{"flush", cards("Ac", "Jc", "9c", "7c", "5c", "3d", "2h"), "同花", []string{"Ac", "Jc", "9c", "7c", "5c"}},
		{"full_house", cards("Ac", "Ad", "Ah", "7s", "7c", "3d", "2h"), "葫芦", []string{"Ac", "Ad", "Ah", "7s", "7c"}},
		{"four_of_a_kind", cards("Ac", "Ad", "Ah", "As", "5c", "3d", "2h"), "四条", []string{"Ac", "Ad", "Ah", "As", "5c"}},
		{"straight_flush", cards("Ac", "2c", "3c", "4c", "5c", "9d", "Th"), "同花顺", []string{"Ac", "2c", "3c", "4c", "5c"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			value, category, best := rankBest(tc.cards)
			if got := handCategoryName(category); got != tc.wantName {
				t.Fatalf("category %d name got %q want %q", category, got, tc.wantName)
			}
			if value != rankCards(tc.cards) {
				t.Fatalf("rankBest value %d != rankCards %d", value, rankCards(tc.cards))
			}
			if !reflect.DeepEqual(sortedCards(best[:]), sortedCards(cards(tc.wantBest...))) {
				t.Fatalf("best five got %v want %v", best, tc.wantBest)
			}
			// 最佳五张必须是输入的 5 张不重复子集。
			pool := map[Card]int{}
			for _, c := range tc.cards {
				pool[c]++
			}
			for _, c := range best {
				pool[c]--
				if pool[c] < 0 {
					t.Fatalf("best five card %s not in input", c)
				}
			}
		})
	}
	// 等级名必须完整覆盖 0-8 且互不相同。
	seen := map[string]bool{}
	for i := 0; i < 9; i++ {
		name := handCategoryName(i)
		if name == "" || seen[name] {
			t.Fatalf("category %d has bad name %q", i, name)
		}
		seen[name] = true
	}
	if handCategoryName(-1) != "" || handCategoryName(9) != "" {
		t.Fatal("out-of-range category must return empty name")
	}
}

// foldPrivacyState 构造一个可控的摊牌/弃牌结束状态：所有座位 all_in，
// folded 指定座位改为 folded；reason 决定结算类型。
func foldPrivacyState(t *testing.T, reason string, folded map[int]bool) State {
	t.Helper()
	s := hand(t, 100, 100, 100)
	s.button = 0
	// 公共牌刻意避开按钮发牌留下的底牌，避免重复。
	s.board = cards("Qh", "Jd", "9c", "7s", "4h")
	s.street = "river"
	s.total = 0
	for i := range s.players {
		if !s.present[i] {
			continue
		}
		s.players[i].Stack = 0
		s.players[i].Committed = 30
		s.players[i].StreetBet = 30
		s.players[i].Status = "all_in"
		if folded[i] {
			s.players[i].Status = "folded"
		}
		s.total += 30
	}
	s.settle(reason)
	return s
}

// TestSettleFoldSecrecy 验证弃牌保密：
//   - showdown 只公开实际参与摊牌（active/all_in）座位的底牌与牌型；
//   - uncontested 不公开任何他人底牌，也不产生任何牌型。
func TestSettleFoldSecrecy(t *testing.T) {
	t.Run("showdown_hides_folded", func(t *testing.T) {
		s := foldPrivacyState(t, "showdown", map[int]bool{2: true})
		r, ok := Result(s)
		if !ok {
			t.Fatal("no result")
		}
		if _, leaked := r.HoleCards[2]; leaked {
			t.Fatal("folded player hole cards leaked")
		}
		if _, leaked := r.HandRanks[2]; leaked {
			t.Fatal("folded player hand rank leaked")
		}
		if _, leaked := r.BestFive[2]; leaked {
			t.Fatal("folded player best five leaked")
		}
		if len(r.HoleCards) != 2 || len(r.HandRanks) != 2 || len(r.BestFive) != 2 {
			t.Fatalf("showdown reveal counts got hole=%d rank=%d best=%d", len(r.HoleCards), len(r.HandRanks), len(r.BestFive))
		}
		for _, seat := range []Seat{0, 1} {
			if _, ok := r.HoleCards[seat]; !ok {
				t.Fatalf("active seat %d hole cards missing", seat)
			}
			if r.HandRanks[seat] == "" {
				t.Fatalf("active seat %d hand rank missing", seat)
			}
			if len(r.BestFive[seat]) != 5 {
				t.Fatalf("active seat %d best five len=%d", seat, len(r.BestFive[seat]))
			}
		}
		// 底牌与最佳五张必须包含公共牌之外的私有信息，且牌型语义正确：
		// seat1 是口袋 2，摊牌胜出；seat0 仅有高牌。
		if r.HandRanks[1] != "一对" {
			t.Fatalf("seat1 rank got %q want 一对", r.HandRanks[1])
		}
		if r.HandRanks[0] != "高牌" {
			t.Fatalf("seat0 rank got %q want 高牌", r.HandRanks[0])
		}
	})
	t.Run("uncontested_hides_all", func(t *testing.T) {
		s := foldPrivacyState(t, "uncontested", map[int]bool{1: true, 2: true})
		r, ok := Result(s)
		if !ok {
			t.Fatal("no result")
		}
		if len(r.HoleCards) != 0 || len(r.HandRanks) != 0 || len(r.BestFive) != 0 {
			t.Fatalf("uncontested leaked cards/ranks: hole=%d rank=%d best=%d", len(r.HoleCards), len(r.HandRanks), len(r.BestFive))
		}
	})
}
