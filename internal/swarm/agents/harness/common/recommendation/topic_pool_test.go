// Package recommendation topic_pool 起始池与后台生成协调单元测试。
//
// 对应 Python 代码：jiuwenswarm/agents/harness/common/recommendation/topic_pool.py
package recommendation

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// poolTestStore 每用例独立账本。
func poolTestStore(t *testing.T) *TopicStarterStore {
	t.Helper()
	return newTestStore(t)
}

// poolTestPromptVersions 标准 prompt 版本表。
func poolTestPromptVersions() map[string]any {
	return map[string]any{"i1": "p1", "i2": "p2", "i3": "p3", "i4": "p4", "i5": "p5"}
}

// poolTestTriggerKey 合法触发键。
func poolTestTriggerKey() map[string]any {
	return BuildGenerationTriggerKey("2026-11-13", 3, 2, "model-x", "https://ark.example.com",
		0.7, 0.9, 128000, poolTestPromptVersions(), "corpus-v1", "retrieval-v1")
}

// poolTestEvidenceBundle 合法证据束（rune 长度对齐 start/end）。
func poolTestEvidenceBundle() map[string]any {
	text := "这是一段证据正文。"
	return map[string]any{
		"fetch_status":        "success",
		"source_url":          "https://example.com/a",
		"source_kind":         "stable",
		"published_at":        "2026-11-12T00:00:00Z",
		"published_at_source": "page_metadata",
		"content_boundary": map[string]any{
			"text":  text,
			"start": 0.0,
			"end":   float64(len([]rune(text))),
		},
	}
}

// poolTestCard 合法卡片。
func poolTestCard(rank int, manifest map[string]any) map[string]any {
	triggerKey, _, _ := payloadMap(manifest["generation_trigger_key"])
	corpusVersion := str(triggerKey["corpus_version"])
	retrievalVersion := str(triggerKey["retrieval_config_version"])
	provenance := map[string]any{
		"model":                     triggerKey["ark_model"],
		"base_url":                  triggerKey["ark_base_url"],
		"effective_temperature":     triggerKey["effective_temperature"],
		"effective_top_p":           triggerKey["effective_top_p"],
		"context_window_tokens":     triggerKey["context_window_tokens"],
		"context_window_source":     "ARK_CONTEXT_WINDOW_TOKENS",
		"decoding_parameter_source": "request_parameters",
		"requested_temperature":     triggerKey["effective_temperature"],
		"requested_top_p":           triggerKey["effective_top_p"],
	}
	// 修订 ID 与清单 card_revision_ids[0] 对齐，保证成批一致
	revision := "rev-1"
	if revisions := anySliceToStrings(manifest["card_revision_ids"]); len(revisions) > 0 {
		revision = revisions[0]
	}
	return map[string]any{
		"card_id":                  "card-1",
		"card_revision_id":         revision,
		"generation_batch_id":      manifest["generation_batch_id"],
		"selector_rank":            float64(rank),
		"source_starter_id":        "",
		"interest_summary_id":      "i-1",
		"lane":                     "personalized",
		"origin":                   "generated",
		"intent":                   "阅读深度报道",
		"title":                    "标题甲",
		"prompt":                   "想读点深度报道吗？",
		"freshness_class":          "evergreen_corpus",
		"effective_time":           1794400000.0,
		"expire_time":              1795000000.0,
		"evidence_bundle":          poolTestEvidenceBundle(),
		"source_memory_unit_ids":   []any{"mu-1"},
		"source_locators":          []any{"loc-1"},
		"identity_anchor":          "anchor-1",
		"context_anchors":          []any{},
		"model_provenance":         provenance,
		"prompt_versions":          poolTestPromptVersions(),
		"policy_id":                POLICY_ID,
		"payload_version":          PAYLOAD_VERSION,
		"retrieval_config_version": retrievalVersion,
		"corpus_version":           corpusVersion,
	}
}

