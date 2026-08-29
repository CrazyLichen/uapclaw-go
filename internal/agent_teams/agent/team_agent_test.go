package agent

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	runnerspawn "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/spawn"
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

	// 配置前：返回零值 SpawnAgentConfig
	cfg := a.BuildSpawnConfig(atschema.TeamRuntimeContext{})
	assert.Equal(t, runnerspawn.SpawnAgentKind(""), cfg.AgentKind, "配置前 AgentKind 应为空")

	// 配置后
	spec := atschema.NewTeamAgentSpec()
	ctx := atschema.TeamRuntimeContext{Role: atschema.TeamRoleLeader, MemberName: "leader_1"}
	a.Configure(context.Background(), spec, ctx)

	cfg = a.BuildSpawnConfig(ctx)
	assert.Equal(t, runnerspawn.SpawnAgentKindTeamAgent, cfg.AgentKind, "配置后 AgentKind 应为 team_agent")
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

// TestTeamAgent_AgentCard 测试返回 AgentCard
func TestTeamAgent_AgentCard(t *testing.T) {
	card := agentschema.NewAgentCard()
	a := NewTeamAgent(card)
	assert.Equal(t, card, a.AgentCard())
}

// TestTeamAgent_State 测试返回 State
func TestTeamAgent_State(t *testing.T) {
	card := agentschema.NewAgentCard()
	a := NewTeamAgent(card)
	assert.NotNil(t, a.State())
}

// TestTeamAgent_Coordination 测试返回 nil
func TestTeamAgent_Coordination(t *testing.T) {
	card := agentschema.NewAgentCard()
	a := NewTeamAgent(card)
	assert.Nil(t, a.Coordination())
}

// TestTeamAgent_CoordinationLoop 测试返回 nil
func TestTeamAgent_CoordinationLoop(t *testing.T) {
	card := agentschema.NewAgentCard()
	a := NewTeamAgent(card)
	assert.Nil(t, a.CoordinationLoop())
}

// TestTeamAgent_SessionManager 测试返回 SessionManager
func TestTeamAgent_SessionManager(t *testing.T) {
	card := agentschema.NewAgentCard()
	a := NewTeamAgent(card)
	assert.NotNil(t, a.SessionManager())
}

// TestTeamAgent_RecoveryManager 测试返回 nil
func TestTeamAgent_RecoveryManager(t *testing.T) {
	card := agentschema.NewAgentCard()
	a := NewTeamAgent(card)
	assert.Nil(t, a.RecoveryManager())
}

// TestTeamAgent_SpawnManager 测试返回 SpawnManager
func TestTeamAgent_SpawnManager(t *testing.T) {
	card := agentschema.NewAgentCard()
	a := NewTeamAgent(card)
	assert.NotNil(t, a.SpawnManager())
}

// TestTeamAgent_StreamController 测试返回 StreamController
func TestTeamAgent_StreamController(t *testing.T) {
	card := agentschema.NewAgentCard()
	a := NewTeamAgent(card)
	assert.NotNil(t, a.StreamController())
}

// TestTeamAgent_EventListeners 测试返回事件监听器
func TestTeamAgent_EventListeners(t *testing.T) {
	card := agentschema.NewAgentCard()
	a := NewTeamAgent(card)
	assert.NotNil(t, a.EventListeners())
	assert.Empty(t, a.EventListeners())
}

// TestTeamAgent_PendingUserQuery 测试返回待处理查询
func TestTeamAgent_PendingUserQuery(t *testing.T) {
	card := agentschema.NewAgentCard()
	a := NewTeamAgent(card)
	assert.Equal(t, "", a.PendingUserQuery())
}

// TestTeamAgent_AddEventListener 测试添加事件监听器
func TestTeamAgent_AddEventListener(t *testing.T) {
	card := agentschema.NewAgentCard()
	a := NewTeamAgent(card)

	handler := "test_handler"
	a.AddEventListener(handler)
	assert.Len(t, a.EventListeners(), 1)
	assert.Equal(t, handler, a.EventListeners()[0])
}

// TestTeamAgent_RemoveEventListener 测试移除事件监听器
func TestTeamAgent_RemoveEventListener(t *testing.T) {
	card := agentschema.NewAgentCard()
	a := NewTeamAgent(card)

	handler1 := "handler1"
	handler2 := "handler2"
	a.AddEventListener(handler1)
	a.AddEventListener(handler2)
	assert.Len(t, a.EventListeners(), 2)

	a.RemoveEventListener(handler1)
	assert.Len(t, a.EventListeners(), 1)
	assert.Equal(t, handler2, a.EventListeners()[0])
}

