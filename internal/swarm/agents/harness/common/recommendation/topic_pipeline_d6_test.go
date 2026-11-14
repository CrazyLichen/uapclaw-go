package recommendation

// topic_pipeline_d6_test.go 对齐 topic_pipeline.py:5943-6343 契约的单元用例。

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// recordingInvoker 记录调用并可带小票的测试调用器。
type recordingInvoker struct {
	calls    map[string]int
	receipts []map[string]any
}

func newRecordingInvoker() *recordingInvoker {
	return &recordingInvoker{calls: map[string]int{}, receipts: []map[string]any{}}
}

func (f *recordingInvoker) Invoke(ctx context.Context, stage, systemPrompt, payload string) (any, error) {
	f.calls[stage]++
	// 对齐 audited invoker：按载荷 request_id 记录厂商小票
	var parsed map[string]any
	_ = json.Unmarshal([]byte(payload), &parsed)
	f.AppendInvocationReceipt(map[string]any{
		"stage": stage, "lineage": map[string]any{
			"component_request_id": parsed["request_id"],
			"input_snapshot_id":    parsed["input_snapshot_id"]},
	})
	return map[string]any{"decisions": []any{
		map[string]any{"interest_summary_id": "int-a", "selected": true,
			"rank": 1, "resonance_score_nullable": nil},
	}}, nil
}

func (f *recordingInvoker) RecordValidation(response any, outcome string) {}

func (f *recordingInvoker) InvocationReceipts() []map[string]any { return f.receipts }

func (f *recordingInvoker) AppendInvocationReceipt(receipt map[string]any) {
	f.receipts = append(f.receipts, receipt)
}

// d6Snapshot 带 LangTermProfileInterest 的快照。
func d6Snapshot(t *testing.T) *V6ContextSnapshot {
	t.Helper()
	snapshot := &V6ContextSnapshot{
		State:           map[string]any{},
		ContextScope:    GlobalNewChatScope,
		InputSnapshotID: "snap-d6",
		ProfileProjection: map[string]any{
			"profile_semantic_version":   1,
			"abstract_interest_clusters": []any{"tech-ai-ml"},
		},
		ShortIntentState: &ShortIntentState{},
	}
	interest, err := NewLongTermProfileInterestInput("int-a", GlobalNewChatScope, 1,
		"tech-ai-ml", "想深入大模型评测", []string{"unit-1"})
	if err != nil {
		t.Fatalf("interest: %v", err)
	}
	snapshot.LongTermProfileInterests = []*LongTermProfileInterestInput{interest}
	return snapshot
}

// TestDistillInterests_NothingToJudge 对齐 :5979-5995 降级不调模型。
func TestDistillInterests_NothingToJudge(t *testing.T) {
	store := newTestStore(t)
	pipeline := NewTopicPipeline(store, nil, nil, nil, nil, "", nil, nil, nil, nil, nil,
		func() float64 { return 1000.0 })
	pipeline.StageTokenCounterFactory = func(string) (*StageTokenCounter, error) {
		return fixedCounter(), nil
	}
	emptySnapshot := &V6ContextSnapshot{
		State:             map[string]any{},
		ContextScope:      GlobalNewChatScope,
		InputSnapshotID:   "snap-empty",
		ProfileProjection: map[string]any{},
		ShortIntentState:  &ShortIntentState{},
	}
	invoker := newRecordingInvoker()
	result, err := pipeline.DistillInterests(emptySnapshot, nil,
		&InterestFeedback{EvidenceByUnit: map[string]map[string]bool{},
			SessionsByUnit: map[string]map[string]bool{}},
		&ArkBackgroundConfig{Model: "m", ContextWindowTokens: 100000},
		invoker, 0, false, false)
	if err != nil {
		t.Fatalf("distill: %v", err)
	}
	if result.Receipt.FallbackReason != RIDNothingToJudge ||
		result.Receipt.ScoreSemantics != "not_scored" {
		t.Fatalf("fallback: %+v", result.Receipt)
	}
	if invoker.calls["rid_interest_distiller"] != 0 {
		t.Fatalf("no model call expected")
	}
}

