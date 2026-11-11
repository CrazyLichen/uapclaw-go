package harness_protocol

import (
	"context"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
)

// TestInteractionResponseStatus_值对齐Python 验证应答状态取值
// Python: interactions.py:39-45 InteractionResponseStatus（4 项）
func TestInteractionResponseStatus_值对齐Python(t *testing.T) {
	cases := map[string]string{
		string(InteractionResponseStatusCompleted): "completed",
		string(InteractionResponseStatusDeclined):  "declined",
		string(InteractionResponseStatusCancelled): "cancelled",
		string(InteractionResponseStatusFailed):    "failed",
	}
	if len(cases) != 4 {
		t.Fatalf("InteractionResponseStatus 成员数 = %d，上游为 4", len(cases))
	}
	for got, want := range cases {
		if got != want {
			t.Errorf("取值 = %q，期望 %q（协议字符串不得本地化）", got, want)
		}
	}
}

// TestToolApprovalDecision_值对齐Python 验证审批决策取值
// Python: interactions.py:48-54 ToolApprovalDecision（4 项）
func TestToolApprovalDecision_值对齐Python(t *testing.T) {
	cases := map[string]string{
		string(ToolApprovalDecisionAllow):           "allow",
		string(ToolApprovalDecisionAllowForSession): "allow_for_session",
		string(ToolApprovalDecisionDeny):            "deny",
		string(ToolApprovalDecisionAbort):           "abort",
	}
	if len(cases) != 4 {
		t.Fatalf("ToolApprovalDecision 成员数 = %d，上游为 4", len(cases))
	}
	for got, want := range cases {
		if got != want {
			t.Errorf("取值 = %q，期望 %q", got, want)
		}
	}
}

// TestInteractionCancelReason_值对齐Python 验证取消原因取值
// Python: interactions.py:57-63 InteractionCancelReason（4 项）
func TestInteractionCancelReason_值对齐Python(t *testing.T) {
	cases := map[string]string{
		string(InteractionCancelReasonProviderWithdrew): "provider_withdrew",
		string(InteractionCancelReasonTurnAborted):      "turn_aborted",
		string(InteractionCancelReasonHarnessStopped):   "harness_stopped",
		string(InteractionCancelReasonDeadlineExceeded): "deadline_exceeded",
	}
	if len(cases) != 4 {
		t.Fatalf("InteractionCancelReason 成员数 = %d，上游为 4", len(cases))
	}
	for got, want := range cases {
		if got != want {
			t.Errorf("取值 = %q，期望 %q", got, want)
		}
	}
}

// TestInteractions_枚举成员总数 三方合计必须 12（spec §2.1 的机器核对值）
func TestInteractions_枚举成员总数(t *testing.T) {
	total := 4 /* status */ + 4 /* decision */ + 4 /* cancel */
	if total != 12 {
		t.Fatalf("interactions.go 枚举成员合计 = %d，上游为 12", total)
	}
}

// TestInteractionDiscriminators_字面量守护 10 个 kind 字面量不得被改（15.4 codec 依赖）
// Q3 决策 1 + Q5b 决策 3-a
func TestInteractionDiscriminators_字面量守护(t *testing.T) {
	reqs := []struct {
		req      HarnessInteractionRequest
		kind     string
		expected string
	}{
		{mustToolApprovalReq(t), "tool_approval", "tool_approval_response"},
		{mustUserInputReq(t), "user_input", "user_input_response"},
		{mustMcpElicitationReq(t), "mcp_elicitation", "mcp_elicitation_response"},
		{mustDynamicToolCallReq(t), "dynamic_tool_call", "dynamic_tool_call_response"},
		{mustProviderInteractionReq(t), "provider_interaction", "provider_interaction_response"},
	}
	if len(reqs) != 5 {
		t.Fatalf("请求类型数 = %d，上游为 5", len(reqs))
	}
	for _, c := range reqs {
		if got := c.req.requestKind(); got != c.kind {
			t.Errorf("%T.requestKind() = %q，期望 %q", c.req, got, c.kind)
		}
		if got := c.req.expectedResponseKind(); got != c.expected {
			t.Errorf("%T.expectedResponseKind() = %q，期望 %q", c.req, got, c.expected)
		}
	}
}

