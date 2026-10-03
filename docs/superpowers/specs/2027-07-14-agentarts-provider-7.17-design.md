# AgentArtsMemoryProvider 7.17 设计文档

## 概述

实现 `AgentArtsMemoryProvider`，对齐 Python `openjiuwen/core/memory/external/agentarts_memory_provider.py`，
适配华为云 AgentArts 记忆服务。通过自封装 HTTP REST 客户端对接 AgentArts Data Plane API，
实现 `MemoryProvider` 接口的全部方法。

## AgentArts 在 Agent 会话中的流程位置

```
Agent 会话生命周期（通过 ExternalMemoryRail 桥接）
  ┌─────────────────────────────────────────────────────────────┐
  │ Init          → 注册 Provider 工具 + 注入 SystemPromptBlock  │
  │ BeforeInvoke  → Provider.Initialize()                       │
  │ BeforeModelCall → Provider.Prefetch(query)                  │
  │ [Agent 工具调用] → Provider.HandleToolCall()                │
  │ AfterInvoke   → Provider.SyncTurn(userMsg, assistantMsg)    │
  │ Uninit        → Provider.OnSessionEnd() + Provider.Shutdown()│
  └─────────────────────────────────────────────────────────────┘
```

**作用**：
1. **搜索记忆**：通过 AgentArts `search_memories` API 检索长期记忆（支持 strategy_type 过滤）
2. **会话映射**：将应用层 session_id 映射到 AgentArts memory_session_id（用 `kv.BaseKVStore` 缓存）
3. **同步对话**：每轮对话后将 user/assistant 消息写入 AgentArts session
4. **工具暴露**：提供 `external_memory_search` 工具供 Agent 主动搜索记忆

## AgentArts Data Plane REST API 规范

### 认证

- Header：`Authorization: Bearer {api_key}`
- Header：`Content-Type: application/json`

### Base URL

默认：`https://memory.cn-southwest-2.huaweicloud-agentarts.com`
构造时可覆盖，对齐 Python `_configure_sdk_data_endpoint`。

### 3 个 API

#### 1. search_memories

| 项目 | 值 |
|------|---|
| HTTP | POST |
| 路径 | `/v1/core/spaces/{space_id}/memories/search` |
| 对齐 Python | `client.search_memories(space_id, filters=...)` |

请求体（对齐 `MemorySearchFilter.to_dict()`）：

```json
{
  "query": "search query",
  "top_k": 10,
  "min_score": 0.5,
  "strategy_type": "semantic",
  "actor_id": "user-id"
}
```

仅包含非零值字段。默认 `top_k=10`、`min_score=0.5`。

响应体：

```json
{
  "records": [
    {
      "record": { "id": "...", "content": "记忆内容", "strategy_type": "semantic", ... },
      "score": 0.91
    }
  ],
  "total": 5,
  "query": "original query"
}
```

Provider 提取 `record.content` + `score`，归一化为 `[{memory, score}]`。

#### 2. create_memory_session

| 项目 | 值 |
|------|---|
| HTTP | POST |
| 路径 | `/v1/core/spaces/{space_id}/sessions` |
| 对齐 Python | `client.create_memory_session(space_id=..., actor_id=..., assistant_id=...)` |

请求体：

```json
{
  "actor_id": "user-id",
  "assistant_id": "assistant-id"
}
```

仅包含非空字段。

响应体：

```json
{
  "id": "server-assigned-session-uuid",
  "space_id": "...",
  "actor_id": "...",
  "assistant_id": "..."
}
```

Provider 读取 `id` 作为 memory_session_id。

#### 3. add_messages

| 项目 | 值 |
|------|---|
| HTTP | POST |
| 路径 | `/v1/core/spaces/{space_id}/sessions/{session_id}/messages` |
| 对齐 Python | `client.add_messages(space_id=..., session_id=..., messages=[...])` |

请求体（对齐 `TextMessage.to_dict()`）：

```json
{
  "messages": [
    {
      "role": "user",
      "parts": [{ "type": "text", "text": "Hello" }],
      "actor_id": "user-id",
      "assistant_id": "assistant-id"
    },
    {
      "role": "assistant",
      "parts": [{ "type": "text", "text": "Hi" }],
      "actor_id": "user-id",
      "assistant_id": "assistant-id"
    }
  ]
}
```

