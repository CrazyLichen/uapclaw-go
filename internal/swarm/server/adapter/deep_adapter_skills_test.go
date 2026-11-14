package adapter

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	harness "github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
	skillrails "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/skills"
	sainterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
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

// newSkillScanTestAdapter 构造带 DeepAgentConfig.Skills 的 adapter 测试载体。
// D-5 回填后数据源为 DeepAgentConfig.Skills（extension_binder 提交）。
func newSkillScanTestAdapter(t *testing.T, skills []string) *DeepAdapter {
	t.Helper()
	card := agentschema.NewAgentCard(agentschema.WithAgentName("scan-test"))
	agent := harness.NewDeepAgent(card)
	cfg := hschema.NewDeepAgentConfig()
	cfg.Card = agent.Card()
	cfg.Skills = skills
	require.NoError(t, agent.ConfigureDeepConfig(context.Background(), cfg))
	return &DeepAdapter{instance: agent, instanceOverrides: map[string]any{}}
}

// TestSkillScanDirs_读DeepAgentConfigSkills_leaf归一前置去重
// D-5 回填：绑定包目录读 DeepAgentConfig.Skills（前置、leaf→父目录归一、去重、
// 空条目跳过），原先错误来源的 instanceOverrides["skills"] 分支已删除。
func TestSkillScanDirs_读DeepAgentConfigSkills_leaf归一前置去重(t *testing.T) {
	pkgDir := t.TempDir()
	leaf := filepath.Join(pkgDir, "my-skill")
	require.NoError(t, os.MkdirAll(leaf, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(leaf, "SKILL.md"), []byte("# s"), 0o644))
	plain := t.TempDir()

	d := newSkillScanTestAdapter(t, []string{leaf, leaf, plain, "", plain})
	dirs := d.skillScanDirs()
	require.NotEmpty(t, dirs)
	assert.Equal(t, pkgDir, dirs[0], "leaf 目录应归一为父目录并前置")
	count := 0
	for _, dir := range dirs {
		if dir == pkgDir {
			count++
		}
	}
	assert.Equal(t, 1, count, "重复条目应去重")
	for _, dir := range dirs {
		assert.NotEmpty(t, dir)
	}
}

// TestSkillScanDirs_包目录压过同名workspace技能 包根前置语义（leaf 父目录在前）。
func TestSkillScanDirs_包目录压过同名workspace技能(t *testing.T) {
	pkgDir := t.TempDir()
	d := newSkillScanTestAdapter(t, []string{pkgDir})
	dirs := d.skillScanDirs()
	require.NotEmpty(t, dirs)
	assert.Equal(t, pkgDir, dirs[0], "包目录应先于 workspace 技能目录")
	if ws := workspace.AgentSkillsDir(); ws != "" {
		assert.Contains(t, dirs, ws)
	}
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

// TestRefreshSkillRails_未绑定会话时安全返回 实例存在但无活跃会话时不得 panic
// （typed-nil 会话由 rail 的 isNilSession 兜底）
func TestRefreshSkillRails_未绑定会话时安全返回(t *testing.T) {
	rail := skillrails.NewSkillUseRail([]string{t.TempDir()})
	d := &DeepAdapter{skillRail: rail, instance: &harness.DeepAgent{}}
	require.NoError(t, d.RefreshSkillRails(context.Background()))
}
