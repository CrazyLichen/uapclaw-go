// 一次调用持有的不可变选项，由其 session 消费一次。
//
// 对应 Python 代码：jiuwenswarm/runtime/run_context.py（20 行）
//
// 语言差异：Python 用 ContextVar[RunContext | None] 隐式传递（default=None），
// Go 用 context.Context 派生值显式传递。Python 的
// `CURRENT_RUN_CONTEXT.set()` / `.get()` 对应 Go 的 WithRunContext /
// CurrentRunContext / ctx 派生。
//
// Python 的 dataclass(frozen=True) 自动生成 `__eq__`，其中 credential_context
// 标了 `compare=False`（相等性判断时忽略）。Go struct 无内建相等语义，故由
// RunContextEqual 显式实现同一规则；直接 `*a == *b` 不可用（含 slice/map 字段
// 不可比较，且会把 credential_context 也算进去）。
//
// 本结构被 run_permissions.go 的兜底路径消费：当请求级 RUN_PERMISSIONS
// 未绑定时，从 CURRENT_RUN_CONTEXT.tool_permissions 读取同一份选择。
// 其余 7 个字段由后续章节（请求入口装配）消费，本批按规则 9 全量落地。

package runtime

import (
	"context"
	"reflect"
)

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

// RunContextEqual 比较两个 RunContext 的相等性，**忽略 CredentialContext**。
//
// Python: dataclass(frozen=True) 自动生成的 __eq__，其中
// `credential_context: Any = field(default=None, repr=False, compare=False)`
// 明确豁免于相等性判断——凭证只影响"用哪个凭据"，不构成"是不是同一次调用选项"。
//
// Go struct 不能直接 `*a == *b`：HostTools（slice）与 ToolPermissions（slice）
// 不可比较，编译期即失败。故逐字段比较，slice 按元素逐一比。
//
// nil 与 nil 相等；一 nil 一非 nil 不等。
func RunContextEqual(a, b *RunContext) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	if a.RequestID != b.RequestID || a.SessionID != b.SessionID {
		return false
	}
	if !optionalIntEqual(a.MaxTurns, b.MaxTurns) {
		return false
	}
	if !optionalFloatEqual(a.MaxBudgetUSD, b.MaxBudgetUSD) {
		return false
	}
	if !anySliceEqual(a.HostTools, b.HostTools) {
		return false
	}
	if !toolPermissionLevelsEqual(a.ToolPermissions, b.ToolPermissions) {
		return false
	}
	if a.SupportsPermissionApproval != b.SupportsPermissionApproval {
		return false
	}
	// CredentialContext 按上游 compare=False 语义跳过。
	return true
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// optionalIntEqual 比较两个可选整数。
func optionalIntEqual(a, b *int) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// optionalFloatEqual 比较两个可选浮点。
func optionalFloatEqual(a, b *float64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// anySliceEqual 按元素比较两个 any 切片（nil 与空切片视为不同，同 Python tuple 语义）。
//
// 元素用 == 比较；不可比较的元素类型（map/slice/func）会 panic，
// 故用反射先判断可比性，不可比时退化为"同一指针视为相等"。
func anySliceEqual(a, b []any) bool {
	if len(a) != len(b) {
		return false
	}
	for index := range a {
		if !equalAnyValue(a[index], b[index]) {
			return false
		}
	}
	return true
}

// toolPermissionLevelsEqual 比较两个工具权限列表（顺序敏感，同 Python tuple 语义）。
func toolPermissionLevelsEqual(a, b []ToolPermissionLevel) bool {
	if len(a) != len(b) {
		return false
	}
	for index := range a {
		if a[index] != b[index] {
			return false
		}
	}
	return true
}

// equalAnyValue 比较两个 any 值，不可比较类型退化为恒不等（避免 panic）。
func equalAnyValue(a, b any) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	leftType := reflect.TypeOf(a)
	if leftType != reflect.TypeOf(b) {
		return false
	}
	if !leftType.Comparable() {
		// map / slice / func 等不可比较：按引用相等判定，不同实例即视为不等。
		return reflect.ValueOf(a).Pointer() == reflect.ValueOf(b).Pointer()
	}
	return a == b
}
