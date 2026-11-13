package recommendation

// topic_outcomes_test.go 对齐 tests/unit_tests/test_topic_recommendation.py 中
// outcome 工件契约（revisit pair / RID tuple / utility example / training slice /
// starter outcome 往返）与新页结果信号队列的用例。

import "testing"

// summaryForTests 构造合法摘要工件。
func summaryForTests(t *testing.T, summaryID, sessionID string, completedAt float64) *SessionSummaryArtifact {
	t.Helper()
	boundary, err := NewSessionCompletionBoundary(sessionID, "evt", "req", 1, completedAt, "success")
	if err != nil {
		t.Fatalf("boundary: %v", err)
	}
	locators := []*SessionSourceLocator{{SessionID: sessionID, Ordinal: 0, EventID: "evt"}}
	summary, err := NewSessionSummaryArtifact(summaryID, sessionID, GlobalNewChatScope,
		"摘要内容", "manual_free_text", "none", []string{"evt"}, locators, boundary,
		completedAt, "provider", "v1", "cfg-v1")
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	return summary
}

// TestBuildRevisitPair_排除原因枚举 对齐 :170-183 五个排除分支 + 自然合格。
func TestBuildRevisitPair(t *testing.T) {
	earlier := summaryForTests(t, "summary:s1:aaa", "s1", 10.0)
	later := summaryForTests(t, "summary:s2:bbb", "s2", 20.0)

	// 同会话
	sameLater := summaryForTests(t, "summary:s1:ccc", "s1", 30.0)
	pair, err := BuildRevisitPair(earlier, sameLater, true, "m", "mv1")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if pair.NaturalEligible || str(pair.ExclusionReason) != "same_session_not_revisit" {
		t.Fatalf("reason=%v", pair.ExclusionReason)
	}

	// 后摘要不晚于先摘要
	notLater := summaryForTests(t, "summary:s2:ddd", "s2", 5.0)
	pair2, _ := BuildRevisitPair(earlier, notLater, true, "m", "mv1")
	if str(pair2.ExclusionReason) != "later_summary_not_later" {
		t.Fatalf("reason=%v", pair2.ExclusionReason)
	}

	// 输入来源不合格
	assisted := summaryForTests(t, "summary:s2:eee", "s2", 30.0)
	assisted.InputOrigin = "pasted"
	pair3, _ := BuildRevisitPair(earlier, assisted, true, "m", "mv1")
	if str(pair3.ExclusionReason) != "ineligible_input_origin" {
		t.Fatalf("reason=%v", pair3.ExclusionReason)
	}

	// 自然合格
	pair4, err := BuildRevisitPair(earlier, later, true, "matcher", "mv1")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if !pair4.NaturalEligible || pair4.ExclusionReason != nil {
		t.Fatalf("should be eligible: %v", pair4.ExclusionReason)
	}
	if pair4.RevisitPairID != "revisit:summary:s1:aaa:summary:s2:bbb:mv1" {
		t.Fatalf("id=%q", pair4.RevisitPairID)
	}

	// 往返
	restored, err := RevisitPairFromPayload(pair4.AsPayload())
	if err != nil || restored.RevisitPairID != pair4.RevisitPairID {
		t.Fatalf("round trip: %v", err)
	}
}

// TestRevisitPairFromPayload_StrictSchema 对齐 _from_payload 键集合恰等。
func TestRevisitPairFromPayloadStrictSchema(t *testing.T) {
	payload := map[string]any{
		"revisit_pair_id": "r", "earlier_summary_id": "a", "later_summary_id": "b",
		"earlier_session_id": "s1", "later_session_id": "s2", "context_scope": "global_new_chat",
		"match_label": true, "matcher_id": "m", "matcher_version": "v",
		"natural_eligible": false, "exclusion_reason": "same_session_not_revisit",
		"observed_at": 1.0, "schema_version": RevisitPairSchemaVersion,
		"extra": 1,
	}
	if _, err := RevisitPairFromPayload(payload); err == nil {
		t.Fatalf("extra key should fail")
	}
	// natural_eligible=true 且 exclusion_reason 非空 → 契约失败
	payload["extra"] = nil
	delete(payload, "extra")
	payload["natural_eligible"] = true
	if _, err := RevisitPairFromPayload(payload); err == nil {
		t.Fatalf("eligible with reason should fail")
	}
}

