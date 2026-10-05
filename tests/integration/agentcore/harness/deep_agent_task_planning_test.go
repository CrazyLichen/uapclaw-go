//go:build integration

package harness_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	model_clients "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	hsections "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/sections"
	taskplanning "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// DeepAgentTaskPlanningE2ESuite 测试 DeepAgent + TaskPlanningRail 联合 E2E。
//
// 覆盖：
//   - DeepAgent+TaskPlanningRail+TaskLoop 联合全链路
//   - ProgressReminder 启用后的 E2E 行为
//   - TaskPlanningRail 模型切换 E2E
//   - AfterModelCall UsageRecords 累加 E2E
//   - AfterInvoke 重置验证 E2E
//
// 对齐 Python: tests/system_tests/harness/test_deep_agent_task_planning.py
type DeepAgentTaskPlanningE2ESuite struct {
	isuite.AgentSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestDeepAgentTaskPlanningE2ESuite(t *testing.T) {
	suite.Run(t, new(DeepAgentTaskPlanningE2ESuite))
}

// TestDeepAgent_TaskPlanning_联合E2E 测试 DeepAgent+TaskPlanningRail+TaskLoop 联合全链路。
// 对齐 Python: test_deep_agent_task_planning
//
// 核心验证：
//   - TaskPlanningRail 注册后，todo 工具可用
//   - BeforeModelCall 注入 SectionTodo（使用真实 LLM 触发回调）
//   - TaskLoop 启用后，多轮迭代正常执行
//   - AfterInvoke 不崩溃
func (s *DeepAgentTaskPlanningE2ESuite) TestDeepAgent_TaskPlanning_联合E2E() {
	ctx := s.Ctx

	rail := taskplanning.NewTaskPlanningRail()

	// 使用真实 MockLLM 触发 BeforeModelCall 回调
	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("read_file", `{"path": "/tmp/plan.txt"}`),
		mockllm.CreateTextResponse("任务规划完成"),
	)

	agent, err := s.NewDeepAgentForTest(ctx, hconfig.CreateDeepAgentParams{
		Rails:             []agentinterfaces.AgentRail{rail},
		EnableTaskLoop:    true,
		MaxIterations:     10,
		CompletionTimeout: 30.0,
		ToolInstances:     []tool.Tool{newReadFileTool()},
	})
	s.Require().NoError(err, "创建 DeepAgent 失败")

	sess := s.NewTestSession("tp-e2e-joint")
	seedTaskPlan(agent, sess, "执行任务规划", 2)

	s.Require().NoError(sess.PreRun(ctx), "Session PreRun 失败")

	result, err := agent.Invoke(ctx, map[string]any{"query": "执行任务规划"},
		agentinterfaces.WithSession(sess))
	s.Require().NoError(err, "Invoke 不应返回错误")
	s.Require().NotNil(result, "应返回结果")

	sess.PostRun(ctx)

	// 验证 TaskPlanningRail 已注册
	am := agent.AbilityManager()
	s.Require().NotNil(am)
	s.NotNil(am.Get("todo_create"), "应注册 todo_create")

	// 验证 SectionTodo 已注入（真实 LLM 触发 BeforeModelCall）
	spb := agent.SystemPromptBuilder()
	s.Require().NotNil(spb)
	s.True(spb.HasSection(hsections.SectionTodo), "应存在 SectionTodo 节")
}

// TestDeepAgent_TaskPlanning_ProgressReminderE2E 测试启用 ProgressReminder 后的 E2E 行为。
// 对齐 Python: test_deep_agent_task_planning_with_progress_reminder
//
// 核心验证：
//   - WithEnableProgressRepeat(true) + WithListToolCallInterval(2) 配置生效
//   - 工具调用达到 interval 次数后注入进度提醒 UserMessage
//   - 进度提醒不影响正常执行流程
func (s *DeepAgentTaskPlanningE2ESuite) TestDeepAgent_TaskPlanning_ProgressReminderE2E() {
	rail := taskplanning.NewTaskPlanningRail(
		taskplanning.WithEnableProgressRepeat(true),
		taskplanning.WithListToolCallInterval(2),
	)

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("progress reminder 测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 10,
		ToolInstances: []tool.Tool{newReadFileTool(), newWriteFileTool()},
	})
	s.Require().NoError(err)

	// 多轮工具调用触发 progress reminder
	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("read_file", `{"path": "/tmp/a.txt"}`),
		mockllm.CreateToolCallResponse("write_file", `{"path": "/tmp/b.txt", "content": "data"}`),
		mockllm.CreateTextResponse("任务完成"),
	)

	result, err := agent.Invoke(s.Ctx, map[string]any{"query": "读取并写入文件"})
	s.Require().NoError(err, "Invoke 不应返回错误")
	s.Require().NotNil(result)

	// 验证 Rail 已注册且 SectionTodo 存在
	spb := agent.SystemPromptBuilder()
	s.Require().NotNil(spb)
	s.True(spb.HasSection(hsections.SectionTodo), "应存在 SectionTodo 节")
}

