//go:build integration

package agent_mode

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/suite"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	hsections "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/sections"
	agentmode "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AgentModeRailSuite 测试 AgentModeRail 代理模式切换。
//
// 覆盖：
//   - Init 注册 3 个模式切换工具
//   - BeforeModelCall 正常模式无 ModeInstructions
//   - 默认模式为 Normal
//   - Uninit 移除工具
//
// 对齐 Python: tests/unit_tests/harness/rails/test_agent_mode_rail.py
type AgentModeRailSuite struct {
	isuite.AgentSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestAgentModeRailSuite(t *testing.T) {
	suite.Run(t, new(AgentModeRailSuite))
}

// TestAgentModeRail_Init注册3个工具 测试 AgentModeRail Init 后注册 switch_mode/enter_plan_mode/exit_plan_mode。
// 对齐 Python: AgentModeRail.init() 中工具注册
func (s *AgentModeRailSuite) TestAgentModeRail_Init注册3个工具() {
	rail := agentmode.NewAgentModeRail(nil)

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("模式切换测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	_, err = agent.Invoke(s.Ctx, map[string]any{"query": "模式测试"})
	s.Require().NoError(err, "Invoke 不应返回错误")

	// 验证 AgentModeRail 已注册
	railType := reflect.TypeOf(&agentmode.AgentModeRail{})
	foundRails := agent.FindRailsByType(railType)
	s.NotEmpty(foundRails, "应注册 AgentModeRail")

	// 验证 3 个模式工具注册
	am := agent.AbilityManager()
	s.Require().NotNil(am)
	s.NotNil(am.Get("switch_mode"), "应注册 switch_mode")
	s.NotNil(am.Get("enter_plan_mode"), "应注册 enter_plan_mode")
	s.NotNil(am.Get("exit_plan_mode"), "应注册 exit_plan_mode")
}

// TestAgentModeRail_BeforeModelCall_正常模式无ModeInstructions 测试正常模式下不注入 ModeInstructions 节。
// 对齐 Python: AgentModeRail.before_model_call() 正常模式移除 SectionModeInstructions
func (s *AgentModeRailSuite) TestAgentModeRail_BeforeModelCall_正常模式无ModeInstructions() {
	rail := agentmode.NewAgentModeRail(nil)

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("模式提示词测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	_, err = agent.Invoke(s.Ctx, map[string]any{"query": "模式测试"})
	s.Require().NoError(err, "Invoke 不应返回错误")

	// 正常模式不应有 ModeInstructions 节
	spb := agent.SystemPromptBuilder()
	s.Require().NotNil(spb)
	s.False(spb.HasSection(hsections.SectionModeInstructions), "正常模式不应有 ModeInstructions 节")
}

// TestAgentModeRail_默认模式为Normal 测试默认模式为 "normal"。
func (s *AgentModeRailSuite) TestAgentModeRail_默认模式为Normal() {
	rail := agentmode.NewAgentModeRail(nil)

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("默认模式测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	cfg := agent.DeepConfig()
	s.Require().NotNil(cfg, "DeepConfig 不应为 nil")
	s.Equal(hschema.AgentModeNormal, cfg.DefaultMode, "默认模式应为 Normal")
}

// TestAgentModeRail_Uninit移除工具 测试 Uninit 移除 3 个模式工具。
func (s *AgentModeRailSuite) TestAgentModeRail_Uninit移除工具() {
	rail := agentmode.NewAgentModeRail(nil)

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("Uninit 测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	_, _ = agent.Invoke(s.Ctx, map[string]any{"query": "测试"})

	// Uninit
	s.NotPanics(func() {
		rail.Uninit(agent)
	}, "Uninit 不应 panic")

	// 验证 3 个工具已从 AM 注销
	am := agent.AbilityManager()
	s.Require().NotNil(am)
	s.Nil(am.Get("switch_mode"), "Uninit 后应注销 switch_mode")
	s.Nil(am.Get("enter_plan_mode"), "Uninit 后应注销 enter_plan_mode")
	s.Nil(am.Get("exit_plan_mode"), "Uninit 后应注销 exit_plan_mode")
}
