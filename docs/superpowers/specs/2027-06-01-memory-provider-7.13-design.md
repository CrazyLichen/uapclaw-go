# 7.13 MemoryProvider 协议 + ExternalMemoryRail 设计

## 概述

实现领域七 7.13 小节：定义外部记忆提供者统一接口 `MemoryProvider`，以及其消费者 `ExternalMemoryRail`。

本次范围仅 7.13 协议接口，不包含具体 Provider 实现（7.14-7.17 延后）。

## 在 Agent 会话流程中的位置与作用

```
Agent 会话生命周期
┌──────────────────────────────────────────────────────────┐
│ 1. 初始化阶段 (Init / BeforeInvoke)                      │
│    ├─ CodingMemoryRail.Init()        ← 7.1-7.5 已实现    │
│    ├─ MemoryRail.Init()              ← 7.6-7.10 已实现   │
│    ├─ ExternalMemoryRail.Init()      ← 7.13 本次实现     │
│    │   └─ provider.Initialize()      ← 核心方法          │
│    └─ ...                                                │
│                                                          │
│ 2. 每轮对话 - 模型调用前 (BeforeModelCall)               │
│    ├─ CodingMemoryRail: 注入编程记忆 context              │
│    ├─ MemoryRail: 注入片段/摘要/变量记忆                   │
│    ├─ ExternalMemoryRail:             ← 7.13 本次实现     │
│    │   └─ provider.Prefetch(query)    ← 核心方法          │
│    │       → 注入 <memory-context> 到系统提示词            │
│    └─ ...                                                │
│                                                          │
│ 3. 工具调用 (HandleToolCall)                              │
│    ├─ CodingMemoryTools: 读写搜索编程记忆                  │
│    ├─ MemoryRail 工具: 冲突检查/写入/搜索                  │
│    ├─ ExternalMemoryRail:             ← 7.13 本次实现     │
│    │   └─ provider.HandleToolCall()   ← 核心方法          │
│    │       → 动态注册到 Agent 的 AbilityManager            │
│    └─ ...                                                │
│                                                          │
│ 4. 每轮对话 - 模型调用后 (AfterInvoke / SyncTurn)        │
│    ├─ MemoryRail: 更新片段/摘要/变量                       │
│    ├─ ExternalMemoryRail:             ← 7.13 本次实现     │
│    │   └─ provider.SyncTurn()         ← 核心方法          │
│    │       → 序列化 + 熔断器保护                           │
│    └─ ...                                                │
│                                                          │
│ 5. 会话结束 (Uninit / OnSessionEnd / Shutdown)            │
│    ├─ provider.Shutdown()             ← 核心方法          │
│    ├─ provider.OnSessionEnd()         ← 核心方法          │
│    └─ 从 AbilityManager 注销工具                          │
└──────────────────────────────────────────────────────────┘
```

核心作用：
1. **抽象层**：定义 7 个必须方法 + 4 个可选方法，将外部记忆后端统一为 `Initialize → Prefetch → SyncTurn → Shutdown` 生命周期
2. **工具桥接**：通过 `GetToolSchemas()` + `HandleToolCall()` 将外部记忆能力动态注册为 Agent 可调用的工具
3. **提示词注入**：通过 `SystemPromptBlock()` + prefetch 结果注入到系统提示词，让 Agent 感知外部记忆
4. **容错保护**：内置熔断器（连续 5 次失败后冷却 120s），prefetch 超时（5s），序列化 syncTurn

## 文件结构与包布局

路径对齐 Python：

| Python | Go | 说明 |
|--------|-----|------|
| `openjiuwen/core/memory/external/provider.py` | `internal/agentcore/memory/external/provider.go` | MemoryProvider 接口 |
| `openjiuwen/core/memory/external/mem0_provider.py` | `internal/agentcore/memory/external/mem0.go` | 本次不做（7.14） |
| `openjiuwen/core/memory/external/openviking_memory_provider.py` | `internal/agentcore/memory/external/openviking.go` | 本次不做（7.15） |
| `openjiuwen/core/memory/external/openjiuwen_memory_provider.py` | `internal/agentcore/memory/external/openjiuwen.go` | 本次不做（7.16） |
| `openjiuwen/core/memory/external/agentarts_memory_provider.py` | `internal/agentcore/memory/external/agentarts.go` | 本次不做（7.17） |
| `openjiuwen/harness/rails/memory/external_memory_rail.py` | `internal/agentcore/harness/rails/memory/external_memory_rail.go` | ExternalMemoryRail |

新增文件：

