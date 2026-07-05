# 9.6 TaskLoopEventExecutor + TaskLoopEventHandler 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 TaskLoopEventExecutor（将内层 ReActAgent 包装为 TaskExecutor）和 TaskLoopEventHandler（事件路由器 + per-round channel 模式），完成 9.6 小节。

**Architecture:** Executor 实现TaskExecutor 接口，持有 DeepAgentProvider 临时接口解耦 DeepAgent；Handler 实现 EventHandler 接口 + interactionQueuesProvider 接口，使用 per-round channel + round_id 模式同步等待异步完成。SessionSpawn 相关逻辑用 any 占位 + 注释标注回填。

**Tech Stack:** Go 1.22+, zerolog 日志, channel 并发模式

**Spec:** `docs/superpowers/specs/2026-11-01-task-loop-event-executor-9.6-design.md`

---

## 文件结构

| 操作 | 文件 | 职责 |
|------|------|------|
| 新建 | `internal/agentcore/harness/task_loop/executor.go` | DeepAgentProvider 接口 + TaskLoopEventExecutor + BuildDeepExecutor |
| 新建 | `internal/agentcore/harness/task_loop/handler.go` | TaskLoopEventHandler（per-round channel + 事件路由） |
| 新建 | `internal/agentcore/harness/task_loop/executor_test.go` | Executor 单元测试 |
| 新建 | `internal/agentcore/harness/task_loop/handler_test.go` | Handler 单元测试 |
| 修改 | `internal/agentcore/harness/task_loop/doc.go` | 添加 executor.go + handler.go 条目 |
| 修改 | `internal/agentcore/harness/task_loop/controller.go` | 更新 interactionQueuesProvider 接口注释（⤴️ 9.6 回填） |

---

### Task 1: executor.go — DeepAgentProvider 接口 + DeepTaskType 常量 + TaskLoopEventExecutor 结构体骨架

**Files:**
- Create: `internal/agentcore/harness/task_loop/executor.go`

- [ ] **Step 1: 创建 executor.go，写入接口定义 + 常量 + 结构体骨架**

