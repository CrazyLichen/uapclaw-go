package recommendation

// topic_pipeline_c6_test.go 对齐 topic_pipeline.py:2889-2952/:3292-3383/
// :4334-4478 契约的单元用例。

import (
	"fmt"
	"strings"
	"testing"
)

// TestAttachInterestSources 对齐 :2889-2952。
func TestAttachInterestSources(t *testing.T) {
	snapshot := snapshotForC3Tests()
	snapshot.State = map[string]any{
		"memory": map[string]any{
			"units": []any{
				map[string]any{
					"companion": map[string]any{
						"unit_id": "unit-1",
						"canonical_source_locators": []any{
							[]any{"s1", 0}, []any{"s9", 0}, // s9 不在本 scope 消息内
						},
					},
				},
			},
		},
	}
	snapshot.Messages = []*V6SourceMessage{
		{EventID: "e1", SessionID: "s1", Ordinal: 0, Content: "我要做大模型评测",
			InputOrigin: "manual_free_text", AssistanceOrigin: "none"},
	}
	interest, err := NewLongTermProfileInterestInput("int-a", GlobalNewChatScope, 1,
		"cluster", "画像文本", []string{"unit-1"})
	if err != nil {
		t.Fatalf("interest: %v", err)
	}
	snapshot.LongTermProfileInterests = []*LongTermProfileInterestInput{interest}

	boundary, _ := NewSessionCompletionBoundary("s1", "e1", "req", 1, 10.0, "success")
	summary, err := NewSessionSummaryArtifact("summary:s1:aaa", "s1", GlobalNewChatScope,
		"摘要内容", "manual_free_text", "none", []string{"e1"},
		[]*SessionSourceLocator{{SessionID: "s1", Ordinal: 0, EventID: "e1"}},
		boundary, 10.0, "provider", "v1", "cfg-v1")
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	feedback := &InterestFeedback{
		EvidenceByUnit: map[string]map[string]bool{"unit-1": {"s1:e1": true}},
		SessionsByUnit: map[string]map[string]bool{"unit-1": {"s1": true}},
	}
	plan := map[string]any{
		"interest_summary_id": "summary:s1:aaa",
		"persona_context":     "初始",
	}
	AttachInterestSources([]map[string]any{plan}, snapshot,
		[]*SessionSummaryArtifact{summary}, feedback)

	persona := plan["persona_context"].(string)
	if !strings.Contains(persona, "所属长期兴趣：画像文本") {
		t.Fatalf("profile text appended: %q", persona)
	}
	if !strings.Contains(persona, "用户原话：\n我要做大模型评测") {
		t.Fatalf("user statement appended: %q", persona)
	}
	locators := plan["source_locators"].([][]any)
	// 摘要 locator {s1,0} 与单元 locator {s1,0} 去重合并；s9 不在 scope 会话内被剔
	if len(locators) != 1 || pyStr(locators[0][0]) != "s1" || pyIntOf(locators[0][1]) != 0 {
		t.Fatalf("locators: %v", locators)
	}

	// 单元来源的卡：unit_ids 显式给出，不再走 holding_units
	unitPlan := map[string]any{
		"source_memory_unit_ids": []any{"unit-1"},
		"persona_context":        "单元卡",
	}
	AttachInterestSources([]map[string]any{unitPlan}, snapshot, nil, feedback)
	unitLocators := unitPlan["source_locators"].([][]any)
	if len(unitLocators) != 1 || pyStr(unitLocators[0][0]) != "s1" {
		t.Fatalf("unit locators: %v", unitLocators)
	}
	if strings.Contains(unitPlan["persona_context"].(string), "所属长期兴趣") {
		t.Fatalf("unit card should not append profile text")
	}
}

