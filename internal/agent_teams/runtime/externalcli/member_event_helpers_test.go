package externalcli

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	harness_protocol "github.com/uapclaw/uapclaw-go/internal/agentcore/harness_protocol"
)

func strPtrT(s string) *string { return &s }

// mustJsonObject 构造冻结 JSON 对象（测试辅助）。
func mustJsonObject(t *testing.T, value map[string]any) harness_protocol.JsonObject {
	t.Helper()
	obj, err := harness_protocol.FreezeJsonObject(value)
	if err != nil {
		t.Fatalf("freeze: %v", err)
	}
	return obj
}

func TestFailureReason_形状(t *testing.T) {
	code := "E-1"
	status := 429
	r := failureReason("boom", &code, "TimeoutError", &status)
	if r.Message != "boom" || r.SDKErrorType != "TimeoutError" || r.SDKErrorCode != "E-1" || r.HTTPStatus == nil || *r.HTTPStatus != 429 {
		t.Fatalf("不符: %+v", r)
	}
	// Python or-default：空实参 → 空串；非 int http_status → nil
	r2 := failureReason("", nil, nil, "not-int")
	if r2.Message != "" || r2.SDKErrorType != "" || r2.SDKErrorCode != "" || r2.HTTPStatus != nil {
		t.Fatalf("or-default 不符: %+v", r2)
	}
	// json 往返的整值 float64 → *int（Python isinstance(int) 分支）
	r3 := failureReason("m", nil, nil, float64(500))
	if r3.HTTPStatus == nil || *r3.HTTPStatus != 500 {
		t.Fatalf("整值 float64 应收 int: %+v", r3)
	}
	// 非整值 float64 → nil
	r4 := failureReason("m", nil, nil, 500.5)
	if r4.HTTPStatus != nil {
		t.Fatalf("非整值 float64 应 nil: %+v", r4)
	}
}

func TestClassifyFailure(t *testing.T) {
	// 无结构化错误 → sdk_error + 固定文案
	cat, reason := classifyFailure(nil)
	if cat != schema.ExternalRuntimeFailureCategorySDKError {
		t.Fatalf("nil 错误分类: %s", cat)
	}
	if reason.Message != "external harness turn failed without a structured error" {
		t.Fatalf("nil 错误文案: %+v", reason)
	}
	// 类别在 USER_ACTION_REQUIRED 闭集内 → 保留
	cat, r := classifyFailure(&harness_protocol.TurnError{
		Message:  "need login",
		Category: strPtrT("auth_required"),
	})
	if cat != schema.ExternalRuntimeFailureCategoryAuthRequired {
		t.Fatalf("闭集内类别应保留: %s", cat)
	}
	if r.Message != "need login" {
		t.Fatalf("reason.message 应透传: %+v", r)
	}
	// 类别在闭集外 → sdk_error；provider_data 取 sdk_error_type/http_status
	status := 500.0
	cat, r2 := classifyFailure(&harness_protocol.TurnError{
		Message:  "bad",
		Category: strPtrT("weird"),
		Code:     strPtrT("E9"),
		ProviderData: mustJsonObject(t, map[string]any{
			"sdk_error_type": "APIError",
			"http_status":    status,
		}),
	})
	if cat != schema.ExternalRuntimeFailureCategorySDKError {
		t.Fatalf("闭集外应归 sdk_error: %s", cat)
	}
	if r2.SDKErrorType != "APIError" || r2.SDKErrorCode != "E9" || r2.HTTPStatus == nil || *r2.HTTPStatus != 500 {
		t.Fatalf("provider_data 提取不符: %+v", r2)
	}
	// provider_data 非 dict → 空 data
	cat, r3 := classifyFailure(&harness_protocol.TurnError{Message: "x", Category: strPtrT("weird")})
	if cat != schema.ExternalRuntimeFailureCategorySDKError || r3.SDKErrorType != "" || r3.HTTPStatus != nil {
		t.Fatalf("无 provider_data 分支不符: %s %+v", cat, r3)
	}
}
