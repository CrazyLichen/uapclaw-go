//go:build integration

package harness

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	hsections "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/sections"
	taskplanning "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// CodeAgentDeepSuite 测试 CodeAgent 深度集成行为。
//
// 覆盖：
//   - CodeAgent + TaskPlanningRail + Session 联合
//   - CodeAgent + 多工具调用 E2E
//   - CodeAgent + SectionTodo 注入验证
//   - CodeAgent 错误恢复
//   - CodeAgent + 自定义工具 E2E
//   - CodeAgent updateRuntimeConfig 路径
//
// 对齐 Python: tests/system_tests/code_agent/test_code_agent_e2e.py
type CodeAgentDeepSuite struct {
	isuite.AgentSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestCodeAgentDeepSuite(t *testing.T) {
	suite.Run(t, new(CodeAgentDeepSuite))
}

// TestCodeAgentDeep_TaskPlanningWithSession 测试 CodeAgent + TaskPlanningRail + Session 联合。
// 对齐 Python: code_agent + session + task_planning
//
// 核心验证：
//   - Session 状态持久化
//   - TaskPlanningRail 工具注册在 Session 上下文中
//   - 多次 Invoke 不崩溃
func (s *CodeAgentDeepSuite) TestCodeAgentDeep_TaskPlanningWithSession() {
	tpr := taskplanning.NewTaskPlanningRail()

	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("todo_create", `{"goal":"测试目标","tasks":["步骤一","步骤二"]}`),
		mockllm.CreateTextResponse("任务计划已创建"),
	)

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:              []agentinterfaces.AgentRail{tpr},
		EnableTaskPlanning: true,
		MaxIterations:      10,
	})
	s.Require().NoError(err)

	sess := s.NewTestSession("code-deep-session")
	s.Require().NoError(sess.PreRun(s.Ctx), "Session PreRun 失败")

	result, err := agent.Invoke(s.Ctx, map[string]any{"query": "创建任务计划"},
		agentinterfaces.WithSession(sess))
	s.Require().NoError(err, "Invoke 不应返回错误")
	s.Require().NotNil(result)

	sess.PostRun(s.Ctx)

	// 验证 TaskPlanningRail 工具已注册
	am := agent.AbilityManager()
	s.Require().NotNil(am)
	s.NotNil(am.Get("todo_create"), "应注册 todo_create")
}

// TestCodeAgentDeep_多工具调用E2E 测试 CodeAgent 多工具调用端到端。
// 对齐 Python: code_agent multi-tool call sequence
//
// 核心验证：
//   - todo_create + read_file + write_file + todo_modify 工具调用链
//   - MockLLM 模拟完整工作流
func (s *CodeAgentDeepSuite) TestCodeAgentDeep_多工具调用E2E() {
	tpr := taskplanning.NewTaskPlanningRail()

	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("todo_create", `{"goal":"开发功能","tasks":["读取需求","实现代码","测试验证"]}`),
		mockllm.CreateToolCallResponse("read_file", `{"path": "/tmp/requirements.txt"}`),
		mockllm.CreateToolCallResponse("write_file", `{"path": "/tmp/impl.go", "content": "package main"}`),
		mockllm.CreateToolCallResponse("todo_modify", `{"task_id":"1","status":"completed"}`),
		mockllm.CreateTextResponse("功能开发完成"),
	)

	readTool := newCAReadFileTool()
	writeTool := newCAWriteFileTool()

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:              []agentinterfaces.AgentRail{tpr},
		EnableTaskPlanning: true,
		MaxIterations:      20,
		ToolInstances:      []tool.Tool{readTool, writeTool},
	})
	s.Require().NoError(err)

	result, err := agent.Invoke(s.Ctx, map[string]any{"query": "开发一个功能"})
	s.Require().NoError(err, "Invoke 不应返回错误")
	s.Require().NotNil(result)

	// 验证结果类型
	resultType, _ := result["result_type"].(string)
	s.Equal("answer", resultType, "结果类型应为 answer")
}

// TestCodeAgentDeep_SectionTodo注入验证 测试 CodeAgent SectionTodo 注入。
// 对齐 Python: TaskPlanningRail.BeforeModelCall() 注入 SectionTodo
func (s *CodeAgentDeepSuite) TestCodeAgentDeep_SectionTodo注入验证() {
	tpr := taskplanning.NewTaskPlanningRail()

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("Section 验证测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:              []agentinterfaces.AgentRail{tpr},
		EnableTaskPlanning: true,
		MaxIterations:      3,
	})
	s.Require().NoError(err)

	_, err = agent.Invoke(s.Ctx, map[string]any{"query": "验证 SectionTodo"})
	s.Require().NoError(err)

	spb := agent.SystemPromptBuilder()
	s.Require().NotNil(spb)
	s.True(spb.HasSection(hsections.SectionTodo), "应存在 SectionTodo 节")
}

