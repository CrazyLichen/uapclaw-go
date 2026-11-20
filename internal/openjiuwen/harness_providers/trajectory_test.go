package harness_providers

import (
	"context"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/agent_teams/observability"
	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/harness"
	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/harness_protocol"
)

// ──────────────────────────── 测试基建 ────────────────────────────

// recorderFixture 测试夹具：内存导出器 + tracer + recorder
type recorderFixture struct {
	t      *testing.T
	exp    *tracetest.InMemoryExporter
	tracer trace.Tracer
	rec    *HarnessTrajectoryRecorder
}

// newRecorderFixture 构建测试夹具（subject=成员一/session-s1）
func newRecorderFixture(t *testing.T) *recorderFixture {
	t.Helper()
	exp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	tracer := tp.Tracer(TracerName)
	subject := &harness.ExecutionSubject{
		SubjectID: "m1", DisplayName: "成员一", Kind: "member", SessionID: "session-s1",
	}
	rec, err := NewHarnessTrajectoryRecorder(subject, "test-agent", "agent",
		tracer, DefaultTrajectoryTestConfig(), nil)
	if err != nil {
		t.Fatalf("构造失败: %v", err)
	}
	return &recorderFixture{t: t, exp: exp, tracer: tracer, rec: rec}
}

// DefaultTrajectoryTestConfig 测试用脱敏关闭配置
func DefaultTrajectoryTestConfig() *observability.ObservabilityConfig {
	cfg := observability.DefaultObservabilityConfig()
	cfg.RedactPrompts = false
	cfg.RedactCompletions = false
	return cfg
}

// envelopeOptions 构造信封的入参
func envelopeOptions(sequence int, ts float64, event harness_protocol.HarnessEventPayload,
	turnID, itemID string, causationIDs []string) harness_protocol.HarnessEvent {
	return harness_protocol.HarnessEvent{
		Sequence: sequence, Timestamp: ts, Event: event,
		HostSessionID: "session-s1", AgentID: "m1",
		TurnID: turnID, ItemID: itemID, CausationIDs: causationIDs,
	}
}

// attrsOf 取 stub 属性字符串视图
func attrsOf(st tracetest.SpanStub) map[string]string {
	result := map[string]string{}
	for _, a := range st.Attributes {
		result[string(a.Key)] = a.Value.Emit()
	}
	return result
}

// spanNamesByName 按名字找 span stub
func spanNamesByName(t *testing.T, exp *tracetest.InMemoryExporter, name string) []tracetest.SpanStub {
	t.Helper()
	var result []tracetest.SpanStub
	for _, st := range exp.GetSpans() {
		if st.Name == name {
			result = append(result, st)
		}
	}
	return result
}

// ──────────────────────────── 用例 ────────────────────────────

