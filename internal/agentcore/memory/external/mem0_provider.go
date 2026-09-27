package external

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// Mem0Provider Mem0 外部记忆提供者。
// 对齐 Python: Mem0MemoryProvider (openjiuwen/core/memory/external/mem0_provider.py)
//
// 将 Mem0 云端 REST API 适配为 MemoryProvider 接口。
// 内置熔断器（对齐 Python _BREAKER_THRESHOLD / _BREAKER_COOLDOWN_SECS），
// ExternalMemoryRail 层熔断器作为外层防线。
type Mem0Provider struct {
	BaseMemoryProvider

	// apiKey API 密钥
	apiKey string
	// userID 用户标识
	userID string
	// agentID Agent 标识
	agentID string
	// rerank 是否启用重排序
	rerank bool

	// client Mem0 HTTP 客户端（延迟初始化，对齐 Python _get_client 懒加载）
	client *mem0HTTPClient
	// initialized 是否已初始化
	initialized bool
	// mu 保护 client 延迟初始化
	mu sync.Mutex

	// consecutiveFailures 连续失败次数
	// 对齐 Python: _consecutive_failures
	consecutiveFailures int
	// breakerOpenUntil 熔断器冷却截止时间
	// 对齐 Python: _breaker_open_until
	breakerOpenUntil time.Time

	// prefetchCancel 取消后台 prefetch goroutine
	// 对齐 Python: _prefetch_task (asyncio.Task)
	prefetchCancel context.CancelFunc
	// prefetchWg 等待后台 prefetch goroutine 退出
	prefetchWg sync.WaitGroup
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// mem0BreakerThreshold 熔断器连续失败阈值
	// 对齐 Python: _BREAKER_THRESHOLD = 5
	mem0BreakerThreshold = 5
	// mem0BreakerCooldownSecs 熔断器冷却时间（秒）
	// 对齐 Python: _BREAKER_COOLDOWN_SECS = 120.0
	mem0BreakerCooldownSecs = 120.0
	// mem0ShutdownWaitTimeout Shutdown 等待 prefetch goroutine 退出的超时
	// 对齐 Python: asyncio.wait_for(..., timeout=2.0)
	mem0ShutdownWaitTimeout = 2 * time.Second
)

var mem0LogComponent = logger.ComponentAgentCore

// mem0ProfileSchema mem0_profile 工具 Schema
// 对齐 Python: PROFILE_SCHEMA
var mem0ProfileSchema = ToolSchema{
	Name: "mem0_profile",
	Description: "Retrieve all stored memories about the user — preferences, facts, " +
		"project context. Fast, no reranking. Use at conversation start.",
	Parameters: map[string]any{
		"type":       "object",
		"properties": map[string]any{},
		"required":   []string{},
	},
}

// mem0SearchSchema mem0_search 工具 Schema
// 对齐 Python: SEARCH_SCHEMA
var mem0SearchSchema = ToolSchema{
	Name: "mem0_search",
	Description: "Search memories by meaning. Returns relevant facts ranked by similarity. " +
		"Set rerank=true for higher accuracy on important queries.",
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query":  map[string]any{"type": "string", "description": "What to search for."},
			"rerank": map[string]any{"type": "boolean", "description": "Enable reranking for precision (default: false)."},
			"top_k":  map[string]any{"type": "integer", "description": "Max results (default: 10, max: 50)."},
		},
		"required": []string{"query"},
	},
}

// mem0ConcludeSchema mem0_conclude 工具 Schema
// 对齐 Python: CONCLUDE_SCHEMA
var mem0ConcludeSchema = ToolSchema{
	Name: "mem0_conclude",
	Description: "Store a durable fact about the user. Stored verbatim (no LLM extraction). " +
		"Use for explicit preferences, corrections, or decisions.",
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"conclusion": map[string]any{"type": "string", "description": "The fact to store."},
		},
		"required": []string{"conclusion"},
	},
}

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewMem0Provider 创建 Mem0Provider 实例。
// 对齐 Python: Mem0MemoryProvider.__init__(api_key=..., user_id=..., agent_id=..., rerank=...)
func NewMem0Provider(apiKey, userID, agentID string, rerank bool) *Mem0Provider {
	return &Mem0Provider{
		apiKey:   apiKey,
		userID:   userID,
		agentID:  agentID,
		rerank:   rerank,
	}
}

