// analyzer.go 评估结果诊断分析门面（17.4b2 Task 11 起）。
// Python: openjiuwen/rsi/harness_rsi/evaluation_result_analyzer/analyzer.py（3,352 行）。
// 上游吃 evaluator 成绩单，逐案深挖失败证据链，产出结构化病历（TeamIssue）。
// 本文件对应 Python 常量面板、提示词构建函数与证据组装辅助区（analyzer.py:78-87、
// 526-757、760-1053、1788-2099、2315-2432）；策略与门面在后续段（Task 12/13）。
package evaluationresultanalyzer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/interfaces"
	harnessrsi "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi/harness_rsi"
	"gopkg.in/yaml.v3"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	rsiconfig "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi/harness_rsi/config"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// textSnippetChars 文本片段预算（Python _TEXT_SNIPPET_CHARS）
	textSnippetChars = 1200
	// metadataSnippetChars 元数据片段预算（Python _METADATA_SNIPPET_CHARS）
	metadataSnippetChars = 2000
	// experienceSnippetChars 经验片段预算（Python _EXPERIENCE_SNIPPET_CHARS）
	experienceSnippetChars = 2000
	// candidateFeedbackChars 先验候选反馈预算（Python _CANDIDATE_FEEDBACK_CHARS）
	candidateFeedbackChars = 8000
	// signalSnippetChars 信号片段预算（Python _SIGNAL_SNIPPET_CHARS）
	signalSnippetChars = 2500
	// evidenceSummaryChars 证据摘要预算（Python _EVIDENCE_SUMMARY_CHARS）
	evidenceSummaryChars = 6000
	// aggregationDiagnosisChars 聚合诊断片段预算（Python _AGGREGATION_DIAGNOSIS_CHARS）
	aggregationDiagnosisChars = 1200
	// aggregationSignalChars 聚合信号预算（Python _AGGREGATION_SIGNAL_CHARS）
	aggregationSignalChars = 4000
	// rawOutputChars 原始输出预算（Python _RAW_OUTPUT_CHARS）
	rawOutputChars = 512
	// maxDiagnosesPerCase 单案诊断数上限（Python _MAX_DIAGNOSES_PER_CASE）
	maxDiagnosesPerCase = 3
)

// ──────────────────────────── 非导出函数 ────────────────────────────

// truncateText 带截断标记的有界文本（Python _truncate_text）
func truncateText(value any, limit int) string {
	text := toStringValue(value)
	if len(text) <= limit {
		return text
	}
	omitted := len(text) - limit
	return fmt.Sprintf("%s\n...[truncated %d chars]", text[:limit], omitted)
}

// oneLine 压平为单行（Python _one_line）
func oneLine(value any, limit int) string {
	return strings.Join(strings.Fields(truncateText(value, limit)), " ")
}

// safeFloat 证据摘要过滤用的分数解析（Python _safe_float；解析失败按 1.0 处理）
func safeFloat(value any) float64 {
	switch v := value.(type) {
	case float64:
		return v
	case int:
		return float64(v)
	case string:
		if f, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
			return f
		}
	}
	return 1.0
}

