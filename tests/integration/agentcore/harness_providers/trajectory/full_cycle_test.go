//go:build integration

package trajectory

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/suite"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/agent_teams/observability"
	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/harness"
	harnessprotocol "github.com/uapclaw/uapclaw-go/internal/openjiuwen/harness_protocol"
	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/harness_providers"
)

// FullCycleSuite fake provider 全周期套件（每个 _test.go 自带 suite.Run，审计硬要求）。
type FullCycleSuite struct {
	suite.Suite
}

// TestFullCycleSuite 集成入口。
func TestFullCycleSuite(t *testing.T) {
	suite.Run(t, new(FullCycleSuite))
}

// harnessFor fake provider 事件流驱动的 recorder + 内存导出器。
type harnessFor struct {
	rec    *harness_providers.HarnessTrajectoryRecorder
	exp    *tracetest.InMemoryExporter
	tracer trace.Tracer
}

// newFakeProviderHarness 构造夹具（可观测性运行时手动接线，模拟宿主初始化后的 create 调用序）
func (s *FullCycleSuite) newFakeProviderHarness() *harnessFor {
	exp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp))
	s.T().Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	tracer := tp.Tracer(harness_providers.TracerName)
	subject := &harness.ExecutionSubject{
		SubjectID: "member-1", DisplayName: "外部成员", Kind: "member",
		SessionID: "itest-session", ParentSubjectID: "team-root",
	}
	rec, err := harness_providers.NewHarnessTrajectoryRecorder(subject, "external-cli", "agent",
		tracer, observability.DefaultObservabilityConfig(), nil)
	s.Require().NoError(err)
	return &harnessFor{rec: rec, exp: exp, tracer: tracer}
}

// env 构造信封
func env(sequence int, ts float64, event harnessprotocol.HarnessEventPayload,
	turnID, itemID string, causationIDs []string) harnessprotocol.HarnessEvent {
	return harnessprotocol.HarnessEvent{
		Sequence: sequence, Timestamp: ts, Event: event,
		HostSessionID: "itest-session", AgentID: "member-1",
		TurnID: turnID, ItemID: itemID, CausationIDs: causationIDs,
	}
}

// attrsOf 取 stub 属性字符串视图
func attrsOfStub(st tracetest.SpanStub) map[string]string {
	result := map[string]string{}
	for _, a := range st.Attributes {
		result[string(a.Key)] = a.Value.Emit()
	}
	return result
}

// TestFakeProvider全周期 宿主驱动序：record_input → STARTED → 请求（含
// input_observed 触发 context.window.commit）→ 工具（causation 归位）→ FINISHED。
// Python: trajectory.py 宿主驱动面（member_runtime/reliability/external_cli_spawn）
func (s *FullCycleSuite) TestFakeProvider全周期() {
	h := s.newFakeProviderHarness()
	rec := h.rec
	// 宿主投递输入
	rec.RecordInput("turn-1", "帮我看下目录")
	// 宿主持久化轮次身份
	rec.RecordTurnIdentity("turn-1", "traj-1", 1)
	// 轮次开始
	rec.Observe(env(1, 1.0, harnessprotocol.TurnLifecycleEvent{Kind: harnessprotocol.TurnEventKindStarted}, "turn-1", "", nil))
	// 模型请求（观测到输入 → 应推进 context.window.commit）
	req := harnessprotocol.ModelRequestEvent{
		RequestID: "req-1", Status: harnessprotocol.ModelRequestStatusCompleted,
		StartedAt: 1.0, EndedAt: 2.0, Model: "claude-sonnet",
		InputObserved: true,
		InputMessages: []harnessprotocol.TurnMessage{{
			MessageID: "im-1", Role: harnessprotocol.MessageRoleUser,
			Content: []harnessprotocol.ContentBlock{{Kind: "text", Content: "帮我看下目录"}},
		}},
		OutputMessage: &harnessprotocol.TurnMessage{
			MessageID: "om-1", Role: harnessprotocol.MessageRoleAssistant,
			Content: []harnessprotocol.ContentBlock{
				{Kind: "tool_call", Content: harnessprotocol.JsonObject{"name": "LS", "arguments": "{}"}, Data: harnessprotocol.JsonObject{"call_id": "call-1"}},
			},
		},
	}
	rec.Observe(env(2, 2.0, &req, "turn-1", "", nil))
	// 工具（causation 指向 req-1）
	rec.Observe(env(3, 2.1, harnessprotocol.ItemLifecycleEvent{
		Kind: harnessprotocol.ItemEventKindStarted, ItemType: "tool",
		Data: harnessprotocol.JsonObject{"name": "LS"},
	}, "turn-1", "item-1", []string{"req-1"}))
	rec.Observe(env(4, 2.6, harnessprotocol.ItemLifecycleEvent{
		Kind: harnessprotocol.ItemEventKindCompleted, ItemType: "tool",
		Data: harnessprotocol.JsonObject{"name": "LS", "result": "file-a\nfile-b"},
	}, "turn-1", "item-1", []string{"req-1"}))
	// 第二次请求
	req2 := harnessprotocol.ModelRequestEvent{
		RequestID: "req-2", Status: harnessprotocol.ModelRequestStatusCompleted,
		StartedAt: 3.0, EndedAt: 4.0, Model: "claude-sonnet",
		InputObserved: false,
	}
	rec.Observe(env(5, 4.0, &req2, "turn-1", "", nil))
	// 轮次完成
	rec.Observe(env(6, 5.0, harnessprotocol.TurnLifecycleEvent{
		Kind:   harnessprotocol.TurnEventKindFinished,
		Result: &harnessprotocol.TurnResult{Status: harnessprotocol.TurnStatusCompleted},
	}, "turn-1", "", nil))

	spans := h.exp.GetSpans()
	s.Require().NotEmpty(spans)
	names := map[string]int{}
	commits := 0
	for _, st := range spans {
		names[st.Name]++
		if st.Name == "context.window.commit" {
			commits++
			attrs := attrsOfStub(st)
			s.Require().Equal("event", attrs[observability.OJTrajectoryRecordKind])
			s.Require().Contains(attrs[observability.OJTrajectoryPayload], `"transition_kind":"epoch_baseline"`)
		}
	}
	// span 树骨架
	s.Require().Equal(1, names["invoke_agent external-cli"], "turn root 应 1 条")
	s.Require().Equal(2, names["chat claude-sonnet"], "inference 应 2 条")
	s.Require().Equal(1, names["execute_tool LS"], "tool 应 1 条")
	// input_observed=true 才 commit
	s.Require().Equal(1, commits, "仅观测到输入的请求推进窗口链")
	// authoritative 归位
	var toolAttrs map[string]string
	for _, st := range spans {
		if st.Name == "execute_tool LS" {
			toolAttrs = attrsOfStub(st)
		}
	}
	s.Require().Equal("true", toolAttrs[observability.OJToolAuthoritative])
	s.Require().Equal("1", toolAttrs[observability.OJStepNumber])
	s.Require().NotEmpty(toolAttrs[observability.OJInferenceID])
	// 轮次身份透传
	for _, st := range spans {
		if st.Name == "invoke_agent external-cli" {
			attrs := attrsOfStub(st)
			s.Require().Equal("traj-1", attrs[observability.OJTurnID])
			s.Require().Equal("1", attrs[observability.OJTurnNumber])
		}
	}
}

