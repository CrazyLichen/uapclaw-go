package web

import (
	"os"
	"path/filepath"
	"testing"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestHandleModelsList_Token字段回填 验证两个 token 字段从 YAML 回填到扁平结构
func TestHandleModelsList_Token字段回填(t *testing.T) {
	home := useTokenConfigHome(t)
	cfgDir := filepath.Join(home, "config")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatalf("建配置目录失败: %v", err)
	}
	fixture := `models:
  defaults:
    - model_client_config:
        model_name: qwen3-coder
        api_base: https://example.com/v1
        api_key: sk-test
        client_provider: OpenAI
        max_input_tokens: 32000
        max_output_tokens: 8192
      model_config_obj:
        temperature: 0.95
      is_default: true
`
	if err := os.WriteFile(filepath.Join(cfgDir, "config.yaml"), []byte(fixture), 0o600); err != nil {
		t.Fatalf("写 fixture 失败: %v", err)
	}

	handler := modelsList(newTestChannel())
	resp, err := callHandler(handler, nil, "")
	if err != nil {
		t.Fatalf("handler 报错: %v", err)
	}
	models, _ := resp["models"].([]map[string]any)
	if len(models) != 1 {
		t.Fatalf("应返回 1 条，实际 %d", len(models))
	}
	if models[0]["max_input_tokens"] != 32000 {
		t.Errorf("max_input_tokens 应为 32000，实际 %v", models[0]["max_input_tokens"])
	}
	if models[0]["max_output_tokens"] != 8192 {
		t.Errorf("max_output_tokens 应为 8192，实际 %v", models[0]["max_output_tokens"])
	}
}

// TestHandleModelsList_Token字段非法值不回填 覆盖 parseInt 失败分支
func TestHandleModelsList_Token字段非法值不回填(t *testing.T) {
	home := useTokenConfigHome(t)
	cfgDir := filepath.Join(home, "config")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatalf("建配置目录失败: %v", err)
	}
	fixture := `models:
  defaults:
    - model_client_config:
        model_name: qwen3-coder
        api_base: https://example.com/v1
        api_key: sk-test
        client_provider: OpenAI
        max_input_tokens: abc
        max_output_tokens: -5
      model_config_obj:
        temperature: 0.95
      is_default: true
`
	if err := os.WriteFile(filepath.Join(cfgDir, "config.yaml"), []byte(fixture), 0o600); err != nil {
		t.Fatalf("写 fixture 失败: %v", err)
	}

	handler := modelsList(newTestChannel())
	resp, err := callHandler(handler, nil, "")
	if err != nil {
		t.Fatalf("handler 报错: %v", err)
	}
	models, _ := resp["models"].([]map[string]any)
	if len(models) != 1 {
		t.Fatalf("应返回 1 条，实际 %d", len(models))
	}
	if _, exists := models[0]["max_input_tokens"]; exists {
		t.Errorf("非法 max_input_tokens 不应回填，实际 %v", models[0]["max_input_tokens"])
	}
	if _, exists := models[0]["max_output_tokens"]; exists {
		t.Errorf("负数 max_output_tokens 不应回填，实际 %v", models[0]["max_output_tokens"])
	}
}
