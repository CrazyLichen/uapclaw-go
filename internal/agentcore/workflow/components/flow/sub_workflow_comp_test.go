package flow

import (
	"context"
	"strings"
	"testing"

	graph "github.com/uapclaw/uapclaw-go/internal/agentcore/graph"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/workflow"
)

// fakeWorkflow 测试替身：记录 Invoke 调用并返回固定输出
type fakeWorkflow struct {
	invokeCalled int
	invokeInputs any
	invokeKwargs map[string]any
	invokeOut    *workflow.WorkflowOutput
	invokeErr    error
}

func (f *fakeWorkflow) Invoke(ctx context.Context, inputs any,
	sess sessioninterfaces.SessionFacade, modelContext any, kwargs map[string]any) (*workflow.WorkflowOutput, error) {
	f.invokeCalled++
	f.invokeInputs = inputs
	f.invokeKwargs = kwargs
	if f.invokeErr != nil {
		return nil, f.invokeErr
	}
	if f.invokeOut == nil {
		return &workflow.WorkflowOutput{Result: map[string]any{"done": true}, State: workflow.WorkflowExecutionStateCompleted}, nil
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

// TestSubWorkflowComponent_Invoke_空缓存不短路 Quirk A 修复锁定（D-14b-1）
func TestSubWorkflowComponent_Invoke_空缓存不短路(t *testing.T) {
	fw := &fakeWorkflow{}
	comp, _ := newSubWorkflowComponentForTest(fw, true) // cacheStream=true
	sess := newFakeNodeSession(t)
	env := map[string]any{
		graph.InputsKey: map[string]any{"q": "hi"},
		graph.ConfigKey: nil,
	}
	out, err := comp.invokeSubWorkflow(context.Background(), env, sess)
	if err != nil {
		t.Fatalf("期望真实转调, got err %v", err)
	}
	if fw.invokeCalled != 1 {
		t.Fatalf("空缓存必须转调子工作流（D-14b-1）, invokeCalled=%d", fw.invokeCalled)
	}
	if fw.invokeKwargs["is_sub"] != true {
		t.Fatalf("期望 kwargs 携带 is_sub=true, got %v", fw.invokeKwargs)
	}
	if out == nil {
		t.Fatal("期望有输出")
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
}
