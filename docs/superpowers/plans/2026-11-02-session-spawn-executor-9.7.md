# 9.7 SessionSpawnExecutor 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 DeepAgent 的会话子进程执行器（SessionSpawnExecutor）及其配套组件（SessionToolkit + 3个工具），完成 session_spawn_task 类型任务的全链路执行，回填所有 ⤵️ 9.7 标注。

**Architecture:** 严格镜像 Python 目录结构。SessionSpawnExecutor 放在 task_loop/session_spawn_executor.go，SessionToolkit + 三个工具放在 tools/subagent/session_tools.go。通过 DeepAgentProvider 接口扩展 CreateSubagent 方法解耦对 9.1 DeepAgent 的依赖。

**Tech Stack:** Go 1.23+, 标准库 sync/uuid/crypto, 项目内 controller/schema/modules/foundation/tool 类型

---

## 文件结构

| 操作 | 文件路径 | 职责 |
|------|---------|------|
| 新建 | `internal/agentcore/harness/tools/subagent/doc.go` | subagent 包文档 |
| 新建 | `internal/agentcore/harness/tools/subagent/session_tools.go` | SessionTaskRow + SessionToolkit + 3个工具 + BuildSessionTools |
| 新建 | `internal/agentcore/harness/tools/subagent/session_tools_test.go` | SessionToolkit + 工具测试 |
| 新建 | `internal/agentcore/harness/task_loop/session_spawn_executor.go` | SessionSpawnExecutor + BuildSessionSpawnExecutor |
| 新建 | `internal/agentcore/harness/task_loop/session_spawn_executor_test.go` | SessionSpawnExecutor 测试 |
| 修改 | `internal/agentcore/harness/task_loop/executor.go` | DeepAgentProvider 扩展 CreateSubagent + 注释更新 |
| 修改 | `internal/agentcore/harness/task_loop/handler.go` | sessionToolkit 类型具体化 + completeSessionSpawn + 辅助方法 |
| 修改 | `internal/agentcore/harness/task_loop/handler_test.go` | 更新 SessionSpawn 分支测试 |
| 修改 | `internal/agentcore/harness/task_loop/doc.go` | 文件目录添加 session_spawn_executor.go |
| 修改 | `IMPLEMENTATION_PLAN.md` | 9.7 状态 ☐ → ✅ |

---

### Task 1: 创建 tools/subagent 包 + SessionTaskRow + SessionToolkit

**Files:**
- Create: `internal/agentcore/harness/tools/subagent/doc.go`
- Create: `internal/agentcore/harness/tools/subagent/session_tools.go`
- Create: `internal/agentcore/harness/tools/subagent/session_tools_test.go`

- [ ] **Step 1: 创建 subagent 包目录和 doc.go**

先确认目录存在：
```bash
mkdir -p internal/agentcore/harness/tools/subagent
```

创建 `doc.go`：
```go
// Package subagent 提供子代理会话工具集，包含异步子任务派生（SessionsSpawnTool）、
// 任务状态跟踪（SessionToolkit）和任务列表/取消操作。
//
// 对齐 Python: openjiuwen/harness/tools/subagent/
//
// 文件目录：
//
//	subagent/
//	├── doc.go              # 包文档
//	└── session_tools.go    # SessionTaskRow + SessionToolkit + SessionsList/Spawn/Cancel 工具
//
// 对应 Python 代码：openjiuwen/harness/tools/subagent/session_tools.py
package subagent
```

- [ ] **Step 2: 在 session_tools.go 中编写 SessionTaskRow + SessionToolkit 结构体和方法**

对照 Python `SessionTaskRow`（第28-36行）和 `SessionToolkit`（第40-84行）。

创建 `session_tools.go`，按 Go 编码规范排列（结构体 → 常量 → 全局变量 → 导出函数 → 非导出函数）：

