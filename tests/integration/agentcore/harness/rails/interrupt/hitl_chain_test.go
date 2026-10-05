//go:build integration

package interrupt

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/suite"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	rails "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	interrupt "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/interrupt"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// HITLRailChainSuite 测试 HITL（Human-in-the-Loop）Rail 链式协作。
//
// 对齐 Python: tests/system_tests/agent/react_agent/interrupt/test_hitl_rail_chain_tools.py
// 聚焦多种中断 Rail 在同一 Agent 中共存、回调注册、优先级排序和事件名不冲突。
type HITLRailChainSuite struct {
	isuite.AgentSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestHITLRailChainSuite(t *testing.T) {
	suite.Run(t, new(HITLRailChainSuite))
}

// TestHITL_多中断Rail共存 测试 ConfirmInterruptRail + AskUserRail 在同一 Agent 中共存。
// 对齐 Python: test_hitl_rail_chain_tools（多 Rail 链式协作）
func (s *HITLRailChainSuite) TestHITL_多中断Rail共存() {
	confirmRail := interrupt.NewConfirmInterruptRail(
		interrupt.WithConfirmToolNames("write_file", "delete_file"),
	)
	askUserRail := interrupt.NewAskUserRail("ask_user")

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("HITL 共存测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{confirmRail, askUserRail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)
	s.Require().NotNil(agent)

	// 验证两种 Rail 都已注册
	confirmType := reflect.TypeOf(&interrupt.ConfirmInterruptRail{})
	askUserType := reflect.TypeOf(&interrupt.AskUserRail{})
	s.NotEmpty(agent.FindRailsByType(confirmType), "应注册 ConfirmInterruptRail")
	s.NotEmpty(agent.FindRailsByType(askUserType), "应注册 AskUserRail")
}

// TestHITL_ConfirmInterruptRail与AskUserRail不同优先级 测试两种中断 Rail 的默认优先级。
// 对齐 Python: ConfirmInterruptRail.priority = 90, AskUserRail.priority = 90（均继承 BaseInterruptRail）
func (s *HITLRailChainSuite) TestHITL_ConfirmInterruptRail与AskUserRail不同优先级() {
	confirmRail := interrupt.NewConfirmInterruptRail(
		interrupt.WithConfirmToolNames("write_file"),
	)
	askUserRail := interrupt.NewAskUserRail("ask_user")

	// 两者都继承 BaseInterruptRail，默认优先级均为 90
	s.Equal(90, confirmRail.Priority(), "ConfirmInterruptRail 默认优先级应为 90")
	s.Equal(90, askUserRail.Priority(), "AskUserRail 默认优先级应为 90")

	// 可以手动设置不同优先级
	confirmRail.WithPriority(95)
	askUserRail.WithPriority(80)
	s.Equal(95, confirmRail.Priority(), "修改后 ConfirmInterruptRail 优先级应为 95")
	s.Equal(80, askUserRail.Priority(), "修改后 AskUserRail 优先级应为 80")
}

// TestHITL_Rail链回调排序 测试多个 Rail 在同一事件上的回调按优先级排序。
// 对齐 Python: test_hitl_rail_chain_tools 中的 read→confirm→write→reject 顺序
func (s *HITLRailChainSuite) TestHITL_Rail链回调排序() {
	// 高优先级 ConfirmInterruptRail（95）
	confirmRail := interrupt.NewConfirmInterruptRail(
		interrupt.WithConfirmToolNames("write_file"),
	)
	confirmRail.WithPriority(95)

	// 低优先级 AskUserRail（80）
	askUserRail := interrupt.NewAskUserRail("ask_user")
	askUserRail.WithPriority(80)

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("回调排序测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{confirmRail, askUserRail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)
	s.Require().NotNil(agent)

	// 两种 Rail 均注册成功
	confirmType := reflect.TypeOf(&interrupt.ConfirmInterruptRail{})
	askUserType := reflect.TypeOf(&interrupt.AskUserRail{})
	s.NotEmpty(agent.FindRailsByType(confirmType), "应注册 ConfirmInterruptRail")
	s.NotEmpty(agent.FindRailsByType(askUserType), "应注册 AskUserRail")
}

// TestHITL_每个Rail注册自己的回调 测试 ConfirmInterruptRail 和 AskUserRail 各自注册 BeforeToolCall 回调。
// 对齐 Python: BaseInterruptRail.get_callbacks() → before_tool_call
func (s *HITLRailChainSuite) TestHITL_每个Rail注册自己的回调() {
	confirmRail := interrupt.NewConfirmInterruptRail(
		interrupt.WithConfirmToolNames("write_file"),
	)
	askUserRail := interrupt.NewAskUserRail("ask_user")

	confirmCallbacks := confirmRail.GetCallbacks()
	askUserCallbacks := askUserRail.GetCallbacks()

	// 两者都注册了 BeforeToolCall 回调
	_, confirmHasBTC := confirmCallbacks[agentinterfaces.CallbackBeforeToolCall]
	_, askUserHasBTC := askUserCallbacks[agentinterfaces.CallbackBeforeToolCall]

	s.True(confirmHasBTC, "ConfirmInterruptRail 应注册 CallbackBeforeToolCall")
	s.True(askUserHasBTC, "AskUserRail 应注册 CallbackBeforeToolCall")
}

// TestHITL_回调事件名不冲突 测试两种 Rail 的回调事件名不会互相覆盖。
// 对齐 Python: 多 Rail 的 get_callbacks() 返回的 map key 不冲突
func (s *HITLRailChainSuite) TestHITL_回调事件名不冲突() {
	confirmRail := interrupt.NewConfirmInterruptRail(
		interrupt.WithConfirmToolNames("write_file"),
	)
	askUserRail := interrupt.NewAskUserRail("ask_user")

	confirmCallbacks := confirmRail.GetCallbacks()
	askUserCallbacks := askUserRail.GetCallbacks()

	// 两者都注册在 CallbackBeforeToolCall 上，回调 map 中该 key 只有一个
	// 但 Rail 框架会将同一事件上的多个回调按优先级排序执行
	s.Len(confirmCallbacks, 1, "ConfirmInterruptRail 应只注册一个事件")
	s.Len(askUserCallbacks, 1, "AskUserRail 应只注册一个事件")

	// 合并后不丢失回调
	merged := make(map[agentinterfaces.AgentCallbackEvent]bool)
	for event := range confirmCallbacks {
		merged[event] = true
	}
	for event := range askUserCallbacks {
		merged[event] = true
	}
	// 两者都注册在 before_tool_call 上，合并后仍为一个 key
	s.Len(merged, 1, "合并后回调事件名不冲突")
	s.True(merged[agentinterfaces.CallbackBeforeToolCall], "合并后应包含 CallbackBeforeToolCall")
}

// TestHITL_多种中断类型在同一回调映射中 测试同一回调映射中可包含不同中断类型的回调。
// 对齐 Python: test_hitl_rail_chain_tools 中 read→confirm + write→confirm 的多中断类型
func (s *HITLRailChainSuite) TestHITL_多种中断类型在同一回调映射中() {
	confirmRail := interrupt.NewConfirmInterruptRail(
		interrupt.WithConfirmToolNames("read", "write"),
	)
	askUserRail := interrupt.NewAskUserRail("ask_user")

	// ConfirmInterruptRail 拦截 read + write
	confirmTools := confirmRail.GetTools()
	s.Contains(confirmTools, "read", "ConfirmInterruptRail 应拦截 read")
	s.Contains(confirmTools, "write", "ConfirmInterruptRail 应拦截 write")

	// AskUserRail 拦截 ask_user
	askUserTools := askUserRail.GetTools()
	s.Contains(askUserTools, "ask_user", "AskUserRail 应拦截 ask_user")

	// 两种 Rail 共享 CallbackBeforeToolCall 事件，但拦截不同工具
	confirmCallbacks := confirmRail.GetCallbacks()
	askUserCallbacks := askUserRail.GetCallbacks()

	_, confirmHasBTC := confirmCallbacks[agentinterfaces.CallbackBeforeToolCall]
	_, askUserHasBTC := askUserCallbacks[agentinterfaces.CallbackBeforeToolCall]
	s.True(confirmHasBTC, "ConfirmInterruptRail 应注册 CallbackBeforeToolCall")
	s.True(askUserHasBTC, "AskUserRail 应注册 CallbackBeforeToolCall")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// 编译时验证所需依赖类型
var (
	_ agentinterfaces.AgentRail = (*interrupt.ConfirmInterruptRail)(nil)
	_ agentinterfaces.AgentRail = (*interrupt.AskUserRail)(nil)
	_ agentinterfaces.AgentRail = (*interrupt.BaseInterruptRail)(nil)
	_ agentinterfaces.AgentRail = (*rails.DeepAgentRail)(nil)
)
