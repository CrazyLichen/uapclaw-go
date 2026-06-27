# 6.19 Controller 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 Controller 主结构体（事件驱动任务编排）+ 回填 IntentToolkits 和 IntentRecognizer/EventHandlerWithIntentRecognition

**Architecture:** Controller 是 ControllerAgent 的核心编排组件，组合 TaskManager/EventQueue/TaskScheduler/EventHandler 四大子模块，提供两阶段初始化、懒启动、会话绑定/解绑、流式/批量执行、状态持久化等能力。IntentToolkits 完整实现，IntentRecognizer 骨架 + ModelProvider 接口占位（⤵️ 6.23 回填），EventHandlerWithIntentRecognition 完整实现。

**Tech Stack:** Go 1.22+, sync.Once/atomic.Bool 生命周期管理, channel 流式输出, select+time.After 首帧超时, zerolog 结构化日志

---

## 文件结构

| 操作 | 文件路径 | 职责 |
|------|---------|------|
| 新建 | `internal/agentcore/controller/controller.go` | Controller 主结构体 + 全部方法 |
| 新建 | `internal/agentcore/controller/doc.go` | 包文档 + 导出 |
| 新建 | `internal/agentcore/controller/modules/intent_toolkits.go` | IntentToolkits 完整实现 |
| 新建 | `internal/agentcore/controller/modules/intent_recognizer.go` | IntentRecognizer 骨架 + EventHandlerWithIntentRecognition |
| 修改 | `internal/agentcore/controller/modules/doc.go` | 补充 intent 文件条目 |
| 新建 | `internal/agentcore/controller/controller_test.go` | Controller 单元测试 |
| 新建 | `internal/agentcore/controller/modules/intent_toolkits_test.go` | IntentToolkits 单元测试 |
| 新建 | `internal/agentcore/controller/modules/intent_recognizer_test.go` | IntentRecognizer + EventHandlerWithIntentRecognition 测试 |
| 修改 | `IMPLEMENTATION_PLAN.md` | 6.19 状态更新 + 6.20/6.21 产出补充 |

---

### Task 1: IntentToolkits 实现

**Files:**
- Create: `internal/agentcore/controller/modules/intent_toolkits.go`
- Test: `internal/agentcore/controller/modules/intent_toolkits_test.go`
- 对应 Python: `openjiuwen/core/controller/modules/intent_toolkits.py`

- [ ] **Step 1: 创建 intent_toolkits.go — 结构体 + 构造函数 + Tool Schema 定义**

```go
//go:build !test

package modules

// ──────────────────────────── 结构体 ────────────────────────────

// IntentToolkits 意图识别工具集，提供 8 个 OpenAI Tool Schema 和对应方法。
// 对应 Python: openjiuwen/core/controller/modules/intent_toolkits.py
type IntentToolkits struct {
    // event 关联的输入事件
    event schema.Event
    // confidenceThreshold 置信度阈值
    confidenceThreshold float64
    // toolSchemaChoices 工具 Schema 映射：tool name → OpenAI tool schema
    toolSchemaChoices map[string]map[string]any
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewIntentToolkits 创建意图工具集
func NewIntentToolkits(event schema.Event, confidenceThreshold float64) *IntentToolkits {
    t := &IntentToolkits{
        event:               event,
        confidenceThreshold: confidenceThreshold,
    }
    t.initToolSchemaChoices()
    return t
}
```

`initToolSchemaChoices()` 初始化 8 个 Tool Schema（create_task / pause_task / cancel_task / resume_task / unknown_task / create_dependent_task / modify_task / supplement_task），1:1 对照 Python `_tool_schema_choices` 字典。

- [ ] **Step 2: 实现 8 个 tool 方法 + lowConfidenceIntent 辅助方法**

对照 Python 逐个实现：

```go
// CreateTask 创建任务意图
func (t *IntentToolkits) CreateTask(confidence float64, taskDescription string) (*schema.Intent, string, error)

// PauseTask 暂停任务意图
func (t *IntentToolkits) PauseTask(confidence float64, taskID string) (*schema.Intent, string, error)

// CancelTask 取消任务意图
func (t *IntentToolkits) CancelTask(confidence float64, taskID string) (*schema.Intent, string, error)

// ResumeTask 恢复任务意图
func (t *IntentToolkits) ResumeTask(confidence float64, taskID string) (*schema.Intent, string, error)

// UnknownTask 未知任务意图
func (t *IntentToolkits) UnknownTask(confidence float64, questionForUser string) (*schema.Intent, string, error)

// CreateDependentTask 创建依赖任务意图
func (t *IntentToolkits) CreateDependentTask(confidence float64, taskDescription string, dependentTaskIDs []string) (*schema.Intent, string, error)

// ModifyTask 修改任务意图
func (t *IntentToolkits) ModifyTask(confidence float64, taskID string, newTaskDescription string) (*schema.Intent, string, error)

// SupplementTask 补充任务意图
func (t *IntentToolkits) SupplementTask(confidence float64, taskID string, supplementInfo string) (*schema.Intent, string, error)

// GetOpenAIToolSchemas 获取 OpenAI Tool Schema 列表
func (t *IntentToolkits) GetOpenAIToolSchemas(choices ...string) []map[string]any

// lowConfidenceIntent 低置信度自动转 unknown_task
func (t *IntentToolkits) lowConfidenceIntent(confidence float64) (*schema.Intent, string)
```

