package handlers

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/agent/coordination"
	schema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/schema/events"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TaskBoardHandler 处理任务板事件：CLAIMED、CREATED、PLAN_REQUEST、
// PLAN_RESPONSE、UPDATED、COMPLETED、CANCELLED、UNBLOCKED。
//
// EVENT_METHOD_MAP:
//   - task_claimed       → onTaskClaimed
//   - task_created       → onTaskBoardEvent
//   - task_plan_request  → onTaskBoardEvent
//   - task_plan_response → onTaskPlanDecision
//   - task_updated       → onTaskBoardEvent
//   - task_completed     → onTaskBoardEvent
//   - task_cancelled     → onTaskBoardEvent
//   - task_unblocked     → onTaskBoardEvent
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
	host coordination.DispatcherHost,
	bp coordination.DispatcherBlueprint,
	inf coordination.DispatcherInfra,
	pollCtrl coordination.PollController,
) *TaskBoardHandler {
	return &TaskBoardHandler{
		BaseCoordinationHandler: NewBaseCoordinationHandler(host, bp, inf, pollCtrl),
	}
}

// GetCallbacks 返回 event_key → 回调方法注册表。
// Python: TaskBoardHandler.get_callbacks
func (h *TaskBoardHandler) GetCallbacks() map[string]coordination.EventCallbackFunc {
	return map[string]coordination.EventCallbackFunc{
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
func (h *TaskBoardHandler) OnTaskClaimed(ctx context.Context, event coordination.CoordinationEvent) {
	if event.IsInner() {
		return
	}
	em := event.Transport
	memberName := h.blueprint.MemberName()
	role := h.blueprint.Role()

	claimMember, _ := em.Payload["member_name"].(string)
	// 对齐 Python: 认领目标是自身时投递任务分配内容
	if claimMember == memberName && role != schema.TeamRoleHumanAgent {
		// 对齐 Python: deliver_input(task_assigned_to_self 格式化内容)
		// TODO(#9.63): 等任务格式化模板就绪后补充
		content := formatTaskAssignedToSelf(em.Payload, role == schema.TeamRoleHumanAgent)
		if err := h.round.DeliverInput(ctx, content, false); err != nil {
			logger.Error(logComponent).
				Err(err).
				Str("event_type", em.EventType).
				Msg("onTaskClaimed: deliver_input 失败")
		}
		return
	}

	// 非自身认领：走通用任务板事件
	h.OnTaskBoardEvent(ctx, event)
}

// OnTaskPlanDecision Leader 对成员计划的审批决策通知。
// 如果 tool_call_id 存在，跳过额外 deliver_input（中断恢复已处理）。
// 否则投递 task_plan_approved_to_self 或 task_plan_rejected_to_self。
// Python: TaskBoardHandler.on_task_plan_decision
func (h *TaskBoardHandler) OnTaskPlanDecision(ctx context.Context, event coordination.CoordinationEvent) {
	if event.IsInner() {
		return
	}
	em := event.Transport

	// 对齐 Python: 有 tool_call_id 时由中断恢复处理，此处跳过
	toolCallID, _ := em.Payload["tool_call_id"].(string)
	if toolCallID != "" {
		return
	}

	approved, _ := em.Payload["approved"].(bool)
	// TODO(#9.63): 等任务格式化模板就绪后补充 task_plan_approved/rejected_to_self
	_ = approved
	_ = ctx
	logger.Debug(logComponent).
		Str("event_type", em.EventType).
		Bool("approved", approved).
		Msg("onTaskPlanDecision: TODO 等格式化模板就绪")
}

// OnTaskBoardEvent 通用任务板事件处理：恢复轮询 + 提醒空闲 agent。
// Python: TaskBoardHandler.on_task_board_event
func (h *TaskBoardHandler) OnTaskBoardEvent(ctx context.Context, event coordination.CoordinationEvent) {
	if event.IsInner() {
		return
	}
	em := event.Transport

	// 恢复轮询
	h.poll.ResumePolls()

	// 提醒空闲 agent 查看任务板
	memberName, _ := em.Payload["member_name"].(string)
	if memberName == "" {
		memberName = h.blueprint.MemberName()
	}
	h.nudgeIdleAgent(ctx, memberName, false)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// nudgeIdleAgent 向空闲 agent 投递任务上下文提醒。
// Leader 审查完整任务板；teammate 审查可认领任务 + 所有未完成任务。
// Python: TaskBoardHandler._nudge_idle_agent
// TODO(#9.63): 等任务后端接口就绪后补充完整逻辑
func (h *TaskBoardHandler) nudgeIdleAgent(ctx context.Context, memberName string, fromPoll bool) {
	_ = fromPoll
	role := h.blueprint.Role()
	if role == schema.TeamRoleLeader {
		// Leader: 审查完整任务板
		// 如果无未完成任务，投递 all_done 提示（persistent vs temporary）
		logger.Debug(logComponent).
			Str("member_name", memberName).
			Bool("from_poll", fromPoll).
			Msg("nudgeIdleAgent: leader TODO 等任务后端就绪")
	} else {
		// Teammate: 审查可认领任务 + 所有未完成任务
		logger.Debug(logComponent).
			Str("member_name", memberName).
			Msg("nudgeIdleAgent: teammate TODO 等任务后端就绪")
	}
}

// formatTaskAssignedToSelf 格式化任务分配通知内容。
// 对齐 Python: 根据 isHumanAgent 选择不同模板
// TODO(#9.63): 等格式化模板就绪后替换
func formatTaskAssignedToSelf(payload map[string]any, isHumanAgent bool) map[string]any {
	taskID, _ := payload["task_id"].(string)
	taskTitle, _ := payload["task_title"].(string)
	return map[string]any{
		"task_id":    taskID,
		"task_title": taskTitle,
		"_template":  "task_assigned_to_self",
		"_human":     isHumanAgent,
	}
}
