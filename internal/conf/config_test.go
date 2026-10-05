package conf

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// base 使用临时端口 8083，证明监听地址与同源白名单确实由 YAML 驱动。
const validBase = `server:
  http:
    host: 127.0.0.1
    port: 8083
    timeout: 10s
frontend:
  dev_port: 5173
data:
  mysql:
    dsn_env: POKER_MYSQL_DSN
  redis:
    addr: 127.0.0.1:6380
    db: 15
    password_env: POKER_REDIS_PASSWORD
agents:
  catalog_path: auto
  request_timeout: 15s
  max_output_tokens: 256
  allow_paid_models: false
  max_live_calls: 1000
`

var envKeys = []string{
	"POKER_CONFIG_FILE", "POKER_HTTP_HOST", "POKER_HTTP_PORT", "POKER_HTTP_TIMEOUT",
	"POKER_FRONTEND_PORT", "POKER_MYSQL_DSN", "POKER_REDIS_ADDR", "POKER_REDIS_PASSWORD",
	"POKER_REDIS_DB", "POKER_CATALOG_PATH", "POKER_ALLOW_PAID_MODELS", "POKER_MAX_LIVE_CALLS",
	"POKER_DECISION_TIMEOUT", "POKER_MAX_OUTPUT_TOKENS", "POKER_HUMAN_ACTION_TIMEOUT",
	"POKER_AI_TURN_DELAY", "POKER_HAND_INTERVAL_SHOWDOWN_SEC", "POKER_HAND_INTERVAL_UNCONTESTED_SEC",
}

func isolateEnv(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	for _, k := range envKeys {
		t.Setenv(k, "")
	}
}

func writeFile(t *testing.T, dir, name, text string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(text), 0600); err != nil {
		t.Fatal("fixture write failed")
	}
	return p
}

func writeBase(t *testing.T, text string) string {
	t.Helper()
	p := writeFile(t, t.TempDir(), "config.yaml", text)
	t.Setenv("POKER_CONFIG_FILE", p)
	return p
}

