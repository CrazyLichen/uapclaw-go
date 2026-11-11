package runtime

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	sessionpkg "github.com/uapclaw/uapclaw-go/internal/swarm/runtime/session"
)

// ──────────────────────────── 测试替身 ────────────────────────────

// cleanupRecorder 记录清理各步骤的调用顺序。
type cleanupRecorder struct {
	// steps 调用顺序
	steps []string
}

// record 追加一步。
func (r *cleanupRecorder) record(step string) { r.steps = append(r.steps, step) }

// fakeCoordinator 协调器替身。
type fakeCoordinator struct {
	// recorder 步骤记录
	recorder *cleanupRecorder
	// snapshot 快照返回（nil 表示会话不在协调器中）
	snapshot *sessionpkg.RuntimeSessionSnapshot
	// closeResult CloseSession 返回值
	closeResult sessionpkg.CloseSessionResult
	// closeErr CloseSession 错误
	closeErr error
}

// SnapshotSession 返回预设快照。
func (f *fakeCoordinator) SnapshotSession(string) *sessionpkg.RuntimeSessionSnapshot {
	return f.snapshot
}

// CloseSession 记录并返回预设结果。
func (f *fakeCoordinator) CloseSession(
	sessionID string, _ *int, _ *time.Duration,
) (sessionpkg.CloseSessionResult, error) {
	f.recorder.record("coordinator.CloseSession")
	result := f.closeResult
	result.SessionID = sessionID
	return result, f.closeErr
}

// fakeCleaner 可清理的 manager 替身。
type fakeCleaner struct {
	// recorder 步骤记录
	recorder *cleanupRecorder
	// cleaned 返回是否清理到内容
	cleaned bool
	// err 返回错误
	err error
}

// GetChatAgent 端口实现（清理用例不用）。
func (fakeCleaner) GetChatAgent(context.Context, string, string, string, string, map[string]any, string) (ChatAgentPort, error) {
	return nil, context.Canceled
}

// GetChatAgentNoWait 端口实现（清理用例不用）。
func (fakeCleaner) GetChatAgentNoWait(string, string, string, string) ChatAgentPort { return nil }

// CleanupSessionRuntime 记录并返回预设结果。
func (f fakeCleaner) CleanupSessionRuntime(context.Context, string, string) (bool, error) {
	f.recorder.record("manager.CleanupSessionRuntime")
	return f.cleaned, f.err
}

// plainManager 不可清理的 manager 替身（验证类型断言跳过）。
type plainManager struct{}

// GetChatAgent 端口实现。
func (plainManager) GetChatAgent(context.Context, string, string, string, string, map[string]any, string) (ChatAgentPort, error) {
	return nil, context.Canceled
}

// GetChatAgentNoWait 端口实现。
func (plainManager) GetChatAgentNoWait(string, string, string, string) ChatAgentPort { return nil }

// ──────────────────────────── 四步顺序 ────────────────────────────

// TestCleanupSession_四步顺序 严格顺序：关协调器 → 清 manager → 解绑 owner → 重置 plan。
// Python: service.py:3052/3058/3066/3070
func TestCleanupSession_四步顺序(t *testing.T) {
	recorder := &cleanupRecorder{}
	planReset := make([]string, 0)
	runtime := NewAgentRuntime(AgentRuntimeDeps{
		Manager:     fakeCleaner{recorder: recorder, cleaned: true},
		Coordinator: &fakeCoordinator{recorder: recorder, snapshot: activeSnapshot("s1")},
		ResetPlanSession: func(sessionID string) {
			recorder.record("plan.ResetSession")
			planReset = append(planReset, sessionID)
		},
	})
	require.NoError(t, runtime.claimAgentExecutionOwner(
		ownerRequest("web", "s1"), ownerExecution(t, "v1")))

	cleaned, err := runtime.CleanupSession(context.Background(), "web", "s1", true)
	require.NoError(t, err)
	require.True(t, cleaned)
	require.Equal(t, []string{
		"coordinator.CloseSession",
		"manager.CleanupSessionRuntime",
		"plan.ResetSession",
	}, recorder.steps)
	require.Equal(t, []string{"s1"}, planReset)

	// 第 3 步 forget 不记录步骤，用表状态验证
	require.Nil(t, runtime.agentExecutionOwner(ownerRequest("web", "s1")), "owner 应已解绑")
}

