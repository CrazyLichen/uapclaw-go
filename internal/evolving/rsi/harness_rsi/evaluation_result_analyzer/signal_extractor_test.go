package evaluationresultanalyzer

import (
	"strings"
	"testing"
)

// TestFingerprintError_归一化与顺序敏感 对照 Python _fingerprint_error 五条正则与顺序。
// Python: signal_extractor.py:24-41
func TestFingerprintError_归一化与顺序敏感(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"时间戳变体1", "crash at 2026-11-15T08:30:00.123Z inside", "crash at <ts> inside"},
		{"时间戳变体2", "crash at 2026-11-15 08:30:00+08:00 inside", "crash at <ts> inside"},
		{"UUID", "id 550e8400-e29b-41d4-a716-446655440000 bad", "id <uuid> bad"},
		{"POSIX路径", "FileNotFoundError: /tmp/eval_8812/run_3/output.json missing", "FileNotFoundError: <path> missing"},
		{"Windows路径", "cannot open C:\\Users\\x\\foo.py now", "cannot open <path> now"},
		{"长十六进制", "checksum deadbeef1234 mismatch", "checksum <hex> mismatch"},
		{"短十六进制不替换", "value abcde kept", "value abcde kept"},
		{"行号", "error at :42 near", "error at :<N> near"},
		{"空白压缩", "a   b\t\nc", "a b c"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := fingerprintError(tc.in); got != tc.want {
				t.Fatalf("指纹不符:\n in=%q\nwant=%q\n got=%q", tc.in, tc.want, got)
			}
		})
	}

	// 顺序敏感：时间戳优先于 hex（时间戳内数字不被吞）
	ts := fingerprintError("at 2026-11-15T08:30:00Z")
	if !strings.Contains(ts, "<ts>") {
		t.Fatalf("时间戳应先于 hex 归一, got %q", ts)
	}
	if strings.Contains(ts, "<hex>") {
		t.Fatalf("时间戳数字不应被 hex 吞掉, got %q", ts)
	}

	// 聚类端到端：两变体报错 → 同一指纹
	e1 := "FileNotFoundError: /tmp/eval_8812/run_3/output.json not found (2026-11-15 09:12:33)"
	e2 := "FileNotFoundError: /tmp/eval_7731/run_9/output.json not found (2026-11-15 09:14:57)"
	if fingerprintError(e1) != fingerprintError(e2) {
		t.Fatal("结构相同的变体报错应聚合同一指纹")
	}
	if fingerprintError(e1) == fingerprintError("PermissionError: /tmp/x denied") {
		t.Fatal("不同错误内容不应错聚")
	}
}

// 构造测试案例的辅助函数。
func sigCase(id, status string, passed bool, errStr string) CaseAnalysisInput {
	return CaseAnalysisInput{CaseID: id, Status: status, EvaluationPassed: passed, Error: errStr}
}

// TestGenericSignalExtractor_分支对照 对照 Python GenericSignalExtractor.extract。
// Python: signal_extractor.py:52-113
func TestGenericSignalExtractor_分支对照(t *testing.T) {
	cases := []CaseAnalysisInput{
		sigCase("c1", "failed", false, "boom path /x/y.py"),
		sigCase("c2", "passed", false, ""), // judge fail
		{CaseID: "c3", Status: "passed", EvaluationPassed: true, Expected: strPtr("want"), Response: "got"},
		{CaseID: "c4", Status: "passed", EvaluationPassed: true, Expected: strPtr("same"), Response: "same"},
		{CaseID: "c5", Status: "passed", EvaluationPassed: true, Expected: nil, Response: "x"}, // missing ref
		sigCase("c6", "boom-crash", false, "boom path /x/y.py"),
	}
	got := GenericSignalExtractor{}.Extract(EvaluationSummaryInput{EvaluationMethod: "m"}, cases)

	if got.Method != "m" {
		t.Fatalf("method 应透传, got %q", got.Method)
	}
	// exec_failures: status=="failed" 仅 c1
	if len(got.ExecFailures) != 1 || got.ExecFailures[0] != "c1" {
		t.Fatalf("exec_failures 不符: %v", got.ExecFailures)
	}
	// judge_failures: 非 "failed" 且未过 → c2、c6（boom-crash 状态不等于 failed）
	if len(got.JudgeFailures) != 2 || got.JudgeFailures[0] != "c2" || got.JudgeFailures[1] != "c6" {
		t.Fatalf("judge_failures 不符: %v", got.JudgeFailures)
	}
	// 聚类：c1/c6 同指纹
	if len(got.ErrorClusters) != 1 {
		t.Fatalf("错误聚类应 1 组, got %v", got.ErrorClusters)
	}
	cluster := got.ErrorClusters[0]
	gotCases := cluster["cases"].([]string)
	if len(gotCases) != 2 || gotCases[0] != "c1" || gotCases[1] != "c6" {
		t.Fatalf("聚类成员不符: %v", gotCases)
	}
	ms := got.MethodSpecific
	if got2, _ := ms["expected_mismatch_cases"].([]string); len(got2) != 1 || got2[0] != "c3" {
		t.Fatalf("expected_mismatch_cases 不符: %v", ms["expected_mismatch_cases"])
	}
	// c2/c6 亦无 Expected（sigCase 未设置）→ 一并计入 missing_reference
	if got3, _ := ms["missing_reference_cases"].([]string); len(got3) != 3 || got3[0] != "c2" || got3[1] != "c5" || got3[2] != "c6" {
		t.Fatalf("missing_reference_cases 不符: %v", ms["missing_reference_cases"])
	}
}

