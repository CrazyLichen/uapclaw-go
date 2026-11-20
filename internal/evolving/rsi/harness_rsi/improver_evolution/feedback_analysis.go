// Package improver_evolution 中 feedback_analysis 部分的确定性复盘分析：
// 校验并汇总一份或多份 Candidate Feedback Ledger v1 输入，产出候选反馈复盘报告。
//
// 对应 Python 代码：openjiuwen/rsi/harness_rsi/improver_evolution/feedback_analysis.py
package improver_evolution

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// ──────────────────────────── 结构体 ────────────────────────────

// surfaceEvidence 单个触发面的触发/回归证据累积器
// （对应 Python class _SurfaceEvidence L234-289）
type surfaceEvidence struct {
	// triggerAvailability 触发可用性计数
	triggerAvailability map[string]int
	// triggerOpportunityCandidates 触发机会候选数（observed）
	triggerOpportunityCandidates int
	// triggerFailureCandidates 触发失败候选数（observed 且 not_triggered）
	triggerFailureCandidates int
	// triggerOpportunityCohorts 触发机会 cohort 集合
	triggerOpportunityCohorts map[string]bool
	// triggerFailureCohorts 触发失败 cohort 集合
	triggerFailureCohorts map[string]bool
	// regressionAvailability 回归可用性计数
	regressionAvailability map[string]int
	// regressionOpportunityCandidates 回归机会候选数（observed）
	regressionOpportunityCandidates int
	// regressionFailureCandidates 回归失败候选数（observed 且退步数>0）
	regressionFailureCandidates int
	// regressionOpportunityCohorts 回归机会 cohort 集合
	regressionOpportunityCohorts map[string]bool
	// regressionFailureCohorts 回归失败 cohort 集合
	regressionFailureCohorts map[string]bool
}

// newSurfaceEvidence 构造零值累积器
func newSurfaceEvidence() *surfaceEvidence {
	return &surfaceEvidence{
		triggerAvailability:          map[string]int{},
		triggerOpportunityCohorts:    map[string]bool{},
		triggerFailureCohorts:        map[string]bool{},
		regressionAvailability:       map[string]int{},
		regressionOpportunityCohorts: map[string]bool{},
		regressionFailureCohorts:     map[string]bool{},
	}
}

// observeTrigger 记录一次候选激活观察（对应 Python observe_trigger L247-256）
func (e *surfaceEvidence) observeTrigger(cohortID string, activation map[string]any) {
	availability, _ := activation["availability"].(string)
	e.triggerAvailability[availability]++
	if availability != "observed" {
		return
	}
	e.triggerOpportunityCandidates++
	e.triggerOpportunityCohorts[cohortID] = true
	if state, _ := activation["state"].(string); state == "not_triggered" {
		e.triggerFailureCandidates++
		e.triggerFailureCohorts[cohortID] = true
	}
}

// observeRegression 记录一次候选回归观察（对应 Python observe_regression L258-267）
func (e *surfaceEvidence) observeRegression(cohortID string, regression map[string]any) {
	availability, _ := regression["availability"].(string)
	e.regressionAvailability[availability]++
	if availability != "observed" {
		return
	}
	e.regressionOpportunityCandidates++
	e.regressionOpportunityCohorts[cohortID] = true
	count := 0
	if v, ok := regression["regressed_case_count"]; ok {
		if n, isInt := v.(int); isInt {
			count = n
		} else if n, ok := v.(int64); ok {
			count = int(n)
		} else if f, ok := v.(float64); ok {
			count = int(f)
		}
	}
	if count > 0 {
		e.regressionFailureCandidates++
		e.regressionFailureCohorts[cohortID] = true
	}
}

// triggerSummary 触发侧汇总（对应 Python trigger_summary L269-278）
func (e *surfaceEvidence) triggerSummary() map[string]any {
	return map[string]any{
		"availability_counts":                  counterDict(e.triggerAvailability),
		"observed_opportunity_candidate_count": e.triggerOpportunityCandidates,
		"failure_candidate_count":              e.triggerFailureCandidates,
		"rate":                                 rate(e.triggerFailureCandidates, e.triggerOpportunityCandidates),
		"opportunity_cohort_count":             len(e.triggerOpportunityCohorts),
		"support_cohort_count":                 len(e.triggerFailureCohorts),
		"evidence_cohort_ids":                  sortedSetKeys(e.triggerFailureCohorts),
	}
}

// regressionSummary 回归侧汇总（对应 Python regression_summary L280-289）
func (e *surfaceEvidence) regressionSummary() map[string]any {
	return map[string]any{
		"availability_counts":                  counterDict(e.regressionAvailability),
		"observed_opportunity_candidate_count": e.regressionOpportunityCandidates,
		"regression_candidate_count":           e.regressionFailureCandidates,
		"rate":                                 rate(e.regressionFailureCandidates, e.regressionOpportunityCandidates),
		"opportunity_cohort_count":             len(e.regressionOpportunityCohorts),
		"support_cohort_count":                 len(e.regressionFailureCohorts),
		"evidence_cohort_ids":                  sortedSetKeys(e.regressionFailureCohorts),
	}
}

// featureEvidence 单个排序特征的对齐证据累积器
// （对应 Python class _FeatureEvidence L292-342）
type featureEvidence struct {
	// comparablePairCount 可比较特征对数
	comparablePairCount int
	// comparableCohorts 可比较 cohort 集合
	comparableCohorts map[string]bool
	// alignedPairCount 预测与实际赢家一致的对数
	alignedPairCount int
	// alignedCohorts 一致 cohort 集合
	alignedCohorts map[string]bool
	// oppositePairCount 不一致的对数
	oppositePairCount int
	// oppositeCohorts 不一致 cohort 集合
	oppositeCohorts map[string]bool
	// increasePairCount 建议 increase 权重的对数
	increasePairCount int
	// increaseCohorts 建议 increase 的 cohort 集合
	increaseCohorts map[string]bool
	// decreasePairCount 建议 decrease 权重的对数
	decreasePairCount int
	// decreaseCohorts 建议 decrease 的 cohort 集合
	decreaseCohorts map[string]bool
}

// newFeatureEvidence 构造零值累积器
func newFeatureEvidence() *featureEvidence {
	return &featureEvidence{
		comparableCohorts: map[string]bool{},
		alignedCohorts:    map[string]bool{},
		oppositeCohorts:   map[string]bool{},
		increaseCohorts:   map[string]bool{},
		decreaseCohorts:   map[string]bool{},
	}
}

// observe 记录一次特征对观察（对应 Python observe L305-319）
func (e *featureEvidence) observe(cohortID string, aligned bool, adjustment string) {
	e.comparablePairCount++
	e.comparableCohorts[cohortID] = true
	if aligned {
		e.alignedPairCount++
		e.alignedCohorts[cohortID] = true
		return
	}
	e.oppositePairCount++
	e.oppositeCohorts[cohortID] = true
	switch adjustment {
	case "increase":
		e.increasePairCount++
		e.increaseCohorts[cohortID] = true
	case "decrease":
		e.decreasePairCount++
		e.decreaseCohorts[cohortID] = true
	}
}