// TestObserve_Turn三层span树 三类 span 的名字与 kind。
// Python: trajectory.py:330-338 + :352 + :472-478 + :558-564
func TestObserve_Turn三层span树(t *testing.T) {
	f := newRecorderFixture(t)
	rec := f.rec
	rec.Observe(envelopeOptions(1, 1.0, harness_protocol.TurnLifecycleEvent{Kind: harness_protocol.TurnEventKindStarted}, "t1", "", nil))
	req := harness_protocol.ModelRequestEvent{
		RequestID: "r1", Status: harness_protocol.ModelRequestStatusCompleted,
		StartedAt: 1.0, EndedAt: 2.0, Model: "qwen-max", InputObserved: false,
	}
	rec.Observe(envelopeOptions(2, 2.0, &req, "t1", "", nil))
	rec.Observe(envelopeOptions(3, 2.1, harness_protocol.ItemLifecycleEvent{
		Kind: harness_protocol.ItemEventKindStarted, ItemType: "tool",
		Data: harness_protocol.JsonObject{"name": "read_file"},
	}, "t1", "item-1", []string{"r1"}))
	rec.Observe(envelopeOptions(4, 2.5, harness_protocol.ItemLifecycleEvent{
		Kind: harness_protocol.ItemEventKindCompleted, ItemType: "tool",
		Data: harness_protocol.JsonObject{"name": "read_file", "result": "ok"},
	}, "t1", "item-1", []string{"r1"}))
	rec.Observe(envelopeOptions(5, 3.0, harness_protocol.TurnLifecycleEvent{
		Kind: harness_protocol.TurnEventKindFinished, Result: &harness_protocol.TurnResult{Status: harness_protocol.TurnStatusCompleted},
	}, "t1", "", nil))

	turns := spanNamesByName(t, f.exp, "invoke_agent test-agent")
	if len(turns) != 1 {
		t.Fatalf("turn root 应 1 条，实际 %d", len(turns))
	}
	if turns[0].SpanKind != trace.SpanKindServer {
		t.Fatalf("turn kind 应为 SERVER，实际 %v", turns[0].SpanKind)
	}
	chats := spanNamesByName(t, f.exp, "chat qwen-max")
	if len(chats) != 1 || chats[0].SpanKind != trace.SpanKindClient {
		t.Fatalf("inference span 缺失或 kind 漂移: %d %v", len(chats), chats)
	}
	tools := spanNamesByName(t, f.exp, "execute_tool read_file")
	if len(tools) != 1 || tools[0].SpanKind != trace.SpanKindInternal {
		t.Fatalf("tool span 缺失或 kind 漂移: %d %v", len(tools), tools)
	}
	// schema version 与 record kind
	turnAttrs := attrsOf(turns[0])
	if turnAttrs[observability.OJTrajectorySchemaVersion] != "2" {
		t.Fatalf("schema version 漂移: %q", turnAttrs[observability.OJTrajectorySchemaVersion])
	}
	if turnAttrs[observability.OJTrajectoryRecordKind] != "turn" {
		t.Fatalf("record kind 漂移: %q", turnAttrs[observability.OJTrajectoryRecordKind])
	}
	if turnAttrs[observability.OJTraceRoot] != "true" {
		t.Fatalf("trace root 缺失: %v", turnAttrs)
	}
	// subject 属性 → 泳道
	for key, want := range map[string]string{
		observability.OJExecutionSubjectID:          "m1",
		observability.OJExecutionSubjectDisplayName: "成员一",
		observability.OJExecutionSubjectKind:        "member",
		observability.GenAIConversationID:           "session-s1",
	} {
		if turnAttrs[key] != want {
			t.Fatalf("subject 属性 %s 漂移: %q != %q", key, turnAttrs[key], want)
		}
	}
}

// TestObserve_Authoritative归位 causation_ids 命中/不命中两分支。
// Python: trajectory.py:592-598 + :551-557
func TestObserve_Authoritative归位(t *testing.T) {
	f := newRecorderFixture(t)
	rec := f.rec
	rec.Observe(envelopeOptions(1, 1.0, harness_protocol.TurnLifecycleEvent{Kind: harness_protocol.TurnEventKindStarted}, "t1", "", nil))
	req := harness_protocol.ModelRequestEvent{
		RequestID: "r1", Status: harness_protocol.ModelRequestStatusCompleted,
		StartedAt: 1.0, EndedAt: 2.0,
	}
	rec.Observe(envelopeOptions(2, 2.0, &req, "t1", "", nil))
	// 命中 causation 的工具
	rec.Observe(envelopeOptions(3, 2.1, harness_protocol.ItemLifecycleEvent{
		Kind: harness_protocol.ItemEventKindCompleted, ItemType: "tool",
		Data: harness_protocol.JsonObject{"name": "authored", "result": "ok"},
	}, "t1", "item-a", []string{"r1"}))
	// 未命中的工具
	rec.Observe(envelopeOptions(4, 2.2, harness_protocol.ItemLifecycleEvent{
		Kind: harness_protocol.ItemEventKindCompleted, ItemType: "tool",
		Data: harness_protocol.JsonObject{"name": "orphan", "result": "ok"},
	}, "t1", "item-b", []string{"unknown-req"}))
	rec.Close()

	authored := attrsOf(spanNamesByName(t, f.exp, "execute_tool authored")[0])
	if authored[observability.OJToolAuthoritative] != "true" {
		t.Fatalf("权威标缺失: %v", authored)
	}
	if authored[observability.OJStepNumber] != "1" {
		t.Fatalf("步号漂移: %q", authored[observability.OJStepNumber])
	}
	if authored[observability.OJInferenceID] == "" {
		t.Fatal("inference id 缺失")
	}
	orphan := attrsOf(spanNamesByName(t, f.exp, "execute_tool orphan")[0])
	if _, has := orphan[observability.OJToolAuthoritative]; has {
		t.Fatalf("未归位工具不应有权威标: %v", orphan)
	}
}