func TestYAMLConfigWiringAndPublicWhitelist(t *testing.T) {
	isolateEnv(t)
	writeBase(t, validBase)
	const dsn = "fixture-dsn-must-not-leak"
	t.Setenv("POKER_MYSQL_DSN", dsn)
	t.Setenv("POKER_REDIS_PASSWORD", "fixture-password-must-not-leak")

	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.HTTPHost != "127.0.0.1" || c.HTTPPort != 8083 || c.HTTPTimeout != 10*time.Second {
		t.Fatalf("http wiring got %s %d %s", c.HTTPHost, c.HTTPPort, c.HTTPTimeout)
	}
	if c.Address != "127.0.0.1:8083" {
		t.Fatalf("address got %s", c.Address)
	}
	if c.FrontendPort != 5173 || c.RedisDB != 15 {
		t.Fatalf("frontend/redis got %d %d", c.FrontendPort, c.RedisDB)
	}
	if c.RedisAddress != "127.0.0.1:6380" || c.AllowPaidModels || c.MaxLiveCalls != 1000 {
		t.Fatalf("defaults got %s %v %d", c.RedisAddress, c.AllowPaidModels, c.MaxLiveCalls)
	}
	if c.DecisionTimeout != 15*time.Second || c.MaxOutputTokens != 256 {
		t.Fatalf("agent limits got %s %d", c.DecisionTimeout, c.MaxOutputTokens)
	}
	if c.MySQLDSN != dsn || c.RedisPassword != "fixture-password-must-not-leak" {
		t.Fatal("secrets not resolved in-process")
	}
	if c.CatalogPath != filepath.Join(os.Getenv("HOME"), ".config", "opencode", "opencode.jsonc") {
		t.Fatalf("auto catalog got %s", c.CatalogPath)
	}

	p, err := LoadPublic()
	if err != nil {
		t.Fatal(err)
	}
	if p.HTTPBaseURL != "http://127.0.0.1:8083" || p.HTTPAddress != "127.0.0.1:8083" || p.HTTPHost != "127.0.0.1" || p.HTTPPort != 8083 || p.FrontendPort != 5173 {
		t.Fatalf("public contract mismatch: %+v", p)
	}
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	out := string(b)
	if strings.Contains(out, dsn) || strings.Contains(out, "fixture-password") || strings.Contains(out, "redis") || strings.Contains(out, "catalog") {
		t.Fatalf("public output leaked private fields: %s", out)
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(b, &keys); err != nil || len(keys) != 5 {
		t.Fatalf("public output must have exactly 5 whitelisted keys: %s", out)
	}
	for _, k := range []string{"http_base_url", "http_address", "http_host", "http_port", "frontend_port"} {
		if _, ok := keys[k]; !ok {
			t.Fatalf("missing public key %s in %s", k, out)
		}
	}
}

func TestLocalOverlayPreservesExplicitFalseAndZero(t *testing.T) {
	isolateEnv(t)
	dir := t.TempDir()
	writeFile(t, dir, "config.yaml", `server:
  http:
    port: 8081
data:
  redis:
    db: 15
agents:
  allow_paid_models: true
  max_live_calls: 500
  max_output_tokens: 128
`)
	writeFile(t, dir, "config.local.yaml", `data:
  redis:
    db: 0
agents:
  allow_paid_models: false
`)
	t.Setenv("POKER_CONFIG_FILE", filepath.Join(dir, "config.yaml"))
	t.Setenv("POKER_MYSQL_DSN", "fixture-dsn")

	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.RedisDB != 0 {
		t.Fatalf("explicit local db:0 was dropped, got %d", c.RedisDB)
	}
	if c.AllowPaidModels {
		t.Fatal("explicit local allow_paid_models:false was dropped")
	}
	if c.MaxLiveCalls != 500 || c.MaxOutputTokens != 128 {
		t.Fatalf("base values not preserved: %d %d", c.MaxLiveCalls, c.MaxOutputTokens)
	}
	if c.HTTPPort != 8081 {
		t.Fatalf("base port not preserved: %d", c.HTTPPort)
	}
}

func TestEnvOverridePriority(t *testing.T) {
	isolateEnv(t)
	writeBase(t, validBase)
	t.Setenv("POKER_MYSQL_DSN", "fixture-dsn")
	t.Setenv("POKER_HTTP_HOST", "127.0.0.1")
	t.Setenv("POKER_HTTP_PORT", "9090")
	t.Setenv("POKER_HTTP_TIMEOUT", "3s")
	t.Setenv("POKER_FRONTEND_PORT", "6000")
	t.Setenv("POKER_REDIS_ADDR", "127.0.0.1:6399")
	t.Setenv("POKER_REDIS_DB", "2")
	t.Setenv("POKER_CATALOG_PATH", "/tmp/explicit-catalog.json")
	t.Setenv("POKER_ALLOW_PAID_MODELS", "true")
	t.Setenv("POKER_MAX_LIVE_CALLS", "42")
	t.Setenv("POKER_DECISION_TIMEOUT", "7s")
	t.Setenv("POKER_MAX_OUTPUT_TOKENS", "99")

	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.Address != "127.0.0.1:9090" || c.HTTPTimeout != 3*time.Second || c.FrontendPort != 6000 {
		t.Fatalf("http env override failed: %s %s %d", c.Address, c.HTTPTimeout, c.FrontendPort)
	}
	if c.RedisAddress != "127.0.0.1:6399" || c.RedisDB != 2 {
		t.Fatalf("redis env override failed: %s %d", c.RedisAddress, c.RedisDB)
	}
	if c.CatalogPath != "/tmp/explicit-catalog.json" || !c.AllowPaidModels || c.MaxLiveCalls != 42 {
		t.Fatalf("agent env override failed: %s %v %d", c.CatalogPath, c.AllowPaidModels, c.MaxLiveCalls)
	}
	if c.DecisionTimeout != 7*time.Second || c.MaxOutputTokens != 99 {
		t.Fatalf("agent limit env override failed: %s %d", c.DecisionTimeout, c.MaxOutputTokens)
	}
}

func TestExplicitFilePathAndSiblingLocal(t *testing.T) {
	isolateEnv(t)
	dir := t.TempDir()
	base := writeFile(t, dir, "custom.yaml", "server:\n  http:\n    port: 8083\ndata:\n  redis:\n    addr: 127.0.0.1:6380\n")
	writeFile(t, dir, "config.local.yaml", "server:\n  http:\n    port: 8123\n")
	t.Setenv("POKER_MYSQL_DSN", "fixture-dsn")

	c, err := LoadFile(base)
	if err != nil {
		t.Fatal(err)
	}
	if c.Address != "127.0.0.1:8123" {
		t.Fatalf("explicit file + local sibling failed: %s", c.Address)
	}
	p, err := LoadPublicFile(base)
	if err != nil {
		t.Fatal(err)
	}
	if p.HTTPPort != 8123 {
		t.Fatalf("public explicit file failed: %+v", p)
	}
}

func TestSafeConfigurationErrors(t *testing.T) {
	const secret = "fixture-secret-never-echo"
	cases := []struct {
		name string
		text string
		want string
	}{
		{"unknown_field", "server:\n  http:\n    bogus: 1\n", "CONFIG_INVALID"},
		{"unknown_section", "unknown_section:\n  a: 1\n", "CONFIG_INVALID"},
		{"duplicate_key", "server:\n  http:\n    port: 8081\n    port: 8082\n", "CONFIG_INVALID"},
		{"multi_document", "server:\n  http:\n    port: 8081\n---\nserver:\n  http:\n    port: 8082\n", "CONFIG_INVALID"},
		{"malformed", "server: [unclosed\n", "CONFIG_INVALID"},
		{"bad_type", "server:\n  http:\n    port: not-a-number\n", "CONFIG_INVALID"},
		{"secret_in_bad_yaml", "agents:\n  allow_paid_models: " + secret + "\n", "CONFIG_INVALID"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			isolateEnv(t)
			writeBase(t, tc.text)
			t.Setenv("POKER_MYSQL_DSN", "fixture-dsn")
			_, err := Load()
			if err == nil || err.Error() != tc.want {
				t.Fatalf("got %v want %s", err, tc.want)
			}
			if strings.Contains(err.Error(), secret) {
				t.Fatal("raw YAML content leaked through error")
			}
		})
	}
}