// TestRecorderClose与Restart恢复 关闭后重建 recorder，外部用户消息 id
// 从已提交窗口恢复一次（Python _remembered_external_user_ids 的重启恢复语义）。
// Python: trajectory.py:247-269
func (s *FullCycleSuite) TestRecorderClose与Restart恢复() {
	h := s.newFakeProviderHarness()
	rec := h.rec
	rec.RecordInput("t1", "用户原话")
	rec.Observe(env(1, 1.0, harnessprotocol.TurnLifecycleEvent{Kind: harnessprotocol.TurnEventKindStarted}, "t1", "", nil))
	req := harnessprotocol.ModelRequestEvent{
		RequestID: "r1", Status: harnessprotocol.ModelRequestStatusCompleted,
		StartedAt: 1.0, EndedAt: 2.0, InputObserved: true,
		InputMessages: []harnessprotocol.TurnMessage{{
			MessageID: "um-1", Role: harnessprotocol.MessageRoleUser,
			Content: []harnessprotocol.ContentBlock{{Kind: "text", Content: "用户原话"}},
		}},
	}
	rec.Observe(env(2, 2.0, &req, "t1", "", nil))
	rec.Observe(env(3, 3.0, harnessprotocol.TurnLifecycleEvent{
		Kind:   harnessprotocol.TurnEventKindFinished,
		Result: &harnessprotocol.TurnResult{Status: harnessprotocol.TurnStatusCompleted},
	}, "t1", "", nil))
	// 成员重启：新 recorder，同 subject。恢复语义经公共可观测面断言：
	// 重启后同输入同消息再次请求，context.window.commit 载荷里
	// um-1 凭恢复的记忆保持 external_user origin（不会重述为 harness 自有）
	rec2, err := harness_providers.NewHarnessTrajectoryRecorder(rec.Subject(), "external-cli", "agent",
		h.tracer, observability.DefaultObservabilityConfig(), nil)
	s.Require().NoError(err)
	expCountBefore := len(h.exp.GetSpans())
	rec2.RecordInput("t2", "用户原话")
	rec2.Observe(env(1, 4.0, harnessprotocol.TurnLifecycleEvent{Kind: harnessprotocol.TurnEventKindStarted}, "t2", "", nil))
	req2 := harnessprotocol.ModelRequestEvent{
		RequestID: "r2", Status: harnessprotocol.ModelRequestStatusCompleted,
		StartedAt: 4.0, EndedAt: 5.0, InputObserved: true,
		InputMessages: []harnessprotocol.TurnMessage{{
			MessageID: "um-1", Role: harnessprotocol.MessageRoleUser,
			Content: []harnessprotocol.ContentBlock{{Kind: "text", Content: "用户原话"}},
		}},
	}
	rec2.Observe(env(2, 5.0, &req2, "t2", "", nil))
	newSpans := h.exp.GetSpans()[expCountBefore:]
	var commitPayload string
	for _, st := range newSpans {
		if st.Name == "context.window.commit" {
			commitPayload = attrsOfStub(st)[observability.OJTrajectoryPayload]
		}
	}
	s.Require().NotEmpty(commitPayload, "重启后的观测请求应推进窗口链")
	s.Require().Contains(commitPayload, `"delta":null`,
		"恢复的记忆应使 um-1 重放无变更（未被重述为 harness 自有）: %s", commitPayload)
	// 已提交窗口确实携带这条消息与 origin
	window := observability.CurrentContextWindowMessages("itest-session", "member-1")
	s.Require().NotEmpty(window)
	var originOK bool
	for _, m := range window {
		if fmt.Sprintf("%v", m["message_id"]) == "um-1" {
			if md, _ := m["metadata"].(map[string]any); md != nil {
				originOK = fmt.Sprintf("%v", md["_openjiuwen_message_origin"]) == "external_user"
			}
		}
	}
	s.Require().True(originOK, "窗口消息应携带 external_user origin")
	s.Require().Equal(1, len(window), "重放无变更时窗口仍一条消息")
}
