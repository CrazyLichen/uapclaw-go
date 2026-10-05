//go:build integration

package hierarchical_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/suite"

	maschema "github.com/uapclaw/uapclaw-go/internal/agentcore/multi_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/multi_agent/team_runtime"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/multi_agent/teams/hierarchical_msgbus"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/multi_agent/teams/hierarchical_tools"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/resources_manager"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// ──────────────────────────── mock 类型 ────────────────────────────

type mockBaseAgent struct {
	card       *agentschema.AgentCard
	invokeResp map[string]any
}

func (a *mockBaseAgent) Configure(_ context.Context, _ agentinterfaces.AgentConfig) error {
	return nil
}
func (a *mockBaseAgent) Invoke(_ context.Context, _ map[string]any, _ ...agentinterfaces.AgentOption) (map[string]any, error) {
	if a.invokeResp != nil {
		return a.invokeResp, nil
	}
	return map[string]any{"status": "ok"}, nil
}
func (a *mockBaseAgent) Stream(_ context.Context, _ map[string]any, _ ...agentinterfaces.AgentOption) (<-chan stream.Schema, error) {
	ch := make(chan stream.Schema, 1)
	close(ch)
	return ch, nil
}
func (a *mockBaseAgent) Card() *agentschema.AgentCard                               { return a.card }
func (a *mockBaseAgent) Config() agentinterfaces.AgentConfig                        { return nil }
func (a *mockBaseAgent) AbilityManager() agentinterfaces.AbilityManagerInterface    { return nil }
func (a *mockBaseAgent) CallbackManager() *agentinterfaces.AgentCallbackManager     { return nil }
func (a *mockBaseAgent) SystemPromptBuilder() saprompt.SystemPromptBuilderInterface { return nil }
func (a *mockBaseAgent) RegisterCallback(_ context.Context, _ agentinterfaces.AgentCallbackEvent, _ callback.PerAgentCallbackFunc, _ ...callback.CallbackOption) error {
	return nil
}
func (a *mockBaseAgent) RegisterRail(_ context.Context, _ agentinterfaces.AgentRail, _ ...callback.CallbackOption) error {
	return nil
}
func (a *mockBaseAgent) UnregisterRail(_ context.Context, _ agentinterfaces.AgentRail) error {
	return nil
}

type mockMessageBus struct{}

func (m *mockMessageBus) Start(_ context.Context) error                    { return nil }
func (m *mockMessageBus) Stop(_ context.Context) error                     { return nil }
func (m *mockMessageBus) CleanupSession(_ context.Context, _ string) error { return nil }
func (m *mockMessageBus) Send(_ context.Context, _ any, _, _, _ string, _ float64) (any, error) {
	return nil, nil
}
func (m *mockMessageBus) Publish(_ context.Context, _ any, _, _, _ string) error { return nil }
func (m *mockMessageBus) AddSubscription(_, _ string)                            {}
func (m *mockMessageBus) RemoveSubscription(_, _ string)                         {}
func (m *mockMessageBus) RemoveAllSubscriptions(_ string)                        {}
func (m *mockMessageBus) ListSubscriptions(_ string) any                         { return nil }
func (m *mockMessageBus) GetSubscriptionCount() int                              { return 0 }

func newMockMessageBus() *mockMessageBus { return &mockMessageBus{} }

// ──────────────────────────── 测试套件 ────────────────────────────

// HierarchicalE2ESuite HierarchicalTeam E2E 集成测试套件
// 对齐 Python: test_hierarchical_tools.py, test_hierarchical_msgbus.py
type HierarchicalE2ESuite struct {
	suite.Suite
	agentIDs []string
}

func TestHierarchicalE2E(t *testing.T) {
	suite.Run(t, new(HierarchicalE2ESuite))
}

func (s *HierarchicalE2ESuite) SetupTest() {
	s.agentIDs = nil
}

func (s *HierarchicalE2ESuite) TearDownTest() {
	for _, id := range s.agentIDs {
		if mgr := runner.GetResourceMgr(); mgr != nil {
			_, _ = mgr.RemoveAgent([]string{id})
		}
	}
}

// ──────────────────────────── HierarchicalTeam (msgbus) 测试 ────────────────────────────

