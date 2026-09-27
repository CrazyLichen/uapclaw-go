# 7.15 OpenVikingProvider 设计

## 概述

实现 OpenVikingMemoryProvider，适配 OpenViking 上下文数据库 REST API，为 Agent 提供 5 个双向记忆工具（search / read / browse / remember / add_resource）和会话型记忆同步。

对应 Python 源码：`openjiuwen/core/memory/external/openviking_memory_provider.py`

## 在 Agent 会话中的流程位置

```
Agent 生命周期
├── Init → ExternalMemoryRail.Init()
│   ├── 注册 Provider 工具到 ability_manager/resource_mgr
│   └── 注入 SystemPromptBlock 到系统提示词
├── BeforeInvoke → ExternalMemoryRail.BeforeInvoke()
│   └── 调 provider.Initialize()（创建 HTTP 客户端 + 健康检查）
├── BeforeModelCall → ExternalMemoryRail.BeforeModelCall()
│   └── 调 provider.Prefetch() → 注入 <memory-context> 到系统提示词
├── [模型调用] → Provider 工具被调用（HandleToolCall）
├── AfterInvoke → ExternalMemoryRail.AfterInvoke()
│   └── 调 provider.SyncTurn()（异步同步对话记录 + 外层熔断器）
└── Uninit → ExternalMemoryRail.Uninit()
    ├── 注销工具
    └── provider.Shutdown()
```

**作用**：OpenVikingProvider 是 MemoryProvider 接口的第二个具体实现（7.14 Mem0Provider 是第一个）。与 Mem0 的纯记忆 CRUD 不同，OpenViking 是**会话型**的——有 session 概念，sync_turn 记录对话轮次，on_session_end 时 commit session 触发记忆提取和索引。

## 关键决策

| 决策项 | 选择 | 理由 |
|--------|------|------|
| 实现范围 | 只做 7.15 | 7.16/7.17 后续单独做 |
| 文件组织 | 拆两个文件 | viking_client.go + viking_provider.go，与 Mem0 保持一致 |
| 熔断器 | 不内建，依赖 ExternalMemoryRail 外层 | 对齐 Python（Python 的 OpenVikingProvider 无内建熔断器） |
| 健康检查 | Initialize 中做 health check | 对齐 Python `await asyncio.to_thread(self._client.health)` |
| 环境变量 | 构造函数中读 os.Getenv fallback | 对齐 Python `os.environ.get("OPENVIKING_ENDPOINT")` |

## 文件结构

```
internal/agentcore/memory/external/
├── doc.go              # 包文档（更新文件目录）
├── provider.go         # MemoryProvider 接口 + BaseMemoryProvider
├── mem0_client.go      # Mem0 HTTP 客户端
├── mem0_provider.go    # Mem0Provider
├── viking_client.go    # OpenViking HTTP 客户端（新增）
├── viking_provider.go  # OpenVikingProvider（新增）
├── viking_client_test.go   # 客户端测试（新增）
└── viking_provider_test.go # Provider 测试（新增）
```

## viking_client.go — HTTP 客户端

对齐 Python `_VikingClient`，封装 OpenViking REST API 的 HTTP 通信。

### 结构体

```go
type vikingClient struct {
    endpoint   string        // API 地址
    account    string        // 账户标识
    user       string        // 用户标识
    agent      string        // Agent 标识
    httpClient *http.Client  // HTTP 客户端
}
```

### 方法（4 个）

| Go 方法 | Python 方法 | 说明 |
|---------|------------|------|
| `health(ctx) bool` | `health() -> bool` | GET /health，200 返回 true |
| `post(ctx, path, body) (map[string]any, error)` | `post(path, body) -> dict` | POST JSON，返回响应 dict |
| `get(ctx, path, params) (map[string]any, error)` | `get(path, params) -> dict` | GET query params，返回响应 dict |
| `close()` | `close()` | 关闭 HTTP 客户端 |

### HTTP Header

```
Content-Type: application/json
X-OpenViking-Account: {account}
X-OpenViking-User: {user}
X-OpenViking-Agent: {agent}
X-API-Key: {apiKey}          # 仅当 apiKey 非空时设置
```

### 超时

Python: `httpx.Client(timeout=30.0)` → Go: `httpClient.Timeout = 30 * time.Second`

## viking_provider.go — Provider

### 结构体

