// signal_extractor.go 提供按 judger 方法键控的零 LLM 确定性信号提取。
// Python: openjiuwen/rsi/harness_rsi/evaluation_result_analyzer/signal_extractor.py（全量对齐）。

package evaluationresultanalyzer

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
)

// ──────────────────────────── 常量 ────────────────────────────

// 指纹归一化正则（顺序敏感：ts → uuid → path → hex → line，见 spec §3.6）。
// Python: signal_extractor.py:24-28
var (
	tsPattern   = regexp.MustCompile(`\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})?`)
	uuidPattern = regexp.MustCompile(`\b[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}\b`)
	pathPattern = regexp.MustCompile(`[A-Za-z]:[/\\][^\s,;'\"\]]+|/[/\w.\-]+`)
	hexPattern  = regexp.MustCompile(`\b[0-9a-fA-F]{6,}\b`)
	linePattern = regexp.MustCompile(`:\d+`)
	maxIterMark = "Max iterations reached without completion"
)

// ──────────────────────────── 结构体 ────────────────────────────

// GenericSignalExtractor default/exact_match 的确定性提取器。
// Python: signal_extractor.py:33 GenericSignalExtractor（name="generic"）。
type GenericSignalExtractor struct{}

// PytestSignalExtractor script_based（pytest）judger 方法的提取器。
// Python: signal_extractor.py:114 PytestSignalExtractor（name="pytest"）。
type PytestSignalExtractor struct{}

// RewardSignalExtractor 带轨迹证据的 reward 型评估器提取器。
// Python: signal_extractor.py:188 RewardSignalExtractor（name="reward"）。
type RewardSignalExtractor struct{}

// AtomicChecksSignalExtractor 原子校验失败与轨迹归因提取器。
// Python: signal_extractor.py:261 AtomicChecksSignalExtractor（name="atomic_checks"）。
type AtomicChecksSignalExtractor struct{}

// LlmJudgeSignalExtractor llm_as_judge 方法的零 LLM 提取器。
// Python: signal_extractor.py:347 LlmJudgeSignalExtractor（name="llm_judge"）。
type LlmJudgeSignalExtractor struct{}

// ──────────────────────────── 导出函数 ────────────────────────────

// Name 提取器稳定名。Python: name = "generic"
func (GenericSignalExtractor) Name() string { return "generic" }

// Name 提取器稳定名。Python: name = "pytest"
func (PytestSignalExtractor) Name() string { return "pytest" }

// Name 提取器稳定名。Python: name = "reward"
func (RewardSignalExtractor) Name() string { return "reward" }

// Name 提取器稳定名。Python: name = "atomic_checks"
func (AtomicChecksSignalExtractor) Name() string { return "atomic_checks" }

// Name 提取器稳定名。Python: name = "llm_judge"
func (LlmJudgeSignalExtractor) Name() string { return "llm_judge" }

// Extract 提取 exec/judge 失败与错误聚类。
// Python: signal_extractor.py:52-113 GenericSignalExtractor.extract
func (GenericSignalExtractor) Extract(summary EvaluationSummaryInput, caseInputs []CaseAnalysisInput) DeterministicSignals {
	method := summary.EvaluationMethod

	var execFailures, judgeFailures, expectedMismatchCases, missingReferenceCases []string
	errorMap := map[string][]string{}
	errorOrder := []string{}

	for _, c := range caseInputs {
		// Python: if case.status == "failed" / elif not case.evaluation_passed
		if c.Status == "failed" {
			execFailures = append(execFailures, c.CaseID)
		} else if !c.EvaluationPassed {
			judgeFailures = append(judgeFailures, c.CaseID)
		}

		// Python: if case.error: fp = _fingerprint_error(...); error_map...
		if c.Error != "" {
			fp := fingerprintError(c.Error)
			if _, exists := errorMap[fp]; !exists {
				errorOrder = append(errorOrder, fp)
			}
			errorMap[fp] = append(errorMap[fp], c.CaseID)
		}

		// Python: 仅对执行成功的案例检查 expected/response（exec 失败的响应是崩溃产物）
		if c.Status != "failed" {
			if c.Expected == nil {
				missingReferenceCases = append(missingReferenceCases, c.CaseID)
			} else if *c.Expected != c.Response {
				expectedMismatchCases = append(expectedMismatchCases, c.CaseID)
			}
		}
	}

	// Python: error_clusters = [{fingerprint, cases} for fp, cases in error_map.items()]
	errorClusters := make([]map[string]any, 0, len(errorOrder))
	for _, fp := range errorOrder {
		errorClusters = append(errorClusters, map[string]any{
			"fingerprint": fp,
			"cases":       errorMap[fp],
		})
	}

	return DeterministicSignals{
		Method:        method,
		ExecFailures:  execFailures,
		JudgeFailures: judgeFailures,
		ErrorClusters: errorClusters,
		MethodSpecific: map[string]any{
			"expected_mismatch_cases": expectedMismatchCases,
			"missing_reference_cases": missingReferenceCases,
		},
	}
}

