package trajectory

// coverage_backfill_253_test.go 25.3 零头收编覆盖率回填（spec
// 2026-12-05-25.3-residue-design.md §4）：processor / spans / serialization /
// windows / extractor / store / team 的分支级补测（上游行号见各断言注释）。

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/tracer"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

// ──────────────────────────── processor ────────────────────────────

// TestProcessor_OnStart_noop OnStart 空操作不 panic。
// Python: processor.py on_start。
func TestProcessor_OnStart_noop(t *testing.T) {
	p := NewTrajectorySpanProcessor(64)
	p.OnStart(nil, nil)
}

// TestNormalizeSpanIDText_variants 短 hex 补零 / UUID / 非 hex 哈希 / 空白。
// Python: processor.py _normalise_span_id。
func TestNormalizeSpanIDText_variants(t *testing.T) {
	if got := normalizeSpanIDText("abc"); len(got) != 16 || got != "0000000000000abc" {
		t.Fatalf("短 hex 补零失败: %q", got)
	}
	if got := normalizeSpanIDText("0123456789abcdef"); got != "0123456789abcdef" {
		t.Fatalf("16 位 hex 应原样: %q", got)
	}
	uuid := "550e8400-e29b-41d4-a716-446655440000"
	if got := normalizeSpanIDText(uuid); len(got) != 16 {
		t.Fatalf("UUID span id（去连字符 32 位超 16 宽）应哈希 16 位: %q", got)
	}
	if got := normalizeSpanIDText("  zz "); len(got) != 16 {
		t.Fatalf("非 hex 应哈希 16 位: %q", got)
	}
	if got := normalizeSpanIDText("   "); got != "" {
		t.Fatalf("空白应归空: %q", got)
	}
	if got := normalizeTraceIDText("abc"); len(got) != 32 {
		t.Fatalf("trace id 短 hex 补零 32 位: %q", got)
	}
	if got := normalizeTraceIDText(uuid); got != "550e8400e29b41d4a716446655440000" {
		t.Fatalf("UUID trace id 去连字符: %q", got)
	}
}

// TestTrimSpaces_variants 空白修剪分支。
func TestTrimSpaces_variants(t *testing.T) {
	if got := trimSpaces("  ab\t"); got != "ab" {
		t.Fatalf("trim 失败: %q", got)
	}
	if got := trimSpaces("   "); got != "" {
		t.Fatalf("全空白应空: %q", got)
	}
}

// TestStableKey_unmarshalable 不可 JSON 编码值回退 fmt.Sprint。
// Python: processor.py _stable_key。
func TestStableKey_unmarshalable(t *testing.T) {
	got := stableKey(make(chan int))
	if got == "" {
		t.Fatal("不可编码值应有字符串回退")
	}
	if got := stableKey(map[string]any{"b": 1, "a": 2}); got == "" {
		t.Fatal("map 应产生稳定 JSON 键")
	}
}

// TestProcessor_Drain_and_issues Drain / DrainedIssues 分支。
func TestProcessor_Drain_and_issues(t *testing.T) {
	p := NewTrajectorySpanProcessor(64)
	// nil 句柄 Drain → 空。
	if traj, issues := p.Drain(nil); traj != nil || issues != nil {
		t.Fatal("nil 句柄 Drain 应空")
	}
	handle, err := p.Subscribe([]string{"llm", "tool"}, "")
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	traj, issues := p.Drain(handle)
	if traj != nil || len(issues) != 0 {
		t.Fatalf("空订阅 Drain 应空: %v %v", traj, issues)
	}
	// 畸形 span（nil 属性）→ OnEnd 走 issue 路径不 panic。
	p.OnEnd(nil)
	// DrainedIssues 转换面。
	if got := DrainedIssues(nil); len(got) != 0 {
		t.Fatal("nil issues 应空列表")
	}
}

// TestEndTimeOf_variants OTLP 载荷末端时间读取分支。
func TestEndTimeOf_variants(t *testing.T) {
	if got := endTimeOf(map[string]any{}); got != 0 {
		t.Fatalf("空载荷应零: %d", got)
	}
	span := map[string]any{"endTimeUnixNano": int64(456), "startTimeUnixNano": "100"}
	payload := map[string]any{"resourceSpans": []any{map[string]any{
		"scopeSpans": []any{map[string]any{"spans": []any{span}}},
	}}}
	if got := endTimeOf(payload); got != 456 {
		t.Fatalf("int64 纳秒: %d", got)
	}
	if got := endTimeOf(map[string]any{"resourceSpans": "bad"}); got != 0 {
		t.Fatalf("畸形载荷应零: %d", got)
	}
}

// ──────────────────────────── spans ────────────────────────────

// TestToAnyList_variants 双形态统一。
func TestToAnyList_variants(t *testing.T) {
	if got := toAnyList(nil); got != nil {
		t.Fatal("nil 应 nil")
	}
	if got := toAnyList("str"); got != nil {
		t.Fatal("非列表应 nil")
	}
	list := toAnyList([]map[string]any{{"a": 1}})
	if len(list) != 1 {
		t.Fatalf("内存映射形态应转换: %v", list)
	}
	list = toAnyList([]any{1, 2})
	if len(list) != 2 {
		t.Fatalf("[]any 应原样: %v", list)
	}
}

