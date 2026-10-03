package poker

import "testing"

// TestPrivateHoleAccess 验证窄接口只在本人座位读取已发牌：
// 非 actor、已弃牌、已全押、手牌完成都能读取自己的牌；未发牌/非法座位拒绝，
// 且绝不返回其他座位底牌。
func TestPrivateHoleAccess(t *testing.T) {
	newHand := func(t *testing.T) State {
		t.Helper()
		s, err := NewHand(HandConfig{Players: []PlayerInit{{Seat: 0, Stack: 1000}, {Seat: 1, Stack: 1000}}, Button: 0, SmallBlind: 5, BigBlind: 10}, StandardDeck())
		if err != nil {
			t.Fatal(err)
		}
		return s
	}

	t.Run("actor_and_non_actor_read_own_cards", func(t *testing.T) {
		s := newHand(t)
		public := Public(s)
		if public.Actor == nil {
			t.Fatal("no actor")
		}
		for seat := Seat(0); seat <= 1; seat++ {
			got, err := PrivateHole(s, seat)
			if err != nil {
				t.Fatalf("seat %d: %v", seat, err)
			}
			if got != s.hole[seat] {
				t.Fatalf("seat %d returned wrong cards", seat)
			}
			if got[0] == "" || got[1] == "" {
				t.Fatalf("seat %d returned empty cards", seat)
			}
		}
		if s.hole[0] == s.hole[1] {
			t.Fatal("distinct seats returned same cards")
		}
	})

	t.Run("folded_allin_complete_still_readable", func(t *testing.T) {
		for _, kind := range []ActionKind{"fold", "call", "raise_to"} {
			s := newHand(t)
			public := Public(s)
			actor := *public.Actor
			action := Action{Kind: kind}
			if kind == "raise_to" {
				action.To = 20
			}
			next, err := Apply(s, actor, action)
			if err != nil {
				t.Fatalf("%s: %v", kind, err)
			}
			if _, err := PrivateHole(next, actor); err != nil {
				t.Fatalf("%s: own cards unavailable after action: %v", kind, err)
			}
		}
		// 全押后仍可读取自己的牌。
		s := newHand(t)
		public := Public(s)
		actor := *public.Actor
		next, err := Apply(s, actor, Action{Kind: "raise_to", To: 1000})
		if err != nil {
			t.Fatal(err)
		}
		for PlayerStatus, _ := range map[PlayerStatus]bool{next.players[actor].Status: true} {
			_ = PlayerStatus
		}
		if _, err := PrivateHole(next, actor); err != nil {
			t.Fatalf("all-in own cards unavailable: %v", err)
		}
	})

	t.Run("illegal_seat_and_undealt_refused", func(t *testing.T) {
		s := newHand(t)
		if _, err := PrivateHole(s, Seat(2)); err == nil {
			t.Fatal("undealt seat accepted")
		}
		if _, err := PrivateHole(s, Seat(9)); err == nil {
			t.Fatal("out-of-range seat accepted")
		}
		if _, err := PrivateHole(State{}, Seat(0)); err == nil {
			t.Fatal("invalid state accepted")
		}
		// Stack=0 的座位存在但未发牌。
		s2, err := NewHand(HandConfig{Players: []PlayerInit{{Seat: 0, Stack: 1000}, {Seat: 1, Stack: 1000}, {Seat: 2, Stack: 0}}, Button: 0, SmallBlind: 5, BigBlind: 10}, StandardDeck())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := PrivateHole(s2, Seat(2)); err == nil {
			t.Fatal("zero-stack undealt seat accepted")
		}
	})
}
