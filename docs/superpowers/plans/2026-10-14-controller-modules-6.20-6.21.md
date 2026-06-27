# Controller 底层组件 6.20+6.21 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 Controller 事件驱动系统的底层组件：6.20 TaskManager/EventQueue + 6.21 TaskScheduler/EventHandler，以及它们依赖的 schema 类型和 MessageQueueInMemory。

**Architecture:** 对齐 Python `openjiuwen/core/controller/` 的模块划分，Go 侧按 `controller/schema/`（类型）、`controller/config/`（配置）、`controller/modules/`（逻辑组件）、`runner/message_queue/`（消息队列基础设施）组织。EventQueue 底层封装 MessageQueueInMemory（Go channel + goroutine），TaskManager 用 sync.RWMutex 保证并发安全，TaskScheduler 用 goroutine + notify channel 做事件驱动调度，EventHandler 用 Embed + setter 注入依赖。

**Tech Stack:** Go 1.22+, sync.RWMutex, context.Context, encoding/json 自定义序列化, go test 表驱动测试

---

## 文件结构

### 新增文件

| 文件路径 | 职责 |
|---------|------|
| `runner/message_queue/doc.go` | 包文档 |
| `runner/message_queue/queue.go` | MessageQueueInMemory + topicSubscription + Subscription |
| `runner/message_queue/message.go` | QueueMessage + InvokeQueueMessage |
| `controller/schema/event.go` | EventType 枚举 + Event 接口 + 5 种具体事件类型 + JSON 多态序列化 |
| `controller/schema/dataframe.go` | DataFrame 接口 + TextDataFrame/JsonDataFrame/FileDataFrame |
| `controller/schema/controller_output.go` | ControllerOutputPayload + ControllerOutputChunk(embed OutputSchema) + ControllerOutput |
| `controller/schema/intent.go` | IntentType 枚举 + Intent 结构体 |
| `controller/schema/task.go` | Task 结构体 + 校验 + JSON 自定义序列化(inputs []Event) |
| `controller/config/doc.go` | 包文档 |
| `controller/config/controller_config.go` | ControllerConfig + DefaultControllerConfig() |
| `controller/modules/doc.go` | 包文档 |
| `controller/modules/event_handler.go` | EventHandler 接口 + EventHandlerBase + EventHandlerInput |
| `controller/modules/task_manager.go` | TaskManager + TaskFilter + TaskManagerState |
| `controller/modules/event_queue.go` | EventQueue |
| `controller/modules/task_executor.go` | TaskExecutor 接口 + TaskExecutorDependencies + TaskExecutorRegistry |
| `controller/modules/task_scheduler.go` | TaskScheduler |

### 新增测试文件

| 文件路径 | 职责 |
|---------|------|
| `runner/message_queue/queue_test.go` | MessageQueueInMemory 测试 |
| `controller/schema/event_test.go` | Event 类型 + JSON 序列化测试 |
| `controller/schema/dataframe_test.go` | DataFrame 类型测试 |
| `controller/schema/controller_output_test.go` | ControllerOutputChunk 测试 |
| `controller/schema/intent_test.go` | IntentType + Intent 测试 |
| `controller/schema/task_test.go` | Task 结构体 + 校验 + JSON 序列化测试 |
| `controller/config/controller_config_test.go` | ControllerConfig 默认值测试 |
| `controller/modules/event_handler_test.go` | EventHandler 接口 + EventHandlerBase 默认方法测试 |
| `controller/modules/task_manager_test.go` | TaskManager CRUD/索引/状态/持久化测试 |
| `controller/modules/event_queue_test.go` | EventQueue 订阅/发布/路由测试 |
| `controller/modules/task_executor_test.go` | TaskExecutor 接口 + Registry 测试 |
| `controller/modules/task_scheduler_test.go` | TaskScheduler 调度/暂停/取消/完成信号测试 |

### 修改文件

| 文件路径 | 修改内容 |
|---------|---------|
| `controller/schema/doc.go` | 更新文件目录，添加新增文件 |
| `controller/schema/task_status.go` | 无修改（已存在） |
| `IMPLEMENTATION_PLAN.md` | 6.20→🔄, 6.21→🔄 |

