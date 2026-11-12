package runtime

import (
	"context"

	"errors"
	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/rails/permissions"
	"testing"

	"github.com/stretchr/testify/require"
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

// TestFacadeListModelCapabilities_待回填返回空 模型目录来源未接线时返回空。
// Python: service.py:718-775（entries 来自 get_default_models + 可执行性过滤）
//
// ⤵️ 待回填：模型路由子系统落地后，此处应产出真实目录。当前返回空是
// 诚实的"未配置模型"态，不是假成功——解析必然失败而不是悄悄放行。
func TestFacadeListModelCapabilities_待回填返回空(t *testing.T) {
	runtime := startedRuntime(t, AgentRuntimeDeps{})
	got := runtime.ListModelCapabilities("")
	require.Empty(t, got.Models)
	require.Empty(t, got.CurrentSelection)
}

// TestBuildModelCatalog_目录构建与解析 底层目录函数本身完整可用。
// Python: service.py:769 return build_model_catalog(...)
func TestBuildModelCatalog_目录构建与解析(t *testing.T) {
	catalog := BuildModelCatalog([]any{
		map[string]any{
			"model_client_config": map[string]any{"model_name": "m1"},
			"is_default":          true,
		},
	}, "")
	require.Len(t, catalog.Models, 1)
	require.Equal(t, "m1#0", catalog.CurrentSelection)

	got, err := ResolveModelSelection(catalog, "m1#0")
	require.NoError(t, err)
	require.Equal(t, "m1", got.ModelName)

	_, err = ResolveModelSelection(catalog, "ghost")
	require.ErrorIs(t, err, ErrModelNotFound)
}

// TestFacadeResolveModelCapability_目录为空时解析失败 未接线不静默放行。
// Python: service.py:777-782
func TestFacadeResolveModelCapability_目录为空时解析失败(t *testing.T) {
	runtime := startedRuntime(t, AgentRuntimeDeps{})
	_, err := runtime.ResolveModelCapability("m1#0")
	require.Error(t, err)
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

// TestCoordinator_未注入时自建 未注入时 Runtime 自建一本账。
// Python: service.py:414 session_coordinator = session_coordinator or RuntimeSessionCoordinator()
//
// 这是"全局只有一本账"的前提：任何创建 Runtime 的路径都自带账本。
// 旧实现返回 nil（等外部注入），导致门面不得不自己再 new 一本 → 两本账。
func TestCoordinator_未注入时自建(t *testing.T) {
	runtime := NewAgentRuntime(AgentRuntimeDeps{})
	coordinator := runtime.Coordinator()
	require.NotNil(t, coordinator, "未注入时应自建")
	require.Same(t, coordinator, runtime.Coordinator(), "多次取同一本账")
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
// TestAgentManager_未注入时自建 AgentRuntime 无 manager 时自建一个。
// Python: plane.py:155 `SingleTenantStrategy(agent_manager or AgentManager())`
// —— 上游 AgentRuntime() 无参调用即自建，故此处不能再返回 nil。
func TestAgentManager_未注入时自建(t *testing.T) {
	runtime := NewAgentRuntime(AgentRuntimeDeps{})
	require.NotNil(t, runtime.AgentManager(), "未注入时应自建 manager")
}

// TestAgentManager_注入时用注入值 外部提供 manager 时不自建。
func TestAgentManager_注入时用注入值(t *testing.T) {
	given := &startTestManager{agent: &startTestChatAgent{}}
	runtime := NewAgentRuntime(AgentRuntimeDeps{Manager: given})
	require.Same(t, given, runtime.AgentManager())
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

// ──────────────────────────── fail-closed 装配守卫 ────────────────────────────

// TestValidateAgentDefinition_未装配模型解析器报错 不静默跳过校验。
// Python: service.py:707-714 resolve_model_capability 恒可用
func TestValidateAgentDefinition_未装配模型解析器报错(t *testing.T) {
	runtime := NewAgentRuntime(AgentRuntimeDeps{})
	definition := map[string]any{
		"name": "abc", "instructions": "do it", "model": "m1",
	}
	_, err := runtime.validateAgentDefinition(definition, "agent.work.normal")
	require.ErrorIs(t, err, ErrModelCatalogUnavailable,
		"未装配时必须报错，不能放行不存在的模型")
}

// TestValidateAgentDefinition_已装配但模型不存在 报 MODEL_NOT_FOUND。
func TestValidateAgentDefinition_已装配但模型不存在(t *testing.T) {
	runtime := NewAgentRuntime(AgentRuntimeDeps{
		ResolveModelCapability: func(model string) (RuntimeModelDescriptor, error) {
			return RuntimeModelDescriptor{}, errors.New("not found")
		},
	})
	definition := map[string]any{
		"name": "abc", "instructions": "do it", "model": "m1",
	}
	_, err := runtime.validateAgentDefinition(definition, "agent.work.normal")
	require.ErrorIs(t, err, ErrAgentDefinitionModelNotFound)
}

// TestValidateAgentDefinition_无model字段跳过校验 不配置模型时不要求解析器。
func TestValidateAgentDefinition_无model字段跳过校验(t *testing.T) {
	runtime := NewAgentRuntime(AgentRuntimeDeps{})
	definition := map[string]any{"name": "abc", "instructions": "do it"}
	execution, err := runtime.validateAgentDefinition(definition, "agent.work.normal")
	require.NoError(t, err)
	require.NotNil(t, execution)
}

// TestBindAgentExecutionRequest_未装配模型解析器报错 不静默丢弃 selection_key。
// Python: service.py:1577-1581 resolve_model_capability(requested).selection_key
func TestBindAgentExecutionRequest_未装配模型解析器报错(t *testing.T) {
	runtime := NewAgentRuntime(AgentRuntimeDeps{})
	execution, err := PrepareAgentExecution(map[string]any{
		"name": "abc", "instructions": "do it", "model": "m1",
	}, "agent.work.normal")
	require.NoError(t, err)

	req := declarativeChatRequest(t, "web", "sess-1", "")
	_, err = runtime.bindAgentExecutionRequest(req, execution)
	require.ErrorIs(t, err, ErrModelCatalogUnavailable)
}
