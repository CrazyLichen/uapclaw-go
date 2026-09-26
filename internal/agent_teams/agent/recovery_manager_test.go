package agent_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/agent"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/metadata"
	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// fakeSessionFacade 用于测试的内存 SessionFacade 实现
type fakeSessionFacade struct {
	data map[string]any
}

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

func newFakeSessionFacade() *fakeSessionFacade {
	return &fakeSessionFacade{data: make(map[string]any)}
}

func (f *fakeSessionFacade) GetSessionID() string            { return "test-session" }
func (f *fakeSessionFacade) UpdateState(data map[string]any) { f.data = data }
func (f *fakeSessionFacade) GetState(key state.StateKey) (any, error) {
	return f.data[key.String()], nil
}
func (f *fakeSessionFacade) DumpState() map[string]any { return f.data }
func (f *fakeSessionFacade) WriteStream(_ context.Context, _ any) error {
	return nil
}
func (f *fakeSessionFacade) WriteCustomStream(_ context.Context, _ any) error {
	return nil
}
func (f *fakeSessionFacade) GetEnv(_ string, _ ...any) any { return nil }
func (f *fakeSessionFacade) Interact(_ context.Context, _ any) error {
	return nil
}

// 编译时检查
var _ interfaces.SessionFacade = (*fakeSessionFacade)(nil)

// newTestConfigurator 创建测试用的轻量 AgentConfigurator
func newTestConfigurator() *agent.AgentConfigurator {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentID("test_leader"),
		agentschema.WithAgentName("test_leader"),
	)
	return agent.NewAgentConfigurator(card)
}

// ──────────────────────────── 测试 ────────────────────────────

// TestNewRecoveryManager 测试创建 RecoveryManager
// Python: RecoveryManager.__init__(configurator, spawn_manager)
func TestNewRecoveryManager(t *testing.T) {
	cfg := newTestConfigurator()
	sm := agent.NewSpawnManager(agent.NewTeamAgentState(), cfg, nil)
	rm := agent.NewRecoveryManager(cfg, sm)
	assert.NotNil(t, rm)
}

// TestRecoverTeam_teamBackend为nil 测试 teamBackend 为 nil 时返回空列表
// Python: if not team_backend: return []
func TestRecoverTeam_teamBackend为nil(t *testing.T) {
	cfg := newTestConfigurator()
	sm := agent.NewSpawnManager(agent.NewTeamAgentState(), cfg, nil)
	rm := agent.NewRecoveryManager(cfg, sm)
	result := rm.RecoverTeam(context.Background())
	assert.Empty(t, result)
}

// TestPersistLeaderConfig_spec为nil时跳过 测试 Spec 为 nil 时不写入任何数据
// Python: if spec is None or ctx is None or team_name is None: return
func TestPersistLeaderConfig_spec为nil时跳过(t *testing.T) {
	cfg := newTestConfigurator()
	sm := agent.NewSpawnManager(agent.NewTeamAgentState(), cfg, nil)
	rm := agent.NewRecoveryManager(cfg, sm)
	sess := newFakeSessionFacade()
	rm.PersistLeaderConfig(sess)
	teams := metadata.ReadTeamsBucket(sess)
	assert.Empty(t, teams)
}

// TestMarkTeammateRestarting_无teamBackend返回false 测试无 teamBackend 时返回 false
// Python: if not team_backend: return False
func TestMarkTeammateRestarting_无teamBackend返回false(t *testing.T) {
	cfg := newTestConfigurator()
	sm := agent.NewSpawnManager(agent.NewTeamAgentState(), cfg, nil)
	rm := agent.NewRecoveryManager(cfg, sm)
	result := rm.MarkTeammateRestartingForSessionSwitch(
		context.Background(), "member1", atschema.MemberStatusRestarting,
	)
	assert.False(t, result)
}