// TestCleanupSession_协调器无该会话则跳过关闭 快照 nil 时不调 CloseSession。
// Python: service.py:3052 if self._owns_session(session_id)
func TestCleanupSession_协调器无该会话则跳过关闭(t *testing.T) {
	recorder := &cleanupRecorder{}
	runtime := NewAgentRuntime(AgentRuntimeDeps{
		Manager:     fakeCleaner{recorder: recorder},
		Coordinator: &fakeCoordinator{recorder: recorder, snapshot: nil},
	})
	_, err := runtime.CleanupSession(context.Background(), "web", "s1", false)
	require.NoError(t, err)
	require.NotContains(t, recorder.steps, "coordinator.CloseSession")
}

// TestCleanupSession_已关闭的会话不再关闭 快照状态为 closed 时跳过。
// Python: service.py:1242 snapshot.state is not RuntimeSessionState.CLOSED
func TestCleanupSession_已关闭的会话不再关闭(t *testing.T) {
	recorder := &cleanupRecorder{}
	runtime := NewAgentRuntime(AgentRuntimeDeps{
		Manager: fakeCleaner{recorder: recorder},
		Coordinator: &fakeCoordinator{recorder: recorder, snapshot: &sessionpkg.RuntimeSessionSnapshot{
			SessionID: "s1", State: sessionpkg.RuntimeSessionStateClosed,
		}},
	})
	_, err := runtime.CleanupSession(context.Background(), "web", "s1", false)
	require.NoError(t, err)
	require.NotContains(t, recorder.steps, "coordinator.CloseSession")
}

// TestCleanupSession_关闭超时报错 timed_out 非空时报 SessionCloseTimeoutError。
// Python: service.py:3056-3057
func TestCleanupSession_关闭超时报错(t *testing.T) {
	recorder := &cleanupRecorder{}
	runtime := NewAgentRuntime(AgentRuntimeDeps{
		Manager: fakeCleaner{recorder: recorder},
		Coordinator: &fakeCoordinator{
			recorder: recorder,
			snapshot: activeSnapshot("s1"),
			closeResult: sessionpkg.CloseSessionResult{
				TimedOut: []string{"exec-1"},
			},
		},
	})
	_, err := runtime.CleanupSession(context.Background(), "web", "s1", true)
	require.Error(t, err)
	var timeoutErr *sessionCloseTimeoutError
	require.ErrorAs(t, err, &timeoutErr)
	require.Equal(t, "s1", timeoutErr.SessionID)
	require.Equal(t, []string{"exec-1"}, timeoutErr.TimedOut)
}

// ──────────────────────────── resetPlanState 两路 ────────────────────────────

// TestCleanupSession_resetPlanState为false不重置 plan 删除事务路径传 false。
// Python: service.py:3070 if reset_plan_state
func TestCleanupSession_resetPlanState为false不重置(t *testing.T) {
	recorder := &cleanupRecorder{}
	runtime := NewAgentRuntime(AgentRuntimeDeps{
		Manager:     fakeCleaner{recorder: recorder},
		Coordinator: &fakeCoordinator{recorder: recorder},
		ResetPlanSession: func(string) {
			recorder.record("plan.ResetSession")
		},
	})
	_, err := runtime.CleanupSession(context.Background(), "web", "s1", false)
	require.NoError(t, err)
	require.NotContains(t, recorder.steps, "plan.ResetSession")
}

