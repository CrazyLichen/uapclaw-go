//go:build integration

package subagent

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/suite"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	hsections "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/sections"
	subagent "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/subagent"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SubagentRailSuite 测试 SubagentRail / VerificationRail / VerificationContractRail。
//
// 覆盖：
//   - SubagentRail Init 注册
//   - VerificationRail BeforeModelCall 注入约束提醒节
//   - VerificationContractRail BeforeModelCall 注入验证契约节
//
// 对齐 Python: tests/unit_tests/harness/test_subagent_rail.py
type SubagentRailSuite struct {
	isuite.AgentSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestSubagentRailSuite(t *testing.T) {
	suite.Run(t, new(SubagentRailSuite))
}

// TestSubagentRail_Init注册 测试 SubagentRail Init 注册到 Agent。
// 对齐 Python: SubagentRail.init() 注册 task_tool（需要 subagents 配置）
func (s *SubagentRailSuite) TestSubagentRail_Init注册() {
	rail := subagent.NewSubagentRail()

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("子代理测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)
	s.Require().NotNil(agent)

	railType := reflect.TypeOf(&subagent.SubagentRail{})
	foundRails := agent.FindRailsByType(railType)
	s.NotEmpty(foundRails, "应注册 SubagentRail")
}

// TestVerificationRail_BeforeModelCall_注入约束提醒节 测试 BeforeModelCall 注入 verification_reminder section。
// 对齐 Python: VerificationRail.before_model_call() 中约束提醒注入
func (s *SubagentRailSuite) TestVerificationRail_BeforeModelCall_注入约束提醒节() {
	rail := subagent.NewVerificationRail()

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("验证测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	_, err = agent.Invoke(s.Ctx, map[string]any{"query": "验证代理"})
	s.Require().NoError(err)

	// 验证 verification_reminder 节已注入
	spb := agent.SystemPromptBuilder()
	s.Require().NotNil(spb)
	s.True(spb.HasSection("verification_reminder"), "应存在 verification_reminder 节")

	// 验证节内容包含 "VERDICT" 关键字
	section := spb.GetSection("verification_reminder")
	s.Require().NotNil(section)
	cnContent := section.Content["cn"]
	enContent := section.Content["en"]
	s.Contains(enContent, "VERDICT", "英文内容应包含 VERDICT 关键字")
	s.NotEmpty(cnContent, "中文内容不应为空")
}

// TestVerificationContractRail_BeforeModelCall_注入契约节 测试 BeforeModelCall 注入 SectionVerificationContract。
// 对齐 Python: VerificationContractRail.before_model_call() 中契约 section 注入
func (s *SubagentRailSuite) TestVerificationContractRail_BeforeModelCall_注入契约节() {
	rail := subagent.NewVerificationContractRail()

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("契约验证测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	_, err = agent.Invoke(s.Ctx, map[string]any{"query": "实现功能"})
	s.Require().NoError(err)

	// 验证 SectionVerificationContract 已注入
	spb := agent.SystemPromptBuilder()
	s.Require().NotNil(spb)
	s.True(spb.HasSection(hsections.SectionVerificationContract), "应存在 SectionVerificationContract 节")

	// 验证节内容包含 "Verification Gate" / "验证门控" 关键字
	section := spb.GetSection(hsections.SectionVerificationContract)
	s.Require().NotNil(section)
	enContent := section.Content["en"]
	cnContent := section.Content["cn"]
	s.Contains(enContent, "Verification Gate", "英文内容应包含 Verification Gate")
	s.Contains(cnContent, "验证门控", "中文内容应包含 验证门控")
}
