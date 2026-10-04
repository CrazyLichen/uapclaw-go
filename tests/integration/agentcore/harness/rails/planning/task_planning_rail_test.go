//go:build integration

package planning

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/suite"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
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
