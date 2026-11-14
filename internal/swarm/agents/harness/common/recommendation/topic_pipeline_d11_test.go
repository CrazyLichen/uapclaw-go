package recommendation

// topic_pipeline_d11_test.go 对齐 topic_pipeline.py:8872-9167 契约的单元用例。

import (
	"testing"
)

// d11Plan 已接地的画像兴趣计划。
func d11Plan(candidateID string) map[string]any {
	binding, _ := PipelineAPIBinding(EVIDENCE_COMPONENT_ID, EvidenceAPIProviderID,
		"v9", EvidenceExtractorVersion)
	receipt, _ := PipelineSuccessReceipt(binding, "snap-11", "req-ev-"+candidateID)
	return map[string]any{
		"candidate_id": candidateID, "category": "tech",
		"context_scope": GlobalNewChatScope, "interest_summary_id": "int-a",
		"intent": "想学", "title": "标题-" + candidateID,
		"prompt": "提示-" + candidateID, "primary_query": "查询",
		"freshness_class":  "recent_development",
		"evidence_receipt": receipt,
		"evidence_bundle": map[string]any{
			"fetch_status": "success", "source_url": "https://e", "source_kind": "stable",
			"document_revision_id": "doc-1",
			"content_boundary":     map[string]any{"text": "证据", "start": 0, "end": 2},
			"exact_spans": []any{map[string]any{
				"candidate_id": candidateID, "document_revision_id": "doc-1",
				"start": 0, "end": 2, "exact_text": "证据"}},
		},
		"search_receipt": map[string]any{
			"receipt_version": OpenjiuwenNativeSearchReceiptVersion,
			"provider_id":     OpenjiuwenNativeSearchProviderID,
			"host_request_id": "h1", "requested_query": "查询",
			"engine": "bing", "result_urls": []any{"https://e"}, "result_count": 1,
		},
		"document_snapshot_manifest": map[string]any{
			"snapshot_date": "2026-11-13", "snapshot_version": "doc-v1",
			"document_revision_ids": []any{"doc-1"}, "documents": []any{},
		},
	}
}

