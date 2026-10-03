package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const cliFixture = `server:
  http:
    host: 127.0.0.1
    port: 8181
    timeout: 5s
frontend:
  dev_port: 6001
`

var cliEnvKeys = []string{
	"POKER_CONFIG_FILE", "POKER_HTTP_HOST", "POKER_HTTP_PORT", "POKER_HTTP_TIMEOUT",
	"POKER_FRONTEND_PORT", "POKER_MYSQL_DSN", "POKER_REDIS_ADDR", "POKER_REDIS_PASSWORD",
	"POKER_REDIS_DB", "POKER_CATALOG_PATH", "POKER_ALLOW_PAID_MODELS", "POKER_MAX_LIVE_CALLS",
	"POKER_DECISION_TIMEOUT", "POKER_MAX_OUTPUT_TOKENS",
}

func isolatEnv(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	for _, k := range cliEnvKeys {
		t.Setenv(k, "")
	}
}

func writeCLIConfig(t *testing.T, text string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(p, []byte(text), 0600); err != nil {
		t.Fatal("fixture write failed")
	}
	return p
}

func TestRunPublicWhitelistJSON(t *testing.T) {
	isolatEnv(t)
	path := writeCLIConfig(t, cliFixture)
	var out bytes.Buffer
	if err := run([]string{"-conf", path}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(out.String(), "\n") {
		t.Fatal("output must end with newline")
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("output is not valid JSON: %s", out.String())
	}
	if len(got) != 5 {
		t.Fatalf("strict whitelist violated: %s", out.String())
	}
	for _, k := range []string{"http_base_url", "http_address", "http_host", "http_port", "frontend_port"} {
		if _, ok := got[k]; !ok {
			t.Fatalf("missing key %s in %s", k, out.String())
		}
	}
	if strings.Contains(out.String(), "redis") || strings.Contains(out.String(), "catalog") {
		t.Fatalf("private field leaked: %s", out.String())
	}
	if string(got["http_base_url"]) != `"http://127.0.0.1:8181"` || string(got["frontend_port"]) != "6001" {
		t.Fatalf("public values mismatch: %s", out.String())
	}
}

func TestRunEnvPriority(t *testing.T) {
	isolatEnv(t)
	path := writeCLIConfig(t, cliFixture)
	t.Setenv("POKER_HTTP_PORT", "8282")
	var out bytes.Buffer
	if err := run([]string{"-conf", path}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"http_address":"127.0.0.1:8282"`) {
		t.Fatalf("env override not applied: %s", out.String())
	}
}

func TestRunExplicitEmptyConfRejected(t *testing.T) {
	isolatEnv(t)
	// 若显式 -conf "" 被误当未指定，会回退到有效的 POKER_CONFIG_FILE 而成功。
	path := writeCLIConfig(t, cliFixture)
	t.Setenv("POKER_CONFIG_FILE", path)
	for _, args := range [][]string{{"-conf", ""}, {"-conf="}} {
		var out bytes.Buffer
		err := run(args, &out)
		if err == nil || err.Error() != "CONFIG_READ_FAILED" {
			t.Fatalf("args %v got %v want CONFIG_READ_FAILED (must not fall back)", args, err)
		}
		if out.Len() != 0 {
			t.Fatalf("error path wrote stdout: %s", out.String())
		}
	}
}

func TestRunSafeErrors(t *testing.T) {
	isolatEnv(t)
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"missing_file", []string{"-conf", filepath.Join(t.TempDir(), "absent.yaml")}, "CONFIG_READ_FAILED"},
		{"unknown_flag", []string{"-bogus"}, "INVALID_COMMAND"},
		{"extra_arg", []string{"extra"}, "INVALID_COMMAND"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			err := run(tc.args, &out)
			if err == nil || err.Error() != tc.want {
				t.Fatalf("got %v want %s", err, tc.want)
			}
			if out.Len() != 0 {
				t.Fatalf("error path must not write to stdout: %s", out.String())
			}
		})
	}
	var out bytes.Buffer
	if err := run([]string{"-conf", writeCLIConfig(t, "server: [broken\n")}, &out); err == nil || err.Error() != "CONFIG_INVALID" {
		t.Fatalf("bad yaml got %v want CONFIG_INVALID", err)
	}
}

// 直连 dsn / password 是私密字段：公共 CLI 必须仍严格 5 键且绝不回显。
func TestRunPublicWhitelistExcludesDirectSecrets(t *testing.T) {
	isolatEnv(t)
	const directDSN = "root:testpw@tcp(127.0.0.1:3306)/poker"
	path := writeCLIConfig(t, `server:
  http:
    host: 127.0.0.1
    port: 8181
    timeout: 5s
frontend:
  dev_port: 6001
data:
  mysql:
    dsn: "`+directDSN+`"
  redis:
    addr: 127.0.0.1:6379
    db: 0
    password: "testpw"
`)
	var out bytes.Buffer
	if err := run([]string{"-conf", path}, &out); err != nil {
		t.Fatal(err)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("output is not valid JSON: %s", out.String())
	}
	if len(got) != 5 {
		t.Fatalf("strict whitelist violated: %s", out.String())
	}
	for _, leak := range []string{"testpw", "dsn", "password", "redis", "mysql", "3306"} {
		if strings.Contains(out.String(), leak) {
			t.Fatalf("direct secret leaked: %s", out.String())
		}
	}
}