---

## Task 1: runner/message_queue — 消息类型

**Files:**
- Create: `internal/agentcore/runner/message_queue/message.go`
- Test: `internal/agentcore/runner/message_queue/message_test.go`（合并在 queue_test.go 中）

- [ ] **Step 1: 创建包目录和 doc.go**

```bash
mkdir -p internal/agentcore/runner/message_queue
```

创建 `internal/agentcore/runner/message_queue/doc.go`：

```go
// Package message_queue 提供基于 Go channel 的内存消息队列实现。
//
// 本包对齐 Python 的 MessageQueueInMemory，支持按 topic 路由、
// 同步/火忘发布、订阅生命周期管理。
//
// 文件目录：
//
//	message_queue/
//	├── doc.go        # 包文档
//	├── message.go    # QueueMessage / InvokeQueueMessage 消息类型
//	└── queue.go      # MessageQueueInMemory / Subscription 核心
//
// 对应 Python 代码：openjiuwen/core/runner/message_queue_inmemory.py
package message_queue
```

- [ ] **Step 2: 实现 message.go — QueueMessage + InvokeQueueMessage**

创建 `internal/agentcore/runner/message_queue/message.go`，内容包含：

```go
package message_queue

import "context"

// ──────────────────────────── 结构体 ────────────────────────────

// QueueMessage 火忘消息，发布后不等待处理完成。
//
// 对应 Python: openjiuwen/core/runner/message_queue_base.py (QueueMessage)
type QueueMessage struct {
	// Payload 消息载荷
	Payload map[string]any
}

// InvokeQueueMessage 同步消息，发布后等待处理完成。
//
// 对应 Python: openjiuwen/core/runner/message_queue_base.py (InvokeQueueMessage)
// Python 中 InvokeQueueMessage 继承 QueueMessage 并增加 response Future。
// Go 中使用 channel 实现等价的同步等待语义。
type InvokeQueueMessage struct {
	// Payload 消息载荷
	Payload map[string]any
	// response 处理结果通道
	response chan invokeResponse
}

// invokeResponse handler 处理结果
type invokeResponse struct {
	// result 处理结果
	result any
	// err 处理错误
	err error
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewQueueMessage 创建火忘消息。
func NewQueueMessage(payload map[string]any) *QueueMessage {
	return &QueueMessage{Payload: payload}
}

// NewInvokeQueueMessage 创建同步消息。
func NewInvokeQueueMessage(payload map[string]any) *InvokeQueueMessage {
	return &InvokeQueueMessage{
		Payload:  payload,
		response: make(chan invokeResponse, 1),
	}
}

// WaitResponse 阻塞等待 handler 处理完成。
//
// 对应 Python: await queue_message.response
func (m *InvokeQueueMessage) WaitResponse(ctx context.Context) (any, error) {
	select {
	case resp := <-m.response:
		return resp.result, resp.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// CompleteResponse handler 调用此方法通知处理完成。
func (m *InvokeQueueMessage) CompleteResponse(result any, err error) {
	m.response <- invokeResponse{result: result, err: err}
}
```

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/runner/message_queue/
git commit -m "feat(controller): 添加 message_queue 包骨架和消息类型定义"
```

---

## Task 2: runner/message_queue — MessageQueueInMemory 核心

**Files:**
- Create: `internal/agentcore/runner/message_queue/queue.go`
- Test: `internal/agentcore/runner/message_queue/queue_test.go`

- [ ] **Step 1: 实现 queue.go — MessageQueueInMemory + Subscription**

创建 `internal/agentcore/runner/message_queue/queue.go`，核心内容：

```go
package message_queue

import (
	"context"
	"sync"
	"sync/atomic"
)

// ──────────────────────────── 结构体 ────────────────────────────

