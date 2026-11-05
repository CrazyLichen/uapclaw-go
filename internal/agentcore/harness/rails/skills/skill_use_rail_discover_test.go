package skills

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	skillpkg "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/skills"
	"github.com/uapclaw/uapclaw-go/internal/evolving/checkpointing"
)

// ──────────────────────────── 非导出函数 ────────────────────────────

// writeSkillMD 写一个 SKILL.md 并显式设置 mtime。
func writeSkillMD(t *testing.T, dir, body string, mtime time.Time) {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o755))
	path := filepath.Join(dir, "SKILL.md")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
	require.NoError(t, os.Chtimes(path, mtime, mtime))
}

// discoveredDirs 提取发现结果中的目录名（去掉根前缀），便于断言。
func discoveredDirs(entries []skillDirEntry, root string) []string {
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		rel, err := filepath.Rel(root, entry.Dir)
		if err != nil {
			rel = entry.Dir
		}
		names = append(names, rel)
	}
	return names
}

func TestDiscoverSkillDirs_递归发现分组目录下的技能(t *testing.T) {
	root := t.TempDir()
	now := time.Now()
	// 分组目录 lark 自身没有 SKILL.md，技能在下一层
	writeSkillMD(t, filepath.Join(root, "lark", "lark-doc"), "# doc", now)
	writeSkillMD(t, filepath.Join(root, "flat"), "# flat", now)

	entries := discoverSkillDirs([]string{root})
	assert.Equal(t, []string{"flat", filepath.Join("lark", "lark-doc")}, discoveredDirs(entries, root))
}

func TestDiscoverSkillDirs_分支遇首个SKILLMD即停止下钻(t *testing.T) {
	root := t.TempDir()
	now := time.Now()
	writeSkillMD(t, filepath.Join(root, "parent"), "# parent", now)
	writeSkillMD(t, filepath.Join(root, "parent", "nested"), "# nested", now)

	entries := discoverSkillDirs([]string{root})
	assert.Equal(t, []string{"parent"}, discoveredDirs(entries, root), "子技能由 skill_tool 披露，不注册为顶层技能")
}

func TestDiscoverSkillDirs_按跳过表与隐藏目录过滤(t *testing.T) {
	root := t.TempDir()
	now := time.Now()
	writeSkillMD(t, filepath.Join(root, "keep"), "# keep", now)
	for _, skipped := range []string{"output", "temp", "assets", "node_modules", ".hidden"} {
		writeSkillMD(t, filepath.Join(root, skipped, "ghost"), "# ghost", now)
	}

	entries := discoverSkillDirs([]string{root})
	assert.Equal(t, []string{"keep"}, discoveredDirs(entries, root))
}

func TestDiscoverSkillDirs_根自身是技能目录(t *testing.T) {
	root := t.TempDir()
	writeSkillMD(t, root, "# root skill", time.Now())

	entries := discoverSkillDirs([]string{root})
	require.Len(t, entries, 1)
	assert.Equal(t, root, entries[0].Dir)
}

func TestDiscoverSkillDirs_不存在的根被跳过(t *testing.T) {
	entries := discoverSkillDirs([]string{filepath.Join(t.TempDir(), "missing")})
	assert.Empty(t, entries)
}

func TestBuildSkillsSnapshotSignature_按SKILLmd内容变化(t *testing.T) {
	root := t.TempDir()
	base := time.Now().Add(-2 * time.Hour)
	writeSkillMD(t, filepath.Join(root, "alpha"), "# alpha", base)

	rail := NewSkillUseRail([]string{root})
	sigBefore := rail.buildSkillsSnapshotSignature()
	require.Len(t, sigBefore, 1)

	// 只改文件内容并推进 mtime：签名必须变化（目录 mtime 不变）
	writeSkillMD(t, filepath.Join(root, "alpha"), "# alpha changed", base.Add(time.Hour))
	sigAfter := rail.buildSkillsSnapshotSignature()
	assert.NotEqual(t, sigBefore, sigAfter)
}

func TestRefreshSkillPromptIfChanged_内容变化后重新加载(t *testing.T) {
	root := t.TempDir()
	base := time.Now().Add(-2 * time.Hour)
	writeSkillMD(t, filepath.Join(root, "alpha"), "---\nname: alpha\ndescription: 旧描述\n---\n", base)

	rail := NewSkillUseRail([]string{root})
	ctx := context.Background()
	rail.refreshSkillPromptIfChanged(ctx)
	require.Len(t, rail.skills, 1)
	assert.Equal(t, "旧描述", rail.skills[0].Description)

	// 内容未变：签名相同，不应重复加载
	assert.True(t, rail.skillsSignatureEquals())

	writeSkillMD(t, filepath.Join(root, "alpha"), "---\nname: alpha\ndescription: 新描述\n---\n", base.Add(time.Hour))
	assert.False(t, rail.skillsSignatureEquals(), "SKILL.md mtime 变化必须被签名捕获")
	rail.refreshSkillPromptIfChanged(ctx)
	require.Len(t, rail.skills, 1)
	assert.Equal(t, "新描述", rail.skills[0].Description)
}

func TestSetSkillsDir与SetDisabledSkills_热刷新入口(t *testing.T) {
	first := t.TempDir()
	second := t.TempDir()

	rail := NewSkillUseRail([]string{first})
	rail.SetSkillsDir([]string{second})
	assert.Equal(t, []string{second}, rail.normalizeSkillDirs())

	rail.SetDisabledSkills([]string{"alpha"})
	_, disabled := rail.disabledSkills["alpha"]
	assert.True(t, disabled, "禁用名单应更新")

	visible := rail.filterSkills([]*skillpkg.Skill{{Name: "alpha"}, {Name: "beta"}})
	require.Len(t, visible, 1)
	assert.Equal(t, "beta", visible[0].Name)
}

func TestFetchEvolutionTextsFor_去重且无记录时写入空文本(t *testing.T) {
	skillsBase := t.TempDir()
	store := checkpointing.NewEvolutionStore([]string{skillsBase})

	rail := NewSkillUseRail([]string{t.TempDir()}, WithEvolutionStore(store))
	rail.fetchEvolutionTextsFor(context.Background(), []*skillpkg.Skill{
		{Name: "alpha", Directory: skillsBase},
		{Name: "alpha", Directory: skillsBase},
		nil,
		{Name: "beta", Directory: skillsBase},
	})

	// 无演进记录 → 文本为空，但两个技能都应被写入（同名去重后只处理一次）
	assert.Len(t, rail.evolutionTexts, 2)
	assert.Equal(t, "", rail.evolutionTexts["alpha"])
	assert.Equal(t, "", rail.evolutionTexts["beta"])
}
