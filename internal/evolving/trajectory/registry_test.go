package trajectory

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ──────────────────────────── MemberTrajectorySnapshot 测试 ────────────────────────────

// TestNewMemberTrajectorySnapshot 创建快照
func TestNewMemberTrajectorySnapshot(t *testing.T) {
	traj := &Trajectory{ExecutionID: "exec-1", SessionID: "sess-1", Steps: []*TrajectoryStep{}}

	// 带完整参数
	snapshot := NewMemberTrajectorySnapshot("team-1", "m1", traj, "leader", "sess-1", 1000)
	assert.Equal(t, "team-1", snapshot.TeamID)
	assert.Equal(t, "m1", snapshot.MemberID)
	assert.Equal(t, "leader", snapshot.MemberRole)
	assert.Equal(t, "sess-1", snapshot.SessionID)
	assert.Equal(t, 1000, snapshot.RecordedAtMs)

	// 默认 sessionID 从轨迹继承
	snapshot = NewMemberTrajectorySnapshot("team-1", "m1", traj, "", "", 0)
	assert.Equal(t, "sess-1", snapshot.SessionID)
	assert.True(t, snapshot.RecordedAtMs > 0) // 自动填充当前时间
}

// ──────────────────────────── InMemoryTrajectoryRegistry 测试 ────────────────────────────

// TestNewInMemoryTrajectoryRegistry 创建注册表
func TestNewInMemoryTrajectoryRegistry(t *testing.T) {
	registry := NewInMemoryTrajectoryRegistry()
	assert.NotNil(t, registry)
	assert.NotNil(t, registry.snapshots)
}

// TestInMemoryTrajectoryRegistry_PublishAndGet 发布和获取
func TestInMemoryTrajectoryRegistry_PublishAndGet(t *testing.T) {
	registry := NewInMemoryTrajectoryRegistry()

	// 空注册表
	result := registry.GetTrajectory("team-1", "sess-1", true)
	assert.Nil(t, result)

	// 发布一个成员轨迹
	traj := &Trajectory{
		ExecutionID: "exec-1",
		SessionID:   "sess-1",
		Source:      "online",
		Steps: []*TrajectoryStep{
			{Kind: StepKindTool, Detail: &ToolCallDetail{ToolName: "view_task"}, StartTimeMs: 100, Meta: map[string]any{"invoke_id": "inv-1"}},
		},
		Cost: CostInfo{"input_tokens": 50, "output_tokens": 25},
		Meta: map[string]any{"member_id": "m1"},
	}
	snapshot := NewMemberTrajectorySnapshot("team-1", "m1", traj, "leader", "sess-1", 1000)
	registry.PublishMemberTrajectory(snapshot)

	// 获取聚合轨迹
	result = registry.GetTrajectory("team-1", "sess-1", true)
	require.NotNil(t, result)
	assert.Equal(t, "team-team-1", result.ExecutionID)
}

// TestInMemoryTrajectoryRegistry_多成员发布 多成员发布
func TestInMemoryTrajectoryRegistry_多成员发布(t *testing.T) {
	registry := NewInMemoryTrajectoryRegistry()

	traj1 := &Trajectory{
		ExecutionID: "exec-1",
		SessionID:   "sess-1",
		Source:      "online",
		Steps: []*TrajectoryStep{
			{Kind: StepKindTool, Detail: &ToolCallDetail{ToolName: "view_task"}, StartTimeMs: 100, Meta: map[string]any{"invoke_id": "inv-1"}},
		},
		Cost: CostInfo{"input_tokens": 50, "output_tokens": 25},
	}
	traj2 := &Trajectory{
		ExecutionID: "exec-2",
		SessionID:   "sess-1",
		Source:      "online",
		Steps: []*TrajectoryStep{
			{Kind: StepKindTool, Detail: &ToolCallDetail{ToolName: "claim_task"}, StartTimeMs: 150, Meta: map[string]any{"invoke_id": "inv-2"}},
		},
		Cost: CostInfo{"input_tokens": 30, "output_tokens": 15},
	}

	registry.PublishMemberTrajectory(NewMemberTrajectorySnapshot("team-1", "m1", traj1, "leader", "sess-1", 1000))
	registry.PublishMemberTrajectory(NewMemberTrajectorySnapshot("team-1", "m2", traj2, "teammate", "sess-1", 1000))

	result := registry.GetTrajectory("team-1", "sess-1", true)
	require.NotNil(t, result)
	assert.Equal(t, 2, result.Meta["member_count"])
}

