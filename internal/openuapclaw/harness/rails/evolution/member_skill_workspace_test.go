package evolution

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEnsureMemberSkillCopy 对齐 member_skill_workspace.py:19-72 全分支。
func TestEnsureMemberSkillCopy(t *testing.T) {
	global := filepath.Join(t.TempDir(), "global")
	member := filepath.Join(t.TempDir(), "member")
	skillDir := filepath.Join(global, "demo-skill")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# demo"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "notes.txt"), []byte("body"), 0o644))

	// 非法名 → ValueError 语义
	_, err := EnsureMemberSkillCopy(member, global, "../escape")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsafe Skill name")
	_, err = EnsureMemberSkillCopy(member, global, "")
	require.Error(t, err)

	// 全局库无 SKILL.md → FileNotFoundError 语义
	bad := filepath.Join(global, "empty-skill")
	require.NoError(t, os.MkdirAll(bad, 0o755))
	_, err = EnsureMemberSkillCopy(member, global, "empty-skill")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not exist")

	// 正常拷贝
	dest, err := EnsureMemberSkillCopy(member, global, "demo-skill")
	require.NoError(t, err)
	assert.True(t, filepath.IsAbs(dest))
	data, rerr := os.ReadFile(filepath.Join(dest, "notes.txt"))
	require.NoError(t, rerr)
	assert.Equal(t, "body", string(data))
	// 成员拷贝不得是全局目录的 symlink
	fi, lerr := os.Lstat(dest)
	require.NoError(t, lerr)
	assert.Equal(t, os.ModeDir, fi.Mode()&os.ModeDir, "应为真实目录而非链接")

	// 已有真实成员目录 → 原样保留
	require.NoError(t, os.WriteFile(filepath.Join(dest, "local-edit.txt"), []byte("mine"), 0o644))
	dest2, err := EnsureMemberSkillCopy(member, global, "demo-skill")
	require.NoError(t, err)
	assert.Equal(t, dest, dest2)
	_, err = os.Stat(filepath.Join(dest2, "local-edit.txt"))
	assert.NoError(t, err, "已有成员目录内容应保留")

	// symlink 指向全局库 → 原子替换为真实拷贝
	linkSkill := "link-skill"
	src := filepath.Join(global, linkSkill)
	require.NoError(t, os.MkdirAll(src, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(src, "SKILL.md"), []byte("# link"), 0o644))
	member2 := filepath.Join(t.TempDir(), "member2")
	require.NoError(t, os.MkdirAll(member2, 0o755))
	require.NoError(t, os.Symlink(src, filepath.Join(member2, linkSkill)))
	dest3, err := EnsureMemberSkillCopy(member2, global, linkSkill)
	require.NoError(t, err)
	fi3, lerr := os.Lstat(dest3)
	require.NoError(t, lerr)
	assert.Equal(t, os.FileMode(0), fi3.Mode()&os.ModeSymlink, "symlink 应被替换为真实拷贝")
}