每个方法逻辑：`confidence < threshold` → `lowConfidenceIntent`，否则构造对应 `Intent` + 返回描述字符串。

- [ ] **Step 3: 编写 intent_toolkits_test.go 测试**

覆盖：构造函数、8 个 tool 方法（正常分支 + 低置信度分支）、GetOpenAIToolSchemas（全量 + 过滤）、lowConfidenceIntent。

- [ ] **Step 4: 运行测试确认通过**

```bash
export GOPROXY=https://goproxy.cn,direct
go test ./internal/agentcore/controller/modules/ -run TestIntentToolkits -v
```

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/controller/modules/intent_toolkits.go internal/agentcore/controller/modules/intent_toolkits_test.go
git commit -m "feat(controller): 实现 IntentToolkits — 8 个意图工具 + OpenAI Tool Schema"
```

---

### Task 2: IntentRecognizer 骨架 + EventHandlerWithIntentRecognition 实现

**Files:**
- Create: `internal/agentcore/controller/modules/intent_recognizer.go`
- Test: `internal/agentcore/controller/modules/intent_recognizer_test.go`
- 对应 Python: `openjiuwen/core/controller/modules/intent_recognizer.py`

- [ ] **Step 1: 创建 intent_recognizer.go — ModelProvider 接口 + IntentRecognizer 骨架**

```go
//go:build !test

package modules

// ──────────────────────────── 接口 ────────────────────────────

// ModelProvider 意图识别所需的模型调用接口
// ⤵️ 6.23 ResourceMgr 实现后回填
type ModelProvider interface {
    // Invoke 调用模型，messages 为消息列表，tools 为工具 Schema
    // 返回模型响应
    Invoke(ctx context.Context, messages []any, tools []map[string]any) (any, error)
}

// ──────────────────────────── 结构体 ────────────────────────────

// IntentRecognizer 意图识别器，负责识别用户输入中的意图。
// 对应 Python: openjiuwen/core/controller/modules/intent_recognizer.py::IntentRecognizer
type IntentRecognizer struct {
    // config 控制器配置
    config *config.ControllerConfig
    // taskManager 任务管理器
    taskManager *TaskManager
    // abilityMgr 能力管理器
    abilityMgr *ability.AbilityManager
    // contextEngine 上下文引擎
    contextEngine iface.ContextEngine
    // modelProvider 模型提供者
    // ⤵️ 6.23 ResourceMgr 实现后回填
    modelProvider ModelProvider
    // systemMessage 系统提示词
    systemMessage string
    // userPromptTemplate 用户提示词模板
    userPromptTemplate string
}

// EventHandlerWithIntentRecognition 基于意图识别的事件处理器
// 对应 Python: openjiuwen/core/controller/modules/intent_recognizer.py::EventHandlerWithIntentRecognition
type EventHandlerWithIntentRecognition struct {
    EventHandlerBase
    // recognizer 意图识别器
    recognizer *IntentRecognizer
}
```

- [ ] **Step 2: 实现 IntentRecognizer 构造 + Recognize 骨架 + prepareUserMessage**

```go
// NewIntentRecognizer 创建意图识别器
func NewIntentRecognizer(cfg *config.ControllerConfig, taskManager *TaskManager, abilityMgr *ability.AbilityManager, contextEngine iface.ContextEngine) *IntentRecognizer

// Recognize 识别意图
// ⤵️ 6.23 ResourceMgr 实现后回填 LLM 调用逻辑
func (r *IntentRecognizer) Recognize(ctx context.Context, event schema.Event, sess sessioninterfaces.SessionFacade) ([]*schema.Intent, error)

// prepareUserMessage 构建用户消息
func (r *IntentRecognizer) prepareUserMessage(ctx context.Context, query string) (string, error)