// TestInteractionResponseDiscriminators_字面量守护
func TestInteractionResponseDiscriminators_字面量守护(t *testing.T) {
	resps := []struct {
		resp HarnessInteractionResponse
		kind string
	}{
		{mustToolApprovalResp(t), "tool_approval_response"},
		{mustUserInputResp(t), "user_input_response"},
		{mustMcpElicitationResp(t), "mcp_elicitation_response"},
		{mustDynamicToolCallResp(t), "dynamic_tool_call_response"},
		{mustProviderInteractionResp(t), "provider_interaction_response"},
	}
	if len(resps) != 5 {
		t.Fatalf("应答类型数 = %d，上游为 5", len(resps))
	}
	for _, c := range resps {
		if got := c.resp.responseKind(); got != c.kind {
			t.Errorf("%T.responseKind() = %q，期望 %q", c.resp, got, c.kind)
		}
	}
}

// TestValidateRequestCommon_三步边界 5 个请求共用校验的正反例矩阵
// Python: interactions.py:22-31 _validate_request_common
func TestValidateRequestCommon_三步边界(t *testing.T) {
	empty := ""
	badDeadline := -1.0
	nanDeadline := math.NaN()

	t.Run("request_id为空时拒绝", func(t *testing.T) {
		if _, err := NewToolApprovalRequest("", "call-1", "shell", ToolApprovalRequestOptions{}); err == nil {
			t.Fatal("空 request_id 应被拒绝")
		}
		if _, err := NewUserInputRequest("", "prompt", UserInputRequestOptions{}); err == nil {
			t.Fatal("空 request_id 应被拒绝")
		}
		if _, err := NewMcpElicitationRequest("", "srv", "prompt", McpElicitationRequestOptions{}); err == nil {
			t.Fatal("空 request_id 应被拒绝")
		}
		if _, err := NewDynamicToolCallRequest("", "call-1", "tool", DynamicToolCallRequestOptions{}); err == nil {
			t.Fatal("空 request_id 应被拒绝")
		}
		if _, err := NewProviderInteractionRequest("", "prov", "type", "1", ProviderInteractionRequestOptions{}); err == nil {
			t.Fatal("空 request_id 应被拒绝")
		}
	})

	// Q7 决策 A 的核心守护：nil 放行、"" 拒绝
	t.Run("provider_session_id为nil时放行", func(t *testing.T) {
		if _, err := NewToolApprovalRequest("r1", "call-1", "shell", ToolApprovalRequestOptions{ProviderSessionID: nil}); err != nil {
			t.Fatalf("nil 应放行（Python None 放行）: %v", err)
		}
	})
	t.Run("provider_session_id为空串时拒绝", func(t *testing.T) {
		_, err := NewToolApprovalRequest("r1", "call-1", "shell", ToolApprovalRequestOptions{ProviderSessionID: &empty})
		if err == nil {
			t.Fatal("空串应被拒绝（Python 显式校验 == \"\"）")
		}
		if !strings.Contains(err.Error(), "provider_session_id must not be empty") {
			t.Errorf("错误文案不符: %v", err)
		}
	})
	t.Run("turn_id为nil时放行", func(t *testing.T) {
		if _, err := NewUserInputRequest("r1", "prompt", UserInputRequestOptions{TurnID: nil}); err != nil {
			t.Fatalf("nil 应放行: %v", err)
		}
	})
	t.Run("turn_id为空串时拒绝", func(t *testing.T) {
		_, err := NewUserInputRequest("r1", "prompt", UserInputRequestOptions{TurnID: &empty})
		if err == nil {
			t.Fatal("空串应被拒绝")
		}
		if !strings.Contains(err.Error(), "turn_id must not be empty") {
			t.Errorf("错误文案不符: %v", err)
		}
	})
	t.Run("deadline_at为负时拒绝", func(t *testing.T) {
		_, err := NewMcpElicitationRequest("r1", "srv", "p", McpElicitationRequestOptions{DeadlineAt: &badDeadline})
		if err == nil {
			t.Fatal("负 deadline 应被拒绝")
		}
	})
	t.Run("deadline_at为NaN时拒绝", func(t *testing.T) {
		_, err := NewMcpElicitationRequest("r1", "srv", "p", McpElicitationRequestOptions{DeadlineAt: &nanDeadline})
		if err == nil {
			t.Fatal("NaN deadline 应被拒绝")
		}
	})
	t.Run("合法deadline放行", func(t *testing.T) {
		ok := 2_000_000_000.0
		if _, err := NewMcpElicitationRequest("r1", "srv", "p", McpElicitationRequestOptions{DeadlineAt: &ok}); err != nil {
			t.Fatalf("合法 deadline 不应报错: %v", err)
		}
	})
}

