package rail

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interaction"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestInvokeInputs_EventKind 验证 InvokeInputs.EventKind() 返回 "invoke"
func TestInvokeInputs_EventKind(t *testing.T) {
	assert.Equal(t, "invoke", (&InvokeInputs{}).EventKind())
}

// TestModelCallInputs_EventKind 验证 ModelCallInputs.EventKind() 返回 "model_call"
func TestModelCallInputs_EventKind(t *testing.T) {
	assert.Equal(t, "model_call", (&ModelCallInputs{}).EventKind())
}

// TestToolCallInputs_EventKind 验证 ToolCallInputs.EventKind() 返回 "tool_call"
func TestToolCallInputs_EventKind(t *testing.T) {
	assert.Equal(t, "tool_call", (&ToolCallInputs{}).EventKind())
}

// TestTaskIterationInputs_EventKind 验证 TaskIterationInputs.EventKind() 返回 "task_iteration"
func TestTaskIterationInputs_EventKind(t *testing.T) {
	assert.Equal(t, "task_iteration", (&TaskIterationInputs{}).EventKind())
}

// TestMapInputs_EventKind 验证 MapInputs.EventKind() 返回 "map"
func TestMapInputs_EventKind(t *testing.T) {
	assert.Equal(t, "map", (&MapInputs{}).EventKind())
}

// TestEventInputs_接口满足 验证各 Inputs struct 编译期满足 EventInputs 接口
func TestEventInputs_接口满足(t *testing.T) {
	var _ EventInputs = (*InvokeInputs)(nil)
	var _ EventInputs = (*ModelCallInputs)(nil)
	var _ EventInputs = (*ToolCallInputs)(nil)
	var _ EventInputs = (*TaskIterationInputs)(nil)
	var _ EventInputs = (*MapInputs)(nil)
}

// TestInvokeQuery_接口满足 验证 InvokeQueryString 和 *InteractiveInput 编译期满足 InvokeQuery 接口
func TestInvokeQuery_接口满足(t *testing.T) {
	var _ InvokeQuery = InvokeQueryString("")
	var _ InvokeQuery = (*interaction.InteractiveInput)(nil)
}

// ──────────────────────────── InvokeQuery ────────────────────────────

// TestInvokeQueryString_IsInteractiveInput 验证 InvokeQueryString.IsInteractiveInput() 返回 false
func TestInvokeQueryString_IsInteractiveInput(t *testing.T) {
	q := InvokeQueryString("你好")
	assert.False(t, q.IsInteractiveInput())
}

// TestInvokeQueryString_PlainText 验证 InvokeQueryString.PlainText() 返回字符串本身
func TestInvokeQueryString_PlainText(t *testing.T) {
	q := InvokeQueryString("hello world")
	assert.Equal(t, "hello world", q.PlainText())

	// 空字符串
	emptyQ := InvokeQueryString("")
	assert.Equal(t, "", emptyQ.PlainText())
}

// TestNewInvokeQueryString 验证 NewInvokeQueryString 构造函数
func TestNewInvokeQueryString(t *testing.T) {
	q := NewInvokeQueryString("测试查询")
	assert.NotNil(t, q)
	assert.False(t, q.IsInteractiveInput())
	assert.Equal(t, "测试查询", q.PlainText())
}

// TestInteractiveInput_InvokeQuery 验证 *InteractiveInput 实现 InvokeQuery 接口
func TestInteractiveInput_InvokeQuery(t *testing.T) {
	// InteractiveInput 无 RawInputs
	ii, err := interaction.NewInteractiveInput()
	assert.NoError(t, err)
	assert.True(t, ii.IsInteractiveInput())
	assert.Equal(t, "", ii.PlainText())

	// InteractiveInput RawInputs 为字符串
	ii2, err := interaction.NewInteractiveInput("恢复内容")
	assert.NoError(t, err)
	assert.True(t, ii2.IsInteractiveInput())
	assert.Equal(t, "恢复内容", ii2.PlainText())

	// InteractiveInput RawInputs 为非字符串
	ii3, err := interaction.NewInteractiveInput(map[string]any{"key": "val"})
	assert.NoError(t, err)
	assert.True(t, ii3.IsInteractiveInput())
	assert.Equal(t, "", ii3.PlainText())
}

// ──────────────────────────── RunKind 枚举 ────────────────────────────

// TestRunKind_值对齐Python 验证 RunKind 枚举值与 Python 一致
func TestRunKind_值对齐Python(t *testing.T) {
	assert.Equal(t, RunKind("normal"), RunKindNormal)
	assert.Equal(t, RunKind("heartbeat"), RunKindHeartbeat)
	assert.Equal(t, RunKind("cron"), RunKindCron)
}

// TestHeartbeatReason_值对齐Python 验证 HeartbeatReason 枚举值与 Python 一致
func TestHeartbeatReason_值对齐Python(t *testing.T) {
	assert.Equal(t, HeartbeatReason("interval"), HeartbeatReasonInterval)
	assert.Equal(t, HeartbeatReason("manual"), HeartbeatReasonManual)
}

// ──────────────────────────── RunContext ────────────────────────────

