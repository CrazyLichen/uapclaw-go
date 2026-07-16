package agent

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// TestNewTeamAgent_配置器 测试 NewTeamAgent 构造时创建 AgentConfigurator
func TestNewTeamAgent_配置器(t *testing.T) {
	card := agentschema.NewAgentCard()
	a := NewTeamAgent(card)

	assert.NotNil(t, a)
	assert.NotNil(t, a.configurator, "configurator 应在构造时创建")
	assert.NotNil(t, a.State())
}

// TestTeamAgent_Blueprint_配置前 测试配置前 Blueprint 为 nil
func TestTeamAgent_Blueprint_配置前(t *testing.T) {
	card := agentschema.NewAgentCard()
	a := NewTeamAgent(card)

	assert.Nil(t, a.Blueprint())
}

// TestTeamAgent_属性代理_配置前 测试配置前属性代理返回初始零值
func TestTeamAgent_属性代理_配置前(t *testing.T) {
	card := agentschema.NewAgentCard()
	a := NewTeamAgent(card)

	// Infra / Resources 由 NewAgentConfigurator 构造，非 nil 但内部字段为零值
	assert.NotNil(t, a.Infra())
	assert.NotNil(t, a.Resources())
	assert.Nil(t, a.Spec())
	assert.Nil(t, a.RuntimeContext())
	assert.Nil(t, a.Harness())
	assert.Equal(t, atschema.TeamRoleLeader, a.Role())
	assert.Equal(t, "temporary", a.Lifecycle(), "无 Blueprint 时默认返回 temporary")
	assert.Nil(t, a.TeamSpec())
	assert.Equal(t, "", a.MemberName())
	assert.Nil(t, a.MessageManager())
	assert.Nil(t, a.TaskManager())
	assert.Nil(t, a.TeamBackend())
	assert.Equal(t, "", a.TeamName())
	assert.False(t, a.IsAgentReady())
}

// TestTeamAgent_Configure 测试 Configure 调用 SetupInfra + SetupAgent
func TestTeamAgent_Configure(t *testing.T) {
	card := agentschema.NewAgentCard()
	a := NewTeamAgent(card)

	spec := atschema.NewTeamAgentSpec()
	ctx := atschema.TeamRuntimeContext{
		Role:       atschema.TeamRoleLeader,
		MemberName: "leader_1",
		TeamSpec:   &atschema.TeamSpec{TeamName: "test_team"},
	}

	result := a.Configure(context.Background(), spec, ctx)

	assert.Equal(t, a, result, "Configure 应返回自身")
	assert.NotNil(t, a.Blueprint(), "Configure 后 Blueprint 应非 nil")
	assert.NotNil(t, a.Infra(), "Configure 后 Infra 应非 nil")
	assert.NotNil(t, a.Resources(), "Configure 后 Resources 应非 nil")
	assert.NotNil(t, a.Spec(), "Configure 后 Spec 应非 nil")
	assert.NotNil(t, a.RuntimeContext(), "Configure 后 RuntimeContext 应非 nil")
	assert.NotNil(t, a.Harness(), "Configure 后 Harness 应非 nil")
	assert.Equal(t, atschema.TeamRoleLeader, a.Role())
	assert.Equal(t, "leader_1", a.MemberName())
	assert.Equal(t, "test_team", a.TeamName())
	assert.True(t, a.IsAgentReady(), "Harness 非-nil 时 IsAgentReady 应返回 true")
}

// TestTeamAgent_Configure_无成员名 测试 Configure 时 MemberName 为空不创建 TeamMember
func TestTeamAgent_Configure_无成员名(t *testing.T) {
	card := agentschema.NewAgentCard()
	a := NewTeamAgent(card)

	spec := atschema.NewTeamAgentSpec()
	ctx := atschema.TeamRuntimeContext{
		Role: atschema.TeamRoleLeader,
		// MemberName 为空
	}

	a.Configure(context.Background(), spec, ctx)
	assert.Nil(t, a.TeamMemberHandle(), "MemberName 为空时不创建 TeamMember")
}

// TestTeamAgent_Configure_有成员名 测试 Configure 时有 MemberName 但无 TeamBackend
func TestTeamAgent_Configure_有成员名(t *testing.T) {
	card := agentschema.NewAgentCard()
	a := NewTeamAgent(card)

	spec := atschema.NewTeamAgentSpec()
	ctx := atschema.TeamRuntimeContext{
		Role:       atschema.TeamRoleLeader,
		MemberName: "leader_1",
	}

	a.Configure(context.Background(), spec, ctx)
	// TeamBackend 为 nil（未实现），所以 CreateMemberHandle 返回 nil
	assert.Nil(t, a.TeamMemberHandle())
}