// summary 特征证据汇总（对应 Python summary L321-342）
func (e *featureEvidence) summary() map[string]any {
	return map[string]any{
		"comparable_pair_count":   e.comparablePairCount,
		"comparable_cohort_count": len(e.comparableCohorts),
		"comparable_cohort_ids":   sortedSetKeys(e.comparableCohorts),
		"aligned_pair_count":      e.alignedPairCount,
		"aligned_cohort_count":    len(e.alignedCohorts),
		"aligned_cohort_ids":      sortedSetKeys(e.alignedCohorts),
		"opposite_pair_count":     e.oppositePairCount,
		"opposite_cohort_count":   len(e.oppositeCohorts),
		"opposite_cohort_ids":     sortedSetKeys(e.oppositeCohorts),
		"increase_weight_evidence": map[string]any{
			"pair_count":   e.increasePairCount,
			"cohort_count": len(e.increaseCohorts),
			"cohort_ids":   sortedSetKeys(e.increaseCohorts),
		},
		"decrease_weight_evidence": map[string]any{
			"pair_count":   e.decreasePairCount,
			"cohort_count": len(e.decreaseCohorts),
			"cohort_ids":   sortedSetKeys(e.decreaseCohorts),
		},
	}
}

// ──────────────────────────── 枚举 ────────────────────────────

// LedgerSource 账本输入源：内存 ledger/cohort 映射（map[string]any）或 YAML/JSON 文件路径（string）。
// 对应 Python TypeAlias `Mapping[str, Any] | str | Path`（偏差 D-e1-1：Go 无 Path 类型，路径即 string）。
type LedgerSource = any

// ──────────────────────────── 常量 ────────────────────────────

// _LEDGER_TYPE 账本级 record_type 字面量
const _LEDGER_TYPE = "single_harness_sibling_candidate_feedback"

// _COHORT_TYPE cohort 级 record_type 字面量
const _COHORT_TYPE = "candidate_feedback_cohort"

// _METRIC_NAMES 三个汇总指标名（顺序照抄 Python）
var _METRIC_NAMES = []string{"best_of_k_gain", "top_m_gain", "selection_regret"}

// _METRIC_STATUSES 指标状态合法取值
var _METRIC_STATUSES = map[string]bool{"available": true, "unavailable": true}

// _ACTIVATION_AVAILABILITIES 激活可用性合法取值
var _ACTIVATION_AVAILABILITIES = map[string]bool{
	"missing_artifact": true,
	"not_applicable":   true,
	"not_instrumented": true,
	"observed":         true,
}

// _ACTIVATION_STATES 激活状态合法取值
var _ACTIVATION_STATES = map[string]bool{
	"missing_artifact": true,
	"not_applicable":   true,
	"not_instrumented": true,
	"not_triggered":    true,
	"triggered":        true,
}

// _REGRESSION_AVAILABILITIES 回归可用性合法取值
var _REGRESSION_AVAILABILITIES = map[string]bool{
	"missing_artifact": true,
	"not_evaluated":    true,
	"not_instrumented": true,
	"observed":         true,
}

// ──────────────────────────── 导出函数 ────────────────────────────
// （AnalyzeCandidateFeedbackLedgers 于 digest 主函数阶段加入）

