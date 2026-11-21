// case_runner.go 单元测试——监考编排。
// Python: case_runner.py 对应测试面；fake backend + fake judger 注入缝；
// 全部落盘路径走 t.TempDir（b438f934a 污源登记的本批隔离）。

package evaluator

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	harnessrsi "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi/harness_rsi"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi/harness_rsi/evaluator/judger"
	agentteams "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams"
)

// ──────────────────────────── 测试替身 ────────────────────────────

// fakeCaseBackend CaseExecutionBackend 假实现。
type fakeCaseBackend struct {
	result       *CaseExecutionResult
	err          error
	executeCalls int
	cleanupCalls int
	lastCtx      context.Context
	lastParams   ExecuteParams
}

func (f *fakeCaseBackend) Execute(ctx context.Context, params ExecuteParams) (*CaseExecutionResult, error) {
	f.executeCalls++
	f.lastCtx = ctx
	f.lastParams = params
	return f.result, f.err
}

func (f *fakeCaseBackend) Cleanup(teamName, sessionID string) { f.cleanupCalls++ }

// fakeRunnerJudger EvaluationJudger 假实现。
type fakeRunnerJudger struct {
	method       string
	result       *judger.JudgeResult
	err          error
	judgeCalls   int
	lastCase     map[string]any
	lastOutput   string
	lastBaseline judger.BaselineContextView
}

func (f *fakeRunnerJudger) MethodName() string { return f.method }

func (f *fakeRunnerJudger) ValidateCase(caseMapping map[string]any) error { return nil }

func (f *fakeRunnerJudger) Judge(ctx context.Context, caseMapping map[string]any,
	executionResult judger.CaseExecutionView, outputDir string, baselineContext judger.BaselineContextView) (*judger.JudgeResult, error) {
	f.judgeCalls++
	f.lastCase = caseMapping
	f.lastOutput = outputDir
	f.lastBaseline = baselineContext
	return f.result, f.err
}

func (f *fakeRunnerJudger) FailureResult(err string) *judger.JudgeResult {
	return &judger.JudgeResult{Method: f.method, Score: 0, Passed: false, Reason: err}
}

func passingJudgeResult() *judger.JudgeResult {
	return &judger.JudgeResult{Method: "exact_match", Score: 1, Passed: true, Reason: "ok",
		Metadata: map[string]any{"score_mode": "binary"}}
}

func failingJudgeResult() *judger.JudgeResult {
	return &judger.JudgeResult{Method: "exact_match", Score: 0, Passed: false, Reason: "mismatch",
		Metadata: map[string]any{"score_mode": "binary"}}
}

// ──────────────────────────── 构造与主链 ────────────────────────────

func TestNewCaseRunner_缺backend报错(t *testing.T) {
	if _, err := NewCaseRunner(nil, nil); err == nil {
		t.Fatal("缺 backend 必须报错（Python ValueError）")
	}
}

