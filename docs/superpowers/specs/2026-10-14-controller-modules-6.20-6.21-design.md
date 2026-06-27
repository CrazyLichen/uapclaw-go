# 事件驱动 Controller 底层组件设计（6.20 + 6.21）

## 概述

本文档描述 Controller 事件驱动系统的底层组件（6.20 TaskManager/EventQueue + 6.21 TaskScheduler/EventHandler）的 Go 实现设计，对齐 Python 源码 `openjiuwen/core/controller/`。

## 在 Agent 会话流程中的位置

```
用户请求 → ControllerAgent (6.22) → Controller (6.19) → EventHandler (6.21) → TaskScheduler (6.21) → TaskExecutor → Ability/LLM
                                         ↑                    ↑
                                   EventQueue (6.20)    TaskManager (6.20)
```

本次实现范围：虚线框内的 6.20 + 6.21 组件，以及它们依赖的 schema 类型和 MessageQueueInMemory。

## 包结构

```
internal/agentcore/
├── runner/
│   └── message_queue/                    # 新增：内存消息队列
│       ├── doc.go
│       ├── queue.go                      # MessageQueueInMemory + Topic + Subscription
│       ├── message.go                    # QueueMessage / InvokeQueueMessage
│       └── queue_test.go
├── controller/
│   ├── schema/                           # 已有，扩展
│   │   ├── doc.go                        # 更新
│   │   ├── task_status.go               # 已有
│   │   ├── task.go                      # 新增：Task 结构体
│   │   ├── event.go                     # 新增：EventType + Event 接口 + 具体事件类型
│   │   ├── dataframe.go                 # 新增：DataFrame 接口 + Text/Json/File 实现
│   │   ├── controller_output.go         # 新增：ControllerOutputPayload/Chunk/Output
│   │   └── intent.go                    # 新增：IntentType + Intent
│   ├── config/                           # 新增
│   │   ├── doc.go
│   │   └── controller_config.go         # ControllerConfig
│   └── modules/                          # 新增：6.20+6.21 核心组件
│       ├── doc.go
│       ├── task_manager.go              # 6.20：TaskManager + TaskManagerState + TaskFilter
│       ├── event_queue.go              # 6.20：EventQueue
│       ├── task_scheduler.go            # 6.21：TaskScheduler
│       ├── task_executor.go             # 6.21：TaskExecutor 接口 + Dependencies + Registry
│       └── event_handler.go             # 6.21：EventHandler 接口 + EventHandlerBase + Input
```

## 对应 Python 源码映射

| Go 文件 | Python 源码 |
|---------|------------|
| `runner/message_queue/queue.go` | `openjiuwen/core/runner/message_queue_inmemory.py` |
| `runner/message_queue/message.go` | `openjiuwen/core/runner/message_queue_base.py` (QueueMessage/InvokeQueueMessage) |
| `controller/schema/event.go` | `openjiuwen/core/controller/schema/event.py` |
| `controller/schema/task.go` | `openjiuwen/core/controller/schema/task.py` |
| `controller/schema/dataframe.go` | `openjiuwen/core/controller/schema/dataframe.py` |
| `controller/schema/controller_output.go` | `openjiuwen/core/controller/schema/controller_output.py` |
| `controller/schema/intent.go` | `openjiuwen/core/controller/schema/intent.py` |
| `controller/config/controller_config.go` | `openjiuwen/core/controller/config.py` |
| `controller/modules/task_manager.go` | `openjiuwen/core/controller/modules/task_manager.py` |
| `controller/modules/event_queue.go` | `openjiuwen/core/controller/modules/event_queue.py` |
| `controller/modules/task_scheduler.go` | `openjiuwen/core/controller/modules/task_scheduler.py` |
| `controller/modules/task_executor.go` | `openjiuwen/core/controller/modules/task_scheduler.py` (TaskExecutor/Registry/Dependencies) |
| `controller/modules/event_handler.go` | `openjiuwen/core/controller/modules/event_handler.py` |

