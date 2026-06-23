# 6.5 AgentCallbackContext 设计

> 本文档描述 6.5 AgentCallbackContext 的 Go 实现设计。
> 对应 Python: `openjiuwen/core/single_agent/rail/base.py` 第 226-416 行。

## 1. 流程位置与作用

AgentCallbackContext 是 Rail 系统与 Agent 运行时之间的**核心中介对象**，位于 ReAct 循环内部：

```
用户请求
  → ReActAgent.Invoke()
    → 创建 AgentCallbackContext                      ← 6.5
    → FireLifecycle(BEFORE_INVOKE, AFTER_INVOKE)     ← 6.5（内部 fire 调用 6.6 回填）
      → ReAct 循环
        → ctx.DrainSteering()                        ← 6.5（完整实现）
        → ⤵️ 6.8: RailedExecute(BEFORE_MODEL_CALL, AFTER_MODEL_CALL, ON_MODEL_EXCEPTION)
          → ctx.Fire(BEFORE_MODEL_CALL)              ← 6.6 回填
          → 检查 HasForceFinishRequest               ← 6.10 回填
          → LLM 调用
          → ctx.Fire(AFTER_MODEL_CALL)               ← 6.6 回填
          → on exception → retry 循环                ← 6.10 回填
        → ctx.ConsumeForceFinish()                   ← 6.10 回填
        → ⤵️ 6.8: RailedExecute(BEFORE_TOOL_CALL, AFTER_TOOL_CALL, ON_TOOL_EXCEPTION)
          → ...（同上模式）
        → ctx.ConsumeForceFinish()                   ← 6.10 回填
      → 返回结果
    → AFTER_INVOKE
```

**承载三个核心控制机制：**

| 机制 | 触发方 | 消费方 | 说明 |
|------|--------|--------|------|
| Retry | Rail 钩子 `RequestRetry()` | `@rail` 包装器（6.8） | 失败后自动重试 |
| Force Finish | Rail 钩子 `RequestForceFinish()` | ReAct 循环 + `@rail` 包装器 | 提前终止并返回指定结果 |
| Steering | 外部 EventHandler `PushSteering()` | ReAct 循环 `DrainSteering()` | 运行时注入消息干预 Agent 行为 |

## 2. 与已有代码的层次关系

项目存在三层回调/生命周期机制，AgentCallbackContext 属于**第 2 层**：

| 层次 | Go 实现 | Python 对应 | 事件类型 | 作用 |
|------|---------|-------------|----------|------|
| 第 1 层：框架观测 | ✅ `LifecycleTool` + `CallbackFramework` | `_ToolMeta.__call__` | `ToolCallEventType`（11 种） | 日志/监控/TransformIO |
| **第 2 层：Agent Rail 控制** | ❌ 6.5-6.10 实现 | `AgentCallbackContext` + `@rail` | `AgentCallbackEvent`（10 种） | **retry/force_finish/steering** |
| 第 3 层：工具级钩子 | ✅ `ToolRail` 接口（空实现） | `ToolRail`（3.13） | 无事件 | 工具级 before/after/on_exception |

**两层事件不桥接，各自独立触发，与 Python 一致。**

## 3. 设计决策

| # | 决策点 | 选择 | 原因 |
|---|--------|------|------|
| 1 | Steering 队列 | 超大 buffered chan (4096)，非阻塞写入，满时 warn 日志丢弃 | Go 惯用并发原语；Python 用无界 asyncio.Queue + put_nowait，对齐"永不阻塞调用方"语义；4096 容量远超实际需求，丢弃比 Python QueueFull 崩溃更友好 |
| 2 | Lifecycle | `FireLifecycle(ctx, before, after, fn)` 函数 + defer | Go 没有 async context manager，函数+defer 是惯用替代；Python lifecycle 不含 retry/force_finish，只做 before/after + inputs 保存/恢复 + 异常设置 |
| 3 | Agent 引用 | `interfaces.BaseAgent` 接口 | 通过 `agent.CallbackManager()` 获取 manager；与现有接口体系一致；6.6 回填时 CallbackManager() 返回类型从 any 改为具体类型 |
| 4 | inputs 类型 | `EventInputs` 接口 + 各 Inputs struct 实现 | Go 无 Union 类型，接口 + type switch 是惯用模式；比 any 有更强的类型提示 |
| 5 | extra 类型 | `map[string]any` | 直接对齐 Python `Dict[str, Any]`；子 ctx 共享同一 map 引用（与 Python 一致） |
| 6 | 实现边界 | B2：结构体+字段+签名，方法体 panic | 6.5 严格只定义 AgentCallbackContext；fire/retry/force_finish 由 6.6/6.10 回填 |
| 7 | @rail 装饰器 | 不在本步讨论 | ⤵️ 6.8 预留注释 |

