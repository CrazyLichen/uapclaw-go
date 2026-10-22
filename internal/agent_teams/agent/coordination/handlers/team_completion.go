package handlers

import (
	"context"

	types "github.com/uapclaw/uapclaw-go/internal/agent_teams/agent/coordination/types"
	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
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
// EVENT_METHOD_MAP 事件到方法映射：
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
	host types.DispatcherHost,
	bp types.DispatcherBlueprint,
	inf types.DispatcherInfra,
	pollCtrl types.PollController,
) *TeamCompletionHandler {
	return &TeamCompletionHandler{
		BaseCoordinationHandler: NewBaseCoordinationHandler(host, bp, inf, pollCtrl),
		completionCallbacks:     make([]func(ctx context.Context) error, 0),
	}
}

// GetCallbacks 返回 event_key → 回调方法注册表。
// Python: TeamCompletionHandler.get_callbacks
func (h *TeamCompletionHandler) GetCallbacks() map[string]types.EventCallbackFunc {
	return map[string]types.EventCallbackFunc{
		string(types.InnerEventTypePollTask): h.OnPollTask,
		events.TeamEventTaskListDrained:      h.OnTaskListDrained,
		events.TeamEventTeamCompleted:        h.OnTeamCompleted,
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
// Python: TeamCompletionHandler.on_poll_task（team_completion.py L96-130）
// S-附录4: 接通已实现的 TeamBackend.IsTeamCompleted（原 TODO(#9.63) 空壳）。
func (h *TeamCompletionHandler) OnPollTask(ctx context.Context, _ types.CoordinationEvent) {
	// 对齐 Python: 仅 leader 评估完成条件
	role := h.blueprint.Role()
	if role != schema.TeamRoleLeader {
		return
	}
	// M-29 同款 nil 守卫
	if h.infra == nil {
		return
	}
	backend := h.infra.TeamBackend()
	if backend == nil {
		return
	}
	// 对齐 Python: if self._round.has_in_flight_round() or self._round.is_agent_running(): return
	// 先于 teamCompletedEmitted 检查，避免 leader 忙碌时永不重置上升沿标志
	if h.round.HasInFlightRound() || h.round.IsAgentRunning() {
		return
	}

	// Python: snapshot = await team_backend.is_team_completed()
	snapshot, err := backend.IsTeamCompleted(ctx)
	if err != nil {
		logger.Error(logComponent).Err(err).
			Msg("onPollTask: is_team_completed check failed")
		return
	}
	if snapshot == nil {
		// 下降沿：重置上升沿（任务列表非空或有成员未 settled）
		h.teamCompletedEmitted = false
		return
	}
	if h.teamCompletedEmitted {
		return
	}

	// 上升沿：发布 TEAM_COMPLETED
	h.publishTeamCompleted(ctx, backend.TeamName(), snapshot)
	h.teamCompletedEmitted = true

	// Python: persistent 团队在完成时自动暂停——关闭 leader 流
	// 让 Runner 最终驱动 finalize → pause；temporary 团队由 leader
	// 通过 clean_team 自行拆除，不在此处处理。
	lifecycle := "temporary"
	if lp, ok := h.blueprint.(interface{ Lifecycle() atschema.TeamLifecycle }); ok {
		lifecycle = string(lp.Lifecycle())
	}
	if lifecycle == "persistent" {
		if err := h.lifecycle.ConcludeCompletedRound(ctx, snapshot.MemberCount, snapshot.TaskCount); err != nil {
			logger.Error(logComponent).Err(err).
				Msg("onPollTask: conclude_completed_round failed")
		}
	}
}

// publishTeamCompleted 在团队 topic 上发布 TEAM_COMPLETED 事件。
// Python: TeamCompletionHandler._publish_team_completed（team_completion.py L132-152）
func (h *TeamCompletionHandler) publishTeamCompleted(ctx context.Context, teamName string, snapshot *atschema.TeamCompletionSnapshot) {
	if h.infra == nil {
		return
	}
	messagerRef := h.infra.Messager()
	if messagerRef == nil {
		return
	}
	// Python: TeamCompletedEvent(team_name=..., member_count=..., task_count=...)
	event := &events.TeamCompletedEvent{
		BaseEventMessage: events.BaseEventMessage{
			TeamName: teamName,
		},
		MemberCount: snapshot.MemberCount,
		TaskCount:   snapshot.TaskCount,
	}
	topicID := events.TeamTopicTeam.Build(schema.GetSessionID(ctx), teamName)
	msg := events.EventMessageFromEvent(event)
	// Python: publish 失败时仅日志（try/except 吞掉）
	if err := messagerRef.Publish(ctx, topicID, msg); err != nil {
		logger.Error(logComponent).Err(err).
			Str("team_name", teamName).
			Msg("publishTeamCompleted: publish failed")
		return
	}
	logger.Info(logComponent).
		Str("team_name", teamName).
		Int("member_count", snapshot.MemberCount).
		Int("task_count", snapshot.TaskCount).
		Msg("team completed")
}

// OnTaskListDrained 记录任务列表清空事件并触发所有注册的完成回调。
// 每个回调隔离执行，一个失败不跳过其余。
// Python: TeamCompletionHandler.on_task_list_drained
func (h *TeamCompletionHandler) OnTaskListDrained(ctx context.Context, event types.CoordinationEvent) {
	if event.IsInner() {
		return
	}
	em := event.Transport

	teamName, _ := em.Payload["team_name"].(string)
	taskCount, _ := em.Payload["task_count"].(int)
	logger.Info(logComponent).
		Str("event_type", em.EventType).
		Str("team_name", teamName).
		Int("task_count", taskCount).
		Int("callbacks", len(h.completionCallbacks)).
		Msg("onTaskListDrained: task list drained")

	// 对齐 Python: 隔离执行每个回调
	for i, cb := range h.completionCallbacks {
		if err := cb(ctx); err != nil {
			logger.Error(logComponent).
				Err(err).
				Int("callback_index", i).
				Msg("onTaskListDrained: callback failed, continuing with remaining")
		}
	}
}

// OnTeamCompleted 消费 TEAM_COMPLETED 事件，记录结构化日志。
// 仅在 teammate 上执行（发出方 leader 自身的副本由 kernel._filter_self 过滤）。
// Python: TeamCompletionHandler.on_team_completed
func (h *TeamCompletionHandler) OnTeamCompleted(_ context.Context, event types.CoordinationEvent) {
	if event.IsInner() {
		return
	}
	em := event.Transport

	memberCount, _ := em.Payload["member_count"].(int)
	taskCount, _ := em.Payload["task_count"].(int)
	logger.Info(logComponent).
		Str("event_type", em.EventType).
		Str("team_name", func() string {
			n, _ := em.Payload["team_name"].(string)
			return n
		}()).
		Int("member_count", memberCount).
		Int("task_count", taskCount).
		Msg("onTeamCompleted: team completed")
}