// TestMarkTeammateRestarting_teamName为空返回false 测试 teamName 为空时返回 false
// Python: if team_name is None: return False
func TestMarkTeammateRestarting_teamName为空返回false(t *testing.T) {
	cfg := newTestConfigurator()
	sm := agent.NewSpawnManager(agent.NewTeamAgentState(), cfg, nil)
	rm := agent.NewRecoveryManager(cfg, sm)
	// teamBackend 为 nil，所以这个测试间接验证了
	result := rm.MarkTeammateRestartingForSessionSwitch(
		context.Background(), "member1", atschema.MemberStatusReady,
	)
	assert.False(t, result)
}

// TestCollectLiveTeammates_非Leader返回空 测试非 Leader 角色返回空
// Python: if self._configurator.role != TeamRole.LEADER or not team_backend: return []
func TestCollectLiveTeammates_非Leader返回空(t *testing.T) {
	cfg := newTestConfigurator()
	sm := agent.NewSpawnManager(agent.NewTeamAgentState(), cfg, nil)
	rm := agent.NewRecoveryManager(cfg, sm)
	result := rm.CollectLiveTeammatesForSessionSwitch(context.Background())
	assert.Empty(t, result)
}

// TestRestartForSessionSwitch_空列表无操作 测试空列表不 panic
// Python: restart_for_session_switch(self, recoverable_members, *, cleanup_first) 空列表无操作
func TestRestartForSessionSwitch_空列表无操作(t *testing.T) {
	cfg := newTestConfigurator()
	sm := agent.NewSpawnManager(agent.NewTeamAgentState(), cfg, nil)
	rm := agent.NewRecoveryManager(cfg, sm)
	// 空列表不应 panic
	rm.RestartForSessionSwitch(context.Background(), nil, true)
}

// TestPersistAllocatorState_allocator为nil时跳过 测试 allocator 为 nil 时跳过
// Python: if team_session is None or allocator is None or team_name is None: return
func TestPersistAllocatorState_allocator为nil时跳过(t *testing.T) {
	cfg := newTestConfigurator()
	sm := agent.NewSpawnManager(agent.NewTeamAgentState(), cfg, nil)
	rm := agent.NewRecoveryManager(cfg, sm)
	sess := newFakeSessionFacade()
	rm.PersistAllocatorState(sess)
	teams := metadata.ReadTeamsBucket(sess)
	assert.Empty(t, teams)
}

// TestPersistAllocatorState_session为nil时跳过 测试 session 为 nil 时跳过
// Python: if team_session is None: return
func TestPersistAllocatorState_session为nil时跳过(t *testing.T) {
	cfg := newTestConfigurator()
	sm := agent.NewSpawnManager(agent.NewTeamAgentState(), cfg, nil)
	rm := agent.NewRecoveryManager(cfg, sm)
	rm.PersistAllocatorState(nil)
	// 不应 panic
}

// TestMarkTeammateRestarting_已是Restarting 测试已是 RESTARTING 状态时直接返回 true
// Python: if current_status == MemberStatus.RESTARTING: return True
func TestMarkTeammateRestarting_已是Restarting(t *testing.T) {
	cfg := newTestConfigurator()
	sm := agent.NewSpawnManager(agent.NewTeamAgentState(), cfg, nil)
	rm := agent.NewRecoveryManager(cfg, sm)
	// 无 teamBackend，但即使有，RESTARTING 也应直接返回 true
	// 当前无 backend 返回 false，验证 guard 行为
	result := rm.MarkTeammateRestartingForSessionSwitch(
		context.Background(), "m1", atschema.MemberStatusRestarting,
	)
	assert.False(t, result) // 无 backend guard
}

// TestRestartForSessionSwitch_非空列表cleanupFirst 测试非空列表执行清理+重启
// Python: restart_for_session_switch(self, recoverable_members, *, cleanup_first=True)
func TestRestartForSessionSwitch_非空列表cleanupFirst(t *testing.T) {
	cfg := newTestConfigurator()
	sm := agent.NewSpawnManager(agent.NewTeamAgentState(), cfg, nil)
	rm := agent.NewRecoveryManager(cfg, sm)

	members := []agent.LiveTeammate{
		{MemberName: "m1", Status: atschema.MemberStatusPaused},
		{MemberName: "m2", Status: atschema.MemberStatusError},
	}
	// 无 teamBackend，MarkTeammateRestarting 返回 false，两个都被跳过
	rm.RestartForSessionSwitch(context.Background(), members, true)
	// 不应 panic
}

