package recommendation

// topic_pipeline_d10_test.go 对齐 topic_pipeline.py:8562-8870 契约的单元用例。

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
)

// jsonUnmarshalForTest JSON 解析。
func jsonUnmarshalForTest(payload string) (map[string]any, error) {
	var decoded map[string]any
	if err := json.Unmarshal([]byte(payload), &decoded); err != nil {
		return nil, err
	}
	return decoded, nil
}

// d10Plan 一张已接地计划。
func d10Plan(candidateID string, score float64) map[string]any {
	return map[string]any{
		"candidate_id": candidateID, "category": "tech",
		"context_scope": GlobalNewChatScope, "interest_summary_id": nil,
		"intent": "想学", "title": "标题-" + candidateID, "prompt": "提示",
		"freshness_class": "recent_development",
		"evidence_bundle": map[string]any{
			"fetch_status": "success", "source_url": "https://e", "source_kind": "stable",
			"document_revision_id": "doc-1",
			"published_at":         "2026-11-13T00:00:00+00:00",
			"content_boundary": map[string]any{
				"text": "证据", "start": 0, "end": 2},
		},
		"previous_questions": []any{}, "dismissed_questions": []any{},
	}
}

// TestSelectionCandidatesFor 对齐 :397-437。
func TestSelectionCandidatesFor(t *testing.T) {
	feedback := &InterestFeedback{EvidenceByUnit: map[string]map[string]bool{},
		SessionsByUnit: map[string]map[string]bool{}}
	newsPlan := d10Plan("news-1", 1)
	newsPlan["news_match"] = "matched"
	plans := []map[string]any{d10Plan("cand-1", 1), newsPlan}
	candidates := SelectionCandidatesFor(plans, GlobalNewChatScope, feedback, newsPlan)
	if len(candidates) != 2 {
		t.Fatalf("candidates: %d", len(candidates))
	}
	if candidates[0].StableRank != 1 || candidates[1].StableRank != 2 {
		t.Fatalf("ranks: %d %d", candidates[0].StableRank, candidates[1].StableRank)
	}
	if candidates[1].ScoringFeatures["news_item"] != true {
		t.Fatalf("news marker: %v", candidates[1].ScoringFeatures)
	}
	if candidates[0].ScoringFeatures["evidence_text"] != "证据" {
		t.Fatalf("evidence text: %v", candidates[0].ScoringFeatures)
	}
}

// TestUtilityInvoker_Batching 对齐 :8613-8641 批切分与分数聚合。
func TestUtilityInvoker_Batching(t *testing.T) {
	store := newTestStore(t)
	pipeline := NewTopicPipeline(store, nil, nil, nil, nil, "", nil, nil, nil, nil, nil,
		func() float64 { return 1000.0 })
	plans := []map[string]any{
		d10Plan("c-1", 1), d10Plan("c-2", 1), d10Plan("c-3", 1), d10Plan("c-4", 1),
	}
	var calls atomic.Int64
	invoke := &fakeTopicStageInvoker{invoke: func(ctx context.Context, stage,
		systemPrompt, payload string) (any, error) {
		calls.Add(1)
		ids := []any{}
		for _, raw := range pyListMaps(payloadMapOrEmpty(parseJSONForTest(payload))["candidates"]) {
			ids = append(ids, raw["candidate_id"])
		}
		scores := []any{}
		for _, id := range ids {
			scores = append(scores, map[string]any{
				"candidate_id": id, "question_focus": "问题",
				"assertion_spans":      []any{},
				"given_conditions":     []any{},
				"click_score_nullable": 0.8, "depth_score_nullable": 0.6,
				"combined_utility_nullable": 0.7, "admitted": true, "reason": "ok"})
		}
		return map[string]any{"scores": scores}, nil
	}}
	invoker := pipeline.UtilityInvoker(plans, invoke)
	payload := map[string]any{
		"candidates": []any{
			map[string]any{"candidate_id": "c-1", "scoring_features": map[string]any{}},
			map[string]any{"candidate_id": "c-2", "scoring_features": map[string]any{}},
			map[string]any{"candidate_id": "c-3", "scoring_features": map[string]any{}},
			map[string]any{"candidate_id": "c-4", "scoring_features": map[string]any{}},
		},
		"request_id": "req-u",
	}
	result, err := invoker(context.Background(), payload)
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	// 4 候选 / 批 3 → 2 批
	if calls.Load() != 2 {
		t.Fatalf("calls: %d", calls.Load())
	}
	scores := pyListMaps(payloadMapOrEmpty(result)["scores"])
	if len(scores) != 4 {
		t.Fatalf("scores: %d", len(scores))
	}
}