// TestRidDecisionContext 对齐 :3292-3383。
func TestRidDecisionContext(t *testing.T) {
	snapshot := snapshotForC3Tests()
	for index := 0; index < 12; index++ {
		snapshot.Messages = append(snapshot.Messages, &V6SourceMessage{
			EventID: fmt.Sprintf("e%d", index), SessionID: "s1", Ordinal: index,
			ObservedAt:  1000.0 + float64(index),
			InputOrigin: "manual_free_text", AssistanceOrigin: "none",
			IntentCluster: "tech-ai-ml", ActionType: "natural_question",
			Content: "问题内容",
		})
	}
	feedback := &InterestFeedback{}
	context := RidDecisionContext(snapshot, nil, feedback, 2000.0)
	if context["activity_group"] != "active" {
		t.Fatalf("activity group: %v", context["activity_group"])
	}
	if context["natural_event_count"] != 12 {
		t.Fatalf("count: %v", context["natural_event_count"])
	}
	if context["activity_group_method"] != "natural_events_last_30_days_ge_10_v1" {
		t.Fatalf("method: %v", context["activity_group_method"])
	}
	recent := context["recent_natural_feedback"].([]map[string]any)
	if len(recent) != 12 {
		t.Fatalf("recent: %d", len(recent))
	}
	if recent[0]["age_seconds"] != 1000.0 {
		t.Fatalf("age: %v", recent[0]["age_seconds"])
	}
	if recent[0]["content"] != ClipSummaryText("问题内容", RIDFeedbackMessageChars) {
		t.Fatalf("content: %v", recent[0]["content"])
	}

	// 少于 10 条 → steady
	small := snapshotForC3Tests()
	for index := 0; index < 3; index++ {
		small.Messages = append(small.Messages, &V6SourceMessage{
			EventID: fmt.Sprintf("e%d", index), SessionID: "s1", Ordinal: index,
			ObservedAt: 1000.0, InputOrigin: "manual_free_text", AssistanceOrigin: "none",
		})
	}
	context2 := RidDecisionContext(small, nil, feedback, 2000.0)
	if context2["activity_group"] != "steady" {
		t.Fatalf("steady: %v", context2["activity_group"])
	}

	// 短增量要求：血缘事件 e0 既是自然消息又落在摘要源事件里
	boundary, _ := NewSessionCompletionBoundary("s1", "e0", "req", 1, 10.0, "success")
	summary, _ := NewSessionSummaryArtifact("summary:s1:aaa", "s1", GlobalNewChatScope,
		"摘要内容", "manual_free_text", "none", []string{"e0"},
		[]*SessionSourceLocator{{SessionID: "s1", Ordinal: 0, EventID: "e0"}},
		boundary, 10.0, "provider", "v1", "cfg-v1")
	snapshot.PendingShortIntentTriggerLineage = []map[string]any{
		lineageFixture([]string{"e0"}, nil, nil),
	}
	context3 := RidDecisionContext(snapshot, []*SessionSummaryArtifact{summary}, feedback, 2000.0)
	if context3["short_delta_lineage_required"] != true {
		t.Fatalf("delta required: %v", context3)
	}
	if ids := context3["required_short_delta_interest_summary_ids"].([]string); len(ids) != 1 ||
		ids[0] != "summary:s1:aaa" {
		t.Fatalf("required ids: %v", context3["required_short_delta_interest_summary_ids"])
	}
}

