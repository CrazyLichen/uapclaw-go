package adapter

import (
	"testing"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestBuildModelFromEntry_输出上限 验证 max_output_tokens 落成 ModelRequestConfig.MaxTokens 且不进 Extra
func TestBuildModelFromEntry_输出上限(t *testing.T) {
	d := &DeepAdapter{}

	mcc := map[string]any{
		"model_name":      "qwen3-coder",
		"api_base":        "https://example.com/v1",
		"api_key":         "sk-test",
		"client_provider": "OpenAI",
		"verify_ssl":      false,
	}
	mco := map[string]any{"temperature": 0.95, "context_window": "32k", "max_tokens": 8192}

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
		if _, ok := m.ClientConfig.Extra["max_tokens"]; ok {
			t.Error("max_tokens 不应进入 ModelClientConfig.Extra")
		}
		if _, ok := m.ClientConfig.Extra["context_window"]; ok {
			t.Error("context_window 不应进入 ModelClientConfig.Extra")
		}
	}

	// 未配置 → nil
	mcc2 := map[string]any{
		"model_name": "qwen3-coder", "api_base": "https://example.com/v1",
		"api_key": "sk-test", "client_provider": "OpenAI", "verify_ssl": false,
	}
	m2, err := d.buildModelFromEntry(mcc2, map[string]any{"temperature": 0.95})
	if err != nil {
		t.Fatalf("构建失败: %v", err)
	}
	if m2.ModelConfig.MaxTokens != nil {
		t.Errorf("未配置时 MaxTokens 应为 nil，实际 %d", *m2.ModelConfig.MaxTokens)
	}
}

// TestBuildModelFromEntry_超时与采样参数 锁定 YAML 整数形态的数值字段不被丢弃。
//
// 回归背景：配置由 yaml.v3 解码进 map[string]any，`timeout: 1800` 解出的是 int，
// 而历史实现只做 `v.(float64)` 断言 → 整数值被静默丢弃，ModelClientConfig 回落到
// 默认 60s，长流式生成（写整页 HTML 之类）会被 http.Client.Timeout 硬切断，
// 表现为工具参数 JSON 半截。temperature/top_p 同为该坑。
func TestBuildModelFromEntry_超时与采样参数(t *testing.T) {
	d := &DeepAdapter{}

	base := func() map[string]any {
		return map[string]any{
			"model_name":      "qwen3-coder",
			"api_base":        "https://example.com/v1",
			"api_key":         "sk-test",
			"client_provider": "OpenAI",
			"verify_ssl":      false,
		}
	}

	// YAML 整数形态
	intMCC := base()
	intMCC["timeout"] = 1800
	m, err := d.buildModelFromEntry(intMCC, map[string]any{"temperature": 1, "top_p": 1})
	if err != nil {
		t.Fatalf("构建失败: %v", err)
	}
	if m.ClientConfig.Timeout != 1800 {
		t.Errorf("整数 timeout 应生效为 1800, 实际 %v", m.ClientConfig.Timeout)
	}
	if m.ModelConfig.Temperature == nil || *m.ModelConfig.Temperature != 1 {
		t.Errorf("整数 temperature 应生效为 1, 实际 %v", m.ModelConfig.Temperature)
	}
	if m.ModelConfig.TopP == nil || *m.ModelConfig.TopP != 1 {
		t.Errorf("整数 top_p 应生效为 1, 实际 %v", m.ModelConfig.TopP)
	}

	// float64 形态（JSON 来源）
	floatMCC := base()
	floatMCC["timeout"] = 900.5
	m2, err := d.buildModelFromEntry(floatMCC, map[string]any{"temperature": 0.3})
	if err != nil {
		t.Fatalf("构建失败: %v", err)
	}
	if m2.ClientConfig.Timeout != 900.5 {
		t.Errorf("浮点 timeout 应生效为 900.5, 实际 %v", m2.ClientConfig.Timeout)
	}

	// 未配置 → 保持 ModelClientConfig 默认 360s
	// （Python: schema/config.py:145 默认 360，且须 >= stream_first_chunk_timeout=300）
	m3, err := d.buildModelFromEntry(base(), map[string]any{})
	if err != nil {
		t.Fatalf("构建失败: %v", err)
	}
	if m3.ClientConfig.Timeout != 360 {
		t.Errorf("未配置时应为默认 360s, 实际 %v", m3.ClientConfig.Timeout)
	}
}
