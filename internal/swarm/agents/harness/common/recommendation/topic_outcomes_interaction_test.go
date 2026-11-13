package recommendation

// topic_outcomes_interaction_test.go 对齐 tests/unit_tests/test_topic_recommendation.py
// 中交互记录半区用例（shown card 门 / 点击 / 否定 / 联接 / sink / 被动 outcome）。

import "testing"

// arkReceiptMap 合法 Ark Web Search 小票（topic_product.py:715-775）。
func arkReceiptMap() map[string]any {
	return map[string]any{
		"receipt_version": ArkWebSearchReceiptVersion, "provider_id": ArkWebSearchProviderID,
		"provider_response_id": "web-resp-1", "response_object": "response",
		"response_status": "completed", "actual_model": "m", "web_search_call_id": "w1",
		"action":       map[string]any{"type": "search", "query": "围棋"},
		"usage":        map[string]any{"tool_usage": map[string]any{"web_search": 1}},
		"result_count": 1, "requested_query": "围棋", "engine": ArkWebSearchProviderID,
	}
}

// poolFixtureForInteraction 直写 v6 产物面事件（清单/卡/曝光）。
// Python 中该面由 topic_pipeline（31b Task 6）以 topic_product POLICY_ID（v6）
// 产出；v5 池（topic_pool.write_starter_pool）是另一条 lane，此处模拟 v6 事件形态。
func poolFixtureForInteraction(t *testing.T, store *TopicStarterStore,
	manifestID, batchID, exposureID, revisionID string) map[string]any {
	t.Helper()
	config := &ArkBackgroundConfig{Model: "m", BaseURL: "https://ark",
		ContextWindowTokens: 128, Temperature: 0.3, TopP: 0.7}
	prov := config.SafeProvenance()
	card := map[string]any{
		"card_id": "card-1", "card_revision_id": revisionID, "generation_batch_id": batchID,
		"selector_rank": 1, "source_starter_id": "starter-1", "interest_summary_id": "i1",
		"interest_rank": 1, "observed_at": 0.0, "lane": "personalized", "origin": "pool",
		"intent": "围棋", "title": "T", "prompt": "T", "freshness_class": "current_event",
		"context_scope": GlobalNewChatScope, "project_id": nil,
		"effective_time": 0.0, "expire_time": 100.0,
		"evidence_bundle": map[string]any{
			"fetch_status": "success", "source_url": "https://example.com/a",
			"source_kind": "authoritative", "published_at": "2026-01-01T00:00:00+00:00",
			"published_at_source": "page",
			"content_boundary":    map[string]any{"text": "hello", "start": 0, "end": 5},
		},
		"source_memory_unit_ids": []any{"i1"}, "source_locators": []any{[]any{"s", 0}},
		"identity_anchor": "", "context_anchors": []any{},
		"model_provenance": prov,
		"prompt_versions":  map[string]any{"i1": "p1", "i2": "p2", "i3": "p3", "i4": "p4", "i5": "p5"},
		"policy_id":        ProductPolicyID, "payload_version": ProductPayloadVersion,
		"retrieval_config_version": RetrievalConfigVersion, "corpus_version": CorpusVersion,
		"profile_semantic_version": 1,
		"provider_invocation_receipts": []any{
			map[string]any{"component_id": "c1", "request_id": "req-1", "provider_response_id": "resp-1"},
		},
		"component_invocation_receipts": []any{
			map[string]any{"component_id": "c1", "request_id": "req-1"},
		},
		"component_receipts": map[string]any{
			"c1": map[string]any{"request_id": "req-1"},
		},
		"generation_input_snapshot_id": "snap1",
		"search_receipt":               arkReceiptMap(),
	}
	manifest := map[string]any{
		"manifest_id": manifestID, "generation_batch_id": batchID,
		"card_revision_ids": []any{revisionID}, "trigger_origin": "lazy_seed",
		"local_date": "2026-01-01", "context_scope": GlobalNewChatScope, "project_id": nil,
		"generation_trigger_key": map[string]any{
			"policy_id": ProductPolicyID, "payload_version": ProductPayloadVersion,
		},
		"generation_input_snapshot":   map[string]any{},
		"serving_compatibility_key":   map[string]any{},
		"negative_preference_version": 0,
		"policy_id":                   ProductPolicyID,
		"payload_version":             ProductPayloadVersion,
		"profile_semantic_version":    1,
	}
	if _, err := store.RecordEvent(map[string]any{
		"session_id": "sess", "slate_id": manifestID, "event_type": "starter_slate_manifest",
		"topic_id": manifestID, "policy_id": ProductPolicyID,
		"payload": manifest, "created_at": 550.0,
	}); err != nil {
		t.Fatalf("manifest event: %v", err)
	}
	if _, err := store.RecordEvent(map[string]any{
		"session_id": "sess", "slate_id": manifestID, "event_type": "starter_card",
		"topic_id": revisionID, "policy_id": ProductPolicyID,
		"payload": card, "created_at": 550.0,
	}); err != nil {
		t.Fatalf("card event: %v", err)
	}
	if _, err := store.RecordEvent(map[string]any{
		"session_id": "sess", "slate_id": exposureID, "event_type": "dynamic_exposure",
		"topic_id": manifestID, "policy_id": ProductPolicyID,
		"payload": map[string]any{
			"manifest_id": manifestID, "generation_batch_id": batchID,
			"context_scope": GlobalNewChatScope, "project_id": nil,
			"card_revision_ids":                []any{revisionID},
			"search_receipts_by_card_revision": map[string]any{revisionID: arkReceiptMap()},
			"provider_response_ids_by_card_revision": map[string]any{
				revisionID: []any{"resp-1"},
			},
		},
		"created_at": 600.0,
	}); err != nil {
		t.Fatalf("exposure event: %v", err)
	}
	return card
}

