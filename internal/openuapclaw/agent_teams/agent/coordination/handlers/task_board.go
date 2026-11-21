package handlers

import (
	"context"
	"strings"

	agentteams "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams"
	types "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/agent/coordination/types"
	schema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/schema/events"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/taskrender"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/prompts"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/tools"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/tools/database"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TaskBoardHandler 处理任务板事件：定向所有权（CLAIMED / REVOKED / CANCELLED /
// UPDATED）、verify 闸（SUBMITTED_FOR_REVIEW / VERIFIED / REVISION_REQUESTED）、
// PLAN_RESPONSE fan-out、看板巡视（CREATED / PLAN_REQUEST / STARTED /
// COMPLETED / UNBLOCKED / RELEASED）与 F_69 首启板面普查。
// EVENT_METHOD_MAP 事件到方法的映射（箭头右侧为 Go 侧处理方法）：
//   - coordination_initial_poll_task 映射 OnInitialTaskPoll（F_69，inner）
//   - task_claimed       映射 OnTaskClaimed
//   - task_revoked       映射 OnTaskRevoked
//   - task_cancelled     映射 OnTaskCancelled
//   - task_updated       映射 OnTaskUpdated
//   - task_submitted_for_review 映射 OnTaskSubmittedForReview
//   - task_revision_requested   映射 OnTaskRevisionRequested
//   - task_verified      映射 OnTaskVerified
//   - task_created / task_plan_request / task_started / task_completed /
//     task_unblocked / task_released 映射 OnTaskBoardEvent
//   - task_plan_response 映射 OnTaskPlanDecision
//
// Python: TaskBoardHandler
type TaskBoardHandler struct {
	BaseCoordinationHandler
}

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
// Python: TaskBoardHandler.EVENT_METHOD_MAP / get_callbacks
func (h *TaskBoardHandler) GetCallbacks() map[string]types.EventCallbackFunc {
	return map[string]types.EventCallbackFunc{
		// F_69 首启一次性板面普查（inner，kernel 入队）。
		string(types.InnerEventTypeInitialPollTask): h.OnInitialTaskPoll,
		// 定向所有权变更：steer 受影响成员。
		events.TeamEventTaskClaimed:  h.OnTaskClaimed,
		events.TeamEventTaskRevoked:  h.OnTaskRevoked,
		events.TeamEventTaskCancelled: h.OnTaskCancelled,
		events.TeamEventTaskUpdated:   h.OnTaskUpdated,
		// verify 闸（F_59）：提交指向 reviewers，结论指向作者。
		events.TeamEventTaskSubmittedForReview: h.OnTaskSubmittedForReview,
		events.TeamEventTaskVerified:           h.OnTaskVerified,
		events.TeamEventTaskRevisionRequested:  h.OnTaskRevisionRequested,
		// 看板巡视（nudge idle agent / leader 观察）。
		events.TeamEventTaskCreated:      h.OnTaskBoardEvent,
		events.TeamEventTaskPlanRequest:  h.OnTaskBoardEvent,
		events.TeamEventTaskPlanResponse: h.OnTaskPlanDecision,
		// scheduled 任务开始执行——leader 观察之；teammate 不被 nudge
		//（已启动的任务既非 pending 也非可认领，不扩大可认领池）。
		events.TeamEventTaskStarted:   h.OnTaskBoardEvent,
		events.TeamEventTaskCompleted: h.OnTaskBoardEvent,
		events.TeamEventTaskUnblocked: h.OnTaskBoardEvent,
		events.TeamEventTaskReleased:  h.OnTaskBoardEvent,
	}
}

// ──────────────────────────── 常量 ────────────────────────────

// teammateNudgeEvents 能**扩大**可认领池的看板事件：新 pending 任务出现
//（TASK_CREATED）、阻塞任务的依赖清空翻回 pending（TASK_UNBLOCKED）、被认领
// 的任务 reset 回 pending 重进池（TASK_RELEASED）。其他看板迁移都不会新增可
// 处理的工作——TASK_UPDATED 只编辑既有 pending/blocked 任务的正文，
// TASK_COMPLETED / TASK_CANCELLED 移除任务，TASK_CLAIMED / plan request 缩小
// 或预留池。所以 idle teammate 只被这三类唤醒；其余看板事件只会烧掉一轮去重
// 扫一张没变的可认领集。leader 豁免（OnTaskBoardEvent 分化）：leader 持有板
// 面级决策权，必须观察每一次迁移。
// Python: TaskBoardHandler._TEAMMATE_NUDGE_EVENTS (task_board.py L66-78)
var teammateNudgeEvents = map[string]bool{
	events.TeamEventTaskCreated:  true,
	events.TeamEventTaskUnblocked: true,
	events.TeamEventTaskReleased:  true,
}

// leaderCompletionEvents 对 leader 意味着「一张任务刚完成」：这类事件的
// leader nudge 以该任务的完成摘要先行（F_120）。
// Python: TaskBoardHandler._LEADER_COMPLETION_EVENTS (task_board.py L80-82)
var leaderCompletionEvents = map[string]bool{
	events.TeamEventTaskCompleted: true,
	events.TeamEventTaskVerified:  true,
}

// leaderQuietEvents 从 leader 视角看只是「进度」的看板事件（F_121），不唤醒：
// 认领、启动、提交评审、返工请求都不需要 leader 决策。
// Python: TaskBoardHandler._LEADER_QUIET_EVENTS (task_board.py L92-100)
var leaderQuietEvents = map[string]bool{
	events.TeamEventTaskClaimed:             true,
	events.TeamEventTaskStarted:             true,
	events.TeamEventTaskSubmittedForReview:  true,
	events.TeamEventTaskRevisionRequested:   true,
}

// 基类 i18n 键：autonomous 模式下有可认领池，撤回/取消后指引成员回看板找活。
// scheduled 变体覆写（无池：等下一次指派，见 scheduled_task_board.go）。
// Python: TaskBoardHandler._REVOKED_FROM_SELF_KEY / _CANCELLED_TO_SELF_KEY
//（task_board.py L104-105；scheduled 覆写 L724-726）
const (
	baseRevokedFromSelfKey   = "dispatcher.task_revoked_from_self"
	baseCancelledToSelfKey   = "dispatcher.task_cancelled_to_self"
	scheduledRevokedFromSelfKey = "dispatcher.task_revoked_from_self_scheduled"
	scheduledCancelledToSelfKey = "dispatcher.task_cancelled_to_self_scheduled"
)

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
	if err := h.round.DeliverInput(ctx, content, true, nil); err != nil {
		logger.Error(logComponent).
			Err(err).
			Str("event_type", em.EventType).
			Msg("onTaskClaimed: deliver_input failed")
	}
}

