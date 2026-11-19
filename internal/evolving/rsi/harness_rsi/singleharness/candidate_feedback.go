// 确定性的候选排序与紧凑反馈账本助手（candidate_feedback.py 一比一对齐）。
package singleharness

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/text/unicode/norm"

	improverevolution "github.com/uapclaw/uapclaw-go/internal/evolving/rsi/harness_rsi/improver_evolution"
)

// feedbackRankingPolicy 排序策略名（Python _RANKING_POLICY）。
const feedbackRankingPolicy = "static_priority_v1"

// instrumentedGroups 有运行时事件插桩的分组（Python _INSTRUMENTED_GROUPS）。
var instrumentedGroups = map[string]bool{"skill": true, "tool": true}

// instrumentedOperations 有插桩的操作（Python _INSTRUMENTED_OPERATIONS）。
var instrumentedOperations = map[string]bool{"add": true, "modify": true, "update": true}

// nonExecutableStatuses 不可执行状态（Python _NON_EXECUTABLE_STATUSES）。
var nonExecutableStatuses = map[string]bool{
	"blocked": true, "failed": true, "inactionable": true, "invalid": true,
	"no_candidate": true, "unsupported": true, "unsupported_capability_request": true,
}

// uninstrumentedGroups 无插桩分组（Python _UNINSTRUMENTED_GROUPS）。
var uninstrumentedGroups = map[string]bool{"config": true, "control": true, "prompt": true, "rail": true}

// whitespacePattern 空白折叠（Python re \s+）。
var whitespacePattern = regexp.MustCompile(`\s+`)

// ──────────────────────────── 导出函数 ────────────────────────────

// CanonicalCandidateFingerprint 排除动作 ID 与文件名的语义指纹（Python canonical_candidate_fingerprint）。
func CanonicalCandidateFingerprint(capabilities []map[string]any) string {
	semanticCapabilities := make([]any, 0, len(capabilities))
	for _, item := range capabilities {
		if item == nil {
			continue
		}
		semanticCapabilities = append(semanticCapabilities, semanticCapability(item))
	}
	sort.Slice(semanticCapabilities, func(i, j int) bool {
		return feedbackCanonicalJSON(semanticCapabilities[i]) < feedbackCanonicalJSON(semanticCapabilities[j])
	})
	payload := []byte(feedbackCanonicalJSON(semanticCapabilities))
	sum := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// RankCandidateProposals 冻结 static_priority_v1 排名并保留提案输入顺序（Python rank_candidate_proposals）。
func RankCandidateProposals(proposals []map[string]any, frozenTargetCaseIDs map[string]bool, improverPolicy *improverevolution.VersionedImproverPolicy) ([]map[string]any, error) {
	policy := improverPolicy
	if policy == nil {
		policy = improverevolution.DefaultImproverPolicy()
	}
	frozenTargets := map[string]bool{}
	for caseID := range frozenTargetCaseIDs {
		if text(caseID) != "" {
			frozenTargets[text(caseID)] = true
		}
	}
	type preparedItem struct {
		position             int
		proposal             map[string]any
		candidateID          string
		candidateIndex       int
		candidateFingerprint string
		capabilities         []map[string]any
		isExecutable         bool
		coveredTargetCaseIDs []string
		coverageRatio        float64
		actionCount          int
		atomicity            float64
		duplicate            bool
		duplicateOfCandidate string
		predictedScore       float64
	}
	prepared := make([]preparedItem, 0, len(proposals))
	for position, proposal := range proposals {
		capabilities := capabilitiesOf(proposal)
		candidateIndex := candidateIndexOf(proposal, position)
		coveredTargets := map[string]bool{}
		for caseID := range proposalTargetCaseIDs(proposal, capabilities) {
			if frozenTargets[caseID] {
				coveredTargets[caseID] = true
			}
		}
		coverageRatio := 0.0
		if len(frozenTargets) > 0 {
			coverageRatio = float64(len(coveredTargets)) / float64(len(frozenTargets))
		}
		actionCount := len(capabilities)
		atomicity := 0.0
		if actionCount > 0 {
			atomicity = 1.0 / float64(actionCount)
		}
		coveredList := make([]string, 0, len(coveredTargets))
		for caseID := range coveredTargets {
			coveredList = append(coveredList, caseID)
		}
		sort.Strings(coveredList)
		prepared = append(prepared, preparedItem{
			position:             position,
			proposal:             proposal,
			candidateID:          candidateID(proposal, candidateIndex),
			candidateIndex:       candidateIndex,
			candidateFingerprint: CanonicalCandidateFingerprint(capabilities),
			capabilities:         capabilities,
			isExecutable:         isExecutable(proposal, capabilities),
			coveredTargetCaseIDs: coveredList,
			coverageRatio:        coverageRatio,
			actionCount:          actionCount,
			atomicity:            atomicity,
		})
	}

	firstByFingerprint := map[string]*preparedItem{}
	orderBy := func(a, b *preparedItem) bool {
		if a.isExecutable != b.isExecutable {
			return !a.isExecutable && b.isExecutable
		}
		if a.candidateIndex != b.candidateIndex {
			return a.candidateIndex < b.candidateIndex
		}
		return a.position < b.position
	}
	sortedForFirst := make([]*preparedItem, len(prepared))
	for i := range prepared {
		sortedForFirst[i] = &prepared[i]
	}
	sort.SliceStable(sortedForFirst, func(i, j int) bool { return orderBy(sortedForFirst[i], sortedForFirst[j]) })
	for _, item := range sortedForFirst {
		if _, present := firstByFingerprint[item.candidateFingerprint]; !present {
			firstByFingerprint[item.candidateFingerprint] = item
		}
	}

	for i := range prepared {
		item := &prepared[i]
		first := firstByFingerprint[item.candidateFingerprint]
		duplicate := first != item
		score, err := improverevolution.ScoreStaticPriority(policy, map[string]any{
			"executable": item.isExecutable,
			"coverage":   item.coverageRatio,
			"atomicity":  item.atomicity,
			"duplicate":  duplicate,
		})
		if err != nil {
			return nil, err
		}
		item.duplicate = duplicate
		if duplicate {
			item.duplicateOfCandidate = first.candidateID
		}
		item.predictedScore = roundFloat(score, 6)
	}

	ranked := make([]*preparedItem, len(prepared))
	for i := range prepared {
		ranked[i] = &prepared[i]
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		a, b := ranked[i], ranked[j]
		if a.predictedScore != b.predictedScore {
			return a.predictedScore > b.predictedScore
		}
		if a.candidateIndex != b.candidateIndex {
			return a.candidateIndex < b.candidateIndex
		}
		return a.position < b.position
	})
	ranks := map[int]int{}
	for rank, item := range ranked {
		ranks[item.position] = rank + 1
	}

	output := make([]map[string]any, 0, len(prepared))
	for i := range prepared {
		item := &prepared[i]
		merged := map[string]any{}
		for key, value := range item.proposal {
			merged[key] = value
		}
		executableFloat := 0.0
		if item.isExecutable {
			executableFloat = 1.0
		}
		duplicateFloat := 0.0
		if item.duplicate {
			duplicateFloat = 1.0
		}
		merged["candidate_id"] = item.candidateID
		merged["candidate_index"] = item.candidateIndex
		merged["candidate_fingerprint"] = item.candidateFingerprint
		merged["predicted_score"] = item.predictedScore
		merged["predicted_rank"] = ranks[item.position]
		merged["ranking_policy"] = policy.RankingPolicy
		merged["improver_version_id"] = policy.VersionID
		merged["improver_policy_digest"] = policy.CanonicalDigest()
		merged["rank_frozen"] = true
		merged["ranking_features"] = map[string]any{
			"executable":                executableFloat,
			"coverage":                  roundFloat(item.coverageRatio, 6),
			"atomicity":                 roundFloat(item.atomicity, 6),
			"duplicate":                 duplicateFloat,
			"covered_target_case_ids":   item.coveredTargetCaseIDs,
			"action_count":              item.actionCount,
			"duplicate_of_candidate_id": item.duplicateOfCandidate,
		}
		output = append(output, merged)
	}
	return output, nil
}

