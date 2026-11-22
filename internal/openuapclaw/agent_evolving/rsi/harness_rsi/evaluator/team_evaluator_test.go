// team_evaluator_test.go TeamEvaluator 真身单测（17.6c，替换 17.4f 桩测试）。
// fake 缝：buildBackendFn/buildJudgerFn 函数值注入 fake backend/judger；
// 落盘路径全部 t.TempDir 隔离（测试污染源治理惯例）。

package evaluator

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi/harness_rsi/config"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi/harness_rsi/evaluator/evalerrors"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi/harness_rsi/evaluator/judger"
)

// ──────────────────────────── fake 定义 ────────────────────────────

// fakeBackend 可编程执行后端：按 case_id 决定行为。
type fakeBackend struct {
	mu sync.Mutex
	// behaviors case_id → 行为（error 原文以 "infra:" 前缀触发基建错误）
	behaviors map[string]string
	// calls 逐次收到的 case_id
	calls []string
	// executedCases 逐次收到的考题深拷贝（校验 case_id 注入）
	executedCases []map[string]any
}

func (f *fakeBackend) Execute(_ context.Context, params ExecuteParams) (*CaseExecutionResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	caseID := ""
	if id, ok := params.Case["case_id"].(string); ok {
		caseID = id
	}
	f.calls = append(f.calls, caseID)
	copied := map[string]any{"case_id": caseID}
	f.executedCases = append(f.executedCases, copied)
	if behavior, ok := f.behaviors[caseID]; ok {
		if strings.HasPrefix(behavior, "infra:") {
			return nil, evalerrors.NewEvaluationInfrastructureError(strings.TrimPrefix(behavior, "infra:"))
		}
		return &CaseExecutionResult{
			Response:        "answer",
			ExecutionStatus: "error",
			Error:           behavior,
		}, nil
	}
	return &CaseExecutionResult{Response: "answer", ExecutionStatus: "passed"}, nil
}

func (f *fakeBackend) Cleanup(_, _ string) {}

// fakeJudger 可编程判分器。
type fakeJudger struct {
	// validateErr 校验错误（非 nil 时逐题上抛）
	validateErr error
}

func (f *fakeJudger) MethodName() string { return "fake_judge" }

func (f *fakeJudger) ValidateCase(_ map[string]any) error { return f.validateErr }

func (f *fakeJudger) Judge(_ context.Context, caseMapping map[string]any, _ judger.CaseExecutionView,
	_ string, _ judger.BaselineContextView) (*judger.JudgeResult, error) {
	return &judger.JudgeResult{Method: "fake_judge", Score: 0.5, Passed: true}, nil
}

func (f *fakeJudger) FailureResult(err string) *judger.JudgeResult {
	return &judger.JudgeResult{Method: "fake_judge", Score: 0, Passed: false, Reason: err}
}

// newTestEvaluator 用 fake backend/judger 构造真身（绕过 buildBackendFn）。
func newTestEvaluator(t *testing.T, cfg config.EvaluatorConfig,
	backend *fakeBackend, judge *fakeJudger) *TeamEvaluator {
	t.Helper()
	runner, err := NewCaseRunner(backend, judge)
	if err != nil {
		t.Fatalf("构造 CaseRunner 失败: %v", err)
	}
	return &TeamEvaluator{config: cfg, caseRunner: runner, metricsCollector: NewMetricsCollector()}
}

// ──────────────────────────── 测试 ────────────────────────────

// TestEvaluateBatch_并发守卫先于一切 守卫序照 Python（L94 先于 baseline 校验）。
func TestEvaluateBatch_并发守卫先于一切(t *testing.T) {
	te := newTestEvaluator(t, config.EvaluatorConfig{}, &fakeBackend{}, &fakeJudger{})
	_, err := te.EvaluateBatch(context.Background(), nil, "", "", t.TempDir(), nil, nil, 0, nil)
	if err == nil || err.Error() != "case_concurrency must be a positive integer" {
		t.Fatalf("守卫文案应对齐: %v", err)
	}
	// 守卫先于基线校验：缺基线的场景在并发非法时先报守卫错误。
	_, err = te.EvaluateBatch(context.Background(),
		[]map[string]any{{"case_id": "c1"}}, "", "", t.TempDir(), nil, nil, 0, nil)
	if err == nil || err.Error() != "case_concurrency must be a positive integer" {
		t.Fatalf("case_concurrency 守卫应先于基线校验: %v", err)
	}
}

