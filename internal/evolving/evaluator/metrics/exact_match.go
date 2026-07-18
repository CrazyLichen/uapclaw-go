package metrics

import (
	"context"
	"reflect"
	"strings"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ExactMatchMetric 精确匹配或归一化匹配指标。
//
// 匹配返回 {"exact_match": 1.0}，不匹配返回 {"exact_match": 0.0}。
// normalize=true 时先归一化（小写+去空格+合并连续空格）再比较。
// 字符串类型走 normalize 后 == 比较，非字符串类型走 reflect.DeepEqual 深度比较。
//
// 对应 Python: openjiuwen/agent_evolving/evaluator/metrics/exact_match.py ExactMatchMetric
type ExactMatchMetric struct {
	// normalize 是否归一化后比较
	normalize bool
}

// ExactMatchOption ExactMatchMetric 构造选项函数。
type ExactMatchOption func(*ExactMatchMetric)

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

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
// 字符串类型：normalize=true 时归一化后比较，normalize=false 时原值比较。
// 非字符串类型（map/dict/number 等）：使用 reflect.DeepEqual 深度比较。
//
// 对应 Python: ExactMatchMetric.compute(prediction, label)
func (m *ExactMatchMetric) Compute(ctx context.Context, prediction, label any, opts ...MetricOption) (MetricResult, error) {
	_ = applyMetricOptions(opts...) // ExactMatch 不需要上下文

	var score float64
	// 字符串类型走 normalize 或原值比较
	predStr, predIsStr := prediction.(string)
	labelStr, labelIsStr := label.(string)
	if predIsStr && labelIsStr {
		if m.normalize {
			score = boolToScore(normalizeExactMatch(predStr) == normalizeExactMatch(labelStr))
		} else {
			score = boolToScore(predStr == labelStr)
		}
	} else {
		// 非字符串类型走 reflect.DeepEqual 深度比较
		score = boolToScore(reflect.DeepEqual(prediction, label))
	}

	return MetricResult{"exact_match": score}, nil
}

// ComputeBatch 批量计算精确匹配分数。
func (m *ExactMatchMetric) ComputeBatch(ctx context.Context, predictions, labels []any, opts ...MetricOption) ([]MetricResult, error) {
	return DefaultComputeBatch(m, ctx, predictions, labels, opts...)
}

// WithNormalize 设置是否归一化。
func WithNormalize(n bool) ExactMatchOption {
	return func(m *ExactMatchMetric) { m.normalize = n }
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// normalizeExactMatch 归一化字符串：小写 + strip + 合并连续空格。
//
// 对应 Python: ExactMatchMetric._normalize(input_data)
func normalizeExactMatch(input string) string {
	result := strings.TrimSpace(strings.ToLower(input))
	return strings.Join(strings.Fields(result), " ")
}

// boolToScore 将 bool 映射为 1.0 或 0.0。
func boolToScore(match bool) float64 {
	if match {
		return 1.0
	}
	return 0.0
}
