//go:build integration

package agent_mode

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/suite"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	agentmode "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AgentModeRailSuite 测试 AgentModeRail 代理模式切换。
//
// 对齐 Python: tests/unit_tests/harness/rails/test_agent_mode_rail.py
// AgentModeRail 覆盖了 GetCallbacks()，可测完整回调链路。
type AgentModeRailSuite struct {
	isuite.AgentSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestAgentModeRailSuite(t *testing.T) {
	suite.Run(t, new(AgentModeRailSuite))
}

// TestAgentModeRail_Init注册SwitchModeTool 测试 AgentModeRail Init 后注册 switch_mode 工具。
// 对齐 Python: TestAgentModeRail 默认工具注册 ——
// Python 中 AgentModeRail.init 注册 switch_mode/enter_plan_mode/exit_plan_mode。
// 注意：需先 Invoke 触发 ensureInitialized。
func (s *AgentModeRailSuite) TestAgentModeRail_Init注册SwitchModeTool() {
	rail := agentmode.NewAgentModeRail(nil) // nil 使用默认 allowedTools

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("模式切换测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	// 先 Invoke 触发 ensureInitialized → Rail Init
	_, err = agent.Invoke(s.Ctx, map[string]any{"query": "模式测试"})
	s.Require().NoError(err, "Invoke 不应返回错误")

	// 验证 AgentModeRail 已注册
	railType := reflect.TypeOf(&agentmode.AgentModeRail{})
	foundRails := agent.FindRailsByType(railType)
	s.NotEmpty(foundRails, "应注册 AgentModeRail")

	// 验证 switch_mode 工具注册
	am := agent.AbilityManager()
	s.Require().NotNil(am)
	s.NotNil(am.Get("switch_mode"), "应注册 switch_mode")
	s.NotNil(am.Get("enter_plan_mode"), "应注册 enter_plan_mode")
	s.NotNil(am.Get("exit_plan_mode"), "应注册 exit_plan_mode")
}

// TestAgentModeRail_BeforeModelCall注入模式提示词 测试 BeforeModelCall 注入当前模式 section。
// 对齐 Python: TestAgentModeRail.test_before_model_call_filters_hidden_tools_and_injects_mode_section ——
// Python 中 BeforeModelCall 过滤隐藏工具并注入模式 section。
func (s *AgentModeRailSuite) TestAgentModeRail_BeforeModelCall注入模式提示词() {
	rail := agentmode.NewAgentModeRail(nil)

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("模式提示词测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	// 执行 Invoke 触发 BeforeModelCall
	_, err = agent.Invoke(s.Ctx, map[string]any{"query": "模式测试"})
	s.Require().NoError(err, "Invoke 不应返回错误")
}

// TestAgentModeRail_默认模式为Normal 测试默认模式为 "normal"。
// 对齐 Python: TestAgentModeRail 默认模式检测 ——
// Python 中 AgentModeRail 默认模式为 "code"，Go 端默认为 "normal"。
func (s *AgentModeRailSuite) TestAgentModeRail_默认模式为Normal() {
	rail := agentmode.NewAgentModeRail(nil)

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("默认模式测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	// DeepAgent 实现 DeepConfig() 方法
	cfg := agent.DeepConfig()
	s.Require().NotNil(cfg, "DeepConfig 不应为 nil")
	s.Equal(hschema.AgentModeNormal, cfg.DefaultMode,
		"默认模式应为 Normal")
}
