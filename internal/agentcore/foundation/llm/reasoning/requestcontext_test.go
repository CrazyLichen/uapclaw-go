package reasoning

import (
	"context"
	"testing"
)

// TestDisabledThinkingFallback_默认关闭 验证默认不启用回退（安全设计核心）。
// Python: request_context.py:11-14 ContextVar(default=False)
func TestDisabledThinkingFallback_默认关闭(t *testing.T) {
	if IsDisabledThinkingFallbackAllowed(context.Background()) {
		t.Fatal("默认必须为 false：拒绝关闭思考时应直接失败，不静默重试")
	}
}

// TestDisabledThinkingFallback_作用域内开启 验证 ctx 派生生效。
// Python: request_context.py:22-28 disabled_thinking_fallback_scope()
func TestDisabledThinkingFallback_作用域内开启(t *testing.T) {
	ctx := WithDisabledThinkingFallback(context.Background())
	if !IsDisabledThinkingFallbackAllowed(ctx) {
		t.Fatal("作用域内应为 true")
	}
}

// TestDisabledThinkingFallback_父ctx不受污染 验证作用域不外泄。
// Python: token reset 后恢复原值（request_context.py:28）
func TestDisabledThinkingFallback_父ctx不受污染(t *testing.T) {
	parent := context.Background()
	_ = WithDisabledThinkingFallback(parent)
	if IsDisabledThinkingFallbackAllowed(parent) {
		t.Fatal("父 ctx 被污染：作用域必须只影响派生 ctx")
	}
}

// TestDisabledThinkingFallback_nilCtx 验证 nil ctx 不 panic。
func TestDisabledThinkingFallback_nilCtx(t *testing.T) {
	if IsDisabledThinkingFallbackAllowed(nil) {
		t.Fatal("nil ctx 应返回 false")
	}
	// With 传 nil 时应回落到 Background 而不是 panic
	ctx := WithDisabledThinkingFallback(nil)
	if !IsDisabledThinkingFallbackAllowed(ctx) {
		t.Fatal("nil 入参应回落 Background 后生效")
	}
}

// TestDisabledThinkingFallback_嵌套仍为true 验证重复进入不取反。
func TestDisabledThinkingFallback_嵌套仍为true(t *testing.T) {
	ctx := WithDisabledThinkingFallback(WithDisabledThinkingFallback(context.Background()))
	if !IsDisabledThinkingFallbackAllowed(ctx) {
		t.Fatal("嵌套后应仍为 true")
	}
}
