package memberoptimizer

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	harnessrsi "github.com/uapclaw/uapclaw-go/internal/evolving/rsi/harness_rsi"
)

func writeOptTestHarness(t *testing.T, root, role string) string {
	t.Helper()
	dir := filepath.Join(root, role)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "harness.yaml"), []byte("name: "+role+"\n"), 0o644))
	return dir
}

func TestMemberOptimizer_Optimize_Freeze跳过(t *testing.T) {
	m := NewMemberOptimizer(&MemberOptimizerConfig{Freeze: true, AllowEmptyHarnessRefsNoop: true}, nil, nil, nil, nil, nil, nil)
	out := t.TempDir()
	refPath, err := m.Optimize(context.Background(), "e.yaml", "a.yaml", "h", out, OptimizeExecuteParams{})
	require.NoError(t, err)
	require.Contains(t, refPath, "member_optimization_ref.yaml")
	data, _ := os.ReadFile(refPath)
	var payload map[string]any
	require.NoError(t, yaml.Unmarshal(data, &payload))
	require.Equal(t, "skipped", payload["status"])
}

func TestMemberOptimizer_Optimize_无问题Noop(t *testing.T) {
	dir := t.TempDir()
	harness := writeOptTestHarness(t, dir, "solver")
	_ = harness
	modelRef := writeOptTestModel(t, dir)
	// analysis 无 issues
	analysisPath := filepath.Join(dir, "analysis.yaml")
	os.WriteFile(analysisPath, []byte("complete: true\nissues: []\n"), 0o644)
	evalPath := filepath.Join(dir, "eval.yaml")
	os.WriteFile(evalPath, []byte("eval_id: e1\ncases: []\n"), 0o644)
	m := NewMemberOptimizer(&MemberOptimizerConfig{AllowEmptyHarnessRefsNoop: true, ModelConfigRef: modelRef}, nil, nil, nil, nil, nil, nil)
	refPath, err := m.Optimize(context.Background(), evalPath, analysisPath, harness, filepath.Join(dir, "out"), OptimizeExecuteParams{})
	require.NoError(t, err)
	data, _ := os.ReadFile(refPath)
	var payload map[string]any
	require.NoError(t, yaml.Unmarshal(data, &payload))
	require.Equal(t, "no_issues", payload["status"])
}

// writeOptTestModel 写最小合法模型配置（测试辅助）。
func writeOptTestModel(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "model.yaml")
	content := "model:\n  model_client_config:\n    client_id: c\n    client_provider: llm_OpenAI\n    model_name: m\n    api_key: k\n    base_url: http://x\n  model_request_config:\n    max_tokens: 512\n"
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	return path
}

func TestAllocateOptimizationDir(t *testing.T) {
	root := t.TempDir()
	first := allocateOptimizationDir(root)
	require.Equal(t, "member_optimization_001", filepath.Base(first))
	second := allocateOptimizationDir(root)
	require.Equal(t, "member_optimization_002", filepath.Base(second))
}

func TestShortID(t *testing.T) {
	require.Regexp(t, `^[0-9a-f]{8}$`, shortID())
}

func TestValidateCandidateHarnessRefs(t *testing.T) {
	dir := t.TempDir()
	good := writeOptTestHarness(t, dir, "solver")
	require.NoError(t, validateCandidateHarnessRefs([]MemberRoleCandidate{{Role: "solver", HarnessRefPath: good}}))
	err := validateCandidateHarnessRefs([]MemberRoleCandidate{{Role: "solver", HarnessRefPath: ""}})
	require.Error(t, err)
	require.Contains(t, err.Error(), "has empty harness_ref_path")
	err = validateCandidateHarnessRefs([]MemberRoleCandidate{{Role: "solver", HarnessRefPath: "/no/such"}})
	require.Contains(t, err.Error(), "does not exist")
}

func TestReadInputHarnessRefs_三形态(t *testing.T) {
	dir := t.TempDir()
	// 目录形态：目录下有 role 子目录
	harnessA := writeOptTestHarness(t, dir, "solver")
	refs := readInputHarnessRefs(dir)
	require.Equal(t, harnessA, refs["solver"])
	// roles 列表形态
	refsFile := filepath.Join(dir, "refs.yaml")
	os.WriteFile(refsFile, []byte("roles:\n- role: solver\n  harness_ref_path: "+harnessA+"\n"), 0o644)
	refs = readInputHarnessRefs(refsFile)
	require.Equal(t, harnessA, refs["solver"])
	// harness_refs dict 形态
	os.WriteFile(refsFile, []byte("harness_refs:\n  solver: "+harnessA+"\n"), 0o644)
	refs = readInputHarnessRefs(refsFile)
	require.Equal(t, harnessA, refs["solver"])
	// 空路径 / 不存在
	require.Empty(t, readInputHarnessRefs(""))
	require.Empty(t, readInputHarnessRefs("/no/such"))
}

