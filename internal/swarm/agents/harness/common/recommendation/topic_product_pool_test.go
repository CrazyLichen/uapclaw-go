package recommendation

import (
	"strings"
	"testing"
)

// poolCSTriggerKey 合法触发键（冷启动卡字段来源）。
func poolCSTriggerKey() map[string]any {
	return map[string]any{
		"local_date":                 "2026-11-12",
		"search_provider_id":         OpenjiuwenNativeSearchProviderId,
		"profile_semantic_version":   "profile-v1",
		"short_intent_version":       "short-v1",
		"negative_preference_version": "neg-v1",
		"generation_config_version":  "gen-v19",
		"corpus_version":             CorpusVersion,
		"document_snapshot_version":  "doc-v1",
		"retrieval_config_version":   "ret-v1",
		"evidence_extractor_version": "ev-v1",
		"input_revision_receipt":     map[string]any{"receipt_id": "snap-1"},
	}
}

// TestCorpusCategory 分类（对齐 :4735-4742）。
func TestCorpusCategory(t *testing.T) {
	if CorpusCategory("科学") != "adjacent_explanation" || CorpusCategory("文化") != "adjacent_explanation" ||
		CorpusCategory("好奇") != "adjacent_explanation" || CorpusCategory("科技") != "broad_timely_practical" {
		t.Fatal("分类不符")
	}
}

// TestCorpusStarterIsLive 与过期时间取 min（对齐 :4743-4757）。
func TestCorpusStarterIsLive(t *testing.T) {
	live := CorpusStarterIsLive(&FixedStarter{ExpiresAt: nil}, 100)
	if !live {
		t.Fatal("无过期时间必须存活")
	}
	dead := CorpusStarterIsLive(&FixedStarter{ExpiresAt: "2026-01-01T00:00:00Z"}, 1800000000)
	if dead {
		t.Fatal("过期必须死亡")
	}
	expire := CorpusCardExpireTime(&FixedStarter{ExpiresAt: "2020-01-01T00:00:00Z"}, 1800000000)
	if expire >= 1800000000+7*24*3600 {
		t.Fatal("过期时间必须取 min")
	}
}

// TestColdStartCorpusCard 语料卡字段与 copy 校验拒绝（对齐 :4759-4847）。
func TestColdStartCorpusCard(t *testing.T) {
	starter := &FixedStarter{StarterID: "s-1", Text: "想了解什么是 WebAssembly 吗", Topic: "科技",
		CorpusVersion: CorpusVersion, ExpiresAt: nil}
	card := ColdStartCorpusCard(starter, 1, "batch-1", poolCSTriggerKey(), 1800000000)
	if card == nil {
		t.Fatal("合法文本必须出卡")
	}
	if card["card_id"] != "cold-corpus:s-1" || card["category"] != "broad_timely_practical" ||
		card["cold_start"] != true || card["context_scope"] != GlobalNewChatScope {
		t.Fatalf("card=%v", card)
	}
	bundle := card["evidence_bundle"].(map[string]any)
	if bundle["source_kind"] != "stable" || bundle["publisher"] != "packaged_corpus" {
		t.Fatalf("bundle=%v", bundle)
	}
	host := bundle["host_receipt"].(map[string]any)
	if host["receipt_version"] != FixedCorpusReceiptVersion || host["starter_id"] != "s-1" {
		t.Fatalf("host=%v", host)
	}
	// 错误标签词开头 → 拒卡
	bad := ColdStartCorpusCard(&FixedStarter{StarterID: "s-2", Text: "错误：这是一个标题"}, 1, "batch-1",
		poolCSTriggerKey(), 1800000000)
	if bad != nil {
		t.Fatalf("标签词开头必须拒卡: %v", bad)
	}
}

// TestColdStartHotListCard 热榜卡字段（对齐 :4852-4949）。
func TestColdStartHotListCard(t *testing.T) {
	entry := &HotListEntry{EntryID: "weibo-hot-band:1", ProviderID: WeiboProviderID, Rank: 1,
		Word: "AI芯片", Summary: "新发布"}
	pull := &HotListPull{ReceiptID: "pull-1", ProviderID: WeiboProviderID,
		ListURL: "https://weibo.com/ajax/statuses/hot_band", PulledAt: 1800000000}
	card := ColdStartHotListCard(entry, pull, 1, "batch-1", poolCSTriggerKey(), 1800000000)
	if card == nil {
		t.Fatal("合法词条必须出卡")
	}
	if card["card_id"] != "cold-hot:weibo-hot-band:1" || card["category"] != "profile_recent_progress" ||
		card["news_match"] != "closest" {
		t.Fatalf("card=%v", card)
	}
	if pyStr(card["title"]) != "热搜上的「AI芯片」是怎么回事？" {
		t.Fatalf("title=%q", card["title"])
	}
	bundle := card["evidence_bundle"].(map[string]any)
	if bundle["source_kind"] != HotListSourceKind || bundle["publisher"] != "weibo.com" {
		t.Fatalf("bundle=%v", bundle)
	}
}