// strPtr 返回字符串指针（测试辅助）。
func strPtr(s string) *string { return &s }

// TestPytestSignalExtractor_证据缺失降级 对照 Python PytestSignalExtractor.extract。
// Python: signal_extractor.py:121-180
func TestPytestSignalExtractor_证据缺失降级(t *testing.T) {
	summary := EvaluationSummaryInput{EvaluationMethod: "script_based"}

	// ① 有证据：按 test_name/assertion_type 聚类
	withEvidence := []CaseAnalysisInput{
		{CaseID: "p1", Status: "passed", EvaluationPassed: false,
			EvaluationMetadata: map[string]any{"evidence": []any{
				map[string]any{"test_name": "t_a", "assertion_type": "assert"},
				map[string]any{"test_name": "t_b", "assertion_type": "assert"},
			}}},
		{CaseID: "p2", Status: "passed", EvaluationPassed: false,
			EvaluationMetadata: map[string]any{"evidence": []any{
				map[string]any{"test_name": "t_a", "assertion_type": "raises"},
			}}},
	}
	got := PytestSignalExtractor{}.Extract(summary, withEvidence)
	ms := got.MethodSpecific
	if _, hasFallback := ms["fallback_reason"]; hasFallback {
		t.Fatalf("有证据不应降级: %v", ms)
	}
	ftc := ms["failed_test_clusters"].(map[string][]string)
	if len(ftc["t_a"]) != 2 || len(ftc["t_b"]) != 1 {
		t.Fatalf("failed_test_clusters 不符: %v", ftc)
	}
	atc := ms["assertion_type_clusters"].(map[string][]string)
	if len(atc["assert"]) != 2 || len(atc["raises"]) != 1 {
		t.Fatalf("assertion_type_clusters 不符: %v", atc)
	}

	// ② 证据缺失 → 回落 generic + pytest_evidence_missing
	noEvidence := []CaseAnalysisInput{
		{CaseID: "p3", Status: "passed", EvaluationPassed: false, EvaluationMetadata: map[string]any{}},
	}
	got2 := PytestSignalExtractor{}.Extract(summary, noEvidence)
	ms2 := got2.MethodSpecific
	if ms2["fallback_reason"] != "pytest_evidence_missing" {
		t.Fatalf("fallback_reason 不符: %v", ms2["fallback_reason"])
	}
	if got2.ExecFailures == nil && got2.JudgeFailures == nil {
		t.Fatal("回落应保留 generic 信号")
	}

	// ③ 混合：部分缺失 → 缺失案例进 evidence_missing_cases，整组走回落分支
	mixed := []CaseAnalysisInput{
		{CaseID: "p4", Status: "passed", EvaluationPassed: false, EvaluationMetadata: map[string]any{}},
		{CaseID: "p5", Status: "passed", EvaluationPassed: false,
			EvaluationMetadata: map[string]any{"evidence": []any{
				map[string]any{"test_name": "t_c", "assertion_type": "assert"},
			}}},
	}
	got3 := PytestSignalExtractor{}.Extract(summary, mixed)
	ms3 := got3.MethodSpecific
	if ms3["fallback_reason"] != "pytest_evidence_missing" {
		t.Fatalf("部分缺失也应降级: %v", ms3)
	}
	em := ms3["evidence_missing_cases"].([]string)
	if len(em) != 1 || em[0] != "p4" {
		t.Fatalf("evidence_missing_cases 不符: %v", em)
	}
}

