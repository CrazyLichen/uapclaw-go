package recommendation

// topic_product_validate3a_test.go 对齐校验族 v3a 契约的单元用例。

import (
	"strings"
	"testing"
)

// v3aManifest 合法 manifest 骨架（最小合法形状）。
func v3aManifest() map[string]any {
	receipt := validReceiptFixture("icebreaker_rid", "snap-3")
	receipt2 := validReceiptFixture("interaction_utility_scorer", "snap-3")
	receipt2["binding"].(map[string]any)["component_id"] = "interaction_utility_scorer"
	invocation := map[string]any{
		"schema_version": ComponentInvocationReceiptVersion,
		"component_id":   "icebreaker_rid", "request_id": "req-1",
		"input_snapshot_id": "snap-3", "binding_scope": "all_cards",
		"candidate_ids": []any{}, "starter_ids": []any{},
	}
	card := map[string]any{
		"card_id": "cand-1", "card_revision_id": "rev-1", "generation_batch_id": "b-1",
		"selector_rank": 1, "category": "tech", "context_scope": GlobalNewChatScope,
		"project_id":                    nil,
		"component_receipts":            map[string]any{"icebreaker_rid": receipt},
		"component_invocation_receipts": []any{invocation},
	}
	return map[string]any{
		"component_receipts":            map[string]any{"icebreaker_rid": receipt},
		"component_invocation_receipts": []any{invocation},
		"ark_invocation_receipts":       []any{validProviderReceiptFixture("v6_session_summarizer")},
		"cards":                         []any{},
		"_test_cards":                   []any{card},
		"card_under_test":               card,
		"receipt2":                      receipt2,
	}
}

// validProviderReceiptFixture 合法厂商小票。
func validProviderReceiptFixture(stage string) map[string]any {
	bindingScope, candidateIDs, starterIDs := "all_cards", []any{}, []any{}
	switch {
	case candidateProviderStages[stage]:
		bindingScope = "candidate"
		candidateIDs = []any{"cand-1"}
	case starterProviderStages[stage]:
		bindingScope = "starter"
	}
	return map[string]any{
		"stage": stage, "provider_response_id": "resp-" + stage,
		"response_object": "chat.completion", "actual_model": "v9",
		"finish_reason": "stop",
		"usage":         map[string]any{"input_tokens": 10, "output_tokens": 5, "total_tokens": 15},
		"latency_ms":    120.5, "structured_output_mode": "json_schema_strict",
		"lineage": map[string]any{
			"binding_scope": bindingScope, "candidate_ids": candidateIDs,
			"starter_ids": starterIDs, "component_request_id": "req-1",
			"input_snapshot_id": "snap-3",
		},
	}
}

// TestNewsSlotRejectionReason 对齐 :4136-4185。
func TestNewsSlotRejectionReason(t *testing.T) {
	// 无 news_slot 键 → 不校验
	if reason := NewsSlotRejectionReason(map[string]any{}, nil); reason != "" {
		t.Fatalf("absent key: %s", reason)
	}
	// 缺席原因合法且无新闻卡 → 通过
	manifest := map[string]any{
		"context_scope": GlobalNewChatScope,
		"news_slot": map[string]any{
			"candidate_id": nil, "card_form": nil, "match": nil,
			"hot_entry_id": nil, "hot_list_pull_receipt_id": nil,
			"absent_reason": "hot_list_unavailable",
		},
	}
	if reason := NewsSlotRejectionReason(manifest, nil); reason != "" {
		t.Fatalf("absent reason: %s", reason)
	}
	// 缺席原因但池里有新闻卡 → fail
	manifest["news_slot"].(map[string]any)["absent_reason"] = "hot_list_unavailable"
	newsCard := map[string]any{
		"card_id": "n1", "evidence_bundle": map[string]any{"source_kind": NewsSourceKind}}
	if reason := NewsSlotRejectionReason(manifest, []map[string]any{newsCard}); reason == "" {
		t.Fatalf("news card with absent reason should fail")
	}
	// 项目 scope → fail
	projectManifest := map[string]any{
		"context_scope": "project:p1", "news_slot": map[string]any{}}
	if reason := NewsSlotRejectionReason(projectManifest, nil); reason == "" {
		t.Fatalf("project scope should fail")
	}
}

// TestValidComponentInvocation 对齐 :2777-2817。
func TestValidComponentInvocation(t *testing.T) {
	invocation := map[string]any{
		"schema_version": ComponentInvocationReceiptVersion,
		"component_id":   "icebreaker_rid", "request_id": "req-1",
		"input_snapshot_id": "snap-3", "binding_scope": "all_cards",
		"candidate_ids": []any{}, "starter_ids": []any{},
	}
	if !ValidComponentInvocation(invocation) {
		t.Fatalf("valid invocation rejected")
	}
	invocation["binding_scope"] = "candidate"
	if ValidComponentInvocation(invocation) {
		t.Fatalf("rid must be all_cards")
	}
	invocation["binding_scope"] = "all_cards"
	invocation["candidate_ids"] = []any{"c1"}
	if ValidComponentInvocation(invocation) {
		t.Fatalf("all_cards with candidates should fail")
	}
}