// TestNewRunContext 验证 NewRunContext 构造函数
func TestNewRunContext(t *testing.T) {
	rc := NewRunContext()
	assert.NotNil(t, rc)
	assert.NotNil(t, rc.Extra)
	assert.Empty(t, rc.Extra)
	assert.Equal(t, HeartbeatReason(""), rc.Reason)
	assert.Equal(t, "", rc.SessionID)
	assert.Equal(t, "", rc.ContextMode)
}

// TestRunContext_字段赋值 验证 RunContext 各字段可赋值
func TestRunContext_字段赋值(t *testing.T) {
	rc := NewRunContext()
	rc.Reason = HeartbeatReasonInterval
	rc.SessionID = "sess-123"
	rc.ContextMode = "lightweight"
	rc.Extra["key"] = "value"

	assert.Equal(t, HeartbeatReasonInterval, rc.Reason)
	assert.Equal(t, "sess-123", rc.SessionID)
	assert.Equal(t, "lightweight", rc.ContextMode)
	assert.Equal(t, "value", rc.Extra["key"])
}

// ──────────────────────────── InvokeInputs ────────────────────────────

// TestInvokeInputs_字段_InvokeQueryString 验证 InvokeInputs 各字段赋值（Query 为 InvokeQueryString）
func TestInvokeInputs_字段_InvokeQueryString(t *testing.T) {
	inputs := &InvokeInputs{
		Query:          NewInvokeQueryString("你好"),
		ConversationID: "conv-1",
		Result:         map[string]any{"key": "val"},
		RunKind:        RunKindHeartbeat,
		RunContext:     &RunContext{Reason: HeartbeatReasonInterval, SessionID: "s1", ContextMode: "lightweight"},
	}

	assert.Equal(t, "你好", inputs.Query.PlainText())
	assert.False(t, inputs.Query.IsInteractiveInput())
	assert.Equal(t, "conv-1", inputs.ConversationID)
	assert.Equal(t, "val", inputs.Result["key"])
	assert.Equal(t, RunKindHeartbeat, inputs.RunKind)
	assert.NotNil(t, inputs.RunContext)
}

// TestInvokeInputs_字段_InteractiveInput 验证 InvokeInputs.Query 为 *InteractiveInput
func TestInvokeInputs_字段_InteractiveInput(t *testing.T) {
	ii, err := interaction.NewInteractiveInput("中断恢复内容")
	assert.NoError(t, err)

	inputs := &InvokeInputs{
		Query:          ii,
		ConversationID: "conv-2",
		RunKind:        RunKindNormal,
	}

	assert.True(t, inputs.Query.IsInteractiveInput())
	assert.Equal(t, "中断恢复内容", inputs.Query.PlainText())
	assert.Equal(t, "conv-2", inputs.ConversationID)
}

// TestInvokeInputs_Query_TypeSwitch 验证 InvokeInputs.Query 的 type switch 用法
func TestInvokeInputs_Query_TypeSwitch(t *testing.T) {
	// InvokeQueryString 分支
	strQuery := NewInvokeQueryString("普通查询")
	inputs1 := &InvokeInputs{Query: strQuery}
	switch q := inputs1.Query.(type) {
	case InvokeQueryString:
		assert.Equal(t, "普通查询", string(q))
	default:
		t.Error("期望 InvokeQueryString 类型")
	}

	// *InteractiveInput 分支
	ii, _ := interaction.NewInteractiveInput("恢复查询")
	inputs2 := &InvokeInputs{Query: ii}
	switch q := inputs2.Query.(type) {
	case *interaction.InteractiveInput:
		assert.NotNil(t, q)
		assert.Equal(t, "恢复查询", q.PlainText())
	default:
		t.Error("期望 *interaction.InteractiveInput 类型")
	}
}

// TestInvokeInputs_IsHeartbeat 验证 IsHeartbeat 方法
func TestInvokeInputs_IsHeartbeat(t *testing.T) {
	assert.True(t, (&InvokeInputs{RunKind: RunKindHeartbeat}).IsHeartbeat())
	assert.False(t, (&InvokeInputs{RunKind: RunKindNormal}).IsHeartbeat())
	assert.False(t, (&InvokeInputs{RunKind: RunKindCron}).IsHeartbeat())
	assert.False(t, (&InvokeInputs{}).IsHeartbeat())
}

// TestInvokeInputs_IsLightweightContext 验证 IsLightweightContext 方法
func TestInvokeInputs_IsLightweightContext(t *testing.T) {
	// 有 RunContext 且 ContextMode 为 lightweight
	assert.True(t, (&InvokeInputs{
		RunContext: &RunContext{ContextMode: "lightweight"},
	}).IsLightweightContext())

	// 有 RunContext 但 ContextMode 不是 lightweight
	assert.False(t, (&InvokeInputs{
		RunContext: &RunContext{ContextMode: "full"},
	}).IsLightweightContext())

	// RunContext 为 nil
	assert.False(t, (&InvokeInputs{}).IsLightweightContext())

	// RunContext 非空但 ContextMode 为空
	assert.False(t, (&InvokeInputs{
		RunContext: &RunContext{},
	}).IsLightweightContext())
}

