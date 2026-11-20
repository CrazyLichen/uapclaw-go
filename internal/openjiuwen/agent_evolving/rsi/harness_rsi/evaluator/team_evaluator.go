// team_evaluator.go Team Skill + member harness 集合评估门面（17.4f 桩）。
// Python: openjiuwen/rsi/harness_rsi/evaluator/team_evaluator.py。
// 真身（CaseRunner / build_judger / MetricsCollector 等依赖链）⤴️ 17.6 评估
// agent 章节补齐，届时按规则 8 删除本桩。

package evaluator

import (
	"context"
	"errors"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	harnessrsi "github.com/uapclaw/uapclaw-go/internal/openjiuwen/agent_evolving/rsi/harness_rsi"
	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/agent_evolving/rsi/harness_rsi/config"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TeamEvaluator Team Skill + member harness 集合对数据集的评估门面（桩）。
//
// 对齐 Python team_evaluator.py:44 TeamEvaluator。参数与返回按 Python 真签名
// 映射；DatasetArtifact / EvaluationCaseTraceRef 已在 harness_rsi/schema.go
// （17.1）落地。
//
// ⤴️ 17.4f 登记回填：17.6 评估 agent 章节落地真身后，按规则 8 删除本桩
// （含哨兵错误与 _test.go，锚点 ErrEvaluatorNotImplemented 全仓 0 命中）。
type TeamEvaluator struct {
	// config 评估器配置（对齐 Python self.config）
	config config.EvaluatorConfig
}

// ──────────────────────────── 全局变量 ────────────────────────────

// ErrEvaluatorNotImplemented 打分桩哨兵错误：17.6 真身落地前任何评分调用立即失败暴露。
var ErrEvaluatorNotImplemented = errors.New("evaluator not implemented (stub, see 17.6)")

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTeamEvaluator 构造打分桩（对齐 Python __init__）。
func NewTeamEvaluator(cfg config.EvaluatorConfig) *TeamEvaluator {
	return &TeamEvaluator{config: cfg}
}

// Evaluate 评估数据集全部用例并返回 eval_ref.yaml 路径（桩）。
//
// Python: team_evaluator.py:66 evaluate → _load_dataset_cases 后转调
// evaluate_batch。_load_dataset_cases 属 17.6 范围，桩期以 nil 占位传参
// （⤴️ 17.6 换为载入产物），转调结构保持对齐。
func (te *TeamEvaluator) Evaluate(ctx context.Context, dataset harnessrsi.DatasetArtifact,
	teamSkillRefPath, harnessRefsPath, outputDir string) (string, error) {
	return te.EvaluateBatch(ctx, nil, teamSkillRefPath, harnessRefsPath, outputDir,
		&dataset, nil, 1, nil)
}

// EvaluateBatch 按输入顺序持久化用例结果（桩）。
//
// Python: team_evaluator.py:82 evaluate_batch。case_concurrency 守卫
// （bool-as-int 与 <1 拒绝）按 Python 原始检查顺序先于桩错误（守卫顺序不得调换）；
// Python 的 isinstance bool 检查在 Go 类型层不可能（bool ≠ int），属已知
// 类型层偏差。baselineContexts 已随 17.4d 前置件收窄为 *BaselineJudgeContext
// （原 ⤴️17.6 收窄回填已于 17.4d 兑现）。
func (te *TeamEvaluator) EvaluateBatch(ctx context.Context, cases []map[string]any,
	teamSkillRefPath, harnessRefsPath, outputDir string,
	dataset *harnessrsi.DatasetArtifact,
	onCaseStage func(map[string]any) error,
	caseConcurrency int,
	baselineContexts map[string]*BaselineJudgeContext) (string, error) {
	if caseConcurrency < 1 {
		return "", errors.New("case_concurrency must be a positive integer")
	}
	logger.Warn(logComponent).
		Str("method", "TeamEvaluator.EvaluateBatch").
		Msg("evaluator stub called; real LLM judge lands in 17.6")
	return "", ErrEvaluatorNotImplemented
}
