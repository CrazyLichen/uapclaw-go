package agents

import (
	"context"
	"fmt"
	"sort"
	"sync"

	ceinterface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/ability"
	saconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/rail"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
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
// 通过 agentInvoker 接口实现虚分发：
// WarpBaseAgent.Invoke → invoker.invokeImpl → ReActAgent.invokeImpl。
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
}

// ──────────────────────────── 枚举 ────────────────────────────

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
	base := single_agent.NewWarpBaseAgent(card, single_agent.NoopResourceManager{})

	agent := &ReActAgent{
		base:          base,
		config:        config,
		promptBuilder: NewSystemPromptBuilder(),
	}

	// 关键：设置虚分发
	base.SetInvoker(agent)

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

// ──────────────────────────── 导出函数 ────────────────────────────

// invokeImpl 实现 agentInvoker 接口 —— ReAct 循环核心逻辑。
//
// 对应 Python: ReActAgent._inner_invoke()
func (a *ReActAgent) invokeImpl(ctx context.Context, inputs map[string]any, opts ...interfaces.AgentOption) (any, error) {
	agentOpts := interfaces.NewAgentOptions(opts...)
	sess := agentOpts.Session

	if sess == nil {
		var err error
		sess, err = session.NewSession(session.WithSessionID("default_session"))
		if err != nil {
			return nil, fmt.Errorf("创建 session 失败: %w", err)
		}
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
	err := cbc.FireLifecycle(rail.CallbackBeforeInvoke, rail.CallbackAfterInvoke, func() error {
		var innerErr error
		result, innerErr = a.reactLoop(ctx, cbc, sess)
		return innerErr
	})
	if err != nil {
		return nil, err
	}

	if invokeResult, ok := cbc.Extra()["invoke_result"]; ok {
		if r, ok2 := invokeResult.(map[string]any); ok2 {
			return r, nil
		}
	}

	return result, nil
}

// streamImpl 实现 agentInvoker 接口 —— 流式调用。
//
// 对应 Python: ReActAgent._inner_stream()
func (a *ReActAgent) streamImpl(ctx context.Context, inputs map[string]any, opts ...interfaces.AgentOption) (<-chan stream.Schema, error) {
	agentOpts := interfaces.NewAgentOptions(opts...)
	sess := agentOpts.Session

	if sess == nil {
		var err error
		sess, err = session.NewSession(session.WithSessionID("default_session"))
		if err != nil {
			return nil, fmt.Errorf("创建 session 失败: %w", err)
		}
	}

	inputs["_streaming"] = true
	outCh := make(chan stream.Schema, 64)

	go func() {
		defer close(outCh)
		result, err := a.invokeImpl(ctx, inputs, opts...)
		if err != nil {
			outCh <- stream.Schema{
				StreamMode: stream.StreamModeLlmOutput,
				Data:       map[string]any{"error": err.Error(), "result_type": "error"},
			}
			return
		}
		if resultMap, ok := result.(map[string]any); ok {
			outCh <- stream.Schema{
				StreamMode: stream.StreamModeLlmOutput,
				Data:       resultMap,
			}
		}
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

// ──────────────────────────── 非导出函数 ────────────────────────────

// reactLoop ReAct 循环核心。
//
// 对应 Python: ReActAgent._inner_invoke() 中的主循环
func (a *ReActAgent) reactLoop(
	ctx context.Context,
	cbc *rail.AgentCallbackContext,
	sess *session.Session,
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
			modelCtx.AddMessage(llmschema.NewUserMessage(invokeInputs.Query.PlainText()))
		}
	}

	var iterResult map[string]any
	for iteration := 0; iteration < maxIter; iteration++ {
		// steering 注入
		if steeringMsgs := cbc.DrainSteering(); len(steeringMsgs) > 0 && modelCtx != nil {
			for _, msg := range steeringMsgs {
				modelCtx.AddMessage(llmschema.NewUserMessage("[STEERING] " + msg))
			}
		}

		// 调用 LLM
		aiMsg, err := a.callModel(ctx, cbc, modelCtx, tools)
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
			modelCtx.AddMessage(aiMsg)
		}

		// 无工具调用
		if aiMsg == nil || len(aiMsg.ToolCalls) == 0 {
			if cbc.HasPendingSteering() {
				continue
			}
			content := ""
			if aiMsg != nil {
				content = aiMsg.Content
			}
			a.saveContexts(sess)
			iterResult = map[string]any{"output": content, "result_type": "answer"}
			break
		}

		// 执行工具
		if _, err := a.executeToolCalls(ctx, cbc, aiMsg.ToolCalls, sess, modelCtx); err != nil {
			logger.Error(logComponent).Str("event_type", "tool_execution_error").Int("iteration", iteration).Err(err).Msg("工具执行失败")
		}

		// force-finish #2
		if finish := cbc.ConsumeForceFinish(); finish != nil {
			a.saveContexts(sess)
			iterResult = finish.Result
			break
		}
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
	tools []llmschema.ToolCall,
) (*llmschema.AssistantMessage, error) {
	previewMsgs := make([]llmschema.BaseMessage, 0)
	if modelCtx != nil {
		previewMsgs = modelCtx.GetMessages()
	}
	cbc.SetInputs(&rail.ModelCallInputs{
		Messages:     previewMsgs,
		Tools:        tools,
		ModelContext:  modelCtx,
	})

	var result *llmschema.AssistantMessage
	err := rail.ModelCallRail.Execute(ctx, cbc, func() error {
		var e error
		result, e = a.railedModelCall(ctx, cbc)
		return e
	})
	return result, err
}

// railedModelCall 在 Rail 钩子内执行 LLM 调用。
func (a *ReActAgent) railedModelCall(ctx context.Context, cbc *rail.AgentCallbackContext) (*llmschema.AssistantMessage, error) {
	systemPrompt := a.promptBuilder.Build()
	systemMsgs := []llmschema.BaseMessage{llmschema.NewSystemMessage(systemPrompt)}

	modelCtx := cbc.ModelContext()
	var messages []llmschema.BaseMessage
	var contextTools []llmschema.ToolCall

	if modelCtx != nil {
		contextWindow, err := modelCtx.GetContextWindow(ctx, systemMsgs, toolsFromInputs(cbc))
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
		inputs.Tools = contextTools
	}

	llmModel, err := a.getLLM()
	if err != nil {
		return nil, err
	}

	isStreaming, _ := cbc.Extra()["_streaming"].(bool)
	modelName := ""
	if a.config != nil {
		modelName = a.config.ModelNameVal
	}

	if isStreaming {
		return a.callLLMStream(ctx, llmModel, modelName, messages, contextTools)
	}
	return a.callLLMInvoke(ctx, llmModel, modelName, messages, contextTools)
}

// callLLMInvoke 非流式 LLM 调用。
func (a *ReActAgent) callLLMInvoke(
	ctx context.Context,
	llmModel *llm.Model,
	modelName string,
	messages []llmschema.BaseMessage,
	tools []llmschema.ToolCall,
) (*llmschema.AssistantMessage, error) {
	resp, err := (*llmModel).Invoke(ctx, messages,
		llm.WithModel(modelName),
		llm.WithTools(tools),
	)
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
	tools []llmschema.ToolCall,
) (*llmschema.AssistantMessage, error) {
	chunkCh, err := (*llmModel).Stream(ctx, messages,
		llm.WithModel(modelName),
		llm.WithTools(tools),
	)
	if err != nil {
		return nil, fmt.Errorf("LLM stream 失败: %w", err)
	}

	var finalMsg *llmschema.AssistantMessage
	for chunk := range chunkCh {
		if finalMsg == nil {
			finalMsg = llmschema.NewAssistantMessage("")
		}
		finalMsg.Content += chunk.Content
		if len(chunk.ToolCalls) > 0 {
			finalMsg.ToolCalls = append(finalMsg.ToolCalls, chunk.ToolCalls...)
		}
	}
	if finalMsg == nil {
		finalMsg = llmschema.NewAssistantMessage("")
	}
	return finalMsg, nil
}

// executeToolCalls 执行工具调用列表。
func (a *ReActAgent) executeToolCalls(
	ctx context.Context,
	cbc *rail.AgentCallbackContext,
	toolCalls []llmschema.ToolCall,
	sess *session.Session,
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

	// 将 toolCalls 转为指针切片以匹配 Execute 签名
	toolCallPtrs := make([]*llmschema.ToolCall, len(toolCalls))
	for i := range toolCalls {
		toolCallPtrs[i] = &toolCalls[i]
	}
	results := am.Execute(ctx, cbc, toolCallPtrs, sess, "")

	for _, r := range results {
		if r.ToolMsg != nil && modelCtx != nil {
			modelCtx.AddMessage(r.ToolMsg)
		}
	}

	return results, nil
}

// initContext 初始化上下文引擎。
func (a *ReActAgent) initContext(ctx context.Context, sess *session.Session) (ceinterface.ModelContext, error) {
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
		model, err := llm.NewModel(
			llm.WithModelName(a.config.ModelNameVal),
			llm.WithModelProvider(a.config.ModelProvider),
			llm.WithAPIKey(a.config.APIKey),
			llm.WithAPIBase(a.config.APIBase),
		)
		if err != nil {
			initErr = err
			return
		}
		a.llm = &model
	})
	if initErr != nil {
		return nil, fmt.Errorf("LLM 初始化失败: %w", initErr)
	}
	return a.llm, nil
}

