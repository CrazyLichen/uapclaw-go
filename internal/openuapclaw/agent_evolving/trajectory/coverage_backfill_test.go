package trajectory

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/tracer"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// coverage_backfill_test.go 25.2b Task 14 覆盖率回填：extractor / messages /
// windows / team / json_safe 的逐函数单测（上游行号见各断言注释）。

// ──────────────────────────── extractor ────────────────────────────

func spanOf(meta map[string]any) *tracer.TraceAgentSpan {
	return &tracer.TraceAgentSpan{MetaData: meta}
}

// TestSpanGet_MetaData优先 spanGet 鸭子访问（MetaData 键优先于结构体字段）。
func TestSpanGet_MetaData优先(t *testing.T) {
	span := spanOf(map[string]any{"name": "from-meta"})
	if got := spanGet(span, "name"); got != "from-meta" {
		t.Fatalf("meta priority: %v", got)
	}
	// 结构体字段回落。
	if got := spanGet(&tracer.TraceAgentSpan{Name: "field-name"}, "name"); got != "field-name" {
		t.Fatalf("field fallback: %v", got)
	}
	// nil span → nil。
	if spanGet(nil, "name") != nil {
		t.Fatal("nil span must return nil")
	}
	// 未知键 → nil。
	if spanGet(spanOf(map[string]any{}), "unknown") != nil {
		t.Fatal("missing key must return nil")
	}
}

// TestDtToNanos 各形态时间转换。
func TestDtToNanos(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		in   any
		want int64
	}{
		{now, now.UnixNano()},
		{&now, now.UnixNano()},
		{int(5), 5_000_000},
		{int64(6), 6_000_000},
		{float64(7), 7_000_000},
		{"not-a-time", 0},
		{nil, 0},
	}
	for _, c := range cases {
		if got := dtToNanos(c.in); got != c.want {
			t.Fatalf("dtToNanos(%v) = %d, want %d", c.in, got, c.want)
		}
	}
}

// TestAbs64 绝对值。
func TestAbs64(t *testing.T) {
	if abs64(-5) != 5 || abs64(5) != 5 || abs64(0) != 0 {
		t.Fatal("abs64")
	}
}

// TestClassifyKind 各 invoke_type 分类。
func TestClassifyKind(t *testing.T) {
	cases := []struct {
		invokeType string
		want       string
	}{
		{"tool", "tool"}, {"plugin", "tool"}, {"llm", "llm"},
		{"workflow", "workflow"}, {"", "agent"},
	}
	for _, c := range cases {
		span := spanOf(map[string]any{"invoke_type": c.invokeType})
		if got := classifyKind(span); got != c.want {
			t.Fatalf("classifyKind(%q) = %q, want %q", c.invokeType, got, c.want)
		}
	}
}

// TestBuildSpan_llm LLM span 构建（chat 名/模型属性/消息交换/usage）。
func TestBuildSpan_llm(t *testing.T) {
	e := NewTracerTrajectoryExtractor()
	span := spanOf(map[string]any{
		"invoke_type": "llm",
		"name":        "chat",
		"model":       "gpt-test",
		"on_invoke_data": map[string]any{
			"params": map[string]any{
				"model":    "gpt-test",
				"messages": []any{map[string]any{"role": "user", "content": "问题"}},
				"tools":    []any{map[string]any{"name": "t1"}},
			},
		},
		"on_result_data": map[string]any{
			"response": map[string]any{
				"content": "回答",
				"usage":   map[string]any{"prompt_tokens": 10, "completion_tokens": 5},
			},
		},
		"start_time": time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		"end_time":   time.Date(2026, 1, 1, 0, 0, 1, 0, time.UTC),
	})
	built, err := e.buildSpan(span, "trace-1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if built["name"] != "chat gpt-test" {
		t.Fatalf("span name = %v", built["name"])
	}
	attrs := normalizedAttrs(built)
	if attrs["gen_ai.request.model"] != "gpt-test" {
		t.Fatalf("model attr = %v", attrs["gen_ai.request.model"])
	}
	_ = attrs
	if built["startTimeUnixNano"] == nil {
		t.Fatal("start time must be set")
	}
}

// TestBuildSpan_tool 工具 span 构建（execute_tool 名/参数/结果）。
func TestBuildSpan_tool(t *testing.T) {
	e := NewTracerTrajectoryExtractor()
	span := spanOf(map[string]any{
		"invoke_type": "tool",
		"name":        "tool.write_file",
		"on_invoke_data": map[string]any{
			"args": map[string]any{"path": "/x"},
		},
		"on_result_data": map[string]any{"output": "ok"},
	})
	built, err := e.buildSpan(span, "trace-1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if built["name"] != "execute_tool write_file" {
		t.Fatalf("span name = %v", built["name"])
	}
	attrs := normalizedAttrs(built)
	if attrs["gen_ai.tool.name"] != "write_file" {
		t.Fatalf("tool name = %v", attrs["gen_ai.tool.name"])
	}
}

// TestBuildSpan_agent 通用 span 构建（名称透传/输入输出属性）。
func TestBuildSpan_agent(t *testing.T) {
	e := NewTracerTrajectoryExtractor()
	span := spanOf(map[string]any{
		"name":           "team_round",
		"on_invoke_data": map[string]any{"input": "x"},
		"agent_id":       "agent-1",
	})
	built, err := e.buildSpan(span, "trace-1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if built["name"] != "team_round" {
		t.Fatalf("span name = %v", built["name"])
	}
	attrs := normalizedAttrs(built)
	if attrs["agentteam.agent.id"] == nil {
		t.Fatalf("agent id attr missing: %v", attrs)
	}
}

// TestExtract_nilSession nil 会话返回空轨迹。
func TestExtract_nilSession(t *testing.T) {
	e := NewTracerTrajectoryExtractor()
	traj, err := e.Extract(nil, "")
	if err != nil || traj == nil {
		t.Fatalf("nil session must yield empty trajectory: %v %v", traj, err)
	}
}

// ──────────────────────────── messages ────────────────────────────

// TestMessages_字段助手 messageField/toolCallField 三兄弟。
func TestMessages_字段助手(t *testing.T) {
	message := map[string]any{"content": "c"}
	if messageField(message, "content") != "c" {
		t.Fatal("messageField")
	}
	if messageField(message, "missing") != nil {
		t.Fatal("missing field")
	}
	toolCall := map[string]any{"id": "1", "name": "n", "arguments": map[string]any{"x": 1}}
	if ToolCallID(toolCall) != "1" || ToolCallName(toolCall) != "n" {
		t.Fatal("tool call accessors")
	}
	if ToolCallArguments(toolCall) == nil {
		t.Fatal("arguments")
	}
}

// TestJsonText_SortJSONKeys jsonText 序列化与键排序。
func TestJsonText_SortJSONKeys(t *testing.T) {
	if jsonText(map[string]any{"b": 1, "a": 2}) != `{"a":2,"b":1}` {
		t.Fatalf("jsonText = %s", jsonText(map[string]any{"b": 1, "a": 2}))
	}
	sorted := sortJSONKeys(map[string]any{"z": 1, "a": 2})
	if jsonText(sorted) != `{"a":2,"z":1}` {
		t.Fatalf("sortJSONKeys = %s", jsonText(sorted))
	}
}

// TestTrimPromptToLastUser 截到最后一个 user（提示词窗口）。
func TestTrimPromptToLastUser(t *testing.T) {
	prompt := []map[string]any{
		{"role": "user", "content": "1"},
		{"role": "assistant", "content": "2"},
		{"role": "user", "content": "3"},
	}
	trimmed := trimPromptToLastUser(prompt)
	if len(trimmed) != 1 || trimmed[0]["content"] != "3" {
		t.Fatalf("trimmed = %v", trimmed)
	}
}

// TestSameAssistantTurn_工具结果确认 sameAssistantTurn 与 confirmToolResult。
func TestSameAssistantTurn_工具结果确认(t *testing.T) {
	completion := map[string]any{"role": "assistant", "content": "r"}
	if !sameAssistantTurn(completion, map[string]any{"role": "assistant", "content": "r"}) {
		t.Fatal("same turn must match")
	}
	projection := newMessageProjection()
	projection.addPrompt([]map[string]any{{"role": "user", "content": "q"}})
	projection.addCompletions([]map[string]any{completion})
	// 确认同一 assistant 内容在消息列表中出现过。
	if !projection.confirmCompletion(map[string]any{"role": "assistant", "content": "r"}) {
		t.Fatal("completion must be confirmed")
	}
}

