# 10.3.3 AgentAdapter 接口与工厂 + 10.3.15 SessionManager 设计

> 本文档记录组合B（10.3.1-6 + 10.3.12 + 10.3.15）中前两个组件的详细设计决策。
> 基于 brainstorming 会话中逐项讨论确认的结果。

---

## 1. 10.3.3 AgentAdapter 接口与工厂

### 1.1 在 Agent 会话流程中的位置与作用

**流程位置**：AgentAdapter 是 AgentServer 内部**适配层**的核心接口，位于：

```
用户入口 → Channel → Gateway → [E2A] → AgentWebSocketServer → AgentManager → JiuWenClaw → ⭐ AgentAdapter → Agent → LLM
```

**作用**：
- 定义 Agent SDK 后端的最小能力集接口
- JiuWenClaw 门面仅依赖此接口驱动任意 SDK 后端（Deep/Code/Agent/Fake），不耦合内部结构
- 工厂函数 `createAdapter(sdk, mode)` 按 SDK + Mode 二维路由创建适配器实例

### 1.2 对应 Python 源码

- **接口 + 工厂**：`jiuwenswarm/server/runtime/agent_adapter/agent_adapters.py`（146行）
- Python 中 `AgentAdapter` 是 `@runtime_checkable` Protocol（7方法）
- Python 中 `cleanup()` 不在 Protocol 里，但 JiuWenClaw 门面通过 `self._adapter.cleanup()` 调用

### 1.3 Go 文件结构

```
internal/swarm/server/adapter/
├── doc.go           # 包文档
├── interface.go     # AgentAdapter 接口定义
└── factory.go       # createAdapter() 工厂 + resolveSDKChoice()
```

### 1.4 AgentAdapter 接口设计

**决策：8 个方法**（Python Protocol 7个 + Go 新增 Cleanup）

```go
// AgentAdapter Agent 适配器接口（swarm 侧定义）。
//
// 最小能力集，JiuWenClaw 门面仅依赖此接口驱动任意 SDK 后端，
// 不耦合其内部结构。
//
// 对应 Python: jiuwenswarm/server/runtime/agent_adapter/agent_adapters.py (AgentAdapter)
type AgentAdapter interface {
    // CreateInstance 初始化底层 SDK Agent。
    // 启动时调用一次，skill install/uninstall 后再次调用。
    CreateInstance(ctx context.Context, config map[string]any, mode string, subMode string) error

    // ReloadAgentConfig 热重载配置，不重启进程。
    // configBase: 完整配置快照，若提供则不再读 config.yaml。
    // envOverrides: 环境变量覆盖，仅覆盖请求中存在的键。
    ReloadAgentConfig(ctx context.Context, configBase map[string]any, envOverrides map[string]any) error

    // ProcessMessageImpl 执行非流式请求，返回完整响应。
    // inputs: 预构建的输入字典，含 conversation_id/query/channel 等。
    ProcessMessageImpl(ctx context.Context, req *schema.AgentRequest, inputs map[string]any) (*schema.AgentResponse, error)

    // ProcessMessageStreamImpl 执行流式请求，通过 channel 返回响应块。
    // 返回的 channel 由适配器关闭（发送终止哨兵后 close）。
    // inputs: 预构建的输入字典，含 conversation_id/query/channel 等。
    ProcessMessageStreamImpl(ctx context.Context, req *schema.AgentRequest, inputs map[string]any) (<-chan *schema.AgentResponseChunk, error)

    // ProcessInterrupt 处理中断请求（pause/resume/cancel/supplement）。
    ProcessInterrupt(ctx context.Context, req *schema.AgentRequest) (*schema.AgentResponse, error)

    // HandleUserAnswer 处理用户回答（evolution 审批或权限审批）。
    HandleUserAnswer(ctx context.Context, req *schema.AgentRequest) (*schema.AgentResponse, error)

    // HandleHeartbeat 处理心跳请求。
    // 返回 nil 表示非心跳请求，继续正常流程；
    // 返回非 nil 表示心跳已处理，上层应短路。
    HandleHeartbeat(ctx context.Context, req *schema.AgentRequest) (*schema.AgentResponse, error)

    // Cleanup 清理适配器资源。
    // Python 中不在 Protocol 里但门面会调用，Go 纳入接口更规范，避免运行时类型断言。
    Cleanup() error
}
```

