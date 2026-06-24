package ability

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 导出函数 ────────────────────────────

func TestNewAbilityExecutionError(t *testing.T) {
	err := NewAbilityExecutionError(
		exception.StatusAbilityExecutionError,
		"call_123",
		"工具执行失败",
	)
	if err.ToolMessage == nil {
		t.Fatal("ToolMessage 不应为 nil")
	}
	if err.ToolMessage.ToolCallID != "call_123" {
		t.Errorf("ToolCallID = %q, want call_123", err.ToolMessage.ToolCallID)
	}
	if err.ToolMessage.Content.Text() != "工具执行失败" {
		t.Errorf("Content = %q, want 工具执行失败", err.ToolMessage.Content.Text())
	}
	if err.Code() != exception.StatusAbilityExecutionError.Code() {
		t.Errorf("Code = %d, want %d", err.Code(), exception.StatusAbilityExecutionError.Code())
	}
}

func TestBuildToolMessageContent_有DataContent(t *testing.T) {
	result := map[string]any{
		"data": map[string]any{
			"content": "搜索结果",
		},
	}
	content := BuildToolMessageContent(result)
	if content != "搜索结果" {
		t.Errorf("content = %q, want 搜索结果", content)
	}
}

func TestBuildToolMessageContent_失败有Error(t *testing.T) {
	result := map[string]any{
		"success": false,
		"error":   "超时",
	}
	content := BuildToolMessageContent(result)
	if content != "超时" {
		t.Errorf("content = %q, want 超时", content)
	}
}

func TestBuildToolMessageContent_其他(t *testing.T) {
	content := BuildToolMessageContent("简单字符串")
	if content != "简单字符串" {
		t.Errorf("content = %q, want 简单字符串", content)
	}
}

func TestBuildToolMessageContent_result解包(t *testing.T) {
	// structToMap 包装的 {"result": "search..."} 应解包为 "search..."
	result := map[string]any{"result": "search results..."}
	got := BuildToolMessageContent(result)
	if got != "search results..." {
		t.Errorf("content = %q, want search results...", got)
	}
}

func TestBuildToolMessageContent_result解包多层(t *testing.T) {
	// result 内再包一层 {"result": {"result": "deep"}}
	result := map[string]any{"result": map[string]any{"result": "deep"}}
	got := BuildToolMessageContent(result)
	if got != "deep" {
		t.Errorf("content = %q, want deep", got)
	}
}

func TestBuildToolMessageContent_普通map走JSON序列化(t *testing.T) {
	result := map[string]any{"message": "created", "count": float64(2)}
	got := BuildToolMessageContent(result)
	// JSON 序列化 key 按字典序排列
	want := `{"count":2,"message":"created"}`
	if got != want {
		t.Errorf("content = %q, want %q", got, want)
	}
}

func TestBuildToolMessageContent_反射提取DataContent(t *testing.T) {
	type toolOutput struct {
		Data    map[string]any
		Success bool
		Error   string
	}
	result := toolOutput{
		Data:    map[string]any{"content": "hello"},
		Success: true,
	}
	got := BuildToolMessageContent(result)
	if got != "hello" {
		t.Errorf("content = %q, want hello", got)
	}
}

func TestBuildToolMessageContent_反射提取Error(t *testing.T) {
	type toolOutput struct {
		Data    any
		Success bool
		Error   string
	}
	result := toolOutput{
		Data:    nil,
		Success: false,
		Error:   "timeout",
	}
	got := BuildToolMessageContent(result)
	if got != "timeout" {
		t.Errorf("content = %q, want timeout", got)
	}
}

func TestBuildToolMessageContent_反射指针类型(t *testing.T) {
	type toolOutput struct {
		Data    map[string]any
		Success bool
		Error   string
	}
	result := &toolOutput{
		Data:    map[string]any{"content": "ptr hello"},
		Success: true,
	}
	got := BuildToolMessageContent(result)
	if got != "ptr hello" {
		t.Errorf("content = %q, want ptr hello", got)
	}
}

func TestAddAbilityResult(t *testing.T) {
	r := AddAbilityResult{Name: "test", Added: true, Reason: "added_tool"}
	if r.Name != "test" {
		t.Errorf("Name = %q, want test", r.Name)
	}
	if !r.Added {
		t.Error("Added 应为 true")
	}
	if r.Reason != "added_tool" {
		t.Errorf("Reason = %q, want added_tool", r.Reason)
	}
}

func TestInterruptAutoConfirmKey(t *testing.T) {
	// 验证常量值对齐 Python: INTERRUPT_AUTO_CONFIRM_KEY = "__interrupt_auto_confirm__"
	if InterruptAutoConfirmKey != state.StringKey("__interrupt_auto_confirm__") {
		t.Errorf("InterruptAutoConfirmKey = %v, want __interrupt_auto_confirm__", InterruptAutoConfirmKey)
	}
}
