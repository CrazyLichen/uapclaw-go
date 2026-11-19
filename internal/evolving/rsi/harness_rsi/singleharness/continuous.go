package singleharness

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	rsi "github.com/uapclaw/uapclaw-go/internal/evolving/rsi"
	harnessrsi "github.com/uapclaw/uapclaw-go/internal/evolving/rsi/harness_rsi"
	evaluatorpkg "github.com/uapclaw/uapclaw-go/internal/evolving/rsi/harness_rsi/evaluator"
	memberoptimizer "github.com/uapclaw/uapclaw-go/internal/evolving/rsi/harness_rsi/member_optimizer"
	"gopkg.in/yaml.v3"
)

// ──────────────────────────── 非导出函数 ────────────────────────────

// subset 从源评估物化子集（Python: continuous.py _subset）。
// 偏差 D-d2：async → 同步（沿 15.16b0 D-rel-2 先例，MaterializeSource 为同步实现）。
func subset(owner *SingleHarnessIterativeOptimizationOrchestrator,
	cases []map[string]any, sourceRef, refs, directory string) (string, error) {
	if complete, _ := evalRefComplete(filepath.Join(directory, "eval_ref.yaml")); complete {
		return filepath.Join(directory, "eval_ref.yaml"), nil
	}
	sourcePayload, err := readYAMLFile(sourceRef)
	if err != nil {
		return "", err
	}
	rows := map[string]matchedCase{}
	for _, rawCase := range toAnySlice(sourcePayload["cases"]) {
		caseMap, ok := rawCase.(map[string]any)
		if !ok {
			continue
		}
		rows[toString(caseMap["case_id"])] = matchedCase{Path: sourceRef, Case: caseMap}
	}
	reused := map[string]bool{}
	for _, singleCase := range cases {
		reused[toString(singleCase["case_id"])] = true
	}
	return MaterializeSource(cases, rows, directory, refs,
		owner.evaluationContext(cases, refs, nil), reused)
}

// existingPaths 存在文件清单（Python: continuous.py _existing_paths）。
func existingPaths(paths ...string) []string {
	out := []string{}
	for _, path := range paths {
		if fileExists(path) {
			out = append(out, path)
		}
	}
	return out
}

// fullEngineering 全量工程判定（Python: continuous.py _full_engineering）。
func fullEngineering(owner *SingleHarnessIterativeOptimizationOrchestrator,
	gate map[string]any, evalRef string) bool {
	failedEvidence, err := failedMachineEvidence(evalRef)
	if err != nil || len(failedEvidence) > 0 {
		return false
	}
	for _, group := range []string{"tool", "skill"} {
		early, _, err := preEditInvokedNamesByCase(evalRef, group, false)
		if err != nil {
			return false
		}
		later, _, err := preEditInvokedNamesByCase(evalRef, group, true)
		if err != nil {
			return false
		}
		matcher := toolNamesMatch
		if group == "skill" {
			matcher = skillNamesMatch
		}
		var capabilities []map[string]any
		for _, rawCapability := range toAnySlice(gate["capabilities"]) {
			if capability, ok := rawCapability.(map[string]any); ok {
				capabilities = append(capabilities, capability)
			}
		}
		fallback := toStringSet(gate["target_case_ids"])
		if len(missingCapabilityInvocations(capabilities, group, early,
			fallback, matcher, later)) > 0 {
			return false
		}
	}
	return true
}

// continuousProgress 连续模式循环内部共享的可变游标。
type continuousTrialRecord struct {
	record map[string]any
}

