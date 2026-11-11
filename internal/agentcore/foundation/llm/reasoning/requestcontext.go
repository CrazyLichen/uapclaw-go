package reasoning

import "context"

// ──────────────────────────── 结构体 ────────────────────────────

// disabledThinkingFallbackKey ctx value 键。
// 用非导出空结构体，避免与其他包的键冲突。
type disabledThinkingFallbackKey struct{}

// ──────────────────────────── 导出函数 ────────────────────────────

// WithDisabledThinkingFallback 返回允许 disabled-thinking 兼容回退的 ctx。
//
// 默认 **不允许**：模型拒绝「关闭思考」时直接失败，不做静默重试。
// 只有显式进入本作用域的调用（上游生产唯一开启点是 symphony 指纹抽取路径
// extractor.py:324）才允许「剥掉 disabled 字段重试一次」。
//
// Python: request_context.py:22-28 disabled_thinking_fallback_scope()
func WithDisabledThinkingFallback(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, disabledThinkingFallbackKey{}, true)
}

// IsDisabledThinkingFallbackAllowed 判定当前请求是否允许 disabled-thinking 回退。
// 默认 false。nil ctx 安全返回 false。
// Python: request_context.py:17-19 is_disabled_thinking_fallback_allowed()
func IsDisabledThinkingFallbackAllowed(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	v, _ := ctx.Value(disabledThinkingFallbackKey{}).(bool)
	return v
}