// TestWantedProfileInterests 对齐 :4378-4390。
func TestWantedProfileInterests(t *testing.T) {
	snapshot := snapshotForC3Tests()
	kept, _ := NewLongTermProfileInterestInput("int-keep", GlobalNewChatScope, 1, "c", "留", []string{"unit-keep"})
	dropped, _ := NewLongTermProfileInterestInput("int-drop", GlobalNewChatScope, 1, "c", "去", []string{"unit-drop"})
	snapshot.LongTermProfileInterests = []*LongTermProfileInterestInput{kept, dropped}
	feedback := &InterestFeedback{
		EvidenceByUnit: map[string]map[string]bool{},
		SessionsByUnit: map[string]map[string]bool{"unit-drop": {"s1": true}},
	}
	// unit-drop 的否定经 following_unit 生效：source_id=unit-drop 的否定记录
	// 落在 unit 键下（evidence_session_ids 命中）
	record := map[string]any{
		"reason": "explicit_not_interested", "context_scope": "global_new_chat",
		"project_id": nil, "record_id": "rec-1", "dismissal_kind": "pause",
		"dismissed_at": 100.0, "source_id": "unit-drop",
		"evidence_session_ids": []any{"s1"},
	}
	feedback.NegativePreferences = []map[string]any{record}
	wanted := WantedProfileInterests(snapshot, feedback)
	if len(wanted) != 1 || wanted[0].InterestSummaryID != "int-keep" {
		t.Fatalf("wanted: %+v", wanted)
	}
}

// TestWantedSessionSummaries 对齐 :4392-4411。
func TestWantedSessionSummaries(t *testing.T) {
	snapshot := snapshotForC3Tests() // global_new_chat
	summary := summaryForTests(t, "summary:s1:aaa", "s1", 10.0)
	feedback := &InterestFeedback{
		EvidenceByUnit: map[string]map[string]bool{},
		SessionsByUnit: map[string]map[string]bool{},
	}
	// as_written_events 直接扣住摘要源事件（memory_evidence_id 形态）
	feedback.NegativePreferences = []map[string]any{
		{"reason": "explicit_not_interested", "context_scope": "global_new_chat",
			"project_id": nil, "record_id": "rec-1", "source_event_ids": []any{"s1:evt"}},
	}
	wanted := WantedSessionSummaries(snapshot, []*SessionSummaryArtifact{summary}, feedback)
	if len(wanted) != 0 {
		t.Fatalf("withheld summary dropped: %d", len(wanted))
	}
	// 项目页直接透传
	projectSnapshot := snapshotForC3Tests()
	projectSnapshot.ContextScope = "project:p1"
	wanted2 := WantedSessionSummaries(projectSnapshot, []*SessionSummaryArtifact{summary}, feedback)
	if len(wanted2) != 1 {
		t.Fatalf("project passthrough: %d", len(wanted2))
	}
}

// TestInterestsBehindDismissals 对齐 :4413-4424。
func TestInterestsBehindDismissals(t *testing.T) {
	feedback := &InterestFeedback{}
	feedback.NegativePreferences = []map[string]any{
		{"reason": "explicit_not_interested", "context_scope": "global_new_chat",
			"project_id": nil, "record_id": "rec-1", "source_event_ids": []any{"s1:evt"}},
	}
	interests := []*RIDInterestDecision{
		{InterestSummaryID: "int-a", SourceLocators: []*SessionSourceLocator{
			{SessionID: "s1", EventID: "evt", Ordinal: 0}}},
		{InterestSummaryID: "int-b", SourceLocators: []*SessionSourceLocator{
			{SessionID: "s1", EventID: "other", Ordinal: 1}}},
	}
	withheld := InterestsBehindDismissals(feedback, interests)
	if withheld["int-a"] != true || withheld["int-b"] == true {
		t.Fatalf("withheld: %v", withheld)
	}
}

// TestProviderMatchesTrigger 对齐 :4426-4437。
func TestProviderMatchesTrigger(t *testing.T) {
	binding, _ := PipelineAPIBinding("pa_bridge_fixed_retriever", "ark", "v9", "cfg")
	receipt, _ := PipelineSuccessReceipt(binding, "snap", "req")
	trigger := map[string]any{
		"component_contract_versions": map[string]any{
			"pa_bridge_fixed_retriever": TERMINAL_CONTRACT_VERSION,
		},
		"model": "v9",
	}
	if !ProviderMatchesTrigger(trigger, "pa_bridge_fixed_retriever", receipt) {
		t.Fatalf("match")
	}
	trigger["model"] = "v8"
	if ProviderMatchesTrigger(trigger, "pa_bridge_fixed_retriever", receipt) {
		t.Fatalf("provider mismatch should fail")
	}
}

