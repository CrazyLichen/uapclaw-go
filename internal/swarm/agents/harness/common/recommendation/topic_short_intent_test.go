package recommendation

// topic_short_intent_test.go 对齐 topic_short_intent.py 契约的单元用例：
// 求值三原因、store 持久化版本递增与 scope 隔离、快照 ID 前缀解析。

import (
	"testing"
)

// behavior 快捷构造。
func behavior(eventID, cluster string) *ShortIntentBehavior {
	return &ShortIntentBehavior{
		EventID: eventID, IntentCluster: cluster, ObservedAt: 1.0,
		InputOrigin: "manual_free_text", AssistanceOrigin: "none",
	}
}

// TestEvaluateShortIntent_HighIntent 对齐 :443-459。
func TestEvaluateShortIntent_HighIntent(t *testing.T) {
	current := []*ShortIntentBehavior{
		behavior("e1", "tech-ai-ml"),
		{EventID: "e2", IntentCluster: "tech-ai-ml", InputOrigin: "manual_free_text",
			AssistanceOrigin: "none", HighIntentKind: "correction"},
		{EventID: "e3", IntentCluster: "tech-ai-ml", InputOrigin: "manual", // 非自然被滤
			AssistanceOrigin: "none", HighIntentKind: "correction"},
	}
	decision, err := EvaluateShortIntent("v6-short:global_new_chat:0", nil, current, nil, false)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	reasons := decision.TriggerReasons
	if len(reasons) != 1 || reasons[0] != "high_intent_event" {
		t.Fatalf("reasons: %v", reasons)
	}
	lineage := decision.ReasonEvidenceLineage[0]
	if ids := pyListStrings(lineage["event_ids"]); len(ids) != 1 || ids[0] != "e2" {
		t.Fatalf("event ids: %v", lineage["event_ids"])
	}
	if kinds := pyListStrings(lineage["intent_kinds"]); len(kinds) != 1 || kinds[0] != "correction" {
		t.Fatalf("kinds: %v", lineage["intent_kinds"])
	}
}

// TestEvaluateShortIntent_SemanticTurnover 对齐 :461-477。
func TestEvaluateShortIntent_SemanticTurnover(t *testing.T) {
	reference := []*ShortIntentBehavior{
		behavior("r1", "finance-investment"), behavior("r2", "finance-investment"),
		behavior("r3", "finance-investment"),
	}
	current := []*ShortIntentBehavior{
		behavior("c1", "tech-ai-ml"), behavior("c2", "tech-ai-ml"),
		behavior("c3", "tech-ai-ml"),
	}
	decision, err := EvaluateShortIntent("snap-1", reference, current, nil, false)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	found := false
	for _, reason := range decision.TriggerReasons {
		if reason == "semantic_turnover" {
			found = true
		}
	}
	if !found {
		t.Fatalf("reasons: %v", decision.TriggerReasons)
	}
	if clusters := decision.DominantIntentClusters; len(clusters) != 1 || clusters[0] != "tech-ai-ml" {
		t.Fatalf("dominant: %v", clusters)
	}
}

// TestEvaluateShortIntent_无原因 对齐空触发与主导簇并列。
func TestEvaluateShortIntent_NoReasons(t *testing.T) {
	// 同一多重集换序：主导簇一致、漂移不触发 → 无原因
	reference := []*ShortIntentBehavior{behavior("r1", "a"), behavior("r2", "a"), behavior("r3", "b")}
	current := []*ShortIntentBehavior{behavior("c1", "a"), behavior("c2", "b"), behavior("c3", "a")}
	decision, err := EvaluateShortIntent("snap-1", reference, current, nil, false)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if len(decision.TriggerReasons) != 0 {
		t.Fatalf("reasons: %v", decision.TriggerReasons)
	}
	if clusters := decision.DominantIntentClusters; len(clusters) != 1 || clusters[0] != "a" {
		t.Fatalf("dominant: %v", clusters)
	}
	if decision.MobileDrift != nil && decision.MobileDrift.ShouldUpdate {
		t.Fatalf("identical multiset should not drift")
	}
	// 主导簇并列：只给当前行为（无参照即无漂移/换手）
	tieCurrent := []*ShortIntentBehavior{behavior("c1", "a"), behavior("c2", "a"),
		behavior("c3", "b"), behavior("c4", "b")}
	tie, err := EvaluateShortIntent("snap-2", nil, tieCurrent, nil, false)
	if err != nil {
		t.Fatalf("tie evaluate: %v", err)
	}
	if clusters := tie.DominantIntentClusters; len(clusters) != 2 || clusters[0] != "a" || clusters[1] != "b" {
		t.Fatalf("dominant tie: %v", clusters)
	}
}

// TestEvaluateShortIntent_空快照ID 对齐 :414-415。
func TestEvaluateShortIntent_EmptySnapshotID(t *testing.T) {
	if _, err := EvaluateShortIntent("  ", nil, nil, nil, false); err == nil {
		t.Fatalf("empty snapshot id should fail")
	}
}