注意 TextMessage 的 wire 格式是 `parts: [{type: "text", text: content}]`，不是直接 `content` 字段。

## 文件结构

```
internal/agentcore/memory/external/
├── agentarts_client.go         # HTTP 客户端 + 请求/响应结构体
├── agentarts_provider.go       # AgentArtsProvider 实现 MemoryProvider
├── agentarts_client_test.go    # 客户端单元测试（httptest mock）
└── agentarts_provider_test.go  # Provider 单元测试
```

### agentarts_client.go

#### 结构体

```go
// ──────────────────────────── 结构体 ────────────────────────────

// agentartsClient AgentArts Data Plane HTTP 客户端。
// 对齐 Python: agentarts.sdk.memory.MemoryClient（自封装 REST 替代 SDK 依赖）
type agentartsClient struct {
    baseURL    string       // Data Plane 基础 URL
    apiKey     string       // Bearer token
    httpClient *http.Client // 带超时的 HTTP 客户端
}

// memorySearchFilter 记忆搜索过滤条件。
// 对齐 Python: MemorySearchFilter (agentarts/sdk/memory/inner/config.py)
type memorySearchFilter struct {
    Query        string   `json:"query"`
    TopK         int      `json:"top_k,omitempty"`
    MinScore     float64  `json:"min_score,omitempty"`
    StrategyType string   `json:"strategy_type,omitempty"`
    ActorID      string   `json:"actor_id,omitempty"`
    AssistantID  string   `json:"assistant_id,omitempty"`
    SessionID    string   `json:"session_id,omitempty"`
}

// memorySearchResponse 记忆搜索响应。
type memorySearchResponse struct {
    Records []memorySearchRecord `json:"records"`
    Total   int                  `json:"total"`
    Query   string               `json:"query"`
}

// memorySearchRecord 单条搜索结果。
type memorySearchRecord struct {
    Record struct {
        ID           string `json:"id"`
        Content      string `json:"content"`
        StrategyType string `json:"strategy_type"`
    } `json:"record"`
    Score float64 `json:"score"`
}

// sessionCreateRequest 创建会话请求。
type sessionCreateRequest struct {
    ActorID      string `json:"actor_id,omitempty"`
    AssistantID  string `json:"assistant_id,omitempty"`
}

// sessionInfo 会话信息响应。
type sessionInfo struct {
    ID          string `json:"id"`
    SpaceID     string `json:"space_id"`
    ActorID     string `json:"actor_id"`
    AssistantID string `json:"assistant_id"`
}

// textMessagePart 文本消息部分。
type textMessagePart struct {
    Type string `json:"type"` // "text"
    Text string `json:"text"`
}

// textMessage 文本消息。
// 对齐 Python: TextMessage.to_dict() — wire 格式使用 parts 数组
type textMessage struct {
    Role         string          `json:"role"`                    // "user"/"assistant"/"system"
    Parts        []textMessagePart `json:"parts"`
    ActorID      string          `json:"actor_id,omitempty"`
    AssistantID  string          `json:"assistant_id,omitempty"`
}

// addMessagesRequest 添加消息请求。
type addMessagesRequest struct {
    Messages []textMessage `json:"messages"`
}

// normalizedMemoryItem 归一化记忆条目（Provider 内部使用）。
// 对齐 Python: {"memory": content, "score": score}
type normalizedMemoryItem struct {
    Memory string  `json:"memory"`
    Score  float64 `json:"score"`
}
```

#### 导出函数

```go
// newAgentArtsClient 创建 AgentArts HTTP 客户端。
func newAgentArtsClient(baseURL, apiKey string) *agentartsClient

// searchMemories 搜索记忆。
// 对齐 Python: client.search_memories(space_id, filters=...)
func (c *agentartsClient) searchMemories(ctx context.Context, spaceID string, filter memorySearchFilter) ([]normalizedMemoryItem, error)

// createMemorySession 创建记忆会话。
// 对齐 Python: client.create_memory_session(space_id=..., actor_id=..., assistant_id=...)
func (c *agentartsClient) createMemorySession(ctx context.Context, spaceID string, req sessionCreateRequest) (*sessionInfo, error)

// addMessages 添加消息到会话。
// 对齐 Python: client.add_messages(space_id=..., session_id=..., messages=[...])
func (c *agentartsClient) addMessages(ctx context.Context, spaceID, sessionID string, msgs []textMessage) error
```

#### 非导出函数

