package singleharness

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// 造提案：n 个能力。
func makeProposal(status string, capabilities ...map[string]any) map[string]any {
	proposal := map[string]any{"capabilities": capabilities}
	if status != "" {
		proposal["status"] = status
	}
	return proposal
}

// TestCanonicalCandidateFingerprint 指纹排除动作 ID 与文件名。
func TestCanonicalCandidateFingerprint(t *testing.T) {
	a := []map[string]any{{
		"action_group": "Prompt", "operation": "modify", "role": "solver",
		"target_path": "a/prompt_sections/x.yaml", "expected_effect": "better",
		"action_id": "act-1", "runtime_name": "solver_tool",
		"lever": "prompt", "hypothesis_id": "h1",
	}}
	b := []map[string]any{{
		"action_group": "prompt", "operation": "MODIFY", "role": "solver",
		"target_path": "b/prompt_sections/different.yaml", "expected_effect": "better",
		"action_id": "act-2", "runtime_name": "other_tool",
		"selected_lever": "prompt", "optimization_hypothesis_ids": []any{"h1"},
	}}
	if CanonicalCandidateFingerprint(a) != CanonicalCandidateFingerprint(b) {
		t.Fatal("语义等价能力应有相同指纹")
	}
	c := []map[string]any{{"action_group": "tool", "operation": "add", "expected_effect": "different"}}
	if CanonicalCandidateFingerprint(a) == CanonicalCandidateFingerprint(c) {
		t.Fatal("不同语义应有不同指纹")
	}
	// 非 mapping 项跳过
	if CanonicalCandidateFingerprint([]map[string]any{nil}) == "" {
		t.Fatal("空能力应有指纹")
	}
}

// TestRankCandidateProposals 排序：可执行优先、重复惩罚、顺序保留。
func TestRankCandidateProposals(t *testing.T) {
	targets := map[string]bool{"c1": true, "c2": true}
	cap := func(group, op string, caseIDs ...string) map[string]any {
		ids := []any{}
		for _, id := range caseIDs {
			ids = append(ids, id)
		}
		return map[string]any{"action_group": group, "operation": op, "target_case_ids": ids}
	}
	// p0：可执行，覆盖 1 个目标；p1：不可执行；p2：与 p0 指纹相同（重复）
	proposals := []map[string]any{
		{"capabilities": []any{cap("prompt", "modify", "c1")}},
		{"capabilities": []any{}, "status": "blocked"},
		{"capabilities": []any{cap("prompt", "modify", "c1")}},
	}
	ranked, err := RankCandidateProposals(proposals, targets, nil)
	if err != nil {
		t.Fatalf("排序失败: %v", err)
	}
	if len(ranked) != 3 {
		t.Fatalf("应保留全部提案: %d", len(ranked))
	}
	if ranked[0]["predicted_rank"] != 1 {
		t.Fatalf("首个提案应排第 1: %v", ranked[0]["predicted_rank"])
	}
	// 重复提案特征
	features0 := ranked[0]["ranking_features"].(map[string]any)
	features2 := ranked[2]["ranking_features"].(map[string]any)
	if features0["duplicate"] != 0.0 {
		t.Fatalf("首个不应为重复: %v", features0["duplicate"])
	}
	if features2["duplicate"] != 1.0 || features2["duplicate_of_candidate_id"] != ranked[0]["candidate_id"] {
		t.Fatalf("第三个应为重复: %v", features2)
	}
	// 输入顺序与 candidate_index
	if ranked[0]["candidate_index"] != 0 || ranked[1]["candidate_index"] != 1 {
		t.Fatalf("候选下标应保持: %v", ranked[1]["candidate_index"])
	}
	if ranked[0]["rank_frozen"] != true || ranked[0]["ranking_policy"] != feedbackRankingPolicy {
		t.Fatalf("冻结字段错误: %v", ranked[0]["rank_frozen"])
	}
	if ranked[0]["improver_version_id"] != "I0" {
		t.Fatalf("策略版本应默认 I0: %v", ranked[0]["improver_version_id"])
	}
	// 覆盖率
	if features0["coverage"] != 0.5 || features0["action_count"] != 1 {
		t.Fatalf("覆盖特征错误: %v", features0)
	}
	// 不可执行排在可执行之后（输出保持输入序：output[1]=blocked 提案）
	if ranked[1]["predicted_rank"] != 3 {
		t.Fatalf("不可执行应垫底: %v", ranked[1]["predicted_rank"])
	}
}