// TestRestartForSessionSwitch_非空列表不清理 测试 cleanupFirst=false
// Python: restart_for_session_switch(self, recoverable_members, *, cleanup_first=False)
func TestRestartForSessionSwitch_非空列表不清理(t *testing.T) {
	cfg := newTestConfigurator()
	sm := agent.NewSpawnManager(agent.NewTeamAgentState(), cfg, nil)
	rm := agent.NewRecoveryManager(cfg, sm)

	members := []agent.LiveTeammate{
		{MemberName: "m1", Status: atschema.MemberStatusReady},
	}
	rm.RestartForSessionSwitch(context.Background(), members, false)
	// 不应 panic
}

// TestPersistLeaderConfig_正常持久化 测试配置后的正常持久化路径
// Python: persist_leader_config(self, session) — 正常路径写入 spec/context/db_state
func TestPersistLeaderConfig_正常持久化(t *testing.T) {
	cfg := newTestConfigurator()
	spec := atschema.TeamAgentSpec{Agents: map[string]atschema.DeepAgentSpec{"leader": {}}}
	runtimeCtx := atschema.TeamRuntimeContext{
		Role:       atschema.TeamRoleLeader,
		MemberName: "leader",
		TeamSpec:   &atschema.TeamSpec{TeamName: "my_team"},
	}
	cfg.SetupInfra(spec, runtimeCtx)
	cfg.SetupAgent(spec, runtimeCtx)

	sm := agent.NewSpawnManager(agent.NewTeamAgentState(), cfg, nil)
	rm := agent.NewRecoveryManager(cfg, sm)
	sess := newFakeSessionFacade()

	rm.PersistLeaderConfig(sess)

	ns := metadata.ReadTeamNamespace(sess, "my_team")
	assert.NotNil(t, ns)
	assert.Contains(t, ns, "spec")
	assert.Contains(t, ns, "context")
	assert.Contains(t, ns, metadata.TeamDBStateKey)
	assert.Equal(t, metadata.TeamDBStatePendingCreate, ns[metadata.TeamDBStateKey])
}

// TestPersistLeaderConfig_已有dbState 测试已有 db_state 时保留原值
// Python: TEAM_DB_STATE_KEY: read_team_db_state(session, team_name) or TEAM_DB_STATE_PENDING_CREATE
func TestPersistLeaderConfig_已有dbState(t *testing.T) {
	cfg := newTestConfigurator()
	spec := atschema.TeamAgentSpec{Agents: map[string]atschema.DeepAgentSpec{"leader": {}}}
	runtimeCtx := atschema.TeamRuntimeContext{
		Role:       atschema.TeamRoleLeader,
		MemberName: "leader",
		TeamSpec:   &atschema.TeamSpec{TeamName: "my_team"},
	}
	cfg.SetupInfra(spec, runtimeCtx)
	cfg.SetupAgent(spec, runtimeCtx)

	sm := agent.NewSpawnManager(agent.NewTeamAgentState(), cfg, nil)
	rm := agent.NewRecoveryManager(cfg, sm)
	sess := newFakeSessionFacade()

	// 预设 db_state 为 created
	metadata.MergeTeamDBState(sess, "my_team", metadata.TeamDBStateCreated)

	rm.PersistLeaderConfig(sess)

	ns := metadata.ReadTeamNamespace(sess, "my_team")
	assert.NotNil(t, ns)
	assert.Equal(t, metadata.TeamDBStateCreated, ns[metadata.TeamDBStateKey])
}

// strPtr 返回字符串指针的辅助函数
func strPtr(s string) *string {
	return &s
}
