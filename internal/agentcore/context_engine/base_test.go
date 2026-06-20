package context_engine

import (
	"testing"

	llm_schema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestContextWindow_GetMessages_空窗口 测试空窗口的 GetMessages
func TestContextWindow_GetMessages_空窗口(t *testing.T) {
	w := &ContextWindow{}
	msgs := w.GetMessages()
	if len(msgs) != 0 {
		t.Errorf("空窗口应返回 0 条消息，实际 %d", len(msgs))
	}
}

// TestContextWindow_GetMessages_合并系统消息和上下文消息 测试消息合并
func TestContextWindow_GetMessages_合并系统消息和上下文消息(t *testing.T) {
	sysMsg := llm_schema.NewBaseMessage(llm_schema.RoleTypeSystem, "系统提示")
	userMsg := llm_schema.NewBaseMessage(llm_schema.RoleTypeUser, "用户输入")

	w := &ContextWindow{
		SystemMessages:  []*llm_schema.BaseMessage{sysMsg},
		ContextMessages: []*llm_schema.BaseMessage{userMsg},
	}

	msgs := w.GetMessages()
	if len(msgs) != 2 {
		t.Fatalf("应返回 2 条消息，实际 %d", len(msgs))
	}
	if msgs[0].Role != llm_schema.RoleTypeSystem {
		t.Errorf("第 1 条消息角色应为 system，实际 %v", msgs[0].Role)
	}
	if msgs[1].Role != llm_schema.RoleTypeUser {
		t.Errorf("第 2 条消息角色应为 user，实际 %v", msgs[1].Role)
	}
}

// TestContextWindow_GetMessages_仅系统消息 测试只有系统消息的情况
func TestContextWindow_GetMessages_仅系统消息(t *testing.T) {
	sysMsg := llm_schema.NewBaseMessage(llm_schema.RoleTypeSystem, "系统提示")

	w := &ContextWindow{
		SystemMessages: []*llm_schema.BaseMessage{sysMsg},
	}

	msgs := w.GetMessages()
	if len(msgs) != 1 {
		t.Fatalf("应返回 1 条消息，实际 %d", len(msgs))
	}
	if msgs[0].Role != llm_schema.RoleTypeSystem {
		t.Errorf("消息角色应为 system，实际 %v", msgs[0].Role)
	}
}

// TestContextWindow_GetTools_空工具 测试空工具列表
func TestContextWindow_GetTools_空工具(t *testing.T) {
	w := &ContextWindow{}
	tools := w.GetTools()
	if tools != nil {
		t.Errorf("空窗口应返回 nil 工具列表，实际 %v", tools)
	}
}

// TestContextWindow_GetTools_有工具 测试工具列表返回
func TestContextWindow_GetTools_有工具(t *testing.T) {
	toolInfo := &schema.ToolInfo{
		Type:        "function",
		Name:        "test_tool",
		Description: "测试工具",
	}

	w := &ContextWindow{
		Tools: []*schema.ToolInfo{toolInfo},
	}

	tools := w.GetTools()
	if len(tools) != 1 {
		t.Fatalf("应返回 1 个工具，实际 %d", len(tools))
	}
	if tools[0].Name != "test_tool" {
		t.Errorf("工具名称应为 test_tool，实际 %s", tools[0].Name)
	}
}

// TestContextStats_零值 测试 ContextStats 零值
func TestContextStats_零值(t *testing.T) {
	var stats ContextStats
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

// TestContextWindow_Statistic为Nil 测试 Statistic 为 nil 时 GetMessages 不受影响
func TestContextWindow_Statistic为Nil(t *testing.T) {
	w := &ContextWindow{
		SystemMessages:  []*llm_schema.BaseMessage{llm_schema.NewBaseMessage(llm_schema.RoleTypeSystem, "hi")},
		ContextMessages: []*llm_schema.BaseMessage{llm_schema.NewBaseMessage(llm_schema.RoleTypeUser, "hello")},
	}

	if w.Statistic != nil {
		t.Error("Statistic 应为 nil")
	}
	msgs := w.GetMessages()
	if len(msgs) != 2 {
		t.Errorf("即使 Statistic 为 nil，GetMessages 也应返回 2 条消息，实际 %d", len(msgs))
	}
}
