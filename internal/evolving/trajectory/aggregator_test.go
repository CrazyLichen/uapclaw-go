package trajectory

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ──────────────────────────── TeamTrajectoryAggregator 测试 ────────────────────────────

// TestNewTeamTrajectoryAggregator 创建聚合器
func TestNewTeamTrajectoryAggregator(t *testing.T) {
	store := NewInMemoryTrajectoryStore()
	agg := NewTeamTrajectoryAggregator(store, "team-1")
	assert.NotNil(t, agg)
	assert.Equal(t, "team-1", agg.teamID)
}

// TestTeamTrajectoryAggregator_Aggregate_空存储 空存储返回空结果
func TestTeamTrajectoryAggregator_Aggregate_空存储(t *testing.T) {
	store := NewInMemoryTrajectoryStore()
	agg := NewTeamTrajectoryAggregator(store, "team-1")

	result := agg.Aggregate("sess-1", true)
	require.NotNil(t, result)
	assert.Equal(t, "team-1", result.TeamID)
	assert.Equal(t, "sess-1", result.SessionID)
	assert.NotNil(t, result.Combined)
	assert.Empty(t, result.Combined.Steps)
	assert.Equal(t, 0, result.Combined.Meta["member_count"])
}

// TestTeamTrajectoryAggregator_Aggregate_有轨迹 聚合轨迹
func TestTeamTrajectoryAggregator_Aggregate_有轨迹(t *testing.T) {
	store := NewInMemoryTrajectoryStore()
	// 保存两个成员的轨迹
	store.Save(&Trajectory{
		ExecutionID: "exec-1",
		SessionID:   "sess-1",
		Source:      "online",
		Meta:        map[string]any{"member_id": "m1", "member_role": "leader"},
		Steps: []*TrajectoryStep{
			{Kind: StepKindTool, Detail: &ToolCallDetail{ToolName: "view_task"}, StartTimeMs: 100, Meta: map[string]any{"invoke_id": "inv-1"}},
			{Kind: StepKindLLM, Detail: &LLMCallDetail{Model: "qwen-max"}, StartTimeMs: 200},
		},
		Cost: CostInfo{"input_tokens": 50, "output_tokens": 25},
	}, "")
	store.Save(&Trajectory{
		ExecutionID: "exec-2",
		SessionID:   "sess-1",
		Source:      "online",
		Meta:        map[string]any{"member_id": "m2"},
		Steps: []*TrajectoryStep{
			{Kind: StepKindTool, Detail: &ToolCallDetail{ToolName: "claim_task"}, StartTimeMs: 150, Meta: map[string]any{"invoke_id": "inv-2"}},
			{Kind: StepKindLLM, Detail: &LLMCallDetail{Model: "qwen-max"}, StartTimeMs: 250},
		},
		Cost: CostInfo{"input_tokens": 30, "output_tokens": 15},
	}, "")

	agg := NewTeamTrajectoryAggregator(store, "team-1")
	result := agg.Aggregate("sess-1", true)

	require.NotNil(t, result)
	assert.Equal(t, "team-1", result.TeamID)
	assert.NotNil(t, result.Combined)
	// Leader 不过滤，但非 Leader 的非协作步骤被过滤
	// m1 是 leader，保留所有步骤（view_task + llm）
	// m2 非 leader，过滤后只保留 claim_task
	assert.Equal(t, 2, result.Combined.Meta["member_count"])
	// 合并步骤按 start_time_ms 排序
	assert.NotEmpty(t, result.Combined.Steps)
}

// ──────────────────────────── AggregateMemberTrajectories 测试 ────────────────────────────

// TestAggregateMemberTrajectories 聚合已加载轨迹
func TestAggregateMemberTrajectories(t *testing.T) {
	trajectories := []*Trajectory{
		{
			ExecutionID: "exec-1",
			SessionID:   "sess-1",
			Source:      "online",
			Meta:        map[string]any{"member_id": "m1"},
			Steps: []*TrajectoryStep{
				{Kind: StepKindTool, Detail: &ToolCallDetail{ToolName: "view_task"}, StartTimeMs: 100, Meta: map[string]any{"invoke_id": "inv-1"}},
			},
			Cost: CostInfo{"input_tokens": 50, "output_tokens": 25},
		},
		{
			ExecutionID: "exec-2",
			SessionID:   "sess-1",
			Source:      "online",
			Meta:        map[string]any{"member_id": "m2"},
			Steps: []*TrajectoryStep{
				{Kind: StepKindTool, Detail: &ToolCallDetail{ToolName: "claim_task"}, StartTimeMs: 150, Meta: map[string]any{"invoke_id": "inv-2"}},
			},
			Cost: CostInfo{"input_tokens": 30, "output_tokens": 15},
		},
	}

	combined := AggregateMemberTrajectories(trajectories, "team-1", "sess-1", true)
	require.NotNil(t, combined)
	assert.Equal(t, "sess-1", combined.SessionID)
	assert.Equal(t, 2, combined.Meta["member_count"])
	assert.NotNil(t, combined.Cost)
	assert.Equal(t, 80, combined.Cost["input_tokens"])
	assert.Equal(t, 40, combined.Cost["output_tokens"])
}

