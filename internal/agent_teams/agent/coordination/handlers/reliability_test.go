// Package handlers 可靠性/外部运行时 handler 单元测试。
//
// 对应 Python：handler.py / external_handler.py 行为内联断言。
package handlers

import (
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"

	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	coordtypes "github.com/uapclaw/uapclaw-go/internal/agent_teams/agent/coordination/types"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/rails/reliability/core"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/rails/reliability/detectors"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/rails/reliability/remediation"
	schema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/schema/events"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interaction"
)

// ──────────────────────────── 测试桩 ────────────────────────────

// fakeRecordingHost 记录 deliver_input 内容的 host 桩。
type fakeRecordingHost struct {
	delivered []string
}

func (f *fakeRecordingHost) IsAgentReady() bool                { return true }
func (f *fakeRecordingHost) IsAgentRunning() bool              { return false }
func (f *fakeRecordingHost) HasInFlightRound() bool            { return false }
func (f *fakeRecordingHost) HasPendingInterrupt() bool         { return false }
func (f *fakeRecordingHost) CancelAgent(context.Context) error { return nil }
func (f *fakeRecordingHost) DeliverInput(_ context.Context, content string, _ bool, runContext *agentinterfaces.RunContext) error {
	f.delivered = append(f.delivered, content)
	return nil
}
func (f *fakeRecordingHost) AutoStartMember(_ context.Context, _ string) bool { return true }
func (f *fakeRecordingHost) ResumeInterrupt(context.Context, *interaction.InteractiveInput) error {
	return nil
}
func (f *fakeRecordingHost) AnnounceTaskRevisions(context.Context) error            { return nil }
func (f *fakeRecordingHost) ShutdownSelf(context.Context) error                     { return nil }
func (f *fakeRecordingHost) ConcludeCompletedRound(context.Context, int, int) error { return nil }

// newHostAndHandler 构造记录 host + handler（leader 或成员角色）。
func newHostAndHandler(role schema.TeamRole, policy *remediation.RemediationPolicy, volleys int) (*fakeRecordingHost, *ReliabilityHandler) {
	host := &fakeRecordingHost{}
	h := NewReliabilityHandler(host, &fakeBP{role: role}, nil, nil, policy, detectors.NewPingPongDetector(volleys))
	return host, h
}

// anomalyEvent 构造异常事件。
func anomalyEvent(member, kind, severity, summary, detector string, evidence map[string]any) coordtypes.CoordinationEvent {
	return coordtypes.CoordinationEvent{
		Transport: &events.EventMessage{
			EventType: events.TeamEventAnomalyDetected,
			Payload: map[string]any{
				"member_name": member, "kind": kind, "severity": severity,
				"summary": summary, "detector": detector, "evidence": evidence,
			},
		},
	}
}

// routeNoticeVerdict 构造路由裁决 evidence。
func routeNoticeVerdict(deployment string, status int) map[string]any {
	return map[string]any{
		"model_failure": map[string]any{
			"route_case": "switchable", "reason": "deterministic",
			"failures": []any{map[string]any{"deployment_id": deployment, "status_code": status}},
		},
	}
}

// ──────────────────────────── ReliabilityHandler ────────────────────────────

func TestReliabilityHandler_非leader恒Return(t *testing.T) {
	host, h := newHostAndHandler(schema.TeamRoleTeammate, remediation.NewRemediationPolicy(nil), 6)
	h.GetCallbacks()[events.TeamEventAnomalyDetected](context.Background(),
		anomalyEvent("coder", "tool_call_loop", "high", "s", "repeat_tool_call", nil))
	assert.Empty(t, host.delivered, "非 leader 不路由")
	assert.NotPanics(t, func() {
		h.HandleLocalAnomaly(context.Background(), &core.Anomaly{Kind: core.AnomalyKindToolCallLoop, Severity: core.SeverityHigh, MemberName: "c"})
	})
	assert.Empty(t, host.delivered)
}

func TestReliabilityHandler_异常事件按severity路由(t *testing.T) {
	host, h := newHostAndHandler(schema.TeamRoleLeader, remediation.NewRemediationPolicy(nil), 6)
	h.GetCallbacks()[events.TeamEventAnomalyDetected](context.Background(),
		anomalyEvent("coder", "tool_call_loop", "medium", "loop detected", "repeat_tool_call", nil))
	assert.Len(t, host.delivered, 1)
	assert.Contains(t, host.delivered[0], "[可靠性告警]")
	assert.Contains(t, host.delivered[0], "[medium] coder: loop detected (detector=repeat_tool_call)")
}

