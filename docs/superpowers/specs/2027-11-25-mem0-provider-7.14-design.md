# 7.14 Mem0Provider 实现设计

## 概述

实现 `Mem0Provider`，将 Mem0 云端 REST API 适配为 Go `MemoryProvider` 接口。
对齐 Python 源码：`openjiuwen/core/memory/external/mem0_provider.py`

## 在 Agent 会话中的流程位置

```
Agent 会话启动
  ├─ ExternalMemoryRail.Init()
  │    ├─ provider.GetToolSchemas()  → 注册 mem0_profile / mem0_search / mem0_conclude
  │    └─ provider.SystemPromptBlock() → 注入 "Mem0 Memory Active" 引导
  ├─ ExternalMemoryRail.BeforeInvoke()
  │    └─ provider.Initialize(opts) → 设置 api_key/user_id/agent_id
  ├─ BeforeModelCall()
  │    └─ provider.Prefetch(query) → 调 mem0.search，结果注入 system prompt
  ├─ 模型生成过程中
  │    └─ provider.HandleToolCall(tool_name, args)
  ├─ AfterInvoke()
  │    └─ provider.SyncTurn(user_msg, assistant_msg) → 调 mem0.add
  └─ Uninit()
       └─ provider.Shutdown() → 取消 prefetch goroutine、清理 client
```

作用：Mem0Provider 是 MemoryProvider 接口的具体实现，让 Agent 能读记忆（Prefetch + search + profile）、
写记忆（conclude + SyncTurn），内置熔断器防止外部 API 故障拖垮主流程。

## 文件结构

```
internal/agentcore/memory/external/
├── doc.go                # 包文档（需更新文件目录）
├── provider.go           # 已有：MemoryProvider 接口 + BaseMemoryProvider
├── provider_test.go      # 已有：接口测试
├── mem0_client.go        # 新增：mem0HTTPClient — REST API 封装
├── mem0_client_test.go   # 新增：client 单元测试
├── mem0_provider.go      # 新增：Mem0Provider — MemoryProvider 实现
└── mem0_provider_test.go # 新增：Provider 单元测试
```

包不变，仍放 `external/`，对齐 Python 目录。不创建子包。

## mem0_client.go — REST API 封装

### 结构体

```go
type mem0HTTPClient struct {
    apiKey     string
    baseURL    string           // 默认 "https://api.mem0.ai"
    httpClient *http.Client
}
```

### 方法

| Go 方法 | Python `_client_call` | REST 端点 | 说明 |
|---------|----------------------|----------|------|
| `search(ctx, query, filters, rerank, topK)` | `client.search(...)` | `POST /v1/memories/search/` | 语义搜索 |
| `getAll(ctx, filters)` | `client.get_all(...)` | `GET /v1/memories/` | 全量拉取 |
| `add(ctx, messages, filters, infer)` | `client.add(...)` | `POST /v1/memories/` | 添加记忆 |

### 请求/响应类型（非导出）

- `mem0SearchRequest` — search 请求体
- `mem0SearchResult` — search 结果项（含 memory + score）
- `mem0AddRequest` — add 请求体（含 messages + filters + infer）
- `mem0MemoryItem` — 通用记忆项（对应 Python `_unwrap_results` 中的 dict）

### 关键设计决策

1. 认证：`Authorization: Token {api_key}` header，对齐 Python SDK
2. filters 构造：search/getAll 用 read filters（user_id + 可选 agent_id），add 用 write filters（user_id + agent_id），对齐 Python `_read_filters()` / `_write_filters()`
3. 响应解析：统一 `unwrapResults` 处理两种格式（`{"results": [...]}` 或 `[...]`），对齐 Python `_unwrap_results()`
4. 错误处理：HTTP 非 2xx 返回 error，Provider 层记录日志+触发熔断
5. 不导出：client 是 Provider 内部实现细节

## mem0_provider.go — Mem0Provider

### 结构体

```go
type Mem0Provider struct {
    BaseMemoryProvider

    apiKey    string
    userID    string
    agentID   string
    rerank    bool

    client              *mem0HTTPClient
    initialized         bool
    mu                  sync.Mutex

    consecutiveFailures int
    breakerOpenUntil    time.Time

    prefetchCancel context.CancelFunc
    prefetchWg     sync.WaitGroup
}
```

