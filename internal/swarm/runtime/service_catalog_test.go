package runtime

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/rails/permissions"
)

// startedRuntime 构造并启动一个 Runtime。
func startedRuntime(t *testing.T, deps AgentRuntimeDeps) *AgentRuntime {
	t.Helper()
	runtime := NewAgentRuntime(deps)
	require.NoError(t, runtime.Start())
	return runtime
}

// ──────────────────────────── 启动守卫 ────────────────────────────

// TestFacade_未启动时拒绝 上游各门面先 _require_started。
// Python: service.py:719/778/787/793/799/806/814 self._require_started()
func TestFacade_未启动时拒绝(t *testing.T) {
	runtime := NewAgentRuntime(AgentRuntimeDeps{})

	_, err := runtime.ResolveModelCapability("m1")
	require.Error(t, err)
	require.Contains(t, err.Error(), "not started")

	_, err = runtime.GetSession(SessionGetInput{ChannelID: "web", SessionID: "s1"})
	require.Error(t, err)

	_, err = runtime.ListSessions(NewSessionListInput("web"))
	require.Error(t, err)

	_, err = runtime.GetPermissionSnapshot(PermissionSnapshotInput{ChannelID: "web"})
	require.Error(t, err)
}

// TestFacade_关闭后拒绝 关闭后门面同样拒绝。
func TestFacade_关闭后拒绝(t *testing.T) {
	runtime := NewAgentRuntime(AgentRuntimeDeps{})
	require.NoError(t, runtime.Start())
	require.NoError(t, runtime.Close())

	_, err := runtime.ResolveModelCapability("m1")
	require.Error(t, err)
	require.Contains(t, err.Error(), "already closed")
}

// ──────────────────────────── 模式门面 ────────────────────────────

// TestFacadeListModeCapabilities_四模式 门面委托包级函数。
// Python: service.py:784-789
func TestFacadeListModeCapabilities_四模式(t *testing.T) {
	runtime := NewAgentRuntime(AgentRuntimeDeps{})
	require.Len(t, runtime.ListModeCapabilities().Modes, 4)
}

// TestFacadeResolveModeCapability_命中与未命中 合法模式返回，未知报错。
// Python: service.py:791-796
func TestFacadeResolveModeCapability_命中与未命中(t *testing.T) {
	runtime := NewAgentRuntime(AgentRuntimeDeps{})

	got, err := runtime.ResolveModeCapability("agent.code.plan")
	require.NoError(t, err)
	require.True(t, got.IsPlan)

	_, err = runtime.ResolveModeCapability("bogus")
	require.ErrorIs(t, err, ErrModeNotFound)
}

// ──────────────────────────── 模型门面 ────────────────────────────

// TestFacadeListModelCapabilities_未注入返回空 无模型提供者时返回空目录。
// Python: service.py:733-775 未配置模型时目录为空
func TestFacadeListModelCapabilities_未注入返回空(t *testing.T) {
	runtime := startedRuntime(t, AgentRuntimeDeps{})
	require.Empty(t, runtime.ListModelCapabilities("").Models)
}

// TestFacadeListModelCapabilities_注入后构建目录 条目经目录构建产出选择键。
// Python: service.py:769 return build_model_catalog(...)
func TestFacadeListModelCapabilities_注入后构建目录(t *testing.T) {
	runtime := startedRuntime(t, AgentRuntimeDeps{
		IterModelEntries: func() []any {
			return []any{
				map[string]any{
					"model_client_config": map[string]any{"model_name": "m1"},
					"is_default":          true,
				},
			}
		},
	})
	got := runtime.ListModelCapabilities("")
	require.Len(t, got.Models, 1)
	require.Equal(t, "m1#0", got.CurrentSelection)
}

// TestFacadeResolveModelCapability_命中与未命中 目录解析成功与失败。
// Python: service.py:777-782
func TestFacadeResolveModelCapability_命中与未命中(t *testing.T) {
	runtime := startedRuntime(t, AgentRuntimeDeps{
		IterModelEntries: func() []any {
			return []any{
				map[string]any{
					"model_client_config": map[string]any{"model_name": "m1"},
					"is_default":          true,
				},
			}
		},
	})

	got, err := runtime.ResolveModelCapability("m1#0")
	require.NoError(t, err)
	require.Equal(t, "m1", got.ModelName)

	_, err = runtime.ResolveModelCapability("ghost")
	require.ErrorIs(t, err, ErrModelNotFound)
}

