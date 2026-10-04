//go:build integration

package interrupt_test

import (
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/suite"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
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

// ReactAgentAutoConfirmSuite ReActAgent AutoConfirm 自动确认测试套件。
// 对齐 Python: tests/system_tests/harness/rail/test_hitl_rail.py
//
// 测试 AutoConfirm 机制：
//  1. session state 预设 auto_confirm → 工具调用自动放行
//  2. 部分工具自动确认 → 未确认工具仍触发中断
//  3. 确认时带 auto_confirm: true → 后续调用自动放行
//  4. WithAutoConfirmKeyFn 自定义键函数 → 细粒度自动确认
//
// 运行方式: go test -tags="sqlite_fts5 test integration" ./tests/integration/agentcore/harness/rails/interrupt/...
type ReactAgentAutoConfirmSuite struct {
	isuite.AgentSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestReactAgentAutoConfirmSuite 运行 ReActAgent AutoConfirm 自动确认测试套件
func TestReactAgentAutoConfirmSuite(t *testing.T) {
	suite.Run(t, new(ReactAgentAutoConfirmSuite))
}

// TestAutoConfirm_单工具自动确认 测试预设 auto_confirm 后单工具自动放行。
// 对齐 Python: test_hitl_rail_auto_confirm（单工具场景）
func (s *ReactAgentAutoConfirmSuite) TestAutoConfirm_单工具自动确认() {
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

	sess := s.NewTestSession("autoconfirm-single")
	s.Require().NoError(sess.PreRun(ctx), "Session PreRun 失败")

	// 预设 auto_confirm：write_file 自动确认
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

// TestAutoConfirm_多工具部分自动确认 测试部分工具自动确认、其余工具仍触发中断。
// 对齐 Python: test_hitl_rail_auto_confirm（多工具部分确认场景）
func (s *ReactAgentAutoConfirmSuite) TestAutoConfirm_多工具部分自动确认() {
	ctx := s.Ctx

	var readInvokeCount atomic.Int32
	var writeInvokeCount atomic.Int32
	readTool := newTrackedReadFileTool(&readInvokeCount)
	writeTool := newTrackedWriteFileTool(&writeInvokeCount)

	rail := interrupt.NewConfirmInterruptRail(interrupt.WithConfirmToolNames("read_file", "write_file"))

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

	sess := s.NewTestSession("autoconfirm-partial")
	s.Require().NoError(sess.PreRun(ctx), "Session PreRun 失败")

	// 只对 read_file 自动确认，write_file 仍需手动确认
	sess.UpdateState(map[string]any{
		saschema.InterruptAutoConfirmKey: map[string]any{"read_file": true},
	})

	// 第 1 步：Invoke 触发 write_file 中断（read_file 自动放行）
	result, err := agent.Invoke(ctx, map[string]any{"query": "读取并写入"},
		agentinterfaces.WithSession(sess),
	)
	s.Require().NoError(err, "首次 Invoke 失败")
	s.Require().NotNil(result, "结果不应为 nil")

	interruptIDs, _ := testhelpers.AssertInterruptResult(s.T(), result, 1)

	// read_file 应已执行（自动确认放行）
	s.GreaterOrEqual(readInvokeCount.Load(), int32(1), "read_file 应已执行（自动确认放行）")
	// write_file 不应执行（被拦截）
	s.Equal(int32(0), writeInvokeCount.Load(), "write_file 被拦截不应执行")

	// 第 2 步：确认 write_file 中断并恢复
	interactiveInput := testhelpers.ConfirmInterrupt(interruptIDs[0], false)
	result, err = agent.Invoke(ctx, map[string]any{"query": interactiveInput},
		agentinterfaces.WithSession(sess),
	)
	s.Require().NoError(err, "恢复 Invoke 失败")
	s.Require().NotNil(result, "恢复结果不应为 nil")

	testhelpers.AssertAnswerResult(s.T(), result)
	s.GreaterOrEqual(writeInvokeCount.Load(), int32(1), "确认后 write_file 应执行")

	sess.PostRun(ctx)
}

// TestAutoConfirm_带auto_confirm标志的确认 测试确认时设置 auto_confirm: true，
// 后续相同工具调用自动放行。
// 对齐 Python: test_hitl_rail_auto_confirm（auto_confirm 标志持久化场景）
func (s *ReactAgentAutoConfirmSuite) TestAutoConfirm_带auto_confirm标志的确认() {
	ctx := s.Ctx

	var writeInvokeCount atomic.Int32
	writeTool := newTrackedWriteFileTool(&writeInvokeCount)

	rail := interrupt.NewConfirmInterruptRail(interrupt.WithConfirmToolNames("write_file"))

	// 第一次：触发中断 → 确认带 auto_confirm
	// 第二次：同一工具应自动放行
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

	sess := s.NewTestSession("autoconfirm-flag")
	s.Require().NoError(sess.PreRun(ctx), "Session PreRun 失败")

	// 第 1 步：首次 Invoke 触发中断
	result, err := agent.Invoke(ctx, map[string]any{"query": "写入文件"},
		agentinterfaces.WithSession(sess),
	)
	s.Require().NoError(err, "首次 Invoke 失败")
	s.Require().NotNil(result, "结果不应为 nil")

	interruptIDs, _ := testhelpers.AssertInterruptResult(s.T(), result, 1)
	s.Require().NotEmpty(interruptIDs, "应有至少一个 interrupt_id")

	// 第 2 步：确认中断，并设置 auto_confirm: true
	interactiveInput := testhelpers.ConfirmInterrupt(interruptIDs[0], true)
	result, err = agent.Invoke(ctx, map[string]any{"query": interactiveInput},
		agentinterfaces.WithSession(sess),
	)
	s.Require().NoError(err, "确认恢复 Invoke 失败")
	s.Require().NotNil(result, "确认恢复结果不应为 nil")

	testhelpers.AssertAnswerResult(s.T(), result)
	s.Equal(int32(1), writeInvokeCount.Load(), "首次确认后 write_file 应执行 1 次")

	// 第 3 步：再次 Invoke 相同工具，应自动放行（不再触发中断）
	result, err = agent.Invoke(ctx, map[string]any{"query": "再次写入文件"},
		agentinterfaces.WithSession(sess),
	)
	s.Require().NoError(err, "第二次 Invoke 失败")
	s.Require().NotNil(result, "第二次结果不应为 nil")

	testhelpers.AssertAnswerResult(s.T(), result)
	s.Equal(int32(2), writeInvokeCount.Load(), "auto_confirm 生效后 write_file 应再次执行")

	sess.PostRun(ctx)
}

// TestAutoConfirm_回调函数验证 测试 WithAutoConfirmKeyFn 自定义键函数。
// 通过回调函数返回 auto-confirm 键，工具自动确认无需手动干预。
// 对齐 Python: ConfirmInterruptRail._get_auto_confirm_key(tool_call) 自定义场景
func (s *ReactAgentAutoConfirmSuite) TestAutoConfirm_回调函数验证() {
	ctx := s.Ctx

	var writeInvokeCount atomic.Int32
	writeTool := newTrackedWriteFileTool(&writeInvokeCount)

	// 使用自定义 AutoConfirmKeyFn，返回工具名作为 key
	rail := interrupt.NewConfirmInterruptRail(
		interrupt.WithConfirmToolNames("write_file"),
		interrupt.WithAutoConfirmKeyFn(func(toolCall *llmschema.ToolCall) string {
			return toolCall.Name
		}),
	)

	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("write_file", `{"filepath":"/tmp/custom_fn.txt","content":"hello"}`),
		mockllm.CreateTextResponse("文件已写入"),
	)

	agent, err := s.NewDeepAgentForTest(ctx, hconfig.CreateDeepAgentParams{
		ToolInstances: []tool.Tool{writeTool},
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 5,
	})
	s.Require().NoError(err, "创建 DeepAgent 失败")

	sess := s.NewTestSession("autoconfirm-callback")
	s.Require().NoError(sess.PreRun(ctx), "Session PreRun 失败")

	// 预设 auto_confirm：通过自定义键函数返回的 key 匹配
	sess.UpdateState(map[string]any{
		saschema.InterruptAutoConfirmKey: map[string]any{"write_file": true},
	})

	// 自定义键函数下 Invoke 应正常完成
	result, err := agent.Invoke(ctx, map[string]any{"query": "写入文件"},
		agentinterfaces.WithSession(sess),
	)
	s.Require().NoError(err, "AutoConfirmKeyFn 下应正常完成")
	s.Require().NotNil(result, "应返回结果")

	testhelpers.AssertAnswerResult(s.T(), result)
	s.GreaterOrEqual(writeInvokeCount.Load(), int32(1), "AutoConfirmKeyFn 下 write_file 应执行")

	sess.PostRun(ctx)
}
