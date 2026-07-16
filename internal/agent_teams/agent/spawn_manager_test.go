package agent

import (
	"context"
	"testing"
	"time"

	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/spawn"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/stretchr/testify/assert"
)

// ──────────────────────────── 结构体 ────────────────────────────

// stubSpawnHandle 测试用 SpawnHandle 桩（模拟 subprocess 句柄）
type stubSpawnHandle struct {
	processID string
	alive     bool
	healthy   bool
}

func (h *stubSpawnHandle) ProcessID() string                                         { return h.processID }
func (h *stubSpawnHandle) IsAlive() bool                                             { return h.alive }
func (h *stubSpawnHandle) IsHealthy() bool                                           { return h.healthy }
func (h *stubSpawnHandle) Shutdown(_ context.Context, _ ...time.Duration) (bool, error) { return true, nil }
func (h *stubSpawnHandle) ForceKill() error                                          { return nil }
func (h *stubSpawnHandle) WaitForCompletion() (int, error)                           { return 0, nil }
func (h *stubSpawnHandle) StartHealthCheck(_ context.Context, _ ...time.Duration) error { return nil }
func (h *stubSpawnHandle) StopHealthCheck() error                                    { return nil }

// ──────────────────────────── 导出函数 ────────────────────────────

// TestNewSpawnManager_基本创建 测试 SpawnManager 基本创建。
func TestNewSpawnManager_基本创建(t *testing.T) {
	state := NewTeamAgentState()
	card := agentschema.NewAgentCard()
	configurator := NewAgentConfigurator(card)

	sm := NewSpawnManager(state, configurator, nil)
	assert.NotNil(t, sm)
	assert.NotNil(t, sm.spawnedHandles)
	assert.NotNil(t, sm.recoveryCancel)
}

// TestNewSpawnManager_带TeamAgentGetter 测试带 TeamAgent getter 的创建。
func TestNewSpawnManager_带TeamAgentGetter(t *testing.T) {
	state := NewTeamAgentState()
	card := agentschema.NewAgentCard()
	configurator := NewAgentConfigurator(card)
	ta := NewTeamAgent(card)

	sm := NewSpawnManager(state, configurator, func() *TeamAgent { return ta })
	assert.NotNil(t, sm)
	assert.Equal(t, ta, sm.getTeamAgent())
}

// ──────────────────────────── 导出方法 ────────────────────────────

// TestSpawnManager_LookupInprocessAgent_空map 测试空 map 时查找返回 nil。
func TestSpawnManager_LookupInprocessAgent_空map(t *testing.T) {
	sm := newTestSpawnManager(t)

	result := sm.LookupInprocessAgent("alice")
	assert.Nil(t, result, "空 map 时应返回 nil")
}

// TestSpawnManager_LookupInprocessAgent_不存在的成员 测试查找不存在的成员返回 nil。
func TestSpawnManager_LookupInprocessAgent_不存在的成员(t *testing.T) {
	sm := newTestSpawnManager(t)

	// 手动添加一个 subprocess 句柄
	sm.spawnedHandles["bob"] = &stubSpawnHandle{processID: "sub-bob", alive: true, healthy: true}

	result := sm.LookupInprocessAgent("alice")
	assert.Nil(t, result, "成员不存在时应返回 nil")
}

// TestSpawnManager_LookupInprocessAgent_subprocess句柄返回nil 测试 subprocess 句柄查找返回 nil。
func TestSpawnManager_LookupInprocessAgent_subprocess句柄返回nil(t *testing.T) {
	sm := newTestSpawnManager(t)

	// 手动添加一个 subprocess 句柄（不是 InProcessSpawnHandle）
	sm.spawnedHandles["bob"] = &stubSpawnHandle{processID: "sub-bob", alive: true, healthy: true}

	result := sm.LookupInprocessAgent("bob")
	assert.Nil(t, result, "subprocess 句柄不是 InProcessSpawnHandle，应返回 nil")
}

// TestSpawnManager_LookupInprocessAgent_inprocess句柄返回agent 测试 inprocess 句柄查找返回 agent 引用。
func TestSpawnManager_LookupInprocessAgent_inprocess句柄返回agent(t *testing.T) {
	sm := newTestSpawnManager(t)

	// 手动添加一个 inprocess 句柄
	agentCard := agentschema.NewAgentCard(agentschema.WithAgentID("test-alice"))
	inprocHandle := spawn.NewInProcessSpawnHandle(
		"inproc-alice",
		func() {},
		make(chan struct{}),
		NewTeamAgent(agentCard),
	)
	sm.spawnedHandles["alice"] = inprocHandle

	result := sm.LookupInprocessAgent("alice")
	assert.NotNil(t, result, "inprocess 句柄应返回 agent 引用")
}

// TestSpawnManager_CleanupTeammate_不存在的成员 测试清理不存在的成员不 panic。
func TestSpawnManager_CleanupTeammate_不存在的成员(t *testing.T) {
	sm := newTestSpawnManager(t)

	// 不应 panic
	sm.CleanupTeammate(context.Background(), "nonexistent")
}

