package server

import (
	"encoding/json"
	"strings"
	"testing"
	"texas-holdem/internal/poker"
	"texas-holdem/internal/service"
)

// TestCompletedHandShowdownDTO 验证摊牌结算 DTO 暴露 hand_rank/best_five，
// 且弃牌结束（无牌型）时该数组为空。
func TestCompletedHandShowdownDTO(t *testing.T) {
	view := service.CompletedHandView{GameID: "g", HandNo: 1, Result: poker.HandResult{
		Reason:    "showdown",
		HandRanks: map[poker.Seat]string{1: "一对", 0: "高牌"},
		BestFive: map[poker.Seat][]poker.Card{
			0: {"Qh", "Jd", "9c", "7s", "4h"},
			1: {"2c", "2s", "Qh", "Jd", "9c"},
		},
	}}
	b, err := json.Marshal(Completed(view))
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Reason   string `json:"reason"`
		Showdown []struct {
			Seat     poker.Seat   `json:"seat"`
			HandRank string       `json:"hand_rank"`
			BestFive []poker.Card `json:"best_five"`
		} `json:"showdown"`
	}
	if err = json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got.Reason != "showdown" || len(got.Showdown) != 2 {
		t.Fatalf("showdown payload mismatch: %s", b)
	}
	// 入口按座位升序稳定输出。
	if got.Showdown[0].Seat != 0 || got.Showdown[0].HandRank != "高牌" || len(got.Showdown[0].BestFive) != 5 {
		t.Fatalf("seat 0 showdown mismatch: %+v", got.Showdown[0])
	}
	if got.Showdown[1].Seat != 1 || got.Showdown[1].HandRank != "一对" || len(got.Showdown[1].BestFive) != 5 {
		t.Fatalf("seat 1 showdown mismatch: %+v", got.Showdown[1])
	}

	// 弃牌结束没有任何牌型，必须输出空数组而非 null。
	b2, err := json.Marshal(Completed(service.CompletedHandView{Result: poker.HandResult{Reason: "uncontested"}}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b2), `"showdown":[]`) {
		t.Fatalf("uncontested showdown must be empty array: %s", b2)
	}
}
