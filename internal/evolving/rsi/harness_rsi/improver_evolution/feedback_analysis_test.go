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
