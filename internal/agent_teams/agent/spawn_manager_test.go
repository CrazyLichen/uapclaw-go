package agent

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/messager"
	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/schema/events"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/spawn"
	databasetools "github.com/uapclaw/uapclaw-go/internal/agent_teams/tools/database"
	teamstools "github.com/uapclaw/uapclaw-go/internal/agent_teams/tools"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// stubSpawnHandle 测试用 SpawnHandle 桩（模拟 subprocess 句柄）
type stubSpawnHandle struct {
	processID string
	alive     bool
	healthy   bool
}

func (h *stubSpawnHandle) ProcessID() string { return h.processID }
func (h *stubSpawnHandle) IsAlive() bool     { return h.alive }
func (h *stubSpawnHandle) IsHealthy() bool   { return h.healthy }
func (h *stubSpawnHandle) Shutdown(_ context.Context, _ ...time.Duration) (bool, error) {
	return true, nil
}
func (h *stubSpawnHandle) ForceKill() error                                             { return nil }
func (h *stubSpawnHandle) WaitForCompletion() (int, error)                              { return 0, nil }
func (h *stubSpawnHandle) StartHealthCheck(_ context.Context, _ ...time.Duration) error { return nil }
func (h *stubSpawnHandle) StopHealthCheck() error                                       { return nil }

// ──────────────────────────── 导出函数 ────────────────────────────

// TestNewSpawnManager_基本创建 测试 SpawnManager 基本创建。
func TestNewSpawnManager_基本创建(t *testing.T) {
	state := NewTeamAgentState()
	card := agentschema.NewAgentCard()
	configurator := NewAgentConfigurator(card)

	sm := NewSpawnManager(context.Background(), state, configurator, nil)
	assert.NotNil(t, sm)
	assert.NotNil(t, sm.spawnedHandles)
	assert.NotNil(t, sm.recoveryCancel)
}

// TestNewSpawnManager_带TeamAgentGetter 测试带 TeamAgent getter 的创建。
func TestNewSpawnManager_带TeamAgentGetter(t *testing.T) {
	state := NewTeamAgentState()
	card := agentschema.NewAgentCard()
	configurator := NewAgentConfigurator(card)
	ta := NewTeamAgent(context.Background(), card)

	sm := NewSpawnManager(context.Background(), state, configurator, func() *TeamAgent { return ta })
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
		NewTeamAgent(context.Background(), agentCard),
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
		NewTeamAgent(context.Background(), agentschema.NewAgentCard()),
	)
	sm.spawnedHandles["alice"] = inprocHandle

	// ForceKill 会等待 done 关闭，先异步关闭 done
	go close(done)
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
		NewTeamAgent(context.Background(), agentschema.NewAgentCard()),
	)
	sm.spawnedHandles["bob"] = spawn.NewInProcessSpawnHandle(
		"inproc-bob", func() {}, done2,
		NewTeamAgent(context.Background(), agentschema.NewAgentCard()),
	)

	// ForceKill 会等待 done 关闭，先异步关闭 done
	go close(done1)
	go close(done2)
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

	ctx, err := sm.BuildContextFromDB(context.Background(), "alice")
	// team_backend 未配置时返回错误
	assert.Error(t, err)
	assert.Equal(t, atschema.TeamRuntimeContext{}, ctx)
}