// ──────────────────────────── 会话门面 ────────────────────────────

// TestFacadeGetSession_委托 门面透传到包级 GetSession（参数校验一致）。
// Python: service.py:798-803
func TestFacadeGetSession_委托(t *testing.T) {
	runtime := startedRuntime(t, AgentRuntimeDeps{})

	_, err := runtime.GetSession(SessionGetInput{ChannelID: "web", SessionID: ""})
	require.ErrorIs(t, err, ErrSessionIDRequired)
}

// TestFacadeListSessions_默认单Agent范围 门面默认 include_team=false。
// Python: service.py:805-810
func TestFacadeListSessions_默认单Agent范围(t *testing.T) {
	runtime := startedRuntime(t, AgentRuntimeDeps{})

	got, err := runtime.ListSessions(SessionListInput{ChannelID: "no-such-channel", Limit: 20})
	require.NoError(t, err)
	require.Equal(t, 0, got.Total)
}

// ──────────────────────────── 权限门面 ────────────────────────────

// TestFacadeGetPermissionSnapshot_委托 门面透传到包级快照读取。
// Python: service.py:812-820
func TestFacadeGetPermissionSnapshot_委托(t *testing.T) {
	stubCapturePermissionLayers(t, permissionsLayers())
	runtime := startedRuntime(t, AgentRuntimeDeps{})

	got, err := runtime.GetPermissionSnapshot(PermissionSnapshotInput{ChannelID: "web"})
	require.NoError(t, err)
	require.Equal(t, PermissionScopeHost, got.Scope)
}

// ──────────────────────────── 访问器 ────────────────────────────

// TestCoordinator_返回注入实例 多次取同一指针。
// Python: service.py:412 self._session_coordinator
func TestCoordinator_返回注入实例(t *testing.T) {
	recorder := &cleanupRecorder{}
	coordinator := &fakeCoordinator{recorder: recorder}
	runtime := NewAgentRuntime(AgentRuntimeDeps{Coordinator: coordinator})

	require.Same(t, coordinator, runtime.Coordinator())
	require.Same(t, coordinator, runtime.Coordinator())
}

// TestCoordinator_未注入返回nil 未注入时为 nil 不 panic。
func TestCoordinator_未注入返回nil(t *testing.T) {
	runtime := NewAgentRuntime(AgentRuntimeDeps{})
	require.Nil(t, runtime.Coordinator())
}

// TestAgentManager_返回注入实例 多次取同一指针。
// Python: service.py:476 self._tenant.get_default_manager()
func TestAgentManager_返回注入实例(t *testing.T) {
	manager := fakeCleaner{recorder: &cleanupRecorder{}}
	runtime := NewAgentRuntime(AgentRuntimeDeps{Manager: manager})

	require.NotNil(t, runtime.AgentManager())
	require.Equal(t, AgentManagerPort(manager), runtime.AgentManager())
}

// TestAgentManager_未注入返回nil 未注入 manager 时为 nil。
func TestAgentManager_未注入返回nil(t *testing.T) {
	runtime := NewAgentRuntime(AgentRuntimeDeps{})
	require.Nil(t, runtime.AgentManager())
}

// permissionsLayers 构造一个最小四层结果供权限门面测试。
func permissionsLayers() permissions.PermissionLayers {
	return permissions.PermissionLayers{
		Global:    map[string]any{"enabled": true},
		User:      map[string]any{},
		Session:   map[string]any{},
		Effective: map[string]any{},
	}
}

// TestRuntimeAccessor_清理路径可用 访问器返回的协调器可被 CleanupSession 使用。
func TestRuntimeAccessor_清理路径可用(t *testing.T) {
	recorder := &cleanupRecorder{}
	coordinator := &fakeCoordinator{recorder: recorder, snapshot: activeSnapshot("s1")}
	runtime := NewAgentRuntime(AgentRuntimeDeps{
		Manager:     fakeCleaner{recorder: recorder},
		Coordinator: coordinator,
	})

	// 走公开访问器确认拿到同一个协调器
	require.Same(t, coordinator, runtime.Coordinator())

	_, err := runtime.CleanupSession(context.Background(), "web", "s1", false)
	require.NoError(t, err)
	require.Equal(t, []string{"coordinator.CloseSession", "manager.CleanupSessionRuntime"}, recorder.steps)
}