## 4. 结构体定义

```go
// AgentCallbackContext Rail 系统与 Agent 运行时之间的核心中介对象。
//
// 承载三个控制机制：Retry（重试）、Force Finish（提前终止）、Steering（外部注入）。
// 在 ReAct 循环中创建，跨事件生命周期持久存在（extra 字段）。
//
// 对应 Python: openjiuwen/core/single_agent/rail/base.py AgentCallbackContext (L226-416)
type AgentCallbackContext struct {
    // agent 当前 Agent 实例引用
    agent interfaces.BaseAgent
    // event 当前回调事件类型（由 Fire 设置）
    event AgentCallbackEvent
    // inputs 当前事件的输入数据（随事件变化）
    inputs EventInputs
    // config 运行时配置
    config interfaces.AgentConfig
    // session 当前 Session
    session *session.Session
    // modelContext 当前 ModelContext
    modelContext ce_interface.ModelContext
    // extra 跨 rail 通信字典（单次 invoke 内跨事件持久，子 ctx 共享）
    extra map[string]any
    // exception 异常对象（在错误事件上设置）
    exception error
    // retryAttempt 当前重试索引号
    retryAttempt int

    // retryRequest 重试请求信号
    // ⤵️ 6.10 回填：类型从 any 改为 *RetryRequest
    retryRequest any
    // forceFinishRequest 强制终止请求信号
    // ⤵️ 6.10 回填：类型从 any 改为 *ForceFinishRequest
    forceFinishRequest any
    // steeringQueue 外部注入的 steering 消息队列
    steeringQueue chan string
}
```

## 5. 方法列表

### 5.1 完整实现的方法（6.5 完成）

| 方法 | 签名 | 说明 |
|------|------|------|
| `BindSteeringQueue` | `(q chan string)` | 绑定外部 steering 队列 |
| `PushSteering` | `(msg string)` | 非阻塞推送 steering 消息，无队列时 no-op，满时 warn 丢弃 |
| `DrainSteering` | `() []string` | 非阻塞排空所有待处理 steering 消息 |
| `HasPendingSteering` | `() bool` | 检查是否有待处理的 steering 消息 |
| `SteeringQueue` | `() chan string` | 返回绑定的 steering 队列 |
| `FireLifecycle` | `(before, after AgentCallbackEvent, fn func() error) error` | before/after 事件生命周期包装，保存/恢复 inputs，捕获异常设 exception |
| `Agent` | `() interfaces.BaseAgent` | 返回 agent 引用 |
| `Event` | `() AgentCallbackEvent` | 返回当前事件 |
| `SetEvent` | `(event AgentCallbackEvent)` | 设置当前事件 |
| `Inputs` | `() EventInputs` | 返回当前 inputs |
| `SetInputs` | `(inputs EventInputs)` | 设置 inputs |
| `Config` | `() interfaces.AgentConfig` | 返回配置 |
| `Session` | `() *session.Session` | 返回 Session |
| `ModelContext` | `() ce_interface.ModelContext` | 返回 ModelContext |
| `Extra` | `() map[string]any` | 返回 extra 字典 |
| `Exception` | `() error` | 返回异常 |
| `SetException` | `(err error)` | 设置异常 |
| `RetryAttempt` | `() int` | 返回重试索引号 |
| `SetRetryAttempt` | `(attempt int)` | 设置重试索引号 |

### 5.2 预留方法（panic 占位，后续步骤回填）

| 方法 | 签名 | 占位行为 | 回填步骤 |
|------|------|----------|----------|
| `Fire` | `(event AgentCallbackEvent) error` | `panic("TODO: 6.6 AgentCallbackManager")` | 6.6 |
| `RequestRetry` | `(delaySeconds float64)` | `panic("TODO: 6.10 RetryRequest")` | 6.10 |
| `ConsumeRetryRequest` | `() any` | `panic("TODO: 6.10 RetryRequest")` | 6.10 |
| `RequestForceFinish` | `(result map[string]any)` | `panic("TODO: 6.10 ForceFinishRequest")` | 6.10 |
| `ConsumeForceFinish` | `() any` | `panic("TODO: 6.10 ForceFinishRequest")` | 6.10 |
| `HasForceFinishRequest` | `() bool` | `panic("TODO: 6.10 ForceFinishRequest")` | 6.10 |