// TestBuildRIDTrainingTuple_契约 对齐 :342-386。
func TestBuildRIDTrainingTuple(t *testing.T) {
	earlier := summaryForTests(t, "summary:s1:aaa", "s1", 10.0)
	later := summaryForTests(t, "summary:s2:bbb", "s2", 20.0)
	pair, err := BuildRevisitPair(earlier, later, true, "m", "mv1")
	if err != nil {
		t.Fatalf("pair: %v", err)
	}
	tuple, err := BuildRIDTrainingTuple("rid-training-v3:b1:p1:snap:mv1", "b1", "snap",
		"local", pair, []string{"neg-intra"}, []string{"neg-inter"})
	if err != nil {
		t.Fatalf("tuple: %v", err)
	}
	if tuple.PositiveSummaryID != earlier.SessionSummaryID ||
		tuple.PositiveWitnessSummaryID != later.SessionSummaryID {
		t.Fatalf("positive wiring wrong")
	}
	// 非 natural pair 拒绝
	ineligible := summaryForTests(t, "summary:s1:ccc", "s1", 10.0)
	ineligible.InputOrigin = "pasted"
	badPair, _ := BuildRevisitPair(ineligible, later, true, "m", "mv1")
	if _, err := BuildRIDTrainingTuple("x", "b1", "snap", "local", badPair, []string{"a"}, []string{"b"}); err == nil {
		t.Fatalf("ineligible pair should fail")
	}
	// 负样本空 → 拒绝
	if _, err := BuildRIDTrainingTuple("x", "b1", "snap", "local", pair, []string{}, []string{"b"}); err == nil {
		t.Fatalf("empty intra should fail")
	}
}

// TestBuildUtilityTrainingExample_特征排除 对齐 :784-836 五个排除分支。
func TestBuildUtilityTrainingExample(t *testing.T) {
	outcome := &StarterOutcome{
		OutcomeID: "outcome:1", UserStateSnapshotID: "snap1", ManifestID: "m",
		GenerationBatchID: "b", ExposureID: "e", ExposurePosition: 1,
		CardRevisionID: "r", ContextScope: GlobalNewChatScope,
		ProviderResponseIDs: []string{"resp"}, ComponentRequestIDs: [][2]string{{"c", "r"}},
		ExposedAt: 10.0, LabelHorizon: 20.0, OutcomeLabel: "shown_only",
		VersionRefs: map[string]string{},
	}
	// 缺特征快照
	ex1, err := BuildUtilityTrainingExample(outcome, nil)
	if err != nil || str(ex1.AsPayload()["training_exclusion_reason"]) != "missing_feature_snapshot" {
		t.Fatalf("reason=%v err=%v", ex1.TrainingExclusionReason, err)
	}
	// 快照 ID 不匹配
	ex2, _ := BuildUtilityTrainingExample(outcome, map[string]any{"snapshot_id": "snap2"})
	if str(ex2.AsPayload()["training_exclusion_reason"]) != "feature_snapshot_id_mismatch" {
		t.Fatalf("reason=%v", ex2.TrainingExclusionReason)
	}
	// 时间越界
	ex3, _ := BuildUtilityTrainingExample(outcome, map[string]any{
		"snapshot_id": "snap1", "observed_at": 30.0, "version": "v", "features": map[string]any{}})
	if str(ex3.AsPayload()["training_exclusion_reason"]) != "feature_snapshot_after_label_horizon" {
		t.Fatalf("reason=%v", ex3.TrainingExclusionReason)
	}
	// 合格：特征被收编，排除原因为空
	ex4, err := BuildUtilityTrainingExample(outcome, map[string]any{
		"snapshot_id": "snap1", "observed_at": 15.0, "version": "v", "features": map[string]any{"f": 1}})
	if err != nil || ex4.TrainingExclusionReason != nil {
		t.Fatalf("should admit: %v %v", ex4.TrainingExclusionReason, err)
	}
	if len(mapAnyOf(ex4.AsPayload()["feature_snapshot"])) == 0 {
		t.Fatalf("feature snapshot should be carried")
	}
}

