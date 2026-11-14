package recommendation

// topic_pipeline_c3_test.go 对齐 topic_pipeline.py:2710-2988 助手契约的
// 单元用例：摘要物化小票、空结果、组件绑定/成功小票、高意图映射、scope
// 匹配、短意图触发证据、兴趣文本索引、标识剔除与可摘要用户轮。

import (
	"strings"
	"testing"
)

// snapshotForC3Tests 构造最小 V6ContextSnapshot。
func snapshotForC3Tests() *V6ContextSnapshot {
	return &V6ContextSnapshot{
		State:           map[string]any{},
		ContextScope:    GlobalNewChatScope,
		InputSnapshotID: "snap-1",
		ShortIntentState: &ShortIntentState{
			TriggerReasons:        []string{},
			ReasonEvidenceLineage: []map[string]any{},
		},
	}
}

// TestSessionSummaryMaterializationReceipt_契约 对齐 :2710-2746 的成功与拒绝分支。
func TestSessionSummaryMaterializationReceipt(t *testing.T) {
	snapshot := snapshotForC3Tests()
	summary := summaryForTests(t, "summary:s1:aaa", "s1", 10.0)

	receipt, err := SessionSummaryMaterializationReceipt(snapshot,
		[]*SessionSummaryArtifact{summary}, []string{"summary:s1:aaa"}, "cfg-v1")
	if err != nil {
		t.Fatalf("success path: %v", err)
	}
	if receipt["schema_version"] != SessionSummaryMaterializationReceiptVersion {
		t.Fatalf("schema_version: %v", receipt["schema_version"])
	}
	if receipt["input_snapshot_id"] != "snap-1" || receipt["context_scope"] != GlobalNewChatScope {
		t.Fatalf("snapshot fields: %v", receipt)
	}
	if reused := receipt["reused_session_summary_ids"].([]string); len(reused) != 0 {
		t.Fatalf("reused should be empty: %v", reused)
	}

	// 重复 generated → 拒绝
	if _, err := SessionSummaryMaterializationReceipt(snapshot, []*SessionSummaryArtifact{summary},
		[]string{"summary:s1:aaa", "summary:s1:aaa"}, "cfg-v1"); err == nil {
		t.Fatalf("duplicate generated should reject")
	}
	// generated 超出摘要集 → 拒绝
	if _, err := SessionSummaryMaterializationReceipt(snapshot, []*SessionSummaryArtifact{summary},
		[]string{"summary:other:bbb"}, "cfg-v1"); err == nil {
		t.Fatalf("non-subset generated should reject")
	}
	// 空 config version → 拒绝
	if _, err := SessionSummaryMaterializationReceipt(snapshot, []*SessionSummaryArtifact{summary},
		[]string{"summary:s1:aaa"}, ""); err == nil {
		t.Fatalf("empty config version should reject")
	}
	// 摘要 config 不一致 → 拒绝
	other := summaryForTests(t, "summary:s2:bbb", "s2", 20.0)
	other.SummaryConfigVersion = "cfg-v2"
	if _, mismatchErr := SessionSummaryMaterializationReceipt(snapshot, []*SessionSummaryArtifact{summary, other},
		nil, "cfg-v1"); mismatchErr == nil {
		t.Fatalf("config mismatch should reject")
	} else if reason, ok := mismatchErr.(*TerminalGenerationRejection); !ok || reason.Reason != "session_summary_materialization_contract_failed" {
		t.Fatalf("rejection type: %v", mismatchErr)
	}
}

// TestEmptyPipelineResult 对齐 :2749-2764 默认快照 ID 与失败原因。
func TestEmptyPipelineResult(t *testing.T) {
	result := EmptyPipelineResult(map[string]any{"k": "v"}, "manual", "boom", "")
	if result.FailureReason != "boom" || len(result.Cards) != 0 {
		t.Fatalf("fields: %+v", result)
	}
	if result.Manifest["generation_input_snapshot_id"] != "unavailable" {
		t.Fatalf("default snapshot id: %v", result.Manifest)
	}
	result2 := EmptyPipelineResult(map[string]any{"k": "v"}, "manual", "boom", "snap-9")
	if result2.Manifest["generation_input_snapshot_id"] != "snap-9" {
		t.Fatalf("explicit snapshot id: %v", result2.Manifest)
	}
	if result2.Manifest["trigger_origin"] != "manual" {
		t.Fatalf("origin: %v", result2.Manifest)
	}
}

// TestPipelineAPIBinding_契约版本分发 对齐 :2767-2782 效用/多样性走质量契约。
func TestPipelineAPIBinding(t *testing.T) {
	for _, componentID := range []string{UTILITY_COMPONENT_ID, DIVERSITY_COMPONENT_ID} {
		binding, err := PipelineAPIBinding(componentID, "ark", "v1", "cfg")
		if err != nil {
			t.Fatalf("%s: %v", componentID, err)
		}
		if binding.ContractVersion != "topic-starter-v6-quality-decisions-v2" {
			t.Fatalf("%s contract: %s", componentID, binding.ContractVersion)
		}
	}
	binding, err := PipelineAPIBinding("pa_bridge_fixed_retriever", "ark", "v1", "cfg")
	if err != nil {
		t.Fatalf("fixed retriever: %v", err)
	}
	if binding.ContractVersion != TERMINAL_CONTRACT_VERSION {
		t.Fatalf("default contract: %s", binding.ContractVersion)
	}
}