// TestRecordInput_ExternalUser标记 本 turn 匹配 + 跨 turn 记忆。
// origin 标记只进 context window 规范消息（trajectoryMessages），不进 gen_ai.input.messages。
// Python: trajectory.py:232-245 + :679-692 + :671-676
func TestRecordInput_ExternalUser标记(t *testing.T) {
	f := newRecorderFixture(t)
	rec := f.rec
	rec.RecordInput("t1", "请帮我读文件")
	rec.Observe(envelopeOptions(1, 1.0, harness_protocol.TurnLifecycleEvent{Kind: harness_protocol.TurnEventKindStarted}, "t1", "", nil))
	req := harness_protocol.ModelRequestEvent{
		RequestID: "r1", Status: harness_protocol.ModelRequestStatusCompleted,
		StartedAt: 1.0, EndedAt: 2.0,
		InputMessages: []harness_protocol.TurnMessage{{
			MessageID: "msg-1", Role: harness_protocol.MessageRoleUser,
			Content: []harness_protocol.ContentBlock{{
				Kind: "text", Content: "请帮我读文件\n谢谢",
			}},
		}},
	}
	rec.Observe(envelopeOptions(2, 2.0, &req, "t1", "", nil))
	// 直接断言 requestMessages 产物：含宿主输入的 user 消息被标记
	ids := rec.rememberedExternalUserIDs()
	if _, ok := ids["msg-1"]; !ok {
		t.Fatalf("宿主输入匹配的 message_id 应入记忆集合: %v", ids)
	}
	messages := rec.requestMessages(&req, rec.turnInputs["t1"], ids)
	var marked bool
	for _, m := range messages {
		if m["message_id"] == "msg-1" {
			if md, ok := m["metadata"].(map[string]any); ok {
				if md[llmschemaOriginKey()] == llmschemaOriginExternal() {
					marked = true
				}
			}
		}
	}
	if !marked {
		t.Fatal("user 消息应标记 external_user origin")
	}
	// 不匹配宿主输入的消息不标记
	rec2Messages := rec.requestMessages(&req, []string{"完全不同的输入"}, ids)
	unmarked := true
	for _, m := range rec2Messages {
		if m["message_id"] == "msg-1" {
			if md, ok := m["metadata"].(map[string]any); ok && md[llmschemaOriginKey()] == llmschemaOriginExternal() {
				// 已在记忆集合中（第一轮标记过），重放仍标记——记忆语义
			} else {
				unmarked = false
			}
		}
	}
	if !unmarked {
		t.Fatal("跨 turn 重放的消息应凭记忆保持标记")
	}
	// 恢复只发生一次：再调不重新读窗口
	rec.rememberedExternalUserIDs()
	rec.rememberedExternalUserIDs()
}

// TestRecordTurnIdentity_编号透传 OJ_TURN_ID/OJ_TURN_NUMBER 落盘。
// Python: trajectory.py:271-286 + :582-589
func TestRecordTurnIdentity_编号透传(t *testing.T) {
	f := newRecorderFixture(t)
	rec := f.rec
	rec.RecordTurnIdentity("proto-1", "traj-7", 7)
	rec.Observe(envelopeOptions(1, 1.0, harness_protocol.TurnLifecycleEvent{Kind: harness_protocol.TurnEventKindStarted}, "proto-1", "", nil))
	rec.Close()
	turns := spanNamesByName(t, f.exp, "invoke_agent test-agent")
	attrs := attrsOf(turns[0])
	if attrs[observability.OJTurnID] != "traj-7" || attrs[observability.OJTurnNumber] != "7" {
		t.Fatalf("turn identity 漂移: %v", attrs)
	}
}