// MessageQueueInMemory 基于 Go channel 的内存消息队列。
//
// 对齐 Python MessageQueueInMemory，支持：
//   - 按 topic 路由消息到不同订阅者
//   - 同步发布（InvokeQueueMessage，等待处理完成）
//   - 火忘发布（QueueMessage，不等待）
//   - 订阅生命周期管理（Activate/Deactivate）
//
// 对应 Python: openjiuwen/core/runner/message_queue_inmemory.py
type MessageQueueInMemory struct {
	// maxSize 单个 topic 的 channel 缓冲大小
	maxSize int
	// timeout 消息处理超时
	timeout time.Duration
	// topics topic name → *topicSubscription
	topics map[string]*topicSubscription
	// mu 保护 topics map
	mu sync.RWMutex
	// running 队列是否运行中
	running atomic.Bool
}

// topicSubscription 非导出的 topic 订阅实体
type topicSubscription struct {
	// topic topic 名称
	topic string
	// ch 消息缓冲 channel
	ch chan *QueueMessage
	// handler 消息处理回调
	handler func(ctx context.Context, msg *QueueMessage)
	// handlerMu 保护 handler 字段
	handlerMu sync.RWMutex
	// active 消费 goroutine 是否活跃
	active atomic.Bool
	// cancel 消费 goroutine 取消函数
	cancel context.CancelFunc
}

// Subscription 导出的订阅句柄
type Subscription struct {
	// ts 内部 topic 订阅实体
	ts *topicSubscription
}
```

核心方法实现要点：
- `NewMessageQueueInMemory(maxSize int, timeout time.Duration) *MessageQueueInMemory`
- `Start()` — 标记 running=true
- `Stop(ctx)` — 标记 running=false，遍历 topics 逐一 Deactivate + close channel
- `Subscribe(topic) *Subscription` — 创建 topic（如不存在），返回 Subscription
- `Unsubscribe(ctx, topic) error` — Deactivate + 从 topics map 删除
- `Produce(ctx, topic, msg) error` — 向 topic channel 写入消息
- `Subscription.SetMessageHandler(handler)` — 设置回调
- `Subscription.Activate()` — 启动消费 goroutine: `for msg := range ch { handler(ctx, msg) }`，对 InvokeQueueMessage 处理完后调用 CompleteResponse
- `Subscription.Deactivate()` — cancel 消费 goroutine

- [ ] **Step 2: 编写 queue_test.go — 表驱动测试**

测试用例覆盖：
- `TestMessageQueueInMemory_启动停止` — Start/Stop 生命周期
- `TestMessageQueueInMemory_生产消费` — Produce → handler 收到消息
- `TestMessageQueueInMemory_同步发布等待` — InvokeQueueMessage → WaitResponse 返回结果
- `TestMessageQueueInMemory_火忘发布` — QueueMessage 不等待
- `TestMessageQueueInMemory_多Topic路由` — 不同 topic 消息到不同 handler
- `TestMessageQueueInMemory_订阅取消` — Unsubscribe 后 Produce 返回错误
- `TestMessageQueueInMemory_激活停用` — Activate/Deactivate 控制消费

- [ ] **Step 3: 运行测试确认通过**

```bash
export GOPROXY=https://goproxy.cn,direct
go test ./internal/agentcore/runner/message_queue/... -v -count=1
```

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/runner/message_queue/
git commit -m "feat(controller): 实现 MessageQueueInMemory 核心和测试"
```

---

## Task 3: controller/schema — DataFrame + EventType + Event 类型

**Files:**
- Create: `internal/agentcore/controller/schema/dataframe.go`
- Create: `internal/agentcore/controller/schema/event.go`
- Test: `internal/agentcore/controller/schema/dataframe_test.go`
- Test: `internal/agentcore/controller/schema/event_test.go`

- [ ] **Step 1: 实现 dataframe.go**

DataFrame 接口 + TextDataFrame / JsonDataFrame / FileDataFrame 实现。每种实现 `DataType() string` 方法返回 "text"/"json"/"file"。

- [ ] **Step 2: 实现 event.go — EventType 枚举 + Event 接口 + 具体事件类型**