---

## 一、runner/message_queue — MessageQueueInMemory

### 1.1 核心类型

#### MessageQueueInMemory

```go
type MessageQueueInMemory struct {
    maxSize int
    timeout time.Duration
    topics  map[string]*topicSubscription  // topic name → subscription
    mu      sync.RWMutex
    running bool
}
```

核心方法：
- `NewMessageQueueInMemory(maxSize int, timeout time.Duration) *MessageQueueInMemory`
- `Start()` — 启动（Python 中 start 只是标记 running，消费 goroutine 在 Activate 时启动）
- `Stop(ctx context.Context) error` — 停止所有消费 goroutine，关闭所有 channel
- `Subscribe(topic string) *Subscription` — 创建 topic + subscription（如已存在返回已有的）
- `Unsubscribe(ctx context.Context, topic string) error` — 取消订阅，关闭对应 channel
- `Produce(ctx context.Context, topic string, msg QueueMessage) error` — 向 topic 生产消息

#### topicSubscription（非导出）

```go
type topicSubscription struct {
    topic   string
    ch      chan QueueMessage     // 带缓冲 channel，缓冲大小 = maxSize
    handler func(ctx context.Context, msg QueueMessage)  // 消息处理回调
    active  atomic.Bool
    cancel  context.CancelFunc   // 消费 goroutine 取消函数
}
```

- `activate()` — 启动消费 goroutine：`for msg := range ch { handler(ctx, msg) }`
- `deactivate()` — 取消消费 goroutine（不关闭 channel，允许再次激活）

#### Subscription（导出句柄）

```go
type Subscription struct {
    ts *topicSubscription
}
```

- `SetMessageHandler(handler func(ctx context.Context, msg QueueMessage))` — 设置消息处理回调
- `Activate()` — 激活消费
- `Deactivate()` — 停用消费

### 1.2 消息类型

#### QueueMessage — 火忘消息

```go
type QueueMessage struct {
    Payload map[string]any
}
```

#### InvokeQueueMessage — 同步消息

```go
type InvokeQueueMessage struct {
    Payload  map[string]any
    response chan invokeResponse  // 处理结果通道
}

type invokeResponse struct {
    result any
    err    error
}
```

- `WaitResponse(ctx context.Context) (any, error)` — 阻塞等待 handler 处理完成
- 消费 goroutine 处理完后向 response channel 写入结果

### 1.3 同步发布 vs 火忘发布

```go
// 同步：PublishEvent 使用 InvokeQueueMessage
msg := NewInvokeQueueMessage(event, session)
queue.Produce(ctx, topic, msg)
result, err := msg.WaitResponse(ctx)  // 阻塞等待 handler 处理完成

// 火忘：PublishEventAsync 使用 QueueMessage
msg := NewQueueMessage(event, session)
queue.Produce(ctx, topic, msg)  // 直接返回，不等待
```

---

## 二、controller/schema — 类型定义

### 2.1 EventType 枚举（event.go）

```go
type EventType string

const (
    EventInput           EventType = "input"
    EventTaskInteraction EventType = "task_interaction"
    EventTaskCompletion  EventType = "task_completion"
    EventTaskFailed      EventType = "task_failed"
    EventFollowUp        EventType = "follow_up"
)
```

### 2.2 Event 接口 + 具体事件类型（event.go）

```go
// Event 事件接口，所有事件类型必须实现
type Event interface {
    GetEventType() EventType
    GetEventID() string
    GetMetadata() map[string]any
    SetMetadata(meta map[string]any)
}
```

具体事件类型：

| 类型 | 固定 EventType | 特有字段 |
|------|---------------|---------|
| `BaseEvent` | 可变 | event_type, event_id, metadata |
| `InputEvent` | `EventInput` | input_data `[]DataFrame` + `FromUserInput()` 工厂方法 |
| `TaskInteractionEvent` | `EventTaskInteraction` | interaction `[]DataFrame`, task `*Task` |
| `TaskCompletionEvent` | `EventTaskCompletion` | task_result `[]DataFrame`, task `*Task` |
| `TaskFailedEvent` | `EventTaskFailed` | error_message string, task `*Task` |
| `FollowUpEvent` | `EventFollowUp` | input_data `[]DataFrame` + `FromText()` 工厂方法 |

