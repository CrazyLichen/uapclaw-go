# 步骤 6.6 AgentCallbackManager + CallbackInfo 统一包装 设计文档

## 概述

本文档描述实现步骤 6.6 AgentCallbackManager（回调管理器）的设计方案，以及对现有 CallbackFramework 的统一改造：所有域加 CallbackInfo 泛型包装、全局 Agent 域重命名、新增 PerAgent 域。

对应 Python 源码：`openjiuwen/core/single_agent/agent_callback_manager.py`

---

## 1. AgentCallbackManager 在 Agent 会话流程中的位置

### 1.1 构建流程位置

```
6.4 AgentCallbackEvent (✅) ── 10种生命周期事件枚举
         ↓
6.5 AgentCallbackContext (✅) ── 回调上下文数据载体（含 Fire/Retry/ForceFinish 预留占位）
         ↓
6.6 AgentCallbackManager (☐) ── ★ 当前步骤：回调注册/注销/执行管理器
         ↓
6.x Rail 系统 (6.7-6.10) ── 基于管理器的生命周期钩子体系
         ↓
6.x ReAct Agent (6.11-6.12) ── 使用完整回调框架的 Agent 实现
```

### 1.2 作用

| 方向 | 作用 |
|------|------|
| 承上 | 为 AgentCallbackContext.Fire() 提供实际执行逻辑（6.5 预留的 panic 占位需要 6.6 回填） |
| 启下 | 为后续 Rail 系统（6.7-6.10）提供 register_rail/unregister_rail 的批量注册能力 |
| 核心职责 | 事件名命名空间隔离（{agentID}_{event}）、回调注册/注销路由、执行调度（委托给全局 CallbackFramework） |
| 设计定位 | 不存储回调本身（存储在全局 CallbackFramework 中），只做路由层——将 per-Agent 事件映射到全局框架 |

### 1.3 两层事件体系

| 层次 | Go 类型 | 事件数 | 用途 |
|------|---------|--------|------|
| 框架级全局观测 | GlobalAgentEventType（重命名后） | 5 | 日志/监控/transform_io/observability（不干预控制流） |
| 实例级 Rail 拦截 | rail.AgentCallbackEvent | 10 | 重试/提前终止/steering/安全拦截（可干预控制流） |

两者互不桥接，各自独立触发，与 Python 保持一致。

---

## 2. CallbackInfo 泛型包装

### 2.1 类型定义

```go
// CallbackInfo 回调注册信息，包装回调函数及其元数据。
//
// 对应 Python: CallbackInfo (openjiuwen/core/runner/callback/models.py)
type CallbackInfo[F any] struct {
    // Callback 回调函数
    Callback F
    // Priority 执行优先级（降序，数值越大越先执行）
    Priority int
    // Once 是否只执行一次（执行后自动禁用）
    Once bool
    // Enabled 是否启用
    Enabled bool
    // Namespace 命名空间（用于分组注销）
    Namespace string
    // Tags 标签集合（用于过滤）
    Tags []string
    // MaxRetries 失败后最大重试次数
    MaxRetries int
    // RetryDelay 重试间隔（秒）
    RetryDelay float64
    // Timeout 执行超时（秒），0 表示不限
    Timeout float64
    // CreatedAt 注册时间戳（秒）
    CreatedAt float64
    // Wrapper 装饰器 wrapper 函数（用于反注册）
    Wrapper F
    // CallbackType 语义类型标记（如 "transform"）
    CallbackType string
}
```

### 2.2 Functional Options

```go
type CallbackOption func(*callbackOptionConfig)

func WithPriority(p int) CallbackOption
func WithOnce() CallbackOption
func WithNamespace(ns string) CallbackOption
func WithTags(tags ...string) CallbackOption
func WithMaxRetries(n int) CallbackOption
func WithRetryDelay(d float64) CallbackOption
func WithTimeout(t float64) CallbackOption
func WithCallbackType(t string) CallbackOption
```

### 2.3 排序逻辑