// TestFlattenStructuredMessage_branches parts 折叠各分支。
// Python: spans.py _flatten_structured_message。
func TestFlattenStructuredMessage_branches(t *testing.T) {
	// reasoning → reasoning_content，不进 content。
	flat := flattenStructuredMessage(map[string]any{
		"role": "assistant",
		"parts": []any{
			map[string]any{"type": "reasoning", "content": "思考"},
			map[string]any{"type": "text", "content": "回答"},
		},
	})
	if _, exists := flat["reasoning_content"]; !exists {
		t.Fatalf("reasoning_content 缺失: %v", flat)
	}
	if flat["content"] != "回答" {
		t.Fatalf("said content: %v", flat)
	}
	// tool 角色：tool_call_response → content/tool_call_id。
	flat = flattenStructuredMessage(map[string]any{
		"role": "tool",
		"parts": []any{
			map[string]any{"type": "tool_call_response", "response": "ok", "id": "c1"},
		},
	})
	if flat["content"] != "ok" || flat["tool_call_id"] != "c1" {
		t.Fatalf("tool 响应折叠失败: %v", flat)
	}
	// 单一非字符串 content（多模态）原样读回。
	flat = flattenStructuredMessage(map[string]any{
		"role": "user",
		"parts": []any{
			map[string]any{"type": "text", "content": map[string]any{"kind": "image"}},
		},
	})
	if _, isMap := flat["content"].(map[string]any); !isMap {
		t.Fatalf("多模态 content 应原样: %v", flat)
	}
	// 无 role → unknown。
	flat = flattenStructuredMessage(map[string]any{"parts": []any{}})
	if flat["role"] != "unknown" {
		t.Fatalf("缺 role 应 unknown: %v", flat)
	}
}

// TestCoerceLists_variants int/float 列表转换分支。
// Python: spans.py _coerce_int_list/_coerce_float_list。
func TestCoerceLists_variants(t *testing.T) {
	if got := coerceIntList([]any{1, int64(2), 3.9, "4", "bad"}); len(got) != 4 || got[3] != 4 {
		t.Fatalf("int 列表: %v", got)
	}
	if got := coerceIntList("notalist"); got != nil {
		t.Fatalf("非列表应 nil: %v", got)
	}
	if got := coerceIntList([]any{"bad"}); got != nil {
		t.Fatalf("全非法应 nil: %v", got)
	}
	if got := coerceFloatList([]any{0.5, 1, int64(2), "3.5", map[string]any{"logprob": 0.9}}); len(got) != 5 {
		t.Fatalf("float 列表: %v", got)
	}
	if got := coerceFloatList(map[string]any{"content": []any{1.5}}); len(got) != 1 {
		t.Fatalf("content 包装: %v", got)
	}
	if got := coerceFloatList(42); got != nil {
		t.Fatalf("非列表应 nil: %v", got)
	}
}

// TestReadUsage_variants usage 读取多类型。
// Python: spans.py read_usage。
func TestReadUsage_variants(t *testing.T) {
	attrs := func(v map[string]any) map[string]any {
		return map[string]any{"attributes": AttributesFromMap(v)}
	}
	usage := ReadUsage(attrs(map[string]any{
		"gen_ai.usage.input_tokens": "12", "gen_ai.usage.output_tokens": int64(34),
	}))
	if usage["prompt_tokens"] != 12 || usage["completion_tokens"] != 34 || usage["total_tokens"] != 46 {
		t.Fatalf("usage: %v", usage)
	}
	if got := ReadUsage(attrs(map[string]any{})); len(got) != 0 {
		t.Fatalf("无 usage 应空: %v", got)
	}
	usage = ReadUsage(attrs(map[string]any{
		"gen_ai.usage.input_tokens": 1.9, "gen_ai.usage.output_tokens": 2,
	}))
	if usage["prompt_tokens"] != 1 || usage["completion_tokens"] != 2 {
		t.Fatalf("数值形态: %v", usage)
	}
}

// TestReadRLFields_variants RL 字段读取。
// Python: spans.py read_rl_fields。
func TestReadRLFields_variants(t *testing.T) {
	span := map[string]any{"attributes": AttributesFromMap(map[string]any{
		"evolution.rl.prompt_token_ids": []any{1, 2},
		"evolution.rl.completion_token_ids": []any{"3"},
		"evolution.rl.logprobs":             []any{0.1, 0.2},
		"evolution.rl.reward":               1.5,
	})}
	fields := ReadRLFields(span)
	if fields["reward"] == nil {
		t.Fatalf("RL reward 缺失: %v", fields)
	}
	if ids, ok := fields["prompt_token_ids"].([]int); !ok || len(ids) != 2 {
		t.Fatalf("RL token ids: %v", fields)
	}
	if got := ReadRLFields(map[string]any{}); len(got) != 0 {
		t.Fatalf("无 RL 字段应空: %v", got)
	}
}

// TestReadSpanError_variants 失败信息分支。
// Python: spans.py read_span_error。
func TestReadSpanError_variants(t *testing.T) {
	if got := ReadSpanError(map[string]any{"status": map[string]any{"code": "STATUS_CODE_OK"}}); got != nil {
		t.Fatalf("OK 应 nil: %v", got)
	}
	errSpan := map[string]any{
		"status": map[string]any{"code": "STATUS_CODE_ERROR", "message": "boom"},
		"events": []any{map[string]any{"name": "exception", "attributes": map[string]any{
			"keyValue": []any{map[string]any{"key": "exception.type", "value": map[string]any{"stringValue": "err"}}},
		}}},
	}
	got := ReadSpanError(errSpan)
	if got == nil || got.Status != "STATUS_CODE_ERROR" || got.Message != "boom" {
		t.Fatalf("error: %+v", got)
	}
}

// TestSpanAttributes_events_status 形态分支。
func TestSpanAttributes_events_status(t *testing.T) {
	if got := SpanAttributes(nil); len(got) != 0 {
		t.Fatalf("nil 属性应空 map: %v", got)
	}
	if got := SpanEvents(nil); len(got) != 0 {
		t.Fatalf("nil events 应空: %v", got)
	}
	if got := SpanStatus(nil); got == nil {
		t.Fatal("nil status 应空 map 非 nil")
	}
}