// BuildCandidateFeedbackCohort 从已评估候选构建一个紧凑 Ledger v1 cohort（Python build_candidate_feedback_cohort）。
func BuildCandidateFeedbackCohort(cohort map[string]any, candidates []map[string]any, selectedCandidateID string, topM int) (map[string]any, error) {
	if topM < 1 {
		return nil, fmt.Errorf("top_m must be at least 1")
	}
	parentHarnessRef := firstTextOf(cohort, "parent_harness_ref", "source_harness_ref", "before_harness_refs_path")
	sourceEvalRef := firstTextOf(cohort, "source_eval_ref", "source_eval_ref_path", "paired_source_eval_ref_path")
	frozenTargets := caseIDsOf(cohort["frozen_target_case_ids"])
	if len(frozenTargets) == 0 {
		frozenTargets = caseIDsOf(cohort["target_case_ids"])
	}
	evaluationProtocol := firstTextOf(cohort, "evaluation_protocol", "evaluation_protocol_id", "protocol_id")
	rankFrozen, _ := cohort["rank_frozen"].(bool)
	rankingPolicy := text(cohort["ranking_policy"])
	if rankingPolicy == "" {
		rankingPolicy = feedbackRankingPolicy
	}

	candidateRecords := make([]map[string]any, 0, len(candidates))
	for position, candidate := range candidates {
		record, err := candidateFeedbackRecord(candidate, candidateRecordParams{
			position:                 position,
			cohortParentHarnessRef:   parentHarnessRef,
			cohortSourceEvalRef:      sourceEvalRef,
			cohortTargetCaseIDs:      frozenTargets,
			cohortEvaluationProtocol: evaluationProtocol,
			cohortRankFrozen:         rankFrozen,
			cohortRankingPolicy:      rankingPolicy,
			selectedCandidateID:      selectedCandidateID,
		})
		if err != nil {
			return nil, err
		}
		candidateRecords = append(candidateRecords, record)
	}
	cohortID := text(cohort["cohort_id"])
	if cohortID == "" {
		cohortID = cohortIDOf(parentHarnessRef, sourceEvalRef, frozenTargets, evaluationProtocol)
	}
	unavailableReason := searchMetricUnavailableReason(
		parentHarnessRef, sourceEvalRef, frozenTargets, evaluationProtocol, rankFrozen, rankingPolicy, candidateRecords)
	searchMetrics := searchMetrics(candidateRecords, topM, unavailableReason)

	runID := text(cohort["run_id"])
	targetList := make([]string, 0, len(frozenTargets))
	for caseID := range frozenTargets {
		targetList = append(targetList, caseID)
	}
	sort.Strings(targetList)
	selectedFound := false
	for _, record := range candidateRecords {
		if record["candidate_id"] == selectedCandidateID {
			selectedFound = true
			break
		}
	}
	return map[string]any{
		"schema_version": 1,
		"record_type":    "candidate_feedback_cohort",
		"cohort": map[string]any{
			"cohort_id":              cohortID,
			"run_id":                 runID,
			"parent_harness_ref":     parentHarnessRef,
			"source_eval_ref":        sourceEvalRef,
			"target_case_ids":        targetList,
			"evaluation_protocol":    evaluationProtocol,
			"rank_frozen":            rankFrozen,
			"ranking_policy":         rankingPolicy,
			"candidate_count":        len(candidateRecords),
			"improver_version_id":    text(cohort["improver_version_id"]),
			"improver_policy_digest": text(cohort["improver_policy_digest"]),
		},
		"candidates": candidateRecords,
		"selection": map[string]any{
			"selected_candidate_id":    selectedCandidateID,
			"selected_candidate_found": selectedFound,
			"top_m":                    topM,
			"promotion_policy":         "best_realized_qualified_candidate",
			"top_m_role":               "counterfactual_metric_only",
		},
		"metrics": searchMetrics,
	}, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// semanticCapability 语义能力投影（Python _semantic_capability）。
func semanticCapability(capability map[string]any) map[string]any {
	leverDecision, _ := capability["lever_decision"].(map[string]any)
	if leverDecision == nil {
		leverDecision = map[string]any{}
	}
	hypotheses := []any{}
	for _, key := range []string{"optimization_hypothesis_ids", "hypothesis_ids", "optimization_contract_sha256"} {
		value := capability[key]
		switch v := value.(type) {
		case []any:
			hypotheses = append(hypotheses, v...)
		case string:
			if v != "" {
				hypotheses = append(hypotheses, v)
			}
		case nil:
		default:
			if v != nil {
				hypotheses = append(hypotheses, v)
			}
		}
	}
	for _, key := range []string{"hypothesis_id", "hypothesis"} {
		if value := capability[key]; value != nil && value != "" {
			hypotheses = append(hypotheses, value)
		}
	}
	normalizedSet := map[string]bool{}
	normalizedList := []string{}
	for _, item := range hypotheses {
		key := feedbackCanonicalJSON(normalizedValue(item))
		if !normalizedSet[key] {
			normalizedSet[key] = true
			normalizedList = append(normalizedList, key)
		}
	}
	sort.Strings(normalizedList)
	lever := text(capability["lever"])
	if lever == "" {
		lever = text(capability["selected_lever"])
	}
	if lever == "" {
		lever = text(leverDecision["selected_lever"])
	}
	return map[string]any{
		"action_group":    normalizeText(capability["action_group"]),
		"operation":       normalizeText(capability["operation"]),
		"role":            normalizeText(capability["role"]),
		"target_family":   targetFamily(capability),
		"expected_effect": normalizeText(capability["expected_effect"]),
		"lever":           normalizeText(lever),
		"hypotheses":      normalizedList,
	}
}

// targetFamily 目标家族别名解析（Python _target_family）。
func targetFamily(capability map[string]any) string {
	explicit := text(capability["target_family"])
	if explicit == "" {
		explicit = text(capability["target_ref"])
	}
	if explicit != "" {
		return normalizeText(explicit)
	}
	actionGroup := normalizeText(capability["action_group"])
	targetPath := strings.ReplaceAll(normalizeText(capability["target_path"]), "\\", "/")
	parts := strings.Split(targetPath, "/")
	aliases := map[string]string{
		"prompt_sections": "prompt",
		"prompts":         "prompt",
		"rails":           "rail",
		"skills":          "skill",
		"tools":           "tool",
	}
	for _, part := range parts[:len(parts)-1] {
		if part == "" {
			continue
		}
		if alias, present := aliases[part]; present {
			return alias
		}
	}
	return actionGroup
}

// normalizedValue 递归规范化（剔除动作 ID 等非语义键，Python _normalized_value）。
func normalizedValue(value any) any {
	switch v := value.(type) {
	case map[string]any:
		normalized := map[string]any{}
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if key == "action_id" || key == "target_path" || key == "runtime_name" {
				continue
			}
			normalized[normalizeText(key)] = normalizedValue(v[key])
		}
		return normalized
	case []any:
		normalized := make([]any, 0, len(v))
		for _, item := range v {
			normalized = append(normalized, normalizedValue(item))
		}
		sort.SliceStable(normalized, func(i, j int) bool {
			return feedbackCanonicalJSON(normalized[i]) < feedbackCanonicalJSON(normalized[j])
		})
		return normalized
	case string:
		return normalizeText(v)
	}
	return value
}