### 工具 Schema（包级常量）

| 常量名 | 工具名 | 参数 | Python 对应 |
|--------|--------|------|------------|
| `mem0ProfileSchema` | `mem0_profile` | 无 | `PROFILE_SCHEMA` |
| `mem0SearchSchema` | `mem0_search` | `query`(必填), `rerank`, `top_k` | `SEARCH_SCHEMA` |
| `mem0ConcludeSchema` | `mem0_conclude` | `conclusion`(必填) | `CONCLUDE_SCHEMA` |

### 生命周期方法

| Go 方法 | Python 方法 | 说明 |
|---------|------------|------|
| `Name()` | `@property name` | 返回 `"mem0"` |
| `IsAvailable()` | `is_available()` | `apiKey != ""` |
| `IsInitialized()` | `@property is_initialized` | 覆盖 BaseMemoryProvider |
| `Initialize(ctx, opts...)` | `async def initialize(**kwargs)` | 设置参数，验证 apiKey，创建 client |
| `GetToolSchemas()` | `get_tool_schemas()` | 返回 3 个 schema |
| `SystemPromptBlock()` | `system_prompt_block()` | `"# Mem0 Memory\nActive. User: {userID}.\n..."` |
| `Prefetch(ctx, query, opts...)` | `async def prefetch(...)` | 调 client.search，格式化 Markdown |
| `QueuePrefetch(ctx, query, opts...)` | `async def queue_prefetch(...)` | goroutine 后台预热 |
| `SyncTurn(ctx, userMsg, assistantMsg, opts...)` | `async def sync_turn(...)` | 调 client.add |
| `HandleToolCall(ctx, toolName, args)` | `async def handle_tool_call(...)` | 分发 3 个工具 |
| `Shutdown(ctx)` | `async def shutdown()` | 取消 prefetch + 清理 |

### 熔断器方法（非导出）

| Go 方法 | Python 方法 | 说明 |
|---------|------------|------|
| `isBreakerOpen()` | `_is_breaker_open()` | 连续失败≥5 且在冷却期内 |
| `recordSuccess()` | `_record_success()` | 重置计数 |
| `recordFailure()` | `_record_failure()` | 递增，≥5 设置冷却截止 |

## 核心方法逻辑

### HandleToolCall

```
HandleToolCall(ctx, toolName, args)
  ├─ 熔断器 → {"error": "Mem0 API temporarily unavailable..."}
  ├─ mem0_profile → getAll → 格式化 or "No memories stored yet."
  ├─ mem0_search → search → 含 score 的结果 or "No relevant memories found."
  ├─ mem0_conclude → add(infer=false) → "Fact stored."
  └─ 未知 → {"error": "Unknown tool: {toolName}"}
  所有分支: 成功→recordSuccess, 异常→recordFailure + {"error": err.Error()}
```

### Prefetch

```
Prefetch → 空query或熔断 → ""
        → client.search → unwrapResults → 空 → ""
        → 有结果 → "## Mem0 Memory\n- line1\n- line2\n..."
        异常 → recordFailure, Debug日志, ""
```

### QueuePrefetch

```
QueuePrefetch → 空query或熔断 → 直接返回
             → 取消上一次 goroutine
             → go func { client.search; recordSuccess/Failure }()
             → 返回（不等结果）
```

### SyncTurn

```
SyncTurn → 熔断或空消息 → nil
        → client.add(messages, writeFilters)
        → 成功→recordSuccess, 异常→recordFailure, Warn日志
```

### Shutdown

```
Shutdown → cancel prefetch goroutine
        → prefetchWg.Wait()（2s 超时，对齐 Python wait_for 2.0s）
        → client = nil, initialized = false
```

## 日志

日志组件：`logger.ComponentAgentCore`

| Go 日志点 | Python 日志点 | 级别 | 字段 |
|----------|-------------|------|------|
| recordFailure 熔断器开启 | `_record_failure` warning | Warn | `consecutive_failures`, `cooldown_secs` |
| Prefetch 失败 | `prefetch` except debug | Debug | `error` |
| QueuePrefetch 失败 | `queue_prefetch` except debug | Debug | `error` |
| SyncTurn 失败 | `sync_turn` except warning | Warn | `error` |

