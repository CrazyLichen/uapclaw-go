package graph_memory

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"strings"
	"sync"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/prompt"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/embedding"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/graph"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/query"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/reranker"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/graph/extraction"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/graph/extraction/prompts/entity_extraction"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/graph/extraction/registry"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"golang.org/x/sync/errgroup"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SearchResult 搜索结果
//
// Python: dict[str, list[tuple[float, BaseGraphObject]]]
type SearchResult struct {
	// Entity 实体搜索结果（分数 + 实体）
	Entity []ScoredGraphObject
	// Relation 关系搜索结果（分数 + 关系）
	Relation []ScoredGraphObject
	// Episode 片段搜索结果（分数 + 片段）
	Episode []ScoredGraphObject
}

// ScoredGraphObject 带分数的图对象
type ScoredGraphObject struct {
	// Score 相似度分数
	Score float64
	// Object 图对象（*graph.Entity / *graph.Relation / *graph.Episode）
	Object any
}

// searchStrategyTuple 搜索策略三元组（实体/关系/片段各一个 SearchConfig）
//
// Python: tuple[SearchConfig, SearchConfig, SearchConfig]
type searchStrategyTuple struct {
	// EntityConfig 实体搜索配置
	EntityConfig *config.SearchConfig
	// RelationConfig 关系搜索配置
	RelationConfig *config.SearchConfig
	// EpisodeConfig 片段搜索配置
	EpisodeConfig *config.SearchConfig
}

// AddMemoryConfig AddMemory 调用配置
//
// Python: add_memory 的关键字参数
type AddMemoryConfig struct {
	// SourceType 片段类型
	SourceType config.EpisodeType
	// UserID 用户标识
	UserID string
	// Content 内容字符串（与 Messages 二选一）
	Content string
	// Messages 消息列表输入（与 Content 二选一，仅 CONVERSATION 类型）
	Messages []llmschema.BaseMessage
	// ContentFmtKwargs 内容格式化参数（对话场景的角色映射，仅 Messages 时有效）
	ContentFmtKwargs map[string]string
	// ReferenceTime 参考时间，nil 表示使用当前时间
	ReferenceTime *time.Time
}

// SearchConfigOptions Search 调用配置
//
// Python: search 的关键字参数
type SearchConfigOptions struct {
	// SearchStrategy 搜索策略名称
	SearchStrategy string
	// SearchEntity 是否搜索实体
	SearchEntity bool
	// SearchRelation 是否搜索关系
	SearchRelation bool
	// SearchEpisode 是否搜索片段
	SearchEpisode bool
	// QueryEmbedding 预计算的查询向量，nil 表示使用嵌入模型
	QueryEmbedding []float64
}

// GraphMemory 图记忆主类，维护知识图谱的添加和检索
//
// 通过 LLM 从内容中抽取实体和关系，合并/去重后存储到图数据库，
// 并支持跨实体、关系和片段的语义搜索。
//
// Python: GraphMemory (base.py)
type GraphMemory struct {
	// DBBackend 图数据库后端
	DBBackend graph.BaseGraphStore
	// Config 图存储配置
	Config *graph.GraphConfig
	// LLMClient LLM 客户端
	LLMClient *llm.Model
	// LLMStructuredOutput 是否使用结构化输出
	LLMStructuredOutput bool
	// Reranker 重排序器
	Reranker reranker.BaseReranker
	// DefaultExtractionStrategy 默认抽取策略
	DefaultExtractionStrategy *config.AddMemStrategy
	// LLMExtraKwargs 额外 LLM 参数
	LLMExtraKwargs map[string]any
	// Language 默认语言
	Language string
	// Debug 是否开启调试日志
	Debug bool
	// TokenRecord token 使用记录
	//
	// Python 的 asyncio 单线程无需加锁；Go 并发调用 InvokeLLM 时需加锁保护
	TokenRecord map[string]int
	// tokenRecordMu TokenRecord 并发写锁
	tokenRecordMu sync.Mutex
	// UserLocks 用户级锁映射
	UserLocks map[string]*sync.Mutex
	// Semaphore LLM 并发信号量
	Semaphore chan struct{}
	// ThreadLock 全局互斥锁
	ThreadLock sync.Mutex
	// TimeTillNextGC 下次 GC 间隔（秒），<0 表示禁用
	TimeTillNextGC float64
	// metricIsSim 是否使用相似度分数（距离越小越相似 → false；越大越相似 → true）
	//
	// Python: self.metric_is_sim = self.db_backend.return_similarity_score
	// 从 dbBackend.ReturnSimilarityScore() 获取
	metricIsSim bool
	// searchStrategies 已注册的搜索策略
	searchStrategies map[string]*searchStrategyTuple
	// lastGC 上次 GC 时间戳
	lastGC float64
	// embedderVal 内部缓存的嵌入模型实例
	//
	// Python: self.db_backend.embedder
	// Go 中 BaseGraphStore 没有 Embedder() 方法，通过 AttachEmbedder 绑定后在此缓存
	embedderVal embedding.BaseEmbedding
	// dbKwargs 传递给图存储工厂的额外参数
	//
	// Python: GraphMemory.__init__(db_kwargs) → GraphStoreFactory.from_config(config, **db_kwargs)
	// 在 NewGraphMemory 中应用选项后传给 NewFromConfig
	dbKwargs map[string]any
}

// tzTaskResult 时区预测异步任务结果
type tzTaskResult struct {
	response string
	err      error
}

// ──────────────────────────── 枚举 ────────────────────────────

// GraphMemoryOption GraphMemory 构造选项函数
type GraphMemoryOption func(*GraphMemory)

// SearchOption Search 调用选项函数
type SearchOption func(*SearchConfigOptions)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// defaultConcurrencyLimit 默认 LLM 并发限制
	//
	// Python: self._semaphore = asyncio.Semaphore(concurrency_limit)
	// 对齐 Python: concurrency_limit = self.db_backend.config.max_concurrent or 8
	defaultConcurrencyLimit = 8
	// defaultGCTime 默认 GC 间隔（秒）
	//
	// Python: self.time_till_next_gc = 300
	defaultGCTime = 300
	// defaultSearchMinScoreEntity 默认实体搜索最低分数
	defaultSearchMinScoreEntity = 0.02
	// defaultSearchMinScoreEpisode 默认片段搜索最低分数
	defaultSearchMinScoreEpisode = 0.025
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewGraphMemory 创建 GraphMemory 实例
//
// Python: GraphMemory.__init__(db_config, llm_client, llm_structured_output, reranker,
//
//	extraction_strategy, db_kwargs, llm_extra_kwargs, language, debug)
func NewGraphMemory(dbConfig *graph.GraphConfig, opts ...GraphMemoryOption) (*GraphMemory, error) {
	// 应用选项以获取 db_kwargs 及其他选项
	gm := &GraphMemory{
		Config:                    dbConfig,
		LLMStructuredOutput:       true,
		DefaultExtractionStrategy: config.NewAddMemStrategy(),
		TokenRecord:               map[string]int{"input_tokens": 0, "output_tokens": 0},
		UserLocks:                 make(map[string]*sync.Mutex),
		searchStrategies: map[string]*searchStrategyTuple{
			"default": {
				EntityConfig:   config.NewSearchConfig(),
				RelationConfig: newSearchConfigWithMinScore(defaultSearchMinScoreEntity),
				EpisodeConfig:  newSearchConfigWithMinScore(defaultSearchMinScoreEpisode),
			},
		},
		lastGC: float64(time.Now().Unix()),
	}

	// 应用选项（包括 WithDBKwargs、WithLanguage 等）
	for _, opt := range opts {
		opt(gm)
	}

	// 创建数据库后端
	// Python: self.db_backend = GraphStoreFactory.from_config(config=db_config, **db_kwargs)
	dbBackend, err := graph.NewFromConfig(dbConfig, gm.dbKwargs)
	if err != nil {
		return nil, err
	}

	gm.DBBackend = dbBackend

	// Python: self.metric_is_sim = self.db_backend.return_similarity_score
	gm.metricIsSim = dbBackend.ReturnSimilarityScore()

	// 校验并确定语言
	// Python: self.language = ensure_valid_language(language, db_config.db_storage_config.language)
	// 如果 WithLanguage 已设置语言，使用该值；否则默认 "cn"
	language := gm.Language
	if language == "" {
		language = "cn"
	}
	if dbConfig.StorageConfig != nil {
		validLang, langErr := entity_extraction.EnsureValidLanguage(language, dbConfig.StorageConfig.Language)
		if langErr != nil {
			return nil, exception.BuildError(exception.StatusMemoryGraphLanguageInvalid,
				exception.WithParam("error_msg", langErr.Error()),
			)
		}
		language = validLang
	}
	gm.Language = language

	// 设置并发限制和 GC 间隔
	concurrencyLimit := defaultConcurrencyLimit
	if dbConfig.MaxConcurrent > 0 {
		concurrencyLimit = dbConfig.MaxConcurrent
	}
	gm.Semaphore = make(chan struct{}, concurrencyLimit)
	gm.TimeTillNextGC = defaultGCTime

	return gm, nil
}

// WithLLMClient 设置 LLM 客户端
func WithLLMClient(client *llm.Model) GraphMemoryOption {
	return func(gm *GraphMemory) { gm.LLMClient = client }
}

// WithLLMStructuredOutput 设置是否使用结构化输出
func WithLLMStructuredOutput(enabled bool) GraphMemoryOption {
	return func(gm *GraphMemory) { gm.LLMStructuredOutput = enabled }
}

// WithReranker 设置重排序器
func WithReranker(r reranker.BaseReranker) GraphMemoryOption {
	return func(gm *GraphMemory) { gm.Reranker = r }
}

// WithExtractionStrategy 设置抽取策略
func WithExtractionStrategy(strategy *config.AddMemStrategy) GraphMemoryOption {
	return func(gm *GraphMemory) { gm.DefaultExtractionStrategy = strategy }
}

// WithLLMExtraKwargs 设置额外 LLM 参数
func WithLLMExtraKwargs(kwargs map[string]any) GraphMemoryOption {
	return func(gm *GraphMemory) { gm.LLMExtraKwargs = kwargs }
}

// WithLanguage 设置默认语言
func WithLanguage(lang string) GraphMemoryOption {
	return func(gm *GraphMemory) { gm.Language = lang }
}

// WithDebug 设置调试模式
func WithDebug(debug bool) GraphMemoryOption {
	return func(gm *GraphMemory) { gm.Debug = debug }
}

// WithDBKwargs 设置传递给图存储工厂的额外参数
//
// Python: GraphMemory.__init__(db_kwargs) → GraphStoreFactory.from_config(config, **db_kwargs)
func WithDBKwargs(kwargs map[string]any) GraphMemoryOption {
	return func(gm *GraphMemory) { gm.dbKwargs = kwargs }
}

// Embedder 获取图后端使用的嵌入模型
//
// Python: @property embedder
func (gm *GraphMemory) Embedder() embedding.BaseEmbedding {
	return gm.embedderVal
}

// AttachEmbedder 绑定嵌入模型
//
// Python: attach_embedder(embedder)
func (gm *GraphMemory) AttachEmbedder(emb embedding.BaseEmbedding) error {
	gm.embedderVal = emb
	return gm.DBBackend.AttachEmbedder(emb)
}

// AttachReranker 绑定重排序器
//
// Python: attach_reranker(reranker)
func (gm *GraphMemory) AttachReranker(r reranker.BaseReranker) error {
	if r == nil {
		return exception.BuildError(exception.StatusMemoryStoreValidationInvalid,
			exception.WithParam("store_type", storeType),
			exception.WithParam("error_msg", "Reranker 必须是 Reranker 的实现，传入的值为 nil"),
		)
	}
	gm.Reranker = r
	return nil
}