// poolTestManifest 合法清单（cards 按序给 card_revision_id）。
func poolTestManifest(revisionIDs ...string) map[string]any {
	triggerKey := poolTestTriggerKey()
	return map[string]any{
		"manifest_id":                 "man-1",
		"generation_batch_id":         "batch-1",
		"card_revision_ids":           toAnySlice(revisionIDs),
		"trigger_origin":              "lazy_seed",
		"local_date":                  triggerKey["local_date"],
		"generation_trigger_key":      triggerKey,
		"generation_input_snapshot":   map[string]any{},
		"serving_compatibility_key":   BuildServingCompatibilityKey(triggerKey),
		"negative_preference_version": triggerKey["negative_preference_version"],
		"policy_id":                   POLICY_ID,
		"payload_version":             PAYLOAD_VERSION,
	}
}

func toAnySlice(values []string) []any {
	out := make([]any, 0, len(values))
	for _, v := range values {
		out = append(out, v)
	}
	return out
}

func TestBuildGenerationTriggerKeyAndServingKey(t *testing.T) {
	key := poolTestTriggerKey()
	assert.Equal(t, POLICY_ID, key["policy_id"])
	assert.Equal(t, PAYLOAD_VERSION, key["payload_version"])
	serving := BuildServingCompatibilityKey(key)
	assert.NotContains(t, serving, "negative_preference_version")
	assert.Equal(t, "model-x", serving["ark_model"])
	// 其余键保持
	assert.Equal(t, key["ark_model"], serving["ark_model"])
	assert.Len(t, serving, len(key)-1)
}

func TestWriteStarterPoolValidation(t *testing.T) {
	store := poolTestStore(t)
	manifest := poolTestManifest("rev-1")
	card := poolTestCard(1, manifest)
	// 合法整批写入
	assert.True(t, WriteStarterPool(store, []map[string]any{card}, manifest, 1794500000, nil, ""))
	// 数量越界（5 张）
	_, err := store.QueryEvents(QueryEventsOptions{EventType: "starter_card"})
	require.NoError(t, err)
	emptyManifest := poolTestManifest()
	assert.False(t, WriteStarterPool(store, make([]map[string]any, 0), emptyManifest, 1, nil, ""))
	// 卡片缺必需字段
	badCard := poolTestCard(1, emptyManifest)
	delete(badCard, "prompt")
	assert.False(t, WriteStarterPool(store, []map[string]any{badCard}, emptyManifest, 1, nil, ""))
	// 卡片带 api_key 机密字段
	secretCard := poolTestCard(1, emptyManifest)
	secretCard["evidence_bundle"].(map[string]any)["api_key"] = "sk-x"
	assert.False(t, WriteStarterPool(store, []map[string]any{secretCard}, emptyManifest, 1, nil, ""))
	// policy_id 不匹配
	wrongPolicy := poolTestCard(1, emptyManifest)
	wrongPolicy["policy_id"] = "other"
	assert.False(t, WriteStarterPool(store, []map[string]any{wrongPolicy}, emptyManifest, 1, nil, ""))
	// prompt_versions 集合不等于 i1..i5
	badPrompts := poolTestCard(1, emptyManifest)
	badPrompts["prompt_versions"] = map[string]any{"i1": "p1"}
	assert.False(t, WriteStarterPool(store, []map[string]any{badPrompts}, emptyManifest, 1, nil, ""))
	// current_event 超 48h
	freshCard := poolTestCard(1, emptyManifest)
	freshCard["freshness_class"] = "current_event"
	freshCard["expire_time"] = 1794400000.0 + 49*3600.0
	assert.False(t, WriteStarterPool(store, []map[string]any{freshCard}, emptyManifest, 1, nil, ""))
	// evergreen 来源非 authoritative/stable
	greenCard := poolTestCard(1, emptyManifest)
	greenCard["evidence_bundle"].(map[string]any)["source_kind"] = "reliable_report"
	assert.False(t, WriteStarterPool(store, []map[string]any{greenCard}, emptyManifest, 1, nil, ""))
	// retrieved-fixed 需语料文本与标题一致（用全新 manifest_id/batch 避免与首个写入撞事件键）
	fixedManifest := poolTestManifest("rev-f1")
	fixedManifest["manifest_id"] = "man-f1"
	fixedManifest["generation_batch_id"] = "batch-f1"
	fixedCard := poolTestCard(1, fixedManifest)
	fixedCard["origin"] = "retrieved-fixed"
	fixedCard["source_starter_id"] = "s1"
	assert.False(t, WriteStarterPool(store, []map[string]any{fixedCard}, fixedManifest, 1, map[string]string{"s1": "不一致"}, ""))
	assert.True(t, WriteStarterPool(store, []map[string]any{fixedCard}, fixedManifest, 1, map[string]string{"s1": "标题甲"}, ""))
	// manifest: trigger_origin 非法
	badManifest := poolTestManifest("rev-1")
	badManifest["trigger_origin"] = "bogus"
	goodCard := poolTestCard(1, badManifest)
	assert.False(t, WriteStarterPool(store, []map[string]any{goodCard}, badManifest, 1, nil, ""))
	// manifest: serving key 不匹配
	badManifest2 := poolTestManifest("rev-1")
	badManifest2["serving_compatibility_key"] = map[string]any{}
	assert.False(t, WriteStarterPool(store, []map[string]any{poolTestCard(1, badManifest2)}, badManifest2, 1, nil, ""))
	// manifest: 卡片 provenance 与触发键不一致
	badManifest3 := poolTestManifest("rev-1")
	provCard := poolTestCard(1, badManifest3)
	provCard["model_provenance"].(map[string]any)["model"] = "other-model"
	assert.False(t, WriteStarterPool(store, []map[string]any{provCard}, badManifest3, 1, nil, ""))
	// manifest: rank 重复
	dupManifest := poolTestManifest("rev-1", "rev-2")
	cardA := poolTestCard(1, dupManifest)
	cardB := poolTestCard(1, dupManifest)
	cardB["card_revision_id"] = "rev-2"
	cardB["card_id"] = "card-2"
	assert.False(t, WriteStarterPool(store, []map[string]any{cardA, cardB}, dupManifest, 1, nil, ""))
	// manifest: card_revision_ids 与排序修订不一致
	misManifest := poolTestManifest("rev-9")
	misCard := poolTestCard(1, misManifest)
	assert.False(t, WriteStarterPool(store, []map[string]any{misCard}, misManifest, 1, nil, ""))
}

