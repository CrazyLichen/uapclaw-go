// 本文件提供 scheduled-dispatch 团队实例的任务板事件反应（F_62）。
//
// scheduled 派发下每一条任务交接——start、review 请求、rework、verified——都
// 以 leader 身份信箱消息的形式来自调度器（agent/scheduling/），leader 的板面
// 认知也来自调度器的 digest 与 escalation。事件驱动侧剩下的只有：
//
//   - 从基类继承的定向所有权 steer（TASK_CLAIMED / TASK_REVOKED / TASK_CANCELLED
//     ——leader 的 update_task 在两种模式下行为相同；相比之下编辑会作为调度器的
//     revision mail 到达承担者，F_129），以及
//   - 在板面变动时保持周期轮询存活。
//
// 基类的 verify-gate 自 steer 与空闲 agent 板面巡检会在调度器的信件旁边重复
// 投递，所以本类重新定义该模式下板面事件的含义：恢复轮询，别的什么都不做。
// 选择发生在装配处（EventDispatcher），从不发生在 handler 方法内部。
//
// Python: ScheduledTaskBoardHandler（task_board.py L685-768）
package handlers

import (
	"context"

	ateamcoordtypes "github.com/uapclaw/uapclaw-go/internal/agent_teams/agent/coordination/types"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/schema/events"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ScheduledTaskBoardHandler scheduled-dispatch 团队实例的任务板反应。
// Python: ScheduledTaskBoardHandler(TaskBoardHandler)（task_board.py L685）
type ScheduledTaskBoardHandler struct {
	TaskBoardHandler
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewScheduledTaskBoardHandler 创建 scheduled 模式任务板 handler。
// Python: EventDispatcher._TASK_BOARD_CLASS["scheduled"]（dispatcher.py L215）
func NewScheduledTaskBoardHandler(
	host ateamcoordtypes.DispatcherHost,
	bp ateamcoordtypes.DispatcherBlueprint,
	inf ateamcoordtypes.DispatcherInfra,
	pollCtrl ateamcoordtypes.PollController,
) *ScheduledTaskBoardHandler {
	return &ScheduledTaskBoardHandler{
		TaskBoardHandler: TaskBoardHandler{
			BaseCoordinationHandler: NewBaseCoordinationHandler(host, bp, inf, pollCtrl),
		},
	}
}

// GetCallbacks scheduled 模式的事件映射覆盖。
//
// 评审投票只在 scheduled 派发下存在：票是纯板面可观测性——leader 侧调度器
// 统计它们。verify-gate handoff 在该模式下是调度器的信件。被编辑的任务以携带
// 新内容的调度器 revision mail 到达承担者（F_129）；它旁边的「重读并继续」
// steer 会重复投递，且会把挂起父任务的 owner 送回父任务。
//
// ⚠️ 已知偏差：TASK_REVIEW_VOTE / TASK_SUBMITTED_FOR_REVIEW /
// TASK_REVISION_REQUESTED 三事件的 Go 常量随 25.2b（评审半区）落地后补进本表；
// TASK_REVOKED 定向 steer 随 reassign（25.4）落。Python: EVENT_METHOD_MAP
// （task_board.py L708-722）。
// Python: ScheduledTaskBoardHandler.EVENT_METHOD_MAP（task_board.py L708-722）
func (h *ScheduledTaskBoardHandler) GetCallbacks() map[string]ateamcoordtypes.EventCallbackFunc {
	callbacks := map[string]ateamcoordtypes.EventCallbackFunc{
		// 定向所有权 steer 继承基类（update_task 两模式行为相同）。
		events.TeamEventTaskClaimed:      h.OnTaskClaimed,
		events.TeamEventTaskUpdated:      h.OnTaskBoardEvent,
		events.TeamEventTaskCompleted:    h.OnTaskBoardEvent,
		events.TeamEventTaskCancelled:    h.OnTaskBoardEvent,
		events.TeamEventTaskUnblocked:    h.OnTaskBoardEvent,
		events.TeamEventTaskPlanRequest:  h.OnTaskBoardEvent,
		events.TeamEventTaskPlanResponse: h.OnTaskPlanDecision,
	}
	return callbacks
}

// OnInitialTaskPoll scheduled 派发下不做启动板面巡检（F_69）。
// autonomous 巡检存在是因为宕机期间被派活的成员没有其他途径得知。scheduled
// 下它有：调度器的 start 扫描在每次唤醒跑，其 handoff 以持久信箱行落地、由
// 启动信箱清扫 drain。在此巡检会把同一任务投递第二遍，紧挨调度器的信。对已
// 活跃期间被交接的任务同样覆盖：调度器的 handoff 扫描会在信箱没有该任务的
// handoff 时给新 owner 发 start。
// Python: ScheduledTaskBoardHandler.on_initial_task_poll（task_board.py L729-742）
func (h *ScheduledTaskBoardHandler) OnInitialTaskPoll(_ context.Context, _ ateamcoordtypes.CoordinationEvent) {
	logger.Debug(logComponent).Msg("scheduled dispatch: startup board survey skipped")
}

// OnTaskBoardEvent scheduled 派发下的板面变动：保持轮询存活，不唤醒任何人。
// 没有可认领池（每条任务都预指派），调度器同时拥有成员 handoff 与 leader 认知
// ——这里的事件驱动巡检只会烧轮次。
// Python: ScheduledTaskBoardHandler.on_task_board_event（task_board.py L744-765）
func (h *ScheduledTaskBoardHandler) OnTaskBoardEvent(ctx context.Context, event ateamcoordtypes.CoordinationEvent) {
	memberName := h.blueprint.DispatcherMemberName()
	if memberName == "" || h.infra == nil || h.infra.TaskManager() == nil {
		return
	}
	h.poll.ResumePolls(ctx)
	if event.IsInner() {
		return
	}
	logger.Debug(logComponent).
		Str("member_name", memberName).
		Str("event_type", event.Transport.EventType).
		Msg("scheduled dispatch: board event observed, no nudge")
}
