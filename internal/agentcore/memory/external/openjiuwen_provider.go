package external

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	db "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/db"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/embedding"
	kv "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/kv"
	vector "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/vector"
	memconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/memory/config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/ltm"
	apiembedding "github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/embedding"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// ──────────────────────────── 结构体 ────────────────────────────

// OpenJiuwenProvider 基于 openjiuwen LongTermMemory 的外部记忆提供者。
// 对齐 Python: OpenJiuwenMemoryProvider (openjiuwen/core/memory/external/openjiuwen_memory_provider.py)
//
// 不依赖外部 REST API，直接使用项目自身 LTM 引擎（全局单例）。
// 支持双模式构造：config dict 驱动创建 store/embedding + 直接注入。
type OpenJiuwenProvider struct {
	BaseMemoryProvider
	// cfg 配置字典（对齐 Python self._config）
	cfg map[string]any
	// kvStore KV 存储（直接注入或 Initialize 中根据 config 创建）
	kvStore kv.BaseKVStore
	// vectorStore 向量存储（直接注入或 Initialize 中根据 config 创建）
	vectorStore vector.BaseVectorStore
	// dbStore 数据库存储（直接注入或 Initialize 中根据 config 创建）
	dbStore db.BaseDbStore
	// embeddingModel 嵌入模型（直接注入或 Initialize 中根据 config 创建）
	embeddingModel embedding.BaseEmbedding
	// engineConfig 记忆引擎配置（直接注入）
	engineConfig *memconfig.MemoryEngineConfig
	// scopeConfig 记忆作用域配置（直接注入或从 config 解析）
	scopeConfig *memconfig.MemoryScopeConfig
	// agentMemoryConfig Agent 记忆配置（直接注入）
	agentMemoryConfig *memconfig.AgentMemoryConfig
	// userID 用户标识
	userID string
	// scopeID 作用域标识
	scopeID string
	// sessionID 会话标识
	sessionID string
	// initialized 是否已初始化
	initialized bool
}

