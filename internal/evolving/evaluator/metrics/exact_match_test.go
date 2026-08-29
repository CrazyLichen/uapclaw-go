package metrics

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/evolving/dataset"
)

// ──────────────────────────── 导出函数测试 ────────────────────────────

// TestExactMatchMetric_Name 测试指标名称
func TestExactMatchMetric_Name(t *testing.T) {
	m := NewExactMatchMetric()
	if m.Name() != "exact_match" {
		t.Errorf("期望 Name=exact_match, 实际=%s", m.Name())
	}
}

// TestExactMatchMetric_HigherIsBetter 测试 HigherIsBetter
func TestExactMatchMetric_HigherIsBetter(t *testing.T) {
	m := NewExactMatchMetric()
	if !m.HigherIsBetter() {
		t.Error("期望 HigherIsBetter=true")
	}
}

// TestExactMatchMetric_匹配 测试归一化匹配
func TestExactMatchMetric_匹配(t *testing.T) {
	m := NewExactMatchMetric()

	// 完全匹配
	result, err := m.Compute(context.Background(), "hello", "hello")
	if err != nil {
		t.Fatalf("不期望错误: %v", err)
	}
	if result["exact_match"] != 1.0 {
		t.Errorf("期望 exact_match=1.0, 实际=%f", result["exact_match"])
	}

	// 大小写不同，归一化后匹配
	result, err = m.Compute(context.Background(), "Hello World", "hello world")
	if err != nil {
		t.Fatalf("不期望错误: %v", err)
	}
	if result["exact_match"] != 1.0 {
		t.Errorf("归一化后期望 exact_match=1.0, 实际=%f", result["exact_match"])
	}

	// 多余空格，归一化后匹配
	result, err = m.Compute(context.Background(), "  hello   world  ", "hello world")
	if err != nil {
		t.Fatalf("不期望错误: %v", err)
	}
	if result["exact_match"] != 1.0 {
		t.Errorf("归一化后期望 exact_match=1.0, 实际=%f", result["exact_match"])
	}
}

// TestExactMatchMetric_不匹配 测试不匹配
func TestExactMatchMetric_不匹配(t *testing.T) {
	m := NewExactMatchMetric()

	result, err := m.Compute(context.Background(), "hello", "world")
	if err != nil {
		t.Fatalf("不期望错误: %v", err)
	}
	if result["exact_match"] != 0.0 {
		t.Errorf("期望 exact_match=0.0, 实际=%f", result["exact_match"])
	}
}

// TestExactMatchMetric_非归一化 测试非归一化模式
func TestExactMatchMetric_非归一化(t *testing.T) {
	m := NewExactMatchMetric(WithNormalize(false))

	// 大小写不同，非归一化不匹配
	result, err := m.Compute(context.Background(), "Hello", "hello")
	if err != nil {
		t.Fatalf("不期望错误: %v", err)
	}
	if result["exact_match"] != 0.0 {
		t.Errorf("非归一化期望 exact_match=0.0, 实际=%f", result["exact_match"])
	}

	// 完全匹配
	result, err = m.Compute(context.Background(), "hello", "hello")
	if err != nil {
		t.Fatalf("不期望错误: %v", err)
	}
	if result["exact_match"] != 1.0 {
		t.Errorf("期望 exact_match=1.0, 实际=%f", result["exact_match"])
	}
}

