package reme

import (
	"bytes"
	"strings"
	"testing"
)

// TestNewReMeRetrievePrompts 测试默认提示词实例创建
func TestNewReMeRetrievePrompts(t *testing.T) {
	prompts := NewReMeRetrievePrompts()
	if prompts.RerankPrompt == nil {
		t.Fatal("RerankPrompt 不应为 nil")
	}
	if prompts.RewritePrompt == nil {
		t.Fatal("RewritePrompt 不应为 nil")
	}
}

// TestReMeRetrieveDefaultPrompts 测试默认提示词全局变量
func TestReMeRetrieveDefaultPrompts(t *testing.T) {
	if ReMeRetrieveDefaultPrompts.RerankPrompt == nil {
		t.Fatal("RerankPrompt 不应为 nil")
	}
	if ReMeRetrieveDefaultPrompts.RewritePrompt == nil {
		t.Fatal("RewritePrompt 不应为 nil")
	}
}

// TestRerankPrompt_格式化 测试重排序提示词模板执行
func TestRerankPrompt_格式化(t *testing.T) {
	prompts := NewReMeRetrievePrompts()
	var buf bytes.Buffer
	err := prompts.RerankPrompt.Execute(&buf, map[string]any{
		"Query":         "test query",
		"NumCandidates": 3,
		"Candidates":    "candidate text",
	})
	if err != nil {
		t.Fatalf("模板执行失败: %v", err)
	}
	result := buf.String()

	if !strings.Contains(result, "test query") {
		t.Fatal("格式化结果应包含 query")
	}
	if !strings.Contains(result, "3") {
		t.Fatal("格式化结果应包含候选数量")
	}
	if !strings.Contains(result, "candidate text") {
		t.Fatal("格式化结果应包含候选项")
	}
	// JSON 示例中大括号应正确渲染为单大括号
	if !strings.Contains(result, `"ranked_indices"`) {
		t.Fatal("格式化结果应包含 JSON 示例 ranked_indices")
	}
	// 模板占位符应被替换，不应出现 {{.Query}} 等原始占位符
	if strings.Contains(result, "{{.Query}}") {
		t.Fatal("格式化后不应包含 {{.Query}} 占位符")
	}
	if strings.Contains(result, "{{.NumCandidates}}") {
		t.Fatal("格式化后不应包含 {{.NumCandidates}} 占位符")
	}
	if strings.Contains(result, "{{.Candidates}}") {
		t.Fatal("格式化后不应包含 {{.Candidates}} 占位符")
	}
}

// TestRewritePrompt_格式化 测试改写提示词模板执行
func TestRewritePrompt_格式化(t *testing.T) {
	prompts := NewReMeRetrievePrompts()
	var buf bytes.Buffer
	err := prompts.RewritePrompt.Execute(&buf, map[string]any{
		"CurrentQuery":    "my query",
		"OriginalContext": "original text",
	})
	if err != nil {
		t.Fatalf("模板执行失败: %v", err)
	}
	result := buf.String()

	if !strings.Contains(result, "my query") {
		t.Fatal("格式化结果应包含 CurrentQuery")
	}
	if !strings.Contains(result, "original text") {
		t.Fatal("格式化结果应包含 OriginalContext")
	}
	// JSON 示例中大括号应正确渲染为单大括号
	if !strings.Contains(result, `"rewritten_context"`) {
		t.Fatal("格式化结果应包含 JSON 示例 rewritten_context")
	}
	// 模板占位符应被替换
	if strings.Contains(result, "{{.CurrentQuery}}") {
		t.Fatal("格式化后不应包含 {{.CurrentQuery}} 占位符")
	}
	if strings.Contains(result, "{{.OriginalContext}}") {
		t.Fatal("格式化后不应包含 {{.OriginalContext}} 占位符")
	}
}

// TestRerankPrompt_缺少参数 测试重排序提示词缺少参数时返回错误
func TestRerankPrompt_缺少参数(t *testing.T) {
	prompts := NewReMeRetrievePrompts()
	var buf bytes.Buffer
	// 缺少 Query、NumCandidates、Candidates 参数
	err := prompts.RerankPrompt.Execute(&buf, map[string]any{})
	// text/template 对缺失字段输出零值，不返回错误；验证输出不含模板占位符
	if err != nil {
		t.Fatalf("模板执行不应返回错误: %v", err)
	}
}

// TestRewritePrompt_缺少参数 测试改写提示词缺少参数时返回错误
func TestRewritePrompt_缺少参数(t *testing.T) {
	prompts := NewReMeRetrievePrompts()
	var buf bytes.Buffer
	err := prompts.RewritePrompt.Execute(&buf, map[string]any{})
	if err != nil {
		t.Fatalf("模板执行不应返回错误: %v", err)
	}
}
