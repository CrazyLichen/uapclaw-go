# 9.60 StreamController 设计文档

## 概述

StreamController 是 TeamAgent 的流式控制层，管理 Agent 执行轮次的生命周期、流式分块处理、输入投递、中断处理和重试逻辑。

对齐 Python: `StreamController` (`openjiuwen/agent_teams/agent/stream_controller.py`)

### 在 Agent 会话流程中的位置

```
用户输入 → TeamAgent.Invoke/Stream()
              │
              ├─ 创建 streamQueue（chan streambase.Schema，nil sentinel 关闭流）
              ├─ 缓存 pendingUserQuery
              ├─ 启动 CoordinationKernel.start(session)         ← 9.62
              │     └─ 事件循环 → AgentLifecycleHandler
              │           └─ TeamAgent.deliver_input()
              │                 └─ StreamController.startRound(content)  ★ 9.60 核心入口
              │                       └─ goroutine: runOneRound(ctx, content)
              │                             ├─ executeRound(ctx, message)     ← 执行状态机
              │                             │     └─ runRetryingStream(ctx, message)
              │                             │           └─ streamOneRound(ctx, query)
              │                             │                 ├─ harness.RunStreaming()  → DeepAgent
              │                             │                 ├─ tagChunk(chunk)         → 标注来源成员
              │                             │                 ├─ streamQueue <- tagged    → 写入队列
              │                             │                 └─ fanOutToObservers(ctx, tagged) → SpawnManager 转发
              │                             │
              │                             └─ finally: 检查 pendingInputs/interrupt/teamCleaned
              │                                   └─ 自动重启动新一轮 或 CloseStream()
              │
              └─ 从 streamQueue 读取直到 nil sentinel → 返回结果
```

### 核心作用

| 作用域 | 说明 |
|--------|------|
| 轮次生命周期 | `startRound` → `executeRound`（状态机 STARTING→RUNNING→COMPLETING→COMPLETED→IDLE）→ 自动续轮 |
| 流式分块处理 | 从 DeepAgent 的 `harness.RunStreaming()` 读取 chunk，标注来源成员（`tagChunk`），写入 `streamQueue`，扇出到观察者 |
| 输入投递 | `steer`/`followUp` 运行中追加，`pendingInputs` 排队等待下一轮 |
| 中断处理 | `hasPendingInterrupt`/`isValidInterruptResume`/`dequeueValidInterruptResume` |
| 协作取消 | 两阶段关闭：先设 `cancelRequested` + `harness.Abort()` 等待 2s，超时则 `cancelRound()`（强制取消 goroutine） |
| 重试逻辑 | 对错误码 181001 自动重试最多 10 次 |
| 跨成员转发 | `ChunkObserver` 机制：SpawnManager 将 Teammate 的 chunk 转发到 Leader 的 streamQueue |
| 团队完成信号 | `EmitCompletionAndClose` 发 `team.completed` 标记再关闭流 |

## 设计决策

### 1. 异步模型映射：goroutine + channel

对齐 Python 不复用 session/stream 基础设施（Python 的 StreamController 也不用 `AsyncStreamQueue`/`StreamEmitter`，只用 `asyncio.Queue`）。

| Python | Go | 说明 |
|--------|-----|------|
| `asyncio.Task` | `goroutine` + `context.CancelFunc` + `roundDone chan struct{}` | goroutine 无需保存引用，`cancelRound` 控制取消，`roundDone` 检测完成 |
| `asyncio.Queue` | `chan streambase.Schema` | nil sentinel 关闭流，对齐 Python 的 `None` sentinel |
| `self._cancel_requested` | `cancelRequested bool` | 相同语义，协作取消标记 |
| `self._chunk_observers` | `[]ChunkObserver` | 相同语义，扇出到外部消费者 |
| `self._resources.harness` | `resources.Harness()` | 对齐 Python：通过 PrivateAgentResources 访问 TeamHarness |

### 2. 依赖方式：通过 resources.Harness() 直接访问

