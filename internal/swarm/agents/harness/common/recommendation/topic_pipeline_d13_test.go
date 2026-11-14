package recommendation

// topic_pipeline_d13_test.go 对齐 topic_pipeline.py:4908-5267/:5268-5393
// 契约的单元用例。

import (
	"testing"
)

// TestGenerate_ScopeMismatch 对齐 :4918-4920。
func TestGenerate_ScopeMismatch(t *testing.T) {
	store := newTestStore(t)
	pipeline := NewTopicPipeline(store, nil, nil, nil, nil, "", nil, nil, nil, nil, nil,
		func() float64 { return 1000.0 })
	result := pipeline.Generate(map[string]any{}, "scheduled_cron",
		&ArkBackgroundConfig{}, GlobalNewChatScope, "p1", "visit-1")
	if result.FailureReason != "scope_mismatch" {
		t.Fatalf("reason: %s", result.FailureReason)
	}
	if len(result.Cards) != 0 {
		t.Fatalf("cards: %d", len(result.Cards))
	}
}

// TestGenerate_VersionMismatch 对齐 :4943-5003 五重版本对拍。
func TestGenerate_VersionMismatch(t *testing.T) {
	store := newTestStore(t)
	pipeline := NewTopicPipeline(store, func(scope string) *V6ContextSnapshot {
		return &V6ContextSnapshot{
			ContextScope: scope, InputSnapshotID: "snap-gen",
			ProfileProjection: map[string]any{"profile_semantic_version": 7},
			ShortIntentState:  &ShortIntentState{ShortIntentVersion: 3},
			StateMaintenanceReceipt: map[string]any{
				"negative_preference_version": 2},
		}
	}, nil, nil, nil, "", nil, nil, nil, nil, nil, func() float64 { return 1000.0 })
	feedbackState := map[string]any{}
	_ = feedbackState
	// trigger 画像版本不符
	result := pipeline.Generate(map[string]any{
		"profile_semantic_version": 9, "short_intent_version": 3,
		"negative_preference_version": 2,
		"search_provider_id":          INJECTEDSearchAuditProviderID,
	}, "scheduled_cron", &ArkBackgroundConfig{}, GlobalNewChatScope, nil, "visit-1")
	if result.FailureReason != "generation_input_version_mismatch" {
		t.Fatalf("profile mismatch: %s", result.FailureReason)
	}
	if result.Manifest["input_snapshot_id"] != "snap-gen" {
		t.Fatalf("snapshot id: %v", result.Manifest["input_snapshot_id"])
	}
	// 短意图版本不符
	result = pipeline.Generate(map[string]any{
		"profile_semantic_version": 7, "short_intent_version": 1,
		"negative_preference_version": 2,
		"search_provider_id":          INJECTEDSearchAuditProviderID,
	}, "scheduled_cron", &ArkBackgroundConfig{}, GlobalNewChatScope, nil, "visit-1")
	if result.FailureReason != "generation_input_version_mismatch" {
		t.Fatalf("short intent mismatch: %s", result.FailureReason)
	}
	// 否定偏好版本不符
	result = pipeline.Generate(map[string]any{
		"profile_semantic_version": 7, "short_intent_version": 3,
		"negative_preference_version": 5,
		"search_provider_id":          INJECTEDSearchAuditProviderID,
	}, "scheduled_cron", &ArkBackgroundConfig{}, GlobalNewChatScope, nil, "visit-1")
	if result.FailureReason != "generation_input_version_mismatch" {
		t.Fatalf("negative version mismatch: %s", result.FailureReason)
	}
}

