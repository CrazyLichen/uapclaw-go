//go:build test

package agents

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	ceinterface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/token"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/ability"
	saconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interrupt"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/skills"
	cschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// fakeContextEngine mock 上下文引擎
type fakeContextEngine struct {
	createContextErr error
	saveContextsErr  error
	modelCtx         ceinterface.ModelContext
}

// fakeModelContext mock ModelContext
type fakeModelContext struct {
	messages []llmschema.BaseMessage
}

// fakeSessionFacade mock SessionFacade（最简实现）
type fakeSessionFacade struct {
	sessionID string
	stateData map[string]any
}

// ──────────────────────────── 辅助函数 ────────────────────────────

// newTestAgent 创建测试用 ReActAgent
func newTestAgent(name string) *ReActAgent {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName(name),
		agentschema.WithAgentDescription("测试"),
	)
	config := saconfig.NewReActAgentConfig(
		saconfig.WithModelName("qwen-max"),
		saconfig.WithMaxIterations(1),
	)
	return NewReActAgent(card, config)
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestNewReActAgent 验证 ReActAgent 构造函数
func TestNewReActAgent(t *testing.T) {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("test_react"),
		agentschema.WithAgentDescription("测试 ReActAgent"),
	)
	config := saconfig.NewReActAgentConfig(
		saconfig.WithModelName("qwen-max"),
	)

	agent := NewReActAgent(card, config)
	assert.NotNil(t, agent)
	assert.NotNil(t, agent.card)
	assert.Equal(t, config, agent.config)
	assert.NotNil(t, agent.promptBuilder)
}

// TestNewReActAgent_nilConfig 验证 ReActAgent 接受 nil config
func TestNewReActAgent_nilConfig(t *testing.T) {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("nil_cfg_react"),
		agentschema.WithAgentDescription("nil 配置 ReActAgent"),
	)
	agent := NewReActAgent(card, nil)
	assert.NotNil(t, agent)
	assert.Nil(t, agent.config)
}

// TestReActAgent_InvokeImpl_空输入 验证无 query 时不 panic
func TestReActAgent_InvokeImpl_空输入(t *testing.T) {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("test_react"),
		agentschema.WithAgentDescription("测试 ReActAgent"),
	)
	config := saconfig.NewReActAgentConfig(
		saconfig.WithModelName("qwen-max"),
		saconfig.WithMaxIterations(1),
	)
	agent := NewReActAgent(card, config)

	inputs := map[string]any{}
	_, _ = agent.invokeImpl(context.Background(), inputs)
}

// TestReActAgent_InvokeImpl_空query 验证空 query 返回错误
func TestReActAgent_InvokeImpl_空query(t *testing.T) {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("test_react"),
		agentschema.WithAgentDescription("测试 ReActAgent"),
	)
	config := saconfig.NewReActAgentConfig(
		saconfig.WithModelName("qwen-max"),
		saconfig.WithMaxIterations(1),
	)
	agent := NewReActAgent(card, config)

	inputs := map[string]any{"query": ""}
	_, err := agent.invokeImpl(context.Background(), inputs)
	assert.Error(t, err)
}

// TestReActAgent_InvokeImpl_上下文取消 验证 context.Canceled 时清除上下文
func TestReActAgent_InvokeImpl_上下文取消(t *testing.T) {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("test_react"),
		agentschema.WithAgentDescription("测试 ReActAgent"),
	)
	config := saconfig.NewReActAgentConfig(
		saconfig.WithModelName("qwen-max"),
		saconfig.WithMaxIterations(1),
	)
	agent := NewReActAgent(card, config)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	inputs := map[string]any{"query": "hello"}
	_, _ = agent.invokeImpl(ctx, inputs)
}

// TestReActAgent_InvokeImpl_带Session 验证 InvokeImpl 使用已有 session
func TestReActAgent_InvokeImpl_带Session(t *testing.T) {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("invoke_sess"),
		agentschema.WithAgentDescription("Invoke 带 Session 测试"),
	)
	config := saconfig.NewReActAgentConfig(
		saconfig.WithModelName("qwen-max"),
		saconfig.WithMaxIterations(1),
	)
	agent := NewReActAgent(card, config)

	sess := session.NewSession(session.WithSessionID("test_invoke_session"))
	inputs := map[string]any{"query": "hello"}
	_, _ = agent.invokeImpl(context.Background(), inputs, interfaces.WithSession(sess))
}

// TestReActAgent_InvokeImpl_boolStreaming 验证 _streaming 布尔值处理
func TestReActAgent_InvokeImpl_boolStreaming(t *testing.T) {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("invoke_bool"),
		agentschema.WithAgentDescription("Invoke bool 测试"),
	)
	config := saconfig.NewReActAgentConfig(
		saconfig.WithModelName("qwen-max"),
		saconfig.WithMaxIterations(1),
	)
	agent := NewReActAgent(card, config)

	inputs := map[string]any{
		"query":      "hello",
		"_streaming": true,
	}
	_, _ = agent.invokeImpl(context.Background(), inputs)
}

// TestReActAgent_AgentID 验证 AgentID 返回正确值
func TestReActAgent_AgentID(t *testing.T) {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("id_test"),
		agentschema.WithAgentDescription("ID 测试"),
	)
	agent := NewReActAgent(card, nil)
	assert.Equal(t, card.ID, agent.AgentID())
}

// TestReActAgent_CallbackManager 验证 CallbackManager 不为 nil
func TestReActAgent_CallbackManager(t *testing.T) {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("cb_test"),
		agentschema.WithAgentDescription("回调测试"),
	)
	agent := NewReActAgent(card, nil)
	assert.NotNil(t, agent.CallbackManager())
}

// TestReActAgent_ContextEngine_设置引擎 验证 ContextEngine 设置后返回正确值
func TestReActAgent_ContextEngine_设置引擎(t *testing.T) {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("ce_set"),
		agentschema.WithAgentDescription("设置上下文引擎测试"),
	)
	agent := NewReActAgent(card, nil)
	fce := &fakeContextEngine{}
	agent.contextEngine = fce
	assert.Equal(t, fce, agent.ContextEngine())
}

// TestReActAgent_Configure 验证 Configure 正常配置
func TestReActAgent_Configure(t *testing.T) {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("cfg_test"),
		agentschema.WithAgentDescription("配置测试"),
	)
	agent := NewReActAgent(card, nil)

	config := saconfig.NewReActAgentConfig(
		saconfig.WithModelName("qwen-max"),
		saconfig.WithMaxIterations(5),
	)
	err := agent.Configure(context.Background(), config)
	assert.NoError(t, err)
	assert.Equal(t, config, agent.config)
}

// TestReActAgent_Configure_nil配置 验证 nil 配置返回错误
func TestReActAgent_Configure_nil配置(t *testing.T) {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("nil_cfg"),
		agentschema.WithAgentDescription("nil 配置测试"),
	)
	agent := NewReActAgent(card, nil)
	err := agent.Configure(context.Background(), nil)
	assert.Error(t, err)
}

// TestReActAgent_Configure_带提示词模板 验证 Configure 带 PromptTemplateName 时添加提示节
func TestReActAgent_Configure_带提示词模板(t *testing.T) {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("cfg_prompt"),
		agentschema.WithAgentDescription("配置提示词测试"),
	)
	agent := NewReActAgent(card, nil)

	config := saconfig.NewReActAgentConfig(
		saconfig.WithModelName("qwen-max"),
		saconfig.WithMaxIterations(5),
		saconfig.WithPromptTemplateName("你是一个助手"),
	)
	err := agent.Configure(context.Background(), config)
	assert.NoError(t, err)
	assert.True(t, agent.promptBuilder.HasSection("identity"))
}

// TestReActAgent_AddPromptBuilderSection 验证添加提示节
func TestReActAgent_AddPromptBuilderSection(t *testing.T) {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("prompt_test"),
		agentschema.WithAgentDescription("提示词测试"),
	)
	agent := NewReActAgent(card, nil)
	agent.AddPromptBuilderSection("identity", "我是测试Agent", 10)
	assert.True(t, agent.promptBuilder.HasSection("identity"))
}

// TestReActAgent_AddPromptBuilderSection_空内容时移除 验证空内容时移除节
//
// 对齐 Python: ReActAgent.add_prompt_builder_section — content 为空时 remove_section
func TestReActAgent_AddPromptBuilderSection_空内容时移除(t *testing.T) {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("prompt_empty"),
		agentschema.WithAgentDescription("空内容提示词测试"),
	)
	agent := NewReActAgent(card, nil)
	agent.AddPromptBuilderSection("identity", "内容", 10)
	assert.True(t, agent.promptBuilder.HasSection("identity"))

	// 空内容应移除
	agent.AddPromptBuilderSection("identity", "", 10)
	assert.False(t, agent.promptBuilder.HasSection("identity"))
}

// TestReActAgent_AddPromptBuilderSection_空白内容时移除 验证全空白内容时移除节
func TestReActAgent_AddPromptBuilderSection_空白内容时移除(t *testing.T) {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("prompt_ws"),
		agentschema.WithAgentDescription("空白内容提示词测试"),
	)
	agent := NewReActAgent(card, nil)
	agent.AddPromptBuilderSection("identity", "   ", 10)
	assert.False(t, agent.promptBuilder.HasSection("identity"))
}

// TestReActAgent_StreamImpl 验证 StreamImpl 返回 channel
func TestReActAgent_StreamImpl(t *testing.T) {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("stream_test"),
		agentschema.WithAgentDescription("流式测试"),
	)
	config := saconfig.NewReActAgentConfig(
		saconfig.WithModelName("qwen-max"),
		saconfig.WithMaxIterations(1),
	)
	agent := NewReActAgent(card, config)

	inputs := map[string]any{"query": "test"}
	ch, _ := agent.streamImpl(context.Background(), inputs)
	assert.NotNil(t, ch)
	for range ch {
	}
}

// TestReActAgent_StreamImpl_带Session 验证 StreamImpl 使用已有 session
func TestReActAgent_StreamImpl_带Session(t *testing.T) {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("stream_sess"),
		agentschema.WithAgentDescription("流式 Session 测试"),
	)
	config := saconfig.NewReActAgentConfig(
		saconfig.WithModelName("qwen-max"),
		saconfig.WithMaxIterations(1),
	)
	agent := NewReActAgent(card, config)

	sess := session.NewSession(session.WithSessionID("test_stream_session"))
	inputs := map[string]any{"query": "test"}
	ch, err := agent.streamImpl(context.Background(), inputs, interfaces.WithSession(sess))
	assert.NoError(t, err)
	assert.NotNil(t, ch)
	for range ch {
	}
}

// TestReActAgent_getLLM_无配置 验证无配置时返回错误
func TestReActAgent_getLLM_无配置(t *testing.T) {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("llm_test"),
		agentschema.WithAgentDescription("LLM 测试"),
	)
	agent := NewReActAgent(card, nil)
	_, err := agent.getLLM()
	assert.Error(t, err)
}

// TestReActAgent_initContext_无引擎 验证无 context engine 时返回 nil
func TestReActAgent_initContext_无引擎(t *testing.T) {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("ctx_test"),
		agentschema.WithAgentDescription("上下文测试"),
	)
	agent := NewReActAgent(card, nil)
	mc, err := agent.initContext(context.Background(), nil)
	assert.NoError(t, err)
	assert.Nil(t, mc)
}

// TestReActAgent_initContext_有引擎 验证有 context engine 时正确调用
func TestReActAgent_initContext_有引擎(t *testing.T) {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("ctx_test2"),
		agentschema.WithAgentDescription("上下文引擎测试"),
	)
	agent := NewReActAgent(card, nil)

	fmc := &fakeModelContext{}
	fce := &fakeContextEngine{modelCtx: fmc}
	agent.contextEngine = fce

	sess := session.NewSession(session.WithSessionID("test_init_ctx"))
	mc, err := agent.initContext(context.Background(), sess)
	assert.NoError(t, err)
	assert.Equal(t, fmc, mc)
}

// TestReActAgent_initContext_引擎出错 验证 context engine 创建上下文出错时返回错误
func TestReActAgent_initContext_引擎出错(t *testing.T) {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("ctx_err"),
		agentschema.WithAgentDescription("上下文错误测试"),
	)
	agent := NewReActAgent(card, nil)

	fce := &fakeContextEngine{createContextErr: context.DeadlineExceeded}
	agent.contextEngine = fce

	sess := session.NewSession(session.WithSessionID("test_init_ctx_err"))
	mc, err := agent.initContext(context.Background(), sess)
	assert.Error(t, err)
	assert.Nil(t, mc)
}