// TestRecordTerminalInteraction_点击与否定 对齐 :1308-1422。
func TestRecordTerminalInteraction(t *testing.T) {
	store := newTestStore(t)
	manifestID, batchID, exposureID, revisionID := "m1", "b1", "e1", "r1"
	poolFixtureForInteraction(t, store, manifestID, batchID, exposureID, revisionID)

	// 精确可见修订的点击
	if !RecordTerminalInteraction(store, "topic_click", manifestID, batchID,
		exposureID, revisionID, GlobalNewChatScope, nil, 700.0) {
		t.Fatalf("click should record")
	}
	// 不存在修订拒绝
	if RecordTerminalInteraction(store, "topic_click", manifestID, batchID,
		exposureID, "other", GlobalNewChatScope, nil, 700.0) {
		t.Fatalf("unknown revision should fail")
	}
	// 早于曝光拒绝
	if RecordTerminalInteraction(store, "topic_click", manifestID, batchID,
		exposureID, revisionID, GlobalNewChatScope, nil, 100.0) {
		t.Fatalf("before exposure should fail")
	}
	// 非法事件类型拒绝
	if RecordTerminalInteraction(store, "bogus", manifestID, batchID,
		exposureID, revisionID, GlobalNewChatScope, nil, 700.0) {
		t.Fatalf("bogus event should fail")
	}
	// 否定：写 dismissal 记录进 sidecar
	if !RecordTerminalInteraction(store, "not_interested", manifestID, batchID,
		exposureID, revisionID, GlobalNewChatScope, nil, 800.0) {
		t.Fatalf("dismissal should record")
	}
	state, ok := HistoryLoadLatestV4(store)
	if !ok {
		t.Fatalf("state after dismissal")
	}
	sidecar := mapAnyOf(state["memory_sidecar"])
	preferences := anyListOf(sidecar["negative_preferences"])
	if len(preferences) != 1 {
		t.Fatalf("preferences=%d", len(preferences))
	}
	record := mapAnyOf(preferences[0])
	if str(record["dismissal_kind"]) != "card" || str(record["reason"]) != "explicit_not_interested" {
		t.Fatalf("record=%v", record)
	}
	if intOf(sidecar["negative_preference_version"]) != 1 {
		t.Fatalf("version=%v", sidecar["negative_preference_version"])
	}
	// 已成立的否定不重复记录
	if RecordTerminalInteraction(store, "not_interested", manifestID, batchID,
		exposureID, revisionID, GlobalNewChatScope, nil, 900.0) {
		t.Fatalf("standing dismissal should not double-record")
	}
}

