package recommendation

// topic_prepare_test.go 对齐 tests/unit_tests/test_topic_recommendation.py 中
// TopicPreparePipeline / TopicPrepareService / 状态维护小票 / 锚点冻结相关用例。

import (
	"context"
	"testing"
)

// TestStateMaintenanceIdentity_非法状态拒绝 对齐上游 state_maintenance_identity
// 的三类失败（state_unavailable / state_mapping_unavailable）。
func TestStateMaintenanceIdentity(t *testing.T) {
	// 空状态 → state_unavailable
	if _, err := stateMaintenanceIdentity(map[string]any{}); err == nil {
		t.Fatalf("empty state should fail")
	} else if err.(*StateMaintenanceReceiptError).Reason != "state_unavailable" {
		t.Fatalf("reason=%q", err.(*StateMaintenanceReceiptError).Reason)
	}
	// 无活跃行为 → state_unavailable
	ledger := map[string]any{"next_behavior_index": 1, "sessions": map[string]any{}}
	if _, err := stateMaintenanceIdentity(map[string]any{
		"version": 1, "history_ledger": ledger,
	}); err == nil || err.(*StateMaintenanceReceiptError).Reason != "state_unavailable" {
		t.Fatalf("no active behaviors should be state_unavailable, got %v", err)
	}
}

// TestBuildStateMaintenanceReceipt_空原因拒绝 对齐 refresh_reason 判空。
func TestBuildStateMaintenanceReceiptEmptyReason(t *testing.T) {
	state := v4StateWithBehavior(1, "s1", 0)
	if _, err := BuildStateMaintenanceReceipt(state, "  "); err == nil {
		t.Fatalf("blank reason should fail")
	} else if err.(*StateMaintenanceReceiptError).Reason != "state_receipt_invalid" {
		t.Fatalf("reason=%q", err.(*StateMaintenanceReceiptError).Reason)
	}
}

// TestPersistStateMaintenanceReceipt_幂等回读 对齐 :275-306 落库-回读-字段校验闭环。
func TestPersistStateMaintenanceReceiptRoundTrip(t *testing.T) {
	store := newTestStore(t)
	state := v4StateWithBehavior(1, "s1", 0)
	receipt, err := PersistStateMaintenanceReceipt(store, state, "behavior_mapping", 100.0)
	if err != nil {
		t.Fatalf("persist failed: %v", err)
	}
	// 幂等：同状态再取回读同一条
	again, err := PersistStateMaintenanceReceipt(store, state, "idempotent_reuse", 200.0)
	if err != nil {
		t.Fatalf("second persist failed: %v", err)
	}
	if got := str(again["receipt_id"]); got != str(receipt["receipt_id"]) {
		t.Fatalf("receipt_id mismatch: %q vs %q", got, str(receipt["receipt_id"]))
	}
	// 字段数恰好 13
	if len(receipt) != len(stateMaintenanceReceiptFields) {
		t.Fatalf("field count=%d want %d", len(receipt), len(stateMaintenanceReceiptFields))
	}
	if str(receipt["refresh_reason"]) != "behavior_mapping" {
		t.Fatalf("refresh_reason=%q", str(receipt["refresh_reason"]))
	}
}

// TestLoadGenerationInputs_快照冻结 对齐 load_generation_inputs :358-393。
func TestLoadGenerationInputsSnapshot(t *testing.T) {
	store := newTestStore(t)
	records := map[string][]map[string]any{}
	inputs := LoadGenerationInputs(store, 1000.0, func() map[string][]map[string]any { return records })
	if inputs == nil {
		t.Fatalf("inputs nil")
	}
	if inputs.State == nil {
		t.Fatalf("state should fall back to empty_history_state")
	}
	snapshot := inputs.GenerationInputSnapshot
	if v, ok := snapshot["active_delta_version"].(int); !ok || v != 0 {
		t.Fatalf("active_delta_version=%v", snapshot["active_delta_version"])
	}
	if v, ok := snapshot["external_memory_version"].(int); !ok || v != 0 {
		t.Fatalf("external_memory_version=%v", snapshot["external_memory_version"])
	}
	if _, present := snapshot["state_maintenance_receipt"]; present {
		t.Fatalf("no receipt persisted yet, key must be absent")
	}
}