// TestNewUserInputRequest_choices深拷贝 Go 无 tuple，需构造期拷贝为独立切片
// Python: interactions.py:106 tuple(self.choices)
func TestNewUserInputRequest_choices深拷贝(t *testing.T) {
	src := []string{"a", "b"}
	got, err := NewUserInputRequest("r1", "prompt", UserInputRequestOptions{Choices: src})
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	src[0] = "changed"
	if got.Choices[0] != "a" {
		t.Error("Choices 未深拷贝，原切片修改污染了结果")
	}
}

// TestNewToolApprovalRequest_arguments含Inf时拒绝 覆盖冻结失败分支
func TestNewToolApprovalRequest_arguments含Inf时拒绝(t *testing.T) {
	_, err := NewToolApprovalRequest("r1", "call-1", "shell", ToolApprovalRequestOptions{
		Arguments: JsonObject{"x": math.Inf(1)},
	})
	if err == nil {
		t.Fatal("含 Inf 的参数应被拒绝（freeze_json_value 拒绝非有限数）")
	}
}

// TestNewToolApprovalResponse_冻结可选参数
// Python: interactions.py:185-189 ToolApprovalResponse.__post_init__
func TestNewToolApprovalResponse_冻结可选参数(t *testing.T) {
	updated := JsonObject{"command": "pwd"}
	got, err := NewToolApprovalResponse("r1", ToolApprovalDecisionAllow, ToolApprovalResponseOptions{
		UpdatedArguments: updated,
	})
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if got.Decision != ToolApprovalDecisionAllow {
		t.Errorf("Decision = %q", got.Decision)
	}
	updated["command"] = "rm -rf /"
	if got.UpdatedArguments["command"] != "pwd" {
		t.Error("UpdatedArguments 未深拷贝")
	}
}

// TestNewToolApprovalResponse_nil可选参数不报错
// Python: if self.updated_arguments is not None → 只在非 None 时冻结
func TestNewToolApprovalResponse_nil可选参数不报错(t *testing.T) {
	got, err := NewToolApprovalResponse("r1", ToolApprovalDecisionDeny, ToolApprovalResponseOptions{})
	if err != nil {
		t.Fatalf("nil UpdatedArguments 应放行（Python None 分支）: %v", err)
	}
	if got.UpdatedArguments != nil {
		t.Errorf("UpdatedArguments 应为 nil，实际 %v", got.UpdatedArguments)
	}
}

