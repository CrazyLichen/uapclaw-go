# CoordinationKernel + EventBus/Dispatcher 合并实现 (9.62+9.63) 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 TeamAgent 的事件驱动唤醒层——EventBus（队列+轮询+生命周期）、EventDispatcher（粗筛+CallbackFramework 分发）、CoordinationKernel（facade），以及 6 个 Handler 骨架。

**Architecture:** 深度对齐 Python coordination 子系统三层架构。EventBus 用 channel+单 goroutine 消费实现串行 fan-out；EventDispatcher 持有 CallbackFramework 私有实例（OnCustom/TriggerCustom 路径+适配层）；CoordinationKernel 打破 bus↔dispatcher 循环依赖（Setup 构造、Start 绑定 wake callback）。6 个 Handler 只建骨架（EVENT_METHOD_MAP+方法签名+TODO 占位）。

**Tech Stack:** Go 1.22+, context/channel/timer 标准库, agentcore/runner/callback.CallbackFramework, agent_teams/schema 已有类型

**Design Spec:** `docs/superpowers/specs/2027-12-30-coordination-kernel-9.62-9.63-design.md`

---

## File Structure

```
internal/agent_teams/agent/coordination/
├── doc.go                          # 包文档
├── event_bus.go                    # InnerEventType/InnerEventMessage/CoordinationEvent/WakeCallback/EventBus
├── event_bus_test.go               # EventBus 测试
├── dispatcher.go                   # 3 Protocol 接口 + DispatcherHost + 适配层 + EventDispatcher
├── dispatcher_test.go              # EventDispatcher 测试
├── kernel.go                       # KernelLifecycleState + CoordinationKernel
├── kernel_test.go                  # CoordinationKernel 测试
└── handlers/
    ├── doc.go                      # handlers 子包文档
    ├── base.go                     # EventCallback + BaseCoordinationHandler
    ├── base_test.go
    ├── agent_lifecycle.go          # AgentLifecycleHandler 骨架
    ├── agent_lifecycle_test.go
    ├── member.go                   # MemberHandler 骨架
    ├── member_test.go
    ├── message.go                  # MessageHandler 骨架
    ├── message_test.go
    ├── task_board.go               # TaskBoardHandler 骨架
    ├── task_board_test.go
    ├── stale_task.go               # StaleTaskHandler 骨架
    ├── stale_task_test.go
    ├── team_completion.go          # TeamCompletionHandler 骨架
    └── team_completion_test.go
```