## 6. EventInputs 接口与 Inputs 结构体

### 6.1 接口定义

```go
// EventInputs 回调事件输入接口。
//
// 各事件类型对应不同的 Inputs 结构体，均实现此接口。
// 调用方通过 type switch 获取具体类型。
//
// 对应 Python: EventInputs = Union[InvokeInputs, ModelCallInputs, ToolCallInputs, TaskIterationInputs, Dict]
type EventInputs interface {
    // EventKind 返回事件输入的种类标识
    EventKind() string
}
```

### 6.2 四个 Inputs 结构体（6.9 完善字段，6.5 前置声明接口 + 最小骨架）

```go
// InvokeInputs BEFORE/AFTER_INVOKE 事件输入。
// ⤵️ 6.9 回填字段
type InvokeInputs struct{}

func (i *InvokeInputs) EventKind() string { return "invoke" }

// ModelCallInputs BEFORE/AFTER_MODEL_CALL 事件输入。
// ⤵️ 6.9 回填字段
type ModelCallInputs struct{}

func (i *ModelCallInputs) EventKind() string { return "model_call" }

// ToolCallInputs BEFORE/AFTER_TOOL_CALL 事件输入。
// ⤵️ 6.9 回填字段
type ToolCallInputs struct{}

func (i *ToolCallInputs) EventKind() string { return "tool_call" }

// TaskIterationInputs BEFORE/AFTER_TASK_ITERATION 事件输入。
// ⤵️ 6.9 回填字段
type TaskIterationInputs struct{}

func (i *TaskIterationInputs) EventKind() string { return "task_iteration" }
```

## 7. Steering 实现细节

### 7.1 队列常量

```go
const (
    // steeringQueueSize steering 队列缓冲区大小
    // Python 用无界 asyncio.Queue，Go 用大容量 buffered chan 对齐
    steeringQueueSize = 4096
)
```

### 7.2 PushSteering 伪代码

```
func (c *AgentCallbackContext) PushSteering(msg string):
    if c.steeringQueue == nil:
        return  // no-op，与 Python 一致
    select:
    case c.steeringQueue <- msg:
        // 成功写入
    default:
        // 队列满，warn 日志丢弃（比 Python QueueFull 崩溃更友好）
        logger.Warn(...).Str("msg", msg).Msg("steering queue full, message dropped")
```

### 7.3 DrainSteering 伪代码

```
func (c *AgentCallbackContext) DrainSteering() []string:
    if c.steeringQueue == nil:
        return []
    var msgs []string
    for:
        select:
        case msg := <-c.steeringQueue:
            msgs = append(msgs, msg)
        default:
            return msgs  // 队列空，返回所有消息
```

## 8. FireLifecycle 实现细节

```go
// FireLifecycle 触发 before/after 事件对的生命周期包装。
//
// 对齐 Python: AgentCallbackContext.lifecycle() async context manager
// 差异：Python 用 async with，Go 用函数 + defer
//
// 流程：
//  1. 保存 inputs
//  2. fire(before)      ← 6.6 回填
//  3. 执行 fn()
//  4. finally: 恢复 inputs → fire(after)  ← 6.6 回填
//
// 异常处理：
//  - fn() 出错时设置 ctx.exception
//  - after 回调出错时：如有原始异常则 log 不掩盖，否则 re-raise
func (c *AgentCallbackContext) FireLifecycle(
    before, after AgentCallbackEvent,
    fn func() error,
) error {
    savedInputs := c.inputs

    // 2. fire(before)
    // ⤵️ 6.6 回填：c.Fire(before)
    _ = before // 占位，避免编译错误

    var origErr error
    err := fn()
    if err != nil {
        origErr = err
        c.exception = err
    }

    // finally: 恢复 inputs + fire(after)
    c.inputs = savedInputs
    // ⤵️ 6.6 回填：c.Fire(after)，异常安全处理
    _ = after // 占位

    if origErr != nil {
        return origErr
    }
    return nil
}
```

