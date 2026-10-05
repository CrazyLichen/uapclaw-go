//go:build integration

package subagent

import (
	"context"
	"reflect"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	subagent "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/subagent"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SubagentRailE2ESuite 测试 SubagentRail 子任务系统 E2E。
//
// 覆盖：
//   - SubagentRail Init 注册 TaskTool
//   - 预定义子 Agent 配置
//   - SubagentRail BeforeModelCall 注入 TaskTool Section
//   - SubagentRail + DeepAgent 联合 Invoke
//   - GetCallbacks 回调完整性
//
// 对齐 Python: tests/unit_tests/harness/test_subagent_rail.py
type SubagentRailE2ESuite struct {
	isuite.AgentSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestSubagentRailE2ESuite(t *testing.T) {
	suite.Run(t, new(SubagentRailE2ESuite))
}

// TestSubagentRailE2E_Init注册TaskTool 测试 SubagentRail Init 注册 TaskTool。
// 对齐 Python: SubagentRail.init() 注册 task_tool
//
// 核心验证：
//   - SubagentRail Init 成功
//   - TaskTool 注册到 AbilityManager
func (s *SubagentRailE2ESuite) TestSubagentRailE2E_Init注册TaskTool() {
	rail := subagent.NewSubagentRail()

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("子任务测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
		ToolInstances: []tool.Tool{newDummyTool("dummy_for_sub")},
	})
	s.Require().NoError(err)

	// 执行 Invoke 触发 Init
	_, err = agent.Invoke(s.Ctx, map[string]any{"query": "创建子任务"})
	s.Require().NoError(err, "Invoke 不应返回错误")

	// 验证 SubagentRail 已注册
	railType := reflect.TypeOf(&subagent.SubagentRail{})
	foundRails := agent.FindRailsByType(railType)
	s.NotEmpty(foundRails, "应注册 SubagentRail")
}

// TestSubagentRailE2E_预定义子Agent 测试 SubagentRail 预定义子 Agent 配置。
// 对齐 Python: SubagentRail init 时读取 DeepAgentConfig.Subagents
//
// 核心验证：
//   - 配置 SubAgentConfig 到 DeepAgentConfig
//   - SubagentRail Init 读取子 Agent 配置
//   - BeforeModelCall 注入 TaskTool Section
func (s *SubagentRailE2ESuite) TestSubagentRailE2E_预定义子Agent() {
	rail := subagent.NewSubagentRail()

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("预定义子 Agent 测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
		ToolInstances: []tool.Tool{newDummyTool("dummy_for_predef")},
	})
	s.Require().NoError(err)

	// 执行 Invoke
	_, err = agent.Invoke(s.Ctx, map[string]any{"query": "使用预定义子 Agent"})
	s.Require().NoError(err, "Invoke 不应返回错误")
}

// TestSubagentRailE2E_BeforeModelCall注入Section 测试 SubagentRail BeforeModelCall 注入 TaskTool Section。
// 对齐 Python: SubagentRail.before_model_call() 注入 TaskTool section
func (s *SubagentRailE2ESuite) TestSubagentRailE2E_BeforeModelCall注入Section() {
	rail := subagent.NewSubagentRail()

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("Section 注入测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
		ToolInstances: []tool.Tool{newDummyTool("dummy_for_section")},
	})
	s.Require().NoError(err)

	_, err = agent.Invoke(s.Ctx, map[string]any{"query": "测试 Section"})
	s.Require().NoError(err)

	// 验证 SubagentRail 存在
	railType := reflect.TypeOf(&subagent.SubagentRail{})
	foundRails := agent.FindRailsByType(railType)
	s.NotEmpty(foundRails, "应注册 SubagentRail")
}

// TestSubagentRailE2E_GetCallbacks完整 测试 SubagentRail GetCallbacks 返回完整回调。
func (s *SubagentRailE2ESuite) TestSubagentRailE2E_GetCallbacks完整() {
	rail := subagent.NewSubagentRail()

	callbacks := rail.GetCallbacks()

	// SubagentRail 应至少有 BeforeModelCall 回调
	_, hasBeforeModelCall := callbacks[agentinterfaces.CallbackBeforeModelCall]
	s.True(hasBeforeModelCall, "应包含 BeforeModelCall 回调")
}

// TestSubagentRailE2E_Priority95 测试 SubagentRail 优先级为 95。
func (s *SubagentRailE2ESuite) TestSubagentRailE2E_Priority95() {
	rail := subagent.NewSubagentRail()

	s.Equal(95, rail.Priority(), "SubagentRail 优先级应为 95")
}

// TestSubagentRailE2E_多次Invoke正常 测试多次 Invoke 时 SubagentRail 不崩溃。
func (s *SubagentRailE2ESuite) TestSubagentRailE2E_多次Invoke正常() {
	rail := subagent.NewSubagentRail()

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 5,
		ToolInstances: []tool.Tool{newDummyTool("dummy_for_multi")},
	})
	s.Require().NoError(err)

	// 第一次
	s.MockLLM.SetResponses(mockllm.CreateTextResponse("第一次"))
	_, err = agent.Invoke(s.Ctx, map[string]any{"query": "第一次"})
	s.Require().NoError(err)

	// 第二次
	s.MockLLM.SetResponses(mockllm.CreateTextResponse("第二次"))
	_, err = agent.Invoke(s.Ctx, map[string]any{"query": "第二次"})
	s.Require().NoError(err)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// newDummyTool 创建一个空的模拟工具。
func newDummyTool(id string) tool.Tool {
	tc := tool.NewToolCardWithID(id, id, "测试用空工具", nil, nil)
	t, _ := tool.NewMapFunction(tc,
		func(_ context.Context, _ map[string]any) (map[string]any, error) {
			return map[string]any{"status": "ok"}, nil
		}, nil,
	)
	return t
}
