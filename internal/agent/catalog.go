package agent

import (
	"context"

	"texas-holdem/internal/poker"
)

type ProviderKind string
type Availability string

type ModelDescriptor struct {
	Ref          string
	Label        string
	Provider     ProviderKind
	Availability Availability
}

type Decision struct {
	Action       poker.Action
	InputTokens  *int64
	OutputTokens *int64
}

type Decider interface {
	Decide(context.Context, poker.Observation) (Decision, error)
}

type Factory interface {
	Models() []ModelDescriptor
	NewDecider(modelRef string) (Decider, error)
}
