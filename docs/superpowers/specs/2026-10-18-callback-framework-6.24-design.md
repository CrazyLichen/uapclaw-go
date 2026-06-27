# 6.24 AsyncCallbackFramework 全量实现设计

> 对齐 Python `openjiuwen/core/runner/callback/` 全量实现，回填 `framework.go` 中 8 个 `⤵️` 占位符

## 1. 概述

### 1.1 流程位置

```
用户请求 → Controller/Runner
              ↓
         ┌─────────────────────────────────────────┐
         │  CallbackFramework (6.24)               │  ← Runner 编排层核心基础设施
         │  事件拦截/变换/通知/过滤/熔断/指标        │
         └─────────────────────────────────────────┘
              ↓ emit_before 触发
         ┌──────────────┐
         │  LLM / Tool / Agent / Workflow 调用     │
         └──────────────┘
              ↓ emit_after 触发
         结果返回
```

6.24 位于 ResourceMgr (6.23) 之上、Runner 单例 (6.25) 之下，是所有业务调用的横切基础设施。

### 1.2 核心作用

1. **事件通知（Observer 模式）**：LLM/Tool/Agent 调用前后触发事件，外部观测者无需侵入业务代码
2. **IO 变换（transform_io）**：调用前后对输入/输出做变换（脱敏、格式转换、缓存注入）
3. **过滤器/熔断器**：回调执行前拦截（限流、熔断、鉴权），保护系统稳定性
4. **链式执行**：按优先级顺序执行回调，支持回滚、重试、超时
5. **指标记录**：记录每个回调的执行次数、耗时、错误率

### 1.3 已实现 vs 待实现

**已实现：**
- 多域注册/触发（LLM/Tool/Session/Context/GlobalAgent/Custom/PerAgent）
- CallbackInfo[F] 泛型包装
- Functional Options（Priority, Once, Namespace, Tags, etc.）
- transform_io（LLM/Agent/Tool 三层 Register + Apply）
- emit_before/emit_after 模式（内联在 LLM/Tool/Agent 调用点）

**待实现（6.24 回填）：**
- BEFORE/AFTER/ERROR 生命周期钩子
- 过滤器管线（全局→事件→回调级三级）
- 熔断器检查
- 回调级超时控制（context.WithTimeout）
- 回调级重试逻辑（max_retries + retry_delay）
- 指标记录（CallbackMetrics）
- 回调链（CallbackChain + rollback/retry/error_handler）
- AbortError（中止触发流程）
- 事件定义补充（Workflow/AgentTeam/Retrieval/Memory/TaskManager 域）
- scope 工具函数（BuildEventName/ParseEventName）

## 2. 设计决策

| 决策项 | 结论 | 理由 |
|--------|------|------|
| 实现方案 | 对齐 Python 全量 | 用户选择 |
| 装饰器 | 不实现，沿用调用点内联 | Go 无装饰器语法，当前内联方式工作正常 |
| 并发模型 | context 驱动，回调同步执行 | 超时/取消通过 ctx 控制，与 Go 惯例一致 |
| 枚举值 | 现有枚举名不动，值已是 scope 格式；新增域用 scope 字符串 | 当前 `_framework:xxx` 格式与 Python 一致 |
| Scope 体系 | 仅实现 BuildEventName/ParseEventName 工具函数 | Python 所有内置事件都用 `_framework` scope，无需自动重写 |
| CallbackChain | 独立结构体，新建 chain.go | 与 Python chain.py 一一对应 |
| 过滤器接口 | `Filter(ctx, event, callbackName string, data any) → FilterResult` | data 可以是类型化结构体或 map[string]any，完全对齐 Python 签名 |
| 指标并发安全 | CallbackMetrics 内置 `sync.Mutex` | 简单直接，与 Go 惯例一致 |
| AbortError | 复用已有 `StatusCallbackExecutionAborted`，Cause 决定传播行为 | 与 Python `raise e.cause` / `raise` 两种路径一致 |
| LLM 日志 | 删除 logging.go，在 model_client 调用点内联记录 | 对齐 Python 的 `llm_logger` 内联模式 |
| 文件组织 | 一一对齐 Python 文件结构 | 用户要求 |

