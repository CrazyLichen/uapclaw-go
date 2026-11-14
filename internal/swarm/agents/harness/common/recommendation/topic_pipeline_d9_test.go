package recommendation

// topic_pipeline_d9_test.go 对齐 topic_pipeline.py:7714-8560 契约的单元用例。

import (
	"context"
	"testing"
)

// d9HostEvidencePlan 项目续聊计划（宿主证据直通）。
func d9HostEvidencePlan(candidateID string) map[string]any {
	plan := ProjectContinuationPlan(candidateID, "int-a", "跟进", "续聊标题",
		&V6SourceMessage{EventID: "evt-1", SessionID: "s1", Ordinal: 0,
			Content: "项目问题", ProjectID: "p1"},
		&V6ContextSnapshot{ContextScope: "project:p1", ProjectID: "p1",
			InputSnapshotID: "snap-9"},
		"想深入")
	return plan
}

// TestGroundPlans_HostEvidence 对齐 :8167-8202 宿主证据直通。
func TestGroundPlans_HostEvidence(t *testing.T) {
	store := newTestStore(t)
	pipeline := NewTopicPipeline(store, nil, nil, nil, nil, "", nil, nil, nil, nil, nil,
		func() float64 { return 1000.0 })
	pipeline.StageTokenCounterFactory = func(string) (*StageTokenCounter, error) {
		return fixedCounter(), nil
	}
	audit, _ := NewExternalInvocationAudit(store, POLICY_ID, "snap-9",
		"project:p1", "visit-1")
	lanes := NewOptionalLanes()
	lanes.MandatoryReady()
	plan := d9HostEvidencePlan("cand-1")
	batch, err := pipeline.GroundPlans([]map[string]any{plan},
		&V6ContextSnapshot{ContextScope: "project:p1", ProjectID: "p1",
			InputSnapshotID: "snap-9"},
		map[string]any{"local_date": "2026-11-13",
			"document_snapshot_version": "doc-v1"},
		&ArkBackgroundConfig{Model: "m", ContextWindowTokens: 100000},
		&fakeTopicStageInvoker{invoke: func(ctx context.Context, stage,
			systemPrompt, payload string) (any, error) {
			return map[string]any{}, nil
		}}, audit, nil, 1, lanes, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("ground: %v", err)
	}
	if len(batch.plans) != 1 {
		t.Fatalf("plans: %d", len(batch.plans))
	}
	decisions := pyListMaps(batch.decisionReceipt["candidate_decisions"])
	if len(decisions) != 1 || decisions[0]["decision"] != "admit" ||
		decisions[0]["terminal_stage"] != "project_history" {
		t.Fatalf("decision: %v", decisions)
	}
	if batch.decisionReceipt["decision"] != "admit" {
		t.Fatalf("aggregate: %v", batch.decisionReceipt["decision"])
	}
}

// TestGroundPlans_HostEvidenceCopyReject 对齐 :8175-8186 文案拒绝。
func TestGroundPlans_HostEvidenceCopyReject(t *testing.T) {
	store := newTestStore(t)
	pipeline := NewTopicPipeline(store, nil, nil, nil, nil, "", nil, nil, nil, nil, nil,
		func() float64 { return 1000.0 })
	pipeline.StageTokenCounterFactory = func(string) (*StageTokenCounter, error) {
		return fixedCounter(), nil
	}
	audit, _ := NewExternalInvocationAudit(store, POLICY_ID, "snap-9",
		"project:p1", "visit-1")
	lanes := NewOptionalLanes()
	lanes.MandatoryReady()
	plan := d9HostEvidencePlan("cand-1")
	plan["title"] = "**标记**非法标题"
	batch, err := pipeline.GroundPlans([]map[string]any{plan},
		&V6ContextSnapshot{ContextScope: "project:p1", ProjectID: "p1",
			InputSnapshotID: "snap-9"},
		map[string]any{"local_date": "2026-11-13",
			"document_snapshot_version": "doc-v1"},
		&ArkBackgroundConfig{Model: "m", ContextWindowTokens: 100000},
		&fakeTopicStageInvoker{invoke: func(ctx context.Context, stage,
			systemPrompt, payload string) (any, error) {
			return map[string]any{}, nil
		}}, audit, nil, 1, lanes, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("ground: %v", err)
	}
	if len(batch.plans) != 0 {
		t.Fatalf("plans: %d", len(batch.plans))
	}
	decisions := pyListMaps(batch.decisionReceipt["candidate_decisions"])
	if len(decisions) != 1 || decisions[0]["decision"] != "reject" ||
		decisions[0]["reason"] != "project_card_copy_invalid" {
		t.Fatalf("decision: %v", decisions)
	}
	if batch.decisionReceipt["decision"] != "abstain" {
		t.Fatalf("aggregate: %v", batch.decisionReceipt["decision"])
	}
}