// TestIsExecutable 可执行判定分支。
func TestIsExecutable(t *testing.T) {
	if !isExecutable(map[string]any{"is_executable": true}, nil) {
		t.Fatal("显式可执行应通过")
	}
	if isExecutable(map[string]any{"executable": false}, nil) {
		t.Fatal("显式不可执行应拒绝")
	}
	if isExecutable(map[string]any{"status": "blocked"}, nil) {
		t.Fatal("blocked 状态应拒绝")
	}
	if isExecutable(map[string]any{}, nil) {
		t.Fatal("无能力应拒绝")
	}
	if isExecutable(map[string]any{}, []map[string]any{{"action_group": "prompt"}}) {
		t.Fatal("缺 operation 应拒绝")
	}
	if !isExecutable(map[string]any{}, []map[string]any{{"action_group": "prompt", "operation": "modify"}}) {
		t.Fatal("完整能力应通过")
	}
}

// TestBuildCandidateFeedbackCohort cohort 构建与指标不可用分支。
func TestBuildCandidateFeedbackCohort(t *testing.T) {
	if _, err := BuildCandidateFeedbackCohort(map[string]any{}, nil, "x", 0); err == nil ||
		err.Error() != "top_m must be at least 1" {
		t.Fatalf("top_m<1 应拒绝: %v", err)
	}
	cohort := map[string]any{
		"parent_harness_ref":     "parent",
		"source_eval_ref":        "src_eval",
		"frozen_target_case_ids": []any{"c1"},
		"evaluation_protocol":    "proto",
		"rank_frozen":            true,
		"run_id":                 "run-1",
	}
	candidates := []map[string]any{
		{"candidate_id": "a", "candidate_index": 0, "predicted_rank": 1, "source_target_score": 0.5, "candidate_target_score": 0.8, "target_gain": 0.3, "rank_frozen": true},
		{"candidate_id": "b", "candidate_index": 1, "predicted_rank": 2, "source_target_score": 0.5, "candidate_target_score": 0.7, "target_gain": 0.2, "rank_frozen": true},
	}
	result, err := BuildCandidateFeedbackCohort(cohort, candidates, "a", 1)
	if err != nil {
		t.Fatalf("构建失败: %v", err)
	}
	cohortOut := result["cohort"].(map[string]any)
	if cohortOut["cohort_id"] == "" || cohortOut["parent_harness_ref"] != "parent" {
		t.Fatalf("cohort 头错误: %v", cohortOut)
	}
	if cohortOut["target_case_ids"].([]string)[0] != "c1" {
		t.Fatalf("目标列表错误: %v", cohortOut["target_case_ids"])
	}
	selection := result["selection"].(map[string]any)
	if selection["selected_candidate_id"] != "a" || selection["selected_candidate_found"] != true {
		t.Fatalf("选择错误: %v", selection)
	}
	metrics := result["metrics"].(map[string]any)
	bestOfK := metrics["best_of_k_gain"].(map[string]any)
	if bestOfK["status"] != "available" || bestOfK["value"] != 0.3 {
		t.Fatalf("best_of_k 错误: %v", bestOfK)
	}
	regret := metrics["selection_regret"].(map[string]any)
	if regret["value"] != 0.0 || regret["best_candidate_id"] != "a" {
		t.Fatalf("selection_regret 错误: %v", regret)
	}
	// 未选候选标记
	records := result["candidates"].([]map[string]any)
	if records[1]["selected"] != false {
		t.Fatal("未选候选不应标记")
	}
	// target_gain 缺省时由分数差推导
	candidates[0] = map[string]any{"candidate_id": "a", "candidate_index": 0, "predicted_rank": 1, "source_target_score": 0.5, "candidate_target_score": 0.9, "rank_frozen": true}
	candidates[1] = map[string]any{"candidate_id": "b", "candidate_index": 1, "predicted_rank": 2, "source_target_score": 0.5, "candidate_target_score": 0.7, "rank_frozen": true}
	result, err = BuildCandidateFeedbackCohort(cohort, candidates, "a", 1)
	if err != nil {
		t.Fatal(err)
	}
	records = result["candidates"].([]map[string]any)
	if gain := records[0]["outcome"].(map[string]any)["target_gain"]; gain == nil || gain.(*float64) == nil || *(gain.(*float64)) != 0.4 {
		t.Fatalf("target_gain 应推导: %v", gain)
	}
}

