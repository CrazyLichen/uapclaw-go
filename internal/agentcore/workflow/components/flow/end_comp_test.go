package flow

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

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

// TestTemplateProcessor_RenderStream_静态段直出变量段取值 验证主循环基础路径
// （end_comp.py:368-441）。
func TestTemplateProcessor_RenderStream_静态段直出变量段取值(t *testing.T) {
	p := NewTemplateProcessor("Hello {{name}}!")
	var frames []templateFrame
	for chunk := range p.RenderStream(context.Background(), map[string]any{"name": "alice"}, nil, 0.2) {
		if chunk.Err != nil {
			t.Fatalf("流中错误: %v", chunk.Err)
		}
		frames = append(frames, chunk.Val)
	}
	if len(frames) != 3 {
		t.Fatalf("期望 3 帧, got %d: %v", len(frames), frames)
	}
	if frames[0].Data != "Hello " || frames[1].Data != "alice" || frames[1].Index != 1 || frames[2].Data != "!" {
		t.Fatalf("帧内容不符: %+v", frames)
	}
}

// TestTemplateProcessor_RenderStream_变量缺失跳过 验证单源 None 值跳过
// （end_comp.py:419-428 debug 日志路径）。
func TestTemplateProcessor_RenderStream_变量缺失跳过(t *testing.T) {
	p := NewTemplateProcessor("A {{x}} B")
	var datas []any
	for chunk := range p.RenderStream(context.Background(), map[string]any{}, nil, 0.2) {
		if chunk.Err != nil {
			t.Fatalf("流中错误: %v", chunk.Err)
		}
		datas = append(datas, chunk.Val.Data)
	}
	if len(datas) != 2 || datas[0] != "A " || datas[1] != " B" {
		t.Fatalf("期望静态段直出+变量段跳过: %v", datas)
	}
}

// TestTemplateProcessor_RenderStream_变量流叶子 验证 channel 叶子逐帧转发
// （end_comp.py:432-435，偏差 D-17-3）。
func TestTemplateProcessor_RenderStream_变量流叶子(t *testing.T) {
	p := NewTemplateProcessor("{{name}}")
	ch := make(chan any, 2)
	ch <- "f1"
	ch <- "f2"
	close(ch)
	var datas []any
	for chunk := range p.RenderStream(context.Background(), map[string]any{"name": (<-chan any)(ch)}, nil, 0.2) {
		if chunk.Err != nil {
			t.Fatalf("流中错误: %v", chunk.Err)
		}
		datas = append(datas, chunk.Val.Data)
	}
	if len(datas) != 2 || datas[0] != "f1" || datas[1] != "f2" {
		t.Fatalf("流叶子转发不符: %v", datas)
	}
}

// TestTemplateProcessor_RenderStream_超时等待后跳过 验证变量缺失且 has_any_value
// 为真时的等待-超时-跳过路径（end_comp.py:429-430 + 380-388）。
func TestTemplateProcessor_RenderStream_超时等待后跳过(t *testing.T) {
	p := NewTemplateProcessor("A {{x}} B{{y}}")
	var datas []any
	start := time.Now()
	for chunk := range p.RenderStream(context.Background(), map[string]any{"y": "Y"}, nil, 0.05) {
		if chunk.Err != nil {
			t.Fatalf("流中错误: %v", chunk.Err)
		}
		datas = append(datas, chunk.Val.Data)
	}
	if len(datas) != 3 || datas[0] != "A " || datas[1] != " B" || datas[2] != "Y" {
		t.Fatalf("期望 A 段超时跳过 + B 段 + Y: %v", datas)
	}
	if time.Since(start) < 50*time.Millisecond {
		t.Fatal("应至少经历一次超时等待")
	}
}

// TestTemplateProcessor_RenderStream_计数归零 验证 finally 段 count 达标后 reset
// （end_comp.py:364-366）。
func TestTemplateProcessor_RenderStream_计数归零(t *testing.T) {
	p := NewTemplateProcessor("{{x}}")
	p.SetDataSourceCount(2)
	// 第一拍：count 0→1，不 reset
	for range p.RenderStream(context.Background(), map[string]any{}, nil, 0.05) {
	}
	p.mu.Lock()
	c1 := p.count
	p.mu.Unlock()
	if c1 != 1 {
		t.Fatalf("第一拍后 count 应为 1, got %d", c1)
	}
	// 第二拍：count 达 2 → reset
	for range p.RenderStream(context.Background(), map[string]any{}, nil, 0.05) {
	}
	p.mu.Lock()
	c2 := p.count
	pos2 := p.currentPosition
	p.mu.Unlock()
	if c2 != 0 || pos2 != 0 {
		t.Fatalf("第二拍后应 reset: count=%d pos=%d", c2, pos2)
	}
	_ = pos2
}