### 1.5 关键设计决策

#### 决策 1：流式返回类型 → channel

| 选项 | 说明 | 结论 |
|------|------|------|
| `<-chan *schema.AgentResponseChunk` | Go 惯用，range 遍历，全项目一致 | ✅ 采用 |
| StreamIterator 接口 | 自定义迭代器（Next/Close），最灵活但多一层抽象 | ❌ |
| callback `onChunk func(...)` | 简单但不够灵活，不支持背压 | ❌ |

**理由**：Python 的 `AsyncIterator[AgentResponseChunk]` 在 Go 中最自然的是 channel。JiuWenClaw 门面的流式桥接也用 channel，全项目语义一致。

#### 决策 2：Cleanup 纳入接口 → 8 方法

| 选项 | 说明 | 结论 |
|------|------|------|
| 8 方法（含 Cleanup） | Go 更规范，编译期保证，避免运行时类型断言 | ✅ 采用 |
| 7 方法（严格对齐 Python Protocol） | Cleanup 不在 Protocol 里，门面用类型断言调用 | ❌ |

**理由**：Python 中 `cleanup()` 不在 Protocol 里但门面通过 `self._adapter.cleanup()` 调用，实际依赖。Go 纳入接口更规范。

#### 决策 3：inputs 参数类型 → map[string]any

| 选项 | 说明 | 结论 |
|------|------|------|
| `map[string]any` | 灵活对齐 Python dict，新增字段不改签名 | ✅ 采用 |
| `AgentInputs` 结构体 | 编译期类型安全，IDE 提示好 | ❌ |

**理由**：Python 的 `inputs` 是纯 dict，字段经常被下游按需读取。结构体字段扩展时需改签名。

#### 决策 4：HandleHeartbeat 返回值语义

- 返回 `nil, nil` → 继续正常流程（对齐 Python 返回 None）
- 返回 `resp, nil` → 短路，不再走正常 Agent 请求流程（对齐 Python 返回 AgentResponse）

#### 决策 5：Python None 语义的 Go 表达

| Python | Go | 说明 |
|--------|-----|------|
| `config: dict \| None = None` | `config map[string]any` | nil map 等价 None |
| `sub_mode: str \| None = None` | `subMode string` | 空字符串等价 None |
| `AgentResponse \| None` | `*schema.AgentResponse` | nil 指针等价 None |

### 1.6 工厂函数设计

```go
// ──────────────────────────── 常量 ────────────────────────────

const (
    // sdkEnvVar SDK 选择环境变量名
    sdkEnvVar = "JIUWENSWARM_AGENT_SDK"
    // defaultSDK 默认 SDK 名称
    defaultSDK = "harness"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ResolveSDKChoice 从环境变量解析 SDK 选择。
//
// 对应 Python: resolve_sdk_choice()
//
// 行为：
//   - 未设置或空 → "harness"（默认）
//   - "harness" → "harness"
//   - "pi" → "pi"（预留，尚未实现）
//   - 未知值 → 警告并回退 "harness"
func ResolveSDKChoice() string

// CreateAdapter 工厂函数，创建 SDK 适配器实例。
//
// 对应 Python: create_adapter(sdk, *, mode)
//
// 参数：
//   - sdk: SDK 名称，若为空则从环境变量解析
//   - mode: 实例模式，"agent"（默认）或 "code"
//
// 路由规则：
//   - sdk="harness" + mode="code" → CodeAdapter
//   - sdk="harness" + 其余 mode → DeepAdapter
//   - sdk="pi" → panic（尚未实现）
//   - 未知 sdk → error
func CreateAdapter(sdk string, mode string) (AgentAdapter, error)
```

### 1.7 方法签名完整对齐表

