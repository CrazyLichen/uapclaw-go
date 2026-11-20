// Package improver_evolution 中 meta_validation 部分：候选 Improver 策略的确定性配对校验。
// 刻意只消费已物化的 checkpoint 结果，不运行 Improver、不改动策略状态、不发布策略版本。
//
// 对应 Python 代码：openjiuwen/rsi/harness_rsi/improver_evolution/meta_validation.py
package improver_evolution

import (
	"errors"
	"github.com/uapclaw/uapclaw-go/internal/common/pythonsem"
	"math"
	"strings"
)

// ──────────────────────────── 结构体 ────────────────────────────

// MetaValidationThresholds 成对宏平均晋升阈值（不可变）。
// 容差以指标单位表达：非退步容差 0.01 允许候选宏平均最多差 0.01。
// 对应 Python frozen dataclass（偏差 D-e1-4：非导出字段+构造函数校验实现冻结语义）。
type MetaValidationThresholds struct {
	minUnseenCheckpoints                             int
	topMGainNonDegradationTolerance                  float64
	minSelectionRegretImprovement                    float64
	bestOfKGainNonDegradationTolerance               float64
	finalHarnessGainPerBudgetNonDegradationTolerance float64
	maxRegressionFailureRateIncrease                 float64
	maxInfrastructureFailureRateIncrease             float64
}

// NewMetaValidationThresholds 构造并执行全部校验（对应 Python __post_init__ L58-69）。
// 入参顺序照抄 dataclass 字段序：min_unseen_checkpoints、top_m_gain 容差、
// min_selection_regret_improvement、best_of_k_gain 容差、final_harness_gain_per_budget 容差、
// max_regression_failure_rate_increase、max_infrastructure_failure_rate_increase。
func NewMetaValidationThresholds(
	minUnseenCheckpoints int,
	topMGainNonDegradationTolerance float64,
	minSelectionRegretImprovement float64,
	bestOfKGainNonDegradationTolerance float64,
	finalHarnessGainPerBudgetNonDegradationTolerance float64,
	maxRegressionFailureRateIncrease float64,
	maxInfrastructureFailureRateIncrease float64,
) (*MetaValidationThresholds, error) {
	if minUnseenCheckpoints < 1 {
		return nil, errors.New("min_unseen_checkpoints must be an integer of at least 1")
	}
	values := map[string]float64{
		"top_m_gain_non_degradation_tolerance":                    topMGainNonDegradationTolerance,
		"min_selection_regret_improvement":                        minSelectionRegretImprovement,
		"best_of_k_gain_non_degradation_tolerance":                bestOfKGainNonDegradationTolerance,
		"final_harness_gain_per_budget_non_degradation_tolerance": finalHarnessGainPerBudgetNonDegradationTolerance,
		"max_regression_failure_rate_increase":                    maxRegressionFailureRateIncrease,
		"max_infrastructure_failure_rate_increase":                maxInfrastructureFailureRateIncrease,
	}
	for _, name := range []string{
		"top_m_gain_non_degradation_tolerance",
		"min_selection_regret_improvement",
		"best_of_k_gain_non_degradation_tolerance",
		"final_harness_gain_per_budget_non_degradation_tolerance",
		"max_regression_failure_rate_increase",
		"max_infrastructure_failure_rate_increase",
	} {
		value := values[name]
		if math.IsInf(value, 0) || math.IsNaN(value) || value < 0 {
			return nil, errors.New(name + " must be a finite non-negative number")
		}
	}
	return &MetaValidationThresholds{
		minUnseenCheckpoints:                             minUnseenCheckpoints,
		topMGainNonDegradationTolerance:                  topMGainNonDegradationTolerance,
		minSelectionRegretImprovement:                    minSelectionRegretImprovement,
		bestOfKGainNonDegradationTolerance:               bestOfKGainNonDegradationTolerance,
		finalHarnessGainPerBudgetNonDegradationTolerance: finalHarnessGainPerBudgetNonDegradationTolerance,
		maxRegressionFailureRateIncrease:                 maxRegressionFailureRateIncrease,
		maxInfrastructureFailureRateIncrease:             maxInfrastructureFailureRateIncrease,
	}, nil
}

