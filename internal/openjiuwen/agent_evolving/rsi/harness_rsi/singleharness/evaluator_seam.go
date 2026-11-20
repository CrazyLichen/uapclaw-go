// evaluator fake 注入缝：编排器对 evaluator 的调用面按 17.4f 桩签名收敛。
// 17.4d Task 6。fake 实现放 _test.go；本缝与桩一起 ⤴️17.6 回填真身后删除（规则 8）。
package singleharness

import (
	"context"

	harnessrsi "github.com/uapclaw/uapclaw-go/internal/openjiuwen/agent_evolving/rsi/harness_rsi"
	harnessconfig "github.com/uapclaw/uapclaw-go/internal/openjiuwen/agent_evolving/rsi/harness_rsi/config"
	analyzerpkg "github.com/uapclaw/uapclaw-go/internal/openjiuwen/agent_evolving/rsi/harness_rsi/evaluation_result_analyzer"
	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/agent_evolving/rsi/harness_rsi/evaluator"
	evaluatorpkg "github.com/uapclaw/uapclaw-go/internal/openjiuwen/agent_evolving/rsi/harness_rsi/evaluator"
	memberoptimizer "github.com/uapclaw/uapclaw-go/internal/openjiuwen/agent_evolving/rsi/harness_rsi/member_optimizer"
)

// ──────────────────────────── 结构体 ────────────────────────────

// EvaluatorInvoker 编排器依赖的 evaluator 评估面（对齐 17.4f 桩的
// Evaluate/EvaluateBatch 签名）。真身即 *evaluator.TeamEvaluator（⤴️17.6）；
// 测试注入 fake 驱动主循环分支。
type EvaluatorInvoker interface {
	// Evaluate 评估数据集全部用例并返回 eval_ref.yaml 路径。
	Evaluate(ctx context.Context, dataset harnessrsi.DatasetArtifact,
		teamSkillRefPath, harnessRefsPath, outputDir string) (string, error)
	// EvaluateBatch 按输入顺序持久化用例结果。
	EvaluateBatch(ctx context.Context, cases []map[string]any,
		teamSkillRefPath, harnessRefsPath, outputDir string,
		dataset *harnessrsi.DatasetArtifact,
		onCaseStage func(map[string]any) error,
		caseConcurrency int,
		baselineContexts map[string]*evaluatorpkg.BaselineJudgeContext) (string, error)
}

// 编译期兜底：桩满足注入面（真身替换时同步满足）。
var _ EvaluatorInvoker = (*evaluator.TeamEvaluator)(nil)

// NewStubEvaluatorInvoker 构造默认注入（接 17.4f 桩）。
func NewStubEvaluatorInvoker(cfg harnessconfig.EvaluatorConfig) EvaluatorInvoker {
	return evaluator.NewTeamEvaluator(cfg)
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ErrEvaluatorStubReexported 供编排器判定"桩被直调"的转发哨兵（evaluator.ErrEvaluatorNotImplemented）。
//
// 编排器分支应使用 errors.Is(err, evaluator.ErrEvaluatorNotImplemented)，
// 此变量仅为可读性别名，⤴️17.6 删桩时一并删除。
var ErrEvaluatorStubReexported = evaluator.ErrEvaluatorNotImplemented

// AnalyzerInvoker 编排器依赖的诊断分析面（Python 构造器 analyzer: Any 注入点）。
// 真身 *analyzerpkg.EvaluationResultAnalyzer（17.4b）；测试注入 fake。
type AnalyzerInvoker interface {
	// Analyze 执行诊断分析并返回 analysis_ref.yaml 路径。
	Analyze(ctx context.Context, invocation harnessrsi.EvaluationResultAnalysisInvocation) (string, error)
}

// OptimizerInvoker 编排器依赖的成员优化面（Python 构造器 member_optimizer: Any 注入点）。
// 真身 *memberoptimizer.MemberOptimizer（17.4c）；测试注入 fake。
type OptimizerInvoker interface {
	// Optimize 执行成员优化并返回 member_optimization_ref.yaml 路径。
	Optimize(ctx context.Context, evalRefPath, analysisResultPath, harnessRefsPath, outputDir string,
		params memberoptimizer.OptimizeExecuteParams) (string, error)
}

// 编译期兜底：真身满足注入面。
var (
	_ AnalyzerInvoker  = (*analyzerpkg.EvaluationResultAnalyzer)(nil)
	_ OptimizerInvoker = (*memberoptimizer.MemberOptimizer)(nil)
)
