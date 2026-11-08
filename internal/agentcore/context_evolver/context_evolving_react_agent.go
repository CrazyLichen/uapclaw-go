package contextevolver

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	ceconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/config"
	cecontext "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/context"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/service"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/agents"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// MemoryAgentConfigInput 记忆 Agent 配置输入。对齐 Python MemoryAgentConfigInput。
type MemoryAgentConfigInput struct {
	// ModelProvider 模型提供商
	ModelProvider string
	// APIKey API 密钥
	APIKey string
	// APIBase API 基地址
	APIBase string
	// ModelName 模型名称
	ModelName string
	// SystemPrompt 系统提示词
	SystemPrompt *string
	// MaxIterations 最大迭代次数
	MaxIterations int
}

// ContextEvolvingReActAgent 带记忆检索能力的 ReActAgent。
// 嵌入 *ReActAgent 获得所有父类方法，重写 Invoke 添加记忆检索。
// 对齐 Python ContextEvolvingReActAgent(ReActAgent)。
// 两层 Invoke 设计：
//   - 外层 Invoke()：路由（matts_mode → RunTrials，否则 → invokeWithMemory）
//   - 内层 invokeWithMemory()：检索记忆 → 增强输入 → 调父类 ReActAgent.Invoke()
//   - Execute()：实现 AgentFlowService 接口，走内层避免递归
//
// Python: openjiuwen/extensions/context_evolver/context_evolving_react_agent.py
type ContextEvolvingReActAgent struct {
	*agents.ReActAgent // 嵌入指针，对齐 Python 继承

	// memoryService 记忆服务
	memoryService *service.TaskMemoryService
	// userID 用户标识
	userID string
	// injectMemoriesInContext 是否将记忆注入上下文
	injectMemoriesInContext bool
	// autoSummarize 是否自动总结。
	// 说明（对齐 Python）：该字段在 Python 与本实现中均只写不读，
	// 真正的自动总结由 ContextEvolutionRail 承担。
	autoSummarize bool
	// autoSummarizeMattsMode 自动总结的 MaTTS 模式。
	// 说明：同 autoSummarize，仅保留参数兼容。
	autoSummarizeMattsMode string
	// lastRetrievedQuery 缓存上次检索的 query
	lastRetrievedQuery string
	// lastRetrievalResult 缓存上次检索结果
	lastRetrievalResult *service.RetrieveResult
}

// ──────────────────────────── 常量 ────────────────────────────

// defaultContextEvolvingSystemPrompt 自动配置时使用的默认系统提示词。
// Python: context_evolving_react_agent.py _auto_configure 中的 default_system_prompt
const defaultContextEvolvingSystemPrompt = "You are a helpful assistant with access to a memory system. " +
	"When relevant memories are provided in your context, use them to inform your responses. " +
	"Always provide accurate, helpful answers based on both your knowledge and any retrieved memories."

// ──────────────────────────── 导出函数 ────────────────────────────

// NewContextEvolvingReActAgent 创建带记忆检索能力的 ReActAgent。
// 对齐 Python ContextEvolvingReActAgent.__init__(card, user_id, memory_service, ...)：
// 构造签名不含 config，父类使用默认 ReActAgentConfig（等价 Python super().__init__(card)），
// 构造末尾调用 AutoConfigure 从环境配置模型客户端。
// 返回 (*Agent, error) 因为构造期需要创建 memoryService 与 AutoConfigure。
func NewContextEvolvingReActAgent(
	ctx context.Context,
	card *agentschema.AgentCard,
	userID string,
	memoryService *service.TaskMemoryService,
	injectMemoriesInContext bool,
	autoSummarize bool,
	autoSummarizeMattsMode string,
	persistType *string,
	persistPath string,
	milvusHost string,
	milvusPort int,
	milvusCollection string,
) (*ContextEvolvingReActAgent, error) {
	// 对齐 Python：memory_service 为 None 时无条件自建（persist_type 为 None 表示纯内存）
	var err error
	if memoryService == nil {
		opts := []service.TaskMemoryServiceOption{}
		if persistType != nil {
			opts = append(opts,
				service.WithPersistType(*persistType),
				service.WithPersistPath(persistPath),
				service.WithMilvusHost(milvusHost),
				service.WithMilvusPort(milvusPort),
				service.WithMilvusCollection(milvusCollection),
			)
		}
		memoryService, err = service.NewTaskMemoryService(opts...)
		if err != nil {
			return nil, fmt.Errorf("创建 TaskMemoryService 失败: %w", err)
		}
	}

	// 对齐 Python: super().__init__(card) —— 父类使用默认配置
	reactAgent := agents.NewReActAgent(card, config.NewReActAgentConfig())

	agent := &ContextEvolvingReActAgent{
		ReActAgent:              reactAgent,
		memoryService:           memoryService,
		userID:                  userID,
		injectMemoriesInContext: injectMemoriesInContext,
		autoSummarize:           autoSummarize,
		autoSummarizeMattsMode:  autoSummarizeMattsMode,
	}

	// 对齐 Python：构造时加载记忆（load_memories 内部吞掉异常，不影响构造）
	if memoryService != nil {
		_ = memoryService.LoadMemories(ctx, userID)
	}

	// 对齐 Python：logger.info("ContextEvolvingReActAgent initialized for user=%s, inject_in_context=%s, auto_summarize=%s, persist_type=%s", ...)
	logger.Info(logger.ComponentAgentCore).
		Str("user_id", userID).
		Bool("inject_in_context", injectMemoriesInContext).
		Bool("auto_summarize", autoSummarize).
		Msg("ContextEvolvingReActAgent initialized")

	// 对齐 Python: self._auto_configure()
	if autoCfgErr := agent.AutoConfigure(ctx); autoCfgErr != nil {
		return nil, autoCfgErr
	}

	return agent, nil
}

