package tools

import (
	"strings"
	"testing"
)

// TestBuildToolCard_tool_search 验证 tool_search 卡片可从注册表构建。
func TestBuildToolCard_tool_search(t *testing.T) {
	card, err := BuildToolCard("tool_search", "ToolSearchTool", "cn", nil, "")
	if err != nil {
		t.Fatalf("构建失败: %v", err)
	}
	if card.Name != "tool_search" {
		t.Fatalf("名称应为 tool_search，got %s", card.Name)
	}
	if !strings.Contains(card.Description, "deferred") {
		t.Fatalf("中文描述应提到 deferred，got %s", card.Description)
	}
	if len(card.InputParams) != 2 {
		t.Fatalf("应有 query/limit 两个参数，got %d", len(card.InputParams))
	}
}

// TestBuildToolCard_tool_call 验证 tool_call 卡片可从注册表构建。
func TestBuildToolCard_tool_call(t *testing.T) {
	card, err := BuildToolCard("tool_call", "ToolCallTool", "en", nil, "")
	if err != nil {
		t.Fatalf("构建失败: %v", err)
	}
	if card.Name != "tool_call" {
		t.Fatalf("名称应为 tool_call，got %s", card.Name)
	}
	if !strings.Contains(strings.ToLower(card.Description), "deferred") {
		t.Fatalf("英文描述应提到 deferred，got %s", card.Description)
	}
	if len(card.InputParams) != 2 {
		t.Fatalf("应有 name/args 两个参数，got %d", len(card.InputParams))
	}
}

// TestGetToolSearchInputParams 验证参数 Schema 的必填项与默认值。
func TestGetToolSearchInputParams(t *testing.T) {
	schemaMap := GetToolSearchInputParams("cn")
	properties, ok := schemaMap["properties"].(map[string]any)
	if !ok {
		t.Fatal("properties 缺失")
	}
	limit, ok := properties["limit"].(map[string]any)
	if !ok {
		t.Fatal("limit 缺失")
	}
	if limit["default"] != 5 {
		t.Fatalf("limit 默认值应为 5，got %v", limit["default"])
	}
	required, ok := schemaMap["required"].([]any)
	if !ok || len(required) != 1 || required[0] != "query" {
		t.Fatalf("required 应为 [query]，got %v", schemaMap["required"])
	}
	if schemaMap["additionalProperties"] != false {
		t.Fatal("additionalProperties 应为 false")
	}
}

// TestGetToolCallInputParams 验证 tool_call 参数 Schema。
func TestGetToolCallInputParams(t *testing.T) {
	schemaMap := GetToolCallInputParams("en")
	required, ok := schemaMap["required"].([]any)
	if !ok || len(required) != 2 {
		t.Fatalf("required 应为 name/args，got %v", schemaMap["required"])
	}
	properties, ok := schemaMap["properties"].(map[string]any)
	if !ok {
		t.Fatal("properties 缺失")
	}
	args, ok := properties["args"].(map[string]any)
	if !ok {
		t.Fatal("args 缺失")
	}
	if args["additionalProperties"] != true {
		t.Fatal("args.additionalProperties 应为 true")
	}
}

// TestToolMetadataProvider_双语回退 验证未知语言回退中文。
func TestToolMetadataProvider_双语回退(t *testing.T) {
	if got := (&ToolSearchMetadataProvider{}).GetDescription("jp"); got != toolSearchDescription["cn"] {
		t.Fatalf("未知语言应回退 cn，got %s", got)
	}
	if got := (&ToolCallMetadataProvider{}).GetDescription("jp"); got != toolCallDescription["cn"] {
		t.Fatalf("未知语言应回退 cn，got %s", got)
	}
}
