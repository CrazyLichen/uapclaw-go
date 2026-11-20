package improver_evolution

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 构造合法 cohort 的最小骨架，测试中在其上覆盖字段制造非法用例
func validCohortFixture() map[string]any {
	return map[string]any{
		"schema_version": 1,
		"record_type":    "candidate_feedback_cohort",
		"cohort": map[string]any{
			"cohort_id":       "c1",
			"candidate_count": 1,
		},
		"candidates": []any{
			validCandidateFixture(),
		},
		"metrics": map[string]any{
			"best_of_k_gain":   map[string]any{"status": "unavailable", "value": nil, "reason": "r"},
			"top_m_gain":       map[string]any{"status": "unavailable", "value": nil, "reason": "r"},
			"selection_regret": map[string]any{"status": "unavailable", "value": nil, "reason": "r"},
		},
	}
}

func validCandidateFixture() map[string]any {
	return map[string]any{
		"candidate_id":          "cand_1",
		"candidate_fingerprint": "fp_1",
		"identity": map[string]any{
			"parent_harness_ref":  "h1",
			"source_eval_ref":     "e1",
			"evaluation_protocol": "p1",
			"target_case_ids":     []any{"case_1"},
		},
		"proposal_rank": map[string]any{
			"rank_frozen":    true,
			"ranking_policy": "score_desc",
		},
		"outcome": map[string]any{"target_gain": 0.1},
		"activation": map[string]any{
			"availability": "observed",
			"state":        "triggered",
			"surfaces":     []any{"skill_search"},
		},
		"regression": map[string]any{
			"target": map[string]any{"availability": "observed", "regressed_case_count": 0},
		},
	}
}

func assertErrMsg(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error %q, got nil", want)
	}
	if err.Error() != want {
		t.Fatalf("error mismatch\n want: %s\n  got: %s", want, err.Error())
	}
}

// assertCohortErrMsg 校验 validateCohort 的错误消息逐字

func assertCandidateErrMsg(t *testing.T, candidate map[string]any, location, want string) {
	t.Helper()
	_, err := validateCandidate(candidate, location)
	assertErrMsg(t, err, want)
}

func assertCohortErrMsg(t *testing.T, cohort map[string]any, location, want string) {
	t.Helper()
	_, err := validateCohort(cohort, location)
	assertErrMsg(t, err, want)
}

// cloneMap 测试用浅拷贝
func cloneMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func TestLoadSource_路径不存在返回逐字错误(t *testing.T) {
	_, err := loadSource("/nonexistent/ledger.yaml")
	assertErrMsg(t, err, "candidate feedback ledger does not exist: /nonexistent/ledger.yaml")
}

func TestLoadSource_目录路径等同不存在(t *testing.T) {
	dir := t.TempDir()
	_, err := loadSource(dir)
	assertErrMsg(t, err, "candidate feedback ledger does not exist: "+dir)
}

