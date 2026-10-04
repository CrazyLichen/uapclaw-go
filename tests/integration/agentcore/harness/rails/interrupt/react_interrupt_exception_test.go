//go:build integration

package interrupt_test

import (
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/suite"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/interrupt"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
	"github.com/uapclaw/uapclaw-go/tests/integration/testhelpers"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ReactAgentInterruptExceptionSuite ReActAgent 中断异常恢复测试套件。
// 对齐 Python: tests/system_tests/harness/rail/test_hitl_rail.py（异常恢复场景）
//
// 测试中断后恢复流程中的异常场景：
//  1. 工具执行错误后恢复：中断-确认后正常完成
//  2. 连续中断后恢复：多次中断-确认后正常完成
//  3. 中断后流式恢复：中断-确认后正常完成
//
// 运行方式: go test -tags="sqlite_fts5 test integration" ./tests/integration/agentcore/harness/rails/interrupt/...
type ReactAgentInterruptExceptionSuite struct {
	isuite.AgentSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestReactAgentInterruptExceptionSuite 运行 ReActAgent 中断异常恢复测试套件
func TestReactAgentInterruptExceptionSuite(t *testing.T) {
	suite.Run(t, new(ReactAgentInterruptExceptionSuite))
}

// TestInterrupt_工具执行错误后恢复 测试工具执行中触发中断后，确认中断可正常恢复。
// 对齐 Python: test_hitl_rail_auto_confirm（异常恢复部分）
//
// 场景：
//  1. MockLLM 返回 write_file 工具调用 → ConfirmInterruptRail 拦截 → result_type=="interrupt"
//  2. 确认中断后，工具已在确认阶段执行，MockLLM 下一个响应为文本 → result_type=="answer"
func (s *ReactAgentInterruptExceptionSuite) TestInterrupt_工具执行错误后恢复() {
	ctx := s.Ctx

	var writeInvokeCount atomic.Int32
	writeTool := newTrackedWriteFileTool(&writeInvokeCount)

	rail := interrupt.NewConfirmInterruptRail(interrupt.WithConfirmToolNames("write_file"))

	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("write_file", `{"filepath":"/tmp/test.txt","content":"hello"}`),
		mockllm.CreateTextResponse("文件已写入"),
	)

	agent, err := s.NewDeepAgentForTest(ctx, hconfig.CreateDeepAgentParams{
		ToolInstances: []tool.Tool{writeTool},
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 5,
	})
	s.Require().NoError(err, "创建 DeepAgent 失败")

	sess := s.NewTestSession("react-interrupt-exception-recover")
	s.Require().NoError(sess.PreRun(ctx), "Session PreRun 失败")

	// 第 1 步：Invoke 触发中断
	result, err := agent.Invoke(ctx, map[string]any{"query": "写入文件"},
		agentinterfaces.WithSession(sess),
	)
	s.Require().NoError(err, "首次 Invoke 失败")
	s.Require().NotNil(result, "结果不应为 nil")

	interruptIDs, _ := testhelpers.AssertInterruptResult(s.T(), result, 1)
	s.Require().NotEmpty(interruptIDs, "应有至少一个 interrupt_id")

	// 第 2 步：确认中断并恢复
	interactiveInput := testhelpers.ConfirmInterrupt(interruptIDs[0], false)
	result, err = agent.Invoke(ctx, map[string]any{"query": interactiveInput},
		agentinterfaces.WithSession(sess),
	)
	s.Require().NoError(err, "恢复 Invoke 失败")
	s.Require().NotNil(result, "恢复结果不应为 nil")

	testhelpers.AssertAnswerResult(s.T(), result)
	s.Equal(int32(1), writeInvokeCount.Load(), "确认后 write_file 应被执行 1 次")

	sess.PostRun(ctx)
}

// TestInterrupt_连续中断后恢复 测试多次中断-确认后 Agent 可正常恢复。
// 对齐 Python: test_hitl_rail_chain_tools（连续中断恢复场景）
//
// 场景：
//  1. MockLLM 返回 write_file 工具调用 → 第 1 次中断
//  2. 确认后 MockLLM 返回第 2 个 write_file 工具调用 → 第 2 次中断
//  3. 再次确认后 MockLLM 返回文本响应 → 正常完成
//  4. 验证 write_file 共被调用 2 次
func (s *ReactAgentInterruptExceptionSuite) TestInterrupt_连续中断后恢复() {
	ctx := s.Ctx

	var writeInvokeCount atomic.Int32
	writeTool := newTrackedWriteFileTool(&writeInvokeCount)

	rail := interrupt.NewConfirmInterruptRail(interrupt.WithConfirmToolNames("write_file"))

	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("write_file", `{"filepath":"/tmp/a.txt","content":"first"}`),
		mockllm.CreateToolCallResponse("write_file", `{"filepath":"/tmp/b.txt","content":"second"}`),
		mockllm.CreateTextResponse("全部文件已写入"),
	)

	agent, err := s.NewDeepAgentForTest(ctx, hconfig.CreateDeepAgentParams{
		ToolInstances: []tool.Tool{writeTool},
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 10,
	})
	s.Require().NoError(err, "创建 DeepAgent 失败")

	sess := s.NewTestSession("react-interrupt-consecutive")
	s.Require().NoError(sess.PreRun(ctx), "Session PreRun 失败")

	// 第 1 步：Invoke 触发第 1 次中断
	result, err := agent.Invoke(ctx, map[string]any{"query": "写入文件"},
		agentinterfaces.WithSession(sess),
	)
	s.Require().NoError(err, "首次 Invoke 失败")
	s.Require().NotNil(result, "结果不应为 nil")

	interruptIDs, _ := testhelpers.AssertInterruptResult(s.T(), result, 1)
	s.Require().NotEmpty(interruptIDs, "应有至少一个 interrupt_id")

	// 第 2 步：确认第 1 次中断，触发第 2 次中断
	interactiveInput := testhelpers.ConfirmInterrupt(interruptIDs[0], false)
	result, err = agent.Invoke(ctx, map[string]any{"query": interactiveInput},
		agentinterfaces.WithSession(sess),
	)
	s.Require().NoError(err, "第 2 次 Invoke 失败")
	s.Require().NotNil(result, "第 2 次结果不应为 nil")

	// 第 2 次也应是中断
	interruptIDs2, _ := testhelpers.AssertInterruptResult(s.T(), result, 1)
	s.Require().NotEmpty(interruptIDs2, "第 2 次中断应有 interrupt_id")

	// 第 3 步：确认第 2 次中断，完成执行
	interactiveInput2 := testhelpers.ConfirmInterrupt(interruptIDs2[0], false)
	result, err = agent.Invoke(ctx, map[string]any{"query": interactiveInput2},
		agentinterfaces.WithSession(sess),
	)
	s.Require().NoError(err, "第 3 次 Invoke 失败")
	s.Require().NotNil(result, "第 3 次结果不应为 nil")

	testhelpers.AssertAnswerResult(s.T(), result)
	s.Equal(int32(2), writeInvokeCount.Load(), "确认 2 次后 write_file 应被执行 2 次")

	sess.PostRun(ctx)
}

