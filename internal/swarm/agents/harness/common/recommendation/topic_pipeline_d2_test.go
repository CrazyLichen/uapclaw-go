package recommendation

// topic_pipeline_d2_test.go 对齐 topic_pipeline.py:4623-4906 契约的单元用例。

import (
	"testing"
)

// d2State 多行为 v4 状态（tag/action_type 可控）。每个 session 一条行为。
func d2State(entries [][3]any) map[string]any {
	state := HistoryEmptyV4State()
	state["version"] = 1
	sessions := map[string]any{}
	mappingEntries := map[string]any{}
	for index, entry := range entries {
		sessionID := entry[0].(string)
		ordinal := entry[1].(int)
		actionType := entry[2].(string)
		behaviorIndex := index + 1
		sessions[sessionID] = map[string]any{"entries": []any{map[string]any{
			"behavior_index": behaviorIndex, "ordinal": ordinal,
			"retracted": false,
			"witness": map[string]any{
				"role": "user", "timestamp_or_0": float64(100 + index),
				"content": "围棋问题", "supported": true,
			},
		}}}
		mappingEntries[itoaD2(behaviorIndex)] = map[string]any{
			"tag": "gaming-interactive", "action_type": actionType,
		}
	}
	state["history_ledger"] = map[string]any{
		"next_behavior_index": len(entries) + 1, "history_revision": 0,
		"sessions": sessions,
	}
	state["behavior_mapping"] = map[string]any{"entries": mappingEntries}
	return state
}

func itoaD2(value int) string {
	if value == 0 {
		return "0"
	}
	digits := ""
	for value > 0 {
		digits = string(rune('0'+value%10)) + digits
		value /= 10
	}
	return digits
}

// d2Records 会话记录：每条行为一个已完结用户请求。
func d2Records(count int) map[string][]map[string]any {
	records := map[string][]map[string]any{}
	for index := 0; index < count; index++ {
		requestID := "req-" + itoaD2(index+1)
		records["s"+itoaD2(index+1)] = []map[string]any{
			{"id": "evt-" + itoaD2(index+1), "request_id": requestID, "role": "user",
				"content": "围棋问题", "input_origin": "manual_free_text",
				"assistance_origin": "none", "timestamp": float64(100 + index)},
			{"request_id": requestID, "role": "assistant", "event_type": "chat.final",
				"content": "回答"},
			{"request_id": requestID, "role": "assistant", "event_type": "chat.request_completed",
				"id": requestID + ":request_completed", "status": "success",
				"timestamp": float64(110 + index), "feedback_only": true},
		}
	}
	return records
}

// TestScopeInputRevision 对齐 :4623-4699。
func TestScopeInputRevision(t *testing.T) {
	store := newTestStore(t)
	pipeline := NewTopicPipeline(store, nil, func() map[string][]map[string]any {
		return d2Records(2)
	}, nil, nil, "", nil, nil, nil, nil, nil, nil)
	revision, err := pipeline.ScopeInputRevision(GlobalNewChatScope)
	if err != nil {
		t.Fatalf("revision: %v", err)
	}
	if len(revision) != 4 { // 2×(answers+witnesses)
		t.Fatalf("entries: %d %v", len(revision), revision)
	}
	// 每个 scope 匹配的会话先 answers 后 witnesses
	first := revision[0].([]any)
	if first[1] != "completed_answers" {
		t.Fatalf("first entry: %v", first)
	}
	second := revision[1].([]any)
	if second[1] != "deprojected-project-history" && second[1] != nil {
		items := second[1].([]any)
		if len(items) != 1 {
			t.Fatalf("witness items: %v", items)
		}
	}
	// 非法 scope 报错
	if _, err := pipeline.ScopeInputRevision("project:"); err == nil {
		t.Fatalf("invalid scope should fail")
	}
}

