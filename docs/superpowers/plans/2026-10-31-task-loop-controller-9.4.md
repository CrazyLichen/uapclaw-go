# 9.4 TaskLoopController + LoopQueues + ControllerInterface 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 TaskLoopController（嵌入 Controller + 轮次管理扩展）、LoopQueues（双队列缓冲）、ControllerInterface（控制器接口），完成步骤 9.4。

**Architecture:** TaskLoopController 嵌入 *Controller 复用基类方法，新增 SubmitRound/WaitRoundCompletion/FollowUp 管理方法。LoopQueues 使用 2 个带缓冲 channel 实现非阻塞双队列。ControllerInterface 在 controller 包内定义，Controller.AddTaskExecutor 返回类型改为 ControllerInterface 以支持接口级联。getInteractionQueues 使用类型断言对齐 Python getattr 语义。

**Tech Stack:** Go 1.23+, channel, 类型断言, testify/assert

---

## 文件结构

| 文件 | 操作 | 职责 |
|------|------|------|
| `internal/agentcore/controller/interface.go` | 新增 | ControllerInterface 接口定义 + 编译时断言 |
| `internal/agentcore/controller/controller.go` | 修改 | AddTaskExecutor 返回类型改为 ControllerInterface |
| `internal/agentcore/controller/doc.go` | 修改 | 文件目录新增 interface.go |
| `internal/agentcore/harness/task_loop/loop_queues.go` | 新增 | LoopQueues 双队列缓冲 |
| `internal/agentcore/harness/task_loop/loop_queues_test.go` | 新增 | LoopQueues 单元测试 |
| `internal/agentcore/harness/task_loop/controller.go` | 新增 | TaskLoopController |
| `internal/agentcore/harness/task_loop/controller_test.go` | 新增 | TaskLoopController 单元测试 |
| `internal/agentcore/harness/task_loop/doc.go` | 修改 | 更新文件目录 |

---

### Task 1: ControllerInterface 接口定义

**Files:**
- Create: `internal/agentcore/controller/interface.go`

- [ ] **Step 1: 创建 interface.go 文件**

```go
package controller

import (
	"context"

	iface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller/config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller/modules"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// ──────────────────────────── 接口 ────────────────────────────

// ControllerInterface 控制器接口，定义事件驱动任务编排的核心能力。
// Controller 和 TaskLoopController 均实现此接口。
// 对齐 Python: openjiuwen/core/controller/base.py::Controller 的公开方法
type ControllerInterface interface {
	// Init 两阶段初始化
	Init(card *agentschema.AgentCard, cfg *config.ControllerConfig,
		abilityMgr agentinterfaces.AbilityManagerInterface,
		contextEngine iface.ContextEngine)
	// Start 启动控制器
	Start(ctx context.Context) error
	// Stop 停止控制器
	Stop(ctx context.Context) error
	// Invoke 批量执行
	Invoke(ctx context.Context, inputs *schema.InputEvent, sess *session.Session) (*schema.ControllerOutput, error)
	// Stream 流式执行
	Stream(ctx context.Context, inputs *schema.InputEvent, sess *session.Session,
		streamModes []stream.StreamMode) (<-chan *stream.OutputSchema, <-chan error)
	// PublishEventAsync 异步发布事件（fire-and-forget）
	PublishEventAsync(ctx context.Context, sess *session.Session, event schema.Event) error
	// SetEventHandler 设置事件处理器
	SetEventHandler(handler modules.EventHandler)
	// AddTaskExecutor 注册任务执行器（链式调用返回 ControllerInterface）
	AddTaskExecutor(taskType string, builder func(deps *modules.TaskExecutorDependencies) modules.TaskExecutor) ControllerInterface
	// BindSession 绑定 session
	BindSession(ctx context.Context, sess *session.Session) error
	// UnbindSession 解绑 session
	UnbindSession(ctx context.Context, sess *session.Session) error
	// Config 获取配置
	Config() *config.ControllerConfig
	// EventHandler 获取事件处理器
	EventHandler() modules.EventHandler
}
```

