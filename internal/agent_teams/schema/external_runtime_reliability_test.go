package schema

import "testing"

func TestUserActionRequired(t *testing.T) {
	// 闭合集内逐项 + 集外默认 False（Python user_action_required .get(category, False)）
	if !UserActionRequired(ExternalRuntimeFailureCategoryAuthRequired) {
		t.Fatal("auth_required 应为 true")
	}
	if !UserActionRequired(ExternalRuntimeFailureCategoryQuotaExceeded) ||
		!UserActionRequired(ExternalRuntimeFailureCategoryRequestRejected) ||
		!UserActionRequired(ExternalRuntimeFailureCategoryProcessStartFailed) {
		t.Fatal("user-action 类别应为 true")
	}
	if UserActionRequired(ExternalRuntimeFailureCategoryRateLimited) ||
		UserActionRequired(ExternalRuntimeFailureCategorySDKError) ||
		UserActionRequired(ExternalRuntimeFailureCategoryUnknown) {
		t.Fatal("非 user-action 类别应为 false")
	}
	if UserActionRequired("no_such_category") {
		t.Fatal("集外类别默认 false")
	}
}

func TestExternalRuntimeFailureReason_默认值(t *testing.T) {
	r := NewExternalRuntimeFailureReason("", "", "", nil)
	if r.Message != "" || r.SDKErrorType != "" || r.SDKErrorCode != "" || r.HTTPStatus != nil {
		t.Fatalf("零值不符: %+v", r)
	}
}

func TestExternalRuntimeFailure_构造(t *testing.T) {
	f := NewExternalRuntimeFailure("fid-1", "team-a", "m1", "claude", "", ExternalRuntimePhaseStartup,
		ExternalRuntimeFailureCategoryAuthRequired, "需要登录", "请重新登录", nil, nil)
	if f.Type != "external_runtime_failed" || f.Category != ExternalRuntimeFailureCategoryAuthRequired || !f.UserActionRequired {
		t.Fatalf("字段不符: %+v", f)
	}
	if f.Reason == nil {
		t.Fatal("reason 为 nil 时应回落默认原因（Python default_factory）")
	}
	if f.Phase != ExternalRuntimePhaseStartup {
		t.Fatalf("phase 不符: %s", f.Phase)
	}
}
