package agent

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	agentteams "github.com/uapclaw/uapclaw-go/internal/agent_teams"
	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/models"
	runnerspawn "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/spawn"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// TestResolveTeamMode_场景描述 测试 resolveTeamMode 各分支。
func TestResolveTeamMode_场景描述(t *testing.T) {
	tests := []struct {
		name string
		spec atschema.TeamAgentSpec
		want string
	}{
		{
			name: "TeamMode已设置时直接返回",
			spec: atschema.TeamAgentSpec{
				TeamMode: "hybrid",
			},
			want: "hybrid",
		},
		{
			name: "TeamMode为自定义值时直接返回",
			spec: atschema.TeamAgentSpec{
				TeamMode: "custom_mode",
			},
			want: "custom_mode",
		},
		{
			name: "TeamMode为空且无非人类成员时返回default",
			spec: atschema.TeamAgentSpec{
				PredefinedMembers: []atschema.TeamMemberSpec{
					{RoleType: atschema.TeamRoleHumanAgent},
				},
			},
			want: "default",
		},
		{
			name: "TeamMode为空且无预定义成员时返回default",
			spec: atschema.TeamAgentSpec{
				PredefinedMembers: []atschema.TeamMemberSpec{},
			},
			want: "default",
		},
		{
			name: "TeamMode为空且有非人类成员时返回hybrid",
			spec: atschema.TeamAgentSpec{
				PredefinedMembers: []atschema.TeamMemberSpec{
					{RoleType: atschema.TeamRoleHumanAgent},
					{RoleType: atschema.TeamRoleTeammate},
				},
			},
			want: "hybrid",
		},
		{
			name: "TeamMode为空且第一个成员就是非人类时返回hybrid",
			spec: atschema.TeamAgentSpec{
				PredefinedMembers: []atschema.TeamMemberSpec{
					{RoleType: atschema.TeamRoleTeammate},
					{RoleType: atschema.TeamRoleHumanAgent},
				},
			},
			want: "hybrid",
		},
		{
			name: "TeamMode为空且成员为Leader时返回hybrid",
			spec: atschema.TeamAgentSpec{
				PredefinedMembers: []atschema.TeamMemberSpec{
					{RoleType: atschema.TeamRoleLeader},
				},
			},
			want: "hybrid",
		},
		{
			name: "TeamMode已设置时忽略成员类型",
			spec: atschema.TeamAgentSpec{
				TeamMode: "default",
				PredefinedMembers: []atschema.TeamMemberSpec{
					{RoleType: atschema.TeamRoleTeammate},
				},
			},
			want: "default",
		},
		{
			name: "空Spec返回default",
			spec: atschema.TeamAgentSpec{},
			want: "default",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveTeamMode(tt.spec)
			if got != tt.want {
				t.Errorf("resolveTeamMode() = %v, 期望 %v", got, tt.want)
			}
		})
	}
}

// TestNewAgentConfigurator 测试构造函数
func TestNewAgentConfigurator(t *testing.T) {
	card := agentschema.NewAgentCard()
	c := NewAgentConfigurator(card)
	require.NotNil(t, c)
	assert.Equal(t, card, c.card)
	assert.NotNil(t, c.Infra())
	assert.NotNil(t, c.Resources())
}

// TestResolveAgentSpec 测试按优先级匹配 AgentSpec
func TestResolveAgentSpec(t *testing.T) {
	spec := atschema.TeamAgentSpec{
		Agents: map[string]atschema.DeepAgentSpec{
			"leader":   {SystemPrompt: "leader-prompt"},
			"teammate": {SystemPrompt: "teammate-prompt"},
			"analyst":  {SystemPrompt: "analyst-prompt"},
			"member_1": {SystemPrompt: "member1-prompt"},
		},
	}

	tests := []struct {
		name              string
		role              atschema.TeamRole
		memberName        string
		wantSystemPrompt  string
	}{
		{"memberName精确匹配", atschema.TeamRoleTeammate, "member_1", "member1-prompt"},
		{"role匹配", atschema.TeamRole("analyst"), "", "analyst-prompt"},
		{"teammate兜底", atschema.TeamRoleTeammate, "nonexistent", "teammate-prompt"},
		{"leader兜底", atschema.TeamRoleLeader, "nonexistent", "leader-prompt"},
		{"memberName优先于role", atschema.TeamRole("analyst"), "member_1", "member1-prompt"},
		{"空memberName走role", atschema.TeamRole("analyst"), "", "analyst-prompt"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveAgentSpec(spec, tt.role, tt.memberName)
			assert.Equal(t, tt.wantSystemPrompt, got.SystemPrompt)
		})
	}
}