## 3. 文件组织

### 3.1 对齐 Python 的文件映射

| Go 文件 | 对应 Python | 内容 |
|---------|------------|------|
| `doc.go` | — | 包文档（Go 特有） |
| `framework.go` | `framework.py` | CallbackFramework 核心（注册/触发/注销/钩子/指标/熔断器/历史） |
| `enums.go` | `enums.py` | FilterAction / ChainAction / HookType |
| `models.go` | `models.py` | CallbackMetrics / FilterResult / ChainContext / ChainResult / CallbackInfo[F] |
| `events.go` | `events.py` | EventBase + scope 工具函数 + 所有事件枚举 + EventData + 函数类型 |
| `filters.go` | `filters.py` | EventFilter 接口 + 7 种过滤器实现 |
| `chain.go` | `chain.py` | CallbackChain 结构体 + Execute/Rollback |
| `errors.go` | `errors.py` | AbortError |
| `utils.go` | `utils.py` | GetCallbackFramework + trigger 便捷函数 |
| `options.go` | — | CallbackOption（Go 特有 Functional Options 模式） |

### 3.2 需删除的文件

| 文件 | 原因 |
|------|------|
| `callback_info.go` | 内容移入 `models.go`，与 Python `models.py` 对齐 |
| `logging.go` | 删除集中化日志，改为 model_client 内联（对齐 Python `llm_logger` 模式） |
| `logging_test.go` | 随 `logging.go` 一起删除 |

### 3.3 framework.go 内容拆分

当前 `framework.go` 承载了核心结构体 + 全局单例 + TransformIO + triggerCallbacks。
拆分后：

| 内容 | 目标文件 |
|------|---------|
| `globalCallbackFramework` + `GetCallbackFramework()` | `utils.go` |
| 其余全部保留 | `framework.go` |

## 4. 各模块详细设计

### 4.1 enums.go（对应 Python enums.py）

```go
// FilterAction 过滤器动作，控制回调是否执行
type FilterAction string

const (
    FilterActionContinue FilterAction = "continue"  // 正常执行
    FilterActionStop     FilterAction = "stop"      // 停止整个事件处理
    FilterActionSkip     FilterAction = "skip"      // 跳过当前回调
    FilterActionModify   FilterAction = "modify"    // 修改参数后继续
)

// ChainAction 链式执行动作，控制回调链流程
type ChainAction string

const (
    ChainActionContinue ChainAction = "continue"  // 继续下一个回调
    ChainActionBreak    ChainAction = "break"     // 中断链，返回当前结果
    ChainActionRetry    ChainAction = "retry"     // 重试当前回调
    ChainActionRollback ChainAction = "rollback"  // 回滚所有已执行回调
)

// HookType 生命周期钩子类型
type HookType string

const (
    HookTypeBefore  HookType = "before"   // 事件处理前
    HookTypeAfter   HookType = "after"    // 事件处理后
    HookTypeError   HookType = "error"    // 出错时
    HookTypeCleanup HookType = "cleanup"  // 清理阶段
)
```

### 4.2 models.go（对应 Python models.py）

合并原 `callback_info.go` 中的 `CallbackInfo[F]`，新增 4 个模型：

