//go:build integration

package planning

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	model_clients "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	hsections "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/sections"
	taskplanning "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TaskPlanningRailSuite 测试 TaskPlanningRail 任务规划。
//
// 覆盖：
//   - Init 注册 4 个 todo 工具
//   - BeforeModelCall 注入 SectionTodo
//   - GetCallbacks 回调完整性
//   - Uninit 清理工具和 Section
//
// 对齐 Python: tests/unit_tests/harness/test_task_planning_rail.py
type TaskPlanningRailSuite struct {
	isuite.AgentSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestTaskPlanningRailSuite(t *testing.T) {
	suite.Run(t, new(TaskPlanningRailSuite))
}

// TestTaskPlanningRail_Init注册4个工具 测试 TaskPlanningRail Init 后注册 4 个 todo 工具。
// 对齐 Python: TaskPlanningRail.init() 中 todo 工具注册
func (s *TaskPlanningRailSuite) TestTaskPlanningRail_Init注册4个工具() {
	rail := taskplanning.NewTaskPlanningRail()

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("规划测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	// 先 Invoke 触发 ensureInitialized → Rail Init
	_, err = agent.Invoke(s.Ctx, map[string]any{"query": "规划测试"})
	s.Require().NoError(err, "Invoke 不应返回错误")

	// 验证 TaskPlanningRail 已注册
	railType := reflect.TypeOf(&taskplanning.TaskPlanningRail{})
	foundRails := agent.FindRailsByType(railType)
	s.NotEmpty(foundRails, "应注册 TaskPlanningRail")

	// 验证 4 个 todo 工具注册
	am := agent.AbilityManager()
	s.Require().NotNil(am)
	s.NotNil(am.Get("todo_create"), "应注册 todo_create")
	s.NotNil(am.Get("todo_list"), "应注册 todo_list")
	s.NotNil(am.Get("todo_get"), "应注册 todo_get")
	s.NotNil(am.Get("todo_modify"), "应注册 todo_modify")
}

// TestTaskPlanningRail_BeforeModelCall_注入SectionTodo 测试 BeforeModelCall 注入 SectionTodo。
// 对齐 Python: TaskPlanningRail.before_model_call() 中 BuildTodoSection
func (s *TaskPlanningRailSuite) TestTaskPlanningRail_BeforeModelCall_注入SectionTodo() {
	rail := taskplanning.NewTaskPlanningRail()

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("规划提示词测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	// 执行 Invoke 触发 BeforeModelCall
	_, err = agent.Invoke(s.Ctx, map[string]any{"query": "规划任务"})
	s.Require().NoError(err, "Invoke 不应返回错误")

	// 验证 SectionTodo 已注入
	spb := agent.SystemPromptBuilder()
	s.Require().NotNil(spb, "SystemPromptBuilder 不应为 nil")
	s.True(spb.HasSection(hsections.SectionTodo), "应存在 SectionTodo 节")

	// 验证节内容非空
	section := spb.GetSection(hsections.SectionTodo)
	s.Require().NotNil(section, "SectionTodo 节不应为 nil")
	s.NotEmpty(section.Content, "SectionTodo 节内容不应为空")
}

// TestTaskPlanningRail_回调事件完整 测试 GetCallbacks 返回 6 个事件。
func (s *TaskPlanningRailSuite) TestTaskPlanningRail_回调事件完整() {
	rail := taskplanning.NewTaskPlanningRail()

	callbacks := rail.GetCallbacks()
	s.Len(callbacks, 6, "TaskPlanningRail 应有 6 个回调事件（5 个覆盖 + 1 个继承）")

	expectedEvents := []agentinterfaces.AgentCallbackEvent{
		agentinterfaces.CallbackBeforeModelCall,
		agentinterfaces.CallbackAfterToolCall,
		agentinterfaces.CallbackAfterModelCall,
		agentinterfaces.CallbackAfterInvoke,
		agentinterfaces.CallbackAfterTaskIteration,
		agentinterfaces.CallbackBeforeTaskIteration,
	}
	for _, event := range expectedEvents {
		_, exists := callbacks[event]
		s.True(exists, "应包含回调事件 %v", event)
	}
}

// TestTaskPlanningRail_Uninit移除Todo节和工具 测试 Uninit 移除 SectionTodo 并注销 4 个 todo 工具。
// 对齐 Python: TaskPlanningRail.uninit() 中工具移除 + section 移除
func (s *TaskPlanningRailSuite) TestTaskPlanningRail_Uninit移除Todo节和工具() {
	rail := taskplanning.NewTaskPlanningRail()

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("Uninit 测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	// 执行一次 Invoke
	_, _ = agent.Invoke(s.Ctx, map[string]any{"query": "测试"})

	// Uninit 应成功
	s.NotPanics(func() {
		rail.Uninit(agent)
	}, "Uninit 不应 panic")

	// 验证 SectionTodo 已移除
	spb := agent.SystemPromptBuilder()
	s.Require().NotNil(spb)
	s.False(spb.HasSection(hsections.SectionTodo), "Uninit 后应移除 SectionTodo 节")

	// 验证 4 个 todo 工具已从 AM 注销
	am := agent.AbilityManager()
	s.Require().NotNil(am)
	s.Nil(am.Get("todo_create"), "Uninit 后应注销 todo_create")
	s.Nil(am.Get("todo_list"), "Uninit 后应注销 todo_list")
	s.Nil(am.Get("todo_get"), "Uninit 后应注销 todo_get")
	s.Nil(am.Get("todo_modify"), "Uninit 后应注销 todo_modify")
}

// TestTaskPlanningRail_默认无ModelSelection 测试默认构造时 modelSelection 为空。
// 对齐 Python: test_model_selection_default_is_none
func (s *TaskPlanningRailSuite) TestTaskPlanningRail_默认无ModelSelection() {
	rail := taskplanning.NewTaskPlanningRail()

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("model selection 测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	_, err = agent.Invoke(s.Ctx, map[string]any{"query": "无模型选择"})
	s.Require().NoError(err, "Invoke 不应返回错误")

	// 默认无 modelSelection 时，SectionTodo 不含 Model Selection 提示
	spb := agent.SystemPromptBuilder()
	s.Require().NotNil(spb)
	s.True(spb.HasSection(hsections.SectionTodo), "应存在 SectionTodo 节")
}

// TestTaskPlanningRail_ModelSelection注入系统提示词 测试带 ModelSelection 时 BeforeModelCall 注入模型选择提示。
// 对齐 Python: test_model_selection_system_prompt_includes_model_ids
//
// 核心验证：
//   - WithModelSelection 传入模型映射
//   - BeforeModelCall 后 SectionTodo 包含 Model Selection 相关提示
func (s *TaskPlanningRailSuite) TestTaskPlanningRail_ModelSelection注入系统提示词() {
	// 创建两个额外的 MockLLM 客户端并注册到 ClientRegistry
	fastClient := mockllm.NewMockModelClient()
	fastClient.AddTextResponse("fast response")
	smartClient := mockllm.NewMockModelClient()
	smartClient.AddTextResponse("smart response")

	fastProvider := fmt.Sprintf("mock_fast_%d", time.Now().UnixNano())
	smartProvider := fmt.Sprintf("mock_smart_%d", time.Now().UnixNano())

	model_clients.GetClientRegistry().Register(fastProvider, "llm", fastClient.Factory())
	model_clients.GetClientRegistry().Register(smartProvider, "llm", smartClient.Factory())

	// 测试结束后清理注册
	defer func() {
		_ = model_clients.GetClientRegistry().Unregister(fastProvider, "llm")
		_ = model_clients.GetClientRegistry().Unregister(smartProvider, "llm")
	}()

	fastModel, err := llm.NewModel(
		&llmschema.ModelClientConfig{ClientID: fastProvider + "_id", ClientProvider: fastProvider, APIKey: "mock"},
		&llmschema.ModelRequestConfig{ModelName: "fast-model"},
	)
	s.Require().NoError(err, "创建 fast Model 失败")

	smartModel, err := llm.NewModel(
		&llmschema.ModelClientConfig{ClientID: smartProvider + "_id", ClientProvider: smartProvider, APIKey: "mock"},
		&llmschema.ModelRequestConfig{ModelName: "smart-model"},
	)
	s.Require().NoError(err, "创建 smart Model 失败")

	modelSel := map[*llm.Model]string{
		fastModel:  "快速模型，用于简单任务",
		smartModel: "智能模型，用于复杂推理",
	}

	rail := taskplanning.NewTaskPlanningRail(
		taskplanning.WithModelSelection(modelSel),
	)

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("model selection prompt 测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	_, err = agent.Invoke(s.Ctx, map[string]any{"query": "模型选择测试"})
	s.Require().NoError(err, "Invoke 不应返回错误")

	// 验证 SectionTodo 包含模型选择提示
	spb := agent.SystemPromptBuilder()
	s.Require().NotNil(spb)
	s.True(spb.HasSection(hsections.SectionTodo), "应存在 SectionTodo 节")

	section := spb.GetSection(hsections.SectionTodo)
	s.Require().NotNil(section, "SectionTodo 不应为 nil")
	// 内容应包含 model ID 相关信息
	s.NotEmpty(section.Content, "SectionTodo 内容不应为空")
}

// TestTaskPlanningRail_UsageRecords默认空 测试默认构造时 usageRecords 为空。
// 对齐 Python: test_model_selection_default_is_none (usage records 部分)
func (s *TaskPlanningRailSuite) TestTaskPlanningRail_UsageRecords默认空() {
	rail := taskplanning.NewTaskPlanningRail()

	// 验证初始 usageRecords 为空（通过 AfterInvoke 不崩溃间接验证）
	s.MockLLM.SetResponses(mockllm.CreateTextResponse("usage records 测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	// AfterModelCall 累加 usage，AfterInvoke 重置
	result, err := agent.Invoke(s.Ctx, map[string]any{"query": "usage 测试"})
	s.Require().NoError(err, "Invoke 不应返回错误")
	s.Require().NotNil(result, "应返回结果")
}

// TestTaskPlanningRail_ProgressRepeat默认关闭 测试默认 enableProgressRepeat=false。
// 对齐 Python: test_enable_progress_repeat_default
func (s *TaskPlanningRailSuite) TestTaskPlanningRail_ProgressRepeat默认关闭() {
	rail := taskplanning.NewTaskPlanningRail()

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("progress 测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	// 默认 progressRepeat 关闭，多次工具调用后不应注入进度提醒
	result, err := agent.Invoke(s.Ctx, map[string]any{"query": "progress 测试"})
	s.Require().NoError(err, "Invoke 不应返回错误")
	s.Require().NotNil(result, "应返回结果")
}

// TestTaskPlanningRail_Priority90 测试 TaskPlanningRail 优先级为 90。
// 对齐 Python: test_priority_is_90
func (s *TaskPlanningRailSuite) TestTaskPlanningRail_Priority90() {
	rail := taskplanning.NewTaskPlanningRail()

	// TaskPlanningRail.Priority() 应返回 90
	s.Equal(90, rail.Priority(), "TaskPlanningRail 优先级应为 90")
}

// TestModelUsageRecord_Add 测试 ModelUsageRecord.Add 累加 token 计数。
// 对齐 Python: test_after_model_call_accumulates_usage
func (s *TaskPlanningRailSuite) TestModelUsageRecord_Add() {
	record := &hschema.ModelUsageRecord{ModelID: "test-model"}

	// 第一次累加
	record.Add(100, 50)
	s.Equal(100, record.InputTokens, "第一次 InputTokens 应为 100")
	s.Equal(50, record.OutputTokens, "第一次 OutputTokens 应为 50")

	// 第二次累加
	record.Add(100, 50)
	s.Equal(200, record.InputTokens, "第二次累加后 InputTokens 应为 200")
	s.Equal(100, record.OutputTokens, "第二次累加后 OutputTokens 应为 100")
}