```go
package subagent

import (
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SessionTaskRow 会话任务行（业务视图）。
// 对齐 Python: SessionTaskRow
type SessionTaskRow struct {
	// TaskID 任务标识
	TaskID string `json:"task_id"`
	// SubSessionID 子会话标识
	SubSessionID string `json:"sub_session_id"`
	// Description 任务描述
	Description string `json:"description"`
	// Status 任务状态
	Status string `json:"status"`
	// Result 执行结果
	Result string `json:"result,omitempty"`
	// Error 错误信息
	Error string `json:"error,omitempty"`
}

// SessionToolkit 会话任务注册表，跟踪异步子任务状态。
// 对齐 Python: SessionToolkit
type SessionToolkit struct {
	// rows 任务行映射
	rows map[string]*SessionTaskRow
	// mu 读写互斥锁
	mu sync.RWMutex
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSessionToolkit 创建会话任务注册表。
func NewSessionToolkit() *SessionToolkit {
	return &SessionToolkit{
		rows: make(map[string]*SessionTaskRow),
	}
}

// UpsertRunning 插入或更新任务为运行中状态。
// 对齐 Python: SessionToolkit.upsert_running
func (t *SessionToolkit) UpsertRunning(taskID, subSessionID, description string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.rows[taskID] = &SessionTaskRow{
		TaskID:       taskID,
		SubSessionID: subSessionID,
		Description:  description,
		Status:       "running",
	}
}

// MarkCompleted 标记任务为已完成。
// 对齐 Python: SessionToolkit.mark_completed
func (t *SessionToolkit) MarkCompleted(taskID, result string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if row, ok := t.rows[taskID]; ok {
		row.Status = "completed"
		row.Result = result
	}
}

// MarkFailed 标记任务为已失败。
// 对齐 Python: SessionToolkit.mark_failed
func (t *SessionToolkit) MarkFailed(taskID, err string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if row, ok := t.rows[taskID]; ok {
		row.Status = "error"
		row.Error = err
	}
}

// MarkCanceled 标记任务为已取消。
// 对齐 Python: SessionToolkit.mark_canceled
func (t *SessionToolkit) MarkCanceled(taskID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if row, ok := t.rows[taskID]; ok {
		row.Status = "canceled"
	}
}

// ListAll 返回所有任务行。
// 对齐 Python: SessionToolkit.list_all
func (t *SessionToolkit) ListAll() []*SessionTaskRow {
	t.mu.RLock()
	defer t.mu.RUnlock()
	result := make([]*SessionTaskRow, 0, len(t.rows))
	for _, row := range t.rows {
		result = append(result, row)
	}
	return result
}

// Get 按 ID 获取任务行。
// 对齐 Python: SessionToolkit.get
func (t *SessionToolkit) Get(taskID string) *SessionTaskRow {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.rows[taskID]
}

// Clear 清空所有任务行。
// 对齐 Python: SessionToolkit.clear
func (t *SessionToolkit) Clear() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.rows = make(map[string]*SessionTaskRow)
}
```

- [ ] **Step 3: 编写 SessionToolkit 的测试**

创建 `session_tools_test.go`：