```go
// CallbackMetrics 回调执行指标，记录调用次数、耗时、错误率。
// 并发安全：内部使用 sync.Mutex 保护所有字段。
type CallbackMetrics struct {
    mu           sync.Mutex
    CallCount    int
    TotalTime    float64
    MinTime      float64
    MaxTime      float64
    ErrorCount   int
    LastCallTime time.Time
}

// Update 记录一次执行
func (m *CallbackMetrics) Update(executionTime float64, isError bool)

// AvgTime 平均执行时间
func (m *CallbackMetrics) AvgTime() float64

// ToDict 序列化为 map
func (m *CallbackMetrics) ToDict() map[string]any

// FilterResult 过滤器返回结果
type FilterResult struct {
    Action        FilterAction
    ModifiedData  any    // FilterActionModify 时使用
    Reason        string
}

// ChainContext 链式执行上下文
type ChainContext struct {
    Event         string
    InitialData   any
    Results       []any
    Metadata      map[string]any
    CurrentIndex  int
    IsCompleted   bool
    IsRolledBack  bool
    StartTime     time.Time
}

// GetLastResult 获取最后一个结果
func (c *ChainContext) GetLastResult() any

// ElapsedTime 已耗时
func (c *ChainContext) ElapsedTime() time.Duration

// ChainResult 链式执行结果
type ChainResult struct {
    Action  ChainAction
    Result  any
    Context *ChainContext
    Error   error
}

// CallbackInfo[F] 泛型回调包装（从原 callback_info.go 移入）
// 字段与 Python CallbackInfo dataclass 对齐
type CallbackInfo[F any] struct {
    Callback     F
    Priority     int
    Once         bool
    Enabled      bool
    Namespace    string
    Tags         []string
    MaxRetries   int
    RetryDelay   float64
    Timeout      float64
    CreatedAt    time.Time
    Wrapper      any
    CallbackType string
}
```

### 4.3 errors.go（对应 Python errors.py）

```go
// AbortError 回调执行中止错误，在回调内部触发以中止整个 trigger 流程。
//
// 对应 Python: openjiuwen/core/runner/callback/errors.py (AbortError)
//
// 传播逻辑（与 Python 对齐）：
//   - Cause != nil → trigger 返回 Cause（对调用方透明，AbortError 仅作为包装器）
//   - Cause == nil → trigger 返回 AbortError 本身
type AbortError struct {
    // base 内嵌 BaseError，复用异常体系
    base *exception.BaseError
    // Reason 中止原因
    Reason string
    // Cause 原始错误
    Cause error
    // Details 额外详情
    Details any
}

func NewAbortError(reason string, cause error) *AbortError
func (e *AbortError) Error() string
func (e *AbortError) Unwrap() error  // 支持 errors.Unwrap/is/As
```

### 4.4 events.go 补充（对应 Python events.py）

在现有事件枚举基础上补充：

```go
// scope 工具函数

const DefaultScope = "_framework"

// BuildEventName 构建带 scope 的事件名
// 对应 Python: build_event_name(scope, event_name)
func BuildEventName(scope, eventName string) string

// ParseEventName 解析带 scope 的事件名，返回 (scope, eventName)
// 对应 Python: parse_event_name(scoped_event)
func ParseEventName(scopedEvent string) (scope, eventName string)

// EventBase 事件基类（提供 scope 支持）
type EventBase struct {
    Scope string
}

// GetEvent 获取带 scope 的完整事件名
func (e *EventBase) GetEvent(eventName string) string
```

**新增事件域枚举：**

| 域 | 枚举类型 | 事件数 | Python 对应 |
|---|---|---|---|
| Workflow | `WorkflowEventType` | 16 | WorkflowEvents |
| AgentTeam | `AgentTeamEventType` | 2 | AgentTeamEvents |
| Retrieval | `RetrievalEventType` | 1 | RetrievalEvents |
| Memory | `MemoryEventType` | 5 | MemoryEvents |
| TaskManager | `TaskManagerEventType` | 6 | TaskManagerEvents |

所有新枚举值的格式为 `"_framework:{event_name}"`，与现有枚举一致。

同时新增各域的 EventData 结构体和回调函数类型，以及 framework.go 中对应的 On/Off/Trigger 方法。

### 4.5 filters.go（对应 Python filters.py）

```go
// EventFilter 事件过滤器接口
type EventFilter interface {
    // Name 过滤器名称
    Name() string
    // Filter 执行过滤逻辑
    //   event: 事件名（scope 格式）
    //   callbackName: 回调函数名
    //   data: 事件数据（类型化结构体或 map[string]any）
    Filter(ctx context.Context, event string, callbackName string, data any) FilterResult
}
```

**7 种过滤器实现：**