// candidateRecordParams 候选记录构建参数。
type candidateRecordParams struct {
	position                 int
	cohortParentHarnessRef   string
	cohortSourceEvalRef      string
	cohortTargetCaseIDs      map[string]bool
	cohortEvaluationProtocol string
	cohortRankFrozen         bool
	cohortRankingPolicy      string
	selectedCandidateID      string
}

// candidateFeedbackRecord 单候选紧凑反馈记录（Python _candidate_feedback_record）。
func candidateFeedbackRecord(candidate map[string]any, params candidateRecordParams) (map[string]any, error) {
	candidateIndex := candidateIndexOf(candidate, params.position)
	candidateIDValue := candidateID(candidate, candidateIndex)
	capabilities := capabilitiesOf(candidate)
	targetCaseIDs := caseIDsOf(candidate["target_case_ids"])
	if len(targetCaseIDs) == 0 {
		targetCaseIDs = copyKeys(params.cohortTargetCaseIDs)
	}
	sourceTargetScore := optionalNumber(candidate["source_target_score"])
	candidateTargetScore := optionalNumber(candidate["candidate_target_score"])
	targetGain := optionalNumber(candidate["target_gain"])
	if targetGain == nil && sourceTargetScore != nil && candidateTargetScore != nil {
		gain := *candidateTargetScore - *sourceTargetScore
		targetGain = &gain
	}
	parentHarnessRef := firstTextOf(candidate, "parent_harness_ref", "source_harness_ref", "before_harness_refs_path")
	if parentHarnessRef == "" {
		parentHarnessRef = params.cohortParentHarnessRef
	}
	sourceEvalRef := firstTextOf(candidate, "source_eval_ref", "source_eval_ref_path", "paired_source_eval_ref_path")
	if sourceEvalRef == "" {
		sourceEvalRef = params.cohortSourceEvalRef
	}
	evaluationProtocol := firstTextOf(candidate, "evaluation_protocol", "evaluation_protocol_id", "protocol_id")
	if evaluationProtocol == "" {
		evaluationProtocol = params.cohortEvaluationProtocol
	}
	rankingPolicy := text(candidate["ranking_policy"])
	if rankingPolicy == "" {
		rankingPolicy = params.cohortRankingPolicy
	}
	rankFrozen := false
	if rawFrozen, present := candidate["rank_frozen"]; present {
		rankFrozen, _ = rawFrozen.(bool)
	} else {
		rankFrozen = params.cohortRankFrozen
	}
	fingerprint := text(candidate["candidate_fingerprint"])
	if fingerprint == "" {
		fingerprint = CanonicalCandidateFingerprint(capabilities)
	}
	targetList := sortedKeysBool(targetCaseIDs)
	rankingFeatures := map[string]any{}
	if raw, ok := candidate["ranking_features"].(map[string]any); ok {
		for key, value := range raw {
			rankingFeatures[key] = value
		}
	}
	causalExperiment, err := causalExperimentSummary(candidate, targetCaseIDs)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"candidate_id":          candidateIDValue,
		"candidate_index":       candidateIndex,
		"candidate_fingerprint": fingerprint,
		"selected":              candidateIDValue == params.selectedCandidateID,
		"identity": map[string]any{
			"parent_harness_ref":  parentHarnessRef,
			"source_eval_ref":     sourceEvalRef,
			"target_case_ids":     targetList,
			"evaluation_protocol": evaluationProtocol,
		},
		"proposal_rank": map[string]any{
			"predicted_score":  optionalNumber(candidate["predicted_score"]),
			"predicted_rank":   positiveInt(candidate["predicted_rank"]),
			"ranking_policy":   rankingPolicy,
			"rank_frozen":      rankFrozen,
			"ranking_features": rankingFeatures,
		},
		"outcome": map[string]any{
			"source_target_score":    sourceTargetScore,
			"candidate_target_score": candidateTargetScore,
			"target_gain":            targetGain,
			"status":                 text(candidate["status"]),
			"reason":                 text(candidate["reason"]),
			"failure_class":          text(candidate["failure_class"]),
			"causal_failure_class":   text(candidate["causal_failure_class"]),
		},
		"causal_experiment": causalExperiment,
		"activation":        activationSummary(candidate, capabilities, targetCaseIDs),
		"regression": map[string]any{
			"target": targetRegression(candidate, targetCaseIDs),
			"non_target": map[string]any{
				"availability":         "not_evaluated",
				"regressed_case_count": nil,
				"rate":                 nil,
				"reason":               "candidate_feedback_is_target_local",
			},
		},
		"verifier_summary": verifierSummary(candidate),
		"cost": map[string]any{
			"availability":  "not_instrumented",
			"calls":         nil,
			"input_tokens":  nil,
			"output_tokens": nil,
			"total_tokens":  nil,
			"total_cost":    nil,
		},
		"evidence_refs": evidenceRefs(candidate),
	}, nil
}