**JSON 序列化**：`[]Event` 需自定义 MarshalJSON/UnmarshalJSON。序列化时写入 `"event_type"` 字段用于分发；反序列化时读取 `"event_type"` 值，switch 到具体类型反序列化。

### 2.3 Task 结构体（task.go）

```go
type Task struct {
    SessionID          string                    `json:"session_id"`
    TaskID             string                    `json:"task_id"`
    TaskType           string                    `json:"task_type"`
    Description        string                    `json:"description,omitempty"`
    Priority           int                       `json:"priority"`
    Inputs             []Event                   `json:"inputs,omitempty"`      // 自定义 JSON 序列化
    Outputs            []*ControllerOutputChunk  `json:"outputs,omitempty"`
    Status             TaskStatus                `json:"status"`
    ParentTaskID       string                    `json:"parent_task_id,omitempty"`
    ContextID          string                    `json:"context_id,omitempty"`
    InputRequiredFields map[string]any           `json:"input_required_fields,omitempty"`
    ErrorMessage       string                    `json:"error_message,omitempty"`
    Metadata           map[string]any            `json:"metadata,omitempty"`
    Extensions         map[string]any            `json:"extensions,omitempty"`
}
```

字段校验（对齐 Python `@field_validator` + `@model_validator`）：
- task_id / session_id / task_type 非空
- priority >= 0
- parent_task_id 不能为空字符串（None 或有值）
- task_id ≠ parent_task_id（无自引用）
- FAILED 状态需 error_message 非空
- INPUT_REQUIRED 状态需 input_required_fields 非空

### 2.4 DataFrame（dataframe.go）

```go
// DataFrame 数据帧接口
type DataFrame interface {
    DataType() string  // "text" | "json" | "file"
}

// TextDataFrame 文本数据帧
type TextDataFrame struct {
    Text string `json:"text"`
}

// JsonDataFrame JSON 数据帧
type JsonDataFrame struct {
    Data map[string]any `json:"data"`
}

// FileDataFrame 文件数据帧
type FileDataFrame struct {
    Name     string `json:"name"`
    MIMEType string `json:"mime_type"`
    Bytes    []byte `json:"bytes,omitempty"`
    URI      string `json:"uri,omitempty"`
}
```

### 2.5 ControllerOutput（controller_output.go）

```go
// ControllerOutputPayload 输出载荷
type ControllerOutputPayload struct {
    Type     string      `json:"type"`  // task_completion | task_interaction | task_failed | processing | all_tasks_processed
    Data     []DataFrame `json:"data"`
    Metadata map[string]any `json:"metadata,omitempty"`
}

// ControllerOutputChunk 输出分片，嵌入 stream.OutputSchema 实现 Schema 接口
type ControllerOutputChunk struct {
    stream.OutputSchema                            // 嵌入：Type/Index/Payload
    Payload   *ControllerOutputPayload `json:"payload"`   // 覆盖 OutputSchema.Payload 为强类型
    LastChunk bool                     `json:"last_chunk"`
}

// 确保 ControllerOutputChunk 实现 stream.Schema 接口
// SchemaType() → OutputSchema.SchemaType()
// Validate() → 自定义校验逻辑

// ControllerOutput 批量输出
type ControllerOutput struct {
    Type         string                     `json:"type"`
    Data         []*ControllerOutputChunk   `json:"data"`
    InputEventID string                    `json:"input_event_id,omitempty"`
}
```

常量：
```go
const (
    TaskProcessing    = "processing"
    AllTasksProcessed = "all_tasks_processed"
)
```

### 2.6 Intent（intent.go）