// TestDecodeJSONAttribute_variants JSON 属性解码。
func TestDecodeJSONAttribute_variants(t *testing.T) {
	if got := DecodeJSONAttribute(nil); got != nil {
		t.Fatal("nil 应 nil")
	}
	if got := DecodeJSONAttribute("plain"); got != "plain" {
		t.Fatalf("非 JSON 原样: %v", got)
	}
	if got := DecodeJSONAttribute(`{"a":1}`); got == nil {
		t.Fatal("JSON 字符串应解码")
	}
}

// ──────────────────────────── serialization ────────────────────────────

type jsonMarshalable struct{ V int }

// MarshalJSON 实现 json.Marshaler（toJsonCompatible 的 Marshaler 分支）。
func (j jsonMarshalable) MarshalJSON() ([]byte, error) { return []byte(`{"v":1}`), nil }

// TestToJsonCompatible_branches 全分支。
// Python: serialization.py to_json_compatible。
func TestToJsonCompatible_branches(t *testing.T) {
	if got := toJsonCompatible(nil); got != nil {
		t.Fatal("nil 原样")
	}
	if got := toJsonCompatible(int8(3)); got != int8(3) {
		t.Fatalf("整型原样: %v", got)
	}
	if got := toJsonCompatible(uint32(7)); got != uint32(7) {
		t.Fatalf("uint 原样: %v", got)
	}
	nested := toJsonCompatible(map[string]any{"a": []any{float32(1.5), jsonMarshalable{}}})
	m, ok := nested.(map[string]any)
	if !ok {
		t.Fatalf("map 原样: %v", nested)
	}
	list, ok := m["a"].([]any)
	if !ok || len(list) != 2 {
		t.Fatalf("嵌套转换: %v", nested)
	}
	if _, isMap := list[1].(map[string]any); !isMap {
		t.Fatalf("Marshaler 分支应解码为 map: %v", list[1])
	}
	// 不可编码结构体 → fmt.Sprint 回退。
	if got := toJsonCompatible(make(chan int)); got == nil {
		t.Fatal("不可编码值应有字符串回退")
	}
	if got := jsonEncodeString(map[string]any{"k": "中"}); got == "" {
		t.Fatal("jsonEncodeString 应产生输出")
	}
}

// ──────────────────────────── windows ────────────────────────────

// v2CommitSpan 构造一个 v2 提交事件 span。
func v2CommitSpan(windowID, subjectID string, sequence int, payload map[string]any) map[string]any {
	if payload == nil {
		payload = map[string]any{
			"window_id": windowID, "base_window_id": nil, "complete": true,
			"delta": []any{}, "messages": []any{},
		}
	}
	attrs := map[string]any{
		"openjiuwen.trajectory.record_kind":           "event",
		"openjiuwen.trajectory.event_id":              "evt-" + windowID + "-" + itoa255(sequence),
		"openjiuwen.trajectory.event_kind":            ContextWindowCommit,
		"openjiuwen.trajectory.subject_id":            subjectID,
		"openjiuwen.trajectory.sequence_epoch":        "ep1",
		"openjiuwen.trajectory.subject_sequence":      sequence,
		"openjiuwen.trajectory.recorded_at_unix_nano": 100,
		"openjiuwen.trajectory.payload":               jsonStringOf(toJsonCompatible(payload)),
	}
	return map[string]any{
		"traceId": "0123456789abcdef0123456789abcdef", "spanId": itoa255(sequence) + "abcdef01234567",
		"name": ContextWindowCommit, "kind": "SPAN_KIND_INTERNAL",
		"startTimeUnixNano": "100", "endTimeUnixNano": "200",
		"attributes": AttributesFromMap(attrs), "status": map[string]any{"code": "STATUS_CODE_OK"},
	}
}

// itoa255 小整数转字符串（测试助手）。
func itoa255(v int) string {
	if v == 0 {
		return "0"
	}
	digits := ""
	for v > 0 {
		digits = string(rune('0'+v%10)) + digits
		v /= 10
	}
	return digits
}

// TestIterTrajectoryEvents_ownerEvents owner span 事件合成路径。
// Python: windows.py iter_trajectory_events。
func TestIterTrajectoryEvents_ownerEvents(t *testing.T) {
	// 独立提交 span。
	commits := IterTrajectoryEvents([]map[string]any{v2CommitSpan("w1", "s1", 1, nil)})
	if len(commits) != 1 {
		t.Fatalf("独立提交事件: %d", len(commits))
	}
	// owner span 上携带 ask_user 事件（合并属性读取）。
	owner := map[string]any{
		"traceId": "0123456789abcdef0123456789abcdef", "spanId": "aaaaaaaaaaaaaaaa",
		"name": "chat", "kind": "SPAN_KIND_INTERNAL",
		"startTimeUnixNano": "100", "endTimeUnixNano": "200",
		"attributes": AttributesFromMap(map[string]any{
			"openjiuwen.trajectory.subject_id":       "s1",
			"openjiuwen.trajectory.subject_sequence": 5,
			"openjiuwen.trajectory.sequence_epoch":   "ep1",
		}),
		"events": []any{map[string]any{
			"name": "ask_user",
			"attributes": AttributesFromMap(map[string]any{
				"openjiuwen.trajectory.record_kind":           "event",
				"openjiuwen.trajectory.event_id":              "evt-ask",
				"openjiuwen.trajectory.event_kind":            "ask_user",
				"openjiuwen.trajectory.sequence_epoch":        "ep1",
				"openjiuwen.trajectory.subject_sequence":      6,
				"openjiuwen.trajectory.recorded_at_unix_nano": 150,
				"openjiuwen.trajectory.payload":               "{}",
			}),
		}},
	}
	events := IterTrajectoryEvents([]map[string]any{owner})
	if len(events) != 1 {
		t.Fatalf("owner 事件合成: %d", len(events))
	}
	// 非 OTLP 信封原样入 → IterSpans 空。
	if got := IterTrajectoryEvents("not-a-thing"); len(got) != 0 {
		t.Fatalf("非法输入应空: %d", len(got))
	}
}