// Extract 从案例评估元数据的 pytest 证据中提取失败聚类；
// 证据缺失时回落 generic 并标记 pytest_evidence_missing。
// Python: signal_extractor.py:121-180 PytestSignalExtractor.extract
func (PytestSignalExtractor) Extract(summary EvaluationSummaryInput, caseInputs []CaseAnalysisInput) DeterministicSignals {
	method := summary.EvaluationMethod
	generic := GenericSignalExtractor{}.Extract(summary, caseInputs)

	var evidenceMissing []string
	failedTestClusters := map[string][]string{}
	assertionTypeClusters := map[string][]string{}

	for _, c := range caseInputs {
		// Python: evidence = case.evaluation_metadata.get("evidence"); if not evidence: ...
		evidence, hasEvidence := c.EvaluationMetadata["evidence"]
		if !hasEvidence || evidence == nil || isEmptyCollection(evidence) {
			evidenceMissing = append(evidenceMissing, c.CaseID)
			continue
		}

		// Python: if isinstance(evidence, list): 按 test_name / assertion_type 聚类
		if items, isList := evidence.([]any); isList {
			for _, rawItem := range items {
				item, isMap := rawItem.(map[string]any)
				if !isMap {
					continue
				}
				testName := stringField(item, "test_name", "unknown")
				failedTestClusters[testName] = append(failedTestClusters[testName], c.CaseID)
				aType := stringField(item, "assertion_type", "unknown")
				assertionTypeClusters[aType] = append(assertionTypeClusters[aType], c.CaseID)
			}
		}
	}

	// Python: if evidence_missing: 回落 + fallback_reason
	if len(evidenceMissing) > 0 {
		merged := copyMap(generic.MethodSpecific)
		merged["evidence_missing_cases"] = evidenceMissing
		merged["fallback_reason"] = "pytest_evidence_missing"
		return DeterministicSignals{
			Method:         method,
			ExecFailures:   generic.ExecFailures,
			JudgeFailures:  generic.JudgeFailures,
			ErrorClusters:  generic.ErrorClusters,
			MethodSpecific: merged,
		}
	}

	return DeterministicSignals{
		Method:        method,
		ExecFailures:  generic.ExecFailures,
		JudgeFailures: generic.JudgeFailures,
		ErrorClusters: generic.ErrorClusters,
		MethodSpecific: map[string]any{
			"failed_test_clusters":    failedTestClusters,
			"assertion_type_clusters": assertionTypeClusters,
		},
	}
}

