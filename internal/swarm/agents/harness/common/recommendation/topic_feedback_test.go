package recommendation

import (
	"strings"
	"testing"
)

// feedbackTestStore 建独立临时库（复用 store 测试建库模式）
func feedbackTestStore(t *testing.T) *TopicStarterStore {
	t.Helper()
	store, err := NewTopicStarterStore(t.TempDir() + "/feedback.db")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

// seedFeedbackPoolCard 预置一张起始池卡（manifest+card+batch）
func seedFeedbackPoolCard(t *testing.T, store *TopicStarterStore, manifestID, revisionID, batchID string) {
	t.Helper()
	card := map[string]any{
		"card_id":             "c1",
		"card_revision_id":    revisionID,
		"generation_batch_id": batchID,
		"selector_rank":       1,
		"source_starter_id":   nil,
		"interest_summary_id": "i1",
		"interest_rank":       1,
		"observed_at":         100.0,
		"lane":                "personalized-intent",
		"origin":              "generated",
		"intent":              "intent-x",
		"title":               "title-x",
		"prompt":              "title-x",
		"freshness_class":     "evergreen_corpus",
		"effective_time":      100.0,
		"expire_time":         200.0,
		"evidence_bundle": map[string]any{
			"fetch_status": "success",
			"source_url":   "https://example.com/a",
			"source_kind":  "stable",
			"content_boundary": map[string]any{
				"text": "hello world", "start": 0, "end": 11,
			},
		},
		"source_memory_unit_ids": []any{"i1"},
		"source_locators":        []any{[]any{"s1", 1}},
		"identity_anchor":        "anchor",
		"context_anchors":        []any{},
		"model_provenance": map[string]any{
			"model":                     "model-x",
			"base_url":                  "https://ark.example",
			"effective_temperature":     0.7,
			"effective_top_p":           0.9,
			"context_window_tokens":     4096,
			"context_window_source":     "ARK_CONTEXT_WINDOW_TOKENS",
			"decoding_parameter_source": "request_parameters",
			"requested_temperature":     0.7,
			"requested_top_p":           0.9,
		},
		"prompt_versions": map[string]any{
			"i1": "v-i1", "i2": "v-i2", "i3": "v-i3", "i4": "v-i4", "i5": "v-i5",
		},
		"policy_id":                POLICY_ID,
		"payload_version":          PAYLOAD_VERSION,
		"retrieval_config_version": "rv1",
		"corpus_version":           "cv1",
	}
	promptVersions := map[string]any{"i1": "v-i1", "i2": "v-i2", "i3": "v-i3", "i4": "v-i4", "i5": "v-i5"}
	triggerKey := BuildGenerationTriggerKey("2026-11-13", 0, 0, "model-x", "https://ark.example",
		0.7, 0.9, 4096, promptVersions, "cv1", "rv1")
	manifest := map[string]any{
		"manifest_id":                 manifestID,
		"generation_batch_id":         batchID,
		"card_revision_ids":           []any{revisionID},
		"trigger_origin":              "lazy_seed",
		"local_date":                  "2026-11-13",
		"generation_trigger_key":      triggerKey,
		"generation_input_snapshot":   map[string]any{},
		"serving_compatibility_key":   BuildServingCompatibilityKey(triggerKey),
		"negative_preference_version": 0,
		"policy_id":                   POLICY_ID,
		"payload_version":             PAYLOAD_VERSION,
	}
	if written := WriteStarterPool(store, []map[string]any{card}, manifest, 100.0, map[string]string{}, ""); !written {
		t.Fatal("pool write failed")
	}
}

func TestRecordV5Interaction_非法事件类型拒绝(t *testing.T) {
	store := feedbackTestStore(t)
	if RecordV5Interaction(store, RecordV5InteractionOptions{EventType: "other", CreatedAt: 1}) {
		t.Fatal("非法类型应拒绝")
	}
}

func TestRecordV5Interaction_卡不存在拒绝(t *testing.T) {
	store := feedbackTestStore(t)
	if RecordV5Interaction(store, RecordV5InteractionOptions{EventType: "topic_click", CreatedAt: 1}) {
		t.Fatal("无卡应拒绝")
	}
}

func TestRecordV5Interaction_精确卡关联点击(t *testing.T) {
	store := feedbackTestStore(t)
	seedFeedbackPoolCard(t, store, "m1", "r1", "b1")
	ok := RecordV5Interaction(store, RecordV5InteractionOptions{EventType: "topic_click", SessionID: "s", SlateID: "sl1", ManifestID: "m1", CardRevisionID: "r1", GenerationBatchID: "b1", CreatedAt: 120.0, RequestID: "req1"})
	if !ok {
		t.Fatal("首次点击应成功")
	}
	// 幂等：同键重复 → false
	if RecordV5Interaction(store, RecordV5InteractionOptions{EventType: "topic_click", SessionID: "s", SlateID: "sl1", ManifestID: "m1", CardRevisionID: "r1", GenerationBatchID: "b1", CreatedAt: 121.0}) {
		t.Fatal("同键重复点击应拒绝")
	}
	events, err := store.QueryEvents(QueryEventsOptions{EventType: "topic_click", Limit: 8})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("events=%d", len(events))
	}
	payload := events[0]["payload"].(map[string]any)
	if payload["interest_summary_id"] != "i1" || payload["request_id"] != "req1" {
		t.Fatalf("payload=%v", payload)
	}
	if payload["intent"] != "intent-x" || payload["title"] != "title-x" {
		t.Fatalf("payload=%v", payload)
	}
}

