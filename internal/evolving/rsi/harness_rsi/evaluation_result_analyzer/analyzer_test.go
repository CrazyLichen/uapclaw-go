// analyzer_test.go 诊断分析辅助区测试（Python test_evaluation_result_analyzer.py 对应段）。
package evaluationresultanalyzer

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	harnessrsi "github.com/uapclaw/uapclaw-go/internal/evolving/rsi/harness_rsi"
	rsiconfig "github.com/uapclaw/uapclaw-go/internal/evolving/rsi/harness_rsi/config"
)

// TestTruncateText 截断标记文案（Python _truncate_text）
func TestTruncateText(t *testing.T) {
	if got := truncateText("short", 10); got != "short" {
		t.Fatalf("短文本: %q", got)
	}
	got := truncateText(strings.Repeat("x", 30), 10)
	if !strings.HasPrefix(got, strings.Repeat("x", 10)) ||
		!strings.HasSuffix(got, "...[truncated 20 chars]") {
		t.Fatalf("截断标记: %q", got)
	}
	if truncateText(nil, 10) != "" {
		t.Fatal("nil 应为空串")
	}
}

// TestOneLine 单行压平（Python _one_line）
func TestOneLine(t *testing.T) {
	if got := oneLine("a\n  b\tc", 100); got != "a b c" {
		t.Fatalf("压平: %q", got)
	}
}

// TestSafeFloat 分数解析失败回退 1.0（Python _safe_float）
func TestSafeFloat(t *testing.T) {
	if safeFloat(0.5) != 0.5 || safeFloat("0.7") != 0.7 || safeFloat(nil) != 1.0 || safeFloat("bad") != 1.0 {
		t.Fatal("safeFloat 语义不符")
	}
}

// TestBoundedJSON 序列化限长（Python _bounded_json）
func TestBoundedJSON(t *testing.T) {
	got := boundedJSON(map[string]any{"k": strings.Repeat("v", 100)}, 20)
	if len(got) > 20+30 || !strings.Contains(got, "truncated") {
		t.Fatalf("限长: %q", got)
	}
}

// TestBoundedStructuredValue 结构保留与降级（Python _bounded_structured_value）
func TestBoundedStructuredValue(t *testing.T) {
	small := map[string]any{"k": "v"}
	if out := boundedStructuredValue(small, 100); out == nil {
		t.Fatal("小对象应原样返回")
	}
	big := map[string]any{"k": strings.Repeat("v", 200)}
	out := boundedStructuredValue(big, 20)
	m, ok := out.(map[string]any)
	if !ok || !strings.Contains(toStringValue(m["truncated_json"]), "truncated") {
		t.Fatalf("超限应降级: %v", out)
	}
}

// TestCaseScopedList 清单收窄（Python _case_scoped_list）
func TestCaseScopedList(t *testing.T) {
	got := caseScopedList([]string{"c1", "c2"}, "c2")
	if len(got) != 1 || got[0] != "c2" {
		t.Fatalf("命中: %v", got)
	}
	if got := caseScopedList([]string{"c1"}, "c2"); len(got) != 0 {
		t.Fatalf("未命中: %v", got)
	}
}

// TestCaseScopedErrorClusters 聚类收窄（Python _case_scoped_error_clusters）
func TestCaseScopedErrorClusters(t *testing.T) {
	clusters := []map[string]any{
		{"fingerprint": "f1", "cases": []any{"c1", "c2"}},
		{"fingerprint": "f2", "cases": []any{"other"}},
	}
	got := caseScopedErrorClusters(clusters, "c2")
	if len(got) != 1 || got[0]["fingerprint"] != "f1" {
		t.Fatalf("收窄: %v", got)
	}
	if cases, ok := got[0]["cases"].([]string); !ok || len(cases) != 1 || cases[0] != "c2" {
		t.Fatalf("cases: %v", got[0]["cases"])
	}
}

// TestCaseScopedValue 嵌套值收窄（Python _case_scoped_value/_value_mentions_case）
func TestCaseScopedValue(t *testing.T) {
	// dict 直接含案例键
	got := caseScopedValue(map[string]any{"c1": "x", "c2": "y"}, "c2")
	m := got.(map[string]any)
	if len(m) != 1 || m["c2"] != "y" {
		t.Fatalf("dict 收窄: %v", m)
	}
	// dict 嵌套提及
	got = caseScopedValue(map[string]any{"sub": map[string]any{"deep": []any{"c1"}}}, "c1")
	m = got.(map[string]any)
	if _, ok := m["sub"]; !ok {
		t.Fatalf("嵌套保留: %v", m)
	}
	// list 含案例
	if got := caseScopedValue([]any{"c1", "z"}, "c1"); len(got.([]string)) != 1 {
		t.Fatalf("list 收窄: %v", got)
	}
	// 标量
	if caseScopedValue("c1", "c1") != "c1" {
		t.Fatal("标量命中")
	}
}