关键设计：
- `EventType` 为 `string` 类型，5 个常量
- `Event` 接口：`GetEventType() / GetEventID() / GetMetadata() / SetMetadata()`
- `BaseEvent` 实现 Event 接口，其他事件类型 embed BaseEvent
- `InputEvent` 增加 `InputData []DataFrame` + `FromUserInput(userInput any) (*InputEvent, error)` 工厂方法（支持 string/dict/InputEvent 三种输入）
- `TaskInteractionEvent` 增加 `Interaction []DataFrame` + `Task *Task`
- `TaskCompletionEvent` 增加 `TaskResult []DataFrame` + `Task *Task`
- `TaskFailedEvent` 增加 `ErrorMessage string` + `Task *Task`
- `FollowUpEvent` 增加 `InputData []DataFrame` + `FromText(text string) *FollowUpEvent` 工厂方法

- [ ] **Step 3: 实现 Event JSON 多态序列化**

`[]Event` 的自定义 MarshalJSON/UnmarshalJSON：
- Marshal：遍历每个 Event，序列化为具体类型的 JSON，确保写入 `"event_type"` 字段
- Unmarshal：先反序列化为 `map[string]any` 读取 `event_type`，switch 到具体类型再反序列化

对齐 `foundation/llm/schema/message.go` 中 `MessageContent` 的自定义序列化模式。

- [ ] **Step 4: 编写测试 — dataframe_test.go + event_test.go**

- `TestDataFrame_类型标识` — DataType() 返回值对齐 Python
- `TestEventType_值对齐Python` — 枚举值与 Python 完全一致
- `TestInputEvent_FromUserInput_字符串` / `TestInputEvent_FromUserInput_字典` / `TestInputEvent_FromUserInput_已有InputEvent`
- `TestFollowUpEvent_FromText`
- `TestEvent_JSON序列化_多态` — []Event 的 marshal/unmarshal round-trip
- `TestEvent_JSON反序列化_按EventType分发`

- [ ] **Step 5: 运行测试确认通过**

```bash
go test ./internal/agentcore/controller/schema/... -v -count=1
```

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/controller/schema/
git commit -m "feat(controller): 实现 DataFrame 和 Event 类型定义及 JSON 多态序列化"
```

---

## Task 4: controller/schema — ControllerOutput + Intent + Task

**Files:**
- Create: `internal/agentcore/controller/schema/controller_output.go`
- Create: `internal/agentcore/controller/schema/intent.go`
- Create: `internal/agentcore/controller/schema/task.go`
- Test: `internal/agentcore/controller/schema/controller_output_test.go`
- Test: `internal/agentcore/controller/schema/intent_test.go`
- Test: `internal/agentcore/controller/schema/task_test.go`

- [ ] **Step 1: 实现 controller_output.go**

- `ControllerOutputPayload`：Type string + Data []DataFrame + Metadata map[string]any
- `ControllerOutputChunk`：embed `stream.OutputSchema` + Payload *ControllerOutputPayload + LastChunk bool，实现 `stream.Schema` 接口（SchemaType() 委托给 OutputSchema，Validate() 自定义校验）
- `ControllerOutput`：Type string + Data []*ControllerOutputChunk + InputEventID string
- 常量：`TaskProcessing = "processing"`, `AllTasksProcessed = "all_tasks_processed"`

- [ ] **Step 2: 实现 intent.go**

- `IntentType` 枚举：9 种意图类型
- `Intent` 结构体：对齐 Python Intent 全部字段

- [ ] **Step 3: 实现 task.go — Task 结构体 + 校验 + JSON 序列化**

Task 结构体对齐 Python Task 全部字段：
- Inputs `[]Event` — 使用 Task 上的自定义 MarshalJSON/UnmarshalJSON 处理
- Outputs `[]*ControllerOutputChunk`
- 字段校验函数 `ValidateTask(task *Task) error`（对齐 Python @field_validator + @model_validator）

- [ ] **Step 4: 编写测试**

- `TestControllerOutputChunk_嵌入OutputSchema` — SchemaType() 和 Validate() 行为
- `TestControllerOutputChunk_实现Schema接口` — 类型断言 `var _ stream.Schema = (*ControllerOutputChunk)(nil)`
- `TestIntentType_值对齐Python`
- `TestTask_字段校验_正常` / `TestTask_字段校验_缺少必填` / `TestTask_字段校验_优先级为负` / `TestTask_字段校验_自引用` / `TestTask_字段校验_FAILED缺errorMessage` / `TestTask_字段校验_INPUT_REQUIRED缺inputRequiredFields`
- `TestTask_JSON序列化_roundTrip` — marshal/unmarshal 一致性
- `TestTask_JSON序列化_inputs多态` — Task 包含不同 Event 类型的 inputs

- [ ] **Step 5: 更新 controller/schema/doc.go**

更新文件目录树，添加所有新增文件。

- [ ] **Step 6: 运行测试确认通过**

```bash
go test ./internal/agentcore/controller/schema/... -v -count=1
```

- [ ] **Step 7: 提交**

```bash
git add internal/agentcore/controller/schema/
git commit -m "feat(controller): 实现 ControllerOutput/Intent/Task 类型定义及校验"
```

---

## Task 5: controller/config — ControllerConfig

**Files:**
- Create: `internal/agentcore/controller/config/doc.go`
- Create: `internal/agentcore/controller/config/controller_config.go`
- Test: `internal/agentcore/controller/config/controller_config_test.go`

- [ ] **Step 1: 实现 controller_config.go**

对齐 Python ControllerConfig 全部字段和默认值。`DefaultControllerConfig()` 返回默认配置实例。

- [ ] **Step 2: 编写测试 — 默认值对齐 Python**

- `TestControllerConfig_默认值对齐Python` — 验证每个字段默认值与 Python 一致

- [ ] **Step 3: 运行测试确认通过**

```bash
go test ./internal/agentcore/controller/config/... -v -count=1
```

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/controller/config/
git commit -m "feat(controller): 实现 ControllerConfig 配置定义"
```