// TestSearchMetricUnavailableReason 不可用原因全分支。
func TestSearchMetricUnavailableReason(t *testing.T) {
	base := func() []map[string]any {
		return []map[string]any{
			{"candidate_id": "a", "proposal_rank": map[string]any{"predicted_rank": 1.0, "rank_frozen": true, "ranking_policy": feedbackRankingPolicy},
				"identity": map[string]any{"parent_harness_ref": "p", "source_eval_ref": "s", "target_case_ids": []any{"c1"}, "evaluation_protocol": "e"},
				"outcome":  map[string]any{"target_gain": 0.3, "source_target_score": 0.5}},
			{"candidate_id": "b", "proposal_rank": map[string]any{"predicted_rank": 2.0, "rank_frozen": true, "ranking_policy": feedbackRankingPolicy},
				"identity": map[string]any{"parent_harness_ref": "p", "source_eval_ref": "s", "target_case_ids": []any{"c1"}, "evaluation_protocol": "e"},
				"outcome":  map[string]any{"target_gain": 0.2, "source_target_score": 0.5}},
		}
	}
	if got := searchMetricUnavailableReason("p", "s", map[string]bool{"c1": true}, "e", true, feedbackRankingPolicy, base()[:1]); got != "requires_at_least_two_comparable_candidates" {
		t.Fatalf("单候选应拒绝: %s", got)
	}
	if got := searchMetricUnavailableReason("", "s", map[string]bool{"c1": true}, "e", true, feedbackRankingPolicy, base()); got != "missing_cohort_field:parent_harness_ref" {
		t.Fatalf("缺父引用应报: %s", got)
	}
	if got := searchMetricUnavailableReason("p", "s", map[string]bool{"c1": true}, "e", false, feedbackRankingPolicy, base()); got != "rank_not_frozen_before_evaluation" {
		t.Fatalf("未冻结应报: %s", got)
	}
	if got := searchMetricUnavailableReason("p", "s", map[string]bool{"c1": true}, "e", true, "", base()); got != "ranking_policy_missing" {
		t.Fatalf("缺策略应报: %s", got)
	}
	dup := base()
	dup[1]["candidate_id"] = "a"
	if got := searchMetricUnavailableReason("p", "s", map[string]bool{"c1": true}, "e", true, feedbackRankingPolicy, dup); got != "duplicate_candidate_id" {
		t.Fatalf("重复 ID 应报: %s", got)
	}
	badRanks := base()
	badRanks[1]["proposal_rank"].(map[string]any)["predicted_rank"] = 3.0
	if got := searchMetricUnavailableReason("p", "s", map[string]bool{"c1": true}, "e", true, feedbackRankingPolicy, badRanks); got != "predicted_ranks_are_not_a_frozen_permutation" {
		t.Fatalf("排名非排列应报: %s", got)
	}
	if got := searchMetricUnavailableReason("p", "s", map[string]bool{"c1": true}, "e", true, feedbackRankingPolicy, base()); got != "" {
		t.Fatalf("合法基线应无原因: %s", got)
	}
	// 身份不匹配
	mismatch := base()
	mismatch[1]["identity"].(map[string]any)["parent_harness_ref"] = "other"
	if got := searchMetricUnavailableReason("p", "s", map[string]bool{"c1": true}, "e", true, feedbackRankingPolicy, mismatch); got != "parent_harness_mismatch" {
		t.Fatalf("父不匹配应报: %s", got)
	}
	// target_gain 缺失
	noGain := base()
	noGain[1]["outcome"].(map[string]any)["target_gain"] = nil
	if got := searchMetricUnavailableReason("p", "s", map[string]bool{"c1": true}, "e", true, feedbackRankingPolicy, noGain); got != "target_gain_missing" {
		t.Fatalf("缺增益应报: %s", got)
	}
	// source 分数不一致
	diffSource := base()
	diffSource[1]["outcome"].(map[string]any)["source_target_score"] = 0.6
	if got := searchMetricUnavailableReason("p", "s", map[string]bool{"c1": true}, "e", true, feedbackRankingPolicy, diffSource); got != "source_target_score_mismatch" {
		t.Fatalf("源分不一致应报: %s", got)
	}
}