// ──────────────────────────── FilterMemberTrajectory 测试 ────────────────────────────

// TestFilterMemberTrajectory 保留协作步骤
func TestFilterMemberTrajectory(t *testing.T) {
	trajectory := &Trajectory{
		ExecutionID: "exec-1",
		SessionID:   "sess-1",
		Source:      "online",
		Steps: []*TrajectoryStep{
			// 协作步骤：有跨成员元数据
			{Kind: StepKindLLM, StartTimeMs: 100, Meta: map[string]any{"invoke_id": "inv-1"}},
			// 协作步骤：协作工具
			{Kind: StepKindTool, Detail: &ToolCallDetail{ToolName: "view_task"}, StartTimeMs: 200},
			// 非协作步骤：纯 LLM 推理
			{Kind: StepKindLLM, Detail: &LLMCallDetail{Model: "qwen-max"}, StartTimeMs: 300},
			// 非协作步骤：非白名单工具
			{Kind: StepKindTool, Detail: &ToolCallDetail{ToolName: "private_tool"}, StartTimeMs: 400},
		},
		Cost: CostInfo{"input_tokens": 100},
		Meta: map[string]any{"member_id": "m1"},
	}

	filtered := FilterMemberTrajectory(trajectory)
	assert.Len(t, filtered.Steps, 2)
	assert.Equal(t, "exec-1", filtered.ExecutionID)
	assert.Equal(t, CostInfo{"input_tokens": 100}, filtered.Cost)
}

// TestFilterMemberTrajectory_全部保留 全部是协作步骤
func TestFilterMemberTrajectory_全部保留(t *testing.T) {
	trajectory := &Trajectory{
		ExecutionID: "exec-1",
		Steps: []*TrajectoryStep{
			{Kind: StepKindTool, Detail: &ToolCallDetail{ToolName: "send_message"}, Meta: map[string]any{"invoke_id": "inv-1"}},
			{Kind: StepKindTool, Detail: &ToolCallDetail{ToolName: "claim_task"}, Meta: map[string]any{"parent_invoke_id": "inv-2"}},
		},
	}

	filtered := FilterMemberTrajectory(trajectory)
	assert.Len(t, filtered.Steps, 2)
}

// TestFilterMemberTrajectory_全部过滤 无协作步骤
func TestFilterMemberTrajectory_全部过滤(t *testing.T) {
	trajectory := &Trajectory{
		ExecutionID: "exec-1",
		Steps: []*TrajectoryStep{
			{Kind: StepKindLLM, Detail: &LLMCallDetail{Model: "qwen-max"}},
			{Kind: StepKindTool, Detail: &ToolCallDetail{ToolName: "private_tool"}},
		},
	}

	filtered := FilterMemberTrajectory(trajectory)
	assert.Empty(t, filtered.Steps)
}

// ──────────────────────────── isCollaborativeStep 测试 ────────────────────────────

// TestIsCollaborativeStep 跨成员元数据键
func TestIsCollaborativeStep_跨成员元数据(t *testing.T) {
	step := &TrajectoryStep{
		Kind: StepKindLLM,
		Meta: map[string]any{"invoke_id": "inv-1"},
	}
	assert.True(t, isCollaborativeStep(step))
}

// TestIsCollaborativeStep_协作工具 协作工具名称
func TestIsCollaborativeStep_协作工具(t *testing.T) {
	for _, toolName := range []string{"view_task", "claim_task", "send_message", "workspace_meta"} {
		step := &TrajectoryStep{
			Kind:   StepKindTool,
			Detail: &ToolCallDetail{ToolName: toolName},
		}
		assert.True(t, isCollaborativeStep(step), "should be collaborative: %s", toolName)
	}
}

// TestIsCollaborativeStep_非协作工具 非协作工具
func TestIsCollaborativeStep_非协作工具(t *testing.T) {
	step := &TrajectoryStep{
		Kind:   StepKindTool,
		Detail: &ToolCallDetail{ToolName: "private_tool"},
	}
	assert.False(t, isCollaborativeStep(step))
}