| 过滤器 | 字段 | Filter 逻辑 |
|--------|------|------------|
| `RateLimitFilter` | maxCalls, timeWindow, calls(deque), mu | 超限返回 SKIP |
| `CircuitBreakerFilter` | failureThreshold, timeout, failures, isOpen, lastFailureTime, mu | 断路返回 SKIP，超时后尝试重置 |
| `ValidationFilter` | validator func(any) bool | 校验失败返回 SKIP |
| `LoggingFilter` | — | 记录日志，返回 CONTINUE |
| `AuthFilter` | requiredRole string | 角色不匹配返回 SKIP |
| `ParamModifyFilter` | modifier func(any) any | 返回 MODIFY + 修改后数据 |
| `ConditionalFilter` | condition func(...) bool, actionOnFalse FilterAction | 条件为假返回 actionOnFalse |

CircuitBreakerFilter 额外提供 `RecordSuccess` / `RecordFailure` 方法。

### 4.6 chain.go（对应 Python chain.py）

```go
// CallbackChain 顺序回调执行链，支持回滚、重试和错误处理。
//
// 对应 Python: openjiuwen/core/runner/callback/chain.py (CallbackChain)
type CallbackChain struct {
    // Name 链标识
    Name string
    // callbacks 回调列表（按优先级降序排列）
    callbacks []*CallbackInfo[any]
    // rollbackHandlers 回滚处理器（回调 → 回滚函数）
    rollbackHandlers map[any]func(ctx context.Context, cctx *ChainContext) error
    // errorHandlers 错误处理器（回调 → 错误处理函数）
    errorHandlers map[any]func(ctx context.Context, cctx *ChainContext, err error) (ChainAction, error)
    // mu 并发读写锁
    mu sync.RWMutex
}

// Add 添加回调到链中
func (c *CallbackChain) Add(info *CallbackInfo[any], rollbackHandler, errorHandler any)

// Remove 移除回调
func (c *CallbackChain) Remove(callback any)

// Execute 执行回调链
// 按优先级降序执行，上一个回调结果作为下一个的输入。
// 支持 Break/Retry/Rollback 三种控制动作。
// 重试逻辑：maxRetries + retryDelay
// 超时逻辑：context.WithTimeout
// 出错时逆序执行 rollbackHandlers
func (c *CallbackChain) Execute(ctx context.Context, context *ChainContext) *ChainResult

// Rollback 逆序执行回滚处理器
func (c *CallbackChain) Rollback(ctx context.Context, context *ChainContext, executedCallbacks []*CallbackInfo[any]) error
```

### 4.7 utils.go（对应 Python utils.py）

```go
// 全局单例（从 framework.go 移入）
var globalCallbackFramework = NewCallbackFramework()

// GetCallbackFramework 获取全局回调框架实例
func GetCallbackFramework() *CallbackFramework

// Trigger 便捷触发函数
// 对应 Python: openjiuwen/core/runner/callback/utils.py trigger()
func Trigger(ctx context.Context, event string, data map[string]any) []any
```

### 4.8 framework.go 回填

在 `triggerCallbacks` 函数中回填 8 个占位符：

```
BEFORE 钩子 → 遍历回调 → {
    跳过 disabled / transform 类型
    过滤器检查（全局 → 事件 → 回调级三级管线）
    熔断器检查
    超时控制（context.WithTimeout，按 CallbackInfo.Timeout 设置）
    重试逻辑（maxRetries + retryDelay 循环）
    → 执行回调
    → 成功：指标记录（is_error=False）、熔断器记录成功
    → 失败：检查 AbortError（Cause 传播逻辑）、指标记录（is_error=True）、
            熔断器记录失败、ERROR 钩子执行
    → Once 回调标记 disabled
} → AFTER 钩子
```

**新增字段到 CallbackFramework：**

