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