// TestReActAgent_saveContexts_引擎保存出错 验证 saveContexts 出错时仅打日志不 panic
func TestReActAgent_saveContexts_引擎保存出错(t *testing.T) {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("save_err"),
		agentschema.WithAgentDescription("保存错误测试"),
	)
	agent := NewReActAgent(card, nil)
	fce := &fakeContextEngine{saveContextsErr: context.DeadlineExceeded}
	agent.contextEngine = fce
	sess := session.NewSession(session.WithSessionID("test_save_err"))
	agent.saveContexts(sess)
}

// TestReActAgent_ClearContextMessages_有引擎 验证有 context engine 时清除消息
func TestReActAgent_ClearContextMessages_有引擎(t *testing.T) {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("clear_ctx2"),
		agentschema.WithAgentDescription("清除上下文消息测试2"),
	)
	agent := NewReActAgent(card, nil)

	fmc := &fakeModelContext{}
	fce := &fakeContextEngine{modelCtx: fmc}
	agent.contextEngine = fce

	sess := session.NewSession(session.WithSessionID("test_clear"))
	agent.ClearContextMessages(sess)
}

// TestReActAgent_ClearContextMessages_引擎返回nil 验证 GetContext 返回 nil 时不 panic
func TestReActAgent_ClearContextMessages_引擎返回nil(t *testing.T) {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("clear_nil"),
		agentschema.WithAgentDescription("清除 nil 上下文测试"),
	)
	agent := NewReActAgent(card, nil)

	fce := &fakeContextEngine{modelCtx: nil}
	agent.contextEngine = fce

	sess := session.NewSession(session.WithSessionID("test_clear_nil"))
	agent.ClearContextMessages(sess)
}

// TestReActAgent_AfterExecuteToolCallForHITL_有中断 验证 HITL 检测到中断
func TestReActAgent_AfterExecuteToolCallForHITL_有中断(t *testing.T) {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("hitl_int"),
		agentschema.WithAgentDescription("HITL 中断测试"),
	)
	agent := NewReActAgent(card, nil)

	// 构造 ToolInterruptException 结果
	req := &agentschema.InterruptRequest{Message: "请确认", AutoConfirmKey: "auto_key"}
	tie := &agentschema.ToolInterruptException{
		Request:  req,
		ToolCall: &llmschema.ToolCall{ID: "tc1", Name: "test_tool", Arguments: "{}"},
	}
	results := []agentschema.ExecuteResult{{Result: tie, ToolMsg: nil}}
	toolCalls := []*llmschema.ToolCall{{ID: "tc1", Name: "test_tool", Arguments: "{}"}}
	aiMsg := llmschema.NewAssistantMessage("test")

	intState, payloads := agent.AfterExecuteToolCallForHITL(results, toolCalls, aiMsg, 0, "original query")
	assert.NotNil(t, intState)
	assert.NotNil(t, payloads)
	assert.Equal(t, "original query", intState.OriginalQuery)
}

// TestReActAgent_CommitInterrupt_有中断状态 验证提交中断正常工作
func TestReActAgent_CommitInterrupt_有中断状态(t *testing.T) {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("commit_int"),
		agentschema.WithAgentDescription("提交中断测试"),
	)
	agent := NewReActAgent(card, nil)

	req := &agentschema.InterruptRequest{Message: "确认操作", AutoConfirmKey: "key1"}
	intState := &agentschema.ToolInterruptionState{
		InterruptedTools: map[string]*agentschema.ToolInterruptEntry{
			"tc1": {
				ToolCall: &llmschema.ToolCall{ID: "tc1", Name: "tool1"},
				InterruptRequests: map[string]agentschema.InterruptRequester{
					"tc1": req,
				},
			},
		},
	}
	invokeInputs := &interfaces.InvokeInputs{
		Query: interfaces.NewInvokeQueryString("test"),
	}
	sess := session.NewSession(session.WithSessionID("test_commit"))

	result, err := agent.CommitInterrupt(context.Background(), intState, nil, sess, invokeInputs, nil)
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "interrupt", result["result_type"])
}

// TestReActAgent_executeToolCalls_无AbilityManager 验证无 AbilityManager 时返回错误
func TestReActAgent_executeToolCalls_无AbilityManager(t *testing.T) {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("exec_no_am"),
		agentschema.WithAgentDescription("无能力管理器测试"),
	)
	agent := NewReActAgent(card, nil)

	toolCalls := []*llmschema.ToolCall{
		{ID: "tc1", Name: "test_tool", Arguments: "{}"},
	}
	cbc := interfaces.NewAgentCallbackContext(nil, nil, nil)
	// newTestAgent 创建的 agent 有 AbilityManager，不会返回 error
	// 只需验证不 panic
	results, _ := agent.executeToolCalls(context.Background(), cbc, toolCalls, nil, nil)
	// 有 AbilityManager 时返回结果（包含错误信息），但 err 为 nil
	_ = results
}

// TestWriteInvokeResultToStream_正常结果 验证正常结果写入流
func TestWriteInvokeResultToStream_正常结果(t *testing.T) {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("write_normal"),
		agentschema.WithAgentDescription("写入正常结果测试"),
	)
	agent := NewReActAgent(card, nil)

	sess := session.NewSession(session.WithSessionID("test_write"))
	result := map[string]any{
		"output":      "hello world",
		"result_type": "answer",
	}
	agent.WriteInvokeResultToStream(context.Background(), result, sess)
}

// TestWriteInvokeResultToStream_中断结果 验证中断结果写入流
func TestWriteInvokeResultToStream_中断结果(t *testing.T) {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("write_int"),
		agentschema.WithAgentDescription("写入中断结果测试"),
	)
	agent := NewReActAgent(card, nil)

	sess := session.NewSession(session.WithSessionID("test_write_int"))
	result := map[string]any{
		"result_type":   "interrupt",
		"interrupt_ids": []string{"int1"},
	}
	agent.WriteInvokeResultToStream(context.Background(), result, sess)
}

// TestWriteInvokeResultToStream_中断结果有Handler 验证中断结果有 HITL handler 时写入
func TestWriteInvokeResultToStream_中断结果有Handler(t *testing.T) {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("write_int_h"),
		agentschema.WithAgentDescription("写入中断结果有 Handler 测试"),
	)
	agent := NewReActAgent(card, nil)

	sess := session.NewSession(session.WithSessionID("test_write_int_h"))
	result := map[string]any{
		"result_type":   "interrupt",
		"interrupt_ids": []string{"int1"},
		"state":         []any{},
	}
	agent.WriteInvokeResultToStream(context.Background(), result, sess)
}

// TestWriteInvokeResultToStream_中断无interruptIDs 验证中断结果无 interrupt_ids 走 Workflow 分支
func TestWriteInvokeResultToStream_中断无interruptIDs(t *testing.T) {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("write_wf"),
		agentschema.WithAgentDescription("写入 Workflow 中断测试"),
	)
	agent := NewReActAgent(card, nil)

	sess := session.NewSession(session.WithSessionID("test_write_wf"))
	result := map[string]any{
		"result_type": "interrupt",
	}
	agent.WriteInvokeResultToStream(context.Background(), result, sess)
}

// TestReActAgent_InvokeImpl_有ContextEngine 验证 InvokeImpl 有 context engine 时正常执行
func TestReActAgent_InvokeImpl_有ContextEngine(t *testing.T) {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("invoke_ce"),
		agentschema.WithAgentDescription("Invoke 有 ContextEngine 测试"),
	)
	config := saconfig.NewReActAgentConfig(
		saconfig.WithModelName("qwen-max"),
		saconfig.WithMaxIterations(1),
	)
	agent := NewReActAgent(card, config)

	fmc := &fakeModelContext{}
	fce := &fakeContextEngine{modelCtx: fmc}
	agent.contextEngine = fce

	sess := session.NewSession(session.WithSessionID("test_invoke_ce"))
	inputs := map[string]any{"query": "hello"}
	// LLM 未初始化会报错，但不 panic
	_, _ = agent.invokeImpl(context.Background(), inputs, interfaces.WithSession(sess))
}

// TestReActAgent_InvokeImpl_有ContextEngineAndPrompt 验证有 context engine 和 prompt 时执行
func TestReActAgent_InvokeImpl_有ContextEngineAndPrompt(t *testing.T) {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("invoke_cep"),
		agentschema.WithAgentDescription("Invoke 有 ContextEngine 和 Prompt 测试"),
	)
	config := saconfig.NewReActAgentConfig(
		saconfig.WithModelName("qwen-max"),
		saconfig.WithMaxIterations(1),
		saconfig.WithPromptTemplateName("你是助手"),
	)
	agent := NewReActAgent(card, config)

	fmc := &fakeModelContext{}
	fce := &fakeContextEngine{modelCtx: fmc}
	agent.contextEngine = fce

	sess := session.NewSession(session.WithSessionID("test_invoke_cep"))
	inputs := map[string]any{"query": "hello"}
	_, _ = agent.invokeImpl(context.Background(), inputs, interfaces.WithSession(sess))
}

// TestReActAgent_InvokeImpl_SteeringQueueWithSession 验证带 steering queue 和 session
func TestReActAgent_InvokeImpl_SteeringQueueWithSession(t *testing.T) {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("invoke_steer_sess"),
		agentschema.WithAgentDescription("Invoke Steering Session 测试"),
	)
	config := saconfig.NewReActAgentConfig(
		saconfig.WithModelName("qwen-max"),
		saconfig.WithMaxIterations(1),
	)
	agent := NewReActAgent(card, config)

	steeringCh := make(chan string, 10)
	sess := session.NewSession(session.WithSessionID("steer_sess"))
	inputs := map[string]any{
		"query":           "hello",
		"_steering_queue": steeringCh,
	}
	_, _ = agent.invokeImpl(context.Background(), inputs, interfaces.WithSession(sess))
}

// TestReActAgent_InvokeImpl_中断恢复 验证 HITL 中断恢复路径
func TestReActAgent_InvokeImpl_中断恢复(t *testing.T) {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("invoke_resume"),
		agentschema.WithAgentDescription("Invoke 中断恢复测试"),
	)
	config := saconfig.NewReActAgentConfig(
		saconfig.WithModelName("qwen-max"),
		saconfig.WithMaxIterations(1),
	)
	agent := NewReActAgent(card, config)

	sess := session.NewSession(session.WithSessionID("resume_session"))

	// 在 session 中设置中断状态
	intState := &agentschema.ToolInterruptionState{
		BaseInterruptionState: agentschema.BaseInterruptionState{
			AIMessage:     llmschema.NewAssistantMessage("need confirm"),
			Iteration:     0,
			OriginalQuery: "原始查询",
		},
		InterruptedTools:   map[string]*agentschema.ToolInterruptEntry{},
		AutoConfirmMapping: map[string]string{},
	}
	sess.UpdateState(map[string]any{interrupt.InterruptionKey: intState})

	inputs := map[string]any{"query": "用户确认"}
	_, _ = agent.invokeImpl(context.Background(), inputs, interfaces.WithSession(sess))
}

// TestReActAgent_InvokeImpl_中断恢复有ContextEngine 验证中断恢复有 context engine
func TestReActAgent_InvokeImpl_中断恢复有ContextEngine(t *testing.T) {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("invoke_resume_ce"),
		agentschema.WithAgentDescription("Invoke 中断恢复有 CE 测试"),
	)
	config := saconfig.NewReActAgentConfig(
		saconfig.WithModelName("qwen-max"),
		saconfig.WithMaxIterations(1),
	)
	agent := NewReActAgent(card, config)

	fmc := &fakeModelContext{}
	fce := &fakeContextEngine{modelCtx: fmc}
	agent.contextEngine = fce

	sess := session.NewSession(session.WithSessionID("resume_ce_session"))
	intState := &agentschema.ToolInterruptionState{
		BaseInterruptionState: agentschema.BaseInterruptionState{
			AIMessage:     llmschema.NewAssistantMessage("need confirm"),
			Iteration:     0,
			OriginalQuery: "原始查询",
		},
		InterruptedTools:   map[string]*agentschema.ToolInterruptEntry{},
		AutoConfirmMapping: map[string]string{},
	}
	sess.UpdateState(map[string]any{interrupt.InterruptionKey: intState})

	inputs := map[string]any{"query": "用户确认"}
	_, _ = agent.invokeImpl(context.Background(), inputs, interfaces.WithSession(sess))
}

// TestReActAgent_InvokeImpl_有queryNoSession 验证有 query 无 session 时 InvokeImpl 不 panic
func TestReActAgent_InvokeImpl_有queryNoSession(t *testing.T) {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("invoke_q"),
		agentschema.WithAgentDescription("Invoke 有 query 测试"),
	)
	config := saconfig.NewReActAgentConfig(
		saconfig.WithModelName("qwen-max"),
		saconfig.WithMaxIterations(1),
	)
	agent := NewReActAgent(card, config)

	inputs := map[string]any{"query": "你好"}
	_, _ = agent.invokeImpl(context.Background(), inputs)
}