// TestActivationSummary 激活摘要各分支。
func TestActivationSummary(t *testing.T) {
	fallback := map[string]bool{"c1": true}
	// 缺 runtime_name
	missing := activationSummary(map[string]any{}, []map[string]any{{"action_group": "tool", "operation": "add"}}, fallback)
	if missing["availability"] != "missing_artifact" || missing["reason"] != "expected_runtime_name_missing" {
		t.Fatalf("缺 runtime 名应 missing_artifact: %v", missing)
	}
	// config 面属无插桩组 → not_instrumented（对齐 Python _UNINSTRUMENTED_GROUPS）
	na := activationSummary(map[string]any{}, []map[string]any{{"action_group": "config", "operation": "modify"}}, fallback)
	if na["availability"] != "not_instrumented" {
		t.Fatalf("config 面应 not_instrumented: %v", na)
	}
	// 未登记分组 → not_applicable
	naa := activationSummary(map[string]any{}, []map[string]any{{"action_group": "mystery", "operation": "modify"}}, fallback)
	if naa["availability"] != "not_applicable" {
		t.Fatalf("未登记面应 not_applicable: %v", naa)
	}
	// prompt 面 + 诊断行为证据
	diagnosed := map[string]any{"candidate_failure_diagnoses": map[string]any{
		"c1": []any{map[string]any{"prior_experiment_assessment": map[string]any{
			"intervention_activated": "yes", "predicted_behavior_occurred": "yes", "predicted_outcome_occurred": "no",
		}}},
	}}
	prompt := []map[string]any{{"action_group": "prompt", "operation": "modify"}}
	observed := activationSummary(diagnosed, prompt, fallback)
	if observed["availability"] != "observed" || observed["state"] != "triggered" || observed["observation_source"] != "candidate_failure_analysis" {
		t.Fatalf("诊断行为证据应生效: %v", observed)
	}
	if surfaces := observed["surfaces"].([]string); len(surfaces) != 1 || surfaces[0] != "prompt" {
		t.Fatalf("面应登记: %v", observed["surfaces"])
	}
	// 混合判定 → not_triggered / mixed
	diagnosed["candidate_failure_diagnoses"] = map[string]any{
		"c1": []any{
			map[string]any{"prior_experiment_assessment": map[string]any{"intervention_activated": "yes"}},
			map[string]any{"prior_experiment_assessment": map[string]any{"intervention_activated": "no"}},
		},
	}
	observed = activationSummary(diagnosed, prompt, fallback)
	if observed["state"] != "not_triggered" || observed["intervention_activated"] != "mixed" {
		t.Fatalf("混合判定错误: %v", observed)
	}
	// 无诊断 → not_instrumented
	none := activationSummary(map[string]any{}, prompt, fallback)
	if none["availability"] != "not_instrumented" || none["reason"] != "surface_has_no_activation_event" {
		t.Fatalf("无诊断应 not_instrumented: %v", none)
	}
	// 插桩面：证据齐 + 触发
	instrumented := []map[string]any{{"action_group": "tool", "operation": "add", "runtime_name": "my_tool", "target_case_ids": []any{"c1"}}}
	withEvidence := map[string]any{"pre_edit_invoked_tool_names_by_case": map[string]any{"c1": []any{"my"}}}
	observed = activationSummary(withEvidence, instrumented, fallback)
	if observed["state"] != "triggered" || observed["expected_pair_count"] != 1 || observed["observed_pre_edit_pair_count"] != 1 {
		t.Fatalf("tool 名去后缀匹配应触发: %v", observed)
	}
	// 不匹配 → not_triggered
	withEvidence["pre_edit_invoked_tool_names_by_case"] = map[string]any{"c1": []any{"other"}}
	observed = activationSummary(withEvidence, instrumented, fallback)
	if observed["state"] != "not_triggered" || observed["missing_pair_count"] != 1 {
		t.Fatalf("未匹配应 not_triggered: %v", observed)
	}
	// 证据缺失 → missing_artifact
	observed = activationSummary(map[string]any{}, instrumented, fallback)
	if observed["availability"] != "missing_artifact" || observed["reason"] != "pre_edit_invocation_evidence_missing" {
		t.Fatalf("证据缺失应报: %v", observed)
	}
}

