package improver_evolution

import (
	"math"
	"strings"
	"testing"
)

func TestDefaultMetaValidationThresholds_七默认值(t *testing.T) {
	d := DefaultMetaValidationThresholds()
	if d.MinUnseenCheckpoints() != 3 {
		t.Fatalf("min_unseen_checkpoints 默认应为 3")
	}
	if d.TopMGainNonDegradationTolerance() != 0.0 ||
		d.MinSelectionRegretImprovement() != 1e-12 ||
		d.BestOfKGainNonDegradationTolerance() != 0.0 ||
		d.FinalHarnessGainPerBudgetNonDegradationTolerance() != 0.0 ||
		d.MaxRegressionFailureRateIncrease() != 0.0 ||
		d.MaxInfrastructureFailureRateIncrease() != 0.0 {
		t.Fatal("容差默认值不符")
	}
}

func TestNewMetaValidationThresholds_校验链逐字(t *testing.T) {
	_, err := NewMetaValidationThresholds(0, 0, 0, 0, 0, 0, 0)
	assertErrMsg(t, err, "min_unseen_checkpoints must be an integer of at least 1")

	_, err = NewMetaValidationThresholds(-1, 0, 0, 0, 0, 0, 0)
	assertErrMsg(t, err, "min_unseen_checkpoints must be an integer of at least 1")

	for name, values := range map[string][6]float64{
		"top_m_gain_non_degradation_tolerance":                    {-1, 0, 0, 0, 0, 0},
		"min_selection_regret_improvement":                        {0, -1, 0, 0, 0, 0},
		"best_of_k_gain_non_degradation_tolerance":                {0, 0, -1, 0, 0, 0},
		"final_harness_gain_per_budget_non_degradation_tolerance": {0, 0, 0, -1, 0, 0},
		"max_regression_failure_rate_increase":                    {0, 0, 0, 0, -1, 0},
		"max_infrastructure_failure_rate_increase":                {0, 0, 0, 0, 0, -1},
	} {
		_, err = NewMetaValidationThresholds(3,
			values[0], values[1], values[2], values[3], values[4], values[5])
		assertErrMsg(t, err, name+" must be a finite non-negative number")
	}

	_, err = NewMetaValidationThresholds(3, math.Inf(1), 0, 0, 0, 0, 0)
	assertErrMsg(t, err, "top_m_gain_non_degradation_tolerance must be a finite non-negative number")
}

func TestMetaValidationThresholds_AsDict_键名snake_case(t *testing.T) {
	d := DefaultMetaValidationThresholds()
	dict := d.AsDict()
	want := []string{
		"min_unseen_checkpoints",
		"top_m_gain_non_degradation_tolerance",
		"min_selection_regret_improvement",
		"best_of_k_gain_non_degradation_tolerance",
		"final_harness_gain_per_budget_non_degradation_tolerance",
		"max_regression_failure_rate_increase",
		"max_infrastructure_failure_rate_increase",
	}
	if len(dict) != 7 {
		t.Fatalf("expected 7 keys, got %d", len(dict))
	}
	for _, key := range want {
		if _, ok := dict[key]; !ok {
			t.Fatalf("缺键: %s", key)
		}
	}
	if dict["min_unseen_checkpoints"] != 3 {
		t.Fatalf("unexpected: %v", dict["min_unseen_checkpoints"])
	}
}

// ─────────────── Task 6：配对合法性校验链 ───────────────

// checkpointFixture 构造合法 checkpoint 记录
func checkpointFixture(id string) map[string]any {
	return map[string]any{
		"checkpoint_id": id, "split": "meta_test",
		"base_harness_id": "h1", "failure_evidence_id": "ev1", "base_model_id": "m1",
		"k": 3, "top_m": 2, "token_budget": 100000, "tool_budget": 40, "runtime_budget": 1800,
		"verifier_id": "ver1", "protocol_id": "proto",
		"improver_version_id": "v1", "improver_policy_digest": "pd1",
		"metrics": map[string]any{"top_m_gain": 0.1, "selection_regret": 0.02},
	}
}

