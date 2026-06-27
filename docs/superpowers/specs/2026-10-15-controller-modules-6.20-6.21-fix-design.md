# 6.20+6.21 实现偏差修复设计

## 概述

6.20+6.21（底层组件）实现过程中，与设计文档及 Python 源码存在 9 处偏差（2 高风险 + 7 中风险）。
本文档记录每处偏差的修复方案、影响范围和实现顺序。

## 修复项总览

| # | 风险 | 编号 | 偏差描述 | 修复方案 |
|---|------|------|---------|---------|
| 1 | 🔴 | H1 | Intent 字段严重缺失 | 完全对齐 Python 9 字段，删除 Go 独有字段，补充 Validate |
| 2 | 🔴 | H2 | payload type 常量大小写错误 | 改为小写对齐 EventType 枚举值 |
| 3 | 🟡 | M1 | AbilityMgr 使用 any 类型 | 改为 `*ability.AbilityManager` 具体类型 |
| 4 | 🟡 | M2 | PauseTask/CancelTask 返回 error | 改为 `(bool, error)` |
| 5 | 🟡 | M3 | ensureSessionCompletionSignal 非导出 | 导出方法 + 从 sessions map 查找 + 补充 Payload.Data |
| 6 | 🟡 | M4 | TaskExecutorRegistry 外部注入 | 改为内部自建，删除 NewTaskScheduler 的 registry 参数 |
| 7 | 🟡 | M5 | TaskExecutor pause/cancel 返回 error | 改为 `(bool, error)` |
| 8 | 🟡 | M6 | Produce 合并两种消息类型 | 拆分为 Produce + ProduceSync |
| 9 | 🟡 | M7 | Activate 接受 timeout 参数 | 去掉参数，Subscription 自身持有 timeout |

## 实现顺序

按风险从高到低：H1 → H2 → M1 → M2 → M3 → M4 → M5 → M6 → M7

---

## H1: Intent 字段对齐 Python

### 当前问题

Go Intent 只有 5 字段（IntentType, TaskID, SessionID, Data, Params），Python 有 9 字段。
Go 独有 SessionID/Data/Params 在 Python 中不存在。

### 修复方案

**目标结构体：**

```go
type Intent struct {
    // IntentType 意图类型
    IntentType IntentType `json:"intent_type"`
    // Event 关联事件（通常为 InputEvent）
    Event Event `json:"event"`
    // TargetTaskID 目标任务ID
    TargetTaskID string `json:"target_task_id,omitempty"`
    // TargetTaskDescription 目标任务描述（CREATE_TASK/SWITCH_TASK 必需）
    TargetTaskDescription string `json:"target_task_description,omitempty"`
    // DependTaskID 依赖任务ID列表（CONTINUE_TASK 必需）
    DependTaskID []string `json:"depend_task_id,omitempty"`
    // SupplementaryInfo 补充信息（SUPPLEMENT_TASK 必需）
    SupplementaryInfo string `json:"supplementary_info,omitempty"`
    // ModificationDetails 修改详情（MODIFY_TASK 必需）
    ModificationDetails string `json:"modification_details,omitempty"`
    // Confidence 意图识别置信度，范围 [0.0, 1.0]
    Confidence float64 `json:"confidence"`
    // Metadata 意图元数据
    Metadata map[string]any `json:"metadata,omitempty"`
    // ClarificationPrompt 澄清提示（UNKNOWN_TASK 必需）
    ClarificationPrompt string `json:"clarification_prompt,omitempty"`
}
```

**删除字段：** TaskID, SessionID, Data, Params
**重命名：** TaskID → TargetTaskID
**新增字段：** Event, TargetTaskDescription, DependTaskID, SupplementaryInfo, ModificationDetails, Confidence, Metadata, ClarificationPrompt

### Validate 方法

对齐 Python `_validate()` 逻辑：

| IntentType | 必填字段 |
|------------|---------|
| CREATE_TASK | target_task_description |
| PAUSE_TASK | target_task_id |
| RESUME_TASK | target_task_id |
| CONTINUE_TASK | depend_task_id |
| SUPPLEMENT_TASK | target_task_id + supplementary_info |
| CANCEL_TASK | target_task_id |
| MODIFY_TASK | target_task_id + modification_details |
| SWITCH_TASK | target_task_description |
| UNKNOWN_TASK | clarification_prompt |

通用校验：confidence ∈ [0.0, 1.0]