// TestRecentExposureWindow_过期与去重 对齐 _recent_exposure_window :309-355。
func TestRecentExposureWindow(t *testing.T) {
	store := newTestStore(t)
	// 用真实 triggerKey 与服务构造合法池（validPoolManifest 交叉校验 provenance）
	config := &ArkBackgroundConfig{Model: "m", BaseURL: "https://ark", ContextWindowTokens: 128, Temperature: 0.3, TopP: 0.7}
	svc := NewTopicPrepareService(store,
		func(context.Context, map[string]any, string, *ArkBackgroundConfig) (*BackgroundGenerationResult, error) {
			return &BackgroundGenerationResult{}, nil
		}, nil,
		func() *ArkBackgroundConfig { return config },
		func() float64 { return 500.0 }, nil)
	triggerKey := svc.triggerKey()
	manifestID := "v5-manifest-x"
	batchID := "v5-batch-x"
	revisionID := "v5-revision-x"
	card := map[string]any{
		"card_id": "card-1", "card_revision_id": revisionID, "generation_batch_id": batchID,
		"selector_rank": 1, "source_starter_id": "starter-1", "interest_summary_id": "i1",
		"interest_rank": 1, "observed_at": 0.0, "lane": "personalized", "origin": "pool",
		"intent": "q", "title": "T", "prompt": "T", "freshness_class": "current_event",
		"effective_time": 0.0, "expire_time": 100.0, "evidence_bundle": map[string]any{
			"fetch_status": "success", "source_url": "https://example.com/a",
			"source_kind": "authoritative", "published_at": "2026-01-01T00:00:00+00:00",
			"published_at_source": "page",
			"content_boundary":    map[string]any{"text": "hello", "start": 0, "end": 5},
		},
		"source_memory_unit_ids": []any{"i1"}, "source_locators": []any{[]any{"s", 0}},
		"identity_anchor": "", "context_anchors": []any{},
		"model_provenance": config.SafeProvenance(),
		"prompt_versions":  mapAnyOf(triggerKey["prompt_versions"]),
		"policy_id":        POLICY_ID, "payload_version": PAYLOAD_VERSION,
		"retrieval_config_version": RetrievalConfigVersion, "corpus_version": CorpusVersion,
	}
	manifest := map[string]any{
		"manifest_id": manifestID, "generation_batch_id": batchID,
		"card_revision_ids": []any{revisionID}, "trigger_origin": "lazy_seed",
		"local_date":                  triggerKey["local_date"],
		"generation_trigger_key":      triggerKey,
		"generation_input_snapshot":   map[string]any{},
		"serving_compatibility_key":   BuildServingCompatibilityKey(triggerKey),
		"negative_preference_version": triggerKey["negative_preference_version"],
		"policy_id":                   POLICY_ID,
		"payload_version":             PAYLOAD_VERSION,
	}
	if !WriteStarterPool(store, []map[string]any{card}, manifest, 500.0, map[string]string{"starter-1": "hello"}, "") {
		t.Fatalf("pool write failed")
	}
	_, _ = store.RecordEvent(map[string]any{
		"session_id": "sess", "slate_id": "slate", "event_type": "dynamic_exposure",
		"topic_id": "topic", "policy_id": POLICY_ID,
		"payload": map[string]any{
			"manifest_id": manifestID, "generation_batch_id": batchID,
			"topics": []any{map[string]any{"card_revision_id": revisionID}},
		},
		"created_at": 900.0,
	})
	window := recentExposureWindow(store, 1000.0)
	if len(window) != 1 {
		t.Fatalf("window len=%d want 1", len(window))
	}
	if str(window[0]["card_revision_id"]) != revisionID {
		t.Fatalf("revision=%q", str(window[0]["card_revision_id"]))
	}
	// 越窗（created_at < cutoff）不进窗口
	_, _ = store.RecordEvent(map[string]any{
		"session_id": "sess", "slate_id": "slate2", "event_type": "dynamic_exposure",
		"topic_id": "topic2", "policy_id": POLICY_ID,
		"payload":    map[string]any{"manifest_id": manifestID, "generation_batch_id": batchID, "topics": []any{}},
		"created_at": 100.0,
	})
	if got := len(recentExposureWindow(store, 1000.0)); got != 1 {
		t.Fatalf("stale exposure should be skipped, len=%d", got)
	}
}