// SetModelProvider 设置模型提供者（⤵️ 6.23 回填时调用）
func (r *IntentRecognizer) SetModelProvider(provider ModelProvider)
```

`Recognize()` 当前返回 `nil, nil` + 日志标注 `⤵️ 6.23`。`prepareUserMessage()` 完整实现（查询当前任务 + 格式化提示词）。

- [ ] **Step 3: 实现 EventHandlerWithIntentRecognition 全部方法**

对照 Python 逐个实现：

```go
// NewEventHandlerWithIntentRecognition 创建基于意图识别的事件处理器
func NewEventHandlerWithIntentRecognition() *EventHandlerWithIntentRecognition

// HandleInput 处理输入事件，识别意图并分发
func (h *EventHandlerWithIntentRecognition) HandleInput(ctx context.Context, input *EventHandlerInput) (map[string]any, error)

// HandleTaskInteraction 处理任务交互事件
func (h *EventHandlerWithIntentRecognition) HandleTaskInteraction(ctx context.Context, input *EventHandlerInput) (map[string]any, error)

// HandleTaskCompletion 处理任务完成事件
func (h *EventHandlerWithIntentRecognition) HandleTaskCompletion(ctx context.Context, input *EventHandlerInput) (map[string]any, error)

// HandleTaskFailed 处理任务失败事件
func (h *EventHandlerWithIntentRecognition) HandleTaskFailed(ctx context.Context, input *EventHandlerInput) (map[string]any, error)

// processCreateTaskIntent 处理创建任务意图
func (h *EventHandlerWithIntentRecognition) processCreateTaskIntent(ctx context.Context, intent *schema.Intent, sess sessioninterfaces.SessionFacade) error

// processPauseTaskIntent 处理暂停任务意图
func (h *EventHandlerWithIntentRecognition) processPauseTaskIntent(ctx context.Context, intent *schema.Intent, sess sessioninterfaces.SessionFacade) error

// processResumeTaskIntent 处理恢复任务意图
func (h *EventHandlerWithIntentRecognition) processResumeTaskIntent(ctx context.Context, intent *schema.Intent, sess sessioninterfaces.SessionFacade) error

// processContinueTaskIntent 处理接续任务意图
func (h *EventHandlerWithIntentRecognition) processContinueTaskIntent(ctx context.Context, intent *schema.Intent, sess sessioninterfaces.SessionFacade) error

// processSupplementTaskIntent 处理补充任务意图
func (h *EventHandlerWithIntentRecognition) processSupplementTaskIntent(ctx context.Context, intent *schema.Intent, sess sessioninterfaces.SessionFacade) error

// processCancelTaskIntent 处理取消任务意图
func (h *EventHandlerWithIntentRecognition) processCancelTaskIntent(ctx context.Context, intent *schema.Intent, sess sessioninterfaces.SessionFacade) error

// processModifyTaskIntent 处理修改任务意图
func (h *EventHandlerWithIntentRecognition) processModifyTaskIntent(ctx context.Context, intent *schema.Intent, sess sessioninterfaces.SessionFacade) error

// processUnknownTaskIntent 处理未知任务意图
func (h *EventHandlerWithIntentRecognition) processUnknownTaskIntent(ctx context.Context, intent *schema.Intent, sess sessioninterfaces.SessionFacade) error
```

- [ ] **Step 4: 编写 intent_recognizer_test.go 测试**

覆盖：IntentRecognizer 构造、prepareUserMessage、Recognize 骨架返回；EventHandlerWithIntentRecognition 构造、HandleTaskInteraction/HandleTaskCompletion/HandleTaskFailed（验证 WriteStream 调用）、processXxxIntent 各方法。

对于 HandleInput 测试：构造 mock IntentRecognizer 返回预设 intents，验证分发逻辑正确。

- [ ] **Step 5: 运行测试确认通过**

```bash
export GOPROXY=https://goproxy.cn,direct
go test ./internal/agentcore/controller/modules/ -run "TestIntentRecognizer|TestEventHandlerWithIntentRecognition" -v
```

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/controller/modules/intent_recognizer.go internal/agentcore/controller/modules/intent_recognizer_test.go
git commit -m "feat(controller): 实现 IntentRecognizer 骨架 + EventHandlerWithIntentRecognition"
```

---

### Task 3: 修改 modules/doc.go — 补充 intent 文件条目

**Files:**
- Modify: `internal/agentcore/controller/modules/doc.go`

- [ ] **Step 1: 在 doc.go 的文件目录中添加 intent_recognizer.go 和 intent_toolkits.go 条目**

在现有文件列表中按正确位置插入：
```
├── intent_recognizer.go  # IntentRecognizer 骨架 + EventHandlerWithIntentRecognition
├── intent_toolkits.go    # IntentToolkits 意图工具集
```

