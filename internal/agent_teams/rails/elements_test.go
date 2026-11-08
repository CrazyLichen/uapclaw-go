package rails_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/rails"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
)

// useTeamRails 每个用例前重置注册表并注册团队 rail。
func useTeamRails(t *testing.T) {
	t.Helper()
	hschema.ResetRailRegistryForTest()
	rails.ResetRegistrationForTest()
	t.Cleanup(func() {
		hschema.ResetRailRegistryForTest()
		rails.ResetRegistrationForTest()
	})
	rails.RegisterTeamRailProviders()
}

// TestTeamRailConstants_8个逐字对齐 8 个 rail 名常量必须与上游 elements.py:64-71 一致。
func TestTeamRailConstants_8个逐字对齐(t *testing.T) {
	assert.Equal(t, "core.team.tool", rails.TeamTool)
	assert.Equal(t, "core.team.policy", rails.TeamPolicy)
	assert.Equal(t, "core.team.workspace", rails.TeamWorkspace)
	assert.Equal(t, "core.team.tool_approval", rails.TeamToolApproval)
	assert.Equal(t, "core.team.plan_mode", rails.TeamPlanMode)
	assert.Equal(t, "core.team.reliability", rails.TeamReliability)
	assert.Equal(t, "core.team.skill_use", rails.TeamSkillUse)
	assert.Equal(t, "core.team.observability", rails.TeamObservability)
}

// TestTeamRailProviders_8个全部可查 注册后 8 个 type 名在注册表里全部命中（含 3 个占位）。
func TestTeamRailProviders_8个全部可查(t *testing.T) {
	useTeamRails(t)

	for _, name := range []string{
		rails.TeamTool, rails.TeamPolicy, rails.TeamWorkspace,
		rails.TeamToolApproval, rails.TeamPlanMode,
		rails.TeamReliability, rails.TeamSkillUse, rails.TeamObservability,
	} {
		_, ok := hschema.LookupRailProvider(name)
		assert.True(t, ok, "rail %q 必须已注册", name)
	}
}

// TestTeamRailProvider_占位返回nil不报错 三个占位 provider 返回 (nil,nil)。
//
// 与「未知 type」同语义：跳过但不中断构建（决策 C8）。
func TestTeamRailProvider_占位返回nil不报错(t *testing.T) {
	useTeamRails(t)

	for _, name := range []string{rails.TeamReliability, rails.TeamSkillUse, rails.TeamObservability} {
		provider, ok := hschema.LookupRailProvider(name)
		require.True(t, ok, "%q 应有占位 provider", name)

		out, err := provider(map[string]any{}, &hschema.BuildContext{})
		assert.NoError(t, err, "%q 占位 provider 不得报错", name)
		assert.Nil(t, out, "%q 占位 provider 应返回 nil", name)
	}
}

// TestTeamRailProvider_占位名统一前缀 团队 rail 名统一 core.team. 前缀。
func TestTeamRailProvider_占位名统一前缀(t *testing.T) {
	for _, name := range []string{
		rails.TeamTool, rails.TeamPolicy, rails.TeamWorkspace,
		rails.TeamToolApproval, rails.TeamPlanMode,
		rails.TeamReliability, rails.TeamSkillUse, rails.TeamObservability,
	} {
		assert.True(t, strings.HasPrefix(name, "core.team."), "团队 rail 名前缀：%q", name)
	}
}

// TestBuildTeamToolRail_无后端返回nil 门卫：无团队后端时不出 rail。
//
// 对齐上游 build_team_tool_rail 的 `backend = get_team_backend(context); if backend is None: return None`。
func TestBuildTeamToolRail_无后端返回nil(t *testing.T) {
	useTeamRails(t)

	provider, ok := hschema.LookupRailProvider(rails.TeamTool)
	require.True(t, ok)

	out, err := provider(map[string]any{}, &hschema.BuildContext{})

	require.NoError(t, err)
	assert.Nil(t, out, "无 backend 时应返回 nil")
}