// readTextIfExists 文本文件读取（Python _read_text_if_exists）
func readTextIfExists(path string, limit int) string {
	if !isRegularFile(path) {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return truncateText(string(data), limit)
}

// readJSONIfExists JSON 文件读取（Python _read_json_if_exists）
func readJSONIfExists(path string) map[string]any {
	if !isRegularFile(path) {
		return map[string]any{}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return map[string]any{}
	}
	var value map[string]any
	if json.Unmarshal(data, &value) != nil || value == nil {
		return map[string]any{}
	}
	return value
}

// boundedJSON 序列化后限长（Python _bounded_json）
func boundedJSON(value any, limit int) string {
	raw, err := json.Marshal(value)
	if err != nil {
		return truncateText(fmt.Sprintf("%v", value), limit)
	}
	return truncateText(string(raw), limit)
}

// boundedStructuredValue 结构完整保留，超限才降级为截断 JSON（Python _bounded_structured_value）
func boundedStructuredValue(value any, limit int) any {
	raw, err := json.Marshal(value)
	if err != nil {
		return value
	}
	if len(raw) <= limit {
		return value
	}
	return map[string]any{"truncated_json": truncateText(string(raw), limit)}
}

// caseScopedList 案例清单按当前案例收窄（Python _case_scoped_list）
func caseScopedList(items []string, caseID string) []string {
	for _, item := range items {
		if item == caseID {
			return []string{caseID}
		}
	}
	return []string{}
}

// caseScopedErrorClusters 错误聚类按当前案例过滤（Python _case_scoped_error_clusters）
func caseScopedErrorClusters(clusters []map[string]any, caseID string) []map[string]any {
	var scoped []map[string]any
	for _, cluster := range clusters {
		cases, _ := cluster["cases"].([]any)
		for _, c := range cases {
			if s, isStr := c.(string); isStr && s == caseID {
				scoped = append(scoped, map[string]any{
					"fingerprint": cluster["fingerprint"],
					"cases":       []string{caseID},
				})
				break
			}
		}
	}
	return scoped
}

// caseScopedMethodSpecific 方法专属信号按当前案例收窄（Python _case_scoped_method_specific）
func caseScopedMethodSpecific(metadata map[string]any, caseID string) map[string]any {
	scoped := map[string]any{}
	for key, value := range metadata {
		scoped[key] = caseScopedValue(value, caseID)
	}
	return scoped
}

// caseScopedValue 值按当前案例收窄（Python _case_scoped_value）
func caseScopedValue(value any, caseID string) any {
	switch v := value.(type) {
	case map[string]any:
		if _, ok := v[caseID]; ok {
			return map[string]any{caseID: v[caseID]}
		}
		out := map[string]any{}
		for key, item := range v {
			if valueMentionsCase(item, caseID) {
				out[key] = caseScopedValue(item, caseID)
			}
		}
		return out
	case []any:
		for _, item := range v {
			if s, isStr := item.(string); isStr && s == caseID {
				return []string{caseID}
			}
		}
		var out []any
		for _, item := range v {
			if m, isMap := item.(map[string]any); isMap && valueMentionsCase(m, caseID) {
				out = append(out, item)
			}
		}
		return out
	case []map[string]any:
		var out []any
		for _, m := range v {
			if valueMentionsCase(m, caseID) {
				out = append(out, m)
			}
		}
		return out
	case []string:
		for _, s := range v {
			if s == caseID {
				return []string{caseID}
			}
		}
		return value
	}
	return value
}

// valueMentionsCase 值中是否提及当前案例（Python _value_mentions_case）
func valueMentionsCase(value any, caseID string) bool {
	switch v := value.(type) {
	case map[string]any:
		for _, item := range v {
			if valueMentionsCase(item, caseID) {
				return true
			}
		}
		return false
	case []any:
		for _, item := range v {
			if valueMentionsCase(item, caseID) {
				return true
			}
		}
		return false
	case []map[string]any:
		for _, item := range v {
			if valueMentionsCase(item, caseID) {
				return true
			}
		}
		return false
	case []string:
		for _, s := range v {
			if s == caseID {
				return true
			}
		}
		return false
	case string:
		return v == caseID
	}
	return false
}

// summarizeEvaluationMetadata 从评估元数据提取 judge 分解（Python _summarize_evaluation_metadata）。
// 无可识别的 LLM-as-judge 字段时返回空 map；从不写入 raw_output 或完整评估元数据。
func summarizeEvaluationMetadata(metadata map[string]any) map[string]any {
	parsed, _ := metadata["parsed"].(map[string]any)
	if parsed == nil {
		return map[string]any{}
	}

	var behaviors []map[string]any
	behaviorsRaw, _ := parsed["behaviors"].([]any)
	for _, entryAny := range behaviorsRaw {
		entry, ok := entryAny.(map[string]any)
		if !ok {
			continue
		}
		behaviors = append(behaviors, map[string]any{
			"id":                     entry["id"],
			"score":                  entry["score"],
			"description":            entry["description"],
			"weight":                 entry["weight"],
			"reason":                 truncateText(entry["reason"], textSnippetChars),
			"failure_reason":         truncateText(entry["failure_reason"], textSnippetChars),
			"missing_capability":     truncateText(entry["missing_capability"], textSnippetChars),
			"suggested_surface_hint": truncateText(entry["suggested_surface_hint"], 80),
			"evidence":               entry["evidence"],
		})
	}

	overallReason := stringValue(parsed["overall_reason"])
	forbiddenHits, _ := parsed["forbidden_hits"].([]any)
	qualityGaps := compactQualityGaps(parsed["quality_gaps"])
	datasetBudget := compactDatasetBudget(parsed["dataset_budget"])
	dimensions := compactJudgeDimensions(parsed["dimensions"])
	criteria := compactJudgeCriteria(metadata)

	if len(behaviors) == 0 && overallReason == "" && len(forbiddenHits) == 0 &&
		len(qualityGaps) == 0 && len(criteria) == 0 {
		return map[string]any{}
	}

	result := map[string]any{
		"behaviors":      behaviors,
		"overall_reason": truncateText(overallReason, textSnippetChars),
	}
	if forbiddenHits != nil {
		result["forbidden_hits"] = forbiddenHits
	} else {
		result["forbidden_hits"] = []any{}
	}
	if len(qualityGaps) > 0 {
		result["quality_gaps"] = qualityGaps
	}
	if len(datasetBudget) > 0 {
		result["dataset_budget"] = datasetBudget
	}
	if len(dimensions) > 0 {
		result["dimensions"] = dimensions
	}
	if len(criteria) > 0 {
		result["criteria"] = criteria
	}
	if _, ok := parsed["quality_gap_score_ceiling"]; ok {
		result["quality_gap_score_ceiling"] = parsed["quality_gap_score_ceiling"]
	}
	if _, ok := parsed["overall_score"]; ok {
		result["overall_score"] = parsed["overall_score"]
	}
	for _, key := range []string{"base_score", "penalty_mode", "total_deduction"} {
		if _, ok := parsed[key]; ok {
			result[key] = parsed[key]
		}
	}
	return result
}

// compactJudgeCriteria 判据压缩（Python _compact_judge_criteria）
func compactJudgeCriteria(metadata map[string]any) []map[string]any {
	var rawCriteria []any
	normalized, _ := metadata["judge_evidence"].(map[string]any)
	if normalized != nil {
		rawCriteria, _ = normalized["criteria"].([]any)
	}
	if rawCriteria == nil {
		rawDetail, _ := metadata["judge_detail"].(map[string]any)
		if rawDetail != nil {
			rawCriteria, _ = rawDetail["criteria"].([]any)
		}
	}
	if rawCriteria == nil {
		return []map[string]any{}
	}
	criteria := make([]map[string]any, 0, len(rawCriteria))
	for index, rawAny := range rawCriteria {
		raw, ok := rawAny.(map[string]any)
		if !ok {
			continue
		}
		criterionID := stringValue(raw["criterion_id"])
		if criterionID == "" {
			criterionID = stringValue(raw["verifier_id"])
		}
		if criterionID == "" {
			criterionID = fmt.Sprintf("criterion_%d", index+1)
		}
		var passed any
		if b, isBool := raw["passed"].(bool); isBool {
			passed = b
		}
		criteria = append(criteria, map[string]any{
			"criterion_id": criterionID,
			"verifier_id":  stringValue(raw["verifier_id"]),
			"score":        raw["score"],
			"passed":       passed,
			"status":       stringValue(raw["status"]),
			"rationale":    truncateText(raw["rationale"], textSnippetChars),
		})
	}
	return criteria
}

// compactQualityGaps 质量缺口压缩（Python _compact_quality_gaps）。
// verification_gap 仅保留在 judge 结果里供观测——评估器证据覆盖不是
// member/team 优化器可修的对象；把它传给诊断曾导致 interaction_smoke
// 这类适配器自有工具被误归为 member harness 工具，产出不可能的工具动作。
func compactQualityGaps(value any) []map[string]any {
	list, ok := value.([]any)
	if !ok {
		return []map[string]any{}
	}
	var gaps []map[string]any
	for i, entryAny := range list {
		if i >= 8 {
			break
		}
		entry, isMap := entryAny.(map[string]any)
		if !isMap {
			continue
		}
		gapType := strings.ToLower(strings.TrimSpace(stringValue(entry["gap_type"])))
		if gapType == "verification_gap" {
			continue
		}
		gaps = append(gaps, map[string]any{
			"id":                       entry["id"],
			"gap_type":                 entry["gap_type"],
			"dimension":                entry["dimension"],
			"severity":                 entry["severity"],
			"affected_roles":           stringList(entry["affected_roles"], 8),
			"likely_surfaces":          stringList(entry["likely_surfaces"], 8),
			"evidence":                 truncateText(entry["evidence"], textSnippetChars),
			"missing_capability":       truncateText(entry["missing_capability"], textSnippetChars),
			"why_it_matters":           truncateText(entry["why_it_matters"], textSnippetChars),
			"data_needed_to_fix":       truncateText(entry["data_needed_to_fix"], textSnippetChars),
			"training_signal_priority": entry["training_signal_priority"],
		})
	}
	return gaps
}

// compactDatasetBudget 数据集预算路由提示压缩（Python _compact_dataset_budget）
func compactDatasetBudget(value any) map[string]any {
	v, ok := value.(map[string]any)
	if !ok {
		return map[string]any{}
	}
	var compactGroups []map[string]any
	groups, _ := v["case_groups"].([]any)
	for i, groupAny := range groups {
		if i >= 8 {
			break
		}
		group, isMap := groupAny.(map[string]any)
		if !isMap {
			continue
		}
		compactGroups = append(compactGroups, map[string]any{
			"source_gap":      group["source_gap"],
			"case_count":      group["case_count"],
			"target_roles":    stringList(group["target_roles"], 8),
			"target_surfaces": stringList(group["target_surfaces"], 8),
		})
	}
	result := map[string]any{}
	if _, ok := v["total_cases"]; ok {
		result["total_cases"] = v["total_cases"]
	}
	if len(compactGroups) > 0 {
		result["case_groups"] = compactGroups
	}
	return result
}

// compactJudgeDimensions 分数诊断保留（Python _compact_judge_dimensions）
func compactJudgeDimensions(value any) map[string]any {
	v, ok := value.(map[string]any)
	if !ok {
		return map[string]any{}
	}
	result := map[string]any{}
	for _, key := range []string{"low_score_behaviors", "avg_behavior_score", "behavior_count", "pass_count", "fail_count"} {
		if _, ok := v[key]; ok {
			result[key] = v[key]
		}
	}
	if _, ok := v["triggered_forbidden_behaviors"]; ok {
		result["triggered_forbidden_behaviors"] = v["triggered_forbidden_behaviors"]
	}
	if perBehaviorScores, ok := v["per_behavior_scores"].(map[string]any); ok {
		keys := make([]string, 0, len(perBehaviorScores))
		for key := range perBehaviorScores {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		if len(keys) > 12 {
			keys = keys[:12]
		}
		clipped := map[string]any{}
		for _, key := range keys {
			clipped[key] = perBehaviorScores[key]
		}
		result["per_behavior_scores"] = clipped
	}
	return result
}

// compactRetrievedExperience 经验检索提示压缩（Python _compact_retrieved_experience）
func compactRetrievedExperience(retrievedExperience map[string]any) map[string]any {
	if retrievedExperience == nil {
		return map[string]any{}
	}
	compactMatches := []map[string]any{}
	matches, _ := retrievedExperience["matches"].([]any)
	for i, itemAny := range matches {
		if i >= 3 {
			break
		}
		item, isMap := itemAny.(map[string]any)
		if !isMap {
			continue
		}
		experienceID := item["experience_id"]
		if experienceID == nil || experienceID == "" {
			experienceID = item["id"]
		}
		summary := item["summary"]
		if summary == nil || summary == "" {
			summary = item["content"]
		}
		experience := item["experience"]
		if experience == nil {
			experience = map[string]any{}
		}
		metadata := item["metadata"]
		if metadata == nil {
			metadata = map[string]any{}
		}
		compactMatches = append(compactMatches, map[string]any{
			"experience_id":     experienceID,
			"component_layer":   item["component_layer"],
			"failure_signature": item["failure_signature"],
			"mechanism_type":    item["mechanism_type"],
			"learning_status":   item["learning_status"],
			"summary":           truncateText(summary, 500),
			"experience":        boundedStructuredValue(experience, 800),
			"metadata":          metadata,
		})
	}
	stage := retrievedExperience["stage"]
	if stage == nil {
		stage = ""
	}
	metadata := retrievedExperience["metadata"]
	if metadata == nil {
		metadata = map[string]any{}
	}
	return map[string]any{
		"stage":    stage,
		"matches":  compactMatches,
		"metadata": metadata,
	}
}

// experienceUsagePolicy 分析 agent 使用检索经验的结构化规则（Python _experience_usage_policy）
func experienceUsagePolicy() map[string]any {
	return map[string]any{
		"must_use_current_evidence_first": true,
		"principle":                       "Current evidence remains authoritative; experience is only a bounded hint.",
		"rules": []string{
			"Do not copy a historical target_ref, component_layer, or recommendation unless current trace/verifier evidence supports it.",
			"Use retrieved general_principles only as reusable hypotheses to check.",
			"Use retrieved anti_patterns as repairs to avoid, not as new facts.",
			"If experience conflicts with current evidence, ignore the experience.",
			"Experience cannot raise confidence without current evidence_refs.",
		},
	}
}

// stageInstruction 阶段专属归因目标（Python _stage_instruction）
func stageInstruction(sourceStage string) string {
	switch {
	case sourceStage == "single_harness_candidate_failure":
		return "Analyze why the evaluated candidate did not finish the target case. " +
			"Compare it with prior_candidate_feedback: preserve official operations " +
			"that moved from failure to success, identify the remaining failed " +
			"operation, and falsify the previous mechanism against the candidate " +
			"patch. Do not re-diagnose the original Source in isolation. This is a " +
			"standalone single-Harness run with no Team Skill optimization surface. " +
			"Attribute reusable defects only to member_harness.<role>.<variable>; use " +
			"unassigned only when the evidence does not support a Harness change. " +
			"Treat candidate_behavior.gate_reason and failure_class as machine-observed " +
			"facts. If a delivered Skill or Tool was not invoked, diagnose activation " +
			"control rather than proposing another equivalent static capability."
	case sourceStage == "single_harness_residual_repair":
		return "Re-analyze the still-failing standalone Harness using the prior candidate " +
			"feedback as a falsification result. Preserve behavior that improved an " +
			"official check. If a capability was delivered but not invoked in time, " +
			"attribute the remaining defect to a runtime control surface rather than " +
			"requesting another equivalent static instruction. Valid target_ref values " +
			"are member_harness.<role>.<variable> or \"unassigned\"."
	case strings.HasPrefix(sourceStage, "single_harness_"):
		return "Analyze the concrete reusable defect in this standalone Harness. There " +
			"is no Team Skill optimization surface in this run. Valid target_ref " +
			"values are member_harness.<role>.<variable> or \"unassigned\". Do not emit " +
			"team_skill targets."
	case sourceStage == "member_stage":
		return "Analyze concrete member harness capability under the current Team Skill. " +
			"Attribute to a specific role's prompt, skill, tool, rail, or restricted runtime config. Valid target_ref " +
			"values are member_harness.<role>.<variable> or \"unassigned\"."
	case sourceStage == "team_skill_stage":
		return "Analyze team organization, role boundaries, collaboration flow, and " +
			"deliverable contract. Attribute Team Skill issues to team_skill.<role>.<variable>; " +
			"use member_harness.<role>.<variable> only for local member capability gaps."
	}
	return "Analyze the concrete optimizable variable supported by current evidence. " +
		"Use member_harness.<role>.<variable>, team_skill.<role>.<variable>, or \"unassigned\"."
}

// buildDiagnosisPrompt 构建逐案诊断提示词（Python _build_diagnosis_prompt）。
// 确定性输入随提示词内联；运行时还可持有隔离的 repository/ 快照，
// 供诊断 agent 对照被评估代码证伪语义假设而不触碰它。
func buildDiagnosisPrompt(
	caseInput *CaseAnalysisInput,
	signals *DeterministicSignals,
	retrievedExperience map[string]any,
	evidenceSummaryAvailable bool,
	sourceStage string,
	priorCandidateFeedback map[string]any,
	harnessContext map[string]any,
) string {
	var evidenceInstruction string
	if evidenceSummaryAvailable {
		evidenceInstruction = "> Use primary_evidence.evidence_summary_text from the inline JSON.\n" +
			"> It contains verifier outcome, judge quality gaps, decisive failed steps, " +
			"and bounded key events.\n" +
			"> If repository/ exists, inspect its relevant source and public tests and run " +
			"a bounded read-only discriminator before selecting a semantic mechanism.\n" +
			"> Do not read trace.json, result.json, hidden tests, gold patches, or paths " +
			"outside this runtime workspace."
	} else {
		evidenceInstruction = "> No evidence_summary.md is available in this runtime workspace.\n" +
			"> Start from the inline JSON. If repository/ exists, use it only to test " +
			"competing hypotheses; otherwise return unassigned when semantics remain ambiguous."
	}

	prompt := formatTemplate(perCaseDiagnosisTemplate, map[string]string{
		"stage_instruction":      stageInstruction(sourceStage),
		"evidence_instruction":   evidenceInstruction,
		"max_diagnoses_per_case": strconv.Itoa(maxDiagnosesPerCase),
		"diagnosis_input": buildDiagnosisInputJSON(
			caseInput, signals, retrievedExperience, evidenceSummaryAvailable,
			sourceStage, priorCandidateFeedback, harnessContext,
		),
	})
	if evidenceSummaryAvailable {
		return executionHistoryNotice + prompt
	}
	return prompt
}

// buildDiagnosisInputJSON 构建逐案诊断提示词的有界内联 JSON（Python _build_diagnosis_input_json）
func buildDiagnosisInputJSON(
	caseInput *CaseAnalysisInput,
	signals *DeterministicSignals,
	retrievedExperience map[string]any,
	evidenceSummaryAvailable bool,
	sourceStage string,
	priorCandidateFeedback map[string]any,
	harnessContext map[string]any,
) string {
	judgeBreakdown := summarizeEvaluationMetadata(caseInput.EvaluationMetadata)
	evidenceSummaryText := ""
	if evidenceSummaryAvailable {
		evidenceSummaryText = truncateText(buildEvidenceSummary(caseInput), evidenceSummaryChars)
	}
	validationInventory := buildValidationInventory(caseInput)
	verifierInventory := buildVerifierInventory(caseInput)
	if harnessContext == nil {
		harnessContext = map[string]any{"status": "not_provided", "roles": []any{}}
	}
	if priorCandidateFeedback == nil {
		priorCandidateFeedback = map[string]any{}
	}
	execFailures := caseScopedList(signals.ExecFailures, caseInput.CaseID)
	judgeFailures := caseScopedList(signals.JudgeFailures, caseInput.CaseID)
	terminationReason := caseInput.TerminationReason
	if terminationReason == "" {
		terminationReason = "unknown"
	}
	payload := map[string]any{
		"current_harness": harnessContext,
		"authoritative_task_contract": map[string]any{
			"provenance":    "case.input",
			"input_excerpt": caseInput.Input,
			"clauses":       BuildTaskClauses(caseInput.Input),
			"policy": ("Only this excerpt establishes what the original task or reproduction " +
				"contains. Agent-authored commands and probes are execution evidence, " +
				"not task-contract facts."),
		},
		"execution_context": ReadExecutionContext(
			filepath.Dir(caseInput.ResultPath), caseInput.ExecutionMetadata, caseInput.TerminationReason,
		),
		"runtime_evidence_manifest":             "evidence_manifest.json",
		"authoritative_benchmark_test_contract": caseInput.BenchmarkTestContract,
		"primary_evidence": map[string]any{
			"evidence_summary_available": evidenceSummaryAvailable,
			"evidence_summary_path":      boolToEvidencePath(evidenceSummaryAvailable),
			"evidence_summary_text":      evidenceSummaryText,
		},
		"deterministic_validation_inventory": validationInventory,
		"deterministic_verifier_inventory":   verifierInventory,
		"prior_candidate_feedback":           boundedStructuredValue(priorCandidateFeedback, candidateFeedbackChars),
		"prior_candidate_feedback_policy": ("Treat paired official test deltas as authoritative experiment " +
			"evidence. Preserve newly passing operations. Diagnose newly regressed " +
			"checks first, then remaining failures. A historical candidate win " +
			"does not override failure or missing output in the current evaluation. Candidate " +
			"diagnoses are hypotheses unless the verifier delta independently " +
			"supports them."),
		"analysis_stage": func() string {
			if sourceStage != "" {
				return sourceStage
			}
			return "unknown"
		}(),
		"anchor_signals": map[string]any{
			"method":         signals.Method,
			"exec_failures":  execFailures,
			"judge_failures": judgeFailures,
			"error_clusters": boundedStructuredValue(
				caseScopedErrorClusters(signals.ErrorClusters, caseInput.CaseID),
				signalSnippetChars,
			),
			"method_specific": boundedStructuredValue(
				caseScopedMethodSpecific(signals.MethodSpecific, caseInput.CaseID),
				signalSnippetChars,
			),
		},
		"case_facts": map[string]any{
			"case_id":                     caseInput.CaseID,
			"status":                      caseInput.Status,
			"score":                       caseInput.Score,
			"evaluation_passed":           caseInput.EvaluationPassed,
			"recorded_termination_reason": terminationReason,
			"evaluation_reason":           truncateText(caseInput.EvaluationReason, textSnippetChars),
			"error":                       truncateText(caseInput.Error, textSnippetChars),
			"judge_breakdown":             judgeBreakdown,
			"training_signal":             boundedStructuredValue(caseInput.TrainingSignal, metadataSnippetChars),
		},
		"fallback_excerpts": map[string]any{
			"input_excerpt":    caseInput.Input,
			"response_excerpt": truncateText(caseInput.Response, textSnippetChars),
		},
		"retrieved_experience": boundedStructuredValue(
			compactRetrievedExperience(retrievedExperience),
			experienceSnippetChars,
		),
		"experience_usage_policy": experienceUsagePolicy(),
	}
	raw, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return "{}"
	}
	return string(raw)
}

// boolToEvidencePath 证据摘要路径（Python "evidence_summary.md" if available else ""）
func boolToEvidencePath(available bool) string {
	if available {
		return "evidence_summary.md"
	}
	return ""
}

// buildAggregationPrompt 构建聚合会诊提示词（Python _build_aggregation_prompt）
func buildAggregationPrompt(
	summary *EvaluationSummaryInput,
	perCaseDiagnoses []map[string]any,
	signals *DeterministicSignals,
	retrievedExperience map[string]any,
	maxIssues int,
	evidenceLimitPerIssue int,
	sourceStage string,
) string {
	diagnosisBudget := aggregationDiagnosisChars * len(perCaseDiagnoses)
	if diagnosisBudget < aggregationDiagnosisChars {
		diagnosisBudget = aggregationDiagnosisChars
	}
	experiencePayload := map[string]any{
		"policy":               experienceUsagePolicy(),
		"retrieved_experience": compactRetrievedExperience(retrievedExperience),
	}
	return formatTemplate(aggregationTemplate, map[string]string{
		"total_cases":              strconv.Itoa(summary.TotalCases),
		"passed_count":             strconv.Itoa(summary.PassedCount),
		"failed_count":             strconv.Itoa(summary.FailedCount),
		"average_score":            strconv.FormatFloat(summary.AverageScore, 'g', -1, 64),
		"evaluation_method":        summary.EvaluationMethod,
		"per_case_diagnoses":       boundedJSON(compactPerCaseDiagnoses(perCaseDiagnoses), diagnosisBudget), // compactPerCaseDiagnoses Task 12 落地
		"retrieved_experience":     boundedJSON(experiencePayload, experienceSnippetChars),
		"exec_failures":            boundedJSON(signals.ExecFailures, aggregationSignalChars),
		"judge_failures":           boundedJSON(signals.JudgeFailures, aggregationSignalChars),
		"error_clusters":           boundedJSON(signals.ErrorClusters, aggregationSignalChars),
		"method_specific":          boundedJSON(signals.MethodSpecific, aggregationSignalChars),
		"max_issues":               strconv.Itoa(maxIssues),
		"evidence_limit_per_issue": strconv.Itoa(evidenceLimitPerIssue),
		"stage_instruction":        stageInstruction(sourceStage),
	})
}

// buildEvidenceSummary 从归一化轨迹与验证器输出构建有界证据摘要（Python _build_evidence_summary）
func buildEvidenceSummary(caseInput *CaseAnalysisInput) string {
	caseDir := filepath.Dir(caseInput.ResultPath)
	verifierDir := filepath.Join(caseDir, "verifier")
	normalizedTracePath := filepath.Join(caseDir, "judge", "normalized_trace.json")

	lines := []string{
		"# Analyzer Evidence Summary",
		"",
		"## Authoritative Task Contract",
		"- provenance: case.input (user/benchmark supplied)",
		"- Only this section establishes what the original reproduction contains.",
		"- Commands and probes in Agent-Generated Execution Evidence are not task facts.",
		"```text",
		caseInput.Input,
		"```",
	}
	if len(caseInput.BenchmarkTestContract) > 0 {
		contract := caseInput.BenchmarkTestContract
		lines = append(lines,
			"",
			"## Authoritative Benchmark Test Contract",
			fmt.Sprintf("- provenance: %s", stringValue(contract["provenance"])),
			"- This is acceptance-test evidence, not the benchmark solution patch.",
			"- FAIL_TO_PASS:",
			"```json",
			mustJSONIndent(contract["fail_to_pass"]),
			"```",
			"- PASS_TO_PASS:",
			"```json",
			mustJSONIndent(contract["pass_to_pass"]),
			"```",
			"- test_patch:",
			"```diff",
			stringValue(contract["test_patch"]),
			"```",
		)
	}
	lines = append(lines,
		"",
		"## Case Facts",
		fmt.Sprintf("- case_id: %s", caseInput.CaseID),
		fmt.Sprintf("- status: %s", caseInput.Status),
		fmt.Sprintf("- score: %s", strconv.FormatFloat(caseInput.Score, 'g', -1, 64)),
		fmt.Sprintf("- evaluation_passed: %v", caseInput.EvaluationPassed),
		fmt.Sprintf("- evaluation_method: %s", caseInput.EvaluationMethod),
		fmt.Sprintf("- evaluation_reason: %s", oneLine(caseInput.EvaluationReason, 500)),
	)
	if caseInput.Error != "" {
		lines = append(lines, fmt.Sprintf("- execution_error: %s", oneLine(caseInput.Error, 500)))
	}

	judgeBreakdown := summarizeEvaluationMetadata(caseInput.EvaluationMetadata)
	qualityGapsAny := judgeBreakdown["quality_gaps"]
	var qualityGaps []map[string]any
	if list, ok := qualityGapsAny.([]map[string]any); ok {
		qualityGaps = list
	} else if listAny, ok := qualityGapsAny.([]any); ok {
		for _, g := range listAny {
			if m, ok := g.(map[string]any); ok {
				qualityGaps = append(qualityGaps, m)
			}
		}
	}
	if len(qualityGaps) > 0 {
		lines = append(lines, "", "## Judge Quality Gaps")
		for i, gap := range qualityGaps {
			if i >= 8 {
				break
			}
			roles := oneLine(gap["affected_roles"], 180)
			surfaces := oneLine(gap["likely_surfaces"], 180)
			lines = append(lines, "- "+
				fmt.Sprintf("id=%s ", oneLine(gap["id"], 120))+
				fmt.Sprintf("severity=%s ", oneLine(gap["severity"], 80))+
				fmt.Sprintf("roles=%s ", roles)+
				fmt.Sprintf("surfaces=%s ", surfaces)+
				fmt.Sprintf("missing_capability=%s ", oneLine(gap["missing_capability"], 300))+
				fmt.Sprintf("evidence=%s", oneLine(gap["evidence"], 500)))
			whyItMatters := oneLine(gap["why_it_matters"], 300)
			if whyItMatters != "" {
				lines = append(lines, fmt.Sprintf("  why_it_matters=%s", whyItMatters))
			}
		}
	}

	behaviorsAny := judgeBreakdown["behaviors"]
	var behaviors []map[string]any
	if list, ok := behaviorsAny.([]map[string]any); ok {
		behaviors = list
	} else if listAny, ok := behaviorsAny.([]any); ok {
		for _, b := range listAny {
			if m, ok := b.(map[string]any); ok {
				behaviors = append(behaviors, m)
			}
		}
	}
	var lowBehaviors []map[string]any
	for _, behavior := range behaviors {
		if safeFloat(behavior["score"]) < 0.8 {
			lowBehaviors = append(lowBehaviors, behavior)
		}
	}
	if len(lowBehaviors) > 0 {
		lines = append(lines, "", "## Low-Score Judge Behaviors")
		for i, behavior := range lowBehaviors {
			if i >= 8 {
				break
			}
			lines = append(lines, "- "+
				fmt.Sprintf("id=%s ", oneLine(behavior["id"], 120))+
				fmt.Sprintf("score=%v ", behavior["score"])+
				fmt.Sprintf("failure_reason=%s ", oneLine(behavior["failure_reason"], 400))+
				fmt.Sprintf("missing_capability=%s ", oneLine(behavior["missing_capability"], 300))+
				fmt.Sprintf("surface_hint=%s ", oneLine(behavior["suggested_surface_hint"], 80))+
				fmt.Sprintf("evidence=%s", oneLine(behavior["evidence"], 300)))
		}
	}

	traceData := readJSONIfExists(normalizedTracePath)
	traceEvents := summarizeNormalizedTrace(traceData)
	validationEvents := validationEventsFromResult(caseInput.ResultPath)
	if len(validationEvents) == 0 {
		validationEvents = traceEvents
	}
	validationInventory := validationInventoryFromEvents(validationEvents, caseInput.EvaluationPassed)
	lines = append(lines, "", "## Deterministic Validation Inventory")
	lines = append(lines, fmt.Sprintf("- project_test_suite_attempted: %v", validationInventory["project_test_suite_attempted"]))
	lines = append(lines, fmt.Sprintf("- project_test_suite_result: %s", validationInventory["project_test_suite_result"]))
	lines = append(lines, fmt.Sprintf("- authoritative_verifier_result: %s", validationInventory["authoritative_verifier_result"]))
	if events, ok := validationInventory["project_test_events"].([]map[string]any); ok {
		for _, event := range events {
			output := toStringValue(event["output"])
			tail := output
			if len(tail) > 700 {
				tail = tail[len(tail)-700:]
			}
			lines = append(lines, "- project_test_event: "+
				fmt.Sprintf("command=%s ", oneLine(event["command"], 500))+
				fmt.Sprintf("result=%s output_tail=", event["result"])+
				oneLine(tail, 700))
		}
	}
	if validationInventory["project_test_suite_result"] == "passed" &&
		validationInventory["authoritative_verifier_result"] == "failed" {
		lines = append(lines,
			"- hard_fact: local project tests passed while the authoritative verifier failed; "+
				"do not diagnose skipped project testing or smoke-only validation.")
	}

	verifierInventory := buildVerifierInventory(caseInput)
	if len(verifierInventory) > 0 {
		lines = append(lines, "", "## Deterministic Verifier Inventory")
		if _, ok := verifierInventory["empty_patch"]; ok {
			lines = append(lines, fmt.Sprintf("- empty_patch: %v", verifierInventory["empty_patch"]))
		}
		if _, ok := verifierInventory["patch_successfully_applied"]; ok {
			lines = append(lines, fmt.Sprintf("- patch_successfully_applied: %v", verifierInventory["patch_successfully_applied"]))
			lines = append(lines, fmt.Sprintf("- resolved: %v", verifierInventory["resolved"]))
			lines = append(lines, fmt.Sprintf("- failed_fail_to_pass_tests: %s", mustJSON(verifierInventory["failed_fail_to_pass_tests"])))
			lines = append(lines, fmt.Sprintf("- failed_pass_to_pass_tests: %s", mustJSON(verifierInventory["failed_pass_to_pass_tests"])))
		}
		failureOutput := strings.TrimSpace(toStringValue(verifierInventory["verifier_failure_output_excerpt"]))
		if failureOutput != "" {
			lines = append(lines,
				"",
				"### Authoritative failure output excerpt",
				"```text",
				truncateText(failureOutput, 3500),
				"```",
			)
		}
		if b, ok := verifierInventory["patch_successfully_applied"].(bool); ok && b {
			if resolved, ok := verifierInventory["resolved"].(bool); ok && !resolved {
				lines = append(lines,
					"- hard_fact: the patch applied successfully; do not attribute the unresolved "+
						"result to patch-application failure or working-tree contamination.")
			}
		}
	}

	reward := strings.TrimSpace(readTextIfExists(filepath.Join(verifierDir, "reward.txt"), 80))
	stdout := readTextIfExists(filepath.Join(verifierDir, "stdout.log"), 1500)
	stderr := readTextIfExists(filepath.Join(verifierDir, "stderr.log"), 1500)
	if reward != "" || stdout != "" || stderr != "" {
		lines = append(lines, "", "## Verifier Outcome")
		if reward != "" {
			lines = append(lines, fmt.Sprintf("- reward=%s", oneLine(reward, 80)))
		}
		if stderr != "" {
			lines = append(lines, "", "### stderr excerpt", "```text", truncateText(stderr, 1500), "```")
		}
		if stdout != "" {
			lines = append(lines, "", "### stdout excerpt", "```text", truncateText(stdout, 1500), "```")
		}
	}

	if len(traceEvents) > 0 {
		var failed []map[string]any
		for _, event := range traceEvents {
			if errStr, _ := event["error"].(string); errStr != "" {
				failed = append(failed, event)
			}
		}
		lines = append(lines, "", "## Agent-Generated Execution Evidence")
		lines = append(lines, "- provenance: evaluated agent trajectory; commands/probes below were agent-authored.")
		lines = append(lines, "- Do not infer original task inputs or expected semantics from these probes.")
		lines = append(lines, "", "### Decisive Failed Steps")
		if len(failed) > 0 {
			for i, event := range failed {
				if i >= 8 {
					break
				}
				lines = append(lines, formatTraceEvent(event))
			}
		} else {
			lines = append(lines, "- No failed tool call was present in the bounded normalized trace.")
		}
		lines = append(lines, "", "### Key Events")
		start := len(traceEvents) - 12
		if start < 0 {
			start = 0
		}
		for _, event := range traceEvents[start:] {
			lines = append(lines, formatTraceEvent(event))
		}
	}

	return strings.TrimSpace(strings.Join(lines, "\n")) + "\n"
}

// buildValidationInventory 构建确定性验证清单（Python _build_validation_inventory）
func buildValidationInventory(caseInput *CaseAnalysisInput) map[string]any {
	caseDir := filepath.Dir(caseInput.ResultPath)
	traceData := readJSONIfExists(filepath.Join(caseDir, "judge", "normalized_trace.json"))
	traceEvents := summarizeNormalizedTrace(traceData)
	events := validationEventsFromResult(caseInput.ResultPath)
	if len(events) == 0 {
		events = traceEvents
	}
	return validationInventoryFromEvents(events, caseInput.EvaluationPassed)
}

// buildVerifierInventory 提取权威补丁应用与测试结果（Python _build_verifier_inventory）
func buildVerifierInventory(caseInput *CaseAnalysisInput) map[string]any {
	metadata := caseInput.EvaluationMetadata
	var emptyPatch any
	if metadata != nil {
		emptyPatch = metadata["empty_patch"]
	}
	var rawReports map[string]any
	if metadata != nil {
		rawReports, _ = metadata["instance_report"].(map[string]any)
	}
	if len(rawReports) == 0 {
		if emptyPatch != nil {
			return map[string]any{"empty_patch": emptyPatch}
		}
		return map[string]any{}
	}
	report, ok := rawReports[caseInput.CaseID].(map[string]any)
	if !ok {
		// Python: next((item for item in raw_reports.values() if isinstance(item, dict)), None)
		for _, key := range sortedMapKeys(rawReports) {
			if item, isMap := rawReports[key].(map[string]any); isMap {
				report = item
				break
			}
		}
	}
	if report == nil {
		return map[string]any{}
	}

	testsStatus, _ := report["tests_status"].(map[string]any)
	failures := func(groupName string) []string {
		group, ok := testsStatus[groupName].(map[string]any)
		if !ok {
			return []string{}
		}
		list, ok := group["failure"].([]any)
		if !ok {
			return []string{}
		}
		var out []string
		for i, item := range list {
			if i >= 24 {
				break
			}
			s := toStringValue(item)
			if strings.TrimSpace(s) != "" {
				out = append(out, s)
			}
		}
		return out
	}

	var testOutputExcerpt any
	if metadata != nil {
		testOutputExcerpt = metadata["test_output_excerpt"]
	}
	return map[string]any{
		"empty_patch":                     emptyPatch,
		"patch_exists":                    report["patch_exists"],
		"patch_successfully_applied":      report["patch_successfully_applied"],
		"resolved":                        report["resolved"],
		"failed_fail_to_pass_tests":       failures("FAIL_TO_PASS"),
		"failed_pass_to_pass_tests":       failures("PASS_TO_PASS"),
		"verifier_failure_output_excerpt": truncateText(toStringValue(testOutputExcerpt), 8000),
	}
}

// validationEventsFromResult 读取执行后端保留的完整命令结果摘录（Python _validation_events_from_result）
func validationEventsFromResult(resultPath string) []map[string]any {
	resultData := readJSONIfExists(resultPath)
	metadata, _ := resultData["metadata"].(map[string]any)
	var execution map[string]any
	if metadata != nil {
		execution, _ = metadata["execution"].(map[string]any)
	}
	var commandLog []any
	if execution != nil {
		commandLog, _ = execution["command_log"].([]any)
	}
	if commandLog == nil {
		return nil
	}
	var events []map[string]any
	for _, recordAny := range commandLog {
		record, ok := recordAny.(map[string]any)
		if !ok {
			continue
		}
		stdoutPart := toStringValue(record["stdout_excerpt"])
		stderrPart := toStringValue(record["stderr_excerpt"])
		var parts []string
		if stdoutPart != "" {
			parts = append(parts, stdoutPart)
		}
		if stderrPart != "" {
			parts = append(parts, stderrPart)
		}
		output := strings.Join(parts, "\n")
		var errorStr string
		switch exitCode := record["exit_code"].(type) {
		case nil:
			errorStr = ""
		case float64:
			if exitCode != 0 {
				errorStr = fmt.Sprintf("exit_code=%s", strconv.FormatFloat(exitCode, 'g', -1, 64))
			}
		case string:
			if exitCode != "" && exitCode != "0" {
				errorStr = fmt.Sprintf("exit_code=%s", exitCode)
			}
		default:
			errorStr = ""
		}
		tail := output
		if len(tail) > 1200 {
			tail = tail[len(tail)-1200:]
		}
		events = append(events, map[string]any{
			"tool":              "command_log",
			"input":             toStringValue(record["command"]),
			"output":            oneLine(output, 300),
			"output_tail":       oneLine(tail, 1200),
			"error":             errorStr,
			"validation_result": validationResultSignal(output, errorStr),
		})
	}
	return events
}

// validationInventoryFromEvents 在 LLM 重读轨迹之前提取项目测试事实（Python _validation_inventory_from_events）
func validationInventoryFromEvents(events []map[string]any, verifierPassed bool) map[string]any {
	var projectEvents []map[string]any
	for _, event := range events {
		command := toStringValue(event["input"])
		lowered := strings.Join(strings.Fields(strings.ToLower(command)), " ")
		isPytestSuite := strings.Contains(lowered, "pytest") &&
			(strings.Contains(" "+lowered+" ", " tests/ ") || strings.Contains(" "+lowered+" ", " ./tests/ "))
		isProjectSuite := isPytestSuite ||
			strings.Contains(lowered, "make test") ||
			strings.Contains(lowered, " tox") ||
			strings.Contains(lowered, "tox ") ||
			strings.Contains(lowered, " nox") ||
			strings.Contains(lowered, "nox ") ||
			strings.Contains(lowered, "npm test") ||
			strings.Contains(lowered, "pnpm test")
		if !isProjectSuite {
			continue
		}
		output := toStringValue(event["output_tail"])
		if output == "" {
			output = toStringValue(event["output"])
		}
		result := toStringValue(event["validation_result"])
		if result == "" {
			result = "unknown"
		}
		projectEvents = append(projectEvents, map[string]any{
			"command": command,
			"output":  output,
			"result":  result,
		})
	}
	suiteResult := "not_observed"
	if len(projectEvents) > 0 {
		anyPassed, anyFailed := false, false
		for _, event := range projectEvents {
			switch event["result"] {
			case "passed":
				anyPassed = true
			case "failed":
				anyFailed = true
			}
		}
		switch {
		case anyPassed:
			suiteResult = "passed"
		case anyFailed:
			suiteResult = "failed"
		}
	}
	var lastEvents []map[string]any
	if len(projectEvents) > 4 {
		lastEvents = projectEvents[len(projectEvents)-4:]
	} else {
		lastEvents = projectEvents
	}
	return map[string]any{
		"project_test_suite_attempted":  len(projectEvents) > 0,
		"project_test_suite_result":     suiteResult,
		"authoritative_verifier_result": boolToPassFail(verifierPassed),
		"project_test_events":           lastEvents,
	}
}

// summarizeNormalizedTrace 从 normalized_trace.json 提取有界工具调用事件（Python _summarize_normalized_trace）
func summarizeNormalizedTrace(traceData map[string]any) []map[string]any {
	var events []map[string]any
	traces, _ := traceData["traces"].([]any)
	if traces == nil {
		return events
	}
	for _, traceAny := range traces {
		trace, ok := traceAny.(map[string]any)
		if !ok {
			continue
		}
		traceID := toStringValue(trace["trace_id"])
		role := toStringValue(trace["member_role"])
		if role == "" {
			role = toStringValue(trace["role"])
		}
		messages, _ := trace["messages"].([]any)
		if messages == nil {
			continue
		}
		for _, messageAny := range messages {
			message, ok := messageAny.(map[string]any)
			if !ok {
				continue
			}
			messageIndex := message["message_index"]
			content := strings.TrimSpace(toStringValue(message["content"]))
			if content != "" {
				events = append(events, map[string]any{
					"trace_id":      traceID,
					"role":          role,
					"message_index": messageIndex,
					"step_pointer":  toStringValue(message["step_pointer"]),
					"tool":          "",
					"input":         "",
					"output":        oneLine(content, 500),
					"error":         "",
				})
			}
			toolCalls, _ := message["tool_calls"].([]any)
			if toolCalls == nil {
				continue
			}
			for _, callAny := range toolCalls {
				call, ok := callAny.(map[string]any)
				if !ok {
					continue
				}
				rawOutput := toStringValue(call["output"])
				rawError := toStringValue(call["error"])
				outputTail := rawOutput
				if len(outputTail) > 300 {
					outputTail = outputTail[len(outputTail)-300:]
				}
				events = append(events, map[string]any{
					"trace_id":          traceID,
					"role":              role,
					"message_index":     messageIndex,
					"step_pointer":      toStringValue(call["step_pointer"]),
					"tool":              toStringValue(call["name"]),
					"input":             oneLine(call["input"], 300),
					"output":            oneLine(rawOutput, 300),
					"output_tail":       oneLine(outputTail, 300),
					"error":             oneLine(rawError, 500),
					"validation_result": validationResultSignal(rawOutput, rawError),
				})
			}
		}
	}
	return events
}

// validationResultSignal 在展示摘录截断尾部前保留测试结果证据（Python _validation_result_signal）
func validationResultSignal(output string, errorStr string) string {
	if strings.TrimSpace(errorStr) != "" {
		return "failed"
	}
	lowered := strings.ToLower(output)
	failedCountRE := regexp.MustCompile(`\b(\d+)\s+failed\b`)
	passedCountRE := regexp.MustCompile(`\b(\d+)\s+passed\b`)
	for _, match := range failedCountRE.FindAllStringSubmatch(lowered, -1) {
		if count, err := strconv.Atoi(match[1]); err == nil && count > 0 {
			return "failed"
		}
	}
	for _, match := range passedCountRE.FindAllStringSubmatch(lowered, -1) {
		if count, err := strconv.Atoi(match[1]); err == nil && count > 0 {
			return "passed"
		}
	}
	return "unknown"
}

// formatTraceEvent 格式化单条轨迹事件（Python _format_trace_event）
func formatTraceEvent(event map[string]any) string {
	status := "ok"
	if errStr, _ := event["error"].(string); errStr != "" {
		status = "err"
	}
	parts := []string{
		fmt.Sprintf("- [%s] trace_id=%v", status, event["trace_id"]),
		fmt.Sprintf("role=%v", event["role"]),
		fmt.Sprintf("message_index=%v", event["message_index"]),
		fmt.Sprintf("step=%v", event["step_pointer"]),
		fmt.Sprintf("tool=%v", event["tool"]),
	}
	if input, _ := event["input"].(string); input != "" {
		parts = append(parts, fmt.Sprintf("input=%v", event["input"]))
	}
	if errStr, _ := event["error"].(string); errStr != "" {
		parts = append(parts, fmt.Sprintf("error=%v", event["error"]))
	} else if output, _ := event["output"].(string); output != "" {
		parts = append(parts, fmt.Sprintf("output=%v", event["output"]))
	}
	return strings.Join(parts, " ")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// mustJSON 紧凑 JSON 序列化（失败回退空串）
func mustJSON(value any) string {
	raw, err := json.Marshal(value)
	if err != nil {
		return "null"
	}
	return string(raw)
}

// mustJSONIndent 缩进 JSON 序列化（失败回退空串）
func mustJSONIndent(value any) string {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return "null"
	}
	return string(raw)
}

// sortedMapKeys map 键排序（Python dict 迭代序的确定性等价；D-15 登记）
func sortedMapKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// boolToPassFail 布尔 → passed/failed（Python "passed" if verifier_passed else "failed"）
func boolToPassFail(verifierPassed bool) string {
	if verifierPassed {
		return "passed"
	}
	return "failed"
}

// diagnosisFailedChecks 诊断失败的检查项（Python _diagnosis_failed_checks；保序去重）
func diagnosisFailedChecks(diagnosis map[string]any) []string {
	cluster, _ := diagnosis["failure_cluster"].(map[string]any)
	var checks []string
	if cluster != nil {
		checks = stringItems(cluster["failed_checks"])
	}
	seen := map[string]bool{}
	var out []string
	for _, c := range checks {
		if !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
	}
	if len(out) > 0 {
		return out
	}
	verifier, ok := diagnosis["verifier_observations"].(map[string]any)
	if !ok {
		return out
	}
	combined := append(stringItems(verifier["failed_fail_to_pass_tests"]), stringItems(verifier["failed_pass_to_pass_tests"])...)
	seen = map[string]bool{}
	for _, c := range combined {
		if !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
	}
	return out
}

// stringItems 字符串项提取（Python _string_items）
func stringItems(value any) []string {
	if list, ok := value.([]string); ok {
		return list
	}
	list, ok := value.([]any)
	if !ok {
		return []string{}
	}
	var out []string
	for _, item := range list {
		out = append(out, toStringValue(item))
	}
	return out
}

// diagnosisFailureCluster 失败聚类（Python _diagnosis_failure_cluster）
func diagnosisFailureCluster(diagnosis map[string]any) map[string]any {
	rawCluster, _ := diagnosis["failure_cluster"].(map[string]any)
	cluster := map[string]any{}
	for k, v := range rawCluster {
		cluster[k] = v
	}
	checks := diagnosisFailedChecks(diagnosis)
	observable := strings.TrimSpace(toStringValue(cluster["observable_behavior"]))
	if observable == "" {
		if contract, ok := diagnosis["decision_contract"].(map[string]any); ok {
			observable = strings.TrimSpace(toStringValue(contract["acceptance_observable"]))
		}
	}
	if len(checks) == 0 && observable == "" {
		return map[string]any{}
	}
	return map[string]any{
		"failed_checks":       checks,
		"observable_behavior": observable,
	}
}

// compactPerCaseDiagnoses 聚合输入压缩：归因字段打包进 attribution 子字典（Python _compact_per_case_diagnoses）
func compactPerCaseDiagnoses(perCaseDiagnoses []map[string]any) []map[string]any {
	var compact []map[string]any
	for _, item := range perCaseDiagnoses {
		issueCategory := item["issue_category"]
		if issueCategory == nil || issueCategory == "" {
			issueCategory = item["category"]
		}
		var affectedComponents any = item["affected_components"]
		if affectedComponents == nil {
			affectedComponents = []any{}
		}
		var evidenceRefs any = item["evidence_refs"]
		if evidenceRefs == nil {
			evidenceRefs = []any{}
		}
		attribution := map[string]any{
			"root_cause":        truncateText(item["root_cause"], 500),
			"critical_mistake":  truncateText(item["critical_mistake"], 500),
			"general_mechanism": truncateText(item["general_mechanism"], 500),
			"decision_contract": boundedStructuredValue(itemOrEmpty(item, "decision_contract"), 1400),
			"failure_cluster":   boundedStructuredValue(diagnosisFailureCluster(item), 1000),
			"target_ref":        item["target_ref"],
			"evidence_refs":     evidenceRefs,
			"confidence":        item["confidence"],
		}
		compact = append(compact, map[string]any{
			"case_id":             item["case_id"],
			"analysis_failed":     item["analysis_failed"],
			"issue_category":      issueCategory,
			"severity":            item["severity"],
			"summary":             truncateText(item["summary"], 500),
			"failure_mode":        item["failure_mode"],
			"affected_components": affectedComponents,
			"recommendation":      truncateText(item["recommendation"], 500),
			"attribution":         attribution,
		})
	}
	return compact
}

// itemOrEmpty 取 map 字段，缺失返回空 map（辅助）
func itemOrEmpty(m map[string]any, key string) any {
	if v, ok := m[key]; ok && v != nil {
		return v
	}
	return map[string]any{}
}

// ──────────────────────────── 错误类型（Python analyzer.py:2534/3351 区段） ────────────────────────────

// diagnosisOutputFormatError 有界 JSON 修复后仍是普通文本（Python _DiagnosisOutputFormatError）
type diagnosisOutputFormatError struct{ message string }

func (e *diagnosisOutputFormatError) Error() string { return e.message }

// diagnosisContentError 解析出的 JSON 无可用诊断（Python _DiagnosisContentError）
type diagnosisContentError struct{ message string }

func (e *diagnosisContentError) Error() string { return e.message }

// ──────────────────────────── 非导出函数（JSON 提取与分类） ────────────────────────────

// removePath 删除存在的文件或目录（Python _remove_path）
func removePath(path string) {
	info, err := os.Stat(path)
	if err != nil {
		return
	}
	if info.IsDir() {
		_ = os.RemoveAll(path)
		return
	}
	_ = os.Remove(path)
}

// extractJSONObject 解码首个 JSON 对象，忽略散文与字符串内大括号（Python _extract_json_object）
func extractJSONObject(text string) map[string]any {
	index := 0
	for index < len(text) {
		start := strings.Index(text[index:], "{")
		if start < 0 {
			return nil
		}
		start += index
		decoder := json.NewDecoder(strings.NewReader(text[start:]))
		var value any
		if err := decoder.Decode(&value); err != nil {
			index = start + 1
			continue
		}
		if obj, ok := value.(map[string]any); ok {
			return obj
		}
		index = start + 1
	}
	return nil
}

// containsIncompleteJSONObject 输出是否含被截断的 JSON 对象（Python _contains_incomplete_json_object）
func containsIncompleteJSONObject(text string) bool {
	matchRE := regexp.MustCompile(`\{\s*"`)
	for _, loc := range matchRE.FindAllStringIndex(text, -1) {
		start := loc[0]
		candidate := strings.TrimSpace(text[start:])
		var probe any
		if json.NewDecoder(strings.NewReader(candidate)).Decode(&probe) == nil {
			continue
		}
		var stack []rune
		inString, escaped, invalidCloser := false, false, false
		for _, char := range candidate {
			if inString {
				switch {
				case escaped:
					escaped = false
				case char == '\\':
					escaped = true
				case char == '"':
					inString = false
				}
				continue
			}
			switch char {
			case '"':
				inString = true
			case '{', '[':
				stack = append(stack, char)
			case '}', ']':
				if len(stack) == 0 {
					invalidCloser = true
				} else {
					opening := stack[len(stack)-1]
					stack = stack[:len(stack)-1]
					if (opening == '{' && char != '}') || (opening == '[' && char != ']') {
						invalidCloser = true
					}
				}
			}
			if invalidCloser {
				break
			}
		}
		if !invalidCloser && (len(stack) > 0 || inString || escaped) {
			return true
		}
	}
	return false
}

// containsModelServiceErrorText 永久服务/鉴权失败与坏格式区分（Python _contains_model_service_error_text）
func containsModelServiceErrorText(raw string) string {
	normalized := strings.Join(strings.Fields(strings.ToLower(toStringValue(raw))), " ")
	for _, marker := range []string{
		"error code:", "invalid_api_key", "authentication failed", "authentication error",
		"unauthorized", "budget_exceeded", "budget has been exceeded",
	} {
		if strings.Contains(normalized, marker) {
			return marker
		}
	}
	return ""
}

// unusableDiagnosisOutputError 分类耗尽的畸形输出而不掩盖永久失败（Python _unusable_diagnosis_output_error）
func unusableDiagnosisOutputError(caseID string, outputs []string) error {
	latest := ""
	for i := len(outputs) - 1; i >= 0; i-- {
		if outputs[i] != "" {
			latest = outputs[i]
			break
		}
	}
	excerpt := truncateText(latest, 256)
	if containsModelServiceErrorText(latest) != "" {
		return fmt.Errorf("per-case diagnosis output contained a model-service error for %s: %s", caseID, excerpt)
	}
	if extractJSONObject(latest) != nil {
		return &diagnosisContentError{
			fmt.Sprintf("per-case diagnosis output contained JSON but no usable diagnosis entries for %s: %s", caseID, excerpt),
		}
	}
	for _, value := range outputs {
		if containsIncompleteJSONObject(value) {
			return fmt.Errorf("%w: per-case diagnosis output remained incomplete JSON after repair for %s: %s",
				harnessrsi.ErrRetryableModelOutput, caseID, excerpt)
		}
	}
	return &diagnosisOutputFormatError{
		fmt.Sprintf("per-case diagnosis output did not contain JSON for %s: %s", caseID, excerpt),
	}
}

// buildJSONRepairPrompt 二次修复格式而不改变证据（Python _build_json_repair_prompt）
func buildJSONRepairPrompt(originalPrompt, previousOutput string) string {
	var problem string
	if extractJSONObject(previousOutput) != nil {
		problem = "Previous diagnosis output contained JSON but no usable diagnosis entries. " +
			"Provide a diagnoses list of objects grounded in the current evidence; " +
			"use target_ref=unassigned when the cause is unsupported."
	} else {
		problem = "Previous diagnosis output was not valid JSON."
	}
	return fmt.Sprintf(`%s

You must convert the diagnosis into the required single valid JSON object.
Do not include Markdown, prose, analysis notes, or text before/after the JSON.
Preserve the original task evidence and target_ref semantics from the original prompt.

Original diagnosis prompt:
%s

Previous output:
%s

Return only the single valid JSON object required by the original prompt.
`, problem, truncateText(originalPrompt, 6000), truncateText(previousOutput, 2000))
}

// buildEvidenceConflictRepairPrompt 只要求调和确定性矛盾（Python _build_evidence_conflict_repair_prompt）
func buildEvidenceConflictRepairPrompt(
	originalPrompt, previousOutput string,
	conflicts []string,
	validationInventory, verifierInventory map[string]any,
) string {
	repairPayload := map[string]any{
		"deterministic_validation_inventory": validationInventory,
		"deterministic_verifier_inventory":   verifierInventory,
		"validation_conflicts":               conflicts,
	}
	return fmt.Sprintf(`The previous diagnosis was valid JSON but contradicted deterministic evidence.

Correct the causal diagnosis, not just its wording. Treat the inventories below as
immutable observations. Preserve supported evidence and change any unsupported
root cause, decision contract, target_ref, or recommendation. If the inventories
do not distinguish a mechanism, return target_ref="unassigned" with low confidence.
Return one valid JSON object only; do not include Markdown or prose.

Deterministic conflict payload:
%s

Original diagnosis prompt:
%s

Previous conflicting JSON:
%s
`, boundedJSON(repairPayload, 5000), truncateText(originalPrompt, 6000), truncateText(previousOutput, 4000))
}

// runAgentWithRepair 运行诊断 DeepAgent 并做一次有界 JSON 修复（Python _run_agent）
func runAgentWithRepair(ctx context.Context, agent interfaces.BaseAgent, prompt string, maxRetries int) (string, error) {
	modelCallRetries := maxRetries
	if modelCallRetries < 0 {
		modelCallRetries = 0
	}
	lastRaw, err := RunDeepAgentText(ctx, agent, prompt, modelCallRetries, "diagnosis agent")
	if err != nil {
		return "", err
	}
	if extractJSONObject(lastRaw) != nil || modelCallRetries == 0 {
		return lastRaw, nil
	}
	return RunDeepAgentText(ctx, agent, buildJSONRepairPrompt(prompt, lastRaw), modelCallRetries, "diagnosis agent JSON repair")
}

// ──────────────────────────── 归一化/聚类/优先级辅助（Python analyzer.py:1054-1470） ────────────────────────────

// normalizeClusterText 聚类文本归一化（Python _normalize_cluster_text）
func normalizeClusterText(value any) string {
	return strings.Join(strings.Fields(strings.ToLower(toStringValue(value))), " ")
}

// clusterTextsAreSimilar 聚类文本相似性（Python _cluster_texts_are_similar）
func clusterTextsAreSimilar(left, right string) bool {
	// Python 实现（analyzer.py:1233-1243）：以词集合 Jaccard 近似判定
	leftWords := strings.Fields(left)
	rightWords := strings.Fields(right)
	if len(leftWords) == 0 || len(rightWords) == 0 {
		return left == right
	}
	leftSet := map[string]bool{}
	for _, w := range leftWords {
		leftSet[w] = true
	}
	rightSet := map[string]bool{}
	for _, w := range rightWords {
		rightSet[w] = true
	}
	intersect := 0
	for w := range leftSet {
		if rightSet[w] {
			intersect++
		}
	}
	union := len(leftSet) + len(rightSet) - intersect
	return float64(intersect)/float64(union) >= 0.6
}

// diagnosesAreDuplicate 判定两条诊断重复（Python _diagnoses_are_duplicate）
func diagnosesAreDuplicate(left, right map[string]any) bool {
	leftChecks := map[string]bool{}
	for _, v := range diagnosisFailedChecks(left) {
		if n := normalizeClusterText(v); n != "" {
			leftChecks[n] = true
		}
	}
	rightChecks := map[string]bool{}
	for _, v := range diagnosisFailedChecks(right) {
		if n := normalizeClusterText(v); n != "" {
			rightChecks[n] = true
		}
	}
	if len(leftChecks) > 0 && len(rightChecks) > 0 && !sameStringSets(leftChecks, rightChecks) {
		return false
	}
	leftCluster := diagnosisFailureCluster(left)
	rightCluster := diagnosisFailureCluster(right)
	leftObservable := normalizeClusterText(leftCluster["observable_behavior"])
	rightObservable := normalizeClusterText(rightCluster["observable_behavior"])
	if leftObservable != "" && rightObservable != "" {
		if !clusterTextsAreSimilar(leftObservable, rightObservable) {
			return false
		}
		return true
	}
	if len(leftChecks) > 0 && len(rightChecks) > 0 {
		return true
	}
	return normalizeTargetRef(left["target_ref"]) == normalizeTargetRef(right["target_ref"]) &&
		normalizeClusterText(left["failure_mode"]) == normalizeClusterText(right["failure_mode"])
}

// sameStringSets 集合相等（辅助）
func sameStringSets(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}

// candidateFeedbackCheckSets 最近一次配对候选实验的检查集（Python _candidate_feedback_check_sets）
func candidateFeedbackCheckSets(feedback map[string]any) map[string]map[string]bool {
	empty := map[string]map[string]bool{
		"regressed": {}, "remaining": {}, "fixed": {},
	}
	if feedback == nil {
		return empty
	}
	experiments, _ := feedback["experiments"].([]any)
	if experiments == nil {
		return empty
	}
	// Python: for experiment in reversed(experiments)
	for i := len(experiments) - 1; i >= 0; i-- {
		experiment, ok := experiments[i].(map[string]any)
		if !ok {
			continue
		}
		delta, ok := experiment["verifier_delta"].(map[string]any)
		if !ok {
			continue
		}
		regressed := normalizedCheckSet(delta,
			"regressed_fail_to_pass", "regressed_pass_to_pass", "regressed_atomic_checks")
		remaining := normalizedCheckSet(delta,
			"remaining_failed_fail_to_pass", "remaining_failed_atomic_checks")
		fixed := normalizedCheckSet(delta,
			"newly_passed_fail_to_pass", "newly_passed_atomic_checks")
		for k := range fixed {
			if regressed[k] || remaining[k] {
				delete(fixed, k)
			}
		}
		return map[string]map[string]bool{
			"regressed": regressed, "remaining": remaining, "fixed": fixed,
		}
	}
	return empty
}

// normalizedCheckSet 键集归一化（Python _normalized_check_set）
func normalizedCheckSet(payload map[string]any, keys ...string) map[string]bool {
	normalizedValues := map[string]bool{}
	for _, key := range keys {
		for _, value := range stringItems(payload[key]) {
			normalized := normalizeClusterText(value)
			if normalized != "" {
				normalizedValues[normalized] = true
			}
		}
	}
	return normalizedValues
}

// diagnosisFeedbackPriority 反馈优先级（Python _diagnosis_feedback_priority）
func diagnosisFeedbackPriority(diagnosis map[string]any, feedbackSets map[string]map[string]bool) int {
	checks := map[string]bool{}
	for _, v := range diagnosisFailedChecks(diagnosis) {
		if n := normalizeClusterText(v); n != "" {
			checks[n] = true
		}
	}
	for k := range checks {
		if feedbackSets["regressed"][k] {
			return 0
		}
	}
	for k := range checks {
		if feedbackSets["remaining"][k] {
			return 1
		}
	}
	// 先前的胜利不是后续执行仍通过的证据
	if len(checks) > 0 && isSubset(checks, feedbackSets["fixed"]) {
		return 3
	}
	return 2
}

// isSubset 子集判定（辅助）
func isSubset(sub, super map[string]bool) bool {
	for k := range sub {
		if !super[k] {
			return false
		}
	}
	return true
}

// normalizeCaseDiagnoses 单响应展开为有界独立诊断（Python _normalize_case_diagnoses）。
// 新响应格式 {"diagnoses": [...]}；旧式单诊断对象仍有效。历史反馈在单案上限内
// 优先回归与残余失败；不能否决当前诊断。
func normalizeCaseDiagnoses(parsed map[string]any, priorCandidateFeedback map[string]any) []map[string]any {
	rawDiagnoses, hasList := parsed["diagnoses"]
	var candidates []map[string]any
	switch {
	case !hasList || rawDiagnoses == nil:
		candidates = []map[string]any{parsed}
	case isAnyList(rawDiagnoses):
		for _, item := range toAnySlice(rawDiagnoses) {
			if m, ok := item.(map[string]any); ok {
				candidates = append(candidates, m)
			}
		}
	}
	feedbackSets := candidateFeedbackCheckSets(priorCandidateFeedback)
	requireCluster := hasList && rawDiagnoses != nil && isAnyList(rawDiagnoses) && len(candidates) > 1
	type rankedDiagnosis struct {
		priority, index int
		diagnosis       map[string]any
	}
	var ranked []rankedDiagnosis
	for index, raw := range candidates {
		diagnosis := map[string]any{}
		for k, v := range raw {
			diagnosis[k] = v
		}
		failureCluster := diagnosisFailureCluster(diagnosis)
		if requireCluster && len(failureCluster) == 0 {
			continue
		}
		if len(failureCluster) > 0 {
			diagnosis["failure_cluster"] = failureCluster
		}
		priority := diagnosisFeedbackPriority(diagnosis, feedbackSets)
		ranked = append(ranked, rankedDiagnosis{priority, index, diagnosis})
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].priority != ranked[j].priority {
			return ranked[i].priority < ranked[j].priority
		}
		return ranked[i].index < ranked[j].index
	})
	var normalized []map[string]any
	for _, item := range ranked {
		duplicate := false
		for _, existing := range normalized {
			if diagnosesAreDuplicate(item.diagnosis, existing) {
				duplicate = true
				break
			}
		}
		if duplicate {
			continue
		}
		normalized = append(normalized, item.diagnosis)
		if len(normalized) >= maxDiagnosesPerCase {
			break
		}
	}
	return normalized
}

