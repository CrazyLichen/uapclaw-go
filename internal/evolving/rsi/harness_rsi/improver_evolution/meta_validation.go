// Package improver_evolution 中 meta_validation 部分：候选 Improver 策略的确定性配对校验。
// 刻意只消费已物化的 checkpoint 结果，不运行 Improver、不改动策略状态、不发布策略版本。
//
// 对应 Python 代码：openjiuwen/rsi/harness_rsi/improver_evolution/meta_validation.py
package improver_evolution

import "math"

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
