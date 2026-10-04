//go:build integration

package harness

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/suite"
	"reflect"

	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// CodeAgentE2ESuite 测试 CodeAgent 端到端流程。
//
// 对齐 Python: tests/system_tests/code_agent/test_code_agent_e2e.py
// 使用 MockLLM 模拟 todo_create/list/modify 工具调用 + TaskPlanningRail。
type CodeAgentE2ESuite struct {
	isuite.AgentSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestCodeAgentE2ESuite(t *testing.T) {
	suite.Run(t, new(CodeAgentE2ESuite))
}

// TestCodeAgent_正常E2E 测试 CodeAgent + TaskPlanningRail 的正常流程。
// 对齐 Python: test_code_agent_normal_e2e
//
// MockLLM 响应序列：
//  1. todo_create（创建 4 个任务）
//  2. todo_list（列出任务）
//  3. todo_modify（标记第一个任务完成）
//  4. 文本回答（"I have completed..."）
func (s *CodeAgentE2ESuite) TestCodeAgent_正常E2E() {
	tpr := rails.NewTaskPlanningRail()

	// MockLLM 模拟 CodeAgent 的典型行为序列
	s.MockLLM.SetResponses(
		// 第 1 轮：创建 4 个待办任务
		mockllm.CreateToolCallResponse("todo_create", `{"goal":"模块开发","tasks":["设计模块架构","实现核心功能","编写单元测试","集成测试"]}`),
		// 第 2 轮：列出任务确认
		mockllm.CreateToolCallResponse("todo_list", `{}`),
		// 第 3 轮：标记第一个任务完成
		mockllm.CreateToolCallResponse("todo_modify", `{"task_id":"1","status":"completed"}`),
		// 第 4 轮：最终文本回答
		mockllm.CreateTextResponse("I have completed the module architecture design and core functionality development plan."),
	)

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:              []agentinterfaces.AgentRail{tpr},
		EnableTaskPlanning: true,
		MaxIterations:      20,
	})
	s.Require().NoError(err)
	s.Require().NotNil(agent)

	sess := s.NewTestSession("code-agent-e2e")
	result, err := agent.Invoke(s.Ctx, map[string]any{
		"query":   "帮我开发一个简单的模块",
		"session": sess,
	})
	s.Require().NoError(err)
	s.Require().NotNil(result)

	// 验证结果为 answer 类型
	resultType, _ := result["result_type"].(string)
	s.Equal("answer", resultType, "结果类型应为 answer")

	// 验证输出包含关键内容
	output, _ := result["output"].(string)
	s.True(
		strings.Contains(strings.ToLower(output), "completed") || strings.Contains(output, "完成"),
		"输出应包含完成信息，实际: %s", output,
	)
}

// TestCodeAgent_TaskPlanningRail自动注册 测试 EnableTaskPlanning=true 时 TaskPlanningRail 自动注册。
// 对齐 Python: create_code_agent 默认包含 TaskPlanningRail
func (s *CodeAgentE2ESuite) TestCodeAgent_TaskPlanningRail自动注册() {
	s.MockLLM.SetResponses(mockllm.CreateTextResponse("测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		EnableTaskPlanning: true,
		MaxIterations:      3,
	})
	s.Require().NoError(err)
	s.Require().NotNil(agent)

	// 验证 TaskPlanningRail 已注册
	railType := reflect.TypeOf(&rails.TaskPlanningRail{})
	foundRails := agent.FindRailsByType(railType)
	s.NotEmpty(foundRails, "EnableTaskPlanning=true 时应自动注册 TaskPlanningRail")
}

// TestCodeAgent_任务创建与修改 测试 todo_create 和 todo_modify 工具的 E2E 交互。
func (s *CodeAgentE2ESuite) TestCodeAgent_任务创建与修改() {
	tpr := rails.NewTaskPlanningRail()

	s.MockLLM.SetResponses(
		// 创建任务
		mockllm.CreateToolCallResponse("todo_create", `{"goal":"重构代码","tasks":["提取公共函数","添加类型定义"]}`),
		// 标记完成
		mockllm.CreateToolCallResponse("todo_modify", `{"task_id":"1","status":"completed"}`),
		// 文本回答
		mockllm.CreateTextResponse("重构计划已制定，第一步已完成。"),
	)

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:              []agentinterfaces.AgentRail{tpr},
		EnableTaskPlanning: true,
		MaxIterations:      10,
	})
	s.Require().NoError(err)

	sess := s.NewTestSession("code-agent-tasks")
	result, err := agent.Invoke(s.Ctx, map[string]any{
		"query":   "重构这段代码",
		"session": sess,
	})
	s.Require().NoError(err)
	s.Require().NotNil(result)

	resultType, _ := result["result_type"].(string)
	s.Equal("answer", resultType, "结果类型应为 answer")
}