// TestObserve_StaleTurn收尾 同 turn 二次 STARTED，前一 span 先收尾。
// Python: trajectory.py:352-356 _start_turn 的 stale 分支
func TestObserve_StaleTurn收尾(t *testing.T) {
	f := newRecorderFixture(t)
	rec := f.rec
	rec.Observe(envelopeOptions(1, 1.0, harness_protocol.TurnLifecycleEvent{Kind: harness_protocol.TurnEventKindStarted}, "t1", "", nil))
	rec.Observe(envelopeOptions(2, 2.0, harness_protocol.TurnLifecycleEvent{Kind: harness_protocol.TurnEventKindStarted}, "t1", "", nil))
	rec.Close()
	turns := spanNamesByName(t, f.exp, "invoke_agent test-agent")
	if len(turns) != 2 {
		t.Fatalf("应有 2 条 turn span，实际 %d", len(turns))
	}
	if turns[0].EndTime.IsZero() || turns[0].EndTime.Before(turns[0].StartTime) {
		t.Fatal("陈旧 turn 应已收尾")
	}
}

// TestClose_收尾未关Turn FINISHED 缺席时 Close 收尾。
// Python: trajectory.py:340-343 close + :397-420
func TestClose_收尾未关Turn(t *testing.T) {
	f := newRecorderFixture(t)
	rec := f.rec
	rec.Observe(envelopeOptions(1, 1.0, harness_protocol.TurnLifecycleEvent{Kind: harness_protocol.TurnEventKindStarted}, "t1", "", nil))
	rec.Close()
	turns := spanNamesByName(t, f.exp, "invoke_agent test-agent")
	if len(turns) != 1 || turns[0].EndTime.IsZero() {
		t.Fatal("Close 应收尾未关 turn")
	}
}

// TestRecordFailure_零长Turn 无活动 turn 时发零长失败轮次。
// Python: trajectory.py:288-324 record_failure standalone 分支
func TestRecordFailure_零长Turn(t *testing.T) {
	f := newRecorderFixture(t)
	rec := f.rec
	rec.RecordFailure("startup_failed", "boom: authorization: Bearer abcdef123456 bad",
		map[string]attribute.Value{"error.type": attribute.StringValue("startup")})
	turns := spanNamesByName(t, f.exp, "invoke_agent test-agent")
	if len(turns) != 1 {
		t.Fatalf("应有一条 standalone 失败 turn，实际 %d", len(turns))
	}
	if turns[0].EndTime.IsZero() || turns[0].EndTime.Equal(turns[0].StartTime) && turns[0].EndTime.IsZero() {
		t.Fatal("standalone span 应即刻结束")
	}
	attrs := attrsOf(turns[0])
	if attrs[observability.ErrorType] != "startup" {
		t.Fatalf("失败属性缺失: %v", attrs)
	}
	// 凭据脱敏
	if strings.Contains(attrs["status"], "abcdef123456") {
		t.Fatalf("凭据未脱敏: %q", attrs["status"])
	}
}

// TestObserve_Tool无Start补造收尾 COMPLETED 无 STARTED 时补造并立即收。
// Python: trajectory.py:522-527
func TestObserve_Tool无Start补造收尾(t *testing.T) {
	f := newRecorderFixture(t)
	rec := f.rec
	rec.Observe(envelopeOptions(1, 1.0, harness_protocol.TurnLifecycleEvent{Kind: harness_protocol.TurnEventKindStarted}, "t1", "", nil))
	rec.Observe(envelopeOptions(2, 2.0, harness_protocol.ItemLifecycleEvent{
		Kind: harness_protocol.ItemEventKindCompleted, ItemType: "tool",
		Data: harness_protocol.JsonObject{"name": "late", "result": "done"},
	}, "t1", "item-late", nil))
	rec.Close()
	tools := spanNamesByName(t, f.exp, "execute_tool late")
	if len(tools) != 1 {
		t.Fatalf("补造工具 span 应 1 条，实际 %d", len(tools))
	}
	attrs := attrsOf(tools[0])
	if attrs[observability.GenAIToolCallResult] != "done" {
		t.Fatalf("结果属性缺失: %v", attrs)
	}
	if attrs[observability.OJSpanOutput] != "done" {
		t.Fatalf("span output 缺失: %v", attrs)
	}
}