// TestTeamAgent_IsAgentRunning 测试返回 false
func TestTeamAgent_IsAgentRunning(t *testing.T) {
	card := agentschema.NewAgentCard()
	a := NewTeamAgent(card)
	assert.False(t, a.IsAgentRunning())
}

// TestTeamAgent_HasInFlightRound 测试返回 false
func TestTeamAgent_HasInFlightRound(t *testing.T) {
	card := agentschema.NewAgentCard()
	a := NewTeamAgent(card)
	assert.False(t, a.HasInFlightRound())
}

// TestTeamAgent_HasPendingInterrupt 测试返回 false
func TestTeamAgent_HasPendingInterrupt(t *testing.T) {
	card := agentschema.NewAgentCard()
	a := NewTeamAgent(card)
	assert.False(t, a.HasPendingInterrupt())
}

// TestTeamAgent_PersistAllocatorState 测试空操作
func TestTeamAgent_PersistAllocatorState(t *testing.T) {
	card := agentschema.NewAgentCard()
	a := NewTeamAgent(card)
	a.PersistAllocatorState()
}

// TestTeamAgent_Interact 测试空操作
func TestTeamAgent_Interact(t *testing.T) {
	card := agentschema.NewAgentCard()
	a := NewTeamAgent(card)
	err := a.Interact(context.Background(), "hello")
	assert.NoError(t, err)
}

// TestTeamAgent_Broadcast 测试空操作
func TestTeamAgent_Broadcast(t *testing.T) {
	card := agentschema.NewAgentCard()
	a := NewTeamAgent(card)
	result, err := a.Broadcast(context.Background(), "announcement")
	assert.Nil(t, result)
	assert.NoError(t, err)
}

// TestTeamAgent_HumanAgentSay 测试空操作
func TestTeamAgent_HumanAgentSay(t *testing.T) {
	card := agentschema.NewAgentCard()
	a := NewTeamAgent(card)
	result, err := a.HumanAgentSay(context.Background(), "hello", "to", "sender")
	assert.Nil(t, result)
	assert.NoError(t, err)
}

// TestTeamAgent_StartCoordination 测试空操作
func TestTeamAgent_StartCoordination(t *testing.T) {
	card := agentschema.NewAgentCard()
	a := NewTeamAgent(card)
	err := a.StartCoordination(context.Background(), nil)
	assert.NoError(t, err)
}

// TestTeamAgent_PauseCoordination 测试空操作
func TestTeamAgent_PauseCoordination(t *testing.T) {
	card := agentschema.NewAgentCard()
	a := NewTeamAgent(card)
	err := a.PauseCoordination(context.Background())
	assert.NoError(t, err)
}

// TestTeamAgent_StopCoordination 测试空操作
func TestTeamAgent_StopCoordination(t *testing.T) {
	card := agentschema.NewAgentCard()
	a := NewTeamAgent(card)
	err := a.StopCoordination(context.Background())
	assert.NoError(t, err)
}

// TestTeamAgent_AutoStartMember 测试返回 false
func TestTeamAgent_AutoStartMember(t *testing.T) {
	card := agentschema.NewAgentCard()
	a := NewTeamAgent(card)
	result := a.AutoStartMember(context.Background(), "t1")
	assert.False(t, result)
}

// TestTeamAgent_AutoStartAll 测试返回 nil
func TestTeamAgent_AutoStartAll(t *testing.T) {
	card := agentschema.NewAgentCard()
	a := NewTeamAgent(card)
	result := a.AutoStartAll(context.Background())
	assert.Nil(t, result)
}

// TestTeamAgent_FromSpawnPayload 测试返回 nil
func TestTeamAgent_FromSpawnPayload(t *testing.T) {
	a, err := FromSpawnPayload(context.Background(), nil)
	assert.Nil(t, a)
	assert.NoError(t, err)
}

// TestTeamAgent_RecoverTeam 测试返回 nil
func TestTeamAgent_RecoverTeam(t *testing.T) {
	card := agentschema.NewAgentCard()
	a := NewTeamAgent(card)
	result, err := a.RecoverTeam(context.Background())
	assert.Nil(t, result)
	assert.NoError(t, err)
}

// TestTeamAgent_RecoverFromSession 测试返回 nil
func TestTeamAgent_RecoverFromSession(t *testing.T) {
	a, err := RecoverFromSession(context.Background(), nil, "test", nil)
	assert.Nil(t, a)
	assert.NoError(t, err)
}