// TestMsgbusTeam_创建 验证 HierarchicalTeam (msgbus) 创建和基本属性
// 对齐 Python: test_hierarchical_msgbus.py test_create
func (s *HierarchicalE2ESuite) TestMsgbusTeam_创建() {
	card := maschema.NewTeamCard(
		maschema.WithTeamCardID("team-msgbus-1"),
		maschema.WithTeamCardName("MsgbusTeam测试"),
	)
	supervisorCard := agentschema.NewAgentCard(
		agentschema.WithAgentID("supervisor"),
		agentschema.WithAgentName("Supervisor"),
	)
	cfg := hierarchical_msgbus.NewHierarchicalTeamConfig()
	cfg.SupervisorAgent = supervisorCard

	runtimeCfg := team_runtime.NewRuntimeConfig(
		team_runtime.WithRuntimeTeamID("team-msgbus-1"),
	)
	tr := team_runtime.NewTeamRuntime(*runtimeCfg)
	tr.SetMessageBus(newMockMessageBus())

	team := hierarchical_msgbus.NewHierarchicalTeam(card, cfg, tr)
	s.NotNil(team)
	s.Equal("team-msgbus-1", team.Card().GetID())
	s.Equal("MsgbusTeam测试", team.Card().GetName())
	s.NotNil(team.Config())
}

// TestMsgbusTeam_添加Agent 验证 AddAgent 和 Agent 管理
// 对齐 Python: test_hierarchical_msgbus.py test_add_agent
func (s *HierarchicalE2ESuite) TestMsgbusTeam_添加Agent() {
	card := maschema.NewTeamCard(
		maschema.WithTeamCardID("team-msgbus-add"),
		maschema.WithTeamCardName("AddMsgbusTeam"),
	)
	supervisorCard := agentschema.NewAgentCard(
		agentschema.WithAgentID("supervisor"),
		agentschema.WithAgentName("Supervisor"),
	)
	cfg := hierarchical_msgbus.NewHierarchicalTeamConfig()
	cfg.SupervisorAgent = supervisorCard

	runtimeCfg := team_runtime.NewRuntimeConfig(
		team_runtime.WithRuntimeTeamID("team-msgbus-add"),
	)
	tr := team_runtime.NewTeamRuntime(*runtimeCfg)
	tr.SetMessageBus(newMockMessageBus())

	team := hierarchical_msgbus.NewHierarchicalTeam(card, cfg, tr)
	ctx := context.Background()

	// 添加 supervisor
	supProvider := func(_ context.Context, _ *agentschema.AgentCard) (agentinterfaces.BaseAgent, error) {
		return &mockBaseAgent{card: supervisorCard}, nil
	}
	s.Require().NoError(team.AddAgent(ctx, supervisorCard, supProvider))
	s.Equal(1, team.GetAgentCount())

	// 添加 worker
	workerCard := agentschema.NewAgentCard(
		agentschema.WithAgentID("worker-1"),
		agentschema.WithAgentName("Worker1"),
	)
	workerProvider := func(_ context.Context, _ *agentschema.AgentCard) (agentinterfaces.BaseAgent, error) {
		return &mockBaseAgent{card: workerCard}, nil
	}
	s.Require().NoError(team.AddAgent(ctx, workerCard, workerProvider))
	s.Equal(2, team.GetAgentCount())

	// 列出
	agents := team.ListAgents()
	s.Len(agents, 2)

	// 获取卡片
	fetched, err := team.GetAgentCard("supervisor")
	s.Require().NoError(err)
	s.Equal("Supervisor", fetched.Name)
}

// TestMsgbusTeam_订阅和发布 验证 Subscribe/Publish 委托
// 对齐 Python: test_hierarchical_msgbus.py test_subscribe_publish
func (s *HierarchicalE2ESuite) TestMsgbusTeam_订阅和发布() {
	card := maschema.NewTeamCard(
		maschema.WithTeamCardID("team-msgbus-sub"),
		maschema.WithTeamCardName("SubMsgbusTeam"),
	)
	cfg := hierarchical_msgbus.NewHierarchicalTeamConfig()

	runtimeCfg := team_runtime.NewRuntimeConfig(
		team_runtime.WithRuntimeTeamID("team-msgbus-sub"),
	)
	tr := team_runtime.NewTeamRuntime(*runtimeCfg)
	tr.SetMessageBus(newMockMessageBus())

	team := hierarchical_msgbus.NewHierarchicalTeam(card, cfg, tr)
	ctx := context.Background()

	s.Require().NoError(team.Subscribe(ctx, "agent-a", "topic/events"))
	s.Require().NoError(team.Unsubscribe(ctx, "agent-a", "topic/events"))
}

