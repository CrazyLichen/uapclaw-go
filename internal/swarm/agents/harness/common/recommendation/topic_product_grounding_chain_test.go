package recommendation

// topic_product_grounding_chain_test.go 对齐 grounding 决策/证据校验与
// terminal_learning_lineage 串联的最小闭环用例。

import "testing"

// groundingFixtureFor 构造最小合法 grounding 全链清单（一候选：搜索→抓取→提取→admit）。
func groundingFixtureFor(t *testing.T, contextScope string, withCards bool) (map[string]any, []map[string]any) {
	t.Helper()
	inputSnapshotID := "snap1"
	searchReceipt := arkReceiptMap()
	searchReceipt["result_urls"] = []any{"https://example.com/a"}
	searchAudit := map[string]any{
		"sequence": 1, "input_snapshot_id": inputSnapshotID, "context_scope": contextScope,
		"operation": "search", "operation_id": "op-s", "provider_id": ArkWebSearchProviderID,
		"candidate_id": "cand-1", "status": "success", "request_id": "req-s",
		"result_id": "res-s", "latency_ms": 10.0, "failure_reason": nil,
	}
	fetchAudit := map[string]any{
		"sequence": 2, "input_snapshot_id": inputSnapshotID, "context_scope": contextScope,
		"operation": "fetch", "operation_id": "op-f", "provider_id": "fetcher",
		"candidate_id": "cand-1", "status": "success", "request_id": "req-f",
		"result_id": "res-f", "latency_ms": 20.0, "failure_reason": nil,
	}
	extractAudit := map[string]any{
		"sequence": 3, "input_snapshot_id": inputSnapshotID, "context_scope": contextScope,
		"operation": "extractor", "operation_id": "op-e", "provider_id": "extractor",
		"candidate_id": "cand-1", "status": "success", "request_id": "req-e",
		"result_id": "res-e", "latency_ms": 30.0, "failure_reason": nil,
	}
	document := map[string]any{
		"document_revision_id": "doc1", "source_url": "https://example.com/a",
		"publisher": "example", "source_kind": "authoritative",
		"published_at": "2026-01-01T00:00:00+00:00",
		"publication_date_provenance": map[string]any{
			"source_type": "page_explicit_metadata", "source_field": "date",
			"raw_value": "2026-01-01T00:00:00+00:00", "search_provider_id": nil,
			"search_receipt_id": nil, "search_result_url": nil,
			"listed_at": "2026-01-01T00:00:00+00:00",
		},
		"fetched_at": 500.0, "snapshot_date": "2026-01-01", "snapshot_version": "sv1",
	}
	summary := summaryForTests(t, "summary:s1:"+hex64("a"), "s1", 10.0)
	componentReceipt := map[string]any{
		"receipt_version": GroundingEvidenceReceiptVersion,
		"binding": map[string]any{
			"component_id": "verbatim_web_evidence", "contract_version": "v1",
			"provider_id": "extractor", "provider_version": "pv", "config_version": "cv",
		},
		"input_snapshot_id": inputSnapshotID, "request_id": "req-e",
		"score_semantics": "uncalibrated_api_proxy", "output_status": "success",
		"fallback_reason": nil,
	}
	candidateDecision := map[string]any{
		"candidate_id": "cand-1", "interest_summary_id": summary.SessionSummaryID, "category": "adjacent_explanation",
		"decision": "admit", "terminal_stage": "admission", "reason": "admitted",
		"external_operations": []any{
			map[string]any{"operation": "search", "operation_id": "op-s", "request_id": "req-s"},
			map[string]any{"operation": "fetch", "operation_id": "op-f", "request_id": "req-f"},
			map[string]any{"operation": "extractor", "operation_id": "op-e", "request_id": "req-e"},
		},
	}
	manifest := map[string]any{
		"policy_id": ProductPolicyID, "payload_version": ProductPayloadVersion,
		"generation_input_snapshot_id": inputSnapshotID, "context_scope": contextScope,
		"document_snapshot": map[string]any{
			"snapshot_date": "2026-01-01", "snapshot_version": "sv1",
			"document_revision_ids": []any{"doc1"}, "documents": []any{document},
		},
		"external_invocation_audit": map[string]any{
			"schema_version":    EXTERNAL_INVOCATION_AUDIT_SCHEMA_VERSION,
			"input_snapshot_id": inputSnapshotID, "context_scope": contextScope,
			"invocations": []any{searchAudit, fetchAudit, extractAudit},
			"counters": map[string]any{
				"total": 3, "success": 3, "failure": 0,
				"by_operation": map[string]any{
					"search":    map[string]any{"total": 1, "success": 1, "failure": 0},
					"fetch":     map[string]any{"total": 1, "success": 1, "failure": 0},
					"extractor": map[string]any{"total": 1, "success": 1, "failure": 0},
				},
			},
		},
		"grounding_decision_receipt": map[string]any{
			"schema_version":      GroundingDecisionReceiptVersion,
			"input_snapshot_id":   inputSnapshotID,
			"context_scope":       contextScope,
			"decision":            "admit",
			"candidate_decisions": []any{candidateDecision},
		},
		"grounding_evidence_receipt": map[string]any{
			"schema_version":    GroundingEvidenceReceiptVersion,
			"input_snapshot_id": inputSnapshotID, "context_scope": contextScope,
			"document_snapshot": map[string]any{
				"snapshot_date": "2026-01-01", "snapshot_version": "sv1",
				"document_revision_ids": []any{"doc1"}, "documents": []any{document},
			},
			"candidates": []any{map[string]any{
				"candidate_id": "cand-1",
				"search": map[string]any{
					"operation_id": "op-s", "request_id": "req-s",
					"requested_query": "围棋", "provider_receipt": searchReceipt,
				},
				"fetches": []any{map[string]any{
					"operation_id": "op-f", "request_id": "req-f",
					"requested_url": "https://example.com/a", "final_url": "https://example.com/a",
					"http_status_code": 200, "document_revision_id": "doc1",
				}},
				"extractor": map[string]any{
					"operation_id": "op-e", "request_id": "req-e",
					"input_document_revision_ids": []any{"doc1"},
					"component_receipt":           componentReceipt,
				},
			}},
		},
		"session_summary_artifacts": []any{summary.AsPayload()},
		"session_summary_coverage_receipt": map[string]any{
			"schema_version":         SessionSummaryCoverageReceiptVersion,
			"input_snapshot_id":      inputSnapshotID,
			"context_scope":          contextScope,
			"summary_config_version": "cfg-v1",
			"sessions": []any{map[string]any{
				"session_id": summary.SessionID, "session_summary_id": summary.SessionSummaryID,
				"input_origin": summary.InputOrigin, "assistance_origin": summary.AssistanceOrigin,
				"rid_eligible": true, "rid_exclusion_reason": nil,
			}},
		},
	}
	decision := ridDecisionFor(summary.SessionSummaryID, "session_summary", 1, true)
	decision["source_session_summary_ids"] = []any{summary.SessionSummaryID}
	decision["source_event_ids"] = stringsToAny(summary.SourceEventIDs)
	var locators []any
	for _, locator := range summary.SourceLocators {
		locators = append(locators, locator.AsPayload())
	}
	decision["source_locators"] = locators
	componentReceipts := map[string]any{"icebreaker_rid": map[string]any{
		"receipt_version": "v1",
		"binding": map[string]any{
			"component_id": "icebreaker_rid", "contract_version": "v1",
			"provider_id": "p", "provider_version": "pv", "config_version": "cv",
		},
		"input_snapshot_id": inputSnapshotID, "request_id": "req1",
		"score_semantics": "uncalibrated_api_proxy", "output_status": "success",
		"fallback_reason": nil,
	}}
	manifest["rid_result"] = map[string]any{
		"rid_result_version": ridResultSchemaVersion, "context_scope": contextScope,
		"activity_group": "active", "threshold_version": "t1",
		"component_receipt": mapAnyOf(componentReceipts["icebreaker_rid"]),
		"trigger_interests": []any{decision}, "rejected_interests": []any{},
	}
	manifest["component_receipts"] = componentReceipts
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
	var cards []map[string]any
	if withCards {
		stripped := jsonRoundTrip(searchReceipt)
		delete(stripped, "result_urls")
		cards = []map[string]any{{
			"card_id": "cand-1", "category": "adjacent_explanation",
			"interest_summary_id": summary.SessionSummaryID, "context_scope": contextScope, "project_id": nil,
			"search_receipt": stripped,
			"evidence_bundle": map[string]any{
				"document_revision_id": "doc1",
			},
		}}
	}
	return manifest, cards
}