// TestRewardSignalExtractor_信号对照 对照 Python RewardSignalExtractor.extract。
// Python: signal_extractor.py:193-253
func TestRewardSignalExtractor_信号对照(t *testing.T) {
	summary := EvaluationSummaryInput{EvaluationMethod: "reward_based"}
	cases := []CaseAnalysisInput{
		{CaseID: "r1", Status: "passed", EvaluationPassed: true,
			EvaluationMetadata: map[string]any{"reward": 0.0}},
		{CaseID: "r2", Status: "passed", EvaluationPassed: true,
			TerminationReason: "max_iterations",
			TrajectoryWindowSummary: map[string]any{
				"failure_signatures": []any{"sig-a", "sig-b"},
			}},
		{CaseID: "r3", Status: "passed", EvaluationPassed: true,
			EvaluationMetadata: map[string]any{"reward": 0.9}},
	}
	got := RewardSignalExtractor{}.Extract(summary, cases)
	ms := got.MethodSpecific
	if zr := ms["zero_reward_cases"].([]string); len(zr) != 1 || zr[0] != "r1" {
		t.Fatalf("zero_reward_cases 不符: %v", ms["zero_reward_cases"])
	}
	if mi := ms["max_iteration_cases"].([]string); len(mi) != 1 || mi[0] != "r2" {
		t.Fatalf("max_iteration_cases 不符: %v", ms["max_iteration_cases"])
	}
	// r1（zero）+ r2（max_iter + signatures）→ member harness 问题
	if mh := ms["member_harness_issue_cases"].([]string); len(mh) != 2 {
		t.Fatalf("member_harness_issue_cases 不符: %v", ms["member_harness_issue_cases"])
	}
	sigByCase := ms["trajectory_failure_signatures_by_case"].(map[string][]string)
	if len(sigByCase["r2"]) != 2 {
		t.Fatalf("failure signatures 不符: %v", sigByCase)
	}
	// generic method_specific 键保留（**generic 合并语义）
	if _, ok := ms["expected_mismatch_cases"]; !ok {
		t.Fatal("reward 方法应合并 generic method_specific")
	}
	// 响应文本含标记串也命中 max_iterations 分支
	cases2 := []CaseAnalysisInput{
		{CaseID: "r4", Status: "passed", EvaluationPassed: true,
			Response: "xxx Max iterations reached without completion yyy"},
	}
	got2 := RewardSignalExtractor{}.Extract(summary, cases2)
	if mi := got2.MethodSpecific["max_iteration_cases"].([]string); len(mi) != 1 {
		t.Fatalf("响应标记串分支不符: %v", mi)
	}
}

// TestAtomicChecksSignalExtractor_分支对照 对照 Python AtomicChecksSignalExtractor.extract。
// Python: signal_extractor.py:266-339
func TestAtomicChecksSignalExtractor_分支对照(t *testing.T) {
	summary := EvaluationSummaryInput{EvaluationMethod: "atomic_checks"}
	cases := []CaseAnalysisInput{
		{CaseID: "a1", Status: "passed", EvaluationPassed: false, Score: 0.5,
			EvaluationMetadata: map[string]any{"atomic_checks": []any{
				map[string]any{"name": " build ", "passed": true},
				map[string]any{"name": "lint", "passed": false, "status": "failed", "detail": strings.Repeat("d", 2500)},
				map[string]any{"name": "skip-me", "passed": false, "status": "skipped"},
				map[string]any{"no-name": 1},
				"not-a-dict",
			}}},
		{CaseID: "a2", Status: "passed", EvaluationPassed: true, Score: 1.0,
			EvaluationMetadata: map[string]any{"atomic_checks": []any{}}},
	}
	got := AtomicChecksSignalExtractor{}.Extract(summary, cases)
	ms := got.MethodSpecific

	byCase := ms["atomic_checks_by_case"].(map[string]map[string][]string)
	if len(byCase["a1"]["passed"]) != 1 || byCase["a1"]["passed"][0] != "build" {
		t.Fatalf("passed 校验（name strip）不符: %v", byCase["a1"])
	}
	if len(byCase["a1"]["failed"]) != 1 || byCase["a1"]["failed"][0] != "lint" {
		t.Fatalf("failed 校验（skipped 排除）不符: %v", byCase["a1"])
	}
	details := ms["failed_check_details_by_case"].(map[string][]map[string]string)
	if len(details["a1"]) != 1 || len(details["a1"][0]["detail"]) != 2000 {
		t.Fatalf("failed detail 截断 2000 不符: %d", len(details["a1"][0]["detail"]))
	}
	if ps := ms["partial_score_cases"].([]string); len(ps) != 1 || ps[0] != "a1" {
		t.Fatalf("partial_score_cases 不符: %v", ms["partial_score_cases"])
	}
	if mh := ms["member_harness_issue_cases"].([]string); len(mh) != 1 || mh[0] != "a1" {
		t.Fatalf("member_harness_issue_cases 不符: %v", ms["member_harness_issue_cases"])
	}
	// 本方法独立计算 exec/judge（非 generic）：a1 无错误且未通过 → judge_failures=[a1]
	if len(got.JudgeFailures) != 1 || got.JudgeFailures[0] != "a1" {
		t.Fatalf("judge_failures 不符: %v", got.JudgeFailures)
	}
}