| # | Go 方法 | Python 方法 | 参数对齐 | 返回值对齐 |
|---|---------|------------|---------|-----------|
| 1 | `CreateInstance(ctx, config, mode, subMode)` | `create_instance(config, *, mode, sub_mode)` | ✅ nil map=None, 空串=None | ✅ error=异常 |
| 2 | `ReloadAgentConfig(ctx, configBase, envOverrides)` | `reload_agent_config(config_base, env_overrides)` | ✅ | ✅ |
| 3 | `ProcessMessageImpl(ctx, req, inputs)` | `process_message_impl(request, inputs)` | ✅ | ✅ |
| 4 | `ProcessMessageStreamImpl(ctx, req, inputs)` | `process_message_stream_impl(request, inputs)` | ✅ | ✅ chan=AsyncIterator |
| 5 | `ProcessInterrupt(ctx, req)` | `process_interrupt(request)` | ✅ | ✅ |
| 6 | `HandleUserAnswer(ctx, req)` | `handle_user_answer(request)` | ✅ | ✅ |
| 7 | `HandleHeartbeat(ctx, req)` | `handle_heartbeat(request)` | ✅ | ✅ nil=None |
| 8 | `Cleanup()` | `cleanup()` (不在Protocol) | — | ✅ |

---

## 2. 10.3.15 SessionManager

### 2.1 在 Agent 会话流程中的位置与作用

**流程位置**：SessionManager 是 JiuWenClaw 门面的内部组件，管理同 session 内任务执行顺序：

```
JiuWenClaw.ProcessMessage → SessionManager.SubmitAndWait → Adapter.ProcessMessageImpl
JiuWenClaw.ProcessMessageStream → SessionManager.SubmitTask → Adapter.ProcessMessageStreamImpl
```

**作用**：
- 管理多 session 并发执行
- **同 session 内任务按先进后出（LIFO）顺序执行**：新任务优先
- 提供任务取消、结果等待等并发控制

### 2.2 对应 Python 源码

- `jiuwenswarm/server/runtime/session/session_manager.py`（187行，9个方法）

### 2.3 Go 文件结构

```
internal/swarm/server/runtime/
├── session_manager.go       # SessionManager 结构体 + 9个方法
└── session_manager_test.go  # 单元测试
```

### 2.4 SessionManager 结构体设计

```go
// SessionManager Session 任务管理器。
//
// 管理多 session 并发执行，同 session 内任务按先进后出顺序执行。
//
// 对应 Python: jiuwenswarm/server/runtime/session/session_manager.py (SessionManager)
type SessionManager struct {
    // mu 保护以下所有 map 的并发访问
    mu sync.Mutex
    // sessionTasks session→当前执行任务的 cancel 函数
    sessionTasks map[string]context.CancelFunc
    // sessionPriorities session→优先级计数器（从 0 递减，LIFO 语义）
    sessionPriorities map[string]int
    // sessionQueues session→优先级堆（container/heap）
    sessionQueues map[string]*priorityHeap
    // sessionProcessors session→消费者 goroutine 的 cancel 函数
    sessionProcessors map[string]context.CancelFunc
    // sessionSignals session→通知消费者有新任务的信号 channel
    sessionSignals map[string]chan struct{}
}
```

### 2.5 辅助类型设计

```go
// priorityItem 优先级队列项。
type priorityItem struct {
    // priority 优先级（数值越小越先出队）
    priority int
    // task 任务函数
    task func(context.Context) (any, error)
}

// priorityHeap 优先级堆，实现 heap.Interface。
//
// 按 priority 升序排列，Pop 取最小值（LIFO：新任务 priority 更小，先出队）。
type priorityHeap []*priorityItem

// 实现 heap.Interface 五个方法：
// Len(), Less(), Swap(), Push(), Pop()
```

### 2.6 方法签名完整对齐表

| # | Go 方法 | Python 方法 | 签名 |
|---|---------|------------|------|
| 1 | `GetSessionID(sessionID string) string` | `get_session_id(session_id: str \| None) -> str` | 空串→"default" |
| 2 | `CancelSessionTask(ctx context.Context, sessionID string, logPrefix string, waitTimeout *time.Duration) error` | `cancel_session_task(session_id, log_msg_prefix, wait_timeout)` | ✅ |
| 3 | `CancelAllSessionTasks(ctx context.Context, logPrefix string) error` | `cancel_all_session_tasks(log_msg_prefix)` | ✅ |
| 4 | `EnsureSessionProcessor(ctx context.Context, sessionID string) error` | `ensure_session_processor(session_id)` | ✅ |
| 5 | `SubmitTask(ctx context.Context, sessionID string, taskFunc func(context.Context) (any, error)) error` | `submit_task(session_id, task_func)` | ✅ |
| 6 | `SubmitAndWait(ctx context.Context, sessionID string, taskFunc func(context.Context) (any, error)) (any, error)` | `submit_and_wait(session_id, taskFunc)` | ✅ |
| 7 | `GetCurrentTask(sessionID string) context.CancelFunc` | `get_current_task(session_id) -> Task \| None` | ✅ 返回cancel |
| 8 | `HasActiveProcessor(sessionID string) bool` | `has_active_processor(session_id)` | ✅ |
| 9 | `HasActiveTasks() bool` | `has_active_tasks()` | ✅ |

