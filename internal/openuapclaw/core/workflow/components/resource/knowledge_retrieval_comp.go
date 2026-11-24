// 8.25 KnowledgeRetrievalComp——知识检索组件（Python knowledge_retrieval_comp.py 一比一）。
// workflow 图叶子执行节点：多 KB 聚合检索（可选 agentic 多跳/图库），惰性初始化，
// 消费领域十三检索管线（13.26-13.28 retrieve_multi_kb_with_source）。
// 已知偏差 D-25-1..4 见 spec docs/superpowers/specs/2026-11-24-825-knowledge-retrieval-comp-design.md §五。
package resource

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/security"
	iface "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/interface"
	forllm "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm"
	llmschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
	modelclients "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/model_clients"
	storeembedding "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/store/embedding"
	graphexe "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/graph/executable"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/runner"
	retrievalcommon "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/retrieval/common"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/retrieval/embedding"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/retrieval/knowledgebase"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/retrieval/retriever"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/retrieval/vector_store"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/workflow/components"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ComponentKBConfig 单知识库配置（Python ComponentKBConfig :42-46 一比一）
type ComponentKBConfig struct {
	// KBConfig 知识库配置（Python kb_config）
	KBConfig retrievalcommon.KnowledgeBaseConfig
	// VectorStoreConfig 向量存储配置（Python vector_store_config）
	VectorStoreConfig retrievalcommon.VectorStoreConfig
	// EmbedConfig 嵌入配置，nil ≙ Python None（vector/hybrid 必填）
	EmbedConfig *embedding.EmbeddingConfig
	// EmbedAdditionalConfig 嵌入额外配置（Python embed_additional_config）
	EmbedAdditionalConfig map[string]any
}

// KnowledgeRetrievalCompConfig 组件配置（Python :49-56 dataclass kw_only 一比一）
type KnowledgeRetrievalCompConfig struct {
	// ComponentKBConfigs 多知识库配置列表
	ComponentKBConfigs []ComponentKBConfig
	// VectorStoreConnectionConfig 向量库连接配置（Python 平铺 kwargs → map 透传）
	VectorStoreConnectionConfig map[string]any
	// RetrievalConfig 检索配置
	RetrievalConfig retrievalcommon.RetrievalConfig
	// ModelID 预注册模型 ID，空串 ≙ Python None
	ModelID string
	// ModelClientConfig 模型客户端配置（agentic 且无 model_id 时直构用）
	ModelClientConfig *llmschema.ModelClientConfig
	// ModelConfig 模型请求配置
	ModelConfig *llmschema.ModelRequestConfig
}

// KnowledgeRetrievalInput 组件输入（Python :59-62；extra=allow → 只取 query 键）
type KnowledgeRetrievalInput struct {
	// Query 检索查询串
	Query string
}

// KnowledgeRetrievalExecutable 执行体（Python KnowledgeRetrievalExecutable :70-78 字段一比一；
// asyncio.Lock 双检 → mu sync.Mutex）
type KnowledgeRetrievalExecutable struct {
	// WorkflowComponent 8.17 组件基座（InvokeFn/TraceMetadataFn 槽承载）
	components.WorkflowComponent
	// cfg 组件配置（Python self._config）
	cfg *KnowledgeRetrievalCompConfig
	// kbs 知识库列表（Python self._kbs，惰性初始化）
	kbs []knowledgebase.KnowledgeBase
	// llm agentic 检索模型实例（Python self._llm，惰性初始化；D-25-5 直构路径经
	// modelClientAdapter 适配能力面）
	llm modelclients.BaseModelClient
	// initialized 惰性初始化标志（Python self._initialized）
	initialized bool
	// mu 初始化锁（Python self._lock asyncio.Lock）
	mu sync.Mutex
	// sess 当前会话（Python self._session）
	sess *session.NodeSessionFacade
}