func TestResolveHarnessRefValue(t *testing.T) {
	refsPath := "/tmp/x/refs.yaml"
	require.True(t, filepath.IsAbs(resolveHarnessRefValue("/abs/path", refsPath)))
	rel := resolveHarnessRefValue("rel/harness", refsPath)
	require.True(t, filepath.IsAbs(rel))
	require.Contains(t, rel, "rel"+string(filepath.Separator)+"harness")
}

func TestIsActionPlanRejection(t *testing.T) {
	require.True(t, isActionPlanRejection(&ActionPlanRejectedError{Msg: "x"}))
	require.True(t, isActionPlanRejection(errNewRuntime("Member action plan validation failed: x")))
	require.False(t, isActionPlanRejection(errNewRuntime("other")))
	require.False(t, isActionPlanRejection(errNewRuntime("")))
}

type runtimeErr struct{ msg string }

func (e runtimeErr) Error() string { return e.msg }

func errNewRuntime(msg string) error { return runtimeErr{msg} }

func TestWriteStageError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "err.json")
	require.NoError(t, writeStageError(path, "action_planning", errNewRuntime("boom"), map[string]any{"k": "v"}))
	data, _ := os.ReadFile(path)
	var payload map[string]any
	require.NoError(t, json.Unmarshal(data, &payload))
	require.Equal(t, "action_planning", payload["stage"])
	require.Equal(t, "boom", payload["error"])
}

func TestWriteRunExecutionResults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "exec.json")
	writeRunExecutionResults(path, []*MemberActionExecutionResult{{ActionID: "a1", Status: "succeeded"}})
	data, _ := os.ReadFile(path)
	var payload map[string]any
	require.NoError(t, json.Unmarshal(data, &payload))
	require.Len(t, payload["results"].([]any), 1)
}

func TestFindReusablePendingOptimization_无匹配(t *testing.T) {
	out := t.TempDir()
	require.Empty(t, findReusablePendingOptimization(out, "e", "a", "h"))
	// 非 pending_gate 状态不命中
	runDir := filepath.Join(out, "member_optimization_001")
	os.MkdirAll(runDir, 0o755)
	os.WriteFile(filepath.Join(runDir, "member_optimization_ref.yaml"),
		[]byte("status: success\npromotion_status: promoted\n"), 0o644)
	require.Empty(t, findReusablePendingOptimization(out, "e", "a", "h"))
}

func TestUpdateCandidateManifest(t *testing.T) {
	dir := t.TempDir()
	manifest := filepath.Join(dir, "manifest.yaml")
	os.WriteFile(manifest, []byte("plan_id: p\n"), 0o644)
	require.NoError(t, updateCandidateManifest(manifest, "candidate_ready", "/refs", []string{"solver"}, nil))
	data, _ := os.ReadFile(manifest)
	require.Contains(t, string(data), "candidate_ready")
	// 非 mapping 拒绝
	bad := filepath.Join(dir, "bad.yaml")
	os.WriteFile(bad, []byte("- a\n"), 0o644)
	require.Error(t, updateCandidateManifest(bad, "x", "", nil, nil))
}

func TestCompileSingleHarnessPlanningInputs(t *testing.T) {
	dir := t.TempDir()
	harness := writeOptTestHarness(t, dir, "solver")
	_ = harness
	candidates := []MemberRoleCandidate{{Role: "solver", MemberName: "solver-1", HarnessRefPath: harness}}
	hypotheses := []map[string]any{{
		"source_issue_id": "i1", "hypothesis_id": "h1", "required_behavior": "X",
		"evidence_refs": []any{"r1"},
		"lever_policy":  map[string]any{"recommended_lever": "instruction", "target_ref": "identity.md"},
	}}
	issues := []harnessrsi.TeamIssue{{IssueID: "i1", Summary: "s", Recommendation: "rec"}}
	roleReport, mechanismReport, selectionReport, err := compileSingleHarnessPlanningInputs(
		issues, candidates, hypotheses, "e", "a", harness, []string{"prompt"})
	require.NoError(t, err)
	require.Len(t, roleReport.AssignedRoleIssues, 1)
	require.Equal(t, 1.0, roleReport.AssignedRoleIssues[0].Confidence)
	require.Equal(t, "instruction", mechanismReport.RoleMechanisms["solver"][0].MechanismType)
	require.Len(t, selectionReport.Targets, 1)
	require.Equal(t, []string{"i1"}, selectionReport.Targets[0].AttributedIssueIDs)
	require.Equal(t, []string{"prompt_section"}, selectionReport.Targets[0].OptimizationSurfaces)
	// 双候选拒绝
	_, _, _, err = compileSingleHarnessPlanningInputs(issues, append(candidates, candidates[0]), hypotheses, "e", "a", harness, nil)
	require.Contains(t, err.Error(), "requires exactly one candidate role")
	// 缺假设
	_, _, _, err = compileSingleHarnessPlanningInputs([]harnessrsi.TeamIssue{{IssueID: "i2"}}, candidates, hypotheses, "e", "a", harness, nil)
	require.Contains(t, err.Error(), "missing immutable hypotheses")
}

