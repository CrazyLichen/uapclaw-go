package callback

import (
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AbortError 回调执行中止错误，在回调内部触发以中止整个 trigger 流程。
//
// 对应 Python: openjiuwen/core/runner/callback/errors.py (AbortError)
//
// 传播逻辑（与 Python 对齐）：
//   - Cause != nil → trigger 返回 Cause（对调用方透明，AbortError 仅作为包装器）
//   - Cause == nil → trigger 返回 AbortError 本身
//
// 在 triggerCallbacks 中的处理：
//  1. 记录指标（is_error=True）
//  2. 熔断器记录失败
//  3. 执行 ERROR 钩子（传入 Cause ?? AbortError）
//  4. 日志记录
//  5. 如果 Cause != nil → 返回 (nil, Cause)
//  6. 如果 Cause == nil → 返回 (nil, AbortError)
type AbortError struct {
	// base 内嵌 BaseError，复用异常体系
	base *exception.BaseError
	// Reason 中止原因
	Reason string
	// Cause 原始错误（非 nil 时，trigger 重新抛出 Cause 而非 AbortError）
	Cause error
	// Details 额外详情
	Details any
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewAbortError 创建中止错误。
//
// 对应 Python: AbortError(reason, cause=cause, details=details)
func NewAbortError(reason string, cause error) *AbortError {
	var baseOpts []exception.ErrorOption
	baseOpts = append(baseOpts, exception.WithMsg(reason))
	if cause != nil {
		baseOpts = append(baseOpts, exception.WithCause(cause))
	}
	return &AbortError{
		base:   exception.BuildError(exception.StatusCallbackExecutionAborted, baseOpts...),
		Reason: reason,
		Cause:  cause,
	}
}

// NewAbortErrorWithDetails 创建带详情的中止错误。
func NewAbortErrorWithDetails(reason string, cause error, details any) *AbortError {
	ae := NewAbortError(reason, cause)
	ae.Details = details
	return ae
}

// Error 实现 error 接口。
func (e *AbortError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("回调执行中止: %s（由 %v 引起）", e.Reason, e.Cause)
	}
	return fmt.Sprintf("回调执行中止: %s", e.Reason)
}

// Unwrap 支持 errors.Unwrap/is/As 链。
func (e *AbortError) Unwrap() error {
	if e.Cause != nil {
		return e.Cause
	}
	return e.base
}
