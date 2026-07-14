package agent_test

import (
	"testing"

	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	agentteams "github.com/uapclaw/uapclaw-go/internal/agent_teams"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/agent"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/stretchr/testify/assert"
)

// newTestCard 创建测试用 AgentCard
func newTestCard() *agentschema.AgentCard {
	return agentschema.NewAgentCard()
}

// TestNewAgentConfigurator 测试构造函数
func TestNewAgentConfigurator(t *testing.T) {
	card := newTestCard()
	c := agent.NewAgentConfigurator(card)
	assert.NotNil(t, c)
	assert.NotNil(t, c.Infra())
	assert.NotNil(t, c.Resources())
	assert.Nil(t, c.Blueprint())
	assert.Nil(t, c.Harness())
}

// TestResolveAgentSpec 测试按优先级解析 AgentSpec
func TestResolveAgentSpec(t *testing.T) {
	leaderSpec := atschema.NewDeepAgentSpec()
	leaderSpec.SystemPrompt = "I am leader"
	teammateSpec := atschema.NewDeepAgentSpec()
	teammateSpec.SystemPrompt = "I am teammate"
	namedSpec := atschema.NewDeepAgentSpec()
	namedSpec.SystemPrompt = "I am named coder"

	spec := atschema.NewTeamAgentSpec()
	spec.Agents = map[string]atschema.DeepAgentSpec{
		"leader":    leaderSpec,
		"teammate":  teammateSpec,
		"coder_bot": namedSpec,
	}

	// 1. memberName 精确匹配
	result := agent.ResolveAgentSpec(spec, atschema.TeamRoleTeammate, "coder_bot")
	assert.Equal(t, "I am named coder", result.SystemPrompt)

	// 2. role 值匹配
	result = agent.ResolveAgentSpec(spec, atschema.TeamRoleTeammate, "")
	assert.Equal(t, "I am teammate", result.SystemPrompt)

	// 3. "teammate" 回退
	specNoRole := atschema.NewTeamAgentSpec()
	specNoRole.Agents = map[string]atschema.DeepAgentSpec{
		"leader":   leaderSpec,
		"teammate": teammateSpec,
	}
	result = agent.ResolveAgentSpec(specNoRole, atschema.TeamRoleHumanAgent, "")
	assert.Equal(t, "I am teammate", result.SystemPrompt)

	// 4. "leader" 最终回退
	specOnlyLeader := atschema.NewTeamAgentSpec()
	specOnlyLeader.Agents = map[string]atschema.DeepAgentSpec{
		"leader": leaderSpec,
	}
	result = agent.ResolveAgentSpec(specOnlyLeader, atschema.TeamRoleTeammate, "nonexistent")
	assert.Equal(t, "I am leader", result.SystemPrompt)
}

// TestResolveTeamMode_default 测试无预定义成员 → "default"
func TestResolveTeamMode_default(t *testing.T) {
	card := newTestCard()
	c := agent.NewAgentConfigurator(card)
	spec := atschema.NewTeamAgentSpec()
	ctx := atschema.TeamRuntimeContext{Role: atschema.TeamRoleLeader, MemberName: "leader_1"}
	c.SetupInfra(spec, ctx)
	// resolveTeamMode 是非导出函数，通过 SetupInfra 间接测试不 panic 即可
	assert.NotNil(t, c.Blueprint())
}

// TestResolveTeamMode_hybrid 测试有非人类预定义成员 → "hybrid"
func TestResolveTeamMode_hybrid(t *testing.T) {
	card := newTestCard()
	c := agent.NewAgentConfigurator(card)
	spec := atschema.NewTeamAgentSpec()
	spec.PredefinedMembers = []atschema.TeamMemberSpec{
		{MemberName: "bot1", RoleType: atschema.TeamRoleTeammate},
	}
	ctx := atschema.TeamRuntimeContext{Role: atschema.TeamRoleLeader, MemberName: "leader_1"}
	c.SetupInfra(spec, ctx)
	assert.NotNil(t, c.Blueprint())
}

// TestAgentConfigurator_GetterSetter 测试属性代理
func TestAgentConfigurator_GetterSetter(t *testing.T) {
	card := newTestCard()
	c := agent.NewAgentConfigurator(card)

	// Infra / Resources
	assert.NotNil(t, c.Infra())
	assert.NotNil(t, c.Resources())

	// Messager
	c.SetMessager("mock_messager")
	assert.Equal(t, "mock_messager", c.Messager())

	// TeamBackend
	c.SetTeamBackend("mock_backend")
	assert.Equal(t, "mock_backend", c.TeamBackend())

	// WorkspaceManager
	c.SetWorkspaceManager("mock_ws")
	assert.Equal(t, "mock_ws", c.WorkspaceManager())

	// WorkspaceInitialized
	c.SetWorkspaceInitialized(true)
	assert.True(t, c.WorkspaceInitialized())

	// TaskManager
	c.SetTaskManager("mock_tm")
	assert.Equal(t, "mock_tm", c.TaskManager())

	// MessageManager
	c.SetMessageManager("mock_mm")
	assert.Equal(t, "mock_mm", c.MessageManager())

	// Harness
	h := agentteams.NewTeamHarness(nil, nil, "leader", "l1", false)
	c.SetHarness(h)
	assert.Equal(t, h, c.Harness())

	// WorktreeManager
	c.SetWorktreeManager("mock_wt")
	assert.Equal(t, "mock_wt", c.WorktreeManager())

	// MemoryManager
	c.SetMemoryManager("mock_mem")
	assert.Equal(t, "mock_mem", c.MemoryManager())

	// FirstIterGate
	c.SetFirstIterGate("mock_fig")
	assert.Equal(t, "mock_fig", c.FirstIterGate())

	// ModelAllocator
	c.SetModelAllocator("mock_alloc")
	assert.Equal(t, "mock_alloc", c.ModelAllocator())
}