// Extract 提取 reward 与 agent 停滞信号。
// Python: signal_extractor.py:193-253 RewardSignalExtractor.extract
func (RewardSignalExtractor) Extract(summary EvaluationSummaryInput, caseInputs []CaseAnalysisInput) DeterministicSignals {
	method := summary.EvaluationMethod
	generic := GenericSignalExtractor{}.Extract(summary, caseInputs)

	var zeroRewardCases, maxIterationCases, memberHarnessIssueCases []string
	trajectoryFailureSignaturesByCase := map[string][]string{}
	trajectoryRecentEventsByCase := map[string][]map[string]string{}
	normalizedTraceAttributionByCase := map[string]map[string]any{}

	for _, c := range caseInputs {
		// Python: if reward == 0 or reward == 0.0（数值等值比较）
		if reward, ok := c.EvaluationMetadata["reward"]; ok && rewardIsZero(reward) {
			zeroRewardCases = append(zeroRewardCases, c.CaseID)
		}

		// Python: termination_reason == "max_iterations" or 标记串 in response
		if c.TerminationReason == "max_iterations" || strings.Contains(c.Response, maxIterMark) {
			maxIterationCases = append(maxIterationCases, c.CaseID)
		}

		// Python: if case_id 在 zero/max 集合中 → member harness 疑似问题
		inZero, inMax := false, false
		for _, id := range zeroRewardCases {
			if id == c.CaseID {
				inZero = true
				break
			}
		}
		for _, id := range maxIterationCases {
			if id == c.CaseID {
				inMax = true
				break
			}
		}
		if inZero || inMax {
			memberHarnessIssueCases = append(memberHarnessIssueCases, c.CaseID)
		}

		// Python: window = case.trajectory_window_summary; if isinstance(dict)
		window := c.TrajectoryWindowSummary
		if window != nil {
			signatures := signalStringList(window["failure_signatures"], 20)
			if len(signatures) > 0 {
				trajectoryFailureSignaturesByCase[c.CaseID] = signatures
				if !containsStr(memberHarnessIssueCases, c.CaseID) {
					memberHarnessIssueCases = append(memberHarnessIssueCases, c.CaseID)
				}
			}
			recentEvents := boundedRecentEvents(window["recent_events"])
			if len(recentEvents) > 0 {
				trajectoryRecentEventsByCase[c.CaseID] = recentEvents
			}
		}

		// Python: attribution = _normalized_trace_attribution(case)
		if attribution := normalizedTraceAttribution(c); attribution != nil {
			normalizedTraceAttributionByCase[c.CaseID] = attribution
		}
	}

	merged := copyMap(generic.MethodSpecific)
	merged["zero_reward_cases"] = zeroRewardCases
	merged["max_iteration_cases"] = maxIterationCases
	merged["member_harness_issue_cases"] = memberHarnessIssueCases
	merged["trajectory_failure_signatures_by_case"] = trajectoryFailureSignaturesByCase
	merged["trajectory_recent_events_by_case"] = trajectoryRecentEventsByCase
	merged["normalized_trace_attribution_by_case"] = normalizedTraceAttributionByCase

	return DeterministicSignals{
		Method:         method,
		ExecFailures:   generic.ExecFailures,
		JudgeFailures:  generic.JudgeFailures,
		ErrorClusters:  generic.ErrorClusters,
		MethodSpecific: merged,
	}
}