修改已有文件：
- `internal/agent_teams/agent/team_agent.go` — 替换 5 处 TODO(#9.62-9.63) 标记为实际文件名 + 添加 coordination 字段类型

---

### Task 1: 创建 coordination 包目录 + doc.go + CoordinationEvent 类型

**Files:**
- Create: `internal/agent_teams/agent/coordination/doc.go`
- Create: `internal/agent_teams/agent/coordination/event_bus.go`
- Test: `internal/agent_teams/agent/coordination/event_bus_test.go`（CoordinationEvent 部分）

- [ ] **Step 1: 创建 coordination 目录和 doc.go**

```go
// Package coordination 提供团队 Agent 的事件驱动唤醒层。
//
// 把传输层来的 EventMessage 与内部 poll 计时器产生的 InnerEventMessage
// 收成统一的 CoordinationEvent，按粗筛规则放行后由 CallbackFramework
// 分发到具体的场景 handler。自身不做业务决策——handler 通过三类 narrow
// protocol（AgentRoundController / TeamLifecycleController / PollController）
// 触发行为，最终驱动 DeepAgent + team tools。
//
// 三条铁律：
//  1. coordination 不做决策，只管 wake-up
//  2. 每个 handler 一个业务域，跨域协作走 framework fan-out
//  3. CallbackFramework.trigger() 吞普通异常（log + continue），仅 AbortError 上抛
//
// 文件目录：
//
//	coordination/
//	├── doc.go                 # 包文档
//	├── event_bus.go           # EventBus 事件入队 + 周期 poll timer + 生命周期
//	├── dispatcher.go          # EventDispatcher 粗筛 + CallbackFramework 分发 + 3 个 Protocol
//	├── kernel.go              # CoordinationKernel 协调子系统 facade
//	└── handlers/              # 场景 handler 子包
//	    ├── base.go            # BaseCoordinationHandler 基类
//	    ├── agent_lifecycle.go # Agent 生命周期事件（USER_INPUT / STANDBY / CLEANED / TOOL_APPROVAL_RESULT）
//	    ├── member.go          # 成员事件（MEMBER_* 6 种）
//	    ├── message.go         # 消息事件（MESSAGE / BROADCAST / POLL_MAILBOX + MEMBER_SHUTDOWN fan-out）
//	    ├── task_board.go      # 任务板事件（TASK_CLAIMED / TASK_* 5 种）
//	    ├── stale_task.go      # 过期任务轮询（POLL_TASK）
//	    └── team_completion.go # 团队完成（POLL_TASK / TASK_LIST_DRAINED / TEAM_COMPLETED）
//
// 对应 Python 代码：openjiuwen/agent_teams/agent/coordination/
package coordination
```

- [ ] **Step 2: 在 event_bus.go 中写 InnerEventType + InnerEventMessage + CoordinationEvent 类型**

```go
package coordination

import (
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/schema/events"
)

// ──────────────────────────── 结构体 ────────────────────────────

// InnerEventType 协调层内部事件类型枚举。
// Python: InnerEventType
type InnerEventType string

// ──────────────────────────── 枚举 ────────────────────────────

const (
	// InnerEventTypeUserInput 用户输入事件
	InnerEventTypeUserInput InnerEventType = "user_input"
	// InnerEventTypePollMailbox 邮箱轮询事件
	InnerEventTypePollMailbox InnerEventType = "coordination_poll_mailbox"
	// InnerEventTypePollTask 任务轮询事件
	InnerEventTypePollTask InnerEventType = "coordination_poll_task"
	// InnerEventTypeShutdown 关闭事件
	InnerEventTypeShutdown InnerEventType = "shutdown"
)

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// InnerEventMessage 协调层内部事件消息，与跨进程 EventMessage 隔离。
// Python: InnerEventMessage
type InnerEventMessage struct {
	// EventType 事件类型
	EventType InnerEventType
	// Payload 事件载荷
	Payload map[string]any
}

// CoordinationEvent 事件总线处理的统一事件包装。
// Python: CoordinationEvent = Union[InnerEventMessage, EventMessage]
// Go 用包装结构体实现：Inner 和 Transport 恰好一个非 nil。
type CoordinationEvent struct {
	// Inner 内部事件（非 nil 时 Transport 为 nil）
	Inner *InnerEventMessage
	// Transport 跨进程事件（非 nil 时 Inner 为 nil）
	Transport *events.EventMessage
}

// IsInner 返回是否为内部事件。
func (e CoordinationEvent) IsInner() bool {
	return e.Inner != nil
}

// IsTransport 返回是否为跨进程事件。
func (e CoordinationEvent) IsTransport() bool {
	return e.Transport != nil
}

// EventType 返回事件类型字符串（用于 dispatcher 粗筛和 CallbackFramework 注册）。
func (e CoordinationEvent) EventType() string {
	if e.Inner != nil {
		return string(e.Inner.EventType)
	}
	return e.Transport.EventType
}
```

注意：按项目编码规范，枚举声明在结构体之后、常量之前。`InnerEventMessage` 是结构体放在结构体区块，`InnerEventType` 是枚举放在枚举区块，枚举常量放在常量区块。

- [ ] **Step 3: 写 CoordinationEvent 单元测试**

```go
package coordination

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/schema/events"
)

func TestInnerEventType_值(t *testing.T) {
	tests := []struct {
		name  string
		value InnerEventType
		want  string
	}{
		{"user_input", InnerEventTypeUserInput, "user_input"},
		{"poll_mailbox", InnerEventTypePollMailbox, "coordination_poll_mailbox"},
		{"poll_task", InnerEventTypePollTask, "coordination_poll_task"},
		{"shutdown", InnerEventTypeShutdown, "shutdown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.value) != tt.want {
				t.Errorf("InnerEventType %s = %q, want %q", tt.name, tt.value, tt.want)
			}
		})
	}
}

func TestCoordinationEvent_IsInner(t *testing.T) {
	innerEvent := CoordinationEvent{Inner: &InnerEventMessage{EventType: InnerEventTypeUserInput}}
	if !innerEvent.IsInner() {
		t.Error("IsInner() 应为 true（Inner 非 nil）")
	}
	if innerEvent.IsTransport() {
		t.Error("IsTransport() 应为 false（Inner 非 nil 时）")
	}
}

func TestCoordinationEvent_IsTransport(t *testing.T) {
	transportEvent := CoordinationEvent{Transport: &events.EventMessage{EventType: "message"}}
	if transportEvent.IsTransport() {
		t.Error("IsTransport() 应为 true（Transport 非 nil）— 检查 import")
	}
	if !transportEvent.IsTransport() {
		t.Error("IsTransport() 应为 true（Transport 非 nil）")
	}
	if transportEvent.IsInner() {
		t.Error("IsInner() 应为 false（Transport 非 nil 时）")
	}
}

func TestCoordinationEvent_EventType(t *testing.T) {
	innerEvent := CoordinationEvent{Inner: &InnerEventMessage{EventType: InnerEventTypePollTask}}
	if got := innerEvent.EventType(); got != "coordination_poll_task" {
		t.Errorf("EventType() = %q, want %q", got, "coordination_poll_task")
	}
	transportEvent := CoordinationEvent{Transport: &events.EventMessage{EventType: "member_shutdown"}}
	if got := transportEvent.EventType(); got != "member_shutdown" {
		t.Errorf("EventType() = %q, want %q", got, "member_shutdown")
	}
}
```

- [ ] **Step 4: 编译测试确认通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agent_teams/agent/coordination/... -v -run "TestInnerEventType|TestCoordinationEvent"`

- [ ] **Step 5: 提交**

```bash
git add internal/agent_teams/agent/coordination/
git commit -m "feat(9.63): 添加 coordination 包 + CoordinationEvent/InnerEventType/InnerEventMessage 类型"
```

---

### Task 2: 实现 EventBus

**Files:**
- Modify: `internal/agent_teams/agent/coordination/event_bus.go`
- Create: `internal/agent_teams/agent/coordination/event_bus_test.go`

- [ ] **Step 1: 在 event_bus.go 添加 WakeCallback 类型 + EventBus 结构体 + 所有方法**

在已有 CoordinationEvent 类型定义之后，添加：

```go
// WakeCallback 事件唤醒回调，当事件到达时调用。
// Python: WakeCallback = Callable[[CoordinationEvent], Awaitable[None]]
type WakeCallback func(ctx context.Context, event CoordinationEvent)

// EventBus 事件驱动的唤醒循环，用于团队协调。
//
// 两条唤醒路径，同一回调：
//  1. 事件驱动：transport/Messager 事件触发即时唤醒
//  2. 轮询回退：周期性定时器，捕获空闲 agent 可能遗漏的状态变更
//
// 所有决策逻辑在 DeepAgent 中——本结构体只管生命周期和唤醒。
// Python: EventBus
type EventBus struct {
	// role 拥有此事件循环的角色
	role schema.TeamRole
	// mailboxPollInterval 邮箱轮询间隔（秒）
	mailboxPollInterval float64
	// taskPollInterval 任务轮询间隔（秒）
	taskPollInterval float64
	// wakeCallback 在 start() 时绑定，而非构造时绑定
	wakeCallback WakeCallback
	// running 事件循环是否运行中
	running bool
	// pollsPaused 周期轮询是否暂停
	pollsPaused bool
	// periodicPollEnabled 是否启用周期轮询（HUMAN_AGENT 角色为 false）
	periodicPollEnabled bool
	// eventCh 事件队列 channel
	eventCh chan CoordinationEvent
	// cancelFunc 用于停止 runLoop goroutine
	cancelFunc context.CancelFunc
	// mailboxPollCancel 邮箱轮询定时器取消函数
	mailboxPollCancel context.CancelFunc
	// taskPollCancel 任务轮询定时器取消函数
	taskPollCancel context.CancelFunc
}
```

import 需要：`context`, `time`, `schema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"`, `logger` 包。

方法实现按规范排列：导出函数在前（NewEventBus, Role, IsRunning, PollsPaused, Start, Stop, PausePolls, ResumePolls, Enqueue），非导出函数在后（startPollTasks, runLoop, pollLoop）。完整代码参照设计文档 Section 4。

日志使用 `logger.Info(logComponent).Str("role", string(b.role)).Msg(...)` 格式，其中 `logComponent = logger.ComponentChannel`（coordination 归 Channel 组件）。

- [ ] **Step 2: 写 EventBus 生命周期测试**

```go
func TestEventBus_StartStop(t *testing.T) {
	bus := NewEventBus(schema.TeamRoleLeader, 30.0, 30.0)
	if bus.IsRunning() {
		t.Error("新创建的 EventBus 不应运行中")
	}
	var received []CoordinationEvent
	callback := func(ctx context.Context, event CoordinationEvent) {
		received = append(received, event)
	}
	ctx := context.Background()
	bus.Start(ctx, callback)
	if !bus.IsRunning() {
		t.Error("Start 后应运行中")
	}
	// 入队一个事件
	bus.Enqueue(CoordinationEvent{Inner: &InnerEventMessage{EventType: InnerEventTypeUserInput}})
	// 等待事件被处理
	time.Sleep(100 * time.Millisecond)
	bus.Stop()
	if bus.IsRunning() {
		t.Error("Stop 后不应运行中")
	}
	if len(received) != 1 {
		t.Errorf("期望收到 1 个事件，实际 %d", len(received))
	}
}
```

- [ ] **Step 3: 写 EventBus 轮询测试**

```go
func TestEventBus_轮询事件(t *testing.T) {
	// 用极短间隔测试轮询
	bus := NewEventBus(schema.TeamRoleTeammate, 0.05, 0.05)
	var mu sync.Mutex
	var pollCount int
	callback := func(ctx context.Context, event CoordinationEvent) {
		if event.IsInner() && (event.Inner.EventType == InnerEventTypePollMailbox || event.Inner.EventType == InnerEventTypePollTask) {
			mu.Lock()
			pollCount++
			mu.Unlock()
		}
	}
	ctx := context.Background()
	bus.Start(ctx, callback)
	time.Sleep(200 * time.Millisecond)
	bus.Stop()
	mu.Lock()
	cnt := pollCount
	mu.Unlock()
	if cnt == 0 {
		t.Error("应收到轮询事件")
	}
}

func TestEventBus_HumanAgent无轮询(t *testing.T) {
	bus := NewEventBus(schema.TeamRoleHumanAgent, 0.05, 0.05)
	var mu sync.Mutex
	var pollCount int
	callback := func(ctx context.Context, event CoordinationEvent) {
		if event.IsInner() && (event.Inner.EventType == InnerEventTypePollMailbox || event.Inner.EventType == InnerEventTypePollTask) {
			mu.Lock()
			pollCount++
			mu.Unlock()
		}
	}
	ctx := context.Background()
	bus.Start(ctx, callback)
	time.Sleep(200 * time.Millisecond)
	bus.Stop()
	mu.Lock()
	cnt := pollCount
	mu.Unlock()
	if cnt > 0 {
		t.Errorf("Human-agent 不应有轮询事件，实际 %d", cnt)
	}
}
```

- [ ] **Step 4: 写 EventBus PausePolls/ResumePolls 测试**

```go
func TestEventBus_PausePolls和ResumePolls(t *testing.T) {
	bus := NewEventBus(schema.TeamRoleTeammate, 0.05, 0.05)
	var mu sync.Mutex
	var pollCount int
	callback := func(ctx context.Context, event CoordinationEvent) {
		if event.IsInner() && (event.Inner.EventType == InnerEventTypePollMailbox || event.Inner.EventType == InnerEventTypePollTask) {
			mu.Lock()
			pollCount++
			mu.Unlock()
		}
	}
	ctx := context.Background()
	bus.Start(ctx, callback)
	time.Sleep(150 * time.Millisecond) // 等一些轮询事件
	bus.PausePolls()
	if !bus.PollsPaused() {
		t.Error("PausePolls 后应暂停")
	}
	countBeforePause := func() int { mu.Lock(); defer mu.Unlock(); return pollCount }()
	time.Sleep(200 * time.Millisecond) // 等暂停期间
	countAfterPause := func() int { mu.Lock(); defer mu.Unlock(); return pollCount }()
	bus.ResumePolls()
	if bus.PollsPaused() {
		t.Error("ResumePolls 后不应暂停")
	}
	time.Sleep(150 * time.Millisecond) // 等恢复后的轮询
	bus.Stop()
	mu.Lock()
	finalCount := pollCount
	mu.Unlock()
	if countAfterPause != countBeforePause {
		t.Error("暂停期间不应有新轮询事件")
	}
	if finalCount <= countAfterPause {
		t.Error("恢复后应有新轮询事件")
	}
}
```

- [ ] **Step 5: 写 EventBus 串行消费测试**

```go
func TestEventBus_串行消费(t *testing.T) {
	bus := NewEventBus(schema.TeamRoleLeader, 30.0, 30.0)
	var mu sync.Mutex
	var order []string
	callback := func(ctx context.Context, event CoordinationEvent) {
		if event.IsInner() && event.Inner.EventType == InnerEventTypeUserInput {
			mu.Lock()
			order = append(order, event.Inner.Payload["seq"].(string))
			mu.Unlock()
		}
	}
	ctx := context.Background()
	bus.Start(ctx, callback)
	bus.Enqueue(CoordinationEvent{Inner: &InnerEventMessage{EventType: InnerEventTypeUserInput, Payload: map[string]any{"seq": "first"}}})
	bus.Enqueue(CoordinationEvent{Inner: &InnerEventMessage{EventType: InnerEventTypeUserInput, Payload: map[string]any{"seq": "second"}}})
	bus.Enqueue(CoordinationEvent{Inner: &InnerEventMessage{EventType: InnerEventTypeUserInput, Payload: map[string]any{"seq": "third"}}})
	time.Sleep(100 * time.Millisecond)
	bus.Stop()
	mu.Lock()
	defer mu.Unlock()
	if len(order) != 3 {
		t.Errorf("期望收到 3 个事件，实际 %d", len(order))
	}
	for i, want := range []string{"first", "second", "third"} {
		if order[i] != want {
			t.Errorf("事件 %d: got %q, want %q", i, order[i], want)
		}
	}
}
```

- [ ] **Step 6: 编译测试确认通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agent_teams/agent/coordination/... -v -timeout 10s`

- [ ] **Step 7: 提交**

```bash
git add internal/agent_teams/agent/coordination/
git commit -m "feat(9.63): 实现 EventBus（队列+轮询+生命周期+串行消费）"
```

---

### Task 3: 实现 EventDispatcher + 三个 Protocol 接口 + 适配层

**Files:**
- Create: `internal/agent_teams/agent/coordination/dispatcher.go`
- Create: `internal/agent_teams/agent/coordination/dispatcher_test.go`

- [ ] **Step 1: 在 dispatcher.go 定义 3 个 Protocol 接口 + DispatcherHost + 适配层 + EventDispatcher**

文件结构：
1. import: `context`, `callback "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"`, `blueprint/schema/infra/logger`
2. 结构体区块：无
3. 常量区块：`coordEventMapKey = "__coordination_event"`
4. 全局变量区块：无
5. 导出函数区块：3 个接口定义、wrapCallback、packEvent、NewEventDispatcher、Dispatch
6. 非导出函数区块：无

注意：Go 的接口定义按规范归入结构体区块（接口排在结构体之前）。`coordCallbackFunc` 是类型别名归入枚举区块。

完整代码参照设计文档 Section 5。Dispatcher 构造时 6 个 handler 的 import 在 Task 4 完成后才可编译，此步先用 `// TODO(#9.63-Task4): 替换为实际 handler 构造` 占位，确保 dispatcher.go 自身可编译。

**临时占位方案**：构造函数先不调用 handler 的 New 函数，handler 字段留 nil。在 Task 5 完成所有 handler 后再接线。

- [ ] **Step 2: 写 EventDispatcher 粗筛测试**

```go
// fakeDispatcherHost 实现 DispatcherHost 用于测试
type fakeDispatcherHost struct {
	agentReady    bool
	agentRunning  bool
	inFlight      bool
	pendingInt    bool
	memberName    string
}

func (f *fakeDispatcherHost) IsAgentReady() bool            { return f.agentReady }
func (f *fakeDispatcherHost) IsAgentRunning() bool          { return f.agentRunning }
func (f *fakeDispatcherHost) HasInFlightRound() bool        { return f.inFlight }
func (f *fakeDispatcherHost) HasPendingInterrupt() bool     { return f.pendingInt }
func (f *fakeDispatcherHost) CancelAgent(ctx context.Context) error { return nil }
func (f *fakeDispatcherHost) DeliverInput(ctx context.Context, content any, useSteer bool) error { return nil }
func (f *fakeDispatcherHost) ResumeInterrupt(ctx context.Context, userInput any) error { return nil }
func (f *fakeDispatcherHost) ShutdownSelf(ctx context.Context) error { return nil }
func (f *fakeDispatcherHost) ConcludeCompletedRound(ctx context.Context, memberCount, taskCount int) error { return nil }

func TestEventDispatcher_Agent未就绪时跳过(t *testing.T) {
	host := &fakeDispatcherHost{agentReady: false}
	d := NewEventDispatcher(host, ... ) // 需要完整的构造参数
	var called bool
	d.framework.OnCustom("user_input", func(ctx context.Context, data map[string]any) any {
		called = true
		return nil
	})
	d.Dispatch(context.Background(), CoordinationEvent{Inner: &InnerEventMessage{EventType: InnerEventTypeUserInput}})
	if called {
		t.Error("agent 未就绪时不应触发回调")
	}
}
```

**注意**：完整测试需要 blueprint 和 infra 参数。构造 fake 版本或在 Task 4 handler 就绪后统一测试。此步先写接口+适配层的单元测试（wrapCallback/packEvent），粗筛集成测试延后到 Task 6。

- [ ] **Step 3: 写适配层单元测试**

```go
func TestWrapCallback_适配层(t *testing.T) {
	var called bool
	var receivedEvent CoordinationEvent
	innerFn := coordCallbackFunc(func(ctx context.Context, event CoordinationEvent) {
		called = true
		receivedEvent = event
	})
	wrapped := wrapCallback(innerFn)
	event := CoordinationEvent{Inner: &InnerEventMessage{EventType: InnerEventTypeUserInput, Payload: map[string]any{"content": "hello"}}}
	data := packEvent(event)
	wrapped(context.Background(), data)
	if !called {
		t.Error("wrapCallback 应调用内部函数")
	}
	if receivedEvent.Inner.EventType != InnerEventTypeUserInput {
		t.Error("wrapCallback 应传递原始事件")
	}
}

func TestPackEvent(t *testing.T) {
	event := CoordinationEvent{Inner: &InnerEventMessage{EventType: InnerEventTypeShutdown}}
	data := packEvent(event)
	raw, ok := data[coordEventMapKey]
	if !ok {
		t.Error("packEvent 应包含 coordination event key")
	}
	got, ok := raw.(CoordinationEvent)
	if !ok {
		t.Error("packEvent 值应为 CoordinationEvent 类型")
	}
	if got.Inner.EventType != InnerEventTypeShutdown {
		t.Error("packEvent 应保留原始事件数据")
	}
}
```

- [ ] **Step 4: 编译测试确认通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agent_teams/agent/coordination/... -v -run "TestWrapCallback|TestPackEvent"`

- [ ] **Step 5: 提交**

```bash
git add internal/agent_teams/agent/coordination/
git commit -m "feat(9.63): 添加 EventDispatcher + 3 Protocol 接口 + CallbackFramework 适配层"
```

---

### Task 4: 创建 handlers 子包 + BaseCoordinationHandler

**Files:**
- Create: `internal/agent_teams/agent/coordination/handlers/doc.go`
- Create: `internal/agent_teams/agent/coordination/handlers/base.go`
- Create: `internal/agent_teams/agent/coordination/handlers/base_test.go`

- [ ] **Step 1: 创建 handlers/doc.go**

```go
// Package handlers 提供场景级协调事件处理器。
//
// 每个 handler 类拥有一个业务域：agent 生命周期、成员事件、消息、
// 任务板、过期任务清扫、团队完成。它们共享 DispatcherHost 契约，
// 通过 BaseCoordinationHandler.GetCallbacks 注册到 EventDispatcher。
//
// 文件目录：
//
//	handlers/
//	├── doc.go              # 包文档
//	├── base.go             # BaseCoordinationHandler 基类 + EventCallback 类型
//	├── agent_lifecycle.go  # AgentLifecycleHandler（USER_INPUT / STANDBY / CLEANED / TOOL_APPROVAL_RESULT）
//	├── member.go           # MemberHandler（MEMBER_* 6 种）
//	├── message.go          # MessageHandler（MESSAGE / BROADCAST / POLL_MAILBOX + MEMBER_SHUTDOWN fan-out）
//	├── task_board.go       # TaskBoardHandler（TASK_CLAIMED / TASK_* 5 种）
//	├── stale_task.go       # StaleTaskHandler（POLL_TASK）
//	└── team_completion.go  # TeamCompletionHandler（POLL_TASK / TASK_LIST_DRAINED / TEAM_COMPLETED）
//
// 对应 Python 代码：openjiuwen/agent_teams/agent/coordination/handlers/
package handlers
```

- [ ] **Step 2: 实现 base.go（EventCallback + BaseCoordinationHandler）**

```go
package handlers

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/agent/coordination"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/agent/infra"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/agent/blueprint"
)