func TestClassifyStarterSlate(t *testing.T) {
	// 空清单
	assert.Equal(t, "static", ClassifyStarterSlate(nil)["selection_mode"])
	// 全泛化
	generic := []map[string]any{{"lane": "fresh-trend"}}
	assert.Equal(t, "generic_dynamic", ClassifyStarterSlate(generic)["selection_mode"])
	assert.Equal(t, 1, ClassifyStarterSlate(generic)["trend_card_count"])
	// 个性化占 3/4 → personalized_dynamic
	personalizedCard := map[string]any{"interest_summary_id": "i", "source_memory_unit_ids": []any{"m"},
		"source_locators": []any{"l"}, "identity_anchor": "a"}
	// 3/4 = 0.75 恰不小于 0.75 → personalized_dynamic（对齐 :382）
	threeQuarters := []map[string]any{personalizedCard, personalizedCard, personalizedCard, {}}
	assert.Equal(t, "personalized_dynamic", ClassifyStarterSlate(threeQuarters)["selection_mode"])
	// 2/4 = 0.5 < 0.75 → mixed_dynamic
	mixed := []map[string]any{personalizedCard, personalizedCard, {}, {}}
	assert.Equal(t, "mixed_dynamic", ClassifyStarterSlate(mixed)["selection_mode"])
	assert.Equal(t, 2, ClassifyStarterSlate(mixed)["personalized_card_count"])
	// 个性化占满
	assert.Equal(t, "personalized_dynamic", ClassifyStarterSlate([]map[string]any{personalizedCard})["selection_mode"])
}

