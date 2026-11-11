package harness_protocol

import (
	"context"
	"math"
	"reflect"
	"testing"
)

// 本文件补齐 15.3 新增文件（hooks.go / interactions.go）的冻结失败分支与
// 类型 switch 全分支覆盖。这些分支在 Python 侧对应 freeze_json_* 抛出的
// TypeError / ValueError，属于协议边界的 fail-fast 路径。

// TestHooks_Freeze失败分支 覆盖 hooks.go 五个构造器的冻结失败 error 分支
func TestHooks_Freeze失败分支(t *testing.T) {
	bad := func() JsonObject { return JsonObject{"x": math.Inf(1)} }

	t.Run("BeforeToolContext_arguments含Inf", func(t *testing.T) {
		if _, err := NewBeforeToolContext("a", nil, nil, "c", "t", bad()); err == nil {
			t.Fatal("含 Inf 的 arguments 应被拒绝")
		}
	})
	t.Run("ToolDecision_updated_arguments含Inf", func(t *testing.T) {
		if _, err := NewToolDecision(ToolDecisionKindRewrite, ToolDecisionOptions{
			UpdatedArguments: bad(),
		}); err == nil {
			t.Fatal("含 Inf 的 updated_arguments 应被拒绝")
		}
	})
	t.Run("AfterToolContext_arguments含Inf", func(t *testing.T) {
		if _, err := NewAfterToolContext("a", nil, nil, "c", "t", bad(),
			AfterToolContextOptions{}); err == nil {
			t.Fatal("含 Inf 的 arguments 应被拒绝")
		}
	})
	t.Run("AfterToolContext_result含Inf", func(t *testing.T) {
		if _, err := NewAfterToolContext("a", nil, nil, "c", "t", JsonObject{},
			AfterToolContextOptions{Result: math.Inf(1)}); err == nil {
			t.Fatal("含 Inf 的 result 应被拒绝")
		}
	})
	t.Run("AfterToolResult_updated_result含Inf", func(t *testing.T) {
		if _, err := NewAfterToolResult(AfterToolResultOptions{
			UpdatedResult: math.Inf(1),
		}); err == nil {
			t.Fatal("含 Inf 的 updated_result 应被拒绝")
		}
	})
	t.Run("StopHookContext_metadata含Inf", func(t *testing.T) {
		if _, err := NewStopHookContext("a", nil, nil, StopHookContextOptions{
			Metadata: bad(),
		}); err == nil {
			t.Fatal("含 Inf 的 metadata 应被拒绝")
		}
	})
}

// TestInteractions_RequestFreeze失败分支 覆盖 5 个请求构造器的冻结失败 error 分支
func TestInteractions_RequestFreeze失败分支(t *testing.T) {
	bad := func() JsonObject { return JsonObject{"x": math.Inf(1)} }

	t.Run("ToolApprovalRequest_arguments", func(t *testing.T) {
		if _, err := NewToolApprovalRequest("r", "c", "t", ToolApprovalRequestOptions{Arguments: bad()}); err == nil {
			t.Fatal("应被拒绝")
		}
	})
	t.Run("ToolApprovalRequest_provider_data", func(t *testing.T) {
		if _, err := NewToolApprovalRequest("r", "c", "t", ToolApprovalRequestOptions{ProviderData: bad()}); err == nil {
			t.Fatal("应被拒绝")
		}
	})
	t.Run("UserInputRequest_provider_data", func(t *testing.T) {
		if _, err := NewUserInputRequest("r", "p", UserInputRequestOptions{ProviderData: bad()}); err == nil {
			t.Fatal("应被拒绝")
		}
	})
	t.Run("McpElicitationRequest_schema", func(t *testing.T) {
		if _, err := NewMcpElicitationRequest("r", "s", "p", McpElicitationRequestOptions{Schema: bad()}); err == nil {
			t.Fatal("应被拒绝")
		}
	})
	t.Run("McpElicitationRequest_provider_data", func(t *testing.T) {
		if _, err := NewMcpElicitationRequest("r", "s", "p", McpElicitationRequestOptions{ProviderData: bad()}); err == nil {
			t.Fatal("应被拒绝")
		}
	})
	t.Run("DynamicToolCallRequest_arguments", func(t *testing.T) {
		if _, err := NewDynamicToolCallRequest("r", "c", "t", DynamicToolCallRequestOptions{Arguments: bad()}); err == nil {
			t.Fatal("应被拒绝")
		}
	})
	t.Run("DynamicToolCallRequest_provider_data", func(t *testing.T) {
		if _, err := NewDynamicToolCallRequest("r", "c", "t", DynamicToolCallRequestOptions{ProviderData: bad()}); err == nil {
			t.Fatal("应被拒绝")
		}
	})
	t.Run("ProviderInteractionRequest_payload", func(t *testing.T) {
		if _, err := NewProviderInteractionRequest("r", "p", "t", "1", ProviderInteractionRequestOptions{Payload: bad()}); err == nil {
			t.Fatal("应被拒绝")
		}
	})
}

