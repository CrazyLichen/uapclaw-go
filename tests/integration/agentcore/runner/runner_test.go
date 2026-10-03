//go:build integration

package runner_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/suite"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/resources_manager"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// RunnerLifeCycleSuite 测试 Runner 启停 + 资源注册。
// 嵌入 RunnerSuite，自动获得 Runner 启停 + MockLLM 注册。
// 对齐 Python: tests/system_tests/runner/test_runner.py
type RunnerLifeCycleSuite struct {
	isuite.RunnerSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestRunnerLifeCycleSuite(t *testing.T) {
	suite.Run(t, new(RunnerLifeCycleSuite))
}

// TestRunner_StartStop_无报错 测试 Runner 启动和停止无报错。
// 对齐 Python: await Runner.start() / await Runner.stop()
func (s *RunnerLifeCycleSuite) TestRunner_StartStop_无报错() {
	// RunnerSuite.SetupSuite 已调用 runner.Start()
	// 验证 ResourceMgr 可用即可
	s.NotNil(s.GetResourceMgr())
}

// TestRunner_GetResourceMgr_非空 测试 GetResourceMgr 返回非 nil。
// 对齐 Python: Runner.resource_mgr
func (s *RunnerLifeCycleSuite) TestRunner_GetResourceMgr_非空() {
	rm := s.GetResourceMgr()
	s.Require().NotNil(rm)
}

// TestRunner_注册AgentCard_可获取 测试向 ResourceMgr 注册 AgentCard 后可获取。
// 对齐 Python: Runner.resource_mgr.add_agent(card, provider)
func (s *RunnerLifeCycleSuite) TestRunner_注册AgentCard_可获取() {
	rm := s.GetResourceMgr()
	card := agentschema.NewAgentCard(
		agentschema.WithAgentID("itest_agent_1"),
		agentschema.WithAgentName("测试Agent"),
	)

	// 创建 stub AgentProvider
	provider := resources_manager.AgentProvider(func(_ context.Context, _ *agentschema.AgentCard) (agentinterfaces.BaseAgent, error) {
		return &stubBaseAgent{card: card}, nil
	})

	// 注册
	err := rm.AddAgent(card, provider)
	s.Require().NoError(err)

	// 获取
	agents, err := rm.GetAgent(s.Ctx, []string{"itest_agent_1"})
	s.Require().NoError(err)
	s.Len(agents, 1)
	s.Equal("测试Agent", agents[0].Card().Name)
}

// TestRunner_注册Tool_可获取 测试向 ResourceMgr 注册 Tool 后可获取。
// 对齐 Python: Runner.resource_mgr.add_tool(tool)
func (s *RunnerLifeCycleSuite) TestRunner_注册Tool_可获取() {
	rm := s.GetResourceMgr()

	// 创建简单的 MapFunction 工具
	tc := tool.NewToolCardWithID("itest_tool_1", "itest_tool", "测试工具", nil, nil)
	mockTool, err := tool.NewMapFunction(tc,
		func(_ context.Context, _ map[string]any) (map[string]any, error) {
			return map[string]any{"result": "ok"}, nil
		}, nil,
	)
	s.Require().NoError(err)

	// 注册
	err = rm.AddTool(mockTool, resources_manager.WithTag(resources_manager.TagGlobal))
	s.Require().NoError(err)

	// 获取
	tools, err := rm.GetTool([]string{"itest_tool_1"})
	s.Require().NoError(err)
	s.Len(tools, 1)
	s.Equal("itest_tool", tools[0].Card().Name)
}

// TestRunner_注销Tool_不可获取 测试注销 Tool 后不可获取。
// 对齐 Python: Runner.resource_mgr.remove_tool(tool_id)
func (s *RunnerLifeCycleSuite) TestRunner_注销Tool_不可获取() {
	rm := s.GetResourceMgr()

	// 注册
	tc := tool.NewToolCardWithID("itest_tool_remove", "itest_tool_r", "待注销工具", nil, nil)
	mockTool, err := tool.NewMapFunction(tc,
		func(_ context.Context, _ map[string]any) (map[string]any, error) {
			return map[string]any{"result": "ok"}, nil
		}, nil,
	)
	s.Require().NoError(err)
	err = rm.AddTool(mockTool, resources_manager.WithTag(resources_manager.TagGlobal))
	s.Require().NoError(err)

	// 注销
	removed, err := rm.RemoveTool([]string{"itest_tool_remove"})
	s.Require().NoError(err)
	s.Contains(removed, "itest_tool_remove")

	// 获取应返回空
	tools, err := rm.GetTool([]string{"itest_tool_remove"})
	s.Require().NoError(err)
	s.Empty(tools)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// stubBaseAgent BaseAgent 桩实现，用于 Runner 注册测试。
// 对齐 Python: 各测试中的 mockBaseAgent
type stubBaseAgent struct {
	card *agentschema.AgentCard
}

func (s *stubBaseAgent) Configure(_ context.Context, _ agentinterfaces.AgentConfig) error {
	return nil
}
func (s *stubBaseAgent) Invoke(_ context.Context, _ map[string]any, _ ...agentinterfaces.AgentOption) (map[string]any, error) {
	return map[string]any{"output": "stub"}, nil
}
func (s *stubBaseAgent) Stream(_ context.Context, _ map[string]any, _ ...agentinterfaces.AgentOption) (<-chan stream.Schema, error) {
	return nil, nil
}
func (s *stubBaseAgent) Card() *agentschema.AgentCard      { return s.card }
func (s *stubBaseAgent) Config() agentinterfaces.AgentConfig { return nil }
func (s *stubBaseAgent) AbilityManager() agentinterfaces.AbilityManagerInterface {
	return nil
}
func (s *stubBaseAgent) CallbackManager() *agentinterfaces.AgentCallbackManager {
	return nil
}
func (s *stubBaseAgent) SystemPromptBuilder() saprompt.SystemPromptBuilderInterface {
	return nil
}
func (s *stubBaseAgent) RegisterCallback(_ context.Context, _ agentinterfaces.AgentCallbackEvent, _ callback.PerAgentCallbackFunc, _ ...callback.CallbackOption) error {
	return nil
}
func (s *stubBaseAgent) RegisterRail(_ context.Context, _ agentinterfaces.AgentRail, _ ...callback.CallbackOption) error {
	return nil
}
func (s *stubBaseAgent) UnregisterRail(_ context.Context, _ agentinterfaces.AgentRail) error {
	return nil
}