// isAnyList 值是否为列表（辅助）
func isAnyList(value any) bool {
	switch value.(type) {
	case []any, []map[string]any, []string:
		return true
	}
	return false
}

// casePriorCandidateFeedback 仅返回当前案例的配对候选实验（Python _case_prior_candidate_feedback）
func casePriorCandidateFeedback(feedback map[string]any, caseID string) map[string]any {
	if feedback == nil {
		return map[string]any{}
	}
	byCase, ok := feedback["by_case"].(map[string]any)
	if !ok {
		return map[string]any{}
	}
	recordsAny, exists := byCase[caseID]
	if !exists {
		return map[string]any{}
	}
	var records []any
	switch v := recordsAny.(type) {
	case map[string]any:
		records = []any{v}
	case []any:
		records = v
	default:
		return map[string]any{}
	}
	var experiments []any
	start := len(records) - 3
	if start < 0 {
		start = 0
	}
	for _, record := range records[start:] {
		if m, ok := record.(map[string]any); ok {
			experiments = append(experiments, m)
		}
	}
	return map[string]any{"case_id": caseID, "experiments": experiments}
}

// normalizeTargetRef 归一化目标引用（Python _normalize_target_ref）
func normalizeTargetRef(value any) string {
	return strings.ReplaceAll(strings.TrimSpace(toStringValue(value)), "-", "_")
}