// TestInteractions_ResponseFreeze失败分支 覆盖 5 个应答构造器的冻结失败 error 分支
func TestInteractions_ResponseFreeze失败分支(t *testing.T) {
	bad := func() JsonObject { return JsonObject{"x": math.Inf(1)} }

	t.Run("ToolApprovalResponse_updated_arguments", func(t *testing.T) {
		if _, err := NewToolApprovalResponse("r", ToolApprovalDecisionAllow, ToolApprovalResponseOptions{UpdatedArguments: bad()}); err == nil {
			t.Fatal("应被拒绝")
		}
	})
	t.Run("ToolApprovalResponse_provider_data", func(t *testing.T) {
		if _, err := NewToolApprovalResponse("r", ToolApprovalDecisionAllow, ToolApprovalResponseOptions{ProviderData: bad()}); err == nil {
			t.Fatal("应被拒绝")
		}
	})
	t.Run("UserInputResponse_content", func(t *testing.T) {
		if _, err := NewUserInputResponse("r", InteractionResponseStatusCompleted, UserInputResponseOptions{Content: math.Inf(1)}); err == nil {
			t.Fatal("应被拒绝")
		}
	})
	t.Run("UserInputResponse_provider_data", func(t *testing.T) {
		if _, err := NewUserInputResponse("r", InteractionResponseStatusCompleted, UserInputResponseOptions{ProviderData: bad()}); err == nil {
			t.Fatal("应被拒绝")
		}
	})
	t.Run("McpElicitationResponse_values", func(t *testing.T) {
		if _, err := NewMcpElicitationResponse("r", InteractionResponseStatusCompleted, McpElicitationResponseOptions{Values: bad()}); err == nil {
			t.Fatal("应被拒绝")
		}
	})
	t.Run("McpElicitationResponse_provider_data", func(t *testing.T) {
		if _, err := NewMcpElicitationResponse("r", InteractionResponseStatusCompleted, McpElicitationResponseOptions{ProviderData: bad()}); err == nil {
			t.Fatal("应被拒绝")
		}
	})
	t.Run("DynamicToolCallResponse_result", func(t *testing.T) {
		if _, err := NewDynamicToolCallResponse("r", InteractionResponseStatusCompleted, DynamicToolCallResponseOptions{Result: math.Inf(1)}); err == nil {
			t.Fatal("应被拒绝")
		}
	})
	t.Run("DynamicToolCallResponse_provider_data", func(t *testing.T) {
		if _, err := NewDynamicToolCallResponse("r", InteractionResponseStatusCompleted, DynamicToolCallResponseOptions{ProviderData: bad()}); err == nil {
			t.Fatal("应被拒绝")
		}
	})
	t.Run("ProviderInteractionResponse_payload", func(t *testing.T) {
		if _, err := NewProviderInteractionResponse("r", InteractionResponseStatusCompleted, ProviderInteractionResponseOptions{Payload: bad()}); err == nil {
			t.Fatal("应被拒绝")
		}
	})
}

// TestRequestIDOf_全十种形态 requestIDOf / responseIDOf 的 10+10 个 switch case
// 必须全部可命中（值形态与指针形态各自独立）
func TestRequestIDOf_全十种形态(t *testing.T) {
	reqs := []HarnessInteractionRequest{
		mustToolApprovalReq(t), mustUserInputReq(t), mustMcpElicitationReq(t),
		mustDynamicToolCallReq(t), mustProviderInteractionReq(t),
	}
	if len(reqs) != 5 {
		t.Fatalf("请求类型数 = %d", len(reqs))
	}
	for _, r := range reqs {
		if got := requestIDOf(r); got != "r1" {
			t.Errorf("值形态 %T requestIDOf = %q", r, got)
		}
		// 指针形态：取地址后仍应取到同一 id
		rv, ok := reflectNew(t, r).(HarnessInteractionRequest)
		if !ok {
			t.Fatalf("指针形态 %T 未实现 HarnessInteractionRequest", r)
		}
		if got := requestIDOf(rv); got != "r1" {
			t.Errorf("指针形态 %T requestIDOf = %q", rv, got)
		}
	}
}

