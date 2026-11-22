// Package evalerrors 提供 RSI 评估域共享的评估基础设施错误类型。
//
// Python 真相源只有一份 evaluator/errors.py EvaluationInfrastructureError，
// judger 直接引用同一类（考场事故与真失败靠同一类的 except 精确区分）。
// Go 包树不可环（父包 evaluator → judger 已有依赖），原实现按"反转缝
// 自有承载"在 judger 复制了一份独立类型，导致 case_runner 的基建错误
// 捕获识别不了 judger 抛出的错误（行为分歧）。2026-11-22 拍板（用户裁决，
// 对齐 17.6c source_evidence 下沉先例）：类型下沉本兄弟子包单一承载，
// evaluator 与 judger 直接以包名引用，不再保留任何别名/兼容壳。
//
// 文件目录：
//
//	evalerrors/
//	└── evalerrors.go      # EvaluationInfrastructureError 单一真身
//
// 对应 Python 代码：openjiuwen/rsi/harness_rsi/evaluator/errors.py（层级偏差：
// Python 在 evaluator 包根，Go 下沉子包，规则 9.1 已登记）。
package evalerrors

// EvaluationInfrastructureError 评估器无法为用例取得可信评分。
// Python: evaluator/errors.py EvaluationInfrastructureError。
type EvaluationInfrastructureError struct {
	// Msg 错误消息
	Msg string
}

// Error 实现 error 接口。
func (e *EvaluationInfrastructureError) Error() string { return e.Msg }

// NewEvaluationInfrastructureError 构造评估基础设施错误。
// Python: errors.py EvaluationInfrastructureError(msg)。
func NewEvaluationInfrastructureError(msg string) *EvaluationInfrastructureError {
	return &EvaluationInfrastructureError{Msg: msg}
}

// 编译期兜底：真身满足 error 接口。
var _ error = (*EvaluationInfrastructureError)(nil)
