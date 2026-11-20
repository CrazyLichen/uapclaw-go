package memberoptimizer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCanonicalSkillIdentifier(t *testing.T) {
	require.Equal(t, "my_skill", CanonicalSkillIdentifier("My Skill"))
	require.Equal(t, "skill_md", CanonicalSkillIdentifier("skills/my-skill/SKILL.md")) // Python split("/")[-1] 语义
	require.Equal(t, "abc123", CanonicalSkillIdentifier("ABC123"))
	require.Equal(t, "member_skill", CanonicalSkillIdentifier("!!!"))
	require.Equal(t, "a_b", CanonicalSkillIdentifier("a  b")) // 连续分隔符合并为单下划线
}

func TestSkillNameFromSkillMdPath(t *testing.T) {
	require.Equal(t, "foo_bar", SkillNameFromSkillMdPath("skills/foo-bar/SKILL.md"))
	require.Equal(t, "", SkillNameFromSkillMdPath("skills/foo"))
	require.Equal(t, "", SkillNameFromSkillMdPath("identity.md"))
}

func TestSnapshotAndChangedFiles(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.md"), []byte("v1"), 0o644)
	os.MkdirAll(filepath.Join(dir, "sub"), 0o755)
	os.WriteFile(filepath.Join(dir, "sub", "b.txt"), []byte("x"), 0o644)
	before := SnapshotFiles(dir, nil)
	// 修改 a.md + 新增 c.md
	os.WriteFile(filepath.Join(dir, "a.md"), []byte("v2"), 0o644)
	os.WriteFile(filepath.Join(dir, "c.md"), []byte("new"), 0o644)
	after := SnapshotFiles(dir, nil)
	changed := ChangedFiles(before, after)
	require.Equal(t, []string{"a.md", "c.md"}, changed)
	// 选定相对路径快照
	subSnap := SnapshotFiles(dir, []string{"sub/b.txt"})
	require.Len(t, subSnap, 1)
}

func TestShouldRunAction(t *testing.T) {
	// 无依赖
	run, _ := ShouldRunAction(MemberOptimizationAction{}, map[string]string{})
	require.True(t, run)
	// always
	run, _ = ShouldRunAction(MemberOptimizationAction{DependsOn: []string{"x"}, RunIf: "always"}, map[string]string{})
	require.True(t, run)
	// dependency_succeeded 且依赖成功
	run, _ = ShouldRunAction(MemberOptimizationAction{DependsOn: []string{"x"}, RunIf: "dependency_succeeded"}, map[string]string{"x": "succeeded"})
	require.True(t, run)
	// 依赖失败
	run, reason := ShouldRunAction(MemberOptimizationAction{DependsOn: []string{"x"}, RunIf: "dependency_succeeded"}, map[string]string{"x": "failed"})
	require.False(t, run)
	require.Equal(t, "dependency failed: [x]", reason)
	// dependency_failed 命中
	run, _ = ShouldRunAction(MemberOptimizationAction{DependsOn: []string{"x"}, RunIf: "dependency_failed"}, map[string]string{"x": "failed"})
	require.True(t, run)
	// dependency_failed 未命中
	run, reason = ShouldRunAction(MemberOptimizationAction{DependsOn: []string{"x"}, RunIf: "dependency_failed"}, map[string]string{"x": "succeeded"})
	require.False(t, run)
	require.Equal(t, "dependency did not fail", reason)
	// 依赖状态缺失
	run, reason = ShouldRunAction(MemberOptimizationAction{DependsOn: []string{"x"}, RunIf: "dependency_succeeded"}, map[string]string{})
	require.False(t, run)
	require.Contains(t, reason, "dependency status unavailable: [x]")
}

func TestPathAllowedByDeclared(t *testing.T) {
	require.True(t, PathAllowedByDeclared("skills/foo/SKILL.md", []string{"skills/"}))
	require.True(t, PathAllowedByDeclared("runtime.yaml", []string{"runtime.yaml"}))
	require.False(t, PathAllowedByDeclared("tools/x.py", []string{"skills/"}))
}

func TestIsRuntimeWorkspaceMetadata(t *testing.T) {
	require.True(t, IsRuntimeWorkspaceMetadata("AGENT.md"))
	require.True(t, IsRuntimeWorkspaceMetadata("context/foo/.workspace"))
	require.True(t, IsRuntimeWorkspaceMetadata("memory/x"))
	require.False(t, IsRuntimeWorkspaceMetadata("identity.md"))
}

