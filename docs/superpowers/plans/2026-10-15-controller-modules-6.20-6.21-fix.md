# 6.20+6.21 实现偏差修复 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 修复 6.20+6.21 底层组件实现与设计/Python 的 9 处偏差（2 高风险 + 7 中风险），确保运行时正确性和类型安全。

**Architecture:** 按风险从高到低逐项修复，每项修复后运行相关测试验证。所有修改限于 controller/modules、controller/schema、controller/config、runner/message_queue 四个包内。

**Tech Stack:** Go 1.22+, testify/assert, 项目自定义 exception 包

---

## Task 1: H1 — Intent 字段对齐 Python + Validate 实现

**Files:**
- Modify: `internal/agentcore/controller/schema/intent.go`
- Modify: `internal/agentcore/controller/schema/intent_test.go`
- Modify: `internal/agentcore/controller/schema/doc.go`（如有字段变更需更新）

- [ ] **Step 1: 重写 Intent 结构体，完全对齐 Python 9 字段**

修改 `internal/agentcore/controller/schema/intent.go`：

```go
// ──────────────────────────── 结构体 ────────────────────────────

// Intent 意图，描述用户对任务的操作意图。
//
// 对应 Python: openjiuwen/core/controller/schema/intent.py (Intent)
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
	// Confidence 意图识别置信度，范围 [0.0, 1.0]，默认 1.0
	Confidence float64 `json:"confidence"`
	// Metadata 意图元数据
	Metadata map[string]any `json:"metadata,omitempty"`
	// ClarificationPrompt 澄清提示（UNKNOWN_TASK 必需）
	ClarificationPrompt string `json:"clarification_prompt,omitempty"`
}
```

删除旧字段：TaskID, SessionID, Data, Params。

- [ ] **Step 2: 实现 Validate() error 方法，对齐 Python _validate**

在 `intent.go` 非导出函数区块前添加导出函数：

```go
// ──────────────────────────── 导出函数 ────────────────────────────

// NewIntent 创建意图实例，初始化默认值并校验。
// 对齐 Python: Intent.__init__ + _post_init + _validate
func NewIntent(intentType IntentType, event Event, opts ...IntentOption) (*Intent, error) {
	i := &Intent{
		IntentType: intentType,
		Event:      event,
		Confidence: 1.0,
		Metadata:   make(map[string]any),
	}
	for _, opt := range opts {
		opt(i)
	}
	if i.Metadata == nil {
		i.Metadata = make(map[string]any)
	}
	if err := i.Validate(); err != nil {
		return nil, err
	}
	return i, nil
}

// Validate 校验意图字段是否满足类型约束。
// 对齐 Python: Intent._validate
func (i *Intent) Validate() error {
	// 校验置信度范围
	if i.Confidence < 0.0 || i.Confidence > 1.0 {
		return exception.NewBaseError(
			exception.StatusAgentControllerRuntimeError,
			exception.WithMsg(fmt.Sprintf("Confidence must be between 0.0 and 1.0, got %v", i.Confidence)),
		)
	}

	// 按意图类型校验必填字段
	switch i.IntentType {
	case IntentCreateTask:
		if i.TargetTaskDescription == "" {
			return exception.NewBaseError(
				exception.StatusAgentControllerRuntimeError,
				exception.WithMsg("CREATE_TASK intent requires target_task_description"),
			)
		}
	case IntentContinueTask:
		if len(i.DependTaskID) == 0 {
			return exception.NewBaseError(
				exception.StatusAgentControllerRuntimeError,
				exception.WithMsg("CONTINUE_TASK intent requires depend_task_id"),
			)
		}
	case IntentSupplementTask:
		if i.TargetTaskID == "" {
			return exception.NewBaseError(
				exception.StatusAgentControllerRuntimeError,
				exception.WithMsg("SUPPLEMENT_TASK intent requires target_task_id"),
			)
		}
		if i.SupplementaryInfo == "" {
			return exception.NewBaseError(
				exception.StatusAgentControllerRuntimeError,
				exception.WithMsg("SUPPLEMENT_TASK intent requires supplementary_info"),
			)
		}
	case IntentModifyTask:
		if i.TargetTaskID == "" {
			return exception.NewBaseError(
				exception.StatusAgentControllerRuntimeError,
				exception.WithMsg("MODIFY_TASK intent requires target_task_id"),
			)
		}
		if i.ModificationDetails == "" {
			return exception.NewBaseError(
				exception.StatusAgentControllerRuntimeError,
				exception.WithMsg("MODIFY_TASK intent requires modification_details"),
			)
		}
	case IntentPauseTask, IntentResumeTask, IntentCancelTask:
		if i.TargetTaskID == "" {
			return exception.NewBaseError(
				exception.StatusAgentControllerRuntimeError,
				exception.WithMsg(fmt.Sprintf("%s intent requires target_task_id", string(i.IntentType))),
			)
		}
	case IntentSwitchTask:
		if i.TargetTaskDescription == "" {
			return exception.NewBaseError(
				exception.StatusAgentControllerRuntimeError,
				exception.WithMsg("SWITCH_TASK intent requires target_task_description"),
			)
		}
	case IntentUnknownTask:
		if i.ClarificationPrompt == "" {
			return exception.NewBaseError(
				exception.StatusAgentControllerRuntimeError,
				exception.WithMsg("UNKNOWN_TASK intent requires clarification_prompt"),
			)
		}
	}
	return nil
}
```

