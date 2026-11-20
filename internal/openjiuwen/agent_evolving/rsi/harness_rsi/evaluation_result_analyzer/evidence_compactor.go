// evidence_compactor.go 提供以决策为中心的确定性证据压缩。
// Python: openjiuwen/rsi/harness_rsi/evaluation_result_analyzer/evidence_compactor.py（全量对齐）。
// 全程无 LLM 摘要：精确数字与结构化工具参数原样保留，压缩只删行不改文。

package evaluationresultanalyzer

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// minSelectedCallsPerTrial 每 trial 挑选调用数下限。Python: _MIN_SELECTED_CALLS_PER_TRIAL
	minSelectedCallsPerTrial = 8
	// maxSelectedCallsPerTrial 每 trial 挑选调用数上限。Python: _MAX_SELECTED_CALLS_PER_TRIAL
	maxSelectedCallsPerTrial = 20
	// maxToolSequence 工具序列截断长度。Python: _MAX_TOOL_SEQUENCE
	maxToolSequence = 80
)

// 各正则逐字对齐 Python 模块级常量。
var (
	// Python: _MUTATING_TOOL_PATTERN
	mutatingToolPattern = regexp.MustCompile(`(?i)(?:^|_)(?:assign|cancel|create|delete|disable|enable|finish|modify|order|patch|save|schedule|send|submit|` +
		`update|write)(?:_|$)`)
	// Python: _ARTIFACT_PATTERN
	artifactPattern = regexp.MustCompile(`(?i)(?:^|[\s` + "`'\"`" + `])([^\s` + "`'\"`" + `]+\.(?:csv|docx|html|json|md|pdf|pptx|txt|xlsx))(?:$|[\s` + "`'\"`" + `])`)
	// Python: _FAILED_RESPONSE_PATTERN
	failedResponsePattern = regexp.MustCompile(`(?i)(?:success\s*=\s*false|"success"\s*:\s*false|exit\s+code\s*:\s*[1-9]\d*|traceback\s*\(|access\s+denied)`)
	// Python: _BASH_MUTATION_PATTERN
	bashMutationPattern = regexp.MustCompile(`(?i)(?:^|[;&|\n]\s*)(?:cp|mv|rm|mkdir|touch|install)\b|` +
		`(?:\.save\s*\(|save_workbook\s*\(|write_text\s*\(|write_bytes\s*\(|` +
		`shutil\.(?:copy|copy2|copytree|move)|libreoffice\b.*--convert-to|(?:^|\s)>\s*[^&])`)
	// Python: _BASH_CONTENT_PATTERN
	bashContentPattern = regexp.MustCompile(`(?is)(?:pdftotext|python\w*\b.*(?:docx|openpyxl|pypdf|python-pptx)|` +
		`(?:cat|sed|head|tail|unzip)\s)`)
	// Python: _CRITICAL_EVIDENCE_TERM_PATTERN
	criticalEvidenceTermPattern = regexp.MustCompile(`[A-Za-z][A-Za-z0-9_.-]{2,}|\d+(?:\.\d+)?|[\x{4e00}-\x{9fff}]{2,8}`)
)

// criticalEvidenceStopwords 关键证据停用词。
// Python: evidence_compactor.py:42-82 _CRITICAL_EVIDENCE_STOPWORDS
var criticalEvidenceStopwords = map[string]bool{
	"and": true, "agent": true, "answer": true, "assessment": true, "because": true,
	"between": true, "but": true, "calculating": true, "conclusion": true, "correctly": true,
	"criterion": true, "did": true, "directly": true, "evidence": true, "explicitly": true,
	"failed": true, "failure": true, "first": true, "identified": true, "met": true,
	"must": true, "not": true, "requirement": true, "required": true, "response": true,
	"second": true, "specific": true, "state": true, "stated": true, "states": true,
	"task": true, "text": true, "than": true, "the": true, "that": true, "this": true,
	"timeline": true, "timelines": true, "with": true,
}

// compactionMarkerFn 生成省略标记（Python format 模板）。
// Python: evidence_compactor.py:83-85 _COMPACTION_MARKER
func compactionMarkerFn(omitted int) string {
	return fmt.Sprintf("[ANALYZER_EVIDENCE_COMPACTION: omitted %d source chars; this marker was not observed by the task agent]", omitted)
}

// ──────────────────────────── 导出函数 ────────────────────────────

// PublicTaskContractSnapshot 保留公开任务与工具契约、剔除评分器内部件。
// Python: evidence_compactor.py:88-115 public_task_contract_snapshot
func PublicTaskContractSnapshot(task map[string]any) map[string]any {
	// Python: public = task.get("public_task_contract") if isinstance(Mapping) else {}
	public := mapField(task, "public_task_contract")
	// Python: metadata = public.get("metadata", task.get("metadata"))
	metadata, ok := public["metadata"].(map[string]any)
	if !ok {
		metadata = mapField(task, "metadata")
	}
	// Python: tool_schemas = public.get("tool_schemas") if isinstance(Sequence) and not str
	var toolSchemas []any
	if raw, isList := public["tool_schemas"].([]any); isList {
		toolSchemas = raw
	}

	taskMetadata := map[string]any{}
	for key, value := range metadata {
		// Python: if key in {canary, category, difficulty, language} and _is_scalar(value)
		if (key == "canary" || key == "category" || key == "difficulty" || key == "language") && isScalar(value) {
			taskMetadata[key] = value
		}
	}

	normalizedToolSchemas := []map[string]any{}
	for _, rawSchema := range toolSchemas {
		schema, isMap := rawSchema.(map[string]any)
		if !isMap {
			continue
		}
		normalized := normalizeToolSchema(schema)
		if len(normalized) > 0 {
			normalizedToolSchemas = append(normalizedToolSchemas, normalized)
		}
	}

	return map[string]any{
		"schema_version": 1,
		"provenance":     "official_suite.public_task_contract",
		// Python: str(task.get("id") or public.get("task_id") or "")
		"task_id":       stringOr(stringField(task, "id", ""), stringField(public, "task_id", "")),
		"domain":        stringOr(stringField(public, "domain", ""), stringField(task, "domain", "")),
		"prompt":        stringOr(stringField(public, "prompt", ""), stringField(task, "prompt", "")),
		"task_metadata": taskMetadata,
		"tool_schemas":  normalizedToolSchemas,
	}
}

// LoadPublicTaskContract 载入已物化的公开契约，旧运行安全回落。
// Python: evidence_compactor.py:118-148 load_public_task_contract
func LoadPublicTaskContract(caseID, resultPath string, evaluationMetadata map[string]any, taskInput string) map[string]any {
	// Python: direct = evaluation_metadata.get("analysis_task_contract")
	if direct, isMap := evaluationMetadata["analysis_task_contract"].(map[string]any); isMap {
		return direct
	}

	// Python: evaluation_dir = result.parent.parent.parent if len(result.parents) >= 3 else Path()
	evaluationDir := ""
	abs := resultPath
	if !filepath.IsAbs(abs) {
		abs, _ = filepath.Abs(abs)
	}
	if filepathDepthParents(abs) >= 3 {
		evaluationDir = filepath.Dir(filepath.Dir(filepath.Dir(abs)))
	}
	// Python: suite_path = evaluation_dir / "official" / "suite.json"
	suite := readMapping(filepath.Join(evaluationDir, "official", "suite.json"))
	// Python: for split_name in ("validation", "evaluation", "tasks")
	for _, splitName := range []string{"validation", "evaluation", "tasks"} {
		tasks, isList := suite[splitName].([]any)
		if !isList {
			continue
		}
		for _, rawTask := range tasks {
			task, isMap := rawTask.(map[string]any)
			if isMap && stringOr(stringField(task, "id", ""), "") == caseID {
				return PublicTaskContractSnapshot(task)
			}
		}
	}

	return map[string]any{
		"schema_version": 1,
		"provenance":     "case.input",
		"task_id":        caseID,
		"prompt":         taskInput,
		"tool_schemas":   []any{},
	}
}