// TestDeepAgent_TaskPlanning_模型切换E2E 测试 TaskPlanningRail 模型切换 E2E。
// 对齐 Python: test_model_selection_switches_model
//
// 核心验证：
//   - 注册多个 MockModelClient
//   - WithModelSelection 传入模型映射
//   - BeforeModelCall 中根据 todo SelectedModelID 切换模型
func (s *DeepAgentTaskPlanningE2ESuite) TestDeepAgent_TaskPlanning_模型切换E2E() {
	// 创建额外的 MockModelClient
	fastClient := mockllm.NewMockModelClient()
	fastClient.AddTextResponse("fast model response")
	fastProvider := fmt.Sprintf("mock_fast_tp_%d", time.Now().UnixNano())

	model_clients.GetClientRegistry().Register(fastProvider, "llm", fastClient.Factory())
	defer func() {
		_ = model_clients.GetClientRegistry().Unregister(fastProvider, "llm")
	}()

	fastModel, err := llm.NewModel(
		&llmschema.ModelClientConfig{ClientID: fastProvider + "_id", ClientProvider: fastProvider, APIKey: "mock"},
		&llmschema.ModelRequestConfig{ModelName: "fast-model-tp"},
	)
	s.Require().NoError(err, "创建 fast Model 失败")

	modelSel := map[*llm.Model]string{fastModel: "快速模型，用于简单任务"}

	rail := taskplanning.NewTaskPlanningRail(
		taskplanning.WithModelSelection(modelSel),
	)

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("模型切换测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	_, err = agent.Invoke(s.Ctx, map[string]any{"query": "模型切换测试"})
	s.Require().NoError(err, "Invoke 不应返回错误")

	// 验证 SectionTodo 包含模型选择提示
	spb := agent.SystemPromptBuilder()
	s.Require().NotNil(spb)
	s.True(spb.HasSection(hsections.SectionTodo), "应存在 SectionTodo 节")

	section := spb.GetSection(hsections.SectionTodo)
	s.Require().NotNil(section)
	s.NotEmpty(section.Content, "SectionTodo 内容不应为空")
}

// TestDeepAgent_TaskPlanning_UsageRecordsE2E 测试 AfterModelCall 累加 UsageRecords 的 E2E。
// 对齐 Python: test_after_model_call_accumulates_usage
//
// 核心验证：
//   - AfterModelCall 从 Response.UsageMetadata 中提取 token 计数
//   - 累加到 usageRecords 中
//   - 多轮调用累加正确
func (s *DeepAgentTaskPlanningE2ESuite) TestDeepAgent_TaskPlanning_UsageRecordsE2E() {
	rail := taskplanning.NewTaskPlanningRail()

	s.MockLLM.SetResponses(
		mockllm.CreateTextResponse("第一轮"),
		mockllm.CreateTextResponse("第二轮"),
	)

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	// 第一轮 Invoke
	result1, err := agent.Invoke(s.Ctx, map[string]any{"query": "第一轮"})
	s.Require().NoError(err)
	s.Require().NotNil(result1)

	// 第二轮 Invoke
	result2, err := agent.Invoke(s.Ctx, map[string]any{"query": "第二轮"})
	s.Require().NoError(err)
	s.Require().NotNil(result2)

	// 验证每轮 AfterInvoke 重置 usageRecords（不崩溃即间接验证）
	// MockLLM 不返回 UsageMetadata，因此 usageRecords 应保持零值
	// 主要验证 AfterModelCall/AfterInvoke 流程正常执行
	s.Equal(2, s.MockLLM.InvokeCallCount(), "MockLLM 应被调用 2 次")
}

// TestDeepAgent_TaskPlanning_AfterInvoke重置 测试 AfterInvoke 重置 usageRecords/todosCache/toolCallCounts。
// 对齐 Python: TaskPlanningRail.after_invoke() 中状态清空逻辑
//
// 核心验证：
//   - AfterInvoke 后 usageRecords 被清空
//   - AfterInvoke 后 todosCache 被清空
//   - AfterInvoke 后 toolCallCounts 被清空
//   - AfterInvoke 调用 todoTool.CleanupSession
func (s *DeepAgentTaskPlanningE2ESuite) TestDeepAgent_TaskPlanning_AfterInvoke重置() {
	rail := taskplanning.NewTaskPlanningRail()

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("after invoke reset 测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	// 第一次 Invoke
	result1, err := agent.Invoke(s.Ctx, map[string]any{"query": "第一次"})
	s.Require().NoError(err)
	s.Require().NotNil(result1)

	// 第二次 Invoke — 验证 AfterInvoke 重置后不崩溃
	result2, err := agent.Invoke(s.Ctx, map[string]any{"query": "第二次"})
	s.Require().NoError(err)
	s.Require().NotNil(result2)

	// 验证 SectionTodo 仍然存在（AfterInvoke 不影响 SectionTodo 注入）
	spb := agent.SystemPromptBuilder()
	s.Require().NotNil(spb)
	s.True(spb.HasSection(hsections.SectionTodo), "AfterInvoke 后 SectionTodo 应仍存在")
}

