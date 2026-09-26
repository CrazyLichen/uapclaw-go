package evolution

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/evolving/trajectory"
)

// ──────────────────────────── 构造测试 ────────────────────────────

// TestNewTeamSkillCreateRail_默认值 验证默认构造
func TestNewTeamSkillCreateRail_默认值(t *testing.T) {
	r := NewTeamSkillCreateRail("/tmp/skills")
	assert.Equal(t, "/tmp/skills", r.skillsDir)
	assert.True(t, r.autoTrigger)
	assert.Equal(t, defaultMinTeamMembers, r.minTeamMembers)
	assert.Equal(t, "cn", r.language)
	assert.Equal(t, teamSkillCreatePriority, r.Priority())
}

// TestNewTeamSkillCreateRail_自定义选项 验证选项函数
func TestNewTeamSkillCreateRail_自定义选项(t *testing.T) {
	r := NewTeamSkillCreateRail("/tmp/skills",
		WithTeamSkillCreateAutoTrigger(false),
		WithTeamSkillCreateMinTeamMembers(5),
		WithTeamSkillCreateLanguage("en"),
	)
	assert.False(t, r.autoTrigger)
	assert.Equal(t, 5, r.minTeamMembers)
	assert.Equal(t, "en", r.language)
}

// ──────────────────────────── Priority 测试 ────────────────────────────

// TestTeamSkillCreateRail_Priority 验证优先级为 85
func TestTeamSkillCreateRail_Priority(t *testing.T) {
	r := NewTeamSkillCreateRail("/tmp/skills")
	assert.Equal(t, 85, r.Priority())
}

// ──────────────────────────── NotifyTeamCompleted 测试 ────────────────────────────

// TestTeamSkillCreateRail_NotifyTeamCompleted_禁用AutoTrigger 验证 autoTrigger 关闭时忽略
func TestTeamSkillCreateRail_NotifyTeamCompleted_禁用AutoTrigger(t *testing.T) {
	r := NewTeamSkillCreateRail("/tmp/skills", WithTeamSkillCreateAutoTrigger(false))
	cbc := &agentinterfaces.AgentCallbackContext{}
	result := r.NotifyTeamCompleted(cbc)
	assert.False(t, result)
}

// TestTeamSkillCreateRail_NotifyTeamCompleted_无Builder 验证无 builder 时返回 false
func TestTeamSkillCreateRail_NotifyTeamCompleted_无Builder(t *testing.T) {
	r := NewTeamSkillCreateRail("/tmp/skills")
	// EvolutionRail 基类未初始化 builder
	cbc := &agentinterfaces.AgentCallbackContext{}
	result := r.NotifyTeamCompleted(cbc)
	assert.False(t, result)
}

// ──────────────────────────── EvolutionExtension 空方法测试 ────────────────────────────

// TestTeamSkillCreateRail_OnBeforeInvoke 验证空实现不报错
func TestTeamSkillCreateRail_OnBeforeInvoke(t *testing.T) {
	r := NewTeamSkillCreateRail("/tmp/skills")
	err := r.OnBeforeInvoke(context.Background(), nil)
	assert.NoError(t, err)
}

// TestTeamSkillCreateRail_OnAfterModelCall 验证空实现不报错
func TestTeamSkillCreateRail_OnAfterModelCall(t *testing.T) {
	r := NewTeamSkillCreateRail("/tmp/skills")
	err := r.OnAfterModelCall(context.Background(), nil)
	assert.NoError(t, err)
}

// TestTeamSkillCreateRail_OnAfterToolCall 验证空实现不报错
func TestTeamSkillCreateRail_OnAfterToolCall(t *testing.T) {
	r := NewTeamSkillCreateRail("/tmp/skills")
	err := r.OnAfterToolCall(context.Background(), nil)
	assert.NoError(t, err)
}

// TestTeamSkillCreateRail_OnAfterInvoke 验证不报错
func TestTeamSkillCreateRail_OnAfterInvoke(t *testing.T) {
	r := NewTeamSkillCreateRail("/tmp/skills")
	err := r.OnAfterInvoke(context.Background(), nil)
	assert.NoError(t, err)
}

// TestTeamSkillCreateRail_OnAfterTaskIteration 验证不报错
func TestTeamSkillCreateRail_OnAfterTaskIteration(t *testing.T) {
	r := NewTeamSkillCreateRail("/tmp/skills")
	err := r.OnAfterTaskIteration(context.Background(), nil)
	assert.NoError(t, err)
}

// TestTeamSkillCreateRail_RunEvolution 验证空实现
func TestTeamSkillCreateRail_RunEvolution(t *testing.T) {
	r := NewTeamSkillCreateRail("/tmp/skills")
	err := r.RunEvolution(context.Background(), nil, nil)
	assert.NoError(t, err)
}

