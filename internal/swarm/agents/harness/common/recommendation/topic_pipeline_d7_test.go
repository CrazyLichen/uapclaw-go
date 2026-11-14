package recommendation

// topic_pipeline_d7_test.go 对齐 topic_pipeline.py:6345-7097 契约的单元用例。

import (
	"testing"
)

// d7Interest 构造 RID 决策。
func d7Interest(id string, units []string, events []string, rank int) *RIDInterestDecision {
	decision := &RIDInterestDecision{InterestSummaryID: id, Selected: true, Rank: rank}
	decision.SourceProfileUnitIDs = units
	for _, event := range events {
		parts := event
		decision.SourceLocators = append(decision.SourceLocators, &SessionSourceLocator{
			SessionID: parts, Ordinal: 0, EventID: "evt-" + parts})
	}
	return decision
}

// TestValidateGlobalInterestCandidates 对齐 :6896-6977。
func TestValidateGlobalInterestCandidates(t *testing.T) {
	snapshot := &V6ContextSnapshot{
		ContextScope: GlobalNewChatScope, InputSnapshotID: "snap-7",
	}
	ranked := []RankedInterest{{InterestSummaryID: "int-a", Rank: 1}}
	summariesByID := map[string]string{"int-a": "想学大模型"}
	slots := []map[string]any{
		{"slot_id": "slot-1", "interest_summary_id": "int-a"},
	}
	// 写下 → 计划；skip → 无计划
	response := map[string]any{"candidates": []any{
		map[string]any{"slot_id": "slot-1", "interest_summary_id": "int-a",
			"positions_left": 0, "direction": "评测", "blocked_by": "",
			"new_angle": "", "decision": "write", "intent": "想学",
			"title": "标题"},
	}}
	plans, err := ValidateGlobalInterestCandidates(response, snapshot, ranked,
		summariesByID, map[string][]string{"int-a": {"unit-1"}}, slots, 100.0)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if len(plans) != 1 || plans[0]["candidate_id"] != "slot-1" ||
		plans[0]["category"] != ProfileInterestCategory {
		t.Fatalf("plans: %v", plans)
	}
	// skip
	response["candidates"].([]any)[0].(map[string]any)["decision"] = "skip"
	plans, err = ValidateGlobalInterestCandidates(response, snapshot, ranked,
		summariesByID, map[string][]string{}, slots, 100.0)
	if err != nil || len(plans) != 0 {
		t.Fatalf("skip: %v %v", plans, err)
	}
	// 重复槽 → 丢弃计数但不失败
	response["candidates"].([]any)[0].(map[string]any)["decision"] = "write"
	response["candidates"] = append(response["candidates"].([]any),
		response["candidates"].([]any)[0])
	plans, err = ValidateGlobalInterestCandidates(response, snapshot, ranked,
		summariesByID, map[string][]string{}, slots, 100.0)
	if err != nil || len(plans) != 1 {
		t.Fatalf("dup slot: %v %v", plans, err)
	}
	// 空方向 write → 丢弃
	response["candidates"].([]any)[0].(map[string]any)["direction"] = "  "
	plans, err = ValidateGlobalInterestCandidates(response, snapshot, ranked,
		summariesByID, map[string][]string{}, slots, 100.0)
	if err != nil || len(plans) != 0 {
		t.Fatalf("blank direction: %v %v", plans, err)
	}
}

// TestValidateRecentCandidates 对齐 :6980-7097。
func TestValidateRecentCandidates(t *testing.T) {
	ranked := []RankedInterest{{InterestSummaryID: "int-a", Rank: 1}}
	summariesByID := map[string]string{"int-a": "想学大模型"}
	slots := []map[string]any{
		{"slot_id": "slot-1", "interest_summary_id": "int-a"},
	}
	response := map[string]any{"candidates": []any{
		map[string]any{"slot_id": "slot-1", "interest_summary_id": "int-a",
			"identity_anchor": "想学大模型", "intent": "跟进", "title": "新进展",
			"primary_query": "大模型 评测", "freshness_class": "recent_development",
			"direction": "评测方法"},
	}}
	plans, err := ValidateRecentCandidates(response, ranked, summariesByID,
		map[string][]string{"int-a": {"unit-1"}}, GlobalNewChatScope,
		"profile_recent_progress", 0, slots)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if len(plans) != 1 || plans[0]["direction"] != "评测方法" ||
		plans[0]["interest_summary"] != "想学大模型" {
		t.Fatalf("plans: %v", plans)
	}
	// 占位查询丢弃
	response["candidates"].([]any)[0].(map[string]any)["primary_query"] = "null"
	plans, err = ValidateRecentCandidates(response, ranked, summariesByID,
		map[string][]string{}, GlobalNewChatScope, "profile_recent_progress", 0, slots)
	if err != nil || len(plans) != 0 {
		t.Fatalf("placeholder: %v %v", plans, err)
	}
	// 低于 minimum → shortfall（1 计划 < 2）
	if _, err := ValidateRecentCandidates(response, ranked, summariesByID,
		map[string][]string{}, GlobalNewChatScope, "profile_recent_progress", 2, slots); err == nil ||
		err.(*TerminalGenerationRejection).Reason != "recent_candidate_shortfall" {
		t.Fatalf("shortfall: %v", err)
	}
}