---

## Task 6: controller/modules — EventHandler 接口 + EventHandlerBase

**Files:**
- Create: `internal/agentcore/controller/modules/event_handler.go`
- Test: `internal/agentcore/controller/modules/event_handler_test.go`

**依赖：** Task 4（schema 类型）, Task 5（config）

- [ ] **Step 1: 创建包目录和 doc.go**

```bash
mkdir -p internal/agentcore/controller/modules
```

创建 `internal/agentcore/controller/modules/doc.go`，包概述 + 文件目录。

- [ ] **Step 2: 实现 event_handler.go**

- `EventHandler` 接口：5 个 Handle 方法 + GetBase() + PrepareRound + WaitCompletion + OnAbort
- `EventHandlerBase` struct：Config / ContextEngine / TaskManager / TaskScheduler / AbilityMgr
- EventHandlerBase 默认实现：HandleFollowUp 返回 not_supported, PrepareRound 返回 0, WaitCompletion 返回 completed, OnAbort 空操作
- `EventHandlerInput` struct：Event + Session SessionFacade

注意：EventHandlerBase 中的 TaskManager / TaskScheduler 字段类型暂用指针（因为本包定义这两个类型），存在包内前向引用。解决方式：在同一包内定义，文件间引用无需 import。

- [ ] **Step 3: 编写测试**

- `TestEventHandlerBase_HandleFollowUp_默认实现` — 返回 `{"status": "not_supported"}`
- `TestEventHandlerBase_PrepareRound_默认实现` — 返回 0
- `TestEventHandlerBase_WaitCompletion_默认实现` — 返回 `{"status": "completed"}`
- `TestEventHandlerBase_OnAbort_默认实现` — 无 panic
- `TestEventHandlerInput_字段访问` — Event 和 Session 正确设置

- [ ] **Step 4: 运行测试确认通过**