// TestBuildTeamToolRail_有后端产出实例 有后端时产出 rail 实例。
func TestBuildTeamToolRail_有后端产出实例(t *testing.T) {
	useTeamRails(t)

	ctx := &hschema.BuildContext{}
	rails.InjectTeamHandles(ctx, rails.TeamHandles{TeamBackend: &tools.TeamBackend{}})

	provider, _ := hschema.LookupRailProvider(rails.TeamTool)
	out, err := provider(map[string]any{"role": "teammate", "team_name": "t1"}, ctx)

	require.NoError(t, err)
	require.NotNil(t, out, "有 backend 时应产出实例")
	_, isRail := out.(*rails.TeamToolRail)
	assert.True(t, isRail, "应返回 *TeamToolRail")
}

// TestBuildTeamPolicyRail_总是产出 policy rail 无条件挂载。
//
// 对齐上游 build_team_policy_rail：「Build the team policy rail (always mounted)」。
func TestBuildTeamPolicyRail_总是产出(t *testing.T) {
	useTeamRails(t)

	provider, ok := hschema.LookupRailProvider(rails.TeamPolicy)
	require.True(t, ok)

	out, err := provider(map[string]any{"member_name": "alice"}, &hschema.BuildContext{Language: "cn"})

	require.NoError(t, err)
	require.NotNil(t, out, "policy rail 应无条件产出")
	_, isRail := out.(*rails.TeamPolicyRail)
	assert.True(t, isRail, "应返回 *TeamPolicyRail")
}

// TestBuildTeamWorkspaceRail_无管理器返回nil 无工作空间管理器时不出 rail。
func TestBuildTeamWorkspaceRail_无管理器返回nil(t *testing.T) {
	useTeamRails(t)

	provider, ok := hschema.LookupRailProvider(rails.TeamWorkspace)
	require.True(t, ok)

	out, err := provider(map[string]any{}, &hschema.BuildContext{})

	require.NoError(t, err)
	assert.Nil(t, out, "无 workspace manager 时应返回 nil")
}

// TestBuildTeamPlanModeRail_产出 plan 模式轨 plan 模式轨无条件产出（是否挂载由 TeamRailSpecs 决定）。
func TestBuildTeamPlanModeRail_产出(t *testing.T) {
	useTeamRails(t)

	provider, ok := hschema.LookupRailProvider(rails.TeamPlanMode)
	require.True(t, ok)

	out, err := provider(map[string]any{}, &hschema.BuildContext{Language: "en"})

	require.NoError(t, err)
	require.NotNil(t, out)
	_, isRail := out.(*rails.TeamPlanModeRail)
	assert.True(t, isRail, "应返回 *TeamPlanModeRail")
}

// TestBuildTeamToolApprovalRail_门卫 无 backend/messager 或无审批工具时返回 nil。
func TestBuildTeamToolApprovalRail_门卫(t *testing.T) {
	useTeamRails(t)

	provider, ok := hschema.LookupRailProvider(rails.TeamToolApproval)
	require.True(t, ok)

	// 无 backend + 无 messager
	out, err := provider(map[string]any{"tool_names": []string{"bash"}}, &hschema.BuildContext{})
	require.NoError(t, err)
	assert.Nil(t, out, "无 backend/messager 时返回 nil")

	// 有 backend 但无审批工具
	ctx := &hschema.BuildContext{}
	rails.InjectTeamHandles(ctx, rails.TeamHandles{TeamBackend: &tools.TeamBackend{}})
	out, err = provider(map[string]any{}, ctx)
	require.NoError(t, err)
	assert.Nil(t, out, "无 tool_names 时返回 nil")
}

// TestTeamRailSpecs_挂载顺序 顺序必须是 tool → policy → workspace → tool_approval → plan_mode。
func TestTeamRailSpecs_挂载顺序(t *testing.T) {
	specs := rails.TeamRailSpecs(rails.TeamRailSpecInput{
		Role:                "leader",
		MemberName:          "l1",
		HasTeamBackend:      true,
		HasWorkspaceManager: true,
		IsTeamPlanLeader:    true,
	})

	require.Len(t, specs, 4, "无审批工具时不出 tool_approval")
	assert.Equal(t, rails.TeamTool, specs[0].Type, "tool 必须最先（急切初始化语义）")
	assert.Equal(t, rails.TeamPolicy, specs[1].Type)
	assert.Equal(t, rails.TeamWorkspace, specs[2].Type)
	assert.Equal(t, rails.TeamPlanMode, specs[3].Type)
}