// RegisterSearchStrategy 注册搜索策略
//
// Python: register_search_strategy(name, search_entity, search_relation, search_episode, force)
func (gm *GraphMemory) RegisterSearchStrategy(
	name string,
	searchEntity *config.SearchConfig,
	searchRelation *config.SearchConfig,
	searchEpisode *config.SearchConfig,
	force ...bool,
) error {
	// Go 类型系统已保证是 SearchConfig，无需 isinstance 检查
	_ = []*config.SearchConfig{searchEntity, searchRelation, searchEpisode}

	gm.ThreadLock.Lock()
	defer gm.ThreadLock.Unlock()

	if name == "" {
		return exception.BuildError(exception.StatusMemoryStoreValidationInvalid,
			exception.WithParam("store_type", storeType),
			exception.WithParam("error_msg", "搜索配置不能注册为空值"),
		)
	}

	forceRegister := len(force) > 0 && force[0]
	if _, exists := gm.searchStrategies[name]; exists && !forceRegister {
		return exception.BuildError(exception.StatusMemoryStoreValidationInvalid,
			exception.WithParam("store_type", storeType),
			exception.WithParam("error_msg", fmt.Sprintf("名为 [%s] 的搜索配置已存在", name)),
		)
	}

	entityConf := searchEntity
	if entityConf == nil {
		// 对齐 Python: Entity 默认 MinScore = defaultSearchMinScoreEntity (0.02)，而非 SearchConfig 默认的 0.3
		entityConf = newSearchConfigWithMinScore(defaultSearchMinScoreEntity)
	}
	relationConf := searchRelation
	if relationConf == nil {
		relationConf = newSearchConfigWithMinScore(defaultSearchMinScoreEntity)
	}
	episodeConf := searchEpisode
	if episodeConf == nil {
		episodeConf = newSearchConfigWithMinScore(defaultSearchMinScoreEpisode)
	}

	gm.searchStrategies[name] = &searchStrategyTuple{
		EntityConfig:   entityConf,
		RelationConfig: relationConf,
		EpisodeConfig:  episodeConf,
	}
	return nil
}

// EnsureThreadLock 确保用户级锁存在（对齐 Python ensure_thread_lock）
//
// Python: ensure_thread_lock(user_id)
func (gm *GraphMemory) EnsureThreadLock(userID string) {
	gm.ThreadLock.Lock()
	defer gm.ThreadLock.Unlock()
	if _, ok := gm.UserLocks[userID]; !ok {
		gm.UserLocks[userID] = &sync.Mutex{}
	}
}

// AddMemory 添加记忆片段到知识图谱（完整 16 步管线）
//
// Python: add_memory(src_type, user_id, content, content_fmt_kwargs, reference_time)
func (gm *GraphMemory) AddMemory(ctx context.Context, cfg AddMemoryConfig) (*GraphMemUpdate, error) {
	gm.EnsureThreadLock(cfg.UserID)

	// 校验嵌入模型
	// Python: if not self.embedder: raise ...
	embedder := gm.Embedder()
	if embedder == nil {
		return nil, exception.BuildError(exception.StatusMemoryGraphEmbedModelNotFound,
			exception.WithParam("error_msg", "use the attach_embedder method to attach one"),
		)
	}

	userLock := gm.UserLocks[cfg.UserID]
	userLock.Lock()
	defer userLock.Unlock()

	// 初始化状态
	state := gm.initState(cfg.ReferenceTime, cfg.SourceType)

	// 1. 准备片段内容 + 检索历史
	content, err := gm.prepareEpisodes(ctx, state, cfg.Content, cfg.Messages, cfg.SourceType, cfg.UserID, cfg.ContentFmtKwargs)
	if err != nil {
		return nil, err
	}
	logger.Debug(logComponent).Str("step", "1_prepare_episodes").Str("user_id", cfg.UserID).Msg("AddMemory step completed")

	// 2. 创建当前 Episode
	currentEpisode, err := CreateEpisode(ctx, gm.DBBackend, cfg.UserID, content, state)
	if err != nil {
		return nil, err
	}
	logger.Debug(logComponent).Str("step", "2_create_episode").Str("user_id", cfg.UserID).Msg("AddMemory step completed")

	// 3. 内容前加时间戳
	content = graph.FormatTimestampISO(state.ReferenceTimestamp, nil) + "\n" + content
	logger.Debug(logComponent).Str("step", "3_prepend_timestamp").Str("user_id", cfg.UserID).Msg("AddMemory step completed")

	// 4. 时区预测（异步，对齐 Python: tz_task = asyncio.create_task(...)）
	tzTaskCh := gm.startTimezoneTask(ctx, content, state)
	logger.Debug(logComponent).Str("step", "4_start_tz_task").Str("user_id", cfg.UserID).Msg("AddMemory step completed")

	// 5. 抽取实体声明
	noExistingEntity, extractedDeclarations, err := gm.extractEntityDeclarations(ctx, cfg.SourceType, content, state)
	if err != nil {
		return nil, err
	}
	logger.Debug(logComponent).Str("step", "5_extract_entities").Str("user_id", cfg.UserID).
		Int("declaration_count", len(extractedDeclarations)).Msg("AddMemory step completed")

	// 6. 等待时区结果，发起关系抽取（对齐 Python: response = await tz_task; state.tasks.append(...)）
	tzResult := <-tzTaskCh
	if tzResult.err != nil {
		return nil, tzResult.err
	}
	// 将关系抽取任务追加到 state.tasks（对齐 Python: state.tasks.append(asyncio.create_task(...))）
	relationTask := gm.startRelationExtractionAsync(ctx, extractedDeclarations, content, state, tzResult.response)
	state.Tasks = append(state.Tasks, relationTask)
	logger.Debug(logComponent).Str("step", "6_relation_extraction").Str("user_id", cfg.UserID).Msg("AddMemory step completed")

	// 7. 获取相关已有实体
	if err := gm.fetchRelevantEntities(ctx, extractedDeclarations, noExistingEntity, cfg.UserID, state); err != nil {
		return nil, err
	}
	logger.Debug(logComponent).Str("step", "7_fetch_entities").Str("user_id", cfg.UserID).Msg("AddMemory step completed")

	// 8. 实体去重（对齐 Python: if existing_entities_list: state.tasks.append(...)）
	existingEntitiesList := gm.entityListFromState(state)
	if len(existingEntitiesList) > 0 {
		dedupeTask := gm.startEntityDedupeAsync(ctx, content, extractedDeclarations, existingEntitiesList, state)
		state.Tasks = append(state.Tasks, dedupeTask)
	}
	logger.Debug(logComponent).Str("step", "8_entity_dedupe").Str("user_id", cfg.UserID).Msg("AddMemory step completed")

	// 9. 实体合并
	resolvedList, err := gm.entityMerge(ctx, extractedDeclarations, existingEntitiesList, state)
	if err != nil {
		return nil, err
	}
	logger.Debug(logComponent).Str("step", "9_entity_merge").Str("user_id", cfg.UserID).Msg("AddMemory step completed")

	// 10. 解析关系抽取结果（对齐 Python: response = await state.tasks.pop(0)）
	if len(state.Tasks) == 0 {
		return nil, exception.BuildError(exception.StatusMemoryGraphInvokeLlmFailed,
			exception.WithParam("error_msg", "relation extraction task not found in state.tasks"),
		)
	}
	relationTaskResult := state.Tasks[0]
	state.Tasks = state.Tasks[1:]
	relationContent, relationErr := relationTaskResult.Wait()
	if relationErr != nil {
		return nil, relationErr
	}
	parsedRelations := extraction.ParseJSON(relationContent, state.Prompting.SchemaRelationMerge)
	if parsedRelations == nil {
		parsedRelations = []any{}
	}
	// 对齐 Python: parse_all_relations(..., entities=extracted_declarations)
	// resolvedList 已是 []EntityOrDeclaration（包含 EntityDeclaration 和 Entity 混合类型）
	relations, entities := ParseAllRelations(
		anyListToMapList(extraction.EnsureList(parsedRelations)),
		resolvedList,
		state.EntityTypes,
		state.CurrentTimestamp,
		cfg.UserID,
	)
	logger.Debug(logComponent).Str("step", "10_parse_relations").Str("user_id", cfg.UserID).
		Int("relation_count", len(relations)).Int("entity_count", len(entities)).Msg("AddMemory step completed")

	// 11. 实体摘要与属性抽取
	entities, err = gm.entityEnrich(ctx, entities, content, state)
	if err != nil {
		return nil, err
	}
	logger.Debug(logComponent).Str("step", "11_entity_enrich").Str("user_id", cfg.UserID).Msg("AddMemory step completed")

	// 12. 关系过滤
	if err := gm.parseRelationFilteringResult(ctx, relations, state); err != nil {
		return nil, err
	}
	logger.Debug(logComponent).Str("step", "12_relation_filter").Str("user_id", cfg.UserID).Msg("AddMemory step completed")

	// 13. 关系去重
	relations, err = gm.handleRelationDedupe(ctx, cfg.UserID, content, relations, state)
	if err != nil {
		return nil, err
	}
	logger.Debug(logComponent).Str("step", "13_relation_dedupe").Str("user_id", cfg.UserID).Msg("AddMemory step completed")

	// 14. 更新因关系删除而受影响的实体（对齐 Python: _update_entities_for_relation_removal(state, update_needs_embed)）
	gm.updateEntitiesForRelationRemoval(ctx, state, resolvedList)
	logger.Debug(logComponent).Str("step", "14_update_entities_for_removal").Str("user_id", cfg.UserID).Msg("AddMemory step completed")

	// 15. 后处理 + 持久化
	if err := ProcessRelations(ctx, gm.DBBackend, entities, relations, state); err != nil {
		return nil, err
	}
	if err := ProcessEntities(ctx, gm.DBBackend, entities, currentEpisode, state); err != nil {
		return nil, err
	}
	ValidateEntitiesEpisodes(entities, currentEpisode, state)

	embedderForPersist := gm.Embedder()
	if embedderForPersist == nil {
		return nil, exception.BuildError(exception.StatusMemoryGraphEmbedModelNotFound,
			exception.WithParam("error_msg", "use the attach_embedder method to attach one"),
		)
	}
	if err := PersistToDB(ctx, gm.DBBackend, state, embedderForPersist, gm.Config); err != nil {
		return nil, err
	}
	logger.Debug(logComponent).Str("step", "15_persist").Str("user_id", cfg.UserID).Msg("AddMemory step completed")

	// 16. 清理 + 刷新
	state.ClearReferences()
	// 对齐 Python: await self.db_backend.refresh(skip_compact=True) — 步骤16 不压缩
	if err := gm.DBBackend.Refresh(ctx, graph.WithSkipCompact(true)); err != nil {
		logger.Warn(logComponent).Err(err).Msg("Graph Memory: refresh failed")
	}

	// GC 检查（对齐 Python: gc.collect()）
	gm.maybeGC(ctx)

	logger.Debug(logComponent).Str("step", "16_cleanup_refresh").Str("user_id", cfg.UserID).Msg("AddMemory step completed")

	return state.MemUpdate.Merge(state.MemUpdateSkipEmbed), nil
}