func TestReliabilityHandler_ESCALATE优先于REPORT(t *testing.T) {
	policy := remediation.NewRemediationPolicy(map[core.Severity][]remediation.RemediationAction{
		core.SeverityHigh: {remediation.RemediationActionLocalSteer, remediation.RemediationActionReportLeader,
			remediation.RemediationActionEscalateUser},
	})
	host, h := newHostAndHandler(schema.TeamRoleLeader, policy, 6)
	h.GetCallbacks()[events.TeamEventAnomalyDetected](context.Background(),
		anomalyEvent("coder", "tool_call_loop", "high", "s", "d", nil))
	assert.Len(t, host.delivered, 1)
	assert.Contains(t, host.delivered[0], "[可靠性·严重]", "ESCALATE_USER 优先（对齐 if/elif 顺序）")
}

func TestReliabilityHandler_routeNotice去重与本地pending(t *testing.T) {
	host, h := newHostAndHandler(schema.TeamRoleLeader, remediation.NewRemediationPolicy(nil), 6)
	cb := h.GetCallbacks()[events.TeamEventAnomalyDetected]
	cb(context.Background(), anomalyEvent("coder", "model_route_notice", "medium", "s1", "model_error", routeNoticeVerdict("dep1", 401)))
	cb(context.Background(), anomalyEvent("writer", "model_route_notice", "medium", "s2", "model_error", routeNoticeVerdict("dep1", 401)))
	assert.Len(t, host.delivered, 1, "同 (deployment, reason, status) 只通告一次")
	assert.Contains(t, host.delivered[0], "[模型路由通知]")
	// leader 本地同键 → 丢弃（成员已报）；异键 → pending
	h.HandleLocalAnomaly(context.Background(), &core.Anomaly{
		Kind: core.AnomalyKindModelRouteNotice, Severity: core.SeverityMedium,
		MemberName: "leader", Summary: "s3", Evidence: routeNoticeVerdict("dep1", 401),
	})
	assert.Len(t, host.delivered, 1)
	assert.Empty(t, h.TakePendingRouteNotices(), "同键被成员通告先占，本地丢弃")
	h.HandleLocalAnomaly(context.Background(), &core.Anomaly{
		Kind: core.AnomalyKindModelRouteNotice, Severity: core.SeverityMedium,
		MemberName: "leader", Summary: "s4", Evidence: routeNoticeVerdict("dep2", 500),
	})
	assert.Len(t, h.TakePendingRouteNotices(), 1, "异键进 pending 等下次输入")
}

func TestReliabilityHandler_MODEL_UNAVAILABLE本地仅告警(t *testing.T) {
	host, h := newHostAndHandler(schema.TeamRoleLeader, remediation.NewRemediationPolicy(nil), 6)
	h.HandleLocalAnomaly(context.Background(), &core.Anomaly{
		Kind: core.AnomalyKindModelUnavailable, Severity: core.SeverityCritical, MemberName: "leader",
	})
	assert.Empty(t, host.delivered, "不可用裁决不投递（避免同故障模型上再开轮）")
	assert.Empty(t, h.TakePendingRouteNotices())
}

func TestReliabilityHandler_onMessage喂团队级pingpong(t *testing.T) {
	host, h := newHostAndHandler(schema.TeamRoleLeader, remediation.NewRemediationPolicy(nil), 2)
	msgEvent := func(from, to string) coordtypes.CoordinationEvent {
		return coordtypes.CoordinationEvent{Transport: &events.EventMessage{
			EventType: events.TeamEventMessage,
			Payload:   map[string]any{"from_member_name": from, "to_member_name": to},
		}}
	}
	cb := h.GetCallbacks()[events.TeamEventMessage]
	for i := 0; i < 2; i++ {
		cb(context.Background(), msgEvent("alice", "bob"))
		cb(context.Background(), msgEvent("bob", "alice"))
	}
	assert.Len(t, host.delivered, 2, "MEDIUM(连击2) 上报后升档 HIGH(连击4) 再上报——边沿触发只升不降")
	assert.Contains(t, host.delivered[0], "2 consecutive volleys")
	assert.Contains(t, host.delivered[1], "4 consecutive volleys")
}

func TestReliabilityHandler_非leader消息不观测(t *testing.T) {
	host, h := newHostAndHandler(schema.TeamRoleTeammate, remediation.NewRemediationPolicy(nil), 1)
	h.GetCallbacks()[events.TeamEventMessage](context.Background(), coordtypes.CoordinationEvent{
		Transport: &events.EventMessage{
			EventType: events.TeamEventMessage,
			Payload:   map[string]any{"from_member_name": "a", "to_member_name": "b"},
		},
	})
	assert.Empty(t, host.delivered)
}

func TestReliabilityHandler_无裁决evidence直投(t *testing.T) {
	host, h := newHostAndHandler(schema.TeamRoleLeader, remediation.NewRemediationPolicy(nil), 6)
	h.GetCallbacks()[events.TeamEventAnomalyDetected](context.Background(),
		anomalyEvent("coder", "model_route_notice", "medium", "s", "model_error", nil))
	assert.Len(t, host.delivered, 1, "无裁决 evidence 的 route notice 直接投递")
}

