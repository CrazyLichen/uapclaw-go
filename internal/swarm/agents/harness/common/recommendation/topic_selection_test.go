// Package recommendation topic_selection 选择层单元测试。
//
// 对应 Python 代码：jiuwenswarm/agents/harness/common/recommendation/topic_selection.py
package recommendation

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// selectionTestBinding 构造指定组件的合法绑定与成功小票。
func selectionTestBinding(t *testing.T, componentID string) (*LearnedComponentBinding, *LearnedComponentReceipt) {
	t.Helper()
	binding, err := NewLearnedComponentBinding(componentID, "provider-1", "v1", "cfg-v1")
	require.NoError(t, err)
	receipt, err := NewLearnedComponentReceipt(binding, "snap-1", "req-1", "uncalibrated_api_proxy", "success", "")
	require.NoError(t, err)
	return binding, receipt
}

// selectionTestCandidate 构造全局合法候选（必过硬过滤）。
func selectionTestCandidate(t *testing.T, id string, rank int, category string) *SelectionCandidate {
	t.Helper()
	c, err := NewSelectionCandidate(id, "global_new_chat", category, rank, true, true, false,
		"i-"+id, "anchor-"+id, []string{"mu-1"}, map[string]any{"k": "v"})
	require.NoError(t, err)
	return c
}

// selectionTestUtility 全部准入、分数 0.8 的效用结果。
func selectionTestUtility(t *testing.T, receipt *LearnedComponentReceipt, ids []string) *UtilityScoringResult {
	t.Helper()
	scoreValue := 0.8
	scores := make([]UtilityScore, 0, len(ids))
	for _, id := range ids {
		scores = append(scores, UtilityScore{
			CandidateID: id, Admitted: true, Reason: "ok",
			ClickScoreNullable: &scoreValue, DepthScoreNullable: &scoreValue,
			CombinedUtilityNullable: &scoreValue,
			GivenConditions:         []map[string]string{},
		})
	}
	return &UtilityScoringResult{Scores: scores, ScorerVersion: receipt.Binding.ConfigVersion, Receipt: receipt}
}

// selectionTestDiversity 三候选全兼容、分数 0.6 的多样性结果。
func selectionTestDiversity(t *testing.T, receipt *LearnedComponentReceipt, ids []string) *DiversityScoringResult {
	t.Helper()
	divValue := 0.6
	pairwise := []PairwiseDiversityScore{}
	for _, pair := range combinations2(ids) {
		pairwise = append(pairwise, PairwiseDiversityScore{
			LeftCandidateID: pair[0], RightCandidateID: pair[1],
			DiversityScoreNullable: &divValue, Compatible: true, Reason: "ok",
		})
	}
	marginal := []MarginalDiversityScore{}
	for _, id := range ids {
		value := divValue
		marginal = append(marginal, MarginalDiversityScore{CandidateID: id, MarginalDiversityNullable: &value})
	}
	return &DiversityScoringResult{PairwiseScores: pairwise, MarginalScores: marginal,
		ScorerVersion: receipt.Binding.ConfigVersion, Receipt: receipt}
}

func TestSelectionCandidateValidation(t *testing.T) {
	_, err := NewSelectionCandidate("", "global_new_chat", "cat", 1, true, true, false, "", "", nil, nil)
	assert.EqualError(t, err, "candidate identity, scope, and category are required")
	_, err = NewSelectionCandidate("c", "global_new_chat", "cat", 0, true, true, false, "", "", nil, nil)
	assert.EqualError(t, err, "stable_rank must be a positive integer")
	c, err := NewSelectionCandidate("c", "global_new_chat", "cat", 1, true, true, false,
		"i1", "a1", []string{"m"}, nil)
	require.NoError(t, err)
	// scoring_features nil → 空 dict（对齐 :112 dict 拷贝）
	assert.NotNil(t, c.ScoringFeatures)
	payload := c.AsScoringPayload()
	// nil features → 空 dict（对齐 :112）
	assert.Equal(t, map[string]any{}, payload["scoring_features"])
	// 非 nil features 原样透传
	c2, err := NewSelectionCandidate("c2", "global_new_chat", "cat", 1, true, true, false,
		"i1", "a1", nil, map[string]any{"k": "v"})
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"k": "v"}, c2.AsScoringPayload()["scoring_features"])
}