添加 IntentOption 函数选项类型：

```go
// ──────────────────────────── 非导出函数 ────────────────────────────

// IntentOption 意图可选配置函数。
type IntentOption func(*Intent)

// WithTargetTaskID 设置目标任务ID。
func WithTargetTaskID(id string) IntentOption {
	return func(i *Intent) { i.TargetTaskID = id }
}

// WithTargetTaskDescription 设置目标任务描述。
func WithTargetTaskDescription(desc string) IntentOption {
	return func(i *Intent) { i.TargetTaskDescription = desc }
}

// WithDependTaskID 设置依赖任务ID列表。
func WithDependTaskID(ids []string) IntentOption {
	return func(i *Intent) { i.DependTaskID = ids }
}

// WithSupplementaryInfo 设置补充信息。
func WithSupplementaryInfo(info string) IntentOption {
	return func(i *Intent) { i.SupplementaryInfo = info }
}

// WithModificationDetails 设置修改详情。
func WithModificationDetails(details string) IntentOption {
	return func(i *Intent) { i.ModificationDetails = details }
}

// WithConfidence 设置置信度。
func WithConfidence(c float64) IntentOption {
	return func(i *Intent) { i.Confidence = c }
}

// WithClarificationPrompt 设置澄清提示。
func WithClarificationPrompt(prompt string) IntentOption {
	return func(i *Intent) { i.ClarificationPrompt = prompt }
}
```

需新增 import: `"fmt"` 和 `"github.com/uapclaw/uapclaw-go/internal/common/exception"`

- [ ] **Step 3: 重写 intent_test.go，覆盖 Validate 全部分支**