// TestBuildTrainingSliceManifest_排除合并 对齐 :526-570。
func TestBuildTrainingSliceManifest(t *testing.T) {
	scope := GlobalNewChatScope
	example := &UtilityTrainingExample{
		UtilityExampleID: "u1", OutcomeID: "o1", UserStateSnapshotID: "s1",
		ContextScope: scope, LabelHorizon: 10.0, Labels: map[string]any{},
		FeatureSnapshot: nil, VersionRefs: map[string]string{},
		SchemaVersion: UtilityTrainingSchemaVersion,
	}
	example.TrainingExclusionReason = strPtr("pre_excluded")
	manifest, err := BuildTrainingSliceManifest("slice-1", 0.0, 20.0, 15.0,
		map[string]any{"training_domain": "utility"}, scope,
		map[string]string{"utility_training_example": UtilityTrainingSchemaVersion},
		[]*UtilityTrainingExample{example}, map[string]float64{"pair_count": 1},
		[]string{"base-artifact"}, nil)
	if err != nil {
		t.Fatalf("manifest: %v", err)
	}
	if !containsString(manifest.IncludedArtifactIDs, "base-artifact") {
		t.Fatalf("included=%v", manifest.IncludedArtifactIDs)
	}
	// Python: 已带排除原因的样本即使无特征快照也保持原原因被排除
	if manifest.ExcludedArtifactReasons["u1"] != "pre_excluded" {
		t.Fatalf("reason=%q", manifest.ExcludedArtifactReasons["u1"])
	}
	// scope 不匹配 → context_scope_mismatch 覆盖原排除原因
	mismatch := &UtilityTrainingExample{
		UtilityExampleID: "u2", ContextScope: "project:p1", LabelHorizon: 10.0,
		Labels: map[string]any{}, SchemaVersion: UtilityTrainingSchemaVersion,
		VersionRefs: map[string]string{},
	}
	manifest2, _ := BuildTrainingSliceManifest("slice-2", 0.0, 20.0, 15.0,
		map[string]any{}, scope, map[string]string{"k": "v"},
		[]*UtilityTrainingExample{mismatch}, nil, nil, nil)
	if manifest2.ExcludedArtifactReasons["u2"] != "context_scope_mismatch" {
		t.Fatalf("reason=%q", manifest2.ExcludedArtifactReasons["u2"])
	}
}

// TestPersistOutcomeArtifact_四列存储 对齐 :929-952 + :864-921。
func TestPersistOutcomeArtifact(t *testing.T) {
	store := newTestStore(t)
	earlier := summaryForTests(t, "summary:s1:aaa", "s1", 10.0)
	later := summaryForTests(t, "summary:s2:bbb", "s2", 20.0)
	pair, _ := BuildRevisitPair(earlier, later, true, "m", "mv1")
	if !PersistOutcomeArtifact(store, pair, 20.0) {
		t.Fatalf("persist failed")
	}
	eventType, artifactID := OutcomeArtifactIdentity(pair)
	if eventType != "revisit_pair" || artifactID != pair.RevisitPairID {
		t.Fatalf("identity=%s/%s", eventType, artifactID)
	}
	rows, err := store.QueryEvents(QueryEventsOptions{
		EventType: eventType, PolicyID: &productPolicyIDPtr, Limit: 8,
	})
	if err != nil || len(rows) != 1 {
		t.Fatalf("rows=%d err=%v", len(rows), err)
	}
	restored, err := RevisitPairFromPayload(rows[0]["payload"])
	if err != nil || restored.RevisitPairID != pair.RevisitPairID {
		t.Fatalf("restore: %v", err)
	}
	// 非法时间戳拒绝
	if PersistOutcomeArtifact(store, pair, -1.0) {
		t.Fatalf("negative timestamp should fail")
	}
}

