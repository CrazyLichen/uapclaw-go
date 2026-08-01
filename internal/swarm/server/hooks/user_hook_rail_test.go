package hooks

import (
	"context"
	"fmt"
	"strings"
	"testing"

	hookscfg "github.com/uapclaw/uapclaw-go/internal/common/hooks"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
)

// TestNewUserHookRail 测试构造
func TestNewUserHookRail(t *testing.T) {
	cfg := hookscfg.HooksConfig{Events: map[string][]hookscfg.HookMatcher{
		hookscfg.HookEventPreToolUse: {{Matcher: "*"}},
	}}
	exec := NewHookExecutor(LLMConfig{})
	rail := NewUserHookRail(cfg, exec)
	if rail == nil {
		t.Error("NewUserHookRail() = nil, want non-nil")
	}
	// 验证 priority=60
	if rail.Priority() != 60 {
		t.Errorf("Priority() = %d, want 60", rail.Priority())
	}
}

// TestUserHookRail_BeforeToolCall_阻塞 测试 blocking 设置 _skip_tool + _hook_feedback
func TestUserHookRail_BeforeToolCall_阻塞(t *testing.T) {
	cfg := hookscfg.HooksConfig{Events: map[string][]hookscfg.HookMatcher{
		hookscfg.HookEventPreToolUse: {
			{
				Matcher: "*",
				Hooks:   []map[string]any{{"type": "command", "command": "echo '{\"decision\": \"block\", \"reason\": \"dangerous\"}' && exit 2", "timeout": 10}},
			},
		},
	}}
	exec := NewHookExecutor(LLMConfig{})
	rail := NewUserHookRail(cfg, exec)

	cbc := agentinterfaces.NewAgentCallbackContext(nil, &agentinterfaces.ToolCallInputs{ToolName: "dangerous_tool", ToolArgs: "{}"}, nil)
	err := rail.BeforeToolCall(context.Background(), cbc)
	if err != nil {
		t.Errorf("BeforeToolCall error: %v", err)
	}
	if cbc.Extra()["_skip_tool"] != true {
		t.Error("_skip_tool should be true after blocking")
	}
	if cbc.Extra()["_hook_feedback"] != "dangerous" {
		t.Errorf("_hook_feedback = %v, want %q", cbc.Extra()["_hook_feedback"], "dangerous")
	}
}

// TestUserHookRail_BeforeToolCall_无匹配 测试无匹配事件时直接返回 nil
func TestUserHookRail_BeforeToolCall_无匹配(t *testing.T) {
	cfg := hookscfg.HooksConfig{} // 空 Events
	exec := NewHookExecutor(LLMConfig{})
	rail := NewUserHookRail(cfg, exec)

	cbc := agentinterfaces.NewAgentCallbackContext(nil, &agentinterfaces.ToolCallInputs{ToolName: "any_tool", ToolArgs: "{}"}, nil)
	err := rail.BeforeToolCall(context.Background(), cbc)
	if err != nil {
		t.Errorf("BeforeToolCall with no hooks should return nil, got: %v", err)
	}
}

// TestUserHookRail_BeforeToolCall_修改输入 测试 modifiedInput 修改 ToolArgs
func TestUserHookRail_BeforeToolCall_修改输入(t *testing.T) {
	cfg := hookscfg.HooksConfig{Events: map[string][]hookscfg.HookMatcher{
		hookscfg.HookEventPreToolUse: {
			{
				Matcher: "*",
				Hooks:   []map[string]any{{"type": "command", "command": "echo '{\"decision\": \"allow\", \"modifiedInput\": {\"tool_args\": \"{\\\"path\\\": \\\"/safe\\\"}\"}}'", "timeout": 10}},
			},
		},
	}}
	exec := NewHookExecutor(LLMConfig{})
	rail := NewUserHookRail(cfg, exec)

	cbc := agentinterfaces.NewAgentCallbackContext(nil, &agentinterfaces.ToolCallInputs{ToolName: "test_tool", ToolArgs: "{}"}, nil)
	err := rail.BeforeToolCall(context.Background(), cbc)
	if err != nil {
		t.Errorf("BeforeToolCall error: %v", err)
	}
	inputs := cbc.Inputs().(*agentinterfaces.ToolCallInputs)
	if inputs.ToolArgs != `{"path": "/safe"}` {
		t.Errorf("ToolArgs = %q, want modified args", inputs.ToolArgs)
	}
}

