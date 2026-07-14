package metrics

import (
	"fmt"
	"strings"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ExactMatchMetric 精确匹配或归一化匹配指标。
//
// 匹配返回 {"exact_match": 1.0}，不匹配返回 {"exact_match": 0.0}。
// normalize=true 时先归一化（小写+去空格+合并连续空格）再比较。
//
// 对应 Python: openjiuwen/agent_evolving/evaluator/metrics/exact_match.py ExactMatchMetric
type ExactMatchMetric struct {
	// normalize 是否归一化后比较
	normalize bool
}

// ExactMatchOption ExactMatchMetric 构造选项函数。
type ExactMatchOption func(*ExactMatchMetric)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewExactMatchMetric 创建 ExactMatchMetric 实例，默认 normalize=true。
func NewExactMatchMetric(opts ...ExactMatchOption) *ExactMatchMetric {
	m := &ExactMatchMetric{normalize: true}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// Name 返回指标名称 "exact_match"。
func (m *ExactMatchMetric) Name() string { return "exact_match" }

// HigherIsBetter 返回 true。
func (m *ExactMatchMetric) HigherIsBetter() bool { return true }

// Compute 计算精确匹配分数。
//
// 对应 Python: ExactMatchMetric.compute(prediction, label)
func (m *ExactMatchMetric) Compute(prediction, label any, opts ...MetricOption) (MetricResult, error) {
	_ = applyMetricOptions(opts...) // ExactMatch 不需要上下文

	var predStr, labelStr string
	if m.normalize {
		predStr = normalizeExactMatch(prediction)
		labelStr = normalizeExactMatch(label)
	} else {
		predStr = fmt.Sprintf("%v", prediction)
		labelStr = fmt.Sprintf("%v", label)
	}

	score := 0.0
	if predStr == labelStr {
		score = 1.0
	}
	return MetricResult{"exact_match": score}, nil
}

// ComputeBatch 批量计算精确匹配分数。
func (m *ExactMatchMetric) ComputeBatch(predictions, labels []any, opts ...MetricOption) ([]MetricResult, error) {
	return DefaultComputeBatch(m, predictions, labels, opts...)
}

// WithNormalize 设置是否归一化。
func WithNormalize(n bool) ExactMatchOption {
	return func(m *ExactMatchMetric) { m.normalize = n }
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// normalizeExactMatch 归一化输入：小写 + strip + 合并连续空格。
//
// 对应 Python: ExactMatchMetric._normalize(input_data)
func normalizeExactMatch(input any) string {
	result := strings.TrimSpace(strings.ToLower(fmt.Sprintf("%v", input)))
	return strings.Join(strings.Fields(result), " ")
}