```go
// doRequest 执行 HTTP 请求并解析 JSON 响应。
func (c *agentartsClient) doRequest(ctx context.Context, method, path string, body any, result any) error

// buildTextMessage 构建 TextMessage。
// 对齐 Python: _text_message(role, content, actor_id=..., assistant_id=...)
func buildTextMessage(role, content, actorID, assistantID string) textMessage
```

### agentarts_provider.go

#### 结构体

```go
// ──────────────────────────── 结构体 ────────────────────────────

// AgentArtsProvider 华为云 AgentArts 记忆服务适配器。
// 对齐 Python: AgentArtsMemoryProvider (agentarts_memory_provider.py)
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
```

#### 常量

```go
const (
    // defaultAgentArtsBaseURL 默认 Data Plane 基础 URL
    defaultAgentArtsBaseURL = "https://memory.cn-southwest-2.huaweicloud-agentarts.com"
    // sessionMappingKeyPrefix session 映射 KV 前缀
    sessionMappingKeyPrefix = "agentarts/session_mapping"
    // maxTopK 搜索最大返回数
    maxTopK = 100
    // defaultTopK 搜索默认返回数
    defaultTopK = 10
    // defaultMinScore 搜索默认最低相似度
    defaultMinScore = 0.5
)
```

#### 工具 Schema

1 个工具 `external_memory_search`：

```json
{
  "name": "external_memory_search",
  "description": "Search long-term external memory for durable facts, user preferences, and prior conversation context.",
  "parameters": {
    "type": "object",
    "properties": {
      "query": { "type": "string", "description": "Memory search query." },
      "top_k": { "type": "integer", "description": "Max results, default 10, max 100." },
      "strategy_type": {
        "type": "string",
        "enum": ["semantic", "summary", "user_preference", "episodic", "event", "custom"],
        "description": "Optional memory strategy type filter."
      },
      "min_score": { "type": "number", "description": "Optional minimum similarity score. (default: 0.5)" }
    },
    "required": ["query"]
  }
}
```

对齐 Python `EXTERNAL_MEMORY_SEARCH_SCHEMA`，提示词从 Python 源码直接复制。

#### MemoryProvider 接口实现

| Go 方法 | Python 方法 | 行为 |
|---------|------------|------|
| `Name()` | `name` | 返回 `"agentarts"` |
| `IsAvailable()` | `is_available()` | `apiKey != "" && spaceID != ""` |
| `Initialize(ctx, opts)` | `initialize(**kwargs)` | 从 opts 解析 actorID/assistantID/sessionID → `ensureMemorySession` → `initialized = true` |
| `GetToolSchemas()` | `get_tool_schemas()` | 返回 `[externalMemorySearchSchema]` |
| `HandleToolCall(ctx, toolName, args)` | `handle_tool_call(tool_name, args)` | 仅处理 `external_memory_search`，调 `_search` → 返回 JSON |
| `Prefetch(ctx, query, opts)` | `prefetch(query, **kwargs)` | 调 `search` → 成功归零失败+1 → 格式化 `"## External Memory\n- ..."` |
| `SyncTurn(ctx, userMsg, assistantMsg, opts)` | `sync_turn(user_msg, assistant_msg, **kwargs)` | 确保 session → `addMessages` → **吞掉错误返回 nil** → 成功归零失败+1 |
| `SystemPromptBlock()` | `system_prompt_block()` | 返回引导文本 |
| `Shutdown(ctx)` | `shutdown()` | `client = nil`, `initialized = false` |
| `IsInitialized()` | `is_initialized` | 返回 `initialized` |

#### 关键私有方法

| Go 方法 | Python 方法 | 职责 |
|---------|------------|------|
| `ensureMemorySession(ctx, sessionID, actorID, assistantID)` | `_ensure_memory_session` | 查 KV 映射 → 有则返回 → 无则调 `createMemorySession` → 存映射 |
| `search(ctx, query, args)` | `_search(query, args)` | 构建 filter → 调 `client.searchMemories` → 归一化为 `[]normalizedMemoryItem` |
| `runtimeActorID(opts)` | `_runtime_actor_id(params)` | 优先 opts.UserID → 运行时 actorID → 默认 actorID |
| `runtimeAssistantID(opts)` | `_runtime_assistant_id(params)` | 优先 opts.ScopeID → 运行时 assistantID → 默认 assistantID |
| `sessionMappingKey(sessionID)` | `_session_mapping_key` | `"agentarts/session_mapping/{sessionID}"` |
| `getClient()` | `_get_client()` | 懒加载 `agentartsClient`（sync.Once） |

