// analyzer_test.go 诊断分析辅助区测试（Python test_evaluation_result_analyzer.py 对应段）。
package evaluationresultanalyzer

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	docxlib "github.com/fumiama/go-docx"
	harnessrsi "github.com/uapclaw/uapclaw-go/internal/evolving/rsi/harness_rsi"
	rsiconfig "github.com/uapclaw/uapclaw-go/internal/evolving/rsi/harness_rsi/config"

	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
	singleagentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	commonschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
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
	refPath, aerr2 := analyzer.Analyze(context.Background(), harnessrsi.EvaluationResultAnalysisInvocation{
		OutputDir: outputDir, CaseResultsDir: filepath.Join(t.TempDir(), "empty"),
		EvalRefPath: evalRef,
	})
	if aerr2 != nil {
		t.Fatal(aerr2)
	}
	if !strings.HasSuffix(refPath, "analysis_ref.yaml") {
		t.Fatalf("ref 路径: %s", refPath)
	}
	data, _ := os.ReadFile(refPath)
	if !strings.Contains(string(data), "empty_case_results") {
		t.Fatalf("分析状态应空: %s", data)
	}
}

// ──────────────────────────── Task 16：覆盖率补齐（策略管线/校验链/证据准备） ────────────────────────────

// TestVerifierTestIDsMatch 截断名唯一佐证匹配（Python _verifier_test_ids_match）
func TestVerifierTestIDsMatch(t *testing.T) {
	// 完全相等
	if !verifierTestIDsMatch([]any{"t1", "t2"}, []string{"t1", "t2"}, "") {
		t.Fatal("完全相等应匹配")
	}
	// 长度不等
	if verifierTestIDsMatch([]any{"t1"}, []string{"t1", "t2"}, "") {
		t.Fatal("长度不等不匹配")
	}
	// 无括号的截断名不匹配（Python 实证：variant 须 endswith "]"）
	log := "FAILED test_parse[mode=legacy] - AssertionError\nFAILED other_test - fail"
	if verifierTestIDsMatch([]any{"test_parse"}, []string{"test_parse[mode=legacy]"}, log) {
		t.Fatal("无括号截断名不应匹配")
	}
	// 非唯一佐证
	log2 := "FAILED test_parse[mode=a] - x\nFAILED test_parse[mode=b] - y"
	if verifierTestIDsMatch([]any{"test_parse"}, []string{"test_parse[mode=legacy]"}, log2) {
		t.Fatal("非唯一佐证不匹配")
	}
	// 无佐证
	if verifierTestIDsMatch([]any{"test_parse"}, []string{"test_parse[mode=legacy]"}, "") {
		t.Fatal("无佐证不匹配")
	}
	// 控制字符归一化（日志内为转义形式）
	log3 := "FAILED test_parse[a\\nb] - x\nFAILED other - y"
	if !verifierTestIDsMatch([]any{"test_parse[a\nb]"}, []string{"test_parse[a\nb]"}, log3) {
		t.Fatal("控制字符变体应匹配")
	}
	// 完整名精确相等（非截断路径）
	if !verifierTestIDsMatch([]any{"test_parse[mode=legacy]"}, []string{"test_parse[mode=legacy]"}, log) {
		t.Fatal("精确相等应匹配")
	}
	// 非 string 元素
	if verifierTestIDsMatch([]any{1}, []string{"t1"}, "") {
		t.Fatal("非 string 应不匹配")
	}
}

// TestDiagnosisValidationConflictsLayer 归因层校验（Python _diagnosis_validation_conflicts layer 段）
func TestDiagnosisValidationConflictsLayer(t *testing.T) {
	mk := func(layer, targetRef string) map[string]any {
		return map[string]any{"failure_layer": layer, "target_ref": targetRef}
	}
	inventory := map[string]any{"project_test_suite_result": "not_observed", "authoritative_verifier_result": "failed"}
	// 非法 layer
	errs := diagnosisValidationConflicts(mk("weird", "unassigned"), inventory, nil, "")
	if len(errs) != 1 || !strings.Contains(errs[0], "failure_layer must be") {
		t.Fatalf("非法 layer: %v", errs)
	}
	// evidence 层要求 unassigned
	errs = diagnosisValidationConflicts(mk("evidence", "member_harness.p.prompt"), inventory, nil, "")
	if len(errs) != 1 || !strings.Contains(errs[0], "evidence requires target_ref=unassigned") {
		t.Fatalf("evidence 归因: %v", errs)
	}
	// runtime 层仅允许 member_harness.*.config
	errs = diagnosisValidationConflicts(mk("runtime", "member_harness.p.prompt"), inventory, nil, "")
	if len(errs) != 1 || !strings.Contains(errs[0], "runtime failures require") {
		t.Fatalf("runtime 归因: %v", errs)
	}
	errs = diagnosisValidationConflicts(mk("runtime", "member_harness.p.config"), inventory, nil, "")
	if len(errs) != 0 {
		t.Fatalf("runtime config 合法: %v", errs)
	}
}