func TestSanitizeRuntimeSemanticText_标识符剥离(t *testing.T) {
	// 前缀剥离
	got := SanitizeRuntimeSemanticText("member_harness.solver.identity.md: vague prompt")
	require.Equal(t, "vague prompt", got)
	// test_xxx → the observed check
	got = SanitizeRuntimeSemanticText("check test_foo_pass")
	require.Equal(t, "the observed check", got)
	// verifier → acceptance evaluation
	got = SanitizeRuntimeSemanticText("official verifier failed")
	require.Equal(t, "acceptance evaluation failed", got)
	// SWE-bench / FAIL_TO_PASS 替换
	got = SanitizeRuntimeSemanticText("SWE-bench FAIL_TO_PASS")
	require.Contains(t, got, "authoritative evaluation")
	require.Contains(t, got, "required checks")
	// hyp/issue id 剥离
	got = SanitizeRuntimeSemanticText("see hyp_abc and issue_1")
	require.NotContains(t, got, "hyp_abc")
	require.NotContains(t, got, "issue_1")
	// 空输入
	require.Equal(t, "", SanitizeRuntimeSemanticText(nil))
}

func TestRuntimeContractProjection_公共边界(t *testing.T) {
	action := MemberOptimizationAction{Constraints: map[string]any{
		"optimization_contracts": []any{map[string]any{
			"public_trigger":     []any{map[string]any{"task": "fix the parser"}},
			"required_behavior":  "handle nested inputs",
			"forbidden_behavior": []any{"skip validation"},
			"decision_contract": map[string]any{
				"wrong_decision": "a", "causal_distinction": "b",
				"required_action": "c", "acceptance_observable": "d",
				"scope_boundary": []any{"e"}, "activation_phase": "task_start",
			},
		}},
	}}
	projected := RuntimeContractProjection(action)
	require.Len(t, projected, 1)
	require.Equal(t, []any{"fix the parser"}, projected[0]["public_tasks"])
	require.Equal(t, "handle nested inputs", projected[0]["required_behavior"])
	dc := projected[0]["decision_contract"].(map[string]any)
	require.Equal(t, "task_start", dc["activation_phase"])
	// 空约束 → 空
	require.Empty(t, RuntimeContractProjection(MemberOptimizationAction{}))
}

func TestValidateGeneratedSkillContract(t *testing.T) {
	// 缺 description
	err := ValidateGeneratedSkillContract("---\nname: x\n---\nbody")
	require.Error(t, err)
	require.Contains(t, err.Error(), "description is required")
	// 空 body
	err = ValidateGeneratedSkillContract("---\nname: x\ndescription: d\n---\n")
	require.Error(t, err)
	require.Contains(t, err.Error(), "must contain runtime instructions")
	// 合法
	require.NoError(t, ValidateGeneratedSkillContract("---\nname: x\ndescription: d\n---\n# Instructions\nDo the thing."))
	// 无 frontmatter 但有 body
	require.NoError(t, ValidateGeneratedSkillContract("# Just body"))
}

func TestSalvageSkillFrontmatter(t *testing.T) {
	salvaged := SalvageSkillFrontmatter("name: my_skill\ndescription: >\n  folded\nother: kept?")
	require.Equal(t, "my_skill", salvaged["name"])
	require.NotContains(t, salvaged, "description") // 块标量被拒
	require.NotContains(t, salvaged, "other")
}

func TestNormalizeSkillFrontmatterName(t *testing.T) {
	// 无 frontmatter → 注入
	got := NormalizeSkillFrontmatterName("# Body", "My Skill", "")
	require.Contains(t, got, "name: my_skill")
	require.Contains(t, got, "description: Reusable member harness skill.")
	require.Contains(t, got, "# Body")
	// 已有 description 保留
	got = NormalizeSkillFrontmatterName("---\nname: wrong\ndescription: keep me\n---\nBody", "right_name", "")
	require.Contains(t, got, "name: right_name")
	require.Contains(t, got, "keep me")
}