func TestRecordV5Interaction_否定反馈写负偏好(t *testing.T) {
	store := feedbackTestStore(t)
	seedFeedbackPoolCard(t, store, "m1", "r1", "b1")
	ok := RecordV5Interaction(store, RecordV5InteractionOptions{EventType: "not_interested", SessionID: "s", SlateID: "sl1", ManifestID: "m1", CardRevisionID: "r1", GenerationBatchID: "b1", CreatedAt: 130.0})
	if !ok {
		t.Fatal("首次否定应成功")
	}
	// 负偏好随状态快照保存
	state, found := HistoryLoadLatestV4(store)
	if !found {
		t.Fatal("应有 v4 快照")
	}
	sidecar := state["memory_sidecar"].(map[string]any)
	preferences := sidecar["negative_preferences"].([]any)
	if len(preferences) != 1 {
		t.Fatalf("preferences=%v", preferences)
	}
	pref := preferences[0].(map[string]any)
	if pref["direction"] != "intent-x" || pref["source_id"] != "i1" {
		t.Fatalf("pref=%v", pref)
	}
	if sidecar["negative_preference_version"] != float64(1) {
		t.Fatalf("version=%v", sidecar["negative_preference_version"])
	}
	// 同卡再次否定：偏好去重但版本递增、事件幂等
	if RecordV5Interaction(store, RecordV5InteractionOptions{EventType: "not_interested", SessionID: "s", SlateID: "sl1", ManifestID: "m1", CardRevisionID: "r1", GenerationBatchID: "b1", CreatedAt: 131.0}) {
		t.Fatal("同键重复否定应拒绝")
	}
}

func TestRecordV5Interaction_空溯源否定偏好(t *testing.T) {
	store := feedbackTestStore(t)
	// 无卡片 → false（精确关联失败）
	if RecordV5Interaction(store, RecordV5InteractionOptions{EventType: "not_interested", SessionID: "s", SlateID: "sl2", ManifestID: "nope", CardRevisionID: "r", GenerationBatchID: "b", CreatedAt: 130.0}) {
		t.Fatal("无卡应拒绝")
	}
}

func TestRecordV5Exposure_曝光事件(t *testing.T) {
	store := feedbackTestStore(t)
	seedFeedbackPoolCard(t, store, "m1", "r1", "b1")
	ok := RecordV5Exposure(store, RecordV5ExposureOptions{SessionID: "s", SlateID: "sl1",
		Served: map[string]any{
			"manifest_id":         "m1",
			"generation_batch_id": "b1",
			"cards": []any{
				map[string]any{"card_revision_id": "r1"},
			},
		}, CreatedAt: 140.0})
	if !ok {
		t.Fatal("曝光应成功")
	}
	events, err := store.QueryEvents(QueryEventsOptions{EventType: "dynamic_exposure", Limit: 8})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("events=%d", len(events))
	}
	payload := events[0]["payload"].(map[string]any)
	topics := payload["topics"].([]any)
	if len(topics) != 1 {
		t.Fatalf("topics=%v", topics)
	}
}

