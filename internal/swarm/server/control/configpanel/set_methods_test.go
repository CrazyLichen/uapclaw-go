package configpanel

import (
	"os"
	"path/filepath"
	"testing"

	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
)

// useSetMethodsDir 临时配置目录（同 control 包布局）。
func useSetMethodsDir(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	pathutil.SetUserHome(home)
	t.Cleanup(func() { pathutil.SetUserHome("") })
	configDir := filepath.Join(home, ".uapclaw", "config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pathutil.ConfigFile(), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestLocaleSetConf_校验与写盘 zh/en 校验逐字（Python: preferred_language must be zh or en）。
func TestLocaleSetConf_校验与写盘(t *testing.T) {
	useSetMethodsDir(t)
	if err := LocaleSetConf(map[string]any{"preferred_language": "fr"}); err == nil {
		t.Fatal("非法语言应报 400")
	}
	if err := LocaleSetConf(map[string]any{"preferred_language": "en"}); err != nil {
		t.Fatalf("合法语言失败: %v", err)
	}
	if GetNestedConfigValue(ReadRawConfig(), []string{"preferred_language"}, nil) != "en" {
		t.Fatal("preferred_language 应写入 yaml")
	}
}

// TestMemoryForbiddenSet_description合并 description dict 合并语义（Python config.py:3404-3405）。
func TestMemoryForbiddenSet_description合并(t *testing.T) {
	useSetMethodsDir(t)
	MemoryForbiddenSet(map[string]any{"enabled": true, "description": map[string]any{"zh": "不要记忆A"}})
	MemoryForbiddenSet(map[string]any{"description": map[string]any{"en": "No memory B"}})
	sec := GetNestedConfigValue(ReadRawConfig(), []string{"memory", "forbidden_memory_definition"}, nil)
	m := sec.(map[string]any)
	if m["enabled"] != true {
		t.Fatalf("enabled 应保留，got %v", m)
	}
	desc := m["description"].(map[string]any)
	if desc["zh"] != "不要记忆A" || desc["en"] != "No memory B" {
		t.Fatalf("description 应合并而非覆盖，got %v", desc)
	}
}

// TestPermissionsOwnerScopes_读写读写回（Python config.py:1188/1199）。
func TestPermissionsOwnerScopes_读写读写回(t *testing.T) {
	useSetMethodsDir(t)
	got := GetPermissionsOwnerScopes()
	if _, ok := got["owner_scopes"].(map[string]any); !ok {
		t.Fatalf("缺省 owner_scopes 应为空表，got %v", got["owner_scopes"])
	}
	UpdatePermissionsOwnerScopes(map[string]any{"a": "b"}, "提示语")
	got = GetPermissionsOwnerScopes()
	if got["deny_guidance_message"] != "提示语" {
		t.Fatalf("deny_guidance_message 应写入，got %v", got)
	}
	os := got["owner_scopes"].(map[string]any)
	if os["a"] != "b" {
		t.Fatalf("owner_scopes 应写入，got %v", os)
	}
}

// TestPathSet_写盘与校验 chrome_path 类型校验 + headless 非 bool 回 true（Python :352-360）。
func TestPathSet_写盘与校验(t *testing.T) {
	useSetMethodsDir(t)
	if _, _, err := PathSet(map[string]any{"chrome_path": 123}); err == nil {
		t.Fatal("chrome_path 非字符串应报 400")
	}
	cp, headless, err := PathSet(map[string]any{"chrome_path": " /usr/bin/chrome ", "headless": "notbool"})
	if err != nil {
		t.Fatalf("失败: %v", err)
	}
	if cp != "/usr/bin/chrome" || headless != true {
		t.Fatalf("期望 trim 后路径 + headless 默认 true，got %s/%v", cp, headless)
	}
	if GetNestedConfigValue(ReadRawConfig(), []string{"browser", "chrome_path"}, nil) != "/usr/bin/chrome" {
		t.Fatal("browser.chrome_path 应写盘")
	}
}
