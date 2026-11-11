package recommendation

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
)

// ──────────────────────────── DetectAllSkills ────────────────────────────

// TestDetectAllSkills_只返回已安装技能 内置未安装的剔除，候选池只剩 local。
// Python: proactive_actions.py:133 _get_all_skills
func TestDetectAllSkills_只返回已安装技能(t *testing.T) {
	resDir := t.TempDir()
	builtinDir := filepath.Join(resDir, "agent", "workspace", "skills")
	require.NoError(t, os.MkdirAll(builtinDir, 0o755))

	// 内置源里放一个「未安装」技能
	writeSkillDir(t, builtinDir, "builtin-only", "---\nname: builtin-only\ndescription: 内置未装\n---\n正文")

	// 用户目录（隔离 workspace）
	userSkills := t.TempDir()
	writeSkillDir(t, userSkills, "local-skill", "---\nname: local-skill\ndescription: 本地已装\n---\n正文")

	names, skills := detectAllSkillsIn(userSkills, builtinDir)

	assert.Contains(t, names, "local-skill", "已安装的本地技能应入候选池")
	assert.NotContains(t, names, "builtin-only", "内置未安装技能不应入候选池")

	for _, s := range skills {
		installed, _ := s["installed"].(bool)
		assert.True(t, installed, "候选池里必须全是已安装技能")
	}
}

// TestDetectAllSkills_跳过下划线目录 以 _ 开头的目录（_disabled_plugins/_marketplace）被跳过。
func TestDetectAllSkills_跳过下划线目录(t *testing.T) {
	resDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(resDir, "agent", "workspace", "skills"), 0o755))

	userSkills := t.TempDir()
	writeSkillDir(t, userSkills, "_hidden", "---\nname: hidden\ndescription: 隐藏\n---\n正文")
	writeSkillDir(t, userSkills, "visible", "---\nname: visible\ndescription: 可见\n---\n正文")

	names, _ := detectAllSkillsIn(userSkills, filepath.Join(resDir, "agent", "workspace", "skills"))
	assert.Contains(t, names, "visible")
	assert.NotContains(t, names, "hidden", "下划线开头目录应被跳过")
}

// TestDetectAllSkills_跳过无SKILLmd目录 目录里没有 SKILL.md 时不入候选池。
func TestDetectAllSkills_跳过无SKILLmd目录(t *testing.T) {
	resDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(resDir, "agent", "workspace", "skills"), 0o755))

	userSkills := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(userSkills, "no-md"), 0o755))
	writeSkillDir(t, userSkills, "has-md", "---\nname: has-md\ndescription: 有\n---\n正文")

	names, _ := detectAllSkillsIn(userSkills, filepath.Join(resDir, "agent", "workspace", "skills"))
	assert.Contains(t, names, "has-md")
	assert.NotContains(t, names, "no-md", "无 SKILL.md 的目录应被跳过")
}

// TestDetectAllSkills_同名去重 两个目录同名技能只保留先扫到的（local 优先）。
func TestDetectAllSkills_同名去重(t *testing.T) {
	resDir := t.TempDir()
	builtinDir := filepath.Join(resDir, "agent", "workspace", "skills")
	require.NoError(t, os.MkdirAll(builtinDir, 0o755))
	writeSkillDir(t, builtinDir, "dup", "---\nname: dup-skill\ndescription: 内置版\n---\n")

	userSkills := t.TempDir()
	writeSkillDir(t, userSkills, "dup", "---\nname: dup-skill\ndescription: 本地版\n---\n")

	_, skills := detectAllSkillsIn(userSkills, builtinDir)
	var matched []map[string]any
	for _, s := range skills {
		if s["name"] == "dup-skill" {
			matched = append(matched, s)
		}
	}
	require.Len(t, matched, 1, "同名技能应去重，只留一条")
	assert.Equal(t, "本地版", matched[0]["description"], "local 先扫，应保留本地版")
}