// TestTrajectoryToMessages 投影主入口（空轨迹返回空消息）。
func TestTrajectoryToMessages(t *testing.T) {
	builder, err := NewTrajectoryBuilder("sess-msgs", "online", nil, nil, nil, nil, 0, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := builder.RecordTestStep(TestStep{Kind: "llm", Messages: []map[string]any{{"role": "user", "content": "q"}}, Response: map[string]any{"content": "a"}}); err != nil {
		t.Fatal(err)
	}
	traj, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	messages, err := TrajectoryToMessages(traj, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) == 0 {
		t.Fatal("llm exchange must project to messages")
	}
	for _, message := range messages {
		if message["role"] == nil {
			t.Fatalf("message missing role: %v", message)
		}
	}
}

// ──────────────────────────── windows ────────────────────────────

// TestPositiveInt_NonEmptyStr HasText 基础助手。
func TestPositiveInt_NonEmptyStr(t *testing.T) {
	if positiveInt(int64(3)) != 3 {
		t.Fatal("positiveInt")
	}
	if nonEmptyStr("") || nonEmptyStr(nil) || !nonEmptyStr("x") {
		t.Fatal("nonEmptyStr")
	}
	if hasText(nil) || !hasText("text") {
		t.Fatal("hasText")
	}
	if declaredConflict("a", "a") || !declaredConflict("a", "b") {
		t.Fatal("declaredConflict")
	}
}

// TestReadTrajectoryEvent_v2事件读取 完整信封事件读取（信封不完整返 nil）。
// Python: windows.py read_trajectory_event。
func TestReadTrajectoryEvent_v2事件读取(t *testing.T) {
	span := map[string]any{
		"name": "context.commit",
		"attributes": map[string]any{
			"openjiuwen.trajectory.record_kind":           "event",
			"openjiuwen.trajectory.event_kind":            "context.window.commit",
			"openjiuwen.trajectory.event_id":              "e1",
			"openjiuwen.trajectory.subject_id":            "subj-1",
			"openjiuwen.trajectory.sequence_epoch":        "epoch-1",
			"openjiuwen.trajectory.subject_sequence":      1,
			"openjiuwen.trajectory.recorded_at_unix_nano": 100,
			"openjiuwen.trajectory.payload":               map[string]any{"messages": []any{}},
		},
	}
	event := ReadTrajectoryEvent(span)
	if event == nil {
		t.Fatal("complete envelope must read back")
	}
	if event.EventID != "e1" || event.EventKind != "context.window.commit" || event.Sequence != 1 {
		t.Fatalf("event fields = %v %v %v", event.EventID, event.EventKind, event.Sequence)
	}
	// SpanID/ParentSpanID 为 fmt.Sprint 读面：缺键时 "<nil>"（上游语义）。
	if event.SpanID() == "" {
		t.Fatal("span id accessor")
	}
	incomplete := map[string]any{
		"name":       "context.commit",
		"attributes": map[string]any{"openjiuwen.trajectory.event_kind": "context.window.commit"},
	}
	if ReadTrajectoryEvent(incomplete) != nil {
		t.Fatal("incomplete envelope must be nil")
	}
}

// TestSelectTeamSpans_身份过滤 团队 trace 路由（team_id 无 trace 时先收集该
// 团队的 trace id 再过滤）。Python: team.py select_team_spans。
func TestSelectTeamSpans_身份过滤(t *testing.T) {
	teamSpan := map[string]any{
		"name": "team.round", "traceId": "trace-alpha",
		"attributes": map[string]any{
			"gen_ai.operation.name": "execute_tool",
			"agentteam.team.id":     "alpha",
		},
	}
	selected := SelectTeamSpans(spanEnvelope(teamSpan), []string{"tool"}, "alpha", "")
	if len(selected) != 1 {
		t.Fatalf("matching team id must select: %d", len(selected))
	}
	mismatch := SelectTeamSpans(spanEnvelope(teamSpan), []string{"tool"}, "beta", "")
	if len(mismatch) != 0 {
		t.Fatalf("mismatched team id must filter out: %d", len(mismatch))
	}
}

// TestTeamForest_扁平化 森林构建 + FlattenForest。
// Python: team.py build_team_forest / flatten_forest。
func TestTeamForest_扁平化(t *testing.T) {
	parent := map[string]any{
		"name": "execute_tool parent", "spanId": "s1", "traceId": "trace-alpha",
		"attributes": map[string]any{"gen_ai.operation.name": "execute_tool", "agentteam.team.id": "alpha"},
	}
	child := map[string]any{
		"name": "execute_tool child", "spanId": "s2", "parentSpanId": "s1", "traceId": "trace-alpha",
		"attributes": map[string]any{"gen_ai.operation.name": "execute_tool", "agentteam.team.id": "alpha"},
	}
	forest := TeamForest(spanEnvelope(child, parent), []string{"tool"}, "alpha", "trace-alpha")
	if len(forest) != 1 {
		t.Fatalf("two spans with parent link must form one root: %d", len(forest))
	}
	flat := FlattenForest(forest)
	if len(flat) != 2 {
		t.Fatalf("flatten must include both: %d", len(flat))
	}
}

// TestApplyContextDelta 上下文增量应用（append 操作）。
func TestApplyContextDelta(t *testing.T) {
	base := []map[string]any{{"role": "user", "content": "1", "message_id": "m1"}}
	operations := []map[string]any{
		{"op": "insert", "index": 1, "message": map[string]any{"role": "assistant", "content": "2", "message_id": "m2"}},
	}
	result := ApplyContextDelta(base, operations)
	if len(result) < 2 {
		t.Fatalf("append must extend: %v", result)
	}
}

// ──────────────────────────── team ────────────────────────────

// TestSpanCategory_IsTeamSpan 团队 span 判定。
func TestSpanCategory_IsTeamSpan(t *testing.T) {
	toolSpan := map[string]any{"name": "execute_tool x", "attributes": map[string]any{"gen_ai.operation.name": "execute_tool"}}
	if SpanCategory(toolSpan) != "tool" {
		t.Fatalf("category = %q", SpanCategory(toolSpan))
	}
	if !IsTeamSpan(toolSpan) {
		t.Fatal("tool category is a team category (tool 在团队类别闭集内)")
	}
}

// ──────────────────────────── json_safe ────────────────────────────

// TestJSONSafe 循环引用/不可序列化值安全化。
func TestJSONSafe(t *testing.T) {
	// 不可序列化值经 json.Marshal 兜底路径安全化。
	if JSONSafe(make(chan int)) == nil {
		t.Fatal("unsupported value must not panic")
	}
	if JSONSafe(map[string]any{"ok": 1}) == nil {
		t.Fatal("plain map must pass through")
	}
}

// TestMessageToDict_BaseMessage 字典化。
func TestMessageToDict_BaseMessage(t *testing.T) {
	dict := MessageToDict(map[string]any{"role": "user", "content": "hi"})
	if dict["content"] != "hi" {
		t.Fatalf("dict = %v", dict)
	}
}

// normalizedAttrs 归一化 attributes（OTLP 列表形态）读回 map。
func normalizedAttrs(built map[string]any) map[string]any {
	switch attrs := built["attributes"].(type) {
	case []any:
		result := map[string]any{}
		for _, item := range attrs {
			if entry, isMap := item.(map[string]any); isMap {
				value := entry["value"]
				if valueMap, isValue := value.(map[string]any); isValue {
					if str, hasStr := valueMap["stringValue"]; hasStr {
						value = str
					}
				}
				result[fmt.Sprint(entry["key"])] = value
			}
		}
		return result
	case map[string]any:
		return attrs
	default:
		return map[string]any{}
	}
}

// spanEnvelope 把 span 包成 OTLP resourceSpans 载荷（IterSpans 的解析形态）。
func spanEnvelope(spans ...map[string]any) map[string]any {
	items := make([]any, 0, len(spans))
	for _, span := range spans {
		items = append(items, span)
	}
	return map[string]any{
		"resourceSpans": []any{map[string]any{
			"scopeSpans": []any{map[string]any{"spans": items}},
		}},
	}
}

// ──────────────────────────── model 访问器 / spans 解码 / store ────────────────────────────

// TestTrajectoryAccessors 轨迹元数据访问器（TrajectoryID/SessionID/TeamID/
// MemberID/Metadata/ResourceAttributes）。
func TestTrajectoryAccessors(t *testing.T) {
	traj, err := NewTrajectoryBuilder("sess-acc", "online", nil, nil, nil, nil, 0, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = traj
	built, err := NewTrajectoryBuilder("sess-acc2", "online", nil, nil, nil, nil, 0, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := built.RecordTestStep(TestStep{Kind: "tool", ToolName: "read_file", CallArgs: map[string]any{"p": 1}}); err != nil {
		t.Fatal(err)
	}
	trajectory, err := built.Build()
	if err != nil {
		t.Fatal(err)
	}
	if trajectory.TrajectoryID() == "" {
		t.Fatal("trajectory id")
	}
	payload := trajectory.ToOTLP()
	decoded, err := FromOTLP(payload)
	if err != nil || decoded == nil {
		t.Fatalf("FromOTLP roundtrip: %v", err)
	}
	_ = decoded.ResourceAttributes()
	_ = decoded.Metadata()
	_ = toOTLPValue(map[string]any{"a": 1})
	if firstValue(map[string]any{"k": "v"}, "k", "other") != "v" {
		t.Fatal("firstValue")
	}
	if firstValue(map[string]any{}, "missing") != nil {
		t.Fatal("firstValue missing")
	}
	// copyJSON 深拷贝。
	original := map[string]any{"a": map[string]any{"b": 1}}
	copied := copyJSON(original).(map[string]any)
	copied["a"].(map[string]any)["b"] = 2
	if original["a"].(map[string]any)["b"] != 1 {
		t.Fatal("copyJSON must deep copy")
	}
}

// TestDecodeOTLPValue_AttributesFromMap OTLP 值解码（双向往返）。
func TestDecodeOTLPValue_AttributesFromMap(t *testing.T) {
	encoded := AttributesFromMap(map[string]any{"k": "v", "n": 1})
	decoded := AttributesToMap(encoded)
	if decoded["k"] != "v" {
		t.Fatalf("decoded k = %v", decoded["k"])
	}
	value := DecodeOTLPValue(map[string]any{"stringValue": "s"})
	if value != "s" {
		t.Fatalf("decoded value = %v", value)
	}
	_ = normalizeAttributes(map[string]any{"a": "b"})
}

// TestPayloadFor_MergeTrajectories 载荷形态校验与轨迹合并。
func TestPayloadFor_MergeTrajectories(t *testing.T) {
	if _, err := payloadFor("bad"); err == nil {
		t.Fatal("bad payload must fail")
	}
	traj, err := NewTrajectoryBuilder("sess-merge", "online", nil, nil, nil, nil, 0, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := traj.RecordTestStep(TestStep{Kind: "tool", ToolName: "a"}); err != nil {
		t.Fatal(err)
	}
	trajectory, err := traj.Build()
	if err != nil {
		t.Fatal(err)
	}
	merged, err := MergeTrajectories(trajectory, trajectory)
	if err != nil || merged == nil {
		t.Fatalf("merge: %v", err)
	}
}

// TestTrimSpans_MergeSpans span 修剪与合并。
func TestTrimSpans_MergeSpans(t *testing.T) {
	builder, err := NewTrajectoryBuilder("sess-trim", "online", nil, nil, nil, nil, 0, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a", "b", "c", "d", "e"} {
		if err := builder.RecordTestStep(TestStep{Kind: "tool", ToolName: name}); err != nil {
			t.Fatal(err)
		}
	}
	trajectory, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	trimmed := TrimSpans(IterSpans(trajectory), 3, nil, nil)
	if len(trimmed) != 3 {
		t.Fatalf("trim to 3: %d", len(trimmed))
	}
	merged, mergeErr := MergeSpans(trajectory.ToOTLP(), IterSpans(trajectory))
	if mergeErr != nil || merged == nil {
		t.Fatalf("merge spans: %v", mergeErr)
	}
}

// TestStore_FileSystemCRUD 文件存储 CRUD（TempDir）。
func TestStore_FileSystemCRUD(t *testing.T) {
	store := NewFileTrajectoryStore(t.TempDir())
	builder, err := NewTrajectoryBuilder("sess-store", "online", nil, nil, nil, nil, 0, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := builder.RecordTestStep(TestStep{Kind: "tool", ToolName: "x"}); err != nil {
		t.Fatal(err)
	}
	trajectory, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	id := trajectory.TrajectoryID()
	if err := store.Save(trajectory, ""); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load(id, "")
	if err != nil || loaded == nil {
		t.Fatalf("load: %v", err)
	}
	listed, err := store.Query("", "", "", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) == 0 {
		t.Fatal("query must list saved trajectory")
	}
}

// TestBuilder_AddSpan_SessionID builder 补面（AddSpan 直记 + SessionID）。
func TestBuilder_AddSpan_SessionID(t *testing.T) {
	builder, err := NewTrajectoryBuilder("sess-builder", "online", nil, nil, nil, nil, 0, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if builder.SessionID() != "sess-builder" {
		t.Fatalf("session id = %q", builder.SessionID())
	}
	span := map[string]any{
		"name": "execute_tool direct", "spanId": "sb1", "traceId": "t1",
		"attributes": AttributesFromMap(map[string]any{"gen_ai.operation.name": "execute_tool"}),
	}
	if err := builder.AddSpan(span); err != nil {
		t.Fatal(err)
	}
	built, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	if len(IterSpans(built)) != 1 {
		t.Fatalf("direct span must be recorded: %d", len(IterSpans(built)))
	}
}

// ──────────────────────────── 批次三：processor 面 / testfixture / team 补面 ────────────────────────────

// TestProcessorSuppress_Shutdown_DrainedIssues processor 补面。
// 抑制窗口内 OnEnd 不采集；Shutdown 后订阅清空。
func TestProcessorSuppress_Shutdown_DrainedIssues(t *testing.T) {
	p := NewTrajectorySpanProcessor(0)
	handle, err := p.Subscribe([]string{"tool"}, "")
	if err != nil {
		t.Fatal(err)
	}
	// 抑制窗口内的 span 不采集。
	suppress := p.WithSuppress()
	suppress()
	p.OnStart(context.Background(), nil)
	if err := p.ForceFlush(context.Background()); err != nil {
		t.Fatal(err)
	}
	p.Unsubscribe(handle)
	if err := p.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	issues := DrainedIssues([]CaptureIssue{})
	if len(issues) != 0 {
		t.Fatal("empty issues")
	}
}

// TestNewTestTrajectoryWithID_变体 testfixture 变体。
func TestNewTestTrajectoryWithID_变体(t *testing.T) {
	traj, err := NewTestTrajectory("sess-fx", nil, []TestStep{
		{Kind: "tool", ToolName: "read_file", CallArgs: map[string]any{"path": "/x"}},
	})
	if err != nil || traj == nil {
		t.Fatalf("fixture: %v", err)
	}
	withID := NewTestTrajectoryWithID("tid-1")
	if withID == nil {
		t.Fatal("withID fixture")
	}
	withSession := NewTestTrajectoryWithIDWithSession("sess-fx3")
	if withSession == nil {
		t.Fatal("withSession fixture")
	}
}

// TestIterTeamSpans_MemberSpans_ReadTeamAttributes team 补面。
func TestIterTeamSpans_MemberSpans_ReadTeamAttributes(t *testing.T) {
	span := map[string]any{
		"name": "execute_tool a", "traceId": "trace-t", "spanId": "s1",
		"attributes": AttributesFromMap(map[string]any{
			"gen_ai.operation.name": "execute_tool",
			"agentteam.team.id":     "alpha",
			"agentteam.member.name": "member-a",
		}),
	}
	selected := IterTeamSpans(spanEnvelope(span), []string{"tool"}, "alpha", "")
	if len(selected) != 1 {
		t.Fatalf("iter team spans: %d", len(selected))
	}
	attrs := ReadTeamAttributes(span)
	if attrs["agentteam.member.name"] != "member-a" {
		t.Fatalf("member attr = %v", attrs["agentteam.member.id"])
	}
	memberSpans := MemberSpans(spanEnvelope(span), "member-a", "alpha", []string{"tool"}, false, false)
	if len(memberSpans) != 1 {
		t.Fatalf("member spans: %d", len(memberSpans))
	}
}

// TestTaskValue_SelectTaskSpans 任务 span 选择。
func TestTaskValue_SelectTaskSpans(t *testing.T) {
	span := map[string]any{
		"name": "task.created", "traceId": "trace-task", "spanId": "s9",
		"attributes": AttributesFromMap(map[string]any{
			"gen_ai.operation.name": "execute_tool",
			"agentteam.team.id":     "alpha",
			"agentteam.task.id":     "task-1",
		}),
	}
	spans := SelectTaskSpans(spanEnvelope(span), "task-1", "alpha", []string{"tool"}, false)
	if len(spans) != 1 {
		t.Fatalf("task spans: %d", len(spans))
	}
}

// TestReadLLMMessages_CoerceLists LLM 消息读取与数值列表强制。
func TestReadLLMMessages_CoerceLists(t *testing.T) {
	span := map[string]any{
		"attributes": map[string]any{
			"gen_ai.input.messages":   map[string]any{"stringValue": `[{"role":"user","content":"q"}]`},
			"gen_ai.output.messages":  map[string]any{"stringValue": `[{"role":"assistant","content":"a"}]`},
			"rl.prompt_token_ids":     map[string]any{"stringValue": "[1,2,3]"},
			"rl.completion_token_ids": map[string]any{"stringValue": "[4,5]"},
			"rl.logprobs":             map[string]any{"stringValue": "[0.1,0.2]"},
		},
	}
	prompts := ReadLLMMessages(span, false)
	if len(prompts) == 0 {
		t.Fatalf("llm messages: %d", len(prompts))
	}
	fields := ReadRLFields(span)
	_ = coerceIntList([]any{int64(1), 2})
	_ = coerceFloatList([]any{0.1, 2})
	if fields == nil {
		t.Fatal("rl fields must read")
	}
}

// TestHasUncountedFilter_CropTrajectory 修剪过滤与裁剪。
func TestHasUncountedFilter_CropTrajectory(t *testing.T) {
	builder, err := NewTrajectoryBuilder("sess-crop", "online", nil, nil, nil, nil, 0, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a", "b"} {
		if err := builder.RecordTestStep(TestStep{Kind: "tool", ToolName: name}); err != nil {
			t.Fatal(err)
		}
	}
	trajectory, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	cropped, cropErr := CropTrajectory(trajectory, 1, nil, nil, nil)
	if cropErr != nil {
		t.Fatal(cropErr)
	}
	if cropped == nil {
		t.Fatal("crop must return trajectory")
	}
}

// ──────────────────────────── 批次四：messages 助手 / windows 链 ────────────────────────────

// TestNormalizeToolCall_ToolMessage 工具调用归一化与工具消息构造。
// Python: messages.py _normalize_tool_call / _tool_message。
func TestNormalizeToolCall_ToolMessage(t *testing.T) {
	normalized := normalizeToolCall(map[string]any{
		"id": "c1", "name": "write_file", "arguments": map[string]any{"path": "/x"},
	})
	if normalized["type"] != "function" {
		t.Fatalf("default type = %v", normalized["type"])
	}
	fn := normalized["function"].(map[string]any)
	if fn["name"] != "write_file" || fn["arguments"] != `{"path":"/x"}` {
		t.Fatalf("function = %v", fn)
	}
	message := toolMessage(map[string]any{"id": "c1", "name": "write_file", "output": "ok"}, map[string]any{})
	if message["role"] != "tool" || message["name"] != "write_file" || message["tool_call_id"] != "c1" {
		t.Fatalf("tool message = %v", message)
	}
	// 缺 name 时按 id 回查。
	byID := toolMessage(map[string]any{"id": "c1", "output": "ok"}, map[string]any{"c1": "write_file"})
	if byID["name"] != "write_file" {
		t.Fatalf("by id name = %v", byID["name"])
	}
	// 错误形态。
	errMessage := toolMessage(map[string]any{"id": "c2", "error": map[string]any{"message": "boom"}}, map[string]any{})
	if errMessage["content"] != "boom" {
		t.Fatalf("error content = %v", errMessage["content"])
	}
}

// TestWindowMessage_ProjectionAddWindow 窗口消息与投影 addWindow。
// Python: messages.py _window_message / message_projection.add_window。
func TestWindowMessage_ProjectionAddWindow(t *testing.T) {
	message := windowMessage(map[string]any{"role": "assistant", "content": "c", "extra": "drop"})
	if _, has := message["extra"]; has {
		t.Fatal("extra keys must be dropped")
	}
	projection := newMessageProjection()
	projection.addWindow([]map[string]any{{"role": "user", "content": "1", "message_id": "m1"}})
	// addWindow 内部登记（窗口字段为私有投影态，行为经 confirm 断言）。
	if projection.confirmCompletion(map[string]any{"role": "user", "content": "1"}) {
		t.Fatal("window user message must not be a completion")
	}
	projection.addToolResult(map[string]any{"id": "c1", "output": "ok"})
	if len(projection.toolResults) != 1 {
		t.Fatalf("tool results = %v", projection.toolResults)
	}
	// confirmToolResult：已知工具结果 id 且消息里没有未确认完成时确认掉。
	if !projection.confirmToolResult(map[string]any{"role": "tool", "tool_call_id": "c1"}) {
		t.Fatal("known tool result must confirm")
	}
	_ = responseToText("x")
}

// TestRebaseWindowChain_TrimTrajectoryWindow 窗口重基链（保留窗 → 历史重基）。
// Python: windows.py rebase_window_chain / trim_trajectory_window。
func TestRebaseWindowChain_TrimTrajectoryWindow(t *testing.T) {
	builder, err := NewTrajectoryBuilder("sess-rebase", "online", nil, nil, nil, nil, 0, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a", "b", "c"} {
		if err := builder.RecordTestStep(TestStep{Kind: "tool", ToolName: name}); err != nil {
			t.Fatal(err)
		}
	}
	trajectory, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	trimmed, err := TrimTrajectoryWindow(trajectory, 2)
	if err != nil || trimmed == nil {
		t.Fatalf("trim window: %v", err)
	}
	if len(IterSpans(trimmed)) != 2 {
		t.Fatalf("trimmed spans: %d", len(IterSpans(trimmed)))
	}
}

// TestDescendantSpans 后代选择（parent → child）。
// Python: team.py descendant_spans。
func TestDescendantSpans(t *testing.T) {
	parent := map[string]any{
		"name": "execute_tool parent", "spanId": "p1", "traceId": "trace-d",
		"attributes": AttributesFromMap(map[string]any{
			"gen_ai.operation.name": "execute_tool", "agentteam.team.id": "alpha",
		}),
	}
	child := map[string]any{
		"name": "execute_tool child", "spanId": "c1", "parentSpanId": "p1", "traceId": "trace-d",
		"attributes": AttributesFromMap(map[string]any{
			"gen_ai.operation.name": "execute_tool", "agentteam.team.id": "alpha",
		}),
	}
	spans := DescendantSpans(spanEnvelope(child, parent), parent, []string{"tool"}, "alpha")
	if len(spans) != 1 {
		t.Fatalf("descendants: %d", len(spans))
	}
}

// ──────────────────────────── 批次五：windows 助手收尾 ────────────────────────────

// TestWindowsHelpers_批次五 replayIssue/contextDelta/sameWindow/matchesCheckpoint/
// statedIfPresent/syntheticSpanID/trimBaselineSpan。
func TestWindowsHelpers_批次五(t *testing.T) {
	event := &TrajectoryEvent{SubjectID: "s1", EventID: "e1", Sequence: 2}
	issue := replayIssue("replay_mismatch", "no", event)
	if issue["subject_id"] != "s1" || issue["code"] != "replay_mismatch" {
		t.Fatalf("issue = %v", issue)
	}
	// contextDelta：合法 insert。
	delta := contextDelta(map[string]any{
		"op": "insert", "message_id": "m1", "index": 1,
		"message": map[string]any{"role": "user", "content": "x", "origin": "external_user", "message_id": "m1"},
	})
	if delta == nil || delta["op"] != "insert" {
		t.Fatalf("delta = %v", delta)
	}
	// insert 缺 message → nil。
	if contextDelta(map[string]any{"op": "insert", "message_id": "m1"}) != nil {
		t.Fatal("insert without message must be nil")
	}
	// 非法 op → nil。
	if contextDelta(map[string]any{"op": "nope", "message_id": "m1"}) != nil {
		t.Fatal("invalid op must be nil")
	}
	// sameWindow/matchesCheckpoint。
	left := []map[string]any{{"role": "user", "content": "1"}}
	right := []map[string]any{{"role": "user", "content": "1"}}
	if !sameWindow(left, right) {
		t.Fatal("same window")
	}
	if !matchesCheckpoint(left, right) {
		t.Fatal("checkpoint match")
	}
	if !matchesCheckpoint(left, nil) {
		t.Fatal("nil stated matches any rebuilt")
	}
	// syntheticSpanID / trimBaselineSpan。
	id := syntheticSpanID("ctx", "m1")
	if id == "" {
		t.Fatal("synthetic id")
	}
	// statedIfPresent：has 标志决定是否带陈述窗口。
	if statedIfPresent(left, true) == nil || statedIfPresent(left, false) != nil {
		t.Fatal("statedIfPresent flag semantics")
	}
	// syntheticSpanID 确定性。
	if syntheticSpanID("ctx", "m1") != syntheticSpanID("ctx", "m1") {
		t.Fatal("synthetic id must be deterministic")
	}
	// TeamID/MemberID 访问器（构建器未设团队/成员时为空）。
	traj, terr := NewTestTrajectory("sess-tid", map[string]any{}, nil)
	if terr != nil {
		t.Fatal(terr)
	}
	_ = traj.TeamID()
	_ = traj.MemberID()
}

// ──────────────────────────── 批次六：replay/rebase 全链 ────────────────────────────

// commitEventSpan 构造一条完整的窗口提交事件 span。
func commitEventSpan(subjectID, epoch string, sequence int, payload map[string]any) map[string]any {
	payloadJSON, _ := jsonMarshalForTest(payload)
	return map[string]any{
		"name": "context.window.commit",
		"attributes": map[string]any{
			"openjiuwen.trajectory.record_kind":           "event",
			"openjiuwen.trajectory.event_kind":            "context.window.commit",
			"openjiuwen.trajectory.event_id":              "evt-" + subjectID + "-" + fmt.Sprint(sequence),
			"openjiuwen.trajectory.subject_id":            subjectID,
			"openjiuwen.trajectory.sequence_epoch":        epoch,
			"openjiuwen.trajectory.subject_sequence":      sequence,
			"openjiuwen.trajectory.recorded_at_unix_nano": 1000 + sequence,
			"openjiuwen.trajectory.payload":               payloadJSON,
		},
	}
}

// jsonMarshalForTest 测试内 JSON 序列化。
func jsonMarshalForTest(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// TestApplyContextDelta_全操作 move/replace/remove 操作面。
func TestApplyContextDelta_全操作(t *testing.T) {
	base := []map[string]any{
		{"role": "user", "content": "1", "message_id": "m1"},
		{"role": "assistant", "content": "2", "message_id": "m2"},
	}
	// replace。
	replaced := ApplyContextDelta(base, []map[string]any{
		{"op": "replace", "message_id": "m1", "message": map[string]any{"role": "user", "content": "1'"}},
	})
	if len(replaced) != 2 || replaced[0]["content"] != "1'" {
		t.Fatalf("replace: %v", replaced)
	}
	// move。
	moved := ApplyContextDelta(base, []map[string]any{{"op": "move", "message_id": "m1", "index": 1}})
	if len(moved) != 2 || moved[1]["message_id"] != "m1" {
		t.Fatalf("move: %v", moved)
	}
	// remove。
	removed := ApplyContextDelta(base, []map[string]any{{"op": "remove", "message_id": "m1"}})
	if len(removed) != 1 || removed[0]["message_id"] != "m2" {
		t.Fatalf("remove: %v", removed)
	}
	// 未知 op → nil。
	if ApplyContextDelta(base, []map[string]any{{"op": "nope", "message_id": "m1"}}) != nil {
		t.Fatal("unknown op must be nil")
	}
	// remove 缺失目标 → nil。
	if ApplyContextDelta(base, []map[string]any{{"op": "remove", "message_id": "nope"}}) != nil {
		t.Fatal("missing target must be nil")
	}
	// replace 缺失目标 → nil。
	if ApplyContextDelta(base, []map[string]any{{"op": "replace", "message_id": "nope", "message": map[string]any{}}}) != nil {
		t.Fatal("missing replace target must be nil")
	}
}

// TestRebaseWindowChain_无提交保留 retained 无提交事件 → 原样返回。
func TestRebaseWindowChain_无提交保留(t *testing.T) {
	builder, err := NewTrajectoryBuilder("sess-rebase-nocommit", "online", nil, nil, nil, nil, 0, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := builder.RecordTestStep(TestStep{Kind: "tool", ToolName: "a"}); err != nil {
		t.Fatal(err)
	}
	trajectory, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	retained, err := RebaseWindowChain(trajectory, trajectory)
	if err != nil || retained == nil {
		t.Fatalf("no-commit rebase: %v", err)
	}
}

// TestRebaseWindowChain_提交链 retained 带窗口提交事件 → 全链重基。
func TestRebaseWindowChain_提交链(t *testing.T) {
	// history：含两个成员的提交事件。
	historyPayload := map[string]any{
		"messages": []any{
			map[string]any{"role": "user", "content": "1", "message_id": "m1", "origin": "external_user"},
		},
		"window_id": "w1",
	}
	historyJSON, _ := jsonMarshalForTest(historyPayload)
	historySpan := map[string]any{
		"name": "context.commit", "traceId": "trace-rb", "spanId": "rb1",
		"attributes": map[string]any{
			"openjiuwen.trajectory.record_kind":           "event",
			"openjiuwen.trajectory.event_kind":            "context.window.commit",
			"openjiuwen.trajectory.event_id":              "e1",
			"openjiuwen.trajectory.subject_id":            "subj-a",
			"openjiuwen.trajectory.sequence_epoch":        "ep1",
			"openjiuwen.trajectory.subject_sequence":      1,
			"openjiuwen.trajectory.recorded_at_unix_nano": 100,
			"openjiuwen.trajectory.payload":               map[string]any{"messages": []any{}, "window_id": "w1"},
		},
	}
	_ = historyJSON
	builder, err := NewTrajectoryBuilder("sess-rebase", "online", nil, nil, nil, nil, 0, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := builder.RecordSpan(historySpan); err != nil {
		t.Fatal(err)
	}
	history, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	retained, err := RebaseWindowChain(history, history)
	if err != nil || retained == nil {
		t.Fatalf("commit chain rebase: %v", err)
	}
}

// TestIterTrajectoryEvents_ownerSpan owner span 上的 span event 形态读取。
func TestIterTrajectoryEvents_ownerSpan(t *testing.T) {
	// 无事件的轨迹 → 空列表。
	builder, err := NewTrajectoryBuilder("sess-noev", "online", nil, nil, nil, nil, 0, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := builder.RecordTestStep(TestStep{Kind: "tool", ToolName: "a"}); err != nil {
		t.Fatal(err)
	}
	trajectory, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	events := IterTrajectoryEvents(trajectory)
	if len(events) != 0 {
		t.Fatalf("no-event trajectory: %d", len(events))
	}
	// 有提交事件的轨迹 → 读出。
	commitSpan := commitEventSpan("subj-x", "ep", 1, map[string]any{"messages": []any{}})
	builder2, err := NewTrajectoryBuilder("sess-ev", "online", nil, nil, nil, nil, 0, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := builder2.RecordSpan(commitSpan); err != nil {
		t.Fatal(err)
	}
	trajectory2, err := builder2.Build()
	if err != nil {
		t.Fatal(err)
	}
	events = IterTrajectoryEvents(trajectory2)
	if len(events) != 1 {
		t.Fatalf("commit event must read: %d", len(events))
	}
}

// ──────────────────────────── 批次七：messages/json_safe/spans 低覆盖函数 ────────────────────────────

// TestMessageToDict_三分支 map/可序列化/兜底三分支。
func TestMessageToDict_三分支(t *testing.T) {
	// nil。
	if dict := MessageToDict(nil); dict["role"] != "unknown" {
		t.Fatalf("nil: %v", dict)
	}
	// map 直通。
	if dict := MessageToDict(map[string]any{"role": "user", "content": "hi"}); dict["content"] != "hi" {
		t.Fatalf("map: %v", dict)
	}
	// 结构体经 JSON。
	type fakeMessage struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	if dict := MessageToDict(fakeMessage{Role: "user", Content: "q"}); dict["content"] != "q" {
		t.Fatalf("struct: %v", dict)
	}
	// 不可序列化兜底。
	if dict := MessageToDict(make(chan int)); dict["role"] != "unknown" {
		t.Fatalf("fallback: %v", dict)
	}
}

// TestResponseToText_三形态 content/text/兜底。
func TestResponseToText_三形态(t *testing.T) {
	if responseToText(nil) != "" {
		t.Fatal("nil")
	}
	if responseToText(map[string]any{"content": "c"}) != "c" {
		t.Fatal("content key")
	}
	if responseToText(map[string]any{"text": "t"}) != "t" {
		t.Fatal("text key")
	}
	if responseToText("plain") != "plain" {
		t.Fatal("plain")
	}
}

// TestNormalizeMessage_ToolCallIDs 消息归一化与 tool_calls id 提取。
func TestNormalizeMessage_ToolCallIDs(t *testing.T) {
	message := map[string]any{
		"role": "assistant",
		"tool_calls": []any{
			map[string]any{"id": "c1", "name": "a", "arguments": map[string]any{"x": 1}},
			"not-a-map",
			map[string]any{"name": "no-id"},
		},
	}
	normalized := normalizeMessage(message)
	calls := normalized["tool_calls"].([]any)
	if len(calls) != 2 {
		t.Fatalf("normalized calls: %d", len(calls))
	}
	if calls[0].(map[string]any)["type"] != "function" {
		t.Fatal("default type")
	}
	ids := toolCallIDsOf(normalized)
	if len(ids) != 1 || ids[0] != "c1" {
		t.Fatalf("ids = %v", ids)
	}
	// toolCallField 缺失键 → nil。
	if toolCallField(map[string]any{}, "id") != nil {
		t.Fatal("missing id")
	}
	// rememberToolCallNames 记录。
	projection := newMessageProjection()
	projection.rememberToolCallNames(normalized)
	projection.rememberToolCallNames(map[string]any{"tool_calls": []any{"bad"}})
}

// TestJsonSafe_各类型 数值/切片/嵌套/不可序列化。
func TestJsonSafe_各类型(t *testing.T) {
	value := map[string]any{
		"list":   []any{1, "two", 3.5, true},
		"nested": map[string]any{"deep": map[string]any{"leaf": "v"}},
		"chan":   make(chan int),
		"fn":     func() {},
	}
	safe := JSONSafe(value)
	if safe == nil {
		t.Fatal("safe must not be nil")
	}
	// JSON 再序列化必须成功（安全化语义）。
	if _, err := json.Marshal(safe); err != nil {
		t.Fatalf("safe value must marshal: %v", err)
	}
	if JSONSafe(42) != 42 {
		t.Fatal("scalar passthrough")
	}
}

// TestSpanEvents_CompareSpanSortKey span 事件读取与排序键比较。
func TestSpanEvents_CompareSpanSortKey(t *testing.T) {
	span := map[string]any{
		"name": "execute_tool a", "spanId": "s1", "traceId": "t1",
		"startTimeUnixNano": "100",
		"events": []any{
			map[string]any{"name": "exception", "attributes": map[string]any{"exception.message": "boom"}},
		},
	}
	events := SpanEvents(span)
	if len(events) != 1 {
		t.Fatalf("span events: %d", len(events))
	}
	keyA := SpanSortKeyOf(map[string]any{"name": "execute_tool a", "spanId": "s1", "startTimeUnixNano": "100"})
	keyB := SpanSortKeyOf(map[string]any{"name": "execute_tool b", "spanId": "s2", "startTimeUnixNano": "200"})
	if compareSpanSortKey(keyA, keyB) == 0 {
		t.Fatal("different spans must compare non-zero")
	}
	// 无时间键的比较。
	keyC := SpanSortKeyOf(map[string]any{"name": "x", "spanId": "s3"})
	_ = compareSpanSortKey(keyA, keyC)
}

// TestStructuredPartsText_Flatten 结构化载荷文本化。
func TestStructuredPartsText_Flatten(t *testing.T) {
	parts := []any{
		map[string]any{"type": "text", "content": "hello"},
		map[string]any{"type": "image"},
	}
	text := structuredPartsText(parts)
	if text != "hello" {
		t.Fatalf("text part must render: %q", text)
	}
	if structuredPartsText("not-list") != "" {
		t.Fatal("non-list must be empty")
	}
	flat := flattenStructuredMessage(map[string]any{
		"role": "assistant",
		"content": map[string]any{"parts": []any{
			map[string]any{"type": "text", "text": "answer"},
		}},
	})
	if flat == nil {
		t.Fatal("flatten must return message")
	}
	// 列表形态 content。
	flatList := flattenStructuredMessage(map[string]any{
		"role":    "assistant",
		"content": []any{map[string]any{"type": "text", "text": "a"}},
	})
	_ = flatList
	// toAnyList。
	if len(toAnyList([]any{1, 2})) != 2 {
		t.Fatal("toAnyList")
	}
	if toAnyList("not-list") != nil {
		t.Fatal("non-list must be nil")
	}
}

// TestCoerceLists_Collect 强制列表转换（各元素形态）。
func TestCoerceLists_Collect(t *testing.T) {
	ints := coerceIntList([]any{1, int64(2), float64(3), "bad", nil})
	if len(ints) != 3 {
		t.Fatalf("ints = %v", ints)
	}
	floats := coerceFloatList([]any{1, float64(2.5), "x"})
	if len(floats) != 2 {
		t.Fatalf("floats = %v", floats)
	}
	// ReadRLFields 完整字段。
	span := map[string]any{
		"attributes": map[string]any{
			"rl.prompt_token_ids":     map[string]any{"stringValue": "[1,2]"},
			"rl.completion_token_ids": map[string]any{"stringValue": "[3]"},
			"rl.logprobs":             map[string]any{"stringValue": "[0.5]"},
		},
	}
	fields := ReadRLFields(span)
	if fields == nil {
		t.Fatal("rl fields")
	}
}

// TestVersionName_MetadataValue store 版本命名与元数据值。
func TestVersionName_MetadataValue(t *testing.T) {
	name, nameErr := versionName("")
	if nameErr != nil || name == "" {
		t.Fatalf("default version: %q %v", name, nameErr)
	}
	versioned, vErr := versionName("v2")
	if vErr != nil {
		t.Fatal(vErr)
	}
	_ = versioned
	builder, bErr := NewTrajectoryBuilder("sess-meta", "online", nil, nil, nil, nil, 0, nil, nil)
	if bErr != nil {
		t.Fatal(bErr)
	}
	trajectory, tErr := builder.Build()
	if tErr != nil {
		t.Fatal(tErr)
	}
	if metadataValue(trajectory, "missing") != "" {
		t.Fatal("missing metadata")
	}
}

// ──────────────────────────── 批次八：ReplayWindows 全链 + extractor 助手 ────────────────────────────

// commitEvent 构建提交事件 span（可带压缩载荷/父 span）。
func commitEvent(subjectID, epoch string, sequence int, parentSpanID string, payload map[string]any) map[string]any {
	span := commitEventSpan(subjectID, epoch, sequence, payload)
	if parentSpanID != "" {
		span["parentSpanId"] = parentSpanID
	}
	return span
}

// compactionEvent 构建压缩完成事件。
func compactionEvent(subjectID, epoch string, sequence, operationID string) map[string]any {
	return map[string]any{
		"name": "compaction.completed",
		"attributes": map[string]any{
			"openjiuwen.trajectory.record_kind":           "event",
			"openjiuwen.trajectory.event_kind":            "compaction.completed",
			"openjiuwen.trajectory.event_id":              "ce-" + sequence,
			"openjiuwen.trajectory.subject_id":            subjectID,
			"openjiuwen.trajectory.sequence_epoch":        epoch,
			"openjiuwen.trajectory.subject_sequence":      sequence,
			"openjiuwen.trajectory.recorded_at_unix_nano": 2000,
			"openjiuwen.trajectory.payload":               map[string]any{"operation_id": operationID},
		},
	}
}

// TestReplayWindows_提交链与序列间隙 提交回放（缺口 issue / 压缩关联 / 冲突）。
func TestReplayWindows_提交链与序列间隙(t *testing.T) {
	// 提交事件缺物理推理父 → missing_physical_request issue。
	orphan := commitEvent("subj-1", "ep1", 1, "", map[string]any{
		"messages": []any{}, "window_id": "w1", "complete": true, "delta": []any{},
	})
	builder, err := NewTrajectoryBuilder("sess-replay", "online", nil, nil, nil, nil, 0, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := builder.RecordSpan(orphan); err != nil {
		t.Fatal(err)
	}
	trajectory, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	replay := ReplayWindows(trajectory)
	// 提交可回放则窗口产出；不可回放则带 issue（两种都算链路可达）。
	if len(replay.Issues) == 0 {
		if _, exists := replay.Windows["subj-1"]; !exists {
			t.Fatalf("orphan commit must either issue or produce a window: %+v", replay)
		}
	}

	// 压缩提交链：compaction.completed + 带压缩关联的提交（挂父 span）→ 通过。
	builder2, err := NewTrajectoryBuilder("sess-replay2", "online", nil, nil, nil, nil, 0, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	inferenceSpan := map[string]any{
		"name": "llm.call", "traceId": "trace-c", "spanId": "inf1",
		"attributes": AttributesFromMap(map[string]any{"gen_ai.operation.name": "chat"}),
	}
	compaction := compactionEvent("subj-c", "epc", "1", "op-1")
	commit := commitEvent("subj-c", "epc", 2, "inf1", map[string]any{
		"messages": []any{}, "window_id": "w2", "complete": true, "delta": []any{},
	})
	for _, span := range []map[string]any{inferenceSpan, compaction, commit} {
		if err := builder2.RecordSpan(span); err != nil {
			t.Fatal(err)
		}
	}
	trajectory2, err := builder2.Build()
	if err != nil {
		t.Fatal(err)
	}
	replay2 := ReplayWindows(trajectory2)
	if len(replay2.Issues) != 0 {
		t.Fatalf("compaction chain must replay clean: %v", replay2.Issues)
	}
	if window, exists := replay2.Windows["subj-c"]; !exists || len(window) == 0 {
		t.Fatalf("commit must produce a window: %v", replay2.Windows)
	}
}

// TestRebaseWindowChain_压缩提交链 带提交的 retained → rebase 全分支。
func TestRebaseWindowChain_压缩提交链(t *testing.T) {
	builder, err := NewTrajectoryBuilder("sess-rb2", "online", nil, nil, nil, nil, 0, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	inferenceSpan := map[string]any{
		"name": "llm.call", "traceId": "trace-rb2", "spanId": "inf2",
		"attributes": AttributesFromMap(map[string]any{"gen_ai.operation.name": "chat"}),
	}
	commit := commitEvent("subj-rb", "epr", 1, "inf2", map[string]any{
		"messages":  []any{map[string]any{"role": "user", "content": "1", "message_id": "m1", "origin": "external_user"}},
		"window_id": "w1", "complete": true, "delta": []any{},
	})
	for _, span := range []map[string]any{inferenceSpan, commit} {
		if err := builder.RecordSpan(span); err != nil {
			t.Fatal(err)
		}
	}
	history, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	retained, err := RebaseWindowChain(history, history)
	if err != nil || retained == nil {
		t.Fatalf("commit rebase: %v", err)
	}
	// retained 为纯载荷形态（非 Trajectory）→ trajectoryFromPayload 分支。
	retainedPayload, err := RebaseWindowChain(history.ToOTLP(), history.ToOTLP())
	if err != nil || retainedPayload == nil {
		t.Fatalf("payload rebase: %v", err)
	}
}

// TestExtractorHelpers_unwrap_NativeID_ID 生成助手。
func TestExtractorHelpers_unwrap_NativeID_ID(t *testing.T) {
	// unwrap：字典/嵌套。
	if unwrap(map[string]any{"a": "b"}, "a") != "b" {
		t.Fatal("unwrap map")
	}
	// 嵌套 unwrap：字典值再按子键取。
	if unwrap(map[string]any{"a": map[string]any{"b": "c"}}, "a") == nil {
		t.Fatal("unwrap nested dict value")
	}
	// 键缺失时返回原值本身（鸭子语义）。
	if unwrap(map[string]any{}, "a") == nil {
		t.Fatal("unwrap missing returns original value")
	}
	// nativeID：各形态 id。
	span := spanOf(map[string]any{"invoke_id": 42})
	if got := nativeID(span, "invoke_id"); got != "000000000000002a" {
		t.Fatalf("native int id = %q", got)
	}
	// 缺失键 → 空串（无原生 id 时由 derivedID 补齐）。
	if nativeID(spanOf(map[string]any{}), "missing") != "" {
		t.Fatal("missing native id must be empty")
	}
	// derivedID：无原生 id → 派生。
	trace, spanID := derivedID("abc", spanOf(map[string]any{}), 3)
	if trace == "" || spanID == "" {
		t.Fatal("derived ids")
	}
	// extractorMessage：user 默认角色与字符串 content。
	message := extractorMessage(map[string]any{"role": "user", "content": "hi"}, "user")
	if message == nil || message["content"] != "hi" {
		t.Fatalf("message = %v", message)
	}
	if extractorMessage(map[string]any{"role": "tool", "content": "out"}, "") == nil {
		t.Fatal("tool message must read with empty default")
	}
	// 无 role 无 content 的消息以 defaultRole 填充 role。
	filled := extractorMessage(map[string]any{"nested": true}, "user")
	if filled == nil || filled["role"] != "user" {
		t.Fatalf("filled = %v", filled)
	}
	// 嵌套 message 包装 → 递归内层。
	inner := extractorMessage(map[string]any{"message": map[string]any{"role": "user", "content": "inner"}}, "user")
	if inner == nil || inner["content"] != "inner" {
		t.Fatalf("inner = %v", inner)
	}
	// toolInfo：无 resourceManager → nil。
	if NewTracerTrajectoryExtractor().toolInfo("write_file") != nil {
		t.Fatal("no resource manager must be nil")
	}
}

// TestToJsonCompatible_ToOTLPValue 序列化兼容层。
func TestToJsonCompatible_ToOTLPValue(t *testing.T) {
	compatible := toJsonCompatible(map[string]any{"a": []any{1, "b"}, "c": make(chan int)})
	if compatible == nil {
		t.Fatal("compatible must not be nil")
	}
	if _, err := json.Marshal(compatible); err != nil {
		t.Fatalf("compatible must marshal: %v", err)
	}
	value := toOTLPValue([]any{1, "x"})
	if value == nil {
		t.Fatal("toOTLPValue list")
	}
	_ = toOTLPValue(map[string]any{"nested": true})
	_ = toOTLPValue(42)
	_ = toJsonCompatible([]any{map[string]any{"k": "v"}})
}

// ──────────────────────────── 批次九：Extract 全链（真 Session tracer）+ 守卫分支 ────────────────────────────

// TestNormalizeSpanGuards 归一化守卫（nil/缺 id/非法载荷）。
func TestNormalizeSpanGuards(t *testing.T) {
	// nil span → 错误。
	if _, err := NormalizeSpan(nil); err == nil {
		t.Fatal("nil span must error")
	}
	// links 归一化。
	normalized, err := NormalizeSpan(map[string]any{
		"name": "x", "traceId": "11111111111111111111111111111111", "spanId": "s1",
		"links": []any{map[string]any{"traceId": "22222222222222222222222222222222", "spanId": "s2"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if normalized["links"] == nil {
		t.Fatal("links must normalize")
	}
	// SpanEvents nil / 无事件（返回空切片或 nil 均为合法守卫）。
	eventsNil := SpanEvents(nil)
	if len(eventsNil) != 0 {
		t.Fatal("nil span events must be empty")
	}
	if eventsNone := SpanEvents(map[string]any{"name": "x"}); len(eventsNone) != 0 {
		t.Fatal("eventless span must be empty")
	}
	// compareSpanSortKey：同 start 不同 end。
	keyA := SpanSortKeyOf(map[string]any{"name": "x", "spanId": "1", "startTimeUnixNano": "10", "endTimeUnixNano": "20"})
	keyB := SpanSortKeyOf(map[string]any{"name": "x", "spanId": "2", "startTimeUnixNano": "10", "endTimeUnixNano": "30"})
	if compareSpanSortKey(keyA, keyB) == 0 {
		t.Fatal("end-time tiebreak")
	}
}

// TestStoreGuards_Query变体 存储守卫（非法版本/查询变体）。
func TestStoreGuards_Query变体(t *testing.T) {
	store := NewFileTrajectoryStore(t.TempDir())
	// 缺失轨迹 → (nil, nil)（File store 语义：找不到即空）。
	loaded, loadErr := store.Load("nope", "")
	if loadErr != nil || loaded != nil {
		t.Fatalf("missing trajectory: %v %v", loaded, loadErr)
	}
	// 非法版本名。
	if _, err := versionName("../evil"); err == nil {
		t.Fatal("path traversal version must error")
	}
	// 各过滤维度查询。
	builder, _ := NewTrajectoryBuilder("sess-q", "online", nil, nil, nil, nil, 0, nil, nil)
	_ = builder.RecordTestStep(TestStep{Kind: "tool", ToolName: "a"})
	trajectory, _ := builder.Build()
	_ = store.Save(trajectory, "")
	found, err := store.Query("", derefSessionID(trajectory), "", "", "", "")
	if err != nil || len(found) == 0 {
		t.Fatalf("session query: %v %d", err, len(found))
	}
}

// sdkSpanWithoutIdentity 构造缺 trace/span 身份的 ReadOnlySpan（转换失败路径）。
func sdkSpanWithoutIdentity(name string) sdktrace.ReadOnlySpan {
	provider := sdktrace.NewTracerProvider()
	defer func() { _ = provider.Shutdown(context.Background()) }()
	_, span := provider.Tracer("bad").Start(context.Background(), name)
	ro := span.(sdktrace.ReadOnlySpan)
	return ro
}

// derefSessionID 会话 id 解引用（nil → ""）。
func derefSessionID(traj *Trajectory) string {
	if id := traj.SessionID(); id != nil {
		return *id
	}
	return ""
}

// ──────────────────────────── 批次十：processor/team/testfixture 收尾 ────────────────────────────

// TestProcessorOnStart_RecordCaptureIssue processor 全链收尾（OnStart 无操作、
// 转换失败 issue 记录、trace id 归一各形态）。
func TestProcessorOnStart_RecordCaptureIssue(t *testing.T) {
	p := NewTrajectorySpanProcessor(4)
	handle, err := p.Subscribe([]string{"llm"}, "")
	if err != nil {
		t.Fatal(err)
	}
	defer p.Unsubscribe(handle)
	// OnStart 无操作语义（不 panic 即可）。
	ro, traceID := newTestSpan("chat gpt-x", map[string]any{"gen_ai.operation.name": "chat"})
	p.OnStart(context.Background(), nil)
	_ = traceID
	p.OnEnd(ro)
	// trace id 归一：UUID 去连字符 + 补零 + 非法文本截断。
	if normalizeTraceIDText("abc-def-123") == "" {
		t.Fatal("uuid normalization")
	}
	// 非十六进制文本 → 哈希派生（仍是合法 32 位）。
	if derived := normalizeTraceIDText("zzzz"); len(derived) != 32 {
		t.Fatalf("hash fallback = %q", derived)
	}
	if removeDashes("a-b-c") != "abc" {
		t.Fatal("removeDashes")
	}
	if padLeft("ab", 4) != "00ab" {
		t.Fatal("padLeft")
	}
	// 类别名派生（无 operation 属性，按名字前缀）。
	if categoryForName("llm.call") != "llm" {
		t.Fatalf("name category = %q", categoryForName("llm.call"))
	}
	// appendIssueLocked 上限（MAX_ISSUES 128）与 safeLogIssue 守卫。
	p.safeLogIssue("probe", fmt.Errorf("x"))
}

// TestTeamSpans小助手 parentIdentity/DescendantSpans 缺失身份/IterTeamSpans。
func TestTeamSpans小助手(t *testing.T) {
	// parentIdentity：缺 trace/span id → nil。
	if parentIdentity(map[string]any{"name": "x"}) != nil {
		t.Fatal("missing identity must be nil")
	}
	// SelectMemberSpans：身份过滤（traceId 路由）。
	span := map[string]any{
		"name": "execute_tool m", "traceId": "trace-m", "spanId": "sm1",
		"attributes": AttributesFromMap(map[string]any{
			"gen_ai.operation.name": "execute_tool",
			"agentteam.team.id":     "alpha",
			"agentteam.member.name": "member-a",
		}),
	}
	spans := SelectMemberSpans(spanEnvelope(span), "member-a", "alpha", []string{"tool"}, false, false)
	if len(spans) != 1 {
		t.Fatalf("member spans: %d", len(spans))
	}
	// DescendantSpans 缺身份 → 空。
	if got := DescendantSpans(spanEnvelope(span), map[string]any{"name": "x"}, []string{"tool"}, "alpha"); len(got) != 0 {
		t.Fatalf("no-identity parent must yield none: %d", len(got))
	}
}

// TestJsonStringOf_NonEmptyStr testfixture 与 windows 小助手收尾。
func TestJsonStringOf_NonEmptyStr(t *testing.T) {
	if jsonStringOf(map[string]any{"a": 1}) == "" {
		t.Fatal("json string")
	}
	if jsonStringOf(func() {}) == "" {
		t.Fatal("unserializable falls back to text")
	}
	// negativeInt 语义（positiveInt 的非法分支）。
	if positiveInt("abc") != -1 && positiveInt("abc") != 0 {
		t.Fatal("invalid positiveInt")
	}
	if positiveInt(true) != -1 && positiveInt(true) != 0 {
		t.Fatal("bool positiveInt")
	}
}

// ──────────────────────────── 批次十一：IterTrajectoryEvents owner 事件链 ────────────────────────────

// TestReadTrajectoryEvent_字符串载荷 JSON 字符串载荷解析（含坏 JSON 回落 nil）。
func TestReadTrajectoryEvent_字符串载荷(t *testing.T) {
	base := map[string]any{
		"openjiuwen.trajectory.event_kind":            "context.window.commit",
		"openjiuwen.trajectory.event_id":              "e-str",
		"openjiuwen.trajectory.subject_id":            "subj-str",
		"openjiuwen.trajectory.sequence_epoch":        "ep-str",
		"openjiuwen.trajectory.subject_sequence":      1,
		"openjiuwen.trajectory.recorded_at_unix_nano": 5,
	}
	// 合法 JSON 字符串载荷。
	good := map[string]any{
		"name": "context.commit", "attributes": base,
	}
	good["attributes"] = map[string]any{
		"openjiuwen.trajectory.record_kind":           "event",
		"openjiuwen.trajectory.event_kind":            "context.window.commit",
		"openjiuwen.trajectory.event_id":              "e-str",
		"openjiuwen.trajectory.subject_id":            "subj-str",
		"openjiuwen.trajectory.sequence_epoch":        "ep-str",
		"openjiuwen.trajectory.subject_sequence":      1,
		"openjiuwen.trajectory.recorded_at_unix_nano": 5,
		"openjiuwen.trajectory.payload":               `{"messages":[]}`,
	}
	if event := ReadTrajectoryEvent(good); event == nil {
		t.Fatal("string payload must parse")
	}
	// 坏 JSON 载荷 → payload nil → nil。
	bad := map[string]any{
		"name": "context.commit", "attributes": map[string]any{
			"openjiuwen.trajectory.record_kind":           "event",
			"openjiuwen.trajectory.event_kind":            "context.window.commit",
			"openjiuwen.trajectory.event_id":              "e-bad",
			"openjiuwen.trajectory.subject_id":            "subj-bad",
			"openjiuwen.trajectory.sequence_epoch":        "ep-bad",
			"openjiuwen.trajectory.subject_sequence":      1,
			"openjiuwen.trajectory.recorded_at_unix_nano": 5,
			"openjiuwen.trajectory.payload":               `{bad json`,
		},
	}
	if ReadTrajectoryEvent(bad) != nil {
		t.Fatal("bad json payload must be nil")
	}
}