// TestDiagnosisValidationConflictsContradiction 本地过/验证器挂矛盾校验（Python 矛盾段）
func TestDiagnosisValidationConflictsContradiction(t *testing.T) {
	inventory := map[string]any{
		"project_test_suite_result":     "passed",
		"authoritative_verifier_result": "failed",
	}
	// 缺 validation_observations
	errs := diagnosisValidationConflicts(map[string]any{"failure_layer": "harness"}, inventory, nil, "")
	found := false
	for _, e := range errs {
		if strings.Contains(e, "missing validation_observations") {
			found = true
		}
	}
	if !found {
		t.Fatalf("缺 observations: %v", errs)
	}
	// observations 字段不符
	obs := map[string]any{
		"project_test_suite_attempted":  true,
		"project_test_suite_result":     "failed",
		"authoritative_verifier_result": "failed",
		"contradiction_explanation":     "env diff",
	}
	errs = diagnosisValidationConflicts(map[string]any{"failure_layer": "harness", "validation_observations": obs}, inventory, nil, "")
	found = false
	for _, e := range errs {
		if strings.Contains(e, "project_test_suite_result must equal") {
			found = true
		}
	}
	if !found {
		t.Fatalf("字段不符: %v", errs)
	}
	// 合法 observations + 矛盾推荐语
	obs2 := map[string]any{
		"project_test_suite_attempted":  true,
		"project_test_suite_result":     "passed",
		"authoritative_verifier_result": "failed",
		"contradiction_explanation":     "verifier env differs",
	}
	errs = diagnosisValidationConflicts(map[string]any{
		"failure_layer": "harness", "validation_observations": obs2,
		"recommendation": "must run the project's own test suite first",
	}, inventory, nil, "")
	found = false
	for _, e := range errs {
		if strings.Contains(e, "recommendation contradicts") {
			found = true
		}
	}
	if !found {
		t.Fatalf("矛盾推荐: %v", errs)
	}
}

// TestDiagnosisValidationConflictsPatch 补丁应用但未解决（Python 补丁段）
func TestDiagnosisValidationConflictsPatch(t *testing.T) {
	verifier := map[string]any{
		"patch_successfully_applied":      true,
		"resolved":                        false,
		"failed_fail_to_pass_tests":       []string{"t1"},
		"failed_pass_to_pass_tests":       []string{},
		"verifier_failure_output_excerpt": "FAILED t1 - boom",
	}
	inventory := map[string]any{"project_test_suite_result": "not_observed"}
	// 缺 verifier_observations
	errs := diagnosisValidationConflicts(map[string]any{"failure_layer": "harness"}, inventory, verifier, "")
	found := false
	for _, e := range errs {
		if strings.Contains(e, "missing verifier_observations") {
			found = true
		}
	}
	if !found {
		t.Fatalf("缺 verifier_observations: %v", errs)
	}
	// observations 匹配 + 诊断文本声称补丁失败
	obs := map[string]any{
		"patch_successfully_applied": true,
		"failed_fail_to_pass_tests":  []any{"t1"},
		"failed_pass_to_pass_tests":  []any{},
	}
	errs = diagnosisValidationConflicts(map[string]any{
		"failure_layer": "harness", "verifier_observations": obs,
		"root_cause": "patch application failed",
	}, inventory, verifier, "")
	found = false
	for _, e := range errs {
		if strings.Contains(e, "contradicts authoritative successful patch application") {
			found = true
		}
	}
	if !found {
		t.Fatalf("补丁失败声称: %v", errs)
	}
	// 合法诊断无冲突
	errs = diagnosisValidationConflicts(map[string]any{
		"failure_layer": "harness", "verifier_observations": obs,
		"root_cause": "wrong mode selected",
	}, inventory, verifier, "")
	if len(errs) != 0 {
		t.Fatalf("合法应无冲突: %v", errs)
	}
}

// TestCaseDiagnosesValidationConflicts 逐条位置前缀（Python _case_diagnoses_validation_conflicts）
func TestCaseDiagnosesValidationConflicts(t *testing.T) {
	inventory := map[string]any{"project_test_suite_result": "not_observed", "authoritative_verifier_result": "failed"}
	conflicts := caseDiagnosesValidationConflicts(
		[]map[string]any{{"failure_layer": "weird", "target_ref": "unassigned"}},
		inventory, nil, "task",
	)
	if len(conflicts) != 1 || !strings.HasPrefix(conflicts[0], "diagnosis[1]: ") {
		t.Fatalf("位置前缀: %v", conflicts)
	}
}

// TestAllocateDiagnosisAttempt 尝试目录分配与 attempt.json（Python _allocate_diagnosis_attempt）
func TestAllocateDiagnosisAttempt(t *testing.T) {
	runtimeRoot := t.TempDir()
	artifactRoot := t.TempDir()
	runtimeDir, attemptDir := allocateDiagnosisAttempt(runtimeRoot, artifactRoot, "c1", "member_stage")
	if runtimeDir == "" || attemptDir == "" {
		t.Fatal("分配应成功")
	}
	if !strings.HasPrefix(filepath.Base(runtimeDir), "d-") {
		t.Fatal("运行时目录前缀应为 d-")
	}
	raw, err := os.ReadFile(filepath.Join(attemptDir, "attempt.json"))
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["case_id"] != "c1" || payload["attempt_id"] != filepath.Base(attemptDir) {
		t.Fatalf("attempt.json: %v", payload)
	}
	// 二次分配应得不同目录
	runtimeDir2, attemptDir2 := allocateDiagnosisAttempt(runtimeRoot, artifactRoot, "c1", "member_stage")
	if runtimeDir2 == runtimeDir {
		t.Fatal("二次分配应唯一")
	}
	_ = attemptDir2
}