func TestActionResourceGuidance_分支(t *testing.T) {
	require.Contains(t, ActionResourceGuidance(MemberOptimizationAction{ActionGroup: "config"}), "Edit only runtime.yaml")
	require.Contains(t, ActionResourceGuidance(MemberOptimizationAction{
		ActionGroup: "tool", Operation: "add", TargetPath: "tools/calc.py",
		Constraints: map[string]any{"class_name": "Calc"},
	}), "Create `tools/calc.py` with a loadable `Calc` class")
	require.Contains(t, ActionResourceGuidance(MemberOptimizationAction{
		ActionGroup: "rail", Operation: "add", TargetPath: "rails/g.py",
	}), "AgentRail subclass")
	require.Contains(t, ActionResourceGuidance(MemberOptimizationAction{
		ActionGroup: "skill", Operation: "add", TargetPath: "skills/x/SKILL.md",
	}), "package-local skill")
	require.Contains(t, ActionResourceGuidance(MemberOptimizationAction{
		ActionGroup: "subagent", Operation: "add", TargetPath: "subagents/x",
	}), "subagent/add")
	require.Contains(t, ActionResourceGuidance(MemberOptimizationAction{
		ActionGroup: "tool", Operation: "modify", TargetPath: "tools/x.py",
	}), "tools/tools.yaml")
	require.Equal(t, "Apply the requested local ExpertHarness package change directly.",
		ActionResourceGuidance(MemberOptimizationAction{ActionGroup: "model"}))
}

func TestSkillTriggerDescription_决策合同优先(t *testing.T) {
	action := MemberOptimizationAction{Constraints: map[string]any{
		"optimization_contracts": []any{map[string]any{
			"decision_contract": map[string]any{"causal_distinction": "input is nested rather than flat."},
			"public_trigger":    []any{"fix parser"},
		}},
	}}
	require.Equal(t, "Use when a task requires deciding whether input is nested rather than flat.",
		SkillTriggerDescription(action))
	// 无决策合同 → public_trigger
	action = MemberOptimizationAction{Constraints: map[string]any{
		"optimization_contracts": []any{map[string]any{"public_trigger": []any{"fix parser"}}},
	}}
	require.Equal(t, "Use when fix parser.", SkillTriggerDescription(action))
	// 全空 → 默认
	require.Equal(t, "Use when the current task matches this reusable procedure.",
		SkillTriggerDescription(MemberOptimizationAction{}))
}

func TestExecuteDeterministicRemove_分支(t *testing.T) {
	ws := t.TempDir()
	declared := []string{"skills/foo", "skills/skills.yaml"}
	os.MkdirAll(filepath.Join(ws, "skills", "foo"), 0o755)
	os.WriteFile(filepath.Join(ws, "skills", "foo", "SKILL.md"), []byte("s"), 0o644)
	os.WriteFile(filepath.Join(ws, "skills", "skills.yaml"), []byte("skills:\n  - skills/foo\n"), 0o644)
	action := MemberOptimizationAction{ActionGroup: "skill", Operation: "remove", TargetPath: "skills/foo/SKILL.md"}
	result := executeDeterministicRemove(ws, action, declared)
	require.Equal(t, "succeeded", result["status"])
	require.FileExists(t, filepath.Join(ws, "skills", "skills.yaml"))
	// manifest 条目已移除
	data, _ := os.ReadFile(filepath.Join(ws, "skills", "skills.yaml"))
	require.NotContains(t, string(data), "skills/foo")
	// 越界拒绝
	action2 := MemberOptimizationAction{ActionGroup: "skill", Operation: "remove", TargetPath: "identity.md"}
	result = executeDeterministicRemove(ws, action2, declared)
	require.Equal(t, "failed", result["status"])
	// 组不支持
	action3 := MemberOptimizationAction{ActionGroup: "model", Operation: "remove", TargetPath: "x.yaml"}
	result = executeDeterministicRemove(ws, action3, []string{"x.yaml"})
	require.Contains(t, result["error"], "deterministic remove unsupported for model")
}