对齐 Python，StreamController 持有 `*PrivateAgentResources`，通过 `resources.Harness()` 获取 `*TeamHarness`。不加额外的 StreamingHarness 接口层。

### 3. streamQueue 元素统一用指针

`chan streambase.Schema` 中所有元素统一用指针类型（`*OutputSchema`、`*TeamOutputSchema`、`*TraceSchema`、`*CustomSchema`）。nil sentinel 关闭流。对齐 Python 引用语义。

### 4. RunStreaming 分支 1 可工作、分支 2 占位

`TeamHarness.RunStreaming` 返回类型从 `(any, error)` 改为 `(<-chan streambase.Schema, error)`：
- 分支 1（`teamSession == nil && !initialPlanMode`）：实现，调已有 `runner.RunAgentStreaming`
- 分支 2（有 team_session）：`⤵️ 待 9.57+ session 层回填`，返回空 channel + nil

## 文件结构

### 新增文件

| 文件 | 说明 |
|------|------|
| `internal/agent_teams/agent/stream_controller.go` | StreamController 核心实现 |
| `internal/agent_teams/agent/stream_controller_test.go` | 单元测试 |

### 需同步修改的文件

| 文件 | 修改内容 |
|------|----------|
| `internal/agent_teams/harness.go` | `RunStreaming` 返回类型改为 `(<-chan streambase.Schema, error)`，实现分支 1 |
| `internal/agent_teams/schema/stream.go` | `NewTeamOutputSchema` 返回 `*TeamOutputSchema`（指针） |
| `internal/agent_teams/agent/team_agent.go` | `streamController any` → `*StreamController`，回填 14 个 `TODO(#9.60)` 方法 |
| `internal/agent_teams/agent/spawn_manager.go` | 回填 `wireInprocessChunkForward`（line 352） |
| `internal/agent_teams/spawn/inprocess_handle.go` | `chunkForward any` → `ChunkObserver` 类型（line 31, 207, 213） |
| `internal/agent_teams/agent/doc.go` | 更新文件目录 |
| `internal/agent_teams/harness.go` | `IsPendingInterruptResumeValid` / `RequestCompletionPoll` / `WakeMailboxIfInterruptCleared` 三个 `⤵️` 标记更新 |

## 类型定义

### ChunkObserver

```go
// ChunkObserver 分块观察者回调。
// 对齐 Python: ChunkObserver = Callable[[OutputSchema], Awaitable[None]]
// 每个分块标注来源成员后触发，用于 SpawnManager 将 Teammate chunk 转发到 Leader 的 streamQueue。
type ChunkObserver func(ctx context.Context, chunk streambase.Schema) error
```

### StreamController 结构体

```go
type StreamController struct {
    // getBlueprint 蓝图获取器（延迟获取，configure 后才有值）
    getBlueprint func() *TeamAgentBlueprint
    // state 共享可变状态
    state *TeamAgentState
    // resources 每实例运行时资源（持有 harness）
    resources *PrivateAgentResources
    // updateStatus 成员状态更新回调
    updateStatus func(ctx context.Context, status atschema.MemberStatus) error
    // updateExecution 执行状态更新回调
    updateExecution func(ctx context.Context, status atschema.ExecutionStatus) error
    // wakeMailboxCb 中断清除后唤醒邮箱回调
    // ⤵️ 待 9.62 CoordinationKernel 章节回填：实际回调从 TeamAgent 传入
    wakeMailboxCb func(ctx context.Context) error
    // requestCompletionPollCb 轮次干净结束时请求完成轮询的回调（仅 Leader 传入）
    // ⤵️ 待 9.62 CoordinationKernel 章节回填：实际回调从 TeamAgent 传入
    requestCompletionPollCb func(ctx context.Context) error

    // streamQueue 流式分块队列（nil sentinel 关闭流）
    streamQueue chan streambase.Schema
    // cancelRound 取消当前轮次的 context cancel 函数
    cancelRound context.CancelFunc
    // roundDone 当前轮次是否完成的信号（goroutine 关闭表示完成）
    roundDone chan struct{}
    // streamingActive 是否正在流式输出
    streamingActive bool
    // cancelRequested 当前轮次是否被协作取消
    cancelRequested bool
    // pendingInterruptResumes 待处理的中断恢复输入
    // ⤵️ 待 Interaction 层实现后回填类型：当前用 any 占位
    pendingInterruptResumes []any
    // pendingInputs 待处理的输入队列（轮次结束后自动消费）
    pendingInputs []any
    // chunkObservers 分块观察者列表
    chunkObservers []ChunkObserver
}
```