// TestResolveAgentSpec_空Spec 测试空 Agents map
func TestResolveAgentSpec_空Spec(t *testing.T) {
	spec := atschema.TeamAgentSpec{Agents: map[string]atschema.DeepAgentSpec{}}
	got := ResolveAgentSpec(spec, atschema.TeamRoleLeader, "")
	assert.Equal(t, atschema.DeepAgentSpec{}, got)
}

// TestSetupInfraOption 测试 SetupInfra 的 Option 函数
func TestSetupInfraOption(t *testing.T) {
	t.Run("WithOnTeammateCreated", func(t *testing.T) {
		called := false
		cb := func(name string) { called = true }
		opt := WithOnTeammateCreated(cb)
		cfg := &setupInfraConfig{}
		opt(cfg)
		assert.NotNil(t, cfg.onTeammateCreated)
		cfg.onTeammateCreated("test")
		assert.True(t, called)
	})

	t.Run("WithOnTeamCleaned", func(t *testing.T) {
		called := false
		cb := func(name string) { called = true }
		opt := WithOnTeamCleaned(cb)
		cfg := &setupInfraConfig{}
		opt(cfg)
		assert.NotNil(t, cfg.onTeamCleaned)
		cfg.onTeamCleaned("test")
		assert.True(t, called)
	})

	t.Run("WithOnTeamBuilt", func(t *testing.T) {
		called := false
		cb := func(name string) { called = true }
		opt := WithOnTeamBuilt(cb)
		cfg := &setupInfraConfig{}
		opt(cfg)
		assert.NotNil(t, cfg.onTeamBuilt)
		cfg.onTeamBuilt("test")
		assert.True(t, called)
	})
}

// TestSetupTeamBackendOption 测试 SetupTeamBackend 的 Option 函数
func TestSetupTeamBackendOption(t *testing.T) {
	t.Run("WithBackendOnTeamCleaned", func(t *testing.T) {
		called := false
		cb := func(ctx context.Context) error { called = true; return nil }
		opt := WithBackendOnTeamCleaned(cb)
		cfg := &setupTeamBackendConfig{}
		opt(cfg)
		assert.NotNil(t, cfg.onTeamCleaned)
		cfg.onTeamCleaned(context.Background())
		assert.True(t, called)
	})

	t.Run("WithBackendOnTeamBuilt", func(t *testing.T) {
		called := false
		cb := func(ctx context.Context) error { called = true; return nil }
		opt := WithBackendOnTeamBuilt(cb)
		cfg := &setupTeamBackendConfig{}
		opt(cfg)
		assert.NotNil(t, cfg.onTeamBuilt)
		cfg.onTeamBuilt(context.Background())
		assert.True(t, called)
	})

	t.Run("WithBackendDB", func(t *testing.T) {
		// nil db 也能设置
		opt := WithBackendDB(nil)
		cfg := &setupTeamBackendConfig{}
		opt(cfg)
		assert.Nil(t, cfg.db)
	})

	t.Run("WithBackendModelAllocator", func(t *testing.T) {
		// nil allocator 也能设置
		opt := WithBackendModelAllocator(nil)
		cfg := &setupTeamBackendConfig{}
		opt(cfg)
		assert.Nil(t, cfg.modelConfigAllocator)
	})

	t.Run("WithBackendLeaderAllocation", func(t *testing.T) {
		allocation := &models.Allocation{GroupIndex: 3}
		opt := WithBackendLeaderAllocation(allocation)
		cfg := &setupTeamBackendConfig{}
		opt(cfg)
		assert.NotNil(t, cfg.leaderAllocation)
		assert.Equal(t, 3, cfg.leaderAllocation.GroupIndex)
	})
}

