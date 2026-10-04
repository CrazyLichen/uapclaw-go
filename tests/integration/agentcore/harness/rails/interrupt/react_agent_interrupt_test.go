//go:build integration

package interrupt_test

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/suite"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/interrupt"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
	"github.com/uapclaw/uapclaw-go/tests/integration/testhelpers"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ReactAgentInterruptSuite ReActAgent 基础中断/恢复测试套件。
// 对齐 Python: tests/system_tests/harness/rail/test_hitl_rail.py
//
// 使用 MockLLM 预设工具调用响应，确保确定性地测试中断/恢复流程：
//  1. agent.Invoke → MockLLM 返回 tool_call → ConfirmInterruptRail 拦截 → result_type=="interrupt"
//  2. agent.Invoke(query=InteractiveInput) → 恢复执行 → result_type=="answer"
//
// 运行方式: go test -tags="sqlite_fts5 test integration" ./tests/integration/agentcore/harness/rails/interrupt/...
type ReactAgentInterruptSuite struct {
	isuite.AgentSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestReactAgentInterruptSuite 运行 ReActAgent 基础中断/恢复测试套件
func TestReactAgentInterruptSuite(t *testing.T) {
	suite.Run(t, new(ReactAgentInterruptSuite))
}

// TestConfirm_确认后继续执行 测试确认中断后 Agent 继续执行。
// 对齐 Python: test_hitl_rail_auto_confirm（确认部分）
func (s *ReactAgentInterruptSuite) TestConfirm_确认后继续执行() {
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

	sess := s.NewTestSession("react-interrupt-confirm")
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
	s.Equal(int32(1), writeInvokeCount.Load(), "write_file 应被执行 1 次")

	sess.PostRun(ctx)
}

// TestConfirm_拒绝后工具未执行 测试拒绝中断后工具不执行。
// 对齐 Python: chain_tools reject 部分
func (s *ReactAgentInterruptSuite) TestConfirm_拒绝后工具未执行() {
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

	sess := s.NewTestSession("react-interrupt-reject")
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

// TestConfirm_链式工具中断 测试链式工具调用中的中断。
// 对齐 Python: test_hitl_rail_chain_tools
func (s *ReactAgentInterruptSuite) TestConfirm_链式工具中断() {
	ctx := s.Ctx

	var readInvokeCount atomic.Int32
	var writeInvokeCount atomic.Int32
	readTool := newTrackedReadFileTool(&readInvokeCount)
	writeTool := newTrackedWriteFileTool(&writeInvokeCount)

	rail := interrupt.NewConfirmInterruptRail(interrupt.WithConfirmToolNames("write_file"))

	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("read_file", `{"filepath":"/tmp/test.txt"}`),
		mockllm.CreateToolCallResponse("write_file", `{"filepath":"/tmp/output.txt","content":"output"}`),
		mockllm.CreateTextResponse("操作完成"),
	)

	agent, err := s.NewDeepAgentForTest(ctx, hconfig.CreateDeepAgentParams{
		ToolInstances: []tool.Tool{readTool, writeTool},
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 5,
	})
	s.Require().NoError(err, "创建 DeepAgent 失败")

	sess := s.NewTestSession("react-interrupt-chain")
	s.Require().NoError(sess.PreRun(ctx), "Session PreRun 失败")

	result, err := agent.Invoke(ctx, map[string]any{"query": "读取然后写入"},
		agentinterfaces.WithSession(sess),
	)
	s.Require().NoError(err, "首次 Invoke 失败")
	s.Require().NotNil(result, "结果不应为 nil")

	interruptIDs, _ := testhelpers.AssertInterruptResult(s.T(), result, 1)

	s.GreaterOrEqual(readInvokeCount.Load(), int32(1), "read_file 应已执行")
	s.Equal(int32(0), writeInvokeCount.Load(), "write_file 被拦截不应执行")

	interactiveInput := testhelpers.ConfirmInterrupt(interruptIDs[0], false)
	result, err = agent.Invoke(ctx, map[string]any{"query": interactiveInput},
		agentinterfaces.WithSession(sess),
	)
	s.Require().NoError(err, "恢复 Invoke 失败")

	testhelpers.AssertAnswerResult(s.T(), result)
	s.GreaterOrEqual(writeInvokeCount.Load(), int32(1), "确认后 write_file 应执行")

	sess.PostRun(ctx)
}

// TestConfirm_并发工具全部确认 测试并发工具调用全部被确认。
// 对齐 Python: test_hitl_rail_concurrent_tools_all_confirmed
func (s *ReactAgentInterruptSuite) TestConfirm_并发工具全部确认() {
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

	sess := s.NewTestSession("react-interrupt-concurrent")
	s.Require().NoError(sess.PreRun(ctx), "Session PreRun 失败")

	result, err := agent.Invoke(ctx, map[string]any{"query": "写入两个文件"},
		agentinterfaces.WithSession(sess),
	)
	s.Require().NoError(err, "首次 Invoke 失败")
	s.Require().NotNil(result, "结果不应为 nil")

	interruptIDs, _ := testhelpers.AssertInterruptResult(s.T(), result, 2)
	s.Len(interruptIDs, 2, "应有 2 个 interrupt_id")

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

// TestConfirm_并发工具部分拒绝 测试并发工具调用部分拒绝。
// 对齐 Python: test_hitl_rail_concurrent_tools_partial_reject_one_round
func (s *ReactAgentInterruptSuite) TestConfirm_并发工具部分拒绝() {
	ctx := s.Ctx

	var writeInvokeCount atomic.Int32
	writeTool := newTrackedWriteFileTool(&writeInvokeCount)

	rail := interrupt.NewConfirmInterruptRail(interrupt.WithConfirmToolNames("write_file"))

	s.MockLLM.SetResponses(
		mockllm.CreateMultiToolCallResponse([]mockllm.ToolCallSpec{
			{Name: "write_file", ArgsJSON: `{"filepath":"/tmp/a.txt","content":"aa"}`},
			{Name: "write_file", ArgsJSON: `{"filepath":"/tmp/b.txt","content":"bb"}`},
		}),
		mockllm.CreateTextResponse("部分操作完成"),
	)

	agent, err := s.NewDeepAgentForTest(ctx, hconfig.CreateDeepAgentParams{
		ToolInstances: []tool.Tool{writeTool},
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 5,
	})
	s.Require().NoError(err, "创建 DeepAgent 失败")

	sess := s.NewTestSession("react-interrupt-partial-reject")
	s.Require().NoError(sess.PreRun(ctx), "Session PreRun 失败")

	result, err := agent.Invoke(ctx, map[string]any{"query": "写入两个文件"},
		agentinterfaces.WithSession(sess),
	)
	s.Require().NoError(err, "首次 Invoke 失败")
	s.Require().NotNil(result, "结果不应为 nil")

	interruptIDs, _ := testhelpers.AssertInterruptResult(s.T(), result, 2)
	s.Len(interruptIDs, 2, "应有 2 个 interrupt_id")

	interactiveInput := testhelpers.ConfirmInterrupt(interruptIDs[0], false)
	interactiveInput.UserInputs[interruptIDs[1]] = map[string]any{
		"approved": false,
		"feedback": "不允许写入此文件",
	}

	result, err = agent.Invoke(ctx, map[string]any{"query": interactiveInput},
		agentinterfaces.WithSession(sess),
	)
	s.Require().NoError(err, "恢复 Invoke 失败")

	testhelpers.AssertAnswerResult(s.T(), result)
	s.Equal(int32(1), writeInvokeCount.Load(), "只有确认的 write_file 应执行")

	sess.PostRun(ctx)
}

// TestConfirm_不同工具选择性拦截 测试只拦截列表中的工具，其他工具放行。
// 对齐 Python: test_hitl_rail_concurrent_tools_one_pass_one_interrupt
func (s *ReactAgentInterruptSuite) TestConfirm_不同工具选择性拦截() {
	ctx := s.Ctx

	var readInvokeCount atomic.Int32
	var writeInvokeCount atomic.Int32
	readTool := newTrackedReadFileTool(&readInvokeCount)
	writeTool := newTrackedWriteFileTool(&writeInvokeCount)

	rail := interrupt.NewConfirmInterruptRail(interrupt.WithConfirmToolNames("write_file"))

	s.MockLLM.SetResponses(
		mockllm.CreateMultiToolCallResponse([]mockllm.ToolCallSpec{
			{Name: "read_file", ArgsJSON: `{"filepath":"/tmp/input.txt"}`},
			{Name: "write_file", ArgsJSON: `{"filepath":"/tmp/output.txt","content":"output"}`},
		}),
		mockllm.CreateTextResponse("操作完成"),
	)

	agent, err := s.NewDeepAgentForTest(ctx, hconfig.CreateDeepAgentParams{
		ToolInstances: []tool.Tool{readTool, writeTool},
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 5,
	})
	s.Require().NoError(err, "创建 DeepAgent 失败")

	sess := s.NewTestSession("react-interrupt-selective")
	s.Require().NoError(sess.PreRun(ctx), "Session PreRun 失败")

	result, err := agent.Invoke(ctx, map[string]any{"query": "读取并写入"},
		agentinterfaces.WithSession(sess),
	)
	s.Require().NoError(err, "首次 Invoke 失败")
	s.Require().NotNil(result, "结果不应为 nil")

	interruptIDs, _ := testhelpers.AssertInterruptResult(s.T(), result, 1)

	s.GreaterOrEqual(readInvokeCount.Load(), int32(1), "read_file 应已执行（不在拦截列表）")
	s.Equal(int32(0), writeInvokeCount.Load(), "write_file 被拦截不应执行")

	interactiveInput := testhelpers.ConfirmInterrupt(interruptIDs[0], false)
	result, err = agent.Invoke(ctx, map[string]any{"query": interactiveInput},
		agentinterfaces.WithSession(sess),
	)
	s.Require().NoError(err, "恢复 Invoke 失败")

	testhelpers.AssertAnswerResult(s.T(), result)
	s.GreaterOrEqual(writeInvokeCount.Load(), int32(1), "确认后 write_file 应执行")

	sess.PostRun(ctx)
}

// TestConfirm_AutoConfirm跳过确认 测试 AutoConfirm 配置跳过确认流程。
// 对齐 Python: test_hitl_rail_auto_confirm
func (s *ReactAgentInterruptSuite) TestConfirm_AutoConfirm跳过确认() {
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

	sess := s.NewTestSession("react-interrupt-autoconfirm")
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

// ──────────────────────────── 非导出函数 ────────────────────────────

// newTrackedWriteFileTool 创建带 invoke_count 追踪的 write_file 工具。
func newTrackedWriteFileTool(invokeCount *atomic.Int32) tool.Tool {
	tc := tool.NewToolCardWithID("write_file", "write_file", "写入文件", nil, nil)
	t, _ := tool.NewMapFunction(tc,
		func(_ context.Context, _ map[string]any) (map[string]any, error) {
			invokeCount.Add(1)
			return map[string]any{"status": "ok", "tool": "write_file"}, nil
		}, nil,
	)
	return t
}

// newTrackedReadFileTool 创建带 invoke_count 追踪的 read_file 工具。
func newTrackedReadFileTool(invokeCount *atomic.Int32) tool.Tool {
	tc := tool.NewToolCardWithID("read_file", "read_file", "读取文件", nil, nil)
	t, _ := tool.NewMapFunction(tc,
		func(_ context.Context, _ map[string]any) (map[string]any, error) {
			invokeCount.Add(1)
			return map[string]any{"status": "ok", "tool": "read_file", "content": "file content"}, nil
		}, nil,
	)
	return t
}