### 构造函数

```go
// NewStreamController 创建新的流式控制器。
// 对齐 Python: StreamController.__init__(blueprint_getter, state, resources, status_updater,
//     execution_updater, wake_mailbox_callback, request_completion_poll_callback)
func NewStreamController(
    getBlueprint func() *TeamAgentBlueprint,
    state *TeamAgentState,
    resources *PrivateAgentResources,
    updateStatus func(ctx context.Context, status atschema.MemberStatus) error,
    updateExecution func(ctx context.Context, status atschema.ExecutionStatus) error,
    opts ...StreamControllerOption,
) *StreamController

// StreamControllerOption 流式控制器可选配置
type StreamControllerOption func(*StreamController)

// WithWakeMailbox 设置中断清除后的邮箱唤醒回调
func WithWakeMailbox(cb func(ctx context.Context) error) StreamControllerOption

// WithRequestCompletionPoll 设置轮次干净结束后的完成轮询回调
func WithRequestCompletionPoll(cb func(ctx context.Context) error) StreamControllerOption
```

## 方法对齐表

| Go 方法 | Python 方法 | 可见性 | 实现说明 |
|---------|------------|--------|----------|
| `NewStreamController(...)` | `__init__` | 导出 | ✅ 全部实现 |
| `memberName()` | `_member_name()` | 非导出 | ✅ |
| `AddChunkObserver(cb)` | `add_chunk_observer(cb)` | 导出 | ✅ |
| `RemoveChunkObserver(cb)` | `remove_chunk_observer(cb)` | 导出 | ✅ 幂等 |
| `tagChunk(chunk)` | `_tag_chunk(chunk)` | 非导出 | ✅ 指针类型断言，4 种情况 |
| `IsAgentRunning()` | `is_agent_running()` | 导出 | ✅ 返回 streamingActive |
| `HasInFlightRound()` | `has_in_flight_round()` | 导出 | ✅ select 非阻塞检查 roundDone |
| `HasPendingInterrupt()` | `has_pending_interrupt()` | 导出 | ✅ 委托 harness |
| `StartRound(ctx, content)` | `start_round(content)` | 导出 | ✅ goroutine 启动 |
| `Steer(ctx, content)` | `steer(content)` | 导出 | ✅ 委托 harness |
| `FollowUp(ctx, content)` | `follow_up(content)` | 导出 | ✅ 委托 harness |
| `CancelAgent(ctx)` | `cancel_agent()` | 导出 | ✅ 状态机 + CooperativeCancel |
| `CloseStream()` | `close_stream()` | 导出 | ✅ nil sentinel |
| `EmitCompletionAndClose(memberCount, taskCount)` | `emit_completion_and_close(...)` | 导出 | ✅ |
| `DrainAgentTask(ctx)` | `drain_agent_task()` | 导出 | ✅ 清队列 + CancelAgent |
| `CooperativeCancel(ctx)` | `cooperative_cancel()` | 导出 | ✅ 两阶段 |
| `IsValidInterruptResume(userInput)` | `is_valid_interrupt_resume(user_input)` | 导出 | ✅ 委托 harness |
| `startRound(ctx, content)` | `start_round(content)` 内部启动逻辑 | 非导出 | ✅ |
| `runOneRound(ctx, message)` | `_run_one_round(message)` | 非导出 | ✅ finally 中自动续轮 |
| `executeRound(ctx, message)` | `_execute_round(message)` | 非导出 | ✅ 状态机 |
| `streamOneRound(ctx, query)` | `_stream_one_round(query)` | 非导出 | ✅ 分支 1 可工作，分支 2 占位 |
| `runRetryingStream(ctx, query)` | `_run_retrying_stream(query)` | 非导出 | ✅ 最多重试 10 次 |
| `dequeueValidInterruptResume()` | `_dequeue_valid_interrupt_resume()` | 非导出 | ✅ |
| `wakeMailboxIfInterruptCleared(ctx)` | `_wake_mailbox_if_interrupt_cleared()` | 非导出 | ✅ |
| `combinePendingInputs(items)` | Python 内联逻辑 | 非导出 | ✅ 分隔符合并 |
| `fanOutToObservers(ctx, tagged)` | Python 内联循环 | 非导出 | ✅ 异常自动移除 |
| `logRoundPanic()` | `_log_agent_task_exception(task)` | 非导出 | ✅ |

