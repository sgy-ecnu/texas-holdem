package agent

import (
	"context"

	"texas-holdem/internal/poker"
)

type mockDecider struct{}

func NewMockDecider() Decider { return &mockDecider{} }

func (*mockDecider) Decide(ctx context.Context, obs poker.Observation) (Decision, error) {
	if ctx.Err() != nil {
		return Decision{Action: fallbackAction(obs)}, contextError(ctx.Err())
	}
	if obs.Legal.CanCheck {
		return Decision{Action: poker.Action{Kind: "check"}}, nil
	}
	return Decision{Action: poker.Action{Kind: "call"}}, nil
}