// TestSpawnManager_CleanupTeammate_inprocess句柄 测试清理 inprocess 句柄。
func TestSpawnManager_CleanupTeammate_inprocess句柄(t *testing.T) {
	sm := newTestSpawnManager(t)

	// 添加一个 inprocess 句柄
	done := make(chan struct{})
	inprocHandle := spawn.NewInProcessSpawnHandle(
		"inproc-alice",
		func() {},
		done,
		NewTeamAgent(agentschema.NewAgentCard()),
	)
	sm.spawnedHandles["alice"] = inprocHandle

	sm.CleanupTeammate(context.Background(), "alice")

	// 验证已从 map 中移除
	_, exists := sm.spawnedHandles["alice"]
	assert.False(t, exists, "清理后应从 map 中移除")

	// 验证 chunkForward 已断开
	assert.Nil(t, inprocHandle.ChunkForward(), "chunkForward 应被断开")
}

// TestSpawnManager_CancelRecoveryTasks 测试取消恢复任务。
func TestSpawnManager_CancelRecoveryTasks(t *testing.T) {
	sm := newTestSpawnManager(t)

	// 不应 panic
	sm.CancelRecoveryTasks()
}

// TestSpawnManager_CancelRecoveryTasks_有任务 测试取消已注册的恢复任务。
func TestSpawnManager_CancelRecoveryTasks_有任务(t *testing.T) {
	sm := newTestSpawnManager(t)

	// 模拟注册一个恢复任务
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sm.recoveryCancel["alice"] = cancel

	sm.CancelRecoveryTasks()

	// 验证已清空
	assert.Empty(t, sm.recoveryCancel, "取消后应清空 recoveryCancel")
	// 验证 context 已取消
	assert.Error(t, ctx.Err())
}

// TestSpawnManager_ShutdownAllHandles_空 测试关闭空句柄集合。
func TestSpawnManager_ShutdownAllHandles_空(t *testing.T) {
	sm := newTestSpawnManager(t)

	// 不应 panic
	sm.ShutdownAllHandles(context.Background())
}

// TestSpawnManager_ShutdownAllHandles_有句柄 测试关闭有句柄的集合。
func TestSpawnManager_ShutdownAllHandles_有句柄(t *testing.T) {
	sm := newTestSpawnManager(t)

	// 添加两个 inprocess 句柄
	done1 := make(chan struct{})
	done2 := make(chan struct{})

	sm.spawnedHandles["alice"] = spawn.NewInProcessSpawnHandle(
		"inproc-alice", func() {}, done1,
		NewTeamAgent(agentschema.NewAgentCard()),
	)
	sm.spawnedHandles["bob"] = spawn.NewInProcessSpawnHandle(
		"inproc-bob", func() {}, done2,
		NewTeamAgent(agentschema.NewAgentCard()),
	)

	sm.ShutdownAllHandles(context.Background())

	// 验证已清空
	assert.Empty(t, sm.spawnedHandles, "关闭后应清空 spawnedHandles")
}

// TestSpawnManager_OnTeammateUnhealthy_无句柄 测试无句柄时不健康回调不 panic。
func TestSpawnManager_OnTeammateUnhealthy_无句柄(t *testing.T) {
	sm := newTestSpawnManager(t)

	// 没有句柄时不应 panic
	sm.OnTeammateUnhealthy("nonexistent")
}

// TestSpawnManager_OnTeammateUnhealthy_触发重启 测试不健康回调触发重启 goroutine。
func TestSpawnManager_OnTeammateUnhealthy_触发重启(t *testing.T) {
	sm := newTestSpawnManager(t)

	sm.OnTeammateUnhealthy("alice")

	// 等待 goroutine 启动
	time.Sleep(50 * time.Millisecond)

	// 验证 recoveryCancel 中有记录
	sm.mu.Lock()
	_, exists := sm.recoveryCancel["alice"]
	sm.mu.Unlock()
	assert.True(t, exists, "应注册恢复任务取消函数")

	// 清理
	sm.CancelRecoveryTasks()
}

// TestSpawnManager_BuildContextFromDB_占位 测试从 DB 恢复上下文（当前占位）。
func TestSpawnManager_BuildContextFromDB_占位(t *testing.T) {
	sm := newTestSpawnManager(t)

	ctx, err := sm.BuildContextFromDB("alice")
	// 当前返回空上下文 + nil error（TODO #9.64）
	assert.NoError(t, err)
	assert.Equal(t, atschema.TeamRuntimeContext{}, ctx)
}

// TestSpawnManager_PublishRestartEvent_占位 测试发布重启事件（当前占位）。
func TestSpawnManager_PublishRestartEvent_占位(t *testing.T) {
	sm := newTestSpawnManager(t)

	// 当前为 no-op（TODO #9.65），不应 panic
	sm.PublishRestartEvent("alice", 1)
}

// TestSpawnManager_RestartTeammate_无DB上下文 测试重启时 DB 上下文为空。
func TestSpawnManager_RestartTeammate_无DB上下文(t *testing.T) {
	sm := newTestSpawnManager(t)

	// BuildContextFromDB 当前返回空上下文
	// SpawnTeammate 会因空 memberName 而失败，但不应 panic
	err := sm.RestartTeammate(context.Background(), "alice", 1)
	// 可能因配置缺失而失败，但不 panic
	_ = err
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// newTestSpawnManager 创建测试用 SpawnManager。
func newTestSpawnManager(t *testing.T) *SpawnManager {
	t.Helper()
	state := NewTeamAgentState()
	card := agentschema.NewAgentCard()
	configurator := NewAgentConfigurator(card)
	return NewSpawnManager(state, configurator, nil)
}
