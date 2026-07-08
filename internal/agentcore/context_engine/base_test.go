package context_engine

import (
	"encoding/json"
	"testing"

	iface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	llm_schema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// mockTokenCounter 用于测试的模拟 Token 计数器
type mockTokenCounter struct{}

func (m *mockTokenCounter) Count(_ string, _ string) (int, error) { return 0, nil }
func (m *mockTokenCounter) CountMessages(_ []llm_schema.BaseMessage, _ string) (int, error) {
	return 0, nil
}
func (m *mockTokenCounter) CountTools(_ []schema.ToolInfoInterface, _ string) (int, error) {
	return 0, nil
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestContextWindow_GetMessages_空窗口 测试空窗口的 GetMessages
func TestContextWindow_GetMessages_空窗口(t *testing.T) {
	w := iface.NewContextWindow()
	msgs := w.GetMessages()
	if len(msgs) != 0 {
		t.Errorf("空窗口应返回 0 条消息，实际 %d", len(msgs))
	}
}

// TestContextWindow_GetMessages_合并系统消息和上下文消息 测试消息合并
func TestContextWindow_GetMessages_合并系统消息和上下文消息(t *testing.T) {
	sysMsg := llm_schema.NewDefaultMessage(llm_schema.RoleTypeSystem, "系统提示")
	userMsg := llm_schema.NewDefaultMessage(llm_schema.RoleTypeUser, "用户输入")

	w := &iface.ContextWindow{
		SystemMessages:  []llm_schema.BaseMessage{sysMsg},
		ContextMessages: []llm_schema.BaseMessage{userMsg},
	}

	msgs := w.GetMessages()
	if len(msgs) != 2 {
		t.Fatalf("应返回 2 条消息，实际 %d", len(msgs))
	}
	if msgs[0].GetRole() != llm_schema.RoleTypeSystem {
		t.Errorf("第 1 条消息角色应为 system，实际 %v", msgs[0].GetRole())
	}
	if msgs[1].GetRole() != llm_schema.RoleTypeUser {
		t.Errorf("第 2 条消息角色应为 user，实际 %v", msgs[1].GetRole())
	}
}

// TestContextWindow_GetMessages_仅系统消息 测试只有系统消息的情况
func TestContextWindow_GetMessages_仅系统消息(t *testing.T) {
	sysMsg := llm_schema.NewDefaultMessage(llm_schema.RoleTypeSystem, "系统提示")

	w := &iface.ContextWindow{
		SystemMessages: []llm_schema.BaseMessage{sysMsg},
	}

	msgs := w.GetMessages()
	if len(msgs) != 1 {
		t.Fatalf("应返回 1 条消息，实际 %d", len(msgs))
	}
	if msgs[0].GetRole() != llm_schema.RoleTypeSystem {
		t.Errorf("消息角色应为 system，实际 %v", msgs[0].GetRole())
	}
}

// TestContextWindow_GetTools_空工具 测试空工具列表
func TestContextWindow_GetTools_空工具(t *testing.T) {
	w := iface.NewContextWindow()
	tools := w.GetTools()
	if len(tools) != 0 {
		t.Errorf("空窗口应返回 0 个工具，实际 %d", len(tools))
	}
}

// TestContextWindow_GetTools_有工具 测试工具列表返回
func TestContextWindow_GetTools_有工具(t *testing.T) {
	toolInfo := &schema.ToolInfo{
		Type:        "function",
		Name:        "test_tool",
		Description: "测试工具",
	}

	w := &iface.ContextWindow{
		Tools: []schema.ToolInfoInterface{toolInfo},
	}

	tools := w.GetTools()
	if len(tools) != 1 {
		t.Fatalf("应返回 1 个工具，实际 %d", len(tools))
	}
	if tools[0].GetName() != "test_tool" {
		t.Errorf("工具名称应为 test_tool，实际 %s", tools[0].GetName())
	}
}

// TestContextStats_零值 测试 ContextStats 零值
func TestContextStats_零值(t *testing.T) {
	var stats iface.ContextStats
	if stats.TotalMessages != 0 {
		t.Errorf("零值 TotalMessages 应为 0，实际 %d", stats.TotalMessages)
	}
	if stats.TotalTokens != 0 {
		t.Errorf("零值 TotalTokens 应为 0，实际 %d", stats.TotalTokens)
	}
	if stats.TotalDialogues != 0 {
		t.Errorf("零值 TotalDialogues 应为 0，实际 %d", stats.TotalDialogues)
	}
}

// TestContextStats_字段完整性 测试 ContextStats 所有 13 个字段零值
func TestContextStats_字段完整性(t *testing.T) {
	var stats iface.ContextStats
	// 消息计数字段
	if stats.TotalMessages != 0 {
		t.Errorf("TotalMessages 应为 0，实际 %d", stats.TotalMessages)
	}
	if stats.SystemMessages != 0 {
		t.Errorf("SystemMessages 应为 0，实际 %d", stats.SystemMessages)
	}
	if stats.UserMessages != 0 {
		t.Errorf("UserMessages 应为 0，实际 %d", stats.UserMessages)
	}
	if stats.AssistantMessages != 0 {
		t.Errorf("AssistantMessages 应为 0，实际 %d", stats.AssistantMessages)
	}
	if stats.ToolMessages != 0 {
		t.Errorf("ToolMessages 应为 0，实际 %d", stats.ToolMessages)
	}
	if stats.Tools != 0 {
		t.Errorf("Tools 应为 0，实际 %d", stats.Tools)
	}
	// Token 计数字段
	if stats.TotalTokens != 0 {
		t.Errorf("TotalTokens 应为 0，实际 %d", stats.TotalTokens)
	}
	if stats.SystemMessageTokens != 0 {
		t.Errorf("SystemMessageTokens 应为 0，实际 %d", stats.SystemMessageTokens)
	}
	if stats.UserMessageTokens != 0 {
		t.Errorf("UserMessageTokens 应为 0，实际 %d", stats.UserMessageTokens)
	}
	if stats.AssistantMessageTokens != 0 {
		t.Errorf("AssistantMessageTokens 应为 0，实际 %d", stats.AssistantMessageTokens)
	}
	if stats.ToolMessageTokens != 0 {
		t.Errorf("ToolMessageTokens 应为 0，实际 %d", stats.ToolMessageTokens)
	}
	if stats.ToolTokens != 0 {
		t.Errorf("ToolTokens 应为 0，实际 %d", stats.ToolTokens)
	}
	// 对话轮次
	if stats.TotalDialogues != 0 {
		t.Errorf("TotalDialogues 应为 0，实际 %d", stats.TotalDialogues)
	}
}

// TestContextWindow_Statistic零值 测试 Statistic 为值类型零值时可直接访问
func TestContextWindow_Statistic零值(t *testing.T) {
	w := &iface.ContextWindow{
		SystemMessages:  []llm_schema.BaseMessage{llm_schema.NewDefaultMessage(llm_schema.RoleTypeSystem, "hi")},
		ContextMessages: []llm_schema.BaseMessage{llm_schema.NewDefaultMessage(llm_schema.RoleTypeUser, "hello")},
	}

	// Statistic 是值类型，零值始终可访问，无需 nil 检查
	if w.Statistic.TotalMessages != 0 {
		t.Errorf("Statistic.TotalMessages 零值应为 0，实际 %d", w.Statistic.TotalMessages)
	}
	if w.Statistic.TotalTokens != 0 {
		t.Errorf("Statistic.TotalTokens 零值应为 0，实际 %d", w.Statistic.TotalTokens)
	}
	msgs := w.GetMessages()
	if len(msgs) != 2 {
		t.Errorf("GetMessages 应返回 2 条消息，实际 %d", len(msgs))
	}
}

// TestNewContextWindow 测试 NewContextWindow 构造函数
func TestNewContextWindow(t *testing.T) {
	w := iface.NewContextWindow()

	// 消息和工具切片应初始化为空切片（非 nil），避免 JSON 序列化为 null
	if w.SystemMessages == nil {
		t.Error("SystemMessages 应为空切片，不应为 nil")
	}
	if len(w.SystemMessages) != 0 {
		t.Errorf("SystemMessages 长度应为 0，实际 %d", len(w.SystemMessages))
	}
	if w.ContextMessages == nil {
		t.Error("ContextMessages 应为空切片，不应为 nil")
	}
	if len(w.ContextMessages) != 0 {
		t.Errorf("ContextMessages 长度应为 0，实际 %d", len(w.ContextMessages))
	}
	if w.Tools == nil {
		t.Error("Tools 应为空切片，不应为 nil")
	}
	if len(w.Tools) != 0 {
		t.Errorf("Tools 长度应为 0，实际 %d", len(w.Tools))
	}

	// Statistic 应为零值 ContextStats
	if w.Statistic.TotalMessages != 0 {
		t.Errorf("Statistic.TotalMessages 应为 0，实际 %d", w.Statistic.TotalMessages)
	}
	if w.Statistic.TotalTokens != 0 {
		t.Errorf("Statistic.TotalTokens 应为 0，实际 %d", w.Statistic.TotalTokens)
	}
}

// TestStatContextWindow_空窗口 验证空窗口统计。
func TestStatContextWindow_空窗口(t *testing.T) {
	w := iface.NewContextWindow()
	counter := &mockTokenCounter{}
	StatContextWindow(w, counter)
	if w.Statistic.TotalMessages != 0 {
		t.Errorf("TotalMessages = %d, want 0", w.Statistic.TotalMessages)
	}
	if w.Statistic.TotalDialogues != 0 {
		t.Errorf("TotalDialogues = %d, want 0", w.Statistic.TotalDialogues)
	}
}

// TestStatContextWindow_有消息 验证有消息时的统计。
func TestStatContextWindow_有消息(t *testing.T) {
	w := &iface.ContextWindow{
		SystemMessages:  []llm_schema.BaseMessage{llm_schema.NewDefaultMessage(llm_schema.RoleTypeSystem, "系统提示")},
		ContextMessages: []llm_schema.BaseMessage{llm_schema.NewDefaultMessage(llm_schema.RoleTypeUser, "你好")},
	}
	counter := &mockTokenCounter{}
	StatContextWindow(w, counter)
	if w.Statistic.TotalMessages != 2 {
		t.Errorf("TotalMessages = %d, want 2", w.Statistic.TotalMessages)
	}
	if w.Statistic.SystemMessages != 1 {
		t.Errorf("SystemMessages = %d, want 1", w.Statistic.SystemMessages)
	}
	if w.Statistic.UserMessages != 1 {
		t.Errorf("UserMessages = %d, want 1", w.Statistic.UserMessages)
	}
}
func TestNewContextWindow_JSON序列化(t *testing.T) {
	w := iface.NewContextWindow()

	data, err := json.Marshal(w)
	if err != nil {
		t.Fatalf("JSON 序列化失败: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("JSON 反序列化失败: %v", err)
	}

	// statistic 应为对象而非 null（与 Python ContextStats() 默认实例对齐）
	stat, ok := parsed["statistic"]
	if !ok {
		t.Error("JSON 中应包含 statistic 字段")
	}
	statMap, ok := stat.(map[string]any)
	if !ok {
		t.Errorf("statistic 应为对象，实际类型 %T", stat)
	}
	if statMap["total_messages"] != float64(0) {
		t.Errorf("statistic.total_messages 应为 0，实际 %v", statMap["total_messages"])
	}

	// system_messages 应为空数组而非 null
	if sysMsgs, ok := parsed["system_messages"]; ok {
		arr, ok := sysMsgs.([]any)
		if !ok || len(arr) != 0 {
			t.Errorf("system_messages 应为空数组，实际 %v", sysMsgs)
		}
	}
}