- [ ] **Step 2: 提交**

```bash
git add internal/agentcore/controller/modules/doc.go
git commit -m "docs(controller): modules/doc.go 补充 intent 文件条目"
```

---

### Task 4: Controller 主结构体实现

**Files:**
- Create: `internal/agentcore/controller/controller.go`
- 对应 Python: `openjiuwen/core/controller/base.py`

- [ ] **Step 1: 创建 controller.go — 结构体 + NewController + Init**

```go
//go:build !test

package controller

import (
    "context"
    "sync"
    "sync/atomic"
    "time"

    ability "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/ability"
    agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
    "github.com/uapclaw/uapclaw-go/internal/agentcore/controller/config"
    "github.com/uapclaw/uapclaw-go/internal/agentcore/controller/modules"
    "github.com/uapclaw/uapclaw-go/internal/agentcore/controller/schema"
    iface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
    "github.com/uapclaw/uapclaw-go/internal/agentcore/session"
    "github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
    sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
    "github.com/uapclaw/uapclaw-go/internal/common/exception"
    "github.com/uapclaw/uapclaw-go/internal/common/logger"
)

const logComponent = logger.ComponentAgentCore

// ──────────────────────────── 结构体 ────────────────────────────

// Controller 事件驱动任务编排控制器。
// 它是 ControllerAgent 的核心组件，负责处理事件、管理任务生命周期、
// 执行意图识别和处理。
// 对应 Python: openjiuwen/core/controller/base.py::Controller
type Controller struct {
    // card Agent 身份元数据
    card *agentschema.AgentCard
    // abilityMgr 能力管理器
    abilityMgr *ability.AbilityManager
    // config 控制器配置
    config *config.ControllerConfig
    // contextEngine 上下文引擎
    contextEngine iface.ContextEngine

    // taskManager 任务管理器（Init 中创建）
    taskManager *modules.TaskManager
    // eventQueue 事件队列（Init 中创建）
    eventQueue *modules.EventQueue
    // taskScheduler 任务调度器（Init 中创建）
    taskScheduler *modules.TaskScheduler
    // eventHandler 事件处理器
    eventHandler modules.EventHandler

    // started 运行状态标记
    started atomic.Bool
    // mu 保护 Start/Stop 并发
    mu sync.RWMutex
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewController 创建空壳 Controller。
// 必须随后调用 Init() 完成初始化。
// 对应 Python: Controller.__init__()
func NewController() *Controller {
    return &Controller{}
}

// Init 两阶段初始化，创建子组件并接线。
// 对应 Python: Controller.init(card, config, ability_manager, context_engine)
func (c *Controller) Init(
    card *agentschema.AgentCard,
    cfg *config.ControllerConfig,
    abilityMgr *ability.AbilityManager,
    contextEngine iface.ContextEngine,
) {
    c.card = card
    c.config = cfg
    c.abilityMgr = abilityMgr
    c.contextEngine = contextEngine

    c.taskManager = modules.NewTaskManager(cfg)
    c.eventQueue = modules.NewEventQueue(cfg)
    c.taskScheduler = modules.NewTaskScheduler(
        cfg,
        c.taskManager,
        contextEngine,  // any 类型，避免循环依赖
        abilityMgr,
        c.eventQueue,
        card,            // any 类型，避免循环依赖
    )

    // 接线：TaskManager 的 onTaskSubmitted 回调 → TaskScheduler.NotifyTaskSubmitted
    c.taskManager.SetOnTaskSubmitted(c.taskScheduler.NotifyTaskSubmitted)
}
```

- [ ] **Step 2: 实现生命周期方法 Start/Stop/ensureStarted**

```go
// Start 启动控制器（EventQueue + TaskScheduler）。
// 对应 Python: Controller.start()
func (c *Controller) Start(ctx context.Context) error {
    c.mu.Lock()
    defer c.mu.Unlock()
    if c.started.Load() {
        return nil
    }
    c.eventQueue.Start()
    if err := c.taskScheduler.Start(ctx); err != nil {
        return err
    }
    c.started.Store(true)
    logger.Info(logComponent).Msg("Controller 已启动")
    return nil
}

// Stop 停止控制器（TaskScheduler + EventQueue）。
// 对应 Python: Controller.stop()
func (c *Controller) Stop(ctx context.Context) error {
    c.mu.Lock()
    defer c.mu.Unlock()
    if !c.started.Load() {
        return nil
    }
    if err := c.taskScheduler.Stop(ctx); err != nil {
        return err
    }
    if err := c.eventQueue.Stop(ctx); err != nil {
        return err
    }
    c.started.Store(false)
    logger.Info(logComponent).Msg("Controller 已停止")
    return nil
}

// ensureStarted 懒启动，确保 EventQueue 和 TaskScheduler 已运行。
// 对应 Python: Controller._ensure_started()
// Go 中无需事件循环检测，简化为首次启动检查。
func (c *Controller) ensureStarted(ctx context.Context) error {
    if c.started.Load() {
        return nil
    }
    return c.Start(ctx)
}
```