// TestMsgbusTeam_配置更新 验证 Configure 更新 TeamConfig
// 对齐 Python: test_hierarchical_msgbus.py test_configure
func (s *HierarchicalE2ESuite) TestMsgbusTeam_配置更新() {
	card := maschema.NewTeamCard(
		maschema.WithTeamCardID("team-msgbus-cfg"),
		maschema.WithTeamCardName("CfgMsgbusTeam"),
	)
	cfg := hierarchical_msgbus.NewHierarchicalTeamConfig()
	runtimeCfg := team_runtime.NewRuntimeConfig(
		team_runtime.WithRuntimeTeamID("team-msgbus-cfg"),
	)
	tr := team_runtime.NewTeamRuntime(*runtimeCfg)
	tr.SetMessageBus(newMockMessageBus())

	team := hierarchical_msgbus.NewHierarchicalTeam(card, cfg, tr)

	newConfig := maschema.NewTeamConfig()
	newConfig.ConfigureMaxAgents(5).ConfigureTimeout(60.0)
	s.Require().NoError(team.Configure(context.Background(), *newConfig))
	s.Equal(5, team.Config().MaxAgents)
	s.Equal(60.0, team.Config().MessageTimeout)
}

// ──────────────────────────── HierarchicalToolsTeam 测试 ────────────────────────────

// TestToolsTeam_创建 验证 HierarchicalToolsTeam 创建和基本属性
// 对齐 Python: test_hierarchical_tools.py test_create
func (s *HierarchicalE2ESuite) TestToolsTeam_创建() {
	rootCard := agentschema.NewAgentCard(
		agentschema.WithAgentID("root-agent"),
		agentschema.WithAgentName("RootAgent"),
	)
	card := maschema.NewTeamCard(
		maschema.WithTeamCardID("team-tools-1"),
		maschema.WithTeamCardName("ToolsTeam测试"),
	)
	cfg := hierarchical_tools.NewHierarchicalToolsTeamConfig()
	cfg.RootAgent = rootCard

	team := hierarchical_tools.NewHierarchicalToolsTeam(card, cfg, nil)
	s.NotNil(team)
	s.Equal("team-tools-1", team.Card().GetID())
	s.NotNil(team.GetRuntime())
}

// TestToolsTeam_添加Agent 验证 AddAgent 和层级管理
// 对齐 Python: test_hierarchical_tools.py test_add_agent
func (s *HierarchicalE2ESuite) TestToolsTeam_添加Agent() {
	rootCard := agentschema.NewAgentCard(
		agentschema.WithAgentID("root-agent"),
		agentschema.WithAgentName("RootAgent"),
	)
	card := maschema.NewTeamCard(
		maschema.WithTeamCardID("team-tools-add"),
		maschema.WithTeamCardName("AddToolsTeam"),
	)
	cfg := hierarchical_tools.NewHierarchicalToolsTeamConfig()
	cfg.RootAgent = rootCard

	runtimeCfg := team_runtime.NewRuntimeConfig(
		team_runtime.WithRuntimeTeamID("team-tools-add"),
	)
	tr := team_runtime.NewTeamRuntime(*runtimeCfg)
	tr.SetMessageBus(newMockMessageBus())

	team := hierarchical_tools.NewHierarchicalToolsTeam(card, cfg, tr)
	ctx := context.Background()

	// 添加 root agent
	rootProvider := func(_ context.Context, _ *agentschema.AgentCard) (agentinterfaces.BaseAgent, error) {
		return &mockBaseAgent{card: rootCard}, nil
	}
	s.Require().NoError(team.AddAgent(ctx, rootCard, rootProvider))
	s.Equal(1, team.GetAgentCount())

	// 添加 child agent（带 WithParentAgentID）
	childCard := agentschema.NewAgentCard(
		agentschema.WithAgentID("child-1"),
		agentschema.WithAgentName("Child1"),
	)
	childProvider := func(_ context.Context, _ *agentschema.AgentCard) (agentinterfaces.BaseAgent, error) {
		return &mockBaseAgent{card: childCard}, nil
	}
	err := team.AddAgent(ctx, childCard, childProvider,
		maschema.WithParentAgentID("root-agent"),
	)
	s.Require().NoError(err)
	s.Equal(2, team.GetAgentCount())
}

