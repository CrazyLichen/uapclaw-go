package flow

import "testing"

// TestTemplateUtils_RenderTemplate_基本替换 验证 {{var}} 批量替换语义。
func TestTemplateUtils_RenderTemplate_基本替换(t *testing.T) {
	got := TemplateUtilsRenderTemplate("Hello {{name}}", map[string]any{"name": "alice"})
	if got != "Hello alice" {
		t.Fatalf("got %q", got)
	}
}

// TestTemplateUtils_RenderTemplate_缺键保留 验证 safe_substitute 缺键保留原样（偏差 D-17-4）。
func TestTemplateUtils_RenderTemplate_缺键保留(t *testing.T) {
	got := TemplateUtilsRenderTemplate("Hello {{name}}", map[string]any{})
	if got != "Hello {{name}}" {
		t.Fatalf("期望缺键保留原样, got %q", got)
	}
}

// TestTemplateUtils_RenderTemplate_嵌套路径值 验证变量值非字符串类型格式化。
func TestTemplateUtils_RenderTemplate_嵌套路径值(t *testing.T) {
	got := TemplateUtilsRenderTemplate("count: {{n}}", map[string]any{"n": 42})
	if got != "count: 42" {
		t.Fatalf("got %q", got)
	}
}

// TestTemplateUtils_RenderTemplateToList_分段 验证正则分段与空段过滤。
func TestTemplateUtils_RenderTemplateToList_分段(t *testing.T) {
	got := TemplateUtilsRenderTemplateToList("Hi {{a}} bye{{b}}")
	want := []string{"Hi ", "{{a}}", " bye", "{{b}}"}
	if len(got) != len(want) {
		t.Fatalf("段数不符: %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("段 %d 不符: %q != %q", i, got[i], want[i])
		}
	}
}

// TestTemplateUtils_RenderTemplateToList_空段过滤 验证首段为变量时无空文本段
// （Python filter(None, ...)）。
func TestTemplateUtils_RenderTemplateToList_空段过滤(t *testing.T) {
	got := TemplateUtilsRenderTemplateToList("{{a}} tail")
	if len(got) != 2 || got[0] != "{{a}}" || got[1] != " tail" {
		t.Fatalf("空段过滤不符: %v", got)
	}
}
