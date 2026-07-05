package task_loop

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller/modules"
	cschema "github.com/uapclaw/uapclaw-go/internal/agentcore/controller/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/interfaces"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interaction"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/rail"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TaskLoopEventExecutor 任务循环事件执行器。
// 实现 modules.TaskExecutor 接口，将深层 Agent 的 ReAct 循环
// 封装为 Controller 领域的标准任务执行流程。
// 对齐 Python: TaskLoopEventExecutor
type TaskLoopEventExecutor struct {
	// deps 任务执行器依赖
	deps *modules.TaskExecutorDependencies
	// provider 深层 Agent 提供者
	provider interfaces.DeepAgentInterface
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTaskLoopEventExecutor 创建任务循环事件执行器。
// 对齐 Python: TaskLoopEventExecutor.__init__
func NewTaskLoopEventExecutor(deps *modules.TaskExecutorDependencies, provider interfaces.DeepAgentInterface) *TaskLoopEventExecutor {
	return &TaskLoopEventExecutor{
		deps:     deps,
		provider: provider,
	}
}

// ExecuteAbility 执行任务，返回输出分片 channel。
// 17 步流程：获取 Agent → 查询任务 → 构建查询 → 加载状态/计划 →
// 构建迭代输入 → 触发前置回调 → 注入 steering 队列 → 调用 ReActAgent → 触发后置回调 → 发送结果。
// 对齐 Python: TaskLoopEventExecutor.execute_ability
func (e *TaskLoopEventExecutor) ExecuteAbility(
	ctx context.Context,
	taskID string,
	sess sessioninterfaces.SessionFacade,
) (<-chan *stream.OutputSchema, error) {
	ch := make(chan *stream.OutputSchema, 1)

	// 步骤 1：获取 provider 和 ReActAgent
	agent := e.provider
	reactAgent := agent.ReactAgent()
	if reactAgent == nil {
		logger.Warn(logComponent).
			Str("task_id", taskID).
			Msg("ReActAgent 为 nil，关闭输出 channel")
		close(ch)
		return ch, nil
	}

	// 步骤 2：查询任务
	tasks, err := e.deps.TaskManager.GetTask(ctx, MakeFilter(taskID))
	if err != nil {
		logger.Error(logComponent).
			Err(err).
			Str("task_id", taskID).
			Str("event_type", "LLM_CALL_ERROR").
			Msg("查询任务失败")
		close(ch)
		return ch, err
	}
	if len(tasks) == 0 {
		logger.Warn(logComponent).
			Str("task_id", taskID).
			Msg("未找到任务")
		close(ch)
		return ch, nil
	}

	task := tasks[0]

	// 步骤 3：提取 query 和 rawInput
	query := taskID
	var rawInput *interaction.InteractiveInput

	if task.Description != "" {
		query = task.Description
	}
	for _, evt := range task.Inputs {
		if inputEvt, ok := evt.(*cschema.InputEvent); ok {
			rawInput = ExtractInteractiveInput(inputEvt)
			break
		}
	}

	// 步骤 4：加载状态，尝试从计划中获取任务信息
	state := agent.LoadState(sess)
	planTask := e.getPlanTask(state, taskID)
	if planTask != nil {
		if planTask.Description != "" {
			query = planTask.Content + ": " + planTask.Description
		} else {
			query = planTask.Content
		}
	}

	// 步骤 5：判断是否为 follow-up
	isFollowUp := false
	if task.Metadata != nil {
		if v, ok := task.Metadata["is_follow_up"]; ok {
			isFollowUp, _ = v.(bool)
		}
	}

	// 步骤 6：获取迭代次数
	var iteration int
	coordinator := agent.LoopCoordinator()
	if coordinator != nil {
		iteration = coordinator.Iteration() + 1
	} else {
		iteration = 1
	}

	// 步骤 7：日志（敏感/非敏感双模式）
	if isSensitive() {
		logger.Info(logComponent).
			Str("task_id", taskID).
			Int("iteration", iteration).
			Bool("is_follow_up", isFollowUp).
			Msg("开始执行任务迭代（敏感模式，隐藏查询内容）")
	} else {
		queryPreview := query
		if len(queryPreview) > 120 {
			queryPreview = queryPreview[:120]
		}
		logger.Info(logComponent).
			Str("task_id", taskID).
			Int("iteration", iteration).
			Bool("is_follow_up", isFollowUp).
			Str("query_preview", queryPreview).
			Msg("开始执行任务迭代")
	}

	// 步骤 8：构建 loopEvent 和 conversationID
	loopEvent, _ := cschema.FromUserInput(query)
	cid := sess.GetSessionID()

	// 步骤 9：构建迭代输入和回调上下文
	// ⤵️ 9.1 回填：agent 参数传 nil，Fire 为空操作
	// 对齐 Python: iter_inputs = TaskIterationInputs(query=query, ...)
	iterInputs := &rail.TaskIterationInputs{
		Iteration:      iteration,
		LoopEvent:      loopEvent,
		ConversationID: cid,
		Query:          query,
		IsFollowUp:     isFollowUp,
	}
	cbCtx := rail.NewAgentCallbackContext(nil, iterInputs, sess)

	// 步骤 10：标记计划任务为进行中
	if planTask != nil && state != nil && state.TaskPlan != nil {
		if markErr := state.TaskPlan.MarkInProgress(taskID); markErr != nil {
			logger.Warn(logComponent).
				Err(markErr).
				Str("task_id", taskID).
				Msg("标记任务为进行中失败")
		}
	}

	// 步骤 11：触发 before_task_iteration 回调
	if fireErr := cbCtx.Fire(rail.CallbackBeforeTaskIteration); fireErr != nil {
		logger.Warn(logComponent).
			Err(fireErr).
			Str("event_type", "before_task_iteration").
			Msg("触发前置回调失败")
	}

	// 步骤 12：确定有效查询
	// 对齐 Python: effective_query = raw_input or iter_inputs.query or query
	var effectiveQuery any = query
	if rawInput != nil {
		effectiveQuery = rawInput
	} else if iterInputs.Query != "" {
		effectiveQuery = iterInputs.Query
	}

	// 步骤 13：构建有效输入
	effective := map[string]any{
		"query":           effectiveQuery,
		"conversation_id": cid,
	}
	if task.Metadata != nil {
		if rk, ok := task.Metadata["run_kind"]; ok {
			effective["run_kind"] = rk
		}
		if rc, ok := task.Metadata["run_context"]; ok {
			effective["run_context"] = rc
		}
	}

	// 步骤 14：注入 steering_queue
	handler := agent.EventHandler()
	if handler != nil {
		if provider, ok := handler.(interactionQueuesProvider); ok {
			queues := provider.InteractionQueues()
			if queues != nil {
				effective["_steering_queue"] = queues.steering
			}
		}
	}

	// 步骤 15-17：异步执行 ReActAgent 并发送结果
	go func() {
		defer close(ch)

		result, invokeErr := reactAgent.Invoke(ctx, effective, agentinterfaces.WithSession(sess))

		if invokeErr != nil {
			// 错误路径：标记取消 + 发送失败事件
			logger.Error(logComponent).
				Err(invokeErr).
				Str("task_id", taskID).
				Int("iteration", iteration).
				Str("event_type", "LLM_CALL_ERROR").
				Msg("任务执行失败")

			if e.getPlanTask(state, taskID) != nil {
				if state != nil && state.TaskPlan != nil {
					_ = state.TaskPlan.MarkCancelled(taskID, invokeErr.Error())
				}
			}

			ch <- &stream.OutputSchema{
				Type: string(cschema.EventTaskFailed),
				Payload: &cschema.ControllerOutputPayload{
					Type: string(cschema.EventTaskFailed),
					Data: []cschema.DataFrame{&cschema.TextDataFrame{Text: invokeErr.Error()}},
					Metadata: map[string]any{
						"task_id": taskID,
					},
				},
				IsLastSchema: true,
			}
			return
		}

		// 成功路径：标记完成 + 触发后置回调 + 发送完成事件
		resultMap := result
		if resultMap == nil {
			resultMap = make(map[string]any)
		}

		// 检查是否被中断（interrupt），中断时跳过 MarkCompleted
		// 对齐 Python: result.get("result_type") != "interrupt"
		resultType := resultMap["result_type"]
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

		// 更新回调上下文并触发 after_task_iteration
		iterInputs.Result = resultMap
		cbCtx.SetInputs(iterInputs)
		if fireErr := cbCtx.Fire(rail.CallbackAfterTaskIteration); fireErr != nil {
			logger.Warn(logComponent).
				Err(fireErr).
				Str("event_type", "after_task_iteration").
				Msg("触发后置回调失败")
		}

		// 完成日志（敏感/非敏感双模式）
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

		ch <- &stream.OutputSchema{
			Type: string(cschema.EventTaskCompletion),
			Payload: &cschema.ControllerOutputPayload{
				Type: string(cschema.EventTaskCompletion),
				Data: []cschema.DataFrame{&cschema.JsonDataFrame{Data: resultMap}},
				Metadata: map[string]any{
					"task_id": taskID,
				},
			},
			IsLastSchema: true,
		}
	}()

	return ch, nil
}

// CanPause 检查任务是否可暂停。深层 Agent 任务不支持暂停。
// 对齐 Python: TaskLoopEventExecutor.can_pause
func (e *TaskLoopEventExecutor) CanPause(_ context.Context, _ string, _ sessioninterfaces.SessionFacade) (bool, string, error) {
	return false, "深层 Agent 任务不支持暂停", nil
}

// Pause 暂停任务。深层 Agent 任务不支持暂停，始终返回 false。
// 对齐 Python: TaskLoopEventExecutor.pause
func (e *TaskLoopEventExecutor) Pause(_ context.Context, _ string, _ sessioninterfaces.SessionFacade) (bool, error) {
	return false, nil
}

// CanCancel 检查任务是否可取消。深层 Agent 任务始终可取消。
// 对齐 Python: TaskLoopEventExecutor.can_cancel
func (e *TaskLoopEventExecutor) CanCancel(_ context.Context, _ string, _ sessioninterfaces.SessionFacade) (bool, string, error) {
	return true, "", nil
}

// Cancel 取消任务：标记计划任务为已取消 + 请求中止循环。
// 对齐 Python: TaskLoopEventExecutor.cancel
func (e *TaskLoopEventExecutor) Cancel(_ context.Context, taskID string, sess sessioninterfaces.SessionFacade) (bool, error) {
	state := e.getState(sess)
	if e.getPlanTask(state, taskID) != nil {
		if state != nil && state.TaskPlan != nil {
			_ = state.TaskPlan.MarkCancelled(taskID, "用户取消")
		}
	}
	coordinator := e.provider.LoopCoordinator()
	if coordinator != nil {
		coordinator.RequestAbort()
	}
	return true, nil
}

// BuildDeepExecutor 构建 hschema.DeepTaskType 执行器的工厂闭包。
// 返回的闭包捕获 provider，供 TaskExecutorRegistry 注册。
// ✅ 9.7 已实现 BuildDeepExecutor，在 9.1 的 _setup_task_loop 中注册到 TaskExecutorRegistry
func BuildDeepExecutor(provider interfaces.DeepAgentInterface) func(deps *modules.TaskExecutorDependencies) modules.TaskExecutor {
	return func(deps *modules.TaskExecutorDependencies) modules.TaskExecutor {
		return NewTaskLoopEventExecutor(deps, provider)
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// getState 从会话加载 DeepAgentState。
// 对齐 Python: TaskLoopEventExecutor._get_state
func (e *TaskLoopEventExecutor) getState(sess sessioninterfaces.SessionFacade) *hschema.DeepAgentState {
	return e.provider.LoadState(sess)
}

// getPlanTask 从状态中获取指定任务 ID 的计划任务。
func (e *TaskLoopEventExecutor) getPlanTask(state *hschema.DeepAgentState, taskID string) *hschema.TodoItem {
	if state == nil || state.TaskPlan == nil {
		return nil
	}
	return state.TaskPlan.GetTask(taskID)
}

// ──────────────────────────── 导出函数 ────────────────────────────

// MakeFilter 创建按任务 ID 过滤的 TaskFilter。
func MakeFilter(taskID string) *modules.TaskFilter {
	return &modules.TaskFilter{
		TaskID: taskID,
	}
}

// ExtractInteractiveInput 从 InputEvent 提取交互式输入。
// 仅从 JsonDataFrame.data["query"] 中提取 *InteractiveInput（中断恢复路径）。
// 对齐 Python: TaskLoopEventExecutor._extract_interactive_input
// Python 不从 TextDataFrame 构造 InteractiveInput，TextDataFrame 走纯字符串路径。
func ExtractInteractiveInput(event *cschema.InputEvent) *interaction.InteractiveInput {
	if event == nil || len(event.InputData) == 0 {
		return nil
	}
	for _, df := range event.InputData {
		// 对齐 Python: data.get("query"), isinstance(query, InteractiveInput)
		if jsonDF, ok := df.(*cschema.JsonDataFrame); ok {
			if q, ok := jsonDF.Data["query"]; ok {
				if ii, ok := q.(*interaction.InteractiveInput); ok {
					return ii
				}
			}
		}
	}
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// isSensitive 读取 IS_SENSITIVE 环境变量，判断是否为敏感模式。
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

// ──────────────────────────── 全局变量 ────────────────────────────

// 编译时接口检查：TaskLoopEventExecutor 必须满足 modules.TaskExecutor
var _ modules.TaskExecutor = (*TaskLoopEventExecutor)(nil)
