package memberoptimizer

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCheckYamlParse(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "a.yaml")
	os.WriteFile(good, []byte("a: 1\n"), 0o644)
	require.Equal(t, "passed", checkYamlParse(good).Status)
	bad := filepath.Join(dir, "b.yaml")
	os.WriteFile(bad, []byte("a: [1\n"), 0o644)
	result := checkYamlParse(bad)
	require.Equal(t, "failed", result.Status)
	require.NotEmpty(t, result.Error)
}

func TestCheckJSONParse(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "a.json")
	os.WriteFile(good, []byte(`{"a":1}`), 0o644)
	require.Equal(t, "passed", checkJSONParse(good).Status)
	bad := filepath.Join(dir, "b.json")
	os.WriteFile(bad, []byte("{"), 0o644)
	require.Equal(t, "failed", checkJSONParse(bad).Status)
}

func TestCheckPythonCompile(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "a.py")
	os.WriteFile(good, []byte("x = 1\n"), 0o644)
	require.Equal(t, "passed", checkPythonCompile(good).Status)
	bad := filepath.Join(dir, "b.py")
	os.WriteFile(bad, []byte("def f(:\n"), 0o644)
	require.Equal(t, "failed", checkPythonCompile(bad).Status)
}

func TestValidatePackagePythonSource_危险面(t *testing.T) {
	// dangerous import
	errs := validatePackagePythonSource("import subprocess\n", "a.py")
	require.Len(t, errs, 1)
	require.Contains(t, errs[0], "dangerous import 'subprocess'")
	// from import
	errs = validatePackagePythonSource("from os.path import join\n", "a.py")
	require.Len(t, errs, 1)
	// 内建危险调用
	errs = validatePackagePythonSource("eval('1')\n", "a.py")
	require.Len(t, errs, 1)
	// 属性调用不等于内建（re.compile）
	require.Empty(t, validatePackagePythonSource("import re\nre.compile('x')\n", "a.py"))
	// builtins 前缀命中
	errs = validatePackagePythonSource("import builtins\nbuiltins.exec('x')\n", "a.py")
	require.Len(t, errs, 1)
	// 语法错误
	errs = validatePackagePythonSource("def f(:\n", "a.py")
	require.Len(t, errs, 1)
}

func TestCheckPackageFileRef(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "tools"), 0o755)
	os.WriteFile(filepath.Join(dir, "tools", "x.py"), []byte("x = 1\n"), 0o644)
	require.Equal(t, "passed", checkPackageFileRef("solver", dir, "tool", "tools/x.py").Status)
	// .. 拒绝
	result := checkPackageFileRef("solver", dir, "tool", "../x.py")
	require.Equal(t, "failed", result.Status)
	// 不存在
	require.Equal(t, "failed", checkPackageFileRef("solver", dir, "tool", "tools/missing.py").Status)
	// 危险 Python 源
	os.WriteFile(filepath.Join(dir, "tools", "bad.py"), []byte("import subprocess\n"), 0o644)
	result = checkPackageFileRef("solver", dir, "tool", "tools/bad.py")
	require.Equal(t, "failed", result.Status)
	require.Contains(t, result.Error, "dangerous import")
}

func TestCheckPromptSectionsManifest(t *testing.T) {
	dir := t.TempDir()
	// 无 manifest → 空
	require.Empty(t, checkPromptSectionsManifest("solver", dir))
	os.MkdirAll(filepath.Join(dir, "prompt_sections", "files"), 0o755)
	os.WriteFile(filepath.Join(dir, "prompt_sections", "files", "a.md"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(dir, "prompt_sections", "sections.yaml"),
		[]byte("sections:\n- name: a\n  file: prompt_sections/files/a.md\n- name: b\n- name: c\n  file: missing.md\n"), 0o644)
	checks := checkPromptSectionsManifest("solver", dir)
	require.Len(t, checks, 3)
	require.Equal(t, "passed", checks[0].Status)
	require.Equal(t, "passed", checks[1].Status) // 无 file 引用 → passed
	require.Equal(t, "failed", checks[2].Status) // missing.md
}

func TestCheckSkillsManifest(t *testing.T) {
	dir := t.TempDir()
	require.Empty(t, checkSkillsManifest("solver", dir))
	// 正常技能
	skillDir := filepath.Join(dir, "skills", "my_skill")
	os.MkdirAll(skillDir, 0o755)
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: my_skill\ndescription: d\n---\nbody\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "skills", "skills.yaml"), []byte("skills:\n- dir: skills/my_skill\n"), 0o644)
	checks := checkSkillsManifest("solver", dir)
	require.Len(t, checks, 2)
	require.Equal(t, "passed", checks[0].Status)
	require.Equal(t, "passed", checks[1].Status)
	// 缺 SKILL.md
	badDir := filepath.Join(dir, "skills", "bad_skill")
	os.MkdirAll(badDir, 0o755)
	os.WriteFile(filepath.Join(dir, "skills", "skills.yaml"), []byte("skills:\n- dir: skills/bad_skill\n"), 0o644)
	checks = checkSkillsManifest("solver", dir)
	require.Equal(t, "failed", checks[0].Status)
	// 危险脚本
	os.WriteFile(filepath.Join(badDir, "SKILL.md"), []byte("---\nname: b\ndescription: d\n---\n"), 0o644)
	os.WriteFile(filepath.Join(badDir, "run.sh"), []byte("rm -rf /\n"), 0o644)
	checks = checkSkillsManifest("solver", dir)
	require.Equal(t, "failed", checks[1].Status)
	require.Contains(t, checks[1].Error, "forbidden executable file type")
}