// runContinuousLoop 连续模式主循环（Python: continuous.py run_continuous）。
// 偏差 D-d2：async → 同步顺序执行（沿 15.16b0 D-rel-2 先例）。
func runContinuousLoop(
	ctx context.Context,
	owner *SingleHarnessIterativeOptimizationOrchestrator,
	request *IterativeSingleHarnessRequest,
	state map[string]any,
	dataset harnessrsi.DatasetArtifact,
	allCases []map[string]any,
	outputDir string,
	maxEpochs int,
	onEvent rsi.OnEvent,
) (IterativeSingleHarnessResult, error) {
	statePath := filepath.Join(outputDir, "single_harness_state.yaml")
	reportPath := filepath.Join(outputDir, "single_harness_report.yaml")
	idsBool := map[string]bool{}
	casesByID := map[string]map[string]any{}
	for _, singleCase := range allCases {
		id := toString(singleCase["case_id"])
		idsBool[id] = true
		casesByID[id] = singleCase
	}
	ids := map[string]struct{}{}
	for id := range idsBool {
		ids[id] = struct{}{}
	}
	baselineScores, err := CompleteScores(toString(state["baseline_eval_ref_path"]), idsBool)
	if err != nil {
		return IterativeSingleHarnessResult{}, err
	}
	baselinePayload, err := readYAMLFile(toString(state["baseline_eval_ref_path"]))
	if err != nil {
		return IterativeSingleHarnessResult{}, err
	}
	if !anyEqual(baselinePayload["evaluation_context"],
		owner.evaluationContext(allCases, toString(state["source_harness_refs_path"]), nil)) {
		return IterativeSingleHarnessResult{}, &evaluatorpkg.EvaluationInfrastructureError{
			Msg: "Initial continuous baseline protocol or dataset does not match"}
	}
	setDefaultString(state, "best_reference_eval_ref_path", toString(state["baseline_eval_ref_path"]))
	if _, ok := state["best_reference_score"]; !ok {
		mean, err := MeanScore(baselineScores)
		if err != nil {
			return IterativeSingleHarnessResult{}, err
		}
		state["best_reference_score"] = mean
	}
	if _, ok := state["historical_peak_score"]; !ok {
		state["historical_peak_score"] = state["best_reference_score"]
	}
	setDefault(state, "continuous_attempts", map[string]any{})
	setDefault(state, "continuous_trials", map[string]any{})
	setDefault(state, "continuous_batches", map[string]any{})
	attempts := mappingOrNil(state["continuous_attempts"])
	trials := mappingOrNil(state["continuous_trials"])
	batchStates := mappingOrNil(state["continuous_batches"])

	save := func() error { return writeYAMLAtomic(statePath, state) }

	for epoch := 1; epoch <= maxEpochs; epoch++ {
		if err := raiseIfCancelled(ctx); err != nil {
			return IterativeSingleHarnessResult{}, err
		}
		checkpointExists := false
		for _, rawItem := range toAnySlice(state["epoch_checkpoints"]) {
			if item, ok := rawItem.(map[string]any); ok && toInt(item["epoch"]) == epoch {
				checkpointExists = true
				break
			}
		}
		if checkpointExists {
			continue
		}
		currentScores, err := CompleteScores(toString(state["best_eval_ref_path"]), idsBool)
		if err != nil {
			return IterativeSingleHarnessResult{}, err
		}
		hasImprovable := false
		for _, score := range currentScores {
			if 1-score > owner.config.Promotion.MinImprovement+EPSILON {
				hasImprovable = true
				break
			}
		}
		if !hasImprovable {
			state["stop_reason"] = "target_threshold_saturated"
			break
		}
		epochRefs := toString(state["best_harness_refs_path"])
		node := fmt.Sprintf("epoch-%03d", epoch)
		ctx = rsi.SetUsageNode(ctx, node)
		state["active_epoch"] = epoch
		state["active_epoch_before_harness_refs_path"] = epochRefs
		if err := save(); err != nil {
			return IterativeSingleHarnessResult{}, err
		}
		if err := rsi.Emit(ctx, onEvent, ActiveEpochNodeEvent(state)); err != nil {
			return IterativeSingleHarnessResult{}, err
		}
		var planned [][]map[string]any
		// Python 恒走 load_files（空清单由 LoadFiles 报错，与 Python 一致）
		planned, err = owner.dataLoader.LoadFiles(request.DatasetFiles, epoch)
		if err != nil {
			return IterativeSingleHarnessResult{}, err
		}
		batches, err := validateAndFilterPlannedBatches(planned, ids)
		if err != nil {
			return IterativeSingleHarnessResult{}, err
		}
		epochPromoted := false
		for batchIndex, plannedBatch := range batches {
			batchIndex++
			batchKey := fmt.Sprintf("epoch_%03d:batch_%03d", epoch, batchIndex)
			if _, doneBatch := mappingOrNil(state["completed_batches"])[batchKey]; doneBatch {
				if truthy(mappingOrNil(mappingOrNil(state["completed_batches"])[batchKey])["promotion_applied"]) {
					epochPromoted = true
				}
				continue
			}
			frozenBatchRaw, frozenExists := batchStates[batchKey]
			var frozenBatch map[string]any
			if !frozenExists {
				frozenBatch = map[string]any{
					"reference_eval": state["best_eval_ref_path"],
					"parent_refs":    state["best_harness_refs_path"],
					"parent_score":   state["best_score"],
				}
				batchStates[batchKey] = frozenBatch
				if err := save(); err != nil {
					return IterativeSingleHarnessResult{}, err
				}
			} else {
				frozenBatch = frozenBatchRaw.(map[string]any)
			}
			referenceEval := toString(frozenBatch["reference_eval"])
			scores, err := CompleteScores(referenceEval, idsBool)
			if err != nil {
				return IterativeSingleHarnessResult{}, err
			}
			batchCases := []map[string]any{}
			for _, rawCase := range plannedBatch {
				caseID := toString(rawCase["case_id"])
				if 1-scores[caseID] > owner.config.Promotion.MinImprovement+EPSILON {
					batchCases = append(batchCases, casesByID[caseID])
				}
			}
			if len(batchCases) == 0 {
				continue
			}
			parentRefs := toString(frozenBatch["parent_refs"])
			parentScore := numberOf(frozenBatch["parent_score"])
			recordedContext := mappingOrNil(readEvalRefSafe(referenceEval)["evaluation_context"])
			currentContext := owner.evaluationContext(allCases, parentRefs, nil)
			for _, field := range []string{"signature", "cases"} {
				if !anyEqual(recordedContext[field], currentContext[field]) {
					return IterativeSingleHarnessResult{}, &evaluatorpkg.EvaluationInfrastructureError{
						Msg: "Comparison parent no longer matches its recorded full evaluation"}
				}
			}
			directory := filepath.Join(outputDir, "evaluations",
				fmt.Sprintf("e%03d", epoch), fmt.Sprintf("b%03d", batchIndex))
			source, err := subset(owner, batchCases, referenceEval, parentRefs,
				filepath.Join(directory, "source"))
			if err != nil {
				return IterativeSingleHarnessResult{}, err
			}
			llm := strings.ReplaceAll(owner.config.Evaluator.EvaluationMethod, "-", "_") == "llm_as_judge"
			compare := llm && owner.config.Evaluator.JudgeBaselineComparison
			var contexts map[string]*evaluatorpkg.BaselineJudgeContext
			var fingerprints map[string]any
			if compare {
				contexts, err = evaluatorpkg.BaselineContexts(referenceEval, allCases)
				if err != nil {
					return IterativeSingleHarnessResult{}, err
				}
				fingerprints = map[string]any{}
				for caseID, context := range contexts {
					fingerprints[caseID] = context.Fingerprint()
				}
			}
			if existing, ok := frozenBatch["baseline_contexts"]; ok && !anyEqual(existing, fingerprints) {
				return IterativeSingleHarnessResult{}, &evaluatorpkg.EvaluationInfrastructureError{
					Msg: "Frozen batch baseline evidence changed during recovery"}
			}
			frozenBatch["baseline_contexts"] = fingerprints
			if err := save(); err != nil {
				return IterativeSingleHarnessResult{}, err
			}
			// contexts → any 形态（编排器缝隙参数）
			var contextsTyped map[string]*evaluatorpkg.BaselineJudgeContext
			if contexts != nil {
				contextsTyped = contexts
			}
			repairRefs, repairSource := parentRefs, source
			var repairCaps []map[string]any
			promoted := false
			lastGate := map[string]any{}
			budget := owner.config.MemberOptimizer.MaxRepairRoundsPerBatch
			if owner.config.MemberOptimizer.MaxIssueAttemptsPerBatch > 0 &&
				owner.config.MemberOptimizer.MaxIssueAttemptsPerBatch < budget {
				budget = owner.config.MemberOptimizer.MaxIssueAttemptsPerBatch
			}
			for attemptIndex := 1; attemptIndex <= budget; attemptIndex++ {
				if err := raiseIfCancelled(ctx); err != nil {
					return IterativeSingleHarnessResult{}, err
				}
				key := fmt.Sprintf("%s:attempt_%03d", batchKey, attemptIndex)
				record := mappingOrNil(attempts[key])
				if attempts[key] == nil {
					record = map[string]any{}
					attempts[key] = record
				}
				if truthy(record["done"]) {
					if gate, ok := record["gate"].(map[string]any); ok {
						lastGate = gate
						if truthy(gate["accepted"]) {
							promoted = true
							break
						}
					}
					if record["repair_refs"] != nil {
						repairRefs = toString(record["repair_refs"])
						repairSource = toString(record["repair_source"])
						repairCaps = capabilitySlice(record["repair_capabilities"])
					}
					continue
				}
				attemptDir := filepath.Join(directory, "attempts", fmt.Sprintf("a%03d", attemptIndex))
				analysis := ""
				if record["analysis_ref"] != nil {
					analysis = toString(record["analysis_ref"])
				}
				if analysis == "" {
					analysis, err = owner.analyze(ctx, analyzeParams{
						evalRefPath: repairSource, harnessRefsPath: repairRefs,
						outputDir: filepath.Join(attemptDir, "analysis"), nodeRef: node,
						priorCandidateFeedback: priorCandidateFeedback(state, batchCases),
						onEvent:                onEvent,
					})
					if err == nil {
						record["analysis_ref"] = analysis
						if err := save(); err != nil {
							return IterativeSingleHarnessResult{}, err
						}
					}
				}
				var hypothesesRef string
				if err == nil {
					hypothesesRef, err = memberoptimizer.CompileOptimizationHypotheses(
						analysis, batchCases, filepath.Join(attemptDir, "optimization_hypotheses.yaml"))
				}
				if err != nil {
					var unavailable *memberoptimizer.AnalysisUnavailableError
					if errors.As(err, &unavailable) {
						if unavailable.AnalysisRefPath == "" {
							unavailable.AnalysisRefPath = analysis
						}
						failure := map[string]any{}
						for k, v := range unavailable.Details() {
							failure[k] = v
						}
						failure["epoch"] = epoch
						failure["batch_index"] = batchIndex
						failure["batch_attempt_index"] = attemptIndex
						failure["source_eval_ref_path"] = repairSource
						failure["harness_refs_path"] = repairRefs
						targetCaseIDs := []any{}
						for _, singleCase := range batchCases {
							targetCaseIDs = append(targetCaseIDs, toString(singleCase["case_id"]))
						}
						failure["target_case_ids"] = targetCaseIDs
						gate := map[string]any{
							"accepted": false, "status": "inconclusive", "reason": "analysis_unavailable",
							"failure_class": "diagnosis_unavailable", "analysis_error": failure,
							"analysis_ref_path": unavailable.AnalysisRefPath,
							"epoch":             epoch, "batch_index": batchIndex, "batch_attempt_index": attemptIndex,
							"source_eval_ref_path": repairSource, "before_harness_refs_path": parentRefs,
							"candidate_harness_refs_path": "", "capabilities": []any{},
							"target_case_ids":                   failure["target_case_ids"],
							"primary_gate_accepted":             false,
							"comparison_baseline_score":         parentScore,
							"comparison_baseline_eval_ref_path": referenceEval,
							"policy_version":                    owner.config.Promotion.PolicyVersion,
						}
						repairRefs, repairSource = parentRefs, source
						repairCaps = nil
						record["done"] = true
						record["gate"] = gate
						record["analysis_failure"] = failure
						record["repair_refs"] = repairRefs
						record["repair_source"] = repairSource
						record["repair_capabilities"] = []any{}
						failures, _ := state["analysis_failures"].([]any)
						state["analysis_failures"] = append(failures, failure)
						if err := refreshOptimizationExperience(state, outputDir); err != nil {
							return IterativeSingleHarnessResult{}, err
						}
						if err := save(); err != nil {
							return IterativeSingleHarnessResult{}, err
						}
						lastGate = gate
						continue
					}
					return IterativeSingleHarnessResult{}, err
				}
				hypotheses, err := memberoptimizer.LoadOptimizationHypotheses(hypothesesRef)
				if err != nil {
					return IterativeSingleHarnessResult{}, err
				}
				analysisIssues, err := analysisIssueCaseIDs(analysis)
				if err != nil {
					return IterativeSingleHarnessResult{}, err
				}
				if len(analysisIssues) == 0 || len(hypotheses) == 0 {
					state["stop_reason"] = "no_executable_hypotheses"
					break
				}
				memberRef := ""
				if record["member_ref"] != nil {
					memberRef = toString(record["member_ref"])
				}
				if memberRef == "" {
					if err := rsi.Emit(ctx, onEvent, rsi.NewNodeStageEvent(node,
						GenerateStagePayload(1, 1, "running", ""), nil)); err != nil {
						return IterativeSingleHarnessResult{}, err
					}
					optimizeCtx, done := rsi.ModelUsageStage(ctx, "optimize")
					memberRef, err = owner.memberOptimizer.Optimize(
						optimizeCtx, repairSource, analysis, repairRefs,
						filepath.Join(outputDir, "member_optimizations"),
						memberoptimizer.OptimizeExecuteParams{
							DeferPublish:               true,
							RejectedCapabilities:       rejectedCapabilities(state),
							SingleHarness:              true,
							OptimizationHypothesesPath: hypothesesRef,
							OptimizationExperience: map[string]any{
								"journal":          toAnySlice(state["optimization_journal"]),
								"lever_scoreboard": mappingOrNil(state["lever_scoreboard"]),
							},
						})
					done()
					if err != nil {
						return IterativeSingleHarnessResult{}, err
					}
					record["member_ref"] = memberRef
					if err := save(); err != nil {
						return IterativeSingleHarnessResult{}, err
					}
					if err := rsi.Emit(ctx, onEvent, rsi.NewNodeStageEvent(node,
						GenerateStagePayload(1, 1, "done", ""), nil)); err != nil {
						return IterativeSingleHarnessResult{}, err
					}
				}
				member, err := readYAMLFile(memberRef)
				if err != nil {
					return IterativeSingleHarnessResult{}, err
				}
				memberStatus := toString(member["status"])
				if memberStatus != "success" && memberStatus != "candidate_ready" && memberStatus != "published" {
					roleResults := mappingOrNil(member["role_results"])
					var errorStrings []string
					for _, rawResult := range roleResults {
						if result, ok := rawResult.(map[string]any); ok && toString(result["error"]) != "" {
							errorStrings = append(errorStrings, toString(result["error"]))
						}
					}
					verificationPath := filepath.Join(filepath.Dir(memberRef), "verification.json")
					generationStage := "execution"
					if memberStatus == "planning_rejected" {
						generationStage = "planning"
					}
					if fileExists(verificationPath) {
						raw, err := os.ReadFile(verificationPath)
						if err == nil {
							var verification map[string]any
							if json.Unmarshal(raw, &verification) == nil {
								if toString(verification["status"]) == "failed" {
									generationStage = "verification"
									for _, rawCheck := range toAnySlice(verification["checks"]) {
										if check, ok := rawCheck.(map[string]any); ok &&
											toString(check["status"]) == "failed" {
											errorStrings = append(errorStrings, fmt.Sprintf("%s: %s",
												toString(check["name"]), toString(check["error"])))
										}
									}
								}
							}
						}
					}
					skipped := memberStatus == "no_actions" || memberStatus == "no_targets" ||
						memberStatus == "no_issues" || memberStatus == "skipped"
					reason := "candidate_generation_failed"
					if skipped || memberStatus == "planning_rejected" {
						reason = memberStatus
					}
					gate := map[string]any{
						"accepted": false,
						"status":   "skipped",
						"reason":   reason,
						"failure_class": func() string {
							if skipped {
								return "not_applicable"
							}
							return "candidate_generation_failure"
						}(),
						"generation_status": memberStatus,
						"generation_stage":  generationStage,
						"generation_error":  strings.Join(errorStrings, "; "),
						"generation_evidence": toAnySlice(existingPaths(
							memberRef,
							filepath.Join(filepath.Dir(memberRef), "verification.json"),
							filepath.Join(filepath.Dir(memberRef), "fix_result.json"),
							filepath.Join(filepath.Dir(memberRef), "execution_results.json"),
							filepath.Join(filepath.Dir(memberRef), "action_planning_error.json"),
						)),
						"epoch": epoch, "batch_index": batchIndex, "batch_attempt_index": attemptIndex,
						"analysis_ref_path": analysis, "member_optimization_ref_path": memberRef,
						"source_eval_ref_path": source, "before_harness_refs_path": parentRefs,
						"candidate_harness_refs_path": "",
					}
					if skipped {
						gate["status"] = "skipped"
					} else {
						gate["status"] = "rejected"
					}
					capabilities, capErr := candidateCapabilities(member)
					if capErr != nil {
						return IterativeSingleHarnessResult{}, capErr
					}
					gate["capabilities"] = capabilities
					targetCaseIDsAny := []any{}
					for _, singleCase := range batchCases {
						targetCaseIDsAny = append(targetCaseIDsAny, toString(singleCase["case_id"]))
					}
					gate["target_case_ids"] = targetCaseIDsAny
					gate["primary_gate_accepted"] = false
					gate["comparison_baseline_score"] = parentScore
					gate["comparison_baseline_eval_ref_path"] = referenceEval
					gate["policy_version"] = owner.config.Promotion.PolicyVersion
					record["done"] = true
					record["gate"] = gate
					record["target_gate"] = cloneCase(gate)
					gates, _ := state["candidate_gates"].([]any)
					state["candidate_gates"] = append(gates, gate)
					if err := persistPromotion(memberRef, "", gate); err != nil {
						return IterativeSingleHarnessResult{}, err
					}
					if err := refreshOptimizationExperience(state, outputDir); err != nil {
						return IterativeSingleHarnessResult{}, err
					}
					if err := save(); err != nil {
						return IterativeSingleHarnessResult{}, err
					}
					lastGate = gate
					continue
				}
				candidateRefs := defaultString(member["optimized_harness_refs_path"], repairRefs)
				memberCaps, err := candidateCapabilities(member)
				if err != nil {
					return IterativeSingleHarnessResult{}, err
				}
				capabilities := mergeRepairCapabilities(repairCaps, memberCaps)
				packageFingerprint, err := PackageFingerprint(candidateRefs)
				if err != nil {
					return IterativeSingleHarnessResult{}, err
				}
				parentFingerprint, err := PackageFingerprint(parentRefs)
				if err != nil {
					return IterativeSingleHarnessResult{}, err
				}
				casesIdentity := map[string]any{}
				for _, singleCase := range allCases {
					caseID := toString(singleCase["case_id"])
					identity, err := evaluatorpkg.CaseIdentity(singleCase)
					if err != nil {
						return IterativeSingleHarnessResult{}, err
					}
					casesIdentity[caseID] = identity
				}
				identityData := map[string]any{
					"package": packageFingerprint, "parent": parentFingerprint,
					"baseline_eval": referenceEval, "baseline_score": parentScore,
					"cases": casesIdentity,
					// 初始协议签名跨候选目录别名稳定。
					"protocol_signature": state["evaluation_protocol_signature"],
					"baseline_contexts":  fingerprints,
					"promotion_policy":   state["promotion_policy"],
				}
				identity := Digest(identityData)
				priorTrialRaw, hasPriorTrial := trials[identity]
				if !hasPriorTrial {
					trials[identity] = map[string]any{"attempt_key": key}
					if err := save(); err != nil {
						return IterativeSingleHarnessResult{}, err
					}
				} else if priorTrial, ok := priorTrialRaw.(map[string]any); ok {
					if !truthy(priorTrial["done"]) && toString(priorTrial["attempt_key"]) != key {
						return IterativeSingleHarnessResult{}, &evaluatorpkg.EvaluationInfrastructureError{
							Msg: "Resume the original unfinished candidate trial"}
					}
				}
				var gate map[string]any
				if record["target_gate"] != nil {
					gate = cloneCase(record["target_gate"].(map[string]any))
				} else {
					priorTrialDone := false
					if hasPriorTrial {
						if priorTrial, ok := priorTrialRaw.(map[string]any); ok {
							priorTrialDone = truthy(priorTrial["done"])
							if priorTrialDone {
								gate = cloneCase(priorTrial["gate"].(map[string]any))
								gate["decision_reused_from"] = priorTrial["attempt_key"]
								// 已成功试验改过 Best，不是新提案。
								gate["accepted"] = false
								gate["status"] = "rejected"
								gate["reason"] = "duplicate_candidate_decision"
								gate["candidate_harness_refs_path"] = candidateRefs
							}
						}
					}
					if gate == nil && packageFingerprint == parentFingerprint {
						targetCaseIDsAny := []any{}
						for _, singleCase := range batchCases {
							targetCaseIDsAny = append(targetCaseIDsAny, toString(singleCase["case_id"]))
						}
						gate = map[string]any{
							"accepted": false, "status": "rejected", "reason": "candidate_content_unchanged",
							"source_eval_ref_path": source, "before_harness_refs_path": parentRefs,
							"candidate_harness_refs_path": candidateRefs, "capabilities": capabilities,
							"target_case_ids": targetCaseIDsAny,
						}
					}
					if gate == nil {
						gate, err = owner.candidateGate(ctx, candidateGateParams{
							cases: batchCases, sourceEvalRef: source, analysisRef: analysis,
							beforeHarnessRefsPath: parentRefs, candidateHarnessRefsPath: candidateRefs,
							memberStatus: memberStatus, capabilities: capabilities,
							outputDir: filepath.Join(attemptDir, "target"), dataset: dataset,
							nodeRef: node, onEvent: onEvent, baselineContexts: contextsTyped,
						})
						if err != nil {
							return IterativeSingleHarnessResult{}, err
						}
					}
					record["target_gate"] = cloneCase(gate)
					if err := save(); err != nil {
						return IterativeSingleHarnessResult{}, err
					}
				}
				targetAccepted := truthy(gate["accepted"])
				gate["epoch"] = epoch
				gate["batch_index"] = batchIndex
				gate["batch_attempt_index"] = attemptIndex
				gate["analysis_ref_path"] = analysis
				gate["member_optimization_ref_path"] = memberRef
				gate["primary_gate_accepted"] = targetAccepted
				gate["comparison_baseline_score"] = parentScore
				gate["comparison_baseline_eval_ref_path"] = referenceEval
				gate["policy_version"] = owner.config.Promotion.PolicyVersion
				if targetAccepted {
					gate["accepted"] = false
					gate["status"] = "pending_full_evaluation"
					err = func() error {
						trialRaw, ok := trials[identity]
						if !ok {
							trial := map[string]any{"attempt_key": key}
							trials[identity] = trial
							trialRaw = trial
						}
						trial := trialRaw.(map[string]any)
						if toString(trial["attempt_key"]) != key {
							return &evaluatorpkg.EvaluationInfrastructureError{
								Msg: "Resume the original unfinished candidate trial"}
						}
						if err := save(); err != nil {
							return err
						}
						targetIDs := toStringSet(gate["target_case_ids"])
						var targetCases []map[string]any
						for _, singleCase := range allCases {
							if _, isTarget := targetIDs[toString(singleCase["case_id"])]; isTarget {
								targetCases = append(targetCases, singleCase)
							}
						}
						targetRef := toString(gate["candidate_eval_ref_path"])
						targetPayload, err := readYAMLFile(targetRef)
						if err != nil {
							return err
						}
						// YAML 往返将结构体字段名小写化——双方归一为 map 后比较
						storedCtxNorm := yamlNormalize(targetPayload["evaluation_context"])
						nowCtxNorm := yamlNormalize(owner.evaluationContext(targetCases, candidateRefs, contextsTyped))
						if !anyEqual(storedCtxNorm, nowCtxNorm) {
							return &evaluatorpkg.EvaluationInfrastructureError{
								Msg: "Target evaluation does not match the frozen candidate and baseline"}
						}
						targetIDsBool := map[string]bool{}
						for id := range targetIDs {
							targetIDsBool[id] = true
						}
						targetScores, err := CompleteScores(targetRef, targetIDsBool)
						if err != nil {
							return err
						}
						proofRaw := record["improvement_proof"]
						var proof map[string]any
						if proofRaw == nil {
							targetIDList := sortedStringSet(targetIDs)
							targetBaselineScores := map[string]any{}
							for _, id := range targetIDList {
								targetBaselineScores[id] = scores[id]
							}
							proof = map[string]any{
								"identity": identity, "package_fingerprint": packageFingerprint,
								"scope": "target_cases", "policy_version": owner.config.Promotion.PolicyVersion,
								"baseline_score": parentScore, "baseline_scores_digest": Digest(scores),
								"target_case_ids": stringSliceToAny(targetIDList), "target_scores": targetScores,
								"target_baseline_scores": targetBaselineScores,
								"target_scores_digest":   Digest(targetScores),
								"target_eval_ref_path":   targetRef,
								"qualified": TargetsImproved(targetScores, scores, targetIDsBool,
									owner.config.Promotion.MinImprovement),
							}
							record["improvement_proof"] = proof
							if err := save(); err != nil {
								return err
							}
						} else {
							proof = proofRaw.(map[string]any)
						}
						identityOK := toString(proof["identity"]) == identity
						targetDigestOK := toString(proof["target_scores_digest"]) == Digest(targetScores)
						baselineDigestOK := toString(proof["baseline_scores_digest"]) == Digest(scores)
						targetsOK := stringSetEqual(toStringSet(proof["target_case_ids"]), targetIDs)
						qualified := truthy(proof["qualified"])
						if !identityOK || !targetDigestOK || !baselineDigestOK {
							return &evaluatorpkg.EvaluationInfrastructureError{
								Msg: "Frozen candidate or baseline identity changed"}
						}
						if !targetsOK || !qualified {
							return &evaluatorpkg.EvaluationInfrastructureError{
								Msg: "Frozen candidate or baseline identity changed"}
						}
						gate["improvement_proof"] = proof
						gate["reason"] = "target_improvement_observed"
						record["gate"] = gate
						if err := save(); err != nil {
							return err
						}
						// 全量阶段重跑全部用例（含目标）。没有第三次试验。
						fullRef, err := owner.evaluate(ctx, evaluateParams{
							cases: allCases, harnessRefsPath: candidateRefs,
							outputDir: filepath.Join(attemptDir, "full"), dataset: dataset,
							nodeRef: node, onEvent: onEvent, baselineContexts: contextsTyped,
							caseConcurrency: owner.config.Scheduling.FullEvaluationConcurrency,
						})
						if err != nil {
							return err
						}
						fullScores, err := CompleteScores(fullRef, idsBool)
						if err != nil {
							return err
						}
						recheckPackage, err := PackageFingerprint(candidateRefs)
						if err != nil {
							return err
						}
						recheckTarget, err := CompleteScores(targetRef, targetIDsBool)
						if err != nil {
							return err
						}
						recheckBaseline, err := CompleteScores(referenceEval, idsBool)
						if err != nil {
							return err
						}
						if recheckPackage != packageFingerprint ||
							Digest(recheckTarget) != toString(proof["target_scores_digest"]) ||
							Digest(recheckBaseline) != toString(proof["baseline_scores_digest"]) {
							return &evaluatorpkg.EvaluationInfrastructureError{
								Msg: "Candidate or frozen score evidence changed during full evaluation"}
						}
						checks, err := FullEvaluationDecision(fullScores, scores, targetIDsBool,
							owner.config.Promotion.NonTargetMaxRegression, proof, identity)
						if err != nil {
							return err
						}
						engineering := fullEngineering(owner, gate, fullRef)
						acceptedNow := truthy(checks["accepted"]) && engineering
						gate["accepted"] = acceptedNow
						if acceptedNow {
							gate["status"] = "accepted"
						} else {
							gate["status"] = "rejected"
						}
						gate["candidate_full_score"] = checks["score"]
						gate["latest_evaluated_score"] = checks["score"]
						gate["full_eval_ref_path"] = fullRef
						gate["full_scores_digest"] = Digest(fullScores)
						gate["full_engineering_passed"] = engineering
						gate["full_checks"] = checks
						if engineering {
							gate["reason"] = toString(checks["reason"])
						} else {
							gate["reason"] = "engineering_failed"
						}
						if peak, ok := state["historical_peak_score"].(float64); ok {
							if score := numberOf(checks["score"]); score != nil && *score > peak {
								state["historical_peak_score"] = *score
							}
						} else {
							state["historical_peak_score"] = checks["score"]
						}
						if acceptedNow {
							state["best_score"] = checks["score"]
							state["best_reference_score"] = checks["score"]
							state["best_eval_ref_path"] = fullRef
							state["best_reference_eval_ref_path"] = fullRef
							state["best_harness_refs_path"] = candidateRefs
							state["best_improvement_proof"] = proof
							state["current_harness_refs_path"] = candidateRefs
							state["working_harness_refs_path"] = candidateRefs
							state["published_harness_refs_path"] = ""
							state["publication_status"] = "not_published"
							promoted = true
						}
						return nil
					}()
					if err != nil {
						var infraErr *evaluatorpkg.EvaluationInfrastructureError
						if errors.As(err, &infraErr) {
							gate["accepted"] = false
							gate["status"] = "inconclusive"
							gate["reason"] = "evaluation_invalid"
							gate["evaluation_error"] = infraErr.Msg
						} else {
							return IterativeSingleHarnessResult{}, err
						}
					}
				}
				if !truthy(gate["accepted"]) && toString(gate["status"]) == "pending_full_evaluation" {
					gate["status"] = "rejected"
				}
				if toString(gate["candidate_failure_analysis_status"]) == "unavailable" {
					repairRefs, repairSource = parentRefs, source
					repairCaps = nil
					record["repair_refs"] = repairRefs
					record["repair_source"] = repairSource
					record["repair_capabilities"] = []any{}
				} else if !promoted && candidateCanContinueLocally(gate, batchCases) {
					repairRefs = candidateRefs
					repairSource = toString(gate["candidate_eval_ref_path"])
					repairCaps = capabilities
					record["repair_refs"] = repairRefs
					record["repair_source"] = repairSource
					record["repair_capabilities"] = repairCaps
				}
				record["done"] = true
				record["gate"] = gate
				if trialRaw, ok := trials[identity].(map[string]any); ok &&
					toString(trialRaw["attempt_key"]) == key {
					trialRaw["done"] = true
					trialRaw["gate"] = cloneCase(gate)
				}
				gates, _ := state["candidate_gates"].([]any)
				state["candidate_gates"] = append(gates, gate)
				persistRefs := ""
				if candidateRefs != parentRefs {
					persistRefs = candidateRefs
				}
				if err := persistPromotion(memberRef, persistRefs, gate); err != nil {
					return IterativeSingleHarnessResult{}, err
				}
				if err := refreshOptimizationExperience(state, outputDir); err != nil {
					return IterativeSingleHarnessResult{}, err
				}
				if err := save(); err != nil {
					return IterativeSingleHarnessResult{}, err
				}
				lastGate = gate
				if promoted {
					break
				}
			}
			completedBatches := mappingOrNil(state["completed_batches"])
			completedBatches[batchKey] = map[string]any{
				"epoch": epoch, "batch_index": batchIndex, "source_eval_ref_path": source,
				"analysis_ref_path":        toString(lastGate["analysis_ref_path"]),
				"before_harness_refs_path": parentRefs,
				"after_harness_refs_path":  state["best_harness_refs_path"],
				"candidate_gate_status":    defaultString(lastGate["status"], "skipped"),
				"candidate_gate_reason":    defaultString(lastGate["reason"], "no_actionable_analysis_issues"),
				"promotion_applied":        promoted,
			}
			epochPromoted = epochPromoted || promoted
			if err := save(); err != nil {
				return IterativeSingleHarnessResult{}, err
			}
		}
		state["active_epoch"] = 0
		checkpoint := map[string]any{
			"epoch": epoch, "before_harness_refs_path": epochRefs,
			"harness_refs_path":          state["best_harness_refs_path"],
			"selected_harness_refs_path": state["best_harness_refs_path"],
			"eval_ref_path":              state["best_eval_ref_path"],
			"score":                      state["best_score"],
			"promotion_applied":          epochPromoted,
			"promotion_reason": func() string {
				if epochPromoted {
					return "two_stage_candidate_accepted"
				}
				return "no_accepted_improvement"
			}(),
			"status": func() string {
				if epochPromoted {
					return "accepted"
				}
				return "rejected"
			}(),
			"policy_version": owner.config.Promotion.PolicyVersion,
		}
		checkpoints, _ := state["epoch_checkpoints"].([]any)
		state["epoch_checkpoints"] = append(checkpoints, checkpoint)
		if err := save(); err != nil {
			return IterativeSingleHarnessResult{}, err
		}
		if err := rsi.Emit(ctx, onEvent, EpochNodeEvent(state, checkpoint)); err != nil {
			return IterativeSingleHarnessResult{}, err
		}
		if err := rsi.Emit(ctx, onEvent, ProgressEvent(state, maxEpochs)); err != nil {
			return IterativeSingleHarnessResult{}, err
		}
	}
	if err := ensureFinalPublication(state, outputDir); err != nil {
		return IterativeSingleHarnessResult{}, err
	}
	state["status"] = "completed"
	if err := save(); err != nil {
		return IterativeSingleHarnessResult{}, err
	}
	if err := writeYAMLAtomic(reportPath, buildReport(state, dataset)); err != nil {
		return IterativeSingleHarnessResult{}, err
	}
	return resultFromState(state, statePath, reportPath), nil
}

// capabilitySlice any 转 capability 切片。
func capabilitySlice(value any) []map[string]any {
	out := []map[string]any{}
	for _, raw := range toAnySlice(value) {
		if capability, ok := raw.(map[string]any); ok {
			out = append(out, capability)
		}
	}
	return out
}

// stringSliceToAny 字符串切片转 any 切片。
func stringSliceToAny(values []string) []any {
	out := make([]any, 0, len(values))
	for _, value := range values {
		out = append(out, value)
	}
	return out
}

// yamlNormalize 经 YAML 序列化往返归一（结构体字段名小写化 + map 键排序）。
func yamlNormalize(value any) any {
	data, err := yaml.Marshal(value)
	if err != nil {
		return value
	}
	var out any
	if err := yaml.Unmarshal(data, &out); err != nil {
		return value
	}
	return out
}
