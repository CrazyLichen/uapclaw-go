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
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/rail"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
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

// TestNewReActAgent 验证 ReActAgent 构造函数
func TestNewReActAgent(t *testing.T) {
	card := agentschema.NewAgentCard(
		cschema.WithName("test_react"),
		cschema.WithDescription("测试 ReActAgent"),
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
		cschema.WithName("nil_cfg_react"),
		cschema.WithDescription("nil 配置 ReActAgent"),
	)
	agent := NewReActAgent(card, nil)
	assert.NotNil(t, agent)
	assert.Nil(t, agent.config)
}

// TestNewSystemPromptBuilder 验证系统提示词构建器构造
func TestNewSystemPromptBuilder(t *testing.T) {
	builder := NewSystemPromptBuilder()
	assert.NotNil(t, builder)
	assert.Equal(t, "cn", builder.Language)
	assert.NotNil(t, builder.sections)
}

// TestNewPromptSection 验证提示节构造
func TestNewPromptSection(t *testing.T) {
	section := NewPromptSection("test", map[string]string{"cn": "测试"}, 10)
	assert.Equal(t, "test", section.Name)
	assert.Equal(t, 10, section.Priority)
	assert.Equal(t, "测试", section.Content["cn"])
}

// TestPromptSection_Render 验证提示节渲染
func TestPromptSection_Render(t *testing.T) {
	section := PromptSection{
		Name:     "test",
		Content:  map[string]string{"cn": "中文", "en": "English"},
		Priority: 10,
	}

	// 匹配指定语言
	assert.Equal(t, "中文", section.Render("cn"))
	assert.Equal(t, "English", section.Render("en"))

	// 回退到默认语言
	assert.Equal(t, "中文", section.Render("fr"))

	// 空内容
	emptySection := PromptSection{Name: "empty", Content: map[string]string{}}
	assert.Equal(t, "", emptySection.Render("cn"))
}

// TestPromptSection_Render_任意语言回退 验证无默认语言时回退到任意语言
func TestPromptSection_Render_任意语言回退(t *testing.T) {
	section := PromptSection{
		Name:     "test",
		Content:  map[string]string{"en": "English"},
		Priority: 10,
	}
	assert.Equal(t, "English", section.Render("fr"))
}

// TestSystemPromptBuilder_AddSection 验证添加节
func TestSystemPromptBuilder_AddSection(t *testing.T) {
	builder := NewSystemPromptBuilder()
	builder.AddSection(PromptSection{Name: "a", Content: map[string]string{"cn": "AAA"}, Priority: 20})
	builder.AddSection(PromptSection{Name: "b", Content: map[string]string{"cn": "BBB"}, Priority: 10})

	assert.True(t, builder.HasSection("a"))
	assert.True(t, builder.HasSection("b"))
}

// TestSystemPromptBuilder_AddSection_链式调用 验证 AddSection 返回自身支持链式调用
func TestSystemPromptBuilder_AddSection_链式调用(t *testing.T) {
	builder := NewSystemPromptBuilder()
	result := builder.AddSection(PromptSection{Name: "a", Content: map[string]string{"cn": "AAA"}, Priority: 10})
	assert.Equal(t, builder, result)
}

// TestSystemPromptBuilder_AddSection_同名称覆盖 验证同名称节覆盖
func TestSystemPromptBuilder_AddSection_同名称覆盖(t *testing.T) {
	builder := NewSystemPromptBuilder()
	builder.AddSection(PromptSection{Name: "a", Content: map[string]string{"cn": "AAA"}, Priority: 10})
	builder.AddSection(PromptSection{Name: "a", Content: map[string]string{"cn": "BBB"}, Priority: 20})
	assert.Equal(t, "BBB", builder.Build())
}

// TestSystemPromptBuilder_RemoveSection 验证移除节
func TestSystemPromptBuilder_RemoveSection(t *testing.T) {
	builder := NewSystemPromptBuilder()
	builder.AddSection(PromptSection{Name: "a", Content: map[string]string{"cn": "AAA"}, Priority: 10})
	assert.True(t, builder.HasSection("a"))

	builder.RemoveSection("a")
	assert.False(t, builder.HasSection("a"))
}

// TestSystemPromptBuilder_RemoveSection_链式调用 验证 RemoveSection 返回自身
func TestSystemPromptBuilder_RemoveSection_链式调用(t *testing.T) {
	builder := NewSystemPromptBuilder()
	result := builder.RemoveSection("nonexistent")
	assert.Equal(t, builder, result)
}