func TestRecordV5Exposure_负向守卫(t *testing.T) {
	store := feedbackTestStore(t)
	// 缺 manifest/batch/空 cards/重复 revision
	if RecordV5Exposure(store, RecordV5ExposureOptions{SessionID: "s", SlateID: "x", Served: map[string]any{}, CreatedAt: 1}) {
		t.Fatal("缺 manifest 应拒绝")
	}
	if RecordV5Exposure(store, RecordV5ExposureOptions{SessionID: "s", SlateID: "x", Served: map[string]any{"manifest_id": "m"}, CreatedAt: 1}) {
		t.Fatal("缺 batch 应拒绝")
	}
	if RecordV5Exposure(store, RecordV5ExposureOptions{SessionID: "s", SlateID: "x", Served: map[string]any{"manifest_id": "m", "generation_batch_id": "b"}, CreatedAt: 1}) {
		t.Fatal("空 cards 应拒绝")
	}
	if RecordV5Exposure(store, RecordV5ExposureOptions{SessionID: "s", SlateID: "x", Served: map[string]any{"manifest_id": "m", "generation_batch_id": "b", "cards": "x"}, CreatedAt: 1}) {
		t.Fatal("cards 非列表应拒绝")
	}
	// 未知 revision
	if RecordV5Exposure(store, RecordV5ExposureOptions{SessionID: "s", SlateID: "x", Served: map[string]any{"manifest_id": "m", "generation_batch_id": "b", "cards": []any{map[string]any{"card_revision_id": "ghost"}}}, CreatedAt: 1}) {
		t.Fatal("未知 revision 应拒绝")
	}
}

func TestRecordV5SlateEvent_清单存在才记录(t *testing.T) {
	store := feedbackTestStore(t)
	seedFeedbackPoolCard(t, store, "m1", "r1", "b1")
	// manifest 不存在 → false
	if RecordV5SlateEvent(store, RecordV5SlateEventOptions{EventType: "page_left", SessionID: "s", SlateID: "sl", ManifestID: "ghost", GenerationBatchID: "b1", CreatedAt: 1}) {
		t.Fatal("ghost manifest 应拒绝")
	}
	// 非法事件类型
	if RecordV5SlateEvent(store, RecordV5SlateEventOptions{EventType: "other", SessionID: "s", SlateID: "sl", ManifestID: "m1", GenerationBatchID: "b1", CreatedAt: 1}) {
		t.Fatal("非法类型应拒绝")
	}
	if !RecordV5SlateEvent(store, RecordV5SlateEventOptions{EventType: "page_left", SessionID: "s", SlateID: "sl", ManifestID: "m1", GenerationBatchID: "b1", CreatedAt: 1}) {
		t.Fatal("合法事件应成功")
	}
}

func TestBuildFeedbackContext_计数与示例(t *testing.T) {
	store := feedbackTestStore(t)
	seedFeedbackPoolCard(t, store, "m1", "r1", "b1")
	_ = RecordV5Exposure(store, RecordV5ExposureOptions{SessionID: "s", SlateID: "sl1",
		Served: map[string]any{"manifest_id": "m1", "generation_batch_id": "b1",
			"cards": []any{map[string]any{"card_revision_id": "r1"}}}, CreatedAt: 140.0})
	_ = RecordV5Interaction(store, RecordV5InteractionOptions{EventType: "topic_click",
		SessionID: "s", SlateID: "sl1", ManifestID: "m1", CardRevisionID: "r1",
		GenerationBatchID: "b1", CreatedAt: 141.0})
	ctx := BuildFeedbackContext(store, []any{"i1", "i2", "i1"})
	counts := ctx["feedback_counts"].(map[string]any)
	c1 := counts["i1"].(map[string]any)
	if intOf(c1["exposure_days"]) != 1 || intOf(c1["clicks"]) != 1 || intOf(c1["not_interested"]) != 0 {
		t.Fatalf("i1 counts=%v", c1)
	}
	if len(counts) != 2 {
		t.Fatalf("去重后应只有 2 个兴趣：%v", counts)
	}
	if len(ctx["positive_exemplars"].([]any)) != 1 {
		t.Fatal("应有 1 条正面示例")
	}
	// 本地日计数：第二日再曝光
	_ = RecordV5Exposure(store, RecordV5ExposureOptions{SessionID: "s", SlateID: "sl2",
		Served: map[string]any{"manifest_id": "m1", "generation_batch_id": "b1",
			"cards": []any{map[string]any{"card_revision_id": "r1"}}}, CreatedAt: 140.0 + 86400})
	ctx2 := BuildFeedbackContext(store, []any{"i1"})
	c1v2 := ctx2["feedback_counts"].(map[string]any)["i1"].(map[string]any)
	if intOf(c1v2["exposure_days"]) != 2 {
		t.Fatalf("两日曝光应计数 2：%v", c1v2)
	}
}