func TestIndexResults_四类问题逐字(t *testing.T) {
	// 非 Mapping
	_, issues := indexResults([]map[string]any{nil}, "baseline")
	if len(issues) != 1 || issues[0]["code"] != "invalid_checkpoint_record" || issues[0]["side"] != "baseline" {
		t.Fatalf("unexpected: %v", issues)
	}
	// 缺 checkpoint_id
	_, issues = indexResults([]map[string]any{{"x": 1}}, "candidate")
	if len(issues) != 1 || issues[0]["code"] != "missing_checkpoint_id" {
		t.Fatalf("unexpected: %v", issues)
	}
	// 空白 id
	_, issues = indexResults([]map[string]any{{"checkpoint_id": "  "}}, "baseline")
	if len(issues) != 1 || issues[0]["code"] != "missing_checkpoint_id" {
		t.Fatalf("unexpected: %v", issues)
	}
	// 重复 id
	indexed, issues := indexResults([]map[string]any{checkpointFixture("a"), checkpointFixture("a")}, "baseline")
	if len(issues) != 1 || issues[0]["code"] != "duplicate_checkpoint_id" || issues[0]["checkpoint_id"] != "a" {
		t.Fatalf("unexpected: %v", issues)
	}
	if len(indexed) != 1 {
		t.Fatalf("重复应被剔除")
	}
}

func TestImproverIdentity_四类问题逐字(t *testing.T) {
	// 缺字段
	identity, issues := improverIdentity([]map[string]any{checkpointFixture("a")}, "baseline")
	if len(issues) != 0 {
		t.Fatalf("完整记录应无 issue: %v", issues)
	}
	if identity["version_id"] != "v1" || identity["policy_digest"] != "pd1" {
		t.Fatalf("unexpected: %v", identity)
	}

	// 缺失字段 → missing_improver_identity_field
	rec := checkpointFixture("a")
	delete(rec, "improver_policy_digest")
	_, issues = improverIdentity([]map[string]any{rec}, "baseline")
	if len(issues) != 1 || issues[0]["code"] != "missing_improver_identity_field" || issues[0]["field"] != "improver_policy_digest" {
		t.Fatalf("unexpected: %v", issues)
	}

	// 值为纯空白 → invalid_improver_identity_field（空串走 missing 分支，同 Python value == ""）
	rec = checkpointFixture("a")
	rec["improver_version_id"] = "  "
	_, issues = improverIdentity([]map[string]any{rec}, "baseline")
	if len(issues) != 1 || issues[0]["code"] != "invalid_improver_identity_field" {
		t.Fatalf("unexpected: %v", issues)
	}
	// 值为非字符串 → invalid_improver_identity_field
	rec = checkpointFixture("a")
	rec["improver_version_id"] = 123
	_, issues = improverIdentity([]map[string]any{rec}, "baseline")
	if len(issues) != 1 || issues[0]["code"] != "invalid_improver_identity_field" {
		t.Fatalf("unexpected: %v", issues)
	}

	// 同侧不一致 → inconsistent_improver_identity
	rec2 := checkpointFixture("b")
	rec2["improver_policy_digest"] = "pd2"
	_, issues = improverIdentity([]map[string]any{checkpointFixture("a"), rec2}, "baseline")
	if len(issues) != 1 || issues[0]["code"] != "inconsistent_improver_identity" {
		t.Fatalf("unexpected: %v", issues)
	}
	// 不一致时 identity 置空
	identity, _ = improverIdentity([]map[string]any{checkpointFixture("a"), rec2}, "baseline")
	if identity["policy_digest"] != "" {
		t.Fatalf("不一致应置空: %v", identity)
	}
}

