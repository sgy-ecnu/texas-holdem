package conf

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 所有坏输入必须收敛为固定 CONFIG_INVALID，且错误文本不得回显输入。
func TestStrictSchemaRejection(t *testing.T) {
	cases := []struct {
		name string
		text string
	}{
		{"float_port", "server:\n  http:\n    port: 8083.9\n"},
		{"quoted_port", "server:\n  http:\n    port: \"8083\"\n"},
		{"null_port", "server:\n  http:\n    port:\n"},
		{"null_host", "server:\n  http:\n    host: null\n"},
		{"null_timeout", "server:\n  http:\n    timeout:\n"},
		{"bool_host", "server:\n  http:\n    host: true\n"},
		{"int_host", "server:\n  http:\n    host: 123\n"},
		{"int_timeout", "server:\n  http:\n    timeout: 10\n"},
		{"non_mapping_section", "server: 1\n"},
		{"non_mapping_nested", "server:\n  http: 1\n"},
		{"non_mapping_data", "data:\n  redis: []\n"},
		{"string_bool", "agents:\n  allow_paid_models: \"false\"\n"},
		{"string_int", "agents:\n  max_output_tokens: \"256\"\n"},
		{"float_int", "agents:\n  max_live_calls: 10.5\n"},
		{"null_section", "server:\n"},
		{"null_root", "null\n"},
		{"scalar_root", "42\n"},
		{"sequence_root", "- a\n"},
		{"empty_mapping", "{}\n"},
		{"empty_file", ""},
		{"comments_only", "# only a comment\n# another\n"},
		{"duplicate_key", "server:\n  http:\n    port: 8081\n    port: 8082\n"},
		{"unknown_field", "server:\n  http:\n    bogus: 1\n"},
		{"unknown_section", "nope:\n  a: 1\n"},
		{"merge_key", "server:\n  http:\n    <<: {port: 8081}\n"},
		{"alias_value", "agents: &a\n  allow_paid_models: false\nserver: *a\n"},
		{"multi_document", "server:\n  http:\n    port: 8081\n---\nserver:\n  http:\n    port: 8082\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			isolateEnv(t)
			writeBase(t, tc.text)
			t.Setenv("POKER_MYSQL_DSN", "fixture-dsn")
			_, err := Load()
			if err == nil || err.Error() != "CONFIG_INVALID" {
				t.Fatalf("got %v want CONFIG_INVALID", err)
			}
			if strings.Contains(err.Error(), "8083") || strings.Contains(err.Error(), "bogus") {
				t.Fatal("error leaked input content")
			}
		})
	}
}

func TestValidScalarTypesAccepted(t *testing.T) {
	isolateEnv(t)
	writeBase(t, `server:
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
    db: 0
    password_env: POKER_REDIS_PASSWORD
agents:
  catalog_path: auto
  request_timeout: 15s
  max_output_tokens: 256
  allow_paid_models: false
  max_live_calls: 1000
`)
	t.Setenv("POKER_MYSQL_DSN", "fixture-dsn")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.RedisDB != 0 || c.AllowPaidModels || c.Address != "127.0.0.1:8083" {
		t.Fatalf("valid scalars not applied: %+v", c)
	}
}

func TestHostValidation(t *testing.T) {
	cases := []struct {
		host string
		ok   bool
	}{
		{"127.0.0.1", true},
		{"192.168.1.10", true},
		{"localhost", true},
		{"example.com", true},
		{"sub.example.com", true},
		{"localhost/path", false},
		{"http://127.0.0.1", false},
		{"127.0.0.1:8080", false},
		{"user@host", false},
		{" 127.0.0.1", false},
		{"127.0.0.1 ", false},
		{"host name", false},
		{"/absolute", false},
		{"bad_host", false},
		{"", false},
	}
	for _, tc := range cases {
		t.Run(tc.host, func(t *testing.T) {
			if got := validHost(tc.host); got != tc.ok {
				t.Fatalf("validHost(%q) got %v want %v", tc.host, got, tc.ok)
			}
		})
	}

	isolateEnv(t)
	writeBase(t, "server:\n  http:\n    host: localhost/path\n")
	t.Setenv("POKER_MYSQL_DSN", "fixture-dsn")
	if _, err := Load(); err == nil || err.Error() != "CONFIG_INVALID" {
		t.Fatalf("bad configured host got %v want CONFIG_INVALID", err)
	}
}

func TestSecretEnvNameValidation(t *testing.T) {
	bad := []string{"1BAD", "BAD-NAME", "has space", "", "A.B"}
	for _, name := range bad {
		t.Run("dsn_env_"+name, func(t *testing.T) {
			isolateEnv(t)
			writeBase(t, "data:\n  mysql:\n    dsn_env: "+quoteYAML(name)+"\n")
			t.Setenv("POKER_MYSQL_DSN", "fixture-dsn")
			if _, err := Load(); err == nil || err.Error() != "CONFIG_INVALID" {
				t.Fatalf("bad dsn_env %q got %v want CONFIG_INVALID", name, err)
			}
		})
	}
	// 合法自定义名可用，且值只从对应 env 读取。
	isolateEnv(t)
	writeBase(t, "data:\n  mysql:\n    dsn_env: MY_CUSTOM_DSN\n")
	t.Setenv("MY_CUSTOM_DSN", "fixture-custom-dsn")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.MySQLDSN != "fixture-custom-dsn" {
		t.Fatal("custom env name not honored")
	}
}

