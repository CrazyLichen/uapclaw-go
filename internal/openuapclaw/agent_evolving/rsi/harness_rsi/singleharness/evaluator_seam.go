// evaluator 注入缝：编排器对 evaluator 的调用面（17.6c 真身签名收窄后）。
// fake 实现放 _test.go。
package singleharness

import (
	"context"

	harnessrsi "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi/harness_rsi"
	harnessconfig "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi/harness_rsi/config"
	analyzerpkg "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi/harness_rsi/evaluation_result_analyzer"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi/harness_rsi/evaluator"
	memberoptimizer "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi/harness_rsi/member_optimizer"
)

// ──────────────────────────── 结构体 ────────────────────────────

// EvaluatorInvoker 编排器依赖的 evaluator 评估面（17.6c 真身签名：
// onCaseStage 收窄为 EvaluationStageEvent，回填件③）。
// 真身即 *evaluator.TeamEvaluator；测试注入 fake 驱动主循环分支。
type EvaluatorInvoker interface {
	// Evaluate 评估数据集全部用例并返回 eval_ref.yaml 路径。
	Evaluate(ctx context.Context, dataset harnessrsi.DatasetArtifact,
		teamSkillRefPath, harnessRefsPath, outputDir string) (string, error)
	// EvaluateBatch 按输入顺序持久化用例结果。
	EvaluateBatch(ctx context.Context, cases []map[string]any,
		teamSkillRefPath, harnessRefsPath, outputDir string,
		dataset *harnessrsi.DatasetArtifact,
		onCaseStage func(evaluator.EvaluationStageEvent) error,
		caseConcurrency int,
		baselineContexts map[string]*evaluator.BaselineJudgeContext) (string, error)
}

// 编译期兜底：真身满足注入面。
var _ EvaluatorInvoker = (*evaluator.TeamEvaluator)(nil)

// NewEvaluatorInvoker 构造默认注入（17.6c 真身 *evaluator.TeamEvaluator）。
func NewEvaluatorInvoker(cfg harnessconfig.EvaluatorConfig) (EvaluatorInvoker, error) {
	return evaluator.NewTeamEvaluator(cfg)
}


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
