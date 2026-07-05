package task_loop

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller/modules"
	cschema "github.com/uapclaw/uapclaw-go/internal/agentcore/controller/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TaskLoopEventHandler 任务循环事件处理器。
// 实现 modules.EventHandler 接口，将 Controller 领域的事件
// 转换为任务循环的轮次提交/完成/中止语义。
// 对齐 Python: TaskLoopEventHandler
type TaskLoopEventHandler struct {
	// base 基础依赖容器
	base modules.EventHandlerBase
	// provider 深层 Agent 提供者
	// ⤵️ 9.1 回填：用 *DeepAgent 替换 DeepAgentProvider
	provider DeepAgentProvider
	// mu 保护轮次状态的互斥锁
	mu sync.Mutex
	// lastResult 上一轮完成结果
	lastResult map[string]any
	// currentCh 当前轮次的完成通知 channel
	currentCh chan map[string]any
	// roundID 当前轮次编号
	roundID int
	// interactionQueues 交互双队列
	interactionQueues *LoopQueues
	// sessionToolkit 会话工具包
	// ⤵️ 9.7 回填：用于 SessionSpawn 分支
	sessionToolkit any
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTaskLoopEventHandler 创建任务循环事件处理器。
// 对齐 Python: TaskLoopEventHandler.__init__
func NewTaskLoopEventHandler(provider DeepAgentProvider) *TaskLoopEventHandler {
	return &TaskLoopEventHandler{
		provider:   provider,
		currentCh:  make(chan map[string]any, 1),
	}
}

// GetBase 返回基础依赖容器。
func (h *TaskLoopEventHandler) GetBase() *modules.EventHandlerBase {
	return &h.base
}

// PrepareRound 准备新一轮次：关闭旧 channel、递增轮次编号、创建新 channel。
// 对齐 Python: TaskLoopEventHandler.prepare_round
func (h *TaskLoopEventHandler) PrepareRound() int {
	h.mu.Lock()
	defer h.mu.Unlock()

	// 关闭旧 channel（安全：close 对 nil 或已关闭的 channel 会 panic，
	// 但 currentCh 在 NewTaskLoopEventHandler 中已初始化，且每轮都重新创建）
	if h.currentCh != nil {
		close(h.currentCh)
	}
	h.roundID++
	h.currentCh = make(chan map[string]any, 1)

	logger.Info(logComponent).
		Int("round_id", h.roundID).
		Msg("准备新轮次")

	return h.roundID
}

// WaitCompletion 等待当前轮次完成，支持超时。
// 对齐 Python: TaskLoopEventHandler.wait_completion
func (h *TaskLoopEventHandler) WaitCompletion(ctx context.Context, timeout time.Duration) map[string]any {
	h.mu.Lock()
	ch := h.currentCh
	h.mu.Unlock()

	if timeout > 0 {
		timer := time.NewTimer(timeout)
		defer timer.Stop()
		select {
		case result := <-ch:
			return result
		case <-ctx.Done():
			logger.Warn(logComponent).
				Err(ctx.Err()).
				Msg("等待轮次完成：上下文取消")
			return map[string]any{"status": "cancelled", "error": ctx.Err().Error()}
		case <-timer.C:
			logger.Warn(logComponent).
				Str("timeout", timeout.String()).
				Msg("等待轮次完成：超时")
			return map[string]any{"status": "timeout"}
		}
	}

	// 无超时：只等待 channel 或上下文取消
	select {
	case result := <-ch:
		return result
	case <-ctx.Done():
		logger.Warn(logComponent).
			Err(ctx.Err()).
			Msg("等待轮次完成：上下文取消")
		return map[string]any{"status": "cancelled", "error": ctx.Err().Error()}
	}
}

// HandleInput 处理输入事件：提取查询 → 确定任务 ID → 提交任务。
// 对齐 Python: TaskLoopEventHandler.handle_input
func (h *TaskLoopEventHandler) HandleInput(ctx context.Context, input *modules.EventHandlerInput) (map[string]any, error) {
	event := input.Event

	// 从元数据读取轮次编号
	metadata := event.GetMetadata()
	var currentRound int
	if v, ok := metadata["_handler_round_id"]; ok {
		if r, ok := v.(int); ok {
			currentRound = r
		}
	}

	// 提取查询文本
	query := extractQuery(event)

	// 从元数据读取运行时上下文
	var taskID, runKind, runContext string
	if v, ok := metadata["task_id"]; ok {
		taskID, _ = v.(string)
	}
	if v, ok := metadata["run_kind"]; ok {
		runKind, _ = v.(string)
	}
	if v, ok := metadata["run_context"]; ok {
		runContext, _ = v.(string)
	}

	// 获取协调器，nil 检查
	coordinator := h.provider.LoopCoordinator()
	if coordinator == nil {
		logger.Warn(logComponent).
			Str("event_type", "LLM_CALL_ERROR").
			Str("method", "HandleInput").
			Msg("LoopCoordinator 为 nil，无法执行任务")
		h.resolveRound(map[string]any{"error": "coordinator is nil"}, currentRound)
		return map[string]any{"status": "error", "error": "coordinator is nil"}, nil
	}

	// 判断是否为 follow-up
	isFollowUp := false
	if v, ok := metadata["is_follow_up"]; ok {
		isFollowUp, _ = v.(bool)
	}

	// 非 follow-up 时从 TaskPlan 获取下一个任务的 ID
	if !isFollowUp {
		state := h.provider.LoadState(input.Session)
		if state != nil && state.TaskPlan != nil {
			nextTask := state.TaskPlan.GetNextTask()
			if nextTask != nil {
				taskID = nextTask.ID
			}
		}
	}

	// taskID 兜底：自动生成
	if taskID == "" {
		taskID = uuid.New().String()
	}

	// 构建核心任务
	coreTask := &cschema.Task{
		SessionID:  input.Session.GetSessionID(),
		TaskID:     taskID,
		TaskType:   DeepTaskType,
		Description: query,
		Status:     cschema.TaskSubmitted,
		Metadata:   make(map[string]any),
	}
	if runKind != "" {
		coreTask.Metadata["run_kind"] = runKind
	}
	if runContext != "" {
		coreTask.Metadata["run_context"] = runContext
	}
	if isFollowUp {
		coreTask.Metadata["is_follow_up"] = true
	}

	logger.Info(logComponent).
		Str("task_id", taskID).
		Str("task_type", DeepTaskType).
		Int("round_id", currentRound).
		Bool("is_follow_up", isFollowUp).
		Msg("提交深层 Agent 任务")

	// 添加任务到管理器
	if err := h.base.TaskManager.AddTask(ctx, coreTask); err != nil {
		logger.Error(logComponent).
			Err(err).
			Str("task_id", taskID).
			Str("event_type", "LLM_CALL_ERROR").
			Str("method", "HandleInput").
			Msg("添加任务失败")
		h.resolveRound(map[string]any{"error": err.Error()}, currentRound)
		return nil, fmt.Errorf("添加任务失败: %w", err)
	}

	return map[string]any{
		"status":  "submitted",
		"task_id": taskID,
	}, nil
}

// HandleTaskInteraction 处理任务交互事件：提取引导指令 → 推入 steering 队列。
// 对齐 Python: TaskLoopEventHandler.handle_task_interaction
func (h *TaskLoopEventHandler) HandleTaskInteraction(ctx context.Context, input *modules.EventHandlerInput) (map[string]any, error) {
	event := input.Event

	// 从 TaskInteractionEvent 提取引导文本
	var steerText string
	if tie, ok := event.(*cschema.TaskInteractionEvent); ok {
		for _, df := range tie.Interaction {
			if textDF, ok := df.(*cschema.TextDataFrame); ok {
				steerText = textDF.Text
				break
			}
		}
	}

	if steerText == "" {
		logger.Warn(logComponent).Msg("HandleTaskInteraction: 未提取到引导文本")
		return map[string]any{"status": "no_steer"}, nil
	}

	// 推入 steering 队列
	if h.interactionQueues != nil {
		h.interactionQueues.PushSteer(steerText)
		logger.Info(logComponent).
			Str("steer_text", steerText).
			Msg("引导指令已注入")
	} else {
		logger.Warn(logComponent).
			Str("steer_text", steerText).
			Msg("InteractionQueues 为 nil，引导指令丢弃")
	}

	return map[string]any{"status": "steer_injected"}, nil
}

// HandleTaskCompletion 处理任务完成事件：提取结果 → resolveRound。
// 对齐 Python: TaskLoopEventHandler.handle_task_completion
func (h *TaskLoopEventHandler) HandleTaskCompletion(ctx context.Context, input *modules.EventHandlerInput) (map[string]any, error) {
	event := input.Event

	// SessionSpawn 分支
	if taskType, ok := event.GetMetadata()["task_type"]; ok {
		if taskType == SessionSpawnTaskType {
			h.resolveRound(map[string]any{"status": "session_spawn_completed"}, 0)
			return map[string]any{"status": "session_spawn_completed"}, nil
		}
	}

	// 从 TaskCompletionEvent 提取结果
	var result map[string]any
	if tce, ok := event.(*cschema.TaskCompletionEvent); ok {
		for _, df := range tce.TaskResult {
			if jsonDF, ok := df.(*cschema.JsonDataFrame); ok {
				result = jsonDF.Data
				break
			}
		}
	}

	if result == nil {
		result = make(map[string]any)
	}

	// 从元数据获取轮次编号
	var roundID int
	if v, ok := event.GetMetadata()["_handler_round_id"]; ok {
		if r, ok := v.(int); ok {
			roundID = r
		}
	}

	logger.Info(logComponent).
		Int("round_id", roundID).
		Msg("任务完成，解析轮次")

	h.resolveRound(result, roundID)
	return result, nil
}

// HandleTaskFailed 处理任务失败事件：resolveRound with error。
// 对齐 Python: TaskLoopEventHandler.handle_task_failed
func (h *TaskLoopEventHandler) HandleTaskFailed(ctx context.Context, input *modules.EventHandlerInput) (map[string]any, error) {
	event := input.Event

	// SessionSpawn 分支
	if taskType, ok := event.GetMetadata()["task_type"]; ok {
		if taskType == SessionSpawnTaskType {
			h.resolveRound(map[string]any{"status": "session_spawn_failed"}, 0)
			return map[string]any{"status": "session_spawn_failed"}, nil
		}
	}

	// 提取错误消息
	var errMsg string
	if tfe, ok := event.(*cschema.TaskFailedEvent); ok {
		errMsg = tfe.ErrorMessage
	}

	// 从元数据获取轮次编号
	var roundID int
	if v, ok := event.GetMetadata()["_handler_round_id"]; ok {
		if r, ok := v.(int); ok {
			roundID = r
		}
	}

	logger.Error(logComponent).
		Str("error_message", errMsg).
		Int("round_id", roundID).
		Str("event_type", "LLM_CALL_ERROR").
		Str("method", "HandleTaskFailed").
		Msg("任务失败")

	result := map[string]any{
		"error": errMsg,
	}
	h.resolveRound(result, roundID)
	return result, nil
}

// HandleFollowUp 处理跟进事件：提取文本 → 推入 follow-up 队列。
// 对齐 Python: TaskLoopEventHandler.handle_follow_up
func (h *TaskLoopEventHandler) HandleFollowUp(ctx context.Context, input *modules.EventHandlerInput) (map[string]any, error) {
	event := input.Event

	// 从 FollowUpEvent 提取文本
	var followUpText string
	if fue, ok := event.(*cschema.FollowUpEvent); ok {
		for _, df := range fue.InputData {
			if textDF, ok := df.(*cschema.TextDataFrame); ok {
				followUpText = textDF.Text
				break
			}
		}
	}

	if followUpText == "" {
		logger.Warn(logComponent).Msg("HandleFollowUp: 未提取到跟进文本")
		return map[string]any{"status": "no_follow_up"}, nil
	}

	// 推入 follow-up 队列
	if h.interactionQueues != nil {
		h.interactionQueues.PushFollowUp(followUpText)
		logger.Info(logComponent).
			Str("follow_up_text", followUpText).
			Msg("跟进消息已入队")
	} else {
		logger.Warn(logComponent).
			Str("follow_up_text", followUpText).
			Msg("InteractionQueues 为 nil，跟进消息丢弃")
	}

	return map[string]any{"status": "follow_up_queued"}, nil
}

// OnAbort 中止回调：resolveRound with aborted。
// 对齐 Python: TaskLoopEventHandler.on_abort
func (h *TaskLoopEventHandler) OnAbort() {
	h.mu.Lock()
	rid := h.roundID
	h.mu.Unlock()

	logger.Warn(logComponent).
		Int("round_id", rid).
		Msg("任务循环中止")

	h.resolveRound(map[string]any{"error": "aborted"}, rid)
}

// InteractionQueues 返回交互双队列。
// 实现 interactionQueuesProvider 接口。
func (h *TaskLoopEventHandler) InteractionQueues() *LoopQueues {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.interactionQueues
}

// SetInteractionQueues 设置交互双队列。
func (h *TaskLoopEventHandler) SetInteractionQueues(queues *LoopQueues) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.interactionQueues = queues
}