func TestListSearchConstraintsValidation(t *testing.T) {
	// 非法 scope
	_, err := NewListSearchConstraints("bogus", nil, 0, 64, 4096, 64, 1, 1)
	assert.EqualError(t, err, "invalid context_scope")
	// 必需兴趣非全局
	_, err = NewListSearchConstraints("project:p1", []string{"i1"}, 0, 64, 4096, 64, 1, 1)
	assert.EqualError(t, err, "required interests must be a global ID tuple")
	// 项目列表带 leading card
	_, err = NewListSearchConstraints("project:p1", nil, 1, 64, 4096, 64, 1, 1)
	assert.EqualError(t, err, "only a global list has a leading card, and at most one")
	// 全局 leading=2
	_, err = NewListSearchConstraints(globalScope, nil, 2, 64, 4096, 64, 1, 1)
	assert.EqualError(t, err, "only a global list has a leading card, and at most one")
	// 非正界
	_, err = NewListSearchConstraints(globalScope, nil, 0, 0, 4096, 64, 1, 1)
	assert.EqualError(t, err, "search bounds must be positive integers")
	// 负权重
	_, err = NewListSearchConstraints(globalScope, nil, 0, 64, 4096, 64, -1, 1)
	assert.EqualError(t, err, "search weights must be non-negative")
	// 合法：全局
	c, err := NewListSearchConstraints(globalScope, []string{"i1"}, 1, 64, 4096, 64, 1, 1)
	require.NoError(t, err)
	assert.Equal(t, map[string]int{"profile_recent_progress": 3, "adjacent_explanation": 2,
		"broad_timely_practical": 2, "profile_interest_continuation": 7}, c.Quotas())
	assert.Equal(t, 6, c.TargetSize())
	assert.Equal(t, 3, c.FirstWindowSize())
	// 合法：项目
	pc, err := NewListSearchConstraints("project:p1", nil, 0, 64, 4096, 64, 1, 1)
	require.NoError(t, err)
	assert.Equal(t, map[string]int{"project_continuation": 3, "project_domain_progress": 1}, pc.Quotas())
	assert.Equal(t, 4, pc.TargetSize())
	assert.Equal(t, 4, pc.FirstWindowSize())
}

