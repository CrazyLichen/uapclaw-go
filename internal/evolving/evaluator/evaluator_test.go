package evaluator

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/evolving/dataset"
	"github.com/uapclaw/uapclaw-go/internal/evolving/evaluator/metrics"
)

// ──────────────────────────── 导出函数测试 ────────────────────────────

// TestNewDefaultEvaluator_空配置 测试空配置构造
func TestNewDefaultEvaluator_空配置(t *testing.T) {
	_, err := NewDefaultEvaluator(
		schema.ModelClientConfig{},
		schema.ModelRequestConfig{},
		"",
	)
	if err == nil {
		t.Error("期望空 ModelClientConfig 时返回错误")
	}
}

// TestNewDefaultEvaluator_有效配置 测试有效配置构造
func TestNewDefaultEvaluator_有效配置(t *testing.T) {
	cfg := schema.NewModelClientConfig("llm_OpenAI", "test-key", "http://localhost:11434/v1")
	e, err := NewDefaultEvaluator(
		*cfg,
		schema.ModelRequestConfig{ModelName: "test-model"},
		"",
	)
	if err != nil {
		t.Fatalf("不期望错误: %v", err)
	}
	if e == nil {
		t.Fatal("期望返回非 nil DefaultEvaluator")
	}
	if e.parser == nil {
		t.Error("期望 parser 已初始化")
	}
}

// TestMetricEvaluator_Evaluate 测试多指标聚合
func TestMetricEvaluator_Evaluate(t *testing.T) {
	em := metrics.NewExactMatchMetric()
	me := NewMetricEvaluator([]metrics.Metric{em}, "mean")

	case_ := dataset.NewCase(
		map[string]any{"query": "1+1"},
		map[string]any{"answer": "2"},
	)
	// predict 和 label 必须相同才能 exact match（都是 map[string]any）
	predict := map[string]any{"answer": "2"}

	ec, err := me.Evaluate(context.Background(), *case_, predict)
	if err != nil {
		t.Fatalf("不期望错误: %v", err)
	}
	if ec.GetScore() != 1.0 {
		t.Errorf("期望 Score=1.0, 实际=%f", ec.GetScore())
	}
	if ec.PerMetric["exact_match"] != 1.0 {
		t.Errorf("期望 PerMetric[exact_match]=1.0, 实际=%f", ec.PerMetric["exact_match"])
	}
}

// TestMetricEvaluator_Evaluate_不匹配 测试不匹配场景
func TestMetricEvaluator_Evaluate_不匹配(t *testing.T) {
	em := metrics.NewExactMatchMetric()
	me := NewMetricEvaluator([]metrics.Metric{em}, "mean")

	case_ := dataset.NewCase(
		map[string]any{"query": "1+1"},
		map[string]any{"answer": "2"},
	)
	predict := map[string]any{"output": "3"}

	ec, err := me.Evaluate(context.Background(), *case_, predict)
	if err != nil {
		t.Fatalf("不期望错误: %v", err)
	}
	if ec.GetScore() != 0.0 {
		t.Errorf("期望 Score=0.0, 实际=%f", ec.GetScore())
	}
}

// TestBatchEvaluate_长度不匹配 测试 cases 和 predicts 长度不一致报错
func TestBatchEvaluate_长度不匹配(t *testing.T) {
	me := NewMetricEvaluator([]metrics.Metric{metrics.NewExactMatchMetric()}, "mean")

	cases := []dataset.Case{*dataset.NewCase(map[string]any{"q": "1"}, map[string]any{"a": "1"})}
	predicts := []map[string]any{{"output": "1"}, {"output": "2"}}

	_, err := me.BatchEvaluate(context.Background(), cases, predicts, 1)
	if err == nil {
		t.Error("期望长度不匹配时返回错误")
	}
}