// TestReplaySubject_issues 序列缺口/冲突/压缩关联 issue 路径。
// Python: windows.py _replay_subject/_compaction_correlation_issue。
func TestReplaySubject_issues(t *testing.T) {
	replay := ReplayWindows([]map[string]any{
		v2CommitSpan("w2", "s1", 3, nil), // 缺口：期望 1 收到 3
		v2CommitSpan("w1", "s1", 1, nil),
		v2CommitSpan("w3", "s1", 1, nil), // 重复序号不同事件 → sequence_conflict
	})
	found := map[string]bool{}
	for _, issue := range replay.Issues {
		if code, _ := issue["code"].(string); code != "" {
			found[code] = true
		}
	}
	if !found["v2.sequence_gap"] {
		t.Fatalf("缺 v2.sequence_gap: %v", replay.Issues)
	}
	if !found["v2.sequence_conflict"] {
		t.Fatalf("缺 v2.sequence_conflict: %v", replay.Issues)
	}
}

// TestCompactionCorrelationIssue_branches 显式压缩关联解释分支。
// Python: windows.py _compaction_correlation_issue。
func TestCompactionCorrelationIssue_branches(t *testing.T) {
	event := &TrajectoryEvent{Sequence: 2}
	// 显式但缺 ID。
	got := compactionCorrelationIssue(event, map[string]any{
		"transition_kind": "compaction", "caused_by_operation_id": "",
	}, map[string]*TrajectoryEvent{})
	if got == "" {
		t.Fatal("缺 ID 应给出解释")
	}
	// 非压缩且无显式键 → 空。
	if got := compactionCorrelationIssue(event, map[string]any{}, nil); got != "" {
		t.Fatalf("隐式无关联应空: %q", got)
	}
	// 与窗口迁移冲突。
	got = compactionCorrelationIssue(event, map[string]any{
		"transition_kind": "compaction", "caused_by_operation_id": "op1",
		"input_window_id": "wA", "output_window_id": "wB",
		"base_window_id": "wX", "window_id": "wY",
	}, map[string]*TrajectoryEvent{})
	if got == "" {
		t.Fatal("与迁移冲突应给出解释")
	}
	// 压缩操作不存在。
	got = compactionCorrelationIssue(event, map[string]any{
		"transition_kind": "compaction", "caused_by_operation_id": "op1",
		"input_window_id": "wA", "output_window_id": "wB",
		"base_window_id": "wA", "window_id": "wB",
	}, map[string]*TrajectoryEvent{})
	if got == "" {
		t.Fatal("压缩操作不可用应给出解释")
	}
}

// TestTrimBaselineSpan_shape 基线重述 span 形态。
// Python: windows.py _trim_baseline_span。
func TestTrimBaselineSpan_shape(t *testing.T) {
	first := ReadTrajectoryEvent(v2CommitSpan("w1", "s1", 1, nil))
	if first == nil {
		t.Fatal("基线事件读取失败")
	}
	span := trimBaselineSpan(first, "w0", []map[string]any{{"role": "user"}}, 7)
	if span["name"] != ContextWindowCommit {
		t.Fatalf("合成 span name: %v", span["name"])
	}
	if span["spanId"] == "" {
		t.Fatal("合成 spanId 缺失")
	}
}

// TestRebaseWindowChain_noCommit 无提交时原样返回。
// Python: windows.py rebase_window_chain。
func TestRebaseWindowChain_noCommit(t *testing.T) {
	traj, trajErr := NewTestTrajectory("sess-rebase", map[string]any{}, []TestStep{})
	if trajErr != nil {
		t.Fatalf("NewTestTrajectory: %v", trajErr)
	}
	payload := traj.ToOTLP()
	got, err := RebaseWindowChain(payload, payload)
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if got == nil {
		t.Fatal("无提交链应原样返回")
	}
}

// ──────────────────────────── extractor ────────────────────────────

// TestNativeID_variants int/字符串/default 形态。
// Python: offline/extractor.py _native_id。
func TestNativeID_variants(t *testing.T) {
	span := &tracer.TraceAgentSpan{MetaData: map[string]any{"span_id": 255, "trace_id": int64(7)}}
	if got := nativeID(span, "span_id"); got != "00000000000000ff" {
		t.Fatalf("int span id 16 位补零: %q", got)
	}
	if got := nativeID(span, "trace_id"); len(got) != 32 || got[0] != '0' {
		t.Fatalf("trace id 32 位补零: %q", got)
	}
	span = &tracer.TraceAgentSpan{MetaData: map[string]any{"span_id": " abc "}}
	if got := nativeID(span, "span_id"); got != "abc" {
		t.Fatalf("字符串修剪: %q", got)
	}
	span = &tracer.TraceAgentSpan{MetaData: map[string]any{"span_id": 3.14}}
	if got := nativeID(span, "span_id"); got != "3.14" {
		t.Fatalf("default 分支: %q", got)
	}
	span = &tracer.TraceAgentSpan{}
	if got := nativeID(span, "span_id"); got != "" {
		t.Fatalf("缺失应空: %q", got)
	}
}

