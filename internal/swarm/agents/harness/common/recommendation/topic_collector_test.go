package recommendation

// topic_collector_test.go 对齐 tests/unit_tests/test_topic_recommendation.py 中
// TopicStarterOutcomeCollector 相关用例（空历史幂等、训练排除、深度标注、
// 训练切片物化、matcher 绑定守卫）。

import (
	"context"
	"testing"
)

// TestTopicStarterOutcomeCollector_绑定守卫 对齐 :726-749。
func TestTopicStarterOutcomeCollectorBinding(t *testing.T) {
	store := newTestStore(t)
	// 负边界拒绝
	if _, err := NewTopicStarterOutcomeCollector(nil, store, nil, nil, nil,
		-1, 100, 10, 10, nil, "", ""); err == nil {
		t.Fatalf("negative bounds should fail")
	}
	// matcher 无 id/version 拒绝
	matcher := func(earlier, later *SessionSummaryArtifact) (bool, error) { return false, nil }
	if _, err := NewTopicStarterOutcomeCollector(nil, store, nil, nil, nil,
		0, 100, 10, 10, matcher, "", ""); err == nil {
		t.Fatalf("matcher without binding should fail")
	}
	// 绑定完整 → 通过
	if _, err := NewTopicStarterOutcomeCollector(nil, store, nil, nil, nil,
		0, 100, 10, 10, matcher, "m", "v1"); err != nil {
		t.Fatalf("complete binding: %v", err)
	}
	// 无 matcher 但带 id 拒绝
	if _, err := NewTopicStarterOutcomeCollector(nil, store, nil, nil, nil,
		0, 100, 10, 10, nil, "m", ""); err == nil {
		t.Fatalf("id without matcher should fail")
	}
}

// TestTopicStarterOutcomeCollector_空历史幂等 对齐空库收集零产出。
func TestTopicStarterOutcomeCollectorEmptyHistory(t *testing.T) {
	store := newTestStore(t)
	collector, err := NewTopicStarterOutcomeCollector(context.Background(), store,
		nil, nil, func() float64 { return 10000.0 }, 0, 100, 100, 100, nil, "", "")
	if err != nil {
		t.Fatalf("collector: %v", err)
	}
	report, err := collector.Collect(nil)
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if report.BoundSessionsScanned != 0 || report.DepthEventsRecorded != 0 {
		t.Fatalf("empty store should yield zeros: %+v", report)
	}
}

// TestTrainingExclusionRecord_契约 对齐 :134-194。
func TestTrainingExclusionRecord(t *testing.T) {
	record, err := NewTrainingExclusionRecord("exclusion-1", "rid", "artifact-1",
		"summary_pair_matcher_unavailable", "snap1", GlobalNewChatScope,
		"manifest-1", "exposure-1", 100.0)
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	if record.ArtifactID() != "exclusion-1" {
		t.Fatalf("artifact id")
	}
	// 非法 training_domain
	if _, err := NewTrainingExclusionRecord("e", "bogus", "a", "r", "s",
		GlobalNewChatScope, "m", "x", 100.0); err == nil {
		t.Fatalf("bogus domain should fail")
	}
	// 非法 scope
	if _, err := NewTrainingExclusionRecord("e", "rid", "a", "r", "s",
		"bogus", "m", "x", 100.0); err == nil {
		t.Fatalf("bogus scope should fail")
	}
	// 往返
	restored, err := TrainingExclusionRecordFromPayload(record.AsPayload())
	if err != nil || restored.ExclusionID != record.ExclusionID {
		t.Fatalf("round trip: %v", err)
	}
	// 多余键拒绝
	extra := record.AsPayload()
	extra["extra"] = 1
	if _, err := TrainingExclusionRecordFromPayload(extra); err == nil {
		t.Fatalf("extra key should fail")
	}
}