// TestEvaluateShortIntent_HighIntentBehaviorsProvided 对齐 :443-445 显式来源。
func TestEvaluateShortIntent_HighIntentBehaviorsProvided(t *testing.T) {
	current := []*ShortIntentBehavior{behavior("c1", "a")}
	highIntent := []*ShortIntentBehavior{
		{EventID: "h1", IntentCluster: "a", InputOrigin: "manual_free_text",
			AssistanceOrigin: "none", HighIntentKind: "new_goal"},
	}
	decision, err := EvaluateShortIntent("snap-1", nil, current, highIntent, true)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if len(decision.TriggerReasons) != 1 || decision.TriggerReasons[0] != "high_intent_event" {
		t.Fatalf("reasons: %v", decision.TriggerReasons)
	}
}

// TestShortIntentScopePrefix 对齐 :180-198 快照 ID 解析。
func TestShortIntentScopePrefix(t *testing.T) {
	cases := []struct {
		id     string
		expect string
	}{
		{"v6-short:global_new_chat:0", "v6-short:global_new_chat:"},
		{"v6-short:global_new_chat:" + hex64("abc") + ":5", "v6-short:global_new_chat:"},
		{"v6-short:project:p1:3", "v6-short:project:p1:"},
		{"other:0", ""},
		{"v6-short:scope", ""},
		{"v6-short::0", ""},
		{"v6-short:scope:x", ""},
	}
	for _, testCase := range cases {
		if got := shortIntentScopePrefix(testCase.id); got != testCase.expect {
			t.Fatalf("%q: got %q want %q", testCase.id, got, testCase.expect)
		}
	}
}

// TestShortIntentStateStore_PersistVersions 对齐 :290-380 版本递增语义。
func TestShortIntentStateStore_PersistVersions(t *testing.T) {
	store, err := NewShortIntentStateStore(newTestStore(t))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	// 基线（无触发）→ 版本 0
	result, err := store.Persist(&ShortIntentDecision{
		InputSnapshotID: "v6-short:global_new_chat:0",
	}, 100.0)
	if err != nil || !result.Created || result.VersionChanged {
		t.Fatalf("baseline: %+v %v", result, err)
	}
	if result.State.ShortIntentVersion != 0 {
		t.Fatalf("version: %d", result.State.ShortIntentVersion)
	}
	// 触发 → 版本 1
	result2, err := store.Persist(&ShortIntentDecision{
		InputSnapshotID: "v6-short:global_new_chat:1",
		TriggerReasons:  []string{"high_intent_event"},
	}, 200.0)
	if err != nil || !result2.Created || !result2.VersionChanged {
		t.Fatalf("trigger: %+v %v", result2, err)
	}
	if result2.State.ShortIntentVersion != 1 {
		t.Fatalf("version: %d", result2.State.ShortIntentVersion)
	}
	// 无触发 → 版本保持 1
	result3, err := store.Persist(&ShortIntentDecision{
		InputSnapshotID: "v6-short:global_new_chat:2",
	}, 300.0)
	if err != nil || result3.State.ShortIntentVersion != 1 {
		t.Fatalf("no-change version: %d %v", result3.State.ShortIntentVersion, err)
	}
	// 幂等：同快照 ID → created=false
	result4, err := store.Persist(&ShortIntentDecision{
		InputSnapshotID: "v6-short:global_new_chat:2",
	}, 400.0)
	if err != nil || result4.Created || result4.VersionChanged {
		t.Fatalf("idempotent: %+v %v", result4, err)
	}
	if result4.State.EvaluatedAt != 300.0 {
		t.Fatalf("kept original: %v", result4.State.EvaluatedAt)
	}
	// 非法 evaluated_at
	if _, err := store.Persist(&ShortIntentDecision{InputSnapshotID: "v6-short:global_new_chat:9"}, 0); err == nil {
		t.Fatalf("zero evaluated_at should fail")
	}
}

// TestShortIntentStateStore_ScopeIsolation 对齐 scope 前缀版本独立。
func TestShortIntentStateStore_ScopeIsolation(t *testing.T) {
	store, err := NewShortIntentStateStore(newTestStore(t))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	if _, err := store.Persist(&ShortIntentDecision{
		InputSnapshotID: "v6-short:global_new_chat:1",
		TriggerReasons:  []string{"high_intent_event"},
	}, 100.0); err != nil {
		t.Fatalf("global: %v", err)
	}
	// 项目 scope 从 0 开始
	result, err := store.Persist(&ShortIntentDecision{
		InputSnapshotID: "v6-short:project:p1:1",
	}, 200.0)
	if err != nil || result.State.ShortIntentVersion != 0 {
		t.Fatalf("project baseline: %+v %v", result, err)
	}
	// load_latest_for_scope 只看本 scope
	latest, err := store.LoadLatestForScope("project:p1")
	if err != nil || latest.InputSnapshotID != "v6-short:project:p1:1" {
		t.Fatalf("scope latest: %+v %v", latest, err)
	}
	none, err := store.LoadLatestForScope("project:other")
	if err != nil || none != nil {
		t.Fatalf("other scope: %+v %v", none, err)
	}
	// load_trigger_for_version 只取触发行
	trigger, err := store.LoadTriggerForVersion("global_new_chat", 1)
	if err != nil || trigger == nil || len(trigger.TriggerReasons) == 0 {
		t.Fatalf("trigger: %+v %v", trigger, err)
	}
	noTrigger, err := store.LoadTriggerForVersion("project:p1", 0)
	if err != nil || noTrigger != nil {
		t.Fatalf("version 0 has no trigger: %+v %v", noTrigger, err)
	}
	// load_for_snapshot / load_latest
	bySnapshot, err := store.LoadForSnapshot("v6-short:global_new_chat:1")
	if err != nil || bySnapshot == nil {
		t.Fatalf("by snapshot: %+v %v", bySnapshot, err)
	}
	allLatest, err := store.LoadLatest()
	if err != nil || allLatest == nil {
		t.Fatalf("latest: %+v %v", allLatest, err)
	}
}