// TestInMemoryTrajectoryRegistry_同成员更新 同成员更新快照
func TestInMemoryTrajectoryRegistry_同成员更新(t *testing.T) {
	registry := NewInMemoryTrajectoryRegistry()

	// 旧快照
	oldTraj := &Trajectory{
		ExecutionID: "exec-1",
		SessionID:   "sess-1",
		Steps:       []*TrajectoryStep{{Kind: StepKindLLM, StartTimeMs: 100}},
	}
	registry.PublishMemberTrajectory(NewMemberTrajectorySnapshot("team-1", "m1", oldTraj, "", "sess-1", 1000))

	// 新快照（更晚的 recorded_at_ms）
	newTraj := &Trajectory{
		ExecutionID: "exec-1",
		SessionID:   "sess-1",
		Steps:       []*TrajectoryStep{{Kind: StepKindLLM, StartTimeMs: 100}, {Kind: StepKindTool, StartTimeMs: 200}},
	}
	registry.PublishMemberTrajectory(NewMemberTrajectorySnapshot("team-1", "m1", newTraj, "", "sess-1", 2000))

	// 不过滤协作步骤，验证快照更新逻辑
	result := registry.GetTrajectory("team-1", "sess-1", false)
	require.NotNil(t, result)
	// 新快照应替换旧快照
	assert.Len(t, result.Steps, 2)
}

// TestInMemoryTrajectoryRegistry_同成员旧快照 旧快照不应替换新快照
func TestInMemoryTrajectoryRegistry_同成员旧快照(t *testing.T) {
	registry := NewInMemoryTrajectoryRegistry()

	// 新快照
	newTraj := &Trajectory{
		ExecutionID: "exec-1",
		SessionID:   "sess-1",
		Steps:       []*TrajectoryStep{{Kind: StepKindLLM, StartTimeMs: 100}, {Kind: StepKindTool, StartTimeMs: 200}},
	}
	registry.PublishMemberTrajectory(NewMemberTrajectorySnapshot("team-1", "m1", newTraj, "", "sess-1", 2000))

	// 旧快照（更早的 recorded_at_ms）
	oldTraj := &Trajectory{
		ExecutionID: "exec-1",
		SessionID:   "sess-1",
		Steps:       []*TrajectoryStep{{Kind: StepKindLLM, StartTimeMs: 100}},
	}
	registry.PublishMemberTrajectory(NewMemberTrajectorySnapshot("team-1", "m1", oldTraj, "", "sess-1", 1000))

	// 不过滤协作步骤，验证快照保留逻辑
	result := registry.GetTrajectory("team-1", "sess-1", false)
	require.NotNil(t, result)
	// 新快照应保留
	assert.Len(t, result.Steps, 2)
}

// TestInMemoryTrajectoryRegistry_ClearSession 清除会话
func TestInMemoryTrajectoryRegistry_ClearSession(t *testing.T) {
	registry := NewInMemoryTrajectoryRegistry()

	traj := &Trajectory{ExecutionID: "exec-1", SessionID: "sess-1", Steps: []*TrajectoryStep{{Kind: StepKindLLM}}}
	registry.PublishMemberTrajectory(NewMemberTrajectorySnapshot("team-1", "m1", traj, "", "sess-1", 1000))

	// 清除
	registry.ClearSession("team-1", "sess-1")
	result := registry.GetTrajectory("team-1", "sess-1", true)
	assert.Nil(t, result)
}

