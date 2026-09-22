package reme

import (
	"fmt"
	"strings"
	"testing"
)

// TestReMeSummaryDefaultPrompts 验证所有提示词非空
func TestReMeSummaryDefaultPrompts(t *testing.T) {
	p := ReMeSummaryDefaultPrompts
	if p.ComparativeMemoryPrompt == "" {
		t.Error("ComparativeMemoryPrompt 不应为空")
	}
	if p.SuccessMemoryPrompt == "" {
		t.Error("SuccessMemoryPrompt 不应为空")
	}
	if p.FailureMemoryPrompt == "" {
		t.Error("FailureMemoryPrompt 不应为空")
	}
	if p.ComparativeAllMemoryPrompt == "" {
		t.Error("ComparativeAllMemoryPrompt 不应为空")
	}
	if p.MemoryValidationPrompt == "" {
		t.Error("MemoryValidationPrompt 不应为空")
	}
}

// TestComparativeMemoryPrompt_格式化 用 strings.ReplaceAll 替换占位符验证
func TestComparativeMemoryPrompt_格式化(t *testing.T) {
	result := comparativeMemoryPrompt
	result = strings.ReplaceAll(result, "{higher_score}", fmt.Sprintf("%v", 9.5))
	result = strings.ReplaceAll(result, "{higher_steps}", "step A")
	result = strings.ReplaceAll(result, "{lower_score}", fmt.Sprintf("%v", 3.2))
	result = strings.ReplaceAll(result, "{lower_steps}", "step B")
	if !strings.Contains(result, "9.5") {
		t.Error("应包含 higher_score=9.5")
	}
	if !strings.Contains(result, "step A") {
		t.Error("应包含 higher_steps=step A")
	}
	if !strings.Contains(result, "3.2") {
		t.Error("应包含 lower_score=3.2")
	}
	if !strings.Contains(result, "step B") {
		t.Error("应包含 lower_steps=step B")
	}
	if !strings.Contains(result, "```json") {
		t.Error("应包含 ```json 代码块")
	}
}

// TestSuccessMemoryPrompt_格式化 用 strings.ReplaceAll 替换占位符验证
func TestSuccessMemoryPrompt_格式化(t *testing.T) {
	result := successMemoryPrompt
	result = strings.ReplaceAll(result, "{query}", "test query")
	result = strings.ReplaceAll(result, "{step_sequence}", "step sequence")
	result = strings.ReplaceAll(result, "{outcome}", "successful")
	if !strings.Contains(result, "test query") {
		t.Error("应包含 query")
	}
	if !strings.Contains(result, "step sequence") {
		t.Error("应包含 step_sequence")
	}
	if !strings.Contains(result, "successful") {
		t.Error("应包含 outcome")
	}
	if !strings.Contains(result, "```json") {
		t.Error("应包含 ```json 代码块")
	}
}

// TestFailureMemoryPrompt_格式化 用 strings.ReplaceAll 替换占位符验证
func TestFailureMemoryPrompt_格式化(t *testing.T) {
	result := failureMemoryPrompt
	result = strings.ReplaceAll(result, "{query}", "test query")
	result = strings.ReplaceAll(result, "{step_sequence}", "step sequence")
	result = strings.ReplaceAll(result, "{outcome}", "failed")
	if !strings.Contains(result, "test query") {
		t.Error("应包含 query")
	}
	if !strings.Contains(result, "step sequence") {
		t.Error("应包含 step_sequence")
	}
	if !strings.Contains(result, "failed") {
		t.Error("应包含 outcome")
	}
	if !strings.Contains(result, "```json") {
		t.Error("应包含 ```json 代码块")
	}
}

// TestComparativeAllMemoryPrompt_格式化 用 strings.ReplaceAll 替换占位符验证
func TestComparativeAllMemoryPrompt_格式化(t *testing.T) {
	result := comparativeAllMemoryPrompt
	result = strings.ReplaceAll(result, "{trajectory}", "trajectory data")
	if !strings.Contains(result, "trajectory data") {
		t.Error("应包含 trajectory")
	}
	if !strings.Contains(result, "```json") {
		t.Error("应包含 ```json 代码块")
	}
}

// TestMemoryValidationPrompt_格式化 用 strings.ReplaceAll 替换占位符验证
func TestMemoryValidationPrompt_格式化(t *testing.T) {
	result := memoryValidationPrompt
	result = strings.ReplaceAll(result, "{condition}", "test condition")
	result = strings.ReplaceAll(result, "{task_memory_content}", "memory content")
	if !strings.Contains(result, "test condition") {
		t.Error("应包含 condition")
	}
	if !strings.Contains(result, "memory content") {
		t.Error("应包含 task_memory_content")
	}
	if !strings.Contains(result, "```json") {
		t.Error("应包含 ```json 代码块")
	}
}