func TestMissingBaseAndInvalidEnv(t *testing.T) {
	isolateEnv(t)
	t.Setenv("POKER_CONFIG_FILE", filepath.Join(t.TempDir(), "absent.yaml"))
	if _, err := Load(); err == nil || err.Error() != "CONFIG_READ_FAILED" {
		t.Fatalf("missing base got %v", err)
	}

	for _, tc := range []struct{ key, value string }{
		{"POKER_HTTP_PORT", "abc"},
		{"POKER_HTTP_TIMEOUT", "soon"},
		{"POKER_FRONTEND_PORT", "0"},
		{"POKER_REDIS_DB", "16"},
		{"POKER_ALLOW_PAID_MODELS", "invalid"},
		{"POKER_MAX_LIVE_CALLS", "0"},
		{"POKER_DECISION_TIMEOUT", "nope"},
		{"POKER_MAX_OUTPUT_TOKENS", "0"},
	} {
		t.Run(tc.key, func(t *testing.T) {
			isolateEnv(t)
			writeBase(t, validBase)
			t.Setenv("POKER_MYSQL_DSN", "fixture-dsn")
			t.Setenv(tc.key, tc.value)
			if _, err := Load(); err == nil || err.Error() != "CONFIG_INVALID" {
				t.Fatalf("got %v want CONFIG_INVALID", err)
			}
		})
	}
}