// TestShortIntentStateStore_LegacyAnchor 对齐 :215-250。
func TestShortIntentStateStore_LegacyAnchor(t *testing.T) {
	store, err := NewShortIntentStateStore(newTestStore(t))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	// 两条无触发行后一条触发行：legacy anchor 取最后触发
	for index, reasons := range [][]string{nil, nil, {"high_intent_event"}} {
		decision := &ShortIntentDecision{InputSnapshotID: snapshotIDFor("global_new_chat", index)}
		if len(reasons) > 0 {
			decision.TriggerReasons = reasons
		}
		if _, err := store.Persist(decision, float64(index+1)); err != nil {
			t.Fatalf("persist %d: %v", index, err)
		}
	}
	anchor, err := store.LoadLegacyAnchorForScope("global_new_chat")
	if err != nil || anchor == nil || len(anchor.TriggerReasons) == 0 {
		t.Fatalf("anchor: %+v %v", anchor, err)
	}
}

// snapshotIDFor 快照 ID 构造（带 64 位 hex digest 形态）。
func snapshotIDFor(scope string, version int) string {
	return "v6-short:" + scope + ":" + hex64(scope+string(rune('a'+version))) + ":" +
		itoa(version)
}

func itoa(value int) string {
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

// TestShortIntentState_RoundTrip 对齐 :81-123 载荷往返。
func TestShortIntentState_RoundTrip(t *testing.T) {
	state := &ShortIntentState{
		ShortIntentVersion: 3, InputSnapshotID: "v6-short:global_new_chat:3",
		EvaluatedAt: 42.0, TriggerReasons: []string{"mobile_drift"},
		ReasonEvidenceLineage:     []map[string]any{{"reason": "mobile_drift"}},
		DominantIntentClusters:    []string{"tech-ai-ml"},
		DriftAnchorSnapshotID:     "v6-short:global_new_chat:2",
		DriftAnchorEventIDs:       []string{"e1"},
		DriftAnchorIntentClusters: []string{"finance-investment"},
	}
	payload := state.AsPayload()
	if payload["drift_anchor_snapshot_id"] != "v6-short:global_new_chat:2" {
		t.Fatalf("anchor: %v", payload["drift_anchor_snapshot_id"])
	}
	restored := ShortIntentStateFromPayload(payload)
	if restored.ShortIntentVersion != 3 || restored.EvaluatedAt != 42.0 ||
		len(restored.TriggerReasons) != 1 || len(restored.ReasonEvidenceLineage) != 1 {
		t.Fatalf("restored: %+v", restored)
	}
	if restored.DriftAnchorSnapshotID != "v6-short:global_new_chat:2" {
		t.Fatalf("anchor restored: %v", restored.DriftAnchorSnapshotID)
	}
	// 空锚点 → 空串
	empty := ShortIntentStateFromPayload(map[string]any{
		"short_intent_version": 0, "input_snapshot_id": "x", "evaluated_at": 1.0,
		"trigger_reasons": []any{}, "reason_evidence_lineage": []any{},
		"dominant_intent_clusters": []any{},
	})
	if empty.DriftAnchorSnapshotID != "" {
		t.Fatalf("empty anchor: %v", empty.DriftAnchorSnapshotID)
	}
}

// TestBuildGenerationStateProjection 对齐 :488-502。
func TestBuildGenerationStateProjection(t *testing.T) {
	state := &ShortIntentState{
		ShortIntentVersion: 2, InputSnapshotID: "v6-short:global_new_chat:2",
		TriggerReasons: []string{"semantic_turnover"},
	}
	projection, err := BuildGenerationStateProjection(state, 5)
	if err != nil {
		t.Fatalf("projection: %v", err)
	}
	if projection["profile_semantic_version"] != 5 ||
		projection["short_intent_version"] != 2 ||
		projection["input_snapshot_id"] != "v6-short:global_new_chat:2" {
		t.Fatalf("fields: %v", projection)
	}
	if reasons := projection["short_intent_trigger_reasons"].([]string); len(reasons) != 1 {
		t.Fatalf("reasons: %v", reasons)
	}
	if _, err := BuildGenerationStateProjection(state, -1); err == nil {
		t.Fatalf("negative version should fail")
	}
}
