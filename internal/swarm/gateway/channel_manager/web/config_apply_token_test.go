package web

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
)

// ──────────────────────────── 常量 ────────────────────────────

// tokenFixtureYAML 带 max_input_tokens 的 models.defaults 测试配置
const tokenFixtureYAML = `models:
  defaults:
    - model_client_config:
        model_name: qwen3-coder
        api_base: https://example.com/v1
        api_key: sk-test
        client_provider: OpenAI
        max_input_tokens: 32000
      model_config_obj:
        temperature: 0.95
      is_default: true
`

// ──────────────────────────── 导出函数 ────────────────────────────

// TestBuildModelsDefaultsFromFrontend_Token字段 验证两个 token 字段的写入与非法值拦截
func TestBuildModelsDefaultsFromFrontend_Token字段(t *testing.T) {
	ctx := context.Background()
	_ = useTokenConfigHome(t)

	// 正常值写入 mcc
	models := []any{
		map[string]any{
			"model_name":        "qwen3-coder",
			"api_base":          "https://example.com/v1",
			"api_key":           "sk-test",
			"model_provider":    "OpenAI",
			"max_input_tokens":  32000,
			"max_output_tokens": 8192,
		},
	}
	got, err := buildModelsDefaultsFromFrontend(ctx, models, nil)
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	mcc, _ := got[0]["model_client_config"].(map[string]any)
	if mcc["max_input_tokens"] != 32000 {
		t.Errorf("max_input_tokens 应为 32000，实际 %v", mcc["max_input_tokens"])
	}
	if mcc["max_output_tokens"] != 8192 {
		t.Errorf("max_output_tokens 应为 8192，实际 %v", mcc["max_output_tokens"])
	}

	// 未填 → 写 0（0 表示未配置，可覆盖旧值）
	models[0] = map[string]any{
		"model_name": "qwen3-coder", "api_base": "https://example.com/v1",
		"api_key": "sk-test", "model_provider": "OpenAI",
	}
	got, err = buildModelsDefaultsFromFrontend(ctx, models, nil)
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	mcc, _ = got[0]["model_client_config"].(map[string]any)
	if mcc["max_input_tokens"] != 0 || mcc["max_output_tokens"] != 0 {
		t.Errorf("未配置时应写 0，实际 input=%v output=%v", mcc["max_input_tokens"], mcc["max_output_tokens"])
	}

	// 负数 / 非整数 → ConfigBadRequest
	bad := []any{map[string]any{
		"model_name": "qwen3-coder", "api_base": "https://example.com/v1",
		"api_key": "sk-test", "model_provider": "OpenAI",
		"max_input_tokens": -1,
	}}
	if _, err := buildModelsDefaultsFromFrontend(ctx, bad, nil); err == nil {
		t.Error("负数应返回错误")
	} else if _, ok := err.(*ConfigBadRequest); !ok {
		t.Errorf("应为 *ConfigBadRequest，实际 %T", err)
	}

	bad[0].(map[string]any)["max_input_tokens"] = "abc"
	if _, err := buildModelsDefaultsFromFrontend(ctx, bad, nil); err == nil {
		t.Error("非整数应返回错误")
	}
}

// TestBuildModelsDefaultsFromFrontend_Token字段覆盖与保留 验证 merge 的"未改保留 / 改则覆盖"语义
func TestBuildModelsDefaultsFromFrontend_Token字段覆盖与保留(t *testing.T) {
	ctx := context.Background()
	writeTokenFixture(t)

	build := func(tokens any) map[string]any {
		item := map[string]any{
			"model_name": "qwen3-coder", "api_base": "https://example.com/v1",
			"api_key": "sk-test", "model_provider": "OpenAI",
			"origin_index": 0,
		}
		if tokens != nil {
			item["max_input_tokens"] = tokens
		}
		got, err := buildModelsDefaultsFromFrontend(ctx, []any{item}, nil)
		if err != nil {
			t.Fatalf("不应报错: %v", err)
		}
		return got[0]["model_client_config"].(map[string]any)
	}

	if v := build(32000)["max_input_tokens"]; v != 32000 {
		t.Errorf("未改动应保留 32000，实际 %v", v)
	}
	if v := build(64000)["max_input_tokens"]; v != 64000 {
		t.Errorf("改动应覆盖为 64000，实际 %v", v)
	}
	if v := build(nil)["max_input_tokens"]; v != 0 {
		t.Errorf("清空应写 0，实际 %v", v)
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// useTokenConfigHome 把配置数据根指向临时目录，返回该目录。
// 注入原理：WorkspaceDir() 优先取 UAPCLAW_DATA_DIR，ConfigDir() = WorkspaceDir()/config，
// config.New("") → pathutil.ConfigFile() = ConfigDir()/config.yaml。
func useTokenConfigHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("UAPCLAW_DATA_DIR", home)
	pathutil.ResetCache()
	t.Cleanup(pathutil.ResetCache)
	return home
}

// writeTokenFixture 写入带 max_input_tokens 的 config.yaml 到当前数据根。
func writeTokenFixture(t *testing.T) {
	t.Helper()
	home := useTokenConfigHome(t)
	cfgDir := filepath.Join(home, "config")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatalf("建配置目录失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "config.yaml"), []byte(tokenFixtureYAML), 0o600); err != nil {
		t.Fatalf("写 fixture 失败: %v", err)
	}
}
