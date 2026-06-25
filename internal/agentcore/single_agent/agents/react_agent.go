package agents

import (
	"context"
	"fmt"
	"sort"
	"sync"

	ceinterface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/ability"
	saconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interrupt"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/rail"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/resource"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	cschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// PromptSection 系统提示词的单一节，支持多语言内容。
//
// 对应 Python: PromptSection (openjiuwen/core/single_agent/prompts/builder.py)
type PromptSection struct {
	// Name 节名称（同名称覆盖）
	Name string
	// Content 多语言内容映射：language → content
	Content map[string]string
	// Priority 优先级（数值越小越靠前）
	Priority int
}

// SystemPromptBuilder 基于节的系统提示词构建器。
//
// 对应 Python: SystemPromptBuilder (openjiuwen/core/single_agent/prompts/builder.py)
type SystemPromptBuilder struct {
	// Language 当前语言（默认 "cn"）
	Language string
	// sections 已注册的节映射：name → PromptSection
	sections map[string]PromptSection
}

// ReActAgent ReAct 循环 Agent：Think → Act → Observe。
//
// 内嵌 WarpBaseAgent 获取 BaseAgent 的全部方法，
// 通过 AgentInvoker 接口实现虚分发：
// WarpBaseAgent.Invoke → invoker.InvokeImpl → ReActAgent.InvokeImpl。
//
// 对应 Python: ReActAgent (openjiuwen/core/single_agent/agents/react_agent.py)
type ReActAgent struct {
	// base 内嵌基础 Agent（提供 Invoke/Stream/Configure/Card 等方法）
	base *single_agent.WarpBaseAgent
	// config Agent 配置
	config *saconfig.ReActAgentConfig
	// contextEngine 上下文引擎
	contextEngine ceinterface.ContextEngine
	// llm LLM 模型实例（延迟初始化）
	llm *llm.Model
	// promptBuilder 系统提示词构建器
	promptBuilder *SystemPromptBuilder
	// llmOnce LLM 初始化同步原语
	llmOnce sync.Once
	// kvReleaseWarningLogged KV cache 释放不支持的一次性警告标记
	kvReleaseWarningLogged bool
	// hitlHandler HITL 中断处理器
	hitlHandler *interrupt.ToolInterruptHandler
}

// ──────────────────────────── 枚举 ────────────────────────────

// KVCacheReleaser KV Cache 释放能力接口。
//
// 对应 Python: llm.supports_kv_cache_release() 方法
type KVCacheReleaser interface {
	SupportsKVCacheRelease() bool
}

// KVCacheKwargsBuilder KV Cache 调用参数构建接口。
//
// 对应 Python: llm.build_kv_cache_invoke_kwargs() 方法
type KVCacheKwargsBuilder interface {
	BuildKVCacheInvokeKwargs(sess sessioninterfaces.SessionFacade, enableKVCacheRelease bool) map[string]any
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// logComponent 日志组件标识
	logComponent = logger.ComponentAgentCore
	// defaultLanguage 默认提示词语言
	defaultLanguage = "cn"
	// defaultMaxIterations 默认最大迭代次数
	defaultMaxIterations = 5
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewReActAgent 创建 ReActAgent 实例。
//
// 对应 Python: ReActAgent.__init__(card)
func NewReActAgent(
	card *agentschema.AgentCard,
	config *saconfig.ReActAgentConfig,
) *ReActAgent {
	base := single_agent.NewWarpBaseAgent(card, &resource.NoopResourceManager{})

	agent := &ReActAgent{
		base:          base,
		config:        config,
		promptBuilder: NewSystemPromptBuilder(),
	}

	// 关键：设置虚分发
	base.SetInvoker(agent)

	// 初始化 HITL 中断处理器
	agent.hitlHandler = interrupt.NewToolInterruptHandler(agent)

	return agent
}

// NewSystemPromptBuilder 创建系统提示词构建器。
func NewSystemPromptBuilder() *SystemPromptBuilder {
	return &SystemPromptBuilder{
		Language: defaultLanguage,
		sections: make(map[string]PromptSection),
	}
}

// NewPromptSection 创建提示节。
func NewPromptSection(name string, content map[string]string, priority int) PromptSection {
	return PromptSection{
		Name:     name,
		Content:  content,
		Priority: priority,
	}
}

// InvokeImpl 实现 AgentInvoker 接口 —— ReAct 循环核心逻辑。
//
// 对应 Python: ReActAgent._inner_invoke()
func (a *ReActAgent) InvokeImpl(ctx context.Context, inputs map[string]any, opts ...interfaces.AgentOption) (any, error) {
	agentOpts := interfaces.NewAgentOptions(opts...)
	sess := agentOpts.Session

	var agentSess *session.Session
	if sess == nil {
		sess = session.NewSession(session.WithSessionID("default_session"))
	}

	// 断言 *session.Session 以获取生命周期方法
	if as, ok := sess.(*session.Session); ok {
		agentSess = as
		as.PreRun(ctx)
	}

	query, _ := inputs["query"].(string)
	conversationID, _ := inputs["conversation_id"].(string)

	invokeInputs := &rail.InvokeInputs{
		Query:          rail.NewInvokeQueryString(query),
		ConversationID: conversationID,
	}
	cbc := rail.NewAgentCallbackContext(a, invokeInputs, sess)

	// 设置 extra
	if userID, ok := inputs["user_id"].(string); ok {
		cbc.Extra()["user_id"] = userID
	}
	if streaming, ok := inputs["_streaming"].(bool); ok {
		cbc.Extra()["_streaming"] = streaming
	} else {
		cbc.Extra()["_streaming"] = false
	}
	if sq, ok := inputs["_steering_queue"]; ok {
		if ch, ok2 := sq.(chan string); ok2 {
			cbc.BindSteeringQueue(ch)
		}
	}

	var result map[string]any
	var loopErr error

	err := cbc.FireLifecycle(rail.CallbackBeforeInvoke, rail.CallbackAfterInvoke, func() error {
		// 加载 HITL 中断状态
		hitlState := a.hitlHandler.Load(sess)
		var interruptionState interrupt.ToolInterruptionState
		if hitlState != nil {
			a.hitlHandler.Clear(sess)
			interruptionState = *hitlState
		}
		// ⤵️ Workflow: interruptionState = a.loadInterruptionState(sess)

		// 如果存在中断状态，恢复原始 query
		if hitlState != nil {
			cbc.Extra()["_original_query"] = hitlState.OriginalQuery
		}

		// 初始化上下文
		modelCtx, ctxErr := a.initContext(ctx, sess)
		if ctxErr != nil {
			return fmt.Errorf("初始化上下文失败: %w", ctxErr)
		}
		cbc.SetModelContext(modelCtx)

		startIteration := 0
		if hitlState != nil {
			// HITL 中断恢复分支
			resumeResult, resumeErr := a.hitlHandler.HandleResume(ctx, &interrupt.ResumeContext{
				State:           &interruptionState,
				UserInput:       query,
				Ctx:             cbc,
				ModelContext:    modelCtx,
				Session:         sess,
				InvokeInputs:    invokeInputs,
				ExecuteToolCall: a.makeExecuteToolCallFunc(),
			})
			if resumeErr != nil {
				return resumeErr
			}
			if resumeResult != nil {
				// 仍有中断，invokeInputs.result 已设置
				result = resumeResult
				return nil
			}
			// 无新中断，从恢复起始迭代继续
			if si, ok := cbc.Extra()[interrupt.ResumeStartIterationKey].(int); ok {
				startIteration = si
				delete(cbc.Extra(), interrupt.ResumeStartIterationKey)
			}
		} else {
			// 正常路径：添加 UserMessage
			if query != "" && modelCtx != nil {
				_, _ = modelCtx.AddMessages(ctx, llmschema.NewUserMessage(query))
			}
		}

		// 调用 ReAct 循环
		if invokeInputs.Result == nil {
			result, loopErr = a.reactLoop(ctx, cbc, sess, startIteration)
		}
		return loopErr
	})

	// context.Canceled 时清除上下文消息
	if err != nil && ctx.Err() == context.Canceled {
		a.ClearContextMessages(sess)
	}
	if err != nil && ctx.Err() != context.Canceled {
		return nil, err
	}

	// 生命周期后处理
	if agentSess != nil {
		a.saveContexts(sess)
		_ = agentSess.CloseStream()
		_ = agentSess.Commit(ctx)
	}

	if invokeResult, ok := cbc.Extra()["invoke_result"]; ok {
		if r, ok2 := invokeResult.(map[string]any); ok2 {
			return r, nil
		}
	}

	return result, nil
}

// StreamImpl 实现 AgentInvoker 接口 —— 流式调用。
//
// 对应 Python: ReActAgent.stream()
func (a *ReActAgent) StreamImpl(ctx context.Context, inputs map[string]any, opts ...interfaces.AgentOption) (<-chan stream.Schema, error) {
	agentOpts := interfaces.NewAgentOptions(opts...)
	sess := agentOpts.Session

	if sess == nil {
		sess = session.NewSession(session.WithSessionID("default_session"))
		// 补充到 opts 中
		opts = append(opts, interfaces.WithSession(sess))
	}

	// AgentSession 生命周期断言（直接断言 *session.Session）
	var agentSess *session.Session
	isAgentSess := false
	if as, ok := sess.(*session.Session); ok {
		agentSess = as
		isAgentSess = true
		if err := as.PreRun(ctx, inputs); err != nil {
			logger.Warn(logComponent).Err(err).Msg("PreRun 失败")
		}
	}

	inputs["_streaming"] = true
	outCh := make(chan stream.Schema, 64)

	go func() {
		defer close(outCh)
		a.innerStream(ctx, sess, agentSess, isAgentSess, inputs, opts, outCh)
	}()

	return outCh, nil
}

// AddPromptBuilderSection 添加或替换提示节。
func (a *ReActAgent) AddPromptBuilderSection(name string, content map[string]string, priority int) {
	a.promptBuilder.AddSection(PromptSection{
		Name:     name,
		Content:  content,
		Priority: priority,
	})
}

// Configure 配置 ReActAgent。
//
// 对应 Python: ReActAgent.configure(config)
func (a *ReActAgent) Configure(ctx context.Context, config *saconfig.ReActAgentConfig) error {
	if config == nil {
		return fmt.Errorf("config 不能为 nil")
	}
	if err := config.Validate(); err != nil {
		return fmt.Errorf("config 校验失败: %w", err)
	}
	a.config = config
	a.promptBuilder = NewSystemPromptBuilder()
	if config.PromptTemplateName != "" {
		a.AddPromptBuilderSection("identity", map[string]string{defaultLanguage: config.PromptTemplateName}, 10)
	}
	return nil
}

// CallbackManager 返回回调管理器（满足 RailAgent 接口）。
func (a *ReActAgent) CallbackManager() *rail.AgentCallbackManager {
	return a.base.CallbackManager()
}

// AgentID 返回 Agent 唯一标识（满足 RailAgent 接口）。
func (a *ReActAgent) AgentID() string {
	return a.base.AgentID()
}

// ContextEngine 返回上下文引擎（满足 InterruptAgent 接口）。
func (a *ReActAgent) ContextEngine() ceinterface.ContextEngine {
	return a.contextEngine
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// reactLoop ReAct 循环核心。
//
// 对应 Python: ReActAgent._inner_invoke() 中的主循环
func (a *ReActAgent) reactLoop(
	ctx context.Context,
	cbc *rail.AgentCallbackContext,
	sess sessioninterfaces.SessionFacade,
	startIteration int,
) (map[string]any, error) {
	maxIter := defaultMaxIterations
	if a.config != nil && a.config.MaxIterations > 0 {
		maxIter = a.config.MaxIterations
	}

	modelCtx, err := a.initContext(ctx, sess)
	if err != nil {
		return nil, fmt.Errorf("初始化上下文失败: %w", err)
	}
	cbc.SetModelContext(modelCtx)

	tools, _ := a.getTools()

	if invokeInputs, ok := cbc.Inputs().(*rail.InvokeInputs); ok && invokeInputs.Query.PlainText() != "" {
		if modelCtx != nil {
			_, _ = modelCtx.AddMessages(ctx, llmschema.NewUserMessage(invokeInputs.Query.PlainText()))
		}
	}

	var iterResult map[string]any
	for iteration := startIteration; iteration < maxIter; iteration++ {
		// steering 注入
		if steeringMsgs := cbc.DrainSteering(); len(steeringMsgs) > 0 && modelCtx != nil {
			for _, msg := range steeringMsgs {
				_, _ = modelCtx.AddMessages(ctx, llmschema.NewUserMessage("[STEERING] "+msg))
			}
		}

		// 调用 LLM
		aiMsg, err := a.callModel(ctx, cbc, modelCtx, tools, sess)
		if err != nil {
			return nil, fmt.Errorf("迭代 %d 模型调用失败: %w", iteration, err)
		}

		// force-finish #1
		if finish := cbc.ConsumeForceFinish(); finish != nil {
			a.saveContexts(sess)
			iterResult = finish.Result
			break
		}

		if aiMsg != nil && modelCtx != nil {
			_, _ = modelCtx.AddMessages(ctx, aiMsg)
		}

		// 无工具调用
		if aiMsg == nil || len(aiMsg.ToolCalls) == 0 {
			if cbc.HasPendingSteering() {
				continue
			}
			content := ""
			if aiMsg != nil {
				content = aiMsg.Content.Text()
			}
			a.saveContexts(sess)
			iterResult = map[string]any{"output": content, "result_type": "answer"}
			break
		}

		// 执行工具
		results, err := a.executeToolCalls(ctx, cbc, aiMsg.ToolCalls, sess, modelCtx)
		if err != nil {
			logger.Error(logComponent).Str("event_type", "tool_execution_error").Int("iteration", iteration).Err(err).Msg("工具执行失败")
		}

		// force-finish #2
		if finish := cbc.ConsumeForceFinish(); finish != nil {
			a.saveContexts(sess)
			iterResult = finish.Result
			break
		}

		// HITL 中断检测
		originalQuery := ""
		if oq, ok := cbc.Extra()["_original_query"].(string); ok {
			originalQuery = oq
		}
		// 将 []ExecuteResult 转为 []any 供 BuildInterruptState 使用
		anyResults := make([]any, len(results))
		for i, r := range results {
			anyResults[i] = [2]any{r.Result, r.ToolMsg}
		}
		hitlInterrupt, _ := a.AfterExecuteToolCallForHITL(
			anyResults, aiMsg.ToolCalls, aiMsg, iteration, originalQuery,
		)
		if hitlInterrupt != nil {
			if invokeInputs, ok := cbc.Inputs().(*rail.InvokeInputs); ok {
				_, _ = a.CommitInterrupt(ctx, hitlInterrupt, modelCtx, sess, invokeInputs, nil)
			}
			break
		}
		// ⤵️ 6.11: Workflow 中断检测
	}

	if iterResult == nil {
		iterResult = map[string]any{"output": "Max iterations reached without completion", "result_type": "error"}
	}

	if invokeInputs, ok := cbc.Inputs().(*rail.InvokeInputs); ok {
		invokeInputs.Result = iterResult
	}

	return iterResult, nil
}

// callModel 调用 LLM（含 Rail 钩子）。
func (a *ReActAgent) callModel(
	ctx context.Context,
	cbc *rail.AgentCallbackContext,
	modelCtx ceinterface.ModelContext,
	tools []*cschema.ToolInfo,
	sess sessioninterfaces.SessionFacade,
) (*llmschema.AssistantMessage, error) {
	previewMsgs := make([]llmschema.BaseMessage, 0)
	if modelCtx != nil {
		previewMsgs = modelCtx.GetMessages(0, true)
	}
	cbc.SetInputs(&rail.ModelCallInputs{
		Messages:     previewMsgs,
		ModelContext: modelCtx,
	})

	var result *llmschema.AssistantMessage
	err := rail.ModelCallRail.Execute(ctx, cbc, func() error {
		var e error
		result, e = a.railedModelCall(ctx, cbc, sess)
		return e
	})
	return result, err
}

// railedModelCall 在 Rail 钩子内执行 LLM 调用。
func (a *ReActAgent) railedModelCall(ctx context.Context, cbc *rail.AgentCallbackContext, sess sessioninterfaces.SessionFacade) (*llmschema.AssistantMessage, error) {
	systemPrompt := a.promptBuilder.Build()
	systemMsgs := []llmschema.BaseMessage{llmschema.NewSystemMessage(systemPrompt)}

	modelCtx := cbc.ModelContext()

	// KV Cache 释放逻辑（对应 Python _railed_model_call L686-720）
	var enableKVRelease bool
	if a.config != nil {
		enableKVRelease = a.config.ContextEngineConfig.EnableKVCacheRelease
	}

	// 提前获取 LLM 实例（KV Cache 检查和 GetContextWindow 都需要）
	llmModel, llmErr := a.getLLM()

	var supportsKVRelease bool
	if llmModel != nil {
		supportsKVRelease = llmModel.SupportsKVCacheRelease()
	}

	// 不支持时一次性警告
	if enableKVRelease && !supportsKVRelease && !a.kvReleaseWarningLogged {
		logger.Warn(logComponent).
			Str("event_type", "kv_cache_release_not_supported").
			Msg("enable_kv_cache_release 已启用但当前 LLM 不支持 KV Cache 释放")
		a.kvReleaseWarningLogged = true
	}

	// 构建 GetContextWindow 选项：支持 KV Cache 时传入 model
	var contextWindowOpts []ceinterface.Option
	if enableKVRelease && supportsKVRelease && llmModel != nil {
		contextWindowOpts = append(contextWindowOpts, ceinterface.WithModel(llmModel))
	}

	var messages []llmschema.BaseMessage
	var contextTools []*cschema.ToolInfo

	if modelCtx != nil {
		contextWindow, err := modelCtx.GetContextWindow(ctx, systemMsgs, nil, 0, 0, contextWindowOpts...)
		if err != nil {
			return nil, fmt.Errorf("获取上下文窗口失败: %w", err)
		}
		messages = contextWindow.GetMessages()
		contextTools = contextWindow.GetTools()
	} else {
		messages = systemMsgs
	}

	if inputs, ok := cbc.Inputs().(*rail.ModelCallInputs); ok {
		inputs.Messages = messages
	}

	// LLM 实例必须在调用前可用
	if llmErr != nil {
		return nil, llmErr
	}
	if llmModel == nil {
		return nil, fmt.Errorf("LLM 实例为 nil")
	}

	// 构建 KV Cache extra kwargs（对应 Python L736-742）
	extraKVPairs := llmModel.BuildKVCacheInvokeKwargs(sess, enableKVRelease)

	isStreaming, _ := cbc.Extra()["_streaming"].(bool)
	modelName := ""
	if a.config != nil {
		modelName = a.config.ModelNameVal
	}

	if isStreaming {
		return a.callLLMStream(ctx, llmModel, modelName, messages, contextTools, sess, extraKVPairs)
	}
	return a.callLLMInvoke(ctx, llmModel, modelName, messages, contextTools, extraKVPairs)
}

// callLLMInvoke 非流式 LLM 调用。
func (a *ReActAgent) callLLMInvoke(
	ctx context.Context,
	llmModel *llm.Model,
	modelName string,
	messages []llmschema.BaseMessage,
	tools []*cschema.ToolInfo,
	extraKVPairs map[string]any,
) (*llmschema.AssistantMessage, error) {
	toolProviders := make([]cschema.ToolInfoProvider, len(tools))
	for i, t := range tools {
		toolProviders[i] = t
	}
	msgsParam := model_clients.NewMessagesParam(messages...)

	// 构建 invoke 选项（含 KV Cache extra kwargs）
	invokeOpts := []model_clients.InvokeOption{
		model_clients.WithInvokeModel(modelName),
		model_clients.WithTools(toolProviders...),
	}
	if len(extraKVPairs) > 0 {
		invokeOpts = append(invokeOpts, model_clients.WithInvokeExtra(extraKVPairs))
	}

	resp, err := (*llmModel).Invoke(ctx, msgsParam, invokeOpts...)
	if err != nil {
		return nil, fmt.Errorf("LLM invoke 失败: %w", err)
	}
	return resp, nil
}

// callLLMStream 流式 LLM 调用。
func (a *ReActAgent) callLLMStream(
	ctx context.Context,
	llmModel *llm.Model,
	modelName string,
	messages []llmschema.BaseMessage,
	tools []*cschema.ToolInfo,
	sess sessioninterfaces.SessionFacade,
	extraKVPairs map[string]any,
) (*llmschema.AssistantMessage, error) {
	toolProviders := make([]cschema.ToolInfoProvider, len(tools))
	for i, t := range tools {
		toolProviders[i] = t
	}
	msgsParam := model_clients.NewMessagesParam(messages...)

	// 构建 stream 选项（含 KV Cache extra kwargs）
	streamOpts := []model_clients.StreamOption{
		model_clients.WithStreamModel(modelName),
		model_clients.WithStreamTools(toolProviders...),
	}
	if len(extraKVPairs) > 0 {
		streamOpts = append(streamOpts, model_clients.WithStreamExtra(extraKVPairs))
	}

	chunkCh, err := (*llmModel).Stream(ctx, msgsParam, streamOpts...)
	if err != nil {
		return nil, fmt.Errorf("LLM stream 失败: %w", err)
	}

	var finalMsg *llmschema.AssistantMessage
	chunkIndex := 0
	for chunk := range chunkCh {
		if finalMsg == nil {
			finalMsg = llmschema.NewAssistantMessage("")
		}
		finalMsg.Content = llmschema.NewTextContent(finalMsg.Content.Text() + chunk.Content.Text())
		if len(chunk.ToolCalls) > 0 {
			finalMsg.ToolCalls = append(finalMsg.ToolCalls, chunk.ToolCalls...)
		}
		if chunk.ReasoningContent != "" {
			finalMsg.ReasoningContent += chunk.ReasoningContent
		}
		if chunk.UsageMetadata != nil {
			finalMsg.UsageMetadata = chunk.UsageMetadata
		}

		// 实时写入 session stream（对齐 Python railed_model_call L776-809）
		if sess != nil {
			if chunk.Content.Text() != "" {
				_ = sess.WriteStream(ctx, &stream.OutputSchema{
					Type:  "llm_output",
					Index: chunkIndex,
					Payload: map[string]any{
						"content":     chunk.Content.Text(),
						"result_type": "answer",
					},
				})
				chunkIndex++
			}
			if chunk.ReasoningContent != "" {
				_ = sess.WriteStream(ctx, &stream.OutputSchema{
					Type:  "llm_reasoning",
					Index: chunkIndex,
					Payload: map[string]any{
						"content":     chunk.ReasoningContent,
						"result_type": "answer",
					},
				})
				chunkIndex++
			}
		}
	}
	if finalMsg == nil {
		finalMsg = llmschema.NewAssistantMessage("")
	}

	// 写入 usage_metadata（对齐 Python L804-809）
	if sess != nil && finalMsg.UsageMetadata != nil {
		_ = sess.WriteStream(ctx, &stream.OutputSchema{
			Type:  "llm_usage",
			Index: 0,
			Payload: map[string]any{
				"usage_metadata": finalMsg.UsageMetadata,
				"result_type":    "answer",
			},
		})
	}

	return finalMsg, nil
}

// executeToolCalls 执行工具调用列表。
func (a *ReActAgent) executeToolCalls(
	ctx context.Context,
	cbc *rail.AgentCallbackContext,
	toolCalls []*llmschema.ToolCall,
	sess sessioninterfaces.SessionFacade,
	modelCtx ceinterface.ModelContext,
) ([]ability.ExecuteResult, error) {
	if len(toolCalls) == 0 {
		return nil, nil
	}

	for _, tc := range toolCalls {
		argsPreview := tc.Arguments
		if len(argsPreview) > 100 {
			argsPreview = argsPreview[:100]
		}
		logger.Info(logComponent).
			Str("event_type", "tool_call").
			Str("tool_name", tc.Name).
			Str("args_preview", argsPreview).
			Msg("执行工具调用")
	}

	am := a.getAbilityManager()
	if am == nil {
		return nil, fmt.Errorf("AbilityManager 未初始化")
	}

	results := am.Execute(ctx, cbc, toolCalls, sess, "")

	for _, r := range results {
		if r.ToolMsg != nil && modelCtx != nil {
			_, _ = modelCtx.AddMessages(ctx, r.ToolMsg)
		}
	}

	return results, nil
}

// initContext 初始化上下文引擎。
func (a *ReActAgent) initContext(ctx context.Context, sess sessioninterfaces.SessionFacade) (ceinterface.ModelContext, error) {
	if a.contextEngine == nil {
		return nil, nil
	}
	return a.contextEngine.CreateContext(ctx, "default_context", sess)
}

// getLLM 获取 LLM 实例（延迟初始化）。
func (a *ReActAgent) getLLM() (*llm.Model, error) {
	if a.llm != nil {
		return a.llm, nil
	}
	if a.config == nil {
		return nil, fmt.Errorf("config 未设置")
	}
	var initErr error
	a.llmOnce.Do(func() {
		clientCfg := &llmschema.ModelClientConfig{
			ClientProvider: a.config.ModelProvider,
			APIKey:         a.config.APIKey,
			APIBase:        a.config.APIBase,
		}
		modelCfg := &llmschema.ModelRequestConfig{
			ModelName: a.config.ModelNameVal,
		}
		model, err := llm.NewModel(clientCfg, modelCfg)
		if err != nil {
			initErr = err
			return
		}
		a.llm = model
	})
	if initErr != nil {
		return nil, fmt.Errorf("LLM 初始化失败: %w", initErr)
	}
	return a.llm, nil
}

// getTools 获取工具列表。
func (a *ReActAgent) getTools() ([]*cschema.ToolInfo, error) {
	am := a.getAbilityManager()
	if am == nil {
		return nil, nil
	}
	tools, _ := am.ListToolInfo(context.Background(), nil)
	return tools, nil
}

// getAbilityManager 返回能力管理器。
func (a *ReActAgent) getAbilityManager() *ability.AbilityManager {
	amAny := a.base.AbilityManager()
	am, ok := amAny.(*ability.AbilityManager)
	if !ok {
		return nil
	}
	return am
}

// saveContexts 保存上下文。
func (a *ReActAgent) saveContexts(sess sessioninterfaces.SessionFacade) {
	if a.contextEngine == nil || sess == nil {
		return
	}
	if _, err := a.contextEngine.SaveContexts(context.Background(), sess, nil); err != nil {
		logger.Warn(logComponent).Str("event_type", "save_contexts_error").Err(err).Msg("保存上下文失败")
	}
}

// makeExecuteToolCallFunc 创建 ExecuteToolCallFunc 闭包，
// 将 ReActAgent.executeToolCalls 适配为 interrupt.ExecuteToolCallFunc 类型。
func (a *ReActAgent) makeExecuteToolCallFunc() interrupt.ExecuteToolCallFunc {
	return func(
		ctx context.Context,
		cbc *rail.AgentCallbackContext,
		toolCalls []*llmschema.ToolCall,
		sess sessioninterfaces.SessionFacade,
		modelCtx ceinterface.ModelContext,
	) ([]any, error) {
		results, err := a.executeToolCalls(ctx, cbc, toolCalls, sess, modelCtx)
		if err != nil {
			return nil, err
		}
		anyResults := make([]any, len(results))
		for i, r := range results {
			anyResults[i] = [2]any{r.Result, r.ToolMsg}
		}
		return anyResults, nil
	}
}

// ──────────────────────────── SystemPromptBuilder 方法 ────────────────────────────

// AddSection 添加或替换节。
func (b *SystemPromptBuilder) AddSection(section PromptSection) *SystemPromptBuilder {
	b.sections[section.Name] = section
	return b
}

// RemoveSection 移除指定名称的节。
func (b *SystemPromptBuilder) RemoveSection(name string) *SystemPromptBuilder {
	delete(b.sections, name)
	return b
}

// HasSection 检查节是否存在。
func (b *SystemPromptBuilder) HasSection(name string) bool {
	_, ok := b.sections[name]
	return ok
}

// Build 按优先级排序并拼接为完整系统提示词。
func (b *SystemPromptBuilder) Build() string {
	sections := make([]PromptSection, 0, len(b.sections))
	for _, s := range b.sections {
		sections = append(sections, s)
	}
	sort.Slice(sections, func(i, j int) bool {
		return sections[i].Priority < sections[j].Priority
	})

	parts := make([]string, 0, len(sections))
	for _, s := range sections {
		if content := s.Render(b.Language); content != "" {
			parts = append(parts, content)
		}
	}

	result := ""
	for i, part := range parts {
		if i > 0 {
			result += "\n\n"
		}
		result += part
	}
	return result
}

// ──────────────────────────── PromptSection 方法 ────────────────────────────

// Render 渲染指定语言的内容。
func (s *PromptSection) Render(language string) string {
	if content, ok := s.Content[language]; ok {
		return content
	}
	if content, ok := s.Content[defaultLanguage]; ok {
		return content
	}
	for _, v := range s.Content {
		return v
	}
	return ""
}