// TestRecentlyExposedFilter 对齐 _recently_exposed/_not_recently_exposed :512-536。
func TestRecentlyExposedFilter(t *testing.T) {
	window := []map[string]any{
		{"title": "A", "source_starter_id": "s1"},
	}
	candidates := []map[string]any{
		{"title": "A", "source_starter_id": "s9"},
		{"title": "B", "source_starter_id": "s1"},
		{"title": "B", "source_starter_id": ""},
		{"title": "C", "source_starter_id": nil},
	}
	got := notRecentlyExposed(candidates, window)
	if len(got) != 2 {
		t.Fatalf("len=%d want 2", len(got))
	}
}

// TestTTLSeconds_枚举完整 对齐 _ttl_seconds :539-544 三分支。
func TestTTLSeconds(t *testing.T) {
	if ttlSeconds("current_event") != CurrentEventTTLSeconds {
		t.Fatalf("current_event")
	}
	if ttlSeconds("recent_development") != RecentDevelopmentTTLSeconds {
		t.Fatalf("recent_development")
	}
	if ttlSeconds("evergreen") != EvergreenTTLSeconds || ttlSeconds("unknown") != EvergreenTTLSeconds {
		t.Fatalf("fallback should be evergreen")
	}
}

// TestHistoryBuildAllowedGroundedAnchors_v5模型切片 对齐 :1286-1327 v5 分支。
func TestHistoryBuildAllowedGroundedAnchorsV5ModelSpan(t *testing.T) {
	behavior := CanonicalBehavior{BehaviorIndex: 1, SessionID: "s1", Ordinal: 0, Content: "讨论围棋布局"}
	units := []map[string]any{{
		"companion": map[string]any{"unit_id": "u1"},
		"core":      map[string]any{"representative_behavior_indices": []any{1}},
	}}
	spans := []SpanProposal{{BehaviorId: 1, SourceLocator: SourceLocator{SessionID: "s1", Ordinal: 0}, Start: 2, End: 4}}
	buckets := HistoryBuildAllowedGroundedAnchors(units, []CanonicalBehavior{behavior},
		map[int]map[string]any{}, spans, POLICY_ID)
	bucket, ok := buckets["u1"]
	if !ok {
		t.Fatalf("bucket u1 missing: %v", buckets)
	}
	if len(bucket.Anchors) != 1 || bucket.Anchors[0] != "围棋" {
		t.Fatalf("anchors=%v", bucket.Anchors)
	}
	// CJK 单字不达标（v5MinCJKChars=2）
	spans2 := []SpanProposal{{BehaviorId: 1, SourceLocator: SourceLocator{SessionID: "s1", Ordinal: 0}, Start: 2, End: 3}}
	buckets2 := HistoryBuildAllowedGroundedAnchors(units, []CanonicalBehavior{behavior},
		map[int]map[string]any{}, spans2, POLICY_ID)
	if len(buckets2["u1"].Anchors) != 0 {
		t.Fatalf("single CJK char should be rejected: %v", buckets2["u1"].Anchors)
	}
	if buckets2["u1"].PolicyId != v5HistoryPolicyID || buckets2["u1"].ValidationStatus != v5ValidationStatus {
		t.Fatalf("policy/status not frozen")
	}
}