// fmtSlice any → 字符串（元素拼接）。
func fmtSlice(value any) string {
	list, _ := value.([]any)
	out := ""
	for _, item := range list {
		out += fmt.Sprintf("%v ", item)
	}
	return out
}

func TestWriteCurrentHarnessRefs_累积发布(t *testing.T) {
	out := t.TempDir()
	harness := writeOptTestHarness(t, out, "solver")
	refsFile := filepath.Join(out, "refs.yaml")
	os.WriteFile(refsFile, []byte("roles:\n- role: solver\n  member_name: solver-1\n  harness_ref_path: "+harness+"\n"), 0o644)
	// 第一次发布
	written := writeCurrentHarnessRefs(harnessRefsWriteRequest{
		outputRoot: out, harnessRefsPath: refsFile,
		publishedRoles: []string{"solver"},
		roleResults:    map[string]RoleResult{"solver": {Status: "published", BeforeHarnessRefPath: harness, AfterHarnessRefPath: harness}},
		pathLayout:     NewMemberOptimizerPathLayout(out),
	})
	data, _ := os.ReadFile(written)
	require.Contains(t, string(data), "published")
	require.Contains(t, string(data), "pending_gate")
	// 累积第二次
	written2 := writeCurrentHarnessRefs(harnessRefsWriteRequest{
		outputRoot: out, harnessRefsPath: refsFile,
		publishedRoles:    []string{"reviewer"},
		roleResults:       map[string]RoleResult{"reviewer": {Status: "published"}},
		pathLayout:        NewMemberOptimizerPathLayout(out),
		refsPath:          filepath.Join(out, "c2.yaml"),
		cumulativePublish: true,
	})
	data2, _ := os.ReadFile(written2)
	require.Contains(t, string(data2), "last_attempt")
	// Python 同语义：cumulative 的 previous_published 读 harness_refs_path 源文件
	//（不含上次 refsPath 落盘）——故只有本次 reviewer
	var payload map[string]any
	require.NoError(t, yaml.Unmarshal(data2, &payload))
	published := fmtSlice(payload["published_roles"])
	require.Contains(t, published, "reviewer")
}

func TestPublishRoleHarness(t *testing.T) {
	out := t.TempDir()
	layout := NewMemberOptimizerPathLayout(out)
	runDir := filepath.Join(out, "member_optimization_001")
	os.MkdirAll(runDir, 0o755)
	integration := filepath.Join(out, "integration")
	os.MkdirAll(filepath.Join(integration, "tools"), 0o755)
	os.WriteFile(filepath.Join(integration, "tools", "x.py"), []byte("x"), 0o644)
	// 即时发布
	published, err := publishRoleHarness(layout, runDir, "solver", integration, false)
	require.NoError(t, err)
	require.Contains(t, published, filepath.Join("current", RoleKey("solver")))
	_, err = os.Stat(filepath.Join(published, "tools", "x.py"))
	require.NoError(t, err)
	// 候选发布
	candidate, err := publishRoleHarness(layout, runDir, "solver", integration, true)
	require.NoError(t, err)
	require.Contains(t, candidate, filepath.Join("candidate", RoleKey("solver")))
}

func TestMaxActionsLimit(t *testing.T) {
	require.Equal(t, 3, maxActionsLimit(0, true))
	require.Equal(t, 3, maxActionsLimit(5, true))
	require.Equal(t, 5, maxActionsLimit(5, false))
	require.Equal(t, 0, maxActionsLimit(0, false))
}

func TestNilIfEmpty(t *testing.T) {
	require.Nil(t, nilIfEmpty([]string{}))
	require.NotNil(t, nilIfEmpty([]string{"x"}))
}

func TestCopyPlanFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "a.txt")
	os.WriteFile(src, []byte("x"), 0o644)
	dst := filepath.Join(dir, "b.txt")
	copyPlanFile(src, dst)
	data, _ := os.ReadFile(dst)
	require.Equal(t, "x", string(data))
}

func TestPlanToDict(t *testing.T) {
	plan := &MemberOptimizationPlan{PlanID: "p"}
	m := planToDict(plan)
	require.Equal(t, "p", m["plan_id"])
}

func TestCoverage_小函数群(t *testing.T) {
	// verification 小函数
	require.True(t, anyCheckFailed([]VerificationCheck{{Status: "failed"}}))
	require.False(t, anyCheckFailed(nil))
	require.True(t, anyFailedWithPrefix([]VerificationCheck{{Name: "yaml_parse:a", Status: "failed"}}, "yaml_parse:"))
	require.False(t, anyFailedWithPrefix(nil, "x:"))
	// buildDefaultBuildContext
	ctx2 := buildDefaultBuildContext()
	require.Equal(t, "en", ctx2.GetLanguage())
	// changedNames
	a := map[string][]byte{"x": []byte("1")}
	b := map[string][]byte{"x": []byte("2"), "y": []byte("3")}
	require.Equal(t, []string{"x", "y"}, changedNames(a, b))
	// getRepairAgent（注入优先）
	verifier := NewHarnessChangeVerifier(&HarnessRepairAgent{}, nil, nil, 300)
	require.NotNil(t, verifier.getRepairAgent("ref"))
	require.NotNil(t, NewHarnessChangeVerifier(nil, nil, nil, 300).getRepairAgent("ref"))
	// minInt
	require.Equal(t, 3, minInt(3, 5))
	require.Equal(t, 2, minInt(5, 2))
	// jsonIndent
	data, err := jsonIndent(map[string]any{"k": 1})
	require.NoError(t, err)
	require.Contains(t, string(data), "\n")
	// looksLikeOrchestratorTeamRoot
	team := t.TempDir()
	require.False(t, looksLikeOrchestratorTeamRoot(team))
	os.MkdirAll(filepath.Join(team, "evaluations"), 0o755)
	require.True(t, looksLikeOrchestratorTeamRoot(team))
	require.False(t, looksLikeOrchestratorTeamRoot(filepath.Join(team, "no")))
}

func TestCheckEffectivePromptBinding_与挂载(t *testing.T) {
	// 正常
	check := checkEffectivePromptBinding("solver", []promptSectionView{{Name: "a"}, {Name: "b"}})
	require.Equal(t, "passed", check.Status)
	// 重名
	check = checkEffectivePromptBinding("solver", []promptSectionView{{Name: "a"}, {Name: "a"}})
	require.Equal(t, "failed", check.Status)
	// 空名
	check = checkEffectivePromptBinding("solver", []promptSectionView{{Name: ""}})
	require.Contains(t, check.Error, "empty name")
	// 技能挂载
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "my_skill")
	os.MkdirAll(skillDir, 0o755)
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: x\ndescription: d\n---\n"), 0o644)
	check = checkSkillRuntimeMounts("solver", &pluginPartsView{SkillDirs: []string{dir}})
	require.Equal(t, "passed", check.Status)
	check = checkSkillRuntimeMounts("solver", &pluginPartsView{SkillDirs: []string{"/no/such"}})
	require.Equal(t, "failed", check.Status)
	check = checkSkillRuntimeMounts("solver", &pluginPartsView{})
	require.Equal(t, "passed", check.Status)
	require.Equal(t, "no skills declared", check.Notes)
}

func TestBuildRepairUserMessage_文案(t *testing.T) {
	agent := NewHarnessRepairAgent("", nil, 300)
	msg := agent.BuildRepairUserMessage("solver", t.TempDir(), []map[string]any{{"name": "yaml_parse:solver/a.yaml", "status": "failed", "error": "e"}})
	require.Contains(t, msg, "## Role Expert Harness Repair")
	require.Contains(t, msg, "Role: solver")
	require.Contains(t, msg, "- yaml_parse:solver/a.yaml: failed | e")
	require.Contains(t, msg, "## Registry Contract")
}

