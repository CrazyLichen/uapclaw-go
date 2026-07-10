package interrupt

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	sessioninteraction "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interaction"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
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

// ──────────────────────────── AddTool/AddTools/GetTools ────────────────────────────

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
	decision := r.Approve(`{"arg":"val"}`)
	assert.IsType(t, &ApproveResult{}, decision)
	assert.Equal(t, `{"arg":"val"}`, decision.NewArgs)
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

// ──────────────────────────── BeforeToolCall ────────────────────────────

// TestBaseInterruptRail_BeforeToolCall_未注册工具 验证未注册工具不拦截
func TestBaseInterruptRail_BeforeToolCall_未注册工具(t *testing.T) {
	r := NewBaseInterruptRail("ask_user")
	cbc := newTestCBC(nil)
	cbc.SetInputs(&agentinterfaces.ToolCallInputs{
		ToolCall: &llmschema.ToolCall{ID: "tc1", Name: "bash", Arguments: "{}"},
		ToolName: "bash",
		ToolArgs: "{}",
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
		ToolArgs: "{}",
	})

	assert.Panics(t, func() {
		_ = r.BeforeToolCall(context.Background(), cbc)
	})
}

// TestBaseInterruptRail_BeforeToolCall_允许 验证有用户输入时放行
func TestBaseInterruptRail_BeforeToolCall_允许(t *testing.T) {
	r := NewBaseInterruptRail("ask_user")
	cbc := newTestCBC(map[string]any{
		saschema.ResumeUserInputKey: map[string]any{
			"tc1": "用户回答",
		},
	})
	cbc.SetInputs(&agentinterfaces.ToolCallInputs{
		ToolCall: &llmschema.ToolCall{ID: "tc1", Name: "ask_user", Arguments: "{}"},
		ToolName: "ask_user",
		ToolArgs: "{}",
	})

	err := r.BeforeToolCall(context.Background(), cbc)
	assert.NoError(t, err)
}

// TestBaseInterruptRail_BeforeToolCall_拒绝 验证拒绝决策设置 _skip_tool
func TestBaseInterruptRail_BeforeToolCall_拒绝(t *testing.T) {
	// 使用自定义 resolveInterruptFn 测试拒绝
	r := NewBaseInterruptRail("dangerous_tool")
	r.resolveInterruptFn = func(_ context.Context, _ *agentinterfaces.AgentCallbackContext, _ *llmschema.ToolCall, _ any, _ map[string]any) InterruptDecision {
		return r.Reject("已拒绝")
	}
	cbc := newTestCBC(nil)
	cbc.SetInputs(&agentinterfaces.ToolCallInputs{
		ToolCall: &llmschema.ToolCall{ID: "tc1", Name: "dangerous_tool", Arguments: "{}"},
		ToolName: "dangerous_tool",
		ToolArgs: "{}",
	})

	err := r.BeforeToolCall(context.Background(), cbc)
	assert.NoError(t, err)
	assert.Equal(t, true, cbc.Extra()["_skip_tool"])

	toolInputs, ok := cbc.Inputs().(*agentinterfaces.ToolCallInputs)
	require.True(t, ok)
	assert.Equal(t, "已拒绝", toolInputs.ToolResult)
}

// ──────────────────────────── GetCallbacks ────────────────────────────

// TestBaseInterruptRail_GetCallbacks 验证回调映射包含 BeforeToolCall
func TestBaseInterruptRail_GetCallbacks(t *testing.T) {
	r := NewBaseInterruptRail("ask_user")
	callbacks := r.GetCallbacks()
	_, hasBeforeToolCall := callbacks[agentinterfaces.CallbackBeforeToolCall]
	assert.True(t, hasBeforeToolCall, "应包含 BeforeToolCall 回调")
}

// ──────────────────────────── getUserInput ────────────────────────────

// TestBaseInterruptRail_getUserInput_InteractiveInput 验证从 InteractiveInput 提取
func TestBaseInterruptRail_getUserInput_InteractiveInput(t *testing.T) {
	r := NewBaseInterruptRail("ask_user")

	interactive, err := sessioninteraction.NewInteractiveInput()
	require.NoError(t, err)
	interactive.UserInputs["tc1"] = "用户回答"

	cbc := newTestCBC(map[string]any{
		saschema.ResumeUserInputKey: interactive,
	})

	userInput := r.getUserInput(cbc, "tc1")
	assert.Equal(t, "用户回答", userInput)
}

// TestBaseInterruptRail_getUserInput_InteractiveInput_未匹配 验证 InteractiveInput 中无匹配
func TestBaseInterruptRail_getUserInput_InteractiveInput_未匹配(t *testing.T) {
	r := NewBaseInterruptRail("ask_user")

	interactive, err := sessioninteraction.NewInteractiveInput()
	require.NoError(t, err)
	interactive.UserInputs["other_id"] = "其他回答"

	cbc := newTestCBC(map[string]any{
		saschema.ResumeUserInputKey: interactive,
	})

	userInput := r.getUserInput(cbc, "tc1")
	assert.Nil(t, userInput)
}

// TestBaseInterruptRail_getUserInput_Dict 验证从 map 提取
func TestBaseInterruptRail_getUserInput_Dict(t *testing.T) {
	r := NewBaseInterruptRail("ask_user")

	cbc := newTestCBC(map[string]any{
		saschema.ResumeUserInputKey: map[string]any{
			"tc1": "dict回答",
		},
	})

	userInput := r.getUserInput(cbc, "tc1")
	assert.Equal(t, "dict回答", userInput)
}

