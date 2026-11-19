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

// TestTemplateProcessor_构造分段 验证变量段剥壳与位置集合（end_comp.py:251-267）。
func TestTemplateProcessor_构造分段(t *testing.T) {
	p := NewTemplateProcessor("Hi {{a}} bye")
	// 分段: ["Hi ", "a", " bye"]（变量段剥壳）
	if len(p.segments) != 3 {
		t.Fatalf("期望 3 段, got %v", p.segments)
	}
	if !p.variablesPositions[1] {
		t.Fatalf("位置 1 应为变量段: %v", p.variablesPositions)
	}
	if p.segments[1] != "a" {
		t.Fatalf("变量段应剥壳: %q", p.segments[1])
	}
	if p.shouldRender() {
		t.Fatal("位置 0 为静态段不应渲染")
	}
	p.currentPosition = 1
	if !p.shouldRender() {
		t.Fatal("位置 1 应为变量段")
	}
}

// TestTemplateProcessor_NeedRender 验证变量有值才渲染（end_comp.py:279-287）。
func TestTemplateProcessor_NeedRender(t *testing.T) {
	p := NewTemplateProcessor("{{a}}")
	if p.needRender(map[string]any{}) {
		t.Fatal("空输入不应渲染")
	}
	if !p.needRender(map[string]any{"a": "x"}) {
		t.Fatal("有值应渲染")
	}
}

// TestTemplateProcessor_Reset 验证位置与计数归零（end_comp.py:304-308）。
func TestTemplateProcessor_Reset(t *testing.T) {
	p := NewTemplateProcessor("a {{b}} c")
	p.currentPosition = 2
	p.count = 1
	p.chunkIndex = 5
	p.reset()
	if p.currentPosition != 0 || p.count != 0 || p.chunkIndex != 0 {
		t.Fatalf("reset 不彻底: %+v", p)
	}
}

// TestTemplateProcessor_SetDataSourceCount 验证数据源计数切换（end_comp.py:269-271）。
func TestTemplateProcessor_SetDataSourceCount(t *testing.T) {
	p := NewTemplateProcessor("a {{b}}")
	p.count = 9
	p.SetDataSourceCount(2)
	if p.dataSourceCount != 2 || p.count != 0 {
		t.Fatalf("setDataSourceCount 不符: %+v", p)
	}
}

// TestTemplateProcessor_Render_整体渲染 验证 Render 委派 TemplateUtils
// （end_comp.py:301-302）。
func TestTemplateProcessor_Render_整体渲染(t *testing.T) {
	p := NewTemplateProcessor("Hi {{name}}")
	if got := p.Render(map[string]any{"name": "alice"}); got != "Hi alice" {
		t.Fatalf("got %q", got)
	}
}

// TestTemplateProcessor_IsFinished 验证推进完毕判定（end_comp.py:443-444）。
func TestTemplateProcessor_IsFinished(t *testing.T) {
	p := NewTemplateProcessor("ab")
	if p.isFinished() {
		t.Fatal("初始位置 0 不应 finished")
	}
	p.currentPosition = 2
	if !p.isFinished() {
		t.Fatal("位置 2 应 finished")
	}
}

// TestTemplateProcessor_GetSegment_越界 验证越界返回空串（end_comp.py:289-292）。
func TestTemplateProcessor_GetSegment_越界(t *testing.T) {
	p := NewTemplateProcessor("ab")
	if p.getSegment(5) != "" {
		t.Fatal("越界应返回空串")
	}
	if p.getSegment(0) != "ab" {
		t.Fatalf("段 0 不符: %q", p.getSegment(0))
	}
}

// TestTemplateProcessor_AdvancePosition 验证位置推进（end_comp.py:297-299）。
func TestTemplateProcessor_AdvancePosition(t *testing.T) {
	p := NewTemplateProcessor("ab")
	if got := p.advancePosition(); got != 1 || p.currentPosition != 1 {
		t.Fatalf("推进不符: %d %d", got, p.currentPosition)
	}
}