// causalExperimentSummary 因果实验摘要（Python _causal_experiment_summary）。
func causalExperimentSummary(candidate map[string]any, targetCaseIDs map[string]bool) (map[string]any, error) {
	contracts := []map[string]any{}
	rawContracts, _ := candidate["causal_intervention_contracts"].([]any)
	for _, raw := range rawContracts {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		contractCaseIDs := caseIDsOf(item["target_case_ids"])
		overlaps := false
		for caseID := range contractCaseIDs {
			if targetCaseIDs[caseID] {
				overlaps = true
				break
			}
		}
		if len(contractCaseIDs) == 0 || overlaps {
			cloned := map[string]any{}
			for key, value := range item {
				cloned[key] = value
			}
			contracts = append(contracts, cloned)
		}
	}
	diagnoses, _ := candidate["candidate_failure_diagnoses"].(map[string]any)
	if diagnoses == nil {
		diagnoses = map[string]any{}
	}
	assessments := map[string]any{}
	for _, caseID := range sortedKeysBool(targetCaseIDs) {
		values := []any{}
		for _, item := range diagnosisItems(diagnoses[caseID]) {
			assessment, ok := item["prior_experiment_assessment"].(map[string]any)
			if ok {
				cloned := map[string]any{}
				for key, value := range assessment {
					cloned[key] = value
				}
				values = append(values, cloned)
			}
		}
		if len(values) > 0 {
			assessments[caseID] = values
		}
	}
	return map[string]any{
		"availability":                          observedOrNot(len(contracts) > 0),
		"prediction_recorded_before_evaluation": len(contracts) > 0,
		"intervention_contracts":                contracts,
		"assessment_by_case":                    assessments,
	}, nil
}

// activationSummary 激活摘要（Python _activation_summary）。
func activationSummary(candidate map[string]any, capabilities []map[string]any, fallbackTargetCaseIDs map[string]bool) map[string]any {
	surfaces := map[string]bool{}
	for _, item := range capabilities {
		surfaces[normalizeText(item["action_group"])] = true
	}
	type pairKey struct {
		group   string
		runtime string
		caseID  string
	}
	expectedPairs := map[pairKey]bool{}
	missingRuntimeName := false
	for _, capability := range capabilities {
		actionGroup := normalizeText(capability["action_group"])
		operation := normalizeText(capability["operation"])
		if !instrumentedGroups[actionGroup] || !instrumentedOperations[operation] {
			continue
		}
		runtimeName := text(capability["runtime_name"])
		if runtimeName == "" {
			missingRuntimeName = true
			continue
		}
		targetCaseIDs := caseIDsOf(capability["target_case_ids"])
		if len(targetCaseIDs) == 0 {
			targetCaseIDs = fallbackTargetCaseIDs
		}
		for caseID := range targetCaseIDs {
			expectedPairs[pairKey{actionGroup, runtimeName, caseID}] = true
		}
	}
	if missingRuntimeName {
		return unavailableActivation("missing_artifact", surfaces, "expected_runtime_name_missing")
	}
	if len(expectedPairs) == 0 {
		if setsIntersect(surfaces, uninstrumentedGroups) {
			observed := behavioralActivationFromDiagnoses(candidate, fallbackTargetCaseIDs)
			if observed != nil {
				observed["surfaces"] = sortedIntersection(surfaces, uninstrumentedGroups)
				return observed
			}
			return unavailableActivation("not_instrumented", surfaces, "surface_has_no_activation_event")
		}
		return unavailableActivation("not_applicable", surfaces, "no_added_or_modified_runtime_capability")
	}

	observedMaps := map[string]map[string]any{}
	for group := range instrumentedGroups {
		key := fmt.Sprintf("pre_edit_invoked_%s_names_by_case", group)
		if raw, present := candidate[key]; present && raw != nil {
			if m, ok := raw.(map[string]any); ok {
				observedMaps[group] = m
			}
		}
	}
	for pair := range expectedPairs {
		m, hasGroup := observedMaps[pair.group]
		if !hasGroup {
			return unavailableActivation("missing_artifact", surfaces, "pre_edit_invocation_evidence_missing")
		}
		if _, hasCase := m[pair.caseID]; !hasCase {
			return unavailableActivation("missing_artifact", surfaces, "pre_edit_invocation_evidence_missing")
		}
	}
	observedCount := 0
	for pair := range expectedPairs {
		rawNames := observedMaps[pair.group][pair.caseID]
		names, _ := rawNames.([]any)
		for _, name := range names {
			if runtimeNamesMatch(pair.group, pair.runtime, toString(name)) {
				observedCount++
				break
			}
		}
	}
	expectedCount := len(expectedPairs)
	surfaceList := sortedIntersection(surfaces, instrumentedGroups)
	state := "not_triggered"
	if observedCount == expectedCount {
		state = "triggered"
	}
	return map[string]any{
		"availability":                 "observed",
		"state":                        state,
		"surfaces":                     surfaceList,
		"expected_pair_count":          expectedCount,
		"observed_pre_edit_pair_count": observedCount,
		"missing_pair_count":           expectedCount - observedCount,
		"trigger_rate":                 float64(observedCount) / float64(expectedCount),
	}
}