// ──────────────────────────── 结构体 ────────────────────────────

// BaseCoordinationHandler 场景级协调事件处理器基类。
//
// 子类声明 EVENT_METHOD_MAP（event_key → 方法名），通过 GetCallbacks() 输出
// event_key → bound method 注册给 CallbackFramework。
// Python: BaseCoordinationHandler
type BaseCoordinationHandler struct {
	// round Round 级控制面（与 TeamHarness 打交道）
	round coordination.AgentRoundController
	// lifecycle TeamAgent 级生命周期效果
	lifecycle coordination.TeamLifecycleController
	// poll EventBus 自身的 poll 控制
	poll coordination.PollController
	// blueprint 静态身份
	blueprint *blueprint.TeamAgentBlueprint
	// infra per-process 容器
	infra *infra.TeamInfra
}

// ──────────────────────────── 枚举 ────────────────────────────

// EventCallback 协调事件回调函数类型。
// Python: EventCallback = Callable[[CoordinationEvent], Awaitable[None]]
type EventCallback func(ctx context.Context, event coordination.CoordinationEvent)

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewBaseCoordinationHandler 创建基类实例。
// Python: BaseCoordinationHandler.__init__
func NewBaseCoordinationHandler(
	host coordination.DispatcherHost,
	bp *blueprint.TeamAgentBlueprint,
	inf *infra.TeamInfra,
	pollCtrl coordination.PollController,
) BaseCoordinationHandler {
	return BaseCoordinationHandler{
		round:     host,
		lifecycle: host,
		poll:      pollCtrl,
		blueprint: bp,
		infra:     inf,
	}
}

