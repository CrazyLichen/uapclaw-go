package recommendation

// topic_pipeline_d8_test.go 对齐 topic_pipeline.py:7098-7712 契约的单元用例。

import (
	"context"
	"testing"
)

// TestValidateProjectContinuations 对齐 :7269-7348。
func TestValidateProjectContinuations(t *testing.T) {
	snapshot := &V6ContextSnapshot{
		ContextScope: "project:p1", ProjectID: "p1", InputSnapshotID: "snap-8",
	}
	source := &V6SourceMessage{
		EventID: "evt-1", SessionID: "s1", Ordinal: 0, Content: "项目问题",
		ProjectID: "p1", InputOrigin: "manual_free_text", AssistanceOrigin: "none",
	}
	eventByID := map[string]*V6SourceMessage{"evt-1": source}
	locators := []*SessionSourceLocator{{
		SessionID: "s1", Ordinal: 0, EventID: "evt-1"}}
	slots := []map[string]any{
		{"slot_id": "slot-1"}, {"slot_id": "slot-2"}, {"slot_id": "slot-3"},
	}
	response := map[string]any{"candidates": []any{
		map[string]any{"slot_id": "slot-1", "interest_summary_id": "int-a",
			"blocked_by": "", "new_angle": "", "nearest_covered": "",
			"nearest_offered": "", "nearest_dismissed": "", "decision": "write",
			"intent": "跟进", "title": "续聊标题", "source_event_id": "evt-1"},
		map[string]any{"slot_id": "slot-2", "interest_summary_id": "int-a",
			"blocked_by": "", "new_angle": "", "nearest_covered": "",
			"nearest_offered": "", "nearest_dismissed": "", "decision": "skip",
			"intent": "", "title": "", "source_event_id": ""},
		map[string]any{"slot_id": "slot-3", "interest_summary_id": "int-a",
			"blocked_by": "", "new_angle": "", "nearest_covered": "",
			"nearest_offered": "", "nearest_dismissed": "", "decision": "skip",
			"intent": "", "title": "", "source_event_id": ""},
	}}
	plans, err := ValidateProjectContinuations(response, false, snapshot,
		map[string][]*SessionSourceLocator{"int-a": locators},
		map[string]string{"int-a": "想深入"}, eventByID, slots)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if len(plans) != 1 || plans[0]["category"] != "project_continuation" ||
		plans[0]["candidate_id"] != "slot-1" {
		t.Fatalf("plans: %v", plans)
	}
	// 全 skip 且不允许空 → insufficient_active_project_actions
	allSkip := map[string]any{"candidates": []any{}}
	for _, slot := range []string{"slot-1", "slot-2", "slot-3"} {
		allSkip["candidates"] = append(allSkip["candidates"].([]any),
			map[string]any{"slot_id": slot, "interest_summary_id": "int-a",
				"blocked_by": "", "new_angle": "", "nearest_covered": "",
				"nearest_offered": "", "nearest_dismissed": "", "decision": "skip",
				"intent": "", "title": "", "source_event_id": ""})
	}
	if _, err := ValidateProjectContinuations(allSkip, false, snapshot,
		map[string][]*SessionSourceLocator{"int-a": locators},
		map[string]string{}, eventByID, slots); err == nil ||
		err.(*TerminalGenerationRejection).Reason != "insufficient_active_project_actions" {
		t.Fatalf("all skip: %v", err)
	}
	// 全 skip 且携带卡 → 允许空
	if plans, err := ValidateProjectContinuations(allSkip, true, snapshot,
		map[string][]*SessionSourceLocator{"int-a": locators},
		map[string]string{}, eventByID, slots); err != nil || len(plans) != 0 {
		t.Fatalf("allow empty: %v %v", plans, err)
	}
	// 源事件不在兴趣定位器中 → 契约失败
	response["candidates"].([]any)[0].(map[string]any)["source_event_id"] = "evt-x"
	if _, err := ValidateProjectContinuations(response, false, snapshot,
		map[string][]*SessionSourceLocator{"int-a": locators},
		map[string]string{}, eventByID, slots); err == nil ||
		err.(*TerminalGenerationRejection).Reason != "project_continuation_contract_failed" {
		t.Fatalf("bad locator: %v", err)
	}
}