// behavioralActivationFromDiagnoses 无运行时事件面用配对 Analyzer 评估（Python _behavioral_activation_from_diagnoses）。
func behavioralActivationFromDiagnoses(candidate map[string]any, targetCaseIDs map[string]bool) map[string]any {
	diagnoses, ok := candidate["candidate_failure_diagnoses"].(map[string]any)
	if !ok {
		return nil
	}
	assessments := []map[string]any{}
	for _, caseID := range sortedKeysBool(targetCaseIDs) {
		for _, item := range diagnosisItems(diagnoses[caseID]) {
			if assessment, ok := item["prior_experiment_assessment"].(map[string]any); ok {
				assessments = append(assessments, assessment)
			}
		}
	}
	if len(assessments) == 0 {
		return nil
	}
	activationValues := map[string]bool{}
	behaviorValues := map[string]bool{}
	outcomeValues := map[string]bool{}
	for _, item := range assessments {
		activationValues[normalizeText(item["intervention_activated"])] = true
		behaviorValues[normalizeText(item["predicted_behavior_occurred"])] = true
		outcomeValues[normalizeText(item["predicted_outcome_occurred"])] = true
	}
	state := "unknown"
	if activationValues["yes"] && len(activationValues) == 1 {
		state = "triggered"
	} else if activationValues["no"] {
		state = "not_triggered"
	}
	return map[string]any{
		"availability":                "observed",
		"state":                       state,
		"observation_source":          "candidate_failure_analysis",
		"assessment_count":            len(assessments),
		"intervention_activated":      singleObservation(activationValues),
		"predicted_behavior_occurred": singleObservation(behaviorValues),
		"predicted_outcome_occurred":  singleObservation(outcomeValues),
	}
}

// singleObservation 单值投影（Python _single_observation）。
func singleObservation(values map[string]bool) string {
	delete(values, "")
	if len(values) == 1 {
		for value := range values {
			return value
		}
	}
	if len(values) > 1 {
		return "mixed"
	}
	return "unknown"
}

// unavailableActivation 不可用激活（Python _unavailable_activation）。
func unavailableActivation(availability string, surfaces map[string]bool, reason string) map[string]any {
	list := []string{}
	for surface := range surfaces {
		if surface != "" {
			list = append(list, surface)
		}
	}
	sort.Strings(list)
	return map[string]any{
		"availability":                 availability,
		"state":                        availability,
		"surfaces":                     list,
		"expected_pair_count":          nil,
		"observed_pre_edit_pair_count": nil,
		"missing_pair_count":           nil,
		"trigger_rate":                 nil,
		"reason":                       reason,
	}
}

// targetRegression 目标回滚（Python _target_regression）。
func targetRegression(candidate map[string]any, targetCaseIDs map[string]bool) map[string]any {
	var regressed map[string]bool
	if raw, ok := candidate["regressed_target_case_ids"].([]any); ok {
		regressed = map[string]bool{}
		for caseID := range caseIDsOf(raw) {
			if targetCaseIDs[caseID] {
				regressed[caseID] = true
			}
		}
	} else {
		sourceScores, sourceOK := candidate["source_case_scores"].(map[string]any)
		candidateScores, candidateOK := candidate["candidate_case_scores"].(map[string]any)
		if !sourceOK || !candidateOK {
			return map[string]any{"availability": "missing_artifact", "regressed_case_count": nil, "rate": nil}
		}
		for caseID := range targetCaseIDs {
			if _, hasSource := sourceScores[caseID]; !hasSource {
				return map[string]any{"availability": "missing_artifact", "regressed_case_count": nil, "rate": nil}
			}
			if _, hasCandidate := candidateScores[caseID]; !hasCandidate {
				return map[string]any{"availability": "missing_artifact", "regressed_case_count": nil, "rate": nil}
			}
		}
		regressed = map[string]bool{}
		for caseID := range targetCaseIDs {
			candidateScore := optionalNumber(candidateScores[caseID])
			sourceScore := optionalNumber(sourceScores[caseID])
			if candidateScore != nil && sourceScore != nil && *candidateScore < *sourceScore {
				regressed[caseID] = true
			}
		}
	}
	var rate any
	if len(targetCaseIDs) > 0 {
		rate = float64(len(regressed)) / float64(len(targetCaseIDs))
	}
	return map[string]any{
		"availability":         "observed",
		"regressed_case_count": len(regressed),
		"rate":                 rate,
	}
}