// TestCompactJudgeCriteria 判据压缩（Python _compact_judge_criteria）
func TestCompactJudgeCriteria(t *testing.T) {
	metadata := map[string]any{
		"judge_evidence": map[string]any{
			"criteria": []any{
				map[string]any{"criterion_id": "c1", "score": 0.5, "passed": true, "status": "ok", "rationale": strings.Repeat("r", 50)},
				"not-a-dict",
			},
		},
	}
	got := compactJudgeCriteria(metadata)
	if len(got) != 1 || got[0]["criterion_id"] != "c1" || got[0]["passed"] != true {
		t.Fatalf("判据: %v", got)
	}
	// 回退 judge_detail
	metadata2 := map[string]any{"judge_detail": map[string]any{"criteria": []any{map[string]any{"verifier_id": "v1"}}}}
	got2 := compactJudgeCriteria(metadata2)
	if len(got2) != 1 || got2[0]["criterion_id"] != "v1" {
		t.Fatalf("judge_detail 回退: %v", got2)
	}
	// 无判据
	if got := compactJudgeCriteria(map[string]any{}); len(got) != 0 {
		t.Fatal("无判据应空")
	}
}

// TestCompactQualityGaps 缺口压缩与 verification_gap 剔除（Python _compact_quality_gaps）
func TestCompactQualityGaps(t *testing.T) {
	value := []any{
		map[string]any{"gap_type": "verification_gap", "id": "skip"},
		map[string]any{"gap_type": "Tool_Gap", "id": "g1", "affected_roles": []any{"planner"}, "evidence": strings.Repeat("e", 40)},
		"not-dict",
	}
	got := compactQualityGaps(value)
	if len(got) != 1 || got[0]["id"] != "g1" {
		t.Fatalf("缺口: %v", got)
	}
	if roles := got[0]["affected_roles"].([]string); len(roles) != 1 || roles[0] != "planner" {
		t.Fatalf("roles: %v", got[0]["affected_roles"])
	}
}

// TestCompactDatasetBudget 数据集预算压缩（Python _compact_dataset_budget）
func TestCompactDatasetBudget(t *testing.T) {
	got := compactDatasetBudget(map[string]any{
		"total_cases": 10,
		"case_groups": []any{
			map[string]any{"source_gap": "missing", "case_count": 2, "target_roles": []any{"planner"}},
			"skip",
		},
	})
	if got["total_cases"] != 10 || len(got["case_groups"].([]map[string]any)) != 1 {
		t.Fatalf("预算: %v", got)
	}
	if got := compactDatasetBudget("not-dict"); len(got) != 0 {
		t.Fatal("非 dict 应空")
	}
}

// TestCompactJudgeDimensions 维度诊断保留（Python _compact_judge_dimensions）
func TestCompactJudgeDimensions(t *testing.T) {
	got := compactJudgeDimensions(map[string]any{
		"pass_count": 2, "other": "dropped",
		"per_behavior_scores": map[string]any{"b1": 0.1, "b2": 0.2},
	})
	if got["pass_count"] != 2 {
		t.Fatalf("保留键: %v", got)
	}
	if _, ok := got["other"]; ok {
		t.Fatal("未知键应丢弃")
	}
	if len(got["per_behavior_scores"].(map[string]any)) != 2 {
		t.Fatalf("分数: %v", got["per_behavior_scores"])
	}
}

// TestCompactRetrievedExperience 经验压缩（Python _compact_retrieved_experience）
func TestCompactRetrievedExperience(t *testing.T) {
	got := compactRetrievedExperience(map[string]any{
		"stage": "member_stage",
		"matches": []any{
			map[string]any{"id": "e1", "summary": strings.Repeat("s", 600), "experience": map[string]any{"k": "v"}},
			"skip",
		},
		"metadata": map[string]any{"m": 1},
	})
	if got["stage"] != "member_stage" {
		t.Fatalf("stage: %v", got)
	}
	matches := got["matches"].([]map[string]any)
	if len(matches) != 1 {
		t.Fatalf("matches: %v", matches)
	}
	if matches[0]["experience_id"] != "e1" {
		t.Fatalf("experience_id 回退 id: %v", matches[0])
	}
	if !strings.Contains(matches[0]["summary"].(string), "truncated") {
		t.Fatalf("summary 应截断至 500: %v", matches[0]["summary"])
	}
	if got := compactRetrievedExperience(nil); len(got) != 0 {
		t.Fatal("nil 应返回空 map（Python _compact_retrieved_experience(None)→{}）")
	}
}