func TestOptimizerOptimize_规划拒绝路径(t *testing.T) {
	// planning_rejected noop 形态直测（ActionPlanRejectedError 检测由
	// isActionPlanRejection 单测承载；planner 拒绝抛出链路由 planner 单测承载）
	dir := t.TempDir()
	m := NewMemberOptimizer(&MemberOptimizerConfig{AllowEmptyHarnessRefsNoop: true}, nil, nil, nil, nil, nil, nil)
	harness := writeOptTestHarness(t, dir, "solver")
	refPath, err := m.writeNoopArtifact("e", "a", harness, filepath.Join(dir, "out"), noopParams{
		status: "planning_rejected", reason: "Member action plan validation failed: x",
		runDir: filepath.Join(dir, "out", "member_optimization_001"),
		roles:  []string{"solver"}, skippedRoles: []string{"solver"},
		roleResults: map[string]RoleResult{"solver": {Status: "skipped", VerificationStatus: "not_run"}},
	})
	require.NoError(t, err)
	data, _ := os.ReadFile(refPath)
	var payload map[string]any
	require.NoError(t, yaml.Unmarshal(data, &payload))
	require.Equal(t, "planning_rejected", payload["status"])
	require.Contains(t, fmtSlice(payload["skipped_roles"]), "solver")
}

// fakeSelectorForOpt 注入选择器替身。
type fakeSelectorForOpt struct {
	report *MemberSelectionReport
}

func (f fakeSelectorForOpt) Select(*RoleAttributionReport, *MechanismAttributionReport, SelectorOptions) *MemberSelectionReport {
	return f.report
}

func TestHarnessAttributorAttribute_桥未注册拒绝(t *testing.T) {
	saved := SnapshotAgentsBridge()
	agentsBridge = AgentsBridge{}
	defer RestoreAgentsBridge(saved)
	attr := NewRoleAttributor(1, nil)
	dir := t.TempDir()
	harness := writeOptTestHarness(t, dir, "solver")
	_ = harness
	_, err := attr.Attribute(context.Background(), AttributeParams{
		TeamIssues:     []harnessrsi.TeamIssue{{IssueID: "i1", Summary: "s", Recommendation: "r", OptimizationTarget: "member_harness"}},
		CandidateRoles: []MemberRoleCandidate{{Role: "solver", HarnessRefPath: harness}},
	})
	require.Error(t, err)
	_ = ctxBackground()
}

func ctxBackground() context.Context { return context.Background() }

func TestPathLayout_编排器根检测(t *testing.T) {
	require.Regexp(t, `^[0-9a-f]{8}$`, hashToken(""))
}

func TestWriteCurrentHarnessRefs_失败与跳过回填(t *testing.T) {
	out := t.TempDir()
	refsFile := filepath.Join(out, "refs.yaml")
	os.WriteFile(refsFile, []byte("version: 1\n"), 0o644)
	written := writeCurrentHarnessRefs(harnessRefsWriteRequest{
		outputRoot: out, harnessRefsPath: refsFile,
		publishedRoles: nil,
		failedRoles:    []string{"solver"},
		skippedRoles:   []string{"reviewer"},
		roleResults: map[string]RoleResult{
			"solver": {Status: "failed", BeforeHarnessRefPath: "", AfterHarnessRefPath: ""},
		},
	})
	data, _ := os.ReadFile(written)
	require.Contains(t, string(data), "failed_roles:")
	require.Contains(t, string(data), "reviewer")
}

func TestAnyToStringList_矩阵(t *testing.T) {
	v, ok := anyToStringList([]string{"a"})
	require.True(t, ok)
	require.Equal(t, []string{"a"}, v)
	v, ok = anyToStringList([]any{"b", 1})
	require.True(t, ok)
	require.Equal(t, []string{"b", "1"}, v)
	_, ok = anyToStringList(nil)
	require.False(t, ok)
	_, ok = anyToStringList([]string(nil))
	require.False(t, ok)
	_, ok = anyToStringList(123)
	require.False(t, ok)
}

func TestSortedKeys_泛型(t *testing.T) {
	m := map[string]bool{"b": true, "a": true}
	require.Equal(t, []string{"a", "b"}, sortedKeys(m))
}

func TestExecutionResultFromRow_缺字段(t *testing.T) {
	result := executionResultFromRow(map[string]any{})
	require.Empty(t, result.ActionID)
	require.Empty(t, result.ChangedFiles)
}

func TestScanSkillDirectory_限额(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: x\ndescription: d\n---\n"), 0o644)
	// 文件数超限
	for i := 0; i < 5; i++ {
		os.WriteFile(filepath.Join(dir, itoa(i)+".txt"), []byte("x"), 0o644)
	}
	result := ScanSkillDirectory(dir, 3, 10*1024*1024)
	errs := strings.Join(toStrSlice(result["errors"]), "; ")
	require.Contains(t, errs, "file count")
	// 字节超限
	result = ScanSkillDirectory(dir, 200, 5)
	errs = strings.Join(toStrSlice(result["errors"]), "; ")
	require.Contains(t, errs, "total size")
}
