package rails_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/models"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/rails"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/team_workspace"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/tools/worktree"
)

// TestTeamHandleKey_键值逐字对齐 17 个键值字符串必须与上游逐字一致。
//
// 键值字符串是 checkpoint 契约（spec 可序列化持久化），改格式会让既有
// 持久化数据失配。上游 team_context.py:65-81。
func TestTeamHandleKey_键值逐字对齐(t *testing.T) {
	cases := map[string]rails.TeamHandleKey{
		"team.team_backend":                   rails.TeamHandleTeamBackend,
		"team.workspace_manager":              rails.TeamHandleWorkspaceManager,
		"team.model_allocator":                rails.TeamHandleModelAllocator,
		"team.messager":                       rails.TeamHandleMessager,
		"team.swarmflow_model_resolver":       rails.TeamHandleSwarmflowModelResolver,
		"team.swarmflow_worker_base_spec":     rails.TeamHandleSwarmflowWorkerBaseSpec,
		"team.swarmflow_human_base_spec":      rails.TeamHandleSwarmflowHumanBaseSpec,
		"team.swarmflow_concurrency_governor": rails.TeamHandleSwarmflowConcurrencyGovernor,
		"team.swarmflow_budget":               rails.TeamHandleSwarmflowBudget,
		"team.reliability_components":         rails.TeamHandleReliabilityComponents,
		"team.permissions_override":           rails.TeamHandlePermissionsOverride,
		"team.worktree_manager":               rails.TeamHandleWorktreeManager,
		"team.swarmflow_external_cli_agents":  rails.TeamHandleSwarmflowExternalCLIAgents,
		"team.swarmflow_expert_specs":         rails.TeamHandleSwarmflowExpertSpecs,
		"team.expert_catalog":                 rails.TeamHandleExpertCatalog,
		"team.expert_binding":                 rails.TeamHandleExpertBinding,
		"team.task_event_tracker":             rails.TeamHandleTaskEventTracker,
	}
	assert.Len(t, cases, 17, "必须恰好 17 个键")

	for want, got := range cases {
		assert.Equal(t, want, string(got), "键值必须与上游逐字一致")
	}
}

// TestInjectTeamHandles_17键全落 传入句柄后 Extras 有 17 个键。
//
// 对齐上游 inject_team_handles：nil 也写入键（`extras[key] = None`），
// 查询方据此区分"未注入该键"与"注入为 nil"。
func TestInjectTeamHandles_17键全落(t *testing.T) {
	ctx := &hschema.BuildContext{}

	rails.InjectTeamHandles(ctx, rails.TeamHandles{})

	extras := ctx.GetExtras()
	assert.Len(t, extras, 17, "17 个键全部写入（含 nil 值）")
	_, exists := extras[string(rails.TeamHandleTeamBackend)]
	assert.True(t, exists, "nil 句柄也占位")
}

// TestInjectTeamHandles_存活句柄可取回 注入的具体句柄能被 Get* 取回。
func TestInjectTeamHandles_存活句柄可取回(t *testing.T) {
	ctx := &hschema.BuildContext{}
	backend := &tools.TeamBackend{}

	rails.InjectTeamHandles(ctx, rails.TeamHandles{
		TeamBackend: backend,
	})

	assert.Same(t, backend, rails.GetTeamBackend(ctx), "应取回同一指针")
}

// TestInjectTeamHandles_专家表nil写空map 对齐上游 `dict(swarmflow_expert_specs or {})`。
//
// 上游不是写 None 而是写空 dict，故注入后取回的是空 map 而非 nil。
func TestInjectTeamHandles_专家表nil写空map(t *testing.T) {
	ctx := &hschema.BuildContext{}

	rails.InjectTeamHandles(ctx, rails.TeamHandles{SwarmflowExpertSpecs: nil})

	got := ctx.GetExtras()[string(rails.TeamHandleSwarmflowExpertSpecs)]
	require.NotNil(t, got, "不应是裸 nil")
	m, ok := got.(map[string]any)
	require.True(t, ok, "应是 map")
	assert.Empty(t, m)
}

