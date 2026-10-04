//go:build integration

package harness_test

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
	"github.com/uapclaw/uapclaw-go/tests/integration/testhelpers"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ForceFinishSuite 测试 RequestForceFinish 机制在各回调点的行为。
// 对照 Python: tests/system_tests/rail/test_force_finish_rail.py
//
// 核心机制：
//   - Rail 在回调中调用 cbc.RequestForceFinish(result) 请求强制结束
//   - RailExecutor.Execute 在 before 钩子后检查 HasForceFinishRequest
//   - 如果有请求：跳过 fn()，直接 fireAfter，返回 forced result
//   - reactLoop 在 callModel/executeToolCalls 后检查 ConsumeForceFinish
//   - 如果有请求：循环中断，返回 forced result
//
// 运行方式: go test -tags=integration ./tests/integration/agentcore/harness/...
type ForceFinishSuite struct {
	isuite.AgentSuite
}

// ForceFinishBeforeModelCallRail 在 BeforeModelCall 中请求强制完成的辅助 Rail。
// 对照 Python: InterceptRail(before_model_call=ctx.request_force_finish(forced))
type ForceFinishBeforeModelCallRail struct {
	agentinterfaces.BaseRail
	// forcedResult 强制完成的结果
	forcedResult map[string]any
}

// ForceFinishAfterModelCallRail 在 AfterModelCall 中请求强制完成的辅助 Rail。
// 对照 Python: InterceptRail(after_model_call=ctx.request_force_finish(forced))
type ForceFinishAfterModelCallRail struct {
	agentinterfaces.BaseRail
	// forcedResult 强制完成的结果
	forcedResult map[string]any
}

// ForceFinishAfterToolCallRail 在 AfterToolCall 中请求强制完成的辅助 Rail。
// 对照 Python: InterceptRail(after_tool_call=ctx.request_force_finish(forced))
type ForceFinishAfterToolCallRail struct {
	agentinterfaces.BaseRail
	// forcedResult 强制完成的结果
	forcedResult map[string]any
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestForceFinishSuite 运行 ForceFinish 集成测试套件
func TestForceFinishSuite(t *testing.T) {
	suite.Run(t, new(ForceFinishSuite))
}

// TestForceFinish_BeforeModelCall跳过LLM 测试 BeforeModelCall 中强制完成跳过 LLM 调用。
// 对照 Python: test_before_model_call_skips_llm_and_returns_result
//
// 核心验证：
//   - Rail 在 BeforeModelCall 中调用 RequestForceFinish
//   - LLM 不被调用（MockLLM.InvokeCallCount == 0）
//   - 返回 forced result
func (s *ForceFinishSuite) TestForceFinish_BeforeModelCall跳过LLM() {
	ctx := s.Ctx

	forcedResult := map[string]any{
		"output":      "强制完成结果",
		"result_type": "answer",
	}
	rail := &ForceFinishBeforeModelCallRail{forcedResult: forcedResult}

	s.MockLLM.SetResponses(
		mockllm.CreateTextResponse("不应出现"),
	)

	agent, err := s.NewDeepAgentForTest(ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 5,
	})
	s.Require().NoError(err, "创建 DeepAgent 失败")

	sess := s.NewTestSession("force-finish-before-model")
	s.Require().NoError(sess.PreRun(ctx), "Session PreRun 失败")

	// 记录调用前的 LLM 调用次数
	callCountBefore := s.MockLLM.InvokeCallCount()

	result, err := agent.Invoke(ctx, map[string]any{"query": "计算任务"},
		agentinterfaces.WithSession(sess),
	)
	s.Require().NoError(err, "Invoke 失败")
	s.Require().NotNil(result)

	sess.PostRun(ctx)

	// 验证返回 forced result
	s.Equal(forcedResult["output"], result["output"],
		"应返回强制完成的结果")
	s.Equal(forcedResult["result_type"], result["result_type"],
		"result_type 应为 answer")

	// 验证 LLM 未被调用
	s.Equal(callCountBefore, s.MockLLM.InvokeCallCount(),
		"BeforeModelCall 强制完成后 LLM 不应被调用")
}

