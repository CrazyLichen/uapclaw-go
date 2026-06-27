# 6.19 Controller 设计文档

## 概述

实现 `Controller` 主结构体——事件驱动任务编排的核心组件，对应 Python `openjiuwen/core/controller/base.py`。
同时回填 `modules/intent_recognizer.py` 和 `modules/intent_toolkits.py` 在 Go 中的缺失实现。

## 流程位置

```
用户输入
  ↓
Runner (6.25) → ControllerAgent (6.22)
  ↓
┌──────────── Controller (6.19) ────────────┐  ← 当前步骤
│  1. ensureStarted()  懒启动               │
│  2. restoreTaskManagerState()  恢复状态    │
│  3. eventQueue.Subscribe()  订阅事件       │
│  4. eventQueue.PublishEvent()  发布输入    │
│  5. TaskScheduler.executeTask()  执行任务  │
│  6. session.StreamIterator()  流式读取     │
│  7. saveTaskManagerState()  保存状态       │
│  8. eventQueue.Unsubscribe()  取消订阅     │
└────────────────────────────────────────────┘
  ↓
TaskManager (6.20) + EventQueue (6.20) + TaskScheduler (6.21) + EventHandler (6.21)
  ↓
TaskExecutor → Ability → LLM/Tool 调用
  ↓
ControllerOutputChunk 流式输出
```

## 设计决策

### 1. 事件循环检测 → sync.Once + atomic.Bool

Python `_ensure_started()` 检测 asyncio 事件循环变化并重建组件。Go 中 goroutine 由 runtime 统一调度，不依赖全局事件循环，无需此机制。

采用 `sync.Once` 保证 `Start()` 只执行一次，`atomic.Bool` 跟踪运行状态。`Stop()` 后可重新 `Start()`。

### 2. 两阶段初始化

`NewController()` 返回空壳（所有字段零值），`Init(card, config, abilityMgr, contextEngine)` 真正创建子组件并接线。
与 Python 的 `__init__` + `init()` 模式 1:1 对照。

### 3. 首帧超时

`Stream()` 内部用 `select` + `time.After(timeout)` 等待第一个 chunk，超时则返回错误。
与 Python 的 `asyncio.wait_for(stream_iter.__anext__(), timeout=first_frame_timeout)` 1:1 对照。

### 4. Stream 返回 channel

`Stream()` 返回 `<-chan *schema.ControllerOutputChunk`，内部启动 goroutine 执行完整流程，
goroutine 结束时 `close(out)`。与项目中 `TaskExecutor.ExecuteAbility()` 的 channel 模式一致。

### 5. 意图识别组件

- **IntentToolkits**：完整实现（不依赖 LLM）
- **IntentRecognizer**：骨架实现，LLM 调用部分用 `ModelProvider` 接口占位，标注 `⤵️ 6.23`
- **EventHandlerWithIntentRecognition**：完整实现（8 个 `_processXxxIntent` 方法）

### 6. Session 依赖

Controller 直接使用 `*session.Session`，可调用 `StreamIterator()`。
传给 modules（TaskScheduler/EventQueue/EventHandler）时隐式转 `SessionFacade`。
与 Python 直接使用 `AgentSession` 具体类型 1:1 对照。

### 7. all_tasks_processed 完成信号

`Stream()` 内部从 session stream 读取 chunk 时：
- `chunk.Payload.Type == "all_tasks_processed"` → break（流结束信号，不发送给调用方）
- 其他类型 → 发送给调用方

与 Python 的 yield + break 逻辑 1:1 对照。

### 8. config 级联传播

`SetConfig(cfg)` 设置配置时，级联传播到 `taskManager`/`eventQueue`/`taskScheduler`/`eventHandler`。

## 文件清单

| # | 文件路径 | 操作 | 对应 Python |
|---|---------|------|------------|
| 1 | `controller/controller.go` | 新建 | `controller/base.py` |
| 2 | `controller/doc.go` | 新建 | `controller/__init__.py` |
| 3 | `controller/modules/intent_recognizer.go` | 新建 | `modules/intent_recognizer.py` |
| 4 | `controller/modules/intent_toolkits.go` | 新建 | `modules/intent_toolkits.py` |
| 5 | `controller/modules/doc.go` | 修改 | `modules/__init__.py` |
| 6 | `controller/controller_test.go` | 新建 | — |
| 7 | `controller/modules/intent_recognizer_test.go` | 新建 | — |
| 8 | `controller/modules/intent_toolkits_test.go` | 新建 | — |