// TestTeamAgent_PersistSessionManifest 测试空操作
func TestTeamAgent_PersistSessionManifest(t *testing.T) {
	card := agentschema.NewAgentCard()
	a := NewTeamAgent(card)
	a.PersistSessionManifest(nil)
}

// TestTeamAgent_DestroyTeam 测试销毁团队
func TestTeamAgent_DestroyTeam(t *testing.T) {
	card := agentschema.NewAgentCard()
	a := NewTeamAgent(card)
	result, err := a.DestroyTeam(context.Background(), true)
	assert.False(t, result)
	assert.NoError(t, err)
}

// TestTeamAgent_StartAgent 测试启动 Agent
func TestTeamAgent_StartAgent(t *testing.T) {
	card := agentschema.NewAgentCard()
	a := NewTeamAgent(card)
	err := a.StartAgent(context.Background(), "hello")
	assert.NoError(t, err)
}

// TestTeamAgent_FollowUp 测试追加输入
func TestTeamAgent_FollowUp(t *testing.T) {
	card := agentschema.NewAgentCard()
	a := NewTeamAgent(card)
	err := a.FollowUp(context.Background(), "more input")
	assert.NoError(t, err)
}

// TestTeamAgent_CancelAgent 测试取消 Agent
func TestTeamAgent_CancelAgent(t *testing.T) {
	card := agentschema.NewAgentCard()
	a := NewTeamAgent(card)
	err := a.CancelAgent(context.Background())
	assert.NoError(t, err)
}

// TestTeamAgent_Steer 测试转向输入
func TestTeamAgent_Steer(t *testing.T) {
	card := agentschema.NewAgentCard()
	a := NewTeamAgent(card)
	err := a.Steer(context.Background(), "steer input")
	assert.NoError(t, err)
}

// TestTeamAgent_DeliverInput 测试投递输入
func TestTeamAgent_DeliverInput(t *testing.T) {
	card := agentschema.NewAgentCard()
	a := NewTeamAgent(card)
	err := a.DeliverInput(context.Background(), "input", true)
	assert.NoError(t, err)
}

// TestTeamAgent_ResumeInterrupt 测试恢复中断
func TestTeamAgent_ResumeInterrupt(t *testing.T) {
	card := agentschema.NewAgentCard()
	a := NewTeamAgent(card)
	err := a.ResumeInterrupt(context.Background(), "input")
	assert.NoError(t, err)
}

// TestTeamAgent_ShutdownSelf 测试请求自身关闭
func TestTeamAgent_ShutdownSelf(t *testing.T) {
	card := agentschema.NewAgentCard()
	a := NewTeamAgent(card)
	err := a.ShutdownSelf(context.Background())
	assert.NoError(t, err)
}

// TestTeamAgent_ConcludeCompletedRound 测试完成轮次
func TestTeamAgent_ConcludeCompletedRound(t *testing.T) {
	card := agentschema.NewAgentCard()
	a := NewTeamAgent(card)
	err := a.ConcludeCompletedRound(context.Background(), 3, 2)
	assert.NoError(t, err)
}

// TestTeamAgent_SpawnTeammate 测试生成 Teammate
func TestTeamAgent_SpawnTeammate(t *testing.T) {
	card := agentschema.NewAgentCard()
	a := NewTeamAgent(card)
	err := a.SpawnTeammate(context.Background(), atschema.TeamRuntimeContext{}, "msg", "sid", nil)
	assert.NoError(t, err)
}

// TestTeamAgent_LookupHumanAgentRuntime 测试查找人类代理
func TestTeamAgent_LookupHumanAgentRuntime(t *testing.T) {
	card := agentschema.NewAgentCard()
	a := NewTeamAgent(card)
	result := a.LookupHumanAgentRuntime("nonexistent")
	assert.Nil(t, result)
}

// TestTeamAgent_IsShutdownRequested 测试关闭请求检查
func TestTeamAgent_IsShutdownRequested(t *testing.T) {
	card := agentschema.NewAgentCard()
	a := NewTeamAgent(card)
	result, err := a.IsShutdownRequested(context.Background())
	assert.False(t, result)
	assert.NoError(t, err)
}

// TestTeamAgent_UpdateStatus 测试更新状态
func TestTeamAgent_UpdateStatus(t *testing.T) {
	card := agentschema.NewAgentCard()
	a := NewTeamAgent(card)
	err := a.UpdateStatus(context.Background(), atschema.MemberStatusReady)
	assert.NoError(t, err)
}