// ──────────────────────────── 非导出函数 ────────────────────────────
// AnalyzeCandidateFeedbackLedgers 校验并汇总一份或多份 Candidate Feedback Ledger v1 输入
// （对应 Python analyze_candidate_feedback_ledgers L45-231，步骤逐条对应）。
// Python 的 bool/非有限数参数守卫在 Go 强类型下不可达（D-e1-6），仅保留 min_support_cohorts 下界检查。
func AnalyzeCandidateFeedbackLedgers(
	sources []LedgerSource,
	minSupportCohorts int,
	highValueGainThreshold float64,
) (map[string]any, error) {
	if minSupportCohorts < 1 {
		return nil, errNew("min_support_cohorts must be at least 1")
	}
	normalizedSources, err := normalizeSources(sources)
	if err != nil {
		return nil, err
	}
	cohorts := make([]map[string]any, 0)
	for sourceIndex, source := range normalizedSources {
		loaded, err := loadAndValidateSource(source, sourceIndex)
		if err != nil {
			return nil, err
		}
		cohorts = append(cohorts, loaded...)
	}
	if err := requireUniqueCohortIDs(cohorts); err != nil {
		return nil, err
	}

	comparableIDs := make([]string, 0)
	nonComparableReasons := map[string]int{}
	nonComparableIDsByReason := map[string][]string{}
	metricAvailability := map[string]map[string]any{}
	for _, name := range _METRIC_NAMES {
		metricAvailability[name] = map[string]any{
			"available_count":     0,
			"unavailable_count":   0,
			"unavailable_reasons": map[string]int{},
		}
	}
	nonpositiveIDs := make([]string, 0)
	highValueOpportunityIDs := make([]string, 0)
	highValueOutsideIDs := make([]string, 0)
	duplicateCandidateCount := 0
	duplicateOpportunityCount := 0
	duplicateCohortIDs := make([]string, 0)
	triggerBySurface := map[string]*surfaceEvidence{}
	regressionBySurface := map[string]*surfaceEvidence{}
	featureEvidence := map[string]*featureEvidence{}
	positiveSelectionRegretIDs := make([]string, 0)
	partialRepairOpportunityIDs := map[string]bool{}
	partialRepairSupportIDs := map[string]bool{}

	for _, cohort := range cohorts {
		cohortID := cohort["cohort"].(map[string]any)["cohort_id"].(string)
		reason := comparabilityReason(cohort)
		if reason != "" {
			nonComparableReasons[reason]++
			nonComparableIDsByReason[reason] = append(nonComparableIDsByReason[reason], cohortID)
		} else {
			comparableIDs = append(comparableIDs, cohortID)
			gains := make([]float64, 0, len(cohort["candidates"].([]any)))
			for _, item := range cohort["candidates"].([]any) {
				gains = append(gains, pyFloat(item.(map[string]any)["outcome"].(map[string]any)["target_gain"]))
			}
			if len(gains) > 0 && maxFloat(gains) <= 0.0 {
				nonpositiveIDs = append(nonpositiveIDs, cohortID)
			}
			if err := collectHighValueOutsideTopM(cohort, highValueGainThreshold,
				&highValueOpportunityIDs, &highValueOutsideIDs); err != nil {
				return nil, err
			}
			collectFeatureEvidence(cohort, featureEvidence)
		}

		selectionRegret := cohort["metrics"].(map[string]any)["selection_regret"].(map[string]any)
		if selectionRegret["status"] == "available" && pyFloat(selectionRegret["value"]) > 0.0 {
			positiveSelectionRegretIDs = append(positiveSelectionRegretIDs, cohortID)
		}

		for _, metricName := range _METRIC_NAMES {
			metric := cohort["metrics"].(map[string]any)[metricName].(map[string]any)
			if metric["status"] == "available" {
				metricAvailability[metricName]["available_count"] = metricAvailability[metricName]["available_count"].(int) + 1
			} else {
				metricAvailability[metricName]["unavailable_count"] = metricAvailability[metricName]["unavailable_count"].(int) + 1
				reasons := metricAvailability[metricName]["unavailable_reasons"].(map[string]int)
				reasons[metric["reason"].(string)]++
			}
		}

		candidateCount, cohortDuplicateCount := cohortDuplicateCounts(cohort)
		duplicateOpportunityCount += candidateCount
		duplicateCandidateCount += cohortDuplicateCount
		if cohortDuplicateCount > 0 {
			duplicateCohortIDs = append(duplicateCohortIDs, cohortID)
		}

		for _, item := range cohort["candidates"].([]any) {
			candidate := item.(map[string]any)
			surfaces := candidate["activation"].(map[string]any)["surfaces"].([]any)
			for _, surfaceItem := range surfaces {
				surface := surfaceItem.(string)
				trigger, present := triggerBySurface[surface]
				if !present {
					trigger = newSurfaceEvidence()
					triggerBySurface[surface] = trigger
				}
				trigger.observeTrigger(cohortID, candidate["activation"].(map[string]any))
				regression, present := regressionBySurface[surface]
				if !present {
					regression = newSurfaceEvidence()
					regressionBySurface[surface] = regression
				}
				regression.observeRegression(cohortID, candidate["regression"].(map[string]any)["target"].(map[string]any))
			}
			// 对应 Python verifier_summary 判定块 L131-145
			verifier, ok := candidate["verifier_summary"].(map[string]any)
			if !ok || verifier["availability"] != "observed" {
				continue
			}
			remaining, hasRemaining := verifier["remaining_failed_requirements_count"]
			newlyPassed, hasNewly := verifier["newly_passed_requirements_count"]
			regressed, hasRegressed := verifier["regressed_requirements_count"]
			partialProgressCases, hasPartial := verifier["partial_progress_case_count"]
			if !(hasRemaining && isNonnegativeInt(remaining) && pyInt(remaining) > 0) {
				continue
			}
			partialRepairOpportunityIDs[cohortID] = true
			newlyPassedObserved := hasNewly && isNonnegativeInt(newlyPassed) && pyInt(newlyPassed) > 0
			noRegression := hasRegressed && isNonnegativeInt(regressed) && pyInt(regressed) == 0
			partialProgressObserved := hasPartial && isNonnegativeInt(partialProgressCases) && pyInt(partialProgressCases) > 0
			if newlyPassedObserved && noRegression && partialProgressObserved {
				partialRepairSupportIDs[cohortID] = true
			}
		}
	}

	// 对应 Python duplicate_opportunity_cohorts=[cohort_id for all cohorts]
	duplicateOpportunityCohorts := make([]string, 0, len(cohorts))
	for _, cohort := range cohorts {
		duplicateOpportunityCohorts = append(duplicateOpportunityCohorts,
			cohort["cohort"].(map[string]any)["cohort_id"].(string))
	}

	stablePatternsList := stablePatterns(
		minSupportCohorts,
		comparableIDs,
		nonpositiveIDs,
		highValueOpportunityIDs,
		highValueOutsideIDs,
		duplicateOpportunityCohorts,
		duplicateCohortIDs,
		triggerBySurface,
		regressionBySurface,
		featureEvidence,
		positiveSelectionRegretIDs,
		sortedSetKeys(partialRepairOpportunityIDs),
		sortedSetKeys(partialRepairSupportIDs),
	)

	digest, err := trainingLedgerDigest(cohorts)
	if err != nil {
		return nil, err
	}

	candidateCount := 0
	for _, cohort := range cohorts {
		candidateCount += len(cohort["candidates"].([]any))
	}
	selectionRegretAvailableIDs := make([]string, 0)
	for _, cohort := range cohorts {
		metric := cohort["metrics"].(map[string]any)["selection_regret"].(map[string]any)
		if metric["status"] == "available" {
			selectionRegretAvailableIDs = append(selectionRegretAvailableIDs,
				cohort["cohort"].(map[string]any)["cohort_id"].(string))
		}
	}
	nonComparableReasonsDict := counterDict(nonComparableReasons)
	nonComparableIDsByReasonSorted := map[string][]string{}
	for reason, ids := range nonComparableIDsByReason {
		nonComparableIDsByReasonSorted[reason] = sortedSetKeys(setOf(ids))
	}
	surfacesOut := map[string]any{}
	surfaceKeys := make([]string, 0, len(triggerBySurface))
	for surface := range triggerBySurface {
		surfaceKeys = append(surfaceKeys, surface)
	}
	sort.Strings(surfaceKeys)
	for _, surface := range surfaceKeys {
		surfacesOut[surface] = map[string]any{
			"trigger_observed_failure": triggerBySurface[surface].triggerSummary(),
			"target_regression":        regressionBySurface[surface].regressionSummary(),
		}
	}
	featureOut := map[string]any{}
	featureKeys := make([]string, 0, len(featureEvidence))
	for feature := range featureEvidence {
		featureKeys = append(featureKeys, feature)
	}
	sort.Strings(featureKeys)
	for _, feature := range featureKeys {
		featureOut[feature] = featureEvidence[feature].summary()
	}

	return map[string]any{
		"schema_version":         1,
		"analysis_type":          "candidate_feedback_analysis",
		"training_ledger_digest": digest,
		"parameters": map[string]any{
			"min_support_cohorts":       minSupportCohorts,
			"high_value_gain_threshold": highValueGainThreshold,
		},
		"input_summary": map[string]any{
			"ledger_count":    len(normalizedSources),
			"cohort_count":    len(cohorts),
			"candidate_count": candidateCount,
		},
		"comparability": map[string]any{
			"comparable_cohort_count":             len(comparableIDs),
			"comparable_cohort_ids":               sortedSetKeys(setOf(comparableIDs)),
			"non_comparable_cohort_count":         len(cohorts) - len(comparableIDs),
			"non_comparable_reasons":              nonComparableReasonsDict,
			"non_comparable_cohort_ids_by_reason": nonComparableIDsByReasonSorted,
		},
		"metric_availability": metricAvailabilityOut(metricAvailability),
		"outcomes": map[string]any{
			"all_candidates_nonpositive":         cohortRate(nonpositiveIDs, comparableIDs),
			"high_value_candidate_outside_top_m": cohortRate(highValueOutsideIDs, highValueOpportunityIDs),
			"positive_selection_regret":          cohortRate(positiveSelectionRegretIDs, selectionRegretAvailableIDs),
			"partial_candidate_residual_repair":  cohortRate(sortedSetKeys(partialRepairSupportIDs), sortedSetKeys(partialRepairOpportunityIDs)),
		},
		"duplicates": map[string]any{
			"availability":                "observed",
			"duplicate_candidate_count":   duplicateCandidateCount,
			"opportunity_candidate_count": duplicateOpportunityCount,
			"rate":                        rate(duplicateCandidateCount, duplicateOpportunityCount),
			"support_cohort_count":        len(setOf(duplicateCohortIDs)),
			"evidence_cohort_ids":         sortedSetKeys(setOf(duplicateCohortIDs)),
		},
		"surfaces":                  surfacesOut,
		"feature_outcome_alignment": featureOut,
		"stable_patterns":           stablePatternsList,
	}, nil
}

// metricAvailabilityOut 指标可用性输出结构（对应 Python L185-192 字典推导）
func metricAvailabilityOut(metricAvailability map[string]map[string]any) map[string]any {
	out := map[string]any{}
	for _, name := range _METRIC_NAMES {
		values := metricAvailability[name]
		out[name] = map[string]any{
			"available_count":     values["available_count"],
			"unavailable_count":   values["unavailable_count"],
			"unavailable_reasons": counterDict(values["unavailable_reasons"].(map[string]int)),
		}
	}
	return out
}