// TestExperienceUsagePolicy 经验使用规则（Python _experience_usage_policy）
func TestExperienceUsagePolicy(t *testing.T) {
	got := experienceUsagePolicy()
	if got["must_use_current_evidence_first"] != true {
		t.Fatalf("规则: %v", got)
	}
	if len(got["rules"].([]string)) != 5 {
		t.Fatalf("rules 数: %v", got["rules"])
	}
}

// TestStageInstruction 阶段指令（Python _stage_instruction）
func TestStageInstruction(t *testing.T) {
	cases := map[string]string{
		"single_harness_candidate_failure": "Analyze why the evaluated candidate",
		"single_harness_residual_repair":   "Re-analyze the still-failing",
		"single_harness_custom":            "no Team Skill optimization surface",
		"member_stage":                     "current Team Skill",
		"team_skill_stage":                 "team_skill.<role>.<variable>;",
		"":                                 "Use member_harness.<role>.<variable>",
	}
	for stage, needle := range cases {
		if got := stageInstruction(stage); !strings.Contains(got, needle) {
			t.Fatalf("stage %q: %q", stage, got)
		}
	}
}

// TestFormatTemplate 占位替换（Python str.format 等价）
func TestFormatTemplate(t *testing.T) {
	got := formatTemplate("Hello {name}, json={data}", map[string]string{
		"name": "world", "data": `{"k": 1}`,
	})
	if got != `Hello world, json={"k": 1}` {
		t.Fatalf("替换: %q", got)
	}
}

// TestBuildDiagnosisPrompt 逐案诊断提示词组装（Python _build_diagnosis_prompt）
func TestBuildDiagnosisPrompt(t *testing.T) {
	caseInput := &CaseAnalysisInput{CaseID: "c1", Input: "do it", ResultPath: filepath.Join(t.TempDir(), "result.json")}
	signals := &DeterministicSignals{Method: "pytest"}
	prompt := buildDiagnosisPrompt(caseInput, signals, nil, true, "", nil, nil)
	if !strings.HasPrefix(prompt, "Evidence access: evidence_summary.md") {
		t.Fatalf("有摘要时前缀 execution history notice: %q", prompt[:60])
	}
	if !strings.Contains(prompt, `"case_id": "c1"`) {
		t.Fatalf("内联 JSON: %q", prompt[len(prompt)-200:])
	}
	// 无摘要：无 notice 前缀
	prompt2 := buildDiagnosisPrompt(caseInput, signals, nil, false, "", nil, nil)
	if strings.HasPrefix(prompt2, "Evidence access:") {
		t.Fatal("无摘要时不应有 notice 前缀")
	}
	if strings.Contains(prompt2, "{evidence_instruction}") || strings.Contains(prompt2, "{diagnosis_input}") {
		t.Fatal("占位符应被替换")
	}
	if !strings.Contains(prompt2, "No evidence_summary.md is available") {
		t.Fatal("无摘要分支指令应注入")
	}
}