```go
package subagent

import (
	"testing"
)

// TestNewSessionToolkit 测试创建 SessionToolkit
func TestNewSessionToolkit(t *testing.T) {
	tk := NewSessionToolkit()
	if tk == nil {
		t.Fatal("NewSessionToolkit 返回 nil")
	}
	if len(tk.ListAll()) != 0 {
		t.Fatal("新创建的 SessionToolkit 应为空")
	}
}

// TestSessionToolkit_UpsertRunning 测试插入运行任务
func TestSessionToolkit_UpsertRunning(t *testing.T) {
	tk := NewSessionToolkit()
	tk.UpsertRunning("task-1", "sub-1", "研究A方向")
	row := tk.Get("task-1")
	if row == nil {
		t.Fatal("应找到 task-1")
	}
	if row.Status != "running" {
		t.Fatalf("期望 running, 实际 %s", row.Status)
	}
	if row.SubSessionID != "sub-1" || row.Description != "研究A方向" {
		t.Fatalf("字段不匹配: %+v", row)
	}
}

// TestSessionToolkit_MarkCompleted 测试标记完成
func TestSessionToolkit_MarkCompleted(t *testing.T) {
	tk := NewSessionToolkit()
	tk.UpsertRunning("task-1", "sub-1", "研究A方向")
	tk.MarkCompleted("task-1", "研究结果")
	row := tk.Get("task-1")
	if row.Status != "completed" {
		t.Fatalf("期望 completed, 实际 %s", row.Status)
	}
	if row.Result != "研究结果" {
		t.Fatalf("期望 研究结果, 实际 %s", row.Result)
	}
}

// TestSessionToolkit_MarkFailed 测试标记失败
func TestSessionToolkit_MarkFailed(t *testing.T) {
	tk := NewSessionToolkit()
	tk.UpsertRunning("task-1", "sub-1", "研究A方向")
	tk.MarkFailed("task-1", "网络错误")
	row := tk.Get("task-1")
	if row.Status != "error" {
		t.Fatalf("期望 error, 实际 %s", row.Status)
	}
	if row.Error != "网络错误" {
		t.Fatalf("期望 网络错误, 实际 %s", row.Error)
	}
}

// TestSessionToolkit_MarkCanceled 测试标记取消
func TestSessionToolkit_MarkCanceled(t *testing.T) {
	tk := NewSessionToolkit()
	tk.UpsertRunning("task-1", "sub-1", "研究A方向")
	tk.MarkCanceled("task-1")
	row := tk.Get("task-1")
	if row.Status != "canceled" {
		t.Fatalf("期望 canceled, 实际 %s", row.Status)
	}
}

// TestSessionToolkit_MarkCompleted_不存在的任务 测试标记不存在任务无副作用
func TestSessionToolkit_MarkCompleted_不存在的任务(t *testing.T) {
	tk := NewSessionToolkit()
	tk.MarkCompleted("nonexistent", "result")
	if row := tk.Get("nonexistent"); row != nil {
		t.Fatal("不应创建不存在的任务行")
	}
}

// TestSessionToolkit_ListAll 测试列出所有任务
func TestSessionToolkit_ListAll(t *testing.T) {
	tk := NewSessionToolkit()
	tk.UpsertRunning("task-1", "sub-1", "任务1")
	tk.UpsertRunning("task-2", "sub-2", "任务2")
	all := tk.ListAll()
	if len(all) != 2 {
		t.Fatalf("期望 2, 实际 %d", len(all))
	}
}

// TestSessionToolkit_Clear 测试清空
func TestSessionToolkit_Clear(t *testing.T) {
	tk := NewSessionToolkit()
	tk.UpsertRunning("task-1", "sub-1", "任务1")
	tk.Clear()
	if len(tk.ListAll()) != 0 {
		t.Fatal("清空后应为空")
	}
}

// TestSessionToolkit_UpsertRunning_覆盖 测试重复 upsert 覆盖
func TestSessionToolkit_UpsertRunning_覆盖(t *testing.T) {
	tk := NewSessionToolkit()
	tk.UpsertRunning("task-1", "sub-1", "旧描述")
	tk.UpsertRunning("task-1", "sub-2", "新描述")
	row := tk.Get("task-1")
	if row.Description != "新描述" {
		t.Fatalf("期望 新描述, 实际 %s", row.Description)
	}
	if row.SubSessionID != "sub-2" {
		t.Fatalf("期望 sub-2, 实际 %s", row.SubSessionID)
	}
	if row.Status != "running" {
		t.Fatalf("期望 running, 实际 %s", row.Status)
	}
}
```

- [ ] **Step 4: 运行测试确认通过**

```bash
cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/harness/tools/subagent/... -v -count=1
```

Expected: PASS，所有测试通过

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/harness/tools/subagent/ && git commit -m "feat(9.7): 新增 subagent 包 + SessionTaskRow + SessionToolkit 及测试"
```

---

### Task 2: 创建 SessionSpawnExecutor

**Files:**
- Create: `internal/agentcore/harness/task_loop/session_spawn_executor.go`
- Create: `internal/agentcore/harness/task_loop/session_spawn_executor_test.go`

- [ ] **Step 1: 扩展 DeepAgentProvider 接口（executor.go）**

在 `executor.go` 的 `DeepAgentProvider` 接口中添加 `CreateSubagent` 方法（在 `ScheduleAutoInvokeOnSpawnDone` 之后）：

```go
// CreateSubagent 创建子 Agent 实例。
// ⤵️ 9.1 回填：9.1 实现 DeepAgent 后，由 *DeepAgent.CreateSubagent 实现。
// 对齐 Python: DeepAgent.create_subagent
CreateSubagent(subagentType string, subSessionID string) (DeepAgentProvider, error)
```

- [ ] **Step 2: 编写 SessionSpawnExecutor**

创建 `session_spawn_executor.go`，对照 Python `session_spawn_executor.py` 完整实现：

```go
package task_loop

