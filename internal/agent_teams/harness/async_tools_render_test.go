package harness

import (
	"strings"
	"testing"
)

// TestRenderResultText_变体 验证 nil/str/map/struct 的渲染。
func TestRenderResultText_变体(t *testing.T) {
	if got := RenderResultText(nil); got != "" {
		t.Fatalf("nil 应为空串，实际 %q", got)
	}
	if got := RenderResultText("hello"); got != "hello" {
		t.Fatalf("string 应原样，实际 %q", got)
	}
	got := RenderResultText(map[string]any{"k": 1})
	if !strings.Contains(got, `"k": 1`) {
		t.Fatalf("map 应为 JSON，实际 %q", got)
	}
	type payload struct{ Name string }
	if got := RenderResultText(payload{Name: "x"}); !strings.Contains(got, "x") {
		t.Fatalf("struct 应可渲染，实际 %q", got)
	}
}