// TestOptionalLaneOf 对齐 :2700-2708。
func TestOptionalLaneOf(t *testing.T) {
	if OptionalLaneOf(map[string]any{"category": "project_domain_progress"}) !=
		PipelineProjectDomainLane {
		t.Fatalf("domain lane")
	}
	if OptionalLaneOf(map[string]any{"source_channel": "news_feed"}) !=
		PipelineHotNewsLane {
		t.Fatalf("news lane")
	}
	if OptionalLaneOf(map[string]any{}) != PipelineGlobalRecentLane {
		t.Fatalf("default lane")
	}
}

// TestCandidateEvidenceFor_PrivateQuery 对齐 :7428-7429 私密查询拒绝。
func TestCandidateEvidenceFor_PrivateQuery(t *testing.T) {
	store := newTestStore(t)
	pipeline := NewTopicPipeline(store, nil, nil, nil, nil, "", nil, nil, nil, nil, nil,
		func() float64 { return 1000.0 })
	pipeline.fetch = func(ctx context.Context, target string, timeoutSeconds int) (map[string]any, error) {
		return map[string]any{"url": target, "status_code": 200}, nil
	}
	snapshot := &V6ContextSnapshot{ContextScope: GlobalNewChatScope,
		InputSnapshotID: "snap-8"}
	audit, _ := NewExternalInvocationAudit(store, POLICY_ID, "snap-8",
		GlobalNewChatScope, "visit-1")
	lanes := NewOptionalLanes()
	lanes.MandatoryReady()
	plan := map[string]any{
		"candidate_id": "cand-1", "primary_query": "联系 user@example.com",
	}
	result := pipeline.CandidateEvidenceFor(plan, snapshot, audit, nil, lanes,
		NewFetchTasks())
	if result.SearchRejection == nil ||
		result.SearchRejection[1] != "search_query_contains_private_material" {
		t.Fatalf("private query rejection: %v", result.SearchRejection)
	}
}

// TestCandidateEvidenceFor_FetchAndDedupe 对齐 :7530-7712 选行去重与抓取。
func TestCandidateEvidenceFor_FetchAndDedupe(t *testing.T) {
	store := newTestStore(t)
	pipeline := NewTopicPipeline(store, nil, nil, nil, nil, "", nil, nil, nil, nil, nil,
		func() float64 { return 1000.0 })
	pipeline.searchOverride = func(ctx context.Context, query string,
		maxResults, timeoutSeconds int) (string, []map[string]string, error) {
		return "bing", []map[string]string{
			{"url": "https://site.example/a", "title": "A"},
			{"url": "https://site.example/a?x=1", "title": "A dup"},
			{"url": "https://other.example/b", "title": "B"},
			{"url": "ftp://bad", "title": "bad"},
		}, nil
	}
	pipeline.fetch = func(ctx context.Context, target string, timeoutSeconds int) (map[string]any, error) {
		return map[string]any{"url": target, "status_code": 200}, nil
	}
	snapshot := &V6ContextSnapshot{ContextScope: GlobalNewChatScope,
		InputSnapshotID: "snap-8"}
	audit, _ := NewExternalInvocationAudit(store, POLICY_ID, "snap-8",
		GlobalNewChatScope, "visit-1")
	lanes := NewOptionalLanes()
	lanes.MandatoryReady()
	plan := map[string]any{"candidate_id": "cand-1", "primary_query": "公开查询"}
	result := pipeline.CandidateEvidenceFor(plan, snapshot, audit, nil, lanes,
		NewFetchTasks())
	if result.SearchRejection != nil {
		t.Fatalf("unexpected rejection: %v", result.SearchRejection)
	}
	// /a 与 /a?x=1 为不同版本化文档 → 三行全部保留（上限 3）
	if len(result.FetchedRows) != 3 {
		t.Fatalf("fetched rows: %d", len(result.FetchedRows))
	}
	if result.SearchReceipt == nil ||
		result.SearchReceipt["search_version"] != nil {
		// 小票存在且带 result_urls/count
		urls := pyListStrings(result.SearchReceipt["result_urls"])
		if len(urls) != 3 { // ftp://bad 被滤
			t.Fatalf("result urls: %v", urls)
		}
	}
	if result.FetchFailure != "" {
		t.Fatalf("fetch failure: %s", result.FetchFailure)
	}
	// 审计引用已记录
	if len(result.FetchAuditRefs) != 3 {
		t.Fatalf("audit refs: %d", len(result.FetchAuditRefs))
	}
}