// Name 返回 Provider 唯一名称。
// Python: @property name -> "mem0"
func (p *Mem0Provider) Name() string {
	return "mem0"
}

// IsAvailable 检查 Provider 是否已配置且就绪。
// Python: is_available() -> bool(self._api_key)
func (p *Mem0Provider) IsAvailable() bool {
	return p.apiKey != ""
}

// IsInitialized 返回 Provider 是否已初始化。
// Python: @property is_initialized -> self._initialized
// 覆盖 BaseMemoryProvider 的默认值（false）。
func (p *Mem0Provider) IsInitialized() bool {
	return p.initialized
}

// GetToolSchemas 返回 Provider 提供的工具 Schema 列表。
// Python: get_tool_schemas() -> [PROFILE_SCHEMA, SEARCH_SCHEMA, CONCLUDE_SCHEMA]
func (p *Mem0Provider) GetToolSchemas() []ToolSchema {
	return []ToolSchema{mem0ProfileSchema, mem0SearchSchema, mem0ConcludeSchema}
}

// SystemPromptBlock 返回 Provider 的系统提示词引导块。
// Python: system_prompt_block() -> "# Mem0 Memory\nActive. User: {self._user_id}.\n..."
func (p *Mem0Provider) SystemPromptBlock() string {
	return "# Mem0 Memory\n" +
		fmt.Sprintf("Active. User: %s.\n", p.userID) +
		"Use mem0_search to find memories, mem0_conclude to store facts, " +
		"mem0_profile for a full overview."
}

// Initialize 初始化 Provider。
// 对齐 Python: async def initialize(self, **kwargs) -> None
func (p *Mem0Provider) Initialize(_ context.Context, opts ...ProviderOption) error {
	po := applyOptions(opts...)

	// 对齐 Python: kwargs.get("api_key") or self._api_key
	if po.UserID != "" {
		p.userID = po.UserID
	}
	if po.ScopeID != "" {
		p.agentID = po.ScopeID // ScopeID 对应 Python 的 agent_id
	}

	if p.apiKey == "" {
		return fmt.Errorf("Mem0 API key is required. Provide api_key in provider initialization")
	}

	p.mu.Lock()
	p.client = newMem0HTTPClient(p.apiKey, "")
	p.initialized = true
	p.mu.Unlock()

	return nil
}

// Prefetch 根据查询预取记忆上下文。
// 对齐 Python: async def prefetch(self, query, **kwargs) -> str
func (p *Mem0Provider) Prefetch(ctx context.Context, query string, opts ...ProviderOption) (string, error) {
	if query == "" || p.isBreakerOpen() {
		return "", nil
	}

	topK := 5
	rerank := p.rerank

	client := p.getClient()
	response, err := client.search(ctx, query, p.readFilters(), rerank, topK)
	if err != nil {
		p.recordFailure()
		logger.Debug(mem0LogComponent).Err(err).Msg("Mem0 prefetch 失败")
		return "", nil
	}

	if len(response) == 0 {
		p.recordSuccess()
		return "", nil
	}

	lines := make([]string, 0, len(response))
	for _, item := range response {
		if item.Memory != "" {
			lines = append(lines, "- "+item.Memory)
		}
	}

	p.recordSuccess()
	if len(lines) == 0 {
		return "", nil
	}
	result := "## Mem0 Memory\n" + joinLines(lines)
	return result, nil
}

// QueuePrefetch 后台预热查询。
// 对齐 Python: async def queue_prefetch(self, query, **kwargs) -> None
// 使用 goroutine + context cancel 对齐 Python asyncio.create_task
func (p *Mem0Provider) QueuePrefetch(ctx context.Context, query string, opts ...ProviderOption) {
	if query == "" || p.isBreakerOpen() {
		return
	}

	// 取消上一次 prefetch goroutine，对齐 Python: self._prefetch_task.cancel()
	if p.prefetchCancel != nil {
		p.prefetchCancel()
	}

	topK := 5
	ctx, cancel := context.WithCancel(ctx)
	p.prefetchCancel = cancel

	p.prefetchWg.Add(1)
	go func() {
		defer p.prefetchWg.Done()
		client := p.getClient()
		_, err := client.search(ctx, query, p.readFilters(), p.rerank, topK)
		if err != nil {
			// context.Canceled 是正常取消，不算失败
			if ctx.Err() != nil {
				return
			}
			p.recordFailure()
			logger.Debug(mem0LogComponent).Err(err).Msg("Mem0 queue_prefetch 失败")
			return
		}
		p.recordSuccess()
	}()
}

