// analyzer.go 评估结果诊断分析门面（17.4b2 Task 11 起）。
// Python: openjiuwen/rsi/harness_rsi/evaluation_result_analyzer/analyzer.py（3,352 行）。
// 上游吃 evaluator 成绩单，逐案深挖失败证据链，产出结构化病历（TeamIssue）。
// 本文件对应 Python 常量面板、提示词构建函数与证据组装辅助区（analyzer.py:78-87、
// 526-757、760-1053、1788-2099、2315-2432）；策略与门面在后续段（Task 12/13）。
package evaluationresultanalyzer

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
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