// Search 搜索图记忆
//
// Python: search(query, user_id, search_strategy, entity, relation, episode, query_embedding)
func (gm *GraphMemory) Search(ctx context.Context, query string, userIDs []string, searchEntity, searchRelation, searchEpisode bool, opts ...SearchOption) (*SearchResult, error) {
	searchOpts := &SearchConfigOptions{
		SearchStrategy: "default",
		SearchEntity:   searchEntity,
		SearchRelation: searchRelation,
		SearchEpisode:  searchEpisode,
	}
	for _, opt := range opts {
		opt(searchOpts)
	}

	// 校验策略存在
	strategyName := searchOpts.SearchStrategy
	if _, ok := gm.searchStrategies[strategyName]; !ok {
		if strings.TrimSpace(strategyName) == "" {
			return nil, exception.BuildError(exception.StatusMemoryStoreValidationInvalid,
				exception.WithParam("store_type", storeType),
				exception.WithParam("error_msg", "策略必须为非空字符串"),
			)
		}
		return nil, exception.BuildError(exception.StatusMemoryStoreValidationInvalid,
			exception.WithParam("store_type", storeType),
			exception.WithParam("error_msg", fmt.Sprintf("未找到策略 [%s]，请使用 register_search_configs 方法注册或使用 \"default\"", strategyName)),
		)
	}

	// 校验输入
	validatedUserIDs, err := ValidateSearchInput(query, userIDs, []bool{searchEntity, searchRelation, searchEpisode})
	if err != nil {
		return nil, err
	}

	// 获取查询向量
	queryEmbedding := searchOpts.QueryEmbedding
	if queryEmbedding == nil {
		embedder := gm.Embedder()
		if embedder == nil {
			return nil, exception.BuildError(exception.StatusMemoryGraphEmbedModelNotFound,
				exception.WithParam("error_msg", "请使用 attach_embedder 方法挂载嵌入模型"),
			)
		}
		queryEmbedding, err = embedder.EmbedQuery(ctx, query)
		if err != nil {
			return nil, err
		}
	} else {
		// 校验 query_embedding 类型（对齐 Python: elif not (isinstance(query_embedding, list) and all(isinstance(val, float) for val in query_embedding))）
		for _, val := range queryEmbedding {
			if math.IsNaN(val) || math.IsInf(val, 0) {
				return nil, exception.BuildError(exception.StatusMemoryStoreValidationInvalid,
					exception.WithParam("store_type", storeType),
					exception.WithParam("error_msg", "query_embedding 必须是 []float64 或 nil"),
				)
			}
		}
	}

	result := &SearchResult{}

	// 并发搜索三个集合（对齐 Python asyncio.as_completed）
	g, gctx := errgroup.WithContext(ctx)

	if searchEntity {
		g.Go(func() error {
			objects, err := gm.performSearch(gctx, 0, validatedUserIDs, strategyName, query, queryEmbedding)
			if err != nil {
				return err
			}
			result.Entity = objects
			return nil
		})
	}

	if searchRelation {
		g.Go(func() error {
			objects, err := gm.performSearch(gctx, 1, validatedUserIDs, strategyName, query, queryEmbedding)
			if err != nil {
				return err
			}
			result.Relation = objects
			return nil
		})
	}

	if searchEpisode {
		g.Go(func() error {
			objects, err := gm.performSearch(gctx, 2, validatedUserIDs, strategyName, query, queryEmbedding)
			if err != nil {
				return err
			}
			result.Episode = objects
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return nil, err
	}

	return result, nil
}

// WithSearchStrategy 设置搜索策略名称
func WithSearchStrategy(name string) SearchOption {
	return func(o *SearchConfigOptions) { o.SearchStrategy = name }
}

// WithQueryEmbedding 设置预计算的查询向量
func WithQueryEmbedding(emb []float64) SearchOption {
	return func(o *SearchConfigOptions) { o.QueryEmbedding = emb }
}

// InvokeLLM 调用 LLM（带重试和信号量控制）
//
// Python: _invoke_llm(kwargs, template, output_model, **extra)
func (gm *GraphMemory) InvokeLLM(ctx context.Context, kwargs map[string]any, tmpl *prompt.PromptTemplate, outputModel map[string]any, extra ...map[string]any) (*llmschema.AssistantMessage, error) {
	// 校验 LLM 客户端
	if gm.LLMClient == nil {
		return nil, exception.BuildError(exception.StatusMemoryGraphInvokeLlmFailed,
			exception.WithParam("error_msg", "LLM 客户端未设置"),
		)
	}

	// 组装调用参数
	outputModelToUse := outputModel
	if !gm.LLMStructuredOutput {
		outputModelToUse = nil
	}

	params, err := AssembleInvokeParams(kwargs, tmpl, outputModelToUse)
	if err != nil {
		return nil, err
	}

	// 合并 llm_extra_kwargs
	if gm.LLMExtraKwargs != nil {
		for k, v := range gm.LLMExtraKwargs {
			params[k] = v
		}
	}
	// 合并 extra
	for _, e := range extra {
		for k, v := range e {
			params[k] = v
		}
	}

	// 从 params 构建消息列表
	messages, err := gm.buildMessages(params)
	if err != nil {
		return nil, err
	}

	// 构建 InvokeOption
	var invokeOpts []model_clients.InvokeOption
	if rf, ok := params["response_format"].(map[string]any); ok && rf != nil {
		invokeOpts = append(invokeOpts, model_clients.WithResponseFormat(rf))
	}

	// 重试逻辑（对齐 Python: should_raise_error）
	maxRetries := gm.Config.RequestMaxRetries
	if maxRetries <= 0 {
		maxRetries = 1
	}

	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		gm.Semaphore <- struct{}{} // 获取信号量
		response, err := gm.LLMClient.Invoke(ctx, messages, invokeOpts...)
		<-gm.Semaphore // 立即释放信号量（对齐 Python: sleep 期间不持有信号量）
		if err == nil {
			// 更新 token 记录（对齐 Python: self.token_record）
			// Python asyncio 单线程无需加锁，Go 并发调用时需加锁
			if response != nil && response.UsageMetadata != nil {
				gm.tokenRecordMu.Lock()
				gm.TokenRecord["input_tokens"] += response.UsageMetadata.InputTokens
				gm.TokenRecord["output_tokens"] += response.UsageMetadata.OutputTokens
				gm.tokenRecordMu.Unlock()
			}

			// 调试日志（对齐 Python: if self.debug）
			if gm.Debug {
				sep := "\n" + strings.Repeat("=", 60) + "\n"
				var queryStr string
				if msgs, ok := params["messages"]; ok {
					if msgList, ok := msgs.([]map[string]any); ok && len(msgList) > 0 {
						if c, ok := msgList[len(msgList)-1]["content"]; ok {
							queryStr = fmt.Sprintf("%v", c)
						}
					}
				}
				debugMsg := fmt.Sprintf("TEMPLATE %s%s%s%s", tmpl.Name, sep, queryStr, sep)
				if response != nil {
					content := response.GetContent()
					if content.IsText() {
						debugMsg += content.Text()
					}
				}
				logger.Info(logComponent).
					Str("event", "graph_memory_llm_invoke").
					Msg(debugMsg)
			}
			return response, nil
		}

		lastErr = err
		if attempt < maxRetries-1 {
			logger.Error(logComponent).Err(err).Msg("Graph Memory LLM Invoke Error")
			// 对齐 Python: await asyncio.sleep(random.random() / 2)
			// 使用 select + time.After 实现可取消的等待，避免持有信号量时无法响应 context 取消
			waitDuration := time.Duration(rand.Float64()*500) * time.Millisecond
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(waitDuration):
			}
		}
	}

	return nil, exception.BuildError(exception.StatusMemoryGraphInvokeLlmFailed,
		exception.WithCause(lastErr),
		exception.WithParam("error_msg", lastErr.Error()),
	)
}

// PrepareEpisodes 验证并规范化片段内容，检索相关历史
//
// Python: _prepare_episodes(src_type, user_id, content, state, content_fmt_kwargs)
func (gm *GraphMemory) PrepareEpisodes(ctx context.Context, state *GraphMemState, content string, messages []llmschema.BaseMessage, srcType config.EpisodeType, userID string, contentFmtKwargs map[string]string) (string, error) {
	return gm.prepareEpisodes(ctx, state, content, messages, srcType, userID, contentFmtKwargs)
}

// ExtractEntityDeclarations 运行 LLM 抽取实体声明
//
// Python: _extract_entity_declarations(src_type, content, state)
func (gm *GraphMemory) ExtractEntityDeclarations(ctx context.Context, srcType config.EpisodeType, content string, state *GraphMemState) (bool, []extraction.EntityDeclaration, error) {
	return gm.extractEntityDeclarations(ctx, srcType, content, state)
}

// FetchRelevantEntities 获取相关已有实体
//
// Python: _fetch_relevant_entities(extracted_declarations, no_existing_entity, user_id, state)
func (gm *GraphMemory) FetchRelevantEntities(ctx context.Context, extractedDeclarations []extraction.EntityDeclaration, noExistingEntity bool, userID string, state *GraphMemState) error {
	return gm.fetchRelevantEntities(ctx, extractedDeclarations, noExistingEntity, userID, state)
}

// EntityEnrich 抽取实体摘要和属性
//
// Python: _entity_enrich(entities, content, state)
func (gm *GraphMemory) EntityEnrich(ctx context.Context, entities []*graph.Entity, content string, state *GraphMemState) ([]*graph.Entity, error) {
	return gm.entityEnrich(ctx, entities, content, state)
}

// RelationDedupe 关系去重
//
// Python: _relation_dedupe(user_id, content, relations, relation_embed_results, state)
func (gm *GraphMemory) RelationDedupe(ctx context.Context, userID string, content string, relations []*graph.Relation, relationEmbedResults [][]float64, state *GraphMemState) error {
	return gm.relationDedupe(ctx, userID, content, relations, relationEmbedResults, state)
}