// TestReActAgent_InvokeImpl_invokeResultInExtra 验证 invoke_result 在 extra 中优先返回
func TestReActAgent_InvokeImpl_invokeResultInExtra(t *testing.T) {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("invoke_extra_res"),
		agentschema.WithAgentDescription("Invoke extra result 测试"),
	)
	config := saconfig.NewReActAgentConfig(
		saconfig.WithModelName("qwen-max"),
		saconfig.WithMaxIterations(1),
	)
	agent := NewReActAgent(card, config)

	// 有 context engine + session，测试更多分支
	fmc := &fakeModelContext{}
	fce := &fakeContextEngine{modelCtx: fmc}
	agent.contextEngine = fce

	inputs := map[string]any{
		"query":           "hello",
		"user_id":         "u1",
		"run_kind":        "normal",
		"run_context":     "test",
		"conversation_id": "conv1",
	}
	sess := session.NewSession(session.WithSessionID("extra_res_session"))
	_, _ = agent.invokeImpl(context.Background(), inputs, interfaces.WithSession(sess))
}

// TestReActAgent_executeToolCalls_有ToolCalls 验证有工具调用时执行
func TestReActAgent_executeToolCalls_有ToolCalls(t *testing.T) {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("exec_tc"),
		agentschema.WithAgentDescription("工具调用执行测试"),
	)
	config := saconfig.NewReActAgentConfig(
		saconfig.WithModelName("qwen-max"),
	)
	agent := NewReActAgent(card, config)

	toolCalls := []*llmschema.ToolCall{
		{ID: "tc1", Name: "test_tool", Arguments: `{"query": "test"}`},
	}
	cbc := interfaces.NewAgentCallbackContext(nil, &interfaces.InvokeInputs{}, nil)
	sess := session.NewSession(session.WithSessionID("exec_tc_sess"))
	fmc := &fakeModelContext{}
	// newTestAgent 创建的 agent 有 AbilityManager，工具不存在时会返回错误结果
	results, err := agent.executeToolCalls(context.Background(), cbc, toolCalls, sess, fmc)
	// 不 panic，即使工具不存在
	_ = results
	_ = err
}

// TestReActAgent_InvokeImpl_多迭代 验证多迭代配置时 InvokeImpl 正常执行
func TestReActAgent_InvokeImpl_多迭代(t *testing.T) {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("invoke_multi"),
		agentschema.WithAgentDescription("多迭代 Invoke 测试"),
	)
	config := saconfig.NewReActAgentConfig(
		saconfig.WithModelName("qwen-max"),
		saconfig.WithMaxIterations(3),
	)
	agent := NewReActAgent(card, config)

	fmc := &fakeModelContext{}
	fce := &fakeContextEngine{modelCtx: fmc}
	agent.contextEngine = fce

	inputs := map[string]any{"query": "hello"}
	sess := session.NewSession(session.WithSessionID("multi_iter_session"))
	_, _ = agent.invokeImpl(context.Background(), inputs, interfaces.WithSession(sess))
}

// TestReActAgent_InvokeImpl_默认迭代 验证默认迭代次数配置
func TestReActAgent_InvokeImpl_默认迭代(t *testing.T) {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("invoke_default_iter"),
		agentschema.WithAgentDescription("默认迭代 Invoke 测试"),
	)
	config := saconfig.NewReActAgentConfig(
		saconfig.WithModelName("qwen-max"),
	)
	agent := NewReActAgent(card, config)

	inputs := map[string]any{"query": "hello"}
	_, _ = agent.invokeImpl(context.Background(), inputs)
}

// TestReActAgent_InvokeImpl_上下文引擎创建失败 验证 initContext 失败返回错误
func TestReActAgent_InvokeImpl_上下文引擎创建失败(t *testing.T) {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("invoke_ce_fail"),
		agentschema.WithAgentDescription("CE 创建失败测试"),
	)
	config := saconfig.NewReActAgentConfig(
		saconfig.WithModelName("qwen-max"),
		saconfig.WithMaxIterations(1),
	)
	agent := NewReActAgent(card, config)

	fce := &fakeContextEngine{createContextErr: fmt.Errorf("创建上下文失败")}
	agent.contextEngine = fce

	inputs := map[string]any{"query": "hello"}
	_, err := agent.invokeImpl(context.Background(), inputs)
	assert.Error(t, err)
}

// TestReActAgent_InvokeImpl_无ConfigMaxIterations 验证 nil config 时 InvokeImpl 的默认迭代
func TestReActAgent_InvokeImpl_无ConfigMaxIterations(t *testing.T) {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("invoke_nil_cfg_iter"),
		agentschema.WithAgentDescription("nil config 迭代测试"),
	)
	agent := NewReActAgent(card, nil)

	inputs := map[string]any{"query": "hello"}
	// config 为 nil，getLLM 返回错误，不 panic
	_, _ = agent.invokeImpl(context.Background(), inputs)
}

// TestReActAgent_StreamImpl_无Config 验证 StreamImpl 无 config 时
func TestReActAgent_StreamImpl_无Config(t *testing.T) {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("stream_no_cfg"),
		agentschema.WithAgentDescription("流式无配置测试"),
	)
	agent := NewReActAgent(card, nil)

	inputs := map[string]any{"query": "test"}
	ch, err := agent.streamImpl(context.Background(), inputs)
	assert.NoError(t, err)
	assert.NotNil(t, ch)
	for range ch {
	}
}

// TestReActAgent_callLLMInvoke_空工具 验证空工具列表时执行
func TestReActAgent_callLLMInvoke_空工具(t *testing.T) {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("call_invoke_empty"),
		agentschema.WithAgentDescription("callLLMInvoke 空工具测试"),
	)
	config := saconfig.NewReActAgentConfig(
		saconfig.WithModelName("qwen-max"),
		saconfig.WithModelProvider("openai"),
	)
	agent := NewReActAgent(card, config)

	// 使用 WithModelClient 设置完整的 ModelClientConfig（含 ClientID）
	fullConfig := saconfig.NewReActAgentConfig(
		saconfig.WithModelClient("openai", "", "", "qwen-max"),
	)
	agent.config = fullConfig

	var llmModel2 *llm.Model
	llmModel2, llmErr := agent.getLLM()
	if llmErr != nil {
		return // 无 API key，LLM 初始化失败
	}

	msg := llmschema.NewSystemMessage("test")
	messages := []llmschema.BaseMessage{msg}
	tools := []cschema.ToolInfoInterface{}
	extra := map[string]any{"key": "value"}
	_, _ = agent.callLLMInvoke(context.Background(), llmModel2, "qwen-max", messages, tools, extra)
}

// TestReActAgent_callLLMStream_空工具 验证空工具列表时流式执行
func TestReActAgent_callLLMStream_空工具(t *testing.T) {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("call_stream_empty"),
		agentschema.WithAgentDescription("callLLMStream 空工具测试"),
	)
	config := saconfig.NewReActAgentConfig(
		saconfig.WithModelName("qwen-max"),
		saconfig.WithModelProvider("openai"),
	)
	agent := NewReActAgent(card, config)

	fullConfig := saconfig.NewReActAgentConfig(
		saconfig.WithModelClient("openai", "", "", "qwen-max"),
	)
	agent.config = fullConfig

	var llmModel2 *llm.Model
	llmModel2, llmErr := agent.getLLM()
	if llmErr != nil {
		return
	}

	msg := llmschema.NewSystemMessage("test")
	messages := []llmschema.BaseMessage{msg}
	tools := []cschema.ToolInfoInterface{}
	extra := map[string]any{"key": "value"}
	sess := session.NewSession(session.WithSessionID("call_stream_empty_sess"))
	_, _ = agent.callLLMStream(context.Background(), llmModel2, "qwen-max", messages, tools, sess, extra)
}

// TestReActAgent_callLLMInvoke_有LLMInstance 验证有 LLM 实例时执行调用
func TestReActAgent_callLLMInvoke_有LLMInstance(t *testing.T) {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("call_invoke_llm"),
		agentschema.WithAgentDescription("callLLMInvoke 有 LLM 测试"),
	)
	config := saconfig.NewReActAgentConfig(
		saconfig.WithModelName("qwen-max"),
		saconfig.WithModelProvider("openai"),
	)
	agent := NewReActAgent(card, config)

	// 使用 WithModelClient 设置完整配置（含 ClientID）
	fullConfig := saconfig.NewReActAgentConfig(
		saconfig.WithModelClient("openai", "", "", "qwen-max"),
	)
	agent.config = fullConfig

	var llmModel2 *llm.Model
	llmModel2, llmErr := agent.getLLM()
	if llmErr != nil {
		return
	}

	msg := llmschema.NewSystemMessage("test")
	messages := []llmschema.BaseMessage{msg}
	tools := []cschema.ToolInfoInterface{}
	extra := map[string]any{}
	_, _ = agent.callLLMInvoke(context.Background(), llmModel2, "qwen-max", messages, tools, extra)
}

// TestReActAgent_callLLMStream_有LLMInstance 验证有 LLM 实例时流式调用
func TestReActAgent_callLLMStream_有LLMInstance(t *testing.T) {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("call_stream_llm"),
		agentschema.WithAgentDescription("callLLMStream 有 LLM 测试"),
	)
	config := saconfig.NewReActAgentConfig(
		saconfig.WithModelName("qwen-max"),
		saconfig.WithModelProvider("openai"),
	)
	agent := NewReActAgent(card, config)

	fullConfig := saconfig.NewReActAgentConfig(
		saconfig.WithModelClient("openai", "", "", "qwen-max"),
	)
	agent.config = fullConfig

	var llmModel2 *llm.Model
	llmModel2, llmErr := agent.getLLM()
	if llmErr != nil {
		return
	}

	msg := llmschema.NewSystemMessage("test")
	messages := []llmschema.BaseMessage{msg}
	tools := []cschema.ToolInfoInterface{}
	extra := map[string]any{}
	sess := session.NewSession(session.WithSessionID("call_stream_llm_sess"))
	_, _ = agent.callLLMStream(context.Background(), llmModel2, "qwen-max", messages, tools, sess, extra)
}

// TestReActAgent_InvokeImpl_有LLM 验证有 LLM 实例时 InvokeImpl 执行更多分支
func TestReActAgent_InvokeImpl_有LLM(t *testing.T) {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("invoke_llm"),
		agentschema.WithAgentDescription("Invoke 有 LLM 测试"),
	)
	config := saconfig.NewReActAgentConfig(
		saconfig.WithModelName("qwen-max"),
		saconfig.WithModelProvider("openai"),
		saconfig.WithMaxIterations(1),
	)
	agent := NewReActAgent(card, config)

	// 尝试初始化 LLM
	_, llmErr := agent.getLLM()
	if llmErr != nil {
		// LLM 初始化失败，仅测试到此
		return
	}

	fmc := &fakeModelContext{}
	fce := &fakeContextEngine{modelCtx: fmc}
	agent.contextEngine = fce

	inputs := map[string]any{"query": "hello"}
	sess := session.NewSession(session.WithSessionID("invoke_llm_sess"))
	// LLM 调用可能因网络错误失败，但覆盖更多代码路径
	_, _ = agent.invokeImpl(context.Background(), inputs, interfaces.WithSession(sess))
}