// BuildCausalEvidenceDigest 构建保留 trial 差异与精确动作的紧凑证据包。
// Python: evidence_compactor.py:151-248 build_causal_evidence_digest
func BuildCausalEvidenceDigest(params map[string]any) map[string]any {
	// Python 关键字参数展开（签名顺序对齐）
	caseID, _ := params["case_id"].(string)
	// Python: task_input 为关键字参数（仅在 LoadPublicTaskContract 契约回落使用，digest 无输出）
	_ = params["task_input"]
	response, _ := params["response"].(string)
	evaluationPassed, _ := params["evaluation_passed"].(bool)
	evaluationScore := floatField(params, "evaluation_score", 0.0)
	evaluationReason, _ := params["evaluation_reason"].(string)
	evaluationMetadata := mapField(params, "evaluation_metadata")
	traceData := mapField(params, "trace_data")
	taskContract := mapField(params, "task_contract")

	// Python: traces = trace_data.get("traces") if isinstance(list) else []
	traces, _ := traceData["traces"].([]any)
	// Python: _aligned_list(metadata.get("trial_scores"), len(traces))
	trialScores := alignedList(evaluationMetadata["trial_scores"], len(traces))
	trialPassed := alignedList(evaluationMetadata["trial_passed"], len(traces))
	trialExitReasons := alignedList(evaluationMetadata["trial_exit_reasons"], len(traces))
	trialDetails := alignedList(evaluationMetadata["trial_details"], len(traces))
	// Python: has_trial_outcomes = bool(traces) and bool(trial_scores) and bool(trial_passed)
	hasTrialOutcomes := len(traces) > 0 && len(trialScores) > 0 && len(trialPassed) > 0
	// Python: critical_evidence_terms = _failed_requirement_terms(evaluation_metadata)
	criticalEvidenceTerms := failedRequirementTerms(evaluationMetadata)

	var trialRecords []map[string]any
	var allCalls []map[string]any
	rawMessageCount := 0
	for index, rawTrace := range traces {
		trace, isMap := rawTrace.(map[string]any)
		if !isMap {
			continue
		}
		// Python: score = trial_scores[index] if has_trial_outcomes else (evaluation_score if len(traces)==1 else None)
		var score any
		var passed any
		if hasTrialOutcomes {
			score = trialScores[index]
			passed = trialPassed[index]
		} else if len(traces) == 1 {
			score = evaluationScore
			passed = evaluationPassed
		}
		// Python: exit_reason = trial_exit_reasons[index] if trial_exit_reasons else ""
		exitReason := ""
		if len(trialExitReasons) > 0 {
			exitReason = toDisplayString(trialExitReasons[index])
		}
		trial := buildTrialRecord(trace, index, score, passed, exitReason, criticalEvidenceTerms)
		var detail any
		if len(trialDetails) > 0 {
			detail = trialDetails[index]
		}
		trial["trial_evaluation"] = compactTrialEvaluation(detail)
		// Python: raw_message_count += int(trial.pop("_raw_message_count")); all_calls.extend(trial.pop("_all_calls"))
		rawMessageCount += intField(trial, "_raw_message_count", 0)
		delete(trial, "_raw_message_count")
		if calls, ok := trial["_all_calls"].([]map[string]any); ok {
			allCalls = append(allCalls, calls...)
		}
		delete(trial, "_all_calls")
		trialRecords = append(trialRecords, trial)
	}

	// Python: _deduplicate_selected_payloads(trial_records)（原地修改）
	deduplicateSelectedPayloads(trialRecords)
	// Python: tool_contracts = _tool_contract_observations(task_contract, all_calls)
	toolContracts := toolContractObservations(taskContract, allCalls)

	// Python: compact_task_contract = {k: _compact_value(v) for k,v in ... if k not in {prompt, tool_schemas}}
	compactTaskContract := map[string]any{}
	for key, value := range taskContract {
		if key != "prompt" && key != "tool_schemas" {
			compactTaskContract[key] = compactValue(value)
		}
	}
	compactTaskContract["prompt_ref"] = "authoritative_task_contract.input_excerpt"
	compactTaskContract["tool_schema_ref"] = "tool_contract_observations"

	// Python: fallback_final_response 条件结构
	fallbackFinalResponse := map[string]any{"available": false, "reason": "per_trial_final_outputs_available"}
	if len(trialRecords) == 0 {
		fallback := map[string]any{"available": true}
		for key, value := range outputSummary(response) {
			fallback[key] = value
		}
		fallbackFinalResponse = fallback
	}

	// Python: selected_tool_call_count = sum(len(trial["selected_actions"]))
	selectedToolCallCount := 0
	for _, trial := range trialRecords {
		if actions, ok := trial["selected_actions"].([]map[string]any); ok {
			selectedToolCallCount += len(actions)
		}
	}

	return map[string]any{
		"schema_version": 1,
		"compression_policy": map[string]any{
			"method":                     "deterministic_decision_centered",
			"no_llm_summarization":       true,
			"trial_boundaries_preserved": hasTrialOutcomes,
			"exact_numbers_and_structured_tool_arguments_preserved": true,
			"action_selection": "dynamic decision-linked budget; prioritize observed failures, state mutations, " +
				"content-bearing reads, tool boundaries, and the terminal window",
			"response_only_field_policy": "A response-only field is not a missing request field unless the public tool schema declares it.",
			"lossless_evidence_policy": "Compacted response excerpts are display views, not task-agent observations. " +
				"ANALYZER_EVIDENCE_COMPACTION markers are generated after execution. " +
				"Exact failed-requirement-linked spans are retained separately with raw evidence pointers.",
		},
		"task_contract": compactTaskContract,
		"outcome": map[string]any{
			"case_id": caseID,
			"passed":  evaluationPassed,
			"score":   evaluationScore,
			"reason":  compactText(evaluationReason, 1000),
			// Python: trial_count = len(trial_records)
			"trial_count":      len(trialRecords),
			"judge_dimensions": judgeDimensions(evaluationMetadata),
			"judge_evidence":   compactJudgeEvidence(evaluationMetadata),
		},
		"tool_contract_observations": toolContracts,
		"critical_evidence_terms":    criticalEvidenceTerms,
		"trials":                     trialRecords,
		"cross_trial_contrast":       crossTrialContrast(trialRecords),
		"fallback_final_response":    fallbackFinalResponse,
		"compression_stats": map[string]any{
			"raw_trace_count":          len(traces),
			"raw_message_count":        rawMessageCount,
			"raw_tool_call_count":      len(allCalls),
			"selected_tool_call_count": selectedToolCallCount,
		},
	}
}

// CompactCandidateFeedback 归一化成对干预证据而不丢失结果信号。
// Python: evidence_compactor.py:339-462 compact_candidate_feedback
func CompactCandidateFeedback(feedback map[string]any) map[string]any {
	if feedback == nil {
		return map[string]any{}
	}
	// Python: records = feedback.get("experiments") if isinstance(list)
	records, isList := feedback["experiments"].([]any)
	if !isList {
		return map[string]any{}
	}
	var experiments []map[string]any
	// Python: for record in records[-3:]
	start := 0
	if len(records) > 3 {
		start = len(records) - 3
	}
	for _, rawRecord := range records[start:] {
		record, isMap := rawRecord.(map[string]any)
		if !isMap {
			continue
		}
		diagnoses, _ := record["candidate_failure_diagnoses"].([]any)
		rawObserved := mapField(record, "observed_outcome")
		rawPrediction := mapField(record, "prediction")

		// Python: strict_score = _compact_score_comparison(raw_observed.get("strict_score"), source=..., candidate=..., delta=...)
		strictScore := compactScoreComparison(
			rawObserved["strict_score"],
			firstPresent([]string{"source_target_score", "source_strict_score"}, rawObserved, record),
			firstPresent([]string{"candidate_target_score", "candidate_strict_score"}, rawObserved, record),
			firstPresent([]string{"target_score_delta", "strict_score_delta"}, rawObserved, record),
		)
		continuousScore := compactScoreComparison(
			rawObserved["continuous_score"],
			firstPresent([]string{"source_native_score", "source_continuous_score"}, rawObserved, record),
			firstPresent([]string{"candidate_native_score", "candidate_continuous_score"}, rawObserved, record),
			firstPresent([]string{"native_score_delta", "continuous_score_delta"}, rawObserved, record),
		)
		// Python: continuous_score.update({... signal 字段 ...})
		continuousScore["source_signal"] = stringOr(
			toDisplayString(firstPresent([]string{"source_native_signal", "source_continuous_signal"}, rawObserved, record)), "")
		continuousScore["candidate_signal"] = stringOr(
			toDisplayString(firstPresent([]string{"candidate_native_signal", "candidate_continuous_signal"}, rawObserved, record)), "")
		continuousScore["role"] = stringOr(
			toDisplayString(firstPresent([]string{"native_signal_role"}, rawObserved, record)), "")

		requirementDelta := firstMapping(
			rawObserved["requirement_delta"], rawObserved["verifier_delta"],
			record["requirement_delta"], record["verifier_delta"])
		dimensionDeltas := firstMapping(
			rawObserved["dimension_deltas"], rawObserved["native_dimension_deltas"],
			record["dimension_deltas"], record["native_dimension_deltas"])

		// Python: contracts = raw_prediction.get(...) if list else record.get(...); if not list: []
		contracts, contractsIsList := rawPrediction["causal_intervention_contracts"].([]any)
		if !contractsIsList {
			contracts, contractsIsList = record["causal_intervention_contracts"].([]any)
		}
		if !contractsIsList {
			contracts = []any{}
		}
		activation := mapField(record, "activation")

		observedOutcome := map[string]any{
			"status":                 stringOr(toDisplayString(firstPresent([]string{"status", "outcome"}, rawObserved, record)), ""),
			"reason":                 stringOr(toDisplayString(firstPresent([]string{"reason"}, rawObserved, record)), ""),
			"strict_score":           strictScore,
			"continuous_score":       continuousScore,
			"requirement_delta":      compactValue(requirementDelta),
			"dimension_deltas":       compactValue(dimensionDeltas),
			"selected_for_promotion": firstPresent([]string{"selected_for_promotion"}, rawObserved, record),
			// Python: v1 别名保留（strict_score["source"] 等）
			"source_target_score":    strictScore["source"],
			"candidate_target_score": strictScore["candidate"],
			"target_score_delta":     strictScore["delta"],
			"source_native_score":    continuousScore["source"],
			"candidate_native_score": continuousScore["candidate"],
			"native_score_delta":     continuousScore["delta"],
			"verifier_delta":         compactValue(requirementDelta),
		}

		experiments = append(experiments, map[string]any{
			"schema_version":  2,
			"experiment_id":   stringOr(stringField(record, "experiment_id", ""), ""),
			"surface":         stringOr(stringField(record, "surface", ""), ""),
			"predicted_rank":  record["predicted_rank"],
			"predicted_score": record["predicted_score"],
			"prediction": map[string]any{
				"predicted_rank":                firstPresent([]string{"predicted_rank"}, rawPrediction, record),
				"predicted_score":               firstPresent([]string{"predicted_score"}, rawPrediction, record),
				"candidate_patch_excerpt":       compactText(firstPresent([]string{"candidate_patch_excerpt"}, rawPrediction, record), 2000),
				"causal_intervention_contracts": compactValue(contracts),
			},
			"observed_outcome":              observedOutcome,
			"activation":                    compactValue(activation),
			"causal_intervention_contracts": compactValue(contracts),
			"verifier_delta":                compactValue(requirementDelta),
			"candidate_failure_diagnoses":   compactCandidateFailureDiagnoses(diagnoses),
		})
	}
	return map[string]any{
		"case_id":     stringOr(stringField(feedback, "case_id", ""), ""),
		"experiments": experiments,
	}
}