// TestTeamRailSpecs_无后端不出tool 无团队后端时不出 tool rail。
func TestTeamRailSpecs_无后端不出tool(t *testing.T) {
	specs := rails.TeamRailSpecs(rails.TeamRailSpecInput{HasTeamBackend: false})

	require.Len(t, specs, 1, "只剩 policy")
	assert.Equal(t, rails.TeamPolicy, specs[0].Type, "policy 无条件挂载")
}

// TestTeamRailSpecs_predefined剔除spawn工具 预定义团队要剔除动态 spawn 工具。
func TestTeamRailSpecs_predefined剔除spawn工具(t *testing.T) {
	exclude := []string{"spawn_teammate", "spawn_human_agent", "spawn_bridge_agent"}
	specs := rails.TeamRailSpecs(rails.TeamRailSpecInput{
		HasTeamBackend: true,
		ExcludeTools:   exclude,
	})

	require.NotEmpty(t, specs)
	assert.Equal(t, exclude, specs[0].Params["exclude_tools"])
}

// TestTeamRailSpecs_审批轨门卫 受协调队友 + 有审批工具 + 后端消息齐备 + 非团队权限时才出。
func TestTeamRailSpecs_审批轨门卫(t *testing.T) {
	base := rails.TeamRailSpecInput{
		HasTeamBackend:        true,
		IsCoordinatedTeammate: true,
		ApprovalTools:         []string{"bash"},
		HasMessager:           true,
	}

	hasApproval := func(specs []hschema.RailSpec) bool {
		for _, s := range specs {
			if s.Type == rails.TeamToolApproval {
				return true
			}
		}
		return false
	}

	assert.True(t, hasApproval(rails.TeamRailSpecs(base)), "条件齐备时应出")

	withPerm := base
	withPerm.TeamPermissionsEnabled = true
	assert.False(t, hasApproval(rails.TeamRailSpecs(withPerm)), "团队权限模式下由 TeamPermissionRail 接管")

	notCoordinated := base
	notCoordinated.IsCoordinatedTeammate = false
	assert.False(t, hasApproval(rails.TeamRailSpecs(notCoordinated)), "非受协调队友不出")

	noTools := base
	noTools.ApprovalTools = nil
	assert.False(t, hasApproval(rails.TeamRailSpecs(noTools)), "无审批工具不出")

	noMessager := base
	noMessager.HasMessager = false
	assert.False(t, hasApproval(rails.TeamRailSpecs(noMessager)), "无消息总线不出")
}

// TestTeamRailSpecs_planMode仅Leader 非 team.plan Leader 不出 plan_mode。
func TestTeamRailSpecs_planMode仅Leader(t *testing.T) {
	specs := rails.TeamRailSpecs(rails.TeamRailSpecInput{
		Role:             "teammate",
		IsTeamPlanLeader: false,
	})

	for _, s := range specs {
		assert.NotEqual(t, rails.TeamPlanMode, s.Type, "非 team.plan Leader 不得出 plan_mode")
	}
}

// TestTeamRailSpecs_params完整传递 policy params 应包含全部身份字段。
func TestTeamRailSpecs_params完整传递(t *testing.T) {
	specs := rails.TeamRailSpecs(rails.TeamRailSpecInput{
		Role:         "teammate",
		MemberName:   "alice",
		Language:     "cn",
		Lifecycle:    "persistent",
		DispatchMode: "scheduled",
		TeamMode:     "default",
		TeamName:     "t1",
	})

	require.NotEmpty(t, specs)
	policy := specs[0]
	assert.Equal(t, rails.TeamPolicy, policy.Type)
	assert.Equal(t, "teammate", policy.Params["role"])
	assert.Equal(t, "alice", policy.Params["member_name"])
	assert.Equal(t, "cn", policy.Params["language"])
	assert.Equal(t, "persistent", policy.Params["lifecycle"])
	assert.Equal(t, "scheduled", policy.Params["dispatch_mode"])
}
