package recommendation

// topic_product_validate1_test.go 对齐 topic_product.py 校验族 v1 契约的用例。

import (
	"testing"
)

// validReceiptFixture 合法组件小票。
func validReceiptFixture(componentID, inputSnapshotID string) map[string]any {
	return map[string]any{
		"receipt_version": "v1",
		"binding": map[string]any{
			"component_id": componentID, "contract_version": "c1",
			"provider_id": "ark", "provider_version": "v9", "config_version": "cfg",
		},
		"input_snapshot_id": inputSnapshotID, "request_id": "req-1",
		"score_semantics": "uncalibrated_api_proxy", "output_status": "success",
		"fallback_reason": nil,
	}
}

// TestValidReceipt 对齐 :3392-3417。
func TestValidReceipt(t *testing.T) {
	receipt := validReceiptFixture("icebreaker_rid", "snap-1")
	if !ValidReceipt(receipt, "icebreaker_rid", "snap-1") {
		t.Fatalf("valid receipt rejected")
	}
	if ValidReceipt(receipt, "diversity_scorer", "snap-1") {
		t.Fatalf("component mismatch should fail")
	}
	receipt["output_status"] = "fallback"
	if ValidReceipt(receipt, "icebreaker_rid", "snap-1") {
		t.Fatalf("non-success output should fail")
	}
	receipt["output_status"] = "success"
	receipt["fallback_reason"] = "x"
	if ValidReceipt(receipt, "icebreaker_rid", "snap-1") {
		t.Fatalf("fallback reason set should fail")
	}
}

// TestValidStateMaintenanceReceipt 对齐 :3459-3530。
func TestValidStateMaintenanceReceipt(t *testing.T) {
	state := v4StateWithBehavior(1, "s1", 0)
	receipt, err := BuildStateMaintenanceReceipt(state, "behavior_mapping")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if !ValidStateMaintenanceReceipt(receipt, 0, 0) {
		t.Fatalf("valid receipt rejected: %v", receipt)
	}
	receipt["mapping_watermark"] = 99.0
	if ValidStateMaintenanceReceipt(receipt, 0, 0) {
		t.Fatalf("mapping watermark > source watermark should fail")
	}
}

// TestValidEvidence 对齐 :3727-3760。
func TestValidEvidence(t *testing.T) {
	card := func(bundle map[string]any) map[string]any {
		return map[string]any{"category": "tech", "evidence_bundle": bundle}
	}
	good := card(map[string]any{
		"fetch_status": "success", "source_url": "https://e", "source_kind": "stable",
		"document_revision_id": "doc-1",
		"content_boundary":     map[string]any{"text": "证据", "start": 0, "end": 2},
	})
	if !ValidEvidence(good) {
		t.Fatalf("good evidence rejected")
	}
	// start/end 与文本长度不一致
	bad := card(map[string]any{
		"fetch_status": "success", "source_url": "https://e", "source_kind": "stable",
		"document_revision_id": "doc-1",
		"content_boundary":     map[string]any{"text": "证据", "start": 0, "end": 3},
	})
	if ValidEvidence(bad) {
		t.Fatalf("boundary mismatch should fail")
	}
	// 非近期类缺发布时间档位：source_kind 非三档 → fail
	unknown := card(map[string]any{
		"fetch_status": "success", "source_url": "https://e", "source_kind": "social",
		"document_revision_id": "doc-1",
		"content_boundary":     map[string]any{"text": "证据", "start": 0, "end": 2},
	})
	if ValidEvidence(unknown) {
		t.Fatalf("unknown source kind should fail")
	}
}

// TestValidProfileInterestHostReceipt 对齐 :3579-3604。
func TestValidProfileInterestHostReceipt(t *testing.T) {
	card := map[string]any{
		"category":                     ProfileInterestCategory,
		"interest_summary_id":          "int-a",
		"generation_input_snapshot_id": "snap-1",
		"search_receipt":               nil,
		"evidence_bundle": map[string]any{
			"document_revision_id": "doc-1", "source_url": "profile://int-a",
			"host_receipt": map[string]any{
				"receipt_version": ProfileInterestReceiptVersion, "lane": "host_profile_interest_locator",
				"input_snapshot_id": "snap-1", "interest_summary_id": "int-a",
				"document_revision_id": "doc-1",
			},
		},
	}
	if !ValidProfileInterestHostReceipt(card) {
		t.Fatalf("valid receipt rejected")
	}
	bundle := card["evidence_bundle"].(map[string]any)
	bundle["source_url"] = "profile://int-b"
	if ValidProfileInterestHostReceipt(card) {
		t.Fatalf("url mismatch should fail")
	}
}

// TestReceiptsMatchTrigger 对齐 :3711-3725。
func TestReceiptsMatchTrigger(t *testing.T) {
	trigger := map[string]any{
		"model": "v9",
		"component_contract_versions": map[string]any{
			"icebreaker_rid": "c1", "diversity_scorer": "c2"},
	}
	receipts := map[string]any{
		"icebreaker_rid":   validReceiptFixture("icebreaker_rid", "snap"),
		"diversity_scorer": validReceiptFixture("diversity_scorer", "snap"),
	}
	// 小票契约都是 c1；diversity 期望 c2 → 不匹配
	if ReceiptsMatchTrigger(receipts, trigger) {
		t.Fatalf("contract mismatch should fail")
	}
	receipts["diversity_scorer"] = map[string]any{
		"receipt_version": "v1",
		"binding": map[string]any{
			"component_id": "diversity_scorer", "contract_version": "c2",
			"provider_id": "ark", "provider_version": "v9", "config_version": "cfg",
		},
		"input_snapshot_id": "snap", "request_id": "r",
		"score_semantics": "uncalibrated_api_proxy", "output_status": "success",
		"fallback_reason": nil,
	}
	if !ReceiptsMatchTrigger(receipts, trigger) {
		t.Fatalf("matching receipts rejected")
	}
}

// TestValidHotListHostReceipt 对齐 :3607-3665。
func TestValidHotListHostReceipt(t *testing.T) {
	card := map[string]any{
		"category":                     "profile_recent_progress",
		"generation_input_snapshot_id": "snap-1",
		"search_receipt":               nil,
		"evidence_bundle": map[string]any{
			"source_kind": HotListSourceKind, "source_url": "https://hot", "document_revision_id": "doc-1",
			"host_receipt": map[string]any{
				"receipt_version": HotListEntryReceiptVersion, "lane": "host_hot_list_entry",
				"input_snapshot_id": "snap-1", "pull_receipt_id": "pull-1",
				"provider_id": "bing", "list_url": "https://list",
				"hot_entry_id": "h1", "hot_list_rank": 2, "document_revision_id": "doc-1",
			},
		},
	}
	if !ValidHotListHostReceipt(card) {
		t.Fatalf("valid receipt rejected")
	}
	bundle := card["evidence_bundle"].(map[string]any)
	receipt := bundle["host_receipt"].(map[string]any)
	receipt["hot_list_rank"] = 0
	if ValidHotListHostReceipt(card) {
		t.Fatalf("zero rank should fail")
	}
}