// Invoke 重写 Invoke 添加记忆检索路由。
// 对齐 Python ContextEvolvingReActAgent.invoke。
// 外层路由逻辑：有 matts_mode → 委托 RunTrials，否则 → invokeWithMemory。
func (a *ContextEvolvingReActAgent) Invoke(ctx context.Context, inputs map[string]any, opts ...interfaces.AgentOption) (map[string]any, error) {
	query, _ := inputs["query"].(string)
	if query == "" {
		// 对齐 Python：无 query 时补警告日志
		logger.Warn(logger.ComponentAgentCore).Msg("query not provided in input")
		return a.ReActAgent.Invoke(ctx, inputs, opts...)
	}

	// 对齐 Python：matts_mode 路由
	mattsMode, hasMattMode := inputs["matts_mode"].(string)
	if hasMattMode && mattsMode != "" {
		groundTruth, _ := inputs["ground_truth"].(string)
		// 对齐 Python: matts_k = inputs.get("matts_k", None)，缺省由 RunTrials 统一解析
		mattsK := parseMattsK(inputs)

		// 委托 MaTTS RunTrials（self 实现 AgentFlowService）
		results, err := service.RunTrials(ctx, a, service.RunTrialsInput{
			Agent:       a, // self，实现 AgentFlowService
			UserID:      a.userID,
			Question:    query,
			GroundTruth: groundTruth,
			MattsK:      mattsK,
			MattsMode:   mattsMode,
		})
		if err != nil {
			return nil, err
		}

		// 转换为 MaTTSResult 返回
		return map[string]any{
			"trials":     results,
			"query":      query,
			"user_id":    a.userID,
			"matts_mode": mattsMode,
		}, nil
	}

	// 普通模式：检索记忆 + 调父类
	return a.invokeWithMemory(ctx, inputs, opts...)
}

// Execute 实现 AgentFlowService 接口。
// runTrialsInner 通过此路径调用，走内层 invokeWithMemory 避免递归。
// 对齐 Python _run_trials_inner 中 getattr(agent, "_invoke_with_memory", agent.invoke)。
func (a *ContextEvolvingReActAgent) Execute(ctx context.Context, query string, sessionID string, opts ...cecontext.AgentFlowOption) (*cecontext.TrajectoryResult, error) {
	// 解析 AgentFlow 选项
	cfg := &cecontext.AgentFlowConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	// 对齐 Python: retrieval_query 优先使用显式传入的值，否则默认用 query
	rq := query
	if cfg.RetrievalQuery != "" {
		rq = cfg.RetrievalQuery
	}

	inputs := map[string]any{
		"query":           query,
		"retrieval_query": rq,
	}

	// 对齐 Python run_trials: session = Session(card=agent.card)，显式传入内层 invoke
	sess := session.NewSession(session.WithSessionID(sessionID))
	if err := sess.PreRun(ctx, inputs); err != nil {
		return nil, fmt.Errorf("创建试验会话失败: %w", err)
	}
	// 外部传入 session 时 invokeImpl 不做收尾，由本处负责
	defer func() {
		_ = sess.CloseStream()
		_ = sess.Commit(ctx)
	}()

	result, err := a.invokeWithMemory(ctx, inputs, interfaces.WithSession(sess))
	if err != nil {
		return nil, err
	}

	answer, _ := result["output"].(string)
	success := trajectorySuccess(result)
	if !success {
		logger.Warn(logger.ComponentAgentCore).
			Str("output", answer).
			Msg("[ContextEvolvingReActAgent] inner result marked as error")
	}

	return &cecontext.TrajectoryResult{
		Answer:     answer,
		Success:    success,
		Trajectory: a.extractTrajectory(sessionID, query, answer),
	}, nil
}