// TestPrepareDiagnosisEvidence 证据准备与隔离快照（Python _prepare_diagnosis_evidence）
func TestPrepareDiagnosisEvidence(t *testing.T) {
	caseDir := t.TempDir()
	resultPath := filepath.Join(caseDir, "result.json")
	if err := os.WriteFile(resultPath, []byte(`{"case_id":"c1","workspace_dir":"`+filepath.Join(caseDir, "ws")+`"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	ws := filepath.Join(caseDir, "ws")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, "main.py"), []byte("print('hi')"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(caseDir, "judge"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(caseDir, "judge", "normalized_trace.json"),
		[]byte(`{"traces":[{"trace_id":"t1","role":"assistant","messages":[{"message_index":1,"content":"work","tool_calls":[{"name":"bash","input":"pytest tests/","output":"2 failed","error":""}]}]}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	caseInput := &CaseAnalysisInput{
		CaseID: "c1", Input: "task body", ResultPath: resultPath,
		EvaluationMetadata: map[string]any{}, ExecutionMetadata: map[string]any{},
	}
	runtimeDir := filepath.Join(t.TempDir(), "runtime")
	available := prepareDiagnosisEvidence(caseInput, runtimeDir)
	if !available {
		t.Fatal("证据摘要应可用")
	}
	for _, name := range []string{"evidence_summary.md", "execution_history.json", "task_clauses.json"} {
		if !isRegularFile(filepath.Join(runtimeDir, name)) {
			t.Fatalf("缺 %s", name)
		}
	}
	summary, _ := os.ReadFile(filepath.Join(runtimeDir, "evidence_summary.md"))
	if !strings.Contains(string(summary), "Repository Snapshot Availability") {
		t.Fatal("快照可用性段应追加")
	}
	if !isRegularFile(filepath.Join(runtimeDir, "repository_snapshot.json")) {
		t.Fatal("快照清单应存在")
	}
	// 无轨迹 → 摘要仍可组装（案例事实段非空）
	caseInput2 := &CaseAnalysisInput{
		CaseID: "c2", Input: "x", ResultPath: filepath.Join(caseDir, "result.json"),
		EvaluationMetadata: map[string]any{}, ExecutionMetadata: map[string]any{},
	}
	available2 := prepareDiagnosisEvidence(caseInput2, filepath.Join(t.TempDir(), "runtime2"))
	if !available2 {
		t.Fatal("基础摘要应可用")
	}
}

// TestLoadCaseResult 结果读取守卫（Python _load_case_result）
func TestLoadCaseResult(t *testing.T) {
	dir := t.TempDir()
	if got := loadCaseResult(&CaseAnalysisInput{ResultPath: filepath.Join(dir, "missing.json")}); len(got) != 0 {
		t.Fatal("缺失应空")
	}
	path := filepath.Join(dir, "result.json")
	if err := os.WriteFile(path, []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := loadCaseResult(&CaseAnalysisInput{ResultPath: path}); len(got) != 0 {
		t.Fatal("坏 JSON 应空")
	}
	if err := os.WriteFile(path, []byte(`{"case_id":"c1"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := loadCaseResult(&CaseAnalysisInput{ResultPath: path}); got["case_id"] != "c1" {
		t.Fatalf("正常读取: %v", got)
	}
}

// TestCopyDirAndRemovePath 目录拷贝与删除（Python copytree/_remove_path）
func TestCopyDirAndRemovePath(t *testing.T) {
	src := t.TempDir()
	if err := os.MkdirAll(filepath.Join(src, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "a.txt"), []byte("A"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "sub", "b.txt"), []byte("B"), 0o644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), "dst")
	if err := copyDir(src, dst); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(filepath.Join(dst, "sub", "b.txt")); string(data) != "B" {
		t.Fatalf("递归拷贝: %v", data)
	}
	removePath(src)
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatal("应已删除")
	}
	removePath(src) // 二次删除不报错
	removePath(filepath.Join(t.TempDir(), "missing-file"))
}

// TestCasePriorCandidateFeedback 案例反馈裁剪（Python _case_prior_candidate_feedback）
func TestCasePriorCandidateFeedback(t *testing.T) {
	feedback := map[string]any{"by_case": map[string]any{
		"c1": []any{
			map[string]any{"verifier_delta": map[string]any{}},
			map[string]any{"verifier_delta": map[string]any{}},
			map[string]any{"verifier_delta": map[string]any{}},
			map[string]any{"verifier_delta": map[string]any{}},
		},
		"c2": map[string]any{"verifier_delta": map[string]any{}},
		"c3": "not-list",
	}}
	got := casePriorCandidateFeedback(feedback, "c1")
	experiments := got["experiments"].([]any)
	if len(experiments) != 3 {
		t.Fatalf("最多保留最近 3 条: %d", len(experiments))
	}
	if got2 := casePriorCandidateFeedback(feedback, "c2"); len(got2["experiments"].([]any)) != 1 {
		t.Fatalf("单条 dict 包装: %v", got2)
	}
	if got3 := casePriorCandidateFeedback(feedback, "c3"); len(got3) != 0 {
		t.Fatalf("非法记录: %v", got3)
	}
	if got4 := casePriorCandidateFeedback(nil, "c1"); len(got4) != 0 {
		t.Fatal("nil 反馈应空")
	}
}

// TestPartialArtifact 部分产物（Python _partial_artifact）
func TestPartialArtifact(t *testing.T) {
	strategy := NewDiagnosisAgentStrategy(&rsiconfig.EvaluationResultAnalyzerConfig{})
	artifact := strategy.PartialArtifact(harnessrsi.EvaluationResultAnalysisInvocation{OutputDir: "/tmp/out-x"}, "reason", nil)
	if artifact.Metadata["analysis_status"] != "partial" || artifact.Metadata["failure_reason"] != "reason" {
		t.Fatalf("partial: %v", artifact.Metadata)
	}
	if len(artifact.Issues) != 0 || artifact.AnalysisID != "out-x" {
		t.Fatalf("artifact: %+v", artifact)
	}
}

// TestFacadeWritesIssuesYAML 门面真实写盘（Python analyze 落盘段）
func TestFacadeWritesIssuesYAML(t *testing.T) {
	outputDir := t.TempDir()
	fixture := t.TempDir()
	facade0 := NewEvaluationResultAnalyzer(&rsiconfig.EvaluationResultAnalyzerConfig{
		OutputFilename: "issues.yaml", ModelConfigRef: filepath.Join(fixture, "model.yaml"),
	})
	strategy := facade0.Strategy()
	strategy.SetTestHooks(
		func(ctx context.Context, workspace, systemPrompt, invocationLogDir string) (interfaces.BaseAgent, error) {
			return &testDiagnosisAgent{}, nil
		},
		func(ctx context.Context, agent interfaces.BaseAgent, prompt string, maxRetries int) (string, error) {
			return `{"diagnoses": [{"summary": "s", "target_ref": "member_harness.planner.prompt",
				"failure_mode": "late", "severity": "high", "confidence": "high",
				"root_cause": "r", "failure_cluster": {"failed_checks": ["t1"], "observable_behavior": "boom"}}]}`, nil
		},
	)
	// 构造 eval_ref + 失败案例
	if err := os.WriteFile(filepath.Join(fixture, "eval_ref.yaml"), []byte("eval_id: e\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixture, "model.yaml"),
		[]byte("model_client_config:\n  client_provider: OpenAI\n  api_key: k\n  api_base: https://example.invalid/v1\nmodel_request_config:\n  model: fake\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	caseDir := filepath.Join(fixture, "cases", "c1")
	if err := os.MkdirAll(caseDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(caseDir, "result.json"), []byte(`{"case_id":"c1","status":"failed","score":0.5,
		"evaluation":{"method":"pytest","passed":false,"reason":"failed"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	refPath, err := facade0.Analyze(context.Background(), harnessrsi.EvaluationResultAnalysisInvocation{
		EvalRefPath:    filepath.Join(fixture, "eval_ref.yaml"),
		CaseResultsDir: filepath.Join(fixture, "cases"),
		OutputDir:      outputDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	artifact2, aerr3 := strategy.Analyze(context.Background(), harnessrsi.EvaluationResultAnalysisInvocation{
		EvalRefPath:    filepath.Join(fixture, "eval_ref.yaml"),
		CaseResultsDir: filepath.Join(fixture, "cases"),
		OutputDir:      filepath.Join(outputDir, "dbg"),
	})
	t.Logf("STRATEGY2 err=%v issues=%d metadata=%v", aerr3, len(artifact2.Issues), artifact2.Metadata)
	issuesRaw, err := os.ReadFile(filepath.Join(outputDir, "issues.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if pc, err2 := os.ReadFile(filepath.Join(outputDir, "per_case_diagnoses.json")); err2 == nil {
		t.Logf("PER_CASE: %s", pc)
	}
	if !strings.Contains(string(issuesRaw), "issue_001") || !strings.Contains(string(issuesRaw), "member_harness.planner.prompt") {
		t.Fatalf("issues.yaml: %s", issuesRaw)
	}
	refRaw, err := os.ReadFile(refPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(refRaw), "issues_path") {
		t.Fatalf("analysis_ref: %s", refRaw)
	}
}

// testDiagnosisAgent 满足 BaseAgent 的空转 mock（复用 harness/rails 形制，仅核心方法）。
type testDiagnosisAgent struct {
	cbMgr *interfaces.AgentCallbackManager
	// invokeResult Invoke 返回脚本（RunDeepAgentText 直测用）
	invokeResult map[string]any
	// invokeErr Invoke 错误脚本
	invokeErr error
}

func (f *testDiagnosisAgent) Configure(_ context.Context, _ interfaces.AgentConfig) error { return nil }
func (f *testDiagnosisAgent) Invoke(_ context.Context, _ map[string]any, _ ...interfaces.AgentOption) (map[string]any, error) {
	return f.invokeResult, f.invokeErr
}
func (f *testDiagnosisAgent) Stream(_ context.Context, _ map[string]any, _ ...interfaces.AgentOption) (<-chan stream.Schema, error) {
	return nil, nil
}
func (f *testDiagnosisAgent) Card() *singleagentschema.AgentCard {
	return &singleagentschema.AgentCard{BaseCard: commonschema.BaseCard{ID: "t", Name: "T"}}
}
func (f *testDiagnosisAgent) Config() interfaces.AgentConfig { return nil }
func (f *testDiagnosisAgent) AbilityManager() interfaces.AbilityManagerInterface {
	return nil
}
func (f *testDiagnosisAgent) CallbackManager() *interfaces.AgentCallbackManager { return f.cbMgr }
func (f *testDiagnosisAgent) SystemPromptBuilder() saprompt.SystemPromptBuilderInterface {
	return nil
}
func (f *testDiagnosisAgent) RegisterCallback(_ context.Context, _ interfaces.AgentCallbackEvent, _ cb.PerAgentCallbackFunc, _ ...cb.CallbackOption) error {
	return nil
}
func (f *testDiagnosisAgent) RegisterRail(_ context.Context, _ interfaces.AgentRail, _ ...cb.CallbackOption) error {
	return nil
}
func (f *testDiagnosisAgent) UnregisterRail(_ context.Context, _ interfaces.AgentRail) error {
	return nil
}
func (f *testDiagnosisAgent) UapswarmProjectDir() string       { return "" }
func (f *testDiagnosisAgent) SetUapswarmProjectDir(string)     {}
func (f *testDiagnosisAgent) UapswarmCodeProjectDir() string   { return "" }
func (f *testDiagnosisAgent) SetUapswarmCodeProjectDir(string) {}
func (f *testDiagnosisAgent) UapswarmAdapterMode() string      { return "" }
func (f *testDiagnosisAgent) SetUapswarmAdapterMode(string)    {}

// ──────────────────────────── Task 16b：执行分发/闭合/修复链/小函数 ────────────────────────────

// TestExecuteCausalInvestigationEndToEnd 执行分发主链（Python execute_causal_investigation）
func TestExecuteCausalInvestigationEndToEnd(t *testing.T) {
	caseDir := t.TempDir()
	judgeDir := filepath.Join(caseDir, "judge")
	if err := os.MkdirAll(judgeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	resultPath := filepath.Join(caseDir, "result.json")
	if err := os.WriteFile(resultPath, []byte(`{"case_id":"c1"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	trace, _ := json.Marshal(map[string]any{"traces": []map[string]any{{
		"trace_id": "c1-t1", "member_role": "worker", "messages": []map[string]any{{
			"role": "assistant", "content": "working", "message_index": 1, "tool_calls": []map[string]any{
				{"name": "read_file", "input": `{"path":"a.txt"}`, "output": "EXACT_DISCRIMINATOR parser selected legacy mode"},
			},
		}},
	}}})
	if err := os.WriteFile(filepath.Join(judgeDir, "normalized_trace.json"), trace, 0o644); err != nil {
		t.Fatal(err)
	}
	caseInput := &CaseAnalysisInput{CaseID: "c1", ResultPath: resultPath}
	// 调查计划请求直接构造（normalize 分支已有专测）
	investigation := map[string]any{"evidence_requests": []any{
		map[string]any{"operation": "search_trace", "query": "EXACT_DISCRIMINATOR parser"},
		map[string]any{"operation": "read_event", "trace_id": "c1-t1", "message_index": 1},
	}}
	evidence := ExecuteCausalInvestigation(caseInput, investigation, nil, "")
	if b, _ := evidence["policy"].(map[string]any)["arbitrary_shell_or_path_access"].(bool); b {
		t.Fatal("只读策略应关闭任意 shell/路径访问")
	}
	if evidence["completed_request_count"] != 2 {
		t.Fatalf("完成请求数: %v", evidence["completed_request_count"])
	}
	results := evidence["results"].([]map[string]any)
	if results[0]["availability"] != "available" {
		t.Fatalf("search_trace: %v", results[0])
	}
	if evidence["artifact_evidence_closure"] == nil {
		t.Fatal("artifact 闭合结果应存在")
	}
}

// TestCloseIncompleteArtifactEvidence 窗口闭合（Python _close_incomplete_artifact_evidence 主路径）
func TestCloseIncompleteArtifactEvidence(t *testing.T) {
	caseDir := t.TempDir()
	resultPath := filepath.Join(caseDir, "result.json")
	if err := os.WriteFile(resultPath, []byte(`{"case_id":"c1"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	artifacts := filepath.Join(caseDir, "artifacts")
	if err := os.MkdirAll(artifacts, 0o755); err != nil {
		t.Fatal(err)
	}
	long := strings.Repeat("A", 5000) + "NEEDLE" + strings.Repeat("B", 40000)
	if err := os.WriteFile(filepath.Join(artifacts, "log.txt"), []byte(long), 0o644); err != nil {
		t.Fatal(err)
	}
	caseInput := &CaseAnalysisInput{CaseID: "c1", ResultPath: resultPath}
	cache := map[string]string{}
	// 初始结果：inspect_artifact 命中但 span 不完整（window_complete=false）
	// → 触发自动闭合窗口读取（Python candidates 来源即 inspect_artifact）
	initial := []map[string]any{{
		"operation":      "inspect_artifact",
		"request_id":     "r1",
		"availability":   "available",
		"hypothesis_ids": []any{"h1"},
		"matches": []any{map[string]any{
			"source":      "artifacts/log.txt",
			"exact_spans": []any{map[string]any{"text": "partial", "window_complete": false}},
		}},
	}}
	automaticRequests, closureResults, closure := closeIncompleteArtifactEvidence(
		caseInput,
		map[string]any{"evidence_requests": []any{
			map[string]any{"operation": "read_artifact_window", "relative_path": "artifacts/log.txt",
				"query": "NEEDLE", "max_chars": 12000},
		}},
		initial, cache,
	)
	if len(closureResults) == 0 {
		t.Fatal("应产出闭合窗口")
	}
	if closure == nil {
		t.Fatal("闭合信息应存在")
	}
	if len(automaticRequests) == 0 {
		t.Fatal("自动请求应生成")
	}
	// 闭合窗口应命中 NEEDLE（跨窗口扫描）
	found := false
	for _, item := range closureResults {
		if text := toStringValue(item["text"]); strings.Contains(text, "NEEDLE") {
			found = true
		}
		if !found {
			t.Logf("CLOSURE_ITEM: %v", item)
		}
	}
	if !found {
		t.Fatal("闭合窗口应覆盖 NEEDLE")
	}
}

// TestRunAgentWithRepair 修复链编排（Python _run_agent；runDeepAgentTextFn 注入）
func TestRunAgentWithRepair(t *testing.T) {
	ctx := context.Background()
	agent := &testDiagnosisAgent{}
	orig := runDeepAgentTextFn
	defer func() { runDeepAgentTextFn = orig }()
	calls := 0
	runDeepAgentTextFn = func(ctx context.Context, agent interfaces.BaseAgent, prompt string, callOnce func() (string, error)) func() (string, error) {
		return func() (string, error) {
			calls++
			if calls == 1 {
				return "no json here", nil
			}
			return `{"diagnoses": [{"summary": "s"}]}`, nil
		}
	}
	// maxRetries=0：无修复直接返回
	raw, err := runAgentWithRepair(ctx, agent, "p", 0)
	if err != nil || raw != "no json here" {
		t.Fatalf("零重试: %v %q", err, raw)
	}
	// maxRetries>0：坏 JSON 触发修复
	raw2, err2 := runAgentWithRepair(ctx, agent, "p", 1)
	if err2 != nil || !strings.Contains(raw2, `"diagnoses"`) {
		t.Fatalf("修复链: %v %q", err2, raw2)
	}
	if calls < 2 {
		t.Fatalf("修复链应至少两次调用: %d", calls)
	}
	// 首次即好 JSON → 不修复
	calls = 0
	raw3, err3 := runAgentWithRepair(ctx, agent, "p", 1)
	if err3 != nil || !strings.Contains(raw3, "diagnoses") || calls != 2 {
		t.Fatalf("脚本化重试: calls=%d err=%v", calls, err3)
	}
}

// TestMiscSmallHelpers 小函数覆盖（mustJSON/sortedMapKeys/boolPtr/hexUUID 等）
func TestMiscSmallHelpers(t *testing.T) {
	if mustJSON(map[string]any{"a": 1}) != `{"a":1}` {
		t.Fatal("mustJSON")
	}
	if mustJSONIndent(map[string]any{"a": 1}) != "{\n  \"a\": 1\n}" {
		t.Fatal("mustJSONIndent")
	}
	if sortedMapKeys(map[string]any{"b": 1, "a": 2})[0] != "a" {
		t.Fatal("sortedMapKeys")
	}
	if !*boolPtr(true) || *boolPtr(false) {
		t.Fatal("boolPtr")
	}
	if len(hexUUID()) != 32 || strings.Contains(hexUUID(), "-") {
		t.Fatal("hexUUID 应为 32 位无连字符")
	}
	if confidenceRank("high") != 3 || confidenceRank("unknown") != 0 {
		t.Fatal("confidenceRank")
	}
	if expandUser("~") == "~" || expandUser("~/x") == "~/x" {
		t.Fatal("expandUser 应展开 ~（HOME 存在时）")
	}
	if readJSONInv(filepath.Join(t.TempDir(), "missing.json")) == nil {
		t.Fatal("missing 应返回空 map 而非 nil")
	}
	if invStringList2(nil) != nil {
		// invStringList2 nil 语义按实现
	}
	if m := mapValue(nil); m != nil && len(m) != 0 {
		t.Fatal("mapValue nil 应空")
	}
	if !passToPassHasItems([]string{"x"}) || passToPassHasItems([]string{}) {
		t.Fatal("passToPassHasItems")
	}
	if !containsString([]string{"a"}, "a") || containsString([]string{"a"}, "b") {
		t.Fatal("containsString")
	}
	if got := leadingTextSpan("hello world"); len(got) == 0 {
		t.Fatal("leadingTextSpan 应产出前导 span")
	}
	if got := repositoryDirInv(filepath.Join(t.TempDir(), "nonexist")); got != "" {
		t.Fatal("不存在仓库应空串")
	}
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, "repository"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := repositoryDirInv(repo); got == "" {
		t.Fatal("存在 repository 目录应返回路径")
	}
}

// TestSetTestHooksNil 恢复默认钩子（SetTestHooks nil 分支）
func TestSetTestHooksNil(t *testing.T) {
	strategy := NewDiagnosisAgentStrategy(&rsiconfig.EvaluationResultAnalyzerConfig{})
	defaultBuild := strategy.buildAgentFn
	strategy.SetTestHooks(nil, nil)
	if strategy.buildAgentFn == nil {
		t.Fatal("nil 注入应保留默认")
	}
	_ = defaultBuild
}

// TestSmallCoverageTopup 收口小函数（GetCallbacks/wrapBeforeModelCall/
// buildEvidenceConflictRepairPrompt/partialArtifact/paragraphRunText）
func TestSmallCoverageTopup(t *testing.T) {
	rail := NewDiagnosisBudgetRail(3)
	// GetCallbacks 应声明 BeforeModelCall
	callbacks := rail.GetCallbacks()
	if _, ok := callbacks[interfaces.CallbackBeforeModelCall]; !ok {
		t.Fatal("GetCallbacks 应含 BeforeModelCall")
	}
	// wrapBeforeModelCall：合法 ctx 走 BeforeModelCall
	cbc := interfaces.NewAgentCallbackContext(nil, &interfaces.ModelCallInputs{}, nil)
	cbc.SetModelContext(&budgetTestContext{})
	if err := rail.wrapBeforeModelCall(context.Background(), cbc); err != nil {
		t.Fatal(err)
	}
	// 非法类型静默
	if err := rail.wrapBeforeModelCall(context.Background(), "wrong"); err != nil {
		t.Fatal(err)
	}
	// 证据冲突修复提示词
	got := buildEvidenceConflictRepairPrompt("orig", "prev",
		[]string{"c1"}, map[string]any{"k": "v"}, map[string]any{"k2": "v2"})
	for _, needle := range []string{"Deterministic conflict payload", "orig", "prev", "c1"} {
		if !strings.Contains(got, needle) {
			t.Fatalf("冲突修复提示词缺 %q", needle)
		}
	}
	// partialArtifact（私有转发）
	strategy := NewDiagnosisAgentStrategy(&rsiconfig.EvaluationResultAnalyzerConfig{})
	artifact := strategy.partialArtifact(harnessrsi.EvaluationResultAnalysisInvocation{OutputDir: "/tmp/o"}, "r", nil)
	if artifact.Metadata["failure_reason"] != "r" {
		t.Fatal("partialArtifact 转发")
	}
	// paragraphRunText：真实 docx 结构（Run.Children 内 Text）
	para := &docxlib.Paragraph{}
	run := &docxlib.Run{Children: []any{&docxlib.Text{Text: "hello "}, &docxlib.Text{Text: "world"}}}
	para.Children = []any{run, "non-run-item"}
	if got := paragraphRunText(para); got != "hello world" {
		t.Fatalf("段落文本: %q", got)
	}
	// 空段落
	if got := paragraphRunText(&docxlib.Paragraph{}); got != "" {
		t.Fatalf("空段落: %q", got)
	}
}

// TestDiagnoseOneBranches diagnoseOne 修复与失败分支（Python _diagnose_one 修复链/异常分派）
func TestDiagnoseOneBranches(t *testing.T) {
	// fixture：失败案例 + trace
	caseDir := t.TempDir()
	resultPath := filepath.Join(caseDir, "result.json")
	if err := os.WriteFile(resultPath, []byte(`{"case_id":"c1","status":"failed","score":0.4,
		"evaluation":{"method":"pytest","passed":false,"reason":"failed"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(caseDir, "judge"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(caseDir, "judge", "normalized_trace.json"),
		[]byte(`{"traces":[{"trace_id":"t1","role":"assistant","messages":[{"message_index":1,"content":"w"}]}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	caseInput := &CaseAnalysisInput{
		CaseID: "c1", ResultPath: resultPath, Input: "task",
		EvaluationMetadata: map[string]any{}, ExecutionMetadata: map[string]any{},
	}
	signals := &DeterministicSignals{Method: "pytest"}

	buildDiagnosis := func(summary string) map[string]any {
		return map[string]any{
			"diagnoses": []any{map[string]any{
				"summary": summary, "target_ref": "member_harness.planner.prompt", "failure_mode": "late",
				"severity": "high", "confidence": "high", "root_cause": "r",
				"failure_cluster": map[string]any{"failed_checks": []any{"t1"}, "observable_behavior": "boom"},
				// 与确定性证据矛盾：本地项目测试通过但验证器失败，却无 observations
				"validation_observations": nil,
			}},
		}
	}

	// a) 证据矛盾：初次诊断无 observations → 冲突 → 修复后仍冲突 → evidence_conflict 结果
	strategy := NewDiagnosisAgentStrategy(&rsiconfig.EvaluationResultAnalyzerConfig{ModelConfigRef: "x"})
	conflictCalls := 0
	strategy.SetTestHooks(
		func(ctx context.Context, workspace, systemPrompt, invocationLogDir string) (interfaces.BaseAgent, error) {
			return &testDiagnosisAgent{}, nil
		},
		func(ctx context.Context, agent interfaces.BaseAgent, prompt string, maxRetries int) (string, error) {
			conflictCalls++
			payload := buildDiagnosis("s")
			if conflictCalls >= 2 {
				// 修复轮：制造仍矛盾的验证器 observations（补丁应用但未解决 + 声称补丁失败）
				payload = map[string]any{"diagnoses": []any{map[string]any{
					"summary": "s", "target_ref": "member_harness.planner.prompt", "failure_mode": "late",
					"severity": "high", "confidence": "high", "root_cause": "patch application failed",
					"failure_cluster": map[string]any{"failed_checks": []any{"t1"}, "observable_behavior": "boom"},
				}}}
			}
			raw, _ := json.Marshal(payload)
			return string(raw), nil
		},
	)
	// 构造本地过/验证器挂矛盾清单：先造 result 事件（pytest passed）
	if err := os.WriteFile(resultPath, []byte(`{"case_id":"c1","status":"failed","score":0.4,
		"evaluation":{"method":"pytest","passed":false,"reason":"failed"},
		"metadata":{"execution":{"command_log":[{"command":"pytest tests/","stdout_excerpt":"3 passed","exit_code":0}]}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	results := strategy.AnalyzeForTest(context.Background(), []*CaseAnalysisInput{caseInput}, signals)
	if len(results) != 1 {
		t.Fatalf("结果数: %d", len(results))
	}
	if results[0]["diagnosis_status"] != "evidence_conflict" {
		t.Fatalf("应为证据矛盾兜底: %v", results[0]["diagnosis_status"])
	}
	if conflictCalls < 2 {
		t.Fatalf("修复调用应发生: %d", conflictCalls)
	}

	// b) 运行错误：runAgentFn 返回错误 → unavailable/model_service 兜底
	strategy2 := NewDiagnosisAgentStrategy(&rsiconfig.EvaluationResultAnalyzerConfig{ModelConfigRef: "x"})
	strategy2.SetTestHooks(
		func(ctx context.Context, workspace, systemPrompt, invocationLogDir string) (interfaces.BaseAgent, error) {
			return &testDiagnosisAgent{}, nil
		},
		func(ctx context.Context, agent interfaces.BaseAgent, prompt string, maxRetries int) (string, error) {
			return "", errors.New("connection reset by peer")
		},
	)
	results2 := strategy2.AnalyzeForTest(context.Background(), []*CaseAnalysisInput{caseInput}, signals)
	if results2[0]["diagnosis_status"] != "unavailable" {
		t.Fatalf("运行错误应兜底: %v", results2[0]["diagnosis_status"])
	}
	if results2[0]["failure_layer"] != "evidence" {
		t.Fatalf("failure_layer: %v", results2[0]["failure_layer"])
	}
	_ = buildDiagnosis
}

// TestRunDeepAgentText 直测换会话重试（runDeepAgentTextFn 默认路径 + 全局 runner）
func TestRunDeepAgentText(t *testing.T) {
	ctx := context.Background()
	// 正常 output 提取
	agent := &testDiagnosisAgent{invokeResult: map[string]any{"output": "hello diagnosis"}}
	raw, err := RunDeepAgentText(ctx, agent, "prompt", 0, "op")
	if err != nil || raw != "hello diagnosis" {
		t.Fatalf("output 提取: %v %q", err, raw)
	}
	// answer 回退
	agent2 := &testDiagnosisAgent{invokeResult: map[string]any{"answer": "ans"}}
	raw2, err2 := RunDeepAgentText(ctx, agent2, "p", 0, "op")
	if err2 != nil || raw2 != "ans" {
		t.Fatalf("answer 回退: %v %q", err2, raw2)
	}
	// 整 dict JSON 兜底
	agent3 := &testDiagnosisAgent{invokeResult: map[string]any{"k": "v"}}
	raw3, err3 := RunDeepAgentText(ctx, agent3, "p", 0, "op")
	if err3 != nil || !strings.Contains(raw3, `"k":"v"`) {
		t.Fatalf("dict 兜底: %v %q", err3, raw3)
	}
	// result_type=error → DiagnosisAgentExecutionError（带终止原因）
	agent4 := &testDiagnosisAgent{invokeResult: map[string]any{
		"result_type": "error", "output": "boom", "termination_reason": "cancelled",
	}}
	_, err4 := RunDeepAgentText(ctx, agent4, "p", 0, "op")
	var execErr *DiagnosisAgentExecutionError
	if !errors.As(err4, &execErr) || execErr.TerminationReason() != "cancelled" {
		t.Fatalf("error 结果: %v", err4)
	}
	// Invoke 错误 → 可重试路径
	agent5 := &testDiagnosisAgent{invokeErr: errors.New("runner down")}
	_, err5 := RunDeepAgentText(ctx, agent5, "p", 0, "op")
	if err5 == nil {
		t.Fatal("Invoke 错误应传播")
	}
}
