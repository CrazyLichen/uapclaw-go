package metrics

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── 导出函数测试 ────────────────────────────

// TestLLMAsJudgeMetric_Name 测试指标名称
func TestLLMAsJudgeMetric_Name(t *testing.T) {
	m := &LLMAsJudgeMetric{}
	if m.Name() != "llm_as_judge" {
		t.Errorf("期望 Name=llm_as_judge, 实际=%s", m.Name())
	}
}

// TestLLMAsJudgeMetric_HigherIsBetter 测试 HigherIsBetter
func TestLLMAsJudgeMetric_HigherIsBetter(t *testing.T) {
	m := &LLMAsJudgeMetric{}
	if !m.HigherIsBetter() {
		t.Error("期望 HigherIsBetter=true")
	}
}

// TestIsPassResult 测试 isPassResult 判断逻辑
// 对应 Python: DefaultEvaluator._is_pass_result(result)
func TestIsPassResult(t *testing.T) {
	tests := []struct {
		input    any
		expected bool
	}{
		{true, true},
		{false, false},
		{"true", true},
		{"True", true},
		{"TRUE", true},
		{"false", false},
		{"yes", false},
		{1, false},
		{nil, false},
	}
	for _, tt := range tests {
		result := isPassResult(tt.input)
		if result != tt.expected {
			t.Errorf("isPassResult(%v) = %v, 期望 %v", tt.input, result, tt.expected)
		}
	}
}

// TestNewLLMAsJudgeMetric_构造测试 测试构造函数基本参数传递
func TestNewLLMAsJudgeMetric_构造测试(t *testing.T) {
	// 构造时需要有效的 ModelClientConfig 和 ModelRequestConfig
	// 由于内部会创建 Model，测试环境可能无法完整创建
	// 这里仅验证参数校验逻辑
	_, err := NewLLMAsJudgeMetric(
		schema.ModelClientConfig{}, // 空 config 应该失败
		schema.ModelRequestConfig{},
		"",
	)
	if err == nil {
		t.Error("期望空 ModelClientConfig 时返回错误")
	}
}

// TestNewLLMAsJudgeMetric_有效配置 测试有效配置创建
func TestNewLLMAsJudgeMetric_有效配置(t *testing.T) {
	cfg := schema.NewModelClientConfig("llm_OpenAI", "test-key", "http://localhost:11434/v1")
	m, err := NewLLMAsJudgeMetric(
		*cfg,
		schema.ModelRequestConfig{ModelName: "test-model"},
		"custom_rule",
	)
	if err != nil {
		t.Fatalf("不期望错误: %v", err)
	}
	if m == nil {
		t.Error("期望返回非 nil LLMAsJudgeMetric")
	}
	if m.parser == nil {
		t.Error("期望 parser 已初始化")
	}
	if m.template == nil {
		t.Error("期望 template 已初始化")
	}
}