```go
// sortCallbacks 按 Priority 降序排列，相同 Priority 按 CreatedAt 升序排列（先注册的先执行）。
func sortCallbacks[F any](callbacks []*CallbackInfo[F]) {
    sort.SliceStable(callbacks, func(i, j int) bool {
        if callbacks[i].Priority != callbacks[j].Priority {
            return callbacks[i].Priority > callbacks[j].Priority  // 降序
        }
        return callbacks[i].CreatedAt < callbacks[j].CreatedAt     // 升序
    })
}
```

### 2.4 Python CallbackInfo 字段对照

| Python 字段 | Go 字段 | 说明 |
|------------|---------|------|
| callback | Callback | 核心字段 |
| priority | Priority | 优先级排序 |
| once | Once | 一次性执行 |
| enabled | Enabled | 启用/禁用 |
| namespace | Namespace | 命名空间分组 |
| tags | Tags | 标签过滤 |
| max_retries | MaxRetries | 回调级重试 |
| retry_delay | RetryDelay | 重试间隔 |
| timeout | Timeout | 执行超时 |
| created_at | CreatedAt | 注册时间戳 |
| wrapper | Wrapper | 装饰器 wrapper |
| callback_type | CallbackType | 语义类型标记 |

---

## 3. 全局 Agent 域重命名

### 3.1 类型重命名

| 现有 | 改为 |
|------|------|
| AgentCallGlobalEventType | GlobalAgentEventType |
| AgentCallbackFunc | GlobalAgentCallbackFunc |
| AgentCallEventData | GlobalAgentEventData |

### 3.2 常量重命名

| 现有 | 改为 |
|------|------|
| AgentStarted | GlobalAgentStarted |
| AgentInvokeInput | GlobalAgentInvokeInput |
| AgentInvokeOutput | GlobalAgentInvokeOutput |
| AgentStreamInput | GlobalAgentStreamInput |
| AgentStreamOutput | GlobalAgentStreamOutput |

### 3.3 方法重命名

| 现有 | 改为 |
|------|------|
| OnAgent() | OnGlobalAgent() |
| OffAgent() | OffGlobalAgent() |
| TriggerAgent() | TriggerGlobalAgent() |

### 3.4 字段重命名

| 现有 | 改为 |
|------|------|
| agentCallbacks | globalAgentCallbacks |

### 3.5 不改的

| 名称 | 原因 |
|------|------|
| RegisterAgentTransformIO | 用户指定不改 |
| TransformAgentIOInput | 用户指定不改 |
| TransformAgentIOOutput | 用户指定不改 |
| agentTransformIO（内部字段） | 与 TransformIO 方法配套 |

---

## 4. PerAgent 域 API

### 4.1 类型定义（callback 包）

```go
// PerAgentCallbackFunc 实例级 PerAgent 回调函数类型。
// agentCallbackContext 实际类型为 *rail.AgentCallbackContext，回调内需类型断言。
//
// 对应 Python: AnyAgentCallback = Union[AgentCallback, SyncAgentCallback]
type PerAgentCallbackFunc func(ctx context.Context, agentCallbackContext any) error
```

### 4.2 CallbackFramework 方法

```go
// OnPerAgent 注册实例级 PerAgent 回调。
// event 格式为 "{agentID}_{event}"（如 "agent1_before_model_call"），由 AgentCallbackManager 构造。
func (fw *CallbackFramework) OnPerAgent(event string, fn PerAgentCallbackFunc, opts ...CallbackOption)

// OffPerAgent 注销指定事件上的单个 PerAgent 回调（按指针匹配）。
func (fw *CallbackFramework) OffPerAgent(event string, fn PerAgentCallbackFunc)

// OffAllPerAgent 清除指定事件上的所有 PerAgent 回调。
func (fw *CallbackFramework) OffAllPerAgent(event string)

// TriggerPerAgent 触发指定事件的所有 PerAgent 回调，按优先级顺序执行。
// agentCallbackContext 实际类型为 *rail.AgentCallbackContext。
// 任一回调返回 error 时停止后续执行并返回该 error。
func (fw *CallbackFramework) TriggerPerAgent(ctx context.Context, event string, agentCallbackContext any) error

// HasPerAgentHooks 检查指定事件是否有已注册的 PerAgent 回调。
func (fw *CallbackFramework) HasPerAgentHooks(event string) bool
```