// SyncTurn 同步一轮对话到外部记忆。
// 对齐 Python: async def sync_turn(self, user_msg, assistant_msg, **kwargs) -> None
func (p *Mem0Provider) SyncTurn(ctx context.Context, userMsg, assistantMsg string, _ ...ProviderOption) error {
	if p.isBreakerOpen() || userMsg == "" || assistantMsg == "" {
		return nil
	}

	messages := []mem0Message{
		{Role: "user", Content: userMsg},
		{Role: "assistant", Content: assistantMsg},
	}

	client := p.getClient()
	err := client.add(ctx, messages, p.writeFilters(), nil)
	if err != nil {
		p.recordFailure()
		logger.Warn(mem0LogComponent).Err(err).Msg("Mem0 sync_turn 失败")
		return err
	}

	p.recordSuccess()
	return nil
}

// HandleToolCall 处理工具调用并返回结果字符串。
// 对齐 Python: async def handle_tool_call(self, tool_name, args) -> str
func (p *Mem0Provider) HandleToolCall(ctx context.Context, toolName string, args map[string]any) (string, error) {
	if p.isBreakerOpen() {
		b, _ := json.Marshal(map[string]string{
			"error": "Mem0 API temporarily unavailable (multiple consecutive failures). Will retry automatically.",
		})
		return string(b), nil
	}

	client := p.getClient()

	switch toolName {
	case "mem0_profile":
		return p.handleProfile(ctx, client)
	case "mem0_search":
		return p.handleSearch(ctx, client, args)
	case "mem0_conclude":
		return p.handleConclude(ctx, client, args)
	default:
		b, _ := json.Marshal(map[string]string{"error": fmt.Sprintf("Unknown tool: %s", toolName)})
		return string(b), nil
	}
}