## 核心流程对齐

### startRound → goroutine 启动

```
Python:                           Go:
start_round(content)              startRound(ctx, content)
  ├─ check harness/queue None       ├─ check harness/streamQueue nil
  ├─ log                            ├─ log
  ├─ agent_task = create_task(      ├─ roundCtx, cancel := context.WithCancel(ctx)
  │     _run_one_round(content))    │   cancelRound = cancel
  └─ add_done_callback(             │   roundDone = make(chan struct{})
      _log_agent_task_exception)    └─ go func() {
                                       defer close(roundDone)
                                       defer logRoundPanic()
                                       runOneRound(roundCtx, content)
                                     }()
```

### runOneRound — 轮次主循环

```
Python:                                    Go:
_run_one_round(message)                   runOneRound(ctx, message)
  ├─ set_member_id(member_name)             ├─ ⤵️ 待 9.62 回填：set_member_id
  ├─ _cancel_requested = False              ├─ cancelRequested = false
  ├─ harness.init_cwd_for_round()           ├─ ⤵️ 待 Harness 层回填：InitCwdForRound()
  ├─ update_status(READY)                   ├─ updateStatus(READY)
  ├─ update_status(BUSY)                    ├─ updateStatus(BUSY)
  ├─ try: _execute_round(message)           ├─ executeRound(ctx, message)
  ├─ except CancelledError:                 │   (ctx cancelled 检测在 executeRound 内)
  │     cancelled=True, raise              │
  ├─ except BaseException:                  │
  │     update_status(ERROR)                │
  ├─ finally:                               └─ defer (finally 逻辑):
  │     ├─ agent_task = None                    ├─ cancelRound = nil
  │     ├─ if team_cleaned:                     ├─ if teamCleaned: CloseStream()
  │     │     close_stream()                    ├─ elif !cancelled && !cancelRequested:
  │     ├─ elif not cancelled                   │     ├─ dequeueValidInterruptResume() → startRound
  │     │     and not _cancel_requested:        │     ├─ elif pendingInputs → startRound
  │     │     ├─ interrupt resume → start       │     └─ else: wakeMailbox + ⤵️ SHUTDOWN/COMPLETION
  │     │     ├─ pending inputs → start
  │     │     └─ wake_mailbox / poll
  │     └─ (CANCELLED path skips restart)
```

### executeRound — 执行状态机

```
Python:                                    Go:
_execute_round(message)                   executeRound(ctx, message)
  ├─ update_execution(STARTING)             ├─ updateExecution(STARTING)
  ├─ update_execution(RUNNING)              ├─ updateExecution(RUNNING)
  ├─ try:                                   ├─ err := runRetryingStream(ctx, message)
  │     _run_retrying_stream(message)       ├─ if err != nil:
  │     ├─ if _cancel_requested:            │     ├─ if ctx.Err() != nil || cancelRequested:
  │     │     update_execution(CANCELLED)   │     │     updateExecution(CANCELLED)
  │     └─ else:                            │     └─ else:
  │           COMPLETING → COMPLETED        │           updateExecution(FAILED)
  ├─ except CancelledError:                 ├─ else:
  │     CANCELLED, raise                    │     ├─ if cancelRequested: CANCELLED
  ├─ except TimeoutError:                   │     └─ else: COMPLETING → COMPLETED
  │     TIMED_OUT, raise                    └─ updateExecution(IDLE)  // finally
  ├─ except Exception:
  │     FAILED, raise
  └─ finally: IDLE
```