// issueCategoryFromTargetRef 目标引用 → 问题类别（Python _issue_category_from_target_ref）
func issueCategoryFromTargetRef(targetRef string) string {
	scope := targetScopeFromTargetRef(strings.ToLower(targetRef))
	if scope == "member_harness" || scope == "team_skill" {
		return scope
	}
	return ""
}

// severityRank 严重度序值（Python _severity_rank）
func severityRank(value any) int {
	switch strings.ToLower(toStringValue(value)) {
	case "low":
		return 1
	case "medium":
		return 2
	case "high":
		return 3
	}
	return 0
}

// confidenceRank 置信度序值（Python _confidence_rank）
func confidenceRank(value any) int {
	return severityRank(value)
}

// targetScopeFromTargetRef 目标引用 → 范围（Python _target_scope_from_target_ref）
func targetScopeFromTargetRef(targetRef string) string {
	parts := strings.Split(targetRef, ".")
	if len(parts) > 0 && (parts[0] == "member_harness" || parts[0] == "team_skill") {
		return parts[0]
	}
	return ""
}

// targetMemberFromTargetRef 目标引用 → 成员（Python _target_member_from_target_ref）
func targetMemberFromTargetRef(targetRef string) string {
	parts := strings.Split(targetRef, ".")
	if len(parts) >= 3 && parts[0] == "member_harness" && parts[1] != "" {
		return parts[1]
	}
	return ""
}