func TestScanSkillDirectory_安全矩阵(t *testing.T) {
	dir := t.TempDir()
	// 缺 SKILL.md
	result := ScanSkillDirectory(dir, 200, 10*1024*1024)
	require.Equal(t, "failed", result["status"])
	os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: x\ndescription: d\n---\n"), 0o644)
	// 符号链接拒绝
	link := filepath.Join(dir, "link")
	os.Symlink(dir, link)
	result = ScanSkillDirectory(dir, 200, 10*1024*1024)
	require.Equal(t, "failed", result["status"])
	require.Contains(t, strings.Join(toStrSlice(result["errors"]), "; "), "symlink not allowed")
	os.Remove(link)
	// 隐藏路径
	os.MkdirAll(filepath.Join(dir, ".hidden"), 0o755)
	os.WriteFile(filepath.Join(dir, ".hidden", "x.md"), []byte("x"), 0o644)
	result = ScanSkillDirectory(dir, 200, 10*1024*1024)
	require.Contains(t, strings.Join(toStrSlice(result["errors"]), "; "), "hidden path not allowed")
	os.RemoveAll(filepath.Join(dir, ".hidden"))
	// 禁用后缀
	os.WriteFile(filepath.Join(dir, "run.sh"), []byte("x"), 0o644)
	result = ScanSkillDirectory(dir, 200, 10*1024*1024)
	require.Contains(t, strings.Join(toStrSlice(result["errors"]), "; "), "forbidden executable file type")
	os.Remove(filepath.Join(dir, "run.sh"))
	// 危险 Python 调用
	os.WriteFile(filepath.Join(dir, "run.py"), []byte("import subprocess\nsubprocess.run('ls')\n"), 0o644)
	result = ScanSkillDirectory(dir, 200, 10*1024*1024)
	require.Contains(t, strings.Join(toStrSlice(result["errors"]), "; "), "dangerous call 'subprocess.run'")
	os.Remove(filepath.Join(dir, "run.py"))
	// 干净 → passed
	result = ScanSkillDirectory(dir, 200, 10*1024*1024)
	require.Equal(t, "passed", result["status"])
	require.Equal(t, 1, result["file_count"])
}

func TestCheckRailRuntimeContract(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "rails"), 0o755)
	// ctx.inputs.get → 违规
	bad := filepath.Join(dir, "rails", "bad_rail.py")
	os.WriteFile(bad, []byte("def hook(ctx):\n    return ctx.inputs.get('response')\n"), 0o644)
	result := checkRailRuntimeContract("solver", dir, "rails/bad_rail.py")
	require.Equal(t, "failed", result.Status)
	require.Contains(t, result.Error, "use ctx.inputs.response/react_iteration")
	// task-loop 钩子
	os.WriteFile(bad, []byte("def before_task_iteration(ctx):\n    pass\n"), 0o644)
	result = checkRailRuntimeContract("solver", dir, "rails/bad_rail.py")
	require.Contains(t, result.Error, "does not run with solver enable_task_loop=False")
	// 语法错误
	os.WriteFile(bad, []byte("def f(:\n"), 0o644)
	result = checkRailRuntimeContract("solver", dir, "rails/bad_rail.py")
	require.Equal(t, "failed", result.Status)
	// 干净
	os.WriteFile(bad, []byte("def hook(ctx):\n    return ctx.inputs.response\n"), 0o644)
	require.Equal(t, "passed", checkRailRuntimeContract("solver", dir, "rails/bad_rail.py").Status)
}

