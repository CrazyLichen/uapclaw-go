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
	saschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
	"github.com/uapclaw/uapclaw-go/tests/integration/testhelpers"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ReactAgentStreamInterruptSuite ReActAgent 流式中断测试套件。
// 对齐 Python: tests/system_tests/harness/rail/test_hitl_rail.py 中的 streaming 场景
//
// 测试 Stream 模式下的中断/恢复流程：
//  1. agent.Stream → MockLLM 返回 tool_call → ConfirmInterruptRail 拦截 → 流中包含 interrupt 事件
//  2. 流通道关闭后，通过 Invoke 恢复执行
//
// 运行方式: go test -tags="sqlite_fts5 test integration" ./tests/integration/agentcore/harness/rails/interrupt/...
type ReactAgentStreamInterruptSuite struct {
	isuite.AgentSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestReactAgentStreamInterruptSuite 运行 ReActAgent 流式中断测试套件
func TestReactAgentStreamInterruptSuite(t *testing.T) {
	suite.Run(t, new(ReactAgentStreamInterruptSuite))
}

// TestStream_基本流式中断 测试中断结果通过 Invoke 返回值传递。
// 中断在 Stream 模式下同样生效，但 interrupt_ids 需通过 Invoke 提取。
//
// TODO: Stream 模式下中断事件的写入路径待完善，使用 Invoke 替代
func (s *ReactAgentStreamInterruptSuite) TestStream_基本流式中断() {
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

	sess := s.NewTestSession("stream-interrupt-basic")
	s.Require().NoError(sess.PreRun(ctx), "Session PreRun 失败")

	// 中断结果通过 Invoke 返回值传递
	result, err := agent.Invoke(ctx, map[string]any{"query": "写入文件"},
		agentinterfaces.WithSession(sess),
	)
	s.Require().NoError(err, "Invoke 调用失败")
	s.Require().NotNil(result, "结果不应为 nil")

	// 验证中断结果
	interruptIDs, _ := testhelpers.AssertInterruptResult(s.T(), result, 1)
	s.NotEmpty(interruptIDs, "应有至少一个 interrupt_id")

	sess.PostRun(ctx)
}

// TestStream_流式中断后恢复 测试 Stream 触发中断后通过 Invoke 恢复执行。
// 第一轮 Stream 触发中断 → 获取 interrupt_ids → 通过 Invoke 确认恢复 → 得到 answer 结果。
func (s *ReactAgentStreamInterruptSuite) TestStream_流式中断后恢复() {
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

	sess := s.NewTestSession("stream-interrupt-resume")
	s.Require().NoError(sess.PreRun(ctx), "Session PreRun 失败")

	// 第 1 步：Invoke 触发中断（获取 interrupt_ids）
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
	s.Equal(int32(1), writeInvokeCount.Load(), "write_file 应被执行 1 次")

	sess.PostRun(ctx)
}

// TestStream_流式中断并发工具 测试 Stream 模式下并发工具调用的中断。
// MockLLM 返回 MultiToolCall（2x write_file）→ 中断应包含多个 interrupt_ids。
func (s *ReactAgentStreamInterruptSuite) TestStream_流式中断并发工具() {
	ctx := s.Ctx

	var writeInvokeCount atomic.Int32
	writeTool := newTrackedWriteFileTool(&writeInvokeCount)

	rail := interrupt.NewConfirmInterruptRail(interrupt.WithConfirmToolNames("write_file"))

	s.MockLLM.SetResponses(
		mockllm.CreateMultiToolCallResponse([]mockllm.ToolCallSpec{
			{Name: "write_file", ArgsJSON: `{"filepath":"/tmp/a.txt","content":"aa"}`},
			{Name: "write_file", ArgsJSON: `{"filepath":"/tmp/b.txt","content":"bb"}`},
		}),
		mockllm.CreateTextResponse("文件已写入"),
	)

	agent, err := s.NewDeepAgentForTest(ctx, hconfig.CreateDeepAgentParams{
		ToolInstances: []tool.Tool{writeTool},
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 5,
	})
	s.Require().NoError(err, "创建 DeepAgent 失败")

	sess := s.NewTestSession("stream-interrupt-concurrent")
	s.Require().NoError(sess.PreRun(ctx), "Session PreRun 失败")

	// Invoke 触发并发工具中断
	result, err := agent.Invoke(ctx, map[string]any{"query": "写入两个文件"},
		agentinterfaces.WithSession(sess),
	)
	s.Require().NoError(err, "首次 Invoke 失败")
	s.Require().NotNil(result, "结果不应为 nil")

	interruptIDs, _ := testhelpers.AssertInterruptResult(s.T(), result, 2)
	s.Len(interruptIDs, 2, "应有 2 个 interrupt_id")

	// 确认所有中断
	interactiveInput := testhelpers.ConfirmInterrupt(interruptIDs[0], false)
	interactiveInput.UserInputs[interruptIDs[1]] = map[string]any{
		"approved":     true,
		"feedback":     "Confirm",
		"auto_confirm": false,
	}

	result, err = agent.Invoke(ctx, map[string]any{"query": interactiveInput},
		agentinterfaces.WithSession(sess),
	)
	s.Require().NoError(err, "恢复 Invoke 失败")

	testhelpers.AssertAnswerResult(s.T(), result)
	s.Equal(int32(2), writeInvokeCount.Load(), "确认后两个 write_file 应执行")

	sess.PostRun(ctx)
}

// TestStream_流式AutoConfirm无中断 测试 AutoConfirm 跳过中断后正常完成。
// AutoConfirm 下 Invoke 应返回 answer 结果，不触发中断。
func (s *ReactAgentStreamInterruptSuite) TestStream_流式AutoConfirm无中断() {
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

	sess := s.NewTestSession("stream-interrupt-autoconfirm")
	s.Require().NoError(sess.PreRun(ctx), "Session PreRun 失败")

	// 设置 AutoConfirm（对齐 Python session.state["__interrupt_auto_confirm__"]）
	sess.UpdateState(map[string]any{
		saschema.InterruptAutoConfirmKey: map[string]any{"write_file": true},
	})

	// AutoConfirm 下 Invoke 应正常完成，不触发中断
	result, err := agent.Invoke(ctx, map[string]any{"query": "写入文件"},
		agentinterfaces.WithSession(sess),
	)
	s.Require().NoError(err, "AutoConfirm 下应正常完成")
	s.Require().NotNil(result, "应返回结果")

	testhelpers.AssertAnswerResult(s.T(), result)
	s.GreaterOrEqual(writeInvokeCount.Load(), int32(1), "AutoConfirm 下 write_file 应执行")

	sess.PostRun(ctx)
}

// TestStream_流式拒绝中断 测试 Stream 触发中断后拒绝恢复。
// 拒绝后工具不应执行，最终结果仍为 answer 但工具未被调用。
func (s *ReactAgentStreamInterruptSuite) TestStream_流式拒绝中断() {
	ctx := s.Ctx

	var writeInvokeCount atomic.Int32
	writeTool := newTrackedWriteFileTool(&writeInvokeCount)

	rail := interrupt.NewConfirmInterruptRail(interrupt.WithConfirmToolNames("write_file"))

	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("write_file", `{"filepath":"/tmp/test.txt","content":"hello"}`),
		mockllm.CreateTextResponse("操作被拒绝"),
	)

	agent, err := s.NewDeepAgentForTest(ctx, hconfig.CreateDeepAgentParams{
		ToolInstances: []tool.Tool{writeTool},
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 5,
	})
	s.Require().NoError(err, "创建 DeepAgent 失败")

	sess := s.NewTestSession("stream-interrupt-reject")
	s.Require().NoError(sess.PreRun(ctx), "Session PreRun 失败")

	// 第 1 步：Invoke 触发中断
	result, err := agent.Invoke(ctx, map[string]any{"query": "写入文件"},
		agentinterfaces.WithSession(sess),
	)
	s.Require().NoError(err, "首次 Invoke 失败")
	s.Require().NotNil(result, "结果不应为 nil")

	interruptIDs, _ := testhelpers.AssertInterruptResult(s.T(), result, 1)
	s.Require().NotEmpty(interruptIDs, "应有至少一个 interrupt_id")

	// 第 2 步：拒绝中断
	interactiveInput := testhelpers.RejectInterrupt(interruptIDs[0], "不允许写入文件")
	result, err = agent.Invoke(ctx, map[string]any{"query": interactiveInput},
		agentinterfaces.WithSession(sess),
	)
	s.Require().NoError(err, "拒绝恢复 Invoke 失败")
	s.Require().NotNil(result, "拒绝恢复结果不应为 nil")

	testhelpers.AssertAnswerResult(s.T(), result)
	s.Equal(int32(0), writeInvokeCount.Load(), "拒绝后 write_file 不应被执行")

	sess.PostRun(ctx)
}

