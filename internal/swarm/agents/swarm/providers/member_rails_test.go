package providers_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	atschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/schema"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/rails"
	hschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/schema"
	coderails "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/code/rails"
	sessionmtools "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/tools"
	teamrails "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/team/rails"
	buildctx "github.com/uapclaw/uapclaw-go/internal/swarm/agents/swarm/buildctx"
	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/swarm/providers"
)

// newTestView 构造最小平台上下文（buildctx 叶包具体类型；内嵌
// *atschema.BuildContext 指针须显式初始化基类字段）。
func newTestView() *buildctx.SwarmBuildContext {
	return &buildctx.SwarmBuildContext{BuildContext: &atschema.BuildContext{}}
}

// TestMemberRailProviders_注册全集注册 member_rails 全部注册名可查。
func TestMemberRailProviders_注册全集注册(t *testing.T) {
	providers.RegisterMemberRailProviders()

	for _, name := range []string{
		"swarm.organization_owner_inbox",
		"swarm.organization_model",
		"swarm.organization_availability",
		"swarm.session_messaging_route",
		"swarm.heartbeat",
		"swarm.personal_context",
		"swarm.skill_retrieval_prompt",
		"swarm.symphony_orchestration_prompt",
		"swarm.runtime_prompt",
		"swarm.team_skill_storage_policy",
		"swarm.team_skill_library_reload",
		"swarm.team_workspace_report_path",
		"swarm.context_processor",
		"swarm.model_anomaly_detection",
		"swarm.plugin_rails",
		"swarm.team_permission_policy",
		"swarm.team_permission",
	} {
		_, ok := hschema.LookupRailProvider(name)
		require.True(t, ok, "rail provider 未注册: %s", name)
	}
}

// TestBuiltinRailProviders_注册全集注册 builtin_rails 四件可查。
func TestBuiltinRailProviders_注册全集注册(t *testing.T) {
	providers.RegisterBuiltinRailProviders()

	for _, name := range []string{
		"swarm.response_prompt",
		"swarm.stream_event",
		"swarm.avatar_prompt",
		"swarm.multimodal_image",
	} {
		_, ok := hschema.LookupRailProvider(name)
		require.True(t, ok, "rail provider 未注册: %s", name)
	}
}

// TestBuildRuntimePromptRail_leader角色分支 runtime_prompt rail 按
// project_dir/task 路径分支构造（Python :346-379）。
func TestBuildRuntimePromptRail_分支构造(t *testing.T) {
	providers.RegisterMemberRailProviders()

	// 有项目：else 前分支，cwd=project_dir
	view := newTestView()
	view.Role = "leader"
	view.ProjectDir = "/tmp/proj-x"
	view.Channel = "web"
	view.Language = "cn"

	provider, ok := hschema.LookupRailProvider("swarm.runtime_prompt")
	require.True(t, ok)
	out, err := provider(nil, view)
	require.NoError(t, err)
	require.NotNil(t, out)

	// projectless：task 路径齐全 → projectless 分支走 resolve_member_work_dir
	view2 := newTestView()
	view2.ProjectDir = ""
	view2.TaskWorkspaceRoot = "/tmp/team-ws/artifacts"
	view2.TeamOutputsDir = "/tmp/team-ws/artifacts/outputs"
	out2, err2 := provider(nil, view2)
	require.NoError(t, err2)
	require.NotNil(t, out2)
}