// TestTeamAgent_SessionID 测试返回空会话 ID
func TestTeamAgent_SessionID(t *testing.T) {
	card := agentschema.NewAgentCard()
	a := NewTeamAgent(card)
	sid := a.SessionID(context.Background())
	assert.Equal(t, "", sid)
}

// TestTeamAgent_ResumeForNewSession 测试恢复新会话
func TestTeamAgent_ResumeForNewSession(t *testing.T) {
	card := agentschema.NewAgentCard()
	a := NewTeamAgent(card)
	ctx, err := a.ResumeForNewSession(context.Background(), nil)
	assert.NoError(t, err)
	assert.NotNil(t, ctx)
}

// TestTeamAgent_RecoverForExistingSession 测试恢复已有会话
func TestTeamAgent_RecoverForExistingSession(t *testing.T) {
	card := agentschema.NewAgentCard()
	a := NewTeamAgent(card)
	ctx, err := a.RecoverForExistingSession(context.Background(), nil)
	assert.NoError(t, err)
	assert.NotNil(t, ctx)
}

// TestTeamAgent_Invoke 测试非流式调用
func TestTeamAgent_Invoke(t *testing.T) {
	card := agentschema.NewAgentCard()
	a := NewTeamAgent(card)
	result, err := a.Invoke(context.Background(), map[string]any{"input": "hello"})
	assert.Nil(t, result)
	assert.NoError(t, err)
}

// TestTeamAgent_Stream 测试流式调用
func TestTeamAgent_Stream(t *testing.T) {
	card := agentschema.NewAgentCard()
	a := NewTeamAgent(card)
	result, err := a.Stream(context.Background(), map[string]any{"input": "hello"})
	assert.Nil(t, result)
	assert.NoError(t, err)
}

// TestTeamAgent_属性代理_configurator为nil 测试 configurator 为 nil 时返回零值
func TestTeamAgent_属性代理_configurator为nil(t *testing.T) {
	card := agentschema.NewAgentCard()
	a := NewTeamAgent(card)
	a.configurator = nil

	assert.Nil(t, a.Blueprint())
	assert.Nil(t, a.Infra())
	assert.Nil(t, a.Resources())
	assert.Nil(t, a.Harness())
	assert.Nil(t, a.Spec())
	assert.Nil(t, a.RuntimeContext())
	assert.Equal(t, atschema.TeamRoleLeader, a.Role())
	assert.Equal(t, "temporary", a.Lifecycle())
	assert.Nil(t, a.TeamSpec())
	assert.Equal(t, "", a.MemberName())
	assert.Nil(t, a.MessageManager())
	assert.Nil(t, a.TaskManager())
	assert.Nil(t, a.TeamBackend())
	assert.Equal(t, "", a.TeamName())
}

// TestTeamAgent_属性代理_配置后 测试配置后属性代理返回正确值
func TestTeamAgent_属性代理_配置后(t *testing.T) {
	card := agentschema.NewAgentCard()
	a := NewTeamAgent(card)

	spec := atschema.NewTeamAgentSpec()
	ctx := atschema.TeamRuntimeContext{
		Role:       atschema.TeamRoleTeammate,
		MemberName: "t1",
		TeamSpec:   &atschema.TeamSpec{TeamName: "my_team"},
	}
	a.Configure(context.Background(), spec, ctx)

	assert.NotNil(t, a.Blueprint())
	assert.NotNil(t, a.Infra())
	assert.NotNil(t, a.Resources())
	assert.NotNil(t, a.Harness())
	assert.NotNil(t, a.Spec())
	assert.NotNil(t, a.RuntimeContext())
	assert.Equal(t, atschema.TeamRoleTeammate, a.Role())
	assert.Equal(t, "temporary", a.Lifecycle())
	assert.NotNil(t, a.TeamSpec())
	assert.Equal(t, "t1", a.MemberName())
	assert.Equal(t, "my_team", a.TeamName())
	assert.True(t, a.IsAgentReady())
}

// TestTeamAgent_SetTeamBackend 测试 SetTeamBackend 方法
func TestTeamAgent_SetTeamBackend(t *testing.T) {
	card := agentschema.NewAgentCard()
	a := NewTeamAgent(card)
	// configurator 为 nil 时不 panic
	a.SetTeamBackend(nil)
}

// TestTaskFailedError_Error 测试 taskFailedError.Error 方法
func TestTaskFailedError_Error(t *testing.T) {
	e := &taskFailedError{code: 1, text: "task failed"}
	assert.Equal(t, "[1] task failed", e.Error())

	e2 := &taskFailedError{code: 0, text: "simple error"}
	assert.Equal(t, "simple error", e2.Error())
}