// stringItems 字符串或字符串清单归一化为非空字符串（Python _string_items）
func stringItemsGo(value any) []string {
	if s, ok := value.(string); ok {
		stripped := strings.TrimSpace(s)
		if stripped != "" {
			return []string{stripped}
		}
		return []string{}
	}
	list, ok := value.([]any)
	if !ok {
		return []string{}
	}
	var out []string
	for _, item := range list {
		if s, isStr := item.(string); isStr {
			if trimmed := strings.TrimSpace(s); trimmed != "" {
				out = append(out, trimmed)
			}
		}
	}
	return out
}

// coordinatorRoleKeys 协调者角色键集（Python _COORDINATOR_ROLE_KEYS，逐字）
var coordinatorRoleKeys = map[string]bool{
	"coordinator": true, "lead": true, "leader": true, "team": true,
	"team_coordinator": true, "team_leader": true,
}

// isTeamCoordinatorRole 判断是否协调者角色（Python is_team_coordinator_role）
func isTeamCoordinatorRole(values ...string) bool {
	for _, value := range values {
		normalized := strings.ReplaceAll(strings.ReplaceAll(strings.ToLower(strings.TrimSpace(value)), "-", "_"), " ", "_")
		if coordinatorRoleKeys[normalized] {
			return true
		}
	}
	return false
}

// issueTargetRef 读取归一化的归属 target_ref（Python _issue_target_ref）
func issueTargetRef(issue harnessrsi.TeamIssue) string {
	attribution, ok := issue.Metadata["attribution"].(map[string]any)
	if !ok {
		return ""
	}
	return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(toStringValue(attribution["target_ref"]))), "-", "_")
}

// withAttributionTargetRef 回写归属 target_ref（Python _with_attribution_target_ref）
func withAttributionTargetRef(metadata map[string]any, targetRef string) map[string]any {
	updated := map[string]any{}
	for k, v := range metadata {
		updated[k] = v
	}
	var attribution map[string]any
	if raw, ok := updated["attribution"].(map[string]any); ok {
		attribution = map[string]any{}
		for k, v := range raw {
			attribution[k] = v
		}
	} else {
		attribution = map[string]any{}
	}
	attribution["target_ref"] = targetRef
	updated["attribution"] = attribution
	return updated
}

// looksLikeCompletionContractIssue 完成契约问题特征（Python _looks_like_completion_contract_issue）
func looksLikeCompletionContractIssue(issue harnessrsi.TeamIssue) bool {
	evidenceJSON, _ := json.Marshal(issue.Evidence)
	metadataJSON, _ := json.Marshal(issue.Metadata)
	text := strings.ToLower(strings.Join([]string{
		issue.Summary, issue.Recommendation, string(evidenceJSON), string(metadataJSON),
	}, "\n"))
	for _, marker := range []string{
		"artifact", "claim_task", "complete", "completion", "deliverable", "file",
		"output", "required", "status", "verify", "verification",
	} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

// coordinatorTeamSkillTargetRef 协调者 team_skill 目标引用（Python _coordinator_team_skill_target_ref）
func coordinatorTeamSkillTargetRef(issue harnessrsi.TeamIssue) string {
	variable := "role_coordination"
	if looksLikeCompletionContractIssue(issue) {
		variable = "constraint_violation"
	}
	return fmt.Sprintf("team_skill.team_leader.%s", variable)
}

// isEvidencePipelineFailure 是否描述分析器/评估器证据管线（Python _is_evidence_pipeline_failure）
func isEvidencePipelineFailure(issue harnessrsi.TeamIssue) bool {
	evidenceJSON, _ := json.Marshal(issue.Evidence)
	metadataJSON, _ := json.Marshal(issue.Metadata)
	text := strings.ToLower(strings.Join([]string{
		issue.Summary, issue.Recommendation, string(evidenceJSON), string(metadataJSON),
	}, "\n"))
	evidenceArtifactHit := false
	for _, marker := range []string{"trajectory_events.jsonl", "normalized_trace.json", "evidence_summary.md"} {
		if strings.Contains(text, marker) {
			evidenceArtifactHit = true
			break
		}
	}
	missingHit := false
	for _, marker := range []string{"no such file", "not found", "missing", "failed to read", "failed to load"} {
		if strings.Contains(text, marker) {
			missingHit = true
			break
		}
	}
	return evidenceArtifactHit && missingHit
}

// targetMembersFromIssue 从显式证据提取成员目标，绝不虚构成员（Python _target_members_from_issue）
func targetMembersFromIssue(issue harnessrsi.TeamIssue) []string {
	var candidates []string
	candidates = append(candidates, stringItemsGo(issue.TargetMembers)...)
	if member := targetMemberFromTargetRef(issueTargetRef(issue)); member != "" {
		candidates = append(candidates, member)
	}
	candidates = append(candidates, stringItemsGo(issue.Metadata["affected_components"])...)
	for _, evidence := range issue.Evidence {
		candidates = append(candidates, stringItemsGo(evidence["affected_components"])...)
		candidates = append(candidates, stringItemsGo(evidence["affected_component"])...)
	}
	seen := map[string]bool{}
	var out []string
	for _, c := range candidates {
		if !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
	}
	return out
}

// dictToTeamIssue 原始输出 dict → TeamIssue，强制类别锁（Python _dict_to_team_issue）。
// 归属从 metadata.attribution（嵌套，优先）或平铺顶层字段（逐案兼容回退）提取；
// 结果恒写入 TeamIssue.metadata["attribution"]。
func dictToTeamIssue(data map[string]any) harnessrsi.TeamIssue {
	rawCategory := toStringValue(data["category"])
	if rawCategory == "" {
		rawCategory = toStringValue(data["issue_category"])
	}
	if rawCategory == "" {
		rawCategory = "team_coordination"
	}
	category := rawCategory
	if category != "member_harness" && category != "team_coordination" {
		category = "team_coordination"
	}
	metadata := map[string]any{}
	if raw, ok := data["metadata"].(map[string]any); ok {
		for k, v := range raw {
			metadata[k] = v
		}
	}
	if _, hasAttr := metadata["attribution"]; !hasAttr {
		nested, isMap := data["metadata"].(map[string]any)
		if isMap {
			if attr, ok := nested["attribution"].(map[string]any); ok {
				metadata["attribution"] = attr
			}
		}
		if _, still := metadata["attribution"]; !still {
			flatKeys := []string{"root_cause", "critical_mistake", "general_mechanism",
				"decision_contract", "failure_cluster", "target_ref", "evidence_refs", "confidence"}
			flat := map[string]any{}
			for _, key := range flatKeys {
				if v, ok := data[key]; ok {
					flat[key] = v
				}
			}
			if len(flat) > 0 {
				metadata["attribution"] = flat
			}
		}
	}
	if affectedComponents := stringItemsGo(data["affected_components"]); len(affectedComponents) > 0 {
		metadata["affected_components"] = affectedComponents
	}
	issueID := toStringValue(data["issue_id"])
	if issueID == "" {
		// Python: f"issue_{id(data)}"（对象地址保证唯一）；Go 无对象身份对等物，
		// 以 uuid 兜底（偏差 D-15：唯一性对齐，值每次运行变化）
		issueID = "issue_" + hexUUID()
	}
	var affectedCases []string
	switch list := data["affected_cases"].(type) {
	case []any:
		for _, item := range list {
			affectedCases = append(affectedCases, toStringValue(item))
		}
	case []string:
		affectedCases = append(affectedCases, list...)
	}
	var evidence []map[string]any
	if list, ok := data["evidence"].([]any); ok {
		for _, item := range list {
			if m, ok := item.(map[string]any); ok {
				evidence = append(evidence, m)
			}
		}
	}
	suspectedScope := toStringValue(data["suspected_team_scope"])
	if suspectedScope == "" {
		suspectedScope = "both"
	}
	severity := toStringValue(data["severity"])
	if severity == "" {
		severity = "medium"
	}
	var targetMembers []string
	if list, ok := data["target_members"].([]any); ok {
		for _, item := range list {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				targetMembers = append(targetMembers, strings.TrimSpace(s))
			}
		}
	}
	return harnessrsi.TeamIssue{
		IssueID:            issueID,
		Category:           category,
		Severity:           severity,
		Summary:            toStringValue(data["summary"]),
		AffectedCases:      affectedCases,
		Evidence:           evidence,
		SuspectedTeamScope: suspectedScope,
		TargetMembers:      targetMembers,
		Recommendation:     toStringValue(data["recommendation"]),
		Metadata:           metadata,
	}
}

// applyG5Mapping 从 suspected_team_scope+category 填充 optimization_target/target_members（Python _apply_g5_mapping）。
// metadata.attribution 的 target_ref 是给优化器的下游提示；显式 unassigned 或
// 指向评估器/分析器证据管线时，该问题被排除出优化器门。
func applyG5Mapping(issue harnessrsi.TeamIssue) harnessrsi.TeamIssue {
	targetRef := issueTargetRef(issue)
	if targetRef == "unassigned" || isEvidencePipelineFailure(issue) {
		issue.OptimizationTarget = ""
		issue.TargetMembers = nil
		return issue
	}
	targetScope := targetScopeFromTargetRef(targetRef)
	if targetScope == "member_harness" {
		if coordinatorIssue := coordinatorMemberIssueAsTeamSkill(issue, targetRef); coordinatorIssue != nil {
			return *coordinatorIssue
		}
		issue.OptimizationTarget = "member_harness"
		issue.TargetMembers = targetMembersFromIssue(issue)
		return issue
	}
	if targetScope == "team_skill" {
		issue.OptimizationTarget = "team_skill"
		issue.TargetMembers = nil
		return issue
	}
	if issue.SuspectedTeamScope == "member" || issue.Category == "member_harness" {
		issue.OptimizationTarget = "member_harness"
		issue.TargetMembers = targetMembersFromIssue(issue)
	} else {
		issue.OptimizationTarget = "team_skill"
		issue.TargetMembers = nil
	}
	return issue
}

// coordinatorMemberIssueAsTeamSkill 协调者/团队协议目标改道 Team Skill 优化（Python _coordinator_member_issue_as_team_skill）。
// team/team_leader 是 Agent Team 协调者而非业务 member harness；若分析器对协作或
// 完成门失败发出 member_harness.team.*，保持 member_harness 会让 MemberOptimizer
// 以 no_targets 失败（不存在这样的成员）。
func coordinatorMemberIssueAsTeamSkill(issue harnessrsi.TeamIssue, targetRef string) *harnessrsi.TeamIssue {
	role := targetMemberFromTargetRef(targetRef)
	if !isTeamCoordinatorRole(role) {
		return nil
	}
	teamTargetRef := coordinatorTeamSkillTargetRef(issue)
	metadata := withAttributionTargetRef(issue.Metadata, teamTargetRef)
	affectedComponents := stringItemsGo(metadata["affected_components"])
	if len(affectedComponents) == 0 {
		metadata["affected_components"] = []string{"team_leader"}
	}
	issue.Category = "team_coordination"
	issue.SuspectedTeamScope = "team_skill"
	issue.OptimizationTarget = "team_skill"
	issue.TargetMembers = nil
	issue.Metadata = metadata
	return &issue
}

// diagnosisGroupDiscriminator 聚合期间区分同案不同诊断（Python _diagnosis_group_discriminator）
func diagnosisGroupDiscriminator(diagnosis map[string]any) string {
	cluster := diagnosisFailureCluster(diagnosis)
	var checks []string
	for _, value := range stringItemsGo(cluster["failed_checks"]) {
		checks = append(checks, normalizeClusterText(value))
	}
	sort.Strings(checks)
	observable := normalizeClusterText(cluster["observable_behavior"])
	if len(checks) > 0 || observable != "" {
		payload, _ := json.Marshal(map[string]any{"checks": checks, "observable": observable})
		return string(payload)
	}
	return normalizeClusterText(orValue(diagnosis["root_cause"], diagnosis["summary"]))
}

// orValue 首个非空值（辅助，Python a or b）
func orValue(a, b any) any {
	if toStringValue(a) != "" {
		return a
	}
	return b
}

// diagnosisUnavailableResult 逐案诊断不可用兜底（Python _diagnosis_unavailable_result）
func diagnosisUnavailableResult(caseInput *CaseAnalysisInput, err error) map[string]any {
	errorText := err.Error()
	var errorType, mechanism string
	var outFormat *diagnosisOutputFormatError
	var contentErr *diagnosisContentError
	var execErr *DiagnosisAgentExecutionError
	switch {
	case errors.As(err, &outFormat):
		errorType = "output_format"
		mechanism = "The diagnosis model exhausted bounded JSON-format repair."
	case errors.As(err, &contentErr):
		errorType = "diagnosis_content"
		mechanism = "Valid JSON contained no usable diagnosis entries after bounded repair."
	case errors.As(err, &execErr):
		errorType = "agent_runtime"
		mechanism = "Diagnosis agent execution failed before completing an answer."
	default:
		errorType = "model_service"
		mechanism = "Retryable model-service failure during analyzer diagnosis."
	}
	return map[string]any{
		"case_id":              caseInput.CaseID,
		"score":                caseInput.Score,
		"evaluation_passed":    caseInput.EvaluationPassed,
		"evaluation_reason":    caseInput.EvaluationReason,
		"analysis_failed":      true,
		"diagnosis_status":     "unavailable",
		"issue_category":       "unassigned",
		"severity":             "low",
		"summary":              "Per-case diagnosis was unavailable because the diagnosis model returned no usable output.",
		"failure_mode":         "diagnosis_unavailable",
		"root_cause":           "Diagnosis model call did not produce usable output for this case.",
		"critical_mistake":     "No case-level attribution was produced.",
		"general_mechanism":    mechanism,
		"target_ref":           "unassigned",
		"evidence_refs":        []any{},
		"affected_components":  []any{},
		"recommendation":       "Do not optimize from this case-level diagnosis; rerun analysis or use other case diagnoses.",
		"confidence":           "low",
		"diagnosis_error_type": errorType,
		"error":                errorText,
	}
}