// TestRecordTerminalSessionStart 对齐 :1457-1528。
func TestRecordTerminalSessionStart(t *testing.T) {
	store := newTestStore(t)
	manifestID, batchID, exposureID, revisionID := "m2", "b2", "e2", "r2"
	poolFixtureForInteraction(t, store, manifestID, batchID, exposureID, revisionID)
	// 无点击 → 拒绝
	if RecordTerminalSessionStart(store, manifestID, batchID, exposureID,
		revisionID, GlobalNewChatScope, nil, "s1", 700.0) {
		t.Fatalf("without click should fail")
	}
	if !RecordTerminalInteraction(store, "topic_click", manifestID, batchID,
		exposureID, revisionID, GlobalNewChatScope, nil, 700.0) {
		t.Fatalf("click setup")
	}
	if !RecordTerminalSessionStart(store, manifestID, batchID, exposureID,
		revisionID, GlobalNewChatScope, nil, "s1", 800.0) {
		t.Fatalf("session start should record")
	}
	// 重复绑定拒绝
	if RecordTerminalSessionStart(store, manifestID, batchID, exposureID,
		revisionID, GlobalNewChatScope, nil, "s2", 900.0) {
		t.Fatalf("existing start should fail")
	}
}

// TestJoinStarterOutcomes_标签判定 对齐 :1861-1876 标签优先级。
func TestJoinStarterOutcomes(t *testing.T) {
	store := newTestStore(t)
	manifestID, batchID, exposureID, revisionID := "m3", "b3", "e3", "r3"
	poolFixtureForInteraction(t, store, manifestID, batchID, exposureID, revisionID)

	// shown_only
	outcomes, err := JoinStarterOutcomes(store, manifestID, exposureID, 1000.0, 4)
	if err != nil {
		t.Fatalf("join: %v", err)
	}
	if len(outcomes) != 1 || outcomes[0].OutcomeLabel != "shown_only" {
		t.Fatalf("label=%v", outcomes)
	}
	// 点击 → clicked_no_start
	if !RecordTerminalInteraction(store, "topic_click", manifestID, batchID,
		exposureID, revisionID, GlobalNewChatScope, nil, 700.0) {
		t.Fatalf("click")
	}
	outcomes, _ = JoinStarterOutcomes(store, manifestID, exposureID, 1000.0, 4)
	if outcomes[0].OutcomeLabel != "clicked_no_start" || !outcomes[0].Clicked {
		t.Fatalf("label=%q", outcomes[0].OutcomeLabel)
	}
	// 启动 + 深度 4 → started_deep
	if !RecordTerminalSessionStart(store, manifestID, batchID, exposureID,
		revisionID, GlobalNewChatScope, nil, "s1", 800.0) {
		t.Fatalf("start")
	}
	if !RecordTerminalConversationDepth(store, manifestID, exposureID, revisionID, "s1", 4, 900.0) {
		t.Fatalf("depth")
	}
	outcomes, _ = JoinStarterOutcomes(store, manifestID, exposureID, 1000.0, 4)
	if outcomes[0].OutcomeLabel != "started_deep" || outcomes[0].ConversationDepth != 4 {
		t.Fatalf("label=%q depth=%d", outcomes[0].OutcomeLabel, outcomes[0].ConversationDepth)
	}
	// 快照 ID / 位置 / 血缘
	if outcomes[0].ExposurePosition != 1 || outcomes[0].UserStateSnapshotID != "snap1" {
		t.Fatalf("wiring=%+v", outcomes[0])
	}
	// 空参数
	if got, _ := JoinStarterOutcomes(store, "", "", 1000.0, 4); got != nil {
		t.Fatalf("empty ids should be nil")
	}
	if got, _ := JoinStarterOutcomes(store, manifestID, exposureID, 1000.0, 0); got != nil {
		t.Fatalf("depth < 1 should be nil")
	}
}

