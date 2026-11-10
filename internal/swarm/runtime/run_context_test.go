package runtime

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
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

// ──────────────────────────── 相等语义（compare=False） ────────────────────────────

// TestRunContextEqual_相同字段相等 八个字段一致时相等。
// Python: dataclass(frozen=True) 生成的 __eq__
func TestRunContextEqual_相同字段相等(t *testing.T) {
	turns, budget := 3, 1.5
	a := &RunContext{
		RequestID: "r1", SessionID: "s1",
		MaxTurns: &turns, MaxBudgetUSD: &budget,
		HostTools:                  []any{"a", 1},
		ToolPermissions:            []ToolPermissionLevel{{Name: "bash", Level: "allow"}},
		SupportsPermissionApproval: true,
	}
	b := &RunContext{
		RequestID: "r1", SessionID: "s1",
		MaxTurns: &turns, MaxBudgetUSD: &budget,
		HostTools:                  []any{"a", 1},
		ToolPermissions:            []ToolPermissionLevel{{Name: "bash", Level: "allow"}},
		SupportsPermissionApproval: true,
	}
	require.True(t, RunContextEqual(a, b))
}

// TestRunContextEqual_忽略CredentialContext credential_context 不参与相等性。
// Python: credential_context: Any = field(default=None, repr=False, compare=False)
func TestRunContextEqual_忽略CredentialContext(t *testing.T) {
	a := &RunContext{RequestID: "r1", CredentialContext: map[string]any{"token": "x"}}
	b := &RunContext{RequestID: "r1", CredentialContext: map[string]any{"token": "y"}}
	require.True(t, RunContextEqual(a, b), "凭证不同不影响相等性")

	b.CredentialContext = nil
	require.True(t, RunContextEqual(a, b), "一侧 nil 也忽略")
}

// TestRunContextEqual_逐字段不等 任一对比字段不同即不等。
func TestRunContextEqual_逐字段不等(t *testing.T) {
	base := &RunContext{RequestID: "r1", SessionID: "s1", SupportsPermissionApproval: true}
	cases := map[string]*RunContext{
		"request_id":  {RequestID: "r2", SessionID: "s1", SupportsPermissionApproval: true},
		"session_id":  {RequestID: "r1", SessionID: "s2", SupportsPermissionApproval: true},
		"approval标志位": {RequestID: "r1", SessionID: "s1", SupportsPermissionApproval: false},
		"host_tools":  {RequestID: "r1", SessionID: "s1", SupportsPermissionApproval: true, HostTools: []any{"x"}},
		"tool_permissions": {RequestID: "r1", SessionID: "s1", SupportsPermissionApproval: true,
			ToolPermissions: []ToolPermissionLevel{{Name: "bash", Level: "deny"}}},
	}
	for name, other := range cases {
		require.False(t, RunContextEqual(base, other), "字段 %s 不同应不等", name)
	}
}

// TestRunContextEqual_可选项nil与值不等 nil 与有值不同。
func TestRunContextEqual_可选项nil与值不等(t *testing.T) {
	turns := 3
	a := &RunContext{SupportsPermissionApproval: true}
	b := &RunContext{SupportsPermissionApproval: true, MaxTurns: &turns}
	require.False(t, RunContextEqual(a, b))

	budget := 2.0
	c := &RunContext{SupportsPermissionApproval: true, MaxBudgetUSD: &budget}
	require.False(t, RunContextEqual(a, c), "预算 nil 与有值不同")
}

// TestRunContextEqual_slice元素不等 同长度但元素不同不等。
func TestRunContextEqual_slice元素不等(t *testing.T) {
	a := &RunContext{HostTools: []any{"a", "b"}}
	b := &RunContext{HostTools: []any{"a", "c"}}
	require.False(t, RunContextEqual(a, b))

	c := &RunContext{HostTools: []any{"a"}}
	require.False(t, RunContextEqual(a, c), "长度不同")
}

// TestRunContextEqual_不可比较元素不panic map 元素走引用相等，不 panic。
func TestRunContextEqual_不可比较元素不panic(t *testing.T) {
	shared := map[string]any{"k": "v"}
	a := &RunContext{HostTools: []any{shared}}
	b := &RunContext{HostTools: []any{shared}}
	require.True(t, RunContextEqual(a, b), "同一 map 实例相等")

	c := &RunContext{HostTools: []any{map[string]any{"k": "v"}}}
	require.False(t, RunContextEqual(a, c), "不同实例不等")
}

// TestRunContextEqual_nil语义 两 nil 相等，一 nil 一非 nil 不等。
func TestRunContextEqual_nil语义(t *testing.T) {
	require.True(t, RunContextEqual(nil, nil))
	require.False(t, RunContextEqual(nil, &RunContext{}))
	require.False(t, RunContextEqual(&RunContext{}, nil))
}

// TestRunContextEqual_工具权限顺序敏感 顺序不同即不等（Python tuple 语义）。
func TestRunContextEqual_工具权限顺序敏感(t *testing.T) {
	a := &RunContext{ToolPermissions: []ToolPermissionLevel{
		{Name: "a", Level: "allow"}, {Name: "b", Level: "deny"},
	}}
	b := &RunContext{ToolPermissions: []ToolPermissionLevel{
		{Name: "b", Level: "deny"}, {Name: "a", Level: "allow"},
	}}
	require.False(t, RunContextEqual(a, b))
}