func TestHumanActionTimeoutConfig(t *testing.T) {
	t.Run("default_60s", func(t *testing.T) {
		isolateEnv(t)
		writeBase(t, validBase)
		t.Setenv("POKER_MYSQL_DSN", "fixture-dsn")
		c, err := Load()
		if err != nil || c.HumanActionTimeout != 60*time.Second {
			t.Fatalf("default human timeout got %v %v", c.HumanActionTimeout, err)
		}
	})
	t.Run("yaml_and_env_override", func(t *testing.T) {
		isolateEnv(t)
		writeBase(t, validBase+"gameplay:\n  human_action_timeout: 90s\n")
		t.Setenv("POKER_MYSQL_DSN", "fixture-dsn")
		c, err := Load()
		if err != nil || c.HumanActionTimeout != 90*time.Second {
			t.Fatalf("yaml human timeout got %v %v", c.HumanActionTimeout, err)
		}
		t.Setenv("POKER_HUMAN_ACTION_TIMEOUT", "45s")
		c, err = Load()
		if err != nil || c.HumanActionTimeout != 45*time.Second {
			t.Fatalf("env human timeout got %v %v", c.HumanActionTimeout, err)
		}
	})
	t.Run("explicit_zero_and_bad_type_rejected", func(t *testing.T) {
		for _, body := range []string{
			"gameplay:\n  human_action_timeout: 0s\n",
			"gameplay:\n  human_action_timeout: -1s\n",
			"gameplay:\n  human_action_timeout:\n",
			"gameplay:\n  human_action_timeout: 60\n",
		} {
			isolateEnv(t)
			writeBase(t, validBase+body)
			t.Setenv("POKER_MYSQL_DSN", "fixture-dsn")
			if _, err := Load(); err == nil || err.Error() != "CONFIG_INVALID" {
				t.Fatalf("bad human timeout %q got %v want CONFIG_INVALID", body, err)
			}
		}
	})
}

func TestAITurnDelayConfig(t *testing.T) {
	t.Run("default_1500ms", func(t *testing.T) {
		isolateEnv(t)
		writeBase(t, validBase)
		t.Setenv("POKER_MYSQL_DSN", "fixture-dsn")
		c, err := Load()
		if err != nil || c.AITurnDelay != 1500*time.Millisecond {
			t.Fatalf("default ai_turn_delay got %v %v", c.AITurnDelay, err)
		}
	})
	t.Run("yaml_and_env_override", func(t *testing.T) {
		isolateEnv(t)
		writeBase(t, validBase+"gameplay:\n  ai_turn_delay: 2s\n")
		t.Setenv("POKER_MYSQL_DSN", "fixture-dsn")
		c, err := Load()
		if err != nil || c.AITurnDelay != 2*time.Second {
			t.Fatalf("yaml ai_turn_delay got %v %v", c.AITurnDelay, err)
		}
		// env 覆盖 YAML。
		t.Setenv("POKER_AI_TURN_DELAY", "300ms")
		c, err = Load()
		if err != nil || c.AITurnDelay != 300*time.Millisecond {
			t.Fatalf("env ai_turn_delay got %v %v", c.AITurnDelay, err)
		}
	})
	t.Run("explicit_zero_negative_and_bad_type_rejected", func(t *testing.T) {
		for _, body := range []string{
			"gameplay:\n  ai_turn_delay: 0s\n",
			"gameplay:\n  ai_turn_delay: -1s\n",
			"gameplay:\n  ai_turn_delay:\n",
			"gameplay:\n  ai_turn_delay: 1.5\n",
		} {
			isolateEnv(t)
			writeBase(t, validBase+body)
			t.Setenv("POKER_MYSQL_DSN", "fixture-dsn")
			if _, err := Load(); err == nil || err.Error() != "CONFIG_INVALID" {
				t.Fatalf("bad ai_turn_delay %q got %v want CONFIG_INVALID", body, err)
			}
		}
		// env 显式 0/负值同样拒绝。
		for _, v := range []string{"0s", "-250ms"} {
			isolateEnv(t)
			writeBase(t, validBase)
			t.Setenv("POKER_MYSQL_DSN", "fixture-dsn")
			t.Setenv("POKER_AI_TURN_DELAY", v)
			if _, err := Load(); err == nil || err.Error() != "CONFIG_INVALID" {
				t.Fatalf("env ai_turn_delay %q got %v want CONFIG_INVALID", v, err)
			}
		}
	})
}