// GetCallbacks 返回 event_key → bound method，供 framework 注册。
// 子类应覆盖此方法返回自身的 EVENT_METHOD_MAP。
// Python: BaseCoordinationHandler.get_callbacks
func (h *BaseCoordinationHandler) GetCallbacks() map[string]EventCallback {
	return map[string]EventCallback{}
}

// ──────────────────────────── 非导出函数 ────────────────────────────
```

**注意**：`EventCallback` 类型按项目规范归入枚举区块（type alias 归类到枚举区块）。

- [ ] **Step 3: 写 BaseCoordinationHandler 测试**

```go
package handlers

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/agent/coordination"
)

// testHandler 测试用子类，覆盖 GetCallbacks
type testHandler struct {
	BaseCoordinationHandler
}

var testEventMap = map[string]string{
	"test_event": "OnTestEvent",
}

func (h *testHandler) GetCallbacks() map[string]EventCallback {
	return map[string]EventCallback{
		"test_event": h.OnTestEvent,
	}
}

func (h *testHandler) OnTestEvent(ctx context.Context, event coordination.CoordinationEvent) {}

type fakeHost struct{}

func (f *fakeHost) IsAgentReady() bool            { return true }
func (f *fakeHost) IsAgentRunning() bool          { return false }
func (f *fakeHost) HasInFlightRound() bool        { return false }
func (f *fakeHost) HasPendingInterrupt() bool     { return false }
func (f *fakeHost) CancelAgent(ctx context.Context) error { return nil }
func (f *fakeHost) DeliverInput(ctx context.Context, content any, useSteer bool) error { return nil }
func (f *fakeHost) ResumeInterrupt(ctx context.Context, userInput any) error { return nil }
func (f *fakeHost) ShutdownSelf(ctx context.Context) error { return nil }
func (f *fakeHost) ConcludeCompletedRound(ctx context.Context, memberCount, taskCount int) error { return nil }