// TestTargetRegression 目标回滚分支。
func TestTargetRegression(t *testing.T) {
	fallback := map[string]bool{"c1": true, "c2": true}
	// 显式清单
	result := targetRegression(map[string]any{"regressed_target_case_ids": []any{"c1"}}, fallback)
	if result["availability"] != "observed" || result["regressed_case_count"] != 1 {
		t.Fatalf("显式清单错误: %v", result)
	}
	if result["rate"] != 0.5 {
		t.Fatalf("比率错误: %v", result["rate"])
	}
	// 分数对比
	scores := map[string]any{
		"source_case_scores":    map[string]any{"c1": 0.8, "c2": 0.5},
		"candidate_case_scores": map[string]any{"c1": 0.7, "c2": 0.5},
	}
	result = targetRegression(scores, fallback)
	if result["regressed_case_count"] != 1 || result["rate"] != 0.5 {
		t.Fatalf("分数对比错误: %v", result)
	}
	// 缺 artifact
	result = targetRegression(map[string]any{}, fallback)
	if result["availability"] != "missing_artifact" {
		t.Fatalf("缺分数应 missing_artifact: %v", result)
	}
	// 缺单用例分数
	partial := map[string]any{"source_case_scores": map[string]any{"c1": 0.8}, "candidate_case_scores": map[string]any{"c1": 0.7}}
	result = targetRegression(partial, fallback)
	if result["availability"] != "missing_artifact" {
		t.Fatalf("用例分数缺失应 missing_artifact: %v", result)
	}
	// 空目标 → rate nil
	empty := targetRegression(scores, map[string]bool{})
	if empty["rate"] != nil {
		t.Fatalf("空目标 rate 应 nil: %v", empty["rate"])
	}
}

// TestVerifierSummary 验证器摘要。
func TestVerifierSummary(t *testing.T) {
	if summary := verifierSummary(map[string]any{}); summary["availability"] != "missing_artifact" || summary["case_count"] != 0 {
		t.Fatalf("缺 artifact 应 missing: %v", summary)
	}
	result := verifierSummary(map[string]any{"verifier_deltas_by_case": map[string]any{
		"c1": map[string]any{"partial_progress": true, "newly_passed_requirements": []any{"a", "b"}, "regressed_requirements": []any{"c"}},
		"c2": map[string]any{"not-a-delta": true},
		"c3": "bad",
	}})
	if result["availability"] != "observed" || result["case_count"] != 2 {
		t.Fatalf("摘要错误: %v", result)
	}
	if result["partial_progress_case_count"] != 1 || result["newly_passed_requirements_count"] != 2 || result["regressed_requirements_count"] != 1 {
		t.Fatalf("计数错误: %v", result)
	}
}