// TestNewResponse_空RequestID均被拒绝 5 个应答共用校验
// Python: interactions.py:34-36 _validate_response_id
func TestNewResponse_空RequestID均被拒绝(t *testing.T) {
	ctors := map[string]func() error{
		"ToolApproval": func() error {
			_, err := NewToolApprovalResponse("", ToolApprovalDecisionAllow, ToolApprovalResponseOptions{})
			return err
		},
		"UserInput": func() error {
			_, err := NewUserInputResponse("", InteractionResponseStatusCompleted, UserInputResponseOptions{})
			return err
		},
		"McpElicitation": func() error {
			_, err := NewMcpElicitationResponse("", InteractionResponseStatusCompleted, McpElicitationResponseOptions{})
			return err
		},
		"DynamicToolCall": func() error {
			_, err := NewDynamicToolCallResponse("", InteractionResponseStatusCompleted, DynamicToolCallResponseOptions{})
			return err
		},
		"ProviderInteraction": func() error {
			_, err := NewProviderInteractionResponse("", InteractionResponseStatusCompleted, ProviderInteractionResponseOptions{})
			return err
		},
	}
	if len(ctors) != 5 {
		t.Fatalf("应答类型数 = %d，上游为 5", len(ctors))
	}
	for name, fn := range ctors {
		t.Run(name, func(t *testing.T) {
			err := fn()
			if err == nil {
				t.Fatal("空 request_id 应被拒绝")
			}
			if !strings.Contains(err.Error(), "response request_id must not be empty") {
				t.Errorf("错误文案不符: %v", err)
			}
		})
	}
}

// TestNewResponse_零值Options不报错 default_factory=dict 归一化守护
func TestNewResponse_零值Options不报错(t *testing.T) {
	if _, err := NewToolApprovalResponse("r1", ToolApprovalDecisionAllow, ToolApprovalResponseOptions{}); err != nil {
		t.Errorf("ToolApprovalResponse: %v", err)
	}
	if _, err := NewUserInputResponse("r1", InteractionResponseStatusCompleted, UserInputResponseOptions{}); err != nil {
		t.Errorf("UserInputResponse: %v", err)
	}
	if _, err := NewMcpElicitationResponse("r1", InteractionResponseStatusCompleted, McpElicitationResponseOptions{}); err != nil {
		t.Errorf("McpElicitationResponse: %v", err)
	}
	if _, err := NewDynamicToolCallResponse("r1", InteractionResponseStatusCompleted, DynamicToolCallResponseOptions{}); err != nil {
		t.Errorf("DynamicToolCallResponse: %v", err)
	}
	if _, err := NewProviderInteractionResponse("r1", InteractionResponseStatusCompleted, ProviderInteractionResponseOptions{}); err != nil {
		t.Errorf("ProviderInteractionResponse: %v", err)
	}
}

// TestNewDynamicToolCallResponse_错误标志透传
// Python: interactions.py:222-237 DynamicToolCallResponse
func TestNewDynamicToolCallResponse_错误标志透传(t *testing.T) {
	msg := "boom"
	got, err := NewDynamicToolCallResponse("r1", InteractionResponseStatusCompleted, DynamicToolCallResponseOptions{
		IsError:      true,
		ErrorMessage: &msg,
		Result:       map[string]any{"partial": true},
	})
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if !got.IsError || got.ErrorMessage == nil || *got.ErrorMessage != "boom" {
		t.Errorf("错误字段未透传: %+v", got)
	}
}

// TestValidateInteractionResponse_五组正例 每种请求配对应答均通过
// Python: interactions.py:273-288 validate_interaction_response
func TestValidateInteractionResponse_五组正例(t *testing.T) {
	cases := []struct {
		name string
		req  HarnessInteractionRequest
		resp HarnessInteractionResponse
	}{
		{"tool_approval", mustToolApprovalReq(t), mustToolApprovalResp(t)},
		{"user_input", mustUserInputReq(t), mustUserInputResp(t)},
		{"mcp_elicitation", mustMcpElicitationReq(t), mustMcpElicitationResp(t)},
		{"dynamic_tool_call", mustDynamicToolCallReq(t), mustDynamicToolCallResp(t)},
		{"provider_interaction", mustProviderInteractionReq(t), mustProviderInteractionResp(t)},
	}
	if len(cases) != 5 {
		t.Fatalf("用例数 = %d，上游 5 组请求-应答配对", len(cases))
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := ValidateInteractionResponse(c.req, c.resp)
			if err != nil {
				t.Fatalf("配对正确不应报错: %v", err)
			}
			if got == nil {
				t.Fatal("应返回原应答对象")
			}
		})
	}
}

