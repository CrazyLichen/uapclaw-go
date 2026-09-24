package handlers

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/agent/coordination"
	schema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/schema/events"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TeamCompletionHandler 处理团队完成事件：POLL_TASK 中的完成评估、
// TASK_LIST_DRAINED 和 TEAM_COMPLETED。
//
// 维护上升沿保护（teamCompletedEmitted）防止重复发出 TEAM_COMPLETED。
// TASK_LIST_DRAINED 时触发所有注册的完成回调。
//
// EVENT_METHOD_MAP:
//   - coordination_poll_task → onPollTask
//   - task_list_drained      → onTaskListDrained
//   - team_completed         → onTeamCompleted
//
// Python: TeamCompletionHandler
type TeamCompletionHandler struct {
	BaseCoordinationHandler
	// teamCompletedEmitted 上升沿保护：True 后不再重复发出 TEAM_COMPLETED。
	// 重新 start() 时通过 Rearm() 重置。
	// Python: _team_completed_emitted
	teamCompletedEmitted bool
	// completionCallbacks 在 TASK_LIST_DRAINED 时触发的回调列表。
	// Python: _completion_callbacks
	completionCallbacks []func(ctx context.Context) error
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTeamCompletionHandler 创建 TeamCompletionHandler 实例。
// Python: TeamCompletionHandler.__init__
func NewTeamCompletionHandler(
	host coordination.DispatcherHost,
	bp coordination.DispatcherBlueprint,
	inf coordination.DispatcherInfra,
	pollCtrl coordination.PollController,
) *TeamCompletionHandler {
	return &TeamCompletionHandler{
		BaseCoordinationHandler: NewBaseCoordinationHandler(host, bp, inf, pollCtrl),
		completionCallbacks:     make([]func(ctx context.Context) error, 0),
	}
}

// GetCallbacks 返回 event_key → 回调方法注册表。
// Python: TeamCompletionHandler.get_callbacks
func (h *TeamCompletionHandler) GetCallbacks() map[string]coordination.EventCallbackFunc {
	return map[string]coordination.EventCallbackFunc{
		string(coordination.InnerEventTypePollTask): h.OnPollTask,
		events.TeamEventTaskListDrained:            h.OnTaskListDrained,
		events.TeamEventTeamCompleted:              h.OnTeamCompleted,
	}
}

// RegisterCompletionCallback 注册在 TASK_LIST_DRAINED 时触发的回调。
// 对齐 Python: TeamCompletionHandler.register_completion_callback
func (h *TeamCompletionHandler) RegisterCompletionCallback(cb func(ctx context.Context) error) {
	h.completionCallbacks = append(h.completionCallbacks, cb)
}

// Rearm 重置上升沿保护，允许下次完成时重新发出 TEAM_COMPLETED。
// 在每次 kernel start() 时调用。
// Python: TeamCompletionHandler.rearm
func (h *TeamCompletionHandler) Rearm() {
	h.teamCompletedEmitted = false
}

// OnPollTask Leader 空闲时评估团队完成条件。
// 通过 TeamBackend.is_team_completed() 检查三个条件：
// 1. 任务列表已清空
// 2. 所有成员处于 settled 状态
// 3. 无 in-flight round
// 上升沿时发布 TEAM_COMPLETED 事件，持久团队还需 conclude_completed_round。
// Python: TeamCompletionHandler.on_poll_task
func (h *TeamCompletionHandler) OnPollTask(ctx context.Context, _ coordination.CoordinationEvent) {
	if h.teamCompletedEmitted {
		return
	}

	role := h.blueprint.Role()
	// 对齐 Python: 仅 leader 评估完成条件
	if role != schema.TeamRoleLeader {
		return
	}

	// TODO(#9.63): 等任务后端接口就绪后补充 is_team_completed 检查
	// 对齐 Python 步骤：
	// 1. 调用 TeamBackend.is_team_completed()
	// 2. 上升沿：!teamCompletedEmitted && is_completed → publish_team_completed
	// 3. 持久团队：conclude_completed_round
	logger.Debug(logComponent).
		Str("role", string(role)).
		Bool("emitted", h.teamCompletedEmitted).
		Msg("onPollTask: TODO 等任务后端就绪")

	_ = ctx
}

// OnTaskListDrained 记录任务列表清空事件并触发所有注册的完成回调。
// 每个回调隔离执行，一个失败不跳过其余。
// Python: TeamCompletionHandler.on_task_list_drained
func (h *TeamCompletionHandler) OnTaskListDrained(ctx context.Context, event coordination.CoordinationEvent) {
	if event.IsInner() {
		return
	}
	em := event.Transport

	logger.Info(logComponent).
		Str("event_type", em.EventType).
		Int("callbacks", len(h.completionCallbacks)).
		Msg("onTaskListDrained: 任务列表已清空")

	// 对齐 Python: 隔离执行每个回调
	for i, cb := range h.completionCallbacks {
		if err := cb(ctx); err != nil {
			logger.Error(logComponent).
				Err(err).
				Int("callback_index", i).
				Msg("onTaskListDrained: 回调执行失败，继续执行其余")
		}
	}
}

// OnTeamCompleted 消费 TEAM_COMPLETED 事件，记录结构化日志。
// 仅在 teammate 上执行（发出方 leader 自身的副本由 kernel._filter_self 过滤）。
// Python: TeamCompletionHandler.on_team_completed
func (h *TeamCompletionHandler) OnTeamCompleted(_ context.Context, event coordination.CoordinationEvent) {
	if event.IsInner() {
		return
	}
	em := event.Transport

	logger.Info(logComponent).
		Str("event_type", em.EventType).
		Str("team_name", func() string {
			n, _ := em.Payload["team_name"].(string)
			return n
		}()).
		Msg("onTeamCompleted: 团队已完成")
}

// ──────────────────────────── 非导出函数 ────────────────────────────