// DefaultMetaValidationThresholds 默认阈值（对应 Python dataclass 默认值）
func DefaultMetaValidationThresholds() *MetaValidationThresholds {
	return &MetaValidationThresholds{
		minUnseenCheckpoints:                             3,
		topMGainNonDegradationTolerance:                  0.0,
		minSelectionRegretImprovement:                    1e-12,
		bestOfKGainNonDegradationTolerance:               0.0,
		finalHarnessGainPerBudgetNonDegradationTolerance: 0.0,
		maxRegressionFailureRateIncrease:                 0.0,
		maxInfrastructureFailureRateIncrease:             0.0,
	}
}

// MinUnseenCheckpoints 访问器（对应 Python 字段 min_unseen_checkpoints）
func (t *MetaValidationThresholds) MinUnseenCheckpoints() int {
	return t.minUnseenCheckpoints
}

// TopMGainNonDegradationTolerance 访问器
func (t *MetaValidationThresholds) TopMGainNonDegradationTolerance() float64 {
	return t.topMGainNonDegradationTolerance
}

// MinSelectionRegretImprovement 访问器
func (t *MetaValidationThresholds) MinSelectionRegretImprovement() float64 {
	return t.minSelectionRegretImprovement
}

// BestOfKGainNonDegradationTolerance 访问器
func (t *MetaValidationThresholds) BestOfKGainNonDegradationTolerance() float64 {
	return t.bestOfKGainNonDegradationTolerance
}

// FinalHarnessGainPerBudgetNonDegradationTolerance 访问器
func (t *MetaValidationThresholds) FinalHarnessGainPerBudgetNonDegradationTolerance() float64 {
	return t.finalHarnessGainPerBudgetNonDegradationTolerance
}

// MaxRegressionFailureRateIncrease 访问器
func (t *MetaValidationThresholds) MaxRegressionFailureRateIncrease() float64 {
	return t.maxRegressionFailureRateIncrease
}

// MaxInfrastructureFailureRateIncrease 访问器
func (t *MetaValidationThresholds) MaxInfrastructureFailureRateIncrease() float64 {
	return t.maxInfrastructureFailureRateIncrease
}