```go
package task_loop

import (
	"context"
	"os"
	"strconv"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller/modules"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema as harnessschema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces as sessioninterfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interaction"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/agents"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/rail"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 接口 ────────────────────────────

// DeepAgentProvider DeepAgent 临时解耦接口，提供 Executor/Handler 所需的运行时访问。
// ⤵️ 9.1 回填：9.1 实现 DeepAgent 后，直接用 *DeepAgent 替换此接口，
// 并删除 DeepAgentProvider 定义。此接口不是真实实现，仅用于解耦编译依赖。
type DeepAgentProvider interface {
	// ReactAgent 返回内层 ReActAgent 实例
	ReactAgent() *agents.ReActAgent
	// LoopCoordinator 返回循环协调器
	LoopCoordinator() *LoopCoordinator
	// EventHandler 返回事件处理器
	EventHandler() modules.EventHandler
	// LoadState 从会话加载 DeepAgentState
	LoadState(sess sessioninterfaces.SessionFacade) *harnessschema.DeepAgentState
	// DeepConfig 返回 DeepAgent 配置
	DeepConfig() *harnessschema.DeepAgentConfig
	// IsInvokeActive 判断是否有活跃的 invoke
	IsInvokeActive() bool
	// IsAutoInvokeScheduled 判断是否已调度自动 invoke
	IsAutoInvokeScheduled() bool
	// SetAutoInvokeScheduled 设置自动 invoke 调度标记
	SetAutoInvokeScheduled(scheduled bool)
	// ScheduleAutoInvokeOnSpawnDone 延迟调度自动 invoke
	// ⤵️ 9.7 回填
	ScheduleAutoInvokeOnSpawnDone(steerText string) error
}

// ──────────────────────────── 常量 ────────────────────────────

// DeepTaskType DeepAgent 任务类型标识
// 对齐 Python: DEEP_TASK_TYPE = "deep_agent_task"
const DeepTaskType = "deep_agent_task"

// SessionSpawnTaskType SessionSpawn 任务类型标识
// 对齐 Python: SESSION_SPAWN_TASK_TYPE = "session_spawn_task"
// ⤵️ 9.7 回填：此常量可能迁移到 tools/subagent 包
const SessionSpawnTaskType = "session_spawn_task"

// ──────────────────────────── 结构体 ────────────────────────────

// TaskLoopEventExecutor TaskExecutor 的 DeepAgent 专用实现，
// 将内层 ReActAgent 的执行包装为 TaskExecutor 接口，使 TaskScheduler 可以调度。
// 对齐 Python: TaskLoopEventExecutor(TaskExecutor)
type TaskLoopEventExecutor struct {
	// deps 依赖容器（TaskManager 等）
	deps *modules.TaskExecutorDependencies
	// provider DeepAgent 运行时访问接口
	// ⤵️ 9.1 回填：替换为 *DeepAgent 直接引用
	provider DeepAgentProvider
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTaskLoopEventExecutor 创建任务循环事件执行器。
// 对齐 Python: TaskLoopEventExecutor.__init__
func NewTaskLoopEventExecutor(deps *modules.TaskExecutorDependencies, provider DeepAgentProvider) *TaskLoopEventExecutor {
	return &TaskLoopEventExecutor{
		deps:    deps,
		provider: provider,
	}
}

// ExecuteAbility 执行任务，将内层 ReActAgent 调用包装为 TaskExecutor 接口。
// 返回输出分片 channel，channel 关闭表示执行结束。
// 对齐 Python: TaskLoopEventExecutor.execute_ability
func (e *TaskLoopEventExecutor) ExecuteAbility(ctx context.Context, taskID string, sess sessioninterfaces.SessionFacade) (<-chan *stream.OutputSchema, error) {
	ch := make(chan *stream.OutputSchema, 1)

	agent := e.provider
	reactAgent := agent.ReactAgent()
	if reactAgent == nil {
		logger.Warn(logComponent).Msg("ExecuteAbility: ReactAgent 为 nil，无法执行")
		close(ch)
		return ch, nil
	}

	// 3. 从 TaskManager 获取 core_task
	tasks, err := e.deps.TaskManager.GetTask(ctx, MakeFilter(taskID))
	if err != nil {
		logger.Error(logComponent).Err(err).Str("task_id", taskID).Msg("ExecuteAbility: 获取任务失败")
		close(ch)
		return ch, err
	}

	// 4. 提取 query + raw_input
	var query any = taskID
	var rawInput any

	if len(tasks) > 0 {
		coreTask := tasks[0]
		if coreTask.Description != "" {
			query = coreTask.Description
		}
		for _, evt := range coreTask.Inputs {
			inputEvt, ok := evt.(*schema.InputEvent)
			if !ok {
				continue
			}
			rawInput = ExtractInteractiveInput(inputEvt)
			if rawInput != nil {
				break
			}
		}
	}

	// 5-6. 获取 state + plan_task
	state := agent.LoadState(sess)
	planTask := e.getPlanTask(state, taskID)
	if planTask != nil {
		if planTask.Description != "" {
			query = planTask.Content + ": " + planTask.Description
		} else {
			query = planTask.Content
		}
	}

	// 7. 读取 is_follow_up
	isFollowUp := false
	if len(tasks) > 0 {
		if meta := tasks[0].Metadata; meta != nil {
			if v, ok := meta["is_follow_up"]; ok {
				isFollowUp, _ = v.(bool)
			}
		}
	}

	// 8. 计算 iteration
	coordinator := agent.LoopCoordinator()
	iteration := 1
	if coordinator != nil {
		iteration = coordinator.Iteration() + 1
	}

	// 9. 日志记录（IS_SENSITIVE 双模式）
	queryPreview := ""
	if qStr, ok := query.(string); ok {
		if len(qStr) > 120 {
			queryPreview = qStr[:120]
		} else {
			queryPreview = qStr
		}
	}
	if isSensitive() {
		logger.Info(logComponent).
			Int("iteration", iteration).
			Str("task_id", taskID).
			Msg("外层循环迭代")
	} else {
		logger.Info(logComponent).
			Int("iteration", iteration).
			Str("task_id", taskID).
			Str("query", queryPreview).
			Msg("外层循环迭代")
	}

	// 10. 构建 TaskIterationInputs + AgentCallbackContext
	loopEvent, loopErr := schema.FromUserInput(query)
	if loopErr != nil {
		logger.Error(logComponent).Err(loopErr).Msg("ExecuteAbility: 构建循环事件失败")
		close(ch)
		return ch, loopErr
	}

	cid := ""
	if sess != nil {
		cid = sess.GetSessionID()
	}

	iterInputs := &rail.TaskIterationInputs{
		Iteration:     iteration,
		LoopEvent:     loopEvent,
		ConversationID: cid,
		Query:         query,
		IsFollowUp:    isFollowUp,
	}

	// AgentCallbackContext 需要 RailAgent 接口
	// ⤵️ 9.1 回填：DeepAgent 实现 RailAgent 后可直接传入
	// 当前使用 nil agent，Fire 为空操作
	cbCtx := rail.NewAgentCallbackContext(nil, iterInputs, sess)

	// 11. MarkInProgress
	if planTask != nil {
		if state != nil && state.TaskPlan != nil {
			if markErr := state.TaskPlan.MarkInProgress(taskID); markErr != nil {
				logger.Warn(logComponent).Err(markErr).Str("task_id", taskID).Msg("MarkInProgress 失败")
			}
		}
	}

	// 12. Fire BEFORE_TASK_ITERATION
	if fireErr := cbCtx.Fire(rail.CallbackBeforeTaskIteration); fireErr != nil {
		logger.Warn(logComponent).Err(fireErr).Msg("BEFORE_TASK_ITERATION 钩子失败")
	}

	// 13. 计算 effective_query
	iterQuery := iterInputs.Query
	effectiveQuery := rawInput
	if effectiveQuery == nil {
		if qStr, ok := iterQuery.(string); ok && qStr != "" {
			effectiveQuery = qStr
		} else if qStr, ok := query.(string); ok {
			effectiveQuery = qStr
		}
	}

	// 14. 构建 effective map
	effective := map[string]any{
		"query": effectiveQuery,
	}
	if cid != "" {
		effective["conversation_id"] = cid
	}
	if len(tasks) > 0 && tasks[0].Metadata != nil {
		metadata := tasks[0].Metadata
		if v, ok := metadata["run_kind"]; ok && v != nil {
			effective["run_kind"] = v
		}
		if v, ok := metadata["run_context"]; ok && v != nil {
			effective["run_context"] = v
		}
	}

	// 15. 注入 steering_queue
	handler := agent.EventHandler()
	if handler != nil {
		if provider, ok := handler.(interactionQueuesProvider); ok {
			if queues := provider.InteractionQueues(); queues != nil {
				effective["_steering_queue"] = queues.steering
			}
		}
	}

	// 16-17. 异步执行 ReActAgent
	go func() {
		defer close(ch)

		// 对齐 Python: react_agent.invoke(effective, session, _streaming=True)
		// ⤵️ 9.1 回填：需要确认 ReActAgent.Invoke 的实际签名和调用方式
		result, invokeErr := reactAgent.Invoke(ctx, effective, sess)

		if invokeErr != nil {
			// 17. 异常路径
			logger.Error(logComponent).
				Err(invokeErr).
				Str("task_id", taskID).
				Int("iteration", iteration).
				Msg("任务执行失败")

			if e.getPlanTask(state, taskID) != nil {
				if state != nil && state.TaskPlan != nil {
					_ = state.TaskPlan.MarkCancelled(taskID, invokeErr.Error())
				}
			}

			payload := &schema.ControllerOutputPayload{
				Type:     string(schema.EventTaskFailed),
				Data:     []schema.DataFrame{&schema.TextDataFrame{Text: invokeErr.Error()}},
				Metadata: map[string]any{"task_id": taskID},
			}
			ch <- &stream.OutputSchema{
				Type:        string(schema.EventTaskFailed),
				Index:       0,
				Payload:     payload,
				IsLastSchema: true,
			}
			return
		}

		// 16. 正常路径
		resultMap, _ := result.(map[string]any)
		if resultMap == nil {
			resultMap = make(map[string]any)
		}

		// MarkCompleted（interrupt 跳过）
		resultType, _ := resultMap["result_type"]
		if resultType != "interrupt" {
			if e.getPlanTask(state, taskID) != nil {
				if state != nil && state.TaskPlan != nil {
					summary := ""
					if output, ok := resultMap["output"]; ok {
						s := fmt.Sprintf("%v", output)
						if len(s) > 200 {
							s = s[:200]
						}
						summary = s
					}
					_ = state.TaskPlan.MarkCompleted(taskID, summary)
				}
			}
		}

		// Fire AFTER_TASK_ITERATION
		iterInputs.Result = resultMap
		cbCtx.SetInputs(iterInputs)
		_ = cbCtx.Fire(rail.CallbackAfterTaskIteration)

		// 完成日志
		if isSensitive() {
			logger.Info(logComponent).
				Int("iteration", iteration).
				Str("task_id", taskID).
				Msg("外层循环迭代完成")
		} else {
			outputPreview := ""
			if output, ok := resultMap["output"]; ok {
				s := fmt.Sprintf("%v", output)
				if len(s) > 200 {
					s = s[:200]
				}
				outputPreview = s
			}
			logger.Info(logComponent).
				Int("iteration", iteration).
				Str("task_id", taskID).
				Str("output", outputPreview).
				Msg("外层循环迭代完成")
		}

		payload := &schema.ControllerOutputPayload{
			Type:     string(schema.EventTaskCompletion),
			Data:     []schema.DataFrame{&schema.JsonDataFrame{Data: resultMap}},
			Metadata: map[string]any{"task_id": taskID},
		}
		ch <- &stream.OutputSchema{
			Type:        string(schema.EventTaskCompletion),
			Index:       0,
			Payload:     payload,
			IsLastSchema: true,
		}
	}()

	return ch, nil
}

// CanPause 暂停不支持。
// 对齐 Python: TaskLoopEventExecutor.can_pause
func (e *TaskLoopEventExecutor) CanPause(_ context.Context, _ string, _ sessioninterfaces.SessionFacade) (bool, string, error) {
	return false, "pause not supported", nil
}

// Pause 暂停不支持。
// 对齐 Python: TaskLoopEventExecutor.pause
func (e *TaskLoopEventExecutor) Pause(_ context.Context, _ string, _ sessioninterfaces.SessionFacade) (bool, error) {
	return false, nil
}

// CanCancel 始终允许取消。
// 对齐 Python: TaskLoopEventExecutor.can_cancel
func (e *TaskLoopEventExecutor) CanCancel(_ context.Context, _ string, _ sessioninterfaces.SessionFacade) (bool, string, error) {
	return true, "", nil
}

// Cancel 取消任务：标记取消 + 请求中止。
// 对齐 Python: TaskLoopEventExecutor.cancel
func (e *TaskLoopEventExecutor) Cancel(ctx context.Context, taskID string, sess sessioninterfaces.SessionFacade) (bool, error) {
	state := e.getState(sess)
	if e.getPlanTask(state, taskID) != nil {
		if state != nil && state.TaskPlan != nil {
			_ = state.TaskPlan.MarkCancelled(taskID, "cancelled")
		}
	}
	coordinator := e.provider.LoopCoordinator()
	if coordinator != nil {
		coordinator.RequestAbort()
	}
	return true, nil
}

// BuildDeepExecutor 创建 TaskLoopEventExecutor 构建闭包，用于注册到 TaskExecutorRegistry。
// 对齐 Python: build_deep_executor(deep_agent)
func BuildDeepExecutor(provider DeepAgentProvider) func(*modules.TaskExecutorDependencies) modules.TaskExecutor {
	return func(deps *modules.TaskExecutorDependencies) modules.TaskExecutor {
		return NewTaskLoopEventExecutor(deps, provider)
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// getState 从会话加载 DeepAgentState。
// 对齐 Python: TaskLoopEventExecutor._get_state
func (e *TaskLoopEventExecutor) getState(sess sessioninterfaces.SessionFacade) *harnessschema.DeepAgentState {
	return e.provider.LoadState(sess)
}

// getPlanTask 获取 TaskPlan 中的指定任务。
// 对齐 Python: TaskLoopEventExecutor._get_plan_task
func (e *TaskLoopEventExecutor) getPlanTask(state *harnessschema.DeepAgentState, taskID string) *harnessschema.TodoItem {
	if state == nil || state.TaskPlan == nil {
		return nil
	}
	return state.TaskPlan.GetTask(taskID)
}

// MakeFilter 构建单个 taskID 的 TaskFilter。
// 对齐 Python: TaskLoopEventExecutor._make_filter
func MakeFilter(taskID string) *modules.TaskFilter {
	return &modules.TaskFilter{TaskID: taskID}
}

// ExtractInteractiveInput 从 InputEvent 提取 InteractiveInput。
// 对齐 Python: TaskLoopEventExecutor._extract_interactive_input
func ExtractInteractiveInput(event *schema.InputEvent) *interaction.InteractiveInput {
	if event == nil {
		return nil
	}
	for _, df := range event.InputData {
		jsonDF, ok := df.(*schema.JsonDataFrame)
		if !ok {
			continue
		}
		if jsonDF.Data == nil {
			continue
		}
		if q, exists := jsonDF.Data["query"]; exists {
			if ii, isII := q.(*interaction.InteractiveInput); isII {
				return ii
			}
		}
	}
	return nil
}

// isSensitive 读取 IS_SENSITIVE 环境变量判断是否敏感模式。
// 默认为敏感模式（true），IS_SENSITIVE=false 时为非敏感模式。
// 对齐 Python: UserConfig.is_sensitive() + base_client.go 已有模式
func isSensitive() bool {
	if v := os.Getenv("IS_SENSITIVE"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return true
}

// 确保 TaskLoopEventExecutor 满足 TaskExecutor 接口
var _ modules.TaskExecutor = (*TaskLoopEventExecutor)(nil)
```