import (
	"context"
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller/modules"
	cschema "github.com/uapclaw/uapclaw-go/internal/agentcore/controller/schema"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SessionSpawnExecutor 会话子进程执行器，执行 SESSION_SPAWN_TASK_TYPE 类型任务。
// 从 TaskManager 获取任务元数据，提取 subagent_type/sub_session_id，
// 通过 DeepAgent.create_subagent 创建子 Agent 并 invoke。
// 对齐 Python: SessionSpawnExecutor
type SessionSpawnExecutor struct {
	// deps 任务执行器依赖
	deps *modules.TaskExecutorDependencies
	// provider 深层 Agent 提供者（用于 CreateSubagent）
	provider DeepAgentProvider
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSessionSpawnExecutor 创建会话子进程执行器。
// 对齐 Python: SessionSpawnExecutor.__init__
func NewSessionSpawnExecutor(deps *modules.TaskExecutorDependencies, provider DeepAgentProvider) *SessionSpawnExecutor {
	return &SessionSpawnExecutor{
		deps:     deps,
		provider: provider,
	}
}

// ExecuteAbility 执行子 Agent 任务。
// 获取任务元数据 → 创建子 Agent → invoke → 发送完成/失败事件。
// 对齐 Python: SessionSpawnExecutor.execute_ability
func (e *SessionSpawnExecutor) ExecuteAbility(
	ctx context.Context,
	taskID string,
	sess sessioninterfaces.SessionFacade,
) (<-chan *stream.OutputSchema, error) {
	ch := make(chan *stream.OutputSchema, 1)

	// 步骤 1：获取任务元数据
	tasks, err := e.deps.TaskManager.GetTask(ctx, MakeFilter(taskID))
	if err != nil {
		logger.Error(logComponent).
			Err(err).
			Str("task_id", taskID).
			Str("event_type", "LLM_CALL_ERROR").
			Str("method", "SessionSpawnExecutor.ExecuteAbility").
			Msg("查询任务失败")
		ch <- e.buildErrorChunk(taskID, err.Error())
		close(ch)
		return ch, nil
	}
	if len(tasks) == 0 {
		logger.Warn(logComponent).
			Str("task_id", taskID).
			Msg("未找到任务")
		ch <- e.buildErrorChunk(taskID, "Task not found")
		close(ch)
		return ch, nil
	}

	task := tasks[0]
	meta := task.Metadata
	if meta == nil {
		meta = make(map[string]any)
	}

	// 步骤 2：提取元数据
	subagentType, _ := meta["subagent_type"].(string)
	if subagentType == "" {
		subagentType = "general-purpose"
	}
	query, _ := meta["task_description"].(string)
	cid, _ := meta["sub_session_id"].(string)

	// 步骤 3：日志
	logger.Info(logComponent).
		Str("task_id", taskID).
		Str("subagent_type", subagentType).
		Str("sub_session_id", cid).
		Msg("开始执行 SessionSpawn 任务")

	// 步骤 4-8：异步执行子 Agent
	go func() {
		defer close(ch)

		// 步骤 4：创建子 Agent
		subAgent, createErr := e.provider.CreateSubagent(subagentType, cid)
		if createErr != nil {
			logger.Error(logComponent).
				Err(createErr).
				Str("task_id", taskID).
				Str("subagent_type", subagentType).
				Str("event_type", "LLM_CALL_ERROR").
				Msg("创建子 Agent 失败")
			ch <- e.buildErrorChunk(taskID, createErr.Error())
			return
		}

		// 步骤 5：调用子 Agent
		effective := map[string]any{
			"query":           query,
			"conversation_id": cid,
		}
		result, invokeErr := subAgent.ReactAgent().Invoke(ctx, effective)

		if invokeErr != nil {
			// 步骤 9a：异常路径
			logger.Error(logComponent).
				Err(invokeErr).
				Str("task_id", taskID).
				Str("event_type", "LLM_CALL_ERROR").
				Msg("SessionSpawn 任务执行失败")
			ch <- e.buildErrorChunk(taskID, invokeErr.Error())
			return
		}

		// 步骤 6：提取输出
		var payload string
		if result != nil {
			if output, ok := result["output"]; ok {
				payload = fmt.Sprintf("%v", output)
			}
		}

		// 步骤 7：完成日志
		logger.Info(logComponent).
			Str("task_id", taskID).
			Int("output_len", len(payload)).
			Msg("SessionSpawn 任务执行完成")

		// 步骤 8：发送完成事件
		ch <- &stream.OutputSchema{
			Type: string(cschema.EventTaskCompletion),
			Payload: &cschema.ControllerOutputPayload{
				Type: string(cschema.EventTaskCompletion),
				Data: []cschema.DataFrame{&cschema.JsonDataFrame{Data: map[string]any{"output": payload}}},
				Metadata: map[string]any{
					"task_id":   taskID,
					"task_type": SessionSpawnTaskType,
				},
			},
			IsLastSchema: true,
		}
	}()

	return ch, nil
}

// CanPause 检查任务是否可暂停。SessionSpawn 任务不支持暂停。
// 对齐 Python: SessionSpawnExecutor.can_pause
func (e *SessionSpawnExecutor) CanPause(_ context.Context, _ string, _ sessioninterfaces.SessionFacade) (bool, string, error) {
	return false, "Session spawn 任务不支持暂停", nil
}

// Pause 暂停任务。SessionSpawn 任务不支持暂停，始终返回 false。
// 对齐 Python: SessionSpawnExecutor.pause
func (e *SessionSpawnExecutor) Pause(_ context.Context, _ string, _ sessioninterfaces.SessionFacade) (bool, error) {
	return false, nil
}

// CanCancel 检查任务是否可取消。SessionSpawn 任务始终可取消。
// 对齐 Python: SessionSpawnExecutor.can_cancel
func (e *SessionSpawnExecutor) CanCancel(_ context.Context, _ string, _ sessioninterfaces.SessionFacade) (bool, string, error) {
	return true, "", nil
}

// Cancel 取消任务。已在 TaskScheduler 中取消，此处直接返回 true。
// 对齐 Python: SessionSpawnExecutor.cancel
func (e *SessionSpawnExecutor) Cancel(_ context.Context, taskID string, _ sessioninterfaces.SessionFacade) (bool, error) {
	logger.Info(logComponent).
		Str("task_id", taskID).
		Msg("取消 SessionSpawn 任务")
	return true, nil
}

// BuildSessionSpawnExecutor 构建 SessionSpawnTaskType 执行器的工厂闭包。
// 返回的闭包捕获 provider，供 TaskExecutorRegistry 注册。
// 对齐 Python: build_session_spawn_executor
func BuildSessionSpawnExecutor(provider DeepAgentProvider) func(deps *modules.TaskExecutorDependencies) modules.TaskExecutor {
	return func(deps *modules.TaskExecutorDependencies) modules.TaskExecutor {
		return NewSessionSpawnExecutor(deps, provider)
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// buildErrorChunk 构建错误输出分片。
// 对齐 Python: SessionSpawnExecutor._build_error_chunk
func (e *SessionSpawnExecutor) buildErrorChunk(taskID string, errMsg string) *stream.OutputSchema {
	return &stream.OutputSchema{
		Type: string(cschema.EventTaskFailed),
		Payload: &cschema.ControllerOutputPayload{
			Type: string(cschema.EventTaskFailed),
			Data: []cschema.DataFrame{&cschema.TextDataFrame{Text: errMsg}},
			Metadata: map[string]any{
				"task_id":   taskID,
				"task_type": SessionSpawnTaskType,
			},
		},
		IsLastSchema: true,
	}
}

// 编译时接口检查：SessionSpawnExecutor 必须满足 modules.TaskExecutor
var _ modules.TaskExecutor = (*SessionSpawnExecutor)(nil)
```

- [ ] **Step 3: 编写 SessionSpawnExecutor 测试**

创建 `session_spawn_executor_test.go`，使用 fake DeepAgentProvider：

```go
package task_loop

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller/modules"
	cschema "github.com/uapclaw/uapclaw-go/internal/agentcore/controller/schema"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/agents"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
)

// fakeSessionSpawnProvider 测试用 DeepAgentProvider mock
type fakeSessionSpawnProvider struct {
	fakeDeepAgentProvider
	// subagent 子 Agent 提供者（CreateSubagent 返回值）
	subagent DeepAgentProvider
	// createErr CreateSubagent 错误
	createErr error
}

func (f *fakeSessionSpawnProvider) CreateSubagent(subagentType string, subSessionID string) (DeepAgentProvider, error) {
	return f.subagent, f.createErr
}

// TestNewSessionSpawnExecutor 测试创建执行器
func TestNewSessionSpawnExecutor(t *testing.T) {
	deps := &modules.TaskExecutorDependencies{}
	provider := &fakeSessionSpawnProvider{}
	executor := NewSessionSpawnExecutor(deps, provider)
	if executor == nil {
		t.Fatal("NewSessionSpawnExecutor 返回 nil")
	}
}

// TestSessionSpawnExecutor_CanPause 测试不支持暂停
func TestSessionSpawnExecutor_CanPause(t *testing.T) {
	executor := NewSessionSpawnExecutor(nil, nil)
	ok, reason, err := executor.CanPause(context.Background(), "task-1", nil)
	if ok || err != nil {
		t.Fatalf("期望 (false, nil), 实际 (%v, %v)", ok, err)
	}
	if reason == "" {
		t.Fatal("应有不可暂停原因")
	}
}

// TestSessionSpawnExecutor_Pause 测试暂停返回 false
func TestSessionSpawnExecutor_Pause(t *testing.T) {
	executor := NewSessionSpawnExecutor(nil, nil)
	ok, err := executor.Pause(context.Background(), "task-1", nil)
	if ok || err != nil {
		t.Fatalf("期望 (false, nil), 实际 (%v, %v)", ok, err)
	}
}

// TestSessionSpawnExecutor_CanCancel 测试支持取消
func TestSessionSpawnExecutor_CanCancel(t *testing.T) {
	executor := NewSessionSpawnExecutor(nil, nil)
	ok, reason, err := executor.CanCancel(context.Background(), "task-1", nil)
	if !ok || err != nil {
		t.Fatalf("期望 (true, nil), 实际 (%v, %v)", ok, err)
	}
	if reason != "" {
		t.Fatalf("不应有不可取消原因, 实际: %s", reason)
	}
}

// TestSessionSpawnExecutor_Cancel 测试取消返回 true
func TestSessionSpawnExecutor_Cancel(t *testing.T) {
	executor := NewSessionSpawnExecutor(nil, nil)
	ok, err := executor.Cancel(context.Background(), "task-1", nil)
	if !ok || err != nil {
		t.Fatalf("期望 (true, nil), 实际 (%v, %v)", ok, err)
	}
}

// TestBuildSessionSpawnExecutor 测试工厂闭包
func TestBuildSessionSpawnExecutor(t *testing.T) {
	provider := &fakeSessionSpawnProvider{}
	factory := BuildSessionSpawnExecutor(provider)
	if factory == nil {
		t.Fatal("BuildSessionSpawnExecutor 返回 nil")
	}
	deps := &modules.TaskExecutorDependencies{}
	executor := factory(deps)
	if executor == nil {
		t.Fatal("工厂闭包返回 nil")
	}
}

// TestSessionSpawnExecutor_ExecuteAbility_任务未找到 测试任务不存在时发送错误分片
func TestSessionSpawnExecutor_ExecuteAbility_任务未找到(t *testing.T) {
	// 使用空 TaskManager（无任务），期望收到 TASK_FAILED 分片
	tm := modules.NewTaskManager(nil)
	deps := &modules.TaskExecutorDependencies{TaskManager: tm}
	provider := &fakeSessionSpawnProvider{}
	executor := NewSessionSpawnExecutor(deps, provider)

	ch, err := executor.ExecuteAbility(context.Background(), "nonexistent-task", nil)
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	out := <-ch
	if out.Type != string(cschema.EventTaskFailed) {
		t.Fatalf("期望 TASK_FAILED, 实际 %s", out.Type)
	}
}

// TestSessionSpawnExecutor_buildErrorChunk 测试错误分片构建
func TestSessionSpawnExecutor_buildErrorChunk(t *testing.T) {
	executor := NewSessionSpawnExecutor(nil, nil)
	chunk := executor.buildErrorChunk("task-1", "网络错误")
	if chunk.Type != string(cschema.EventTaskFailed) {
		t.Fatalf("期望 TASK_FAILED, 实际 %s", chunk.Type)
	}
	if !chunk.IsLastSchema {
		t.Fatal("应为最后分片")
	}
	taskType, _ := chunk.Payload.Metadata["task_type"].(string)
	if taskType != SessionSpawnTaskType {
		t.Fatalf("期望 %s, 实际 %s", SessionSpawnTaskType, taskType)
	}
}
```

- [ ] **Step 4: 运行测试确认通过**

```bash
cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/harness/task_loop/... -v -count=1 -run "SessionSpawn"
```

Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/harness/task_loop/session_spawn_executor.go internal/agentcore/harness/task_loop/session_spawn_executor_test.go internal/agentcore/harness/task_loop/executor.go && git commit -m "feat(9.7): 新增 SessionSpawnExecutor + DeepAgentProvider.CreateSubagent 扩展及测试"
```

---

### Task 3: 在 session_tools.go 中实现三个工具

**Files:**
- Modify: `internal/agentcore/harness/tools/subagent/session_tools.go`
- Modify: `internal/agentcore/harness/tools/subagent/session_tools_test.go`

- [ ] **Step 1: 在 session_tools.go 中添加 SessionsListTool + SessionsSpawnTool + SessionsCancelTool + BuildSessionTools**

对照 Python `session_tools.py`（87-367行）。使用 `foundation/tool.MapFunction` 模式。在文件末尾添加：

```go
import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"

	"github.com/google/uuid"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller/modules"
	cschema "github.com/uapclaw/uapclaw-go/internal/agentcore/controller/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/task_loop"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// logComponent 日志组件标识
	logComponent = logger.ComponentAgentCore
)

// ──────────────────────────── 结构体（续）────────────────────────────

// SessionsListTool 查看所有后台异步子任务的工具。
// 对齐 Python: SessionsListTool
type SessionsListTool struct {
	// toolkit 会话任务注册表
	toolkit *SessionToolkit
	// language 语言
	language string
}

// SessionsSpawnTool 创建异步后台子代理任务的工具。
// 对齐 Python: SessionsSpawnTool
type SessionsSpawnTool struct {
	// provider 深层 Agent 提供者
	provider task_loop.DeepAgentProvider
	// toolkit 会话任务注册表
	toolkit *SessionToolkit
	// language 语言
	language string
}

// SessionsCancelTool 取消后台异步子任务的工具。
// 对齐 Python: SessionsCancelTool
type SessionsCancelTool struct {
	// provider 深层 Agent 提供者
	provider task_loop.DeepAgentProvider
	// toolkit 会话任务注册表
	toolkit *SessionToolkit
	// language 语言
	language string
}
```

然后实现每个工具的 `invoke` 方法（使用 MapFunction 包装），以及 `BuildSessionTools` 工厂函数。SessionsSpawnTool.Invoke 的核心逻辑对照 Python 259-327行；SessionsCancelTool.Invoke 对照 Python 146-229行；SessionsListTool.Invoke 对照 Python 97-118行。

- [ ] **Step 2: 编写三个工具的测试**

在 `session_tools_test.go` 中添加工具 Invoke 测试。

- [ ] **Step 3: 运行测试确认通过**

```bash
cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/harness/tools/subagent/... -v -count=1
```

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/harness/tools/subagent/ && git commit -m "feat(9.7): 新增 SessionsList/Spawn/Cancel 工具及 BuildSessionTools 工厂"
```

---

### Task 4: 回填 handler.go — sessionToolkit 类型具体化 + completeSessionSpawn + 辅助方法

**Files:**
- Modify: `internal/agentcore/harness/task_loop/handler.go`
- Modify: `internal/agentcore/harness/task_loop/handler_test.go`

- [ ] **Step 1: 修改 handler.go — sessionToolkit 字段类型和 SetSessionToolkit 参数**

将 `sessionToolkit any` 改为 `sessionToolkit *subagent.SessionToolkit`，更新 `SetSessionToolkit` 参数类型。添加 import `"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/tools/subagent"`。

- [ ] **Step 2: 在 handler.go 中添加 completeSessionSpawn 方法**

对照 Python `_complete_session_spawn`（530-587行），添加完整实现。

- [ ] **Step 3: 在 handler.go 中添加 extractResultFromEvent + extractErrorFromEvent + formatSessionSpawnSteer 辅助方法**

对照 Python（590-633行）。

- [ ] **Step 4: 修改 HandleTaskCompletion 中 SessionSpawn 分支**

将占位代码替换为调用 `completeSessionSpawn(taskID, input, false)`，返回包含 `task_id` 的结果。

- [ ] **Step 5: 修改 HandleTaskFailed 中 SessionSpawn 分支**

将占位代码替换为调用 `completeSessionSpawn(taskID, input, true)`，返回包含 `task_id` + `error` 的结果。

- [ ] **Step 6: 更新 handler_test.go 中 SessionSpawn 相关测试**

更新 `TestTaskLoopEventHandler_HandleTaskCompletion_SessionSpawn` 和 `TestTaskLoopEventHandler_HandleTaskFailed_SessionSpawn`，验证 toolkit.MarkCompleted/MarkFailed 调用。添加 `TestExtractResultFromEvent`、`TestExtractErrorFromEvent`、`TestFormatSessionSpawnSteer` 测试。

- [ ] **Step 7: 运行测试确认通过**

```bash
cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/harness/task_loop/... -v -count=1
```

- [ ] **Step 8: 提交**

```bash
git add internal/agentcore/harness/task_loop/handler.go internal/agentcore/harness/task_loop/handler_test.go && git commit -m "feat(9.7): 回填 handler.go — completeSessionSpawn + 辅助方法 + SessionToolkit 类型具体化"
```

---

### Task 5: 回填 executor.go 注释 + 更新 doc.go

**Files:**
- Modify: `internal/agentcore/harness/task_loop/executor.go`
- Modify: `internal/agentcore/harness/task_loop/doc.go`

- [ ] **Step 1: 更新 executor.go 中 ⤵️ 9.7 回填注释**

将第65行 `⤵️ 9.7 回填：注册到 TaskExecutorRegistry` 更新为 `✅ 9.7 已实现 BuildSessionSpawnExecutor，在 9.1 的 _setup_task_loop 中注册到 TaskExecutorRegistry`。

同理更新第68-69行和第398行的注释。

- [ ] **Step 2: 更新 task_loop/doc.go 文件目录**

在文件目录树中添加 `session_spawn_executor.go` 和 `session_spawn_executor_test.go` 条目。

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/harness/task_loop/executor.go internal/agentcore/harness/task_loop/doc.go && git commit -m "docs(9.7): 更新 executor.go 回填注释 + doc.go 文件目录"
```

---

### Task 6: 全量编译 + 覆盖率检查 + IMPLEMENTATION_PLAN.md 更新

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 全量编译检查**

```bash
cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./...
```

Expected: 编译成功，无错误

- [ ] **Step 2: 运行全量测试 + 覆盖率**

```bash
cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test -cover ./internal/agentcore/harness/task_loop/... ./internal/agentcore/harness/tools/subagent/...
```

Expected: 所有测试通过，覆盖率 ≥ 85%

- [ ] **Step 3: 更新 IMPLEMENTATION_PLAN.md 中 9.7 状态**

将 `9.7 | ☐ | SessionSpawnExecutor` 改为 `9.7 | ✅ | SessionSpawnExecutor`

- [ ] **Step 4: 提交**

```bash
git add IMPLEMENTATION_PLAN.md && git commit -m "docs: 9.7 SessionSpawnExecutor 状态更新为 ✅"
```