## 9. 文件结构

```
single_agent/rail/
├── doc.go              # 包文档（已有，需更新文件目录）
├── event.go            # AgentCallbackEvent 枚举（已有，6.4）
├── context.go          # AgentCallbackContext 结构体 + 方法（新增）
└── inputs.go           # EventInputs 接口 + 4 个 Inputs struct（新增）
```

## 10. 回填路径

| 回填步骤 | 回填内容 |
|----------|----------|
| **6.6** | `Fire()` 方法体（调用 `AgentCallbackManager.Execute()`）；`FireLifecycle` 中的 `Fire(before)`/`Fire(after)` 调用；`WarpBaseAgent.callbackManager` 类型从 `any` 改为 `*AgentCallbackManager`；`interfaces.BaseAgent` 中 `CallbackManager()` 返回类型回填 |
| **6.8** | `@rail` 等价包装函数（RailedExecute），在方法级包装 retry 循环 + force_finish 检查 + before/after/on_exception 事件触发 |
| **6.9** | 4 个 Inputs struct 的字段补充完善（InvokeInputs: query/conversationId/result/runKind/runContext；ModelCallInputs: messages/tools/modelContext/response；ToolCallInputs: toolCall/toolName/toolArgs/toolResult/toolMsg；TaskIterationInputs: iteration/loopEvent/conversationId/result/query/isFollowUp） |
| **6.10** | `RequestRetry`/`ConsumeRetryRequest`/`RequestForceFinish`/`ConsumeForceFinish`/`HasForceFinishRequest` 方法体实现；`retryRequest` 字段类型从 `any` 改为 `*RetryRequest`；`forceFinishRequest` 字段类型从 `any` 改为 `*ForceFinishRequest`；定义 `RetryRequest`/`ForceFinishRequest` 类型 |
| **ToolCallContext 回填** | 已有 `ability/ability_types.go` 中的 `ToolCallContext` 增加 `callbackCtx *AgentCallbackContext` 字段，以及 force_finish/steering_queue/skip_tool 预留字段 |

## 11. 测试策略

### 11.1 6.5 完整实现部分

| 测试 | 覆盖内容 |
|------|----------|
| `TestNewAgentCallbackContext` | 构造函数，字段初始化 |
| `TestPushSteering_无队列` | 无队列时 no-op |
| `TestPushSteering_正常写入` | 正常写入并 DrainSteering 验证 |
| `TestPushSteering_队列满丢弃` | 写满 4096 后再写，验证不阻塞、warn 日志 |
| `TestDrainSteering_无队列` | 无队列返回空 |
| `TestDrainSteering_空队列` | 空队列返回空 |
| `TestDrainSteering_多条消息` | push N 条后 drain 返回全部 |
| `TestHasPendingSteering` | 各种状态下检查 |
| `TestBindSteeringQueue` | 绑定后 push/drain 可用 |
| `TestSteeringQueue` | 返回绑定的队列 |
| `TestFireLifecycle_正常流程` | before → fn → after，inputs 恢复 |
| `TestFireLifecycle_异常时设置Exception` | fn 出错时 exception 被设置 |
| `TestFireLifecycle_恢复Inputs` | fn 内修改 inputs，after 时恢复 |
| `TestGetterSetter` | Event/Inputs/Exception/RetryAttempt 等 getter/setter |

### 11.2 预留方法部分

| 测试 | 覆盖内容 |
|------|----------|
| `TestFire_预留Panic` | 验证 panic 信息包含 "6.6" |
| `TestRequestRetry_预留Panic` | 验证 panic 信息包含 "6.10" |
| `TestRequestForceFinish_预留Panic` | 验证 panic 信息包含 "6.10" |
| `TestConsumeRetryRequest_预留Panic` | 验证 panic 信息包含 "6.10" |
| `TestConsumeForceFinish_预留Panic` | 验证 panic 信息包含 "6.10" |
| `TestHasForceFinishRequest_预留Panic` | 验证 panic 信息包含 "6.10" |

### 11.3 EventInputs 接口部分

| 测试 | 覆盖内容 |
|------|----------|
| `TestEventInputs_EventKind` | 各 Inputs struct 的 EventKind() 返回值 |
| `TestEventInputs_接口满足` | 编译期接口满足检查 |