// diagnosisEvidenceConflictResult 证据矛盾兜底（Python _diagnosis_evidence_conflict_result）
func diagnosisEvidenceConflictResult(caseInput *CaseAnalysisInput, conflicts []string) map[string]any {
	return map[string]any{
		"case_id":             caseInput.CaseID,
		"score":               caseInput.Score,
		"evaluation_passed":   caseInput.EvaluationPassed,
		"evaluation_reason":   caseInput.EvaluationReason,
		"analysis_failed":     true,
		"diagnosis_status":    "evidence_conflict",
		"issue_category":      "unassigned",
		"severity":            "low",
		"summary":             "Diagnosis contradicted deterministic evaluation evidence.",
		"failure_mode":        "diagnosis_evidence_conflict",
		"root_cause":          strings.Join(conflicts, "; "),
		"critical_mistake":    "The diagnosis model contradicted code-derived test or verifier evidence.",
		"general_mechanism":   "Do not optimize from a diagnosis that contradicts deterministic evidence.",
		"target_ref":          "unassigned",
		"evidence_refs":       []any{},
		"affected_components": []any{},
		"recommendation":      "Rerun diagnosis with deterministic validation and verifier inventories enforced.",
		"confidence":          "low",
	}
}

// ──────────────────────────── 聚合（Python _aggregate_structured_diagnoses 等） ────────────────────────────

// aggregateStructuredDiagnoses 无第二次模型调用的规范化逐案诊断分组（Python _aggregate_structured_diagnoses）
func aggregateStructuredDiagnoses(perCaseResults []map[string]any, maxIssues, evidenceLimitPerIssue int) []harnessrsi.TeamIssue {
	type baseKey struct{ caseID, targetRef, failureMode string }
	baseCounts := map[baseKey]int{}
	for _, item := range perCaseResults {
		if b, _ := item["analysis_failed"].(bool); b {
			continue
		}
		targetRef := normalizeTargetRef(item["target_ref"])
		if targetRef == "" || targetRef == "unassigned" {
			continue
		}
		failureMode := toStringValue(item["failure_mode"])
		key := baseKey{toStringValue(item["case_id"]), targetRef, failureMode}
		baseCounts[key]++
	}
	// Python: {(t, f) for (_, t, f), count in base_counts.items() if count > 1}——
	// 统计每个 (target_ref, failure_mode) 组合被几个案例占据
	seenCaseByTF := map[[2]string]map[string]bool{}
	for key := range baseCounts {
		tf := [2]string{key.targetRef, key.failureMode}
		if seenCaseByTF[tf] == nil {
			seenCaseByTF[tf] = map[string]bool{}
		}
		seenCaseByTF[tf][key.caseID] = true
	}
	type groupKey struct{ targetRef, failureMode, discriminator string }
	groups := map[groupKey][]map[string]any{}
	for _, item := range perCaseResults {
		if b, _ := item["analysis_failed"].(bool); b {
			continue
		}
		targetRef := normalizeTargetRef(item["target_ref"])
		if targetRef == "" || targetRef == "unassigned" {
			continue
		}
		failureMode := toStringValue(item["failure_mode"])
		var discriminator string
		if len(seenCaseByTF[[2]string{targetRef, failureMode}]) > 1 {
			discriminator = diagnosisGroupDiscriminator(item)
		}
		key := groupKey{targetRef, failureMode, discriminator}
		groups[key] = append(groups[key], item)
	}
	type rankedGroup struct {
		key   groupKey
		items []map[string]any
	}
	var ranked []rankedGroup
	for key, items := range groups {
		ranked = append(ranked, rankedGroup{key, items})
	}
	maxSeverity := func(items []map[string]any) int {
		best := 0
		for _, item := range items {
			if r := severityRank(item["severity"]); r > best {
				best = r
			}
		}
		return best
	}
	maxConfidence := func(items []map[string]any) int {
		best := 0
		for _, item := range items {
			if r := confidenceRank(item["confidence"]); r > best {
				best = r
			}
		}
		return best
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		a, b := ranked[i], ranked[j]
		if maxSeverity(a.items) != maxSeverity(b.items) {
			return maxSeverity(a.items) > maxSeverity(b.items)
		}
		if maxConfidence(a.items) != maxConfidence(b.items) {
			return maxConfidence(a.items) > maxConfidence(b.items)
		}
		if a.key.targetRef != b.key.targetRef {
			return a.key.targetRef < b.key.targetRef
		}
		if a.key.failureMode != b.key.failureMode {
			return a.key.failureMode < b.key.failureMode
		}
		return a.key.discriminator < b.key.discriminator
	})
	limit := maxIssues
	if limit < 0 {
		limit = 0
	}
	if len(ranked) > limit {
		ranked = ranked[:limit]
	}
	issues := []harnessrsi.TeamIssue{}
	for index, group := range ranked {
		items := group.items
		strongest := items[0]
		for _, item := range items[1:] {
			si := []int{severityRank(item["severity"]), confidenceRank(item["confidence"])}
			ss := []int{severityRank(strongest["severity"]), confidenceRank(strongest["confidence"])}
			if si[0] > ss[0] || (si[0] == ss[0] && si[1] > ss[1]) {
				strongest = item
			}
		}
		var evidence []map[string]any
		for _, item := range items {
			components := stringItemsGo(item["affected_components"])
			component := ""
			if len(components) > 0 {
				component = components[0]
			}
			evidence = append(evidence, map[string]any{
				"case_id":            toStringValue(item["case_id"]),
				"failure_mode":       orValue(item["failure_mode"], group.key.failureMode),
				"affected_component": component,
				"failure_cluster":    diagnosisFailureCluster(item),
			})
		}
		seen := map[string]bool{}
		var affectedComponents []string
		for _, item := range items {
			for _, component := range stringItemsGo(item["affected_components"]) {
				if !seen[component] {
					seen[component] = true
					affectedComponents = append(affectedComponents, component)
				}
			}
		}
		issueCategory := issueCategoryFromTargetRef(group.key.targetRef)
		issueCategoryValue := issueCategory
		if issueCategoryValue != "member_harness" {
			issueCategoryValue = "team_coordination"
		}
		summary := toStringValue(strongest["summary"])
		severity := toStringValue(strongest["severity"])
		if severity == "" {
			severity = "medium"
		}
		recommendation := toStringValue(strongest["recommendation"])
		var affectedCases []string
		for _, item := range items {
			if cid := toStringValue(item["case_id"]); cid != "" {
				affectedCases = append(affectedCases, cid)
			}
		}
		evidenceLimit := evidenceLimitPerIssue
		if evidenceLimit < 1 {
			evidenceLimit = 1
		}
		if len(evidence) > evidenceLimit {
			evidence = evidence[:evidenceLimit]
		}
		decisionContract := map[string]any{}
		if dc, ok := strongest["decision_contract"].(map[string]any); ok {
			decisionContract = dc
		}
		var candidateExplanations []any
		if list, ok := strongest["candidate_explanations"].([]any); ok {
			candidateExplanations = list
		}
		var discriminatingEvidence []any
		if list, ok := strongest["discriminating_evidence"].([]any); ok {
			discriminatingEvidence = list
		}
		var taskContractChecks []any
		if list, ok := strongest["task_contract_checks"].([]any); ok {
			taskContractChecks = list
		}
		var evidenceRefs []any
		if list, ok := strongest["evidence_refs"].([]any); ok {
			evidenceRefs = list
		}
		failureLayer := strongest["failure_layer"]
		if failureLayer == nil {
			failureLayer = "unknown"
		}
		scope := "team_skill"
		if issueCategory == "member_harness" {
			scope = "member"
		}
		issue := dictToTeamIssue(map[string]any{
			"issue_id":             fmt.Sprintf("issue_%03d", index+1),
			"category":             issueCategoryValue,
			"severity":             severity,
			"summary":              summary,
			"affected_cases":       affectedCases,
			"affected_components":  affectedComponents,
			"evidence":             evidence,
			"suspected_team_scope": scope,
			"recommendation":       recommendation,
			"metadata": map[string]any{
				"attribution": map[string]any{
					"root_cause":              toStringValue(strongest["root_cause"]),
					"critical_mistake":        toStringValue(strongest["critical_mistake"]),
					"general_mechanism":       toStringValue(strongest["general_mechanism"]),
					"decision_contract":       decisionContract,
					"failure_cluster":         diagnosisFailureCluster(strongest),
					"failure_layer":           failureLayer,
					"observed_failure":        strongest["observed_failure"],
					"candidate_explanations":  candidateExplanations,
					"discriminating_evidence": discriminatingEvidence,
					"execution_termination":   strongest["execution_termination"],
					"evidence_manifest_path":  toStringValue(strongest["evidence_manifest_path"]),
					"task_contract_checks":    taskContractChecks,
					"target_ref":              group.key.targetRef,
					"evidence_refs":           evidenceRefs,
					"confidence":              toStringValue(strongest["confidence"]),
				},
			},
		})
		issues = append(issues, applyG5Mapping(issue))
	}
	return issues
}