func TestHandIntervalConfig(t *testing.T) {
	t.Run("default_12s_8s", func(t *testing.T) {
		isolateEnv(t)
		writeBase(t, validBase)
		t.Setenv("POKER_MYSQL_DSN", "fixture-dsn")
		c, err := Load()
		if err != nil || c.HandIntervalShowdown != 12*time.Second || c.HandIntervalUncontested != 8*time.Second {
			t.Fatalf("default hand interval got showdown=%v uncontested=%v err=%v", c.HandIntervalShowdown, c.HandIntervalUncontested, err)
		}
	})
	t.Run("yaml_and_env_override", func(t *testing.T) {
		isolateEnv(t)
		writeBase(t, validBase+"gameplay:\n  hand_interval_showdown_sec: 20\n  hand_interval_uncontested_sec: 5\n")
		t.Setenv("POKER_MYSQL_DSN", "fixture-dsn")
		c, err := Load()
		if err != nil || c.HandIntervalShowdown != 20*time.Second || c.HandIntervalUncontested != 5*time.Second {
			t.Fatalf("yaml hand interval got showdown=%v uncontested=%v err=%v", c.HandIntervalShowdown, c.HandIntervalUncontested, err)
		}
		t.Setenv("POKER_HAND_INTERVAL_SHOWDOWN_SEC", "7")
		c, err = Load()
		if err != nil || c.HandIntervalShowdown != 7*time.Second {
			t.Fatalf("env hand interval got showdown=%v err=%v", c.HandIntervalShowdown, err)
		}
	})
	t.Run("explicit_zero_negative_and_bad_type_rejected", func(t *testing.T) {
		for _, body := range []string{
			"gameplay:\n  hand_interval_showdown_sec: 0\n",
			"gameplay:\n  hand_interval_showdown_sec: -1\n",
			"gameplay:\n  hand_interval_showdown_sec:\n",
			"gameplay:\n  hand_interval_showdown_sec: 1.5\n",
			"gameplay:\n  hand_interval_uncontested_sec: 0\n",
		} {
			isolateEnv(t)
			writeBase(t, validBase+body)
			t.Setenv("POKER_MYSQL_DSN", "fixture-dsn")
			if _, err := Load(); err == nil || err.Error() != "CONFIG_INVALID" {
				t.Fatalf("bad hand interval %q got %v want CONFIG_INVALID", body, err)
			}
		}
		for _, v := range []string{"0", "-3", "abc"} {
			isolateEnv(t)
			writeBase(t, validBase)
			t.Setenv("POKER_MYSQL_DSN", "fixture-dsn")
			t.Setenv("POKER_HAND_INTERVAL_UNCONTESTED_SEC", v)
			if _, err := Load(); err == nil || err.Error() != "CONFIG_INVALID" {
				t.Fatalf("env hand interval %q got %v want CONFIG_INVALID", v, err)
			}
		}
	})
}

func TestPublicDoesNotRequireStorageSecret(t *testing.T) {
	isolateEnv(t)
	writeBase(t, validBase)
	// 无 POKER_MYSQL_DSN：服务路径要求存储，公共路径必须仍然成功。
	if _, err := Load(); err == nil || err.Error() != "CONFIG_STORAGE_REQUIRED" {
		t.Fatalf("service load got %v want CONFIG_STORAGE_REQUIRED", err)
	}
	p, err := LoadPublic()
	if err != nil {
		t.Fatalf("public load must not require secrets: %v", err)
	}
	if p.HTTPAddress != "127.0.0.1:8083" {
		t.Fatalf("public address got %s", p.HTTPAddress)
	}
}
