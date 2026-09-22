package reme

import (
	"strings"
	"testing"
)

// TestReMeRetrieveDefaultPrompts 测试默认提示词实例
func TestReMeRetrieveDefaultPrompts(t *testing.T) {
	if ReMeRetrieveDefaultPrompts.RerankPrompt == "" {
		t.Fatal("RerankPrompt 不应为空")
	}
	if ReMeRetrieveDefaultPrompts.RewritePrompt == "" {
		t.Fatal("RewritePrompt 不应为空")
	}

	// 验证包含关键占位符
	if !strings.Contains(ReMeRetrieveDefaultPrompts.RerankPrompt, "{query}") {
		t.Fatal("RerankPrompt 应包含 {query} 占位符")
	}
	if !strings.Contains(ReMeRetrieveDefaultPrompts.RerankPrompt, "{num_candidates}") {
		t.Fatal("RerankPrompt 应包含 {num_candidates} 占位符")
	}
	if !strings.Contains(ReMeRetrieveDefaultPrompts.RerankPrompt, "{candidates}") {
		t.Fatal("RerankPrompt 应包含 {candidates} 占位符")
	}
	if !strings.Contains(ReMeRetrieveDefaultPrompts.RewritePrompt, "{current_query}") {
		t.Fatal("RewritePrompt 应包含 {current_query} 占位符")
	}
	if !strings.Contains(ReMeRetrieveDefaultPrompts.RewritePrompt, "{original_context}") {
		t.Fatal("RewritePrompt 应包含 {original_context} 占位符")
	}
}

// TestRerankPrompt_格式化 测试重排序提示词格式化
func TestRerankPrompt_格式化(t *testing.T) {
	result := FormatRerankPrompt(ReMeRetrieveDefaultPrompts.RerankPrompt, "test query", 3, "candidate text")
	if !strings.Contains(result, "test query") {
		t.Fatal("格式化结果应包含 query")
	}
	if !strings.Contains(result, "3") {
		t.Fatal("格式化结果应包含候选数量")
	}
	if !strings.Contains(result, "candidate text") {
		t.Fatal("格式化结果应包含候选项")
	}
	// 占位符应被替换
	if strings.Contains(result, "{query}") {
		t.Fatal("格式化后不应包含 {query} 占位符")
	}
	if strings.Contains(result, "{num_candidates}") {
		t.Fatal("格式化后不应包含 {num_candidates} 占位符")
	}
	if strings.Contains(result, "{candidates}") {
		t.Fatal("格式化后不应包含 {candidates} 占位符")
	}
}

// TestRewritePrompt_格式化 测试改写提示词格式化
func TestRewritePrompt_格式化(t *testing.T) {
	result := FormatRewritePrompt(ReMeRetrieveDefaultPrompts.RewritePrompt, "my query", "original text")
	if !strings.Contains(result, "my query") {
		t.Fatal("格式化结果应包含 current_query")
	}
	if !strings.Contains(result, "original text") {
		t.Fatal("格式化结果应包含 original_context")
	}
	// 占位符应被替换
	if strings.Contains(result, "{current_query}") {
		t.Fatal("格式化后不应包含 {current_query} 占位符")
	}
	if strings.Contains(result, "{original_context}") {
		t.Fatal("格式化后不应包含 {original_context} 占位符")
	}
}