// TestRecordTerminalConversationDepth 对齐 :1531-1580。
func TestRecordTerminalConversationDepth(t *testing.T) {
	store := newTestStore(t)
	// 无 session_bound → 拒绝
	if RecordTerminalConversationDepth(store, "m", "e", "r", "s1", 3, 100.0) {
		t.Fatalf("no start event should fail")
	}
	// 写入 session_bound
	startPayload := map[string]any{
		"manifest_id": "m", "exposure_id": "e", "card_revision_id": "r",
		"started_session_id": "s1", "context_scope": GlobalNewChatScope,
	}
	_, _ = store.RecordEvent(map[string]any{
		"session_id": "s1", "slate_id": "e", "event_type": "session_bound",
		"topic_id": "r", "policy_id": ProductPolicyID, "payload": startPayload,
		"created_at": 50.0,
	})
	if !RecordTerminalConversationDepth(store, "m", "e", "r", "s1", 3, 100.0) {
		t.Fatalf("depth record failed")
	}
	// 负深度拒绝
	if RecordTerminalConversationDepth(store, "m", "e", "r", "s1", -1, 100.0) {
		t.Fatalf("negative depth should fail")
	}
	// 早于 start 拒绝
	if RecordTerminalConversationDepth(store, "m", "e", "r", "s1", 3, 40.0) {
		t.Fatalf("before start should fail")
	}
	// 深度事件落库
	rows, _ := store.QueryEvents(QueryEventsOptions{EventType: "conversation_depth",
		PolicyID: &productPolicyIDPtr, Limit: 8})
	if len(rows) != 1 || intOf(mapAnyOf(rows[0]["payload"])["conversation_depth"]) != 3 {
		t.Fatalf("depth payload wrong: %v", rows)
	}
}

// TestRecordTerminalFutureActivity_资格标注 对齐 :1583-1673。
func TestRecordTerminalFutureActivity(t *testing.T) {
	store := newTestStore(t)
	_, _ = store.RecordEvent(map[string]any{
		"session_id": "s1", "slate_id": "e", "event_type": "session_bound",
		"topic_id": "r", "policy_id": ProductPolicyID,
		"payload": map[string]any{
			"manifest_id": "m", "exposure_id": "e", "card_revision_id": "r",
			"started_session_id": "s1", "context_scope": GlobalNewChatScope,
		},
		"created_at": 50.0,
	})
	// 自然合格：manual_free_text + none + 不同会话
	if !RecordTerminalFutureActivity(store, "m", "e", "r", "s1", "s2",
		"evt1", "session:s2:history:0", "manual_free_text", "none",
		GlobalNewChatScope, 100.0) {
		t.Fatalf("natural activity should record")
	}
	// 协助输入 → assisted_input_excluded
	if !RecordTerminalFutureActivity(store, "m", "e", "r", "s1", "s3",
		"evt2", "session:s3:history:0", "manual_free_text", "assistant",
		GlobalNewChatScope, 110.0) {
		t.Fatalf("assisted activity should still record (with exclusion)")
	}
	rows, _ := store.QueryEvents(QueryEventsOptions{EventType: "future_activity",
		PolicyID: &productPolicyIDPtr, Limit: 8})
	if len(rows) != 2 {
		t.Fatalf("rows=%d", len(rows))
	}
	first := mapAnyOf(rows[1]["payload"])
	second := mapAnyOf(rows[0]["payload"])
	natural := first
	if str(first["later_session_id"]) != "s2" {
		natural = second
	}
	if natural["natural_revisit_eligible"] != true || natural["exclusion_reason"] != nil {
		t.Fatalf("natural eligibility wrong: %v", natural)
	}
}