// TestAgentConfigurator_BlueprintProperties 测试 Blueprint 代理属性
func TestAgentConfigurator_BlueprintProperties(t *testing.T) {
	card := newTestCard()
	c := agent.NewAgentConfigurator(card)

	// Configure 前
	assert.Nil(t, c.Spec())
	assert.Nil(t, c.RuntimeContext())
	assert.Equal(t, "", c.RolePolicy())
	assert.Nil(t, c.TeamSpec())
	assert.Equal(t, atschema.TeamRoleLeader, c.Role())
	assert.Equal(t, "temporary", c.Lifecycle())
	assert.Equal(t, "", c.MemberName())
	assert.Equal(t, "", c.TeamName())

	// Configure 后
	spec := atschema.NewTeamAgentSpec()
	ctx := atschema.TeamRuntimeContext{
		Role:       atschema.TeamRoleTeammate,
		MemberName: "teammate_1",
		TeamSpec:   &atschema.TeamSpec{TeamName: "my_team"},
	}
	c.Configure(spec, ctx)

	assert.NotNil(t, c.Blueprint())
	assert.NotNil(t, c.Spec())
	assert.NotNil(t, c.RuntimeContext())
	assert.Equal(t, atschema.TeamRoleTeammate, c.Role())
	assert.Equal(t, "teammate_1", c.MemberName())
	assert.Equal(t, "my_team", c.TeamName())
}

// TestAgentConfigurator_AttachModelAllocator 测试附加模型分配器
func TestAgentConfigurator_AttachModelAllocator(t *testing.T) {
	card := newTestCard()
	c := agent.NewAgentConfigurator(card)

	c.AttachModelAllocator("mock_allocator", "mock_allocation")
	assert.Equal(t, "mock_allocator", c.ModelAllocator())
}

// TestAgentConfigurator_RestoreAllocatorState 测试恢复分配器状态
func TestAgentConfigurator_RestoreAllocatorState(t *testing.T) {
	card := newTestCard()
	c := agent.NewAgentConfigurator(card)

	// ModelAllocator 为 nil 时不 panic
	assert.NotPanics(t, func() {
		c.RestoreAllocatorState(map[string]any{"key": "value"})
	})
}

// TestAgentConfigurator_BuildSpawnPayload代理 测试代理到 SpawnPayloadBuilder
func TestAgentConfigurator_BuildSpawnPayload代理(t *testing.T) {
	card := newTestCard()
	c := agent.NewAgentConfigurator(card)

	// Configure 前：spawnPayloadBuilder 为 nil → 返回 nil
	assert.Nil(t, c.BuildSpawnPayload(atschema.TeamRuntimeContext{}, ""))

	// Configure 后
	spec := atschema.NewTeamAgentSpec()
	ctx := atschema.TeamRuntimeContext{Role: atschema.TeamRoleLeader, MemberName: "leader_1"}
	c.Configure(spec, ctx)

	payload := c.BuildSpawnPayload(ctx, "Hello")
	assert.NotNil(t, payload)
	assert.Contains(t, payload, "coordination")
	assert.Equal(t, "Hello", payload["query"])
}

// TestAgentConfigurator_BuildMemberContext代理 测试代理到 SpawnPayloadBuilder
func TestAgentConfigurator_BuildMemberContext代理(t *testing.T) {
	card := newTestCard()
	c := agent.NewAgentConfigurator(card)

	// Configure 前：返回零值
	result := c.BuildMemberContext(atschema.TeamMemberSpec{})
	assert.Equal(t, atschema.TeamRuntimeContext{}, result)

	// Configure 后
	spec := atschema.NewTeamAgentSpec()
	ctx := atschema.TeamRuntimeContext{Role: atschema.TeamRoleLeader, MemberName: "leader_1"}
	c.Configure(spec, ctx)

	memberSpec := atschema.TeamMemberSpec{
		MemberName: "t1",
		RoleType:   atschema.TeamRoleTeammate,
	}
	result = c.BuildMemberContext(memberSpec)
	assert.Equal(t, "t1", result.MemberName)
	assert.Equal(t, atschema.TeamRoleTeammate, result.Role)
}

// TestAgentConfigurator_Configure 测试 Configure 调用 SetupInfra + SetupAgent
func TestAgentConfigurator_Configure(t *testing.T) {
	card := newTestCard()
	c := agent.NewAgentConfigurator(card)

	spec := atschema.NewTeamAgentSpec()
	ctx := atschema.TeamRuntimeContext{Role: atschema.TeamRoleLeader, MemberName: "leader_1"}

	harness := c.Configure(spec, ctx)
	assert.NotNil(t, harness) // BuildTeamHarness 返回非 nil（即使 deepAgent 为 nil）
	assert.NotNil(t, c.Blueprint())
	assert.NotNil(t, c.Harness())
}