// normalizeSources 归一输入源列表（对应 Python _normalize_sources L345-353）。
// Go 入参已为序列形态，仅校验每个元素是映射或路径字符串。
func normalizeSources(sources []LedgerSource) ([]LedgerSource, error) {
	for _, source := range sources {
		switch source.(type) {
		case map[string]any, string:
		default:
			return nil, errNew("each source must be a ledger mapping or path")
		}
	}
	return sources, nil
}

// loadAndValidateSource 加载并校验单个输入源，返回其 cohort 列表
// （对应 Python _load_and_validate_source L356-371）。
func loadAndValidateSource(source LedgerSource, sourceIndex int) ([]map[string]any, error) {
	value, err := loadSource(source)
	if err != nil {
		return nil, err
	}
	location := fmt.Sprintf("source[%d]", sourceIndex)
	mapping, err := requireMapping(value, location)
	if err != nil {
		return nil, err
	}
	var rawCohorts []any
	if recordType, _ := mapping["record_type"].(string); recordType == _COHORT_TYPE {
		rawCohorts = []any{mapping}
	} else {
		if err := requireExact(mapping["schema_version"], 1, location+".schema_version"); err != nil {
			return nil, err
		}
		if err := requireExact(mapping["ledger_type"], _LEDGER_TYPE, location+".ledger_type"); err != nil {
			return nil, err
		}
		cohortsValue, present := mapping["cohorts"]
		if !present {
			cohortsValue = nil
		}
		rawList, ok := cohortsValue.([]any)
		if !ok {
			return nil, errNew(location + ".cohorts must be a list")
		}
		rawCohorts = rawList
	}
	cohorts := make([]map[string]any, 0, len(rawCohorts))
	for cohortIndex, cohort := range rawCohorts {
		validated, err := validateCohort(cohort, fmt.Sprintf("%s.cohorts[%d]", location, cohortIndex))
		if err != nil {
			return nil, err
		}
		cohorts = append(cohorts, validated)
	}
	return cohorts, nil
}

// loadSource 读取单个输入源（对应 Python _load_source L374-384）。
// 映射原样返回；字符串按文件路径读取，.json 后缀（大小写不敏感）按 JSON 解析，
// 其余按 YAML 解析。JSON 数字用 UseNumber 还原 Python 的 int/float 二分语义。
func loadSource(source LedgerSource) (any, error) {
	switch s := source.(type) {
	case map[string]any:
		// 对应 Python dict(source) 的浅拷贝：仅顶层新建，嵌套共享（本包只读不改，语义等价）
		shallow := make(map[string]any, len(s))
		for key, value := range s {
			shallow[key] = value
		}
		return shallow, nil
	case string:
		info, err := os.Stat(s)
		if err != nil || info.IsDir() {
			return nil, errNew(fmt.Sprintf("candidate feedback ledger does not exist: %s", s))
		}
		text, err := os.ReadFile(s)
		if err != nil {
			return nil, errNew(fmt.Sprintf("failed to read candidate feedback ledger %s: %s", s, err))
		}
		if strings.EqualFold(filepath.Ext(s), ".json") {
			decoded, err := decodeJSONWithInts(text)
			if err != nil {
				return nil, errNew(fmt.Sprintf("failed to read candidate feedback ledger %s: %s", s, err))
			}
			return decoded, nil
		}
		var decoded any
		if err := yamlUnmarshal(text, &decoded); err != nil {
			return nil, errNew(fmt.Sprintf("failed to read candidate feedback ledger %s: %s", s, err))
		}
		return decoded, nil
	default:
		return nil, errNew("sources must be a ledger mapping, path, or sequence of them")
	}
}

// decodeJSONWithInts 按 Python json.loads 的数字语义解码：无小数点/指数的整数还原为 int64，
// 其余为 float64（对应 json.loads 对 int/float 的区分）。
func decodeJSONWithInts(text []byte) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(text))
	decoder.UseNumber()
	var raw any
	if err := decoder.Decode(&raw); err != nil {
		return nil, err
	}
	return convertJSONNumbers(raw), nil
}

// convertJSONNumbers 递归将 json.Number 还原为 int64/float64
func convertJSONNumbers(value any) any {
	switch v := value.(type) {
	case map[string]any:
		for key, item := range v {
			v[key] = convertJSONNumbers(item)
		}
		return v
	case []any:
		for i, item := range v {
			v[i] = convertJSONNumbers(item)
		}
		return v
	case json.Number:
		text := v.String()
		if !strings.ContainsAny(text, ".eE") {
			if n, err := strconv.ParseInt(text, 10, 64); err == nil {
				return n
			}
		}
		f, err := strconv.ParseFloat(text, 64)
		if err != nil {
			return v
		}
		return f
	default:
		return value
	}
}

// yamlUnmarshal yaml.v3 解析（对应 Python yaml.safe_load 的安全语义）
func yamlUnmarshal(text []byte, out *any) error {
	return yaml.Unmarshal(text, out)
}

// validateCohort 校验单个 cohort 记录（对应 Python _validate_cohort L387-412）
func validateCohort(value any, location string) (map[string]any, error) {
	cohort, err := requireMapping(value, location)
	if err != nil {
		return nil, err
	}
	if err := requireExact(cohort["schema_version"], 1, location+".schema_version"); err != nil {
		return nil, err
	}
	if err := requireExact(cohort["record_type"], _COHORT_TYPE, location+".record_type"); err != nil {
		return nil, err
	}
	identity, err := requireMapping(mapValue(cohort, "cohort"), location+".cohort")
	if err != nil {
		return nil, err
	}
	if err := requireNonemptyText(identity["cohort_id"], location+".cohort.cohort_id"); err != nil {
		return nil, err
	}
	candidatesValue, present := cohort["candidates"]
	if !present {
		candidatesValue = nil
	}
	rawCandidates, ok := candidatesValue.([]any)
	if !ok {
		return nil, errNew(location + ".candidates must be a list")
	}
	// 对应 Python identity.get("candidate_count") != len(candidates)
	if !pyEqual(identity["candidate_count"], len(rawCandidates)) {
		return nil, errNew(location + ".cohort.candidate_count must match candidates length")
	}
	for candidateIndex, candidate := range rawCandidates {
		if _, err := validateCandidate(candidate, fmt.Sprintf("%s.candidates[%d]", location, candidateIndex)); err != nil {
			return nil, err
		}
	}
	metrics, err := requireMapping(mapValue(cohort, "metrics"), location+".metrics")
	if err != nil {
		return nil, err
	}
	for _, metricName := range _METRIC_NAMES {
		metric, err := requireMapping(mapValue(metrics, metricName), fmt.Sprintf("%s.metrics.%s", location, metricName))
		if err != nil {
			return nil, err
		}
		status, _ := metric["status"].(string)
		if !_METRIC_STATUSES[status] {
			return nil, errNew(fmt.Sprintf("%s.metrics.%s.status has unsupported value %s",
				location, metricName, pyRepr(metric["status"])))
		}
		if status == "available" {
			if !isNumber(metric["value"]) {
				return nil, errNew(location + fmt.Sprintf(".metrics.%s.value must be finite when available", metricName))
			}
		} else {
			valuePresent := metric["value"] != nil
			if valuePresent {
				return nil, errNew(location + fmt.Sprintf(".metrics.%s.value must be null when unavailable", metricName))
			}
			if err := requireNonemptyText(metric["reason"],
				fmt.Sprintf("%s.metrics.%s.reason", location, metricName)); err != nil {
				return nil, err
			}
		}
	}
	return cohort, nil
}

