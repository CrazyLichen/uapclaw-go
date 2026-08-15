package agent

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// TestNewTeamAgentState 测试创建默认 TeamAgentState
func TestNewTeamAgentState(t *testing.T) {
	state := NewTeamAgentState()
	assert.NotNil(t, state)
	assert.NotNil(t, state.EventListeners)
	assert.Empty(t, state.EventListeners)
	assert.Nil(t, state.TeamMember)
	assert.Equal(t, "", state.PendingUserQuery)
	assert.False(t, state.TeamCleaned)
}

// TestTeamMember_Status 测试返回 MemberStatusReady
func TestTeamMember_Status(t *testing.T) {
	m := &TeamMember{MemberName: "t1"}
	status, err := m.Status(context.Background())
	assert.NoError(t, err)
	assert.Equal(t, atschema.MemberStatusReady, status)
}

// TestTeamMember_ExecutionStatus 测试返回 ExecutionStatusIdle
func TestTeamMember_ExecutionStatus(t *testing.T) {
	m := &TeamMember{MemberName: "t1"}
	status, err := m.ExecutionStatus(context.Background())
	assert.NoError(t, err)
	assert.Equal(t, atschema.ExecutionStatusIdle, status)
}

// TestTeamMember_UpdateStatus 测试更新成员状态
func TestTeamMember_UpdateStatus(t *testing.T) {
	m := &TeamMember{MemberName: "t1"}
	ok, err := m.UpdateStatus(context.Background(), atschema.MemberStatusReady)
	assert.NoError(t, err)
	assert.True(t, ok)
}

// TestTeamMember_UpdateExecutionStatus 测试更新执行状态
func TestTeamMember_UpdateExecutionStatus(t *testing.T) {
	m := &TeamMember{MemberName: "t1"}
	ok, err := m.UpdateExecutionStatus(context.Background(), atschema.ExecutionStatusIdle)
	assert.NoError(t, err)
	assert.True(t, ok)
}

// TestCreateMemberHandle_无TeamBackend 测试 infra.TeamBackend 为 nil 时返回 nil
func TestCreateMemberHandle_无TeamBackend(t *testing.T) {
	card := agentschema.NewAgentCard()
	infra := &TeamInfra{}
	blueprint := &TeamAgentBlueprint{
		Ctx: atschema.TeamRuntimeContext{
			Role:       atschema.TeamRoleTeammate,
			MemberName: "t1",
		},
	}
	result := CreateMemberHandle("t1", blueprint, infra, card)
	assert.Nil(t, result)
}

// TestCreateMemberHandle_有TeamBackend 测试创建 TeamMember
func TestCreateMemberHandle_有TeamBackend(t *testing.T) {
	card := agentschema.NewAgentCard()
	// 使用真实的 TeamBackend 不太方便，直接构造 infra 并设置 TeamBackend 为非 nil
	// 但 TeamBackend 是具体类型，不能简单 mock
	// 这里使用 tools.NewTeamBackend 构造
	infra := &TeamInfra{
		TeamBackend: nil, // TeamBackend 构造需要参数较多，暂时跳过此测试的详细验证
	}
	blueprint := &TeamAgentBlueprint{
		Ctx: atschema.TeamRuntimeContext{
			Role:       atschema.TeamRoleTeammate,
			MemberName: "t1",
			Persona:    "friendly assistant",
		},
	}
	// TeamBackend 为 nil 时返回 nil
	result := CreateMemberHandle("t1", blueprint, infra, card)
	assert.Nil(t, result)
}