### 4.3 CallbackFramework 结构体

```go
type CallbackFramework struct {
    mu                  sync.RWMutex
    llmCallbacks        map[LLMCallEventType][]*CallbackInfo[LLMCallbackFunc]
    toolCallbacks       map[ToolCallEventType][]*CallbackInfo[ToolCallbackFunc]
    sessionCallbacks    map[SessionCallEventType][]*CallbackInfo[SessionCallbackFunc]
    customCallbacks     map[string][]*CallbackInfo[CustomCallbackFunc]
    contextCallbacks    map[ContextCallEventType][]*CallbackInfo[ContextCallbackFunc]
    globalAgentCallbacks map[GlobalAgentEventType][]*CallbackInfo[GlobalAgentCallbackFunc]
    perAgentCallbacks   map[string][]*CallbackInfo[PerAgentCallbackFunc]        // 新增
    llmTransformIO      map[LLMCallEventType]*llmTransformIOEntry
    agentTransformIO    map[GlobalAgentEventType]*agentTransformIOEntry
    toolTransformIO     map[ToolCallEventType]*toolTransformIOEntry
}
```

---

## 5. 公共 Trigger 逻辑

### 5.1 执行策略

```go
type triggerStrategy int

const (
    // strategyCollect 收集所有返回值，不中断（观测型）
    strategyCollect triggerStrategy = iota
    // strategyAbortOnError 遇 error 中断（控制型）
    strategyAbortOnError
)
```

### 5.2 泛型公共触发方法

```go
// triggerCallbacks 泛型触发核心逻辑（CallbackFramework 非导出方法）。
func (fw *CallbackFramework) triggerCallbacks[F any, E comparable, D any](
    callbacksMap map[E][]*CallbackInfo[F],
    event E,
    data D,
    ctx context.Context,
    strategy triggerStrategy,
    execute func(F, context.Context, D) (any, error),
) ([]any, error) {
    if ctx == nil {
        return nil, nil
    }

    fw.mu.RLock()
    callbacks := callbacksMap[event]
    fw.mu.RUnlock()

    // ⤵️ 回填：BEFORE 钩子执行（对应 Python: _execute_hooks(event, HookType.BEFORE)）

    var results []any
    for _, info := range callbacks {
        if !info.Enabled {
            continue
        }
        if info.CallbackType == "transform" {
            continue
        }

        // ⤵️ 回填：过滤器检查（对应 Python: _apply_filters）
        // ⤵️ 回填：熔断器检查（对应 Python: _circuit_breakers）
        // ⤵️ 回填：回调级超时控制（对应 Python: trigger_with_timeout）
        // ⤵️ 回填：回调级重试（对应 Python: max_retries/retry_delay）

        result, err := execute(info.Callback, ctx, data)

        if err != nil {
            // ⤵️ 回填：ERROR 钩子执行（对应 Python: _execute_hooks(event, HookType.ERROR)）
            // ⤵️ 回填：指标记录（is_error=True）
            if strategy == strategyAbortOnError {
                return nil, err
            }
            continue
        }

        // ⤵️ 回填：指标记录（is_error=False）
        results = append(results, result)

        if info.Once {
            info.Enabled = false
        }
    }

    // ⤵️ 回填：AFTER 钩子执行（对应 Python: _execute_hooks(event, HookType.AFTER)）

    return results, nil
}
```

### 5.3 各域 Trigger 调用示例