// TestNaturalExclusionReason_枚举 对齐 :1583-1596。
func TestNaturalExclusionReason(t *testing.T) {
	if naturalExclusionReason("pasted", "none", "s1", "s2") != "ineligible_input_origin" {
		t.Fatalf("input origin")
	}
	if naturalExclusionReason("manual_free_text", "assistant", "s1", "s2") != "assisted_input_excluded" {
		t.Fatalf("assistance origin")
	}
	if naturalExclusionReason("manual_free_text", "none", "s1", "s1") != "same_session_not_revisit" {
		t.Fatalf("same session")
	}
	if naturalExclusionReason("manual_free_text", "none", "s1", "s2") != "" {
		t.Fatalf("should be eligible")
	}
}

// TestNewPageOutcomeQueue_排队与排空 对齐 :1993-2021。
func TestNewPageOutcomeQueue(t *testing.T) {
	queue := &NewPageOutcomeQueue{}
	signal, err := NewNewPageOutcomeSignal("accepted", "m", "b", "e", "r",
		GlobalNewChatScope, nil, [][2]string{{"c1", "r1"}}, []string{"resp"}, nil,
		100.0, false)
	if err != nil {
		t.Fatalf("signal: %v", err)
	}
	if queue.Publish(signal) != "queued" || queue.PendingCount() != 1 {
		t.Fatalf("should queue")
	}
	var drained []string
	results := queue.AttachSink(func(s *NewPageOutcomeSignal) bool {
		drained = append(drained, s.CardRevisionID)
		return true
	})
	if len(results) != 1 || !results[0] || len(drained) != 1 {
		t.Fatalf("drain=%v", results)
	}
	if queue.PendingCount() != 0 {
		t.Fatalf("pending should be empty")
	}
	if queue.Publish(signal) != "recorded" {
		t.Fatalf("post-attach publish should record")
	}
	// 二次挂接返回 nil
	if queue.AttachSink(func(*NewPageOutcomeSignal) bool { return true }) != nil {
		t.Fatalf("second attach should return nil")
	}
}

// TestNewPageOutcomeSignal_HostWritten 对齐 :1967-1972。
func TestNewPageOutcomeSignalHostWritten(t *testing.T) {
	// host_written 不允许携带模型血缘
	if _, err := NewNewPageOutcomeSignal("accepted", "m", "b", "e", "r",
		GlobalNewChatScope, nil, [][2]string{{"c", "r"}}, []string{"resp"}, nil,
		100.0, true); err == nil {
		t.Fatalf("host-written with provenance should fail")
	}
	// 非法 scope
	if _, err := NewNewPageOutcomeSignal("accepted", "m", "b", "e", "r",
		"bogus", nil, [][2]string{{"c", "r"}}, []string{"resp"}, nil, 100.0, false); err == nil {
		t.Fatalf("bogus scope should fail")
	}
}

// TestArtifactFromPayload_分发 对齐 :839-861。
func TestArtifactFromPayload(t *testing.T) {
	earlier := summaryForTests(t, "summary:s1:aaa", "s1", 10.0)
	later := summaryForTests(t, "summary:s2:bbb", "s2", 20.0)
	pair, _ := BuildRevisitPair(earlier, later, true, "m", "mv1")
	artifact, err := ArtifactFromPayload(pair.AsPayload())
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if restored, ok := artifact.(*RevisitPair); !ok || restored.RevisitPairID != pair.RevisitPairID {
		t.Fatalf("wrong type: %T", artifact)
	}
	if _, err := ArtifactFromPayload(map[string]any{"schema_version": "bogus"}); err == nil {
		t.Fatalf("unknown version should fail")
	}
}

// TestProjectIDForScope 对齐 topic_product.py:3254-3266。
func TestProjectIDForScope(t *testing.T) {
	if id, err := ProjectIDForScope(GlobalNewChatScope); err != nil || id != "" {
		t.Fatalf("global: %v %v", id, err)
	}
	if id, err := ProjectIDForScope("project:abc"); err != nil || id != "abc" {
		t.Fatalf("project: %v %v", id, err)
	}
	if _, err := ProjectIDForScope("project: "); err == nil {
		t.Fatalf("blank project should fail")
	}
	if _, err := ProjectIDForScope("bogus"); err == nil {
		t.Fatalf("bogus scope should fail")
	}
}

// strPtr 字符串指针。
func strPtr(value string) *string { return &value }
