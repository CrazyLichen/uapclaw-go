package judger

// 评估基础设施错误（judger 包内自有定义）。
// Python: evaluator/errors.py EvaluationInfrastructureError——judger 直接引用；
// Go 包树不可环（父包 evaluator → judger 已有依赖），错误类型经依赖反转缝
// 在本包自有承载（与 RegisterRequirementResultsContract 注册缝同款决策）。
// 语义与父包 evaluator.EvaluationInfrastructureError 完全一致。

// EvaluationInfrastructureError 评估器无法为用例取得可信评分。
// Python: evaluator/errors.py EvaluationInfrastructureError。
type EvaluationInfrastructureError struct {
	// Msg 错误描述
	Msg string
}

// Error 实现 error 接口。
func (e *EvaluationInfrastructureError) Error() string { return e.Msg }

// NewEvaluationInfrastructureError 构造评估基础设施错误。
// Python: errors.py EvaluationInfrastructureError(msg)。
func NewEvaluationInfrastructureError(msg string) *EvaluationInfrastructureError {
	return &EvaluationInfrastructureError{Msg: msg}
}

var _ error = (*EvaluationInfrastructureError)(nil)