// TestComponentLineageRejectionReason 对齐 :2845-2937。
func TestComponentLineageRejectionReason(t *testing.T) {
	data := v3aManifest()
	card := data["card_under_test"].(map[string]any)
	manifest := map[string]any{
		"component_invocation_receipts": data["component_invocation_receipts"],
		"component_receipts":            data["component_receipts"],
	}
	// 卡的调用表需含同一 all_cards 调用
	card["component_invocation_receipts"] = data["component_invocation_receipts"]
	if reason := ComponentLineageRejectionReason(manifest, []map[string]any{card}); reason != "" {
		t.Fatalf("valid lineage rejected: %s", reason)
	}
	// 卡缺调用表 → fail
	card["component_invocation_receipts"] = []any{}
	if reason := ComponentLineageRejectionReason(manifest, []map[string]any{card}); reason == "" {
		t.Fatalf("card without invocations should fail")
	}
}

// TestValidProviderReceipt 对齐 :2939-3086。
func TestValidProviderReceipt(t *testing.T) {
	receipt := validProviderReceiptFixture("v6_session_summarizer")
	if !ValidProviderReceipt(receipt) {
		t.Fatalf("valid receipt rejected")
	}
	// latency 负数
	receipt["latency_ms"] = -1.0
	if ValidProviderReceipt(receipt) {
		t.Fatalf("negative latency should fail")
	}
	receipt["latency_ms"] = 120.5
	// finish_reason 非法
	receipt["finish_reason"] = "length"
	if ValidProviderReceipt(receipt) {
		t.Fatalf("finish_reason should be stop")
	}
	receipt["finish_reason"] = "stop"
	// 候选阶段空 candidate_ids 拒绝
	candidateReceipt := validProviderReceiptFixture("v6_utility_scorer")
	candidateReceipt["lineage"].(map[string]any)["candidate_ids"] = []any{}
	if ValidProviderReceipt(candidateReceipt) {
		t.Fatalf("candidate stage with empty ids should fail")
	}
	// 复用标记只归 rid
	receipt["reused_for_input_snapshot_id"] = "snap-9"
	if ValidProviderReceipt(receipt) {
		t.Fatalf("reused marker on summarizer should fail")
	}
	delete(receipt, "reused_for_input_snapshot_id")
	// 秘密字段
	receipt["api_key"] = "sk"
	if ValidProviderReceipt(receipt) {
		t.Fatalf("secret field should fail")
	}
}

// TestProviderLineageRejectionReason 对齐 :3087-3189。
func TestProviderLineageRejectionReason(t *testing.T) {
	providerReceipt := validProviderReceiptFixture("v6_session_summarizer")
	manifest := map[string]any{
		"ark_invocation_receipts": []any{providerReceipt},
	}
	card := map[string]any{
		"card_id": "cand-1", "category": "tech", "generation_input_snapshot_id": "snap-3",
		"provider_invocation_receipts": []any{providerReceipt},
		"component_receipts":           map[string]any{},
	}
	if reason := ProviderLineageRejectionReason(manifest, []map[string]any{card}); reason != "" {
		t.Fatalf("valid lineage rejected: %s", reason)
	}
	// 卡缺厂商小票 → fail
	card["provider_invocation_receipts"] = []any{}
	if reason := ProviderLineageRejectionReason(manifest, []map[string]any{card}); reason == "" {
		t.Fatalf("card without provider receipts should fail")
	}
	// 模型不一致
	card["provider_invocation_receipts"] = []any{providerReceipt}
	other := validProviderReceiptFixture("v6_session_summarizer")
	other["actual_model"] = "v8"
	manifest["ark_invocation_receipts"] = []any{providerReceipt, other}
	if reason := ProviderLineageRejectionReason(manifest, []map[string]any{card}); reason == "" {
		t.Fatalf("mixed models should fail")
	}
	_ = strings.TrimSpace("")
}

// TestTerminalModelBindingRejectionReason 对齐 :3192-3240。
func TestTerminalModelBindingRejectionReason(t *testing.T) {
	providerReceipt := validProviderReceiptFixture("v6_session_summarizer")
	manifest := map[string]any{
		"ark_invocation_receipts":   []any{providerReceipt},
		"component_receipts":        map[string]any{},
		"session_summary_artifacts": []any{},
		"search_receipts":           map[string]any{},
	}
	card := map[string]any{
		"component_receipts":           map[string]any{},
		"provider_invocation_receipts": []any{providerReceipt},
		"search_receipt":               nil,
	}
	if reason := TerminalModelBindingRejectionReason(manifest, []map[string]any{card}, "v9"); reason != "" {
		t.Fatalf("valid binding rejected: %s", reason)
	}
	// 期望模型为空
	if reason := TerminalModelBindingRejectionReason(manifest, []map[string]any{card}, "  "); reason != "expected_model_contract_failed" {
		t.Fatalf("empty model: %s", reason)
	}
	// 厂商小票模型不一致
	other := validProviderReceiptFixture("v6_session_summarizer")
	other["actual_model"] = "v8"
	manifest["ark_invocation_receipts"] = []any{other}
	if reason := TerminalModelBindingRejectionReason(manifest, []map[string]any{card}, "v9"); reason != "provider_model_binding_contract_failed" {
		t.Fatalf("model mismatch: %s", reason)
	}
}