- [ ] **Step 3: 实现属性访问方法（Getter/Setter）**

```go
// Config 获取控制器配置
func (c *Controller) Config() *config.ControllerConfig

// SetConfig 设置控制器配置，级联传播到所有子组件。
// 对应 Python: Controller.config.setter
func (c *Controller) SetConfig(cfg *config.ControllerConfig) {
    c.mu.Lock()
    c.config = cfg
    c.mu.Unlock()
    if c.taskManager != nil {
        c.taskManager.SetConfig(cfg)
    }
    if c.eventQueue != nil {
        c.eventQueue.SetConfig(cfg)
    }
    if c.taskScheduler != nil {
        c.taskScheduler.SetConfig(cfg)
    }
    if c.eventHandler != nil {
        c.eventHandler.GetBase().Config = cfg
    }
}

// EventQueue 获取事件队列
func (c *Controller) EventQueue() *modules.EventQueue

// TaskManager 获取任务管理器
func (c *Controller) TaskManager() *modules.TaskManager

// TaskScheduler 获取任务调度器
func (c *Controller) TaskScheduler() *modules.TaskScheduler

// EventHandler 获取事件处理器
func (c *Controller) EventHandler() modules.EventHandler

// ContextEngine 获取上下文引擎
func (c *Controller) ContextEngine() iface.ContextEngine

// SetContextEngine 设置上下文引擎
func (c *Controller) SetContextEngine(ce iface.ContextEngine)

// AbilityManager 获取能力管理器
func (c *Controller) AbilityManager() *ability.AbilityManager

// SetAbilityManager 设置能力管理器
func (c *Controller) SetAbilityManager(am *ability.AbilityManager)
```

- [ ] **Step 4: 实现 SetEventHandler + AddTaskExecutor/RemoveTaskExecutor/GetTaskExecutor**

```go
// SetEventHandler 注入事件处理器并接线依赖。
// 对应 Python: Controller.set_event_handler(event_handler)
func (c *Controller) SetEventHandler(handler modules.EventHandler) {
    c.eventHandler = handler
    base := handler.GetBase()
    base.Config = c.config
    base.ContextEngine = c.contextEngine
    base.TaskScheduler = c.taskScheduler
    base.TaskManager = c.taskManager
    base.AbilityMgr = c.abilityMgr
}

// AddTaskExecutor 注册 TaskExecutor，支持链式调用。
// 对应 Python: Controller.add_task_executor(task_type, builder)
func (c *Controller) AddTaskExecutor(taskType string, builder func(deps *modules.TaskExecutorDependencies) modules.TaskExecutor) *Controller {
    c.taskScheduler.TaskExecutorRegistry().AddTaskExecutor(taskType, builder)
    return c
}

// RemoveTaskExecutor 移除 TaskExecutor。
// 对应 Python: Controller.remove_task_executor(task_type)
func (c *Controller) RemoveTaskExecutor(taskType string) {
    c.taskScheduler.TaskExecutorRegistry().RemoveTaskExecutor(taskType)
}

// GetTaskExecutor 获取 TaskExecutor。
// 对应 Python: Controller.get_task_executor(...)
func (c *Controller) GetTaskExecutor(taskType string, deps *modules.TaskExecutorDependencies) (modules.TaskExecutor, error) {
    return c.taskScheduler.TaskExecutorRegistry().GetTaskExecutor(taskType, deps)
}
```

- [ ] **Step 5: 实现状态持久化方法 restoreTaskManagerState/saveTaskManagerState**

```go
// restoreTaskManagerState 从 session 恢复 TaskManager 状态。
// 对应 Python: Controller._restore_task_manager_state(session)
func (c *Controller) restoreTaskManagerState(ctx context.Context, sess *session.Session) bool {
    // 1. session.GetState("controller") 获取保存的状态
    // 2. 无状态 → taskManager.ClearState() → return false
    // 3. 有状态 → TaskManagerState 反序列化 → taskManager.LoadState()
    // 4. 失败 → taskManager.ClearState() → return false
    // 5. 成功 → return true
}

// saveTaskManagerState 保存 TaskManager 状态到 session。
// 对应 Python: Controller._save_task_manager_state(session)
func (c *Controller) saveTaskManagerState(ctx context.Context, sess *session.Session) error {
    // 1. config.EnableTaskPersistence == false → 跳过
    // 2. taskManager.GetState() 获取当前状态
    // 3. session.UpdateState({"controller": controllerState}) 保存
}
```

