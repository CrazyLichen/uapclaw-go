package rail

import (
	"context"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// RailExecutor @rail 装饰器的 Go 等价：将函数包裹在 before/after/on_exception 钩子中。
//
// 对齐 Python: @rail(before, after, on_exception) 装饰器
// (openjiuwen/core/single_agent/rail/base.py L579-667)
//
// Go 没有装饰器语法，用结构体 + 闭包模式替代：
//
//	var modelCallRail = NewRailExecutor(
//	    rail.CallbackBeforeModelCall,
//	    rail.CallbackAfterModelCall,
//	    rail.CallbackOnModelException,
//	)
//	err := modelCallRail.Execute(ctx, cbc, func() error {
//	    result, e = a.callModel(ctx, cbc)
//	    return e
//	})
//
// Execute 内含完整流程：
//  1. before 钩子触发
//  2. force-finish 门控（before 钩子可请求跳过方法体）
//  3. 执行原始函数 fn
//  4. on_exception 钩子触发（异常时）
//  5. 重试循环（on_exception 钩子可请求重试）
//  6. after 钩子触发（finally 语义，始终执行）
type RailExecutor struct {
	// Before 方法执行前触发的事件（空字符串表示不触发）
	Before AgentCallbackEvent
	// After 方法执行后触发的事件（finally 语义，始终执行；空字符串表示不触发）
	After AgentCallbackEvent
	// OnException 方法异常时触发的事件（空字符串表示不触发）
	OnException AgentCallbackEvent
}

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// ModelCallRail 模型调用的 Rail 执行器。
	//
	// 对齐 Python: @rail(before=BEFORE_MODEL_CALL, after=AFTER_MODEL_CALL, on_exception=ON_MODEL_EXCEPTION)
	// 用法：
	//   err := rail.ModelCallRail.Execute(ctx, cbc, func() error { ... })
	ModelCallRail = NewRailExecutor(
		CallbackBeforeModelCall,
		CallbackAfterModelCall,
		CallbackOnModelException,
	)
	// ToolCallRail 工具调用的 Rail 执行器。
	//
	// 对齐 Python: @rail(before=BEFORE_TOOL_CALL, after=AFTER_TOOL_CALL, on_exception=ON_TOOL_EXCEPTION)
	// 用法：
	//   err := rail.ToolCallRail.Execute(ctx, cbc, func() error { ... })
	ToolCallRail = NewRailExecutor(
		CallbackBeforeToolCall,
		CallbackAfterToolCall,
		CallbackOnToolException,
	)
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewRailExecutor 创建 RailExecutor 实例。
func NewRailExecutor(before, after, onException AgentCallbackEvent) *RailExecutor {
	return &RailExecutor{
		Before:      before,
		After:       after,
		OnException: onException,
	}
}

// ──────────────────────────── 导出方法 ────────────────────────────

// Execute 在 before/after/on_exception 钩子包裹下执行 fn。
//
// 对齐 Python: @rail 装饰器的 wrapper 函数 (base.py L603-663)
//
// 流程：
//  1. 清除残留 retry 请求
//  2. 设置 retryAttempt / 清空 exception
//  3. 触发 before 钩子
//  4. force-finish 门控：如果 before 钩子请求了 force-finish，跳过 fn 直接返回 nil
//  5. 执行 fn()
//  6. 如果 fn() 出错：
//     a. 设置 exception
//     b. 触发 on_exception 钩子
//     c. 检查重试请求：如果有则继续循环，否则触发 after 并返回错误
//  7. 触发 after 钩子（finally 语义）
//
// after 事件行为：
//   - 正常返回：触发 after，返回 nil
//   - 异常返回：触发 after，返回原始错误（after 回调出错时 log 不掩盖原始错误）
//   - context 取消：跳过 after 事件（对齐 Python CancelledError 保护）
func (re *RailExecutor) Execute(
	ctx context.Context,
	cbc *AgentCallbackContext,
	fn func() error,
) error {
	attempt := 0
	for {
		// 1. 清除残留 retry 请求
		_ = cbc.ConsumeRetryRequest()
		// 2. 设置重试计数 / 清空异常
		cbc.SetRetryAttempt(attempt)
		cbc.SetException(nil)

		var excToRaise error

		// 3. 触发 before 钩子
		if re.Before != "" {
			if err := cbc.Fire(re.Before); err != nil {
				return err
			}
		}

		// 4. force-finish 门控
		if cbc.HasForceFinishRequest() {
			// before 钩子请求了 force-finish，跳过方法体
			// 触发 after 事件（对齐 Python: return None 之前仍进入 finally）
			return re.fireAfter(ctx, cbc, nil)
		}

		// 5. 执行原始函数
		err := fn()
		if err != nil {
			// 6a. 异常路径
			excToRaise = err
			cbc.SetException(err)

			// 6b. 触发 on_exception 钩子
			if re.OnException != "" {
				// on_exception 回调出错时 log 不掩盖原始异常（对齐 Python L624-630）
				if cbErr := cbc.Fire(re.OnException); cbErr != nil {
					logger.Error(logComponent).
						Str("event", string(re.OnException)).
						Err(cbErr).
						Msg("on_exception 回调出错")
				}
			}

			// 6c. 检查重试请求
			if retryReq := cbc.ConsumeRetryRequest(); retryReq != nil {
				// 延迟重试（对齐 Python: await asyncio.sleep(retry_request.delay_seconds)）
				if retryReq.DelaySeconds > 0 {
					select {
					case <-time.After(time.Duration(retryReq.DelaySeconds * float64(time.Second))):
						// 延迟结束，继续重试
					case <-ctx.Done():
						// context 在等待期间被取消
						return re.fireAfter(ctx, cbc, err)
					}
				}
				attempt++
				continue
			}

			// 无重试请求 → 触发 after 并返回错误
			return re.fireAfter(ctx, cbc, excToRaise)
		}

		// 7. 正常路径 → 触发 after 并返回
		return re.fireAfter(ctx, cbc, nil)
	}
}

// RailEvents 返回此执行器关联的三个事件。
//
// 对齐 Python: wrapper.rail_events = (before, after, on_exception)
// 供反射/调试/测试使用。
func (re *RailExecutor) RailEvents() (before, after, onException AgentCallbackEvent) {
	return re.Before, re.After, re.OnException
}

// ──────────────────────────── 非导出方法 ────────────────────────────

// fireAfter 触发 after 事件。
//
// 对齐 Python: finally 块中 after 事件触发逻辑 (base.py L642-663)
//
// 规则：
//   - context 已取消时跳过 after 事件（对齐 Python CancelledError 保护）
//   - after 回调出错且有原始异常时 → log 不掩盖原始异常
//   - after 回调出错且无原始异常时 → 返回 after 回调的错误
//   - 无 after 事件时 → 直接返回原始异常（可能为 nil）
func (re *RailExecutor) fireAfter(ctx context.Context, cbc *AgentCallbackContext, origErr error) error {
	// 无 after 事件
	if re.After == "" {
		return origErr
	}

	// context 已取消 → 跳过 after 事件
	// 对齐 Python: is_cancelled = isinstance(..., asyncio.CancelledError)
	if isCancelled(ctx) {
		return origErr
	}

	// 触发 after 钩子
	afterErr := cbc.Fire(re.After)
	if afterErr != nil {
		if origErr != nil {
			// after 回调出错但有原始异常 → log 不掩盖
			logger.Error(logComponent).
				Str("event", string(re.After)).
				Err(afterErr).
				Msg("after 回调出错，掩盖原始异常")
			return origErr
		}
		// after 回调出错且无原始异常 → 返回 after 错误
		return afterErr
	}

	return origErr
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// isCancelled 检查 context 是否已被取消。
//
// 对齐 Python: isinstance(sys.exc_info()[1], asyncio.CancelledError)
// Go 中 ctx.Err() != nil 等价于协程被取消（context.Canceled）或超时（context.DeadlineExceeded）。
func isCancelled(ctx context.Context) bool {
	return ctx.Err() != nil
}