// verifierSummary 验证器摘要（Python _verifier_summary）。
func verifierSummary(candidate map[string]any) map[string]any {
	deltas, ok := candidate["verifier_deltas_by_case"].(map[string]any)
	if !ok || len(deltas) == 0 {
		return map[string]any{"availability": "missing_artifact", "case_count": 0}
	}
	listFields := []string{
		"newly_passed_requirements", "remaining_failed_requirements", "regressed_requirements",
		"newly_passed_fail_to_pass", "remaining_failed_fail_to_pass", "regressed_fail_to_pass",
		"regressed_pass_to_pass", "newly_passed_atomic_checks", "remaining_failed_atomic_checks",
		"regressed_atomic_checks",
	}
	summary := map[string]int{}
	for _, field := range listFields {
		summary[field] = 0
	}
	partialProgressCases := 0
	observedCases := 0
	for _, raw := range deltas {
		delta, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		observedCases++
		if partial, _ := delta["partial_progress"].(bool); partial {
			partialProgressCases++
		}
		for _, field := range listFields {
			if items, ok := delta[field].([]any); ok {
				summary[field] += len(items)
			}
		}
	}
	result := map[string]any{
		"availability":                "observed",
		"case_count":                  observedCases,
		"partial_progress_case_count": partialProgressCases,
	}
	for _, field := range listFields {
		result[field+"_count"] = summary[field]
	}
	return result
}

// evidenceRefs 证据引用别名解析（Python _evidence_refs）。
func evidenceRefs(candidate map[string]any) map[string]string {
	aliases := map[string][]string{
		"source_eval_ref":         {"source_eval_ref", "source_eval_ref_path", "paired_source_eval_ref_path"},
		"candidate_eval_ref":      {"candidate_eval_ref", "candidate_eval_ref_path"},
		"candidate_harness_ref":   {"candidate_harness_ref", "candidate_harness_refs_path"},
		"member_optimization_ref": {"member_optimization_ref", "member_optimization_ref_path"},
		"failure_analysis_ref":    {"failure_analysis_ref", "candidate_failure_analysis_ref_path"},
	}
	refs := map[string]string{}
	outputKeys := make([]string, 0, len(aliases))
	for key := range aliases {
		outputKeys = append(outputKeys, key)
	}
	sort.Strings(outputKeys)
	for _, outputKey := range outputKeys {
		if value := firstTextOf(candidate, aliases[outputKey]...); value != "" {
			refs[outputKey] = value
		}
	}
	return refs
}