// getTools 获取工具列表。
func (a *ReActAgent) getTools() ([]llmschema.ToolCall, error) {
	am := a.getAbilityManager()
	if am == nil {
		return nil, nil
	}
	_ = am.ListToolInfo()
	return nil, nil
}

// getAbilityManager 返回能力管理器。
func (a *ReActAgent) getAbilityManager() *ability.AbilityManager {
	amAny := a.base.AbilityManager()
	if amAny == nil {
		return nil
	}
	if am, ok := amAny.(*ability.AbilityManager); ok {
		return am
	}
	return nil
}

// saveContexts 保存上下文。
func (a *ReActAgent) saveContexts(sess *session.Session) {
	if a.contextEngine == nil || sess == nil {
		return
	}
	if err := a.contextEngine.SaveContexts(context.Background(), sess); err != nil {
		logger.Warn(logComponent).Str("event_type", "save_contexts_error").Err(err).Msg("保存上下文失败")
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

// ──────────────────────────── 非导出函数 ────────────────────────────

// toolsFromInputs 从 AgentCallbackContext 的 Inputs 中提取工具列表。
func toolsFromInputs(cbc *rail.AgentCallbackContext) []llmschema.ToolCall {
	if inputs, ok := cbc.Inputs().(*rail.ModelCallInputs); ok {
		return inputs.Tools
	}
	return nil
}