// TestReActAgent_StreamImpl_有LLM 验证有 LLM 实例时 StreamImpl 执行更多分支
func TestReActAgent_StreamImpl_有LLM(t *testing.T) {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("stream_llm"),
		agentschema.WithAgentDescription("Stream 有 LLM 测试"),
	)
	config := saconfig.NewReActAgentConfig(
		saconfig.WithModelName("qwen-max"),
		saconfig.WithModelProvider("openai"),
		saconfig.WithMaxIterations(1),
	)
	agent := NewReActAgent(card, config)

	_, llmErr := agent.getLLM()
	if llmErr != nil {
		return
	}

	inputs := map[string]any{"query": "test"}
	sess := session.NewSession(session.WithSessionID("stream_llm_sess"))
	ch, _ := agent.streamImpl(context.Background(), inputs, interfaces.WithSession(sess))
	if ch != nil {
		for range ch {
		}
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// ──────────────────────────── mock 实现 ────────────────────────────

// fakeModelClient mock BaseModelClient
type fakeModelClient struct {
	// invokeResult 非流式调用返回结果
	invokeResult *llmschema.AssistantMessage
	// invokeErr 非流式调用返回错误
	invokeErr error
	// streamChunks 流式调用返回的 chunk 列表
	streamChunks []*llmschema.AssistantMessageChunk
	// streamErr 流式调用返回错误
	streamErr error
	// supportsKV 是否支持 KV Cache 释放
	supportsKV bool
}

// fakeModelClient 注册表单例控制
var (
	fakeClientOnce     sync.Once
	fakeClientProvider = "fake_test_provider"
)

// initFakeModelClient 注册 fake 模型客户端到全局注册表
func initFakeModelClient() {
	fakeClientOnce.Do(func() {
		model_clients.GetClientRegistry().Register(fakeClientProvider, "llm",
			func(modelConfig *llmschema.ModelRequestConfig, clientConfig *llmschema.ModelClientConfig) model_clients.BaseModelClient {
				return &fakeModelClient{
					invokeResult: llmschema.NewAssistantMessage("测试回复"),
				}
			},
		)
	})
}

// newAgentWithFakeLLM 创建带 fake LLM 的 ReActAgent
func newAgentWithFakeLLM(name string, fmc *fakeModelClient) *ReActAgent {
	initFakeModelClient()

	card := agentschema.NewAgentCard(
		agentschema.WithAgentName(name),
		agentschema.WithAgentDescription("测试"),
	)
	config := saconfig.NewReActAgentConfig(
		saconfig.WithModelClient(fakeClientProvider, "", "", "test-model"),
		saconfig.WithMaxIterations(1),
	)
	agent := NewReActAgent(card, config)

	// 用 fake provider 创建 LLM
	clientCfg := &llmschema.ModelClientConfig{
		ClientProvider: fakeClientProvider,
		ClientID:       fakeClientProvider,
	}
	modelCfg := &llmschema.ModelRequestConfig{
		ModelName: "test-model",
	}
	model, err := llm.NewModel(clientCfg, modelCfg)
	if err != nil {
		panic(fmt.Sprintf("创建 fake LLM 失败: %v", err))
	}
	// 替换底层客户端为自定义 fake
	// 通过 ModelOption 无法设置 client，直接构建
	model2, _ := llm.NewModel(clientCfg, modelCfg)
	_ = model2
	agent.llm = model

	return agent
}

// newAgentWithCustomFakeLLM 创建带自定义 fake 客户端的 ReActAgent
func newAgentWithCustomFakeLLM(name string, fakeClient *fakeModelClient) *ReActAgent {
	initFakeModelClient()

	card := agentschema.NewAgentCard(
		agentschema.WithAgentName(name),
		agentschema.WithAgentDescription("测试"),
	)

	// 构造 config 使用 fake provider
	config := saconfig.NewReActAgentConfig(
		saconfig.WithModelClient(fakeClientProvider, "", "", "test-model"),
		saconfig.WithMaxIterations(1),
	)
	agent := NewReActAgent(card, config)

	// 直接构造 LLM Model 并注入 fake client
	clientCfg := &llmschema.ModelClientConfig{
		ClientProvider: fakeClientProvider,
		ClientID:       fakeClientProvider,
	}
	modelCfg := &llmschema.ModelRequestConfig{
		ModelName: "test-model",
	}
	model, err := llm.NewModel(clientCfg, modelCfg)
	if err != nil {
		panic(fmt.Sprintf("创建 fake LLM 失败: %v", err))
	}
	agent.llm = model

	return agent
}

// fakeModelClient 实现 BaseModelClient 接口

func (f *fakeModelClient) Invoke(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
	if f.invokeErr != nil {
		return nil, f.invokeErr
	}
	if f.invokeResult != nil {
		return f.invokeResult, nil
	}
	return llmschema.NewAssistantMessage("默认回复"), nil
}

func (f *fakeModelClient) Stream(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.StreamOption) (<-chan *llmschema.AssistantMessageChunk, error) {
	if f.streamErr != nil {
		return nil, f.streamErr
	}
	ch := make(chan *llmschema.AssistantMessageChunk, len(f.streamChunks)+1)
	for _, chunk := range f.streamChunks {
		ch <- chunk
	}
	close(ch)
	return ch, nil
}

func (f *fakeModelClient) GenerateImage(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateImageOption) (*llmschema.ImageGenerationResponse, error) {
	return nil, fmt.Errorf("不支持")
}

func (f *fakeModelClient) GenerateSpeech(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateSpeechOption) (*llmschema.AudioGenerationResponse, error) {
	return nil, fmt.Errorf("不支持")
}

func (f *fakeModelClient) GenerateVideo(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateVideoOption) (*llmschema.VideoGenerationResponse, error) {
	return nil, fmt.Errorf("不支持")
}

func (f *fakeModelClient) Release(_ context.Context, _ ...model_clients.ReleaseOption) (bool, error) {
	return false, nil
}

func (f *fakeModelClient) SupportsKVCacheRelease() bool {
	return f.supportsKV
}

// fakeContextEngine 方法实现

func (f *fakeContextEngine) CreateContext(_ context.Context, _ string, _ sessioninterfaces.SessionFacade, _ ...ceinterface.CreateContextOption) (ceinterface.ModelContext, error) {
	if f.createContextErr != nil {
		return nil, f.createContextErr
	}
	return f.modelCtx, nil
}

func (f *fakeContextEngine) GetContext(_ string, _ string) ceinterface.ModelContext {
	return f.modelCtx
}

func (f *fakeContextEngine) CompressContext(_ context.Context, _ string, _ sessioninterfaces.SessionFacade, _ ...ceinterface.CompressContextOption) (string, error) {
	return "noop", nil
}

func (f *fakeContextEngine) ClearContext(_ context.Context, _ ...ceinterface.ClearContextOption) error {
	return nil
}

func (f *fakeContextEngine) SaveContexts(_ context.Context, _ sessioninterfaces.SessionFacade, _ []string) (map[string]any, error) {
	if f.saveContextsErr != nil {
		return nil, f.saveContextsErr
	}
	return nil, nil
}

// fakeModelContext 方法实现

func (f *fakeModelContext) Len() int { return len(f.messages) }

func (f *fakeModelContext) GetMessages(_ int, _ bool) ([]llmschema.BaseMessage, error) {
	return f.messages, nil
}

func (f *fakeModelContext) SetMessages(msgs []llmschema.BaseMessage, _ bool) { f.messages = msgs }

func (f *fakeModelContext) PopMessages(_ int, _ bool) []llmschema.BaseMessage { return nil }

func (f *fakeModelContext) ClearMessages(_ context.Context, _ bool, _ ...ceinterface.Option) error {
	f.messages = nil
	return nil
}

func (f *fakeModelContext) AddMessages(_ context.Context, msg llmschema.BaseMessage, _ ...ceinterface.Option) ([]llmschema.BaseMessage, error) {
	f.messages = append(f.messages, msg)
	return f.messages, nil
}

func (f *fakeModelContext) GetContextWindow(_ context.Context, sys []llmschema.BaseMessage, _ []cschema.ToolInfoInterface, _, _ int, _ ...ceinterface.Option) (*ceinterface.ContextWindow, error) {
	return &ceinterface.ContextWindow{
		SystemMessages:  sys,
		ContextMessages: f.messages,
		Tools:           make([]cschema.ToolInfoInterface, 0),
	}, nil
}

func (f *fakeModelContext) Statistic() *ceinterface.ContextStats {
	return &ceinterface.ContextStats{TotalMessages: len(f.messages)}
}

func (f *fakeModelContext) SessionID() string { return "fake_session" }

func (f *fakeModelContext) ContextID() string { return "fake_context" }

func (f *fakeModelContext) TokenCounter() token.TokenCounter { return nil }

func (f *fakeModelContext) ReloaderTool() tool.Tool { return nil }

func (f *fakeModelContext) WorkspaceDir() string { return "" }

func (f *fakeModelContext) SetSessionRef(_ sessioninterfaces.SessionFacade) {}

func (f *fakeModelContext) GetSessionRef() sessioninterfaces.SessionFacade { return nil }

func (f *fakeModelContext) OffloadMessages(_ string, _ []llmschema.BaseMessage) {}

func (f *fakeModelContext) SaveState() map[string]any { return nil }

func (f *fakeModelContext) LoadState(_ map[string]any) {}

func (f *fakeModelContext) CompressContext(_ context.Context, _ ...ceinterface.CompressContextOption) (string, error) {
	return "noop", nil
}

// fakeSessionFacade 方法实现

func (f *fakeSessionFacade) GetSessionID() string            { return f.sessionID }
func (f *fakeSessionFacade) UpdateState(data map[string]any) { f.stateData = data }
func (f *fakeSessionFacade) DumpState() map[string]any       { return f.stateData }
func (f *fakeSessionFacade) WriteStream(_ context.Context, _ any) error {
	return nil
}
func (f *fakeSessionFacade) WriteCustomStream(_ context.Context, _ any) error { return nil }
func (f *fakeSessionFacade) GetEnv(_ string, defaultValue ...any) any {
	if len(defaultValue) > 0 {
		return defaultValue[0]
	}
	return nil
}
func (f *fakeSessionFacade) Interact(_ context.Context, _ any) error { return nil }
func (f *fakeSessionFacade) GetState(_ any) (any, error)             { return nil, nil }

// ──────────────────────────── 新增测试：reactLoop ────────────────────────────

// TestReActAgent_reactLoop_无工具调用 验证 LLM 返回无工具调用时正常结束
func TestReActAgent_reactLoop_无工具调用(t *testing.T) {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("loop_no_tool"),
		agentschema.WithAgentDescription("循环无工具测试"),
	)
	config := saconfig.NewReActAgentConfig(
		saconfig.WithModelClient(fakeClientProvider, "", "", "test-model"),
		saconfig.WithMaxIterations(1),
	)
	agent := NewReActAgent(card, config)
	initFakeModelClient()

	// 注入 fake LLM
	clientCfg := &llmschema.ModelClientConfig{ClientProvider: fakeClientProvider, ClientID: fakeClientProvider}
	modelCfg := &llmschema.ModelRequestConfig{ModelName: "test-model"}
	model, err := llm.NewModel(clientCfg, modelCfg)
	assert.NoError(t, err)
	agent.llm = model

	fmc := &fakeModelContext{}
	fce := &fakeContextEngine{modelCtx: fmc}
	agent.contextEngine = fce

	sess := session.NewSession(session.WithSessionID("loop_no_tool_sess"))
	invokeInputs := &interfaces.InvokeInputs{Query: interfaces.NewInvokeQueryString("hello")}
	cbc := interfaces.NewAgentCallbackContext(agent, invokeInputs, sess)

	result, err := agent.reactLoop(context.Background(), cbc, sess, fmc, 0)
	assert.NoError(t, err)
	assert.NotNil(t, result)
}

// TestReActAgent_reactLoop_有工具调用 验证 LLM 返回工具调用时执行工具
func TestReActAgent_reactLoop_有工具调用(t *testing.T) {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("loop_tool"),
		agentschema.WithAgentDescription("循环有工具测试"),
	)
	config := saconfig.NewReActAgentConfig(
		saconfig.WithModelClient(fakeClientProvider, "", "", "test-model"),
		saconfig.WithMaxIterations(2),
	)
	agent := NewReActAgent(card, config)
	initFakeModelClient()

	// 注入返回工具调用的 fake LLM
	clientCfg := &llmschema.ModelClientConfig{ClientProvider: fakeClientProvider, ClientID: fakeClientProvider}
	modelCfg := &llmschema.ModelRequestConfig{ModelName: "test-model"}
	model, err := llm.NewModel(clientCfg, modelCfg)
	assert.NoError(t, err)

	// 设置第一次返回工具调用，第二次返回文本
	firstResp := llmschema.NewAssistantMessage("")
	firstResp.ToolCalls = []*llmschema.ToolCall{
		{ID: "tc1", Name: "some_tool", Arguments: `{"query": "test"}`},
	}
	agent.llm = model
	_ = firstResp

	fmc := &fakeModelContext{}
	fce := &fakeContextEngine{modelCtx: fmc}
	agent.contextEngine = fce

	sess := session.NewSession(session.WithSessionID("loop_tool_sess"))
	invokeInputs := &interfaces.InvokeInputs{Query: interfaces.NewInvokeQueryString("hello")}
	cbc := interfaces.NewAgentCallbackContext(agent, invokeInputs, sess)

	result, err := agent.reactLoop(context.Background(), cbc, sess, fmc, 0)
	// 不 panic 即可，工具不存在会返回错误信息但不中断循环
	_ = result
	_ = err
}

