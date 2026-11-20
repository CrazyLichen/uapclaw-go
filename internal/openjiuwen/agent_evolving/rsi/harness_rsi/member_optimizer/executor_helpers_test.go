package memberoptimizer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	harnessrsi "github.com/uapclaw/uapclaw-go/internal/openjiuwen/agent_evolving/rsi/harness_rsi"
)

func TestValidatePackageRegistries_冲突与合法(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "skills", "foo"), 0o755)
	os.WriteFile(filepath.Join(root, "skills", "foo", "SKILL.md"), []byte("s"), 0o644)
	// 合法登记
	os.WriteFile(filepath.Join(root, "skills", "skills.yaml"), []byte("skills:\n  - skills/foo\n"), 0o644)
	require.Empty(t, ValidatePackageRegistries(root))
	// 冲突登记（同 dir 不同选项）
	os.WriteFile(filepath.Join(root, "skills", "skills.yaml"),
		[]byte("skills:\n  - skills/foo\n  - {dir: skills/foo, priority: 5}\n"), 0o644)
	issues := ValidatePackageRegistries(root)
	require.NotEmpty(t, issues)
	require.Equal(t, "registry_conflict", issues[0].Code)
}

func TestNormalizePackageRegistries_事务性修复(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "skills", "foo"), 0o755)
	os.WriteFile(filepath.Join(root, "skills", "foo", "SKILL.md"), []byte("s"), 0o644)
	// 别名形态登记（file_path 别名）→ 归一重写
	os.WriteFile(filepath.Join(root, "skills", "skills.yaml"), []byte("skills:\n  - {dir: skills/foo, file_path: skills/foo}\n"), 0o644)
	report := NormalizePackageRegistries(root, []string{"skills/skills.yaml"})
	require.Empty(t, report.Issues)
	require.Equal(t, []string{"skills/skills.yaml"}, report.ChangedFiles)
	require.NotEmpty(t, report.Changes)
	// 不可写路径 → 跳过
	report = NormalizePackageRegistries(root, []string{"other/x.yaml"})
	require.Empty(t, report.ChangedFiles)
	// 坏档 → issue
	os.WriteFile(filepath.Join(root, "skills", "skills.yaml"), []byte("skills: not-a-list\n"), 0o644)
	report = NormalizePackageRegistries(root, []string{"skills/skills.yaml"})
	require.Len(t, report.Issues, 1)
	require.Equal(t, "skills must be a list", report.Issues[0].Message)
}

func TestUpsertPromptSection_别名去重(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "prompt_sections", "files"), 0o755)
	os.WriteFile(filepath.Join(root, "prompt_sections", "files", "x.md"), []byte("c"), 0o644)
	// 未声明 manifest → 拒绝
	err := UpsertPromptSection(root, "x", "prompt_sections/files/x.md", nil, []string{"other"}, false)
	require.Error(t, err)
	require.Contains(t, err.Error(), "prompt action must declare")
	// 首次插入
	require.NoError(t, UpsertPromptSection(root, "x", "prompt_sections/files/x.md", nil, []string{"prompt_sections/sections.yaml"}, false))
	data, _ := os.ReadFile(filepath.Join(root, "prompt_sections", "sections.yaml"))
	require.Contains(t, string(data), "name: x")
	// 同名同 target 再插 → 幂等
	require.NoError(t, UpsertPromptSection(root, "x", "prompt_sections/files/x.md", nil, []string{"prompt_sections/sections.yaml"}, false))
	// 同名不同 target 不替换 → duplicate（y.md 须真实存在——resolve 先于查重）
	os.WriteFile(filepath.Join(root, "prompt_sections", "files", "y.md"), []byte("c2"), 0o644)
	err = UpsertPromptSection(root, "x", "prompt_sections/files/y.md", nil, []string{"prompt_sections/sections.yaml"}, false)
	require.Error(t, err)
	require.Contains(t, err.Error(), "prompt_section_duplicate")
	// 替换模式
	require.NoError(t, UpsertPromptSection(root, "x", "prompt_sections/files/y.md", nil, []string{"prompt_sections/sections.yaml"}, true))
	data, _ = os.ReadFile(filepath.Join(root, "prompt_sections", "sections.yaml"))
	require.Contains(t, string(data), "files/y.md")
}

func TestRegistryRelative_安全面(t *testing.T) {
	root := t.TempDir()
	// 转义包根
	_, err := registryRelative(root, "sub/../../outside")
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsafe package-relative reference")
}

func TestRegistryPath_符号链接拒绝(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "skills"), 0o755)
	os.Symlink(filepath.Join(root, "skills"), filepath.Join(root, "skills", "link"))
	_, err := registryPath(root, "skills/link/skills.yaml")
	require.Error(t, err)
	require.Contains(t, err.Error(), "cannot follow symbolic links")
}

