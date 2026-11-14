package recommendation

// topic_product_validate2_test.go 对齐 topic_product.py:3843-4017 等契约的用例。

import (
	"testing"
)

// v2Card 构造一张过全部基线闸门的合法卡。
func v2Card(t *testing.T) (map[string]any, map[string]any) {
	t.Helper()
	receipt := validReceiptFixture("icebreaker_rid", "snap-2")
	receipt2 := validReceiptFixture("interaction_utility_scorer", "snap-2")
	receipt2["binding"].(map[string]any)["component_id"] = "interaction_utility_scorer"
	receiptWeb := validReceiptFixture("verbatim_web_evidence", "snap-2")
	receiptWeb["binding"].(map[string]any)["component_id"] = "verbatim_web_evidence"
	card := map[string]any{
		"card_id": "cand-1", "card_revision_id": "rev-1", "generation_batch_id": "b-1",
		"selector_rank": 1, "category": "tech", "context_scope": GlobalNewChatScope,
		"project_id": nil, "interest_summary_id": "int-a", "source_starter_id": nil,
		"intent": "想学", "title": "标题", "prompt": "提示",
		"effective_time": 100.0, "expire_time": 100.0 + 7*24*60*60,
		"evidence_bundle": map[string]any{
			"fetch_status": "success", "source_url": "https://e", "source_kind": "stable",
			"document_revision_id": "doc-1",
			"content_boundary":     map[string]any{"text": "证据", "start": 0, "end": 2},
		},
		"search_provider_id": OpenjiuwenNativeSearchProviderID,
		"search_receipt": map[string]any{
			"receipt_version": OpenjiuwenNativeSearchReceiptVersion,
			"provider_id":     OpenjiuwenNativeSearchProviderID,
			"host_request_id": "host-1", "requested_query": "围棋定式",
			"engine": "bing", "result_urls": []any{"https://result.example/a"},
			"result_count": 1,
		},
		"source_memory_unit_ids": []any{}, "source_locators": []any{},
		"project_source_locators":       []any{},
		"generation_input_snapshot_id":  "snap-2",
		"state_maintenance_receipt":     PersistedStateReceiptForV2(t),
		"component_invocation_receipts": []any{},
		"provider_invocation_receipts":  []any{},
		"profile_semantic_version":      0, "short_intent_version": 0,
		"component_receipts": map[string]any{
			"icebreaker_rid": receipt, "interaction_utility_scorer": receipt2,
			"verbatim_web_evidence": receiptWeb,
		},
		"generation_config_version": "topic-starter-market-generation-v10",
		"corpus_version":            "corpus-v1", "document_snapshot_version": "doc-v1",
		"retrieval_config_version": "ret-v1", "evidence_extractor_version": "ext-v1",
		"list_search_version": "list-v1", "policy_id": POLICY_ID,
		"payload_version": PAYLOAD_VERSION,
	}
	trigger := map[string]any{
		"context_scope": GlobalNewChatScope, "project_id": nil,
		"local_date": "2026-11-13", "profile_semantic_version": 0,
		"short_intent_version": 0, "negative_preference_version": 0,
		"generation_config_version":  "topic-starter-market-generation-v10",
		"corpus_version":             "corpus-v1",
		"document_snapshot_version":  "doc-v1",
		"retrieval_config_version":   "ret-v1",
		"evidence_extractor_version": "ext-v1",
		"search_provider_id":         OpenjiuwenNativeSearchProviderID,
		"component_contract_versions": map[string]any{
			"icebreaker_rid": "c1", "interaction_utility_scorer": "c1",
			"verbatim_web_evidence": "c1", "diversity_scorer": "c1",
		},
		"model": "v9",
		"input_revision_receipt": map[string]any{
			"receipt_version": InputRevisionReceiptVersion,
			"context_scope":   GlobalNewChatScope, "revision_digest": "d1",
		},
	}
	return card, trigger
}

// PersistedStateReceiptForV2 合法状态维护小票（经 Build 构造）。
func PersistedStateReceiptForV2(t *testing.T) map[string]any {
	t.Helper()
	state := v4StateWithBehavior(1, "s1", 0)
	receipt, err := BuildStateMaintenanceReceipt(state, "behavior_mapping")
	if err != nil {
		t.Fatalf("receipt: %v", err)
	}
	return receipt
}

