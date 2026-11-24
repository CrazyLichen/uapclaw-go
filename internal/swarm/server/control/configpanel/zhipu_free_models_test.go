package configpanel

import (
	"os"
	"path/filepath"
	"testing"

	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
)

// useZhipuConfigDir 临时配置目录（同 control 包布局）。
func useZhipuConfigDir(t *testing.T) {
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

// TestZhipuFreeModelEntries_形状 条目应为 models.defaults 形状（zen 同构契约）。
func TestZhipuFreeModelEntries_形状(t *testing.T) {
	t.Setenv("ZHIPU_BUILTIN_API_KEY", "test-key")
	entries := ZhipuFreeModelEntries()
	if len(entries) == 0 {
		t.Fatal("env 注入 key 后应产出条目")
	}
	mcc := entries[0]["model_client_config"].(map[string]any)
	if mcc["model_name"] != "glm-4.5-flash" {
		t.Fatalf("model_name 应为 glm-4.5-flash，got %v", mcc["model_name"])
	}
	if mcc["api_base"] != ZhipuAPIBase || mcc["api_key"] != "test-key" {
		t.Fatalf("api 三要素不符: %v", mcc)
	}
	if mcc["client_provider"] != "OpenAI" {
		t.Fatal("client_provider 应为 OpenAI（OpenAI 兼容协议）")
	}
	if entries[0]["is_free"] != true {
		t.Fatal("is_free 应为 true")
	}
}

// TestAppendZhipuFreeModels_内置key缺省展示 27.4 内置 key 落库（用户拍板）后，
// 缺省（env 未配置）即用内置常量展示条目；"key 缺省回空"分支仅在部署方
// 清空内置常量时生效（防御性，源码常量非空时不可达）。
func TestAppendZhipuFreeModels_内置key缺省展示(t *testing.T) {
	t.Setenv("ZHIPU_BUILTIN_API_KEY", "")
	rows := AppendZhipuFreeModels(nil)
	if len(rows) != 1 {
		t.Fatalf("内置 key 缺省应展示 1 条免费档, got %v", rows)
	}
	if rows[0]["api_key"] != ZhipuBuiltinAPIKey {
		t.Fatalf("应使用内置常量 key: %v", rows[0]["api_key"])
	}
}

// TestResolveZhipuAPIKey_env优先 env 注入优先于内置常量。
func TestResolveZhipuAPIKey_env优先(t *testing.T) {
	t.Setenv("ZHIPU_BUILTIN_API_KEY", "env-key")
	if got := resolveZhipuAPIKey(); got != "env-key" {
		t.Fatalf("env 应优先: %q", got)
	}
	t.Setenv("ZHIPU_BUILTIN_API_KEY", "")
	if got := resolveZhipuAPIKey(); got != ZhipuBuiltinAPIKey {
		t.Fatalf("无 env 应回内置常量: %q", got)
	}
}

// TestAppendZhipuFreeModels_注入展示与去重 env 注入后展示，且与既有条目同名去重。
func TestAppendZhipuFreeModels_注入展示与去重(t *testing.T) {
	t.Setenv("ZHIPU_BUILTIN_API_KEY", "test-key")
	rows := AppendZhipuFreeModels(nil)
	found := false
	for _, r := range rows {
		if r["model_name"] == "glm-4.5-flash" {
			found = true
			if r["is_free"] != true || r["api_base"] != ZhipuAPIBase {
				t.Fatalf("行字段不符: %v", r)
			}
		}
	}
	if !found {
		t.Fatal("key 注入后应展示 glm-4.5-flash")
	}
	// 去重：已含同名条目时不重复追加
	existing := []map[string]any{{"model_name": "glm-4.5-flash"}}
	if got := AppendZhipuFreeModels(existing); len(got) != 1 {
		t.Fatalf("同名条目应去重，got %d", len(got))
	}
}

// TestModelsListPayload_含Zhipu条目 models.list 展示链路（zen 关断下 zhipu 为免费档来源）。
func TestModelsListPayload_含Zhipu条目(t *testing.T) {
	useZhipuConfigDir(t)
	t.Setenv("ZHIPU_BUILTIN_API_KEY", "test-key")
	payload := ModelsListPayload(ReadRawConfig())
	rows := payload["models"].([]map[string]any)
	found := false
	for _, r := range rows {
		if r["model_name"] == "glm-4.5-flash" {
			found = true
			if r["is_free"] != true {
				t.Fatal("zhipu 条目 is_free 应为 true")
			}
		}
	}
	if !found {
		t.Fatal("models.list 应追加内置 zhipu 免费条目")
	}
}
