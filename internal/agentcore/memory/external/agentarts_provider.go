package external

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/kv"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AgentArtsProvider 华为云 AgentArts 记忆服务适配器。
// 对齐 Python: AgentArtsMemoryProvider (agentarts_memory_provider.py)
//
// 将 AgentArts Data Plane REST API 适配为 MemoryProvider 接口。
// 仅跟踪 consecutiveFailures 计数，无内建熔断器（对齐 Python 语义），
// 靠外层 ExternalMemoryRail 统一熔断。
type AgentArtsProvider struct {
	BaseMemoryProvider
	// baseURL Data Plane 基础 URL
	baseURL string
	// apiKey Bearer token
	apiKey string
	// spaceID AgentArts Space 标识
	spaceID string
	// defaultActorID 构造时传入的默认 actor_id
	defaultActorID string
	// actorID 运行时 actor_id（Initialize 可覆盖）
	actorID string
	// defaultAssistantID 构造时传入的默认 assistant_id
	defaultAssistantID string
	// assistantID 运行时 assistant_id（Initialize 可覆盖）
	assistantID string
	// client 懒加载 HTTP 客户端
	client *agentartsClient
	// clientOnce 客户端初始化守卫
	clientOnce sync.Once
	// sessionID 当前会话标识
	sessionID string
	// sessionMappingStore session_id → memory_session_id 映射存储
	sessionMappingStore kv.BaseKVStore
	// initialized 是否已初始化
	initialized bool
	// mu 保护并发访问
	mu sync.RWMutex
	// consecutiveFailures 连续失败计数（对齐 Python _consecutive_failures）
	consecutiveFailures int
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// sessionMappingKeyPrefix session 映射 KV 前缀
	// 对齐 Python: _SESSION_MAPPING_KEY_PREFIX
	sessionMappingKeyPrefix = "agentarts/session_mapping"
	// maxTopK 搜索最大返回数
	// 对齐 Python: _MAX_TOP_K = 100
	maxTopK = 100
	// defaultTopK 搜索默认返回数
	// 对齐 Python: _DEFAULT_TOP_K = 10
	defaultTopK = 10
	// defaultMinScore 搜索默认最低相似度
	// 对齐 Python: _DEFAULT_MIN_SCORE = 0.5
	defaultMinScore = 0.5
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// agentartsLogComponent 日志组件标识
	agentartsLogComponent = logger.ComponentAgentCore

	// externalMemorySearchSchema external_memory_search 工具 Schema
	// 对齐 Python: EXTERNAL_MEMORY_SEARCH_SCHEMA（提示词从 Python 源码直接复制）
	externalMemorySearchSchema = ToolSchema{
		Name: "external_memory_search",
		Description: "Search long-term external memory for durable facts, user preferences, " +
			"and prior conversation context.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "Memory search query.",
				},
				"top_k": map[string]any{
					"type":        "integer",
					"description": "Max results, default 10, max 100.",
				},
				"strategy_type": map[string]any{
					"type":        "string",
					"enum":        []string{"semantic", "summary", "user_preference", "episodic", "event", "custom"},
					"description": "Optional memory strategy type filter. Acceptable values: semantic, summary, user_preference, episodic, event, custom.",
				},
				"min_score": map[string]any{
					"type":        "number",
					"description": "Optional minimum similarity score. (default: 0.5)",
				},
			},
			"required": []string{"query"},
		},
	}
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewAgentArtsProvider 创建 AgentArtsProvider 实例。
// 对齐 Python: AgentArtsMemoryProvider.__init__(base_url=..., api_key=..., space_id=...,
//
//	actor_id=..., assistant_id=..., session_mapping_store=...)
//
// sessionMappingStore 为 nil 时默认使用 kv.NewInMemoryKVStore()。
func NewAgentArtsProvider(baseURL, apiKey, spaceID, actorID, assistantID string, sessionMappingStore kv.BaseKVStore) *AgentArtsProvider {
	if sessionMappingStore == nil {
		sessionMappingStore = kv.NewInMemoryKVStore()
	}
	return &AgentArtsProvider{
		baseURL:             baseURL,
		apiKey:              apiKey,
		spaceID:             spaceID,
		defaultActorID:      actorID,
		actorID:             actorID,
		defaultAssistantID:  assistantID,
		assistantID:         assistantID,
		sessionMappingStore: sessionMappingStore,
	}
}

