//go:build integration

package progressive

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/suite"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	hsections "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/sections"
	progressive "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ProgressiveToolRailSuite 测试 ProgressiveToolRail 渐进式工具。
//
// 覆盖：
//   - Init 注册 search_tools/load_tools 元工具
//   - BeforeModelCall 注入导航节和规则节
//   - Uninit 清理元工具
//
// 对齐 Python: tests/unit_tests/harness/test_progressive_tool_rail.py
type ProgressiveToolRailSuite struct {
	isuite.AgentSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestProgressiveToolRailSuite(t *testing.T) {
	suite.Run(t, new(ProgressiveToolRailSuite))
}

// TestProgressiveToolRail_Init注册元工具 测试 NewProgressiveToolRail + Init 注册 search_tools/load_tools。
// 对齐 Python: ProgressiveToolRail.init() 中元工具注册
func (s *ProgressiveToolRailSuite) TestProgressiveToolRail_Init注册元工具() {
	config := hschema.NewDeepAgentConfig()
	config.ProgressiveToolEnabled = true
	rail := progressive.NewProgressiveToolRail(config)

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("渐进式测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	_, err = agent.Invoke(s.Ctx, map[string]any{"query": "渐进式测试"})
	s.Require().NoError(err, "Invoke 不应返回错误")

	// 验证 ProgressiveToolRail 已注册
	railType := reflect.TypeOf(&progressive.ProgressiveToolRail{})
	foundRails := agent.FindRailsByType(railType)
	s.NotEmpty(foundRails, "应注册 ProgressiveToolRail")

	// 验证元工具注册
	am := agent.AbilityManager()
	s.Require().NotNil(am)
	s.NotNil(am.Get("search_tools"), "应注册 search_tools")
	s.NotNil(am.Get("load_tools"), "应注册 load_tools")
}

// TestProgressiveToolRail_BeforeModelCall_注入导航节和规则节 测试 BeforeModelCall 注入 SectionToolNavigation + SectionProgressiveToolRules。
// 对齐 Python: ProgressiveToolRail.before_model_call() 中 section 注入
func (s *ProgressiveToolRailSuite) TestProgressiveToolRail_BeforeModelCall_注入导航节和规则节() {
	config := hschema.NewDeepAgentConfig()
	config.ProgressiveToolEnabled = true
	rail := progressive.NewProgressiveToolRail(config)

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("导航节注入测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	_, err = agent.Invoke(s.Ctx, map[string]any{"query": "导航测试"})
	s.Require().NoError(err, "Invoke 不应返回错误")

	// 验证导航节和规则节已注入
	spb := agent.SystemPromptBuilder()
	s.Require().NotNil(spb)
	s.True(spb.HasSection(hsections.SectionToolNavigation), "应存在 SectionToolNavigation 节")
	s.True(spb.HasSection(hsections.SectionProgressiveToolRules), "应存在 SectionProgressiveToolRules 节")

	// 验证节内容非空
	navSection := spb.GetSection(hsections.SectionToolNavigation)
	s.Require().NotNil(navSection)
	s.NotEmpty(navSection.Content, "SectionToolNavigation 内容不应为空")

	rulesSection := spb.GetSection(hsections.SectionProgressiveToolRules)
	s.Require().NotNil(rulesSection)
	s.NotEmpty(rulesSection.Content, "SectionProgressiveToolRules 内容不应为空")
}

// TestProgressiveToolRail_Uninit清理 测试 Uninit 移除元工具。
func (s *ProgressiveToolRailSuite) TestProgressiveToolRail_Uninit清理() {
	config := hschema.NewDeepAgentConfig()
	config.ProgressiveToolEnabled = true
	rail := progressive.NewProgressiveToolRail(config)

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

	// 验证元工具已从 AM 注销
	am := agent.AbilityManager()
	s.Require().NotNil(am)
	s.Nil(am.Get("search_tools"), "Uninit 后应注销 search_tools")
	s.Nil(am.Get("load_tools"), "Uninit 后应注销 load_tools")
}