// TestEvidenceRefsAndHelpers 引用别名与助手。
func TestEvidenceRefsAndHelpers(t *testing.T) {
	refs := evidenceRefs(map[string]any{
		"source_eval_ref_path":         "s.yaml",
		"candidate_eval_ref":           "c.yaml",
		"member_optimization_ref_path": "m.yaml",
	})
	if len(refs) != 3 || refs["source_eval_ref"] != "s.yaml" || refs["candidate_eval_ref"] != "c.yaml" {
		t.Fatalf("引用别名错误: %v", refs)
	}
	// runtimeNamesMatch
	if runtimeNamesMatch("tool", "my-tool", "My Tool") {
		t.Fatal("空格归一后不应匹配（对齐 Python：normalize 产生 my tool≠my_tool）")
	}
	if !runtimeNamesMatch("tool", "my-tool", "my_tool") {
		t.Fatal("tool 连字符归一应匹配")
	}
	if !runtimeNamesMatch("tool", "my", "my_tool") {
		t.Fatal("tool 后缀应剥除")
	}
	if runtimeNamesMatch("tool", "my", "other_tool") {
		t.Fatal("不同名不应匹配")
	}
	if !runtimeNamesMatch("skill", "my_tool", "my_tool") {
		t.Fatal("非 tool 组完全相同应匹配")
	}
	if runtimeNamesMatch("skill", "my", "my_tool") {
		t.Fatal("非 tool 组不去后缀")
	}
	if runtimeNamesMatch("tool", "", "x") {
		t.Fatal("空计划名不匹配")
	}
	// normalizeText
	if normalizeText("  A  B\t\nC ") != "a b c" {
		t.Fatalf("空白折叠错误: %q", normalizeText("  A  B\t\nC "))
	}
	if normalizeText("ＡＢ") != "ab" {
		t.Fatalf("NFKC 应折叠全角: %q", normalizeText("ＡＢ"))
	}
	// positiveInt / candidateIndexOf / caseIDsOf
	if positiveInt("3") == nil || positiveInt(0) != nil || positiveInt(-1) != nil {
		t.Fatal("positiveInt 分支错误")
	}
	if candidateIndexOf(map[string]any{"candidate_index": "7"}, 2) != 7 {
		t.Fatal("字符串下标应解析")
	}
	if candidateIndexOf(map[string]any{"candidate_index": -1}, 2) != 2 {
		t.Fatal("负下标应回退")
	}
	if !reflect.DeepEqual(sortedKeysBool(map[string]bool{"b": true, "a": true}), []string{"a", "b"}) {
		t.Fatal("键排序错误")
	}
	if !setsIntersect(map[string]bool{"x": true}, map[string]bool{"x": true, "y": true}) {
		t.Fatal("相交应通过")
	}
	// cohortIDOf 稳定性
	id1 := cohortIDOf("p", "s", map[string]bool{"c1": true}, "e")
	id2 := cohortIDOf("p", "s", map[string]bool{"c1": true}, "e")
	if id1 != id2 || len(id1) != len("cohort_")+16 {
		t.Fatalf("cohort ID 应稳定: %s", id1)
	}
	// diagnosisItems
	if len(diagnosisItems([]any{map[string]any{}, "bad"})) != 1 {
		t.Fatal("非 mapping 应过滤")
	}
	if len(diagnosisItems(map[string]any{})) != 1 {
		t.Fatal("单映射应包装")
	}
	if len(diagnosisItems(42)) != 0 {
		t.Fatal("其他类型应空")
	}
}

// TestNormalizedValueDeep 递归规范化（剔键/排序/标量透传）。
func TestNormalizedValueDeep(t *testing.T) {
	value := map[string]any{
		"runtime_name": "x", "target_path": "p", "action_id": "a",
		"b": 1, "a": []any{"x", 2},
	}
	normalized := normalizedValue(value).(map[string]any)
	if _, present := normalized["runtime_name"]; present {
		t.Fatal("runtime_name 应剔除")
	}
	if _, present := normalized["action_id"]; present {
		t.Fatal("action_id 应剔除")
	}
	if len(normalized) != 2 {
		t.Fatalf("应只剩语义键: %v", normalized)
	}
	// 列表排序按 canonical json
	list := normalizedValue([]any{"b", "a"}).([]any)
	if list[0] != "a" || list[1] != "b" {
		t.Fatalf("列表应排序: %v", list)
	}
	if got := normalizedValue(42); got != 42 {
		t.Fatal("标量应透传")
	}
}