// TestAgentConfigurator_GetterSetter 测试所有 getter/setter
func TestAgentConfigurator_GetterSetter(t *testing.T) {
	card := agentschema.NewAgentCard()
	c := NewAgentConfigurator(card)

	t.Run("Infra/Resources初始值", func(t *testing.T) {
		assert.NotNil(t, c.Infra())
		assert.NotNil(t, c.Resources())
	})

	t.Run("Blueprint初始为nil", func(t *testing.T) {
		assert.Nil(t, c.Blueprint())
	})

	t.Run("Messager设置后获取", func(t *testing.T) {
		// Messager 为 nil 时也能正常设置
		c.SetMessager(nil)
		assert.Nil(t, c.Messager())
	})

	t.Run("TeamBackend设置后获取", func(t *testing.T) {
		// 先设置必要的组件
		c.SetTeamBackend(nil)
		assert.Nil(t, c.TeamBackend())
	})

	t.Run("WorkspaceManager设置后获取", func(t *testing.T) {
		c.SetWorkspaceManager("ws_manager")
		assert.Equal(t, "ws_manager", c.WorkspaceManager())
	})

	t.Run("WorkspaceInitialized设置后获取", func(t *testing.T) {
		c.SetWorkspaceInitialized(true)
		assert.True(t, c.WorkspaceInitialized())
		c.SetWorkspaceInitialized(false)
		assert.False(t, c.WorkspaceInitialized())
	})

	t.Run("TaskManager设置后获取", func(t *testing.T) {
		c.SetTaskManager(nil)
		assert.Nil(t, c.TaskManager())
	})

	t.Run("MessageManager设置后获取", func(t *testing.T) {
		c.SetMessageManager(nil)
		assert.Nil(t, c.MessageManager())
	})

	t.Run("Harness设置后获取", func(t *testing.T) {
		harness := agentteams.BuildTeamHarness(nil, "leader", "l1", nil, nil, nil, nil, nil, nil, false)
		c.SetHarness(harness)
		assert.Equal(t, harness, c.Harness())
	})

	t.Run("WorktreeManager设置后获取", func(t *testing.T) {
		c.SetWorktreeManager("wt_manager")
		assert.Equal(t, "wt_manager", c.WorktreeManager())
	})

	t.Run("FirstIterGate设置后获取", func(t *testing.T) {
		c.SetFirstIterGate("gate")
		assert.Equal(t, "gate", c.FirstIterGate())
	})

	t.Run("ModelAllocator设置后获取", func(t *testing.T) {
		c.SetModelAllocator(nil)
		assert.Nil(t, c.ModelAllocator())
	})

	t.Run("MemoryManager设置后获取", func(t *testing.T) {
		c.SetMemoryManager(nil)
		assert.Nil(t, c.MemoryManager())
	})

	t.Run("Spec/RuntimeContext配置前为nil", func(t *testing.T) {
		assert.Nil(t, c.Spec())
		assert.Nil(t, c.RuntimeContext())
	})

	t.Run("RolePolicy配置前为空", func(t *testing.T) {
		assert.Equal(t, "", c.RolePolicy())
	})

	t.Run("TeamSpec配置前为nil", func(t *testing.T) {
		assert.Nil(t, c.TeamSpec())
	})

	t.Run("Role配置前默认Leader", func(t *testing.T) {
		assert.Equal(t, atschema.TeamRoleLeader, c.Role())
	})

	t.Run("Lifecycle配置前默认temporary", func(t *testing.T) {
		assert.Equal(t, "temporary", c.Lifecycle())
	})

	t.Run("MemberName配置前为空", func(t *testing.T) {
		assert.Equal(t, "", c.MemberName())
	})

	t.Run("TeamName配置前为空", func(t *testing.T) {
		assert.Equal(t, "", c.TeamName())
	})
}

// TestAgentConfigurator_Configure 测试 Configure 主入口
func TestAgentConfigurator_Configure(t *testing.T) {
	card := agentschema.NewAgentCard()
	c := NewAgentConfigurator(card)

	spec := atschema.NewTeamAgentSpec()
	ctx := atschema.TeamRuntimeContext{
		Role:       atschema.TeamRoleLeader,
		MemberName: "leader_1",
		TeamSpec:   &atschema.TeamSpec{TeamName: "test_team"},
	}

	harness := c.Configure(spec, ctx)
	assert.NotNil(t, harness)
	assert.NotNil(t, c.Blueprint())
	assert.Equal(t, atschema.TeamRoleLeader, c.Role())
	assert.Equal(t, "leader_1", c.MemberName())
	assert.Equal(t, "test_team", c.TeamName())
	assert.NotNil(t, c.Spec())
	assert.NotNil(t, c.RuntimeContext())
	assert.Equal(t, "", c.RolePolicy())
	assert.NotNil(t, c.TeamSpec())
	assert.Equal(t, "temporary", c.Lifecycle())
}