// SetSessionToolkit 设置会话工具包。
// ⤵️ 9.7 回填：用于 SessionSpawn 分支
func (h *TaskLoopEventHandler) SetSessionToolkit(toolkit any) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.sessionToolkit = toolkit
}

// LastResult 返回上一轮完成结果。
func (h *TaskLoopEventHandler) LastResult() map[string]any {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.lastResult
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// resolveRound 解析轮次：将结果非阻塞写入当前轮次的完成 channel。
// 若 roundID 不匹配则丢弃（过期轮次的结果）。
// 对齐 Python: TaskLoopEventHandler._resolve_round
func (h *TaskLoopEventHandler) resolveRound(result map[string]any, roundID int) {
	h.mu.Lock()
	defer h.mu.Unlock()

	// 轮次不匹配，丢弃过期结果
	if roundID != h.roundID {
		logger.Warn(logComponent).
			Int("expected_round_id", h.roundID).
			Int("got_round_id", roundID).
			Msg("轮次编号不匹配，丢弃过期结果")
		return
	}

	h.lastResult = result

	// 非阻塞写入 channel（channel cap=1，若已满则说明已有结果，丢弃）
	if h.currentCh != nil {
		select {
		case h.currentCh <- result:
		default:
			logger.Warn(logComponent).
				Int("round_id", h.roundID).
				Msg("当前轮次 channel 已满，丢弃重复结果")
		}
	}
}

// extractQuery 从事件中提取查询文本。
// 支持 InputEvent 和 FollowUpEvent 两种事件类型。
// 对齐 Python: TaskLoopEventHandler._extract_query
func extractQuery(event cschema.Event) string {
	switch evt := event.(type) {
	case *cschema.InputEvent:
		for _, df := range evt.InputData {
			if textDF, ok := df.(*cschema.TextDataFrame); ok {
				return textDF.Text
			}
		}
	case *cschema.FollowUpEvent:
		for _, df := range evt.InputData {
			if textDF, ok := df.(*cschema.TextDataFrame); ok {
				return textDF.Text
			}
		}
	}
	return ""
}

// 编译时接口检查：TaskLoopEventHandler 必须满足 modules.EventHandler
var _ modules.EventHandler = (*TaskLoopEventHandler)(nil)

// 编译时接口检查：TaskLoopEventHandler 必须满足 interactionQueuesProvider
var _ interactionQueuesProvider = (*TaskLoopEventHandler)(nil)