// TestRefreshShortIntent_Baseline 对齐 :4781-4802 首次基线。
func TestRefreshShortIntent_Baseline(t *testing.T) {
	store := newTestStore(t)
	state := d2State([][3]any{{"s1", 0, "natural_question"}, {"s2", 0, "natural_question"},
		{"s3", 0, "natural_question"}})
	if _, err := HistorySaveV4State(store, state, nil, 40.0); err != nil {
		t.Fatalf("save: %v", err)
	}
	if _, err := PersistStateMaintenanceReceipt(store, state, "behavior_mapping", 50.0); err != nil {
		t.Fatalf("receipt: %v", err)
	}
	pipeline := NewTopicPipeline(store, nil, func() map[string][]map[string]any {
		return d2Records(3)
	}, nil, nil, "", nil, nil, nil, nil, nil, nil)
	if reason := pipeline.RefreshShortIntent(GlobalNewChatScope); reason != "" {
		t.Fatalf("first run: %s", reason)
	}
	shortStore, err := NewShortIntentStateStore(store)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	latest, err := shortStore.LoadLatestForScope(GlobalNewChatScope)
	if err != nil || latest == nil {
		t.Fatalf("latest: %+v %v", latest, err)
	}
	// 三条自然行为 ≥ MIN_DRIFT_SAMPLES → 基线锚点就绪
	if latest.ShortIntentVersion != 0 || latest.DriftAnchorSnapshotID == "" {
		t.Fatalf("baseline: %+v", latest)
	}
	if len(latest.DriftAnchorEventIDs) != 3 ||
		len(latest.DriftAnchorIntentClusters) != 3 {
		t.Fatalf("anchor: %v", latest.DriftAnchorEventIDs)
	}
	// 幂等：同快照再跑不新增
	if reason := pipeline.RefreshShortIntent(GlobalNewChatScope); reason != "" {
		t.Fatalf("second run: %s", reason)
	}
	again, _ := shortStore.LoadLatestForScope(GlobalNewChatScope)
	if again.InputSnapshotID != latest.InputSnapshotID {
		t.Fatalf("idempotent: %s vs %s", again.InputSnapshotID, latest.InputSnapshotID)
	}
	// 非法 scope
	if reason := pipeline.RefreshShortIntent("project:"); reason != "invalid_context_scope" {
		t.Fatalf("invalid: %s", reason)
	}
}

// TestRefreshShortIntent_HighIntentTrigger 对齐 :4852-4905 触发递增。
func TestRefreshShortIntent_HighIntentTrigger(t *testing.T) {
	store := newTestStore(t)
	state := d2State([][3]any{{"s1", 0, "natural_question"}, {"s2", 0, "natural_question"},
		{"s3", 0, "natural_question"}})
	if _, err := HistorySaveV4State(store, state, nil, 40.0); err != nil {
		t.Fatalf("save: %v", err)
	}
	if _, err := PersistStateMaintenanceReceipt(store, state, "behavior_mapping", 50.0); err != nil {
		t.Fatalf("receipt: %v", err)
	}
	pipeline := NewTopicPipeline(store, nil, func() map[string][]map[string]any {
		return d2Records(4)
	}, nil, nil, "", nil, nil, nil, nil, nil, nil)
	if reason := pipeline.RefreshShortIntent(GlobalNewChatScope); reason != "" {
		t.Fatalf("baseline run: %s", reason)
	}
	// 新行为：correction → high_intent_kind=correction → 触发
	// （先跑一次 3 行为基线，v4 状态升级到 4 行为后再次求值）
	state1 := d2State([][3]any{{"s1", 0, "natural_question"}, {"s2", 0, "natural_question"},
		{"s3", 0, "natural_question"}})
	_ = state1
	state2 := d2State([][3]any{{"s1", 0, "natural_question"}, {"s2", 0, "natural_question"},
		{"s3", 0, "natural_question"}, {"s4", 0, "correction"}})
	if _, err := HistorySaveV4State(store, state2, nil, 60.0); err != nil {
		t.Fatalf("save v2: %v", err)
	}
	if _, err := PersistStateMaintenanceReceipt(store, state2, "behavior_mapping", 70.0); err != nil {
		t.Fatalf("receipt v2: %v", err)
	}
	if reason := pipeline.RefreshShortIntent(GlobalNewChatScope); reason != "" {
		t.Fatalf("trigger run: %s", reason)
	}
	shortStore, _ := NewShortIntentStateStore(store)
	latest, err := shortStore.LoadLatestForScope(GlobalNewChatScope)
	if err != nil || latest == nil {
		t.Fatalf("latest: %+v %v", latest, err)
	}
	if latest.ShortIntentVersion != 1 {
		t.Fatalf("version: %d", latest.ShortIntentVersion)
	}
	found := false
	for _, reason := range latest.TriggerReasons {
		if reason == "high_intent_event" {
			found = true
		}
	}
	if !found {
		t.Fatalf("reasons: %v", latest.TriggerReasons)
	}
	// 触发后锚点 = 参照等长当前窗口（3 条）
	if latest.DriftAnchorSnapshotID == "" || len(latest.DriftAnchorEventIDs) != 3 {
		t.Fatalf("anchor: %+v", latest)
	}
}
