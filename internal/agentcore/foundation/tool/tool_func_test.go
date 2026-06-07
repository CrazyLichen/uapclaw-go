package tool

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 导出函数测试 ────────────────────────────

// TestNewTool_最简用法 测试 NewTool(fn) 自动推断
func TestNewTool_最简用法(t *testing.T) {
	fn, err := NewTool(searchFunc)
	if err != nil {
		t.Fatalf("NewTool 失败: %v", err)
	}
	if fn.Card().Name == "" {
		t.Error("Name 不应为空")
	}
	if len(fn.Card().InputParams) == 0 {
		t.Error("InputParams 不应为空")
	}
}

// TestNewTool_自定义名称 测试 WithToolName
func TestNewTool_自定义名称(t *testing.T) {
	fn, err := NewTool(searchFunc, WithToolName("custom_search"))
	if err != nil {
		t.Fatalf("NewTool 失败: %v", err)
	}
	if fn.Card().Name != "custom_search" {
		t.Errorf("Name: 期望 custom_search，实际 %q", fn.Card().Name)
	}
}

// TestNewTool_自定义描述 测试 WithToolDescription
func TestNewTool_自定义描述(t *testing.T) {
	fn, err := NewTool(searchFunc, WithToolDescription("自定义搜索工具"))
	if err != nil {
		t.Fatalf("NewTool 失败: %v", err)
	}
	if fn.Card().Description != "自定义搜索工具" {
		t.Errorf("Description: 期望 自定义搜索工具，实际 %q", fn.Card().Description)
	}
}

// TestNewTool_Invoke端到端 测试 NewTool 创建后的完整调用
func TestNewTool_Invoke端到端(t *testing.T) {
	fn, _ := NewTool(searchFunc, WithToolName("search"))
	result, err := fn.Invoke(context.Background(), map[string]any{
		"query": "hello",
	})
	if err != nil {
		t.Fatalf("Invoke 失败: %v", err)
	}
	if result == nil {
		t.Error("result 不应为 nil")
	}
}

// TestNewStreamTool_最简用法 测试 NewStreamTool 自动推断
func TestNewStreamTool_最简用法(t *testing.T) {
	fn, err := NewStreamTool(streamSearchFunc, WithToolName("stream_search"))
	if err != nil {
		t.Fatalf("NewStreamTool 失败: %v", err)
	}
	if fn.Card().Name != "stream_search" {
		t.Errorf("Name: 期望 stream_search，实际 %q", fn.Card().Name)
	}
}

// TestNewTool_WithToolInputParams 测试 WithToolInputParams 选项
func TestNewTool_WithToolInputParams(t *testing.T) {
	customParams := []*schema.Param{
		schema.NewStringParam("q", "查询", true),
	}
	fn, err := NewTool(searchFunc, WithToolInputParams(customParams))
	if err != nil {
		t.Fatalf("NewTool 失败: %v", err)
	}
	if len(fn.Card().InputParams) != 1 {
		t.Errorf("InputParams: 期望 1 个，实际 %d", len(fn.Card().InputParams))
	}
}

// TestNewTool_WithToolCard 测试 WithToolCard 选项
func TestNewTool_WithToolCard(t *testing.T) {
	card := NewToolCard("my_tool", "自定义", []*schema.Param{
		schema.NewStringParam("q", "查询", true),
	}, nil)
	fn, err := NewTool(searchFunc, WithToolCard(card))
	if err != nil {
		t.Fatalf("NewTool 失败: %v", err)
	}
	if fn.Card().Name != "my_tool" {
		t.Errorf("Name: 期望 my_tool，实际 %q", fn.Card().Name)
	}
}