// TestCodeAgentDeep_工具错误恢复 测试 CodeAgent 工具调用失败后的恢复能力。
// 对齐 Python: code_agent error recovery
func (s *CodeAgentDeepSuite) TestCodeAgentDeep_工具错误恢复() {
	tpr := taskplanning.NewTaskPlanningRail()

	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("todo_create", `{"goal":"错误恢复测试","tasks":["可能失败的任务"]}`),
		mockllm.CreateToolCallResponse("fail_tool", `{"input": "test"}`),
		mockllm.CreateTextResponse("已处理工具错误，继续执行"),
	)

	failTool := newCAFailTool()

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:              []agentinterfaces.AgentRail{tpr},
		EnableTaskPlanning: true,
		MaxIterations:      10,
		ToolInstances:      []tool.Tool{failTool},
	})
	s.Require().NoError(err)

	result, err := agent.Invoke(s.Ctx, map[string]any{"query": "执行可能失败的操作"})
	s.Require().NoError(err, "即使工具失败 Invoke 也不应返回错误")
	s.Require().NotNil(result)
}

// TestCodeAgentDeep_自定义工具E2E 测试 CodeAgent 注册自定义工具的 E2E。
// 对齐 Python: code_agent custom tool registration
func (s *CodeAgentDeepSuite) TestCodeAgentDeep_自定义工具E2E() {
	tpr := taskplanning.NewTaskPlanningRail()

	customTool := newCACustomTool("data_processor")

	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("data_processor", `{"input": "test data"}`),
		mockllm.CreateTextResponse("数据处理完成"),
	)

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:              []agentinterfaces.AgentRail{tpr},
		EnableTaskPlanning: true,
		MaxIterations:      10,
		ToolInstances:      []tool.Tool{customTool},
	})
	s.Require().NoError(err)

	result, err := agent.Invoke(s.Ctx, map[string]any{"query": "处理数据"})
	s.Require().NoError(err)
	s.Require().NotNil(result)

	// 验证自定义工具已注册
	am := agent.AbilityManager()
	s.Require().NotNil(am)
	s.NotNil(am.Get("data_processor"), "应注册 data_processor")
}

// TestCodeAgentDeep_多次Invoke不崩溃 测试 CodeAgent 多次 Invoke 不崩溃。
func (s *CodeAgentDeepSuite) TestCodeAgentDeep_多次Invoke不崩溃() {
	tpr := taskplanning.NewTaskPlanningRail()

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:              []agentinterfaces.AgentRail{tpr},
		EnableTaskPlanning: true,
		MaxIterations:      10,
	})
	s.Require().NoError(err)

	// 第一次
	s.MockLLM.SetResponses(mockllm.CreateTextResponse("第一次"))
	result1, err := agent.Invoke(s.Ctx, map[string]any{"query": "第一次"})
	s.Require().NoError(err)
	s.Require().NotNil(result1)

	// 第二次
	s.MockLLM.SetResponses(mockllm.CreateTextResponse("第二次"))
	result2, err := agent.Invoke(s.Ctx, map[string]any{"query": "第二次"})
	s.Require().NoError(err)
	s.Require().NotNil(result2)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// newCAReadFileTool 创建 read_file 模拟工具。
func newCAReadFileTool() tool.Tool {
	tc := tool.NewToolCardWithID("read_file", "read_file", "读取文件", nil, nil)
	t, _ := tool.NewMapFunction(tc,
		func(_ context.Context, inputs map[string]any) (map[string]any, error) {
			path, _ := inputs["path"].(string)
			return map[string]any{"content": fmt.Sprintf("文件 %s 的内容", path)}, nil
		}, nil,
	)
	return t
}

// newCAWriteFileTool 创建 write_file 模拟工具。
func newCAWriteFileTool() tool.Tool {
	tc := tool.NewToolCardWithID("write_file", "write_file", "写入文件", nil, nil)
	t, _ := tool.NewMapFunction(tc,
		func(_ context.Context, _ map[string]any) (map[string]any, error) {
			return map[string]any{"status": "ok"}, nil
		}, nil,
	)
	return t
}

// newCAFailTool 创建总是失败的模拟工具。
func newCAFailTool() tool.Tool {
	tc := tool.NewToolCardWithID("fail_tool", "fail_tool", "总是失败的工具", nil, nil)
	t, _ := tool.NewMapFunction(tc,
		func(_ context.Context, _ map[string]any) (map[string]any, error) {
			return nil, fmt.Errorf("工具执行失败: 模拟错误")
		}, nil,
	)
	return t
}

// newCACustomTool 创建自定义模拟工具。
func newCACustomTool(id string) tool.Tool {
	tc := tool.NewToolCardWithID(id, id, "自定义数据处理工具", nil, nil)
	t, _ := tool.NewMapFunction(tc,
		func(_ context.Context, inputs map[string]any) (map[string]any, error) {
			input, _ := inputs["input"].(string)
			return map[string]any{"result": "processed: " + input}, nil
		}, nil,
	)
	return t
}
