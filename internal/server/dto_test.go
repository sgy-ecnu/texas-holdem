package server

import (
	"encoding/json"
	"strings"
	"testing"
	"texas-holdem/internal/poker"
	"texas-holdem/internal/service"
)

func TestPublicDTOPrivacy(t *testing.T) {
	b, err := MarshalGame(service.GameView{ID: "game", Status: "created", Mode: "mock"})
	if err != nil {
		t.Fatal(err)
	}
	var v map[string]any
	if err = json.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"id", "revision", "status", "reason", "mode", "max_hands", "hands_completed", "live_calls_used", "max_live_calls", "storage_degraded", "seats", "hand", "last_action"} {
		if _, ok := v[key]; !ok {
			t.Fatalf("missing %s", key)
		}
	}
	if strings.Contains(string(b), "hole") || strings.Contains(string(b), "snapshot") {
		t.Fatal("private fields leaked")
	}
	if v["hand"] != nil || v["reason"] != nil || v["last_action"] != nil {
		t.Fatal("nullable fields must be null")
	}
	if _, ok := v["seats"].([]any); !ok {
		t.Fatal("arrays must be []")
	}
}

var potSeatJSONCases = []struct {
	name              string
	eligible, winners []poker.Seat
	wantEligible      string
	wantWinners       string
}{
	{"nonempty", []poker.Seat{0, 2, 8}, []poker.Seat{2}, "[0,2,8]", "[2]"},
	{"multiple_winners", []poker.Seat{0, 1, 2}, []poker.Seat{0, 2}, "[0,1,2]", "[0,2]"},
	{"empty", []poker.Seat{}, []poker.Seat{}, "[]", "[]"},
	{"nil", nil, nil, "[]", "[]"},
}

func assertPotSeatJSON(t *testing.T, data []byte, eligible, winners string) {
	t.Helper()
	var body struct {
		Pots []struct {
			Eligible json.RawMessage `json:"eligible"`
			Winners  json.RawMessage `json:"winners"`
		} `json:"pots"`
	}
	if err := json.Unmarshal(data, &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Pots) != 1 {
		t.Fatalf("pot count got %d want 1", len(body.Pots))
	}
	if got := string(body.Pots[0].Eligible); got != eligible {
		t.Errorf("eligible got %s want %s", got, eligible)
	}
	if got := string(body.Pots[0].Winners); got != winners {
		t.Errorf("winners got %s want %s", got, winners)
	}
}

func TestActionDTOCarriesAmount(t *testing.T) {
	cases := []struct {
		name               string
		action             service.ActionView
		wantAmount, wantTo poker.Chips
	}{
		{"call", service.ActionView{Kind: "call", To: 0, Amount: 100}, 100, 0},
		{"raise_to", service.ActionView{Kind: "raise_to", To: 50, Amount: 40}, 40, 50},
		{"fold", service.ActionView{Kind: "fold", To: 0, Amount: 0}, 0, 0},
		{"check", service.ActionView{Kind: "check", To: 0, Amount: 0}, 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := tc.action
			b, err := MarshalGame(service.GameView{ID: "g", Status: "created", Mode: "mock", LastAction: &a})
			if err != nil {
				t.Fatal(err)
			}
			var raw map[string]any
			if err = json.Unmarshal(b, &raw); err != nil {
				t.Fatal(err)
			}
			la, ok := raw["last_action"].(map[string]any)
			if !ok {
				t.Fatalf("last_action missing: %s", b)
			}
			if _, ok := la["amount"]; !ok {
				t.Fatalf("amount key missing: %s", b)
			}
			var body struct {
				LastAction struct {
					Amount poker.Chips `json:"amount"`
					To     poker.Chips `json:"to"`
				} `json:"last_action"`
			}
			if err = json.Unmarshal(b, &body); err != nil {
				t.Fatal(err)
			}
			if body.LastAction.Amount != tc.wantAmount || body.LastAction.To != tc.wantTo {
				t.Fatalf("got amount=%d to=%d want amount=%d to=%d", body.LastAction.Amount, body.LastAction.To, tc.wantAmount, tc.wantTo)
			}
		})
	}
}

func TestCompletedPotSeatArrays(t *testing.T) {
	for _, tc := range potSeatJSONCases {
		t.Run(tc.name, func(t *testing.T) {
			view := service.CompletedHandView{Result: poker.HandResult{Pots: []poker.PotAward{{Eligible: tc.eligible, Winners: tc.winners}}}}
			data, err := json.Marshal(Completed(view))
			if err != nil {
				t.Fatal(err)
			}
			assertPotSeatJSON(t, data, tc.wantEligible, tc.wantWinners)
		})
	}
}
