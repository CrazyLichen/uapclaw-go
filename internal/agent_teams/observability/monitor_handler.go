package observability

import (
	"context"
	"fmt"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/schema/events"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 常量 ────────────────────────────

var (
	// taskOpenTypes 任务开启事件类型集合
	// Python: _TASK_OPEN_TYPES = frozenset({TeamEvent.TASK_CREATED})
	taskOpenTypes = map[string]bool{
		events.TeamEventTaskCreated: true,
	}
	// taskCloseTypes 任务关闭事件类型集合
	// Python: _TASK_CLOSE_TYPES
	taskCloseTypes = map[string]bool{
		events.TeamEventTaskCompleted: true,
		events.TeamEventTaskCancelled: true,
		events.TeamEventTaskUnblocked: true,
	}
	// memberTypes 成员事件类型集合
	// Python: _MEMBER_TYPES
	memberTypes = map[string]bool{
		events.TeamEventMemberSpawned:          true,
		events.TeamEventMemberRestarted:        true,
		events.TeamEventMemberStatusChanged:    true,
		events.TeamEventMemberExecutionChanged: true,
		events.TeamEventMemberShutdown:         true,
		events.TeamEventMemberCanceled:         true,
	}
	// messageTypes 消息事件类型集合
	// Python: _MESSAGE_TYPES
	messageTypes = map[string]bool{
		events.TeamEventMessage:   true,
		events.TeamEventBroadcast: true,
	}
)

// ──────────────────────────── 结构体 ────────────────────────────

// OtelTeamMonitorHandler 消费 TeamAgent EventMessage 事件流的 OTel handler。
// Python: OtelTeamMonitorHandler (monitor_handler.py)
//
// 通过 TeamAgent.add_event_listener 注册，将团队/任务事件转换为 OTel span。
// 当前为桩实现：HandleEvent 逻辑完整，但 attach_to_team_agent 暂为 no-op（待 9.55 完成后回填）。
type OtelTeamMonitorHandler struct {
	// config 当前可观测性配置
	config *ObservabilityConfig
	// injectedTracer 可选显式注入的 tracer（测试用）
	injectedTracer trace.Tracer
	// teamSpans 活跃的 team span（key=teamName）
	teamSpans map[string]trace.Span
	// taskSpans 活跃的 task span（key=taskID）
	taskSpans map[string]trace.Span
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewOtelTeamMonitorHandler 创建 OtelTeamMonitorHandler。
// Python: OtelTeamMonitorHandler(config, tracer=...)
func NewOtelTeamMonitorHandler(config *ObservabilityConfig, tracer trace.Tracer) *OtelTeamMonitorHandler {
	return &OtelTeamMonitorHandler{
		config:         config,
		injectedTracer: tracer,
		teamSpans:      make(map[string]trace.Span),
		taskSpans:      make(map[string]trace.Span),
	}
}

// HandleEvent 事件分发入口。
// Python: OtelTeamMonitorHandler.__call__(self, event: EventMessage)
// 签名对齐 messager.MessagerHandler，9.55 回填时可直接传入 AddEventListener。
func (h *OtelTeamMonitorHandler) HandleEvent(_ context.Context, event *events.EventMessage) error {
	defer func() {
		if r := recover(); r != nil {
			logger.Warn(logComponent).Any("error", r).Str("event_type", event.EventType).
				Msg("otel monitor handler 异常")
		}
	}()

	etype := event.EventType
	payload := event.Payload
	if payload == nil {
		payload = make(map[string]any)
	}
	teamName := strVal(payload["team_name"])

	switch {
	case etype == events.TeamEventCreated:
		h.openTeamSpan(teamName, payload)
	case etype == events.TeamEventCleaned:
		h.closeTeamSpan(teamName)
	case etype == events.TeamEventStandby:
		h.recordTeamEvent(teamName, etype, map[string]any{ATEventType: etype})
	case taskOpenTypes[etype]:
		h.openTaskSpan(teamName, payload)
	case taskCloseTypes[etype]:
		h.closeTaskSpan(payload, etype)
	case etype == events.TeamEventTaskUpdated || etype == events.TeamEventTaskClaimed:
		h.recordTaskEvent(payload, etype)
	case memberTypes[etype]:
		h.recordMemberEvent(teamName, payload, etype)
	case messageTypes[etype]:
		h.recordMessageEvent(teamName, payload, etype)
	}
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// tracer 解析 tracer。
func (h *OtelTeamMonitorHandler) tracer() trace.Tracer {
	if h.injectedTracer != nil {
		return h.injectedTracer
	}
	return GetTracer("openjiuwen.agent_teams.observability.monitor")
}

// openTeamSpan 打开长生命周期的 team root span。
// Python: _open_team_span(team_name, payload)
func (h *OtelTeamMonitorHandler) openTeamSpan(teamName string, payload map[string]any) {
	if _, exists := h.teamSpans[teamName]; exists {
		return
	}
	// Python: span = self._tracer().start_span(name=f"team.{team_name}", kind=SpanKind.INTERNAL)
	_, span := h.tracer().Start(context.Background(), "team."+teamName, trace.WithSpanKind(trace.SpanKindInternal))
	span.SetAttributes(
		attribute.String(ATTeamName, teamName),
		attribute.String(ATTeamDisplayName, strVal(payload["display_name"], teamName)),
		attribute.String(ATEventType, events.TeamEventCreated),
	)
	if leader := strVal(payload["leader_member_name"]); leader != "" {
		span.SetAttributes(attribute.String(ATTeamLeader, leader))
	}
	h.teamSpans[teamName] = span
}

// closeTeamSpan 关闭 team root span。
// Python: _close_team_span(team_name)
func (h *OtelTeamMonitorHandler) closeTeamSpan(teamName string) {
	span, ok := h.teamSpans[teamName]
	if !ok {
		return
	}
	span.SetStatus(codes.Ok, "")
	span.End()
	delete(h.teamSpans, teamName)
}

// recordTeamEvent 在 team span 上记录事件。
// Python: _record_team_event(team_name, name, attrs)
func (h *OtelTeamMonitorHandler) recordTeamEvent(teamName string, name string, attrs map[string]any) {
	span, ok := h.teamSpans[teamName]
	if !ok {
		return
	}
	span.AddEvent(name, trace.WithAttributes(mapToAttributes(attrs)...))
}

// openTaskSpan 打开 per-task span。
// Python: _open_task_span(team_name, payload)
func (h *OtelTeamMonitorHandler) openTaskSpan(teamName string, payload map[string]any) {
	taskID := strVal(payload["task_id"])
	if taskID == "" {
		return
	}
	if _, exists := h.taskSpans[taskID]; exists {
		return
	}
	_, span := h.tracer().Start(context.Background(), "task."+taskID, trace.WithSpanKind(trace.SpanKindInternal))
	span.SetAttributes(attribute.String(ATTaskID, taskID))
	if teamName != "" {
		span.SetAttributes(attribute.String(ATTeamName, teamName))
	}
	if status := strVal(payload["status"]); status != "" {
		span.SetAttributes(attribute.String(ATTaskStatus, status))
	}
	assignee := strVal(payload["assignee"])
	if assignee == "" {
		assignee = strVal(payload["member_name"])
	}
	if assignee != "" {
		span.SetAttributes(attribute.String(ATTaskAssignee, assignee))
	}
	h.taskSpans[taskID] = span
}

// closeTaskSpan 关闭 task span。
// Python: _close_task_span(payload, etype)
func (h *OtelTeamMonitorHandler) closeTaskSpan(payload map[string]any, etype string) {
	taskID := strVal(payload["task_id"])
	span, ok := h.taskSpans[taskID]
	if !ok {
		return
	}
	// Python: span.set_attribute(AT_TASK_STATUS, etype.replace("task_", ""))
	span.SetAttributes(attribute.String(ATTaskStatus, strings.TrimPrefix(etype, "task_")))
	span.SetStatus(codes.Ok, "")
	span.End()
	delete(h.taskSpans, taskID)
}

// recordTaskEvent 在 task span 上记录事件。
// Python: _record_task_event(payload, etype)
func (h *OtelTeamMonitorHandler) recordTaskEvent(payload map[string]any, etype string) {
	taskID := strVal(payload["task_id"])
	span, ok := h.taskSpans[taskID]
	if !ok {
		return
	}
	attrs := map[string]any{ATEventType: etype, ATTaskID: taskID}
	if member := strVal(payload["member_name"]); member != "" {
		attrs[ATTaskAssignee] = member
	}
	span.AddEvent(etype, trace.WithAttributes(mapToAttributes(attrs)...))
}

// recordMemberEvent 在 team span 上记录 member 事件。
// Python: _record_member_event(team_name, payload, etype)
func (h *OtelTeamMonitorHandler) recordMemberEvent(teamName string, payload map[string]any, etype string) {
	attrs := map[string]any{
		ATEventType:  etype,
		ATMemberName: strVal(payload["member_name"]),
	}
	if _, ok := payload["old_status"]; ok {
		attrs[ATMemberStatusOld] = strVal(payload["old_status"])
	}
	if _, ok := payload["new_status"]; ok {
		attrs[ATMemberStatusNew] = strVal(payload["new_status"])
	}
	if _, ok := payload["reason"]; ok {
		attrs[ATMemberRestartReason] = strVal(payload["reason"])
	}
	if rc, ok := payload["restart_count"]; ok {
		attrs[ATMemberRestartCount] = toInt(rc)
	}
	if f, ok := payload["force"]; ok {
		attrs[ATMemberShutdownForce] = toBool(f)
	}
	h.recordTeamEvent(teamName, etype, attrs)
}

// recordMessageEvent 在 team span 上记录 message/broadcast 事件。
// Python: _record_message_event(team_name, payload, etype)
func (h *OtelTeamMonitorHandler) recordMessageEvent(teamName string, payload map[string]any, etype string) {
	attrs := map[string]any{
		ATEventType:        etype,
		ATMessageID:        strVal(payload["message_id"]),
		ATMessageFrom:      strVal(payload["from_member_name"]),
		ATMessageTo:        strVal(payload["to_member_name"]),
		ATMessageBroadcast: etype == events.TeamEventBroadcast,
	}
	h.recordTeamEvent(teamName, etype, attrs)
}

// strVal 从 map 中提取字符串值，支持默认值。
func strVal(v any, defaults ...string) string {
	if v == nil {
		if len(defaults) > 0 {
			return defaults[0]
		}
		return ""
	}
	return fmt.Sprintf("%v", v)
}

// toInt 将 any 转换为 int。
func toInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	default:
		return 0
	}
}

// toBool 将 any 转换为 bool。
func toBool(v any) bool {
	switch b := v.(type) {
	case bool:
		return b
	default:
		return false
	}
}

// mapToAttributes 将 map[string]any 转换为 []attribute.KeyValue。
func mapToAttributes(m map[string]any) []attribute.KeyValue {
	attrs := make([]attribute.KeyValue, 0, len(m))
	for k, v := range m {
		switch val := v.(type) {
		case string:
			attrs = append(attrs, attribute.String(k, val))
		case int:
			attrs = append(attrs, attribute.Int(k, val))
		case int64:
			attrs = append(attrs, attribute.Int64(k, val))
		case float64:
			attrs = append(attrs, attribute.Float64(k, val))
		case bool:
			attrs = append(attrs, attribute.Bool(k, val))
		default:
			attrs = append(attrs, attribute.String(k, fmt.Sprintf("%v", v)))
		}
	}
	return attrs
}
