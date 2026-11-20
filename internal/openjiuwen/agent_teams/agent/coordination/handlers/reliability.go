// Package handlers leader 侧可靠性协调 handler（25.1g）。
//
// 对应 Python：agent_teams/reliability/handler.py。
// 消费 leader 的跨进程异常流，按策略把每个异常路由进 leader 自己的轮次
// （leader LLM 用既有团队工具决策：发消息 / 取消任务 / 停成员 / spawn）。
// 同时对消息事件跑团队级 pingpong 检测——这是任何成员级 rail 都观测不到的。
// 镜像 StaleTaskHandler：读状态 + 窄协议 nudge 的协调 handler。
package handlers

import (
	"context"

	types "github.com/uapclaw/uapclaw-go/internal/openjiuwen/agent_teams/agent/coordination/types"
	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/agent_teams/rails/reliability/core"
	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/agent_teams/rails/reliability/detectors"
	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/agent_teams/rails/reliability/remediation"
	schema "github.com/uapclaw/uapclaw-go/internal/openjiuwen/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/agent_teams/schema/events"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// routeNoticeKey 路由通告去重键：哪个路由器、哪个 deployment、为何失败。
//
// Python: handler.py _route_notice_key 返回的四元组。共用一个模型组路由器的成员
// 会看到相同故障，所以键不含成员；钉死模型的成员有自己的路由器（pinned_model）
// 且自行切换，故钉住的模型是键的一部分。failures[0] 是抛错的 deployment。
type routeNoticeKey struct {
	pinnedModel  any
	deploymentID any
	reason       any
	statusCode   any
}

// ReliabilityHandler 把上报的异常按策略路由进 leader 循环 + 团队级 pingpong 检测。
//
// Python: handler.py ReliabilityHandler（仅 leader）。
type ReliabilityHandler struct {
	BaseCoordinationHandler
	// policy 严重度→动作策略
	policy *remediation.RemediationPolicy
	// pingpong 团队级乒乓检测器
	pingpong *detectors.PingPongDetector
	// noticed 已通告的 route notices（按 routeNoticeKey 去重）
	noticed map[routeNoticeKey]bool
	// pendingRouteNotices leader 自己调用产生的 route notices，等下一次输入
	pendingRouteNotices []string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewReliabilityHandler 构造可靠性协调 handler。
//
// Python: handler.py ReliabilityHandler.__init__。
func NewReliabilityHandler(
	host types.DispatcherHost,
	bp types.DispatcherBlueprint,
	inf types.DispatcherInfra,
	pollCtrl types.PollController,
	policy *remediation.RemediationPolicy,
	pingpong *detectors.PingPongDetector,
) *ReliabilityHandler {
	return &ReliabilityHandler{
		BaseCoordinationHandler: NewBaseCoordinationHandler(host, bp, inf, pollCtrl),
		policy:                  policy,
		pingpong:                pingpong,
		noticed:                 make(map[routeNoticeKey]bool),
	}
}

// GetCallbacks 返回 EVENT_METHOD_MAP（3 项）。
//
// Python: handler.py EVENT_METHOD_MAP = {ANOMALY_DETECTED: on_anomaly_detected,
// MESSAGE: on_message, BROADCAST: on_message}。
func (h *ReliabilityHandler) GetCallbacks() map[string]types.EventCallbackFunc {
	return map[string]types.EventCallbackFunc{
		events.TeamEventAnomalyDetected: func(ctx context.Context, event types.CoordinationEvent) {
			h.onAnomalyDetected(ctx, event)
		},
		events.TeamEventMessage: func(ctx context.Context, event types.CoordinationEvent) {
			h.onMessage(ctx, event)
		},
		events.TeamEventBroadcast: func(ctx context.Context, event types.CoordinationEvent) {
			h.onMessage(ctx, event)
		},
	}
}

// ──────────────────────────── 事件处理 ────────────────────────────

// onAnomalyDetected 按策略把上报的异常路由进 leader 循环。
//
// Python: handler.py ReliabilityHandler.on_anomaly_detected。
func (h *ReliabilityHandler) onAnomalyDetected(ctx context.Context, event types.CoordinationEvent) {
	if h.blueprint.Role() != schema.TeamRoleLeader {
		return
	}
	if event.Transport == nil || event.Transport.EventType != events.TeamEventAnomalyDetected {
		return
	}
	logger.Info(logComponent).
		Str("member", strAny(event.Transport.Payload["member_name"])).
		Str("kind", strAny(event.Transport.Payload["kind"])).
		Str("severity", strAny(event.Transport.Payload["severity"])).
		Msg("reliability anomaly")
	if strAny(event.Transport.Payload["kind"]) == string(core.AnomalyKindModelRouteNotice) {
		h.deliverRouteNotice(ctx,
			strAny(event.Transport.Payload["summary"]),
			event.Transport.Payload["evidence"])
		return
	}
	severity := core.Severity(strAny(event.Transport.Payload["severity"]))
	h.route(ctx, severity, formatEventSummary(event.Transport.Payload))
}

// onMessage 对消息事件跑团队级 pingpong 检测（仅 leader）。
//
// Python: handler.py ReliabilityHandler.on_message。
func (h *ReliabilityHandler) onMessage(ctx context.Context, event types.CoordinationEvent) {
	if h.blueprint.Role() != schema.TeamRoleLeader {
		return
	}
	if event.Transport == nil {
		return
	}
	et := event.Transport.EventType
	if et != events.TeamEventMessage && et != events.TeamEventBroadcast {
		return
	}
	to := strAny(event.Transport.Payload["to_member_name"])
	anomaly := h.pingpong.Observe(&core.Signal{
		Kind:       core.SignalKindMessage,
		MemberName: strAny(event.Transport.Payload["from_member_name"]),
		PeerMember: &to,
	})
	if anomaly != nil {
		h.route(ctx, anomaly.Severity, anomaly.Summary)
	}
}

// HandleLocalAnomaly 把 leader 本地异常直送 leader 循环。
//
// Python: handler.py ReliabilityHandler.handle_local_anomaly。leader 自检：自己的
// rail 在进程内喂异常到此处（绕过 messager self-filter），走与跨进程异常事件相同的
// 策略路由。关于 leader 自己调用的 route notice 等它下一次被受理的输入而不是
// 立即投递：该调用已在切换后的模型上出了答案，通告轮会把答案顶掉。记录通告仍会
// 在成员稍后上报同一故障时把它丢弃。拿不到裁决时仅记日志：投递会在同一故障模型上
// 再开一轮 leader 轮次，失败后再次上报；失败的那轮已经给用户展示了可读错误。
func (h *ReliabilityHandler) HandleLocalAnomaly(ctx context.Context, anomaly *core.Anomaly) {
	if h.blueprint.Role() != schema.TeamRoleLeader {
		return
	}
	if anomaly.Kind == core.AnomalyKindModelRouteNotice {
		key := noticeKeyFromEvidence(anomaly.Evidence)
		if key != nil {
			if h.noticed[*key] {
				return
			}
			h.noticed[*key] = true
		}
		h.pendingRouteNotices = append(h.pendingRouteNotices, h.routeNoticeText(ctx, anomaly.Summary, anomaly.Evidence))
		return
	}
	if anomaly.Kind == core.AnomalyKindModelUnavailable {
		logger.Warn(logComponent).
			Str("summary", anomaly.Summary).
			Msg("leader model anomaly (not delivered to leader)")
		return
	}
	h.route(ctx, anomaly.Severity, formatAnomalySummary(anomaly))
}

// TakePendingRouteNotices 返回 leader 自己的、等下一次输入的 route notices 并清空。
//
// Python: handler.py ReliabilityHandler.take_pending_route_notices。可靠性 rail 把
// 返回值折进 leader 下一次被受理输入的最前面，这样通告既不自己开一轮（会顶掉
// 切换后模型已给出的答案）也不会丢失。
func (h *ReliabilityHandler) TakePendingRouteNotices() []string {
	notices := h.pendingRouteNotices
	h.pendingRouteNotices = nil
	return notices
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// deliverRouteNotice 每（路由器, deployment, 原因）组合通告一次 deployment 切换。
//
// Python: handler.py ReliabilityHandler._deliver_route_notice。切换是模型组路由器的
// 属性而非恰巧命中它的成员的属性，所以通告不点名成员，第二个上报同一故障的成员
// 会被丢弃。通告说明路由器服务谁：未钉模型的全部成员，或只钉到 pinned_model 的
// 成员。
func (h *ReliabilityHandler) deliverRouteNotice(ctx context.Context, summary string, evidence any) {
	key := noticeKeyFromEvidence(mapOrNone(evidence))
	if key != nil {
		if h.noticed[*key] {
			return
		}
		h.noticed[*key] = true
	}
	if err := h.round.DeliverInput(ctx, h.routeNoticeText(ctx, summary, mapOrNone(evidence)), true, nil); err != nil {
		logger.Error(logComponent).Err(err).Msg("deliver_route_notice: deliver_input failed")
	}
}

// routeNoticeText 渲染 leader 读的 route notice。
//
// Python: handler.py ReliabilityHandler._route_notice_text。
func (h *ReliabilityHandler) routeNoticeText(_ context.Context, summary string, evidence map[string]any) string {
	verdict := routeVerdict(evidence)
	var pinned string
	var scope string
	if verdict != nil {
		pinned = strAny(verdict["pinned_model"])
	}
	if pinned != "" {
		scope = i18nT("reliability.route_notice_scope_pinned", map[string]any{"model": pinned})
	} else {
		scope = i18nT("reliability.route_notice_scope_group", nil)
	}
	return i18nT("reliability.route_notice", map[string]any{"summary": summary, "scope": scope})
}

// route 按严重度把报告或升级投进 leader 自己的轮次。
//
// Python: handler.py ReliabilityHandler._route（if ESCALATE_USER elif REPORT_LEADER）。
func (h *ReliabilityHandler) route(ctx context.Context, severity core.Severity, summary string) {
	actions := h.policy.ActionsFor(severity)
	escalate, report := false, false
	for _, a := range actions {
		if a == remediation.RemediationActionEscalateUser {
			escalate = true
		}
		if a == remediation.RemediationActionReportLeader {
			report = true
		}
	}
	var content string
	if escalate {
		content = i18nT("reliability.escalate_user", map[string]any{"summary": summary})
	} else if report {
		content = i18nT("reliability.report_leader", map[string]any{"summary": summary})
	} else {
		return
	}
	if err := h.round.DeliverInput(ctx, content, true, nil); err != nil {
		logger.Error(logComponent).Err(err).Msg("route anomaly: deliver_input failed")
	}
}

// ──────────────────────────── 包级助手（handler.py 模块级函数） ────────────────────────────

// noticeKeyFromEvidence 从 evidence 提取路由通告去重键；拿不到裁决返回 nil。
//
// Python: handler.py _route_notice_key。
func noticeKeyFromEvidence(evidence map[string]any) *routeNoticeKey {
	verdict := routeVerdict(evidence)
	if verdict == nil {
		return nil
	}
	failures := dictListAny(verdict["failures"])
	if len(failures) == 0 {
		return nil
	}
	trigger := failures[0]
	return &routeNoticeKey{
		pinnedModel:  verdict["pinned_model"],
		deploymentID: trigger["deployment_id"],
		reason:       verdict["reason"],
		statusCode:   trigger["status_code"],
	}
}

// routeVerdict 取 evidence 里的 model_failure 裁决（非 dict 返回 nil）。
//
// Python: handler.py _route_verdict。
func routeVerdict(evidence map[string]any) map[string]any {
	if evidence == nil {
		return nil
	}
	if m, ok := evidence["model_failure"].(map[string]any); ok {
		return m
	}
	return nil
}

// formatEventSummary 把异常事件渲染成 leader 看的一行摘要。
//
// Python: handler.py ReliabilityHandler._format。
func formatEventSummary(payload map[string]any) string {
	return "[" + strAny(payload["severity"]) + "] " + strAny(payload["member_name"]) + ": " +
		strAny(payload["summary"]) + " (detector=" + strAny(payload["detector"]) + ")"
}

// formatAnomalySummary 把本地异常渲染成一行摘要。
//
// Python: handler.py ReliabilityHandler._format_anomaly。
func formatAnomalySummary(anomaly *core.Anomaly) string {
	return "[" + string(anomaly.Severity) + "] " + anomaly.MemberName + ": " +
		anomaly.Summary + " (detector=" + anomaly.Detector + ")"
}

// ──────────────────────────── 小助手 ────────────────────────────

// strAny any→string（nil 返回空串；非 string 用 %v）。
func strAny(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	logger.Debug(logComponent).Msg("reliability payload field not a string")
	return ""
}

// dictListAny any→[]map[string]any（过滤非 dict 元素）。
func dictListAny(v any) []map[string]any {
	list, ok := v.([]any)
	if !ok {
		return nil
	}
	var out []map[string]any
	for _, item := range list {
		if m, ok := item.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

// mapOrNone any → map[string]any（nil 安全）。
func mapOrNone(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return nil
}

// i18nT i18n 渲染小包装（错误降级为 key 本身）。
func i18nT(key string, kwargs map[string]any) string {
	s, err := schema.T(key, kwargs)
	if err != nil {
		return key
	}
	return s
}