// ExtractCriticalEvidenceSpans 返回与失败条款词绑定的精确行窗口。
// 与展示摘录不同：这些窗口取自压缩前的原始工具响应；返回文本永不使用未标注省略。
// Python: evidence_compactor.py:963-1025 extract_critical_evidence_spans
func ExtractCriticalEvidenceSpans(value any, terms []string, maxSpans, maxTotalChars int) []map[string]any {
	str := ""
	if s, isStr := value.(string); isStr {
		str = s
	} else {
		str = toDisplayString(value)
	}
	// Python: normalized_terms = list(dict.fromkeys(str(term).casefold() for term in terms if str(term).strip()))
	normalizedTerms := []string{}
	seenTerms := map[string]bool{}
	for _, term := range terms {
		if strings.TrimSpace(term) == "" {
			continue
		}
		lowered := strings.ToLower(term)
		if !seenTerms[lowered] {
			seenTerms[lowered] = true
			normalizedTerms = append(normalizedTerms, lowered)
		}
	}
	text, projection := readableEvidenceText(str)
	if text == "" || len(normalizedTerms) == 0 {
		return nil
	}
	if maxSpans <= 0 || maxTotalChars <= 0 {
		return nil
	}

	// Python: lines = text.splitlines() or [text]
	lines := splitLinesPlain(text)
	if len(lines) == 0 {
		lines = []string{text}
	}

	type rankedItem struct {
		matchCount int
		index      int
		matched    []string
	}
	var ranked []rankedItem
	for index, line := range lines {
		lowered := strings.ToLower(line)
		var matched []string
		matchedSet := map[string]bool{}
		for _, term := range normalizedTerms {
			if strings.Contains(lowered, term) && !matchedSet[term] {
				matchedSet[term] = true
				matched = append(matched, term)
			}
		}
		if len(matched) > 0 {
			ranked = append(ranked, rankedItem{matchCount: len(matchedSet), index: index, matched: matched})
		}
	}
	if len(ranked) == 0 {
		return nil
	}

	// Python: sorted(ranked, key=lambda item: (-item[0], item[1]))[:max_spans]
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].matchCount != ranked[j].matchCount {
			return ranked[i].matchCount > ranked[j].matchCount
		}
		return ranked[i].index < ranked[j].index
	})
	limit := maxSpans
	if limit > len(ranked) {
		limit = len(ranked)
	}
	selectedLines := ranked[:limit]

	// Python: for _, index, matched in sorted(selected_lines, key=lambda item: item[1])
	sort.SliceStable(selectedLines, func(i, j int) bool {
		return selectedLines[i].index < selectedLines[j].index
	})
	type window struct {
		start, end int
		matched    map[string]bool
	}
	var windows []window
	for _, item := range selectedLines {
		// Python: start = max(0, index-3); end = min(len(lines), index+4)
		start := item.index - 3
		if start < 0 {
			start = 0
		}
		end := item.index + 4
		if end > len(lines) {
			end = len(lines)
		}
		matchedSet := map[string]bool{}
		for _, term := range item.matched {
			matchedSet[term] = true
		}
		// Python: if windows and start <= windows[-1][1]: 合并
		if len(windows) > 0 && start <= windows[len(windows)-1].end {
			last := &windows[len(windows)-1]
			if end > last.end {
				last.end = end
			}
			for term := range matchedSet {
				last.matched[term] = true
			}
		} else {
			windows = append(windows, window{start: start, end: end, matched: matchedSet})
		}
	}

	var spans []map[string]any
	remaining := maxTotalChars
	for _, win := range windows {
		if remaining <= 0 {
			break
		}
		rawSpan := strings.Join(lines[win.start:win.end], "\n")
		matchedList := make([]string, 0, len(win.matched))
		for term := range win.matched {
			matchedList = append(matchedList, term)
		}
		spanText, complete := boundedCriticalSpan(rawSpan, win.matched, remaining)
		if spanText == "" {
			continue
		}
		sortedMatched := append([]string(nil), matchedList...)
		sort.Strings(sortedMatched)
		spans = append(spans, map[string]any{
			"source":          "raw_tool_response",
			"projection":      projection,
			"line_start":      win.start + 1,
			"line_end":        win.end,
			"matched_terms":   sortedMatched,
			"text":            spanText,
			"window_complete": complete,
		})
		remaining -= len(spanText)
	}
	return spans
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// compactTrialEvaluation 保留每 trial 评分器证据、缺失不视为零。
// Python: evidence_compactor.py:251-299 _compact_trial_evaluation
func compactTrialEvaluation(value any) map[string]any {
	m, isMap := value.(map[string]any)
	if !isMap {
		return map[string]any{
			"schema_version": 1,
			"availability": map[string]any{
				"score_file": "not_instrumented", "score_reason": "not_instrumented",
				"judge_detail": "not_instrumented", "dimension_scores": "not_instrumented",
			},
			"score_reason": "", "judge_detail": nil, "dimension_scores": map[string]any{}, "source": map[string]any{},
		}
	}
	// Python: raw_availability → availability = {str(k): str(v)}
	rawAvailability := mapField(m, "availability")
	availability := map[string]any{}
	for key, item := range rawAvailability {
		if str, isStr := item.(string); isStr {
			availability[key] = str
		}
	}
	// Python: raw_dimensions → dimensions
	rawDimensions := mapField(m, "dimension_scores")
	dimensions := map[string]any{}
	for name, rawDimension := range rawDimensions {
		dim, isMap := rawDimension.(map[string]any)
		if !isMap {
			continue
		}
		// Python: state = str(raw_dimension.get("availability") or "not_available")
		state := stringField(dim, "availability", "")
		if state == "" {
			state = "not_available"
		}
		rawScore := dim["value"]
		// Python: value = raw_score if _is_number(raw_score) else None
		var scoreValue any
		if isNumber(rawScore) {
			scoreValue = rawScore
		}
		// Python: source = str(...) if raw_dimension.get("source") else None
		var sourceValue any
		if rawSource, hasSource := dim["source"]; hasSource && rawSource != nil {
			if s, isStr := rawSource.(string); isStr && s != "" {
				sourceValue = s
			} else if _, isStr := rawSource.(string); !isStr {
				sourceValue = toDisplayString(rawSource)
			}
		}
		dimensions[name] = map[string]any{"availability": state, "value": scoreValue, "source": sourceValue}
	}
	// Python: judge_detail = _compact_value(...) if Mapping else None
	var judgeDetail any
	if jd, isMap := m["judge_detail"].(map[string]any); isMap {
		judgeDetail = compactValue(jd)
	}
	// Python: source = _compact_value(source) if Mapping else {}
	source := map[string]any{}
	if src, isMap := m["source"].(map[string]any); isMap {
		source = compactValue(src).(map[string]any)
	}
	// Python: score = value.get("score") if _is_number else None; passed 同理
	var scoreOut any
	if isNumber(m["score"]) {
		scoreOut = m["score"]
	}
	var passedOut any
	if p, isBool := m["passed"].(bool); isBool {
		passedOut = p
	}
	return map[string]any{
		"schema_version":   1,
		"availability":     availability,
		"score":            scoreOut,
		"passed":           passedOut,
		"score_reason":     compactText(m["score_reason"], 1000),
		"judge_detail":     judgeDetail,
		"dimension_scores": dimensions,
		"source":           source,
	}
}