### 影响范围

- `intent.go`：结构体重写 + 新增 Validate + 新增 NewIntent 工厂
- `intent_test.go`：测试用例全部重写
- 引用 Intent 的代码需同步修改（搜索 `Intent{` 构造和 `.TaskID`/`.SessionID`/`.Data`/`.Params` 访问）

---

## H2: Payload Type 常量修复

### 当前问题

```go
payloadTypeTaskCompletion  = "TASK_COMPLETION"   // 错误：大写
payloadTypeTaskInteraction = "TASK_INTERACTION"   // 错误：大写
payloadTypeTaskFailed      = "TASK_FAILED"        // 错误：大写
```

Python 使用 EventType 枚举值（小写）：`"task_completion"`, `"task_interaction"`, `"task_failed"`。
TaskScheduler 的 switch 用这些常量匹配 `chunk.Payload.Type`，如果 TaskExecutor 返回小写值则匹配不上。

### 修复方案

```go
const (
    payloadTypeTaskCompletion  = "task_completion"
    payloadTypeTaskInteraction = "task_interaction"
    payloadTypeTaskFailed      = "task_failed"
)
```

### 影响范围

- `task_scheduler.go`：常量值修改，switch 分支逻辑不变
- `task_scheduler_test.go`：涉及 payload type 的测试需同步修改

---

## M1: AbilityMgr 类型修复

### 当前问题

EventHandlerBase.AbilityMgr、TaskScheduler.abilityMgr、TaskExecutorDependencies.AbilityMgr 均为 `any`。

### 修复方案

- 导入 `ability "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/ability"`
- 所有 AbilityMgr 字段改为 `*ability.AbilityManager`

### 影响范围

- `event_handler.go`：EventHandlerBase.AbilityMgr 类型
- `task_scheduler.go`：abilityMgr 字段类型、NewTaskScheduler 参数类型
- `task_executor.go`：TaskExecutorDependencies.AbilityMgr 类型
- 所有测试中使用 AbilityMgr 的地方需同步修改

---

## M2: PauseTask/CancelTask 返回值修复

### 当前问题

```go
func (s *TaskScheduler) PauseTask(ctx context.Context, taskID string) error
func (s *TaskScheduler) CancelTask(ctx context.Context, taskID string) error
```

Python 返回 `bool`（是否成功），Go 只有 `error`（无法区分"任务不存在"和"任务状态不允许暂停"）。

### 修复方案

```go
func (s *TaskScheduler) PauseTask(ctx context.Context, taskID string) (bool, error)
func (s *TaskScheduler) CancelTask(ctx context.Context, taskID string) (bool, error)
```

- `bool`：是否成功暂停/取消（对齐 Python）
- `error`：系统级异常（Go 惯例增强）

### 影响范围

- `task_scheduler.go`：PauseTask/CancelTask 签名 + 实现
- 调用 PauseTask/CancelTask 的代码需适配

---

## M3: EnsureSessionCompletionSignal 修复

### 当前问题

1. 只有非导出 `ensureSessionCompletionSignal`，Controller 无法从外部调用
2. 额外接受 `sess` 参数，Python 内部从 `self._sessions` 查找
3. Payload.Data 缺失（Python 设置 `data=[TextDataFrame(...)]`）

### 修复方案

```go
// EnsureSessionCompletionSignal 检查并发送 all_tasks_processed 信号。
// 对齐 Python: TaskScheduler.ensure_session_completion_signal
func (s *TaskScheduler) EnsureSessionCompletionSignal(ctx context.Context, sessionID string) {
    // 从 s.sessions map 中查找 session（对齐 Python: self._sessions.get）
    // 补充 Payload.Data = []*DataFrame{NewTextDataFrame("All tasks have been successfully processed")}
}
```

- 删除 `ensureSessionCompletionSignal` 的 `sess` 参数
- 重命名为导出方法 `EnsureSessionCompletionSignal`
- 内部统一从 `s.sessions` 查找 session
- 补充 `Payload.Data`

### 影响范围

- `task_scheduler.go`：方法签名修改 + 实现
- `executeTaskWrapper` 中的调用点需适配（去掉 sess 参数）

---

## M4: TaskExecutorRegistry 注入方式修复

### 当前问题

Go 将 `*TaskExecutorRegistry` 作为 `NewTaskScheduler` 参数外部注入，Python 在 `__init__` 内部自建。

### 修复方案

