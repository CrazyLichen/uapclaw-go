package service

import (
	"context"
	"crypto/md5"
	"fmt"
	"strings"

	cecontext "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/context"
	ceconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/config"
	op "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/op"
	cepersistence "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/persistence"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/vector_store"
	ceschema "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/schema"
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

// RetrieveResult 检索结果。对齐 Python RetrieveResponse(BaseModel)。
type RetrieveResult struct {
	// MemoryString 格式化后的记忆文本
	MemoryString string
	// RetrievedMemory 检索到的记忆项列表
	RetrievedMemory []ceschema.MemoryItem
	// Query 查询
	Query string
	// UserID 用户标识
	UserID string
	// Algorithm 算法名称
	Algorithm string
}

// SummarizeResult 摘要结果。对齐 Python SummarizeResponse(BaseModel)。
type SummarizeResult struct {
	// Memories 更新后的记忆节点列表
	Memories []*schema.VectorNode
	// UserID 用户标识
	UserID string
	// Query 查询
	Query string
	// Algorithm 算法名称
	Algorithm string
}

// AddMemoryResult 添加记忆结果。
type AddMemoryResult struct {
	// Status 操作状态
	Status string
	// MemoryID 记忆标识
	MemoryID string
	// UserID 用户标识
	UserID string
	// Algorithm 算法名称
	Algorithm string
}

// MaTTSResult MaTTS 试验结果。
type MaTTSResult struct {
	// Trials 试验结果列表
	Trials []TrialOutput
	// Query 查询
	Query string
	// UserID 用户标识
	UserID string
	// MattsMode MaTTS 模式
	MattsMode string
}

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
// 仅保留连接/初始化参数，运行时参数（top_k/llm_rerank 等）由 config 实时读取。
// 对齐 Python TaskMemoryService.__init__ 中 self.xxx 字段与 config.get() 的分离。
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
}