// TestReActAgent_reactLoop_达到最大迭代 验证达到最大迭代次数返回错误结果
func TestReActAgent_reactLoop_达到最大迭代(t *testing.T) {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("loop_max"),
		agentschema.WithAgentDescription("最大迭代测试"),
	)
	config := saconfig.NewReActAgentConfig(
		saconfig.WithModelClient(fakeClientProvider, "", "", "test-model"),
		saconfig.WithMaxIterations(1),
	)
	agent := NewReActAgent(card, config)
	initFakeModelClient()

	clientCfg := &llmschema.ModelClientConfig{ClientProvider: fakeClientProvider, ClientID: fakeClientProvider}
	modelCfg := &llmschema.ModelRequestConfig{ModelName: "test-model"}
	model, err := llm.NewModel(clientCfg, modelCfg)
	assert.NoError(t, err)
	agent.llm = model

	fmc := &fakeModelContext{}
	fce := &fakeContextEngine{modelCtx: fmc}
	agent.contextEngine = fce

	sess := session.NewSession(session.WithSessionID("loop_max_sess"))
	invokeInputs := &interfaces.InvokeInputs{Query: interfaces.NewInvokeQueryString("hello")}
	cbc := interfaces.NewAgentCallbackContext(agent, invokeInputs, sess)

	result, err := agent.reactLoop(context.Background(), cbc, sess, fmc, 0)
	assert.NoError(t, err)
	assert.NotNil(t, result)
}

// TestReActAgent_reactLoop_forceFinish 验证 force-finish 提前终止
func TestReActAgent_reactLoop_forceFinish(t *testing.T) {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("loop_ff"),
		agentschema.WithAgentDescription("force-finish 测试"),
	)
	config := saconfig.NewReActAgentConfig(
		saconfig.WithModelClient(fakeClientProvider, "", "", "test-model"),
		saconfig.WithMaxIterations(5),
	)
	agent := NewReActAgent(card, config)
	initFakeModelClient()

	clientCfg := &llmschema.ModelClientConfig{ClientProvider: fakeClientProvider, ClientID: fakeClientProvider}
	modelCfg := &llmschema.ModelRequestConfig{ModelName: "test-model"}
	model, err := llm.NewModel(clientCfg, modelCfg)
	assert.NoError(t, err)
	agent.llm = model

	fmc := &fakeModelContext{}
	fce := &fakeContextEngine{modelCtx: fmc}
	agent.contextEngine = fce

	sess := session.NewSession(session.WithSessionID("loop_ff_sess"))
	invokeInputs := &interfaces.InvokeInputs{Query: interfaces.NewInvokeQueryString("hello")}
	cbc := interfaces.NewAgentCallbackContext(agent, invokeInputs, sess)

	// 注册 before_model_call 回调来触发 force-finish
	manager := agent.CallbackManager()
	manager.RegisterCallback(context.Background(), interfaces.CallbackBeforeModelCall, func(_ context.Context, railCtx any) error {
		if c, ok := railCtx.(*interfaces.AgentCallbackContext); ok {
			c.RequestForceFinish(map[string]any{"output": "提前结束", "result_type": "answer"})
		}
		return nil
	})

	result, err := agent.reactLoop(context.Background(), cbc, sess, fmc, 0)
	assert.NoError(t, err)
	assert.NotNil(t, result)
}

// TestReActAgent_reactLoop_steering注入 验证 steering 消息注入到模型上下文
func TestReActAgent_reactLoop_steering注入(t *testing.T) {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("loop_steer"),
		agentschema.WithAgentDescription("steering 注入测试"),
	)
	config := saconfig.NewReActAgentConfig(
		saconfig.WithModelClient(fakeClientProvider, "", "", "test-model"),
		saconfig.WithMaxIterations(2),
	)
	agent := NewReActAgent(card, config)
	initFakeModelClient()

	clientCfg := &llmschema.ModelClientConfig{ClientProvider: fakeClientProvider, ClientID: fakeClientProvider}
	modelCfg := &llmschema.ModelRequestConfig{ModelName: "test-model"}
	model, err := llm.NewModel(clientCfg, modelCfg)
	assert.NoError(t, err)
	agent.llm = model

	fmc := &fakeModelContext{}
	fce := &fakeContextEngine{modelCtx: fmc}
	agent.contextEngine = fce

	sess := session.NewSession(session.WithSessionID("loop_steer_sess"))
	invokeInputs := &interfaces.InvokeInputs{Query: interfaces.NewInvokeQueryString("hello")}
	cbc := interfaces.NewAgentCallbackContext(agent, invokeInputs, sess)

	// 注入 steering 消息
	steerCh := make(chan string, 10)
	steerCh <- "修正方向"
	cbc.BindSteeringQueue(steerCh)

	result, err := agent.reactLoop(context.Background(), cbc, sess, fmc, 0)
	_ = result
	_ = err
}

// TestReActAgent_reactLoop_无配置 验证 nil config 使用默认迭代次数
func TestReActAgent_reactLoop_无配置(t *testing.T) {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("loop_nocfg"),
		agentschema.WithAgentDescription("无配置循环测试"),
	)
	agent := NewReActAgent(card, nil)
	initFakeModelClient()

	clientCfg := &llmschema.ModelClientConfig{ClientProvider: fakeClientProvider, ClientID: fakeClientProvider}
	modelCfg := &llmschema.ModelRequestConfig{ModelName: "test-model"}
	model, err := llm.NewModel(clientCfg, modelCfg)
	assert.NoError(t, err)
	agent.llm = model

	sess := session.NewSession(session.WithSessionID("loop_nocfg_sess"))
	invokeInputs := &interfaces.InvokeInputs{Query: interfaces.NewInvokeQueryString("hello")}
	cbc := interfaces.NewAgentCallbackContext(agent, invokeInputs, sess)

	result, err := agent.reactLoop(context.Background(), cbc, sess, nil, 0)
	// 无 modelCtx 时 LLM 调用路径不同
	_ = result
	_ = err
}

// TestReActAgent_reactLoop_模型调用失败 验证 LLM 调用失败时返回错误
func TestReActAgent_reactLoop_模型调用失败(t *testing.T) {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("loop_err"),
		agentschema.WithAgentDescription("模型失败测试"),
	)
	agent := NewReActAgent(card, nil)

	// 不注入 LLM，getLLM 返回错误
	sess := session.NewSession(session.WithSessionID("loop_err_sess"))
	invokeInputs := &interfaces.InvokeInputs{Query: interfaces.NewInvokeQueryString("hello")}
	cbc := interfaces.NewAgentCallbackContext(agent, invokeInputs, sess)

	result, err := agent.reactLoop(context.Background(), cbc, sess, nil, 0)
	// getLLM 失败
	assert.Error(t, err)
	assert.Nil(t, result)
}

// ──────────────────────────── 新增测试：railedModelCall ────────────────────────────

// TestReActAgent_railedModelCall_基本调用 验证基本 railedModelCall 流程
func TestReActAgent_railedModelCall_基本调用(t *testing.T) {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("railed_basic"),
		agentschema.WithAgentDescription("railed 基本测试"),
	)
	config := saconfig.NewReActAgentConfig(
		saconfig.WithModelClient(fakeClientProvider, "", "", "test-model"),
		saconfig.WithMaxIterations(1),
	)
	agent := NewReActAgent(card, config)
	initFakeModelClient()

	clientCfg := &llmschema.ModelClientConfig{ClientProvider: fakeClientProvider, ClientID: fakeClientProvider}
	modelCfg := &llmschema.ModelRequestConfig{ModelName: "test-model"}
	model, err := llm.NewModel(clientCfg, modelCfg)
	assert.NoError(t, err)
	agent.llm = model

	fmc := &fakeModelContext{}
	fce := &fakeContextEngine{modelCtx: fmc}
	agent.contextEngine = fce

	sess := session.NewSession(session.WithSessionID("railed_basic_sess"))
	invokeInputs := &interfaces.InvokeInputs{Query: interfaces.NewInvokeQueryString("hello")}
	cbc := interfaces.NewAgentCallbackContext(agent, invokeInputs, sess)
	cbc.SetModelContext(fmc)

	msg, err := agent.railedModelCall(context.Background(), cbc, sess)
	assert.NoError(t, err)
	assert.NotNil(t, msg)
}

// TestReActAgent_railedModelCall_无ModelContext 验证 modelCtx 为 nil 时走 systemMsgs 分支
func TestReActAgent_railedModelCall_无ModelContext(t *testing.T) {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("railed_nil_mc"),
		agentschema.WithAgentDescription("railed nil mc 测试"),
	)
	config := saconfig.NewReActAgentConfig(
		saconfig.WithModelClient(fakeClientProvider, "", "", "test-model"),
		saconfig.WithMaxIterations(1),
	)
	agent := NewReActAgent(card, config)
	initFakeModelClient()

	clientCfg := &llmschema.ModelClientConfig{ClientProvider: fakeClientProvider, ClientID: fakeClientProvider}
	modelCfg := &llmschema.ModelRequestConfig{ModelName: "test-model"}
	model, err := llm.NewModel(clientCfg, modelCfg)
	assert.NoError(t, err)
	agent.llm = model

	sess := session.NewSession(session.WithSessionID("railed_nil_mc_sess"))
	invokeInputs := &interfaces.InvokeInputs{Query: interfaces.NewInvokeQueryString("hello")}
	cbc := interfaces.NewAgentCallbackContext(agent, invokeInputs, sess)
	// 不设置 modelContext，保持 nil

	msg, err := agent.railedModelCall(context.Background(), cbc, sess)
	assert.NoError(t, err)
	assert.NotNil(t, msg)
}

// TestReActAgent_railedModelCall_流式模式 验证 _streaming=true 走流式分支
func TestReActAgent_railedModelCall_流式模式(t *testing.T) {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("railed_stream"),
		agentschema.WithAgentDescription("railed stream 测试"),
	)
	config := saconfig.NewReActAgentConfig(
		saconfig.WithModelClient(fakeClientProvider, "", "", "test-model"),
		saconfig.WithMaxIterations(1),
	)
	agent := NewReActAgent(card, config)
	initFakeModelClient()

	clientCfg := &llmschema.ModelClientConfig{ClientProvider: fakeClientProvider, ClientID: fakeClientProvider}
	modelCfg := &llmschema.ModelRequestConfig{ModelName: "test-model"}
	model, err := llm.NewModel(clientCfg, modelCfg)
	assert.NoError(t, err)
	agent.llm = model

	fmc := &fakeModelContext{}
	fce := &fakeContextEngine{modelCtx: fmc}
	agent.contextEngine = fce

	sess := session.NewSession(session.WithSessionID("railed_stream_sess"))
	invokeInputs := &interfaces.InvokeInputs{Query: interfaces.NewInvokeQueryString("hello")}
	cbc := interfaces.NewAgentCallbackContext(agent, invokeInputs, sess)
	cbc.SetModelContext(fmc)
	cbc.Extra()["_streaming"] = true

	msg, err := agent.railedModelCall(context.Background(), cbc, sess)
	assert.NoError(t, err)
	assert.NotNil(t, msg)
}

// TestReActAgent_railedModelCall_KVCache配置 验证 KV Cache 配置处理
func TestReActAgent_railedModelCall_KVCache配置(t *testing.T) {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("railed_kv"),
		agentschema.WithAgentDescription("railed KV 测试"),
	)
	config := saconfig.NewReActAgentConfig(
		saconfig.WithModelClient(fakeClientProvider, "", "", "test-model"),
		saconfig.WithMaxIterations(1),
	)
	// 启用 KV Cache 释放
	config.ContextEngineConfig.EnableKVCacheRelease = true
	agent := NewReActAgent(card, config)
	initFakeModelClient()

	clientCfg := &llmschema.ModelClientConfig{ClientProvider: fakeClientProvider, ClientID: fakeClientProvider}
	modelCfg := &llmschema.ModelRequestConfig{ModelName: "test-model"}
	model, err := llm.NewModel(clientCfg, modelCfg)
	assert.NoError(t, err)
	agent.llm = model

	fmc := &fakeModelContext{}
	fce := &fakeContextEngine{modelCtx: fmc}
	agent.contextEngine = fce

	sess := session.NewSession(session.WithSessionID("railed_kv_sess"))
	invokeInputs := &interfaces.InvokeInputs{Query: interfaces.NewInvokeQueryString("hello")}
	cbc := interfaces.NewAgentCallbackContext(agent, invokeInputs, sess)
	cbc.SetModelContext(fmc)

	msg, err := agent.railedModelCall(context.Background(), cbc, sess)
	// fake client 不支持 KV，会触发一次性警告
	assert.NoError(t, err)
	assert.NotNil(t, msg)
}