// TestUserHookRail_BeforeToolCall_附加上下文 测试 additionalContext 追加到 Extra
func TestUserHookRail_BeforeToolCall_附加上下文(t *testing.T) {
	cfg := hookscfg.HooksConfig{Events: map[string][]hookscfg.HookMatcher{
		hookscfg.HookEventPreToolUse: {
			{
				Matcher: "*",
				Hooks:   []map[string]any{{"type": "command", "command": "echo '{\"decision\": \"allow\", \"additionalContext\": \"extra info\"}'", "timeout": 10}},
			},
		},
	}}
	exec := NewHookExecutor(LLMConfig{})
	rail := NewUserHookRail(cfg, exec)

	cbc := agentinterfaces.NewAgentCallbackContext(nil, &agentinterfaces.ToolCallInputs{ToolName: "test_tool", ToolArgs: "{}"}, nil)
	err := rail.BeforeToolCall(context.Background(), cbc)
	if err != nil {
		t.Errorf("BeforeToolCall error: %v", err)
	}
	if cbc.Extra()["_hook_additional_context"] != "extra info" {
		t.Errorf("_hook_additional_context = %v, want %q", cbc.Extra()["_hook_additional_context"], "extra info")
	}
}

// TestUserHookRail_AfterToolCall_阻塞 测试 blocking 设置 _post_tool_hook_feedback
func TestUserHookRail_AfterToolCall_阻塞(t *testing.T) {
	cfg := hookscfg.HooksConfig{Events: map[string][]hookscfg.HookMatcher{
		hookscfg.HookEventPostToolUse: {
			{
				Matcher: "*",
				Hooks:   []map[string]any{{"type": "command", "command": "echo '{\"decision\": \"block\", \"reason\": \"post blocked\"}' && exit 2", "timeout": 10}},
			},
		},
	}}
	exec := NewHookExecutor(LLMConfig{})
	rail := NewUserHookRail(cfg, exec)

	cbc := agentinterfaces.NewAgentCallbackContext(nil, &agentinterfaces.ToolCallInputs{ToolName: "test_tool", ToolArgs: "{}", ToolResult: "result"}, nil)
	err := rail.AfterToolCall(context.Background(), cbc)
	if err != nil {
		t.Errorf("AfterToolCall error: %v", err)
	}
	if cbc.Extra()["_post_tool_hook_feedback"] != "post blocked" {
		t.Errorf("_post_tool_hook_feedback = %v, want %q", cbc.Extra()["_post_tool_hook_feedback"], "post blocked")
	}
}

// TestUserHookRail_AfterToolCall_附加上下文 测试 additionalContext 拼接到 ToolResult
func TestUserHookRail_AfterToolCall_附加上下文(t *testing.T) {
	cfg := hookscfg.HooksConfig{Events: map[string][]hookscfg.HookMatcher{
		hookscfg.HookEventPostToolUse: {
			{
				Matcher: "*",
				Hooks:   []map[string]any{{"type": "command", "command": "echo '{\"decision\": \"allow\", \"additionalContext\": \"extra info\"}'", "timeout": 10}},
			},
		},
	}}
	exec := NewHookExecutor(LLMConfig{})
	rail := NewUserHookRail(cfg, exec)

	cbc := agentinterfaces.NewAgentCallbackContext(nil, &agentinterfaces.ToolCallInputs{ToolName: "test_tool", ToolArgs: "{}", ToolResult: "original result"}, nil)
	err := rail.AfterToolCall(context.Background(), cbc)
	if err != nil {
		t.Errorf("AfterToolCall error: %v", err)
	}
	inputs := cbc.Inputs().(*agentinterfaces.ToolCallInputs)
	resultStr := fmt.Sprintf("%v", inputs.ToolResult)
	if !strings.Contains(resultStr, "extra info") {
		t.Errorf("ToolResult should contain 'extra info', got: %q", resultStr)
	}
	if !strings.Contains(resultStr, "original result") {
		t.Errorf("ToolResult should still contain 'original result', got: %q", resultStr)
	}
}

