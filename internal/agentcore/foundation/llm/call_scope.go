package llm

import (
	"context"
	"crypto/rand"
	"encoding/hex"
)

// 本文件提供每次 LLM 调用的请求级身份作用域。
// Python: openjiuwen/core/foundation/llm/call_scope.py
//
// 每次 Model.Invoke / Model.Stream 打开一个作用域，为整个请求周期绑定一个
// 全新 call id。挂在回调框架（LLMCallEvents.*）上的观察者读取该 id 来区分
// 在途请求：同一请求的 input 事件、每个流式分片、output 事件与 error 事件
// 报告同一个 id，且并发请求之间永不共享。
//
// Go 侧 context 不可变：contextvar 的 set/reset 语义映射为
// "派生子 ctx + 显式还原闭包"。Python __exit__ 的还原表现为调用方在作用域
// 结束后继续使用外层 ctx（见 spec 偏差 P1）。

// ──────────────────────────── 枚举 ────────────────────────────

// ctxKey context 键类型（非导出，防外部伪造）。
type ctxKey int

const (
	// ctxKeyCallID 当前 LLM 请求的调用 ID（作用域外为空串）。
	ctxKeyCallID ctxKey = iota
	// ctxKeyUnifiedCompletion 是否期望 Model 生命周期发出统一收尾事件。
	ctxKeyUnifiedCompletion
	// ctxKeyObservationSuppressed 是否抑制本调用的观测。
	ctxKeyObservationSuppressed
)

// ──────────────────────────── 导出函数 ────────────────────────────

// GetCurrentLLMCallID 返回当前上下文在途 LLM 请求的 ID；无作用域返回空串
// （绕过 Model 的调用方、或任何 LLM 调用之外触发的回调）。
// Python: get_current_llm_call_id()
func GetCurrentLLMCallID(ctx context.Context) string {
	if v, ok := ctx.Value(ctxKeyCallID).(string); ok {
		return v
	}
	return ""
}

// ExpectsUnifiedLLMCompletion 返回当前调用是否由 Model 生命周期负责成功收尾。
// Python: expects_unified_llm_completion()——经 Model 路由的调用以
// LLM_INVOKE_OUTPUT / LLM_STREAM_COMPLETED 终止；旧的兼容回调流以
// LLM_OUTPUT 终止，观察者据此保留旧终端事件。
func ExpectsUnifiedLLMCompletion(ctx context.Context) bool {
	if v, ok := ctx.Value(ctxKeyUnifiedCompletion).(bool); ok {
		return v
	}
	return false
}

// IsLLMObservationSuppressed 返回本次内部调用是否被排除出 agent 轨迹。
// Python: is_llm_observation_suppressed()
func IsLLMObservationSuppressed(ctx context.Context) bool {
	if v, ok := ctx.Value(ctxKeyObservationSuppressed).(bool); ok {
		return v
	}
	return false
}

// SuppressLLMObservation 将内部模型探测从用户可见轨迹中排除。
// Python: LlmObservationSuppression.__enter__/__exit__
// 返回携带抑制标志的新 ctx 与还原闭包（还原闭包返回外层 ctx）。
func SuppressLLMObservation(ctx context.Context) (context.Context, func() context.Context) {
	next := context.WithValue(ctx, ctxKeyObservationSuppressed, true)
	return next, func() context.Context { return ctx }
}

// EnterCallScope 为一次 LLM 请求绑定全新 call id（Python: LlmCallScope()）。
// 省略显式 ID 时生成 uuid4 hex（常规路径）。覆盖整个请求周期：
// input 事件 → 输出事件。还原闭包为作用域结束的显式边界标记（Python
// __exit__ 的等价物：调用方此后继续使用外层 ctx）。
func EnterCallScope(ctx context.Context, unifiedCompletion bool) (context.Context, func()) {
	var b [16]byte
	// crypto/rand 失败仅影响唯一性，ID 仍非空可用（与 uuid4 概率前提一致）
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40 // uuid4 版本位
	b[8] = (b[8] & 0x3f) | 0x80 // uuid4 变体位
	return EnterCallScopeWithID(ctx, hex.EncodeToString(b[:]), unifiedCompletion)
}

// EnterCallScopeWithID 以显式 ID 进入作用域（Python: LlmCallScope(call_id=...)）。
// 记住外层调用 ID 与 unified 标志并在还原闭包中引用（Python __enter__ 的
// _previous 语义；Go ctx 不可变，还原即回到外层 ctx 变量）。
func EnterCallScopeWithID(ctx context.Context, callID string, unifiedCompletion bool) (context.Context, func()) {
	prevID, _ := ctx.Value(ctxKeyCallID).(string)
	prevUnified, _ := ctx.Value(ctxKeyUnifiedCompletion).(bool)
	next := context.WithValue(
		context.WithValue(ctx, ctxKeyCallID, callID),
		ctxKeyUnifiedCompletion, unifiedCompletion,
	)
	return next, func() {
		// Python __exit__ 还原 _previous/_previous_unified_completion；
		// Go ctx 不可变，作用域收束表现为调用方改用外层 ctx，此处保留
		// 旧值读取以明确还原目标的等价物（spec 偏差 P1）。
		_, _ = prevID, prevUnified
	}
}