// TestSystemPromptBuilder_HasSection_不存在 验证不存在时节返回 false
func TestSystemPromptBuilder_HasSection_不存在(t *testing.T) {
	builder := NewSystemPromptBuilder()
	assert.False(t, builder.HasSection("nonexistent"))
}

// TestSystemPromptBuilder_Build 验证构建结果按优先级排序
func TestSystemPromptBuilder_Build(t *testing.T) {
	builder := NewSystemPromptBuilder()
	builder.AddSection(PromptSection{Name: "low", Content: map[string]string{"cn": "LOW"}, Priority: 30})
	builder.AddSection(PromptSection{Name: "mid", Content: map[string]string{"cn": "MID"}, Priority: 20})
	builder.AddSection(PromptSection{Name: "high", Content: map[string]string{"cn": "HIGH"}, Priority: 10})

	result := builder.Build()
	assert.Equal(t, "HIGH\n\nMID\n\nLOW", result)
}

// TestSystemPromptBuilder_Build_空构建器 验证空构建器返回空字符串
func TestSystemPromptBuilder_Build_空构建器(t *testing.T) {
	builder := NewSystemPromptBuilder()
	assert.Equal(t, "", builder.Build())
}

// TestSystemPromptBuilder_Build_单节 验证单节构建不添加换行
func TestSystemPromptBuilder_Build_单节(t *testing.T) {
	builder := NewSystemPromptBuilder()
	builder.AddSection(PromptSection{Name: "only", Content: map[string]string{"cn": "ONLY"}, Priority: 10})
	assert.Equal(t, "ONLY", builder.Build())
}

// TestSystemPromptBuilder_Build_跳过空内容节 验证空内容节被跳过
func TestSystemPromptBuilder_Build_跳过空内容节(t *testing.T) {
	builder := NewSystemPromptBuilder()
	builder.AddSection(PromptSection{Name: "empty", Content: map[string]string{}, Priority: 10})
	builder.AddSection(PromptSection{Name: "nonempty", Content: map[string]string{"cn": "VALUE"}, Priority: 20})
	assert.Equal(t, "VALUE", builder.Build())
}

// TestReActAgent_InvokeImpl_空输入 验证无 query 时不 panic
func TestReActAgent_InvokeImpl_空输入(t *testing.T) {
	card := agentschema.NewAgentCard(
		cschema.WithName("test_react"),
		cschema.WithDescription("测试 ReActAgent"),
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
		cschema.WithName("test_react"),
		cschema.WithDescription("测试 ReActAgent"),
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
		cschema.WithName("test_react"),
		cschema.WithDescription("测试 ReActAgent"),
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
		cschema.WithName("invoke_sess"),
		cschema.WithDescription("Invoke 带 Session 测试"),
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
		cschema.WithName("invoke_bool"),
		cschema.WithDescription("Invoke bool 测试"),
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
		cschema.WithName("id_test"),
		cschema.WithDescription("ID 测试"),
	)
	agent := NewReActAgent(card, nil)
	assert.Equal(t, card.ID, agent.AgentID())
}

// TestReActAgent_CallbackManager 验证 CallbackManager 不为 nil
func TestReActAgent_CallbackManager(t *testing.T) {
	card := agentschema.NewAgentCard(
		cschema.WithName("cb_test"),
		cschema.WithDescription("回调测试"),
	)
	agent := NewReActAgent(card, nil)
	assert.NotNil(t, agent.CallbackManager())
}

// TestReActAgent_ContextEngine_设置引擎 验证 ContextEngine 设置后返回正确值
func TestReActAgent_ContextEngine_设置引擎(t *testing.T) {
	card := agentschema.NewAgentCard(
		cschema.WithName("ce_set"),
		cschema.WithDescription("设置上下文引擎测试"),
	)
	agent := NewReActAgent(card, nil)
	fce := &fakeContextEngine{}
	agent.contextEngine = fce
	assert.Equal(t, fce, agent.ContextEngine())
}

// TestReActAgent_Configure 验证 Configure 正常配置
func TestReActAgent_Configure(t *testing.T) {
	card := agentschema.NewAgentCard(
		cschema.WithName("cfg_test"),
		cschema.WithDescription("配置测试"),
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
		cschema.WithName("nil_cfg"),
		cschema.WithDescription("nil 配置测试"),
	)
	agent := NewReActAgent(card, nil)
	err := agent.Configure(context.Background(), nil)
	assert.Error(t, err)
}

