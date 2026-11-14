package recommendation

// topic_pipeline_c5_test.go 对齐 topic_pipeline.py:3409-3783 与
// session_history.py:145-243 助手契约的单元用例。

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"
)

// utf8Len Python len() 等价（rune 计数）。
func utf8Len(value string) int { return utf8.RuneCountInString(value) }

// TestIsTerminalCompletionRow 对齐 session_history.py:145-165。
func TestIsTerminalCompletionRow(t *testing.T) {
	completion := map[string]any{"role": "assistant", "feedback_only": true}
	if !IsTerminalCompletionRow("r1", 5, 5, completion, "r1:request_completed", "success", 10.0) {
		t.Fatalf("valid row should close")
	}
	// 非组内最后一条
	if IsTerminalCompletionRow("r1", 4, 5, completion, "r1:request_completed", "success", 10.0) {
		t.Fatalf("non-last row should not close")
	}
	// role 非 assistant
	if IsTerminalCompletionRow("r1", 5, 5, map[string]any{"role": "user", "feedback_only": true},
		"r1:request_completed", "success", 10.0) {
		t.Fatalf("user role should not close")
	}
	// feedback_only 缺失
	if IsTerminalCompletionRow("r1", 5, 5, map[string]any{"role": "assistant"},
		"r1:request_completed", "success", 10.0) {
		t.Fatalf("missing feedback_only should not close")
	}
	// event_id 不匹配
	if IsTerminalCompletionRow("r1", 5, 5, completion, "other", "success", 10.0) {
		t.Fatalf("event id mismatch should not close")
	}
	// 非终态 status
	if IsTerminalCompletionRow("r1", 5, 5, completion, "r1:request_completed", "running", 10.0) {
		t.Fatalf("non-terminal status should not close")
	}
	// 时间戳非数值或负数
	if IsTerminalCompletionRow("r1", 5, 5, completion, "r1:request_completed", "success", "x") {
		t.Fatalf("non-numeric timestamp should not close")
	}
	if IsTerminalCompletionRow("r1", 5, 5, completion, "r1:request_completed", "success", -1.0) {
		t.Fatalf("negative timestamp should not close")
	}
}

// completedRecord 一条会话记录。
func completedRecord(requestID, role, eventType, content string, ordinal int) map[string]any {
	return map[string]any{
		"request_id": requestID, "role": role, "event_type": eventType,
		"content": content, "ordinal": ordinal,
	}
}

// TestProjectCompletedSessionPrefix 对齐 session_history.py:167-243。
func TestProjectCompletedSessionPrefix(t *testing.T) {
	// 成功前缀：r1 完结，r2 未完结
	records := []map[string]any{
		completedRecord("r1", "user", "", "问", 0),
		completedRecord("r1", "assistant", "chat.final", "答", 1),
		{
			"request_id": "r1", "role": "assistant", "event_type": SessionRequestCompletedEventType,
			"id": "r1:request_completed", "status": "success", "timestamp": 10.0, "feedback_only": true,
		},
		completedRecord("r2", "user", "", "再问", 3),
	}
	prefix := ProjectCompletedSessionPrefix("s1", records, true)
	if prefix == nil {
		t.Fatalf("prefix should exist")
	}
	if prefix.Boundary.RequestID != "r1" || prefix.Boundary.Ordinal != 2 {
		t.Fatalf("boundary: %+v", prefix.Boundary)
	}
	if len(prefix.CompletedRequestIDs) != 1 || prefix.CompletedRequestIDs[0] != "r1" {
		t.Fatalf("completed ids: %v", prefix.CompletedRequestIDs)
	}
	if len(prefix.CompletedUserOrdinals) != 1 || prefix.CompletedUserOrdinals[0] != 0 {
		t.Fatalf("user ordinals: %v", prefix.CompletedUserOrdinals)
	}

	// 无 gaps：未完结的 r2 截断 —— allow_gaps 只影响无完结行的组
	prefix2 := ProjectCompletedSessionPrefix("s1", records, false)
	if prefix2 == nil || prefix2.Boundary.RequestID != "r1" {
		t.Fatalf("gaps=false keeps r1 prefix: %v", prefix2)
	}

	// 无任何完结行 → nil
	empty := ProjectCompletedSessionPrefix("s1", records[:2], true)
	if empty != nil {
		t.Fatalf("no completions: %v", empty)
	}

	// 空 session → nil
	if ProjectCompletedSessionPrefix("", records, true) != nil {
		t.Fatalf("empty session id")
	}

	// 重复完结：写序最后一条裁决
	dup := []map[string]any{
		completedRecord("r1", "user", "", "问", 0),
		{"request_id": "r1", "role": "assistant", "event_type": SessionRequestCompletedEventType,
			"id": "r1:request_completed", "status": "error", "timestamp": 5.0, "feedback_only": true},
		{"request_id": "r1", "role": "assistant", "event_type": SessionRequestCompletedEventType,
			"id": "r1:request_completed", "status": "success", "timestamp": 9.0, "feedback_only": true},
	}
	prefix3 := ProjectCompletedSessionPrefix("s1", dup, true)
	if prefix3 == nil || prefix3.Boundary.Status != "success" || prefix3.Boundary.Ordinal != 2 {
		t.Fatalf("last completion wins: %+v", prefix3)
	}

	// 非终态完结行（缺 feedback_only 形状）+ allow_gaps → 跳过该组
	invalid := []map[string]any{
		completedRecord("r1", "user", "", "问", 0),
		{"request_id": "r1", "role": "assistant", "event_type": SessionRequestCompletedEventType,
			"id": "r1:request_completed", "status": "success", "timestamp": 5.0},
		completedRecord("r2", "user", "", "再问", 2),
		{"request_id": "r2", "role": "assistant", "event_type": SessionRequestCompletedEventType,
			"id": "r2:request_completed", "status": "success", "timestamp": 8.0, "feedback_only": true},
	}
	prefix4 := ProjectCompletedSessionPrefix("s1", invalid, true)
	if prefix4 == nil || prefix4.Boundary.RequestID != "r2" {
		t.Fatalf("invalid group skipped with gaps: %+v", prefix4)
	}
	prefix5 := ProjectCompletedSessionPrefix("s1", invalid, false)
	if prefix5 != nil {
		t.Fatalf("invalid group breaks prefix without gaps: %+v", prefix5)
	}
}