// TestAgentConfigurator_SetupInfra 测试 SetupInfra
func TestAgentConfigurator_SetupInfra(t *testing.T) {
	card := agentschema.NewAgentCard()
	c := NewAgentConfigurator(card)

	spec := atschema.NewTeamAgentSpec()
	ctx := atschema.TeamRuntimeContext{
		Role:       atschema.TeamRoleTeammate,
		MemberName: "t1",
		TeamSpec:   &atschema.TeamSpec{TeamName: "team_a"},
	}

	// 不 panic 即可
	c.SetupInfra(spec, ctx)
	assert.NotNil(t, c.Blueprint())
	assert.Equal(t, atschema.TeamRoleTeammate, c.Role())
	assert.Equal(t, "t1", c.MemberName())
}

// TestAgentConfigurator_SetupInfra_WithOptions 测试 SetupInfra 带 Option
func TestAgentConfigurator_SetupInfra_WithOptions(t *testing.T) {
	card := agentschema.NewAgentCard()
	c := NewAgentConfigurator(card)

	spec := atschema.NewTeamAgentSpec()
	ctx := atschema.TeamRuntimeContext{
		Role:       atschema.TeamRoleLeader,
		MemberName: "l1",
	}

	createdCalled := false
	// 验证所有 Option 回调可以被正确赋值
	c.SetupInfra(spec, ctx,
		WithOnTeammateCreated(func(name string) { createdCalled = true }),
		WithOnTeamCleaned(func(name string) {}),
		WithOnTeamBuilt(func(name string) {}),
	)

	assert.NotNil(t, c.Blueprint())
	// 手动触发回调验证
	if c.onTeammateCreated != nil {
		c.onTeammateCreated("test")
		assert.True(t, createdCalled)
	}
}

// TestAgentConfigurator_SetupAgent 测试 SetupAgent
func TestAgentConfigurator_SetupAgent(t *testing.T) {
	card := agentschema.NewAgentCard()
	c := NewAgentConfigurator(card)

	spec := atschema.NewTeamAgentSpec()
	ctx := atschema.TeamRuntimeContext{
		Role:       atschema.TeamRoleLeader,
		MemberName: "l1",
		TeamSpec:   &atschema.TeamSpec{TeamName: "test_team"},
	}

	// 先 SetupInfra
	c.SetupInfra(spec, ctx)

	harness := c.SetupAgent(spec, ctx)
	assert.NotNil(t, harness)
	assert.Equal(t, harness, c.Harness())
}

// TestAgentConfigurator_CreateWorkspaceManager 测试返回 nil
func TestAgentConfigurator_CreateWorkspaceManager(t *testing.T) {
	card := agentschema.NewAgentCard()
	c := NewAgentConfigurator(card)
	result := c.CreateWorkspaceManager(atschema.NewTeamAgentSpec(), atschema.TeamRuntimeContext{})
	assert.Nil(t, result)
}

// TestAgentConfigurator_CreateWorktreeManager 测试返回 nil
func TestAgentConfigurator_CreateWorktreeManager(t *testing.T) {
	card := agentschema.NewAgentCard()
	c := NewAgentConfigurator(card)
	result := c.CreateWorktreeManager(atschema.NewTeamAgentSpec())
	assert.Nil(t, result)
}

// TestAgentConfigurator_UpdateModelPool 测试模型池更新
func TestAgentConfigurator_UpdateModelPool(t *testing.T) {
	t.Run("resources为nil时不panic", func(t *testing.T) {
		card := agentschema.NewAgentCard()
		c := NewAgentConfigurator(card)
		c.resources = nil
		c.UpdateModelPool(nil)
	})

	t.Run("空pool不panic", func(t *testing.T) {
		card := agentschema.NewAgentCard()
		c := NewAgentConfigurator(card)
		c.UpdateModelPool(nil)
	})
}

