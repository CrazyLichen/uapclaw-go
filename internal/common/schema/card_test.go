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

// ──────────────────────────── 非导出函数 ────────────────────────────

func TestNewBaseCard_DefaultID(t *testing.T) {
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

func TestNewBaseCard_WithOptions(t *testing.T) {
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

func TestNewBaseCard_WithID(t *testing.T) {
	card := NewBaseCard(WithID("custom-id-123"))
	if card.ID != "custom-id-123" {
		t.Errorf("期望 ID %q，实际 %q", "custom-id-123", card.ID)
	}
}

func TestBaseCard_ToolInfo(t *testing.T) {
	card := NewBaseCard(
		WithName("search"),
		WithDescription("搜索工具"),
	)
	info := card.ToolInfo()
	if info.Type != "function" {
		t.Errorf("期望 Type %q，实际 %q", "function", info.Type)
	}
	if info.Name != "search" {
		t.Errorf("期望 Name %q，实际 %q", "search", info.Name)
	}
	if info.Description != "搜索工具" {
		t.Errorf("期望 Description %q，实际 %q", "搜索工具", info.Description)
	}
	// 无参数时 Parameters 应为空 map 而非 nil
	if len(info.Parameters) != 0 {
		t.Errorf("期望 Parameters 为空 map，实际 %v", info.Parameters)
	}
}

func TestBaseCard_String(t *testing.T) {
	card := NewBaseCard(WithName("my-tool"))
	s := card.String()
	if !strings.Contains(s, "id=") {
		t.Errorf("String() 缺少 id=: %s", s)
	}
	if !strings.Contains(s, "name=my-tool") {
		t.Errorf("String() 缺少 name=my-tool: %s", s)
	}
}

func TestBaseCard_Embedding(t *testing.T) {
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

func TestNewBaseCard_IDUniqueness(t *testing.T) {
	ids := make(map[string]bool, 100)
	for i := 0; i < 100; i++ {
		card := NewBaseCard()
		if ids[card.ID] {
			t.Errorf("重复 ID: %s", card.ID)
		}
		ids[card.ID] = true
	}
}

func TestBaseCard_JSONSerialization(t *testing.T) {
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