// TestBatchEvaluate_并行测试 测试并行批量评估
func TestBatchEvaluate_并行测试(t *testing.T) {
	me := NewMetricEvaluator([]metrics.Metric{metrics.NewExactMatchMetric()}, "mean")

	cases := []dataset.Case{
		*dataset.NewCase(map[string]any{"q": "1"}, map[string]any{"a": "hello"}),
		*dataset.NewCase(map[string]any{"q": "2"}, map[string]any{"a": "world"}),
	}
	predicts := []map[string]any{
		{"a": "hello"},
		{"a": "wrong"},
	}

	results, err := me.BatchEvaluate(context.Background(), cases, predicts, 2)
	if err != nil {
		t.Fatalf("不期望错误: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("期望 2 个结果, 实际=%d", len(results))
	}
	if results[0].GetScore() != 1.0 {
		t.Errorf("期望 results[0].GetScore()=1.0, 实际=%f", results[0].GetScore())
	}
	if results[1].Score != 0.0 {
		t.Errorf("期望 results[1].Score=0.0, 实际=%f", results[1].Score)
	}
}

// TestAggScore_mean 测试 mean 聚合
func TestAggScore_mean(t *testing.T) {
	result := aggScore([]float64{1.0, 0.0, 0.5}, "mean")
	if result != 0.5 {
		t.Errorf("期望 mean=0.5, 实际=%f", result)
	}
}

// TestAggScore_first 测试 first 聚合
func TestAggScore_first(t *testing.T) {
	result := aggScore([]float64{1.0, 0.0, 0.5}, "first")
	if result != 1.0 {
		t.Errorf("期望 first=1.0, 实际=%f", result)
	}
}

// TestAggScore_空切片 测试空切片
func TestAggScore_空切片(t *testing.T) {
	result := aggScore([]float64{}, "mean")
	if result != 0.0 {
		t.Errorf("期望空切片=0.0, 实际=%f", result)
	}
}

// TestAggScore_未知聚合方式 测试未知聚合方式默认 mean
func TestAggScore_未知聚合方式(t *testing.T) {
	result := aggScore([]float64{1.0, 0.0}, "unknown")
	if result != 0.5 {
		t.Errorf("期望未知聚合方式默认 mean=0.5, 实际=%f", result)
	}
}

// TestIsPassResult 测试 metrics.IsPassResult 判断逻辑
func TestIsPassResult(t *testing.T) {
	tests := []struct {
		input    any
		expected bool
	}{
		{true, true},
		{false, false},
		{"true", true},
		{"True", true},
		{"TRUE", true},
		{"false", false},
		{nil, false},
		{1, false},
	}
	for _, tt := range tests {
		result := metrics.IsPassResult(tt.input)
		if result != tt.expected {
			t.Errorf("IsPassResult(%v) = %v, 期望 %v", tt.input, result, tt.expected)
		}
	}
}

// TestMapBoolToScore 测试 mapBoolToScore 映射
func TestMapBoolToScore(t *testing.T) {
	if mapBoolToScore(true) != 1.0 {
		t.Errorf("mapBoolToScore(true) 期望 1.0")
	}
	if mapBoolToScore(false) != 0.0 {
		t.Errorf("mapBoolToScore(false) 期望 0.0")
	}
	if mapBoolToScore("true") != 1.0 {
		t.Errorf("mapBoolToScore(\"true\") 期望 1.0")
	}
	if mapBoolToScore(nil) != 0.0 {
		t.Errorf("mapBoolToScore(nil) 期望 0.0")
	}
}

// TestSafeConvert 测试 safeConvert 安全转换
func TestSafeConvert(t *testing.T) {
	if safeConvert(0.5) != 0.5 {
		t.Errorf("safeConvert(0.5) 期望 0.5")
	}
	if safeConvert(1.0) != 1.0 {
		t.Errorf("safeConvert(1.0) 期望 1.0")
	}
}

// TestMetricEvaluator_多指标聚合 测试多指标场景
// 使用自定义指标模拟不同分数的聚合
func TestMetricEvaluator_多指标聚合(t *testing.T) {
	// 使用两个 ExactMatchMetric 但传不同输入，一个匹配一个不匹配
	em := metrics.NewExactMatchMetric()
	me := NewMetricEvaluator([]metrics.Metric{em}, "mean")

	case_ := dataset.NewCase(
		map[string]any{"query": "1+1"},
		map[string]any{"answer": "2"},
	)
	predict := map[string]any{"answer": "2"}

	ec, err := me.Evaluate(context.Background(), *case_, predict)
	if err != nil {
		t.Fatalf("不期望错误: %v", err)
	}
	// 单指标匹配 → mean = 1.0
	if ec.GetScore() != 1.0 {
		t.Errorf("期望 Score=1.0, 实际=%f", ec.GetScore())
	}
}

// TestMetricEvaluator_多指标聚合_混合分数 测试多指标不同分数的 mean 聚合
func TestMetricEvaluator_多指标聚合_混合分数(t *testing.T) {
	// 创建一个总是返回固定分数的 mock 指标
	m1 := &fixedScoreMetric{name: "metric_a", score: 1.0}
	m2 := &fixedScoreMetric{name: "metric_b", score: 0.0}
	me := NewMetricEvaluator([]metrics.Metric{m1, m2}, "mean")

	case_ := dataset.NewCase(
		map[string]any{"query": "test"},
		map[string]any{"answer": "test"},
	)
	predict := map[string]any{"answer": "test"}

	ec, err := me.Evaluate(context.Background(), *case_, predict)
	if err != nil {
		t.Fatalf("不期望错误: %v", err)
	}
	// (1.0 + 0.0) / 2 = 0.5
	if ec.GetScore() != 0.5 {
		t.Errorf("期望 Score=0.5, 实际=%f", ec.GetScore())
	}
}

// TestMetricEvaluator_first聚合 测试 first 聚合方式
func TestMetricEvaluator_first聚合(t *testing.T) {
	m1 := &fixedScoreMetric{name: "metric_a", score: 1.0}
	m2 := &fixedScoreMetric{name: "metric_b", score: 0.0}
	me := NewMetricEvaluator([]metrics.Metric{m1, m2}, "first")

	case_ := dataset.NewCase(
		map[string]any{"query": "test"},
		map[string]any{"answer": "test"},
	)
	predict := map[string]any{"answer": "test"}

	ec, err := me.Evaluate(context.Background(), *case_, predict)
	if err != nil {
		t.Fatalf("不期望错误: %v", err)
	}
	// first 取第一个 metric 的分数 → 1.0
	if ec.GetScore() != 1.0 {
		t.Errorf("期望 Score=1.0 (first), 实际=%f", ec.GetScore())
	}
}

// fixedScoreMetric 固定分数的测试 mock 指标
type fixedScoreMetric struct {
	name  string
	score float64
}

func (f *fixedScoreMetric) Name() string         { return f.name }
func (f *fixedScoreMetric) HigherIsBetter() bool { return true }
func (f *fixedScoreMetric) Compute(_ context.Context, _, _ any, _ ...metrics.MetricOption) (metrics.MetricResult, error) {
	return metrics.MetricResult{f.name: f.score}, nil
}
func (f *fixedScoreMetric) ComputeBatch(_ context.Context, _, _ []any, _ ...metrics.MetricOption) ([]metrics.MetricResult, error) {
	return nil, nil
}
