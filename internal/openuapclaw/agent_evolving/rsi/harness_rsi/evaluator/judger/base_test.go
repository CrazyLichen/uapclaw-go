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

// TestFailureResult 执行失败规范化 0 分结果（Python base.py _failure_result）。
func TestFailureResult(t *testing.T) {
	dummy := &stubJudger{method: "llm_as_judge"}
	result := dummy.FailureResult("boom")
	if result.Method != "llm_as_judge" || result.Score != 0.0 || result.Passed {
		t.Fatalf("应为 method 携带 0 分失败结果: %+v", result)
	}
	if result.Reason != "boom" {
		t.Fatalf("reason 应为原错误: %q", result.Reason)
	}
	empty := dummy.FailureResult("")
	if empty.Reason != "case execution failed" {
		t.Fatalf("空错误应回落缺省文案: %q", empty.Reason)
	}
}

// TestReferenceAnswer 参考答案向后兼容别名逐层回退（Python base.py _reference_answer）。
func TestReferenceAnswer(t *testing.T) {
	if got := ReferenceAnswer(map[string]any{"reference": map[string]any{"answer": "A"}}); got != "A" {
		t.Fatalf("reference.answer 优先: %v", got)
	}
	if got := ReferenceAnswer(map[string]any{"reference": map[string]any{"expected_output": "B"}}); got != "B" {
		t.Fatalf("reference.expected_output 次选: %v", got)
	}
	if got := ReferenceAnswer(map[string]any{"reference_answer": "C"}); got != "C" {
		t.Fatalf("顶层 reference_answer 回退: %v", got)
	}
	if got := ReferenceAnswer(map[string]any{"expected_output": "D"}); got != "D" {
		t.Fatalf("顶层 expected_output 回退: %v", got)
	}
	if got := ReferenceAnswer(map[string]any{}); got != nil {
		t.Fatalf("无答案应返回 nil: %v", got)
	}
}

// TestComparableResponse 仅解包原生最终答案信封（Python base.py _comparable_response）。
func TestComparableResponse(t *testing.T) {
	envelope := map[string]any{"result_type": "answer", "output": "final"}
	if got := ComparableResponse(envelope, "expect"); got != "final" {
		t.Fatalf("answer 信封应解包: %v", got)
	}
	if got := ComparableResponse(map[string]any{"result_type": "task"}, "expect"); got == nil {
		t.Fatal("非 answer 信封不解包")
	}
	if got := ComparableResponse("plain", "expect"); got != "plain" {
		t.Fatalf("非 dict 响应原样返回: %v", got)
	}
	if got := ComparableResponse(envelope, 42); got == nil {
		t.Fatal("期望非字符串时不解包")
	}
}

// TestJudgeResult_不可变字段 JudgeResult 结构契约。
func TestJudgeResult_不可变字段(t *testing.T) {
	result := JudgeResult{Method: "exact_match", Score: 1.0, Passed: true, Reason: "ok", Metadata: map[string]any{"k": "v"}}
	if result.Metadata["k"] != "v" {
		t.Fatalf("metadata 应承载: %+v", result)
	}
}

// stubJudger 测试桩。
type stubJudger struct {
	method string
}

func (s *stubJudger) MethodName() string          { return s.method }
func (s *stubJudger) FailureResult(err string) *JudgeResult {
	return failureResult(s.method, err)
}