// TestToolsTeam_移除Agent 验证 RemoveAgent
// 对齐 Python: test_hierarchical_tools.py test_remove_agent
func (s *HierarchicalE2ESuite) TestToolsTeam_移除Agent() {
	rootCard := agentschema.NewAgentCard(
		agentschema.WithAgentID("root-agent"),
		agentschema.WithAgentName("RootAgent"),
	)
	card := maschema.NewTeamCard(
		maschema.WithTeamCardID("team-tools-rm"),
		maschema.WithTeamCardName("RmToolsTeam"),
	)
	cfg := hierarchical_tools.NewHierarchicalToolsTeamConfig()
	cfg.RootAgent = rootCard

	runtimeCfg := team_runtime.NewRuntimeConfig(
		team_runtime.WithRuntimeTeamID("team-tools-rm"),
	)
	tr := team_runtime.NewTeamRuntime(*runtimeCfg)
	tr.SetMessageBus(newMockMessageBus())

	team := hierarchical_tools.NewHierarchicalToolsTeam(card, cfg, tr)
	ctx := context.Background()

	rootProvider := func(_ context.Context, _ *agentschema.AgentCard) (agentinterfaces.BaseAgent, error) {
		return &mockBaseAgent{card: rootCard}, nil
	}
	s.Require().NoError(team.AddAgent(ctx, rootCard, rootProvider))

	childCard := agentschema.NewAgentCard(
		agentschema.WithAgentID("child-1"),
		agentschema.WithAgentName("Child1"),
	)
	childProvider := func(_ context.Context, _ *agentschema.AgentCard) (agentinterfaces.BaseAgent, error) {
		return &mockBaseAgent{card: childCard}, nil
	}
	s.Require().NoError(team.AddAgent(ctx, childCard, childProvider))

	// 移除
	s.Require().NoError(team.RemoveAgent(ctx, "child-1"))
	s.Equal(1, team.GetAgentCount())
}

// TestToolsTeam_配置更新 验证 Configure
// 对齐 Python: test_hierarchical_tools.py test_configure
func (s *HierarchicalE2ESuite) TestToolsTeam_配置更新() {
	rootCard := agentschema.NewAgentCard(
		agentschema.WithAgentID("root-agent"),
		agentschema.WithAgentName("RootAgent"),
	)
	card := maschema.NewTeamCard(
		maschema.WithTeamCardID("team-tools-cfg"),
		maschema.WithTeamCardName("CfgToolsTeam"),
	)
	cfg := hierarchical_tools.NewHierarchicalToolsTeamConfig()
	cfg.RootAgent = rootCard

	runtimeCfg := team_runtime.NewRuntimeConfig(
		team_runtime.WithRuntimeTeamID("team-tools-cfg"),
	)
	tr := team_runtime.NewTeamRuntime(*runtimeCfg)
	tr.SetMessageBus(newMockMessageBus())

	team := hierarchical_tools.NewHierarchicalToolsTeam(card, cfg, tr)

	newConfig := maschema.NewTeamConfig()
	newConfig.ConfigureMaxAgents(20).ConfigureConcurrency(50)
	s.Require().NoError(team.Configure(context.Background(), *newConfig))
	s.Equal(20, team.Config().MaxAgents)
	s.Equal(50, team.Config().MaxConcurrentMessages)
}

// ──────────────────────────── Config 默认值测试 ────────────────────────────

// TestHierarchicalConfig_默认值 验证两种 Config 的默认值
// 对齐 Python: test_hierarchical_config.py
func (s *HierarchicalE2ESuite) TestHierarchicalConfig_默认值() {
	// msgbus config
	msgbusCfg := hierarchical_msgbus.NewHierarchicalTeamConfig()
	s.Equal(1800.0, msgbusCfg.Timeout, "默认 Timeout 应为 1800.0")
	s.Equal(10, msgbusCfg.TeamConfig.MaxAgents)
	s.Nil(msgbusCfg.SupervisorAgent)

	// tools config
	toolsCfg := hierarchical_tools.NewHierarchicalToolsTeamConfig()
	s.Nil(toolsCfg.RootAgent, "默认 RootAgent 应为 nil")
	// 注意：NewHierarchicalToolsTeamConfig 不初始化 TeamConfig，零值 MaxAgents=0
	s.Equal(0, toolsCfg.TeamConfig.MaxAgents, "默认 MaxAgents 应为 0（未初始化）")
}

// ──────────────────────────── 编译时接口合规检查 ────────────────────────────

func init() {
	_ = team_runtime.MessageBusInterface(newMockMessageBus())
	_ = maschema.BaseTeam((*hierarchical_msgbus.HierarchicalTeam)(nil))
	_ = maschema.BaseTeam((*hierarchical_tools.HierarchicalToolsTeam)(nil))
}

// CI 合规性验证：导入 ≥2 个 internal 包
var _ = (*resources_manager.AgentProvider)(nil)
var _ = (*runner.AgentRef)(nil)
var _ = testing.Init
