package runtime

import (
	"context"
	"testing"
)

// TestRunContext_默认值 验证 8 个字段的构造默认值
func TestRunContext_默认值(t *testing.T) {
	rc := NewRunContext()
	if rc.RequestID != "" || rc.SessionID != "" {
		t.Error("request_id/session_id 应默认为空串")
	}
	if rc.MaxTurns != nil {
		t.Error("max_turns 应默认为 nil")
	}
	if rc.HostTools == nil || len(rc.HostTools) != 0 {
		t.Error("host_tools 应默认为空元组（非 nil）")
	}
	if rc.MaxBudgetUSD != nil {
		t.Error("max_budget_usd 应默认为 nil")
	}
	if rc.ToolPermissions != nil {
		t.Error("tool_permissions 应默认为 nil")
	}
	if rc.CredentialContext != nil {
		t.Error("credential_context 应默认为 nil")
	}
	if !rc.SupportsPermissionApproval {
		t.Error("supports_permission_approval 应默认为 true")
	}
}

// TestCurrentRunContext_未绑定返回nil 验证 ContextVar default=None
func TestCurrentRunContext_未绑定返回nil(t *testing.T) {
	if got := CurrentRunContext(context.Background()); got != nil {
		t.Errorf("got %v, want nil", got)
	}
}

// TestWithRunContext_绑定与读取 验证 ctx 派生链
func TestWithRunContext_绑定与读取(t *testing.T) {
	rc := &RunContext{
		RequestID: "req-1",
		SessionID: "sess-1",
		ToolPermissions: []ToolPermissionLevel{
			{Name: "bash", Level: "allow"},
		},
	}
	ctx := WithRunContext(context.Background(), rc)

	got := CurrentRunContext(ctx)
	if got == nil {
		t.Fatal("should read back bound RunContext")
	}
	if got.RequestID != "req-1" || got.SessionID != "sess-1" {
		t.Errorf("identity mismatch: %+v", got)
	}
	if len(got.ToolPermissions) != 1 || got.ToolPermissions[0].Name != "bash" {
		t.Errorf("tool_permissions mismatch: %v", got.ToolPermissions)
	}
}

// TestWithRunContext_不污染父ctx 验证 ctx 派生隔离
func TestWithRunContext_不污染父ctx(t *testing.T) {
	parent := context.Background()
	child := WithRunContext(parent, &RunContext{RequestID: "child"})

	if CurrentRunContext(parent) != nil {
		t.Error("parent should not be affected")
	}
	if CurrentRunContext(child) == nil {
		t.Error("child should carry RunContext")
	}
}

// TestWithRunContext_显式清空 验证传 nil 可清空绑定
func TestWithRunContext_显式清空(t *testing.T) {
	ctx := WithRunContext(context.Background(), &RunContext{RequestID: "x"})
	ctx = WithRunContext(ctx, nil)

	if got := CurrentRunContext(ctx); got != nil {
		t.Errorf("explicit nil should clear binding, got %v", got)
	}
}
