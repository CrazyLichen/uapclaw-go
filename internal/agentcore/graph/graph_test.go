package graph

import (
	"context"
	"testing"

	pregel "github.com/uapclaw/uapclaw-go/internal/agentcore/graph/pregel"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 测试工具 ────────────────────────────

// fakeSel 记录调用的最小 selector
type fakeSel struct{ called bool }

func (f *fakeSel) Select(_ context.Context, _ any) ([]string, error) {
	f.called = true
	return []string{"b"}, nil
}

// fakeExec 满足 GeneralExecutor 的最小执行器
type fakeExec struct {
	BaseExecutable[map[string]any, map[string]any]
}

func (f *fakeExec) OnInvoke(_ context.Context, _ map[string]any, _ session.BaseSession, _ map[string]any) (map[string]any, error) {
	return map[string]any{}, nil
}
func (f *fakeExec) OnStream(_ context.Context, _ map[string]any, _ session.BaseSession, _ map[string]any) (<-chan StreamChunk[map[string]any], error) {
	return nil, nil
}
func (f *fakeExec) OnCollect(_ context.Context, _ <-chan map[string]any, _ session.BaseSession, _ map[string]any) (map[string]any, error) {
	return map[string]any{}, nil
}
func (f *fakeExec) OnTransform(_ context.Context, _ <-chan map[string]any, _ session.BaseSession, _ map[string]any) (<-chan StreamChunk[map[string]any], error) {
	return nil, nil
}

// catchErr 捕获 panic 返回 error（构图方法对齐 Python raise → Go panic）
func catchErr(fn func()) (err error) {
	defer func() {
		if r := recover(); r != nil {
			if be, ok := r.(*exception.BaseError); ok {
				err = be
			} else {
				panic(r)
			}
		}
	}()
	fn()
	return nil
}

// ──────────────────────────── 用例 ────────────────────────────

// TestNewPregelGraph_初始字段 构造后各容器非 nil
func TestNewPregelGraph_初始字段(t *testing.T) {
	g := NewPregelGraph()
	if g.nodes == nil || g.edges == nil || g.waits == nil || g.branches == nil || g.branchTargets == nil {
		t.Fatal("构造后容器应非 nil（对应 Python __init__ 各字段初始化）")
	}
	if g.preg != nil || g.sessionRef != nil {
		t.Fatal("preg/sessionRef 初始应为 nil")
	}
}

// TestAddNode_校验分支 对齐 graph.py:86-98 的三个 error 分支与 waits 登记
func TestAddNode_校验分支(t *testing.T) {
	g := NewPregelGraph()
	if err := catchErr(func() { g.AddNode("", nil, false) }); err == nil {
		t.Fatal("空 nodeID 应报 NodeIDInvalid")
	}
	if err := catchErr(func() { g.AddNode("a", nil, false) }); err == nil {
		t.Fatal("node 为 nil 应报 NodeInvalid")
	}
	g.AddNode("a", &fakeExec{}, false)
	if err := catchErr(func() { g.AddNode("a", &fakeExec{}, false) }); err == nil {
		t.Fatal("重复注册应报错（already exist）")
	}
	g.AddNode("b", &fakeExec{}, true)
	if _, ok := g.waits["b"]; !ok {
		t.Fatal("waitForAll=true 应登记 waits")
	}
	if len(g.nodes) != 2 {
		t.Fatalf("应有 2 个节点, got %d", len(g.nodes))
	}
}

// TestAddEdge_校验分支 对齐 graph.py:103-120 四个 error 分支
func TestAddEdge_校验分支(t *testing.T) {
	g := NewPregelGraph()
	cases := []struct {
		name   string
		source any
		target string
	}{
		{"nil source", nil, "b"},
		{"空串 source", "", "b"},
		{"list 含空串", []string{"a", ""}, "b"},
		{"空 target", "a", ""},
	}
	for _, c := range cases {
		if err := catchErr(func() { g.AddEdge(c.source, c.target) }); err == nil {
			t.Fatalf("%s 应报 EdgeInvalid", c.name)
		}
	}
	g.AddEdge("a", "b")
	g.AddEdge([]string{"x", "y"}, "b")
	if len(g.edges) != 2 {
		t.Fatalf("应登记 2 条边, got %d", len(g.edges))
	}
}

// TestStartNode_EndNode 对齐 graph.py:69-84
func TestStartNode_EndNode(t *testing.T) {
	g := NewPregelGraph()
	g.StartNode("a")
	if len(g.edges) != 1 {
		t.Fatal("StartNode 应登记一条边")
	}
	if src, ok := g.edges[0].source.([]string); !ok || len(src) != 1 || src[0] != pregel.Start {
		t.Fatalf("StartNode 源应为 []string{Start}, got %#v", g.edges[0].source)
	}
	g.AddNode("a", &fakeExec{}, false)
	g.EndNode("a")
	if len(g.edges) != 2 {
		t.Fatal("EndNode 应再登记一条边")
	}
	if g.edges[1].target != pregel.End {
		t.Fatal("EndNode 目标应为 End")
	}
}

// TestAddConditionalEdges_覆盖键 对齐 graph.py:122-132：同名覆盖、异名并存
func TestAddConditionalEdges_覆盖键(t *testing.T) {
	g := NewPregelGraph()
	s1, s2 := &fakeSel{}, &fakeSel{}
	g.AddConditionalEdges("a", s1)
	g.AddConditionalEdges("a", s2) // 同类型同名 → 覆盖（临时 getCallableName 用 %T，同型即同名）
	if len(g.branches["a"]) != 1 {
		t.Fatalf("同名 selector 应覆盖, got %d", len(g.branches["a"]))
	}
	if err := catchErr(func() { g.AddConditionalEdges("", s1) }); err == nil {
		t.Fatal("空 source 应报 ConditionEdgeInvalid")
	}
	if err := catchErr(func() { g.AddConditionalEdges("a", nil) }); err == nil {
		t.Fatal("router 为 nil 应报 ConditionEdgeInvalid")
	}
}

// TestRegisterBranchTargets_守卫 对齐 graph.py:134-143：仅 targets>1 才登记
func TestRegisterBranchTargets_守卫(t *testing.T) {
	g := NewPregelGraph()
	g.RegisterBranchTargets("a", []string{"x", "y"})
	if len(g.branchTargets["a"]) != 2 {
		t.Fatal("len>1 应登记")
	}
	g.RegisterBranchTargets("a", []string{"x"})
	if len(g.branchTargets["a"]) != 2 {
		t.Fatal("len==1 不应覆盖已登记项")
	}
}