// Extract 提取原子校验失败与轨迹归因。
// Python: signal_extractor.py:266-339 AtomicChecksSignalExtractor.extract
func (AtomicChecksSignalExtractor) Extract(summary EvaluationSummaryInput, caseInputs []CaseAnalysisInput) DeterministicSignals {
	method := summary.EvaluationMethod
	generic := GenericSignalExtractor{}.Extract(summary, caseInputs)

	atomicChecksByCase := map[string]map[string][]string{}
	failedCheckDetailsByCase := map[string][]map[string]string{}
	var partialScoreCases, memberHarnessIssueCases []string
	trajectoryFailureSignaturesByCase := map[string][]string{}
	trajectoryRecentEventsByCase := map[string][]map[string]string{}
	normalizedTraceAttributionByCase := map[string]map[string]any{}

	var execFailures, judgeFailures []string
	for _, c := range caseInputs {
		// Python: exec/judge_failures 在此方法独立计算（非 generic）
		if c.Error != "" {
			execFailures = append(execFailures, c.CaseID)
		}
		if c.Error == "" && !c.EvaluationPassed {
			judgeFailures = append(judgeFailures, c.CaseID)
		}

		// Python: raw_checks = case.evaluation_metadata.get("atomic_checks", [])
		rawChecks := valueOr(c.EvaluationMetadata, "atomic_checks", []any{})
		var passedChecks, failedChecks []string
		var failedDetails []map[string]string
		if checks, isList := rawChecks.([]any); isList {
			for _, rawCheck := range checks {
				check, isMap := rawCheck.(map[string]any)
				if !isMap {
					continue
				}
				// Python: name = str(check.get("name", "") or "").strip(); 空则跳过
				name := strings.TrimSpace(stringField(check, "name", ""))
				if name == "" {
					continue
				}
				// Python: if check.get("passed") is True
				if passed, ok := check["passed"].(bool); ok && passed {
					passedChecks = append(passedChecks, name)
				} else if stringField(check, "status", "") != "skipped" {
					// Python: elif str(check.get("status", "")) != "skipped"
					failedChecks = append(failedChecks, name)
					// Python: detail 截断至 2000
					failedDetails = append(failedDetails, map[string]string{
						"name":   name,
						"detail": truncateStr(stringField(check, "detail", ""), 2000),
					})
				}
			}
		}
		atomicChecksByCase[c.CaseID] = map[string][]string{
			"passed": passedChecks,
			"failed": failedChecks,
		}
		if len(failedDetails) > 0 {
			failedCheckDetailsByCase[c.CaseID] = failedDetails
			memberHarnessIssueCases = append(memberHarnessIssueCases, c.CaseID)
		}
		// Python: if 0.0 < case.score < 1.0
		if c.Score > 0.0 && c.Score < 1.0 {
			partialScoreCases = append(partialScoreCases, c.CaseID)
		}

		window := c.TrajectoryWindowSummary
		if window != nil {
			signatures := signalStringList(window["failure_signatures"], 20)
			if len(signatures) > 0 {
				trajectoryFailureSignaturesByCase[c.CaseID] = signatures
			}
			recentEvents := boundedRecentEvents(window["recent_events"])
			if len(recentEvents) > 0 {
				trajectoryRecentEventsByCase[c.CaseID] = recentEvents
			}
		}
		if attribution := normalizedTraceAttribution(c); attribution != nil {
			normalizedTraceAttributionByCase[c.CaseID] = attribution
		}
	}

	return DeterministicSignals{
		Method:        method,
		ExecFailures:  execFailures,
		JudgeFailures: judgeFailures,
		ErrorClusters: generic.ErrorClusters,
		MethodSpecific: map[string]any{
			"atomic_checks_by_case":                 atomicChecksByCase,
			"failed_check_details_by_case":          failedCheckDetailsByCase,
			"partial_score_cases":                   partialScoreCases,
			"member_harness_issue_cases":            memberHarnessIssueCases,
			"trajectory_failure_signatures_by_case": trajectoryFailureSignaturesByCase,
			"trajectory_recent_events_by_case":      trajectoryRecentEventsByCase,
			"normalized_trace_attribution_by_case":  normalizedTraceAttributionByCase,
		},
	}
}