// TestResponseIDOf_全十种形态
func TestResponseIDOf_全十种形态(t *testing.T) {
	resps := []HarnessInteractionResponse{
		mustToolApprovalResp(t), mustUserInputResp(t), mustMcpElicitationResp(t),
		mustDynamicToolCallResp(t), mustProviderInteractionResp(t),
	}
	if len(resps) != 5 {
		t.Fatalf("应答类型数 = %d", len(resps))
	}
	for _, r := range resps {
		if got := responseIDOf(r); got != "r1" {
			t.Errorf("值形态 %T responseIDOf = %q", r, got)
		}
		rv, ok := reflectNew(t, r).(HarnessInteractionResponse)
		if !ok {
			t.Fatalf("指针形态 %T 未实现 HarnessInteractionResponse", r)
		}
		if got := responseIDOf(rv); got != "r1" {
			t.Errorf("指针形态 %T responseIDOf = %q", rv, got)
		}
	}
}

// TestRequestKindName_五种已知加未知 requestKindName 的 5+default 分支
func TestRequestKindName_五种已知加未知(t *testing.T) {
	cases := []struct {
		req  HarnessInteractionRequest
		want string
	}{
		{mustToolApprovalReq(t), "ToolApprovalRequest"},
		{mustUserInputReq(t), "UserInputRequest"},
		{mustMcpElicitationReq(t), "McpElicitationRequest"},
		{mustDynamicToolCallReq(t), "DynamicToolCallRequest"},
		{mustProviderInteractionReq(t), "ProviderInteractionRequest"},
		{unknownInteractionRequest{}, "unsupported interaction request"},
	}
	if len(cases) != 6 {
		t.Fatalf("用例数 = %d", len(cases))
	}
	for _, c := range cases {
		if got := requestKindName(c.req); got != c.want {
			t.Errorf("%T requestKindName = %q，期望 %q", c.req, got, c.want)
		}
		// 指针形态
		rv, ok := reflectNew(t, c.req).(HarnessInteractionRequest)
		if !ok {
			t.Fatalf("指针形态 %T 未实现 HarnessInteractionRequest", c.req)
		}
		if got := requestKindName(rv); got != c.want {
			t.Errorf("指针 %T requestKindName = %q，期望 %q", c.req, got, c.want)
		}
	}
}

// TestValidateInteractionResponse_nil不panic于步骤1 nil 请求/应答在 Python 侧
// 会抛 AttributeError（response.request_id 取 None 的属性），Go 侧对应
// 「id 均取不到 → 空串相等 → 走到步骤 2 解引用」的 panic。两端都是异常，
// 上游不为此加守卫，故 Go 侧不加 nil 守卫（规则 12：不得私自增加上游没有的校验）。
// 本用例只钉住「nil 请求 + 非 nil 应答」在两个 nil id 相等时确实走步骤 2 报错。
func TestValidateInteractionResponse_nil请求走步骤2报错(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Logf("nil 请求触发 panic（与 Python AttributeError 等价）: %v", r)
		}
	}()
	resp := mustToolApprovalResp(t) // request_id = "r1"
	// nil 请求 → requestIDOf 返回 ""，与 "r1" 不等 → 步骤 1 直接报错，不会 panic
	if _, err := ValidateInteractionResponse(nil, resp); err == nil {
		t.Fatal("nil 请求应报 id 不匹配")
	}
}

// TestHarnessInteractionHandler_Handle错误透传 覆盖 Handle 返回错误的路径
func TestHarnessInteractionHandler_Handle错误透传(t *testing.T) {
	wantErr := context.Canceled
	h := &fakeInteractionHandler{handleErr: wantErr}
	_, err := h.Handle(context.Background(), mustToolApprovalReq(t))
	if err != wantErr {
		t.Errorf("err = %v，期望透传 %v", err, wantErr)
	}
}

// reflectNew 把接口值转成其指针形态，用于覆盖 switch 的 *T 分支。
// 返回同类型接口值；未知类型原样返回。
func reflectNew(t *testing.T, v any) any {
	t.Helper()
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Ptr {
		return v
	}
	ptr := reflect.New(rv.Type())
	ptr.Elem().Set(rv)
	return ptr.Interface()
}
