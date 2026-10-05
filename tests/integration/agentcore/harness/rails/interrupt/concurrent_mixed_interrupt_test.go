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
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
	"github.com/uapclaw/uapclaw-go/tests/integration/testhelpers"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ConcurrentMixedInterruptSuite HITL 并发工具混合中断集成测试套件。
//
// 覆盖 3+ 并发工具中断、选择性放行、拒绝后 AutoConfirm 存储、
// 多轮中断恢复等场景。
//
// 对齐 Python: tests/system_tests/agent/react_agent/interrupt/test_react_agent_interrupt_concurrent_tools.py
type ConcurrentMixedInterruptSuite struct {
	isuite.AgentSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestConcurrentMixedInterruptSuite 运行并发混合中断测试套件
func TestConcurrentMixedInterruptSuite(t *testing.T) {
	suite.Run(t, new(ConcurrentMixedInterruptSuite))
}

// TestConcurrent_三个并发工具全部确认 测试 3 个并发 write_file 调用全部被确认。
// 对齐 Python: test_hitl_rail_concurrent_tools_all_confirmed（扩展为 3 个工具）
func (s *ConcurrentMixedInterruptSuite) TestConcurrent_三个并发工具全部确认() {
	ctx := s.Ctx

	var writeInvokeCount atomic.Int32
	writeTool := newConcurrentWriteFileTool(&writeInvokeCount)

	rail := interrupt.NewConfirmInterruptRail(interrupt.WithConfirmToolNames("write_file"))

	s.MockLLM.SetResponses(
		mockllm.CreateMultiToolCallResponse([]mockllm.ToolCallSpec{
			{Name: "write_file", ArgsJSON: `{"filepath":"/tmp/a.txt","content":"aa"}`},
			{Name: "write_file", ArgsJSON: `{"filepath":"/tmp/b.txt","content":"bb"}`},
			{Name: "write_file", ArgsJSON: `{"filepath":"/tmp/c.txt","content":"cc"}`},
		}),
		mockllm.CreateTextResponse("三个文件已写入"),
	)

	agent, err := s.NewDeepAgentForTest(ctx, hconfig.CreateDeepAgentParams{
		ToolInstances: []tool.Tool{writeTool},
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 5,
	})
	s.Require().NoError(err, "创建 DeepAgent 失败")

	sess := s.NewTestSession("concurrent-3-confirm")
	s.Require().NoError(sess.PreRun(ctx), "Session PreRun 失败")

	// 触发 3 个并发中断
	result, err := agent.Invoke(ctx, map[string]any{"query": "写入三个文件"},
		agentinterfaces.WithSession(sess),
	)
	s.Require().NoError(err, "首次 Invoke 失败")
	s.Require().NotNil(result, "结果不应为 nil")

	interruptIDs, _ := testhelpers.AssertInterruptResult(s.T(), result, 3)
	s.Len(interruptIDs, 3, "应有 3 个 interrupt_id")

	// 全部确认
	interactiveInput := testhelpers.ConfirmInterrupt(interruptIDs[0], false)
	interactiveInput.UserInputs[interruptIDs[1]] = map[string]any{
		"approved": true, "feedback": "Confirm", "auto_confirm": false,
	}
	interactiveInput.UserInputs[interruptIDs[2]] = map[string]any{
		"approved": true, "feedback": "Confirm", "auto_confirm": false,
	}

	result, err = agent.Invoke(ctx, map[string]any{"query": interactiveInput},
		agentinterfaces.WithSession(sess),
	)
	s.Require().NoError(err, "恢复 Invoke 失败")

	testhelpers.AssertAnswerResult(s.T(), result)
	s.Equal(int32(3), writeInvokeCount.Load(), "确认后三个 write_file 应全部执行")

	sess.PostRun(ctx)
}

// TestConcurrent_三个并发工具部分拒绝 测试 3 个并发 write_file 调用确认 2 个拒绝 1 个。
func (s *ConcurrentMixedInterruptSuite) TestConcurrent_三个并发工具部分拒绝() {
	ctx := s.Ctx

	var writeInvokeCount atomic.Int32
	writeTool := newConcurrentWriteFileTool(&writeInvokeCount)

	rail := interrupt.NewConfirmInterruptRail(interrupt.WithConfirmToolNames("write_file"))

	s.MockLLM.SetResponses(
		mockllm.CreateMultiToolCallResponse([]mockllm.ToolCallSpec{
			{Name: "write_file", ArgsJSON: `{"filepath":"/tmp/a.txt","content":"aa"}`},
			{Name: "write_file", ArgsJSON: `{"filepath":"/tmp/b.txt","content":"bb"}`},
			{Name: "write_file", ArgsJSON: `{"filepath":"/tmp/c.txt","content":"cc"}`},
		}),
		mockllm.CreateTextResponse("部分操作完成"),
	)

	agent, err := s.NewDeepAgentForTest(ctx, hconfig.CreateDeepAgentParams{
		ToolInstances: []tool.Tool{writeTool},
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 5,
	})
	s.Require().NoError(err, "创建 DeepAgent 失败")

	sess := s.NewTestSession("concurrent-3-partial-reject")
	s.Require().NoError(sess.PreRun(ctx), "Session PreRun 失败")

	result, err := agent.Invoke(ctx, map[string]any{"query": "写入三个文件"},
		agentinterfaces.WithSession(sess),
	)
	s.Require().NoError(err, "首次 Invoke 失败")

	interruptIDs, _ := testhelpers.AssertInterruptResult(s.T(), result, 3)
	s.Len(interruptIDs, 3, "应有 3 个 interrupt_id")

	// 确认 2 个，拒绝 1 个
	interactiveInput := testhelpers.ConfirmInterrupt(interruptIDs[0], false)
	interactiveInput.UserInputs[interruptIDs[1]] = map[string]any{
		"approved": true, "feedback": "Confirm",
	}
	interactiveInput.UserInputs[interruptIDs[2]] = map[string]any{
		"approved": false, "feedback": "不允许写入 c.txt",
	}

	result, err = agent.Invoke(ctx, map[string]any{"query": interactiveInput},
		agentinterfaces.WithSession(sess),
	)
	s.Require().NoError(err, "恢复 Invoke 失败")

	testhelpers.AssertAnswerResult(s.T(), result)
	s.Equal(int32(2), writeInvokeCount.Load(), "只有确认的 2 个 write_file 应执行")

	sess.PostRun(ctx)
}

// TestConcurrent_三并发混合读写 测试 3 个并发工具中 read_file 放行 + 2 个 write_file 中断。
func (s *ConcurrentMixedInterruptSuite) TestConcurrent_三并发混合读写() {
	ctx := s.Ctx

	var readInvokeCount atomic.Int32
	var writeInvokeCount atomic.Int32
	readTool := newConcurrentReadFileTool(&readInvokeCount)
	writeTool := newConcurrentWriteFileTool(&writeInvokeCount)

	rail := interrupt.NewConfirmInterruptRail(interrupt.WithConfirmToolNames("write_file"))

	s.MockLLM.SetResponses(
		mockllm.CreateMultiToolCallResponse([]mockllm.ToolCallSpec{
			{Name: "read_file", ArgsJSON: `{"filepath":"/tmp/input.txt"}`},
			{Name: "write_file", ArgsJSON: `{"filepath":"/tmp/a.txt","content":"aa"}`},
			{Name: "write_file", ArgsJSON: `{"filepath":"/tmp/b.txt","content":"bb"}`},
		}),
		mockllm.CreateTextResponse("操作完成"),
	)

	agent, err := s.NewDeepAgentForTest(ctx, hconfig.CreateDeepAgentParams{
		ToolInstances: []tool.Tool{readTool, writeTool},
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 5,
	})
	s.Require().NoError(err, "创建 DeepAgent 失败")

	sess := s.NewTestSession("concurrent-3-mixed-rw")
	s.Require().NoError(sess.PreRun(ctx), "Session PreRun 失败")

	result, err := agent.Invoke(ctx, map[string]any{"query": "读取并写入"},
		agentinterfaces.WithSession(sess),
	)
	s.Require().NoError(err, "首次 Invoke 失败")

	// read_file 不被拦截，write_file 被拦截 → 2 个 interrupt_id
	interruptIDs, _ := testhelpers.AssertInterruptResult(s.T(), result, 2)
	s.Len(interruptIDs, 2, "应有 2 个 interrupt_id（write_file）")
	s.GreaterOrEqual(readInvokeCount.Load(), int32(1), "read_file 应已执行（不在拦截列表）")

	// 确认所有 write_file
	interactiveInput := testhelpers.ConfirmInterrupt(interruptIDs[0], false)
	interactiveInput.UserInputs[interruptIDs[1]] = map[string]any{
		"approved": true, "feedback": "Confirm",
	}

	result, err = agent.Invoke(ctx, map[string]any{"query": interactiveInput},
		agentinterfaces.WithSession(sess),
	)
	s.Require().NoError(err, "恢复 Invoke 失败")

	testhelpers.AssertAnswerResult(s.T(), result)
	s.Equal(int32(2), writeInvokeCount.Load(), "确认后两个 write_file 应执行")

	sess.PostRun(ctx)
}

// TestConcurrent_四并发混合选择性放行 测试 4 个并发工具中 2x read_file 放行 + 2x write_file 中断。
func (s *ConcurrentMixedInterruptSuite) TestConcurrent_四并发混合选择性放行() {
	ctx := s.Ctx

	var readInvokeCount atomic.Int32
	var writeInvokeCount atomic.Int32
	readTool := newConcurrentReadFileTool(&readInvokeCount)
	writeTool := newConcurrentWriteFileTool(&writeInvokeCount)

	rail := interrupt.NewConfirmInterruptRail(interrupt.WithConfirmToolNames("write_file"))

	s.MockLLM.SetResponses(
		mockllm.CreateMultiToolCallResponse([]mockllm.ToolCallSpec{
			{Name: "read_file", ArgsJSON: `{"filepath":"/tmp/a.txt"}`},
			{Name: "read_file", ArgsJSON: `{"filepath":"/tmp/b.txt"}`},
			{Name: "write_file", ArgsJSON: `{"filepath":"/tmp/out1.txt","content":"out1"}`},
			{Name: "write_file", ArgsJSON: `{"filepath":"/tmp/out2.txt","content":"out2"}`},
		}),
		mockllm.CreateTextResponse("操作完成"),
	)

	agent, err := s.NewDeepAgentForTest(ctx, hconfig.CreateDeepAgentParams{
		ToolInstances: []tool.Tool{readTool, writeTool},
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 5,
	})
	s.Require().NoError(err, "创建 DeepAgent 失败")

	sess := s.NewTestSession("concurrent-4-selective")
	s.Require().NoError(sess.PreRun(ctx), "Session PreRun 失败")

	result, err := agent.Invoke(ctx, map[string]any{"query": "读取两个文件并写入两个文件"},
		agentinterfaces.WithSession(sess),
	)
	s.Require().NoError(err, "首次 Invoke 失败")

	interruptIDs, _ := testhelpers.AssertInterruptResult(s.T(), result, 2)
	s.Len(interruptIDs, 2, "应有 2 个 interrupt_id（仅 write_file）")
	s.GreaterOrEqual(readInvokeCount.Load(), int32(2), "两个 read_file 应已执行")

	// 确认 write_file
	interactiveInput := testhelpers.ConfirmInterrupt(interruptIDs[0], false)
	interactiveInput.UserInputs[interruptIDs[1]] = map[string]any{
		"approved": true, "feedback": "Confirm",
	}

	result, err = agent.Invoke(ctx, map[string]any{"query": interactiveInput},
		agentinterfaces.WithSession(sess),
	)
	s.Require().NoError(err, "恢复 Invoke 失败")

	testhelpers.AssertAnswerResult(s.T(), result)
	s.Equal(int32(2), writeInvokeCount.Load(), "确认后两个 write_file 应执行")

	sess.PostRun(ctx)
}

// TestConcurrent_拒绝后不影响后续Invoke 测试拒绝一个工具后，后续 Invoke 不受影响。
func (s *ConcurrentMixedInterruptSuite) TestConcurrent_拒绝后不影响后续Invoke() {
	ctx := s.Ctx

	var writeInvokeCount atomic.Int32
	writeTool := newConcurrentWriteFileTool(&writeInvokeCount)

	rail := interrupt.NewConfirmInterruptRail(interrupt.WithConfirmToolNames("write_file"))

	// 第一轮：1 个 write_file 被拒绝
	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("write_file", `{"filepath":"/tmp/denied.txt","content":"no"}`),
		mockllm.CreateTextResponse("操作被拒绝"),
		// 第二轮：1 个 write_file 被确认
		mockllm.CreateToolCallResponse("write_file", `{"filepath":"/tmp/approved.txt","content":"yes"}`),
		mockllm.CreateTextResponse("文件已写入"),
	)

	agent, err := s.NewDeepAgentForTest(ctx, hconfig.CreateDeepAgentParams{
		ToolInstances: []tool.Tool{writeTool},
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 5,
	})
	s.Require().NoError(err, "创建 DeepAgent 失败")

	sess := s.NewTestSession("concurrent-reject-then-confirm")
	s.Require().NoError(sess.PreRun(ctx), "Session PreRun 失败")

	// 第一轮：拒绝
	result, err := agent.Invoke(ctx, map[string]any{"query": "写入文件"},
		agentinterfaces.WithSession(sess),
	)
	s.Require().NoError(err, "首次 Invoke 失败")

	interruptIDs, _ := testhelpers.AssertInterruptResult(s.T(), result, 1)
	interactiveInput := testhelpers.RejectInterrupt(interruptIDs[0], "不允许写入")
	result, err = agent.Invoke(ctx, map[string]any{"query": interactiveInput},
		agentinterfaces.WithSession(sess),
	)
	s.Require().NoError(err, "拒绝恢复 Invoke 失败")
	testhelpers.AssertAnswerResult(s.T(), result)
	s.Equal(int32(0), writeInvokeCount.Load(), "拒绝后 write_file 不应执行")

	// 第二轮：确认（新 Invoke）
	result, err = agent.Invoke(ctx, map[string]any{"query": "再次写入文件"},
		agentinterfaces.WithSession(sess),
	)
	s.Require().NoError(err, "第二次 Invoke 失败")

	interruptIDs2, _ := testhelpers.AssertInterruptResult(s.T(), result, 1)
	interactiveInput2 := testhelpers.ConfirmInterrupt(interruptIDs2[0], false)
	result, err = agent.Invoke(ctx, map[string]any{"query": interactiveInput2},
		agentinterfaces.WithSession(sess),
	)
	s.Require().NoError(err, "确认恢复 Invoke 失败")
	testhelpers.AssertAnswerResult(s.T(), result)
	s.Equal(int32(1), writeInvokeCount.Load(), "确认后 write_file 应执行")

	sess.PostRun(ctx)
}