func TestLoadSource_读取失败包装逐字错误(t *testing.T) {
	// 无读权限模拟：传一个不存在文件的中间目录不可行，改用坏 JSON 内容
	p := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(p, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := loadSource(p)
	if err == nil || !strings.HasPrefix(err.Error(), "failed to read candidate feedback ledger "+p+": ") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadSource_JSON后缀数字保留整数语义(t *testing.T) {
	p := filepath.Join(t.TempDir(), "led.JSON") // casefold 比较
	if err := os.WriteFile(p, []byte(`{"schema_version":1,"n":1.5,"i":2}`), 0o644); err != nil {
		t.Fatal(err)
	}
	v, err := loadSource(p)
	if err != nil {
		t.Fatal(err)
	}
	m := v.(map[string]any)
	if _, ok := m["schema_version"].(int64); !ok {
		t.Fatalf("JSON 整数应还原为 int64，got %T", m["schema_version"])
	}
	if _, ok := m["n"].(float64); !ok {
		t.Fatalf("JSON 浮点应为 float64，got %T", m["n"])
	}
	if _, ok := m["i"].(int64); !ok {
		t.Fatalf("JSON 整数应还原为 int64，got %T", m["i"])
	}
}

func TestLoadSource_YAML解析(t *testing.T) {
	p := filepath.Join(t.TempDir(), "led.yaml")
	if err := os.WriteFile(p, []byte("schema_version: 1\nk: v\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	v, err := loadSource(p)
	if err != nil {
		t.Fatal(err)
	}
	m := v.(map[string]any)
	if m["k"] != "v" {
		t.Fatalf("unexpected: %v", m)
	}
}

func TestNormalizeSources_元素类型非法(t *testing.T) {
	_, err := normalizeSources([]LedgerSource{123})
	assertErrMsg(t, err, "each source must be a ledger mapping or path")
}

func TestLoadAndValidateSource_cohort单记录形态(t *testing.T) {
	cohort := validCohortFixture()
	cohorts, err := loadAndValidateSource(cohort, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(cohorts) != 1 || cohorts[0]["record_type"] != "candidate_feedback_cohort" {
		t.Fatalf("unexpected: %v", cohorts)
	}
}

func TestLoadAndValidateSource_ledger形态校验错误逐字(t *testing.T) {
	base := map[string]any{"schema_version": 1, "ledger_type": "single_harness_sibling_candidate_feedback", "cohorts": []any{}}

	// schema_version 错
	v1 := cloneMap(base)
	v1["schema_version"] = 2
	_, err := loadAndValidateSource(v1, 0)
	assertErrMsg(t, err, "source[0].schema_version must be 1, got 2")

	// ledger_type 错
	v2 := cloneMap(base)
	v2["ledger_type"] = "wrong"
	_, err = loadAndValidateSource(v2, 0)
	assertErrMsg(t, err, "source[0].ledger_type must be 'single_harness_sibling_candidate_feedback', got 'wrong'")

	// cohorts 非 list
	v3 := cloneMap(base)
	v3["cohorts"] = "x"
	_, err = loadAndValidateSource(v3, 0)
	assertErrMsg(t, err, "source[0].cohorts must be a list")
}

func TestLoadAndValidateSource_非映射入参(t *testing.T) {
	_, err := loadAndValidateSource(123, 0)
	assertErrMsg(t, err, "sources must be a ledger mapping, path, or sequence of them")
}

func TestValidateCohort_错误分支逐字(t *testing.T) {
	loc := "s.cohorts[0]"

	// schema_version 错
	c := validCohortFixture()
	c["schema_version"] = 2
	assertCohortErrMsg(t, c, loc, loc+".schema_version must be 1, got 2")

	// record_type 错
	c = validCohortFixture()
	c["record_type"] = "wrong"
	assertCohortErrMsg(t, c, loc, loc+".record_type must be 'candidate_feedback_cohort', got 'wrong'")

	// cohort 非 mapping
	c = validCohortFixture()
	c["cohort"] = "x"
	assertCohortErrMsg(t, c, loc, loc+".cohort must be a mapping")

	// cohort_id 空
	c = validCohortFixture()
	c["cohort"].(map[string]any)["cohort_id"] = "  "
	assertCohortErrMsg(t, c, loc, loc+".cohort.cohort_id must be a non-empty string")

	// candidates 非 list
	c = validCohortFixture()
	c["candidates"] = "x"
	assertCohortErrMsg(t, c, loc, loc+".candidates must be a list")

	// candidate_count 不匹配
	c = validCohortFixture()
	c["cohort"].(map[string]any)["candidate_count"] = 2
	assertCohortErrMsg(t, c, loc, loc+".cohort.candidate_count must match candidates length")

	// metrics 非 mapping
	c = validCohortFixture()
	c["metrics"] = "x"
	c["cohort"].(map[string]any)["candidate_count"] = 1
	assertCohortErrMsg(t, c, loc, loc+".metrics must be a mapping")

	// metric 缺失
	c = validCohortFixture()
	c["cohort"].(map[string]any)["candidate_count"] = 1
	c["metrics"].(map[string]any)["best_of_k_gain"] = nil
	// Python metrics.get(metric_name) 为 None → require_mapping 报错
	assertCohortErrMsg(t, c, loc, loc+".metrics.best_of_k_gain must be a mapping")

	// status 非法
	c = validCohortFixture()
	c["cohort"].(map[string]any)["candidate_count"] = 1
	c["metrics"].(map[string]any)["best_of_k_gain"] = map[string]any{"status": "weird"}
	assertCohortErrMsg(t, c, loc, loc+".metrics.best_of_k_gain.status has unsupported value 'weird'")

	// available 但 value 非有限数
	c = validCohortFixture()
	c["cohort"].(map[string]any)["candidate_count"] = 1
	c["metrics"].(map[string]any)["best_of_k_gain"] = map[string]any{"status": "available", "value": "x"}
	assertCohortErrMsg(t, c, loc, loc+".metrics.best_of_k_gain.value must be finite when available")

	// unavailable 但 value 非 null
	c = validCohortFixture()
	c["cohort"].(map[string]any)["candidate_count"] = 1
	c["metrics"].(map[string]any)["best_of_k_gain"] = map[string]any{"status": "unavailable", "value": 1.0, "reason": "r"}
	assertCohortErrMsg(t, c, loc, loc+".metrics.best_of_k_gain.value must be null when unavailable")

	// unavailable 且 reason 空
	c = validCohortFixture()
	c["cohort"].(map[string]any)["candidate_count"] = 1
	c["metrics"].(map[string]any)["best_of_k_gain"] = map[string]any{"status": "unavailable", "value": nil, "reason": " "}
	assertCohortErrMsg(t, c, loc, loc+".metrics.best_of_k_gain.reason must be a non-empty string")
}

func TestValidateCandidate_错误分支逐字(t *testing.T) {
	loc := "s.cohorts[0].candidates[0]"

	// candidate_id 空
	c := validCandidateFixture()
	c["candidate_id"] = ""
	assertCandidateErrMsg(t, c, loc, loc+".candidate_id must be a non-empty string")

	// fingerprint 空
	c = validCandidateFixture()
	c["candidate_fingerprint"] = nil
	assertCandidateErrMsg(t, c, loc, loc+".candidate_fingerprint must be a non-empty string")

	// identity 非 mapping
	c = validCandidateFixture()
	c["identity"] = 1
	assertCandidateErrMsg(t, c, loc, loc+".identity must be a mapping")

	// identity 字段空
	for _, field := range []string{"parent_harness_ref", "source_eval_ref", "evaluation_protocol"} {
		c = validCandidateFixture()
		c["identity"].(map[string]any)[field] = ""
		assertCandidateErrMsg(t, c, loc, loc+".identity."+field+" must be a non-empty string")
	}

	// target_case_ids 非 list
	c = validCandidateFixture()
	c["identity"].(map[string]any)["target_case_ids"] = "x"
	assertCandidateErrMsg(t, c, loc, loc+".identity.target_case_ids must be a list")

	// rank 非 mapping
	c = validCandidateFixture()
	c["proposal_rank"] = nil
	assertCandidateErrMsg(t, c, loc, loc+".proposal_rank must be a mapping")

	// rank_frozen 非 bool
	c = validCandidateFixture()
	c["proposal_rank"].(map[string]any)["rank_frozen"] = 1
	assertCandidateErrMsg(t, c, loc, loc+".proposal_rank.rank_frozen must be a boolean")

	// ranking_policy 空
	c = validCandidateFixture()
	c["proposal_rank"].(map[string]any)["ranking_policy"] = ""
	assertCandidateErrMsg(t, c, loc, loc+".proposal_rank.ranking_policy must be a non-empty string")

	// ranking_features 非 mapping
	c = validCandidateFixture()
	c["proposal_rank"].(map[string]any)["ranking_features"] = []any{1}
	assertCandidateErrMsg(t, c, loc, loc+".proposal_rank.ranking_features must be a mapping when present")

	// outcome 非 mapping
	c = validCandidateFixture()
	c["outcome"] = 3
	assertCandidateErrMsg(t, c, loc, loc+".outcome must be a mapping")

	// target_gain 非数
	c = validCandidateFixture()
	c["outcome"].(map[string]any)["target_gain"] = "x"
	assertCandidateErrMsg(t, c, loc, loc+".outcome.target_gain must be finite or null")

	// activation 非 mapping
	c = validCandidateFixture()
	c["activation"] = "x"
	assertCandidateErrMsg(t, c, loc, loc+".activation must be a mapping")

	// availability 非法
	c = validCandidateFixture()
	c["activation"].(map[string]any)["availability"] = "weird"
	assertCandidateErrMsg(t, c, loc, loc+".activation.availability has unsupported value 'weird'")

	// state 非法
	c = validCandidateFixture()
	c["activation"].(map[string]any)["state"] = "weird"
	assertCandidateErrMsg(t, c, loc, loc+".activation.state has unsupported value 'weird'")

	// observed 但 state 非 triggered/not_triggered
	c = validCandidateFixture()
	c["activation"].(map[string]any)["state"] = "not_instrumented"
	assertCandidateErrMsg(t, c, loc, loc+".activation.state must be triggered or not_triggered when observed")

	// 非 observed 但 state 未保留
	c = validCandidateFixture()
	c["activation"].(map[string]any)["availability"] = "missing_artifact"
	assertCandidateErrMsg(t, c, loc, loc+".activation.state must preserve unavailable state 'missing_artifact'")

	// surfaces 非 list
	c = validCandidateFixture()
	c["activation"].(map[string]any)["surfaces"] = "x"
	assertCandidateErrMsg(t, c, loc, loc+".activation.surfaces must be a list of non-empty strings")

	// surfaces 含空串
	c = validCandidateFixture()
	c["activation"].(map[string]any)["surfaces"] = []any{""}
	assertCandidateErrMsg(t, c, loc, loc+".activation.surfaces must be a list of non-empty strings")

	// regression 非 mapping
	c = validCandidateFixture()
	c["regression"] = "x"
	assertCandidateErrMsg(t, c, loc, loc+".regression must be a mapping")

	// target 非 mapping
	c = validCandidateFixture()
	c["regression"].(map[string]any)["target"] = "x"
	assertCandidateErrMsg(t, c, loc, loc+".regression.target must be a mapping")

	// target availability 非法
	c = validCandidateFixture()
	c["regression"].(map[string]any)["target"] = map[string]any{"availability": "weird"}
	assertCandidateErrMsg(t, c, loc, loc+".regression.target.availability has unsupported value 'weird'")

	// observed 但 count 非负整数
	c = validCandidateFixture()
	c["regression"].(map[string]any)["target"] = map[string]any{"availability": "observed", "regressed_case_count": -1}
	assertCandidateErrMsg(t, c, loc, loc+".regression.target.regressed_case_count must be non-negative")

	// observed 但 count 是 bool
	c = validCandidateFixture()
	c["regression"].(map[string]any)["target"] = map[string]any{"availability": "observed", "regressed_case_count": true}
	assertCandidateErrMsg(t, c, loc, loc+".regression.target.regressed_case_count must be non-negative")

	// 非 observed 但 count 非 null
	c = validCandidateFixture()
	c["regression"].(map[string]any)["target"] = map[string]any{"availability": "not_evaluated", "regressed_case_count": 1}
	assertCandidateErrMsg(t, c, loc, loc+".regression.target.regressed_case_count must be null when unavailable")
}

func TestValidateCohort_候选校验带位置前缀(t *testing.T) {
	c := validCohortFixture()
	c["cohort"].(map[string]any)["candidate_count"] = 1
	c["candidates"].([]any)[0].(map[string]any)["candidate_id"] = ""
	assertCohortErrMsg(t, c, "s.cohorts[0]",
		"s.cohorts[0].candidates[0].candidate_id must be a non-empty string")
}

func TestIsNumber_排除布尔与非有限数(t *testing.T) {
	if isNumber(true) || isNumber("1") || isNumber(nil) {
		t.Fatal("bool/string/nil 不应是数字")
	}
	if !isNumber(1) || !isNumber(1.5) || !isNumber(int64(3)) {
		t.Fatal("int/int64/float64 应是数字")
	}
	if isNumber(math.Inf(1)) || isNumber(math.Inf(-1)) {
		t.Fatal("Inf 不是有限数")
	}
}

func TestIsNonnegativeInt_排除布尔(t *testing.T) {
	if !isNonnegativeInt(0) || !isNonnegativeInt(3) || !isNonnegativeInt(int64(2)) {
		t.Fatal("非负整数应通过")
	}
	if isNonnegativeInt(true) || isNonnegativeInt(-1) || isNonnegativeInt(1.0) {
		t.Fatal("bool/负数/浮点不应通过")
	}
}

func TestRequireNonemptyText_纯空白拒绝(t *testing.T) {
	if err := requireNonemptyText("ok", "loc"); err != nil {
		t.Fatal(err)
	}
	assertErrMsg(t, requireNonemptyText("  ", "loc"), "loc must be a non-empty string")
	assertErrMsg(t, requireNonemptyText(nil, "loc"), "loc must be a non-empty string")
	assertErrMsg(t, requireNonemptyText(1, "loc"), "loc must be a non-empty string")
}

func TestRequireExact_数值跨类型相等(t *testing.T) {
	// Python 1 == 1.0 为 True，requireExact 必须复刻数值跨类型相等
	if err := requireExact(1.0, 1, "loc"); err != nil {
		t.Fatalf("float 1.0 应等于 int 1: %v", err)
	}
	assertErrMsg(t, requireExact(2, 1, "loc"), "loc must be 1, got 2")
}

// ─────────────── Task 2：证据收集器与可比性判定 ───────────────

// comparableCohortFixture 构造可通过 _comparability_reason 的最小 cohort（2 候选）
func comparableCohortFixture() map[string]any {
	cohortIdentity := map[string]any{
		"cohort_id":           "cc1",
		"candidate_count":     2,
		"parent_harness_ref":  "h1",
		"source_eval_ref":     "e1",
		"evaluation_protocol": "p1",
		"target_case_ids":     []any{"case_1"},
		"rank_frozen":         true,
		"ranking_policy":      "score_desc",
	}
	makeCandidate := func(id string, rank int, gain float64) map[string]any {
		c := validCandidateFixture()
		c["candidate_id"] = id
		c["identity"] = map[string]any{
			"parent_harness_ref":  "h1",
			"source_eval_ref":     "e1",
			"evaluation_protocol": "p1",
			"target_case_ids":     []any{"case_1"},
		}
		c["proposal_rank"] = map[string]any{
			"predicted_rank":  rank,
			"predicted_score": 0.9 - 0.1*float64(rank),
			"rank_frozen":     true,
			"ranking_policy":  "score_desc",
		}
		c["outcome"] = map[string]any{"target_gain": gain}
		return c
	}
	return map[string]any{
		"schema_version": 1,
		"record_type":    "candidate_feedback_cohort",
		"cohort":         cohortIdentity,
		"candidates":     []any{makeCandidate("a", 1, 0.2), makeCandidate("b", 2, 0.1)},
		"metrics": map[string]any{
			"best_of_k_gain":   map[string]any{"status": "available", "value": 0.2, "candidate_id": "a"},
			"top_m_gain":       map[string]any{"status": "available", "value": 0.2, "effective_m": 2},
			"selection_regret": map[string]any{"status": "unavailable", "value": nil, "reason": "r"},
		},
	}
}

func TestSurfaceEvidence_触发观察三态计数(t *testing.T) {
	e := newSurfaceEvidence()
	// observed + triggered → 机会
	e.observeTrigger("c1", map[string]any{"availability": "observed", "state": "triggered"})
	// observed + not_triggered → 机会+失败
	e.observeTrigger("c2", map[string]any{"availability": "observed", "state": "not_triggered"})
	// 非 observed → 只计 availability
	e.observeTrigger("c3", map[string]any{"availability": "not_instrumented", "state": "not_instrumented"})
	got := e.triggerSummary()
	if got["observed_opportunity_candidate_count"] != 2 || got["failure_candidate_count"] != 1 {
		t.Fatalf("unexpected: %v", got)
	}
	if got["opportunity_cohort_count"] != 2 || got["support_cohort_count"] != 1 {
		t.Fatalf("unexpected: %v", got)
	}
	ids := got["evidence_cohort_ids"].([]string)
	if len(ids) != 1 || ids[0] != "c2" {
		t.Fatalf("unexpected ids: %v", ids)
	}
	av := got["availability_counts"].(map[string]int)
	if av["observed"] != 2 || av["not_instrumented"] != 1 {
		t.Fatalf("unexpected availability: %v", av)
	}
}

func TestSurfaceEvidence_回归观察计数(t *testing.T) {
	e := newSurfaceEvidence()
	e.observeRegression("c1", map[string]any{"availability": "observed", "regressed_case_count": 2})
	e.observeRegression("c2", map[string]any{"availability": "observed", "regressed_case_count": 0})
	e.observeRegression("c3", map[string]any{"availability": "not_evaluated"})
	got := e.regressionSummary()
	if got["observed_opportunity_candidate_count"] != 2 || got["regression_candidate_count"] != 1 {
		t.Fatalf("unexpected: %v", got)
	}
	if got["rate"] != 0.5 {
		t.Fatalf("rate: %v", got["rate"])
	}
}

func TestSurfaceEvidence_空观察速率为nil(t *testing.T) {
	e := newSurfaceEvidence()
	got := e.triggerSummary()
	if got["rate"] != nil {
		t.Fatalf("分母为0时 rate 应为 nil: %v", got["rate"])
	}
}

func TestFeatureEvidence_观察与汇总(t *testing.T) {
	e := newFeatureEvidence()
	e.observe("c1", true, "")
	e.observe("c2", false, "increase")
	e.observe("c3", false, "decrease")
	s := e.summary()
	if s["aligned_pair_count"] != 1 || s["opposite_pair_count"] != 2 {
		t.Fatalf("unexpected: %v", s)
	}
	inc := s["increase_weight_evidence"].(map[string]any)
	dec := s["decrease_weight_evidence"].(map[string]any)
	if inc["pair_count"] != 1 || dec["pair_count"] != 1 {
		t.Fatalf("unexpected: %v %v", inc, dec)
	}
	if s["comparable_pair_count"] != 3 {
		t.Fatalf("unexpected: %v", s)
	}
}

func TestComparabilityReason_全部原因逐字(t *testing.T) {
	// 基线：可比较 → ""
	base := comparableCohortFixture()
	if reason := comparabilityReason(base); reason != "" {
		t.Fatalf("基线应可比较，got %q", reason)
	}

	// 单候选
	c := comparableCohortFixture()
	c["candidates"] = c["candidates"].([]any)[:1]
	c["cohort"].(map[string]any)["candidate_count"] = 1
	if reason := comparabilityReason(c); reason != "requires_at_least_two_comparable_candidates" {
		t.Fatalf("got %q", reason)
	}

	// cohort_identity_missing
	c = comparableCohortFixture()
	c["cohort"].(map[string]any)["parent_harness_ref"] = ""
	if reason := comparabilityReason(c); reason != "cohort_identity_missing" {
		t.Fatalf("got %q", reason)
	}

	// rank_not_frozen_before_evaluation
	c = comparableCohortFixture()
	c["cohort"].(map[string]any)["rank_frozen"] = false
	if reason := comparabilityReason(c); reason != "rank_not_frozen_before_evaluation" {
		t.Fatalf("got %q", reason)
	}

	// duplicate_candidate_id
	c = comparableCohortFixture()
	c["candidates"].([]any)[1].(map[string]any)["candidate_id"] = "a"
	if reason := comparabilityReason(c); reason != "duplicate_candidate_id" {
		t.Fatalf("got %q", reason)
	}

	// predicted_rank_missing
	c = comparableCohortFixture()
	c["candidates"].([]any)[0].(map[string]any)["proposal_rank"].(map[string]any)["predicted_rank"] = 1.0
	if reason := comparabilityReason(c); reason != "predicted_rank_missing" {
		t.Fatalf("got %q", reason)
	}

	// predicted_ranks_not_permutation
	c = comparableCohortFixture()
	c["candidates"].([]any)[1].(map[string]any)["proposal_rank"].(map[string]any)["predicted_rank"] = 1
	if reason := comparabilityReason(c); reason != "predicted_ranks_not_permutation" {
		t.Fatalf("got %q", reason)
	}

	// identity 字段不匹配
	c = comparableCohortFixture()
	c["candidates"].([]any)[0].(map[string]any)["identity"].(map[string]any)["parent_harness_ref"] = "h2"
	if reason := comparabilityReason(c); reason != "parent_harness_ref_mismatch" {
		t.Fatalf("got %q", reason)
	}

	// target_case_set_mismatch
	c = comparableCohortFixture()
	c["candidates"].([]any)[0].(map[string]any)["identity"].(map[string]any)["target_case_ids"] = []any{"case_9"}
	if reason := comparabilityReason(c); reason != "target_case_set_mismatch" {
		t.Fatalf("got %q", reason)
	}

	// candidate_rank_not_frozen
	c = comparableCohortFixture()
	c["candidates"].([]any)[0].(map[string]any)["proposal_rank"].(map[string]any)["rank_frozen"] = false
	if reason := comparabilityReason(c); reason != "candidate_rank_not_frozen" {
		t.Fatalf("got %q", reason)
	}

	// candidate_ranking_policy_mismatch
	c = comparableCohortFixture()
	c["candidates"].([]any)[0].(map[string]any)["proposal_rank"].(map[string]any)["ranking_policy"] = "other"
	if reason := comparabilityReason(c); reason != "candidate_ranking_policy_mismatch" {
		t.Fatalf("got %q", reason)
	}

	// target_gain_missing
	c = comparableCohortFixture()
	c["candidates"].([]any)[0].(map[string]any)["outcome"].(map[string]any)["target_gain"] = nil
	if reason := comparabilityReason(c); reason != "target_gain_missing" {
		t.Fatalf("got %q", reason)
	}

	// search_metric_unavailable
	c = comparableCohortFixture()
	c["metrics"].(map[string]any)["best_of_k_gain"] = map[string]any{"status": "unavailable", "value": nil, "reason": "r"}
	if reason := comparabilityReason(c); reason != "search_metric_unavailable" {
		t.Fatalf("got %q", reason)
	}
}

func TestCohortDuplicateCounts(t *testing.T) {
	c := comparableCohortFixture()
	c["candidates"].([]any)[1].(map[string]any)["candidate_fingerprint"] =
		c["candidates"].([]any)[0].(map[string]any)["candidate_fingerprint"]
	total, dup := cohortDuplicateCounts(c)
	if total != 2 || dup != 1 {
		t.Fatalf("got %d %d", total, dup)
	}
}

func TestCollectFeatureEvidence_组合枚举与对齐判定(t *testing.T) {
	c := comparableCohortFixture()
	cands := c["candidates"].([]any)
	// a: rank1 score0.8 gain0.2 size2.0；b: rank2 score0.7 gain0.1 size1.0
	// 预测赢家 a（0.8>0.7），实际赢家 a（0.2>0.1）→ aligned
	fe := map[string]*featureEvidence{}
	cands[0].(map[string]any)["proposal_rank"].(map[string]any)["ranking_features"] = map[string]any{"size": 2.0}
	cands[1].(map[string]any)["proposal_rank"].(map[string]any)["ranking_features"] = map[string]any{"size": 1.0}
	collectFeatureEvidence(c, fe)
	if e, ok := fe["size"]; !ok || e.alignedPairCount != 1 {
		t.Fatalf("unexpected: %v", fe)
	}

	// 反向：实际赢家是 b → misaligned，higher_feature_predicted=True → decrease
	c["candidates"].([]any)[1].(map[string]any)["outcome"].(map[string]any)["target_gain"] = 0.3
	fe = map[string]*featureEvidence{}
	collectFeatureEvidence(c, fe)
	if e := fe["size"]; e.oppositePairCount != 1 || e.decreasePairCount != 1 {
		t.Fatalf("unexpected: %v", e.summary())
	}

	// 相同增益 → 跳过
	c["candidates"].([]any)[1].(map[string]any)["outcome"].(map[string]any)["target_gain"] = 0.2
	fe = map[string]*featureEvidence{}
	collectFeatureEvidence(c, fe)
	if len(fe) != 0 {
		t.Fatalf("相同增益应跳过: %v", fe)
	}
}

func TestCollectFeatureEvidence_多个差异特征跳过(t *testing.T) {
	c := comparableCohortFixture()
	cands := c["candidates"].([]any)
	cands[0].(map[string]any)["proposal_rank"].(map[string]any)["ranking_features"] =
		map[string]any{"size": 2.0, "depth": 1.0}
	cands[1].(map[string]any)["proposal_rank"].(map[string]any)["ranking_features"] =
		map[string]any{"size": 1.0, "depth": 2.0}
	fe := map[string]*featureEvidence{}
	collectFeatureEvidence(c, fe)
	if len(fe) != 0 {
		t.Fatalf("两个差异特征应跳过: %v", fe)
	}
}

func TestNumericRankingFeatures_排除布尔与非数(t *testing.T) {
	c := map[string]any{
		"proposal_rank": map[string]any{
			"ranking_features": map[string]any{"a": 1.5, "b": true, "c": "x", "d": 2},
		},
	}
	got := numericRankingFeatures(c)
	if len(got) != 2 || got["a"] != 1.5 || got["d"] != 2.0 {
		t.Fatalf("unexpected: %v", got)
	}
}

func TestCollectHighValueOutsideTopM_全分支(t *testing.T) {
	// best ≤ threshold → 跳过
	c := comparableCohortFixture()
	var opp, sup []string
	if err := collectHighValueOutsideTopM(c, 0.3, &opp, &sup); err != nil {
		t.Fatal(err)
	}
	if len(opp) != 0 || len(sup) != 0 {
		t.Fatalf("best低于阈值应跳过")
	}

	// best > threshold 且 best 候选 rank > effective_m → 支持
	c = comparableCohortFixture()
	c["metrics"].(map[string]any)["best_of_k_gain"].(map[string]any)["candidate_id"] = "b"
	c["candidates"].([]any)[1].(map[string]any)["outcome"].(map[string]any)["target_gain"] = 0.3
	c["metrics"].(map[string]any)["top_m_gain"].(map[string]any)["effective_m"] = 1
	if err := collectHighValueOutsideTopM(c, 0.1, &opp, &sup); err != nil {
		t.Fatal(err)
	}
	if len(opp) != 1 || len(sup) != 1 || sup[0] != "cc1" {
		t.Fatalf("unexpected: %v %v", opp, sup)
	}

	// best 候选不在 candidates → 逐字错误
	c = comparableCohortFixture()
	c["metrics"].(map[string]any)["best_of_k_gain"].(map[string]any)["candidate_id"] = "ghost"
	err := collectHighValueOutsideTopM(c, 0.0, &opp, &sup)
	assertErrMsg(t, err, "cohort 'cc1' best_of_k_gain candidate_id is not in candidates")

	// effective_m 非法 → 逐字错误
	c = comparableCohortFixture()
	c["metrics"].(map[string]any)["top_m_gain"].(map[string]any)["effective_m"] = 0
	err = collectHighValueOutsideTopM(c, 0.0, &opp, &sup)
	assertErrMsg(t, err, "cohort 'cc1' top_m_gain.effective_m must be a positive integer")
}

// ─────────────── Task 3：稳定模式枚举 ───────────────

func TestAppendPattern_低于支持门槛不入列(t *testing.T) {
	patterns := []map[string]any{}
	appendPattern(&patterns, 2, "pid", "ptype", nil, []string{"c1"}, []string{"c1", "c2"},
		map[string]any{"field": "f", "operation": "set", "value": true, "rationale": "r"})
	if len(patterns) != 0 {
		t.Fatalf("support=1 < min=2 应过滤")
	}
	appendPattern(&patterns, 1, "pid", "ptype", nil, []string{"c2", "c1", "c1"}, []string{"c3", "c1"},
		map[string]any{"field": "f", "operation": "set", "value": true, "rationale": "r"})
	if len(patterns) != 1 {
		t.Fatalf("support=2 >= min=1 应入列")
	}
	p := patterns[0]
	if p["pattern_id"] != "pid" || p["support_cohorts"] != 2 || p["opportunity_cohorts"] != 2 {
		t.Fatalf("unexpected: %v", p)
	}
	if p["rate"] != 1.0 {
		t.Fatalf("rate: %v", p["rate"])
	}
	ids := p["evidence_cohort_ids"].([]string)
	if len(ids) != 2 || ids[0] != "c1" || ids[1] != "c2" {
		t.Fatalf("去重排序: %v", ids)
	}
}

func TestStablePatterns_固定四模式与surface模式(t *testing.T) {
	trigger := newSurfaceEvidence()
	trigger.triggerFailureCohorts["c1"] = true
	trigger.triggerOpportunityCohorts["c1"] = true
	regression := newSurfaceEvidence()
	regression.regressionFailureCohorts["c2"] = true
	regression.regressionOpportunityCohorts["c2"] = true

	patterns := stablePatterns(
		1,
		[]string{"c1"}, []string{"c1"}, []string{"c1"}, []string{"c1"},
		[]string{"c1"}, []string{"c1"},
		map[string]*surfaceEvidence{"s1": trigger},
		map[string]*surfaceEvidence{"s1": regression},
		map[string]*featureEvidence{},
		[]string{}, []string{"c1"}, []string{"c1"},
	)
	ids := make([]string, 0, len(patterns))
	for _, p := range patterns {
		ids = append(ids, p["pattern_id"].(string))
	}
	want := []string{
		"all_candidates_nonpositive",
		"partial_candidate_has_residual_requirements",
		"high_value_candidate_outside_top_m",
		"duplicate_candidates",
		"trigger_observed_failure:s1",
		"target_regression:s1",
	}
	if strings.Join(ids, ",") != strings.Join(want, ",") {
		t.Fatalf("顺序应照抄 Python:\n want: %v\n  got: %v", want, ids)
	}
	// 固定模式 policy_change 抽查
	p0 := patterns[0]
	pc := p0["recommended_policy_change"].(map[string]any)
	if pc["field"] != "generation_directives.require_distinct_intervention_surfaces" ||
		pc["operation"] != "set" || pc["value"] != true {
		t.Fatalf("unexpected: %v", pc)
	}
	// surface 模式字段名拼接与 surface 值
	p4 := patterns[4]
	if p4["surface"] != "s1" {
		t.Fatalf("unexpected surface: %v", p4)
	}
	pc4 := p4["recommended_policy_change"].(map[string]any)
	if pc4["field"] != "generation_directives.require_activation_evidence.s1" {
		t.Fatalf("unexpected: %v", pc4)
	}
}

func TestStablePatterns_排序错位方向判定(t *testing.T) {
	fe := newFeatureEvidence()
	fe.comparableCohorts["c1"] = true
	fe.comparableCohorts["c2"] = true
	fe.comparableCohorts["c3"] = true
	fe.increaseCohorts["c1"] = true
	fe.increaseCohorts["c3"] = true
	fe.increaseCohorts["c2"] = true
	fe.decreaseCohorts["c3"] = true
	patterns := stablePatterns(
		1, []string{"c1", "c2"}, []string{}, []string{}, []string{},
		[]string{}, []string{},
		map[string]*surfaceEvidence{}, map[string]*surfaceEvidence{},
		map[string]*featureEvidence{"w": fe},
		[]string{"c1", "c2"}, []string{}, []string{},
	)
	if len(patterns) != 1 {
		t.Fatalf("increase/decrease 支持数不等应产出 1 条: %v", patterns)
	}
	p := patterns[0]
	if p["pattern_id"] != "ranking_misalignment:w:increase" {
		t.Fatalf("支持数相等时 increase 侧胜出: %v", p)
	}
	pc := p["recommended_policy_change"].(map[string]any)
	if pc["field"] != "ranking_weights.w" || pc["operation"] != "increase" || pc["value"] != 0.1 {
		t.Fatalf("unexpected: %v", pc)
	}
}

func TestStablePatterns_支持数相等跳过(t *testing.T) {
	fe := newFeatureEvidence()
	fe.increaseCohorts["c1"] = true
	fe.decreaseCohorts["c2"] = true
	patterns := stablePatterns(
		1, []string{"c1", "c2"}, []string{}, []string{}, []string{},
		[]string{}, []string{},
		map[string]*surfaceEvidence{}, map[string]*surfaceEvidence{},
		map[string]*featureEvidence{"w": fe},
		[]string{"c1", "c2"}, []string{}, []string{},
	)
	if len(patterns) != 0 {
		t.Fatalf("支持数相等应跳过: %v", patterns)
	}
}

func TestCohortRate_结构与排序(t *testing.T) {
	got := cohortRate([]string{"b", "a", "b"}, []string{"x", "a"})
	if got["support_cohort_count"] != 2 || got["opportunity_cohort_count"] != 2 || got["rate"] != 1.0 {
		t.Fatalf("unexpected: %v", got)
	}
	ids := got["evidence_cohort_ids"].([]string)
	if ids[0] != "a" || ids[1] != "b" {
		t.Fatalf("排序: %v", ids)
	}
	if cohortRate([]string{}, []string{})["rate"] != nil {
		t.Fatal("分母 0 应为 nil")
	}
}
