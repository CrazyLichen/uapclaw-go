package flow

import (
	"context"
	"fmt"
	"strings"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/graph/stream_actor"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/workflow/components/ability"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/constants"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
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

// TestEnd_Stream_有模板_信封帧 验证 OutputSchema{EndNodeStream} 包装（end_comp.py:106-114）。
func TestEnd_Stream_有模板_信封帧(t *testing.T) {
	e, _ := NewEnd(mustConfig("Hi {{n}}"))
	inner := newFakeVertexSession(t, "n1", "End")
	ch, err := e.OnStream(context.Background(), map[string]any{"n": "cat"}, inner, nil)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	var got []stream.OutputSchema
	for chunk := range ch {
		if chunk.Err != nil {
			t.Fatalf("流中错误: %v", chunk.Err)
		}
		got = append(got, chunk.Val["frame"].(stream.OutputSchema))
	}
	if len(got) != 2 || got[0].Type != constants.EndNodeStream {
		t.Fatalf("信封帧不符: %+v", got)
	}
	if got[0].Payload.(map[string]any)["response"] != "Hi " {
		t.Fatalf("首帧 payload 不符: %+v", got[0])
	}
	if got[1].Payload.(map[string]any)["response"] != "cat" {
		t.Fatalf("次帧 payload 不符: %+v", got[1])
	}
}

// TestEnd_Stream_无模板_逐key吐出 验证 dict 逐 key 输出（end_comp.py:116-119）。
func TestEnd_Stream_无模板_逐key吐出(t *testing.T) {
	e, _ := NewEnd(nil)
	inner := newFakeVertexSession(t, "n1", "End")
	ch, err := e.OnStream(context.Background(), map[string]any{"a": 1, "b": 2}, inner, nil)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	count := 0
	seen := map[string]bool{}
	for chunk := range ch {
		if chunk.Err != nil {
			t.Fatalf("流中错误: %v", chunk.Err)
		}
		count++
		if wrapper, ok := chunk.Val["output"].(map[string]any); ok {
			for k := range wrapper {
				seen[k] = true
			}
		}
	}
	if count != 2 || !seen["a"] || !seen["b"] {
		t.Fatalf("逐 key 输出不符: count=%d seen=%v", count, seen)
	}
}

// TestEnd_Transform_无模板_叶子帧 验证流入经叶子抽取逐帧吐出（end_comp.py:163-171）。
func TestEnd_Transform_无模板_叶子帧(t *testing.T) {
	e, _ := NewEnd(nil)
	inner := newFakeVertexSession(t, "n1", "End")
	in := make(chan map[string]any, 1)
	in <- map[string]any{"llm1": map[string]any{"text": "hi"}}
	close(in)
	ch, err := e.OnTransform(context.Background(), in, inner, nil)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	var first map[string]any
	for chunk := range ch {
		if chunk.Err != nil {
			t.Fatalf("流中错误: %v", chunk.Err)
		}
		if first == nil {
			first = chunk.Val
		}
	}
	if first == nil {
		t.Fatal("无输出帧")
	}
	wrapper, ok := first["output"].(map[string]any)
	if !ok || wrapper["llm1.text"] != "hi" {
		t.Fatalf("叶子抽取帧不符: %v", first)
	}
}

// TestEnd_Collect_无模板_聚合 验证 {"output": chunks}（end_comp.py:193-202）。
func TestEnd_Collect_无模板_聚合(t *testing.T) {
	e, _ := NewEnd(nil)
	inner := newFakeVertexSession(t, "n1", "End")
	in := make(chan map[string]any, 1)
	in <- map[string]any{"k": "v"}
	close(in)
	out, err := e.OnCollect(context.Background(), in, inner, nil)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if _, ok := out["output"]; !ok {
		t.Fatalf("期望 output 聚合信封: %v", out)
	}
}

// TestEnd_Collect_有模板_渲染 验证模板路径（end_comp.py:191-192）。
func TestEnd_Collect_有模板_渲染(t *testing.T) {
	e, _ := NewEnd(mustConfig("Hi {{n}}"))
	inner := newFakeVertexSession(t, "n1", "End")
	in := make(chan map[string]any, 1)
	in <- map[string]any{"n": "joe"}
	close(in)
	out, err := e.OnCollect(context.Background(), in, inner, nil)
	if err != nil || out["response"] != "Hi joe" {
		t.Fatalf("模板聚合不符: out=%v err=%v", out, err)
	}
}

// TestEnd_Transform_有模板_信封帧 验证 transform 模板路径（end_comp.py:154-162）。
func TestEnd_Transform_有模板_信封帧(t *testing.T) {
	e, _ := NewEnd(mustConfig("Hi {{n}}"))
	inner := newFakeVertexSession(t, "n1", "End")
	in := make(chan map[string]any, 1)
	in <- map[string]any{"n": "zoe"}
	close(in)
	ch, err := e.OnTransform(context.Background(), in, inner, nil)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	n := 0
	for chunk := range ch {
		if chunk.Err != nil {
			t.Fatalf("流中错误: %v", chunk.Err)
		}
		if _, ok := chunk.Val["frame"].(stream.OutputSchema); ok {
			n++
		}
	}
	if n != 2 {
		t.Fatalf("期望 2 个信封帧, got %d", n)
	}
}

// TestEnd_Render_Mix_首调用未渲染超时同步渲染兜底 验证 mix 首拍超时降级
// （end_comp.py:219-236 未渲染分支）。
func TestEnd_Render_Mix_首调用未渲染超时同步渲染兜底(t *testing.T) {
	e, _ := NewEnd(mustConfig("Hi {{n}}"))
	e.SetMix()
	inner := newFakeVertexSession(t, "n1", "End")
	// 极小超时触发超时路径（batchTemplate 尚未被渲染）
	compSessX, csErr := session.NewComponentSession(inner, false)
	if csErr != nil {
		t.Fatal(csErr)
	}
	out, err := e.render(context.Background(), map[string]any{"n": "x"}, 0.01, compSessX)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if out == nil || out["response"] != "Hi x" {
		t.Fatalf("mix 兜底应同步渲染返回 response: %v", out)
	}
	if e.batchTemplate != nil {
		t.Fatal("超时降级后 batchTemplate 应已清空")
	}
}

// TestEnd_Render_Mix_第二拍等待首拍渲染完成 验证 condition.wait 唤醒路径
// （end_comp.py:220-223 + 237-238）。
func TestEnd_Render_Mix_第二拍等待首拍渲染完成(t *testing.T) {
	e, _ := NewEnd(mustConfig("Hi {{n}}"))
	e.SetMix()
	inner := newFakeVertexSession(t, "n1", "End")
	compSess, csErr := session.NewComponentSession(inner, false)
	if csErr != nil {
		t.Fatal(csErr)
	}
	var first, second map[string]any
	var err1, err2 error
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		first, err1 = e.render(context.Background(), map[string]any{"n": "a"}, 5, compSess)
	}()
	time.Sleep(10 * time.Millisecond) // 保证首拍先建 batchTemplate
	go func() {
		defer wg.Done()
		second, err2 = e.render(context.Background(), map[string]any{"n": "b"}, 5, compSess)
	}()
	wg.Wait()
	if err1 != nil || err2 != nil {
		t.Fatalf("render 错误: %v %v", err1, err2)
	}
	// Python 语义（end_comp.py:219-245）：创建者等待另一拍渲染并返回 None；
	// 第二拍合并自身 inputs 渲染并返回 {"response": ...}
	if first != nil {
		t.Fatalf("首拍（创建者）应返回 nil: %v", first)
	}
	if second == nil || second["response"] != "Hi b" {
		t.Fatalf("第二拍应返回合并渲染结果: %v", second)
	}
}