// TestGroundingDecisionRejectionReason_合法闭环 对齐 :1621-1876。
func TestGroundingDecisionRejectionReason(t *testing.T) {
	manifest, cards := groundingFixtureFor(t, GlobalNewChatScope, true)
	if got := groundingDecisionRejectionReason(manifest, cards); got != "" {
		t.Fatalf("reason=%q", got)
	}
	// 决策聚合不一致：全 admit 但 receipt 决策 partial
	receipt := mapAnyOf(manifest["grounding_decision_receipt"])
	receipt["decision"] = "partial"
	if groundingDecisionRejectionReason(manifest, cards) == "" {
		t.Fatalf("decision mismatch should fail")
	}
	receipt["decision"] = "admit"
	// 卡决策必须存在且 admit
	if groundingDecisionRejectionReason(manifest, []map[string]any{{"card_id": "ghost"}}) == "" {
		t.Fatalf("unreferenced card should fail")
	}
}

// TestGroundingEvidenceReceiptRejectionReason_合法闭环 对齐 :2023-2410。
func TestGroundingEvidenceReceiptRejectionReason(t *testing.T) {
	manifest, cards := groundingFixtureFor(t, GlobalNewChatScope, true)
	if got := GroundingEvidenceReceiptRejectionReason(manifest, cards); got != "" {
		t.Fatalf("reason=%q", got)
	}
	// 文档 ID 表与文档表不符
	snapshot := mapAnyOf(mapAnyOf(manifest["grounding_evidence_receipt"])["document_snapshot"])
	snapshot["document_revision_ids"] = []any{}
	if GroundingEvidenceReceiptRejectionReason(manifest, cards) == "" {
		t.Fatalf("document id mismatch should fail")
	}
	snapshot["document_revision_ids"] = []any{"doc1"}
	// 宿主证据候选不得带外部操作
	decision := mapAnyOf(mapAnyOf(manifest["grounding_decision_receipt"])["candidate_decisions"].([]any)[0])
	decision["category"] = "project_continuation"
	if GroundingEvidenceReceiptRejectionReason(manifest, cards) == "" {
		t.Fatalf("host evidence with operations should fail")
	}
}

// TestTerminalLearningLineageRejectionReason_串联 对齐 :1529-1576。
func TestTerminalLearningLineageRejectionReason(t *testing.T) {
	manifest, cards := groundingFixtureFor(t, GlobalNewChatScope, true)
	if got := TerminalLearningLineageRejectionReason(manifest, cards, false); got != "" {
		t.Fatalf("reason=%q", got)
	}
	// 缺字段
	delete(manifest, "rid_result")
	if got := TerminalLearningLineageRejectionReason(manifest, cards, false); got != "manifest_required_field_rid_result_missing" {
		t.Fatalf("missing field reason=%q", got)
	}
	// 非清单输入
	if TerminalLearningLineageRejectionReason([]any{}, cards, false) != "manifest_type_contract_failed" {
		t.Fatalf("type contract")
	}
}