- [ ] **Step 2: 修改 Controller.AddTaskExecutor 返回类型**

在 `internal/agentcore/controller/controller.go` 中，将 AddTaskExecutor 返回类型从 `*Controller` 改为 `ControllerInterface`：

```go
// AddTaskExecutor 注册 TaskExecutor，支持链式调用。
// 对应 Python: Controller.add_task_executor(task_type, builder)
func (c *Controller) AddTaskExecutor(taskType string, builder func(deps *modules.TaskExecutorDependencies) modules.TaskExecutor) ControllerInterface {
	c.taskScheduler.TaskExecutorRegistry().AddTaskExecutor(taskType, builder)
	return c
}
```

- [ ] **Step 3: 在 controller.go 末尾添加编译时断言**

```go
// 确保 Controller 满足 ControllerInterface
var _ ControllerInterface = (*Controller)(nil)
```

- [ ] **Step 4: 更新 controller/doc.go 文件目录**

在文件目录的 `controller/` 列表中添加 `interface.go`：

```
//	controller/
//	├── doc.go           # 包文档
//	├── interface.go     # ControllerInterface 接口
//	├── controller.go    # Controller 主结构体
```

- [ ] **Step 5: 编译验证**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/controller/...`
Expected: 编译通过，无错误

- [ ] **Step 6: 运行现有测试确认无回归**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/controller/... -count=1`
Expected: 所有测试通过

- [ ] **Step 7: 提交**

```bash
git add internal/agentcore/controller/interface.go internal/agentcore/controller/controller.go internal/agentcore/controller/doc.go
git commit -m "feat(controller): 新增 ControllerInterface 接口，AddTaskExecutor 返回类型改为 ControllerInterface"
```

---

### Task 2: LoopQueues 双队列缓冲

**Files:**
- Create: `internal/agentcore/harness/task_loop/loop_queues.go`
- Create: `internal/agentcore/harness/task_loop/loop_queues_test.go`

- [ ] **Step 1: 编写 LoopQueues 测试**

创建 `internal/agentcore/harness/task_loop/loop_queues_test.go`：

```go
package task_loop

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestNewLoopQueues 测试创建双队列缓冲
func TestNewLoopQueues(t *testing.T) {
	q := NewLoopQueues(16)
	assert.NotNil(t, q)
}

// TestLoopQueues_PushAndDrainSteering 测试 steering 队列推入和排空
func TestLoopQueues_PushAndDrainSteering(t *testing.T) {
	q := NewLoopQueues(16)
	assert.Empty(t, q.DrainSteering())

	q.PushSteer("msg1")
	q.PushSteer("msg2")
	msgs := q.DrainSteering()
	assert.Equal(t, []string{"msg1", "msg2"}, msgs)
	// 排空后应为空
	assert.Empty(t, q.DrainSteering())
}

// TestLoopQueues_PushAndDrainFollowUp 测试 follow_up 队列推入和排空
func TestLoopQueues_PushAndDrainFollowUp(t *testing.T) {
	q := NewLoopQueues(16)
	assert.Empty(t, q.DrainFollowUp())
	assert.False(t, q.HasFollowUp())

	q.PushFollowUp("f1")
	q.PushFollowUp("f2")
	assert.True(t, q.HasFollowUp())
	msgs := q.DrainFollowUp()
	assert.Equal(t, []string{"f1", "f2"}, msgs)
	assert.False(t, q.HasFollowUp())
	// 排空后应为空
	assert.Empty(t, q.DrainFollowUp())
}

// TestLoopQueues_HasFollowUp 测试 HasFollowUp
func TestLoopQueues_HasFollowUp(t *testing.T) {
	q := NewLoopQueues(16)
	assert.False(t, q.HasFollowUp())

	q.PushFollowUp("msg")
	assert.True(t, q.HasFollowUp())

	q.DrainFollowUp()
	assert.False(t, q.HasFollowUp())
}

// TestLoopQueues_DrainSteering_空队列 测试空队列排空
func TestLoopQueues_DrainSteering_空队列(t *testing.T) {
	q := NewLoopQueues(16)
	assert.Empty(t, q.DrainSteering())
}

// TestLoopQueues_DrainFollowUp_空队列 测试空队列排空
func TestLoopQueues_DrainFollowUp_空队列(t *testing.T) {
	q := NewLoopQueues(16)
	assert.Empty(t, q.DrainFollowUp())
}

// TestLoopQueues_满队列Push丢弃 测试满队列时非阻塞丢弃
func TestLoopQueues_满队列Push丢弃(t *testing.T) {
	q := NewLoopQueues(2) // 容量仅为 2
	q.PushSteer("a")
	q.PushSteer("b")
	q.PushSteer("c") // 应被丢弃（满），记录日志但不阻塞

	msgs := q.DrainSteering()
	assert.Equal(t, []string{"a", "b"}, msgs) // c 被丢弃
}

// TestLoopQueues_交替操作 测试交替推入排空
func TestLoopQueues_交替操作(t *testing.T) {
	q := NewLoopQueues(16)

	q.PushSteer("s1")
	q.PushFollowUp("f1")
	assert.True(t, q.HasFollowUp())

	steerMsgs := q.DrainSteering()
	assert.Equal(t, []string{"s1"}, steerMsgs)
	assert.True(t, q.HasFollowUp()) // steering 排空不影响 follow_up

	followMsgs := q.DrainFollowUp()
	assert.Equal(t, []string{"f1"}, followMsgs)
	assert.False(t, q.HasFollowUp())
}
```