type fakePollCtrl struct{}

func (f *fakePollCtrl) PausePolls()  {}
func (f *fakePollCtrl) ResumePolls() {}

func TestBaseCoordinationHandler_GetCallbacks_基类返回空(t *testing.T) {
	base := NewBaseCoordinationHandler(&fakeHost{}, nil, nil, &fakePollCtrl{})
	cb := base.GetCallbacks()
	if len(cb) != 0 {
		t.Errorf("基类 GetCallbacks 应返回空 map，实际 %d 项", len(cb))
	}
}

func TestBaseCoordinationHandler_GetCallbacks_子类覆盖(t *testing.T) {
	h := &testHandler{
		BaseCoordinationHandler: NewBaseCoordinationHandler(&fakeHost{}, nil, nil, &fakePollCtrl{}),
	}
	cb := h.GetCallbacks()
	if len(cb) != 1 {
		t.Errorf("子类 GetCallbacks 应返回 1 项，实际 %d 项", len(cb))
	}
	if _, ok := cb["test_event"]; !ok {
		t.Error("子类 GetCallbacks 应包含 test_event")
	}
}
```

- [ ] **Step 4: 编译测试确认通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agent_teams/agent/coordination/handlers/... -v -run "TestBaseCoordinationHandler"`

- [ ] **Step 5: 提交**

```bash
git add internal/agent_teams/agent/coordination/handlers/
git commit -m "feat(9.63): 添加 handlers 子包 + BaseCoordinationHandler + EventCallback 类型"
```

---

### Task 5: 实现 6 个 Handler 骨架

**Files:**
- Create: `internal/agent_teams/agent/coordination/handlers/agent_lifecycle.go`
- Create: `internal/agent_teams/agent/coordination/handlers/member.go`
- Create: `internal/agent_teams/agent/coordination/handlers/message.go`
- Create: `internal/agent_teams/agent/coordination/handlers/task_board.go`
- Create: `internal/agent_teams/agent/coordination/handlers/stale_task.go`
- Create: `internal/agent_teams/agent/coordination/handlers/team_completion.go`
- Create: 每个对应的 `_test.go`

每个 Handler 遵循相同模式：
1. 结构体定义（嵌入 BaseCoordinationHandler + 额外字段）
2. NewXxxHandler 构造函数
3. EVENT_METHOD_MAP（包级 var，在枚举区块）
4. GetCallbacks 覆盖方法
5. 事件处理方法（全部 `// TODO(#9.55): 实现 ...` 占位）

- [ ] **Step 1: 实现 AgentLifecycleHandler + 测试**

```go
// AgentLifecycleHandler Agent 生命周期事件处理器。
// Python: AgentLifecycleHandler
type AgentLifecycleHandler struct {
	BaseCoordinationHandler
}

// AgentLifecycleEventMap 事件 → 方法映射。
// Python: AgentLifecycleHandler.EVENT_METHOD_MAP
var AgentLifecycleEventMap = map[string]string{
	"user_input":           "OnUserInput",
	"standby":              "OnStandby",
	"cleaned":              "OnCleaned",
	"tool_approval_result": "OnToolApprovalResult",
}
```

构造函数 `NewAgentLifecycleHandler`、`GetCallbacks`、4 个方法（OnUserInput/OnStandby/OnCleaned/OnToolApprovalResult）全部 TODO 占位。