```go
type OpenVikingProvider struct {
    BaseMemoryProvider
    endpoint    string         // API 地址
    apiKey      string         // API 密钥
    account     string         // 账户标识
    user        string         // 用户标识
    agent       string         // Agent 标识
    client      *vikingClient  // HTTP 客户端（延迟初始化）
    sessionID   string         // 会话标识
    initialized bool           // 是否已初始化
}
```

### 构造函数 — 环境变量 fallback

```go
func NewOpenVikingProvider(endpoint, apiKey, account, user, agent string) *OpenVikingProvider
```

| 参数 | 环境变量 fallback | Python 默认值 |
|------|-------------------|--------------|
| endpoint | `OPENVIKING_ENDPOINT` | `""` |
| apiKey | `OPENVIKING_API_KEY` | `""` |
| account | `OPENVIKING_ACCOUNT` | `"default"` |
| user | `OPENVIKING_USER` | `"default"` |
| agent | `OPENVIKING_AGENT` | `"hermes"` |

### 5 个工具 Schema 变量

| Go 变量 | Python 常量 | 工具名 |
|---------|------------|--------|
| `vikingSearchSchema` | `VIKING_SEARCH_SCHEMA` | `viking_search` |
| `vikingReadSchema` | `VIKING_READ_SCHEMA` | `viking_read` |
| `vikingBrowseSchema` | `VIKING_BROWSE_SCHEMA` | `viking_browse` |
| `vikingRememberSchema` | `VIKING_REMEMBER_SCHEMA` | `viking_remember` |
| `vikingAddResourceSchema` | `VIKING_ADD_RESOURCE_SCHEMA` | `viking_add_resource` |

### 9 个 MemoryProvider 接口方法

| Go 方法 | Python 方法 | 关键逻辑 |
|---------|------------|---------|
| `Name() string` | `name` | 返回 `"openviking"` |
| `IsAvailable() bool` | `is_available()` | `endpoint != ""` |
| `IsInitialized() bool` | `is_initialized` | 返回 `initialized` 字段 |
| `Initialize(ctx, opts...) error` | `async initialize(**kwargs)` | 从 opts 取 sessionID → 创建 vikingClient → health check → 失败设 nil+warn |
| `SystemPromptBlock() string` | `system_prompt_block()` | 硬编码 5 行引导文本 |
| `Prefetch(ctx, query, opts...) (string, error)` | `async prefetch(query, **kwargs)` | POST /api/v1/search/find → 取 memories+resources 各前 3 → 格式化 |
| `SyncTurn(ctx, userMsg, assistantMsg, opts...) error` | `async sync_turn(user_msg, assistant_msg, **kwargs)` | POST /api/v1/sessions/{sid}/messages × 2（user + assistant），截断 4000 字符 |
| `HandleToolCall(ctx, toolName, args) (string, error)` | `async handle_tool_call(tool_name, args)` | switch 5 个工具名 → 各 handleXxx 方法 |
| `OnSessionEnd(ctx, messages) error` | `async on_session_end(messages)` | POST /api/v1/sessions/{sid}/commit |
| `Shutdown(ctx) error` | `async shutdown()` | client.close() + 设 nil |

### 5 个 handleXxx 私有方法

| Go 方法 | 对应工具 | 核心 API 调用 |
|---------|---------|--------------|
| `handleVikingSearch(ctx, args) (string, error)` | viking_search | POST /api/v1/search/find → 按 score 降序排序 → 格式化 |
| `handleVikingRead(ctx, args) (string, error)` | viking_read | GET /api/v1/content/abstract\|overview\|read → 截断 8000 字符 |
| `handleVikingBrowse(ctx, args) (string, error)` | viking_browse | GET /api/v1/fs/tree\|ls\|stat → 格式化条目（最多 50 个） |
| `handleVikingRemember(ctx, args) (string, error)` | viking_remember | POST /api/v1/sessions/{sid}/messages 带 [Remember] 前缀 |
| `handleVikingAddResource(ctx, args) (string, error)` | viking_add_resource | POST /api/v1/resources → 返回 root_uri |

## 测试

### viking_client_test.go

| 测试函数 | 覆盖 |
|---------|------|
| `TestVikingClient_Health_成功` | GET /health 返回 200 → true |
| `TestVikingClient_Health_失败` | 服务不可达 → false |
| `TestVikingClient_Post_成功` | POST 返回 JSON → 解析为 map |
| `TestVikingClient_Post_错误状态码` | 返回 500 → error |
| `TestVikingClient_Get_带参数` | GET 带 query params → 解析为 map |
| `TestVikingClient_Get_错误状态码` | 返回 404 → error |
| `TestVikingClient_Close` | 调用 close 不 panic |
| `TestVikingClient_请求Header` | 验证 Content-Type / X-OpenViking-* / X-API-Key |

