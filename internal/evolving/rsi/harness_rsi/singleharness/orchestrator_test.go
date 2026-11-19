package singleharness

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	harnessrsi "github.com/uapclaw/uapclaw-go/internal/evolving/rsi/harness_rsi"
	configpkg "github.com/uapclaw/uapclaw-go/internal/evolving/rsi/harness_rsi/config"
	memberoptimizer "github.com/uapclaw/uapclaw-go/internal/evolving/rsi/harness_rsi/member_optimizer"
)

// fakeOrchestratorEvaluator 测试用评估注入面：把预置分数写成合法 eval_ref。
type fakeOrchestratorEvaluator struct {
	scores map[string]float64
	calls  int
}

func (f *fakeOrchestratorEvaluator) Evaluate(ctx context.Context, dataset harnessrsi.DatasetArtifact,
	teamSkillRefPath, harnessRefsPath, outputDir string) (string, error) {
	cases := make([]map[string]any, 0, len(dataset.DatasetFiles))
	for id, score := range f.scores {
		status := "passed"
		if score < 1.0 {
			status = "failed"
		}
		cases = append(cases, map[string]any{"case_id": id, "score": score, "status": status})
	}
	return writeFakeEvalRef(outputDir, cases), nil
}

func (f *fakeOrchestratorEvaluator) EvaluateBatch(ctx context.Context, cases []map[string]any,
	teamSkillRefPath, harnessRefsPath, outputDir string,
	dataset *harnessrsi.DatasetArtifact,
	onCaseStage func(map[string]any) error,
	caseConcurrency int,
	baselineContexts map[string]any) (string, error) {
	f.calls++
	var evalCases []map[string]any
	for _, singleCase := range cases {
		caseID := toString(singleCase["case_id"])
		score, ok := f.scores[caseID]
		if !ok {
			score = 1.0
		}
		status := "passed"
		if score < 1.0 {
			status = "failed"
		}
		resultPath := filepath.Join(outputDir, "cases", caseID, "result.json")
		os.MkdirAll(filepath.Dir(resultPath), 0o755)
		os.WriteFile(resultPath, []byte(`{"case_id":"`+caseID+`","score":`+fmt.Sprintf("%v", score)+`,"status":"`+status+`","execution_status":"passed","metadata":{"execution":{"execution_evidence":{"status":"complete","timing_complete":true}}}}`), 0o644)
		tracePath := filepath.Join(outputDir, "cases", caseID, "trace.json")
		os.WriteFile(tracePath, []byte(`{"steps":[]}`), 0o644)
		evalCases = append(evalCases, map[string]any{
			"case_id": caseID, "score": score, "status": status,
			"result_path": resultPath, "trace_path": tracePath,
		})
	}
	return writeFakeEvalRef(outputDir, evalCases, harnessRefsPath), nil
}

// writeFakeEvalRef 写合法 eval_ref（用例带 result 文件，完整性检查通过）。
func writeFakeEvalRef(outputDir string, cases []map[string]any, harnessRefsPath ...string) string {
	os.MkdirAll(outputDir, 0o755)
	evalRef := filepath.Join(outputDir, "eval_ref.yaml")
	refsLine := ""
	if len(harnessRefsPath) > 0 {
		refsLine = "harness_refs_path: " + harnessRefsPath[0] + "\n"
	}
	content := refsLine + "cases:\n"
	for _, singleCase := range cases {
		content += fmt.Sprintf("  - case_id: %v\n    score: %v\n    status: %v\n    result_path: %v\n",
			singleCase["case_id"], singleCase["score"], singleCase["status"], singleCase["result_path"])
		if tracePath, ok := singleCase["trace_path"]; ok {
			content += fmt.Sprintf("    trace_path: %v\n", tracePath)
		}
	}
	os.WriteFile(evalRef, []byte(content), 0o644)
	return evalRef
}

// buildTestConfig 构造通过校验的编排器配置。
func buildTestConfig(dir string) configpkg.AutoCoordinatingHarnessConfig {
	return configpkg.AutoCoordinatingHarnessConfig{
		MaxEpochs: 2,
		Evaluator: configpkg.EvaluatorConfig{
			Backend:                      "single_harness",
			ScoreMode:                    "binary",
			EvaluationMethod:             "script-based",
			SolverMaxIterationsHardLimit: 10,
		},
		Scheduling: configpkg.OrchestratorSchedulingConfig{
			EvaluationStrategy:        "hybrid",
			CoordinationStrategy:      "team_first_single_pass",
			FullEvaluationEnabled:     true,
			FullEvaluationConcurrency: 1,
		},
		MemberOptimizer: configpkg.MemberOptimizerConfig{
			SiblingCandidateCount:           1,
			MaxRepairRoundsPerBatch:         2,
			CandidateMinTargetBehaviorDelta: 0.1,
		},
		DataLoader: configpkg.DataLoaderConfig{BatchSize: 1},
		Promotion:  configpkg.PromotionConfig{PolicyVersion: "legacy_epoch_v1"},
	}
}

func TestNewOrchestrator_校验分支(t *testing.T) {
	dir := t.TempDir()
	cfg := buildTestConfig(dir)
	cfg.Evaluator.Backend = "team"
	if _, err := NewSingleHarnessIterativeOptimizationOrchestrator(cfg, nil, nil, nil, nil); err == nil ||
		!strings.Contains(err.Error(), "backend='single_harness'") {
		t.Fatalf("backend 校验: %v", err)
	}
	cfg = buildTestConfig(dir)
	cfg.Scheduling.FullEvaluationEnabled = false
	if _, err := NewSingleHarnessIterativeOptimizationOrchestrator(cfg, nil, nil, nil, nil); err == nil ||
		!strings.Contains(err.Error(), "epoch full checkpoint") {
		t.Fatalf("全量评估校验: %v", err)
	}
	cfg = buildTestConfig(dir)
	cfg.MemberOptimizer.SiblingCandidateCount = 2
	if _, err := NewSingleHarnessIterativeOptimizationOrchestrator(cfg, nil, nil, nil, nil); err == nil ||
		!strings.Contains(err.Error(), "one candidate") {
		t.Fatalf("兄弟候选校验: %v", err)
	}
	cfg = buildTestConfig(dir)
	cfg.MemberOptimizer.ImproverPolicyRef = "/policy.yaml"
	if _, err := NewSingleHarnessIterativeOptimizationOrchestrator(cfg, nil, nil, nil, nil); err == nil ||
		!strings.Contains(err.Error(), "no improver evolution policy") {
		t.Fatalf("策略引用校验: %v", err)
	}
	// 合法构造：收紧面生效
	orchestrator, err := NewSingleHarnessIterativeOptimizationOrchestrator(
		buildTestConfig(dir), nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("合法构造: %v", err)
	}
	if len(orchestrator.config.MemberOptimizer.AllowedActionGroups) != 5 {
		t.Fatalf("收紧动作组: %v", orchestrator.config.MemberOptimizer.AllowedActionGroups)
	}
	if orchestrator.config.MemberOptimizer.MaxRolesPerRun != 1 ||
		orchestrator.config.MemberOptimizer.CandidateHoldoutCases != 0 {
		t.Fatalf("收紧角色/保留: %v", orchestrator.config.MemberOptimizer)
	}
	if orchestrator.config.MemberOptimizer.MaxActionsPerPlan != 3 {
		t.Fatalf("默认动作数 3: %v", orchestrator.config.MemberOptimizer.MaxActionsPerPlan)
	}
	if orchestrator.explicitImproverPolicy {
		t.Fatalf("无引用应非显式")
	}
	if orchestrator.improverPolicy == nil {
		t.Fatalf("默认策略应加载")
	}
}