// TestSummaryOriginsAndAbstractContext 对齐 :3409-3432。
func TestSummaryOriginsAndAbstractContext(t *testing.T) {
	messages := []*V6SourceMessage{
		{InputOrigin: "manual_free_text", AssistanceOrigin: "none", IntentCluster: "tech-ai-ml"},
		{InputOrigin: "manual_free_text", AssistanceOrigin: "none", IntentCluster: "unmapped"},
	}
	input, assistance := SummaryOrigins(messages)
	if input != "manual_free_text" || assistance != "none" {
		t.Fatalf("single: %s/%s", input, assistance)
	}
	input2, assistance2 := SummaryOrigins([]*V6SourceMessage{
		{InputOrigin: "a", AssistanceOrigin: "none"},
		{InputOrigin: "b", AssistanceOrigin: "auto"},
	})
	if input2 != "mixed" || assistance2 != "mixed" {
		t.Fatalf("mixed: %s/%s", input2, assistance2)
	}
	context := SummaryAbstractContext(messages)
	if len(context) != 1 || context[0] != "tech-ai-ml" {
		t.Fatalf("context: %v", context)
	}
	empty := SummaryAbstractContext([]*V6SourceMessage{{IntentCluster: "unmapped"}})
	if len(empty) != 1 || empty[0] != "general_interest" {
		t.Fatalf("fallback: %v", empty)
	}
}

// TestSelectSummaryTurns 对齐 :3435-3459 预算语义。
func TestSelectSummaryTurns(t *testing.T) {
	turn := func(eventID, content string) SummaryTurn {
		return SummaryTurn{Message: &V6SourceMessage{EventID: eventID, Content: content}, Content: content}
	}
	// 六条各 6000 字（截断后各 1500）：预算 8000 只装得下最新五条
	var turns []SummaryTurn
	for index := 0; index < 6; index++ {
		turns = append(turns, turn(fmt.Sprintf("e%d", index), strings.Repeat("字", 6000)))
	}
	selected := SelectSummaryTurns(turns)
	if len(selected) != 5 {
		t.Fatalf("selected: %d", len(selected))
	}
	// 保持原顺序，且是最新的五条
	if selected[0].Message.EventID != "e1" || selected[4].Message.EventID != "e5" {
		t.Fatalf("order: %v..%v", selected[0].Message.EventID, selected[4].Message.EventID)
	}
	// 截断生效
	if got := selected[0].Content; utf8Len(got) != 1500 {
		t.Fatalf("clipped content len: %d", utf8Len(got))
	}
	if !strings.HasSuffix(selected[0].Content, SummaryTruncationMarker) {
		t.Fatalf("marker: %q", selected[0].Content)
	}
	// 第一条保留轮即使单独超预算也保留（其余为空轮）
	solo := SelectSummaryTurns([]SummaryTurn{
		turn("e9", strings.Repeat("巨", 9000)),
	})
	if len(solo) != 1 || utf8Len(solo[0].Content) != 1500 {
		t.Fatalf("solo over budget kept: %d %d", len(solo), utf8Len(solo[0].Content))
	}
}

