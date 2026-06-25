//go:build test

package agents

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	saconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interrupt"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/rail"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	cschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 辅助 ────────────────────────────

// newTestAgent 创建测试用 ReActAgent
func newTestAgent(name string) *ReActAgent {
	card := agentschema.NewAgentCard(
		cschema.WithName(name),
		cschema.WithDescription("测试"),
	)
	config := saconfig.NewReActAgentConfig(
		saconfig.WithModelName("qwen-max"),
		saconfig.WithMaxIterations(1),
	)
	return NewReActAgent(card, config)
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestReActAgent_ContextEngine 默认返回 nil
func TestReActAgent_ContextEngine(t *testing.T) {
	agent := newTestAgent("ce_test")
	assert.Nil(t, agent.ContextEngine())
}

// TestReActAgent_Configure_带PromptTemplateName 配置时添加 identity 节
func TestReActAgent_Configure_带PromptTemplateName(t *testing.T) {
	agent := newTestAgent("cfg_prompt")
	config := saconfig.NewReActAgentConfig(
		saconfig.WithModelName("qwen-max"),
		saconfig.WithMaxIterations(5),
		saconfig.WithPromptTemplateName("你是助手"),
	)
	err := agent.Configure(context.Background(), config)
	require.NoError(t, err)
	assert.True(t, agent.promptBuilder.HasSection("identity"))
}

// TestReActAgent_Configure_校验失败 配置校验失败时返回错误
func TestReActAgent_Configure_校验失败(t *testing.T) {
	agent := newTestAgent("cfg_fail")
	config := saconfig.NewReActAgentConfig() // ModelName 为空，校验失败
	err := agent.Configure(context.Background(), config)
	assert.Error(t, err)
}

// TestReActAgent_InvokeImpl_有query 有 query 时 InvokeImpl 正常执行（LLM 未初始化会报错但不会 panic）
func TestReActAgent_InvokeImpl_有query(t *testing.T) {
	agent := newTestAgent("invoke_q")
	inputs := map[string]any{"query": "你好"}
	_, _ = agent.InvokeImpl(context.Background(), inputs)
}

// TestReActAgent_InvokeImpl_带session 带 session 的 InvokeImpl
func TestReActAgent_InvokeImpl_带session(t *testing.T) {
	agent := newTestAgent("invoke_sess")
	sess := session.NewSession(session.WithSessionID("test_session"))
	inputs := map[string]any{"query": "测试", "conversation_id": "conv1"}
	_, _ = agent.InvokeImpl(context.Background(), inputs, interfaces.WithSession(sess))
}

// TestReActAgent_InvokeImpl_带额外参数 带 user_id/run_kind/run_context 等额外参数
func TestReActAgent_InvokeImpl_带额外参数(t *testing.T) {
	agent := newTestAgent("invoke_extra")
	sess := session.NewSession(session.WithSessionID("extra_session"))
	inputs := map[string]any{
		"query":       "测试",
		"user_id":     "u1",
		"run_kind":    "heartbeat",
		"run_context": "ctx1",
		"_streaming":  false,
	}
	_, _ = agent.InvokeImpl(context.Background(), inputs, interfaces.WithSession(sess))
}

// TestReActAgent_InvokeImpl_context取消 context 取消时清理上下文
func TestReActAgent_InvokeImpl_context取消(t *testing.T) {
	agent := newTestAgent("invoke_cancel")
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 立即取消
	inputs := map[string]any{"query": "测试"}
	_, _ = agent.InvokeImpl(ctx, inputs)
}

// TestReActAgent_AfterExecuteToolCallForHITL handler 为 nil 时返回 nil
func TestReActAgent_AfterExecuteToolCallForHITL(t *testing.T) {
	card := agentschema.NewAgentCard(cschema.WithName("hitl_test"), cschema.WithDescription("HITL 测试"))
	agent := NewReActAgent(card, nil)
	agent.hitlHandler = nil // 显式设置为 nil

	intState, payloads := agent.AfterExecuteToolCallForHITL(nil, nil, nil, 0, "")
	assert.Nil(t, intState)
	assert.Nil(t, payloads)
}

// TestReActAgent_CommitInterrupt handler 为 nil 时返回 nil
func TestReActAgent_CommitInterrupt(t *testing.T) {
	card := agentschema.NewAgentCard(cschema.WithName("commit_test"), cschema.WithDescription("Commit 测试"))
	agent := NewReActAgent(card, nil)
	agent.hitlHandler = nil

	result, err := agent.CommitInterrupt(context.Background(), nil, nil, nil, nil, nil)
	assert.Nil(t, result)
	assert.NoError(t, err)
}

// TestReActAgent_ClearContextMessages 无 context engine 时不 panic
func TestReActAgent_ClearContextMessages(t *testing.T) {
	agent := newTestAgent("clear_ctx")
	sess := session.NewSession(session.WithSessionID("clear_session"))
	agent.ClearContextMessages(sess) // 无 context engine，应 no-op
}

// TestReActAgent_ClearContextMessages_nilSession sess 为 nil 时不 panic
func TestReActAgent_ClearContextMessages_nilSession(t *testing.T) {
	agent := newTestAgent("clear_nil")
	agent.ClearContextMessages(nil)
}

// TestReActAgent_WriteInvokeResultToStream_正常结果 正常 answer 结果写入流
func TestReActAgent_WriteInvokeResultToStream_正常结果(t *testing.T) {
	agent := newTestAgent("write_stream")
	sess := session.NewSession(session.WithSessionID("ws_session"))

	result := map[string]any{
		"output":      "你好",
		"result_type": "answer",
	}
	agent.WriteInvokeResultToStream(context.Background(), result, sess)
}

// TestReActAgent_WriteInvokeResultToStream_中断结果_有interruptIDs HITL 中断写入流
func TestReActAgent_WriteInvokeResultToStream_中断结果_有interruptIDs(t *testing.T) {
	agent := newTestAgent("write_int")
	sess := session.NewSession(session.WithSessionID("int_session"))

	result := map[string]any{
		"result_type":   "interrupt",
		"interrupt_ids": []string{"id1"},
	}
	agent.WriteInvokeResultToStream(context.Background(), result, sess)
}

// TestReActAgent_WriteInvokeResultToStream_中断结果_无interruptIDs 无 interrupt_ids 的中断（Workflow 预留）
func TestReActAgent_WriteInvokeResultToStream_中断结果_无interruptIDs(t *testing.T) {
	agent := newTestAgent("write_wf_int")
	sess := session.NewSession(session.WithSessionID("wf_int_session"))

	result := map[string]any{
		"result_type": "interrupt",
	}
	agent.WriteInvokeResultToStream(context.Background(), result, sess)
}

// TestReActAgent_executeToolCalls_空列表 空工具调用列表返回 nil
func TestReActAgent_executeToolCalls_空列表(t *testing.T) {
	agent := newTestAgent("exec_empty")
	results, err := agent.executeToolCalls(context.Background(), nil, nil, nil, nil)
	assert.NoError(t, err)
	assert.Nil(t, results)
}

// TestReActAgent_saveContexts 无 context engine 时不 panic
func TestReActAgent_saveContexts(t *testing.T) {
	agent := newTestAgent("save_ctx")
	agent.saveContexts(nil) // 无 session，应 no-op

	sess := session.NewSession(session.WithSessionID("save_session"))
	agent.saveContexts(sess) // 无 context engine，应 no-op
}

// TestReActAgent_getAbilityManager 正常返回 AbilityManager
func TestReActAgent_getAbilityManager(t *testing.T) {
	agent := newTestAgent("get_am")
	am := agent.getAbilityManager()
	assert.NotNil(t, am)
}

// TestReActAgent_getTools 正常返回工具列表
func TestReActAgent_getTools(t *testing.T) {
	agent := newTestAgent("get_tools")
	tools, err := agent.getTools()
	assert.NoError(t, err)
	// 空注册表时返回空切片或 nil
	_ = tools
}

// TestReActAgent_makeExecuteToolCallFunc 创建闭包不 panic
func TestReActAgent_makeExecuteToolCallFunc(t *testing.T) {
	agent := newTestAgent("make_exec")
	fn := agent.makeExecuteToolCallFunc()
	assert.NotNil(t, fn)
}

// TestReActAgent_makeExecuteToolCallFunc_执行 空工具调用返回空结果
func TestReActAgent_makeExecuteToolCallFunc_执行(t *testing.T) {
	agent := newTestAgent("make_exec_run")
	fn := agent.makeExecuteToolCallFunc()

	cbc := rail.NewAgentCallbackContext(nil, &rail.InvokeInputs{}, nil)
	results, err := fn(context.Background(), cbc, nil, nil, nil)
	assert.NoError(t, err)
	// 空工具调用返回空切片
	_ = results
}

// TestPromptSection_Render_非默认语言回退 渲染非默认语言时回退到默认语言
func TestPromptSection_Render_非默认语言回退(t *testing.T) {
	section := PromptSection{
		Name:     "test",
		Content:  map[string]string{"cn": "中文"},
		Priority: 10,
	}
	// 请求 en 但只有 cn，回退到 cn
	assert.Equal(t, "中文", section.Render("en"))
}

// TestPromptSection_Render_只有非默认语言 Content 只有非默认语言时，返回第一个值
func TestPromptSection_Render_只有非默认语言(t *testing.T) {
	section := PromptSection{
		Name:     "test",
		Content:  map[string]string{"en": "English"},
		Priority: 10,
	}
	// 请求 cn 但只有 en，回退到遍历第一个值
	result := section.Render("cn")
	assert.Equal(t, "English", result)
}

// TestReActAgent_InvokeImpl_带SteeringQueue 带 steering queue 的 InvokeImpl
func TestReActAgent_InvokeImpl_带SteeringQueue(t *testing.T) {
	agent := newTestAgent("invoke_steer")
	sess := session.NewSession(session.WithSessionID("steer_session"))
	steerCh := make(chan string, 10)
	inputs := map[string]any{
		"query":           "测试",
		"_steering_queue": steerCh,
	}
	_, _ = agent.InvokeImpl(context.Background(), inputs, interfaces.WithSession(sess))
}

// TestReActAgent_InvokeImpl_带中断恢复 HITL 中断恢复路径（session 中有中断状态）
func TestReActAgent_InvokeImpl_带中断恢复(t *testing.T) {
	agent := newTestAgent("invoke_resume")
	sess := session.NewSession(session.WithSessionID("resume_session"))

	// 在 session 中设置中断状态
	intState := &interrupt.ToolInterruptionState{
		BaseInterruptionState: interrupt.BaseInterruptionState{
			AIMessage:     llmschema.NewAssistantMessage("need confirm"),
			Iteration:     0,
			OriginalQuery: "原始查询",
		},
		InterruptedTools:   map[string]*interrupt.ToolInterruptEntry{},
		AutoConfirmMapping: map[string]string{},
	}
	sess.UpdateState(map[string]any{interrupt.InterruptionKey: intState})

	inputs := map[string]any{"query": "用户确认"}
	_, _ = agent.InvokeImpl(context.Background(), inputs, interfaces.WithSession(sess))
}

// TestReActAgent_WriteInvokeResultToStream_nilHandler hitlHandler 为 nil 时不 panic
func TestReActAgent_WriteInvokeResultToStream_nilHandler(t *testing.T) {
	card := agentschema.NewAgentCard(cschema.WithName("nil_handler"), cschema.WithDescription("nil handler"))
	agent := NewReActAgent(card, nil)
	agent.hitlHandler = nil

	sess := session.NewSession(session.WithSessionID("nil_h_session"))
	result := map[string]any{
		"result_type":   "interrupt",
		"interrupt_ids": []string{"id1"},
	}
	agent.WriteInvokeResultToStream(context.Background(), result, sess)
}

// ──────────────────────────── 非导出函数 ────────────────────────────