func TestExecute_主链六步(t *testing.T) {
	dir := t.TempDir()
	backend := &fakeCaseBackend{result: &CaseExecutionResult{
		Response: "done", ExecutionStatus: "passed", WorkspaceDir: "",
		Metadata: map[string]any{
			"command_log":       []any{map[string]any{"command": "ls", "exit_code": 0}},
			"workspace_changes": map[string]any{"added": []any{"out.txt"}, "modified": []any{}, "removed": []any{}},
		},
	}}
	jd := &fakeRunnerJudger{method: "exact_match", result: passingJudgeResult()}
	runner, err := NewCaseRunner(backend, jd)
	if err != nil {
		t.Fatalf("构造失败: %v", err)
	}
	caseDir := filepath.Join(dir, "cases", "fix_login")
	ref, err := runner.Execute(context.Background(), CaseRunnerParams{
		Case:      map[string]any{"case_id": "fix_login", "input": "task text"},
		OutputDir: caseDir,
	})
	if err != nil {
		t.Fatalf("execute 失败: %v", err)
	}
	// ①题目目录已建
	if _, err := os.Stat(caseDir); err != nil {
		t.Fatalf("case_dir 未创建: %v", err)
	}
	// ②答题（backend 收到 output_dir/session_id，且任务级 home 绑定生效）
	if backend.lastParams.OutputDir != caseDir || backend.lastParams.SessionID == "" {
		t.Fatalf("backend 参数不对: %+v", backend.lastParams)
	}
	if got := agentteams.GetHomeWithContext(backend.lastCtx); got != caseDir {
		t.Fatalf("任务级 home 未绑定到 case_dir: got %q want %q", got, caseDir)
	}
	// ③判分
	if jd.judgeCalls != 1 {
		t.Fatalf("judger 调用次数 = %d, want 1", jd.judgeCalls)
	}
	// ④归档与成绩单
	for _, name := range []string{"result.json", "trace.json",
		filepath.Join("tr", "trajectory_events.jsonl"),
		filepath.Join("judge", "normalized_trace.json")} {
		if _, err := os.Stat(filepath.Join(caseDir, name)); err != nil {
			t.Fatalf("归档件缺失 %s: %v", name, err)
		}
	}
	if ref.CaseID != "fix_login" || ref.Status != "passed" || ref.Score == nil || *ref.Score != 1 {
		t.Fatalf("trace ref 不符: %+v", ref)
	}
	// ⑤外层 ctx 不被污染
	if got := agentteams.GetHomeWithContext(context.Background()); got == caseDir {
		t.Fatal("外层 ctx 被任务级 home 污染")
	}
	// ⑥收尾清理：legacy workspace 与 .agent_teams 已删
	if _, err := os.Stat(filepath.Join(caseDir, "workspace")); !os.IsNotExist(err) {
		t.Fatal("legacy workspace 未清理")
	}
	if backend.cleanupCalls != 1 {
		t.Fatalf("cleanup 调用次数 = %d, want 1", backend.cleanupCalls)
	}
}

func TestExecute_错误路径写错误件(t *testing.T) {
	dir := t.TempDir()
	backend := &fakeCaseBackend{err: context.DeadlineExceeded}
	runner, _ := NewCaseRunner(backend, nil)
	ref, err := runner.Execute(context.Background(), CaseRunnerParams{
		Case: map[string]any{"case_id": "boom"}, OutputDir: filepath.Join(dir, "case"),
	})
	if err != nil {
		t.Fatalf("非基建错误应降级为错误件而非上抛: %v", err)
	}
	if ref == nil || ref.Status != "error" {
		t.Fatalf("错误 ref 不符: %+v", ref)
	}
	var payload map[string]any
	b, _ := os.ReadFile(filepath.Join(dir, "case", "result.json"))
	if json.Unmarshal(b, &payload) != nil || payload["status"] != "error" {
		t.Fatalf("result.json 错误件不符: %s", b)
	}
}

func TestExecute_基建错误上抛(t *testing.T) {
	dir := t.TempDir()
	backend := &fakeCaseBackend{err: NewEvaluationInfrastructureError("no judger")}
	runner, _ := NewCaseRunner(backend, nil)
	ref, err := runner.Execute(context.Background(), CaseRunnerParams{
		Case: map[string]any{"case_id": "infra"}, OutputDir: filepath.Join(dir, "case"),
	})
	if err == nil {
		t.Fatal("基建错误必须上抛")
	}
	if ref != nil {
		t.Fatalf("基建错误不应返回 ref: %+v", ref)
	}
	b, rerr := os.ReadFile(filepath.Join(dir, "case", "evaluation_error.json"))
	if rerr != nil {
		t.Fatalf("evaluation_error.json 未落盘: %v", rerr)
	}
	if !strings.Contains(string(b), "error") {
		t.Fatalf("evaluation_error.json 内容不符: %s", b)
	}
}

// ──────────────────────────── 判分 _judge ────────────────────────────

