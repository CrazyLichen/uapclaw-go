package handlers

import (
	"context"
	"fmt"
	"strings"

	types "github.com/uapclaw/uapclaw-go/internal/agent_teams/agent/coordination/types"
	schema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/schema/events"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools/database"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TaskBoardHandler 处理任务板事件：CLAIMED、CREATED、PLAN_REQUEST、
// PLAN_RESPONSE、UPDATED、COMPLETED、CANCELLED、UNBLOCKED。
//
// EVENT_METHOD_MAP 事件到方法的映射（箭头右侧为 Go 侧处理方法）：
//   - task_claimed       映射 onTaskClaimed
//   - task_created       映射 onTaskBoardEvent
//   - task_plan_request  映射 onTaskBoardEvent
//   - task_plan_response 映射 onTaskPlanDecision
//   - task_updated       映射 onTaskBoardEvent
//   - task_completed     映射 onTaskBoardEvent
//   - task_cancelled     映射 onTaskBoardEvent
//   - task_unblocked     映射 onTaskBoardEvent
//
// Python: TaskBoardHandler
type TaskBoardHandler struct {
	BaseCoordinationHandler
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTaskBoardHandler 创建 TaskBoardHandler 实例。
// Python: TaskBoardHandler.__init__
func NewTaskBoardHandler(
	host types.DispatcherHost,
	bp types.DispatcherBlueprint,
	inf types.DispatcherInfra,
	pollCtrl types.PollController,
) *TaskBoardHandler {
	return &TaskBoardHandler{
		BaseCoordinationHandler: NewBaseCoordinationHandler(host, bp, inf, pollCtrl),
	}
}

// GetCallbacks 返回 event_key → 回调方法注册表。
// Python: TaskBoardHandler.get_callbacks
func (h *TaskBoardHandler) GetCallbacks() map[string]types.EventCallbackFunc {
	return map[string]types.EventCallbackFunc{
		events.TeamEventTaskClaimed:      h.OnTaskClaimed,
		events.TeamEventTaskCreated:      h.OnTaskBoardEvent,
		events.TeamEventTaskPlanRequest:  h.OnTaskBoardEvent,
		events.TeamEventTaskPlanResponse: h.OnTaskPlanDecision,
		events.TeamEventTaskUpdated:      h.OnTaskBoardEvent,
		events.TeamEventTaskCompleted:    h.OnTaskBoardEvent,
		events.TeamEventTaskCancelled:    h.OnTaskBoardEvent,
		events.TeamEventTaskUnblocked:    h.OnTaskBoardEvent,
	}
}

// OnTaskClaimed 收到任务认领事件。如果认领目标是自己，投递任务分配内容。
// 如果认领目标是其他人（或 human-agent），走 onTaskBoardEvent 通用逻辑。
// Python: TaskBoardHandler.on_task_claimed
func (h *TaskBoardHandler) OnTaskClaimed(ctx context.Context, event types.CoordinationEvent) {
	// 对齐 Python: if not member_name or self._infra.task_manager is None: return
	memberName := h.blueprint.DispatcherMemberName()
	if memberName == "" {
		return
	}

	if event.IsInner() {
		return
	}
	em := event.Transport
	role := h.blueprint.Role()

	claimMember, _ := em.Payload["member_name"].(string)

	// 对齐 Python: claim 不是自己时
	if claimMember != memberName {
		// human-agent 忽略他人的 claim（avatar 不自主扫任务板）
		if role == schema.TeamRoleHumanAgent {
			return
		}
		// 非 human-agent 走通用任务板事件，通知空闲 agent
		h.OnTaskBoardEvent(ctx, event)
		return
	}

	// 对齐 Python: claim 是自己时恢复轮询 + deliver_input（human-agent 也走此路径，但用不同模板）
	h.poll.ResumePolls(ctx)
	taskID, _ := em.Payload["task_id"].(string)
	content := renderTaskAssignedToSelf(ctx, h.infra, taskID, role == schema.TeamRoleHumanAgent)
	// 对齐 Python: deliver_input 默认 use_steer=True
	if err := h.round.DeliverInput(ctx, content, true); err != nil {
		logger.Error(logComponent).
			Err(err).
			Str("event_type", em.EventType).
			Msg("onTaskClaimed: deliver_input failed")
	}
}

// OnTaskPlanDecision Leader 对成员计划的审批决策通知。
// 如果 tool_call_id 存在，跳过额外 deliver_input（中断恢复已处理）。
// 否则投递 task_plan_approved_to_self 或 task_plan_rejected_to_self。
// Python: TaskBoardHandler.on_task_plan_decision（task_board.py L112-134）
// S-附录4: 完整实装（原 TODO 桩仅日志）。
func (h *TaskBoardHandler) OnTaskPlanDecision(ctx context.Context, event types.CoordinationEvent) {
	// 对齐 Python: if not member_name or self._infra.task_manager is None: return
	memberName := h.blueprint.DispatcherMemberName()
	if memberName == "" || h.infra == nil || h.infra.TaskManager() == nil {
		return
	}

	if event.IsInner() {
		return
	}
	em := event.Transport

	// 对齐 Python: payload.member_name != member_name → 转发 on_task_board_event
	// （Python member_name 为 None 时也转发——None != member_name 恒真；
	// Go 以空串表示 None，空串 != memberName 同样转发，语义一致）
	payloadMember, _ := em.Payload["member_name"].(string)
	if payloadMember != memberName {
		h.OnTaskBoardEvent(ctx, event)
		return
	}

	// 对齐 Python: await self._poll.resume_polls() — 计划决策后恢复轮询
	h.poll.ResumePolls(ctx)

	// 对齐 Python: 有 tool_call_id 时由中断恢复处理，此处跳过
	toolCallID, _ := em.Payload["tool_call_id"].(string)
	if toolCallID != "" {
		logger.Debug(logComponent).
			Str("member_name", memberName).
			Msg("task plan decision resumes pending interrupt, skip extra deliver_input")
		return
	}

	// 对齐 Python L128-134: 按 approved 选择模板投递
	approved, _ := em.Payload["approved"].(bool)
	feedback, _ := em.Payload["feedback"].(string)
	taskID, _ := em.Payload["task_id"].(string)
	key := "dispatcher.task_plan_approved_to_self"
	if !approved {
		key = "dispatcher.task_plan_rejected_to_self"
	}
	content, tErr := schema.T(key, map[string]any{"task_id": taskID, "feedback": feedback})
	if tErr != nil {
		logger.Warn(logComponent).Err(tErr).Str("key", key).
			Msg("onTaskPlanDecision: i18n render failed")
		content = key
	}
	if err := h.round.DeliverInput(ctx, content, true); err != nil {
		logger.Error(logComponent).
			Err(err).
			Str("event_type", em.EventType).
			Msg("onTaskPlanDecision: deliver_input failed")
	}
}

// OnTaskBoardEvent 通用任务板事件处理：恢复轮询 + 提醒空闲 agent。
// Python: TaskBoardHandler.on_task_board_event
func (h *TaskBoardHandler) OnTaskBoardEvent(ctx context.Context, event types.CoordinationEvent) {
	// 对齐 Python: if not member_name or self._infra.task_manager is None: return
	if h.blueprint.DispatcherMemberName() == "" {
		return
	}

	if event.IsInner() {
		return
	}
	em := event.Transport

	// 恢复轮询
	h.poll.ResumePolls(ctx)

	// 提醒空闲 agent 查看任务板
	memberName, _ := em.Payload["member_name"].(string)
	if memberName == "" {
		memberName = h.blueprint.DispatcherMemberName()
	}
	h.nudgeIdleAgent(ctx, memberName, false)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// nudgeIdleAgent 向空闲 agent 投递任务上下文提醒。
// Leader 审查完整任务板；teammate 审查可认领任务 + 所有未完成任务。
// Python: TaskBoardHandler._nudge_idle_agent（task_board.py L151-194）
// S-附录4: 接通已实现的 TaskManager.ListTasks（原 TODO(#9.63) 空壳仅日志）。
func (h *TaskBoardHandler) nudgeIdleAgent(ctx context.Context, memberName string, fromPoll bool) {
	// 对齐 Python: self._infra.task_manager is None → return
	// （Go 端 infra 本身可能为 nil——测试构造 NewEventDispatcher(host, bp, nil, poll)）
	if h.infra == nil {
		return
	}
	taskMgr := h.infra.TaskManager()
	if taskMgr == nil {
		return
	}
	// Python: all_tasks = await self._infra.task_manager.list_tasks()
	allTasks, err := taskMgr.ListTasks(ctx, "")
	if err != nil {
		logger.Warn(logComponent).Err(err).
			Str("member_name", memberName).
			Msg("nudgeIdleAgent: list_tasks failed")
		return
	}
	// Python: terminal = {"completed", "cancelled"}; incomplete = [...]
	incomplete := make([]*database.TeamTaskBase, 0, len(allTasks))
	for _, tk := range allTasks {
		if tk.Status != "completed" && tk.Status != "cancelled" {
			incomplete = append(incomplete, tk)
		}
	}

	// Python: from_poll and LEADER and not incomplete → return
	if fromPoll && h.blueprint.Role() == schema.TeamRoleLeader && len(incomplete) == 0 {
		return
	}

	role := h.blueprint.Role()
	logger.Debug(logComponent).
		Str("member_name", memberName).
		Int("incomplete_tasks", len(incomplete)).
		Msg("nudgeIdleAgent")

	var lines []string
	if role == schema.TeamRoleLeader {
		// Leader: 无未完成任务时投递 all_done 提示（persistent vs temporary）
		if len(incomplete) == 0 {
			// Python: self._blueprint.lifecycle（窄接口无此方法，
			// 实际实现 TeamAgentBlueprint 有 Lifecycle()——类型断言获取）
			lifecycle := "temporary"
			if lp, ok := h.blueprint.(interface{ Lifecycle() schema.TeamLifecycle }); ok {
				lifecycle = string(lp.Lifecycle())
			}
			lifecycleKey := "dispatcher.all_done_temporary"
			if lifecycle == "persistent" {
				lifecycleKey = "dispatcher.all_done_persistent"
			}
			prompt, tErr := schema.T(lifecycleKey)
			if tErr != nil {
				logger.Warn(logComponent).Err(tErr).Str("key", lifecycleKey).
					Msg("nudgeIdleAgent: i18n render failed")
				return
			}
			if err := h.round.DeliverInput(ctx, prompt, true); err != nil {
				logger.Error(logComponent).Err(err).
					Str("member_name", memberName).
					Msg("nudgeIdleAgent: deliver_input failed")
			}
			return
		}
		lines = append(lines, mustT("dispatcher.leader_task_board"))
	} else {
		// Teammate: 审查可认领任务（pending 且无 assignee）
		claimable := 0
		for _, tk := range incomplete {
			if tk.Status == "pending" && tk.Assignee == nil {
				claimable++
			}
		}
		// Python: if not claimable and not incomplete: return
		if claimable == 0 && len(incomplete) == 0 {
			return
		}
		lines = append(lines, mustT("dispatcher.teammate_task_list"))
	}

	// Python: for task in incomplete → "- [{task_id}] [{status}] {title}: {content}{assignee}"
	unassignedMarker := mustT("dispatcher.task_unassigned_marker")
	for _, tk := range incomplete {
		assignee := unassignedMarker
		if tk.Assignee != nil && *tk.Assignee != "" {
			assignee = " → " + *tk.Assignee
		}
		lines = append(lines, fmt.Sprintf("- [%s] [%s] %s: %s%s", tk.TaskID, tk.Status, tk.Title, tk.Content, assignee))
	}

	if err := h.round.DeliverInput(ctx, strings.Join(lines, "\n"), true); err != nil {
		logger.Error(logComponent).Err(err).
			Str("member_name", memberName).
			Msg("nudgeIdleAgent: deliver_input failed")
	}
}

// renderTaskAssignedToSelf 渲染任务分配通知内容。
// 对齐 Python on_task_claimed（task_board.py L81-110）：human-agent 用
// hitt.task_assigned_to_self_human 模板（含 title 尽力查找）；普通成员用
// dispatcher.task_assigned_to_self。
func renderTaskAssignedToSelf(ctx context.Context, infra types.DispatcherInfra, taskID string, isHumanAgent bool) string {
	if !isHumanAgent {
		if content, err := schema.T("dispatcher.task_assigned_to_self", map[string]any{"task_id": taskID}); err == nil {
			return content
		}
		return "task_assigned: " + taskID
	}
	// Python: title 查找是尽力而为（异常不得打断 dispatch 循环）
	title := ""
	if infra != nil {
		if taskMgr := infra.TaskManager(); taskMgr != nil && taskID != "" {
			if task, err := taskMgr.Get(ctx, taskID); err == nil && task != nil {
				title = task.Title
			} else if err != nil {
				logger.Warn(logComponent).Err(err).
					Str("task_id", taskID).
					Msg("task_assigned_to_human_agent: title lookup failed")
			}
		}
	}
	if content, err := schema.T("hitt.task_assigned_to_self_human", map[string]any{
		"task_id": taskID,
		"title":   title,
	}); err == nil {
		return content
	}
	return "task_assigned_to_human: " + taskID
}

// mustT 渲染 i18n 键（无参数），失败时回退为键名本身。
func mustT(key string) string {
	if content, err := schema.T(key); err == nil {
		return content
	}
	return key
}