func TestRuntimeToolNamesMatch(t *testing.T) {
	require.True(t, runtimeToolNamesMatch("my_tool", "my_tool"))
	require.True(t, runtimeToolNamesMatch("my-tool", "my_tool"))
	// Python removesuffix 语义：my_tool 与 my_tool_tool 均以 _tool 结尾，各剥一次后
	// 分别为 my / my_tool → 不匹配（Python 同语义）
	require.False(t, runtimeToolNamesMatch("my_tool", "my_tool_tool"))
	require.False(t, runtimeToolNamesMatch("", "my_tool"))
	require.False(t, runtimeToolNamesMatch("other", "my_tool"))
}

func TestValidatePlanSchema(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "plan.yaml")
	os.WriteFile(good, []byte("plan_id: p\nactions: []\naction_waves: []\n"), 0o644)
	require.Equal(t, "passed", validatePlanSchema(good)[0].Status)
	bad := filepath.Join(dir, "bad.yaml")
	os.WriteFile(bad, []byte("plan_id: p\n"), 0o644)
	result := validatePlanSchema(bad)[0]
	require.Equal(t, "failed", result.Status)
	require.Contains(t, result.Error, "Missing required fields")
}

func TestValidateExecutionResults(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "exec.json")
	os.WriteFile(good, []byte(`{"results": []}`), 0o644)
	require.Equal(t, "passed", validateExecutionResults(good)[0].Status)
	bad := filepath.Join(dir, "bad.json")
	os.WriteFile(bad, []byte(`{}`), 0o644)
	require.Equal(t, "failed", validateExecutionResults(bad)[0].Status)
}

func TestRepairable判定矩阵(t *testing.T) {
	require.True(t, isWorktreeRepairableCheck("yaml_parse:role/a.yaml"))
	require.False(t, isWorktreeRepairableCheck("plan_schema"))
	require.True(t, isBlockingUnrepairableCheck("plan_schema"))
	require.True(t, isBlockingUnrepairableCheck("action_policy:a1"))
	require.False(t, isBlockingUnrepairableCheck("yaml_parse:role/a.yaml"))
	require.True(t, roleIsRepairable([]VerificationCheck{
		{Name: "yaml_parse:role/a.yaml", Status: "failed"},
	}))
	require.False(t, roleIsRepairable([]VerificationCheck{
		{Name: "plan_schema", Status: "failed"},
	}))
	require.False(t, roleIsRepairable([]VerificationCheck{{Name: "x", Status: "passed"}}))
	require.Len(t, repairableFailedChecks([]VerificationCheck{
		{Name: "yaml_parse:a", Status: "failed"}, {Name: "plan_schema", Status: "failed"},
	}), 1)
}

func TestSummarizeFailedChecks(t *testing.T) {
	checks := []map[string]any{
		{"name": "yaml_parse:a.yaml", "status": "failed", "error": strings.Repeat("x", 600)},
	}
	summary := summarizeFailedChecks(checks, 12)
	require.Contains(t, summary, "- yaml_parse:a.yaml: failed")
	require.Contains(t, summary, "...")
	// 截断到 500
	require.Less(t, len(strings.Split(summary, "| ")[1]), 510)
	require.Equal(t, "- no failed checks", summarizeFailedChecks(nil, 12))
}

func TestFailedCheckRelativePath(t *testing.T) {
	require.Equal(t, "a/b.yaml", failedCheckRelativePath("yaml_parse:role/a/b.yaml"))
	// Python split("/", 1)[1] 语义：单段路径返回尾段 "b.yaml"
	require.Equal(t, "b.yaml", failedCheckRelativePath("yaml_parse:a/b.yaml"))
	require.Equal(t, "rails/x.py", failedCheckRelativePath("tool_file_ref:solver:rails/x.py"))
	require.Equal(t, "rails/x.py", failedCheckRelativePath("rail_runtime_contract:solver:rails/x.py"))
	require.Empty(t, failedCheckRelativePath("plan_schema"))
}

func TestSummarizeAgentResponse(t *testing.T) {
	require.Equal(t, "DeepAgent repair turn completed.", summarizeAgentResponse(""))
	require.Equal(t, "a\nb\nc\nd\ne\nf\ng\nh", summarizeAgentResponse("a\n\nb\nc\nd\ne\nf\ng\nh\ni")) // 前 8 非空行
	long := strings.Repeat("x", 900)
	require.Less(t, len(summarizeAgentResponse(long)), 810)
}