## 熔断器参数

```go
const (
    mem0BreakerThreshold   = 5     // _BREAKER_THRESHOLD = 5
    mem0BreakerCooldownSec = 120.0 // _BREAKER_COOLDOWN_SECS = 120.0
)
```

## 测试策略

### mem0_client_test.go（httptest mock server）

| 测试 | 场景 |
|------|------|
| `TestMem0HTTPClient_Search` | 正常搜索 |
| `TestMem0HTTPClient_Search_空结果` | 返回空数组 |
| `TestMem0HTTPClient_Search_响应格式dict` | `{"results": [...]}` |
| `TestMem0HTTPClient_Search_响应格式list` | 直接 `[...]` |
| `TestMem0HTTPClient_GetAll` | 全量拉取 |
| `TestMem0HTTPClient_GetAll_带agentID` | filters 含 agent_id |
| `TestMem0HTTPClient_Add` | infer=true |
| `TestMem0HTTPClient_Add_Conclude` | infer=false |
| `TestMem0HTTPClient_认证失败` | 401 |
| `TestMem0HTTPClient_服务器错误` | 500 |
| `TestMem0HTTPClient_请求超时` | ctx cancel |

### mem0_provider_test.go

| 测试 | 场景 |
|------|------|
| `TestMem0Provider_Name` | "mem0" |
| `TestMem0Provider_IsAvailable` | apiKey 非空/为空 |
| `TestMem0Provider_Initialize` | 正常+缺失报错 |
| `TestMem0Provider_Initialize_覆盖参数` | opts 覆盖 |
| `TestMem0Provider_GetToolSchemas` | 3 个 schema |
| `TestMem0Provider_SystemPromptBlock` | 含 userID |
| `TestMem0Provider_Prefetch` | Markdown 格式 |
| `TestMem0Provider_Prefetch_空结果` | 空串 |
| `TestMem0Provider_Prefetch_熔断器开启` | 空串 |
| `TestMem0Provider_Prefetch_API失败` | recordFailure |
| `TestMem0Provider_QueuePrefetch` | 后台执行 |
| `TestMem0Provider_QueuePrefetch_熔断器开启` | 不启动 |
| `TestMem0Provider_SyncTurn` | 正常 |
| `TestMem0Provider_SyncTurn_熔断器开启` | 跳过 |
| `TestMem0Provider_SyncTurn_空消息` | 跳过 |
| `TestMem0Provider_HandleToolCall_Profile` | 正常 |
| `TestMem0Provider_HandleToolCall_Search` | 正常 |
| `TestMem0Provider_HandleToolCall_Search_缺query` | error JSON |
| `TestMem0Provider_HandleToolCall_Conclude` | infer=false |
| `TestMem0Provider_HandleToolCall_未知工具` | error JSON |
| `TestMem0Provider_HandleToolCall_熔断器开启` | unavailable JSON |
| `TestMem0Provider_熔断器` | 5次→开启→冷却→恢复 |
| `TestMem0Provider_Shutdown` | 取消+清理 |
| `TestMem0Provider_IsInitialized` | 初始化前后 |

覆盖率目标：两个文件均 ≥ 90%，无需 `//go:build` 标签。

## 回填

1. `doc.go` 文件目录树新增 `mem0_client.go` + `mem0_provider.go`
2. `IMPLEMENTATION_PLAN.md` 7.14 状态 `☐` → `✅`
3. DeepAdapter 集成（`buildExternalMemoryRail` / `_handle_external_memory_rail_by_config`）属于 10.6.3-10 回填范围，7.14 留 TODO 注释

## 设计决策记录

| 决策 | 选择 | 理由 |
|------|------|------|
| 熔断器位置 | 双重（Provider 内 + Rail 外） | 对齐 Python，Provider 可独立使用 |
| Mem0 客户端 | 自封装 HTTP | 零外部依赖，完全自控，Go 无官方 SDK |
| 文件结构 | 拆出独立 mem0_client.go | 对齐 Python _get_client/_client_call 分层，职责分离 |
| queue_prefetch | goroutine 对齐 | 完整对齐 Python asyncio.create_task，Shutdown 时取消 |
| 实现范围 | 仅 7.14 | 7.15-7.17 留后续 |
