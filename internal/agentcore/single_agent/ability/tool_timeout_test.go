package ability

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
)

// ──────────────────────────── 非导出函数 ────────────────────────────

// TestResolveCallTimeout_无resilience块 验证缺省返回 300
func TestResolveCallTimeout_无resilience块(t *testing.T) {
	card := tool.NewToolCard("t", "", nil, nil)
	got := resolveCallTimeout(card)
	if got == nil || *got != defaultToolCallTimeout {
		t.Fatalf("应为 %v，实际 %v", defaultToolCallTimeout, got)
	}
}

// TestResolveCallTimeout_显式正数 验证正数原样返回
func TestResolveCallTimeout_显式正数(t *testing.T) {
	card := tool.NewToolCard("t", "", nil, map[string]any{
		"resilience": map[string]any{"timeout_s": float64(42)},
	})
	got := resolveCallTimeout(card)
	if got == nil || *got != 42 {
		t.Fatalf("应为 42，实际 %v", got)
	}
}

// TestResolveCallTimeout_显式nil 验证 nil 表示豁免
func TestResolveCallTimeout_显式nil(t *testing.T) {
	card := tool.NewToolCard("t", "", nil, map[string]any{
		"resilience": map[string]any{"timeout_s": nil},
	})
	if got := resolveCallTimeout(card); got != nil {
		t.Fatalf("应为 nil（豁免），实际 %v", *got)
	}
}

// TestResolveCallTimeout_非正数 验证 0 与负数表示豁免
func TestResolveCallTimeout_非正数(t *testing.T) {
	for _, v := range []float64{0, -1} {
		card := tool.NewToolCard("t", "", nil, map[string]any{
			"resilience": map[string]any{"timeout_s": v},
		})
		if got := resolveCallTimeout(card); got != nil {
			t.Fatalf("timeout_s=%v 应为 nil（豁免），实际 %v", v, *got)
		}
	}
}

// TestResolveCallTimeout_键不存在 验证只有 resilience 块无 timeout_s 时取默认
func TestResolveCallTimeout_键不存在(t *testing.T) {
	card := tool.NewToolCard("t", "", nil, map[string]any{
		"resilience": map[string]any{"max_attempts": 3},
	})
	got := resolveCallTimeout(card)
	if got == nil || *got != defaultToolCallTimeout {
		t.Fatalf("应为 %v，实际 %v", defaultToolCallTimeout, got)
	}
}

// TestResolveCallTimeout_类型非法 验证 timeout_s 非数值时回退默认
func TestResolveCallTimeout_类型非法(t *testing.T) {
	card := tool.NewToolCard("t", "", nil, map[string]any{
		"resilience": map[string]any{"timeout_s": "abc"},
	})
	got := resolveCallTimeout(card)
	if got == nil || *got != defaultToolCallTimeout {
		t.Fatalf("应为 %v，实际 %v", defaultToolCallTimeout, got)
	}
}

// TestResolveCallTimeout_resilience非dict 验证 resilience 不是 map 时回退默认
func TestResolveCallTimeout_resilience非dict(t *testing.T) {
	card := tool.NewToolCard("t", "", nil, map[string]any{"resilience": "bad"})
	got := resolveCallTimeout(card)
	if got == nil || *got != defaultToolCallTimeout {
		t.Fatalf("应为 %v，实际 %v", defaultToolCallTimeout, got)
	}
}

// TestResolveCallTimeout_nil卡片 验证 card 为 nil 时回退默认
func TestResolveCallTimeout_nil卡片(t *testing.T) {
	got := resolveCallTimeout(nil)
	if got == nil || *got != defaultToolCallTimeout {
		t.Fatalf("应为 %v，实际 %v", defaultToolCallTimeout, got)
	}
}

// TestEffectiveCallTimeout_豁免走硬上限 验证 nil 预算被提升为 3600
func TestEffectiveCallTimeout_豁免走硬上限(t *testing.T) {
	if got := effectiveCallTimeout(nil); got != maxToolCallTimeoutHardLimit {
		t.Fatalf("应为 %v，实际 %v", maxToolCallTimeoutHardLimit, got)
	}
}

// TestEffectiveCallTimeout_正常值透传 验证非 nil 原样返回
func TestEffectiveCallTimeout_正常值透传(t *testing.T) {
	v := 42.0
	if got := effectiveCallTimeout(&v); got != 42 {
		t.Fatalf("应为 42，实际 %v", got)
	}
}

// TestResolveCallTimeout_环境变量默认 验证 DEFAULT_TOOL_CALL_TIMEOUT 生效
func TestResolveCallTimeout_环境变量默认(t *testing.T) {
	t.Setenv("DEFAULT_TOOL_CALL_TIMEOUT", "120")
	card := tool.NewToolCard("t", "", nil, nil)
	got := resolveCallTimeout(card)
	if got == nil || *got != 120 {
		t.Fatalf("环境变量应生效为 120，实际 %v", got)
	}
}

// TestResolveInvocationTimeout_委托工具钩子 验证走 Tool.GetCallTimeout
func TestResolveInvocationTimeout_委托工具钩子(t *testing.T) {
	tl := &stubTimeoutTool{value: 999}
	if got := resolveInvocationTimeout(tl, map[string]any{}, 300); got != 999 {
		t.Fatalf("应委托到工具钩子返回 999，实际 %v", got)
	}
}

// TestResolveInvocationTimeout_nil工具 验证 tl 为 nil 时返回默认
func TestResolveInvocationTimeout_nil工具(t *testing.T) {
	if got := resolveInvocationTimeout(nil, map[string]any{}, 456); got != 456 {
		t.Fatalf("应为 456，实际 %v", got)
	}
}

// ──────────────────────────── 测试替身 ────────────────────────────

// stubTimeoutTool 只覆盖 GetCallTimeout 的测试替身。
type stubTimeoutTool struct {
	value float64
}

func (s *stubTimeoutTool) Card() *tool.ToolCard { return tool.NewToolCard("stub", "", nil, nil) }

func (s *stubTimeoutTool) Invoke(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (map[string]any, error) {
	return nil, nil
}

func (s *stubTimeoutTool) Stream(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, nil
}

func (s *stubTimeoutTool) GetCallTimeout(_ map[string]any, _ float64) float64 { return s.value }