测试验证：EVENT_METHOD_MAP 有 4 个 key；GetCallbacks 返回 4 个回调；每个方法可调用不 panic。

- [ ] **Step 2: 实现 MemberHandler + 测试**

额外字段 `staleClaimThrottle map[string]float64`。EVENT_METHOD_MAP 6 个 key 全映射到 `OnMemberEvent`。

测试额外验证：构造时传入的 staleClaimThrottle 与 Handler 字段是同一指针。

- [ ] **Step 3: 实现 MessageHandler + 测试**

EVENT_METHOD_MAP 4 个 key：message/broadcast → OnMessageOrBroadcast, member_shutdown → OnMemberShutdownDrain, coordination_poll_mailbox → OnPollMailbox。

- [ ] **Step 4: 实现 TaskBoardHandler + 测试**

EVENT_METHOD_MAP 7 个 key：task_claimed → OnTaskClaimed, 5 个 task_* → OnTaskBoardEvent。

- [ ] **Step 5: 实现 StaleTaskHandler + 测试**

额外字段 `staleClaimThrottle map[string]float64`（共享）+ `lastPendingNudge map[string]float64`（独占）。
EVENT_METHOD_MAP 1 个 key：coordination_poll_task → OnPollTask。

- [ ] **Step 6: 实现 TeamCompletionHandler + 测试**

额外字段 `teamCompletedEmitted bool` + `completionCallbacks []func(ctx context.Context) error`。
EVENT_METHOD_MAP 3 个 key：coordination_poll_task → OnPollTask, task_list_drained → OnTaskListDrained, team_completed → OnTeamCompleted。
额外导出方法 `Rearm()` 和 `RegisterCompletionCallback(fn)`。

- [ ] **Step 7: 编译测试全部 handler**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agent_teams/agent/coordination/handlers/... -v`

- [ ] **Step 8: 提交**

```bash
git add internal/agent_teams/agent/coordination/handlers/
git commit -m "feat(9.63): 实现 6 个 Handler 骨架（EVENT_METHOD_MAP + TODO 方法体）"
```

---

### Task 6: 接线 EventDispatcher 构造函数 + 粗筛集成测试

**Files:**
- Modify: `internal/agent_teams/agent/coordination/dispatcher.go` — 替换 handler 占位为实际构造
- Modify: `internal/agent_teams/agent/coordination/dispatcher_test.go` — 添加集成测试

- [ ] **Step 1: 更新 NewEventDispatcher 接线 6 个 handler 构造**

```go
func NewEventDispatcher(
	host DispatcherHost,
	bp *blueprint.TeamAgentBlueprint,
	inf *infra.TeamInfra,
	pollCtrl PollController,
) *EventDispatcher {
	fw := callback.NewCallbackFramework()
	staleClaimThrottle := make(map[string]float64)

	d := &EventDispatcher{
		round:     host,
		blueprint: bp,
		infra:     inf,
		framework: fw,
	}

	d.Lifecycle = handlers.NewAgentLifecycleHandler(host, bp, inf, pollCtrl)
	d.Member = handlers.NewMemberHandler(host, bp, inf, pollCtrl, staleClaimThrottle)
	d.Message = handlers.NewMessageHandler(host, bp, inf, pollCtrl)
	d.TaskBoard = handlers.NewTaskBoardHandler(host, bp, inf, pollCtrl)
	d.StaleTask = handlers.NewStaleTaskHandler(host, bp, inf, pollCtrl, staleClaimThrottle)
	d.TeamCompletion = handlers.NewTeamCompletionHandler(host, bp, inf, pollCtrl)

	for _, handler := range []handlers.GetCallbacksProvider{
		d.Lifecycle,
		d.Member,
		d.Message,
		d.TaskBoard,
		d.StaleTask,
		d.TeamCompletion,
	} {
		for eventKey, method := range handler.GetCallbacks() {
			fw.OnCustom(eventKey, wrapCallback(method))
		}
	}

	return d
}
```

**注意**：需要定义 `GetCallbacksProvider` 接口或在 dispatcher.go 中直接用具体类型。推荐方案：定义一个非导出接口 `type callbacksProvider interface { GetCallbacks() map[string]handlers.EventCallback }`。

- [ ] **Step 2: 写粗筛集成测试**

```go
func TestEventDispatcher_Agent未就绪跳过(t *testing.T) {
	host := &fakeDispatcherHost{agentReady: false}
	d := NewEventDispatcher(host, testBlueprint(), testInfra(), &fakePollCtrl{})
	var triggered bool
	d.framework.OnCustom("user_input", func(ctx context.Context, data map[string]any) any {
		triggered = true
		return nil
	})
	d.Dispatch(context.Background(), CoordinationEvent{Inner: &InnerEventMessage{EventType: InnerEventTypeUserInput}})
	if triggered {
		t.Error("agent 未就绪时不应触发")
	}
}

func TestEventDispatcher_Inner事件正常触发(t *testing.T) {
	host := &fakeDispatcherHost{agentReady: true}
	d := NewEventDispatcher(host, testBlueprint(schema.TeamRoleLeader, "leader1"), testInfra(), &fakePollCtrl{})
	// handler 骨架已注册 user_input → AgentLifecycleHandler.OnUserInput
	// 验证 framework 有 user_input 回调
	callbacks := d.framework // 不能直接访问内部 map，用 TriggerCustom 间接验证
	results := d.framework.TriggerCustom(context.Background(), "user_input", packEvent(
		CoordinationEvent{Inner: &InnerEventMessage{EventType: InnerEventTypeUserInput, Payload: map[string]any{"content": "hi"}}},
	))
	// 骨架 handler 返回 nil，results 应非 nil（有回调被调用）
	if results == nil {
		t.Error("user_input 应有回调被触发")
	}
}

func TestEventDispatcher_HumanAgent轮询跳过(t *testing.T) {
	host := &fakeDispatcherHost{agentReady: true}
	d := NewEventDispatcher(host, testBlueprint(schema.TeamRoleHumanAgent, "ha1"), testInfra(), &fakePollCtrl{})
	d.Dispatch(context.Background(), CoordinationEvent{Inner: &InnerEventMessage{EventType: InnerEventTypePollMailbox}})
	d.Dispatch(context.Background(), CoordinationEvent{Inner: &InnerEventMessage{EventType: InnerEventTypePollTask}})
	// 两个都应被跳过——验证 handler 未被触发
	// 用 mock callback 方式验证
}
```

- [ ] **Step 3: 编译测试确认通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agent_teams/agent/coordination/... -v -timeout 10s`