```bash
go test ./internal/agentcore/controller/modules/... -v -count=1
```

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/controller/modules/
git commit -m "feat(controller): 实现 EventHandler 接口和 EventHandlerBase 默认实现"
```

---

## Task 7: controller/modules — TaskManager

**Files:**
- Create: `internal/agentcore/controller/modules/task_manager.go`
- Test: `internal/agentcore/controller/modules/task_manager_test.go`

**依赖：** Task 4（schema/Task + TaskFilter + TaskManagerState）, Task 5（config）

- [ ] **Step 1: 实现 task_manager.go**

核心结构和方法按设计文档 4.1 节实现：
- `TaskManager` struct 含 tasks map + 4 种索引 + sync.RWMutex + onTaskSubmitted 回调
- CRUD 方法：AddTask / GetTask / PopTask / UpdateTask / RemoveTask
- 状态方法：UpdateTaskStatus / SetPriority / GetChildTask
- 持久化方法：GetState / LoadState / ClearState
- 通知：SetOnTaskSubmitted
- `TaskFilter` struct 含校验（至少一个条件非零值）
- `TaskManagerState` struct（可序列化状态快照）
- Functional Options 模式：`WithChildren(bool)` / `IsRecursive(bool)` / `WithErrorMessage(string)` 用于 UpdateTaskStatus

- [ ] **Step 2: 编写测试 — 表驱动测试**

- `TestTaskManager_AddTask_正常` / `TestTaskManager_AddTask_重复ID报错`
- `TestTaskManager_GetTask_按TaskID` / `TestTaskManager_GetTask_按SessionID` / `TestTaskManager_GetTask_按Status` / `TestTaskManager_GetTask_按优先级` / `TestTaskManager_GetTask_按IsRoot` / `TestTaskManager_GetTask_带子任务`
- `TestTaskManager_PopTask_正常` / `TestTaskManager_PopTask_filter为nil报错`
- `TestTaskManager_UpdateTask_正常` / `TestTaskManager_UpdateTask_不存在返回false` / `TestTaskManager_UpdateTask_优先级变更更新索引` / `TestTaskManager_UpdateTask_父任务变更更新层级`
- `TestTaskManager_RemoveTask_正常`
- `TestTaskManager_UpdateTaskStatus_正常` / `TestTaskManager_UpdateTaskStatus_FAILED设置ErrorMessage`
- `TestTaskManager_SetPriority_正常` / `TestTaskManager_SetPriority_更新索引`
- `TestTaskManager_GetChildTask_直接子任务` / `TestTaskManager_GetChildTask_递归子任务`
- `TestTaskManager_状态持久化_GetStateLoadState` / `TestTaskManager_状态持久化_ClearState`
- `TestTaskManager_提交通知回调` — AddTask SUBMITTED 状态触发 onTaskSubmitted
- `TestTaskManager_并发安全` — 多 goroutine 并发 AddTask/GetTask 无 data race

- [ ] **Step 3: 运行测试确认通过**

```bash
go test ./internal/agentcore/controller/modules/... -run TestTaskManager -v -count=1 -race
```

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/controller/modules/task_manager.go internal/agentcore/controller/modules/task_manager_test.go
git commit -m "feat(controller): 实现 TaskManager 任务管理器及索引"
```

---

## Task 8: controller/modules — EventQueue

**Files:**
- Create: `internal/agentcore/controller/modules/event_queue.go`
- Test: `internal/agentcore/controller/modules/event_queue_test.go`

**依赖：** Task 2（MessageQueueInMemory）, Task 6（EventHandler）, Task 5（config）

- [ ] **Step 1: 实现 event_queue.go**

核心结构和方法按设计文档 4.2 节实现：
- `EventQueue` struct 含 config + *MessageQueueInMemory + eventHandler
- `NewEventQueue(config) *EventQueue` — 内部创建 MessageQueueInMemory
- `SetEventHandler(handler EventHandler)` — 设置事件处理器
- `Start()` / `Stop(ctx)` — 启停
- `Subscribe(ctx, agentID, sessionID)` — 为 5 种事件类型创建 topic + 订阅，每个 topic 绑定对应 handler 方法
- `Unsubscribe(ctx, agentID, sessionID)` — 取消 5 种事件订阅
- `PublishEvent(ctx, agentID, sess, event)` — 同步发布（InvokeQueueMessage + WaitResponse）
- `PublishEventAsync(ctx, agentID, sess, event)` — 火忘发布（QueueMessage）
- 内部 `buildTopic(agentID, sessionID, eventType)` → `"{agentID}_{sessionID}_{eventType}"`