func TestReadRuntimePolicy_配置校验(t *testing.T) {
	root := t.TempDir()
	// 无 runtime.yaml → 默认
	policy, err := harnessrsi.ReadRuntimePolicy(root, 300, "error", 300)
	require.NoError(t, err)
	require.Equal(t, 300, policy.MaxIterations)
	// 合法 runtime.yaml
	os.WriteFile(filepath.Join(root, "runtime.yaml"), []byte("max_iterations: 50\non_max_iterations: error\n"), 0o644)
	policy, err = harnessrsi.ReadRuntimePolicy(root, 300, "error", 300)
	require.NoError(t, err)
	require.Equal(t, 50, policy.MaxIterations)
	// 超限
	os.WriteFile(filepath.Join(root, "runtime.yaml"), []byte("max_iterations: 9999\n"), 0o644)
	_, err = harnessrsi.ReadRuntimePolicy(root, 300, "error", 300)
	require.Error(t, err)
}

func TestExecuteDeterministicRemove_工具与prompt分支(t *testing.T) {
	ws := t.TempDir()
	// tool remove + manifest
	declared := []string{"tools/calc.py", "tools/tools.yaml"}
	os.MkdirAll(filepath.Join(ws, "tools"), 0o755)
	os.WriteFile(filepath.Join(ws, "tools", "calc.py"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(ws, "tools", "tools.yaml"), []byte("tools:\n  - {file: tools/calc.py, class_name: C}\n"), 0o644)
	action := MemberOptimizationAction{ActionGroup: "tool", Operation: "remove", TargetPath: "tools/calc.py"}
	result := executeDeterministicRemove(ws, action, declared)
	require.Equal(t, "succeeded", result["status"])
	remove := result["remove"].(map[string]any)
	require.Equal(t, []any{"tools/calc.py"}, remove["removed_paths"])
	require.Equal(t, []any{"tools/tools.yaml"}, remove["manifest_updates"])
	// prompt remove + sections.yaml 条目删除
	ws2 := t.TempDir()
	declared2 := []string{"prompt_sections/files/x.md", "prompt_sections/sections.yaml"}
	os.MkdirAll(filepath.Join(ws2, "prompt_sections", "files"), 0o755)
	os.WriteFile(filepath.Join(ws2, "prompt_sections", "files", "x.md"), []byte("c"), 0o644)
	os.WriteFile(filepath.Join(ws2, "prompt_sections", "sections.yaml"),
		[]byte("sections:\n  - {name: x, file: prompt_sections/files/x.md}\n"), 0o644)
	action2 := MemberOptimizationAction{ActionGroup: "prompt", Operation: "remove",
		TargetPath: "prompt_sections/files/x.md", Constraints: map[string]any{"section_name": "x"}}
	result = executeDeterministicRemove(ws2, action2, declared2)
	require.Equal(t, "succeeded", result["status"])
	data, _ := os.ReadFile(filepath.Join(ws2, "prompt_sections", "sections.yaml"))
	require.NotContains(t, string(data), "name: x")
	// 什么都没删 → nothing removed
	ws3 := t.TempDir()
	action3 := MemberOptimizationAction{ActionGroup: "config", Operation: "remove", TargetPath: "runtime.yaml"}
	result = executeDeterministicRemove(ws3, action3, []string{"runtime.yaml"})
	require.Contains(t, result["error"], "nothing removed")
}

func TestSkillRootFromTarget(t *testing.T) {
	require.Equal(t, "skills/foo", skillRootFromTarget("skills/foo/SKILL.md"))
	require.Equal(t, "skills/foo", skillRootFromTarget("skills/foo"))
	require.Equal(t, "identity.md", skillRootFromTarget("identity.md"))
}

func TestManifestPathMatches_别名规则(t *testing.T) {
	require.True(t, manifestPathMatches("skills/foo", "skills/foo", false))
	require.True(t, manifestPathMatches("foo.md", "prompt_sections/files/foo.md", false)) // prompt files 别名
	require.False(t, manifestPathMatches("skills", "skills/foo", true))                   // exact_only
	require.True(t, manifestPathMatches("skills", "skills/foo", false))                   // 目录包含
}

func TestRemovePath_空父目录回收(t *testing.T) {
	ws := t.TempDir()
	os.MkdirAll(filepath.Join(ws, "a", "b"), 0o755)
	os.WriteFile(filepath.Join(ws, "a", "b", "f.txt"), []byte("x"), 0o644)
	removed := removePath(ws, "a/b/f.txt")
	require.Equal(t, []string{"a/b/f.txt"}, removed)
	// 空父目录已回收
	require.NoDirExists(t, filepath.Join(ws, "a", "b"))
	require.DirExists(t, ws)
}

func TestSnakeCaseAndTokens(t *testing.T) {
	require.Equal(t, "member_optimizer_tool", snakeCase("__"))
	require.NotEqual(t, "", shortToken("x"))
	require.Equal(t, shortToken(""), shortToken("default"))
}

func TestRegistryIssueDescribe(t *testing.T) {
	issue := RegistryIssue{Code: "c", Manifest: "m.yaml", Index: 3, Message: "bad"}
	require.Equal(t, "c: m.yaml[3]: bad", issue.Describe())
}

func TestLoadMemberOptimizerModel_父包版(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "model.yaml")
	os.WriteFile(path, []byte("model:\n  model_client_config:\n    client_id: c\n    client_provider: llm_OpenAI\n    model_name: m\n    api_key: k\n    base_url: http://x\n  model_request_config:\n    max_tokens: 512\n"), 0o644)
	model, err := LoadMemberOptimizerModel(path)
	require.NoError(t, err)
	require.NotNil(t, model)
	// 空引用 / 缺文件 / 坏映射
	_, err = LoadMemberOptimizerModel("")
	require.Contains(t, err.Error(), "model_config_ref is required")
	_, err = LoadMemberOptimizerModel(filepath.Join(dir, "no.yaml"))
	require.Contains(t, err.Error(), "not found")
	bad := filepath.Join(dir, "bad.yaml")
	os.WriteFile(bad, []byte("- a\n- b\n"), 0o644)
	_, err = LoadMemberOptimizerModel(bad)
	// Go yaml 阶段即拒序列（Python yaml.safe_load 得 list → model_data 非 dict 报 mapping 错误；
	// Go 错误文案等价点在 yaml 层——语义同为「顶层须映射」）
	require.Error(t, err)
	// 缺 client 配置
	bad2 := filepath.Join(dir, "bad2.yaml")
	os.WriteFile(bad2, []byte("model:\n  model_request_config: {}\n"), 0o644)
	_, err = LoadMemberOptimizerModel(bad2)
	require.Contains(t, err.Error(), "model_client_config")
}