// TestGenerate_SearchProviderMismatch 对齐 :5063-5087 三检。
func TestGenerate_SearchProviderMismatch(t *testing.T) {
	store := newTestStore(t)
	pipeline := NewTopicPipeline(store, func(scope string) *V6ContextSnapshot {
		return &V6ContextSnapshot{
			ContextScope: scope, InputSnapshotID: "snap-gen",
			ProfileProjection: map[string]any{"profile_semantic_version": 0},
			ShortIntentState:  &ShortIntentState{},
			StateMaintenanceReceipt: map[string]any{
				"negative_preference_version": 0},
		}
	}, nil, nil, nil, "ark-x", nil, nil, nil, nil, nil, func() float64 { return 1000.0 })
	// 触发键厂商与注入身份不符
	result := pipeline.Generate(map[string]any{
		"profile_semantic_version": 0, "short_intent_version": 0,
		"negative_preference_version": 0,
		"search_provider_id":          "other",
	}, "scheduled_cron", &ArkBackgroundConfig{}, GlobalNewChatScope, nil, "visit-1")
	if result.FailureReason != "search_provider_version_mismatch" {
		t.Fatalf("provider mismatch: %s", result.FailureReason)
	}
	// 三检一致但摘要批次后 RID 放弃链路 → 走到 deeper（此处仅验证通过三检后
	// 不再报 search_provider_version_mismatch）
	result = pipeline.Generate(map[string]any{
		"profile_semantic_version": 0, "short_intent_version": 0,
		"negative_preference_version": 0,
		"search_provider_id":          INJECTEDSearchAuditProviderID,
	}, "scheduled_cron", &ArkBackgroundConfig{}, GlobalNewChatScope, nil, "visit-1")
	if result.FailureReason == "search_provider_version_mismatch" {
		t.Fatalf("consistent identity should pass the checks")
	}
}

// TestRefreshNews_NoNewsCard 对齐 :5292-5293。
func TestRefreshNews_NoNewsCard(t *testing.T) {
	store := newTestStore(t)
	pipeline := NewTopicPipeline(store, nil, nil, nil, nil, "", nil, nil, nil, nil, nil,
		func() float64 { return 1000.0 })
	// 不存在 manifest
	result := pipeline.RefreshNews(&ArkBackgroundConfig{}, "m-none", "visit-1")
	if result.FailureReason != "no_news_card_to_replace" {
		t.Fatalf("reason: %s", result.FailureReason)
	}
	// 落一个无新闻卡的池
	manifest := map[string]any{
		"manifest_id": "m-13", "generation_batch_id": "b-13",
		"card_revision_ids": []any{"rev-13"},
		"cold_start":        true, "context_scope": GlobalNewChatScope,
	}
	card := map[string]any{
		"card_id": "cand-13", "card_revision_id": "rev-13",
		"generation_batch_id": "b-13", "cold_start": true,
		"title": "t", "prompt": "p", "context_scope": GlobalNewChatScope,
	}
	_, _ = store.RecordEvent(map[string]any{
		"session_id": "s1", "slate_id": "m-13", "event_type": "starter_slate_manifest",
		"topic_id": "m-13", "policy_id": POLICY_ID, "payload": manifest,
		"created_at": 10.0})
	_, _ = store.RecordEvent(map[string]any{
		"session_id": "s1", "slate_id": "m-13", "event_type": "starter_card",
		"topic_id": "rev-13", "policy_id": POLICY_ID, "payload": card,
		"created_at": 10.0})
	result = pipeline.RefreshNews(&ArkBackgroundConfig{}, "m-13", "visit-1")
	if result.FailureReason != "no_news_card_to_replace" {
		t.Fatalf("no lead: %s", result.FailureReason)
	}
}

// TestLoadExactGlobalPool_Rejections 对齐 topic_product.py:4553-4637。
func TestLoadExactGlobalPool_Rejections(t *testing.T) {
	store := newTestStore(t)
	// manifest ID 不存在
	if manifest, cards := LoadExactGlobalPool(store, "m-x"); manifest != nil || cards != nil {
		t.Fatalf("missing manifest")
	}
	// 重复行 → None
	for index := 0; index < 2; index++ {
		_, _ = store.RecordEvent(map[string]any{
			"session_id": "s1", "slate_id": "m-dup", "event_type": "starter_slate_manifest",
			"topic_id": "m-dup", "policy_id": POLICY_ID,
			"payload": map[string]any{"manifest_id": "m-dup",
				"context_scope": GlobalNewChatScope},
			"created_at": float64(10 + index),
		})
	}
	if manifest, _ := LoadExactGlobalPool(store, "m-dup"); manifest != nil {
		t.Fatalf("duplicated manifest should fail")
	}
}