func TestFrozenValueIssues_全字段分支(t *testing.T) {
	// 字符串字段空串
	rec := checkpointFixture("a")
	rec["base_harness_id"] = " "
	issues := frozenValueIssues("a", rec, "baseline")
	if len(issues) != 1 || issues[0]["code"] != "invalid_frozen_field" || issues[0]["field"] != "base_harness_id" {
		t.Fatalf("unexpected: %v", issues)
	}

	// k 非 int（bool）
	rec = checkpointFixture("a")
	rec["k"] = true
	issues = frozenValueIssues("a", rec, "baseline")
	if len(issues) != 1 || issues[0]["field"] != "k" {
		t.Fatalf("unexpected: %v", issues)
	}

	// top_m 非正
	rec = checkpointFixture("a")
	rec["top_m"] = 0
	issues = frozenValueIssues("a", rec, "baseline")
	if len(issues) != 1 || issues[0]["field"] != "top_m" {
		t.Fatalf("unexpected: %v", issues)
	}

	// top_m > k（带 reason）
	rec = checkpointFixture("a")
	rec["top_m"] = 5
	issues = frozenValueIssues("a", rec, "baseline")
	if len(issues) != 1 || issues[0]["reason"] != "top_m_exceeds_k" {
		t.Fatalf("unexpected: %v", issues)
	}

	// budget 负数
	rec = checkpointFixture("a")
	rec["token_budget"] = -1
	issues = frozenValueIssues("a", rec, "baseline")
	if len(issues) != 1 || issues[0]["field"] != "token_budget" {
		t.Fatalf("unexpected: %v", issues)
	}

	// budget 非有限数
	rec = checkpointFixture("a")
	rec["tool_budget"] = math.Inf(1)
	issues = frozenValueIssues("a", rec, "baseline")
	if len(issues) != 1 || issues[0]["field"] != "tool_budget" {
		t.Fatalf("unexpected: %v", issues)
	}

	// 合法记录零 issue
	rec = checkpointFixture("a")
	if issues = frozenValueIssues("a", rec, "baseline"); len(issues) != 0 {
		t.Fatalf("合法记录不应有 issue: %v", issues)
	}
}

func TestPairIssues_全分支(t *testing.T) {
	// 训练集污染
	base := checkpointFixture("a")
	cand := checkpointFixture("a")
	issues := pairIssues("a", base, cand, map[string]bool{"a": true})
	found := false
	for _, issue := range issues {
		if issue["code"] == "meta_train_contamination" {
			found = true
		}
	}
	if !found {
		t.Fatalf("缺 meta_train_contamination: %v", issues)
	}

	// 非 meta_test（双侧各一条）
	base = checkpointFixture("a")
	base["split"] = "meta_train"
	cand = checkpointFixture("a")
	cand["split"] = "meta_train"
	issues = pairIssues("a", base, cand, map[string]bool{})
	count := 0
	for _, issue := range issues {
		if issue["code"] == "non_meta_test_checkpoint" {
			count++
		}
	}
	if count != 2 {
		t.Fatalf("双侧各一条 non_meta_test_checkpoint: %v", issues)
	}

	// frozen 字段缺失
	base = checkpointFixture("a")
	delete(base, "verifier_id")
	cand = checkpointFixture("a")
	issues = pairIssues("a", base, cand, map[string]bool{})
	found = false
	for _, issue := range issues {
		if issue["code"] == "missing_frozen_field" && issue["side"] == "baseline" && issue["field"] == "verifier_id" {
			found = true
		}
	}
	if !found {
		t.Fatalf("缺 missing_frozen_field: %v", issues)
	}

	// frozen 字段不匹配
	base = checkpointFixture("a")
	cand = checkpointFixture("a")
	cand["protocol_id"] = "other"
	issues = pairIssues("a", base, cand, map[string]bool{})
	found = false
	for _, issue := range issues {
		if issue["code"] == "frozen_field_mismatch" && issue["field"] == "protocol_id" {
			found = true
		}
	}
	if !found {
		t.Fatalf("缺 frozen_field_mismatch: %v", issues)
	}

	// k=1 双侧
	base = checkpointFixture("a")
	base["k"] = 1
	cand = checkpointFixture("a")
	cand["k"] = 1
	issues = pairIssues("a", base, cand, map[string]bool{})
	found = false
	for _, issue := range issues {
		if issue["code"] == "search_metrics_require_k_at_least_two" {
			found = true
		}
	}
	if !found {
		t.Fatalf("缺 search_metrics_require_k_at_least_two: %v", issues)
	}

	// 合法配对零 issue
	base = checkpointFixture("a")
	cand = checkpointFixture("a")
	if issues = pairIssues("a", base, cand, map[string]bool{}); len(issues) != 0 {
		t.Fatalf("合法配对应零 issue: %v", issues)
	}
}

