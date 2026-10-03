package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"texas-holdem/internal/poker"
)

func writeConfig(t *testing.T, dir, name, text string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(text), 0600); err != nil {
		t.Fatal("fixture write failed")
	}
	return p
}

func fixtureFactory(t *testing.T, npm, endpoint string) Factory {
	t.Helper()
	config := map[string]any{"provider": map[string]any{"test": map[string]any{
		"npm": npm, "options": map[string]any{"baseURL": endpoint, "apiKey": "fixture-only-token"},
		"models": map[string]any{"model": map[string]any{"name": "safe model"}},
	}}}
	b, _ := json.Marshal(config)
	c, err := LoadCatalog(writeConfig(t, t.TempDir(), "opencode.json", string(b)))
	if err != nil {
		t.Fatal(err)
	}
	f, err := NewFactory(c)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func observation(check bool) poker.Observation {
	return poker.Observation{Self: 0, Hole: [2]poker.Card{"Ac", "Ks"},
		State: poker.PublicState{Street: "flop", Board: []poker.Card{"2c", "3d", "4h"},
			Players: []poker.Player{{Seat: 0, Stack: 1000}, {Seat: 1, Stack: 900}}},
		Legal: poker.LegalActions{CanCheck: check, CanCall: !check, CanFold: true, CanRaise: true, MinRaiseTo: 100, MaxRaiseTo: 1000}}
}

func TestConfigLoaderRedaction(t *testing.T) {
	for _, source := range []string{"literal", "env", "file"} {
		t.Run(source, func(t *testing.T) {
			dir := t.TempDir()
			key := "fixture-secret-never-export"
			ref := key
			switch source {
			case "env":
				t.Setenv("AGENT_FIXTURE_KEY", key)
				ref = "{env:AGENT_FIXTURE_KEY}"
			case "file":
				writeConfig(t, dir, "key.txt", key+"\n")
				ref = "{file:key.txt}"
			}
			text := `{"provider":{"safe":{"npm":"@ai-sdk/openai-compatible","options":{"apiKey":` + fmt.Sprintf("%q", ref) + `,"baseURL":"https://example.invalid/v1"},"models":{"m":{"name":"` + key + `"}}}}}`
			p := writeConfig(t, dir, "opencode.json", text)
			c, err := LoadCatalog(p)
			if err != nil {
				t.Fatal(err)
			}
			f, err := NewFactory(c)
			if err != nil {
				t.Fatal(err)
			}
			d, err := f.NewDecider("safe/m")
			if err != nil {
				t.Fatal(err)
			}
			for _, v := range []any{c, f, d, f.Models()} {
				b, err := json.Marshal(v)
				if err != nil {
					t.Fatal("safe marshal failed")
				}
				for _, out := range []string{string(b), fmt.Sprintf("%v %+v %#v", v, v, v)} {
					if strings.Contains(out, key) || strings.Contains(out, "example.invalid") || strings.Contains(out, ref) {
						t.Fatal("private config escaped safe output")
					}
				}
			}
			writeConfig(t, dir, "opencode.json", `{"provider":{"apiKey":"`+key+`",BROKEN`)
			_, err = LoadCatalog(p)
			if err == nil || err.Error() != "CONFIG_INVALID" {
				t.Fatal("unsafe or absent parse error")
			}
		})
	}
}

func TestJSONCLexerAndMerge(t *testing.T) {
	dir := t.TempDir()
	p := writeConfig(t, dir, "opencode.json", `{"provider":{"p":{"npm":"@ai-sdk/openai-compatible","options":{"apiKey":"fixture","baseURL":"https://example.invalid/v1"},"models":{"old":{},"same":{"id":"old-id"}}},"other":{"npm":"custom-package","models":{"x":{}}}}}`)
	writeConfig(t, dir, "opencode.jsonc", `{
	 // strings must not be mistaken for comments
	 "provider": {"p": {"options":{"apiKey":"quoted\"///*literal*/"},
	 "models":{"new":{},"same":{"id":"new-id"},},},}, /* trailing comma */
	}`)
	c, err := LoadCatalog(p)
	if err != nil {
		t.Fatal(err)
	}
	f, _ := NewFactory(c)
	var refs []string
	for _, m := range f.Models() {
		refs = append(refs, m.Ref)
	}
	if !reflect.DeepEqual(refs, []string{"mock", "other/x", "p/new", "p/old", "p/same"}) {
		t.Fatalf("unexpected safe model refs: %v", refs)
	}
	if c.models["p/same"].modelID != "new-id" {
		t.Fatal("jsonc model override missing")
	}
	if f.Models()[1].Availability != "unsupported" {
		t.Fatal("unknown package inferred as compatible")
	}
	for _, bad := range []string{`{"provider":{/* unterminated`, `{"provider":{"p":{},,}}`, `{"provider":{,}}`, `{"provider":{}} {}`, `{"provider":{"p":{"options":{"apiKey":"{env:MISSING_AGENT_FIXTURE}"},"npm":"@ai-sdk/openai","models":{"m":{}}}}}`} {
		writeConfig(t, dir, "opencode.jsonc", bad)
		_, err := LoadCatalog(p)
		if strings.Contains(bad, "MISSING_AGENT_FIXTURE") {
			if err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err == nil || err.Error() != "CONFIG_INVALID" {
			t.Fatal("malformed JSONC accepted or unsafe error")
		}
	}
}

func TestModelListAndMock(t *testing.T) {
	f, err := NewFactory(&Catalog{})
	if err != nil {
		t.Fatal(err)
	}
	models := f.Models()
	if len(models) != 1 || models[0].Ref != "mock" || models[0].Availability != "mock" {
		t.Fatal("mock missing")
	}
	models[0].Ref = "mutated"
	if f.Models()[0].Ref != "mock" {
		t.Fatal("catalog alias")
	}
	if _, err := f.NewDecider("unknown"); err == nil {
		t.Fatal("unknown model accepted")
	}
	for _, check := range []bool{true, false} {
		d, err := NewMockDecider().Decide(context.Background(), observation(check))
		want := poker.ActionKind("call")
		if check {
			want = "check"
		}
		if err != nil || d.Action.Kind != want || d.InputTokens != nil || d.OutputTokens != nil {
			t.Fatal("mock contract")
		}
	}
}

func TestProviderAdapters(t *testing.T) {
	for _, protocol := range []string{"openai", "anthropic"} {
		t.Run(protocol, func(t *testing.T) {
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Method != http.MethodPost {
					t.Error("method")
				}
				var req map[string]json.RawMessage
				if json.NewDecoder(r.Body).Decode(&req) != nil {
					t.Error("invalid request")
				}
				if string(req["model"]) != `"model"` || string(req["max_tokens"]) != "256" || string(req["stream"]) != "false" {
					t.Error("model/token/stream contract")
				}
				var messages []struct {
					Role    string
					Content string
				}
				if json.Unmarshal(req["messages"], &messages) != nil || len(messages) != 1 || messages[0].Role != "user" {
					t.Error("history present")
				}
				if len(messages) == 1 {
					var got poker.Observation
					expected, _ := json.Marshal(observation(true))
					if messages[0].Content != actionPrompt+string(expected) || json.Unmarshal([]byte(strings.TrimPrefix(messages[0].Content, actionPrompt)), &got) != nil || !reflect.DeepEqual(got, observation(true)) {
						t.Error("observation altered or unexpected input")
					}
				}
				w.Header().Set("Content-Type", "application/json")
				if protocol == "openai" {
					if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer fixture-only-token" || r.Header.Get("x-api-key") != "" {
						t.Error("openai auth/path contract")
					}
					io.WriteString(w, `{"choices":[{"message":{"content":"{\"kind\":\"check\"}"}}],"usage":{"prompt_tokens":7,"completion_tokens":3}}`)
				} else {
					if r.URL.Path != "/v1/messages" || r.Header.Get("x-api-key") != "fixture-only-token" || r.Header.Get("anthropic-version") != "2023-06-01" || r.Header.Get("Authorization") != "" {
						t.Error("anthropic auth/path contract")
					}
					io.WriteString(w, `{"content":[{"type":"text","text":"{\"kind\":\"check\"}"}],"usage":{"input_tokens":7,"output_tokens":3}}`)
				}
			}))
			defer srv.Close()
			npm := "@ai-sdk/openai-compatible"
			if protocol == "anthropic" {
				npm = "@ai-sdk/anthropic"
			}
			f := fixtureFactory(t, npm, srv.URL+"/v1")
			if calls.Load() != 0 || f.Models()[1].Availability != "configured" {
				t.Fatal("startup probe or wrong initial availability")
			}
			d, _ := f.NewDecider("test/model")
			for i := 0; i < 2; i++ {
				decision, err := d.Decide(context.Background(), observation(true))
				if err != nil || decision.Action.Kind != "check" || decision.InputTokens == nil || *decision.InputTokens != 7 || decision.OutputTokens == nil || *decision.OutputTokens != 3 {
					t.Fatal("response/usage contract")
				}
			}
			if calls.Load() != 2 || f.Models()[1].Availability != "available" {
				t.Fatal("retry or availability contract")
			}
		})
	}
}

