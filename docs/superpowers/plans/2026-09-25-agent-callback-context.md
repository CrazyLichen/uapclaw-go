# 6.5 AgentCallbackContext 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 AgentCallbackContext 结构体及其方法，作为 Rail 系统与 Agent 运行时之间的核心中介对象

**Architecture:** 在 `single_agent/rail/` 包下新增 `context.go`（AgentCallbackContext 结构体 + steering/lifecycle 方法 + 预留 panic 占位方法）和 `inputs.go`（EventInputs 接口 + 4 个 Inputs 骨架）。steering 用超大 buffered chan (4096)，lifecycle 用 FireLifecycle 函数+defer，fire/retry/force_finish 方法体 panic 留给 6.6/6.10 回填。

**Tech Stack:** Go 1.22+, testify/assert, 项目内 logger/ce_interface/session/interfaces 包

---

## 文件结构

| 操作 | 文件 | 职责 |
|------|------|------|
| 新建 | `internal/agentcore/single_agent/rail/inputs.go` | EventInputs 接口 + 4 个 Inputs struct 骨架 |
| 新建 | `internal/agentcore/single_agent/rail/inputs_test.go` | EventInputs 接口测试 |
| 新建 | `internal/agentcore/single_agent/rail/context.go` | AgentCallbackContext 结构体 + 全部方法 |
| 新建 | `internal/agentcore/single_agent/rail/context_test.go` | AgentCallbackContext 全部测试 |
| 修改 | `internal/agentcore/single_agent/rail/doc.go` | 更新文件目录 + 核心类型索引 |
| 修改 | `internal/agentcore/single_agent/ability/ability_types.go:54-67` | ToolCallContext 增加 callbackCtx 字段（⤵️ 6.5 回填） |

---

### Task 1: EventInputs 接口 + 4 个 Inputs struct

**Files:**
- Create: `internal/agentcore/single_agent/rail/inputs.go`
- Test: `internal/agentcore/single_agent/rail/inputs_test.go`

- [ ] **Step 1: 写 inputs.go**

```go
package rail

// ──────────────────────────── 接口 ────────────────────────────

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

// ──────────────────────────── 结构体 ────────────────────────────

// InvokeInputs BEFORE/AFTER_INVOKE 事件输入。
// ⤵️ 6.9 回填字段
type InvokeInputs struct{}

// ModelCallInputs BEFORE/AFTER_MODEL_CALL 事件输入。
// ⤵️ 6.9 回填字段
type ModelCallInputs struct{}

// ToolCallInputs BEFORE/AFTER_TOOL_CALL 事件输入。
// ⤵️ 6.9 回填字段
type ToolCallInputs struct{}

// TaskIterationInputs BEFORE/AFTER_TASK_ITERATION 事件输入。
// ⤵️ 6.9 回填字段
type TaskIterationInputs struct{}

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// EventKind 实现 EventInputs 接口
func (i *InvokeInputs) EventKind() string { return "invoke" }

func (i *ModelCallInputs) EventKind() string { return "model_call" }

func (i *ToolCallInputs) EventKind() string { return "tool_call" }

func (i *TaskIterationInputs) EventKind() string { return "task_iteration" }
```

- [ ] **Step 2: 写 inputs_test.go**

```go
package rail

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestInvokeInputs_EventKind 验证 InvokeInputs.EventKind() 返回 "invoke"
func TestInvokeInputs_EventKind(t *testing.T) {
	assert.Equal(t, "invoke", (&InvokeInputs{}).EventKind())
}

// TestModelCallInputs_EventKind 验证 ModelCallInputs.EventKind() 返回 "model_call"
func TestModelCallInputs_EventKind(t *testing.T) {
	assert.Equal(t, "model_call", (&ModelCallInputs{}).EventKind())
}

// TestToolCallInputs_EventKind 验证 ToolCallInputs.EventKind() 返回 "tool_call"
func TestToolCallInputs_EventKind(t *testing.T) {
	assert.Equal(t, "tool_call", (&ToolCallInputs{}).EventKind())
}

// TestTaskIterationInputs_EventKind 验证 TaskIterationInputs.EventKind() 返回 "task_iteration"
func TestTaskIterationInputs_EventKind(t *testing.T) {
	assert.Equal(t, "task_iteration", (&TaskIterationInputs{}).EventKind())
}

// TestEventInputs_接口满足 验证各 Inputs struct 编译期满足 EventInputs 接口
func TestEventInputs_接口满足(t *testing.T) {
	var _ EventInputs = (*InvokeInputs)(nil)
	var _ EventInputs = (*ModelCallInputs)(nil)
	var _ EventInputs = (*ToolCallInputs)(nil)
	var _ EventInputs = (*TaskIterationInputs)(nil)
}
```