```
internal/agentcore/
├── memory/external/                     # 新包
│   ├── doc.go                           # 包文档
│   ├── provider.go                      # MemoryProvider 接口 + BaseMemoryProvider + ProviderOption
│   └── provider_test.go                 # 接口 + Base 默认值测试
│
├── harness/rails/memory/               # 已有包，新增文件
│   ├── doc.go                           # 更新：加入 external_memory_rail.go 条目
│   ├── coding_memory_rail.go            # 已有
│   ├── memory_rail.go                   # 已有
│   ├── external_memory_rail.go          # 新增：ExternalMemoryRail
│   └── external_memory_rail_test.go     # 新增：ExternalMemoryRail 测试
```

包依赖方向：`harness/rails/memory/` → `memory/external/` + `rails/`，单向，无循环。

## MemoryProvider 接口设计

### 方案选择：大接口 + BaseMemoryProvider 嵌入

选择理由：
1. Python `MemoryProvider(ABC)` 就是一个包含全部方法的类，方案 B 完全对齐
2. 调用简单：`ExternalMemoryRail` 直接调 `provider.Shutdown()` 无需类型断言
3. `BaseMemoryProvider` 嵌入后具体 Provider 只需覆盖关心的可选方法，减少样板代码
4. Go 生态中 `Base*` 嵌入模式常见

### 接口定义

```go
// MemoryProvider 外部记忆提供者接口。
// 对齐 Python: MemoryProvider(ABC) (openjiuwen/core/memory/external/provider.py)
//
// 所有方法对齐 Python ABC:
//   - 必须实现: Name, IsAvailable, Initialize, GetToolSchemas,
//     HandleToolCall, Prefetch, SyncTurn
//   - 可选覆盖: SystemPromptBlock, Shutdown, OnSessionEnd, IsInitialized
type MemoryProvider interface {
    // Name 返回 Provider 唯一名称。
    // Python: @property name -> str
    Name() string

    // IsAvailable 检查 Provider 是否已配置且就绪（无网络调用）。
    // Python: is_available() -> bool
    IsAvailable() bool

    // Initialize 初始化 Provider。
    // Python: async def initialize(self, **kwargs) -> None
    Initialize(ctx context.Context, opts ...ProviderOption) error

    // GetToolSchemas 返回 Provider 提供的工具 Schema 列表。
    // Python: get_tool_schemas() -> list[dict[str, Any]]
    GetToolSchemas() []ToolSchema

    // HandleToolCall 处理工具调用并返回结果字符串。
    // Python: async def handle_tool_call(self, tool_name: str, args: dict) -> str
    HandleToolCall(ctx context.Context, toolName string, args map[string]any) (string, error)

    // Prefetch 根据查询预取记忆上下文。
    // Python: async def prefetch(self, query: str, **kwargs) -> str
    Prefetch(ctx context.Context, query string, opts ...ProviderOption) (string, error)

    // SyncTurn 同步一轮对话到外部记忆。
    // Python: async def sync_turn(self, user_msg: str, assistant_msg: str, **kwargs) -> None
    SyncTurn(ctx context.Context, userMsg, assistantMsg string, opts ...ProviderOption) error

    // SystemPromptBlock 返回 Provider 的系统提示词引导块。
    // Python: system_prompt_block() -> str（默认 ""）
    SystemPromptBlock() string

    // Shutdown 关闭 Provider 释放资源。
    // Python: async def shutdown() -> None（默认 no-op）
    Shutdown(ctx context.Context) error

    // OnSessionEnd 会话结束时回调。
    // Python: async def on_session_end(messages) -> None（默认 no-op）
    OnSessionEnd(ctx context.Context, messages []map[string]any) error

    // IsInitialized 返回 Provider 是否已初始化。
    // Python: @property is_initialized -> bool（默认 False）
    IsInitialized() bool
}
```

### ProviderOption（混合模式：固定参数 + functional options）

Python 中 `initialize(**kwargs)` / `prefetch(query, **kwargs)` / `sync_turn(user_msg, assistant_msg, **kwargs)` 的 kwargs 主要是 `user_id/scope_id/session_id`。

Go 采用混合模式：核心参数固定签名，扩展参数用 `opts ...ProviderOption`。

```go
// ToolSchema 工具 Schema 定义，对应 Python get_tool_schemas() 返回的 dict
type ToolSchema struct {
    Name        string         `json:"name"`
    Description string         `json:"description"`
    Parameters  map[string]any `json:"parameters"`
}

// ProviderOption Provider 可选参数的 functional option
type ProviderOption func(*ProviderOptions)

// ProviderOptions Provider 可选参数集合
type ProviderOptions struct {
    UserID    string
    ScopeID   string
    SessionID string
}

// WithUserID 设置用户标识
func WithUserID(id string) ProviderOption {
    return func(opts *ProviderOptions) { opts.UserID = id }
}

// WithScopeID 设置作用域标识
func WithScopeID(id string) ProviderOption {
    return func(opts *ProviderOptions) { opts.ScopeID = id }
}

// WithSessionID 设置会话标识
func WithSessionID(id string) ProviderOption {
    return func(opts *ProviderOptions) { opts.SessionID = id }
}

// applyOptions 应用 functional options 并返回 ProviderOptions
func applyOptions(opts ...ProviderOption) ProviderOptions {
    var po ProviderOptions
    for _, o := range opts {
        o(&po)
    }
    return po
}
```