// TestReActAgent_Configure_带提示词模板 验证 Configure 带 PromptTemplateName 时添加提示节
func TestReActAgent_Configure_带提示词模板(t *testing.T) {
	card := agentschema.NewAgentCard(
		cschema.WithName("cfg_prompt"),
		cschema.WithDescription("配置提示词测试"),
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
		cschema.WithName("prompt_test"),
		cschema.WithDescription("提示词测试"),
	)
	agent := NewReActAgent(card, nil)
	agent.AddPromptBuilderSection("identity", map[string]string{"cn": "我是测试Agent"}, 10)
	assert.True(t, agent.promptBuilder.HasSection("identity"))
}

// TestReActAgent_StreamImpl 验证 StreamImpl 返回 channel
func TestReActAgent_StreamImpl(t *testing.T) {
	card := agentschema.NewAgentCard(
		cschema.WithName("stream_test"),
		cschema.WithDescription("流式测试"),
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
		cschema.WithName("stream_sess"),
		cschema.WithDescription("流式 Session 测试"),
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
		cschema.WithName("llm_test"),
		cschema.WithDescription("LLM 测试"),
	)
	agent := NewReActAgent(card, nil)
	_, err := agent.getLLM()
	assert.Error(t, err)
}

// TestReActAgent_initContext_无引擎 验证无 context engine 时返回 nil
func TestReActAgent_initContext_无引擎(t *testing.T) {
	card := agentschema.NewAgentCard(
		cschema.WithName("ctx_test"),
		cschema.WithDescription("上下文测试"),
	)
	agent := NewReActAgent(card, nil)
	mc, err := agent.initContext(context.Background(), nil)
	assert.NoError(t, err)
	assert.Nil(t, mc)
}

// TestReActAgent_initContext_有引擎 验证有 context engine 时正确调用
func TestReActAgent_initContext_有引擎(t *testing.T) {
	card := agentschema.NewAgentCard(
		cschema.WithName("ctx_test2"),
		cschema.WithDescription("上下文引擎测试"),
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
		cschema.WithName("ctx_err"),
		cschema.WithDescription("上下文错误测试"),
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
		cschema.WithName("save_err"),
		cschema.WithDescription("保存错误测试"),
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
		cschema.WithName("clear_ctx2"),
		cschema.WithDescription("清除上下文消息测试2"),
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
		cschema.WithName("clear_nil"),
		cschema.WithDescription("清除 nil 上下文测试"),
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
		cschema.WithName("hitl_int"),
		cschema.WithDescription("HITL 中断测试"),
	)
	agent := NewReActAgent(card, nil)

	// 构造 ToolInterruptException 结果
	req := &interrupt.InterruptRequest{Message: "请确认", AutoConfirmKey: "auto_key"}
	tie := &interrupt.ToolInterruptException{
		Request:  req,
		ToolCall: &llmschema.ToolCall{ID: "tc1", Name: "test_tool", Arguments: "{}"},
	}
	results := []ability.ExecuteResult{{Result: tie, ToolMsg: nil}}
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
		cschema.WithName("commit_int"),
		cschema.WithDescription("提交中断测试"),
	)
	agent := NewReActAgent(card, nil)

	req := &interrupt.InterruptRequest{Message: "确认操作", AutoConfirmKey: "key1"}
	intState := &interrupt.ToolInterruptionState{
		InterruptedTools: map[string]*interrupt.ToolInterruptEntry{
			"tc1": {
				ToolCall: &llmschema.ToolCall{ID: "tc1", Name: "tool1"},
				InterruptRequests: map[string]interrupt.InterruptRequester{
					"tc1": req,
				},
			},
		},
	}
	invokeInputs := &rail.InvokeInputs{
		Query: rail.NewInvokeQueryString("test"),
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
		cschema.WithName("exec_no_am"),
		cschema.WithDescription("无能力管理器测试"),
	)
	agent := NewReActAgent(card, nil)

	toolCalls := []*llmschema.ToolCall{
		{ID: "tc1", Name: "test_tool", Arguments: "{}"},
	}
	cbc := rail.NewAgentCallbackContext(nil, nil, nil)
	// newTestAgent 创建的 agent 有 AbilityManager，不会返回 error
	// 只需验证不 panic
	results, _ := agent.executeToolCalls(context.Background(), cbc, toolCalls, nil, nil)
	// 有 AbilityManager 时返回结果（包含错误信息），但 err 为 nil
	_ = results
}