// Shutdown 关闭 Provider 释放资源。
// 对齐 Python: async def shutdown(self) -> None
func (p *Mem0Provider) Shutdown(_ context.Context) error {
	// 取消 prefetch goroutine，对齐 Python: self._prefetch_task.cancel()
	if p.prefetchCancel != nil {
		p.prefetchCancel()
	}

	// 等待 goroutine 退出，带超时，对齐 Python: asyncio.wait_for(..., timeout=2.0)
	done := make(chan struct{})
	go func() {
		p.prefetchWg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(mem0ShutdownWaitTimeout):
		// 超时后继续清理，对齐 Python: except TimeoutError -> pass
	}

	p.prefetchCancel = nil
	p.mu.Lock()
	p.client = nil
	p.initialized = false
	p.mu.Unlock()

	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// getClient 延迟初始化并返回 Mem0 HTTP 客户端。
// 对齐 Python: _get_client() — 懒加载模式
func (p *Mem0Provider) getClient() *mem0HTTPClient {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.client != nil {
		return p.client
	}
	p.client = newMem0HTTPClient(p.apiKey, "")
	return p.client
}

// readFilters 构造读操作的 filters。
// 对齐 Python: _read_filters() -> {"user_id": ..., "agent_id": ...(可选)}
func (p *Mem0Provider) readFilters() map[string]any {
	filters := map[string]any{"user_id": p.userID}
	if p.agentID != "" {
		filters["agent_id"] = p.agentID
	}
	return filters
}

// writeFilters 构造写操作的 filters。
// 对齐 Python: _write_filters() -> {"user_id": ..., "agent_id": ...}
func (p *Mem0Provider) writeFilters() map[string]any {
	return map[string]any{
		"user_id":  p.userID,
		"agent_id": p.agentID,
	}
}

// isBreakerOpen 检查熔断器是否处于开启状态。
// 对齐 Python: _is_breaker_open()
func (p *Mem0Provider) isBreakerOpen() bool {
	if p.consecutiveFailures < mem0BreakerThreshold {
		return false
	}
	if time.Now().After(p.breakerOpenUntil) {
		p.consecutiveFailures = 0
		return false
	}
	return true
}

// recordSuccess 记录成功，重置连续失败计数。
// 对齐 Python: _record_success()
func (p *Mem0Provider) recordSuccess() {
	p.consecutiveFailures = 0
}

// recordFailure 记录失败，递增连续失败计数，达到阈值时开启熔断器。
// 对齐 Python: _record_failure()
func (p *Mem0Provider) recordFailure() {
	p.consecutiveFailures++
	if p.consecutiveFailures >= mem0BreakerThreshold {
		p.breakerOpenUntil = time.Now().Add(time.Duration(mem0BreakerCooldownSecs * float64(time.Second)))
		logger.Warn(mem0LogComponent).
			Int("consecutive_failures", p.consecutiveFailures).
			Float64("cooldown_secs", mem0BreakerCooldownSecs).
			Msg("[Mem0Provider] 熔断器开启")
	}
}

// handleProfile 处理 mem0_profile 工具调用。
// 对齐 Python: handle_tool_call 中 tool_name == "mem0_profile" 分支
func (p *Mem0Provider) handleProfile(ctx context.Context, client *mem0HTTPClient) (string, error) {
	response, err := client.getAll(ctx, p.readFilters())
	if err != nil {
		p.recordFailure()
		b, _ := json.Marshal(map[string]string{"error": err.Error()})
		return string(b), nil
	}

	p.recordSuccess()

	if len(response) == 0 {
		b, _ := json.Marshal(map[string]string{"result": "No memories stored yet."})
		return string(b), nil
	}

	lines := make([]string, 0, len(response))
	for _, item := range response {
		if item.Memory != "" {
			lines = append(lines, item.Memory)
		}
	}

	b, _ := json.Marshal(map[string]any{
		"result": joinLines(lines),
		"count":  len(lines),
	})
	return string(b), nil
}

// handleSearch 处理 mem0_search 工具调用。
// 对齐 Python: handle_tool_call 中 tool_name == "mem0_search" 分支
func (p *Mem0Provider) handleSearch(ctx context.Context, client *mem0HTTPClient, args map[string]any) (string, error) {
	query, _ := args["query"].(string)
	if query == "" {
		b, _ := json.Marshal(map[string]string{"error": "Missing required parameter: query"})
		return string(b), nil
	}

	topK := 10
	if v, ok := args["top_k"]; ok {
		switch n := v.(type) {
		case float64:
			topK = int(n)
		case int:
			topK = n
		}
	}
	if topK > 50 {
		topK = 50
	}

	rerank := false
	if v, ok := args["rerank"]; ok {
		rerank, _ = v.(bool)
	}

	response, err := client.search(ctx, query, p.readFilters(), rerank, topK)
	if err != nil {
		p.recordFailure()
		b, _ := json.Marshal(map[string]string{"error": err.Error()})
		return string(b), nil
	}

	p.recordSuccess()

	if len(response) == 0 {
		b, _ := json.Marshal(map[string]string{"result": "No relevant memories found."})
		return string(b), nil
	}

	b, _ := json.Marshal(map[string]any{
		"results": response,
		"count":   len(response),
	})
	return string(b), nil
}

// handleConclude 处理 mem0_conclude 工具调用。
// 对齐 Python: handle_tool_call 中 tool_name == "mem0_conclude" 分支
func (p *Mem0Provider) handleConclude(ctx context.Context, client *mem0HTTPClient, args map[string]any) (string, error) {
	conclusion, _ := args["conclusion"].(string)
	if conclusion == "" {
		b, _ := json.Marshal(map[string]string{"error": "Missing required parameter: conclusion"})
		return string(b), nil
	}

	inferFalse := false
	messages := []mem0Message{{Role: "user", Content: conclusion}}
	err := client.add(ctx, messages, p.writeFilters(), &inferFalse)
	if err != nil {
		p.recordFailure()
		b, _ := json.Marshal(map[string]string{"error": err.Error()})
		return string(b), nil
	}

	p.recordSuccess()
	b, _ := json.Marshal(map[string]string{"result": "Fact stored."})
	return string(b), nil
}

// joinLines 拼接多行文本。
func joinLines(lines []string) string {
	result := ""
	for i, line := range lines {
		if i > 0 {
			result += "\n"
		}
		result += line
	}
	return result
}