func TestSnapshotAndEnforceRepairScope(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "tools", "x.py"), []byte("x"), 0o644)
	os.MkdirAll(filepath.Join(root, "tools"), 0o755)
	os.WriteFile(filepath.Join(root, "tools", "x.py"), []byte("x = 1\n"), 0o644)
	before := snapshotRepairFiles(root)
	// 修改合法文件 + 新建非法文件
	os.WriteFile(filepath.Join(root, "tools", "x.py"), []byte("x = 2\n"), 0o644)
	os.WriteFile(filepath.Join(root, "audit", "leak.yaml"), []byte("secret"), 0o644)
	os.MkdirAll(filepath.Join(root, "audit"), 0o755)
	os.WriteFile(filepath.Join(root, "audit", "leak.yaml"), []byte("secret"), 0o644)
	after := snapshotRepairFiles(root)
	forbidden := enforceRepairScope(root, before, after)
	require.Equal(t, []string{"audit/leak.yaml"}, forbidden)
	// 越界文件被回滚（删除）
	_, err := os.Stat(filepath.Join(root, "audit", "leak.yaml"))
	require.True(t, os.IsNotExist(err))
	// 合法修改保留
	data, _ := os.ReadFile(filepath.Join(root, "tools", "x.py"))
	require.Equal(t, "x = 2\n", string(data))
}

func TestListRepairContextFiles(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "a.py"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(root, "b.pyc"), []byte("x"), 0o644)
	files := listRepairContextFiles(root)
	require.Equal(t, []string{"a.py"}, files)
	// 不存在目录
	require.Empty(t, listRepairContextFiles(filepath.Join(root, "no")))
}

func TestRepairFailedFileContext(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "a.yaml"), []byte("line1\nline2\n"), 0o644)
	got := repairFailedFileContext(root, []map[string]any{{"name": "yaml_parse:solver/a.yaml"}})
	require.Contains(t, got, "### a.yaml")
	require.Contains(t, got, "0001: line1")
	// 无文件指向
	require.Equal(t, "No failed check pointed to a concrete local file.",
		repairFailedFileContext(root, []map[string]any{{"name": "plan_schema"}}))
}

func TestHarnessChangeVerifier_Verify_通过与失败(t *testing.T) {
	dir := t.TempDir()
	wtDir := filepath.Join(dir, "run", "wt")
	// plan.yaml + execution_results.json
	os.WriteFile(filepath.Join(dir, "run", "plan.yaml"), []byte("plan_id: p\nactions: []\naction_waves: []\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "run", "execution_results.json"), []byte(`{"results": []}`), 0o644)
	// 角色整合区：干净空目录（integration_dir passed；无 harness.yaml → expert_harness_load fail）
	integration := filepath.Join(wtDir, "r123", "integration")
	os.MkdirAll(integration, 0o755)
	verifier := NewHarnessChangeVerifier(nil, nil, nil, 300)
	plan := &MemberOptimizationPlan{PlanID: "p", Targets: []MemberOptimizationTarget{{Role: "solver"}}}
	result, err := verifier.Verify(plan, wtDir, filepath.Join(dir, "verification.json"))
	require.NoError(t, err)
	require.Equal(t, "failed", result.Status) // expert_harness_load 失败（可修复前缀）
	require.True(t, result.Repairable)
	// 落盘验证
	data, err := os.ReadFile(filepath.Join(dir, "verification.json"))
	require.NoError(t, err)
	var payload map[string]any
	require.NoError(t, json.Unmarshal(data, &payload))
	require.Equal(t, "failed", payload["status"])
}

func TestHarnessChangeVerifier_Repair_已通过即免修(t *testing.T) {
	dir := t.TempDir()
	verifier := NewHarnessChangeVerifier(nil, nil, nil, 300)
	result := &MemberVerificationResult{Status: "passed"}
	fix, err := verifier.Repair(result, filepath.Join(dir, "wt"), filepath.Join(dir, "fix.json"), "", 2)
	require.NoError(t, err)
	require.Equal(t, "not_needed", fix.Status)
	require.Equal(t, "passed", fix.FinalVerificationStatus)
}

func TestHarnessRepairAgent_RepairRole_无可修复(t *testing.T) {
	agent := NewHarnessRepairAgent("", nil, 300)
	result, err := agent.RepairRole(t.TempDir(), "solver", []map[string]any{{"name": "plan_schema", "status": "failed"}})
	require.NoError(t, err)
	require.Equal(t, "failed", result["status"])
}

func TestPrepareOutputPath(t *testing.T) {
	out := filepath.Join(t.TempDir(), "sub", "v.json")
	resolved, err := PrepareOutputPath(out)
	require.NoError(t, err)
	require.True(t, strings.HasSuffix(resolved, "v.json"))
}

func TestValidateActionResultsByRole_缺失与跳过(t *testing.T) {
	dir := t.TempDir()
	execPath := filepath.Join(dir, "exec.json")
	os.WriteFile(execPath, []byte(`{"results": [{"action_id": "a1", "status": "succeeded", "merge_status": "merged"}]}`), 0o644)
	plan := &MemberOptimizationPlan{
		PlanID: "p",
		Targets: []MemberOptimizationTarget{{Role: "solver", AttributedIssueIDs: []string{"i1"}}},
		Actions: []MemberOptimizationAction{{
			ActionID: "a1", Role: "solver", ActionGroup: "config", Operation: "modify",
			TargetPath: "runtime.yaml", DeclaredWritePaths: []string{"runtime.yaml"},
			AttributedIssueIDs: []string{"i1"},
		}},
	}
	checksByRole := validateActionResultsByRole(plan, execPath)
	require.NotEmpty(t, checksByRole["solver"])
	// 坏 JSON → execution_results_load
	os.WriteFile(execPath, []byte("{"), 0o644)
	checksByRole = validateActionResultsByRole(plan, execPath)
	require.Equal(t, "execution_results_load", checksByRole["solver"][0].Name)
}

func TestToolInputSchemaError(t *testing.T) {
	require.Empty(t, toolInputSchemaError(nil))
	require.Empty(t, toolInputSchemaError([]*paramLike{}))
}

func TestLoadHarnessPlugin_缺manifest(t *testing.T) {
	_, err := loadHarnessPlugin(t.TempDir())
	require.Error(t, err)
}

func TestBridgeExtractText_兜底(t *testing.T) {
	require.Equal(t, "hi", agentsBridgeExtractText(map[string]any{"text": "hi"}))
	require.Empty(t, agentsBridgeExtractText(map[string]any{}))
}

func TestBridgeUnregistered_RepairRole(t *testing.T) {
	saved := SnapshotAgentsBridge()
	agentsBridge = AgentsBridge{}
	defer RestoreAgentsBridge(saved)
	agent := NewHarnessRepairAgent("", nil, 300)
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "prompt_sections"), 0o755)
	// make a repairable check
	_, err := agent.RepairRole(dir, "solver", []map[string]any{{"name": "yaml_parse:solver/a.yaml", "status": "failed"}})
	require.Error(t, err)
	require.Contains(t, err.Error(), "agents bridge not registered")
	_ = context.Background()
}