// TestSpawnManager_PublishRestartEvent_完整发布 测试 M-31 完整实现。
//
// configurator 注入 InProcessMessager + TeamBackend → Subscribe 捕获事件，
// 断言 topicID/eventType/member_name/restart_count。
func TestSpawnManager_PublishRestartEvent_完整发布(t *testing.T) {
	state := NewTeamAgentState()
	card := agentschema.NewAgentCard()
	configurator := NewAgentConfigurator(card)

	// 注入 InProcessMessager + TeamBackend
	msgr := messager.NewInProcessMessager(atschema.NewMessagerTransportConfig())
	configurator.SetMessager(msgr)
	db := databasetools.NewInMemoryTeamDatabase()
	backend := teamstools.NewTeamBackend("t-restart", "leader", true, db, nil)
	configurator.SetTeamBackend(backend)

	sm := NewSpawnManager(context.Background(), state, configurator, nil)

	// Subscribe 团队 topic 捕获事件
	type captured struct {
		topicID string
		msg     *events.EventMessage
	}
	got := make(chan captured, 1)
	topicID := events.TeamTopicTeam.Build("", "t-restart")
	err := msgr.Subscribe(context.Background(), topicID, func(ctx context.Context, msg *events.EventMessage) error {
		got <- captured{topicID: topicID, msg: msg}
		return nil
	})
	if err != nil {
		t.Fatalf("Subscribe 失败: %v", err)
	}

	sm.PublishRestartEvent(context.Background(), "alice", 3)

	select {
	case c := <-got:
		if c.msg.EventType != events.TeamEventMemberRestarted {
			t.Errorf("EventType = %q, want %q", c.msg.EventType, events.TeamEventMemberRestarted)
		}
		if c.msg.Payload["member_name"] != "alice" {
			t.Errorf("member_name = %v, want alice", c.msg.Payload["member_name"])
		}
		if c.msg.Payload["restart_count"] != 3 {
			t.Errorf("restart_count = %v, want 3", c.msg.Payload["restart_count"])
		}
		// Python publish_restart_event 不传 reason——payload 中 reason 为零值空串
		// （Go ToPayload 固定含 reason 键，零值语义等价 Python None）
		if c.msg.Payload["reason"] != "" {
			t.Errorf("reason = %v, want 空串（对齐 Python 不传 reason）", c.msg.Payload["reason"])
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("2s 内未捕获到 MemberRestartedEvent")
	}
}

// TestSpawnManager_PublishRestartEvent_nil配置不panic 测试降级路径
func TestSpawnManager_PublishRestartEvent_nil配置不panic(t *testing.T) {
	sm := newTestSpawnManager(t)

	// configurator 无 messager/teamBackend → 静默返回，不 panic
	sm.PublishRestartEvent(context.Background(), "alice", 1)
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
	return NewSpawnManager(context.Background(), state, configurator, nil)
}

// ---------------------------------------------------------------------------
// SpawnedHandles / SpawnedHandleNames 句柄查询
// ---------------------------------------------------------------------------

// TestSpawnManager_SpawnedHandles_空 测试无句柄时返回空 map。
func TestSpawnManager_SpawnedHandles_空(t *testing.T) {
	sm := newTestSpawnManager(t)
	handles := sm.SpawnedHandles()
	assert.Empty(t, handles)
}

// TestSpawnManager_SpawnedHandles_有句柄 测试手动添加句柄后返回快照。
func TestSpawnManager_SpawnedHandles_有句柄(t *testing.T) {
	sm := newTestSpawnManager(t)

	handle1 := &stubSpawnHandle{processID: "p1", alive: true}
	handle2 := &stubSpawnHandle{processID: "p2", alive: true}
	sm.mu.Lock()
	sm.spawnedHandles["alice"] = handle1
	sm.spawnedHandles["bob"] = handle2
	sm.mu.Unlock()

	handles := sm.SpawnedHandles()
	assert.Len(t, handles, 2)
	assert.Contains(t, handles, "alice")
	assert.Contains(t, handles, "bob")

	// 验证是快照而非引用
	delete(handles, "alice")
	sm.mu.Lock()
	_, exists := sm.spawnedHandles["alice"]
	sm.mu.Unlock()
	assert.True(t, exists, "删除快照不应影响原始 map")
}

// TestSpawnManager_SpawnedHandleNames_空 测试无句柄时返回空切片。
func TestSpawnManager_SpawnedHandleNames_空(t *testing.T) {
	sm := newTestSpawnManager(t)
	names := sm.SpawnedHandleNames()
	assert.Empty(t, names)
}

// TestSpawnManager_SpawnedHandleNames_有句柄 测试返回所有成员名。
func TestSpawnManager_SpawnedHandleNames_有句柄(t *testing.T) {
	sm := newTestSpawnManager(t)

	sm.mu.Lock()
	sm.spawnedHandles["alice"] = &stubSpawnHandle{processID: "p1"}
	sm.spawnedHandles["bob"] = &stubSpawnHandle{processID: "p2"}
	sm.mu.Unlock()

	names := sm.SpawnedHandleNames()
	assert.Len(t, names, 2)
	assert.Contains(t, names, "alice")
	assert.Contains(t, names, "bob")
}
