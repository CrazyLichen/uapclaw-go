package service

import (
	"context"
	"crypto/md5"
	"fmt"
	"strings"

	cecontext "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/context"
	op "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/op"
	cepersistence "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/persistence"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/vector_store"
	acesummary "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/summary/task/ace"
	rbsummary "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/summary/task/rb"
	remesummary "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/summary/task/reme"
	aceretrieve "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/retrieve/task/ace"
	rbretrieve "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/retrieve/task/rb"
	emeretrieve "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/retrieve/task/reme"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AddMemoryRequest 手动添加记忆请求。对齐 Python AddMemoryRequest。
type AddMemoryRequest struct {
	// Content 记忆内容（必填）
	Content string
	// Query 用于 embedding 的查询（仅 ReasoningBank）
	Query *string
	// WhenToUse 何时使用此记忆（仅 ReMe/RefCon/DivCon）
	WhenToUse *string
	// Title 记忆标题（仅 ReasoningBank）
	Title *string
	// Description 记忆描述（仅 ReasoningBank）
	Description *string
	// Section 记忆分类（仅 ACE），默认 "general"
	Section string
	// Label 记忆标签（仅 ReasoningBank）
	Label *string
}

// TaskMemoryServiceConfig 任务记忆服务配置。
// 集中管理管线组装所需的全部参数，对齐 Python config.yaml 中的相关键。
type TaskMemoryServiceConfig struct {
	// LLMModel LLM 模型名称，默认 "gpt-5.2"
	LLMModel string
	// EmbeddingModel Embedding 模型名称，默认 "text-embedding-3-small"
	EmbeddingModel string
	// APIKey API 密钥
	APIKey string
	// APIBase API 基地址
	APIBase string
	// RetrievalAlgo 检索算法，默认 "ACE"
	RetrievalAlgo string
	// SummaryAlgo 总结算法，默认 "ACE"
	SummaryAlgo string

	// --- 持久化 ---
	// PersistType 持久化类型，nil=关闭
	PersistType *string
	// PersistPath JSON 持久化路径模板
	PersistPath string
	// MilvusHost Milvus 主机
	MilvusHost string
	// MilvusPort Milvus 端口
	MilvusPort int
	// MilvusCollection Milvus 集合名
	MilvusCollection string

	// --- ReMe 检索参数 ---
	// TopKRetrieval ReMe 检索 top-k，默认 10
	TopKRetrieval int
	// TopKRerank ReMe 重排 top-k，默认 5
	TopKRerank int
	// LLMRerank 是否使用 LLM 重排，默认 true
	LLMRerank bool
	// LLMRewrite 是否使用 LLM 改写，默认 true
	LLMRewrite bool

	// --- RB 检索参数 ---
	// TopKQuery RB 检索 top-k，默认 1
	TopKQuery int

	// --- ACE 总结参数 ---
	// UseGroundTruth 是否使用参考答案，默认 false
	UseGroundTruth bool
	// MaxPlaybookSize Playbook 最大条目数，默认 50
	MaxPlaybookSize int

	// --- ReMe 总结参数 ---
	// ExtractBestTraj 是否提取最佳轨迹，默认 true
	ExtractBestTraj bool
	// ExtractWorstTraj 是否提取最差轨迹，默认 true
	ExtractWorstTraj bool
	// ExtractComparativeTraj 是否提取对比轨迹，默认 true
	ExtractComparativeTraj bool
	// MemoryValidation 是否验证记忆，默认 true
	MemoryValidation bool
	// MemoryDeduplication 是否去重，默认 true
	MemoryDeduplication bool
}

// TaskMemoryService 任务记忆服务，提供记忆检索/总结/管理的统一入口。
// 对齐 Python TaskMemoryService。
//
// Python: openjiuwen/extensions/context_evolver/service/task_memory_service.py
type TaskMemoryService struct {
	// serviceContext 共享服务上下文
	serviceContext *cecontext.ServiceContext
	// llm LLM 服务
	llm cecontext.LLMService
	// embedding Embedding 服务
	embedding cecontext.EmbeddingService
	// vectorStore 向量存储
	vectorStore cecontext.VectorStoreService
	// retrievalAlgorithm 检索算法名称
	retrievalAlgorithm string
	// summaryAlgorithm 总结算法名称
	summaryAlgorithm string
	// retrieveFlow 检索管线
	retrieveFlow op.BaseOp
	// summaryFlow 总结管线
	summaryFlow op.BaseOp
	// persistType 持久化类型（nil=关闭）
	persistType *string
	// persistPath JSON 持久化路径模板
	persistPath string
	// milvusHost Milvus 主机
	milvusHost string
	// milvusPort Milvus 端口
	milvusPort int
	// milvusCollection Milvus 集合名
	milvusCollection string
	// persistenceHelper 持久化助手
	persistenceHelper *cepersistence.MemoryPersistenceHelper
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// algoNameMap 算法名规范化映射。对齐 Python normalize_algo_name。
var algoNameMap = map[string]string{
	"RB":            "ReasoningBank",
	"REASONINGBANK": "ReasoningBank",
	"REME":          "ReMe",
	"ACE":           "ACE",
	"REFCON":        "RefCon",
	"DIVCON":        "DivCon",
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTaskMemoryService 创建任务记忆服务。
// 对齐 Python TaskMemoryService.__init__(...)。
// 从 TaskMemoryServiceConfig 读取参数，内部创建 OpenAILLMWrapper + OpenAIEmbeddingWrapper，
// 注册到 ServiceContext。
func NewTaskMemoryService(cfg *TaskMemoryServiceConfig) (*TaskMemoryService, error) {
	// 应用默认值
	cfg = applyConfigDefaults(cfg)

	// 对齐 Python：logger.info("Initializing TaskMemoryService...")
	logger.Info(logComponent).Msg("Initializing TaskMemoryService")

	// 对齐 Python：创建 ServiceContext
	sc := cecontext.NewServiceContext()

	// 对齐 Python：创建 OpenAILLMWrapper
	llm, err := NewOpenAILLMWrapper(cfg.LLMModel, cfg.APIKey, cfg.APIBase, 0.7, 2000)
	if err != nil {
		return nil, exception.NewBaseError(
			exception.StatusToolchainEvolvingMemoryServiceInitFailed,
			exception.WithMsg(fmt.Sprintf("Failed to create LLM wrapper: %s", err.Error())),
		)
	}

	// 对齐 Python：创建 OpenAIEmbeddingWrapper
	emb, err := NewOpenAIEmbeddingWrapper(cfg.EmbeddingModel, cfg.APIKey, cfg.APIBase)
	if err != nil {
		return nil, exception.NewBaseError(
			exception.StatusToolchainEvolvingMemoryServiceInitFailed,
			exception.WithMsg(fmt.Sprintf("Failed to create Embedding wrapper: %s", err.Error())),
		)
	}

	// 对齐 Python：创建 MemoryVectorStore
	vs := vector_store.NewMemoryVectorStore()

	// 对齐 Python：注册服务
	sc.RegisterService("llm", llm)
	sc.RegisterService("embedding_model", emb)
	sc.RegisterService("vector_store", vs)

	return newTaskMemoryServiceWithServices(sc, llm, emb, vs, cfg)
}

// NewTaskMemoryServiceWithServices 使用已构造好的 LLM/Embedding 服务创建任务记忆服务。
// 方便测试时注入 mock，也方便复用已有的服务实例。
func NewTaskMemoryServiceWithServices(
	llm cecontext.LLMService,
	emb cecontext.EmbeddingService,
	vectorStore cecontext.VectorStoreService,
	cfg *TaskMemoryServiceConfig,
) (*TaskMemoryService, error) {
	cfg = applyConfigDefaults(cfg)

	sc := cecontext.NewServiceContext()
	sc.RegisterService("llm", llm)
	sc.RegisterService("embedding_model", emb)
	sc.RegisterService("vector_store", vectorStore)

	return newTaskMemoryServiceWithServices(sc, llm, emb, vectorStore, cfg)
}

// Retrieve 检索记忆。对齐 Python TaskMemoryService.retrieve(user_id, query, **kwargs)。
func (s *TaskMemoryService) Retrieve(ctx context.Context, userID string, query string) (map[string]any, error) {
	// 对齐 Python：logger.info("Retrieving task memory for user=%s, query='%s...'", ...)
	logger.Info(logComponent).
		Str("user_id", userID).
		Str("query", truncate(query, 50)).
		Msg("Retrieving task memory")

	// 对齐 Python：创建 RuntimeContext
	rc := cecontext.NewRuntimeContext()
	rc.Set("user_id", userID)
	rc.Set("query", query)

	// 对齐 Python：执行 retrieveFlow
	if err := s.retrieveFlow.Execute(ctx, rc); err != nil {
		return nil, exception.NewBaseError(
			exception.StatusToolchainEvolvingMemoryRetrieveExecutionError,
			exception.WithMsg(err.Error()),
		)
	}

	// 对齐 Python：按算法格式化结果
	retrievedMemories := rc.Get("retrieved_memories")
	var memoryString string

	switch s.retrievalAlgorithm {
	case "ReasoningBank":
		memoryString = formatRBMemoryString(retrievedMemories)
	case "ACE":
		memoryString = formatACEMemoryString(retrievedMemories)
	default:
		// ReMe/RefCon/DivCon：直接从 RuntimeContext 取 memory_string
		if ms, ok := rc.Get("memory_string").(string); ok {
			memoryString = ms
		}
	}

	// 构建返回结果
	var memoriesList []any
	if ml, ok := retrievedMemories.([]any); ok {
		memoriesList = ml
	}

	result := map[string]any{
		"memory_string":    memoryString,
		"retrieved_memory": memoriesList,
		"query":            query,
		"user_id":          userID,
		"algorithm":        s.retrievalAlgorithm,
	}

	// 对齐 Python：logger.info("Retrieved memories:\n%s\nUsing %s memories", ...)
	logger.Info(logComponent).
		Int("memories_count", len(memoriesList)).
		Str("algorithm", s.retrievalAlgorithm).
		Msg("Retrieved memories")

	return result, nil
}

// Summarize 总结记忆。对齐 Python TaskMemoryService.summarize(user_id, matts, query, trajectories, **kwargs)。
func (s *TaskMemoryService) Summarize(ctx context.Context, userID string, matts string, query string, trajectories []string, extraKwargs ...map[string]any) (map[string]any, error) {
	// 对齐 Python：logger.info("Summarizing %s trajectories for user=%s", ...)
	logger.Info(logComponent).
		Int("trajectory_count", len(trajectories)).
		Str("user_id", userID).
		Msg("Summarizing trajectories")

	// 对齐 Python：创建 RuntimeContext
	rc := cecontext.NewRuntimeContext()
	rc.Set("user_id", userID)
	rc.Set("matts", matts)
	rc.Set("query", query)
	rc.Set("trajectories", trajectories)

	// 合并额外参数
	for _, kw := range extraKwargs {
		for k, v := range kw {
			rc.Set(k, v)
		}
	}

	// 对齐 Python：执行 summaryFlow
	if err := s.summaryFlow.Execute(ctx, rc); err != nil {
		return nil, exception.NewBaseError(
			exception.StatusToolchainEvolvingMemorySummarizeExecutionError,
			exception.WithMsg(err.Error()),
		)
	}

	// 对齐 Python：获取结果
	memories := rc.Get("memories")

	result := map[string]any{
		"memories":  memories,
		"user_id":   userID,
		"query":     query,
		"algorithm": s.summaryAlgorithm,
	}

	return result, nil
}

// AddMemory 手动添加记忆。对齐 Python TaskMemoryService.add_memory(user_id, request)。
func (s *TaskMemoryService) AddMemory(ctx context.Context, userID string, req AddMemoryRequest) (map[string]any, error) {
	// 对齐 Python：logger.info("Adding manual %s memory for user=%s", ...)
	logger.Info(logComponent).
		Str("algorithm", s.summaryAlgorithm).
		Str("user_id", userID).
		Msg("Adding manual memory")

	var node *schema.VectorNode
	var memoryID string

	switch s.summaryAlgorithm {
	case "ReasoningBank":
		node, memoryID = s.createRBMemory(ctx, userID, req)
	case "ReMe", "RefCon", "DivCon":
		node, memoryID = s.createReMeMemory(ctx, userID, req)
	case "ACE":
		node, memoryID = s.createACEMemory(ctx, userID, req)
	default:
		return nil, exception.NewBaseError(
			exception.StatusToolchainEvolvingMemoryAddExecutionError,
			exception.WithMsg(fmt.Sprintf("Unsupported algorithm for add_memory: %s", s.summaryAlgorithm)),
		)
	}

	if node == nil {
		return nil, exception.NewBaseError(
			exception.StatusToolchainEvolvingMemoryAddExecutionError,
			exception.WithMsg("Failed to create memory node"),
		)
	}

	// 对齐 Python：获取 embedding
	emb, err := s.embedding.Embed(ctx, node.Content)
	if err != nil {
		return nil, exception.NewBaseError(
			exception.StatusToolchainEvolvingMemoryEmbeddingExecutionError,
			exception.WithMsg(fmt.Sprintf("Failed to embed memory: %s", err.Error())),
		)
	}
	node.Embedding = emb

	// 对齐 Python：upsert 到 vectorStore
	if err := s.vectorStore.Upsert(ctx, node); err != nil {
		return nil, exception.NewBaseError(
			exception.StatusToolchainEvolvingMemoryVectorStoreExecutionError,
			exception.WithMsg(fmt.Sprintf("Failed to upsert memory: %s", err.Error())),
		)
	}

	// 对齐 Python：持久化
	if s.persistenceHelper != nil {
		algoName := algoToPersistName(s.summaryAlgorithm)
		if err := s.persistenceHelper.Save(userID, algoName, node.ToDict()); err != nil {
			logger.Error(logComponent).Err(err).Msg("Failed to persist memory")
		}
	}

	// 对齐 Python：logger.info("Added %s memory: %s", ...)
	logger.Info(logComponent).
		Str("algorithm", s.summaryAlgorithm).
		Str("memory_id", memoryID).
		Msg("Added memory")

	return map[string]any{
		"status":    "success",
		"memory_id": memoryID,
		"user_id":   userID,
		"algorithm": s.summaryAlgorithm,
	}, nil
}

// LoadMemories 从持久化后端加载记忆。对齐 Python TaskMemoryService.load_memories(user_id)。
func (s *TaskMemoryService) LoadMemories(ctx context.Context, userID string) error {
	if s.persistenceHelper == nil {
		return nil // no-op
	}

	algoName := algoToPersistName(s.summaryAlgorithm)
	nodesDict, err := s.persistenceHelper.Load(userID, algoName)
	if err != nil {
		logger.Error(logComponent).Err(err).Msg("Failed to load memories from persistence")
		return err
	}

	// 对齐 Python：将加载的 node 插入 vectorStore
	if nodes, ok := nodesDict["nodes"]; ok {
		if nodeList, ok := nodes.([]any); ok {
			loaded := 0
			for _, n := range nodeList {
				if nodeData, ok := n.(map[string]any); ok {
					vn, err := schema.VectorNodeFromDict(nodeData)
					if err != nil {
						logger.Warn(logComponent).
							Err(err).
							Msg("Failed to load node from persistence")
						continue
					}
					s.vectorStore.(*vector_store.MemoryVectorStore).LoadNode(vn.ID, vn)
					loaded++
				}
			}
			// 对齐 Python：logger.info("Loaded %d memories into vector store (algo=%s)", ...)
			logger.Info(logComponent).
				Int("loaded_count", loaded).
				Str("algorithm", s.summaryAlgorithm).
				Msg("Loaded memories into vector store")
		}
	}

	return nil
}

// Reconfigure 重新配置算法并重建管线。对齐 Python TaskMemoryService.reconfigure(algorithm)。
func (s *TaskMemoryService) Reconfigure(algorithm string) error {
	normalized, err := NormalizeAlgoName(algorithm)
	if err != nil {
		return err
	}

	s.retrievalAlgorithm = normalized
	s.summaryAlgorithm = normalized

	// 重建管线
	s.retrieveFlow = s.createRetrieveFlow()
	s.summaryFlow = s.createSummaryFlow()

	// 对齐 Python：logger.info("TaskMemoryService reconfigured: retrieval=%s, summary=%s", ...)
	logger.Info(logComponent).
		Str("retrieval", s.retrievalAlgorithm).
		Str("summary", s.summaryAlgorithm).
		Msg("TaskMemoryService reconfigured")

	return nil
}

// PersistType 返回持久化类型。对齐 Python @property persist_type。
func (s *TaskMemoryService) PersistType() *string { return s.persistType }

// PersistPath 返回持久化路径。对齐 Python @property persist_path。
func (s *TaskMemoryService) PersistPath() string { return s.persistPath }

// MilvusHost 返回 Milvus 主机。对齐 Python @property milvus_host。
func (s *TaskMemoryService) MilvusHost() string { return s.milvusHost }

// MilvusPort 返回 Milvus 端口。对齐 Python @property milvus_port。
func (s *TaskMemoryService) MilvusPort() int { return s.milvusPort }

// MilvusCollection 返回 Milvus 集合名。对齐 Python @property milvus_collection。
func (s *TaskMemoryService) MilvusCollection() string { return s.milvusCollection }

// PersistenceHelper 返回持久化助手。对齐 Python @property persistence_helper。
func (s *TaskMemoryService) PersistenceHelper() *cepersistence.MemoryPersistenceHelper {
	return s.persistenceHelper
}

// NormalizeAlgoName 规范化算法名称。对齐 Python normalize_algo_name。
func NormalizeAlgoName(algo string) (string, error) {
	upper := strings.ToUpper(algo)
	if normalized, ok := algoNameMap[upper]; ok {
		return normalized, nil
	}
	return "", exception.NewBaseError(
		exception.StatusToolchainEvolvingMemoryConfigInvalid,
		exception.WithMsg(fmt.Sprintf("Unknown algorithm: %s", algo)),
	)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// newTaskMemoryServiceWithServices 内部构造逻辑，被 NewTaskMemoryService 和 NewTaskMemoryServiceWithServices 共用。
func newTaskMemoryServiceWithServices(
	sc *cecontext.ServiceContext,
	llm cecontext.LLMService,
	emb cecontext.EmbeddingService,
	vs cecontext.VectorStoreService,
	cfg *TaskMemoryServiceConfig,
) (*TaskMemoryService, error) {
	// 对齐 Python：规范化算法名
	retrievalAlgo, err := NormalizeAlgoName(cfg.RetrievalAlgo)
	if err != nil {
		return nil, err
	}
	summaryAlgo, err := NormalizeAlgoName(cfg.SummaryAlgo)
	if err != nil {
		return nil, err
	}

	// 对齐 Python：Configuration 日志
	logger.Info(logComponent).
		Str("llm_model", cfg.LLMModel).
		Str("embedding_model", cfg.EmbeddingModel).
		Msg("Configuration")

	// 对齐 Python：Selected algorithms 日志
	logger.Info(logComponent).
		Str("retrieval", retrievalAlgo).
		Str("summary", summaryAlgo).
		Msg("Selected algorithms")

	svc := &TaskMemoryService{
		serviceContext:     sc,
		llm:               llm,
		embedding:         emb,
		vectorStore:       vs,
		retrievalAlgorithm: retrievalAlgo,
		summaryAlgorithm:  summaryAlgo,
		persistType:       cfg.PersistType,
		persistPath:       cfg.PersistPath,
		milvusHost:        cfg.MilvusHost,
		milvusPort:        cfg.MilvusPort,
		milvusCollection:  cfg.MilvusCollection,
	}

	// 对齐 Python：创建 MemoryPersistenceHelper
	if cfg.PersistType != nil && *cfg.PersistType != "" {
		opts := []cepersistence.PersistenceOption{}
		if cfg.PersistPath != "" {
			opts = append(opts, cepersistence.WithPersistPath(cfg.PersistPath))
		}
		svc.persistenceHelper = cepersistence.NewMemoryPersistenceHelper(opts...)

		// 对齐 Python：logger.info("Memory persistence enabled: type=%s, path=%s", ...)
		logger.Info(logComponent).
			Str("type", *cfg.PersistType).
			Str("path", cfg.PersistPath).
			Msg("Memory persistence enabled")
	}

	// 对齐 Python：创建管线
	svc.retrieveFlow = svc.createRetrieveFlow()
	svc.summaryFlow = svc.createSummaryFlow()

	// 对齐 Python：logger.info("TaskMemoryService initialized successfully with retrieval=%s, summary=%s", ...)
	logger.Info(logComponent).
		Str("retrieval", retrievalAlgo).
		Str("summary", summaryAlgo).
		Msg("TaskMemoryService initialized successfully")

	return svc, nil
}

// applyConfigDefaults 应用配置默认值。
func applyConfigDefaults(cfg *TaskMemoryServiceConfig) *TaskMemoryServiceConfig {
	if cfg == nil {
		cfg = &TaskMemoryServiceConfig{}
	}
	if cfg.LLMModel == "" {
		cfg.LLMModel = "gpt-5.2"
	}
	if cfg.EmbeddingModel == "" {
		cfg.EmbeddingModel = "text-embedding-3-small"
	}
	if cfg.RetrievalAlgo == "" {
		cfg.RetrievalAlgo = "ACE"
	}
	if cfg.SummaryAlgo == "" {
		cfg.SummaryAlgo = "ACE"
	}
	if cfg.TopKRetrieval == 0 {
		cfg.TopKRetrieval = 10
	}
	if cfg.TopKRerank == 0 {
		cfg.TopKRerank = 5
	}
	if cfg.TopKQuery == 0 {
		cfg.TopKQuery = 1
	}
	if cfg.MaxPlaybookSize == 0 {
		cfg.MaxPlaybookSize = 50
	}
	// 布尔默认值：零值为 false，但 Python 默认是 true
	// 只有在明确设置时才生效，这里不自动设为 true
	// LLMRerank/LLMRewrite/ExtractBestTraj/ExtractWorstTraj/ExtractComparativeTraj
	// 的默认值在 createFlow 中按需设置
	return cfg
}

// createRetrieveFlow 创建检索管线。对齐 Python _create_retrieve_flow。
func (s *TaskMemoryService) createRetrieveFlow() op.BaseOp {
	sc := s.serviceContext

	switch s.retrievalAlgorithm {
	case "ReasoningBank":
		return rbretrieve.NewRBRecallMemoryOp(sc, 1) // topK 默认 1

	case "ACE":
		return aceretrieve.NewACERecallMemoryOp(sc)

	case "ReMe":
		llmRerank := true
		llmRewrite := true
		return op.Seq(emeretrieve.NewRecallMemoryOp(sc, 10)).
			Then(emeretrieve.NewRerankMemoryOp(sc, llmRerank, 5)).
			Then(emeretrieve.NewRewriteMemoryOp(sc, llmRewrite))

	case "RefCon", "DivCon":
		// 同 ReMe 但用硬编码参数
		return op.Seq(emeretrieve.NewRecallMemoryOp(sc, 10)).
			Then(emeretrieve.NewRerankMemoryOp(sc, true, 5)).
			Then(emeretrieve.NewRewriteMemoryOp(sc, true))

	default:
		// 不应到达（NormalizeAlgoName 已校验），防御性返回 ACE
		return aceretrieve.NewACERecallMemoryOp(sc)
	}
}

// createSummaryFlow 创建总结管线。对齐 Python _create_summary_flow。
func (s *TaskMemoryService) createSummaryFlow() op.BaseOp {
	sc := s.serviceContext

	switch s.summaryAlgorithm {
	case "ACE":
		flow := op.Seq(acesummary.NewLoadPlaybookOp(sc)).
			Then(op.Par(acesummary.NewReflectOp(sc, false)).With(acesummary.NewParallelReflectOp(sc, false))).
			Then(op.Par(acesummary.NewCurateOp(sc)).With(acesummary.NewParallelCurateOp(sc))).
			Then(acesummary.NewApplyDeltaOp(sc, 50))
		if s.persistType != nil {
			flow = flow.Then(acesummary.NewPersistMemoryOp(sc, s.persistenceHelper))
		}
		return flow

	case "ReasoningBank":
		parOp := op.Par(rbsummary.NewSummarizeMemoryOp(sc)).With(rbsummary.NewSummarizeMemoryParallelOp(sc))
		flow := op.Seq(parOp).Then(rbsummary.NewUpdateVectorStoreOp(sc))
		if s.persistType != nil {
			flow = flow.Then(rbsummary.NewPersistMemoryOp(sc, s.persistenceHelper))
		}
		return flow

	case "ReMe":
		flow := op.Seq(remesummary.NewTrajectoryPreprocessOp(sc)).
			Then(op.Par(
				remesummary.NewSuccessExtractionOp(sc, true),
			).With(
				remesummary.NewFailureExtractionOp(sc, true),
			).With(
				remesummary.NewComparativeExtractionOp(sc, true),
			)).
			Then(remesummary.NewMemoryValidationOp(sc, true)).
			Then(remesummary.NewMemoryDeduplicationOp(sc, true, 0.9)).
			Then(remesummary.NewUpdateVectorStoreOp(sc))
		if s.persistType != nil {
			flow = flow.Then(remesummary.NewPersistMemoryOp(sc, s.persistenceHelper))
		}
		return flow

	case "RefCon", "DivCon":
		flow := op.Seq(remesummary.NewTrajectoryPreprocessOp(sc)).
			Then(remesummary.NewComparativeAllExtractionOp(sc, true)).
			Then(remesummary.NewMemoryValidationOp(sc, false)).
			Then(remesummary.NewMemoryDeduplicationOp(sc, true, 0.9)).
			Then(remesummary.NewUpdateVectorStoreOp(sc))
		if s.persistType != nil {
			flow = flow.Then(remesummary.NewPersistMemoryOp(sc, s.persistenceHelper))
		}
		return flow

	default:
		// 防御性返回 ACE flow
		return op.Seq(acesummary.NewLoadPlaybookOp(sc)).
			Then(op.Par(acesummary.NewReflectOp(sc, false)).With(acesummary.NewParallelReflectOp(sc, false))).
			Then(op.Par(acesummary.NewCurateOp(sc)).With(acesummary.NewParallelCurateOp(sc))).
			Then(acesummary.NewApplyDeltaOp(sc, 50))
	}
}

// createRBMemory 创建 ReasoningBank 格式的记忆。
func (s *TaskMemoryService) createRBMemory(_ context.Context, userID string, req AddMemoryRequest) (*schema.VectorNode, string) {
	title := ""
	if req.Title != nil {
		title = *req.Title
	}
	description := ""
	if req.Description != nil {
		description = *req.Description
	}
	query := description
	if req.Query != nil {
		query = *req.Query
	}
	label := ""
	if req.Label != nil {
		label = *req.Label
	}

	memoryID := fmt.Sprintf("rb_%s_%s", userID, title)
	metadata := map[string]any{
		"workspace_id": userID,
		"algorithm":    "ReasoningBank",
		"memory_type":  "manual",
		"title":        title,
		"description":  description,
		"query":        query,
		"label":        label,
	}

	return schema.NewVectorNode(memoryID, req.Content, nil, metadata), memoryID
}

// createReMeMemory 创建 ReMe 格式的记忆。
func (s *TaskMemoryService) createReMeMemory(_ context.Context, userID string, req AddMemoryRequest) (*schema.VectorNode, string) {
	whenToUse := ""
	if req.WhenToUse != nil {
		whenToUse = *req.WhenToUse
	}

	memoryID := fmt.Sprintf("reme_%s_%d", userID, len(whenToUse))
	metadata := map[string]any{
		"workspace_id": userID,
		"algorithm":    s.summaryAlgorithm,
		"memory_type":  "manual",
		"score":        1.0,
		"step_type":    "manual",
		"confidence":   1.0,
		"freq":         1,
		"utility":      1,
		"when_to_use":  whenToUse,
	}

	return schema.NewVectorNode(memoryID, req.Content, nil, metadata), memoryID
}

// createACEMemory 创建 ACE 格式的记忆。
func (s *TaskMemoryService) createACEMemory(_ context.Context, userID string, req AddMemoryRequest) (*schema.VectorNode, string) {
	section := req.Section
	if section == "" {
		section = "general"
	}

	// 对齐 Python：生成 ID = section + MD5(content)
	hash := md5.Sum([]byte(req.Content))
	memoryID := fmt.Sprintf("%s_%x", section, hash[:8])

	metadata := map[string]any{
		"workspace_id": userID,
		"algorithm":    "ACE",
		"memory_type":  "manual",
		"section":      section,
		"helpful":      0,
		"harmful":      0,
		"neutral":      0,
	}

	return schema.NewVectorNode(memoryID, req.Content, nil, metadata), memoryID
}

// algoToPersistName 算法名到持久化名的映射。
// 对齐 Python：ACE->ace, ReasoningBank->rb, ReMe/RefCon/DivCon->reme。
func algoToPersistName(algo string) string {
	switch algo {
	case "ACE":
		return "ace"
	case "ReasoningBank":
		return "rb"
	case "ReMe", "RefCon", "DivCon":
		return "reme"
	default:
		return strings.ToLower(algo)
	}
}

// formatRBMemoryString 格式化 ReasoningBank 检索结果为文本。
func formatRBMemoryString(memories any) string {
	// ReasoningBank: title/description/content 格式
	if memories == nil {
		return ""
	}
	// 简单格式化：尝试转为字符串
	return fmt.Sprintf("%v", memories)
}

// formatACEMemoryString 格式化 ACE 检索结果为文本。
func formatACEMemoryString(memories any) string {
	if memories == nil {
		return ""
	}
	return fmt.Sprintf("%v", memories)
}