func TestScoreInteractionUtility(t *testing.T) {
	binding, receipt := selectionTestBinding(t, UTILITY_COMPONENT_ID)
	_ = binding
	candidates := []*SelectionCandidate{
		selectionTestCandidate(t, "c1", 1, "profile_interest_continuation"),
		selectionTestCandidate(t, "c2", 2, "adjacent_explanation"),
	}
	scorer := func(ctx context.Context, payload map[string]any) (any, error) {
		assert.Equal(t, "snap-1", payload["input_snapshot_id"])
		return map[string]any{"scores": []any{
			map[string]any{"candidate_id": "c1", "question_focus": "q", "assertion_spans": []any{},
				"given_conditions": []any{}, "click_score_nullable": 0.5, "depth_score_nullable": nil,
				"combined_utility_nullable": 0.7, "admitted": true, "reason": "ok"},
			map[string]any{"candidate_id": "c2", "question_focus": "q", "assertion_spans": []any{},
				"given_conditions": []any{}, "click_score_nullable": nil, "depth_score_nullable": nil,
				"combined_utility_nullable": 0.2, "admitted": false, "reason": "low"},
		}}, nil
	}
	result, err := ScoreInteractionUtility(map[string]any{"u": 1}, candidates, ScoreInvoker(scorer), receipt)
	require.NoError(t, err)
	require.Len(t, result.Scores, 2)
	assert.Equal(t, "cfg-v1", result.ScorerVersion)
	assert.True(t, result.Scores[0].Admitted)
	assert.Nil(t, result.Scores[0].DepthScoreNullable)
	assert.Equal(t, 0.7, *result.Scores[0].CombinedUtilityNullable)
	assert.False(t, result.Scores[1].Admitted)
	// 候选 ID 重复
	_, err = ScoreInteractionUtility(nil, []*SelectionCandidate{candidates[0], candidates[0]}, ScoreInvoker(scorer), receipt)
	assert.EqualError(t, err, "candidate IDs must be unique")
	// 组件不匹配
	_, diversityReceipt := selectionTestBinding(t, DIVERSITY_COMPONENT_ID)
	_, err = ScoreInteractionUtility(nil, candidates, ScoreInvoker(scorer), diversityReceipt)
	assert.EqualError(t, err, "receipt component_id does not match scorer")
	// 失败小票
	failedReceipt, err := NewLearnedComponentReceipt(receipt.Binding, "snap-1", "req-1", "not_scored", "failed", "boom")
	require.NoError(t, err)
	_, err = ScoreInteractionUtility(nil, candidates, ScoreInvoker(scorer), failedReceipt)
	assert.EqualError(t, err, "scoring receipt must be successful")
	// 响应 schema 错误
	badScorer := func(ctx context.Context, payload map[string]any) (any, error) {
		return map[string]any{"wrong": 1}, nil
	}
	_, err = ScoreInteractionUtility(nil, candidates, ScoreInvoker(badScorer), receipt)
	assert.EqualError(t, err, "utility scorer output schema invalid")
	// 覆盖缺失
	partialScorer := func(ctx context.Context, payload map[string]any) (any, error) {
		return map[string]any{"scores": []any{
			map[string]any{"candidate_id": "c1", "question_focus": "q", "assertion_spans": []any{},
				"given_conditions": []any{}, "click_score_nullable": nil, "depth_score_nullable": nil,
				"combined_utility_nullable": 0.7, "admitted": true, "reason": "ok"},
		}}, nil
	}
	_, err = ScoreInteractionUtility(nil, candidates, ScoreInvoker(partialScorer), receipt)
	assert.EqualError(t, err, "utility scorer must cover every candidate exactly once")
	// given_conditions 非法 source
	badConditionScorer := func(ctx context.Context, payload map[string]any) (any, error) {
		return map[string]any{"scores": []any{
			map[string]any{"candidate_id": "c1", "question_focus": "q", "assertion_spans": []any{},
				"given_conditions": []any{map[string]any{"card_quote": "a", "condition": "b",
					"source": "bogus", "source_quote": "c"}},
				"click_score_nullable": nil, "depth_score_nullable": nil,
				"combined_utility_nullable": 0.7, "admitted": true, "reason": "ok"},
		}}, nil
	}
	_, err = ScoreInteractionUtility(nil, candidates, ScoreInvoker(badConditionScorer), receipt)
	assert.EqualError(t, err, "utility scorer output schema invalid")
}

func TestScoreDiversity(t *testing.T) {
	_, receipt := selectionTestBinding(t, DIVERSITY_COMPONENT_ID)
	ids := []string{"c1", "c2", "c3"}
	candidates := []*SelectionCandidate{}
	for i, id := range ids {
		candidates = append(candidates, selectionTestCandidate(t, id, i+1, "profile_interest_continuation"))
	}
	scorer := func(ctx context.Context, payload map[string]any) (any, error) {
		pairwise := []any{}
		for _, pair := range combinations2(ids) {
			pairwise = append(pairwise, map[string]any{
				"left_candidate_id": pair[0], "right_candidate_id": pair[1],
				"diversity_score_nullable": 0.6, "compatible": true, "reason": "ok"})
		}
		marginal := []any{}
		for _, id := range ids {
			marginal = append(marginal, map[string]any{"candidate_id": id, "marginal_diversity_nullable": 0.6})
		}
		return map[string]any{"pairwise_scores": pairwise, "marginal_scores": marginal}, nil
	}
	result, err := ScoreDiversity(candidates, ScoreInvoker(scorer), receipt)
	require.NoError(t, err)
	assert.Len(t, result.PairwiseScores, 3)
	assert.Len(t, result.MarginalScores, 3)
	// 成对键归一化（left>right 输入也归一）
	assert.Equal(t, "c1", result.PairwiseScores[0].LeftCandidateID)
	// 缺一个对 → 全覆盖失败
	shortScorer := func(ctx context.Context, payload map[string]any) (any, error) {
		return map[string]any{"pairwise_scores": []any{
			map[string]any{"left_candidate_id": "c1", "right_candidate_id": "c2",
				"diversity_score_nullable": 0.6, "compatible": true, "reason": "ok"},
		}, "marginal_scores": []any{}}, nil
	}
	_, err = ScoreDiversity(candidates, ScoreInvoker(shortScorer), receipt)
	assert.EqualError(t, err, "diversity scorer must cover every pair exactly once")
	// 自身成对
	selfScorer := func(ctx context.Context, payload map[string]any) (any, error) {
		return map[string]any{"pairwise_scores": []any{
			map[string]any{"left_candidate_id": "c1", "right_candidate_id": "c1",
				"diversity_score_nullable": 0.6, "compatible": true, "reason": "ok"},
		}, "marginal_scores": []any{}}, nil
	}
	_, err = ScoreDiversity(candidates, ScoreInvoker(selfScorer), receipt)
	assert.EqualError(t, err, "diversity scorer pair membership invalid")
}