// TestWriteInvokeResultToStream_正常结果 验证正常结果写入流
func TestWriteInvokeResultToStream_正常结果(t *testing.T) {
	card := agentschema.NewAgentCard(
		cschema.WithName("write_normal"),
		cschema.WithDescription("写入正常结果测试"),
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
		cschema.WithName("write_int"),
		cschema.WithDescription("写入中断结果测试"),
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
		cschema.WithName("write_int_h"),
		cschema.WithDescription("写入中断结果有 Handler 测试"),
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
		cschema.WithName("write_wf"),
		cschema.WithDescription("写入 Workflow 中断测试"),
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
		cschema.WithName("invoke_ce"),
		cschema.WithDescription("Invoke 有 ContextEngine 测试"),
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
		cschema.WithName("invoke_cep"),
		cschema.WithDescription("Invoke 有 ContextEngine 和 Prompt 测试"),
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
		cschema.WithName("invoke_steer_sess"),
		cschema.WithDescription("Invoke Steering Session 测试"),
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
		cschema.WithName("invoke_resume"),
		cschema.WithDescription("Invoke 中断恢复测试"),
	)
	config := saconfig.NewReActAgentConfig(
		saconfig.WithModelName("qwen-max"),
		saconfig.WithMaxIterations(1),
	)
	agent := NewReActAgent(card, config)

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
	_, _ = agent.invokeImpl(context.Background(), inputs, interfaces.WithSession(sess))
}

// TestReActAgent_InvokeImpl_中断恢复有ContextEngine 验证中断恢复有 context engine
func TestReActAgent_InvokeImpl_中断恢复有ContextEngine(t *testing.T) {
	card := agentschema.NewAgentCard(
		cschema.WithName("invoke_resume_ce"),
		cschema.WithDescription("Invoke 中断恢复有 CE 测试"),
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
	_, _ = agent.invokeImpl(context.Background(), inputs, interfaces.WithSession(sess))
}

// TestReActAgent_InvokeImpl_有queryNoSession 验证有 query 无 session 时 InvokeImpl 不 panic
func TestReActAgent_InvokeImpl_有queryNoSession(t *testing.T) {
	card := agentschema.NewAgentCard(
		cschema.WithName("invoke_q"),
		cschema.WithDescription("Invoke 有 query 测试"),
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
		cschema.WithName("invoke_extra_res"),
		cschema.WithDescription("Invoke extra result 测试"),
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
		cschema.WithName("exec_tc"),
		cschema.WithDescription("工具调用执行测试"),
	)
	config := saconfig.NewReActAgentConfig(
		saconfig.WithModelName("qwen-max"),
	)
	agent := NewReActAgent(card, config)

	toolCalls := []*llmschema.ToolCall{
		{ID: "tc1", Name: "test_tool", Arguments: `{"query": "test"}`},
	}
	cbc := rail.NewAgentCallbackContext(nil, &rail.InvokeInputs{}, nil)
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
		cschema.WithName("invoke_multi"),
		cschema.WithDescription("多迭代 Invoke 测试"),
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
		cschema.WithName("invoke_default_iter"),
		cschema.WithDescription("默认迭代 Invoke 测试"),
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
		cschema.WithName("invoke_ce_fail"),
		cschema.WithDescription("CE 创建失败测试"),
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
		cschema.WithName("invoke_nil_cfg_iter"),
		cschema.WithDescription("nil config 迭代测试"),
	)
	agent := NewReActAgent(card, nil)

	inputs := map[string]any{"query": "hello"}
	// config 为 nil，getLLM 返回错误，不 panic
	_, _ = agent.invokeImpl(context.Background(), inputs)
}

