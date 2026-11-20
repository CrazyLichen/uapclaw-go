package memberoptimizer

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRoleWorktreePath_哈希目录(t *testing.T) {
	path := RoleWorktreePath(t.TempDir(), "solver")
	require.Len(t, filepath.Base(path), 8) // sha1 前 8 位
	// 同角色同路径、异角色异路径
	require.Equal(t, path, RoleWorktreePath(filepath.Dir(path), "solver"))
	require.NotEqual(t, filepath.Base(path), filepath.Base(RoleWorktreePath(t.TempDir(), "reviewer")))
}

func TestValidateWorktreePath_禁写与越界(t *testing.T) {
	root := t.TempDir()
	require.True(t, ValidateWorktreePath("identity.md", root))
	require.True(t, ValidateWorktreePath("skills/x/SKILL.md", root))
	require.False(t, ValidateWorktreePath("", root))
	require.False(t, ValidateWorktreePath("current_harnesses/solver", root), "禁写共享产物目录")
	require.False(t, ValidateWorktreePath("current_harness_refs.yaml", root))
	require.False(t, ValidateWorktreePath(".publish_tmp/x", root))
	require.False(t, ValidateWorktreePath("../escape", root), "越界")
}

func TestPrepareIntegrationWorktree_拷贝与登记簿播种(t *testing.T) {
	worktreesDir := t.TempDir()
	// 源 harness：identity.md + expert_harness.yaml（含 prompt_sections 声明）
	source := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(source, "identity.md"), []byte("# identity"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(source, "expert_harness.yaml"), []byte(
		"schema_version: expert_harness.v1\nprompt_sections:\n  - file: prompt_sections/files/extra.md\n"), 0o644))
	sectionsDir := filepath.Join(source, "prompt_sections", "files")
	require.NoError(t, os.MkdirAll(sectionsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(sectionsDir, "extra.md"), []byte("content"), 0o644))

	integration, err := PrepareIntegrationWorktree("solver", source, worktreesDir)
	require.NoError(t, err)
	// 目录形态 wt/{role_hash}/i
	require.True(t, withinDir(worktreesDir, integration))
	require.Equal(t, "i", filepath.Base(integration))
	// 拷贝生效
	require.FileExists(t, filepath.Join(integration, "identity.md"))
	// 登记簿播种：sections.yaml 生成且 name 回落 file stem
	data, err := os.ReadFile(filepath.Join(integration, "prompt_sections", "sections.yaml"))
	require.NoError(t, err)
	require.Contains(t, string(data), "sections:")
	require.Contains(t, string(data), "name: extra")
	// manifest 降级为 1.0 并移除声明
	manifestData, err := os.ReadFile(filepath.Join(integration, "expert_harness.yaml"))
	require.NoError(t, err)
	require.Contains(t, string(manifestData), `schema_version: "1.0"`)
	require.NotContains(t, string(manifestData), "prompt_sections:")
	// roles.yaml 映射
	mapping, err := os.ReadFile(filepath.Join(worktreesDir, "roles.yaml"))
	require.NoError(t, err)
	require.Contains(t, string(mapping), "solver:")
}

func TestPrepareIntegrationWorktree_无manifest(t *testing.T) {
	worktreesDir := t.TempDir()
	source := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(source, "a.md"), []byte("x"), 0o644))
	integration, err := PrepareIntegrationWorktree("solver", source, worktreesDir)
	require.NoError(t, err)
	require.FileExists(t, filepath.Join(integration, "a.md"))
}

func TestPrepareIntegrationWorktree_空引用(t *testing.T) {
	integration, err := PrepareIntegrationWorktree("solver", "", t.TempDir())
	require.NoError(t, err)
	require.NotEmpty(t, integration)
}

func TestPrepareActionWorktree_快照与禁写拒绝(t *testing.T) {
	worktreesDir := t.TempDir()
	source := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(source, "identity.md"), []byte("v1"), 0o644))
	integration, err := PrepareIntegrationWorktree("solver", source, worktreesDir)
	require.NoError(t, err)

	action := MemberOptimizationAction{ActionID: "act1", Role: "solver", TargetPath: "skills/new/SKILL.md"}
	actionWorktree, err := PrepareActionWorktree(action, integration, worktreesDir, 2)
	require.NoError(t, err)
	// 快照内容存在
	require.FileExists(t, filepath.Join(actionWorktree, "identity.md"))
	// 路径形态 a/{wave:x}/{action_hash}
	require.Contains(t, filepath.ToSlash(actionWorktree), "/a/2/")
	// 禁写 target_path 拒绝
	forbidden := MemberOptimizationAction{ActionID: "act2", Role: "solver", TargetPath: "current_harnesses/x"}
	_, err = PrepareActionWorktree(forbidden, integration, worktreesDir, 2)
	require.Error(t, err)
	require.Contains(t, err.Error(), "outside role worktree or references forbidden paths")
}