// TestClipSummaryText 对齐 :3505-3511。
func TestClipSummaryText(t *testing.T) {
	if got := ClipSummaryText("短文本", 10); got != "短文本" {
		t.Fatalf("short: %q", got)
	}
	got := ClipSummaryText(strings.Repeat("长", 20), 10)
	if utf8Len(got) != 10 || !strings.HasSuffix(got, SummaryTruncationMarker) {
		t.Fatalf("clipped: %q len=%d", got, utf8Len(got))
	}
	if ClipSummaryTextAny(nil, 10) != nil {
		t.Fatalf("nil passthrough")
	}
}

// TestSummaryCoverageAndIdentity 对齐 :3462-3502。
func TestSummaryCoverageAndIdentity(t *testing.T) {
	messages := []*V6SourceMessage{
		{EventID: "e1", Ordinal: 0, Content: "一", ActionType: "natural_question", IntentCluster: "c1",
			InputOrigin: "manual_free_text", AssistanceOrigin: "none"},
		{EventID: "e2", Ordinal: 1, Content: "二", ActionType: "correction", IntentCluster: "c2",
			InputOrigin: "manual_free_text", AssistanceOrigin: "none"},
	}
	coverage := SummaryCoverageIDs(messages)
	if len(coverage) != 2 || !coverage["e1"] || !coverage["e2"] {
		t.Fatalf("coverage: %v", coverage)
	}
	payload := SessionIdentityPayload("s1", messages)
	if payload["session_id"] != "s1" || payload["input_origin"] != "manual_free_text" {
		t.Fatalf("identity: %v", payload)
	}
	pattern := payload["interaction_pattern"].([]any)
	if len(pattern) != 2 || pattern[0] != "natural_question" || pattern[1] != "correction" {
		t.Fatalf("pattern: %v", pattern)
	}
	sessionPayloads := payload["messages"].([]map[string]any)
	if len(sessionPayloads) != 2 || sessionPayloads[0]["event_id"] != "e1" {
		t.Fatalf("messages: %v", sessionPayloads)
	}
}

// TestSessionModelPayload 对齐 :3525-3559 计数字段。
func TestSessionModelPayload(t *testing.T) {
	var messages []*V6SourceMessage
	for index := 0; index < 25; index++ {
		messages = append(messages, &V6SourceMessage{
			EventID: fmt.Sprintf("e%d", index), Ordinal: index,
			Content: strings.Repeat("文", 200), ActionType: "natural_question",
			IntentCluster: "tech-ai-ml", InputOrigin: "manual_free_text", AssistanceOrigin: "none",
		})
	}
	payload := SessionModelPayload("s1", messages)
	if payload["source_turn_count"] != 25 {
		t.Fatalf("source turn count: %v", payload["source_turn_count"])
	}
	if payload["supplied_turn_count"] != SummaryMaxTurnsPerSession {
		t.Fatalf("supplied turn count: %v", payload["supplied_turn_count"])
	}
	if input := payload["input_origin"]; input != "manual_free_text" {
		t.Fatalf("origin: %v", input)
	}
}

// fakeStageCounter 脚本化估算器。
type fakeStageCounter struct {
	values []int
	calls  int
}

func (f *fakeStageCounter) EstimateStageTokens(systemPrompt, userPayload string, reservedOutput int) int {
	value := f.values[min(f.calls, len(f.values)-1)]
	f.calls++
	if systemPrompt == "" || userPayload == "" {
		f.calls--
		panic("empty prompt/payload in fakeStageCounter")
	}
	return value
}