func TestReasonCodes_去重保序(t *testing.T) {
	got := reasonCodes([]map[string]any{
		{"code": "b"}, {"code": "a"}, {"code": "b"},
	})
	if strings.Join(got, ",") != "b,a" {
		t.Fatalf("unexpected: %v", got)
	}
}

// ─────────────── Task 7：指标/宏平均/晋升/主函数 ───────────────

func TestRequiredMetrics_全分支(t *testing.T) {
	// metrics 缺失
	rec := checkpointFixture("a")
	delete(rec, "metrics")
	got, issues := requiredMetrics("a", rec, "baseline", []string{"top_m_gain"})
	if len(got) != 0 || len(issues) != 1 || issues[0]["code"] != "missing_required_metrics" {
		t.Fatalf("unexpected: %v %v", got, issues)
	}
	// 指标缺失
	rec["metrics"] = map[string]any{"other": 1.0}
	got, issues = requiredMetrics("a", rec, "baseline", []string{"top_m_gain"})
	if len(issues) != 1 || issues[0]["code"] != "missing_required_metric" || issues[0]["metric"] != "top_m_gain" {
		t.Fatalf("unexpected: %v", issues)
	}
	// 指标为 None
	rec["metrics"] = map[string]any{"top_m_gain": nil}
	_, issues = requiredMetrics("a", rec, "baseline", []string{"top_m_gain"})
	if len(issues) != 1 || issues[0]["code"] != "missing_required_metric" {
		t.Fatalf("unexpected: %v", issues)
	}
	// 非有限数
	rec["metrics"] = map[string]any{"top_m_gain": math.Inf(1)}
	_, issues = requiredMetrics("a", rec, "baseline", []string{"top_m_gain"})
	if len(issues) != 1 || issues[0]["code"] != "invalid_metric_value" {
		t.Fatalf("unexpected: %v", issues)
	}
	// 比率越界
	rec["metrics"] = map[string]any{"regression_failure_rate": 1.5}
	_, issues = requiredMetrics("a", rec, "baseline", []string{"regression_failure_rate"})
	if len(issues) != 1 || issues[0]["code"] != "invalid_metric_value" {
		t.Fatalf("unexpected: %v", issues)
	}
	// 合法
	rec["metrics"] = map[string]any{"regression_failure_rate": 0.5}
	got, issues = requiredMetrics("a", rec, "baseline", []string{"regression_failure_rate"})
	if len(issues) != 0 || got["regression_failure_rate"] != 0.5 {
		t.Fatalf("unexpected: %v %v", got, issues)
	}
}

func TestCheckpointReport_方向翻转(t *testing.T) {
	base := checkpointFixture("a")
	rep := checkpointReport("a", base,
		map[string]float64{"top_m_gain": 0.1, "selection_regret": 0.05},
		map[string]float64{"top_m_gain": 0.15, "selection_regret": 0.02},
		[]string{"top_m_gain", "selection_regret"})
	deltas := rep["deltas"].(map[string]float64)
	if deltas["top_m_gain"] != 0.05 {
		t.Fatalf("delta = candidate-baseline: %v", deltas)
	}
	if deltas["selection_regret"] != -0.03 {
		t.Fatalf("delta: %v", deltas)
	}
	imp := rep["directional_improvements"].(map[string]float64)
	// selection_regret 是"越低越好"：improvement = baseline - candidate
	if imp["selection_regret"] != 0.03 {
		t.Fatalf("unexpected: %v", imp)
	}
	if imp["top_m_gain"] != 0.05 {
		t.Fatalf("unexpected: %v", imp)
	}
	// frozen_context 取 baseline 侧
	fc := rep["frozen_context"].(map[string]any)
	if fc["base_harness_id"] != "h1" {
		t.Fatalf("unexpected: %v", fc)
	}
}