### BaseMemoryProvider 默认实现

```go
// BaseMemoryProvider MemoryProvider 可选方法的默认实现。
// 具体 Provider 嵌入此结构体后只需覆盖关心的方法。
type BaseMemoryProvider struct{}

func (BaseMemoryProvider) SystemPromptBlock() string                              { return "" }
func (BaseMemoryProvider) Shutdown(_ context.Context) error                      { return nil }
func (BaseMemoryProvider) OnSessionEnd(_ context.Context, _ []map[string]any) error { return nil }
func (BaseMemoryProvider) IsInitialized() bool                                   { return false }
```

## ExternalMemoryRail 设计

### 常量

```go
const (
    externalMemoryRailPriority        = 75                // Python: priority = 75
    externalMemoryPrefetchTimeout     = 5 * time.Second   // Python: PREFETCH_TIMEOUT = 5.0
    externalMemorySyncBreakerThreshold = 5                // Python: _SYNC_BREAKER_THRESHOLD = 5
    externalMemorySyncBreakerCooldown = 120 * time.Second  // Python: _SYNC_BREAKER_COOLDOWN = 120.0
    externalMemoryShutdownTimeout     = 10 * time.Second   // Python: shutdown timeout=10.0
    externalMemoryPrefetchSection     = "external_memory_prefetch" // Python: EXTERNAL_MEMORY_PREFETCH_SECTION
)
```

### 结构体

```go
type ExternalMemoryRail struct {
    rails.DeepAgentRail
    provider                   ext.MemoryProvider
    userID                     string
    scopeID                    string
    sessionID                  string
    initialized                bool
    ownedToolNames             map[string]struct{}
    ownedToolIDs               map[string]struct{}
    systemPromptBuilder        saprompt.SystemPromptBuilderInterface
    prefetchCache              *string
    prefetchInvokeID           uintptr
    syncMu                     sync.Mutex          // 保护 syncTurn 串行
    syncDone                   chan struct{}        // 上一次 sync 完成信号
    syncConsecutiveFailures    int
    syncBreakerUntil           time.Time
}
```

### 方法列表

| Go 方法 | Python 方法 | 说明 |
|---------|------------|------|
| `NewExternalMemoryRail` | `__init__` | 构造函数 |
| `Init` | `init` | 注册 Provider 工具 + 注入 system_prompt_block |
| `Uninit` | `uninit` | 注销工具 + 关闭 Provider |
| `BeforeInvoke` | `before_invoke` | 调 provider.Initialize() |
| `BeforeModelCall` | `before_model_call` | 调 provider.Prefetch() 注入记忆上下文 |
| `AfterInvoke` | `after_invoke` | 调 provider.SyncTurn()（序列化 + 熔断器） |
| `registerProviderTools` | `_register_provider_tools` | 将 Provider 工具注册到 Agent |
| `resolveUserTextForMemory` | `_resolve_user_text_for_memory` | 从回调上下文解析用户文本 |
| `extractAssistantOutput` | `_extract_assistant_output` | 从回调上下文提取助手输出 |
| `isBackgroundRun` | `_is_background_run` | 判断是否后台运行 |
| `buildMemoryContextBlock` | `_build_memory_context_block` | 构建 `<memory-context>` 包裹块 |

### syncTurn 序列化方案

Python 用 `asyncio.Task` + `await asyncio.shield()` 实现序列化 sync_turn。Go 用 `sync.Mutex` + done channel：

```go
// AfterInvoke 中：
//   1. 加锁检查熔断器
//   2. 等待上一次 sync 完成（5s 超时，对齐 Python）
//   3. 启动新 goroutine 执行 provider.SyncTurn()
//   4. 更新 syncDone channel
```

Go 的 goroutine 天然并发，用 done channel 确保串行等待，比 Python 的 `asyncio.shield` 更简单。

### prefetchCache 策略

对齐 Python：
- `BeforeInvoke` 时清空缓存（`prefetchCache = nil`）
- `BeforeModelCall` 时检查是否同一 invoke（比较 `prefetchInvokeID`），命中缓存直接用
- 未命中则调 `provider.Prefetch()`，5s 超时
- 结果注入为 `<memory-context>` section（Priority 55）