func TestReadStarterPoolStaticAndExact(t *testing.T) {
	store := poolTestStore(t)
	// 无清单 → 静态空池
	read := ReadStarterPool(store, BuildServingCompatibilityKey(poolTestTriggerKey()), 2, nil, 1794500000)
	assert.Nil(t, read["manifest_id"])
	assert.Empty(t, read["cards"])
	assert.Equal(t, "static", read["selection_mode"])
	// 写入批次后按兼容键读取
	manifest := poolTestManifest("rev-1")
	card := poolTestCard(1, manifest)
	require.True(t, WriteStarterPool(store, []map[string]any{card}, manifest, 1794410000, nil, ""))
	read = ReadStarterPool(store, BuildServingCompatibilityKey(poolTestTriggerKey()), 2, nil, 1794500000)
	assert.Equal(t, "man-1", read["manifest_id"])
	assert.Equal(t, 2, read["manifest_negative_preference_version"])
	require.Len(t, read["cards"], 1)
	returnedCard := read["cards"].([]map[string]any)[0]
	assert.Equal(t, "rev-1", returnedCard["card_revision_id"])
	// 负偏好版本更高的清单不可读
	read = ReadStarterPool(store, BuildServingCompatibilityKey(poolTestTriggerKey()), 1, nil, 1794500000)
	assert.Nil(t, read["manifest_id"])
	// FindExactPoolCard
	found := FindExactPoolCard(store, "man-1", "rev-1", "batch-1")
	require.NotNil(t, found)
	assert.Equal(t, "rev-1", found["card_revision_id"])
	assert.Nil(t, FindExactPoolCard(store, "man-1", "rev-9", "batch-1"))
	assert.Nil(t, FindExactPoolCard(store, "man-9", "rev-1", "batch-1"))
	// 过期卡被滤除（卡片在 manifest 变更后构造，保证 batch 一致）
	expiredManifest := poolTestManifest("rev-2")
	expiredManifest["manifest_id"] = "man-2"
	expiredManifest["generation_batch_id"] = "batch-2"
	expiredCard := poolTestCard(1, expiredManifest)
	expiredCard["card_revision_id"] = "rev-2"
	// 窗口整体落在 now 之前（validPoolCard 仍要求 effective < expire）
	expiredCard["effective_time"] = 1794380000.0
	expiredCard["expire_time"] = 1794390000.0
	require.True(t, WriteStarterPool(store, []map[string]any{expiredCard}, expiredManifest, 1794420000, nil, ""))
	read = ReadStarterPool(store, BuildServingCompatibilityKey(poolTestTriggerKey()), 2, nil, 1794500000)
	assert.Empty(t, read["cards"])
}

func TestApplyNegativePreferences(t *testing.T) {
	candidate := func(intent string) map[string]any {
		return map[string]any{
			"candidate_id":           intent,
			"intent":                 intent,
			"persona":                "",
			"primary_query":          "",
			"fallback_query":         "",
			"identity_anchor":        "",
			"context_anchors":        []any{},
			"source_memory_unit_ids": []any{"mu-9"},
		}
	}
	// 方向命中（CJK 子串）
	preferences := []map[string]any{{"direction": "健身", "source_id": "", "aliases": []any{}, "anchors": []any{}}}
	survivors, rejected := ApplyNegativePreferences([]map[string]any{candidate("健身计划")}, preferences)
	assert.Empty(t, survivors)
	require.Len(t, rejected, 1)
	assert.Equal(t, "negative_preference_contract_failed", rejected[0]["reason"])
	// ASCII 词边界：other 不匹配 another
	preferences = []map[string]any{{"direction": "other", "source_id": "", "aliases": []any{}, "anchors": []any{}}}
	survivors, _ = ApplyNegativePreferences([]map[string]any{candidate("another")}, preferences)
	assert.Len(t, survivors, 1)
	// 来源 ID 命中
	preferences = []map[string]any{{"direction": "", "source_id": "mu-9", "aliases": []any{}, "anchors": []any{}}}
	survivors, _ = ApplyNegativePreferences([]map[string]any{candidate("无关")}, preferences)
	assert.Empty(t, survivors)
}