// TestBaseInterruptRail_getUserInput_Dict_未匹配 验证 map 中无匹配时返回整个 map
func TestBaseInterruptRail_getUserInput_Dict_未匹配(t *testing.T) {
	r := NewBaseInterruptRail("ask_user")

	rawMap := map[string]any{"other_key": "其他值"}
	cbc := newTestCBC(map[string]any{
		saschema.ResumeUserInputKey: rawMap,
	})

	userInput := r.getUserInput(cbc, "tc1")
	result, ok := userInput.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "其他值", result["other_key"])
}

// TestBaseInterruptRail_getUserInput_无输入 验证 extra 中无用户输入
func TestBaseInterruptRail_getUserInput_无输入(t *testing.T) {
	r := NewBaseInterruptRail("ask_user")
	cbc := newTestCBC(nil)

	userInput := r.getUserInput(cbc, "tc1")
	assert.Nil(t, userInput)
}

// TestBaseInterruptRail_getUserInput_其他类型 验证其他类型直接返回
func TestBaseInterruptRail_getUserInput_其他类型(t *testing.T) {
	r := NewBaseInterruptRail("ask_user")

	cbc := newTestCBC(map[string]any{
		saschema.ResumeUserInputKey: "直接字符串",
	})

	userInput := r.getUserInput(cbc, "tc1")
	assert.Equal(t, "直接字符串", userInput)
}

// ──────────────────────────── applyDecision ────────────────────────────

// TestBaseInterruptRail_applyDecision_ApproveResult_修改参数 验证 NewArgs 非空时修改 ToolArgs
func TestBaseInterruptRail_applyDecision_ApproveResult_修改参数(t *testing.T) {
	r := NewBaseInterruptRail()
	cbc := newTestCBC(nil)
	toolInputs := &agentinterfaces.ToolCallInputs{
		ToolCall: &llmschema.ToolCall{ID: "tc1", Name: "bash", Arguments: `{"cmd":"ls"}`},
		ToolName: "bash",
		ToolArgs: `{"cmd":"ls"}`,
	}

	r.applyDecision(cbc, toolInputs, &ApproveResult{NewArgs: `{"cmd":"pwd"}`})
	assert.Equal(t, `{"cmd":"pwd"}`, toolInputs.ToolArgs)
}

// TestBaseInterruptRail_applyDecision_ApproveResult_无修改 验证 NewArgs 为空时不修改
func TestBaseInterruptRail_applyDecision_ApproveResult_无修改(t *testing.T) {
	r := NewBaseInterruptRail()
	cbc := newTestCBC(nil)
	toolInputs := &agentinterfaces.ToolCallInputs{
		ToolCall: &llmschema.ToolCall{ID: "tc1", Name: "bash", Arguments: `{"cmd":"ls"}`},
		ToolName: "bash",
		ToolArgs: `{"cmd":"ls"}`,
	}

	r.applyDecision(cbc, toolInputs, &ApproveResult{NewArgs: ""})
	assert.Equal(t, `{"cmd":"ls"}`, toolInputs.ToolArgs)
}

// TestBaseInterruptRail_applyDecision_InterruptResult 验证中断决策抛出 panic
func TestBaseInterruptRail_applyDecision_InterruptResult(t *testing.T) {
	r := NewBaseInterruptRail()
	cbc := newTestCBC(nil)
	toolInputs := &agentinterfaces.ToolCallInputs{
		ToolCall: &llmschema.ToolCall{ID: "tc1", Name: "bash", Arguments: "{}"},
		ToolName: "bash",
		ToolArgs: "{}",
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
		ToolArgs: "{}",
	}

	r.applyDecision(cbc, toolInputs, &RejectResult{ToolResult: "拒绝结果"})

	assert.Equal(t, true, cbc.Extra()["_skip_tool"])
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

// ──────────────────────────── resolveToolCallID ────────────────────────────

// TestBaseInterruptRail_resolveToolCallID 验证 nil ToolCall 返回空字符串
func TestBaseInterruptRail_resolveToolCallID(t *testing.T) {
	r := NewBaseInterruptRail()
	assert.Equal(t, "", r.resolveToolCallID(nil))
	assert.Equal(t, "tc1", r.resolveToolCallID(&llmschema.ToolCall{ID: "tc1"}))
}

// ──────────────────────────── skipTool ────────────────────────────

// TestBaseInterruptRail_skipTool_自定义ToolMessage 验证拒绝时使用自定义 ToolMessage
func TestBaseInterruptRail_skipTool_自定义ToolMessage(t *testing.T) {
	r := NewBaseInterruptRail()
	cbc := newTestCBC(nil)
	toolInputs := &agentinterfaces.ToolCallInputs{
		ToolCall: &llmschema.ToolCall{ID: "tc1", Name: "bash", Arguments: "{}"},
		ToolName: "bash",
		ToolArgs: "{}",
	}
	customMsg := llmschema.NewToolMessage("tc1", "自定义拒绝消息")

	r.skipTool(cbc, toolInputs, &RejectResult{ToolResult: "拒绝", ToolMessage: customMsg})

	assert.Equal(t, true, cbc.Extra()["_skip_tool"])
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
		ToolArgs: "{}",
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
	if extra != nil {
		for k, v := range extra {
			cbc.Extra()[k] = v
		}
	}
	return cbc
}