// TestTopicStarterStoreOutcomeSink_血缘校验 对齐 :2030-2145。
func TestTopicStarterStoreOutcomeSink(t *testing.T) {
	store := newTestStore(t)
	manifestID, batchID, exposureID, revisionID := "m4", "b4", "e4", "r4"
	poolFixtureForInteraction(t, store, manifestID, batchID, exposureID, revisionID)
	sink := NewTopicStarterStoreOutcomeSink(store)

	signal, err := NewNewPageOutcomeSignal("accepted", manifestID, batchID, exposureID,
		revisionID, GlobalNewChatScope, nil, [][2]string{{"c1", "req-1"}},
		[]string{"resp-1"}, arkReceiptMap(), 700.0, false)
	if err != nil {
		t.Fatalf("signal: %v", err)
	}
	if !sink.Apply(signal) {
		t.Fatalf("sink should accept matching provenance")
	}
	// 血缘不匹配拒绝
	badSignal, _ := NewNewPageOutcomeSignal("accepted", manifestID, batchID, exposureID,
		revisionID, GlobalNewChatScope, nil, [][2]string{{"c1", "wrong"}},
		[]string{"resp-1"}, nil, 710.0, false)
	if sink.Apply(badSignal) {
		t.Fatalf("mismatched request id should fail")
	}
	// 点击已由 sink 记录
	rows, _ := store.QueryEvents(QueryEventsOptions{EventType: "topic_click",
		PolicyID: &productPolicyIDPtr, Limit: 8})
	if len(rows) != 1 {
		t.Fatalf("clicks=%d", len(rows))
	}
}

// TestRecordTerminalSlateOutcome_被动结果 对齐 :2148-2230。
func TestRecordTerminalSlateOutcome(t *testing.T) {
	store := newTestStore(t)
	manifestID, batchID, exposureID, revisionID := "m5", "b5", "e5", "r5"
	poolFixtureForInteraction(t, store, manifestID, batchID, exposureID, revisionID)

	if !RecordTerminalSlateOutcome(store, "page_left", manifestID, batchID,
		exposureID, GlobalNewChatScope, nil, 700.0) {
		t.Fatalf("page_left should record")
	}
	// 早于曝光拒绝
	if RecordTerminalSlateOutcome(store, "post_exposure_direct", manifestID, batchID,
		exposureID, GlobalNewChatScope, nil, 100.0) {
		t.Fatalf("before exposure should fail")
	}
	// 非法事件类型
	if RecordTerminalSlateOutcome(store, "bogus", manifestID, batchID,
		exposureID, GlobalNewChatScope, nil, 700.0) {
		t.Fatalf("bogus type should fail")
	}
	// scope 与 project 不匹配
	if RecordTerminalSlateOutcome(store, "page_left", manifestID, batchID,
		exposureID, "project:p1", nil, 700.0) {
		t.Fatalf("scope mismatch should fail")
	}
}

// TestValidSearchReceipt_四种schema 对齐 topic_product.py:715-775。
func TestValidSearchReceipt(t *testing.T) {
	// native：字段恰等 + URL 唯一 + 计数一致
	native := map[string]any{
		"receipt_version": OpenjiuwenNativeSearchReceiptVersion,
		"provider_id":     OpenjiuwenNativeSearchProviderID,
		"host_request_id": "host-1", "requested_query": "围棋",
		"engine": "duckduckgo", "result_urls": []any{"https://a.com", "https://b.com"},
		"result_count": 2,
	}
	if !ValidSearchReceipt(native) {
		t.Fatalf("native receipt should pass")
	}
	bad := jsonRoundTrip(native)
	bad["result_count"] = 3
	if ValidSearchReceipt(bad) {
		t.Fatalf("count mismatch should fail")
	}
	// Ark：provider/engine/response 校验 + action/usage
	ark := map[string]any{
		"receipt_version": ArkWebSearchReceiptVersion, "provider_id": ArkWebSearchProviderID,
		"provider_response_id": "resp", "response_object": "response",
		"response_status": "completed", "actual_model": "m", "web_search_call_id": "w1",
		"action":       map[string]any{"type": "search", "query": "围棋"},
		"usage":        map[string]any{"tool_usage": map[string]any{"web_search": 1}},
		"result_count": 1, "requested_query": "围棋", "engine": ArkWebSearchProviderID,
	}
	if !ValidSearchReceipt(ark) {
		t.Fatalf("ark receipt should pass")
	}
	wrongAction := jsonRoundTrip(ark)
	mapAnyOf(wrongAction["action"])["type"] = "browse"
	if ValidSearchReceipt(wrongAction) {
		t.Fatalf("wrong action should fail")
	}
	// 未知版本拒绝
	if ValidSearchReceipt(map[string]any{"receipt_version": "bogus"}) {
		t.Fatalf("unknown version should fail")
	}
}