```go
type IntentType string

const (
    IntentCreateTask    IntentType = "create_task"
    IntentPauseTask     IntentType = "pause_task"
    IntentResumeTask    IntentType = "resume_task"
    IntentContinueTask  IntentType = "continue_task"
    IntentSupplementTask IntentType = "supplement_task"
    IntentCancelTask    IntentType = "cancel_task"
    IntentModifyTask    IntentType = "modify_task"
    IntentSwitchTask    IntentType = "switch_task"
    IntentUnknownTask   IntentType = "unknown_task"
)

type Intent struct {
    IntentType           IntentType    `json:"intent_type"`
    Event               Event         `json:"event"`
    TargetTaskID        string        `json:"target_task_id,omitempty"`
    TargetTaskDescription string      `json:"target_task_description,omitempty"`
    DependTaskID        string        `json:"depend_task_id,omitempty"`
    SupplementaryInfo   map[string]any `json:"supplementary_info,omitempty"`
    ModificationDetails map[string]any `json:"modification_details,omitempty"`
    Confidence          float64       `json:"confidence"`
    Metadata            map[string]any `json:"metadata,omitempty"`
    ClarificationPrompt string       `json:"clarification_prompt,omitempty"`
}
```

---

## 三、controller/config — ControllerConfig

```go
type ControllerConfig struct {
    // 调度配置
    MaxConcurrentTasks int      `json:"max_concurrent_tasks"`     // 默认 5
    ScheduleInterval   float64  `json:"schedule_interval"`       // 默认 1.0 秒
    TaskTimeout        *float64 `json:"task_timeout,omitempty"`  // 默认 nil（无超时）

    // 任务管理配置
    DefaultTaskPriority  int  `json:"default_task_priority"`    // 默认 1
    EnableTaskPersistence bool `json:"enable_task_persistence"` // 默认 false

    // 事件队列配置
    EventQueueSize int     `json:"event_queue_size"`   // 默认 10000
    EventTimeout   float64 `json:"event_timeout"`      // 默认 300 秒

    // 意图识别配置
    EnableIntentRecognition    bool     `json:"enable_intent_recognition"`      // 默认 false
    IntentLLMID               string   `json:"intent_llm_id"`                 // 默认 ""
    IntentConfidenceThreshold float64  `json:"intent_confidence_threshold"`   // 默认 0.7
    IntentTypeList            []string `json:"intent_type_list"`              // 默认 ["create_task","pause_task","resume_task","cancel_task","unknown_task"]

    // 默认响应
    DefaultResponseType string `json:"default_response_type"`  // 默认 "text"
    DefaultResponseText string `json:"default_response_text"`  // 默认 ""

    // 完成信号
    SuppressCompletionSignal bool `json:"suppress_completion_signal"` // 默认 false

    // 流配置
    StreamFirstFrameTimeout float64 `json:"stream_first_frame_timeout"` // 默认 30.0
}
```

`DefaultControllerConfig()` 返回上述默认值的配置实例。

---

## 四、controller/modules — 核心组件

### 4.1 TaskManager (6.20)

#### 数据结构

```go
type TaskManager struct {
    config  *ControllerConfig
    tasks   map[string]*Task                   // taskID → Task
    priorityIndex   map[int][]string           // priority → []taskID
    parentToChildren map[string]map[string]struct{}  // parentTaskID → {childTaskID}
    childToParent   map[string]string          // childTaskID → parentTaskID
    rootTasks       map[string]struct{}        // 根任务集合
    mu              sync.RWMutex               // 并发安全
    onTaskSubmitted func()                     // SUBMITTED 状态通知回调
}
```

#### 核心方法

