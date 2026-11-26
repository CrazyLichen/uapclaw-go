// ForkedAgent：以隔离 ReActAgent 承载压缩执行。
// 对应 Python 代码：openjiuwen/core/context_engine/processor/forked/compressor/support/forked_agent.py（131 行）
//
// 刻意只拥有 Agent 生命周期，不实现 ReAct 循环——循环、模型调用、tool-call
// 解析与 tool 消息处理仍由 ReActAgent 承担。
// 偏差 D-24.15-b9：PY 经 agent._init_context(session) 私有钩子播种上下文；
// Go initContext 未导出，等价走公开面 agent.ContextEngine().CreateContext + AddMessages。
package support

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm"
	llm_schema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
	sessionagent "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session"
	saagents "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/agents"
	saconfig "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/config"
	saiface "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/interfaces"
	agentschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ForkedAgent 隔离 ReActAgent 承载的压缩执行器。
// Python: forked_agent.py:16 (ForkedAgent(CompressionExecutor))
type ForkedAgent struct {
	*CompressionExecutor

	modelConfig       *llm_schema.ModelRequestConfig
	modelClientConfig *llm_schema.ModelClientConfig
	agentName         string
	agentDescription  string
	promptTemplate    []map[string]any
	maxIterations     int
	agent             *saagents.ReActAgent
	agentCard         *agentschema.AgentCard
	agentNamespace    string
	// configureAgent 子类装配钩子（Python: _configure_agent 虚方法，构造期注入规避 embedding 静态派发）
	configureAgent func(agent *saagents.ReActAgent)
}

// NewForkedAgent 创建 forked 执行器。
// Python: forked_agent.py:23 (__init__)
func NewForkedAgent(
	model *llm.Model,
	modelConfig *llm_schema.ModelRequestConfig,
	modelClientConfig *llm_schema.ModelClientConfig,
	agentName string,
	agentDescription string,
	promptTemplate []map[string]any,
	maxIterations int,
) *ForkedAgent {
	return &ForkedAgent{
		CompressionExecutor: NewCompressionExecutor(model),
		modelConfig:         modelConfig,
		modelClientConfig:   modelClientConfig,
		agentName:           agentName,
		agentDescription:    agentDescription,
		promptTemplate:      promptTemplate,
		maxIterations:       maxIterations,
		agentNamespace:      agentName + "_" + strings.ReplaceAll(uuid.New().String(), "-", ""),
	}
}

// Agent 返回已创建的 agent（未创建为 nil）。
// Python: forked_agent.py:49 (agent property)
func (f *ForkedAgent) Agent() *saagents.ReActAgent { return f.agent }

// AgentCard 返回 agent 卡片（未创建为 nil）。
func (f *ForkedAgent) AgentCard() *agentschema.AgentCard { return f.agentCard }

// Model 返回底层模型（执行体消费）。
func (f *ForkedAgent) Model() *llm.Model { return f.model }

// AgentCardID 返回卡片 id（未创建为空）。
func (f *ForkedAgent) AgentCardID() string {
	if f.agentCard != nil {
		return f.agentCard.ID
	}
	return ""
}

// EnsureAgent 惰性创建 agent（card/config 构造 + SetLLM + 装配钩子）。
// Python: forked_agent.py:53 (_ensure_agent)
func (f *ForkedAgent) EnsureAgent(model *llm.Model) *saagents.ReActAgent {
	if f.agent != nil {
		return f.agent
	}
	card := agentschema.NewAgentCard(
		agentschema.WithAgentID(f.agentNamespace+"_agent"),
		agentschema.WithAgentName(f.agentName),
		agentschema.WithAgentDescription(f.agentDescription),
	)
	modelName := ""
	if f.modelConfig != nil {
		modelName = f.modelConfig.ModelName
	}
	config := saconfig.NewReActAgentConfig(
		saconfig.WithModelName(modelName),
		saconfig.WithModelClientConfig(f.modelClientConfig),
		saconfig.WithModelRequestConfig(f.modelConfig),
		saconfig.WithPromptTemplate(f.promptTemplate),
		saconfig.WithMaxIterations(f.maxIterations),
	)
	agent := saagents.NewReActAgent(card, config)
	agent.SetLLM(model)
	f.agentCard = card
	f.agent = agent
	if f.configureAgent != nil {
		f.configureAgent(agent)
	}
	return agent
}

