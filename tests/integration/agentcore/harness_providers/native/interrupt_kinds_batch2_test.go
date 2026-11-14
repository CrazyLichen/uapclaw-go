//go:build integration

package native

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	interrupt "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/interrupt"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	hp "github.com/uapclaw/uapclaw-go/internal/agentcore/harness_protocol"
	saschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
)

// TestNativeInterruptKindsBatch2Suite 中断类型映射第二批用例。
//
// 对应 Python: tests/unit_tests/harness_providers/test_native_interrupt_kinds.py
// 的 batch-2 段（空应答 / 中止先于客户端 / 能力未声明 / ask_user 形状 /
// undeclared kind 透传）。
type TestNativeInterruptKindsBatch2Suite struct {
	NativeHarnessSuite
}

func TestNativeInterruptKindsBatch2SuiteEntry(t *testing.T) {
	suite.Run(t, new(TestNativeInterruptKindsBatch2Suite))
}

// ──────────────────────────── 导出函数 ────────────────────────────

// Test审批空应答视为拒绝 参数化 content=nil / content=""，工具不得执行。
// Python: test_an_approval_answered_with_nothing_is_rejected（2 例）
func (s *TestNativeInterruptKindsBatch2Suite) Test审批空应答视为拒绝() {
	cases := []struct {
		name    string
		content hp.JsonValue
	}{
		{"content为null", nil},
		{"content为空串", ""},
	}
	for _, testCase := range cases {
		s.Run(testCase.name, func() {
			toolInstance := newCountingTool("doc_tool")
			host := newRecordedHost(func(request hp.HarnessInteractionRequest) (hp.HarnessInteractionResponse, error) {
				req := request.(hp.UserInputRequest)
				return hp.NewUserInputResponse(req.RequestID, hp.InteractionResponseStatusCompleted,
					hp.UserInputResponseOptions{Content: testCase.content})
			})
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
			s.Equal(int32(0), toolInstance.InvokeCount(), "空应答应视为拒绝，工具不得执行")
		})
	}
}

// Test中止在客户端执行前停止轮次 双调用一响应（client tool + 权限工具），
// 宿主对审批回 ABORT：只问审批，客户端调用保持挂起，pending 含两种 kind。
// Python: test_an_aborted_approval_stops_the_turn_before_the_client_runs_anything
func (s *TestNativeInterruptKindsBatch2Suite) Test中止在客户端执行前停止轮次() {
	clientTool := newClipboardClientTool()
	permissionTool := newCountingTool("doc_tool")
	host := newRecordedHost(func(request hp.HarnessInteractionRequest) (hp.HarnessInteractionResponse, error) {
		if req, ok := request.(hp.DynamicToolCallRequest); ok {
			return hp.NewDynamicToolCallResponse(req.RequestID, hp.InteractionResponseStatusCompleted,
				hp.DynamicToolCallResponseOptions{Result: "pasted"})
		}
		req := request.(hp.ToolApprovalRequest)
		return hp.NewToolApprovalResponse(req.RequestID, hp.ToolApprovalDecisionAbort,
			hp.ToolApprovalResponseOptions{})
	})
	// Python: 同一模型响应携带两个 tool_call（clipboard + doc_tool）。
	first := mockllm.CreateMultiToolCallResponse([]mockllm.ToolCallSpec{
		{Name: "read_clipboard", ArgsJSON: "{}", CallID: "call_c"},
		{Name: "doc_tool", ArgsJSON: "{}", CallID: "call_p"},
	})
	result, err := s.turnWith(
		host, allCapabilities(),
		func(params hconfig.CreateDeepAgentParams) hconfig.CreateDeepAgentParams {
			params.ToolInstances = []tool.Tool{clientTool, permissionTool}
			params.Rails = []agentRail{newPermissionRail(askPermissionsConfig("doc_tool"))}
			return params
		},
		first,
	)
	s.Require().NoError(err)

	// 只收到审批请求（client call 排最后，审批先问且选择停轮）。
	requests := host.all()
	s.Require().Len(requests, 1)
	_, isApproval := requests[0].(hp.ToolApprovalRequest)
	s.True(isApproval, "应先收到 ToolApprovalRequest，实际 %T", requests[0])

	s.Require().NotNil(result.StopReason)
	s.Equal("interrupt", *result.StopReason)
	pending, ok := result.ProviderData["pending_interrupts"].([]any)
	s.Require().True(ok)
	s.Require().Len(pending, 2)
	kinds := []string{}
	for _, item := range pending {
		entry, _ := item.(map[string]any)
		kinds = append(kinds, entry["kind"].(string))
	}
	// Python: sorted(...) == [CLIENT_TOOL, PERMISSION]
	s.ElementsMatch([]string{"client_tool", "permission"}, kinds)
}