// TestAgentConfigurator_AttachModelAllocator 测试附加模型分配器
func TestAgentConfigurator_AttachModelAllocator(t *testing.T) {
	card := agentschema.NewAgentCard()
	c := NewAgentConfigurator(card)
	// 不 panic 即可
	c.AttachModelAllocator(nil, nil)
	assert.Nil(t, c.ModelAllocator())
}

// TestAgentConfigurator_RestoreAllocatorState 测试恢复分配器状态
func TestAgentConfigurator_RestoreAllocatorState(t *testing.T) {
	t.Run("resources为nil时不panic", func(t *testing.T) {
		card := agentschema.NewAgentCard()
		c := NewAgentConfigurator(card)
		c.resources = nil
		c.RestoreAllocatorState(map[string]any{"key": "value"})
	})

	t.Run("ModelAllocator为nil时不panic", func(t *testing.T) {
		card := agentschema.NewAgentCard()
		c := NewAgentConfigurator(card)
		c.RestoreAllocatorState(map[string]any{"key": "value"})
	})

	t.Run("有ModelAllocator时调用LoadStateDict", func(t *testing.T) {
		card := agentschema.NewAgentCard()
		c := NewAgentConfigurator(card)
		mockAlloc := &mockModelAllocator{state: map[string]any{"existing": "data"}}
		c.SetModelAllocator(mockAlloc)
		state := map[string]any{"key": "value"}
		c.RestoreAllocatorState(state)
		assert.Equal(t, state, mockAlloc.loadedState)
	})
}

// mockModelAllocator 测试用 mock 模型分配器
type mockModelAllocator struct {
	state       map[string]any
	loadedState map[string]any
}

func (m *mockModelAllocator) Allocate(modelName string) *models.Allocation {
	return nil
}

func (m *mockModelAllocator) StateDict() map[string]any {
	return m.state
}

func (m *mockModelAllocator) LoadStateDict(state map[string]any) {
	m.loadedState = state
}

// TestAgentConfigurator_BuildSpawnPayload_配置前 测试配置前返回 nil
func TestAgentConfigurator_BuildSpawnPayload_配置前(t *testing.T) {
	card := agentschema.NewAgentCard()
	c := NewAgentConfigurator(card)
	result := c.BuildSpawnPayload(atschema.TeamRuntimeContext{}, "")
	assert.Nil(t, result)
}

// TestAgentConfigurator_BuildMemberContext_配置前 测试配置前返回零值
func TestAgentConfigurator_BuildMemberContext_配置前(t *testing.T) {
	card := agentschema.NewAgentCard()
	c := NewAgentConfigurator(card)
	result := c.BuildMemberContext(atschema.TeamMemberSpec{})
	assert.Equal(t, atschema.TeamRuntimeContext{}, result)
}

// TestAgentConfigurator_BuildMemberMessagerConfig_配置前 测试配置前返回 nil
func TestAgentConfigurator_BuildMemberMessagerConfig_配置前(t *testing.T) {
	card := agentschema.NewAgentCard()
	c := NewAgentConfigurator(card)
	result := c.BuildMemberMessagerConfig("test")
	assert.Nil(t, result)
}

// TestAgentConfigurator_BuildMemberMessagerConfig_配置后 测试配置后通过 SpawnPayloadBuilder 代理
func TestAgentConfigurator_BuildMemberMessagerConfig_配置后(t *testing.T) {
	card := agentschema.NewAgentCard()
	c := NewAgentConfigurator(card)
	spec := atschema.NewTeamAgentSpec()
	ctx := atschema.TeamRuntimeContext{
		Role:       atschema.TeamRoleLeader,
		MemberName: "l1",
	}
	c.Configure(spec, ctx)
	// BuildMemberMessagerConfig 代理到 SpawnPayloadBuilder
	// 返回值取决于具体实现，不 panic 即可
	_ = c.BuildMemberMessagerConfig("l1")
}

// TestAgentConfigurator_BuildSpawnConfig_配置前 测试配置前返回零值
func TestAgentConfigurator_BuildSpawnConfig_配置前(t *testing.T) {
	card := agentschema.NewAgentCard()
	c := NewAgentConfigurator(card)
	result := c.BuildSpawnConfig(atschema.TeamRuntimeContext{})
	assert.Equal(t, runnerspawn.SpawnAgentConfig{}, result)
}

// mockTeamDatabase 测试用 mock 数据库（仅用于类型验证，实际未使用）