// compactJudgeEvidence 保留 trial 分数文件没有的聚合条款反馈。
// Python: evidence_compactor.py:302-336 _compact_judge_evidence
func compactJudgeEvidence(metadata map[string]any) map[string]any {
	// Python: normalized = metadata.get("judge_evidence")
	if normalized, isMap := metadata["judge_evidence"].(map[string]any); isMap {
		criteria := []any{}
		if c, isList := normalized["criteria"].([]any); isList {
			criteria = c
		}
		return map[string]any{
			"schema_version": 1,
			// Python: str(normalized.get("availability") or "not_available")
			"availability":       stringOr(stringField(normalized, "availability", ""), "not_available"),
			"grading_run_status": stringField(normalized, "grading_run_status", ""),
			"criteria":           compactValue(criteria),
		}
	}
	// Python: detail = metadata.get("judge_detail"); if not Mapping: not_instrumented
	detail, isMap := metadata["judge_detail"].(map[string]any)
	if !isMap {
		return map[string]any{
			"schema_version": 1, "availability": "not_instrumented",
			"grading_run_status": "", "criteria": []any{},
		}
	}
	// Python: raw_criteria = detail.get("criteria"); if not list: not_available
	rawCriteria, isList := detail["criteria"].([]any)
	if !isList {
		return map[string]any{
			"schema_version": 1, "availability": "not_available",
			"grading_run_status": stringField(detail, "grading_run_status", ""),
			"criteria":           []any{},
		}
	}
	var criteria []map[string]any
	for _, item := range rawCriteria {
		if c, isMap := item.(map[string]any); isMap {
			criteria = append(criteria, compactValue(c).(map[string]any))
		}
	}
	availability := "invalid"
	if len(criteria) > 0 {
		availability = "available"
	}
	return map[string]any{
		"schema_version": 1, "availability": availability,
		"grading_run_status": stringField(detail, "grading_run_status", ""),
		"criteria":           criteria,
	}
}

// firstPresent 返回首个显式存在的别名（包括 false 与零值）。
// keys 为别名清单；values 为依次查找的 mapping。
// Python: evidence_compactor.py:465-471 _first_present
func firstPresent(keys []string, values ...any) any {
	for _, raw := range values {
		m, isMap := raw.(map[string]any)
		if !isMap {
			continue
		}
		for _, key := range keys {
			if value, ok := m[key]; ok {
				return value
			}
		}
	}
	return nil
}

// compactCandidateFailureDiagnoses 候选失败诊断投影（前 2 条）。
// Python: evidence_compactor.py:474-493 _compact_candidate_failure_diagnoses
func compactCandidateFailureDiagnoses(diagnoses []any) []map[string]any {
	keys := []string{
		"summary", "root_cause", "target_ref", "recommendation",
		"decision_contract", "hypothesis_assessment", "prior_experiment_assessment",
	}
	var compacted []map[string]any
	for i, rawDiagnosis := range diagnoses {
		if i >= 2 {
			break
		}
		diagnosis, isMap := rawDiagnosis.(map[string]any)
		if !isMap {
			continue
		}
		item := map[string]any{}
		for _, key := range keys {
			if value, ok := diagnosis[key]; ok {
				item[key] = compactValue(value)
			}
		}
		compacted = append(compacted, item)
	}
	return compacted
}

// firstMapping 返回首个 Mapping 值。
// Python: evidence_compactor.py:496-500 _first_mapping
func firstMapping(values ...any) map[string]any {
	for _, value := range values {
		if m, isMap := value.(map[string]any); isMap {
			return m
		}
	}
	return map[string]any{}
}

// compactScoreComparison 归一化 source/candidate/delta 分数对比。
// Python: evidence_compactor.py:503-520 _compact_score_comparison
func compactScoreComparison(nested, source, candidate, delta any) map[string]any {
	nestedMap, isMap := nested.(map[string]any)
	if !isMap {
		nestedMap = map[string]any{}
	}
	// Python: nested.get("source") if "source" in nested else source（含 None 显式存在）
	sourceValue := source
	if _, ok := nestedMap["source"]; ok {
		sourceValue = nestedMap["source"]
	}
	candidateValue := candidate
	if _, ok := nestedMap["candidate"]; ok {
		candidateValue = nestedMap["candidate"]
	}
	deltaValue := delta
	if _, ok := nestedMap["delta"]; ok {
		deltaValue = nestedMap["delta"]
	}
	// Python: if not _is_number(delta) and _is_number(source) and _is_number(candidate): delta = candidate - source
	if !isNumber(deltaValue) && isNumber(sourceValue) && isNumber(candidateValue) {
		deltaValue = toFloat(candidateValue) - toFloat(sourceValue)
	}
	var sourceOut, candidateOut, deltaOut any
	if isNumber(sourceValue) {
		sourceOut = sourceValue
	}
	if isNumber(candidateValue) {
		candidateOut = candidateValue
	}
	if isNumber(deltaValue) {
		deltaOut = deltaValue
	}
	return map[string]any{"source": sourceOut, "candidate": candidateOut, "delta": deltaOut}
}

// buildTrialRecord 构建单 trial 记录。
// Python: evidence_compactor.py:523-627 _build_trial_record
func buildTrialRecord(trace map[string]any, index int, score, passed, exitReason any, criticalEvidenceTerms []string) map[string]any {
	// Python: trace_id = str(trace.get("trace_id") or f"trace_{index+1}")
	traceID := stringField(trace, "trace_id", "")
	if traceID == "" {
		traceID = fmt.Sprintf("trace_%d", index+1)
	}
	// Python: role = str(trace.get("member_role") or trace.get("role") or "")
	role := stringOr(stringField(trace, "member_role", ""), stringField(trace, "role", ""))
	messages, _ := trace["messages"].([]any)

	var calls []map[string]any
	var assistantOutputs []map[string]any
	for _, rawMessage := range messages {
		message, isMap := rawMessage.(map[string]any)
		if !isMap {
			continue
		}
		messageIndex := message["message_index"]
		if _, has := message["message_index"]; !has {
			messageIndex = ""
		}
		// Python: step_pointer = str(message.get("step_pointer") or "")
		stepPointer := stringField(message, "step_pointer", "")
		// Python: content = str(message.get("content") or "").strip()
		content := strings.TrimSpace(stringField(message, "content", ""))
		// Python: if role == "assistant" and content: assistant_outputs.append(...)
		if stringField(message, "role", "") == "assistant" && content != "" {
			assistantOutputs = append(assistantOutputs, map[string]any{
				"message_index": messageIndex, "step_pointer": stepPointer, "content": content,
			})
		}
		rawCalls, isList := message["tool_calls"].([]any)
		if !isList {
			continue
		}
		for callIndex, rawCall := range rawCalls {
			call, isMap := rawCall.(map[string]any)
			if !isMap {
				continue
			}
			rawInput := call["input"]
			if _, has := call["input"]; !has {
				rawInput = ""
			}
			rawOutput := call["output"]
			if _, has := call["output"]; !has {
				rawOutput = ""
			}
			// Python: raw_output_text = str(raw_output or "")
			rawOutputText := stringOr(toDisplayString(rawOutput), "")
			if rawOutput == nil {
				rawOutputText = ""
			}
			// Python: raw_error = str(raw_call.get("error") or "")
			rawError := stringOr(stringField(call, "error", ""), "")
			evidenceID := fmt.Sprintf("%s:message_%v:call_%d", traceID, messageIndex, callIndex)
			calls = append(calls, map[string]any{
				"evidence_id":   evidenceID,
				"trace_id":      traceID,
				"role":          role,
				"message_index": messageIndex,
				// Python: str(raw_call.get("step_pointer") or step_pointer)
				"step_pointer":      stringOr(stringField(call, "step_pointer", ""), stepPointer),
				"tool":              stringField(call, "name", ""),
				"request":           parseAndCompact(rawInput),
				"response":          parseAndCompact(rawOutput),
				"response_evidence": responseEvidenceView(rawOutputText, evidenceID, criticalEvidenceTerms),
				"error":             compactText(rawError, 1000),
				"decision_context":  compactText(content, 600),
				"_raw_input":        stringOr(toDisplayString(rawInput), ""),
				"_raw_output":       rawOutputText,
			})
		}
	}
	// Python: selected = _select_calls(calls); public_selected = [...]
	selected := selectCalls(calls)
	var publicSelected []map[string]any
	for _, call := range selected {
		publicSelected = append(publicSelected, publicCall(call))
	}
	finalOutput := map[string]any{}
	if len(assistantOutputs) > 0 {
		finalOutput = assistantOutputs[len(assistantOutputs)-1]
	}
	// Python: delivered_output, delivered_reference = _finish_delivery(calls)
	deliveredOutput, deliveredReference := finishDelivery(calls)
	deliveryChannel := "assistant_message"
	if deliveredOutput != "" {
		merged := map[string]any{"content": deliveredOutput}
		for key, value := range deliveredReference {
			merged[key] = value
		}
		finalOutput = merged
		deliveryChannel = "finish_tool_request"
	}
	// Python: sequence = [str(call.get("tool") or "") for call in calls]
	sequence := make([]string, 0, len(calls))
	for _, call := range calls {
		sequence = append(sequence, stringOr(stringField(call, "tool", ""), ""))
	}
	sequenceTruncated := len(sequence) > maxToolSequence
	if len(sequence) > maxToolSequence {
		sequence = sequence[:maxToolSequence]
	}

	// Python: selection_coverage 统计
	coverage := map[string]any{
		"policy":                               "decision_linked_dynamic_budget",
		"selected_count":                       len(selected),
		"omitted_count":                        len(calls) - len(selected),
		"failed_call_count":                    countWhere(calls, callFailed),
		"selected_failed_call_count":           countWhere(selected, callFailed),
		"state_mutation_call_count":            countWhere(calls, callMutatesState),
		"selected_state_mutation_call_count":   countWhere(selected, callMutatesState),
		"content_evidence_call_count":          countWhere(calls, callHasContentEvidence),
		"selected_content_evidence_call_count": countWhere(selected, callHasContentEvidence),
	}
	if omitted := len(calls) - len(selected); omitted < 0 {
		coverage["omitted_count"] = 0
	}

	// Python: final_output = {**_output_summary(...), delivery_channel, evidence_ref}
	finalOutputSummary := outputSummary(stringOr(toDisplayString(finalOutput["content"]), ""))
	finalOutputBlock := map[string]any{}
	for key, value := range finalOutputSummary {
		finalOutputBlock[key] = value
	}
	finalOutputBlock["delivery_channel"] = deliveryChannel
	finalOutputBlock["evidence_ref"] = map[string]any{
		"trace_id":      traceID,
		"role":          role,
		"message_index": valueOr(finalOutput, "message_index", ""),
		"step_pointer":  valueOr(finalOutput, "step_pointer", ""),
	}

	return map[string]any{
		"trial_id": traceID,
		"role":     role,
		// Python: passed if isinstance(passed, bool) else None
		"passed": boolOrNone(passed),
		// Python: score if _is_number(score) else None
		"score":                   numberOrNone(score),
		"exit_reason":             stringOr(toDisplayString(exitReason), ""),
		"tool_call_count":         len(calls),
		"tool_sequence":           sequence,
		"tool_sequence_truncated": sequenceTruncated,
		"selected_actions":        publicSelected,
		"selection_coverage":      coverage,
		"final_output":            finalOutputBlock,
		"_raw_message_count":      len(messages),
		"_all_calls":              calls,
	}
}

