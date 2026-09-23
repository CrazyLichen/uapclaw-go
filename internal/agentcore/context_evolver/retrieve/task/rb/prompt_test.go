package rb

import (
	"bytes"
	"strings"
	"testing"
)

// TestNewReasoningBankPrompt 测试默认提示词实例创建
func TestNewReasoningBankPrompt(t *testing.T) {
	prompts := NewReasoningBankPrompt()
	if prompts.LLMJudgeSystemPrompt == nil {
		t.Fatal("LLMJudgeSystemPrompt 不应为 nil")
	}
	if prompts.LLMJudgeUserPrompt == nil {
		t.Fatal("LLMJudgeUserPrompt 不应为 nil")
	}
	if prompts.BestOfNEvalPrompt == nil {
		t.Fatal("BestOfNEvalPrompt 不应为 nil")
	}
	if prompts.SelfContrastPrompt == nil {
		t.Fatal("SelfContrastPrompt 不应为 nil")
	}
	if prompts.SequentialFirstRefinePrompt == nil {
		t.Fatal("SequentialFirstRefinePrompt 不应为 nil")
	}
	if prompts.SequentialFollowUpRefinePrompt == nil {
		t.Fatal("SequentialFollowUpRefinePrompt 不应为 nil")
	}
}

// TestDefaultReasoningBankPrompt 测试默认提示词全局变量
func TestDefaultReasoningBankPrompt(t *testing.T) {
	if defaultReasoningBankPrompt.LLMJudgeSystemPrompt == nil {
		t.Fatal("defaultReasoningBankPrompt.LLMJudgeSystemPrompt 不应为 nil")
	}
	if defaultReasoningBankPrompt.SelfContrastPrompt == nil {
		t.Fatal("defaultReasoningBankPrompt.SelfContrastPrompt 不应为 nil")
	}
}

// TestLLMJudgeSystemPrompt_格式化 测试 LLM 评判系统提示词模板执行
func TestLLMJudgeSystemPrompt_格式化(t *testing.T) {
	prompts := NewReasoningBankPrompt()
	var buf bytes.Buffer
	err := prompts.LLMJudgeSystemPrompt.Execute(&buf, map[string]any{})
	if err != nil {
		t.Fatalf("模板执行失败: %v", err)
	}
	result := buf.String()
	if !strings.Contains(result, "evaluating the performance of an agent") {
		t.Fatal("格式化结果应包含评判描述")
	}
	if !strings.Contains(result, `"success" or "failure"`) {
		t.Fatal("格式化结果应包含状态选项")
	}
}

// TestLLMJudgeUserPrompt_格式化 测试 LLM 评判用户提示词模板执行
func TestLLMJudgeUserPrompt_格式化(t *testing.T) {
	prompts := NewReasoningBankPrompt()
	var buf bytes.Buffer
	err := prompts.LLMJudgeUserPrompt.Execute(&buf, map[string]any{
		"Query":      "test query",
		"Trajectory": "test trajectory",
	})
	if err != nil {
		t.Fatalf("模板执行失败: %v", err)
	}
	result := buf.String()
	if !strings.Contains(result, "test query") {
		t.Fatal("格式化结果应包含 Query")
	}
	if !strings.Contains(result, "test trajectory") {
		t.Fatal("格式化结果应包含 Trajectory")
	}
	// 模板占位符应被替换
	if strings.Contains(result, "{{.Query}}") {
		t.Fatal("格式化后不应包含 {{.Query}} 占位符")
	}
	if strings.Contains(result, "{{.Trajectory}}") {
		t.Fatal("格式化后不应包含 {{.Trajectory}} 占位符")
	}
}

// TestBestOfNEvalPrompt_格式化 测试 Best-of-N 评估提示词模板执行
func TestBestOfNEvalPrompt_格式化(t *testing.T) {
	prompts := NewReasoningBankPrompt()
	var buf bytes.Buffer
	err := prompts.BestOfNEvalPrompt.Execute(&buf, map[string]any{
		"NumTrajectories":  3,
		"Query":            "find the answer",
		"TrajDescriptions": "Trajectory 1: ...\nTrajectory 2: ...",
		"MaxIndex":         2,
	})
	if err != nil {
		t.Fatalf("模板执行失败: %v", err)
	}
	result := buf.String()
	if !strings.Contains(result, "3 candidate trajectories") {
		t.Fatal("格式化结果应包含候选数量")
	}
	if !strings.Contains(result, "find the answer") {
		t.Fatal("格式化结果应包含 Query")
	}
	if !strings.Contains(result, "Trajectory 1: ...") {
		t.Fatal("格式化结果应包含 TrajDescriptions")
	}
	if !strings.Contains(result, "0-2") {
		t.Fatal("格式化结果应包含 MaxIndex 范围")
	}
	// 模板占位符应被替换
	if strings.Contains(result, "{{.NumTrajectories}}") {
		t.Fatal("格式化后不应包含 {{.NumTrajectories}} 占位符")
	}
}