需要使用 `session.GetState(state.StringKey("controller"))` 和 `session.UpdateState(map[string]any{"controller": stateData})`。

- [ ] **Step 6: 实现 BindSession/UnbindSession**

```go
// BindSession 绑定 session 到 Controller 基础设施。
// 对应 Python: Controller.bind_session(session)
func (c *Controller) BindSession(ctx context.Context, sess *session.Session) error {
    if err := c.ensureStarted(ctx); err != nil {
        return err
    }
    sessionID := sess.GetSessionID()
    c.restoreTaskManagerState(ctx, sess)
    c.taskScheduler.Sessions()[sessionID] = sess
    return c.eventQueue.Subscribe(ctx, c.card.ID(), sessionID)
}

// UnbindSession 解绑 session 并执行清理。
// 对应 Python: Controller.unbind_session(session)
func (c *Controller) UnbindSession(ctx context.Context, sess *session.Session) error {
    sessionID := sess.GetSessionID()
    _ = c.saveTaskManagerState(ctx, sess)
    if err := c.eventQueue.Unsubscribe(ctx, c.card.ID(), sessionID); err != nil {
        return err
    }
    delete(c.taskScheduler.Sessions(), sessionID)
    return nil
}
```

- [ ] **Step 7: 实现 PublishEventAsync**

```go
// PublishEventAsync 异步发布事件（fire-and-forget）。
// 对应 Python: Controller.publish_event_async(session, event)
func (c *Controller) PublishEventAsync(ctx context.Context, sess *session.Session, event schema.Event) error {
    return c.eventQueue.PublishEventAsync(ctx, c.card.ID(), sess, event)
}
```

- [ ] **Step 8: 实现 Stream — 流式执行（核心方法）**

```go
// Stream 流式执行，返回输出 chunk channel。
// 对应 Python: Controller.stream(inputs, session, stream_modes)
func (c *Controller) Stream(
    ctx context.Context,
    inputs *schema.InputEvent,
    sess *session.Session,
    streamModes []stream.StreamMode,
) <-chan *schema.ControllerOutputChunk {
    out := make(chan *schema.ControllerOutputChunk)

    go func() {
        defer close(out)

        // 0. 懒启动
        if err := c.ensureStarted(ctx); err != nil {
            logger.Error(logComponent).Err(err).Msg("Stream ensureStarted 失败")
            return
        }

        agentID := c.card.ID()
        sessionID := sess.GetSessionID()

        // 1. 恢复 TaskManager 状态
        stateRestored := c.restoreTaskManagerState(ctx, sess)
        if !stateRestored {
            logger.Info(logComponent).Str("session_id", sessionID).Msg("以全新 TaskManager 状态启动")
        }

        // 2. 注册 session
        c.taskScheduler.Sessions()[sessionID] = sess

        // 异常路径保护
        defer func() {
            // 7. 保存 TaskManager 状态
            _ = c.saveTaskManagerState(ctx, sess)
            // 8. 取消订阅
            _ = c.eventQueue.Unsubscribe(ctx, agentID, sessionID)
            // 9. 移除 session
            delete(c.taskScheduler.Sessions(), sessionID)
            logger.Info(logComponent).Str("session_id", sessionID).
                Int("active_sessions", len(c.taskScheduler.Sessions())).
                Msg("session 完成")
        }()

        // 3. 订阅事件
        if err := c.eventQueue.Subscribe(ctx, agentID, sessionID); err != nil {
            logger.Error(logComponent).Err(err).Msg("Stream Subscribe 失败")
            return
        }

        // 4. 发布输入事件（同步，等待 handler 处理完）
        if err := c.eventQueue.PublishEvent(ctx, agentID, sess, inputs); err != nil {
            logger.Error(logComponent).Err(err).Msg("Stream PublishEvent 失败")
            return
        }

        // 5. 确保完成信号（如果 handler 没创建任务，立即发送 all_tasks_processed）
        c.taskScheduler.EnsureSessionCompletionSignal(ctx, sessionID)

        // 6. 从 session stream 读取 chunk
        firstFrameTimeout := c.config.StreamFirstFrameTimeout
        if firstFrameTimeout <= 0 {
            firstFrameTimeout = 30.0
        }

        iter := sess.StreamIterator()
        gotFirst := false

        // 首帧超时等待
        select {
        case firstChunk, ok := <-iter:
            if !ok {
                return // stream 已关闭
            }
            gotFirst = true
            // 检查首帧是否为 all_tasks_processed
            if !c.isCompletionSignal(firstChunk) {
                out <- firstChunk
            }
        case <-time.After(time.Duration(firstFrameTimeout * float64(time.Second))):
            logger.Error(logComponent).Float64("timeout", firstFrameTimeout).Str("session_id", sessionID).Msg("首帧超时")
            return
        case <-ctx.Done():
            logger.Error(logComponent).Err(ctx.Err()).Msg("Stream 上下文取消")
            return
        }

        if !gotFirst {
            return
        }

        // 继续读取后续 chunk
        for chunk := range iter {
            if c.isCompletionSignal(chunk) {
                logger.Info(logComponent).Str("session_id", sessionID).Msg("所有任务已处理完毕，停止流")
                break
            }
            out <- chunk
        }
    }()

    return out
}

// isCompletionSignal 检查 chunk 是否为 all_tasks_processed 完成信号
func (c *Controller) isCompletionSignal(chunk *schema.ControllerOutputChunk) bool {
    if chunk == nil || chunk.Payload == nil {
        return false
    }
    return chunk.Payload.Type == schema.AllTasksProcessed
}
```