// TestTeamSkillCreateRail_AllowEvolutionTrigger 验证始终允许
func TestTeamSkillCreateRail_AllowEvolutionTrigger(t *testing.T) {
	r := NewTeamSkillCreateRail("/tmp/skills")
	assert.True(t, r.AllowEvolutionTrigger(TriggerNone, nil))
}

// TestTeamSkillCreateRail_GetEvolutionTotalTimeoutSecs 验证返回 0
func TestTeamSkillCreateRail_GetEvolutionTotalTimeoutSecs(t *testing.T) {
	r := NewTeamSkillCreateRail("/tmp/skills")
	assert.Equal(t, float64(0), r.GetEvolutionTotalTimeoutSecs())
}

// ──────────────────────────── 非导出函数测试 ────────────────────────────

// TestCountSpawnMemberCalls_无Builder 验证无 builder 时返回 0
func TestCountSpawnMemberCalls_无Builder(t *testing.T) {
	r := NewTeamSkillCreateRail("/tmp/skills")
	assert.Equal(t, 0, r.countSpawnMemberCalls())
}

// TestCountSpawnMemberCalls_有Builder 验证有 builder 和步骤时正确计数
func TestCountSpawnMemberCalls_有Builder(t *testing.T) {
	r := NewTeamSkillCreateRail("/tmp/skills")

	// 手动设置 builder 和步骤
	builder := trajectory.NewTrajectoryBuilder("test-session", "online")
	builder.RecordStep(&trajectory.TrajectoryStep{
		Kind: trajectory.StepKindTool,
		Detail: &trajectory.ToolCallDetail{
			ToolName: "spawn_member",
		},
	})
	builder.RecordStep(&trajectory.TrajectoryStep{
		Kind: trajectory.StepKindTool,
		Detail: &trajectory.ToolCallDetail{
			ToolName: "other_tool",
		},
	})
	builder.RecordStep(&trajectory.TrajectoryStep{
		Kind: trajectory.StepKindTool,
		Detail: &trajectory.ToolCallDetail{
			ToolName: "spawn_member_team_a",
		},
	})

	// 需要设置 builder — 同包测试可直接访问非导出字段
	r.EvolutionRail.builder = builder

	count := r.countSpawnMemberCalls()
	assert.Equal(t, 2, count) // "spawn_member" 和 "spawn_member_team_a"
}

// TestBuildFollowUpPrompt_CN 验证中文提示词
func TestBuildFollowUpPrompt_CN(t *testing.T) {
	r := NewTeamSkillCreateRail("/my/skills", WithTeamSkillCreateLanguage("cn"))
	prompt := r.buildFollowUpPrompt()
	assert.Contains(t, prompt, "/my/skills")
	assert.Contains(t, prompt, "确认")
}

// TestBuildFollowUpPrompt_EN 验证英文提示词
func TestBuildFollowUpPrompt_EN(t *testing.T) {
	r := NewTeamSkillCreateRail("/my/skills", WithTeamSkillCreateLanguage("en"))
	prompt := r.buildFollowUpPrompt()
	assert.Contains(t, prompt, "/my/skills")
	assert.Contains(t, prompt, "confirm")
}

// TestCanEnqueueCreationFollowUp_基本条件 验证基本判断条件
func TestCanEnqueueCreationFollowUp_基本条件(t *testing.T) {
	r := NewTeamSkillCreateRail("/tmp/skills")

	// autoTrigger=true, sessionID 非空, completedSessionID 匹配
	r.completedSessionID = "sess-1"
	r.proposedSpawnCounts = make(map[string]int)

	assert.True(t, r.canEnqueueCreationFollowUp("sess-1", 3))
	assert.False(t, r.canEnqueueCreationFollowUp("sess-2", 3)) // sessionID 不匹配
	assert.False(t, r.canEnqueueCreationFollowUp("sess-1", 1)) // 低于阈值 2
}

// TestCanEnqueueCreationFollowUp_防重复 验证防重复条件
func TestCanEnqueueCreationFollowUp_防重复(t *testing.T) {
	r := NewTeamSkillCreateRail("/tmp/skills")
	r.completedSessionID = "sess-1"
	r.proposedSpawnCounts = map[string]int{"sess-1": 3}

	// 相同 session 的 spawn count 不超过已提议的，应跳过
	assert.False(t, r.canEnqueueCreationFollowUp("sess-1", 3))
	assert.False(t, r.canEnqueueCreationFollowUp("sess-1", 2))
	assert.True(t, r.canEnqueueCreationFollowUp("sess-1", 4)) // 更高计数可再提议
}

// TestCanEnqueueCreationFollowUp_禁用AutoTrigger 验证关闭时跳过
func TestCanEnqueueCreationFollowUp_禁用AutoTrigger(t *testing.T) {
	r := NewTeamSkillCreateRail("/tmp/skills", WithTeamSkillCreateAutoTrigger(false))
	r.completedSessionID = "sess-1"
	assert.False(t, r.canEnqueueCreationFollowUp("sess-1", 5))
}
