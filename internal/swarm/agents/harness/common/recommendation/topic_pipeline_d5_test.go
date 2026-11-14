package recommendation

// topic_pipeline_d5_test.go 对齐 topic_pipeline.py:5396-5942 契约的单元用例。

import (
	"context"
	"fmt"
	"math"
	"strings"
	"testing"
)

// fixedCounter 固定每字符 1 token 的计数器。
func fixedCounter() *StageTokenCounter {
	counter, _ := NewStageTokenCounter(func(text string) int { return len([]rune(text)) })
	return counter
}

// TestStageTokenCounter 对齐 topic_history.py:553-603 估算与预检。
func TestStageTokenCounter(t *testing.T) {
	counter := fixedCounter()
	// framed = 两侧框架化文本计数 + 3
	framed := counter.FramedTokens("sys", "user")
	expected := counter.CountText("<|start|>system\nsys<|end|>") +
		counter.CountText("<|start|>user\nuser<|end|>") + 3
	if framed != expected {
		t.Fatalf("framed: %d want %d", framed, expected)
	}
	estimate := counter.EstimateStageTokens("sys", "user", 100)
	expectedEstimate := int(math.Ceil(float64(framed)*1.25)) + 100
	if estimate != expectedEstimate {
		t.Fatalf("estimate: %d want %d", estimate, expectedEstimate)
	}
	limit := estimate + 10
	preflight := PreflightStage(counter, ContextLimitInfo{Value: &limit, Source: "s"},
		"sys", "user", 100)
	if preflight.Status != "fit" {
		t.Fatalf("fit: %v", preflight.Status)
	}
	limitSmall := estimate - 1
	preflight2 := PreflightStage(counter, ContextLimitInfo{Value: &limitSmall, Source: "s"},
		"sys", "user", 100)
	if preflight2.Status != "context_limit" {
		t.Fatalf("limit: %v", preflight2.Status)
	}
	// nil 计数器 → unverified
	preflight3 := PreflightStage(nil, ContextLimitInfo{Value: &limit, Source: "s"},
		"sys", "user", 100)
	if preflight3.Status != "context_limit_unverified" {
		t.Fatalf("unverified: %v", preflight3.Status)
	}
	// 未注入计数函数 → fail closed
	if _, err := NewStageTokenCounter(nil); err == nil {
		t.Fatalf("nil counter should fail")
	}
}

// TestSummaryStageBudget_FailClosed 对齐 :5396-5411。
func TestSummaryStageBudget_FailClosed(t *testing.T) {
	// 无上下文窗口
	_, _, err := SummaryStageBudget(&ArkBackgroundConfig{}, nil)
	if err == nil || err.(*TerminalGenerationRejection).Reason != "session_summary_context_limit_unverified" {
		t.Fatalf("no window: %v", err)
	}
	// 工厂失败
	config := &ArkBackgroundConfig{Model: "m", ContextWindowTokens: 1000}
	_, _, err2 := SummaryStageBudget(config, func(string) (*StageTokenCounter, error) {
		return nil, &ContextCountingUnverified{Reason: "x"}
	})
	if err2 == nil || err2.(*TerminalGenerationRejection).Reason != "session_summary_context_limit_unverified" {
		t.Fatalf("factory failed: %v", err2)
	}
	// 正常
	counter, limit, err3 := SummaryStageBudget(config,
		func(string) (*StageTokenCounter, error) { return fixedCounter(), nil })
	if err3 != nil || counter == nil || limit != 1000 {
		t.Fatalf("ok: %v %v %d", counter, err3, limit)
	}
}

// d5Snapshot 两会话快照（各一条自然消息 + 完结边界）。
func d5Snapshot(t *testing.T) *V6ContextSnapshot {
	t.Helper()
	snapshot := &V6ContextSnapshot{
		State:           map[string]any{},
		ContextScope:    GlobalNewChatScope,
		InputSnapshotID: "snap-d5",
	}
	for _, sessionID := range []string{"s1", "s2"} {
		boundary, err := NewSessionCompletionBoundary(sessionID,
			"evt-"+sessionID, "req-"+sessionID, 1, 100.0, "success")
		if err != nil {
			t.Fatalf("boundary: %v", err)
		}
		snapshot.CompletedSessionPrefixes = append(snapshot.CompletedSessionPrefixes,
			&CompletedSessionProjection{Boundary: boundary})
		snapshot.Messages = append(snapshot.Messages, &V6SourceMessage{
			EventID: "evt-" + sessionID, SessionID: sessionID, Ordinal: 0,
			Content: "问题", ObservedAt: 50.0, RequestID: "req-" + sessionID,
			InputOrigin: "manual_free_text", AssistanceOrigin: "none",
			IntentCluster: "tech-ai-ml", ActionType: "natural_question",
		})
	}
	snapshot.ShortIntentState = &ShortIntentState{}
	return snapshot
}

