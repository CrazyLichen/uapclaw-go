package repl

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSlashCommands_表与隐藏别名 对齐 repl.py:656-678。
func TestSlashCommands_表与隐藏别名(t *testing.T) {
	for _, name := range []string{"/help", "/exit", "/quit", "/clear",
		"/status", "/cost", "/compact", "/sessions", "/auto-harness"} {
		assert.True(t, IsSlashCommand(name), name)
	}
	// /quit 是隐藏别名：命令存在但描述省略
	assert.True(t, IsSlashCommand("/quit"))
	_, hasQuit := SlashDescriptions["/quit"]
	assert.False(t, hasQuit, "/quit 刻意省略描述")
	// 未注册的 slash 不是命令
	assert.False(t, IsSlashCommand("/nope"))
	assert.False(t, IsSlashCommand("hello"))
}

// TestSkillCommands_注册与隔离 动态技能命令。
func TestSkillCommands_注册与隔离(t *testing.T) {
	ResetSkillCommands()
	RegisterSkillCommand("/my-skill", "做某事")
	assert.True(t, IsSlashCommand("/my-skill"))
	assert.Equal(t, "做某事", SkillCommands()["/my-skill"])
	ResetSkillCommands()
	assert.False(t, IsSlashCommand("/my-skill"))
}

// TestAnsweredAsTyped 对齐 repl.py:960-966。
func TestAnsweredAsTyped(t *testing.T) {
	assert.False(t, AnsweredAsTyped("confirm"), "审批类有固定形态")
	assert.False(t, AnsweredAsTyped("ask_user"), "ask_user 有交互框")
	assert.True(t, AnsweredAsTyped("free_form"), "其余自由打字")
}

// TestReadSkillDescription_取首段正文 对齐 repl.py:752-777。
func TestReadSkillDescription_取首段正文(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "SKILL.md")
	require.NoError(t, os.WriteFile(path, []byte(`---
name: demo
---

# Demo Skill

做演示用的技能。

细节段落。
`), 0o644))
	assert.Equal(t, "做演示用的技能。", ReadSkillDescription(path))
	// 缺文件
	assert.Equal(t, "", ReadSkillDescription(filepath.Join(dir, "nope.md")))
}

// TestScanSkillDirs_注册与非存在跳过 对齐 repl.py:778-820。
func TestScanSkillDirs_注册与非存在跳过(t *testing.T) {
	ResetSkillCommands()
	root := t.TempDir()
	skillsDir := filepath.Join(root, "skills")
	require.NoError(t, os.MkdirAll(filepath.Join(skillsDir, "demo"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(skillsDir, "demo", "SKILL.md"),
		[]byte("# Demo\n\n技能描述"), 0o644))
	// 无 SKILL.md 的子目录不注册
	require.NoError(t, os.MkdirAll(filepath.Join(skillsDir, "empty"), 0o755))

	count := ScanSkillDirs([]string{skillsDir, filepath.Join(root, "missing")})
	assert.Equal(t, 1, count)
	assert.True(t, IsSlashCommand("/demo"))
	assert.Equal(t, "技能描述", SkillCommands()["/demo"])
	ResetSkillCommands()
}

// TestSortedSkillNames_内置加技能 排序合并。
func TestSortedSkillNames_内置加技能(t *testing.T) {
	ResetSkillCommands()
	RegisterSkillCommand("/z-skill", "z")
	names := SortedSkillNames()
	require.NotEmpty(t, names)
	// 排序且含内置与技能
	found := map[string]bool{}
	for _, name := range names {
		found[name] = true
	}
	assert.True(t, found["/help"])
	assert.True(t, found["/z-skill"])
	for i := 1; i < len(names); i++ {
		require.Less(t, names[i-1], names[i], "应升序")
	}
	ResetSkillCommands()
}

// TestHandleShell_空命令与真实命令 对齐 repl.py:725-751。
func TestHandleShell_空命令与真实命令(t *testing.T) {
	require.ErrorContains(t, HandleShell("!"), "usage")
	require.ErrorContains(t, HandleShell("!  "), "usage")
	// 真实命令
	require.NoError(t, HandleShell("!true"))
	// 退出码非 0 → 错误
	require.Error(t, HandleShell("!false"))
}

// TestScanSkillDirs_波浪号路径 ~ 展开的注册。
func TestScanSkillDirs_波浪号路径(t *testing.T) {
	ResetSkillCommands()
	home := t.TempDir()
	t.Setenv("HOME", home)
	skillsDir := filepath.Join(home, ".openjiuwen", "workspace", "skills")
	require.NoError(t, os.MkdirAll(filepath.Join(skillsDir, "波浪技能"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(skillsDir, "波浪技能", "SKILL.md"),
		[]byte("# 波浪\n\n波浪描述"), 0o644))

	count := ScanSkillDirs([]string{"~/.openjiuwen/workspace/skills"})
	assert.Equal(t, 1, count)
	assert.True(t, IsSlashCommand("/波浪技能"))
	ResetSkillCommands()
}