// KnowledgeRetrievalComponent 组件外壳（Python KnowledgeRetrievalComponent :272-281）
type KnowledgeRetrievalComponent struct {
	// WorkflowComponent 组件基座（槽优先：ToExecutableFn 注入 self，勿 override 基座两方法）
	components.WorkflowComponent
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewKnowledgeRetrievalExecutable 构造执行体（Python __init__ :71-78）
func NewKnowledgeRetrievalExecutable(cfg *KnowledgeRetrievalCompConfig) (*KnowledgeRetrievalExecutable, error) {
	if cfg == nil {
		return nil, exception.BuildError(
			exception.StatusComponentKnowledgeRetrievalInputParamError,
			exception.WithMsg("component config is required"))
	}
	e := &KnowledgeRetrievalExecutable{cfg: cfg}
	e.InvokeFn = e.invokeEntry
	e.TraceMetadataFn = e.traceMetadata
	return e, nil
}

// NewKnowledgeRetrievalComponent 构造组件外壳（Python __init__ :273-275；槽注入 self，
// 8.20 教训：基座槽优先，不 override ToExecutable/AddComponentToGraph 两方法）
func NewKnowledgeRetrievalComponent(cfg *KnowledgeRetrievalCompConfig) (*KnowledgeRetrievalComponent, error) {
	exec, err := NewKnowledgeRetrievalExecutable(cfg)
	if err != nil {
		return nil, err
	}
	c := &KnowledgeRetrievalComponent{}
	c.ToExecutableFn = func() graphexe.GeneralExecutor { return exec }
	c.TraceMetadataFn = exec.traceMetadata
	return c, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// modelClientAdapter 直构路径适配器（D-25-5：Python Model 即 LLM 客户端本体，无转写能力；
// Go model_clients.BaseModelClient 能力面更宽——嵌 *forllm.Model 促齐既有方法，
// TranscribeAudio 显式不支持，与 Python Model 能力面一致）
type modelClientAdapter struct {
	*forllm.Model
}

// TranscribeAudio 直构 Model 不支持音频转写（Python Model 无此能力，语义一致）
func (a *modelClientAdapter) TranscribeAudio(ctx context.Context,
	audioPath string, opts ...llmschema.TranscribeAudioOption,
) (*llmschema.TranscriptionResponse, error) {
	return nil, fmt.Errorf("transcribe audio not supported by directly constructed model")
}

// validateKnowledgeRetrievalInputs 校验并提取输入（Python validate_inputs :249-258 静态方法；
// pydantic ValidationError → 101102，消息对齐 ExceptionUtils.format_validation_error 风格 D-25-4）
func validateKnowledgeRetrievalInputs(inputs map[string]any) (KnowledgeRetrievalInput, error) {
	raw, ok := inputs["query"]
	if !ok {
		return KnowledgeRetrievalInput{}, exception.BuildError(
			exception.StatusComponentKnowledgeRetrievalInputParamError,
			exception.WithMsg("query\n  Field required"))
	}
	// pydantic str 字段 lax 模式不接受 int→str 强转（pythonsem "string" 是 str()
	// 宽松语义，用于提示词渲染，此处不适用）——直接类型断言
	query, ok := raw.(string)
	if !ok {
		return KnowledgeRetrievalInput{}, exception.BuildError(
			exception.StatusComponentKnowledgeRetrievalInputParamError,
			exception.WithMsg(fmt.Sprintf("query\n  Input should be a valid string [%v]", raw)))
	}
	return KnowledgeRetrievalInput{Query: query}, nil
}

// formatKnowledgeRetrievalOutput 组装输出（Python _format_output :260-269；
// KnowledgeRetrievalOutput.model_dump() 等价 map）
func formatKnowledgeRetrievalOutput(results []retrievalcommon.MultiKBRetrievalResult) map[string]any {
	texts := make([]string, 0, len(results))
	for _, r := range results {
		texts = append(texts, r.Text)
	}
	return map[string]any{
		"results": texts,
		"context": strings.Join(texts, "\n\n"),
	}
}

// invokeEntry 执行入口（Python invoke :80-143 十步一比一；InvokeFn 槽绑定）
func (e *KnowledgeRetrievalExecutable) invokeEntry(ctx context.Context,
	inputs map[string]any, sess *session.NodeSessionFacade, modelContext iface.ModelContext,
) (map[string]any, error) {
	// 步骤1：记录会话（Python :82 self._set_session）
	e.setSession(sess)

	// 步骤2：惰性初始化（Python :83）
	if err := e.initializeIfNeeded(ctx); err != nil {
		return nil, err
	}

	// 步骤3：校验并提取 query（Python :86-92）
	in, err := validateKnowledgeRetrievalInputs(inputs)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(in.Query) == "" {
		return nil, exception.BuildError(
			exception.StatusComponentKnowledgeRetrievalInputParamError,
			exception.WithMsg("Query must be a non-empty string"))
	}

	// 步骤4：起始日志（Python :94-106；字段逐项对齐）
	logger.Info(logger.ComponentAgentCore).
		Str("event_type", "WORKFLOW_COMPONENT_START").
		Str("component_id", sess.GetExecutableID()).
		Str("component_type_str", "KnowledgeRetrievalComponent").
		Str("session_id", sess.GetSessionID()).
		Int("query_length", len([]rune(in.Query))).
		Int("top_k", e.cfg.RetrievalConfig.TopK).
		Bool("sensitive_mode", security.IsSensitive()).
		Msg("Knowledge retrieval started")

	// 步骤5：多 KB 聚合检索（Python :108-113；错误日志的 component_id 用入参 session
	// 而非 self._session——Python :118 原样保留）
	results, err := knowledgebase.RetrieveMultiKBWithSource(ctx, e.kbs, in.Query, &e.cfg.RetrievalConfig, nil)
	if err != nil {
		logger.Error(logger.ComponentAgentCore).
			Str("event_type", "WORKFLOW_COMPONENT_ERROR").
			Str("component_id", sess.GetComponentID()).
			Str("component_type_str", "KnowledgeRetrievalComponent").
			Str("session_id", sess.GetSessionID()).
			Err(err).
			Msg("Knowledge retrieval retrieve call failed")
		return nil, exception.BuildError(
			exception.StatusComponentKnowledgeRetrievalInvokeCallFailed,
			exception.WithMsg("Knowledge retrieval retrieve call failed"), exception.WithCause(err))
	}

	// 步骤6：组装输出（Python :129）
	output := formatKnowledgeRetrievalOutput(results)

	// 步骤7：结束日志（Python :131-141）
	logger.Info(logger.ComponentAgentCore).
		Str("event_type", "WORKFLOW_COMPONENT_END").
		Str("component_id", sess.GetExecutableID()).
		Str("component_type_str", "KnowledgeRetrievalComponent").
		Str("session_id", sess.GetSessionID()).
		Int("num_results", len(results)).
		Bool("sensitive_mode", security.IsSensitive()).
		Msg("Knowledge retrieval completed")

	return output, nil
}

// setSession 记录会话（Python _set_session :145-147）
func (e *KnowledgeRetrievalExecutable) setSession(sess *session.NodeSessionFacade) {
	e.sess = sess
}

// traceMetadata 追踪元数据（Python trace_metadata :148-165；TraceMetadataFn 槽绑定：
// top_k → gen_ai.retrieval.top_k，data_source_ids → gen_ai.data_source.id，
// request_model → gen_ai.request.model 仅 agentic 且有 model_id 时）
func (e *KnowledgeRetrievalExecutable) traceMetadata() map[string]any {
	ids := make([]string, 0, len(e.cfg.ComponentKBConfigs))
	for i := range e.cfg.ComponentKBConfigs {
		ids = append(ids, e.cfg.ComponentKBConfigs[i].KBConfig.KBID)
	}
	metadata := map[string]any{
		"top_k":           e.cfg.RetrievalConfig.TopK,
		"data_source_ids": ids,
	}
	if e.cfg.RetrievalConfig.Agentic && e.cfg.ModelID != "" {
		metadata["request_model"] = e.cfg.ModelID
	}
	return metadata
}

// initializeIfNeeded 惰性初始化（Python _initialize_if_needed :167-183；asyncio.Lock
// 双检 → sync.Mutex 双检；失败保持未初始化可重试，wrap 101100）
func (e *KnowledgeRetrievalExecutable) initializeIfNeeded(ctx context.Context) error {
	if e.initialized {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.initialized {
		return nil
	}
	// Python :175：agentic 时先建模型实例
	if e.cfg.RetrievalConfig.Agentic {
		llm, err := e.createLLMInstance(ctx)
		if err != nil {
			return exception.BuildError(
				exception.StatusComponentKnowledgeRetrievalInvokeCallFailed,
				exception.WithMsg("Failed to initialise knowledge retrieval component"),
				exception.WithCause(err))
		}
		e.llm = llm
	}
	// Python :176：建知识库列表
	kbs, err := e.createKnowledgeBases(ctx)
	if err != nil {
		return exception.BuildError(
			exception.StatusComponentKnowledgeRetrievalInvokeCallFailed,
			exception.WithMsg("Failed to initialise knowledge retrieval component"),
			exception.WithCause(err))
	}
	e.kbs = kbs
	e.initialized = true
	return nil
}

// createKnowledgeBases 构建知识库列表（Python _create_knowledge_bases :185-233）。
// D-25-2：Go KB 收显式 retriever（13.27 不对称直译），组件按 index_type 组装检索器；
// D-25-3：Go NewOpenAIEmbedding 无 error 返回，embed 构造失败分支收窄为配置校验。
func (e *KnowledgeRetrievalExecutable) createKnowledgeBases(ctx context.Context) ([]knowledgebase.KnowledgeBase, error) {
	// Python :186 retrieval_config.use_graph（Go *bool，nil → 默认 false）
	useGraph := e.cfg.RetrievalConfig.UseGraph != nil && *e.cfg.RetrievalConfig.UseGraph
	kbInstances := make([]knowledgebase.KnowledgeBase, 0, len(e.cfg.ComponentKBConfigs))

	for i := range e.cfg.ComponentKBConfigs {
		kbCfg := &e.cfg.ComponentKBConfigs[i]

		// Python :190-192 create_vector_store(vector_store_config, **connection_config)
		vectorStore, err := vector_store.CreateVectorStore(kbCfg.VectorStoreConfig)
		if err != nil {
			return nil, err
		}

		// Python :194-212 embed model needed for vector/hybrid search
		var embedModel storeembedding.BaseEmbedding
		if kbCfg.EmbedConfig == nil {
			if kbCfg.KBConfig.IndexType == retrievalcommon.IndexTypeVector ||
				kbCfg.KBConfig.IndexType == retrievalcommon.IndexTypeHybrid {
				return nil, exception.BuildError(
					exception.StatusComponentKnowledgeRetrievalEmbedModelInitError,
					exception.WithMsg("Embedding config is required for vector or hybrid index type"))
			}
		} else {
			embedModel = embedding.NewOpenAIEmbedding(*kbCfg.EmbedConfig)
		}

		// D-25-2：按索引类型组装检索器（Python KB 内部组装的 Go 显式化）
		retr, err := e.buildRetriever(kbCfg, vectorStore, embedModel)
		if err != nil {
			return nil, err
		}

		// Python :214-231 use_graph 二选一构造
		var kb knowledgebase.KnowledgeBase
		if useGraph {
			kb, err = knowledgebase.NewGraphKnowledgeBase(kbCfg.KBConfig, retr, retr,
				knowledgebase.WithVectorStore(vectorStore),
				knowledgebase.WithEmbedModel(embedModel),
				knowledgebase.WithLLMClient(e.llm))
		} else {
			kb, err = knowledgebase.NewSimpleKnowledgeBase(kbCfg.KBConfig, retr,
				knowledgebase.WithVectorStore(vectorStore),
				knowledgebase.WithEmbedModel(embedModel),
				knowledgebase.WithLLMClient(e.llm))
		}
		if err != nil {
			return nil, err
		}
		kbInstances = append(kbInstances, kb)
	}
	return kbInstances, nil
}

// buildRetriever 按索引类型组装检索器（D-25-2；Python 无此函数——KB 内部组装的 Go 显式化；
// agentic 时包装 AgenticRetriever，13.25 语义）
func (e *KnowledgeRetrievalExecutable) buildRetriever(
	kbCfg *ComponentKBConfig, vs vector_store.VectorStore, em storeembedding.BaseEmbedding,
) (retriever.Retriever, error) {
	var base retriever.Retriever
	switch kbCfg.KBConfig.IndexType {
	case retrievalcommon.IndexTypeBM25:
		base = retriever.NewSparseRetriever(vs)
	case retrievalcommon.IndexTypeHybrid:
		base = retriever.NewHybridRetriever(vs, em)
	default:
		base = retriever.NewVectorRetriever(vs, em)
	}
	if e.cfg.RetrievalConfig.Agentic && e.llm != nil {
		return retriever.NewAgenticRetriever(base, e.llm)
	}
	return base, nil
}

// createLLMInstance 创建 agentic 检索模型实例（Python _create_llm_instance :235-246；
// D-25-1：get_model(id=...) 单实例 → GetModel(ctx, []string) 取 [0]，8.18 getModel 先例）
func (e *KnowledgeRetrievalExecutable) createLLMInstance(ctx context.Context) (modelclients.BaseModelClient, error) {
	if e.cfg.ModelID == "" {
		if e.cfg.ModelClientConfig == nil || e.cfg.ModelConfig == nil {
			return nil, exception.BuildError(
				exception.StatusComponentKnowledgeRetrievalLLMModelInitError,
				exception.WithMsg("LLM model config is required for agentic retrieval"))
		}
		m, err := forllm.NewModel(e.cfg.ModelClientConfig, e.cfg.ModelConfig)
		if err != nil {
			return nil, err
		}
		return &modelClientAdapter{Model: m}, nil
	}
	// Python :244-246 from runner import Runner; await Runner.resource_mgr.get_model(id=...)
	mgr := runner.GetResourceMgr()
	if mgr == nil {
		return nil, fmt.Errorf("resource mgr is not initialized")
	}
	clients, err := mgr.GetModel(ctx, []string{e.cfg.ModelID})
	if err != nil {
		return nil, err
	}
	if len(clients) == 0 {
		return nil, fmt.Errorf("model %s not found", e.cfg.ModelID)
	}
	return clients[0], nil
}