// searchMetricUnavailableReason 搜索指标不可用原因（Python _search_metric_unavailable_reason）。
func searchMetricUnavailableReason(
	parentHarnessRef, sourceEvalRef string,
	targetCaseIDs map[string]bool,
	evaluationProtocol string,
	rankFrozen bool,
	rankingPolicy string,
	candidates []map[string]any,
) string {
	if len(candidates) < 2 {
		return "requires_at_least_two_comparable_candidates"
	}
	type requiredField struct {
		name  string
		value any
	}
	required := []requiredField{
		{"parent_harness_ref", parentHarnessRef},
		{"source_eval_ref", sourceEvalRef},
		{"target_case_ids", targetCaseIDs},
		{"evaluation_protocol", evaluationProtocol},
	}
	var missing []string
	for _, field := range required {
		switch v := field.value.(type) {
		case string:
			if v == "" {
				missing = append(missing, field.name)
			}
		case map[string]bool:
			if len(v) == 0 {
				missing = append(missing, field.name)
			}
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return "missing_cohort_field:" + strings.Join(missing, ",")
	}
	if !rankFrozen {
		return "rank_not_frozen_before_evaluation"
	}
	if rankingPolicy == "" {
		return "ranking_policy_missing"
	}

	candidateIDs := make([]string, 0, len(candidates))
	for _, item := range candidates {
		candidateIDs = append(candidateIDs, toString(item["candidate_id"]))
	}
	seen := map[string]bool{}
	for _, id := range candidateIDs {
		if seen[id] {
			return "duplicate_candidate_id"
		}
		seen[id] = true
	}
	rankSet := map[float64]bool{}
	for _, item := range candidates {
		rank := proposalRankValue(item)["predicted_rank"]
		if rank == nil {
			return "predicted_ranks_are_not_a_frozen_permutation"
		}
		switch v := rank.(type) {
		case *float64:
			rankSet[*v] = true
		case float64:
			rankSet[v] = true
		}
	}
	for expected := 1; expected <= len(candidates); expected++ {
		if !rankSet[float64(expected)] {
			return "predicted_ranks_are_not_a_frozen_permutation"
		}
	}
	if len(rankSet) != len(candidates) {
		return "predicted_ranks_are_not_a_frozen_permutation"
	}
	sourceScores := map[float64]bool{}
	hasNilSource := false
	for _, item := range candidates {
		identity := item["identity"].(map[string]any)
		if identity["parent_harness_ref"] != parentHarnessRef {
			return "parent_harness_mismatch"
		}
		if identity["source_eval_ref"] != sourceEvalRef {
			return "source_evaluation_mismatch"
		}
		identityTargets := map[string]bool{}
		switch rawTargets := identity["target_case_ids"].(type) {
		case []any:
			for _, raw := range rawTargets {
				identityTargets[toString(raw)] = true
			}
		case []string:
			for _, raw := range rawTargets {
				identityTargets[raw] = true
			}
		}
		if !sameKeysBB(identityTargets, targetCaseIDs) {
			return "target_case_set_mismatch"
		}
		if identity["evaluation_protocol"] != evaluationProtocol {
			return "evaluation_protocol_mismatch"
		}
		rank := proposalRankValue(item)
		if rank["rank_frozen"] != true {
			return "candidate_rank_not_frozen_before_evaluation"
		}
		if rank["ranking_policy"] != rankingPolicy {
			return "candidate_ranking_policy_mismatch"
		}
		outcome := item["outcome"].(map[string]any)
		if outcome["target_gain"] == nil {
			return "target_gain_missing"
		}
		sourceScore := optionalNumber(outcome["source_target_score"])
		if sourceScore == nil {
			hasNilSource = true
		} else {
			sourceScores[*sourceScore] = true
		}
	}
	if hasNilSource {
		return "source_target_score_missing"
	}
	if len(sourceScores) != 1 {
		return "source_target_score_mismatch"
	}
	return ""
}

// searchMetrics 搜索指标（Python _search_metrics）。
func searchMetrics(candidates []map[string]any, topM int, unavailableReason string) map[string]any {
	if unavailableReason != "" {
		unavailable := unavailableMetric(unavailableReason)
		bestOfK := map[string]any{}
		for key, value := range unavailable {
			bestOfK[key] = value
		}
		bestOfK["k"] = len(candidates)
		topMGain := map[string]any{}
		for key, value := range unavailable {
			topMGain[key] = value
		}
		topMGain["requested_m"] = topM
		topMGain["effective_m"] = nil
		selectionRegret := map[string]any{}
		for key, value := range unavailable {
			selectionRegret[key] = value
		}
		return map[string]any{
			"best_of_k_gain":   bestOfK,
			"top_m_gain":       topMGain,
			"selection_regret": selectionRegret,
		}
	}

	gainOf := func(item map[string]any) float64 { return *optionalNumber(item["outcome"].(map[string]any)["target_gain"]) }
	rankOf := func(item map[string]any) float64 {
		rank := proposalRankValue(item)["predicted_rank"]
		switch v := rank.(type) {
		case *float64:
			return *v
		case float64:
			return v
		}
		return 0
	}
	byGain := append([]map[string]any{}, candidates...)
	sort.SliceStable(byGain, func(i, j int) bool {
		a, b := byGain[i], byGain[j]
		if gainOf(a) != gainOf(b) {
			return gainOf(a) > gainOf(b)
		}
		if rankOf(a) != rankOf(b) {
			return rankOf(a) < rankOf(b)
		}
		return a["candidate_index"].(int) < b["candidate_index"].(int)
	})
	best := byGain[0]
	effectiveM := topM
	if len(candidates) < effectiveM {
		effectiveM = len(candidates)
	}
	var topCandidates []map[string]any
	for _, item := range candidates {
		if rankOf(item) <= float64(effectiveM) {
			topCandidates = append(topCandidates, item)
		}
	}
	sort.SliceStable(topCandidates, func(i, j int) bool {
		a, b := topCandidates[i], topCandidates[j]
		if gainOf(a) != gainOf(b) {
			return gainOf(a) > gainOf(b)
		}
		if rankOf(a) != rankOf(b) {
			return rankOf(a) < rankOf(b)
		}
		return a["candidate_index"].(int) < b["candidate_index"].(int)
	})
	topBest := topCandidates[0]
	var predictedTop1 map[string]any
	for _, item := range candidates {
		if rankOf(item) == 1 {
			predictedTop1 = item
			break
		}
	}
	return map[string]any{
		"best_of_k_gain": map[string]any{
			"status":       "available",
			"value":        gainOf(best),
			"candidate_id": best["candidate_id"],
			"k":            len(candidates),
		},
		"top_m_gain": map[string]any{
			"status":       "available",
			"value":        gainOf(topBest),
			"candidate_id": topBest["candidate_id"],
			"requested_m":  topM,
			"effective_m":  effectiveM,
		},
		"selection_regret": map[string]any{
			"status":                      "available",
			"value":                       gainOf(best) - gainOf(predictedTop1),
			"best_candidate_id":           best["candidate_id"],
			"predicted_top1_candidate_id": predictedTop1["candidate_id"],
		},
	}
}

// unavailableMetric 不可用指标（Python _unavailable_metric）。
func unavailableMetric(reason string) map[string]any {
	return map[string]any{"status": "unavailable", "value": nil, "reason": reason}
}

// proposalTargetCaseIDs 提案目标用例并集（Python _proposal_target_case_ids）。
func proposalTargetCaseIDs(proposal map[string]any, capabilities []map[string]any) map[string]bool {
	targetCaseIDs := caseIDsOf(proposal["target_case_ids"])
	for _, capability := range capabilities {
		for caseID := range caseIDsOf(capability["target_case_ids"]) {
			targetCaseIDs[caseID] = true
		}
	}
	return targetCaseIDs
}

// isExecutable 可执行判定（Python _is_executable）。
func isExecutable(proposal map[string]any, capabilities []map[string]any) bool {
	for _, key := range []string{"is_executable", "executable"} {
		if value, ok := proposal[key].(bool); ok {
			return value
		}
	}
	status := normalizeText(proposal["proposal_status"])
	if status == "" {
		status = normalizeText(proposal["status"])
	}
	if nonExecutableStatuses[status] {
		return false
	}
	if len(capabilities) == 0 {
		return false
	}
	for _, capability := range capabilities {
		if text(capability["action_group"]) == "" || text(capability["operation"]) == "" {
			return false
		}
	}
	return true
}

// capabilitiesOf 能力清单复制（Python _capabilities）。
func capabilitiesOf(value map[string]any) []map[string]any {
	raw, ok := value["capabilities"].([]any)
	if !ok {
		return nil
	}
	capabilities := []map[string]any{}
	for _, item := range raw {
		if m, isMap := item.(map[string]any); isMap {
			cloned := map[string]any{}
			for key, v := range m {
				cloned[key] = v
			}
			capabilities = append(capabilities, cloned)
		}
	}
	return capabilities
}

// candidateIndexOf 候选下标（Python _candidate_index）。
func candidateIndexOf(candidate map[string]any, fallback int) int {
	switch value := candidate["candidate_index"].(type) {
	case int:
		if value >= 0 {
			return value
		}
	case int64:
		if value >= 0 {
			return int(value)
		}
	case float64:
		if value >= 0 && value == float64(int(value)) {
			return int(value)
		}
	case string:
		if trimmed := strings.TrimSpace(value); isAllDigits(trimmed) {
			parsed, err := strconv.Atoi(trimmed)
			if err == nil {
				return parsed
			}
		}
	}
	return fallback
}

// candidateID 候选标识（Python _candidate_id）。
func candidateID(candidate map[string]any, candidateIndex int) string {
	if id := text(candidate["candidate_id"]); id != "" {
		return id
	}
	return fmt.Sprintf("candidate_%03d", candidateIndex)
}

// positiveInt 正整数投影（Python _positive_int）。
func positiveInt(value any) *float64 {
	switch v := value.(type) {
	case int:
		if v > 0 {
			f := float64(v)
			return &f
		}
	case int64:
		if v > 0 {
			f := float64(v)
			return &f
		}
	case float64:
		if v > 0 && v == float64(int(v)) {
			return &v
		}
	case string:
		if trimmed := strings.TrimSpace(v); isAllDigits(trimmed) {
			parsed, err := strconv.Atoi(trimmed)
			if err == nil && parsed > 0 {
				f := float64(parsed)
				return &f
			}
		}
	}
	return nil
}

// caseIDsOf 字符串集合投影（Python _case_ids）。
func caseIDsOf(value any) map[string]bool {
	out := map[string]bool{}
	list, ok := value.([]any)
	if !ok {
		return out
	}
	for _, item := range list {
		if text(item) != "" {
			out[text(item)] = true
		}
	}
	return out
}

// firstTextOf 依次取首个非空文本（Python _first_text）。
func firstTextOf(value map[string]any, keys ...string) string {
	for _, key := range keys {
		if t := text(value[key]); t != "" {
			return t
		}
	}
	return ""
}

// runtimeNamesMatch 运行时名匹配（Python _runtime_names_match）。
func runtimeNamesMatch(actionGroup, planned, observed string) bool {
	plannedName := strings.ReplaceAll(normalizeText(planned), "-", "_")
	observedName := strings.ReplaceAll(normalizeText(observed), "-", "_")
	if actionGroup == "tool" {
		plannedName = strings.TrimSuffix(plannedName, "_tool")
		observedName = strings.TrimSuffix(observedName, "_tool")
	}
	return plannedName != "" && plannedName == observedName
}

// cohortIDOf cohort 标识（Python _cohort_id）。
func cohortIDOf(parentHarnessRef, sourceEvalRef string, targetCaseIDs map[string]bool, evaluationProtocol string) string {
	payload := feedbackCanonicalJSON(map[string]any{
		"parent_harness_ref":  parentHarnessRef,
		"source_eval_ref":     sourceEvalRef,
		"target_case_ids":     sortedKeysBool(targetCaseIDs),
		"evaluation_protocol": evaluationProtocol,
	})
	sum := sha256.Sum256([]byte(payload))
	return "cohort_" + hex.EncodeToString(sum[:])[:16]
}

// feedbackCanonicalJSON 规范 JSON：键排序、紧凑、非 ASCII 不转义（Python _canonical_json）。
func feedbackCanonicalJSON(value any) string {
	encoded, err := marshalCompactJSON(value)
	if err != nil {
		return fmt.Sprint(value)
	}
	return encoded
}

// normalizeText NFKC + casefold + 空白折叠（Python _normalize_text）。
func normalizeText(value any) string {
	normalized := strings.ToLower(strings.TrimSpace(yamlText(value)))
	return whitespacePattern.ReplaceAllString(normalized, " ")
}

// text 文本投影（Python _text）。
func text(value any) string {
	if s, ok := value.(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}

// yamlText 任意值文本化（NFKC 输入预处理）。
func yamlText(value any) string {
	if s, ok := value.(string); ok {
		return nfkcNormalize(s)
	}
	return ""
}

// proposalRankValue 取 proposal_rank 映射。
func proposalRankValue(item map[string]any) map[string]any {
	rank, _ := item["proposal_rank"].(map[string]any)
	if rank == nil {
		return map[string]any{}
	}
	return rank
}

// diagnosisItems 诊断值归一为映射列表。
func diagnosisItems(raw any) []map[string]any {
	switch v := raw.(type) {
	case []any:
		items := []map[string]any{}
		for _, item := range v {
			if m, ok := item.(map[string]any); ok {
				items = append(items, m)
			}
		}
		return items
	case map[string]any:
		return []map[string]any{v}
	}
	return nil
}

// observedOrNot observed/not_instrumented。
func observedOrNot(observed bool) string {
	if observed {
		return "observed"
	}
	return "not_instrumented"
}

// sortedKeysBool 键排序列表。
func sortedKeysBool(source map[string]bool) []string {
	keys := make([]string, 0, len(source))
	for key := range source {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// sortedIntersection 交集排序列表。
func sortedIntersection(a, b map[string]bool) []string {
	out := []string{}
	for key := range a {
		if b[key] {
			out = append(out, key)
		}
	}
	sort.Strings(out)
	return out
}

// setsIntersect 集合相交判断。
func setsIntersect(a, b map[string]bool) bool {
	for key := range a {
		if b[key] {
			return true
		}
	}
	return false
}

// roundFloat 四舍五入到指定位。
func roundFloat(value float64, places int) float64 {
	shift := math.Pow(10, float64(places))
	return math.Round(value*shift) / shift
}

// isAllDigits 纯数字判断。
func isAllDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// nfkcNormalizer NFKC 规范化器。
var nfkcNormalizer = norm.NFKC

// marshalCompactJSON 紧凑 JSON（键排序由 encoding/json map 语义保证，非 ASCII 不转义）。
func marshalCompactJSON(value any) (string, error) {
	var buffer strings.Builder
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return "", err
	}
	return strings.TrimSuffix(buffer.String(), "\n"), nil
}

// nfkcNormalize NFKC 规范化。
func nfkcNormalize(value string) string {
	return nfkcNormalizer.String(value)
}