// TestPipelineSuccessReceipt 对齐 :2785-2798。
func TestPipelineSuccessReceipt(t *testing.T) {
	binding, _ := PipelineAPIBinding(UTILITY_COMPONENT_ID, "ark", "v1", "cfg")
	receipt, err := PipelineSuccessReceipt(binding, "snap-1", "req-1")
	if err != nil {
		t.Fatalf("receipt: %v", err)
	}
	if receipt.ScoreSemantics != "uncalibrated_api_proxy" || receipt.OutputStatus != "success" || receipt.FallbackReason != "" {
		t.Fatalf("fields: %+v", receipt)
	}
}

// TestPipelineHighIntentKind 对齐 :2801-2806。
func TestPipelineHighIntentKind(t *testing.T) {
	if got := PipelineHighIntentKind("explicit_goal_preference_task"); got != "new_goal" {
		t.Fatalf("goal: %s", got)
	}
	if got := PipelineHighIntentKind("correction"); got != "correction" {
		t.Fatalf("correction: %s", got)
	}
	if got := PipelineHighIntentKind("recent_action"); got != "recent_action" {
		t.Fatalf("recent: %s", got)
	}
	if got := PipelineHighIntentKind("other"); got != "" {
		t.Fatalf("unknown: %s", got)
	}
}

// TestPipelineScopeMatches 对齐 :2809-2812。
func TestPipelineScopeMatches(t *testing.T) {
	for _, bucket := range []string{"", "default", "default_code"} {
		if !PipelineScopeMatches(bucket, GlobalNewChatScope) {
			t.Fatalf("bucket %q should match", bucket)
		}
	}
	if PipelineScopeMatches("proj-9", GlobalNewChatScope) {
		t.Fatalf("non-bucket project should not match")
	}
	if !PipelineScopeMatches(nil, GlobalNewChatScope) {
		t.Fatalf("nil project should match empty bucket")
	}
	if !PipelineScopeMatches("p1", "project:p1") {
		t.Fatalf("project scope should match")
	}
	if PipelineScopeMatches("p2", "project:p1") {
		t.Fatalf("project scope mismatch should fail")
	}
}

// lineageFixture 构造血缘条目。
func lineageFixture(eventIDs, current, reference []string) map[string]any {
	toAny := func(values []string) []any {
		out := make([]any, 0, len(values))
		for _, value := range values {
			out = append(out, value)
		}
		return out
	}
	return map[string]any{
		"event_ids":           toAny(eventIDs),
		"current_event_ids":   toAny(current),
		"reference_event_ids": toAny(reference),
	}
}

// TestShortTriggerEvidenceEventIDs 对齐 :2815-2838 event_ids 并集 + current−reference。
func TestShortTriggerEvidenceEventIDs(t *testing.T) {
	lineage := []map[string]any{
		// current−reference 差集："e2"
		lineageFixture([]string{"e1"}, []string{"e2", "e3"}, []string{"e3"}),
	}
	got := ShortTriggerEvidenceEventIDs(lineage)
	if len(got) != 2 || !got["e1"] || !got["e2"] {
		t.Fatalf("evidence: %v", got)
	}
	// 空串事件被过滤
	lineage2 := []map[string]any{{"event_ids": []any{"", "e5"}}}
	got2 := ShortTriggerEvidenceEventIDs(lineage2)
	if len(got2) != 1 || !got2["e5"] {
		t.Fatalf("filtered: %v", got2)
	}
}

// TestShortTriggerNaturalDeltaEventIDs 对齐 :2841-2857 pending 优先与状态回落。
func TestShortTriggerNaturalDeltaEventIDs(t *testing.T) {
	snapshot := snapshotForC3Tests()
	snapshot.Messages = []*V6SourceMessage{
		{EventID: "e1", InputOrigin: "manual_free_text", AssistanceOrigin: "none"},
		{EventID: "e2", InputOrigin: "manual_free_text", AssistanceOrigin: "assist"},
	}
	// pending 血缘优先
	snapshot.PendingShortIntentTriggerLineage = []map[string]any{
		lineageFixture([]string{"e1", "e2"}, nil, nil),
	}
	got := ShortTriggerNaturalDeltaEventIDs(snapshot)
	if len(got) != 1 || !got["e1"] {
		t.Fatalf("pending lineage natural filter: %v", got)
	}
	// pending nil + 有触发原因 → 回落状态血缘
	snapshot.PendingShortIntentTriggerLineage = nil
	snapshot.ShortIntentState.TriggerReasons = []string{"why"}
	snapshot.ShortIntentState.ReasonEvidenceLineage = []map[string]any{
		lineageFixture([]string{"e2"}, nil, nil),
	}
	got2 := ShortTriggerNaturalDeltaEventIDs(snapshot)
	if len(got2) != 0 {
		t.Fatalf("non-natural e2 should drop: %v", got2)
	}
	// 无触发原因且无 pending → 空
	snapshot.ShortIntentState.TriggerReasons = []string{}
	if got3 := ShortTriggerNaturalDeltaEventIDs(snapshot); len(got3) != 0 {
		t.Fatalf("no reasons: %v", got3)
	}
}

