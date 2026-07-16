package dataset

import (
	"fmt"
	"testing"
)

// ──────────────────────────── 导出函数测试 ────────────────────────────

// TestNewCaseLoader 测试 CaseLoader 构造
func TestNewCaseLoader(t *testing.T) {
	cases := []Case{
		*NewCase(map[string]any{"q": "1"}, map[string]any{"a": "1"}),
		*NewCase(map[string]any{"q": "2"}, map[string]any{"a": "2"}),
	}
	loader := NewCaseLoader(cases)
	if loader.Len() != 2 {
		t.Errorf("期望 Len=2, 实际=%d", loader.Len())
	}
	if len(loader.Cases()) != 2 {
		t.Errorf("期望 Cases 长度 2, 实际=%d", len(loader.Cases()))
	}
}

// TestCaseLoader_Split 测试拆分
func TestCaseLoader_Split(t *testing.T) {
	cases := make([]Case, 10)
	for i := range cases {
		cases[i] = *NewCase(map[string]any{"q": i}, map[string]any{"a": i})
	}
	loader := NewCaseLoader(cases)

	train, val, err := loader.Split(0.8)
	if err != nil {
		t.Fatalf("期望无错误, 实际=%v", err)
	}
	// 10 个样本 80% 拆分 → train 8, val 2
	if train.Len() != 8 {
		t.Errorf("期望 train.Len=8, 实际=%d", train.Len())
	}
	if val.Len() != 2 {
		t.Errorf("期望 val.Len=2, 实际=%d", val.Len())
	}
}

// TestCaseLoader_Split_空样本 测试空 CaseLoader 拆分
func TestCaseLoader_Split_空样本(t *testing.T) {
	loader := NewCaseLoader(nil)
	train, val, err := loader.Split(0.8)
	if err != nil {
		t.Fatalf("期望无错误, 实际=%v", err)
	}
	if train.Len() != 0 {
		t.Errorf("期望 train.Len=0, 实际=%d", train.Len())
	}
	if val.Len() != 0 {
		t.Errorf("期望 val.Len=0, 实际=%d", val.Len())
	}
}

// TestCaseLoader_Split_Ratio校验 测试 ratio 范围校验
func TestCaseLoader_Split_Ratio校验(t *testing.T) {
	loader := NewCaseLoader([]Case{*NewCase(map[string]any{"q": 1}, map[string]any{"a": 1})})

	_, _, err := loader.Split(-0.1)
	if err == nil {
		t.Error("期望 ratio < 0 返回错误，实际无错误")
	}

	_, _, err = loader.Split(1.5)
	if err == nil {
		t.Error("期望 ratio > 1 返回错误，实际无错误")
	}

	// 边界值应正常
	_, _, err = loader.Split(0.0)
	if err != nil {
		t.Errorf("期望 ratio=0.0 无错误, 实际=%v", err)
	}
	_, _, err = loader.Split(1.0)
	if err != nil {
		t.Errorf("期望 ratio=1.0 无错误, 实际=%v", err)
	}
}

// TestCaseLoader_Split_可复现 测试相同 seed 得到相同结果
func TestCaseLoader_Split_可复现(t *testing.T) {
	cases := make([]Case, 100)
	for i := range cases {
		cases[i] = *NewCase(map[string]any{"q": i}, map[string]any{"a": i}, WithCaseID(fmt.Sprintf("case-%d", i)))
	}
	loader := NewCaseLoader(cases)

	train1, val1, _ := loader.Split(0.8, 42)
	train2, val2, _ := loader.Split(0.8, 42)

	// 相同 seed 应产生相同的拆分结果
	for i := range train1.Cases() {
		if train1.Cases()[i].CaseID != train2.Cases()[i].CaseID {
			t.Errorf("相同 seed 下 train[%d] 不一致: %s vs %s", i, train1.Cases()[i].CaseID, train2.Cases()[i].CaseID)
			break
		}
	}
	for i := range val1.Cases() {
		if val1.Cases()[i].CaseID != val2.Cases()[i].CaseID {
			t.Errorf("相同 seed 下 val[%d] 不一致: %s vs %s", i, val1.Cases()[i].CaseID, val2.Cases()[i].CaseID)
			break
		}
	}
}

// TestCaseLoader_Split_先打乱 测试 Split 内部先打乱再拆分
func TestCaseLoader_Split_先打乱(t *testing.T) {
	cases := make([]Case, 100)
	for i := range cases {
		cases[i] = *NewCase(map[string]any{"q": i}, map[string]any{"a": i}, WithCaseID(fmt.Sprintf("case-%d", i)))
	}
	loader := NewCaseLoader(cases)

	train, _, _ := loader.Split(0.8, 42)

	// 高概率不会与原始顺序完全一致
	sameCount := 0
	for i := range train.Cases() {
		if train.Cases()[i].CaseID == fmt.Sprintf("case-%d", i) {
			sameCount++
		}
	}
	if sameCount == train.Len() {
		t.Error("期望 Split 后序列与原始不同，但完全一致（未 shuffle）")
	}
}

// TestCaseLoader_ShuffleCases 测试打乱
func TestCaseLoader_ShuffleCases(t *testing.T) {
	cases := make([]Case, 100)
	for i := range cases {
		cases[i] = *NewCase(map[string]any{"q": i}, map[string]any{"a": i}, WithCaseID(fmt.Sprintf("case-%d", i)))
	}
	loader := NewCaseLoader(cases)

	original := make([]Case, len(loader.Cases()))
	copy(original, loader.Cases())

	loader.ShuffleCases()

	// 打乱后长度不变
	if loader.Len() != 100 {
		t.Errorf("期望 Len=100, 实际=%d", loader.Len())
	}
	// 高概率不会完全一致（100 个元素打乱后与原序列相同的概率极低）
	sameCount := 0
	for i := range original {
		if original[i].CaseID == loader.Cases()[i].CaseID {
			sameCount++
		}
	}
	if sameCount == 100 {
		t.Error("期望 ShuffleCases 后序列与原始不同，但完全一致")
	}
}

// ──────────────────────────── 非导出函数测试 ────────────────────────────

// TestShuffleCases_不修改原列表 测试 shuffleCases 返回副本
func TestShuffleCases_不修改原列表(t *testing.T) {
	cases := make([]Case, 10)
	for i := range cases {
		cases[i] = *NewCase(map[string]any{"q": i}, map[string]any{"a": i}, WithCaseID(fmt.Sprintf("case-%d", i)))
	}
	original := make([]Case, len(cases))
	copy(original, cases)

	_ = shuffleCases(cases, 0)

	// 原列表应保持不变
	for i := range cases {
		if cases[i].CaseID != original[i].CaseID {
			t.Error("shuffleCases 不应修改原列表")
			break
		}
	}
}
