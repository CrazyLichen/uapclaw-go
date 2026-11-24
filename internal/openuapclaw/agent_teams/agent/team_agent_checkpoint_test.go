package agent

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/messager"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/metadata"
	atschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/tools"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/tools/database"
)

// newTestTeamBackendForAgent 创建测试用 TeamBackend（agent 包内联助手，
// 避免跨包引测试工具）。database 为内存实现。
func newTestTeamBackendForAgent(t *testing.T) *tools.TeamBackend {
	t.Helper()
	return tools.NewTeamBackend("test-team", "leader", true,
		database.NewInMemoryTeamDatabase(),
		messager.NewInProcessMessager(atschema.NewMessagerTransportConfig()))
}

// ──────────────────────────── 测试 ────────────────────────────

// TestTeamAgent_SetCheckpoint_首次存成功 验证首次存储返回 nil（成功）。
// Python: set_checkpoint (team_agent.py:1774-1797)
func TestTeamAgent_SetCheckpoint_首次存成功(t *testing.T) {
	a := newTestTeamAgent(t)
	conflict := a.SetCheckpoint("code-ready", 12, "编码就绪", "alice")
	require.Nil(t, conflict)
	snapshot := a.checkpointsSnapshot()
	require.Len(t, snapshot, 1)
	require.Equal(t, metadata.CheckpointRecord{Count: 12, Description: "编码就绪", CreatedBy: "alice"},
		snapshot["code-ready"])
}

// TestTeamAgent_SetCheckpoint_重名冲突返回既有记录 验证团队全局唯一名。
// Python: set_checkpoint existing 分支 (team_agent.py:1787-1793)
func TestTeamAgent_SetCheckpoint_重名冲突返回既有记录(t *testing.T) {
	a := newTestTeamAgent(t)
	require.Nil(t, a.SetCheckpoint("cp", 10, "", "alice"))
	conflict := a.SetCheckpoint("cp", 99, "其他", "bob")
	require.NotNil(t, conflict)
	require.Equal(t, 10, conflict["count"])
	require.Equal(t, "alice", conflict["created_by"])
	// 表未被污染
	snapshot := a.checkpointsSnapshot()
	require.Len(t, snapshot, 1)
	require.Equal(t, 10, snapshot["cp"].Count)
}

// TestTeamAgent_SetCheckpointsFrom_整表覆盖 验证冷恢复/share 的覆盖语义。
// Python: set_checkpoints_from (team_agent.py:1815-1822)
func TestTeamAgent_SetCheckpointsFrom_整表覆盖(t *testing.T) {
	a := newTestTeamAgent(t)
	require.Nil(t, a.SetCheckpoint("old", 1, "", "x"))
	a.setCheckpointsFrom(map[string]metadata.CheckpointRecord{
		"restored": {Count: 42, CreatedBy: "leader"},
	})
	snapshot := a.checkpointsSnapshot()
	require.Len(t, snapshot, 1)
	require.Equal(t, 42, snapshot["restored"].Count)
	// 旧表被替换（整表覆盖，非合并）
	_, ok := snapshot["old"]
	require.False(t, ok)
}

// TestTeamAgent_ShareCheckpointsWith 验证 in-process 队友共享 leader 权威映射。
// Python: share_checkpoints_with (team_agent.py:1699-1701)
func TestTeamAgent_ShareCheckpointsWith(t *testing.T) {
	leader := newTestTeamAgent(t)
	mate := newTestTeamAgent(t)
	require.Nil(t, leader.SetCheckpoint("shared", 7, "d", "leader"))
	leader.ShareCheckpointsWith(mate)
	snapshot := mate.checkpointsSnapshot()
	require.Len(t, snapshot, 1)
	require.Equal(t, 7, snapshot["shared"].Count)
}

// TestTeamAgent_SetCheckpoint_回调往返 验证 leader 装配的回调链：
// backend.StoreCheckpoint → agent.SetCheckpoint → backend.ListCheckpoints
// 读到权威表（Python team_agent.py:738-739 接线语义）。
func TestTeamAgent_SetCheckpoint_回调往返(t *testing.T) {
	a := newTestTeamAgent(t)
	backend := newTestTeamBackendForAgent(t)
	// 模拟 Configure 的 leader 装配（team_agent.py:738-739）
	backend.SetStoreCheckpointFn(a.SetCheckpoint)
	backend.SetCheckpointListFn(func() map[string]map[string]any {
		snapshot := a.checkpointsSnapshot()
		out := make(map[string]map[string]any, len(snapshot))
		for name, rec := range snapshot {
			out[name] = map[string]any{
				"count":       rec.Count,
				"description": rec.Description,
				"created_by":  rec.CreatedBy,
			}
		}
		return out
	})
	// 成员经 backend 存（leader 命名空间直写）
	conflict := backend.StoreCheckpoint("via-backend", 5, "desc", "mate")
	require.Nil(t, conflict)
	// 权威表可见
	list := backend.ListCheckpoints()
	require.Len(t, list, 1)
	require.Equal(t, 5, list["via-backend"]["count"])
	// 重名冲突经回调带出
	conflict = backend.StoreCheckpoint("via-backend", 9, "x", "other")
	require.NotNil(t, conflict)
	require.Equal(t, 5, conflict["count"])
}

// TestTeamAgent_冷恢复还原索引表后重名冲突仍生效 验证冷恢复还原契约：
// ReadTeamCheckpoints 读出的表经 setCheckpointsFrom 还原后，SetCheckpoint 的
// 团队全局唯一名语义不被破坏（还原记录与存量同 view）。
// Python: recover_from_session 内 read_team_checkpoints → set_checkpoints_from
// (team_agent.py:2175-2179)；运行时链路由集成用例覆盖（fork_checkpoint_test.go）。
func TestTeamAgent_冷恢复还原索引表后重名冲突仍生效(t *testing.T) {
	a := newTestTeamAgent(t)
	// 模拟冷恢复：持久化表（MergeTeamCheckpoints 落盘形态）还原进索引
	persisted := map[string]metadata.CheckpointRecord{
		"code-ready": {Count: 12, Description: "编码就绪", CreatedBy: "alice"},
	}
	a.setCheckpointsFrom(persisted)
	// 还原后重名冲突生效
	conflict := a.SetCheckpoint("code-ready", 99, "其他", "bob")
	require.NotNil(t, conflict)
	require.Equal(t, 12, conflict["count"])
	require.Equal(t, "alice", conflict["created_by"])
	// 表内容保持还原态
	snapshot := a.checkpointsSnapshot()
	require.Len(t, snapshot, 1)
	require.Equal(t, persisted["code-ready"], snapshot["code-ready"])
}