// TestCausalExperimentSummaryBranches 因果实验摘要分支。
func TestCausalExperimentSummaryBranches(t *testing.T) {
	candidate := map[string]any{
		"causal_intervention_contracts": []any{
			map[string]any{"target_case_ids": []any{"c1"}, "prediction": "p1"},
			map[string]any{"target_case_ids": []any{"c9"}, "prediction": "p9"},
			map[string]any{"prediction": "p-none"},
			"bad",
		},
		"candidate_failure_diagnoses": map[string]any{
			"c1": []any{map[string]any{"prior_experiment_assessment": map[string]any{"state": "ok"}}},
			"c2": map[string]any{"prior_experiment_assessment": map[string]any{"state": "wrapped"}},
			"c3": []any{"bad"},
		},
	}
	summary, err := causalExperimentSummary(candidate, map[string]bool{"c1": true, "c2": true})
	if err != nil {
		t.Fatal(err)
	}
	if summary["availability"] != "observed" || summary["prediction_recorded_before_evaluation"] != true {
		t.Fatalf("有契约应 observed: %v", summary)
	}
	contracts := summary["intervention_contracts"].([]map[string]any)
	if len(contracts) != 2 {
		t.Fatalf("c1 契约与无目标契约应保留: %d", len(contracts))
	}
	assessments := summary["assessment_by_case"].(map[string]any)
	if len(assessments) != 2 {
		t.Fatalf("c1/c2 诊断应收录: %v", assessments)
	}
	// 无契约 → not_instrumented
	empty, err := causalExperimentSummary(map[string]any{}, map[string]bool{"c1": true})
	if err != nil || empty["availability"] != "not_instrumented" {
		t.Fatalf("无契约应 not_instrumented: %v %v", empty, err)
	}
}

// TestSearchMetricsUnavailableBranch 不可用指标分支。
func TestSearchMetricsUnavailableBranch(t *testing.T) {
	metrics := searchMetrics([]map[string]any{}, 3, "some_reason")
	bestOfK := metrics["best_of_k_gain"].(map[string]any)
	if bestOfK["status"] != "unavailable" || bestOfK["k"] != 0 || bestOfK["reason"] != "some_reason" {
		t.Fatalf("best_of_k 不可用形态错误: %v", bestOfK)
	}
	topM := metrics["top_m_gain"].(map[string]any)
	if topM["requested_m"] != 3 || topM["effective_m"] != nil {
		t.Fatalf("top_m 不可用形态错误: %v", topM)
	}
	if metrics["selection_regret"].(map[string]any)["reason"] != "some_reason" {
		t.Fatal("regret 应带原因")
	}
}

// TestOptionalNumberAndIntOf 数值投影补全。
func TestOptionalNumberAndIntOf(t *testing.T) {
	if optionalNumber(true) != nil || optionalNumber(nil) != nil || optionalNumber("abc") != nil {
		t.Fatal("非法输入应 nil")
	}
	if v := optionalNumber(" 2.5 "); v == nil || *v != 2.5 {
		t.Fatal("字符串数字应解析")
	}
	f := 1.5
	if v := optionalNumber(&f); v == nil || *v != 1.5 {
		t.Fatal("*float64 应透传")
	}
	if intOf(7, 0) != 7 || intOf(int64(8), 0) != 8 || intOf(9.9, 0) != 9 || intOf("x", 5) != 5 {
		t.Fatal("intOf 分支错误")
	}
}

// TestExpandUserBranches 波浪线展开分支。
func TestExpandUserBranches(t *testing.T) {
	if got := expandUser("/abs/path"); got != "/abs/path" {
		t.Fatalf("绝对路径应原样: %s", got)
	}
	if got := expandUser("rel/path"); got != "rel/path" {
		t.Fatalf("相对路径应原样: %s", got)
	}
	home, err := os.UserHomeDir()
	if err == nil {
		if got := expandUser("~/sub"); got != filepath.Join(home, "sub") {
			t.Fatalf("~ 展开错误: %s", got)
		}
	}
}

// TestNumericOfBranches 数值提取补全。
func TestNumericOfBranches(t *testing.T) {
	if v, ok := numericOf(int64(3)); !ok || v != 3 {
		t.Fatal("int64 应接受")
	}
	if v, ok := numericOf(2.5); !ok || v != 2.5 {
		t.Fatal("float64 应接受")
	}
	if _, ok := numericOf("x"); ok {
		t.Fatal("字符串应拒绝")
	}
}