func TestHybridListSearchGlobalSuccess(t *testing.T) {
	ids := []string{"c1", "c2", "c3", "c4"}
	candidates := []*SelectionCandidate{}
	for i, id := range ids {
		candidates = append(candidates, selectionTestCandidate(t, id, i+1, "profile_interest_continuation"))
	}
	_, utilityReceipt := selectionTestBinding(t, UTILITY_COMPONENT_ID)
	_, diversityReceipt := selectionTestBinding(t, DIVERSITY_COMPONENT_ID)
	utility := selectionTestUtility(t, utilityReceipt, ids)
	diversity := selectionTestDiversity(t, diversityReceipt, ids)
	constraints, err := NewListSearchConstraints(globalScope, nil, 0, 64, 4096, 64, 1, 1)
	require.NoError(t, err)
	result, err := HybridListSearch(candidates, utility, diversity, constraints)
	require.NoError(t, err)
	assert.Empty(t, result.FailureReason)
	// 全局目标 7 > 候选 4 → 全选
	assert.ElementsMatch(t, ids, result.PreferredList)
	for _, decision := range result.Decisions {
		assert.True(t, decision.Selected)
		assert.Equal(t, []string{"selected_by_host_list_search"}, decision.Reasons)
	}
	// 轨迹里首选行标 preferred
	foundPreferred := false
	for _, entry := range result.BoundedBeamTrace.Entries {
		if entry.Status == "preferred" {
			foundPreferred = true
			assert.Equal(t, ids, entry.CandidateIDs)
		}
	}
	assert.True(t, foundPreferred)
	assert.Equal(t, LIST_SEARCH_VERSION, result.BoundedBeamTrace.SearchVersion)
	metadata := result.AsManifestMetadata()
	assert.Contains(t, metadata["component_receipts"], UTILITY_COMPONENT_ID)
	assert.Contains(t, metadata["component_receipts"], DIVERSITY_COMPONENT_ID)
}

func TestHybridListSearchHardFilters(t *testing.T) {
	ids := []string{"c1", "c2"}
	candidates := []*SelectionCandidate{
		selectionTestCandidate(t, "c1", 1, "profile_interest_continuation"),
		selectionTestCandidate(t, "c2", 2, "profile_interest_continuation"),
	}
	// c2 scope 不匹配 + 证据不可用 → 硬过滤
	c2Broken, err := NewSelectionCandidate("c2", "project:p1", "profile_interest_continuation", 2,
		false, true, false, "i-c2", "anchor-c2", nil, nil)
	require.NoError(t, err)
	candidates[1] = c2Broken
	_, utilityReceipt := selectionTestBinding(t, UTILITY_COMPONENT_ID)
	_, diversityReceipt := selectionTestBinding(t, DIVERSITY_COMPONENT_ID)
	utility := selectionTestUtility(t, utilityReceipt, ids)
	diversity := selectionTestDiversity(t, diversityReceipt, ids)
	constraints, err := NewListSearchConstraints(globalScope, nil, 0, 64, 4096, 64, 1, 1)
	require.NoError(t, err)
	result, err := HybridListSearch(candidates, utility, diversity, constraints)
	require.NoError(t, err)
	assert.Equal(t, []string{"c1"}, result.PreferredList)
	// c2 判定：scope_mismatch + evidence_ineligible
	var c2Decision *CandidateHostDecision
	for i := range result.Decisions {
		if result.Decisions[i].CandidateID == "c2" {
			c2Decision = &result.Decisions[i]
		}
	}
	require.NotNil(t, c2Decision)
	assert.False(t, c2Decision.Selected)
	assert.Contains(t, c2Decision.Reasons, "scope_mismatch")
	assert.Contains(t, c2Decision.Reasons, "evidence_ineligible")
}