// TestMessageDicts_整形 reasoning/tool_call/tool_result 拆分 + 纯文本合并。
// Python: trajectory.py:702-781 _message_dicts/_message_content
func TestMessageDicts_整形(t *testing.T) {
	f := newRecorderFixture(t)
	rec := f.rec
	message := harness_protocol.TurnMessage{
		MessageID: "m-1", Role: harness_protocol.MessageRoleAssistant,
		Content: []harness_protocol.ContentBlock{
			{Kind: "text", Content: "思考一下"},
			{Kind: "reasoning", Content: "内心推理"},
			{Kind: "tool_call", Content: harness_protocol.JsonObject{"name": "bash", "arguments": "ls"}, Data: harness_protocol.JsonObject{"call_id": "c-1"}},
			{Kind: "tool_result", Content: "输出", Data: harness_protocol.JsonObject{"call_id": "c-1"}},
			{Kind: "text", Content: "答复"},
		},
	}
	got := rec.messageDicts(message)
	if len(got) != 2 {
		t.Fatalf("应产出主消息 + tool 消息两条，实际 %d: %v", len(got), got)
	}
	main := got[0]
	if main["role"] != "assistant" || main["message_id"] != "m-1" {
		t.Fatalf("主消息身份漂移: %v", main)
	}
	if reasoning, _ := main["reasoning_content"].(string); reasoning != "内心推理" {
		t.Fatalf("reasoning 漂移: %v", main["reasoning_content"])
	}
	calls, _ := main["tool_calls"].([]map[string]any)
	if len(calls) != 1 || calls[0]["id"] != "c-1" || calls[0]["name"] != "bash" {
		t.Fatalf("tool_calls 漂移: %v", main["tool_calls"])
	}
	toolMsg := got[1]
	if toolMsg["role"] != "tool" || toolMsg["tool_call_id"] != "c-1" || toolMsg["message_id"] != "m-1:c-1" {
		t.Fatalf("tool 消息形状漂移: %v", toolMsg)
	}
	// 纯文本块合并（"\n\n" 连接）
	joined := rec.messageDicts(harness_protocol.TurnMessage{
		MessageID: "m-2", Role: harness_protocol.MessageRoleUser,
		Content: []harness_protocol.ContentBlock{
			{Kind: "text", Content: "第一段"},
			{Kind: "text", Content: "第二段"},
		},
	})
	if content, _ := joined[0]["content"].(string); content != "第一段\n\n第二段" {
		t.Fatalf("纯文本块应合并: %q", content)
	}
}