// TestLlmJudgeSignalExtractor_分支对照 对照 Python LlmJudgeSignalExtractor.extract。
// Python: signal_extractor.py:357-432
func TestLlmJudgeSignalExtractor_分支对照(t *testing.T) {
	summary := EvaluationSummaryInput{EvaluationMethod: "llm_as_judge"}

	// ① 正常维度解析
	dimOK := map[string]any{
		"low_score_behaviors":           []any{"b1"},
		"triggered_forbidden_behaviors": []any{"forbidden-x"},
		"per_behavior_scores":           map[string]any{"b1": 0.3, "b2": 0.9},
		"behavior_diagnostics": map[string]any{
			"b1": map[string]any{"reason": "bad", "evidence": "ev"},
		},
		"avg_behavior_score": 0.6,
		"pass_count":         3,
		"fail_count":         1,
	}
	dimEmpty := map[string]any{"low_score_behaviors": []any{}}
	cases := []CaseAnalysisInput{
		{CaseID: "l1", EvaluationMetadata: map[string]any{"parsed": map[string]any{"dimensions": dimOK}}},
		{CaseID: "l2", EvaluationMetadata: map[string]any{"parsed": map[string]any{"dimensions": dimEmpty}}},
	}
	got := LlmJudgeSignalExtractor{}.Extract(summary, cases)
	ms := got.MethodSpecific
	if low := ms["low_score_behaviors"].(map[string][]string); len(low["l1"]) != 1 || low["l1"][0] != "b1" {
		t.Fatalf("low_score_behaviors 不符: %v", ms["low_score_behaviors"])
	}
	if dist := ms["behavior_score_distribution"].(map[string]map[string]float64); dist["l1"]["b1"] != 0.3 {
		t.Fatalf("per_behavior_scores 不符: %v", dist)
	}
	if avg := ms["avg_behavior_score"].(map[string]float64); avg["l1"] != 0.6 {
		t.Fatalf("avg_behavior_score 不符: %v", avg)
	}
	if pf := ms["behavior_pass_fail_counts"].(map[string]map[string]int); pf["l1"]["pass_count"] != 3 || pf["l1"]["fail_count"] != 1 {
		t.Fatalf("pass/fail counts 不符: %v", pf)
	}
	// l2 的 dimensions 是合法 dict（空清单）→ 正常处理；rationale_missing_cases 键不存在
	if rm, exists := ms["rationale_missing_cases"]; exists {
		t.Fatalf("l2 维度合法不应进 rationale_missing, got %v", rm)
	}
	if _, hasFallback := ms["fallback_reason"]; hasFallback {
		t.Fatal("部分缺失不应整体降级")
	}
	diag := ms["behavior_diagnostics"].(map[string]map[string]map[string]string)
	if diag["l1"]["b1"]["reason"] != "bad" {
		t.Fatalf("behavior_diagnostics 不符: %v", diag)
	}

	// ② 全量缺失 → 回落 generic + parsed_dimensions_missing
	allMissing := []CaseAnalysisInput{
		{CaseID: "l3", EvaluationMetadata: map[string]any{}},
	}
	got2 := LlmJudgeSignalExtractor{}.Extract(summary, allMissing)
	ms2 := got2.MethodSpecific
	if ms2["fallback_reason"] != "parsed_dimensions_missing" {
		t.Fatalf("全量缺失应降级: %v", ms2)
	}
}

