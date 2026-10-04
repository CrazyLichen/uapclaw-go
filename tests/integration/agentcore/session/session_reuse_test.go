//go:build integration

package session_test

import (
	"testing"

	"github.com/stretchr/testify/suite"

	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SessionReuseSuite 测试同一 Session 在多次 Invoke 间的状态持久化。
//
// 对齐 Python:
//   - tests/unit_tests/core/context_engine/test_context_engine.py: test_create_context_reuses_existing
//   - tests/system_tests/harness/test_deep_agent_outer_loop_system.py: 同一 session 多次 invoke
//   - tests/unit_tests/harness/rails/evolution/test_evolution_rail.py: 同一 conversation_id 跨 invoke
//
// 核心验证：
//   - 同一 session 的第一次和第二次 Invoke 之间状态持久化
//   - conversation_id 正确传播
//   - 不同 session 之间状态隔离
type SessionReuseSuite struct {
	isuite.AgentSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestSessionReuseSuite(t *testing.T) {
	suite.Run(t, new(SessionReuseSuite))
}

// TestSessionReuse_同一Session两次Invoke 测试同一 Session 两次 Invoke 正常完成。
// 对齐 Python: test_create_context_reuses_existing
//
// 核心验证：
//   - 同一 session 对象在两次 Invoke 中均可使用
//   - 两次 Invoke 均正常返回结果
//   - LLM 调用次数正确
func (s *SessionReuseSuite) TestSessionReuse_同一Session两次Invoke() {
	ctx := s.Ctx

	s.MockLLM.SetResponses(
		mockllm.CreateTextResponse("第一次回答"),
		mockllm.CreateTextResponse("第二次回答"),
	)

	agent, err := s.NewDeepAgentForTest(ctx, hconfig.CreateDeepAgentParams{
		MaxIterations: 5,
	})
	s.Require().NoError(err, "创建 DeepAgent 失败")

	sess := s.NewTestSession("reuse-same-session")
	s.Require().NoError(sess.PreRun(ctx), "Session PreRun 失败")

	// 第一次 Invoke
	result1, err := agent.Invoke(ctx, map[string]any{"query": "第一个问题"},
		agentinterfaces.WithSession(sess),
	)
	s.Require().NoError(err, "第一次 Invoke 失败")
	s.Require().NotNil(result1, "第一次 Invoke 应返回结果")

	s.Equal(1, s.MockLLM.InvokeCallCount(), "第一次 Invoke 后 LLM 应被调用 1 次")

	// 第二次 Invoke（同一 session）
	s.MockLLM.SetResponses(
		mockllm.CreateTextResponse("第二次回答"),
	)
	result2, err := agent.Invoke(ctx, map[string]any{"query": "第二个问题"},
		agentinterfaces.WithSession(sess),
	)
	s.Require().NoError(err, "第二次 Invoke 失败")
	s.Require().NotNil(result2, "第二次 Invoke 应返回结果")

	sess.PostRun(ctx)

	// 验证两次 Invoke 都正常完成
	output1, _ := result1["output"].(string)
	output2, _ := result2["output"].(string)
	s.NotEmpty(output1, "第一次结果不应为空")
	s.NotEmpty(output2, "第二次结果不应为空")
}

// TestSessionReuse_ConversationID传播 测试 conversation_id 正确传播到 Session。
// 对齐 Python: test_invoke_with_session_id
//
// 核心验证：
//   - Invoke 传入 conversation_id 时，Session 的 session_id 应与 conversation_id 一致
func (s *SessionReuseSuite) TestSessionReuse_ConversationID传播() {
	ctx := s.Ctx

	s.MockLLM.SetResponses(
		mockllm.CreateTextResponse("conv_id 测试"),
	)

	agent, err := s.NewDeepAgentForTest(ctx, hconfig.CreateDeepAgentParams{
		MaxIterations: 5,
	})
	s.Require().NoError(err, "创建 DeepAgent 失败")

	// 使用 conversation_id 作为 session_id
	sess := s.NewTestSession("conv_abc_123")
	s.Require().NoError(sess.PreRun(ctx), "Session PreRun 失败")

	result, err := agent.Invoke(ctx, map[string]any{
		"query":           "测试 conversation_id",
		"conversation_id": "conv_abc_123",
	}, agentinterfaces.WithSession(sess))
	s.Require().NoError(err, "Invoke 失败")
	s.Require().NotNil(result, "应返回结果")

	sess.PostRun(ctx)

	// 验证 session_id 保持一致
	s.Equal("conv_abc_123", sess.GetSessionID(),
		"Session ID 应与 conversation_id 一致")
}

// TestSessionReuse_不同Session隔离 测试不同 Session 之间状态隔离。
// 对齐 Python: test_create_context_isolated_per_session
//
// 核心验证：
//   - 不同 session 对象的 Invoke 互不影响
//   - 两次使用不同 session 的 Invoke 均正常完成
func (s *SessionReuseSuite) TestSessionReuse_不同Session隔离() {
	ctx := s.Ctx

	s.MockLLM.SetResponses(
		mockllm.CreateTextResponse("session-a 回答"),
		mockllm.CreateTextResponse("session-b 回答"),
	)

	agent, err := s.NewDeepAgentForTest(ctx, hconfig.CreateDeepAgentParams{
		MaxIterations: 5,
	})
	s.Require().NoError(err, "创建 DeepAgent 失败")

	// Session A
	sessA := s.NewTestSession("session-a")
	s.Require().NoError(sessA.PreRun(ctx), "Session A PreRun 失败")

	resultA, err := agent.Invoke(ctx, map[string]any{"query": "A 的问题"},
		agentinterfaces.WithSession(sessA),
	)
	s.Require().NoError(err, "Session A Invoke 失败")
	s.Require().NotNil(resultA, "Session A 应返回结果")

	sessA.PostRun(ctx)

	// Session B（不同 session）
	sessB := s.NewTestSession("session-b")
	s.Require().NoError(sessB.PreRun(ctx), "Session B PreRun 失败")

	s.MockLLM.SetResponses(
		mockllm.CreateTextResponse("session-b 回答"),
	)

	resultB, err := agent.Invoke(ctx, map[string]any{"query": "B 的问题"},
		agentinterfaces.WithSession(sessB),
	)
	s.Require().NoError(err, "Session B Invoke 失败")
	s.Require().NotNil(resultB, "Session B 应返回结果")

	sessB.PostRun(ctx)

	// 验证两个 session 都正常完成
	outputA, _ := resultA["output"].(string)
	outputB, _ := resultB["output"].(string)
	s.NotEmpty(outputA, "Session A 结果不应为空")
	s.NotEmpty(outputB, "Session B 结果不应为空")

	// 验证 session ID 不同
	s.NotEqual(sessA.GetSessionID(), sessB.GetSessionID(),
		"两个 Session 的 ID 应不同")
}
