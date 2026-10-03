package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"texas-holdem/internal/poker"
)

const (
	maxResponseBytes = 1 << 20
	defaultTimeout   = 15 * time.Second
	defaultMaxTokens = 256
	actionPrompt     = "Choose one legal poker action from the observation. Return only a JSON object with kind (fold, check, call, raise_to) and optional integer to. For raise_to, to is the total street bet target; otherwise omit to or set it to 0. No markdown, explanations, or additional fields. Observation:\n"
)

type liveDecider struct {
	config          modelConfig
	client          *http.Client
	timeout         time.Duration
	maxTokens       int
	setAvailability func(Availability)
}

func (*liveDecider) String() string               { return "agent.Decider(redacted)" }
func (*liveDecider) GoString() string             { return "agent.Decider(redacted)" }
func (*liveDecider) MarshalJSON() ([]byte, error) { return []byte(`{}`), nil }

func (d *liveDecider) Decide(parent context.Context, obs poker.Observation) (Decision, error) {
	fallback := Decision{Action: fallbackAction(obs)}
	if parent.Err() != nil {
		return fallback, contextError(parent.Err())
	}
	timeout := d.timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	body, err := d.requestBody(obs)
	if err != nil {
		return fallback, errors.New("MODEL_REQUEST_INVALID")
	}
	path := "/chat/completions"
	if d.config.descriptor.Provider == "anthropic" {
		path = "/messages"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(d.config.endpoint, "/")+path, bytes.NewReader(body))
	if err != nil {
		return fallback, errors.New("MODEL_REQUEST_INVALID")
	}
	for k, v := range d.config.headers {
		req.Header.Set(k, v)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if d.config.descriptor.Provider == "anthropic" {
		req.Header.Set("x-api-key", d.config.apiKey)
		req.Header.Set("anthropic-version", "2023-06-01")
	} else {
		req.Header.Set("Authorization", "Bearer "+d.config.apiKey)
	}
	resp, err := d.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			if !errors.Is(ctx.Err(), context.Canceled) {
				d.setAvailability("unavailable")
			}
			return fallback, contextError(ctx.Err())
		}
		d.setAvailability("unavailable")
		return fallback, errors.New("MODEL_REQUEST_FAILED")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		d.setAvailability("unavailable")
		if resp.StatusCode == 401 || resp.StatusCode == 403 {
			return fallback, errors.New("MODEL_AUTH_FAILED")
		}
		return fallback, errors.New("MODEL_HTTP_ERROR")
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		if ctx.Err() != nil {
			if !errors.Is(ctx.Err(), context.Canceled) {
				d.setAvailability("unavailable")
			}
			return fallback, contextError(ctx.Err())
		}
		d.setAvailability("unavailable")
		return fallback, errors.New("MODEL_BODY_READ_FAILED")
	}
	if len(data) > maxResponseBytes {
		d.setAvailability("unavailable")
		return fallback, errors.New("MODEL_BODY_TOO_LARGE")
	}
	text, input, output, err := d.response(data)
	if err != nil {
		d.setAvailability("unavailable")
		return fallback, errors.New("MODEL_PROTOCOL_ERROR")
	}
	// Reachability is independent of whether the returned poker action is valid.
	d.setAvailability("available")
	action, err := parseAction(text)
	if err != nil || !actionLegal(action, obs.Legal) {
		fallback.InputTokens, fallback.OutputTokens = input, output
		return fallback, errors.New("MODEL_INVALID_ACTION")
	}
	return Decision{Action: action, InputTokens: input, OutputTokens: output}, nil
}

func contextError(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return errors.New("MODEL_TIMEOUT")
	}
	return errors.New("MODEL_CANCELED")
}

func fallbackAction(obs poker.Observation) poker.Action {
	if obs.Legal.CanCheck {
		return poker.Action{Kind: "check"}
	}
	return poker.Action{Kind: "fold"}
}

func parseAction(text string) (poker.Action, error) {
	invalid := errors.New("MODEL_INVALID_ACTION")
	data := []byte(text)
	if len(data) > maxResponseBytes || rejectDuplicateKeys(data) != nil {
		return poker.Action{}, invalid
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(data, &fields) != nil || fields == nil {
		return poker.Action{}, invalid
	}
	for name := range fields {
		if name != "kind" && name != "to" {
			return poker.Action{}, invalid
		}
	}
	var kind string
	if raw, exists := fields["kind"]; !exists || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || json.Unmarshal(raw, &kind) != nil {
		return poker.Action{}, invalid
	}
	var to poker.Chips
	rawTo, exists := fields["to"]
	if exists && (bytes.Equal(bytes.TrimSpace(rawTo), []byte("null")) || json.Unmarshal(rawTo, &to) != nil) {
		return poker.Action{}, invalid
	}
	switch kind {
	case "fold", "check", "call":
		if to != 0 {
			return poker.Action{}, invalid
		}
	case "raise_to":
		if !exists || to <= 0 {
			return poker.Action{}, invalid
		}
	default:
		return poker.Action{}, invalid
	}
	return poker.Action{Kind: poker.ActionKind(kind), To: to}, nil
}

func actionLegal(action poker.Action, legal poker.LegalActions) bool {
	switch action.Kind {
	case "fold":
		return legal.CanFold
	case "check":
		return legal.CanCheck
	case "call":
		return legal.CanCall
	case "raise_to":
		if !legal.CanRaise || action.To > legal.MaxRaiseTo {
			return false
		}
		if action.To >= legal.MinRaiseTo {
			return true
		}
		return legal.ShortAllInTo != nil && action.To == *legal.ShortAllInTo && action.To == legal.MaxRaiseTo
	}
	return false
}

func (d *liveDecider) requestBody(obs poker.Observation) ([]byte, error) {
	b, err := json.Marshal(obs)
	if err != nil {
		return nil, errors.New("MODEL_REQUEST_INVALID")
	}
	maxTokens := d.maxTokens
	if maxTokens <= 0 {
		maxTokens = defaultMaxTokens
	}
	return json.Marshal(struct {
		Model     string           `json:"model"`
		Messages  []requestMessage `json:"messages"`
		MaxTokens int              `json:"max_tokens"`
		Stream    bool             `json:"stream"`
	}{Model: d.config.modelID, Messages: []requestMessage{{Role: "user", Content: actionPrompt + string(b)}}, MaxTokens: maxTokens, Stream: false})
}

type requestMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func (d *liveDecider) response(data []byte) (string, *int64, *int64, error) {
	if d.config.descriptor.Provider == "anthropic" {
		return anthropicResponse(data)
	}
	return openAIResponse(data)
}

func validUsage(input, output *int64) bool {
	return (input == nil || *input >= 0) && (output == nil || *output >= 0)
}