```go
// TriggerLLM（观测型，收集返回值）
func (fw *CallbackFramework) TriggerLLM(ctx context.Context, event LLMCallEventType, data *LLMCallEventData) []any {
    results, _ := fw.triggerCallbacks(fw.llmCallbacks, event, data, ctx,
        strategyCollect,
        func(fn LLMCallbackFunc, ctx context.Context, data *LLMCallEventData) (any, error) {
            return fn(ctx, data), nil
        },
    )
    return results
}

// TriggerPerAgent（控制型，error 中断）
func (fw *CallbackFramework) TriggerPerAgent(ctx context.Context, event string, agentCallbackContext any) error {
    _, err := fw.triggerCallbacks(fw.perAgentCallbacks, event, agentCallbackContext, ctx,
        strategyAbortOnError,
        func(fn PerAgentCallbackFunc, ctx context.Context, data any) (any, error) {
            return nil, fn(ctx, data)
        },
    )
    return err
}
```

### 5.4 On* 方法改造示例

```go
func (fw *CallbackFramework) OnLLM(event LLMCallEventType, fn LLMCallbackFunc, opts ...CallbackOption) {
    cfg := applyCallbackOptions(opts...)
    info := &CallbackInfo[LLMCallbackFunc]{
        Callback:     fn,
        Priority:     cfg.Priority,
        Once:         cfg.Once,
        Enabled:      true,
        Namespace:    cfg.Namespace,
        Tags:         cfg.Tags,
        MaxRetries:   cfg.MaxRetries,
        RetryDelay:   cfg.RetryDelay,
        Timeout:      cfg.Timeout,
        CreatedAt:    float64(time.Now().UnixNano()) / 1e9,
        CallbackType: cfg.CallbackType,
    }

    fw.mu.Lock()
    defer fw.mu.Unlock()
    fw.llmCallbacks[event] = append(fw.llmCallbacks[event], info)
    sortCallbacks(fw.llmCallbacks[event])
}
```

---

## 6. AgentCallbackManager（rail 包）

### 6.1 结构体

```go
// AgentCallbackManager PerAgent 实例级回调管理器。
//
// 对应 Python: AgentCallbackManager (openjiuwen/core/single_agent/agent_callback_manager.py)
// 不自持回调存储，将注册/触发委托给全局 CallbackFramework，
// 通过 "{agentID}_{event}" 前缀实现命名空间隔离。
type AgentCallbackManager struct {
    // agentID Agent 唯一标识，用于构造事件名前缀
    agentID string
}
```

### 6.2 方法

```go
// NewAgentCallbackManager 创建回调管理器。
func NewAgentCallbackManager(agentID string) *AgentCallbackManager

// RegisterCallback 注册回调。
// 对应 Python: AgentCallbackManager.register_callback(event, callback, priority)
func (m *AgentCallbackManager) RegisterCallback(ctx context.Context, event AgentCallbackEvent, fn callback.PerAgentCallbackFunc, opts ...callback.CallbackOption)

// RegisterRail 批量注册一个 Rail 实例的所有回调。
// 对应 Python: AgentCallbackManager.register_rail(rail)
// ⤵️ 6.7 回填：rail 参数类型从 any 改为 AgentRail
func (m *AgentCallbackManager) RegisterRail(ctx context.Context, rail any, opts ...callback.CallbackOption) error

// UnregisterRail 批量注销一个 Rail 实例的所有回调。
// 对应 Python: AgentCallbackManager.unregister_rail(rail)
// ⤵️ 6.7 回填：rail 参数类型从 any 改为 AgentRail
func (m *AgentCallbackManager) UnregisterRail(ctx context.Context, rail any) error

// Unregister 注销指定事件上的单个回调。
// 对应 Python: AgentCallbackManager.unregister(event, callback)
func (m *AgentCallbackManager) Unregister(event AgentCallbackEvent, fn callback.PerAgentCallbackFunc)

// Clear 清除回调。不传 event 时清除所有事件的回调。
// 对应 Python: AgentCallbackManager.clear(event)
func (m *AgentCallbackManager) Clear(events ...AgentCallbackEvent)

// HasHooks 检查指定事件是否有已注册的回调。
// 对应 Python: AgentCallbackManager.has_hooks(event)
func (m *AgentCallbackManager) HasHooks(event AgentCallbackEvent) bool

// Execute 触发指定事件的所有回调。
// 对应 Python: AgentCallbackManager.execute(event, ctx)
func (m *AgentCallbackManager) Execute(ctx context.Context, event AgentCallbackEvent, railCtx *AgentCallbackContext) error

// getAgentEvent 生成带 agentID 前缀的事件名。
// 对应 Python: AgentCallbackManager._get_agent_event(event)
// 返回格式: "{agentID}_{event}"，如 "agent1_before_model_call"
func (m *AgentCallbackManager) getAgentEvent(event AgentCallbackEvent) string
```

