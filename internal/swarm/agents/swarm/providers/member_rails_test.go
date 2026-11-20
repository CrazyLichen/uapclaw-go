package providers_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/swarm"
	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/swarm/providers"
)

// newTestView 构造满足 providers.SwarmContextView 的最小平台上下文。
// SwarmBuildContext（父包）内嵌 *atschema.BuildContext 指针（基类字段
// Role/ProjectDir/Language/Workspace 等在其中），必须显式初始化基类。
func newTestView() *swarm.SwarmBuildContext {
	return &swarm.SwarmBuildContext{BuildContext: &atschema.BuildContext{}}
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
	assert.Nil(t, out7, "基座 service 绑定段未实施，返回 nil（15.20 回收点）")
}

// fakeHeartbeatService 心跳服务占位（any 别名）。
type fakeHeartbeatService struct{}

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
	providers.SetContextProcessorRailBuilder(func(config map[string]any) any {
		return fakeContextProcessorRail{}
	})
	defer providers.SetContextProcessorRailBuilder(nil)
	out3, err3 := provider(map[string]any{}, newTestView())
	require.NoError(t, err3)
	assert.NotNil(t, out3, "注入后应产出 rail")
}

// fakeContextProcessorRail 注入回环用例的 rail 占位。
type fakeContextProcessorRail struct{}

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
	assert.Nil(t, out2, "leader 命中但群聊基座未实施（F2 ⤴️25.6）")
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
	assert.Nil(t, out, "开关开但基座 ⤴️9.11b，返回 nil")

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