// TestDistillInterests_CallAndRemember 对齐 :6063-6103 调用与记忆。
func TestDistillInterests_CallAndRemember(t *testing.T) {
	store := newTestStore(t)
	pipeline := NewTopicPipeline(store, nil, nil, nil, nil, "", nil, nil, nil, nil, nil,
		func() float64 { return 1000.0 })
	pipeline.StageTokenCounterFactory = func(string) (*StageTokenCounter, error) {
		return fixedCounter(), nil
	}
	snapshot := d6Snapshot(t)
	invoker := newRecordingInvoker()
	feedback := &InterestFeedback{EvidenceByUnit: map[string]map[string]bool{},
		SessionsByUnit: map[string]map[string]bool{}}
	result, err := pipeline.DistillInterests(snapshot, nil, feedback,
		&ArkBackgroundConfig{Model: "m", ExpectedModel: "m-v1",
			ContextWindowTokens: 100000, Temperature: 0.3, TopP: 0.9, BaseURL: "https://ark"},
		invoker, 0, false, false)
	if err != nil {
		t.Fatalf("distill: %v", err)
	}
	if invoker.calls["rid_interest_distiller"] != 1 {
		t.Fatalf("calls: %d", invoker.calls["rid_interest_distiller"])
	}
	if len(result.Decisions) != 1 || result.Decisions[0].InterestSummaryID != "int-a" {
		t.Fatalf("decisions: %+v", result.Decisions)
	}
	// rid_user_context 落库
	rows, _ := store.QueryEvents(QueryEventsOptions{
		EventType: "rid_user_context_snapshot", Limit: 5, NewestFirst: true})
	if len(rows) != 1 {
		t.Fatalf("user context rows: %d", len(rows))
	}
	// rid_decision 记忆事件（provider_receipt 命中 component_request_id）
	decisionRows, _ := store.QueryEvents(QueryEventsOptions{
		EventType: RIDDecisionEventType, SlateID: stringPtr(GlobalNewChatScope),
		PolicyID: stringPtr(POLICY_ID), Limit: 5, NewestFirst: true})
	if len(decisionRows) != 1 {
		t.Fatalf("decision rows: %d", len(decisionRows))
	}
}

// TestDistillInterests_Reuse 对齐 :6163-6243 复用路径。
func TestDistillInterests_Reuse(t *testing.T) {
	store := newTestStore(t)
	pipeline := NewTopicPipeline(store, nil, nil, nil, nil, "", nil, nil, nil, nil, nil,
		func() float64 { return 1000.0 })
	pipeline.StageTokenCounterFactory = func(string) (*StageTokenCounter, error) {
		return fixedCounter(), nil
	}
	snapshot := d6Snapshot(t)
	config := &ArkBackgroundConfig{Model: "m", ExpectedModel: "m-v1",
		ContextWindowTokens: 100000, Temperature: 0.3, TopP: 0.9, BaseURL: "https://ark"}
	feedback := &InterestFeedback{EvidenceByUnit: map[string]map[string]bool{},
		SessionsByUnit: map[string]map[string]bool{}}

	invoker := newRecordingInvoker()
	if _, err := pipeline.DistillInterests(snapshot, nil, feedback, config,
		invoker, 0, false, false); err != nil {
		t.Fatalf("first: %v", err)
	}
	firstCalls := invoker.calls["rid_interest_distiller"]

	// 同日同输入第二次 → 复用，不再调用模型
	second := newRecordingInvoker()
	if _, err := pipeline.DistillInterests(snapshot, nil, feedback, config,
		second, 0, false, false); err != nil {
		t.Fatalf("second: %v", err)
	}
	if second.calls["rid_interest_distiller"] != 0 {
		t.Fatalf("reuse should not call model: %d", second.calls["rid_interest_distiller"])
	}
	// 复用后 provider_receipt 追加
	if len(second.InvocationReceipts()) != 1 {
		t.Fatalf("receipt appended: %d", len(second.InvocationReceipts()))
	}
	_ = firstCalls
	_ = strings.TrimSpace("")
}

