package evolving

import "testing"

// ──────────────────────────── 常量测试 ────────────────────────────

// TestTuneConstant_默认值 验证 TuneConstant 常量与 Python TuneConstant 对齐
func TestTuneConstant_默认值(t *testing.T) {
	// 默认值
	if DefaultExampleNum != 1 {
		t.Errorf("DefaultExampleNum 期望 1, 实际 %d", DefaultExampleNum)
	}
	if DefaultIterationNum != 3 {
		t.Errorf("DefaultIterationNum 期望 3, 实际 %d", DefaultIterationNum)
	}
	if DefaultMaxSampledExampleNum != 10 {
		t.Errorf("DefaultMaxSampledExampleNum 期望 10, 实际 %d", DefaultMaxSampledExampleNum)
	}
	if DefaultParallelNum != 1 {
		t.Errorf("DefaultParallelNum 期望 1, 实际 %d", DefaultParallelNum)
	}
	if DefaultMaxNumSampleErrorCases != 10 {
		t.Errorf("DefaultMaxNumSampleErrorCases 期望 10, 实际 %d", DefaultMaxNumSampleErrorCases)
	}
	if DefaultEarlyStopScore != 1.0 {
		t.Errorf("DefaultEarlyStopScore 期望 1.0, 实际 %f", DefaultEarlyStopScore)
	}

	// 合法范围
	if MinIterationNum != 1 {
		t.Errorf("MinIterationNum 期望 1, 实际 %d", MinIterationNum)
	}
	if MaxIterationNum != 20 {
		t.Errorf("MaxIterationNum 期望 20, 实际 %d", MaxIterationNum)
	}
	if MinParallelNum != 1 {
		t.Errorf("MinParallelNum 期望 1, 实际 %d", MinParallelNum)
	}
	if MaxParallelNum != 20 {
		t.Errorf("MaxParallelNum 期望 20, 实际 %d", MaxParallelNum)
	}
	if MinExampleNum != 0 {
		t.Errorf("MinExampleNum 期望 0, 实际 %d", MinExampleNum)
	}
	if MaxExampleNum != 20 {
		t.Errorf("MaxExampleNum 期望 20, 实际 %d", MaxExampleNum)
	}
}
