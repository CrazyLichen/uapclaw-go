package external

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// OpenVikingProvider OpenViking 外部记忆提供者。
// 对齐 Python: OpenVikingMemoryProvider (openjiuwen/core/memory/external/openviking_memory_provider.py)
//
// 适配 OpenViking 上下文数据库 REST API，提供 5 个双向记忆工具和会话型记忆同步。
// 与 Mem0Provider 不同，OpenViking 是会话型的：sync_turn 记录对话轮次，on_session_end 时 commit session。
// 不内建熔断器，依赖 ExternalMemoryRail 外层熔断器（对齐 Python 无内建熔断器）。
type OpenVikingProvider struct {
	BaseMemoryProvider
	// endpoint API 地址
	endpoint string
	// apiKey API 密钥
	apiKey string
	// account 账户标识
	account string
	// user 用户标识
	user string
	// agent Agent 标识
	agent string
	// client OpenViking HTTP 客户端（Initialize 中创建）
	client *vikingClient
	// sessionID 会话标识
	sessionID string
	// initialized 是否已初始化
	initialized bool
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

var vikingLogComponent = logger.ComponentAgentCore

// vikingSearchSchema viking_search 工具 Schema
// 对齐 Python: VIKING_SEARCH_SCHEMA
var vikingSearchSchema = ToolSchema{
	Name:        "viking_search",
	Description: "在知识库中进行全域搜索.",
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{"type": "string", "description": "搜索查询词."},
			"mode": map[string]any{
				"type":        "string",
				"enum":        []string{"auto", "fast", "deep"},
				"description": "搜索模式（默认：auto）.",
			},
			"top_k": map[string]any{"type": "integer", "description": "最大返回结果数（默认10）."},
		},
		"required": []string{"query"},
	},
}

// vikingReadSchema viking_read 工具 Schema
// 对齐 Python: VIKING_READ_SCHEMA
var vikingReadSchema = ToolSchema{
	Name:        "viking_read",
	Description: "读取 viking:// URI 上的内容.",
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"uri": map[string]any{"type": "string", "description": "要读取的 viking:// URI."},
			"detail": map[string]any{
				"type":        "string",
				"enum":        []string{"abstract", "overview", "full"},
				"description": "详情级别（默认：overview）.",
			},
		},
		"required": []string{"uri"},
	},
}

// vikingBrowseSchema viking_browse 工具 Schema
// 对齐 Python: VIKING_BROWSE_SCHEMA
var vikingBrowseSchema = ToolSchema{
	Name:        "viking_browse",
	Description: "浏览知识库结构.",
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        []string{"list", "tree", "stat"},
				"description": "浏览操作.",
			},
			"path": map[string]any{"type": "string", "description": "浏览路径（默认：/）."},
		},
		"required": []string{"action"},
	},
}

// vikingRememberSchema viking_remember 工具 Schema
// 对齐 Python: VIKING_REMEMBER_SCHEMA
var vikingRememberSchema = ToolSchema{
	Name:        "viking_remember",
	Description: "显式存储一个事实或偏好.",
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"content": map[string]any{"type": "string", "description": "要记住的事实."},
			"category": map[string]any{
				"type":        "string",
				"enum":        []string{"preference", "entity", "event", "case", "pattern"},
				"description": "记忆类别.",
			},
		},
		"required": []string{"content"},
	},
}