```go
//go:build test

package schema

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestIntentType_值对齐Python 验证 9 个枚举值与 Python IntentType 字符串值完全对齐。
func TestIntentType_值对齐Python(t *testing.T) {
	expected := map[IntentType]string{
		IntentCreateTask:     "create_task",
		IntentPauseTask:      "pause_task",
		IntentResumeTask:     "resume_task",
		IntentContinueTask:   "continue_task",
		IntentSupplementTask: "supplement_task",
		IntentCancelTask:     "cancel_task",
		IntentModifyTask:     "modify_task",
		IntentSwitchTask:     "switch_task",
		IntentUnknownTask:    "unknown_task",
	}
	for it, want := range expected {
		if string(it) != want {
			t.Errorf("IntentType 值不对齐: got %q, want %q", string(it), want)
		}
	}
	if len(expected) != 9 {
		t.Errorf("IntentType 枚举数量 = %d, want 9", len(expected))
	}
}

// TestNewIntent_创建成功 测试合法意图创建。
func TestNewIntent_创建成功(t *testing.T) {
	event := &InputEvent{BaseEvent: BaseEvent{EventIDField: "e1"}}
	intent, err := NewIntent(IntentCreateTask, event,
		WithTargetTaskDescription("做某事"),
	)
	assert.NoError(t, err)
	assert.Equal(t, IntentCreateTask, intent.IntentType)
	assert.Equal(t, "e1", intent.Event.GetEventID())
	assert.Equal(t, "做某事", intent.TargetTaskDescription)
	assert.Equal(t, 1.0, intent.Confidence)
}

// TestNewIntent_置信度越界 测试置信度超出 [0.0, 1.0] 范围返回错误。
func TestNewIntent_置信度越界(t *testing.T) {
	event := &InputEvent{BaseEvent: BaseEvent{EventIDField: "e1"}}
	_, err := NewIntent(IntentCreateTask, event,
		WithTargetTaskDescription("做某事"),
		WithConfidence(1.5),
	)
	assert.Error(t, err)
}

// TestValidate_CREATE_TASK缺描述 测试 CREATE_TASK 缺少 target_task_description。
func TestValidate_CREATE_TASK缺描述(t *testing.T) {
	event := &InputEvent{BaseEvent: BaseEvent{EventIDField: "e1"}}
	intent := &Intent{IntentType: IntentCreateTask, Event: event, Confidence: 1.0}
	err := intent.Validate()
	assert.Error(t, err)
}

// TestValidate_CONTINUE_TASK缺依赖 测试 CONTINUE_TASK 缺少 depend_task_id。
func TestValidate_CONTINUE_TASK缺依赖(t *testing.T) {
	event := &InputEvent{BaseEvent: BaseEvent{EventIDField: "e1"}}
	intent := &Intent{IntentType: IntentContinueTask, Event: event, Confidence: 1.0}
	err := intent.Validate()
	assert.Error(t, err)
}

// TestValidate_SUPPLEMENT_TASK缺字段 测试 SUPPLEMENT_TASK 缺少必填字段。
func TestValidate_SUPPLEMENT_TASK缺字段(t *testing.T) {
	event := &InputEvent{BaseEvent: BaseEvent{EventIDField: "e1"}}
	// 缺 target_task_id
	intent := &Intent{IntentType: IntentSupplementTask, Event: event, Confidence: 1.0, SupplementaryInfo: "info"}
	assert.Error(t, intent.Validate())
	// 缺 supplementary_info
	intent2 := &Intent{IntentType: IntentSupplementTask, Event: event, Confidence: 1.0, TargetTaskID: "t1"}
	assert.Error(t, intent2.Validate())
}

// TestValidate_MODIFY_TASK缺字段 测试 MODIFY_TASK 缺少必填字段。
func TestValidate_MODIFY_TASK缺字段(t *testing.T) {
	event := &InputEvent{BaseEvent: BaseEvent{EventIDField: "e1"}}
	intent := &Intent{IntentType: IntentModifyTask, Event: event, Confidence: 1.0, ModificationDetails: "改"}
	assert.Error(t, intent.Validate())
}

// TestValidate_PAUSE_TASK缺ID 测试 PAUSE_TASK/RESUME_TASK/CANCEL_TASK 缺少 target_task_id。
func TestValidate_PAUSE_TASK缺ID(t *testing.T) {
	event := &InputEvent{BaseEvent: BaseEvent{EventIDField: "e1"}}
	for _, it := range []IntentType{IntentPauseTask, IntentResumeTask, IntentCancelTask} {
		intent := &Intent{IntentType: it, Event: event, Confidence: 1.0}
		assert.Error(t, intent.Validate(), "IntentType=%s should require target_task_id", it)
	}
}

// TestValidate_SWITCH_TASK缺描述 测试 SWITCH_TASK 缺少 target_task_description。
func TestValidate_SWITCH_TASK缺描述(t *testing.T) {
	event := &InputEvent{BaseEvent: BaseEvent{EventIDField: "e1"}}
	intent := &Intent{IntentType: IntentSwitchTask, Event: event, Confidence: 1.0}
	assert.Error(t, intent.Validate())
}

// TestValidate_UNKNOWN_TASK缺提示 测试 UNKNOWN_TASK 缺少 clarification_prompt。
func TestValidate_UNKNOWN_TASK缺提示(t *testing.T) {
	event := &InputEvent{BaseEvent: BaseEvent{EventIDField: "e1"}}
	intent := &Intent{IntentType: IntentUnknownTask, Event: event, Confidence: 1.0}
	assert.Error(t, intent.Validate())
}

// TestValidate_各类型合法 测试各意图类型满足必填字段后 Validate 通过。
func TestValidate_各类型合法(t *testing.T) {
	event := &InputEvent{BaseEvent: BaseEvent{EventIDField: "e1"}}
	cases := []struct {
		name   string
		intent *Intent
	}{
		{"CREATE_TASK", &Intent{IntentType: IntentCreateTask, Event: event, TargetTaskDescription: "做", Confidence: 1.0}},
		{"CONTINUE_TASK", &Intent{IntentType: IntentContinueTask, Event: event, DependTaskID: []string{"t1"}, Confidence: 1.0}},
		{"SUPPLEMENT_TASK", &Intent{IntentType: IntentSupplementTask, Event: event, TargetTaskID: "t1", SupplementaryInfo: "补", Confidence: 1.0}},
		{"MODIFY_TASK", &Intent{IntentType: IntentModifyTask, Event: event, TargetTaskID: "t1", ModificationDetails: "改", Confidence: 1.0}},
		{"PAUSE_TASK", &Intent{IntentType: IntentPauseTask, Event: event, TargetTaskID: "t1", Confidence: 1.0}},
		{"RESUME_TASK", &Intent{IntentType: IntentResumeTask, Event: event, TargetTaskID: "t1", Confidence: 1.0}},
		{"CANCEL_TASK", &Intent{IntentType: IntentCancelTask, Event: event, TargetTaskID: "t1", Confidence: 1.0}},
		{"SWITCH_TASK", &Intent{IntentType: IntentSwitchTask, Event: event, TargetTaskDescription: "切", Confidence: 1.0}},
		{"UNKNOWN_TASK", &Intent{IntentType: IntentUnknownTask, Event: event, ClarificationPrompt: "请澄清", Confidence: 1.0}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.NoError(t, c.intent.Validate())
		})
	}
}
```