// Name 返回 Provider 唯一名称。
// Python: @property name -> "agentarts"
func (p *AgentArtsProvider) Name() string {
	return "agentarts"
}

// IsAvailable 检查 Provider 是否已配置且就绪（无网络调用）。
// Python: is_available() -> bool(self._api_key and self._space_id)
func (p *AgentArtsProvider) IsAvailable() bool {
	return p.apiKey != "" && p.spaceID != ""
}

// IsInitialized 返回 Provider 是否已初始化。
// Python: @property is_initialized -> self._initialized
func (p *AgentArtsProvider) IsInitialized() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.initialized
}

// GetToolSchemas 返回 Provider 提供的工具 Schema 列表。
// Python: get_tool_schemas() -> [EXTERNAL_MEMORY_SEARCH_SCHEMA]
func (p *AgentArtsProvider) GetToolSchemas() []ToolSchema {
	return []ToolSchema{externalMemorySearchSchema}
}

// SystemPromptBlock 返回 Provider 的系统提示词引导块。
// Python: system_prompt_block() -> "# External Memory\nUse `external_memory_search`..."
func (p *AgentArtsProvider) SystemPromptBlock() string {
	return "# External Memory\n" +
		"Use `external_memory_search` to retrieve durable facts, user preferences, " +
		"and prior conversation context from long-term external memory."
}

// Initialize 初始化 Provider。
// 对齐 Python: async def initialize(self, **kwargs) -> None
func (p *AgentArtsProvider) Initialize(ctx context.Context, opts ...ProviderOption) error {
	po := applyOptions(opts...)

	// 对齐 Python: self._actor_id = kwargs.get("user_id")
	if po.UserID != "" {
		p.actorID = po.UserID
	}
	// 对齐 Python: self._assistant_id = kwargs.get("assistant_id") or kwargs.get("scope_id")
	if po.ScopeID != "" {
		p.assistantID = po.ScopeID
	}
	sessionID := po.SessionID

	// 对齐 Python: logger.info("[AgentArtsMemoryProvider] initializing with params: %s", json.dumps(kwargs))
	logger.Info(agentartsLogComponent).
		Str("provider", "agentarts").
		Str("user_id", p.actorID).
		Str("assistant_id", p.assistantID).
		Str("session_id", sessionID).
		Msg("[AgentArtsMemoryProvider] initializing with params")

	// 对齐 Python: await self._ensure_memory_session(session_id, actor_id=..., assistant_id=...)
	// Python 无 try/except，异常直接传播
	if sessionID != "" {
		if _, err := p.ensureMemorySession(ctx, sessionID, p.actorID, p.assistantID); err != nil {
			// 对齐 Python: 异常传播，initialized 不设为 true
			return fmt.Errorf("ensureMemorySession 失败: %w", err)
		}
	}

	p.mu.Lock()
	p.sessionID = sessionID
	p.initialized = true
	p.mu.Unlock()

	return nil
}

// Prefetch 根据查询预取记忆上下文。
// 对齐 Python: async def prefetch(self, query, **kwargs) -> str
func (p *AgentArtsProvider) Prefetch(ctx context.Context, query string, _ ...ProviderOption) (string, error) {
	if query == "" {
		return "", nil
	}

	items, err := p.search(ctx, query, map[string]any{})
	if err != nil {
		p.recordFailure()
		// 对齐 Python: logger.debug("[AgentArtsMemoryProvider] prefetch failed: %s", exc)
		logger.Debug(agentartsLogComponent).Err(err).Msg("[AgentArtsMemoryProvider] prefetch failed")
		return "", nil
	}

	p.recordSuccess()

	if len(items) == 0 {
		return "", nil
	}

	// 对齐 Python: "## External Memory\n" + "\n".join(f"- {item['memory']}" for item in items)
	lines := make([]string, 0, len(items))
	for _, item := range items {
		if item.Memory != "" {
			lines = append(lines, "- "+item.Memory)
		}
	}
	if len(lines) == 0 {
		return "", nil
	}
	return "## External Memory\n" + strings.Join(lines, "\n"), nil
}

