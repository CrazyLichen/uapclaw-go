//go:build integration

package context_engineer

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/suite"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	ce "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/context_engineer"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ContextEngineerRailSuite 测试 ContextAssembleRail / ContextProcessorRail。
//
// 对齐 Python: tests/unit_tests/harness/test_context_assemble_rail.py
// + tests/unit_tests/harness/test_context_processor_rail.py
// 两个 Rail 均覆盖了 GetCallbacks()，可测回调链路。
type ContextEngineerRailSuite struct {
	isuite.AgentSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestContextEngineerRailSuite(t *testing.T) {
	suite.Run(t, new(ContextEngineerRailSuite))
}

// TestContextAssembleRail_Init成功 测试 NewContextAssembleRail + Init 成功。
// 对齐 Python: TestContextAssembleRail.test_rail_init_captures_system_prompt_builder ——
// Python 中 ContextAssembleRail.init 捕获 system_prompt_builder 和 ability_manager。
// 注意：需先 Invoke 触发 ensureInitialized。
func (s *ContextEngineerRailSuite) TestContextAssembleRail_Init成功() {
	rail := ce.NewContextAssembleRail()

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("上下文组装测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	// 先 Invoke 触发 ensureInitialized → Rail Init
	_, err = agent.Invoke(s.Ctx, map[string]any{"query": "上下文组装测试"})
	s.Require().NoError(err, "Invoke 不应返回错误")

	// 验证 ContextAssembleRail 已注册
	railType := reflect.TypeOf(&ce.ContextAssembleRail{})
	foundRails := agent.FindRailsByType(railType)
	s.NotEmpty(foundRails, "应注册 ContextAssembleRail")
}

// TestContextProcessorRail_Init成功 测试 NewContextProcessorRail + Init 成功。
// 对齐 Python: TestContextProcessorRail.test_init_processors_merge ——
// Python 中 ContextProcessorRail.init 合并预设处理器和用户处理器。
// 注意：需先 Invoke 触发 ensureInitialized。
func (s *ContextEngineerRailSuite) TestContextProcessorRail_Init成功() {
	rail := ce.NewContextProcessorRail()

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("上下文处理测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	// 先 Invoke 触发 ensureInitialized
	_, err = agent.Invoke(s.Ctx, map[string]any{"query": "上下文处理测试"})
	s.Require().NoError(err, "Invoke 不应返回错误")

	// 验证 ContextProcessorRail 已注册
	railType := reflect.TypeOf(&ce.ContextProcessorRail{})
	foundRails := agent.FindRailsByType(railType)
	s.NotEmpty(foundRails, "应注册 ContextProcessorRail")
}

// TestContextProcessorRail_GetCallbacks事件完整 测试 GetCallbacks 返回 7 个事件。
// 对齐 Python: TestContextProcessorRail 回调注册 ——
// Python 中 ContextProcessorRail 注册 before_invoke/before_model_call/after_model_call/after_tool_call/on_model_exception，
// Go 端额外继承 DeepAgentRail 的 before_task_iteration + after_task_iteration。
func (s *ContextEngineerRailSuite) TestContextProcessorRail_GetCallbacks事件完整() {
	rail := ce.NewContextProcessorRail()

	callbacks := rail.GetCallbacks()
	s.Len(callbacks, 7, "ContextProcessorRail 应有 7 个回调事件（5 个覆盖 + 2 个继承）")

	expectedEvents := []agentinterfaces.AgentCallbackEvent{
		agentinterfaces.CallbackBeforeInvoke,
		agentinterfaces.CallbackBeforeModelCall,
		agentinterfaces.CallbackAfterModelCall,
		agentinterfaces.CallbackAfterToolCall,
		agentinterfaces.CallbackOnModelException,
		agentinterfaces.CallbackBeforeTaskIteration,
		agentinterfaces.CallbackAfterTaskIteration,
	}
	for _, event := range expectedEvents {
		_, exists := callbacks[event]
		s.True(exists, "应包含回调事件 %v", event)
	}
}