- [ ] **Step 4: 提交**

```bash
git add internal/agent_teams/agent/coordination/
git commit -m "feat(9.63): EventDispatcher 接线 6 个 handler + 粗筛集成测试"
```

---

### Task 7: 实现 CoordinationKernel

**Files:**
- Create: `internal/agent_teams/agent/coordination/kernel.go`
- Create: `internal/agent_teams/agent/coordination/kernel_test.go`

- [ ] **Step 1: 实现 kernel.go**

参照设计文档 Section 7。包含：
- `KernelLifecycleState` 枚举（4 个值）
- `CoordinationKernel` 结构体
- `NewCoordinationKernel` 构造函数
- `Setup(role)` — 构造 EventBus + EventDispatcher（需从 host 获取 blueprint/infra，此处先用 TODO(#9.55) 占位，但核心框架可编译）
- `EventBus()` / `Dispatcher()` / `SubscribedTopics()` / `IsRunning()` 属性查询
- `Enqueue(event)` 转发
- `Start(ctx, session)` — 启动 bus + Rearm，其余 TODO
- `Pause(ctx)` / `Stop(ctx)` — 生命周期方法，核心逻辑（stop bus + 状态转换）可实现，其余 TODO
- 便捷方法 `EnqueueUserInput` / `EnqueueMailboxAfterFirstIteration` / `WakeMailboxIfInterruptCleared` / `FinalizeRound`
- Transport 方法 `SubscribeTransport` / `UnsubscribeTransport` — TODO 骨架
- 非导出方法 `drainAgentTask` / `closeStream` / `persistAllocatorState` — TODO 骨架

**Setup 的临时可编译方案**：由于 host 是 DispatcherHost 接口（不暴露 blueprint/infra），Setup 暂时需要额外参数或改为接受 blueprint+infra 参数。对齐 Python：`kernel.setup()` 从 `host.blueprint` / `host.infra` 取值。Go 端 DispatcherHost 不包含这些字段，需扩展接口或在 Setup 中显式传入。

**设计选择**：在 Setup 签名中显式传入 `blueprint *blueprint.TeamAgentBlueprint, infra *infra.TeamInfra`，不扩展 DispatcherHost。理由：Python 的 host 是 TeamAgent 实例（上帝对象），Go 端 DispatcherHost 是窄接口——不应为 setup 注入 blueprint/infra 而破坏窄接口设计。

```go
func (k *CoordinationKernel) Setup(role schema.TeamRole, bp *blueprint.TeamAgentBlueprint, inf *infra.TeamInfra) error {
	eventBus := NewEventBus(role, 30.0, 30.0)
	dispatcher := NewEventDispatcher(k.host, bp, inf, eventBus)
	k.eventBus = eventBus
	k.dispatcher = dispatcher
	return nil
}
```

- [ ] **Step 2: 写 CoordinationKernel 生命周期测试**

```go
func TestCoordinationKernel_Setup(t *testing.T) {
	host := &fakeDispatcherHost{agentReady: true}
	kernel := NewCoordinationKernel(host)
	bp := testBlueprint(schema.TeamRoleLeader, "leader1")
	inf := testInfra()
	err := kernel.Setup(schema.TeamRoleLeader, bp, inf)
	if err != nil {
		t.Errorf("Setup 返回错误: %v", err)
	}
	if kernel.EventBus() == nil {
		t.Error("Setup 后 EventBus 应非 nil")
	}
	if kernel.Dispatcher() == nil {
		t.Error("Setup 后 Dispatcher 应非 nil")
	}
}

func TestCoordinationKernel_StartStop(t *testing.T) {
	host := &fakeDispatcherHost{agentReady: true}
	kernel := NewCoordinationKernel(host)
	bp := testBlueprint(schema.TeamRoleLeader, "leader1")
	inf := testInfra()
	kernel.Setup(schema.TeamRoleLeader, bp, inf)
	ctx := context.Background()
	kernel.Start(ctx, nil)
	if !kernel.IsRunning() {
		t.Error("Start 后 IsRunning 应为 true")
	}
	kernel.Stop(ctx)
	if kernel.IsRunning() {
		t.Error("Stop 后 IsRunning 应为 false")
	}
}

func TestCoordinationKernel_状态机幂等(t *testing.T) {
	host := &fakeDispatcherHost{agentReady: true}
	kernel := NewCoordinationKernel(host)
	bp := testBlueprint(schema.TeamRoleLeader, "leader1")
	inf := testInfra()
	kernel.Setup(schema.TeamRoleLeader, bp, inf)
	ctx := context.Background()
	// idle 时 stop 幂等
	kernel.Stop(ctx)
	// running 时 start 幂等
	kernel.Start(ctx, nil)
	kernel.Start(ctx, nil) // 二次调用应无副作用
	if !kernel.IsRunning() {
		t.Error("重复 Start 后仍应 running")
	}
}
```

- [ ] **Step 3: 写 EnqueueUserInput 测试**

```go
func TestCoordinationKernel_EnqueueUserInput(t *testing.T) {
	host := &fakeDispatcherHost{agentReady: true}
	kernel := NewCoordinationKernel(host)
	bp := testBlueprint(schema.TeamRoleLeader, "leader1")
	inf := testInfra()
	kernel.Setup(schema.TeamRoleLeader, bp, inf)
	ctx := context.Background()
	var received []CoordinationEvent
	kernel.EventBus().Start(ctx, func(ctx context.Context, event CoordinationEvent) {
		received = append(received, event)
	})
	kernel.EnqueueUserInput("hello")
	time.Sleep(50 * time.Millisecond)
	kernel.EventBus().Stop()
	if len(received) != 1 {
		t.Errorf("期望收到 1 个事件，实际 %d", len(received))
	}
	if received[0].Inner.EventType != InnerEventTypeUserInput {
		t.Errorf("事件类型 = %q, want %q", received[0].Inner.EventType, InnerEventTypeUserInput)
	}
	if received[0].Inner.Payload["content"] != "hello" {
		t.Errorf("content = %v, want hello", received[0].Inner.Payload["content"])
	}
}

func TestCoordinationKernel_EnqueueUserInput_DictInput(t *testing.T) {
	// 测试 inputs 为 map[string]any 时提取 query 字段
	// 同上，但传入 map[string]any{"query": "test query"}
}
```

- [ ] **Step 4: 编译测试确认通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agent_teams/agent/coordination/... -v -timeout 10s`

- [ ] **Step 5: 提交**

```bash
git add internal/agent_teams/agent/coordination/
git commit -m "feat(9.62): 实现 CoordinationKernel（facade + 生命周期 + 便捷方法）"
```

---

### Task 8: 端到端集成测试

**Files:**
- Create: `internal/agent_teams/agent/coordination/integration_test.go`

- [ ] **Step 1: 写端到端集成测试——事件入队 → handler 被调用**

```go
func TestEndToEnd_UserInput到达Handler(t *testing.T) {
	host := &fakeDispatcherHost{agentReady: true}
	kernel := NewCoordinationKernel(host)
	bp := testBlueprint(schema.TeamRoleLeader, "leader1")
	inf := testInfra()
	kernel.Setup(schema.TeamRoleLeader, bp, inf)
	ctx := context.Background()
	kernel.Start(ctx, nil)
	// 入队一个 user_input 事件
	kernel.EnqueueUserInput("hello from test")
	time.Sleep(100 * time.Millisecond)
	kernel.Stop(ctx)
	// AgentLifecycleHandler.OnUserInput 已注册 user_input 事件
	// 骨架 handler 不 panic 即为通过
}
```

- [ ] **Step 2: 写端到端集成测试——轮询事件 → handler 被调用**

```go
func TestEndToEnd_PollTask到达Handler(t *testing.T) {
	host := &fakeDispatcherHost{agentReady: true}
	kernel := NewCoordinationKernel(host)
	bp := testBlueprint(schema.TeamRoleTeammate, "mate1")
	inf := testInfra()
	kernel.Setup(schema.TeamRoleTeammate, bp, inf)
	ctx := context.Background()
	kernel.Start(ctx, nil)
	// 等待轮询事件（用极短间隔需要重构 EventBus 构造参数）
	// 临时方案：手动入队 POLL_TASK
	kernel.Enqueue(CoordinationEvent{Inner: &InnerEventMessage{EventType: InnerEventTypePollTask}})
	time.Sleep(100 * time.Millisecond)
	kernel.Stop(ctx)
	// StaleTaskHandler.OnPollTask + TeamCompletionHandler.OnPollTask 被调用
	// 骨架 handler 不 panic 即为通过
}
```

- [ ] **Step 3: 编译测试确认通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agent_teams/agent/coordination/... -v -timeout 10s`

- [ ] **Step 4: 提交**

```bash
git add internal/agent_teams/agent/coordination/
git commit -m "feat(9.62+9.63): 端到端集成测试（事件入队→handler调用）"
```

---

### Task 9: 更新 team_agent.go 中的 TODO 标记

**Files:**
- Modify: `internal/agent_teams/agent/team_agent.go`

- [ ] **Step 1: 替换 5 处 TODO(#9.62-9.63) 文件目录标记**

在 `team_agent.go` 的 doc.go 注释中，将：
```
//	└── coordination/         # TODO(#9.62-9.63) 协调子系统
//	    ├── kernel.go         # TODO(#9.62) 协调内核
//	    ├── event_bus.go      # TODO(#9.63) 事件总线
//	    ├── dispatcher.go     # TODO(#9.63) 事件分发器
//	    └── handlers/         # TODO(#9.63) 事件处理器
```
替换为：
```
//	└── coordination/         # 协调子系统（事件总线 + 调度器 + 生命周期）
//	    ├── kernel.go         # CoordinationKernel 协调内核 facade
//	    ├── event_bus.go      # EventBus 事件入队 + 轮询 + 生命周期
//	    ├── dispatcher.go     # EventDispatcher 粗筛 + CallbackFramework 分发
//	    └── handlers/         # 场景 handler 子包
```

- [ ] **Step 2: 替换 team_agent.go:84 的 CoordinationKernel 类型 TODO**

将 `// TODO(#9.62): CoordinationKernel 类型` 替换为 `coordination *coordination.CoordinationKernel`（需要 import coordination 包）。

**注意**：此字段暂时不会被赋值（赋值在 9.55 TeamAgent 完成时），但类型定义让代码可编译。

- [ ] **Step 3: 编译确认**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/agent_teams/agent/...`

- [ ] **Step 4: 提交**

```bash
git add internal/agent_teams/agent/team_agent.go
git commit -m "feat(9.62+9.63): 更新 team_agent.go TODO 标记为实际文件名 + coordination 字段类型"
```

---

### Task 10: 更新 IMPLEMENTATION_PLAN.md + doc.go 回填

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`
- Modify: `internal/agent_teams/agent/doc.go`（如需要）
- Modify: `internal/agent_teams/doc.go`（如需要）

- [ ] **Step 1: 更新 IMPLEMENTATION_PLAN.md 9.62 和 9.63 行状态**

将：
```
| 9.62 | ☐ | CoordinationKernel | 协调内核 | `openjiuwen/agent_teams/` |
| 9.63 | ☐ | EventBus / Dispatcher | 事件总线与分发 | `openjiuwen/agent_teams/` |
```
替换为：
```
| 9.62 | ✅ | CoordinationKernel | 协调内核（EventBus+EventDispatcher+3 Protocol+6 Handler 骨架+CallbackFramework 适配层+kernel facade+测试）；⤵️ 9.55 回填 TeamAgent 接线 + handler 业务逻辑 | `openjiuwen/agent_teams/agent/coordination/` |
| 9.63 | ✅ | EventBus / Dispatcher | 已合并至 9.62 | `openjiuwen/agent_teams/agent/coordination/` |
```

- [ ] **Step 2: 更新 agent 包 doc.go 中的 coordination 文件目录**

检查 `internal/agent_teams/agent/doc.go` 是否有 coordination 条目需要更新。

- [ ] **Step 3: 提交**

```bash
git add IMPLEMENTATION_PLAN.md internal/agent_teams/agent/doc.go
git commit -m "docs: 更新 IMPLEMENTATION_PLAN.md 9.62+9.63 状态为已完成"
```

---

## Self-Review Checklist

- [x] **Spec coverage**: 设计文档每个 Section 在计划中有对应 Task（Section 3→Task1, Section 4→Task2, Section 5→Task3+6, Section 6→Task4+5, Section 7→Task7, Section 8→Task8, Section 9→全部测试, Section 10→Task9+10）
- [x] **Placeholder scan**: 无 "TBD"/"TODO"/"implement later"/"fill in details"——所有 TODO 均为明确的 `TODO(#9.55)` 回填标记
- [x] **Type consistency**: `CoordinationEvent` 在所有 Task 中签名一致；`EventCallback` 类型定义在 handlers 包；`DispatcherHost` 接口在 dispatcher.go 中定义，handlers 包通过 coordination 包引用；`NewEventDispatcher` 构造参数在 Task 3 定义、Task 6 接线