func TestResolveIntegrationWorktreePath_legacy回退(t *testing.T) {
	dir := t.TempDir()
	// 均不存在 → 短路径
	got := ResolveIntegrationWorktreePath(dir, "solver")
	require.Equal(t, IntegrationWorktreePath(dir, "solver"), got)
	// legacy 存在 → 回退
	legacy := filepath.Join(LegacyRoleWorktreePath(dir, "reviewer"), "integration")
	require.NoError(t, os.MkdirAll(legacy, 0o755))
	got = ResolveIntegrationWorktreePath(dir, "reviewer")
	require.Equal(t, legacy, got)
}

func TestRegistryRelative_安全校验(t *testing.T) {
	root := t.TempDir()
	_, err := registryRelative(root, "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "non-empty")
	_, err = registryRelative(root, "../x")
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsafe package-relative reference")
	_, err = registryRelative(root, "C:\\x")
	require.Error(t, err)
	_, err = registryRelative(root, "/abs")
	require.Error(t, err)
	resolved, err := registryRelative(root, "skills/foo")
	require.NoError(t, err)
	require.True(t, withinDir(root, resolved))
}

func TestResolveRegistryResource_别名解析(t *testing.T) {
	root := t.TempDir()
	// prompt: files 别名剥离
	filesDir := filepath.Join(root, "prompt_sections", "files")
	require.NoError(t, os.MkdirAll(filesDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(filesDir, "x.md"), []byte("c"), 0o644))
	resolved, err := resolveRegistryResource(root, "prompt_sections/files/x.md", false)
	require.NoError(t, err)
	require.Equal(t, "prompt_sections/files/x.md", resolved)
	// 别名 files/x.md 同样可达（单一现存目标）
	resolved, err = resolveRegistryResource(root, "files/x.md", false)
	require.NoError(t, err)
	require.Equal(t, "prompt_sections/files/x.md", resolved)
	// 技能：SKILL.md 上溯父目录
	skillDir := filepath.Join(root, "skills", "foo")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("s"), 0o644))
	resolved, err = resolveRegistryResource(root, "skills/foo", true)
	require.NoError(t, err)
	require.Equal(t, "skills/foo", resolved)
	// 技能挂载缺 SKILL.md
	emptyDir := filepath.Join(root, "skills", "empty")
	require.NoError(t, os.MkdirAll(emptyDir, 0o755))
	_, err = resolveRegistryResource(root, "skills/empty", true)
	require.Error(t, err)
	require.Contains(t, err.Error(), "no package-local SKILL.md")
	// 双现存目标拒绝：raw="prompt_sections/x.md" 时 direct 与 files 别名各为一现存目标
	dup := filepath.Join(root, "prompt_sections", "x.md")
	require.NoError(t, os.WriteFile(dup, []byte("c"), 0o644))
	_, err = resolveRegistryResource(root, "prompt_sections/x.md", false)
	require.Error(t, err)
	require.Contains(t, err.Error(), "existing targets")
}

func TestExpandSkillMounts_父目录展开(t *testing.T) {
	root := t.TempDir()
	// skills/parent/{a,b}/SKILL.md
	for _, name := range []string{"a", "b"} {
		dir := filepath.Join(root, "skills", "parent", name)
		require.NoError(t, os.MkdirAll(dir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("s"), 0o644))
	}
	mounts, err := expandSkillMounts(root, []any{"skills/parent"})
	require.NoError(t, err)
	require.Len(t, mounts, 2)
	require.Equal(t, "skills/parent/a", mounts[0]["dir"])
	require.Equal(t, "skills/parent/b", mounts[1]["dir"])
	// dirs 列表展开
	mounts, err = expandSkillMounts(root, []any{map[string]any{"dirs": []any{"skills/parent"}, "priority": 5}})
	require.NoError(t, err)
	require.Len(t, mounts, 2)
	require.Equal(t, 5, mounts[0]["priority"])
	// 冲突选项拒绝
	_, err = expandSkillMounts(root, []any{"skills/parent", map[string]any{"dir": "skills/parent", "priority": 9}})
	require.Error(t, err)
	require.Contains(t, err.Error(), "conflicting options")
}

func TestSynchronizePluginManifest_JSON编译(t *testing.T) {
	pkg := t.TempDir()
	// JSON manifest + skills 登记
	require.NoError(t, os.WriteFile(filepath.Join(pkg, "manifest.json"), []byte(
		`{"name": "p", "skills": [], "prompt_sections": []}`), 0o644))
	skillsDir := filepath.Join(pkg, "skills", "foo")
	require.NoError(t, os.MkdirAll(skillsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(skillsDir, "SKILL.md"), []byte("s"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(pkg, "skills", "skills.yaml"), []byte("skills:\n  - skills/foo\n"), 0o644))
	require.NoError(t, SynchronizePluginManifest(pkg))
	// JSON 更新为叶子目录挂载
	data, err := os.ReadFile(filepath.Join(pkg, "manifest.json"))
	require.NoError(t, err)
	require.Contains(t, string(data), `"dir": "skills/foo"`)
	// 无 manifest → no-op
	require.NoError(t, SynchronizePluginManifest(t.TempDir()))
}