// finishDelivery 从 finish 工具调用提取最终交付答案。
// Python: evidence_compactor.py:630-645 _finish_delivery
func finishDelivery(calls []map[string]any) (string, map[string]any) {
	// Python: for call in reversed(calls)
	for i := len(calls) - 1; i >= 0; i-- {
		call := calls[i]
		// Python: if str(call.get("tool") or "").lower() != "finish": continue
		if strings.ToLower(stringOr(stringField(call, "tool", ""), "")) != "finish" {
			continue
		}
		rawInput := stringOr(stringField(call, "_raw_input", ""), "")
		var payload map[string]any
		if json.Unmarshal([]byte(rawInput), &payload) != nil {
			continue
		}
		// Python: answer = payload.get("answer") if isinstance(payload, Mapping) else None
		answer, isStr := payload["answer"].(string)
		if isStr && strings.TrimSpace(answer) != "" {
			return answer, map[string]any{
				"message_index": valueOr(call, "message_index", ""),
				"step_pointer":  valueOr(call, "step_pointer", ""),
			}
		}
	}
	return "", map[string]any{}
}

// selectCalls 动态预算 + 六理由打分挑选。
// Python: evidence_compactor.py:648-700 _select_calls
func selectCalls(calls []map[string]any) []map[string]any {
	// Python: budget = min(_MAX, max(_MIN, (len(calls)*2+2)//3))
	n := len(calls)
	budget := (n*2 + 2) / 3
	if budget < minSelectedCallsPerTrial {
		budget = minSelectedCallsPerTrial
	}
	if budget > maxSelectedCallsPerTrial {
		budget = maxSelectedCallsPerTrial
	}
	// Python: if len(calls) <= budget: 全保留 complete_trace
	if n <= budget {
		for _, call := range calls {
			call["selection_reasons"] = []string{"complete_trace"}
		}
		return calls
	}

	// Python: first_by_tool / last_by_tool
	firstByTool := map[string]int{}
	lastByTool := map[string]int{}
	for index, call := range calls {
		tool := stringOr(stringField(call, "tool", ""), "")
		if _, exists := firstByTool[tool]; !exists {
			firstByTool[tool] = index
		}
		lastByTool[tool] = index
	}
	firstIndexes := map[int]bool{}
	for _, index := range firstByTool {
		firstIndexes[index] = true
	}
	lastIndexes := map[int]bool{}
	for _, index := range lastByTool {
		lastIndexes[index] = true
	}

	// Python: _reasons(index)（保留理由清单）
	reasons := func(index int) []string {
		call := calls[index]
		var list []string
		if callFailed(call) {
			list = append(list, "observed_failure")
		}
		if callMutatesState(call) {
			list = append(list, "state_mutation")
		}
		if callHasContentEvidence(call) {
			list = append(list, "content_evidence")
		}
		if firstIndexes[index] {
			list = append(list, "first_use_of_tool")
		}
		if lastIndexes[index] {
			list = append(list, "last_use_of_tool")
		}
		if index >= n-3 {
			list = append(list, "terminal_window")
		}
		return list
	}

	// Python: ranked = sorted(range(len(calls)), key=lambda index: (六键升序倒排))
	type rankedIndex struct {
		index    int
		failed   bool
		mutates  bool
		content  bool
		boundary bool // 首末次（true=是边界）
		terminal bool // 非末 3 条（true=非末窗）
		rawLen   int
	}
	ranked := make([]rankedIndex, 0, n)
	for index := 0; index < n; index++ {
		call := calls[index]
		rawOutput := stringOr(stringField(call, "_raw_output", ""), stringField(call, "response", ""))
		ranked = append(ranked, rankedIndex{
			index:    index,
			failed:   callFailed(call),
			mutates:  callMutatesState(call),
			content:  callHasContentEvidence(call),
			boundary: firstIndexes[index] || lastIndexes[index],
			terminal: index < n-3,
			rawLen:   len(rawOutput),
		})
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		a, b := ranked[i], ranked[j]
		// Python 键序：not failed, not mutates, not content, not boundary, is_non_terminal, -len(raw), index
		if a.failed != b.failed {
			return a.failed // false 排前（not failed 更小）
		}
		if a.mutates != b.mutates {
			return a.mutates
		}
		if a.content != b.content {
			return a.content
		}
		if a.boundary != b.boundary {
			return a.boundary // 边界（false）排前
		}
		if a.terminal != b.terminal {
			return !a.terminal // 末窗（terminal=false）排前
		}
		if a.rawLen != b.rawLen {
			return a.rawLen > b.rawLen // 长输出优先
		}
		return a.index < b.index
	})

	selectedIndexes := map[int]bool{}
	for i := 0; i < budget && i < len(ranked); i++ {
		selectedIndexes[ranked[i].index] = true
	}
	for index := range selectedIndexes {
		reasonList := reasons(index)
		if len(reasonList) == 0 {
			reasonList = []string{"dynamic_budget"}
		}
		calls[index]["selection_reasons"] = reasonList
	}
	// Python: return [calls[index] for index in sorted(selected_indexes)]（按原序输出）
	indexes := make([]int, 0, len(selectedIndexes))
	for index := range selectedIndexes {
		indexes = append(indexes, index)
	}
	sort.Ints(indexes)
	result := make([]map[string]any, 0, len(indexes))
	for _, index := range indexes {
		result = append(result, calls[index])
	}
	return result
}

// publicCall 公开投影：剔除私有键与空值、非失败非变异剔除 decision_context。
// Python: evidence_compactor.py:703-711 _public_call
func publicCall(call map[string]any) map[string]any {
	public := map[string]any{}
	for key, value := range call {
		if strings.HasPrefix(key, "_") {
			continue
		}
		// Python: value not in ("", {}, []) or key in {evidence_id, tool, request}
		if isEqualEmpty(value) && key != "evidence_id" && key != "tool" && key != "request" {
			continue
		}
		public[key] = value
	}
	// Python: if not failed and not mutates: public.pop("decision_context", None)
	if !callFailed(call) && !callMutatesState(call) {
		delete(public, "decision_context")
	}
	return public
}

// deduplicateSelectedPayloads 跨 trial 选中载荷去重（原地标记 duplicate_of）。
// Python: evidence_compactor.py:714-734 _deduplicate_selected_payloads
func deduplicateSelectedPayloads(trials []map[string]any) {
	seen := map[string]string{}
	for _, trial := range trials {
		actions, ok := trial["selected_actions"].([]map[string]any)
		if !ok {
			continue
		}
		for _, call := range actions {
			// Python: payload = {tool, request, response, error}
			payload := map[string]any{
				"tool":     call["tool"],
				"request":  call["request"],
				"response": call["response"],
				"error":    call["error"],
			}
			// Python: sha256(json.dumps(payload, ensure_ascii=False, sort_keys=True, default=str))
			canonical := canonicalJSON(payload)
			original, exists := seen[canonical]
			if exists && original != "" {
				call["duplicate_of"] = original
				delete(call, "request")
				delete(call, "response")
				delete(call, "decision_context")
			} else {
				// Python: seen[fingerprint] = str(call.get("evidence_id") or "")
				seen[canonical] = stringOr(stringField(call, "evidence_id", ""), "")
			}
		}
	}
}