### 6.3 Python 对齐对照

| Python | Go |
|--------|-----|
| register_callback(event, callback, priority) | RegisterCallback(ctx, event, fn, opts...) |
| register_rail(rail) | RegisterRail(ctx, rail, opts...) |
| unregister_rail(rail) | UnregisterRail(ctx, rail) |
| unregister(event, callback) | Unregister(event, fn) |
| clear(event) | Clear(events...) — 可变参数 |
| has_hooks(event) | HasHooks(event) |
| execute(event, ctx) | Execute(ctx, event, railCtx) |
| _get_agent_event(event) | getAgentEvent(event) |

---

## 7. 回填内容

### 7.1 AgentCallbackContext.Fire()

文件：`rail/context.go`

```go
// 改造前
func (c *AgentCallbackContext) Fire(_ AgentCallbackEvent) error {
    panic("TODO: 6.6 AgentCallbackManager")
}

// 改造后
func (c *AgentCallbackContext) Fire(event AgentCallbackEvent) error {
    c.event = event
    return c.agent.CallbackManager().(*AgentCallbackManager).Execute(c.context(), event, c)
}
```

### 7.2 AgentCallbackContext.FireLifecycle() 两处 Fire 调用

文件：`rail/context.go`

```go
// 改造前
_ = before // 占位
_ = after  // 占位

// 改造后
if err := c.Fire(before); err != nil {
    return err
}
// ...
_ = c.Fire(after) // 异常安全：忽略 after 阶段的错误
```

### 7.3 BaseAgent 接口

文件：`single_agent/interfaces/interface.go`

保持 `any` 类型避免循环依赖：

```go
// CallbackManager 返回回调管理器。
// 实际类型为 *rail.AgentCallbackManager，调用方需类型断言。
CallbackManager() any

// RegisterCallback 注册回调。
// event 实际类型为 rail.AgentCallbackEvent，fn 实际类型为 callback.PerAgentCallbackFunc。
RegisterCallback(ctx context.Context, event any, fn any, opts ...callback.CallbackOption) error

// RegisterRail 注册 Rail。
// rail 实际类型为 *rail.AgentRail（6.7 回填）。
RegisterRail(ctx context.Context, rail any, opts ...callback.CallbackOption) error
```

### 7.4 WarpBaseAgent

文件：`single_agent/base.go`

```go
// callbackManager 回调管理器
callbackManager *rail.AgentCallbackManager

func (w *WarpBaseAgent) CallbackManager() any { return w.callbackManager }

func (w *WarpBaseAgent) RegisterCallback(ctx context.Context, event any, fn any, opts ...callback.CallbackOption) error {
    w.callbackManager.RegisterCallback(ctx, event.(rail.AgentCallbackEvent), fn.(callback.PerAgentCallbackFunc), opts...)
    return nil
}

func (w *WarpBaseAgent) RegisterRail(ctx context.Context, rail any, opts ...callback.CallbackOption) error {
    return w.callbackManager.RegisterRail(ctx, rail, opts...)
}

func (w *WarpBaseAgent) UnregisterRail(ctx context.Context, rail any) error {
    return w.callbackManager.UnregisterRail(ctx, rail)
}
```

### 7.5 构造时初始化

```go
w.callbackManager = rail.NewAgentCallbackManager(w.card.ID)
```

---

## 8. 包依赖关系