## Controller 结构体

```go
type Controller struct {
    // Init 后设置的字段
    card          *agentschema.AgentCard
    abilityMgr    *ability.AbilityManager
    config        *config.ControllerConfig
    contextEngine iface.ContextEngine

    // Init 中创建的子组件
    taskManager   *modules.TaskManager
    eventQueue    *modules.EventQueue
    taskScheduler *modules.TaskScheduler
    eventHandler  modules.EventHandler   // 接口类型

    // 生命周期状态
    started       atomic.Bool
    startOnce     sync.Once
    mu            sync.RWMutex
}
```

## Controller 方法清单

### 构造与初始化

| Go 方法 | 签名 | 对应 Python |
|---------|------|------------|
| `NewController` | `func NewController() *Controller` | `__init__` |
| `Init` | `func (c *Controller) Init(card, config, abilityMgr, contextEngine)` | `init` |

### 生命周期

| Go 方法 | 签名 | 对应 Python |
|---------|------|------------|
| `Start` | `func (c *Controller) Start(ctx context.Context) error` | `start` |
| `Stop` | `func (c *Controller) Stop(ctx context.Context) error` | `stop` |
| `ensureStarted` | `func (c *Controller) ensureStarted(ctx context.Context) error` | `_ensure_started` |

### 会话管理

| Go 方法 | 签名 | 对应 Python |
|---------|------|------------|
| `BindSession` | `func (c *Controller) BindSession(ctx, session) error` | `bind_session` |
| `UnbindSession` | `func (c *Controller) UnbindSession(ctx, session) error` | `unbind_session` |

### 执行

| Go 方法 | 签名 | 对应 Python |
|---------|------|------------|
| `Invoke` | `func (c *Controller) Invoke(ctx, inputs, session) (*schema.ControllerOutput, error)` | `invoke` |
| `Stream` | `func (c *Controller) Stream(ctx, inputs, session, streamModes) <-chan *schema.ControllerOutputChunk` | `stream` |

### 状态持久化

| Go 方法 | 签名 | 对应 Python |
|---------|------|------------|
| `restoreTaskManagerState` | `func (c *Controller) restoreTaskManagerState(ctx, session) bool` | `_restore_task_manager_state` |
| `saveTaskManagerState` | `func (c *Controller) saveTaskManagerState(ctx, session) error` | `_save_task_manager_state` |

### TaskExecutor 注册

| Go 方法 | 签名 | 对应 Python |
|---------|------|------------|
| `AddTaskExecutor` | `func (c *Controller) AddTaskExecutor(taskType, builder) *Controller` | `add_task_executor` |
| `RemoveTaskExecutor` | `func (c *Controller) RemoveTaskExecutor(taskType)` | `remove_task_executor` |
| `GetTaskExecutor` | `func (c *Controller) GetTaskExecutor(cfg, abilityMgr, contextEngine, taskManager) (modules.TaskExecutor, error)` | `get_task_executor` |

### 事件发布

| Go 方法 | 签名 | 对应 Python |
|---------|------|------------|
| `PublishEventAsync` | `func (c *Controller) PublishEventAsync(ctx, session, event) error` | `publish_event_async` |

### EventHandler 注入

| Go 方法 | 签名 | 对应 Python |
|---------|------|------------|
| `SetEventHandler` | `func (c *Controller) SetEventHandler(handler modules.EventHandler)` | `set_event_handler` |

### 属性访问（Getter/Setter）

| Go 方法 | 对应 Python property |
|---------|---------------------|
| `Config() / SetConfig(cfg)` | `config` getter/setter（级联传播） |
| `EventQueue()` | `event_queue` |
| `TaskManager()` | `task_manager` |
| `TaskScheduler()` | `task_scheduler` |
| `EventHandler()` | `event_handler` |
| `ContextEngine() / SetContextEngine()` | `context_engine` getter/setter |
| `AbilityManager() / SetAbilityManager()` | `ability_manager` getter/setter |

## IntentToolkits 设计

对照 Python `intent_toolkits.py`，完整实现。

### 结构体

```go
type IntentToolkits struct {
    event               schema.Event
    confidenceThreshold float64
    toolSchemaChoices   map[string]map[string]any
}
```

### 方法