**注意**：`session.StreamIterator()` 返回 `<-chan stream.Schema`，而 `out` channel 类型是 `<-chan *schema.ControllerOutputChunk`。需要在读取后做类型断言 `chunk.(*schema.ControllerOutputChunk)` 来转换。

- [ ] **Step 9: 实现 Invoke — 批量执行**

```go
// Invoke 批量执行，收集所有 chunk 后返回 ControllerOutput。
// 对应 Python: Controller.invoke(inputs, session)
func (c *Controller) Invoke(
    ctx context.Context,
    inputs *schema.InputEvent,
    sess *session.Session,
) (*schema.ControllerOutput, error) {
    ch := c.Stream(ctx, inputs, sess, []stream.StreamMode{stream.StreamModeOutput})

    var chunks []*schema.ControllerOutputChunk
    for chunk := range ch {
        chunks = append(chunks, chunk)
    }

    return &schema.ControllerOutput{
        Type: string(schema.EventTaskCompletion),
        Data: chunks,
    }, nil
}
```

- [ ] **Step 10: 确保所有方法对照 Python base.py，不遗漏**

对照清单：
- [x] `__init__` → `NewController`
- [x] `init` → `Init`
- [x] `start` → `Start`
- [x] `stop` → `Stop`
- [x] `_ensure_started` → `ensureStarted`
- [x] `bind_session` → `BindSession`
- [x] `unbind_session` → `UnbindSession`
- [x] `invoke` → `Invoke`
- [x] `stream` → `Stream`
- [x] `_restore_task_manager_state` → `restoreTaskManagerState`
- [x] `_save_task_manager_state` → `saveTaskManagerState`
- [x] `add_task_executor` → `AddTaskExecutor`
- [x] `remove_task_executor` → `RemoveTaskExecutor`
- [x] `get_task_executor` → `GetTaskExecutor`
- [x] `publish_event_async` → `PublishEventAsync`
- [x] `set_event_handler` → `SetEventHandler`
- [x] `config` property → `Config`/`SetConfig`
- [x] `event_queue` property → `EventQueue`
- [x] `task_manager` property → `TaskManager`
- [x] `task_scheduler` property → `TaskScheduler`
- [x] `event_handler` property → `EventHandler`
- [x] `context_engine` property → `ContextEngine`/`SetContextEngine`
- [x] `ability_manager` property → `AbilityManager`/`SetAbilityManager`

---

### Task 5: Controller 单元测试

**Files:**
- Create: `internal/agentcore/controller/controller_test.go`

- [ ] **Step 1: 编写 NewController + Init 测试**

```go
func TestNewController(t *testing.T)              // 空壳构造
func TestController_Init(t *testing.T)            // 初始化，验证子组件创建 + 接线
```

- [ ] **Step 2: 编写 Start/Stop/ensureStarted 测试**

```go
func TestController_Start(t *testing.T)           // 启动后 started==true
func TestController_Stop(t *testing.T)            // 停止后 started==false
func TestController_ensureStarted(t *testing.T)   // 懒启动，多次调用幂等
```

- [ ] **Step 3: 编写 SetConfig 级联传播测试**

```go
func TestController_SetConfig(t *testing.T)       // 验证 config 传播到 4 个子组件
```

- [ ] **Step 4: 编写 SetEventHandler 测试**

```go
func TestController_SetEventHandler(t *testing.T) // 验证 EventHandler 依赖接线
```

- [ ] **Step 5: 编写 AddTaskExecutor/RemoveTaskExecutor 测试**