// vikingAddResourceSchema viking_add_resource 工具 Schema
// 对齐 Python: VIKING_ADD_RESOURCE_SCHEMA
var vikingAddResourceSchema = ToolSchema{
	Name:        "viking_add_resource",
	Description: "索引一个 URL 或文档以供后续搜索.",
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"url":   map[string]any{"type": "string", "description": "要索引的 URL 或文件路径."},
			"title": map[string]any{"type": "string", "description": "可选标题."},
		},
		"required": []string{"url"},
	},
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewOpenVikingProvider 创建 OpenVikingProvider 实例。
// 对齐 Python: OpenVikingMemoryProvider.__init__(endpoint, api_key, account, user, agent)
//
// 参数为空时从环境变量 fallback：
//   - endpoint → OPENVIKING_ENDPOINT
//   - apiKey → OPENVIKING_API_KEY
//   - account → OPENVIKING_ACCOUNT（默认 "default"）
//   - user → OPENVIKING_USER（默认 "default"）
//   - agent → OPENVIKING_AGENT（默认 "hermes"）
func NewOpenVikingProvider(endpoint, apiKey, account, user, agent string) *OpenVikingProvider {
	// 对齐 Python: self._endpoint = endpoint or os.environ.get("OPENVIKING_ENDPOINT", "")
	if endpoint == "" {
		endpoint = os.Getenv("OPENVIKING_ENDPOINT")
	}
	if apiKey == "" {
		apiKey = os.Getenv("OPENVIKING_API_KEY")
	}
	// 对齐 Python: self._account = account or os.environ.get("OPENVIKING_ACCOUNT", "default")
	if account == "" {
		if envVal := os.Getenv("OPENVIKING_ACCOUNT"); envVal != "" {
			account = envVal
		} else {
			account = "default"
		}
	}
	if user == "" {
		if envVal := os.Getenv("OPENVIKING_USER"); envVal != "" {
			user = envVal
		} else {
			user = "default"
		}
	}
	if agent == "" {
		if envVal := os.Getenv("OPENVIKING_AGENT"); envVal != "" {
			agent = envVal
		} else {
			agent = "hermes"
		}
	}
	return &OpenVikingProvider{
		endpoint: endpoint,
		apiKey:   apiKey,
		account:  account,
		user:     user,
		agent:    agent,
	}
}

// Name 返回 Provider 唯一名称。
// Python: @property name -> "openviking"
func (p *OpenVikingProvider) Name() string {
	return "openviking"
}

// IsAvailable 检查 Provider 是否已配置且就绪。
// Python: is_available() -> bool(self._endpoint)
func (p *OpenVikingProvider) IsAvailable() bool {
	return p.endpoint != ""
}

// IsInitialized 返回 Provider 是否已初始化。
// Python: @property is_initialized -> self._client is not None
func (p *OpenVikingProvider) IsInitialized() bool {
	return p.initialized
}

// GetToolSchemas 返回 Provider 提供的工具 Schema 列表。
// Python: get_tool_schemas() -> [VIKING_SEARCH_SCHEMA, ...]
func (p *OpenVikingProvider) GetToolSchemas() []ToolSchema {
	return []ToolSchema{
		vikingSearchSchema,
		vikingReadSchema,
		vikingBrowseSchema,
		vikingRememberSchema,
		vikingAddResourceSchema,
	}
}

// SystemPromptBlock 返回 Provider 的系统提示词引导块。
// Python: system_prompt_block()
func (p *OpenVikingProvider) SystemPromptBlock() string {
	return "# OpenViking Memory\n\n" +
		"Use `viking_search` to find knowledge (modes: auto/fast/deep).\n" +
		"Use `viking_read` to read content at a viking:// URI (levels: abstract/overview/full).\n" +
		"Use `viking_browse` to navigate the knowledge structure.\n" +
		"Use `viking_remember` to explicitly store facts.\n" +
		"Use `viking_add_resource` to index URLs/documents."
}