func TestExecuteDeterministicAddScaffold_工具与护栏(t *testing.T) {
	ws := t.TempDir()
	declared := []string{"tools/calc.py", "tools/tools.yaml"}
	action := MemberOptimizationAction{
		ActionGroup: "tool", Operation: "add", TargetPath: "tools/calc.py",
		Description: "adds numbers", Constraints: map[string]any{"class_name": "CalcTool"},
	}
	result := executeDeterministicAddScaffold(ws, action, declared)
	require.Equal(t, "succeeded", result["status"])
	content, _ := os.ReadFile(filepath.Join(ws, "tools", "calc.py"))
	require.Contains(t, string(content), "class CalcTool(Tool):")
	require.Contains(t, string(content), "id='calc_tool'")
	require.Contains(t, string(content), `description='adds numbers'`)
	manifest, _ := os.ReadFile(filepath.Join(ws, "tools", "tools.yaml"))
	require.Contains(t, string(manifest), "class_name: CalcTool")
	// rail 脚手架
	ws2 := t.TempDir()
	declared2 := []string{"rails/g.py", "rails/rails.yaml"}
	action2 := MemberOptimizationAction{
		ActionGroup: "rail", Operation: "add", TargetPath: "rails/g.py",
		Constraints: map[string]any{"class_name": "GuardRail"},
	}
	result = executeDeterministicAddScaffold(ws2, action2, declared2)
	require.Equal(t, "succeeded", result["status"])
	content, _ = os.ReadFile(filepath.Join(ws2, "rails", "g.py"))
	require.Contains(t, string(content), "class GuardRail(AgentRail):")
	// 缺 class_name（声明含目标与 manifest）
	result = executeDeterministicAddScaffold(ws, MemberOptimizationAction{
		ActionGroup: "tool", Operation: "add", TargetPath: "tools/x.py",
	}, []string{"tools/x.py", "tools/tools.yaml"})
	require.Contains(t, result["error"], "requires constraints.class_name")
	// 非 add 形态 → skipped
	result = executeDeterministicAddScaffold(ws, MemberOptimizationAction{
		ActionGroup: "prompt", Operation: "add", TargetPath: "identity.md",
	}, []string{"identity.md"})
	require.Equal(t, "skipped", result["status"])
}

func TestNormalizeRailManifestRefs(t *testing.T) {
	ws := t.TempDir()
	os.MkdirAll(filepath.Join(ws, "rails"), 0o755)
	os.WriteFile(filepath.Join(ws, "rails", "rails.yaml"), []byte("rails:\n  - file: some/rail.py\n    class_name: R\n"), 0o644)
	action := MemberOptimizationAction{ActionGroup: "rail"}
	fix := normalizeRailManifestRefs(ws, action, []string{"rails/rails.yaml"})
	require.NotNil(t, fix)
	require.Equal(t, []any{"rails/rail.py"}, fix["normalized_file_refs"])
	data, _ := os.ReadFile(filepath.Join(ws, "rails", "rails.yaml"))
	require.Contains(t, string(data), "file: rails/rail.py")
	// 非 rail 组 → nil
	require.Nil(t, normalizeRailManifestRefs(ws, MemberOptimizationAction{ActionGroup: "tool"}, []string{"rails/rails.yaml"}))
}

func TestValidateGeneratedActionResources(t *testing.T) {
	ws := t.TempDir()
	// 危险 import 拒绝
	os.WriteFile(filepath.Join(ws, "evil.py"), []byte("import os\n"), 0o644)
	errs := validateGeneratedActionResources(ws, MemberOptimizationAction{}, []string{"evil.py"}, 300)
	require.Len(t, errs, 1)
	require.Contains(t, errs[0], "dangerous import 'os'")
	// 语法错误
	os.WriteFile(filepath.Join(ws, "bad.py"), []byte("def f(:\n"), 0o644)
	errs = validateGeneratedActionResources(ws, MemberOptimizationAction{}, []string{"bad.py"}, 300)
	require.Contains(t, errs[0], "python_compile:bad.py")
	// yaml 坏档
	os.WriteFile(filepath.Join(ws, "x.yaml"), []byte("a: [1\n"), 0o644)
	errs = validateGeneratedActionResources(ws, MemberOptimizationAction{}, []string{"x.yaml"}, 300)
	require.Contains(t, errs[0], "yaml_parse:x.yaml")
}

