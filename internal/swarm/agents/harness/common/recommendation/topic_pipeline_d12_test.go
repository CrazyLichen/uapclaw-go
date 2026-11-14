package recommendation

// topic_pipeline_d12_test.go 对齐 topic_pipeline.py:9169-9704 契约的单元用例。

import (
	"context"
	"testing"
)

// TestGenerateFromSummaries_RIDAbstained 对齐 :9351-9428 放弃清单。
func TestGenerateFromSummaries_RIDAbstained(t *testing.T) {
	store := newTestStore(t)
	pipeline := NewTopicPipeline(store, nil, nil, nil, nil, "", nil, nil, nil, nil, nil,
		func() float64 { return 1000.0 })
	pipeline.StageTokenCounterFactory = func(string) (*StageTokenCounter, error) {
		return fixedCounter(), nil
	}
	snapshot := &V6ContextSnapshot{
		ContextScope: GlobalNewChatScope, InputSnapshotID: "snap-12",
		ProfileProjection: map[string]any{}, ShortIntentState: &ShortIntentState{},
	}
	feedback := &InterestFeedback{EvidenceByUnit: map[string]map[string]bool{},
		SessionsByUnit: map[string]map[string]bool{}}
	config := &ArkBackgroundConfig{Model: "m", ExpectedModel: "m-v1",
		ContextWindowTokens: 100000, Temperature: 0.3, TopP: 0.9, BaseURL: "https://ark"}
	invoke := &fakeTopicStageInvoker{invoke: func(ctx context.Context, stage,
		systemPrompt, payload string) (any, error) {
		return map[string]any{"scores": []any{}}, nil
	}}
	audit, _ := NewExternalInvocationAudit(store, POLICY_ID, "snap-12",
		GlobalNewChatScope, "visit-1")
	summaryBatch, err := pipeline.SummarizeSessions(snapshot, config, invoke)
	if err != nil {
		t.Fatalf("summaries: %v", err)
	}
	invocation := &generationInvocation{
		earlyEvidence: map[string]*candidateEvidenceFuture{},
		fetchTasks:    NewFetchTasks(),
		newsDone:      make(chan newsOutcome, 1),
	}
	close(invocation.newsDone)
	triggerKey := map[string]any{"local_date": "2026-11-13",
		"document_snapshot_version": "doc-v1"}
	result, err := pipeline.GenerateFromSummaries(snapshot, feedback, triggerKey,
		"scheduled_cron", config, invoke, audit, nil, summaryBatch, nil, invocation)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if result.FailureReason != "rid_abstained" {
		t.Fatalf("failure reason: %s", result.FailureReason)
	}
	if len(result.Cards) != 0 {
		t.Fatalf("cards: %d", len(result.Cards))
	}
	abstention := payloadMapOrEmpty(result.Manifest["rid_abstention_receipt"])
	if abstention["schema_version"] != RIDAbstentionReceiptVersion ||
		abstention["trigger_origin"] != "scheduled_cron" {
		t.Fatalf("abstention: %v", abstention)
	}
	if _, hasRID := result.Manifest["component_receipts"].(map[string]any)[RID_COMPONENT_ID]; !hasRID {
		t.Fatalf("rid receipt: %v", result.Manifest["component_receipts"])
	}
}

// TestGeneratePrepared_AbstainedPath 对齐 :9169-9225 主入口与取消。
func TestGeneratePrepared_AbstainedPath(t *testing.T) {
	store := newTestStore(t)
	pipeline := NewTopicPipeline(store, nil, nil, nil, nil, "", nil, nil, nil, nil, nil,
		func() float64 { return 1000.0 })
	pipeline.StageTokenCounterFactory = func(string) (*StageTokenCounter, error) {
		return fixedCounter(), nil
	}
	pipeline.hotList = nil
	snapshot := &V6ContextSnapshot{
		ContextScope: GlobalNewChatScope, InputSnapshotID: "snap-12",
		ProfileProjection: map[string]any{}, ShortIntentState: &ShortIntentState{},
	}
	feedback := &InterestFeedback{EvidenceByUnit: map[string]map[string]bool{},
		SessionsByUnit: map[string]map[string]bool{}}
	config := &ArkBackgroundConfig{Model: "m", ExpectedModel: "m-v1",
		ContextWindowTokens: 100000, Temperature: 0.3, TopP: 0.9, BaseURL: "https://ark"}
	invoke := &fakeTopicStageInvoker{invoke: func(ctx context.Context, stage,
		systemPrompt, payload string) (any, error) {
		return map[string]any{"scores": []any{}}, nil
	}}
	audit, _ := NewExternalInvocationAudit(store, POLICY_ID, "snap-12",
		GlobalNewChatScope, "visit-1")
	result, err := pipeline.GeneratePrepared(snapshot, feedback,
		map[string]any{"local_date": "2026-11-13",
			"document_snapshot_version": "doc-v1"},
		"scheduled_cron", config, invoke, audit, nil, nil)
	if err != nil {
		t.Fatalf("prepared: %v", err)
	}
	if result == nil || result.FailureReason != "rid_abstained" {
		t.Fatalf("result: %+v", result)
	}
}
