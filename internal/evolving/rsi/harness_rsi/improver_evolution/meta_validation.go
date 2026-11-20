// Package improver_evolution 中 meta_validation 部分：候选 Improver 策略的确定性配对校验。
// 刻意只消费已物化的 checkpoint 结果，不运行 Improver、不改动策略状态、不发布策略版本。
//
// 对应 Python 代码：openjiuwen/rsi/harness_rsi/improver_evolution/meta_validation.py
package improver_evolution

import (
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
		return nil, errNew("min_unseen_checkpoints must be an integer of at least 1")
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
			return nil, errNew(name + " must be a finite non-negative number")
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
		if !pyEqual(split, "meta_test") {
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
		if hasBase && hasCand && !pyEqual(baseValue, candValue) {
			issues = append(issues, map[string]any{
				"code":          "frozen_field_mismatch",
				"checkpoint_id": checkpointID,
				"field":         field,
			})
		}
	}
	if pyEqual(baseline["k"], 1) && pyEqual(candidate["k"], 1) {
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
	if isPositiveInt(k) && isPositiveInt(topM) && pyInt(topM) > pyInt(k) {
		issues = append(issues, map[string]any{
			"code": "invalid_frozen_field", "checkpoint_id": checkpointID,
			"field": "top_m", "reason": "top_m_exceeds_k",
		})
	}
	for _, field := range []string{"token_budget", "tool_budget", "runtime_budget"} {
		value := record[field]
		if value != nil && (!isFiniteNumber(value) || pyFloat(value) < 0) {
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
	return !isBool(value) && isInt(value) && pyInt(value) > 0
}

// isFiniteNumber 有限数判定且非布尔（对应 Python _is_finite_number L573-574）
func isFiniteNumber(value any) bool {
	if isBool(value) {
		return false
	}
	return isNumber(value)
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