// validateCandidate 校验单个候选记录（对应 Python _validate_candidate L415-460）
func validateCandidate(value any, location string) (map[string]any, error) {
	candidate, err := requireMapping(value, location)
	if err != nil {
		return nil, err
	}
	if err := requireNonemptyText(candidate["candidate_id"], location+".candidate_id"); err != nil {
		return nil, err
	}
	if err := requireNonemptyText(candidate["candidate_fingerprint"], location+".candidate_fingerprint"); err != nil {
		return nil, err
	}
	identity, err := requireMapping(mapValue(candidate, "identity"), location+".identity")
	if err != nil {
		return nil, err
	}
	for _, field := range []string{"parent_harness_ref", "source_eval_ref", "evaluation_protocol"} {
		if err := requireNonemptyText(identity[field], location+".identity."+field); err != nil {
			return nil, err
		}
	}
	caseIDs, ok := identity["target_case_ids"].([]any)
	if !ok {
		return nil, errNew(location + ".identity.target_case_ids must be a list")
	}
	_ = caseIDs
	rank, err := requireMapping(mapValue(candidate, "proposal_rank"), location+".proposal_rank")
	if err != nil {
		return nil, err
	}
	if _, ok := rank["rank_frozen"].(bool); !ok {
		return nil, errNew(location + ".proposal_rank.rank_frozen must be a boolean")
	}
	if err := requireNonemptyText(rank["ranking_policy"], location+".proposal_rank.ranking_policy"); err != nil {
		return nil, err
	}
	// 对应 Python "ranking_features" in rank and not isinstance(dict)
	if features, present := rank["ranking_features"]; present {
		if _, ok := features.(map[string]any); !ok {
			return nil, errNew(location + ".proposal_rank.ranking_features must be a mapping when present")
		}
	}
	outcome, err := requireMapping(mapValue(candidate, "outcome"), location+".outcome")
	if err != nil {
		return nil, err
	}
	if gain := outcome["target_gain"]; gain != nil && !isNumber(gain) {
		return nil, errNew(location + ".outcome.target_gain must be finite or null")
	}

	activation, err := requireMapping(mapValue(candidate, "activation"), location+".activation")
	if err != nil {
		return nil, err
	}
	availability, _ := activation["availability"].(string)
	state, _ := activation["state"].(string)
	if !_ACTIVATION_AVAILABILITIES[availability] {
		return nil, errNew(fmt.Sprintf("%s.activation.availability has unsupported value %s",
			location, pyRepr(activation["availability"])))
	}
	if !_ACTIVATION_STATES[state] {
		return nil, errNew(fmt.Sprintf("%s.activation.state has unsupported value %s",
			location, pyRepr(activation["state"])))
	}
	if availability == "observed" && state != "triggered" && state != "not_triggered" {
		return nil, errNew(location + ".activation.state must be triggered or not_triggered when observed")
	}
	if availability != "observed" && state != availability {
		return nil, errNew(fmt.Sprintf("%s.activation.state must preserve unavailable state %s",
			location, pyRepr(availability)))
	}
	surfaces, ok := activation["surfaces"].([]any)
	if !ok {
		return nil, errNew(location + ".activation.surfaces must be a list of non-empty strings")
	}
	for _, surface := range surfaces {
		text, ok := surface.(string)
		if !ok || text == "" {
			return nil, errNew(location + ".activation.surfaces must be a list of non-empty strings")
		}
	}

	regression, err := requireMapping(mapValue(candidate, "regression"), location+".regression")
	if err != nil {
		return nil, err
	}
	target, err := requireMapping(mapValue(regression, "target"), location+".regression.target")
	if err != nil {
		return nil, err
	}
	targetAvailability, _ := target["availability"].(string)
	if !_REGRESSION_AVAILABILITIES[targetAvailability] {
		return nil, errNew(fmt.Sprintf("%s.regression.target.availability has unsupported value %s",
			location, pyRepr(target["availability"])))
	}
	count, hasCount := target["regressed_case_count"]
	if targetAvailability == "observed" {
		// 对应 Python isinstance(count, bool) or not isinstance(int) or count < 0
		if isBool(count) || !isInt(count) || pyInt(count) < 0 || !hasCount {
			return nil, errNew(location + ".regression.target.regressed_case_count must be non-negative")
		}
	} else if hasCount && count != nil {
		return nil, errNew(location + ".regression.target.regressed_case_count must be null when unavailable")
	}
	return candidate, nil
}

// activationStateValue 取激活 state 原值（含非字符串形态，供 pyRepr 展示）
func activationStateValue(activation map[string]any) any {
	return activation["state"]
}

// requireMapping 要求值必须是映射（对应 Python _require_mapping L783-786）
func requireMapping(value any, location string) (map[string]any, error) {
	mapping, ok := value.(map[string]any)
	if !ok {
		return nil, errNew(location + " must be a mapping")
	}
	return mapping, nil
}

// requireExact 要求值与期望严格相等（对应 Python _require_exact L789-791）。
// 数值跨类型相等复刻 Python `1 == 1.0` 语义。
func requireExact(value any, expected any, location string) error {
	if !pyEqual(value, expected) {
		return errNew(fmt.Sprintf("%s must be %s, got %s", location, pyRepr(expected), pyRepr(value)))
	}
	return nil
}

// requireNonemptyText 要求值为非空白字符串（对应 Python _require_nonempty_text L794-796）
func requireNonemptyText(value any, location string) error {
	text, ok := value.(string)
	if !ok || strings.TrimSpace(text) == "" {
		return errNew(location + " must be a non-empty string")
	}
	return nil
}

// isNumber 判断是否有限数且非布尔（对应 Python _is_number L799-800）
func isNumber(value any) bool {
	if isBool(value) {
		return false
	}
	switch v := value.(type) {
	case int:
		return true
	case int64:
		return true
	case float64:
		return !math.IsInf(v, 0) && !math.IsNaN(v)
	default:
		return false
	}
}

// isNonnegativeInt 判断是否非负整数且非布尔（对应 Python _is_nonnegative_int L803-804）
func isNonnegativeInt(value any) bool {
	if isBool(value) || !isInt(value) {
		return false
	}
	return pyInt(value) >= 0
}

