package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	manifest "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/manifest"
	hschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/schema"
)

// TestApplyTeamWorktreeShellGuard_合并既有deny 已有 sys_operation rail 时合并模式且保留原模式。
// Python: _apply_team_worktree_shell_guard :107-124
func TestApplyTeamWorktreeShellGuard_合并既有deny(t *testing.T) {
	rails := []hschema.RailSpec{{
		Type:   manifest.SysOperation,
		Params: map[string]any{"bash_deny_patterns": []string{"\\brm -rf /\\b"}},
	}}
	guarded := applyTeamWorktreeShellGuard(rails, true)
	require.Len(t, guarded, 1)
	deny, ok := guarded[0].Params["bash_deny_patterns"].([]string)
	require.True(t, ok)
	assert.Contains(t, deny, "\\brm -rf /\\b")
	for _, p := range teamWorktreeBashDenyPatterns {
		assert.Contains(t, deny, p)
	}
}

// TestApplyTeamWorktreeShellGuard_无sysop追加 无 sys_operation rail 时追加一条。
// Python: :125-135
func TestApplyTeamWorktreeShellGuard_无sysop追加(t *testing.T) {
	rails := []hschema.RailSpec{{Type: "core.skill_use"}}
	guarded := applyTeamWorktreeShellGuard(rails, true)
	assert.Len(t, guarded, 2)
	assert.Equal(t, manifest.SysOperation, guarded[1].Type)
	assert.True(t, hasTeamWorktreeShellGuard(guarded))
}

// TestApplyTeamWorktreeShellGuard_禁用直通 enabled=false 原样返回。
// Python: :108-110
func TestApplyTeamWorktreeShellGuard_禁用直通(t *testing.T) {
	rails := []hschema.RailSpec{{Type: "core.skill_use"}}
	assert.Equal(t, rails, applyTeamWorktreeShellGuard(rails, false))
}

// TestApplyTeamWorktreeShellGuard_幂等 已带 guard 的 rails 再应用不重复。
func TestApplyTeamWorktreeShellGuard_幂等(t *testing.T) {
	rails := applyTeamWorktreeShellGuard([]hschema.RailSpec{{Type: manifest.SysOperation}}, true)
	guarded := applyTeamWorktreeShellGuard(rails, true)
	require.Len(t, guarded, 1)
	deny := guarded[0].Params["bash_deny_patterns"].([]string)
	assert.Len(t, deny, len(teamWorktreeBashDenyPatterns), "不重复追加（幂等判定基准）")
}

// TestCompileTeamWorktreeDenyPatterns 拒绝模式必须兼容 RE2。
func TestCompileTeamWorktreeDenyPatterns(t *testing.T) {
	assert.NotPanics(t, compileTeamWorktreeDenyPatterns)
}
