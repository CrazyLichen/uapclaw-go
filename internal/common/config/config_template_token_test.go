package config

import (
	"os"
	"strings"
	"testing"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestResourcesConfigTemplate_Token与压缩段 验证模板包含 token 字段与压缩配置段
func TestResourcesConfigTemplate_Token与压缩段(t *testing.T) {
	raw, err := os.ReadFile("../../../resources/config.yaml")
	if err != nil {
		t.Fatalf("读取模板失败: %v", err)
	}
	text := string(raw)
	required := []string{
		"max_input_tokens",
		"max_output_tokens",
		"compression_trigger_ratio: 0.85",
		"compression_target_ratio: 0.55",
		"enabled: true",
		"enable_reload: true",
		"message_summary_offloader_config",
		"dialogue_compressor_config",
		"current_round_compressor_config",
		"round_level_compressor_config",
		"full_compact_processor_config",
		"tool_result_budget_processor_config",
	}
	for _, key := range required {
		if !strings.Contains(text, key) {
			t.Errorf("模板缺少 %q", key)
		}
	}
}

// TestResourcesConfigTemplate_可解析 验证模板是合法 YAML 且关键值与 Python 模板一致
func TestResourcesConfigTemplate_可解析(t *testing.T) {
	cfg, err := New("../../../resources/config.yaml")
	if err != nil {
		t.Fatalf("构造 config 失败: %v", err)
	}
	data, err := cfg.Load()
	if err != nil {
		t.Fatalf("解析模板失败: %v", err)
	}
	react, _ := data["react"].(map[string]any)
	cec, _ := react["context_engine_config"].(map[string]any)
	if cec == nil {
		t.Fatal("缺少 react.context_engine_config")
	}
	if enabled, _ := cec["enabled"].(bool); !enabled {
		t.Errorf("enabled 应为 true，实际 %v", cec["enabled"])
	}
	if ratio, ok := cec["compression_trigger_ratio"].(float64); !ok || ratio != 0.85 {
		t.Errorf("compression_trigger_ratio 应为 0.85，实际 %v", cec["compression_trigger_ratio"])
	}
	if ratio, ok := cec["compression_target_ratio"].(float64); !ok || ratio != 0.55 {
		t.Errorf("compression_target_ratio 应为 0.55，实际 %v", cec["compression_target_ratio"])
	}
	models, _ := data["models"].(map[string]any)
	defaults, _ := models["defaults"].([]any)
	if len(defaults) == 0 {
		t.Fatal("models.defaults 为空")
	}
	first, _ := defaults[0].(map[string]any)
	mcc, _ := first["model_client_config"].(map[string]any)
	if _, ok := mcc["max_input_tokens"]; !ok {
		t.Error("model_client_config 缺少 max_input_tokens")
	}
	if _, ok := mcc["max_output_tokens"]; !ok {
		t.Error("model_client_config 缺少 max_output_tokens")
	}
}
