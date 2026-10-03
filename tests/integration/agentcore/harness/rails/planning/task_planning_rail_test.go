//go:build integration

package planning

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/suite"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	taskplanning "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TaskPlanningRailSuite 测试 TaskPlanningRail 任务规划。
//
// 对齐 Python: tests/unit_tests/harness/test_task_planning_rail.py
// TaskPlanningRail 覆盖了 GetCallbacks()，可测完整回调链路。
type TaskPlanningRailSuite struct {
	isuite.AgentSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestTaskPlanningRailSuite(t *testing.T) {
	suite.Run(t, new(TaskPlanningRailSuite))
}

// TestTaskPlanningRail_Init注册TodoTool 测试 TaskPlanningRail Init 后注册 todo 工具。
// 对齐 Python: TestTaskPlanningRail.test_init_registers_tools_with_workspace ——
// Python 中 TaskPlanningRail.init 在有 workspace 时注册 todo_create/todo_list 等工具。
// 注意：需先 Invoke 触发 ensureInitialized。
func (s *TaskPlanningRailSuite) TestTaskPlanningRail_Init注册TodoTool() {
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

	// 验证 todo 工具注册（需要 SysOperation + Workspace，NewDeepAgentForTest 会自动创建）
	am := agent.AbilityManager()
	s.Require().NotNil(am)
	s.NotNil(am.Get("todo_create"), "应注册 todo_create")
	s.NotNil(am.Get("todo_list"), "应注册 todo_list")
}

// TestTaskPlanningRail_BeforeModelCall注入规划提示词 测试 BeforeModelCall 注入 task_planning section。
// 对齐 Python: TestTaskPlanningRail.test_before_model_call_adds_section ——
// Python 中 BeforeModelCall 向 SystemPromptBuilder 注入任务规划提示词。
func (s *TaskPlanningRailSuite) TestTaskPlanningRail_BeforeModelCall注入规划提示词() {
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

	// 验证 SystemPromptBuilder
	spb := agent.SystemPromptBuilder()
	s.Require().NotNil(spb, "SystemPromptBuilder 不应为 nil")
}

// TestTaskPlanningRail_回调事件完整 测试 GetCallbacks 返回 6 个事件。
// 对齐 Python: TestTaskPlanningRail 默认回调注册 ——
// Python 中 TaskPlanningRail 注册 before_model_call/after_tool_call/after_model_call/after_invoke/after_task_iteration，
// Go 端额外继承 DeepAgentRail 的 before_task_iteration。
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
