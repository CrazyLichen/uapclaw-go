package team

import (
	"testing"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestBuildDefaultModelDict_Token字段透传 验证团队默认模型字典保留两个 token 键
func TestBuildDefaultModelDict_Token字段透传(t *testing.T) {
	configBase := map[string]any{
		"models": map[string]any{
			"defaults": []any{
				map[string]any{
					"model_client_config": map[string]any{
						"model_name":        "qwen3-coder",
						"api_base":          "https://example.com/v1",
						"api_key":           "sk-test",
						"client_provider":   "OpenAI",
						"max_input_tokens":  32000,
						"max_output_tokens": 8192,
					},
					"is_default": true,
				},
			},
		},
	}
	got := buildDefaultModelDict(configBase)
	mcc, _ := got["model_client_config"].(map[string]any)
	if mcc["max_input_tokens"] != 32000 {
		t.Errorf("max_input_tokens 应透传 32000，实际 %v", mcc["max_input_tokens"])
	}
	if mcc["max_output_tokens"] != 8192 {
		t.Errorf("max_output_tokens 应透传 8192，实际 %v", mcc["max_output_tokens"])
	}
}