// TestFitSummarySession 对齐 :3599-3639 收缩语义。
func TestFitSummarySession(t *testing.T) {
	snapshot := snapshotForC3Tests()
	payload := map[string]any{
		"session_id": "s1",
		"interaction_pattern": []any{"natural_question", "correction"},
		"messages": []map[string]any{
			{"event_id": "e1", "content": "第一问", "assistant_answer": "第一答"},
			{"event_id": "e2", "content": "第二问", "assistant_answer": "第二答"},
		},
	}
	// 第一次估算超限 → 丢最旧轮；第二次估算达标
	counter := &fakeStageCounter{values: []int{9999, 100}}
	fitted := FitSummarySession(payload, counter, snapshot, 500)
	if fitted == nil {
		t.Fatalf("fitted nil")
	}
	messages := fitted["messages"].([]map[string]any)
	if len(messages) != 1 || messages[0]["event_id"] != "e2" {
		t.Fatalf("dropped oldest: %v", messages)
	}
	pattern := fitted["interaction_pattern"].([]any)
	if len(pattern) != 1 || pattern[0] != "correction" {
		t.Fatalf("pattern trimmed: %v", pattern)
	}

	// 单轮逐半收缩后达标
	counter2 := &fakeStageCounter{values: []int{9999, 9999, 100}}
	fitted2 := FitSummarySession(map[string]any{
		"messages":            []map[string]any{{"event_id": "e1", "content": "很长的问题内容", "assistant_answer": "很长的回答内容"}},
		"interaction_pattern": []any{"natural_question"},
	}, counter2, snapshot, 500)
	if fitted2 == nil {
		t.Fatalf("halving should fit eventually")
	}
	single := fitted2["messages"].([]map[string]any)
	if single[0]["content"] == "很长的问题内容" {
		t.Fatalf("content should be halved: %v", single[0])
	}

	// 内容缩到只剩 1 字仍超限 → nil
	counter3 := &fakeStageCounter{values: []int{9999}}
	none := FitSummarySession(map[string]any{
		"messages":            []map[string]any{{"event_id": "e1", "content": "问", "assistant_answer": ""}},
		"interaction_pattern": []any{"natural_question"},
	}, counter3, snapshot, 5)
	if none != nil {
		t.Fatalf("impossible should return nil")
	}
}

// TestPlanSummaryBatches 对齐 :3642-3673 贪心批。
func TestPlanSummaryBatches(t *testing.T) {
	snapshot := snapshotForC3Tests()
	payload := func(id string) map[string]any {
		return map[string]any{"session_id": id, "messages": []map[string]any{{"content": "x"}}}
	}
	// 估算值驱动：1 个 100、2 个 9999 → 每个一批
	counter := &fakeStageCounter{values: []int{100, 9999, 100}}
	batches := PlanSummaryBatches([]map[string]any{payload("a"), payload("b")}, counter, snapshot, 500)
	if len(batches) != 2 || len(batches[0]) != 1 || batches[0][0] != 0 || batches[1][0] != 1 {
		t.Fatalf("batches: %v", batches)
	}
	// 首个即超限 → nil
	counter2 := &fakeStageCounter{values: []int{9999}}
	if PlanSummaryBatches([]map[string]any{payload("a")}, counter2, snapshot, 500) != nil {
		t.Fatalf("single overflow should fail")
	}
	// 全部装进一批
	counter3 := &fakeStageCounter{values: []int{100, 200}}
	batches3 := PlanSummaryBatches([]map[string]any{payload("a"), payload("b")}, counter3, snapshot, 500)
	if len(batches3) != 1 || len(batches3[0]) != 2 {
		t.Fatalf("one batch: %v", batches3)
	}
}

// TestRecentSummarySessions 对齐 :3676-3703。
func TestRecentSummarySessions(t *testing.T) {
	prefix := func(completedAt float64) *CompletedSessionProjection {
		boundary, err := NewSessionCompletionBoundary("s", "evt", "r", 1, completedAt, "success")
		if err != nil {
			t.Fatalf("boundary: %v", err)
		}
		return &CompletedSessionProjection{Boundary: boundary}
	}
	grouped := map[string][]*V6SourceMessage{
		"recent1": {{EventID: "old1"}}, "recent2": {{EventID: "old2"}},
		"stale": {{EventID: "old3"}}, "delta": {{EventID: "new1"}},
	}
	prefixes := map[string]*CompletedSessionProjection{
		"recent1": prefix(10_000_000.0), "recent2": prefix(9_000_000.0),
		"stale": prefix(1_000_000.0), "delta": prefix(100_000.0),
	}
	got := RecentSummarySessions(grouped, prefixes, []string{"new1"}, 10_000_000.0)
	// stale 超窗被剔除；delta 虽旧但携带增量必须保留
	if len(got) != 3 {
		t.Fatalf("sessions: %v", got)
	}
	joined := strings.Join(got, ",")
	if strings.Contains(joined, "stale") || !strings.Contains(joined, "delta") {
		t.Fatalf("membership: %v", got)
	}
}

