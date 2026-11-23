package web

import (
	"context"
	"testing"
)

// ──────────────────────────── 导出函数测试 ────────────────────────────

// ──────────────────────────── 非导出函数测试 ────────────────────────────

// TestClearAgentConfigCache_NilClient 验证 agentClient 为 nil 时直接返回（不 panic）。
func TestClearAgentConfigCache_NilClient(t *testing.T) {
	clearAgentConfigCache(nil, context.Background())
}
