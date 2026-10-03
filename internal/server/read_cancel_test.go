package server

import (
	"context"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"texas-holdem/internal/service"
)

// 使用真实 Engine/Handler，不启动监听端口或真实数据库。
type pollingRepository struct{ service.Repository }

func (pollingRepository) Current(ctx context.Context) (service.Record, error) {
	if err := ctx.Err(); err != nil {
		return service.Record{}, fmt.Errorf("TEST_PRIVATE_CONTEXT_DETAIL: %w", err)
	}
	return service.Record{View: service.GameView{ID: gameID, Status: "paused", HandsCompleted: 1}}, nil
}
func (r pollingRepository) Get(ctx context.Context, _ string) (service.Record, error) {
	return r.Current(ctx)
}
func (pollingRepository) Hand(ctx context.Context, _ string, n uint32) (service.CompletedHandView, error) {
	if err := ctx.Err(); err != nil {
		return service.CompletedHandView{}, err
	}
	return service.CompletedHandView{GameID: gameID, HandNo: n}, nil
}
func (pollingRepository) Health(ctx context.Context) (service.HealthView, error) {
	h := service.HealthView{MySQL: "available", Redis: "available"}
	if err := ctx.Err(); err != nil {
		return h, fmt.Errorf("TEST_PRIVATE_CONTEXT_DETAIL: %w", err)
	}
	return h, nil
}

func TestAbortedPollingDoesNotFaultManager(t *testing.T) {
	for _, deadline := range []bool{false, true} {
		kind := "cancel"
		if deadline {
			kind = "deadline"
		}
		for _, endpoint := range []string{"get", "current", "hand", "health"} {
			t.Run(kind+"/"+endpoint, func(t *testing.T) {
				m, err := service.New(context.Background(), pollingRepository{}, nil, service.Options{})
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(m.Close)
				path := "/api/v1/games/" + gameID
				switch endpoint {
				case "current":
					path = "/api/v1/games/current"
				case "hand":
					path += "/hands/1"
				case "health":
					path = "/api/v1/health"
				}
				ctx, cancel := context.WithCancel(context.Background())
				if deadline {
					cancel()
					ctx, cancel = context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
				}
				cancel()
				h := Handler(m, nil)
				req := httptest.NewRequest("GET", "http://127.0.0.1:8081"+path, nil).WithContext(ctx)
				w := httptest.NewRecorder()
				h.ServeHTTP(w, req)
				if w.Code != 503 {
					t.Errorf("aborted read status got %d want 503", w.Code)
				}
				if strings.Contains(w.Body.String(), "TEST_PRIVATE") || strings.Contains(w.Body.String(), ctx.Err().Error()) {
					t.Error("raw cancellation detail leaked")
				}
				if endpoint != "health" && !strings.Contains(w.Body.String(), `"code":"STORAGE_UNAVAILABLE"`) {
					t.Error("fixed safe error code missing")
				}
				if endpoint == "health" && strings.Contains(w.Body.String(), `"mysql":"unavailable"`) {
					t.Error("caller cancellation falsely reported MySQL outage")
				}
				for _, next := range []string{"/api/v1/games/" + gameID, "/api/v1/health"} {
					if w := request(h, "GET", next, "", nil); w.Code != 200 {
						t.Errorf("subsequent %s status got %d want 200", next, w.Code)
					}
				}
			})
		}
	}
}
