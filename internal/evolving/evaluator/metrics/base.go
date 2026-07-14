package metrics

import (
	"github.com/uapclaw/uapclaw-go/internal/evolving/dataset"
)

// ──────────────────────────── 结构体 ────────────────────────────

// Metric 评估指标抽象接口。
//
// 对应 Python: openjiuwen/agent_evolving/evaluator/metrics/base.py Metric
type Metric interface {
	// Name 指标标识名
	Name() string
	// HigherIsBetter 是否越高越好，默认 true
	HigherIsBetter() bool
	// Compute 计算单个样本的指标分数
	Compute(prediction, label any, opts ...MetricOption) (MetricResult, error)
	// ComputeBatch 批量计算指标分数
	ComputeBatch(predictions, labels []any, opts ...MetricOption) ([]MetricResult, error)
}

// metricContext 指标计算的上下文信息，通过 MetricOption 注入。
type metricContext struct {
	// question 查询上下文，对应 Python case.inputs
	question map[string]any
	// case_ 原始 Case
	case_ *dataset.Case
}

// MetricOption 指标计算的可选参数函数。
type MetricOption func(*metricContext)

// ──────────────────────────── 类型别名 ────────────────────────────

// MetricResult 指标计算结果，统一用 map 表示。
// 单指标：{"exact_match": 1.0}  多指标：{"precision": 0.8, "recall": 0.6}
//
// 对应 Python: MetricResult = Union[float, Dict[str, float]]
type MetricResult = map[string]float64

// ──────────────────────────── 导出函数 ────────────────────────────

// WithQuestion 设置查询上下文，对应 Python case.inputs。
func WithQuestion(q map[string]any) MetricOption {
	return func(mc *metricContext) { mc.question = q }
}

// WithCase 设置原始 Case。
func WithCase(c dataset.Case) MetricOption {
	return func(mc *metricContext) { mc.case_ = &c }
}

// DefaultComputeBatch 默认批量计算实现：逐个调用 Compute。
//
// 对应 Python: Metric.compute_batch() 默认实现
func DefaultComputeBatch(m Metric, predictions, labels []any, opts ...MetricOption) ([]MetricResult, error) {
	results := make([]MetricResult, len(predictions))
	for i := range predictions {
		result, err := m.Compute(predictions[i], labels[i], opts...)
		if err != nil {
			return nil, err
		}
		results[i] = result
	}
	return results, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// applyMetricOptions 应用选项并返回上下文。
func applyMetricOptions(opts ...MetricOption) *metricContext {
	mc := &metricContext{}
	for _, opt := range opts {
		opt(mc)
	}
	return mc
}