// Initialize 初始化 Provider。
// 对齐 Python: async def initialize(self, **kwargs) -> None
func (p *OpenVikingProvider) Initialize(ctx context.Context, opts ...ProviderOption) error {
	po := applyOptions(opts...)

	// 对齐 Python: self._session_id = kwargs.get("session_id", "")
	if po.SessionID != "" {
		p.sessionID = po.SessionID
	}

	// 对齐 Python: self._client = _VikingClient(self._endpoint, self._api_key, ...)
	p.client = newVikingClient(p.endpoint, p.apiKey, p.account, p.user, p.agent)

	// 对齐 Python: healthy = await asyncio.to_thread(self._client.health)
	healthy := p.client.health(ctx)
	if !healthy {
		// 对齐 Python: logger.warning("OpenViking at %s not reachable", self._endpoint)
		logger.Warn(vikingLogComponent).
			Str("endpoint", p.endpoint).
			Msg("OpenViking 不可达")
		p.client = nil
		p.initialized = false
		return nil
	}

	p.initialized = true
	return nil
}

// Prefetch 根据查询预取记忆上下文。
// 对齐 Python: async def prefetch(self, query, **kwargs) -> str
func (p *OpenVikingProvider) Prefetch(ctx context.Context, query string, _ ...ProviderOption) (string, error) {
	// 对齐 Python: if not self._client or not query: return ""
	if p.client == nil || query == "" {
		return "", nil
	}

	// 对齐 Python: resp = await asyncio.to_thread(self._client.post, "/api/v1/search/find", {"query": query, "top_k": 5})
	resp, err := p.client.post(ctx, "/api/v1/search/find", map[string]any{
		"query": query,
		"top_k": 5,
	})
	if err != nil {
		// 对齐 Python: logger.debug("OpenViking prefetch failed: %s", e)
		logger.Debug(vikingLogComponent).Err(err).Msg("OpenViking prefetch 失败")
		return "", nil
	}

	// 对齐 Python: result = resp.get("result", {})
	result, _ := resp["result"].(map[string]any)
	if result == nil {
		return "", nil
	}

	// 对齐 Python: for ctx_type in ("memories", "resources"): items = result.get(ctx_type, [])  for item in items[:3]:
	parts := make([]string, 0)
	for _, ctxType := range []string{"memories", "resources"} {
		items, _ := result[ctxType].([]any)
		for i, item := range items {
			if i >= 3 {
				break
			}
			itemMap, _ := item.(map[string]any)
			if itemMap == nil {
				continue
			}
			uri, _ := itemMap["uri"].(string)
			abstract, _ := itemMap["abstract"].(string)
			score := floatVal(itemMap["score"])
			if abstract != "" {
				parts = append(parts, fmt.Sprintf("- [%.2f] %s (%s)", score, abstract, uri))
			}
		}
	}

	// 对齐 Python: if not parts: return ""
	if len(parts) == 0 {
		return "", nil
	}
	// 对齐 Python: return "## OpenViking Context\n" + "\n".join(parts)
	return "## OpenViking Context\n" + strings.Join(parts, "\n"), nil
}

// SyncTurn 同步一轮对话到外部记忆。
// 对齐 Python: async def sync_turn(self, user_msg, assistant_msg, **kwargs) -> None
func (p *OpenVikingProvider) SyncTurn(ctx context.Context, userMsg, assistantMsg string, opts ...ProviderOption) error {
	// 对齐 Python: if not self._client: return
	if p.client == nil {
		return nil
	}

	// 对齐 Python: sid = kwargs.get("session_id", self._session_id)
	sid := p.sessionID
	po := applyOptions(opts...)
	if po.SessionID != "" {
		sid = po.SessionID
	}

	// 对齐 Python: await asyncio.to_thread(self._client.post, f"/api/v1/sessions/{sid}/messages", {"role": "user", "content": user_msg[:4000]})
	_, err := p.client.post(ctx, fmt.Sprintf("/api/v1/sessions/%s/messages", sid), map[string]any{
		"role":    "user",
		"content": truncStr(userMsg, 4000),
	})
	if err != nil {
		// 对齐 Python: logger.debug("OpenViking sync failed: %s", e)
		logger.Debug(vikingLogComponent).Err(err).Msg("OpenViking sync_turn 失败")
		return nil
	}

	// 对齐 Python: await asyncio.to_thread(self._client.post, ..., {"role": "assistant", "content": assistant_msg[:4000]})
	_, err = p.client.post(ctx, fmt.Sprintf("/api/v1/sessions/%s/messages", sid), map[string]any{
		"role":    "assistant",
		"content": truncStr(assistantMsg, 4000),
	})
	if err != nil {
		logger.Debug(vikingLogComponent).Err(err).Msg("OpenViking sync_turn 失败")
		return nil
	}

	return nil
}