// TestExactDeltaLinkedInterestIDs 对齐 :2860-2870 事件与 locator 双通道命中。
func TestExactDeltaLinkedInterestIDs(t *testing.T) {
	rid := &RIDResult{Decisions: []*RIDInterestDecision{
		{
			InterestSummaryID: "int-a", Selected: true, Rank: 1,
			SourceEventIDs: []string{"e1"},
		},
		{
			InterestSummaryID: "int-b", Selected: true, Rank: 2,
			SourceLocators: []*SessionSourceLocator{{SessionID: "s1", EventID: "e3", Ordinal: 0}},
		},
		{
			InterestSummaryID: "int-c", Selected: true, Rank: 3,
			SourceEventIDs: []string{"e9"},
		},
	}}
	got := ExactDeltaLinkedInterestIDs(rid, map[string]bool{"e1": true, "e3": true})
	if len(got) != 2 || !got["int-a"] || !got["int-b"] {
		t.Fatalf("linked: %v", got)
	}
	if got["int-c"] {
		t.Fatalf("unrelated interest should not link")
	}
}

// TestInterestSummaryTextByID 对齐 :2873-2886 摘要覆盖画像同 ID。
func TestInterestSummaryTextByID(t *testing.T) {
	snapshot := snapshotForC3Tests()
	interest, err := NewLongTermProfileInterestInput("int-a", GlobalNewChatScope, 1,
		"cluster", "画像文本", []string{"unit-1"})
	if err != nil {
		t.Fatalf("interest: %v", err)
	}
	snapshot.LongTermProfileInterests = []*LongTermProfileInterestInput{interest}
	summary := summaryForTests(t, "int-a", "s1", 10.0)
	got := InterestSummaryTextByID(snapshot, []*SessionSummaryArtifact{summary})
	if got["int-a"] != "摘要内容" {
		t.Fatalf("summary should override profile: %q", got["int-a"])
	}
}

// TestWithoutIdentifiers 对齐 :2955-2959 长标识符优先与分隔符剔除。
func TestWithoutIdentifiers(t *testing.T) {
	got := WithoutIdentifiers("ab-long-id and ab short", []string{"ab-long-id", "ab"})
	if strings.Contains(got, "long-id") || strings.Contains(got, "  ") {
		t.Fatalf("normalize: %q", got)
	}
	got2 := WithoutIdentifiers("，标题：内容；", nil)
	if got2 != "标题：内容" {
		t.Fatalf("trim separators: %q", got2)
	}
	got3 := WithoutIdentifiers(nil, nil)
	if got3 != "" {
		t.Fatalf("nil value: %q", got3)
	}
}

// TestUnitSourceSessionIDs 对齐 :2962-2974 locator 形状过滤。
func TestUnitSourceSessionIDs(t *testing.T) {
	companion := map[string]any{
		"canonical_source_locators": []any{
			[]any{"s1", 2},
			[]any{"  ", 3},
			[]any{"s2", 1, "extra"},
			"not-a-list",
		},
	}
	got := UnitSourceSessionIDs(companion)
	if len(got) != 1 || !got["s1"] {
		t.Fatalf("sessions: %v", got)
	}
	got2 := UnitSourceSessionIDs(map[string]any{})
	if len(got2) != 0 {
		t.Fatalf("empty: %v", got2)
	}
}

// TestSummarizableUserTurn 对齐 :2977-2988 五个分支。
func TestSummarizableUserTurn(t *testing.T) {
	if !SummarizableUserTurn(map[string]any{"content": " hello "}) {
		t.Fatalf("no origin keys should pass")
	}
	if SummarizableUserTurn(map[string]any{"content": "   "}) {
		t.Fatalf("blank content should fail")
	}
	if SummarizableUserTurn(map[string]any{"input_origin": "x"}) {
		t.Fatalf("missing content should fail")
	}
	if !SummarizableUserTurn(map[string]any{"content": "hi", "input_origin": "manual_free_text", "assistance_origin": "none"}) {
		t.Fatalf("natural input should pass")
	}
	if !SummarizableUserTurn(map[string]any{"content": "hi", "assistance_origin": "auto_complete"}) {
		t.Fatalf("attributed assistance should pass")
	}
	for _, origin := range []string{"", "none", "unknown"} {
		if SummarizableUserTurn(map[string]any{"content": "hi", "input_origin": "synthetic", "assistance_origin": origin}) {
			t.Fatalf("origin %q should fail", origin)
		}
	}
}