// TestForceFinish_AfterModelCall阻止工具执行 测试 AfterModelCall 中强制完成阻止工具执行。
// 对照 Python: test_after_model_call_stops_before_tool_execution
//
// 核心验证：
//   - LLM 被调用 1 次（返回 tool_call）
//   - Rail 在 AfterModelCall 中调用 RequestForceFinish
//   - 工具不执行（BeforeToolCall 不触发）
//   - 返回 forced result
func (s *ForceFinishSuite) TestForceFinish_AfterModelCall阻止工具执行() {
	ctx := s.Ctx

	forcedResult := map[string]any{
		"output":      "模型调用后强制完成",
		"result_type": "answer",
	}
	rail := &ForceFinishAfterModelCallRail{forcedResult: forcedResult}
	toolTrace := testhelpers.NewToolTraceRail()

	addTool := newAddTool()

	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("add", `{"a": 1, "b": 2}`),
		mockllm.CreateTextResponse("不应出现"),
	)

	agent, err := s.NewDeepAgentForTest(ctx, hconfig.CreateDeepAgentParams{
		ToolInstances: []tool.Tool{addTool},
		Rails:         []agentinterfaces.AgentRail{rail, toolTrace},
		MaxIterations: 5,
	})
	s.Require().NoError(err, "创建 DeepAgent 失败")

	sess := s.NewTestSession("force-finish-after-model")
	s.Require().NoError(sess.PreRun(ctx), "Session PreRun 失败")

	callCountBefore := s.MockLLM.InvokeCallCount()

	result, err := agent.Invoke(ctx, map[string]any{"query": "计算 1+2"},
		agentinterfaces.WithSession(sess),
	)
	s.Require().NoError(err, "Invoke 失败")
	s.Require().NotNil(result)

	sess.PostRun(ctx)

	// 验证 LLM 被调用恰好 1 次
	s.Equal(callCountBefore+1, s.MockLLM.InvokeCallCount(),
		"AfterModelCall 强制完成前 LLM 应被调用 1 次")

	// 验证返回 forced result
	s.Equal(forcedResult["output"], result["output"],
		"应返回强制完成的结果")

	// 验证工具未执行（ToolTrace 无记录）
	s.Equal(0, toolTrace.TotalCalls(),
		"AfterModelCall 强制完成后工具不应执行")
}

// TestForceFinish_AfterToolCall中断循环 测试 AfterToolCall 中强制完成中断 ReAct 循环。
// 对照 Python: test_after_tool_call_breaks_loop
//
// 核心验证：
//   - LLM 被调用 1 次（返回 tool_call）
//   - 工具执行完成
//   - Rail 在 AfterToolCall 中调用 RequestForceFinish
//   - ReAct 循环中断，不再调用 LLM
//   - 返回 forced result
func (s *ForceFinishSuite) TestForceFinish_AfterToolCall中断循环() {
	ctx := s.Ctx

	forcedResult := map[string]any{
		"output":      "工具执行后强制完成",
		"result_type": "answer",
	}
	rail := &ForceFinishAfterToolCallRail{forcedResult: forcedResult}

	addTool := newAddTool()

	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("add", `{"a": 3, "b": 4}`),
		mockllm.CreateTextResponse("不应出现"),
	)

	agent, err := s.NewDeepAgentForTest(ctx, hconfig.CreateDeepAgentParams{
		ToolInstances: []tool.Tool{addTool},
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 5,
	})
	s.Require().NoError(err, "创建 DeepAgent 失败")

	sess := s.NewTestSession("force-finish-after-tool")
	s.Require().NoError(sess.PreRun(ctx), "Session PreRun 失败")

	callCountBefore := s.MockLLM.InvokeCallCount()

	result, err := agent.Invoke(ctx, map[string]any{"query": "计算 3+4"},
		agentinterfaces.WithSession(sess),
	)
	s.Require().NoError(err, "Invoke 失败")
	s.Require().NotNil(result)

	sess.PostRun(ctx)

	// 验证 LLM 只被调用 1 次（工具执行后循环中断）
	s.Equal(callCountBefore+1, s.MockLLM.InvokeCallCount(),
		"AfterToolCall 强制完成后 LLM 应只被调用 1 次")

	// 验证返回 forced result
	s.Equal(forcedResult["output"], result["output"],
		"应返回强制完成的结果")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// GetCallbacks 注册 BeforeModelCall 回调。