Subscribe 内部实现关键：为每个 EventType 创建 topic，订阅后设置 message handler（从 payload 提取 event + session，构造 EventHandlerInput，调用对应的 handler 方法）。

- [ ] **Step 2: 编写测试**

需要创建 fakeEventHandler 实现 EventHandler 接口用于测试：
- `TestEventQueue_订阅取消` — Subscribe 后可 Produce，Unsubscribe 后 Produce 失败
- `TestEventQueue_同步发布_等待处理完成` — PublishEvent 返回后 handler 已处理
- `TestEventQueue_火忘发布_不等处理` — PublishEventAsync 立即返回
- `TestEventQueue_Topic路由_不同事件类型到不同Handler` — 5 种事件分别路由到对应 handler 方法
- `TestEventQueue_启停` — Start/Stop 生命周期

- [ ] **Step 3: 运行测试确认通过**

```bash
go test ./internal/agentcore/controller/modules/... -run TestEventQueue -v -count=1
```

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/controller/modules/event_queue.go internal/agentcore/controller/modules/event_queue_test.go
git commit -m "feat(controller): 实现 EventQueue 事件发布订阅队列"
```

---

## Task 9: controller/modules — TaskExecutor 接口 + Registry

**Files:**
- Create: `internal/agentcore/controller/modules/task_executor.go`
- Test: `internal/agentcore/controller/modules/task_executor_test.go`

**依赖：** Task 4（schema 类型）, Task 5（config）

- [ ] **Step 1: 实现 task_executor.go**

- `TaskExecutor` 接口：ExecuteAbility(ctx, taskID, sess) `<-chan *ControllerOutputChunk` / CanPause / Pause / CanCancel / Cancel
- `TaskExecutorDependencies` struct：Config + AbilityMgr + ContextEngine + TaskManager + EventQueue
- `TaskExecutorRegistry` struct：builders map[string]func(deps *TaskExecutorDependencies) TaskExecutor + sync.RWMutex
- Registry 方法：AddTaskExecutor / RemoveTaskExecutor / GetTaskExecutor

- [ ] **Step 2: 编写测试**

创建 fakeTaskExecutor 实现 TaskExecutor 接口：
- `TestTaskExecutorRegistry_注册获取` — Add + Get 正常
- `TestTaskExecutorRegistry_未注册返回错误` — Get 不存在的 type 报错
- `TestTaskExecutorRegistry_移除` — Remove 后 Get 报错
- `TestTaskExecutor_ExecuteAbility返回Channel` — fake executor 返回 channel 并发射 chunk

- [ ] **Step 3: 运行测试确认通过**

```bash
go test ./internal/agentcore/controller/modules/... -run TestTaskExecutor -v -count=1
```

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/controller/modules/task_executor.go internal/agentcore/controller/modules/task_executor_test.go
git commit -m "feat(controller): 实现 TaskExecutor 接口和 TaskExecutorRegistry"
```

---

## Task 10: controller/modules — TaskScheduler

**Files:**
- Create: `internal/agentcore/controller/modules/task_scheduler.go`
- Test: `internal/agentcore/controller/modules/task_scheduler_test.go`

**依赖：** Task 7（TaskManager）, Task 8（EventQueue）, Task 9（TaskExecutor）

这是最复杂的组件，实现按设计文档 4.5 节。

- [ ] **Step 1: 实现 task_scheduler.go — 核心结构**

```go
type TaskScheduler struct {
	config               *config.ControllerConfig
	taskManager          *TaskManager
	contextEngine        iface.ContextEngine
	abilityMgr           *ability.AbilityManager
	eventQueue           *EventQueue
	taskExecutorRegistry *TaskExecutorRegistry
	sessions             map[string]sessioninterfaces.SessionFacade
	card                 *agentschema.AgentCard
	runningTasks         map[string]*runningTaskEntry
	mu                   sync.Mutex
	running              atomic.Bool
	notifyCh             chan struct{}
	cancelFunc           context.CancelFunc
}

type runningTaskEntry struct {
	executor TaskExecutor
	cancel   context.CancelFunc
}
```

- [ ] **Step 2: 实现核心方法**

