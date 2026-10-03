//go:build integration

package mcp

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/suite"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	mcprail "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// McpRailSuite 测试 McpRail MCP 资源浏览。
//
// 对齐 Python: tests/unit_tests/harness/rails/test_mcp_rail.py
// 注意：McpRail 未覆盖 GetCallbacks()，回调方法不会自动触发，
// 仅验证 Init 注册工具和 Uninit 清理。
type McpRailSuite struct {
	isuite.AgentSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestMcpRailSuite(t *testing.T) {
	suite.Run(t, new(McpRailSuite))
}

// TestMcpRail_Init注册McpTools 测试 McpRail Init 后注册 MCP 资源浏览工具。
// 对齐 Python: TestMcpRailInit.test_adds_both_cards_to_ability_manager ——
// Python 中 McpRail.init 注册 list_mcp_resources + read_mcp_resource 到 AbilityManager。
// 注意：需先 Invoke 触发 ensureInitialized。
func (s *McpRailSuite) TestMcpRail_Init注册McpTools() {
	rail := mcprail.NewMcpRail()

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("MCP 测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	// 先 Invoke 触发 ensureInitialized → Rail Init
	_, err = agent.Invoke(s.Ctx, map[string]any{"query": "MCP 测试"})
	s.Require().NoError(err, "Invoke 不应返回错误")

	// 验证 McpRail 已注册
	railType := reflect.TypeOf(&mcprail.McpRail{})
	foundRails := agent.FindRailsByType(railType)
	s.NotEmpty(foundRails, "应注册 McpRail")

	// 验证 MCP 工具注册
	am := agent.AbilityManager()
	s.Require().NotNil(am)
	s.NotNil(am.Get("list_mcp_resources"), "应注册 list_mcp_resources")
	s.NotNil(am.Get("read_mcp_resource"), "应注册 read_mcp_resource")
}

// TestMcpRail_无配置时不崩溃 测试无 MCP 服务端配置时 Init 不崩溃。
// 对齐 Python: TestMcpRailInit.test_tools_constructed_with_agent_language_and_id ——
// Python 中 McpRail.init 不依赖实际 MCP 服务端配置。
func (s *McpRailSuite) TestMcpRail_无配置时不崩溃() {
	rail := mcprail.NewMcpRail()

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("无配置测试"))

	// 不传 Mcps 配置
	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	// 先 Invoke 触发 ensureInitialized
	_, err = agent.Invoke(s.Ctx, map[string]any{"query": "无配置测试"})
	s.Require().NoError(err, "无 MCP 配置时 Invoke 不应崩溃")

	// 工具仍注册（浏览工具不依赖实际 MCP 服务端）
	am := agent.AbilityManager()
	s.Require().NotNil(am)
	s.NotNil(am.Get("list_mcp_resources"), "无配置时仍应注册 list_mcp_resources")
}

// TestMcpRail_Uninit清理 测试 Uninit 后工具从 AbilityManager 移除。
// 对齐 Python: TestMcpRailUninit.test_removes_tool_names_from_ability_manager ——
// Python 中 McpRail.uninit 从 AbilityManager 移除已注册工具。
func (s *McpRailSuite) TestMcpRail_Uninit清理() {
	rail := mcprail.NewMcpRail()

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("清理测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	// 执行一次 Invoke 确保 Init 完成
	_, _ = agent.Invoke(s.Ctx, map[string]any{"query": "清理前"})

	// Uninit
	rail.Uninit(agent)

	// 验证工具已移除
	am := agent.AbilityManager()
	s.Require().NotNil(am)
	s.Nil(am.Get("list_mcp_resources"), "Uninit 后 list_mcp_resources 应移除")
	s.Nil(am.Get("read_mcp_resource"), "Uninit 后 read_mcp_resource 应移除")
}