// ──────────────────────────── ExternalRuntimeHandler ────────────────────────────

// newExternalHostAndHandler 构造记录 host + external handler。
func newExternalHostAndHandler(role schema.TeamRole) (*fakeRecordingHost, *ExternalRuntimeHandler) {
	host := &fakeRecordingHost{}
	h := NewExternalRuntimeHandler(host, &fakeBP{role: role}, nil, nil)
	return host, h
}

func TestExternalRuntimeHandler_重试进度渲染(t *testing.T) {
	host, h := newExternalHostAndHandler(schema.TeamRoleLeader)
	attempt, maxAttempts := 2, 5
	h.GetCallbacks()[events.TeamEventExternalRuntimeRetrying](context.Background(), coordtypes.CoordinationEvent{
		Transport: &events.EventMessage{
			EventType: events.TeamEventExternalRuntimeRetrying,
			Payload: map[string]any{
				"member_name": "coder", "agent_kind": "claude_code", "model": "glm-4",
				"category": "rate_limited", "summary": "429", "attempt": attempt,
				"max_attempts": maxAttempts,
				"reason":       map[string]any{"message": "quota", "http_status": 429, "sdk_error_code": "E429"},
			},
		},
	})
	assert.Len(t, host.delivered, 1)
	assert.Contains(t, host.delivered[0], "[三方运行时]")
	assert.Contains(t, host.delivered[0], "成员 coder（claude_code，模型 glm-4）")
	assert.Contains(t, host.delivered[0], "进度 2/5")
}

func TestExternalRuntimeHandler_字段缺省占位(t *testing.T) {
	host, h := newExternalHostAndHandler(schema.TeamRoleLeader)
	h.GetCallbacks()[events.TeamEventExternalRuntimeRetrying](context.Background(), coordtypes.CoordinationEvent{
		Transport: &events.EventMessage{
			EventType: events.TeamEventExternalRuntimeRetrying,
			Payload:   map[string]any{},
		},
	})
	assert.Len(t, host.delivered, 1)
	assert.Contains(t, host.delivered[0], "成员 unknown", "member_name 缺省 → unknown")
	assert.Contains(t, host.delivered[0], "模型 <unknown>", "model 缺省 → <unknown>")
}

func TestExternalRuntimeHandler_非leader与类型不符(t *testing.T) {
	host, h := newExternalHostAndHandler(schema.TeamRoleTeammate)
	h.GetCallbacks()[events.TeamEventExternalRuntimeRetrying](context.Background(), coordtypes.CoordinationEvent{
		Transport: &events.EventMessage{EventType: events.TeamEventExternalRuntimeRetrying, Payload: map[string]any{}},
	})
	assert.Empty(t, host.delivered, "非 leader 恒 return")
	host2, h2 := newExternalHostAndHandler(schema.TeamRoleLeader)
	h2.GetCallbacks()[events.TeamEventExternalRuntimeRetrying](context.Background(),
		coordtypes.CoordinationEvent{Transport: &events.EventMessage{EventType: "other", Payload: map[string]any{}}})
	assert.Empty(t, host2.delivered, "事件类型不符跳过")
}

// ──────────────────────────── 包级助手 ────────────────────────────

func TestHandlerHelpers(t *testing.T) {
	assert.Equal(t, "abc", strAny("abc"))
	assert.Equal(t, "", strAny(nil))
	assert.Len(t, dictListAny([]any{map[string]any{}, 42, "x"}), 1)
	assert.Nil(t, dictListAny("not list"))
	assert.Nil(t, mapOrNone("not map"))
	assert.Nil(t, noticeKeyFromEvidence(nil))
	assert.Nil(t, noticeKeyFromEvidence(map[string]any{"model_failure": "not dict"}))
	assert.Nil(t, noticeKeyFromEvidence(map[string]any{"model_failure": map[string]any{}}))
	assert.NotNil(t, noticeKeyFromEvidence(routeNoticeVerdict("d", 1)))
	assert.Equal(t, "[high] coder: s (detector=d)",
		formatEventSummary(map[string]any{"severity": "high", "member_name": "coder", "summary": "s", "detector": "d"}))
	assert.Equal(t, "[high] c: s (detector=d)",
		formatAnomalySummary(&core.Anomaly{Severity: core.SeverityHigh, MemberName: "c", Summary: "s", Detector: "d"}))
	assert.Equal(t, "7", anyOrUnknown(7))
	assert.Equal(t, "<unknown>", anyOrUnknown(nil))
	assert.Equal(t, "1", anyOrUnknown(int64(1)))
	assert.Equal(t, "3", anyOrUnknown(3.0))
	assert.Equal(t, "x", anyOrUnknown("x"))
}