// toolContractObservations 工具契约观察（schema 与实际调用的字段对照）。
// Python: evidence_compactor.py:737-793 _tool_contract_observations
func toolContractObservations(taskContract map[string]any, calls []map[string]any) []map[string]any {
	schemas := map[string]map[string]any{}
	if rawSchemas, isList := taskContract["tool_schemas"].([]any); isList {
		for _, rawSchema := range rawSchemas {
			schema, isMap := rawSchema.(map[string]any)
			if !isMap {
				continue
			}
			name := stringField(schema, "name", "")
			if name != "" {
				schemas[name] = schema
			}
		}
	}

	// Python: defaultdict(set) 三组 + Counter
	observedRequestFields := map[string]map[string]bool{}
	observedResponseFields := map[string]map[string]bool{}
	observedResponseLeafFields := map[string]map[string]bool{}
	callCounts := map[string]int{}

	for _, call := range calls {
		tool := stringOr(stringField(call, "tool", ""), "")
		if tool == "" {
			continue
		}
		callCounts[tool]++
		request := call["request"]
		response := call["response"]
		if reqMap, isMap := request.(map[string]any); isMap {
			if observedRequestFields[tool] == nil {
				observedRequestFields[tool] = map[string]bool{}
			}
			for key := range reqMap {
				observedRequestFields[tool][key] = true
			}
		}
		if respMap, isMap := response.(map[string]any); isMap {
			if observedResponseFields[tool] == nil {
				observedResponseFields[tool] = map[string]bool{}
			}
			if observedResponseLeafFields[tool] == nil {
				observedResponseLeafFields[tool] = map[string]bool{}
			}
			for key := range respMap {
				observedResponseFields[tool][key] = true
			}
			for leaf := range leafFieldNames(respMap, 0) {
				observedResponseLeafFields[tool][leaf] = true
			}
		}
	}

	// Python: for tool in sorted(set(call_counts) | set(schemas))
	toolSet := map[string]bool{}
	for tool := range callCounts {
		toolSet[tool] = true
	}
	for tool := range schemas {
		toolSet[tool] = true
	}
	toolNames := make([]string, 0, len(toolSet))
	for tool := range toolSet {
		toolNames = append(toolNames, tool)
	}
	sort.Strings(toolNames)

	var observations []map[string]any
	for _, tool := range toolNames {
		schema := schemas[tool]
		if schema == nil {
			schema = map[string]any{}
		}
		// Python: allowed = set(map(str, schema.get("allowed_request_fields", [])))
		allowed := stringSetFromList(schema["allowed_request_fields"])
		required := stringSetFromList(schema["required_request_fields"])
		requestFields := observedRequestFields[tool]
		responseFields := observedResponseFields[tool]
		responseLeafFields := observedResponseLeafFields[tool]

		observation := map[string]any{
			"tool":                          tool,
			"call_count":                    callCounts[tool],
			"public_schema_available":       len(schema) > 0,
			"description":                   valueOr(schema, "description", ""),
			"allowed_request_fields":        sortedKeys(allowed),
			"required_request_fields":       sortedKeys(required),
			"request_field_contracts":       valueOr(schema, "request_field_contracts", map[string]any{}),
			"observed_request_fields":       sortedKeySet(requestFields),
			"observed_response_fields":      sortedKeySet(responseFields),
			"observed_response_leaf_fields": sortedKeySet(responseLeafFields),
			// Python: response_fields - request_fields
			"response_only_fields": sortedKeySet(setDifference(responseFields, requestFields)),
		}
		// Python: if schema: 三个额外差集
		if len(schema) > 0 {
			observation["response_fields_not_in_public_request_schema"] = sortedKeySet(setDifference(responseFields, allowed))
			observation["response_leaf_fields_not_in_public_request_schema"] = sortedKeySet(setDifference(responseLeafFields, allowed))
			observation["observed_request_fields_outside_public_schema"] = sortedKeySet(setDifference(requestFields, allowed))
		}
		observations = append(observations, observation)
	}
	return observations
}

// crossTrialContrast 跨 trial 对比。
// Python: evidence_compactor.py:796-832 _cross_trial_contrast
func crossTrialContrast(trials []map[string]any) map[string]any {
	if len(trials) == 0 {
		return map[string]any{"available": false, "reason": "no_normalized_traces"}
	}
	sequences := make([][]string, 0, len(trials))
	for _, trial := range trials {
		seq := []string{}
		switch raw := trial["tool_sequence"].(type) {
		case []string:
			seq = raw
		case []any:
			for _, item := range raw {
				seq = append(seq, toDisplayString(item))
			}
		}
		sequences = append(sequences, seq)
	}
	toolSets := make([]map[string]bool, 0, len(sequences))
	for _, seq := range sequences {
		set := map[string]bool{}
		for _, tool := range seq {
			set[tool] = true
		}
		toolSets = append(toolSets, set)
	}
	var passedIDs, failedIDs []string
	for _, trial := range trials {
		// Python: trial.get("passed") is True / is False（严格 bool 身份）
		if b, isBool := trial["passed"].(bool); isBool && b {
			passedIDs = append(passedIDs, stringField(trial, "trial_id", ""))
		}
		if b, isBool := trial["passed"].(bool); isBool && !b {
			failedIDs = append(failedIDs, stringField(trial, "trial_id", ""))
		}
	}
	successTools := map[string]bool{}
	failureTools := map[string]bool{}
	for index, trial := range trials {
		if b, isBool := trial["passed"].(bool); isBool && b {
			for tool := range toolSets[index] {
				successTools[tool] = true
			}
		}
		if b, isBool := trial["passed"].(bool); isBool && !b {
			for tool := range toolSets[index] {
				failureTools[tool] = true
			}
		}
	}
	// Python: stable_tools = set.intersection(*tool_sets) if tool_sets else set()
	stableTools := map[string]bool{}
	if len(toolSets) > 0 {
		first := true
		for _, set := range toolSets {
			if first {
				for tool := range set {
					stableTools[tool] = true
				}
				first = false
				continue
			}
			for tool := range stableTools {
				if !set[tool] {
					delete(stableTools, tool)
				}
			}
		}
	}
	// Python: success_only / failure_only 仅当 passed_ids and failed_ids
	successOnly := []string{}
	failureOnly := []string{}
	if len(passedIDs) > 0 && len(failedIDs) > 0 {
		successOnly = sortedKeySet(setDifference(successTools, failureTools))
		failureOnly = sortedKeySet(setDifference(failureTools, successTools))
	}

	// Python: final_output_comparison
	finalOutputComparison := make([]map[string]any, 0, len(trials))
	for _, trial := range trials {
		finalOutput := mapField(trial, "final_output")
		finalOutputComparison = append(finalOutputComparison, map[string]any{
			"trial_id":          stringField(trial, "trial_id", ""),
			"passed":            trial["passed"],
			"score":             trial["score"],
			"character_count":   valueOr(finalOutput, "character_count", 0),
			"line_count":        valueOr(finalOutput, "line_count", 0),
			"delivery_channel":  stringField(finalOutput, "delivery_channel", ""),
			"artifact_mentions": valueOr(finalOutput, "artifact_mentions", []any{}),
			"evidence_ref":      mapField(finalOutput, "evidence_ref"),
		})
	}

	return map[string]any{
		"available":                      len(trials) > 1,
		"successful_trials":              passedIDs,
		"failed_trials":                  failedIDs,
		"stable_tools":                   sortedKeySet(stableTools),
		"success_only_tools":             successOnly,
		"failure_only_tools":             failureOnly,
		"first_tool_sequence_divergence": firstSequenceDivergence(trials, sequences),
		"terminal_action_variants":       terminalActionVariants(trials),
		"final_output_comparison":        finalOutputComparison,
	}
}

// firstSequenceDivergence 定位工具序列首次分歧步。
// Python: evidence_compactor.py:835-846 _first_sequence_divergence
func firstSequenceDivergence(trials []map[string]any, sequences [][]string) map[string]any {
	if len(sequences) < 2 {
		return map[string]any{}
	}
	maxLength := 0
	for _, seq := range sequences {
		if len(seq) > maxLength {
			maxLength = len(seq)
		}
	}
	for index := 0; index < maxLength; index++ {
		var values []string
		for _, seq := range sequences {
			if index < len(seq) {
				values = append(values, seq[index])
			} else {
				values = append(values, "<end>")
			}
		}
		if len(dedupeStrings(values)) > 1 {
			byTrial := map[string]any{}
			for i, trial := range trials {
				byTrial[stringField(trial, "trial_id", "")] = values[i]
			}
			return map[string]any{"tool_index": index, "by_trial": byTrial}
		}
	}
	return map[string]any{}
}

// terminalActionVariants 终结动作变体（按变异工具分组）。
// Python: evidence_compactor.py:849-865 _terminal_action_variants
func terminalActionVariants(trials []map[string]any) []map[string]any {
	byTool := map[string][]map[string]any{}
	for _, trial := range trials {
		actions, ok := trial["selected_actions"].([]map[string]any)
		if !ok {
			continue
		}
		for _, action := range actions {
			tool := stringOr(stringField(action, "tool", ""), "")
			if isMutatingTool(tool) {
				byTool[tool] = append(byTool[tool], map[string]any{
					"trial_id":    stringField(trial, "trial_id", ""),
					"passed":      trial["passed"],
					"score":       trial["score"],
					"request":     action["request"],
					"evidence_id": action["evidence_id"],
				})
			}
		}
	}
	tools := make([]string, 0, len(byTool))
	for tool := range byTool {
		tools = append(tools, tool)
	}
	sort.Strings(tools)
	result := make([]map[string]any, 0, len(tools))
	for _, tool := range tools {
		result = append(result, map[string]any{"tool": tool, "variants": byTool[tool]})
	}
	return result
}

