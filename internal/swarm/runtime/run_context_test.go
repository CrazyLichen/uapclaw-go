package runtime

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestNewRunContext_默认值 构造后 supports_permission_approval 默认 true，其余零值。
// Python: run_context.py:8-17 dataclass 默认值（supports_permission_approval=True）
func TestNewRunContext_默认值(t *testing.T) {
	ctx := NewRunContext("req-1", "sess-1")
	require.Equal(t, "req-1", ctx.RequestID)
	require.Equal(t, "sess-1", ctx.SessionID)
	require.Nil(t, ctx.MaxTurns)
	require.Empty(t, ctx.HostTools)
	require.Nil(t, ctx.MaxBudgetUSD)
	require.Nil(t, ctx.ToolPermissions)
	require.Nil(t, ctx.CredentialContext)
	require.True(t, ctx.SupportsPermissionApproval, "上游默认 True，Go 零值 false 须由构造补齐")
}

// TestCurrentRunContext_未设置返回nil 对齐上游 ContextVar default=None。
// Python: run_context.py:20 ContextVar(default=None)
func TestCurrentRunContext_未设置返回nil(t *testing.T) {
	require.Nil(t, CurrentRunContext("flow-never-set"))
}

// TestSetCurrentRunContext_读取一致 设置后能读到同一份值。
// Python: run_context.py:20 CURRENT_RUN_CONTEXT.set(...)
func TestSetCurrentRunContext_读取一致(t *testing.T) {
	ctx := NewRunContext("req-1", "sess-1")
	restore := SetCurrentRunContext("flow-a", &ctx)
	defer restore()

	got := CurrentRunContext("flow-a")
	require.NotNil(t, got)
	require.Equal(t, "req-1", got.RequestID)
}

// TestSetCurrentRunContext_恢复清空 恢复函数在原先未设置时删除条目。
// Python: ContextVar.reset(token) 回到未设置状态
func TestSetCurrentRunContext_恢复清空(t *testing.T) {
	ctx := NewRunContext("req-1", "sess-1")
	restore := SetCurrentRunContext("flow-b", &ctx)
	restore()
	require.Nil(t, CurrentRunContext("flow-b"))
}

// TestSetCurrentRunContext_恢复原值 嵌套设置后恢复回外层值。
// Python: ContextVar token 可精确回滚一个层级
func TestSetCurrentRunContext_恢复原值(t *testing.T) {
	outer := NewRunContext("outer", "s1")
	restoreOuter := SetCurrentRunContext("flow-c", &outer)
	defer restoreOuter()

	inner := NewRunContext("inner", "s1")
	restoreInner := SetCurrentRunContext("flow-c", &inner)

	require.Equal(t, "inner", CurrentRunContext("flow-c").RequestID)
	restoreInner()
	require.Equal(t, "outer", CurrentRunContext("flow-c").RequestID)
}

// TestSetCurrentRunContext_不同执行流互不影响 每个 flowKey 独立。
// Python: ContextVar 每个执行流独立
func TestSetCurrentRunContext_不同执行流互不影响(t *testing.T) {
	a := NewRunContext("a", "s1")
	b := NewRunContext("b", "s2")
	restoreA := SetCurrentRunContext("flow-x", &a)
	defer restoreA()
	restoreB := SetCurrentRunContext("flow-y", &b)
	defer restoreB()

	require.Equal(t, "a", CurrentRunContext("flow-x").RequestID)
	require.Equal(t, "b", CurrentRunContext("flow-y").RequestID)
}

// TestRunContextEqual_忽略凭据上下文 credential_context 上游 compare=False 不参与比较。
// Python: run_context.py:16 field(compare=False)
func TestRunContextEqual_忽略凭据上下文(t *testing.T) {
	a := NewRunContext("r", "s")
	a.CredentialContext = "cred-A"
	b := NewRunContext("r", "s")
	b.CredentialContext = "cred-B"
	require.True(t, RunContextEqual(a, b), "credential_context 不参与相等比较")
}

// TestRunContextEqual_其余字段参与比较 任一业务字段不同即不等。
func TestRunContextEqual_其余字段参与比较(t *testing.T) {
	base := NewRunContext("r", "s")

	other := base
	other.RequestID = "r2"
	require.False(t, RunContextEqual(base, other))

	other = base
	other.SupportsPermissionApproval = false
	require.False(t, RunContextEqual(base, other))

	one := 1
	other = base
	other.MaxTurns = &one
	require.False(t, RunContextEqual(base, other))

	budget := 1.5
	other = base
	other.MaxBudgetUSD = &budget
	require.False(t, RunContextEqual(base, other))

	other = base
	other.HostTools = []any{"tool"}
	require.False(t, RunContextEqual(base, other))

	other = base
	other.ToolPermissions = [][2]string{{"file", "allow"}}
	require.False(t, RunContextEqual(base, other))
}

// TestRunContextEqual_可选字段同为nil视为相等 两侧都未设置时相等。
func TestRunContextEqual_可选字段同为nil视为相等(t *testing.T) {
	require.True(t, RunContextEqual(NewRunContext("r", "s"), NewRunContext("r", "s")))
}