// TestRetainedCandidatePlans 对齐 :6694-6798。
func TestRetainedCandidatePlans(t *testing.T) {
	store := newTestStore(t)
	pipeline := NewTopicPipeline(store, nil, nil, nil, nil, "", nil, nil, nil, nil, nil,
		func() float64 { return 1000.0 })
	snapshot := &V6ContextSnapshot{
		ContextScope: GlobalNewChatScope, InputSnapshotID: "snap-7",
		ProjectID: nil,
	}
	feedback := &InterestFeedback{EvidenceByUnit: map[string]map[string]bool{},
		SessionsByUnit: map[string]map[string]bool{}}
	rid := &RIDResult{Decisions: []*RIDInterestDecision{d7Interest("int-a", []string{"unit-1"}, nil, 1)}}
	triggerKey := map[string]any{"k": "v"}
	summariesByID := map[string]string{"int-a": "想学大模型"}
	// 池为空 → 无携带
	if plans := pipeline.RetainedCandidatePlans(snapshot, rid, feedback, triggerKey,
		summariesByID, 100.0); len(plans) != 0 {
		t.Fatalf("empty pool: %v", plans)
	}
	// 兼容池里一张未打开画像兴趣卡 → 携带
	servedKey := BuildServingCompatibilityKey(triggerKey)
	poolManifest := map[string]any{
		"manifest_id": "m-7", "generation_batch_id": "b-7",
		"card_revision_ids":           []any{"rev-7"},
		"serving_compatibility_key":   servedKey,
		"context_scope":               GlobalNewChatScope,
		"negative_preference_version": 0, "cold_start": true,
	}
	cardPayload := map[string]any{
		"card_id": "cand-7", "card_revision_id": "rev-7",
		"generation_batch_id": "b-7", "category": ProfileInterestCategory,
		"interest_summary_id": "int-a", "intent": "想学", "title": "标题",
		"prompt": "提示", "expire_time": 100.0 + 400*24*60*60,
		"source_memory_unit_ids": []any{"unit-1"}, "cold_start": true,
		"context_scope": GlobalNewChatScope,
		"evidence_bundle": map[string]any{
			"fetch_status": "success", "source_url": "https://e", "source_kind": "stable",
			"document_revision_id": "doc-7",
			"content_boundary":     map[string]any{"text": "证据", "start": 0, "end": 2},
		},
	}
	if ok, _ := store.RecordEvent(map[string]any{
		"session_id": "s1", "slate_id": "m-7", "event_type": "starter_slate_manifest",
		"topic_id": "m-7", "policy_id": POLICY_ID, "payload": poolManifest,
		"created_at": 10.0,
	}); !ok {
		t.Fatalf("record manifest")
	}
	if ok, _ := store.RecordEvent(map[string]any{
		"session_id": "s1", "slate_id": "m-7", "event_type": "starter_card",
		"topic_id": "rev-7", "policy_id": POLICY_ID, "payload": cardPayload,
		"created_at": 10.0,
	}); !ok {
		t.Fatalf("record card")
	}
	plans := pipeline.RetainedCandidatePlans(snapshot, rid, feedback, triggerKey,
		summariesByID, 100.0)
	if len(plans) != 1 || plans[0]["interest_summary_id"] != "int-a" {
		t.Fatalf("carried: %v", plans)
	}
	// 释放时间不足（剩 100 秒 < 300 秒时限）→ 不携带
	plans = pipeline.RetainedCandidatePlans(snapshot, rid, feedback, triggerKey,
		summariesByID, 100.0+400*24*60*60-100)
	if len(plans) != 0 {
		t.Fatalf("short remaining: %v", plans)
	}
}

// TestOfferedQuestions_Empty 对齐 :6345-6412 无曝光。
func TestOfferedQuestions_Empty(t *testing.T) {
	store := newTestStore(t)
	pipeline := NewTopicPipeline(store, nil, nil, nil, nil, "", nil, nil, nil, nil, nil,
		func() float64 { return 1000.0 })
	snapshot := &V6ContextSnapshot{ContextScope: GlobalNewChatScope}
	if questions := pipeline.OfferedQuestions(snapshot, false); len(questions) != 0 {
		t.Fatalf("questions: %v", questions)
	}
}