func TestCatalogPathExplicitEmptyRejected(t *testing.T) {
	isolateEnv(t)
	writeBase(t, "agents:\n  catalog_path: \"\"\n")
	t.Setenv("POKER_MYSQL_DSN", "fixture-dsn")
	if _, err := Load(); err == nil || err.Error() != "CONFIG_INVALID" {
		t.Fatalf("explicit empty catalog_path got %v want CONFIG_INVALID", err)
	}
}

func TestLocalOverlaySemantics(t *testing.T) {
	base := "server:\n  http:\n    port: 8081\ndata:\n  redis:\n    addr: 127.0.0.1:6380\nagents:\n  allow_paid_models: true\n"
	// 全注释 local 视为 noop（example 即此形态）。
	t.Run("comments_noop", func(t *testing.T) {
		isolateEnv(t)
		dir := t.TempDir()
		writeFile(t, dir, "config.yaml", base)
		writeFile(t, dir, "config.local.yaml", "# comment only\n\n# more\n")
		t.Setenv("POKER_CONFIG_FILE", filepath.Join(dir, "config.yaml"))
		t.Setenv("POKER_MYSQL_DSN", "fixture-dsn")
		c, err := Load()
		if err != nil || c.HTTPPort != 8081 || !c.AllowPaidModels {
			t.Fatalf("comments local must be noop: %v %+v", err, c)
		}
	})
	// 空 mapping local 也视为 noop。
	t.Run("empty_mapping_noop", func(t *testing.T) {
		isolateEnv(t)
		dir := t.TempDir()
		writeFile(t, dir, "config.yaml", base)
		writeFile(t, dir, "config.local.yaml", "{}\n")
		t.Setenv("POKER_CONFIG_FILE", filepath.Join(dir, "config.yaml"))
		t.Setenv("POKER_MYSQL_DSN", "fixture-dsn")
		c, err := Load()
		if err != nil || c.HTTPPort != 8081 {
			t.Fatalf("empty mapping local must be noop: %v %+v", err, c)
		}
	})
	// local root null / 字段 null / 未知字段 / alias 必须拒绝。
	for _, tc := range []struct{ name, local string }{
		{"root_null", "null\n"},
		{"field_null", "server:\n  http:\n    port:\n"},
		{"null_section", "server:\n  http:\n"},
		{"unknown_field", "server:\n  http:\n    bogus: 1\n"},
		{"alias", "agents: &a\n  allow_paid_models: false\nserver: *a\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolateEnv(t)
			dir := t.TempDir()
			writeFile(t, dir, "config.yaml", base)
			writeFile(t, dir, "config.local.yaml", tc.local)
			t.Setenv("POKER_CONFIG_FILE", filepath.Join(dir, "config.yaml"))
			t.Setenv("POKER_MYSQL_DSN", "fixture-dsn")
			if _, err := Load(); err == nil || err.Error() != "CONFIG_INVALID" {
				t.Fatalf("local %s got %v want CONFIG_INVALID", tc.name, err)
			}
		})
	}
}

func TestDiscoverPathExplicitEmptyRejected(t *testing.T) {
	isolateEnv(t)
	t.Setenv("POKER_CONFIG_FILE", "")
	if _, err := discoverPath(); err == nil || err.Error() != "CONFIG_READ_FAILED" {
		t.Fatalf("explicit empty POKER_CONFIG_FILE got %v want CONFIG_READ_FAILED", err)
	}
	t.Setenv("POKER_CONFIG_FILE", "/tmp/x.yaml")
	if p, err := discoverPath(); err != nil || p != "/tmp/x.yaml" {
		t.Fatalf("explicit path not honored: %q %v", p, err)
	}
	os.Unsetenv("POKER_CONFIG_FILE")
	if p, err := discoverPath(); err != nil || p != DefaultPath {
		t.Fatalf("unset must fall back to default: %q %v", p, err)
	}
}

func TestLoadFileEmptyRejected(t *testing.T) {
	isolateEnv(t)
	if _, err := LoadFile(""); err == nil || err.Error() != "CONFIG_READ_FAILED" {
		t.Fatalf("LoadFile empty got %v want CONFIG_READ_FAILED", err)
	}
	if _, err := LoadPublicFile(""); err == nil || err.Error() != "CONFIG_READ_FAILED" {
		t.Fatalf("LoadPublicFile empty got %v want CONFIG_READ_FAILED", err)
	}
}

// quoteYAML 输出一个安全的双引号 YAML 标量，避免测试文本本身破坏文档结构。
func quoteYAML(s string) string {
	return "\"" + strings.ReplaceAll(s, "\"", "\\\"") + "\""
}