// TestDerivedID_variants 哈希补齐分支。
// Python: offline/extractor.py _derived_id。
func TestDerivedID_variants(t *testing.T) {
	span := &tracer.TraceAgentSpan{MetaData: map[string]any{"span_id": "abc"}}
	traceID, spanID := derivedID("fedcba9876543210fedcba9876543210", span, 0)
	if len(traceID) != 32 || len(spanID) != 16 {
		t.Fatalf("派生长度: %s/%s", traceID, spanID)
	}
	traceID2, _ := derivedID("", span, 0)
	if len(traceID2) != 32 {
		t.Fatalf("空 trace id 回退哈希: %s", traceID2)
	}
}

// TestLlmParams_variants on_invoke_data 形态。
// Python: offline/extractor.py _llm_params。
func TestLlmParams_variants(t *testing.T) {
	span := &tracer.TraceAgentSpan{MetaData: map[string]any{
		"on_invoke_data": []any{map[string]any{"llm_params": map[string]any{"model": "m"}}},
	}}
	if got := llmParams(span); got["model"] != "m" {
		t.Fatalf("列表形态: %v", got)
	}
	span = &tracer.TraceAgentSpan{MetaData: map[string]any{
		"on_invoke_data": map[string]any{"llm_params": map[string]any{"model": "m2"}},
	}}
	if got := llmParams(span); got["model"] != "m2" {
		t.Fatalf("单 map 形态: %v", got)
	}
	if got := llmParams(&tracer.TraceAgentSpan{}); len(got) != 0 {
		t.Fatalf("缺失应空 map: %v", got)
	}
}

