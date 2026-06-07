package single_agent

import (
	"testing"

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