// HandleToolCall 处理工具调用并返回结果字符串。
// 对齐 Python: async def handle_tool_call(self, tool_name, args) -> str
func (p *OpenVikingProvider) HandleToolCall(ctx context.Context, toolName string, args map[string]any) (string, error) {
	// 对齐 Python: if not self._client: return json.dumps({"error": "OpenViking not connected"})
	if p.client == nil {
		b, _ := json.Marshal(map[string]string{"error": "OpenViking not connected"})
		return string(b), nil
	}

	// 对齐 Python: try: ... except Exception as e: return json.dumps({"error": str(e)})
	result, err := p.dispatchToolCall(ctx, toolName, args)
	if err != nil {
		b, _ := json.Marshal(map[string]string{"error": err.Error()})
		return string(b), nil
	}
	b, _ := json.Marshal(result)
	return string(b), nil
}

// OnSessionEnd 会话结束时回调。
// 对齐 Python: async def on_session_end(self, messages) -> None
func (p *OpenVikingProvider) OnSessionEnd(ctx context.Context, _ []map[string]any) error {
	// 对齐 Python: if not self._client or not self._session_id: return
	if p.client == nil || p.sessionID == "" {
		return nil
	}

	// 对齐 Python: await asyncio.to_thread(self._client.post, f"/api/v1/sessions/{self._session_id}/commit", {})
	_, err := p.client.post(ctx, fmt.Sprintf("/api/v1/sessions/%s/commit", p.sessionID), map[string]any{})
	if err != nil {
		// 对齐 Python: logger.debug("OpenViking session commit failed: %s", e)
		logger.Debug(vikingLogComponent).Err(err).Msg("OpenViking session commit 失败")
	}
	return nil
}