// TestCardIsNewsAndColdStart 对齐 :850-855/:4394-4395。
func TestCardIsNewsAndColdStart(t *testing.T) {
	newsCard := map[string]any{"evidence_bundle": map[string]any{"source_kind": NewsSourceKind}}
	if !CardIsNews(newsCard) {
		t.Fatalf("news card")
	}
	hotCard := map[string]any{"evidence_bundle": map[string]any{"source_kind": HotListSourceKind}}
	if !CardIsNews(hotCard) {
		t.Fatalf("hot list card")
	}
	plain := map[string]any{"evidence_bundle": map[string]any{"source_kind": "authoritative"}}
	if CardIsNews(plain) {
		t.Fatalf("not news")
	}
	if !IsColdStartCard(map[string]any{"cold_start": true}) {
		t.Fatalf("cold start")
	}
	if IsColdStartCard(map[string]any{"cold_start": false}) {
		t.Fatalf("not cold start")
	}
}

// TestCardUsesHostEvidence 对齐 :3667-3674。
func TestCardUsesHostEvidence(t *testing.T) {
	for _, category := range []string{"project_continuation", ProfileInterestCategory} {
		if !CardUsesHostEvidence(map[string]any{"category": category}) {
			t.Fatalf("category %q should use host evidence", category)
		}
	}
	corpus := map[string]any{
		"category": "adjacent_explanation",
		"evidence_bundle": map[string]any{
			"host_receipt": map[string]any{"lane": "host_fixed_corpus_entry"}},
	}
	if !CardUsesHostEvidence(corpus) {
		t.Fatalf("corpus receipt should use host evidence")
	}
	if CardUsesHostEvidence(map[string]any{"category": "other"}) {
		t.Fatalf("plain card should not")
	}
}

// TestMemorySessionsByUnit 对齐 :5534-5545。
func TestMemorySessionsByUnit(t *testing.T) {
	state := map[string]any{"memory": map[string]any{"units": []any{
		map[string]any{"companion": map[string]any{
			"unit_id": "u1", "canonical_source_locators": []any{[]any{"s1", 0}, []any{"s2", 1}},
		}},
		map[string]any{}, // 无 companion → 跳过
	}}}
	sessions := MemorySessionsByUnit(state)
	if len(sessions["u1"]) != 2 || len(sessions) != 1 {
		t.Fatalf("sessions=%v", sessions)
	}
}

// TestInterestEvidenceAndSessionIds 对齐 :5559-5586。
func TestInterestEvidenceAndSessionIds(t *testing.T) {
	manifest := map[string]any{"rid_result": map[string]any{
		"trigger_interests": []any{
			map[string]any{"interest_summary_id": "i1", "source_locators": []any{
				map[string]any{"session_id": "s1", "event_id": "e1"},
			}},
		},
		"rejected_interests": []any{
			map[string]any{"interest_summary_id": "i2", "source_locators": []any{
				map[string]any{"session_id": "s2", "event_id": "e2"},
			}},
		},
	}}
	evidence := InterestEvidenceIds(manifest)
	if len(evidence["i1"]) != 1 || evidence["i1"][0] != "s1:e1" {
		t.Fatalf("evidence=%v", evidence)
	}
	sessions := InterestSessionIds(manifest)
	if !sessions["i2"]["s2"] {
		t.Fatalf("sessions=%v", sessions)
	}
}

// TestMigrateDismissalRecords_标记一次性 对齐 :1152-1168。
func TestMigrateDismissalRecords(t *testing.T) {
	store := newTestStore(t)
	if err := MigrateDismissalRecords(store, 100.0); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	rows, _ := store.QueryEvents(QueryEventsOptions{
		EventType: DismissalMigrationEventType, PolicyID: &productPolicyIDPtr, Limit: 1})
	if len(rows) != 1 {
		t.Fatalf("marker event missing")
	}
	// 二次运行无事发生
	if err := MigrateDismissalRecords(store, 200.0); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
	rows2, _ := store.QueryEvents(QueryEventsOptions{
		EventType: DismissalMigrationEventType, PolicyID: &productPolicyIDPtr, Limit: 8})
	if len(rows2) != 1 {
		t.Fatalf("marker should be once, got %d", len(rows2))
	}
}

// TestHotEventKey 对齐 topic_news.py:280-283。
func TestHotEventKey(t *testing.T) {
	if HotEventKey("AI 芯片!") != "ai芯片" {
		t.Fatalf("key=%q", HotEventKey("AI 芯片!"))
	}
	if HotEventKey("ＡＩ芯片") != HotEventKey("AI芯片") {
		t.Fatalf("NFKC width should fold")
	}
}