func (r *ForceFinishBeforeModelCallRail) GetCallbacks() map[agentinterfaces.AgentCallbackEvent]cb.PerAgentCallbackFunc {
	return r.BuildCallbacks(
		r.CallbackFrom(agentinterfaces.CallbackBeforeModelCall, func(ctx context.Context, railCtx any) error {
			return r.BeforeModelCall(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
		}),
	)
}

// BeforeModelCall 在模型调用前请求强制完成。
func (r *ForceFinishBeforeModelCallRail) BeforeModelCall(_ context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	cbc.RequestForceFinish(r.forcedResult)
	return nil
}

// GetCallbacks 注册 AfterModelCall 回调。
func (r *ForceFinishAfterModelCallRail) GetCallbacks() map[agentinterfaces.AgentCallbackEvent]cb.PerAgentCallbackFunc {
	return r.BuildCallbacks(
		r.CallbackFrom(agentinterfaces.CallbackAfterModelCall, func(ctx context.Context, railCtx any) error {
			return r.AfterModelCall(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
		}),
	)
}

// AfterModelCall 在模型调用后请求强制完成。
func (r *ForceFinishAfterModelCallRail) AfterModelCall(_ context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	cbc.RequestForceFinish(r.forcedResult)
	return nil
}

// GetCallbacks 注册 AfterToolCall 回调。
func (r *ForceFinishAfterToolCallRail) GetCallbacks() map[agentinterfaces.AgentCallbackEvent]cb.PerAgentCallbackFunc {
	return r.BuildCallbacks(
		r.CallbackFrom(agentinterfaces.CallbackAfterToolCall, func(ctx context.Context, railCtx any) error {
			return r.AfterToolCall(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
		}),
	)
}

// AfterToolCall 在工具执行后请求强制完成。
func (r *ForceFinishAfterToolCallRail) AfterToolCall(_ context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	cbc.RequestForceFinish(r.forcedResult)
	return nil
}

// newAddTool 创建一个模拟 add 工具。
func newAddTool() tool.Tool {
	tc := tool.NewToolCardWithID("add", "add", "加法运算", nil, nil)
	t, _ := tool.NewMapFunction(tc,
		func(_ context.Context, inputs map[string]any) (map[string]any, error) {
			a, _ := inputs["a"].(float64)
			b, _ := inputs["b"].(float64)
			return map[string]any{"result": a + b}, nil
		}, nil,
	)
	return t
}

// ForceFinishCaptureRail 在 BeforeModelCall 中请求强制完成并在 AfterInvoke 中捕获结果的辅助 Rail。
// 对照 Python: CaptureRail（before_model_call=request_force_finish, after_invoke=capture）
type ForceFinishCaptureRail struct {
	agentinterfaces.BaseRail
	// forcedResult 强制完成的结果
	forcedResult map[string]any
	// captured AfterInvoke 中捕获的 result
	captured []map[string]any
	// mu 保护 captured
	mu sync.Mutex
}

// GetCallbacks 注册 BeforeModelCall + AfterInvoke 回调。
func (r *ForceFinishCaptureRail) GetCallbacks() map[agentinterfaces.AgentCallbackEvent]cb.PerAgentCallbackFunc {
	return r.BuildCallbacks(
		r.CallbackFrom(agentinterfaces.CallbackBeforeModelCall, func(ctx context.Context, railCtx any) error {
			cbc := railCtx.(*agentinterfaces.AgentCallbackContext)
			cbc.RequestForceFinish(r.forcedResult)
			return nil
		}),
		r.CallbackFrom(agentinterfaces.CallbackAfterInvoke, func(ctx context.Context, railCtx any) error {
			cbc := railCtx.(*agentinterfaces.AgentCallbackContext)
			if inputs, ok := cbc.Inputs().(*agentinterfaces.InvokeInputs); ok && inputs != nil {
				r.mu.Lock()
				r.captured = append(r.captured, inputs.Result)
				r.mu.Unlock()
			}
			return nil
		}),
	)
}

// GetCaptured 返回捕获的 AfterInvoke 结果列表。
func (r *ForceFinishCaptureRail) GetCaptured() []map[string]any {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make([]map[string]any, len(r.captured))
	copy(result, r.captured)
	return result
}

// TestForceFinish_Result在AfterInvoke中可见 测试强制完成结果在 AfterInvoke 回调中通过 inputs.Result 可访问。
// 对照 Python: test_force_finish_result_visible_in_after_invoke
//
// 核心验证：
//   - Rail 在 BeforeModelCall 中调用 RequestForceFinish
//   - AfterInvoke 回调中 cbc.Inputs().(*InvokeInputs).Result 等于 forcedResult
func (s *ForceFinishSuite) TestForceFinish_Result在AfterInvoke中可见() {
	ctx := s.Ctx

	forcedResult := map[string]any{
		"output":      "forced_result",
		"result_type": "answer",
	}
	captureRail := &ForceFinishCaptureRail{forcedResult: forcedResult}

	s.MockLLM.SetResponses(
		mockllm.CreateTextResponse("不应出现"),
	)

	agent, err := s.NewDeepAgentForTest(ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{captureRail},
		MaxIterations: 5,
	})
	s.Require().NoError(err, "创建 DeepAgent 失败")

	sess := s.NewTestSession("force-finish-after-invoke")
	s.Require().NoError(sess.PreRun(ctx), "Session PreRun 失败")

	result, err := agent.Invoke(ctx, map[string]any{"query": "测试强制完成"},
		agentinterfaces.WithSession(sess),
	)
	s.Require().NoError(err, "Invoke 失败")
	s.Require().NotNil(result)

	sess.PostRun(ctx)

	// 验证返回 forced result
	s.Equal(forcedResult["output"], result["output"],
		"应返回强制完成的结果")

	// 验证 AfterInvoke 中捕获的结果与 forcedResult 相同
	captured := captureRail.GetCaptured()
	s.Len(captured, 1, "AfterInvoke 应被调用恰好 1 次")
	if len(captured) > 0 {
		s.Equal(forcedResult["output"], captured[0]["output"],
			"AfterInvoke 中 inputs.Result.output 应等于 forcedResult.output")
		s.Equal(forcedResult["result_type"], captured[0]["result_type"],
			"AfterInvoke 中 inputs.Result.result_type 应等于 forcedResult.result_type")
	}
}

// TestForceFinish_带ConversationID 测试带 conversation_id 时强制完成正常工作。
// 对照 Python: test_force_finish_with_conversation_id
//
// 核心验证：
//   - Invoke 传入 conversation_id
//   - Rail 在 BeforeModelCall 中调用 RequestForceFinish
//   - 返回 forced result，conversation_id 不影响行为
func (s *ForceFinishSuite) TestForceFinish_带ConversationID() {
	ctx := s.Ctx

	forcedResult := map[string]any{
		"output":      "with_conv_id",
		"result_type": "answer",
	}
	rail := &ForceFinishBeforeModelCallRail{forcedResult: forcedResult}

	s.MockLLM.SetResponses(
		mockllm.CreateTextResponse("不应出现"),
	)

	agent, err := s.NewDeepAgentForTest(ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 5,
	})
	s.Require().NoError(err, "创建 DeepAgent 失败")

	sess := s.NewTestSession("force-finish-conv-id")
	s.Require().NoError(sess.PreRun(ctx), "Session PreRun 失败")

	// 带 conversation_id 的 Invoke
	result, err := agent.Invoke(ctx, map[string]any{
		"query":           "测试强制完成",
		"conversation_id": "conv_123",
	}, agentinterfaces.WithSession(sess))
	s.Require().NoError(err, "Invoke 失败")
	s.Require().NotNil(result)

	sess.PostRun(ctx)

	// 验证返回 forced result
	s.Equal(forcedResult["output"], result["output"],
		"带 conversation_id 时应返回强制完成的结果")
	s.Equal(forcedResult["result_type"], result["result_type"],
		"result_type 应为 answer")
}
