// Package judger 提供 LLM 评委基础判定（17.6 将补齐 EvaluationJudger 真身）。
//
// 文件目录：
//
//	judger/
//	└── base.go    # 遗留完成态评分识别（judger/base.py 前置切片）
//
// 对应 Python 代码：openjiuwen/rsi/harness_rsi/evaluator/judger/base.py
package judger

// IsExecutionOnlyEvaluation 识别遗留的仅完成态评分——不得作为证据复用
// （Python judger/base.py _is_execution_only_evaluation；导出因跨包使用）。
func IsExecutionOnlyEvaluation(evaluation map[string]any) bool {
	method, _ := evaluation["method"].(string)
	if method == "none" {
		return true
	}
	metadata, ok := evaluation["metadata"].(map[string]any)
	if !ok {
		return false
	}
	status, _ := metadata["rule_engine_status"].(string)
	return status == "backend_completed"
}