// TestIsCollaborativeStep_技能文件访问 读写技能文件
func TestIsCollaborativeStep_技能文件访问(t *testing.T) {
	step := &TrajectoryStep{
		Kind:   StepKindTool,
		Detail: &ToolCallDetail{ToolName: "read_file", CallArgs: map[string]any{"path": "/skill/config.yaml"}},
	}
	assert.True(t, isCollaborativeStep(step))
}

// TestIsCollaborativeStep_非技能文件访问 读写非技能文件
func TestIsCollaborativeStep_非技能文件访问(t *testing.T) {
	step := &TrajectoryStep{
		Kind:   StepKindTool,
		Detail: &ToolCallDetail{ToolName: "read_file", CallArgs: map[string]any{"path": "/data/config.yaml"}},
	}
	assert.False(t, isCollaborativeStep(step))
}

// TestIsCollaborativeStep_纯LLM推理 纯 LLM 推理步骤
func TestIsCollaborativeStep_纯LLM推理(t *testing.T) {
	step := &TrajectoryStep{
		Kind:   StepKindLLM,
		Detail: &LLMCallDetail{Model: "qwen-max"},
	}
	assert.False(t, isCollaborativeStep(step))
}

// ──────────────────────────── isLeaderTrajectory 测试 ────────────────────────────

// TestIsLeaderTrajectory_成员角色 Leader 成员
func TestIsLeaderTrajectory_成员角色(t *testing.T) {
	trajectory := &Trajectory{
		Meta: map[string]any{"member_role": "leader"},
	}
	assert.True(t, isLeaderTrajectory(trajectory, "m1"))
}

// TestIsLeaderTrajectory_非Leader 非 Leader 成员
func TestIsLeaderTrajectory_非Leader(t *testing.T) {
	trajectory := &Trajectory{
		Meta: map[string]any{"member_role": "teammate"},
	}
	assert.False(t, isLeaderTrajectory(trajectory, "m1"))
}

// TestIsLeaderTrajectory_成员ID为Leader 成员 ID 为 leader
func TestIsLeaderTrajectory_成员ID为Leader(t *testing.T) {
	trajectory := &Trajectory{Meta: map[string]any{}}
	assert.True(t, isLeaderTrajectory(trajectory, "leader"))
}

// ──────────────────────────── mergeMemberTrajectory 测试 ────────────────────────────

// TestMergeMemberTrajectory_现有为Nil 现有为 nil
func TestMergeMemberTrajectory_现有为Nil(t *testing.T) {
	new := &Trajectory{ExecutionID: "exec-1", Steps: []*TrajectoryStep{{Kind: StepKindLLM}}}
	result := mergeMemberTrajectory(nil, new)
	assert.Equal(t, new, result)
}

// TestMergeMemberTrajectory_前缀合并 前缀合并
func TestMergeMemberTrajectory_前缀合并(t *testing.T) {
	step1 := &TrajectoryStep{Kind: StepKindLLM, StartTimeMs: 100}
	step2 := &TrajectoryStep{Kind: StepKindTool, StartTimeMs: 200}
	existing := &Trajectory{ExecutionID: "exec-1", Steps: []*TrajectoryStep{step1}}
	new := &Trajectory{ExecutionID: "exec-1", Steps: []*TrajectoryStep{step1, step2}}

	result := mergeMemberTrajectory(existing, new)
	assert.Equal(t, new, result)
}

// TestMergeMemberTrajectory_拼接合并 非前缀拼接
func TestMergeMemberTrajectory_拼接合并(t *testing.T) {
	existing := &Trajectory{
		ExecutionID: "exec-1",
		SessionID:   "sess-1",
		Source:      "online",
		Steps:       []*TrajectoryStep{{Kind: StepKindLLM, StartTimeMs: 100}},
		Cost:        CostInfo{"input_tokens": 50},
		Meta:        map[string]any{"member_id": "m1"},
	}
	new := &Trajectory{
		ExecutionID: "exec-1",
		SessionID:   "sess-2",
		Source:      "online",
		CaseID:      "case-1",
		Steps:       []*TrajectoryStep{{Kind: StepKindTool, StartTimeMs: 200}},
		Cost:        CostInfo{"input_tokens": 30, "output_tokens": 15},
		Meta:        map[string]any{"member_id": "m1", "extra": "val"},
	}

	result := mergeMemberTrajectory(existing, new)
	assert.Equal(t, "exec-1", result.ExecutionID)
	assert.Equal(t, "sess-1", result.SessionID)
	assert.Equal(t, "case-1", result.CaseID)
	assert.Len(t, result.Steps, 2)
	assert.Equal(t, CostInfo{"input_tokens": 80, "output_tokens": 15}, result.Cost)
	assert.Equal(t, "m1", result.Meta["member_id"])
	assert.Equal(t, "val", result.Meta["extra"])
}