// TestValidateInteractionResponse_应答类型错配 对齐上游 test_protocol.py:498-504
func TestValidateInteractionResponse_应答类型错配(t *testing.T) {
	req := mustToolApprovalReq(t)
	resp := mustUserInputResp(t)
	_, err := ValidateInteractionResponse(req, resp)
	if err == nil {
		t.Fatal("类型错配应被拒绝")
	}
	msg := err.Error()
	if !strings.Contains(msg, "requires") || !strings.Contains(msg, "got") {
		t.Errorf("错误文案应含 requires/got: %v", err)
	}
	// Go 侧 kind 是自定字符串（spec §6 B6：与 Python 类名不同）
	if !strings.Contains(msg, "tool_approval_response") || !strings.Contains(msg, "user_input_response") {
		t.Errorf("错误文案应含两个 kind 字面量: %v", err)
	}
}

// TestValidateInteractionResponse_request_id不匹配 对齐上游 test_protocol.py:505-508
func TestValidateInteractionResponse_request_id不匹配(t *testing.T) {
	req := mustToolApprovalReq(t) // request_id = "r1"
	wrong, err := NewToolApprovalResponse("r2", ToolApprovalDecisionDeny, ToolApprovalResponseOptions{})
	if err != nil {
		t.Fatalf("构造失败: %v", err)
	}
	_, err = ValidateInteractionResponse(req, wrong)
	if err == nil {
		t.Fatal("id 不匹配应被拒绝")
	}
	if !strings.Contains(err.Error(), "does not match") {
		t.Errorf("错误文案应含 does not match: %v", err)
	}
}

// TestValidateInteractionResponse_值形态与指针形态结果一致 双形态识别守护
// spec B13：Go 值形态与指针形态是不同动态类型，只识别一种会静默失配
func TestValidateInteractionResponse_值形态与指针形态结果一致(t *testing.T) {
	type pair struct {
		name string
		req  HarnessInteractionRequest
		resp HarnessInteractionResponse
	}
	pairs := []pair{
		{"tool_approval", mustToolApprovalReq(t), mustToolApprovalResp(t)},
		{"user_input", mustUserInputReq(t), mustUserInputResp(t)},
		{"mcp_elicitation", mustMcpElicitationReq(t), mustMcpElicitationResp(t)},
		{"dynamic_tool_call", mustDynamicToolCallReq(t), mustDynamicToolCallResp(t)},
		{"provider_interaction", mustProviderInteractionReq(t), mustProviderInteractionResp(t)},
	}
	if len(pairs) != 5 {
		t.Fatalf("用例数 = %d", len(pairs))
	}
	for _, p := range pairs {
		t.Run(p.name+"_值形态", func(t *testing.T) {
			if _, err := ValidateInteractionResponse(p.req, p.resp); err != nil {
				t.Errorf("值形态应通过: %v", err)
			}
		})
		t.Run(p.name+"_指针形态", func(t *testing.T) {
			reqPtr := p.req
			respPtr := p.resp
			if _, err := ValidateInteractionResponse(reqPtr, respPtr); err != nil {
				t.Errorf("指针形态应通过: %v", err)
			}
		})
	}
}

