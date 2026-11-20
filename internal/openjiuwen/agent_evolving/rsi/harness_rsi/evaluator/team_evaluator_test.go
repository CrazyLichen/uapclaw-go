package evaluator

import (
	"context"
	"errors"
	"testing"

	harnessrsi "github.com/uapclaw/uapclaw-go/internal/openjiuwen/agent_evolving/rsi/harness_rsi"
	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/agent_evolving/rsi/harness_rsi/config"
)

// TestTeamEvaluatorEvaluate_桩返回哨兵 打分桩在 17.6 真身落地前一律返回哨兵错误。
func TestTeamEvaluatorEvaluate_桩返回哨兵(t *testing.T) {
	te := NewTeamEvaluator(config.EvaluatorConfig{})
	_, err := te.Evaluate(context.Background(), harnessrsi.DatasetArtifact{}, "team_skill_ref", "harness_refs.yaml", "out")
	if !errors.Is(err, ErrEvaluatorNotImplemented) {
		t.Fatalf("应返回 ErrEvaluatorNotImplemented，得到 %v", err)
	}
}

// TestTeamEvaluatorEvaluateBatch_桩返回哨兵 批量入口同样返回哨兵错误。
func TestTeamEvaluatorEvaluateBatch_桩返回哨兵(t *testing.T) {
	te := NewTeamEvaluator(config.EvaluatorConfig{})
	_, err := te.EvaluateBatch(context.Background(), nil, "team_skill_ref", "harness_refs.yaml", "out",
		nil /* dataset */, nil /* onCaseStage */, 1 /* caseConcurrency */, nil /* baselineContexts */)
	if !errors.Is(err, ErrEvaluatorNotImplemented) {
		t.Fatalf("应返回 ErrEvaluatorNotImplemented，得到 %v", err)
	}
}

// TestTeamEvaluatorEvaluateBatch_并发数守卫 caseConcurrency < 1 先于桩错误被拒绝
// （对齐 Python evaluate_batch 首个 ValueError 分支，守卫顺序不得调换）。
func TestTeamEvaluatorEvaluateBatch_并发数守卫(t *testing.T) {
	te := NewTeamEvaluator(config.EvaluatorConfig{})
	_, err := te.EvaluateBatch(context.Background(), nil, "", "", "",
		nil, nil, 0, nil)
	if err == nil || errors.Is(err, ErrEvaluatorNotImplemented) {
		t.Fatalf("并发数 0 应先返回守卫错误而非桩错误，得到 %v", err)
	}
}