### 回填修改

1. **`doc.go`**：文件目录添加 `agentarts_client.go` + `agentarts_provider.go`
2. **`deep_adapter_rails.go`**：`buildExternalMemoryProvider` switch 添加 `case "agentarts"` 分支：
   ```go
   case "agentarts":
       baseURL := strVal(cfg["base_url"])
       apiKey := strVal(cfg["api_key"])
       spaceID := strVal(cfg["space_id"])
       actorID := strVal(cfg["actor_id"])
       assistantID := strVal(cfg["assistant_id"])
       provider := ext.NewAgentArtsProvider(baseURL, apiKey, spaceID, actorID, assistantID, nil)
       if !provider.IsAvailable() {
           logger.Warn(logComponent).Msg("buildExternalMemoryProvider: AgentArtsProvider unavailable")
           return nil
       }
       return provider
   ```
3. **`IMPLEMENTATION_PLAN.md`**：7.17 状态 ☐ → ✅

### 日志同步

对齐 Python `logger.info/warning/debug` 调用，使用 `logger.ComponentAgentCore` 组件：

| Python 日志点 | Go 日志 | 级别 |
|-------------|---------|------|
| `initializing with params: %s` | `Initialize` 入参 JSON | Info |
| `search agentarts memory space [%s] with filters: %s` | `search` 入参 spaceID + filter JSON | Info |
| `found %d relevant memory records` | `search` 返回条数 | Info |
| `prefetch failed: %s` | `Prefetch` 失败 | Debug |
| `use exist session mapping entry: %s -> %s` | `ensureMemorySession` 命中缓存 | Info |
| `add session mapping entry: %s -> %s` | `ensureMemorySession` 新建映射 | Info |
| `failed to search relevant memories` | `HandleToolCall` 搜索失败 | Warn |
| `AgentArts sync failed: %s` | `SyncTurn` 失败 | Warn |

### 测试策略

- **agentarts_client_test.go**：`httptest.NewServer` mock 3 个 API 端点
  - 正常搜索 → 返回 records → 验证归一化
  - 搜索空结果 → 返回空 records
  - 搜索服务端错误 → 返回 error
  - 创建会话 → 返回 sessionInfo
  - 创建会话失败 → 返回 error
  - 添加消息 → 正常返回
  - 添加消息失败 → 返回 error
  - 超时 → context.Cancel

- **agentarts_provider_test.go**：
  - `NewAgentArtsProvider` 构造
  - `Name/IsAvailable/IsInitialized` 基本属性
  - `Initialize` 正常流程（含 session 映射创建）
  - `Initialize` 使用已有映射
  - `GetToolSchemas` 验证 schema 结构
  - `HandleToolCall` 正常搜索
  - `HandleToolCall` 未知工具名
  - `HandleToolCall` 缺少 query 参数
  - `HandleToolCall` 搜索失败
  - `Prefetch` 正常
  - `Prefetch` 空查询
  - `Prefetch` 搜索失败
  - `SyncTurn` 正常
  - `SyncTurn` 空消息跳过
  - `SyncTurn` 错误吞掉返回 nil
  - `Shutdown` 清理
  - `consecutiveFailures` 计数
  - `SystemPromptBlock` 非空
  - `runtimeActorID/runtimeAssistantID` 优先级

- 目标覆盖率 ≥ 85%

### 设计决策记录

| 决策 | 选择 | 原因 |
|------|------|------|
| 客户端实现 | 自封装 HTTP REST | Go 无官方 SDK，逆向 Python SDK REST API |
| Session 映射存储 | 构造函数接受可选 `kv.BaseKVStore`，默认 InMemoryKVStore | 与 Python 完全对齐 |
| 熔断器 | 仅跟踪 `consecutiveFailures` 计数，无正式熔断 | 与 Python 对齐，靠外层 ExternalMemoryRail 熔断 |
| SyncTurn 错误处理 | 吞掉错误返回 nil | 与 Python `sync_turn` 完全对齐，与 Mem0Provider Go 实现一致 |
| 请求/响应结构体 | 全 typed（不像 Viking 用 `map[string]any`） | AgentArts API 返回格式固定，typed 更安全 |