// TestHistoryBuildAllowedGroundedAnchors_v4窄路径 对齐 :1330-1417 v4 分支。
func TestHistoryBuildAllowedGroundedAnchorsV4NarrowPath(t *testing.T) {
	behavior := CanonicalBehavior{BehaviorIndex: 1, SessionID: "s1", Ordinal: 0,
		Content: "我要做一个多目标优化问题"}
	mapping := map[int]map[string]any{
		1: {"action_type": "explicit_goal_preference_task", "literal_focus_span": "多目标优化"},
	}
	units := []map[string]any{{
		"companion": map[string]any{"unit_id": "u1"},
		"core":      map[string]any{"representative_behavior_indices": []any{1}},
	}}
	buckets := HistoryBuildAllowedGroundedAnchors(units, []CanonicalBehavior{behavior}, mapping, nil, v4HistoryPolicyID)
	bucket := buckets["u1"]
	if !bucket.ExplicitSingleEvidence || bucket.IdentityAnchor != "多目标优化" {
		t.Fatalf("narrow path not applied: %+v", bucket)
	}
	// 非 explicit_goal_preference_task 不取窄路径
	mapping2 := map[int]map[string]any{1: {"action_type": "follow_up", "literal_focus_span": "多目标优化"}}
	buckets2 := HistoryBuildAllowedGroundedAnchors(units, []CanonicalBehavior{behavior}, mapping2, nil, v4HistoryPolicyID)
	if buckets2["u1"].ExplicitSingleEvidence {
		t.Fatalf("narrow path must require explicit_goal_preference_task")
	}
}

// TestChineseRepeatedAnchors_极大子串 对齐 :1017-1114：包含关系去重。
func TestChineseRepeatedAnchors(t *testing.T) {
	cleaned := []locatorContent{
		{locator: SourceLocator{SessionID: "s1", Ordinal: 0}, content: "讨论多目标优化方法"},
		{locator: SourceLocator{SessionID: "s2", Ordinal: 0}, content: "多目标优化很有意思"},
	}
	terms := chineseRepeatedAnchors(cleaned)
	found := false
	for _, item := range terms {
		if item.term == "多目标优化" {
			found = true
		}
		if item.term == "目标" || item.term == "优化" {
			t.Fatalf("non-maximal substring %q should be dropped", item.term)
		}
	}
	if !found {
		t.Fatalf("maximal substring missing: %v", terms)
	}
}

// TestParseSpanProposals_StrictSchema 对齐 _parse_span_proposals :703-758。
func TestParseSpanProposalsStrictSchema(t *testing.T) {
	if got := parseSpanProposals("not a dict"); len(got) != 0 {
		t.Fatalf("non-dict payload should be empty")
	}
	if got := parseSpanProposals(map[string]any{"extra": 1, "proposals": []any{}}); len(got) != 0 {
		t.Fatalf("extra keys should be rejected")
	}
	valid := map[string]any{
		"proposals": []any{
			map[string]any{"behavior_id": 1, "source_locator": []any{"s1", 0}, "start": 2, "end": 4},
			map[string]any{"behavior_id": 1, "source_locator": []any{"s1", 0}, "start": 2, "end": 4}, // 重复
			map[string]any{"behavior_id": 1, "source_locator": []any{"s1", 0}, "start": 2},           // 缺键
		},
	}
	got := parseSpanProposals(valid)
	if len(got) != 1 || got[0].Start != 2 || got[0].End != 4 {
		t.Fatalf("got=%+v", got)
	}
}