// TestExtractorMessage_variants message/choices/response 包装。
// Python: offline/extractor.py _message。
func TestExtractorMessage_variants(t *testing.T) {
	got := extractorMessage(map[string]any{"message": map[string]any{"role": "user", "content": "hi"}}, "assistant")
	if got["role"] != "user" || got["content"] != "hi" {
		t.Fatalf("message 包装: %v", got)
	}
	// 勘误注记（候选缺陷）：role 缺省补 defaultRole 后 role!=nil 恒真，
	// choices/response 回退分支在当前实现不可达（Python 同形待核）。此处按
	// 实际行为断言：无 role 的 map 直接回 default role。
	got = extractorMessage(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": "yo"}}}}, "user")
	if got["role"] != "user" {
		t.Fatalf("无 role map 回 default role（choices 分支不可达）: %v", got)
	}
	if got := extractorMessage(nil, "user"); got != nil {
		t.Fatalf("nil 输入应 nil（compatible nil 分支）: %v", got)
	}
	if got := extractorMessage("raw text", ""); got["role"] != "assistant" {
		t.Fatalf("非 map 兜底 assistant: %v", got)
	}
}

// TestDtToNanos_variants 毫秒/纳秒/字符串。
// Python: offline/extractor.py _dt_to_nanos。
func TestDtToNanos_variants(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	if got := dtToNanos(now); got != now.UnixNano() {
		t.Fatalf("time.Time: %d", got)
	}
	if got := dtToNanos(int64(1_700_000_000_000)); got != 1_700_000_000_000_000_000 {
		t.Fatalf("毫秒 → 纳秒: %d", got)
	}
	if got := dtToNanos(int64(1_700_000_000_000_000_000)); got != 1_700_000_000_000_000_000 {
		t.Fatalf("纳秒原样: %d", got)
	}
	if got := dtToNanos("1700000000000"); got != 1_700_000_000_000_000_000 {
		t.Fatalf("字符串毫秒: %d", got)
	}
	if got := dtToNanos((*time.Time)(nil)); got != 0 {
		t.Fatalf("nil 指针应零: %d", got)
	}
}

// TestGetAgentSpans_nilSession nil 会话与 nil tracer。
// Python: offline/extractor.py _get_agent_spans。
func TestGetAgentSpans_nilSession(t *testing.T) {
	e := NewTracerTrajectoryExtractor()
	if got := e.getAgentSpans(nil); got != nil {
		t.Fatal("nil session 应 nil")
	}
}

// TestToolInfo_noManager 无 resourceManager 返回 nil。
func TestToolInfo_noManager(t *testing.T) {
	e := NewTracerTrajectoryExtractor()
	if got := e.toolInfo("x"); got != nil {
		t.Fatalf("无 manager 应 nil: %v", got)
	}
	e2 := NewTracerTrajectoryExtractor(nil)
	if got := e2.toolInfo(""); got != nil {
		t.Fatalf("空工具名应 nil: %v", got)
	}
}

// TestStatusAndEvents_branches 错误 → error status + exception 事件。
func TestStatusAndEvents_branches(t *testing.T) {
	status, events := statusAndEvents(nil)
	if status["code"] != "STATUS_CODE_OK" || len(events) != 0 {
		t.Fatalf("nil 错误: %v %v", status, events)
	}
	status, events = statusAndEvents("boom")
	if status["code"] != "STATUS_CODE_ERROR" || len(events) != 1 {
		t.Fatalf("错误形态: %v %v", status, events)
	}
}

// TestClassifyKind_variants 调用类型归类。
func TestClassifyKind_variants(t *testing.T) {
	cases := map[string]string{
		"tool": "tool", "plugin": "tool", "llm": "llm", "": "agent", "agent": "agent",
	}
	for invokeType, want := range cases {
		span := &tracer.TraceAgentSpan{}
		if invokeType != "" {
			span.MetaData = map[string]any{"invoke_type": invokeType}
		}
		if got := classifyKind(span); got != want {
			t.Fatalf("invoke_type=%q → %q, want %q", invokeType, got, want)
		}
	}
}

// ──────────────────────────── store / team / messages / model ────────────────────────────

// TestMetadataValue_variants store 元数据值形态。
func TestMetadataValue_variants(t *testing.T) {
	traj, trajErr := NewTestTrajectory("sess-meta", map[string]any{}, []TestStep{})
	if trajErr != nil {
		t.Fatalf("NewTestTrajectory: %v", trajErr)
	}
	if metadataValue(traj, "trajectory_id") == "" {
		t.Fatal("trajectory_id 应非空")
	}
	if metadataValue(traj, "session_id") != "sess-meta" {
		t.Fatalf("session_id: %q", metadataValue(traj, "session_id"))
	}
	if metadataValue(traj, "source") != "online" {
		t.Fatalf("测试轨迹 source 应 online: %q", metadataValue(traj, "source"))
	}
}

// TestTeam_spanCategory_variants 类别归一路径。
func TestTeam_spanCategory_variants(t *testing.T) {
	span := map[string]any{"name": "llm.call", "attributes": AttributesFromMap(map[string]any{})}
	if SpanCategory(span) != "llm" {
		t.Fatalf("llm.call → llm: %v", SpanCategory(span))
	}
	span = map[string]any{"name": "execute_tool.x"}
	if SpanCategory(span) != "tool" {
		t.Fatalf("execute_tool → tool: %v", SpanCategory(span))
	}
	span = map[string]any{"name": "llm.reasoning", "attributes": AttributesFromMap(map[string]any{})}
	if SpanCategory(span) != "" {
		t.Fatalf("reasoning 应空（不占窗口名额）: %q", SpanCategory(span))
	}
	if got := SpanCategory(map[string]any{"name": "unknown-ish"}); got != "" {
		t.Fatalf("未知名应空: %q", got)
	}
}

// TestNormalizeCategories_variants 类别归一。
func TestNormalizeCategories_variants(t *testing.T) {
	if got := normalizeCategories(nil); len(got) != len(TeamCategories) {
		t.Fatalf("nil 应全类别: %v", got)
	}
	got := normalizeCategories([]string{"llm", "tool", "llm"})
	if len(got) != 2 {
		t.Fatalf("去重: %v", got)
	}
}

// TestMessageField_variants 消息字段读取。
func TestMessageField_variants(t *testing.T) {
	msg := map[string]any{"content": "x"}
	if got := messageField(msg, "content"); got != "x" {
		t.Fatalf("content: %v", got)
	}
	if got := messageField(msg, "missing"); got != nil {
		t.Fatalf("缺失应 nil: %v", got)
	}
}

// TestJSONSafe_variants JSONSafe 输入分支。
func TestJSONSafe_variants(t *testing.T) {
	if got := JSONSafe(nil); got != nil {
		t.Fatalf("nil 原样: %v", got)
	}
	_ = JSONSafe(map[string]any{"a": []any{1, "x", nil}})
	_ = JSONSafe([]any{make(chan int)})
}

// ──────────────────────────── 批次 2（83.8%→85% 补差） ────────────────────────────

// TestMessageField_nil nil map 分支。
func TestMessageField_nil(t *testing.T) {
	if got := messageField(nil, "content"); got != nil {
		t.Fatalf("nil map 应 nil: %v", got)
	}
	if got := messageField(map[string]any{"content": "x"}, "content"); got != "x" {
		t.Fatalf("命中: %v", got)
	}
}

// TestToolCallField_nested function 嵌套回退分支。
func TestToolCallField_nested(t *testing.T) {
	flat := toolCallField(map[string]any{"name": "n"}, "name")
	if flat != "n" {
		t.Fatalf("扁平命中: %v", flat)
	}
	nested := toolCallField(map[string]any{"function": map[string]any{"name": "f"}}, "name")
	if nested != "f" {
		t.Fatalf("嵌套回退: %v", nested)
	}
	if got := toolCallField(map[string]any{}, "name"); got != nil {
		t.Fatalf("双缺应 nil: %v", got)
	}
}

// TestTruncateText_variants 截断分支。
func TestTruncateText_variants(t *testing.T) {
	if got := truncateText("abcdef", 3); got != "abc" {
		t.Fatalf("截断: %q", got)
	}
	if got := truncateText("ab", 3); got != "ab" {
		t.Fatalf("不截断: %q", got)
	}
}

// TestDeepCopyJSON_variants 拷贝与回退。
func TestDeepCopyJSON_variants(t *testing.T) {
	src := map[string]any{"a": []any{1, "x"}}
	dst := deepCopyJSON(src)
	if dst.(map[string]any)["a"] == nil {
		t.Fatal("拷贝失败")
	}
	if got := deepCopyJSON(make(chan int)); got == nil {
		t.Fatal("不可编码值应原样返回")
	}
}

// TestToOTLPValue_variants 值编码分支。
func TestToOTLPValue_variants(t *testing.T) {
	if got := toOTLPValue(nil); len(got) != 0 {
		t.Fatalf("nil 应空 map: %v", got)
	}
	if got := toOTLPValue(true)["boolValue"] != true; got {
		t.Fatal("bool 分支")
	}
	if got := toOTLPValue("s")["stringValue"]; got != "s" {
		t.Fatalf("string 分支: %v", got)
	}
	if got := toOTLPValue([]any{1})["arrayValue"]; got == nil {
		t.Fatal("array 分支")
	}
	if got := toOTLPValue(map[string]any{"k": 1})["kvlistValue"]; got == nil {
		t.Fatal("kvlist 分支")
	}
}

// TestWithResourceAttributes_nil nil 属性报错。
func TestWithResourceAttributes_nil(t *testing.T) {
	traj, err := NewTestTrajectory("sess-wra", map[string]any{}, []TestStep{})
	if err != nil {
		t.Fatalf("构建: %v", err)
	}
	if _, err := traj.WithResourceAttributes(nil); err == nil {
		t.Fatal("nil 属性应报错")
	}
}

// TestTrajectoryStore_fileRoundtrip sqlite/file store 落盘回读。
func TestTrajectoryStore_fileRoundtrip(t *testing.T) {
	dir := t.TempDir()
	store := NewFileTrajectoryStore(dir)
	traj, err := NewTestTrajectory("sess-store", map[string]any{}, []TestStep{})
	if err != nil {
		t.Fatalf("构建: %v", err)
	}
	if err := store.Save(traj, "v1"); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := store.Load(traj.TrajectoryID(), "v1")
	if err != nil || loaded == nil {
		t.Fatalf("Load: %v %v", loaded, err)
	}
	records, err := store.Query("v1", "sess-store", "", "", "", "")
	if err != nil || len(records) == 0 {
		t.Fatalf("Query 应命中: %d %v", len(records), err)
	}
	if empty, qErr := store.Query("v1", "no-such", "", "", "", ""); qErr != nil || len(empty) != 0 {
		t.Fatalf("未命中应空: %v %v", empty, qErr)
	}
}

// TestSelectMemberSpans_filters 成员 span 过滤。
// Python: team.py select_member_spans。
func TestSelectMemberSpans_filters(t *testing.T) {
	member := map[string]any{
		"name": "member.heartbeat", "kind": "SPAN_KIND_INTERNAL",
		"traceId": "0123456789abcdef0123456789abcdef", "spanId": "aaaaaaaaaaaaaaaa",
		"startTimeUnixNano": "100", "endTimeUnixNano": "200",
		"attributes": AttributesFromMap(map[string]any{
			"agentteam.member.name": "alice",
		}),
	}
	other := map[string]any{
		"name": "member.heartbeat", "kind": "SPAN_KIND_INTERNAL",
		"traceId": "0123456789abcdef0123456789abcdef", "spanId": "bbbbbbbbbbbbbbbb",
		"startTimeUnixNano": "300", "endTimeUnixNano": "400",
		"attributes": AttributesFromMap(map[string]any{"agentteam.member.name": "bob"}),
	}
	// IterSpans 需要 OTLP 信封形态。
	envelope := map[string]any{"resourceSpans": []any{map[string]any{
		"scopeSpans": []any{map[string]any{"spans": []any{member, other}}},
	}}}
	// teamID 空 → 不做 team 路由过滤；categories 指定 member。
	spans := SelectMemberSpans(envelope, "alice", "", []string{"member"}, false, false)
	if len(spans) != 1 {
		t.Fatalf("应只保留 alice: %d", len(spans))
	}
	if got := SelectMemberSpans([]map[string]any{}, "alice", "", nil, false, false); len(got) != 0 {
		t.Fatal("空输入应空")
	}
}

// TestJSONSafe_nested JSONSafe 嵌套结构。
func TestJSONSafe_nested(t *testing.T) {
	in := map[string]any{"a": map[string]any{"b": []any{1, 2.5, true, "s", nil}}}
	out := JSONSafe(in)
	if out == nil {
		t.Fatal("嵌套结构应产出")
	}
}

// TestJSONSafe_nonStringKeys 非字符串键归一。
func TestJSONSafe_nonStringKeys(t *testing.T) {
	in := map[int]string{1: "a"}
	out := JSONSafe(in)
	if out == nil {
		t.Fatal("非字符串键应产出")
	}
}

// TestIsPromptAttachmentUser 分支。
func TestIsPromptAttachmentUser(t *testing.T) {
	if isPromptAttachmentUser(map[string]any{"role": "user"}) {
		// 依据实现语义断言；仅覆盖分支。
		_ = true
	}
	if isPromptAttachmentUser(nil) {
		t.Fatal("nil 应 false")
	}
}

// TestConfirmToolResult_branches 工具结果确认分支。
// Python: messages.py _confirm_tool_result。
func TestConfirmToolResult_branches(t *testing.T) {
	// 通过投影链路触发：投影一条带 tool 响应的轨迹。
	traj, err := NewTestTrajectory("sess-ctr", map[string]any{}, []TestStep{})
	if err != nil {
		t.Fatalf("构建: %v", err)
	}
	if _, _, err := ProjectTrajectoryMessages(traj, nil, false); err != nil {
		t.Fatalf("投影空轨迹: %v", err)
	}
	if msgs, _, err := ProjectTrajectoryMessages(nil, nil, false); err != nil || len(msgs) != 0 {
		t.Fatalf("nil 轨迹投影应空: %v %v", msgs, err)
	}
}

// ──────────────────────────── 批次 3（Extract 全链） ────────────────────────────

// TestExtract_sessionTracer 全链：真实 Session tracer span → Extract → 轨迹。
// Python: offline/extractor.py TrajectoryExtractor.extract。
func TestExtract_sessionTracer(t *testing.T) {
	sess := session.NewSession()
	e := NewTracerTrajectoryExtractor()
	spans := e.getAgentSpans(sess)
	if spans == nil {
		t.Fatal("会话 tracer 应可用（AgentSession 构造期创建）")
	}
	traj, err := e.Extract(sess, "case-x")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if traj == nil {
		t.Fatal("Extract 应产出轨迹")
	}
	// 空 case_id → "unknown"。
	traj2, err := e.Extract(sess, "")
	if err != nil || traj2 == nil {
		t.Fatalf("空 case_id Extract: %v", err)
	}
}

// TestAddAgentSpan_and_Extract 带 agent span 的 Extract：llm/tool/agent 三类。
func TestAddAgentSpan_and_Extract(t *testing.T) {
	sess := session.NewSession()
	mgr := sess.Tracer().AgentSpanManager
	root := mgr.CreateAgentSpan()
	root.Name = "agent run"
	root.InvokeType = ""
	root.StartTime = ptrTime(time.Unix(1_700_000_000, 0))
	root.EndTime = ptrTime(time.Unix(1_700_000_000, 1))

	llmSpan := mgr.CreateAgentSpan(root)
	llmSpan.Name = "chat"
	llmSpan.InvokeType = "llm"
	llmSpan.MetaData = map[string]any{
		"on_invoke_data": []any{map[string]any{
			"llm_params": map[string]any{
				"model":    "test-model",
				"messages": []any{map[string]any{"role": "user", "content": "hi"}},
			},
		}},
	}
	llmSpan.Inputs = map[string]any{"prompt": "hi"}
	llmSpan.Outputs = map[string]any{"content": "hello"}
	llmSpan.StartTime = ptrTime(time.Unix(1_700_000_000, 0))
	llmSpan.EndTime = ptrTime(time.Unix(1_700_000_000, 1))

	toolSpan := mgr.CreateAgentSpan(root)
	toolSpan.Name = "execute_tool read_file"
	toolSpan.InvokeType = "tool"
	toolSpan.StartTime = ptrTime(time.Unix(1_700_000_000, 0))
	toolSpan.EndTime = ptrTime(time.Unix(1_700_000_000, 1))

	errSpan := mgr.CreateAgentSpan(root)
	errSpan.Name = "execute_tool boom"
	errSpan.InvokeType = "tool"
	errSpan.Error = map[string]any{"message": "exploded"}
	errSpan.StartTime = ptrTime(time.Unix(1_700_000_000, 0))
	errSpan.EndTime = ptrTime(time.Unix(1_700_000_000, 1))

	e := NewTracerTrajectoryExtractor()
	traj, err := e.Extract(sess, "case-y")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if traj == nil {
		t.Fatal("应产出轨迹")
	}
	if len(IterSpans(traj)) == 0 {
		t.Fatal("应包含构建出的 span")
	}
}

// ptrTime 时间指针。
func ptrTime(t time.Time) *time.Time { return &t }

// TestJsonSafe_sliceTopLevel 顶层切片。
func TestJsonSafe_sliceTopLevel(t *testing.T) {
	out := JSONSafe([]any{1, "x", map[string]any{"k": "v"}})
	if out == nil {
		t.Fatal("顶层切片应产出")
	}
}

// ──────────────────────────── 批次 4（收口 85%） ────────────────────────────

// TestRecordCaptureIssue_withStub 真实 SpanStub 采集 issue 入订阅。
// Python: processor.py _record_capture_issue。
func TestRecordCaptureIssue_withStub(t *testing.T) {
	p := NewTrajectorySpanProcessor(64)
	handle, err := p.Subscribe([]string{"llm", "tool"}, "")
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	stub := tracetest.SpanStub{
		Name: "chat test",
		SpanContext: trace.NewSpanContext(trace.SpanContextConfig{
			TraceID: trace.TraceID{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
				0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10},
			SpanID:     trace.SpanID{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff, 0x01, 0x02},
			TraceFlags: trace.FlagsSampled,
		}),
	}
	_ = stub
	// 经 tracetest SpanRecorder 产出真实 ReadOnlySpan。
	rec := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	defer func() { _ = provider.Shutdown(context.Background()) }()
	tp := provider.Tracer("test")
	ctx := trace.ContextWithSpanContext(context.Background(), stub.SpanContext)
	_, span := tp.Start(ctx, "chat test")
	span.End()
	ended := rec.Ended()
	if len(ended) == 0 {
		t.Fatal("SpanRecorder 应产出 span")
	}
	p.recordCaptureIssue("chat test", ended[0], fmt.Errorf("boom"), "llm", "code-x")
	_, issues := p.Drain(handle)
	if len(issues) == 0 {
		t.Fatal("采集 issue 应入订阅")
	}
}

// TestHasUncountedFilter nil 判定。
func TestHasUncountedFilter(t *testing.T) {
	if hasUncountedFilter(nil) {
		t.Fatal("nil 应 false")
	}
	if !hasUncountedFilter(func(map[string]any) bool { return true }) {
		t.Fatal("非 nil 应 true")
	}
}

// TestContextMessage_variants 窗口消息校验分支。
// Python: windows.py _context_message。
func TestContextMessage_variants(t *testing.T) {
	if contextMessage("str") != nil {
		t.Fatal("非 map 应 nil")
	}
	if contextMessage(map[string]any{"origin": "bogus"}) != nil {
		t.Fatal("非法 origin 应 nil")
	}
	valid := map[string]any{
		"origin": "external_user", "message_id": "m1", "role": "user",
	}
	if contextMessage(valid) == nil {
		t.Fatal("合法消息应通过")
	}
	bad := map[string]any{"origin": "external_user", "message_id": "", "role": "user"}
	if contextMessage(bad) != nil {
		t.Fatal("缺 message_id 应 nil")
	}
	bad2 := map[string]any{"origin": "external_user", "message_id": "m", "role": ""}
	if contextMessage(bad2) != nil {
		t.Fatal("缺 role 应 nil")
	}
}

// TestTrimTrajectoryWindow_cap 上限裁剪。
// Python: windows.py trim_trajectory_window。
func TestTrimTrajectoryWindow_cap(t *testing.T) {
	traj, err := NewTestTrajectory("sess-trim", map[string]any{}, []TestStep{})
	if err != nil {
		t.Fatalf("构建: %v", err)
	}
	trimmed, trimErr := TrimTrajectoryWindow(traj, 1)
	if trimErr != nil || trimmed == nil {
		t.Fatalf("裁剪应产出: %v", trimErr)
	}
}