// TestReusedRIDDecision_KeyMismatch 对齐 :6192-6205 键不一致不复用。
func TestReusedRIDDecision_KeyMismatch(t *testing.T) {
	store := newTestStore(t)
	pipeline := NewTopicPipeline(store, nil, nil, nil, nil, "", nil, nil, nil, nil, nil,
		func() float64 { return 1000.0 })
	// 写一条 reuse_key 不匹配的缓存
	if _, err := store.RecordEvent(map[string]any{
		"session_id": "topic-starter-rid", "slate_id": GlobalNewChatScope,
		"event_type": RIDDecisionEventType, "topic_id": "v6-rid-old",
		"policy_id": POLICY_ID, "payload": map[string]any{
			"reuse_key":  map[string]any{"input_snapshot_id": "other", "local_date": "1970-01-01"},
			"request_id": "v6-rid-old",
		},
		"created_at": 10.0,
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	pipeline.StageTokenCounterFactory = func(string) (*StageTokenCounter, error) {
		return fixedCounter(), nil
	}
	invoker := newRecordingInvoker()
	snapshot := d6Snapshot(t)
	feedback := &InterestFeedback{EvidenceByUnit: map[string]map[string]bool{},
		SessionsByUnit: map[string]map[string]bool{}}
	result, err := pipeline.DistillInterests(snapshot, nil, feedback,
		&ArkBackgroundConfig{Model: "m", ContextWindowTokens: 100000},
		invoker, 0, false, false)
	if err != nil {
		t.Fatalf("distill: %v", err)
	}
	// 键不匹配 → 正常调用模型
	if invoker.calls["rid_interest_distiller"] != 1 {
		t.Fatalf("should call model on mismatch: %d", invoker.calls["rid_interest_distiller"])
	}
	if result == nil || result.Receipt.RequestID == "v6-rid-old" {
		t.Fatalf("fresh decision expected")
	}
}

// TestHotNewsLead_BlockedEntries 对齐 :6261-6272 否定过滤与不可用。
func TestHotNewsLead_BlockedEntries(t *testing.T) {
	store := newTestStore(t)
	pipeline := NewTopicPipeline(store, nil, nil, nil, nil, "", nil, nil, nil, nil, nil,
		func() float64 { return 1000.0 })
	snapshot := d6Snapshot(t)
	feedback := &InterestFeedback{EvidenceByUnit: map[string]map[string]bool{},
		SessionsByUnit: map[string]map[string]bool{}}
	feedback.NegativePreferences = []map[string]any{
		{"reason": "explicit_not_interested", "context_scope": GlobalNewChatScope,
			"project_id": nil, "record_id": "rec-1",
			"news_source_url": SearchPageURL("blocked-word")},
	}
	// 无 pull → hot_list_unavailable
	if plan, reason := pipeline.HotNewsLead(snapshot, nil, feedback,
		newRecordingInvoker()); plan != nil || reason != "hot_list_unavailable" {
		t.Fatalf("no pull: %v %v", plan, reason)
	}
	// 全部被否定的词条 → hot_list_unavailable
	pull := &HotListPull{Entries: []*HotListEntry{
		{EntryID: "h1", Word: "blocked-word", Summary: "s"},
	}}
	if plan, reason := pipeline.HotNewsLead(snapshot, pull, feedback,
		newRecordingInvoker()); plan != nil || reason != "hot_list_unavailable" {
		t.Fatalf("all blocked: %v %v", plan, reason)
	}
}