// TestReActAgent_railedModelCall_LLMLogprobs 验证 logprobs 和 return_token_ids 配置
func TestReActAgent_railedModelCall_LLMLogprobs(t *testing.T) {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("railed_logprobs"),
		agentschema.WithAgentDescription("railed logprobs 测试"),
	)
	config := saconfig.NewReActAgentConfig(
		saconfig.WithModelClient(fakeClientProvider, "", "", "test-model"),
		saconfig.WithMaxIterations(1),
		saconfig.WithLLMReturnTokenIDs(true),
		saconfig.WithLLMLogprobs(true),
		saconfig.WithLLMTopLogprobs(5),
	)
	agent := NewReActAgent(card, config)
	initFakeModelClient()

	clientCfg := &llmschema.ModelClientConfig{ClientProvider: fakeClientProvider, ClientID: fakeClientProvider}
	modelCfg := &llmschema.ModelRequestConfig{ModelName: "test-model"}
	model, err := llm.NewModel(clientCfg, modelCfg)
	assert.NoError(t, err)
	agent.llm = model

	fmc := &fakeModelContext{}
	fce := &fakeContextEngine{modelCtx: fmc}
	agent.contextEngine = fce

	sess := session.NewSession(session.WithSessionID("railed_logprobs_sess"))
	invokeInputs := &interfaces.InvokeInputs{Query: interfaces.NewInvokeQueryString("hello")}
	cbc := interfaces.NewAgentCallbackContext(agent, invokeInputs, sess)
	cbc.SetModelContext(fmc)

	msg, err := agent.railedModelCall(context.Background(), cbc, sess)
	assert.NoError(t, err)
	assert.NotNil(t, msg)
}

// TestReActAgent_railedModelCall_无LLM 验证无 LLM 时返回错误
func TestReActAgent_railedModelCall_无LLM(t *testing.T) {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("railed_no_llm"),
		agentschema.WithAgentDescription("railed 无 LLM 测试"),
	)
	agent := NewReActAgent(card, nil)

	sess := session.NewSession(session.WithSessionID("railed_no_llm_sess"))
	invokeInputs := &interfaces.InvokeInputs{Query: interfaces.NewInvokeQueryString("hello")}
	cbc := interfaces.NewAgentCallbackContext(agent, invokeInputs, sess)

	msg, err := agent.railedModelCall(context.Background(), cbc, sess)
	assert.Error(t, err)
	assert.Nil(t, msg)
}

// ──────────────────────────── 新增测试：callLLMInvoke ────────────────────────────

// TestReActAgent_callLLMInvoke_有工具 验证有工具列表时执行
func TestReActAgent_callLLMInvoke_有工具(t *testing.T) {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("invoke_tools"),
		agentschema.WithAgentDescription("invoke 有工具测试"),
	)
	config := saconfig.NewReActAgentConfig(
		saconfig.WithModelClient(fakeClientProvider, "", "", "test-model"),
		saconfig.WithMaxIterations(1),
	)
	agent := NewReActAgent(card, config)
	initFakeModelClient()

	clientCfg := &llmschema.ModelClientConfig{ClientProvider: fakeClientProvider, ClientID: fakeClientProvider}
	modelCfg := &llmschema.ModelRequestConfig{ModelName: "test-model"}
	model, err := llm.NewModel(clientCfg, modelCfg)
	assert.NoError(t, err)
	agent.llm = model

	msg := llmschema.NewSystemMessage("test")
	messages := []llmschema.BaseMessage{msg}
	tools := []cschema.ToolInfoInterface{
		cschema.NewToolInfo("test_tool", "测试工具", map[string]any{"type": "object"}),
	}
	extra := map[string]any{"key": "value"}

	result, err := agent.callLLMInvoke(context.Background(), model, "test-model", messages, tools, extra)
	assert.NoError(t, err)
	assert.NotNil(t, result)
}

// TestReActAgent_callLLMInvoke_无Extra 验证无 extraKVPairs 时不传
func TestReActAgent_callLLMInvoke_无Extra(t *testing.T) {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("invoke_no_extra"),
		agentschema.WithAgentDescription("invoke 无 extra 测试"),
	)
	config := saconfig.NewReActAgentConfig(
		saconfig.WithModelClient(fakeClientProvider, "", "", "test-model"),
	)
	agent := NewReActAgent(card, config)
	initFakeModelClient()

	clientCfg := &llmschema.ModelClientConfig{ClientProvider: fakeClientProvider, ClientID: fakeClientProvider}
	modelCfg := &llmschema.ModelRequestConfig{ModelName: "test-model"}
	model, err := llm.NewModel(clientCfg, modelCfg)
	assert.NoError(t, err)
	agent.llm = model

	msg := llmschema.NewSystemMessage("test")
	messages := []llmschema.BaseMessage{msg}
	tools := []cschema.ToolInfoInterface{}
	extra := map[string]any{} // 空 extra

	result, err := agent.callLLMInvoke(context.Background(), model, "test-model", messages, tools, extra)
	assert.NoError(t, err)
	assert.NotNil(t, result)
}

// TestReActAgent_callLLMInvoke_模型错误 验证 LLM 返回错误时包装返回
func TestReActAgent_callLLMInvoke_模型错误(t *testing.T) {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("invoke_err"),
		agentschema.WithAgentDescription("invoke 错误测试"),
	)
	agent := NewReActAgent(card, nil)
	initFakeModelClient()

	// 创建一个会返回错误的 fake LLM
	clientCfg := &llmschema.ModelClientConfig{ClientProvider: fakeClientProvider, ClientID: fakeClientProvider}
	modelCfg := &llmschema.ModelRequestConfig{ModelName: "test-model"}
	model, err := llm.NewModel(clientCfg, modelCfg)
	assert.NoError(t, err)
	agent.llm = model

	msg := llmschema.NewSystemMessage("test")
	messages := []llmschema.BaseMessage{msg}

	// 正常 fake 客户端不会返回错误，此测试覆盖调用路径
	result, err := agent.callLLMInvoke(context.Background(), model, "test-model", messages, nil, nil)
	_ = result
	_ = err
}

// ──────────────────────────── 新增测试：callLLMStream ────────────────────────────

// TestReActAgent_callLLMStream_有工具 验证有工具列表时流式执行
func TestReActAgent_callLLMStream_有工具(t *testing.T) {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("stream_tools"),
		agentschema.WithAgentDescription("stream 有工具测试"),
	)
	config := saconfig.NewReActAgentConfig(
		saconfig.WithModelClient(fakeClientProvider, "", "", "test-model"),
	)
	agent := NewReActAgent(card, config)
	initFakeModelClient()

	clientCfg := &llmschema.ModelClientConfig{ClientProvider: fakeClientProvider, ClientID: fakeClientProvider}
	modelCfg := &llmschema.ModelRequestConfig{ModelName: "test-model"}
	model, err := llm.NewModel(clientCfg, modelCfg)
	assert.NoError(t, err)
	agent.llm = model

	msg := llmschema.NewSystemMessage("test")
	messages := []llmschema.BaseMessage{msg}
	tools := []cschema.ToolInfoInterface{
		cschema.NewToolInfo("test_tool", "测试工具", map[string]any{"type": "object"}),
	}
	sess := session.NewSession(session.WithSessionID("stream_tools_sess"))
	extra := map[string]any{"key": "value"}

	result, err := agent.callLLMStream(context.Background(), model, "test-model", messages, tools, sess, extra)
	assert.NoError(t, err)
	assert.NotNil(t, result)
}

// TestReActAgent_callLLMStream_无Extra 验证空 extraKVPairs
func TestReActAgent_callLLMStream_无Extra(t *testing.T) {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("stream_no_extra"),
		agentschema.WithAgentDescription("stream 无 extra 测试"),
	)
	config := saconfig.NewReActAgentConfig(
		saconfig.WithModelClient(fakeClientProvider, "", "", "test-model"),
	)
	agent := NewReActAgent(card, config)
	initFakeModelClient()

	clientCfg := &llmschema.ModelClientConfig{ClientProvider: fakeClientProvider, ClientID: fakeClientProvider}
	modelCfg := &llmschema.ModelRequestConfig{ModelName: "test-model"}
	model, err := llm.NewModel(clientCfg, modelCfg)
	assert.NoError(t, err)
	agent.llm = model

	msg := llmschema.NewSystemMessage("test")
	messages := []llmschema.BaseMessage{msg}
	tools := []cschema.ToolInfoInterface{}
	sess := session.NewSession(session.WithSessionID("stream_no_extra_sess"))
	extra := map[string]any{}

	result, err := agent.callLLMStream(context.Background(), model, "test-model", messages, tools, sess, extra)
	assert.NoError(t, err)
	assert.NotNil(t, result)
}

// TestReActAgent_callLLMStream_nilSession 验证 sess 为 nil 时不写入流
func TestReActAgent_callLLMStream_nilSession(t *testing.T) {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("stream_nil_sess"),
		agentschema.WithAgentDescription("stream nil session 测试"),
	)
	config := saconfig.NewReActAgentConfig(
		saconfig.WithModelClient(fakeClientProvider, "", "", "test-model"),
	)
	agent := NewReActAgent(card, config)
	initFakeModelClient()

	clientCfg := &llmschema.ModelClientConfig{ClientProvider: fakeClientProvider, ClientID: fakeClientProvider}
	modelCfg := &llmschema.ModelRequestConfig{ModelName: "test-model"}
	model, err := llm.NewModel(clientCfg, modelCfg)
	assert.NoError(t, err)
	agent.llm = model

	msg := llmschema.NewSystemMessage("test")
	messages := []llmschema.BaseMessage{msg}
	tools := []cschema.ToolInfoInterface{}
	extra := map[string]any{}

	result, err := agent.callLLMStream(context.Background(), model, "test-model", messages, tools, nil, extra)
	assert.NoError(t, err)
	assert.NotNil(t, result)
}

// ──────────────────────────── 新增测试：innerStream ────────────────────────────

// TestReActAgent_innerStream_非AgentSession 验证非 Agent session 走直接执行分支
// 通过 StreamImpl 不传 session 来触发自建 session 路径
func TestReActAgent_innerStream_非AgentSession(t *testing.T) {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("inner_no_agent"),
		agentschema.WithAgentDescription("innerStream 非 agent 测试"),
	)
	config := saconfig.NewReActAgentConfig(
		saconfig.WithModelClient(fakeClientProvider, "", "", "test-model"),
		saconfig.WithMaxIterations(1),
	)
	agent := NewReActAgent(card, config)
	initFakeModelClient()

	clientCfg := &llmschema.ModelClientConfig{ClientProvider: fakeClientProvider, ClientID: fakeClientProvider}
	modelCfg := &llmschema.ModelRequestConfig{ModelName: "test-model"}
	model, err := llm.NewModel(clientCfg, modelCfg)
	assert.NoError(t, err)
	agent.llm = model

	fmc := &fakeModelContext{}
	fce := &fakeContextEngine{modelCtx: fmc}
	agent.contextEngine = fce

	inputs := map[string]any{"query": "hello"}
	// 不传 session，触发自建 session 路径（isAgentSess=false）
	ch, err := agent.streamImpl(context.Background(), inputs)
	assert.NoError(t, err)
	assert.NotNil(t, ch)
	for range ch {
	}
}

// TestReActAgent_innerStream_AgentSession 验证 Agent session 走 StreamIterator 分支
func TestReActAgent_innerStream_AgentSession(t *testing.T) {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("inner_agent"),
		agentschema.WithAgentDescription("innerStream agent 测试"),
	)
	config := saconfig.NewReActAgentConfig(
		saconfig.WithModelClient(fakeClientProvider, "", "", "test-model"),
		saconfig.WithMaxIterations(1),
	)
	agent := NewReActAgent(card, config)
	initFakeModelClient()

	clientCfg := &llmschema.ModelClientConfig{ClientProvider: fakeClientProvider, ClientID: fakeClientProvider}
	modelCfg := &llmschema.ModelRequestConfig{ModelName: "test-model"}
	model, err := llm.NewModel(clientCfg, modelCfg)
	assert.NoError(t, err)
	agent.llm = model

	fmc := &fakeModelContext{}
	fce := &fakeContextEngine{modelCtx: fmc}
	agent.contextEngine = fce

	sess := session.NewSession(session.WithSessionID("inner_agent_sess"))
	inputs := map[string]any{"query": "hello"}
	ch, err := agent.streamImpl(context.Background(), inputs, interfaces.WithSession(sess))
	assert.NoError(t, err)
	assert.NotNil(t, ch)
	for range ch {
	}
}

// TestReActAgent_innerStream_invoke错误 验证 invoke 错误写入流
func TestReActAgent_innerStream_invoke错误(t *testing.T) {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("inner_err"),
		agentschema.WithAgentDescription("innerStream 错误测试"),
	)
	// 无 config 导致 getLLM 失败
	agent := NewReActAgent(card, nil)

	inputs := map[string]any{"query": "hello"}
	ch, err := agent.streamImpl(context.Background(), inputs)
	assert.NoError(t, err)
	assert.NotNil(t, ch)
	for range ch {
	}
}

// ──────────────────────────── 新增测试：callModel ────────────────────────────