// parseMattsK 从 inputs 解析 matts_k，缺省返回 0 交由 RunTrials 读取配置。
// 对齐 Python: matts_k = inputs.get("matts_k", None)。
func parseMattsK(inputs map[string]any) int {
	switch v := inputs["matts_k"].(type) {
	case int:
		return v
	case float64:
		return int(v)
	case json.Number:
		if n, err := v.Int64(); err == nil {
			return int(n)
		}
	}
	return 0
}

// extractTrajectory 从会话上下文提取并格式化轨迹。
// 对齐 Python run_trials:
// context = agent.context_engine.get_context(session_id=..., context_id="default_context_id")
// trajectory = format_trajectory(context.get_messages())
func (a *ContextEvolvingReActAgent) extractTrajectory(sessionID, query, answer string) string {
	fallback := fmt.Sprintf("USER: %s\nASSISTANT: %s", query, answer)
	engine := a.ReActAgent.ContextEngine()
	if engine == nil {
		return fallback
	}
	mc := engine.GetContext("default_context_id", sessionID)
	if mc == nil {
		return fallback
	}
	msgs, err := mc.GetMessages(0, true)
	if err != nil || len(msgs) == 0 {
		return fallback
	}
	if out := service.FormatTrajectory(messagesToServiceMessages(msgs)); out != "" {
		return out
	}
	return fallback
}

// messagesToServiceMessages 将上下文消息转换为轨迹格式化用的消息。
func messagesToServiceMessages(msgs []llmschema.BaseMessage) []service.Message {
	converted := make([]service.Message, 0, len(msgs))
	for _, m := range msgs {
		msg := service.Message{Role: trajectoryRole(m.GetRole()), Content: m.GetContent().Text()}
		if am, ok := m.(*llmschema.AssistantMessage); ok {
			for _, tc := range am.ToolCalls {
				if tc == nil {
					continue
				}
				msg.ToolCalls = append(msg.ToolCalls, service.ToolCall{Name: tc.Name, Args: tc.Arguments})
			}
		}
		converted = append(converted, msg)
	}
	return converted
}

// trajectoryRole 将 RoleType 映射为轨迹角色名。
func trajectoryRole(rt llmschema.RoleType) string {
	switch rt {
	case llmschema.RoleTypeUser:
		return "user"
	case llmschema.RoleTypeAssistant:
		return "assistant"
	case llmschema.RoleTypeTool:
		return "tool"
	default:
		return "system"
	}
}

// GetMemoryService 返回记忆服务。
func (a *ContextEvolvingReActAgent) GetMemoryService() *service.TaskMemoryService {
	return a.memoryService
}

// AutoConfigure 从配置自动配置 Agent 的模型客户端。
// 对齐 Python ContextEvolvingReActAgent._auto_configure()：
// 新建 ReActAgentConfig → configure_model_client → configure_prompt_template →
// configure_max_iterations(5) → self.configure(config)。
// 注意：只配置 Agent，不改动 memoryService（Python 同样不碰）。
func (a *ContextEvolvingReActAgent) AutoConfigure(ctx context.Context) error {
	apiKey := ceconfig.GetString("API_KEY", "")
	if apiKey == "" {
		// 对齐 Python: if not api_key: return（静默跳过，不返回错误）
		return nil
	}
	provider := ceconfig.GetString("MODEL_PROVIDER", "OpenAI")
	apiBase := ceconfig.GetString("API_BASE", "https://api.openai.com/v1")
	modelName := ceconfig.GetString("MODEL_NAME", "gpt-4")

	cfg := config.NewReActAgentConfig(
		config.WithModelProvider(strings.ToLower(provider)),
		config.WithAPIKey(apiKey),
		config.WithAPIBase(apiBase),
		config.WithModelName(modelName),
		config.WithMaxIterations(5),
		config.WithPromptTemplate([]map[string]any{
			{"role": "system", "content": defaultContextEvolvingSystemPrompt},
		}),
	)
	// 对齐 Python: self.configure(config) 为整体替换
	if err := a.ReActAgent.Configure(ctx, cfg); err != nil {
		return fmt.Errorf("AutoConfigure: 配置 Agent 失败: %w", err)
	}
	// Go 的 config.PromptTemplate 无消费方；Configure 会重建 promptBuilder，
	// 因此必须在 Configure 之后把默认系统提示词落到 identity 节。
	a.ReActAgent.AddPromptBuilderSection(
		agents.IdentitySectionName,
		defaultContextEvolvingSystemPrompt,
		agents.IdentitySectionPriority,
	)

	logger.Info(logger.ComponentAgentCore).
		Str("model_name", modelName).
		Str("api_base", apiBase).
		Msg("AutoConfigure: agent configured from context_evolver config")
	return nil
}

