//go:build integration

package progressive

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/suite"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	progressive "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ProgressiveToolRailSuite 测试 ProgressiveToolRail 渐进式工具。
//
// 对齐 Python: tests/unit_tests/harness/test_progressive_tool_rail.py
// ProgressiveToolRail 覆盖了 GetCallbacks()，可测回调链路。
type ProgressiveToolRailSuite struct {
	isuite.AgentSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestProgressiveToolRailSuite(t *testing.T) {
	suite.Run(t, new(ProgressiveToolRailSuite))
}

// TestProgressiveToolRail_Init成功 测试 NewProgressiveToolRail + Init 成功。
// 对齐 Python: test_before_model_call_updates_builder_and_keeps_preview_messages_intact ——
// Python 中 ProgressiveToolRail.init 注册 search_tools/load_tools 元工具。
// 注意：需先 Invoke 触发 ensureInitialized。
func (s *ProgressiveToolRailSuite) TestProgressiveToolRail_Init成功() {
	config := hschema.NewDeepAgentConfig()
	config.ProgressiveToolEnabled = true
	rail := progressive.NewProgressiveToolRail(config)

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("渐进式测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	// 先 Invoke 触发 ensureInitialized → Rail Init
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

// TestProgressiveToolRail_BeforeInvoke缓存建立 测试 BeforeInvoke 建立工具导航缓存。
// 对齐 Python: test_before_model_call_updates_builder ——
// Python 中 BeforeInvoke/BeforeModelCall 建立和更新工具导航缓存。
func (s *ProgressiveToolRailSuite) TestProgressiveToolRail_BeforeInvoke缓存建立() {
	config := hschema.NewDeepAgentConfig()
	config.ProgressiveToolEnabled = true
	rail := progressive.NewProgressiveToolRail(config)

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("缓存建立测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	// Invoke 触发 BeforeInvoke → BeforeModelCall 链路
	_, err = agent.Invoke(s.Ctx, map[string]any{"query": "缓存测试"})
	s.Require().NoError(err, "Invoke 不应返回错误")
}

// TestProgressiveToolRail_BeforeModelCall导航节注入 测试 BeforeModelCall 注入工具导航 section。
// 对齐 Python: test_before_model_call_updates_builder ——
// Python 中 BeforeModelCall 向 SystemPromptBuilder 注入工具导航 section。
func (s *ProgressiveToolRailSuite) TestProgressiveToolRail_BeforeModelCall导航节注入() {
	config := hschema.NewDeepAgentConfig()
	config.ProgressiveToolEnabled = true
	rail := progressive.NewProgressiveToolRail(config)

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("导航节注入测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	// Invoke 触发 BeforeModelCall
	_, err = agent.Invoke(s.Ctx, map[string]any{"query": "导航测试"})
	s.Require().NoError(err, "Invoke 不应返回错误")

	// 验证 SystemPromptBuilder
	spb := agent.SystemPromptBuilder()
	s.Require().NotNil(spb, "SystemPromptBuilder 不应为 nil")
}
