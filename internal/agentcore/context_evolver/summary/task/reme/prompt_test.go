package reme

import (
	"bytes"
	"strings"
	"testing"
)

// TestReMeSummaryDefaultPrompts 验证所有提示词模板非空
func TestReMeSummaryDefaultPrompts(t *testing.T) {
	p := ReMeSummaryDefaultPrompts
	if p.ComparativeMemoryPrompt == nil {
		t.Error("ComparativeMemoryPrompt 不应为空")
	}
	if p.SuccessMemoryPrompt == nil {
		t.Error("SuccessMemoryPrompt 不应为空")
	}
	if p.FailureMemoryPrompt == nil {
		t.Error("FailureMemoryPrompt 不应为空")
	}
	if p.ComparativeAllMemoryPrompt == nil {
		t.Error("ComparativeAllMemoryPrompt 不应为空")
	}
	if p.MemoryValidationPrompt == nil {
		t.Error("MemoryValidationPrompt 不应为空")
	}
}

// TestComparativeMemoryPrompt_格式化 用模板渲染验证
func TestComparativeMemoryPrompt_格式化(t *testing.T) {
	prompts := NewReMeSummaryPrompts()
	var buf bytes.Buffer
	err := prompts.ComparativeMemoryPrompt.Execute(&buf, map[string]any{
		"HigherScore": 9.5,
		"HigherSteps": "step A",
		"LowerScore":  3.2,
		"LowerSteps":  "step B",
	})
	if err != nil {
		t.Fatalf("模板执行失败: %v", err)
	}
	result := buf.String()
	if !strings.Contains(result, "9.5") {
		t.Error("应包含 HigherScore=9.5")
	}
	if !strings.Contains(result, "step A") {
		t.Error("应包含 HigherSteps=step A")
	}
	if !strings.Contains(result, "3.2") {
		t.Error("应包含 LowerScore=3.2")
	}
	if !strings.Contains(result, "step B") {
		t.Error("应包含 LowerSteps=step B")
	}
	if !strings.Contains(result, "```json") {
		t.Error("应包含 ```json 代码块")
	}
}

// TestSuccessMemoryPrompt_格式化 用模板渲染验证
func TestSuccessMemoryPrompt_格式化(t *testing.T) {
	prompts := NewReMeSummaryPrompts()
	var buf bytes.Buffer
	err := prompts.SuccessMemoryPrompt.Execute(&buf, map[string]any{
		"Query":        "test query",
		"StepSequence": "step sequence",
		"Outcome":      "successful",
	})
	if err != nil {
		t.Fatalf("模板执行失败: %v", err)
	}
	result := buf.String()
	if !strings.Contains(result, "test query") {
		t.Error("应包含 Query")
	}
	if !strings.Contains(result, "step sequence") {
		t.Error("应包含 StepSequence")
	}
	if !strings.Contains(result, "successful") {
		t.Error("应包含 Outcome")
	}
	if !strings.Contains(result, "```json") {
		t.Error("应包含 ```json 代码块")
	}
}

// TestFailureMemoryPrompt_格式化 用模板渲染验证
func TestFailureMemoryPrompt_格式化(t *testing.T) {
	prompts := NewReMeSummaryPrompts()
	var buf bytes.Buffer
	err := prompts.FailureMemoryPrompt.Execute(&buf, map[string]any{
		"Query":        "test query",
		"StepSequence": "step sequence",
		"Outcome":      "failed",
	})
	if err != nil {
		t.Fatalf("模板执行失败: %v", err)
	}
	result := buf.String()
	if !strings.Contains(result, "test query") {
		t.Error("应包含 Query")
	}
	if !strings.Contains(result, "step sequence") {
		t.Error("应包含 StepSequence")
	}
	if !strings.Contains(result, "failed") {
		t.Error("应包含 Outcome")
	}
	if !strings.Contains(result, "```json") {
		t.Error("应包含 ```json 代码块")
	}
}

// TestComparativeAllMemoryPrompt_格式化 用模板渲染验证
func TestComparativeAllMemoryPrompt_格式化(t *testing.T) {
	prompts := NewReMeSummaryPrompts()
	var buf bytes.Buffer
	err := prompts.ComparativeAllMemoryPrompt.Execute(&buf, map[string]any{
		"Trajectory": "trajectory data",
	})
	if err != nil {
		t.Fatalf("模板执行失败: %v", err)
	}
	result := buf.String()
	if !strings.Contains(result, "trajectory data") {
		t.Error("应包含 Trajectory")
	}
	if !strings.Contains(result, "```json") {
		t.Error("应包含 ```json 代码块")
	}
}

// TestMemoryValidationPrompt_格式化 用模板渲染验证
func TestMemoryValidationPrompt_格式化(t *testing.T) {
	prompts := NewReMeSummaryPrompts()
	var buf bytes.Buffer
	err := prompts.MemoryValidationPrompt.Execute(&buf, map[string]any{
		"Condition":        "test condition",
		"TaskMemoryContent": "memory content",
	})
	if err != nil {
		t.Fatalf("模板执行失败: %v", err)
	}
	result := buf.String()
	if !strings.Contains(result, "test condition") {
		t.Error("应包含 Condition")
	}
	if !strings.Contains(result, "memory content") {
		t.Error("应包含 TaskMemoryContent")
	}
	if !strings.Contains(result, "```json") {
		t.Error("应包含 ```json 代码块")
	}
}