// TestInvokeInputs_IsCron 验证 IsCron 方法
func TestInvokeInputs_IsCron(t *testing.T) {
	assert.True(t, (&InvokeInputs{RunKind: RunKindCron}).IsCron())
	assert.False(t, (&InvokeInputs{RunKind: RunKindNormal}).IsCron())
	assert.False(t, (&InvokeInputs{}).IsCron())
}

// ──────────────────────────── ModelCallInputs ────────────────────────────

// TestModelCallInputs_字段 验证 ModelCallInputs 各字段赋值
func TestModelCallInputs_字段(t *testing.T) {
	msgs := []schema.BaseMessage{schema.NewDefaultMessage(schema.RoleTypeUser, "hello")}
	tc := schema.NewToolCall("id1", "tool1", "{}")
	resp := &schema.AssistantMessage{}

	inputs := &ModelCallInputs{
		Messages: msgs,
		Tools:    []schema.ToolCall{*tc},
		Response: resp,
	}

	assert.Len(t, inputs.Messages, 1)
	assert.Len(t, inputs.Tools, 1)
	assert.Equal(t, resp, inputs.Response)
	assert.Nil(t, inputs.ModelContext)
}

// TestModelCallInputs_默认零值 验证零值状态
func TestModelCallInputs_默认零值(t *testing.T) {
	inputs := &ModelCallInputs{}
	assert.Nil(t, inputs.Messages)
	assert.Nil(t, inputs.Tools)
	assert.Nil(t, inputs.ModelContext)
	assert.Nil(t, inputs.Response)
}

// ──────────────────────────── ToolCallInputs ────────────────────────────

// TestToolCallInputs_字段 验证 ToolCallInputs 各字段赋值
func TestToolCallInputs_字段(t *testing.T) {
	tc := schema.NewToolCall("id1", "search", `{"q": "test"}`)
	tm := schema.NewToolMessage("id1", "result")

	inputs := &ToolCallInputs{
		ToolCall:   tc,
		ToolName:   "search",
		ToolArgs:   map[string]any{"q": "test"},
		ToolResult: "result data",
		ToolMsg:    tm,
	}

	assert.Equal(t, tc, inputs.ToolCall)
	assert.Equal(t, "search", inputs.ToolName)
	assert.Equal(t, "result data", inputs.ToolResult)
	assert.Equal(t, tm, inputs.ToolMsg)
}

// TestToolCallInputs_默认零值 验证零值状态
func TestToolCallInputs_默认零值(t *testing.T) {
	inputs := &ToolCallInputs{}
	assert.Nil(t, inputs.ToolCall)
	assert.Equal(t, "", inputs.ToolName)
	assert.Nil(t, inputs.ToolArgs)
	assert.Nil(t, inputs.ToolResult)
	assert.Nil(t, inputs.ToolMsg)
}

// ──────────────────────────── TaskIterationInputs ────────────────────────────

// TestTaskIterationInputs_字段 验证 TaskIterationInputs 各字段赋值
func TestTaskIterationInputs_字段(t *testing.T) {
	inputs := &TaskIterationInputs{
		Iteration:      3,
		LoopEvent:      "some_event",
		ConversationID: "conv-1",
		Result:         map[string]any{"status": "ok"},
		Query:          "follow-up query",
		IsFollowUp:     true,
	}

	assert.Equal(t, 3, inputs.Iteration)
	assert.Equal(t, "some_event", inputs.LoopEvent)
	assert.Equal(t, "conv-1", inputs.ConversationID)
	assert.Equal(t, "ok", inputs.Result["status"])
	assert.Equal(t, "follow-up query", inputs.Query)
	assert.True(t, inputs.IsFollowUp)
}

// TestTaskIterationInputs_默认零值 验证零值状态
func TestTaskIterationInputs_默认零值(t *testing.T) {
	inputs := &TaskIterationInputs{}
	assert.Equal(t, 0, inputs.Iteration)
	assert.Nil(t, inputs.LoopEvent)
	assert.Equal(t, "", inputs.ConversationID)
	assert.Nil(t, inputs.Result)
	assert.Equal(t, "", inputs.Query)
	assert.False(t, inputs.IsFollowUp)
}

// ──────────────────────────── MapInputs ────────────────────────────

// TestMapInputs_字段 验证 MapInputs 各字段赋值
func TestMapInputs_字段(t *testing.T) {
	inputs := &MapInputs{
		Data: map[string]any{"key": "value", "num": 42},
	}

	assert.Equal(t, "value", inputs.Data["key"])
	assert.Equal(t, 42, inputs.Data["num"])
}

// TestMapInputs_默认零值 验证零值状态
func TestMapInputs_默认零值(t *testing.T) {
	inputs := &MapInputs{}
	assert.Nil(t, inputs.Data)
}

// TestNewMapInputs 验证 NewMapInputs 构造函数
func TestNewMapInputs(t *testing.T) {
	inputs := NewMapInputs()
	assert.NotNil(t, inputs)
	assert.NotNil(t, inputs.Data)
	assert.Empty(t, inputs.Data)
}

// ──────────────────────────── 非导出函数 ────────────────────────────