func TestStrictActionJSON(t *testing.T) {
	for _, text := range []string{`{"kind":"check"}`, `{"kind":"call","to":0}`, `{"kind":"raise_to","to":100}`} {
		if _, err := parseAction(text); err != nil {
			t.Fatal("valid action rejected")
		}
	}
	for _, text := range []string{`null`, `{}`, `[]`, `{"kind":"check","extra":1}`, `{"kind":"check"} {}`, `{"kind":"check","to":1}`, `{"kind":"raise_to"}`, `{"kind":"raise_to","to":-1}`, `{"kind":"raise_to","to":1.5}`, `{"kind":"all_in"}`, "```json\n{}\n```", `{"kind":"check","kind":"fold"}`, `{"kind":null}`, `{"Kind":"check"}`, `{"kind":"check","to":null}`} {
		if _, err := parseAction(text); err == nil || err.Error() != "MODEL_INVALID_ACTION" {
			t.Fatal("invalid action accepted or unsafe error")
		}
	}
}

func TestDecisionFallback(t *testing.T) {
	for _, scenario := range []string{"timeout", "malformed", "illegal", "auth", "oversize", "protocol"} {
		for _, check := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/check=%v", scenario, check), func(t *testing.T) {
				var calls atomic.Int32
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls.Add(1)
					switch scenario {
					case "timeout":
						select {
						case <-r.Context().Done():
						case <-time.After(100 * time.Millisecond):
						}
						return
					case "auth":
						w.WriteHeader(401)
						io.WriteString(w, "response-private-content")
						return
					case "oversize":
						io.WriteString(w, strings.Repeat("x", maxResponseBytes+1))
						return
					case "protocol":
						io.WriteString(w, "response-private-content")
						return
					}
					text := `{"kind":"check","extra":"response-private-content"}`
					if scenario == "illegal" {
						text = `{"kind":"raise_to","to":99999}`
					}
					b, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": text}}}})
					w.Write(b)
				}))
				defer srv.Close()
				f := fixtureFactory(t, "@ai-sdk/openai", srv.URL+"/v1")
				d, _ := f.NewDecider("test/model")
				ctx := context.Background()
				if scenario == "timeout" {
					var cancel context.CancelFunc
					ctx, cancel = context.WithTimeout(ctx, 30*time.Millisecond)
					defer cancel()
				}
				decision, err := d.Decide(ctx, observation(check))
				want := poker.ActionKind("fold")
				if check {
					want = "check"
				}
				if err == nil || decision.Action.Kind != want || strings.Contains(err.Error(), "response-private") {
					t.Fatal("fallback or safe error contract")
				}
				if calls.Load() != 1 {
					t.Fatal("unexpected retry")
				}
				availability := Availability("unavailable")
				if scenario == "illegal" || scenario == "malformed" {
					availability = "available"
				}
				if f.Models()[1].Availability != availability {
					t.Fatal("action failure confused with transport availability")
				}
			})
		}
	}
}