// TestInMemoryTrajectoryRegistry_不同会话隔离 不同会话隔离
func TestInMemoryTrajectoryRegistry_不同会话隔离(t *testing.T) {
	registry := NewInMemoryTrajectoryRegistry()

	traj1 := &Trajectory{ExecutionID: "exec-1", SessionID: "sess-1", Steps: []*TrajectoryStep{{Kind: StepKindLLM}}}
	traj2 := &Trajectory{ExecutionID: "exec-2", SessionID: "sess-2", Steps: []*TrajectoryStep{{Kind: StepKindTool}}}

	registry.PublishMemberTrajectory(NewMemberTrajectorySnapshot("team-1", "m1", traj1, "", "sess-1", 1000))
	registry.PublishMemberTrajectory(NewMemberTrajectorySnapshot("team-1", "m1", traj2, "", "sess-2", 1000))

	// 不过滤协作步骤，验证会话隔离
	result1 := registry.GetTrajectory("team-1", "sess-1", false)
	result2 := registry.GetTrajectory("team-1", "sess-2", false)
	require.NotNil(t, result1)
	require.NotNil(t, result2)
	// 两个会话的轨迹互不影响
	assert.Len(t, result1.Steps, 1)
	assert.Len(t, result2.Steps, 1)
}

// ──────────────────────────── shouldKeepCurrent 测试 ────────────────────────────

// TestShouldKeepCurrent 当前快照时间戳更大
func TestShouldKeepCurrent_时间戳更大(t *testing.T) {
	current := &snapshotEntry{
		snapshot: &MemberTrajectorySnapshot{RecordedAtMs: 2000},
		sequence: 1,
	}
	incoming := &snapshotEntry{
		snapshot: &MemberTrajectorySnapshot{RecordedAtMs: 1000},
		sequence: 2,
	}
	// 当前快照时间戳更大，应保留
	assert.True(t, shouldKeepCurrent(current, incoming))
}

// TestShouldKeepCurrent_传入快照时间戳更大
func TestShouldKeepCurrent_传入快照时间戳更大(t *testing.T) {
	current := &snapshotEntry{
		snapshot: &MemberTrajectorySnapshot{RecordedAtMs: 1000},
		sequence: 1,
	}
	incoming := &snapshotEntry{
		snapshot: &MemberTrajectorySnapshot{RecordedAtMs: 2000},
		sequence: 2,
	}
	// 传入快照时间戳更大，应替换
	assert.False(t, shouldKeepCurrent(current, incoming))
}

// TestShouldKeepCurrent_时间戳相同序列号更大 当前序列号更大
func TestShouldKeepCurrent_时间戳相同序列号更大(t *testing.T) {
	current := &snapshotEntry{
		snapshot: &MemberTrajectorySnapshot{RecordedAtMs: 1000},
		sequence: 2,
	}
	incoming := &snapshotEntry{
		snapshot: &MemberTrajectorySnapshot{RecordedAtMs: 1000},
		sequence: 1,
	}
	// 时间戳相同，当前序列号更大，应保留
	assert.True(t, shouldKeepCurrent(current, incoming))
}

// ──────────────────────────── trajectoryForSnapshot 测试 ────────────────────────────

// TestTrajectoryForSnapshot 注入成员元数据
func TestTrajectoryForSnapshot(t *testing.T) {
	traj := &Trajectory{
		ExecutionID: "exec-1",
		SessionID:   "sess-1",
		Source:      "online",
		Steps:       []*TrajectoryStep{{Kind: StepKindLLM}},
		Meta:        map[string]any{"key": "value"},
	}
	snapshot := &MemberTrajectorySnapshot{
		TeamID:     "team-1",
		MemberID:   "m1",
		MemberRole: "leader",
		Trajectory: traj,
	}

	result := trajectoryForSnapshot(snapshot)
	assert.Equal(t, "exec-1", result.ExecutionID)
	assert.Equal(t, "m1", result.Meta["member_id"])
	assert.Equal(t, "leader", result.Meta["member_role"])
	assert.Equal(t, "value", result.Meta["key"])
	// 原始轨迹的 Meta 不应被修改
	assert.NotContains(t, traj.Meta, "member_id")
}

// ──────────────────────────── NowMs 测试 ────────────────────────────

// TestNowMs 返回合理时间戳
func TestNowMs(t *testing.T) {
	ms := NowMs()
	assert.True(t, ms > 0)
	// 应该是秒级时间戳 * 1000
	assert.True(t, ms > 1000000000000) // 2001 年之后
}