// TestTeamAgent_BuildSpawnPayload 测试代理到 configurator
func TestTeamAgent_BuildSpawnPayload(t *testing.T) {
	card := agentschema.NewAgentCard()
	a := NewTeamAgent(card)

	// 配置前
	assert.Nil(t, a.BuildSpawnPayload(atschema.TeamRuntimeContext{}, ""))

	// 配置后
	spec := atschema.NewTeamAgentSpec()
	ctx := atschema.TeamRuntimeContext{Role: atschema.TeamRoleLeader, MemberName: "leader_1"}
	a.Configure(context.Background(), spec, ctx)

	payload := a.BuildSpawnPayload(ctx, "Hello team")
	assert.NotNil(t, payload)
	assert.Contains(t, payload, "coordination")
	assert.Equal(t, "Hello team", payload["query"])
}

// TestTeamAgent_BuildMemberContext 测试代理到 configurator
func TestTeamAgent_BuildMemberContext(t *testing.T) {
	card := agentschema.NewAgentCard()
	a := NewTeamAgent(card)

	// 配置前
	result := a.BuildMemberContext(atschema.TeamMemberSpec{})
	assert.Equal(t, atschema.TeamRuntimeContext{}, result)

	// 配置后
	spec := atschema.NewTeamAgentSpec()
	ctx := atschema.TeamRuntimeContext{Role: atschema.TeamRoleLeader, MemberName: "leader_1"}
	a.Configure(context.Background(), spec, ctx)

	memberSpec := atschema.TeamMemberSpec{
		MemberName: "t1",
		RoleType:   atschema.TeamRoleTeammate,
	}
	result = a.BuildMemberContext(memberSpec)
	assert.Equal(t, "t1", result.MemberName)
	assert.Equal(t, atschema.TeamRoleTeammate, result.Role)
}

// TestTeamAgent_BuildSpawnConfig 测试代理到 configurator
func TestTeamAgent_BuildSpawnConfig(t *testing.T) {
	card := agentschema.NewAgentCard()
	a := NewTeamAgent(card)

	// 配置前
	assert.Nil(t, a.BuildSpawnConfig(atschema.TeamRuntimeContext{}))

	// 配置后
	spec := atschema.NewTeamAgentSpec()
	ctx := atschema.TeamRuntimeContext{Role: atschema.TeamRoleLeader, MemberName: "leader_1"}
	a.Configure(context.Background(), spec, ctx)

	assert.NotNil(t, a.BuildSpawnConfig(ctx)) // 配置后返回 SpawnAgentConfig
}

// TestTeamAgent_AttachModelAllocator 测试代理到 configurator
func TestTeamAgent_AttachModelAllocator(t *testing.T) {
	card := agentschema.NewAgentCard()
	a := NewTeamAgent(card)

	// 不 panic 即可
	a.AttachModelAllocator("mock_allocator", "mock_allocation")
}

// TestTeamAgent_RestoreAllocatorState 测试代理到 configurator
func TestTeamAgent_RestoreAllocatorState(t *testing.T) {
	card := agentschema.NewAgentCard()
	a := NewTeamAgent(card)

	// 不 panic 即可
	a.RestoreAllocatorState(map[string]any{"key": "value"})
}

// TestTeamAgent_UpdateModelPool 测试代理到 configurator
func TestTeamAgent_UpdateModelPool(t *testing.T) {
	card := agentschema.NewAgentCard()
	a := NewTeamAgent(card)

	// 不 panic 即可
	a.UpdateModelPool("mock_pool")
}

// TestTeamAgent_RegisterRail 测试代理到 harness
func TestTeamAgent_RegisterRail(t *testing.T) {
	card := agentschema.NewAgentCard()
	a := NewTeamAgent(card)

	// 配置前：configurator.Harness() 为 nil
	result, err := a.RegisterRail(context.Background(), "mock_rail")
	assert.Equal(t, a, result)
	assert.NoError(t, err)
}

// TestTeamAgent_UnregisterRail 测试代理到 harness
func TestTeamAgent_UnregisterRail(t *testing.T) {
	card := agentschema.NewAgentCard()
	a := NewTeamAgent(card)

	result, err := a.UnregisterRail(context.Background(), "mock_rail")
	assert.Equal(t, a, result)
	assert.NoError(t, err)
}