func TestJudge_三分支(t *testing.T) {
	backendResult := &CaseExecutionResult{ExecutionStatus: "passed",
		JudgeResult: &judger.JudgeResult{Method: "exact_match", Score: 1, Passed: true}}
	runner := &CaseRunner{backend: &fakeCaseBackend{result: backendResult}}
	// 分支1：backend 自带判分（无 baseline）
	res, err := runner.judge(context.Background(), map[string]any{}, backendResult, "dir", nil)
	if err != nil {
		t.Fatalf("分支1 judge 失败: %v", err)
	}
	if res == nil || !res.Passed {
		t.Fatalf("分支1 应复用 backend JudgeResult: %+v", res)
	}
	// 分支1 变体：backend 判分 + baseline → 基建错误
	if _, err2 := runner.judge(context.Background(), map[string]any{}, backendResult, "dir", fakeBaseline()); err2 == nil {
		t.Fatal("backend 判分不能绕过 baseline LLM judging")
	}
	// 分支2：judger 注入
	jd := &fakeRunnerJudger{method: "exact_match", result: passingJudgeResult()}
	runner2 := &CaseRunner{backend: &fakeCaseBackend{result: &CaseExecutionResult{ExecutionStatus: "passed"}}, judger: jd}
	if _, err := runner2.judge(context.Background(), map[string]any{}, &CaseExecutionResult{ExecutionStatus: "passed"}, "dir", nil); err != nil {
		t.Fatalf("分支2 judge 失败: %v", err)
	}
	// 分支2 变体：baseline 但非 llm_as_judge → 基建错误
	if _, err := runner2.judge(context.Background(), map[string]any{}, &CaseExecutionResult{ExecutionStatus: "passed"}, "dir", fakeBaseline()); err == nil {
		t.Fatal("baseline 对比必须用 llm_as_judge")
	}
	// 分支3：无 judger 无 backend 判分 → 基建错误
	runner3 := &CaseRunner{backend: &fakeCaseBackend{result: &CaseExecutionResult{ExecutionStatus: "passed"}}}
	if _, err := runner3.judge(context.Background(), map[string]any{}, &CaseExecutionResult{ExecutionStatus: "passed"}, "dir", nil); err == nil {
		t.Fatal("无判分来源必须报基建错误")
	}
	// 兜底：仅完成态评分无效
	only := &CaseRunner{backend: &fakeCaseBackend{result: &CaseExecutionResult{ExecutionStatus: "passed",
		JudgeResult: &judger.JudgeResult{Method: "completion", Score: 1, Passed: true}}}}
	if _, err := only.judge(context.Background(), map[string]any{},
		&CaseExecutionResult{ExecutionStatus: "passed",
			JudgeResult: &judger.JudgeResult{Method: "none", Score: 1, Passed: true}}, "dir", nil); err == nil {
		t.Fatal("仅完成态评分必须拒绝")
	}
}

// fakeBaselineView 非空基线视图假实现（ BaselineContextView 六方法）。
type fakeBaselineView struct{}

func (f *fakeBaselineView) Validate(caseMapping map[string]any) (map[string]any, error) {
	return map[string]any{}, nil
}
func (f *fakeBaselineView) Fingerprint() string        { return "fp" }
func (f *fakeBaselineView) CaseIDView() string         { return "case" }
func (f *fakeBaselineView) EvalRefPathView() string    { return "/tmp/eval_ref.yaml" }
func (f *fakeBaselineView) ResultPathView() string     { return "/tmp/result.json" }
func (f *fakeBaselineView) EvidenceDigestView() string { return "digest" }

func fakeBaseline() judger.BaselineContextView { return &fakeBaselineView{} }

// ──────────────────────────── 状态映射 ────────────────────────────

func TestCaseResultStatus_分支(t *testing.T) {
	passed := passingJudgeResult()
	failed := failingJudgeResult()
	continuous := &judger.JudgeResult{Method: "m", Passed: false, Metadata: map[string]any{"score_mode": "continuous"}}
	if got := caseResultStatus("passed", passed); got != "passed" {
		t.Fatalf("passed 执行+通过判分 = %q", got)
	}
	if got := caseResultStatus("passed", failed); got != "failed" {
		t.Fatalf("passed 执行+未过判分 = %q", got)
	}
	if got := caseResultStatus("failed", continuous); got != "failed" {
		t.Fatalf("失败执行+连续评分 = %q", got)
	}
	if got := caseResultStatus("failed", failed); got != "error" {
		t.Fatalf("失败执行+二元未过 = %q（执行未完成即 error）", got)
	}
}