// TaskMemoryService 任务记忆服务，提供记忆检索/总结/管理的统一入口。
// 对齐 Python TaskMemoryService。
//
// Python: openjiuwen/extensions/context_evolver/service/task_memory_service.py
type TaskMemoryService struct {
	// serviceContext 共享服务上下文
	serviceContext *cecontext.ServiceContext
	// llm LLM 服务（具体类型，对齐 Python self.llm）
	llm *OpenAILLMWrapper
	// embedding Embedding 服务（具体类型，对齐 Python self.embedding）
	embedding *OpenAIEmbeddingWrapper
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
		logger.Error(logComponent).Err(err).Msg("TaskMemoryService initialization failed")
		return nil, exception.NewBaseError(
			exception.StatusToolchainEvolvingMemoryServiceInitFailed,
			exception.WithMsg(fmt.Sprintf("Failed to create LLM wrapper: %s", err.Error())),
		)
	}

	// 对齐 Python：创建 OpenAIEmbeddingWrapper
	emb, err := NewOpenAIEmbeddingWrapper(cfg.EmbeddingModel, cfg.APIKey, cfg.APIBase)
	if err != nil {
		logger.Error(logComponent).Err(err).Msg("TaskMemoryService initialization failed")
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

// Retrieve 检索记忆。对齐 Python TaskMemoryService.retrieve(user_id, query, **kwargs)。
func (s *TaskMemoryService) Retrieve(ctx context.Context, userID string, query string) (*RetrieveResult, error) {
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
	// 使用 GetTyped[[]ceschema.MemoryItem] 提取，遍历调 FormatMemoryString 构建 MemoryString
	var memoryString string
	retrievedMemories, _ := cecontext.GetTyped[[]ceschema.MemoryItem](rc, "retrieved_memories")

	switch s.retrievalAlgorithm {
	case "ReasoningBank", "ACE":
		// RB/ACE：遍历 MemoryItem 切片，调 FormatMemoryString 拼接
		memoryString = formatMemoryItems(retrievedMemories)
	default:
		// ReMe/RefCon/DivCon：直接从 RuntimeContext 取 memory_string
		if ms, ok := rc.Get("memory_string").(string); ok {
			memoryString = ms
		}
	}

	result := &RetrieveResult{
		MemoryString:    memoryString,
		RetrievedMemory: retrievedMemories,
		Query:           query,
		UserID:          userID,
		Algorithm:       s.retrievalAlgorithm,
	}

	// 对齐 Python：logger.info("Retrieved memories:\n%s\nUsing %s memories", ...)
	logger.Info(logComponent).
		Int("memories_count", len(retrievedMemories)).
		Str("algorithm", s.retrievalAlgorithm).
		Msg("Retrieved memories")

	return result, nil
}

// Summarize 总结记忆。对齐 Python TaskMemoryService.summarize(user_id, matts, query, trajectories, **kwargs)。
func (s *TaskMemoryService) Summarize(ctx context.Context, userID string, matts string, query string, trajectories []string, extraKwargs ...map[string]any) (*SummarizeResult, error) {
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
	memories, _ := cecontext.GetTyped[[]*schema.VectorNode](rc, "memories")

	result := &SummarizeResult{
		Memories:  memories,
		UserID:    userID,
		Query:     query,
		Algorithm: s.summaryAlgorithm,
	}

	return result, nil
}

// AddMemory 手动添加记忆。对齐 Python TaskMemoryService.add_memory(user_id, request)。
func (s *TaskMemoryService) AddMemory(ctx context.Context, userID string, req AddMemoryRequest) (*AddMemoryResult, error) {
	// 对齐 Python：logger.info("Adding manual %s memory for user=%s", ...)
	logger.Info(logComponent).
		Str("algorithm", s.summaryAlgorithm).
		Str("user_id", userID).
		Msg("Adding manual memory")

	var node *schema.VectorNode
	var memoryID string

	switch s.summaryAlgorithm {
	case "ReasoningBank":
		if req.Content == "" || (req.Title == nil || *req.Title == "") || (req.Description == nil || *req.Description == "") {
			return nil, exception.NewBaseError(
				exception.StatusToolchainEvolvingMemoryAddExecutionError,
				exception.WithMsg("ReasoningBank add_memory requires content, title, and description"),
			)
		}
		node, memoryID = s.createRBMemory(ctx, userID, req)
	case "ReMe", "RefCon", "DivCon":
		if req.Content == "" || (req.WhenToUse == nil || *req.WhenToUse == "") {
			return nil, exception.NewBaseError(
				exception.StatusToolchainEvolvingMemoryAddExecutionError,
				exception.WithMsg(fmt.Sprintf("%s add_memory requires content and when_to_use", s.summaryAlgorithm)),
			)
		}
		node, memoryID = s.createReMeMemory(ctx, userID, req)
	case "ACE":
		if req.Content == "" || req.Section == "" {
			return nil, exception.NewBaseError(
				exception.StatusToolchainEvolvingMemoryAddExecutionError,
				exception.WithMsg("ACE add_memory requires content and section"),
			)
		}
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

	// 对齐 Python：持久化（{node_id: node_data} dict 格式）
	if s.persistenceHelper != nil {
		algoName := algoToPersistName(s.summaryAlgorithm)
		if err := s.persistenceHelper.Save(userID, algoName, map[string]any{memoryID: node.ToDict()}); err != nil {
			logger.Error(logComponent).Err(err).Msg("Failed to persist memory")
		}
	}

	// 对齐 Python：logger.info("Added %s memory: %s", ...)
	logger.Info(logComponent).
		Str("algorithm", s.summaryAlgorithm).
		Str("memory_id", memoryID).
		Msg("Added memory")

	return &AddMemoryResult{
		Status:    "success",
		MemoryID:  memoryID,
		UserID:    userID,
		Algorithm: s.summaryAlgorithm,
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

	loaded := 0
	// 对齐 Python：将加载的 node 插入 vectorStore
	// Python 用 {node_id: node_data} dict 格式，不是 {"nodes": [...]} list
	for id, nodeData := range nodesDict {
		if nodeMap, ok := nodeData.(map[string]any); ok {
			vn, err := schema.VectorNodeFromDict(nodeMap)
			if err != nil {
				logger.Warn(logComponent).
					Str("node_id", id).
					Err(err).
					Msg("Failed to load node from persistence")
				continue
			}
			if err := s.vectorStore.Upsert(ctx, vn); err != nil {
				logger.Warn(logComponent).
					Str("node_id", id).
					Err(err).
					Msg("Failed to upsert node into vector store")
			}
			loaded++
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

// GetPlaybook 获取用户的 Playbook 记忆。对齐 Python TaskMemoryService.get_playbook(user_id)。
// 仅 ACE 算法有 Playbook 概念，其他算法返回空。
func (s *TaskMemoryService) GetPlaybook(ctx context.Context, userID string) ([]*schema.VectorNode, error) {
	if s.summaryAlgorithm != "ACE" {
		return nil, nil
	}
	// 使用 vectorStore.GetAll 获取所有 ACE 记忆
	if getAller, ok := s.vectorStore.(interface {
		GetAll(map[string]any) []*schema.VectorNode
	}); ok {
		filter := map[string]any{
			"workspace_id": userID,
			"type":         "ace_memory",
		}
		return getAller.GetAll(filter), nil
	}
	return nil, nil
}

// ClearPlaybook 清除用户的 Playbook 记忆。对齐 Python TaskMemoryService.clear_playbook(user_id)。
// 仅 ACE 算法有 Playbook 概念，其他算法返回 nil。
func (s *TaskMemoryService) ClearPlaybook(ctx context.Context, userID string) error {
	if s.summaryAlgorithm != "ACE" {
		return nil
	}
	// 清除 vectorStore 中所有 ACE 记忆
	if clearer, ok := s.vectorStore.(interface{ Clear() }); ok {
		clearer.Clear()
	}
	return nil
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

// newTaskMemoryServiceWithServices 内部构造逻辑，被 NewTaskMemoryService 共用。
func newTaskMemoryServiceWithServices(
	sc *cecontext.ServiceContext,
	llm *OpenAILLMWrapper,
	emb *OpenAIEmbeddingWrapper,
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
// 仅处理连接/初始化参数，运行时参数由 ceconfig 实时读取。
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
	return cfg
}

// createRetrieveFlow 创建检索管线。对齐 Python _create_retrieve_flow。
// 运行时参数从 ceconfig 实时读取，对齐 Python config.get()。
func (s *TaskMemoryService) createRetrieveFlow() op.BaseOp {
	sc := s.serviceContext

	switch s.retrievalAlgorithm {
	case "ReasoningBank":
		topK := ceconfig.GetInt("TOPK_QUERY", 1)
		return rbretrieve.NewRBRecallMemoryOp(sc, topK)

	case "ACE":
		return aceretrieve.NewACERecallMemoryOp(sc)

	case "ReMe":
		topKRetrieval := ceconfig.GetInt("TOPK_RETRIEVAL", 10)
		topKRerank := ceconfig.GetInt("TOPK_RERANK", 5)
		llmRerank := ceconfig.GetBool("LLM_RERANK", true)
		llmRewrite := ceconfig.GetBool("LLM_REWRITE", true)
		return op.Seq(emeretrieve.NewRecallMemoryOp(sc, topKRetrieval)).
			Then(emeretrieve.NewRerankMemoryOp(sc, llmRerank, topKRerank)).
			Then(emeretrieve.NewRewriteMemoryOp(sc, llmRewrite))

	case "RefCon", "DivCon":
		// 同 ReMe 但用硬编码参数（对齐 Python）
		return op.Seq(emeretrieve.NewRecallMemoryOp(sc, 10)).
			Then(emeretrieve.NewRerankMemoryOp(sc, true, 5)).
			Then(emeretrieve.NewRewriteMemoryOp(sc, true))

	default:
		// 不应到达（NormalizeAlgoName 已校验），防御性返回 ACE
		return aceretrieve.NewACERecallMemoryOp(sc)
	}
}

// createSummaryFlow 创建总结管线。对齐 Python _create_summary_flow。
// 运行时参数从 ceconfig 实时读取，对齐 Python config.get()。
func (s *TaskMemoryService) createSummaryFlow() op.BaseOp {
	sc := s.serviceContext

	switch s.summaryAlgorithm {
	case "ACE":
		maxPlaybookSize := ceconfig.GetInt("MAX_PLAYBOOK_SIZE", 50)
		useGroundTruth := ceconfig.GetBool("USE_GROUNDTRUTH", false)
		flow := op.Seq(acesummary.NewLoadPlaybookOp(sc)).
			Then(op.Par(acesummary.NewReflectOp(sc, useGroundTruth)).With(acesummary.NewParallelReflectOp(sc, useGroundTruth))).
			Then(op.Par(acesummary.NewCurateOp(sc)).With(acesummary.NewParallelCurateOp(sc))).
			Then(acesummary.NewApplyDeltaOp(sc, maxPlaybookSize))
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
		extractBest := ceconfig.GetBool("EXTRACT_BEST_TRAJ", true)
		extractWorst := ceconfig.GetBool("EXTRACT_WORST_TRAJ", true)
		extractComparative := ceconfig.GetBool("EXTRACT_COMPARATIVE_TRAJ", true)
		memoryValidation := ceconfig.GetBool("MEMORY_VALIDATION", true)
		memoryDeduplication := ceconfig.GetBool("MEMORY_DEDUPLICATION", true)
		flow := op.Seq(remesummary.NewTrajectoryPreprocessOp(sc)).
			Then(op.Par(
				remesummary.NewSuccessExtractionOp(sc, extractBest),
			).With(
				remesummary.NewFailureExtractionOp(sc, extractWorst),
			).With(
				remesummary.NewComparativeExtractionOp(sc, extractComparative),
			)).
			Then(remesummary.NewMemoryValidationOp(sc, memoryValidation)).
			Then(remesummary.NewMemoryDeduplicationOp(sc, memoryDeduplication, 0.9)).
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
		maxPlaybookSize := ceconfig.GetInt("MAX_PLAYBOOK_SIZE", 50)
		return op.Seq(acesummary.NewLoadPlaybookOp(sc)).
			Then(op.Par(acesummary.NewReflectOp(sc, false)).With(acesummary.NewParallelReflectOp(sc, false))).
			Then(op.Par(acesummary.NewCurateOp(sc)).With(acesummary.NewParallelCurateOp(sc))).
			Then(acesummary.NewApplyDeltaOp(sc, maxPlaybookSize))
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

	// 对齐 Python: memoryID = reasoning_bank_{workspaceID}_{md5(title|content)}
	combined := title
	if combined == "" {
		combined = req.Content
	}
	contentHash := md5.Sum([]byte(combined))
	memoryID := fmt.Sprintf("reasoning_bank_%s_%x", userID, contentHash)

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

	// 对齐 Python: memoryID = reme_{workspaceID}_{md5(when_to_use)[:12]}
	contentHash := md5.Sum([]byte(whenToUse))
	memoryID := fmt.Sprintf("reme_%s_%x", userID, contentHash[:6]) // 6 bytes = 12 hex chars

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

// formatMemoryItems 遍历 MemoryItem 切片，调 FormatMemoryString 拼接为文本。
// 对齐 Python 各算法的 memory_string 格式化逻辑。
func formatMemoryItems(items []ceschema.MemoryItem) string {
	if len(items) == 0 {
		return ""
	}
	parts := make([]string, 0, len(items))
	for _, item := range items {
		parts = append(parts, item.FormatMemoryString())
	}
	return strings.Join(parts, "\n")
}