// TestConcurrent_确认带AutoConfirm存储 测试确认时带 auto_confirm=true 后后续调用自动放行。
func (s *ConcurrentMixedInterruptSuite) TestConcurrent_确认带AutoConfirm存储() {
	ctx := s.Ctx

	var writeInvokeCount atomic.Int32
	writeTool := newConcurrentWriteFileTool(&writeInvokeCount)

	rail := interrupt.NewConfirmInterruptRail(interrupt.WithConfirmToolNames("write_file"))

	// 第一轮和第二轮各一个 write_file
	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("write_file", `{"filepath":"/tmp/first.txt","content":"first"}`),
		mockllm.CreateTextResponse("第一次写入完成"),
		mockllm.CreateToolCallResponse("write_file", `{"filepath":"/tmp/second.txt","content":"second"}`),
		mockllm.CreateTextResponse("第二次写入完成"),
	)

	agent, err := s.NewDeepAgentForTest(ctx, hconfig.CreateDeepAgentParams{
		ToolInstances: []tool.Tool{writeTool},
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 5,
	})
	s.Require().NoError(err, "创建 DeepAgent 失败")

	sess := s.NewTestSession("concurrent-autoconfirm")
	s.Require().NoError(sess.PreRun(ctx), "Session PreRun 失败")

	// 第一轮：触发中断 → 确认并设置 auto_confirm=true
	result, err := agent.Invoke(ctx, map[string]any{"query": "写入文件"},
		agentinterfaces.WithSession(sess),
	)
	s.Require().NoError(err, "首次 Invoke 失败")

	interruptIDs, _ := testhelpers.AssertInterruptResult(s.T(), result, 1)
	interactiveInput := testhelpers.ConfirmInterrupt(interruptIDs[0], true) // auto_confirm=true
	result, err = agent.Invoke(ctx, map[string]any{"query": interactiveInput},
		agentinterfaces.WithSession(sess),
	)
	s.Require().NoError(err, "确认恢复 Invoke 失败")
	testhelpers.AssertAnswerResult(s.T(), result)
	s.Equal(int32(1), writeInvokeCount.Load(), "确认后 write_file 应执行 1 次")

	// 验证 session 中已存储 auto_confirm key
	autoConfirmVal, stateErr := sess.GetState(state.StringKey(saschema.InterruptAutoConfirmKey))
	s.Require().NoError(stateErr, "获取 auto_confirm 状态不应失败")
	s.Require().NotNil(autoConfirmVal, "auto_confirm 配置不应为 nil")
	autoConfirmMap, ok := autoConfirmVal.(map[string]any)
	s.Require().True(ok, "auto_confirm 应为 map[string]any 类型")
	s.True(autoConfirmMap["write_file"].(bool), "write_file auto_confirm 应为 true")

	// 第二轮：auto_confirm 生效，不再触发中断
	result, err = agent.Invoke(ctx, map[string]any{"query": "再次写入文件"},
		agentinterfaces.WithSession(sess),
	)
	s.Require().NoError(err, "第二次 Invoke 失败")
	s.Require().NotNil(result, "第二次结果不应为 nil")

	testhelpers.AssertAnswerResult(s.T(), result)
	s.Equal(int32(2), writeInvokeCount.Load(), "auto_confirm 生效后 write_file 应再次执行")

	sess.PostRun(ctx)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// newConcurrentWriteFileTool 创建带 invoke_count 追踪的 write_file 工具。
func newConcurrentWriteFileTool(invokeCount *atomic.Int32) tool.Tool {
	tc := tool.NewToolCardWithID("write_file", "write_file", "写入文件", nil, nil)
	t, _ := tool.NewMapFunction(tc,
		func(_ context.Context, _ map[string]any) (map[string]any, error) {
			invokeCount.Add(1)
			return map[string]any{"status": "ok", "tool": "write_file"}, nil
		}, nil,
	)
	return t
}

// newConcurrentReadFileTool 创建带 invoke_count 追踪的 read_file 工具。
func newConcurrentReadFileTool(invokeCount *atomic.Int32) tool.Tool {
	tc := tool.NewToolCardWithID("read_file", "read_file", "读取文件", nil, nil)
	t, _ := tool.NewMapFunction(tc,
		func(_ context.Context, _ map[string]any) (map[string]any, error) {
			invokeCount.Add(1)
			return map[string]any{"status": "ok", "tool": "read_file", "content": "file content"}, nil
		}, nil,
	)
	return t
}