// Test未声明USER_INPUT能力的宿主仍被询问 参数化：无能力 / 仅 TOOL_APPROVAL。
// Python: test_a_host_that_did_not_declare_user_input_is_still_asked_questions（2 例）
func (s *TestNativeInterruptKindsBatch2Suite) Test未声明USER_INPUT能力的宿主仍被询问() {
	cases := []struct {
		name         string
		capabilities map[hp.HostCapability]bool
	}{
		{"无任何能力", map[hp.HostCapability]bool{}},
		{"仅声明审批", map[hp.HostCapability]bool{hp.HostCapabilityToolApproval: true}},
	}
	for _, testCase := range cases {
		s.Run(testCase.name, func() {
			host := newRecordedHost(blueAnswer)
			result, err := s.turnWith(
				host, testCase.capabilities,
				func(params hconfig.CreateDeepAgentParams) hconfig.CreateDeepAgentParams {
					params.Rails = []agentRail{interrupt.NewAskUserRail()}
					return params
				},
				mockllm.CreateMultiToolCallResponse([]mockllm.ToolCallSpec{
					{Name: "ask_user", ArgsJSON: askArgsJSON, CallID: "call_q"},
				}),
			)
			s.Require().NoError(err)

			requests := host.all()
			s.Require().Len(requests, 1)
			_, isUserInput := requests[0].(hp.UserInputRequest)
			s.True(isUserInput, "应收到 UserInputRequest，实际 %T", requests[0])
			s.Contains(s.llmToolResultsJoined(), `"Which color?"="blue"`)
			s.Equal("done", finalOutputOf(result))
		})
	}
}

