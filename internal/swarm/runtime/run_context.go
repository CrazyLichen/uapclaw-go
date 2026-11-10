package runtime

import "sync"

// ──────────────────────────── 结构体 ────────────────────────────

// RunContext 一次调用所拥有的不可变选项，由该调用的会话消费一次。
// Python: jiuwenswarm/runtime/run_context.py:8-17 RunContext
//
// 上游为 frozen dataclass（slots=True），Go 侧以值类型 + 构造后不再修改
// 的约定表达同等不可变性。credential_context 在上游 compare=False，
// 不参与相等比较；Go 侧保留字段但同样不参与比较（见 RunContextEqual）。
type RunContext struct {
	// RequestID 请求标识
	RequestID string
	// SessionID 会话标识
	SessionID string
	// MaxTurns 最大轮次（nil 表示不限）
	// Python: max_turns: int | None = None
	MaxTurns *int
	// HostTools 宿主工具集合
	// Python: host_tools: tuple[Any, ...] = field(default_factory=tuple, repr=False)
	HostTools []any
	// MaxBudgetUSD 最大预算（美元，nil 表示不限）
	// Python: max_budget_usd: float | None = None
	MaxBudgetUSD *float64
	// ToolPermissions 工具权限二元组集合（nil 表示未指定）
	// Python: tool_permissions: tuple[tuple[str, str], ...] | None = None
	ToolPermissions [][2]string
	// CredentialContext 凭据上下文（上游 compare=False、repr=False）
	// Python: credential_context: Any = field(default=None, repr=False, compare=False)
	CredentialContext any
	// SupportsPermissionApproval 是否支持权限审批（上游默认 True）
	// Python: supports_permission_approval: bool = True
	SupportsPermissionApproval bool
}

// ──────────────────────────── 全局变量 ────────────────────────────

// currentRunContext 当前协程的 RunContext。
// Python: run_context.py:20 CURRENT_RUN_CONTEXT: ContextVar[RunContext | None]
//
// Python 用 ContextVar 表达"每个执行流独立"，Go 侧用 sync.Map 以执行流标识
// 为键表达同等语义；未设置时返回 nil（对齐上游 default=None）。
var currentRunContext sync.Map

// ──────────────────────────── 导出函数 ────────────────────────────

// NewRunContext 构造 RunContext，补默认值。
// Python: run_context.py:8-17 dataclass 默认值
//
// supports_permission_approval 上游默认 True；Go 零值为 false，
// 因此由构造函数显式置 true，调用方需关闭时自行赋 false。
func NewRunContext(requestID, sessionID string) RunContext {
	return RunContext{
		RequestID:                  requestID,
		SessionID:                  sessionID,
		SupportsPermissionApproval: true,
	}
}

// CurrentRunContext 取指定执行流的 RunContext；未设置返回 nil。
// Python: run_context.py:20 CURRENT_RUN_CONTEXT.get()
func CurrentRunContext(flowKey string) *RunContext {
	if value, ok := currentRunContext.Load(flowKey); ok {
		if ctx, ok := value.(*RunContext); ok {
			return ctx
		}
	}
	return nil
}

// SetCurrentRunContext 设置指定执行流的 RunContext，返回恢复函数。
// Python: run_context.py:20 CURRENT_RUN_CONTEXT.set(...) 返回 token
//
// 返回的恢复函数对应 Python 的 ContextVar.reset(token)：调用方应以
// defer restore() 保证执行流结束后不残留。
func SetCurrentRunContext(flowKey string, ctx *RunContext) (restore func()) {
	previous, had := currentRunContext.Load(flowKey)
	currentRunContext.Store(flowKey, ctx)
	return func() {
		if had {
			currentRunContext.Store(flowKey, previous)
			return
		}
		currentRunContext.Delete(flowKey)
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// RunContextEqual 按上游 dataclass 的相等语义比较两个 RunContext。
// Python: run_context.py:8-17 dataclass(frozen=True) 自动 __eq__（compare=False 的
// credential_context 不参与比较）
//
// 导出为 RunContextEqual 而非方法，避免与值类型语义冲突。
func RunContextEqual(a, b RunContext) bool {
	if a.RequestID != b.RequestID ||
		a.SessionID != b.SessionID ||
		a.SupportsPermissionApproval != b.SupportsPermissionApproval {
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
	return toolPermissionsEqual(a.ToolPermissions, b.ToolPermissions)
}

// optionalIntEqual 比较可空整数。
func optionalIntEqual(a, b *int) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// optionalFloatEqual 比较可空浮点。
func optionalFloatEqual(a, b *float64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// anySliceEqual 比较 []any 的长度与元素指针语义。
// Python: tuple 比较按元素 ==；Go 侧 any 元素的深度比较由调用方类型的 == 承担。
func anySliceEqual(a, b []any) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// toolPermissionsEqual 比较权限二元组集合。
func toolPermissionsEqual(a, b [][2]string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