func TestValidatePackagePythonSource_gopapy扫描(t *testing.T) {
	// 危险导入
	errs := validatePackagePythonSource("import os\nimport requests\n", "a.py")
	require.Contains(t, errs[0], "dangerous import 'os'")
	// from 导入
	errs = validatePackagePythonSource("from subprocess import run\n", "a.py")
	require.Len(t, errs, 1)
	// 危险调用
	errs = validatePackagePythonSource("eval('1')\n", "a.py")
	require.Contains(t, errs[0], "dangerous call 'eval'")
	// builtins 前缀
	errs = validatePackagePythonSource("builtins.eval('1')\n", "a.py")
	require.Contains(t, errs[0], "dangerous call 'eval'")
	// 普通库 API 不拒绝（re.compile）
	require.Empty(t, validatePackagePythonSource("import re\nre.compile('x')\n", "a.py"))
	// os.system 属性调用：Attribute 值非 builtins → _call_name 返回空 → 不计调用错误
	// （Python 同语义：os.system 在无 import 场景下 0 错误）
	require.Empty(t, validatePackagePythonSource("os.system('ls')\n", "a.py"))
	// 语法错误
	require.Len(t, validatePackagePythonSource("def f(:\n", "a.py"), 1)
}

func TestActionBundleKey(t *testing.T) {
	action := MemberOptimizationAction{Role: "solver", AttributedIssueIDs: []string{"i1"}}
	key, ok := ActionBundleKey(action)
	require.True(t, ok)
	require.Equal(t, "solver", key[0])
	require.Equal(t, "i1", key[1])
	// 多 issue → false
	_, ok = ActionBundleKey(MemberOptimizationAction{Role: "r", AttributedIssueIDs: []string{"i1", "i2"}})
	require.False(t, ok)
	// 空 role → false
	_, ok = ActionBundleKey(MemberOptimizationAction{AttributedIssueIDs: []string{"i1"}})
	require.False(t, ok)
}

func TestEvaluateRoleExecution_回退语义(t *testing.T) {
	plan := &MemberOptimizationPlan{
		Actions: []MemberOptimizationAction{
			{ActionID: "primary", Role: "solver", AttributedIssueIDs: []string{"i1"}},
			{ActionID: "fallback", Role: "solver", RunIf: "dependency_failed", DependsOn: []string{"primary"}, AttributedIssueIDs: []string{"i1"}},
		},
	}
	// 主动作失败 + 回退成功 → 主动作 satisfied
	results := []*MemberActionExecutionResult{
		{ActionID: "primary", Status: "failed", MergeStatus: "merged", Error: "boom"},
		{ActionID: "fallback", Status: "succeeded", MergeStatus: "merged"},
	}
	outcomes := EvaluateRoleExecution(plan, results, "solver")
	require.True(t, outcomes["primary"].Satisfied)
	require.Contains(t, outcomes["primary"].Reason, "failed primary action replaced by successful fallback(s): [fallback]")
	require.True(t, outcomes["fallback"].Satisfied)
	// 主成功 + 回退跳过 → 回退 satisfied（依赖成功无需回退）
	results = []*MemberActionExecutionResult{
		{ActionID: "primary", Status: "succeeded", MergeStatus: "merged"},
		{ActionID: "fallback", Status: "skipped"},
	}
	outcomes = EvaluateRoleExecution(plan, results, "solver")
	require.True(t, outcomes["primary"].Satisfied)
	require.True(t, outcomes["fallback"].Satisfied)
	require.Equal(t, "fallback not needed because dependencies succeeded", outcomes["fallback"].Reason)
	// 缺结果
	outcomes = EvaluateRoleExecution(plan, nil, "solver")
	require.False(t, outcomes["primary"].Satisfied)
	require.Equal(t, "missing execution result", outcomes["primary"].Reason)
	// 角色错误清单
	errs := RoleExecutionErrors(plan, nil, "ghost-role")
	require.Equal(t, []string{"role has no planned actions"}, errs)
}

func TestSnakeCase(t *testing.T) {
	require.Equal(t, "calc_tool", snakeCase("CalcTool"))
	require.Equal(t, "a", snakeCase("A"))
	require.Equal(t, "member_optimizer_tool", snakeCase(""))
}