- [ ] **Step 4: 运行测试验证**

```bash
cd /home/opensource/uap-claw-go && go test -tags test -v ./internal/agentcore/controller/schema/... -run TestIntent
```

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/controller/schema/intent.go internal/agentcore/controller/schema/intent_test.go
git commit -m "fix(controller): 对齐 Python Intent 9 字段 + Validate 校验逻辑 (H1)"
```

---

## Task 2: H2 — Payload Type 常量改为小写

**Files:**
- Modify: `internal/agentcore/controller/modules/task_scheduler.go:66-72`
- Modify: `internal/agentcore/controller/modules/task_scheduler_test.go`（如有引用 payloadType 常量）

- [ ] **Step 1: 修改 payloadType 常量值为小写**

修改 `internal/agentcore/controller/modules/task_scheduler.go` 行 66-72：

```go
const (
	// payloadTypeTaskCompletion 任务完成载荷类型（对齐 EventType 枚举值）
	payloadTypeTaskCompletion = "task_completion"
	// payloadTypeTaskInteraction 任务交互载荷类型（对齐 EventType 枚举值）
	payloadTypeTaskInteraction = "task_interaction"
	// payloadTypeTaskFailed 任务失败载荷类型（对齐 EventType 枚举值）
	payloadTypeTaskFailed = "task_failed"
)
```

- [ ] **Step 2: 检查并更新测试中引用 payloadType 的地方**

检查 `task_scheduler_test.go` 中 `configurableFakeTaskExecutor.ExecuteAbility` 和 `TestTaskScheduler_执行TaskInteraction`/`TestTaskScheduler_执行TaskFailed` 中的 payload type 引用，确保使用小写值。

- [ ] **Step 3: 运行测试验证**

```bash
cd /home/opensource/uap-claw-go && go test -tags test -v ./internal/agentcore/controller/modules/... -run "TestTaskScheduler"
```

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/controller/modules/task_scheduler.go internal/agentcore/controller/modules/task_scheduler_test.go
git commit -m "fix(controller): payload type 常量改为小写对齐 EventType 枚举值 (H2)"
```

---

## Task 3: M1 — AbilityMgr 改为 *ability.AbilityManager 具体类型

**Files:**
- Modify: `internal/agentcore/controller/modules/event_handler.go:17-28`
- Modify: `internal/agentcore/controller/modules/event_handler_test.go`
- Modify: `internal/agentcore/controller/modules/task_scheduler.go` (abilityMgr 字段 + NewTaskScheduler 参数)
- Modify: `internal/agentcore/controller/modules/task_executor.go:19-30` (TaskExecutorDependencies.AbilityMgr)
- Modify: `internal/agentcore/controller/modules/task_scheduler_test.go` (NewTaskScheduler 调用)

- [ ] **Step 1: 在 event_handler.go 中将 AbilityMgr 改为 *ability.AbilityManager**

添加 import:
```go
ability "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/ability"
```

修改 EventHandlerBase 结构体：
```go
type EventHandlerBase struct {
	Config        *config.ControllerConfig
	ContextEngine iface.ContextEngine
	TaskManager   *TaskManager
	TaskScheduler *TaskScheduler
	AbilityMgr    *ability.AbilityManager
}
```

- [ ] **Step 2: 在 task_scheduler.go 中将 abilityMgr 改为 *ability.AbilityManager**

修改 TaskScheduler 结构体中 `abilityMgr any` 为 `abilityMgr *ability.AbilityManager`。
修改 NewTaskScheduler 参数 `abilityMgr any` 为 `abilityMgr *ability.AbilityManager`。
添加 import `ability` 包。