func TestHistorySaveV4State_见证剥离与事件同事务(t *testing.T) {
	store := feedbackTestStore(t)
	// 第一次保存：带 witness 的快照
	stateA := HistoryEmptyV4State()
	ledger := stateA["history_ledger"].(map[string]any)
	ledger["sessions"] = map[string]any{
		"s1": map[string]any{"entries": []any{map[string]any{"witness": map[string]any{"x": 1}}}},
	}
	if _, err := HistorySaveV4State(store, stateA, nil, 10.0); err != nil {
		t.Fatalf("save A: %v", err)
	}
	// 第二次保存：前版 witness 被剥离
	stateB := HistoryEmptyV4State()
	stateB["memory_sidecar"].(map[string]any)["negative_preference_version"] = 3.0
	event := map[string]any{
		"session_id": "s", "slate_id": "sl", "event_type": "state_success",
		"topic_id": nil, "policy_id": "", "created_at": 20.0,
	}
	if _, err := HistorySaveV4State(store, stateB, []map[string]any{event}, 20.0); err != nil {
		t.Fatalf("save B: %v", err)
	}
	// 直读 version 1 校验 witness 已剥离
	db, err := store.connect()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	var snapshotJSON string
	if err := db.QueryRow("SELECT snapshot_json FROM profile_snapshot WHERE version = 1").Scan(&snapshotJSON); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(snapshotJSON, "witness") {
		t.Fatal("前版 witness 应被剥离")
	}
	// 事件以 v4 默认 policy 落库
	events, err := store.QueryEvents(QueryEventsOptions{EventType: "state_success", Limit: 8})
	if err != nil || len(events) != 1 {
		t.Fatalf("events=%d err=%v", len(events), err)
	}
	if pyStr(events[0]["policy_id"]) != v4HistoryPolicyID {
		t.Fatalf("policy=%v", events[0]["policy_id"])
	}
}

func TestRecordV5Interaction_二次否定偏好去重但版本递增(t *testing.T) {
	store := feedbackTestStore(t)
	seedFeedbackPoolCard(t, store, "m1", "r1", "b1")
	seedFeedbackPoolCard(t, store, "m2", "r2", "b2")
	if !RecordV5Interaction(store, RecordV5InteractionOptions{EventType: "not_interested",
		SessionID: "s", SlateID: "sl1", ManifestID: "m1", CardRevisionID: "r1",
		GenerationBatchID: "b1", CreatedAt: 130.0}) {
		t.Fatal("首次否定应成功")
	}
	if !RecordV5Interaction(store, RecordV5InteractionOptions{EventType: "not_interested",
		SessionID: "s", SlateID: "sl2", ManifestID: "m2", CardRevisionID: "r2",
		GenerationBatchID: "b2", CreatedAt: 140.0}) {
		t.Fatal("二次否定应成功")
	}
	state, _ := HistoryLoadLatestV4(store)
	sidecar := state["memory_sidecar"].(map[string]any)
	// 两卡同 intent/溯源 → 偏好按 `preference not in preferences` 去重为 1 条
	if len(sidecar["negative_preferences"].([]any)) != 1 {
		t.Fatalf("同偏好应去重：%v", sidecar["negative_preferences"])
	}
	if intOf(sidecar["negative_preference_version"]) != 2 {
		t.Fatalf("版本应递增到 2：%v", sidecar["negative_preference_version"])
	}
}
