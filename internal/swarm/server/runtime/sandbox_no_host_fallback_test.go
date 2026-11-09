package runtime

import (
	"context"
	"testing"
)

// TestNoHostFallbackRequired_默认false 验证未标记时为 false
func TestNoHostFallbackRequired_默认false(t *testing.T) {
	ctx := context.Background()
	if NoHostFallbackRequired(ctx) {
		t.Error("default should be false")
	}
}

// TestRequireNoHostFallback_标记后为true 验证标记生效
func TestRequireNoHostFallback_标记后为true(t *testing.T) {
	ctx := RequireNoHostFallback(context.Background())
	if !NoHostFallbackRequired(ctx) {
		t.Error("should be true after RequireNoHostFallback")
	}
}

// TestClearNoHostFallback_清除后为false 验证清除生效
func TestClearNoHostFallback_清除后为false(t *testing.T) {
	ctx := RequireNoHostFallback(context.Background())
	ctx = ClearNoHostFallback(ctx)
	if NoHostFallbackRequired(ctx) {
		t.Error("should be false after ClearNoHostFallback")
	}
}

// TestNoHostFallback_不污染父ctx 验证 ctx 派生隔离
func TestNoHostFallback_不污染父ctx(t *testing.T) {
	parent := context.Background()
	child := RequireNoHostFallback(parent)

	if NoHostFallbackRequired(parent) {
		t.Error("parent context should not be affected")
	}
	if !NoHostFallbackRequired(child) {
		t.Error("child context should be marked")
	}
}

// TestNoHostFallback_重复标记幂等 验证多次 Require 仍为 true
func TestNoHostFallback_重复标记幂等(t *testing.T) {
	ctx := RequireNoHostFallback(context.Background())
	ctx = RequireNoHostFallback(ctx)
	if !NoHostFallbackRequired(ctx) {
		t.Error("repeated Require should stay true")
	}
}

// TestNoHostFallback_清除后可重新标记 验证 clear → require 循环
func TestNoHostFallback_清除后可重新标记(t *testing.T) {
	ctx := RequireNoHostFallback(context.Background())
	ctx = ClearNoHostFallback(ctx)
	ctx = RequireNoHostFallback(ctx)
	if !NoHostFallbackRequired(ctx) {
		t.Error("should be re-markable after clear")
	}
}