// TestTopicStarterOutcomeCollector_深度标注与训练切片 对齐 :1440-1470 与
// :1795-1800/:2100-2130 主链路（session_bound → 深度 → 训练切片）。
func TestTopicStarterOutcomeCollectorDepthAndSlice(t *testing.T) {
	store := newTestStore(t)
	// 用 grounding 闭环 fixture 构造可联接的 v6 面
	fixtureManifest, fixtureCards := groundingFixtureFor(t, GlobalNewChatScope, true)
	manifestID := "m-fix"
	_ = fixtureCards
	mapAnyOf(fixtureManifest)["manifest_id"] = manifestID
	mapAnyOf(fixtureManifest)["generation_batch_id"] = "batch1"
	mapAnyOf(fixtureManifest)["card_revision_ids"] = []any{"cand-1"}
	if _, err := store.RecordEvent(map[string]any{
		"session_id": "sess", "slate_id": manifestID, "event_type": "starter_slate_manifest",
		"topic_id": manifestID, "policy_id": ProductPolicyID,
		"payload": fixtureManifest, "created_at": 550.0,
	}); err != nil {
		t.Fatalf("manifest: %v", err)
	}
	card := fixtureCards[0]
	mapAnyOf(card)["generation_batch_id"] = "batch1"
	mapAnyOf(card)["card_revision_id"] = "cand-1"
	// Ark 卡小票存储形态不含 result_urls（topic_product.py:191-206 12 字段恰等；
	// result_urls 是 grounding 证据小票的运行时附加字段）
	mapAnyOf(card)["search_receipt"] = arkReceiptMap()
	mapAnyOf(card)["evidence_bundle"] = map[string]any{"document_revision_id": "doc1"}
	mapAnyOf(card)["provider_invocation_receipts"] = []any{
		map[string]any{"component_id": "c1", "request_id": "req-1", "provider_response_id": "resp-1"},
	}
	mapAnyOf(card)["component_invocation_receipts"] = []any{
		map[string]any{"component_id": "c1", "request_id": "req-1"},
	}
	mapAnyOf(card)["component_receipts"] = map[string]any{
		"c1": map[string]any{"request_id": "req-1"},
	}
	mapAnyOf(card)["source_starter_id"] = "starter-1"
	mapAnyOf(card)["corpus_version"] = CorpusVersion
	mapAnyOf(card)["generation_input_snapshot_id"] = "snap1"
	if _, err := store.RecordEvent(map[string]any{
		"session_id": "sess", "slate_id": manifestID, "event_type": "starter_card",
		"topic_id": "cand-1", "policy_id": ProductPolicyID,
		"payload": card, "created_at": 550.0,
	}); err != nil {
		t.Fatalf("card: %v", err)
	}
	exposureID := "e-fix"
	if _, err := store.RecordEvent(map[string]any{
		"session_id": "sess", "slate_id": exposureID, "event_type": "dynamic_exposure",
		"topic_id": manifestID, "policy_id": ProductPolicyID,
		"payload": map[string]any{
			"manifest_id": manifestID, "generation_batch_id": "batch1",
			"context_scope": GlobalNewChatScope, "project_id": nil,
			"card_revision_ids": []any{"cand-1"},
			"search_receipts_by_card_revision": map[string]any{
				"cand-1": mapAnyOf(card)["search_receipt"],
			},
			"provider_response_ids_by_card_revision": map[string]any{
				"cand-1": []any{"resp-1"},
			},
		},
		"created_at": 600.0,
	}); err != nil {
		t.Fatalf("exposure: %v", err)
	}
	// 点击 + 启动 + 深度
	if !RecordTerminalInteraction(store, "topic_click", manifestID, "batch1",
		exposureID, "cand-1", GlobalNewChatScope, nil, 700.0) {
		t.Fatalf("click")
	}
	if !RecordTerminalSessionStart(store, manifestID, "batch1", exposureID,
		"cand-1", GlobalNewChatScope, nil, "s-start", 800.0) {
		t.Fatalf("session start")
	}
	// 会话记录（深度计数的用户记录）
	history := []map[string]any{
		{"role": "user", "content": "围棋", "timestamp": 810.0, "request_id": "r1"},
	}
	// collector 配置：视界后收集
	now := 600.0 + DefaultLabelHorizonSeconds + 10.0
	collector, err := NewTopicStarterOutcomeCollector(context.Background(), store,
		func(string) []map[string]any { return history },
		func() []map[string]any {
			return []map[string]any{
				{"session_id": "s-start", "user_id": "local", "project_id": ""},
			}
		},
		func() float64 { return now }, 0, DefaultLabelHorizonSeconds,
		100, 100, nil, "", "")
	if err != nil {
		t.Fatalf("collector: %v", err)
	}
	report, err := collector.Collect(nil)
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if report.DepthEventsRecorded != 1 {
		t.Fatalf("depth=%d", report.DepthEventsRecorded)
	}
	if report.StarterOutcomeArtifactsRecorded == 0 {
		t.Fatalf("starter outcomes should be recorded: %+v", report)
	}
	if report.UtilityExampleArtifactsRecorded == 0 {
		t.Fatalf("utility examples should be recorded")
	}
	if report.TrainingSliceArtifactsRecorded == 0 {
		t.Fatalf("training slice should be recorded")
	}
	// 训练切片已落库
	rows, _ := store.QueryEvents(QueryEventsOptions{
		EventType: "training_slice_manifest", PolicyID: &productPolicyIDPtr, Limit: 8})
	if len(rows) != 1 {
		t.Fatalf("slices=%d", len(rows))
	}
}