// AsDict 报告 thresholds 键输出（对应 Python asdict(thresholds)，键名 snake_case）
func (t *MetaValidationThresholds) AsDict() map[string]any {
	return map[string]any{
		"min_unseen_checkpoints":                                  t.minUnseenCheckpoints,
		"top_m_gain_non_degradation_tolerance":                    t.topMGainNonDegradationTolerance,
		"min_selection_regret_improvement":                        t.minSelectionRegretImprovement,
		"best_of_k_gain_non_degradation_tolerance":                t.bestOfKGainNonDegradationTolerance,
		"final_harness_gain_per_budget_non_degradation_tolerance": t.finalHarnessGainPerBudgetNonDegradationTolerance,
		"max_regression_failure_rate_increase":                    t.maxRegressionFailureRateIncrease,
		"max_infrastructure_failure_rate_increase":                t.maxInfrastructureFailureRateIncrease,
	}
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// _MODES 校验模式合法取值
var _MODES = map[string]bool{"offline_rerank": true, "live_generation": true}

// _FROZEN_FIELDS 冻结字段清单（顺序照抄 Python L18-29）
var _FROZEN_FIELDS = []string{
	"base_harness_id",
	"failure_evidence_id",
	"base_model_id",
	"k",
	"top_m",
	"token_budget",
	"tool_budget",
	"runtime_budget",
	"verifier_id",
	"protocol_id",
}

// _OFFLINE_METRICS offline_rerank 模式必需指标
var _OFFLINE_METRICS = []string{"top_m_gain", "selection_regret"}

// _LIVE_METRICS live_generation 模式必需指标
var _LIVE_METRICS = []string{
	"best_of_k_gain",
	"top_m_gain",
	"selection_regret",
	"final_harness_gain_per_budget",
	"regression_failure_rate",
	"infrastructure_failure_rate",
}

// _RATE_METRICS 越低越好的比率指标集合
var _RATE_METRICS = map[string]bool{"regression_failure_rate": true, "infrastructure_failure_rate": true}

// ──────────────────────────── 非导出函数 ────────────────────────────

// indexResults 按 checkpoint_id 建索引并收集问题（对应 Python _index_results L258-278）
func indexResults(results []map[string]any, side string) (map[string]map[string]any, []map[string]any) {
	indexed := map[string]map[string]any{}
	issues := make([]map[string]any, 0)
	for position, record := range results {
		if record == nil {
			issues = append(issues, map[string]any{
				"code": "invalid_checkpoint_record", "side": side, "position": position,
			})
			continue
		}
		checkpointID := record["checkpoint_id"]
		text, ok := checkpointID.(string)
		if !ok || strings.TrimSpace(text) == "" {
			issues = append(issues, map[string]any{
				"code": "missing_checkpoint_id", "side": side, "position": position,
			})
			continue
		}
		checkpointID = strings.TrimSpace(text)
		if _, present := indexed[checkpointID.(string)]; present {
			issues = append(issues, map[string]any{
				"code": "duplicate_checkpoint_id", "side": side, "checkpoint_id": checkpointID,
			})
			continue
		}
		indexed[checkpointID.(string)] = record
	}
	return indexed, issues
}

// improverIdentity 提取同侧 Improver 身份并校验一致性（对应 Python _improver_identity L281-335）
func improverIdentity(results []map[string]any, side string) (map[string]string, []map[string]any) {
	fields := []string{"improver_version_id", "improver_policy_digest"}
	valuesByField := map[string]map[string]bool{}
	for _, field := range fields {
		valuesByField[field] = map[string]bool{}
	}
	issues := make([]map[string]any, 0)
	for position, record := range results {
		if record == nil {
			continue
		}
		checkpointID := record["checkpoint_id"]
		checkpointDetail := ""
		if text, ok := checkpointID.(string); ok {
			checkpointDetail = text
		}
		for _, field := range fields {
			value, present := record[field]
			if !present || value == nil || value == "" {
				issues = append(issues, map[string]any{
					"code":          "missing_improver_identity_field",
					"side":          side,
					"position":      position,
					"checkpoint_id": checkpointDetail,
					"field":         field,
				})
				continue
			}
			text, ok := value.(string)
			if !ok || strings.TrimSpace(text) == "" {
				issues = append(issues, map[string]any{
					"code":          "invalid_improver_identity_field",
					"side":          side,
					"position":      position,
					"checkpoint_id": checkpointDetail,
					"field":         field,
				})
				continue
			}
			valuesByField[field][strings.TrimSpace(text)] = true
		}
	}
	for _, field := range fields {
		if len(valuesByField[field]) > 1 {
			issues = append(issues, map[string]any{
				"code":  "inconsistent_improver_identity",
				"side":  side,
				"field": field,
			})
		}
	}
	// 对应 Python next(iter(version_ids)) if len == 1 else ""
	identity := map[string]string{}
	for _, field := range fields {
		value := ""
		if len(valuesByField[field]) == 1 {
			for key := range valuesByField[field] {
				value = key
			}
		}
		identity[field] = value
	}
	return map[string]string{
		"version_id":    identity["improver_version_id"],
		"policy_digest": identity["improver_policy_digest"],
	}, issues
}

// pairIssues 校验单个 checkpoint 的配对合法性（对应 Python _pair_issues L338-380，分支顺序照抄）
func pairIssues(checkpointID string, baseline, candidate map[string]any, trainIDs map[string]bool) []map[string]any {
	issues := make([]map[string]any, 0)
	if trainIDs[checkpointID] {
		issues = append(issues, map[string]any{
			"code": "meta_train_contamination", "checkpoint_id": checkpointID,
		})
	}
	for _, sideRecord := range []struct {
		side   string
		record map[string]any
	}{{"baseline", baseline}, {"candidate", candidate}} {
		split := sideRecord.record["split"]
		if !pythonsem.PyEqual(split, "meta_test") {
			issues = append(issues, map[string]any{
				"code":          "non_meta_test_checkpoint",
				"checkpoint_id": checkpointID,
				"side":          sideRecord.side,
			})
		}
		for _, field := range _FROZEN_FIELDS {
			value, present := sideRecord.record[field]
			if !present || value == nil || value == "" {
				issues = append(issues, map[string]any{
					"code":          "missing_frozen_field",
					"checkpoint_id": checkpointID,
					"side":          sideRecord.side,
					"field":         field,
				})
			}
		}
		issues = append(issues, frozenValueIssues(checkpointID, sideRecord.record, sideRecord.side)...)
	}
	for _, field := range _FROZEN_FIELDS {
		baseValue, hasBase := baseline[field]
		candValue, hasCand := candidate[field]
		if hasBase && hasCand && !pythonsem.PyEqual(baseValue, candValue) {
			issues = append(issues, map[string]any{
				"code":          "frozen_field_mismatch",
				"checkpoint_id": checkpointID,
				"field":         field,
			})
		}
	}
	if pythonsem.PyEqual(baseline["k"], 1) && pythonsem.PyEqual(candidate["k"], 1) {
		issues = append(issues, map[string]any{
			"code": "search_metrics_require_k_at_least_two", "checkpoint_id": checkpointID,
		})
	}
	return issues
}

// frozenValueIssues 校验冻结字段取值合法性（对应 Python _frozen_value_issues L383-434）
func frozenValueIssues(checkpointID string, record map[string]any, side string) []map[string]any {
	issues := make([]map[string]any, 0)
	for _, field := range []string{"base_harness_id", "failure_evidence_id", "base_model_id", "verifier_id", "protocol_id"} {
		value, present := record[field]
		if !present {
			continue
		}
		text, ok := value.(string)
		if !ok || strings.TrimSpace(text) == "" {
			issues = append(issues, map[string]any{
				"code": "invalid_frozen_field", "checkpoint_id": checkpointID,
				"side": side, "field": field,
			})
		}
	}
	for _, field := range []string{"k", "top_m"} {
		value := record[field]
		if value != nil && !isPositiveInt(value) {
			issues = append(issues, map[string]any{
				"code": "invalid_frozen_field", "checkpoint_id": checkpointID,
				"side": side, "field": field,
			})
		}
	}
	k := record["k"]
	topM := record["top_m"]
	if isPositiveInt(k) && isPositiveInt(topM) && pythonsem.PyInt(topM) > pythonsem.PyInt(k) {
		issues = append(issues, map[string]any{
			"code": "invalid_frozen_field", "checkpoint_id": checkpointID,
			"field": "top_m", "reason": "top_m_exceeds_k",
		})
	}
	for _, field := range []string{"token_budget", "tool_budget", "runtime_budget"} {
		value := record[field]
		if value != nil && (!isFiniteNumber(value) || pythonsem.PyFloat(value) < 0) {
			issues = append(issues, map[string]any{
				"code": "invalid_frozen_field", "checkpoint_id": checkpointID,
				"side": side, "field": field,
			})
		}
	}
	return issues
}

// isPositiveInt 正整数判定（对应 Python _is_positive_int L577-578）
func isPositiveInt(value any) bool {
	return !pythonsem.IsBool(value) && pythonsem.IsInt(value) && pythonsem.PyInt(value) > 0
}

// isFiniteNumber 有限数判定且非布尔（对应 Python _is_finite_number L573-574）
func isFiniteNumber(value any) bool {
	if pythonsem.IsBool(value) {
		return false
	}
	return pythonsem.IsNumber(value)
}

// reasonCodes 从 issues 提取去重保序的 code 列表（对应 Python _reason_codes L569-570）
func reasonCodes(issues []map[string]any) []string {
	seen := map[string]bool{}
	out := make([]string, 0)
	for _, issue := range issues {
		code := pyStr(issue["code"])
		if !seen[code] {
			seen[code] = true
			out = append(out, code)
		}
	}
	return out
}

// PairedMetaValidate 在成对 unseen checkpoint 上比较基线 Improver I_t 与候选 Improver
// （对应 Python paired_meta_validate L72-208，步骤逐条对应）。
// D-e1-2：thresholds 为 *MetaValidationThresholds（nil=默认），不保留 Mapping 动态形态。
func PairedMetaValidate(
	baselineResults []map[string]any,
	candidateResults []map[string]any,
	mode string,
	thresholds *MetaValidationThresholds,
	metaTrainCheckpointIDs []string,
) map[string]any {
	resolvedThresholds := resolveThresholds(thresholds)
	report := emptyReport(mode, resolvedThresholds)

	if !_MODES[mode] {
		return reject(report, "invalid_mode", map[string]any{"mode": mode})
	}

	baselineByID, baselineIssues := indexResults(baselineResults, "baseline")
	candidateByID, candidateIssues := indexResults(candidateResults, "candidate")
	baselineIdentity, baselineIdentityIssues := improverIdentity(baselineResults, "baseline")
	candidateIdentity, candidateIdentityIssues := improverIdentity(candidateResults, "candidate")
	report["baseline_improver"] = map[string]any{"version_id": baselineIdentity["version_id"], "policy_digest": baselineIdentity["policy_digest"]}
	report["candidate_improver"] = map[string]any{"version_id": candidateIdentity["version_id"], "policy_digest": candidateIdentity["policy_digest"]}
	issues := make([]map[string]any, 0)
	issues = append(issues, baselineIssues...)
	issues = append(issues, candidateIssues...)
	issues = append(issues, baselineIdentityIssues...)
	issues = append(issues, candidateIdentityIssues...)
	if len(baselineResults) == 0 || len(candidateResults) == 0 {
		issues = append(issues, map[string]any{"code": "empty_checkpoint_results"})
	}

	// 对应 Python policy_digest 双侧非空且相等
	if baselineIdentity["policy_digest"] != "" && candidateIdentity["policy_digest"] != "" &&
		baselineIdentity["policy_digest"] == candidateIdentity["policy_digest"] {
		issues = append(issues, map[string]any{"code": "identical_improver_policy_digest"})
	}

	baselineIDs := sortedSetKeys(mapKeySet(baselineByID))
	candidateIDs := sortedSetKeys(mapKeySet(candidateByID))
	if !stringSliceSetEqual(baselineIDs, candidateIDs) {
		issues = append(issues, map[string]any{
			"code":           "checkpoint_set_mismatch",
			"baseline_only":  sortedSetKeys(difference(mapKeySet(baselineByID), mapKeySet(candidateByID))),
			"candidate_only": sortedSetKeys(difference(mapKeySet(candidateByID), mapKeySet(baselineByID))),
		})
	}

	trainIDs := map[string]bool{}
	for _, id := range metaTrainCheckpointIDs {
		trimmed := strings.TrimSpace(id)
		if trimmed != "" {
			trainIDs[trimmed] = true
		}
	}
	intersection := sortedSetKeys(intersect(mapKeySet(baselineByID), mapKeySet(candidateByID)))
	for _, checkpointID := range intersection {
		issues = append(issues, pairIssues(checkpointID, baselineByID[checkpointID], candidateByID[checkpointID], trainIDs)...)
	}

	if len(issues) > 0 {
		report["validation"].(map[string]any)["issues"] = issues
		report["validation"].(map[string]any)["reason_codes"] = reasonCodes(issues)
		report["validation"].(map[string]any)["checkpoint_count"] = len(intersection)
		report["promotion"].(map[string]any)["reason_codes"] = append([]string{}, reasonCodes(issues)...)
		return report
	}

	// 对应 Python required_metrics 选择 L150
	requiredMetricsList := _OFFLINE_METRICS
	if mode == "live_generation" {
		requiredMetricsList = _LIVE_METRICS
	}
	checkpointReports := make([]map[string]any, 0)
	metricIssues := make([]map[string]any, 0)
	for _, checkpointID := range sortedSetKeys(mapKeySet(baselineByID)) {
		baseline := baselineByID[checkpointID]
		candidate := candidateByID[checkpointID]
		baselineMetrics, baselineMetricIssues := requiredMetrics(checkpointID, baseline, "baseline", requiredMetricsList)
		candidateMetrics, candidateMetricIssues := requiredMetrics(checkpointID, candidate, "candidate", requiredMetricsList)
		metricIssues = append(metricIssues, baselineMetricIssues...)
		metricIssues = append(metricIssues, candidateMetricIssues...)
		if len(baselineMetricIssues) > 0 || len(candidateMetricIssues) > 0 {
			continue
		}
		checkpointReports = append(checkpointReports,
			checkpointReport(checkpointID, baseline, baselineMetrics, candidateMetrics, requiredMetricsList))
	}

	report["validation"] = map[string]any{
		"status":                  "accepted",
		"checkpoint_count":        len(mapKeySet(baselineByID)),
		"unseen_checkpoint_count": len(mapKeySet(baselineByID)),
		"reason_codes":            []string{},
		"issues":                  []map[string]any{},
	}
	report["checkpoint_ids"] = sortedSetKeys(mapKeySet(baselineByID))
	report["required_metrics"] = append([]string{}, requiredMetricsList...)

	if len(metricIssues) > 0 {
		report["validation"].(map[string]any)["metrics_status"] = "incomplete"
		report["validation"].(map[string]any)["issues"] = metricIssues
		report["validation"].(map[string]any)["reason_codes"] = reasonCodes(metricIssues)
		report["promotion"].(map[string]any)["reason_codes"] = append([]string{}, reasonCodes(metricIssues)...)
		return report
	}

	report["validation"].(map[string]any)["metrics_status"] = "complete"
	report["checkpoints"] = checkpointReports
	report["macro_averages"] = macroAverages(checkpointReports, requiredMetricsList)
	report["promotion"] = promotionDecision(mode,
		anyToTypedMacroAverages(report["macro_averages"]),
		len(checkpointReports), resolvedThresholds)
	return report
}

// resolveThresholds 阈值解析（对应 Python _thresholds L211-220 收窄为 nil→默认，D-e1-2）
func resolveThresholds(value *MetaValidationThresholds) *MetaValidationThresholds {
	if value == nil {
		return DefaultMetaValidationThresholds()
	}
	return value
}

// emptyReport 空报告骨架（对应 Python _empty_report L223-247）
func emptyReport(mode string, thresholds *MetaValidationThresholds) map[string]any {
	return map[string]any{
		"schema_version": 1,
		"record_type":    "paired_improver_meta_validation",
		"mode":           mode,
		"validation": map[string]any{
			"status":                  "rejected",
			"checkpoint_count":        0,
			"unseen_checkpoint_count": 0,
			"reason_codes":            []string{},
			"issues":                  []map[string]any{},
		},
		"checkpoint_ids":     []string{},
		"required_metrics":   []string{},
		"checkpoints":        []map[string]any{},
		"macro_averages":     map[string]any{},
		"thresholds":         thresholds.AsDict(),
		"baseline_improver":  map[string]any{},
		"candidate_improver": map[string]any{},
		"promotion": map[string]any{
			"status":       "inconclusive",
			"scope":        "full_improver",
			"reason_codes": []string{},
		},
	}
}

// reject 拒绝报告（对应 Python _reject L250-255）
func reject(report map[string]any, code string, detail map[string]any) map[string]any {
	issue := map[string]any{"code": code}
	for key, value := range detail {
		issue[key] = value
	}
	report["validation"].(map[string]any)["issues"] = []map[string]any{issue}
	report["validation"].(map[string]any)["reason_codes"] = []string{code}
	report["promotion"].(map[string]any)["reason_codes"] = []string{code}
	return report
}

// requiredMetrics 提取必需指标（对应 Python _required_metrics L437-472）
func requiredMetrics(checkpointID string, record map[string]any, side string, requiredMetricsList []string) (map[string]float64, []map[string]any) {
	metrics, ok := record["metrics"].(map[string]any)
	if !ok {
		return map[string]float64{}, []map[string]any{
			{"code": "missing_required_metrics", "checkpoint_id": checkpointID, "side": side},
		}
	}
	output := map[string]float64{}
	issues := make([]map[string]any, 0)
	for _, metric := range requiredMetricsList {
		value, present := metrics[metric]
		if !present || value == nil {
			issues = append(issues, map[string]any{
				"code": "missing_required_metric", "checkpoint_id": checkpointID,
				"side": side, "metric": metric,
			})
			continue
		}
		if !isFiniteNumber(value) || (_RATE_METRICS[metric] && (pythonsem.PyFloat(value) < 0 || pythonsem.PyFloat(value) > 1)) {
			issues = append(issues, map[string]any{
				"code": "invalid_metric_value", "checkpoint_id": checkpointID,
				"side": side, "metric": metric,
			})
			continue
		}
		output[metric] = pythonsem.PyFloat(value)
	}
	return output, issues
}

// checkpointReport 单 checkpoint 对比报告（对应 Python _checkpoint_report L475-499）
func checkpointReport(
	checkpointID string,
	baseline map[string]any,
	baselineMetrics map[string]float64,
	candidateMetrics map[string]float64,
	requiredMetricsList []string,
) map[string]any {
	deltas := map[string]float64{}
	improvements := map[string]float64{}
	for _, metric := range requiredMetricsList {
		deltas[metric] = rounded(candidateMetrics[metric] - baselineMetrics[metric])
		if metric == "selection_regret" || _RATE_METRICS[metric] {
			// 越低越好：improvement = baseline - candidate
			improvements[metric] = rounded(baselineMetrics[metric] - candidateMetrics[metric])
		} else {
			improvements[metric] = rounded(candidateMetrics[metric] - baselineMetrics[metric])
		}
	}
	frozenContext := map[string]any{}
	for _, field := range _FROZEN_FIELDS {
		frozenContext[field] = baseline[field]
	}
	return map[string]any{
		"checkpoint_id":            checkpointID,
		"frozen_context":           frozenContext,
		"baseline_metrics":         copyFloats(baselineMetrics),
		"candidate_metrics":        copyFloats(candidateMetrics),
		"deltas":                   deltas,
		"directional_improvements": improvements,
	}
}

// macroAverages 宏平均（对应 Python _macro_averages L502-518）
func macroAverages(checkpoints []map[string]any, requiredMetricsList []string) map[string]any {
	output := map[string]any{}
	for _, metric := range requiredMetricsList {
		baselineSum := 0.0
		candidateSum := 0.0
		for _, item := range checkpoints {
			baselineSum += item["baseline_metrics"].(map[string]float64)[metric]
			candidateSum += item["candidate_metrics"].(map[string]float64)[metric]
		}
		baseline := baselineSum / float64(len(checkpoints))
		candidate := candidateSum / float64(len(checkpoints))
		lowerIsBetter := metric == "selection_regret" || _RATE_METRICS[metric]
		improvement := candidate - baseline
		if lowerIsBetter {
			improvement = baseline - candidate
		}
		direction := "higher_is_better"
		if lowerIsBetter {
			direction = "lower_is_better"
		}
		output[metric] = map[string]any{
			"baseline":    rounded(baseline),
			"candidate":   rounded(candidate),
			"delta":       rounded(candidate - baseline),
			"improvement": rounded(improvement),
			"direction":   direction,
		}
	}
	return output
}

// promotionDecision 晋升判定（对应 Python _promotion_decision L521-566，分支顺序照抄）
func promotionDecision(mode string, macroAverages map[string]map[string]any, checkpointCount int, thresholds *MetaValidationThresholds) map[string]any {
	reasons := make([]string, 0)
	insufficientEvidence := checkpointCount < thresholds.MinUnseenCheckpoints()
	if insufficientEvidence {
		reasons = append(reasons, "insufficient_unseen_checkpoints")
	}
	if macroAverages["top_m_gain"]["delta"].(float64) < -thresholds.TopMGainNonDegradationTolerance() {
		reasons = append(reasons, "top_m_gain_decreased")
	}
	if macroAverages["selection_regret"]["improvement"].(float64) < thresholds.MinSelectionRegretImprovement() {
		reasons = append(reasons, "selection_regret_not_improved")
	}
	if mode == "offline_rerank" {
		offlineReasons := append([]string{}, reasons...)
		rankerAssessment := "ineligible"
		if insufficientEvidence {
			rankerAssessment = "inconclusive"
		} else if len(offlineReasons) == 0 {
			rankerAssessment = "eligible"
		}
		return map[string]any{
			"status":                    "inconclusive",
			"scope":                     "ranker_evidence_only",
			"offline_ranker_assessment": rankerAssessment,
			"reason_codes":              append(offlineReasons, "offline_rerank_cannot_promote_full_improver"),
		}
	}
	if macroAverages["best_of_k_gain"]["delta"].(float64) < -thresholds.BestOfKGainNonDegradationTolerance() {
		reasons = append(reasons, "best_of_k_gain_decreased")
	}
	if macroAverages["final_harness_gain_per_budget"]["delta"].(float64) <
		-thresholds.FinalHarnessGainPerBudgetNonDegradationTolerance() {
		reasons = append(reasons, "final_harness_gain_per_budget_decreased")
	}
	if macroAverages["regression_failure_rate"]["delta"].(float64) > thresholds.MaxRegressionFailureRateIncrease() {
		reasons = append(reasons, "regression_failure_rate_increased")
	}
	if macroAverages["infrastructure_failure_rate"]["delta"].(float64) > thresholds.MaxInfrastructureFailureRateIncrease() {
		reasons = append(reasons, "infrastructure_failure_rate_increased")
	}
	status := "ineligible"
	if len(reasons) == 0 {
		status = "eligible"
		reasons = append(reasons, "promotion_thresholds_satisfied")
	}
	if insufficientEvidence {
		status = "inconclusive"
	}
	return map[string]any{
		"status":       status,
		"scope":        "full_improver",
		"reason_codes": reasons,
	}
}

// rounded 复刻 Python round(x, 12)
func rounded(value float64) float64 {
	return math.Round(value*1e12) / 1e12
}

// copyFloats 复制 float map
func copyFloats(source map[string]float64) map[string]float64 {
	out := make(map[string]float64, len(source))
	for key, value := range source {
		out[key] = value
	}
	return out
}

// mapKeySet map 键集合
func mapKeySet(m map[string]map[string]any) map[string]bool {
	out := map[string]bool{}
	for key := range m {
		out[key] = true
	}
	return out
}

// difference 集合差（对应 Python set - set）
func difference(left, right map[string]bool) map[string]bool {
	out := map[string]bool{}
	for key := range left {
		if !right[key] {
			out[key] = true
		}
	}
	return out
}

// stringSliceSetEqual 集合级相等（对应 Python set(baseline_ids) != set(candidate_ids)）
func stringSliceSetEqual(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	return len(setOf(left)) == len(setOf(right)) && len(intersect(setOf(left), setOf(right))) == len(setOf(left))
}

// anyToTypedMacroAverages 将 map[string]any 宏平均收窄为判定所需类型
func anyToTypedMacroAverages(value any) map[string]map[string]any {
	raw := value.(map[string]any)
	out := map[string]map[string]any{}
	for metric, item := range raw {
		entry := item.(map[string]any)
		out[metric] = entry
	}
	return out
}