// Shutdown 关闭 Provider 释放资源。
// 对齐 Python: async def shutdown(self) -> None
func (p *OpenVikingProvider) Shutdown(_ context.Context) error {
	if p.client != nil {
		// 对齐 Python: self._client.close() — try/except 包裹
		func() {
			defer func() {
				if r := recover(); r != nil {
					// 对齐 Python: logger.debug("OpenViking client close failed: %s", e)
					logger.Debug(vikingLogComponent).
						Str("event_type", "viking_provider_shutdown").
						Msgf("关闭客户端失败: %v", r)
				}
			}()
			p.client.close()
		}()
		p.client = nil
	}
	p.initialized = false
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// dispatchToolCall 分发工具调用到对应的 handleXxx 方法。
// 对齐 Python: handle_tool_call 中的 if/elif 分支
func (p *OpenVikingProvider) dispatchToolCall(ctx context.Context, toolName string, args map[string]any) (any, error) {
	switch toolName {
	case "viking_search":
		return p.handleVikingSearch(ctx, args)
	case "viking_read":
		return p.handleVikingRead(ctx, args)
	case "viking_browse":
		return p.handleVikingBrowse(ctx, args)
	case "viking_remember":
		return p.handleVikingRemember(ctx, args)
	case "viking_add_resource":
		return p.handleVikingAddResource(ctx, args)
	default:
		return nil, fmt.Errorf("unknown tool: %s", toolName)
	}
}

// handleVikingSearch 处理 viking_search 工具调用。
// 对齐 Python: handle_tool_call 中 tool_name == "viking_search" 分支
func (p *OpenVikingProvider) handleVikingSearch(ctx context.Context, args map[string]any) (any, error) {
	// 对齐 Python: query = args.get("query", ""); if not query: return json.dumps({"error": "query is required"})
	query, _ := args["query"].(string)
	if query == "" {
		return nil, fmt.Errorf("query is required")
	}

	// 对齐 Python: payload = {"query": query}
	payload := map[string]any{"query": query}

	// 对齐 Python: mode = args.get("mode", "auto"); if mode != "auto": payload["mode"] = mode
	mode, _ := args["mode"].(string)
	if mode != "" && mode != "auto" {
		payload["mode"] = mode
	}

	// 对齐 Python: if args.get("scope"): payload["target_uri"] = args["scope"]
	if scope, _ := args["scope"].(string); scope != "" {
		payload["target_uri"] = scope
	}

	// 对齐 Python: if args.get("limit"): payload["top_k"] = args["limit"]
	if limit := floatVal(args["limit"]); limit > 0 {
		payload["top_k"] = int(limit)
	}

	// 对齐 Python: resp = await asyncio.to_thread(self._client.post, "/api/v1/search/find", payload)
	resp, err := p.client.post(ctx, "/api/v1/search/find", payload)
	if err != nil {
		return nil, err
	}

	// 对齐 Python: result = resp.get("result", {})
	resultMap, _ := resp["result"].(map[string]any)
	if resultMap == nil {
		return map[string]any{"results": []any{}, "total": 0}, nil
	}

	// 对齐 Python: for ctx_type in ("memories", "resources", "skills"): ...
	type scoredEntry struct {
		score float64
		entry map[string]any
	}
	var scoredEntries []scoredEntry

	for _, ctxType := range []string{"memories", "resources", "skills"} {
		items, _ := resultMap[ctxType].([]any)
		for _, item := range items {
			itemMap, _ := item.(map[string]any)
			if itemMap == nil {
				continue
			}
			// 对齐 Python: raw_score = item.get("score"); sort_score = raw_score if raw_score is not None else 0.0
			rawScore := floatVal(itemMap["score"])
			sortScore := rawScore

			entry := map[string]any{
				"uri":      strVal(itemMap["uri"]),
				"type":     strings.TrimRight(ctxType, "s"), // 对齐 Python: ctx_type.rstrip("s")
				"abstract": strVal(itemMap["abstract"]),
			}
			// 对齐 Python: entry["score"] = round(raw_score, 3) if raw_score is not None else 0.0
			if rawScore > 0 {
				entry["score"] = roundTo3(rawScore)
			} else {
				entry["score"] = 0.0
			}

			// 对齐 Python: if item.get("relations"): entry["related"] = [r.get("uri") for r in item["relations"][:3]]
			if relations, ok := itemMap["relations"].([]any); ok && len(relations) > 0 {
				related := make([]string, 0, 3)
				for i, r := range relations {
					if i >= 3 {
						break
					}
					if rMap, ok := r.(map[string]any); ok {
						related = append(related, strVal(rMap["uri"]))
					}
				}
				entry["related"] = related
			}

			scoredEntries = append(scoredEntries, scoredEntry{score: sortScore, entry: entry})
		}
	}

	// 对齐 Python: scored_entries.sort(key=lambda x: x[0], reverse=True)
	sort.Slice(scoredEntries, func(i, j int) bool {
		return scoredEntries[i].score > scoredEntries[j].score
	})

	formatted := make([]map[string]any, len(scoredEntries))
	for i, se := range scoredEntries {
		formatted[i] = se.entry
	}

	// 对齐 Python: result = {"results": formatted, "total": resp.get("result", {}).get("total", len(formatted))}
	total := floatVal(resultMap["total"])
	if total == 0 {
		total = float64(len(formatted))
	}
	return map[string]any{
		"results": formatted,
		"total":   int(total),
	}, nil
}

// handleVikingRead 处理 viking_read 工具调用。
// 对齐 Python: handle_tool_call 中 tool_name == "viking_read" 分支
func (p *OpenVikingProvider) handleVikingRead(ctx context.Context, args map[string]any) (any, error) {
	// 对齐 Python: uri = args.get("uri", ""); if not uri: return json.dumps({"error": "uri is required"})
	uri, _ := args["uri"].(string)
	if uri == "" {
		return nil, fmt.Errorf("uri is required")
	}

	// 对齐 Python: level = args.get("level", args.get("detail", "overview"))
	level := strVal(args["level"])
	if level == "" {
		level = strVal(args["detail"])
	}
	if level == "" {
		level = "overview"
	}

	var result map[string]any
	var err error

	// 对齐 Python: if level == "abstract": ... elif level == "full": ... else: overview
	switch level {
	case "abstract":
		result, err = p.client.get(ctx, "/api/v1/content/abstract", map[string]string{"uri": uri})
	case "full":
		result, err = p.client.get(ctx, "/api/v1/content/read", map[string]string{"uri": uri})
	default:
		result, err = p.client.get(ctx, "/api/v1/content/overview", map[string]string{"uri": uri})
	}
	if err != nil {
		return nil, err
	}

	// 对齐 Python: content = result.get("result", "")
	content := result["result"]
	var contentStr string
	switch v := content.(type) {
	case string:
		contentStr = v
	case map[string]any:
		// 对齐 Python: if not isinstance(content, str): content = content.get("content", "")
		contentStr, _ = v["content"].(string)
	default:
		contentStr = fmt.Sprintf("%v", content)
	}

	// 对齐 Python: if len(content) > 8000: content = content[:8000] + "\n\n[... truncated, ...]"
	if len(contentStr) > 8000 {
		contentStr = contentStr[:8000] + "\n\n[... truncated, use a more specific URI or abstract level]"
	}

	return map[string]any{
		"uri":     uri,
		"level":   level,
		"content": contentStr,
	}, nil
}

// handleVikingBrowse 处理 viking_browse 工具调用。
// 对齐 Python: handle_tool_call 中 tool_name == "viking_browse" 分支
func (p *OpenVikingProvider) handleVikingBrowse(ctx context.Context, args map[string]any) (any, error) {
	// 对齐 Python: action = args.get("action", "list")
	action, _ := args["action"].(string)
	if action == "" {
		action = "list"
	}

	// 对齐 Python: browse_path = args.get("path", "viking://")
	browsePath, _ := args["path"].(string)
	if browsePath == "" {
		browsePath = "viking://"
	}

	// 对齐 Python: endpoint_map = {"tree": "/api/v1/fs/tree", "list": "/api/v1/fs/ls", "stat": "/api/v1/fs/stat"}
	endpointMap := map[string]string{
		"tree": "/api/v1/fs/tree",
		"list": "/api/v1/fs/ls",
		"stat": "/api/v1/fs/stat",
	}
	path, ok := endpointMap[action]
	if !ok {
		path = "/api/v1/fs/ls"
	}

	// 对齐 Python: result = await asyncio.to_thread(self._client.get, path, {"uri": browse_path})
	result, err := p.client.get(ctx, path, map[string]string{"uri": browsePath})
	if err != nil {
		return nil, err
	}

	// 对齐 Python: entries = result.get("result", {})
	entries := result["result"]

	// 对齐 Python: if action in ("list", "tree") and isinstance(entries, list):
	if action == "list" || action == "tree" {
		if entriesList, ok := entries.([]any); ok {
			// 对齐 Python: for e in entries[:50]:
			formatted := make([]map[string]any, 0, len(entriesList))
			for i, e := range entriesList {
				if i >= 50 {
					break
				}
				eMap, _ := e.(map[string]any)
				if eMap == nil {
					continue
				}
				name := strVal(eMap["rel_path"])
				if name == "" {
					name = strVal(eMap["name"])
				}
				entryType := "file"
				if isDir, _ := eMap["isDir"].(bool); isDir {
					entryType = "dir"
				}
				formatted = append(formatted, map[string]any{
					"name":     name,
					"uri":      strVal(eMap["uri"]),
					"type":     entryType,
					"abstract": strVal(eMap["abstract"]),
				})
			}
			return map[string]any{
				"path":    browsePath,
				"entries": formatted,
			}, nil
		}
	}

	// stat 或非 list/tree 时直接返回原始 result
	return result, nil
}

// handleVikingRemember 处理 viking_remember 工具调用。
// 对齐 Python: handle_tool_call 中 tool_name == "viking_remember" 分支
func (p *OpenVikingProvider) handleVikingRemember(ctx context.Context, args map[string]any) (any, error) {
	// 对齐 Python: content = args.get("content", ""); if not content: return json.dumps({"error": "content is required"})
	content, _ := args["content"].(string)
	if content == "" {
		return nil, fmt.Errorf("content is required")
	}

	// 对齐 Python: category = args.get("category", "")
	// 对齐 Python: text = f"[Remember] {content}"; if category: text = f"[Remember — {category}] {content}"
	category, _ := args["category"].(string)
	text := fmt.Sprintf("[Remember] %s", content)
	if category != "" {
		text = fmt.Sprintf("[Remember — %s] %s", category, content)
	}

	// 对齐 Python: await asyncio.to_thread(self._client.post, f"/api/v1/sessions/{self._session_id}/messages", {"role": "user", "parts": [...]})
	_, err := p.client.post(ctx, fmt.Sprintf("/api/v1/sessions/%s/messages", p.sessionID), map[string]any{
		"role":  "user",
		"parts": []map[string]any{{"type": "text", "text": text}},
	})
	if err != nil {
		return nil, err
	}

	// 对齐 Python: result = {"status": "stored", "message": "Memory recorded. ..."}
	return map[string]any{
		"status":  "stored",
		"message": "Memory recorded. Will be extracted and indexed on session commit.",
	}, nil
}

// handleVikingAddResource 处理 viking_add_resource 工具调用。
// 对齐 Python: handle_tool_call 中 tool_name == "viking_add_resource" 分支
func (p *OpenVikingProvider) handleVikingAddResource(ctx context.Context, args map[string]any) (any, error) {
	// 对齐 Python: url = args.get("url", ""); if not url: return json.dumps({"error": "url is required"})
	urlStr, _ := args["url"].(string)
	if urlStr == "" {
		return nil, fmt.Errorf("url is required")
	}

	// 对齐 Python: payload = {"path": url}
	payload := map[string]any{"path": urlStr}

	// 对齐 Python: if args.get("reason"): payload["reason"] = args["reason"]
	if reason, _ := args["reason"].(string); reason != "" {
		payload["reason"] = reason
	}

	// 对齐 Python: result = await asyncio.to_thread(self._client.post, "/api/v1/resources", payload)
	result, err := p.client.post(ctx, "/api/v1/resources", payload)
	if err != nil {
		return nil, err
	}

	// 对齐 Python: res_data = result.get("result", {}); result = {"status": "added", "root_uri": res_data.get("root_uri", ""), ...}
	resData, _ := result["result"].(map[string]any)
	rootURI := ""
	if resData != nil {
		rootURI, _ = resData["root_uri"].(string)
	}

	return map[string]any{
		"status":   "added",
		"root_uri": rootURI,
		"message":  "Resource queued for processing. Use viking_search after a moment to find it.",
	}, nil
}

// truncStr 截断字符串到指定长度。
// 对齐 Python: user_msg[:4000]
func truncStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}

// floatVal 从 map 值中提取 float64。
func floatVal(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case int64:
		return float64(n)
	default:
		return 0
	}
}

// strVal 从 map 值中提取 string。
func strVal(v any) string {
	s, _ := v.(string)
	return s
}

// roundTo3 保留 3 位小数。
// 对齐 Python: round(raw_score, 3)
func roundTo3(f float64) float64 {
	return float64(int(f*1000+0.5)) / 1000
}