func TestBuildExecutorUserMessage_模板段(t *testing.T) {
	action := mkActionStruct()
	action.Constraints = map[string]any{"optimization_contracts": []any{
		map[string]any{"required_behavior": "X", "public_trigger": []any{map[string]any{"task": "t"}}},
	}}
	msg := BuildExecutorUserMessage(*action, "PLAN SUMMARY", []string{"s"}, nil, "/tmp/ws")
	for _, want := range []string{
		"## Action to Execute",
		"Action ID: a1",
		"## Public Runtime Contract",
		"## Current Declared File Contents",
		"## Supported Host Contracts (read-only)",
		"## Action-Specific Guidance",
		"## Constraints",
		"## Output",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("user message 缺段: %s", want)
		}
	}
	// 无 contract → Requested Change 段
	action2 := mkActionStruct()
	action2.Constraints = map[string]any{}
	msg2 := BuildExecutorUserMessage(*action2, "PLAN", nil, nil, "")
	require.Contains(t, msg2, "## Requested Change")
	require.Contains(t, msg2, "## Plan Summary\n\nPLAN")
}

func TestParseExecutorActionResponse(t *testing.T) {
	_, err := parseExecutorActionResponse("not json")
	require.Error(t, err)
	_, err = parseExecutorActionResponse(`{"no_status": 1}`)
	require.Error(t, err)
	_, err = parseExecutorActionResponse(`{"status": "bogus"}`)
	require.Error(t, err)
	_, err = parseExecutorActionResponse(`{"status": "succeeded", "errors": "bad"}`)
	require.Error(t, err)
	parsed, err := parseExecutorActionResponse(`{"status": "succeeded", "errors": []}`)
	require.NoError(t, err)
	require.Equal(t, "succeeded", parsed["status"])
}

func TestApplyStructuredFileWrites(t *testing.T) {
	ws := t.TempDir()
	declared := []string{"pkg/prompt_sections/files/x.md"}
	action := MemberOptimizationAction{ActionGroup: "prompt", Operation: "add", TargetPath: "pkg/prompt_sections/files/x.md"}
	// 合法写入
	written, err := applyStructuredFileWrites(ws, action, declared, []any{
		map[string]any{"path": "pkg/prompt_sections/files/x.md", "content": "hello"},
	})
	require.NoError(t, err)
	require.Equal(t, []string{"pkg/prompt_sections/files/x.md"}, written)
	require.FileExists(t, filepath.Join(ws, "pkg", "prompt_sections", "files", "x.md"))
	// 越界路径拒绝
	_, err = applyStructuredFileWrites(ws, action, declared, []any{
		map[string]any{"path": "other/y.md", "content": "x"},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "outside declared_write_paths")
	// 绝对路径拒绝
	_, err = applyStructuredFileWrites(ws, action, declared, []any{
		map[string]any{"path": "/abs/x.md", "content": "x"},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "safe relative path")
	// 缺 content
	_, err = applyStructuredFileWrites(ws, action, declared, []any{
		map[string]any{"path": "pkg/prompt_sections/files/x.md"},
	})
	require.Error(t, err)
}

func TestMergeSubwave_合并与登记簿同步(t *testing.T) {
	runDir := t.TempDir()
	integration := t.TempDir()
	actionWorktree := t.TempDir()
	os.MkdirAll(filepath.Join(actionWorktree, "prompt_sections", "files"), 0o755)
	os.WriteFile(filepath.Join(actionWorktree, "prompt_sections", "files", "new.md"), []byte("c"), 0o644)
	result := &MemberActionExecutionResult{
		ActionID: "a1", Role: "solver", Status: "succeeded",
		WorktreePath: actionWorktree,
		ChangedFiles: []string{"prompt_sections/files/new.md"},
	}
	merge := MergeSubwave(subwaveMergeRequest{
		role: "solver", waveIndex: 1, subwaveIndex: 0,
		results:        []*MemberActionExecutionResult{result},
		integrationDir: integration, runDir: runDir,
	})
	require.Equal(t, "merged", merge.Status)
	require.Equal(t, []string{"prompt_sections/files/new.md"}, merge.MergedFiles)
	require.FileExists(t, filepath.Join(integration, "prompt_sections", "files", "new.md"))
	require.FileExists(t, merge.ArtifactPath)
}

func TestIntOrDefault(t *testing.T) {
	require.Equal(t, 30, intOrDefault(30, 10))
	require.Equal(t, 30, intOrDefault(30.0, 10))
	require.Equal(t, 10, intOrDefault(true, 10))
	require.Equal(t, 10, intOrDefault("bad", 10))
	require.Equal(t, 42, intOrDefault("42", 10))
}
