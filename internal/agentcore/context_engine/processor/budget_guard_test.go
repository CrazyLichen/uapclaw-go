package processor

import (
	"strings"
	"testing"

	iface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/token"
)

// ──────────────────────────── 结构体 ────────────────────────────

// budgetWindowContext 带窗口能力的 ModelContext 桩（只覆写本用例用到的方法）。
type budgetWindowContext struct {
	iface.ModelContext
	// window 上下文窗口
	window int
}

// budgetNoWindowContext 不带窗口能力的 ModelContext 桩。
type budgetNoWindowContext struct {
	iface.ModelContext
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestEffectiveContextBudget_取最小正值 验证窗口与调用预算取较小值。
func TestEffectiveContextBudget_取最小正值(t *testing.T) {
	mc := &budgetWindowContext{window: 200000}
	if got := EffectiveContextBudget(mc, 50000); got != 50000 {
		t.Fatalf("min(窗口, 调用预算) = %d, want 50000", got)
	}
	if got := EffectiveContextBudget(mc, 0); got != 200000 {
		t.Fatalf("无调用预算应取窗口，got %d", got)
	}
}

// TestEffectiveContextBudget_无窗口返回零 验证两者都未知时返回 0（调用方据此跳过守卫）。
func TestEffectiveContextBudget_无窗口返回零(t *testing.T) {
	mc := &budgetNoWindowContext{}
	if got := EffectiveContextBudget(mc, 0); got != 0 {
		t.Fatalf("两者都未知应返回 0，got %d", got)
	}
	if got := EffectiveContextBudget(mc, 1234); got != 1234 {
		t.Fatalf("有调用预算时返回调用预算，got %d", got)
	}
}

// TestTruncateMessageContent_保留后缀 验证截断保留首尾与 [[OFFLOAD:...]] 后缀。
func TestTruncateMessageContent_保留后缀(t *testing.T) {
	long := strings.Repeat("a", 20000) + "\n[[OFFLOAD:abc]]"
	got := TruncateMessageContentToFixedHeadTailIfOverTokenLimit(
		long, 100, func(s string) int { return len(s) / 3 }, "\n[[OFFLOAD:abc]]")
	if !strings.HasSuffix(got, "\n[[OFFLOAD:abc]]") {
		t.Fatalf("后缀丢失: %q", got[len(got)-40:])
	}
	if !strings.Contains(got, TruncatedByBudgetMarker) {
		t.Fatalf("缺少截断标记")
	}
	if len(got) >= len(long) {
		t.Fatalf("截断后长度未下降: %d >= %d", len(got), len(long))
	}
}

// TestTruncateMessageContent_未超限原样返回 验证未触发时不改动内容。
func TestTruncateMessageContent_未超限原样返回(t *testing.T) {
	content := "短内容"
	if got := TruncateMessageContentToFixedHeadTailIfOverTokenLimit(
		content, 100, func(string) int { return 1 }, ""); got != content {
		t.Fatalf("未超限应原样返回，got %q", got)
	}
}

// TestTruncateMessageContent_零阈值返回标记 验证 trigger<=0 直接给标记（对齐上游语义）。
func TestTruncateMessageContent_零阈值返回标记(t *testing.T) {
	if got := TruncateMessageContentToFixedHeadTailIfOverTokenLimit(
		"x", 0, func(string) int { return 1 }, ""); got != TruncatedByBudgetMarker {
		t.Fatalf("got %q", got)
	}
	if got := TruncateMessageContentToFixedHeadTailIfOverTokenLimit(
		"x", 0, func(string) int { return 1 }, "\n[[OFFLOAD:h]]"); got != "\n[[OFFLOAD:h]]" {
		t.Fatalf("应保留后缀，got %q", got)
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// ContextMaxTokens 覆写：返回桩窗口。
func (c *budgetWindowContext) ContextMaxTokens() int { return c.window }

// TokenCounter 覆写：返回 nil，触发 3 字符/token 降级。
func (c *budgetWindowContext) TokenCounter() token.TokenCounter { return nil }

// TokenCounter 覆写：返回 nil。
func (c *budgetNoWindowContext) TokenCounter() token.TokenCounter { return nil }
