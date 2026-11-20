// reverse_seams.go judger 对父包 evaluator 能力的依赖反转注册缝。
// Python 侧 llm_as_judge 直接引用同树 evaluator.transport / evaluator.optimization_signals；
// Go 包树不可环（evaluator → judger 已有依赖），两个函数经 init 注册注入
// （与 errors.go 反转缝、RegisterRequirementResultsContract 同款决策）。

package judger

// ──────────────────────────── 全局变量 ────────────────────────────

// transientTransportErrorFn 瞬态传输错误判定函数（evaluator.TransientTransportError 注册）。
var transientTransportErrorFn func(errText string) string

// optimizationSignalsContractFn 优化信号契约函数（evaluator.OptimizationSignalsContract 注册）。
var optimizationSignalsContractFn func(continuousScore any, dimensions map[string]any, source string) map[string]any

// ──────────────────────────── 导出函数 ────────────────────────────

// RegisterTransientTransportError 由父包 evaluator 在 init 时注册瞬态传输判定。
func RegisterTransientTransportError(fn func(errText string) string) {
	transientTransportErrorFn = fn
}

// RegisterOptimizationSignalsContract 由父包 evaluator 在 init 时注册优化信号契约。
func RegisterOptimizationSignalsContract(fn func(continuousScore any, dimensions map[string]any, source string) map[string]any) {
	optimizationSignalsContractFn = fn
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// callTransientTransportError 经缝调用瞬态传输判定；未注册时视为非瞬态（空串）。
func callTransientTransportError(errText string) string {
	if transientTransportErrorFn == nil {
		return ""
	}
	return transientTransportErrorFn(errText)
}

// callOptimizationSignalsContract 经缝调用优化信号契约；未注册时返回空结构。
func callOptimizationSignalsContract(continuousScore any, dimensions map[string]any, source string) map[string]any {
	if optimizationSignalsContractFn == nil {
		return map[string]any{}
	}
	return optimizationSignalsContractFn(continuousScore, dimensions, source)
}

// ──────────────────────────── SWE-bench 验证缝 ────────────────────────────

// SwebenchVerifierView SWE-bench 官方验证能力视图（依赖反转缝）。
// Python: script_based.py 直接引用 evaluator.swebench_runtime 的
// collect_model_patch / run_official_swebench_evaluation；Go 包树不可环
// （真身属父包 evaluator/swebench_runtime.go，Task 11 落地），经本缝注入。
type SwebenchVerifierView interface {
	// CollectModelPatch 从工作区收集模型 patch（Python collect_model_patch）
	CollectModelPatch(workspaceDir string) (string, error)
	// RunOfficialEvaluation 跑官方验证（Python run_official_swebench_evaluation）
	RunOfficialEvaluation(caseMapping map[string]any, modelPatch, outputDir string) (map[string]any, error)
}

// swebenchVerifierFn 注册的验证实现。
var swebenchVerifierFn SwebenchVerifierView

// RegisterSwebenchVerifier 由父包 evaluator 在 init 时注册官方验证实现。
func RegisterSwebenchVerifier(view SwebenchVerifierView) {
	swebenchVerifierFn = view
}

// callCollectModelPatch 经缝调用 patch 收集；未注册时报基建错误。
func callCollectModelPatch(workspaceDir string) (string, error) {
	if swebenchVerifierFn == nil {
		return "", NewEvaluationInfrastructureError("swebench verifier not registered")
	}
	return swebenchVerifierFn.CollectModelPatch(workspaceDir)
}

// callRunOfficialSwebenchEvaluation 经缝调用官方验证；未注册时报基建错误。
func callRunOfficialSwebenchEvaluation(caseMapping map[string]any, modelPatch, outputDir string) (map[string]any, error) {
	if swebenchVerifierFn == nil {
		return nil, NewEvaluationInfrastructureError("swebench verifier not registered")
	}
	return swebenchVerifierFn.RunOfficialEvaluation(caseMapping, modelPatch, outputDir)
}