// Test未声明任何能力的宿主以文字应答审批 空能力集仍收三选一 UserInputRequest。
// Python: test_a_host_that_declared_nothing_answers_an_approval_with_a_word
func (s *TestNativeInterruptKindsBatch2Suite) Test未声明任何能力的宿主以文字应答审批() {
	toolInstance := newCountingTool("doc_tool")
	host := newRecordedHost(allowApproval)
	_, err := s.turnWith(
		host, map[hp.HostCapability]bool{},
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
	s.Require().True(ok)
	s.Equal([]any{"allow", "allow_always", "deny"}, choicesOf(req.Choices))
	s.Equal(int32(1), toolInstance.InvokeCount(), "文字 yes 归一为允许后工具应执行")
}

// TestAskUser以UserInputRequest到达宿主 验证 kind=ask_user 与 prompt 透传。
// Python: test_ask_user_goes_to_the_host_as_user_input
func (s *TestNativeInterruptKindsBatch2Suite) TestAskUser以UserInputRequest到达宿主() {
	host := newRecordedHost(blueAnswer)
	_, err := s.turnWith(
		host, allCapabilities(),
		func(params hconfig.CreateDeepAgentParams) hconfig.CreateDeepAgentParams {
			params.Rails = []agentRail{interrupt.NewAskUserRail()}
			return params
		},
		mockllm.CreateMultiToolCallResponse([]mockllm.ToolCallSpec{
			{Name: "ask_user", ArgsJSON: askArgsJSON, CallID: "call_q"},
		}),
	)
	s.Require().NoError(err)

	requests := host.all()
	s.Require().Len(requests, 1)
	req, ok := requests[0].(hp.UserInputRequest)
	s.Require().True(ok)
	s.Equal("ask_user", req.ProviderData["kind"])
	s.Equal("Which color?", req.Prompt)
	seen := s.llmToolResultsSeen()
	s.Require().Len(seen, 1)
	s.Contains(seen[0][1], `"Which color?"="blue"`)
}

// Test未声明kind的原样透传 自定义无 kind rail：宿主答案原样回给 rail。
// Python: test_an_undeclared_kind_gets_the_host_answer_as_is（UndeclaredTextRail）
func (s *TestNativeInterruptKindsBatch2Suite) Test未声明kind的原样透传() {
	host := newRecordedHost(func(request hp.HarnessInteractionRequest) (hp.HarnessInteractionResponse, error) {
		if req, ok := request.(hp.UserInputRequest); ok {
			return hp.NewUserInputResponse(req.RequestID, hp.InteractionResponseStatusCompleted,
				hp.UserInputResponseOptions{Content: "Paris"})
		}
		return allowApproval(request)
	})
	_, err := s.turnWith(
		host, allCapabilities(),
		func(params hconfig.CreateDeepAgentParams) hconfig.CreateDeepAgentParams {
			params.ToolInstances = []tool.Tool{newCountingTool("doc_tool")}
			params.Rails = []agentRail{newUndeclaredTextRail("doc_tool")}
			return params
		},
		mockllm.CreateMultiToolCallResponse([]mockllm.ToolCallSpec{
			{Name: "doc_tool", ArgsJSON: "{}", CallID: "call_n"},
		}),
	)
	s.Require().NoError(err)

	requests := host.all()
	s.Require().Len(requests, 1)
	req, ok := requests[0].(hp.UserInputRequest)
	s.Require().True(ok)
	s.Empty(req.Choices, "未声明 kind 不提供 choices")
	s.Equal("", req.ProviderData["kind"], "kind 应为 UNDECLARED 空串")
	s.NotEmpty(req.ProviderData["interrupt_id"])
	s.Equal(`GOT:"Paris"`, s.llmToolResultsJoined())
}

// Test未声明审批rail接受宿主审批dict 无 kind 的 confirm rail 收 approved 形状应答。
// Python: test_an_undeclared_approval_rail_takes_the_host_approval_dict（UndeclaredConfirmRail）
func (s *TestNativeInterruptKindsBatch2Suite) Test未声明审批rail接受宿主审批dict() {
	toolInstance := newCountingTool("doc_tool")
	host := newRecordedHost(func(request hp.HarnessInteractionRequest) (hp.HarnessInteractionResponse, error) {
		req := request.(hp.UserInputRequest)
		return hp.NewUserInputResponse(req.RequestID, hp.InteractionResponseStatusCompleted,
			hp.UserInputResponseOptions{Content: map[string]any{"approved": true}})
	})
	confirmRail := interrupt.NewConfirmInterruptRail(interrupt.WithConfirmToolNames("doc_tool"))
	confirmRail.SetInterruptKind("") // Python: interrupt_kind = InterruptKind.UNDECLARED 类属性覆盖
	_, err := s.turnWith(
		host, allCapabilities(),
		func(params hconfig.CreateDeepAgentParams) hconfig.CreateDeepAgentParams {
			params.ToolInstances = []tool.Tool{toolInstance}
			params.Rails = []agentRail{confirmRail}
			return params
		},
		mockllm.CreateMultiToolCallResponse([]mockllm.ToolCallSpec{
			{Name: "doc_tool", ArgsJSON: "{}", CallID: "call_n"},
		}),
	)
	s.Require().NoError(err)

	requests := host.all()
	s.Require().Len(requests, 1)
	_, isUserInput := requests[0].(hp.UserInputRequest)
	s.True(isUserInput, "未声明 kind 应以 UserInputRequest 问宿主，实际 %T", requests[0])
	s.Equal(int32(1), toolInstance.InvokeCount(), "approved 形状应答应放行工具")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// turnWith 组合 agent 构建 + runTurn（镜像 TestNativeInterruptKindsSuite.turnWith；
// 挂在本套件上以便批二用例复用）。
func (s *TestNativeInterruptKindsBatch2Suite) turnWith(
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

// askArgsJSON ask_user 工具的问题参数。
// Python: test_native_interrupt_kinds.py:312 ASK_ARGS
const askArgsJSON = `{"questions": [{"header": "Color", "question": "Which color?", "options": []}]}`

// blueAnswer 以 dict 形式回答颜色问题。
// Python: test_native_interrupt_kinds.py:315-317 blue
func blueAnswer(request hp.HarnessInteractionRequest) (hp.HarnessInteractionResponse, error) {
	req := request.(hp.UserInputRequest)
	return hp.NewUserInputResponse(req.RequestID, hp.InteractionResponseStatusCompleted,
		hp.UserInputResponseOptions{Content: map[string]any{"answers": map[string]any{"Which color?": "blue"}}})
}

// undeclaredTextRail 无 kind 的自由文本提问 rail（测试内自定义）。
// Python: test_native_interrupt_kinds.py:380-386 UndeclaredTextRail(BaseInterruptRail)：
// 无输入 → interrupt("Which city?")；有输入 → reject("GOT:" + json.dumps(user_input))
type undeclaredTextRail struct {
	interrupt.BaseInterruptRail
}

func newUndeclaredTextRail(toolNames ...string) *undeclaredTextRail {
	r := &undeclaredTextRail{BaseInterruptRail: *interrupt.NewBaseInterruptRail(toolNames...)}
	r.ResolveInterruptFn = r.resolve
	return r
}

func (r *undeclaredTextRail) resolve(
	_ context.Context,
	_ *agentinterfaces.AgentCallbackContext,
	_ *llmschema.ToolCall,
	userInput any,
	_ map[string]any,
) interrupt.InterruptDecision {
	if userInput == nil {
		return r.Interrupt(&saschema.InterruptRequest{Message: "Which city?"})
	}
	rendered, err := json.Marshal(userInput)
	if err != nil {
		return r.Reject("GOT:?")
	}
	return r.Reject(fmt.Sprintf("GOT:%s", rendered))
}