// TestTopicStarterOutcomeCollector_点击否定偏好对 对齐 :1994-2040。
func TestTopicStarterOutcomeCollectorPreferencePair(t *testing.T) {
	store := newTestStore(t)
	fixtureManifest, fixtureCards := groundingFixtureFor(t, GlobalNewChatScope, true)
	manifestID := "m-pref"
	mapAnyOf(fixtureManifest)["manifest_id"] = manifestID
	mapAnyOf(fixtureManifest)["generation_batch_id"] = "batch1"
	mapAnyOf(fixtureManifest)["card_revision_ids"] = []any{"cand-1"}
	mapAnyOf(fixtureManifest)["generation_config_version"] = GenerationConfigVersionV6
	// 束搜索痕迹提供 ranker/diversity 版本
	mapAnyOf(fixtureManifest)["list_search"] = map[string]any{
		"bounded_beam_trace": map[string]any{
			"utility_scorer_version": "u1", "diversity_scorer_version": "d1"},
	}
	if _, err := store.RecordEvent(map[string]any{
		"session_id": "sess", "slate_id": manifestID, "event_type": "starter_slate_manifest",
		"topic_id": manifestID, "policy_id": ProductPolicyID,
		"payload": fixtureManifest, "created_at": 550.0,
	}); err != nil {
		t.Fatalf("manifest: %v", err)
	}
	card := fixtureCards[0]
	mapAnyOf(card)["generation_batch_id"] = "batch1"
	mapAnyOf(card)["card_revision_id"] = "cand-1"
	mapAnyOf(card)["not_interested"] = true
	// Ark 卡小票存储形态不含 result_urls（topic_product.py:191-206 12 字段恰等；
	// result_urls 是 grounding 证据小票的运行时附加字段）
	mapAnyOf(card)["search_receipt"] = arkReceiptMap()
	mapAnyOf(card)["evidence_bundle"] = map[string]any{"document_revision_id": "doc1"}
	mapAnyOf(card)["provider_invocation_receipts"] = []any{
		map[string]any{"component_id": "c1", "request_id": "req-1", "provider_response_id": "resp-1"},
	}
	mapAnyOf(card)["component_invocation_receipts"] = []any{
		map[string]any{"component_id": "c1", "request_id": "req-1"},
	}
	mapAnyOf(card)["component_receipts"] = map[string]any{
		"c1": map[string]any{"request_id": "req-1"},
	}
	mapAnyOf(card)["source_starter_id"] = "starter-1"
	mapAnyOf(card)["corpus_version"] = CorpusVersion
	mapAnyOf(card)["generation_input_snapshot_id"] = "snap1"
	if _, err := store.RecordEvent(map[string]any{
		"session_id": "sess", "slate_id": manifestID, "event_type": "starter_card",
		"topic_id": "cand-1", "policy_id": ProductPolicyID,
		"payload": card, "created_at": 550.0,
	}); err != nil {
		t.Fatalf("card: %v", err)
	}
	exposureID := "e-pref"
	if _, err := store.RecordEvent(map[string]any{
		"session_id": "sess", "slate_id": exposureID, "event_type": "dynamic_exposure",
		"topic_id": manifestID, "policy_id": ProductPolicyID,
		"payload": map[string]any{
			"manifest_id": manifestID, "generation_batch_id": "batch1",
			"context_scope": GlobalNewChatScope, "project_id": nil,
			"card_revision_ids": []any{"cand-1"},
			"search_receipts_by_card_revision": map[string]any{
				"cand-1": mapAnyOf(card)["search_receipt"],
			},
			"provider_response_ids_by_card_revision": map[string]any{
				"cand-1": []any{"resp-1"},
			},
		},
		"created_at": 600.0,
	}); err != nil {
		t.Fatalf("exposure: %v", err)
	}
	if !RecordTerminalInteraction(store, "not_interested", manifestID, "batch1",
		exposureID, "cand-1", GlobalNewChatScope, nil, 700.0) {
		t.Fatalf("dismissal")
	}
	now := 600.0 + DefaultLabelHorizonSeconds + 10.0
	collector, err := NewTopicStarterOutcomeCollector(context.Background(), store,
		nil, nil, func() float64 { return now }, 0, DefaultLabelHorizonSeconds,
		100, 100, nil, "", "")
	if err != nil {
		t.Fatalf("collector: %v", err)
	}
	report, err := collector.Collect(nil)
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if report.StarterOutcomeArtifactsRecorded == 0 {
		t.Fatalf("outcomes should record")
	}
	// 负样本无正样本 → 无偏好对（positive_ids 为空）
	if report.PreferencePairArtifactsRecorded != 0 {
		t.Fatalf("no positives so no preference pairs: %d", report.PreferencePairArtifactsRecorded)
	}
	if report.TrainingSliceArtifactsRecorded == 0 {
		t.Fatalf("slice should record")
	}
}

