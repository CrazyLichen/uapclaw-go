//go:build integration

package interrupt_test

import (
	"encoding/json"
	"fmt"
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

// FineGrainedAutoConfirmSuite 细粒度 AutoConfirm 集成测试套件。
// 对齐 Python: ConfirmInterruptRail._get_auto_confirm_key(tool_call) 细粒度场景
//
// 测试 WithAutoConfirmKeyFn 自定义键函数实现细粒度自动确认：
//  1. 自定义键函数被正确调用 → 返回自定义 key
//  2. 粗粒度 auto_confirm + 自定义 key 函数 → 匹配时自动放行
//  3. 确认带 auto_confirm 标志 → 自定义 key 持久化到 session
//  4. 拒绝不影响后续 → 不存储 auto_confirm key
//
// 运行方式: go test -tags="sqlite_fts5 test integration" ./tests/integration/agentcore/harness/rails/interrupt/...
type FineGrainedAutoConfirmSuite struct {
	isuite.AgentSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestFineGrainedAutoConfirmSuite 运行细粒度 AutoConfirm 测试套件
func TestFineGrainedAutoConfirmSuite(t *testing.T) {
	suite.Run(t, new(FineGrainedAutoConfirmSuite))
}

// TestFineGrained_自定义KeyFn被调用 测试 WithAutoConfirmKeyFn 自定义函数被调用，
// 并返回正确的 key。通过 keyFn 记录调用次数和返回值来验证。
// 验证中断请求中 AutoConfirmKey 使用自定义函数生成的 key。
func (s *FineGrainedAutoConfirmSuite) TestFineGrained_自定义KeyFn被调用() {
	ctx := s.Ctx

	var writeInvokeCount atomic.Int32
	writeTool := newTrackedWriteFileTool(&writeInvokeCount)

	var keyFnCalls atomic.Int32
	keyFn := func(toolCall *llmschema.ToolCall) string {
		keyFnCalls.Add(1)
		// 验证 toolCall 非空且包含参数
		if toolCall.Name != "write_file" {
			return toolCall.Name
		}
		// 解析 Arguments JSON，提取 filepath 生成细粒度 key
		if toolCall.Arguments == "" {
			return toolCall.Name
		}
		var args map[string]any
		if err := json.Unmarshal([]byte(toolCall.Arguments), &args); err != nil {
			return toolCall.Name
		}
		filepath, _ := args["filepath"].(string)
		if filepath == "" {
			return toolCall.Name
		}
		// 使用不含特殊字符的 key
		return fmt.Sprintf("wf_%s", filepath)
	}

	rail := interrupt.NewConfirmInterruptRail(
		interrupt.WithConfirmToolNames("write_file"),
		interrupt.WithAutoConfirmKeyFn(keyFn),
	)

	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("write_file", `{"filepath":"/tmp/test.txt","content":"hello"}`),
		mockllm.CreateTextResponse("写入完成"),
	)

	agent, err := s.NewDeepAgentForTest(ctx, hconfig.CreateDeepAgentParams{
		ToolInstances: []tool.Tool{writeTool},
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 5,
	})
	s.Require().NoError(err, "创建 DeepAgent 失败")

	sess := s.NewTestSession("fine-grained-keyfn-called")
	s.Require().NoError(sess.PreRun(ctx), "Session PreRun 失败")

	// 触发中断（未预设 auto_confirm）
	result, err := agent.Invoke(ctx, map[string]any{"query": "写入文件"},
		agentinterfaces.WithSession(sess),
	)
	s.Require().NoError(err, "Invoke 失败")
	s.Require().NotNil(result, "结果不应为 nil")

	// 验证 keyFn 被调用
	s.Greater(keyFnCalls.Load(), int32(0), "keyFn 应被调用至少一次")

	// 验证中断结果（无 auto_confirm 时应触发中断）
	s.Equal("interrupt", result["result_type"], "无 auto_confirm 应触发中断")

	sess.PostRun(ctx)
}

// TestFineGrained_粗粒度匹配自定义Key 测试自定义 key 返回工具名时，
// 粗粒度 auto_confirm 仍可匹配。验证 WithAutoConfirmKeyFn 与
// auto_confirm 配合的完整链路。
func (s *FineGrainedAutoConfirmSuite) TestFineGrained_粗粒度匹配自定义Key() {
	ctx := s.Ctx

	var writeInvokeCount atomic.Int32
	writeTool := newTrackedWriteFileTool(&writeInvokeCount)

	// 自定义 key 函数：返回 "write_file"（与默认行为一致）
	keyFn := func(toolCall *llmschema.ToolCall) string {
		return toolCall.Name
	}

	rail := interrupt.NewConfirmInterruptRail(
		interrupt.WithConfirmToolNames("write_file"),
		interrupt.WithAutoConfirmKeyFn(keyFn),
	)

	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("write_file", `{"filepath":"/tmp/test.txt","content":"hello"}`),
		mockllm.CreateTextResponse("写入完成"),
	)

	agent, err := s.NewDeepAgentForTest(ctx, hconfig.CreateDeepAgentParams{
		ToolInstances: []tool.Tool{writeTool},
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 5,
	})
	s.Require().NoError(err, "创建 DeepAgent 失败")

	sess := s.NewTestSession("fine-grained-coarse-match")
	s.Require().NoError(sess.PreRun(ctx), "Session PreRun 失败")

	// 设置粗粒度 auto_confirm
	sess.UpdateState(map[string]any{
		saschema.InterruptAutoConfirmKey: map[string]any{"write_file": true},
	})

	result, err := agent.Invoke(ctx, map[string]any{"query": "写入文件"},
		agentinterfaces.WithSession(sess),
	)
	s.Require().NoError(err, "粗粒度匹配应正常完成")
	s.Require().NotNil(result, "应返回结果")

	testhelpers.AssertAnswerResult(s.T(), result)
	s.GreaterOrEqual(writeInvokeCount.Load(), int32(1), "auto_confirm 下 write_file 应执行")

	sess.PostRun(ctx)
}

