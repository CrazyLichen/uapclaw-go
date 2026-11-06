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
		"context_window: 0",
		"max_tokens: 0",
		"trigger_context_ratio: 0.8",
		"min_target_context_ratio: 0.1",
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
	if _, ok := cec["compression_trigger_ratio"]; ok {
		t.Error("compression_trigger_ratio 已删除（由各压缩器 trigger_context_ratio 取代）")
	}
	roundLevel, _ := cec["round_level_compressor_config"].(map[string]any)
	if ratio, ok := roundLevel["trigger_context_ratio"].(float64); !ok || ratio != 0.8 {
		t.Errorf("round_level trigger_context_ratio 应为 0.8，实际 %v", roundLevel["trigger_context_ratio"])
	}
	if ratio, ok := roundLevel["min_target_context_ratio"].(float64); !ok || ratio != 0.1 {
		t.Errorf("round_level min_target_context_ratio 应为 0.1，实际 %v", roundLevel["min_target_context_ratio"])
	}
	models, _ := data["models"].(map[string]any)
	defaults, _ := models["defaults"].([]any)
	if len(defaults) == 0 {
		t.Fatal("models.defaults 为空")
	}
	first, _ := defaults[0].(map[string]any)
	mco, _ := first["model_config_obj"].(map[string]any)
	if _, ok := mco["context_window"]; !ok {
		t.Error("model_config_obj 缺少 context_window")
	}
	if _, ok := mco["max_tokens"]; !ok {
		t.Error("model_config_obj 缺少 max_tokens")
	}
	mcc, _ := first["model_client_config"].(map[string]any)
	if _, ok := mcc["max_input_tokens"]; ok {
		t.Error("model_client_config 不应再有 max_input_tokens")
	}
}
