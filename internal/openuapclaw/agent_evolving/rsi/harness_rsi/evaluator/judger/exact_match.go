// exact_match.go 精确匹配判分器。
// Python: openjiuwen/rsi/harness_rsi/evaluator/judger/exact_match.py（61 行全量对齐）。

package judger

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi/harness_rsi/evaluator/evalerrors"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ExactMatchJudger 与用例参考答案严格相等即得分。
// Python: exact_match.py:17 ExactMatchJudger(EvaluationJudger)。
type ExactMatchJudger struct{}

var _ EvaluationJudger = (*ExactMatchJudger)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// MethodName 方法标识（Python 类属性 method = "exact_match"）。
func (j *ExactMatchJudger) MethodName() string { return "exact_match" }

// ValidateCase 仅支持参考答案判分。
// Python: exact_match.py:21 validate_case。
func (j *ExactMatchJudger) ValidateCase(caseMapping map[string]any) error {
	reference, _ := caseMapping["reference"].(map[string]any)
	if reference == nil {
		reference = map[string]any{}
	}
	_, hasRubric := reference["rubric"]
	_, hasFiles := reference["files"]
	_, hasSwebench := caseMapping["swebench"]
	if hasRubric || hasFiles || hasSwebench {
		return evalerrors.NewEvaluationInfrastructureError(
			"exact_match only supports reference answers, not rubric or file grading")
	}
	if ReferenceAnswer(caseMapping) == nil {
		return evalerrors.NewEvaluationInfrastructureError(
			"exact_match cannot score this case: a reference answer is required")
	}
	return nil
}

// Judge 按严格相等打分。
// Python: exact_match.py:32 judge。
func (j *ExactMatchJudger) Judge(_ context.Context, caseMapping map[string]any,
	executionResult CaseExecutionView, outputDir string, _ BaselineContextView) (*JudgeResult, error) {
	_ = outputDir // Python: _ = output_dir
	if err := j.ValidateCase(caseMapping); err != nil {
		return nil, err
	}
	expected := ReferenceAnswer(caseMapping)
	if executionResult.ExecutionStatusView() != "passed" {
		return failureResult(j.MethodName(), executionResult.ErrorView()), nil
	}
	passed := ComparableResponse(executionResult.ResponseView(), expected) == expected
	reason := ""
	if !passed {
		reason = "response does not exactly match reference answer"
	}
	return &JudgeResult{
		Method: j.MethodName(),
		Score:  passedScore(passed),
		Passed: passed,
		Reason: reason,
	}, nil
}

// FailureResult 执行失败的规范化 0 分结果（Python _failure_result 经基类）。
func (j *ExactMatchJudger) FailureResult(errText string) *JudgeResult {
	return failureResult(j.MethodName(), errText)
}
