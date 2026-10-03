package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseArgs(t *testing.T) {
	for _, tc := range []struct {
		name     string
		args     []string
		path     string
		explicit bool
		migrate  bool
		errCode  string
	}{
		{"no_args", nil, "", false, false, ""},
		{"conf", []string{"-conf", "x.yaml"}, "x.yaml", true, false, ""},
		{"conf_empty", []string{"-conf", ""}, "", true, false, ""},
		{"conf_equals_empty", []string{"-conf="}, "", true, false, ""},
		{"migrate", []string{"migrate"}, "", false, true, ""},
		{"conf_migrate", []string{"-conf", "x.yaml", "migrate"}, "x.yaml", true, true, ""},
		{"bad_flag", []string{"-bogus"}, "", false, false, "INVALID_COMMAND"},
		{"bad_subcommand", []string{"bogus"}, "", false, false, "INVALID_COMMAND"},
		{"extra_arg", []string{"migrate", "x"}, "", false, false, "INVALID_COMMAND"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, ex, mig, err := parseArgs(tc.args)
			if tc.errCode != "" {
				if err == nil || err.Error() != tc.errCode {
					t.Fatalf("got %v want %s", err, tc.errCode)
				}
				return
			}
			if err != nil || p != tc.path || ex != tc.explicit || mig != tc.migrate {
				t.Fatalf("got path=%q explicit=%v migrate=%v err=%v", p, ex, mig, err)
			}
		})
	}
}

func TestResolveConfigExplicitEmptyRejected(t *testing.T) {
	// 显式 -conf "" 必须拒绝，不得回退 POKER_CONFIG_FILE 或默认路径。
	if _, err := resolveConfig("", true); err == nil || err.Error() != "CONFIG_READ_FAILED" {
		t.Fatalf("got %v want CONFIG_READ_FAILED", err)
	}
}

// 服务路径必须能加载 config.local.yaml 中的直连 dsn / password（不启动 HTTP、不连库）。
func TestResolveConfigLoadsDirectSecrets(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	const directDSN = "root:testpw@tcp(127.0.0.1:3306)/poker"
	path := filepath.Join(t.TempDir(), "config.yaml")
	text := `server:
  http:
    host: 127.0.0.1
    port: 8083
    timeout: 10s
data:
  mysql:
    dsn: "` + directDSN + `"
  redis:
    addr: 127.0.0.1:6379
    db: 0
    password: "testpw"
`
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatal("fixture write failed")
	}
	c, err := resolveConfig(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if c.MySQLDSN != directDSN || c.RedisPassword != "testpw" {
		t.Fatalf("direct secrets not resolved in service path: %q %q", c.MySQLDSN, c.RedisPassword)
	}
}