| 方法 | 读/写 | 说明 |
|------|--------|------|
| `AddTask(ctx, task *Task) error` | 写 | 添加任务，更新索引和层级，触发 onTaskSubmitted |
| `GetTask(ctx, filter *TaskFilter) ([]*Task, error)` | 读 | 按条件查询，返回深拷贝 |
| `PopTask(ctx, filter *TaskFilter) ([]*Task, error)` | 写 | 查询并移除，返回深拷贝 |
| `UpdateTask(ctx, task *Task) bool` | 写 | 更新任务，同步更新索引 |
| `RemoveTask(ctx, filter *TaskFilter) error` | 写 | 按条件删除 |
| `UpdateTaskStatus(ctx, taskID string, newStatus TaskStatus, opts ...TaskStatusOption) error` | 写 | 更新状态，支持 with_children/is_recursive/error_message 选项 |
| `SetPriority(ctx, taskID string, newPriority int, opts ...TaskPriorityOption) error` | 写 | 设置优先级 |
| `GetChildTask(ctx, taskID string, isRecursive bool) ([]*Task, error)` | 读 | 获取子任务 |
| `SetOnTaskSubmitted(callback func())` | 写 | 注册 SUBMITTED 回调 |
| `GetState(ctx) (*TaskManagerState, error)` | 读 | 获取可序列化状态快照 |
| `LoadState(ctx, state *TaskManagerState) error` | 写 | 从快照恢复 |
| `ClearState(ctx) error` | 写 | 清空所有状态 |

#### TaskFilter

```go
type TaskFilter struct {
    TaskID      any     // string 或 []string
    SessionID   string
    UserID      string
    Priority    any     // int 或 "highest"
    Status      TaskStatus
    WithChildren bool
    IsRoot      bool
}
```

校验：至少一个过滤条件非零值。

#### TaskManagerState

```go
type TaskManagerState struct {
    Tasks           map[string]*Task   `json:"tasks"`
    PriorityIndex   map[int][]string   `json:"priority_index"`
    ParentToChildren map[string]map[string]struct{} `json:"parent_to_children"`
    ChildrenToParent map[string]string `json:"children_to_parent"`
    RootTasks       map[string]struct{} `json:"root_tasks"`
}
```

### 4.2 EventQueue (6.20)

#### 数据结构

```go
type EventQueue struct {
    config       *ControllerConfig
    queue        *message_queue.MessageQueueInMemory
    eventHandler EventHandler
}
```

#### 核心方法

| 方法 | 说明 |
|------|------|
| `NewEventQueue(config *ControllerConfig) *EventQueue` | 创建，内部创建 MessageQueueInMemory |
| `SetEventHandler(handler EventHandler)` | 设置事件处理器 |
| `Start()` | 启动 MessageQueueInMemory |
| `Stop(ctx context.Context) error` | 停止 |
| `Subscribe(ctx, agentID, sessionID string) error` | 为 5 种事件类型创建 topic + 订阅，每个 topic 绑定对应 handler 方法 |
| `Unsubscribe(ctx, agentID, sessionID string) error` | 取消 5 种事件订阅 |
| `PublishEvent(ctx, agentID string, sess SessionFacade, event Event) error` | 同步发布：使用 InvokeQueueMessage，等待 handler 处理完成 |
| `PublishEventAsync(ctx, agentID string, sess SessionFacade, event Event) error` | 火忘发布：使用 QueueMessage |

内部方法：
- `buildTopic(agentID, sessionID string, eventType EventType) string` → `"{agentID}_{sessionID}_{eventType}"`

#### 订阅映射

| EventType | Handler 方法 |
|-----------|-------------|
| `EventInput` | `eventHandler.HandleInput` |
| `EventTaskInteraction` | `eventHandler.HandleTaskInteraction` |
| `EventTaskCompletion` | `eventHandler.HandleTaskCompletion` |
| `EventTaskFailed` | `eventHandler.HandleTaskFailed` |
| `EventFollowUp` | `eventHandler.HandleFollowUp` |

### 4.3 EventHandler (6.21)

#### 接口