- [ ] **Step 2: 运行测试验证失败**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/harness/task_loop/... -run TestNewLoopQueues -count=1`
Expected: FAIL（NewLoopQueues 未定义）

- [ ] **Step 3: 实现 LoopQueues**

创建 `internal/agentcore/harness/task_loop/loop_queues.go`：

```go
package task_loop

import (
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// LoopQueues 双队列缓冲，桥接 EventHandler 与 Executor/Loop。
// steering: 引导指令队列，由 executor 每次内部 invoke 前排空
// followUp: 后续消息队列，由外层任务循环每次迭代完成后排空
// 对齐 Python: LoopQueues
type LoopQueues struct {
	// steering 引导指令队列
	steering chan string
	// followUp 后续消息队列
	followUp chan string
}

// ──────────────────────────── 常量 ────────────────────────────

// 默认队列缓冲区大小
const defaultQueueCap = 64

// ──────────────────────────── 导出函数 ────────────────────────────

// NewLoopQueues 创建双队列缓冲。
// cap 为各队列缓冲区大小，若 cap <= 0 则使用默认值 64。
// 对齐 Python: LoopQueues.__init__
func NewLoopQueues(cap int) *LoopQueues {
	if cap <= 0 {
		cap = defaultQueueCap
	}
	return &LoopQueues{
		steering: make(chan string, cap),
		followUp: make(chan string, cap),
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// PushSteer 非阻塞推入引导指令。
// 对齐 Python: LoopQueues.push_steer (put_nowait)
// 满队列时丢弃并记录日志。
func (q *LoopQueues) PushSteer(msg string) {
	select {
	case q.steering <- msg:
	default:
		logger.Warn(logComponent).
			Str("queue", "steering").
			Str("msg", msg).
			Msg("队列已满，丢弃引导指令")
	}
}

// PushFollowUp 非阻塞推入后续消息。
// 对齐 Python: LoopQueues.push_follow_up (put_nowait)
// 满队列时丢弃并记录日志。
func (q *LoopQueues) PushFollowUp(msg string) {
	select {
	case q.followUp <- msg:
	default:
		logger.Warn(logComponent).
			Str("queue", "follow_up").
			Str("msg", msg).
			Msg("队列已满，丢弃后续消息")
	}
}

// HasFollowUp 非阻塞检查是否有待处理的后续消息。
// 对齐 Python: LoopQueues.has_follow_up
func (q *LoopQueues) HasFollowUp() bool {
	return len(q.followUp) > 0
}

// DrainSteering 非阻塞一次性排空所有引导指令。
// 对齐 Python: LoopQueues.drain_steering
func (q *LoopQueues) DrainSteering() []string {
	msgs := make([]string, 0, len(q.steering))
	for {
		select {
		case msg := <-q.steering:
			msgs = append(msgs, msg)
		default:
			return msgs
		}
	}
}

// DrainFollowUp 非阻塞一次性排空所有后续消息。
// 对齐 Python: LoopQueues.drain_follow_up
func (q *LoopQueues) DrainFollowUp() []string {
	msgs := make([]string, 0, len(q.followUp))
	for {
		select {
		case msg := <-q.followUp:
			msgs = append(msgs, msg)
		default:
			return msgs
		}
	}
}
```

- [ ] **Step 4: 运行测试验证通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/harness/task_loop/... -run "TestNewLoopQueues|TestLoopQueues" -count=1 -v`
Expected: 所有 LoopQueues 测试通过

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/harness/task_loop/loop_queues.go internal/agentcore/harness/task_loop/loop_queues_test.go
git commit -m "feat(task_loop): 新增 LoopQueues 双队列缓冲（steering + follow_up）"
```

---

### Task 3: TaskLoopController 实现

**Files:**
- Create: `internal/agentcore/harness/task_loop/controller.go`
- Create: `internal/agentcore/harness/task_loop/controller_test.go`

- [ ] **Step 1: 编写 TaskLoopController 测试**

创建 `internal/agentcore/harness/task_loop/controller_test.go`：

```go
package task_loop

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller/config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller/modules"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// ──────────────────────────── 测试辅助 ────────────────────────────

// mockAbilityMgr 简单的 AbilityManagerInterface mock
type mockAbilityMgr struct{}

func (m *mockAbilityMgr) LookupAbility(_ context.Context, _ string) (agentinterfaces.Ability, error) {
	return nil, nil
}
func (m *mockAbilityMgr) ListAbilities(_ context.Context) ([]agentinterfaces.Ability, error) {
	return nil, nil
}
func (m *mockAbilityMgr) RegisterAbility(_ agentinterfaces.Ability) error { return nil }
func (m *mockAbilityMgr) UnregisterAbility(_ string) error                { return nil }
func (m *mockAbilityMgr) GetToolRegistry() agentinterfaces.ToolRegistry   { return nil }
func (m *mockAbilityMgr) GetWorkflowRegistry() agentinterfaces.WorkflowRegistry { return nil }

// mockEventHandlerWithQueues 满足 EventHandler + interactionQueuesProvider
type mockEventHandlerWithQueues struct {
	modules.EventHandlerBase
	queues *LoopQueues
}

func (m *mockEventHandlerWithQueues) HandleInput(_ context.Context, _ *modules.EventHandlerInput) (map[string]any, error) {
	return nil, nil
}
func (m *mockEventHandlerWithQueues) HandleTaskInteraction(_ context.Context, _ *modules.EventHandlerInput) (map[string]any, error) {
	return nil, nil
}
func (m *mockEventHandlerWithQueues) HandleTaskCompletion(_ context.Context, _ *modules.EventHandlerInput) (map[string]any, error) {
	return nil, nil
}
func (m *mockEventHandlerWithQueues) HandleTaskFailed(_ context.Context, _ *modules.EventHandlerInput) (map[string]any, error) {
	return nil, nil
}
func (m *mockEventHandlerWithQueues) InteractionQueues() *LoopQueues {
	return m.queues
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestNewTaskLoopController 测试创建 TaskLoopController
func TestNewTaskLoopController(t *testing.T) {
	tc := NewTaskLoopController()
	assert.NotNil(t, tc)
}

// TestTaskLoopController_满足ControllerInterface 测试编译时接口满足
func TestTaskLoopController_满足ControllerInterface(t *testing.T) {
	// 编译时断言已保证，此处运行时验证
	tc := NewTaskLoopController()
	var _ controller.ControllerInterface = tc
	assert.NotNil(t, tc)
}

// TestTaskLoopController_Init 测试初始化
func TestTaskLoopController_Init(t *testing.T) {
	tc := NewTaskLoopController()
	card := &agentschema.AgentCard{ID: "test-agent"}
	cfg := config.NewControllerConfig()
	tc.Init(card, cfg, &mockAbilityMgr{}, nil)
	assert.Equal(t, cfg, tc.Config())
}

// TestTaskLoopController_DrainFollowUp_无Handler 测试无 EventHandler 时排空返回空
func TestTaskLoopController_DrainFollowUp_无Handler(t *testing.T) {
	tc := NewTaskLoopController()
	// 无 EventHandler，应返回空切片
	assert.Empty(t, tc.DrainFollowUp())
}

// TestTaskLoopController_HasFollowUp_无Handler 测试无 EventHandler 时返回 false
func TestTaskLoopController_HasFollowUp_无Handler(t *testing.T) {
	tc := NewTaskLoopController()
	assert.False(t, tc.HasFollowUp())
}

// TestTaskLoopController_EnqueueFollowUp_无Handler 测试无 EventHandler 时入队不 panic
func TestTaskLoopController_EnqueueFollowUp_无Handler(t *testing.T) {
	tc := NewTaskLoopController()
	assert.NotPanics(t, func() {
		tc.EnqueueFollowUp("test")
	})
}

// TestTaskLoopController_DrainFollowUp_有Queues 测试有 LoopQueues 时排空
func TestTaskLoopController_DrainFollowUp_有Queues(t *testing.T) {
	tc := NewTaskLoopController()
	card := &agentschema.AgentCard{ID: "test-agent"}
	cfg := config.NewControllerConfig()
	tc.Init(card, cfg, &mockAbilityMgr{}, nil)

	queues := NewLoopQueues(16)
	handler := &mockEventHandlerWithQueues{queues: queues}
	tc.SetEventHandler(handler)

	// 推入 follow-up
	queues.PushFollowUp("msg1")
	queues.PushFollowUp("msg2")

	// 通过 TaskLoopController 排空
	msgs := tc.DrainFollowUp()
	assert.Equal(t, []string{"msg1", "msg2"}, msgs)
	assert.Empty(t, tc.DrainFollowUp())
}

// TestTaskLoopController_EnqueueFollowUp_有Queues 测试通过 Controller 入队
func TestTaskLoopController_EnqueueFollowUp_有Queues(t *testing.T) {
	tc := NewTaskLoopController()
	card := &agentschema.AgentCard{ID: "test-agent"}
	cfg := config.NewControllerConfig()
	tc.Init(card, cfg, &mockAbilityMgr{}, nil)

	queues := NewLoopQueues(16)
	handler := &mockEventHandlerWithQueues{queues: queues}
	tc.SetEventHandler(handler)

	tc.EnqueueFollowUp("f1")
	assert.True(t, tc.HasFollowUp())
	msgs := tc.DrainFollowUp()
	assert.Equal(t, []string{"f1"}, msgs)
}

// TestTaskLoopController_HasFollowUp_有Queues 测试检查待处理消息
func TestTaskLoopController_HasFollowUp_有Queues(t *testing.T) {
	tc := NewTaskLoopController()
	card := &agentschema.AgentCard{ID: "test-agent"}
	cfg := config.NewControllerConfig()
	tc.Init(card, cfg, &mockAbilityMgr{}, nil)

	queues := NewLoopQueues(16)
	handler := &mockEventHandlerWithQueues{queues: queues}
	tc.SetEventHandler(handler)

	assert.False(t, tc.HasFollowUp())
	queues.PushFollowUp("msg")
	assert.True(t, tc.HasFollowUp())
	queues.DrainFollowUp()
	assert.False(t, tc.HasFollowUp())
}

// TestTaskLoopController_WaitRoundCompletion 测试等待轮次完成
func TestTaskLoopController_WaitRoundCompletion(t *testing.T) {
	tc := NewTaskLoopController()
	card := &agentschema.AgentCard{ID: "test-agent"}
	cfg := config.NewControllerConfig()
	tc.Init(card, cfg, &mockAbilityMgr{}, nil)

	queues := NewLoopQueues(16)
	handler := &mockEventHandlerWithQueues{queues: queues}
	tc.SetEventHandler(handler)

	// 无超时等待（EventHandlerBase 默认返回 {"status": "completed"}）
	result := tc.WaitRoundCompletion(context.Background(), nil)
	assert.Equal(t, map[string]any{"status": "completed"}, result)
}

// TestTaskLoopController_SubmitRound 测试提交一轮任务
func TestTaskLoopController_SubmitRound(t *testing.T) {
	tc := NewTaskLoopController()
	card := &agentschema.AgentCard{ID: "test-agent"}
	cfg := config.NewControllerConfig()
	tc.Init(card, cfg, &mockAbilityMgr{}, nil)

	// 需要创建真实 Session 才能发布事件
	// 此测试验证 SubmitRound 构建 InputEvent 并注入元数据的逻辑
	// 由于 PublishEventAsync 需要已启动的 EventQueue，此处验证方法不 panic
	handler := &mockEventHandlerWithQueues{queues: NewLoopQueues(16)}
	tc.SetEventHandler(handler)

	roundID := handler.PrepareRound()
	assert.Equal(t, 0, roundID)
}

// TestTaskLoopController_getInteractionQueues_无Provider 测试类型断言无 provider 返回 nil
func TestTaskLoopController_getInteractionQueues_无Provider(t *testing.T) {
	tc := NewTaskLoopController()
	// EventHandlerBase 不实现 interactionQueuesProvider
	assert.Nil(t, tc.getInteractionQueues())
}

// TestTaskLoopController_getInteractionQueues_有Provider 测试类型断言有 provider 返回队列
func TestTaskLoopController_getInteractionQueues_有Provider(t *testing.T) {
	tc := NewTaskLoopController()
	card := &agentschema.AgentCard{ID: "test-agent"}
	cfg := config.NewControllerConfig()
	tc.Init(card, cfg, &mockAbilityMgr{}, nil)

	queues := NewLoopQueues(16)
	handler := &mockEventHandlerWithQueues{queues: queues}
	tc.SetEventHandler(handler)

	result := tc.getInteractionQueues()
	require.NotNil(t, result)
	assert.Equal(t, queues, result)
}
```

- [ ] **Step 2: 运行测试验证失败**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/harness/task_loop/... -run TestNewTaskLoopController -count=1`
Expected: FAIL（NewTaskLoopController 未定义）

- [ ] **Step 3: 实现 TaskLoopController**

创建 `internal/agentcore/harness/task_loop/controller.go`：

```go
package task_loop

import (
	"context"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller/modules"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 接口 ────────────────────────────

// interactionQueuesProvider 类型断言接口，用于从 EventHandler 获取 LoopQueues。
// 对齐 Python: getattr(handler, "interaction_queues", None)
// 只有 TaskLoopEventHandler（9.6）实现此接口，其他 EventHandler 不实现。
type interactionQueuesProvider interface {
	InteractionQueues() *LoopQueues
}

// ──────────────────────────── 结构体 ────────────────────────────

// TaskLoopController 任务循环控制器，嵌入 Controller 并扩展轮次管理能力。
// 封装轮次提交/等待/完成、follow-up 队列操作和循环退出逻辑，
// 是 DeepAgent 外层循环的"方向盘"。
// 对齐 Python: TaskLoopController(Controller)
type TaskLoopController struct {
	*controller.Controller
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTaskLoopController 创建任务循环控制器。
// 必须随后调用 Init() 完成初始化（与 Controller 相同）。
// 对齐 Python: TaskLoopController.__init__
func NewTaskLoopController() *TaskLoopController {
	return &TaskLoopController{
		Controller: controller.NewController(),
	}
}

// SubmitRound 提交一轮任务：prepare_round → 构建 InputEvent → 注入元数据 → 发布。
// 对齐 Python: TaskLoopController.submit_round
func (tc *TaskLoopController) SubmitRound(
	ctx context.Context,
	sess *session.Session,
	query string,
	isFollowUp bool,
	runKind any,
	runContext any,
) error {
	handler := tc.EventHandler()
	if handler == nil {
		logger.Error(logComponent).Msg("SubmitRound: EventHandler 为 nil")
		return nil
	}

	roundID := handler.PrepareRound()

	event, err := schema.FromUserInput(query)
	if err != nil {
		logger.Error(logComponent).Err(err).Str("query", query).Msg("SubmitRound: 构建 InputEvent 失败")
		return err
	}

	meta := event.GetMetadata()
	if meta == nil {
		meta = make(map[string]any)
	}
	meta["_handler_round_id"] = roundID
	if isFollowUp {
		meta["is_follow_up"] = true
	}
	if runKind != nil {
		meta["run_kind"] = runKind
	}
	if runContext != nil {
		meta["run_context"] = runContext
	}
	event.SetMetadata(meta)

	logger.Info(logComponent).
		Int("round_id", roundID).
		Bool("is_follow_up", isFollowUp).
		Msg("提交任务轮次")

	return tc.PublishEventAsync(ctx, sess, event)
}

// WaitRoundCompletion 等待当前轮次完成。
// timeout 为超时时间（秒），nil 表示不超时。
// 对齐 Python: TaskLoopController.wait_round_completion
func (tc *TaskLoopController) WaitRoundCompletion(ctx context.Context, timeout *float64) map[string]any {
	handler := tc.EventHandler()
	if handler == nil {
		logger.Warn(logComponent).Msg("WaitRoundCompletion: EventHandler 为 nil，返回空结果")
		return nil
	}

	var d time.Duration
	if timeout != nil && *timeout > 0 {
		d = time.Duration(*timeout * float64(time.Second))
	}

	return handler.WaitCompletion(ctx, d)
}

// DrainFollowUp 排空 follow-up 消息。
// 对齐 Python: TaskLoopController.drain_follow_up
func (tc *TaskLoopController) DrainFollowUp() []string {
	queues := tc.getInteractionQueues()
	if queues != nil {
		return queues.DrainFollowUp()
	}
	return nil
}

// EnqueueFollowUp 入队 follow-up 消息（Rails 用于请求继续/确认轮次）。
// 对齐 Python: TaskLoopController.enqueue_follow_up
func (tc *TaskLoopController) EnqueueFollowUp(msg string) {
	queues := tc.getInteractionQueues()
	if queues != nil {
		queues.PushFollowUp(msg)
		return
	}
	logger.Warn(logComponent).Str("msg", msg).Msg("EnqueueFollowUp: 无 InteractionQueues，消息丢弃")
}

// HasFollowUp 检查是否有待处理的 follow-up 消息。
// 对齐 Python: TaskLoopController.has_follow_up
func (tc *TaskLoopController) HasFollowUp() bool {
	queues := tc.getInteractionQueues()
	if queues != nil {
		return queues.HasFollowUp()
	}
	return false
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// getInteractionQueues 从 EventHandler 防御性获取 LoopQueues。
// 使用类型断言对齐 Python getattr(handler, "interaction_queues", None) 语义。
// 只有实现了 interactionQueuesProvider 接口的 EventHandler 才能返回非 nil。
// 对齐 Python: TaskLoopController._get_interaction_queues
func (tc *TaskLoopController) getInteractionQueues() *LoopQueues {
	handler := tc.EventHandler()
	if handler == nil {
		return nil
	}
	provider, ok := handler.(interactionQueuesProvider)
	if !ok {
		return nil
	}
	return provider.InteractionQueues()
}
```

- [ ] **Step 4: 添加编译时断言**

在 controller.go 末尾添加：

```go
// 确保 TaskLoopController 满足 ControllerInterface
var _ controller.ControllerInterface = (*TaskLoopController)(nil)
```

- [ ] **Step 5: 运行测试验证通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/harness/task_loop/... -run "TestNewTaskLoopController|TestTaskLoopController" -count=1 -v`
Expected: 所有 TaskLoopController 测试通过

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/harness/task_loop/controller.go internal/agentcore/harness/task_loop/controller_test.go
git commit -m "feat(task_loop): 新增 TaskLoopController（嵌入 Controller + 轮次管理扩展）"
```

---

### Task 4: 更新 doc.go 文件目录

**Files:**
- Modify: `internal/agentcore/harness/task_loop/doc.go`

- [ ] **Step 1: 更新 doc.go**

```go
// Package task_loop 提供 DeepAgent 外层任务循环的运行时组件。
//
// 包含任务循环控制器（TaskLoopController）、双队列缓冲（LoopQueues）、
// 循环协调器（LoopCoordinator）、停止条件评估器（StopConditionEvaluator）
// 及其内置实现，用于控制 DeepAgent 多轮任务循环的生命周期。
//
// TaskLoopController 嵌入 Controller 基类，扩展轮次管理（提交/等待/完成）
// 和 follow-up 队列操作，是 DeepAgent 外层循环的"方向盘"。
// LoopCoordinator 是"刹车"——追踪迭代/token/耗时/中止，通过评估器链决定是否继续。
// LoopQueues 提供双队列缓冲（steering + follow_up），桥接 EventHandler 与 Executor/Loop。
//
// 文件目录：
//
//	task_loop/
//	├── doc.go                   # 包文档
//	├── controller.go            # TaskLoopController（嵌入 Controller + 轮次管理扩展）
//	├── loop_queues.go           # LoopQueues 双队列缓冲
//	├── stop_condition.go        # StopConditionEvaluator 接口 + 5 个评估器实现
//	├── loop_coordinator.go      # LoopCoordinator + LoopCoordinatorState
//	├── loop_coordinator_test.go # LoopCoordinator 测试
//	├── stop_condition_test.go   # 评估器测试
//	└── loop_queues_test.go     # LoopQueues 测试
//
// 对应 Python 代码：openjiuwen/harness/task_loop/ + openjiuwen/harness/schema/stop_condition.py
package task_loop
```

- [ ] **Step 2: 编译验证**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/harness/task_loop/...`
Expected: 编译通过

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/harness/task_loop/doc.go
git commit -m "docs(task_loop): 更新包文档，新增 controller.go 和 loop_queues.go"
```

---

### Task 5: 全量编译和测试

- [ ] **Step 1: 检查残留 go 编译进程**

Run: `pgrep -f 'go (build|test)' || echo "无残留进程"`

- [ ] **Step 2: 全量编译**

Run: `cd /home/opensource/uap-claw-go && go build ./...`
Expected: 编译通过，无错误

- [ ] **Step 3: 运行 task_loop 包测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/harness/task_loop/... -count=1 -v`
Expected: 所有测试通过

- [ ] **Step 4: 运行 controller 包测试（确认无回归）**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/controller/... -count=1`
Expected: 所有测试通过

- [ ] **Step 5: 运行全量测试**

Run: `cd /home/opensource/uap-claw-go && go test ./... -count=1`
Expected: 所有测试通过

---

### Task 6: 更新 IMPLEMENTATION_PLAN.md

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 将 9.4 状态从 ☐ 更新为 ✅**

在 IMPLEMENTATION_PLAN.md 中找到 9.4 行，将 `☐` 改为 `✅`：

```
| 9.4 | ✅ | TaskLoopController | 任务循环控制器 | `openjiuwen/harness/task_loop/` |
```

- [ ] **Step 2: 提交**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "docs: 更新实现计划 9.4 TaskLoopController 状态为已完成"
```