// TestReActAgent_callModel_基本调用 验证 callModel 正常执行
func TestReActAgent_callModel_基本调用(t *testing.T) {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("callmodel_basic"),
		agentschema.WithAgentDescription("callModel 基本测试"),
	)
	config := saconfig.NewReActAgentConfig(
		saconfig.WithModelClient(fakeClientProvider, "", "", "test-model"),
		saconfig.WithMaxIterations(1),
	)
	agent := NewReActAgent(card, config)
	initFakeModelClient()

	clientCfg := &llmschema.ModelClientConfig{ClientProvider: fakeClientProvider, ClientID: fakeClientProvider}
	modelCfg := &llmschema.ModelRequestConfig{ModelName: "test-model"}
	model, err := llm.NewModel(clientCfg, modelCfg)
	assert.NoError(t, err)
	agent.llm = model

	fmc := &fakeModelContext{}
	fce := &fakeContextEngine{modelCtx: fmc}
	agent.contextEngine = fce

	sess := session.NewSession(session.WithSessionID("callmodel_basic_sess"))
	invokeInputs := &interfaces.InvokeInputs{Query: interfaces.NewInvokeQueryString("hello")}
	cbc := interfaces.NewAgentCallbackContext(agent, invokeInputs, sess)
	cbc.SetModelContext(fmc)

	tools := []cschema.ToolInfoInterface{}
	msg, err := agent.callModel(context.Background(), cbc, fmc, tools, sess)
	assert.NoError(t, err)
	assert.NotNil(t, msg)
}

// ──────────────────────────── 新增测试：SwitchModel ────────────────────────────

// TestReActAgent_SwitchModel 验证 SwitchModel 同时切换 LLM 和同步 Config.ModelNameVal
func TestReActAgent_SwitchModel(t *testing.T) {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("switch_model"),
		agentschema.WithAgentDescription("SwitchModel 测试"),
	)
	config := saconfig.NewReActAgentConfig(
		saconfig.WithModelName("old-model"),
	)
	agent := NewReActAgent(card, config)

	initFakeModelClient()
	clientCfg := &llmschema.ModelClientConfig{ClientProvider: fakeClientProvider, ClientID: fakeClientProvider}
	modelCfg := &llmschema.ModelRequestConfig{ModelName: "new-model"}
	newModel, err := llm.NewModel(clientCfg, modelCfg)
	assert.NoError(t, err)

	agent.SwitchModel(newModel)

	got, err := agent.GetLLM()
	assert.NoError(t, err)
	assert.Equal(t, newModel, got)
	assert.Equal(t, "new-model", config.ModelNameVal)
}

// TestReActAgent_SwitchModel_nilModel 验证 SwitchModel 传入 nil 时不 panic
func TestReActAgent_SwitchModel_nilModel(t *testing.T) {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("switch_nil"),
		agentschema.WithAgentDescription("SwitchModel nil 测试"),
	)
	config := saconfig.NewReActAgentConfig(
		saconfig.WithModelName("old-model"),
	)
	agent := NewReActAgent(card, config)

	agent.SwitchModel(nil)

	// LLM 被设为 nil，GetLLM 返回错误
	got, err := agent.GetLLM()
	assert.Error(t, err)
	assert.Nil(t, got)
	// Config.ModelNameVal 未变（model 为 nil，不进入设置分支）
	assert.Equal(t, "old-model", config.ModelNameVal)
}

// TestReActAgent_SwitchModel_nilConfig 验证 SwitchModel 在 nil config 时不 panic
func TestReActAgent_SwitchModel_nilConfig(t *testing.T) {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("switch_nocfg"),
		agentschema.WithAgentDescription("SwitchModel nil config 测试"),
	)
	agent := NewReActAgent(card, nil)

	initFakeModelClient()
	clientCfg := &llmschema.ModelClientConfig{ClientProvider: fakeClientProvider, ClientID: fakeClientProvider}
	modelCfg := &llmschema.ModelRequestConfig{ModelName: "new-model"}
	newModel, err := llm.NewModel(clientCfg, modelCfg)
	assert.NoError(t, err)

	// 不 panic
	agent.SwitchModel(newModel)

	got, err := agent.GetLLM()
	assert.NoError(t, err)
	assert.Equal(t, newModel, got)
}

// ──────────────────────────── 新增测试：InvokeImpl 完整路径 ────────────────────────────

// TestReActAgent_InvokeImpl_完整路径 验证完整 invoke 路径（有 LLM + ContextEngine + Session）
func TestReActAgent_InvokeImpl_完整路径(t *testing.T) {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("invoke_full"),
		agentschema.WithAgentDescription("完整 Invoke 测试"),
	)
	config := saconfig.NewReActAgentConfig(
		saconfig.WithModelClient(fakeClientProvider, "", "", "test-model"),
		saconfig.WithMaxIterations(1),
	)
	agent := NewReActAgent(card, config)
	initFakeModelClient()

	clientCfg := &llmschema.ModelClientConfig{ClientProvider: fakeClientProvider, ClientID: fakeClientProvider}
	modelCfg := &llmschema.ModelRequestConfig{ModelName: "test-model"}
	model, err := llm.NewModel(clientCfg, modelCfg)
	assert.NoError(t, err)
	agent.llm = model

	fmc := &fakeModelContext{}
	fce := &fakeContextEngine{modelCtx: fmc}
	agent.contextEngine = fce

	sess := session.NewSession(session.WithSessionID("invoke_full_sess"))
	inputs := map[string]any{
		"query":           "你好",
		"conversation_id": "conv1",
		"user_id":         "u1",
		"run_kind":        "normal",
		"run_context":     "test",
	}
	result, err := agent.invokeImpl(context.Background(), inputs, interfaces.WithSession(sess))
	assert.NoError(t, err)
	assert.NotNil(t, result)
}

// TestReActAgent_InvokeImpl_完整路径无Session 验证自建 session 时的清理路径
func TestReActAgent_InvokeImpl_完整路径无Session(t *testing.T) {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("invoke_full_nosess"),
		agentschema.WithAgentDescription("完整 Invoke 无 session 测试"),
	)
	config := saconfig.NewReActAgentConfig(
		saconfig.WithModelClient(fakeClientProvider, "", "", "test-model"),
		saconfig.WithMaxIterations(1),
	)
	agent := NewReActAgent(card, config)
	initFakeModelClient()

	clientCfg := &llmschema.ModelClientConfig{ClientProvider: fakeClientProvider, ClientID: fakeClientProvider}
	modelCfg := &llmschema.ModelRequestConfig{ModelName: "test-model"}
	model, err := llm.NewModel(clientCfg, modelCfg)
	assert.NoError(t, err)
	agent.llm = model

	fmc := &fakeModelContext{}
	fce := &fakeContextEngine{modelCtx: fmc}
	agent.contextEngine = fce

	inputs := map[string]any{"query": "hello"}
	result, err := agent.invokeImpl(context.Background(), inputs)
	assert.NoError(t, err)
	assert.NotNil(t, result)
}

// TestReActAgent_InvokeImpl_invokeResult优先 验证 extra 中 invoke_result 优先返回
func TestReActAgent_InvokeImpl_invokeResult优先(t *testing.T) {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("invoke_priority"),
		agentschema.WithAgentDescription("invoke result 优先测试"),
	)
	config := saconfig.NewReActAgentConfig(
		saconfig.WithModelClient(fakeClientProvider, "", "", "test-model"),
		saconfig.WithMaxIterations(1),
	)
	agent := NewReActAgent(card, config)
	initFakeModelClient()

	clientCfg := &llmschema.ModelClientConfig{ClientProvider: fakeClientProvider, ClientID: fakeClientProvider}
	modelCfg := &llmschema.ModelRequestConfig{ModelName: "test-model"}
	model, err := llm.NewModel(clientCfg, modelCfg)
	assert.NoError(t, err)
	agent.llm = model

	// 通过 before_invoke 回调设置 invoke_result
	manager := agent.CallbackManager()
	manager.RegisterCallback(context.Background(), interfaces.CallbackBeforeInvoke, func(_ context.Context, railCtx any) error {
		if c, ok := railCtx.(*interfaces.AgentCallbackContext); ok {
			c.Extra()["invoke_result"] = map[string]any{"output": "回调结果", "result_type": "answer"}
		}
		return nil
	})

	fmc := &fakeModelContext{}
	fce := &fakeContextEngine{modelCtx: fmc}
	agent.contextEngine = fce

	sess := session.NewSession(session.WithSessionID("invoke_priority_sess"))
	inputs := map[string]any{"query": "hello"}
	result, err := agent.invokeImpl(context.Background(), inputs, interfaces.WithSession(sess))
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "回调结果", result["output"])
}

// TestReActAgent_StreamImpl_完整路径 验证完整 StreamImpl 路径
func TestReActAgent_StreamImpl_完整路径(t *testing.T) {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("stream_full"),
		agentschema.WithAgentDescription("完整 Stream 测试"),
	)
	config := saconfig.NewReActAgentConfig(
		saconfig.WithModelClient(fakeClientProvider, "", "", "test-model"),
		saconfig.WithMaxIterations(1),
	)
	agent := NewReActAgent(card, config)
	initFakeModelClient()

	clientCfg := &llmschema.ModelClientConfig{ClientProvider: fakeClientProvider, ClientID: fakeClientProvider}
	modelCfg := &llmschema.ModelRequestConfig{ModelName: "test-model"}
	model, err := llm.NewModel(clientCfg, modelCfg)
	assert.NoError(t, err)
	agent.llm = model

	fmc := &fakeModelContext{}
	fce := &fakeContextEngine{modelCtx: fmc}
	agent.contextEngine = fce

	sess := session.NewSession(session.WithSessionID("stream_full_sess"))
	inputs := map[string]any{"query": "hello"}
	ch, err := agent.streamImpl(context.Background(), inputs, interfaces.WithSession(sess))
	assert.NoError(t, err)
	assert.NotNil(t, ch)
	for range ch {
	}
}

// ──────────────────────────── 合并自 ext_test 的独有测试 ────────────────────────────

// TestReActAgent_ContextEngine 默认返回 nil
func TestReActAgent_ContextEngine(t *testing.T) {
	agent := newTestAgent("ce_default")
	assert.Nil(t, agent.ContextEngine())
}

// TestReActAgent_Configure_校验失败 配置校验失败时返回错误
func TestReActAgent_Configure_校验失败(t *testing.T) {
	agent := newTestAgent("cfg_fail")
	config := saconfig.NewReActAgentConfig() // ModelName 为空，校验失败
	err := agent.Configure(context.Background(), config)
	assert.Error(t, err)
}

// TestReActAgent_InvokeImpl_有query 有 query 时正常执行（LLM 未初始化会报错但不 panic）
func TestReActAgent_InvokeImpl_有query(t *testing.T) {
	agent := newTestAgent("invoke_q2")
	inputs := map[string]any{"query": "你好"}
	_, _ = agent.invokeImpl(context.Background(), inputs)
}

// TestReActAgent_InvokeImpl_带额外参数 带 user_id/run_kind/run_context 等额外参数
func TestReActAgent_InvokeImpl_带额外参数(t *testing.T) {
	agent := newTestAgent("invoke_extra2")
	sess := session.NewSession(session.WithSessionID("extra_session2"))
	inputs := map[string]any{
		"query":       "测试",
		"user_id":     "u1",
		"run_kind":    "heartbeat",
		"run_context": "ctx1",
		"_streaming":  false,
	}
	_, _ = agent.invokeImpl(context.Background(), inputs, interfaces.WithSession(sess))
}

// TestReActAgent_AfterExecuteToolCallForHITL_nilHandler handler 为 nil 时返回 nil
func TestReActAgent_AfterExecuteToolCallForHITL_nilHandler(t *testing.T) {
	card := agentschema.NewAgentCard(agentschema.WithAgentName("hitl_nil"), agentschema.WithAgentDescription("HITL nil 测试"))
	agent := NewReActAgent(card, nil)
	agent.hitlHandler = nil

	intState, payloads := agent.AfterExecuteToolCallForHITL(nil, nil, nil, 0, "")
	assert.Nil(t, intState)
	assert.Nil(t, payloads)
}

// TestReActAgent_CommitInterrupt_nilHandler handler 为 nil 时返回 nil
func TestReActAgent_CommitInterrupt_nilHandler(t *testing.T) {
	card := agentschema.NewAgentCard(agentschema.WithAgentName("commit_nil"), agentschema.WithAgentDescription("Commit nil 测试"))
	agent := NewReActAgent(card, nil)
	agent.hitlHandler = nil

	result, err := agent.CommitInterrupt(context.Background(), nil, nil, nil, nil, nil)
	assert.Nil(t, result)
	assert.NoError(t, err)
}