// TestSummarizeSessions_FullFlow 对齐 :5441-5689 缓存与生成两路。
func TestSummarizeSessions_FullFlow(t *testing.T) {
	store := newTestStore(t)
	pipeline := NewTopicPipeline(store, nil, nil, nil, nil, "", nil, nil, nil, nil, nil,
		func() float64 { return 200.0 })
	pipeline.StageTokenCounterFactory = func(string) (*StageTokenCounter, error) {
		return fixedCounter(), nil
	}
	snapshot := d5Snapshot(t)
	config := &ArkBackgroundConfig{Model: "m", ExpectedModel: "m-v1",
		ContextWindowTokens: 100000, Temperature: 0.3, TopP: 0.9, BaseURL: "https://ark"}

	calls := 0
	invoke := func(ctx context.Context, stage, systemPrompt, payload string) (any, error) {
		calls++
		if stage != "v6_session_summarizer" {
			t.Fatalf("stage: %s", stage)
		}
		// 按载荷里的 session_id 回摘要
		summaries := []any{}
		for _, sessionID := range []string{"s1", "s2"} {
			if strings.Contains(payload, `"session_id":"`+sessionID+`"`) {
				summaries = append(summaries, map[string]any{
					"session_id": sessionID, "summary": "摘要-" + sessionID,
				})
			}
		}
		return map[string]any{"summaries": summaries}, nil
	}

	batch, err := pipeline.SummarizeSessions(snapshot, config,
		&fakeTopicStageInvoker{invoke: invoke})
	if err != nil {
		t.Fatalf("summarize: %v", err)
	}
	if len(batch.artifacts) != 2 {
		t.Fatalf("artifacts: %d", len(batch.artifacts))
	}
	for _, artifact := range batch.artifacts {
		if !strings.HasPrefix(artifact.SummaryText, "摘要-") {
			t.Fatalf("summary: %q", artifact.SummaryText)
		}
	}
	if len(batch.generatedSummaryIDs) != 2 {
		t.Fatalf("generated: %v", batch.generatedSummaryIDs)
	}
	coverage := batch.coverageReceipt
	if coverage["schema_version"] != SessionSummaryCoverageReceiptVersion {
		t.Fatalf("coverage version: %v", coverage["schema_version"])
	}
	if sessions := pyListMaps(coverage["sessions"]); len(sessions) != 2 {
		t.Fatalf("coverage sessions: %v", coverage["sessions"])
	}

	// 先落库第一次产物（调用方职责，对齐 generate 先 persist 再续跑）
	if err := pipeline.PersistSessionSummaries(batch.artifacts); err != nil {
		t.Fatalf("persist: %v", err)
	}
	// 第二次：同快照 → 摘要缓存命中，不再调用模型
	callsBefore := calls
	batch2, err := pipeline.SummarizeSessions(snapshot, config,
		&fakeTopicStageInvoker{invoke: invoke})
	if err != nil {
		t.Fatalf("cached: %v", err)
	}
	if len(batch2.artifacts) != 2 || len(batch2.generatedSummaryIDs) != 0 {
		t.Fatalf("cached batch: %d %v", len(batch2.artifacts), batch2.generatedSummaryIDs)
	}
	if calls != callsBefore {
		t.Fatalf("cached run should not call model: %d → %d", callsBefore, calls)
	}
}

// TestRequestSessionSummaries_Validation 对齐 :5691-5734 五类答案过滤。
func TestRequestSessionSummaries_Validation(t *testing.T) {
	store := newTestStore(t)
	pipeline := NewTopicPipeline(store, nil, nil, nil, nil, "", nil, nil, nil, nil, nil, nil)
	snapshot := d5Snapshot(t)
	sessions := []map[string]any{
		{"session_id": "s1", "messages": []any{}},
		{"session_id": "s2", "messages": []any{}},
	}
	invoke := &fakeTopicStageInvoker{invoke: func(ctx context.Context, stage, systemPrompt, payload string) (any, error) {
		return map[string]any{"summaries": []any{
			map[string]any{"session_id": "s1", "summary": " 好 "}, // strip 后保留
			map[string]any{"session_id": "s1", "summary": "重复"},  // 重名剔除
			map[string]any{"session_id": "s9", "summary": "未知"},  // 非期望剔除
			map[string]any{"session_id": "s2", "summary": "  "},  // 空白剔除
			map[string]any{"session_id": "s2"},                   // 缺 summary 键剔除
		}}, nil
	}}
	answered, err := pipeline.RequestSessionSummaries(snapshot, sessions, invoke)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	// s1 命名两次 → 恰一次约束不满足 → 剔除
	if _, present := answered["s1"]; present {
		t.Fatalf("duplicate naming should drop: %v", answered)
	}
	if _, present := answered["s2"]; present {
		t.Fatalf("blank/missing summary should drop: %v", answered)
	}
}

