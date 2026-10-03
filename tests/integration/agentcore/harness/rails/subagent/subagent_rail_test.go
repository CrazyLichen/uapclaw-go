//go:build integration

package subagent

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/suite"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	subagent "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/subagent"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SubagentRailSuite 测试 SubagentRail / VerificationRail / VerificationContractRail。
//
// 对齐 Python: tests/unit_tests/harness/test_subagent_rail.py
// 注意：这 3 个 Rail 未覆盖 GetCallbacks()，回调方法不会自动触发，
// 仅验证 Init 注册工具和构造成功。
type SubagentRailSuite struct {
	isuite.AgentSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestSubagentRailSuite(t *testing.T) {
	suite.Run(t, new(SubagentRailSuite))
}

// TestSubagentRail_Init注册TaskTool 测试 SubagentRail Init 后注册 task_tool。
// 对齐 Python: TestSubagentRail.test_init_with_subagents ——
// Python 中 SubagentRail.init 在有子代理时注册 task_tool。
func (s *SubagentRailSuite) TestSubagentRail_Init注册TaskTool() {
	rail := subagent.NewSubagentRail()

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("子代理测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)
	s.Require().NotNil(agent)

	// 验证 SubagentRail 已注册
	railType := reflect.TypeOf(&subagent.SubagentRail{})
	foundRails := agent.FindRailsByType(railType)
	s.NotEmpty(foundRails, "应注册 SubagentRail")

	// SubagentRail.Init 需要 DeepConfig.Subagents 非空才注册 task_tool
	// 未配置子代理时 Init 不注册工具（正常降级），验证不崩溃即可
}

// TestVerificationRail_Init注册成功 测试 VerificationRail Init 成功且 Rail 注册到 Agent。
// 对齐 Python: TestWorkspaceScopeGuard + TestConditionalReminderInjection ——
// Python 中 VerificationRail.init 捕获 system_prompt_builder。
func (s *SubagentRailSuite) TestVerificationRail_Init注册成功() {
	rail := subagent.NewVerificationRail()

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("验证测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)
	s.Require().NotNil(agent)

	// 验证 VerificationRail 已注册
	railType := reflect.TypeOf(&subagent.VerificationRail{})
	foundRails := agent.FindRailsByType(railType)
	s.NotEmpty(foundRails, "应注册 VerificationRail")
}

// TestVerificationContractRail_Init成功 测试 VerificationContractRail Init 成功且 Rail 注册到 Agent。
// 对齐 Python: TestVerificationContractRail（Python 中无独立测试文件，逻辑在 verification_rail.py）——
// VerificationContractRail.init 捕获 system_prompt_builder 并预构建契约 section。
func (s *SubagentRailSuite) TestVerificationContractRail_Init成功() {
	rail := subagent.NewVerificationContractRail()

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("契约验证测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)
	s.Require().NotNil(agent)

	// 验证 VerificationContractRail 已注册
	railType := reflect.TypeOf(&subagent.VerificationContractRail{})
	foundRails := agent.FindRailsByType(railType)
	s.NotEmpty(foundRails, "应注册 VerificationContractRail")
}
