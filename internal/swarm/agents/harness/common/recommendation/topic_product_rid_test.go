package recommendation

// topic_product_rid_test.go 对齐上游 RID 结果/选择校验与审计索引用例。

import "testing"

// ridDecisionFor 构造一条 RID 决策。
func ridDecisionFor(interestID, sourceKind string, rank any, selected bool) map[string]any {
	decision := map[string]any{
		"interest_summary_id": interestID, "source_kind": sourceKind,
		"source_session_summary_ids": []any{}, "source_profile_interest_ids": []any{},
		"source_profile_unit_ids": []any{}, "source_event_ids": []any{},
		"source_locators": []any{}, "selected": selected, "rank": rank,
		"resonance_score_nullable": nil, "score_semantics": "uncalibrated_api_proxy",
	}
	return decision
}

// ridManifestFor 构造最小合法 RID 清单。
func ridManifestFor(t *testing.T, contextScope string, summary *SessionSummaryArtifact) map[string]any {
	t.Helper()
	receipt := map[string]any{
		"receipt_version": "v1",
		"binding": map[string]any{
			"component_id": "icebreaker_rid", "contract_version": "v1",
			"provider_id": "p", "provider_version": "pv", "config_version": "cv",
		},
		"input_snapshot_id": "snap1", "request_id": "req1",
		"score_semantics": "uncalibrated_api_proxy", "output_status": "success",
		"fallback_reason": nil,
	}
	decision := ridDecisionFor(summary.SessionSummaryID, "session_summary", 1, true)
	decision["source_session_summary_ids"] = []any{summary.SessionSummaryID}
	decision["source_event_ids"] = []any{"evt"}
	locators := []any{}
	for _, locator := range summary.SourceLocators {
		locators = append(locators, locator.AsPayload())
	}
	decision["source_locators"] = locators
	return map[string]any{
		"policy_id": ProductPolicyID, "payload_version": ProductPayloadVersion,
		"context_scope": contextScope, "generation_input_snapshot_id": "snap1",
		"session_summary_artifacts": []any{summary.AsPayload()},
		"component_receipts":        map[string]any{"icebreaker_rid": receipt},
		"rid_result": map[string]any{
			"rid_result_version": ridResultSchemaVersion,
			"context_scope":      contextScope, "activity_group": "active",
			"threshold_version": "t1", "component_receipt": receipt,
			"trigger_interests": []any{decision}, "rejected_interests": []any{},
		},
	}
}

// TestRidResultRejectionReason_合法清单 对齐 :1247-1390 合格路径。
func TestRidResultRejectionReasonValid(t *testing.T) {
	summary := summaryForTests(t, "summary:s1:"+hex64("a"), "s1", 10.0)
	manifest := ridManifestFor(t, GlobalNewChatScope, summary)
	if got := ridResultRejectionReason(manifest, false); got != "" {
		t.Fatalf("reason=%q", got)
	}
}

// TestRidResultRejectionReason_契约破坏 对齐主要失败分支。
func TestRidResultRejectionReasonBroken(t *testing.T) {
	summary := summaryForTests(t, "summary:s1:"+hex64("a"), "s1", 10.0)
	manifest := ridManifestFor(t, GlobalNewChatScope, summary)
	reason := "manifest_field_rid_result_contract_failed"
	// 版本错
	result := mapAnyOf(manifest["rid_result"])
	result["rid_result_version"] = "bogus"
	if ridResultRejectionReason(manifest, false) != reason {
		t.Fatalf("version")
	}
	result["rid_result_version"] = ridResultSchemaVersion
	// scope 错
	result["context_scope"] = "project:p1"
	if ridResultRejectionReason(manifest, false) != reason {
		t.Fatalf("scope")
	}
	result["context_scope"] = GlobalNewChatScope
	// activity_group 错
	result["activity_group"] = "idle"
	if ridResultRejectionReason(manifest, false) != reason {
		t.Fatalf("activity group")
	}
	result["activity_group"] = "active"
	// 非全局页未消费全部自然摘要 → 拒绝（补一个未消费的自然摘要）
	extra := summaryForTests(t, "summary:s2:"+hex64("b"), "s2", 11.0)
	projectManifest := ridManifestFor(t, "project:p1", summary)
	artifacts := anyListOf(projectManifest["session_summary_artifacts"])
	projectManifest["session_summary_artifacts"] = append(artifacts, extra.AsPayload())
	if ridResultRejectionReason(projectManifest, false) != reason {
		t.Fatalf("natural summary not consumed on project page")
	}
	// 全局页 news_only 允许空 trigger
	result["trigger_interests"] = []any{}
	if ridResultRejectionReason(manifest, true) != "" {
		t.Fatalf("news_only should allow empty")
	}
	if ridResultRejectionReason(manifest, false) != reason {
		t.Fatalf("empty trigger without news_only should fail")
	}
}