// TestAssembleResult_Basic 对齐 :8872-9167 装配主链。
func TestAssembleResult_Basic(t *testing.T) {
	store := newTestStore(t)
	pipeline := NewTopicPipeline(store, nil, nil, nil, nil, "", nil, nil, nil, nil, nil,
		func() float64 { return 1000.0 })
	snapshot := &V6ContextSnapshot{
		ContextScope: GlobalNewChatScope, InputSnapshotID: "snap-11",
		ProfileProjection: map[string]any{}, ShortIntentState: &ShortIntentState{},
		StateMaintenanceReceipt: PersistedStateReceiptForV2(t),
	}
	ridBinding, _ := PipelineAPIBinding(RID_COMPONENT_ID, "ark", "v9", RID_API_PROMPT_VERSION)
	ridReceipt, _ := PipelineSuccessReceipt(ridBinding, "snap-11", "req-rid")
	rid := &RIDResult{
		ContextScope: GlobalNewChatScope, ActivityGroup: "g",
		ThresholdVersion: RIDThresholdVersion, Receipt: ridReceipt,
	}
	utilityBinding := UtilityBinding(&ArkBackgroundConfig{Model: "v9"})
	utilityReceipt, _ := PipelineSuccessReceipt(utilityBinding, "snap-11", "req-ut")
	diversityBinding, _ := PipelineAPIBinding(DIVERSITY_COMPONENT_ID, "ark", "v9",
		DiversityConfigVersion)
	diversityReceipt, _ := PipelineSuccessReceipt(diversityBinding, "snap-11", "req-div")
	selection := &ListSearchResult{
		PreferredList: []string{"cand-1"},
		Decisions: []CandidateHostDecision{
			{CandidateID: "cand-1", Selected: true,
				Reasons: []string{"selected_by_host_list_search"}},
		},
		BoundedBeamTrace: BoundedBeamTrace{
			Entries: []BeamTraceEntry{{
				TraceIndex: 1, CandidateIDs: []string{"cand-1"}, Depth: 1,
				Status: "preferred", RetainedInBeam: true,
				HardConstraintsPass: true,
			}},
			PreferredCandidateIDs: []string{"cand-1"}, TargetSize: 6,
			ExpansionCount: 1, BeamWidth: 2, MaxExpansions: 5,
			UtilityScorerVersion: "cfg", DiversityScorerVersion: "cfg",
			SearchVersion: LIST_SEARCH_VERSION,
		},
		UtilityReceipt: utilityReceipt, DiversityReceipt: diversityReceipt,
		SearchVersion: LIST_SEARCH_VERSION,
	}
	triggerKey := map[string]any{
		"local_date": "2026-11-13", "profile_semantic_version": 0,
		"short_intent_version": 0, "negative_preference_version": 0,
		"generation_config_version": "topic-starter-market-generation-v10",
		"corpus_version":            "corpus-v1", "document_snapshot_version": "doc-v1",
		"retrieval_config_version": "ret-v1", "evidence_extractor_version": "ext-v1",
		"search_provider_id": OpenjiuwenNativeSearchProviderID,
		"component_contract_versions": map[string]any{
			"icebreaker_rid":             TERMINAL_CONTRACT_VERSION,
			"interaction_utility_scorer": TERMINAL_CONTRACT_VERSION,
			"verbatim_web_evidence":      TERMINAL_CONTRACT_VERSION,
			"diversity_scorer":           "topic-starter-v6-quality-decisions-v2"},
		"model": "v9",
		"input_revision_receipt": map[string]any{
			"receipt_version": InputRevisionReceiptVersion,
			"context_scope":   GlobalNewChatScope, "revision_digest": "d1"},
	}
	plan := d11Plan("cand-1")
	groundingEvidenceReceipt := map[string]any{
		"schema_version":    GroundingEvidenceReceiptVersion,
		"input_snapshot_id": "snap-11", "context_scope": GlobalNewChatScope,
		"document_snapshot": map[string]any{
			"snapshot_date": "2026-11-13", "snapshot_version": "doc-v1",
			"document_revision_ids": []any{"doc-1"}, "documents": []any{},
		},
		"candidates": []any{},
	}
	groundingDecisionReceipt := map[string]any{
		"schema_version": GroundingDecisionReceiptVersion,
		"decision":       "admit",
		"candidate_decisions": []any{
			map[string]any{"candidate_id": "cand-1", "decision": "admit"}},
	}
	result, err := pipeline.AssembleResult([]string{"cand-1"},
		[]map[string]any{plan}, selection, nil, snapshot, nil,
		map[string]any{"schema_version": SessionSummaryCoverageReceiptVersion,
			"input_snapshot_id": "snap-11", "context_scope": GlobalNewChatScope,
			"summary_config_version": "cfg", "sessions": []any{}},
		nil, "cfg", rid, nil, groundingDecisionReceipt, groundingEvidenceReceipt,
		triggerKey, "scheduled_cron",
		&ArkBackgroundConfig{Model: "v9", ContextWindowTokens: 100000,
			Temperature: 0.3, TopP: 0.9, BaseURL: "https://ark"},
		nil, map[string]string{})
	if err != nil {
		t.Fatalf("assemble: %v", err)
	}
	if len(result.Cards) != 1 || result.Cards[0]["selector_rank"] != 1 {
		t.Fatalf("cards: %v", result.Cards)
	}
	if result.Manifest["manifest_id"] == "" ||
		result.Manifest["generation_batch_id"] != result.Cards[0]["generation_batch_id"] {
		t.Fatalf("manifest ids: %v", result.Manifest["manifest_id"])
	}
	if result.Manifest["policy_id"] != POLICY_ID {
		t.Fatalf("policy: %v", result.Manifest["policy_id"])
	}
	listSearch := payloadMapOrEmpty(result.Manifest["list_search"])
	if pyStr(listSearch["search_version"]) != LIST_SEARCH_VERSION {
		t.Fatalf("list search: %v", listSearch)
	}
}

