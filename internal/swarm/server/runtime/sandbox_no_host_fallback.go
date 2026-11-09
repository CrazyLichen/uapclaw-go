// 任务级执行域：禁止沙箱到宿主的回退。
//
// 对应 Python 代码：jiuwenswarm/server/runtime/sandbox_no_host_fallback.py（38 行）
//
// 语言差异：Python 用 ContextVar[bool] 隐式传递，Go 用 context.Context
// 派生值显式传递（与 sys_operation/cwd 同款设计）。
// 因此 Python 的三个函数在 Go 侧分别是读值函数与返回新 ctx 的派生函数。

package runtime

import "context"

// ──────────────────────────── 常量 ────────────────────────────

// noHostFallbackKey 标记值的 context key 类型（避免与其他包冲突）。
//
// Python: _NO_HOST_FALLBACK = ContextVar("jiuwenswarm_no_host_fallback", default=False)
type noHostFallbackKey struct{}

// ──────────────────────────── 导出函数 ────────────────────────────

// NoHostFallbackRequired 返回当前沙箱操作是否必须留在非宿主域。
//
// Python: no_host_fallback_required() -> bool
func NoHostFallbackRequired(ctx context.Context) bool {
	v, ok := ctx.Value(noHostFallbackKey{}).(bool)
	return ok && v
}

// RequireNoHostFallback 要求下一次沙箱操作失败而非回退到宿主，
// 返回派生后的 ctx。
//
// Python: require_no_host_fallback() -> None（_NO_HOST_FALLBACK.set(True)）
func RequireNoHostFallback(ctx context.Context) context.Context {
	return context.WithValue(ctx, noHostFallbackKey{}, true)
}

// ClearNoHostFallback 在终态操作后清除任务级执行域，返回派生后的 ctx。
//
// Python: clear_no_host_fallback() -> None（_NO_HOST_FALLBACK.set(False)）
func ClearNoHostFallback(ctx context.Context) context.Context {
	return context.WithValue(ctx, noHostFallbackKey{}, false)
}