// TestValidateVerbatimSpans_边界与歧义 对齐 validate_verbatim_spans :799-868。
func TestValidateVerbatimSpans(t *testing.T) {
	behaviors := []CanonicalBehavior{{BehaviorIndex: 1, SessionID: "s1", Ordinal: 0, Content: "abc def"}}
	messages := []map[string]any{{
		"behavior_id":    1,
		"source_locator": []any{"s1", 0},
		"content":        "abc def",
	}}
	valid := HistoryValidateVerbatimSpans(messages, behaviors,
		[]SpanProposal{{BehaviorId: 1, SourceLocator: SourceLocator{SessionID: "s1", Ordinal: 0}, Start: 0, End: 3}})
	if len(valid) != 1 || valid[0].Text != "abc" {
		t.Fatalf("valid span rejected: %+v", valid)
	}
	// 越界
	out := HistoryValidateVerbatimSpans(messages, behaviors,
		[]SpanProposal{{BehaviorId: 1, SourceLocator: SourceLocator{SessionID: "s1", Ordinal: 0}, Start: 0, End: 99}})
	if len(out) != 0 {
		t.Fatalf("out of range should be dropped")
	}
	// 定位器不匹配
	mismatch := HistoryValidateVerbatimSpans(messages, behaviors,
		[]SpanProposal{{BehaviorId: 1, SourceLocator: SourceLocator{SessionID: "s9", Ordinal: 0}, Start: 0, End: 3}})
	if len(mismatch) != 0 {
		t.Fatalf("locator mismatch should be dropped")
	}
	// ASCII 词边界：切到 def 中间（start=4,end=5 → "d"，右邻 e 为词字符 → 丢弃）
	half := HistoryValidateVerbatimSpans(messages, behaviors,
		[]SpanProposal{{BehaviorId: 1, SourceLocator: SourceLocator{SessionID: "s1", Ordinal: 0}, Start: 4, End: 5}})
	if len(half) != 0 {
		t.Fatalf("word-interior slice should be dropped: %+v", half)
	}
}

// TestTopicPreparePipeline_ContextLimitUnverified 对齐 :617-618。
func TestTopicPreparePipelineContextLimitUnverified(t *testing.T) {
	store := newTestStore(t)
	pipeline := NewTopicPreparePipeline(store, nil, nil, nil, nil, nil, nil, nil, nil)
	reason := pipeline.RefreshState(context.Background(), &ArkBackgroundConfig{ContextWindowTokens: 0})
	if reason != "context_limit_unverified" {
		t.Fatalf("reason=%q", reason)
	}
}

// TestTopicPreparePipeline_空历史幂等复用 对齐 :656-685 空历史分支：
// 状态 ok 但无活跃行为 → no_eligible_history。
func TestTopicPreparePipelineEmptyHistory(t *testing.T) {
	store := newTestStore(t)
	pipeline := NewTopicPreparePipeline(store, nil,
		func() map[string][]map[string]any { return map[string][]map[string]any{} },
		nil, nil, nil, nil, func() float64 { return 100.0 }, nil)
	reason := pipeline.RefreshState(context.Background(), &ArkBackgroundConfig{ContextWindowTokens: 128, Model: "m", BaseURL: "https://ark"})
	if reason != "no_eligible_history" {
		t.Fatalf("reason=%q want no_eligible_history", reason)
	}
}

// TestTopicPreparePipelineGenerate_状态失败空结果 对齐 :690-715。
func TestTopicPreparePipelineGenerateStateFailure(t *testing.T) {
	store := newTestStore(t)
	pipeline := NewTopicPreparePipeline(store, nil,
		func() map[string][]map[string]any { return map[string][]map[string]any{} },
		nil, nil, nil, nil, func() float64 { return 100.0 }, nil)
	triggerKey := map[string]any{"local_date": "2026-01-01"}
	result, err := pipeline.Generate(context.Background(), triggerKey, "scheduled_cron",
		&ArkBackgroundConfig{ContextWindowTokens: 0})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Cards) != 0 || result.FailureReason != "context_limit_unverified" {
		t.Fatalf("cards=%d reason=%q", len(result.Cards), result.FailureReason)
	}
	manifest := result.Manifest
	if str(manifest["trigger_origin"]) != "scheduled_cron" {
		t.Fatalf("trigger_origin missing")
	}
	audit, ok := manifest["external_invocation_audit"].(map[string]any)
	if !ok {
		t.Fatalf("audit receipt missing")
	}
	if _, ok := audit["counters"].(map[string]any); !ok {
		t.Fatalf("counters missing")
	}
}