func TestFilterDistinctPersonalizedCandidates(t *testing.T) {
	personalized := func(id, bucket, anchor string) map[string]any {
		return map[string]any{"candidate_id": id, "source_memory_unit_ids": []any{bucket},
			"identity_anchor": anchor}
	}
	generic := func(id string) map[string]any {
		return map[string]any{"candidate_id": id}
	}
	candidates := []map[string]any{
		personalized("a", "mu-1", "anchor-1"),
		personalized("b", "mu-1", "anchor-2"), // 桶冲突
		personalized("c", "mu-2", "anchor-1"), // 锚冲突
		generic("d"),
		personalized("e", "mu-2", "anchor-2"),
	}
	kept := FilterDistinctPersonalizedCandidates(candidates, 4)
	ids := []string{}
	for _, k := range kept {
		ids = append(ids, str(k["candidate_id"]))
	}
	assert.Equal(t, []string{"a", "d", "e"}, ids)
}

func TestCoordinatorAlreadyWritten(t *testing.T) {
	store := poolTestStore(t)
	coord := NewBackgroundGenerationCoordinator(store, 5.0, nil, "", nil)
	manifest := poolTestManifest("rev-1")
	card := poolTestCard(1, manifest)
	require.True(t, WriteStarterPool(store, []map[string]any{card}, manifest, 1794410000, nil, ""))
	// 已写 → true
	assert.True(t, coord.alreadyWritten(poolTestTriggerKey()))
	// cold_start 预置池不算关闭
	manifest2 := poolTestManifest("rev-2")
	manifest2["manifest_id"] = "man-2"
	manifest2["generation_batch_id"] = "batch-2"
	manifest2["cold_start"] = true
	card2 := poolTestCard(1, manifest2)
	card2["card_revision_id"] = "rev-2"
	card2["card_id"] = "card-2"
	require.True(t, WriteStarterPool(store, []map[string]any{card2}, manifest2, 1794420000, nil, ""))
	// 最新清单是 cold_start 预置池 → 不关闭本轮（Python :646-649）
	assert.False(t, coord.alreadyWritten(poolTestTriggerKey()))
	// 未写过触发键 → false
	otherKey := poolTestTriggerKey()
	otherKey["corpus_version"] = "corpus-v9"
	assert.False(t, coord.alreadyWritten(otherKey))
}