// Extract 从解析出的 llm_as_judge 维度中提取逐行为分数数据；
// parsed 维度缺失时回落 generic 并标记 parsed_dimensions_missing。
// Python: signal_extractor.py:357-432 LlmJudgeSignalExtractor.extract
func (LlmJudgeSignalExtractor) Extract(summary EvaluationSummaryInput, caseInputs []CaseAnalysisInput) DeterministicSignals {
	method := summary.EvaluationMethod
	generic := GenericSignalExtractor{}.Extract(summary, caseInputs)

	var rationaleMissing []string
	lowScoreBehaviors := map[string][]string{}
	triggeredForbiddenBehaviors := map[string][]string{}
	behaviorScoreDistribution := map[string]map[string]float64{}
	behaviorDiagnosticsByCase := map[string]map[string]map[string]string{}
	avgBehaviorScore := map[string]float64{}
	behaviorPassFailCounts := map[string]map[string]int{}

	for _, c := range caseInputs {
		// Python: parsed = case.evaluation_metadata.get("parsed", {})
		parsed := mapField(c.EvaluationMetadata, "parsed")
		// Python: dimensions = parsed.get("dimensions") if isinstance(parsed, dict) else None
		dimensions, isMap := parsed["dimensions"].(map[string]any)
		if !isMap {
			rationaleMissing = append(rationaleMissing, c.CaseID)
			continue
		}

		cid := c.CaseID
		lowScoreBehaviors[cid] = stringListOrEmpty(dimensions["low_score_behaviors"])
		triggeredForbiddenBehaviors[cid] = stringListOrEmpty(dimensions["triggered_forbidden_behaviors"])
		behaviorScoreDistribution[cid] = floatMapOrEmpty(dimensions["per_behavior_scores"])
		behaviorDiagnosticsByCase[cid] = behaviorDiagnostics(dimensions["behavior_diagnostics"])
		// Python: float(dimensions.get("avg_behavior_score", 0.0))
		avgBehaviorScore[cid] = floatField(dimensions, "avg_behavior_score", 0.0)
		behaviorPassFailCounts[cid] = map[string]int{
			// Python: int(dimensions.get("pass_count", 0)) / fail_count
			"pass_count": intField(dimensions, "pass_count", 0),
			"fail_count": intField(dimensions, "fail_count", 0),
		}
	}

	// Python: if rationale_missing and len == len(case_inputs): 全量缺失 → 回落
	if len(rationaleMissing) > 0 && len(rationaleMissing) == len(caseInputs) {
		merged := copyMap(generic.MethodSpecific)
		merged["rationale_missing_cases"] = rationaleMissing
		merged["fallback_reason"] = "parsed_dimensions_missing"
		return DeterministicSignals{
			Method:         method,
			ExecFailures:   generic.ExecFailures,
			JudgeFailures:  generic.JudgeFailures,
			ErrorClusters:  generic.ErrorClusters,
			MethodSpecific: merged,
		}
	}

	methodSpecific := map[string]any{
		"low_score_behaviors":           lowScoreBehaviors,
		"triggered_forbidden_behaviors": triggeredForbiddenBehaviors,
		"behavior_score_distribution":   behaviorScoreDistribution,
		"behavior_diagnostics":          behaviorDiagnosticsByCase,
		"avg_behavior_score":            avgBehaviorScore,
		"behavior_pass_fail_counts":     behaviorPassFailCounts,
	}
	if len(rationaleMissing) > 0 {
		methodSpecific["rationale_missing_cases"] = rationaleMissing
	}

	return DeterministicSignals{
		Method:         method,
		ExecFailures:   generic.ExecFailures,
		JudgeFailures:  generic.JudgeFailures,
		ErrorClusters:  generic.ErrorClusters,
		MethodSpecific: methodSpecific,
	}
}

// signalExtractorRegistry 提取器注册表（Python dict → Go map + RWMutex）。
// Python: signal_extractor.py:439-451 _EXTRACTOR_MAP
var (
	signalExtractorMu  sync.RWMutex
	signalExtractorMap = map[string]SignalExtractor{
		"script_based":  PytestSignalExtractor{},
		"reward_based":  RewardSignalExtractor{},
		"atomic_checks": AtomicChecksSignalExtractor{},
		"llm_as_judge":  LlmJudgeSignalExtractor{},
	}
)

// RegisterSignalExtractor 注册评估器适配器提供的按方法提取器。
// Python: signal_extractor.py:454-461 register_signal_extractor
func RegisterSignalExtractor(method string, extractor SignalExtractor) error {
	// Python: normalized = str(method).strip(); if not normalized: raise ValueError
	normalized := strings.TrimSpace(method)
	if normalized == "" {
		return errors.New("signal extractor method must be non-empty")
	}
	// Python: if not callable(getattr(extractor, "extract", None)): raise TypeError
	if extractor == nil {
		return errors.New("signal extractor must define extract(summary, case_inputs)")
	}
	signalExtractorMu.Lock()
	defer signalExtractorMu.Unlock()
	signalExtractorMap[normalized] = extractor
	return nil
}