// AddTool 添加工具到当前 Agent。
// 对齐 Python: ContextEvolvingReActAgent.add_tool(tool)
func (a *ContextEvolvingReActAgent) AddTool(t tool.Tool) {
	am := a.AbilityManager()
	if am != nil {
		am.Add(t.Card())
	}
	resourceMgr := runner.GetResourceMgr()
	if resourceMgr != nil {
		_ = resourceMgr.AddTool(t)
	}
}

// AddTools 批量添加工具到当前 Agent。
// 对齐 Python: ContextEvolvingReActAgent.add_tools(tools)
func (a *ContextEvolvingReActAgent) AddTools(tools []tool.Tool) {
	for _, t := range tools {
		a.AddTool(t)
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// invokeWithMemory 内层：检索记忆 → 增强输入 → 调父类 ReActAgent.Invoke。
// 对齐 Python _invoke_with_memory。
func (a *ContextEvolvingReActAgent) invokeWithMemory(ctx context.Context, inputs map[string]any, opts ...interfaces.AgentOption) (map[string]any, error) {
	query, _ := inputs["query"].(string)
	retrievalQuery := query
	if rq, ok := inputs["retrieval_query"].(string); ok && rq != "" {
		retrievalQuery = rq
	}

	// 说明：Python 的 RB MaTTS 通过直接改写 llm.temperature 实现多样性
	// （retrieve/task/reasoning_bank/matts.py:52-90）；Go 的 agent-flow 路径无等价通道，
	// 故不提供 llm_temperature 入参（原实现读取后丢弃，属静默失效）。

	// 对齐 Python：1. 检索记忆（带缓存）
	var memoryString string
	var memoriesUsed int

	if a.memoryService != nil {
		if a.lastRetrievedQuery == retrievalQuery && a.lastRetrievalResult != nil {
			// 对齐 Python：logger.info("Reusing cached memory retrieval result")
			logger.Info(logger.ComponentAgentCore).Msg("reusing cached memory retrieval results")
			memoryString = a.lastRetrievalResult.MemoryString
			memoriesUsed = len(a.lastRetrievalResult.RetrievedMemory)
		} else {
			result, err := a.memoryService.Retrieve(ctx, a.userID, retrievalQuery)
			if err != nil {
				// 对齐 Python：logger.error("Failed to retrieve memories: %s", e)
				logger.Error(logger.ComponentAgentCore).Err(err).Msg("memory retrieval failed")
			} else {
				a.lastRetrievedQuery = retrievalQuery
				a.lastRetrievalResult = result
				memoryString = result.MemoryString
				memoriesUsed = len(result.RetrievedMemory)
				// 对齐 Python：logger.info("Retrieved %s memories for query", ...)
				logger.Info(logger.ComponentAgentCore).
					Int("memories_used", memoriesUsed).
					Msg("memory retrieval completed")
			}
		}
	}

	// 对齐 Python：2. 增强输入
	augmentedInputs := copyMap(inputs)
	if memoriesUsed > 0 && memoryString != "" {
		if a.injectMemoriesInContext {
			// 对齐 Python：memory_context = f"Some Related Experience to help you complete the task:\n{memory_string}\n"
			memoryContext := fmt.Sprintf("Some Related Experience to help you complete the task:\n%s\n", memoryString)
			augmentedInputs["query"] = fmt.Sprintf("%s\n\n%s", memoryContext, query)
		} else {
			augmentedInputs["memory_context"] = memoryString
			augmentedInputs["memories_used"] = memoriesUsed
		}
	}

	// 对齐 Python：3. 调父类原始 ReAct 循环（super().invoke()）
	result, err := a.ReActAgent.Invoke(ctx, augmentedInputs, opts...)
	if result != nil {
		result["memories_used"] = memoriesUsed
	}
	return result, err
}

// copyMap 深拷贝 map[string]any。
func copyMap(m map[string]any) map[string]any {
	result := make(map[string]any, len(m))
	for k, v := range m {
		result[k] = v
	}
	return result
}

// trajectorySuccess 判断内层 invoke 结果是否应记为成功轨迹。
// ReAct 循环在「空回答 / 迭代耗尽 / provider 错误」时会返回 result_type=error
// 且 output 携带说明文案；若一律置 Success=true，失败轨迹会被当作成功样本
// 喂进演进数据。
func trajectorySuccess(result map[string]any) bool {
	if result == nil {
		return false
	}
	resultType, ok := result["result_type"].(string)
	if !ok {
		return true
	}
	return !strings.EqualFold(resultType, "error")
}
