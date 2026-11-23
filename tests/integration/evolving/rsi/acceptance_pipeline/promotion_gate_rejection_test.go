//go:build integration

// 17.10 验收点②：晋级门拒绝路径。accFakeEvaluator 恒给低分制造考砸 →
// legacy_epoch_v1 晋级门拒绝 → 晋级不采纳、best harness 不变、发布状态
// 不产生新工件。拒绝语义对齐 Python iterative.py 的 checkpoint
// status="rejected" / promotion_applied=False 形态。
//
// 跨子系统 ≥3：singleharness（真编排器+真晋级门/发布缝）+ harness_rsi 顶层
// （状态机/落盘）+ evaluator 契约（fake 边界）。

package rsi

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/suite"
	"gopkg.in/yaml.v3"

	rsi "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi"
	harnessrsi "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi/harness_rsi"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi/harness_rsi/evaluator"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi/harness_rsi/singleharness"
)

// ──────────────────────────── 结构体 ────────────────────────────

// PromotionGateRejectionSuite 晋级门拒绝套件。
type PromotionGateRejectionSuite struct{ suite.Suite }

// accFakeEvaluator 真编排器评估缝替身：按预置评分写真实 eval_ref 形态
// （本包内复刻 chain3FakeEvaluator——父目录非导出符号跨包不可见）。
type accFakeEvaluator struct {
	scores map[string]float64
	calls  int
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestPromotionGateRejectionSuite 入口。
func TestPromotionGateRejectionSuite(t *testing.T) { suite.Run(t, new(PromotionGateRejectionSuite)) }

// Evaluate 单次评估（对齐 chain3FakeEvaluator.Evaluate）。
func (f *accFakeEvaluator) Evaluate(ctx context.Context, dataset harnessrsi.DatasetArtifact,
	teamSkillRefPath, harnessRefsPath, outputDir string) (string, error) {
	cases := make([]map[string]any, 0, len(dataset.DatasetFiles))
	for _, id := range dataset.DatasetFiles {
		cases = append(cases, map[string]any{"case_id": id, "score": f.scores[id], "status": "passed"})
	}
	return accWriteEvalRef(outputDir, cases, harnessRefsPath), nil
}

// EvaluateBatch 批量评估（对齐 chain3FakeEvaluator.EvaluateBatch）。
func (f *accFakeEvaluator) EvaluateBatch(ctx context.Context, cases []map[string]any,
	teamSkillRefPath, harnessRefsPath, outputDir string,
	dataset *harnessrsi.DatasetArtifact,
	onCaseStage func(evaluator.EvaluationStageEvent) error,
	caseConcurrency int,
	baselineContexts map[string]*evaluator.BaselineJudgeContext) (string, error) {
	f.calls++
	_ = teamSkillRefPath
	evalCases := []map[string]any{}
	for _, single := range cases {
		caseID, _ := single["case_id"].(string)
		score, ok := f.scores[caseID]
		if !ok {
			score = 1.0
		}
		resultPath := filepath.Join(outputDir, "cases", caseID, "result.json")
		_ = os.MkdirAll(filepath.Dir(resultPath), 0o755)
		_ = os.WriteFile(resultPath, []byte(`{"case_id":"`+caseID+`","score":`+
			jsonFloat(score)+`,"status":"passed","execution_status":"passed","metadata":{"execution":{"execution_evidence":{"status":"complete","timing_complete":true}}}}`), 0o644)
		tracePath := filepath.Join(outputDir, "cases", caseID, "trace.json")
		_ = os.WriteFile(tracePath, []byte(`{"steps":[]}`), 0o644)
		evalCases = append(evalCases, map[string]any{
			"case_id": caseID, "score": score, "status": "passed",
			"result_path": resultPath, "trace_path": tracePath,
		})
	}
	return accWriteEvalRef(outputDir, evalCases, harnessRefsPath), nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// accWriteEvalRef 写 eval_ref.yaml（对齐 Python eval_ref 消费形态，
// 复刻 chain3WriteEvalRef）。
func accWriteEvalRef(outputDir string, cases []map[string]any, harnessRefsPath string) string {
	_ = os.MkdirAll(outputDir, 0o755)
	evalRef := filepath.Join(outputDir, "eval_ref.yaml")
	content := ""
	if harnessRefsPath != "" {
		content += "harness_refs_path: " + harnessRefsPath + "\n"
	}
	content += "cases:\n"
	for _, single := range cases {
		content += "  - case_id: " + toStringHelper(single["case_id"]) +
			"\n    score: " + jsonFloat(single["score"]) +
			"\n    status: " + toStringHelper(single["status"]) +
			"\n    result_path: " + toStringHelper(single["result_path"]) +
			"\n    trace_path: " + toStringHelper(single["trace_path"]) + "\n"
	}
	_ = os.WriteFile(evalRef, []byte(content), 0o644)
	return evalRef
}

// jsonFloat 浮点格式化（零值 "0"，非零常规格式）。
func jsonFloat(v any) string {
	f, _ := v.(float64)
	if f == 0 {
		return "0"
	}
	return trimTrailingZeros(f)
}

// toStringHelper any → string（map 取值兜底）。
func toStringHelper(v any) string { return fmt.Sprint(v) }

// trimTrailingZeros 浮点去尾零格式化。
func trimTrailingZeros(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }

// accReadYAML 读 YAML 文件为 map（测试断言用）。
func accReadYAML(t *testing.T, path string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	m := map[string]any{}
	if err := yaml.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

// TestPromotionGate_低分连续拒绝_不发布 低分场景：全部 epoch 拒绝。
func (s *PromotionGateRejectionSuite) TestPromotionGate_低分连续拒绝_不发布() {
	// 全 case 低分：候选永远打不过基线（基线同分时无 MinImprovement 改进）。
	fake := &accFakeEvaluator{scores: map[string]float64{"case-pass": 0.0, "case-fail": 0.0}}

	root := s.T().TempDir()
	dataset := accWriteDataset(s.T(), filepath.Join(root, "dataset"))
	refsPath := accWriteHarnessRefs(s.T(), root)
	outputDir := filepath.Join(root, "output")
	maxIter := 2
	req, err := singleharness.NewIterativeSingleHarnessRequest(
		dataset.DatasetFiles, refsPath, outputDir, dataset.DatasetID,
		false, "", false, "acc-reject", &maxIter)
	s.Require().NoError(err)

	orchestrator, err := singleharness.NewSingleHarnessIterativeOptimizationOrchestrator(
		accOrchestratorConfig(), fake, nil, nil, nil)
	s.Require().NoError(err)

	// 拒绝不是失败：流水线正常走完（Python 拒绝分支不抛错）。
	_, err = orchestrator.Run(s.T().Context(), req,
		func(context.Context, rsi.EngineEvent) error { return nil })
	s.Require().NoError(err)
	s.Require().Greater(fake.calls, 0, "fake evaluator 未被调用")

	// 断言 1：全部 epoch checkpoint 未晋级（promotion_applied=False，
	// 对齐 iterative.py:4724；零改进路径 status 为 verified，有候选被否时
	// status 为 rejected——两种形态同属晋级门拒绝，这里锁不晋级不变量与
	// 拒绝原因在案）。
	state := accReadYAML(s.T(), filepath.Join(outputDir, "single_harness_state.yaml"))
	checkpoints, _ := state["epoch_checkpoints"].([]any)
	s.Require().NotEmpty(checkpoints, "无 epoch checkpoint 落盘")
	for _, raw := range checkpoints {
		cp, ok := raw.(map[string]any)
		s.Require().True(ok)
		s.Require().Equal(false, cp["promotion_applied"], "epoch %v 意外晋级", cp["epoch"])
		// 拒绝原因在案：要么显式 promotion_reason（如
		// no_provisional_harness_change），要么零改进种子分支
		// （noop_initial_score_seed=True，对齐 iterative.py:4725-4728）。
		reason, _ := cp["promotion_reason"].(string)
		noopSeed := cp["noop_initial_score_seed"] == true
		s.Require().True(reason != "" || noopSeed, "epoch %v 缺拒绝语义（reason=%q noop=%v）",
			cp["epoch"], reason, noopSeed)
	}

	// 断言 2：best harness 未被候选替换（仍为 source 引用路径）。
	s.Require().Equal(refsPath, state["best_harness_refs_path"])

	// 断言 3：无新工件发布（publication_status 以 not_published 开头——实测
	// 值 not_published_no_improvement；发布路径为空）。
	pubStatus, _ := state["publication_status"].(string)
	s.Require().True(strings.HasPrefix(pubStatus, "not_published"),
		"意外发布状态：%q", pubStatus)
	s.Require().Equal("", state["published_harness_refs_path"])
}