// HandleToolCall 处理工具调用并返回结果字符串。
// 对齐 Python: async def handle_tool_call(self, tool_name, args) -> str
func (p *AgentArtsProvider) HandleToolCall(ctx context.Context, toolName string, args map[string]any) (string, error) {
	// 对齐 Python: if tool_name != "external_memory_search" → {"error": "Unknown tool: ..."}
	if toolName != "external_memory_search" {
		b, _ := json.Marshal(map[string]string{"error": fmt.Sprintf("Unknown tool: %s", toolName)})
		return string(b), nil
	}

	// 对齐 Python: query = args.get("query", "")
	query, _ := args["query"].(string)
	if query == "" {
		// 对齐 Python: return json.dumps({"error": "Missing required parameter: query"})
		b, _ := json.Marshal(map[string]string{"error": "Missing required parameter: query"})
		return string(b), nil
	}

	items, err := p.search(ctx, query, args)
	if err != nil {
		p.recordFailure()
		// 对齐 Python: logger.warning("[AgentArtsMemoryProvider] failed to search relevant memories", exc)
		logger.Warn(agentartsLogComponent).Err(err).Msg("[AgentArtsMemoryProvider] failed to search relevant memories")
		b, _ := json.Marshal(map[string]string{"error": err.Error()})
		return string(b), nil
	}

	p.recordSuccess()

	if len(items) == 0 {
		// 对齐 Python: return json.dumps({"result": "No relevant memories found.", "count": 0})
		b, _ := json.Marshal(map[string]any{"result": "No relevant memories found.", "count": 0})
		return string(b), nil
	}

	// 对齐 Python: return json.dumps({"results": items, "count": len(items)})
	payload := make([]map[string]any, len(items))
	for i, item := range items {
		payload[i] = map[string]any{"memory": item.Memory, "score": item.Score}
	}
	b, _ := json.Marshal(map[string]any{"results": payload, "count": len(payload)})
	return string(b), nil
}

// SyncTurn 同步一轮对话到外部记忆。
// 对齐 Python: async def sync_turn(self, user_msg, assistant_msg, **kwargs) -> None
// 错误被静默吞掉（对齐 Python: except Exception → logger.warning + self._consecutive_failures += 1）
func (p *AgentArtsProvider) SyncTurn(ctx context.Context, userMsg, assistantMsg string, opts ...ProviderOption) error {
	// 对齐 Python: if not user_msg or not assistant_msg: return
	if userMsg == "" || assistantMsg == "" {
		return nil
	}

	po := applyOptions(opts...)
	// 对齐 Python: actor_id = self._runtime_actor_id(kwargs)
	actorID := po.UserID
	if actorID == "" {
		p.mu.RLock()
		actorID = p.actorID
		p.mu.RUnlock()
	}
	// 对齐 Python: assistant_id = self._runtime_assistant_id(kwargs)
	assistantID := po.ScopeID
	if assistantID == "" {
		p.mu.RLock()
		assistantID = p.assistantID
		p.mu.RUnlock()
	}

	sessionID := po.SessionID
	if sessionID == "" {
		p.mu.RLock()
		sessionID = p.sessionID
		p.mu.RUnlock()
	}

	// 对齐 Python: logger.info("[AgentArtsMemoryProvider] sync_turn with params: %s", json.dumps(kwargs))
	logger.Info(agentartsLogComponent).
		Str("user_id", actorID).
		Str("assistant_id", assistantID).
		Str("session_id", sessionID).
		Msg("[AgentArtsMemoryProvider] sync_turn with params")

	// 对齐 Python: await self._ensure_memory_session(kwargs.get("session_id"), ...)
	memorySessionID, err := p.ensureMemorySession(ctx, sessionID, actorID, assistantID)
	if err != nil {
		p.recordFailure()
		// 对齐 Python: 吞掉错误
		logger.Warn(agentartsLogComponent).Err(err).Msg("[AgentArtsMemoryProvider] SyncTurn ensureMemorySession 失败")
		return nil
	}

	// 对齐 Python: await asyncio.to_thread(client.add_messages, ...)
	client := p.getClient()
	msgs := []textMessage{
		buildTextMessage("user", userMsg, actorID, assistantID),
		buildTextMessage("assistant", assistantMsg, actorID, assistantID),
	}
	if err := client.addMessages(ctx, p.spaceID, memorySessionID, msgs); err != nil {
		p.recordFailure()
		// 对齐 Python: logger.warning("AgentArts sync failed: %s", exc)
		logger.Warn(agentartsLogComponent).Err(err).Msg("[AgentArtsMemoryProvider] AgentArts sync failed")
		return nil // 对齐 Python: 吞掉错误
	}

	p.recordSuccess()
	return nil
}