// TestBuildHeartbeatRail_门控矩阵 心跳 rail 三类跳过 + service/session 缺省。
//
// Python: _build_heartbeat_rail (member_rails.py:143-196)。
func TestBuildHeartbeatRail_门控矩阵(t *testing.T) {
	providers.RegisterMemberRailProviders()
	provider, ok := hschema.LookupRailProvider("swarm.heartbeat")
	require.True(t, ok)

	// service 缺 → nil
	view := newTestView()
	view.SessionID = "sess-1"
	out, err := provider(nil, view)
	require.NoError(t, err)
	assert.Nil(t, out, "service 为 nil 应跳过")

	// session 缺 → nil
	view2 := newTestView()
	view2.HeartbeatJobService = fakeHeartbeatService{}
	out2, err2 := provider(nil, view2)
	require.NoError(t, err2)
	assert.Nil(t, out2, "session 为空应跳过")

	// cron 元数据 → nil
	view3 := newTestView()
	view3.SessionID = "sess-1"
	view3.HeartbeatJobService = fakeHeartbeatService{}
	view3.RequestMetadata = map[string]any{"cron": true}
	out3, err3 := provider(nil, view3)
	require.NoError(t, err3)
	assert.Nil(t, out3, "cron 会话应跳过")

	// __cron__ 渠道 → nil
	view4 := newTestView()
	view4.SessionID = "sess-1"
	view4.HeartbeatJobService = fakeHeartbeatService{}
	view4.ChannelID = "__cron__"
	out4, err4 := provider(nil, view4)
	require.NoError(t, err4)
	assert.Nil(t, out4, "__cron__ 渠道应跳过")

	// cron_* 会话 id → nil
	view5 := newTestView()
	view5.SessionID = "cron_123"
	view5.HeartbeatJobService = fakeHeartbeatService{}
	out5, err5 := provider(nil, view5)
	require.NoError(t, err5)
	assert.Nil(t, out5, "cron_* 会话应跳过")

	// xiaoyi 渠道 → nil
	view6 := newTestView()
	view6.SessionID = "sess-1"
	view6.HeartbeatJobService = fakeHeartbeatService{}
	view6.ChannelID = "xiaoyi"
	out6, err6 := provider(nil, view6)
	require.NoError(t, err6)
	assert.Nil(t, out6, "无心跳支持渠道应跳过")

	// 全部通过门控（基座 service 绑定段 ⤴️15.20，工厂返回 nil 属预期）
	view7 := newTestView()
	view7.SessionID = "sess-1"
	view7.HeartbeatJobService = fakeHeartbeatService{}
	view7.ChannelID = "web"
	out7, err7 := provider(nil, view7)
	require.NoError(t, err7)
	// 15.20 ⑫：骨架已回填——正常渠道返回 rail（工具注册缝 ⤴️16.4）
	require.NotNil(t, out7, "service/context 绑定段已随 15.20 ⑫ 回填")
	hb := out7.(*coderails.HeartbeatRail)
	assert.Equal(t, "web", hb.ToolContext()["channel_id"])
	assert.Equal(t, "sess-1", hb.ToolContext()["session_id"])
	assert.Empty(t, hb.RegisteredToolNames(), "Bridge 缺省降级：无工具注册")
}

// fakeHeartbeatService 心跳服务占位（any 别名）。
type fakeHeartbeatService struct{}

// HandleOperation 窄面实现（25.6 批二接口收窄连带）。
func (fakeHeartbeatService) HandleOperation(string, map[string]any, string, string, string, string) (map[string]any, error) {
	return nil, nil
}

// TestBuildTeamPermissionRail_门控 backend/messager/权限开关三重门控。
//
// Python: _build_team_permission_rail (member_rails.py:813-819)。
func TestBuildTeamPermissionRail_门控(t *testing.T) {
	providers.RegisterMemberRailProviders()
	provider, ok := hschema.LookupRailProvider("swarm.team_permission")
	require.True(t, ok)

	// 无 backend → nil
	view := newTestView()
	out, err := provider(nil, view)
	require.NoError(t, err)
	assert.Nil(t, out, "backend 缺失应跳过")

	// 权限未开 → nil（无 backend 时先短路，此处验证 params 门控）
	view2 := newTestView()
	out2, err2 := provider(map[string]any{
		"permissions_config": map[string]any{"enabled": false},
	}, view2)
	require.NoError(t, err2)
	assert.Nil(t, out2)
}

// TestBuildContextProcessorRail_开关门控 上下文压缩 rail 开关门控。
func TestBuildContextProcessorRail_开关门控(t *testing.T) {
	providers.RegisterMemberRailProviders()
	provider, ok := hschema.LookupRailProvider("swarm.context_processor")
	require.True(t, ok)

	// 显式关闭 → nil
	out, err := provider(map[string]any{"context_engine_enabled": false}, newTestView())
	require.NoError(t, err)
	assert.Nil(t, out)

	// 默认开（context_engine_enabled 缺省 true）→ 基座 builder 未注入时 nil
	//（D-15.16f-view 配套缝：注册段注入后通电，见注入回环用例）
	out2, err2 := provider(map[string]any{}, newTestView())
	require.NoError(t, err2)
	assert.Nil(t, out2, "builder 未注入（team 基座经注册段注入）应跳过")

	// 注入回环：注入后产出 rail
	providers.SetContextProcessorRailBuilder(func(config map[string]any) agentinterfaces.AgentRail {
		return &fakeContextProcessorRail{}
	})
	defer providers.SetContextProcessorRailBuilder(nil)
	out3, err3 := provider(map[string]any{}, newTestView())
	require.NoError(t, err3)
	assert.NotNil(t, out3, "注入后应产出 rail")
}

