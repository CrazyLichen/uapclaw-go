package hooks

import (
	"context"
	"fmt"
	"strings"
	"testing"

	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	hookscfg "github.com/uapclaw/uapclaw-go/internal/common/hooks"
)

// mockSessionFacade 用于测试的 Session 桩实现，对齐项目其他测试中的 fakeSessionFacade 模式
type mockSessionFacade struct {
	sessionID string
}

func (m *mockSessionFacade) GetSessionID() string                             { return m.sessionID }
func (m *mockSessionFacade) UpdateState(_ map[string]any)                     {}
func (m *mockSessionFacade) GetState(_ state.StateKey) (any, error)           { return nil, nil }
func (m *mockSessionFacade) DumpState() map[string]any                        { return nil }
func (m *mockSessionFacade) WriteStream(_ context.Context, _ any) error       { return nil }
func (m *mockSessionFacade) WriteCustomStream(_ context.Context, _ any) error { return nil }
func (m *mockSessionFacade) GetEnv(_ string, _ ...any) any                    { return nil }
func (m *mockSessionFacade) Interact(_ context.Context, _ any) error          { return nil }

// 编译时接口检查
var _ sessioninterfaces.SessionFacade = (*mockSessionFacade)(nil)

// TestNewUserHookRail 测试构造
func TestNewUserHookRail(t *testing.T) {
	cfg := hookscfg.HooksConfig{Events: map[string][]hookscfg.HookMatcher{
		hookscfg.HookEventPreToolUse: {{Matcher: "*"}},
	}}
	exec := NewHookExecutor(LLMConfig{})
	rail := NewUserHookRail(cfg, exec)
	if rail == nil {
		t.Fatal("NewUserHookRail() = nil, want non-nil")
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

	cbc := agentinterfaces.NewAgentCallbackContext(nil, &agentinterfaces.ToolCallInputs{ToolName: "dangerous_tool", ToolArgs: map[string]any{}}, nil)
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

	cbc := agentinterfaces.NewAgentCallbackContext(nil, &agentinterfaces.ToolCallInputs{ToolName: "any_tool", ToolArgs: map[string]any{}}, nil)
	err := rail.BeforeToolCall(context.Background(), cbc)
	if err != nil {
		t.Errorf("BeforeToolCall with no hooks should return nil, got: %v", err)
	}
}

// TestUserHookRail_BeforeToolCall_修改输入 测试 modifiedInput 修改 ToolArgs
// 对齐 Python: ctx.inputs.tool_args = r.modified_input（整个 dict 赋值给 tool_args）
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

	cbc := agentinterfaces.NewAgentCallbackContext(nil, &agentinterfaces.ToolCallInputs{ToolName: "test_tool", ToolArgs: map[string]any{}}, nil)
	err := rail.BeforeToolCall(context.Background(), cbc)
	if err != nil {
		t.Errorf("BeforeToolCall error: %v", err)
	}
	inputs := cbc.Inputs().(*agentinterfaces.ToolCallInputs)
	// 对齐 Python: ctx.inputs.tool_args = r.modified_input — 整个 dict 赋值给 tool_args
	// ToolArgs 现在是 map[string]any，modifiedInput 的 dict 直接赋值
	expected := map[string]any{"tool_args": `{"path": "/safe"}`}
	if fmt.Sprintf("%v", inputs.ToolArgs) != fmt.Sprintf("%v", expected) {
		t.Errorf("ToolArgs = %v, want %v", inputs.ToolArgs, expected)
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

	cbc := agentinterfaces.NewAgentCallbackContext(nil, &agentinterfaces.ToolCallInputs{ToolName: "test_tool", ToolArgs: map[string]any{}}, nil)
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

	cbc := agentinterfaces.NewAgentCallbackContext(nil, &agentinterfaces.ToolCallInputs{ToolName: "test_tool", ToolArgs: map[string]any{}, ToolResult: "result"}, nil)
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

	cbc := agentinterfaces.NewAgentCallbackContext(nil, &agentinterfaces.ToolCallInputs{ToolName: "test_tool", ToolArgs: map[string]any{}, ToolResult: "original result"}, nil)
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

	cbc := agentinterfaces.NewAgentCallbackContext(nil, &agentinterfaces.ToolCallInputs{ToolName: "test_tool", ToolArgs: map[string]any{}}, nil)
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

	cbc := agentinterfaces.NewAgentCallbackContext(nil, &agentinterfaces.ToolCallInputs{ToolName: "test_tool", ToolArgs: map[string]any{}}, nil)
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

// TestUserHookRail_BeforeToolCall_带SessionID 测试 Session 存在时 session_id 传递到 hookInput
// 对齐 Python: hook_input={"event": ..., "session_id": getattr(ctx, "session_id", "")}
func TestUserHookRail_BeforeToolCall_带SessionID(t *testing.T) {
	cfg := hookscfg.HooksConfig{Events: map[string][]hookscfg.HookMatcher{
		hookscfg.HookEventPreToolUse: {
			{
				Matcher: "*",
				// command hook 输出 additionalContext 验证 hook 成功执行（session_id 已传递到 hookInput）
				Hooks: []map[string]any{{"type": "command", "command": `echo '{"decision": "allow", "additionalContext": "session_ok"}'`, "timeout": 10}},
			},
		},
	}}
	exec := NewHookExecutor(LLMConfig{})
	rail := NewUserHookRail(cfg, exec)

	sess := &mockSessionFacade{sessionID: "test-session-123"}
	cbc := agentinterfaces.NewAgentCallbackContext(nil, &agentinterfaces.ToolCallInputs{ToolName: "test_tool", ToolArgs: map[string]any{}}, sess)
	err := rail.BeforeToolCall(context.Background(), cbc)
	if err != nil {
		t.Errorf("BeforeToolCall error: %v", err)
	}
	// 验证 additionalContext 被设置（说明 hook 成功执行，session_id 已传递到 hookInput）
	if cbc.Extra()["_hook_additional_context"] != "session_ok" {
		t.Errorf("_hook_additional_context = %v, want %q", cbc.Extra()["_hook_additional_context"], "session_ok")
	}
}

// TestUserHookRail_BeforeToolCall_修改工具名 测试 modifiedInput 含 _tool_name 时修改 ToolName
// 对齐 Python: new_name = r.modified_input.get("_tool_name"); ctx.inputs.tool_name = new_name
func TestUserHookRail_BeforeToolCall_修改工具名(t *testing.T) {
	cfg := hookscfg.HooksConfig{Events: map[string][]hookscfg.HookMatcher{
		hookscfg.HookEventPreToolUse: {
			{
				Matcher: "*",
				Hooks: []map[string]any{{
					"type":    "command",
					"command": `echo '{"decision": "allow", "modifiedInput": {"_tool_name": "safe_tool", "path": "/safe"}}'`,
					"timeout": 10,
				}},
			},
		},
	}}
	exec := NewHookExecutor(LLMConfig{})
	rail := NewUserHookRail(cfg, exec)

	cbc := agentinterfaces.NewAgentCallbackContext(nil, &agentinterfaces.ToolCallInputs{ToolName: "dangerous_tool", ToolArgs: map[string]any{}}, nil)
	err := rail.BeforeToolCall(context.Background(), cbc)
	if err != nil {
		t.Errorf("BeforeToolCall error: %v", err)
	}
	inputs := cbc.Inputs().(*agentinterfaces.ToolCallInputs)
	if inputs.ToolName != "safe_tool" {
		t.Errorf("ToolName = %q, want %q", inputs.ToolName, "safe_tool")
	}
}

// TestUserHookRail_BeforeToolCall_多次附加上下文 测试多个 hook 返回 additionalContext 时换行拼接
// 对齐 Python: existing = ctx.extra.get("_hook_additional_context", "")
//              ctx.extra["_hook_additional_context"] = existing + "\n" + r.additional_context
func TestUserHookRail_BeforeToolCall_多次附加上下文(t *testing.T) {
	cfg := hookscfg.HooksConfig{Events: map[string][]hookscfg.HookMatcher{
		hookscfg.HookEventPreToolUse: {
			{
				Matcher: "*",
				Hooks: []map[string]any{
					{"type": "command", "command": `echo '{"decision": "allow", "additionalContext": "context1"}'`, "timeout": 10},
					{"type": "command", "command": `echo '{"decision": "allow", "additionalContext": "context2"}'`, "timeout": 10},
				},
			},
		},
	}}
	exec := NewHookExecutor(LLMConfig{})
	rail := NewUserHookRail(cfg, exec)

	cbc := agentinterfaces.NewAgentCallbackContext(nil, &agentinterfaces.ToolCallInputs{ToolName: "test_tool", ToolArgs: map[string]any{}}, nil)
	err := rail.BeforeToolCall(context.Background(), cbc)
	if err != nil {
		t.Errorf("BeforeToolCall error: %v", err)
	}
	got := cbc.Extra()["_hook_additional_context"]
	if got != "context1\ncontext2" {
		t.Errorf("_hook_additional_context = %q, want %q", got, "context1\ncontext2")
	}
}

// TestUserHookRail_AfterToolCall_非string结果 测试 ToolResult 为非 string 类型时 JSON 序列化保底
// 对齐 Python: current = ctx.inputs.tool_result or ""（Python 中 tool_result 通常是 string，
// Go 中 ToolResult 为 any 类型，非 string 时需 JSON 序列化保底）
func TestUserHookRail_AfterToolCall_非string结果(t *testing.T) {
	cfg := hookscfg.HooksConfig{Events: map[string][]hookscfg.HookMatcher{
		hookscfg.HookEventPostToolUse: {
			{
				Matcher: "*",
				Hooks: []map[string]any{{"type": "command", "command": `echo '{"decision": "allow", "additionalContext": "extra info"}'`, "timeout": 10}},
			},
		},
	}}
	exec := NewHookExecutor(LLMConfig{})
	rail := NewUserHookRail(cfg, exec)

	// ToolResult 为 map[string]any（非 string），触发 JSON 序列化保底路径
	cbc := agentinterfaces.NewAgentCallbackContext(nil, &agentinterfaces.ToolCallInputs{
		ToolName:   "test_tool",
		ToolArgs:   map[string]any{},
		ToolResult: map[string]any{"key": "value"},
	}, nil)
	err := rail.AfterToolCall(context.Background(), cbc)
	if err != nil {
		t.Errorf("AfterToolCall error: %v", err)
	}
	inputs := cbc.Inputs().(*agentinterfaces.ToolCallInputs)
	resultStr, ok := inputs.ToolResult.(string)
	if !ok {
		t.Fatalf("ToolResult should be string after hook, got %T", inputs.ToolResult)
	}
	if !strings.Contains(resultStr, `"key"`) {
		t.Errorf("ToolResult should contain JSON-serialized key, got: %q", resultStr)
	}
	if !strings.Contains(resultStr, "extra info") {
		t.Errorf("ToolResult should contain additionalContext 'extra info', got: %q", resultStr)
	}
}

// TestUserHookRail_AfterInvoke_超长reason截断 测试 reason 超过 200 字符时日志截断但 _stop_hook_feedback 存完整值
// 对齐 Python: ctx.extra["_stop_hook_feedback"] = r.error（存完整值）
//              logger.info("UserHookRail: Stop hook feedback: %s", r.error[:200])（日志截断）
func TestUserHookRail_AfterInvoke_超长reason截断(t *testing.T) {
	// 构造超长 reason（300 字符）
	longReason := strings.Repeat("x", 300)

	cfg := hookscfg.HooksConfig{Events: map[string][]hookscfg.HookMatcher{
		hookscfg.HookEventStop: {
			{
				Matcher: "*",
				// exit 2 + 超长 reason
				Hooks: []map[string]any{{
					"type":     "command",
					"command":  fmt.Sprintf(`echo '{"decision": "block", "reason": "%s"}' && exit 2`, longReason),
					"timeout":  10,
				}},
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
	feedback := cbc.Extra()["_stop_hook_feedback"]
	feedbackStr, ok := feedback.(string)
	if !ok {
		t.Fatalf("_stop_hook_feedback should be string, got %T", feedback)
	}
	// 对齐 Python: _stop_hook_feedback 存完整值（不截断），截断仅用于日志
	if len(feedbackStr) != len(longReason) {
		t.Errorf("_stop_hook_feedback length = %d, want %d (full reason, no truncation)", len(feedbackStr), len(longReason))
	}
}
