package web

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
)

// ──────────────────────────── 常量 ────────────────────────────

// tokenFixtureYAML 带 context_window 的 models.defaults 测试配置（对齐上游模型条目结构）
const tokenFixtureYAML = `models:
  defaults:
    - model_client_config:
        model_name: qwen3-coder
        api_base: https://example.com/v1
        api_key: sk-test
        client_provider: OpenAI
      model_config_obj:
        temperature: 0.95
        context_window: 32000
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
			"model_name":     "qwen3-coder",
			"api_base":       "https://example.com/v1",
			"api_key":        "sk-test",
			"model_provider": "OpenAI",
			"context_window": "32k",
			"max_tokens":     8192,
		},
	}
	got, err := buildModelsDefaultsFromFrontend(ctx, models, nil)
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	mco, _ := got[0]["model_config_obj"].(map[string]any)
	if mco["context_window"] != 32768 {
		t.Errorf("context_window 应为 32768（32k），实际 %v", mco["context_window"])
	}
	if mco["max_tokens"] != 8192 {
		t.Errorf("max_tokens 应为 8192，实际 %v", mco["max_tokens"])
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
	mco, _ = got[0]["model_config_obj"].(map[string]any)
	if mco["context_window"] != 0 || mco["max_tokens"] != 0 {
		t.Errorf("未配置时应写 0，实际 window=%v tokens=%v", mco["context_window"], mco["max_tokens"])
	}

	// 负数 / 非整数 → ConfigBadRequest
	bad := []any{map[string]any{
		"model_name": "qwen3-coder", "api_base": "https://example.com/v1",
		"api_key": "sk-test", "model_provider": "OpenAI",
		"context_window": -1,
	}}
	if _, err := buildModelsDefaultsFromFrontend(ctx, bad, nil); err == nil {
		t.Error("负数应返回错误")
	} else if _, ok := err.(*ConfigBadRequest); !ok {
		t.Errorf("应为 *ConfigBadRequest，实际 %T", err)
	}

	bad[0].(map[string]any)["context_window"] = 0
	if _, err := buildModelsDefaultsFromFrontend(ctx, bad, nil); err == nil {
		t.Error("0 应视为未配置并返回错误（此处为 -1 残留值）")
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
			item["context_window"] = tokens
		}
		got, err := buildModelsDefaultsFromFrontend(ctx, []any{item}, nil)
		if err != nil {
			t.Fatalf("不应报错: %v", err)
		}
		return got[0]["model_config_obj"].(map[string]any)
	}

	if v := build(32000)["context_window"]; v != 32000 {
		t.Errorf("未改动应保留 32000，实际 %v", v)
	}
	if v := build(64000)["context_window"]; v != 64000 {
		t.Errorf("改动应覆盖为 64000，实际 %v", v)
	}
	if v := build(nil)["context_window"]; v != 0 {
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

// TestBuildModelsDefaultsFromFrontend_Token字段边界 覆盖空串与输出上限非法值分支
func TestBuildModelsDefaultsFromFrontend_Token字段边界(t *testing.T) {
	ctx := context.Background()
	_ = useTokenConfigHome(t)

	base := func() map[string]any {
		return map[string]any{
			"model_name": "qwen3-coder", "api_base": "https://example.com/v1",
			"api_key": "sk-test", "model_provider": "OpenAI",
		}
	}

	// 空串视为未配置
	item := base()
	item["context_window"] = ""
	item["max_tokens"] = ""
	got, err := buildModelsDefaultsFromFrontend(ctx, []any{item}, nil)
	if err != nil {
		t.Fatalf("空串不应报错: %v", err)
	}
	mco, _ := got[0]["model_config_obj"].(map[string]any)
	if mco["context_window"] != 0 || mco["max_tokens"] != 0 {
		t.Errorf("空串应写 0，实际 %v/%v", mco["context_window"], mco["max_tokens"])
	}

	// 输出上限非法值
	for _, badVal := range []any{-5, "abc"} {
		bad := base()
		bad["max_tokens"] = badVal
		if _, err := buildModelsDefaultsFromFrontend(ctx, []any{bad}, nil); err == nil {
			t.Errorf("max_tokens=%v 应返回错误", badVal)
		} else if _, ok := err.(*ConfigBadRequest); !ok {
			t.Errorf("应为 *ConfigBadRequest，实际 %T", err)
		}
	}
}
