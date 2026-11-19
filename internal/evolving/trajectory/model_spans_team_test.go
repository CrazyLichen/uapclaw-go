package trajectory

import (
	"encoding/json"
	"fmt"
	"testing"
)

// mkAttr 构造 OTLP 属性条目。
func mkAttr(key string, value map[string]any) map[string]any {
	return map[string]any{"key": key, "value": value}
}

// strValue 构造 stringValue AnyValue。
func strValue(s string) map[string]any { return map[string]any{"stringValue": s} }

// mkSpan 构造最小规范 span。
func mkSpan(traceID, spanID, name string, attrs map[string]any) map[string]any {
	span := map[string]any{
		"traceId": traceID,
		"spanId":  spanID,
		"name":    name,
	}
	if attrs != nil {
		span["attributes"] = AttributesFromMap(attrs)
	}
	return span
}

// mkPayload 构造单资源单 scope 载荷（自动注入 trajectory_id）。
func mkPayload(spans ...map[string]any) map[string]any {
	spanList := make([]any, 0, len(spans))
	for _, span := range spans {
		spanList = append(spanList, span)
	}
	return map[string]any{
		"resourceSpans": []any{
			map[string]any{
				"resource": map[string]any{"attributes": []any{
					mkAttr(TrajectoryID, strValue("traj-1")),
					mkAttr(SessionID, strValue("sess-1")),
				}},
				"scopeSpans": []any{
					map[string]any{"scope": map[string]any{}, "spans": spanList},
				},
			},
		},
	}
}

// TestTrajectory_构造校验 校验载荷校验三规则。
// Python: model.py Trajectory.__init__。
func TestTrajectory_构造校验(t *testing.T) {
	// 缺 resourceSpans。
	if _, err := NewTrajectory(map[string]any{}); err == nil {
		t.Fatal("empty payload should fail")
	}
	// 缺 trajectory_id。
	_, err := NewTrajectory(map[string]any{"resourceSpans": []any{map[string]any{}}})
	if err == nil {
		t.Fatal("missing trajectory_id should fail")
	}
	// team_id 无 session_id。
	teamPayload := mkPayload(mkSpan("t", "s", "agent.alice", nil))
	resourceSpans := teamPayload["resourceSpans"].([]any)
	resourceSpan := resourceSpans[0].(map[string]any)
	resource := resourceSpan["resource"].(map[string]any)
	resource["attributes"] = []any{mkAttr(TrajectoryID, strValue("traj-1")), mkAttr(TeamID, strValue("team-1"))}
	if _, err := NewTrajectory(teamPayload); err == nil {
		t.Fatal("team_id without session_id should fail")
	}
	// 合法载荷。
	if _, err := NewTrajectory(mkPayload(mkSpan("t", "s", "agent.alice", nil))); err != nil {
		t.Fatalf("valid payload should pass: %v", err)
	}
}

// TestTrajectory_ToOTLP_独立拷贝 修改返回值不影响原对象。
// Python: model.py to_otlp（_copy_json）。
func TestTrajectory_ToOTLP_独立拷贝(t *testing.T) {
	traj, err := NewTrajectory(mkPayload(mkSpan("t", "s1", "agent.alice", nil)))
	if err != nil {
		t.Fatal(err)
	}
	payload := traj.ToOTLP()
	spans := payload["resourceSpans"].([]any)
	scope := spans[0].(map[string]any)["scopeSpans"].([]any)[0].(map[string]any)
	scope["spans"].([]any)[0].(map[string]any)["name"] = "mutated"
	if got := IterSpans(traj)[0]["name"]; got == "mutated" {
		t.Fatal("ToOTLP must return a detached copy")
	}
}

// TestTrajectory_WithResourceAttributes 合并资源属性。
// Python: model.py with_resource_attributes。
func TestTrajectory_WithResourceAttributes(t *testing.T) {
	traj, _ := NewTrajectory(mkPayload(mkSpan("t", "s1", "agent.alice", nil)))
	merged, err := traj.WithResourceAttributes(map[string]any{TeamID: "team-1"})
	if err != nil {
		t.Fatal(err)
	}
	if got := merged.ResourceAttributes()[TeamID]; got != "team-1" {
		t.Fatalf("team_id = %v, want team-1", got)
	}
	if traj.ResourceAttributes()[TeamID] != nil {
		t.Fatal("original must be unchanged")
	}
}