// TestExternalInvocationAuditIndex_契约 对齐 :1111-1245。
func TestExternalInvocationAuditIndex(t *testing.T) {
	invocation := map[string]any{
		"sequence": 1, "input_snapshot_id": "snap1", "context_scope": GlobalNewChatScope,
		"operation": "search", "operation_id": "op1", "provider_id": "p",
		"candidate_id": "c1", "status": "success", "request_id": "r1",
		"result_id": "res1", "latency_ms": 12.5, "failure_reason": nil,
	}
	manifest := map[string]any{
		"generation_input_snapshot_id": "snap1", "context_scope": GlobalNewChatScope,
		"external_invocation_audit": map[string]any{
			"schema_version":    EXTERNAL_INVOCATION_AUDIT_SCHEMA_VERSION,
			"input_snapshot_id": "snap1",
			"context_scope":     GlobalNewChatScope,
			"invocations":       []any{invocation},
			"counters": map[string]any{
				"total": 1, "success": 1, "failure": 0,
				"by_operation": map[string]any{
					"search":    map[string]any{"total": 1, "success": 1, "failure": 0},
					"fetch":     map[string]any{"total": 0, "success": 0, "failure": 0},
					"extractor": map[string]any{"total": 0, "success": 0, "failure": 0},
				},
			},
		},
	}
	index := ExternalInvocationAuditIndex(manifest)
	if index == nil {
		t.Fatalf("valid audit should index")
	}
	if _, ok := index[[3]string{"search", "op1", "r1"}]; !ok {
		t.Fatalf("key missing")
	}
	// counters 不符 → nil
	bad := jsonRoundTrip(manifest)
	mapAnyOf(mapAnyOf(mapAnyOf(bad["external_invocation_audit"])["counters"])["by_operation"])["search"] = map[string]any{"total": 5, "success": 1, "failure": 0}
	if ExternalInvocationAuditIndex(bad) != nil {
		t.Fatalf("counter mismatch should be nil")
	}
	// 成功但无 result_id → nil
	noResult := jsonRoundTrip(manifest)
	inv := anyListOf(mapAnyOf(noResult["external_invocation_audit"])["invocations"])[0]
	mapAnyOf(inv)["result_id"] = nil
	if ExternalInvocationAuditIndex(noResult) != nil {
		t.Fatalf("success without result_id should be nil")
	}
	// 重复 operation_id → nil
	dup := jsonRoundTrip(manifest)
	second := jsonRoundTrip(invocation)
	second["sequence"] = 2
	second["request_id"] = "r2"
	auditInvocations := anyListOf(mapAnyOf(dup["external_invocation_audit"])["invocations"])
	mapAnyOf(dup["external_invocation_audit"])["invocations"] = append(auditInvocations, second)
	// 修复 counters：total=2
	mapAnyOf(mapAnyOf(dup["external_invocation_audit"])["counters"])["total"] = 2
	mapAnyOf(mapAnyOf(dup["external_invocation_audit"])["counters"])["success"] = 2
	mapAnyOf(mapAnyOf(mapAnyOf(dup["external_invocation_audit"])["counters"])["by_operation"])["search"] = map[string]any{"total": 2, "success": 2, "failure": 0}
	if ExternalInvocationAuditIndex(dup) != nil {
		t.Fatalf("duplicate operation_id should be nil")
	}
}

// TestRidInterestSelectionRejectionReason_全局页 对齐 :1392-1527。
func TestRidInterestSelectionRejectionReason(t *testing.T) {
	summary := summaryForTests(t, "summary:s1:"+hex64("a"), "s1", 10.0)
	manifest := ridManifestFor(t, GlobalNewChatScope, summary)
	decision := mapAnyOf(mapAnyOf(manifest["rid_result"])["trigger_interests"].([]any)[0])
	selection := map[string]any{
		"policy":                                "all_rid_selected_v1",
		"selected_interest_summary_ids":         []any{summary.SessionSummaryID},
		"original_rid_ranks":                    map[string]any{summary.SessionSummaryID: 1},
		"delta_linked_interest_summary_id":      nil,
		"short_trigger_delta_event_ids":         []any{},
		"extra_recent_slot_interest_summary_id": nil,
		"direction_withheld_candidates":         []any{},
		"restated_offered_candidates":           []any{},
	}
	manifest["rid_interest_selection"] = selection
	cards := []map[string]any{{"card_id": "c1", "interest_summary_id": summary.SessionSummaryID}}
	if got := ridInterestSelectionRejectionReason(manifest, cards, false); got != "" {
		t.Fatalf("reason=%q", got)
	}
	// 卡片兴趣不在选择集 → 拒绝
	cards[0]["interest_summary_id"] = "other"
	if ridInterestSelectionRejectionReason(manifest, cards, false) == "" {
		t.Fatalf("unselected interest should fail")
	}
	cards[0]["interest_summary_id"] = summary.SessionSummaryID
	// policy 错误
	selection["policy"] = "bogus"
	if ridInterestSelectionRejectionReason(manifest, cards, false) == "" {
		t.Fatalf("bad policy should fail")
	}
	selection["policy"] = "all_rid_selected_v1"
	// rank 不匹配
	selection["original_rid_ranks"] = map[string]any{summary.SessionSummaryID: 9}
	if ridInterestSelectionRejectionReason(manifest, cards, false) == "" {
		t.Fatalf("rank mismatch should fail")
	}
	selection["original_rid_ranks"] = map[string]any{summary.SessionSummaryID: decision["rank"]}
	// 项目页：selection 必须缺席
	projectManifest := ridManifestFor(t, "project:p1", summary)
	projectManifest["rid_result"].(map[string]any)["context_scope"] = "project:p1"
	projectManifest["context_scope"] = "project:p1"
	if ridInterestSelectionRejectionReason(projectManifest, cards, false) != "" {
		t.Fatalf("project page without selection should pass")
	}
	projectManifest["rid_interest_selection"] = selection
	if ridInterestSelectionRejectionReason(projectManifest, cards, false) == "" {
		t.Fatalf("project page with selection should fail")
	}
}

// TestRetainedCandidateIds 对齐 :3053-3067。
func TestRetainedCandidateIds(t *testing.T) {
	if len(RetainedCandidateIds(nil)) != 0 {
		t.Fatalf("nil manifest")
	}
	manifest := map[string]any{"retained_candidates": map[string]any{
		"c1": map[string]any{"from_snapshot": "s1"}, "c2": true,
	}}
	ids := RetainedCandidateIds(manifest)
	if !ids["c1"] || !ids["c2"] || len(ids) != 2 {
		t.Fatalf("ids=%v", ids)
	}
}