- [ ] **Step 3: 在 task_executor.go 中将 AbilityMgr 改为 *ability.AbilityManager**

修改 TaskExecutorDependencies：
```go
type TaskExecutorDependencies struct {
	Config        *config.ControllerConfig
	AbilityMgr    *ability.AbilityManager
	ContextEngine iface.ContextEngine
	TaskManager   *TaskManager
	EventQueue    *EventQueue
}
```

添加 import `ability` 包。

- [ ] **Step 4: 更新测试中的 AbilityMgr 使用**

- `event_handler_test.go:63-69`：将 `AbilityMgr: "test-ability"` 改为 `AbilityMgr: nil`（测试中不需要真实 AbilityManager）
- `task_scheduler_test.go`：将 NewTaskScheduler 调用中 `abilityMgr` 参数改为 `nil`（测试中不需要真实 AbilityManager）

- [ ] **Step 5: 运行测试验证**

```bash
cd /home/opensource/uap-claw-go && go test -tags test -v ./internal/agentcore/controller/modules/...
```

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/controller/modules/
git commit -m "fix(controller): AbilityMgr 从 any 改为 *ability.AbilityManager 具体类型 (M1)"
```

---

## Task 4: M2 — PauseTask/CancelTask 返回 (bool, error)

**Files:**
- Modify: `internal/agentcore/controller/modules/task_scheduler.go:189,291` (签名)
- Modify: `internal/agentcore/controller/modules/task_scheduler.go:189-286,291-406` (实现)
- Modify: `internal/agentcore/controller/modules/task_scheduler_test.go` (调用点)

- [ ] **Step 1: 修改 PauseTask 签名和实现**

```go
func (s *TaskScheduler) PauseTask(ctx context.Context, taskID string) (bool, error)
```

实现逻辑修改：
- "任务不在 runningTasks 中" → `return false, nil`（不是错误，只是未执行）
- "session 不存在" → `return false, nil`（对齐 Python 返回 False）
- "CanPause 返回 false" → `return false, nil`
- "executor.Pause 异常" → `return false, err`
- 成功暂停 → `return true, nil`
- GetTask 系统异常 → `return false, err`

- [ ] **Step 2: 修改 CancelTask 签名和实现**

```go
func (s *TaskScheduler) CancelTask(ctx context.Context, taskID string) (bool, error)
```

实现逻辑修改：
- SUBMITTED 直接取消 → `return true, nil`
- 已终态幂等 → `return true, nil`
- "不在 runningTasks 中" → `return false, nil`
- "session 不存在" → `return false, nil`
- "CanCancel 返回 false" → `return false, nil`
- 成功取消 → `return true, nil`

- [ ] **Step 3: 更新测试中 PauseTask/CancelTask 的调用**

- `task_scheduler_test.go:471`：`err = sched.PauseTask(...)` → `ok, err := sched.PauseTask(...); assert.True(t, ok); assert.NoError(t, err)`
- `task_scheduler_test.go:496`：`err = sched.CancelTask(...)` → `ok, err := sched.CancelTask(...); assert.True(t, ok); assert.NoError(t, err)`
- `task_scheduler_test.go:543`：同上

- [ ] **Step 4: 运行测试验证**

```bash
cd /home/opensource/uap-claw-go && go test -tags test -v ./internal/agentcore/controller/modules/... -run "TestTaskScheduler_暂停|TestTaskScheduler_取消"
```

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/controller/modules/task_scheduler.go internal/agentcore/controller/modules/task_scheduler_test.go
git commit -m "fix(controller): PauseTask/CancelTask 返回 (bool, error) 对齐 Python+Go 惯例 (M2)"
```

---

## Task 5: M3 — EnsureSessionCompletionSignal 修复

**Files:**
- Modify: `internal/agentcore/controller/modules/task_scheduler.go:732-768` (方法重构)
- Modify: `internal/agentcore/controller/modules/task_scheduler.go:657-669` (executeTaskWrapper 调用点)

- [ ] **Step 1: 将 ensureSessionCompletionSignal 重构为导出方法**

删除 `sess` 参数，从 `s.sessions` map 中查找 session，补充 Payload.Data：