| Go 方法 | 对应 Python |
|---------|------------|
| `NewIntentToolkits(event, confidenceThreshold)` | `__init__` |
| `CreateTask(confidence, taskDesc) (*Intent, string, error)` | `create_task` |
| `PauseTask(confidence, taskID) (*Intent, string, error)` | `pause_task` |
| `CancelTask(confidence, taskID) (*Intent, string, error)` | `cancel_task` |
| `ResumeTask(confidence, taskID) (*Intent, string, error)` | `resume_task` |
| `UnknownTask(confidence, question) (*Intent, string, error)` | `unknown_task` |
| `CreateDependentTask(confidence, desc, ids) (*Intent, string, error)` | `create_dependent_task` |
| `ModifyTask(confidence, taskID, newDesc) (*Intent, string, error)` | `modify_task` |
| `SupplementTask(confidence, taskID, info) (*Intent, string, error)` | `supplement_task` |
| `GetOpenAIToolSchemas(choices) []map[string]any` | `get_openai_tool_schemas` |
| `lowConfidenceIntent(confidence) (*Intent, string)` | `_low_confidence_intent` |

### Tool Schema 定义

8 个 OpenAI Tool Schema（create_task / pause_task / cancel_task / resume_task / unknown_task / create_dependent_task / modify_task / supplement_task），1:1 对照 Python。

## IntentRecognizer 设计（骨架 + 接口占位）

### ModelProvider 接口

```go
// ModelProvider 意图识别所需的模型调用接口
// ⤵️ 6.23 ResourceMgr 实现后回填
type ModelProvider interface {
    Invoke(ctx context.Context, messages []Message, tools []map[string]any) (*ModelResponse, error)
}
```

### 结构体

```go
type IntentRecognizer struct {
    config        *config.ControllerConfig
    taskManager   *modules.TaskManager
    abilityMgr    *ability.AbilityManager
    contextEngine iface.ContextEngine
    modelProvider ModelProvider  // ⤵️ 6.23 回填

    systemMessage      // 系统提示词
    userPromptTemplate // 用户提示词模板
}
```

### 方法

| Go 方法 | 说明 | 状态 |
|---------|------|------|
| `NewIntentRecognizer(config, taskManager, contextEngine)` | 构造 | 完整 |
| `Recognize(ctx, event, session) ([]*Intent, error)` | 识别意图 | 骨架，LLM 调用标注 ⤵️ 6.23 |
| `prepareUserMessage(ctx, query) (UserMessage, error)` | 构建用户消息 | 完整 |

## EventHandlerWithIntentRecognition 设计

对照 Python `intent_recognizer.py` 中的 `EventHandlerWithIntentRecognition` 类，完整实现。

### 结构体

```go
type EventHandlerWithIntentRecognition struct {
    modules.EventHandlerBase
    recognizer *IntentRecognizer
}
```

### 方法

| Go 方法 | 对应 Python | 说明 |
|---------|------------|------|
| `NewEventHandlerWithIntentRecognition()` | `__init__` | 构造 |
| `HandleInput(ctx, inputs) (map[string]any, error)` | `handle_input` | 识别意图 → errgroup 并发分发 |
| `HandleTaskInteraction(ctx, inputs) (map[string]any, error)` | `handle_task_interaction` | 将 interaction 写入 stream |
| `HandleTaskCompletion(ctx, inputs) (map[string]any, error)` | `handle_task_completion` | 将 result 写入 stream |
| `HandleTaskFailed(ctx, inputs) (map[string]any, error)` | `handle_task_failed` | 将 error 写入 stream |
| `processCreateTaskIntent(ctx, intent, session) error` | `_process_create_task_intent` | 创建 Task |
| `processPauseTaskIntent(ctx, intent, session) error` | `_process_pause_task_intent` | 暂停 Task |
| `processResumeTaskIntent(ctx, intent, session) error` | `_process_resume_task_intent` | 恢复 Task |
| `processContinueTaskIntent(ctx, intent, session) error` | `_process_continue_task_intent` | 接续 Task |
| `processSupplementTaskIntent(ctx, intent, session) error` | `_process_supplement_task_intent` | 补充 Task |
| `processCancelTaskIntent(ctx, intent, session) error` | `_process_cancel_task_intent` | 取消 Task |
| `processModifyTaskIntent(ctx, intent, session) error` | `_process_modify_task_intent` | 修改 Task |
| `processUnknownTaskIntent(ctx, intent, session) error` | `_process_unknown_task_intent` | 未知意图 |

## Stream() 内部流程