// TestGroundPlans_WebSearchUnavailable 对齐 :7731-7732 搜索不可用。
func TestGroundPlans_WebSearchUnavailable(t *testing.T) {
	store := newTestStore(t)
	pipeline := NewTopicPipeline(store, nil, nil, nil, nil, "", nil, nil, nil, nil, nil,
		func() float64 { return 1000.0 })
	pipeline.StageTokenCounterFactory = func(string) (*StageTokenCounter, error) {
		return fixedCounter(), nil
	}
	audit, _ := NewExternalInvocationAudit(store, POLICY_ID, "snap-9",
		GlobalNewChatScope, "visit-1")
	lanes := NewOptionalLanes()
	lanes.MandatoryReady()
	// 搜索提供方未配置（默认 _unconfigured_v6_search）→ 搜索失败记录拒绝
	plan := map[string]any{
		"candidate_id": "cand-1", "title": "标题", "prompt": "提示",
		"primary_query": "查询", "category": "tech", "freshness_class": "recent_development",
	}
	batch, groundErr := pipeline.GroundPlans([]map[string]any{plan},
		&V6ContextSnapshot{ContextScope: GlobalNewChatScope,
			InputSnapshotID: "snap-9"},
		map[string]any{"local_date": "2026-11-13",
			"document_snapshot_version": "doc-v1"},
		&ArkBackgroundConfig{Model: "m", ContextWindowTokens: 100000},
		&fakeTopicStageInvoker{invoke: func(ctx context.Context, stage,
			systemPrompt, payload string) (any, error) {
			return map[string]any{}, nil
		}}, audit, nil, 1, lanes, nil, nil, nil, nil)
	if groundErr != nil {
		t.Fatalf("ground: %v", groundErr)
	}
	decisions := pyListMaps(batch.decisionReceipt["candidate_decisions"])
	if len(decisions) != 1 || decisions[0]["decision"] != "reject" ||
		decisions[0]["reason"] != "provider_invocation_failed" {
		t.Fatalf("decision: %v", decisions)
	}
}

// TestGroundPlans_WebNoResults 对齐 :7594-7596/7973-7980 无合格结果。
func TestGroundPlans_WebNoResults(t *testing.T) {
	store := newTestStore(t)
	pipeline := NewTopicPipeline(store, nil, nil, nil, nil, "", nil, nil, nil, nil, nil,
		func() float64 { return 1000.0 })
	pipeline.StageTokenCounterFactory = func(string) (*StageTokenCounter, error) {
		return fixedCounter(), nil
	}
	pipeline.searchOverride = func(ctx context.Context, query string,
		maxResults, timeoutSeconds int) (string, []map[string]string, error) {
		return "bing", nil, nil
	}
	audit, _ := NewExternalInvocationAudit(store, POLICY_ID, "snap-9",
		GlobalNewChatScope, "visit-1")
	lanes := NewOptionalLanes()
	lanes.MandatoryReady()
	plan := map[string]any{
		"candidate_id": "cand-1", "title": "标题", "prompt": "提示",
		"primary_query": "公开查询", "category": "tech",
		"freshness_class": "recent_development",
	}
	batch, err := pipeline.GroundPlans([]map[string]any{plan},
		&V6ContextSnapshot{ContextScope: GlobalNewChatScope,
			InputSnapshotID: "snap-9"},
		map[string]any{"local_date": "2026-11-13",
			"document_snapshot_version": "doc-v1"},
		&ArkBackgroundConfig{Model: "m", ContextWindowTokens: 100000},
		&fakeTopicStageInvoker{invoke: func(ctx context.Context, stage,
			systemPrompt, payload string) (any, error) {
			return map[string]any{}, nil
		}}, audit, pipeline.searchOverride, 1, lanes, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("ground: %v", err)
	}
	decisions := pyListMaps(batch.decisionReceipt["candidate_decisions"])
	if len(decisions) != 1 || decisions[0]["decision"] != "reject" ||
		decisions[0]["terminal_stage"] != "admission" ||
		decisions[0]["reason"] != "no_eligible_search_result" {
		t.Fatalf("decision: %v", decisions)
	}
}