// comparabilityReason 判定 cohort 不可比较的原因，可比较返回空串
// （对应 Python _comparability_reason L463-496，分支顺序照抄）
func comparabilityReason(cohort map[string]any) string {
	candidates := cohort["candidates"].([]any)
	if len(candidates) < 2 {
		return "requires_at_least_two_comparable_candidates"
	}
	identity := cohort["cohort"].(map[string]any)
	requiredFields := []string{"parent_harness_ref", "source_eval_ref", "evaluation_protocol"}
	for _, field := range requiredFields {
		if pyFalsy(identity[field]) {
			return "cohort_identity_missing"
		}
	}
	if pyFalsy(identity["target_case_ids"]) {
		return "cohort_identity_missing"
	}
	if identity["rank_frozen"] != true {
		return "rank_not_frozen_before_evaluation"
	}
	candidateIDs := make([]string, 0, len(candidates))
	for _, item := range candidates {
		candidateIDs = append(candidateIDs, item.(map[string]any)["candidate_id"].(string))
	}
	if len(candidateIDs) != len(setOf(candidateIDs)) {
		return "duplicate_candidate_id"
	}
	// 对应 Python ranks = [candidate["proposal_rank"].get("predicted_rank") for ...]
	ranks := make([]any, 0, len(candidates))
	for _, item := range candidates {
		ranks = append(ranks, item.(map[string]any)["proposal_rank"].(map[string]any)["predicted_rank"])
	}
	for _, r := range ranks {
		if isBool(r) || !isInt(r) {
			return "predicted_rank_missing"
		}
	}
	// 对应 Python set(ranks) != set(range(1, len(candidates)+1))
	expected := map[int64]bool{}
	for i := 1; i <= len(candidates); i++ {
		expected[int64(i)] = true
	}
	actual := map[int64]bool{}
	for _, r := range ranks {
		actual[pyInt(r)] = true
	}
	if len(actual) != len(expected) {
		return "predicted_ranks_not_permutation"
	}
	for k := range expected {
		if !actual[k] {
			return "predicted_ranks_not_permutation"
		}
	}
	for _, item := range candidates {
		candidate := item.(map[string]any)
		candidateIdentity := candidate["identity"].(map[string]any)
		for _, field := range requiredFields {
			if !pyEqual(candidateIdentity[field], identity[field]) {
				return field + "_mismatch"
			}
		}
		// 对应 Python set(candidate_target_case_ids) != set(identity_target_case_ids)
		if !stringSetEqual(candidateIdentity["target_case_ids"], identity["target_case_ids"]) {
			return "target_case_set_mismatch"
		}
		if candidate["proposal_rank"].(map[string]any)["rank_frozen"] != true {
			return "candidate_rank_not_frozen"
		}
		if !pyEqual(candidate["proposal_rank"].(map[string]any)["ranking_policy"], identity["ranking_policy"]) {
			return "candidate_ranking_policy_mismatch"
		}
		if !isNumber(candidate["outcome"].(map[string]any)["target_gain"]) {
			return "target_gain_missing"
		}
	}
	for _, name := range []string{"best_of_k_gain", "top_m_gain"} {
		metric := cohort["metrics"].(map[string]any)[name].(map[string]any)
		if metric["status"] != "available" {
			return "search_metric_unavailable"
		}
	}
	return ""
}

// pyFalsy 复刻 Python falsy 判定（None/空串/空列表/0）
func pyFalsy(value any) bool {
	switch v := value.(type) {
	case nil:
		return true
	case string:
		return v == ""
	case bool:
		return !v
	case int:
		return v == 0
	case int64:
		return v == 0
	case float64:
		return v == 0
	case []any:
		return len(v) == 0
	case map[string]any:
		return len(v) == 0
	default:
		return false
	}
}

// setOf 去重集合
func setOf(items []string) map[string]bool {
	out := map[string]bool{}
	for _, item := range items {
		out[item] = true
	}
	return out
}

