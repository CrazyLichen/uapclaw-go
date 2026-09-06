package team_workspace

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
)

// ──────────────────────────── 非导出函数 ────────────────────────────

// newRailTestCBC 构造一个工具调用的 AgentCallbackContext。
func newRailTestCBC(toolName string, toolArgs map[string]any) *interfaces.AgentCallbackContext {
	inputs := &interfaces.ToolCallInputs{
		ToolName: toolName,
		ToolArgs: toolArgs,
	}
	return interfaces.NewAgentCallbackContext(nil, inputs, nil)
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestNewTeamWorkspaceRail 构造
func TestNewTeamWorkspaceRail(t *testing.T) {
	m := newTestManager(t, true)
	r := NewTeamWorkspaceRail(m, "member-1")

	assert.NotNil(t, r)
	assert.Equal(t, m, r.ws)
	assert.Equal(t, "member-1", r.memberName)
	assert.Equal(t, pullIntervalDefault, r.pullInterval)
}

// TestBeforeToolCall_非团队路径忽略
func TestBeforeToolCall_非团队路径忽略(t *testing.T) {
	m := newTestManager(t, true)
	r := NewTeamWorkspaceRail(m, "member-1")

	cbc := newRailTestCBC("write_file", map[string]any{"file_path": "src/main.go"})
	err := r.BeforeToolCall(context.Background(), cbc)

	assert.NoError(t, err)
	_, hasReject := cbc.Extra()["workspace_lock_rejected"]
	assert.False(t, hasReject)
}

// TestBeforeToolCall_读操作放行
func TestBeforeToolCall_读操作放行(t *testing.T) {
	m := newTestManager(t, true)
	r := NewTeamWorkspaceRail(m, "member-1")

	cbc := newRailTestCBC("read_file", map[string]any{"file_path": ".team/test-team/artifacts/report.md"})
	err := r.BeforeToolCall(context.Background(), cbc)

	assert.NoError(t, err)
	_, hasReject := cbc.Extra()["workspace_lock_rejected"]
	assert.False(t, hasReject)
}

// TestBeforeToolCall_写操作无锁放行
func TestBeforeToolCall_写操作无锁放行(t *testing.T) {
	m := newTestManager(t, true)
	r := NewTeamWorkspaceRail(m, "member-1")

	cbc := newRailTestCBC("write_file", map[string]any{"file_path": ".team/test-team/artifacts/report.md"})
	err := r.BeforeToolCall(context.Background(), cbc)

	assert.NoError(t, err)
	_, hasReject := cbc.Extra()["workspace_lock_rejected"]
	assert.False(t, hasReject)
}

// TestBeforeToolCall_写操作锁冲突
func TestBeforeToolCall_写操作锁冲突(t *testing.T) {
	m := newTestManager(t, true)
	m.config.ConflictStrategy = ConflictStrategyLock

	// 先由另一个成员获取锁
	path := ".team/test-team/artifacts/report.md"
	acquired, err := m.AcquireLock(context.Background(), path, "other-member", "Other Member")
	require.NoError(t, err)
	require.True(t, acquired)

	r := NewTeamWorkspaceRail(m, "member-1")
	cbc := newRailTestCBC("write_file", map[string]any{"file_path": path})
	err = r.BeforeToolCall(context.Background(), cbc)

	// 不返回错误，但 Extra 中标记拒绝
	assert.NoError(t, err)
	rejectMsg, hasReject := cbc.Extra()["workspace_lock_rejected"]
	assert.True(t, hasReject)
	assert.Contains(t, rejectMsg, "other-member")
}

// TestBeforeToolCall_写操作自己的锁
func TestBeforeToolCall_写操作自己的锁(t *testing.T) {
	m := newTestManager(t, true)
	m.config.ConflictStrategy = ConflictStrategyLock

	// 当前成员自己持有锁
	path := ".team/test-team/artifacts/report.md"
	acquired, err := m.AcquireLock(context.Background(), path, "member-1", "Member One")
	require.NoError(t, err)
	require.True(t, acquired)

	r := NewTeamWorkspaceRail(m, "member-1")
	cbc := newRailTestCBC("write_file", map[string]any{"file_path": path})
	err = r.BeforeToolCall(context.Background(), cbc)

	// 自己的锁，不应被拒绝
	assert.NoError(t, err)
	_, hasReject := cbc.Extra()["workspace_lock_rejected"]
	assert.False(t, hasReject)
}

// TestAfterToolCall_非团队路径忽略
func TestAfterToolCall_非团队路径忽略(t *testing.T) {
	m := newTestManager(t, true)
	r := NewTeamWorkspaceRail(m, "member-1")

	cbc := newRailTestCBC("write_file", map[string]any{"file_path": "src/main.go"})
	err := r.AfterToolCall(context.Background(), cbc)

	assert.NoError(t, err)
}

// TestAfterToolCall_写操作后AutoCommit
func TestAfterToolCall_写操作后AutoCommit(t *testing.T) {
	m := newTestManager(t, true)
	require.NoError(t, m.Initialize(context.Background()))

	// 在工作空间中创建一个文件（模拟 write_file 工具的结果）
	relPath := "artifacts/report.md"
	fullPath := filepath.Join(m.WorkspacePath(), relPath)
	require.NoError(t, os.MkdirAll(filepath.Dir(fullPath), 0o755))
	require.NoError(t, os.WriteFile(fullPath, []byte("hello"), 0o644))

	r := NewTeamWorkspaceRail(m, "member-1")
	cbc := newRailTestCBC("write_file", map[string]any{"file_path": ".team/test-team/" + relPath})
	err := r.AfterToolCall(context.Background(), cbc)

	assert.NoError(t, err)

	// 验证 git 提交已生成
	history, err := m.GetHistory(context.Background(), relPath, 1)
	require.NoError(t, err)
	require.Len(t, history, 1, "应有一次自动提交")
	assert.Contains(t, history[0].Message, "[member-1]")
}

// TestAfterToolCall_写操作后发布事件
func TestAfterToolCall_写操作后发布事件(t *testing.T) {
	var capturedEvent string
	var capturedPayload any

	m := newTestManager(t, false)
	m = NewTeamWorkspaceManager(
		TeamWorkspaceConfig{
			Enabled:          true,
			ArtifactDirs:     []string{"artifacts"},
			VersionControl:   false,
			ConflictStrategy: ConflictStrategyLock,
		},
		m.WorkspacePath(),
		m.TeamName(),
		WorkspaceModeLocal,
		WithPublishEvent(func(eventType string, event any) {
			capturedEvent = eventType
			capturedPayload = event
		}),
	)

	r := NewTeamWorkspaceRail(m, "member-1")
	cbc := newRailTestCBC("write_file", map[string]any{"file_path": ".team/test-team/artifacts/report.md"})
	err := r.AfterToolCall(context.Background(), cbc)

	assert.NoError(t, err)
	assert.Equal(t, eventWorkspaceArtifactUpdated, capturedEvent)

	payload, ok := capturedPayload.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "test-team", payload["team_name"])
	assert.Equal(t, "member-1", payload["member_name"])
	assert.Equal(t, "artifacts/report.md", payload["artifact_path"])
}