// ReplaceOneSideOfRelation 更新关系的一端（lhs 或 rhs）指向目标实体，
// 若同一关系中已存在该目标实体的引用则标记为有缺陷。
//
// Python: _replace_one_side_of_relation(side, relation, tgt_uuid, entity_relation_updates, state)
func ReplaceOneSideOfRelation(side string, relation *graph.Relation, tgtUUID string, entityRelationUpdates map[string]map[string]*graph.Relation, state *GraphMemState) {
	if _, ok := entityRelationUpdates[tgtUUID]; !ok {
		entityRelationUpdates[tgtUUID] = make(map[string]*graph.Relation)
	}
	if _, exists := entityRelationUpdates[tgtUUID][relation.UUID]; !exists {
		// 首次出现，添加延迟更新
		state.RelationDeferredUpdates[tgtUUID] = append(state.RelationDeferredUpdates[tgtUUID], deferredRelationUpdate{
			Relation: relation,
			Field:    side,
			Value:    tgtUUID,
		})
		entityRelationUpdates[tgtUUID][relation.UUID] = relation
	} else {
		// 重复出现，标记为有缺陷关系
		state.FaultyRelations[relation.UUID] = relation
		delete(entityRelationUpdates[tgtUUID], relation.UUID)
		// 从延迟更新中移除该关系的所有任务
		var filtered []deferredRelationUpdate
		for _, task := range state.RelationDeferredUpdates[tgtUUID] {
			if task.Relation != relation {
				filtered = append(filtered, task)
			}
		}
		state.RelationDeferredUpdates[tgtUUID] = filtered
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// newSearchConfigWithMinScore 创建带最低分数的 SearchConfig
func newSearchConfigWithMinScore(minScore float64) *config.SearchConfig {
	sc := config.NewSearchConfig()
	sc.MinScore = minScore
	return sc
}

// initState 创建 GraphMemState（对齐 Python _init_state）
func (gm *GraphMemory) initState(referenceTime *time.Time, srcType config.EpisodeType) *GraphMemState {
	strategy := gm.DefaultExtractionStrategy

	state := NewGraphMemState()
	state.Strategy = strategy
	state.EpisodeType = srcType
	state.EntityTypes = []extraction.EntityDef{
		*extraction.DefaultEntity,
		*extraction.HumanEntity,
		*extraction.AIEntity,
	}
	state.Prompting.Language = gm.Language
	state.Prompting.EntityExtractionLanguage = "cn"
	if !strategy.ChineseEntity {
		state.Prompting.EntityExtractionLanguage = gm.Language
	}
	state.Prompting.RelationExtractionLanguage = "cn"
	if !strategy.ChineseRelation {
		state.Prompting.RelationExtractionLanguage = gm.Language
	}
	state.Prompting.EntityDedupeLanguage = "cn"
	if !strategy.ChineseEntityDedupe {
		state.Prompting.EntityDedupeLanguage = gm.Language
	}
	state.Prompting.SchemaEntityExtraction = extraction.BuildResponseFormat(extraction.EntitySummary{}, gm.Language)
	state.Prompting.SchemaEntityDedupe = extraction.BuildResponseFormat(extraction.EntityDuplication{}, state.Prompting.EntityDedupeLanguage)
	state.Prompting.SchemaRelationMerge = extraction.BuildResponseFormat(extraction.MergeRelations{}, gm.Language)
	state.Prompting.SchemaRelationFilter = extraction.BuildResponseFormat(extraction.RelevantFacts{}, gm.Language)
	state.Extras = map[string]any{"summary_target": fmt.Sprintf("%d", strategy.SummaryTarget)}

	if referenceTime == nil {
		state.ReferenceTimestamp = state.CurrentTimestamp
	} else {
		state.ReferenceTimestamp = referenceTime.Unix()
	}

	return state
}

// prepareEpisodes 验证并规范化片段内容，检索相关历史
//
// Python: _prepare_episodes(src_type, user_id, content, state, content_fmt_kwargs)
func (gm *GraphMemory) prepareEpisodes(ctx context.Context, state *GraphMemState, content string, messages []llmschema.BaseMessage, srcType config.EpisodeType, userID string, contentFmtKwargs map[string]string) (string, error) {
	// 校验输入（对齐 Python: validate_add_memory_input(...)，校验失败直接返回 error）
	if err := ValidateAddMemoryInput(gm.Config.StorageConfig.UserID, srcType, userID, contentFmtKwargs); err != nil {
		return "", err
	}

	// 二选一校验（对齐 Python: isinstance(content, str) vs list[BaseMessage | dict]）
	if content == "" && len(messages) == 0 {
		return "", exception.BuildError(exception.StatusMemoryStoreValidationInvalid,
			exception.WithParam("store_type", storeType),
			exception.WithParam("error_msg", "content 和 messages 不能同时为空"),
		)
	}
	if content != "" && len(messages) > 0 {
		return "", exception.BuildError(exception.StatusMemoryStoreValidationInvalid,
			exception.WithParam("store_type", storeType),
			exception.WithParam("error_msg", "content 和 messages 互斥，请仅提供其中一个"),
		)
	}

	// content 为字符串时 content_fmt_kwargs 无效（对齐 Python: if content_fmt_kwargs: raise error）
	if content != "" && len(contentFmtKwargs) > 0 {
		return "", exception.BuildError(exception.StatusMemoryStoreValidationInvalid,
			exception.WithParam("store_type", storeType),
			exception.WithParam("error_msg", "content 为字符串时 content_fmt_kwargs 无效，请留空"),
		)
	}

	// 消息列表路径（对齐 Python: content is list[BaseMessage | dict]）
	if len(messages) > 0 {
		if srcType != config.EpisodeTypeConversation {
			return "", exception.BuildError(exception.StatusMemoryStoreValidationInvalid,
				exception.WithParam("store_type", storeType),
				exception.WithParam("error_msg", "messages 输入需要 src_type=CONVERSATION"),
			)
		}
		dicts, err := Msg2Dict(messages, false)
		if err != nil {
			return "", exception.BuildError(exception.StatusMemoryStoreValidationInvalid,
				exception.WithParam("store_type", storeType),
				exception.WithParam("error_msg", "content 必须是字符串或 dict/BaseMessage 格式的消息列表"),
				exception.WithCause(err),
			)
		}
		// 校验每条消息有 role + content（对齐 Python: if not all((isinstance(msg, dict) and "role" in msg and "content" in msg) for msg in content)）
		for _, msg := range dicts {
			if _, ok := msg["role"]; !ok {
				return "", exception.BuildError(exception.StatusMemoryStoreValidationInvalid,
					exception.WithParam("store_type", storeType),
					exception.WithParam("error_msg", `content 不是包含 "role" 和 "content" 键的字典列表`),
				)
			}
			if _, ok := msg["content"]; !ok {
				return "", exception.BuildError(exception.StatusMemoryStoreValidationInvalid,
					exception.WithParam("store_type", storeType),
					exception.WithParam("error_msg", `content 不是包含 "role" 和 "content" 键的字典列表`),
				)
			}
		}
		// 格式化消息列表（对齐 Python: content = format_list_of_messages(content, role_replace=content_fmt_kwargs)）
		content = graph.FormatListOfMessages(dicts, contentFmtKwargs, "")
	}

	// 内容为空检查
	content = strings.TrimSpace(content)
	if content == "" {
		return "", exception.BuildError(exception.StatusMemoryStoreValidationInvalid,
			exception.WithParam("store_type", storeType),
			exception.WithParam("error_msg", "content 必须是非空的字符串或消息列表"),
		)
	}

	// 检索相关历史
	var result []*graph.Episode
	recallStrategy := state.Strategy.RecallEpisode
	maximize := gm.metricIsSim || recallStrategy.RankConfig.HigherIsBetter()

	if recallStrategy.TopK > 0 {
		isEmpty, err := gm.DBBackend.IsEmpty(ctx, graph.EpisodeCollection)
		if err != nil {
			logger.Warn(logComponent).Err(err).Str("collection", graph.EpisodeCollection).Msg("Graph Memory: IsEmpty check failed, skipping episode search")
		}
		if err == nil && !isEmpty {
			// 组装搜索查询
			var queryComponents []query.QueryExpr
			queryComponents = append(queryComponents, query.FilterUser(userID, "user_id"))
			if recallStrategy.SameKind {
				queryComponents = append(queryComponents, &query.ComparisonExpr{
					Field:    "obj_type",
					Operator: "==",
					Value:    srcType.String(),
				})
			}
			if recallStrategy.ExcludeFutureResults {
				queryComponents = append(queryComponents, &query.ComparisonExpr{
					Field:    "valid_since",
					Operator: "<=",
					Value:    state.ReferenceTimestamp,
				})
			}
			epSearchQuery := query.ChainFilters(queryComponents)

			// 检索片段
			searchResult, err := gm.DBBackend.Search(ctx, content,
				graph.WithCollection(graph.EpisodeCollection),
				graph.WithK(recallStrategy.TopK),
				graph.WithRankerConfig(recallStrategy.RankConfig),
				graph.WithFilterExpr(epSearchQuery),
				graph.WithLanguage(state.Prompting.Language),
			)
			if err == nil {
				if epList, ok := searchResult[graph.EpisodeCollection]; ok {
					var filtered []map[string]any
					for _, r := range epList {
						distance, _ := r["distance"].(float64)
						if maximize {
							if distance >= recallStrategy.MinScore {
								filtered = append(filtered, r)
							}
						} else {
							if distance <= recallStrategy.MinScore {
								filtered = append(filtered, r)
							}
						}
					}
					for _, ep := range filtered {
						result = append(result, state.LookupTable.GetEpisode(ep))
					}
				}
			}
		}
	}

	// 按 valid_since 排序
	sortEpisodesByValidSince(result)

	// 设置 history
	var historyLines []string
	for _, ep := range result {
		historyLines = append(historyLines, graph.FormatTimestampISO(ep.CreatedAt, nil)+"\n"+ep.Content)
	}
	if len(historyLines) > 0 {
		state.History = strings.Join(historyLines, "\n---\n")
	}

	return content, nil
}

// extractEntityDeclarations 运行 LLM 抽取实体声明
//
// Python: _extract_entity_declarations(src_type, content, state)
func (gm *GraphMemory) extractEntityDeclarations(ctx context.Context, srcType config.EpisodeType, content string, state *GraphMemState) (bool, []extraction.EntityDeclaration, error) {
	entityTypesPtr := make([]*registry.EntityDef, len(state.EntityTypes))
	for i := range state.EntityTypes {
		et := state.EntityTypes[i]
		entityTypesPtr[i] = &et
	}

	kwargs, tmpl, outputModel := extraction.ExtractEntityDeclaration(
		srcType, content, state.History, "", entityTypesPtr,
		state.Prompting.EntityExtractionLanguage, state.Extras, 2,
	)

	response, err := gm.InvokeLLM(ctx, kwargs, tmpl, outputModel)
	if err != nil {
		return false, nil, err
	}

	extractedRaw := extraction.ParseJSON(assistantContent(response), outputModel)
	if extractedRaw == nil {
		extractedRaw = []any{}
	}

	// 处理嵌套 dict
	switch v := extractedRaw.(type) {
	case map[string]any:
		if len(v) == 1 {
			for _, inner := range v {
				if list, ok := inner.([]any); ok {
					extractedRaw = list
				} else if innerMap, ok := inner.(map[string]any); ok {
					extractedRaw = []any{innerMap}
				}
			}
		}
	}

	// 解析实体列表
	var extractedList []map[string]any
	if list, ok := extractedRaw.([]any); ok {
		entityNames := map[string]struct{}{}
		for _, item := range list {
			entMap, ok := item.(map[string]any)
			if !ok {
				continue
			}
			name, _ := entMap["name"].(string)
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			// 对齐 Python: extraction.pop("name", "")
			delete(entMap, "name")
			// 对齐 Python: casefold() 大小写不敏感过滤系统实体名
			lowerName := strings.ToLower(name)
			if lowerName == "user" || lowerName == "assistant" {
				continue
			}
			entityNames[name] = struct{}{}

			// 获取 type_id（对齐 Python: next(v for k, v in extraction.items() if "type" in k.casefold())）
			var typeID int
			for k, v := range entMap {
				if strings.Contains(strings.ToLower(k), "type") {
					if id, ok := toInt(v); ok {
						typeID = id
						break
					}
				}
			}
			extractedList = append(extractedList, map[string]any{
				"name":           name,
				"entity_type_id": typeID,
			})
		}
	}

	// 转为 EntityDeclaration
	var declarations []extraction.EntityDeclaration
	for _, ent := range extractedList {
		name, _ := ent["name"].(string)
		if strings.TrimSpace(name) == "" {
			continue
		}
		typeID, _ := toInt(ent["entity_type_id"])
		declarations = append(declarations, extraction.EntityDeclaration{
			Name:         name,
			EntityTypeID: typeID,
		})
	}

	// 判断是否没有已有实体
	noExistingEntity := false
	isEmpty, err := gm.DBBackend.IsEmpty(ctx, graph.EntityCollection)
	if err != nil {
		logger.Warn(logComponent).Err(err).Str("collection", graph.EntityCollection).Msg("Graph Memory: IsEmpty check failed, assuming entity collection non-empty")
	}
	if err == nil {
		noExistingEntity = isEmpty
	}

	// 异步嵌入实体名称（对齐 Python: state.tasks.append(asyncio.create_task(self.embedder.embed_documents(...)))）
	// 与关系抽取 LLM 调用并行执行
	if !noExistingEntity && len(declarations) > 0 {
		names := make([]string, len(declarations))
		for i, ent := range declarations {
			names[i] = ent.Name
		}
		embedCh := make(embedTask, 1)
		embedder := gm.Embedder()
		go func() {
			if embedder == nil {
				embedCh <- embedResult{Err: fmt.Errorf("embedder not available")}
				return
			}
			embeddings, embedErr := embedder.EmbedDocuments(ctx, names, embedding.WithBatchSize(gm.Config.EmbedBatchSize))
			embedCh <- embedResult{Embeddings: embeddings, Err: embedErr}
		}()
		state.EmbedTask = embedCh
	}

	return noExistingEntity, declarations, nil
}

// fetchRelevantEntities 获取相关已有实体
//
// Python: _fetch_relevant_entities(extracted_declarations, no_existing_entity, user_id, state)
func (gm *GraphMemory) fetchRelevantEntities(ctx context.Context, extractedDeclarations []extraction.EntityDeclaration, noExistingEntity bool, userID string, state *GraphMemState) error {
	if noExistingEntity {
		return nil
	}

	// 对齐 Python: if len(state.tasks) <= 1: return
	// Python 中 tasks <= 1 时意味着只有关系抽取任务，无嵌入任务可等待
	if len(state.Tasks) <= 1 {
		return nil
	}

	// 等待嵌入任务完成（对齐 Python: entity_embed_results = await state.tasks.pop(-2)）
	// Python 中嵌入任务在 _extract_entity_declarations 中通过 asyncio.create_task 发起
	var entityEmbedResults [][]float64
	if state.EmbedTask != nil {
		embedResults, embedErr := state.EmbedTask.Wait()
		state.EmbedTask = nil
		if embedErr != nil {
			logger.Warn(logComponent).Err(embedErr).Msg("Graph Memory: entity name embedding failed")
			return nil
		}
		entityEmbedResults = embedResults
	} else {
		// 降级：嵌入任务未启动时同步嵌入
		names := make([]string, len(extractedDeclarations))
		for i, ent := range extractedDeclarations {
			names[i] = ent.Name
		}
		if len(names) == 0 {
			return nil
		}
		embedder := gm.Embedder()
		if embedder == nil {
			return nil
		}
		var err error
		entityEmbedResults, err = embedder.EmbedDocuments(ctx, names, embedding.WithBatchSize(gm.Config.EmbedBatchSize))
		if err != nil {
			logger.Warn(logComponent).Err(err).Msg("Graph Memory: entity name embedding failed")
			return nil
		}
	}

	for i, ent := range extractedDeclarations {
		if i >= len(entityEmbedResults) {
			break
		}
		emb := entityEmbedResults[i]

		// 获取实体类型
		var entityType *registry.EntityDef
		if ent.EntityTypeID < len(state.EntityTypes) {
			et := state.EntityTypes[ent.EntityTypeID]
			entityType = &et
		}

		filterByUser := query.FilterUser(userID, "user_id")
		recallEntity := state.Strategy.RecallEntity
		maximize := gm.metricIsSim || recallEntity.RankConfig.HigherIsBetter()

		// 如果不限制同类型，执行跨类型搜索
		if !recallEntity.SameKind {
			searchResult, err := gm.DBBackend.Search(ctx, ent.Name,
				graph.WithCollection(graph.EntityCollection),
				graph.WithK(recallEntity.TopK),
				graph.WithRankerConfig(recallEntity.RankConfig),
				graph.WithFilterExpr(filterByUser),
				graph.WithQueryEmbedding(emb),
				graph.WithLanguage(state.Prompting.Language),
			)
			if err == nil {
				if entityList, ok := searchResult[graph.EntityCollection]; ok {
					for _, r := range entityList {
						distance, _ := r["distance"].(float64)
						if maximize && distance >= recallEntity.MinScore || !maximize && distance <= recallEntity.MinScore {
							state.RetrievedEntities[r["uuid"].(string)] = state.LookupTable.GetEntity(r)
						}
					}
				}
			}
		}

		// 按类型搜索
		if entityType != nil {
			typedFilter := &query.LogicalExpr{
				Operator: "and",
				Left:     filterByUser,
				Right: &query.MatchExpr{
					Field:     "obj_type",
					Value:     entityType.Name,
					MatchMode: query.MatchModeExact,
				},
			}
			searchResult, err := gm.DBBackend.Search(ctx, ent.Name,
				graph.WithCollection(graph.EntityCollection),
				graph.WithK(recallEntity.TopK),
				graph.WithRankerConfig(recallEntity.RankConfig),
				graph.WithFilterExpr(typedFilter),
				graph.WithQueryEmbedding(emb),
				graph.WithLanguage(state.Prompting.Language),
			)
			if err == nil {
				if entityList, ok := searchResult[graph.EntityCollection]; ok {
					for _, r := range entityList {
						distance, _ := r["distance"].(float64)
						if maximize && distance >= recallEntity.MinScore || !maximize && distance <= recallEntity.MinScore {
							state.RetrievedEntities[r["uuid"].(string)] = state.LookupTable.GetEntity(r)
						}
					}
				}
			}
		}

		// 精确/包含匹配
		topKHalf := int(math.Ceil(float64(recallEntity.TopK) / 2))
		exactMatch := &query.LogicalExpr{
			Operator: "and",
			Left:     filterByUser,
			Right:    &query.MatchExpr{Field: "name", Value: ent.Name, MatchMode: query.MatchModeExact},
		}
		infixMatch := &query.LogicalExpr{
			Operator: "and",
			Left:     filterByUser,
			Right:    &query.MatchExpr{Field: "name", Value: ent.Name, MatchMode: query.MatchModeInfix},
		}

		exactResult, _ := gm.DBBackend.Query(ctx, graph.EntityCollection,
			graph.WithExpr(exactMatch),
			graph.WithK(topKHalf),
			graph.WithSilenceErrors(true),
		)
		infixResult, _ := gm.DBBackend.Query(ctx, graph.EntityCollection,
			graph.WithExpr(infixMatch),
			graph.WithK(topKHalf),
			graph.WithSilenceErrors(true),
		)

		for _, e := range append(exactResult, infixResult...) {
			if uuid, ok := e["uuid"].(string); ok {
				state.RetrievedEntities[uuid] = state.LookupTable.GetEntity(e)
			}
		}
	}

	return nil
}

// entityMerge 实体合并
//
// Python: _entity_merge(extracted_declarations, existing_entities_list, state)
// 返回混合列表 []EntityOrDeclaration（对齐 Python: list[Union[EntityDeclaration, Entity]]）
func (gm *GraphMemory) entityMerge(ctx context.Context, extractedDeclarations []extraction.EntityDeclaration, existingEntitiesList []map[string]any, state *GraphMemState) ([]EntityOrDeclaration, error) {
	if len(existingEntitiesList) == 0 {
		// 没有已有实体，直接返回纯声明列表
		result := make([]EntityOrDeclaration, len(extractedDeclarations))
		for i := range extractedDeclarations {
			d := extractedDeclarations[i]
			result[i] = EntityOrDeclaration{Decl: &d}
		}
		return result, nil
	}

	// 等待所有 tasks 完成（对齐 Python: if state.tasks: await asyncio.wait(state.tasks)）
	// asyncTask.Wait() 使用 sync.Once 缓存结果，可安全多次调用
	for _, task := range state.Tasks {
		if task != nil {
			_, _ = task.Wait()
		}
	}

	// 获取实体去重 LLM 结果（对齐 Python: response_resolve_entity = await state.tasks.pop()）
	var dedupeEntityRaw []any
	if len(state.Tasks) > 0 {
		// 取最后一个 task 的结果（即实体去重任务，对齐 Python: state.tasks.pop()）
		dedupeTask := state.Tasks[len(state.Tasks)-1]
		dedupeContent, dedupeErr := dedupeTask.Wait()
		state.Tasks = state.Tasks[:len(state.Tasks)-1]
		if dedupeErr != nil {
			logger.Warn(logComponent).Err(dedupeErr).Msg("Graph Memory: entity dedup LLM failed")
		} else {
			dedupeResult := extraction.ParseJSON(dedupeContent, state.Prompting.SchemaEntityDedupe)
			if dedupeResult != nil {
				dedupeEntityRaw = extraction.EnsureList(dedupeResult)
			}
		}
	}

	// 构建已有实体列表
	var existingEntities []*graph.Entity
	for _, entityDict := range existingEntitiesList {
		existingEntities = append(existingEntities, state.LookupTable.GetEntity(entityDict))
	}

	// 解析去重结果
	dedupeList := anyListToMapList(dedupeEntityRaw)

	// 解析实体合并（对齐 Python: resolve_entities）
	resolved, mergePairs, entityUUIDsToRemove := ResolveEntities(extractedDeclarations, existingEntities, dedupeList)

	if !state.Strategy.MergeEntities {
		mergePairs = nil
	} else {
		for uuid := range entityUUIDsToRemove {
			state.MemUpdate.RemovedEntity[uuid] = struct{}{}
		}
	}

	// 对齐 Python: resolve_entities 返回混合列表 list[Union[EntityDeclaration, Entity]]
	// resolved 直接传递给 ParseAllRelations，不再将已有实体转回 EntityDeclaration
	// （Python: extracted_declarations = resolved，保留 Entity 对象作为 lhs/rhs 引用）

	// 处理合并任务（对齐 Python: _entity_merge 的 blocking/non-blocking 分派）
	if len(mergePairs) > 0 {
		var blockingTasks []MergePair    // 目标实体在 extractedDeclarations 中（阻塞实体摘要抽取）
		var nonBlockingTasks []MergePair // 目标实体不在 extractedDeclarations 中

		// 对齐 Python: if tgt in extracted_declarations
		// Python 中 extracted_declarations 在 resolve_entities 后是混合类型列表
		// 通过名字匹配判断目标实体是否在 resolved 列表中
		resolvedNameSet := make(map[string]struct{})
		for _, item := range resolved {
			resolvedNameSet[item.Name()] = struct{}{}
		}

		for _, pair := range mergePairs {
			if _, inResult := resolvedNameSet[pair.Target.Name]; inResult {
				blockingTasks = append(blockingTasks, pair)
			} else {
				nonBlockingTasks = append(nonBlockingTasks, pair)
			}
		}

		// 先处理阻塞任务（对齐 Python: task = asyncio.create_task(self._invoke_llm(...))）
		// Go: 用 invokeLLMAsync 并发启动，entityEnrich 中等待完成
		for _, pair := range blockingTasks {
			kwargs, tmpl, outputModel := extraction.MergeExistingEntities(
				pair.Target, pair.Sources, state.Prompting.Language, state.Extras, 2,
			)
			task := gm.invokeLLMAsync(ctx, kwargs, tmpl, outputModel)
			// 对齐 Python: state.pending_merge[tgt.uuid] = task
			pending := &pendingMergeTask{
				SourceTask: task,
				done:       make(chan struct{}),
			}
			state.PendingMerge[pair.Target.UUID] = pending
			// 对齐 Python: await task — 等待合并完成
			go func(t *asyncTask, p *pendingMergeTask) {
				content, err := t.Wait()
				p.Result = content
				p.Err = err
				close(p.done)
			}(task, pending)
			// 对齐 Python: state.merging_tasks.append(task); state.merging_tasks_entities[task] = tgt
			state.MergingTasks = append(state.MergingTasks, task)
			state.MergingTasksEntities[task] = pair.Target
		}

		// 再处理非阻塞任务（对齐 Python: for tgt, prompt_entity_merge in non_blocking_tasks: asyncio.create_task(...)）
		for _, pair := range nonBlockingTasks {
			kwargs, tmpl, outputModel := extraction.MergeExistingEntities(
				pair.Target, pair.Sources, state.Prompting.Language, state.Extras, 2,
			)
			// 对齐 Python: task = asyncio.create_task(self._invoke_llm(*prompt_entity_merge))
			// 非阻塞任务并发发起，与阻塞任务并行执行
			mergeTask := gm.invokeLLMAsync(ctx, kwargs, tmpl, outputModel)
			state.MergingTasks = append(state.MergingTasks, mergeTask)
			state.MergingTasksEntities[mergeTask] = pair.Target
		}

		// 解析合并实体后的关系和 Episode 引用（对齐 Python: await self._resolve_entity_merges(merging_args, state)）
		if err := gm.resolveEntityMerges(ctx, mergePairs, state); err != nil {
			logger.Warn(logComponent).Err(err).Msg("Graph Memory: resolveEntityMerges failed")
		}
	}

	return resolved, nil
}

// entityEnrich 抽取实体摘要和属性
//
// Python: _entity_enrich(entities, content, state)
func (gm *GraphMemory) entityEnrich(ctx context.Context, entities []*graph.Entity, content string, state *GraphMemState) ([]*graph.Entity, error) {
	state.Tasks = state.Tasks[:0]

	// 分类阻塞/非阻塞实体（对齐 Python: entities_blocking / entities_non_blocking）
	var entitiesBlocking []*graph.Entity
	var entitiesNonBlocking []*graph.Entity

	for _, entity := range entities {
		if _, isPending := state.PendingMerge[entity.UUID]; isPending {
			entitiesBlocking = append(entitiesBlocking, entity)
		} else {
			entitiesNonBlocking = append(entitiesNonBlocking, entity)
		}
	}

	// 合并列表（对齐 Python: entities = entities_non_blocking + entities_blocking）
	entities = append(entitiesNonBlocking, entitiesBlocking...)

	// 先启动非阻塞任务（对齐 Python: for entity in entities_non_blocking: state.tasks.append(...)）
	for _, entity := range entitiesNonBlocking {
		kwargs, tmpl, outputModel := extraction.ExtractEntityAttributes(
			entity, content, state.History, state.Prompting.Language, state.Extras, 2,
		)
		task := gm.invokeLLMAsync(ctx, kwargs, tmpl, outputModel)
		state.Tasks = append(state.Tasks, task)
	}

	// 阻塞任务需要先等 pending merge 完成（对齐 Python: response = await task; update_entity(...)）
	for _, entity := range entitiesBlocking {
		pendingTask := state.PendingMerge[entity.UUID]
		if pendingTask != nil {
			// 对齐 Python: response = await task — 等待合并完成
			<-pendingTask.done
			if pendingTask.Err == nil && pendingTask.Result != "" {
				UpdateEntity(entity, pendingTask.Result, state.Prompting.SchemaEntityExtraction)
			} else if pendingTask.Err != nil {
				logger.Warn(logComponent).Err(pendingTask.Err).Str("entity", entity.Name).Msg("Graph Memory: failed waiting for entity merge completion")
			}
			// 对齐 Python: state.merging_tasks.remove(task) — 从合并任务列表中移除已完成的任务
			if pendingTask.SourceTask != nil {
				for j, mt := range state.MergingTasks {
					if mt == pendingTask.SourceTask {
						state.MergingTasks = append(state.MergingTasks[:j], state.MergingTasks[j+1:]...)
						delete(state.MergingTasksEntities, mt)
						break
					}
				}
			}
			delete(state.PendingMerge, entity.UUID)
		}
		// 然后发起摘要/属性抽取
		kwargs, tmpl, outputModel := extraction.ExtractEntityAttributes(
			entity, content, state.History, state.Prompting.Language, state.Extras, 2,
		)
		task := gm.invokeLLMAsync(ctx, kwargs, tmpl, outputModel)
		state.Tasks = append(state.Tasks, task)
	}

	// 等待所有任务完成（对齐 Python: if state.tasks: await asyncio.wait(state.tasks)）
	// asyncTask.Wait() 使用 sync.Once 缓存结果，可安全多次调用
	for _, task := range state.Tasks {
		if _, err := task.Wait(); err != nil {
			logger.Warn(logComponent).Err(err).Msg("Graph Memory: entity summary extraction failed")
		}
	}

	// 更新实体（对齐 Python: for entity, future in zip(entities, state.tasks)）
	n := len(entities)
	if len(state.Tasks) < n {
		n = len(state.Tasks)
	}
	for i := 0; i < n; i++ {
		entity := entities[i]
		content, err := state.Tasks[i].Wait()
		if err != nil || content == "" {
			continue
		}
		UpdateEntity(entity, content, state.Prompting.SchemaEntityExtraction)
	}

	state.Tasks = state.Tasks[:0]
	return entities, nil
}

// parseRelationFilteringResult 解析关系过滤结果
//
// Python: _parse_relation_filtering_result(relations, state)
func (gm *GraphMemory) parseRelationFilteringResult(ctx context.Context, relations []*graph.Relation, state *GraphMemState) error {
	// 处理关系过滤任务（对齐 Python: if state.relation_filter_tasks: await asyncio.wait(...)）
	for task, taskItem := range state.RelationFilterTasks {
		taskContent, taskErr := task.Wait()
		tgtEntity := taskItem.TargetEntity
		relationList := taskItem.Relations
		tgtUUID := tgtEntity.UUID

		if taskErr != nil {
			// 异常时保留全部关系（对齐 Python: except Exception: relations_filtered = new_relation_list）
			state.MergeInfos[tgtUUID].NewRelations = relationList
			continue
		}

		dedupeEntity := extraction.ParseJSON(taskContent, state.Prompting.SchemaRelationFilter)
		if dedupeMap, ok := dedupeEntity.(map[string]any); ok {
			keepIDs := dedupeMap["relevant_relations"]
			if idList, ok := keepIDs.([]any); ok {
				var relationsFiltered []*graph.Relation
				for _, idVal := range idList {
					// 对齐 Python: [new_relation_list[i - 1] for i in keep_ids]
					// Python 不做边界检查，Go 保留 id >= 1 防止负索引 panic
					if id, ok := toInt(idVal); ok && id >= 1 && id <= len(relationList) {
						relationsFiltered = append(relationsFiltered, relationList[id-1])
					}
				}
				state.MergeInfos[tgtUUID].NewRelations = relationsFiltered
			} else {
				state.MergeInfos[tgtUUID].NewRelations = relationList
			}
		} else {
			state.MergeInfos[tgtUUID].NewRelations = relationList
		}
	}

	// 处理延迟更新（对齐 Python: for tgt_uuid, merge_info in state.merge_infos.items()）
	for tgtUUID, mergeInfo := range state.MergeInfos {
		for _, deferred := range state.RelationDeferredUpdates[tgtUUID] {
			relation := deferred.Relation
			field := deferred.Field
			value := deferred.Value

			// 检查关系是否在新关系列表中
			inNewRelations := false
			for _, r := range mergeInfo.NewRelations {
				if r == relation {
					inNewRelations = true
					break
				}
			}

			if inNewRelations {
				// 更新关系端点（对齐 Python: setattr(relation, field, tgt_uuid)）
				switch field {
				case "lhs":
					// 优先从 LookupTable 获取完整 Entity，找不到时回退到空壳构造
					if e, ok := state.LookupTable.Entities[value]; ok {
						relation.LHS = e
					} else {
						relation.LHS = &graph.Entity{NamedGraphObject: graph.NamedGraphObject{BaseGraphObject: graph.BaseGraphObject{UUID: value}}}
					}
				case "rhs":
					// 优先从 LookupTable 获取完整 Entity，找不到时回退到空壳构造
					if e, ok := state.LookupTable.Entities[value]; ok {
						relation.RHS = e
					} else {
						relation.RHS = &graph.Entity{NamedGraphObject: graph.NamedGraphObject{BaseGraphObject: graph.BaseGraphObject{UUID: value}}}
					}
				}
				if !containsRelationPtr(state.MemUpdateSkipEmbed.UpdatedRelation, relation) {
					state.MemUpdateSkipEmbed.UpdatedRelation = append(state.MemUpdateSkipEmbed.UpdatedRelation, relation)
				}
			} else {
				// 关系不在保留列表中，标记为待删除
				state.MemUpdate.RemovedRelation[relation.UUID] = struct{}{}
				state.ToRemove[relation.UUID] = relation
			}
		}
	}

	ClassifyRelationsExtracted(relations, state)
	return nil
}

// handleRelationDedupe 关系去重处理
//
// Python: _handle_relation_dedupe(user_id, content, relations, state)
func (gm *GraphMemory) handleRelationDedupe(ctx context.Context, userID string, content string, relations []*graph.Relation, state *GraphMemState) ([]*graph.Relation, error) {
	// 移除待删除的关系（对齐 Python: for relation in state.to_remove: ...）
	// 使用 filter 方式构建新切片，避免遍历中修改切片导致索引偏移
	removeSet := make(map[string]struct{}, len(state.ToRemove))
	for uuid := range state.ToRemove {
		removeSet[uuid] = struct{}{}
	}
	filtered := make([]*graph.Relation, 0, len(relations))
	for _, rel := range relations {
		if _, ok := removeSet[rel.UUID]; !ok {
			filtered = append(filtered, rel)
		}
	}
	relations = filtered

	// 批量嵌入 + 去重
	if state.Strategy.MergeRelations && len(state.TmpBuffer) > 0 {
		isEmpty, err := gm.DBBackend.IsEmpty(ctx, graph.RelationCollection)
		if err != nil || isEmpty {
			return relations, nil
		}

		// 提取文本用于嵌入
		texts := make([]string, 0, len(state.TmpBuffer))
		for _, item := range state.TmpBuffer {
			if s, ok := item.(string); ok {
				texts = append(texts, s)
			}
		}
		if len(texts) == 0 {
			return relations, nil
		}

		embedder := gm.Embedder()
		if embedder == nil {
			return relations, nil
		}

		results, err := embedder.EmbedDocuments(ctx, texts, embedding.WithBatchSize(gm.Config.EmbedBatchSize))
		if err != nil {
			logger.Warn(logComponent).Err(err).Msg("Graph Memory: relation content embedding failed")
			return relations, nil
		}

		err = gm.relationDedupe(ctx, userID, content, relations, results, state)
		if err != nil {
			return relations, err
		}
		return relations, nil
	}
	return relations, nil
}

// relationDedupe 关系去重
//
// Python: _relation_dedupe(user_id, content, relations, relation_embed_results, state)
func (gm *GraphMemory) relationDedupe(ctx context.Context, userID string, content string, relations []*graph.Relation, relationEmbedResults [][]float64, state *GraphMemState) error {
	var dedupeRelationTasks []DedupeRelationTask

	for i, newRelation := range relations {
		if i >= len(relationEmbedResults) {
			break
		}
		emb := relationEmbedResults[i]

		// 检查 lhs/rhs 有效性
		lhs := newRelation.LHSUUID()
		rhs := newRelation.RHSUUID()
		if lhs == "" || rhs == "" {
			continue
		}

		// 搜索相似关系
		lhsRhs := []any{lhs, rhs}
		filterExpr := &query.LogicalExpr{
			Operator: "and",
			Left: &query.LogicalExpr{
				Operator: "and",
				Left:     query.InList("lhs", lhsRhs),
				Right:    query.InList("rhs", lhsRhs),
			},
			Right: query.FilterUser(userID, "user_id"),
		}

		recallRelation := state.Strategy.RecallRelation
		maximize := gm.metricIsSim || recallRelation.RankConfig.HigherIsBetter()

		var currentRelations []*graph.Relation
		searchResult, err := gm.DBBackend.Search(ctx, newRelation.Content,
			graph.WithCollection(graph.RelationCollection),
			graph.WithK(recallRelation.TopK),
			graph.WithRankerConfig(recallRelation.RankConfig),
			graph.WithFilterExpr(filterExpr),
			graph.WithQueryEmbedding(emb),
			graph.WithLanguage(state.Prompting.Language),
		)
		if err == nil {
			if relList, ok := searchResult[graph.RelationCollection]; ok {
				for _, r := range relList {
					distance, _ := r["distance"].(float64)
					if maximize && distance >= recallRelation.MinScore || !maximize && distance <= recallRelation.MinScore {
						retrievedRel := state.LookupTable.GetRelation(r)
						state.RetrievedRelations[retrievedRel.UUID] = retrievedRel
						currentRelations = append(currentRelations, retrievedRel)
					}
				}
			}
		}

		if len(currentRelations) > 0 {
			// 构建去重提示词 — 对齐 Python _endpoint_to_entity_dict：
			// 如果 LHS/RHS 已是完整 Entity（Content 非空），直接使用；否则从 LookupTable 查找
			var existingEntities []*graph.Entity
			if newRelation.LHS != nil && strings.TrimSpace(newRelation.LHS.Content) != "" {
				existingEntities = append(existingEntities, newRelation.LHS)
			} else if e, ok := state.LookupTable.Entities[lhs]; ok {
				existingEntities = append(existingEntities, e)
			}
			if newRelation.RHS != nil && strings.TrimSpace(newRelation.RHS.Content) != "" {
				existingEntities = append(existingEntities, newRelation.RHS)
			} else if e, ok := state.LookupTable.Entities[rhs]; ok {
				existingEntities = append(existingEntities, e)
			}

			existingRelMaps := make([]map[string]any, len(currentRelations))
			for j, r := range currentRelations {
				existingRelMaps[j] = r.ToMap()
			}

			kwargs, tmpl, outputModel := extraction.DedupeRelationList(
				content, newRelation, existingRelMaps, existingEntities,
				state.History, "", state.Prompting.Language, 2,
			)

			response, err := gm.InvokeLLM(ctx, kwargs, tmpl, outputModel)
			if err != nil {
				logger.Warn(logComponent).Err(err).Str("relation", newRelation.Name).Msg("Graph Memory: relation dedup LLM failed")
				continue
			}

			dedupeRelationTasks = append(dedupeRelationTasks, DedupeRelationTask{
				Relation:          newRelation,
				ExistingRelations: existingRelMaps,
				Response:          assistantContent(response),
			})
		}
	}

	ParseRelationUUIDsToRemove(dedupeRelationTasks, state)
	return nil
}

// updateEntitiesForRelationRemoval 更新因关系删除而受影响的实体
//
// Python: _update_entities_for_relation_removal(state, extracted_declarations)
func (gm *GraphMemory) updateEntitiesForRelationRemoval(ctx context.Context, state *GraphMemState, updateNeedsEmbed []EntityOrDeclaration) {
	// 对齐 Python: for relation in state.to_remove
	// 从 relation.LHS.UUID 和 relation.RHS.UUID 取端点实体 UUID
	entitiesToRemoveRelationsFrom := make(map[string]struct{})
	for _, relation := range state.ToRemove {
		if relation.LHS != nil {
			entitiesToRemoveRelationsFrom[relation.LHS.UUID] = struct{}{}
		}
		if relation.RHS != nil {
			entitiesToRemoveRelationsFrom[relation.RHS.UUID] = struct{}{}
		}
	}

	if len(entitiesToRemoveRelationsFrom) == 0 {
		return
	}

	// 查询受影响的实体
	ids := make([]any, 0, len(entitiesToRemoveRelationsFrom))
	for id := range entitiesToRemoveRelationsFrom {
		ids = append(ids, id)
	}

	queryResult, err := gm.DBBackend.Query(ctx, graph.EntityCollection, graph.WithIDs(ids...))
	if err != nil {
		return
	}

	for _, e := range queryResult {
		entity := state.LookupTable.GetEntity(e)
		if existing, ok := state.LookupTable.Entities[entity.UUID]; ok {
			entity = existing
		}

		needsReEmbed := false
		for _, item := range updateNeedsEmbed {
			// 对齐 Python: isinstance(existing_entity_item, Entity) and existing_entity_item.uuid == entity.uuid
			if item.IsEntity() && item.Entity.UUID == entity.UUID {
				entity = item.Entity
				needsReEmbed = true
				break
			}
		}

		updateWithoutEmbed := false
		for relationUUID := range state.MemUpdate.RemovedRelation {
			if containsString(entity.Relations, relationUUID) {
				entity.Relations = removeString(entity.Relations, relationUUID)
				if !needsReEmbed {
					updateWithoutEmbed = true
				}
			}
		}

		if updateWithoutEmbed && !containsEntityPtr(state.MemUpdateSkipEmbed.UpdatedEntity, entity) {
			if _, removed := state.MemUpdate.RemovedEntity[entity.UUID]; !removed {
				state.MemUpdateSkipEmbed.UpdatedEntity = append(state.MemUpdateSkipEmbed.UpdatedEntity, entity)
			}
		}
	}
}

// startTimezoneTask 启动时区预测异步任务
func (gm *GraphMemory) startTimezoneTask(ctx context.Context, content string, state *GraphMemState) <-chan tzTaskResult {
	ch := make(chan tzTaskResult, 1)
	go func() {
		defer close(ch)
		kwargs, tmpl, outputModel := extraction.ExtractTimezone(
			content, state.History, "", state.Prompting.Language, 2,
		)
		resp, err := gm.InvokeLLM(ctx, kwargs, tmpl, outputModel)
		if err != nil {
			ch <- tzTaskResult{err: err}
			return
		}
		ch <- tzTaskResult{response: assistantContent(resp)}
	}()
	return ch
}

// startRelationExtractionAsync 启动关系抽取异步任务（返回 asyncTask，对齐 Python: state.tasks.append(...)）
//
// Python: state.tasks.append(asyncio.create_task(self._invoke_llm(*extract_relation_declaration(...))))
func (gm *GraphMemory) startRelationExtractionAsync(
	ctx context.Context,
	entities []extraction.EntityDeclaration,
	content string,
	state *GraphMemState,
	tzResponse string,
) *asyncTask {
	task := newAsyncTask()
	go func() {
		if gm.LLMClient == nil {
			task.Send("", fmt.Errorf("LLM client is not set"))
			return
		}
		tzInfo := extraction.ParseJSON(tzResponse,
			extraction.BuildResponseFormat(extraction.TimezonePredictions{}, state.Prompting.Language),
		)
		if tzInfo == nil {
			tzInfo = []any{}
		}

		entityTypesPtr := make([]*registry.EntityDef, len(state.EntityTypes))
		for i := range state.EntityTypes {
			et := state.EntityTypes[i]
			entityTypesPtr[i] = &et
		}

		kwargs, tmpl, outputModel := extraction.ExtractRelationDeclaration(
			nil, entities, state.ReferenceTimestamp, tzInfo,
			content, state.History, entityTypesPtr, "",
			state.Prompting.RelationExtractionLanguage, 2,
		)

		resp, err := gm.InvokeLLM(ctx, kwargs, tmpl, outputModel)
		task.Send(assistantContent(resp), err)
	}()
	return task
}

// startEntityDedupeAsync 启动实体去重异步任务（返回 asyncTask，对齐 Python: state.tasks.append(...)）
//
// Python: state.tasks.append(asyncio.create_task(self._invoke_llm(*dedupe_entity_list(...))))
func (gm *GraphMemory) startEntityDedupeAsync(
	ctx context.Context,
	content string,
	candidateEntities []extraction.EntityDeclaration,
	existingEntities []map[string]any,
	state *GraphMemState,
) *asyncTask {
	task := newAsyncTask()
	go func() {
		if gm.LLMClient == nil {
			task.Send("", fmt.Errorf("LLM client is not set"))
			return
		}
		entityTypesPtr := make([]*registry.EntityDef, len(state.EntityTypes))
		for i := range state.EntityTypes {
			et := state.EntityTypes[i]
			entityTypesPtr[i] = &et
		}

		kwargs, tmpl, outputModel := extraction.DedupeEntityList(
			content, candidateEntities, existingEntities, entityTypesPtr,
			state.History, "", state.Prompting.EntityDedupeLanguage, 2,
		)

		resp, err := gm.InvokeLLM(ctx, kwargs, tmpl, outputModel)
		task.Send(assistantContent(resp), err)
	}()
	return task
}

// invokeLLMAsync 异步调用 LLM（返回 asyncTask，对齐 Python: asyncio.create_task(self._invoke_llm(...))）
func (gm *GraphMemory) invokeLLMAsync(ctx context.Context, kwargs map[string]any, tmpl *prompt.PromptTemplate, outputModel map[string]any) *asyncTask {
	task := newAsyncTask()
	go func() {
		resp, err := gm.InvokeLLM(ctx, kwargs, tmpl, outputModel)
		task.Send(assistantContent(resp), err)
	}()
	return task
}

// entityListFromState 从 state.RetrievedEntities 构建 map 列表
func (gm *GraphMemory) entityListFromState(state *GraphMemState) []map[string]any {
	if len(state.RetrievedEntities) == 0 {
		return nil
	}
	result := make([]map[string]any, 0, len(state.RetrievedEntities))
	for _, entity := range state.RetrievedEntities {
		result = append(result, entity.ToMap())
	}
	return result
}

// performSearch 执行单集合搜索
func (gm *GraphMemory) performSearch(ctx context.Context, colIdx int, userIDs []string, strategyName string, q string, queryEmbedding []float64) ([]ScoredGraphObject, error) {
	collectionNames := []string{graph.EntityCollection, graph.RelationCollection, graph.EpisodeCollection}
	collection := collectionNames[colIdx]

	strategy := gm.searchStrategies[strategyName]
	var searchConfig *config.SearchConfig
	switch colIdx {
	case 0:
		searchConfig = strategy.EntityConfig
	case 1:
		searchConfig = strategy.RelationConfig
	case 2:
		searchConfig = strategy.EpisodeConfig
	}

	// 校验 reranker
	if searchConfig.Rerank && gm.Reranker == nil {
		return nil, exception.BuildError(exception.StatusMemoryStoreValidationInvalid,
			exception.WithParam("store_type", storeType),
			exception.WithParam("error_msg", fmt.Sprintf("搜索策略 [%s]（%s）启用了 rerank=True 但未设置 reranker，请使用 attach_reranker 方法挂载", strategyName, collection)),
		)
	}

	// 构建过滤表达式
	var filterExpr query.QueryExpr
	if len(userIDs) == 1 {
		filterExpr = query.FilterUser(userIDs[0], "user_id")
	} else {
		filterExpr = query.FilterUsers(userIDs, "user_id")
	}

	if searchConfig.FilterExpr != nil {
		filterExpr = &query.LogicalExpr{Operator: "and", Left: filterExpr, Right: searchConfig.FilterExpr}
	}

	// 执行搜索
	var searchOpts []graph.Option
	searchOpts = append(searchOpts,
		graph.WithCollection(collection),
		graph.WithK(searchConfig.TopK),
		graph.WithRankerConfig(searchConfig.RankConfig),
		graph.WithBFS(searchConfig.BFSDepth, searchConfig.BFSK),
		graph.WithFilterExpr(filterExpr),
		graph.WithLanguage(searchConfig.Language),
		graph.WithQueryEmbedding(queryEmbedding),
		graph.WithMinScore(searchConfig.MinScore),
	)
	if searchConfig.Rerank {
		searchOpts = append(searchOpts, graph.WithReranker(gm.Reranker))
	}
	if len(searchConfig.OutputFields) > 0 {
		searchOpts = append(searchOpts, graph.WithOutputFields(searchConfig.OutputFields...))
	}

	searchResult, err := gm.DBBackend.Search(ctx, q, searchOpts...)
	if err != nil {
		return nil, err
	}

	var objects []ScoredGraphObject
	if resultList, ok := searchResult[collection]; ok {
		for _, r := range resultList {
			distance, _ := r["distance"].(float64)
			// 根据 collection 类型构造对应的图对象
			var obj any
			uuid, _ := r["uuid"].(string)
			switch collection {
			case graph.EntityCollection:
				obj = stateLookupEntity(r, uuid)
			case graph.RelationCollection:
				obj = stateLookupRelation(r, uuid)
			case graph.EpisodeCollection:
				obj = stateLookupEpisode(r, uuid)
			}
			objects = append(objects, ScoredGraphObject{Score: distance, Object: obj})
		}
	}

	return objects, nil
}

// stateLookupEntity 从搜索结果构造 Entity。
// 对齐 Python LookupTables.get_entity：使用 Entity(**input_obj) 从完整 dict 构造。
func stateLookupEntity(r map[string]any, uuid string) *graph.Entity {
	e := graph.NewEntity()
	e.UUID = uuid
	if v, ok := r["name"].(string); ok {
		e.Name = v
	}
	if v, ok := r["content"].(string); ok {
		e.Content = v
	}
	if v, ok := r["user_id"].(string); ok {
		e.UserID = v
	}
	// 对齐 Python Entity(**input_obj)：补充完整字段
	if v, ok := r["created_at"].(float64); ok {
		e.CreatedAt = int64(v)
	}
	if v, ok := r["language"].(string); ok {
		e.Language = v
	}
	if v, ok := r["obj_type"].(string); ok {
		e.ObjType = v
	}
	if v, ok := r["name_embedding"].([]any); ok {
		e.NameEmbedding = anySliceToFloat64Slice(v)
	}
	if v, ok := r["content_embedding"].([]any); ok {
		e.ContentEmbedding = anySliceToFloat64Slice(v)
	}
	if v, ok := r["relations"].([]any); ok {
		e.Relations = anySliceToStringSlice(v)
	}
	if v, ok := r["episodes"].([]any); ok {
		e.Episodes = anySliceToStringSlice(v)
	}
	if v, ok := r["attributes"].(map[string]any); ok {
		e.Attributes = v
	}
	if v, ok := r["metadata"].(map[string]any); ok {
		e.Metadata = v
	}
	return e
}

// stateLookupRelation 从搜索结果构造 Relation。
// 对齐 Python LookupTables.get_relation：使用 Relation(**input_obj) 从完整 dict 构造。
func stateLookupRelation(r map[string]any, uuid string) *graph.Relation {
	rel := graph.NewRelation()
	rel.UUID = uuid
	if v, ok := r["name"].(string); ok {
		rel.Name = v
	}
	if v, ok := r["content"].(string); ok {
		rel.Content = v
	}
	if v, ok := r["user_id"].(string); ok {
		rel.UserID = v
	}
	// 对齐 Python Relation(**input_obj)：补充完整字段
	if v, ok := r["created_at"].(float64); ok {
		rel.CreatedAt = int64(v)
	}
	if v, ok := r["language"].(string); ok {
		rel.Language = v
	}
	if v, ok := r["obj_type"].(string); ok {
		rel.ObjType = v
	}
	if v, ok := r["name_embedding"].([]any); ok {
		rel.NameEmbedding = anySliceToFloat64Slice(v)
	}
	if v, ok := r["content_embedding"].([]any); ok {
		rel.ContentEmbedding = anySliceToFloat64Slice(v)
	}
	if v, ok := r["valid_since"].(float64); ok {
		rel.ValidSince = int64(v)
	}
	if v, ok := r["valid_until"].(float64); ok {
		rel.ValidUntil = int64(v)
	}
	if v, ok := r["metadata"].(map[string]any); ok {
		rel.Metadata = v
	}
	if v, ok := r["lhs"].(string); ok && v != "" {
		rel.LHS = &graph.Entity{}
		rel.LHS.UUID = v
	}
	if v, ok := r["rhs"].(string); ok && v != "" {
		rel.RHS = &graph.Entity{}
		rel.RHS.UUID = v
	}
	return rel
}

// stateLookupEpisode 从搜索结果构造 Episode。
// 对齐 Python LookupTables.get_episode：使用 Episode(**input_obj) 从完整 dict 构造。
func stateLookupEpisode(r map[string]any, uuid string) *graph.Episode {
	ep := graph.NewEpisode()
	ep.UUID = uuid
	if v, ok := r["content"].(string); ok {
		ep.Content = v
	}
	if v, ok := r["user_id"].(string); ok {
		ep.UserID = v
	}
	// 对齐 Python Episode(**input_obj)：补充完整字段
	if v, ok := r["created_at"].(float64); ok {
		ep.CreatedAt = int64(v)
	}
	if v, ok := r["language"].(string); ok {
		ep.Language = v
	}
	if v, ok := r["obj_type"].(string); ok {
		ep.ObjType = v
	}
	if v, ok := r["content_embedding"].([]any); ok {
		ep.ContentEmbedding = anySliceToFloat64Slice(v)
	}
	if v, ok := r["valid_since"].(float64); ok {
		ep.ValidSince = int64(v)
	}
	if v, ok := r["entities"].([]any); ok {
		ep.Entities = anySliceToStringSlice(v)
	}
	if v, ok := r["metadata"].(map[string]any); ok {
		ep.Metadata = v
	}
	return ep
}

// maybeGC 检查是否需要手动 GC
func (gm *GraphMemory) maybeGC(ctx context.Context) {
	if gm.TimeTillNextGC < 0 {
		return
	}
	gm.ThreadLock.Lock()
	defer gm.ThreadLock.Unlock()

	now := float64(time.Now().Unix())
	if now-gm.lastGC > gm.TimeTillNextGC {
		gm.lastGC = now
		// 对齐 Python: await self.db_backend.refresh(skip_compact=False) — GC 后执行压缩
		if err := gm.DBBackend.Refresh(ctx, graph.WithSkipCompact(false)); err != nil {
			logger.Warn(logComponent).Err(err).Msg("Graph Memory: GC refresh failed")
		}
	}
}

// resolveEntityMerges 解析合并实体后的关系和 Episode 引用
//
// Python: _resolve_entity_merges(merging_args, state)
func (gm *GraphMemory) resolveEntityMerges(ctx context.Context, mergingArgs []MergePair, state *GraphMemState) error {
	episodesToUpdate := make(map[string]struct{})
	entityRelationUpdates := make(map[string]map[string]*graph.Relation)
	mapSrc2Tgt := make(map[string]string)

	for _, pair := range mergingArgs {
		tgtEntity := pair.Target
		tgtUUID := tgtEntity.UUID
		srcEntities := pair.Sources

		state.MergeInfos[tgtUUID] = &EntityMerge{
			Target:          tgtEntity,
			Source:          make(map[string]*graph.Entity),
			NewRelations:    make([]*graph.Relation, 0),
			RelationsToKeep: make(map[string]struct{}),
		}
		alias := make(map[string]struct{})
		alias[tgtUUID] = struct{}{}

		entityRelationUpdates[tgtUUID] = make(map[string]*graph.Relation)
		state.RelationDeferredUpdates[tgtUUID] = make([]deferredRelationUpdate, 0)

		for _, srcEntity := range srcEntities {
			state.MergeInfos[tgtUUID].Source[srcEntity.UUID] = srcEntity
			alias[srcEntity.UUID] = struct{}{}
			mapSrc2Tgt[srcEntity.UUID] = tgtUUID

			// 合并 Episode 引用
			srcEpisodeUUIDs := srcEntity.Episodes
			tgtEntity.Episodes = append(tgtEntity.Episodes, srcEpisodeUUIDs...)
			for _, epUUID := range srcEpisodeUUIDs {
				episodesToUpdate[epUUID] = struct{}{}
			}

			if len(srcEntity.Relations) == 0 {
				continue
			}
			if err := gm.resolveEachRelation(ctx, tgtUUID, srcEntity, mapSrc2Tgt, entityRelationUpdates, state, alias); err != nil {
				logger.Warn(logComponent).Err(err).
					Str("src_entity_uuid", srcEntity.UUID).
					Msg("Graph Memory: resolveEachRelation 失败")
			}
		}
		tgtEntity.Episodes = uniqueStringSlice(tgtEntity.Episodes)
	}

	// 标记有缺陷关系为待删除
	for uuid := range state.FaultyRelations {
		state.MemUpdate.RemovedRelation[uuid] = struct{}{}
	}

	// 分发合并任务（关系过滤 LLM + Episode 更新）
	return gm.dispatchEntityMergeTasks(ctx, episodesToUpdate, entityRelationUpdates, state)
}

// dispatchEntityMergeTasks 分发实体合并任务（关系过滤 LLM + Episode 更新）
//
// Python: _dispatch_entity_merge_tasks(episodes_to_update, entity_relation_updates, state)
func (gm *GraphMemory) dispatchEntityMergeTasks(ctx context.Context, episodesToUpdate map[string]struct{}, entityRelationUpdates map[string]map[string]*graph.Relation, state *GraphMemState) error {
	objCache := state.LookupTable

	// 关系过滤（对齐 Python: if state.strategy.merge_filter）
	if state.Strategy.MergeFilter {
		for tgtUUID, relationDict := range entityRelationUpdates {
			tgtEntity := objCache.Entities[tgtUUID]
			if tgtEntity == nil {
				continue
			}
			relationList := make([]*graph.Relation, 0)
			for _, r := range relationDict {
				if _, faulty := state.FaultyRelations[r.UUID]; !faulty {
					relationList = append(relationList, r)
				}
			}
			state.MergeInfos[tgtUUID].NewRelations = relationList

			kwargs, tmpl, outputModel := extraction.FilterRelationsForMerge(
				tgtEntity, relationList, state.Prompting.Language, state.Extras, 2,
			)
			// 异步发起关系过滤任务（对齐 Python: task = asyncio.create_task(self._invoke_llm(...))）
			filterTask := gm.invokeLLMAsync(ctx, kwargs, tmpl, outputModel)
			state.RelationFilterTasks[filterTask] = &relationFilterTaskItem{
				TargetEntity: tgtEntity,
				Relations:    relationList,
			}
		}
	}

	// 更新 Episode 引用
	if len(episodesToUpdate) > 0 {
		ids := make([]any, 0, len(episodesToUpdate))
		for id := range episodesToUpdate {
			ids = append(ids, id)
		}
		queryResult, err := gm.DBBackend.Query(ctx, graph.EpisodeCollection, graph.WithIDs(ids...))
		if err != nil {
			logger.Warn(logComponent).Err(err).Msg("Graph Memory: Episode query failed")
		} else {
			for _, e := range queryResult {
				ep := state.LookupTable.GetEpisode(e)
				state.MemUpdateSkipEmbed.UpdatedEpisode = append(state.MemUpdateSkipEmbed.UpdatedEpisode, ep)
			}
		}
	}

	return nil
}

// resolveEachRelation 将源实体的每个关系重新映射到目标实体
//
// Python: _resolve_each_relation(tgt_uuid, src_entity, map_src2tgt, entity_relation_updates, state, *, alias)
func (gm *GraphMemory) resolveEachRelation(
	ctx context.Context,
	tgtUUID string,
	srcEntity *graph.Entity,
	mapSrc2Tgt map[string]string,
	entityRelationUpdates map[string]map[string]*graph.Relation,
	state *GraphMemState,
	alias map[string]struct{},
) error {
	selfPointing := make(map[string]struct{})

	// 查询源实体的所有关系（对齐 Python: query_result = await self.db_backend.query(RELATION_COLLECTION, ids=src_entity.relations)）
	srcRelationIDs := anySliceFromStringSlice(srcEntity.Relations)
	queryResult, err := gm.DBBackend.Query(ctx, graph.RelationCollection, graph.WithIDs(srcRelationIDs...))
	if err != nil {
		return err
	}
	var srcRelations []*graph.Relation
	for _, r := range queryResult {
		srcRelations = append(srcRelations, state.LookupTable.GetRelation(r))
	}

	for _, relation := range srcRelations {
		toReplace := srcEntity.UUID
		lhsRhs := map[string]bool{relation.LHSUUID(): true, relation.RHSUUID(): true}

		// 自指向关系 → 移除
		allInAlias := true
		for uuid := range lhsRhs {
			if _, ok := alias[uuid]; !ok {
				allInAlias = false
				break
			}
		}
		if allInAlias {
			state.FaultyRelations[relation.UUID] = relation
			selfPointing[relation.UUID] = struct{}{}
			toReplace = ""
		}

		// 重新映射关系端点
		for toReplace != "" && toReplace != tgtUUID {
			if _, faulty := state.FaultyRelations[relation.UUID]; faulty {
				break
			}
			mapped, ok := mapSrc2Tgt[toReplace]
			if !ok {
				break
			}
			if relation.LHSUUID() == toReplace {
				ReplaceOneSideOfRelation("lhs", relation, tgtUUID, entityRelationUpdates, state)
				break
			}
			if relation.RHSUUID() == toReplace {
				ReplaceOneSideOfRelation("rhs", relation, tgtUUID, entityRelationUpdates, state)
				break
			}
			toReplace = mapped
		}

		// 无法映射的关系 → 标记为有缺陷
		if _, isSelf := selfPointing[relation.UUID]; !isSelf {
			if _, faulty := state.FaultyRelations[relation.UUID]; !faulty {
				if toReplace != "" && toReplace != tgtUUID {
					relationRepr := fmt.Sprintf("[%s]-<%s>->[%s]", relation.LHSUUID(), relation.UUID, relation.RHSUUID())
					logger.Warn(logComponent).
						Str("relation_uuid", relation.UUID).
						Str("src_entity_uuid", srcEntity.UUID).
						Str("tgt_uuid", tgtUUID).
						Str("relation_repr", relationRepr).
						Msg("Graph Memory: relation not connected to entity (caught remapping)")
					state.FaultyRelations[relation.UUID] = relation
				}
			}
		}
	}
	return nil
}

// buildMessages 从 params 构建 MessagesParam
func (gm *GraphMemory) buildMessages(params map[string]any) (model_clients.MessagesParam, error) {
	msgsVal, ok := params["messages"]
	if !ok {
		return model_clients.MessagesParam{}, fmt.Errorf("params 中缺少 messages")
	}

	dictMessages, ok := msgsVal.([]map[string]any)
	if !ok {
		return model_clients.MessagesParam{}, fmt.Errorf("messages 类型不正确: %T", msgsVal)
	}

	// 直接使用 dict 格式的消息列表
	return model_clients.NewDictsMessagesParam(dictMessages), nil
}

// sortEpisodesByValidSince 按 ValidSince 排序 Episode
func sortEpisodesByValidSince(episodes []*graph.Episode) {
	for i := 0; i < len(episodes); i++ {
		for j := i + 1; j < len(episodes); j++ {
			if episodes[i].ValidSince > episodes[j].ValidSince {
				episodes[i], episodes[j] = episodes[j], episodes[i]
			}
		}
	}
}

// assistantContent 提取 AssistantMessage 的文本内容
func assistantContent(msg *llmschema.AssistantMessage) string {
	if msg == nil {
		return ""
	}
	c := msg.GetContent()
	if c.IsText() {
		return c.Text()
	}
	return c.String()
}

// anyListToMapList 将 []any 转为 []map[string]any（过滤非 map 元素）
func anyListToMapList(items []any) []map[string]any {
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if m, ok := item.(map[string]any); ok {
			result = append(result, m)
		}
	}
	return result
}

// anySliceFromStringSlice 将 []string 转为 []any
func anySliceFromStringSlice(items []string) []any {
	result := make([]any, 0, len(items))
	for _, s := range items {
		result = append(result, s)
	}
	return result
}

// anySliceToStringSlice 将 []any 转为 []string，跳过非 string 元素
func anySliceToStringSlice(items []any) []string {
	result := make([]string, 0, len(items))
	for _, item := range items {
		if s, ok := item.(string); ok {
			result = append(result, s)
		}
	}
	return result
}

// anySliceToFloat64Slice 将 []any 转为 []float64，跳过非数值元素
func anySliceToFloat64Slice(items []any) []float64 {
	result := make([]float64, 0, len(items))
	for _, item := range items {
		if f, ok := item.(float64); ok {
			result = append(result, f)
		}
	}
	return result
}

// containsRelationPtr 检查关系指针是否在列表中
func containsRelationPtr(list []*graph.Relation, target *graph.Relation) bool {
	for _, r := range list {
		if r == target {
			return true
		}
	}
	return false
}