// TestValidateInteractionResponse_指针形态请求也能取到request_id
// 用 *ToolApprovalRequest 作为接口值时必须能取到 RequestID（否则静默失配）
func TestValidateInteractionResponse_指针形态请求也能取到request_id(t *testing.T) {
	reqVal := mustToolApprovalReq(t)
	reqPtr := &reqVal
	respVal := mustToolApprovalResp(t)
	respPtr := &respVal

	if _, err := ValidateInteractionResponse(reqPtr, respVal); err != nil {
		t.Errorf("指针请求+值应答应通过: %v", err)
	}
	if _, err := ValidateInteractionResponse(reqVal, respPtr); err != nil {
		t.Errorf("值请求+指针应答应通过: %v", err)
	}
	if _, err := ValidateInteractionResponse(reqPtr, respPtr); err != nil {
		t.Errorf("指针请求+指针应答应通过: %v", err)
	}
}

// TestValidateInteractionResponse_指针形态类型错配也报错
func TestValidateInteractionResponse_指针形态类型错配也报错(t *testing.T) {
	reqVal := mustToolApprovalReq(t)
	respVal := mustUserInputResp(t)
	_, err := ValidateInteractionResponse(&reqVal, &respVal)
	if err == nil {
		t.Fatal("指针形态错配应被拒绝（不得静默通过）")
	}
}

// unknownInteractionRequest 未知请求类型，用于 default 分支测试
type unknownInteractionRequest struct{}

// requestKind 返回自定 discriminator。
func (unknownInteractionRequest) requestKind() string { return "unknown" }

// expectedResponseKind 返回自定期望 discriminator。
func (unknownInteractionRequest) expectedResponseKind() string { return "unknown_response" }

// TestValidateInteractionResponse_未知请求类型 default 分支
// Python 侧字典 KeyError 会抛裸异常，Go 收敛为 HarnessProtocolError
func TestValidateInteractionResponse_未知请求类型(t *testing.T) {
	_, err := ValidateInteractionResponse(unknownInteractionRequest{}, mustToolApprovalResp(t))
	if err == nil {
		t.Fatal("未知请求类型应返回 HarnessProtocolError")
	}
	var protoErr *HarnessProtocolError
	if !errors.As(err, &protoErr) {
		t.Errorf("应为 *HarnessProtocolError，实际 %T", err)
	}
}

// fakeInteractionHandler 测试替身：记录 handle/cancel 调用
type fakeInteractionHandler struct {
	// response 返回给 Handle 的应答；nil 时按请求构造 ALLOW / COMPLETED
	response HarnessInteractionResponse
	// handleErr 强制 Handle 返回错误
	handleErr error
	// cancels 记录 requestID 序列
	cancels []string
	// cancelReasons 记录解析后的 reason
	cancelReasons []InteractionCancelReason
	// cancelCalls 记录 Cancel 被调用次数（验证幂等调用可重入）
	cancelCalls int
}

// Handle 按配置应答，未配置时按请求类型构造 ALLOW / COMPLETED。
func (f *fakeInteractionHandler) Handle(_ context.Context, request HarnessInteractionRequest) (HarnessInteractionResponse, error) {
	if f.handleErr != nil {
		return nil, f.handleErr
	}
	if f.response != nil {
		return f.response, nil
	}
	if r, ok := request.(ToolApprovalRequest); ok {
		return NewToolApprovalResponse(r.RequestID, ToolApprovalDecisionAllow, ToolApprovalResponseOptions{})
	}
	return NewUserInputResponse(requestIDOf(request), InteractionResponseStatusCompleted, UserInputResponseOptions{})
}

// Cancel 记录取消原因，零参时取 Python 默认值 provider_withdrew。
func (f *fakeInteractionHandler) Cancel(_ context.Context, requestID string, opts ...CancelOption) error {
	f.cancelCalls++
	reason := InteractionCancelReasonProviderWithdrew // Python 默认值
	for _, opt := range opts {
		if opt != nil {
			opt(&reason)
		}
	}
	f.cancels = append(f.cancels, requestID)
	f.cancelReasons = append(f.cancelReasons, reason)
	return nil
}

