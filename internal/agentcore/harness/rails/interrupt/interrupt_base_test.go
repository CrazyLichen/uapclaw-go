package interrupt

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	interruptpkg "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interrupt"
	saschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// ──────────────────────────── 构造函数 ────────────────────────────

// TestNewBaseInterruptRail 验证构造函数、默认优先级和工具名注册
func TestNewBaseInterruptRail(t *testing.T) {
	r := NewBaseInterruptRail("ask_user", "write_file")
	assert.Equal(t, 90, r.Priority())
	assert.Contains(t, r.toolNames, "ask_user")
	assert.Contains(t, r.toolNames, "write_file")
	assert.Len(t, r.toolNames, 2)
}

// TestNewBaseInterruptRail_无工具名 验证空工具名列表
func TestNewBaseInterruptRail_无工具名(t *testing.T) {
	r := NewBaseInterruptRail()
	assert.Equal(t, 90, r.Priority())
	assert.Empty(t, r.toolNames)
}

// ──────────────────────────── AddTool/AddTools/GetTools 工具增删查 ────────────────────────────

// TestBaseInterruptRail_AddTool 验证单个工具名注册
func TestBaseInterruptRail_AddTool(t *testing.T) {
	r := NewBaseInterruptRail("ask_user")
	r.AddTool("bash")
	assert.Contains(t, r.toolNames, "bash")
}

// TestBaseInterruptRail_AddTools 验证批量工具名注册
func TestBaseInterruptRail_AddTools(t *testing.T) {
	r := NewBaseInterruptRail()
	r.AddTools([]string{"bash", "write_file", "edit_file"})
	assert.Len(t, r.toolNames, 3)
}

// TestBaseInterruptRail_GetTools 验证获取工具名列表
func TestBaseInterruptRail_GetTools(t *testing.T) {
	r := NewBaseInterruptRail("ask_user", "bash")
	tools := r.GetTools()
	assert.Len(t, tools, 2)
	assert.Contains(t, tools, "ask_user")
	assert.Contains(t, tools, "bash")
}

// ──────────────────────────── 决策构造 ────────────────────────────

// TestBaseInterruptRail_Approve 验证允许决策构造
func TestBaseInterruptRail_Approve(t *testing.T) {
	r := NewBaseInterruptRail()
	args := map[string]any{"arg": "val"}
	decision := r.Approve(&args)
	assert.IsType(t, &ApproveResult{}, decision)
	assert.NotNil(t, decision.NewArgs)
	assert.Equal(t, args, *decision.NewArgs)
}

// TestBaseInterruptRail_Reject 验证拒绝决策构造
func TestBaseInterruptRail_Reject(t *testing.T) {
	r := NewBaseInterruptRail()
	decision := r.Reject("已拒绝")
	assert.IsType(t, &RejectResult{}, decision)
	assert.Equal(t, "已拒绝", decision.ToolResult)
}

// TestBaseInterruptRail_Interrupt 验证中断决策构造
func TestBaseInterruptRail_Interrupt(t *testing.T) {
	r := NewBaseInterruptRail()
	req := &saschema.InterruptRequest{Message: "请确认"}
	decision := r.Interrupt(req)
	assert.IsType(t, &InterruptResult{}, decision)
	assert.Equal(t, req, decision.Request)
}

// ──────────────────────────── BeforeToolCall 工具调用前钩子 ────────────────────────────

// TestBaseInterruptRail_BeforeToolCall_未注册工具 验证未注册工具不拦截
func TestBaseInterruptRail_BeforeToolCall_未注册工具(t *testing.T) {
	r := NewBaseInterruptRail("ask_user")
	cbc := newTestCBC(nil)
	cbc.SetInputs(&agentinterfaces.ToolCallInputs{
		ToolCall: &llmschema.ToolCall{ID: "tc1", Name: "bash", Arguments: "{}"},
		ToolName: "bash",
		ToolArgs: map[string]any{},
	})

	err := r.BeforeToolCall(context.Background(), cbc)
	assert.NoError(t, err)
}

// TestBaseInterruptRail_BeforeToolCall_中断 验证无用户输入时抛出中断
func TestBaseInterruptRail_BeforeToolCall_中断(t *testing.T) {
	r := NewBaseInterruptRail("ask_user")
	cbc := newTestCBC(nil)
	cbc.SetInputs(&agentinterfaces.ToolCallInputs{
		ToolCall: &llmschema.ToolCall{ID: "tc1", Name: "ask_user", Arguments: "{}"},
		ToolName: "ask_user",
		ToolArgs: map[string]any{},
	})

	assert.Panics(t, func() {
		_ = r.BeforeToolCall(context.Background(), cbc)
	})
}