// TestCleanupSession_resetPlanState为true重置 断连/CLI 路径传 true。
func TestCleanupSession_resetPlanState为true重置(t *testing.T) {
	recorder := &cleanupRecorder{}
	runtime := NewAgentRuntime(AgentRuntimeDeps{
		Manager:     fakeCleaner{recorder: recorder},
		Coordinator: &fakeCoordinator{recorder: recorder},
		ResetPlanSession: func(string) {
			recorder.record("plan.ResetSession")
		},
	})
	_, err := runtime.CleanupSession(context.Background(), "web", "s1", true)
	require.NoError(t, err)
	require.Contains(t, recorder.steps, "plan.ResetSession")
}

// TestCleanupSession_未注入plan重置不panic ResetPlanSession 为 nil 时安全。
func TestCleanupSession_未注入plan重置不panic(t *testing.T) {
	recorder := &cleanupRecorder{}
	runtime := NewAgentRuntime(AgentRuntimeDeps{
		Manager:     fakeCleaner{recorder: recorder},
		Coordinator: &fakeCoordinator{recorder: recorder},
	})
	_, err := runtime.CleanupSession(context.Background(), "web", "s1", true)
	require.NoError(t, err)
}

// ──────────────────────────── 失败语义（D-2） ────────────────────────────

// TestCleanupSession_清理失败仍解绑owner manager 报错时 owner 仍被解绑。
// Python: service.py:3066 forget 在 manager 循环之后无条件执行
func TestCleanupSession_清理失败仍解绑owner(t *testing.T) {
	recorder := &cleanupRecorder{}
	runtime := NewAgentRuntime(AgentRuntimeDeps{
		Manager:     fakeCleaner{recorder: recorder, err: errors.New("cleanup boom")},
		Coordinator: &fakeCoordinator{recorder: recorder},
	})
	require.NoError(t, runtime.claimAgentExecutionOwner(
		ownerRequest("web", "s1"), ownerExecution(t, "v1")))

	_, err := runtime.CleanupSession(context.Background(), "web", "s1", true)
	require.NoError(t, err, "spec D-2：单条清理失败只告警不中断")
	require.Nil(t, runtime.agentExecutionOwner(ownerRequest("web", "s1")))
}

// TestCleanupSession_协调器错误上抛 关协调器失败直接返回错误，不继续后续步骤。
// Python:3055 await close_session 抛错即传播
func TestCleanupSession_协调器错误上抛(t *testing.T) {
	recorder := &cleanupRecorder{}
	runtime := NewAgentRuntime(AgentRuntimeDeps{
		Manager: fakeCleaner{recorder: recorder},
		Coordinator: &fakeCoordinator{
			recorder: recorder,
			snapshot: activeSnapshot("s1"),
			closeErr: errors.New("close boom"),
		},
	})
	_, err := runtime.CleanupSession(context.Background(), "web", "s1", true)
	require.Error(t, err)
	require.Contains(t, err.Error(), "close boom")
	require.NotContains(t, recorder.steps, "manager.CleanupSessionRuntime")
}

// ──────────────────────────── 边界 ────────────────────────────

// TestCleanupSession_关闭后拒绝 Runtime 已关闭时报错。
// Python: service.py:3037 if self._closed: raise RuntimeStateError
func TestCleanupSession_关闭后拒绝(t *testing.T) {
	runtime := NewAgentRuntime(AgentRuntimeDeps{})
	require.NoError(t, runtime.Close())

	_, err := runtime.CleanupSession(context.Background(), "web", "s1", true)
	require.Error(t, err)
	require.Contains(t, err.Error(), "already closed")
}

// TestCleanupSession_不可清理的manager被跳过 无 CleanupSessionRuntime 的 manager 跳过。
// Python: 上游所有 manager 都有该方法；Go 用类型断言容错
func TestCleanupSession_不可清理的manager被跳过(t *testing.T) {
	runtime := NewAgentRuntime(AgentRuntimeDeps{
		Manager: plainManager{},
	})
	cleaned, err := runtime.CleanupSession(context.Background(), "web", "s1", false)
	require.NoError(t, err)
	require.False(t, cleaned)
}