// TestBuildEvidenceSummary 证据摘要组装（Python _build_evidence_summary）
func TestBuildEvidenceSummary(t *testing.T) {
	caseDir := t.TempDir()
	resultPath := filepath.Join(caseDir, "result.json")
	if err := os.WriteFile(resultPath, []byte(`{"evaluation":{"metadata":{}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	caseInput := &CaseAnalysisInput{
		CaseID: "c1", Status: "failed", Score: 0.2, Input: "task body",
		EvaluationMethod: "pytest", EvaluationReason: "2 tests failed",
		ResultPath: resultPath, EvaluationMetadata: map[string]any{},
	}
	summary := buildEvidenceSummary(caseInput)
	for _, section := range []string{
		"# Analyzer Evidence Summary", "## Authoritative Task Contract",
		"## Case Facts", "## Deterministic Validation Inventory",
	} {
		if !strings.Contains(summary, section) {
			t.Fatalf("缺段 %q", section)
		}
	}
	if !strings.Contains(summary, "task body") || !strings.Contains(summary, "2 tests failed") {
		t.Fatalf("案例事实未进摘要")
	}
	if !strings.HasSuffix(summary, "\n") {
		t.Fatal("应以换行结尾")
	}
}

// TestBuildVerifierInventory 权威验证器清单（Python _build_verifier_inventory）
func TestBuildVerifierInventory(t *testing.T) {
	caseInput := &CaseAnalysisInput{
		CaseID: "c1",
		EvaluationMetadata: map[string]any{
			"empty_patch":         false,
			"test_output_excerpt": strings.Repeat("o", 9000),
			"instance_report": map[string]any{
				"c1": map[string]any{
					"patch_successfully_applied": true, "resolved": false,
					"tests_status": map[string]any{
						"FAIL_TO_PASS": map[string]any{"failure": []any{"t1", "t2", ""}},
						"PASS_TO_PASS": map[string]any{"failure": []any{}},
					},
				},
			},
		},
	}
	got := buildVerifierInventory(caseInput)
	if got["patch_successfully_applied"] != true || got["resolved"] != false {
		t.Fatalf("报告: %v", got)
	}
	if ftp := got["failed_fail_to_pass_tests"].([]string); len(ftp) != 2 {
		t.Fatalf("失败清单: %v", ftp)
	}
	if excerpt := toStringValue(got["verifier_failure_output_excerpt"]); !strings.Contains(excerpt, "truncated") {
		t.Fatalf("摘录应截断至 8000")
	}
	// 无报告但有 empty_patch
	caseInput2 := &CaseAnalysisInput{CaseID: "c1", EvaluationMetadata: map[string]any{"empty_patch": true}}
	got2 := buildVerifierInventory(caseInput2)
	if got2["empty_patch"] != true {
		t.Fatalf("仅 empty_patch: %v", got2)
	}
	// 无报告无 empty_patch
	if got3 := buildVerifierInventory(&CaseAnalysisInput{CaseID: "c1", EvaluationMetadata: map[string]any{}}); len(got3) != 0 {
		t.Fatalf("应空: %v", got3)
	}
}

// TestValidationInventoryFromEvents 项目测试事实提取（Python _validation_inventory_from_events/_validation_result_signal）
func TestValidationInventoryFromEvents(t *testing.T) {
	events := []map[string]any{
		{"input": "pytest tests/ -x", "output_tail": "3 passed in 1.2s", "validation_result": "passed"},
		{"input": "ls", "output_tail": "files", "validation_result": "unknown"},
		{"input": "make test", "output_tail": "2 failed", "validation_result": "failed"},
	}
	got := validationInventoryFromEvents(events, false)
	if got["project_test_suite_attempted"] != true || got["project_test_suite_result"] != "passed" {
		t.Fatalf("清单: %v", got)
	}
	if got["authoritative_verifier_result"] != "failed" {
		t.Fatalf("验证器结果: %v", got)
	}
	if events := got["project_test_events"].([]map[string]any); len(events) != 2 {
		t.Fatalf("pytest+make test 应各收一条（ls 不收）: %v", events)
	}
	// 信号判定
	if validationResultSignal("2 passed", "") != "passed" ||
		validationResultSignal("3 failed", "") != "failed" ||
		validationResultSignal("ok", "exit_code=1") != "failed" ||
		validationResultSignal("nothing", "") != "unknown" {
		t.Fatal("validation_result_signal 语义不符")
	}
}

// TestSummarizeNormalizedTrace 轨迹事件提取（Python _summarize_normalized_trace）
func TestSummarizeNormalizedTrace(t *testing.T) {
	traceData := map[string]any{
		"traces": []any{
			map[string]any{
				"trace_id": "t1", "role": "assistant",
				"messages": []any{
					map[string]any{
						"message_index": 0, "content": "hello world",
						"tool_calls": []any{
							map[string]any{"name": "bash", "input": "ls", "output": "2 passed", "error": "", "step_pointer": "s1"},
						},
					},
				},
			},
			"not-dict",
		},
	}
	events := summarizeNormalizedTrace(traceData)
	if len(events) != 2 { // 1 条 content + 1 条 tool_call
		t.Fatalf("events: %v", events)
	}
	if events[0]["output"] != "hello world" {
		t.Fatalf("content 事件: %v", events[0])
	}
	if events[1]["tool"] != "bash" || events[1]["validation_result"] != "passed" {
		t.Fatalf("tool_call 事件: %v", events[1])
	}
}

// TestFormatTraceEvent 事件格式化（Python _format_trace_event）
func TestFormatTraceEvent(t *testing.T) {
	okEvent := map[string]any{"trace_id": "t1", "role": "assistant", "message_index": 1, "step_pointer": "s", "tool": "bash", "output": "done"}
	got := formatTraceEvent(okEvent)
	if !strings.Contains(got, "[ok]") || !strings.Contains(got, "output=done") {
		t.Fatalf("ok 事件: %q", got)
	}
	errEvent := map[string]any{"trace_id": "t1", "role": "assistant", "message_index": 2, "step_pointer": "s", "tool": "bash", "error": "boom"}
	if got := formatTraceEvent(errEvent); !strings.Contains(got, "[err]") || !strings.Contains(got, "error=boom") {
		t.Fatalf("err 事件: %q", got)
	}
}

// TestBuildAggregationPrompt 聚合提示词组装（Python _build_aggregation_prompt）
func TestBuildAggregationPrompt(t *testing.T) {
	summary := &EvaluationSummaryInput{TotalCases: 3, PassedCount: 1, FailedCount: 2, AverageScore: 0.5, EvaluationMethod: "pytest"}
	diagnoses := []map[string]any{
		{"case_id": "c1", "summary": "bad", "root_cause": strings.Repeat("r", 600)},
	}
	signals := &DeterministicSignals{Method: "pytest", ExecFailures: []string{"c1"}}
	got := buildAggregationPrompt(summary, diagnoses, signals, nil, 5, 3, "member_stage")
	if !strings.Contains(got, "total_cases") || !strings.Contains(got, "3") {
		t.Fatalf("统计: %q", got[:200])
	}
	if strings.Contains(got, "{per_case_diagnoses}") || strings.Contains(got, "{stage_instruction}") {
		t.Fatal("占位符应全部替换")
	}
	if !strings.Contains(got, "current Team Skill") {
		t.Fatalf("阶段指令 member_stage 文案: %q", got[len(got)-400:])
	}
}

// ──────────────────────────── Task 12/13：策略/门面/后处理 ────────────────────────────

// TestExtractJSONObject 首个 JSON 对象提取（Python _extract_json_object）
func TestExtractJSONObject(t *testing.T) {
	if got := extractJSONObject(`prose {"a": 1} more {"b": 2}`); toStringValue(got["a"]) != "1" {
		t.Fatalf("首个对象: %v", got)
	}
	if got := extractJSONObject("no json"); got != nil {
		t.Fatalf("无 JSON: %v", got)
	}
	if got := extractJSONObject(`{"s": "brace } inside"}`); toStringValue(got["s"]) != "brace } inside" {
		t.Fatalf("字符串内大括号: %v", got)
	}
}

// TestContainsIncompleteJSONObject 截断 JSON 检测（Python _contains_incomplete_json_object）
func TestContainsIncompleteJSONObject(t *testing.T) {
	if !containsIncompleteJSONObject(`result: {"a": "b`) && !containsIncompleteJSONObject(`{"a": "b`) {
		t.Fatal("截断对象应检出")
	}
	if containsIncompleteJSONObject(`{"a": 1}`) {
		t.Fatal("完整对象不应检出")
	}
	if containsIncompleteJSONObject(`{"a": "}}"}`) {
		t.Fatal("字符串内大括号的完整对象不应误报")
	}
}

// TestUnusableDiagnosisOutputError 分类（Python _unusable_diagnosis_output_error）
func TestUnusableDiagnosisOutputError(t *testing.T) {
	// 无 JSON → 格式错误
	err := unusableDiagnosisOutputError("c1", []string{"plain prose"})
	var formatErr *diagnosisOutputFormatError
	if !errors.As(err, &formatErr) {
		t.Fatalf("应格式错误: %v", err)
	}
	// 有 JSON 无诊断 → 内容错误
	err = unusableDiagnosisOutputError("c1", []string{`{"other": 1}`})
	var contentErr *diagnosisContentError
	if !errors.As(err, &contentErr) {
		t.Fatalf("应内容错误: %v", err)
	}
	// 截断 JSON → 可重试输出
	err = unusableDiagnosisOutputError("c1", []string{`{"diagnoses": [{"a": "b`})
	if !harnessrsi.IsRetryableModelCallFailure(err) {
		t.Fatalf("应可重试: %v", err)
	}
	// 服务错误文本
	err = unusableDiagnosisOutputError("c1", []string{"Error code: 401 - invalid_api_key"})
	if containsModelServiceErrorText(err.Error()) == "" {
		t.Fatalf("应含服务错误标记: %v", err)
	}
}

// TestNormalizeCaseDiagnoses 展开/排序/去重（Python _normalize_case_diagnoses）
func TestNormalizeCaseDiagnoses(t *testing.T) {
	// 旧式单诊断
	got := normalizeCaseDiagnoses(map[string]any{"summary": "s", "target_ref": "member_harness.planner.prompt"}, nil)
	if len(got) != 1 {
		t.Fatalf("旧式单诊断: %v", got)
	}
	// 列表去重：同 target_ref+failure_mode+cluster → 只留一条（多条须带 failure_cluster）
	parsed := map[string]any{"diagnoses": []any{
		map[string]any{"summary": "a", "target_ref": "member_harness.p.prompt", "failure_mode": "late-call",
			"failure_cluster": map[string]any{"failed_checks": []any{"t1"}, "observable_behavior": "boom"}},
		map[string]any{"summary": "b", "target_ref": "member_harness.p.prompt", "failure_mode": "late-call",
			"failure_cluster": map[string]any{"failed_checks": []any{"t1"}, "observable_behavior": "boom"}},
	}}
	got = normalizeCaseDiagnoses(parsed, nil)
	if len(got) != 1 {
		t.Fatalf("去重: %v", got)
	}
	// 多条需 failure_cluster（requireCluster）：缺 cluster 的被剔除
	parsed = map[string]any{"diagnoses": []any{
		map[string]any{"summary": "a", "target_ref": "x"},
		map[string]any{"summary": "b", "target_ref": "y"},
	}}
	got = normalizeCaseDiagnoses(parsed, nil)
	if len(got) != 0 {
		t.Fatalf("多条缺 cluster 应剔除: %v", got)
	}
}

// TestCandidateFeedbackCheckSets 反馈检查集（Python _candidate_feedback_check_sets）
func TestCandidateFeedbackCheckSets(t *testing.T) {
	feedback := map[string]any{"experiments": []any{
		map[string]any{"verifier_delta": map[string]any{
			"regressed_fail_to_pass": []any{"t1"}, "newly_passed_fail_to_pass": []any{"t1", "t2"},
		}},
	}}
	sets := candidateFeedbackCheckSets(feedback)
	if !sets["regressed"]["t1"] {
		t.Fatalf("regressed: %v", sets)
	}
	if sets["fixed"]["t1"] {
		t.Fatal("t1 已回归，不应留在 fixed")
	}
	if !sets["fixed"]["t2"] {
		t.Fatalf("fixed 应含 t2: %v", sets)
	}
	// nil → 全空
	sets = candidateFeedbackCheckSets(nil)
	if len(sets["regressed"]) != 0 {
		t.Fatal("nil 应空")
	}
}

// TestDiagnosisFeedbackPriority 优先级（Python _diagnosis_feedback_priority）
func TestDiagnosisFeedbackPriority(t *testing.T) {
	sets := map[string]map[string]bool{
		"regressed": {"t1": true}, "remaining": {"t2": true}, "fixed": {"t3": true},
	}
	mk := func(checks ...string) map[string]any {
		return map[string]any{"failure_cluster": map[string]any{"failed_checks": checks, "observable_behavior": ""}}
	}
	if diagnosisFeedbackPriority(mk("t1"), sets) != 0 {
		t.Fatal("regressed → 0")
	}
	if diagnosisFeedbackPriority(mk("t2"), sets) != 1 {
		t.Fatal("remaining → 1")
	}
	if diagnosisFeedbackPriority(mk("t3"), sets) != 3 {
		t.Fatal("仅 fixed → 3")
	}
	if diagnosisFeedbackPriority(mk("t9"), sets) != 2 {
		t.Fatal("其他 → 2")
	}
}

// TestDictToTeamIssue 类别锁与归属提取（Python _dict_to_team_issue）
func TestDictToTeamIssue(t *testing.T) {
	// 平铺字段回退
	issue := dictToTeamIssue(map[string]any{
		"issue_category": "weird", "summary": "s", "root_cause": "r", "target_ref": "member_harness.p.prompt",
		"affected_components": []any{"tool_x"},
	})
	if issue.Category != "team_coordination" {
		t.Fatalf("类别锁: %s", issue.Category)
	}
	attr := issue.Metadata["attribution"].(map[string]any)
	if attr["root_cause"] != "r" {
		t.Fatalf("平铺归属: %v", attr)
	}
	if comps := issue.Metadata["affected_components"].([]string); len(comps) != 1 {
		t.Fatalf("affected_components: %v", issue.Metadata)
	}
	// 嵌套优先
	issue2 := dictToTeamIssue(map[string]any{
		"metadata": map[string]any{"attribution": map[string]any{"target_ref": "team_skill.x.y"}},
	})
	if issue2.Metadata["attribution"].(map[string]any)["target_ref"] != "team_skill.x.y" {
		t.Fatal("嵌套归属应优先")
	}
}

// TestApplyG5Mapping G5 映射路径（Python _apply_g5_mapping）
func TestApplyG5Mapping(t *testing.T) {
	// member 路径
	issue := dictToTeamIssue(map[string]any{
		"suspected_team_scope": "member",
		"metadata":             map[string]any{"attribution": map[string]any{"target_ref": "member_harness.planner.prompt"}, "affected_components": []any{"planner"}},
	})
	mapped := applyG5Mapping(issue)
	if mapped.OptimizationTarget != "member_harness" || len(mapped.TargetMembers) != 1 || mapped.TargetMembers[0] != "planner" {
		t.Fatalf("member 路径: %+v", mapped)
	}
	// unassigned → 清空
	issue2 := dictToTeamIssue(map[string]any{"suspected_team_scope": "member",
		"metadata": map[string]any{"attribution": map[string]any{"target_ref": "unassigned"}}})
	mapped2 := applyG5Mapping(issue2)
	if mapped2.OptimizationTarget != "" || len(mapped2.TargetMembers) != 0 {
		t.Fatalf("unassigned: %+v", mapped2)
	}
	// 证据管线失败 → 排除出优化器门
	issue3 := dictToTeamIssue(map[string]any{
		"summary":              "missing evidence_summary.md file",
		"recommendation":       "fix normalized_trace.json not found",
		"suspected_team_scope": "member",
		"metadata":             map[string]any{"attribution": map[string]any{"target_ref": "member_harness.planner.prompt"}},
	})
	mapped3 := applyG5Mapping(issue3)
	if mapped3.OptimizationTarget != "" {
		t.Fatalf("证据管线失败应排除: %+v", mapped3)
	}
	// coordinator 改道
	issue4 := dictToTeamIssue(map[string]any{
		"summary":  "leader did not verify deliverable file",
		"metadata": map[string]any{"attribution": map[string]any{"target_ref": "member_harness.team.prompt"}},
	})
	mapped4 := applyG5Mapping(issue4)
	if mapped4.Category != "team_coordination" || mapped4.OptimizationTarget != "team_skill" {
		t.Fatalf("coordinator 改道: %+v", mapped4)
	}
	if attr := mapped4.Metadata["attribution"].(map[string]any); !strings.HasPrefix(attr["target_ref"].(string), "team_skill.team_leader.") {
		t.Fatalf("team_skill target_ref: %v", attr)
	}
}

// TestAggregateStructuredDiagnoses 聚合分组与编号（Python _aggregate_structured_diagnoses）
func TestAggregateStructuredDiagnoses(t *testing.T) {
	perCase := []map[string]any{
		{"case_id": "c1", "target_ref": "member_harness.planner.prompt", "failure_mode": "late",
			"severity": "high", "confidence": "high", "summary": "s1", "root_cause": "r1",
			"affected_components": []any{"planner"}},
		{"case_id": "c2", "target_ref": "member_harness.planner.prompt", "failure_mode": "late",
			"severity": "low", "confidence": "medium", "summary": "s2", "root_cause": "r2"},
		{"case_id": "c3", "target_ref": "unassigned", "analysis_failed": true},
	}
	// 同 (target_ref, failure_mode) 碰撞时按 discriminator 细分；
	// c1/c2 无 failure_cluster → discriminator=root_cause（r1≠r2）→ 两个 issue
	issues := aggregateStructuredDiagnoses(perCase, 5, 2)
	if len(issues) != 2 {
		t.Fatalf("碰撞细分应 2 条: %d", len(issues))
	}
	issue := issues[0]
	if issue.IssueID != "issue_001" || issue.Category != "member_harness" {
		t.Fatalf("编号/类别: %+v", issue)
	}
	if !sameStrings(issue.AffectedCases, []string{"c1"}) {
		t.Fatalf("受影响案例: %v", issue.AffectedCases)
	}
	if issue.Severity != "high" {
		t.Fatalf("最强者 severity: %s", issue.Severity)
	}
	if issue.Summary != "s1" {
		t.Fatalf("最强者 summary: %s", issue.Summary)
	}
	// max_issues=0 → 空
	if got := aggregateStructuredDiagnoses(perCase, 0, 2); len(got) != 0 {
		t.Fatalf("max_issues=0: %d", len(got))
	}
}

// sameStrings 字符串切片相等（辅助）
func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestDiagnosisUnavailableAndConflictResults 兜底结果（Python _diagnosis_unavailable_result/_diagnosis_evidence_conflict_result）
func TestDiagnosisUnavailableAndConflictResults(t *testing.T) {
	caseInput := &CaseAnalysisInput{CaseID: "c1", Score: 0.3, EvaluationReason: "r"}
	got := diagnosisUnavailableResult(caseInput, &diagnosisOutputFormatError{"boom"})
	if got["diagnosis_error_type"] != "output_format" || got["analysis_failed"] != true {
		t.Fatalf("unavailable: %v", got)
	}
	got2 := diagnosisUnavailableResult(caseInput, &diagnosisContentError{"no entries"})
	if got2["diagnosis_error_type"] != "diagnosis_content" {
		t.Fatalf("content 分类: %v", got2)
	}
	got3 := diagnosisUnavailableResult(caseInput, errors.New("timeout"))
	if got3["diagnosis_error_type"] != "model_service" {
		t.Fatalf("默认 model_service: %v", got3)
	}
	conflict := diagnosisEvidenceConflictResult(caseInput, []string{"e1", "e2"})
	if conflict["diagnosis_status"] != "evidence_conflict" || conflict["root_cause"] != "e1; e2" {
		t.Fatalf("conflict: %v", conflict)
	}
}

// TestBuildJSONRepairPrompt 修复提示词（Python _build_json_repair_prompt）
func TestBuildJSONRepairPrompt(t *testing.T) {
	got := buildJSONRepairPrompt("orig prompt", "not json at all")
	if !strings.Contains(got, "Previous diagnosis output was not valid JSON.") {
		t.Fatalf("非 JSON 分支: %q", got[:200])
	}
	if !strings.Contains(got, "orig prompt") || !strings.Contains(got, "not json at all") {
		t.Fatal("原文应内嵌")
	}
	got2 := buildJSONRepairPrompt("p", `{"x": 1}`)
	if !strings.Contains(got2, "no usable diagnosis entries") {
		t.Fatalf("JSON 无诊断分支: %q", got2[:200])
	}
}

// TestConstrainIssueToInvocation 单 harness 重映射（Python _constrain_issue_to_invocation）
func TestConstrainIssueToInvocation(t *testing.T) {
	invocation := harnessrsi.EvaluationResultAnalysisInvocation{
		HarnessRefsPath: "/tmp/harness", TeamSkillRefPath: "",
	}
	// team_skill target_ref → member_harness 重映射
	issue := dictToTeamIssue(map[string]any{
		"category": "team_coordination", "suspected_team_scope": "team_skill",
		"metadata": map[string]any{"attribution": map[string]any{"target_ref": "team_skill.writer.tone"}},
	})
	got := constrainIssueToInvocation(issue, invocation)
	if got.Category != "member_harness" || got.OptimizationTarget != "member_harness" {
		t.Fatalf("重映射: %+v", got)
	}
	if attr := got.Metadata["attribution"].(map[string]any); attr["target_ref"] != "member_harness.writer.tone" {
		t.Fatalf("target_ref: %v", attr)
	}
	// 非 single harness 不动
	invocation2 := harnessrsi.EvaluationResultAnalysisInvocation{
		HarnessRefsPath: "/h", TeamSkillRefPath: "/ts",
	}
	issue2 := dictToTeamIssue(map[string]any{
		"category": "team_coordination",
		"metadata": map[string]any{"attribution": map[string]any{"target_ref": "team_skill.writer.tone"}},
	})
	got2 := constrainIssueToInvocation(issue2, invocation2)
	if got2.Category != "team_coordination" {
		t.Fatalf("非 single harness 应不动: %+v", got2)
	}
}

// TestCaseArtifactIndex 案例工件索引（Python _case_artifact_index）
func TestCaseArtifactIndex(t *testing.T) {
	root := t.TempDir()
	caseDir := filepath.Join(root, "c1")
	judgeDir := filepath.Join(caseDir, "judge")
	if err := os.MkdirAll(judgeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(caseDir, "result.json"), []byte(`{"case_id":"c1"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(caseDir, "trace.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(judgeDir, "normalized_trace.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	index := caseArtifactIndex(root)
	ref := index["c1"]
	if ref == nil || !strings.HasSuffix(toStringValue(ref["result_path"]), "result.json") {
		t.Fatalf("索引: %v", ref)
	}
	if _, ok := ref["trace_path"]; !ok {
		t.Fatal("trace_path 应记录")
	}
	if _, ok := ref["normalized_trace_path"]; !ok {
		t.Fatal("normalized_trace_path 应记录")
	}
}

// TestFacadeEmptyCases 门面空案例分支（Python analyze→empty_case_results）
func TestFacadeEmptyCases(t *testing.T) {
	outputDir := t.TempDir()
	analyzer := NewEvaluationResultAnalyzer(&rsiconfig.EvaluationResultAnalyzerConfig{
		OutputFilename: "issues.yaml",
	})
	evalRef := filepath.Join(t.TempDir(), "eval_ref.yaml")
	if err := os.WriteFile(evalRef, []byte("summary_path: ''\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	refPath, err := analyzer.Analyze(context.Background(), harnessrsi.EvaluationResultAnalysisInvocation{
		OutputDir: outputDir, CaseResultsDir: filepath.Join(t.TempDir(), "empty"),
		EvalRefPath: evalRef,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(refPath, "analysis_ref.yaml") {
		t.Fatalf("ref 路径: %s", refPath)
	}
	data, _ := os.ReadFile(refPath)
	if !strings.Contains(string(data), "empty_case_results") {
		t.Fatalf("分析状态应空: %s", data)
	}
}