// Shutdown 关闭 Provider 释放资源。
// 对齐 Python: async def shutdown(self) -> None
// Python: self._client = None; self._initialized = False
func (p *AgentArtsProvider) Shutdown(_ context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.clientOnce = sync.Once{}
	p.client = nil
	p.initialized = false
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// getClient 延迟初始化并返回 AgentArts HTTP 客户端。
// 对齐 Python: _get_client() — 懒加载模式
func (p *AgentArtsProvider) getClient() *agentartsClient {
	p.clientOnce.Do(func() {
		if p.client == nil {
			p.client = newAgentArtsClient(p.baseURL, p.apiKey)
		}
	})
	return p.client
}

// ensureMemorySession 确保记忆会话存在，返回 memory_session_id。
// 对齐 Python: _ensure_memory_session(session_id, actor_id=..., assistant_id=...)
func (p *AgentArtsProvider) ensureMemorySession(ctx context.Context, sessionID, actorID, assistantID string) (string, error) {
	// 对齐 Python: session_id = session_id or self._session_id
	if sessionID == "" {
		p.mu.RLock()
		sessionID = p.sessionID
		p.mu.RUnlock()
	}
	// 对齐 Python: if not session_id: raise RuntimeError("`session_id` is required")
	if sessionID == "" {
		return "", fmt.Errorf("session_id is required")
	}

	// 对齐 Python: actor_id = actor_id or self._actor_id or self._default_actor_id
	if actorID == "" {
		actorID = p.actorID
	}
	if actorID == "" {
		actorID = p.defaultActorID
	}
	// 对齐 Python: assistant_id = assistant_id or self._assistant_id or self._default_assistant_id
	if assistantID == "" {
		assistantID = p.assistantID
	}
	if assistantID == "" {
		assistantID = p.defaultAssistantID
	}

	mappingKey := p.sessionMappingKey(sessionID)

	// 对齐 Python: memory_session_id = await self._session_mapping_store.get(mapping_key)
	// 对齐 Python: memory_session_id = self._normalize_memory_session_id(...)
	existing, err := p.sessionMappingStore.Get(ctx, mappingKey)
	if err != nil {
		logger.Debug(agentartsLogComponent).Err(err).Str("key", mappingKey).Msg("[AgentArtsMemoryProvider] 读取 session 映射失败")
	}
	if len(existing) > 0 {
		memorySessionID := string(existing)
		// 对齐 Python: logger.info("[AgentArtsMemoryProvider] use exist session mapping entry: %s -> %s", ...)
		logger.Info(agentartsLogComponent).
			Str("session_id", sessionID).
			Str("memory_session_id", memorySessionID).
			Msg("[AgentArtsMemoryProvider] use exist session mapping entry")
		return memorySessionID, nil
	}

	// 对齐 Python: client.create_memory_session(space_id=..., actor_id=..., assistant_id=...)
	client := p.getClient()
	req := sessionCreateRequest{
		ActorID:     actorID,
		AssistantID: assistantID,
	}
	info, err := client.createMemorySession(ctx, p.spaceID, req)
	if err != nil {
		return "", fmt.Errorf("create_memory_session 失败: %w", err)
	}
	// 对齐 Python: if not memory_session_id: raise RuntimeError("AgentArts create_memory_session did not return a session id")
	if info.ID == "" {
		return "", fmt.Errorf("AgentArts create_memory_session did not return a session id")
	}

	// 对齐 Python: logger.info("[AgentArtsMemoryProvider] add session mapping entry: %s -> %s", ...)
	logger.Info(agentartsLogComponent).
		Str("session_id", sessionID).
		Str("memory_session_id", info.ID).
		Msg("[AgentArtsMemoryProvider] add session mapping entry")

	// 对齐 Python: await self._session_mapping_store.set(mapping_key, memory_session_id)
	if err := p.sessionMappingStore.Set(ctx, mappingKey, []byte(info.ID)); err != nil {
		logger.Warn(agentartsLogComponent).Err(err).Msg("[AgentArtsMemoryProvider] 存储 session 映射失败")
	}

	return info.ID, nil
}

// search 执行记忆搜索并返回归一化结果。
// 对齐 Python: _search(query, args) -> list[dict]
func (p *AgentArtsProvider) search(ctx context.Context, query string, args map[string]any) ([]normalizedMemoryItem, error) {
	client := p.getClient()

	// 对齐 Python: _memory_search_filter(query, args) → 构建 MemorySearchFilter
	filter := memorySearchFilter{
		Query: query,
	}

	// 对齐 Python: actor_id = self._runtime_actor_id(args)
	actorID := p.runtimeActorID(args)
	if actorID != "" {
		filter.ActorID = actorID
	}

	// 对齐 Python: top_k = args.get("top_k"); if top_k is None: top_k = _DEFAULT_TOP_K
	topK := defaultTopK
	if v, ok := args["top_k"]; ok {
		switch n := v.(type) {
		case float64:
			topK = int(n)
		case int:
			topK = n
		}
	}
	// 对齐 Python: min(int(top_k), _MAX_TOP_K)
	if topK > maxTopK {
		topK = maxTopK
	}
	filter.TopK = topK

	// 对齐 Python: min_score = _DEFAULT_MIN_SCORE if args.get("min_score") is None else args["min_score"]
	minScore := defaultMinScore
	if v, ok := args["min_score"]; ok {
		switch n := v.(type) {
		case float64:
			minScore = n
		case int:
			minScore = float64(n)
		}
	}
	filter.MinScore = minScore

	// 对齐 Python: if args.get("strategy_type") is not None: filter_kwargs["strategy_type"] = args["strategy_type"]
	if v, ok := args["strategy_type"].(string); ok && v != "" {
		filter.StrategyType = v
	}

	// 对齐 Python: logger.info("[AgentArtsMemoryProvider] search agentarts memory space [%s] with filters: %s", ...)
	logger.Info(agentartsLogComponent).
		Str("space_id", p.spaceID).
		Str("query", query).
		Int("top_k", topK).
		Float64("min_score", minScore).
		Str("strategy_type", filter.StrategyType).
		Msg("[AgentArtsMemoryProvider] search agentarts memory space")

	items, err := client.searchMemories(ctx, p.spaceID, filter)
	if err != nil {
		return nil, err
	}

	// 对齐 Python: logger.info("[AgentArtsMemoryProvider] found %d relevant memory records", len(items))
	logger.Info(agentartsLogComponent).
		Int("count", len(items)).
		Msg("[AgentArtsMemoryProvider] found relevant memory records")

	return items, nil
}

// runtimeActorID 解析运行时 actor_id。
// 对齐 Python: _runtime_actor_id(params) — 优先 params.user_id → self._actor_id → self._default_actor_id
func (p *AgentArtsProvider) runtimeActorID(args map[string]any) string {
	// 对齐 Python: user_id = params.get("user_id"); if user_id: return str(user_id)
	if v, ok := args["user_id"].(string); ok && v != "" {
		return v
	}
	p.mu.RLock()
	actorID := p.actorID
	p.mu.RUnlock()
	if actorID != "" {
		return actorID
	}
	return p.defaultActorID
}

// runtimeAssistantID 解析运行时 assistant_id。
// 对齐 Python: _runtime_assistant_id(params) — 优先 params.assistant_id → params.scope_id → self._assistant_id → self._default_assistant_id
func (p *AgentArtsProvider) runtimeAssistantID(args map[string]any) string {
	// 对齐 Python: assistant_id = params.get("assistant_id"); if assistant_id: return str(assistant_id)
	if v, ok := args["assistant_id"].(string); ok && v != "" {
		return v
	}
	// 对齐 Python: scope_id = params.get("scope_id"); if scope_id: return str(scope_id)
	if v, ok := args["scope_id"].(string); ok && v != "" {
		return v
	}
	p.mu.RLock()
	assistantID := p.assistantID
	p.mu.RUnlock()
	if assistantID != "" {
		return assistantID
	}
	return p.defaultAssistantID
}

// sessionMappingKey 生成 session 映射 KV key。
// 对齐 Python: _session_mapping_key(session_id) -> f"{_SESSION_MAPPING_KEY_PREFIX}/{session_id}"
func (p *AgentArtsProvider) sessionMappingKey(sessionID string) string {
	return sessionMappingKeyPrefix + "/" + sessionID
}

// recordSuccess 记录成功，重置连续失败计数。
// 对齐 Python: self._consecutive_failures = 0
func (p *AgentArtsProvider) recordSuccess() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.consecutiveFailures = 0
}

// recordFailure 记录失败，递增连续失败计数。
// 对齐 Python: self._consecutive_failures += 1
func (p *AgentArtsProvider) recordFailure() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.consecutiveFailures++
}