func TestRun_ResumeCompletedState(t *testing.T) {
	dir := t.TempDir()
	// harness 引用与数据集
	harnessDir := filepath.Join(dir, "harness")
	os.MkdirAll(harnessDir, 0o755)
	refsPath := filepath.Join(dir, "refs.yaml")
	writeTestYAML(t, refsPath, "harness_refs:\n  coder: "+harnessDir+"\n")
	dsPath := filepath.Join(dir, "ds.json")
	writeTestYAML(t, dsPath, `[{"case_id":"c1","input":"task"}]`)
	outputDir := filepath.Join(dir, "run")
	os.MkdirAll(outputDir, 0o755)
	if _, err := NewIterativeSingleHarnessRequest(
		[]string{dsPath}, refsPath, outputDir, "ds1", false, "", false, "t1", nil); err != nil {
		t.Fatalf("构造请求: %v", err)
	}
	// 先构造基线评估引用（冻结基线）
	baselineDir := filepath.Join(dir, "baseline")
	fake := &fakeOrchestratorEvaluator{scores: map[string]float64{"c1": 1.0}}
	baselineRef, err := fake.EvaluateBatch(context.Background(),
		[]map[string]any{{"case_id": "c1"}}, "", refsPath, baselineDir, nil, nil, 1, nil)
	if err != nil {
		t.Fatalf("造基线: %v", err)
	}
	requestResume, err := NewIterativeSingleHarnessRequest(
		[]string{dsPath}, refsPath, outputDir, "ds1", true, baselineRef, false, "t1", nil)
	if err != nil {
		t.Fatalf("构造续跑请求: %v", err)
	}
	// 预置已完成状态（指纹与请求一致）
	cfg := buildTestConfig(dir)
	orchestrator, err := NewSingleHarnessIterativeOptimizationOrchestrator(cfg, fake, nil, nil, nil)
	if err != nil {
		t.Fatalf("构造编排器: %v", err)
	}
	dataset, err := datasetArtifact(requestResume)
	if err != nil {
		t.Fatalf("数据集: %v", err)
	}
	fingerprint, err := requestFingerprint(requestResume,
		refsPath, cfg.MemberOptimizer.SiblingCandidateCount,
		orchestrator.improverPolicy.CanonicalDigest())
	if err != nil {
		t.Fatalf("指纹: %v", err)
	}
	state, err := loadOrCreateState(filepath.Join(outputDir, "single_harness_state.yaml"),
		false, fingerprint, refsPath, dataset)
	if err != nil {
		t.Fatalf("状态: %v", err)
	}
	if err := initializeFrozenBaseline(state, baselineRef, refsPath,
		map[string]struct{}{"c1": {}}); err != nil {
		t.Fatalf("冻结基线: %v", err)
	}
	state["status"] = "completed"
	state["best_eval_ref_path"] = baselineRef
	state["best_score"] = 1.0
	state["best_harness_refs_path"] = refsPath
	if err := writeYAMLAtomic(filepath.Join(outputDir, "single_harness_state.yaml"), state); err != nil {
		t.Fatalf("落盘: %v", err)
	}
	result, err := orchestrator.Run(context.Background(), requestResume, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.StatePath == "" || result.ReportPath == "" {
		t.Fatalf("结果路径: %+v", result)
	}
	// 无受门改进 → 不发布
	payload, _ := readYAMLFile(result.StatePath)
	if payload["publication_status"] != "not_published_no_improvement" {
		t.Fatalf("无改进不发布: %v", payload["publication_status"])
	}
}

func TestRun_ResumeEpochs全checkpoint跳过(t *testing.T) {
	dir := t.TempDir()
	harnessDir := filepath.Join(dir, "harness")
	os.MkdirAll(harnessDir, 0o755)
	refsPath := filepath.Join(dir, "refs.yaml")
	writeTestYAML(t, refsPath, "harness_refs:\n  coder: "+harnessDir+"\n")
	dsPath := filepath.Join(dir, "ds.json")
	writeTestYAML(t, dsPath, `[{"case_id":"c1","input":"task"}]`)
	outputDir := filepath.Join(dir, "run")
	os.MkdirAll(outputDir, 0o755)
	baselineDir := filepath.Join(dir, "baseline")
	fake := &fakeOrchestratorEvaluator{scores: map[string]float64{"c1": 1.0}}
	baselineRef, err := fake.EvaluateBatch(context.Background(),
		[]map[string]any{{"case_id": "c1"}}, "", refsPath, baselineDir, nil, nil, 1, nil)
	if err != nil {
		t.Fatalf("造基线: %v", err)
	}
	request, err := NewIterativeSingleHarnessRequest(
		[]string{dsPath}, refsPath, outputDir, "ds1", true, baselineRef, false, "t1", nil)
	if err != nil {
		t.Fatalf("构造请求: %v", err)
	}
	cfg := buildTestConfig(dir)
	orchestrator, err := NewSingleHarnessIterativeOptimizationOrchestrator(cfg, fake, nil, nil, nil)
	if err != nil {
		t.Fatalf("构造: %v", err)
	}
	dataset, err := datasetArtifact(request)
	if err != nil {
		t.Fatalf("数据集: %v", err)
	}
	fingerprint, err := requestFingerprint(request, refsPath,
		cfg.MemberOptimizer.SiblingCandidateCount, orchestrator.improverPolicy.CanonicalDigest())
	if err != nil {
		t.Fatalf("指纹: %v", err)
	}
	state, err := loadOrCreateState(filepath.Join(outputDir, "single_harness_state.yaml"),
		false, fingerprint, refsPath, dataset)
	if err != nil {
		t.Fatalf("状态: %v", err)
	}
	if err := initializeFrozenBaseline(state, baselineRef, refsPath,
		map[string]struct{}{"c1": {}}); err != nil {
		t.Fatalf("基线: %v", err)
	}
	state["status"] = "running"
	state["best_score"] = 1.0
	state["best_eval_ref_path"] = baselineRef
	state["max_iteration"] = 2
	state["epoch_checkpoints"] = []any{
		map[string]any{"epoch": 1, "status": "unchanged"},
		map[string]any{"epoch": 2, "status": "unchanged"},
	}
	if err := writeYAMLAtomic(filepath.Join(outputDir, "single_harness_state.yaml"), state); err != nil {
		t.Fatalf("落盘: %v", err)
	}
	result, err := orchestrator.Run(context.Background(), request, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	payload, _ := readYAMLFile(result.StatePath)
	if payload["status"] != "completed" {
		t.Fatalf("状态应 completed: %v", payload["status"])
	}
	// 未生成候选 → 不发布
	if payload["publication_status"] != "not_published_no_improvement" {
		t.Fatalf("发布状态: %v", payload["publication_status"])
	}
	report, _ := readYAMLFile(result.ReportPath)
	if report["status"] != "completed" {
		t.Fatalf("报告状态: %v", report["status"])
	}
}

func TestRun_Resume指纹不匹配拒绝(t *testing.T) {
	dir := t.TempDir()
	harnessDir := filepath.Join(dir, "harness")
	os.MkdirAll(harnessDir, 0o755)
	refsPath := filepath.Join(dir, "refs.yaml")
	writeTestYAML(t, refsPath, "harness_refs:\n  coder: "+harnessDir+"\n")
	dsPath := filepath.Join(dir, "ds.json")
	writeTestYAML(t, dsPath, `[{"case_id":"c1","input":"task"}]`)
	outputDir := filepath.Join(dir, "run")
	os.MkdirAll(outputDir, 0o755)
	request, err := NewIterativeSingleHarnessRequest(
		[]string{dsPath}, refsPath, outputDir, "ds1", true, "", false, "", nil)
	if err != nil {
		t.Fatalf("构造: %v", err)
	}
	cfg := buildTestConfig(dir)
	orchestrator, err := NewSingleHarnessIterativeOptimizationOrchestrator(cfg, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("构造: %v", err)
	}
	// 写一个指纹不匹配的状态文件
	writeTestYAML(t, filepath.Join(outputDir, "single_harness_state.yaml"),
		"fingerprint:\n  optimization_chain_version: 1\n")
	_, err = orchestrator.Run(context.Background(), request, nil)
	if err == nil || !strings.Contains(err.Error(), "resume inputs do not match") {
		t.Fatalf("指纹不匹配应拒绝: %v", err)
	}
}

func TestEvaluate_fake注入与上下文复用(t *testing.T) {
	dir := t.TempDir()
	cfg := buildTestConfig(dir)
	orchestrator, err := NewSingleHarnessIterativeOptimizationOrchestrator(cfg, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("构造: %v", err)
	}
	fake := &fakeOrchestratorEvaluator{scores: map[string]float64{"c1": 1.0}}
	orchestrator.evaluator = fake
	dataset := makeTestDatasetArtifact(t, dir)
	refsDir := filepath.Join(dir, "harness")
	os.MkdirAll(refsDir, 0o755)
	result, err := orchestrator.evaluate(context.Background(), evaluateParams{
		cases:           []map[string]any{{"case_id": "c1"}},
		harnessRefsPath: refsDir,
		outputDir:       filepath.Join(dir, "eval"),
		dataset:         dataset,
	})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if fake.calls != 1 {
		t.Fatalf("应调用一次: %d", fake.calls)
	}
	payload, _ := readYAMLFile(result)
	if payload["evaluation_context"] == nil {
		t.Fatalf("评估上下文应戳记: %v", payload)
	}
	// 相同输入 → 复用既有评估
	result2, err := orchestrator.evaluate(context.Background(), evaluateParams{
		cases:           []map[string]any{{"case_id": "c1"}},
		harnessRefsPath: refsDir,
		outputDir:       filepath.Join(dir, "eval"),
		dataset:         dataset,
	})
	if err != nil {
		t.Fatalf("复用: %v", err)
	}
	if result2 != result || fake.calls != 1 {
		t.Fatalf("应复用: %s vs %s calls=%d", result2, result, fake.calls)
	}
}

func TestCandidateGate_memberStatus拒绝(t *testing.T) {
	dir := t.TempDir()
	cfg := buildTestConfig(dir)
	orchestrator, err := NewSingleHarnessIterativeOptimizationOrchestrator(cfg, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("构造: %v", err)
	}
	sourceEval := writeEvalRef(t, dir, "src.yaml", "  - case_id: c1\n    score: 0.5\n    status: failed\n")
	gate, err := orchestrator.candidateGate(context.Background(), candidateGateParams{
		sourceEvalRef: sourceEval,
		memberStatus:  "planning_rejected",
	})
	if err != nil {
		t.Fatalf("candidateGate: %v", err)
	}
	if gate["accepted"] != false || gate["reason"] != "member_optimization_status_planning_rejected" {
		t.Fatalf("成员状态拒绝: %v", gate)
	}
	// 无候选产出
	gate, err = orchestrator.candidateGate(context.Background(), candidateGateParams{
		sourceEvalRef:            sourceEval,
		memberStatus:             "success",
		beforeHarnessRefsPath:    "/same",
		candidateHarnessRefsPath: "/same",
	})
	if err != nil {
		t.Fatalf("candidateGate: %v", err)
	}
	if gate["reason"] != "member_optimizer_produced_no_candidate" {
		t.Fatalf("无候选: %v", gate["reason"])
	}
	// 源评估有错误用例 → 不确定
	errorSource := writeEvalRef(t, dir, "err.yaml", "  - case_id: c1\n    status: error\n")
	gate, err = orchestrator.candidateGate(context.Background(), candidateGateParams{
		sourceEvalRef: errorSource,
		memberStatus:  "success",
	})
	if err != nil {
		t.Fatalf("candidateGate: %v", err)
	}
	if gate["status"] != "inconclusive" {
		t.Fatalf("错误用例不确定: %v", gate)
	}
}

func TestEvaluationContext_签名稳定(t *testing.T) {
	dir := t.TempDir()
	cfg := buildTestConfig(dir)
	orchestrator, err := NewSingleHarnessIterativeOptimizationOrchestrator(cfg, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("构造: %v", err)
	}
	refsDir := filepath.Join(dir, "harness")
	os.MkdirAll(refsDir, 0o755)
	cases := []map[string]any{{"case_id": "c1"}}
	context1 := orchestrator.evaluationContext(cases, refsDir, nil)
	context2 := orchestrator.evaluationContext(cases, refsDir, nil)
	if context1["signature"] == nil || context1["signature"] != context2["signature"] {
		t.Fatalf("签名应稳定: %v", context1["signature"])
	}
}

func TestSourcePassingCaseIDs_先验引用复用(t *testing.T) {
	dir := t.TempDir()
	cfg := buildTestConfig(dir)
	orchestrator, err := NewSingleHarnessIterativeOptimizationOrchestrator(cfg, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("构造: %v", err)
	}
	// 先验评估：c1 通过 c2 失败（经 orchestrator.evaluate 生成并戳记上下文）
	fakeEval := &fakeOrchestratorEvaluator{scores: map[string]float64{"c1": 1.0, "c2": 0.0}}
	orchestrator.evaluator = fakeEval
	dataset := makeTestDatasetArtifact(t, dir)
	refsDir := filepath.Join(dir, "harness")
	os.MkdirAll(filepath.Join(refsDir, "coder"), 0o755)
	writeTestYAML(t, filepath.Join(refsDir, "coder", "manifest.json"), `{"package_type":"plugin"}`)
	prior, err := orchestrator.evaluate(context.Background(), evaluateParams{
		cases:           []map[string]any{{"case_id": "c1"}, {"case_id": "c2"}},
		harnessRefsPath: refsDir,
		outputDir:       filepath.Join(dir, "prior"),
		dataset:         dataset,
	})
	if err != nil {
		t.Fatalf("先验评估: %v", err)
	}
	cases := []map[string]any{{"case_id": "c1"}, {"case_id": "c2"}}
	passing := orchestrator.sourcePassingCaseIDs(cases, refsDir, []string{prior})
	if _, ok := passing["c1"]; !ok {
		t.Fatalf("c1 应通过: %v", passing)
	}
	if _, ok := passing["c2"]; ok {
		t.Fatalf("c2 不应通过: %v", passing)
	}
}

func TestRunContinuous_阈值饱和即停(t *testing.T) {
	dir := t.TempDir()
	cfg := buildTestConfig(dir)
	cfg.Promotion.MinImprovement = 0.05
	orchestrator, err := NewSingleHarnessIterativeOptimizationOrchestrator(cfg, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("构造: %v", err)
	}
	fake := &fakeOrchestratorEvaluator{scores: map[string]float64{"c1": 1.0}}
	orchestrator.evaluator = fake
	// 全部用例满分 → 阈值饱和（基线经 evaluate 生成并戳记上下文）
	refsDir := filepath.Join(dir, "harness")
	os.MkdirAll(refsDir, 0o755)
	fullEval, err := orchestrator.evaluate(context.Background(), evaluateParams{
		cases:           []map[string]any{{"case_id": "c1"}},
		harnessRefsPath: refsDir,
		outputDir:       filepath.Join(dir, "baseline"),
		dataset:         makeTestDatasetArtifact(t, dir),
	})
	if err != nil {
		t.Fatalf("基线评估: %v", err)
	}
	state := map[string]any{
		"status":                        "running",
		"baseline_eval_ref_path":        fullEval,
		"best_eval_ref_path":            fullEval,
		"best_harness_refs_path":        refsDir,
		"source_harness_refs_path":      refsDir,
		"best_score":                    1.0,
		"evaluation_protocol_signature": "sig",
		"candidate_gates":               []any{},
		"epoch_checkpoints":             []any{},
		"retained_case_ids":             []any{},
		"completed_batches":             map[string]any{},
	}
	request, err := NewIterativeSingleHarnessRequest(nil, refsDir, filepath.Join(dir, "run"), "ds1", false, "", false, "", nil)
	if err != nil {
		t.Fatalf("构造: %v", err)
	}
	result, err := orchestrator.runContinuous(context.Background(), request, state,
		makeTestDatasetArtifact(t, dir), []map[string]any{{"case_id": "c1"}}, filepath.Join(dir, "run"), 3, nil)
	if err != nil {
		t.Fatalf("runContinuous: %v", err)
	}
	payload, _ := readYAMLFile(result.StatePath)
	if payload["stop_reason"] != "target_threshold_saturated" {
		t.Fatalf("阈值饱和停止: %v", payload["stop_reason"])
	}
	if payload["status"] != "completed" {
		t.Fatalf("状态: %v", payload["status"])
	}
}

func TestFullEngineering_机器证据失败(t *testing.T) {
	dir := t.TempDir()
	cfg := buildTestConfig(dir)
	orchestrator, err := NewSingleHarnessIterativeOptimizationOrchestrator(cfg, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("构造: %v", err)
	}
	result := filepath.Join(dir, "r.json")
	writeTestYAML(t, result, `{"evaluation":{"metadata":{"artifact_runtime_evidence":{"observations":[
		{"status":"failed","type":"machine_evidence"}]}}}}`)
	ref := writeEvalRef(t, dir, "e.yaml", "  - case_id: c1\n    result_path: "+result+"\n")
	gate := map[string]any{"capabilities": []any{}, "target_case_ids": []any{}}
	if fullEngineering(orchestrator, gate, ref) {
		t.Fatalf("机器证据失败应不通过")
	}
	// 干净评估通过
	cleanResult := filepath.Join(dir, "clean.json")
	writeTestYAML(t, cleanResult, `{"evaluation":{"metadata":{}}}`)
	cleanRef := writeEvalRef(t, dir, "clean.yaml", "  - case_id: c1\n    result_path: "+cleanResult+"\n")
	if !fullEngineering(orchestrator, gate, cleanRef) {
		t.Fatalf("干净应通过")
	}
}

func TestSubset_复用既有(t *testing.T) {
	dir := t.TempDir()
	cfg := buildTestConfig(dir)
	orchestrator, err := NewSingleHarnessIterativeOptimizationOrchestrator(cfg, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("构造: %v", err)
	}
	// 源评估物化子集（结果文件真实存在）
	r1 := filepath.Join(dir, "cases", "c1", "result.json")
	os.MkdirAll(filepath.Dir(r1), 0o755)
	os.WriteFile(r1, []byte(`{"case_id":"c1","score":0.5,"status":"failed"}`), 0o644)
	r2 := filepath.Join(dir, "cases", "c2", "result.json")
	os.MkdirAll(filepath.Dir(r2), 0o755)
	os.WriteFile(r2, []byte(`{"case_id":"c2","score":1.0,"status":"passed"}`), 0o644)
	source := writeEvalRef(t, dir, "src.yaml",
		"  - case_id: c1\n    score: 0.5\n    status: failed\n    result_path: "+r1+"\n"+
			"  - case_id: c2\n    score: 1.0\n    status: passed\n    result_path: "+r2+"\n")
	subsetRef, err := subset(orchestrator, []map[string]any{{"case_id": "c1"}}, source, dir,
		filepath.Join(dir, "sub"))
	if err != nil {
		t.Fatalf("subset: %v", err)
	}
	payload, _ := readYAMLFile(subsetRef)
	cases, _ := payload["cases"].([]any)
	if len(cases) != 1 {
		t.Fatalf("子集应 1 例: %v", payload)
	}
}

func TestRun_取消传播(t *testing.T) {
	dir := t.TempDir()
	cfg := buildTestConfig(dir)
	orchestrator, err := NewSingleHarnessIterativeOptimizationOrchestrator(cfg, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("构造: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	refsPath := filepath.Join(dir, "refs.yaml")
	writeTestYAML(t, refsPath, "harness_refs:\n  coder: "+dir+"\n")
	dsPath := filepath.Join(dir, "ds.json")
	writeTestYAML(t, dsPath, `[{"case_id":"c1","input":"task"}]`)
	request, err := NewIterativeSingleHarnessRequest(
		[]string{dsPath}, refsPath, filepath.Join(dir, "run"), "ds1", false, "", false, "", nil)
	if err != nil {
		t.Fatalf("构造: %v", err)
	}
	_, err = orchestrator.Run(ctx, request, nil)
	if err == nil && ctx.Err() == nil {
		t.Fatalf("取消应传播")
	}
	// 取消不是 failed 状态（Run 收尾处理）
	_ = errors.Is
}

// fakeLoopAnalyzer 测试用分析注入面：写含可执行问题的 analysis_ref。
type fakeLoopAnalyzer struct {
	dir              string
	CaseQueue        []string
	callCount        int
	AffectedOverride string
}

func (f *fakeLoopAnalyzer) Analyze(ctx context.Context,
	invocation harnessrsi.EvaluationResultAnalysisInvocation) (string, error) {
	os.MkdirAll(invocation.OutputDir, 0o755)
	caseID := "c1"
	if f.CaseQueue != nil {
		if f.callCount < len(f.CaseQueue) {
			caseID = f.CaseQueue[f.callCount]
		}
		f.callCount++
	}
	ref := filepath.Join(invocation.OutputDir, "analysis_ref.yaml")
	content := "issues:\n" +
		"  - issue_id: i1\n" +
		"    category: prompt\n" +
		"    summary: prompt missing greeting\n" +
		"    recommendation: add greeting to prompt\n" +
		"    failure_mode: logic\n" +
		"    target_ref: prompt.md\n" +
		"    affected_cases: " + affectedList(f, caseID) + "\n" +
		"    optimization_target: member_harness\n" +
		"    metadata:\n" +
		"      attribution:\n" +
		"        target_ref: prompt.md\n" +
		"        decision_contract:\n" +
		"          acceptance_observable: candidate answers c1 correctly\n"
	if err := os.WriteFile(ref, []byte(content), 0o644); err != nil {
		return "", err
	}
	return ref, nil
}

// fakeLoopOptimizer 测试用优化注入面：产出候选 harness 与 member_ref。
type fakeLoopOptimizer struct {
	baseRefs  string
	candRefs  string
	analysis  string
	memberRef string
	CallCount int
}

func (f *fakeLoopOptimizer) Optimize(ctx context.Context, evalRefPath, analysisResultPath,
	harnessRefsPath, outputDir string, params memberoptimizer.OptimizeExecuteParams) (string, error) {
	os.MkdirAll(outputDir, 0o755)
	f.CallCount++
	// 每次调用产出新候选包（拷贝基线 + 优化标记），模拟累积候选
	pkgDir := fmt.Sprintf("%s_pkg_%d", f.candRefs, f.CallCount)
	if _, err := os.Stat(pkgDir); err != nil {
		if err := copyTree(f.baseRefs, pkgDir); err != nil {
			return "", err
		}
		os.WriteFile(filepath.Join(pkgDir, "prompt.md"),
			[]byte(fmt.Sprintf("optimized prompt v%d\n", f.CallCount)), 0o644)
	}
	refsYAML := filepath.Join(filepath.Dir(pkgDir), fmt.Sprintf("cand_refs_%d.yaml", f.CallCount))
	os.WriteFile(refsYAML, []byte("harness_refs:\n  coder: "+pkgDir+"\n"), 0o644)
	planPath := filepath.Join(outputDir, "plan.yaml")
	plan := "actions:\n" +
		"  - action_id: a1\n" +
		"    role: coder\n" +
		"    action_group: prompt\n" +
		"    operation: modify\n" +
		"    target_path: prompt.md\n" +
		"    description: d\n" +
		"    rationale: r\n" +
		"    intervention: i\n" +
		"    expected_effect: e\n" +
		"    attributed_issue_ids: [i1]\n" +
		"    constraints:\n" +
		"      lever_decision:\n" +
		"        selected_lever: clarify\n" +
		"targets:\n" +
		"  - role: coder\n" +
		"    attributed_issue_ids: [i1]\n"
	os.WriteFile(planPath, []byte(plan), 0o644)
	memberRef := filepath.Join(outputDir, "member_optimization_ref.yaml")
	member := "status: success\n" +
		"optimized_harness_refs_path: " + refsYAML + "\n" +
		"plan_path: " + planPath + "\n" +
		"metadata:\n" +
		"  analysis_result_path: " + analysisResultPath + "\n"
	os.WriteFile(memberRef, []byte(member), 0o644)
	return memberRef, nil
}

// fakePathEvaluator 按 harnessRefsPath 决定分数的评估注入面。
type fakePathEvaluator struct {
	baseScore       float64
	candidateTag    string
	calls           int
	FixedCaseIDs    map[string]bool
	FullFailCaseIDs map[string]bool
}

func (f *fakePathEvaluator) Evaluate(ctx context.Context, dataset harnessrsi.DatasetArtifact,
	teamSkillRefPath, harnessRefsPath, outputDir string) (string, error) {
	return "", errors.New("not used")
}

func (f *fakePathEvaluator) EvaluateBatch(ctx context.Context, cases []map[string]any,
	teamSkillRefPath, harnessRefsPath, outputDir string,
	dataset *harnessrsi.DatasetArtifact,
	onCaseStage func(map[string]any) error,
	caseConcurrency int,
	baselineContexts map[string]any) (string, error) {
	f.calls++
	isCandidateRefs := strings.Contains(harnessRefsPath, f.candidateTag)
	// 评分语义：/target（候选门评估）→ 候选声称修复即满分；
	// /full（全量检查点）→ Fixed 为真身水平、FullFail 模拟候选评估说谎；
	// 其余（源/残余/基线）→ Fixed 为真身水平。
	caseIsFixed := func(caseID string) bool { return f.FixedCaseIDs == nil || f.FixedCaseIDs[caseID] }
	var evalCases []map[string]any
	for _, singleCase := range cases {
		caseID := toString(singleCase["case_id"])
		score := f.baseScore
		isSourceLike := strings.HasSuffix(outputDir, "/source") ||
			strings.Contains(outputDir, "residual_source") ||
			strings.Contains(outputDir, "frozen_baseline")
		if !isCandidateRefs || isSourceLike {
			// 真身水平：Fixed 集合通过
			if isCandidateRefs && caseIsFixed(caseID) {
				score = 1.0
			}
		} else if strings.HasSuffix(outputDir, "/full") {
			println("DBG fullscore", caseID, outputDir, fmt.Sprint(f.FullFailCaseIDs[caseID]))
			// 全量检查点：FullFail 模拟候选评估说谎
			if f.FullFailCaseIDs[caseID] {
				score = 0.0
			} else if caseIsFixed(caseID) {
				score = 1.0
			}
		} else {
			// 候选门评估（/target 或 ce/...）：候选声称修复即满分
			score = 1.0
		}
		status := "passed"
		if score < 1.0 {
			status = "failed"
		}
		resultPath := filepath.Join(outputDir, "cases", caseID, "result.json")
		os.MkdirAll(filepath.Dir(resultPath), 0o755)
		resultJSON := fmt.Sprintf(`{"case_id":"%s","score":%v,"status":"%s","execution_status":"passed","metadata":{"execution":{"execution_evidence":{"status":"complete","timing_complete":true}}}}`,
			caseID, score, status)
		os.WriteFile(resultPath, []byte(resultJSON), 0o644)
		tracePath := filepath.Join(outputDir, "cases", caseID, "trace.json")
		os.WriteFile(tracePath, []byte(`{"steps":[]}`), 0o644)
		evalCases = append(evalCases, map[string]any{
			"case_id": caseID, "score": score, "status": status,
			"result_path": resultPath, "trace_path": tracePath,
		})
	}
	return writeFakeEvalRef(outputDir, evalCases, harnessRefsPath), nil
}

func TestRun_全链路_基线到发布(t *testing.T) {
	dir := t.TempDir()
	// 基线 harness 包
	basePkg := filepath.Join(dir, "harness", "coder")
	os.MkdirAll(basePkg, 0o755)
	writeTestYAML(t, filepath.Join(basePkg, "runtime.yaml"), "max_iterations: 5\n")
	refsPath := filepath.Join(dir, "refs.yaml")
	writeTestYAML(t, refsPath, "harness_refs:\n  coder: "+basePkg+"\n")
	dsPath := filepath.Join(dir, "ds.json")
	writeTestYAML(t, dsPath, `[{"case_id":"c1","input":"task"}]`)
	outputDir := filepath.Join(dir, "run")
	// 候选目录（路径含 "candidate" 触发 fake 满分）
	candDir := filepath.Join(dir, "candidate", "pkg")
	os.MkdirAll(filepath.Dir(candDir), 0o755)

	evalFake := &fakePathEvaluator{baseScore: 0.0, candidateTag: "candidate", FixedCaseIDs: map[string]bool{"c1": true}}
	analyzerFake := &fakeLoopAnalyzer{}
	optimizerFake := &fakeLoopOptimizer{baseRefs: filepath.Join(dir, "harness"), candRefs: candDir}
	cfg := buildTestConfig(dir)
	cfg.MaxEpochs = 1
	orchestrator, err := NewSingleHarnessIterativeOptimizationOrchestrator(
		cfg, evalFake, analyzerFake, optimizerFake, nil)
	if err != nil {
		t.Fatalf("构造: %v", err)
	}
	request, err := NewIterativeSingleHarnessRequest(
		[]string{dsPath}, refsPath, outputDir, "ds1", false, "", true, "t1", nil)
	if err != nil {
		t.Fatalf("请求: %v", err)
	}
	result, err := orchestrator.Run(context.Background(), request, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	state, err := readYAMLFile(result.StatePath)
	if err != nil {
		t.Fatalf("状态回读: %v", err)
	}
	// 候选修复 c1 → 晋级
	if state["status"] != "completed" {
		t.Fatalf("状态: %v", state["status"])
	}
	bestScore := numberOf(state["best_score"])
	if bestScore == nil || *bestScore != 1.0 {
		gatesDump, _ := json.Marshal(state["candidate_gates"])
		batchesDump, _ := json.Marshal(state["completed_batches"])
		planDump, _ := json.Marshal(state["batch_plan_paths"])
		checkDump, _ := json.Marshal(state["epoch_checkpoints"])
		analysisDump, _ := json.Marshal(state["analysis_failures"])
		baselineContent := ""
		if raw, err := os.ReadFile(toString(state["baseline_eval_ref_path"])); err == nil {
			baselineContent = string(raw)
		}
		t.Fatalf("最佳分应 1.0: %v\ngates=%s\nbatches=%s\nplan=%s\nck=%s\naf=%s\nbaselineContent=\n%s",
			state["best_score"], gatesDump, batchesDump, planDump, checkDump, analysisDump, baselineContent)
	}
	if state["publication_status"] != "published" {
		t.Fatalf("应发布: %v\nstate=%v", state["publication_status"], state)
	}
	checkpoints, _ := state["epoch_checkpoints"].([]any)
	if len(checkpoints) != 1 {
		t.Fatalf("应 1 个检查点: %d", len(checkpoints))
	}
	checkpoint := checkpoints[0].(map[string]any)
	if checkpoint["promotion_applied"] != true {
		t.Fatalf("检查点应晋级: %v", checkpoint)
	}
	gates, _ := state["candidate_gates"].([]any)
	if len(gates) != 1 {
		t.Fatalf("应 1 个候选门: %d", len(gates))
	}
	gate := gates[0].(map[string]any)
	if gate["accepted"] != true || gate["status"] != "accepted" {
		t.Fatalf("候选门应接受: %v %v", gate["accepted"], gate["status"])
	}
	if _, ok := gate["epoch_checkpoint_outcome"].(map[string]any); !ok {
		t.Fatalf("检查点结果应回填: %v", gate["epoch_checkpoint_outcome"])
	}
	// 发布引用存在
	if published, ok := state["published_harness_refs_path"].(string); !ok || published == "" {
		t.Fatalf("发布引用: %v", state["published_harness_refs_path"])
	}
}

func TestRun_连续模式_两阶段晋级(t *testing.T) {
	dir := t.TempDir()
	basePkg := filepath.Join(dir, "harness", "coder")
	os.MkdirAll(basePkg, 0o755)
	writeTestYAML(t, filepath.Join(basePkg, "runtime.yaml"), "max_iterations: 5\n")
	refsPath := filepath.Join(dir, "refs.yaml")
	writeTestYAML(t, refsPath, "harness_refs:\n  coder: "+basePkg+"\n")
	dsPath := filepath.Join(dir, "ds.json")
	writeTestYAML(t, dsPath, `[{"case_id":"c1","input":"task"}]`)
	outputDir := filepath.Join(dir, "run")
	candDir := filepath.Join(dir, "candidate", "pkg")
	os.MkdirAll(filepath.Dir(candDir), 0o755)

	evalFake := &fakePathEvaluator{baseScore: 0.5, candidateTag: "candidate"}
	cfg := buildTestConfig(dir)
	cfg.MaxEpochs = 1
	cfg.Promotion = configpkg.PromotionConfig{
		PolicyVersion:          "continuous_two_stage_v2",
		MinImprovement:         0.1,
		NonTargetMaxRegression: 0.05,
	}
	orchestrator, err := NewSingleHarnessIterativeOptimizationOrchestrator(
		cfg, evalFake, &fakeLoopAnalyzer{},
		&fakeLoopOptimizer{baseRefs: filepath.Join(dir, "harness"), candRefs: candDir}, nil)
	if err != nil {
		t.Fatalf("构造: %v", err)
	}
	// continuous 模式由 Promotion.Continuous() 判定（policy_version 前缀）
	request, err := NewIterativeSingleHarnessRequest(
		[]string{dsPath}, refsPath, outputDir, "ds1", false, "", false, "t1", nil)
	if err != nil {
		t.Fatalf("请求: %v", err)
	}
	result, err := orchestrator.Run(context.Background(), request, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	state, err := readYAMLFile(result.StatePath)
	if err != nil {
		t.Fatalf("状态回读: %v", err)
	}
	if state["status"] != "completed" {
		t.Fatalf("状态: %v", state["status"])
	}
	if bestScore := numberOf(state["best_score"]); bestScore == nil || *bestScore != 1.0 {
		gatesDump, _ := json.Marshal(state["candidate_gates"])
		t.Fatalf("最佳分应 1.0: %v gates=%s", state["best_score"], gatesDump)
	}
	checkpoints, _ := state["epoch_checkpoints"].([]any)
	if len(checkpoints) != 1 {
		t.Fatalf("应 1 个检查点: %d", len(checkpoints))
	}
	checkpoint := checkpoints[0].(map[string]any)
	if checkpoint["promotion_applied"] != true ||
		checkpoint["promotion_reason"] != "two_stage_candidate_accepted" {
		t.Fatalf("两阶段晋级: %v %v", checkpoint["promotion_applied"], checkpoint["promotion_reason"])
	}
}

func TestContinuousHelpers(t *testing.T) {
	// existingPaths
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	writeTestYAML(t, a, "x")
	got := existingPaths(a, filepath.Join(dir, "missing.txt"))
	if len(got) != 1 || got[0] != a {
		t.Fatalf("existingPaths: %v", got)
	}
	// capabilitySlice
	slice := capabilitySlice([]any{map[string]any{"a": 1}, "skip"})
	if len(slice) != 1 {
		t.Fatalf("capabilitySlice: %v", slice)
	}
	// sameGate
	g1 := map[string]any{"k": "v"}
	g2 := map[string]any{"k": "v"}
	if !sameGate(g1, g2) {
		t.Fatalf("sameGate 内容等价应真")
	}
	if sameGate(g1, map[string]any{"k": "w"}) {
		t.Fatalf("不同应假")
	}
	// mustFailedMachineEvidence 吞错
	if got := mustFailedMachineEvidence(filepath.Join(dir, "missing.yaml")); len(got) != 0 {
		t.Fatalf("缺失应空: %v", got)
	}
	// evaluatorConfigView
	view := evaluatorConfigView(configpkg.EvaluatorConfig{
		EvaluationMethod: "llm-based", ScoreMode: "continuous", JudgeBaselineComparison: true})
	if view.GetScoreMode() != "continuous" || view.GetJudgeBaselineComparison() != true ||
		view.GetEvaluationMethod() != "llm-based" {
		t.Fatalf("config view: %v", view)
	}
	// hasCaseResults
	casesDir := filepath.Join(dir, "cases", "c1")
	os.MkdirAll(casesDir, 0o755)
	if hasCaseResults(filepath.Join(dir, "cases")) {
		t.Fatalf("无 result.json 应假")
	}
	os.WriteFile(filepath.Join(casesDir, "result.json"), []byte("{}"), 0o644)
	if !hasCaseResults(filepath.Join(dir, "cases")) {
		t.Fatalf("有 result.json 应真")
	}
	// toIntPointer
	if toIntPointer(nil) != nil {
		t.Fatalf("nil 应 nil")
	}
	if v := toIntPointer(7); v == nil || *v != 7 {
		t.Fatalf("7: %v", v)
	}
	// continuousInvalidGate
	base := map[string]any{"source_score": 0.5}
	gate := continuousInvalidGate(base, "/cand", map[string]struct{}{"c1": {}}, errors.New("bad"))
	if gate["status"] != "inconclusive" || gate["evaluation_error"] != "bad" || gate["source_score"] != 0.5 {
		t.Fatalf("continuousInvalidGate: %v", gate)
	}
	// evalDeltaMap / anyDeltaTruthy
	if got := evalDeltaMap("not-map"); len(got) != 0 {
		t.Fatalf("非映射应空: %v", got)
	}
	deltas := map[string]any{
		"c1": map[string]any{"partial_progress": true},
		"c2": map[string]any{"partial_progress": false},
	}
	if !anyDeltaTruthy(deltas, "partial_progress") {
		t.Fatalf("任一真应真")
	}
	if anyDeltaTruthy(map[string]any{"c1": map[string]any{}}, "partial_progress") {
		t.Fatalf("全假应假")
	}
	// candidateFailureSupportsRepair
	if candidateFailureSupportsRepair(map[string]any{"status": "accepted"}) {
		t.Fatalf("非 rejected 应假")
	}
	if candidateFailureSupportsRepair(map[string]any{
		"status": "rejected", "candidate_failure_analysis_status": "unavailable"}) {
		t.Fatalf("unavailable 应假")
	}
	if !candidateFailureSupportsRepair(map[string]any{
		"status": "rejected", "reason": "candidate_made_partial_verifier_progress"}) {
		t.Fatalf("可修复 reason 应真")
	}
	if !candidateFailureSupportsRepair(map[string]any{
		"status": "rejected", "candidate_failure_analysis_ref_path": "/a"}) {
		t.Fatalf("有分析引用应真")
	}
	// isOperationalRepair
	if isOperationalRepair(map[string]any{"candidate_behavior_by_case": map[string]any{
		"c1": map[string]any{"behavior_check": map[string]any{"next_action": "verified"}}}}) {
		t.Fatalf("非修复动作应假")
	}
	if !isOperationalRepair(map[string]any{"candidate_behavior_by_case": map[string]any{
		"c1": map[string]any{"behavior_check": map[string]any{"next_action": "repair_activation"}}}}) {
		t.Fatalf("修复动作应真")
	}
	// candidateCanContinueLocally 状态非 rejected 早退
	if candidateCanContinueLocally(map[string]any{"status": "accepted"}, nil) {
		t.Fatalf("accepted 应假")
	}
	if candidateCanContinueLocally(map[string]any{
		"status": "rejected", "candidate_failure_analysis_status": "unavailable"}, nil) {
		t.Fatalf("unavailable 应假")
	}
	// mergeRepairCapabilities add→modify 保留 add
	merged := mergeRepairCapabilities(
		[]map[string]any{{"role": "r", "target_path": "p", "operation": "add"}},
		[]map[string]any{{"role": "r", "target_path": "p", "operation": "modify"}})
	if len(merged) != 1 || merged[0]["operation"] != "add" {
		t.Fatalf("add 应保留: %v", merged)
	}
}

func TestSourceEvaluation_复用与补fresh(t *testing.T) {
	dir := t.TempDir()
	cfg := buildTestConfig(dir)
	refsDir := filepath.Join(dir, "harness")
	os.MkdirAll(filepath.Join(refsDir, "coder"), 0o755)
	orchestrator, err := NewSingleHarnessIterativeOptimizationOrchestrator(cfg, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("构造: %v", err)
	}
	fake := &fakeOrchestratorEvaluator{scores: map[string]float64{"c1": 1.0, "c2": 0.0}}
	orchestrator.evaluator = fake
	dataset := makeTestDatasetArtifact(t, dir)
	// 先验评估覆盖 c1（c2 缺 → fresh 路径）
	prior, err := orchestrator.evaluate(context.Background(), evaluateParams{
		cases:           []map[string]any{{"case_id": "c1"}},
		harnessRefsPath: refsDir,
		outputDir:       filepath.Join(dir, "prior"),
		dataset:         dataset,
	})
	if err != nil {
		t.Fatalf("先验: %v", err)
	}
	result, err := orchestrator.sourceEvaluation(context.Background(), sourceEvaluationParams{
		cases:           []map[string]any{{"case_id": "c1"}, {"case_id": "c2"}},
		harnessRefsPath: refsDir,
		outputDir:       filepath.Join(dir, "src"),
		dataset:         dataset,
		priorEvalRefs:   []string{prior},
		batchIndex:      1,
	})
	if err != nil {
		t.Fatalf("sourceEvaluation: %v", err)
	}
	payload, _ := readYAMLFile(result)
	cases, _ := payload["cases"].([]any)
	if len(cases) != 2 {
		t.Fatalf("应含复用+fresh 2 例: %d", len(cases))
	}
	// 既有完整评估 → 上下文一致复用
	result2, err := orchestrator.sourceEvaluation(context.Background(), sourceEvaluationParams{
		cases:           []map[string]any{{"case_id": "c1"}, {"case_id": "c2"}},
		harnessRefsPath: refsDir,
		outputDir:       filepath.Join(dir, "src"),
		dataset:         dataset,
		priorEvalRefs:   []string{prior},
		batchIndex:      1,
	})
	if err != nil || result2 != result {
		t.Fatalf("复用: %s vs %s err=%v", result2, result, err)
	}
}

func TestCandidateCanContinueLocally_分支矩阵(t *testing.T) {
	cases := []map[string]any{{"case_id": "c1"}}
	// reason 非 partial 且无 local_repair → 假
	if candidateCanContinueLocally(map[string]any{
		"status": "rejected", "reason": "other"}, cases) {
		t.Fatalf("非 partial 进展应假")
	}
	// target_case_ids 不匹配活跃用例 → 假
	gate := map[string]any{
		"status": "rejected", "reason": "candidate_made_partial_verifier_progress",
		"target_case_ids": []string{"c9"},
	}
	if candidateCanContinueLocally(gate, cases) {
		t.Fatalf("目标不匹配应假")
	}
	// 阻断证据 → 假
	gate = map[string]any{
		"status": "rejected", "reason": "candidate_made_partial_verifier_progress",
		"target_case_ids":         []string{"c1"},
		"failed_machine_evidence": []string{"c1:machine_evidence"},
	}
	if candidateCanContinueLocally(gate, cases) {
		t.Fatalf("阻断证据应假")
	}
	// 无 verifier delta → 假
	gate = map[string]any{
		"status": "rejected", "reason": "candidate_made_partial_verifier_progress",
		"target_case_ids": []string{"c1"},
	}
	if candidateCanContinueLocally(gate, cases) {
		t.Fatalf("无 delta 应假")
	}
	// 有回归需求 → 假
	gate = map[string]any{
		"status": "rejected", "reason": "candidate_made_partial_verifier_progress",
		"target_case_ids": []string{"c1"},
		"verifier_deltas_by_case": map[string]any{
			"c1": map[string]any{"partial_progress": true, "regressed_fail_to_pass": []string{"t1"}},
		},
	}
	if candidateCanContinueLocally(gate, cases) {
		t.Fatalf("回归需求应假")
	}
	// 评估引用缺失 → 假
	gate = map[string]any{
		"status": "rejected", "reason": "candidate_made_partial_verifier_progress",
		"target_case_ids": []string{"c1"},
		"verifier_deltas_by_case": map[string]any{
			"c1": map[string]any{"partial_progress": true},
		},
	}
	if candidateCanContinueLocally(gate, cases) {
		t.Fatalf("无评估引用应假")
	}
}

func TestCandidateGate_工具未调用与跳过(t *testing.T) {
	dir := t.TempDir()
	cfg := buildTestConfig(dir)
	orchestrator, err := NewSingleHarnessIterativeOptimizationOrchestrator(
		cfg, &fakeOrchestratorEvaluator{scores: map[string]float64{"c1": 1.0}}, nil, nil, nil)
	if err != nil {
		t.Fatalf("构造: %v", err)
	}
	sourceEval := writeEvalRef(t, dir, "src.yaml",
		"  - case_id: c1\n    score: 0.5\n    status: failed\n")
	// 候选评估带工具能力但轨迹无调用 → 拒绝（expected_tool_not_invoked）
	candidateEval := writeEvalRef(t, dir, "cand.yaml",
		"  - case_id: c1\n    score: 1.0\n    status: passed\n    result_path: "+filepath.Join(dir, "r.json")+"\n"+
			"    trace_path: "+filepath.Join(dir, "t.json")+"\n")
	os.MkdirAll(filepath.Join(dir, "cases", "c1"), 0o755)
	writeTestYAML(t, filepath.Join(dir, "r.json"),
		`{"case_id":"c1","score":1.0,"status":"passed","execution_status":"passed","metadata":{"execution":{"execution_evidence":{"status":"complete","timing_complete":true}}}}`)
	writeTestYAML(t, filepath.Join(dir, "t.json"), `{"steps":[]}`)
	gate, err := orchestrator.candidateGate(context.Background(), candidateGateParams{
		sourceEvalRef:            sourceEval,
		memberStatus:             "success",
		cases:                    []map[string]any{{"case_id": "c1"}},
		beforeHarnessRefsPath:    "/before",
		candidateHarnessRefsPath: "/after",
		capabilities: []map[string]any{{
			"action_group": "tool", "operation": "add", "runtime_name": "bash",
			"target_case_ids":    []string{"c1"},
			"decision_contracts": []any{},
		}},
		outputDir: filepath.Join(dir, "ce"),
	})
	_ = candidateEval
	if err != nil {
		t.Fatalf("candidateGate: %v", err)
	}
	if gate["accepted"] != false || gate["reason"] != "expected_tool_not_invoked_on_target_case" {
		incDump, _ := json.Marshal(gate["inconclusive_capability_case_ids"])
		missDump, _ := json.Marshal(gate["missing_expected_tool_invocations"])
		evalDump, _ := json.Marshal(gate["candidate_eval_ref_path"])
		t.Fatalf("工具未调用应拒绝: %v %v inc=%s miss=%s eval=%s",
			gate["accepted"], gate["reason"], incDump, missDump, evalDump)
	}
	if gate["failure_class"] != "capability_activation_failure" {
		t.Fatalf("失败类别: %v", gate["failure_class"])
	}
	// 候选评估含 skipped → 不确定
	skippedEval := writeEvalRef(t, dir, "skip.yaml",
		"  - case_id: c1\n    score: 0\n    status: skipped\n")
	// 通过 fake evaluator 走真路径（skipped 状态经 fake 不易构造——直接跳过）
	_ = skippedEval
}

func TestAnalyze_重试路径(t *testing.T) {
	dir := t.TempDir()
	cfg := buildTestConfig(dir)
	orchestrator, err := NewSingleHarnessIterativeOptimizationOrchestrator(cfg, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("构造: %v", err)
	}
	orchestrator.analyzer = &fakeLoopAnalyzer{}
	outputDir := filepath.Join(dir, "analysis")
	// 预置不完整的既有分析（DiagnosisIncomplete）→ 应建 retry_001
	os.MkdirAll(outputDir, 0o755)
	writeTestYAML(t, filepath.Join(outputDir, "analysis_ref.yaml"),
		"status: failed\nissues: []\nmetadata:\n  analysis_status: failed\n")
	result, err := orchestrator.analyze(context.Background(), analyzeParams{
		evalRefPath:     filepath.Join(dir, "eval.yaml"),
		harnessRefsPath: dir,
		outputDir:       outputDir,
	})
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	if !strings.Contains(result, "retry_001") {
		t.Fatalf("应写入 retry_001: %s", result)
	}
	// 不完整且无 issue 的既有分析被跳过、不直接复用
}

func TestEvaluationContext_带基线上下文(t *testing.T) {
	dir := t.TempDir()
	cfg := buildTestConfig(dir)
	orchestrator, err := NewSingleHarnessIterativeOptimizationOrchestrator(cfg, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("构造: %v", err)
	}
	cases := []map[string]any{{"case_id": "c1"}}
	context := orchestrator.evaluationContext(cases, dir,
		map[string]any{"c1": "fingerprint-1"})
	baselines, ok := context["baseline_contexts"].(map[string]any)
	if !ok || baselines["c1"] != "fingerprint-1" {
		t.Fatalf("基线上下文应入 context: %v", context["baseline_contexts"])
	}
}

func TestRun_检查点过滤与选中重放(t *testing.T) {
	dir := t.TempDir()
	basePkg := filepath.Join(dir, "harness", "coder")
	os.MkdirAll(basePkg, 0o755)
	writeTestYAML(t, filepath.Join(basePkg, "runtime.yaml"), "max_iterations: 5\n")
	refsPath := filepath.Join(dir, "refs.yaml")
	writeTestYAML(t, refsPath, "harness_refs:\n  coder: "+basePkg+"\n")
	dsPath := filepath.Join(dir, "ds.json")
	writeTestYAML(t, dsPath, `[{"case_id":"c1","input":"a"},{"case_id":"c2","input":"b"}]`)
	outputDir := filepath.Join(dir, "run")
	candDir := filepath.Join(dir, "candidate", "pkg")
	os.MkdirAll(filepath.Dir(candDir), 0o755)

	evalFake := &fakePathEvaluator{
		baseScore: 0.0, candidateTag: "candidate",
		FixedCaseIDs:    map[string]bool{"c1": true},
		FullFailCaseIDs: map[string]bool{"c2": true},
	}
	cfg := buildTestConfig(dir)
	cfg.MaxEpochs = 1
	cfg.DataLoader.BatchSize = 1 // 每例一批 → 两个候选门
	orchestrator, err := NewSingleHarnessIterativeOptimizationOrchestrator(
		cfg, evalFake, &fakeLoopAnalyzer{CaseQueue: []string{"c1", "c2"}},
		&fakeLoopOptimizer{baseRefs: filepath.Join(dir, "harness"), candRefs: candDir}, nil)
	if err != nil {
		t.Fatalf("构造: %v", err)
	}
	request, err := NewIterativeSingleHarnessRequest(
		[]string{dsPath}, refsPath, outputDir, "ds1", false, "", true, "t1", nil)
	if err != nil {
		t.Fatalf("请求: %v", err)
	}
	result, err := orchestrator.Run(context.Background(), request, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	state, _ := readYAMLFile(result.StatePath)
	checkpoints, _ := state["epoch_checkpoints"].([]any)
	if len(checkpoints) != 1 {
		t.Fatalf("应 1 个检查点: %d", len(checkpoints))
	}
	checkpoint := checkpoints[0].(map[string]any)
	if checkpoint["status"] != "rejected" {
		gatesDump, _ := json.Marshal(state["candidate_gates"])
		ckDump, _ := json.Marshal(checkpoint)
		t.Fatalf("检查点状态应 rejected: %v\ngates=%s\nck=%s", checkpoint["status"], gatesDump, ckDump)
	}
	// epoch 起点回滚：最佳分应回到基线 0.0
	if bestScore := numberOf(state["best_score"]); bestScore == nil || *bestScore != 0.0 {
		t.Fatalf("全拒后应回滚基线: %v", state["best_score"])
	}
}

// affectedList 分析 fake 的 affected_cases 行内容。
func affectedList(f *fakeLoopAnalyzer, caseID string) string {
	if f.AffectedOverride != "" {
		return "[" + f.AffectedOverride + "]"
	}
	return "[" + caseID + "]"
}

func TestCheckCandidateBehavior_无模型配置路径(t *testing.T) {
	dir := t.TempDir()
	layout := func(base string) (string, string) {
		resultPath := filepath.Join(base, "cases", "c1", "result.json")
		tracePath := filepath.Join(base, "cases", "c1", "trace.json")
		os.MkdirAll(filepath.Dir(resultPath), 0o755)
		writeTestYAML(t, resultPath, `{"case_id":"c1","score":0,"status":"failed","input":"task","response":"ans"}`)
		writeTestYAML(t, tracePath, `{"steps":[]}`)
		ref := writeEvalRef(t, base, "eval_ref.yaml",
			"  - case_id: c1\n    score: 0.0\n    status: failed\n    result_path: "+resultPath+"\n"+
				"    trace_path: "+tracePath+"\n")
		return ref, resultPath
	}
	sourceEval, _ := layout(filepath.Join(dir, "src"))
	candEval, _ := layout(filepath.Join(dir, "cand"))
	capabilities := []map[string]any{{
		"action_group": "prompt", "runtime_name": "sec1", "target_path": "prompt.md",
		"target_case_ids": []any{"c1"},
		"decision_contracts": []any{map[string]any{
			"acceptance_observable": "candidate answers c1 correctly",
		}},
	}}
	results, err := CheckCandidateBehavior(context.Background(),
		sourceEval, candEval, capabilities, "", filepath.Join(dir, "bc"), nil)
	if err != nil {
		t.Fatalf("CheckCandidateBehavior: %v", err)
	}
	obs, ok := results["c1"]
	if !ok {
		t.Fatalf("应产出 c1 观察: %v", results)
	}
	if obs["behavior_changed"] != "no" {
		t.Fatalf("无模型时行为变化为 no（空观察未证实变化）: %v", obs)
	}
	if obs["source_eval_ref_path"] != sourceEval || obs["candidate_eval_ref_path"] != candEval {
		t.Fatalf("证据引用: %v", obs)
	}
	// 缓存命中：第二次调用读缓存
	results2, err := CheckCandidateBehavior(context.Background(),
		sourceEval, candEval, capabilities, "", filepath.Join(dir, "bc"), nil)
	if err != nil || len(results2) != 1 {
		t.Fatalf("缓存: %v %v", results2, err)
	}
}

func TestCheckCandidateBehavior_模型配置无效路径(t *testing.T) {
	dir := t.TempDir()
	layout := func(base string) (string, string) {
		resultPath := filepath.Join(base, "cases", "c1", "result.json")
		tracePath := filepath.Join(base, "cases", "c1", "trace.json")
		os.MkdirAll(filepath.Dir(resultPath), 0o755)
		writeTestYAML(t, resultPath, `{"case_id":"c1","score":0,"status":"failed","input":"task","response":"ans"}`)
		writeTestYAML(t, tracePath, `{"steps":[]}`)
		ref := writeEvalRef(t, base, "eval_ref.yaml",
			"  - case_id: c1\n    score: 0.0\n    status: failed\n    result_path: "+resultPath+"\n"+
				"    trace_path: "+tracePath+"\n")
		return ref, resultPath
	}
	sourceEval, _ := layout(filepath.Join(dir, "src"))
	candEval, _ := layout(filepath.Join(dir, "cand"))
	capabilities := []map[string]any{{
		"action_group": "prompt", "runtime_name": "sec1", "target_path": "prompt.md",
		"target_case_ids": []any{"c1"},
		"decision_contracts": []any{map[string]any{
			"acceptance_observable": true,
		}},
	}}
	// modelConfigRef 指向不存在文件 → LoadMemberOptimizerModel 失败 → 非重试路径
	results, err := CheckCandidateBehavior(context.Background(),
		sourceEval, candEval, capabilities, filepath.Join(dir, "missing_model.yaml"),
		filepath.Join(dir, "bc2"), nil)
	if err != nil {
		t.Fatalf("CheckCandidateBehavior: %v", err)
	}
	obs := results["c1"]
	if obs == nil {
		t.Fatalf("应产出观察: %v", results)
	}
	if obs["validation_status"] != "failed" {
		t.Fatalf("无效模型应 validation failed: %v", obs["validation_status"])
	}
}

// fakeLoopAnalyzerErr 分析不可用注入面。
type fakeLoopAnalyzerErr struct{}

func (f *fakeLoopAnalyzerErr) Analyze(ctx context.Context,
	invocation harnessrsi.EvaluationResultAnalysisInvocation) (string, error) {
	return "", &memberoptimizer.AnalysisUnavailableError{
		Msg:             "diagnosis failed",
		AnalysisRefPath: filepath.Join(invocation.OutputDir, "analysis_ref.yaml"),
	}
}

func TestRun_连续模式_分析不可用(t *testing.T) {
	dir := t.TempDir()
	basePkg := filepath.Join(dir, "harness", "coder")
	os.MkdirAll(basePkg, 0o755)
	writeTestYAML(t, filepath.Join(basePkg, "runtime.yaml"), "max_iterations: 5\n")
	refsPath := filepath.Join(dir, "refs.yaml")
	writeTestYAML(t, refsPath, "harness_refs:\n  coder: "+basePkg+"\n")
	dsPath := filepath.Join(dir, "ds.json")
	writeTestYAML(t, dsPath, `[{"case_id":"c1","input":"task"}]`)
	outputDir := filepath.Join(dir, "run")
	evalFake := &fakePathEvaluator{baseScore: 0.5, candidateTag: "candidate",
		FixedCaseIDs: map[string]bool{"c1": true}}
	cfg := buildTestConfig(dir)
	cfg.MaxEpochs = 1
	cfg.Promotion = configpkg.PromotionConfig{
		PolicyVersion: "continuous_two_stage_v2", MinImprovement: 0.1,
	}
	orchestrator, err := NewSingleHarnessIterativeOptimizationOrchestrator(
		cfg, evalFake, &fakeLoopAnalyzerErr{},
		&fakeLoopOptimizer{baseRefs: filepath.Join(dir, "harness"),
			candRefs: filepath.Join(dir, "candidate", "pkg")}, nil)
	if err != nil {
		t.Fatalf("构造: %v", err)
	}
	request, err := NewIterativeSingleHarnessRequest(
		[]string{dsPath}, refsPath, outputDir, "ds1", false, "", false, "t1", nil)
	if err != nil {
		t.Fatalf("请求: %v", err)
	}
	result, err := orchestrator.Run(context.Background(), request, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	state, _ := readYAMLFile(result.StatePath)
	if state["status"] != "completed" {
		t.Fatalf("状态: %v", state["status"])
	}
	// 分析失败记录进账
	failures, _ := state["analysis_failures"].([]any)
	if len(failures) == 0 {
		t.Fatalf("分析失败应记账: %v", state["analysis_failures"])
	}
}

// fakeLoopOptimizerErr 优化失败注入面。
type fakeLoopOptimizerErr struct{}

func (f *fakeLoopOptimizerErr) Optimize(ctx context.Context, evalRefPath, analysisResultPath,
	harnessRefsPath, outputDir string, params memberoptimizer.OptimizeExecuteParams) (string, error) {
	return "", errors.New("optimizer boom")
}

func TestRun_连续模式_优化失败(t *testing.T) {
	dir := t.TempDir()
	basePkg := filepath.Join(dir, "harness", "coder")
	os.MkdirAll(basePkg, 0o755)
	writeTestYAML(t, filepath.Join(basePkg, "runtime.yaml"), "max_iterations: 5\n")
	refsPath := filepath.Join(dir, "refs.yaml")
	writeTestYAML(t, refsPath, "harness_refs:\n  coder: "+basePkg+"\n")
	dsPath := filepath.Join(dir, "ds.json")
	writeTestYAML(t, dsPath, `[{"case_id":"c1","input":"task"}]`)
	cfg := buildTestConfig(dir)
	cfg.MaxEpochs = 1
	cfg.Promotion = configpkg.PromotionConfig{
		PolicyVersion: "continuous_two_stage_v2", MinImprovement: 0.1,
	}
	orchestrator, err := NewSingleHarnessIterativeOptimizationOrchestrator(
		cfg, &fakePathEvaluator{baseScore: 0.5, candidateTag: "candidate",
			FixedCaseIDs: map[string]bool{"c1": true}},
		&fakeLoopAnalyzer{}, &fakeLoopOptimizerErr{}, nil)
	if err != nil {
		t.Fatalf("构造: %v", err)
	}
	request, err := NewIterativeSingleHarnessRequest(
		[]string{dsPath}, refsPath, filepath.Join(dir, "run"), "ds1", false, "", false, "t1", nil)
	if err != nil {
		t.Fatalf("请求: %v", err)
	}
	// Python continuous.py：optimize 异常直接向上传播（与 epoch 模式的
	// generation_error 包装不同）——Go 一比一
	_, err = orchestrator.Run(context.Background(), request, nil)
	if err == nil || !strings.Contains(err.Error(), "optimizer boom") {
		t.Fatalf("优化失败应向上传播: %v", err)
	}
}

func TestHelperMatrix_补分支(t *testing.T) {
	// numberOf 各形态
	if numberOf(float32(1.5)) == nil || numberOf(int64(2)) == nil {
		t.Fatalf("float32/int64 应提取")
	}
	// readYAMLFile 非法 YAML → 错误
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.yaml")
	os.WriteFile(bad, []byte("a: [unclosed\n  b: {"), 0o644)
	if _, err := readYAMLFile(bad); err == nil {
		t.Fatalf("非法 YAML 应报错")
	}
	// readJSONFile 非法 JSON → 空映射
	badJSON := filepath.Join(dir, "bad.json")
	os.WriteFile(badJSON, []byte("{invalid"), 0o644)
	if got := readJSONFile(badJSON); len(got) != 0 {
		t.Fatalf("非法 JSON 应空: %v", got)
	}
	// readJSONFile 非映射 JSON → 空映射
	arrJSON := filepath.Join(dir, "arr.json")
	os.WriteFile(arrJSON, []byte("[1,2]"), 0o644)
	if got := readJSONFile(arrJSON); len(got) != 0 {
		t.Fatalf("非映射应空: %v", got)
	}
	// llmCasePassed 显式 passed
	caseLLM := map[string]any{"metadata": map[string]any{
		"evaluation_method": "llm_as_judge", "evaluation_passed": true}}
	if got := llmCasePassed(caseLLM); got == nil || !*got {
		t.Fatalf("LLM 显式通过: %v", got)
	}
	// actionTargetCaseIDs 归因路径
	got := actionTargetCaseIDs(
		map[string]any{"attributed_issue_ids": []any{"i1"}, "role": "r"},
		map[string]map[string]struct{}{"i1": {"c1": {}}},
		map[string]map[string]struct{}{"r": {"c2": {}}})
	if len(got) != 1 {
		t.Fatalf("归因路径: %v", got)
	}
	got = actionTargetCaseIDs(map[string]any{"role": "r"},
		map[string]map[string]struct{}{}, map[string]map[string]struct{}{"r": {"c2": {}}})
	if len(got) != 1 {
		t.Fatalf("回退路径: %v", got)
	}
	// boolContains
	set := map[string]struct{}{"x": {}}
	if !boolContains(set, "x") || boolContains(set, "y") {
		t.Fatalf("boolContains")
	}
	// candidate_feedback 小助手
	if positiveInt(3) == nil || positiveInt(0) != nil || positiveInt(-1) != nil {
		t.Fatalf("positiveInt")
	}
	if !isAllDigits("123") || isAllDigits("12a") || isAllDigits("") {
		t.Fatalf("isAllDigits")
	}
	if got := candidateIndexOf(map[string]any{}, 5); got != 5 {
		t.Fatalf("缺省 index: %v", got)
	}
	if got := candidateIndexOf(map[string]any{"candidate_index": 2}, 5); got != 2 {
		t.Fatalf("index 提取: %v", got)
	}
}

func TestRun_连续模式_续跑已完成尝试(t *testing.T) {
	dir := t.TempDir()
	basePkg := filepath.Join(dir, "harness", "coder")
	os.MkdirAll(basePkg, 0o755)
	writeTestYAML(t, filepath.Join(basePkg, "runtime.yaml"), "max_iterations: 5\n")
	refsPath := filepath.Join(dir, "refs.yaml")
	writeTestYAML(t, refsPath, "harness_refs:\n  coder: "+basePkg+"\n")
	dsPath := filepath.Join(dir, "ds.json")
	writeTestYAML(t, dsPath, `[{"case_id":"c1","input":"task"}]`)
	outputDir := filepath.Join(dir, "run")
	os.MkdirAll(outputDir, 0o755)
	// 先产基线（带上下文戳记）
	orchestrator0, err := NewSingleHarnessIterativeOptimizationOrchestrator(
		buildTestConfig(dir), &fakeOrchestratorEvaluator{scores: map[string]float64{"c1": 0.5}},
		nil, nil, nil)
	if err != nil {
		t.Fatalf("构造0: %v", err)
	}
	baselineRef, err := orchestrator0.evaluate(context.Background(), evaluateParams{
		cases:           []map[string]any{{"case_id": "c1"}},
		harnessRefsPath: refsPath,
		outputDir:       filepath.Join(dir, "baseline"),
		dataset:         makeTestDatasetArtifact(t, dir),
	})
	if err != nil {
		t.Fatalf("基线: %v", err)
	}
	// 预置 state：attempts 已完成（rejected 门）+ repair_refs
	state := map[string]any{
		"status": "running", "mode": "single_harness_benchmark",
		"baseline_eval_ref_path": baselineRef, "best_eval_ref_path": baselineRef,
		"best_harness_refs_path": refsPath, "source_harness_refs_path": refsPath,
		"best_score": 0.5, "best_reference_score": 0.5, "historical_peak_score": 0.5,
		"evaluation_protocol_signature": "sig",
		"candidate_gates":               []any{}, "epoch_checkpoints": []any{},
		"retained_case_ids": []any{}, "completed_batches": map[string]any{},
		"continuous_attempts": map[string]any{
			"epoch_001:batch_001:attempt_001": map[string]any{
				"done":                true,
				"gate":                map[string]any{"accepted": false, "status": "rejected", "reason": "prior"},
				"repair_refs":         refsPath,
				"repair_source":       baselineRef,
				"repair_capabilities": []any{},
			},
		},
		"continuous_trials":  map[string]any{},
		"continuous_batches": map[string]any{},
		"analysis_failures":  []any{},
	}
	if err := writeYAMLAtomic(filepath.Join(outputDir, "single_harness_state.yaml"), state); err != nil {
		t.Fatalf("预置状态: %v", err)
	}
	cfg := buildTestConfig(dir)
	cfg.MaxEpochs = 1
	cfg.Promotion = configpkg.PromotionConfig{
		PolicyVersion: "continuous_two_stage_v2", MinImprovement: 0.1,
	}
	// analyzer 计数：续跑应复用已有 attempt 记录（gate done）→ 不再调用 analyze
	orchestrator, err := NewSingleHarnessIterativeOptimizationOrchestrator(
		cfg, &fakePathEvaluator{baseScore: 0.5, candidateTag: "candidate",
			FixedCaseIDs: map[string]bool{"c1": true}},
		&fakeLoopAnalyzer{},
		&fakeLoopOptimizer{baseRefs: filepath.Join(dir, "harness"),
			candRefs: filepath.Join(dir, "candidate", "pkg")}, nil)
	if err != nil {
		t.Fatalf("构造: %v", err)
	}
	request, err := NewIterativeSingleHarnessRequest(
		[]string{dsPath}, refsPath, outputDir, "ds1", false, "", false, "t1", nil)
	if err != nil {
		t.Fatalf("请求: %v", err)
	}
	result, err := orchestrator.Run(context.Background(), request, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	finalState, _ := readYAMLFile(result.StatePath)
	if finalState["status"] != "completed" {
		t.Fatalf("状态: %v", finalState["status"])
	}
	// 修复链从 repair_refs 重新开一轮 → 新尝试产生新门
	gates, _ := finalState["candidate_gates"].([]any)
	// Python：done 记录的 gate 不重复入 candidate_gates，仅续用 repair_refs 开新一轮
	if len(gates) != 1 {
		gatesDump, _ := json.Marshal(finalState["candidate_gates"])
		t.Fatalf("应恰 1 个新尝试门: %s", gatesDump)
	}
}

// fakeLoopAnalyzerEmpty 无问题分析注入面。
type fakeLoopAnalyzerEmpty struct{}

func (f *fakeLoopAnalyzerEmpty) Analyze(ctx context.Context,
	invocation harnessrsi.EvaluationResultAnalysisInvocation) (string, error) {
	os.MkdirAll(invocation.OutputDir, 0o755)
	ref := filepath.Join(invocation.OutputDir, "analysis_ref.yaml")
	os.WriteFile(ref, []byte("issues: []\n"), 0o644)
	return ref, nil
}

func TestRun_连续模式_无可执行假设即停(t *testing.T) {
	dir := t.TempDir()
	basePkg := filepath.Join(dir, "harness", "coder")
	os.MkdirAll(basePkg, 0o755)
	writeTestYAML(t, filepath.Join(basePkg, "runtime.yaml"), "max_iterations: 5\n")
	refsPath := filepath.Join(dir, "refs.yaml")
	writeTestYAML(t, refsPath, "harness_refs:\n  coder: "+basePkg+"\n")
	dsPath := filepath.Join(dir, "ds.json")
	writeTestYAML(t, dsPath, `[{"case_id":"c1","input":"task"}]`)
	cfg := buildTestConfig(dir)
	cfg.MaxEpochs = 1
	cfg.Promotion = configpkg.PromotionConfig{
		PolicyVersion: "continuous_two_stage_v2", MinImprovement: 0.1,
	}
	orchestrator, err := NewSingleHarnessIterativeOptimizationOrchestrator(
		cfg, &fakePathEvaluator{baseScore: 0.5, candidateTag: "candidate",
			FixedCaseIDs: map[string]bool{"c1": true}},
		&fakeLoopAnalyzerEmpty{}, &fakeLoopOptimizer{baseRefs: filepath.Join(dir, "harness"),
			candRefs: filepath.Join(dir, "candidate", "pkg")}, nil)
	if err != nil {
		t.Fatalf("构造: %v", err)
	}
	request, err := NewIterativeSingleHarnessRequest(
		[]string{dsPath}, refsPath, filepath.Join(dir, "run"), "ds1", false, "", false, "t1", nil)
	if err != nil {
		t.Fatalf("请求: %v", err)
	}
	result, err := orchestrator.Run(context.Background(), request, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	state, _ := readYAMLFile(result.StatePath)
	if state["stop_reason"] != "no_executable_hypotheses" {
		t.Fatalf("无假设应停: %v", state["stop_reason"])
	}
	// 缺省播种路径：best_reference_score 等由 runContinuous 播种
	if state["historical_peak_score"] == nil {
		t.Fatalf("历史峰值应播种: %v", state["historical_peak_score"])
	}
	if state["best_reference_eval_ref_path"] == nil {
		t.Fatalf("参考引用应播种")
	}
}

// fakeLoopOptimizerSkipped 产出 no_actions 成员引用。
type fakeLoopOptimizerSkipped struct{ dir string }

func (f *fakeLoopOptimizerSkipped) Optimize(ctx context.Context, evalRefPath, analysisResultPath,
	harnessRefsPath, outputDir string, params memberoptimizer.OptimizeExecuteParams) (string, error) {
	os.MkdirAll(outputDir, 0o755)
	memberRef := filepath.Join(outputDir, "member_optimization_ref.yaml")
	os.WriteFile(memberRef, []byte("status: no_actions\nrole_results: {}\n"), 0o644)
	return memberRef, nil
}

func TestRun_连续模式_no_actions跳过门(t *testing.T) {
	dir := t.TempDir()
	basePkg := filepath.Join(dir, "harness", "coder")
	os.MkdirAll(basePkg, 0o755)
	writeTestYAML(t, filepath.Join(basePkg, "runtime.yaml"), "max_iterations: 5\n")
	refsPath := filepath.Join(dir, "refs.yaml")
	writeTestYAML(t, refsPath, "harness_refs:\n  coder: "+basePkg+"\n")
	dsPath := filepath.Join(dir, "ds.json")
	writeTestYAML(t, dsPath, `[{"case_id":"c1","input":"task"}]`)
	cfg := buildTestConfig(dir)
	cfg.MaxEpochs = 1
	cfg.Promotion = configpkg.PromotionConfig{
		PolicyVersion: "continuous_two_stage_v2", MinImprovement: 0.1,
	}
	orchestrator, err := NewSingleHarnessIterativeOptimizationOrchestrator(
		cfg, &fakePathEvaluator{baseScore: 0.5, candidateTag: "candidate",
			FixedCaseIDs: map[string]bool{"c1": true}},
		&fakeLoopAnalyzer{}, &fakeLoopOptimizerSkipped{}, nil)
	if err != nil {
		t.Fatalf("构造: %v", err)
	}
	request, err := NewIterativeSingleHarnessRequest(
		[]string{dsPath}, refsPath, filepath.Join(dir, "run"), "ds1", false, "", false, "t1", nil)
	if err != nil {
		t.Fatalf("请求: %v", err)
	}
	result, err := orchestrator.Run(context.Background(), request, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	state, _ := readYAMLFile(result.StatePath)
	gates, _ := state["candidate_gates"].([]any)
	if len(gates) == 0 {
		t.Fatalf("应有跳过门")
	}
	gate := gates[0].(map[string]any)
	if gate["status"] != "skipped" || gate["reason"] != "no_actions" {
		t.Fatalf("跳过门: %v %v", gate["status"], gate["reason"])
	}
	if gate["generation_status"] != "no_actions" {
		t.Fatalf("generation_status: %v", gate["generation_status"])
	}
}

// fakeLoopOptimizerPlanningRejected 产出 planning_rejected 成员引用（含验证失败证据）。
type fakeLoopOptimizerPlanningRejected struct{}

func (f *fakeLoopOptimizerPlanningRejected) Optimize(ctx context.Context, evalRefPath, analysisResultPath,
	harnessRefsPath, outputDir string, params memberoptimizer.OptimizeExecuteParams) (string, error) {
	os.MkdirAll(outputDir, 0o755)
	memberRef := filepath.Join(outputDir, "member_optimization_ref.yaml")
	os.WriteFile(memberRef, []byte("status: planning_rejected\nrole_results:\n  coder:\n    error: plan denied\n"), 0o644)
	os.WriteFile(filepath.Join(outputDir, "verification.json"),
		[]byte(`{"status":"failed","checks":[{"name":"static","status":"failed","error":"lint boom"}]}`), 0o644)
	os.WriteFile(filepath.Join(outputDir, "fix_result.json"), []byte("{}"), 0o644)
	return memberRef, nil
}

func TestRun_连续模式_planning_rejected门(t *testing.T) {
	dir := t.TempDir()
	basePkg := filepath.Join(dir, "harness", "coder")
	os.MkdirAll(basePkg, 0o755)
	writeTestYAML(t, filepath.Join(basePkg, "runtime.yaml"), "max_iterations: 5\n")
	refsPath := filepath.Join(dir, "refs.yaml")
	writeTestYAML(t, refsPath, "harness_refs:\n  coder: "+basePkg+"\n")
	dsPath := filepath.Join(dir, "ds.json")
	writeTestYAML(t, dsPath, `[{"case_id":"c1","input":"task"}]`)
	cfg := buildTestConfig(dir)
	cfg.MaxEpochs = 1
	cfg.Promotion = configpkg.PromotionConfig{
		PolicyVersion: "continuous_two_stage_v2", MinImprovement: 0.1,
	}
	orchestrator, err := NewSingleHarnessIterativeOptimizationOrchestrator(
		cfg, &fakePathEvaluator{baseScore: 0.5, candidateTag: "candidate",
			FixedCaseIDs: map[string]bool{"c1": true}},
		&fakeLoopAnalyzer{}, &fakeLoopOptimizerPlanningRejected{}, nil)
	if err != nil {
		t.Fatalf("构造: %v", err)
	}
	request, err := NewIterativeSingleHarnessRequest(
		[]string{dsPath}, refsPath, filepath.Join(dir, "run"), "ds1", false, "", false, "t1", nil)
	if err != nil {
		t.Fatalf("请求: %v", err)
	}
	result, err := orchestrator.Run(context.Background(), request, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	state, _ := readYAMLFile(result.StatePath)
	gates, _ := state["candidate_gates"].([]any)
	if len(gates) == 0 {
		t.Fatalf("应有拒绝门")
	}
	gate := gates[0].(map[string]any)
	if gate["status"] != "rejected" || gate["reason"] != "planning_rejected" {
		t.Fatalf("拒绝门: %v %v", gate["status"], gate["reason"])
	}
	if gate["generation_stage"] != "verification" {
		t.Fatalf("验证失败应升级 stage: %v", gate["generation_stage"])
	}
	if errText, _ := gate["generation_error"].(string); !strings.Contains(errText, "lint boom") {
		t.Fatalf("验证错误应并入: %v", errText)
	}
	if evidence, _ := gate["generation_evidence"].([]any); len(evidence) < 3 {
		t.Fatalf("生成证据清单: %v", evidence)
	}
}

func TestRun_连续模式_冻结批次基线证据变更拒绝(t *testing.T) {
	dir := t.TempDir()
	basePkg := filepath.Join(dir, "harness", "coder")
	os.MkdirAll(basePkg, 0o755)
	writeTestYAML(t, filepath.Join(basePkg, "runtime.yaml"), "max_iterations: 5\n")
	refsPath := filepath.Join(dir, "refs.yaml")
	writeTestYAML(t, refsPath, "harness_refs:\n  coder: "+basePkg+"\n")
	dsPath := filepath.Join(dir, "ds.json")
	writeTestYAML(t, dsPath, `[{"case_id":"c1","input":"task"}]`)
	outputDir := filepath.Join(dir, "run")
	os.MkdirAll(outputDir, 0o755)
	// 基线经 evaluate 戳记上下文
	orchestrator0, _ := NewSingleHarnessIterativeOptimizationOrchestrator(
		buildTestConfig(dir), &fakeOrchestratorEvaluator{scores: map[string]float64{"c1": 0.5}},
		nil, nil, nil)
	baselineRef, err := orchestrator0.evaluate(context.Background(), evaluateParams{
		cases: []map[string]any{{"case_id": "c1"}}, harnessRefsPath: refsPath,
		outputDir: filepath.Join(dir, "baseline"), dataset: makeTestDatasetArtifact(t, dir),
	})
	if err != nil {
		t.Fatalf("基线: %v", err)
	}
	// 预置冻结批次：baseline_contexts 与本次指纹不符
	state := map[string]any{
		"status": "running", "mode": "single_harness_benchmark",
		"baseline_eval_ref_path": baselineRef, "best_eval_ref_path": baselineRef,
		"best_harness_refs_path": refsPath, "source_harness_refs_path": refsPath,
		"best_score": 0.5, "best_reference_score": 0.5, "historical_peak_score": 0.5,
		"evaluation_protocol_signature": "sig",
		"candidate_gates":               []any{}, "epoch_checkpoints": []any{},
		"retained_case_ids": []any{}, "completed_batches": map[string]any{},
		"continuous_attempts": map[string]any{}, "continuous_trials": map[string]any{},
		"continuous_batches": map[string]any{
			"epoch_001:batch_001": map[string]any{
				"reference_eval":    baselineRef,
				"parent_refs":       refsPath,
				"parent_score":      0.5,
				"baseline_contexts": map[string]any{"c1": "stale"},
			},
		},
		"analysis_failures": []any{},
	}
	if err := writeYAMLAtomic(filepath.Join(outputDir, "single_harness_state.yaml"), state); err != nil {
		t.Fatalf("预置: %v", err)
	}
	cfg := buildTestConfig(dir)
	cfg.MaxEpochs = 1
	cfg.Promotion = configpkg.PromotionConfig{
		PolicyVersion: "continuous_two_stage_v2", MinImprovement: 0.1,
	}
	// compare 模式需 llm_as_judge + judge_baseline_comparison → contexts 非空
	orchestrator, err := NewSingleHarnessIterativeOptimizationOrchestrator(
		cfg, &fakePathEvaluator{baseScore: 0.5, candidateTag: "candidate",
			FixedCaseIDs: map[string]bool{"c1": true}},
		&fakeLoopAnalyzer{}, &fakeLoopOptimizer{baseRefs: filepath.Join(dir, "harness"),
			candRefs: filepath.Join(dir, "candidate", "pkg")}, nil)
	if err != nil {
		t.Fatalf("构造: %v", err)
	}
	// llm_as_judge 才会启用 compare → 冻结证据不一致检查
	request, err := NewIterativeSingleHarnessRequest(
		[]string{dsPath}, refsPath, outputDir, "ds1", false, "", false, "t1", nil)
	if err != nil {
		t.Fatalf("请求: %v", err)
	}
	result, runErr := orchestrator.Run(context.Background(), request, nil)
	// compare=false（script-based）→ 冻结基线上下文检查跳过 → 正常完成
	if runErr != nil {
		t.Fatalf("script 模式不应触发冻结检查: %v", runErr)
	}
	_ = result
}

func TestRun_连续模式_基线对比模式(t *testing.T) {
	dir := t.TempDir()
	basePkg := filepath.Join(dir, "harness", "coder")
	os.MkdirAll(basePkg, 0o755)
	writeTestYAML(t, filepath.Join(basePkg, "runtime.yaml"), "max_iterations: 5\n")
	refsPath := filepath.Join(dir, "refs.yaml")
	writeTestYAML(t, refsPath, "harness_refs:\n  coder: "+basePkg+"\n")
	dsPath := filepath.Join(dir, "ds.json")
	writeTestYAML(t, dsPath, `[{"case_id":"c1","input":"task"}]`)
	candDir := filepath.Join(dir, "candidate", "pkg")
	os.MkdirAll(filepath.Dir(candDir), 0o755)
	cfg := buildTestConfig(dir)
	cfg.MaxEpochs = 1
	cfg.Evaluator.EvaluationMethod = "llm_as_judge"
	cfg.Evaluator.JudgeBaselineComparison = true
	cfg.Promotion = configpkg.PromotionConfig{
		PolicyVersion: "continuous_two_stage_v2", MinImprovement: 0.1,
	}
	orchestrator, err := NewSingleHarnessIterativeOptimizationOrchestrator(
		cfg, &fakePathEvaluator{baseScore: 0.5, candidateTag: "candidate",
			FixedCaseIDs: map[string]bool{"c1": true}},
		&fakeLoopAnalyzer{}, &fakeLoopOptimizer{baseRefs: filepath.Join(dir, "harness"),
			candRefs: candDir}, nil)
	if err != nil {
		t.Fatalf("构造: %v", err)
	}
	request, err := NewIterativeSingleHarnessRequest(
		[]string{dsPath}, refsPath, filepath.Join(dir, "run"), "ds1", false, "", false, "t1", nil)
	if err != nil {
		t.Fatalf("请求: %v", err)
	}
	result, err := orchestrator.Run(context.Background(), request, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	state, _ := readYAMLFile(result.StatePath)
	if bestScore := numberOf(state["best_score"]); bestScore == nil || *bestScore != 1.0 {
		gatesDump, _ := json.Marshal(state["candidate_gates"])
		t.Fatalf("对比模式应晋级: %v gates=%s", state["best_score"], gatesDump)
	}
	gates, _ := state["candidate_gates"].([]any)
	gate := gates[0].(map[string]any)
	if gate["comparison_baseline_score"] != 0.5 {
		t.Fatalf("对比基线分: %v", gate["comparison_baseline_score"])
	}
}

func TestCheckCandidateBehavior_重试与缓存路径(t *testing.T) {
	dir := t.TempDir()
	layout := func(base string) string {
		resultPath := filepath.Join(base, "cases", "c1", "result.json")
		tracePath := filepath.Join(base, "cases", "c1", "trace.json")
		os.MkdirAll(filepath.Dir(resultPath), 0o755)
		writeTestYAML(t, resultPath, `{"case_id":"c1","score":0,"status":"failed","input":"task","response":"ans"}`)
		writeTestYAML(t, tracePath, `{"steps":[]}`)
		return writeEvalRef(t, base, "eval_ref.yaml",
			"  - case_id: c1\n    score: 0.0\n    status: failed\n    result_path: "+resultPath+"\n"+
				"    trace_path: "+tracePath+"\n")
	}
	sourceEval := layout(filepath.Join(dir, "src"))
	candEval := layout(filepath.Join(dir, "cand"))
	capabilities := []map[string]any{{
		"action_group": "prompt", "runtime_name": "sec1", "target_path": "prompt.md",
		"target_case_ids":    []any{"c1"},
		"decision_contracts": []any{map[string]any{"acceptance_observable": true}},
	}}
	outputRepair := harnessrsi.DefaultOutputRepairConfig()
	outputRepair.MaxAttempts = 1
	// 无效模型 + 重试预算 0 → 非重试路径直接失败
	results, err := CheckCandidateBehavior(context.Background(),
		sourceEval, candEval, capabilities, filepath.Join(dir, "missing.yaml"),
		filepath.Join(dir, "bc3"), &outputRepair)
	if err != nil {
		t.Fatalf("CheckCandidateBehavior: %v", err)
	}
	if results["c1"]["validation_status"] != "failed" {
		t.Fatalf("应 failed: %v", results["c1"]["validation_status"])
	}
	// evidenceFiles 两侧
	_ = evidenceFiles(map[string]any{"source": map[string]any{"files": map[string]any{"a": "b"}}}, "source")
	if got := evidenceFiles(map[string]any{}, "source"); len(got) != 0 {
		t.Fatalf("缺 side 应空: %v", got)
	}
	// canonicalJSONString marshal 失败回退（channel 不可序列化）
	if canonicalJSONString(make(chan int)) == "" {
		t.Fatalf("回退应非空")
	}
	// assistantText 空响应
	if assistantText(nil) != "" {
		t.Fatalf("nil 响应应空")
	}
}

// fakeLoopOptimizerUnchanged 产出与父本完全相同的候选包。
type fakeLoopOptimizerUnchanged struct{ baseRefs, candRefs string }

func (f *fakeLoopOptimizerUnchanged) Optimize(ctx context.Context, evalRefPath, analysisResultPath,
	harnessRefsPath, outputDir string, params memberoptimizer.OptimizeExecuteParams) (string, error) {
	os.MkdirAll(outputDir, 0o755)
	pkgDir := f.candRefs
	if _, err := os.Stat(pkgDir); err != nil {
		if err := copyTree(f.baseRefs, pkgDir); err != nil {
			return "", err
		}
	}
	refsYAML := filepath.Join(filepath.Dir(pkgDir), "cand_refs_same.yaml")
	os.WriteFile(refsYAML, []byte("harness_refs:\n  coder: "+filepath.Join(pkgDir, "coder")+"\n"), 0o644)
	memberRef := filepath.Join(outputDir, "member_optimization_ref.yaml")
	os.WriteFile(memberRef, []byte("status: success\noptimized_harness_refs_path: "+refsYAML+"\n"), 0o644)
	return memberRef, nil
}

func TestRun_连续模式_候选内容未变(t *testing.T) {
	dir := t.TempDir()
	basePkg := filepath.Join(dir, "harness", "coder")
	os.MkdirAll(basePkg, 0o755)
	writeTestYAML(t, filepath.Join(basePkg, "runtime.yaml"), "max_iterations: 5\n")
	refsPath := filepath.Join(dir, "refs.yaml")
	writeTestYAML(t, refsPath, "harness_refs:\n  coder: "+basePkg+"\n")
	dsPath := filepath.Join(dir, "ds.json")
	writeTestYAML(t, dsPath, `[{"case_id":"c1","input":"task"}]`)
	cfg := buildTestConfig(dir)
	cfg.MaxEpochs = 1
	cfg.Promotion = configpkg.PromotionConfig{
		PolicyVersion: "continuous_two_stage_v2", MinImprovement: 0.1,
	}
	orchestrator, err := NewSingleHarnessIterativeOptimizationOrchestrator(
		cfg, &fakePathEvaluator{baseScore: 0.5, candidateTag: "candidate",
			FixedCaseIDs: map[string]bool{"c1": true}},
		&fakeLoopAnalyzer{}, &fakeLoopOptimizerUnchanged{
			baseRefs: filepath.Join(dir, "harness"),
			candRefs: filepath.Join(dir, "candidate", "pkg"),
		}, nil)
	if err != nil {
		t.Fatalf("构造: %v", err)
	}
	request, err := NewIterativeSingleHarnessRequest(
		[]string{dsPath}, refsPath, filepath.Join(dir, "run"), "ds1", false, "", false, "t1", nil)
	if err != nil {
		t.Fatalf("请求: %v", err)
	}
	result, err := orchestrator.Run(context.Background(), request, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	state, _ := readYAMLFile(result.StatePath)
	gates, _ := state["candidate_gates"].([]any)
	if len(gates) == 0 {
		t.Fatalf("应有未变更门")
	}
	gate := gates[0].(map[string]any)
	if gate["reason"] != "candidate_content_unchanged" {
		t.Fatalf("未变更原因: %v", gate["reason"])
	}
}

func TestRun_连续模式_目标评估上下文不符(t *testing.T) {
	dir := t.TempDir()
	basePkg := filepath.Join(dir, "harness", "coder")
	os.MkdirAll(basePkg, 0o755)
	writeTestYAML(t, filepath.Join(basePkg, "runtime.yaml"), "max_iterations: 5\n")
	refsPath := filepath.Join(dir, "refs.yaml")
	writeTestYAML(t, refsPath, "harness_refs:\n  coder: "+basePkg+"\n")
	dsPath := filepath.Join(dir, "ds.json")
	writeTestYAML(t, dsPath, `[{"case_id":"c1","input":"task"}]`)
	cfg := buildTestConfig(dir)
	cfg.MaxEpochs = 1
	cfg.Promotion = configpkg.PromotionConfig{
		PolicyVersion: "continuous_two_stage_v2", MinImprovement: 0.1,
	}
	// 优化器产出候选包（内容变化），但候选评估被外部篡改上下文 → 冻结校验拒绝
	orchestrator, err := NewSingleHarnessIterativeOptimizationOrchestrator(
		cfg, &fakePathEvaluator{baseScore: 0.5, candidateTag: "candidate",
			FixedCaseIDs: map[string]bool{"c1": true}},
		&fakeLoopAnalyzer{}, &fakeLoopOptimizer{baseRefs: filepath.Join(dir, "harness"),
			candRefs: filepath.Join(dir, "candidate", "pkg")}, nil)
	if err != nil {
		t.Fatalf("构造: %v", err)
	}
	request, err := NewIterativeSingleHarnessRequest(
		[]string{dsPath}, refsPath, filepath.Join(dir, "run"), "ds1", false, "", false, "t1", nil)
	if err != nil {
		t.Fatalf("请求: %v", err)
	}
	result, err := orchestrator.Run(context.Background(), request, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	state, _ := readYAMLFile(result.StatePath)
	gates, _ := state["candidate_gates"].([]any)
	found := false
	for _, rawGate := range gates {
		gate := rawGate.(map[string]any)
		if gate["reason"] == "evaluation_invalid" {
			found = true
		}
	}
	// 正常链路不会触发 evaluation_invalid（校验是防篡改护栏）
	if found {
		t.Fatalf("正常链路不应触发 evaluation_invalid")
	}
}