### 2.7 关键设计决策

#### 决策 1：LIFO 队列实现方式 → PriorityQueue + goroutine 消费者

| 选项 | 说明 | 结论 |
|------|------|------|
| `container/heap` + goroutine 消费者 | 严格对齐 Python PriorityQueue，支持多任务排队 | ✅ 采用 |
| 互斥锁 + 取消旧任务 | 更简单但丢失排队语义（只支持"新任务替代旧任务"） | ❌ |

**理由**：Python 的 PriorityQueue 支持队列中积压多个任务，LIFO 保证最新的先执行。同 session 内多个请求会排队（如流式请求 + 中断并发），互斥锁方案丢失排队语义。

**LIFO 机制**：`sessionPriorities` 从 0 递减（0, -1, -2, ...），heap.Pop 取最小值 → 后提交的任务 priority 更小 → 先出队执行。

#### 决策 2：taskFunc 类型 → func(ctx) (any, error)

| 选项 | 说明 | 结论 |
|------|------|------|
| `func(ctx context.Context) (any, error)` | Go 惯例，显式传入 ctx，支持取消传播 | ✅ 采用 |
| `func() (any, error)` | 对齐 Python `Callable[[], Awaitable]`，需闭包捕获 ctx | ❌ |

**理由**：Go 惯例显式传入 ctx，任务内部可通过 `ctx.Done()` 感知取消。Python 的取消通过 `asyncio.Task.cancel()` + `CancelledError` 传播，不需要显式传参，但 Go 的 context 机制要求显式传播。

#### 决策 3：GetSessionID 参数类型 → string（空串等价 None）

| 选项 | 说明 | 结论 |
|------|------|------|
| `string`（空串=None） | 简洁，Python `or` 语义对空串也生效 | ✅ 采用 |
| `*string`（nil=None） | 与 AgentRequest.SessionID 类型一致 | ❌ |

**理由**：Python `session_id or "default"` 对 None 和空串都返回 "default"。Go 空串等价最简洁。

#### 决策 4：submit_and_wait 结果桥接 → channel

Python 用 `asyncio.Future`：`set_result` / `set_exception` / `await`。

Go 用 buffered channel：

```go
type taskResult struct {
    value any
    err   error
}
resultCh := make(chan taskResult, 1)
// 任务完成: resultCh <- taskResult{value: result}
// 任务异常: resultCh <- taskResult{err: err}
// 调用侧: select { case r := <-resultCh: return r.value, r.err; case <-ctx.Done(): ... }
```

#### 决策 5：消费者 goroutine 生命周期

- `EnsureSessionProcessor` 启动消费者 goroutine
- 消费者循环：等 signal → lock → heap.Pop → unlock → 执行任务
- 关闭方式：`close(sessionSignals[sessionID])` 或 cancel processor context
- 消费者退出时清理：从所有 map 中移除 session 条目

### 2.8 Python→Go 关键语义映射

| Python | Go |
|--------|-----|
| `asyncio.PriorityQueue` | `container/heap` + signal channel |
| `asyncio.Task` | `goroutine` + `context.CancelFunc` |
| `asyncio.Future` (set_result/set_exception/await) | `chan taskResult` (buffered 1) |
| `asyncio.create_task(fn)` | `go fn(ctx)` |
| `task.cancel()` | `cancelFn()` |
| `task.done()` | `ctx.Err() != nil` 或 select ctx.Done |
| `queue.task_done()` | 无需（Go channel 自带） |
| 哨兵 `None` 关闭处理器 | `close(signalCh)` |
| `await queue.get()` 阻塞等待 | `<-signalCh` 阻塞等待 |
