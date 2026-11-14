package recommendation

// topic_product_validate3b_test.go 对齐校验族 v3b 契约的单元用例。

import (
	"testing"
)

// TestDiversityInvocationCovers 对齐 :2436-2452。
func TestDiversityInvocationCovers(t *testing.T) {
	manifest := map[string]any{
		"component_receipts": map[string]any{
			"diversity_scorer": map[string]any{"request_id": "req-div"},
		},
		"component_invocation_receipts": []any{
			map[string]any{"component_id": "diversity_scorer", "request_id": "req-div",
				"candidate_ids": []any{"c1", "c2"}},
		},
	}
	if !DiversityInvocationCovers(manifest, []string{"c1", "c2"}) {
		t.Fatalf("covered")
	}
	if DiversityInvocationCovers(manifest, []string{"c1"}) {
		t.Fatalf("partial coverage should fail")
	}
	// 请求 ID 不符
	manifest["component_receipts"].(map[string]any)["diversity_scorer"] =
		map[string]any{"request_id": "req-other"}
	if DiversityInvocationCovers(manifest, []string{"c1", "c2"}) {
		t.Fatalf("request id mismatch should fail")
	}
}

// TestValidManifest_RejectionChain 对齐 :4186-4373 主链逐闸。
func TestValidManifest_RejectionChain(t *testing.T) {
	// 非 dict
	if reason := ManifestRejectionReason(nil, nil, false); reason != "manifest_type_contract_failed" {
		t.Fatalf("type: %s", reason)
	}
	// 缺必需字段：按字母序第一个
	manifest := map[string]any{"manifest_id": "m1"}
	reason := ManifestRejectionReason(manifest, nil, false)
	if reason != "manifest_required_field_ark_invocation_receipts_missing" {
		t.Fatalf("missing field: %s", reason)
	}
	// policy_id 错误
	full := validManifestSkeleton()
	full["policy_id"] = "wrong"
	if reason := ManifestRejectionReason(full, nil, false); reason != "manifest_field_policy_id_contract_failed" {
		t.Fatalf("policy: %s", reason)
	}
	// trigger_origin 非法
	full = validManifestSkeleton()
	full["trigger_origin"] = "unknown"
	if reason := ManifestRejectionReason(full, nil, false); reason != "manifest_field_trigger_origin_contract_failed" {
		t.Fatalf("origin: %s", reason)
	}
}

// validManifestSkeleton v3b manifest 骨架（过类型/字段闸的最小形状）。
func validManifestSkeleton() map[string]any {
	manifest := map[string]any{}
	for field := range manifestFields {
		manifest[field] = nil
	}
	manifest["policy_id"] = POLICY_ID
	manifest["payload_version"] = PAYLOAD_VERSION
	manifest["trigger_origin"] = "scheduled_cron"
	manifest["context_scope"] = GlobalNewChatScope
	manifest["project_id"] = nil
	manifest["generation_trigger_key"] = map[string]any{}
	manifest["component_receipts"] = map[string]any{}
	manifest["generation_input_snapshot_id"] = "snap-x"
	manifest["state_maintenance_receipt"] = map[string]any{}
	return manifest
}

// TestLoadCardRevisions 对齐 :5151-5175。
func TestLoadCardRevisions(t *testing.T) {
	store := newTestStore(t)
	manifest := map[string]any{
		"manifest_id": "m1", "generation_batch_id": "b1",
		"card_revision_ids": []any{"rev-1", "rev-2"},
	}
	// 无卡 → 空表
	if cards := LoadCardRevisions(store, manifest); len(cards) != 0 {
		t.Fatalf("no cards: %v", cards)
	}
	// 落两张卡（一代批次）
	for _, revision := range []string{"rev-1", "rev-2"} {
		if ok, _ := store.RecordEvent(map[string]any{
			"session_id": "s1", "slate_id": "m1", "event_type": "starter_card",
			"topic_id": revision, "policy_id": POLICY_ID,
			"payload": map[string]any{"card_revision_id": revision,
				"generation_batch_id": "b1"},
			"created_at": 10.0,
		}); !ok {
			t.Fatalf("record card %s", revision)
		}
	}
	cards := LoadCardRevisions(store, manifest)
	if len(cards) != 2 {
		t.Fatalf("cards: %d", len(cards))
	}
	// 批次不符 → 空表
	badManifest := map[string]any{
		"manifest_id": "m1", "generation_batch_id": "b2",
		"card_revision_ids": []any{"rev-1"},
	}
	if cards := LoadCardRevisions(store, badManifest); len(cards) != 0 {
		t.Fatalf("batch mismatch: %v", cards)
	}
	// manifest revision 缺失 → 空表
	shortManifest := map[string]any{
		"manifest_id": "m1", "generation_batch_id": "b1",
		"card_revision_ids": []any{"rev-1", "rev-missing"},
	}
	if cards := LoadCardRevisions(store, shortManifest); len(cards) != 0 {
		t.Fatalf("missing revision: %v", cards)
	}
}

// TestCompatiblePool_NegativePreferenceGate 对齐 :5465-5509 版本上界。
func TestCompatiblePool_NegativePreferenceGate(t *testing.T) {
	store := newTestStore(t)
	key := map[string]any{"k": "v"}
	manifest := map[string]any{
		"manifest_id": "m2", "generation_batch_id": "b1",
		"negative_preference_version": 3,
		"serving_compatibility_key":   key,
		"context_scope":               GlobalNewChatScope,
		"project_id":                  nil,
	}
	// 冷启动形态以通过可读闸
	manifest["cold_start"] = true
	manifest["card_revision_ids"] = []any{"rev-1"}
	if ok, _ := store.RecordEvent(map[string]any{
		"session_id": "s1", "slate_id": "m2", "event_type": "starter_slate_manifest",
		"topic_id": "m2", "policy_id": POLICY_ID, "payload": manifest,
		"created_at": 10.0,
	}); !ok {
		t.Fatalf("record manifest")
	}
	if ok, _ := store.RecordEvent(map[string]any{
		"session_id": "s1", "slate_id": "m2", "event_type": "starter_card",
		"topic_id": "rev-1", "policy_id": POLICY_ID,
		"payload": map[string]any{"card_revision_id": "rev-1",
			"generation_batch_id": "b1", "cold_start": true,
			"title": "t", "prompt": "p", "context_scope": GlobalNewChatScope},
		"created_at": 10.0,
	}); !ok {
		t.Fatalf("record card")
	}
	// 反馈版本 5 ≥ manifest 3 → 可读
	foundManifest, cards := CompatiblePool(store, key, GlobalNewChatScope, nil, 5)
	if foundManifest == nil || len(cards) != 1 {
		t.Fatalf("compatible: %v %d", foundManifest, len(cards))
	}
	// 反馈版本 2 < 3 → 不可见
	if m2, cards2 := CompatiblePool(store, key, GlobalNewChatScope, nil, 2); m2 != nil || len(cards2) != 0 {
		t.Fatalf("older feedback version should not see newer pool")
	}
}
