package rb

import (
	"bytes"
	"strings"
	"testing"
)

// TestNewReasoningBankSummaryPrompt 验证构造函数不 panic 且所有模板非 nil
func TestNewReasoningBankSummaryPrompt(t *testing.T) {
	p := NewReasoningBankSummaryPrompt()
	if p == nil {
		t.Fatal("NewReasoningBankSummaryPrompt 返回 nil")
	}
	if p.ExtractSuccessTrajSystemPrompt == nil {
		t.Error("ExtractSuccessTrajSystemPrompt 不应为 nil")
	}
	if p.ExtractFailTrajSystemPrompt == nil {
		t.Error("ExtractFailTrajSystemPrompt 不应为 nil")
	}
	if p.ExtractTrajUserPrompt == nil {
		t.Error("ExtractTrajUserPrompt 不应为 nil")
	}
	if p.LLMJudgeSystemPrompt == nil {
		t.Error("LLMJudgeSystemPrompt 不应为 nil")
	}
	if p.LLMJudgeUserPrompt == nil {
		t.Error("LLMJudgeUserPrompt 不应为 nil")
	}
	if p.ParallelScalingSystemPrompt == nil {
		t.Error("ParallelScalingSystemPrompt 不应为 nil")
	}
	if p.ParallelScalingUserPrompt == nil {
		t.Error("ParallelScalingUserPrompt 不应为 nil")
	}
}

// TestDefaultReasoningBankSummaryPrompt 验证全局默认实例非 nil
func TestDefaultReasoningBankSummaryPrompt(t *testing.T) {
	if defaultReasoningBankSummaryPrompt == nil {
		t.Fatal("defaultReasoningBankSummaryPrompt 不应为 nil")
	}
}

// TestExtractTrajUserPrompt_执行 验证带占位符模板可正确执行
func TestExtractTrajUserPrompt_执行(t *testing.T) {
	p := NewReasoningBankSummaryPrompt()
	var buf bytes.Buffer
	err := p.ExtractTrajUserPrompt.Execute(&buf, map[string]string{
		"Query":      "test query",
		"Trajectory": "test trajectory",
	})
	if err != nil {
		t.Fatalf("Execute 失败: %v", err)
	}
	result := buf.String()
	if result != "Query: test query\nTrajectory: test trajectory\n" {
		t.Errorf("输出不符合预期, got: %q", result)
	}
}

// TestLLMJudgeUserPrompt_执行 验证带占位符模板可正确执行
func TestLLMJudgeUserPrompt_执行(t *testing.T) {
	p := NewReasoningBankSummaryPrompt()
	var buf bytes.Buffer
	err := p.LLMJudgeUserPrompt.Execute(&buf, map[string]string{
		"Query":      "judge query",
		"Trajectory": "judge trajectory",
	})
	if err != nil {
		t.Fatalf("Execute 失败: %v", err)
	}
	result := buf.String()
	if result != "Query: judge query\nTrajectory: judge trajectory\n" {
		t.Errorf("输出不符合预期, got: %q", result)
	}
}

// TestParallelScalingUserPrompt_执行 验证带占位符模板可正确执行
func TestParallelScalingUserPrompt_执行(t *testing.T) {
	p := NewReasoningBankSummaryPrompt()
	var buf bytes.Buffer
	err := p.ParallelScalingUserPrompt.Execute(&buf, map[string]string{
		"Query":        "scaling query",
		"Trajectories": "scaling trajectories",
	})
	if err != nil {
		t.Fatalf("Execute 失败: %v", err)
	}
	result := buf.String()
	expected := "Query: scaling query\n\nTrajectories:\nscaling trajectories"
	if result != expected {
		t.Errorf("输出不符合预期, got: %q, want: %q", result, expected)
	}
}

// TestSystemPrompts_执行 验证无占位符系统提示词模板可正确执行
func TestSystemPrompts_执行(t *testing.T) {
	p := NewReasoningBankSummaryPrompt()
	data := map[string]string{} // 无占位符，空数据即可

	// 测试 ExtractSuccessTrajSystemPrompt
	var buf bytes.Buffer
	if err := p.ExtractSuccessTrajSystemPrompt.Execute(&buf, data); err != nil {
		t.Fatalf("ExtractSuccessTrajSystemPrompt Execute 失败: %v", err)
	}
	if buf.Len() == 0 {
		t.Error("ExtractSuccessTrajSystemPrompt 输出不应为空")
	}

	// 测试 ExtractFailTrajSystemPrompt
	buf.Reset()
	if err := p.ExtractFailTrajSystemPrompt.Execute(&buf, data); err != nil {
		t.Fatalf("ExtractFailTrajSystemPrompt Execute 失败: %v", err)
	}
	if buf.Len() == 0 {
		t.Error("ExtractFailTrajSystemPrompt 输出不应为空")
	}

	// 测试 LLMJudgeSystemPrompt
	buf.Reset()
	if err := p.LLMJudgeSystemPrompt.Execute(&buf, data); err != nil {
		t.Fatalf("LLMJudgeSystemPrompt Execute 失败: %v", err)
	}
	if buf.Len() == 0 {
		t.Error("LLMJudgeSystemPrompt 输出不应为空")
	}

	// 测试 ParallelScalingSystemPrompt
	buf.Reset()
	if err := p.ParallelScalingSystemPrompt.Execute(&buf, data); err != nil {
		t.Fatalf("ParallelScalingSystemPrompt Execute 失败: %v", err)
	}
	if buf.Len() == 0 {
		t.Error("ParallelScalingSystemPrompt 输出不应为空")
	}
}

// TestPromptContent_包含关键片段 验证提示词包含关键内容
func TestPromptContent_包含关键片段(t *testing.T) {
	// 验证成功轨迹提示词
	if !strings.Contains(extractSuccessTrajSystemPrompt, "successfully accomplished") {
		t.Error("extractSuccessTrajSystemPrompt 应包含 'successfully accomplished'")
	}
	if !strings.Contains(extractSuccessTrajSystemPrompt, "# Memory Item i") {
		t.Error("extractSuccessTrajSystemPrompt 应包含 '# Memory Item i'")
	}

	// 验证失败轨迹提示词
	if !strings.Contains(extractFailTrajSystemPrompt, "attempted to resolve the task but failed") {
		t.Error("extractFailTrajSystemPrompt 应包含 'attempted to resolve the task but failed'")
	}

	// 验证 LLM 判定提示词
	if !strings.Contains(llmJudgeSystemPrompt, "evaluating the performance") {
		t.Error("llmJudgeSystemPrompt 应包含 'evaluating the performance'")
	}
	if !strings.Contains(llmJudgeSystemPrompt, `"success" or "failure"`) {
		t.Error("llmJudgeSystemPrompt 应包含 '\"success\" or \"failure\"'")
	}

	// 验证并行缩放提示词
	if !strings.Contains(parallelScalingSystemPrompt, "self-contrast reasoning") {
		t.Error("parallelScalingSystemPrompt 应包含 'self-contrast reasoning'")
	}
	if !strings.Contains(parallelScalingSystemPrompt, "at most 5 memory items") {
		t.Error("parallelScalingSystemPrompt 应包含 'at most 5 memory items'")
	}
}