- 删除 `NewTaskScheduler` 的 `registry` 参数
- 内部 `NewTaskExecutorRegistry()` 自建
- 添加公开 `TaskExecutorRegistry()` getter 方法供 Controller 逐个注册

```go
func NewTaskScheduler(cfg, taskManager, contextEngine, abilityMgr, eventQueue, card) *TaskScheduler {
    return &TaskScheduler{
        taskExecutorRegistry: NewTaskExecutorRegistry(),
        // ...
    }
}

func (s *TaskScheduler) TaskExecutorRegistry() *TaskExecutorRegistry {
    return s.taskExecutorRegistry
}
```

### 影响范围

- `task_scheduler.go`：NewTaskScheduler 签名修改 + 新增 getter
- 所有调用 NewTaskScheduler 的代码需删除 registry 参数

---

## M5: TaskExecutor 接口返回值修复

### 当前问题

```go
Pause(ctx, taskID, sess) error
Cancel(ctx, taskID, sess) error
```

Python `pause`/`cancel` 返回 `bool`。

### 修复方案

```go
type TaskExecutor interface {
    ExecuteAbility(ctx, taskID, sess) (<-chan *schema.ControllerOutputChunk, error)
    CanPause(ctx, taskID, sess) (bool, string, error)    // 保持不变
    Pause(ctx, taskID, sess) (bool, error)               // error → (bool, error)
    CanCancel(ctx, taskID, sess) (bool, string, error)   // 保持不变
    Cancel(ctx, taskID, sess) (bool, error)              // error → (bool, error)
}
```

与 M2 的 PauseTask/CancelTask 签名统一。

### 影响范围

- `task_executor.go`：接口签名修改
- TaskExecutor 的所有实现方需适配
- `task_scheduler.go` 中调用 executor.Pause/Cancel 的地方需适配

---

## M6: Produce 方法拆分

### 当前问题

```go
Produce(ctx, topic, msg *QueueMessage, invoke *InvokeQueueMessage) error
```

合并了火忘和同步两种发布模式。

### 修复方案

```go
// Produce 火忘发布消息，不等待处理结果。
func (q *MessageQueueInMemory) Produce(ctx context.Context, topic string, msg *QueueMessage) error

// ProduceSync 同步发布消息，等待处理结果。
func (q *MessageQueueInMemory) ProduceSync(ctx context.Context, topic string, invoke *InvokeQueueMessage) error
```

### 影响范围

- `queue.go`：Produce 拆分为两个方法
- `event_queue.go`：PublishEvent 和 PublishEventAsync 调用点需适配
- `queue_test.go`：测试用例需适配

---

## M7: Subscription.Activate 去掉 timeout 参数

### 当前问题

```go
func (s *Subscription) Activate(timeout time.Duration)
```

Python `activate()` 不接受参数，timeout 在 `__init__` 中设置。

### 修复方案

- `topicSubscription` 持有 `timeout time.Duration` 字段（在 Subscribe 时从 MessageQueueInMemory.timeout 传入）
- `Activate()` 无参数，内部使用 `s.ts.timeout`

```go
func (s *Subscription) Activate() {
    s.ts.activate()  // 使用 topicSubscription 自身的 timeout
}
```

### 影响范围

- `queue.go`：topicSubscription 新增 timeout 字段、Subscribe 时传入、Activate 去参数
- `queue_test.go`：调用 Activate(timeout) 的地方需去掉参数

---

## 风险评估

| 修复项 | 代码改动量 | 测试影响 | 向后兼容性 |
|--------|-----------|---------|-----------|
| H1 | 大（结构体重写） | 大（测试全部重写） | 破坏（Intent 构造方式变化） |
| H2 | 小（常量值） | 小 | 兼容（字符串值改为正确值） |
| M1 | 中（类型替换） | 中 | 破坏（any→具体类型） |
| M2 | 中（签名变化） | 中 | 破坏（返回值变化） |
| M3 | 中（方法重构） | 小 | 破坏（方法签名变化） |
| M4 | 小（构造函数） | 小 | 破坏（参数变化） |
| M5 | 小（接口签名） | 中 | 破坏（接口变化） |
| M6 | 中（方法拆分） | 中 | 破坏（API 变化） |
| M7 | 小（参数去掉） | 小 | 破坏（签名变化） |

所有修复项均为**破坏性变更**，但因 6.20+6.21 刚实现尚未被外部依赖，影响范围可控。
