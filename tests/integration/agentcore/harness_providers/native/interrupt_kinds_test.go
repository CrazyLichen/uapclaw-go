//go:build integration

package native

import (
	"context"
	"testing"

	"github.com/stretchr/testify/suite"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	hp "github.com/uapclaw/uapclaw-go/internal/agentcore/harness_protocol"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
)

// TestNativeInterruptKindsSuite 中断类型映射端到端用例。
//
// 对应 Python: tests/unit_tests/harness_providers/test_native_interrupt_kinds.py
// （文档串：native harness 把每种中断映射到正确的宿主交互）
type TestNativeInterruptKindsSuite struct {
	NativeHarnessSuite
}

func TestNativeInterruptKindsSuiteEntry(t *testing.T) {
	suite.Run(t, new(TestNativeInterruptKindsSuite))
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestCard_声明其使用的可选宿主能力 验证 card 的 optional_host_capabilities 覆盖三个能力。
// Python: test_card_declares_the_optional_host_capabilities_it_uses
func (s *TestNativeInterruptKindsSuite) TestCard_声明其使用的可选宿主能力() {
	card := s.nativeCardOf()
	for _, capability := range []hp.HostCapability{
		hp.HostCapabilityUserInput,
		hp.HostCapabilityToolApproval,
		hp.HostCapabilityDynamicToolCall,
	} {
		s.True(card.OptionalHostCapabilities[capability],
			"card 应声明可选宿主能力 %s", capability)
	}
}

// TestD_权限询问以工具审批到达宿主 验证 permission ask → ToolApprovalRequest → ALLOW → 工具执行。
// Python: test_d_permission_ask_reaches_the_host_as_a_tool_approval（Issue D 回归）
func (s *TestNativeInterruptKindsSuite) TestD_权限询问以工具审批到达宿主() {
	toolInstance := newCountingTool("doc_tool")
	host := newRecordedHost(allowApproval)
	result, err := s.turnWith(
		host, allCapabilities(),
		func(params hconfig.CreateDeepAgentParams) hconfig.CreateDeepAgentParams {
			params.ToolInstances = []tool.Tool{toolInstance}
			params.Rails = []agentRail{newPermissionRail(askPermissionsConfig("doc_tool"))}
			return params
		},
		mockllm.CreateMultiToolCallResponse([]mockllm.ToolCallSpec{
			{Name: "doc_tool", ArgsJSON: `{"x": "1"}`, CallID: "call_D"},
		}),
	)
	s.Require().NoError(err)

	requests := host.all()
	s.Require().Len(requests, 1, "应恰好一次宿主请求")
	approval, ok := requests[0].(hp.ToolApprovalRequest)
	s.Require().True(ok, "permission ask 应映射为 ToolApprovalRequest，实际 %T", requests[0])
	s.Equal("call_D", approval.CallID)
	s.NotNil(approval.ProviderData["interrupt_id"])
	s.NotEqual("call_D", approval.ProviderData["interrupt_id"],
		"interrupt_id 应与 tool_call_id 不同")
	s.Equal("permission", approval.ProviderData["kind"])
	s.Equal(int32(1), toolInstance.InvokeCount(), "批准后工具应执行一次")
	// Python: assert tool.invoke_count == 1 and result.final_output == "done"
	s.Equal("done", finalOutputOf(result), "resume 后应得到最终输出")
	_ = err
}

// Test纯文本宿主以文字应答审批 验证未声明 TOOL_APPROVAL 的宿主收到三选一 UserInputRequest。
// Python: test_text_only_host_answers_an_approval_with_a_word
func (s *TestNativeInterruptKindsSuite) Test纯文本宿主以文字应答审批() {
	toolInstance := newCountingTool("doc_tool")
	host := newRecordedHost(allowApproval)
	_, err := s.turnWith(
		host, map[hp.HostCapability]bool{hp.HostCapabilityUserInput: true},
		func(params hconfig.CreateDeepAgentParams) hconfig.CreateDeepAgentParams {
			params.ToolInstances = []tool.Tool{toolInstance}
			params.Rails = []agentRail{newPermissionRail(askPermissionsConfig("doc_tool"))}
			return params
		},
		mockllm.CreateMultiToolCallResponse([]mockllm.ToolCallSpec{
			{Name: "doc_tool", ArgsJSON: "{}", CallID: "call_t"},
		}),
	)
	s.Require().NoError(err)

	requests := host.all()
	s.Require().Len(requests, 1)
	req, ok := requests[0].(hp.UserInputRequest)
	s.Require().True(ok, "纯文本宿主应收到 UserInputRequest，实际 %T", requests[0])
	s.Equal([]any{"allow", "allow_always", "deny"}, choicesOf(req.Choices))
	s.Equal(int32(1), toolInstance.InvokeCount(), "文字 yes 归一为允许后工具应执行")
}

// Test审批时编辑参数即拒绝 验证 updated_arguments 非 nil 时不执行原调用。
// Python: test_approval_with_edited_arguments_is_denied_not_run_with_the_original
func (s *TestNativeInterruptKindsSuite) Test审批时编辑参数即拒绝() {
	toolInstance := newCountingTool("doc_tool")
	host := newRecordedHost(func(request hp.HarnessInteractionRequest) (hp.HarnessInteractionResponse, error) {
		if req, ok := request.(hp.ToolApprovalRequest); ok {
			return hp.NewToolApprovalResponse(req.RequestID, hp.ToolApprovalDecisionAllow,
				hp.ToolApprovalResponseOptions{UpdatedArguments: map[string]any{"x": "SAFE"}})
		}
		req := request.(hp.UserInputRequest)
		return hp.NewUserInputResponse(req.RequestID, hp.InteractionResponseStatusCompleted,
			hp.UserInputResponseOptions{Content: "yes"})
	})
	_, err := s.turnWith(
		host, allCapabilities(),
		func(params hconfig.CreateDeepAgentParams) hconfig.CreateDeepAgentParams {
			params.ToolInstances = []tool.Tool{toolInstance}
			params.Rails = []agentRail{newPermissionRail(askPermissionsConfig("doc_tool"))}
			return params
		},
		mockllm.CreateMultiToolCallResponse([]mockllm.ToolCallSpec{
			{Name: "doc_tool", ArgsJSON: `{"x": "1"}`, CallID: "call_u"},
		}),
	)
	s.Require().NoError(err)

	s.Equal(int32(0), toolInstance.InvokeCount(), "编辑参数不被支持，调用不得执行")
	s.Contains(s.llmToolResultsJoined(), "editing arguments is not supported",
		"模型应收到拒绝反馈")
}

// Test中止或中断使轮次带着挂起询问停止 参数化 DENY+interrupt / ABORT。
// Python: test_abort_or_interrupt_stops_the_turn_with_the_ask_pending（2 例）
func (s *TestNativeInterruptKindsSuite) Test中止或中断使轮次带着挂起询问停止() {
	cases := []struct {
		name      string
		decision  hp.ToolApprovalDecision
		interrupt bool
	}{
		{"DENY加中断", hp.ToolApprovalDecisionDeny, true},
		{"ABORT", hp.ToolApprovalDecisionAbort, false},
	}
	for _, tc := range cases {
		s.Run(tc.name, func() {
			toolInstance := newCountingTool("doc_tool")
			host := newRecordedHost(func(request hp.HarnessInteractionRequest) (hp.HarnessInteractionResponse, error) {
				req, ok := request.(hp.ToolApprovalRequest)
				s.Require().True(ok)
				return hp.NewToolApprovalResponse(req.RequestID, tc.decision,
					hp.ToolApprovalResponseOptions{Interrupt: tc.interrupt})
			})
			result, err := s.turnWith(
				host, allCapabilities(),
				func(params hconfig.CreateDeepAgentParams) hconfig.CreateDeepAgentParams {
					params.ToolInstances = []tool.Tool{toolInstance}
					params.Rails = []agentRail{newPermissionRail(askPermissionsConfig("doc_tool"))}
					return params
				},
				mockllm.CreateMultiToolCallResponse([]mockllm.ToolCallSpec{
					{Name: "doc_tool", ArgsJSON: "{}", CallID: "call_s"},
				}),
			)
			s.Require().NoError(err)

			s.Equal(int32(0), toolInstance.InvokeCount())
			s.Require().NotNil(result.StopReason)
			s.Equal("interrupt", *result.StopReason)
			pending, ok := result.ProviderData["pending_interrupts"].([]any)
			s.Require().True(ok, "provider_data 应带 pending_interrupts")
			s.Require().Len(pending, 1)
			first, _ := pending[0].(map[string]any)
			s.Equal("call_s", first["tool_call_id"])
			s.Equal("permission", first["kind"])
			ids, _ := result.ProviderData["pending_interrupt_ids"].([]any)
			s.Equal([]any{"call_s"}, ids)
		})
	}
}

// Test客户端工具经动态工具调用执行 验证 client tool → DynamicToolCallRequest → 结果回填。
// Python: test_client_tool_runs_through_a_dynamic_tool_call
func (s *TestNativeInterruptKindsSuite) Test客户端工具经动态工具调用执行() {
	clientTool := newClipboardClientTool()
	host := newRecordedHost(func(request hp.HarnessInteractionRequest) (hp.HarnessInteractionResponse, error) {
		req, ok := request.(hp.DynamicToolCallRequest)
		s.Require().True(ok, "应收到 DynamicToolCallRequest，实际 %T", request)
		s.Equal("call_c", req.CallID)
		s.Equal("read_clipboard", req.ToolName)
		s.Equal(map[string]any{"action": "read_clipboard"}, req.ProviderData["client_request"])
		return hp.NewDynamicToolCallResponse(req.RequestID, hp.InteractionResponseStatusCompleted,
			hp.DynamicToolCallResponseOptions{Result: "Meeting at 3 pm"})
	})
	_, err := s.turnWith(
		host, allCapabilities(),
		func(params hconfig.CreateDeepAgentParams) hconfig.CreateDeepAgentParams {
			params.ToolInstances = []tool.Tool{clientTool}
			return params
		},
		mockllm.CreateMultiToolCallResponse([]mockllm.ToolCallSpec{
			{Name: "read_clipboard", ArgsJSON: "{}", CallID: "call_c"},
		}),
	)
	s.Require().NoError(err)

	seen := s.llmToolResultsSeen()
	s.Require().Len(seen, 1)
	s.Equal("call_c", seen[0][0])
	s.Equal("Meeting at 3 pm", seen[0][1])
}

// Test宿主取消的客户端调用是取消工具错误 验证 CANCELLED 应答 → 取消文案。
// Python: test_client_tool_cancelled_by_the_host_is_a_cancelled_tool_error
func (s *TestNativeInterruptKindsSuite) Test宿主取消的客户端调用是取消工具错误() {
	clientTool := newClipboardClientTool()
	host := newRecordedHost(func(request hp.HarnessInteractionRequest) (hp.HarnessInteractionResponse, error) {
		req, ok := request.(hp.DynamicToolCallRequest)
		s.Require().True(ok)
		return hp.NewDynamicToolCallResponse(req.RequestID, hp.InteractionResponseStatusCancelled,
			hp.DynamicToolCallResponseOptions{})
	})
	result, err := s.turnWith(
		host, allCapabilities(),
		func(params hconfig.CreateDeepAgentParams) hconfig.CreateDeepAgentParams {
			params.ToolInstances = []tool.Tool{clientTool}
			return params
		},
		mockllm.CreateMultiToolCallResponse([]mockllm.ToolCallSpec{
			{Name: "read_clipboard", ArgsJSON: "{}", CallID: "call_c"},
		}),
	)
	s.Require().NoError(err)

	s.Contains(toolResultsJoined(result), "was cancelled")
	s.Equal("done", result.FinalOutput, "取消后轮次继续完成")
}

// Test未声明动态工具调用的宿主得到明确失败 验证无 DYNAMIC_TOOL_CALL 声明 → 工具错误且不打扰宿主。
// Python: test_client_tool_without_dynamic_tool_calls_fails_clearly
func (s *TestNativeInterruptKindsSuite) Test未声明动态工具调用的宿主得到明确失败() {
	clientTool := newClipboardClientTool()
	host := newRecordedHost(allowApproval)
	result, err := s.turnWith(
		host, map[hp.HostCapability]bool{
			hp.HostCapabilityUserInput:     true,
			hp.HostCapabilityToolApproval:  true,
		},
		func(params hconfig.CreateDeepAgentParams) hconfig.CreateDeepAgentParams {
			params.ToolInstances = []tool.Tool{clientTool}
			return params
		},
		mockllm.CreateMultiToolCallResponse([]mockllm.ToolCallSpec{
			{Name: "read_clipboard", ArgsJSON: "{}", CallID: "call_c"},
		}),
	)
	s.Require().NoError(err)

	s.Empty(host.all(), "宿主不应被打扰")
	s.Contains(toolResultsJoined(result), "cannot run client tools")
}

// Test客户端结果为null完成调用 验证 result=nil → COMPLETED 且轮次正常完成。
// Python: test_a_client_result_of_null_completes_the_call
func (s *TestNativeInterruptKindsSuite) Test客户端结果为null完成调用() {
	clientTool := newClipboardClientTool()
	host := newRecordedHost(func(request hp.HarnessInteractionRequest) (hp.HarnessInteractionResponse, error) {
		req := request.(hp.DynamicToolCallRequest)
		return hp.NewDynamicToolCallResponse(req.RequestID, hp.InteractionResponseStatusCompleted,
			hp.DynamicToolCallResponseOptions{Result: nil})
	})
	result, err := s.turnWith(
		host, allCapabilities(),
		func(params hconfig.CreateDeepAgentParams) hconfig.CreateDeepAgentParams {
			params.ToolInstances = []tool.Tool{clientTool}
			return params
		},
		mockllm.CreateMultiToolCallResponse([]mockllm.ToolCallSpec{
			{Name: "read_clipboard", ArgsJSON: "{}", CallID: "call_c"},
		}),
	)
	s.Require().NoError(err)

	s.Require().NotNil(result.StopReason)
	s.Equal("answer", *result.StopReason)
	s.Equal("done", result.FinalOutput)
	seen := toolResultsSeen(result)
	s.Require().Len(seen, 1)
	s.Equal("call_c", seen[0][0])
}

// Test失败的客户端调用是错误而非取消 参数化 FAILED(非 error) / DECLINED(is_error)。
// Python: test_a_failed_client_call_is_an_error_not_a_cancellation（2 例）
func (s *TestNativeInterruptKindsSuite) Test失败的客户端调用是错误而非取消() {
	cases := []struct {
		name    string
		status  hp.InteractionResponseStatus
		isError bool
	}{
		{"FAILED非error", hp.InteractionResponseStatusFailed, false},
		{"DECLINED加isError", hp.InteractionResponseStatusDeclined, true},
	}
	for _, tc := range cases {
		s.Run(tc.name, func() {
			clientTool := newClipboardClientTool()
			host := newRecordedHost(func(request hp.HarnessInteractionRequest) (hp.HarnessInteractionResponse, error) {
				req := request.(hp.DynamicToolCallRequest)
				return hp.NewDynamicToolCallResponse(req.RequestID, tc.status,
					hp.DynamicToolCallResponseOptions{IsError: tc.isError, ErrorMessage: strPtr("no native tool gateway is available")})
			})
			_, err := s.turnWith(
				host, allCapabilities(),
				func(params hconfig.CreateDeepAgentParams) hconfig.CreateDeepAgentParams {
					params.ToolInstances = []tool.Tool{clientTool}
					return params
				},
				mockllm.CreateMultiToolCallResponse([]mockllm.ToolCallSpec{
					{Name: "read_clipboard", ArgsJSON: "{}", CallID: "call_c"},
				}),
			)
			s.Require().NoError(err)
			seen := s.llmToolResultsSeen()
			s.Require().Len(seen, 1)
			s.Equal("no native tool gateway is available", seen[0][1])
		})
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// turnWith 组合 agent 构建 + runTurn。
func (s *TestNativeInterruptKindsSuite) turnWith(
	host *recordedHost,
	capabilities map[hp.HostCapability]bool,
	customize func(hconfig.CreateDeepAgentParams) hconfig.CreateDeepAgentParams,
	firstResponse mockllm.MockResponse,
) (hp.TurnResult, error) {
	s.MockLLM.SetResponses(
		firstResponse,
		mockllm.CreateTextResponse("done"),
	)
	h := newHarness(func(ctx context.Context) (*harness.DeepAgent, error) {
		params := customize(hconfig.CreateDeepAgentParams{})
		return s.newAgentWithParams(params)
	})
	result := s.runTurn(h, host, capabilities)
	return result, nil
}

// nativeCardOf 取 native harness 的协议卡（经实例 Card()）。
func (s *TestNativeInterruptKindsSuite) nativeCardOf() hp.HarnessCard {
	return newHarness(func(ctx context.Context) (*harness.DeepAgent, error) {
		return s.newAgentWithParams(hconfig.CreateDeepAgentParams{})
	}).Card()
}

// finalOutputOf 取终态输出文本。
func finalOutputOf(result hp.TurnResult) string {
	if text, ok := result.FinalOutput.(string); ok {
		return text
	}
	return ""
}

// strPtr 字符串转指针。
func strPtr(value string) *string { return &value }

// choicesOf 协议 choices 切片归一为 any 切片（对齐 Python tuple 比较）。
func choicesOf(choices []string) []any {
	out := make([]any, 0, len(choices))
	for _, item := range choices {
		out = append(out, item)
	}
	return out
}

// agentRail rail 约束别名（suite 夹具用）。
type agentRail = agentinterfaces.AgentRail

// contextCtx 别名避免 import 噪音。
type contextCtx = context.Context