// verifierTestIDsMatch 匹配报告 ID，仅允许唯一佐证的截断名（Python _verifier_test_ids_match）
func verifierTestIDsMatch(observed any, expected []string, failureOutput string) bool {
	list, ok := observed.([]any)
	if !ok {
		return false
	}
	var observedNames []string
	for _, item := range list {
		s, isStr := item.(string)
		if !isStr {
			return false
		}
		observedNames = append(observedNames, s)
	}
	if len(observedNames) != len(expected) {
		return false
	}
	same := true
	for i := range observedNames {
		if observedNames[i] != expected[i] {
			same = false
			break
		}
	}
	if same {
		return true
	}
	// 某些验证器报告仅保留首个空白分隔 token；共享前缀不是证据：
	// 要求唯一的 FAILED 日志记录佐证
	failures := map[string]map[string]bool{}
	for _, line := range strings.Split(failureOutput, "\n") {
		// Python: line.strip().split(maxsplit=1)——首 token 与余文整体
		parts := strings.SplitN(strings.TrimSpace(line), " ", 2)
		if len(parts) == 2 && parts[0] == "FAILED" {
			body := strings.TrimSpace(parts[1])
			head := strings.Fields(body)[0]
			if failures[head] == nil {
				failures[head] = map[string]bool{}
			}
			failures[head][body] = true
		}
	}
	for i, name := range observedNames {
		reported := expected[i]
		if name == reported {
			continue
		}
		if !strings.Contains(reported, "[") || strings.HasSuffix(reported, "]") {
			return false
		}
		records := failures[reported]
		if len(records) != 1 {
			return false
		}
		record := ""
		for k := range records {
			record = k
		}
		// 仅在日志佐证时归一化控制字符表示
		escaped := name
		for _, pair := range [][2]string{{"\n", `\n`}, {"\r", `\r`}, {"\t", `\t`}} {
			escaped = strings.ReplaceAll(escaped, pair[0], pair[1])
			escaped = strings.ReplaceAll(escaped, "\\"+pair[1], pair[1])
		}
		matched := false
		for _, variant := range []string{name, escaped} {
			if !strings.HasSuffix(variant, "]") {
				continue
			}
			if record == variant || strings.HasPrefix(record, variant+" - ") {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

// diagnosisValidationConflicts 拒绝与确定性测试/验证器事实矛盾的诊断（Python _diagnosis_validation_conflicts）
func diagnosisValidationConflicts(diagnosis map[string]any, inventory map[string]any, verifierInventory map[string]any, publicTask string) []string {
	var errs []string
	// Python: if public_task is not None——调用方恒传 case.input（可为空串），判据为"提供即校验"
	if publicTask != "" || true {
		errs = append(errs, NormalizeContractChecks(diagnosis, publicTask)...)
	}
	layer := toStringValue(diagnosis["failure_layer"])
	if layer == "" {
		layer = "unknown"
	}
	if layer != "runtime" && layer != "harness" && layer != "evidence" && layer != "unknown" {
		errs = append(errs, "[failure_attribution] failure_layer must be runtime/harness/evidence/unknown")
	}
	if layer == "evidence" || layer == "runtime" {
		target := toStringValue(diagnosis["target_ref"])
		if target == "" {
			target = "unassigned"
		}
		if layer == "evidence" && target != "unassigned" {
			errs = append(errs, "[failure_attribution] failure_layer=evidence requires target_ref=unassigned")
		}
		if layer == "runtime" && target != "unassigned" &&
			!(strings.HasPrefix(target, "member_harness.") && strings.HasSuffix(target, ".config")) {
			errs = append(errs, "[failure_attribution] runtime failures require restricted member config or unassigned")
		}
	}
	if inventory["project_test_suite_result"] == "passed" && inventory["authoritative_verifier_result"] == "failed" {
		observations, ok := diagnosis["validation_observations"].(map[string]any)
		if !ok {
			errs = append(errs, "missing validation_observations for local-pass/verifier-fail contradiction")
		} else {
			for key, want := range map[string]any{
				"project_test_suite_attempted":  true,
				"project_test_suite_result":     "passed",
				"authoritative_verifier_result": "failed",
			} {
				got := observations[key]
				if fmt.Sprintf("%v", got) != fmt.Sprintf("%v", want) {
					errs = append(errs, fmt.Sprintf("validation_observations.%s must equal %v", key, want))
				}
			}
			if strings.TrimSpace(toStringValue(observations["contradiction_explanation"])) == "" {
				errs = append(errs, "validation_observations.contradiction_explanation must be non-empty")
			}
		}
		recommendation := strings.Join(strings.Fields(strings.ToLower(toStringValue(diagnosis["recommendation"]))), " ")
		for _, phrase := range []string{
			"require running the project's existing test suite",
			"must run the project's own test suite",
			"rather than only a self-authored smoke",
			"instead of only a self-authored smoke",
		} {
			if strings.Contains(recommendation, phrase) {
				errs = append(errs, "recommendation contradicts observed successful project-suite execution")
				break
			}
		}
	}
	if verifierInventory == nil {
		verifierInventory = map[string]any{}
	}
	if b, ok := verifierInventory["patch_successfully_applied"].(bool); ok && b {
		if resolved, ok := verifierInventory["resolved"].(bool); ok && !resolved {
			observations, ok := diagnosis["verifier_observations"].(map[string]any)
			if !ok {
				errs = append(errs, "missing verifier_observations for applied-but-unresolved patch")
			} else {
				expectedVerifier := map[string]any{
					"patch_successfully_applied": true,
					"failed_fail_to_pass_tests":  verifierInventory["failed_fail_to_pass_tests"],
					"failed_pass_to_pass_tests":  verifierInventory["failed_pass_to_pass_tests"],
				}
				for key, want := range expectedVerifier {
					observed := observations[key]
					var matches bool
					if wantList, isList := want.([]string); isList {
						matches = verifierTestIDsMatch(observed, wantList, toStringValue(verifierInventory["verifier_failure_output_excerpt"]))
					} else {
						matches = fmt.Sprintf("%v", observed) == fmt.Sprintf("%v", want)
					}
					if !matches {
						errs = append(errs, fmt.Sprintf("verifier_observations.%s must equal %v", key, want))
					}
				}
			}
			var diagnosisTexts []string
			for _, key := range []string{"summary", "root_cause", "critical_mistake", "general_mechanism", "recommendation"} {
				diagnosisTexts = append(diagnosisTexts, strings.ToLower(toStringValue(diagnosis[key])))
			}
			diagnosisText := strings.Join(diagnosisTexts, " ")
			for _, claim := range []string{
				"patch application to fail", "patch application failed",
				"prevented patch application", "patch failed to apply",
				"could not apply the patch",
			} {
				if strings.Contains(diagnosisText, claim) {
					errs = append(errs, "diagnosis contradicts authoritative successful patch application")
					break
				}
			}
		}
	}
	return errs
}

// caseDiagnosesValidationConflicts 逐条校验并保留位置（Python _case_diagnoses_validation_conflicts）
func caseDiagnosesValidationConflicts(diagnoses []map[string]any, inventory, verifierInventory map[string]any, publicTask string) []string {
	var conflicts []string
	for index, diagnosis := range diagnoses {
		for _, errText := range diagnosisValidationConflicts(diagnosis, inventory, verifierInventory, publicTask) {
			conflicts = append(conflicts, fmt.Sprintf("diagnosis[%d]: %s", index+1, errText))
		}
	}
	return conflicts
}

// ──────────────────────────── 结构体 ────────────────────────────

// DiagnosisAgentStrategy 全链路 DeepAgent 策略：拥有读取、提取与诊断。
// Python: analyzer.py:2558-3145 DiagnosisAgentStrategy。
// analyze 只接收原始 invocation（Protocol 契约）；全部数据准备
// （经验检索、CaseReader、SignalExtractor）在类内运行。逐案 DeepAgent 使用
// 隔离证据与被评估 Harness 声明；原始案例目录不进 agent 工作区。
// 病历编译是确定性的，不调用第二个模型。
type DiagnosisAgentStrategy struct {
	// config 分析器配置
	config *rsiconfig.EvaluationResultAnalyzerConfig
	// caseReader 案例读取器
	caseReader *CaseReader
	// agentRuntime 诊断 agent 运行时
	agentRuntime *DiagnosisAgentRuntime
	// buildAgentFn agent 构建入口（默认 agentRuntime.BuildAgent；测试注入点，
	// 对齐 Python 测试 monkeypatch strategy._build_agent）
	buildAgentFn func(ctx context.Context, workspace, systemPrompt, invocationLogDir string) (interfaces.BaseAgent, error)
	// runAgentFn 运行入口（默认 runAgentWithRepair；测试注入点，对齐 Python
	// 测试 monkeypatch _run_agent）
	runAgentFn func(ctx context.Context, agent interfaces.BaseAgent, prompt string, maxRetries int) (string, error)
}

// StrategyName 策略名（Python DiagnosisAgentStrategy.name）
const StrategyName = "diagnosis_agent"

// ──────────────────────────── 导出函数 ────────────────────────────

// NewDiagnosisAgentStrategy 创建策略。
// Python: __init__(config)
func NewDiagnosisAgentStrategy(cfg *rsiconfig.EvaluationResultAnalyzerConfig) *DiagnosisAgentStrategy {
	applyConfigDefaults(cfg)
	strategy := &DiagnosisAgentStrategy{
		config:       cfg,
		caseReader:   NewCaseReader(),
		agentRuntime: NewDiagnosisAgentRuntime(cfg),
	}
	strategy.buildAgentFn = strategy.agentRuntime.BuildAgent
	strategy.runAgentFn = runAgentWithRepair
	return strategy
}

// AnalyzeForTest 测试注入点：直接驱动逐案诊断管线（绕过文件系统读取）。
// 对齐 Python 测试对 _per_case_diagnosis 的直接调用面。
func (s *DiagnosisAgentStrategy) AnalyzeForTest(ctx context.Context, caseInputs []*CaseAnalysisInput, signals *DeterministicSignals) []map[string]any {
	return s.perCaseDiagnosis(ctx, caseInputs, signals, map[string]any{}, "", nil, "", "", s.outputRootForTest())
}

// outputRootForTest 临时输出根（测试辅助）。
func (s *DiagnosisAgentStrategy) outputRootForTest() string {
	dir, err := os.MkdirTemp("", "ach_analyzer_artifacts_")
	if err != nil {
		return ""
	}
	return dir
}

// BuildAnalysisStrategy 工厂：从配置创建策略（Python build_analysis_strategy）。
// DeepAgent 构建推迟到首次 analyze 调用，model_config_ref 为空时本函数同样成功。
func BuildAnalysisStrategy(cfg *rsiconfig.EvaluationResultAnalyzerConfig) *DiagnosisAgentStrategy {
	return NewDiagnosisAgentStrategy(cfg)
}

// Analyze 从原始 invocation 运行完整分析管线到结构化问题清单。
// Python: analyze（步骤 1-5 见 doc 注释）
func (s *DiagnosisAgentStrategy) Analyze(ctx context.Context, invocation harnessrsi.EvaluationResultAnalysisInvocation) (harnessrsi.EvaluationResultAnalysisArtifact, error) {
	// 步骤 1：检索优化经验（零 LLM）——Go 侧占位（Python 同为空 dict 初始化）
	retrievedExperience := map[string]any{}

	// 步骤 2：读 eval_ref、summary、逐案输入
	evalRef, err := s.caseReader.ReadEvalRef(invocation.EvalRefPath)
	if err != nil {
		return harnessrsi.EvaluationResultAnalysisArtifact{}, err
	}
	summary := s.caseReader.ReadSummary(toStringValue(evalRef["summary_path"]))
	caseInputs := s.caseReader.ReadCaseInputs(invocation.CaseResultsDir)

	if len(caseInputs) == 0 {
		return harnessrsi.EvaluationResultAnalysisArtifact{
			AnalysisID: filepath.Base(invocation.OutputDir),
			Issues:     []harnessrsi.TeamIssue{},
			Metadata: map[string]any{
				"analysis_status":      "empty_case_results",
				"model_config_ref":     s.config.ModelConfigRef,
				"retrieved_experience": retrievedExperience,
			},
		}, nil
	}

	// 步骤 3：分发方法感知的 SignalExtractor（零 LLM）
	evalMethod := summary.EvaluationMethod
	if evalMethod == "" {
		evalMethod = "default"
	}
	extractor := BuildSignalExtractor(evalMethod)
	signalsValue := extractor.Extract(summary, caseInputs)
	signals := &signalsValue

	modelConfigRef := s.config.DiagnosisAgentModelConfigRef
	if modelConfigRef == "" {
		modelConfigRef = s.config.ModelConfigRef
	}
	if modelConfigRef == "" {
		return s.partialArtifact(invocation, "model_config_ref must be set", retrievedExperience), nil
	}

	outputDir, err := filepath.Abs(invocation.OutputDir)
	if err != nil {
		return harnessrsi.EvaluationResultAnalysisArtifact{}, err
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return harnessrsi.EvaluationResultAnalysisArtifact{}, err
	}

	// 只诊断未通过或不完整（连续分数）的案例
	var diagnosisCaseInputs []*CaseAnalysisInput
	for i := range caseInputs {
		caseInput := &caseInputs[i]
		if !caseInput.EvaluationPassed {
			diagnosisCaseInputs = append(diagnosisCaseInputs, caseInput)
			continue
		}
		metadata := caseInput.EvaluationMetadata
		continuousScore := false
		if metadata != nil {
			continuousScore = toStringValue(metadata["score_mode"]) == "continuous"
		}
		incomplete := caseInput.Score < 1.0 && (caseInput.EvaluationMethod != "llm_as_judge" || continuousScore)
		if incomplete {
			diagnosisCaseInputs = append(diagnosisCaseInputs, caseInput)
		}
	}

	// 步骤 4：逐案诊断（带被评估 Harness 与隔离证据）
	perCaseResults := s.perCaseDiagnosis(
		ctx, diagnosisCaseInputs, signals, retrievedExperience,
		invocation.SourceStage, invocation.PriorCandidateFeedback,
		invocation.EvalRefPath, invocation.HarnessRefsPath, outputDir,
	)
	perCaseDiagnosesPath := filepath.Join(outputDir, "per_case_diagnoses.json")
	if err := writeJSONFile(perCaseDiagnosesPath, map[string]any{"per_case_diagnoses": perCaseResults}); err != nil {
		return harnessrsi.EvaluationResultAnalysisArtifact{}, err
	}
	failedCaseIDs := map[string]bool{}
	for _, item := range perCaseResults {
		if b, _ := item["analysis_failed"].(bool); b {
			failedCaseIDs[toStringValue(item["case_id"])] = true
		}
	}
	partialCases := map[string]bool{}
	for _, item := range perCaseResults {
		if repair, ok := item["output_repair"].(map[string]any); ok {
			if toStringValue(repair["status"]) == "partial" {
				partialCases[toStringValue(item["case_id"])] = true
			}
		}
	}

	// 步骤 5：编译受支持的诊断为 TeamIssue，不再调用模型
	issues := aggregateStructuredDiagnoses(perCaseResults, s.config.MaxIssues, s.config.EvidenceLimitPerIssue)
	analysisStatus := "completed"
	if len(failedCaseIDs) == len(diagnosisCaseInputs) && len(failedCaseIDs) > 0 {
		analysisStatus = "failed"
	} else if len(failedCaseIDs) > 0 || len(partialCases) > 0 {
		analysisStatus = "partial"
	}
	mappedIssues := make([]harnessrsi.TeamIssue, 0, len(issues))
	for _, issue := range issues {
		mappedIssues = append(mappedIssues, applyG5Mapping(issue))
	}
	if len(mappedIssues) > s.config.MaxIssues {
		mappedIssues = mappedIssues[:s.config.MaxIssues]
	}
	modelRefFinal := s.config.DiagnosisAgentModelConfigRef
	if modelRefFinal == "" {
		modelRefFinal = s.config.ModelConfigRef
	}
	return harnessrsi.EvaluationResultAnalysisArtifact{
		AnalysisID: filepath.Base(invocation.OutputDir),
		Issues:     mappedIssues,
		Metadata: map[string]any{
			"analysis_status":         analysisStatus,
			"strategy":                StrategyName,
			"model_config_ref":        modelRefFinal,
			"signals_method":          signals.Method,
			"per_case_count":          len(caseInputs),
			"diagnosed_case_count":    len(diagnosisCaseInputs),
			"diagnosis_count":         len(perCaseResults),
			"diagnosis_failed_count":  len(failedCaseIDs),
			"diagnosis_partial_count": len(partialCases),
			"per_case_diagnoses_path": perCaseDiagnosesPath,
			"retrieved_experience":    retrievedExperience,
		},
	}, nil
}

// SetTestHooks 注入 agent 构建与运行测试替身（仅测试使用）。
// 对齐 Python 测试 monkeypatch strategy._build_agent / _run_agent 的注入面；
// nil 传入恢复默认实现。
func (s *DiagnosisAgentStrategy) SetTestHooks(
	buildAgent func(ctx context.Context, workspace, systemPrompt, invocationLogDir string) (interfaces.BaseAgent, error),
	runAgent func(ctx context.Context, agent interfaces.BaseAgent, prompt string, maxRetries int) (string, error),
) {
	if buildAgent != nil {
		s.buildAgentFn = buildAgent
	}
	if runAgent != nil {
		s.runAgentFn = runAgent
	}
}

// PartialArtifact 部分产物兜底（Python _partial_artifact）
func (s *DiagnosisAgentStrategy) PartialArtifact(invocation harnessrsi.EvaluationResultAnalysisInvocation, reason string, retrievedExperience map[string]any) harnessrsi.EvaluationResultAnalysisArtifact {
	return harnessrsi.EvaluationResultAnalysisArtifact{
		AnalysisID: filepath.Base(invocation.OutputDir),
		Issues:     []harnessrsi.TeamIssue{},
		Metadata: map[string]any{
			"analysis_status":      "partial",
			"strategy":             StrategyName,
			"failure_reason":       reason,
			"retrieved_experience": retrievedExperience,
		},
	}
}

// partialArtifact 内部转发（保持私有命名对应 Python）
func (s *DiagnosisAgentStrategy) partialArtifact(invocation harnessrsi.EvaluationResultAnalysisInvocation, reason string, retrievedExperience map[string]any) harnessrsi.EvaluationResultAnalysisArtifact {
	return s.PartialArtifact(invocation, reason, retrievedExperience)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// perCaseDiagnosis 按稳定确定性顺序逐案诊断（Python _per_case_diagnosis）。
// 每案获得包含有界诊断证据与（可用时）被评估仓库隔离拷贝的临时运行时
// 工作区；原始评估器案例目录永不暴露。
func (s *DiagnosisAgentStrategy) perCaseDiagnosis(
	ctx context.Context,
	caseInputs []*CaseAnalysisInput,
	signals *DeterministicSignals,
	retrievedExperience map[string]any,
	sourceStage string,
	priorCandidateFeedback map[string]any,
	evalRefPath, harnessRefsPath string,
	outputDir string,
) []map[string]any {
	runtimeRoot, err := os.MkdirTemp("", "ach_analyzer_")
	if err != nil {
		return []map[string]any{{"analysis_failed": true, "error": err.Error()}}
	}
	defer removePath(runtimeRoot)
	results := []map[string]any{}
	for _, caseInput := range caseInputs {
		results = append(results, s.diagnoseOne(
			ctx, caseInput, signals, retrievedExperience, sourceStage,
			priorCandidateFeedback, evalRefPath, harnessRefsPath,
			runtimeRoot, outputDir,
		)...)
	}
	return results
}

// diagnoseOne 单案诊断（Python _diagnose_one）
func (s *DiagnosisAgentStrategy) diagnoseOne(
	ctx context.Context,
	caseInput *CaseAnalysisInput,
	signals *DeterministicSignals,
	retrievedExperience map[string]any,
	sourceStage string,
	priorCandidateFeedback map[string]any,
	evalRefPath, harnessRefsPath string,
	runtimeRoot, outputDir string,
) []map[string]any {
	artifactRoot := outputDir
	evidenceArchiveBase := filepath.Join(artifactRoot, "diagnosis_attempts")
	var runtimeDir, attemptDir string
	var evidenceArchive string
	var manifestPath string
	cleanup := func() {}
	defer func() { cleanup() }()

	// 正常路径体（Python try 块），错误按类型分派
	result, diagErr := func() ([]map[string]any, error) {
		runtimeDir, attemptDir = allocateDiagnosisAttempt(runtimeRoot, evidenceArchiveBase, caseInput.CaseID, sourceStage)
		evidenceArchive = filepath.Join(attemptDir, "evidence")
		if err := os.MkdirAll(evidenceArchive, 0o755); err != nil {
			return nil, err
		}
		cleanup = func() { removePath(runtimeDir) }

		evidenceSummaryAvailable := prepareDiagnosisEvidence(caseInput, runtimeDir)
		runtimeEvidenceDir := filepath.Join(runtimeDir, "runtime_evidence")
		if info, statErr := os.Stat(runtimeEvidenceDir); statErr == nil && info.IsDir() {
			if err := copyDir(runtimeEvidenceDir, filepath.Join(evidenceArchive, "runtime_evidence")); err != nil {
				return nil, err
			}
		}
		for _, name := range []string{"evidence_manifest.json", "runtime_log_search.json", "task_clauses.json"} {
			source := filepath.Join(runtimeDir, name)
			if isRegularFile(source) {
				data, _ := os.ReadFile(source)
				if err := os.WriteFile(filepath.Join(evidenceArchive, name), data, 0o644); err != nil {
					return nil, err
				}
			}
		}
		caseFeedback := casePriorCandidateFeedback(priorCandidateFeedback, caseInput.CaseID)
		var harnessContext map[string]any
		if evalRefPath != "" {
			hc, err := PrepareHarnessContext(evalRefPath, harnessRefsPath, runtimeDir)
			if err != nil {
				return nil, err
			}
			harnessContext = hc
		}
		prompt := buildDiagnosisPrompt(
			caseInput, signals, retrievedExperience, evidenceSummaryAvailable,
			sourceStage, caseFeedback, harnessContext,
		)
		// Python: invocation_log_dir=str(attempt_dir / "logs")
		agent, err := s.buildAgentFn(ctx, runtimeDir, diagnosisSystemPrompt, filepath.Join(attemptDir, "logs"))
		if err != nil {
			return nil, err
		}
		validationInventory := buildValidationInventory(caseInput)
		verifierInventory := buildVerifierInventory(caseInput)
		raw, err := s.runAgentFn(ctx, agent, prompt, s.config.DiagnosisAgentMaxRetries)
		if err != nil {
			return nil, err
		}
		invalidOutputs := []string{raw}
		parsed := extractJSONObject(raw)
		var diagnoses []map[string]any
		if parsed != nil {
			diagnoses = normalizeCaseDiagnoses(parsed, caseFeedback)
		}
		if len(diagnoses) == 0 {
			repairRaw, repairErr := s.runAgentFn(ctx, agent, buildJSONRepairPrompt(prompt, raw), 0)
			if repairErr != nil {
				return nil, repairErr
			}
			invalidOutputs = append(invalidOutputs, repairRaw)
			repairParsed := extractJSONObject(repairRaw)
			if repairParsed != nil {
				repairDiagnoses := normalizeCaseDiagnoses(repairParsed, caseFeedback)
				if len(repairDiagnoses) > 0 {
					raw = repairRaw
					parsed = repairParsed
					diagnoses = repairDiagnoses
				}
			}
		}
		if len(diagnoses) == 0 {
			return nil, unusableDiagnosisOutputError(caseInput.CaseID, invalidOutputs)
		}
		validationConflicts := caseDiagnosesValidationConflicts(
			diagnoses, validationInventory, verifierInventory, caseInput.Input,
		)
		if len(validationConflicts) > 0 {
			repairRaw, repairErr := s.runAgentFn(ctx, agent, buildEvidenceConflictRepairPrompt(
				prompt, raw, validationConflicts, validationInventory, verifierInventory,
			), 0)
			if repairErr != nil {
				return nil, repairErr
			}
			repairParsed := extractJSONObject(repairRaw)
			if repairParsed != nil {
				repairDiagnoses := normalizeCaseDiagnoses(repairParsed, caseFeedback)
				if len(repairDiagnoses) > 0 {
					repairedConflicts := caseDiagnosesValidationConflicts(
						repairDiagnoses, validationInventory, verifierInventory, caseInput.Input,
					)
					if len(repairedConflicts) == 0 {
						diagnoses = repairDiagnoses
						validationConflicts = nil
					} else {
						validationConflicts = repairedConflicts
					}
				} else {
					validationConflicts = append(validationConflicts,
						"evidence-conflict repair output contained no diagnoses")
				}
			} else {
				validationConflicts = append(validationConflicts,
					"evidence-conflict repair output did not contain JSON")
			}
		}
		if len(validationConflicts) > 0 {
			logger.Warn(logger.ComponentAgentCore).
				Str("case_id", caseInput.CaseID).
				Str("conflicts", strings.Join(validationConflicts, "; ")).
				Msg("per-case diagnosis evidence conflict after repair")
			return []map[string]any{diagnosisEvidenceConflictResult(caseInput, validationConflicts)}, nil
		}
		execCtx := ReadExecutionContext(filepath.Dir(caseInput.ResultPath), caseInput.ExecutionMetadata, caseInput.TerminationReason)
		results := []map[string]any{}
		for index, diagnosis := range diagnoses {
			item := map[string]any{
				"case_id":                         caseInput.CaseID,
				"diagnosis_index":                 index + 1,
				"diagnosis_count":                 len(diagnoses),
				"score":                           caseInput.Score,
				"evaluation_passed":               caseInput.EvaluationPassed,
				"evaluation_reason":               caseInput.EvaluationReason,
				"execution_termination":           execCtx["execution_termination"],
				"verifier_failure_output_excerpt": toStringValue(verifierInventory["verifier_failure_output_excerpt"]),
			}
			for k, v := range diagnosis {
				item[k] = v
			}
			results = append(results, item)
		}
		return results, nil
	}()

	if diagErr != nil {
		outFormat := &diagnosisOutputFormatError{}
		contentErr := &diagnosisContentError{}
		execErr := &DiagnosisAgentExecutionError{}
		if errors.As(diagErr, &outFormat) || errors.As(diagErr, &contentErr) || errors.As(diagErr, &execErr) ||
			harnessrsi.IsRetryableModelCallFailure(diagErr) {
			logger.Warn(logger.ComponentAgentCore).
				Str("case_id", caseInput.CaseID).
				Err(diagErr).
				Msg("per-case diagnosis unavailable")
			failure := diagnosisUnavailableResult(caseInput, diagErr)
			failure["failure_layer"] = "evidence"
			if manifestPath != "" && isRegularFile(manifestPath) {
				failure["evidence_manifest_path"] = manifestPath
			} else if evidenceArchive != "" {
				p := filepath.Join(evidenceArchive, "evidence_manifest.json")
				if isRegularFile(p) {
					failure["evidence_manifest_path"] = p
				}
			}
			execCtx := ReadExecutionContext(filepath.Dir(caseInput.ResultPath), caseInput.ExecutionMetadata, caseInput.TerminationReason)
			failure["execution_termination"] = execCtx["execution_termination"]
			return []map[string]any{failure}
		}
		logger.Error(logger.ComponentAgentCore).
			Str("case_id", caseInput.CaseID).
			Err(diagErr).
			Msg("per-case diagnosis failed")
		return []map[string]any{{"analysis_failed": true, "error": diagErr.Error(), "case_id": caseInput.CaseID}}
	}
	return result
}

// allocateDiagnosisAttempt 预留一次诊断尝试的归档目录（Python _allocate_diagnosis_attempt）
func allocateDiagnosisAttempt(runtimeRoot, artifactRoot, caseID, sourceStage string) (string, string) {
	for i := 0; i < 10; i++ {
		runtimeDir := makeDiagnosisRuntimeDir(runtimeRoot)
		attemptDir := filepath.Join(artifactRoot, filepath.Base(runtimeDir))
		if err := ValidateDiagnosisPaths(runtimeDir, attemptDir, caseID, sourceStage); err != nil {
			return "", ""
		}
		// Python: attempt_dir.mkdir(parents=True, exist_ok=False)
		if err := os.MkdirAll(filepath.Dir(attemptDir), 0o755); err != nil {
			continue
		}
		if err := os.Mkdir(attemptDir, 0o755); err != nil {
			if os.IsExist(err) {
				continue
			}
			continue
		}
		if err := writeJSONFile(filepath.Join(attemptDir, "attempt.json"), map[string]any{
			"schema_version": 1,
			"case_id":        caseID,
			"attempt_id":     filepath.Base(attemptDir),
		}); err != nil {
			continue
		}
		return runtimeDir, attemptDir
	}
	return "", ""
}

// makeDiagnosisRuntimeDir 创建唯一案例作用域诊断运行时目录路径（Python _make_diagnosis_runtime_dir）
func makeDiagnosisRuntimeDir(runtimeRoot string) string {
	return filepath.Join(runtimeRoot, "d-"+hexUUID())
}

// loadCaseResult 读取评估器所有的 result 载荷而不暴露给 agent（Python _load_case_result）
func loadCaseResult(caseInput *CaseAnalysisInput) map[string]any {
	if !isRegularFile(caseInput.ResultPath) {
		return map[string]any{}
	}
	data, err := os.ReadFile(caseInput.ResultPath)
	if err != nil {
		return map[string]any{}
	}
	var payload map[string]any
	if json.Unmarshal(data, &payload) != nil {
		return map[string]any{}
	}
	return payload
}

// prepareRepositorySnapshotForCase 把被评估工作区拷入隔离诊断快照（Python _prepare_repository_snapshot）
func prepareRepositorySnapshotForCase(caseInput *CaseAnalysisInput, runtimeDir string) bool {
	result := loadCaseResult(caseInput)
	workspaceValue := ""
	if s, ok := result["workspace_dir"].(string); ok {
		workspaceValue = strings.TrimSpace(s)
	}
	patchValue := ""
	if evaluation, ok := result["evaluation"].(map[string]any); ok {
		if metadata, ok := evaluation["metadata"].(map[string]any); ok {
			if s, ok := metadata["model_patch_path"].(string); ok {
				patchValue = strings.TrimSpace(s)
			}
		}
	}
	artifactsDir := filepath.Join(filepath.Dir(caseInput.ResultPath), "artifacts")
	artifactsArg := ""
	if info, err := os.Stat(artifactsDir); err == nil && info.IsDir() {
		artifactsArg = artifactsDir
	}
	manifest, err := PrepareRepositorySnapshot(workspaceValue, patchValue, runtimeDir, artifactsArg)
	if err != nil {
		logger.Warn(logger.ComponentAgentCore).
			Str("case_id", caseInput.CaseID).
			Err(err).
			Msg("diagnosis snapshot failed; see repository_snapshot.json")
		return false
	}
	if errors, ok := manifest["errors"]; ok && fmt.Sprintf("%v", errors) != "[]" {
		logger.Warn(logger.ComponentAgentCore).
			Str("case_id", caseInput.CaseID).
			Int("copy_errors", len(toAnySlice(errors))).
			Msg("diagnosis snapshot has copy errors; see repository_snapshot.json")
	}
	return toStringValue(manifest["repository"]) == "complete"
}

// prepareDiagnosisEvidence 写入证据与隔离的被评估仓库快照（Python _prepare_diagnosis_evidence）
func prepareDiagnosisEvidence(caseInput *CaseAnalysisInput, runtimeDir string) bool {
	removePath(runtimeDir)
	if err := os.MkdirAll(runtimeDir, 0o755); err != nil {
		return false
	}
	prepareRepositorySnapshotForCase(caseInput, runtimeDir)
	_, _ = PrepareRuntimeEvidence(
		filepath.Dir(caseInput.ResultPath), caseInput.ExecutionMetadata, runtimeDir,
		caseInput.TerminationReason,
	)
	if err := writeJSONFile(filepath.Join(runtimeDir, "task_clauses.json"), map[string]any{
		"provenance": "case.input",
		"clauses":    BuildTaskClauses(caseInput.Input),
	}); err != nil {
		return false
	}
	history := ProjectExecutionHistory(
		readJSONIfExists(filepath.Join(filepath.Dir(caseInput.ResultPath), "judge", "normalized_trace.json")),
	)
	historyRaw, _ := json.Marshal(history)
	if err := os.WriteFile(filepath.Join(runtimeDir, "execution_history.json"), historyRaw, 0o644); err != nil {
		return false
	}
	summary := buildEvidenceSummary(caseInput)
	if strings.TrimSpace(summary) == "" {
		return false
	}
	summary += "\n\n## Repository Snapshot Availability\n" +
		"Read repository_snapshot.json before repository probes. It lists any missing " +
		"or unreadable files. Readable repository files and source_patch.diff are retained " +
		"independently. artifacts/ contains harvested agent outputs, not a complete " +
		"repository; its copy status is reported separately in the manifest. " +
		"A missing snapshot file is not evidence of missing source code. " +
		"Do not search outside this diagnosis workspace for omitted evidence.\n"
	if err := os.WriteFile(filepath.Join(runtimeDir, "evidence_summary.md"), []byte(summary), 0o644); err != nil {
		return false
	}
	return true
}

// copyDir 递归拷贝目录（Python shutil.copytree dirs_exist_ok=True）
func copyDir(src, dst string) error {
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		s := filepath.Join(src, entry.Name())
		d := filepath.Join(dst, entry.Name())
		if entry.IsDir() {
			if err := copyDir(s, d); err != nil {
				return err
			}
			continue
		}
		data, err := os.ReadFile(s)
		if err != nil {
			return err
		}
		if err := os.WriteFile(d, data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// writeJSONFile 缩进 JSON 落盘（Python _write_json）
func writeJSONFile(path string, payload map[string]any) error {
	raw, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o644)
}

// ──────────────────────────── 门面（Python EvaluationResultAnalyzer） ────────────────────────────

// EvaluationResultAnalyzer 薄门面：创建输出目录、委托策略、写产物（Python EvaluationResultAnalyzer:3146-3191）
type EvaluationResultAnalyzer struct {
	// config 分析器配置
	config *rsiconfig.EvaluationResultAnalyzerConfig
	// strategy 分析策略
	strategy *DiagnosisAgentStrategy
}

// NewEvaluationResultAnalyzer 创建门面。
func NewEvaluationResultAnalyzer(cfg *rsiconfig.EvaluationResultAnalyzerConfig) *EvaluationResultAnalyzer {
	return &EvaluationResultAnalyzer{config: cfg, strategy: BuildAnalysisStrategy(cfg)}
}

// applyConfigDefaults 归零值回填（Python dataclass 默认 max_issues=20/
// evidence_limit_per_issue=5/output_filename="issues.yaml"——直接构造配置对象时同样生效）
func applyConfigDefaults(cfg *rsiconfig.EvaluationResultAnalyzerConfig) {
	if cfg.MaxIssues == 0 {
		cfg.MaxIssues = 20
	}
	if cfg.EvidenceLimitPerIssue == 0 {
		cfg.EvidenceLimitPerIssue = 5
	}
	if cfg.OutputFilename == "" {
		cfg.OutputFilename = "issues.yaml"
	}
}

// Strategy 暴露内部策略（测试注入用；对齐 Python 测试对 strategy 内部方法的 monkeypatch）。
func (a *EvaluationResultAnalyzer) Strategy() *DiagnosisAgentStrategy {
	return a.strategy
}

// Analyze 分析评估结果并返回分析产物引用路径（Python analyze）。
// 返回写入的 analysis_ref.yaml 路径。
func (a *EvaluationResultAnalyzer) Analyze(ctx context.Context, invocation harnessrsi.EvaluationResultAnalysisInvocation) (string, error) {
	outputDir, err := filepath.Abs(invocation.OutputDir)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return "", err
	}
	issuesPath := filepath.Join(outputDir, a.config.OutputFilename)
	analysisRefPath := filepath.Join(outputDir, "analysis_ref.yaml")

	artifact, err := a.strategy.Analyze(ctx, invocation)
	if err != nil {
		return "", err
	}

	var issuesDicts []map[string]any
	for _, issue := range artifact.Issues {
		issuesDicts = append(issuesDicts, backfillIssueEvidenceRefs(teamIssueToDict(constrainIssueToInvocation(issue, invocation)), invocation))
	}
	if err := writeYAMLFile(issuesPath, map[string]any{"issues": issuesDicts}); err != nil {
		return "", err
	}
	analysisRef := buildAnalysisRefDict(outputDir, invocation, issuesPath, issuesDicts, artifact.Metadata)
	if err := writeYAMLFile(analysisRefPath, analysisRef); err != nil {
		return "", err
	}
	return analysisRefPath, nil
}

// teamIssueToDict TeamIssue → dict（Python asdict）
func teamIssueToDict(issue harnessrsi.TeamIssue) map[string]any {
	raw, _ := json.Marshal(issue)
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return out
}

// constrainIssueToInvocation 把分析器目标限制在本次运行暴露的优化面内（Python _constrain_issue_to_invocation）。
// standalone Harness invocation 有 harness refs 路径而无 Team Skill refs 路径；
// 模型仍可能把跨步行为描述为 team_skill——持久化该值会让 member-only 编译器
// 静默丢弃本可行动的问题。保留诊断，仅把所有权边界重映射到可用的 member Harness。
func constrainIssueToInvocation(issue harnessrsi.TeamIssue, invocation harnessrsi.EvaluationResultAnalysisInvocation) harnessrsi.TeamIssue {
	isSingleHarness := invocation.HarnessRefsPath != "" && invocation.TeamSkillRefPath == ""
	targetRef := issueTargetRef(issue)
	targetsTeamSkill := issue.OptimizationTarget == "team_skill" || strings.HasPrefix(targetRef, "team_skill.")
	if !isSingleHarness || !targetsTeamSkill {
		return issue
	}
	if strings.HasPrefix(targetRef, "team_skill.") {
		targetRef = "member_harness." + strings.TrimPrefix(targetRef, "team_skill.")
	} else if targetRef != "unassigned" {
		explicitMembers := targetMembersFromIssue(issue)
		if len(explicitMembers) > 0 {
			targetRef = fmt.Sprintf("member_harness.%s.prompt_section", explicitMembers[0])
		} else {
			targetRef = "unassigned"
		}
	}
	if targetRef == "unassigned" {
		issue.OptimizationTarget = ""
		issue.TargetMembers = nil
		return issue
	}
	metadata := withAttributionTargetRef(issue.Metadata, targetRef)
	issue.Category = "member_harness"
	issue.SuspectedTeamScope = "member"
	issue.OptimizationTarget = "member_harness"
	issue.Metadata = metadata
	issue.TargetMembers = targetMembersFromIssue(issue)
	return issue
}

// backfillIssueEvidenceRefs 模型输出缺 evidence_refs 时回填具体案例工件引用（Python _backfill_issue_evidence_refs）
func backfillIssueEvidenceRefs(issue map[string]any, invocation harnessrsi.EvaluationResultAnalysisInvocation) map[string]any {
	metadata := map[string]any{}
	if raw, ok := issue["metadata"].(map[string]any); ok {
		for k, v := range raw {
			metadata[k] = v
		}
	}
	attribution := map[string]any{}
	if raw, ok := metadata["attribution"].(map[string]any); ok {
		for k, v := range raw {
			attribution[k] = v
		}
	}
	if existing, ok := attribution["evidence_refs"].([]any); ok && len(existing) > 0 {
		return issue
	}
	var affectedCases []string
	if list, ok := issue["affected_cases"].([]any); ok {
		for _, item := range list {
			if s := strings.TrimSpace(toStringValue(item)); s != "" {
				affectedCases = append(affectedCases, s)
			}
		}
	}
	if len(affectedCases) == 0 {
		return issue
	}
	caseIndex := caseArtifactIndex(invocation.CaseResultsDir)
	var refs []any
	for _, caseID := range affectedCases {
		if ref, ok := caseIndex[caseID]; ok {
			refs = append(refs, ref)
		}
	}
	if len(refs) == 0 {
		return issue
	}
	if len(refs) > 3 {
		refs = refs[:3]
	}
	attribution["evidence_refs"] = refs
	metadata["attribution"] = attribution
	issue["metadata"] = metadata
	return issue
}

// caseArtifactIndex 案例工件索引（Python _case_artifact_index）
func caseArtifactIndex(caseResultsDir string) map[string]map[string]any {
	root, err := filepath.Abs(caseResultsDir)
	if err != nil {
		return map[string]map[string]any{}
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return map[string]map[string]any{}
	}
	index := map[string]map[string]any{}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		resultPath := filepath.Join(root, entry.Name(), "result.json")
		if !isRegularFile(resultPath) {
			continue
		}
		data, err := os.ReadFile(resultPath)
		if err != nil {
			return map[string]map[string]any{} // Python: raise ValueError
		}
		var result map[string]any
		if json.Unmarshal(data, &result) != nil {
			return map[string]map[string]any{}
		}
		caseID := strings.TrimSpace(toStringValue(result["case_id"]))
		if caseID == "" {
			continue
		}
		caseDir := filepath.Dir(resultPath)
		ref := map[string]any{
			"case_id":     caseID,
			"result_path": resultPath,
		}
		tracePath := filepath.Join(caseDir, "trace.json")
		if isRegularFile(tracePath) {
			ref["trace_path"] = tracePath
		}
		normalizedTracePath := filepath.Join(caseDir, "judge", "normalized_trace.json")
		if isRegularFile(normalizedTracePath) {
			ref["normalized_trace_path"] = normalizedTracePath
		}
		if _, exists := index[caseID]; !exists {
			index[caseID] = ref
		}
	}
	return index
}

// buildAnalysisRefDict 构建分析引用 dict（Python _build_analysis_ref_dict）
func buildAnalysisRefDict(
	outputDir string,
	invocation harnessrsi.EvaluationResultAnalysisInvocation,
	issuesPath string,
	issuesDicts []map[string]any,
	artifactMetadata map[string]any,
) map[string]any {
	// retrieved_experience 提升为顶层键（向后兼容）
	retrievedExperience := artifactMetadata["retrieved_experience"]
	coreMetadata := map[string]any{}
	for k, v := range artifactMetadata {
		if k != "retrieved_experience" {
			coreMetadata[k] = v
		}
	}
	return map[string]any{
		"analysis_id":          filepath.Base(outputDir),
		"created_at":           timeNow().Format("2006-01-02T15:04:05Z07:00"),
		"source_eval_ref_path": invocation.EvalRefPath,
		"case_results_dir":     invocation.CaseResultsDir,
		"case_traces_dir":      invocation.CaseTracesDir,
		"team_skill_ref_path":  invocation.TeamSkillRefPath,
		"harness_refs_path":    invocation.HarnessRefsPath,
		"issues_path":          issuesPath,
		"issues":               issuesDicts,
		"retrieved_experience": retrievedExperience,
		"metadata":             coreMetadata,
	}
}

// writeYAMLFile YAML 落盘（Python _write_yaml；键序保持插入序）
func writeYAMLFile(path string, payload map[string]any) error {
	data, err := yaml.Marshal(payload)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// timeNow 当前时间（测试注入点；对齐 datetime.now(timezone.utc).astimezone()）
var timeNow = time.Now