// TestUserHookRail_OnToolException_无匹配 测试无匹配事件时返回 nil
func TestUserHookRail_OnToolException_无匹配(t *testing.T) {
	cfg := hookscfg.HooksConfig{} // 空 Events
	exec := NewHookExecutor(LLMConfig{})
	rail := NewUserHookRail(cfg, exec)

	cbc := agentinterfaces.NewAgentCallbackContext(nil, &agentinterfaces.ToolCallInputs{ToolName: "test_tool", ToolArgs: "{}"}, nil)
	err := rail.OnToolException(context.Background(), cbc)
	if err != nil {
		t.Errorf("OnToolException with no hooks should return nil, got: %v", err)
	}
}

// TestUserHookRail_OnToolException_有匹配 测试有匹配事件时执行 hook（仅通知，不改变流程）
func TestUserHookRail_OnToolException_有匹配(t *testing.T) {
	cfg := hookscfg.HooksConfig{Events: map[string][]hookscfg.HookMatcher{
		hookscfg.HookEventPostToolUseFailure: {
			{
				Matcher: "*",
				Hooks:   []map[string]any{{"type": "command", "command": "echo 'failure noted'", "timeout": 10}},
			},
		},
	}}
	exec := NewHookExecutor(LLMConfig{})
	rail := NewUserHookRail(cfg, exec)

	cbc := agentinterfaces.NewAgentCallbackContext(nil, &agentinterfaces.ToolCallInputs{ToolName: "test_tool", ToolArgs: "{}"}, nil)
	err := rail.OnToolException(context.Background(), cbc)
	if err != nil {
		t.Errorf("OnToolException error: %v", err)
	}
	// OnToolException 不修改 Extra 或 Inputs，对齐 Python: 仅通知收集
}

// TestUserHookRail_AfterInvoke_阻塞 测试 blocking 设置 _stop_hook_feedback
func TestUserHookRail_AfterInvoke_阻塞(t *testing.T) {
	cfg := hookscfg.HooksConfig{Events: map[string][]hookscfg.HookMatcher{
		hookscfg.HookEventStop: {
			{
				Matcher: "*",
				Hooks:   []map[string]any{{"type": "command", "command": "echo '{\"decision\": \"block\", \"reason\": \"stop blocked\"}' && exit 2", "timeout": 10}},
			},
		},
	}}
	exec := NewHookExecutor(LLMConfig{})
	rail := NewUserHookRail(cfg, exec)

	cbc := agentinterfaces.NewAgentCallbackContext(nil, &agentinterfaces.InvokeInputs{}, nil)
	err := rail.AfterInvoke(context.Background(), cbc)
	if err != nil {
		t.Errorf("AfterInvoke error: %v", err)
	}
	if cbc.Extra()["_stop_hook_feedback"] != "stop blocked" {
		t.Errorf("_stop_hook_feedback = %v, want %q", cbc.Extra()["_stop_hook_feedback"], "stop blocked")
	}
}

// TestUserHookRail_AfterInvoke_无匹配 测试无匹配事件时返回 nil
func TestUserHookRail_AfterInvoke_无匹配(t *testing.T) {
	cfg := hookscfg.HooksConfig{} // 空 Events
	exec := NewHookExecutor(LLMConfig{})
	rail := NewUserHookRail(cfg, exec)

	cbc := agentinterfaces.NewAgentCallbackContext(nil, &agentinterfaces.InvokeInputs{}, nil)
	err := rail.AfterInvoke(context.Background(), cbc)
	if err != nil {
		t.Errorf("AfterInvoke with no hooks should return nil, got: %v", err)
	}
}
