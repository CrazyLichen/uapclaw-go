// Package evaluator 提供 RSI harness 评估能力（17.6 将补齐 TeamEvaluator 真身）。
//
// 文件目录：
//
//	evaluator/
//	├── doc.go                 # 包文档
//	├── errors.go              # 评估异常分类
//	├── runtime_adapters.go    # 运行时适配前置件（17.4b1）
//	└── team_evaluator.go     # TeamEvaluator 打分桩（⤴️17.6 回填真身后删）
//
// 对应 Python 代码：openjiuwen/rsi/harness_rsi/evaluator/
package evaluator

// EvaluationInfrastructureError 评估器无法为用例取得可信评分（Python evaluator/errors.py
// EvaluationInfrastructureError；17.4d 前置件切片，全量 taxonomy 随 17.6 落地）。
type EvaluationInfrastructureError struct {
	// Msg 错误消息
	Msg string
}

// Error 实现 error 接口。
func (e *EvaluationInfrastructureError) Error() string { return e.Msg }

// NewEvaluationInfrastructureError 构造评估基础设施错误。
func NewEvaluationInfrastructureError(msg string) *EvaluationInfrastructureError {
	return &EvaluationInfrastructureError{Msg: msg}
}

// 确保 sentinel 满足 error（编译期兜底）。
var _ error = (*EvaluationInfrastructureError)(nil)