```go
// EnsureSessionCompletionSignal 检查并发送 all_tasks_processed 信号。
//
// 对齐 Python: TaskScheduler.ensure_session_completion_signal
// Controller.stream() 在 publish_event 返回后调用此方法，
// 使得没有新任务的轮次也能发送完成信号。
func (s *TaskScheduler) EnsureSessionCompletionSignal(ctx context.Context, sessionID string) {
	s.mu.Lock()
	suppress := s.config.SuppressCompletionSignal
	s.mu.Unlock()

	if suppress {
		logger.Debug(logComponent).
			Str("session_id", sessionID).
			Msg("完成信号被抑制")
		return
	}

	if !s.areAllTasksCompleted(ctx, sessionID) {
		return
	}

	// 从 sessions map 查找 session（对齐 Python: self._sessions.get）
	s.mu.Lock()
	sess, exists := s.sessions[sessionID]
	s.mu.Unlock()

	if !exists {
		logger.Warn(logComponent).
			Str("session_id", sessionID).
			Msg("会话不存在，无法发送完成信号")
		return
	}

	// 发送 all_tasks_processed chunk 到 session 流（补充 Payload.Data 对齐 Python）
	chunk := &schema.ControllerOutputChunk{
		Payload: &schema.ControllerOutputPayload{
			Type: schema.AllTasksProcessed,
			Data: []schema.DataFrame{
				&schema.TextDataFrame{Text: "All tasks have been successfully processed"},
			},
		},
		LastChunk: true,
	}
	if err := sess.WriteStream(ctx, chunk); err != nil {
		logger.Error(logComponent).
			Str("event_type", "LLM_CALL_ERROR").
			Str("session_id", sessionID).
			Err(err).
			Msg("写入 all_tasks_processed 信号失败")
		return
	}

	logger.Info(logComponent).
		Str("event_type", "all_tasks_processed").
		Str("session_id", sessionID).
		Msg("所有任务已处理，已发送完成信号")
}
```

- [ ] **Step 2: 修改 executeTaskWrapper 中的调用点**

行 667 改为：
```go
s.EnsureSessionCompletionSignal(ctx, tasks[0].SessionID)
```

- [ ] **Step 3: 运行测试验证**

```bash
cd /home/opensource/uap-claw-go && go test -tags test -v ./internal/agentcore/controller/modules/...
```

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/controller/modules/task_scheduler.go
git commit -m "fix(controller): EnsureSessionCompletionSignal 导出 + sessions map 查找 + 补充 Payload.Data (M3)"
```

---

## Task 6: M4 — TaskExecutorRegistry 改为内部自建

**Files:**
- Modify: `internal/agentcore/controller/modules/task_scheduler.go:81-102` (NewTaskScheduler 签名)
- Modify: `internal/agentcore/controller/modules/task_scheduler_test.go` (调用点)

- [ ] **Step 1: 删除 NewTaskScheduler 的 registry 参数，内部自建**

修改 NewTaskScheduler 签名：
```go
func NewTaskScheduler(
	cfg *config.ControllerConfig,
	taskManager *TaskManager,
	contextEngine *ability.AbilityManager,  // M1 已修改
	abilityMgr *ability.AbilityManager,      // M1 已修改
	eventQueue *EventQueue,
	card any,
) *TaskScheduler {
	return &TaskScheduler{
		config:               cfg,
		taskManager:          taskManager,
		contextEngine:        contextEngine,
		abilityMgr:           abilityMgr,
		eventQueue:           eventQueue,
		taskExecutorRegistry: NewTaskExecutorRegistry(),  // 内部自建
		sessions:             make(map[string]sessioninterfaces.SessionFacade),
		card:                 card,
		runningTasks:         make(map[string]*runningTaskEntry),
		notifyCh:             make(chan struct{}, 1),
	}
}
```

- [ ] **Step 2: 添加 TaskExecutorRegistry() getter 方法**

```go
// TaskExecutorRegistry 返回任务执行器注册表，供 Controller 逐个注册 executor builder。
// 对齐 Python: TaskScheduler.task_executor_registry property
func (s *TaskScheduler) TaskExecutorRegistry() *TaskExecutorRegistry {
	return s.taskExecutorRegistry
}
```

- [ ] **Step 3: 更新测试中 NewTaskScheduler 的调用**

删除 `task_scheduler_test.go` 中所有 `NewTaskExecutorRegistry()` 和传递 `registry` 参数的代码。

- [ ] **Step 4: 运行测试验证**

```bash
cd /home/opensource/uap-claw-go && go test -tags test -v ./internal/agentcore/controller/modules/...
```

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/controller/modules/task_scheduler.go internal/agentcore/controller/modules/task_scheduler_test.go
git commit -m "fix(controller): TaskExecutorRegistry 改为内部自建 + 添加 getter (M4)"
```

---

## Task 7: M5 — TaskExecutor 接口 pause/cancel 返回 (bool, error)