```go
type CallbackFramework struct {
    // ...现有字段...

    // hooks 生命周期钩子（事件 → HookType → 钩子函数列表）
    hooks map[string]map[HookType][]func(ctx context.Context, event string, data any)
    // filters 事件级过滤器（事件 → 过滤器列表）
    filters map[string][]EventFilter
    // globalFilters 全局过滤器
    globalFilters []EventFilter
    // callbackFilters 回调级过滤器（回调 → 过滤器列表）
    callbackFilters map[any][]EventFilter
    // metrics 执行指标（"{event}:{callbackName}" → CallbackMetrics）
    metrics map[string]*CallbackMetrics
    // circuitBreakers 熔断器（"{event}:{callbackName}" → CircuitBreakerFilter）
    circuitBreakers map[string]*CircuitBreakerFilter
    // enableEventHistory 是否启用事件历史
    enableEventHistory bool
    // eventHistory 事件历史记录
    eventHistory deque[any]  // 或用环形缓冲区
    // enableMetrics 是否启用指标
    enableMetrics bool
}
```

**新增方法：**

| 方法 | 对应 Python | 说明 |
|------|------------|------|
| `AddFilter(event, filter)` | `add_filter` | 添加事件级过滤器 |
| `AddGlobalFilter(filter)` | `add_global_filter` | 添加全局过滤器 |
| `AddCircuitBreaker(event, callback, threshold, timeout)` | `add_circuit_breaker` | 添加熔断器 |
| `AddHook(event, hookType, hook)` | `add_hook` | 添加生命周期钩子 |
| `TriggerChain(ctx, event, data)` | `trigger_chain` | 链式触发 |
| `TriggerParallel(ctx, event, data)` | `trigger_parallel` | 并发触发（errgroup） |
| `TriggerUntil(ctx, event, condition, data)` | `trigger_until` | 触发直到条件满足 |
| `TriggerWithTimeout(ctx, event, timeout, data)` | `trigger_with_timeout` | 带总超时的触发 |
| `GetMetrics(event, callbackName)` | `get_metrics` | 查询指标 |
| `ResetMetrics()` | `reset_metrics` | 重置指标 |
| `GetSlowCallbacks(threshold)` | `get_slow_callbacks` | 查询慢回调 |
| `EnableEventHistory(enabled)` | `enable_event_history` | 开关事件历史 |
| `GetEventHistory(event, since)` | `get_event_history` | 查询事件历史 |
| `GetStatistics()` | `get_statistics` | 框架统计信息 |
| `SaveState(filepath)` | `save_state` | 保存状态到 JSON |

## 5. 删除 logging.go 影响分析

### 5.1 需清理的代码

| 文件 | 清理内容 |
|------|---------|
| `framework.go` L147-155 | 删除 `NewCallbackFramework` 中 9 行 `OnLLM(*, LoggingLLMCallback)` 注册 |
| `logging.go` | 整个文件删除 |
| `logging_test.go` | 整个文件删除 |
| `framework_test.go` | 调整引用 `LoggingLLMCallback` 的测试用例（L175, L193, L273, L814, L871） |

### 5.2 LLM 日志替代方案

对齐 Python 的 `llm_logger` 内联模式：
- Python：在 `base_model_client.py` / `openai_model_client.py` 中直接调用 `llm_logger.info/error`
- Go：在 `internal/agentcore/foundation/llm/` 的 model client 调用点使用 `logger.Info(logger.ComponentAgentCore)` 记录

当前 Go 的 model client 中**已经有内联日志**（与 Python 一致），`LoggingLLMCallback` 是额外的集中化日志，删除后不会丢失日志能力。

## 6. 测试策略

每个新增文件配备 `_test.go`，覆盖率 ≥ 85%：

| 文件 | 测试重点 |
|------|---------|
| `enums_test.go` | 枚举值字符串验证 |
| `models_test.go` | CallbackMetrics 并发安全 + FilterResult/ChainContext/ChainResult 构造 |
| `errors_test.go` | AbortError 传播逻辑（Cause 有/无）+ errors.As/Is |
| `events_test.go` | BuildEventName/ParseEventName + 新增域枚举值 |
| `filters_test.go` | 7 种过滤器逻辑 + FilterResult 动作 |
| `chain_test.go` | 顺序执行/优先级/Break/Retry/Rollback/超时/错误处理 |
| `utils_test.go` | 全局单例 + Trigger 便捷函数 |
| `framework_test.go` | 回填后的 triggerCallbacks 完整流程（钩子/过滤器/熔断器/指标/AbortError） |