func TestPublishedExecutionStatus_分支(t *testing.T) {
	continuous := &judger.JudgeResult{Method: "m", Passed: false, Metadata: map[string]any{"score_mode": "continuous"}}
	failed := failingJudgeResult()
	if got := publishedExecutionStatus("failed", "failed", continuous); got != "failed" {
		t.Fatalf("graded harness failure 应发布 failed: %q", got)
	}
	if got := publishedExecutionStatus("failed", "failed", failed); got != "failed" {
		t.Fatalf("原执行状态应保持: %q", got)
	}
	if got := publishedExecutionStatus("passed", "passed", passingJudgeResult()); got != "passed" {
		t.Fatalf("passed 应保持: %q", got)
	}
}

// ──────────────────────────── 预期产物解析 ────────────────────────────

func TestExpectedArtifactFiles_三形态(t *testing.T) {
	// 形态1：reference mapping 五键
	got := expectedArtifactFiles(map[string]any{
		"reference": map[string]any{"expected_artifacts": []any{"a.txt", "sub/b.md"}}})
	if len(got) != 2 || got[0] != "a.txt" || got[1] != "b.md" {
		t.Fatalf("reference 键解析不符: %v", got)
	}
	// 形态1 变体：artifact_contract
	got = expectedArtifactFiles(map[string]any{
		"reference": map[string]any{"artifact_contract": map[string]any{"final_files": []any{"x.json"}}}})
	if len(got) != 1 || got[0] != "x.json" {
		t.Fatalf("artifact_contract 解析不符: %v", got)
	}
	// 形态3：canonical 兜底
	got = expectedArtifactFiles(map[string]any{
		"case_id": "c", "notes": "deliver index.html and styles.css plus content_brief.md"})
	if len(got) != 3 {
		t.Fatalf("canonical 兜底不符: %v", got)
	}
	// 空形态
	if got := expectedArtifactFiles(map[string]any{"case_id": "c"}); len(got) != 0 {
		t.Fatalf("空形态应返回空: %v", got)
	}
}

// ──────────────────────────── 轨迹事件 ────────────────────────────

func TestBuildTrajectoryEvents_有界序列(t *testing.T) {
	bt := map[string]any{
		"command_log":       []any{map[string]any{"command": "make test", "exit_code": 2, "stderr_excerpt": "boom"}},
		"workspace_changes": map[string]any{"added": []any{"a.go"}, "modified": []any{}, "removed": []any{}},
		"artifact_refs":     map[string]any{"harvested": []any{"a.go"}, "missing": []any{}},
	}
	events := buildTrajectoryEvents("response text", bt, passingJudgeResult(), "passed", "")
	types := []string{}
	for _, e := range events {
		types = append(types, e["event_type"].(string))
	}
	want := []string{"tool_call", "workspace_change", "agent_response", "artifact_harvest", "verifier_result", "case_outcome"}
	if strings.Join(types, ",") != strings.Join(want, ",") {
		t.Fatalf("事件序不符: %v", types)
	}
	for i, e := range events {
		if e["event_index"] != i+1 {
			t.Fatalf("event_index 编号不符: %v", e["event_index"])
		}
	}
}

func TestBuildPreJudgeTrajectoryEvents_序(t *testing.T) {
	bt := map[string]any{
		"command_log":       []any{},
		"workspace_changes": map[string]any{"added": []any{}, "modified": []any{}, "removed": []any{}},
		"artifact_refs":     map[string]any{"harvested": []any{}, "missing": []any{}},
	}
	events := buildPreJudgeTrajectoryEvents("resp", bt, "error", "kaboom")
	if len(events) != 2 {
		t.Fatalf("pre-judge 事件数 = %d, want 2（agent_response+case_outcome）", len(events))
	}
	last := events[len(events)-1]
	if last["event_type"] != "case_outcome" {
		t.Fatalf("末事件应为 case_outcome: %v", last["event_type"])
	}
}

