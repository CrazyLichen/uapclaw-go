package observability

import (
	"context"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

// newTestTracer 建测试用 tracer 与内存导出器。
func newTestTracer(t *testing.T) (trace.Tracer, *tracetest.InMemoryExporter) {
	t.Helper()
	exp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	return tp.Tracer("test"), exp
}

// startTestSpan 开一个带属性的测试 span。
func startTestSpan(t *testing.T, tracer trace.Tracer, attrs map[string]string) trace.Span {
	t.Helper()
	opts := make([]trace.SpanStartOption, 0, len(attrs))
	for k, v := range attrs {
		opts = append(opts, trace.WithAttributes(attribute.String(k, v)))
	}
	_, span := tracer.Start(context.Background(), "test-parent", opts...)
	return span
}

// TestEmitContextWindowCommit_compaction拒绝 compaction 请求不得推进链。
// Python: trajectory_events.py:172-177
func TestEmitContextWindowCommit_compaction拒绝(t *testing.T) {
	tracer, exp := newTestTracer(t)
	span := startTestSpan(t, tracer, map[string]string{
		GenAIConversationID:    "s1",
		OJExecutionSubjectID:   "m1",
		OJExecutionSubjectKind: "member",
	})
	AdvanceContextWindow("s1", "m1", "w1", []map[string]any{{"message_id": "a", "content": "x"}})
	got := EmitContextWindowCommit(tracer, span, []map[string]any{{"message_id": "a", "content": "x"}}, "compaction",
		map[string]string{GenAIConversationID: "s1", OJExecutionSubjectID: "m1"})
	if got != nil {
		t.Fatal("compaction 请求不得推进链")
	}
	if len(exp.GetSpans()) != 0 {
		t.Fatalf("不应产生事件 span: %d", len(exp.GetSpans()))
	}
}

// TestEmitContextWindowCommit_基线与事件属性 基线提交完整窗口并携带 v2 属性。
// Python: trajectory_events.py:163-205 + :313-352
func TestEmitContextWindowCommit_基线与事件属性(t *testing.T) {
	tracer, exp := newTestTracer(t)
	span := startTestSpan(t, tracer, map[string]string{
		GenAIConversationID:    "s2",
		OJExecutionSubjectID:   "m2",
		OJExecutionSubjectKind: "member",
		OJTurnID:               "turn-1",
	})
	msgs := []map[string]any{{"message_id": "a", "role": "user", "content": "hi"}}
	parentAttrs := map[string]string{GenAIConversationID: "s2", OJExecutionSubjectID: "m2", OJTurnID: "turn-1"}
	got := EmitContextWindowCommit(tracer, span, msgs, "assistant", parentAttrs)
	if got == nil {
		t.Fatal("应产出事件 span")
	}
	spans := exp.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("应导出 1 条事件 span，实际 %d", len(spans))
	}
	st := spans[0]
	if st.Name != "context.window.commit" {
		t.Fatalf("事件名漂移: %q", st.Name)
	}
	find := func(key string) (string, bool) {
		for _, a := range st.Attributes {
			if string(a.Key) == key {
				return a.Value.AsString(), true
			}
		}
		return "", false
	}
	if v, _ := find(OJTrajectoryRecordKind); v != "event" {
		t.Fatalf("record kind 应为 event: %q", v)
	}
	if v, _ := find(OJTrajectorySchemaVersion); v != TrajectorySpanSchemaVersion {
		t.Fatalf("schema version 漂移: %q", v)
	}
	if _, ok := find(OJTrajectorySequenceEpoch); !ok {
		t.Fatal("缺 sequence_epoch")
	}
	payload, ok := find(OJTrajectoryPayload)
	if !ok || !strings.Contains(payload, `"transition_kind":"epoch_baseline"`) {
		t.Fatalf("基线载荷应含完整窗口: %q", payload)
	}
	if !strings.Contains(payload, `"complete":true`) {
		t.Fatalf("基线 complete 应为 true: %q", payload)
	}
	// 父 span 不 recording 时不发
	ended := startTestSpan(t, tracer, nil)
	ended.End()
	if got := EmitContextWindowCommit(tracer, ended, msgs, "assistant", nil); got != nil {
		t.Fatal("父 span 已结束应返回 nil")
	}
}