// SetConfigureAgent 注入子类装配钩子。
// Python: _configure_agent 覆盖点。
func (f *ForkedAgent) SetConfigureAgent(fn func(agent *saagents.ReActAgent)) {
	f.configureAgent = fn
}

// InvokeAgent 经 ReActAgent 会话生命周期执行压缩请求。
// Python: forked_agent.py:88 (invoke)
func (f *ForkedAgent) InvokeAgent(
	ctx context.Context,
	model *llm.Model,
	request *CompressionRequest,
) (*CompressionResult, error) {
	agent := f.EnsureAgent(model)
	seedMessages := BuildContextMessages(request)
	agentSession := f.createAgentSession()
	inputs := map[string]any{
		"query":           request.Prompt,
		"conversation_id": f.agentNamespace,
	}

	var response *llm_schema.AssistantMessage
	var invokeResult map[string]any
	func() {
		defer func() {
			_ = agentSession.CloseStream()
			_ = agentSession.Commit(ctx)
		}()
		if err := agentSession.PreRun(ctx, inputs); err != nil {
			panic(compressionErrorFrom(err))
		}
		if err := primeContext(ctx, agent, agentSession, seedMessages); err != nil {
			panic(compressionErrorFrom(err))
		}
		result, err := agent.Invoke(ctx, inputs, saiface.WithSession(agentSession))
		if err != nil {
			panic(compressionErrorFrom(err))
		}
		invokeResult = result
	}()
	_ = response
	// agent.invoke 返回 map；取 output 文本包装为 CompressionResult（usage 提取见 PY _extract_usage）
	resp := responseFromInvokeResult(invokeResult)
	f.lastResponse = resp
	var usage any
	if resp != nil && resp.UsageMetadata != nil {
		usage = resp.UsageMetadata
	}
	return &CompressionResult{Response: resp, Usage: usage}, nil
}

// createAgentSession 创建隔离会话。
// Python: forked_agent.py:118 (_create_agent_session)
func (f *ForkedAgent) createAgentSession() *sessionagent.Session {
	if f.agentCard == nil {
		panic(fmt.Errorf("Forked agent card is not initialized"))
	}
	return sessionagent.CreateAgentSession(
		f.agentNamespace+"_"+strings.ReplaceAll(uuid.New().String(), "-", "")[:8],
		f.agentCard,
		nil,
	)
}

// primeContext 播种种子上下文（空上下文断言 + 追加种子消息）。
// Python: forked_agent.py:131 (_prime_context)；偏差 D-24.15-b9（公开面等价）。
func primeContext(
	ctx context.Context,
	agent *saagents.ReActAgent,
	session *sessionagent.Session,
	seedMessages []llm_schema.BaseMessage,
) error {
	engine := agent.ContextEngine()
	if engine == nil {
		return fmt.Errorf("ReActAgent does not support context initialization")
	}
	mc, err := engine.CreateContext(ctx, agentSessionID(session), session)
	if err != nil {
		return err
	}
	if existing, _ := mc.GetMessages(0, true); len(existing) > 0 {
		return fmt.Errorf("forked agent context must be empty before seeding")
	}
	if len(seedMessages) > 0 {
		if _, err := mc.AddMessages(ctx, seedMessages); err != nil {
			return err
		}
	}
	return nil
}

// agentSessionID 会话 id 读取（facade 直取）。
func agentSessionID(session *sessionagent.Session) string {
	return session.GetSessionID()
}

// compressionErrorFrom 包装为分类压缩错误。
// Python: forked_agent.py:101 (classify_compression_error)
func compressionErrorFrom(err error) error {
	return classifyCompressionError(err)
}

// responseFromInvokeResult 从 agent invoke map 结果提取响应消息。
// Python: agent.invoke 返回 AssistantMessage 直返；Go 返回 map（output 键承载）。
func responseFromInvokeResult(result map[string]any) *llm_schema.AssistantMessage {
	if result == nil {
		return nil
	}
	if resp, ok := result["output"].(*llm_schema.AssistantMessage); ok {
		return resp
	}
	return nil
}