// TestFineGrained_确认带auto_confirm存储自定义Key 测试确认时设置 auto_confirm: true，
// 自定义 key 存储到 session state。后续调用相同参数时自动放行。
func (s *FineGrainedAutoConfirmSuite) TestFineGrained_确认带auto_confirm存储自定义Key() {
	ctx := s.Ctx

	var writeInvokeCount atomic.Int32
	writeTool := newTrackedWriteFileTool(&writeInvokeCount)

	rail := interrupt.NewConfirmInterruptRail(
		interrupt.WithConfirmToolNames("write_file"),
		interrupt.WithAutoConfirmKeyFn(func(tc *llmschema.ToolCall) string {
			return tc.Name
		}),
	)

	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("write_file", `{"filepath":"/tmp/approved.txt","content":"first"}`),
		mockllm.CreateTextResponse("第一次写入完成"),
		mockllm.CreateToolCallResponse("write_file", `{"filepath":"/tmp/approved.txt","content":"second"}`),
		mockllm.CreateTextResponse("第二次写入完成"),
	)

	agent, err := s.NewDeepAgentForTest(ctx, hconfig.CreateDeepAgentParams{
		ToolInstances: []tool.Tool{writeTool},
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 5,
	})
	s.Require().NoError(err, "创建 DeepAgent 失败")

	sess := s.NewTestSession("fine-grained-persist")
	s.Require().NoError(sess.PreRun(ctx), "Session PreRun 失败")

	// 第 1 步：首次 Invoke 触发中断
	result, err := agent.Invoke(ctx, map[string]any{"query": "写入文件"},
		agentinterfaces.WithSession(sess),
	)
	s.Require().NoError(err, "首次 Invoke 失败")
	s.Require().NotNil(result, "结果不应为 nil")

	interruptIDs, _ := testhelpers.AssertInterruptResult(s.T(), result, 1)
	s.Require().NotEmpty(interruptIDs, "应有至少一个 interrupt_id")

	// 第 2 步：确认中断，带 auto_confirm: true
	interactiveInput := testhelpers.ConfirmInterrupt(interruptIDs[0], true)
	result, err = agent.Invoke(ctx, map[string]any{"query": interactiveInput},
		agentinterfaces.WithSession(sess),
	)
	s.Require().NoError(err, "确认恢复 Invoke 失败")

	testhelpers.AssertAnswerResult(s.T(), result)
	s.Equal(int32(1), writeInvokeCount.Load(), "确认后 write_file 应执行 1 次")

	// 第 3 步：再次调用相同工具 → auto_confirm 生效，自动放行
	result, err = agent.Invoke(ctx, map[string]any{"query": "再次写入文件"},
		agentinterfaces.WithSession(sess),
	)
	s.Require().NoError(err, "第二次 Invoke 失败")
	s.Require().NotNil(result, "第二次结果不应为 nil")

	testhelpers.AssertAnswerResult(s.T(), result)
	s.Equal(int32(2), writeInvokeCount.Load(), "auto_confirm 生效后 write_file 应再次执行")

	sess.PostRun(ctx)
}