// fakeContextProcessorRail 注入回环用例的 rail 占位（嵌入 BaseRail 满足
// AgentRail 接口面）。
type fakeContextProcessorRail struct {
	rails.DeepAgentRail
}

// TestBuildModelAnomalyDetection_开关门控 execution_guard 门控（params 入口）。
func TestBuildModelAnomalyDetection_开关门控(t *testing.T) {
	providers.RegisterMemberRailProviders()
	provider, ok := hschema.LookupRailProvider("swarm.model_anomaly_detection")
	require.True(t, ok)

	// 未开 → nil
	out, err := provider(map[string]any{
		"rail_config": map[string]any{"enabled": false},
	}, newTestView())
	require.NoError(t, err)
	assert.Nil(t, out)

	// 开 → 产出 rail
	out2, err2 := provider(map[string]any{
		"rail_config": map[string]any{"enabled": true, "max_retries": 3},
	}, newTestView())
	require.NoError(t, err2)
	assert.NotNil(t, out2)
}

// TestBuildSessionMessagingRoute_leaderOnly 非 leader 返回 nil（F2 缝分支）。
func TestBuildSessionMessagingRoute_leaderOnly(t *testing.T) {
	providers.RegisterMemberRailProviders()
	provider, ok := hschema.LookupRailProvider("swarm.session_messaging_route")
	require.True(t, ok)

	view := newTestView()
	view.Role = "teammate"
	out, err := provider(nil, view)
	require.NoError(t, err)
	assert.Nil(t, out, "teammate 不挂 session messaging 路由")

	view.Role = "leader"
	out2, err2 := provider(nil, view)
	require.NoError(t, err2)
	// 25.6 批一 T5：缝已填体（对齐 member_rails.py:132 返回 SessionMessagingRouteRail()）
	assert.NotNil(t, out2, "leader 应返回 SessionMessagingRouteRail")
	_, isRail := out2.(*sessionmtools.SessionMessagingRouteRail)
	assert.True(t, isRail, "返回类型应为 SessionMessagingRouteRail")
}

// TestBuildSymphonyOrchestration_leaderOnly leader-only 门控（领域十六缝）。
func TestBuildSymphonyOrchestration_leaderOnly(t *testing.T) {
	providers.RegisterMemberRailProviders()
	provider, ok := hschema.LookupRailProvider("swarm.symphony_orchestration_prompt")
	require.True(t, ok)

	view := newTestView()
	view.Role = "teammate"
	out, err := provider(nil, view)
	require.NoError(t, err)
	assert.Nil(t, out)
}

// TestBuildSkillRetrievalPrompt_开关门控 检索开关门控（9.11b 缝）。
func TestBuildSkillRetrievalPrompt_开关门控(t *testing.T) {
	providers.RegisterMemberRailProviders()
	provider, ok := hschema.LookupRailProvider("swarm.skill_retrieval_prompt")
	require.True(t, ok)

	view := newTestView()
	view.Config = map[string]any{"skill_retrieval": map[string]any{"enabled": true}}
	out, err := provider(nil, view)
	require.NoError(t, err)
	assert.Nil(t, out, "开关开但基座 ⤴️16.4，返回 nil")

	view.Config = map[string]any{}
	out2, err2 := provider(nil, view)
	require.NoError(t, err2)
	assert.Nil(t, out2, "开关关直接跳过")
}

// TestBuildContextNonPlatform_返回nil 非平台上下文（最小 BuildContext）
// 经 railProviderAdapter 返回 nil 不 panic。
func TestBuildContextNonPlatform_返回nil(t *testing.T) {
	providers.RegisterMemberRailProviders()
	provider, ok := hschema.LookupRailProvider("swarm.runtime_prompt")
	require.True(t, ok)

	out, err := provider(nil, &hschema.BuildContext{Language: "cn"})
	require.NoError(t, err)
	assert.Nil(t, out, "非平台上下文应跳过")
}

