//go:build integration

package runner_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/suite"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/resources_manager"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
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

// TestRunner_Agent资源管理_完整生命周期 端到端测试 Runner 级 Agent 资源管理：
// 注册 → 按 ID 运行（会话 PreRun/Invoke/PostRun 全链路）→ 移除 → 再运行失败。
//
// 对齐 Python: test_runner.py test_runner_agent_resource_management（@skip 状态）。
// 注：Python 原测试的交互中断恢复部分（Questioner __interaction__ → 二次调用恢复）
// 依赖 WorkflowAgent/IntentDetection/Questioner（计划 8.14-8.24 未移植，A 类缺口），
// 此处对齐其资源管理核心链路；交互恢复场景待工作流组件移植后补充。
func (s *RunnerLifeCycleSuite) TestRunner_Agent资源管理_完整生命周期() {
	rm := s.GetResourceMgr()
	s.Require().NotNil(rm)

	agentID := "itest_resource_agent"
	conversationID := "c124"

	// -------------------- Step 1: 添加智能体 --------------------
	// 对齐 Python: Runner.resource_mgr.add_agent(AgentCard(id=agent_id), agent=agent)
	agent := &stubBaseAgent{
		card: agentschema.NewAgentCard(
			agentschema.WithAgentID(agentID),
			agentschema.WithAgentName("资源管理测试Agent"),
		),
	}
	provider := resources_manager.AgentProvider(func(_ context.Context, _ *agentschema.AgentCard) (agentinterfaces.BaseAgent, error) {
		return agent, nil
	})
	err := rm.AddAgent(agent.card, provider)
	s.Require().NoError(err, "智能体添加应成功")

	// 二次添加同 ID 应报错（对齐 Python add_tool 重复语义）
	err = rm.AddAgent(agent.card, provider)
	s.Error(err, "重复添加同 ID 智能体应报错")

	// -------------------- Step 2: 通过 ID 运行智能体 --------------------
	// 对齐 Python: await Runner.run_agent(agent_id, {"query": ..., "conversation_id": ...})
	inputs := map[string]any{
		"query":           "查询天气",
		"conversation_id": conversationID,
	}
	result, err := runner.RunAgent(s.Ctx, runner.ByAgentID(agentID), inputs, runner.SessionRef{}, nil, nil)
	s.Require().NoError(err, "按 ID 运行智能体应成功")
	s.Equal(1, agent.invokeCount, "注册的 Agent 实例应被执行恰好一次")
	s.Contains(result, "output")
	s.Equal("stub", result["output"])

	// -------------------- Step 3: 移除智能体 --------------------
	// 对齐 Python: Runner.resource_mgr.remove_agent(id=agent_id)
	removedCards, err := rm.RemoveAgent([]string{agentID})
	s.Require().NoError(err, "移除智能体应成功")
	s.Require().Len(removedCards, 1)
	s.Equal(agentID, removedCards[0].ID)

	// -------------------- Step 4: 移除后再次运行应失败 --------------------
	// 对齐 Python: with self.assertRaises(Exception): await Runner.run_agent(...)
	_, err = runner.RunAgent(s.Ctx, runner.ByAgentID(agentID),
		map[string]any{"query": "查询天气", "conversation_id": conversationID},
		runner.SessionRef{}, nil, nil)
	s.Error(err, "移除后的智能体运行应失败")
}

// TestRunner_Agent资源管理_未注册ID运行失败 测试按未注册 ID 直接运行失败。
// 对齐 Python Step 5 语义的另一面：不存在的 agent ID 在 GetAgent 阶段即报错。
func (s *RunnerLifeCycleSuite) TestRunner_Agent资源管理_未注册ID运行失败() {
	_, err := runner.RunAgent(s.Ctx, runner.ByAgentID("never_registered_agent"),
		map[string]any{"query": "test"},
		runner.SessionRef{}, nil, nil)
	s.Error(err, "未注册 ID 运行应失败")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// stubBaseAgent BaseAgent 桩实现，用于 Runner 注册测试。
// 对齐 Python: 各测试中的 mockBaseAgent
type stubBaseAgent struct {
	// card 工具卡片
	card *agentschema.AgentCard
	// invokeCount Invoke 被调用次数（验证注册的 Agent 真正被执行）
	invokeCount int
}

func (s *stubBaseAgent) Configure(_ context.Context, _ agentinterfaces.AgentConfig) error {
	return nil
}
func (s *stubBaseAgent) Invoke(_ context.Context, _ map[string]any, _ ...agentinterfaces.AgentOption) (map[string]any, error) {
	s.invokeCount++
	return map[string]any{"output": "stub"}, nil
}
func (s *stubBaseAgent) Stream(_ context.Context, _ map[string]any, _ ...agentinterfaces.AgentOption) (<-chan stream.Schema, error) {
	return nil, nil
}
func (s *stubBaseAgent) Card() *agentschema.AgentCard        { return s.card }
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