## DeepAdapter 回填方案（本次不修改，设计预案）

7.13 完成后，DeepAdapter 的 `⤵️ 10.6.3-10` 标记需要回填：

| 文件 | ⤵️ 位置 | 回填内容 |
|------|---------|---------|
| `deep_adapter_rails.go:497` | `buildExternalMemoryRail()` 返回 nil | 构造 ExternalMemoryRail 实例 |
| `deep_adapter_rails.go:770` | `updatePlanModeRails` 中 | 实现 `handleExternalMemoryRailByConfig` |
| `deep_adapter_rails.go:911` | `updateAgentModeRails` 中 | 同上，agent 模式分支 |

回填逻辑设计：

```go
func (d *DeepAdapter) buildExternalMemoryRail() sainterfaces.AgentRail {
    engine := getMemoryEngine(d.configCache)
    if engine != "cloud" && engine != "both" {
        return nil
    }
    // 7.14-7.17 完成后根据配置选择 Provider
    var provider ext.MemoryProvider
    // switch providerType { ... }
    if provider == nil || !provider.IsAvailable() {
        return nil
    }
    return memory.NewExternalMemoryRail(provider, d.userID, d.scopeID, d.sessionID)
}

func (d *DeepAdapter) handleExternalMemoryRailByConfig(ctx context.Context) {
    engine := getMemoryEngine(d.configCache)
    shouldHave := (engine == "cloud" || engine == "both")
    if shouldHave && d.externalMemoryRail == nil {
        rail := d.buildExternalMemoryRail()
        if rail != nil && d.instance != nil {
            d.instance.RegisterRail(ctx, rail)
            d.externalMemoryRail = rail
            d.externalMemoryRailRegistered = true
        }
    } else if !shouldHave && d.externalMemoryRail != nil {
        if d.instance != nil {
            d.instance.UnregisterRail(ctx, d.externalMemoryRail)
        }
        d.externalMemoryRail = nil
        d.externalMemoryRailRegistered = false
    }
}
```

**回填时机**：7.14-7.17 至少一个 Provider 完成后，否则 `buildExternalMemoryRail()` 始终返回 nil。

## 测试策略

### 测试文件

| 测试文件 | 覆盖内容 |
|----------|---------|
| `memory/external/provider_test.go` | ToolSchema 序列化、ProviderOption 应用、BaseMemoryProvider 4 个默认方法、applyOptions |
| `harness/rails/memory/external_memory_rail_test.go` | 用 fakeProvider 测试完整生命周期 |

### fakeProvider（测试专用）

```go
type fakeProvider struct {
    ext.BaseMemoryProvider
    name           string
    available      bool
    initialized    bool
    toolSchemas    []ext.ToolSchema
    prefetchResult string
    prefetchErr    error
    syncTurnCalls  []syncTurnRecord
    shutdownCalled bool
}
```

### 关键测试用例

1. `TestExternalMemoryRail_Init_注册工具和提示词`
2. `TestExternalMemoryRail_BeforeInvoke_初始化Provider`
3. `TestExternalMemoryRail_BeforeModelCall_Prefetch注入`
4. `TestExternalMemoryRail_BeforeModelCall_Prefetch超时`
5. `TestExternalMemoryRail_AfterInvoke_SyncTurn序列化`
6. `TestExternalMemoryRail_AfterInvoke_熔断器`
7. `TestExternalMemoryRail_AfterInvoke_跳过后台运行`
8. `TestExternalMemoryRail_Uninit_注销和关闭`
9. `TestBuildMemoryContextBlock`
10. `TestResolveUserTextForMemory`
11. `TestExtractAssistantOutput`

### 覆盖率目标

- `memory/external/` — ≥ 90%（纯逻辑，无外部依赖）
- `harness/rails/memory/` (新增部分) — ≥ 85%（fakeProvider mock，无真实外部调用）

## 产出汇总

| 产出 | 文件 |
|------|------|
| MemoryProvider 接口 + BaseMemoryProvider + ProviderOption | `internal/agentcore/memory/external/provider.go` |
| 包文档 | `internal/agentcore/memory/external/doc.go` |
| Provider 接口测试 | `internal/agentcore/memory/external/provider_test.go` |
| ExternalMemoryRail | `internal/agentcore/harness/rails/memory/external_memory_rail.go` |
| ExternalMemoryRail 测试 | `internal/agentcore/harness/rails/memory/external_memory_rail_test.go` |
| doc.go 更新 | `internal/agentcore/harness/rails/memory/doc.go` |

DeepAdapter 回填（`⤵️ 10.6.3-10`）不在本次范围。