// TestRegisteredSubagentAndToolProviders_可查 code_subagents/runtime_tools/
// tools/design 注册名可查（Task 6 覆盖面回归）。
func TestRegisteredSubagentAndToolProviders_可查(t *testing.T) {
	providers.RegisterSubagentProviders()
	providers.RegisterToolProviders()
	providers.RegisterRuntimeToolProviders()
	providers.RegisterSkillProviders()
	providers.RegisterDesignProviders()

	for _, name := range []string{
		"swarm.code_agent", "swarm.statusline_setup_agent",
		"swarm.browser_agent", "swarm.cua_agent",
	} {
		_, ok := hschema.LookupSubagentProvider(name)
		require.True(t, ok, "subagent provider 未注册: %s", name)
	}
	for _, name := range []string{
		"swarm.skill_toolkit", "swarm.skill_retrieval", "swarm.user_todos",
		"swarm.video", "swarm.image_gen", "swarm.video_gen", "swarm.visual_gen",
		"swarm.xiaoyi_phone", "swarm.symphony_toolkit",
		"swarm.a4p_intent_authorization", "swarm.code_extra_tools",
		"swarm.organization_owner_inbox_tools", "swarm.organization_model_tools",
		"swarm.session_messaging_tools", "swarm.cron_tools", "swarm.send_file",
		"swarm.designer_graph_tools",
	} {
		_, ok := hschema.LookupToolProvider(name)
		require.True(t, ok, "tool provider 未注册: %s", name)
	}
	for _, name := range []string{
		"swarm.member_skill_toolkit",
		"swarm.designer_authoring", "swarm.designer_execution",
		"swarm.designer_surface_sanitizer",
	} {
		_, ok := hschema.LookupRailProvider(name)
		require.True(t, ok, "rail provider 未注册: %s", name)
	}
}

// TestBuildCronTools_cron会话跳过 cron 执行会话不挂 cron 工具。
func TestBuildCronTools_cron会话跳过(t *testing.T) {
	providers.RegisterRuntimeToolProviders()
	provider, ok := hschema.LookupToolProvider("swarm.cron_tools")
	require.True(t, ok)

	view := newTestView()
	view.MemberCardID = "card-1"
	view.ChannelID = "web"
	view.RequestMetadata = map[string]any{"cron": true}
	tools, err := provider(nil, view)
	require.NoError(t, err)
	assert.Empty(t, tools, "cron 执行会话应跳过")
}

// TestBuildSendFileTools_门控矩阵 request/channel 缺失与 send_file_allowed 门控。
func TestBuildSendFileTools_门控矩阵(t *testing.T) {
	providers.RegisterRuntimeToolProviders()
	provider, ok := hschema.LookupToolProvider("swarm.send_file")
	require.True(t, ok)

	// 缺 request_id → nil
	view := newTestView()
	view.ChannelID = "web"
	tools, err := provider(nil, view)
	require.NoError(t, err)
	assert.Empty(t, tools)

	// 渠道禁用 → nil
	view2 := newTestView()
	view2.RequestID = "req-1"
	view2.ChannelID = "web"
	tools2, err2 := provider(map[string]any{
		"channels_config": map[string]any{"web": map[string]any{"send_file_allowed": false}},
	}, view2)
	require.NoError(t, err2)
	assert.Empty(t, tools2)
}

// TestBrowserKeyOf 每成员浏览器键派生（member_name 优先、session 折入）。
func TestBrowserKeyOf(t *testing.T) {
	assert.Empty(t, providers.BrowserKeyOf("", "", ""))
	assert.Equal(t, "leader", providers.BrowserKeyOf("", "", "leader"))
	assert.Equal(t, "sess-1-browser-x", providers.BrowserKeyOf("sess-1", "browser-x", "teammate"))
}