func TestHybridListSearchUtilityRejectedAndAbstained(t *testing.T) {
	ids := []string{"c1"}
	candidates := []*SelectionCandidate{selectionTestCandidate(t, "c1", 1, "profile_interest_continuation")}
	_, utilityReceipt := selectionTestBinding(t, UTILITY_COMPONENT_ID)
	_, diversityReceipt := selectionTestBinding(t, DIVERSITY_COMPONENT_ID)
	// 效用拒绝
	utility := selectionTestUtility(t, utilityReceipt, ids)
	utility.Scores[0].Admitted = false
	diversity := selectionTestDiversity(t, diversityReceipt, ids)
	constraints, err := NewListSearchConstraints(globalScope, nil, 0, 64, 4096, 64, 1, 1)
	require.NoError(t, err)
	result, err := HybridListSearch(candidates, utility, diversity, constraints)
	require.NoError(t, err)
	// 空列表 + quality_abstained（全部拒绝）
	assert.Equal(t, "quality_abstained", result.FailureReason)
	// 效用拒绝进入硬原因
	assert.Contains(t, result.Decisions[0].Reasons, "utility_rejected")
}

func TestHybridListSearchProjectContinuationRequired(t *testing.T) {
	candidates := []*SelectionCandidate{
		selectionTestCandidate(t, "c1", 1, "project_domain_progress"),
	}
	candidates[0].ContextScope = "project:p1"
	candidates[0].InterestSummaryID = ""
	candidates[0].IdentityAnchor = ""
	_, utilityReceipt := selectionTestBinding(t, UTILITY_COMPONENT_ID)
	_, diversityReceipt := selectionTestBinding(t, DIVERSITY_COMPONENT_ID)
	utility := selectionTestUtility(t, utilityReceipt, []string{"c1"})
	diversity := selectionTestDiversity(t, diversityReceipt, []string{"c1"})
	constraints, err := NewListSearchConstraints("project:p1", nil, 0, 64, 4096, 64, 1, 1)
	require.NoError(t, err)
	result, err := HybridListSearch(candidates, utility, diversity, constraints)
	require.NoError(t, err)
	// 项目列表缺 project_continuation → 不可完整（Python :1113-1119：硬原因为空 → list_level_not_selected）
	assert.Empty(t, result.PreferredList)
	assert.Equal(t, "no_feasible_complete_list", result.FailureReason)
	assert.Equal(t, []string{"no_feasible_complete_list"}, result.Decisions[0].Reasons)
	// 轨迹行带 required 原因
	foundRequired := false
	for _, entry := range result.BoundedBeamTrace.Entries {
		if stringInSlice("project_continuation_required", entry.FailureReasons) {
			foundRequired = true
		}
	}
	assert.True(t, foundRequired)
}