// TestBaseInterruptRail_BeforeToolCall_允许 验证归属表中有本 owner 的答案时放行
// （Python: resume 时 _get_user_input 经 find_own_answer 读到答案 → Approve）
func TestBaseInterruptRail_BeforeToolCall_允许(t *testing.T) {
	r := NewBaseInterruptRail("ask_user")
	cbc := newTestCBC(map[string]any{
		saschema.ResumeInterruptAnswersKey: interruptpkg.CallAnswers{
			"tc1": map[string]*saschema.InterruptAnswer{
				r.InterruptOwner(): {Owner: r.InterruptOwner(), Value: "用户回答"},
			},
		},
	})
	cbc.SetInputs(&agentinterfaces.ToolCallInputs{
		ToolCall: &llmschema.ToolCall{ID: "tc1", Name: "ask_user", Arguments: "{}"},
		ToolName: "ask_user",
		ToolArgs: map[string]any{},
	})

	err := r.BeforeToolCall(context.Background(), cbc)
	assert.NoError(t, err)
}

// TestBaseInterruptRail_BeforeToolCall_拒绝 验证拒绝决策设置 _skip_tool
func TestBaseInterruptRail_BeforeToolCall_拒绝(t *testing.T) {
	// 使用自定义 ResolveInterruptFn 测试拒绝
	r := NewBaseInterruptRail("dangerous_tool")
	r.ResolveInterruptFn = func(_ context.Context, _ *agentinterfaces.AgentCallbackContext, _ *llmschema.ToolCall, _ any, _ map[string]any) InterruptDecision {
		return r.Reject("已拒绝")
	}
	cbc := newTestCBC(nil)
	cbc.SetInputs(&agentinterfaces.ToolCallInputs{
		ToolCall: &llmschema.ToolCall{ID: "tc1", Name: "dangerous_tool", Arguments: "{}"},
		ToolName: "dangerous_tool",
		ToolArgs: map[string]any{},
	})

	err := r.BeforeToolCall(context.Background(), cbc)
	assert.NoError(t, err)
	v, _ := cbc.GetExtra("_skip_tool")
	assert.Equal(t, true, v)

	toolInputs, ok := cbc.Inputs().(*agentinterfaces.ToolCallInputs)
	require.True(t, ok)
	assert.Equal(t, "已拒绝", toolInputs.ToolResult)
}

// ──────────────────────────── GetCallbacks 回调注册 ────────────────────────────

// TestBaseInterruptRail_GetCallbacks 验证回调映射包含 BeforeToolCall
func TestBaseInterruptRail_GetCallbacks(t *testing.T) {
	r := NewBaseInterruptRail("ask_user")
	callbacks := r.GetCallbacks()
	_, hasBeforeToolCall := callbacks[agentinterfaces.CallbackBeforeToolCall]
	assert.True(t, hasBeforeToolCall, "应包含 BeforeToolCall 回调")
}

// ──────────────────────────── GetUserInput 用户输入获取 ────────────────────────────

// newCallAnswers 构造恢复答案归属表（owner 路由语义）。
func newCallAnswers(toolCallID, owner, value string) interruptpkg.CallAnswers {
	return interruptpkg.CallAnswers{
		toolCallID: map[string]*saschema.InterruptAnswer{
			owner: {Owner: owner, Value: value},
		},
	}
}

// TestBaseInterruptRail_GetUserInput_答案归属命中 验证按 owner 路由读取答案值。
// Python: BaseInterruptRail._get_user_input = find_own_answer(ctx.extra, tool_call_id).value
func TestBaseInterruptRail_GetUserInput_答案归属命中(t *testing.T) {
	r := NewBaseInterruptRail("ask_user")

	cbc := newTestCBC(map[string]any{
		saschema.ResumeInterruptAnswersKey: newCallAnswers("tc1", r.InterruptOwner(), "用户回答"),
	})

	userInput := r.GetUserInput(cbc, "tc1")
	assert.Equal(t, "用户回答", userInput)
}

