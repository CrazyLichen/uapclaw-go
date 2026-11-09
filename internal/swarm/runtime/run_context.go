// 一次调用持有的不可变选项，由其 session 消费一次。
//
// 对应 Python 代码：jiuwenswarm/runtime/run_context.py（20 行）
//
// 语言差异：Python 用 ContextVar[RunContext | None] 隐式传递（default=None），
// Go 用 context.Context 派生值显式传递。Python 的 `_cwd_state.set()/reset()`
// 对应 Go 的 WithRunContext / ctx 派生。
//
// 本结构被 run_permissions.go 的兜底路径消费：当请求级 RUN_PERMISSIONS
// 未绑定时，从 CURRENT_RUN_CONTEXT.tool_permissions 读取同一份选择。
// 其余 7 个字段由后续章节（请求入口装配）消费，本批按规则 9 全量落地。

package runtime

import "context"

// ──────────────────────────── 结构体 ────────────────────────────

// RunContext 一次调用持有的不可变选项。
//
// Python: @dataclass(frozen=True, slots=True) RunContext
type RunContext struct {
	// RequestID 请求标识
	RequestID string
	// SessionID 会话标识
	SessionID string
	// MaxTurns 最大轮次，nil 表示未设置（Python None）
	MaxTurns *int
	// HostTools 宿主工具列表，默认空（Python field(default_factory=tuple)）
	HostTools []any
	// MaxBudgetUSD 预算上限，nil 表示未设置
	MaxBudgetUSD *float64
	// ToolPermissions 请求级工具权限选择（二元组列表），nil 表示未设置。
	// Python: tuple[tuple[str, str], ...] | None
	ToolPermissions []ToolPermissionLevel
	// CredentialContext 凭证上下文，默认 nil
	CredentialContext any
	// SupportsPermissionApproval 是否支持权限审批，默认 true
	SupportsPermissionApproval bool
}

// ToolPermissionLevel 一项工具权限选择（对应 Python 的 (name, level) 二元组）。
//
// Python: tool_permissions: tuple[tuple[str, str], ...]
type ToolPermissionLevel struct {
	// Name 工具名（可能未归一，消费方需 NormalizePermissionToolName）
	Name string
	// Level 权限级别（allow / ask / deny）
	Level string
}

// runContextKey RunContext 的 context key 类型。
type runContextKey struct{}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewRunContext 构造带默认值的 RunContext。
//
// Python: RunContext()（dataclass 默认值）
func NewRunContext() *RunContext {
	return &RunContext{
		HostTools:                  []any{},
		SupportsPermissionApproval: true,
	}
}

// WithRunContext 把 RunContext 绑定到 ctx，返回派生后的 ctx。
// 传 nil 表示显式清空绑定。
//
// Python: CURRENT_RUN_CONTEXT.set(context)
func WithRunContext(ctx context.Context, rc *RunContext) context.Context {
	return context.WithValue(ctx, runContextKey{}, rc)
}

// CurrentRunContext 从 ctx 读取 RunContext，未绑定返回 nil。
//
// Python: CURRENT_RUN_CONTEXT.get()
func CurrentRunContext(ctx context.Context) *RunContext {
	rc, ok := ctx.Value(runContextKey{}).(*RunContext)
	if !ok {
		return nil
	}
	return rc
}