func TestHybridListSearchBudgetExhausted(t *testing.T) {
	ids := []string{"c1", "c2", "c3", "c4", "c5"}
	candidates := []*SelectionCandidate{}
	for i, id := range ids {
		candidates = append(candidates, selectionTestCandidate(t, id, i+1, "profile_interest_continuation"))
	}
	_, utilityReceipt := selectionTestBinding(t, UTILITY_COMPONENT_ID)
	_, diversityReceipt := selectionTestBinding(t, DIVERSITY_COMPONENT_ID)
	utility := selectionTestUtility(t, utilityReceipt, ids)
	diversity := selectionTestDiversity(t, diversityReceipt, ids)
	// max_expansions=2、max_candidates 放宽到 64，7 层目标必然预算耗尽
	constraints, err := NewListSearchConstraints(globalScope, nil, 0, 64, 2, 64, 1, 1)
	require.NoError(t, err)
	result, err := HybridListSearch(candidates, utility, diversity, constraints)
	require.NoError(t, err)
	assert.Equal(t, "search_budget_exhausted", result.FailureReason)
	assert.Empty(t, result.PreferredList)
	foundBudget := false
	for _, entry := range result.BoundedBeamTrace.Entries {
		if entry.Status == "search_budget_exhausted" {
			foundBudget = true
			assert.Contains(t, entry.FailureReasons, "search_budget_exhausted")
		}
	}
	assert.True(t, foundBudget)
}

func TestHybridListSearchIncompatiblePair(t *testing.T) {
	ids := []string{"c1", "c2"}
	candidates := []*SelectionCandidate{
		selectionTestCandidate(t, "c1", 1, "profile_interest_continuation"),
		selectionTestCandidate(t, "c2", 2, "adjacent_explanation"),
	}
	_, utilityReceipt := selectionTestBinding(t, UTILITY_COMPONENT_ID)
	_, diversityReceipt := selectionTestBinding(t, DIVERSITY_COMPONENT_ID)
	utility := selectionTestUtility(t, utilityReceipt, ids)
	diversity := selectionTestDiversity(t, diversityReceipt, ids)
	// c1/c2 不兼容
	diversity.PairwiseScores[0].Compatible = false
	constraints, err := NewListSearchConstraints(globalScope, nil, 0, 64, 4096, 64, 1, 1)
	require.NoError(t, err)
	result, err := HybridListSearch(candidates, utility, diversity, constraints)
	require.NoError(t, err)
	// 二元列表不兼容 → 单卡列表可行
	assert.Equal(t, []string{"c1"}, result.PreferredList)
	var c2Decision *CandidateHostDecision
	for i := range result.Decisions {
		if result.Decisions[i].CandidateID == "c2" {
			c2Decision = &result.Decisions[i]
		}
	}
	require.NotNil(t, c2Decision)
	assert.Contains(t, c2Decision.Reasons, "list_level_not_selected")
}

func TestHybridListSearchRequiredFirstWindow(t *testing.T) {
	ids := []string{"c1", "c2"}
	candidates := []*SelectionCandidate{
		selectionTestCandidate(t, "c1", 1, "profile_interest_continuation"),
		selectionTestCandidate(t, "c2", 2, "adjacent_explanation"),
	}
	// c2 的兴趣不在必需集合内，c1 在
	_, utilityReceipt := selectionTestBinding(t, UTILITY_COMPONENT_ID)
	_, diversityReceipt := selectionTestBinding(t, DIVERSITY_COMPONENT_ID)
	utility := selectionTestUtility(t, utilityReceipt, ids)
	diversity := selectionTestDiversity(t, diversityReceipt, ids)
	// 必需兴趣可用 → 首窗命中
	constraints, err := NewListSearchConstraints(globalScope, []string{"i-c1"}, 0, 64, 4096, 64, 1, 1)
	require.NoError(t, err)
	result, err := HybridListSearch(candidates, utility, diversity, constraints)
	require.NoError(t, err)
	assert.Equal(t, []string{"c1", "c2"}, result.PreferredList)
	// 必需兴趣不可用（c2 无匹配、把 c1 改成无关兴趣）
	unavailable := []*SelectionCandidate{selectionTestCandidate(t, "c1", 1, "profile_interest_continuation")}
	unavailable[0].InterestSummaryID = "i-other"
	utility2 := selectionTestUtility(t, utilityReceipt, []string{"c1"})
	diversity2 := selectionTestDiversity(t, diversityReceipt, []string{"c1"})
	constraints2, err := NewListSearchConstraints(globalScope, []string{"i-c1"}, 0, 64, 4096, 64, 1, 1)
	require.NoError(t, err)
	result, err = HybridListSearch(unavailable, utility2, diversity2, constraints2)
	require.NoError(t, err)
	assert.Equal(t, "required_first_window_interest_unavailable", result.FailureReason)
}

