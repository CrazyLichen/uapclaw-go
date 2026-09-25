package evolution

import (
	"context"
	"fmt"

	ceservice "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/service"
	hinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	saconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/config"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── 接口 ────────────────────────────

// taskMemoryServicer 记忆服务接口（非导出，用于测试 mock）。
//
// 提取 TaskMemoryService 的关键方法，ContextEvolutionRail 持有此接口而非具体类型。
// 实现 ceservice.SummarizeTrajectorier 接口，可直接传给 SummarizeTrajectories。
// 对齐 Python: ContextEvolutionRail 使用 TaskMemoryService 实例。
type taskMemoryServicer interface {
	// Retrieve 检索记忆
	Retrieve(ctx context.Context, userID string, query string) (*ceservice.RetrieveResult, error)
	// LoadMemories 加载已有记忆
	LoadMemories(ctx context.Context, userID string) error
	// SummaryAlgorithm 返回总结算法名称
	SummaryAlgorithm() string
	// Summarize 总结轨迹
	Summarize(ctx context.Context, userID string, matts string, query string, trajectories []string, extraKwargs ...map[string]any) (*ceservice.SummarizeResult, error)
}

// ──────────────────────────── 结构体 ────────────────────────────

// ContextEvolutionRail 上下文演化轨道。
//
// 嵌入 DeepAgentRail（对齐 Python ContextEvolutionRail(DeepAgentRail)），
// Priority=50，在 BeforeTaskIteration 中检索记忆注入系统提示词，
// 在 AfterTaskIteration 中恢复原始提示词、标注 memoriesUsed、自动总结轨迹。
//
// 设计决策：
//   - 直接嵌入 DeepAgentRail，不通过 EvolutionExtension 模式（与 SkillEvolutionRail 不同）
//   - 记忆注入通过 type-assert Agent → DeepAgentInterface → ReactAgent → Config → PromptTemplate
//   - PromptTemplate 修改通过 ReActAgent.Configure() 应用（对齐 Python 直接赋值语义）
//
// Python: openjiuwen/harness/rails/evolution/context_evolution_rail.py
type ContextEvolutionRail struct {
	rails.DeepAgentRail

	// userID 用户标识
	userID string
	// memoryService 记忆服务（接口持有，便于测试 mock）
	memoryService taskMemoryServicer
	// injectMemoriesInContext 是否将记忆注入系统提示词
	injectMemoriesInContext bool
	// autoSummarize 是否自动总结轨迹
	autoSummarize bool
	// autoSummarizeMattsMode 自动总结的 MaTTS 模式
	autoSummarizeMattsMode string

	// memoriesUsed 当前迭代使用的记忆数量（每次 beforeTaskIteration 重置）
	memoriesUsed int
	// originalPromptTemplate 注入记忆前的原始提示词模板（用于恢复）
	originalPromptTemplate []map[string]any

	// lastRetrievedQuery 上次检索的查询（简单缓存）
	lastRetrievedQuery string
	// lastRetrievalResult 上次检索的结果
	lastRetrievalResult *ceservice.RetrieveResult

	// agent Agent 引用（首次 beforeTaskIteration 时捕获）
	agent agentinterfaces.BaseAgent
	// currentQuery 当前迭代的查询（beforeTaskIteration 保存，afterTaskIteration 使用）
	currentQuery string
}

// ContextEvolutionRailOption 构造选项函数。
type ContextEvolutionRailOption func(*ContextEvolutionRail)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// contextEvolutionPriority 上下文演化轨道优先级
	// 对齐 Python: ContextEvolutionRail.priority = 50
	contextEvolutionPriority = 50

	// memoryBlockHeader 记忆注入块头部
	// 对齐 Python: "Some Related Experience to help you complete the task:\n"
	memoryBlockHeader = "Some Related Experience to help you complete the task:\n"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewContextEvolutionRail 创建上下文演化轨道实例。
//
// memoryService 为 nil 时尝试创建默认 TaskMemoryService（对齐 Python TaskMemoryService() 无参创建）。
// 如果创建失败（如 ceconfig 未配置 API_KEY），打印警告并跳过记忆功能。
//
// 对齐 Python: ContextEvolutionRail(user_id, memory_service, inject_memories_in_context, auto_summarize, auto_summarize_matts_mode)
func NewContextEvolutionRail(
	userID string,
	memoryService *ceservice.TaskMemoryService,
	opts ...ContextEvolutionRailOption,
) *ContextEvolutionRail {
	r := &ContextEvolutionRail{
		userID:                  userID,
		injectMemoriesInContext: true,
		autoSummarize:           true,
		autoSummarizeMattsMode:  "none",
	}
	// 注入 TaskMemoryService
	if memoryService != nil {
		r.memoryService = memoryService
	}
	for _, opt := range opts {
		opt(r)
	}

	// 对齐 Python：memoryService 为 nil 时尝试无参创建（对齐 Python TaskMemoryService()）
	if r.memoryService == nil {
		svc, err := ceservice.NewTaskMemoryService()
		if err != nil {
			logger.Warn(logComponent).Err(err).Str("user_id", r.userID).Msg("ContextEvolutionRail 无法创建默认 TaskMemoryService，跳过记忆功能")
		} else {
			r.memoryService = svc
		}
	}

	// 加载已有记忆（对齐 Python __init__ 末尾调用 load_memories）
	if r.memoryService != nil {
		if err := r.memoryService.LoadMemories(context.Background(), r.userID); err != nil {
			logger.Warn(logComponent).Err(err).Str("user_id", r.userID).Msg("ContextEvolutionRail 加载记忆失败")
		}
	}

	logger.Info(logComponent).
		Str("user_id", userID).
		Bool("inject", r.injectMemoriesInContext).
		Bool("auto_summarize", r.autoSummarize).
		Msg("ContextEvolutionRail 初始化完成")

	return r
}

// WithInjectMemoriesInContext 设置是否注入记忆到上下文。
func WithInjectMemoriesInContext(v bool) ContextEvolutionRailOption {
	return func(r *ContextEvolutionRail) { r.injectMemoriesInContext = v }
}

// WithAutoSummarize 设置是否自动总结轨迹。
func WithAutoSummarize(v bool) ContextEvolutionRailOption {
	return func(r *ContextEvolutionRail) { r.autoSummarize = v }
}

// WithAutoSummarizeMattsMode 设置自动总结的 MaTTS 模式。
func WithAutoSummarizeMattsMode(mode string) ContextEvolutionRailOption {
	return func(r *ContextEvolutionRail) { r.autoSummarizeMattsMode = mode }
}

// WithMemoryService 设置记忆服务（用于测试注入 mock）。
func WithMemoryService(svc taskMemoryServicer) ContextEvolutionRailOption {
	return func(r *ContextEvolutionRail) { r.memoryService = svc }
}

// Priority 返回轨道优先级。
// 对齐 Python: ContextEvolutionRail.priority = 50
func (r *ContextEvolutionRail) Priority() int { return contextEvolutionPriority }

// GetCallbacks 注册回调。
//
// 合并 DeepAgentRail 基础回调 + BeforeTaskIteration / AfterTaskIteration。
// 对齐 Python: ContextEvolutionRail 注册 before_task_iteration / after_task_iteration。
func (r *ContextEvolutionRail) GetCallbacks() map[agentinterfaces.AgentCallbackEvent]cb.PerAgentCallbackFunc {
	callbacks := r.DeepAgentRail.GetCallbacks()
	callbacks[agentinterfaces.CallbackBeforeTaskIteration] = func(ctx context.Context, railCtx any) error {
		return r.beforeTaskIteration(ctx, railCtx)
	}
	callbacks[agentinterfaces.CallbackAfterTaskIteration] = func(ctx context.Context, railCtx any) error {
		return r.afterTaskIteration(ctx, railCtx)
	}
	return callbacks
}

// MemoriesUsed 返回当前迭代使用的记忆数量。
func (r *ContextEvolutionRail) MemoriesUsed() int { return r.memoriesUsed }

// CurrentQuery 返回当前迭代的查询。
func (r *ContextEvolutionRail) CurrentQuery() string { return r.currentQuery }

// AgentRef 返回 Agent 引用。
func (r *ContextEvolutionRail) AgentRef() agentinterfaces.BaseAgent { return r.agent }

// ──────────────────────────── 非导出函数 ────────────────────────────

// beforeTaskIteration 在每次任务迭代前检索记忆并注入系统提示词。
//
// 对齐 Python: ContextEvolutionRail.before_task_iteration(ctx) 108-193 行
//
// 流程：
//  1. 重置每次迭代状态
//  2. 捕获 agent 引用
//  3. 从 TaskIterationInputs 获取 query
//  4. 检索记忆（使用简单缓存避免重复检索）
//  5. 注入记忆到 system message 末尾（通过 ReActAgent.Configure）
func (r *ContextEvolutionRail) beforeTaskIteration(ctx context.Context, cbcRaw any) error {
	cbc, ok := cbcRaw.(*agentinterfaces.AgentCallbackContext)
	if !ok || cbc == nil {
		return nil
	}

	// 1. 重置每次迭代状态（对齐 Python: self.memories_used = 0; self.original_prompt_template = None）
	r.memoriesUsed = 0
	r.originalPromptTemplate = nil

	// 2. 捕获 agent 引用（对齐 Python: if self._agent is None: self._agent = ctx.agent）
	if r.agent == nil {
		r.agent = cbc.Agent()
	}

	// 3. 获取 query（对齐 Python: query = getattr(ctx.inputs, "query", None) or ""）
	inputs := cbc.Inputs()
	taskInputs, ok := inputs.(*agentinterfaces.TaskIterationInputs)
	if !ok || taskInputs == nil {
		return nil
	}
	query := taskInputs.Query
	if query == "" {
		return nil
	}

	// 4. 保存 currentQuery（对齐 Python: self._current_query = query）
	r.currentQuery = query

	// 5. 检索记忆
	if r.memoryService == nil {
		return nil
	}

	var memoryResult *ceservice.RetrieveResult
	// 对齐 Python: 如果上次检索查询相同，复用缓存结果
	if r.lastRetrievedQuery == query && r.lastRetrievalResult != nil {
		memoryResult = r.lastRetrievalResult
		logger.Info(logComponent).Msg("复用缓存的记忆检索结果")
	} else {
		result, err := r.memoryService.Retrieve(ctx, r.userID, query)
		if err != nil {
			logger.Error(logComponent).Err(err).Msg("检索记忆失败")
			return nil // 不中断 Agent（对齐 Python: except → return）
		}
		memoryResult = result
		r.lastRetrievedQuery = query
		r.lastRetrievalResult = result
	}

	memoryString := memoryResult.MemoryString
	r.memoriesUsed = len(memoryResult.RetrievedMemory)
	logger.Info(logComponent).Int("memories_used", r.memoriesUsed).Msg("检索到记忆")

	// 6. 记忆注入（对齐 Python: if not (self.memories_used > 0 and memory_string and self.inject_memories_in_context): return）
	if r.memoriesUsed <= 0 || memoryString == "" || !r.injectMemoriesInContext {
		return nil
	}

	// type-assert: BaseAgent → DeepAgentInterface（对齐 Python: inner_agent = getattr(agent, "react_agent", agent)）
	deepAgent, ok := cbc.Agent().(hinterfaces.DeepAgentInterface)
	if !ok {
		logger.Warn(logComponent).Msg("Agent 非 DeepAgentInterface，跳过记忆注入")
		return nil
	}

	reactAgent := deepAgent.ReactAgent()
	if reactAgent == nil {
		logger.Warn(logComponent).Msg("ReactAgent 为 nil，跳过记忆注入")
		return nil
	}

	agentCfg := reactAgent.Config()
	reactCfg, ok := agentCfg.(*saconfig.ReActAgentConfig)
	if !ok {
		logger.Warn(logComponent).Msg("Agent config 非 ReActAgentConfig，跳过记忆注入")
		return nil
	}

	// 深拷贝原始 PromptTemplate（对齐 Python: self.original_prompt_template = [dict(msg) for msg in inner_agent.config.prompt_template]）
	r.originalPromptTemplate = deepCopyPromptTemplate(reactCfg.PromptTemplate)

	// 构造记忆块（对齐 Python: memory_block = f"Some Related Experience...\n{memory_string}\n"）
	memoryBlock := memoryBlockHeader + memoryString + "\n"

	// 在 system message 末尾追加记忆块（对齐 Python 180-192 行）
	newTemplate := make([]map[string]any, len(reactCfg.PromptTemplate))
	for i, msg := range reactCfg.PromptTemplate {
		newMsg := make(map[string]any, len(msg))
		for k, v := range msg {
			newMsg[k] = v
		}
		if role, _ := msg["role"].(string); role == "system" {
			content, _ := msg["content"].(string)
			newMsg["content"] = content + "\n\n" + memoryBlock
		}
		newTemplate[i] = newMsg
	}

	// 应用新 PromptTemplate（Go 通过 Configure 方法，Python 直接赋值）
	newReactCfg := *reactCfg
	newReactCfg.PromptTemplate = newTemplate
	if err := reactAgent.Configure(ctx, &newReactCfg); err != nil {
		logger.Error(logComponent).Err(err).Msg("注入记忆到 PromptTemplate 失败")
		r.originalPromptTemplate = nil
		return nil
	}

	logger.Debug(logComponent).Msg("已注入记忆上下文到 Agent 系统提示词")
	return nil
}

// afterTaskIteration 在每次任务迭代后恢复原始提示词、标注 memoriesUsed、自动总结轨迹。
//
// 对齐 Python: ContextEvolutionRail.after_task_iteration(ctx) 199-243 行
//
// 流程：
//  1. 恢复原始 PromptTemplate
//  2. 标注 memoriesUsed 到回调上下文
//  3. 自动总结轨迹（仅支持 matts_mode="none"）
func (r *ContextEvolutionRail) afterTaskIteration(ctx context.Context, cbcRaw any) error {
	cbc, ok := cbcRaw.(*agentinterfaces.AgentCallbackContext)
	if !ok || cbc == nil {
		return nil
	}

	// 1. 恢复原始提示词模板（对齐 Python 204-212 行）
	if r.originalPromptTemplate != nil {
		deepAgent, ok := cbc.Agent().(hinterfaces.DeepAgentInterface)
		if ok && deepAgent != nil {
			reactAgent := deepAgent.ReactAgent()
			if reactAgent != nil {
				agentCfg := reactAgent.Config()
				if reactCfg, ok := agentCfg.(*saconfig.ReActAgentConfig); ok {
					newReactCfg := *reactCfg
					newReactCfg.PromptTemplate = r.originalPromptTemplate
					_ = reactAgent.Configure(ctx, &newReactCfg)
				}
			}
		}
		r.originalPromptTemplate = nil
		logger.Debug(logComponent).Msg("已恢复原始 Agent 系统提示词")
	}

	// 2. 标注 memoriesUsed（对齐 Python: result["memories_used"] = self.memories_used）
	if cbc.Extra() != nil {
		cbc.Extra()["memories_used"] = r.memoriesUsed
	}

	// 3. 自动总结（对齐 Python 221-243 行）
	// Python 注释：only support matts_mode = "none", because other matts_mode need to call multiple invoke
	if r.autoSummarize && r.currentQuery != "" && r.memoryService != nil {
		trajectory := r.extractTrajectory(cbc)
		if trajectory != "" {
			// 对齐 Python: feedback, score = _evaluate_trial(self._current_query, trajectory)
			feedback, score := ceservice.EvaluateTrial(r.currentQuery, trajectory, "")
			logger.Info(logComponent).Msg("正在为当前轨迹运行自动总结")
			// 对齐 Python: await _summarize_trajectories(self.memory_service, self.user_id, SummarizeTrajectoriesInput(...))
			_, err := ceservice.SummarizeTrajectories(ctx, r.memoryService, r.userID, ceservice.SummarizeTrajectoriesInput{
				Query:      r.currentQuery,
				Trajectory: []string{trajectory},
				MattsMode:  "none",
				Feedback:   []string{feedback},
				Score:      []int{score},
			})
			if err != nil {
				logger.Error(logComponent).Err(err).Msg("afterTaskIteration 自动总结失败")
			}
		}
	}

	return nil
}

// extractTrajectory 从 agent 的 context_engine 提取对话轨迹。
//
// 对齐 Python: ContextEvolutionRail.extract_trajectory(ctx) 249-267 行
//
// 流程：
//  1. 获取 DeepAgent 的 ReactAgent
//  2. 从 ReactAgent 获取 ContextEngine
//  3. 通过 ContextEngine.GetContext 获取 ModelContext
//  4. 从 ModelContext 获取消息列表
//  5. 转换为 service.Message 格式
//  6. 调用 FormatTrajectory 格式化
func (r *ContextEvolutionRail) extractTrajectory(cbc *agentinterfaces.AgentCallbackContext) string {
	defer func() {
		if err := recover(); err != nil {
			logger.Warn(logComponent).Any("panic", err).Msg("提取轨迹时发生 panic")
		}
	}()

	deepAgent, ok := cbc.Agent().(hinterfaces.DeepAgentInterface)
	if !ok || deepAgent == nil {
		return ""
	}

	reactAgent := deepAgent.ReactAgent()
	if reactAgent == nil {
		return ""
	}

	ce := reactAgent.ContextEngine()
	if ce == nil {
		return ""
	}

	sess := cbc.Session()
	if sess == nil {
		return ""
	}

	sessionID := sess.GetSessionID()
	// 对齐 Python: context = inner_agent.context_engine.get_context(session_id=session.get_session_id(), context_id="default_context_id")
	mc := ce.GetContext("default_context_id", sessionID)
	if mc == nil {
		return ""
	}

	// 对齐 Python: context.get_messages()
	messages, err := mc.GetMessages(0, true)
	if err != nil {
		logger.Warn(logComponent).Err(err).Msg("获取对话消息失败")
		return ""
	}

	// 转换为 service.Message 格式（对齐 Python: format_trajectory(context.get_messages())）
	serviceMsgs := make([]ceservice.Message, 0, len(messages))
	for _, msg := range messages {
		role := roleTypeToString(msg.GetRole())
		content := msg.GetContent().Text()
		serviceMsgs = append(serviceMsgs, ceservice.Message{
			Role:    role,
			Content: content,
		})
	}

	return ceservice.FormatTrajectory(serviceMsgs)
}

// deepCopyPromptTemplate 深拷贝提示词模板。
// 对齐 Python: [dict(msg) for msg in inner_agent.config.prompt_template]
func deepCopyPromptTemplate(tpl []map[string]any) []map[string]any {
	if tpl == nil {
		return nil
	}
	result := make([]map[string]any, len(tpl))
	for i, msg := range tpl {
		newMsg := make(map[string]any, len(msg))
		for k, v := range msg {
			newMsg[k] = v
		}
		result[i] = newMsg
	}
	return result
}

// roleTypeToString 将 RoleType 转换为字符串。
// 对齐 Python: message.role 属性返回 "system"/"user"/"assistant"/"tool"。
func roleTypeToString(rt llmschema.RoleType) string {
	switch rt {
	case llmschema.RoleTypeSystem:
		return "system"
	case llmschema.RoleTypeUser:
		return "user"
	case llmschema.RoleTypeAssistant:
		return "assistant"
	case llmschema.RoleTypeTool:
		return "tool"
	default:
		return fmt.Sprintf("unknown(%d)", rt)
	}
}

// compile-time 接口断言
var _ taskMemoryServicer = (*ceservice.TaskMemoryService)(nil)