// TestSelfContrastPrompt_格式化 测试自对比记忆提取提示词模板执行
func TestSelfContrastPrompt_格式化(t *testing.T) {
	prompts := NewReasoningBankPrompt()
	var buf bytes.Buffer
	err := prompts.SelfContrastPrompt.Execute(&buf, map[string]any{
		"Query":                  "solve the problem",
		"NumSuccessful":          2,
		"SuccessfulTrajectories": "Trajectory 0: ok...",
		"NumFailed":              1,
		"FailedTrajectories":     "Trajectory 1: bad...",
	})
	if err != nil {
		t.Fatalf("模板执行失败: %v", err)
	}
	result := buf.String()
	if !strings.Contains(result, "solve the problem") {
		t.Fatal("格式化结果应包含 Query")
	}
	if !strings.Contains(result, "Successful Trajectories (2)") {
		t.Fatal("格式化结果应包含成功轨迹数量")
	}
	if !strings.Contains(result, "Trajectory 0: ok...") {
		t.Fatal("格式化结果应包含成功轨迹内容")
	}
	if !strings.Contains(result, "Failed Trajectories (1)") {
		t.Fatal("格式化结果应包含失败轨迹数量")
	}
	if !strings.Contains(result, "Trajectory 1: bad...") {
		t.Fatal("格式化结果应包含失败轨迹内容")
	}
	// 输出格式部分的 Markdown 标题应正确渲染
	if !strings.Contains(result, "# Memory Item 1") {
		t.Fatal("格式化结果应包含 Memory Item 1 示例")
	}
	if !strings.Contains(result, "## Title") {
		t.Fatal("格式化结果应包含 Title 示例")
	}
	// 模板占位符应被替换
	if strings.Contains(result, "{{.Query}}") {
		t.Fatal("格式化后不应包含 {{.Query}} 占位符")
	}
	if strings.Contains(result, "{{.NumSuccessful}}") {
		t.Fatal("格式化后不应包含 {{.NumSuccessful}} 占位符")
	}
}

// TestSequentialFirstRefinePrompt_格式化 测试串行缩放首次精炼提示词模板执行
func TestSequentialFirstRefinePrompt_格式化(t *testing.T) {
	prompts := NewReasoningBankPrompt()
	var buf bytes.Buffer
	err := prompts.SequentialFirstRefinePrompt.Execute(&buf, map[string]any{
		"CurrentAnswer": "my previous answer",
		"Query":         "my query",
	})
	if err != nil {
		t.Fatalf("模板执行失败: %v", err)
	}
	result := buf.String()
	if !strings.Contains(result, "my previous answer") {
		t.Fatal("格式化结果应包含 CurrentAnswer")
	}
	if !strings.Contains(result, "my query") {
		t.Fatal("格式化结果应包含 Query")
	}
	if !strings.Contains(result, "carefully re-examine") {
		t.Fatal("格式化结果应包含首次精炼引导语")
	}
	// 模板占位符应被替换
	if strings.Contains(result, "{{.CurrentAnswer}}") {
		t.Fatal("格式化后不应包含 {{.CurrentAnswer}} 占位符")
	}
}

// TestSequentialFollowUpRefinePrompt_格式化 测试串行缩放后续精炼提示词模板执行
func TestSequentialFollowUpRefinePrompt_格式化(t *testing.T) {
	prompts := NewReasoningBankPrompt()
	var buf bytes.Buffer
	err := prompts.SequentialFollowUpRefinePrompt.Execute(&buf, map[string]any{
		"CurrentAnswer": "updated answer",
		"Query":         "follow up query",
	})
	if err != nil {
		t.Fatalf("模板执行失败: %v", err)
	}
	result := buf.String()
	if !strings.Contains(result, "updated answer") {
		t.Fatal("格式化结果应包含 CurrentAnswer")
	}
	if !strings.Contains(result, "follow up query") {
		t.Fatal("格式化结果应包含 Query")
	}
	if !strings.Contains(result, "Let's check again") {
		t.Fatal("格式化结果应包含后续精炼引导语")
	}
	// 模板占位符应被替换
	if strings.Contains(result, "{{.CurrentAnswer}}") {
		t.Fatal("格式化后不应包含 {{.CurrentAnswer}} 占位符")
	}
}