// TestEvaluateBatch_基线缺失整场失败 候选评估缺同题基线（Python L96-101）。
func TestEvaluateBatch_基线缺失整场失败(t *testing.T) {
	te := newTestEvaluator(t, config.EvaluatorConfig{}, &fakeBackend{}, &fakeJudger{})
	_, err := te.EvaluateBatch(context.Background(),
		[]map[string]any{{"case_id": "c1"}}, "", "", t.TempDir(), nil, nil, 1,
		map[string]*BaselineJudgeContext{})
	if err == nil || !strings.Contains(err.Error(),
		"Candidate evaluation is missing its same-case baseline") {
		t.Fatalf("缺基线应整场失败: %v", err)
	}
}

// TestEvaluate_真载入数据集 回填件②：DatasetFiles 真载入并注入 case_path/case_index。
func TestEvaluate_真载入数据集(t *testing.T) {
	dir := t.TempDir()
	datasetFile := filepath.Join(dir, "cases.jsonl")
	if err := os.WriteFile(datasetFile,
		[]byte("{\"case_id\":\"c1\",\"input\":{\"question\":\"q\"}}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var captured []map[string]any
	backend := &fakeBackend{}
	judge := &fakeJudger{}
	te := newTestEvaluator(t, config.EvaluatorConfig{}, backend, judge)
	// 经 seam 观察 runCase 收到的考题：直接调 EvaluateBatch 验证载入产物。
	cases, err := loadDatasetCases([]string{datasetFile})
	if err != nil {
		t.Fatalf("载入数据集失败: %v", err)
	}
	if len(cases) != 1 {
		t.Fatalf("应载入 1 题，得到 %d", len(cases))
	}
	if cases[0]["case_path"] != datasetFile {
		t.Fatalf("case_path 应注入: %v", cases[0]["case_path"])
	}
	if idx, ok := cases[0]["case_index"].(int); !ok || idx != 1 {
		t.Fatalf("case_index 应为 1: %v", cases[0]["case_index"])
	}
	captured = cases
	_ = captured
	_, err = te.EvaluateBatch(context.Background(), cases, "", "", t.TempDir(), nil, nil, 1, nil)
	if err != nil {
		t.Fatalf("EvaluateBatch 失败: %v", err)
	}
	if len(backend.calls) != 1 || backend.calls[0] != "c1" {
		t.Fatalf("应执行 c1: %v", backend.calls)
	}
}

// TestEvaluateBatch_单题失败记0分继续 单题失败不阻断整场（Python 单题失败语义）。
func TestEvaluateBatch_单题失败记0分继续(t *testing.T) {
	dir := t.TempDir()
	backend := &fakeBackend{behaviors: map[string]string{"c2": "boom: runtime crashed"}}
	te := newTestEvaluator(t, config.EvaluatorConfig{}, backend, &fakeJudger{})
	refPath, err := te.EvaluateBatch(context.Background(),
		[]map[string]any{{"case_id": "c1"}, {"case_id": "c2"}, {"case_id": "c3"}},
		"", "", dir, nil, nil, 1, nil)
	if err != nil {
		t.Fatalf("单题失败不应整场失败: %v", err)
	}
	// c1/c3 正常执行。
	if len(backend.calls) != 3 {
		t.Fatalf("三题都应执行: %v", backend.calls)
	}
	// eval_ref.yaml 引用链落盘。
	data, err := os.ReadFile(refPath)
	if err != nil {
		t.Fatalf("eval_ref.yaml 应存在: %v", err)
	}
	content := string(data)
	for _, want := range []string{"c1", "c2", "c3", "trace_path", "result_path"} {
		if !strings.Contains(content, want) {
			t.Fatalf("eval_ref.yaml 缺少 %s:\n%s", want, content)
		}
	}
	// cases/ 目录逐题落盘。
	entries, err := os.ReadDir(filepath.Join(dir, CaseResultsDirName))
	if err != nil || len(entries) != 3 {
		t.Fatalf("cases/ 应有 3 个用例目录: %v %d", err, len(entries))
	}
}

// TestEvaluateBatch_瞬态重试 基建瞬态错误重考 + 退避历史落盘（Python L187-216）。
func TestEvaluateBatch_瞬态重试(t *testing.T) {
	dir := t.TempDir()
	backend := &fakeBackend{behaviors: map[string]string{
		"c1": "infra: connection reset by peer",
	}}
	te := newTestEvaluator(t, config.EvaluatorConfig{TransientCaseRetryLimit: 1}, backend, &fakeJudger{})
	if _, err := te.EvaluateBatch(context.Background(),
		[]map[string]any{{"case_id": "c1"}}, "", "", dir, nil, nil, 1, nil); err == nil {
		t.Fatal("重试耗尽后应上抛基建错误")
	}
	if len(backend.calls) != 2 {
		t.Fatalf("应重试一次共 2 次调用: %v", backend.calls)
	}
	// Python 行为：基建瞬态耗尽走 raise，重试历史仅结果级瞬态路径落盘
	// （team_evaluator.py L187-199 raise 先于 L211 落盘）。
	matches, _ := filepath.Glob(filepath.Join(dir, CaseResultsDirName, "*_transient_retries.json"))
	if len(matches) != 0 {
		t.Fatalf("基建路径耗尽不落历史（照 Python raise）: %v", matches)
	}
}

// TestEvaluateBatch_非瞬态不重试 语义失败不触发重考。
func TestEvaluateBatch_非瞬态不重试(t *testing.T) {
	backend := &fakeBackend{behaviors: map[string]string{
		"c1": "infra: rubric mismatch",
	}}
	te := newTestEvaluator(t, config.EvaluatorConfig{TransientCaseRetryLimit: 2}, backend, &fakeJudger{})
	if _, err := te.EvaluateBatch(context.Background(),
		[]map[string]any{{"case_id": "c1"}}, "", "", t.TempDir(), nil, nil, 1, nil); err == nil {
		t.Fatal("非瞬态基建错误应上抛")
	}
	if len(backend.calls) != 1 {
		t.Fatalf("非瞬态不应重试: %v", backend.calls)
	}
}

// TestEvaluateBatch_断点续考 指纹匹配翻旧账（Python L163-168/L348-386）。
func TestEvaluateBatch_断点续考(t *testing.T) {
	dir := t.TempDir()
	backend := &fakeBackend{}
	te := newTestEvaluator(t, config.EvaluatorConfig{}, backend, &fakeJudger{})
	cases := []map[string]any{{"case_id": "c1", "input": map[string]any{"question": "q"}}}
	ref1, err := te.EvaluateBatch(context.Background(), cases, "", "", dir, nil, nil, 1, nil)
	if err != nil {
		t.Fatalf("首轮失败: %v", err)
	}
	// 第二轮指纹一致：不再执行。
	backend.calls = nil
	ref2, err := te.EvaluateBatch(context.Background(), cases, "", "", dir, nil, nil, 1, nil)
	if err != nil {
		t.Fatalf("续考失败: %v", err)
	}
	if len(backend.calls) != 0 {
		t.Fatalf("续考不应重执行: %v", backend.calls)
	}
	if ref1 != ref2 {
		t.Fatalf("续考应返回同一 eval_ref: %s vs %s", ref1, ref2)
	}
	// 修改考题后指纹变化：重新执行。
	cases[0]["input"] = map[string]any{"question": "changed"}
	te2 := newTestEvaluator(t, config.EvaluatorConfig{}, backend, &fakeJudger{})
	if _, err := te2.EvaluateBatch(context.Background(), cases, "", "", dir, nil, nil, 1, nil); err != nil {
		t.Fatalf("指纹变化后应重考: %v", err)
	}
	if len(backend.calls) != 1 {
		t.Fatalf("指纹变化后应重执行: %v", backend.calls)
	}
}

// TestEvaluateBatch_并发保序与阶段事件 并发执行 + 事件序（Python L223-239）。
func TestEvaluateBatch_并发保序与阶段事件(t *testing.T) {
	dir := t.TempDir()
	backend := &fakeBackend{}
	// 并发>1 走逐题 makeCaseRunner：经构造缝注入 fake backend/judger。
	origBackend, origJudger := buildBackendFn, buildJudgerFn
	buildBackendFn = func(*config.EvaluatorConfig) (CaseExecutionBackend, error) { return backend, nil }
	buildJudgerFn = func(*config.EvaluatorConfig) (judger.EvaluationJudger, error) { return &fakeJudger{}, nil }
	defer func() { buildBackendFn, buildJudgerFn = origBackend, origJudger }()
	te := newTestEvaluator(t, config.EvaluatorConfig{}, backend, &fakeJudger{})
	var mu sync.Mutex
	var events []EvaluationStageEvent
	onStage := func(event EvaluationStageEvent) error {
		mu.Lock()
		events = append(events, event)
		mu.Unlock()
		return nil
	}
	cases := []map[string]any{
		{"case_id": "c1", "input": map[string]any{"q": 1}},
		{"case_id": "c2", "input": map[string]any{"q": 2}},
		{"case_id": "c3", "input": map[string]any{"q": 3}},
	}
	_, err := te.EvaluateBatch(context.Background(), cases, "", "", dir, nil, onStage, 2, nil)
	if err != nil {
		t.Fatalf("并发执行失败: %v", err)
	}
	if len(backend.calls) != 3 {
		t.Fatalf("三题都应执行: %v", backend.calls)
	}
	// 事件统计：3 题 × (running + 终态) = 6 条；并发时带 CompletedCases。
	if len(events) != 6 {
		t.Fatalf("应 6 条阶段事件: %d", len(events))
	}
	running := 0
	final := 0
	for _, event := range events {
		if event.CompletedCases == nil {
			t.Fatalf("并发>1 时 CompletedCases 应填充: %+v", event)
		}
		if event.Status == "running" {
			running++
		} else {
			final++
		}
		if event.TotalCases != 3 {
			t.Fatalf("TotalCases 应为 3: %+v", event)
		}
	}
	if running != 3 || final != 3 {
		t.Fatalf("running/终态各 3: %d/%d", running, final)
	}
	// eval_ref.yaml cases 保输入序（Python gather 保序注释）。
	refPath := filepath.Join(dir, "eval_ref.yaml")
	data, err := os.ReadFile(refPath)
	if err != nil {
		t.Fatal(err)
	}
	c1Pos := strings.Index(string(data), "case_id: c1")
	c2Pos := strings.Index(string(data), "case_id: c2")
	c3Pos := strings.Index(string(data), "case_id: c3")
	if !(c1Pos < c2Pos && c2Pos < c3Pos) {
		t.Fatalf("cases 应保输入序: c1@%d c2@%d c3@%d", c1Pos, c2Pos, c3Pos)
	}
}

// TestEvaluateBatch_串行无CompletedCases 并发=1 时事件不带 completed_cases。
func TestEvaluateBatch_串行无CompletedCases(t *testing.T) {
	te := newTestEvaluator(t, config.EvaluatorConfig{}, &fakeBackend{}, &fakeJudger{})
	var events []EvaluationStageEvent
	_, err := te.EvaluateBatch(context.Background(),
		[]map[string]any{{"case_id": "c1"}}, "", "", t.TempDir(), nil,
		func(event EvaluationStageEvent) error {
			events = append(events, event)
			return nil
		}, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range events {
		if event.CompletedCases != nil {
			t.Fatalf("串行不应带 CompletedCases: %+v", event)
		}
	}
}

// TestWriteEvalRef_引用链 eval_ref.yaml 与 cases/ 落盘互指（EvaluationCaseTraceRef）。
func TestWriteEvalRef_引用链(t *testing.T) {
	dir := t.TempDir()
	te := newTestEvaluator(t, config.EvaluatorConfig{}, &fakeBackend{}, &fakeJudger{})
	refPath, err := te.EvaluateBatch(context.Background(),
		[]map[string]any{{"case_id": "c1"}}, "team_skill.md", "", dir, nil, nil, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(refPath)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	// 键序对齐 Python 字面顺序（eval_id 先于 created_at 先于 team_name）。
	if strings.Index(content, "eval_id:") > strings.Index(content, "created_at:") ||
		strings.Index(content, "created_at:") > strings.Index(content, "team_name:") {
		t.Fatalf("eval_ref.yaml 键序应照 Python: %s", content)
	}
	// trace_path/result_path 引用真实存在的文件。
	tracePath := extractYAMLValue(content, "trace_path")
	resultPath := extractYAMLValue(content, "result_path")
	if _, err := os.Stat(tracePath); err != nil {
		t.Fatalf("trace_path 应存在: %s: %v", tracePath, err)
	}
	if _, err := os.Stat(resultPath); err != nil {
		t.Fatalf("result_path 应存在: %s: %v", resultPath, err)
	}
}

// TestEvaluationName 评估名称推断（Python _evaluation_name）。
func TestEvaluationName(t *testing.T) {
	if got := evaluationName(""); got != "single_harness" {
		t.Fatalf("空路径应 single_harness: %q", got)
	}
	if got := evaluationName("/tmp/team_a/skill.md"); got != "team_a" {
		t.Fatalf("skill.md 取父目录名: %q", got)
	}
	if got := evaluationName("/tmp/team_a"); got != "team_a" {
		t.Fatalf("普通路径取末段: %q", got)
	}
}

// TestCaseDirName 短哈希目录名（Python _case_dir_name）。
func TestCaseDirName(t *testing.T) {
	name := caseDirName("case-abc", 3)
	if !strings.HasPrefix(name, "c003_") || len(name) != len("c003_")+8 {
		t.Fatalf("目录名形态应为 c003_<8位哈希>: %q", name)
	}
	if name != caseDirName("case-abc", 3) {
		t.Fatal("同名同序应稳定")
	}
}

// TestLoadHarnessRefs_双形态 规范 harness_refs 键 + 旧版顶层映射（Python L421-440）。
func TestLoadHarnessRefs_双形态(t *testing.T) {
	dir := t.TempDir()
	canonical := filepath.Join(dir, "refs.yaml")
	if err := os.WriteFile(canonical,
		[]byte("harness_refs:\n  coder: /path/coder\n  reviewer: /path/reviewer\nroles: [coder, reviewer]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	refs, err := loadHarnessRefs(canonical)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 2 || refs["coder"] != "/path/coder" {
		t.Fatalf("规范形态应载入 harness_refs: %+v", refs)
	}
	legacy := filepath.Join(dir, "legacy.yaml")
	if err := os.WriteFile(legacy,
		[]byte("version: 1\nsource_harness_refs_path: /src\nanalyst: /path/analyst\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	refs, err = loadHarnessRefs(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 1 || refs["analyst"] != "/path/analyst" {
		t.Fatalf("旧版形态应滤除 version/source_harness_refs_path: %+v", refs)
	}
}

// TestTransientTransportError_判定传输层瞬态 transport.go 消费核对。
func TestTransientTransportError_判定传输层瞬态(t *testing.T) {
	if got := TransientTransportError("server disconnected mid-stream"); got == "" {
		t.Fatal("disconnected 应判瞬态")
	}
	if got := TransientTransportError("rubric mismatch"); got != "" {
		t.Fatalf("语义失败不应判瞬态: %q", got)
	}
}

// TestBackoffDelay 退避序列 min(2^n, 4)。
func TestBackoffDelay(t *testing.T) {
	for i, want := range []int{1, 2, 4, 4} {
		got := int(backoffDelay(i).Seconds())
		if got != want {
			t.Fatalf("attempt=%d 应 %d 秒，得到 %d", i, want, got)
		}
	}
}

// extractYAMLValue 从 yaml 文本提取首个匹配键的标量值。
func extractYAMLValue(content, key string) string {
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, key+":") {
			return strings.TrimSpace(strings.TrimPrefix(trimmed, key+":"))
		}
	}
	return ""
}