func TestBuildDeclaredFileContext_分支(t *testing.T) {
	// 无 worktree
	require.Equal(t, "No action worktree was provided.", BuildDeclaredFileContext("", nil, 0))
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "d"), 0o755)
	// 空目录
	got := BuildDeclaredFileContext(root, []string{"d"}, 100)
	require.Contains(t, got, "Directory exists and is empty.")
	// 不存在文件
	got = BuildDeclaredFileContext(root, []string{"missing.md"}, 100)
	require.Contains(t, got, "File does not exist.")
	// 越界
	got = BuildDeclaredFileContext(root, []string{"../x"}, 100)
	require.Contains(t, got, "Path is outside the action worktree")
	// 文件内容截断
	os.WriteFile(filepath.Join(root, "big.md"), []byte(strings.Repeat("x", 300)), 0o644)
	got = BuildDeclaredFileContext(root, []string{"big.md"}, 100)
	require.Contains(t, got, "[content truncated]")
}

func TestBuildExecutorRetryMessage_文案(t *testing.T) {
	msg := BuildExecutorRetryMessage("ORIG", "PREV", "ERR")
	require.Contains(t, msg, "## Previous Invalid Output\n\nPREV")
	require.Contains(t, msg, "## Validation Error\n\nERR")
	require.Contains(t, msg, "the controller verifies real writes")
}

func TestIsAddLikeScaffoldAction_modify缺失目标(t *testing.T) {
	ws := t.TempDir()
	// tool modify 目标不存在 → true
	require.True(t, isAddLikeScaffoldAction(ws, MemberOptimizationAction{
		ActionGroup: "tool", Operation: "modify", TargetPath: "tools/new.py",
	}))
	// 目标存在 → false
	os.MkdirAll(filepath.Join(ws, "tools"), 0o755)
	os.WriteFile(filepath.Join(ws, "tools", "new.py"), []byte("x"), 0o644)
	require.False(t, isAddLikeScaffoldAction(ws, MemberOptimizationAction{
		ActionGroup: "tool", Operation: "modify", TargetPath: "tools/new.py",
	}))
}

func TestEmptyPyValueBranches(t *testing.T) {
	require.True(t, isEmptyPyValue(nil))
	require.True(t, isEmptyPyValue(""))
	require.True(t, isEmptyPyValue([]any{}))
	require.True(t, isEmptyPyValue(map[string]any{}))
	require.False(t, isEmptyPyValue(0))
	require.False(t, isEmptyPyValue("x"))
}

func TestNewMemberActionPlannerAgent_默认构造(t *testing.T) {
	agent := NewMemberActionPlannerAgent("ref", 2, "ws", []string{"s"})
	require.NotNil(t, agent)
	require.Equal(t, "ref", agent.modelConfigRef)
	require.Equal(t, 2, agent.retryLimit)
}

func TestBridge快照替换_仅父包(t *testing.T) {
	// NewMemberActionPlannerAgent 默认构造桥（无 agents init）——父包自持桥字段
	require.NotNil(t, NewMemberActionPlannerAgent("r", 1, "", nil))
}