// TestScoreAndSelect_NoRegular 对齐 :8751-8763 无常规候选。
func TestScoreAndSelect_NoRegular(t *testing.T) {
	store := newTestStore(t)
	pipeline := NewTopicPipeline(store, nil, nil, nil, nil, "", nil, nil, nil, nil, nil,
		func() float64 { return 1000.0 })
	pipeline.StageTokenCounterFactory = func(string) (*StageTokenCounter, error) {
		return fixedCounter(), nil
	}
	snapshot := &V6ContextSnapshot{ContextScope: GlobalNewChatScope,
		InputSnapshotID: "snap-10", ProfileProjection: map[string]any{}}
	feedback := &InterestFeedback{EvidenceByUnit: map[string]map[string]bool{},
		SessionsByUnit: map[string]map[string]bool{}}
	newsPlan := d10Plan("news-1", 1)
	newsPlan["news_match"] = "matched"
	plans := []map[string]any{newsPlan}
	config := &ArkBackgroundConfig{Model: "m", ContextWindowTokens: 100000,
		Temperature: 0.3, TopP: 0.9, BaseURL: "https://ark"}
	invoke := &fakeTopicStageInvoker{invoke: func(ctx context.Context, stage,
		systemPrompt, payload string) (any, error) {
		if stage == "v6_utility_scorer" {
			parsed := payloadMapOrEmpty(parseJSONForTest(payload))
			scores := []any{}
			for _, raw := range pyListMaps(parsed["candidates"]) {
				scores = append(scores, map[string]any{
					"candidate_id": raw["candidate_id"], "question_focus": "问题",
					"assertion_spans": []any{}, "given_conditions": []any{},
					"click_score_nullable": 0.8, "depth_score_nullable": 0.6,
					"combined_utility_nullable": 0.7, "admitted": true, "reason": "ok"})
			}
			return map[string]any{"scores": scores}, nil
		}
		return map[string]any{"pairs": []any{}}, nil
	}}
	result, newsLeadID, newsRejection, err := pipeline.ScoreAndSelect(plans, snapshot,
		feedback, config, invoke, nil, nil)
	if err != nil {
		t.Fatalf("score: %v", err)
	}
	if newsLeadID != "news-1" || newsRejection != "" {
		t.Fatalf("lead: %s %s", newsLeadID, newsRejection)
	}
	if result == nil || len(result.PreferredList) != 0 {
		t.Fatalf("news-only result: %+v", result)
	}
	// 质量决策已落
	rows, _ := store.QueryEvents(QueryEventsOptions{
		EventType: "quality_decision", Limit: 5, NewestFirst: true})
	if len(rows) != 1 {
		t.Fatalf("quality decisions: %d", len(rows))
	}
}

// TestLatestScopeManifestID 对齐 :8858-8870。
func TestLatestScopeManifestID(t *testing.T) {
	store := newTestStore(t)
	pipeline := NewTopicPipeline(store, nil, nil, nil, nil, "", nil, nil, nil, nil, nil,
		func() float64 { return 1000.0 })
	if id := pipeline.LatestScopeManifestID(GlobalNewChatScope); id != "" {
		t.Fatalf("empty scope: %s", id)
	}
	if ok, _ := store.RecordEvent(map[string]any{
		"session_id": "s1", "slate_id": "s1", "event_type": "starter_slate_manifest",
		"topic_id": "m-late", "policy_id": POLICY_ID,
		"payload": map[string]any{"manifest_id": "m-late",
			"context_scope": GlobalNewChatScope},
		"created_at": 20.0,
	}); !ok {
		t.Fatalf("record")
	}
	if id := pipeline.LatestScopeManifestID(GlobalNewChatScope); id != "m-late" {
		t.Fatalf("latest: %s", id)
	}
}

// parseJSONForTest 测试用 JSON 解析。
func parseJSONForTest(payload string) map[string]any {
	decoded, err := jsonUnmarshalForTest(payload)
	if err != nil {
		return map[string]any{}
	}
	return decoded
}
