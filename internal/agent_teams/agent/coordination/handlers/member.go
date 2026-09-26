package handlers

import (
	"context"

	types "github.com/uapclaw/uapclaw-go/internal/agent_teams/agent/coordination/types"
	schema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/schema/events"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// MemberHandler 处理成员生命周期事件：spawned、restarted、status_changed、
// execution_changed、shutdown、canceled。
//
// EVENT_METHOD_MAP:
//   - member_spawned          → onMemberEvent
//   - member_restarted        → onMemberEvent
//   - member_status_changed   → onMemberEvent
//   - member_execution_changed → onMemberEvent
//   - member_shutdown          → onMemberEvent
//   - member_canceled          → onMemberEvent
//
// Python: MemberHandler
type MemberHandler struct {
	BaseCoordinationHandler
	// staleClaimThrottle 过期认领节流映射（task_id → 上次提醒时间戳），
	// 与 StaleTaskHandler 共享引用。
	// Python: _last_stale_nudge
	staleClaimThrottle map[string]float64
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// staleClaimSeconds 过期认领阈值（秒），与 Python _STALE_CLAIM_SECONDS 对齐
	staleClaimSeconds = 600.0
)

// ──────────────────────────── 全局变量 ────────────────────────────

// idleNudgeStatuses 触发过期认领提醒的空闲状态集合。
// 对齐 Python: _IDLE_NUDGE_STATUSES = frozenset({MemberStatus.READY.value, MemberStatus.ERROR.value})
var idleNudgeStatuses = map[string]bool{
	string(schema.MemberStatusReady): true,
	string(schema.MemberStatusError): true,
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewMemberHandler 创建 MemberHandler 实例。
// staleClaimThrottle 与 StaleTaskHandler 共享引用。
// Python: MemberHandler.__init__
func NewMemberHandler(
	host types.DispatcherHost,
	bp types.DispatcherBlueprint,
	inf types.DispatcherInfra,
	pollCtrl types.PollController,
	staleClaimThrottle map[string]float64,
) *MemberHandler {
	if staleClaimThrottle == nil {
		staleClaimThrottle = make(map[string]float64)
	}
	return &MemberHandler{
		BaseCoordinationHandler: NewBaseCoordinationHandler(host, bp, inf, pollCtrl),
		staleClaimThrottle:      staleClaimThrottle,
	}
}

// GetCallbacks 返回 event_key → 回调方法注册表。
// Python: MemberHandler.get_callbacks
func (h *MemberHandler) GetCallbacks() map[string]types.EventCallbackFunc {
	return map[string]types.EventCallbackFunc{
		events.TeamEventMemberSpawned:          h.OnMemberEvent,
		events.TeamEventMemberRestarted:        h.OnMemberEvent,
		events.TeamEventMemberStatusChanged:    h.OnMemberEvent,
		events.TeamEventMemberExecutionChanged: h.OnMemberEvent,
		events.TeamEventMemberShutdown:         h.OnMemberEvent,
		events.TeamEventMemberCanceled:         h.OnMemberEvent,
	}
}

// StaleClaimThrottle 返回共享的过期认领节流映射引用。
// 供测试验证 Member 和 StaleTask handler 共享同一映射。
func (h *MemberHandler) StaleClaimThrottle() map[string]float64 {
	return h.staleClaimThrottle
}

// OnMemberEvent 根据角色分发成员事件处理。
// Leader 走 handleLeaderMemberEvent，teammate 走 handleTeammateMemberEvent。
// Python: MemberHandler.on_member_event
func (h *MemberHandler) OnMemberEvent(ctx context.Context, event types.CoordinationEvent) {
	if event.IsInner() {
		return
	}
	role := h.blueprint.Role()
	if role == schema.TeamRoleLeader {
		h.handleLeaderMemberEvent(ctx, event)
	} else {
		h.handleTeammateMemberEvent(ctx, event)
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// handleTeammateMemberEvent 非 leader 成员处理成员事件。
// 仅处理目标为自己的事件：MEMBER_CANCELED 调 cancel_agent，
// MEMBER_SHUTDOWN 对于 HUMAN_AGENT 走关闭流程。
// Python: MemberHandler._handle_teammate_member_event
func (h *MemberHandler) handleTeammateMemberEvent(ctx context.Context, event types.CoordinationEvent) {
	em := event.Transport
	targetMember, _ := em.Payload["member_name"].(string)
	memberName := h.blueprint.MemberName()
	// 对齐 Python: 只关心自己的事件
	if targetMember != "" && targetMember != memberName {
		return
	}

	switch em.EventType {
	case events.TeamEventMemberCanceled:
		if err := h.round.CancelAgent(ctx); err != nil {
			logger.Error(logComponent).
				Err(err).
				Str("event_type", em.EventType).
				Msg("handleTeammateMemberEvent: cancel_agent 失败")
		}
	case events.TeamEventMemberShutdown:
		role := h.blueprint.Role()
		if role == schema.TeamRoleHumanAgent {
			h.shutdownHumanAgent(ctx, event)
		}
	}
}

// shutdownHumanAgent 处理 human-agent 关闭逻辑。
// 强制或无 in-flight round 时直接关闭，否则交给 round-end 检查。
// Python: MemberHandler._shutdown_human_agent
func (h *MemberHandler) shutdownHumanAgent(ctx context.Context, event types.CoordinationEvent) {
	em := event.Transport
	force, _ := em.Payload["force"].(bool)
	if force || !h.round.HasInFlightRound() {
		if err := h.lifecycle.ShutdownSelf(ctx); err != nil {
			logger.Error(logComponent).
				Err(err).
				Msg("shutdownHumanAgent: shutdown_self 失败")
		}
	}
}

// handleLeaderMemberEvent Leader 处理成员事件：记录日志 + 过期认领提醒。
// Python: MemberHandler._handle_leader_member_event
func (h *MemberHandler) handleLeaderMemberEvent(_ context.Context, event types.CoordinationEvent) {
	em := event.Transport
	// 对齐 Python: 记录每个成员生命周期转换
	logger.Info(logComponent).
		Str("event_type", em.EventType).
		Str("member_name", func() string {
			n, _ := em.Payload["member_name"].(string)
			return n
		}()).
		Msg("leader: member event received")

	// 对齐 Python: MEMBER_STATUS_CHANGED 时触发过期认领提醒
	if em.EventType == events.TeamEventMemberStatusChanged {
		memberName, _ := em.Payload["member_name"].(string)
		oldStatus, _ := em.Payload["old_status"].(string)
		newStatus, _ := em.Payload["new_status"].(string)
		h.nudgeIdleMemberWithStaleClaims(memberName, oldStatus, newStatus)
	}
}

// nudgeIdleMemberWithStaleClaims 当成员进入空闲状态时检查过期认领。
// 对齐 Python: MemberHandler._nudge_idle_member_with_stale_claims
// TODO(#9.63): 等消息管理器和任务后端接口就绪后补充完整提醒逻辑
func (h *MemberHandler) nudgeIdleMemberWithStaleClaims(targetID, oldStatus, newStatus string) {
	// 对齐 Python: if not target_id: return
	if targetID == "" {
		return
	}
	// 对齐 Python: if new_status == old_status: return
	if newStatus == oldStatus {
		return
	}
	// 仅在转换到空闲状态时触发
	if !idleNudgeStatuses[newStatus] {
		return
	}
	_ = oldStatus // 预留：Python 会根据 old_status 判断是否需要提醒
	_ = targetID  // 预留：向目标成员发送过期认领提醒
	logger.Debug(logComponent).
		Str("target_id", targetID).
		Str("new_status", newStatus).
		Float64("stale_threshold", staleClaimSeconds).
		Msg("nudgeIdleMemberWithStaleClaims: 检查过期认领")
}