// BuildSignalExtractor 按 judger 方法名分派 SignalExtractor；未知方法回落 generic。
// Python: signal_extractor.py:464-483 build_signal_extractor
func BuildSignalExtractor(method string) SignalExtractor {
	signalExtractorMu.RLock()
	defer signalExtractorMu.RUnlock()
	if extractor, ok := signalExtractorMap[method]; ok {
		return extractor
	}
	return GenericSignalExtractor{}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// fingerprintError 归一化报错字符串中的易变 token，产出稳定聚类键。
// 替换顺序即语义（ts → uuid → path → hex → line → 压空白），与 Python 逐条对应。
// Python: signal_extractor.py:31-41 _fingerprint_error
func fingerprintError(errorStr string) string {
	text := tsPattern.ReplaceAllString(errorStr, "<ts>")
	text = uuidPattern.ReplaceAllString(text, "<uuid>")
	text = pathPattern.ReplaceAllString(text, "<path>")
	text = hexPattern.ReplaceAllString(text, "<hex>")
	text = linePattern.ReplaceAllString(text, ":<N>")
	// Python: " ".join(text.split())
	return strings.Join(strings.Fields(text), " ")
}

// signalStringList 字符串清单投影（前 limit 个）。
// Python: signal_extractor.py:486-489 _string_list
func signalStringList(value any, limit int) []string {
	list, isList := value.([]any)
	if !isList {
		return nil
	}
	result := make([]string, 0, len(list))
	for i, item := range list {
		if i >= limit {
			break
		}
		result = append(result, toDisplayString(item))
	}
	return result
}

// boundedRecentEvents 有界近期事件投影。
// Python: signal_extractor.py:492-505 _bounded_recent_events
func boundedRecentEvents(value any) []map[string]string {
	list, isList := value.([]any)
	if !isList {
		return nil
	}
	events := make([]map[string]string, 0, len(list))
	for i, rawItem := range list {
		if i >= 20 {
			break
		}
		item, isMap := rawItem.(map[string]any)
		if !isMap {
			continue
		}
		events = append(events, map[string]string{
			// Python: str(item.get("event_type", ""))[:80] / [:1200]
			"event_type": truncateStr(stringField(item, "event_type", ""), 80),
			"summary":    truncateStr(stringField(item, "summary", ""), 1200),
		})
	}
	return events
}

// normalizedTraceAttribution 从归一化轨迹证据推导确定性归因种子。
// Python: signal_extractor.py:508-561 _normalized_trace_attribution
func normalizedTraceAttribution(c CaseAnalysisInput) map[string]any {
	traceSummary := c.NormalizedTraceSummary
	if traceSummary == nil {
		return nil
	}
	traces, _ := traceSummary["traces"].([]any)
	for _, rawTrace := range traces {
		trace, isMap := rawTrace.(map[string]any)
		if !isMap {
			continue
		}
		traceID := stringField(trace, "trace_id", "")
		role := normalizedTraceRole(trace)
		messages, isList := trace["messages"].([]any)
		if !isList {
			continue
		}
		for _, rawMessage := range messages {
			message, isMap := rawMessage.(map[string]any)
			if !isMap {
				continue
			}
			messageIndex := message["message_index"]
			toolCalls, isList := message["tool_calls"].([]any)
			if !isList {
				continue
			}
			for _, rawCall := range toolCalls {
				call, isMap := rawCall.(map[string]any)
				if !isMap {
					continue
				}
				// Python: error = str(call.get("error", "") or ""); if not error: continue
				errorStr := stringField(call, "error", "")
				if errorStr == "" {
					continue
				}
				// Python: tool_name = str(call.get("name", "tool"))
				toolName := stringField(call, "name", "tool")
				if toolName == "" {
					toolName = "tool"
				}
				stepPointer := stringField(call, "step_pointer", "")
				command := stringField(call, "input", "")
				displayRole := role
				if displayRole == "" {
					displayRole = "unattributed execution"
				}
				evidenceRef := map[string]any{
					"trace_id":      traceID,
					"role":          role,
					"message_index": messageIndex,
					"step_pointer":  stepPointer,
				}
				targetRef := "unassigned"
				if role != "" {
					targetRef = fmt.Sprintf("member_harness.%s.skill", role)
				}
				confidence := "low"
				if role != "" {
					confidence = "medium"
				}
				return map[string]any{
					// Python: f"{display_role} encountered a failing {tool_name} step and did "
					//         f"not recover before the verifier failed."
					"root_cause": fmt.Sprintf("%s encountered a failing %s step and did "+
						"not recover before the verifier failed.", displayRole, toolName),
					// Python: f"Earliest decisive failed step: {step_pointer or 'unknown step'}; "
					//         f"{tool_name} input={command[:240]!r}; error={error[:240]!r}."
					"critical_mistake": fmt.Sprintf("Earliest decisive failed step: %s; %s input=%q; error=%q.",
						pointerOrUnknown(stepPointer), toolName, truncateStr(command, 240), truncateStr(errorStr, 240)),
					// Python: 固定机制文案
					"general_mechanism": "Strengthen the member workflow so failed commands or verifier errors trigger " +
						"inspection, targeted correction, and re-verification before completion.",
					"target_ref":    targetRef,
					"evidence_refs": []any{evidenceRef},
					"confidence":    confidence,
				}
			}
		}
	}
	return nil
}

// normalizedTraceRole 归一化轨迹角色（team/unattributed 视为未归属）。
// Python: signal_extractor.py:564-568 _normalized_trace_role
func normalizedTraceRole(trace map[string]any) string {
	// Python: role = str(trace.get("member_role") or trace.get("member_id") or "").strip()
	role := strings.TrimSpace(stringField(trace, "member_role", stringField(trace, "member_id", "")))
	if role == "" || role == "team" || role == "unattributed" {
		return ""
	}
	return role
}

// behaviorDiagnostics 行为诊断投影。
// Python: signal_extractor.py:571-585 _behavior_diagnostics
func behaviorDiagnostics(value any) map[string]map[string]string {
	raw, isMap := value.(map[string]any)
	if !isMap {
		return map[string]map[string]string{}
	}
	diagnostics := map[string]map[string]string{}
	for behaviorID, rawDiag := range raw {
		diag, isMap := rawDiag.(map[string]any)
		if !isMap {
			continue
		}
		diagnostics[behaviorID] = map[string]string{
			"reason":                 stringField(diag, "reason", ""),
			"failure_reason":         stringField(diag, "failure_reason", ""),
			"missing_capability":     stringField(diag, "missing_capability", ""),
			"suggested_surface_hint": stringField(diag, "suggested_surface_hint", ""),
			"evidence":               stringField(diag, "evidence", ""),
		}
	}
	return diagnostics
}

// rewardIsZero 数值零值判断（Python reward == 0 or reward == 0.0）。
func rewardIsZero(reward any) bool {
	switch v := reward.(type) {
	case float64:
		return v == 0
	case int:
		return v == 0
	case int64:
		return v == 0
	case bool:
		// Python False == 0 恒真；bool 隐式数值语义（对齐审查要点）
		return !v
	}
	return false
}

// isEmptyCollection 空容器判断（Python not evidence 对空 list/dict/空串为真）。
func isEmptyCollection(value any) bool {
	switch v := value.(type) {
	case string:
		return v == ""
	case []any:
		return len(v) == 0
	case map[string]any:
		return len(v) == 0
	case bool:
		return !v
	}
	return false
}

// copyMap 浅拷贝 method_specific。
func copyMap(source map[string]any) map[string]any {
	copied := make(map[string]any, len(source))
	for key, value := range source {
		copied[key] = value
	}
	return copied
}

// containsStr 清单包含判断。
func containsStr(list []string, target string) bool {
	for _, item := range list {
		if item == target {
			return true
		}
	}
	return false
}

// stringListOrEmpty 字符串清单（非清单得空清单）。
// Python: dimensions.get("low_score_behaviors") or []
func stringListOrEmpty(value any) []string {
	list, isList := value.([]any)
	if !isList {
		return []string{}
	}
	result := make([]string, 0, len(list))
	for _, item := range list {
		result = append(result, toDisplayString(item))
	}
	return result
}

// floatMapOrEmpty float 映射（非映射得空映射）。
func floatMapOrEmpty(value any) map[string]float64 {
	m, isMap := value.(map[string]any)
	if !isMap {
		return map[string]float64{}
	}
	result := make(map[string]float64, len(m))
	for key, item := range m {
		result[key] = floatField(m, key, 0.0)
		_ = item
	}
	return result
}

// truncateStr 前缀截断（Python s[:n]）。
func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// pointerOrUnknown 步骤指针或未知占位。
// Python: step_pointer or 'unknown step'
func pointerOrUnknown(pointer string) string {
	if pointer == "" {
		return "unknown step"
	}
	return pointer
}
