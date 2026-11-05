package adapter

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	skillrails "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/skills"
	sainterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
	skill "github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime/skill"
)

// TestRefreshSkillRails_无rail时安全返回 未构建 rail 时不得 panic
func TestRefreshSkillRails_无rail时安全返回(t *testing.T) {
	d := &DeepAdapter{}
	assert.NoError(t, d.RefreshSkillRails(context.Background()))
}

// TestRefreshSkillRails_重载并清基线 有 rail 时重载成功
func TestRefreshSkillRails_重载并清基线(t *testing.T) {
	rail := skillrails.NewSkillUseRail([]string{t.TempDir()})
	var asRail sainterfaces.AgentRail = rail
	d := &DeepAdapter{skillRail: asRail}

	require.NoError(t, d.RefreshSkillRails(context.Background()))
	assert.NotNil(t, rail.GetSkillsForSession(nil), "刷新后应可读出技能视图（空目录为空切片）")
}

// TestSkillScanDirs_workspace兜底 无绑定目录时回落到 workspace 技能目录
func TestSkillScanDirs_workspace兜底(t *testing.T) {
	d := &DeepAdapter{instanceOverrides: map[string]any{}}
	dirs := d.skillScanDirs()
	ws := workspace.AgentSkillsDir()
	if ws == "" {
		assert.Empty(t, dirs)
		return
	}
	assert.Contains(t, dirs, ws)
}

// TestSkillScanDirs_绑定目录前置去重 绑定包目录优先且不重复
func TestSkillScanDirs_绑定目录前置去重(t *testing.T) {
	bound := t.TempDir()
	d := &DeepAdapter{instanceOverrides: map[string]any{"skills": []any{bound, bound}}}
	dirs := d.skillScanDirs()
	require.NotEmpty(t, dirs)
	assert.Equal(t, bound, dirs[0], "绑定目录应前置")
	count := 0
	for _, dir := range dirs {
		if dir == bound {
			count++
		}
	}
	assert.Equal(t, 1, count, "重复绑定目录应去重")
}

// TestSkillScanDirs_字符串形式绑定目录 单目录可用 string 声明
func TestSkillScanDirs_字符串形式绑定目录(t *testing.T) {
	bound := t.TempDir()
	d := &DeepAdapter{instanceOverrides: map[string]any{"skills": bound}}
	dirs := d.skillScanDirs()
	require.NotEmpty(t, dirs)
	assert.Equal(t, bound, dirs[0])
}

// TestSkillScanDirs_字符串切片绑定目录 多目录可用 []string 声明
func TestSkillScanDirs_字符串切片绑定目录(t *testing.T) {
	first := t.TempDir()
	second := t.TempDir()
	d := &DeepAdapter{instanceOverrides: map[string]any{"skills": []string{first, second}}}
	dirs := d.skillScanDirs()
	require.GreaterOrEqual(t, len(dirs), 2)
	assert.Equal(t, first, dirs[0])
	assert.Equal(t, second, dirs[1])
}

// TestSyncMcpServersForRuntime_空配置仍刷新技能 服务增删收尾必须调用技能刷新（rail 未构建时安全返回）
func TestSyncMcpServersForRuntime_空配置仍刷新技能(t *testing.T) {
	d := &DeepAdapter{instanceOverrides: map[string]any{}}
	require.NoError(t, d.syncMcpServersForRuntime(context.Background(), map[string]any{}, "test"))
}

// TestRefreshSkillRails_更新禁用名单 有 skillManager 时把磁盘禁用名单推给 rail
func TestRefreshSkillRails_更新禁用名单(t *testing.T) {
	rail := skillrails.NewSkillUseRail([]string{t.TempDir()})
	d := &DeepAdapter{
		skillRail:    rail,
		skillManager: skill.NewSkillManager(t.TempDir()),
	}
	require.NoError(t, d.RefreshSkillRails(context.Background()))
}

// TestSkillScanDirs_忽略空字符串 空条目不得进入扫描根
func TestSkillScanDirs_忽略空字符串(t *testing.T) {
	bound := t.TempDir()
	d := &DeepAdapter{instanceOverrides: map[string]any{"skills": []any{"", bound}}}
	dirs := d.skillScanDirs()
	require.NotEmpty(t, dirs)
	assert.Equal(t, bound, dirs[0])
	for _, dir := range dirs {
		assert.NotEmpty(t, dir)
	}
}