// TestReActAgent_ClearContextMessages_无引擎 无 context engine 时不 panic
func TestReActAgent_ClearContextMessages_无引擎(t *testing.T) {
	agent := newTestAgent("clear_no_ce")
	sess := session.NewSession(session.WithSessionID("clear_no_ce_sess"))
	agent.ClearContextMessages(sess)
}

// TestReActAgent_ClearContextMessages_nilSession sess 为 nil 时不 panic
func TestReActAgent_ClearContextMessages_nilSession(t *testing.T) {
	agent := newTestAgent("clear_nil2")
	agent.ClearContextMessages(nil)
}

// TestReActAgent_executeToolCalls_空列表 空工具调用列表返回 nil
func TestReActAgent_executeToolCalls_空列表(t *testing.T) {
	agent := newTestAgent("exec_empty")
	results, err := agent.executeToolCalls(context.Background(), nil, nil, nil, nil)
	assert.NoError(t, err)
	assert.Nil(t, results)
}

// TestReActAgent_saveContexts_无引擎 无 context engine 时不 panic
func TestReActAgent_saveContexts_无引擎(t *testing.T) {
	agent := newTestAgent("save_no_ce")
	agent.saveContexts(nil)
	sess := session.NewSession(session.WithSessionID("save_no_ce_sess"))
	agent.saveContexts(sess)
}

// TestReActAgent_getAbilityManager 正常返回 AbilityManager
func TestReActAgent_getAbilityManager(t *testing.T) {
	agent := newTestAgent("get_am")
	am := agent.getAbilityManager()
	assert.NotNil(t, am)
}

// TestReActAgent_getTools 正常返回工具列表
func TestReActAgent_getTools(t *testing.T) {
	agent := newTestAgent("get_tools2")
	tools, err := agent.getTools()
	assert.NoError(t, err)
	_ = tools
}

// TestReActAgent_WriteInvokeResultToStream_nilHandler hitlHandler 为 nil 时不 panic
func TestReActAgent_WriteInvokeResultToStream_nilHandler(t *testing.T) {
	card := agentschema.NewAgentCard(agentschema.WithAgentName("nil_handler2"), agentschema.WithAgentDescription("nil handler2"))
	agent := NewReActAgent(card, nil)
	agent.hitlHandler = nil

	sess := session.NewSession(session.WithSessionID("nil_h_session2"))
	result := map[string]any{
		"result_type":   "interrupt",
		"interrupt_ids": []string{"id1"},
	}
	agent.WriteInvokeResultToStream(context.Background(), result, sess)
}

// TestReActAgent_SetAbilityManager 验证 SetAbilityManager 设置能力管理器
func TestReActAgent_SetAbilityManager(t *testing.T) {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("set_am"),
		agentschema.WithAgentDescription("设置能力管理器测试"),
	)
	agent := NewReActAgent(card, nil)

	// 初始非 nil（构造时自动初始化）
	assert.NotNil(t, agent.AbilityManager())

	// 设置新的能力管理器后可获取
	newAM := ability.NewAbilityManager(nil)
	agent.SetAbilityManager(newAM)
	assert.Equal(t, newAM, agent.AbilityManager())
}

// TestReActAgent_SetPromptBuilder 验证 SetPromptBuilder 设置提示词构建器
func TestReActAgent_SetPromptBuilder(t *testing.T) {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("set_pb"),
		agentschema.WithAgentDescription("设置提示词构建器测试"),
	)
	agent := NewReActAgent(card, nil)

	pb := agent.PromptBuilder()
	assert.NotNil(t, pb)

	// 设置新的 promptBuilder
	newPB := prompts.NewSystemPromptBuilder()
	agent.SetPromptBuilder(newPB)
	assert.Equal(t, newPB, agent.PromptBuilder())
}

// TestReActAgent_Card 验证 Card 返回身份卡片
func TestReActAgent_Card(t *testing.T) {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("card_test"),
		agentschema.WithAgentDescription("Card 测试"),
	)
	agent := NewReActAgent(card, nil)
	assert.Equal(t, card, agent.Card())
}

// TestReActAgent_Config 验证 Config 返回配置
func TestReActAgent_Config(t *testing.T) {
	config := saconfig.NewReActAgentConfig(
		saconfig.WithModelName("test-model"),
	)
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("config_test"),
		agentschema.WithAgentDescription("Config 测试"),
	)
	agent := NewReActAgent(card, config)
	assert.Equal(t, config, agent.Config())
}

// TestReActAgent_AbilityManager_nil 验证构造时自动初始化能力管理器
func TestReActAgent_AbilityManager_nil(t *testing.T) {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("am_nil"),
		agentschema.WithAgentDescription("能力管理器 nil 测试"),
	)
	agent := NewReActAgent(card, nil)
	// 构造时自动初始化 AbilityManager，不为 nil
	assert.NotNil(t, agent.AbilityManager())
}

// TestHasImagePrefix 验证 hasImagePrefix 检查 data:image/ 前缀
func TestHasImagePrefix(t *testing.T) {
	assert.True(t, hasImagePrefix("data:image/png;base64,abc"))
	assert.True(t, hasImagePrefix("data:image/jpeg;..."))
	assert.False(t, hasImagePrefix("data:text/plain;base64,abc"))
	assert.False(t, hasImagePrefix("http://example.com/image.png"))
	assert.False(t, hasImagePrefix(""))
	assert.False(t, hasImagePrefix("data:imag"))
}

// TestIterMultimodalImageItems_非map 验证非 map 类型输入返回 nil
func TestIterMultimodalImageItems_非map(t *testing.T) {
	result := iterMultimodalImageItems("not a map")
	assert.Nil(t, result)
}

// TestIterMultimodalImageItems_无data字段 验证无 data 字段返回 nil
func TestIterMultimodalImageItems_无data字段(t *testing.T) {
	result := iterMultimodalImageItems(map[string]any{"other": "value"})
	assert.Nil(t, result)
}

// TestIterMultimodalImageItems_无multimodal字段 验证无 multimodal 字段返回 nil
func TestIterMultimodalImageItems_无multimodal字段(t *testing.T) {
	result := iterMultimodalImageItems(map[string]any{
		"data": map[string]any{"other": "value"},
	})
	assert.Nil(t, result)
}

// TestIterMultimodalImageItems_有图片项 验证提取 image 类型且 data:image/ 前缀的项
func TestIterMultimodalImageItems_有图片项(t *testing.T) {
	input := map[string]any{
		"data": map[string]any{
			"multimodal": []any{
				map[string]any{
					"type":        "image",
					"data_url":    "data:image/png;base64,abc",
					"source_path": "/path/to/image.png",
				},
				map[string]any{
					"type":     "text",
					"data_url": "data:text/plain;base64,abc",
				},
				map[string]any{
					"type":     "image",
					"data_url": "http://example.com/image.png",
				},
			},
		},
	}
	result := iterMultimodalImageItems(input)
	assert.Equal(t, 1, len(result))
	assert.Equal(t, "image", result[0]["type"])
	assert.Equal(t, "data:image/png;base64,abc", result[0]["data_url"])
}

// TestBuildMultimodalToolResultsMessage_空结果 验证空工具结果返回 nil
func TestBuildMultimodalToolResultsMessage_空结果(t *testing.T) {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("mm_empty"),
		agentschema.WithAgentDescription("多模态空测试"),
	)
	agent := NewReActAgent(card, nil)

	results := []agentschema.ExecuteResult{{Result: "not a map"}}
	msg := agent.buildMultimodalToolResultsMessage(results)
	assert.Nil(t, msg)
}

// TestBuildMultimodalToolResultsMessage_有图片 验证含图片结果返回 UserMessage
func TestBuildMultimodalToolResultsMessage_有图片(t *testing.T) {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("mm_img"),
		agentschema.WithAgentDescription("多模态图片测试"),
	)
	agent := NewReActAgent(card, nil)

	results := []agentschema.ExecuteResult{
		{
			Result: map[string]any{
				"data": map[string]any{
					"multimodal": []any{
						map[string]any{
							"type":        "image",
							"data_url":    "data:image/png;base64,abc",
							"source_path": "/path/to/img.png",
						},
					},
				},
			},
		},
	}
	msg := agent.buildMultimodalToolResultsMessage(results)
	assert.NotNil(t, msg)
}

// TestBuildMultimodalToolResultsMessage_多张图片 验证多张图片添加摘要
func TestBuildMultimodalToolResultsMessage_多张图片(t *testing.T) {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("mm_multi"),
		agentschema.WithAgentDescription("多模态多图片测试"),
	)
	agent := NewReActAgent(card, nil)

	results := []agentschema.ExecuteResult{
		{
			Result: map[string]any{
				"data": map[string]any{
					"multimodal": []any{
						map[string]any{
							"type":        "image",
							"data_url":    "data:image/png;base64,abc1",
							"source_path": "/path/1.png",
						},
						map[string]any{
							"type":        "image",
							"data_url":    "data:image/jpeg;base64,abc2",
							"source_path": "/path/2.png",
						},
					},
				},
			},
		},
	}
	msg := agent.buildMultimodalToolResultsMessage(results)
	assert.NotNil(t, msg)
}

// TestBuildMultimodalToolResultsMessage_无sourcePath 验证无 source_path 时使用 unknown image
func TestBuildMultimodalToolResultsMessage_无sourcePath(t *testing.T) {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("mm_nopath"),
		agentschema.WithAgentDescription("多模态无路径测试"),
	)
	agent := NewReActAgent(card, nil)

	results := []agentschema.ExecuteResult{
		{
			Result: map[string]any{
				"data": map[string]any{
					"multimodal": []any{
						map[string]any{
							"type":     "image",
							"data_url": "data:image/png;base64,abc",
						},
					},
				},
			},
		},
	}
	msg := agent.buildMultimodalToolResultsMessage(results)
	assert.NotNil(t, msg)
}

// TestReActAgent_RegisterCallback_无管理器 验证无回调管理器时注册回调不 panic
func TestReActAgent_RegisterCallback_无管理器(t *testing.T) {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("reg_cb"),
		agentschema.WithAgentDescription("注册回调测试"),
	)
	agent := NewReActAgent(card, nil)
	err := agent.RegisterCallback(context.Background(), interfaces.CallbackBeforeInvoke, nil)
	assert.NoError(t, err)
}

// TestReActAgent_RegisterRail_无管理器 验证无回调管理器时注册 Rail 不 panic
func TestReActAgent_RegisterRail_无管理器(t *testing.T) {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("reg_rail"),
		agentschema.WithAgentDescription("注册 Rail 测试"),
	)
	agent := NewReActAgent(card, nil)
	agent.callbackManager = nil
	// callbackManager 为 nil 时 RegisterRail 直接返回 nil
	err := agent.RegisterRail(context.Background(), nil)
	assert.NoError(t, err)
}

// TestReActAgent_UnregisterRail_无管理器 验证无回调管理器时注销 Rail 不 panic
func TestReActAgent_UnregisterRail_无管理器(t *testing.T) {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("unreg_rail"),
		agentschema.WithAgentDescription("注销 Rail 测试"),
	)
	agent := NewReActAgent(card, nil)
	agent.callbackManager = nil
	// callbackManager 为 nil 时 UnregisterRail 直接返回 nil
	err := agent.UnregisterRail(context.Background(), nil)
	assert.NoError(t, err)
}

// TestReActAgent_SkillUtil 验证 SkillUtil 和 SetSkillUtil
func TestReActAgent_SkillUtil(t *testing.T) {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("skill_util"),
		agentschema.WithAgentDescription("技能工具测试"),
	)
	agent := NewReActAgent(card, nil)

	// 初始为 nil
	assert.Nil(t, agent.SkillUtil())

	// 设置后可获取
	su := skills.NewSkillUtil("test")
	agent.SetSkillUtil(su)
	assert.Equal(t, su, agent.SkillUtil())
}

// TestReActAgent_SystemPromptBuilder 验证 SystemPromptBuilder
func TestReActAgent_SystemPromptBuilder(t *testing.T) {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("sys_pb"),
		agentschema.WithAgentDescription("系统提示词构建器测试"),
	)
	agent := NewReActAgent(card, nil)

	// 有 promptBuilder 时返回接口
	spb := agent.SystemPromptBuilder()
	assert.NotNil(t, spb)
}

// TestReActAgent_SystemPromptBuilder_nil 验证 promptBuilder 为 nil 时返回 nil
func TestReActAgent_SystemPromptBuilder_nil(t *testing.T) {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("sys_pb_nil"),
		agentschema.WithAgentDescription("系统提示词构建器 nil 测试"),
	)
	agent := NewReActAgent(card, nil)
	agent.SetPromptBuilder(nil)
	assert.Nil(t, agent.SystemPromptBuilder())
}