```go
type EventHandler interface {
    HandleInput(ctx context.Context, input *EventHandlerInput) (map[string]any, error)
    HandleTaskInteraction(ctx context.Context, input *EventHandlerInput) (map[string]any, error)
    HandleTaskCompletion(ctx context.Context, input *EventHandlerInput) (map[string]any, error)
    HandleTaskFailed(ctx context.Context, input *EventHandlerInput) (map[string]any, error)
    HandleFollowUp(ctx context.Context, input *EventHandlerInput) (map[string]any, error)
    GetBase() *EventHandlerBase
    PrepareRound() int
    WaitCompletion(ctx context.Context, timeout time.Duration) map[string]any
    OnAbort()
}
```

#### EventHandlerBase — 依赖容器 + 默认实现

```go
type EventHandlerBase struct {
    Config        *ControllerConfig
    ContextEngine iface.ContextEngine
    TaskManager   *TaskManager
    TaskScheduler *TaskScheduler
    AbilityMgr    *ability.AbilityManager
}
```

默认实现（对齐 Python EventHandler 的非 abstract 方法）：

| 方法 | 默认行为 |
|------|---------|
| `HandleFollowUp` | 返回 `{"status": "not_supported"}` |
| `PrepareRound` | 返回 `0` |
| `WaitCompletion` | 返回 `{"status": "completed"}` |
| `OnAbort` | 空操作 |

#### 依赖注入方式（Embed + Controller.SetEventHandler）

```go
// 具体实现示例
type ReActEventHandler struct {
    EventHandlerBase        // embed，获得所有依赖字段 + 默认方法
    roundID          int
}

func NewReActEventHandler() *ReActEventHandler {
    return &ReActEventHandler{}  // 空壳，依赖后续由 Controller 注入
}

func (h *ReActEventHandler) GetBase() *EventHandlerBase {
    return &h.EventHandlerBase
}

// HandleFollowUp 不需覆写，继承 EventHandlerBase 默认实现
```

```go
// Controller.SetEventHandler — 对齐 Python setter 注入
func (c *Controller) SetEventHandler(handler EventHandler) {
    base := handler.GetBase()
    base.Config = c.config
    base.ContextEngine = c.contextEngine
    base.TaskManager = c.taskManager
    base.TaskScheduler = c.taskScheduler
    base.AbilityMgr = c.abilityMgr
    c.eventHandler = handler
}
```

#### EventHandlerInput

```go
type EventHandlerInput struct {
    Event   Event
    Session sessioninterfaces.SessionFacade
}
```

### 4.4 TaskExecutor (6.21)

#### 接口

```go
type TaskExecutor interface {
    ExecuteAbility(ctx context.Context, taskID string, sess sessioninterfaces.SessionFacade) (<-chan *ControllerOutputChunk, error)
    CanPause(ctx context.Context, taskID string, sess sessioninterfaces.SessionFacade) (bool, string, error)
    Pause(ctx context.Context, taskID string, sess sessioninterfaces.SessionFacade) error
    CanCancel(ctx context.Context, taskID string, sess sessioninterfaces.SessionFacade) (bool, string, error)
    Cancel(ctx context.Context, taskID string, sess sessioninterfaces.SessionFacade) error
}
```

`ExecuteAbility` 返回 `<-chan *ControllerOutputChunk`：
- TaskExecutor 在内部 goroutine 中执行任务，通过 channel 逐个发射 output chunk
- channel 关闭表示执行结束
- 消费方（TaskScheduler）range 遍历 channel 读取

#### TaskExecutorDependencies

```go
type TaskExecutorDependencies struct {
    Config        *ControllerConfig
    AbilityMgr    *ability.AbilityManager
    ContextEngine iface.ContextEngine
    TaskManager   *TaskManager
    EventQueue    *EventQueue
}
```

#### TaskExecutorRegistry

```go
type TaskExecutorRegistry struct {
    builders map[string]func(deps *TaskExecutorDependencies) TaskExecutor
    mu       sync.RWMutex
}
```

- `AddTaskExecutor(taskType string, builder func(deps *TaskExecutorDependencies) TaskExecutor)`
- `RemoveTaskExecutor(taskType string)`
- `GetTaskExecutor(taskType string, deps *TaskExecutorDependencies) (TaskExecutor, error)`

