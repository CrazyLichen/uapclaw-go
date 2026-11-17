package externalcli

import (
	"strings"
	"testing"
)

func strPtr(s string) *string { return &s }

func TestTruncateActivityContent(t *testing.T) {
	if got := TruncateActivityContent(nil); got != nil {
		t.Fatalf("nil 应透传: %v", got)
	}
	short := "ok"
	if got := TruncateActivityContent(&short); *got != "ok" {
		t.Fatalf("限内透传: %v", *got)
	}
	long := strings.Repeat("a", ActivityContentLimit+10)
	got := TruncateActivityContent(&long)
	if len([]rune(*got)) != ActivityContentLimit+len([]rune(ActivityTruncatedMarker)) {
		t.Fatalf("截断长度不符: %d", len([]rune(*got)))
	}
	if !strings.HasSuffix(*got, ActivityTruncatedMarker) {
		t.Fatalf("截断须带省略标记")
	}
}

func TestBuildActivityEvent(t *testing.T) {
	ev := BuildActivityEvent("tool_call", "id-1", strPtr("bash"), strPtr("ls -la"),
		"agent-1", "exec", "执行命令", "corr-9")
	if ev.Kind != "agent_activity" || ev.ActivityType != "tool_call" ||
		ev.ToolCallID != "id-1" || ev.ToolName == nil || *ev.ToolName != "bash" ||
		ev.Content == nil || *ev.Content != "ls -la" || ev.Message != "tool: bash" {
		t.Fatalf("字段不符: %+v", ev)
	}
	if ev.AgentID != "agent-1" || ev.Phase != "exec" || ev.Label != "执行命令" || ev.CorrelationID != "corr-9" {
		t.Fatalf("身份四元组不符: %+v", ev)
	}
	// 长内容截断生效（引擎侧唯一上限）
	long := strings.Repeat("b", ActivityContentLimit+1)
	ev2 := BuildActivityEvent("tool_result", "id-2", strPtr("bash"), &long, "", "", "", "")
	if len([]rune(*ev2.Content)) != ActivityContentLimit+len([]rune(ActivityTruncatedMarker)) {
		t.Fatalf("content 未截断")
	}
	// toolName 为 nil 时 message 仅前缀
	ev3 := BuildActivityEvent("tool_call", "id-3", nil, nil, "", "", "", "")
	if ev3.Message != "tool: " {
		t.Fatalf("nil 工具名 message 不符: %q", ev3.Message)
	}
}