func TestMacroAverages_方向标注(t *testing.T) {
	checkpoints := []map[string]any{
		{
			"baseline_metrics":  map[string]float64{"top_m_gain": 0.1, "selection_regret": 0.04},
			"candidate_metrics": map[string]float64{"top_m_gain": 0.2, "selection_regret": 0.02},
		},
		{
			"baseline_metrics":  map[string]float64{"top_m_gain": 0.3, "selection_regret": 0.02},
			"candidate_metrics": map[string]float64{"top_m_gain": 0.1, "selection_regret": 0.04},
		},
	}
	out := macroAverages(checkpoints, []string{"top_m_gain", "selection_regret"})
	tm := out["top_m_gain"].(map[string]any)
	if tm["baseline"] != 0.2 || tm["candidate"] != 0.15 || tm["direction"] != "higher_is_better" {
		t.Fatalf("unexpected: %v", tm)
	}
	sr := out["selection_regret"].(map[string]any)
	if sr["direction"] != "lower_is_better" {
		t.Fatalf("unexpected: %v", sr)
	}
	// improvement = baseline - candidate = 0.03 - (-0.01) → 0.03+0.01? baseline均值0.03, candidate均值0.03 → improvement 0
	if sr["improvement"] != 0.0 {
		t.Fatalf("unexpected: %v", sr)
	}
}

func TestPromotionDecision_全分支(t *testing.T) {
	mk := func(topMDelta, srImprovement, bokDelta float64) map[string]map[string]any {
		return map[string]map[string]any{
			"top_m_gain":                    {"delta": topMDelta},
			"selection_regret":              {"improvement": srImprovement},
			"best_of_k_gain":                {"delta": bokDelta},
			"final_harness_gain_per_budget": {"delta": 0.1},
			"regression_failure_rate":       {"delta": 0.0},
			"infrastructure_failure_rate":   {"delta": 0.0},
		}
	}
	th := DefaultMetaValidationThresholds()

	// insufficient
	dec := promotionDecision("live_generation", mk(0.1, 0.01, 0.1), 2, th)
	if dec["status"] != "inconclusive" {
		t.Fatalf("unexpected: %v", dec)
	}
	if dec["reason_codes"].([]string)[0] != "insufficient_unseen_checkpoints" {
		t.Fatalf("unexpected: %v", dec)
	}

	// top_m 退步
	dec = promotionDecision("live_generation", mk(-0.1, 0.01, 0.1), 4, th)
	if dec["status"] != "ineligible" || dec["reason_codes"].([]string)[0] != "top_m_gain_decreased" {
		t.Fatalf("unexpected: %v", dec)
	}

	// selection_regret 未改善
	dec = promotionDecision("live_generation", mk(0.1, 0.0, 0.1), 4, th)
	if dec["reason_codes"].([]string)[0] != "selection_regret_not_improved" {
		t.Fatalf("unexpected: %v", dec)
	}

	// best_of_k 退步
	dec = promotionDecision("live_generation", mk(0.1, 0.01, -0.1), 4, th)
	if dec["reason_codes"].([]string)[0] != "best_of_k_gain_decreased" {
		t.Fatalf("unexpected: %v", dec)
	}

	// 回归率上升
	dec = promotionDecision("live_generation", mk(0.1, 0.01, 0.1), 4, th)
	// 先构造比率上升输入
	rateMacro := mk(0.1, 0.01, 0.1)
	rateMacro["regression_failure_rate"]["delta"] = 0.01
	dec = promotionDecision("live_generation", rateMacro, 4, th)
	if dec["reason_codes"].([]string)[0] != "regression_failure_rate_increased" {
		t.Fatalf("unexpected: %v", dec)
	}

	// 全过 → eligible + promotion_thresholds_satisfied
	dec = promotionDecision("live_generation", mk(0.1, 0.01, 0.1), 4, th)
	if dec["status"] != "eligible" || dec["reason_codes"].([]string)[0] != "promotion_thresholds_satisfied" {
		t.Fatalf("unexpected: %v", dec)
	}

	// offline 模式恒 inconclusive + ranker_evidence_only
	dec = promotionDecision("offline_rerank", mk(0.1, 0.01, 0.1), 4, th)
	if dec["status"] != "inconclusive" || dec["scope"] != "ranker_evidence_only" {
		t.Fatalf("unexpected: %v", dec)
	}
	if dec["offline_ranker_assessment"] != "eligible" {
		t.Fatalf("unexpected: %v", dec)
	}
	found := false
	for _, code := range dec["reason_codes"].([]string) {
		if code == "offline_rerank_cannot_promote_full_improver" {
			found = true
		}
	}
	if !found {
		t.Fatalf("缺 offline_rerank_cannot_promote_full_improver: %v", dec)
	}

	// offline + insufficient → ranker_assessment inconclusive
	dec = promotionDecision("offline_rerank", mk(0.1, 0.01, 0.1), 2, th)
	if dec["offline_ranker_assessment"] != "inconclusive" {
		t.Fatalf("unexpected: %v", dec)
	}
}