// TestAttributes双向 编码/解码往返。
// Python: spans.py encode_otlp_value / decode_otlp_value。
func TestAttributes双向(t *testing.T) {
	encoded := EncodeOTLPValue(map[string]any{"a": "x", "n": 3, "f": 1.5, "b": true, "arr": []any{"p"}})
	decoded := DecodeOTLPValue(encoded)
	m := decoded.(map[string]any)
	if m["a"] != "x" || m["n"] != 3 || m["b"] != true {
		t.Fatalf("roundtrip mismatch: %#v", m)
	}
	// nil → stringValue ""。
	if got := EncodeOTLPValue(nil)["stringValue"]; got != "" {
		t.Fatalf("nil encode = %v", got)
	}
}

// TestAttributesToMap_列表与映射双形态 两种形态均可解码。
// Python: spans.py attributes_to_map。
func TestAttributesToMap_列表与映射双形态(t *testing.T) {
	list := []any{mkAttr("k", strValue("v"))}
	if got := AttributesToMap(list)["k"]; got != "v" {
		t.Fatalf("list form = %v", got)
	}
	plain := map[string]any{"k": strValue("v")}
	if got := AttributesToMap(plain)["k"]; got != "v" {
		t.Fatalf("mapping form = %v", got)
	}
}

// TestSpanIdentityOf 缺一返 nil。
// Python: spans.py span_identity。
func TestSpanIdentityOf(t *testing.T) {
	if SpanIdentityOf(mkSpan("t", "", "x", nil)) != nil {
		t.Fatal("missing spanId should return nil")
	}
	id := SpanIdentityOf(mkSpan("t", "s", "x", nil))
	if id == nil || id.TraceID != "t" || id.SpanID != "s" {
		t.Fatalf("identity = %+v", id)
	}
}

// TestMergePayloads_身份去重 相同身份只保留首个。
// Python: spans.py merge_payloads。
func TestMergePayloads_身份去重(t *testing.T) {
	a := mkPayload(mkSpan("t", "s1", "agent.alice", nil))
	b := mkPayload(mkSpan("t", "s1", "agent.alice", nil), mkSpan("t", "s2", "llm.call", nil))
	merged, err := MergePayloads(a, b)
	if err != nil {
		t.Fatal(err)
	}
	spans := IterSpans(merged)
	if len(spans) != 2 {
		t.Fatalf("span count = %d, want 2 (dedup by identity)", len(spans))
	}
}