// TestBaseInterruptRail_GetUserInput_未命中 验证归属表无该调用时返回 nil
func TestBaseInterruptRail_GetUserInput_未命中(t *testing.T) {
	r := NewBaseInterruptRail("ask_user")

	cbc := newTestCBC(map[string]any{
		saschema.ResumeInterruptAnswersKey: newCallAnswers("other_id", r.InterruptOwner(), "其他回答"),
	})

	userInput := r.GetUserInput(cbc, "tc1")
	assert.Nil(t, userInput)
}

// TestBaseInterruptRail_GetUserInput_owner隔离 验证只读本 owner 那份答案
func TestBaseInterruptRail_GetUserInput_owner隔离(t *testing.T) {
	r := NewBaseInterruptRail("ask_user")

	cbc := newTestCBC(map[string]any{
		saschema.ResumeInterruptAnswersKey: newCallAnswers("tc1", "other.rail", "别人的答案"),
	})

	userInput := r.GetUserInput(cbc, "tc1")
	assert.Nil(t, userInput)
}

// TestBaseInterruptRail_GetUserInput_无归属表 验证无恢复答案时返回 nil
func TestBaseInterruptRail_GetUserInput_无归属表(t *testing.T) {
	r := NewBaseInterruptRail("ask_user")

	cbc := newTestCBC(map[string]any{})

	userInput := r.GetUserInput(cbc, "tc1")
	assert.Nil(t, userInput)
}

// ──────────────────────────── applyDecision 决策应用 ────────────────────────────

// TestBaseInterruptRail_applyDecision_ApproveResult_修改参数 验证 NewArgs 非空时修改 ToolArgs
func TestBaseInterruptRail_applyDecision_ApproveResult_修改参数(t *testing.T) {
	r := NewBaseInterruptRail()
	cbc := newTestCBC(nil)
	toolInputs := &agentinterfaces.ToolCallInputs{
		ToolCall: &llmschema.ToolCall{ID: "tc1", Name: "bash", Arguments: `{"cmd":"ls"}`},
		ToolName: "bash",
		ToolArgs: map[string]any{"cmd": "ls"},
	}

	pwdArgs := map[string]any{"cmd": "pwd"}
	r.applyDecision(cbc, toolInputs, &ApproveResult{NewArgs: &pwdArgs})
	assert.Equal(t, map[string]any{"cmd": "pwd"}, toolInputs.ToolArgs)
}

// TestBaseInterruptRail_applyDecision_ApproveResult_无修改 验证 NewArgs 为空时不修改
func TestBaseInterruptRail_applyDecision_ApproveResult_无修改(t *testing.T) {
	r := NewBaseInterruptRail()
	cbc := newTestCBC(nil)
	toolInputs := &agentinterfaces.ToolCallInputs{
		ToolCall: &llmschema.ToolCall{ID: "tc1", Name: "bash", Arguments: `{"cmd":"ls"}`},
		ToolName: "bash",
		ToolArgs: map[string]any{"cmd": "ls"},
	}

	r.applyDecision(cbc, toolInputs, &ApproveResult{NewArgs: nil})
	assert.Equal(t, map[string]any{"cmd": "ls"}, toolInputs.ToolArgs)
}

// TestBaseInterruptRail_applyDecision_InterruptResult 验证中断决策抛出 panic
func TestBaseInterruptRail_applyDecision_InterruptResult(t *testing.T) {
	r := NewBaseInterruptRail()
	cbc := newTestCBC(nil)
	toolInputs := &agentinterfaces.ToolCallInputs{
		ToolCall: &llmschema.ToolCall{ID: "tc1", Name: "bash", Arguments: "{}"},
		ToolName: "bash",
		ToolArgs: map[string]any{},
	}

	assert.Panics(t, func() {
		r.applyDecision(cbc, toolInputs, &InterruptResult{
			Request: &saschema.InterruptRequest{Message: "请确认"},
		})
	})
}

// TestBaseInterruptRail_applyDecision_RejectResult 验证拒绝决策设置 _skip_tool
func TestBaseInterruptRail_applyDecision_RejectResult(t *testing.T) {
	r := NewBaseInterruptRail()
	cbc := newTestCBC(nil)
	toolInputs := &agentinterfaces.ToolCallInputs{
		ToolCall: &llmschema.ToolCall{ID: "tc1", Name: "bash", Arguments: "{}"},
		ToolName: "bash",
		ToolArgs: map[string]any{},
	}

	r.applyDecision(cbc, toolInputs, &RejectResult{ToolResult: "拒绝结果"})

	v, _ := cbc.GetExtra("_skip_tool")
	assert.Equal(t, true, v)
	assert.Equal(t, "拒绝结果", toolInputs.ToolResult)
	assert.NotNil(t, toolInputs.ToolMsg)
}