### streamOneRound — 单轮流式读取

```
Python:                                    Go:
_stream_one_round(query)                  streamOneRound(ctx, query)
  ├─ inputs = {"query": query}              ├─ inputMap := map[string]any{"query": query}
  ├─ streaming_active = True                ├─ streamingActive = true (defer reset)
  ├─ try:                                   ├─ chunkCh, err := harness.RunStreaming(ctx, inputMap, ...)
  │     async for chunk in                  ├─ for chunk := range chunkCh:
  │       harness.run_streaming(inputs):    │     ├─ if errorSeen: continue
  │     ├─ if error_seen: continue          │     ├─ detectTaskFailed(chunk) → set errorSeen
  │     ├─ _detect_task_failed(chunk)       │     ├─ tagged := tagChunk(chunk)
  │     ├─ tagged = _tag_chunk(chunk)       │     ├─ streamQueue <- tagged
  │     ├─ stream_queue.put(tagged)         │     └─ fanOutToObservers(ctx, tagged)
  │     └─ fan-out to observers             └─ return (errorCode, errorText)
  └─ finally: streaming_active = False
```

### CooperativeCancel — 协作取消

```
Python:                                    Go:
cooperative_cancel()                      CooperativeCancel(ctx)
  ├─ if task is None or done: return        ├─ if roundDone == nil: return
  ├─ _cancel_requested = True               ├─ cancelRequested = true
  ├─ harness.abort()                        ├─ harness.Abort(ctx)
  ├─ try:                                   ├─ select:
  │     await wait_for(                     │     case <-roundDone:  // 正常完成
  │       shield(task),                     │       return
  │       timeout=2.0)                      │     case <-time.After(2s):
  │   except TimeoutError:                  │       cancelRound()  // 强制取消
  │     task.cancel()                       │
  └─ suppress all exceptions                └─ (suppress all errors)
```

### tagChunk — 分块标注

```
Python:                                    Go:
_tag_chunk(chunk)                         tagChunk(chunk)
  ├─ get member_name, role from bp          ├─ get memberName, role from bp
  ├─ if no member_name or                   ├─ if no memberName or
  │   not isinstance(OutputSchema):         │   chunk is TraceSchema/CustomSchema:
  │     return chunk                        │     return chunk
  ├─ if isinstance(TeamOutputSchema):       ├─ if *TeamOutputSchema (type assert):
  │     ├─ if match: return chunk           │     ├─ if match: return chunk
  │     └─ else: model_copy(update=...)     │     └─ else: shallow copy + update labels
  └─ TeamOutputSchema.from_output(...)      └─ NewTeamOutputSchema(outChunk, ...)
```

## 常量对齐

| Go 常量 | Python 常量 | 值 |
|---------|------------|-----|
| `maxRetryAttempts` | `_MAX_RETRY_ATTEMPTS` | 10 |
| `cooperativeAbortTimeoutSeconds` | `_COOPERATIVE_ABORT_TIMEOUT_SECONDS` | 2.0 |
| `retryQuery` | `_RETRY_QUERY` | "刚才有异常状况，继续执行" |
| `taskFailedPayloadType` | `_TASK_FAILED_PAYLOAD_TYPE` | "task_failed" |
| `retryableErrorCodes` | `_RETRYABLE_ERROR_CODES` | {181001} |

## 回填标记汇总