// TestRecentRIDSummaries 对齐 :3706-3722。
func TestRecentRIDSummaries(t *testing.T) {
	var summaries []*SessionSummaryArtifact
	for index := 0; index < 35; index++ {
		summaries = append(summaries, summaryForTests(t,
			fmt.Sprintf("summary:s%d:%03d", index, index), fmt.Sprintf("s%d", index), float64(index)))
	}
	got := RecentRIDSummaries(summaries, nil)
	if len(got) != RIDSessionSummaryLimit {
		t.Fatalf("limit: %d", len(got))
	}
	// 最新优先
	if got[0].CompletedAt != 34.0 {
		t.Fatalf("newest first: %v", got[0].CompletedAt)
	}
	// 必留项在limit外也保留
	required := []string{"summary:s0:000"}
	got2 := RecentRIDSummaries(summaries, required)
	found := false
	for _, item := range got2 {
		if item.SessionSummaryID == "summary:s0:000" {
			found = true
		}
	}
	if !found || len(got2) != RIDSessionSummaryLimit {
		t.Fatalf("required kept: %d", len(got2))
	}
	// 少于上限直接透传
	if got3 := RecentRIDSummaries(summaries[:5], nil); len(got3) != 5 {
		t.Fatalf("passthrough: %d", len(got3))
	}
}

// TestRIDRequestEstimatePayload 对齐 :3725-3750 结构键。
func TestRIDRequestEstimatePayload(t *testing.T) {
	binding, _ := PipelineAPIBinding("pa_bridge_fixed_retriever", "ark", "v1", "cfg")
	interest, _ := NewLongTermProfileInterestInput("int-a", GlobalNewChatScope, 1, "cluster", "文本", []string{"unit-1"})
	summary := summaryForTests(t, "summary:s1:aaa", "s1", 10.0)
	payload := RIDRequestEstimatePayload(map[string]any{
		"binding":                     binding,
		"input_snapshot_id":           "snap-1",
		"user_profile":                map[string]any{"k": "v"},
		"long_term_profile_interests": []*LongTermProfileInterestInput{interest},
		"session_summaries":           []*SessionSummaryArtifact{summary},
		"activity_group":              "active",
		"threshold_version":           "tv1",
		"feedback_context":            map[string]any{"fk": "fv"},
		"delta":                       map[string]any{"dk": "dv"},
		"decision_context":            map[string]any{"ck": "cv"},
	})
	if !strings.Contains(payload, `"request_id":"v6-rid-`) {
		t.Fatalf("request id: %s", payload)
	}
	if !strings.Contains(payload, `"prompt_version":"`+RID_API_PROMPT_VERSION+`"`) {
		t.Fatalf("prompt version: %s", payload)
	}
	if !strings.Contains(payload, `"interest_summary_id":"int-a"`) ||
		!strings.Contains(payload, `"session_summary_id":"summary:s1:aaa"`) {
		t.Fatalf("payloads: %s", payload)
	}
	if strings.Contains(payload, `\u`) {
		t.Fatalf("ensure_ascii=False expected: %s", payload)
	}
}

// TestCompletedAnswers 对齐 :3753-3783。
func TestCompletedAnswers(t *testing.T) {
	records := []map[string]any{
		completedRecord("r1", "user", "", "问", 0),
		completedRecord("r1", "assistant", "chat.final", "  答案一  ", 1),
		completedRecord("r1", "assistant", "chat.final", "答案二", 2),
		{"request_id": "r1", "role": "assistant", "event_type": SessionRequestCompletedEventType,
			"id": "r1:request_completed", "status": "success", "timestamp": 10.0, "feedback_only": true},
		completedRecord("r2", "user", "", "再问", 4),
		completedRecord("r2", "assistant", "chat.final", "未完结答案", 5),
	}
	got := CompletedAnswers("s1", records)
	if len(got) != 1 {
		t.Fatalf("only completed requests: %v", got)
	}
	if got["r1"] != "答案一\n\n答案二" {
		t.Fatalf("joined: %q", got["r1"])
	}
	// 失败完结的请求不计
	records2 := []map[string]any{
		completedRecord("r1", "user", "", "问", 0),
		completedRecord("r1", "assistant", "chat.final", "答案", 1),
		{"request_id": "r1", "role": "assistant", "event_type": SessionRequestCompletedEventType,
			"id": "r1:request_completed", "status": "error", "timestamp": 10.0, "feedback_only": true},
	}
	if got2 := CompletedAnswers("s1", records2); len(got2) != 0 {
		t.Fatalf("failed status excluded: %v", got2)
	}
	// 无完结 → 空表
	if got3 := CompletedAnswers("s1", records[:2]); len(got3) != 0 {
		t.Fatalf("no prefix: %v", got3)
	}
}