// TestComponentInvocationReceipt 对齐 :4439-4478。
func TestComponentInvocationReceipt(t *testing.T) {
	binding, _ := PipelineAPIBinding("pa_bridge_fixed_retriever", "ark", "v9", "cfg")
	receipt, _ := PipelineSuccessReceipt(binding, "snap", "req")
	invocation := ComponentInvocationReceipt(receipt, "candidate",
		[]string{"cand-1"}, []string{})
	if invocation["schema_version"] != ComponentInvocationReceiptVersion {
		t.Fatalf("version: %v", invocation["schema_version"])
	}
	if invocation["component_id"] != "pa_bridge_fixed_retriever" || invocation["binding_scope"] != "candidate" {
		t.Fatalf("fields: %v", invocation)
	}
	// all_cards
	if !ComponentInvocationAppliesToCard(map[string]any{"binding_scope": "all_cards"},
		map[string]any{}) {
		t.Fatalf("all cards")
	}
	// candidate 命中
	if !ComponentInvocationAppliesToCard(invocation, map[string]any{"card_id": "cand-1"}) {
		t.Fatalf("candidate hit")
	}
	if ComponentInvocationAppliesToCard(invocation, map[string]any{"card_id": "other"}) {
		t.Fatalf("candidate miss")
	}
	// starter
	starterInvocation := ComponentInvocationReceipt(receipt, "starter", nil, []string{"st-1"})
	if !ComponentInvocationAppliesToCard(starterInvocation, map[string]any{"source_starter_id": "st-1"}) {
		t.Fatalf("starter hit")
	}
	if ComponentInvocationAppliesToCard(map[string]any{"binding_scope": "unknown"}, map[string]any{}) {
		t.Fatalf("unknown scope")
	}
}

// TestFlattenEvidence 对齐 :4334-4367。
func TestFlattenEvidence(t *testing.T) {
	binding, _ := PipelineAPIBinding("verbatim_web_evidence", "ark", "v9", "cfg")
	receipt, _ := PipelineSuccessReceipt(binding, "snap", "req")
	bundle := &EvidenceBundle{
		CandidateID: "cand-1", SnapshotDate: "2026-11-13", SnapshotVersion: "v1",
		SourceGateMode: "frozen",
		Spans: []ExactEvidenceSpan{{
			CandidateID: "cand-1", DocumentRevisionID: "doc-1",
			SourceURL: "https://example.com/a", SourceKind: "stable", Publisher: "例站",
			RetrievedAt: 1763000000, Start: 0, End: 5, ExactText: "证据文本",
			PublishedAt: "2026-11-01",
			PublicationDateProvenance: &PublicationDateProvenance{
				SourceType: "page_explicit_metadata", SourceField: "published_at",
			},
		}},
		ComponentReceipt: receipt,
	}
	payload := FlattenEvidence(bundle)
	if payload["candidate_id"] != "cand-1" || payload["fetch_status"] != "success" {
		t.Fatalf("fields: %v", payload)
	}
	boundary := payload["content_boundary"].(map[string]any)
	if boundary["text"] != "证据文本" {
		t.Fatalf("boundary: %v", boundary)
	}
	if payload["published_at_source"] != "page_explicit_metadata:published_at" {
		t.Fatalf("provenance source: %v", payload["published_at_source"])
	}
	if _, present := payload["publication_date_provenance"]; !present {
		t.Fatalf("provenance payload missing")
	}
	if !strings.Contains(payload["retrieved_at"].(string), "T") {
		t.Fatalf("iso format: %v", payload["retrieved_at"])
	}
	// 无发布时间 → 不带 published_at 键
	bundle.Spans[0].PublishedAt = ""
	payload2 := FlattenEvidence(bundle)
	if _, present := payload2["published_at"]; present {
		t.Fatalf("no published_at expected")
	}
}