// OpenJiuwenProviderOption OpenJiuwenProvider 的可选参数。
type OpenJiuwenProviderOption func(*OpenJiuwenProvider)

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// defaultRecallUserMemNum 默认用户记忆召回数量
	// 对齐 Python: DEFAULT_RECALL_USER_MEM_NUM = 5
	defaultRecallUserMemNum = 5
	// defaultRecallHistoryMemNum 默认历史摘要召回数量
	// 对齐 Python: DEFAULT_RECALL_HISTORY_MEM_NUM = 3
	defaultRecallHistoryMemNum = 3
	// defaultKVBackend 默认 KV 后端
	// 对齐 Python: _DEFAULT_KV_BACKEND = "memory"
	defaultKVBackend = "memory"
	// defaultVectorBackend 默认向量后端
	// 对齐 Python: _DEFAULT_VECTOR_BACKEND = "chroma"
	defaultVectorBackend = "chroma"
	// defaultDBBackend 默认数据库后端
	// 对齐 Python: _DEFAULT_DB_BACKEND = "sqlite"
	defaultDBBackend = "sqlite"
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// ojLogComponent 日志组件标识
	ojLogComponent = logger.ComponentAgentCore

	// ltmSearchSchema ltm_search 工具 Schema
	// 对齐 Python: LTM_SEARCH_SCHEMA
	ltmSearchSchema = ToolSchema{
		Name:        "ltm_search",
		Description: "在长期记忆中搜索相关信息。搜索范围包括用户用户画像、情景记忆和语义记忆。",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query":     map[string]any{"type": "string", "description": "搜索查询内容"},
				"num":       map[string]any{"type": "integer", "description": "最大返回结果数量", "default": defaultRecallUserMemNum},
				"threshold": map[string]any{"type": "number", "description": "最小相关性阈值 (0-1)", "default": 0.3},
			},
			"required": []string{"query"},
		},
	}

	// ltmSearchSummarySchema ltm_search_summary 工具 Schema
	// 对齐 Python: LTM_SEARCH_SUMMARY_SCHEMA
	ltmSearchSummarySchema = ToolSchema{
		Name:        "ltm_search_summary",
		Description: "在长期记忆中搜索历史会话摘要。用于回忆之前讨论的话题和达成的结论。",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{"type": "string", "description": "搜索查询内容"},
				"num":   map[string]any{"type": "integer", "description": "最大返回结果数量", "default": defaultRecallHistoryMemNum},
			},
			"required": []string{"query"},
		},
	}
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewOpenJiuwenProvider 创建 OpenJiuwenProvider 实例。
// 对齐 Python: OpenJiuwenMemoryProvider.__init__(config, kv_store, vector_store, db_store, embedding_model, engine_config, scope_config, agent_memory_config)
//
// 双模式构造：config dict 驱动创建 store/embedding + 直接注入。
// config 为 nil 时默认空 map。注入的 store/embedding 优先，缺失的由 Initialize 中根据 config 创建。
func NewOpenJiuwenProvider(config map[string]any, opts ...OpenJiuwenProviderOption) *OpenJiuwenProvider {
	if config == nil {
		config = make(map[string]any)
	}
	p := &OpenJiuwenProvider{
		cfg:               config,
		userID:            ltm.DefaultValue,
		scopeID:           ltm.DefaultValue,
		sessionID:         ltm.DefaultValue,
		agentMemoryConfig: memconfig.DefaultAgentMemoryConfig(),
	}
	// 对齐 Python L79: self._scope_config = scope_config or self._parse_scope_config()
	p.scopeConfig = p.parseScopeConfig()
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// OJWithKVStore 设置 KV 存储。
func OJWithKVStore(store kv.BaseKVStore) OpenJiuwenProviderOption {
	return func(p *OpenJiuwenProvider) { p.kvStore = store }
}

// OJWithVectorStore 设置向量存储。
func OJWithVectorStore(store vector.BaseVectorStore) OpenJiuwenProviderOption {
	return func(p *OpenJiuwenProvider) { p.vectorStore = store }
}

// OJWithDbStore 设置数据库存储。
func OJWithDbStore(store db.BaseDbStore) OpenJiuwenProviderOption {
	return func(p *OpenJiuwenProvider) { p.dbStore = store }
}

// OJWithEmbeddingModel 设置嵌入模型。
func OJWithEmbeddingModel(model embedding.BaseEmbedding) OpenJiuwenProviderOption {
	return func(p *OpenJiuwenProvider) { p.embeddingModel = model }
}

// OJWithEngineConfig 设置记忆引擎配置。
func OJWithEngineConfig(cfg *memconfig.MemoryEngineConfig) OpenJiuwenProviderOption {
	return func(p *OpenJiuwenProvider) { p.engineConfig = cfg }
}

// OJWithScopeConfig 设置记忆作用域配置。
func OJWithScopeConfig(cfg *memconfig.MemoryScopeConfig) OpenJiuwenProviderOption {
	return func(p *OpenJiuwenProvider) { p.scopeConfig = cfg }
}

// OJWithAgentMemoryConfig 设置 Agent 记忆配置。
func OJWithAgentMemoryConfig(cfg *memconfig.AgentMemoryConfig) OpenJiuwenProviderOption {
	return func(p *OpenJiuwenProvider) { p.agentMemoryConfig = cfg }
}

// Name 返回 Provider 唯一名称。
// Python: @property name -> "openjiuwen"
func (p *OpenJiuwenProvider) Name() string {
	return "openjiuwen"
}

// IsAvailable 检查 Provider 是否已配置且就绪。
// Python: is_available() -> bool
// 三 store 都有 OR config.embedding.model_name 存在
func (p *OpenJiuwenProvider) IsAvailable() bool {
	if p.kvStore != nil && p.vectorStore != nil && p.dbStore != nil {
		return true
	}
	embedCfg, _ := p.cfg["embedding"].(map[string]any)
	if embedCfg == nil {
		return false
	}
	modelName, _ := embedCfg["model_name"].(string)
	return modelName != ""
}

// IsInitialized 返回 Provider 是否已初始化。
// Python: @property is_initialized -> self._is_initialized
func (p *OpenJiuwenProvider) IsInitialized() bool {
	return p.initialized
}

// GetToolSchemas 返回 Provider 提供的工具 Schema 列表。
// Python: get_tool_schemas() -> [LTM_SEARCH_SCHEMA, LTM_SEARCH_SUMMARY_SCHEMA]
func (p *OpenJiuwenProvider) GetToolSchemas() []ToolSchema {
	return []ToolSchema{ltmSearchSchema, ltmSearchSummarySchema}
}

// SystemPromptBlock 返回 Provider 的系统提示词引导块。
// Python: system_prompt_block() (L133-148)
// 提示词内容从 Python 源码直接复制，禁止自行翻译或改写。
func (p *OpenJiuwenProvider) SystemPromptBlock() string {
	return "# Long-Term Memory System\n\n" +
		"You have long-term memory capabilities and can remember user information across sessions. " +
		"The system automatically extracts valuable information from conversations and stores it in memory.\n\n" +
		"## Memory Search\n\n" +
		"When you need to recall previous information, use the `ltm_search` tool to search long-term memory.\n" +
		"- Search queries should contain key information (names, dates, event keywords)\n" +
		"- If results are insufficient, try searching again with different keywords\n\n" +
		"## Automatic Memory\n\n" +
		"The system automatically extracts from each conversation:\n" +
		"- User profile (identity, preferences, habits)\n" +
		"- Episodic memory (specific events, decisions)\n" +
		"- Semantic memory (background knowledge, technical details)\n" +
		"- Conversation summaries (key conclusions, main points)"
}

// Shutdown 关闭 Provider 释放资源。
// Python: async def shutdown() -> None: self._is_initialized = False
func (p *OpenJiuwenProvider) Shutdown(_ context.Context) error {
	p.initialized = false
	return nil
}

// Initialize 初始化 Provider。
// 对齐 Python: async def initialize(self, **kwargs) -> None (L100-131)
//
// 完整流程：
// 1. 从 opts 解析 user_id / scope_id / session_id
// 2. 缺失 store → 根据 config 创建
// 3. 获取 LTM 全局单例
// 4. LTM.KVStore 为 nil → RegisterStore
// 5. scopeConfig 不为 nil → LTM.SetScopeConfig
// 6. p.initialized = true
func (p *OpenJiuwenProvider) Initialize(ctx context.Context, opts ...ProviderOption) error {
	po := applyOptions(opts...)

	// 对齐 Python: self._user_id = kwargs.get("user_id", self._user_id)
	if po.UserID != "" {
		p.userID = po.UserID
	}
	if po.ScopeID != "" {
		p.scopeID = po.ScopeID
	}
	if po.SessionID != "" {
		p.sessionID = po.SessionID
	}

	// 对齐 Python: if self._kv_store is None: self._kv_store = self._create_kv_store()
	if p.kvStore == nil {
		p.kvStore = p.createKVStore()
	}
	if p.vectorStore == nil {
		p.vectorStore = p.createVectorStore()
	}
	if p.dbStore == nil {
		p.dbStore = p.createDBStore(ctx)
	}
	if p.embeddingModel == nil {
		p.embeddingModel = p.createEmbedding()
	}

	// 对齐 Python: if not self._kv_store or not self._vector_store or not self._db_store: logger.error(...); return
	if p.kvStore == nil || p.vectorStore == nil || p.dbStore == nil {
		// 对齐 Python L114-116: logger.error("[OpenJiuwenMemoryProvider] Store creation failed")
		// Go 差异：Python 是 void 返回，Go 签名返回 error，应返回 error 让调用方感知失败
		logger.Error(ojLogComponent).Msg("Store 创建失败")
		return fmt.Errorf("store creation failed: kv=%v vector=%v db=%v",
			p.kvStore != nil, p.vectorStore != nil, p.dbStore != nil)
	}

	// 对齐 Python: self._ltm = LongTermMemory()
	ltmInstance := ltm.GetLongTermMemory()

	// 对齐 Python: if self._ltm.kv_store is None: await self._ltm.register_store(...)
	if ltmInstance.KVStore() == nil {
		registerOpts := []ltm.RegisterStoreOption{
			ltm.WithVectorStore(p.vectorStore),
			ltm.WithDbStore(p.dbStore),
			ltm.WithEmbeddingModel(p.embeddingModel),
		}
		if err := ltmInstance.RegisterStore(ctx, p.kvStore, registerOpts...); err != nil {
			logger.Error(ojLogComponent).Err(err).Msg("LTM RegisterStore 失败")
			return err
		}
	}

	// 对齐 Python: if self._scope_config: await self._ltm.set_scope_config(self._scope_id, self._scope_config)
	// 注意：scopeConfig 在构造时已解析（对齐 Python L79），此处仅使用
	if p.scopeConfig != nil {
		if err := ltmInstance.SetScopeConfig(ctx, p.scopeID, p.scopeConfig); err != nil {
			logger.Warn(ojLogComponent).Err(err).Str("scope_id", p.scopeID).Msg("SetScopeConfig 失败")
		}
	}

	// 对齐 Python L131: self._is_initialized = True
	p.initialized = true
	return nil
}

// HandleToolCall 处理工具调用并返回结果字符串。
// 对齐 Python: async def handle_tool_call(self, tool_name, args) -> str (L153-164)
func (p *OpenJiuwenProvider) HandleToolCall(ctx context.Context, toolName string, args map[string]any) (string, error) {
	// 对齐 Python L154-155: if not self._ltm or not self._is_initialized: return json.dumps({"error": "Memory provider not initialized"})
	if !p.initialized {
		b, _ := json.Marshal(map[string]string{"error": "Memory provider not initialized"})
		return string(b), nil
	}
	ltmInstance := ltm.GetLongTermMemory()
	if !ltmInstance.IsInitialized() {
		b, _ := json.Marshal(map[string]string{"error": "Memory provider not initialized"})
		return string(b), nil
	}

	// 对齐 Python L156-164: try: if/elif/else except Exception as e: return json.dumps({"error": str(e), "results": []})
	switch toolName {
	case "ltm_search", "ltm_search_summary":
		result, err := p.dispatchToolCall(ctx, toolName, args)
		if err != nil {
			// 对齐 Python L163-164: except Exception as e: return json.dumps({"error": str(e), "results": []})
			b, _ := json.Marshal(map[string]any{"error": err.Error(), "results": []any{}})
			return string(b), nil
		}
		b, _ := json.Marshal(result)
		return string(b), nil
	default:
		// 对齐 Python L162: return json.dumps({"error": f"Unknown tool: {tool_name}"})
		b, _ := json.Marshal(map[string]string{"error": fmt.Sprintf("Unknown tool: %s", toolName)})
		return string(b), nil
	}
}

// Prefetch 根据查询预取记忆上下文。
// 对齐 Python: async def prefetch(self, query, **kwargs) -> str (L166-201)
func (p *OpenJiuwenProvider) Prefetch(ctx context.Context, query string, opts ...ProviderOption) (string, error) {
	// 对齐 Python L167-168: if not self._ltm or not self._is_initialized: return ""
	ltmInstance := ltm.GetLongTermMemory()
	if !p.initialized || !ltmInstance.IsInitialized() {
		return "", nil
	}

	po := applyOptions(opts...)
	userID := p.userID
	if po.UserID != "" {
		userID = po.UserID
	}
	scopeID := p.scopeID
	if po.ScopeID != "" {
		scopeID = po.ScopeID
	}

	parts := make([]string, 0)

	// 对齐 Python L172-185: mem_results = await self._ltm.search_user_mem(...)
	func() {
		defer func() {
			if r := recover(); r != nil {
				// 对齐 Python L185-186: except Exception as e: logger.warning(f"prefetch search_user_mem failed: {e}")
				logger.Warn(ojLogComponent).Any("panic", r).Msg("prefetch search_user_mem 失败")
			}
		}()
		results, err := ltmInstance.SearchUserMem(ctx, query, defaultRecallUserMemNum,
			ltm.SearchWithUserID(userID),
			ltm.SearchWithScopeID(scopeID),
			ltm.SearchWithThreshold(0.3),
		)
		if err != nil {
			// 对齐 Python L185-186: logger.warning(f"prefetch search_user_mem failed: {e}")
			logger.Warn(ojLogComponent).Err(err).Msg("prefetch search_user_mem 失败")
			return
		}
		if len(results) > 0 {
			// 对齐 Python L181: parts.append("## Related Memories")
			parts = append(parts, "## Related Memories")
			for _, r := range results {
				// 对齐 Python L183: type_label = r.mem_info.type.value if r.mem_info.type else "unknown"
				typeLabel := "unknown"
				if r.MemInfo != nil {
					typeLabel = r.MemInfo.Type.String()
				}
				content := ""
				if r.MemInfo != nil {
					content = r.MemInfo.Content
				}
				// 对齐 Python L184: parts.append(f"- [{type_label}] {r.mem_info.content} (score: {r.score:.2f})")
				parts = append(parts, fmt.Sprintf("- [%s] %s (score: %.2f)", typeLabel, content, r.Score))
			}
		}
	}()

	// 对齐 Python L187-199: summary_results = await self._ltm.search_user_history_summary(...)
	func() {
		defer func() {
			if r := recover(); r != nil {
				// 对齐 Python L199-200: except Exception as e: logger.warning(f"prefetch search_user_history_summary failed: {e}")
				logger.Warn(ojLogComponent).Any("panic", r).Msg("prefetch search_user_history_summary 失败")
			}
		}()
		results, err := ltmInstance.SearchUserHistorySummary(ctx, query, defaultRecallHistoryMemNum,
			ltm.SearchWithUserID(userID),
			ltm.SearchWithScopeID(scopeID),
			ltm.SearchWithThreshold(0.3),
		)
		if err != nil {
			logger.Warn(ojLogComponent).Err(err).Msg("prefetch search_user_history_summary 失败")
			return
		}
		if len(results) > 0 {
			// 对齐 Python L196: parts.append("\n## Related History Summaries")
			parts = append(parts, "\n## Related History Summaries")
			for _, r := range results {
				content := ""
				if r.MemInfo != nil {
					content = r.MemInfo.Content
				}
				// 对齐 Python L198: parts.append(f"- {r.mem_info.content} (score: {r.score:.2f})")
				parts = append(parts, fmt.Sprintf("- %s (score: %.2f)", content, r.Score))
			}
		}
	}()

	// 对齐 Python L200-201: if not parts: return "" / return "\n".join(parts) if parts else ""
	if len(parts) == 0 {
		return "", nil
	}
	result := ""
	for i, part := range parts {
		if i > 0 {
			result += "\n"
		}
		result += part
	}
	return result, nil
}

// SyncTurn 同步一轮对话到外部记忆。
// 对齐 Python: async def sync_turn(self, user_msg, assistant_msg, **kwargs) -> None (L203-225)
func (p *OpenJiuwenProvider) SyncTurn(ctx context.Context, userMsg, assistantMsg string, opts ...ProviderOption) error {
	// 对齐 Python L204-205: if not self._ltm or not self._is_initialized: return
	ltmInstance := ltm.GetLongTermMemory()
	if !p.initialized || !ltmInstance.IsInitialized() {
		return nil
	}

	po := applyOptions(opts...)
	userID := p.userID
	if po.UserID != "" {
		userID = po.UserID
	}
	scopeID := p.scopeID
	if po.ScopeID != "" {
		scopeID = po.ScopeID
	}
	sessionID := p.sessionID
	if po.SessionID != "" {
		sessionID = po.SessionID
	}

	// 对齐 Python L209: messages: list[BaseMessage] = []
	var messages []llmschema.BaseMessage
	// 对齐 Python L210-211: if user_msg: messages.append(UserMessage(content=user_msg))
	if userMsg != "" {
		messages = append(messages, llmschema.NewUserMessage(userMsg))
	}
	// 对齐 Python L212-213: if assistant_msg: messages.append(AssistantMessage(content=assistant_msg))
	if assistantMsg != "" {
		messages = append(messages, llmschema.NewAssistantMessage(assistantMsg))
	}
	// 对齐 Python L214-215: if not messages: return
	if len(messages) == 0 {
		return nil
	}

	// 对齐 Python L216-224: try: await self._ltm.add_messages(...) except Exception as e: logger.warning(...)
	func() {
		defer func() {
			if r := recover(); r != nil {
				// 对齐 Python L224-225: except Exception as e: logger.warning(f"sync_turn add_messages failed: {e}")
				logger.Warn(ojLogComponent).Any("panic", r).Msg("sync_turn add_messages 失败")
			}
		}()
		// 对齐 Python L217-223: await self._ltm.add_messages(messages, self._agent_memory_config, user_id=user_id, scope_id=scope_id, session_id=session_id)
		_, err := ltmInstance.AddMessages(ctx, messages, p.agentMemoryConfig,
			ltm.WithUserID(userID),
			ltm.WithScopeID(scopeID),
			ltm.WithSessionID(sessionID),
		)
		if err != nil {
			// 对齐 Python L224-225: logger.warning(f"sync_turn add_messages failed: {e}")
			logger.Warn(ojLogComponent).Err(err).Msg("sync_turn add_messages 失败")
		}
	}()

	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// dispatchToolCall 分发工具调用到对应的 handleXxx 方法。
// 对齐 Python: handle_tool_call 中的 if/elif 分支
func (p *OpenJiuwenProvider) dispatchToolCall(ctx context.Context, toolName string, args map[string]any) (map[string]any, error) {
	switch toolName {
	case "ltm_search":
		return p.handleSearch(ctx, args)
	case "ltm_search_summary":
		return p.handleSearchSummary(ctx, args)
	default:
		return nil, fmt.Errorf("unknown tool: %s", toolName)
	}
}

// handleSearch 处理 ltm_search 工具调用。
// 对齐 Python: _handle_search (L302-324)
func (p *OpenJiuwenProvider) handleSearch(ctx context.Context, args map[string]any) (map[string]any, error) {
	// 对齐 Python L303-308: results = await self._ltm.search_user_mem(query=..., num=..., user_id=..., scope_id=..., threshold=...)
	query, _ := args["query"].(string)
	num := defaultRecallUserMemNum
	if n, ok := args["num"]; ok {
		num = int(floatVal(n))
	}
	// 对齐 Python L308: threshold=args.get("threshold", 0.3)
	// 使用 floatVal 统一处理 int/float64 等数字类型
	// 对齐 Python L308: threshold=args.get("threshold", 0.3)
	// 直接使用传入值，threshold=0 是合法的（不进行相关性过滤）
	threshold := 0.3
	if t, ok := args["threshold"]; ok {
		threshold = floatVal(t)
	}

	ltmInstance := ltm.GetLongTermMemory()
	results, err := ltmInstance.SearchUserMem(ctx, query, num,
		ltm.SearchWithUserID(p.userID),
		ltm.SearchWithScopeID(p.scopeID),
		ltm.SearchWithThreshold(threshold),
	)
	if err != nil {
		return nil, err
	}

	// 对齐 Python L310-324: 构建 results 列表
	type searchResult struct {
		ID      string  `json:"id"`
		Content string  `json:"content"`
		Type    string  `json:"type"`
		Score   float64 `json:"score"`
	}
	out := make([]searchResult, 0, len(results))
	for _, r := range results {
		typeStr := "unknown"
		memID := ""
		content := ""
		score := r.Score
		if r.MemInfo != nil {
			// 对齐 Python L315: type_label = r.mem_info.type.value if r.mem_info.type else "unknown"
			typeStr = r.MemInfo.Type.String()
			memID = r.MemInfo.MemID
			content = r.MemInfo.Content
		}
		out = append(out, searchResult{
			ID: memID, Content: content, Type: typeStr, Score: score,
		})
	}
	return map[string]any{"results": out, "count": len(out)}, nil
}

// handleSearchSummary 处理 ltm_search_summary 工具调用。
// 对齐 Python: _handle_search_summary (L326-347)
func (p *OpenJiuwenProvider) handleSearchSummary(ctx context.Context, args map[string]any) (map[string]any, error) {
	// 对齐 Python L327-331: results = await self._ltm.search_user_history_summary(query=..., num=..., user_id=..., scope_id=..., threshold=0.3)
	query, _ := args["query"].(string)
	num := defaultRecallHistoryMemNum
	if n, ok := args["num"]; ok {
		num = int(floatVal(n))
	}

	ltmInstance := ltm.GetLongTermMemory()
	results, err := ltmInstance.SearchUserHistorySummary(ctx, query, num,
		ltm.SearchWithUserID(p.userID),
		ltm.SearchWithScopeID(p.scopeID),
		ltm.SearchWithThreshold(0.3),
	)
	if err != nil {
		return nil, err
	}

	// 对齐 Python L333-346: 构建 results 列表
	type summaryResult struct {
		ID      string  `json:"id"`
		Content string  `json:"content"`
		Score   float64 `json:"score"`
	}
	out := make([]summaryResult, 0, len(results))
	for _, r := range results {
		memID := ""
		content := ""
		score := r.Score
		if r.MemInfo != nil {
			memID = r.MemInfo.MemID
			content = r.MemInfo.Content
		}
		out = append(out, summaryResult{ID: memID, Content: content, Score: score})
	}
	return map[string]any{"results": out, "count": len(out)}, nil
}

// createKVStore 根据 config 创建 KV 存储。
// 对齐 Python: _create_kv_store (L240-258)
func (p *OpenJiuwenProvider) createKVStore() kv.BaseKVStore {
	kvCfg, _ := p.cfg["kv"].(map[string]any)
	if kvCfg == nil {
		kvCfg = make(map[string]any)
	}
	// 对齐 Python L242: backend = kv_cfg.get("backend", self._DEFAULT_KV_BACKEND)
	backend, _ := kvCfg["backend"].(string)
	if backend == "" {
		backend = defaultKVBackend
	}
	switch backend {
	case "memory":
		// 对齐 Python L244-245: InMemoryKVStore()
		return kv.NewInMemoryKVStore()
	case "shelve":
		// 对齐 Python L253-254: ShelveStore(db_path=kv_cfg.get("path", "memory_kv"))
		path, _ := kvCfg["path"].(string)
		if path == "" {
			path = resolveLTMDir() + "/kv"
		}
		store, err := kv.NewFileKVStore(path)
		if err != nil {
			// 对齐 Python L256-257: logger.error("[OpenJiuwenMemoryProvider] KV store creation failed ({backend}): {e}")
			logger.Error(ojLogComponent).Str("backend", backend).Err(err).Msg("KV store 创建失败")
			return nil
		}
		return store
	case "sqlite":
		// 对齐 Python L247-251: DbBasedKVStore(engine)
		path, _ := kvCfg["path"].(string)
		if path == "" {
			path = resolveLTMDir() + "/memory_kv.db"
		}
		dsn := fmt.Sprintf("file:%s?cache=shared&_journal_mode=WAL", path)
		gormDB, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
		if err != nil {
			logger.Error(ojLogComponent).Str("backend", backend).Err(err).Msg("KV store 创建失败")
			return nil
		}
		return kv.NewDbBasedKVStore(gormDB)
	default:
		logger.Error(ojLogComponent).Str("backend", backend).Msg("不支持的 KV 后端类型")
		return nil
	}
}

// createVectorStore 根据 config 创建向量存储。
// 对齐 Python: _create_vector_store (L260-270)
func (p *OpenJiuwenProvider) createVectorStore() vector.BaseVectorStore {
	vecCfg, _ := p.cfg["vector"].(map[string]any)
	if vecCfg == nil {
		vecCfg = make(map[string]any)
	}
	// 对齐 Python L262: backend = vec_cfg.get("backend", self._DEFAULT_VECTOR_BACKEND)
	backend, _ := vecCfg["backend"].(string)
	if backend == "" {
		backend = defaultVectorBackend
	}
	switch backend {
	case "chroma":
		// 对齐 Python L264-266: create_vector_store(backend, **{k: v for k, v in vec_cfg.items() if k != "backend"})
		persistDir, _ := vecCfg["persist_directory"].(string)
		if persistDir == "" {
			persistDir = resolveLTMDir() + "/chroma"
		}
		return vector.NewChromaVectorStore(persistDir)
	default:
		// 对齐 Python L268-269: logger.error("[OpenJiuwenMemoryProvider] Vector store creation failed ({backend}): {e}")
		logger.Error(ojLogComponent).Str("backend", backend).Msg("不支持的 vector 后端类型")
		return nil
	}
}

// createDBStore 根据 config 创建数据库存储。
// 对齐 Python: _create_db_store (L272-284)
func (p *OpenJiuwenProvider) createDBStore(ctx context.Context) db.BaseDbStore {
	dbCfg, _ := p.cfg["db"].(map[string]any)
	if dbCfg == nil {
		dbCfg = make(map[string]any)
	}
	// 对齐 Python L274: backend = db_cfg.get("backend", self._DEFAULT_DB_BACKEND)
	backend, _ := dbCfg["backend"].(string)
	if backend == "" {
		backend = defaultDBBackend
	}
	switch backend {
	case "sqlite":
		// 对齐 Python L276-280: DefaultDbStore(engine)
		path, _ := dbCfg["path"].(string)
		if path == "" {
			path = resolveLTMDir() + "/ltm.db"
		}
		dsn := fmt.Sprintf("file:%s?cache=shared&_journal_mode=WAL", path)
		gormDB, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
		if err != nil {
			// 对齐 Python L282-283: logger.error("[OpenJiuwenMemoryProvider] DB store creation failed ({backend}): {e}")
			logger.Error(ojLogComponent).Str("backend", backend).Err(err).Msg("DB store 创建失败")
			return nil
		}
		return db.NewDefaultDbStore(gormDB)
	default:
		logger.Error(ojLogComponent).Str("backend", backend).Msg("不支持的 DB 后端类型")
		return nil
	}
}

// createEmbedding 根据 config 创建嵌入模型。
// 对齐 Python: _create_embedding (L286-300)
func (p *OpenJiuwenProvider) createEmbedding() embedding.BaseEmbedding {
	embedCfg, _ := p.cfg["embedding"].(map[string]any)
	if embedCfg == nil {
		return nil
	}
	// 对齐 Python L288-289: if not embed_cfg.get("model_name"): return None
	modelName, _ := embedCfg["model_name"].(string)
	if modelName == "" {
		return nil
	}
	// 对齐 Python L291-297: APIEmbedding(config)
	baseURL, _ := embedCfg["base_url"].(string)
	apiKey, _ := embedCfg["api_key"].(string)
	cfg := apiembedding.EmbeddingConfig{
		ModelName: modelName,
		BaseURL:   baseURL,
		APIKey:    apiKey,
	}
	emb := apiembedding.NewAPIEmbedding(cfg)
	if emb == nil {
		// 对齐 Python L298-299: logger.error("[OpenJiuwenMemoryProvider] Embedding creation failed: {e}")
		logger.Error(ojLogComponent).Msg("Embedding 创建失败")
		return nil
	}
	return emb
}

// parseScopeConfig 从 config 解析 MemoryScopeConfig。
// 对齐 Python: _parse_scope_config (L230-238)
func (p *OpenJiuwenProvider) parseScopeConfig() *memconfig.MemoryScopeConfig {
	scopeCfg, _ := p.cfg["scope_config"].(map[string]any)
	if len(scopeCfg) == 0 {
		return nil
	}
	data, err := json.Marshal(scopeCfg)
	if err != nil {
		// 对齐 Python L237: logger.warning("[OpenJiuwenMemoryProvider] Failed to parse scope_config: {e}")
		logger.Warn(ojLogComponent).Err(err).Msg("解析 scope_config 失败")
		return nil
	}
	cfg := &memconfig.MemoryScopeConfig{}
	if err := json.Unmarshal(data, cfg); err != nil {
		logger.Warn(ojLogComponent).Err(err).Msg("解析 scope_config 失败")
		return nil
	}
	return cfg
}

// resolveLTMDir 解析 LTM 数据目录。
// 对齐 Python: _resolve_ltm_dir() → {workspace_dir}/memory/ltm
func resolveLTMDir() string {
	return filepath.Join(workspace.WorkspaceDir(), "memory", "ltm")
}