### 4.5 TaskScheduler (6.21)

#### 数据结构

```go
type TaskScheduler struct {
    config             *ControllerConfig
    taskManager        *TaskManager
    contextEngine      iface.ContextEngine
    abilityMgr         *ability.AbilityManager
    eventQueue         *EventQueue
    taskExecutorRegistry *TaskExecutorRegistry
    sessions           map[string]sessioninterfaces.SessionFacade  // sessionID → Session
    card               *agentschema.AgentCard

    runningTasks       map[string]*runningTaskEntry  // taskID → entry
    mu                 sync.Mutex                    // 保护 runningTasks + sessions
    running            atomic.Bool
    notifyCh           chan struct{}                 // 事件驱动唤醒信号
    cancelFunc         context.CancelFunc            // 调度 goroutine 取消
}

type runningTaskEntry struct {
    executor TaskExecutor
    cancel   context.CancelFunc
}
```

#### 核心方法

| 方法 | 说明 |
|------|------|
| `NewTaskScheduler(config, taskManager, contextEngine, abilityMgr, eventQueue, card) *TaskScheduler` | 构造 |
| `Start(ctx) error` | 启动调度 goroutine |
| `Stop(ctx) error` | 停止调度，取消所有运行中任务 |
| `NotifyTaskSubmitted()` | 唤醒调度循环（`notifyCh <- struct{}{}`） |
| `PauseTask(ctx, taskID string) (bool, error)` | 暂停任务：can_pause → pause → cancel goroutine → update status PAUSED |
| `CancelTask(ctx, taskID string) (bool, error)` | 取消任务：SUBMITTED 直接标记 / WORKING → can_cancel → cancel goroutine → update status CANCELED |
| `EnsureSessionCompletionSignal(ctx, sessionID string) error` | 公开方法，所有任务完成时发送完成信号 |

#### schedule 调度循环

```go
func (s *TaskScheduler) schedule(ctx context.Context) {
    for s.running.Load() {
        // 1. 扫描 SUBMITTED 任务
        tasks, _ := s.taskManager.GetTask(ctx, &TaskFilter{Status: TaskSubmitted})

        // 2. 并发启动任务（受 maxConcurrentTasks 限制）
        for _, task := range tasks {
            sess := s.sessions[task.SessionID]
            if sess == nil { continue }
            s.mu.Lock()
            if len(s.runningTasks) >= s.config.MaxConcurrentTasks { s.mu.Unlock(); break }
            if _, exists := s.runningTasks[task.TaskID]; exists { s.mu.Unlock(); continue }
            // 启动执行 goroutine
            go s.executeTaskWrapper(ctx, task.TaskID, sess)
            s.mu.Unlock()
        }

        // 3. 等待唤醒或超时
        select {
        case <-s.notifyCh:  // 有新 SUBMITTED 任务
        case <-time.After(time.Duration(s.config.ScheduleInterval * float64(time.Second))):
        case <-ctx.Done():
            return
        }
    }
}
```

#### executeTask 执行流程

```go
func (s *TaskScheduler) executeTask(ctx context.Context, taskID string, sess SessionFacade) error {
    // 1. 获取 Task
    task := s.taskManager.GetTask(ctx, &TaskFilter{TaskID: taskID})

    // 2. 创建 TaskExecutor
    deps := &TaskExecutorDependencies{...}
    executor := s.taskExecutorRegistry.GetTaskExecutor(task.TaskType, deps)

    // 3. 更新状态 WORKING
    s.taskManager.UpdateTaskStatus(ctx, taskID, TaskWorking)

    // 4. 流式执行
    ch, _ := executor.ExecuteAbility(ctx, taskID, sess)
    for chunk := range ch {
        // 4.1 写入 Session 流
        sess.WriteStream(ctx, chunk)

        // 4.2 根据 payload.type 判断状态
        switch chunk.Payload.Type {
        case string(EventTaskCompletion):
            s.taskManager.UpdateTaskStatus(ctx, taskID, TaskCompleted)
            s.publishTaskEvent(ctx, taskID, sess, chunk)
            return
        case string(EventTaskInteraction):
            s.taskManager.UpdateTaskStatus(ctx, taskID, TaskInputRequired)
            s.publishTaskEvent(ctx, taskID, sess, chunk)
            return
        case string(EventTaskFailed):
            s.taskManager.UpdateTaskStatus(ctx, taskID, TaskFailed)
            s.publishTaskEvent(ctx, taskID, sess, chunk)
            return
        case TaskProcessing:
            // 继续执行
        }
    }
}
```