func TestObservationPrivacy(t *testing.T) {
	typ := reflect.TypeOf(poker.Observation{})
	if typ.NumField() != 4 {
		t.Fatal("observation contract changed")
	}
	for _, name := range []string{"State", "Self", "Hole", "Legal"} {
		if _, ok := typ.FieldByName(name); !ok {
			t.Fatal("observation contract changed")
		}
	}
	// Request serialization is asserted in TestProviderAdapters: one message, exactly this observation.
	b, _ := json.Marshal(observation(true))
	for _, forbidden := range []string{"HoleCards", "Deck", "History", "HandResult", "AcKsOpponent"} {
		if strings.Contains(string(b), forbidden) {
			t.Fatal("private/history field in observation")
		}
	}
}

func TestAvailabilityConcurrent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"choices":[{"message":{"content":"{\"kind\":\"check\"}"}}]}`)
	}))
	defer srv.Close()
	f := fixtureFactory(t, "@ai-sdk/openai", srv.URL+"/v1")
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d, err := f.NewDecider("test/model")
			if err != nil {
				t.Error(err)
				return
			}
			if _, err = d.Decide(context.Background(), observation(true)); err != nil {
				t.Error(err)
			}
			_ = f.Models()
		}()
	}
	wg.Wait()
}

func TestLocalCatalogModels(t *testing.T) {
	path := os.Getenv("AGENT_CATALOG_PATH")
	if path == "" {
		t.Skip("未提供显式只读配置路径；不自动读取用户配置")
	}
	c, err := LoadCatalog(path)
	if err != nil {
		t.Fatal(err)
	}
	f, err := NewFactory(c)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range f.Models() {
		t.Logf("ref=%s provider=%s availability=%s", m.Ref, m.Provider, m.Availability)
	}
}

func TestConfigLoaderFailures(t *testing.T) {
	for _, tc := range []struct{ name, text, code string }{
		{"duplicate-key", `{"provider":{},"provider":{}}`, "CONFIG_INVALID"},
		{"bad-model-id", `{"provider":{"p":{"models":{"bad model":{}}}}}`, "CONFIG_INVALID"},
		{"unsafe-endpoint", `{"provider":{"p":{"options":{"baseURL":"https://user:fixture@example.invalid/v1"}}}}`, "CONFIG_ENDPOINT_INVALID"},
		{"credential-newline", `{"provider":{"p":{"options":{"apiKey":"fixture\nprivate"}}}}`, "CONFIG_CREDENTIAL_INVALID"},
		{"bad-env-reference", `{"provider":{"p":{"options":{"apiKey":"{env:bad-name}"}}}}`, "CONFIG_REFERENCE_INVALID"},
		{"missing-file", `{"provider":{"p":{"options":{"apiKey":"{file:not-present}"}}}}`, "CONFIG_CREDENTIAL_READ_FAILED"},
		{"reserved-header", `{"provider":{"p":{"options":{"headers":{"Authorization":"fixture-private"}}}}}`, "CONFIG_HEADER_INVALID"},
		{"header-collision", `{"provider":{"p":{"options":{"headers":{"x-custom":"fixture-a","X-Custom":"fixture-b"}}}}}`, "CONFIG_HEADER_INVALID"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := LoadCatalog(writeConfig(t, t.TempDir(), "opencode.json", tc.text))
			if err == nil || err.Error() != tc.code {
				t.Fatalf("expected safe code %s; raw errors are not printed", tc.code)
			}
		})
	}
	if _, err := NewFactory(nil); err == nil || err.Error() != "CONFIG_REQUIRED" {
		t.Fatal("nil catalog accepted")
	}
	dir := t.TempDir()
	p := writeConfig(t, dir, "opencode.json", `{"provider":{"p":{"npm":"@ai-sdk/openai-compatible","options":{"apiKey":"fixture"},"models":{"m":{}}},"u":{"npm":"custom","options":{"baseURL":"https://example.invalid/v1","apiKey":"fixture"},"models":{"m":{}}}}}`)
	c, err := LoadCatalog(p)
	if err != nil {
		t.Fatal(err)
	}
	f, _ := NewFactory(c)
	if _, err := f.NewDecider("p/m"); err == nil || err.Error() != "MODEL_NOT_CONFIGURED" {
		t.Fatal("compatible package without endpoint enabled")
	}
	if _, err := f.NewDecider("u/m"); err == nil || err.Error() != "MODEL_UNSUPPORTED" {
		t.Fatal("unknown package inferred from URL")
	}
}

func TestLegalActionBoundaries(t *testing.T) {
	short := poker.Chips(75)
	legal := poker.LegalActions{CanFold: true, CanCheck: true, CanCall: true, CanRaise: true, MinRaiseTo: 100, MaxRaiseTo: 75, ShortAllInTo: &short}
	if !actionLegal(poker.Action{Kind: "raise_to", To: 75}, legal) || actionLegal(poker.Action{Kind: "raise_to", To: 74}, legal) {
		t.Fatal("short all-in boundary")
	}
	legal.MaxRaiseTo = 1000
	if actionLegal(poker.Action{Kind: "raise_to", To: 75}, legal) {
		t.Fatal("non-all-in short raise accepted")
	}
	for _, to := range []poker.Chips{100, 1000} {
		if !actionLegal(poker.Action{Kind: "raise_to", To: to}, legal) {
			t.Fatal("full raise boundary")
		}
	}
	for _, to := range []poker.Chips{99, 1001} {
		if actionLegal(poker.Action{Kind: "raise_to", To: to}, legal) {
			t.Fatal("illegal raise boundary")
		}
	}
	for _, kind := range []poker.ActionKind{"fold", "check", "call", "raise_to"} {
		if actionLegal(poker.Action{Kind: kind, To: 100}, poker.LegalActions{}) {
			t.Fatal("missing action right ignored")
		}
	}
}

func TestHTTPPrivacyRedirects(t *testing.T) {
	var forwarded atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { forwarded.Add(1) }))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer source.Close()
	f := fixtureFactory(t, "@ai-sdk/openai", source.URL+"/v1")
	d, _ := f.NewDecider("test/model")
	decision, err := d.Decide(context.Background(), observation(true))
	if err == nil || err.Error() != "MODEL_HTTP_ERROR" || decision.Action.Kind != "check" || forwarded.Load() != 0 {
		t.Fatal("redirect forwarded credentials or unsafe result")
	}
}

func TestCancellationAndLimits(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	defer srv.Close()
	f := fixtureFactory(t, "@ai-sdk/anthropic", srv.URL+"/v1")
	if f.(*modelFactory).client.Timeout != 15*time.Second {
		t.Fatal("default timeout changed")
	}
	d, _ := f.NewDecider("test/model")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	decision, err := d.Decide(ctx, observation(false))
	if err == nil || err.Error() != "MODEL_CANCELED" || decision.Action.Kind != "fold" || calls.Load() != 0 || f.Models()[1].Availability != "configured" {
		t.Fatal("cancellation triggered call or availability update")
	}
}

func TestFactoryOptionsWireTimeoutAndTokens(t *testing.T) {
	config := map[string]any{"provider": map[string]any{"test": map[string]any{
		"npm": "@ai-sdk/openai-compatible", "options": map[string]any{"baseURL": "https://example.invalid/v1", "apiKey": "fixture-only-token"},
		"models": map[string]any{"model": map[string]any{}},
	}}}
	b, _ := json.Marshal(config)
	c, err := LoadCatalog(writeConfig(t, t.TempDir(), "opencode.json", string(b)))
	if err != nil {
		t.Fatal(err)
	}
	f, err := NewFactoryWithOptions(c, Options{RequestTimeout: 3 * time.Second, MaxOutputTokens: 77})
	if err != nil {
		t.Fatal(err)
	}
	if f.(*modelFactory).client.Timeout != 3*time.Second {
		t.Fatal("request timeout not wired to client")
	}
	d, err := f.NewDecider("test/model")
	if err != nil {
		t.Fatal(err)
	}
	ld, ok := d.(*liveDecider)
	if !ok || ld.timeout != 3*time.Second || ld.maxTokens != 77 {
		t.Fatal("decider timeout/token limits not wired")
	}
	body, err := ld.requestBody(observation(true))
	if err != nil {
		t.Fatal(err)
	}
	var req struct {
		MaxTokens int `json:"max_tokens"`
	}
	if json.Unmarshal(body, &req) != nil || req.MaxTokens != 77 {
		t.Fatalf("max_tokens not wired: %s", body)
	}
	def, err := NewFactoryWithOptions(&Catalog{}, Options{})
	if err != nil || def.(*modelFactory).client.Timeout != 15*time.Second {
		t.Fatal("zero-value options must fall back to safe defaults")
	}
}

func TestProviderResponseErrors(t *testing.T) {
	for _, protocol := range []string{"openai", "anthropic"} {
		t.Run(protocol, func(t *testing.T) {
			decode := openAIResponse
			valid := `{"choices":[{"message":{"content":"{\"kind\":\"check\"}"}}]}`
			negative := `{"choices":[{"message":{"content":"{}"}}],"usage":{"prompt_tokens":-1}}`
			if protocol == "anthropic" {
				decode = anthropicResponse
				valid = `{"content":[{"type":"text","text":"{\"kind\":\"check\"}"}]}`
				negative = `{"content":[{"type":"text","text":"{}"}],"usage":{"output_tokens":-1}}`
			}
			_, in, out, err := decode([]byte(valid))
			if err != nil || in != nil || out != nil {
				t.Fatal("missing usage must remain null")
			}
			for _, body := range []string{negative, valid + ` {}`, `{"error":{"message":"fixture-response-private"}}`, `null`, `{}`, `{"choices":[],"content":[]}`, `{"content":[{"type":"tool_use","text":"{}"}]}`} {
				if _, _, _, err := decode([]byte(body)); err == nil || err.Error() != "MODEL_PROTOCOL_ERROR" {
					t.Fatal("unsafe or missing protocol error")
				}
			}
		})
	}
}