```
callback 包 ← (无外部依赖，纯自包含)
    ↑
interfaces 包 ← callback（CallbackOption）
    ↑
rail 包 ← interfaces（BaseAgent）+ callback（OnPerAgent/TriggerPerAgent 等）
    ↑
single_agent/base.go ← rail（AgentCallbackManager）+ callback（TriggerGlobalAgent）+ interfaces（BaseAgent）
```

**全单向，无循环依赖** ✅

### 循环依赖避免策略

| 接口方法 | 类型策略 | 原因 |
|---------|---------|------|
| CallbackManager() any | any | 避免 interfaces → rail 循环 |
| RegisterCallback(ctx, event any, fn any, opts...) | event/fn 用 any | 避免 interfaces → rail 循环 |
| RegisterRail(ctx, rail any, opts...) | rail 用 any | 避免 interfaces → rail 循环（6.7 回填时需用接口/子包提取解决） |

---

## 9. 文件组织

### 9.1 新增文件

| 文件路径 | 说明 |
|----------|------|
| internal/agentcore/runner/callback/callback_info.go | CallbackInfo[F] 泛型结构体 |
| internal/agentcore/runner/callback/options.go | Functional Options 定义 |
| internal/agentcore/runner/callback/callback_info_test.go | CallbackInfo 测试 |
| internal/agentcore/runner/callback/options_test.go | Options 测试 |
| internal/agentcore/single_agent/rail/manager.go | AgentCallbackManager |
| internal/agentcore/single_agent/rail/manager_test.go | AgentCallbackManager 测试 |

### 9.2 修改文件

| 文件路径 | 变更内容 |
|----------|----------|
| internal/agentcore/runner/callback/doc.go | 更新包文档 |
| internal/agentcore/runner/callback/events.go | GlobalAgent 域重命名 + PerAgentCallbackFunc 定义 |
| internal/agentcore/runner/callback/framework.go | 所有域 map value 改为 []*CallbackInfo[F]；GlobalAgent 域重命名；PerAgent 域新增；triggerCallbacks 泛型公共方法；sortCallbacks |
| internal/agentcore/runner/callback/logging.go | 适配 OnLLM 新签名 |
| internal/agentcore/single_agent/rail/doc.go | 更新包文档 |
| internal/agentcore/single_agent/rail/context.go | 回填 Fire() + FireLifecycle() |
| internal/agentcore/single_agent/interfaces/interface.go | 回填 CallbackManager/RegisterCallback/RegisterRail 签名 |
| internal/agentcore/single_agent/base.go | 回填 callbackManager 类型 + 委托实现 + 初始化 |

### 9.3 测试文件适配

| 文件路径 | 变更内容 |
|----------|----------|
| internal/agentcore/runner/callback/events_test.go | 适配重命名 |
| internal/agentcore/runner/callback/framework_test.go | 适配重命名 + CallbackInfo 包装 + PerAgent 域测试 |
| internal/agentcore/runner/callback/logging_test.go | 适配 OnLLM 新签名 |
| internal/agentcore/single_agent/rail/context_test.go | 适配 Fire/FireLifecycle 回填 |
| internal/agentcore/single_agent/base_test.go | 适配 TriggerGlobalAgent 重命名 |

---

## 10. 影响范围总结

| 类别 | 范围 | 文件数 |
|------|------|--------|
| 新增文件 | callback_info.go, options.go, manager.go + 对应测试 | 6 |
| 重命名 | GlobalAgent 域类型/常量/方法 | events.go, framework.go + 所有引用点 |
| CallbackInfo 包装 | 所有域 map value 类型 + On/Off/Trigger 方法 | framework.go |
| PerAgent 域 | 新增字段 + 5个方法 + 公共 triggerCallbacks | framework.go |
| AgentCallbackManager | 新增结构体 + 8个方法 | manager.go（新） |
| 回填占位 | Fire(), FireLifecycle(), BaseAgent, WarpBaseAgent | context.go, interface.go, base.go |
| 测试适配 | 所有 callback 和 rail 测试 + base 测试 | ~6个测试文件 |