```
Stream(ctx, inputs, session, streamModes) → (<-chan *ControllerOutputChunk, <-chan error)
  │
  ├─ 创建 out = make(chan *ControllerOutputChunk)
  ├─ 启动 goroutine:
  │   ├─ ensureStarted(ctx)
  │   ├─ restoreTaskManagerState(ctx, session)
  │   ├─ taskScheduler.Sessions()[sessionID] = session
  │   ├─ eventQueue.Subscribe(agentID, sessionID)
  │   ├─ eventQueue.PublishEvent(agentID, session, inputs)
  │   ├─ taskScheduler.EnsureSessionCompletionSignal(sessionID)
  │   │
  │   ├─ iter := session.StreamIterator()
  │   ├─ select { case chunk <- iter: ... case <-time.After(timeout): error }
  │   │   └─ 首帧超时保护
  │   │
  │   ├─ for chunk := range iter:
  │   │   ├─ chunk.Payload.Type == "all_tasks_processed" → break
  │   │   └─ out <- chunk
  │   │
  │   └─ finally:
  │       ├─ saveTaskManagerState(ctx, session)
  │       ├─ eventQueue.Unsubscribe(agentID, sessionID)
  │       ├─ delete(taskScheduler.Sessions(), sessionID)
  │       └─ close(out)
  │
  └─ return out
```

## Invoke() 内部流程

```
Invoke(ctx, inputs, session) → (*ControllerOutput, error)
  │
  ├─ ch := Stream(ctx, inputs, session, []StreamMode{StreamModeOutput})
  ├─ for chunk := range ch:
  │   └─ chunks = append(chunks, chunk)
  │
  └─ return &ControllerOutput{Type: EventTaskCompletion, Data: chunks}, nil
```

## 回填标注

| 位置 | 标注 | 回填来源 |
|------|------|---------|
| IntentRecognizer.modelProvider 字段 | `⤵️ 6.23 ResourceMgr 实现后回填` | 6.23 |
| IntentRecognizer.Recognize() LLM 调用 | `⤵️ 6.23 ResourceMgr 实现后回填` | 6.23 |
| IMPLEMENTATION_PLAN.md 6.19 状态 | `☐` → `🔄` → `✅` | — |
| IMPLEMENTATION_PLAN.md 6.20/6.21 产出描述 | 补充 intent_recognizer/intent_toolkits | — |

## 测试策略

### controller.go 测试

- `TestNewController` — 空壳构造
- `TestController_Init` — 两阶段初始化，子组件创建 + 接线验证
- `TestController_StartStop` — 启动/停止生命周期
- `TestController_ensureStarted` — 懒启动（多次调用只启动一次）
- `TestController_SetConfig` — 级联传播验证
- `TestController_SetEventHandler` — EventHandler 注入 + 依赖接线
- `TestController_AddRemoveTaskExecutor` — TaskExecutor 注册/移除
- `TestController_BindUnbindSession` — 会话绑定/解绑
- `TestController_Invoke` — 批量执行
- `TestController_Stream_正常流` — 流式执行，验证 chunk 输出
- `TestController_Stream_首帧超时` — 首帧超时返回错误
- `TestController_Stream_allTasksProcessed过滤` — 完成信号被过滤
- `TestController_restoreTaskManagerState` — 状态恢复（成功/失败/无状态）
- `TestController_saveTaskManagerState` — 状态保存（启用/禁用持久化）

### intent_toolkits.go 测试

- `TestNewIntentToolkits` — 构造
- `TestIntentToolkits_CreateTask` — 各置信度分支
- `TestIntentToolkits_PauseTask` — 各置信度分支
- 其余 8 个 tool 方法类似
- `TestIntentToolkits_GetOpenAIToolSchemas` — Schema 生成
- `TestIntentToolkits_lowConfidenceIntent` — 低置信度转 unknown

### intent_recognizer.go 测试

- `TestNewIntentRecognizer` — 构造
- `TestIntentRecognizer_Recognize_骨架` — 验证接口占位
- `TestEventHandlerWithIntentRecognition_HandleInput` — 意图路由分发
- `TestEventHandlerWithIntentRecognition_HandleTaskInteraction` — 交互事件处理
- `TestEventHandlerWithIntentRecognition_HandleTaskCompletion` — 完成事件处理
- `TestEventHandlerWithIntentRecognition_HandleTaskFailed` — 失败事件处理
- `TestEventHandlerWithIntentRecognition_processXxxIntent` — 各意图处理方法
