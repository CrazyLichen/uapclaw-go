package adapter

import (
	"testing"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestBuildModelFromEntry_输出上限 验证 max_output_tokens 落成 ModelRequestConfig.MaxTokens 且不进 Extra
func TestBuildModelFromEntry_输出上限(t *testing.T) {
	d := &DeepAdapter{}

	mcc := map[string]any{
		"model_name":        "qwen3-coder",
		"api_base":          "https://example.com/v1",
		"api_key":           "sk-test",
		"client_provider":   "OpenAI",
		"verify_ssl":        false,
		"max_input_tokens":  32000,
		"max_output_tokens": 8192,
	}
	mco := map[string]any{"temperature": 0.95}

	m, err := d.buildModelFromEntry(mcc, mco)
	if err != nil {
		t.Fatalf("构建失败: %v", err)
	}
	if m.ModelConfig == nil || m.ModelConfig.MaxTokens == nil {
		t.Fatal("MaxTokens 不应为 nil")
	}
	if *m.ModelConfig.MaxTokens != 8192 {
		t.Errorf("MaxTokens 应为 8192，实际 %d", *m.ModelConfig.MaxTokens)
	}
	// 白名单生效：两个键都不应进 client Extra
	if m.ClientConfig != nil {
		if _, ok := m.ClientConfig.Extra["max_output_tokens"]; ok {
			t.Error("max_output_tokens 不应进入 ModelClientConfig.Extra")
		}
		if _, ok := m.ClientConfig.Extra["max_input_tokens"]; ok {
			t.Error("max_input_tokens 不应进入 ModelClientConfig.Extra")
		}
	}

	// 未配置 → nil
	mcc2 := map[string]any{
		"model_name": "qwen3-coder", "api_base": "https://example.com/v1",
		"api_key": "sk-test", "client_provider": "OpenAI", "verify_ssl": false,
	}
	m2, err := d.buildModelFromEntry(mcc2, mco)
	if err != nil {
		t.Fatalf("构建失败: %v", err)
	}
	if m2.ModelConfig.MaxTokens != nil {
		t.Errorf("未配置时 MaxTokens 应为 nil，实际 %d", *m2.ModelConfig.MaxTokens)
	}
}