func TestFailureSignatures_三签名(t *testing.T) {
	toolFail := []map[string]any{{"event_type": "tool_call", "data": map[string]any{"exit_code": 1}}}
	swebenchJudge := &judger.JudgeResult{Method: "swebench_official", Passed: false}
	got := failureSignatures(append(toolFail,
		map[string]any{"event_type": "workspace_change", "data": map[string]any{}}), swebenchJudge)
	if strings.Join(got, ",") != "tool_execution_failure,patch_quality_gap" {
		t.Fatalf("签名不符: %v", got)
	}
	got = failureSignatures(toolFail, swebenchJudge)
	if strings.Join(got, ",") != "tool_execution_failure,workspace_edit_gap" {
		t.Fatalf("无改动签名不符: %v", got)
	}
	if got := failureSignatures(toolFail, passingJudgeResult()); len(got) != 0 {
		t.Fatalf("通过判分应无签名: %v", got)
	}
}

// ──────────────────────────── 规范化 trace ────────────────────────────

func TestNormalizedTraceFromEvents_契约(t *testing.T) {
	events := []map[string]any{
		{"event_type": "tool_call", "event_index": 1, "summary": "s",
			"data": map[string]any{"command": "ls", "exit_code": 0, "stdout_excerpt": "f.go"}},
		{"event_type": "verifier_result", "event_index": 2, "summary": "v",
			"data": map[string]any{"method": "exact_match", "passed": false, "reason": "bad"}},
		{"event_type": "agent_response", "event_index": 3, "summary": "hi"},
	}
	trace := normalizedTraceFromEvents("c1", events, failingJudgeResult(), "m1", "coder", nil,
		map[string]any{"status": "complete"})
	if trace["case_id"] != "c1" {
		t.Fatalf("case_id 不符: %v", trace["case_id"])
	}
	traces := trace["traces"].([]map[string]any)
	if len(traces) != 1 || traces[0]["trace_id"] != "c1__m1__case" {
		t.Fatalf("case 轨迹不符: %v", traces)
	}
	msgs := traces[0]["messages"].([]map[string]any)
	if len(msgs) != 3 {
		t.Fatalf("消息数 = %d, want 3", len(msgs))
	}
	if _, ok := msgs[0]["tool_calls"]; !ok {
		t.Fatal("tool_call 事件应产出 tool_calls")
	}
	if msgs[0]["role"] != "assistant" || msgs[0]["message_index"] != 0 {
		t.Fatalf("消息头不符: %v", msgs[0])
	}
}

func TestNormalizedRoleTraces_降采样与空轨迹(t *testing.T) {
	dir := t.TempDir()
	// 大文件：> 2MB 触发尾部截取
	big := filepath.Join(dir, "big.jsonl")
	f, _ := os.Create(big)
	f.WriteString(`{"steps":[]}` + strings.Repeat("x", 2_100_000) + "\n")
	f.Close()
	// 空步轨迹文件
	small := filepath.Join(dir, "empty_role.jsonl")
	os.WriteFile(small, []byte(`{"steps":[]}`+"\n"), 0o644)
	traces := normalizedRoleTracesFromTrajectoryDir("c1", dir)
	if len(traces) != 2 {
		t.Fatalf("轨迹数 = %d, want 2", len(traces))
	}
	for _, tr := range traces {
		msgs := tr["messages"].([]map[string]any)
		if len(msgs) == 0 {
			t.Fatalf("%s 无消息", tr["trace_id"])
		}
		if tr["projection_complete"] != false {
			t.Fatalf("%s 应标记投影不完整", tr["trace_id"])
		}
	}
}

// ──────────────────────────── 收卷与清理 ────────────────────────────

func TestHarvestArtifacts_收卷与降级查找(t *testing.T) {
	dir := t.TempDir()
	ws := filepath.Join(dir, "ws")
	os.MkdirAll(filepath.Join(ws, "artifacts", "nested"), 0o755)
	os.WriteFile(filepath.Join(ws, "artifacts", "nested", "report.md"), []byte("data"), 0o644)
	ref := harvestArtifacts(ws, filepath.Join(dir, "artifacts"), []string{"report.md"})
	if len(ref["harvested"]) != 1 || ref["harvested"][0] != "report.md" {
		t.Fatalf("收卷结果不符: %v", ref)
	}
	if _, err := os.Stat(filepath.Join(dir, "artifacts", "report.md")); err != nil {
		t.Fatalf("嵌套产物未提升到根: %v", err)
	}
	missing := harvestArtifacts(ws, filepath.Join(dir, "artifacts2"), []string{"nope.txt"})
	if len(missing["missing"]) != 1 || missing["missing"][0] != "nope.txt" {
		t.Fatalf("缺失登记不符: %v", missing)
	}
}