// ──────────────────────────── 决策接口 ────────────────────────────

// TestInterruptDecision_接口断言 验证三种决策实现 InterruptDecision
func TestInterruptDecision_接口断言(t *testing.T) {
	var d InterruptDecision = &ApproveResult{}
	assert.NotNil(t, d)
	d = &RejectResult{}
	assert.NotNil(t, d)
	d = &InterruptResult{}
	assert.NotNil(t, d)
}

// ──────────────────────────── resolveToolCallID 工具调用 ID 解析 ────────────────────────────

// TestBaseInterruptRail_resolveToolCallID 验证 nil ToolCall 返回空字符串
func TestBaseInterruptRail_resolveToolCallID(t *testing.T) {
	r := NewBaseInterruptRail()
	assert.Equal(t, "", r.resolveToolCallID(nil))
	assert.Equal(t, "tc1", r.resolveToolCallID(&llmschema.ToolCall{ID: "tc1"}))
}

// ──────────────────────────── skipTool 跳过工具 ────────────────────────────

// TestBaseInterruptRail_skipTool_自定义ToolMessage 验证拒绝时使用自定义 ToolMessage
func TestBaseInterruptRail_skipTool_自定义ToolMessage(t *testing.T) {
	r := NewBaseInterruptRail()
	cbc := newTestCBC(nil)
	toolInputs := &agentinterfaces.ToolCallInputs{
		ToolCall: &llmschema.ToolCall{ID: "tc1", Name: "bash", Arguments: "{}"},
		ToolName: "bash",
		ToolArgs: map[string]any{},
	}
	customMsg := llmschema.NewToolMessage("tc1", "自定义拒绝消息")

	r.skipTool(cbc, toolInputs, &RejectResult{ToolResult: "拒绝", ToolMessage: customMsg})

	v, _ := cbc.GetExtra("_skip_tool")
	assert.Equal(t, true, v)
	assert.Equal(t, "拒绝", toolInputs.ToolResult)
	assert.Equal(t, customMsg, toolInputs.ToolMsg)
}

// ──────────────────────────── BeforeToolCall 非ToolCallInputs ────────────────────────────

// TestBaseInterruptRail_BeforeToolCall_非ToolCallInputs 验证非 ToolCallInputs 输入不拦截
func TestBaseInterruptRail_BeforeToolCall_非ToolCallInputs(t *testing.T) {
	r := NewBaseInterruptRail("ask_user")
	cbc := newTestCBC(nil)
	cbc.SetInputs(&agentinterfaces.InvokeInputs{})

	err := r.BeforeToolCall(context.Background(), cbc)
	assert.NoError(t, err)
}

// ──────────────────────────── GetCallbacks 调用验证 ────────────────────────────

// TestBaseInterruptRail_GetCallbacks_调用BeforeToolCall 验证回调可正常调用
func TestBaseInterruptRail_GetCallbacks_调用BeforeToolCall(t *testing.T) {
	r := NewBaseInterruptRail("ask_user")
	callbacks := r.GetCallbacks()
	fn, ok := callbacks[agentinterfaces.CallbackBeforeToolCall]
	require.True(t, ok)

	cbc := newTestCBC(nil)
	cbc.SetInputs(&agentinterfaces.ToolCallInputs{
		ToolCall: &llmschema.ToolCall{ID: "tc1", Name: "bash", Arguments: "{}"},
		ToolName: "bash",
		ToolArgs: map[string]any{},
	})

	err := fn(context.Background(), cbc)
	assert.NoError(t, err)
}

// ──────────────────────────── 辅助类型 ────────────────────────────

// 确保 cb 包被引用（AbortError 定义）
var _ cb.AbortError

// newTestCBC 创建测试用 AgentCallbackContext，可选预填充 extra
func newTestCBC(extra map[string]any) *agentinterfaces.AgentCallbackContext {
	cbc := agentinterfaces.NewAgentCallbackContext(nil, nil, nil)
	for k, v := range extra {
		cbc.SetExtra(k, v)
	}
	return cbc
}