注意：上述代码中 `reactAgent.Invoke` 的签名需要根据实际 ReActAgent 方法调整，标注 ⤵️ 9.1 回填。`fmt` import 需要补充。

- [ ] **Step 2: 编译验证**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/harness/task_loop/...`

预期：可能有 import 路径或类型不匹配的编译错误，逐一修复直到编译通过。

- [ ] **Step 3: Commit**

```bash
git add internal/agentcore/harness/task_loop/executor.go
git commit -m "feat(9.6): 添加 TaskLoopEventExecutor + DeepAgentProvider 接口骨架"
```

---

### Task 2: handler.go — TaskLoopEventHandler 结构体 + per-round channel 模式

**Files:**
- Create: `internal/agentcore/harness/task_loop/handler.go`

- [ ] **Step 1: 创建 handler.go，写入完整实现**

```go
package task_loop

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller/modules"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TaskLoopEventHandler 事件处理器，驱动外层任务循环。
// 创建 core Task 提交 TaskScheduler 调度，由 TaskLoopEventExecutor 执行。
// 使用 per-round channel 模式：每轮迭代创建新 resultCh + 递增 round_id，
// completion/failed/abort 事件通过 resolveRound 写入 channel。
// 对齐 Python: TaskLoopEventHandler(EventHandler)
type TaskLoopEventHandler struct {
	// base 基础依赖容器
	base modules.EventHandlerBase
	// provider DeepAgent 运行时访问接口
	// ⤵️ 9.1 回填：替换为 *DeepAgent 直接引用
	provider DeepAgentProvider
	// mu 保护 currentCh + roundID 的并发访问
	mu sync.Mutex
	// lastResult 最近一轮的结果
	lastResult map[string]any
	// currentCh 当前轮次的结果 channel
	currentCh chan map[string]any
	// roundID 单调递增轮次编号，防止过期完成写入错误 channel
	roundID int
	// interactionQueues 交互队列（steer + follow_up）
	interactionQueues *LoopQueues
	// sessionToolkit Session 工具包
	// ⤵️ 9.7 回填：替换为具体 SessionToolkit 类型
	sessionToolkit any
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTaskLoopEventHandler 创建任务循环事件处理器。
// 对齐 Python: TaskLoopEventHandler.__init__
func NewTaskLoopEventHandler(provider DeepAgentProvider) *TaskLoopEventHandler {
	return &TaskLoopEventHandler{
		provider: provider,
	}
}

// GetBase 返回 EventHandlerBase。
func (h *TaskLoopEventHandler) GetBase() *modules.EventHandlerBase {
	return &h.base
}

// PrepareRound 准备轮次，创建新 channel + 递增 round_id。
// 必须在 PublishEventAsync 之前调用。
// 对齐 Python: TaskLoopEventHandler.prepare_round
func (h *TaskLoopEventHandler) PrepareRound() int {
	h.mu.Lock()
	defer h.mu.Unlock()

	// 关闭未完成的旧 channel
	if h.currentCh != nil {
		select {
		case <-h.currentCh:
			// 已关闭或已读取
		default:
			close(h.currentCh)
		}
	}

	h.roundID++
	h.currentCh = make(chan map[string]any, 1)
	return h.roundID
}

// WaitCompletion 等待当前轮次完成。
// timeout 为 0 表示不超时。
// 对齐 Python: TaskLoopEventHandler.wait_completion
func (h *TaskLoopEventHandler) WaitCompletion(ctx context.Context, timeout time.Duration) map[string]any {
	h.mu.Lock()
	ch := h.currentCh
	h.mu.Unlock()

	if ch == nil {
		return map[string]any{"error": "no active round"}
	}

	var result map[string]any

	if timeout > 0 {
		timer := time.NewTimer(timeout)
		defer timer.Stop()
		select {
		case result = <-ch:
		case <-ctx.Done():
			result = map[string]any{"error": "cancelled"}
		case <-timer.C:
			result = map[string]any{"error": "completion_timeout"}
		}
	} else {
		select {
		case result = <-ch:
		case <-ctx.Done():
			result = map[string]any{"error": "cancelled"}
		}
	}

	if result == nil {
		result = map[string]any{"status": "completed"}
	}
	h.lastResult = result
	return result
}

// HandleInput 处理输入事件，创建 core Task 提交调度。
// 对齐 Python: TaskLoopEventHandler.handle_input
func (h *TaskLoopEventHandler) HandleInput(ctx context.Context, input *modules.EventHandlerInput) (map[string]any, error) {
	agent := h.provider
	event := input.Event
	sess := input.Session

	// 读取 round_id（从事件元数据或当前 _round_id）
	currentRound := h.roundID
	if meta := event.GetMetadata(); meta != nil {
		if v, ok := meta["_handler_round_id"]; ok {
			if rid, isInt := v.(int); isInt {
				currentRound = rid
			}
		}
	}

	// 提取 query
	query := extractQuery(event)

	// 解析 task_id
	taskID := ""
	if meta := event.GetMetadata(); meta != nil {
		if v, ok := meta["task_id"]; ok {
			taskID, _ = v.(string)
		}
	}

	var runKind, runContext any
	if meta := event.GetMetadata(); meta != nil {
		runKind = meta["run_kind"]
		runContext = meta["run_context"]
	}

	coordinator := agent.LoopCoordinator()
	if coordinator == nil {
		logger.Warn(logComponent).Msg("HandleInput: LoopCoordinator 为 nil")
		h.resolveRound(map[string]any{"error": "no LoopCoordinator"}, currentRound)
		return map[string]any{"status": "failed"}, nil
	}

	// is_follow_up 判断
	isFollowUp := false
	if meta := event.GetMetadata(); meta != nil {
		if v, ok := meta["is_follow_up"]; ok {
			isFollowUp, _ = v.(bool)
		}
	}

	// 从 TaskPlan 获取 task_id（follow_up 用随机 UUID）
	if taskID == "" && !isFollowUp && sess != nil {
		state := agent.LoadState(sess)
		if state != nil && state.TaskPlan != nil {
			nextTask := state.TaskPlan.GetNextTask()
			if nextTask != nil {
				taskID = nextTask.ID
			}
		}
	}

	if taskID == "" {
		taskID = uuid.New().Hex()
	}

	sessionID := "default"
	if sess != nil {
		sessionID = sess.GetSessionID()
	}

	// 构建 task 元数据
	taskMetadata := map[string]any{
		"_handler_round_id": currentRound,
		"is_follow_up":      isFollowUp,
	}
	if runKind != nil {
		taskMetadata["run_kind"] = runKind
	}
	if runContext != nil {
		taskMetadata["run_context"] = runContext
	}

	// 构建 core Task
	coreTask := &schema.Task{
		SessionID:  sessionID,
		TaskID:     taskID,
		TaskType:   DeepTaskType,
		Description: query,
		Status:     schema.TaskSubmitted,
		Metadata:   taskMetadata,
	}

	// 如果 event 是 InputEvent，存入 Inputs
	if inputEvt, ok := event.(*schema.InputEvent); ok {
		coreTask.Inputs = []schema.Event{inputEvt}
	}

	// AddTask
	if h.base.TaskManager == nil {
		h.resolveRound(map[string]any{"error": "task_manager is nil"}, currentRound)
		return map[string]any{"status": "failed"}, nil
	}

	if err := h.base.TaskManager.AddTask(ctx, coreTask); err != nil {
		logger.Error(logComponent).Err(err).Msg("HandleInput: 添加任务失败")
		h.resolveRound(map[string]any{"error": err.Error()}, currentRound)
		return map[string]any{"status": "failed", "error": err.Error()}, nil
	}

	return map[string]any{"status": "submitted", "task_id": taskID}, nil
}

// HandleTaskInteraction 处理任务交互事件（steer）。
// 对齐 Python: TaskLoopEventHandler.handle_task_interaction
func (h *TaskLoopEventHandler) HandleTaskInteraction(_ context.Context, input *modules.EventHandlerInput) (map[string]any, error) {
	event := input.Event
	msg := ""

	interactionEvt, ok := event.(*schema.TaskInteractionEvent)
	if ok && interactionEvt.Interaction != nil {
		for _, df := range interactionEvt.Interaction {
			if textDF, isText := df.(*schema.TextDataFrame); isText {
				msg = textDF.Text
				break
			}
		}
	}

	if msg != "" && h.interactionQueues != nil {
		h.interactionQueues.PushSteer(msg)
	}

	steerPreview := msg
	if len(steerPreview) > 100 {
		steerPreview = steerPreview[:100]
	}
	logger.Info(logComponent).Str("msg", steerPreview).Msg("Steer 已注入")

	return map[string]any{"status": "steer_injected", "msg": msg}, nil
}

// HandleTaskCompletion 处理任务完成事件。
// 对齐 Python: TaskLoopEventHandler.handle_task_completion
func (h *TaskLoopEventHandler) HandleTaskCompletion(_ context.Context, input *modules.EventHandlerInput) (map[string]any, error) {
	event := input.Event
	meta := event.GetMetadata()

	taskType := ""
	taskID := ""
	if meta != nil {
		if v, ok := meta["task_type"]; ok {
			taskType, _ = v.(string)
		}
		if v, ok := meta["task_id"]; ok {
			taskID, _ = v.(string)
		}
	}

	// SessionSpawn 分支
	if taskType == SessionSpawnTaskType {
		// ⤵️ 9.7 回填：调用 _completeSessionSpawn(taskID, input, false)
		logger.Info(logComponent).Str("task_id", taskID).Msg("SessionSpawn 完成（⤵️ 9.7 回填）")
		return map[string]any{"status": "session_spawn_completed", "task_id": taskID}, nil
	}

	// 获取 round_id
	roundID := h.roundID
	if meta != nil {
		if v, ok := meta["_handler_round_id"]; ok {
			if rid, isInt := v.(int); isInt {
				roundID = rid
			}
		}
	}

	// 提取 result
	result := map[string]any{}
	completionEvt, ok := event.(*schema.TaskCompletionEvent)
	if ok && completionEvt.TaskResult != nil {
		for _, df := range completionEvt.TaskResult {
			if jsonDF, isJSON := df.(*schema.JsonDataFrame); isJSON && jsonDF.Data != nil {
				result = jsonDF.Data
				break
			}
			if textDF, isText := df.(*schema.TextDataFrame); isText {
				result["output"] = textDF.Text
			}
		}
	}

	h.resolveRound(result, roundID)

	return map[string]any{"status": "completed", "task_id": taskID}, nil
}

// HandleTaskFailed 处理任务失败事件。
// 对齐 Python: TaskLoopEventHandler.handle_task_failed
func (h *TaskLoopEventHandler) HandleTaskFailed(_ context.Context, input *modules.EventHandlerInput) (map[string]any, error) {
	event := input.Event
	meta := event.GetMetadata()

	taskType := ""
	taskID := ""
	if meta != nil {
		if v, ok := meta["task_type"]; ok {
			taskType, _ = v.(string)
		}
		if v, ok := meta["task_id"]; ok {
			taskID, _ = v.(string)
		}
	}

	// SessionSpawn 分支
	if taskType == SessionSpawnTaskType {
		// ⤵️ 9.7 回填：调用 _completeSessionSpawn(taskID, input, true)
		logger.Info(logComponent).Str("task_id", taskID).Msg("SessionSpawn 失败（⤵️ 9.7 回填）")
		return map[string]any{"status": "session_spawn_failed", "task_id": taskID}, nil
	}

	// 获取 round_id
	roundID := h.roundID
	if meta != nil {
		if v, ok := meta["_handler_round_id"]; ok {
			if rid, isInt := v.(int); isInt {
				roundID = rid
			}
		}
	}

	errorMsg := "unknown"
	if failedEvt, ok := event.(*schema.TaskFailedEvent); ok {
		errorMsg = failedEvt.ErrorMessage
	}

	h.resolveRound(map[string]any{"error": errorMsg}, roundID)

	return map[string]any{"status": "failed", "task_id": taskID, "error": errorMsg}, nil
}

// HandleFollowUp 处理跟进事件。
// 对齐 Python: TaskLoopEventHandler.handle_follow_up
func (h *TaskLoopEventHandler) HandleFollowUp(_ context.Context, input *modules.EventHandlerInput) (map[string]any, error) {
	event := input.Event
	msg := ""

	if followUpEvt, ok := event.(*schema.FollowUpEvent); ok {
		for _, df := range followUpEvt.InputData {
			if textDF, isText := df.(*schema.TextDataFrame); isText {
				msg = textDF.Text
				break
			}
		}
	}

	if msg != "" && h.interactionQueues != nil {
		h.interactionQueues.PushFollowUp(msg)
	}

	followUpPreview := msg
	if len(followUpPreview) > 100 {
		followUpPreview = followUpPreview[:100]
	}
	logger.Info(logComponent).Str("msg", followUpPreview).Msg("Follow-up 已入队")

	return map[string]any{"status": "follow_up_queued", "msg": msg}, nil
}

// OnAbort 中止回调，resolve 当前轮次为 aborted。
// 对齐 Python: TaskLoopEventHandler.on_abort
func (h *TaskLoopEventHandler) OnAbort() {
	h.resolveRound(map[string]any{"error": "aborted"}, h.roundID)
}

// InteractionQueues 返回交互队列，实现 interactionQueuesProvider 接口。
// 对齐 Python: TaskLoopEventHandler.interaction_queues property
func (h *TaskLoopEventHandler) InteractionQueues() *LoopQueues {
	return h.interactionQueues
}

// SetInteractionQueues 设置交互队列。
func (h *TaskLoopEventHandler) SetInteractionQueues(queues *LoopQueues) {
	h.interactionQueues = queues
}

// SetSessionToolkit 设置 Session 工具包。
// ⤵️ 9.7 回填：参数类型替换为具体 SessionToolkit
func (h *TaskLoopEventHandler) SetSessionToolkit(toolkit any) {
	h.sessionToolkit = toolkit
}

// LastResult 返回最近一轮的结果。
func (h *TaskLoopEventHandler) LastResult() map[string]any {
	return h.lastResult
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// resolveRound 写入当前轮次的结果 channel。
// roundID 不匹配时丢弃（防止过期完成写入错误 channel）。
// 对齐 Python: TaskLoopEventHandler._resolve_future
func (h *TaskLoopEventHandler) resolveRound(result map[string]any, roundID int) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if roundID != h.roundID {
		logger.Warn(logComponent).
			Int("round_id", roundID).
			Int("current_round_id", h.roundID).
			Msg("过期 resolve，丢弃结果")
		return
	}

	if h.currentCh != nil {
		select {
		case h.currentCh <- result:
		default:
			logger.Warn(logComponent).Msg("resolveRound: channel 已满，丢弃结果")
		}
	}
}

// extractQuery 从 InputEvent 提取文本 query。
// 对齐 Python: TaskLoopEventHandler._extract_query
func extractQuery(event schema.Event) string {
	inputEvt, ok := event.(*schema.InputEvent)
	if !ok {
		return ""
	}
	for _, df := range inputEvt.InputData {
		if textDF, isText := df.(*schema.TextDataFrame); isText && textDF.Text != "" {
			return textDF.Text
		}
		if jsonDF, isJSON := df.(*schema.JsonDataFrame); isJSON && jsonDF.Data != nil {
			if q, exists := jsonDF.Data["query"]; exists {
				return fmt.Sprintf("%v", q)
			}
		}
	}
	return ""
}

// 确保 TaskLoopEventHandler 满足 EventHandler 接口
var _ modules.EventHandler = (*TaskLoopEventHandler)(nil)

// 确保 TaskLoopEventHandler 满足 interactionQueuesProvider 接口
var _ interactionQueuesProvider = (*TaskLoopEventHandler)(nil)
```

- [ ] **Step 2: 编译验证**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/harness/task_loop/...`

修复编译错误直到通过。

- [ ] **Step 3: Commit**

```bash
git add internal/agentcore/harness/task_loop/handler.go
git commit -m "feat(9.6): 添加 TaskLoopEventHandler + per-round channel 模式"
```

---

### Task 3: 更新 doc.go + controller.go 注释

**Files:**
- Modify: `internal/agentcore/harness/task_loop/doc.go`
- Modify: `internal/agentcore/harness/task_loop/controller.go`

- [ ] **Step 1: 更新 doc.go，添加 executor.go + handler.go 条目**

更新文件目录部分，在 `loop_coordinator.go` 行后添加：

```
//	├── executor.go             # TaskLoopEventExecutor + DeepAgentProvider 接口 + BuildDeepExecutor
//	├── handler.go              # TaskLoopEventHandler（per-round channel + 事件路由）
```

同步更新包功能概述，补充说明 TaskLoopEventExecutor 和 TaskLoopEventHandler 的职责。

- [ ] **Step 2: 更新 controller.go 中 interactionQueuesProvider 注释**

将注释从 `// 只有 TaskLoopEventHandler（9.6）实现此接口，其他 EventHandler 不实现。` 更新为：

```go
// interactionQueuesProvider 类型断言接口，用于从 EventHandler 获取 LoopQueues。
// 对齐 Python: getattr(handler, "interaction_queues", None)
// ⤴️ 9.6 回填：TaskLoopEventHandler 已实现此接口。
```

- [ ] **Step 3: Commit**

```bash
git add internal/agentcore/harness/task_loop/doc.go internal/agentcore/harness/task_loop/controller.go
git commit -m "docs(9.6): 更新 doc.go 和 controller.go 注释"
```

---

### Task 4: executor_test.go — TaskLoopEventExecutor 单元测试

**Files:**
- Create: `internal/agentcore/harness/task_loop/executor_test.go`

- [ ] **Step 1: 创建 executor_test.go，实现 fake DeepAgentProvider + 基础测试**

测试文件需要实现 fake DeepAgentProvider 用于 mock 测试。包含以下测试用例：

- `TestNewTaskLoopEventExecutor` — 构造函数验证
- `TestTaskLoopEventExecutor_CanPause返回不支持` — CanPause 返回 `(false, "pause not supported", nil)`
- `TestTaskLoopEventExecutor_CanCancel始终允许` — CanCancel 返回 `(true, "", nil)`
- `TestTaskLoopEventExecutor_Pause返回不支持` — Pause 返回 `(false, nil)`
- `TestTaskLoopEventExecutor_Cancel标记取消并请求中止` — Cancel 调用 MarkCancelled + RequestAbort
- `TestBuildDeepExecutor` — 工厂函数返回正确的闭包
- `TestMakeFilter` — MakeFilter 构建正确的 TaskFilter
- `TestExtractInteractiveInput_正常提取` — 从 InputEvent 提取 InteractiveInput
- `TestExtractInteractiveInput_Nil事件` — nil 事件返回 nil
- `TestIsSensitive` — IS_SENSITIVE 环境变量读取

- [ ] **Step 2: 运行测试验证通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/harness/task_loop/ -run "TestNewTaskLoopEventExecutor|TestTaskLoopEventExecutor_Can|TestTaskLoopEventExecutor_Pause|TestTaskLoopEventExecutor_Cancel|TestBuildDeepExecutor|TestMakeFilter|TestExtractInteractiveInput|TestIsSensitive" -v`

- [ ] **Step 3: Commit**

```bash
git add internal/agentcore/harness/task_loop/executor_test.go
git commit -m "test(9.6): 添加 TaskLoopEventExecutor 基础单元测试"
```

---

### Task 5: executor_test.go — ExecuteAbility 集成测试

**Files:**
- Modify: `internal/agentcore/harness/task_loop/executor_test.go`

- [ ] **Step 1: 添加 ExecuteAbility 相关测试**

- `TestTaskLoopEventExecutor_ExecuteAbility_ReactAgent为Nil时返回空` — ReactAgent 返回 nil 时，channel 立即关闭且无输出
- `TestTaskLoopEventExecutor_ExecuteAbility_正常执行` — 完整流程：mock DeepAgentProvider 返回预设 ReActAgent，验证 channel 收到 TASK_COMPLETION payload
- `TestTaskLoopEventExecutor_ExecuteAbility_执行失败` — mock 返回错误，验证 channel 收到 TASK_FAILED payload

注意：ExecuteAbility 测试依赖 ReActAgent.Invoke 的 mock，需要创建 fake ReActAgent。如果 ReActAgent 结构体方法较多，可以用最小 fake 或标注 `⤵️ 9.1 回填`。若当前无法 mock Invoke，则标注 TODO 并跳过。

- [ ] **Step 2: 运行测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/harness/task_loop/ -run "TestTaskLoopEventExecutor_ExecuteAbility" -v`

- [ ] **Step 3: Commit**

```bash
git add internal/agentcore/harness/task_loop/executor_test.go
git commit -m "test(9.6): 添加 ExecuteAbility 测试"
```

---

### Task 6: handler_test.go — TaskLoopEventHandler 单元测试

**Files:**
- Create: `internal/agentcore/harness/task_loop/handler_test.go`

- [ ] **Step 1: 创建 handler_test.go，实现全部测试用例**

包含以下测试用例：

- `TestNewTaskLoopEventHandler` — 构造函数验证
- `TestTaskLoopEventHandler_PrepareRound递增` — 连续调用 PrepareRound，验证 roundID 递增 + channel 重建
- `TestTaskLoopEventHandler_WaitCompletion_正常完成` — PrepareRound → resolveRound → WaitCompletion 收到结果
- `TestTaskLoopEventHandler_WaitCompletion_超时` — WaitCompletion 超时返回 `{"error": "completion_timeout"}`
- `TestTaskLoopEventHandler_WaitCompletion_无活跃轮次` — 无 PrepareRound 时返回 `{"error": "no active round"}`
- `TestTaskLoopEventHandler_ResolveRound_匹配RoundID` — roundID 匹配时成功写入
- `TestTaskLoopEventHandler_ResolveRound_过期RoundID丢弃` — roundID 不匹配时丢弃，Warn 日志
- `TestTaskLoopEventHandler_HandleTaskInteraction_注入Steer` — steer 消息推入 interactionQueues
- `TestTaskLoopEventHandler_HandleFollowUp_入队` — follow-up 消息推入 interactionQueues
- `TestTaskLoopEventHandler_OnAbort` — resolveRound with `{"error": "aborted"}`
- `TestTaskLoopEventHandler_InteractionQueues` — 实现 interactionQueuesProvider 接口
- `TestTaskLoopEventHandler_SetInteractionQueues` — setter 验证
- `TestExtractQuery_文本提取` — 从 InputEvent 提取文本
- `TestExtractQuery_字典提取` — 从 InputEvent 的 JsonDataFrame 提取 query
- `TestExtractQuery_非InputEvent` — 非 InputEvent 事件返回空串

- [ ] **Step 2: 运行测试验证通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/harness/task_loop/ -run "TestNewTaskLoopEventHandler|TestTaskLoopEventHandler_|TestExtractQuery" -v`

- [ ] **Step 3: Commit**

```bash
git add internal/agentcore/harness/task_loop/handler_test.go
git commit -m "test(9.6): 添加 TaskLoopEventHandler 单元测试"
```

---

### Task 7: handler_test.go — HandleInput 测试

**Files:**
- Modify: `internal/agentcore/harness/task_loop/handler_test.go`

- [ ] **Step 1: 添加 HandleInput 相关测试**

- `TestTaskLoopEventHandler_HandleInput_正常提交` — 创建 Task 并 AddTask，返回 `{"status": "submitted", "task_id": ...}`
- `TestTaskLoopEventHandler_HandleInput_无LoopCoordinator时失败` — coordinator nil 时返回 `{"status": "failed"}`

这些测试需要 mock TaskManager，可以通过 EventHandlerBase 注入。

- [ ] **Step 2: 运行测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/harness/task_loop/ -run "TestTaskLoopEventHandler_HandleInput" -v`

- [ ] **Step 3: Commit**

```bash
git add internal/agentcore/harness/task_loop/handler_test.go
git commit -m "test(9.6): 添加 HandleInput 测试"
```

---

### Task 8: 全量编译 + 测试 + 更新 IMPLEMENTATION_PLAN.md

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 全量编译验证**

Run: `cd /home/opensource/uap-claw-go && go build ./...`

修复任何编译错误。

- [ ] **Step 2: 全量测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/harness/task_loop/ -v -cover`

确保所有测试通过，覆盖率 ≥ 85%。

- [ ] **Step 3: 更新 IMPLEMENTATION_PLAN.md**

将 9.6 行状态从 `☐` 改为 `✅`。

- [ ] **Step 4: Commit**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "chore: 更新实现计划 9.6 状态为已完成"
```
