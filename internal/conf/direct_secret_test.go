package conf

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// 本文件覆盖 data.mysql.dsn / data.redis.password 直连字段：
//   - 直连非空值优先于 *_env 引用；
//   - dsn 显式空串等同未设置（回退 dsn_env）；
//   - password 显式空串等同无密码（不回退 password_env）；
//   - 两者都不提供时：dsn 触发 CONFIG_STORAGE_REQUIRED，password 为空；
//   - 新增字段同样过严格类型 / null 校验；
//   - 公共白名单不新增、也不泄露任何直连秘密。
//
// 所有秘密值均为占位假值 "testpw"，绝不使用真实凭据。

const directBaseEnvOnly = `server:
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
    addr: 127.0.0.1:6379
    db: 0
    password_env: POKER_REDIS_PASSWORD
agents:
  catalog_path: auto
  request_timeout: 15s
  max_output_tokens: 256
  allow_paid_models: false
  max_live_calls: 1000
`

func TestDirectSecretOverridesEnv(t *testing.T) {
	isolateEnv(t)
	dir := t.TempDir()
	const directDSN = "root:testpw@tcp(127.0.0.1:3306)/poker"
	writeFile(t, dir, "config.yaml", directBaseEnvOnly)
	writeFile(t, dir, "config.local.yaml", `data:
  mysql:
    dsn: "`+directDSN+`"
  redis:
    password: "testpw"
`)
	t.Setenv("POKER_CONFIG_FILE", filepath.Join(dir, "config.yaml"))
	t.Setenv("POKER_MYSQL_DSN", "env-dsn-should-not-win")
	t.Setenv("POKER_REDIS_PASSWORD", "env-pw-should-not-win")

	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.MySQLDSN != directDSN {
		t.Fatalf("direct dsn must win over env, got %q", c.MySQLDSN)
	}
	if c.RedisPassword != "testpw" {
		t.Fatalf("direct password must win over env, got %q", c.RedisPassword)
	}
}

func TestDirectSecretEmptyStringSemantics(t *testing.T) {
	isolateEnv(t)
	dir := t.TempDir()
	writeFile(t, dir, "config.yaml", directBaseEnvOnly)
	// dsn 显式空串等同未设置：回退 env；password 显式空串等同无密码：不回退 env。
	writeFile(t, dir, "config.local.yaml", `data:
  mysql:
    dsn: ""
  redis:
    password: ""
`)
	t.Setenv("POKER_CONFIG_FILE", filepath.Join(dir, "config.yaml"))
	t.Setenv("POKER_MYSQL_DSN", "env-dsn-fallback")
	t.Setenv("POKER_REDIS_PASSWORD", "env-pw-must-not-be-read")

	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.MySQLDSN != "env-dsn-fallback" {
		t.Fatalf("empty dsn must fall back to env, got %q", c.MySQLDSN)
	}
	if c.RedisPassword != "" {
		t.Fatalf("empty password must mean no password, got %q", c.RedisPassword)
	}
}

func TestEnvOnlyFallbackWithoutDirect(t *testing.T) {
	isolateEnv(t)
	writeBase(t, directBaseEnvOnly)
	t.Setenv("POKER_MYSQL_DSN", "env-dsn-only")
	t.Setenv("POKER_REDIS_PASSWORD", "env-pw-only")

	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.MySQLDSN != "env-dsn-only" || c.RedisPassword != "env-pw-only" {
		t.Fatalf("env-only resolution failed: %q %q", c.MySQLDSN, c.RedisPassword)
	}
}

func TestSecretsAbsentBehaveClearly(t *testing.T) {
	isolateEnv(t)
	base := writeBase(t, directBaseEnvOnly)
	// 无直接值、无 env 值：服务路径缺 DSN 必须报 CONFIG_STORAGE_REQUIRED。
	if _, err := Load(); err == nil || err.Error() != "CONFIG_STORAGE_REQUIRED" {
		t.Fatalf("absent dsn got %v want CONFIG_STORAGE_REQUIRED", err)
	}
	// 解析器层面：dsn / password 都为空串且不报错。
	c, err := load(base)
	if err != nil {
		t.Fatal(err)
	}
	if c.MySQLDSN != "" || c.RedisPassword != "" {
		t.Fatalf("absent secrets must stay empty: %q %q", c.MySQLDSN, c.RedisPassword)
	}
}

func TestDirectSecretStrictTypes(t *testing.T) {
	cases := []struct {
		name string
		text string
	}{
		{"dsn_null", "data:\n  mysql:\n    dsn:\n"},
		{"dsn_int", "data:\n  mysql:\n    dsn: 123\n"},
		{"dsn_bool", "data:\n  mysql:\n    dsn: true\n"},
		{"dsn_sequence", "data:\n  mysql:\n    dsn: [a]\n"},
		{"password_null", "data:\n  redis:\n    password:\n"},
		{"password_int", "data:\n  redis:\n    password: 123\n"},
		{"password_bool", "data:\n  redis:\n    password: false\n"},
		{"password_mapping", "data:\n  redis:\n    password: {}\n"},
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
		})
	}
}

func TestDirectSecretEmptyStringAccepted(t *testing.T) {
	// 类型正确是 string，值可以为空串；空串本身不是类型错误。
	for _, text := range []string{
		"data:\n  mysql:\n    dsn: \"\"\n",
		"data:\n  redis:\n    password: \"\"\n",
	} {
		isolateEnv(t)
		writeBase(t, text)
		t.Setenv("POKER_MYSQL_DSN", "fixture-dsn")
		if _, err := Load(); err != nil {
			t.Fatalf("empty direct secret rejected: %v", err)
		}
	}
}

func TestPublicWhitelistExcludesDirectSecrets(t *testing.T) {
	isolateEnv(t)
	dir := t.TempDir()
	const directDSN = "root:testpw@tcp(127.0.0.1:3306)/poker"
	writeFile(t, dir, "config.yaml", directBaseEnvOnly)
	writeFile(t, dir, "config.local.yaml", `data:
  mysql:
    dsn: "`+directDSN+`"
  redis:
    password: "testpw"
`)
	t.Setenv("POKER_CONFIG_FILE", filepath.Join(dir, "config.yaml"))

	p, err := LoadPublic()
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	out := string(b)
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(b, &keys); err != nil || len(keys) != 5 {
		t.Fatalf("public output must stay exactly 5 keys: %s", out)
	}
	for _, leak := range []string{"testpw", "dsn", "password", "redis", "mysql", "3306"} {
		if strings.Contains(out, leak) {
			t.Fatalf("public output leaked %q: %s", leak, out)
		}
	}
}