var _ HarnessInteractionHandler = (*fakeInteractionHandler)(nil)

// TestHarnessInteractionHandler_两方法齐全 Q2 决策守护
// Python: interactions.py:291-338（handle + cancel）
func TestHarnessInteractionHandler_两方法齐全(t *testing.T) {
	var h HarnessInteractionHandler = &fakeInteractionHandler{}
	ctx := context.Background()

	resp, err := h.Handle(ctx, mustToolApprovalReq(t))
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if _, err := ValidateInteractionResponse(mustToolApprovalReq(t), resp); err != nil {
		t.Fatalf("Handle 返回值应能通过配对校验: %v", err)
	}
	if err := h.Cancel(ctx, "r1"); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
}

// TestHarnessInteractionHandler_Pending方法已删除 Q2 决策守护
func TestHarnessInteractionHandler_Pending方法已删除(t *testing.T) {
	var h HarnessInteractionHandler = &fakeInteractionHandler{}
	typ := reflect.TypeOf(h)
	if typ.NumMethod() != 2 {
		names := make([]string, 0, typ.NumMethod())
		for i := 0; i < typ.NumMethod(); i++ {
			names = append(names, typ.Method(i).Name)
		}
		t.Fatalf("方法数 = %d，期望 2（%v）", typ.NumMethod(), names)
	}
	if _, ok := typ.MethodByName("Pending"); ok {
		t.Error("Pending() 必须已删除（Python interactions.py 无此方法，Q2 决策 A）")
	}
	for _, want := range []string{"Handle", "Cancel"} {
		if _, ok := typ.MethodByName(want); !ok {
			t.Errorf("缺少方法 %s", want)
		}
	}
}

// TestCancel_零参默认ProviderWithdrew Q8 决策 2 的核心守护
// Python: cancel(reason=InteractionCancelReason.PROVIDER_WITHDREW) 默认值
func TestCancel_零参默认ProviderWithdrew(t *testing.T) {
	h := &fakeInteractionHandler{}
	if err := h.Cancel(context.Background(), "r1"); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if len(h.cancelReasons) != 1 {
		t.Fatalf("cancelReasons = %v", h.cancelReasons)
	}
	if h.cancelReasons[0] != InteractionCancelReasonProviderWithdrew {
		t.Errorf("默认 reason = %q，期望 %q", h.cancelReasons[0], InteractionCancelReasonProviderWithdrew)
	}
}

// TestCancel_显式option覆盖 四种 reason 全值
func TestCancel_显式option覆盖(t *testing.T) {
	reasons := []InteractionCancelReason{
		InteractionCancelReasonProviderWithdrew,
		InteractionCancelReasonTurnAborted,
		InteractionCancelReasonHarnessStopped,
		InteractionCancelReasonDeadlineExceeded,
	}
	if len(reasons) != 4 {
		t.Fatalf("取消原因数 = %d，上游为 4", len(reasons))
	}
	for _, want := range reasons {
		t.Run(string(want), func(t *testing.T) {
			h := &fakeInteractionHandler{}
			if err := h.Cancel(context.Background(), "r1", NewCancelOption(want)); err != nil {
				t.Fatalf("Cancel: %v", err)
			}
			if h.cancelReasons[0] != want {
				t.Errorf("reason = %q，期望 %q", h.cancelReasons[0], want)
			}
		})
	}
}

// TestCancel_后者覆盖前者 多个 option 时最后一个胜出
func TestCancel_后者覆盖前者(t *testing.T) {
	h := &fakeInteractionHandler{}
	err := h.Cancel(context.Background(), "r1",
		NewCancelOption(InteractionCancelReasonTurnAborted),
		NewCancelOption(InteractionCancelReasonDeadlineExceeded),
	)
	if err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if h.cancelReasons[0] != InteractionCancelReasonDeadlineExceeded {
		t.Errorf("reason = %q，期望最后设置的 deadline_exceeded", h.cancelReasons[0])
	}
}