func TestHarvestChangedWorkspaceFiles_越界与过滤(t *testing.T) {
	dir := t.TempDir()
	ws := filepath.Join(dir, "ws")
	os.MkdirAll(ws, 0o755)
	os.WriteFile(filepath.Join(ws, "out.txt"), []byte("v"), 0o644)
	os.WriteFile(filepath.Join(ws, "junk.log"), []byte("v"), 0o644)
	ref := harvestChangedWorkspaceFiles(ws, filepath.Join(dir, "artifacts"),
		map[string]any{"added": []any{"out.txt", "junk.log", "../escape.txt"}}, nil)
	found := false
	for _, h := range ref["harvested"] {
		if h == "out.txt" {
			found = true
		}
	}
	if !found {
		t.Fatalf("out.txt 未收卷: %v", ref)
	}
	if _, err := os.Stat(filepath.Join(dir, "artifacts", "junk.log")); !os.IsNotExist(err) {
		t.Fatal(".log 应被过滤")
	}
	if _, err := os.Stat(filepath.Join(dir, "artifacts", "escape.txt")); !os.IsNotExist(err) {
		t.Fatal("越界路径不得写出")
	}
}

func TestCleanupScratch_分支(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".agent_teams"), 0o755)
	os.MkdirAll(filepath.Join(dir, "workspace"), 0o755)
	os.WriteFile(filepath.Join(dir, "team.db"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(dir, "team.db-wal"), []byte("x"), 0o644)
	cleanupScratch(dir, dir)
	if _, err := os.Stat(filepath.Join(dir, ".agent_teams")); !os.IsNotExist(err) {
		t.Fatal(".agent_teams 未清理")
	}
	if _, err := os.Stat(filepath.Join(dir, "workspace")); !os.IsNotExist(err) {
		t.Fatal("legacy workspace 未清理")
	}
	if _, err := os.Stat(filepath.Join(dir, "team.db")); !os.IsNotExist(err) {
		t.Fatal("team.db 未清理")
	}
	// 独立 runtime home：home 本身删除，case_dir 仅清 .agent_teams
	home := filepath.Join(dir, "home2")
	os.MkdirAll(filepath.Join(home, ".agent_teams"), 0o755)
	os.MkdirAll(filepath.Join(dir, ".agent_teams"), 0o755)
	cleanupScratch(dir, home)
	if _, err := os.Stat(home); !os.IsNotExist(err) {
		t.Fatal("独立 runtime home 未删除")
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatal("case_dir 本身不得删除")
	}
}

func TestRuntimeHomeDir_平台分支(t *testing.T) {
	dir := t.TempDir()
	got := runtimeHomeDir(dir, "s1")
	if runtime.GOOS == "windows" {
		if !strings.Contains(got, "ach_team_runtime") {
			t.Fatalf("Windows 应走短路径: %q", got)
		}
	} else if got != dir {
		t.Fatalf("非 Windows 应等于 case_dir: %q", got)
	}
}

func TestSkipHarvestPath_过滤表(t *testing.T) {
	skip := []string{".git/config", ".agent_teams/x", "node_modules/a", "sub/venv/b", "x.pyc", "y.log", ""}
	keep := []string{"src/main.go", "out.txt"}
	for _, p := range skip {
		if !skipHarvestPath(p) {
			t.Fatalf("%q 应跳过", p)
		}
	}
	for _, p := range keep {
		if skipHarvestPath(p) {
			t.Fatalf("%q 不应跳过", p)
		}
	}
}

var _ = harnessrsi.EvaluationCaseTraceRef{} // 保持 schema 依赖可见
