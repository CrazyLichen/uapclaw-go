package tool

import (
	"context"
	"fmt"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 测试用结构体 ────────────────────────────

// searchInput 搜索输入
type searchInput struct {
	Query string `json:"query" jsonschema:"description=搜索关键词,required"`
	Limit int    `json:"limit,omitempty" jsonschema:"description=返回数量上限,default=10"`
}

// searchOutput 搜索输出
type searchOutput struct {
	Results []string `json:"results"`
	Total   int      `json:"total"`
}

// ──────────────────────────── 测试用函数 ────────────────────────────

// searchFunc 搜索函数
func searchFunc(ctx context.Context, input searchInput, opts ...ToolOption) (searchOutput, error) {
	return searchOutput{
		Results: []string{input.Query},
		Total:   input.Limit,
	}, nil
}

// ──────────────────────────── 导出函数测试 ────────────────────────────

// TestNewInvokeFunction_自动推断 创建时自动推断泛型参数
func TestNewInvokeFunction_自动推断(t *testing.T) {
	fn, err := NewInvokeFunction("search", searchFunc)
	if err != nil {
		t.Fatalf("NewInvokeFunction 失败: %v", err)
	}
	if fn.Card().Name != "search" {
		t.Errorf("Name: 期望 search，实际 %q", fn.Card().Name)
	}
	if len(fn.Card().InputParams) != 2 {
		t.Errorf("InputParams: 期望 2 个，实际 %d", len(fn.Card().InputParams))
	}
}

// TestInvokeFunction_Invoke_完整流程 测试 map→struct→fn→map 完整流程
func TestInvokeFunction_Invoke_完整流程(t *testing.T) {
	fn, _ := NewInvokeFunction("search", searchFunc)
	result, err := fn.Invoke(context.Background(), map[string]any{
		"query": "hello",
		"limit": float64(5),
	})
	if err != nil {
		t.Fatalf("Invoke 失败: %v", err)
	}
	results, ok := result["results"].([]any)
	if !ok {
		t.Fatalf("results 类型错误: %T", result["results"])
	}
	if len(results) != 1 || results[0] != "hello" {
		t.Errorf("results: 期望 [hello]，实际 %v", results)
	}
}

// TestInvokeFunction_Invoke_默认值填充 测试默认值填充
func TestInvokeFunction_Invoke_默认值填充(t *testing.T) {
	fn, _ := NewInvokeFunction("search", searchFunc)
	result, err := fn.Invoke(context.Background(), map[string]any{
		"query": "hello",
	})
	if err != nil {
		t.Fatalf("Invoke 失败: %v", err)
	}
	total, ok := result["total"].(float64)
	if !ok {
		t.Fatalf("total 类型错误: %T", result["total"])
	}
	if total != 10 {
		t.Errorf("total: 期望 10（默认值），实际 %v", total)
	}
}

// TestInvokeFunction_Invoke_必填缺失 测试必填字段缺失
func TestInvokeFunction_Invoke_必填缺失(t *testing.T) {
	fn, _ := NewInvokeFunction("search", searchFunc)
	_, err := fn.Invoke(context.Background(), map[string]any{})
	if err == nil {
		t.Error("缺少必填字段 query 应返回错误")
	}
}

// TestInvokeFunction_Stream_不支持 测试 Invoke 模式的 Stream 返回错误
func TestInvokeFunction_Stream_不支持(t *testing.T) {
	fn, _ := NewInvokeFunction("search", searchFunc)
	_, err := fn.Stream(context.Background(), map[string]any{"query": "test"})
	if err == nil {
		t.Error("Invoke 模式 Stream 应返回 ErrStreamNotSupported")
	}
}

// TestInvokeFunction_Card_ToolInfo ToolInfo 测试
func TestInvokeFunction_Card_ToolInfo(t *testing.T) {
	fn, _ := NewInvokeFunction("search", searchFunc)
	info := fn.Card().ToolInfo()
	if info.GetName() != "search" {
		t.Errorf("ToolInfo.Name: 期望 search，实际 %q", info.GetName())
	}
	if info.GetParameters() == nil {
		t.Error("ToolInfo.Parameters 不应为 nil")
	}
}

// TestNewInvokeFunction_自定义描述 测试 WithDescription 选项
func TestNewInvokeFunction_自定义描述(t *testing.T) {
	fn, err := NewInvokeFunction("search", searchFunc, WithDescription("自定义搜索"))
	if err != nil {
		t.Fatalf("NewInvokeFunction 失败: %v", err)
	}
	if fn.Card().Description != "自定义搜索" {
		t.Errorf("Description: 期望 自定义搜索，实际 %q", fn.Card().Description)
	}
}

// TestNewInvokeFunction_手动InputParams 测试 WithInputParams 选项
func TestNewInvokeFunction_手动InputParams(t *testing.T) {
	customParams := []*schema.Param{
		schema.NewStringParam("custom_query", "自定义查询", true),
	}
	fn, err := NewInvokeFunction("search", searchFunc, WithInputParams(customParams))
	if err != nil {
		t.Fatalf("NewInvokeFunction 失败: %v", err)
	}
	if len(fn.Card().InputParams) != 1 {
		t.Errorf("InputParams: 期望 1 个，实际 %d", len(fn.Card().InputParams))
	}
	if fn.Card().InputParams[0].Name != "custom_query" {
		t.Errorf("InputParams[0].Name: 期望 custom_query，实际 %q", fn.Card().InputParams[0].Name)
	}
}

// TestNewInvokeFunction_WithCard 测试 WithCard 选项
func TestNewInvokeFunction_WithCard(t *testing.T) {
	card := NewToolCard("custom", "自定义卡片", []*schema.Param{
		schema.NewStringParam("q", "查询", true),
	}, nil)
	fn, err := NewInvokeFunction("search", searchFunc, WithCard(card))
	if err != nil {
		t.Fatalf("NewInvokeFunction 失败: %v", err)
	}
	if fn.Card().Name != "custom" {
		t.Errorf("Name: 期望 custom，实际 %q", fn.Card().Name)
	}
}

// TestInvokeFunction_Invoke_函数返回错误 测试用户函数执行失败
func TestInvokeFunction_Invoke_函数返回错误(t *testing.T) {
	errFunc := func(ctx context.Context, input searchInput, opts ...ToolOption) (searchOutput, error) {
		return searchOutput{}, fmt.Errorf("执行失败")
	}
	fn, _ := NewInvokeFunction("search", errFunc)
	_, err := fn.Invoke(context.Background(), map[string]any{"query": "test"})
	if err == nil {
		t.Error("函数返回错误时 Invoke 应返回错误")
	}
}

// TestInvokeFunction_Invoke_空输入参数 测试无 InputParams 时的直接调用
func TestInvokeFunction_Invoke_空输入参数(t *testing.T) {
	type emptyFuncInput struct{}
	emptyFunc := func(ctx context.Context, input emptyFuncInput, opts ...ToolOption) (map[string]any, error) {
		return map[string]any{"ok": true}, nil
	}
	fn, _ := NewInvokeFunction("empty", emptyFunc)
	result, err := fn.Invoke(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("Invoke 失败: %v", err)
	}
	if result["ok"] != true {
		t.Errorf("ok: 期望 true，实际 %v", result["ok"])
	}
}