// ──────────────────────────── stepsArePrefix 测试 ────────────────────────────

// TestStepsArePrefix 前缀判断
func TestStepsArePrefix(t *testing.T) {
	step1 := &TrajectoryStep{Kind: StepKindLLM}
	step2 := &TrajectoryStep{Kind: StepKindTool}
	step3 := &TrajectoryStep{Kind: StepKindLLM, StartTimeMs: 100}

	// 前缀匹配
	assert.True(t, stepsArePrefix([]*TrajectoryStep{step1}, []*TrajectoryStep{step1, step2}))
	// 完全相同
	assert.True(t, stepsArePrefix([]*TrajectoryStep{step1}, []*TrajectoryStep{step1}))
	// 空前缀
	assert.True(t, stepsArePrefix([]*TrajectoryStep{}, []*TrajectoryStep{step1}))
	// 非前缀
	assert.False(t, stepsArePrefix([]*TrajectoryStep{step1}, []*TrajectoryStep{step2}))
	// 前缀比目标长
	assert.False(t, stepsArePrefix([]*TrajectoryStep{step1, step2}, []*TrajectoryStep{step1}))
	// 不同步骤
	assert.False(t, stepsArePrefix([]*TrajectoryStep{step1}, []*TrajectoryStep{step3}))
}

// ──────────────────────────── mergeCost 测试 ────────────────────────────

// TestMergeCost 成本合并
func TestMergeCost(t *testing.T) {
	// 都为 nil
	assert.Nil(t, mergeCost(nil, nil))

	// 一个为 nil
	result := mergeCost(CostInfo{"input_tokens": 50}, nil)
	assert.Equal(t, CostInfo{"input_tokens": 50}, result)

	// 都有值
	result = mergeCost(
		CostInfo{"input_tokens": 50, "output_tokens": 25},
		CostInfo{"input_tokens": 30, "output_tokens": 15},
	)
	assert.Equal(t, CostInfo{"input_tokens": 80, "output_tokens": 40}, result)
}

// ──────────────────────────── buildCombinedTrajectory 测试 ────────────────────────────

// TestBuildCombinedTrajectory 构建合并轨迹
func TestBuildCombinedTrajectory(t *testing.T) {
	members := map[string]*Trajectory{
		"m1": {
			ExecutionID: "exec-1",
			SessionID:   "sess-1",
			Source:      "online",
			Steps: []*TrajectoryStep{
				{Kind: StepKindLLM, StartTimeMs: 200},
				{Kind: StepKindTool, StartTimeMs: 100, Detail: &ToolCallDetail{ToolName: "view_task"}},
			},
			Cost: CostInfo{"input_tokens": 50, "output_tokens": 25},
		},
		"m2": {
			ExecutionID: "exec-2",
			SessionID:   "sess-1",
			Source:      "online",
			Steps: []*TrajectoryStep{
				{Kind: StepKindTool, StartTimeMs: 150, Detail: &ToolCallDetail{ToolName: "claim_task"}},
			},
			Cost: CostInfo{"input_tokens": 30, "output_tokens": 15},
		},
	}

	combined := buildCombinedTrajectory(members, "team-1", "sess-1")
	assert.Equal(t, "team-team-1", combined.ExecutionID)
	assert.Equal(t, "sess-1", combined.SessionID)
	assert.Equal(t, "online", combined.Source)
	assert.Len(t, combined.Steps, 3)
	// 按 start_time_ms 排序
	assert.Equal(t, 100, combined.Steps[0].StartTimeMs)
	assert.Equal(t, 150, combined.Steps[1].StartTimeMs)
	assert.Equal(t, 200, combined.Steps[2].StartTimeMs)
	// 成本
	assert.Equal(t, 80, combined.Cost["input_tokens"])
	assert.Equal(t, 40, combined.Cost["output_tokens"])
	// 元数据
	assert.Equal(t, 2, combined.Meta["member_count"])
}

// TestBuildCombinedTrajectory_零成本 零成本时 cost 为 nil
func TestBuildCombinedTrajectory_零成本(t *testing.T) {
	members := map[string]*Trajectory{
		"m1": {Steps: []*TrajectoryStep{{Kind: StepKindLLM, StartTimeMs: 100}}},
	}

	combined := buildCombinedTrajectory(members, "team-1", "sess-1")
	assert.Nil(t, combined.Cost)
}