// TestConsumeAllIterators_递归耗尽 验证嵌套 dict/list 中 channel 全部耗尽
// （end_comp.py:310-354，偏差 D-17-3）。
func TestConsumeAllIterators_递归耗尽(t *testing.T) {
	ch1 := make(chan any, 1)
	ch1 <- 1
	close(ch1)
	empty := make(chan any)
	close(empty)
	inputs := map[string]any{
		"a": []any{ch1, map[string]any{"b": empty}},
		"c": (<-chan any)(empty),
	}
	consumeAllIterators(inputs)
	// 耗尽后再次读立即关闭——range 无阻塞即通过
	for range ch1 {
	}
}

// codeOf 提取错误码（测试辅助：对齐 StatusCode 100010 断言）。
// mustConfig 构造合法 EndConfig（测试辅助）。
func mustConfig(tpl string) *EndConfig {
	c, err := NewEndConfig(tpl)
	if err != nil {
		panic(err)
	}
	return c
}

// codeOf 提取错误码（测试辅助：对齐 StatusCode 100010 断言）。
func codeOf(t *testing.T, err error) int {
	t.Helper()
	var be *exception.BaseError
	if !errors.As(err, &be) {
		t.Fatalf("非 BaseError: %v", err)
	}
	return be.Status().Code()
}

// TestNewEnd_带配置 验证模板处理器建立（end_comp.py:42-44）。
func TestNewEnd_带配置(t *testing.T) {
	e, err := NewEnd(mustConfig("Hi {{n}}"))
	if err != nil || e.template == nil || e.conf == nil {
		t.Fatalf("构造失败: err=%v template=%v", err, e.template)
	}
}

// TestNewEnd_配置无效 验证 100010 错误（end_comp.py:45-46）。
func TestNewEnd_配置无效(t *testing.T) {
	// 直接构造空模板配置（不走 mustConfig，其 panic 会先于 NewEnd 校验触发）
	_, err := NewEnd(&EndConfig{})
	if err == nil || codeOf(t, err) != 100010 {
		t.Fatalf("期望 COMPONENT_END_PARAM_INVALID(100010), got %v", err)
	}
}

// TestNewEnd_无配置 验证 conf/template 双 nil（end_comp.py:47-50）。
func TestNewEnd_无配置(t *testing.T) {
	e, err := NewEnd(nil)
	if err != nil || e.conf != nil || e.template != nil {
		t.Fatalf("无配置构造不符: err=%v", err)
	}
}

// TestEnd_SetMix 验证 mix 置位与模板数据源计数（end_comp.py:53-57）。
func TestEnd_SetMix(t *testing.T) {
	e, _ := NewEnd(mustConfig("Hi {{n}}"))
	e.SetMix()
	if !e.mix || e.template.dataSourceCount != 2 {
		t.Fatalf("SetMix 不符: mix=%v count=%d", e.mix, e.template.dataSourceCount)
	}
}

// TestEnd_SetMix_无模板 验证无模板时仅置位不触碰 template（end_comp.py:53-57 if 分支）。
func TestEnd_SetMix_无模板(t *testing.T) {
	e, _ := NewEnd(nil)
	e.SetMix()
	if !e.mix || e.template != nil {
		t.Fatalf("SetMix 无模板不符: mix=%v", e.mix)
	}
}

// TestEnd_Invoke_无模板_过滤None信封 验证 {"output": {...}}（end_comp.py:74-79）。
func TestEnd_Invoke_无模板_过滤None信封(t *testing.T) {
	e, _ := NewEnd(nil)
	inner := newFakeVertexSession(t, "n1", "End")
	out, err := e.OnInvoke(context.Background(), map[string]any{"a": 1, "b": nil}, inner, nil)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	wrapper, ok := out["output"].(map[string]any)
	if !ok || wrapper["a"] != 1 || len(wrapper) != 1 {
		t.Fatalf("期望 output 信封过滤 None: %v", out)
	}
}

// TestEnd_Invoke_无模板_全None返回nil 验证 output 为 nil 时结果 nil（end_comp.py:79）。
func TestEnd_Invoke_无模板_全None返回nil(t *testing.T) {
	e, _ := NewEnd(nil)
	inner := newFakeVertexSession(t, "n1", "End")
	out, err := e.OnInvoke(context.Background(), map[string]any{"b": nil}, inner, nil)
	if err != nil || out != nil {
		t.Fatalf("期望 nil 输出: out=%v err=%v", out, err)
	}
}

// TestEnd_Invoke_有模板_渲染 验证 {"response": 渲染串}（end_comp.py:70-73）。
func TestEnd_Invoke_有模板_渲染(t *testing.T) {
	e, _ := NewEnd(mustConfig("Hi {{n}}"))
	inner := newFakeVertexSession(t, "n1", "End")
	out, err := e.OnInvoke(context.Background(), map[string]any{"n": "bob"}, inner, nil)
	if err != nil || out["response"] != "Hi bob" {
		t.Fatalf("模板渲染不符: out=%v err=%v", out, err)
	}
}