// TestWriteTerminalPool_拒绝路径 整池拒绝（对齐 :4520-4551 + :4671-4734）。
func TestWriteTerminalPool_拒绝路径(t *testing.T) {
	store := newTestStore(t)
	card := map[string]any{
		"card_id": "c-1", "card_revision_id": "r-1", "generation_batch_id": "b-1",
		"selector_rank": 1, "category": "profile_recent_progress",
		"title": "标题", "prompt": "提示", "context_scope": GlobalNewChatScope,
		"evidence_bundle": map[string]any{},
		"search_provider_id": OpenjiuwenNativeSearchProviderId, "search_receipt": nil,
	}
	manifest := map[string]any{
		"manifest_id": "m-1", "generation_batch_id": "b-1",
		"card_revision_ids": []any{"r-1"}, "context_scope": GlobalNewChatScope,
		"generation_trigger_key": map[string]any{"search_provider_id": OpenjiuwenNativeSearchProviderId},
		"search_provider_id":     OpenjiuwenNativeSearchProviderId,
		"search_receipts":        map[string]any{"c-1": nil},
		"grounding_evidence_receipt": map[string]any{"candidates": []any{}},
		"external_invocation_audit":  map[string]any{"invocations": []any{}},
		"profile_semantic_version":   "profile-v1",
	}
	result := WriteTerminalPool(store, []map[string]any{card}, manifest, 1800000000.0, "m-1")
	if result.Bool() || result.Reason == "" {
		t.Fatalf("缺字段 manifest 必须整池拒绝: %+v", result)
	}
}

// TestSelectRegularExposure 环序/方向分散/新闻首槽（对齐 :5363-5430）。
func TestSelectRegularExposure(t *testing.T) {
	now := 1800000000.0
	newCard := func(id string, revision string, interest string, news bool, rank int) map[string]any {
		card := map[string]any{
			"card_id": id, "card_revision_id": revision, "interest_summary_id": interest,
			"selector_rank": rank, "direction": "",
		}
		if news {
			card["evidence_bundle"] = map[string]any{"source_kind": "news_report", "published_at": isoTime(now-1800)}
		} else {
			card["evidence_bundle"] = map[string]any{"source_kind": "stable"}
		}
		return card
	}
	cards := []map[string]any{
		newCard("news", "rev-news", "", true, 1),
		newCard("a", "rev-a", "int-1", false, 2),
		newCard("b", "rev-b", "int-1", false, 3),
		newCard("c", "rev-c", "int-2", false, 4),
	}
	eligible := map[string]bool{"rev-news": true, "rev-a": true, "rev-b": true, "rev-c": true}
	selected := SelectRegularExposure(cards, eligible, GlobalNewChatScope, 0, now, map[string]bool{})
	// 首屏 4 卡：新闻首槽 + spread 两卡（a 与 b 同方向只取其一）+ fill 补位
	if len(selected) != 4 {
		t.Fatalf("selected=%d: %v", len(selected), selected)
	}
	if pyStr(selected[0]["card_id"]) != "news" {
		t.Fatalf("首槽必须是新闻: %v", selected[0])
	}
	if pyStr(selected[1]["card_id"]) == "b" {
		t.Fatal("spread 阶段不得先取同方向第二张")
	}
	// 空可集 → 空
	if got := SelectRegularExposure(cards, map[string]bool{}, GlobalNewChatScope, 0, now, nil); len(got) != 0 {
		t.Fatalf("got=%v", got)
	}
}

// TestFirstSlotNewsCard 新闻槽缺席与时效窗（对齐 :5432-5465）。
func TestFirstSlotNewsCard(t *testing.T) {
	now := 1800000000.0
	old := map[string]any{
		"card_id": "old", "card_revision_id": "rev-old", "selector_rank": 1,
		"evidence_bundle": map[string]any{"source_kind": "news_report", "published_at": isoTime(now - 100*3600)},
	}
	fresh := map[string]any{
		"card_id": "fresh", "card_revision_id": "rev-fresh", "selector_rank": 3,
		"evidence_bundle": map[string]any{"source_kind": "news_report", "published_at": isoTime(now - 1800)},
	}
	cards := []map[string]any{old, fresh}
	eligible := map[string]bool{"rev-old": true, "rev-fresh": true}
	got := FirstSlotNewsCard(cards, eligible, now)
	if pyStr(got["card_id"]) != "fresh" {
		t.Fatalf("超窗新闻必须被排除: %v", got)
	}
	if FirstSlotNewsCard(cards, map[string]bool{}, now) != nil {
		t.Fatal("无可集必须返回 nil")
	}
}

// TestIsColdStartManifest 判定（对齐 :4390-4396）。
func TestIsColdStartManifest(t *testing.T) {
	if !IsColdStartManifest(map[string]any{"cold_start": true}) {
		t.Fatal("true 必须判冷启动")
	}
	if IsColdStartManifest(map[string]any{"cold_start": false}) || IsColdStartManifest("x") {
		t.Fatal("false/非 map 不得判冷启动")
	}
}

// TestShortDeltaSuccessorRejectionReason 短增量后继守卫（对齐 :4635-4670）。
func TestShortDeltaSuccessorRejectionReason(t *testing.T) {
	store := newTestStore(t)
	manifest := map[string]any{
		"context_scope": "project",
		"short_interest_selection": map[string]any{"interest_id": "int-1"},
		"supersedes_manifest_id":   "m-0",
	}
	if got := ShortDeltaSuccessorRejectionReason(store, manifest, nil); got != "" {
		t.Fatalf("非全局 scope 必须豁免: %q", got)
	}
	// 全局 + 有 delta + 有前任但前任无法精确加载
	manifest["context_scope"] = GlobalNewChatScope
	if got := ShortDeltaSuccessorRejectionReason(store, manifest, nil); got != "manifest_short_delta_predecessor_exact_load_failed" {
		t.Fatalf("got=%q", got)
	}
	// 无 delta → 豁免
	manifest2 := map[string]any{"context_scope": GlobalNewChatScope}
	if got := ShortDeltaSuccessorRejectionReason(store, manifest2, nil); got != "" {
		t.Fatalf("got=%q", got)
	}
	_ = strings.TrimSpace
}