// TestDetectAllSkills_资源目录缺失 找不到 resources 时内置分支跳过，不 panic。
func TestDetectAllSkills_资源目录缺失(t *testing.T) {
	// 不应 panic
	names, skills := detectAllSkillsIn(t.TempDir(), "")
	assert.NotNil(t, names)
	assert.NotNil(t, skills)
}

// TestGetBuiltinSkillsDirForRecommendation 两级回退与 skill 包一致。
func TestGetBuiltinSkillsDirForRecommendation(t *testing.T) {
	t.Run("primary存在", func(t *testing.T) {
		resDir := t.TempDir()
		primary := filepath.Join(resDir, "agent", "workspace", "skills")
		require.NoError(t, os.MkdirAll(primary, 0o755))
		t.Setenv(pathutil.EnvResourcesDir, resDir)

		assert.Equal(t, primary, getBuiltinSkillsDirForRecommendation())
	})

	t.Run("primary缺失回落fallback", func(t *testing.T) {
		resDir := t.TempDir()
		t.Setenv(pathutil.EnvResourcesDir, resDir)

		want := filepath.Join(resDir, "agent", "skills")
		assert.Equal(t, want, getBuiltinSkillsDirForRecommendation())
	})

	t.Run("资源目录缺失返回空", func(t *testing.T) {
		t.Setenv(pathutil.EnvResourcesDir, filepath.Join(t.TempDir(), "not-exist"))
		assert.Empty(t, getBuiltinSkillsDirForRecommendation())
	})
}

// writeSkillDir 在 parent 下建 skillName 目录并写 SKILL.md。
func writeSkillDir(t *testing.T, parent, dirName, content string) {
	t.Helper()
	dir := filepath.Join(parent, dirName)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644))
}

// ──────────────────────────── SaveRecommendationState 异常分支 ────────────────────────────

// TestSaveRecommendationState_目录不可写 写入失败只告警不返回错误（Python 吞异常）。
func TestSaveRecommendationState_目录不可写(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root 用户不受文件权限限制，跳过")
	}
	dir := t.TempDir()
	require.NoError(t, os.Chmod(dir, 0o500))
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	// 不应 panic，也不应有返回值
	SaveRecommendationState(&RecommendationState{}, filepath.Join(dir, "sub", "state.json"))
}

// TestSaveRecommendationState_默认路径 空 path 走默认路径（不实际写用户目录，只验证不 panic）。
func TestDefaultRecommendationStatePath(t *testing.T) {
	path := DefaultRecommendationStatePath()
	assert.True(t, filepath.IsAbs(path), "默认路径应为绝对路径")
	assert.Equal(t, "recommendation.json", filepath.Base(path))
	assert.Contains(t, path, "agent")
}

// ──────────────────────────── parseSimpleYAMLMap ────────────────────────────

// TestParseSimpleYAMLMap 扁平结构成功；嵌套/列表结构降级为 false。
func TestParseSimpleYAMLMap(t *testing.T) {
	t.Run("扁平结构", func(t *testing.T) {
		got, ok := parseSimpleYAMLMap("name: my-skill\ndescription: 描述")
		require.True(t, ok)
		assert.Equal(t, "my-skill", got["name"])
		assert.Equal(t, "描述", got["description"])
	})

	t.Run("列表结构降级", func(t *testing.T) {
		_, ok := parseSimpleYAMLMap("name: x\ntags:\n  - a\n  - b")
		assert.False(t, ok, "列表结构应降级")
	})

	t.Run("缩进嵌套降级", func(t *testing.T) {
		_, ok := parseSimpleYAMLMap("outer:\n  inner: 1")
		assert.False(t, ok, "缩进嵌套应降级")
	})

	t.Run("空内容降级", func(t *testing.T) {
		_, ok := parseSimpleYAMLMap("")
		assert.False(t, ok)
	})

	t.Run("注释与空行被跳过", func(t *testing.T) {
		got, ok := parseSimpleYAMLMap("# 注释\n\nname: v\n")
		require.True(t, ok)
		assert.Equal(t, "v", got["name"])
	})
}