// TestCancel_幂等可重入 Cancel 重复调用不报错（接口契约：幂等）
// Python: interactions.py:294-299 "cancel is idempotent"
func TestCancel_幂等可重入(t *testing.T) {
	h := &fakeInteractionHandler{}
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if err := h.Cancel(ctx, "r1"); err != nil {
			t.Fatalf("第 %d 次 Cancel 报错: %v", i+1, err)
		}
	}
	if h.cancelCalls != 3 {
		t.Errorf("cancelCalls = %d，期望 3（证明可重入）", h.cancelCalls)
	}
}

// 测试用构造助手（供本文件内所有用例复用）

func mustToolApprovalReq(t *testing.T) ToolApprovalRequest {
	t.Helper()
	r, err := NewToolApprovalRequest("r1", "call-1", "shell", ToolApprovalRequestOptions{})
	if err != nil {
		t.Fatalf("ToolApprovalRequest: %v", err)
	}
	return r
}

func mustUserInputReq(t *testing.T) UserInputRequest {
	t.Helper()
	r, err := NewUserInputRequest("r1", "prompt", UserInputRequestOptions{})
	if err != nil {
		t.Fatalf("UserInputRequest: %v", err)
	}
	return r
}

func mustMcpElicitationReq(t *testing.T) McpElicitationRequest {
	t.Helper()
	r, err := NewMcpElicitationRequest("r1", "srv", "prompt", McpElicitationRequestOptions{})
	if err != nil {
		t.Fatalf("McpElicitationRequest: %v", err)
	}
	return r
}

func mustDynamicToolCallReq(t *testing.T) DynamicToolCallRequest {
	t.Helper()
	r, err := NewDynamicToolCallRequest("r1", "call-1", "tool", DynamicToolCallRequestOptions{})
	if err != nil {
		t.Fatalf("DynamicToolCallRequest: %v", err)
	}
	return r
}

func mustProviderInteractionReq(t *testing.T) ProviderInteractionRequest {
	t.Helper()
	r, err := NewProviderInteractionRequest("r1", "prov", "type", "1", ProviderInteractionRequestOptions{})
	if err != nil {
		t.Fatalf("ProviderInteractionRequest: %v", err)
	}
	return r
}

func mustToolApprovalResp(t *testing.T) ToolApprovalResponse {
	t.Helper()
	r, err := NewToolApprovalResponse("r1", ToolApprovalDecisionAllow, ToolApprovalResponseOptions{})
	if err != nil {
		t.Fatalf("ToolApprovalResponse: %v", err)
	}
	return r
}

func mustUserInputResp(t *testing.T) UserInputResponse {
	t.Helper()
	r, err := NewUserInputResponse("r1", InteractionResponseStatusCompleted, UserInputResponseOptions{})
	if err != nil {
		t.Fatalf("UserInputResponse: %v", err)
	}
	return r
}

func mustMcpElicitationResp(t *testing.T) McpElicitationResponse {
	t.Helper()
	r, err := NewMcpElicitationResponse("r1", InteractionResponseStatusCompleted, McpElicitationResponseOptions{})
	if err != nil {
		t.Fatalf("McpElicitationResponse: %v", err)
	}
	return r
}

func mustDynamicToolCallResp(t *testing.T) DynamicToolCallResponse {
	t.Helper()
	r, err := NewDynamicToolCallResponse("r1", InteractionResponseStatusCompleted, DynamicToolCallResponseOptions{})
	if err != nil {
		t.Fatalf("DynamicToolCallResponse: %v", err)
	}
	return r
}

func mustProviderInteractionResp(t *testing.T) ProviderInteractionResponse {
	t.Helper()
	r, err := NewProviderInteractionResponse("r1", InteractionResponseStatusCompleted, ProviderInteractionResponseOptions{})
	if err != nil {
		t.Fatalf("ProviderInteractionResponse: %v", err)
	}
	return r
}
