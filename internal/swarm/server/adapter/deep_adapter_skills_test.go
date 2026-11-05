package adapter

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	skillrails "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/skills"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	sainterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
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

// TestLoopSession_无注入且无实例返回nil 未初始化 adapter 不得 panic
func TestLoopSession_无注入且无实例返回nil(t *testing.T) {
	d := &DeepAdapter{}
	assert.Nil(t, d.loopSession())
}

// TestRefreshSkillRails_清会话基线 刷新后旧基线被清空，下轮重拍
func TestRefreshSkillRails_清会话基线(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, "alpha")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(skillDir, "SKILL.md"),
		[]byte("---\nname: alpha\ndescription: 示例\n---\n# alpha\n"), 0o644))

	rail := skillrails.NewSkillUseRail([]string{root})
	sess := newRefreshFakeSession("sess-1")
	ctx := context.Background()

	// 先经 BeforeInvoke 落盘基线
	require.NoError(t, rail.BeforeInvoke(ctx, sainterfaces.NewAgentCallbackContext(nil, nil, sess)))
	raw, err := sess.GetState(state.StringKey(skillStateKey))
	require.NoError(t, err)
	require.NotNil(t, raw, "前置条件：基线已落盘")

	d := &DeepAdapter{
		skillRail:           rail,
		loopSessionProvider: func() sessioninterfaces.SessionFacade { return sess },
	}
	require.NoError(t, d.RefreshSkillRails(ctx))

	raw, err = sess.GetState(state.StringKey(skillStateKey))
	require.NoError(t, err)
	assert.Nil(t, raw, "刷新后必须清空会话基线，强制下轮重拍")
}

// ──────────────────────────── 结构体 ────────────────────────────

// refreshFakeSession 带状态的会话假实现（技能基线读写用）
type refreshFakeSession struct {
	// id 会话 id
	id string
	// stateMap 状态表
	stateMap map[string]any
}

// ──────────────────────────── 常量 ────────────────────────────

// skillStateKey 技能基线的会话状态键（与 rails 包内常量同值，属跨包契约）
const skillStateKey = "skill_use"

// ──────────────────────────── 非导出函数 ────────────────────────────

func newRefreshFakeSession(id string) *refreshFakeSession {
	return &refreshFakeSession{id: id, stateMap: map[string]any{}}
}

func (f *refreshFakeSession) GetSessionID() string { return f.id }

func (f *refreshFakeSession) UpdateState(data map[string]any) {
	for k, v := range data {
		f.stateMap[k] = v
	}
}

func (f *refreshFakeSession) GetState(key state.StateKey) (any, error) {
	return f.stateMap[key.String()], nil
}

func (f *refreshFakeSession) DumpState() map[string]any                    { return f.stateMap }
func (f *refreshFakeSession) WriteStream(context.Context, any) error       { return nil }
func (f *refreshFakeSession) WriteCustomStream(context.Context, any) error { return nil }
func (f *refreshFakeSession) GetEnv(string, ...any) any                    { return nil }
func (f *refreshFakeSession) Interact(context.Context, any) error          { return nil }
func (f *refreshFakeSession) ClearSession(context.Context)                 {}

// TestSyncMcpServersForRuntime_空配置仍刷新技能 服务增删收尾必须调用技能刷新（rail 未构建时安全返回）
func TestSyncMcpServersForRuntime_空配置仍刷新技能(t *testing.T) {
	d := &DeepAdapter{instanceOverrides: map[string]any{}}
	require.NoError(t, d.syncMcpServersForRuntime(context.Background(), map[string]any{}, "test"))
}