// TestInterrupt_中断后流式恢复 测试中断后使用 Invoke 恢复可正常完成。
// 对齐 Python: test_hitl_rail_auto_confirm（Invoke 恢复场景）
//
// 场景：
//  1. Invoke 触发中断
//  2. 使用 Invoke 确认中断并恢复
//  3. 验证最终结果为 answer
func (s *ReactAgentInterruptExceptionSuite) TestInterrupt_中断后流式恢复() {
	ctx := s.Ctx

	var writeInvokeCount atomic.Int32
	writeTool := newTrackedWriteFileTool(&writeInvokeCount)

	rail := interrupt.NewConfirmInterruptRail(interrupt.WithConfirmToolNames("write_file"))

	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("write_file", `{"filepath":"/tmp/stream.txt","content":"streaming"}`),
		mockllm.CreateTextResponse("文件已写入"),
	)

	agent, err := s.NewDeepAgentForTest(ctx, hconfig.CreateDeepAgentParams{
		ToolInstances: []tool.Tool{writeTool},
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 5,
	})
	s.Require().NoError(err, "创建 DeepAgent 失败")

	sess := s.NewTestSession("react-interrupt-streaming-recover")
	s.Require().NoError(sess.PreRun(ctx), "Session PreRun 失败")

	// 第 1 步：Invoke 触发中断
	result, err := agent.Invoke(ctx, map[string]any{"query": "写入文件"},
		agentinterfaces.WithSession(sess),
	)
	s.Require().NoError(err, "首次 Invoke 失败")
	s.Require().NotNil(result, "结果不应为 nil")

	interruptIDs, _ := testhelpers.AssertInterruptResult(s.T(), result, 1)
	s.Require().NotEmpty(interruptIDs, "应有至少一个 interrupt_id")

	// 第 2 步：使用 Invoke 确认中断并恢复
	interactiveInput := testhelpers.ConfirmInterrupt(interruptIDs[0], false)
	result, err = agent.Invoke(ctx, map[string]any{"query": interactiveInput},
		agentinterfaces.WithSession(sess),
	)
	s.Require().NoError(err, "恢复 Invoke 失败")
	s.Require().NotNil(result, "恢复结果不应为 nil")

	testhelpers.AssertAnswerResult(s.T(), result)
	s.Equal(int32(1), writeInvokeCount.Load(), "确认后 write_file 应被执行 1 次")

	sess.PostRun(ctx)
}