**Files:**
- Modify: `internal/agentcore/controller/modules/task_executor.go:43-55` (接口签名)
- Modify: `internal/agentcore/controller/modules/task_scheduler.go` (调用 executor.Pause/Cancel 的地方)
- Modify: `internal/agentcore/controller/modules/task_scheduler_test.go` (fakeTaskExecutor 实现)
- Modify: `internal/agentcore/controller/modules/task_executor_test.go` (fakeTaskExecutor)

- [ ] **Step 1: 修改 TaskExecutor 接口签名**

```go
type TaskExecutor interface {
	ExecuteAbility(ctx context.Context, taskID string, sess sessioninterfaces.SessionFacade) (<-chan *schema.ControllerOutputChunk, error)
	CanPause(ctx context.Context, taskID string, sess sessioninterfaces.SessionFacade) (bool, string, error)
	Pause(ctx context.Context, taskID string, sess sessioninterfaces.SessionFacade) (bool, error)
	CanCancel(ctx context.Context, taskID string, sess sessioninterfaces.SessionFacade) (bool, string, error)
	Cancel(ctx context.Context, taskID string, sess sessioninterfaces.SessionFacade) (bool, error)
}
```

- [ ] **Step 2: 更新 task_scheduler.go 中调用 executor.Pause/Cancel 的地方**

PauseTask 中 `entry.executor.Pause(...)` 调用改为处理 `(bool, error)` 返回。
CancelTask 中 `entry.executor.Cancel(...)` 调用同理。

- [ ] **Step 3: 更新测试中的 fakeTaskExecutor 实现**

修改 `configurableFakeTaskExecutor` 的 Pause/Cancel 方法签名和返回值：
```go
func (e *configurableFakeTaskExecutor) Pause(ctx context.Context, taskID string, sess sessioninterfaces.SessionFacade) (bool, error) {
	return true, e.pauseErr
}
func (e *configurableFakeTaskExecutor) Cancel(ctx context.Context, taskID string, sess sessioninterfaces.SessionFacade) (bool, error) {
	return true, e.cancelErr
}
```

修改 `task_executor_test.go` 中的 `fakeTaskExecutor` 同理。

- [ ] **Step 4: 运行测试验证**

