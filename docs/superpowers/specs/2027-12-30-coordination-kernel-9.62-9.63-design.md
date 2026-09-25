# 9.62+9.63 CoordinationKernel + EventBus/Dispatcher 合并实现设计

> 状态：Draft
> 日期：2027-12-30
> 章节：9.62 CoordinationKernel + 9.63 EventBus / Dispatcher（合并实现）
> Python 源码：`openjiuwen/agent_teams/agent/coordination/`

## 1. 概述

实现 TeamAgent 的事件驱动唤醒层（coordination 子系统），包括：

- **EventBus**：事件入队 + 周期轮询定时器 + 生命周期管理（start/stop/pause_polls/resume_polls）
- **EventDispatcher**：粗筛规则 + 私有 CallbackFramework 实例 + 6 个场景 Handler 注册与 fan-out
- **CoordinationKernel**：协调子系统 facade，持有 EventBus + EventDispatcher，对外提供 Setup/Start/Pause/Stop 生命周期
- **6 个 Handler 骨架**：EVENT_METHOD_MAP + 方法签名，方法体 TODO(#9.55) 占位

### 1.1 在 Agent 会话中的流程位置与作用

```
用户请求 → TeamAgent.invoke()/stream()
              ↓
         CoordinationKernel.start(session)       ← 9.62
              ↓
         EventBus.start(wake_callback=dispatcher.dispatch)  ← 9.63
              ↓
         ┌─────────────────────────────────────────────────┐
         │  EventBus 事件循环（两条唤醒路径）：              │
         │  1. 事件驱动：Messager transport 事件 → enqueue  │
         │  2. 轮询回退：mailbox_poll / task_poll 定时器    │
         └─────────────────────────────────────────────────┘
              ↓
         EventDispatcher.dispatch(event)          ← 9.63
           ├─ 粗筛：agent_ready? inner vs transport? 角色门控?
           └─ CallbackFramework.trigger(event_key, event)
              ↓
         6 个场景 Handler（按注册顺序 fan-out）
           ├─ AgentLifecycleHandler  → deliver_input / cancel_agent
           ├─ MemberHandler          → deliver_input / shutdown_self
           ├─ MessageHandler         → deliver_input / drain
           ├─ TaskBoardHandler       → deliver_input
           ├─ StaleTaskHandler       → deliver_input
           └─ TeamCompletionHandler  → conclude_completed_round
              ↓
         TeamHarness（DeepAgent 执行 LLM 调用 + team tools）
```

**核心作用**：EventBus / Dispatcher 是 TeamAgent 的"心跳系统"——不做任何业务决策（铁律 1），只负责唤醒（事件到达时叫醒 DeepAgent）和分发（按粗筛规则决定事件该不该处理，然后 fan-out 到具体 handler）。

### 1.2 三条铁律

1. **coordination 不做决策**——loop 只管 wake-up，所有业务行为由 DeepAgent + team tools 驱动
2. **每个 handler 一个业务域**——跨域协作走 CallbackFramework fan-out，不在一个 handler 内调另一个 handler
3. **异常语义**——CallbackFramework.TriggerCustom 吞普通异常（log + continue），handler 自己用 `logger.Error` 记录关键失败

## 2. 关键设计决策

| 决策点 | 选择 | 理由 |
|--------|------|------|
| 9.62 与 9.63 | 合并实现 | Python 中三者紧密耦合——kernel 持有 event_bus + dispatcher，分开无法端到端测试 |
| 回调框架 | NewCallbackFramework() 私有实例 + 适配层 | Python 复用 AsyncCallbackFramework 创建私有实例；Go 端 agent_teams 已大量依赖 agentcore，无反向依赖问题 |
| 并发模型 | channel + 单 goroutine 消费 | 对齐 Python asyncio.Queue 的 FIFO + 单消费者语义，保持"串行 fan-out" |
| CoordinationEvent 类型 | 包装结构体（Inner + Transport 恰好一个非 nil） | 用户选择，不修改已有 EventMessage |
| Handler 实现范围 | 框架先行 + 6 个 Handler 骨架 | 业务逻辑依赖 9.55 TeamAgent 的 Protocol 方法，此节只建骨架 |
| 前置依赖 | 仅 9.62+9.63，其余 stub/TODO | 不做 9.55/9.61 等章节，所有外部依赖用 TODO(#9.55) 占位 |

## 3. CoordinationEvent 包装结构体与内部事件类型

```go
// InnerEventType 协调层内部事件类型枚举。Python: InnerEventType
type InnerEventType string

const (
    InnerEventTypeUserInput    InnerEventType = "user_input"
    InnerEventTypePollMailbox  InnerEventType = "coordination_poll_mailbox"
    InnerEventTypePollTask     InnerEventType = "coordination_poll_task"
    InnerEventTypeShutdown     InnerEventType = "shutdown"
)

// InnerEventMessage 协调层内部事件消息。Python: InnerEventMessage
type InnerEventMessage struct {
    EventType InnerEventType
    Payload   map[string]any
}

// CoordinationEvent 事件总线处理的统一事件包装。
// Python: CoordinationEvent = Union[InnerEventMessage, EventMessage]
// Go 用包装结构体：Inner 和 Transport 恰好一个非 nil。
type CoordinationEvent struct {
    Inner     *InnerEventMessage     // 非 nil 时 Transport 为 nil
    Transport *events.EventMessage   // 非 nil 时 Inner 为 nil
}

func (e CoordinationEvent) IsInner() bool     { return e.Inner != nil }
func (e CoordinationEvent) IsTransport() bool { return e.Transport != nil }
func (e CoordinationEvent) EventType() string {
    if e.Inner != nil { return string(e.Inner.EventType) }
    return e.Transport.EventType
}
```

## 4. EventBus

```go
// WakeCallback 事件唤醒回调。Python: WakeCallback
type WakeCallback func(ctx context.Context, event CoordinationEvent)

// EventBus 事件驱动的唤醒循环。Python: EventBus
type EventBus struct {
    role                schema.TeamRole
    mailboxPollInterval float64
    taskPollInterval    float64
    wakeCallback        WakeCallback
    running             bool
    pollsPaused         bool
    periodicPollEnabled bool   // HUMAN_AGENT 为 false

    eventCh       chan CoordinationEvent       // 容量 256
    cancelFunc    context.CancelFunc           // runLoop 退出
    mailboxPollCancel context.CancelFunc
    taskPollCancel    context.CancelFunc
}
```

### 4.1 Python asyncio → Go 映射

| Python | Go |
|--------|-----|
| `asyncio.Queue[CoordinationEvent]` | `chan CoordinationEvent` 容量 256 |
| `asyncio.create_task(self._run_loop())` | `go b.runLoop(loopCtx)` |
| `asyncio.sleep(interval)` | `time.Ticker` + `context.Cancel` |
| `task.cancel()` | `cancelFunc()` 取消 context |
| `await asyncio.wait_for(task, timeout=5)` | Go 直接 `cancelFunc()` + 发 SHUTDOWN——channel 关闭和 context cancel 是更可靠的退出机制 |
| `InnerEventMessage(event_type=SHUTDOWN)` | 向 channel 发 shutdown 事件，runLoop 收到后退出 |
| `_periodic_poll_enabled` 门控 | `periodicPollEnabled bool`，HUMAN_AGENT 为 false |

### 4.2 关键方法

| 方法 | Python 对应 | 说明 |
|------|-------------|------|
| `NewEventBus(role, mailboxPollInterval, taskPollInterval)` | `EventBus.__init__` | |
| `Start(ctx, wakeCallback)` | `EventBus.start` | 绑定 wakeCallback 并启动 runLoop + poll 定时器 |
| `Stop()` | `EventBus.stop` | 发 SHUTDOWN + cancel context，重置 pollsPaused |
| `PausePolls()` | `EventBus.pause_polls` | 取消 poll 定时器 goroutine |
| `ResumePolls()` | `EventBus.resume_polls` | 重新启动 poll 定时器 |
| `Enqueue(event)` | `EventBus.enqueue` | 向 eventCh 发送事件 |

### 4.3 runLoop 核心逻辑

```go
func (b *EventBus) runLoop(ctx context.Context) {
    for {
        select {
        case <-ctx.Done():
            return
        case event := <-b.eventCh:
            if event.Inner != nil && event.Inner.EventType == InnerEventTypeShutdown {
                return
            }
            if b.wakeCallback != nil {
                func() {
                    defer func() {
                        if r := recover(); r != nil {
                            logger.Error(logComponent).Str("event_type", event.EventType()).
                                Any("recover", r).Msg("EventBus: error in wakeCallback")
                        }
                    }()
                    b.wakeCallback(ctx, event)
                }()
            }
        }
    }
}
```

## 5. EventDispatcher + 三个 Protocol 接口 + 适配层

### 5.1 三个 Protocol 接口

```go
// AgentRoundController Round 级控制面。Python: AgentRoundController
type AgentRoundController interface {
    IsAgentReady() bool
    IsAgentRunning() bool
    HasInFlightRound() bool
    HasPendingInterrupt() bool
    CancelAgent(ctx context.Context) error
    DeliverInput(ctx context.Context, content any, useSteer bool) error
    ResumeInterrupt(ctx context.Context, userInput any) error
}

// TeamLifecycleController TeamAgent 级生命周期效果。Python: TeamLifecycleController
type TeamLifecycleController interface {
    ShutdownSelf(ctx context.Context) error
    ConcludeCompletedRound(ctx context.Context, memberCount, taskCount int) error
}

// PollController EventBus 周期轮询控制面。Python: PollController
type PollController interface {
    PausePolls()
    ResumePolls()
}

// DispatcherHost 组合 host 契约。Python: DispatcherHost
type DispatcherHost interface {
    AgentRoundController
    TeamLifecycleController
}
```

### 5.2 CallbackFramework 适配层

```go
const coordEventMapKey = "__coordination_event"

type coordCallbackFunc func(ctx context.Context, event CoordinationEvent)

// wrapCallback 将 coordination handler 回调包装为 CallbackFramework 的 CustomCallbackFunc。
func wrapCallback(fn coordCallbackFunc) callback.CustomCallbackFunc {
    return func(ctx context.Context, data map[string]any) any {
        raw, ok := data[coordEventMapKey]
        if !ok { return nil }
        event, ok := raw.(CoordinationEvent)
        if !ok { return nil }
        fn(ctx, event)
        return nil
    }
}

// packEvent 将 CoordinationEvent 打包进 map[string]any。
func packEvent(event CoordinationEvent) map[string]any {
    return map[string]any{coordEventMapKey: event}
}
```

### 5.3 EventDispatcher

```go
type EventDispatcher struct {
    round     AgentRoundController
    blueprint *blueprint.TeamAgentBlueprint
    infra     *infra.TeamInfra
    framework *callback.CallbackFramework   // 私有实例，与全局单例隔离

    Lifecycle      *handlers.AgentLifecycleHandler
    Member         *handlers.MemberHandler
    Message        *handlers.MessageHandler
    TaskBoard      *handlers.TaskBoardHandler
    StaleTask      *handlers.StaleTaskHandler
    TeamCompletion *handlers.TeamCompletionHandler
}
```

构造时：
1. 创建 `NewCallbackFramework()` 私有实例
2. 创建 `staleClaimThrottle := make(map[string]float64)` 共享引用
3. 构造 6 个 handler，按 `(lifecycle, member, message, task_board, stale_task, team_completion)` 顺序
4. 遍历每个 handler 的 `GetCallbacks()`，调用 `fw.OnCustom(eventKey, wrapCallback(method))`

### 5.4 Dispatch 粗筛规则

```go
func (d *EventDispatcher) Dispatch(ctx context.Context, event CoordinationEvent) {
    // 粗筛 1：agent 未就绪 → 跳过
    if !d.round.IsAgentReady() { return }

    // 内部事件分支
    if event.IsInner() {
        // Human-agent 绝不自发轮询
        if role == TeamRoleHumanAgent &&
            (event.Inner.EventType == InnerEventTypePollTask ||
                event.Inner.EventType == InnerEventTypePollMailbox) { return }
        fw.TriggerCustom(ctx, string(event.Inner.EventType), packEvent(event))
        return
    }

    // Transport 事件：无 member_name → 跳过
    if blueprint.MemberName == "" { return }

    // Human-agent 白名单门控
    if role == HumanAgent && !allowed[event.Transport.EventType] { return }

    fw.TriggerCustom(ctx, event.Transport.EventType, packEvent(event))
}
```

## 6. Handlers 骨架

### 6.1 BaseCoordinationHandler

```go
type EventCallback func(ctx context.Context, event CoordinationEvent)

type BaseCoordinationHandler struct {
    round     AgentRoundController
    lifecycle TeamLifecycleController
    poll      PollController
    blueprint *blueprint.TeamAgentBlueprint
    infra     *infra.TeamInfra
}

func (h *BaseCoordinationHandler) GetCallbacks() map[string]EventCallback {
    return map[string]EventCallback{}  // 子类覆盖
}
```

### 6.2 六个 Handler EVENT_METHOD_MAP

| Handler | EVENT_METHOD_MAP | 额外字段 |
|---------|-----------------|----------|
| AgentLifecycleHandler | `user_input→OnUserInput, standby→OnStandby, cleaned→OnCleaned, tool_approval_result→OnToolApprovalResult` | — |
| MemberHandler | `member_joined→OnMemberEvent, member_left→OnMemberEvent, member_shutdown→OnMemberEvent, member_canceled→OnMemberEvent, member_status_changed→OnMemberEvent, member_error→OnMemberEvent` | `staleClaimThrottle map[string]float64`（共享） |
| MessageHandler | `message→OnMessageOrBroadcast, broadcast→OnMessageOrBroadcast, member_shutdown→OnMemberShutdownDrain, coordination_poll_mailbox→OnPollMailbox` | — |
| TaskBoardHandler | `task_claimed→OnTaskClaimed, task_created→OnTaskBoardEvent, task_updated→OnTaskBoardEvent, task_completed→OnTaskBoardEvent, task_failed→OnTaskBoardEvent, task_canceled→OnTaskBoardEvent` | — |
| StaleTaskHandler | `coordination_poll_task→OnPollTask` | `staleClaimThrottle map[string]float64`（共享）+ `lastPendingNudge map[string]float64`（独占） |
| TeamCompletionHandler | `coordination_poll_task→OnPollTask, task_list_drained→OnTaskListDrained, team_completed→OnTeamCompleted` | `teamCompletedEmitted bool` + `completionCallbacks []func(...)` |

**Fan-out 顺序由注册顺序保证**：
- `POLL_TASK`：StaleTaskHandler.OnPollTask 先于 TeamCompletionHandler.OnPollTask
- `MEMBER_SHUTDOWN`：MemberHandler.OnMemberEvent 先于 MessageHandler.OnMemberShutdownDrain

所有方法体 `TODO(#9.55)` 占位。TeamCompletionHandler 额外暴露 `Rearm()` 和 `RegisterCompletionCallback(fn)`。

## 7. CoordinationKernel

```go
type KernelLifecycleState string
const (
    KernelLifecycleIdle    KernelLifecycleState = "idle"
    KernelLifecycleRunning KernelLifecycleState = "running"
    KernelLifecyclePaused  KernelLifecycleState = "paused"
    KernelLifecycleStopped KernelLifecycleState = "stopped"
)

type CoordinationKernel struct {
    host            DispatcherHost
    subscribedTopics []string
    eventBus        *EventBus
    dispatcher      *EventDispatcher
    lifecycleState  KernelLifecycleState
}
```

### 7.1 构造顺序（打破循环依赖）

```
NewCoordinationKernel(host)
    ↓
Setup(role)
    ├─ NewEventBus(role, 30.0, 30.0)
    ├─ NewEventDispatcher(host, blueprint, infra, eventBus)  // bus 作为 PollController
    └─ 赋值 k.eventBus, k.dispatcher
    ↓
Start(ctx, session)
    ├─ TODO(#9.55): session 绑定 / team backend / workspace / memory / status
    ├─ eventBus.Start(ctx, dispatcher.Dispatch)  // 闭合循环依赖
    ├─ TODO(#9.55): subscribe_transport
    └─ dispatcher.TeamCompletion.Rearm()
```

### 7.2 生命周期方法

| 方法 | Python 对应 | 说明 |
|------|-------------|------|
| `Setup(role)` | `CoordinationKernel.setup` | 构造 bus + dispatcher |
| `Start(ctx, session)` | `CoordinationKernel.start` | 启动 bus + 订阅 transport |
| `Pause(ctx)` | `CoordinationKernel.pause` | 暂停（持久团队），仅 running → paused 有效，其余状态幂等返回 |
| `Stop(ctx)` | `CoordinationKernel.stop` | 停止（终态），任何状态 → stopped |
| `Enqueue(event)` | `CoordinationKernel.enqueue` | 转发到 eventBus |
| `EnqueueUserInput(inputs)` | `CoordinationKernel.enqueue_user_input` | 便捷方法 |
| `EnqueueMailboxAfterFirstIteration()` | `CoordinationKernel.enqueue_mailbox_after_first_iteration` | 便捷方法 |
| `WakeMailboxIfInterruptCleared()` | `CoordinationKernel.wake_mailbox_if_interrupt_cleared` | 便捷方法 |
| `FinalizeRound(ctx)` | `CoordinationKernel.finalize_round` | round 结束清理 |
| `SubscribeTransport(ctx, teamName)` | `CoordinationKernel.subscribe_transport` | Messager 订阅 |
| `UnsubscribeTransport()` | `CoordinationKernel.unsubscribe_transport` | Messager 退订 |

幂等性：Pause 仅 `running → paused` 有效（其余状态幂等返回 nil），Stop 终态为 `stopped`（idle/stopped 幂等返回 nil，running/paused → stopped）。

## 8. 文件组织

```
internal/agent_teams/agent/coordination/
├── doc.go                         # 包文档
├── event_bus.go                   # EventBus + InnerEventType + InnerEventMessage + CoordinationEvent + WakeCallback
├── event_bus_test.go              # EventBus 测试
├── dispatcher.go                  # EventDispatcher + 3 Protocol 接口 + DispatcherHost + 适配层
├── dispatcher_test.go             # EventDispatcher 测试
├── kernel.go                      # CoordinationKernel
├── kernel_test.go                 # CoordinationKernel 测试
└── handlers/
    ├── doc.go                     # handlers 子包文档
    ├── base.go                    # BaseCoordinationHandler + EventCallback 类型
    ├── base_test.go
    ├── agent_lifecycle.go         # AgentLifecycleHandler 骨架
    ├── agent_lifecycle_test.go
    ├── member.go                  # MemberHandler 骨架
    ├── member_test.go
    ├── message.go                 # MessageHandler 骨架
    ├── message_test.go
    ├── task_board.go              # TaskBoardHandler 骨架
    ├── task_board_test.go
    ├── stale_task.go              # StaleTaskHandler 骨架
    ├── stale_task_test.go
    ├── team_completion.go         # TeamCompletionHandler 骨架
    └── team_completion_test.go
```

## 9. 测试策略

### 9.1 可独立测试的部分

| 测试目标 | 测试内容 | mock 方式 |
|----------|----------|-----------|
| EventBus 生命周期 | Start → Enqueue → WakeCallback 被调用 → Stop | fake WakeCallback，channel 读取验证 |
| EventBus 轮询 | Start 后定时收到 POLL_MAILBOX / POLL_TASK | 短间隔（0.1s）验证 eventCh |
| EventBus PausePolls/ResumePolls | 暂停后无轮询事件，恢复后有 | 同上 |
| EventBus HUMAN_AGENT 无轮询 | NewEventBus(HumanAgent) 后无 poll goroutine | 验证无轮询事件到达 |
| EventBus 串行消费 | 连续 Enqueue 两个事件，WakeCallback 按顺序收到 | channel FIFO 天然保证 |
| CoordinationEvent 包装 | Inner/Transport 互斥 + EventType() | 纯逻辑测试 |
| EventDispatcher 粗筛 | agent_ready=false 跳过；inner + HumanAgent + POLL 跳过；transport 白名单 | fake DispatcherHost |
| EventDispatcher 触发 | 注册 handler 后 trigger 对应 event_key | fake handler 记录调用 |
| EventDispatcher fan-out 顺序 | 同一 event_key 上两个 handler 按注册顺序 | 记录调用序号 |
| CoordinationKernel Setup | 构造后 eventBus 和 dispatcher 非 nil | fake host |
| CoordinationKernel 生命周期 | idle → running → paused → stopped | 检查 IsRunning() |
| BaseCoordinationHandler | GetCallbacks 返回子类 EVENT_METHOD_MAP | 子类测试 |
| 6 个 Handler 骨架 | EVENT_METHOD_MAP 正确性 + 方法可调用 | 直接调方法 |

### 9.2 依赖 9.55 无法测试的部分（TODO）

- kernel.start() 中的 session 绑定、team backend 初始化、workspace/memory toolkit
- handler 方法体内业务逻辑
- transport 订阅/退订（依赖 Messager）

## 10. 回填标记

### 10.1 9.62+9.63 产生的 ⤵️ 标记

| 文件 | 标记 | 回填内容 |
|------|------|----------|
| `coordination/kernel.go` | 多处 `TODO(#9.55)` | session 绑定、team backend 初始化、workspace/memory toolkit、status 更新 |
| `coordination/handlers/*.go` | 每个方法体 `TODO(#9.55)` | handler 业务逻辑实现 |
| `coordination/kernel.go` | `TODO(#9.55)` | SubscribeTransport / UnsubscribeTransport |
| `coordination/kernel.go` | `TODO(#9.55)` | host.MemberName / host.Role 等属性访问 |

### 10.2 9.62+9.63 完成后消除的 ⤵️ 标记

| 原位置 | 原标记 | 消除方式 |
|--------|--------|----------|
| `team_agent.go:31` | `TODO(#9.62-9.63) 协调子系统` | 替换为实际文件目录 |
| `team_agent.go:32` | `TODO(#9.62) 协调内核` | 替换为 `kernel.go` |
| `team_agent.go:33` | `TODO(#9.63) 事件总线` | 替换为 `event_bus.go` |
| `team_agent.go:34` | `TODO(#9.63) 事件分发器` | 替换为 `dispatcher.go` |
| `team_agent.go:35` | `TODO(#9.63) 事件处理器` | 替换为 `handlers/` |
| `team_agent.go:84` | `TODO(#9.62): CoordinationKernel 类型` | 替换为 `coordination *CoordinationKernel`（接线在 9.55） |

### 10.3 9.55 完成时才能消除的标记

以下标记 9.62+9.63 只提供类型定义，TeamAgent 真正接线在 9.55：

| 文件 | 标记 |
|------|------|
| `team_agent.go:114` | `⤵️ 待 9.62: WithWakeMailbox / WithRequestCompletionPoll` |
| `team_agent.go:116` | `TODO(#9.62): 构建 CoordinationKernel(self)` |
| `team_agent.go:195/205` | `TODO(#9.62): 返回 coordination / coordination.EventBus()` |
| `team_agent.go:478-512` | `TODO(#9.62): coordination.Setup/Start/Enqueue/FinalizeRound` |
| `team_agent.go:668-689` | `TODO(#9.62): start/pause/stop_coordination` |
| `stream_controller.go:43,46` | `⤵️ 待 9.62: wakeMailboxCb / requestCompletionPollCb` |
| `runtime/manager.go:142-175` | `⤵️ 待 9.62: coordination 生命周期调用` |

### 10.4 实现计划状态更新

```
| 9.62 | ✅ | CoordinationKernel | 协调内核（EventBus+EventDispatcher+3 Protocol+6 Handler 骨架+CallbackFramework 适配层+kernel facade+测试）；⤵️ 9.55 回填 TeamAgent 接线 + handler 业务逻辑 |
| 9.63 | ✅ | EventBus / Dispatcher | 已合并至 9.62 |
```