- [ ] **Step 3: 运行测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/single_agent/rail/... -run "Test(Invoke|ModelCall|ToolCall|TaskIteration)Inputs_EventKind|TestEventInputs_接口满足" -v`

Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/single_agent/rail/inputs.go internal/agentcore/single_agent/rail/inputs_test.go
git commit -m "feat(rail): 添加 EventInputs 接口和 4 个 Inputs struct 骨架 (6.5)"
```

---

### Task 2: AgentCallbackContext 结构体 + 构造函数 + getter/setter

**Files:**
- Create: `internal/agentcore/single_agent/rail/context.go`
- Test: `internal/agentcore/single_agent/rail/context_test.go`

- [ ] **Step 1: 写 context.go 结构体和构造函数部分**

```go
package rail

import (
	ceinterface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
)

// ──────────────────────────── 结构体 ────────────────────────────

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
	modelContext ceinterface.ModelContext
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

// ──────────────────────────── 常量 ────────────────────────────

const (
	// steeringQueueSize steering 队列缓冲区大小
	// Python 用无界 asyncio.Queue，Go 用大容量 buffered chan 对齐
	steeringQueueSize = 4096
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewAgentCallbackContext 创建 AgentCallbackContext 实例。
//
// 对应 Python: AgentCallbackContext(agent=..., inputs=..., session=...)
func NewAgentCallbackContext(
	agent interfaces.BaseAgent,
	inputs EventInputs,
	sess *session.Session,
) *AgentCallbackContext {
	return &AgentCallbackContext{
		agent:  agent,
		inputs: inputs,
		session: sess,
		extra:  make(map[string]any),
	}
}
```

- [ ] **Step 2: 写 context.go getter/setter 方法部分**

```go
// Agent 返回当前 Agent 实例引用
func (c *AgentCallbackContext) Agent() interfaces.BaseAgent { return c.agent }

// Event 返回当前回调事件类型
func (c *AgentCallbackContext) Event() AgentCallbackEvent { return c.event }

// SetEvent 设置当前回调事件类型
func (c *AgentCallbackContext) SetEvent(event AgentCallbackEvent) { c.event = event }

// Inputs 返回当前事件输入数据
func (c *AgentCallbackContext) Inputs() EventInputs { return c.inputs }

// SetInputs 设置当前事件输入数据
func (c *AgentCallbackContext) SetInputs(inputs EventInputs) { c.inputs = inputs }

// Config 返回运行时配置
func (c *AgentCallbackContext) Config() interfaces.AgentConfig { return c.config }

// SetConfig 设置运行时配置
func (c *AgentCallbackContext) SetConfig(config interfaces.AgentConfig) { c.config = config }

// Session 返回当前 Session
func (c *AgentCallbackContext) Session() *session.Session { return c.session }

// ModelContext 返回当前 ModelContext
func (c *AgentCallbackContext) ModelContext() ceinterface.ModelContext { return c.modelContext }

// SetModelContext 设置当前 ModelContext
func (c *AgentCallbackContext) SetModelContext(mc ceinterface.ModelContext) { c.modelContext = mc }

// Extra 返回 extra 通信字典
func (c *AgentCallbackContext) Extra() map[string]any { return c.extra }

// Exception 返回异常对象
func (c *AgentCallbackContext) Exception() error { return c.exception }

// SetException 设置异常对象
func (c *AgentCallbackContext) SetException(err error) { c.exception = err }

// RetryAttempt 返回当前重试索引号
func (c *AgentCallbackContext) RetryAttempt() int { return c.retryAttempt }

// SetRetryAttempt 设置当前重试索引号
func (c *AgentCallbackContext) SetRetryAttempt(attempt int) { c.retryAttempt = attempt }
```

- [ ] **Step 3: 写 context_test.go 构造函数和 getter/setter 测试**

