package flow

import (
	"context"
	"strings"
	"sync"
	"testing"

	graph "github.com/uapclaw/uapclaw-go/internal/agentcore/graph"
	graphexe "github.com/uapclaw/uapclaw-go/internal/agentcore/graph/executable"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	sessionStream "github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/workflow"
)

// fakeWorkflow 测试替身：记录 Invoke 调用并返回固定输出
type fakeWorkflow struct {
	invokeCalled int
	invokeInputs any
	invokeKwargs map[string]any
	invokeOut    any
	invokeErr    error
	streamFrames []workflow.WorkflowChunk
}

func (f *fakeWorkflow) Invoke(ctx context.Context, inputs any,
	sess sessioninterfaces.SessionFacade, modelContext any, kwargs map[string]any) (any, error) {
	f.invokeCalled++
	f.invokeInputs = inputs
	f.invokeKwargs = kwargs
	if f.invokeErr != nil {
		return nil, f.invokeErr
	}
	if f.invokeOut == nil {
		return map[string]any{"response": map[string]any{"done": true}}, nil
	}
	return f.invokeOut, nil
}

// fakeNodeSessionFacade 最小 NodeSessionFacade 替身：invokeSubWorkflow 转调链只要求 SessionFacade 面
// （生产 InvokeFn 槽收 *session.NodeSessionFacade——node.go:42 已断言满足 SessionFacade——后转调私有方法）
type fakeNodeSessionFacade struct {
	sessioninterfaces.SessionFacade
}

func newFakeNodeSession(t *testing.T) sessioninterfaces.SessionFacade {
	t.Helper()
	return &fakeNodeSessionFacade{}
}

// newSubWorkflowComponentForTest 测试构造缝：注入 fake 调用缝，
// 生产构造函数只收 *workflow.Workflow（Python 形参类型 Workflow）。
func newSubWorkflowComponentForTest(fw *fakeWorkflow, cacheStream bool) (*SubWorkflowComponent, error) {
	comp, err := newSubWorkflowComponent(&workflow.Workflow{}, WithCacheStream(cacheStream))
	if err != nil {
		return nil, err
	}
	comp.subWorkflowInvokeFn = fw.Invoke
	comp.subWorkflowStreamFn = fw.Stream
	return comp, nil
}