// TestValidCard_AcceptAndReject 对齐 :3843-4017 主闸门。
func TestValidCard_AcceptAndReject(t *testing.T) {
	card, trigger := v2Card(t)
	// newsLead=true 时不要求 diversity_scorer 小票
	if !ValidCard(card, trigger, true) {
		t.Fatalf("valid card rejected")
	}
	// newsLead=false 时多出 diversity 也必须在场 → 补齐后通过
	receipt3 := validReceiptFixture("diversity_scorer", "snap-2")
	receipt3["binding"].(map[string]any)["component_id"] = "diversity_scorer"
	if ValidCard(card, trigger, false) {
		t.Fatalf("missing diversity receipt should fail when not news lead")
	}
	card["component_receipts"].(map[string]any)["diversity_scorer"] = receipt3
	if !ValidCard(card, trigger, false) {
		t.Fatalf("complete receipts accepted")
	}
	// 秘密字段拒绝
	card["api_key"] = "sk-1"
	if ValidCard(card, trigger, false) {
		t.Fatalf("secret field should fail")
	}
	delete(card, "api_key")
	// 有效期倒挂
	card["expire_time"] = 50.0
	if ValidCard(card, trigger, false) {
		t.Fatalf("expired ordering should fail")
	}
	card["expire_time"] = 100.0 + 7*24*60*60
	// 空标题
	card["title"] = "  "
	if ValidCard(card, trigger, false) {
		t.Fatalf("blank title should fail")
	}
	card["title"] = "标题"
	// 全局页项目定位器必须为空
	card["project_source_locators"] = []any{map[string]any{"session_id": "s1"}}
	if ValidCard(card, trigger, false) {
		t.Fatalf("global card with project locators should fail")
	}
}

// TestValidScope 对齐 :3266-3273。
func TestValidScope(t *testing.T) {
	if !ValidScope(GlobalNewChatScope, nil) {
		t.Fatalf("global nil project")
	}
	if !ValidScope("project:p1", "p1") {
		t.Fatalf("project scope")
	}
	if ValidScope("project:p1", nil) {
		t.Fatalf("project scope with nil project")
	}
	if ValidScope(GlobalNewChatScope, "p1") {
		t.Fatalf("global with project")
	}
	if ValidScope("project:", nil) {
		t.Fatalf("invalid scope")
	}
}

// TestValidPoolComposition 对齐 :4027-4043。
func TestValidPoolComposition(t *testing.T) {
	card := func(category string) map[string]any {
		return map[string]any{"category": category}
	}
	if !ValidPoolComposition(GlobalNewChatScope, []map[string]any{
		card("profile_recent_progress"), card("adjacent_explanation"),
	}) {
		t.Fatalf("global composition")
	}
	if ValidPoolComposition(GlobalNewChatScope, []map[string]any{}) {
		t.Fatalf("empty pool rejected")
	}
	if ValidPoolComposition(GlobalNewChatScope, []map[string]any{card("unknown")}) {
		t.Fatalf("unknown category rejected")
	}
	// 项目页必须含 project_continuation
	if ValidPoolComposition("project:p1", []map[string]any{card("project_domain_progress")}) {
		t.Fatalf("project pool without continuation rejected")
	}
	if !ValidPoolComposition("project:p1", []map[string]any{
		card("project_continuation"), card("project_domain_progress")}) {
		t.Fatalf("project composition")
	}
}

// TestShortDeltaInterestID 对齐 :4046-4102。
func TestShortDeltaInterestID(t *testing.T) {
	// 无 selection → 无要求
	if id, hasValue, valid := ShortDeltaInterestID(map[string]any{}); !valid ||
		hasValue || id != "" {
		t.Fatalf("no selection: %q %v %v", id, hasValue, valid)
	}
	// 空列表 → 无要求
	id, hasValue, valid := ShortDeltaInterestID(map[string]any{
		"rid_interest_selection": map[string]any{
			"short_trigger_delta_event_ids": []any{}}})
	if !valid || hasValue || id != "" {
		t.Fatalf("empty delta: %q %v %v", id, hasValue, valid)
	}
	// 重复事件 → 契约失败
	id, hasValue, valid = ShortDeltaInterestID(map[string]any{
		"rid_interest_selection": map[string]any{
			"short_trigger_delta_event_ids": []any{"e1", "e1"}}})
	if valid || !hasValue || id != "" {
		t.Fatalf("duplicate: %q %v %v", id, hasValue, valid)
	}
	// 命中链接兴趣
	id, hasValue, valid = ShortDeltaInterestID(map[string]any{
		"rid_interest_selection": map[string]any{
			"short_trigger_delta_event_ids":    []any{"e1"},
			"delta_linked_interest_summary_id": "int-a"},
		"rid_result": map[string]any{"trigger_interests": []any{
			map[string]any{"interest_summary_id": "int-a",
				"source_event_ids": []any{"e1"},
				"source_locators":  []any{map[string]any{"event_id": "e1"}}}}}})
	if !valid || !hasValue || id != "int-a" {
		t.Fatalf("linked: %q %v %v", id, hasValue, valid)
	}
}

// TestCardOpened 对齐 :5239-5256。
func TestCardOpened(t *testing.T) {
	store := newTestStore(t)
	card := map[string]any{"card_revision_id": "rev-1",
		"context_scope": GlobalNewChatScope, "card_id": "cand-1"}
	if CardOpened(store, card) {
		t.Fatalf("no clicks")
	}
	if ok, _ := store.RecordEvent(map[string]any{
		"session_id": "s1", "slate_id": "s1", "event_type": "topic_click",
		"topic_id": "rev-1", "policy_id": POLICY_ID,
		"payload":    map[string]any{"card_revision_id": "rev-1"},
		"created_at": 10.0,
	}); !ok {
		t.Fatalf("record click")
	}
	if !CardOpened(store, card) {
		t.Fatalf("clicked card should be opened")
	}
}