```go
package rail

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestNewAgentCallbackContext 验证构造函数字段初始化
func TestNewAgentCallbackContext(t *testing.T) {
	ctx := NewAgentCallbackContext(nil, nil, nil)
	assert.NotNil(t, ctx)
	assert.Nil(t, ctx.Agent())
	assert.Nil(t, ctx.Inputs())
	assert.Nil(t, ctx.Session())
	assert.NotNil(t, ctx.Extra())
	assert.Empty(t, ctx.Extra())
}

// TestAgentCallbackContext_GetterSetter 验证各 getter/setter 方法
func TestAgentCallbackContext_GetterSetter(t *testing.T) {
	ctx := NewAgentCallbackContext(nil, &InvokeInputs{}, nil)

	// Event
	ctx.SetEvent(CallbackBeforeInvoke)
	assert.Equal(t, CallbackBeforeInvoke, ctx.Event())

	// Inputs
	inputs := &ModelCallInputs{}
	ctx.SetInputs(inputs)
	assert.Equal(t, inputs, ctx.Inputs())

	// Exception
	err := assert.AnError
	ctx.SetException(err)
	assert.Equal(t, err, ctx.Exception())

	// RetryAttempt
	ctx.SetRetryAttempt(3)
	assert.Equal(t, 3, ctx.RetryAttempt())

	// Extra 可以直接修改
	ctx.Extra()["key"] = "value"
	assert.Equal(t, "value", ctx.Extra()["key"])
}
```

- [ ] **Step 4: 运行测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/single_agent/rail/... -run "TestNewAgentCallbackContext|TestAgentCallbackContext_GetterSetter" -v`

Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/single_agent/rail/context.go internal/agentcore/single_agent/rail/context_test.go
git commit -m "feat(rail): 添加 AgentCallbackContext 结构体、构造函数和 getter/setter (6.5)"
```

---

### Task 3: Steering 方法实现

**Files:**
- Modify: `internal/agentcore/single_agent/rail/context.go` — 追加 steering 方法
- Modify: `internal/agentcore/single_agent/rail/context_test.go` — 追加 steering 测试

- [ ] **Step 1: 在 context.go 中追加 steering 方法（在导出函数区块尾部）**

```go
// BindSteeringQueue 绑定外部 steering 队列。
//
// 对应 Python: AgentCallbackContext.bind_steering_queue(queue)
func (c *AgentCallbackContext) BindSteeringQueue(q chan string) {
	c.steeringQueue = q
}

// PushSteering 非阻塞推送 steering 消息。
//
// 无队列时 no-op，队列满时 warn 日志丢弃。
// 对应 Python: AgentCallbackContext.push_steering(msg)
func (c *AgentCallbackContext) PushSteering(msg string) {
	if c.steeringQueue == nil {
		return
	}
	select {
	case c.steeringQueue <- msg:
	default:
		// 队列满，warn 日志丢弃
		logger.Warn(logComponent).
			Str("event_type", "steering_queue_full").
			Str("msg", msg).
			Msg("steering 队列已满，消息丢弃")
	}
}

// DrainSteering 非阻塞排空所有待处理 steering 消息。
//
// 对应 Python: AgentCallbackContext.drain_steering() -> List[str]
func (c *AgentCallbackContext) DrainSteering() []string {
	if c.steeringQueue == nil {
		return nil
	}
	var msgs []string
	for {
		select {
		case msg := <-c.steeringQueue:
			msgs = append(msgs, msg)
		default:
			return msgs
		}
	}
}

// HasPendingSteering 检查是否有待处理的 steering 消息。
//
// 对应 Python: AgentCallbackContext.has_pending_steering() -> bool
func (c *AgentCallbackContext) HasPendingSteering() bool {
	if c.steeringQueue == nil {
		return false
	}
	return len(c.steeringQueue) > 0
}

// SteeringQueue 返回绑定的 steering 队列。
//
// 对应 Python: AgentCallbackContext.steering_queue 属性
func (c *AgentCallbackContext) SteeringQueue() chan string {
	return c.steeringQueue
}
```

注意：需要在 context.go 顶部 import 区追加 logger 相关导入：

```go
import (
	ceinterface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)
```

同时在常量区块追加：