| 位置 | 标记 | 等待章节 |
|------|------|----------|
| `runOneRound` 中 set_member_id 上下文变量 | ⤵️ | 9.62 CoordinationKernel |
| `runOneRound` 中 requestCompletionPollCb 调用 | ⤵️ | 9.62 CoordinationKernel |
| `runOneRound` 中 SHUTDOWN_REQUESTED 检查 | ⤵️ | TeamMember 状态检查 |
| `streamOneRound` 中 RunStreaming 分支 2 | ⤵️ | 9.57+ session 层 |
| `pendingInterruptResumes` 元素类型 `[]any` | ⤵️ | Interaction 层 |
| `wakeMailboxCb` 实际回调 | ⤵️ | 9.62 CoordinationKernel |
| `requestCompletionPollCb` 实际回调 | ⤵️ | 9.62 CoordinationKernel |

## team_agent.go 回填

`streamController any` → `*StreamController`，以下 14 个方法从 TODO 变为实际委托：

1. `IsAgentRunning()` → `streamController.IsAgentRunning()`
2. `HasInFlightRound()` → `streamController.HasInFlightRound()`
3. `HasPendingInterrupt()` → `streamController.HasPendingInterrupt()`
4. `Invoke()` → 创建 streamQueue + 从 streamQueue 读取
5. `Stream()` → 同 Invoke 但持续 yield
6. `DeliverInput()` → 运行中 steer/follow-up；飞行中入队；否则 startRound
7. `StartAgent()` → startRound
8. `FollowUp()` → streamController.FollowUp()
9. `CancelAgent()` → CancelAgent
10. `Steer()` → streamController.Steer()
11. `ResumeInterrupt()` → 验证中断 → 飞行中入队 → 否则 startRound
12. `ShutdownSelf()` → CooperativeCancel + teamMember.updateStatus(SHUTDOWN)
13. `ConcludeCompletedRound()` → EmitCompletionAndClose
14. `IsShutdownRequested()` → teamMember.status() 检查

## spawn_manager.go 回填

`wireInprocessChunkForward(handle)` 实现：
- teammate StreamController.AddChunkObserver(forwardCb)
- forwardCb: teammate chunk → leader streamQueue.put(chunk)
- cleanup: teammate StreamController.RemoveChunkObserver(forwardCb)

## inprocess_handle.go 回填

- `chunkForward any` → `ChunkObserver` 类型
- `ChunkForward()` getter 返回 `ChunkObserver`
- `SetChunkForward(v ChunkObserver)` setter

## TeamHarness.RunStreaming 修改

返回类型从 `(any, error)` 改为 `(<-chan streambase.Schema, error)`。

分支 1 实现：
```go
if teamSession == nil && !h.initialPlanMode {
    return runner.RunAgentStreaming(ctx, runner.AgentRef{Agent: h.deepAgent},
        inputs, runner.SessionRef{ID: sessionID}, nil, nil, nil)
}
```

分支 2 占位：
```go
// ⤵️ 待 9.57+ session 层回填：实现 _prepare_agent_session + _ensure_initial_plan_mode
ch := make(chan streambase.Schema)
close(ch)
return ch, nil
```

## TeamOutputSchema 修改

`NewTeamOutputSchema` 返回类型从 `TeamOutputSchema` 改为 `*TeamOutputSchema`：

```go
func NewTeamOutputSchema(base stream.OutputSchema, sourceMember *string, role *TeamRole) *TeamOutputSchema {
    return &TeamOutputSchema{
        OutputSchema: base,
        SourceMember: sourceMember,
        Role:         role,
    }
}
```

## 测试策略

### 可 mock 测试的部分

- 构造函数 + Option 模式
- AddChunkObserver / RemoveChunkObserver
- tagChunk 四种情况（用 fake Schema 实现）
- HasInFlightRound / IsAgentRunning 状态查询
- CloseStream / EmitCompletionAndClose
- combinePendingInputs 分隔符合并
- dequeueValidInterruptResume
- fanOutToObservers 异常自动移除
- detectTaskFailed / isRetryableErrorCode
- runRetryingStream 重试逻辑（用 fake harness）

### 需要 build tag 隔离的部分

- 真实 harness.RunStreaming 端到端流式 → `//go:build integration`

### 测试文件

`internal/agent_teams/agent/stream_controller_test.go`

覆盖率目标：≥ 85%（排除 `//go:build integration` 隔离的测试）