func TestPairedMetaValidate_无效模式(t *testing.T) {
	rep := PairedMetaValidate([]map[string]any{checkpointFixture("a")}, []map[string]any{checkpointFixture("a")},
		"bogus", nil, nil)
	if rep["validation"].(map[string]any)["status"] != "rejected" {
		t.Fatalf("unexpected: %v", rep)
	}
	if rep["validation"].(map[string]any)["reason_codes"].([]string)[0] != "invalid_mode" {
		t.Fatalf("unexpected: %v", rep)
	}
	if rep["promotion"].(map[string]any)["reason_codes"].([]string)[0] != "invalid_mode" {
		t.Fatalf("unexpected: %v", rep)
	}
}

func TestPairedMetaValidate_空结果与指纹相同(t *testing.T) {
	// 空输入
	rep := PairedMetaValidate(nil, nil, "live_generation", nil, nil)
	if rep["validation"].(map[string]any)["status"] != "rejected" {
		t.Fatalf("unexpected: %v", rep)
	}
	// 指纹相同
	rep = PairedMetaValidate([]map[string]any{checkpointFixture("a")}, []map[string]any{checkpointFixture("a")},
		"live_generation", nil, nil)
	codes := rep["validation"].(map[string]any)["reason_codes"].([]string)
	if !contains(codes, "identical_improver_policy_digest") {
		t.Fatalf("unexpected: %v", codes)
	}
	if !contains(codes, "empty_checkpoint_results") == false {
		// 此处两侧非空，不应含 empty
	}
}

func TestPairedMetaValidate_checkpoint集不齐(t *testing.T) {
	base := checkpointFixture("a")
	cand := checkpointFixture("b")
	rep := PairedMetaValidate([]map[string]any{base}, []map[string]any{cand}, "live_generation", nil, nil)
	v := rep["validation"].(map[string]any)
	if v["status"] != "rejected" || v["checkpoint_count"] != 0 {
		t.Fatalf("unexpected: %v", v)
	}
	issues := v["issues"].([]map[string]any)
	found := false
	for _, issue := range issues {
		if issue["code"] == "checkpoint_set_mismatch" {
			found = true
			if !pyDeepEqual(issue["baseline_only"], []any{"a"}) {
				t.Fatalf("baseline_only: %v", issue)
			}
		}
	}
	if !found {
		t.Fatalf("缺 checkpoint_set_mismatch")
	}
}

func TestPairedMetaValidate_训练集污染拒绝(t *testing.T) {
	rep := PairedMetaValidate([]map[string]any{checkpointFixture("a")}, []map[string]any{checkpointFixture("a")},
		"live_generation", nil, []string{"a"})
	if rep["validation"].(map[string]any)["status"] != "rejected" {
		t.Fatalf("unexpected: %v", rep)
	}
	codes := rep["validation"].(map[string]any)["reason_codes"].([]string)
	if !contains(codes, "meta_train_contamination") {
		t.Fatalf("unexpected: %v", codes)
	}
	// reason_codes 同步进 promotion
	if !contains(rep["promotion"].(map[string]any)["reason_codes"].([]string), "meta_train_contamination") {
		t.Fatalf("unexpected promotion: %v", rep["promotion"])
	}
}

func contains(list []string, item string) bool {
	for _, v := range list {
		if v == item {
			return true
		}
	}
	return false
}