```bash
cd /home/opensource/uap-claw-go && go test -tags test -v ./internal/agentcore/controller/modules/...
```

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/controller/modules/task_executor.go internal/agentcore/controller/modules/task_scheduler.go internal/agentcore/controller/modules/task_scheduler_test.go internal/agentcore/controller/modules/task_executor_test.go
git commit -m "fix(controller): TaskExecutor.Pause/Cancel 返回 (bool, error) (M5)"
```

---

## Task 8: M6 — Produce 拆分为 Produce + ProduceSync

**Files:**
- Modify: `internal/agentcore/runner/message_queue/queue.go:182-203` (Produce 方法)
- Modify: `internal/agentcore/runner/message_queue/queue_test.go` (所有 Produce 调用)
- Modify: `internal/agentcore/controller/modules/event_queue.go:188,241` (PublishEvent/PublishEventAsync 调用)

- [ ] **Step 1: 将 Produce 拆分为两个方法**

```go
// Produce 火忘发布消息，不等待处理结果。
func (q *MessageQueueInMemory) Produce(ctx context.Context, topic string, msg *QueueMessage) error {
	if !q.running.Load() {
		return ErrQueueNotRunning
	}
	q.mu.RLock()
	ts, exists := q.topics[topic]
	q.mu.RUnlock()
	if !exists {
		return ErrTopicNotFound
	}
	internal := &internalMessage{payload: msg.Payload}
	select {
	case ts.ch <- internal:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// ProduceSync 同步发布消息，等待处理结果。
func (q *MessageQueueInMemory) ProduceSync(ctx context.Context, topic string, invoke *InvokeQueueMessage) error {
	if !q.running.Load() {
		return ErrQueueNotRunning
	}
	q.mu.RLock()
	ts, exists := q.topics[topic]
	q.mu.RUnlock()
	if !exists {
		return ErrTopicNotFound
	}
	internal := &internalMessage{
		payload:  invoke.Payload,
		invokeCh: invoke.responseCh,
	}
	select {
	case ts.ch <- internal:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
```

- [ ] **Step 2: 更新 event_queue.go 中的调用**

PublishEvent（行 188）：
```go
invoke := message_queue.NewInvokeQueueMessage(payload)
err := eq.queue.ProduceSync(ctx, topic, invoke)
```

PublishEventAsync（行 241）：
```go
err := eq.queue.Produce(ctx, topic, message_queue.NewQueueMessage(payload))
```

- [ ] **Step 3: 更新 queue_test.go 中所有调用**

- 同步发布测试：`Produce(ctx, topic, NewQueueMessage(payload), invoke)` → `ProduceSync(ctx, topic, invoke)`
- 火忘发布测试：`Produce(ctx, topic, NewQueueMessage(payload), nil)` → `Produce(ctx, topic, NewQueueMessage(payload))`

- [ ] **Step 4: 运行测试验证**

```bash
cd /home/opensource/uap-claw-go && go test -tags test -v ./internal/agentcore/runner/message_queue/... ./internal/agentcore/controller/modules/...
```

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/runner/message_queue/queue.go internal/agentcore/runner/message_queue/queue_test.go internal/agentcore/controller/modules/event_queue.go
git commit -m "fix(message_queue): Produce 拆分为 Produce + ProduceSync (M6)"
```

---

## Task 9: M7 — Activate 去掉 timeout 参数

**Files:**
- Modify: `internal/agentcore/runner/message_queue/queue.go` (topicSubscription 新增 timeout 字段, Activate 去参数, Subscribe 传入 timeout)
- Modify: `internal/agentcore/runner/message_queue/queue_test.go` (Activate 调用去掉参数)
- Modify: `internal/agentcore/controller/modules/event_queue.go` (Subscribe 中 Activate 调用去掉参数)

- [ ] **Step 1: 给 topicSubscription 添加 timeout 字段**

```go
type topicSubscription struct {
	topic    string
	ch       chan *internalMessage
	handler  func(ctx context.Context, payload map[string]any) (any, error)
	handlerMu sync.RWMutex
	active   atomic.Bool
	cancel   context.CancelFunc
	done     chan struct{}
	timeout  time.Duration   // 新增：消息处理超时（对齐 Python SubscriptionInMemory._timeout）
}
```

- [ ] **Step 2: 在 Subscribe 中将 timeout 传入 topicSubscription**

在 MessageQueueInMemory.Subscribe 方法中，创建 topicSubscription 时设置 timeout：
```go
ts := &topicSubscription{
    topic:   topic,
    ch:      make(chan *internalMessage, q.maxSize),
    done:    make(chan struct{}),
    timeout: q.timeout,   // 从 MessageQueueInMemory.timeout 传入
}
```

- [ ] **Step 3: 修改 Activate 去掉 timeout 参数，内部使用 s.ts.timeout**

```go
func (s *Subscription) Activate() {
	s.ts.activate()
}
```

底层 `activate` 方法也去掉 timeout 参数，内部使用 `ts.timeout`：
```go
func (ts *topicSubscription) activate() {
	// ... 启动消费 goroutine，使用 ts.timeout 作为超时
}
```

- [ ] **Step 4: 更新所有调用 Activate 的地方**

- `queue_test.go`：`sub.Activate(timeout)` → `sub.Activate()`
- `event_queue.go` Subscribe 中：`sub.Activate(timeout)` → `sub.Activate()`

- [ ] **Step 5: 运行测试验证**

```bash
cd /home/opensource/uap-claw-go && go test -tags test -v ./internal/agentcore/runner/message_queue/... ./internal/agentcore/controller/modules/...
```

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/runner/message_queue/queue.go internal/agentcore/runner/message_queue/queue_test.go internal/agentcore/controller/modules/event_queue.go
git commit -m "fix(message_queue): Activate 去掉 timeout 参数，Subscription 自身持有 (M7)"
```

---

## Task 10: 全量测试 + 覆盖率检查

**Files:** 无新文件

- [ ] **Step 1: 运行全量单元测试**

```bash
cd /home/opensource/uap-claw-go && go test -tags test -v ./internal/agentcore/controller/... ./internal/agentcore/runner/message_queue/...
```

- [ ] **Step 2: 检查覆盖率**

```bash
cd /home/opensource/uap-claw-go && go test -tags test -cover ./internal/agentcore/controller/schema/... ./internal/agentcore/controller/modules/... ./internal/agentcore/runner/message_queue/...
```

确认覆盖率 ≥ 85%。

- [ ] **Step 3: 更新 IMPLEMENTATION_PLAN.md**

确认 6.20 和 6.21 状态仍为 ✅（本次修复不改变完成状态）。

- [ ] **Step 4: 最终提交**

```bash
git add -A
git commit -m "chore: 6.20+6.21 偏差修复完成，全量测试通过"
```