func TestExecutionResultFromRow(t *testing.T) {
	row := map[string]any{
		"action_id": "a1", "role": "solver", "status": "succeeded",
		"merge_status": "merged", "changed_files": []any{"a.md"},
		"declared_write_paths": []any{"a.md"}, "error": "",
	}
	result := executionResultFromRow(row)
	require.Equal(t, "a1", result.ActionID)
	require.Equal(t, "merged", result.MergeStatus)
	require.Equal(t, []string{"a.md"}, result.ChangedFiles)
}

func TestValidateActionResultsByRole_回退成功(t *testing.T) {
	dir := t.TempDir()
	execPath := filepath.Join(dir, "exec.json")
	// 主动作失败 + 回退成功
	os.WriteFile(execPath, []byte(`{"results": [
		{"action_id": "primary", "status": "failed", "merge_status": "not_merged", "error": "boom"},
		{"action_id": "fallback", "status": "succeeded", "merge_status": "merged"}
	]}`), 0o644)
	plan := &MemberOptimizationPlan{
		PlanID: "p",
		Targets: []MemberOptimizationTarget{{Role: "solver", AttributedIssueIDs: []string{"i1"}}},
		Actions: []MemberOptimizationAction{
			{ActionID: "primary", Role: "solver", ActionGroup: "config", Operation: "modify",
				TargetPath: "runtime.yaml", DeclaredWritePaths: []string{"runtime.yaml"}, AttributedIssueIDs: []string{"i1"}},
			{ActionID: "fallback", Role: "solver", ActionGroup: "config", Operation: "modify", RunIf: "dependency_failed",
				TargetPath: "runtime.yaml", DeclaredWritePaths: []string{"runtime.yaml"}, AttributedIssueIDs: []string{"i1"},
				DependsOn: []string{"primary"}},
		},
	}
	checksByRole := validateActionResultsByRole(plan, execPath)
	// primary: fallback 成功 → satisfied
	names := map[string]string{}
	for _, check := range checksByRole["solver"] {
		names[check.Name] = check.Status
	}
	require.Equal(t, "passed", names["action_result:primary"])
	require.Equal(t, "passed", names["action_result:fallback"])
	require.Equal(t, "passed", names["role_action_bundle:solver"])
}

func toStrSlice(value any) []string {
	if list, ok := value.([]string); ok {
		return list
	}
	return nil
}