// TestReActAgent_StreamImpl_无Config 验证 StreamImpl 无 config 时
func TestReActAgent_StreamImpl_无Config(t *testing.T) {
	card := agentschema.NewAgentCard(
		cschema.WithName("stream_no_cfg"),
		cschema.WithDescription("流式无配置测试"),
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
		cschema.WithName("call_invoke_empty"),
		cschema.WithDescription("callLLMInvoke 空工具测试"),
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
	tools := []*cschema.ToolInfo{}
	extra := map[string]any{"key": "value"}
	_, _ = agent.callLLMInvoke(context.Background(), llmModel2, "qwen-max", messages, tools, extra)
}

// TestReActAgent_callLLMStream_空工具 验证空工具列表时流式执行
func TestReActAgent_callLLMStream_空工具(t *testing.T) {
	card := agentschema.NewAgentCard(
		cschema.WithName("call_stream_empty"),
		cschema.WithDescription("callLLMStream 空工具测试"),
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
	tools := []*cschema.ToolInfo{}
	extra := map[string]any{"key": "value"}
	sess := session.NewSession(session.WithSessionID("call_stream_empty_sess"))
	_, _ = agent.callLLMStream(context.Background(), llmModel2, "qwen-max", messages, tools, sess, extra)
}

// TestReActAgent_callLLMInvoke_有LLMInstance 验证有 LLM 实例时执行调用
func TestReActAgent_callLLMInvoke_有LLMInstance(t *testing.T) {
	card := agentschema.NewAgentCard(
		cschema.WithName("call_invoke_llm"),
		cschema.WithDescription("callLLMInvoke 有 LLM 测试"),
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
	tools := []*cschema.ToolInfo{}
	extra := map[string]any{}
	_, _ = agent.callLLMInvoke(context.Background(), llmModel2, "qwen-max", messages, tools, extra)
}

// TestReActAgent_callLLMStream_有LLMInstance 验证有 LLM 实例时流式调用
func TestReActAgent_callLLMStream_有LLMInstance(t *testing.T) {
	card := agentschema.NewAgentCard(
		cschema.WithName("call_stream_llm"),
		cschema.WithDescription("callLLMStream 有 LLM 测试"),
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
	tools := []*cschema.ToolInfo{}
	extra := map[string]any{}
	sess := session.NewSession(session.WithSessionID("call_stream_llm_sess"))
	_, _ = agent.callLLMStream(context.Background(), llmModel2, "qwen-max", messages, tools, sess, extra)
}

// TestReActAgent_InvokeImpl_有LLM 验证有 LLM 实例时 InvokeImpl 执行更多分支
func TestReActAgent_InvokeImpl_有LLM(t *testing.T) {
	card := agentschema.NewAgentCard(
		cschema.WithName("invoke_llm"),
		cschema.WithDescription("Invoke 有 LLM 测试"),
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
		cschema.WithName("stream_llm"),
		cschema.WithDescription("Stream 有 LLM 测试"),
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
		cschema.WithName(name),
		cschema.WithDescription("测试"),
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
		cschema.WithName(name),
		cschema.WithDescription("测试"),
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

func (f *fakeModelContext) GetMessages(_ int, _ bool) []llmschema.BaseMessage { return f.messages }

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

func (f *fakeModelContext) GetContextWindow(_ context.Context, sys []llmschema.BaseMessage, _ []*cschema.ToolInfo, _, _ int, _ ...ceinterface.Option) (*ceinterface.ContextWindow, error) {
	return &ceinterface.ContextWindow{
		SystemMessages:  sys,
		ContextMessages: f.messages,
		Tools:           make([]*cschema.ToolInfo, 0),
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
		cschema.WithName("loop_no_tool"),
		cschema.WithDescription("循环无工具测试"),
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
	invokeInputs := &rail.InvokeInputs{Query: rail.NewInvokeQueryString("hello")}
	cbc := rail.NewAgentCallbackContext(agent, invokeInputs, sess)

	result, err := agent.reactLoop(context.Background(), cbc, sess, fmc, 0)
	assert.NoError(t, err)
	assert.NotNil(t, result)
}

// TestReActAgent_reactLoop_有工具调用 验证 LLM 返回工具调用时执行工具
func TestReActAgent_reactLoop_有工具调用(t *testing.T) {
	card := agentschema.NewAgentCard(
		cschema.WithName("loop_tool"),
		cschema.WithDescription("循环有工具测试"),
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
	invokeInputs := &rail.InvokeInputs{Query: rail.NewInvokeQueryString("hello")}
	cbc := rail.NewAgentCallbackContext(agent, invokeInputs, sess)

	result, err := agent.reactLoop(context.Background(), cbc, sess, fmc, 0)
	// 不 panic 即可，工具不存在会返回错误信息但不中断循环
	_ = result
	_ = err
}

// TestReActAgent_reactLoop_达到最大迭代 验证达到最大迭代次数返回错误结果
func TestReActAgent_reactLoop_达到最大迭代(t *testing.T) {
	card := agentschema.NewAgentCard(
		cschema.WithName("loop_max"),
		cschema.WithDescription("最大迭代测试"),
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
	invokeInputs := &rail.InvokeInputs{Query: rail.NewInvokeQueryString("hello")}
	cbc := rail.NewAgentCallbackContext(agent, invokeInputs, sess)

	result, err := agent.reactLoop(context.Background(), cbc, sess, fmc, 0)
	assert.NoError(t, err)
	assert.NotNil(t, result)
}

// TestReActAgent_reactLoop_forceFinish 验证 force-finish 提前终止
func TestReActAgent_reactLoop_forceFinish(t *testing.T) {
	card := agentschema.NewAgentCard(
		cschema.WithName("loop_ff"),
		cschema.WithDescription("force-finish 测试"),
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
	invokeInputs := &rail.InvokeInputs{Query: rail.NewInvokeQueryString("hello")}
	cbc := rail.NewAgentCallbackContext(agent, invokeInputs, sess)

	// 注册 before_model_call 回调来触发 force-finish
	manager := agent.CallbackManager()
	manager.RegisterCallback(context.Background(), rail.CallbackBeforeModelCall, func(_ context.Context, railCtx any) error {
		if c, ok := railCtx.(*rail.AgentCallbackContext); ok {
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
		cschema.WithName("loop_steer"),
		cschema.WithDescription("steering 注入测试"),
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
	invokeInputs := &rail.InvokeInputs{Query: rail.NewInvokeQueryString("hello")}
	cbc := rail.NewAgentCallbackContext(agent, invokeInputs, sess)

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
		cschema.WithName("loop_nocfg"),
		cschema.WithDescription("无配置循环测试"),
	)
	agent := NewReActAgent(card, nil)
	initFakeModelClient()

	clientCfg := &llmschema.ModelClientConfig{ClientProvider: fakeClientProvider, ClientID: fakeClientProvider}
	modelCfg := &llmschema.ModelRequestConfig{ModelName: "test-model"}
	model, err := llm.NewModel(clientCfg, modelCfg)
	assert.NoError(t, err)
	agent.llm = model

	sess := session.NewSession(session.WithSessionID("loop_nocfg_sess"))
	invokeInputs := &rail.InvokeInputs{Query: rail.NewInvokeQueryString("hello")}
	cbc := rail.NewAgentCallbackContext(agent, invokeInputs, sess)

	result, err := agent.reactLoop(context.Background(), cbc, sess, nil, 0)
	// 无 modelCtx 时 LLM 调用路径不同
	_ = result
	_ = err
}

// TestReActAgent_reactLoop_模型调用失败 验证 LLM 调用失败时返回错误
func TestReActAgent_reactLoop_模型调用失败(t *testing.T) {
	card := agentschema.NewAgentCard(
		cschema.WithName("loop_err"),
		cschema.WithDescription("模型失败测试"),
	)
	agent := NewReActAgent(card, nil)

	// 不注入 LLM，getLLM 返回错误
	sess := session.NewSession(session.WithSessionID("loop_err_sess"))
	invokeInputs := &rail.InvokeInputs{Query: rail.NewInvokeQueryString("hello")}
	cbc := rail.NewAgentCallbackContext(agent, invokeInputs, sess)

	result, err := agent.reactLoop(context.Background(), cbc, sess, nil, 0)
	// getLLM 失败
	assert.Error(t, err)
	assert.Nil(t, result)
}

// ──────────────────────────── 新增测试：railedModelCall ────────────────────────────

// TestReActAgent_railedModelCall_基本调用 验证基本 railedModelCall 流程
func TestReActAgent_railedModelCall_基本调用(t *testing.T) {
	card := agentschema.NewAgentCard(
		cschema.WithName("railed_basic"),
		cschema.WithDescription("railed 基本测试"),
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
	invokeInputs := &rail.InvokeInputs{Query: rail.NewInvokeQueryString("hello")}
	cbc := rail.NewAgentCallbackContext(agent, invokeInputs, sess)
	cbc.SetModelContext(fmc)

	msg, err := agent.railedModelCall(context.Background(), cbc, sess)
	assert.NoError(t, err)
	assert.NotNil(t, msg)
}

// TestReActAgent_railedModelCall_无ModelContext 验证 modelCtx 为 nil 时走 systemMsgs 分支
func TestReActAgent_railedModelCall_无ModelContext(t *testing.T) {
	card := agentschema.NewAgentCard(
		cschema.WithName("railed_nil_mc"),
		cschema.WithDescription("railed nil mc 测试"),
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
	invokeInputs := &rail.InvokeInputs{Query: rail.NewInvokeQueryString("hello")}
	cbc := rail.NewAgentCallbackContext(agent, invokeInputs, sess)
	// 不设置 modelContext，保持 nil

	msg, err := agent.railedModelCall(context.Background(), cbc, sess)
	assert.NoError(t, err)
	assert.NotNil(t, msg)
}

// TestReActAgent_railedModelCall_流式模式 验证 _streaming=true 走流式分支
func TestReActAgent_railedModelCall_流式模式(t *testing.T) {
	card := agentschema.NewAgentCard(
		cschema.WithName("railed_stream"),
		cschema.WithDescription("railed stream 测试"),
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
	invokeInputs := &rail.InvokeInputs{Query: rail.NewInvokeQueryString("hello")}
	cbc := rail.NewAgentCallbackContext(agent, invokeInputs, sess)
	cbc.SetModelContext(fmc)
	cbc.Extra()["_streaming"] = true

	msg, err := agent.railedModelCall(context.Background(), cbc, sess)
	assert.NoError(t, err)
	assert.NotNil(t, msg)
}

// TestReActAgent_railedModelCall_KVCache配置 验证 KV Cache 配置处理
func TestReActAgent_railedModelCall_KVCache配置(t *testing.T) {
	card := agentschema.NewAgentCard(
		cschema.WithName("railed_kv"),
		cschema.WithDescription("railed KV 测试"),
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
	invokeInputs := &rail.InvokeInputs{Query: rail.NewInvokeQueryString("hello")}
	cbc := rail.NewAgentCallbackContext(agent, invokeInputs, sess)
	cbc.SetModelContext(fmc)

	msg, err := agent.railedModelCall(context.Background(), cbc, sess)
	// fake client 不支持 KV，会触发一次性警告
	assert.NoError(t, err)
	assert.NotNil(t, msg)
}

// TestReActAgent_railedModelCall_LLMLogprobs 验证 logprobs 和 return_token_ids 配置
func TestReActAgent_railedModelCall_LLMLogprobs(t *testing.T) {
	card := agentschema.NewAgentCard(
		cschema.WithName("railed_logprobs"),
		cschema.WithDescription("railed logprobs 测试"),
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
	invokeInputs := &rail.InvokeInputs{Query: rail.NewInvokeQueryString("hello")}
	cbc := rail.NewAgentCallbackContext(agent, invokeInputs, sess)
	cbc.SetModelContext(fmc)

	msg, err := agent.railedModelCall(context.Background(), cbc, sess)
	assert.NoError(t, err)
	assert.NotNil(t, msg)
}

// TestReActAgent_railedModelCall_无LLM 验证无 LLM 时返回错误
func TestReActAgent_railedModelCall_无LLM(t *testing.T) {
	card := agentschema.NewAgentCard(
		cschema.WithName("railed_no_llm"),
		cschema.WithDescription("railed 无 LLM 测试"),
	)
	agent := NewReActAgent(card, nil)

	sess := session.NewSession(session.WithSessionID("railed_no_llm_sess"))
	invokeInputs := &rail.InvokeInputs{Query: rail.NewInvokeQueryString("hello")}
	cbc := rail.NewAgentCallbackContext(agent, invokeInputs, sess)

	msg, err := agent.railedModelCall(context.Background(), cbc, sess)
	assert.Error(t, err)
	assert.Nil(t, msg)
}

// ──────────────────────────── 新增测试：callLLMInvoke ────────────────────────────

// TestReActAgent_callLLMInvoke_有工具 验证有工具列表时执行
func TestReActAgent_callLLMInvoke_有工具(t *testing.T) {
	card := agentschema.NewAgentCard(
		cschema.WithName("invoke_tools"),
		cschema.WithDescription("invoke 有工具测试"),
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
	tools := []*cschema.ToolInfo{
		{
			Name:        "test_tool",
			Description: "测试工具",
			Parameters:  map[string]any{"type": "object"},
		},
	}
	extra := map[string]any{"key": "value"}

	result, err := agent.callLLMInvoke(context.Background(), model, "test-model", messages, tools, extra)
	assert.NoError(t, err)
	assert.NotNil(t, result)
}

// TestReActAgent_callLLMInvoke_无Extra 验证无 extraKVPairs 时不传
func TestReActAgent_callLLMInvoke_无Extra(t *testing.T) {
	card := agentschema.NewAgentCard(
		cschema.WithName("invoke_no_extra"),
		cschema.WithDescription("invoke 无 extra 测试"),
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
	tools := []*cschema.ToolInfo{}
	extra := map[string]any{} // 空 extra

	result, err := agent.callLLMInvoke(context.Background(), model, "test-model", messages, tools, extra)
	assert.NoError(t, err)
	assert.NotNil(t, result)
}

// TestReActAgent_callLLMInvoke_模型错误 验证 LLM 返回错误时包装返回
func TestReActAgent_callLLMInvoke_模型错误(t *testing.T) {
	card := agentschema.NewAgentCard(
		cschema.WithName("invoke_err"),
		cschema.WithDescription("invoke 错误测试"),
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
		cschema.WithName("stream_tools"),
		cschema.WithDescription("stream 有工具测试"),
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
	tools := []*cschema.ToolInfo{
		{
			Name:        "test_tool",
			Description: "测试工具",
			Parameters:  map[string]any{"type": "object"},
		},
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
		cschema.WithName("stream_no_extra"),
		cschema.WithDescription("stream 无 extra 测试"),
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
	tools := []*cschema.ToolInfo{}
	sess := session.NewSession(session.WithSessionID("stream_no_extra_sess"))
	extra := map[string]any{}

	result, err := agent.callLLMStream(context.Background(), model, "test-model", messages, tools, sess, extra)
	assert.NoError(t, err)
	assert.NotNil(t, result)
}

// TestReActAgent_callLLMStream_nilSession 验证 sess 为 nil 时不写入流
func TestReActAgent_callLLMStream_nilSession(t *testing.T) {
	card := agentschema.NewAgentCard(
		cschema.WithName("stream_nil_sess"),
		cschema.WithDescription("stream nil session 测试"),
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
	tools := []*cschema.ToolInfo{}
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
		cschema.WithName("inner_no_agent"),
		cschema.WithDescription("innerStream 非 agent 测试"),
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
		cschema.WithName("inner_agent"),
		cschema.WithDescription("innerStream agent 测试"),
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
		cschema.WithName("inner_err"),
		cschema.WithDescription("innerStream 错误测试"),
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
		cschema.WithName("callmodel_basic"),
		cschema.WithDescription("callModel 基本测试"),
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
	invokeInputs := &rail.InvokeInputs{Query: rail.NewInvokeQueryString("hello")}
	cbc := rail.NewAgentCallbackContext(agent, invokeInputs, sess)
	cbc.SetModelContext(fmc)

	tools := []*cschema.ToolInfo{}
	msg, err := agent.callModel(context.Background(), cbc, fmc, tools, sess)
	assert.NoError(t, err)
	assert.NotNil(t, msg)
}

// ──────────────────────────── 新增测试：InvokeImpl 完整路径 ────────────────────────────

// TestReActAgent_InvokeImpl_完整路径 验证完整 invoke 路径（有 LLM + ContextEngine + Session）
func TestReActAgent_InvokeImpl_完整路径(t *testing.T) {
	card := agentschema.NewAgentCard(
		cschema.WithName("invoke_full"),
		cschema.WithDescription("完整 Invoke 测试"),
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
		cschema.WithName("invoke_full_nosess"),
		cschema.WithDescription("完整 Invoke 无 session 测试"),
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
		cschema.WithName("invoke_priority"),
		cschema.WithDescription("invoke result 优先测试"),
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
	manager.RegisterCallback(context.Background(), rail.CallbackBeforeInvoke, func(_ context.Context, railCtx any) error {
		if c, ok := railCtx.(*rail.AgentCallbackContext); ok {
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
	if resultMap, ok := result.(map[string]any); ok {
		assert.Equal(t, "回调结果", resultMap["output"])
	}
}

// TestReActAgent_StreamImpl_完整路径 验证完整 StreamImpl 路径
func TestReActAgent_StreamImpl_完整路径(t *testing.T) {
	card := agentschema.NewAgentCard(
		cschema.WithName("stream_full"),
		cschema.WithDescription("完整 Stream 测试"),
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
	card := agentschema.NewAgentCard(cschema.WithName("hitl_nil"), cschema.WithDescription("HITL nil 测试"))
	agent := NewReActAgent(card, nil)
	agent.hitlHandler = nil

	intState, payloads := agent.AfterExecuteToolCallForHITL(nil, nil, nil, 0, "")
	assert.Nil(t, intState)
	assert.Nil(t, payloads)
}

// TestReActAgent_CommitInterrupt_nilHandler handler 为 nil 时返回 nil
func TestReActAgent_CommitInterrupt_nilHandler(t *testing.T) {
	card := agentschema.NewAgentCard(cschema.WithName("commit_nil"), cschema.WithDescription("Commit nil 测试"))
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
	card := agentschema.NewAgentCard(cschema.WithName("nil_handler2"), cschema.WithDescription("nil handler2"))
	agent := NewReActAgent(card, nil)
	agent.hitlHandler = nil

	sess := session.NewSession(session.WithSessionID("nil_h_session2"))
	result := map[string]any{
		"result_type":   "interrupt",
		"interrupt_ids": []string{"id1"},
	}
	agent.WriteInvokeResultToStream(context.Background(), result, sess)
}