- `NewTaskScheduler(config, taskManager, contextEngine, abilityMgr, eventQueue, card) *TaskScheduler`
- `Start(ctx) error` — running=true, 启动调度 goroutine
- `Stop(ctx) error` — running=false, cancel 所有运行中任务, cancel 调度 goroutine
- `NotifyTaskSubmitted()` — `notifyCh <- struct{}{}`
- `Sessions() map[string]sessioninterfaces.SessionFacade` — 返回 sessions（供 Controller 读写）
- `TaskExecutorRegistry() *TaskExecutorRegistry` — 返回 registry（供 Controller 注册 executor）

- [ ] **Step 3: 实现调度循环 schedule(ctx)**

后台 goroutine：
1. 扫描 SUBMITTED 任务
2. 并发启动（受 maxConcurrentTasks 限制）
3. 等待 notifyCh 或 ScheduleInterval 超时
4. 退出时 waitAllTasksComplete

- [ ] **Step 4: 实现 executeTask + executeTaskWrapper**

- `executeTask(ctx, taskID, sess)` — 获取 Task → 创建 Executor → WORKING → range channel → WriteStream → 按 payload.type 更新状态
- `executeTaskWrapper(ctx, taskID, sess)` — 包装：处理超时/取消/异常 → ensureSessionCompletionSignal

- [ ] **Step 5: 实现 PauseTask / CancelTask**

对齐 Python 的 pause_task / cancel_task 逻辑。

- [ ] **Step 6: 实现 ensureSessionCompletionSignal**

检查所有任务是否终态，若是则向 session 流写入 all_tasks_processed chunk。

- [ ] **Step 7: 编写测试**

创建测试辅助设施（fake TaskExecutor、fake Session 等）：
- `TestTaskScheduler_启停` — Start/Stop 生命周期
- `TestTaskScheduler_调度执行` — 提交任务 → 调度器自动执行 → 状态变为 COMPLETED
- `TestTaskScheduler_并发执行` — 多个 SUBMITTED 任务并发执行
- `TestTaskScheduler_最大并发限制` — 超过 maxConcurrentTasks 时等待
- `TestTaskScheduler_暂停任务` / `TestTaskScheduler_取消任务`
- `TestTaskScheduler_任务超时` — task_timeout 后标记 FAILED
- `TestTaskScheduler_完成信号` — 所有任务完成后写入 all_tasks_processed
- `TestTaskScheduler_NotifyTaskSubmitted` — 唤醒调度循环

- [ ] **Step 8: 运行测试确认通过**

```bash
go test ./internal/agentcore/controller/modules/... -run TestTaskScheduler -v -count=1 -timeout 60s
```

- [ ] **Step 9: 提交**

```bash
git add internal/agentcore/controller/modules/task_scheduler.go internal/agentcore/controller/modules/task_scheduler_test.go
git commit -m "feat(controller): 实现 TaskScheduler 任务调度器"
```

---

## Task 11: 更新 modules/doc.go + 全量测试 + 更新 IMPLEMENTATION_PLAN.md

**Files:**
- Modify: `internal/agentcore/controller/modules/doc.go`
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 更新 modules/doc.go**

更新文件目录树，添加所有新增文件（不含 _test.go）。

- [ ] **Step 2: 全量测试**

```bash
go test ./internal/agentcore/controller/... -v -count=1 -cover
go test ./internal/agentcore/runner/message_queue/... -v -count=1 -cover
```

确认覆盖率 ≥ 85%。

- [ ] **Step 3: 更新 IMPLEMENTATION_PLAN.md**

将 6.20 和 6.21 的状态从 `☐` 改为 `✅`：

```
| 6.20 | ✅ | TaskManager / EventQueue | 任务与事件队列 | `openjiuwen/core/controller/` |
| 6.21 | ✅ | TaskScheduler / EventHandler | 调度器与事件处理器 | `openjiuwen/core/controller/` |
```

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/controller/modules/doc.go IMPLEMENTATION_PLAN.md
git commit -m "feat(controller): 完成 6.20+6.21 底层组件实现，更新实现计划状态"
```