// TestExactMatchMetric_ComputeBatch 测试批量计算
func TestExactMatchMetric_ComputeBatch(t *testing.T) {
	m := NewExactMatchMetric()
	predictions := []any{"hello", "world"}
	labels := []any{"hello", "foo"}

	results, err := m.ComputeBatch(context.Background(), predictions, labels)
	if err != nil {
		t.Fatalf("不期望错误: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("期望 2 个结果, 实际=%d", len(results))
	}
	if results[0]["exact_match"] != 1.0 {
		t.Errorf("期望 results[0] exact_match=1.0, 实际=%f", results[0]["exact_match"])
	}
	if results[1]["exact_match"] != 0.0 {
		t.Errorf("期望 results[1] exact_match=0.0, 实际=%f", results[1]["exact_match"])
	}
}

// TestExactMatchMetric_map类型深度比较 测试 map 类型使用 reflect.DeepEqual
func TestExactMatchMetric_map类型深度比较(t *testing.T) {
	m := NewExactMatchMetric()

	// 相同 map（key 顺序不同）— DeepEqual 应返回 true
	pred := map[string]any{"answer": "2", "output": "1"}
	label := map[string]any{"output": "1", "answer": "2"}
	result, err := m.Compute(context.Background(), pred, label)
	if err != nil {
		t.Fatalf("不期望错误: %v", err)
	}
	if result["exact_match"] != 1.0 {
		t.Errorf("期望 map 深度匹配 exact_match=1.0, 实际=%f", result["exact_match"])
	}

	// 不同 map
	pred2 := map[string]any{"answer": "2"}
	label2 := map[string]any{"answer": "3"}
	result2, err := m.Compute(context.Background(), pred2, label2)
	if err != nil {
		t.Fatalf("不期望错误: %v", err)
	}
	if result2["exact_match"] != 0.0 {
		t.Errorf("期望 map 不匹配 exact_match=0.0, 实际=%f", result2["exact_match"])
	}
}

// TestExactMatchMetric_混合类型 测试字符串 vs 非字符串混合比较
func TestExactMatchMetric_混合类型(t *testing.T) {
	m := NewExactMatchMetric()

	// 字符串 vs map — 类型不同，DeepEqual 返回 false
	result, err := m.Compute(context.Background(), "hello", map[string]any{"answer": "hello"})
	if err != nil {
		t.Fatalf("不期望错误: %v", err)
	}
	if result["exact_match"] != 0.0 {
		t.Errorf("期望不同类型不匹配 exact_match=0.0, 实际=%f", result["exact_match"])
	}

	// 数字比较
	result2, err := m.Compute(context.Background(), 42, 42)
	if err != nil {
		t.Fatalf("不期望错误: %v", err)
	}
	if result2["exact_match"] != 1.0 {
		t.Errorf("期望数字匹配 exact_match=1.0, 实际=%f", result2["exact_match"])
	}
}

// ──────────────────────────── WithQuestion / WithCase 测试 ────────────────────────────

// TestWithQuestion 测试 WithQuestion 选项设置 question 上下文
func TestWithQuestion(t *testing.T) {
	q := map[string]any{"query": "测试问题"}
	mc := applyMetricOptions(WithQuestion(q))
	if mc.question == nil {
		t.Fatal("question 不应为 nil")
	}
	if mc.question["query"] != "测试问题" {
		t.Errorf("question[query] = %v, want 测试问题", mc.question["query"])
	}
}

// TestWithCase 测试 WithCase 选项设置 case 上下文
func TestWithCase(t *testing.T) {
	c := dataset.Case{Inputs: map[string]any{"q": "hello"}}
	mc := applyMetricOptions(WithCase(c))
	if mc.case_ == nil {
		t.Fatal("case 不应为 nil")
	}
	if mc.case_.Inputs["q"] != "hello" {
		t.Errorf("case.Inputs[q] = %v, want hello", mc.case_.Inputs["q"])
	}
}

// TestApplyMetricOptions_多个选项 测试多个 MetricOption 同时生效
func TestApplyMetricOptions_多个选项(t *testing.T) {
	q := map[string]any{"q": "test"}
	c := dataset.Case{Inputs: map[string]any{"q": "test"}}
	mc := applyMetricOptions(WithQuestion(q), WithCase(c))
	if mc.question == nil || mc.case_ == nil {
		t.Error("question 和 case 都应设置")
	}
}

// TestApplyMetricOptions_无选项 测试无选项时上下文字段为零值
func TestApplyMetricOptions_无选项(t *testing.T) {
	mc := applyMetricOptions()
	if mc.question != nil {
		t.Error("无选项时 question 应为 nil")
	}
	if mc.case_ != nil {
		t.Error("无选项时 case 应为 nil")
	}
}

// TestDefaultComputeBatch_通过ExactMatch 测试 DefaultComputeBatch 委托到 Compute
func TestDefaultComputeBatch_通过ExactMatch(t *testing.T) {
	m := NewExactMatchMetric()
	predictions := []any{"a", "b", "c"}
	labels := []any{"a", "x", "c"}
	results, err := DefaultComputeBatch(m, context.Background(), predictions, labels)
	if err != nil {
		t.Fatalf("不期望错误: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("期望 3 个结果, 实际=%d", len(results))
	}
	if results[0]["exact_match"] != 1.0 {
		t.Errorf("results[0] exact_match = %f, want 1.0", results[0]["exact_match"])
	}
	if results[1]["exact_match"] != 0.0 {
		t.Errorf("results[1] exact_match = %f, want 0.0", results[1]["exact_match"])
	}
	if results[2]["exact_match"] != 1.0 {
		t.Errorf("results[2] exact_match = %f, want 1.0", results[2]["exact_match"])
	}
}