// TestTopicStarterOutcomeCollector_Matcher拒绝 对齐 :822-828 拒绝传播。
func TestTopicStarterOutcomeCollectorMatcherRefused(t *testing.T) {
	store := newTestStore(t)
	refusedErr := &ProviderFailureError{Msg: "invalid key", CodeStr: "", HTTPStatus: 401}
	collector, err := NewTopicStarterOutcomeCollector(context.Background(), store,
		nil, nil, func() float64 { return 100.0 }, 0, 100, 10, 10,
		func(earlier, later *SessionSummaryArtifact) (bool, error) {
			return false, refusedErr
		}, "m", "v1")
	if err != nil {
		t.Fatalf("collector: %v", err)
	}
	earlier := summaryForTests(t, "summary:s1:"+hex64("a"), "s1", 10.0)
	later := summaryForTests(t, "summary:s2:"+hex64("b"), "s2", 20.0)
	// 拒绝 → matcher_unavailable 而非上抛
	label, reason, matchErr := collector.matchSummaryPair(earlier, later)
	if matchErr != nil {
		t.Fatalf("match: %v", matchErr)
	}
	if label != nil || reason != "summary_pair_matcher_unavailable" {
		t.Fatalf("label=%v reason=%q", label, reason)
	}
	// 拒绝类失败后所有后续配对直接拒绝（refused 标记）
	if !collector.summaryPairMatcherRefused {
		t.Fatalf("refused flag should set")
	}
}