// TestBuildOrganizationModelAvailability_Rail切换 ⑤半区工厂闭合断言。
func TestBuildOrganizationModelAvailability_Rail切换(t *testing.T) {
	providers.RegisterMemberRailProviders()
	provider, ok := hschema.LookupRailProvider("swarm.organization_model")
	require.True(t, ok)
	view := newTestView()
	view.TeamID = "team-1"
	view.SessionID = "sess-1"
	view.Role = "leader"
	out, err := provider(nil, view)
	require.NoError(t, err)
	require.NotNil(t, out, "基座已随 15.20 ⑤落地（backend 缝 nil ⤴️25.9）")
	_, isModelRail := out.(*teamrails.OrganizationModelRail)
	assert.True(t, isModelRail)

	provider2, _ := hschema.LookupRailProvider("swarm.organization_availability")
	out2, err2 := provider2(nil, view)
	require.NoError(t, err2)
	require.NotNil(t, out2)
	_, isAvailRail := out2.(*teamrails.OrganizationAvailabilityRail)
	assert.True(t, isAvailRail)
}

// TestBuildTeamSkillStoragePolicy_Rail切换 ④工厂闭合断言。
func TestBuildTeamSkillStoragePolicy_Rail切换(t *testing.T) {
	providers.RegisterMemberRailProviders()
	provider, ok := hschema.LookupRailProvider("swarm.team_skill_storage_policy")
	require.True(t, ok)
	view := newTestView()
	view.GlobalSkillsDir = "/global/skills"
	view.TeamWsRoot = "/team/ws"
	view.TeamSkillVisibilityPath = "/team/ws/skills-visibility.json"
	out, err := provider(nil, view)
	require.NoError(t, err)
	require.NotNil(t, out)
	_, isPolicyRail := out.(*teamrails.TeamSkillStoragePolicyRail)
	assert.True(t, isPolicyRail)

	// Skill 库未知 → nil（Python 门控）
	view2 := newTestView()
	out2, err2 := provider(nil, view2)
	require.NoError(t, err2)
	assert.Nil(t, out2)

	// library_reload
	provider3, _ := hschema.LookupRailProvider("swarm.team_skill_library_reload")
	out3, err3 := provider3(nil, view)
	require.NoError(t, err3)
	require.NotNil(t, out3)
	_, isReloadRail := out3.(*teamrails.TeamSkillLibraryReloadRail)
	assert.True(t, isReloadRail)
}

// TestBuildTeamPermissionPolicy_Rail切换 ⑥半区工厂闭合断言。
func TestBuildTeamPermissionPolicy_Rail切换(t *testing.T) {
	providers.RegisterMemberRailProviders()
	provider, ok := hschema.LookupRailProvider("swarm.team_permission_policy")
	require.True(t, ok)
	view := newTestView()
	view.Language = "cn"
	params := map[string]any{
		"permissions_config": map[string]any{
			"enabled": true,
			"tools":   map[string]any{"bash": "allow"},
		},
	}
	out, err := provider(params, view)
	require.NoError(t, err)
	require.NotNil(t, out)
	_, isPolicyRail := out.(*teamrails.TeamPermissionPolicyRail)
	assert.True(t, isPolicyRail)

	// enabled=false → nil
	params2 := map[string]any{"permissions_config": map[string]any{"enabled": false}}
	out2, err2 := provider(params2, view)
	require.NoError(t, err2)
	assert.Nil(t, out2)
}

// TestBuildHeartbeatRail_ToolContext形状 对齐 Python :183-196 tool_context。
func TestBuildHeartbeatRail_ToolContext形状(t *testing.T) {
	providers.RegisterMemberRailProviders()
	provider, ok := hschema.LookupRailProvider("swarm.heartbeat")
	require.True(t, ok)
	view := newTestView()
	view.HeartbeatJobService = fakeHeartbeatService{}
	view.SessionID = "sess-hb"
	view.ChannelID = "web"
	view.UserID = "u1"
	view.Mode = "team"
	view.RequestMetadata = map[string]any{"member_card_id": "mc-9"}
	out, err := provider(nil, view)
	require.NoError(t, err)
	require.NotNil(t, out)
	hb := out.(*coderails.HeartbeatRail)
	tc := hb.ToolContext()
	assert.Equal(t, "web", tc["channel_id"])
	assert.Equal(t, "sess-hb", tc["session_id"])
	assert.Equal(t, "u1", tc["user_id"])
	assert.Equal(t, "team", tc["mode"])
	assert.Equal(t, "team_member_mc-9", tc["tool_scope"])
}