// TestGetTeamBackend_缺失返回nil 空 Extras 时各 getter 返回 nil 不 panic。
func TestGetTeamBackend_缺失返回nil(t *testing.T) {
	ctx := &hschema.BuildContext{}

	assert.Nil(t, rails.GetTeamBackend(ctx))
	assert.Nil(t, rails.GetTeamWorkspaceManager(ctx))
	assert.Nil(t, rails.GetModelAllocator(ctx))
	assert.Nil(t, rails.GetMessager(ctx))
	assert.Nil(t, rails.GetWorktreeManager(ctx))
	assert.Nil(t, rails.GetTaskEventTracker(ctx))
}

// TestGetTeamBackend_nilContext安全 ctx 为 nil 时不 panic。
func TestGetTeamBackend_nilContext安全(t *testing.T) {
	assert.Nil(t, rails.GetTeamBackend(nil))
	assert.Nil(t, rails.GetMessager(nil))
	assert.Nil(t, rails.GetTeamWorkspaceManager(nil))
}

// TestGetTeamBackend_类型不符返回nil Extras 里塞了错误类型时安全返回 nil。
func TestGetTeamBackend_类型不符返回nil(t *testing.T) {
	ctx := &hschema.BuildContext{}
	ctx.GetExtras()[string(rails.TeamHandleTeamBackend)] = "not-a-backend"

	assert.Nil(t, rails.GetTeamBackend(ctx), "类型断言失败应返回 nil 而非 panic")
}

// TestGetWorkspaceManager_类型正确可取回 验证各具体类型的 getter 能取回。
func TestGetWorkspaceManager_类型正确可取回(t *testing.T) {
	ctx := &hschema.BuildContext{}
	wsMgr := &team_workspace.TeamWorkspaceManager{}
	wtMgr := &worktree.WorktreeManager{}

	rails.InjectTeamHandles(ctx, rails.TeamHandles{
		WorkspaceManager: wsMgr,
		WorktreeManager:  wtMgr,
	})

	assert.Same(t, wsMgr, rails.GetTeamWorkspaceManager(ctx))
	assert.Same(t, wtMgr, rails.GetWorktreeManager(ctx))
}

// TestGetModelAllocator_接口类型可取回 ModelAllocator 是接口，验证 type-assert 到接口。
func TestGetModelAllocator_接口类型可取回(t *testing.T) {
	ctx := &hschema.BuildContext{}
	var alloc models.ModelAllocator = &models.RoundRobinModelAllocator{}

	rails.InjectTeamHandles(ctx, rails.TeamHandles{ModelAllocator: alloc})

	assert.NotNil(t, rails.GetModelAllocator(ctx))
}

// TestGetSwarmflowExternalCLIAgents_缺失返回空切片 对齐上游 `tuple(... or ())`。
func TestGetSwarmflowExternalCLIAgents_缺失返回空切片(t *testing.T) {
	ctx := &hschema.BuildContext{}

	got := rails.GetSwarmflowExternalCLIAgents(ctx)

	assert.NotNil(t, got, "应对齐上游返回空 tuple 而非 None")
	assert.Empty(t, got)
}

// TestGetSwarmflowExpertSpecs_缺失返回空map 对齐上游 `dict(... or {})`。
func TestGetSwarmflowExpertSpecs_缺失返回空map(t *testing.T) {
	ctx := &hschema.BuildContext{}

	got := rails.GetSwarmflowExpertSpecs(ctx)

	assert.NotNil(t, got, "应对齐上游返回空 dict 而非 None")
	assert.Empty(t, got)
}

// TestGetPermissionsOverride_缺失返回nil 权限收窄缺失时返回 nil（上游 Optional）。
func TestGetPermissionsOverride_缺失返回nil(t *testing.T) {
	ctx := &hschema.BuildContext{}

	assert.Nil(t, rails.GetPermissionsOverride(ctx))
}

// TestGetPermissionsOverride_有值可取回 注入后能取回。
func TestGetPermissionsOverride_有值可取回(t *testing.T) {
	ctx := &hschema.BuildContext{}
	override := map[string]string{"bash": "deny"}

	rails.InjectTeamHandles(ctx, rails.TeamHandles{PermissionsOverride: override})

	got := rails.GetPermissionsOverride(ctx)
	require.NotNil(t, got)
	m, ok := got.(map[string]string)
	require.True(t, ok)
	assert.Equal(t, "deny", m["bash"])
}