// TestAssembleResult_Rejections 对齐 :8902-8922/8923-8931/8992-9009 守卫。
func TestAssembleResult_Rejections(t *testing.T) {
	store := newTestStore(t)
	pipeline := NewTopicPipeline(store, nil, nil, nil, nil, "", nil, nil, nil, nil, nil,
		func() float64 { return 1000.0 })
	snapshot := &V6ContextSnapshot{
		ContextScope: GlobalNewChatScope, InputSnapshotID: "snap-11",
		ProfileProjection: map[string]any{}, ShortIntentState: &ShortIntentState{},
		StateMaintenanceReceipt: PersistedStateReceiptForV2(t),
	}
	ridBinding, _ := PipelineAPIBinding(RID_COMPONENT_ID, "ark", "v9", RID_API_PROMPT_VERSION)
	ridReceipt, _ := PipelineSuccessReceipt(ridBinding, "snap-11", "req-rid")
	rid := &RIDResult{Receipt: ridReceipt}
	utilityReceipt, _ := PipelineSuccessReceipt(
		UtilityBinding(&ArkBackgroundConfig{Model: "v9"}), "snap-11", "req-ut")
	selection := &ListSearchResult{
		PreferredList:  []string{"cand-1"},
		Decisions:      []CandidateHostDecision{},
		UtilityReceipt: utilityReceipt, SearchVersion: LIST_SEARCH_VERSION,
	}
	triggerKey := map[string]any{
		"local_date": "2026-11-13", "profile_semantic_version": 0,
		"short_intent_version": 0, "negative_preference_version": 0,
		"generation_config_version": "gen", "corpus_version": "c",
		"document_snapshot_version": "d", "retrieval_config_version": "r",
		"evidence_extractor_version": "e",
		"search_provider_id":         OpenjiuwenNativeSearchProviderID,
		"component_contract_versions": map[string]any{
			"icebreaker_rid":             TERMINAL_CONTRACT_VERSION,
			"interaction_utility_scorer": TERMINAL_CONTRACT_VERSION,
			"verbatim_web_evidence":      TERMINAL_CONTRACT_VERSION,
			"diversity_scorer":           "topic-starter-v6-quality-decisions-v2"},
		"model": "v9",
		"input_revision_receipt": map[string]any{
			"receipt_version": InputRevisionReceiptVersion,
			"context_scope":   GlobalNewChatScope, "revision_digest": "d1"},
	}
	// 无网页证据小票的网页计划 → missing_web_evidence_receipt
	plan := d11Plan("cand-1")
	delete(plan, "evidence_receipt")
	groundingEvidenceReceipt := map[string]any{
		"document_snapshot": map[string]any{
			"snapshot_date": "2026-11-13", "snapshot_version": "doc-v1",
			"document_revision_ids": []any{"doc-1"}, "documents": []any{}},
		"candidates": []any{},
	}
	groundingDecisionReceipt := map[string]any{"decision": "admit",
		"candidate_decisions": []any{}}
	_, err := pipeline.AssembleResult([]string{"cand-1"},
		[]map[string]any{plan}, selection, nil, snapshot, nil,
		map[string]any{"schema_version": SessionSummaryCoverageReceiptVersion,
			"input_snapshot_id": "snap-11", "context_scope": GlobalNewChatScope,
			"summary_config_version": "cfg", "sessions": []any{}},
		nil, "cfg", rid, nil, groundingDecisionReceipt, groundingEvidenceReceipt,
		triggerKey, "scheduled_cron",
		&ArkBackgroundConfig{Model: "v9"}, nil, map[string]string{})
	if err == nil || err.(*TerminalGenerationRejection).Reason != "missing_web_evidence_receipt" {
		t.Fatalf("missing evidence receipt: %v", err)
	}
	// 证据 span 超出文档快照
	plan2 := d11Plan("cand-1")
	bundle := plan2["evidence_bundle"].(map[string]any)
	bundle["exact_spans"] = []any{map[string]any{
		"document_revision_id": "doc-unknown"}}
	_, err = pipeline.AssembleResult([]string{"cand-1"},
		[]map[string]any{plan2}, selection, nil, snapshot, nil,
		map[string]any{"schema_version": SessionSummaryCoverageReceiptVersion,
			"input_snapshot_id": "snap-11", "context_scope": GlobalNewChatScope,
			"summary_config_version": "cfg", "sessions": []any{}},
		nil, "cfg", rid, nil, groundingDecisionReceipt, groundingEvidenceReceipt,
		triggerKey, "scheduled_cron",
		&ArkBackgroundConfig{Model: "v9"}, nil, map[string]string{})
	if err == nil || err.(*TerminalGenerationRejection).Reason != "evidence_outside_document_snapshot" {
		t.Fatalf("outside snapshot: %v", err)
	}
	// 组件小票契约版本与触发键不匹配
	plan3 := d11Plan("cand-1")
	badTrigger := map[string]any{}
	for key, value := range triggerKey {
		badTrigger[key] = value
	}
	badTrigger["component_contract_versions"] = map[string]any{
		"icebreaker_rid": "wrong", "interaction_utility_scorer": UtilityConfigVersion,
		"verbatim_web_evidence": EvidenceExtractorVersion,
		"diversity_scorer":      DiversityConfigVersion}
	_, err = pipeline.AssembleResult([]string{"cand-1"},
		[]map[string]any{plan3}, selection, nil, snapshot, nil,
		map[string]any{"schema_version": SessionSummaryCoverageReceiptVersion,
			"input_snapshot_id": "snap-11", "context_scope": GlobalNewChatScope,
			"summary_config_version": "cfg", "sessions": []any{}},
		nil, "cfg", rid, nil, groundingDecisionReceipt, groundingEvidenceReceipt,
		badTrigger, "scheduled_cron",
		&ArkBackgroundConfig{Model: "v9"}, nil, map[string]string{})
	if err == nil || err.(*TerminalGenerationRejection).Reason != "component_trigger_version_mismatch" {
		t.Fatalf("trigger mismatch: %v", err)
	}
}