// TestEnd_Invoke_有模板_Mix 全链验证：SetMix 后 Invoke 两拍行为
// （首拍超时兜底渲染，二拍返回 nil——stream 单一消费方场景）。
func TestEnd_Invoke_有模板_Mix_超时兜底(t *testing.T) {
	e, _ := NewEnd(mustConfig("Hi {{n}}"))
	e.SetMix()
	inner := newFakeVertexSession(t, "n1", "End")
	// GetEnv 默认 5s，但 fake config 无 env 默认值来源时为 0 → 无超时等待；
	// 置显式小超时验证兜底路径
	compSessM, csErr := session.NewComponentSession(inner, false)
	if csErr != nil {
		t.Fatal(csErr)
	}
	out, err := e.render(context.Background(), map[string]any{"n": "mix"}, 0.01, compSessM)
	if err != nil || out == nil {
		t.Fatalf("mix invoke 不符: out=%v err=%v", out, err)
	}
}

// ──────────────────────────── StreamReader 叶子（D-17-3 修正回归） ────────────────────────────

// newStreamReaderForTest 构造真实引擎形态的 StreamReader：
// StreamProcessor + Generator(schema 引用路径) + Run 分发循环（背景 goroutine）。
// 引擎 preStream 递给组件的流叶子即此形态（vertex.go:431-443 一比一）。
func newStreamReaderForTest(t *testing.T, nodeID, rPath string) (*stream_actor.StreamProcessor, *stream_actor.StreamReader) {
	t.Helper()
	p := stream_actor.NewStreamProcessor(nodeID, nil, 0)
	gen := p.Generator(map[string]any{"a": rPath}, nil)
	sr, ok := gen["a"].(*stream_actor.StreamReader)
	if !ok {
		t.Fatalf("Generator 未产出 StreamReader 叶子: %T", gen["a"])
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go p.Run(ctx, ability.ComponentAbilityTransform)
	return p, sr
}

// feedStreamReaderFrame 投递一帧（消息形状 {producer: {path: value}}，对齐 Run 路由）。
func feedStreamReaderFrame(p *stream_actor.StreamProcessor, producer, path string, value any) {
	p.Receive(stream_actor.StreamPayload{
		Message:       map[string]any{producer: map[string]any{path: value}},
		SourceAbility: ability.ComponentAbilityTransform,
	})
}

// feedStreamReaderEnd 投递 END_ 哨兵（manager.py:122-124 end_message 同款形状）。
func feedStreamReaderEnd(p *stream_actor.StreamProcessor, producer string) {
	p.Receive(stream_actor.StreamPayload{
		Message:       map[string]any{producer: "END_" + producer},
		SourceAbility: ability.ComponentAbilityTransform,
	})
}

// TestTemplateProcessor_RenderStream_StreamReader叶子回归 验证流叶判定走
// *stream_actor.StreamReader 分支（D-17-3 修正：引擎真实递出物，非 <-chan any）。
func TestTemplateProcessor_RenderStream_StreamReader叶子回归(t *testing.T) {
	p, sr := newStreamReaderForTest(t, "end1", "${llm1.text}")
	feedStreamReaderFrame(p, "llm1", "text", "f1")
	feedStreamReaderFrame(p, "llm1", "text", "f2")
	feedStreamReaderEnd(p, "llm1") // 主队列 FIFO：f1→f2→END，消费两帧后哨兵终止

	tp := NewTemplateProcessor("{{a}}")
	inputs := map[string]any{"a": sr}
	var datas []any
	for chunk := range tp.RenderStream(context.Background(), inputs, nil, 0) {
		if chunk.Err != nil {
			t.Fatalf("流中错误: %v", chunk.Err)
		}
		datas = append(datas, chunk.Val.Data)
	}
	if len(datas) != 2 || datas[0] != "f1" || datas[1] != "f2" {
		t.Fatalf("StreamReader 叶子逐帧转发不符: %v", datas)
	}
}

// TestTemplateProcessor_RenderStream_StreamReader叶子后到 验证模板位置驱动的惰性消费：
// 渲染启动时队列已有帧，位置到达该段才消费（上屏顺序由位置决定，非到达顺序）。
func TestTemplateProcessor_RenderStream_StreamReader叶子后到(t *testing.T) {
	p, sr := newStreamReaderForTest(t, "end2", "${llm1.text}")
	feedStreamReaderFrame(p, "llm1", "text", "late")
	feedStreamReaderEnd(p, "llm1")
	time.Sleep(50 * time.Millisecond) // 让帧先入队，模拟"位置未到先攒着"

	tp := NewTemplateProcessor("A {{a}} B")
	inputs := map[string]any{"a": sr}
	var datas []any
	for chunk := range tp.RenderStream(context.Background(), inputs, nil, 0) {
		if chunk.Err != nil {
			t.Fatalf("流中错误: %v", chunk.Err)
		}
		datas = append(datas, chunk.Val.Data)
	}
	if len(datas) != 3 || datas[0] != "A " || datas[1] != "late" || datas[2] != " B" {
		t.Fatalf("惰性消费+静态段交错不符: %v", datas)
	}
}

// TestConsumeAllIterators_StreamReader耗尽 验证收尾耗尽识别引擎流叶
// （残帧被丢弃不上屏，队列悬挂收口）。
func TestConsumeAllIterators_StreamReader耗尽(t *testing.T) {
	p, sr := newStreamReaderForTest(t, "end3", "${llm1.text}")
	feedStreamReaderFrame(p, "llm1", "text", "residual")
	feedStreamReaderFrame(p, "llm1", "text", "residual2")
	feedStreamReaderEnd(p, "llm1")

	consumeAllIterators(map[string]any{"a": sr, "b": "scalar"})
	// 耗尽后队列关闭，Next 立即 false——再次耗尽不阻塞即通过
	done := make(chan struct{})
	go func() {
		consumeAllIterators(sr)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("二次耗尽阻塞，StreamReader 未被识别")
	}
}

// TestEnd_OnTransform_真实StreamReader信封 端到端形状回归：
// preStream 信封（单元素 dict-of-StreamReader）→ OnTransform → 信封帧逐个吐出。
func TestEnd_OnTransform_真实StreamReader信封(t *testing.T) {
	e, err := NewEnd(mustConfig("Hi {{a}}"))
	if err != nil {
		t.Fatalf("构造失败: %v", err)
	}
	p, sr := newStreamReaderForTest(t, "end4", "${llm1.text}")
	feedStreamReaderFrame(p, "llm1", "text", "alice")
	feedStreamReaderEnd(p, "llm1")

	inner := newFakeVertexSession(t, "end4", "End")
	envelope := make(chan map[string]any, 1)
	envelope <- map[string]any{"a": sr}
	close(envelope)

	ch, err := e.OnTransform(context.Background(), envelope, inner, nil)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	var resp strings.Builder
	for c := range ch {
		if c.Err != nil {
			t.Fatalf("流中错误: %v", c.Err)
		}
		// TRANSFORM 输出为逐帧信封 map[frame]=stream.OutputSchema{Payload:{response: 段文本}}
		f, ok := c.Val["frame"].(stream.OutputSchema)
		if !ok {
			t.Fatalf("信封形状不符: %v", c.Val)
		}
		resp.WriteString(fmt.Sprintf("%v", f.Payload.(map[string]any)["response"]))
	}
	if resp.String() != "Hi alice" {
		t.Fatalf("模板渲染 StreamReader 叶子不符: %q", resp.String())
	}
}