func TestNewsOnlySearchResult(t *testing.T) {
	_, utilityReceipt := selectionTestBinding(t, UTILITY_COMPONENT_ID)
	utility := selectionTestUtility(t, utilityReceipt, []string{"c1"})
	result, err := NewsOnlySearchResult(utility, globalScope)
	require.NoError(t, err)
	assert.Empty(t, result.PreferredList)
	assert.Empty(t, result.Decisions)
	assert.Empty(t, result.FailureReason)
	assert.Equal(t, GLOBAL_POOL_TARGET_SIZE-1, result.BoundedBeamTrace.TargetSize)
	assert.Equal(t, "", result.DiversityScorerVersion)
	assert.Nil(t, result.DiversityReceipt)
	metadata := result.AsManifestMetadata()
	assert.NotContains(t, metadata["component_receipts"], DIVERSITY_COMPONENT_ID)
}

func TestMineHierarchicalPairs(t *testing.T) {
	// 构造轨迹：preferred + 三种负例 + 去重
	passTrue := true
	passFalse := false
	trace := &BoundedBeamTrace{
		PreferredCandidateIDs:  []string{"c1", "c2"},
		UtilityScorerVersion:   "u1",
		DiversityScorerVersion: "d1",
		SearchVersion:          LIST_SEARCH_VERSION,
		Entries: []BeamTraceEntry{
			{TraceIndex: 1, CandidateIDs: []string{"c1", "c2"}, FailureReasons: []string{}},
			{TraceIndex: 2, CandidateIDs: []string{"c3", "c4"}, HardConstraintsPass: true,
				UtilityPass: &passTrue, DiversityPass: &passFalse, FailureReasons: []string{"diversity_incompatible"}},
			{TraceIndex: 3, CandidateIDs: []string{"c5"}, HardConstraintsPass: true,
				UtilityPass: &passFalse, DiversityPass: &passTrue, FailureReasons: []string{"utility_rejected"}},
			{TraceIndex: 4, CandidateIDs: []string{"c6"}, FailureReasons: []string{"scope_mismatch"}},
			{TraceIndex: 5, CandidateIDs: []string{"c3", "c4"}, HardConstraintsPass: true,
				UtilityPass: &passTrue, DiversityPass: &passFalse, FailureReasons: []string{"diversity_incompatible"}},
			{TraceIndex: 6, CandidateIDs: []string{"c1", "c2"}, HardConstraintsPass: true,
				UtilityPass: &passFalse, DiversityPass: &passFalse, FailureReasons: []string{"x"}},
		},
	}
	pairs, err := MineHierarchicalPairs(trace)
	require.NoError(t, err)
	// trace 6 与 preferred 相同 ID 被跳过；trace 5 与 trace 2 重复被合并
	require.Len(t, pairs, 3)
	assert.Equal(t, "diversity_neg", pairs[0].NegativeType)
	assert.Equal(t, "utility_neg", pairs[1].NegativeType)
	assert.Equal(t, "joint_failure_neg", pairs[2].NegativeType)
	// 去重后 trace_index=5 的重复对被合并
	assert.Equal(t, 2, pairs[0].SourceTraceIndex)
	assert.Equal(t, PREFERENCE_PAIR_VERSION+":2:diversity_neg", pairs[0].PairID)
	payload := pairs[0].AsPayload()
	assert.Equal(t, []string{"c1", "c2"}, payload["preferred_candidate_ids"])
	// 无 preferred → 空
	empty, err := MineHierarchicalPairs(&BoundedBeamTrace{})
	require.NoError(t, err)
	assert.Empty(t, empty)
	// nil 轨迹 → TypeError 对齐
	_, err = MineHierarchicalPairs(nil)
	assert.EqualError(t, err, "beam_trace must be BoundedBeamTrace")
}
