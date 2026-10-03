package conf

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"texas-holdem/internal/agent"
)

// 合成目录无凭据、环境引用或文件引用，不会读取真实用户模型配置。
const jsonCatalogFixture = `{"provider":{"fixture":{"npm":"custom-package","models":{"json-only":{},"shared":{}}}}}`
const jsoncCatalogFixture = `{
	// JSONC 覆盖 provider，但必须保留 JSON 的独有模型。
	"provider":{"fixture":{"npm":"@ai-sdk/openai-compatible","models":{"jsonc-only":{},"shared":{},},},},
}`

// isolatedCatalogHome 隔离环境并准备一个 base 配置；返回 opencode 合成目录。
func isolatedCatalogHome(t *testing.T) string {
	t.Helper()
	isolateEnv(t)
	home := os.Getenv("HOME")
	t.Setenv("POKER_MYSQL_DSN", "test-only-dsn")
	t.Setenv("POKER_REDIS_ADDR", "127.0.0.1:6379")
	writeBase(t, validBase)
	dir := filepath.Join(home, ".config", "opencode")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal("fixture directory creation failed")
	}
	return dir
}

func writeCatalogFixture(t *testing.T, dir, name, text string) string {
	t.Helper()
	return writeFile(t, dir, name, text)
}

func TestDefaultCatalogDiscoveryAndLoader(t *testing.T) {
	for _, tc := range []struct {
		name        string
		json, jsonc bool
		selected    string
		wantRefs    []string
		wantKind    agent.ProviderKind
	}{
		{"json_only", true, false, "opencode.json", []string{"mock", "fixture/json-only", "fixture/shared"}, "unsupported"},
		{"jsonc_only", false, true, "opencode.jsonc", []string{"mock", "fixture/jsonc-only", "fixture/shared"}, "openai_compatible"},
		{"both", true, true, "opencode.jsonc", []string{"mock", "fixture/json-only", "fixture/jsonc-only", "fixture/shared"}, "openai_compatible"},
		{"neither", false, false, "opencode.jsonc", nil, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := isolatedCatalogHome(t)
			if tc.json {
				writeCatalogFixture(t, dir, "opencode.json", jsonCatalogFixture)
			}
			if tc.jsonc {
				writeCatalogFixture(t, dir, "opencode.jsonc", jsoncCatalogFixture)
			}
			c, err := Load()
			if err != nil {
				t.Fatal(err)
			}
			if c.CatalogPath != filepath.Join(dir, tc.selected) {
				t.Errorf("selected catalog filename got %s want %s", filepath.Base(c.CatalogPath), tc.selected)
			}
			catalog, err := agent.LoadCatalog(c.CatalogPath)
			if tc.wantRefs == nil {
				if err == nil || err.Error() != "CONFIG_READ_FAILED" {
					t.Fatalf("missing catalog error got %v want CONFIG_READ_FAILED", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			factory, err := agent.NewFactory(catalog)
			if err != nil {
				t.Fatal(err)
			}
			var refs []string
			for _, model := range factory.Models() {
				refs = append(refs, model.Ref)
				if model.Ref != "mock" && model.Provider != tc.wantKind {
					t.Errorf("provider got %s want %s", model.Provider, tc.wantKind)
				}
			}
			if !reflect.DeepEqual(refs, tc.wantRefs) {
				t.Fatalf("model refs got %v want %v", refs, tc.wantRefs)
			}
		})
	}
}

func TestExplicitMissingCatalogPath(t *testing.T) {
	for _, name := range []string{"missing.json", "opencode.json", "opencode.jsonc"} {
		t.Run(name, func(t *testing.T) {
			dir := isolatedCatalogHome(t)
			// 缺失的显式兄弟路径不得被另一个已有兄弟文件兜底。
			sibling := "opencode.jsonc"
			if name == "opencode.jsonc" {
				sibling = "opencode.json"
			}
			writeCatalogFixture(t, dir, sibling, `{}`)
			path := filepath.Join(dir, name)
			t.Setenv("POKER_CATALOG_PATH", path)
			c, err := Load()
			if err != nil {
				t.Fatal(err)
			}
			if c.CatalogPath != path {
				t.Fatal("explicit path was replaced")
			}
			if _, err := agent.LoadCatalog(c.CatalogPath); err == nil || err.Error() != "CONFIG_READ_FAILED" {
				t.Fatalf("explicit missing catalog error got %v want CONFIG_READ_FAILED", err)
			}
		})
	}
}