```go
func TestController_AddTaskExecutor(t *testing.T)
func TestController_RemoveTaskExecutor(t *testing.T)
```

- [ ] **Step 6: 编写 BindSession/UnbindSession 测试**

```go
func TestController_BindSession(t *testing.T)
func TestController_UnbindSession(t *testing.T)
```

- [ ] **Step 7: 编写 restoreTaskManagerState/saveTaskManagerState 测试**

```go
func TestController_restoreTaskManagerState_无状态(t *testing.T)     // ClearState
func TestController_restoreTaskManagerState_有状态(t *testing.T)     // LoadState
func TestController_restoreTaskManagerState_状态损坏(t *testing.T)   // 失败回退
func TestController_saveTaskManagerState_禁用持久化(t *testing.T)    // 跳过
func TestController_saveTaskManagerState_启用持久化(t *testing.T)    // 正常保存
```

- [ ] **Step 8: 编写 Invoke + Stream 测试**

```go
func TestController_Invoke(t *testing.T)                             // 批量执行
func TestController_Stream_正常流(t *testing.T)                     // 流式执行
func TestController_Stream_首帧超时(t *testing.T)                   // 超时错误
func TestController_Stream_allTasksProcessed过滤(t *testing.T)       // 完成信号过滤
func TestController_isCompletionSignal(t *testing.T)                 // 信号判断
```

这些测试需要 mock Session、EventQueue 等。Session mock 需实现 `StreamIterator()` 返回预设 chunk 的 channel。

- [ ] **Step 9: 运行全部测试确认通过**

```bash
export GOPROXY=https://goproxy.cn,direct
go test ./internal/agentcore/controller/ -v -cover
```

- [ ] **Step 10: 提交**

```bash
git add internal/agentcore/controller/controller.go internal/agentcore/controller/controller_test.go
git commit -m "feat(controller): 实现 Controller 主结构体 — 事件驱动任务编排"
```

---

### Task 6: 创建 controller/doc.go — 包文档

**Files:**
- Create: `internal/agentcore/controller/doc.go`
- 对应 Python: `openjiuwen/core/controller/__init__.py`

- [ ] **Step 1: 编写 doc.go**

```go
// Package controller 提供事件驱动任务编排控制器，是 ControllerAgent 的核心组件。
//
// Controller 负责：
//   - 处理事件（通过 EventQueue + EventHandler）
//   - 管理任务生命周期（通过 TaskManager + TaskScheduler）
//   - 执行意图识别和处理（通过 IntentRecognizer）
//   - 流式/批量输出（通过 Session StreamIterator）
//
// 文件目录：
//
//	controller/
//	├── doc.go           # 包文档
//	├── controller.go    # Controller 主结构体
//	├── config/          # 控制器配置
//	├── modules/         # 核心子模块
//	└── schema/          # 公共类型定义
//
// 对应 Python 代码：openjiuwen/core/controller/
package controller
```

- [ ] **Step 2: 提交**

```bash
git add internal/agentcore/controller/doc.go
git commit -m "docs(controller): 添加包文档 doc.go"
```

---

### Task 7: 更新 IMPLEMENTATION_PLAN.md

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 更新 6.19 状态**

将 `6.19 | ☐ | Controller` 改为 `6.19 | ✅ | Controller | ✅ Controller 主结构体：两阶段初始化、懒启动、会话绑定/解绑、Stream/Invoke 流式/批量执行、状态持久化、config 级联传播、首帧超时；✅ IntentToolkits 完整实现；✅ IntentRecognizer 骨架（⤵️ 6.23 回填 LLM 调用）；✅ EventHandlerWithIntentRecognition 完整实现 | openjiuwen/core/controller/base.py`

- [ ] **Step 2: 更新 6.20 产出描述**

补充 `+ intent_toolkits.go` 到产出列。

- [ ] **Step 3: 更新 6.21 产出描述**

补充 `+ intent_recognizer.go` 到产出列。

- [ ] **Step 4: 提交**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "docs: 更新实现计划 6.19 Controller 状态为已完成"
```

---

### Task 8: 最终验证 — 全量编译 + 测试

- [ ] **Step 1: 全量编译**

```bash
export GOPROXY=https://goproxy.cn,direct
go build ./...
```

- [ ] **Step 2: 全量测试（含覆盖率）**

```bash
go test -cover ./internal/agentcore/controller/...
```

- [ ] **Step 3: 确认覆盖率 ≥ 85%**

如有不达标包，补充测试。

- [ ] **Step 4: 最终提交（如有补充）**

```bash
git add -A
git commit -m "test(controller): 补充测试至覆盖率达标"
```