// TestCleanupSession_遍历多个manager 注入多 manager 时逐个清理，任一命中即为真。
// Python: service.py:3061-3065 for manager in managers: cleaned = bool(...) or cleaned
func TestCleanupSession_遍历多个manager(t *testing.T) {
	first := &cleanupRecorder{}
	second := &cleanupRecorder{}
	runtime := NewAgentRuntime(AgentRuntimeDeps{
		Manager: fakeCleaner{recorder: first, cleaned: false},
		IterManagers: func() []AgentManagerPort {
			return []AgentManagerPort{
				fakeCleaner{recorder: first, cleaned: false},
				fakeCleaner{recorder: second, cleaned: true},
			}
		},
	})
	cleaned, err := runtime.CleanupSession(context.Background(), "web", "s1", false)
	require.NoError(t, err)
	require.True(t, cleaned, "第二个 manager 清理到内容")
	require.Equal(t, []string{"manager.CleanupSessionRuntime"}, first.steps)
	require.Equal(t, []string{"manager.CleanupSessionRuntime"}, second.steps)
}

// TestCleanupSession_空会话仍可清理 session 为空时不 panic。
func TestCleanupSession_空会话仍可清理(t *testing.T) {
	recorder := &cleanupRecorder{}
	runtime := NewAgentRuntime(AgentRuntimeDeps{
		Manager:     fakeCleaner{recorder: recorder},
		Coordinator: &fakeCoordinator{recorder: recorder},
	})
	_, err := runtime.CleanupSession(context.Background(), "", "", false)
	require.NoError(t, err)
}

// ──────────────────────────── 辅助 ────────────────────────────

// activeSnapshot 构造一个活跃会话快照。
func activeSnapshot(sessionID string) *sessionpkg.RuntimeSessionSnapshot {
	return &sessionpkg.RuntimeSessionSnapshot{
		SessionID: sessionID,
		State:     sessionpkg.RuntimeSessionStateActive,
	}
}

// RegisterSession 登记会话（记账并返回空快照）。
func (f *fakeCoordinator) RegisterSession(
	sessionID, _ string, _ sessionpkg.SessionPersistencePolicy,
) (sessionpkg.RuntimeSessionSnapshot, error) {
	f.recorder.record("coordinator.RegisterSession")
	return sessionpkg.RuntimeSessionSnapshot{SessionID: sessionID}, nil
}

// RunUnary 执行任务并透传结果。
func (f *fakeCoordinator) RunUnary(
	ctx context.Context, _, _ string, _ sessionpkg.SessionWorkKind,
	task func(context.Context) (any, error), _ sessionpkg.UnaryOptions,
) (any, error) {
	f.recorder.record("coordinator.RunUnary")
	return task(ctx)
}

// CancelExecution 记录取消并返回空结果。
func (f *fakeCoordinator) CancelExecution(
	_ string, _ sessionpkg.CancelExecutionOptions,
) (sessionpkg.CancelExecutionResult, error) {
	f.recorder.record("coordinator.CancelExecution")
	return sessionpkg.CancelExecutionResult{}, nil
}

// SnapshotSessions 返回空快照集。
func (f *fakeCoordinator) SnapshotSessions() []sessionpkg.RuntimeSessionSnapshot {
	return nil
}

// Close 记录关闭。
func (f *fakeCoordinator) Close(_ *time.Duration, _ *[]string) error {
	f.recorder.record("coordinator.Close")
	return nil
}

// 编译期断言：测试替身满足扩展后的端口
var _ SessionCoordinatorPort = (*fakeCoordinator)(nil)

// QuiesceSession 置会话 QUIESCING 并返回生命周期上下文。
// Python: RuntimeSessionCoordinator.quiesce_session
func (f *fakeCoordinator) QuiesceSession(
	sessionID string, _ *int,
) (*sessionpkg.SessionLifecycleContext, error) {
	f.recorder.record("coordinator.QuiesceSession")
	return &sessionpkg.SessionLifecycleContext{SessionID: sessionID}, nil
}