```go
const (
	// logComponent 日志组件标识
	logComponent = logger.ComponentAgentCore
	// steeringQueueSize steering 队列缓冲区大小
	// Python 用无界 asyncio.Queue，Go 用大容量 buffered chan 对齐
	steeringQueueSize = 4096
)
```

- [ ] **Step 2: 在 context_test.go 中追加 steering 测试**

```go
// TestPushSteering_无队列 验证无队列时 PushSteering 为 no-op
func TestPushSteering_无队列(t *testing.T) {
	ctx := NewAgentCallbackContext(nil, nil, nil)
	// 不应 panic
	ctx.PushSteering("test")
}

// TestPushSteering_正常写入 验证正常写入后可 DrainSteering 读出
func TestPushSteering_正常写入(t *testing.T) {
	ctx := NewAgentCallbackContext(nil, nil, nil)
	q := make(chan string, steeringQueueSize)
	ctx.BindSteeringQueue(q)

	ctx.PushSteering("msg1")
	ctx.PushSteering("msg2")

	msgs := ctx.DrainSteering()
	assert.Equal(t, []string{"msg1", "msg2"}, msgs)
}

// TestPushSteering_队列满丢弃 验证队列满时 PushSteering 不阻塞
func TestPushSteering_队列满丢弃(t *testing.T) {
	q := make(chan string, 2)
	ctx := NewAgentCallbackContext(nil, nil, nil)
	ctx.BindSteeringQueue(q)

	ctx.PushSteering("a")
	ctx.PushSteering("b")
	// 队列已满（容量 2），再写应 no-op 不阻塞
	ctx.PushSteering("c")

	msgs := ctx.DrainSteering()
	assert.Equal(t, []string{"a", "b"}, msgs)
}

// TestDrainSteering_无队列 验证无队列时返回 nil
func TestDrainSteering_无队列(t *testing.T) {
	ctx := NewAgentCallbackContext(nil, nil, nil)
	assert.Nil(t, ctx.DrainSteering())
}

// TestDrainSteering_空队列 验证空队列返回 nil
func TestDrainSteering_空队列(t *testing.T) {
	ctx := NewAgentCallbackContext(nil, nil, nil)
	ctx.BindSteeringQueue(make(chan string, steeringQueueSize))
	assert.Nil(t, ctx.DrainSteering())
}

// TestHasPendingSteering 验证各种状态下 HasPendingSteering
func TestHasPendingSteering(t *testing.T) {
	ctx := NewAgentCallbackContext(nil, nil, nil)

	// 无队列
	assert.False(t, ctx.HasPendingSteering())

	// 空队列
	q := make(chan string, steeringQueueSize)
	ctx.BindSteeringQueue(q)
	assert.False(t, ctx.HasPendingSteering())

	// 有消息
	ctx.PushSteering("msg")
	assert.True(t, ctx.HasPendingSteering())

	// drain 后
	ctx.DrainSteering()
	assert.False(t, ctx.HasPendingSteering())
}

// TestBindSteeringQueue 验证绑定后 push/drain 可用
func TestBindSteeringQueue(t *testing.T) {
	ctx := NewAgentCallbackContext(nil, nil, nil)
	q := make(chan string, steeringQueueSize)
	ctx.BindSteeringQueue(q)
	assert.Equal(t, q, ctx.SteeringQueue())

	ctx.PushSteering("test")
	msgs := ctx.DrainSteering()
	assert.Equal(t, []string{"test"}, msgs)
}

// TestSteeringQueue 验证 SteeringQueue 返回绑定的队列
func TestSteeringQueue(t *testing.T) {
	ctx := NewAgentCallbackContext(nil, nil, nil)
	assert.Nil(t, ctx.SteeringQueue())

	q := make(chan string, steeringQueueSize)
	ctx.BindSteeringQueue(q)
	assert.Equal(t, q, ctx.SteeringQueue())
}
```

- [ ] **Step 3: 运行测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/single_agent/rail/... -run "Test(Push|Drain|Has|Bind)Steering|TestSteeringQueue" -v`

Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/single_agent/rail/context.go internal/agentcore/single_agent/rail/context_test.go
git commit -m "feat(rail): 实现 AgentCallbackContext steering 方法 (6.5)"
```

---

### Task 4: FireLifecycle 方法实现

