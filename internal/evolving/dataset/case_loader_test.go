package dataset

import "testing"

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

	train, val := loader.Split(0.8)
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
	train, val := loader.Split(0.8)
	if train.Len() != 0 {
		t.Errorf("期望 train.Len=0, 实际=%d", train.Len())
	}
	if val.Len() != 0 {
		t.Errorf("期望 val.Len=0, 实际=%d", val.Len())
	}
}

// TestCaseLoader_ShuffleCases 测试打乱
func TestCaseLoader_ShuffleCases(t *testing.T) {
	cases := make([]Case, 100)
	for i := range cases {
		cases[i] = *NewCase(map[string]any{"q": i}, map[string]any{"a": i}, WithCaseID(string(rune(i))))
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