// OnTaskUpdated 任务内容被编辑；若目标是自己，让任务通知器立刻说话。
//
// member_name 携带当前 owner（编辑不改 owner）。目标是自己时经任务通知器
// 投递（F-129 的唯一来源，渲染 revision 区间 + 编辑备注 + 要求重读）；
// 其他成员落到通用任务板逻辑（leader 观察；编辑不扩大可认领池，故不推成员）。
// 本方法自己不渲染任何内容，所以通知不会到两次。
//
// human-agent 已认领的任务被 leader 编辑是不允许的（上游拒绝），
// 所以更新事件永远不会指向 human avatar——不需要 HITT 分支。
//
// 注意上游此方法**没有** is_inner 守卫（与 on_task_claimed 不同），故这里也不加。
//
// Python: TaskBoardHandler.on_task_updated (task_board.py:332-360)
func (h *TaskBoardHandler) OnTaskUpdated(ctx context.Context, event types.CoordinationEvent) {
	memberName := h.blueprint.DispatcherMemberName()
	if memberName == "" || h.infra == nil || h.infra.TaskManager() == nil {
		return
	}
	em := event.Transport

	payloadMember, _ := em.Payload["member_name"].(string)
	if payloadMember != memberName {
		// 别人的编辑：走通用任务板逻辑
		h.OnTaskBoardEvent(ctx, event)
		return
	}

	// 是自己的任务：恢复轮询 + 让通知器立刻说话
	h.poll.ResumePolls(ctx)
	taskID, _ := em.Payload["task_id"].(string)
	logger.Info(logComponent).
		Str("event_type", "task_updated_for_self").
		Str("member_name", memberName).
		Str("task_id", taskID).
		Msg("received TASK_UPDATED for self")

	if err := h.round.AnnounceTaskRevisions(ctx); err != nil {
		logger.Warn(logComponent).Err(err).
			Str("member_name", memberName).
			Msg("onTaskUpdated: announce_task_revisions failed")
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
	if err := h.round.DeliverInput(ctx, content, true, nil); err != nil {
		logger.Error(logComponent).
			Err(err).
			Str("event_type", em.EventType).
			Msg("onTaskPlanDecision: deliver_input failed")
	}
}

// OnTaskRevoked 本成员名下的任务被 leader 改派给了别人。
// on_task_claimed 的「失去方」镜像：payload.member_name 是**前任** assignee。
// 指向自己时把成员从已不属于它的任务上 steer 开（它可能正在做），并指回看板。
// 其他成员的事件忽略——reset 已发 TASK_RELEASED 去 nudge 空闲池，leader 自己
// 驱动了改派。human-agent 的票 leader 不可动（上游拒绝），所以撤回永远不会
// 指向 human avatar——无需 HITT 渲染分支。
// Python: TaskBoardHandler.on_task_revoked (task_board.py L266-297)
func (h *TaskBoardHandler) OnTaskRevoked(ctx context.Context, event types.CoordinationEvent) {
	h.deliverOwnershipLoss(ctx, event, baseRevokedFromSelfKey, "task-revoked")
}

// OnTaskCancelled 任务被取消；若 assignee 是自己则 steer 离开。
// member_name 携带被取消任务的（前任）assignee。指向自己时投递「停下并重新
// 巡视」提示（成员可能正在做这张已作废的票）。其他成员落到看板 handler，
// 让 idle leader 仍能观察板面变化（TASK_CANCELLED 移除任务——不 nudge
// teammate）。human-agent 的票 leader 不可动（上游拒绝取消），取消永不指向
// human avatar——无需 HITT 分支。
// Python: TaskBoardHandler.on_task_cancelled (task_board.py L300-330)
func (h *TaskBoardHandler) OnTaskCancelled(ctx context.Context, event types.CoordinationEvent) {
	h.deliverOwnershipLoss(ctx, event, baseCancelledToSelfKey, "task-cancelled", func(h *TaskBoardHandler) {
		h.OnTaskBoardEvent(ctx, event)
	})
}

// deliverOwnershipLoss 定向所有权丢失的共用体：REVOKED 只投自己（他人忽略），
// CANCELLED 他人走看板。scheduled 变体经 keyOverride 换 i18n 键。
// Python: on_task_revoked / on_task_cancelled 的公共骨架。
func (h *TaskBoardHandler) deliverOwnershipLoss(ctx context.Context, event types.CoordinationEvent, bodyKey, kind string,
	onOtherMember ...func(*TaskBoardHandler)) {
	// 对齐 Python: if not member_name or self._infra.task_manager is None: return
	memberName := h.blueprint.DispatcherMemberName()
	if memberName == "" || h.infra == nil || h.infra.TaskManager() == nil {
		return
	}
	if event.IsInner() {
		return
	}
	em := event.Transport
	payloadMember, _ := em.Payload["member_name"].(string)
	if payloadMember != memberName {
		// REVOKED：他人忽略；CANCELLED：落到看板 handler（idle leader 观察）。
		for _, fallback := range onOtherMember {
			fallback(h)
		}
		return
	}
	h.poll.ResumePolls(ctx)
	taskID, _ := em.Payload["task_id"].(string)
	body, tErr := schema.T(bodyKey, map[string]any{"task_id": taskID})
	if tErr != nil {
		logger.Warn(logComponent).Err(tErr).Str("key", bodyKey).
			Msg("deliverOwnershipLoss: i18n render failed")
		body = bodyKey
	}
	content := agentteams.RenderEvent(kind, body, agentteams.EventOptions{})
	logger.Info(logComponent).
		Str("member_name", memberName).
		Str("task_id", taskID).
		Msg("received ownership-loss task event for self")
	if err := h.round.DeliverInput(ctx, content, true, nil); err != nil {
		logger.Error(logComponent).Err(err).
			Str("event_type", em.EventType).
			Msg("deliverOwnershipLoss: deliver_input failed")
	}
}

// OnTaskSubmittedForReview 任务进入 verify 闸；唤醒其 reviewers。
// payload.member_name 是作者，reviewer 列出必须验证的成员。本成员是 reviewer
// 之一时，steer 它去检查交付物并调 verify_task。其他成员落到看板 handler
//（提交不扩大可认领池，非 reviewer 的 teammate 不被 nudge）。
// Python: TaskBoardHandler.on_task_submitted_for_review (task_board.py L391-424)
func (h *TaskBoardHandler) OnTaskSubmittedForReview(ctx context.Context, event types.CoordinationEvent) {
	memberName := h.blueprint.DispatcherMemberName()
	if memberName == "" || h.infra == nil || h.infra.TaskManager() == nil {
		return
	}
	if event.IsInner() {
		return
	}
	em := event.Transport
	if !memberInList(payloadMemberList(em.Payload["reviewer"]), memberName) {
		h.OnTaskBoardEvent(ctx, event)
		return
	}
	h.poll.ResumePolls(ctx)
	taskID, _ := em.Payload["task_id"].(string)
	author, _ := em.Payload["member_name"].(string)
	if author == "" {
		author = "?"
	}
	body, tErr := schema.T("dispatcher.task_submitted_for_review_to_reviewer",
		map[string]any{"task_id": taskID, "author": author})
	if tErr != nil {
		logger.Warn(logComponent).Err(tErr).Str("key", "dispatcher.task_submitted_for_review_to_reviewer").
			Msg("OnTaskSubmittedForReview: i18n render failed")
		body = "dispatcher.task_submitted_for_review_to_reviewer"
	}
	content := agentteams.RenderEvent("task-review-request", body, agentteams.EventOptions{})
	logger.Info(logComponent).
		Str("member_name", memberName).
		Str("task_id", taskID).
		Msg("received TASK_SUBMITTED_FOR_REVIEW as reviewer")
	if err := h.round.DeliverInput(ctx, content, true, nil); err != nil {
		logger.Error(logComponent).Err(err).
			Str("event_type", em.EventType).
			Msg("OnTaskSubmittedForReview: deliver_input failed")
	}
}

// OnTaskRevisionRequested reviewer 打回任务；steer 作者返工。
// member_name 携带作者——任务仍在作者手上（IN_REVIEW → IN_PROGRESS）。指向
// 自己时投递 reviewer 反馈并指向重新提交。其他成员落到看板 handler（leader 观察）。
// Python: TaskBoardHandler.on_task_revision_requested (task_board.py L426-456)
func (h *TaskBoardHandler) OnTaskRevisionRequested(ctx context.Context, event types.CoordinationEvent) {
	memberName := h.blueprint.DispatcherMemberName()
	if memberName == "" || h.infra == nil || h.infra.TaskManager() == nil {
		return
	}
	if event.IsInner() {
		return
	}
	em := event.Transport
	payloadMember, _ := em.Payload["member_name"].(string)
	if payloadMember != memberName {
		h.OnTaskBoardEvent(ctx, event)
		return
	}
	h.poll.ResumePolls(ctx)
	taskID, _ := em.Payload["task_id"].(string)
	feedback, _ := em.Payload["feedback"].(string)
	body, tErr := schema.T("dispatcher.task_revision_requested_to_self",
		map[string]any{"task_id": taskID, "feedback": feedback})
	if tErr != nil {
		logger.Warn(logComponent).Err(tErr).Str("key", "dispatcher.task_revision_requested_to_self").
			Msg("OnTaskRevisionRequested: i18n render failed")
		body = "dispatcher.task_revision_requested_to_self"
	}
	content := agentteams.RenderEvent("task-revision", body, agentteams.EventOptions{})
	logger.Info(logComponent).
		Str("member_name", memberName).
		Str("task_id", taskID).
		Msg("received TASK_REVISION_REQUESTED for self")
	if err := h.round.DeliverInput(ctx, content, true, nil); err != nil {
		logger.Error(logComponent).Err(err).
			Str("event_type", em.EventType).
			Msg("OnTaskRevisionRequested: deliver_input failed")
	}
}

// OnTaskVerified reviewer 放行任务；作者被解锁可接新活。
// member_name 携带作者。评审期间作者持有任务（one-active 不变量计入
// IN_REVIEW），所以 pass 时作者被解锁——steer 它回看板。其他成员落到看板
// handler（leader 观察；下游解锁单独经 TASK_UNBLOCKED 到达）。
// Python: TaskBoardHandler.on_task_verified (task_board.py L458-485)
func (h *TaskBoardHandler) OnTaskVerified(ctx context.Context, event types.CoordinationEvent) {
	memberName := h.blueprint.DispatcherMemberName()
	if memberName == "" || h.infra == nil || h.infra.TaskManager() == nil {
		return
	}
	if event.IsInner() {
		return
	}
	em := event.Transport
	payloadMember, _ := em.Payload["member_name"].(string)
	if payloadMember != memberName {
		h.OnTaskBoardEvent(ctx, event)
		return
	}
	h.poll.ResumePolls(ctx)
	taskID, _ := em.Payload["task_id"].(string)
	body, tErr := schema.T("dispatcher.task_verified_to_self", map[string]any{"task_id": taskID})
	if tErr != nil {
		logger.Warn(logComponent).Err(tErr).Str("key", "dispatcher.task_verified_to_self").
			Msg("OnTaskVerified: i18n render failed")
		body = "dispatcher.task_verified_to_self"
	}
	content := agentteams.RenderEvent("task-verified", body, agentteams.EventOptions{})
	logger.Info(logComponent).
		Str("member_name", memberName).
		Str("task_id", taskID).
		Msg("received TASK_VERIFIED for self")
	if err := h.round.DeliverInput(ctx, content, true, nil); err != nil {
		logger.Error(logComponent).Err(err).
			Str("event_type", em.EventType).
			Msg("OnTaskVerified: deliver_input failed")
	}
}

// OnInitialTaskPoll 成员运行体起来后的一次性板面普查（F_69）。
// 启动信箱清扫的对偶：那个拾取宕机期间发给它的消息，这个拾取宕机期间派给
// 它的活。建队时指派的任务只由一次瞬态 TASK_CLAIMED 宣告——尚未启动的成员
// 永远收不到，否则会 idle 到 stale-claim 窗口过期。呈现与其他 nudge 相同的
// 板面切片——可认领工作 + 指派给自己的任务——并继承空切片时的静默：对着
// 没有自己工作的板子启动的成员绝不烧掉一轮。
// Python: TaskBoardHandler.on_initial_task_poll (task_board.py L547-565)
func (h *TaskBoardHandler) OnInitialTaskPoll(ctx context.Context, event types.CoordinationEvent) {
	memberName := h.blueprint.DispatcherMemberName()
	if memberName == "" || h.infra == nil || h.infra.TaskManager() == nil {
		return
	}
	h.nudgeIdleAgent(ctx, memberName, false, "")
}

// OnTaskBoardEvent 通用任务板事件处理：TASK_CREATED/UPDATED/COMPLETED/
// CANCELLED/UNBLOCKED/RELEASED（+ PLAN_REQUEST/STARTED）唤醒 idle agent。
// 闸门用任务级检查而非 is_agent_running：在 pre-stream 或 finalize 窗口
// nudge 会调 _start_agent 覆盖还活着的 agent 任务。
// teammate 只在可认领集可能扩大时被唤醒（teammateNudgeEvents）。
// leader 只被可能需要其决策的事件唤醒（F_121）：leaderQuietEvents 与已有
// assignee 的任务的解锁都是纯进度，跳过；leader 的下一次唤醒终会巡视全板。
// resume_polls 对每个事件都触发，nudge 过滤器永不卡死周期轮询。
// Python: TaskBoardHandler.on_task_board_event (task_board.py L487-530)
func (h *TaskBoardHandler) OnTaskBoardEvent(ctx context.Context, event types.CoordinationEvent) {
	// 对齐 Python: if not member_name or self._infra.task_manager is None: return
	memberName := h.blueprint.DispatcherMemberName()
	if memberName == "" || h.infra == nil || h.infra.TaskManager() == nil {
		return
	}
	if event.IsInner() {
		return
	}
	em := event.Transport

	// 恢复轮询（对齐 Python: await self._poll.resume_polls()）
	h.poll.ResumePolls(ctx)

	// teammate 只被扩大可认领池的三类唤醒
	if h.blueprint.Role() != schema.TeamRoleLeader && !teammateNudgeEvents[em.EventType] {
		logger.Debug(logComponent).
			Str("member_name", memberName).
			Str("event_type", em.EventType).
			Msg("skip teammate nudge: event does not grow claimable set")
		return
	}
	// leader 只被需要其决策的事件唤醒（F_121）
	if h.blueprint.Role() == schema.TeamRoleLeader && h.leaderNeedsNoDecision(ctx, em) {
		logger.Debug(logComponent).
			Str("member_name", memberName).
			Str("event_type", em.EventType).
			Msg("skip leader nudge: event needs no leader decision")
		return
	}
	logger.Debug(logComponent).
		Str("member_name", memberName).
		Msg("task trigger detected, nudging idle agent")

	// F_120：完成类事件的 leader nudge 以该任务完成摘要先行。
	justCompleted := ""
	if h.blueprint.Role() == schema.TeamRoleLeader && leaderCompletionEvents[em.EventType] {
		justCompleted, _ = em.Payload["task_id"].(string)
	}
	h.nudgeIdleAgent(ctx, memberName, false, justCompleted)
}

// leaderNeedsNoDecision 看板事件对 leader 是否只是进度、无需决策（F_121）。
// TASK_UNBLOCKED 按任务 owner 分化：已指派的任务由其 assignee 拾起；无主的
// 进可认领池、可能需要 leader 指派（例如指给人成员）。任务行已消失的也无需。
// Python: TaskBoardHandler._leader_needs_no_decision (task_board.py L532-545)
func (h *TaskBoardHandler) leaderNeedsNoDecision(ctx context.Context, em *events.EventMessage) bool {
	if leaderQuietEvents[em.EventType] {
		return true
	}
	if em.EventType != events.TeamEventTaskUnblocked {
		return false
	}
	taskID, _ := em.Payload["task_id"].(string)
	task, err := h.infra.TaskManager().Get(ctx, taskID)
	if err != nil {
		logger.Warn(logComponent).Err(err).Str("task_id", taskID).
			Msg("leaderNeedsNoDecision: get failed")
		return false
	}
	return task == nil || (task.Assignee != nil && *task.Assignee != "")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// nudgeIdleAgent 向空闲 agent 投递任务上下文。
// Leader：审查完整任务板（每张未完成任务）以决定是否重新规划、指派或收尾。
// Teammate：只看到现在能开工的——可认领任务（pending + 无主）+ 指派给自己的
// pending 任务。他人认领/在飞的票不呈现——teammate 被唤醒是为了接它能动手的
// 活，不是巡视别人在做什么。
// 板面巡视是提醒而非打断（决策维度 3）：它从不说「你手上的活作废了」，所以
// 走 follow-up 队列（use_steer=false），当前轮结束后才被读。此前用 steer
// 投递会以一张成员尚无需立即行动的巡视打断其推理。
// Python: TaskBoardHandler._nudge_idle_agent (task_board.py L567-698)
func (h *TaskBoardHandler) nudgeIdleAgent(ctx context.Context, memberName string, fromPoll bool, justCompleted string) {
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
		Msg("nudge_idle_agent")

	var body string
	if role == schema.TeamRoleLeader {
		// F_120：刚完成任务的摘要先行于看板 / all-done 提示——
		// 看板只显示未完成工作，否则会把它丢掉。
		completedBlock := justCompletedBlock(ctx, allTasks, justCompleted)
		if len(incomplete) == 0 {
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
			// Python: body = "\n\n".join([*completed_block, prompt])
			body = agentteams.RenderEvent("all-done", strings.Join(append(completedBlock, prompt), "\n\n"), agentteams.EventOptions{})
			if err := h.round.DeliverInput(ctx, body, true, nil); err != nil {
				logger.Error(logComponent).Err(err).
					Str("member_name", memberName).
					Msg("nudgeIdleAgent: deliver_input failed")
			}
			return
		}
		// Leader 写过每张任务的正文，所以它的看板是每任务一行轻量行
		//（状态、owner、阻塞者）而非每次 nudge 重发正文；view_task(get) 有。
		// Python: summaries = await ...list_tasks_with_deps()；行渲染
		// with_content=False + blocked_by 列表。
		lines := append([]string{}, completedBlock...)
		lines = append(lines, mustT("dispatcher.leader_task_board"))
		summaries, serr := taskMgr.ListTasksWithDeps(ctx)
		if serr != nil {
			logger.Warn(logComponent).Err(serr).
				Str("member_name", memberName).
				Msg("nudgeIdleAgent: list_tasks_with_deps failed")
			return
		}
		nowMs := database.GetCurrentTime()
		for _, task := range summaries {
			if task.Status == "completed" || task.Status == "cancelled" {
				continue
			}
			line, rerr := taskrender.RenderTaskLine(taskSummaryLike{task}, nowMs, false, task.BlockedBy)
			if rerr != nil {
				logger.Warn(logComponent).Err(rerr).
					Str("task_id", task.TaskID).
					Msg("nudgeIdleAgent: render_task_line failed")
				continue
			}
			lines = append(lines, line)
		}
		body = agentteams.RenderEvent("task-board", strings.Join(lines, "\n"), agentteams.EventOptions{})
	} else {
		// Teammate：只呈现现在能开工的——可认领任务（pending + 无主）与指派给
		// 自己的 pending 任务。指派给它的 pending 可能建于它启动之前；在这里喂
		// 给它使启动普查可执行，同时不暴露其他成员的工作。自己的 blocked 或
		// 在飞任务不列出：blocked 会在解锁时唤醒它，在飞已在其手上（停滞的由
		// stale-claim sweep 追），列出只会白白唤醒。
		var boardTasks []*database.TeamTaskBase
		for _, tk := range incomplete {
			if tk.Status == "pending" && (tk.Assignee == nil || *tk.Assignee == "" || *tk.Assignee == memberName) {
				boardTasks = append(boardTasks, tk)
			}
		}
		if len(boardTasks) == 0 {
			return
		}
		// Teammate 保留正文：正文不是它写的，它靠正文与自己的专长匹配选活。
		nowMs := database.GetCurrentTime()
		lines := []string{mustT("dispatcher.teammate_task_list")}
		for _, tk := range boardTasks {
			line, rerr := taskrender.RenderTaskLine(taskBaseLike{tk}, nowMs, true, nil)
			if rerr != nil {
				logger.Warn(logComponent).Err(rerr).
					Str("task_id", tk.TaskID).
					Msg("nudgeIdleAgent: render_task_line failed")
				continue
			}
			lines = append(lines, line)
		}
		body = agentteams.RenderEvent("task-board", strings.Join(lines, "\n"), agentteams.EventOptions{})
	}

	// Python: deliver_input(..., use_steer=False)——见函数头注释。
	if err := h.round.DeliverInput(ctx, body, false, nil); err != nil {
		logger.Error(logComponent).Err(err).
			Str("member_name", memberName).
			Msg("nudgeIdleAgent: deliver_input failed")
	}
}

// justCompletedBlock 为 leader nudge 背后的任务渲染「刚完成」行（F_120）。
// 一条 headline、带完成摘要的任务行，以及——当还有更多可读（完成备注，或
// 截断的摘要）——view_task(action='note') 提示。未指名已完成任务时为空。
// Python: _just_completed_block (task_board.py L701-727，模块级)
func justCompletedBlock(ctx context.Context, allTasks []*database.TeamTaskBase, taskID string) []string {
	if taskID == "" {
		return nil
	}
	var task *database.TeamTaskBase
	for _, row := range allTasks {
		if row.TaskID == taskID {
			task = row
			break
		}
	}
	if task == nil || task.Status != "completed" {
		return nil
	}
	assignee := "-"
	if task.Assignee != nil && *task.Assignee != "" {
		assignee = *task.Assignee
	}
	line, tErr := schema.T("dispatcher.leader_just_completed_line",
		map[string]any{"task_id": task.TaskID, "title": task.Title, "assignee": assignee})
	if tErr != nil {
		logger.Warn(logComponent).Err(tErr).Str("key", "dispatcher.leader_just_completed_line").
			Msg("justCompletedBlock: i18n render failed")
		return nil
	}
	summary := prompts.CompletionSummary(task)
	if summary != "" {
		if s, serr := schema.T("dispatcher.leader_just_completed_summary", map[string]any{"summary": summary}); serr == nil {
			line += s
		}
	}
	lines := []string{mustT("dispatcher.leader_just_completed"), line}
	note := ""
	if task.Note != nil {
		note = *task.Note
	}
	if strings.TrimSpace(note) != "" || prompts.CompletionSummaryTruncated(task) {
		if h, herr := schema.T("dispatcher.leader_just_completed_note_hint", map[string]any{"task_id": task.TaskID}); herr == nil {
			lines = append(lines, h)
		}
	}
	return []string{strings.Join(lines, "\n")}
}

// payloadMemberList 从事件 payload 中取成员列表并归一为 []string。
// wire 载荷跨进程序列化后为 []any，进程内为 []string（既有双形态坑）。
// Python: payload.reviewer or []。
func payloadMemberList(v any) []string {
	switch list := v.(type) {
	case []string:
		return list
	case []any:
		out := make([]string, 0, len(list))
		for _, item := range list {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

// memberInList 判断 member 是否出现在成员列表中。
// Python: member_name not in reviewers。
func memberInList(reviewers []string, member string) bool {
	for _, m := range reviewers {
		if m == member {
			return true
		}
	}
	return false
}

// taskSummaryLike TaskSummary 的 TaskLike 适配（leader 看板行）。
type taskSummaryLike struct {
	s *tools.TaskSummary
}

func (a taskSummaryLike) TaskID() string { return a.s.TaskID }
func (a taskSummaryLike) Status() string { return a.s.Status }
func (a taskSummaryLike) Title() string  { return a.s.Title }
func (a taskSummaryLike) Content() string {
	// leader 行 with_content=false，正文不被消费；上游 summary 无正文列。
	return ""
}
func (a taskSummaryLike) Assignee() string { return a.s.Assignee }
func (a taskSummaryLike) UpdatedAtMs() *int64 {
	// 上游 render_task_line 的 now_ms 由任务行 updated_at 计算；摘要视图未带
	// 时间戳（list_tasks_with_deps 无该列），nil → FormatTimeContext 兜底。
	return nil
}

// taskBaseLike TeamTaskBase 的 TaskLike 适配（teammate 看板行）。
type taskBaseLike struct {
	t *database.TeamTaskBase
}

func (a taskBaseLike) TaskID() string { return a.t.TaskID }
func (a taskBaseLike) Status() string { return a.t.Status }
func (a taskBaseLike) Title() string  { return a.t.Title }
func (a taskBaseLike) Content() string { return a.t.Content }
func (a taskBaseLike) Assignee() string {
	if a.t.Assignee != nil {
		return *a.t.Assignee
	}
	return ""
}
func (a taskBaseLike) UpdatedAtMs() *int64 { return nil }

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