// normalizeToolSchema 归一化工具 schema。
// Python: evidence_compactor.py:868-891 _normalize_tool_schema
func normalizeToolSchema(schema map[string]any) map[string]any {
	// Python: function = schema.get("function") if Mapping else schema
	function := schema
	if fn, isMap := schema["function"].(map[string]any); isMap {
		function = fn
	}
	name := stringField(function, "name", "")
	if name == "" {
		return map[string]any{}
	}
	parameters := mapField(function, "parameters")
	properties := mapField(parameters, "properties")
	allowed := make([]string, 0, len(properties))
	for field := range properties {
		allowed = append(allowed, field)
	}
	sort.Strings(allowed)
	required := []string{}
	if req, isList := parameters["required"].([]any); isList {
		for _, item := range req {
			required = append(required, toDisplayString(item))
		}
	}
	sort.Strings(required)
	contracts := map[string]any{}
	for field, rawSpec := range properties {
		spec, isMap := rawSpec.(map[string]any)
		if !isMap {
			contracts[field] = map[string]any{}
			continue
		}
		entry := map[string]any{}
		for _, key := range []string{"type", "description", "enum", "items"} {
			if value, ok := spec[key]; ok {
				entry[key] = compactValue(value)
			}
		}
		contracts[field] = entry
	}
	return map[string]any{
		"name":                    name,
		"description":             compactText(valueOr(function, "description", ""), 500),
		"allowed_request_fields":  allowed,
		"required_request_fields": required,
		"request_field_contracts": contracts,
	}
}

// judgeDimensions judge 详情中的标量字段投影。
// Python: evidence_compactor.py:894-898 _judge_dimensions
func judgeDimensions(metadata map[string]any) map[string]any {
	detail, isMap := metadata["judge_detail"].(map[string]any)
	if !isMap {
		return map[string]any{}
	}
	out := map[string]any{}
	for key, value := range detail {
		if isScalar(value) {
			out[key] = value
		}
	}
	return out
}

// leafFieldNames 响应叶字段名集合（深度限 3）。
// Python: evidence_compactor.py:901-909 _leaf_field_names
func leafFieldNames(value map[string]any, depth int) map[string]bool {
	leaves := map[string]bool{}
	for key, item := range value {
		if m, isMap := item.(map[string]any); isMap && depth < 3 {
			for leaf := range leafFieldNames(m, depth+1) {
				leaves[leaf] = true
			}
		} else {
			leaves[key] = true
		}
	}
	return leaves
}

// outputSummary 输出摘要（字符/行数/产物提及/摘录）。
// Python: evidence_compactor.py:912-924 _output_summary
func outputSummary(value string) map[string]any {
	text := value
	var artifacts []string
	for _, match := range artifactPattern.FindAllStringSubmatch(text, -1) {
		candidate := match[1]
		if !containsStr(artifacts, candidate) {
			artifacts = append(artifacts, candidate)
		}
	}
	if len(artifacts) > 12 {
		artifacts = artifacts[:12]
	}
	return map[string]any{
		"character_count":   len(text),
		"line_count":        len(splitLinesPlain(text)),
		"artifact_mentions": artifacts,
		"excerpt":           headTail(text, 1600),
	}
}

// failedRequirementTerms 从失败评估条款提取有界锚点词（≤48）。
// Python: evidence_compactor.py:927-960 _failed_requirement_terms
func failedRequirementTerms(metadata map[string]any) []string {
	// Python: normalized = metadata.get("judge_evidence"); detail = normalized if Mapping else metadata.get("judge_detail")
	detail := map[string]any{}
	if normalized, isMap := metadata["judge_evidence"].(map[string]any); isMap {
		detail = normalized
	} else if jd, isMap := metadata["judge_detail"].(map[string]any); isMap {
		detail = jd
	}
	criteria, _ := detail["criteria"].([]any)
	var values []string
	for _, rawCriterion := range criteria {
		criterion, isMap := rawCriterion.(map[string]any)
		if !isMap {
			continue
		}
		// Python: if _is_number(score) and float(score) > 0: continue
		if isNumber(criterion["score"]) && toFloat(criterion["score"]) > 0 {
			continue
		}
		for _, key := range []string{"criterion_id", "verifier_id", "rationale"} {
			if value, ok := criterion[key]; ok && value != nil {
				if str, isStr := value.(string); isStr && str != "" {
					values = append(values, str)
				} else if _, isStr := value.(string); !isStr {
					values = append(values, toDisplayString(value))
				}
			}
		}
	}

	var terms []string
	seen := map[string]bool{}
	for _, value := range values {
		for _, match := range criticalEvidenceTermPattern.FindAllString(value, -1) {
			normalizedTerm := strings.ToLower(match)
			if criticalEvidenceStopwords[normalizedTerm] || seen[normalizedTerm] {
				continue
			}
			seen[normalizedTerm] = true
			terms = append(terms, match)
			// Python: if len(terms) >= 48: return terms
			if len(terms) >= 48 {
				return terms
			}
		}
	}
	return terms
}

// readableEvidenceText 投影序列化换行而不做语义摘要。
// Python: evidence_compactor.py:1028-1035 _readable_evidence_text
func readableEvidenceText(text string) (string, string) {
	if !strings.Contains(text, "\n") && strings.Contains(text, "\\n") {
		out := strings.ReplaceAll(text, "\\r\\n", "\n")
		out = strings.ReplaceAll(out, "\\n", "\n")
		out = strings.ReplaceAll(out, "\\t", "\t")
		return out, "escaped_newlines_normalized"
	}
	return text, "verbatim"
}

// boundedCriticalSpan 有界关键窗口（超限时围绕最早命中位置居中）。
// Python: evidence_compactor.py:1038-1048 _bounded_critical_span
func boundedCriticalSpan(text string, matchedTerms map[string]bool, limit int) (string, bool) {
	if len(text) <= limit {
		return text, true
	}
	lowered := strings.ToLower(text)
	positions := []int{}
	for term := range matchedTerms {
		if idx := strings.Index(lowered, term); idx >= 0 {
			positions = append(positions, idx)
		}
	}
	center := len(text) / 2
	if len(positions) > 0 {
		center = positions[0]
		for _, p := range positions[1:] {
			if p < center {
				center = p
			}
		}
	}
	headroom := limit / 3
	if headroom < 0 {
		headroom = 0
	}
	start := center - headroom
	if start < 0 {
		start = 0
	}
	end := start + limit
	if end > len(text) {
		end = len(text)
	}
	start = end - limit
	if start < 0 {
		start = 0
	}
	return text[start:end], false
}

// responseEvidenceView 响应证据视图。
// Python: evidence_compactor.py:1051-1066 _response_evidence_view
func responseEvidenceView(rawOutput, evidenceID string, criticalTerms []string) map[string]any {
	sourceChars := len(rawOutput)
	displayComplete := sourceChars <= 1200
	omissionOrigin := "none"
	if !displayComplete {
		omissionOrigin = "analyzer_evidence_compactor"
	}
	return map[string]any{
		"raw_evidence_ref":         evidenceID,
		"source":                   "public_normalized_execution_trace.raw_tool_response",
		"source_char_count":        sourceChars,
		"display_excerpt_complete": displayComplete,
		"display_omission_origin":  omissionOrigin,
		"task_agent_observed_display_omission_marker": false,
		"critical_spans": ExtractCriticalEvidenceSpans(rawOutput, criticalTerms, 3, 6000),
	}
}

// parseAndCompact 解析 JSON 字符串后压缩；非 JSON 走头尾摘录。
// Python: evidence_compactor.py:1069-1079 _parse_and_compact
func parseAndCompact(value any) any {
	if str, isStr := value.(string); isStr {
		text := strings.TrimSpace(str)
		if text == "" {
			return ""
		}
		var parsed any
		if json.Unmarshal([]byte(text), &parsed) != nil {
			return headTail(text, 2000)
		}
		return compactValue(parsed)
	}
	return compactValue(value)
}

// compactValue 深度限 5 的有界压缩。
// Python: evidence_compactor.py:1082-1101 _compact_value
func compactValue(value any) any {
	return compactValueDepth(value, 0)
}

