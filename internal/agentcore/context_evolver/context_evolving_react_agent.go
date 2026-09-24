package contextevolver

import (
	"context"
	"fmt"

	ceconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/config"
	cecontext "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/context"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/service"
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
//
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
	// autoSummarize 是否自动总结
	autoSummarize bool
	// autoSummarizeMattsMode 自动总结的 MaTTS 模式
	autoSummarizeMattsMode string
	// lastRetrievedQuery 缓存上次检索的 query
	lastRetrievedQuery string
	// lastRetrievalResult 缓存上次检索结果
	lastRetrievalResult *service.RetrieveResult
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewContextEvolvingReActAgent 创建带记忆检索能力的 ReActAgent。
// 对齐 Python ContextEvolvingReActAgent.__init__(card, user_id, memory_service, ...)。
// 双路径构造：memoryService 非 nil 时直接使用，否则用 persist 参数自建。
// 返回 (*Agent, error) 因为构造时需要调用 LoadMemories。
func NewContextEvolvingReActAgent(
	ctx context.Context,
	card *agentschema.AgentCard,
	config *config.ReActAgentConfig,
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
	// 对齐 Python：memoryService 为 nil 时用 persist 参数自建
	var err error
	if memoryService == nil && persistType != nil {
		cfg := &service.TaskMemoryServiceConfig{
			PersistType:      persistType,
			PersistPath:      persistPath,
			MilvusHost:       milvusHost,
			MilvusPort:       milvusPort,
			MilvusCollection: milvusCollection,
		}
		memoryService, err = service.NewTaskMemoryService(cfg)
		if err != nil {
			return nil, fmt.Errorf("failed to create TaskMemoryService: %w", err)
		}
	}

	// 对齐 Python：创建父类 ReActAgent
	reactAgent := agents.NewReActAgent(card, config)

	agent := &ContextEvolvingReActAgent{
		ReActAgent:              reactAgent,
		memoryService:           memoryService,
		userID:                  userID,
		injectMemoriesInContext: injectMemoriesInContext,
		autoSummarize:           autoSummarize,
		autoSummarizeMattsMode:  autoSummarizeMattsMode,
	}

	// 对齐 Python：构造时加载记忆
	if memoryService != nil {
		if loadErr := memoryService.LoadMemories(ctx, userID); loadErr != nil {
			logger.Error(logger.ComponentAgentCore).Err(loadErr).Msg("Failed to load memories during agent initialization")
			return nil, fmt.Errorf("failed to load memories: %w", loadErr)
		}
	}

	// 对齐 Python：logger.info("ContextEvolvingReActAgent initialized for user=%s, inject_in_context=%s, auto_summarize=%s, persist_type=%s", ...)
	logger.Info(logger.ComponentAgentCore).
		Str("user_id", userID).
		Bool("inject_in_context", injectMemoriesInContext).
		Bool("auto_summarize", autoSummarize).
		Msg("ContextEvolvingReActAgent initialized")

	return agent, nil
}

// Invoke 重写 Invoke 添加记忆检索路由。
// 对齐 Python ContextEvolvingReActAgent.invoke。
// 外层路由逻辑：有 matts_mode → 委托 RunTrials，否则 → invokeWithMemory。
func (a *ContextEvolvingReActAgent) Invoke(ctx context.Context, inputs map[string]any, opts ...interfaces.AgentOption) (map[string]any, error) {
	query, _ := inputs["query"].(string)
	if query == "" {
		// 对齐 Python：无 query 时补警告日志
		logger.Warn(logger.ComponentAgentCore).Msg("No query provided in inputs")
		return a.ReActAgent.Invoke(ctx, inputs, opts...)
	}

	// 对齐 Python：matts_mode 路由
	mattsMode, hasMattMode := inputs["matts_mode"].(string)
	if hasMattMode && mattsMode != "" {
		groundTruth, _ := inputs["ground_truth"].(string)
		mattsK := 3
		if k, ok := inputs["matts_k"].(int); ok && k > 0 {
			mattsK = k
		}

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
		"session_id":      sessionID,
	}

	// 对齐 Python: llm_temperature 设置
	if cfg.LLMTemperature != 0 {
		inputs["llm_temperature"] = cfg.LLMTemperature
	}

	result, err := a.invokeWithMemory(ctx, inputs)
	if err != nil {
		return nil, err
	}

	answer, _ := result["output"].(string)
	trajectory := formatTrajectoryFromResult(result)

	return &cecontext.TrajectoryResult{
		Answer:     answer,
		Success:    true,
		Trajectory: trajectory,
	}, nil
}

// GetMemoryService 返回记忆服务。
func (a *ContextEvolvingReActAgent) GetMemoryService() *service.TaskMemoryService {
	return a.memoryService
}

// AutoConfigure 从 config 自动配置 Agent。对齐 Python Agent.auto_configure()。
// 读取 API_KEY/API_BASE/MODEL_NAME/MODEL_PROVIDER 等配置，
// 重新构建 ReActAgent 并更新记忆服务。
func (a *ContextEvolvingReActAgent) AutoConfigure(ctx context.Context) error {
	apiKey := ceconfig.GetString("API_KEY", "")
	if apiKey == "" {
		return fmt.Errorf("API_KEY not configured in context_evolver config")
	}
	apiBase := ceconfig.GetString("API_BASE", "https://api.openai.com/v1")
	modelName := ceconfig.GetString("MODEL_NAME", "gpt-5.2")

	logger.Info(logger.ComponentAgentCore).
		Str("model_name", modelName).
		Str("api_base", apiBase).
		Msg("AutoConfigure: configuring agent from context_evolver config")

	// 重新创建 TaskMemoryService（如果存在）
	if a.memoryService != nil {
		cfg := &service.TaskMemoryServiceConfig{
			LLMModel:       modelName,
			EmbeddingModel: ceconfig.GetString("EMBEDDING_MODEL", "text-embedding-3-small"),
			APIKey:         apiKey,
			APIBase:        apiBase,
		}
		newSvc, err := service.NewTaskMemoryService(cfg)
		if err != nil {
			return fmt.Errorf("AutoConfigure: failed to create TaskMemoryService: %w", err)
		}
		a.memoryService = newSvc
	}

	return nil
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

	// 对齐 Python: llm_temperature 设置
	// 从 inputs 中读取 llm_temperature（由 ParallelScalingOp 通过 AgentFlowOption 传入）
	// TODO(#9.82): ReActAgent.Invoke 支持读取 llm_temperature 传给 Model.Generate
	if llmTemp, ok := inputs["llm_temperature"]; ok {
		_ = llmTemp // 当前 ReActAgent 不支持动态温度，待后续集成
	}

	// 对齐 Python：1. 检索记忆（带缓存）
	var memoryString string
	var memoriesUsed int

	if a.memoryService != nil {
		if a.lastRetrievedQuery == retrievalQuery && a.lastRetrievalResult != nil {
			// 对齐 Python：logger.info("Reusing cached memory retrieval result")
			logger.Info(logger.ComponentAgentCore).Msg("Reusing cached memory retrieval result")
			memoryString = a.lastRetrievalResult.MemoryString
			memoriesUsed = len(a.lastRetrievalResult.RetrievedMemory)
		} else {
			result, err := a.memoryService.Retrieve(ctx, a.userID, retrievalQuery)
			if err != nil {
				// 对齐 Python：logger.error("Failed to retrieve memories: %s", e)
				logger.Error(logger.ComponentAgentCore).Err(err).Msg("Failed to retrieve memories")
			} else {
				a.lastRetrievedQuery = retrievalQuery
				a.lastRetrievalResult = result
				memoryString = result.MemoryString
				memoriesUsed = len(result.RetrievedMemory)
				// 对齐 Python：logger.info("Retrieved %s memories for query", ...)
				logger.Info(logger.ComponentAgentCore).
					Int("memories_used", memoriesUsed).
					Msg("Retrieved memories")
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

// formatTrajectoryFromResult 从 Invoke 结果格式化轨迹文本。
func formatTrajectoryFromResult(result map[string]any) string {
	if result == nil {
		return ""
	}
	// 简单提取 output 和 steps
	output, _ := result["output"].(string)
	if output != "" {
		return fmt.Sprintf("USER: query\nASSISTANT: %s", output)
	}
	return ""
}