func TestCoordinatorStartLifecycle(t *testing.T) {
	store := poolTestStore(t)
	coord := NewBackgroundGenerationCoordinator(store, 5.0, nil, "", nil)
	// 已写触发 → already_written（不执行 generate）
	manifest := poolTestManifest("rev-1")
	require.True(t, WriteStarterPool(store, []map[string]any{poolTestCard(1, manifest)}, manifest, 1794410000, nil, ""))
	called := false
	run := coord.Start(context.Background(), poolTestTriggerKey(),
		func(ctx context.Context) *BackgroundGenerationResult {
			called = true
			return &BackgroundGenerationResult{}
		}, nil, false, false, false)
	assert.Equal(t, "already_written", run.Wait())
	assert.False(t, called)
	// 未写触发：require_ark_config 且无配置 → background_model_unavailable
	otherKey := poolTestTriggerKey()
	otherKey["corpus_version"] = "corpus-v2"
	run = coord.Start(context.Background(), otherKey,
		func(ctx context.Context) *BackgroundGenerationResult { return &BackgroundGenerationResult{} },
		nil, true, false, false)
	assert.Equal(t, "background_model_unavailable", run.Wait())
	// 生成成功并触发键匹配 → written
	key2 := poolTestTriggerKey()
	key2["corpus_version"] = "corpus-v3"
	run = coord.Start(context.Background(), key2,
		func(ctx context.Context) *BackgroundGenerationResult {
			m := poolTestManifest("rev-3")
			m["generation_trigger_key"] = key2
			m["serving_compatibility_key"] = BuildServingCompatibilityKey(key2)
			m["manifest_id"] = "man-3"
			m["generation_batch_id"] = "batch-3"
			// 触发键/批次变更后构造卡片，保证 provenance 与 batch 一致
			c := poolTestCard(1, m)
			c["card_revision_id"] = "rev-3"
			return &BackgroundGenerationResult{Cards: []map[string]any{c}, Manifest: m}
		}, nil, false, false, false)
	assert.Equal(t, "written", run.Wait())
	assert.Zero(t, coord.InflightCount())
	// 触发键不匹配 → rejected
	key3 := poolTestTriggerKey()
	key3["corpus_version"] = "corpus-v4"
	run = coord.Start(context.Background(), key3,
		func(ctx context.Context) *BackgroundGenerationResult {
			return &BackgroundGenerationResult{Cards: []map[string]any{}, Manifest: map[string]any{"generation_trigger_key": "wrong"}}
		}, nil, false, false, false)
	assert.Equal(t, "background_generation_rejected", run.Wait())
	// 弃权集合 → abstained
	key4 := poolTestTriggerKey()
	key4["corpus_version"] = "corpus-v5"
	run = coord.Start(context.Background(), key4,
		func(ctx context.Context) *BackgroundGenerationResult {
			return &BackgroundGenerationResult{FailureReason: "quality_abstained", Manifest: map[string]any{"generation_trigger_key": key4}}
		}, nil, false, false, false)
	assert.Equal(t, "abstained", run.Wait())
	// 生成器 panic → background_generation_failed
	key5 := poolTestTriggerKey()
	key5["corpus_version"] = "corpus-v6"
	run = coord.Start(context.Background(), key5,
		func(ctx context.Context) *BackgroundGenerationResult {
			panic("boom")
		}, nil, false, false, false)
	assert.Equal(t, "background_generation_failed", run.Wait())
	// 超时 → background_generation_deadline
	coord2 := NewBackgroundGenerationCoordinator(store, 0.05, nil, "", nil)
	key6 := poolTestTriggerKey()
	key6["corpus_version"] = "corpus-v7"
	run = coord2.Start(context.Background(), key6,
		func(ctx context.Context) *BackgroundGenerationResult {
			time.Sleep(300 * time.Millisecond)
			return &BackgroundGenerationResult{}
		}, nil, false, false, false)
	assert.Equal(t, "background_generation_deadline", run.Wait())
	// 事件留痕：started/deadline/failed/rejected/abstained
	events, err := store.ListEvents()
	require.NoError(t, err)
	seen := map[string]bool{}
	for _, event := range events {
		seen[str(event["event_type"])] = true
	}
	for _, expected := range []string{"background_model_unavailable", "background_generation_started",
		"background_generation_rejected", "background_generation_abstained", "background_generation_failed"} {
		assert.True(t, seen[expected], "缺少事件 %s", expected)
	}
}

func TestCoordinatorInflightDedupe(t *testing.T) {
	store := poolTestStore(t)
	coord := NewBackgroundGenerationCoordinator(store, 5.0, nil, "", nil)
	release := make(chan struct{})
	key := poolTestTriggerKey()
	key["corpus_version"] = "corpus-dedupe"
	started := make(chan struct{})
	generate := func(ctx context.Context) *BackgroundGenerationResult {
		close(started)
		<-release
		return &BackgroundGenerationResult{Manifest: map[string]any{}}
	}
	run1 := coord.Start(context.Background(), key, generate, nil, false, false, false)
	<-started
	run2 := coord.Start(context.Background(), key, generate, nil, false, false, false)
	// 同 token 在飞 → 返回同一 run
	assert.Equal(t, 1, coord.InflightCount())
	close(release)
	assert.Equal(t, run1, run2)
	assert.Equal(t, "background_generation_rejected", run1.Wait())
	assert.Zero(t, coord.InflightCount())
	_ = strings.TrimSpace
}

func TestFindExactPoolCardQuery(t *testing.T) {
	store := poolTestStore(t)
	manifest := poolTestManifest("rev-1")
	require.True(t, WriteStarterPool(store, []map[string]any{poolTestCard(1, manifest)}, manifest, 1794410000, nil, ""))
	// 清单成员关系连接精确卡修订
	card := FindExactPoolCard(store, "man-1", "rev-1", "batch-1")
	require.NotNil(t, card)
	assert.Equal(t, "card-1", card["card_id"])
}

func TestStaticPoolReadShape(t *testing.T) {
	read := staticPoolRead()
	assert.Nil(t, read["manifest_id"])
	assert.Nil(t, read["generation_batch_id"])
	assert.Nil(t, read["manifest_negative_preference_version"])
	assert.Empty(t, read["cards"])
	assert.Equal(t, 0, read["slate_card_count"])
}