func compactValueDepth(value any, depth int) any {
	// Python: if depth >= 5: return _compact_text(value, 500)
	if depth >= 5 {
		return compactText(value, 500)
	}
	if m, isMap := value.(map[string]any); isMap {
		keys := make([]string, 0, len(m))
		for key := range m {
			keys = append(keys, key)
		}
		// Python: items[:30] 按 dict 插入序；Go map 无序——按键排序近似（偏差 D-6 消费面）
		sort.Strings(keys)
		compact := map[string]any{}
		limit := len(keys)
		if limit > 30 {
			limit = 30
		}
		for _, key := range keys[:limit] {
			compact[key] = compactValueDepth(m[key], depth+1)
		}
		if len(keys) > 30 {
			compact["_omitted_field_count"] = len(keys) - 30
		}
		return compact
	}
	if list, isList := value.([]any); isList {
		limit := len(list)
		if limit > 30 {
			limit = 30
		}
		compactItems := make([]any, 0, limit+1)
		for _, item := range list[:limit] {
			compactItems = append(compactItems, compactValueDepth(item, depth+1))
		}
		if len(list) > 30 {
			compactItems = append(compactItems, map[string]any{"_omitted_item_count": len(list) - 30})
		}
		return compactItems
	}
	if str, isStr := value.(string); isStr {
		return headTail(str, 1200)
	}
	if isScalar(value) {
		return value
	}
	return compactText(value, 500)
}

// alignedList 长度对齐校验（不齐得空清单）。
// Python: evidence_compactor.py:1104-1105 _aligned_list
func alignedList(value any, expectedLength int) []any {
	if list, isList := value.([]any); isList && len(list) == expectedLength {
		return list
	}
	return nil
}

// isMutatingTool 变异类工具名判断。
// Python: evidence_compactor.py:1108-1109 _is_mutating_tool
func isMutatingTool(tool string) bool {
	return mutatingToolPattern.MatchString(tool)
}

// callFailed 调用失败判断（error 非空或响应含失败特征）。
// Python: evidence_compactor.py:1112-1115 _call_failed
func callFailed(call map[string]any) bool {
	if strings.TrimSpace(stringOr(stringField(call, "error", ""), "")) != "" {
		return true
	}
	// Python: str(call.get("_raw_output") or call.get("response") or "")
	rawOutput := stringOr(stringField(call, "_raw_output", ""), stringField(call, "response", ""))
	return failedResponsePattern.MatchString(rawOutput)
}

// callMutatesState 状态变异判断（工具名或 bash 命令模式）。
// Python: evidence_compactor.py:1118-1124 _call_mutates_state
func callMutatesState(call map[string]any) bool {
	tool := stringOr(stringField(call, "tool", ""), "")
	if isMutatingTool(tool) {
		return true
	}
	// Python: if tool.lower() not in {bash, shell, exec, exec_command}: return False
	lower := strings.ToLower(tool)
	if lower != "bash" && lower != "shell" && lower != "exec" && lower != "exec_command" {
		return false
	}
	return bashMutationPattern.MatchString(callCommand(call))
}

// callHasContentEvidence 内容证据判断。
// Python: evidence_compactor.py:1127-1136 _call_has_content_evidence
func callHasContentEvidence(call map[string]any) bool {
	if callFailed(call) {
		return false
	}
	tool := strings.ToLower(stringOr(stringField(call, "tool", ""), ""))
	rawOutput := stringOr(stringField(call, "_raw_output", ""), stringField(call, "response", ""))
	// Python: if len(raw_output) < 300: return False
	if len(rawOutput) < 300 {
		return false
	}
	// Python: if tool.startswith("read") or tool in {open_file, view_file}: return True
	if strings.HasPrefix(tool, "read") || tool == "open_file" || tool == "view_file" {
		return true
	}
	return (tool == "bash" || tool == "shell" || tool == "exec" || tool == "exec_command") &&
		bashContentPattern.MatchString(callCommand(call))
}

// callCommand 提取调用命令（request/command|cmd 或 JSON 解码）。
// Python: evidence_compactor.py:1139-1151 _call_command
func callCommand(call map[string]any) string {
	rawInput := stringOr(stringField(call, "_raw_input", ""), "")
	if rawInput == "" {
		if req, ok := call["request"]; ok {
			if str, isStr := req.(string); isStr {
				rawInput = str
			} else if m, isMap := req.(map[string]any); isMap {
				return stringOr(stringField(m, "command", ""), stringField(m, "cmd", ""))
			}
		}
	}
	if rawInput == "" {
		return ""
	}
	// 注意 Python 分支顺序：先 Mapping（request 是 dict 直接取 command/cmd）
	if m, isMap := valueOr(call, "_raw_input", nil).(map[string]any); isMap {
		return stringOr(stringField(m, "command", ""), stringField(m, "cmd", ""))
	}
	var parsed any
	if json.Unmarshal([]byte(rawInput), &parsed) != nil {
		return rawInput
	}
	if m, isMap := parsed.(map[string]any); isMap {
		return stringOr(stringField(m, "command", ""), stringField(m, "cmd", ""))
	}
	return rawInput
}

// isScalar 标量判断（None/str/int/float/bool）。
// Python: evidence_compactor.py:1154-1155 _is_scalar
func isScalar(value any) bool {
	switch value.(type) {
	case nil, string, bool, int, int64, float64, json.Number:
		return true
	}
	return false
}

// isNumber 数值判断（排除 bool）。
// Python: evidence_compactor.py:1158-1159 _is_number
func isNumber(value any) bool {
	switch v := value.(type) {
	case float64, int, int64:
		return true
	case json.Number:
		_, err := v.Float64()
		return err == nil
	}
	return false
}

// toFloat 数值转 float64。
func toFloat(value any) float64 {
	switch v := value.(type) {
	case float64:
		return v
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case json.Number:
		f, _ := v.Float64()
		return f
	}
	return 0
}

// compactText 头尾压缩为字符串。
// Python: evidence_compactor.py:1162-1163 _compact_text
func compactText(value any, limit int) string {
	if value == nil {
		return headTail("", limit)
	}
	return headTail(stringOr(toDisplayString(value), ""), limit)
}

// headTail 头 70% 尾 30% + 省略标记。
// Python: evidence_compactor.py:1166-1173 _head_tail
func headTail(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	head := int(float64(limit) * 0.7)
	if head < 1 {
		head = 1
	}
	tail := limit - head
	if tail < 1 {
		tail = 1
	}
	omitted := len(text) - head - tail
	marker := compactionMarkerFn(omitted)
	return text[:head] + "\n" + marker + "\n" + text[len(text)-tail:]
}

// readMapping JSON mapping 安全读取。
// Python: evidence_compactor.py:1176-1183 _read_mapping
func readMapping(path string) map[string]any {
	if !isFile(path) {
		return map[string]any{}
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return map[string]any{}
	}
	var value any
	if json.Unmarshal(content, &value) != nil {
		return map[string]any{}
	}
	if m, isMap := value.(map[string]any); isMap {
		return m
	}
	return map[string]any{}
}

// ──────────────────────────── 通用助手 ────────────────────────────

// isEqualEmpty 等价 Python value in ("", {}, [])。
func isEqualEmpty(value any) bool {
	switch v := value.(type) {
	case string:
		return v == ""
	case map[string]any:
		return len(v) == 0
	case []any:
		return len(v) == 0
	}
	return false
}

// boolOrNone bool 身份保留（Python isinstance(passed, bool)）。
func boolOrNone(value any) any {
	if b, isBool := value.(bool); isBool {
		return b
	}
	return nil
}

// numberOrNone 数值身份保留（Python _is_number）。
func numberOrNone(value any) any {
	if isNumber(value) {
		return value
	}
	return nil
}

// stringOr 首个非空字符串（Python x or y）。
func stringOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// countWhere 清单条件计数。
func countWhere(calls []map[string]any, predicate func(map[string]any) bool) int {
	count := 0
	for _, call := range calls {
		if predicate(call) {
			count++
		}
	}
	return count
}

// canonicalJSON 排序键的稳定 JSON（Python sort_keys=True）。
func canonicalJSON(value any) string {
	out, _ := json.Marshal(sortJSONValue(value))
	return string(out)
}

// sortJSONValue 递归对 map 键排序（encoding/json 已排序 map 键，list 保序透传）。
func sortJSONValue(value any) any {
	switch v := value.(type) {
	case map[string]any:
		return v
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = sortJSONValue(item)
		}
		return out
	default:
		return value
	}
}

// splitLinesPlain 按行界切分（不含行尾符，等价 Python str.splitlines()）。
func splitLinesPlain(text string) []string {
	if text == "" {
		return []string{}
	}
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	return lines
}

// stringSetFromList 字符串集合（Python set(map(str, list))）。
func stringSetFromList(value any) map[string]bool {
	set := map[string]bool{}
	if list, isList := value.([]any); isList {
		for _, item := range list {
			set[toDisplayString(item)] = true
		}
	}
	return set
}

// sortedKeys 排序键清单。
func sortedKeys(set map[string]bool) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// sortedKeySet 键集合排序。
func sortedKeySet(set map[string]bool) []string {
	return sortedKeys(set)
}

// setDifference 差集 a - b。
func setDifference(a, b map[string]bool) map[string]bool {
	out := map[string]bool{}
	for key := range a {
		if !b[key] {
			out[key] = true
		}
	}
	return out
}

// dedupeStrings 去重字符串清单。
func dedupeStrings(items []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, item := range items {
		if !seen[item] {
			seen[item] = true
			out = append(out, item)
		}
	}
	return out
}

// filepathDepthParents 父目录计数（Python len(result.parents)）。
func filepathDepthParents(path string) int {
	clean := filepath.Clean(path)
	depth := strings.Count(clean, string(filepath.Separator))
	if !filepath.IsAbs(clean) {
		depth++
	}
	return depth
}