// TestPersistSessionSummaryBatch_Conflict 对齐 :5885-5941 幂等与冲突。
func TestPersistSessionSummaryBatch_Conflict(t *testing.T) {
	store := newTestStore(t)
	pipeline := NewTopicPipeline(store, nil, nil, nil, nil, "", nil, nil, nil, nil, nil, nil)
	summary := summaryForTests(t, "summary:s1:aaa", "s1", 10.0)
	if err := pipeline.PersistSessionSummaries([]*SessionSummaryArtifact{summary}); err != nil {
		t.Fatalf("first persist: %v", err)
	}
	// 幂等：同一摘要再写
	if err := pipeline.PersistSessionSummaries([]*SessionSummaryArtifact{summary}); err != nil {
		t.Fatalf("idempotent: %v", err)
	}
	// 冲突：同 ID 不同内容
	mutated := summaryForTests(t, "summary:s1:aaa", "s1", 10.0)
	mutated.SummaryText = "另一个摘要"
	if err := pipeline.PersistSessionSummaries([]*SessionSummaryArtifact{mutated}); err == nil {
		t.Fatalf("conflict should fail")
	} else if rejection, ok := err.(*TerminalGenerationRejection); !ok ||
		rejection.Reason != "session_summary_persistence_conflict" {
		t.Fatalf("type: %v", err)
	}
	// 覆盖小票落库 + 幂等
	batch := &sessionSummaryBatch{
		artifacts: []*SessionSummaryArtifact{},
		coverageReceipt: map[string]any{
			"schema_version":         SessionSummaryCoverageReceiptVersion,
			"input_snapshot_id":      "snap-1",
			"context_scope":          GlobalNewChatScope,
			"summary_config_version": "cfg-v1",
			"sessions":               []any{},
		},
		summaryConfigVersion: "cfg-v1",
	}
	if err := pipeline.PersistSessionSummaryBatch(batch); err != nil {
		t.Fatalf("coverage: %v", err)
	}
	if err := pipeline.PersistSessionSummaryBatch(batch); err != nil {
		t.Fatalf("coverage idempotent: %v", err)
	}
}

// TestSummaryLags 对齐 :2991-2996。
func TestSummaryLags(t *testing.T) {
	if !summaryLags(nil, 10.0, map[string]bool{"a": true}) {
		t.Fatalf("no summary lags")
	}
	if !summaryLags(map[string]any{"completed_at": 5.0, "source_event_ids": []any{"a"}},
		10.0, map[string]bool{"a": true}) {
		t.Fatalf("older boundary lags")
	}
	if summaryLags(map[string]any{"completed_at": 20.0, "source_event_ids": []any{"a"}},
		10.0, map[string]bool{"a": true}) {
		t.Fatalf("covered turns fresh")
	}
	if !summaryLags(map[string]any{"completed_at": 20.0, "source_event_ids": []any{"b"}},
		10.0, map[string]bool{"a": true}) {
		t.Fatalf("missing turn lags")
	}
	// 传入的 turnIDs 不得被改写
	turns := map[string]bool{"a": true}
	_ = summaryLags(map[string]any{"completed_at": 20.0, "source_event_ids": []any{"a"}}, 10.0, turns)
	if !turns["a"] {
		t.Fatalf("turnIDs mutated")
	}
	_ = fmt.Sprint("")
}

// fakeTopicStageInvoker 测试用阶段调用器。
type fakeTopicStageInvoker struct {
	invoke func(ctx context.Context, stage, systemPrompt, payload string) (any, error)
}

func (f *fakeTopicStageInvoker) Invoke(ctx context.Context, stage, systemPrompt, payload string) (any, error) {
	return f.invoke(ctx, stage, systemPrompt, payload)
}

func (f *fakeTopicStageInvoker) RecordValidation(response any, outcome string) {}

func (f *fakeTopicStageInvoker) InvocationReceipts() []map[string]any { return nil }

func (f *fakeTopicStageInvoker) AppendInvocationReceipt(receipt map[string]any) {}