// TestBuildFinalResult_空累积 返回空 map（Python workflow_comp.py:47-48）
func TestBuildFinalResult_空累积(t *testing.T) {
	s := &SubWorkflowStreamState{}
	got, err := s.BuildFinalResult()
	if err != nil {
		t.Fatalf("期望无错误, got %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("期望空 map, got %v", got)
	}
}

// TestBuildFinalResult_嵌套output展开 复刻 Python :50-55
func TestBuildFinalResult_嵌套output展开(t *testing.T) {
	s := &SubWorkflowStreamState{}
	s.Accumulate(map[string]any{"output": map[string]any{"out": "x"}})
	got, err := s.BuildFinalResult()
	if err != nil {
		t.Fatalf("期望无错误, got %v", err)
	}
	if v, ok := got["out"]; !ok || v != "x" {
		t.Fatalf("期望 out=x, got %v", got)
	}
}

// TestBuildFinalResult_同key字符串拼接 复刻 Python merged[key]+value
func TestBuildFinalResult_同key字符串拼接(t *testing.T) {
	s := &SubWorkflowStreamState{}
	s.Accumulate(map[string]any{"k": "a"})
	s.Accumulate(map[string]any{"k": "b"})
	got, _ := s.BuildFinalResult()
	if got["k"] != "ab" {
		t.Fatalf("期望 k=ab, got %v", got)
	}
}

// TestBuildFinalResult_同key数字求和 int/int、float/float、int/float
func TestBuildFinalResult_同key数字求和(t *testing.T) {
	s := &SubWorkflowStreamState{}
	s.Accumulate(map[string]any{"a": 1, "b": 1.5, "c": 2})
	s.Accumulate(map[string]any{"a": 2, "b": 2.5, "c": 0.5})
	got, _ := s.BuildFinalResult()
	if got["a"] != 3.0 || got["b"] != 4.0 || got["c"] != 2.5 {
		t.Fatalf("期望 a=3 b=4.0 c=2.5, got %v", got)
	}
}

// TestBuildFinalResult_异类型报错 对齐 Python TypeError（dict+dict / str+int）
func TestBuildFinalResult_异类型报错(t *testing.T) {
	cases := []struct {
		name   string
		frames []map[string]any
	}{
		{"dict加dict", []map[string]any{{"k": map[string]any{"x": 1}}, {"k": map[string]any{"y": 2}}}},
		{"str加int", []map[string]any{{"k": "a"}, {"k": 1}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := &SubWorkflowStreamState{}
			for _, f := range c.frames {
				s.Accumulate(f)
			}
			if _, err := s.BuildFinalResult(); err == nil {
				t.Fatalf("期望报错（对齐 Python TypeError）, got nil")
			}
		})
	}
}

// TestBuildFinalResult_非dict帧跳过 复刻 Python isinstance(output, dict) 过滤
func TestBuildFinalResult_非dict帧跳过(t *testing.T) {
	s := &SubWorkflowStreamState{}
	s.Accumulate("not a dict")
	s.Accumulate(42)
	s.Accumulate(map[string]any{"k": "v"})
	got, _ := s.BuildFinalResult()
	if got["k"] != "v" || len(got) != 1 {
		t.Fatalf("期望仅 k=v, got %v", got)
	}
}

// TestClear 清空后重新累积
func TestClear(t *testing.T) {
	s := &SubWorkflowStreamState{}
	s.Accumulate(map[string]any{"k": "v"})
	s.Clear()
	got, _ := s.BuildFinalResult()
	if len(got) != 0 {
		t.Fatalf("Clear 后期望空, got %v", got)
	}
	s.Accumulate(map[string]any{"k2": "v2"})
	got, _ = s.BuildFinalResult()
	if got["k2"] != "v2" {
		t.Fatalf("Clear 后应可重新累积, got %v", got)
	}
}

// TestNewSubWorkflowComponent_nil拒绝 构造参数校验（Python __init__ :70-72）
func TestNewSubWorkflowComponent_nil拒绝(t *testing.T) {
	_, err := NewSubWorkflowComponent(nil)
	if err == nil {
		t.Fatal("期望 nil sub_workflow 报错")
	}
	if !strings.Contains(err.Error(), "sub_workflow is None") {
		t.Fatalf("文案复刻 Python, got %v", err)
	}
}

// TestSubWorkflowComponent_三override 方法提升遮蔽（Python :87-91 / :93-94）
func TestSubWorkflowComponent_三override(t *testing.T) {
	fw := &fakeWorkflow{}
	comp, err := newSubWorkflowComponentForTest(fw, false)
	if err != nil {
		t.Fatalf("构造失败: %v", err)
	}
	if !comp.GraphInvoker() {
		t.Fatal("GraphInvoker 应恒 true（Python :87-88）")
	}
	if comp.ComponentType() != SubWorkflowComponentType {
		t.Fatalf("期望 %q, got %q", SubWorkflowComponentType, comp.ComponentType())
	}
	if comp.SubWorkflow() == nil {
		t.Fatal("SubWorkflow() 应返回注入实例")
	}
}

// TestSubWorkflowComponent_Invoke_空缓存短路 Quirk A 逐字复刻锁定（D-14b-1 已知行为）：
// Python workflow_comp.py:81-84 空累积 build_final_result 返回 {}（非 None）→
// cached is not None 命中 → 批入腿不执行子工作流（用户拍板：对齐按上游真实实现）
func TestSubWorkflowComponent_Invoke_空缓存短路(t *testing.T) {
	fw := &fakeWorkflow{}
	comp, _ := newSubWorkflowComponentForTest(fw, true) // cacheStream=true
	sess := newFakeNodeSession(t)
	env := map[string]any{
		graph.InputsKey: map[string]any{"q": "hi"},
		graph.ConfigKey: nil,
	}
	out, err := comp.invokeSubWorkflow(context.Background(), env, sess)
	if err != nil {
		t.Fatalf("空缓存短路不应报错, got %v", err)
	}
	if fw.invokeCalled != 0 {
		t.Fatalf("Quirk A 复刻：空缓存即短路, 不应转调子工作流, invokeCalled=%d", fw.invokeCalled)
	}
	m, ok := out["output"].(map[string]any)
	if !ok || len(m) != 0 {
		t.Fatalf("期望 {'output': {}} 形态, got %v", out)
	}
}

// TestSubWorkflowComponent_Invoke_非空短路 缓存命中不重跑
func TestSubWorkflowComponent_Invoke_非空短路(t *testing.T) {
	fw := &fakeWorkflow{}
	comp, _ := newSubWorkflowComponentForTest(fw, true)
	comp.streamState.Accumulate(map[string]any{"output": map[string]any{"out": "cached"}})
	sess := newFakeNodeSession(t)
	env := map[string]any{graph.InputsKey: map[string]any{"q": "hi"}, graph.ConfigKey: nil}
	out, err := comp.invokeSubWorkflow(context.Background(), env, sess)
	if err != nil {
		t.Fatalf("短路路径不应报错: %v", err)
	}
	if fw.invokeCalled != 0 {
		t.Fatalf("缓存命中不应重跑子工作流, invokeCalled=%d", fw.invokeCalled)
	}
	m, ok := out["output"].(map[string]any)
	if !ok || m["out"] != "cached" {
		t.Fatalf("期望 {'output': cached} 形态, got %v", out)
	}
}

// TestSubWorkflowComponent_Invoke_关缓存恒转调 cache_stream=false
func TestSubWorkflowComponent_Invoke_关缓存恒转调(t *testing.T) {
	fw := &fakeWorkflow{}
	comp, _ := newSubWorkflowComponentForTest(fw, false)
	comp.streamState.Accumulate(map[string]any{"output": map[string]any{"out": "cached"}})
	sess := newFakeNodeSession(t)
	env := map[string]any{graph.InputsKey: map[string]any{"q": "hi"}, graph.ConfigKey: nil}
	if _, err := comp.invokeSubWorkflow(context.Background(), env, sess); err != nil {
		t.Fatalf("转调不应报错: %v", err)
	}
	if fw.invokeCalled != 1 {
		t.Fatalf("cache_stream=false 恒转调, invokeCalled=%d", fw.invokeCalled)
	}
	if fw.invokeKwargs["is_sub"] != true {
		t.Fatalf("期望 kwargs 携带 is_sub=true（Python :82-83）, got %v", fw.invokeKwargs)
	}
}

// fakeWorkflow.Stream 流式替身（返回预置帧序列）
func (f *fakeWorkflow) Stream(ctx context.Context, inputs any,
	sess sessioninterfaces.SessionFacade, modelContext any, streamModes []sessionStream.StreamMode,
	kwargs map[string]any) (<-chan workflow.WorkflowChunk, error) {
	out := make(chan workflow.WorkflowChunk)
	go func() {
		defer close(out)
		for _, fr := range f.streamFrames {
			out <- fr
		}
	}()
	return out, nil
}

// TestSubWorkflowComponent_Stream_逐帧累积并转发 cache_stream=true（Python stream :76-81）
func TestSubWorkflowComponent_Stream_逐帧累积并转发(t *testing.T) {
	fw := &fakeWorkflow{streamFrames: []workflow.WorkflowChunk{
		&sessionStream.OutputSchema{Type: "message", Index: 0, Payload: map[string]any{"out": "a"}},
		&sessionStream.OutputSchema{Type: "message", Index: 1, Payload: map[string]any{"out": "b"}},
	}}
	comp, _ := newSubWorkflowComponentForTest(fw, true)
	env := map[string]any{graph.InputsKey: map[string]any{"q": "hi"}, graph.ConfigKey: nil}
	ch, err := comp.StreamFn(context.Background(), env, nil)
	if err != nil {
		t.Fatalf("流启动失败: %v", err)
	}
	var got []graphexe.StreamChunk[any]
	for c := range ch {
		got = append(got, c)
	}
	if len(got) != 2 || got[0].Err != nil {
		t.Fatalf("期望两帧无错误, got %v", got)
	}
	// 转发形态：Schema 载荷 dict（D-14b-6，流边路径提取可用形态）
	if got[0].Val.(map[string]any)["out"] != "a" {
		t.Fatalf("期望帧承载载荷 dict, got %v", got[0].Val)
	}
	// 累积的是原始帧值：Schema 帧 BuildFinalResult 跳过（对齐 Python isinstance dict）
	merged, _ := comp.streamState.BuildFinalResult()
	if len(merged) != 0 {
		t.Fatalf("Schema 帧应被合并过滤, got %v", merged)
	}
}

// TestSubWorkflowComponent_Stream_关缓存不累积 cache_stream=false
func TestSubWorkflowComponent_Stream_关缓存不累积(t *testing.T) {
	fw := &fakeWorkflow{streamFrames: []workflow.WorkflowChunk{
		&sessionStream.CustomSchema{Type: "out", Data: map[string]any{"output": map[string]any{"out": "x"}}},
	}}
	comp, _ := newSubWorkflowComponentForTest(fw, false)
	env := map[string]any{graph.InputsKey: map[string]any{}, graph.ConfigKey: nil}
	ch, _ := comp.StreamFn(context.Background(), env, nil)
	for range ch {
	}
	merged, _ := comp.streamState.BuildFinalResult()
	if len(merged) != 0 {
		t.Fatalf("cache_stream=false 不应累积, got %v", merged)
	}
}

// TestSubWorkflowComponent_Stream_流后批式联动 Schema 帧过滤后仍短路（Quirk A 复刻）
func TestSubWorkflowComponent_Stream_dict帧累积命中短路(t *testing.T) {
	// Schema 帧经累积后被合并过滤（对齐 Python isinstance dict），缓存为空 {}；
	// Quirk A 复刻：cache_stream 开启时空缓存 {} 仍短路（cached 非 None），
	// 流后批式 invoke 同样不转调子工作流。
	fw := &fakeWorkflow{streamFrames: []workflow.WorkflowChunk{
		&sessionStream.CustomSchema{Type: "out", Data: map[string]any{"output": map[string]any{"out": "x"}}},
	}}
	comp, _ := newSubWorkflowComponentForTest(fw, true)
	env := map[string]any{graph.InputsKey: map[string]any{}, graph.ConfigKey: nil}
	ch, _ := comp.StreamFn(context.Background(), env, nil)
	for range ch {
	}
	// 流后批式 invoke：空缓存（Schema 帧被过滤）→ {} 非 None → 仍短路
	out, err := comp.invokeSubWorkflow(context.Background(), env, newFakeNodeSession(t))
	if err != nil {
		t.Fatalf("短路路径不应报错: %v", err)
	}
	if fw.invokeCalled != 0 {
		t.Fatalf("Quirk A 复刻：cache_stream 开启恒短路, invokeCalled=%d", fw.invokeCalled)
	}
	if _, ok := out["output"].(map[string]any); !ok {
		t.Fatalf("期望 {'output': {}} 形态, got %v", out)
	}
}

// TestGetStreamOutput_两态 开=合并结果 关=nil（Python get_stream_output :81-85）
func TestGetStreamOutput_两态(t *testing.T) {
	fw := &fakeWorkflow{}
	compOn, _ := newSubWorkflowComponentForTest(fw, true)
	compOn.streamState.Accumulate(map[string]any{"output": map[string]any{"out": "v"}})
	if m := compOn.GetStreamOutput(); m == nil || m["out"] != "v" {
		t.Fatalf("开启时期望合并结果, got %v", m)
	}
	compOff, _ := newSubWorkflowComponentForTest(fw, false)
	compOff.streamState.Accumulate(map[string]any{"output": map[string]any{"out": "v"}})
	if m := compOff.GetStreamOutput(); m != nil {
		t.Fatalf("关闭时期望 nil, got %v", m)
	}
}

// TestSubWorkflowComponent_元类装饰四事件对断言 8.14 回填①：
// __init_subclass__ 装饰序（D-17-5 等价承载）在本组件经 OnInvoke/OnStream 时
// ComponentBatchInput/ComponentBatchOutput 成对、ComponentStreamOutput 逐帧触发。
func TestSubWorkflowComponent_元类装饰四事件对断言(t *testing.T) {
	fw := &fakeWorkflow{streamFrames: []workflow.WorkflowChunk{
		&sessionStream.OutputSchema{Type: "message", Index: 0, Payload: "f0"},
		&sessionStream.OutputSchema{Type: "message", Index: 1, Payload: "f1"},
	}}
	comp, _ := newSubWorkflowComponentForTest(fw, true)
	inner := newFakeVertexSession(t, "n_sub", "SubWorkflow")
	env := map[string]any{graph.InputsKey: map[string]any{}, graph.ConfigKey: nil}

	fwCb := callback.GetCallbackFramework()
	var seq []callback.WorkflowEventType
	var mu sync.Mutex
	collect := func(ev callback.WorkflowEventType) {
		fwCb.OnWorkflow(ev, func(ctx context.Context, data *callback.WorkflowEventData) any {
			mu.Lock()
			defer mu.Unlock()
			seq = append(seq, data.Event)
			return nil
		})
	}
	collect(callback.ComponentBatchInput)
	collect(callback.ComponentBatchOutput)
	collect(callback.ComponentStreamOutput)

	// 批入轨：Input → Output 成对
	if _, err := comp.OnInvoke(context.Background(), env, inner, nil); err != nil {
		t.Fatalf("OnInvoke 失败: %v", err)
	}
	mu.Lock()
	invokeSeq := append([]callback.WorkflowEventType(nil), seq...)
	seq = seq[:0]
	mu.Unlock()
	if len(invokeSeq) != 2 || invokeSeq[0] != callback.ComponentBatchInput || invokeSeq[1] != callback.ComponentBatchOutput {
		t.Fatalf("invoke 轨四事件未成对: %v", invokeSeq)
	}

	// 流轨：Input → 逐帧 StreamOutput(×2) → Output
	ch, err := comp.OnStream(context.Background(), env, inner, nil)
	if err != nil {
		t.Fatalf("OnStream 失败: %v", err)
	}
	for range ch {
	}
	// 流轨（Python on_stream 装饰序，component.py:52-58）：
	// emit_before(BATCH_INPUT) → emit_after(STREAM_OUTPUT, per_item 逐帧触发)。
	// transform_io 只触发 transform 订阅者，不进 OnWorkflow 序列；
	// 流轨对常规订阅者无末尾 BATCH_OUTPUT。
	mu.Lock()
	streamSeq := append([]callback.WorkflowEventType(nil), seq...)
	mu.Unlock()
	if len(streamSeq) != 3 ||
		streamSeq[0] != callback.ComponentBatchInput ||
		streamSeq[1] != callback.ComponentStreamOutput ||
		streamSeq[2] != callback.ComponentStreamOutput {
		t.Fatalf("stream 轨序列不符（Input→逐帧 StreamOutput×2，无末尾 Output）: %v", streamSeq)
	}
}