---

## 五、依赖关系图

```
controller/schema  ←──  controller/config  ←──  controller/modules
     │                        │                       │
     │                        │                       ├── task_manager (依赖 schema + config)
     │                        │                       ├── event_queue  (依赖 schema + config + event_handler + message_queue)
     │                        │                       ├── task_scheduler (依赖 schema + config + task_manager + event_queue + task_executor)
     │                        │                       ├── task_executor (依赖 schema + config + session + ability + context_engine)
     │                        │                       └── event_handler (依赖 schema + config + session + context_engine)
     │                        │
     └── 无外部依赖           └── 依赖 schema

runner/message_queue  ←──  controller/modules/event_queue
```

---

## 六、关键设计决策汇总

| 决策项 | 选择 | 原因 |
|--------|------|------|
| EventQueue 底层 | Go channel + goroutine 封装 MessageQueueInMemory | 需要 topic 路由 + 同步等待 + 订阅管理，裸 channel 不够 |
| MessageQueueInMemory 位置 | `runner/message_queue/` | 对齐 Python 放在 runner/ 下 |
| TaskManager 并发 | `sync.RWMutex` | 读多写少场景，RLock 允许并发读 |
| TaskScheduler 调度 | goroutine + `chan struct{}` 唤醒 | notifyCh 只传信号不传数据，调度循环被事件驱动唤醒 |
| EventHandler 依赖注入 | Embed EventHandlerBase + Controller.SetEventHandler | 对齐 Python setter 模式，解决 Controller↔EventHandler 循环依赖 |
| TaskExecutor.ExecuteAbility 返回 | `<-chan *ControllerOutputChunk` | 自然支持流式输出和取消，与 Go LLM Stream 模式一致 |
| ControllerOutputChunk | embed `stream.OutputSchema` + 扩展字段 | 可直接写入 Session 流（实现 Schema 接口），无需转换 |
| Schema 类型补全 | 随 6.20+6.21 一起补到 `controller/schema/` | 按需补充，不单独做全量 schema 移植 |
| ControllerConfig 位置 | `controller/config/` | 独立子包，与 schema 分离（配置 vs 数据模型） |
| 文件组织 | `controller/modules/` 对齐 Python | 与 Python `controller/modules/` 目录结构一致 |
| Task.inputs 类型 | `[]Event` 接口切片 + 自定义 JSON 序列化 | 类型安全，对齐 Python List[Event]，按 event_type 分发反序列化 |
| DataFrame | 新建 `controller/schema/dataframe.go` | Go 侧无等价类型，ContentPart 是 LLM 消息专用 |

---

## 七、测试策略

- 所有组件使用可 mock 的依赖（SessionFacade 接口、ContextEngine 接口、AbilityManager 等）
- MessageQueueInMemory：测试 Produce/Subscribe/Unsubscribe/同步等待/火忘/超时
- TaskManager：测试 CRUD / 状态更新 / 优先级索引 / 父子层级 / 状态持久化 / 并发安全
- EventQueue：测试订阅/取消/同步发布/火忘发布/topic 路由
- TaskScheduler：测试调度循环/并发执行/暂停/取消/完成信号/超时
- TaskExecutor：接口测试（使用 fake 实现）
- EventHandler：接口测试 + EventHandlerBase 默认方法测试
- 覆盖率目标 ≥ 85%