// sortedSetKeys 集合排序为字符串切片（对应 Python sorted(set)）
func sortedSetKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for key := range set {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

// stringSetEqual 复刻 Python set(list) == set(list) 比较
func stringSetEqual(left, right any) bool {
	leftList, okL := left.([]any)
	rightList, okR := right.([]any)
	if !okL || !okR {
		return false
	}
	leftSet := map[string]bool{}
	for _, item := range leftList {
		leftSet[pyStr(item)] = true
	}
	rightSet := map[string]bool{}
	for _, item := range rightList {
		rightSet[pyStr(item)] = true
	}
	if len(leftSet) != len(rightSet) {
		return false
	}
	for key := range leftSet {
		if !rightSet[key] {
			return false
		}
	}
	return true
}

// pyStr 字符串化（对应 Python str()，仅用于集合键）
func pyStr(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	return fmt.Sprintf("%v", value)
}

// collectHighValueOutsideTopM 收集"高点候选在 Top-m 预算外"证据
// （对应 Python _collect_high_value_outside_top_m L499-520）
func collectHighValueOutsideTopM(cohort map[string]any, threshold float64, opportunityIDs, supportIDs *[]string) error {
	best := cohort["metrics"].(map[string]any)["best_of_k_gain"].(map[string]any)
	topM := cohort["metrics"].(map[string]any)["top_m_gain"].(map[string]any)
	if pyFloat(best["value"]) <= threshold {
		return nil
	}
	cohortID := cohort["cohort"].(map[string]any)["cohort_id"].(string)
	*opportunityIDs = append(*opportunityIDs, cohortID)
	candidateID := best["candidate_id"]
	var candidate map[string]any
	for _, item := range cohort["candidates"].([]any) {
		if pyEqual(item.(map[string]any)["candidate_id"], candidateID) {
			candidate = item.(map[string]any)
			break
		}
	}
	if candidate == nil {
		return errNew(fmt.Sprintf("cohort %s best_of_k_gain candidate_id is not in candidates", pyRepr(cohortID)))
	}
	effectiveM := topM["effective_m"]
	if isBool(effectiveM) || !isInt(effectiveM) || pyInt(effectiveM) < 1 {
		return errNew(fmt.Sprintf("cohort %s top_m_gain.effective_m must be a positive integer", pyRepr(cohortID)))
	}
	predictedRank := candidate["proposal_rank"].(map[string]any)["predicted_rank"]
	if pyInt(predictedRank) > pyInt(effectiveM) {
		*supportIDs = append(*supportIDs, cohortID)
	}
	return nil
}

// cohortDuplicateCounts 返回（候选指纹总数，重复指纹数）
// （对应 Python _cohort_duplicate_counts L523-525）
func cohortDuplicateCounts(cohort map[string]any) (int, int) {
	fingerprints := make([]string, 0)
	for _, item := range cohort["candidates"].([]any) {
		fingerprints = append(fingerprints, item.(map[string]any)["candidate_fingerprint"].(string))
	}
	return len(fingerprints), len(fingerprints) - len(setOf(fingerprints))
}

// collectFeatureEvidence 收集特征-结果对齐证据：候选两两组合，恰好一个数值特征不同
// 且预测/实际赢家不一致时计入（对应 Python _collect_feature_evidence L528-563）
func collectFeatureEvidence(cohort map[string]any, featureEvidence map[string]*featureEvidence) {
	cohortID := cohort["cohort"].(map[string]any)["cohort_id"].(string)
	candidates := cohort["candidates"].([]any)
	for i := 0; i < len(candidates); i++ {
		for j := i + 1; j < len(candidates); j++ {
			first := candidates[i].(map[string]any)
			second := candidates[j].(map[string]any)
			firstFeatures := numericRankingFeatures(first)
			secondFeatures := numericRankingFeatures(second)
			if len(firstFeatures) == 0 || !featureKeySetsEqual(firstFeatures, secondFeatures) {
				continue
			}
			differing := make([]string, 0)
			for feature, value := range firstFeatures {
				if value != secondFeatures[feature] {
					differing = append(differing, feature)
				}
			}
			sort.Strings(differing)
			if len(differing) != 1 {
				continue
			}
			feature := differing[0]
			firstGain := pyFloat(first["outcome"].(map[string]any)["target_gain"])
			secondGain := pyFloat(second["outcome"].(map[string]any)["target_gain"])
			firstScore := first["proposal_rank"].(map[string]any)["predicted_score"]
			secondScore := second["proposal_rank"].(map[string]any)["predicted_score"]
			if firstGain == secondGain || !isNumber(firstScore) || !isNumber(secondScore) {
				continue
			}
			if pyFloat(firstScore) == pyFloat(secondScore) {
				continue
			}
			predictedWinnerIsFirst := pyFloat(firstScore) > pyFloat(secondScore)
			realizedWinnerIsFirst := firstGain > secondGain
			aligned := predictedWinnerIsFirst == realizedWinnerIsFirst
			adjustment := ""
			if !aligned {
				higherFeatureIsFirst := firstFeatures[feature] > secondFeatures[feature]
				higherFeaturePredicted := predictedWinnerIsFirst == higherFeatureIsFirst
				if higherFeaturePredicted {
					adjustment = "decrease"
				} else {
					adjustment = "increase"
				}
			}
			evidence, present := featureEvidence[feature]
			if !present {
				evidence = newFeatureEvidence()
				featureEvidence[feature] = evidence
			}
			evidence.observe(cohortID, aligned, adjustment)
		}
	}
}

// featureKeySetsEqual 特征键集合相等（对应 Python first_features.keys() != second_features.keys()）
func featureKeySetsEqual(left, right map[string]float64) bool {
	if len(left) != len(right) {
		return false
	}
	for key := range left {
		if _, ok := right[key]; !ok {
			return false
		}
	}
	return true
}

// numericRankingFeatures 提取数值排序特征（对应 Python _numeric_ranking_features L566-570）
func numericRankingFeatures(candidate map[string]any) map[string]float64 {
	raw, ok := candidate["proposal_rank"].(map[string]any)["ranking_features"].(map[string]any)
	if !ok {
		return map[string]float64{}
	}
	out := map[string]float64{}
	for key, value := range raw {
		if isNumber(value) {
			out[key] = pyFloat(value)
		}
	}
	return out
}

// rate 计算比率，分母为 0 返回 nil（对应 Python _rate L757-758）
func rate(numerator, denominator int) any {
	if denominator == 0 {
		return nil
	}
	return float64(numerator) / float64(denominator)
}

// counterDict 计数 map 键排序输出（对应 Python _counter_dict L761-762）
func counterDict(counter map[string]int) map[string]int {
	out := make(map[string]int, len(counter))
	for key, value := range counter {
		out[key] = value
	}
	return out
}

// stablePatterns 枚举稳定模式清单（对应 Python _stable_patterns L573-714，模式顺序照抄）
func stablePatterns(
	minSupportCohorts int,
	comparableIDs, nonpositiveIDs, highValueOpportunityIDs, highValueOutsideIDs []string,
	duplicateOpportunityCohorts, duplicateCohortIDs []string,
	triggerBySurface, regressionBySurface map[string]*surfaceEvidence,
	featureEvidence map[string]*featureEvidence,
	positiveSelectionRegretIDs, partialRepairOpportunityIDs, partialRepairSupportIDs []string,
) []map[string]any {
	patterns := make([]map[string]any, 0)
	appendPattern(&patterns, minSupportCohorts,
		"all_candidates_nonpositive", "all_candidates_nonpositive", nil,
		nonpositiveIDs, comparableIDs,
		map[string]any{
			"field":     "generation_directives.require_distinct_intervention_surfaces",
			"operation": "set",
			"value":     true,
			"rationale": "Comparable cohorts repeatedly produced no positive-gain candidate.",
		})
	appendPattern(&patterns, minSupportCohorts,
		"partial_candidate_has_residual_requirements", "partial_candidate_has_residual_requirements", nil,
		partialRepairSupportIDs, partialRepairOpportunityIDs,
		map[string]any{
			"field":     "generation_directives.preserve_partial_progress_and_target_residual",
			"operation": "set",
			"value":     true,
			"rationale": ("Candidates repeatedly improved some requirements without regression while residual " +
				"requirements remained; repair should preserve verified gains and target only the residue."),
		})
	appendPattern(&patterns, minSupportCohorts,
		"high_value_candidate_outside_top_m", "high_value_candidate_outside_top_m", nil,
		highValueOutsideIDs, highValueOpportunityIDs,
		map[string]any{
			"field":     "budget_policy.top_m",
			"operation": "increase",
			"value":     1,
			"rationale": ("The realized best positive-gain candidate repeatedly ranked outside the current Top-m budget."),
		})
	appendPattern(&patterns, minSupportCohorts,
		"duplicate_candidates", "duplicate_candidates", nil,
		duplicateCohortIDs, duplicateOpportunityCohorts,
		map[string]any{
			"field":     "generation_directives.require_unique_candidate_fingerprint",
			"operation": "set",
			"value":     true,
			"rationale": "Multiple cohorts spent evaluation budget on semantically duplicate candidates.",
		})
	// 对应 Python for surface, evidence in sorted(trigger_by_surface.items())
	surfaces := make([]string, 0, len(triggerBySurface))
	for surface := range triggerBySurface {
		surfaces = append(surfaces, surface)
	}
	sort.Strings(surfaces)
	for _, surface := range surfaces {
		evidence := triggerBySurface[surface]
		appendPattern(&patterns, minSupportCohorts,
			fmt.Sprintf("trigger_observed_failure:%s", surface), "trigger_observed_failure", surface,
			sortedSetKeys(evidence.triggerFailureCohorts), sortedSetKeys(evidence.triggerOpportunityCohorts),
			map[string]any{
				"field":     fmt.Sprintf("generation_directives.require_activation_evidence.%s", surface),
				"operation": "set",
				"value":     true,
				"rationale": fmt.Sprintf("Observed %s candidates repeatedly failed to trigger on all expected case pairs.", surface),
			})
		regression := regressionBySurface[surface]
		appendPattern(&patterns, minSupportCohorts,
			fmt.Sprintf("target_regression:%s", surface), "target_regression", surface,
			sortedSetKeys(regression.regressionFailureCohorts), sortedSetKeys(regression.regressionOpportunityCohorts),
			map[string]any{
				"field":     fmt.Sprintf("generation_directives.avoid_target_regression.%s", surface),
				"operation": "set",
				"value":     true,
				"rationale": fmt.Sprintf("Observed %s candidates repeatedly regressed at least one target case.", surface),
			})
	}
	// 对应 Python ranking_misalignment：increase/decrease 支持集与正向 regret 交集数量比较
	positiveRegret := setOf(positiveSelectionRegretIDs)
	features := make([]string, 0, len(featureEvidence))
	for feature := range featureEvidence {
		features = append(features, feature)
	}
	sort.Strings(features)
	for _, feature := range features {
		evidence := featureEvidence[feature]
		increaseSupport := sortedSetKeys(intersect(evidence.increaseCohorts, positiveRegret))
		decreaseSupport := sortedSetKeys(intersect(evidence.decreaseCohorts, positiveRegret))
		if len(increaseSupport) == len(decreaseSupport) {
			continue
		}
		operation := "decrease"
		support := decreaseSupport
		if len(increaseSupport) > len(decreaseSupport) {
			operation = "increase"
			support = increaseSupport
		}
		opportunities := sortedSetKeys(intersect(evidence.comparableCohorts, positiveRegret))
		appendPattern(&patterns, minSupportCohorts,
			fmt.Sprintf("ranking_misalignment:%s:%s", feature, operation), "ranking_misalignment", nil,
			support, opportunities,
			map[string]any{
				"field":     fmt.Sprintf("ranking_weights.%s", feature),
				"operation": operation,
				"value":     0.1,
				"rationale": (fmt.Sprintf("Isolated %s pairs repeatedly opposed realized ordering in cohorts with positive "+
					"selection regret.", feature)),
			})
	}
	return patterns
}

// appendPattern 按支持门槛追加模式条目（对应 Python _append_pattern L717-743）
func appendPattern(
	patterns *[]map[string]any,
	minSupportCohorts int,
	patternID, patternType string,
	surface any,
	supportIDs, opportunityIDs []string,
	policyChange map[string]any,
) {
	support := sortedSetKeys(setOf(supportIDs))
	opportunity := sortedSetKeys(setOf(opportunityIDs))
	if len(support) < minSupportCohorts {
		return
	}
	*patterns = append(*patterns, map[string]any{
		"pattern_id":                patternID,
		"pattern_type":              patternType,
		"surface":                   surface,
		"support_cohorts":           len(support),
		"opportunity_cohorts":       len(opportunity),
		"rate":                      rate(len(support), len(opportunity)),
		"evidence_cohort_ids":       support,
		"recommended_policy_change": policyChange,
	})
}

// cohortRate 速率汇总结构（对应 Python _cohort_rate L746-754）
func cohortRate(supportIDs, opportunityIDs []string) map[string]any {
	support := sortedSetKeys(setOf(supportIDs))
	opportunity := sortedSetKeys(setOf(opportunityIDs))
	return map[string]any{
		"support_cohort_count":     len(support),
		"opportunity_cohort_count": len(opportunity),
		"rate":                     rate(len(support), len(opportunity)),
		"evidence_cohort_ids":      support,
	}
}

// intersect 集合交集（对应 Python set & set）
func intersect(left, right map[string]bool) map[string]bool {
	out := map[string]bool{}
	for key := range left {
		if right[key] {
			out[key] = true
		}
	}
	return out
}

// requireUniqueCohortIDs 校验跨输入 cohort_id 唯一（对应 Python _require_unique_cohort_ids L776-780）
func requireUniqueCohortIDs(cohorts []map[string]any) error {
	counts := map[string]int{}
	for _, cohort := range cohorts {
		counts[cohort["cohort"].(map[string]any)["cohort_id"].(string)]++
	}
	duplicates := make([]string, 0)
	for id, count := range counts {
		if count > 1 {
			duplicates = append(duplicates, id)
		}
	}
	sort.Strings(duplicates)
	if len(duplicates) > 0 {
		return errNew(fmt.Sprintf("duplicate cohort_id across inputs: %s", strings.Join(duplicates, ", ")))
	}
	return nil
}

// maxFloat 取最大浮点值
func maxFloat(values []float64) float64 {
	best := values[0]
	for _, v := range values[1:] {
		if v > best {
			best = v
		}
	}
	return best
}

// trainingLedgerDigest 训练账本摘要：cohort 按 id 排序后做 Python json.dumps(sort_keys=True,
// separators=(",",":"), ensure_ascii=False) 等价的紧凑序列化再取 sha256
// （对应 Python _training_ledger_digest L765-773；float 序列化走 pyFloatRepr 复刻 repr 语义，D-e1-3）
func trainingLedgerDigest(cohorts []map[string]any) (string, error) {
	ordered := make([]map[string]any, len(cohorts))
	copy(ordered, cohorts)
	sort.Slice(ordered, func(i, j int) bool {
		return ordered[i]["cohort"].(map[string]any)["cohort_id"].(string) <
			ordered[j]["cohort"].(map[string]any)["cohort_id"].(string)
	})
	payload, err := marshalCanonical(ordered)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return fmt.Sprintf("sha256:%x", sum), nil
}

// marshalCanonical 复刻 Python json.dumps(sort_keys=True, separators=(",",":"), ensure_ascii=False)：
// 键排序、紧凑分隔符、非 ASCII 原文输出、float 走 repr 语义
func marshalCanonical(value any) ([]byte, error) {
	var buf bytes.Buffer
	if err := writeCanonicalJSON(&buf, value); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// writeCanonicalJSON 递归写出 canonical JSON
func writeCanonicalJSON(buf *bytes.Buffer, value any) error {
	switch v := value.(type) {
	case nil:
		buf.WriteString("null")
		return nil
	case bool:
		if v {
			buf.WriteString("true")
		} else {
			buf.WriteString("false")
		}
		return nil
	case string:
		return writeJSONString(buf, v)
	case int:
		buf.WriteString(strconv.Itoa(v))
		return nil
	case int64:
		buf.WriteString(strconv.FormatInt(v, 10))
		return nil
	case float64:
		buf.WriteString(canonicalFloatRepr(v))
		return nil
	case map[string]any:
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		buf.WriteByte('{')
		for i, key := range keys {
			if i > 0 {
				buf.WriteByte(',')
			}
			if err := writeJSONString(buf, key); err != nil {
				return err
			}
			buf.WriteByte(':')
			if err := writeCanonicalJSON(buf, v[key]); err != nil {
				return err
			}
		}
		buf.WriteByte('}')
		return nil
	default:
		// 切片/数组（含 []string 等具体切片类型）与具体 map 类型
		rv := reflect.ValueOf(value)
		switch rv.Kind() {
		case reflect.Map:
			keys := make([]string, 0, rv.Len())
			iter := rv.MapRange()
			for iter.Next() {
				keys = append(keys, iter.Key().Interface().(string))
			}
			sort.Strings(keys)
			buf.WriteByte('{')
			for i, key := range keys {
				if i > 0 {
					buf.WriteByte(',')
				}
				if err := writeJSONString(buf, key); err != nil {
					return err
				}
				buf.WriteByte(':')
				if err := writeCanonicalJSON(buf, rv.MapIndex(reflect.ValueOf(key)).Interface()); err != nil {
					return err
				}
			}
			buf.WriteByte('}')
			return nil
		case reflect.Slice, reflect.Array:
			buf.WriteByte('[')
			for i := 0; i < rv.Len(); i++ {
				if i > 0 {
					buf.WriteByte(',')
				}
				if err := writeCanonicalJSON(buf, rv.Index(i).Interface()); err != nil {
					return err
				}
			}
			buf.WriteByte(']')
			return nil
		default:
			return errNew(fmt.Sprintf("unsupported canonical JSON type %T", value))
		}
	}
}

// canonicalFloatRepr 复刻 Python float repr（json.dumps 对 float 的输出即 repr）
func canonicalFloatRepr(f float64) string {
	if math.IsInf(f, 0) || math.IsNaN(f) {
		// 校验链已保证有限数；防御性兜底
		return "null"
	}
	return pyFloatRepr(f)
}

// writeJSONString 写出 JSON 字符串字面量（非 ASCII 原文，转义仅 JSON 必需字符）
func writeJSONString(buf *bytes.Buffer, text string) error {
	encoded, err := json.Marshal(text)
	if err != nil {
		return err
	}
	buf.Write(encoded)
	return nil
}