// TestSignalExtractorRegistry_注册与回落 对照 Python 注册表行为。
// Python: signal_extractor.py:439-483
func TestSignalExtractorRegistry_注册与回落(t *testing.T) {
	// ① 已知方法分派
	if e := BuildSignalExtractor("script_based"); e.Name() != "pytest" {
		t.Fatalf("script_based 应分派 pytest, got %s", e.Name())
	}
	if e := BuildSignalExtractor("reward_based"); e.Name() != "reward" {
		t.Fatalf("reward_based 应分派 reward, got %s", e.Name())
	}
	if e := BuildSignalExtractor("atomic_checks"); e.Name() != "atomic_checks" {
		t.Fatalf("atomic_checks 应分派, got %s", e.Name())
	}
	if e := BuildSignalExtractor("llm_as_judge"); e.Name() != "llm_judge" {
		t.Fatalf("llm_as_judge 应分派, got %s", e.Name())
	}

	// ② 未知方法回落 generic
	if e := BuildSignalExtractor("unknown-method"); e.Name() != "generic" {
		t.Fatalf("未知方法应回落 generic, got %s", e.Name())
	}

	// ③ 注册自定义提取器后分派命中
	fake := &fakeExtractor{}
	if err := RegisterSignalExtractor("custom-method", fake); err != nil {
		t.Fatal(err)
	}
	if e := BuildSignalExtractor("custom-method"); e.Name() != "fake" {
		t.Fatalf("注册后应命中, got %s", e.Name())
	}

	// ④ 空方法名报错（Python ValueError 文案）
	if err := RegisterSignalExtractor("  ", fake); err == nil ||
		err.Error() != "signal extractor method must be non-empty" {
		t.Fatalf("空方法名错误文案不符: %v", err)
	}

	// ⑤ nil 提取器报错（Python TypeError 文案）
	if err := RegisterSignalExtractor("another-method", nil); err == nil ||
		err.Error() != "signal extractor must define extract(summary, case_inputs)" {
		t.Fatalf("nil 提取器错误文案不符: %v", err)
	}
}

// TestNormalizedTraceAttribution_归因种子 对照 Python _normalized_trace_attribution。
// Python: signal_extractor.py:508-568
func TestNormalizedTraceAttribution_归因种子(t *testing.T) {
	// 有角色 + 带错误的 tool call
	c := CaseAnalysisInput{
		NormalizedTraceSummary: map[string]any{
			"traces": []any{
				map[string]any{
					"trace_id":    "tr-9",
					"member_role": "tester",
					"messages": []any{
						map[string]any{
							"message_index": 2,
							"tool_calls": []any{
								map[string]any{"name": "bash", "input": "make test", "error": "exit 2", "step_pointer": "s3"},
							},
						},
					},
				},
			},
		},
	}
	got := normalizedTraceAttribution(c)
	if got == nil {
		t.Fatal("应产出归因")
	}
	if got["root_cause"] != "tester encountered a failing bash step and did not recover before the verifier failed." {
		t.Fatalf("root_cause 不符: %v", got["root_cause"])
	}
	if got["critical_mistake"] != "Earliest decisive failed step: s3; bash input=\"make test\"; error=\"exit 2\"." {
		t.Fatalf("critical_mistake 不符: %v", got["critical_mistake"])
	}
	if got["target_ref"] != "member_harness.tester.skill" || got["confidence"] != "medium" {
		t.Fatalf("target_ref/confidence 不符: %v %v", got["target_ref"], got["confidence"])
	}
	refs := got["evidence_refs"].([]any)
	ref := refs[0].(map[string]any)
	if ref["trace_id"] != "tr-9" || ref["message_index"] != 2 || ref["step_pointer"] != "s3" {
		t.Fatalf("evidence_refs 不符: %v", ref)
	}

	// team/unattributed 角色 → 低置信度 + unassigned
	c2 := CaseAnalysisInput{
		NormalizedTraceSummary: map[string]any{
			"traces": []any{map[string]any{
				"member_role": "team",
				"messages": []any{map[string]any{
					"message_index": 0,
					"tool_calls":    []any{map[string]any{"error": "boom"}},
				}},
			}},
		},
	}
	got2 := normalizedTraceAttribution(c2)
	if got2["confidence"] != "low" || got2["target_ref"] != "unassigned" {
		t.Fatalf("未归属角色不符: %v %v", got2["confidence"], got2["target_ref"])
	}
	if !strings.Contains(got2["root_cause"].(string), "unattributed execution") {
		t.Fatalf("display_role 应回落 unattributed execution: %v", got2["root_cause"])
	}
	if !strings.Contains(got2["critical_mistake"].(string), "unknown step") {
		t.Fatalf("空 step_pointer 应回落 unknown step: %v", got2["critical_mistake"])
	}

	// 无错误 → nil
	c3 := CaseAnalysisInput{NormalizedTraceSummary: map[string]any{
		"traces": []any{map[string]any{"messages": []any{map[string]any{
			"tool_calls": []any{map[string]any{"name": "bash"}},
		}}},
		}},
	}
	if normalizedTraceAttribution(c3) != nil {
		t.Fatal("无错误调用应返回 nil")
	}
}