**Files:**
- Modify: `internal/agentcore/single_agent/rail/context.go` — 追加 FireLifecycle
- Modify: `internal/agentcore/single_agent/rail/context_test.go` — 追加 FireLifecycle 测试

- [ ] **Step 1: 在 context.go 中追加 FireLifecycle 方法**

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
//   - fn() 出错时设置 ctx.exception
//   - after 回调出错时：如有原始异常则 log 不掩盖，否则 re-raise
func (c *AgentCallbackContext) FireLifecycle(
	before, after AgentCallbackEvent,
	fn func() error,
) error {
	savedInputs := c.inputs

	// 2. fire(before)
	// ⤵️ 6.6 回填：if err := c.Fire(before); err != nil { ... }
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

- [ ] **Step 2: 在 context_test.go 中追加 FireLifecycle 测试**

```go
// TestFireLifecycle_正常流程 验证 before → fn → after，inputs 恢复
func TestFireLifecycle_正常流程(t *testing.T) {
	origInputs := &InvokeInputs{}
	ctx := NewAgentCallbackContext(nil, origInputs, nil)

	executed := false
	err := ctx.FireLifecycle(CallbackBeforeInvoke, CallbackAfterInvoke, func() error {
		executed = true
		// fn 内修改 inputs
		ctx.SetInputs(&ModelCallInputs{})
		return nil
	})

	assert.NoError(t, err)
	assert.True(t, executed)
	// inputs 应恢复为原始值
	assert.Equal(t, origInputs, ctx.Inputs())
}

// TestFireLifecycle_异常时设置Exception 验证 fn 出错时 exception 被设置
func TestFireLifecycle_异常时设置Exception(t *testing.T) {
	ctx := NewAgentCallbackContext(nil, &InvokeInputs{}, nil)

	testErr := assert.AnError
	err := ctx.FireLifecycle(CallbackBeforeModelCall, CallbackAfterModelCall, func() error {
		return testErr
	})

	assert.Equal(t, testErr, err)
	assert.Equal(t, testErr, ctx.Exception())
}

// TestFireLifecycle_恢复Inputs 验证 fn 内修改 inputs 后，after 时恢复
func TestFireLifecycle_恢复Inputs(t *testing.T) {
	origInputs := &InvokeInputs{}
	ctx := NewAgentCallbackContext(nil, origInputs, nil)

	_ = ctx.FireLifecycle(CallbackBeforeToolCall, CallbackAfterToolCall, func() error {
		ctx.SetInputs(&ToolCallInputs{})
		assert.IsType(t, &ToolCallInputs{}, ctx.Inputs()) // fn 内 inputs 已变
		return nil
	})

	assert.Equal(t, origInputs, ctx.Inputs()) // fn 后 inputs 恢复
}
```

- [ ] **Step 3: 运行测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/single_agent/rail/... -run "TestFireLifecycle" -v`

Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/single_agent/rail/context.go internal/agentcore/single_agent/rail/context_test.go
git commit -m "feat(rail): 实现 FireLifecycle 方法 (6.5)"
```

---

### Task 5: 预留 panic 占位方法

**Files:**
- Modify: `internal/agentcore/single_agent/rail/context.go` — 追加 Fire/Retry/ForceFinish 占位方法
- Modify: `internal/agentcore/single_agent/rail/context_test.go` — 追加 panic 占位测试

- [ ] **Step 1: 在 context.go 中追加预留 panic 方法**

```go
// Fire 触发回调事件。
//
// 对应 Python: AgentCallbackContext.fire(event)
// ⤵️ 6.6 回填：调用 agent.CallbackManager().Execute(event, self)
func (c *AgentCallbackContext) Fire(_ AgentCallbackEvent) error {
	panic("TODO: 6.6 AgentCallbackManager")
}

// RequestRetry 请求重试。
//
// 在 on_model_exception / on_tool_exception 钩子内调用。
// 对应 Python: AgentCallbackContext.request_retry(delay_seconds)
// ⤵️ 6.10 回填
func (c *AgentCallbackContext) RequestRetry(_ float64) {
	panic("TODO: 6.10 RetryRequest")
}

// ConsumeRetryRequest 消费重试请求（一次性）。
//
// 对应 Python: AgentCallbackContext.consume_retry_request() -> Optional[RetryRequest]
// ⤵️ 6.10 回填
func (c *AgentCallbackContext) ConsumeRetryRequest() any {
	panic("TODO: 6.10 RetryRequest")
}

// RequestForceFinish 请求提前终止。
//
// 在任何钩子中调用（如 before_model_call、after_tool_call）。
// 对应 Python: AgentCallbackContext.request_force_finish(result)
// ⤵️ 6.10 回填
func (c *AgentCallbackContext) RequestForceFinish(_ map[string]any) {
	panic("TODO: 6.10 ForceFinishRequest")
}

// ConsumeForceFinish 消费提前终止请求（一次性）。
//
// 对应 Python: AgentCallbackContext.consume_force_finish() -> Optional[ForceFinishRequest]
// ⤵️ 6.10 回填
func (c *AgentCallbackContext) ConsumeForceFinish() any {
	panic("TODO: 6.10 ForceFinishRequest")
}

// HasForceFinishRequest 检查是否有待处理的提前终止请求。
//
// 对应 Python: AgentCallbackContext.has_force_finish_request -> bool
// ⤵️ 6.10 回填
func (c *AgentCallbackContext) HasForceFinishRequest() bool {
	panic("TODO: 6.10 ForceFinishRequest")
}
```

- [ ] **Step 2: 在 context_test.go 中追加 panic 占位测试**

```go
// TestFire_预留Panic 验证 Fire 方法 panic 信息包含 "6.6"
func TestFire_预留Panic(t *testing.T) {
	ctx := NewAgentCallbackContext(nil, nil, nil)
	assert.PanicsWithValue(t, "TODO: 6.6 AgentCallbackManager", func() {
		_ = ctx.Fire(CallbackBeforeModelCall)
	})
}

// TestRequestRetry_预留Panic 验证 RequestRetry 方法 panic 信息包含 "6.10"
func TestRequestRetry_预留Panic(t *testing.T) {
	ctx := NewAgentCallbackContext(nil, nil, nil)
	assert.PanicsWithValue(t, "TODO: 6.10 RetryRequest", func() {
		ctx.RequestRetry(1.0)
	})
}

// TestConsumeRetryRequest_预留Panic 验证 ConsumeRetryRequest 方法 panic 信息包含 "6.10"
func TestConsumeRetryRequest_预留Panic(t *testing.T) {
	ctx := NewAgentCallbackContext(nil, nil, nil)
	assert.PanicsWithValue(t, "TODO: 6.10 RetryRequest", func() {
		_ = ctx.ConsumeRetryRequest()
	})
}

// TestRequestForceFinish_预留Panic 验证 RequestForceFinish 方法 panic 信息包含 "6.10"
func TestRequestForceFinish_预留Panic(t *testing.T) {
	ctx := NewAgentCallbackContext(nil, nil, nil)
	assert.PanicsWithValue(t, "TODO: 6.10 ForceFinishRequest", func() {
		ctx.RequestForceFinish(nil)
	})
}

// TestConsumeForceFinish_预留Panic 验证 ConsumeForceFinish 方法 panic 信息包含 "6.10"
func TestConsumeForceFinish_预留Panic(t *testing.T) {
	ctx := NewAgentCallbackContext(nil, nil, nil)
	assert.PanicsWithValue(t, "TODO: 6.10 ForceFinishRequest", func() {
		_ = ctx.ConsumeForceFinish()
	})
}

// TestHasForceFinishRequest_预留Panic 验证 HasForceFinishRequest 方法 panic 信息包含 "6.10"
func TestHasForceFinishRequest_预留Panic(t *testing.T) {
	ctx := NewAgentCallbackContext(nil, nil, nil)
	assert.PanicsWithValue(t, "TODO: 6.10 ForceFinishRequest", func() {
		_ = ctx.HasForceFinishRequest()
	})
}
```

- [ ] **Step 3: 运行测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/single_agent/rail/... -run "Test(Fire|RequestRetry|ConsumeRetryRequest|RequestForceFinish|ConsumeForceFinish|HasForceFinishRequest)_预留" -v`

Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/single_agent/rail/context.go internal/agentcore/single_agent/rail/context_test.go
git commit -m "feat(rail): 添加 Fire/Retry/ForceFinish 预留 panic 占位方法 (6.5)"
```

---

### Task 6: 更新 doc.go 包文档

**Files:**
- Modify: `internal/agentcore/single_agent/rail/doc.go`

- [ ] **Step 1: 更新 doc.go**

```go
// Package rail 提供 Agent 生命周期 Rail 系统的基础定义。
//
// Rail 是 class-based 的生命周期钩子机制，允许在 Agent 执行流程的
// 特定时机注入拦截逻辑（重试、提前终止、steering 等）。
//
// 本包与框架层 callback/ 包的事件体系是不同层次：
//   - 本包 AgentCallbackEvent = per-Agent 实例级生命周期事件
//   - callback.AgentCallGlobalEventType = 框架级全局观测事件
//
// 两者不桥接，各自独立触发，与 Python 保持一致。
//
// 文件目录：
//
//	rail/
//	├── doc.go       # 包文档
//	├── event.go     # AgentCallbackEvent 枚举定义
//	├── context.go   # AgentCallbackContext 结构体与方法
//	└── inputs.go    # EventInputs 接口及各事件 Inputs 结构体
//
// 核心类型/接口索引：
//
//	AgentCallbackEvent       — 10 种生命周期事件枚举
//	AgentCallbackContext     — Rail 系统核心中介对象（retry/force_finish/steering）
//	EventInputs              — 回调事件输入接口
//	InvokeInputs             — BEFORE/AFTER_INVOKE 事件输入
//	ModelCallInputs          — BEFORE/AFTER_MODEL_CALL 事件输入
//	ToolCallInputs           — BEFORE/AFTER_TOOL_CALL 事件输入
//	TaskIterationInputs      — BEFORE/AFTER_TASK_ITERATION 事件输入
//
// 对应 Python 代码：openjiuwen/core/single_agent/rail/base.py
package rail
```

- [ ] **Step 2: 提交**

```bash
git add internal/agentcore/single_agent/rail/doc.go
git commit -m "docs(rail): 更新包文档，添加 context.go/inputs.go 条目和核心类型索引 (6.5)"
```

---

### Task 7: ToolCallContext 回填 callbackCtx 字段

**Files:**
- Modify: `internal/agentcore/single_agent/ability/ability_types.go:54-67`

- [ ] **Step 1: 在 ToolCallContext 中追加 callbackCtx 字段**

将现有的 `ToolCallContext` 结构体修改为：

```go
// ToolCallContext 工具调用上下文（预留，6.5 回填）。
type ToolCallContext struct {
	// ToolCall 工具调用信息
	ToolCall *llmschema.ToolCall
	// ToolName 工具名称
	ToolName string
	// ToolArgs 工具参数
	ToolArgs map[string]any
	// ToolResult 工具执行结果
	ToolResult any
	// ToolMsg 工具返回消息
	ToolMsg *llmschema.ToolMessage
	// callbackCtx 所属 AgentCallbackContext（6.5 回填）
	// 用于在 ToolRail 钩子中访问 retry/force_finish/steering 等控制机制
	callbackCtx *rail.AgentCallbackContext
	// ⤵️ 预留字段：force_finish / steering_queue / skip_tool
}
```

注意：需要在 `ability_types.go` 的 import 中追加：

```go
"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/rail"
```

由于 `callbackCtx` 是非导出字段，且当前 `ToolRail` 接口的钩子签名不使用此字段，不影响现有编译。

- [ ] **Step 2: 编译验证**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/single_agent/ability/...`

Expected: 编译成功

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/single_agent/ability/ability_types.go
git commit -m "feat(ability): ToolCallContext 添加 callbackCtx 字段，回填 6.5"
```

---

### Task 8: 全量测试 + IMPLEMENTATION_PLAN.md 状态更新

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md` — 6.5 步骤状态 ☐→✅

- [ ] **Step 1: 运行 rail 包全量测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/single_agent/rail/... -v -cover`

Expected: PASS，覆盖率 ≥ 85%

- [ ] **Step 2: 运行 ability 包编译验证**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/single_agent/ability/... -v -cover`

Expected: PASS

- [ ] **Step 3: 更新 IMPLEMENTATION_PLAN.md 中 6.5 步骤状态**

将 `6.5 AgentCallbackContext` 行的 `☐` 改为 `✅`

- [ ] **Step 4: 提交**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "docs: 更新实现计划 6.5 AgentCallbackContext 状态为已完成"
```
