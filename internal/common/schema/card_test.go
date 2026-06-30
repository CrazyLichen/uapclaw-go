package schema

import (
	"encoding/json"
	"strings"
	"testing"
)

// ──────────────────────────── 结构体 ────────────────────────────

// testSubCard 测试用子类，验证 BaseCard 结构体嵌入和 ToolInfo() 覆写。
type testSubCard struct {
	BaseCard
	InputParams map[string]any
}

// ToolInfo 覆写 BaseCard.ToolInfo()，模拟 ToolCard/AgentCard 的行为。
func (c *testSubCard) ToolInfo() *ToolInfo {
	return NewToolInfo(c.Name, c.Description, c.InputParams)
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestBaseCard_满足CardInterface 验证 *BaseCard 满足 CardInterface。
func TestBaseCard_满足CardInterface(t *testing.T) {
	card := NewBaseCard(WithID("test-id"), WithName("test-name"))
	var iface CardInterface = card
	if iface.GetID() != "test-id" {
		t.Errorf("GetID() = %q, want %q", iface.GetID(), "test-id")
	}
	if iface.GetName() != "test-name" {
		t.Errorf("GetName() = %q, want %q", iface.GetName(), "test-name")
	}
}

// TestWorkflowCard_满足CardInterface 验证 *WorkflowCard 满足 CardInterface。
func TestWorkflowCard_满足CardInterface(t *testing.T) {
	card := NewWorkflowCard(WithID("wf-1"), WithName("wf-name"))
	var iface CardInterface = card
	if iface.GetID() != "wf-1" {
		t.Errorf("GetID() = %q, want %q", iface.GetID(), "wf-1")
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

func TestNewBaseCard_默认ID(t *testing.T) {
	card := NewBaseCard()
	// ID 应为 32 位 hex（无连字符）
	if len(card.ID) != 32 {
		t.Errorf("期望 ID 长度 32，实际 %d: %s", len(card.ID), card.ID)
	}
	// ID 应全部为 hex 字符
	for _, c := range card.ID {
		if !isHexChar(c) {
			t.Errorf("ID 包含非 hex 字符: %c", c)
		}
	}
	// Name 和 Description 应为空字符串
	if card.Name != "" {
		t.Errorf("期望 Name 为空，实际 %q", card.Name)
	}
	if card.Description != "" {
		t.Errorf("期望 Description 为空，实际 %q", card.Description)
	}
}

func TestNewBaseCard_带选项(t *testing.T) {
	card := NewBaseCard(
		WithName("test-agent"),
		WithDescription("测试 Agent"),
	)
	if card.Name != "test-agent" {
		t.Errorf("期望 Name %q，实际 %q", "test-agent", card.Name)
	}
	if card.Description != "测试 Agent" {
		t.Errorf("期望 Description %q，实际 %q", "测试 Agent", card.Description)
	}
	// ID 应自动生成
	if card.ID == "" {
		t.Error("期望 ID 自动生成，实际为空")
	}
}

func TestNewBaseCard_带ID(t *testing.T) {
	card := NewBaseCard(WithID("custom-id-123"))
	if card.ID != "custom-id-123" {
		t.Errorf("期望 ID %q，实际 %q", "custom-id-123", card.ID)
	}
}

func TestBaseCard_工具信息(t *testing.T) {
	card := NewBaseCard(
		WithName("search"),
		WithDescription("搜索工具"),
	)
	// BaseCard.ToolInfo() 返回 nil，子类应覆写此方法
	info := card.ToolInfo()
	if info != nil {
		t.Errorf("BaseCard.ToolInfo() 应返回 nil，实际 %v", info)
	}
}

func TestBaseCard_字符串表示(t *testing.T) {
	card := NewBaseCard(WithName("my-tool"))
	s := card.String()
	if !strings.Contains(s, "id=") {
		t.Errorf("String() 缺少 id=: %s", s)
	}
	if !strings.Contains(s, "name=my-tool") {
		t.Errorf("String() 缺少 name=my-tool: %s", s)
	}
}

func TestBaseCard_嵌入(t *testing.T) {
	// 验证子类嵌入 BaseCard 后可正常访问字段和覆写方法
	sub := &testSubCard{
		BaseCard: BaseCard{
			ID:          "test-id",
			Name:        "sub-card",
			Description: "子类名片",
		},
		InputParams: map[string]any{"key": "value"},
	}

	// 嵌入字段可直接访问
	if sub.Name != "sub-card" {
		t.Errorf("嵌入后 Name 期望 %q，实际 %q", "sub-card", sub.Name)
	}

	// 覆写 ToolInfo() 应返回子类版本
	info := sub.ToolInfo()
	if info.Name != "sub-card" {
		t.Errorf("覆写 ToolInfo Name 期望 %q，实际 %q", "sub-card", info.Name)
	}
	if info.Parameters["key"] != "value" {
		t.Errorf("覆写 ToolInfo Parameters 期望包含 key=value，实际 %v", info.Parameters)
	}
}

func TestNewBaseCard_ID唯一性(t *testing.T) {
	ids := make(map[string]bool, 100)
	for i := 0; i < 100; i++ {
		card := NewBaseCard()
		if ids[card.ID] {
			t.Errorf("重复 ID: %s", card.ID)
		}
		ids[card.ID] = true
	}
}

func TestBaseCard_JSON序列化(t *testing.T) {
	card := NewBaseCard(
		WithName("json-test"),
		WithDescription("JSON 序列化测试"),
	)
	data, err := json.Marshal(card)
	if err != nil {
		t.Fatalf("JSON 序列化失败: %v", err)
	}

	var decoded BaseCard
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("JSON 反序列化失败: %v", err)
	}
	if decoded.Name != card.Name {
		t.Errorf("反序列化 Name 期望 %q，实际 %q", card.Name, decoded.Name)
	}
	if decoded.ID != card.ID {
		t.Errorf("反序列化 ID 期望 %q，实际 %q", card.ID, decoded.ID)
	}
}

// isHexChar 判断字符是否为 hex 字符。
func isHexChar(c rune) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

func TestWorkflowCard_ToolInfo_有参数(t *testing.T) {
	card := NewWorkflowCard(
		WithName("my_workflow"),
		WithDescription("我的工作流"),
	)
	card.Version = "1.0"
	card.InputParams = map[string]any{
		"type":       "object",
		"properties": map[string]any{"query": map[string]any{"type": "string"}},
	}
	info := card.ToolInfo()
	if info.Name != "my_workflow" {
		t.Errorf("Name = %q, want my_workflow", info.Name)
	}
	if info.Description != "我的工作流" {
		t.Errorf("Description = %q, want 我的工作流", info.Description)
	}
	if card.Version != "1.0" {
		t.Errorf("Version = %q, want 1.0", card.Version)
	}
}

func TestWorkflowCard_ToolInfo_无参数(t *testing.T) {
	card := NewWorkflowCard(WithName("empty_wf"))
	info := card.ToolInfo()
	if info.Name != "empty_wf" {
		t.Errorf("Name = %q, want empty_wf", info.Name)
	}
	props, ok := info.Parameters["properties"]
	if ok {
		// properties 存在时应为空 map
		if m, ok2 := props.(map[string]any); ok2 && len(m) != 0 {
			t.Errorf("无参数时 properties 应为空，实际 %v", m)
		}
	}
	// 无参数时 InputParams 为 nil，ToolInfo 用 make(map[string]any)
	// 所以 Parameters 是空 map，不含 properties 键也正常
}

func TestWorkflowCard_能力(t *testing.T) {
	card := NewWorkflowCard(WithName("wf"), WithDescription("工作流"))
	if card.AbilityName() != "wf" {
		t.Errorf("AbilityName = %q, want wf", card.AbilityName())
	}
	if card.AbilityKind() != AbilityKindWorkflow {
		t.Errorf("AbilityKind = %v, want AbilityKindWorkflow", card.AbilityKind())
	}
	var _ Ability = card // 编译期接口检查
}

// TestWorkflowCard_AbilityID 验证 WorkflowCard.AbilityID() 返回 card.ID。
func TestWorkflowCard_AbilityID(t *testing.T) {
	card := NewWorkflowCard(WithName("wf"))
	if got := card.AbilityID(); got != card.ID {
		t.Errorf("AbilityID() = %q, want %q", got, card.ID)
	}
}

// TestBaseCard_GoString 验证 GoString 输出格式。
func TestBaseCard_GoString(t *testing.T) {
	card := NewBaseCard(WithName("test"), WithDescription("描述"))
	got := card.GoString()
	if !strings.Contains(got, "BaseCard{") {
		t.Errorf("GoString() 缺少 BaseCard{: %s", got)
	}
	if !strings.Contains(got, `Name:"test"`) {
		t.Errorf("GoString() 缺少 Name 字段: %s", got)
	}
	if !strings.Contains(got, `Description:"描述"`) {
		t.Errorf("GoString() 缺少 Description 字段: %s", got)
	}
}

// TestAbilityKind_String 验证 AbilityKind.String() 各枚举值。
func TestAbilityKind_String(t *testing.T) {
	tests := []struct {
		kind AbilityKind
		want string
	}{
		{AbilityKindTool, "tool"},
		{AbilityKindWorkflow, "workflow"},
		{AbilityKindAgent, "agent"},
		{AbilityKindMcpServer, "mcp_server"},
	}
	for _, tt := range tests {
		if got := tt.kind.String(); got != tt.want {
			t.Errorf("AbilityKind(%d).String() = %q, want %q", tt.kind, got, tt.want)
		}
	}
}

// TestBaseCard_GetID 验证 GetID() 返回 ID 字段。
func TestBaseCard_GetID(t *testing.T) {
	card := NewBaseCard(WithID("test-id"))
	if got := card.GetID(); got != "test-id" {
		t.Errorf("GetID() = %q, want %q", got, "test-id")
	}
}

// TestBaseCard_GetName 验证 GetName() 返回 Name 字段。
func TestBaseCard_GetName(t *testing.T) {
	card := NewBaseCard(WithName("test-name"))
	if got := card.GetName(); got != "test-name" {
		t.Errorf("GetName() = %q, want %q", got, "test-name")
	}
}

// TestBaseCard_GetDescription 验证 GetDescription() 返回 Description 字段。
func TestBaseCard_GetDescription(t *testing.T) {
	card := NewBaseCard(WithDescription("测试描述"))
	if got := card.GetDescription(); got != "测试描述" {
		t.Errorf("GetDescription() = %q, want %q", got, "测试描述")
	}
}
