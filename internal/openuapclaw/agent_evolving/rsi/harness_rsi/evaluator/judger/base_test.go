package judger

import "testing"

// TestIsExecutionOnlyEvaluation 遗留完成态评分识别全分支。
func TestIsExecutionOnlyEvaluation(t *testing.T) {
	if !IsExecutionOnlyEvaluation(map[string]any{"method": "none"}) {
		t.Fatal("method=none 应识别")
	}
	if !IsExecutionOnlyEvaluation(map[string]any{"metadata": map[string]any{"rule_engine_status": "backend_completed"}}) {
		t.Fatal("backend_completed 应识别")
	}
	if IsExecutionOnlyEvaluation(map[string]any{"method": "rubric", "metadata": map[string]any{"rule_engine_status": "ok"}}) {
		t.Fatal("正常评分不应识别")
	}
	if IsExecutionOnlyEvaluation(map[string]any{}) {
		t.Fatal("空评估不应识别")
	}
}