### viking_provider_test.go

| 测试函数 | 覆盖 |
|---------|------|
| `TestNewOpenVikingProvider_默认值` | 空参数 → 环境变量 fallback + 默认值 |
| `TestNewOpenVikingProvider_显式参数` | 有参数 → 不读环境变量 |
| `TestOpenVikingProvider_Name` | 返回 `"openviking"` |
| `TestOpenVikingProvider_IsAvailable_有Endpoint` | endpoint 非空 → true |
| `TestOpenVikingProvider_IsAvailable_无Endpoint` | endpoint 空 → false |
| `TestOpenVikingProvider_Initialize_成功` | health check 通过 → initialized=true |
| `TestOpenVikingProvider_Initialize_健康检查失败` | health 失败 → client=nil, initialized=false |
| `TestOpenVikingProvider_Initialize_从Opts取SessionID` | opts 中带 sessionID → 覆盖 |
| `TestOpenVikingProvider_SystemPromptBlock` | 返回硬编码文本 |
| `TestOpenVikingProvider_Prefetch_有结果` | 返回 "## OpenViking Context\n- ..." 格式 |
| `TestOpenVikingProvider_Prefetch_无Client` | client=nil → 返回空串 |
| `TestOpenVikingProvider_Prefetch_空查询` | query="" → 返回空串 |
| `TestOpenVikingProvider_SyncTurn_成功` | POST 两次 messages 成功 |
| `TestOpenVikingProvider_SyncTurn_无Client` | client=nil → 无操作 |
| `TestOpenVikingProvider_HandleToolCall_各工具` | 5 个工具各一个成功用例 |
| `TestOpenVikingProvider_HandleToolCall_未知工具` | 返回 `{"error": "Unknown tool: ..."}` |
| `TestOpenVikingProvider_HandleToolCall_无Client` | 返回 `{"error": "OpenViking not connected"}` |
| `TestOpenVikingProvider_OnSessionEnd_成功` | POST commit 成功 |
| `TestOpenVikingProvider_OnSessionEnd_无Client` | client=nil → 无操作 |
| `TestOpenVikingProvider_Shutdown` | close + 设 nil |

## doc.go 更新

在 `external/doc.go` 的文件目录中新增两个条目：

```
//	external/
//	├── doc.go              # 包文档
//	├── provider.go         # MemoryProvider 接口 + BaseMemoryProvider + ToolSchema + ProviderOption
//	├── mem0_client.go      # Mem0 HTTP 客户端
//	├── mem0_provider.go    # Mem0Provider
//	├── viking_client.go    # OpenViking HTTP 客户端（REST API 封装：health/post/get/close + 身份 Header）
//	└── viking_provider.go  # OpenVikingProvider — MemoryProvider 的 OpenViking 实现（5 工具+会话管理）
```

## IMPLEMENTATION_PLAN.md 更新

7.15 行状态改为 `✅`，描述补充具体实现内容。

## 日志同步

对齐 Python 的 `memory_logger` 调用，使用 `logger.ComponentAgentCore`（`extMemoryLogComponent` 已在 `external_memory_rail.go` 中定义，Provider 层使用同组件）：

| Python 日志点 | Go 日志点 |
|--------------|----------|
| `logger.warning("OpenViking at %s not reachable", self._endpoint)` | Initialize 中 health 失败 → `logger.Warn(...).Str("endpoint", ...).Msg("OpenViking 不可达")` |
| `logger.warning("httpx not installed — OpenViking disabled")` | 不适用（Go 无动态依赖） |
| `logger.warning("OpenViking init failed: %s", e)` | Initialize 异常 → `logger.Warn(...).Err(err).Msg("OpenViking 初始化失败")` |
| `logger.debug("OpenViking prefetch failed: %s", e)` | Prefetch 异常 → `logger.Debug(...).Err(err).Msg("OpenViking prefetch 失败")` |
| `logger.debug("OpenViking sync failed: %s", e)` | SyncTurn 异常 → `logger.Debug(...).Err(err).Msg("OpenViking sync_turn 失败")` |
| `logger.debug("OpenViking session commit failed: %s", e)` | OnSessionEnd 异常 → `logger.Debug(...).Err(err).Msg("OpenViking session commit 失败")` |
| `logger.debug("OpenViking client close failed: %s", e)` | Shutdown 异常 → `logger.Debug(...).Err(err).Msg("OpenViking client close 失败")` |