// TestPrepareService_Serve无配置懒种子 对齐 :1110-1124。
func TestPrepareServiceServeNoConfig(t *testing.T) {
	store := newTestStore(t)
	svc := NewTopicPrepareService(store,
		func(context.Context, map[string]any, string, *ArkBackgroundConfig) (*BackgroundGenerationResult, error) {
			t.Fatalf("generate should not be called without config")
			return nil, nil
		}, nil,
		func() *ArkBackgroundConfig { return nil }, func() float64 { return 100.0 }, nil)
	got := svc.Serve("sess-1")
	if truthy(got["available"]) || str(got["reason"]) != "background_model_unavailable" {
		t.Fatalf("got=%v", got)
	}
	if str(got["session_id"]) != "sess-1" || str(got["policy_id"]) != POLICY_ID {
		t.Fatalf("identity fields missing: %v", got)
	}
	// coordinator 的拒绝任务收尾后，在飞数归零（对齐 Python inflight 惰性回收）
	svc.WaitForBackground()
	if svc.InflightCount() != 0 {
		t.Fatalf("inflight should drain to 0")
	}
}

// TestPrepareService_TriggerKey 对齐 _trigger_key :1011-1036。
func TestPrepareServiceTriggerKey(t *testing.T) {
	store := newTestStore(t)
	svc := NewTopicPrepareService(store,
		func(context.Context, map[string]any, string, *ArkBackgroundConfig) (*BackgroundGenerationResult, error) {
			return &BackgroundGenerationResult{}, nil
		}, nil,
		func() *ArkBackgroundConfig {
			return &ArkBackgroundConfig{Model: "doubao", BaseURL: "https://ark", ContextWindowTokens: 128, Temperature: 0.3, TopP: 0.7}
		},
		func() float64 { return 1767000000.0 }, nil)
	key := svc.triggerKey()
	if str(key["ark_model"]) != "doubao" {
		t.Fatalf("ark_model=%v", key["ark_model"])
	}
	promptVersions := mapAnyOf(key["prompt_versions"])
	if str(promptVersions["i1"]) != I1PromptVersion || str(promptVersions["i3"]) != I3RetrievalPromptVersion {
		t.Fatalf("prompt_versions=%v", promptVersions)
	}
	if str(key["corpus_version"]) != CorpusVersion || str(key["retrieval_config_version"]) != RetrievalConfigVersion {
		t.Fatalf("versions missing")
	}
}

// v4StateWithBehavior 构造带一条活跃行为的 v4 状态（小票身份测试用）。
func v4StateWithBehavior(version int, sessionID string, ordinal int) map[string]any {
	state := HistoryEmptyV4State()
	state["version"] = version
	witness := map[string]any{
		"role": "user", "timestamp_or_0": 0.0, "content": "围棋", "supported": true,
	}
	state["history_ledger"] = map[string]any{
		"next_behavior_index": 2, "history_revision": 0,
		"sessions": map[string]any{
			sessionID: map[string]any{
				"entries": []any{map[string]any{
					"behavior_index": 1, "ordinal": ordinal, "retracted": false, "witness": witness,
				}},
			},
		},
	}
	state["behavior_mapping"] = map[string]any{"entries": map[string]any{
		"1": map[string]any{"tag": "gaming-interactive"},
	}}
	return state
}