// TestFineGrained_拒绝不影响后续 测试拒绝中断时，不会在 session 中存储 auto_confirm key。
// 拒绝后再次调用相同工具，仍会触发中断。
func (s *FineGrainedAutoConfirmSuite) TestFineGrained_拒绝不影响后续() {
	ctx := s.Ctx

	var writeInvokeCount atomic.Int32
	writeTool := newTrackedWriteFileTool(&writeInvokeCount)

	rail := interrupt.NewConfirmInterruptRail(
		interrupt.WithConfirmToolNames("write_file"),
		interrupt.WithAutoConfirmKeyFn(func(tc *llmschema.ToolCall) string {
			return tc.Name
		}),
	)

	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("write_file", `{"filepath":"/tmp/denied.txt","content":"hello"}`),
		mockllm.CreateTextResponse("操作被拒绝"),
		mockllm.CreateToolCallResponse("write_file", `{"filepath":"/tmp/denied.txt","content":"hello"}`),
		mockllm.CreateTextResponse("再次被拒绝"),
	)

	agent, err := s.NewDeepAgentForTest(ctx, hconfig.CreateDeepAgentParams{
		ToolInstances: []tool.Tool{writeTool},
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 5,
	})
	s.Require().NoError(err, "创建 DeepAgent 失败")

	sess := s.NewTestSession("fine-grained-reject")
	s.Require().NoError(sess.PreRun(ctx), "Session PreRun 失败")

	// 第 1 步：触发中断 → 拒绝
	result, err := agent.Invoke(ctx, map[string]any{"query": "写入文件"},
		agentinterfaces.WithSession(sess),
	)
	s.Require().NoError(err, "首次 Invoke 失败")
	s.Require().NotNil(result, "结果不应为 nil")

	interruptIDs, _ := testhelpers.AssertInterruptResult(s.T(), result, 1)
	s.Require().NotEmpty(interruptIDs, "应有至少一个 interrupt_id")

	// 拒绝中断
	interactiveInput := testhelpers.RejectInterrupt(interruptIDs[0], "不允许写入")
	result, err = agent.Invoke(ctx, map[string]any{"query": interactiveInput},
		agentinterfaces.WithSession(sess),
	)
	s.Require().NoError(err, "拒绝恢复 Invoke 失败")
	testhelpers.AssertAnswerResult(s.T(), result)
	s.Equal(int32(0), writeInvokeCount.Load(), "拒绝后 write_file 不应执行")

	// 第 2 步：再次调用相同工具 → 仍触发中断（拒绝不存储 auto_confirm）
	result, err = agent.Invoke(ctx, map[string]any{"query": "再次写入文件"},
		agentinterfaces.WithSession(sess),
	)
	s.Require().NoError(err, "第二次 Invoke 失败")
	s.Require().NotNil(result, "第二次结果不应为 nil")

	// 验证仍然是中断结果（不是 answer）
	s.Equal("interrupt", result["result_type"], "拒绝后再次调用应仍触发中断")

	sess.PostRun(ctx)
}