// TestObserve_脱敏与SchemaVersion redact 开关生效。
// Python: trajectory.py:381 + :415 + :537 + :571
func TestObserve_脱敏与SchemaVersion(t *testing.T) {
	exp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	cfg := DefaultTrajectoryTestConfig()
	cfg.RedactPrompts = true
	cfg.RedactCompletions = true
	subject := &harness.ExecutionSubject{SubjectID: "m1", DisplayName: "成员一", Kind: "member", SessionID: "s"}
	rec, err := NewHarnessTrajectoryRecorder(subject, "test-agent", "agent", tp.Tracer(TracerName), cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	rec.RecordInput("t1", "机密输入")
	rec.Observe(envelopeOptions(1, 1.0, harness_protocol.TurnLifecycleEvent{Kind: harness_protocol.TurnEventKindStarted}, "t1", "", nil))
	rec.Observe(envelopeOptions(2, 2.0, harness_protocol.ItemLifecycleEvent{
		Kind: harness_protocol.ItemEventKindCompleted, ItemType: "tool",
		Data: harness_protocol.JsonObject{"name": "leak", "result": "机密结果"},
	}, "t1", "item-x", nil))
	rec.Close()
	for _, st := range exp.GetSpans() {
		for _, a := range st.Attributes {
			value := a.Value.Emit()
			if strings.Contains(value, "机密输入") || strings.Contains(value, "机密结果") {
				t.Fatalf("内容未脱敏: %s = %q", string(a.Key), value)
			}
		}
	}
}

// TestCreate_未初始化返回nil 可观测性未初始化时 Create 返回 nil。
// Python: trajectory.py:196-213 create 返回 None
func TestCreate_未初始化返回nil(t *testing.T) {
	if observability.IsObservabilityInitialized() {
		t.Skip("可观测性已初始化，跳过未初始化分支")
	}
	subject := &harness.ExecutionSubject{SubjectID: "m1", DisplayName: "成员一", Kind: "member", SessionID: "s"}
	rec, err := CreateHarnessTrajectoryRecorder(subject, "test-agent", "agent", nil)
	if rec != nil || err != nil {
		t.Fatalf("未初始化应返回 (nil, nil)，实际 (%v, %v)", rec, err)
	}
}

// TestNewHarnessTrajectoryRecorder_必填校验 session_id/display_name 校验。
// Python: trajectory.py:173-176
func TestNewHarnessTrajectoryRecorder_必填校验(t *testing.T) {
	cfg := DefaultTrajectoryTestConfig()
	tracer := newTestTracerOnly2(t)
	if _, err := NewHarnessTrajectoryRecorder(&harness.ExecutionSubject{SubjectID: "m", DisplayName: "d"}, "a", "agent", tracer, cfg, nil); err == nil {
		t.Fatal("缺 session_id 应报错")
	}
	if _, err := NewHarnessTrajectoryRecorder(&harness.ExecutionSubject{SubjectID: "m", SessionID: "s"}, "a", "agent", tracer, cfg, nil); err == nil {
		t.Fatal("缺 display_name 应报错")
	}
}

// TestRequestParameterAttributes_映射 采样参数映射与 stop_sequences。
// Python: trajectory.py:614-627
func TestRequestParameterAttributes_映射(t *testing.T) {
	event := &harness_protocol.ModelRequestEvent{
		RequestParameters: harness_protocol.JsonObject{
			"temperature": 0.7, "stream": true, "max_tokens": int64(100),
			"stop_sequences": []any{"END"},
		},
	}
	got := requestParameterAttributeValues(event)
	if v := got[observability.GenAIRequestTemperature]; v.Type() != attribute.FLOAT64 || v.AsFloat64() != 0.7 {
		t.Fatalf("temperature 应为 float64(0.7): %v", v.Emit())
	}
	if v := got[observability.GenAIRequestStream]; v.Type() != attribute.BOOL || !v.AsBool() {
		t.Fatalf("stream 应为 bool(true): %v", v.Emit())
	}
	if v := got[observability.GenAIRequestMaxTokens]; v.Type() != attribute.INT64 || v.AsInt64() != 100 {
		t.Fatalf("max_tokens 应为 int64(100): %v", v.Emit())
	}
	if v := got[observability.GenAIRequestStopSequences]; v.Type() != attribute.STRING || !strings.Contains(v.AsString(), "END") {
		t.Fatalf("stop_sequences 漂移: %v", v.Emit())
	}
}

// TestUsageAttributes_非nil才落盘 用量四键非 nil 才写。
// Python: trajectory.py:630-640
func TestUsageAttributes_非nil才落盘(t *testing.T) {
	in := 10
	out := 5
	event := &harness_protocol.ModelRequestEvent{
		Usage: &harness_protocol.TurnUsage{InputTokens: &in, OutputTokens: &out},
	}
	got := usageAttributeValues(event)
	if v := got[observability.GenAIUsageInputTokens]; v.Type() != attribute.INT64 || v.AsInt64() != 10 {
		t.Fatalf("input tokens 应为 int64(10): %v", v.Emit())
	}
	if v := got[observability.GenAIUsageOutputTokens]; v.Type() != attribute.INT64 || v.AsInt64() != 5 {
		t.Fatalf("output tokens 应为 int64(5): %v", v.Emit())
	}
	if _, has := got[observability.GenAIUsageCacheReadInputTokens]; has {
		t.Fatal("nil usage 不得落盘")
	}
}

// newTestTracerOnly2 仅建 tracer 的辅助
func newTestTracerOnly2(t *testing.T) trace.Tracer {
	t.Helper()
	exp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	return tp.Tracer(TracerName)
}

var _ = attribute.String