// TestTrimTrajectory_保留空组与元数据 裁剪后结构保留。
// Python: spans.py trim_trajectory。
func TestTrimTrajectory_保留空组与元数据(t *testing.T) {
	payload := mkPayload(mkSpan("t", "s1", "agent.alice", nil), mkSpan("t", "s2", "llm.call", nil))
	traj, err := NewTrajectory(payload)
	if err != nil {
		t.Fatal(err)
	}
	trimmed, err := TrimTrajectory(traj, 1, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(IterSpans(trimmed)) != 1 {
		t.Fatalf("trimmed span count = %d, want 1", len(IterSpans(trimmed)))
	}
	if trimmed.TrajectoryID() != "traj-1" {
		t.Fatal("trajectory_id metadata must survive trim")
	}
}

// TestTrimTrajectory_Uncounted不计名额 uncounted span 不占预算但保留。
// Python: spans.py trim_trajectory uncounted 语义。
func TestTrimTrajectory_Uncounted不计名额(t *testing.T) {
	work := mkSpan("t", "s1", "agent.alice", nil)
	event := mkSpan("t", "s2", "event.x", map[string]any{obsRecordKind(): "event"})
	payload := mkPayload(work, event)
	traj, _ := NewTrajectory(payload)
	trimmed, err := TrimTrajectory(traj, 1, nil, nil, IsTrajectoryEventSpan)
	if err != nil {
		t.Fatal(err)
	}
	if len(IterSpans(trimmed)) != 2 {
		t.Fatalf("event span must be kept alongside the work span, got %d", len(IterSpans(trimmed)))
	}
}

// obsRecordKind 本包测试用的 record kind 键（避免导入环）。
func obsRecordKind() string { return "openjiuwen.trajectory.record.kind" }

// TestSpanCategory_命名前缀表 命名归类全分支。
// Python: team.py span_category。
func TestSpanCategory_命名前缀表(t *testing.T) {
	cases := []struct {
		name string
		want string
	}{
		{"llm.call", "llm"},
		{"llm.reasoning", ""},
		{"tool.write_file", "tool"},
		{"execute_tool x", "tool"},
		{"team.policy", "team"},
		{"agent.alice", "agent"},
		{"task.t-1", "task"},
		{"msg.hello", "message"},
		{"message.hello", "message"},
		{"member.alice", "member"},
		{"plan.a", "plan"},
		{"event.b", "event"},
	}
	for _, c := range cases {
		if got := SpanCategory(mkSpan("t", "s", c.name, nil)); got != c.want {
			t.Fatalf("SpanCategory(%q) = %q, want %q", c.name, got, c.want)
		}
	}
}

// TestSelectMemberSpans_树闭包收后代 成员根 span 的后代全部入选。
// Python: team.py select_member_spans + _descendant_keys。
func TestSelectMemberSpans_树闭包收后代(t *testing.T) {
	attrsAlice := map[string]any{"agentteam.member.name": "alice"}
	root := mkSpan("t", "s3", "agent.alice", attrsAlice)
	llm := mkSpan("t", "s1", "llm.call", attrsAlice)
	llm["parentSpanId"] = "s3"
	tool := mkSpan("t", "s2", "tool.write_file", attrsAlice)
	tool["parentSpanId"] = "s1"
	bobRoot := mkSpan("t", "s4", "agent bob", map[string]any{"agentteam.member.name": "bob"})
	payload := mkPayload(root, llm, tool, bobRoot)
	traj, _ := NewTrajectory(payload)
	selected := SelectMemberSpans(traj, "alice", "", nil, true, false)
	if len(selected) != 3 {
		t.Fatalf("alice spans = %d, want 3 (root + llm + tool descendants)", len(selected))
	}
	bob := SelectMemberSpans(traj, "bob", "", nil, true, false)
	if len(bob) != 1 {
		t.Fatalf("bob spans = %d, want 1", len(bob))
	}
}

// TestSelectTeamSpans_团队trace路由 无团队归属 span 时空集。
// Python: team.py select_team_spans team_id 路由。
func TestSelectTeamSpans_团队trace路由(t *testing.T) {
	attrs := map[string]any{"agentteam.team.id": "team-1"}
	span := mkSpan("t", "s1", "agent.alice", attrs)
	payload := mkPayload(span)
	traj, _ := NewTrajectory(payload)
	if got := SelectTeamSpans(traj, nil, "team-1", ""); len(got) != 1 {
		t.Fatalf("matched = %d, want 1", len(got))
	}
	if got := SelectTeamSpans(traj, nil, "team-other", ""); len(got) != 0 {
		t.Fatalf("unmatched team should route to empty, got %d", len(got))
	}
}

// TestGroupSpansByMember_MemberIDs 分组成员与成员清单。
// Python: team.py group_spans_by_member / member_ids。
func TestGroupSpansByMember_MemberIDs(t *testing.T) {
	attrsAlice := map[string]any{"agentteam.member.name": "alice"}
	attrsBob := map[string]any{"agentteam.member.name": "bob"}
	payload := mkPayload(mkSpan("t", "s1", "agent.alice", attrsAlice), mkSpan("t", "s2", "agent bob", attrsBob))
	traj, _ := NewTrajectory(payload)
	groups := GroupSpansByMember(traj, "", nil)
	if len(groups["alice"]) != 1 || len(groups["bob"]) != 1 {
		t.Fatalf("groups = %#v", groups)
	}
	ids := MemberIDs(traj, "")
	if len(ids) != 2 {
		t.Fatalf("member ids = %v", ids)
	}
}

// TestReadLLMExchange_往返 write → read 往返一致。
// Python: spans.py write_llm_exchange / read_llm_exchange。
func TestReadLLMExchange_往返(t *testing.T) {
	prompts := []map[string]any{{"role": "user", "content": "hi"}}
	completions := []map[string]any{{"role": "assistant", "content": "hello"}}
	attrs := WriteLLMExchange(prompts, completions)
	span := mkSpan("t", "s1", "llm.call", attrs)
	gotPrompts, gotCompletions := ReadLLMExchange(span)
	if len(gotPrompts) != 1 || gotPrompts[0]["content"] != "hi" {
		t.Fatalf("prompts = %#v", gotPrompts)
	}
	if len(gotCompletions) != 1 || gotCompletions[0]["content"] != "hello" {
		t.Fatalf("completions = %#v", gotCompletions)
	}
}

// TestReadToolCall_结构与错误 工具身份/输入/输出/错误读取。
// Python: spans.py read_tool_call / read_span_error。
func TestReadToolCall_结构与错误(t *testing.T) {
	attrs := map[string]any{
		"gen_ai.tool.name":          "write_file",
		"gen_ai.tool.call.id":       "c1",
		"gen_ai.tool.call.arguments": `{"path":"a.txt"}`,
		"gen_ai.tool.call.result":   `{"ok":true}`,
	}
	span := mkSpan("t", "s1", "tool.write_file", attrs)
	result := ReadToolCall(span)
	if result["name"] != "write_file" || result["id"] != "c1" {
		t.Fatalf("tool call = %#v", result)
	}
	if got, ok := result["input"].(map[string]any); !ok || got["path"] != "a.txt" {
		t.Fatalf("input = %#v", result["input"])
	}
	// 无失败信息 → error 缺省。
	if _, hasError := result["error"]; hasError {
		t.Fatal("healthy span should have no error")
	}
}

// TestReadUsage 用量聚合。
// Python: spans.py read_usage。
func TestReadUsage(t *testing.T) {
	attrs := map[string]any{"gen_ai.usage.input_tokens": 10, "gen_ai.usage.output_tokens": 5}
	usage := ReadUsage(mkSpan("t", "s", "llm.call", attrs))
	if usage["prompt_tokens"] != 10 || usage["completion_tokens"] != 5 || usage["total_tokens"] != 15 {
		t.Fatalf("usage = %v", usage)
	}
}

// TestReadSpanError_状态码与异常事件 status ERROR 识别。
// Python: spans.py read_span_error。
func TestReadSpanError_状态码与异常事件(t *testing.T) {
	span := mkSpan("t", "s1", "tool.x", nil)
	span["status"] = map[string]any{"code": "STATUS_CODE_ERROR", "message": "boom"}
	err := ReadSpanError(span)
	if err == nil || err.Status != "STATUS_CODE_ERROR" || err.Message != "boom" {
		t.Fatalf("error = %+v", err)
	}
}

// TestNormalizeMessage_结构化Parts折叠 parts → 扁平形态。
// Python: spans.py _flatten_structured_message（经 read_llm_exchange 生效）。
func TestNormalizeMessage_结构化Parts折叠(t *testing.T) {
	// write 输入契约是扁平消息（Python docstring：build span attributes by hand）。
	flatInput := map[string]any{
		"role":    "assistant",
		"content": "answer",
		"tool_calls": []any{
			map[string]any{"id": "c1", "name": "write_file", "arguments": "{}"},
		},
	}
	attrs := WriteLLMExchange(nil, []map[string]any{flatInput})
	span := mkSpan("t", "s1", "llm.call", attrs)
	_, completions := ReadLLMExchange(span)
	flat := completions[0]
	if flat["content"] != "answer" {
		t.Fatalf("content = %v", flat["content"])
	}
	toolCalls, _ := flat["tool_calls"].([]any)
	firstCall, _ := toolCalls[0].(map[string]any)
	_ = firstCall
	if len(toolCalls) != 1 || firstCall["name"] != "write_file" {
		t.Fatalf("tool_calls = %#v", flat["tool_calls"])
	}
}

// TestReplayWindows_单提交基线 基线提交直接建窗。
// Python: windows.py replay_windows / _replay_subject。
func TestReplayWindows_单提交基线(t *testing.T) {
	windowMessage := map[string]any{
		"message_id": "m1", "role": "user", "origin": "external_user", "content": "hi",
	}
	payload := map[string]any{
		"window_id": "w1", "complete": true, "delta": []any{},
		"messages": []any{windowMessage},
		"transition_kind": "epoch_baseline", "baseline_reason": "runtime_epoch_start",
	}
	attrs := map[string]any{
		"openjiuwen.trajectory.event_id":            "e1",
		"openjiuwen.trajectory.event_kind":          "context.window.commit",
		"openjiuwen.trajectory.subject_id":          "subj-1",
		"openjiuwen.trajectory.sequence_epoch":      "epoch-1",
		"openjiuwen.trajectory.subject_sequence":    0,
		"openjiuwen.trajectory.recorded_at_unix_nano": 1,
		"openjiuwen.trajectory.payload":             mustJSON(payload),
		"openjiuwen.trajectory.record.kind":         "event",
	}
	span := mkSpan("t", "s1", "context.window.commit", attrs)
	traj, _ := NewTrajectory(mkPayload(span))
	replay := ReplayWindows(traj)
	window := replay.Windows["subj-1"]["w1"]
	if len(window) != 1 || window[0]["message_id"] != "m1" {
		t.Fatalf("replayed window = %#v", window)
	}
	if len(replay.Issues) != 0 {
		t.Fatalf("issues = %v", replay.Issues)
	}
}

// mustJSON 紧凑序列化助手。
func mustJSON(value map[string]any) string {
	data, _ := jsonMarshal(value)
	return string(data)
}

// TestProjectTrajectoryMessages_无窗口缺窗报告 无提交窗口 → missing_context_window。
// Python: messages.py project_trajectory_messages。
func TestProjectTrajectoryMessages_无窗口缺窗报告(t *testing.T) {
	prompts := []map[string]any{{"role": "user", "content": "hi"}}
	completions := []map[string]any{{"role": "assistant", "content": "hello"}}
	attrs := WriteLLMExchange(prompts, completions)
	attrs["openjiuwen.request.purpose"] = "chat"
	span := mkSpan("t", "s1", "llm.call", attrs)
	traj, _ := NewTrajectory(mkPayload(span))
	messages, issues, err := ProjectTrajectoryMessages(traj, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 2 {
		t.Fatalf("messages = %d, want 2", len(messages))
	}
	found := false
	for _, issue := range issues {
		if issue["code"] == "missing_context_window" {
			found = true
		}
	}
	if !found {
		t.Fatal("missing_context_window issue expected")
	}
}

// TestTrajectoryToMessages_压缩请求排除 compaction 请求不贡献消息。
// Python: messages.py is_compaction_span 过滤。
func TestTrajectoryToMessages_压缩请求排除(t *testing.T) {
	attrs := WriteLLMExchange([]map[string]any{{"role": "user", "content": "summarize"}}, nil)
	attrs["openjiuwen.request.purpose"] = "compaction"
	span := mkSpan("t", "s1", "llm.call", attrs)
	traj, _ := NewTrajectory(mkPayload(span))
	messages, err := TrajectoryToMessages(traj, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 0 {
		t.Fatalf("compaction must contribute nothing, got %d", len(messages))
	}
}

// TestBuilder_身份去重与滑窗 组装器去重 + maxSpans 滑窗。
// Python: offline/builder.py record_span。
func TestBuilder_身份去重与滑窗(t *testing.T) {
	builder, err := NewTrajectoryBuilder("sess", "offline", nil, nil, nil, nil, 2, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if err := builder.RecordSpan(mkSpan("t", fmtSprint(i), "agent x", nil)); err != nil {
			t.Fatal(err)
		}
	}
	if got := len(builder.Spans()); got != 2 {
		t.Fatalf("spans after window = %d, want 2", got)
	}
	// 重复身份忽略。
	if err := builder.RecordSpan(mkSpan("t", "1", "agent x", nil)); err != nil {
		t.Fatal(err)
	}
	if got := len(builder.Spans()); got != 2 {
		t.Fatalf("duplicate identity must be ignored, got %d", got)
	}
}

// TestBuilder_Build_资源属性 组装产物资源属性齐备。
// Python: offline/builder.py build / _resource_attributes。
func TestBuilder_Build_资源属性(t *testing.T) {
	caseID := "case-1"
	builder, _ := NewTrajectoryBuilder("sess", "offline", &caseID, nil, nil, map[string]any{"member_id": "alice"}, 0, nil, nil)
	if err := builder.RecordSpan(mkSpan("t", "s1", "agent x", nil)); err != nil {
		t.Fatal(err)
	}
	traj, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	attrs := traj.ResourceAttributes()
	if attrs[TrajectoryID] == nil || attrs[CaseID] != "case-1" || attrs[obsMemberName()] != "alice" {
		t.Fatalf("resource attrs = %#v", attrs)
	}
	if traj.SessionID() == nil || *traj.SessionID() != "sess" {
		t.Fatalf("session_id = %v", traj.SessionID())
	}
}

// obsMemberName 测试用成员键。
func obsMemberName() string { return MemberID }

// TestInMemoryStore_SaveLoadQuery 存取与过滤。
// Python: store.py InMemoryTrajectoryStore。
func TestInMemoryStore_SaveLoadQuery(t *testing.T) {
	store := NewInMemoryTrajectoryStore()
	traj, _ := NewTrajectory(mkPayload(mkSpan("t", "s1", "agent.alice", nil)))
	if err := store.Save(traj, ""); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load("traj-1", "")
	if err != nil || loaded == nil || loaded.TrajectoryID() != "traj-1" {
		t.Fatalf("load = %v, %v", loaded, err)
	}
	// source 缺省 offline。
	results, err := store.Query("", "", "", "", "", "offline")
	if err != nil || len(results) != 1 {
		t.Fatalf("query = %d, %v", len(results), err)
	}
}

// jsonMarshal / fmtSprint 测试助手（标准库薄封装）。
func jsonMarshal(value any) ([]byte, error) { return json.Marshal(value) }
func fmtSprint(v any) string                { return fmt.Sprint(v) }
