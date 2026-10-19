package handlers

import (
	"context"
	"fmt"
	"strings"
	"time"

	agentteams "github.com/uapclaw/uapclaw-go/internal/agent_teams"
	types "github.com/uapclaw/uapclaw-go/internal/agent_teams/agent/coordination/types"
	schema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/schema/events"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools/database"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// MemberHandler 处理成员生命周期事件：spawned、restarted、status_changed、
// execution_changed、shutdown、canceled。
//
// EVENT_METHOD_MAP 事件到方法映射：
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
	memberName := h.blueprint.DispatcherMemberName()
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
				Msg("handleTeammateMemberEvent: cancel_agent failed")
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
				Msg("shutdownHumanAgent: shutdown_self failed")
		}
	}
}

// handleLeaderMemberEvent Leader 处理成员事件：记录日志 + 过期认领提醒。
// Python: MemberHandler._handle_leader_member_event
func (h *MemberHandler) handleLeaderMemberEvent(ctx context.Context, event types.CoordinationEvent) {
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
		h.nudgeIdleMemberWithStaleClaims(ctx, memberName, oldStatus, newStatus)
	}
}

// nudgeIdleMemberWithStaleClaims 当成员进入空闲状态时检查过期认领。
// 查询该成员名下 CLAIMED 状态、updated_at 超过阈值且未在节流窗口内的任务，
// 汇总为一条消息发送给该成员。
// S-32: 对齐 Python MemberHandler._nudge_idle_member_with_stale_claims
func (h *MemberHandler) nudgeIdleMemberWithStaleClaims(ctx context.Context, targetID, oldStatus, newStatus string) {
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
	// h.infra 接口本身可能为 nil（测试注入 nil），先判空再访问
	if h.infra == nil {
		return
	}
	taskMgr := h.infra.TaskManager()
	msgMgr := h.infra.MessageManager()
	if taskMgr == nil || msgMgr == nil {
		return
	}
	claimed, err := taskMgr.GetTasksByAssignee(ctx, targetID, string(schema.TaskStatusClaimed))
	if err != nil {
		logger.Error(logComponent).Err(err).
			Str("target_id", targetID).
			Msg("nudgeIdleMemberWithStaleClaims: get tasks by assignee failed")
		return
	}
	if len(claimed) == 0 {
		return
	}
	nowMS := float64(time.Now().UnixMilli())
	thresholdMS := staleClaimSeconds * 1000
	stale := make([]*database.TeamTaskBase, 0)
	for _, task := range claimed {
		if task.UpdatedAt == 0 {
			continue
		}
		if nowMS-float64(task.UpdatedAt) < thresholdMS {
			continue
		}
		lastNudge := h.staleClaimThrottle[task.TaskID]
		if nowMS/1000-lastNudge < staleClaimSeconds {
			continue
		}
		stale = append(stale, task)
	}
	if len(stale) == 0 {
		return
	}
	for _, task := range stale {
		h.staleClaimThrottle[task.TaskID] = nowMS / 1000
	}
	// 对齐 Python: lines = [t("dispatcher.stale_claim_header", count=len(stale))] + 任务列表
	header, err := agentteams.T("dispatcher.stale_claim_header", map[string]any{
		"count": len(stale),
	})
	if err != nil {
		logger.Error(logComponent).Err(err).Msg("nudgeIdleMemberWithStaleClaims: i18n lookup failed")
		return
	}
	lines := []string{header}
	for _, task := range stale {
		lines = append(lines, fmt.Sprintf("- [%s] %s: %s", task.TaskID, task.Title, task.Content))
	}
	if _, err := msgMgr.SendMessage(ctx, strings.Join(lines, "\n"), targetID, h.blueprint.DispatcherMemberName()); err != nil {
		logger.Error(logComponent).Err(err).
			Str("target_id", targetID).
			Msg("nudgeIdleMemberWithStaleClaims: send_message failed")
	}
	logger.Info(logComponent).
		Str("target_id", targetID).
		Int("stale_count", len(stale)).
		Str("new_status", newStatus).
		Msg("leader nudged member about stale claimed tasks")
}
