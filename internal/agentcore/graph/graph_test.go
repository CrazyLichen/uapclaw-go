package graph

import (
	"context"
	"strings"
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

// TestGetCallableName_三级 对齐 graph.py:403-409 三级回退 + spec 偏差 D-3
func TestGetCallableName_三级(t *testing.T) {
	// 一级：具名函数（SelectorFunc 包装）→ 函数短名
	if got := getCallableName(pregel.SelectorFunc(namedSelector)); got != "namedSelector" {
		t.Fatalf("一级应取函数短名 namedSelector, got %q", got)
	}
	// 一级：匿名闭包 → funcN（不归一 <lambda>，偏差 D-3：不触发覆盖）
	anon := pregel.SelectorFunc(func(_ context.Context, _ any) ([]string, error) { return nil, nil })
	if got := getCallableName(anon); !strings.Contains(got, "func") {
		t.Fatalf("匿名闭包应为编译器名 funcN 形态, got %q", got)
	}
	// 二级：结构体 selector → 类型名（对应 __class__.__name__）
	if got := getCallableName(&fakeSel{}); got != "fakeSel" {
		t.Fatalf("二级应取类型名 fakeSel, got %q", got)
	}
	// 三级：兜底（Go 中实际不可达，仅验证不 panic）
	_ = getCallableName(nil)
}

func namedSelector(_ context.Context, _ any) ([]string, error) { return []string{"b"}, nil }

// TestResolveBarrierGroups 对齐 graph.py:180-237 CNF 解析：
// 互斥前驱（同 branch 两出口的下游）合并 OR 组，共享/独立前驱保持 AND 单元素组
func TestResolveBarrierGroups(t *testing.T) {
	g := NewPregelGraph()
	// branch 节点 "br" 两个出口 exit1/exit2；各自下游 t1/t2 汇入 wait 节点 "w"
	g.RegisterBranchTargets("br", []string{"exit1", "exit2"})
	g.AddEdge("exit1", "t1")
	g.AddEdge("exit2", "t2")
	// w 的前驱：t1（br 出口下游）、t2（br 出口下游）、common（共享）
	g.AddEdge("t1", "w")
	g.AddEdge("t2", "w")
	g.AddEdge("common", "w")

	sources := []map[string]struct{}{
		{"t1": {}}, {"t2": {}}, {"common": {}},
	}
	got := g.resolveBarrierGroups("w", sources)
	if len(got) != 2 {
		t.Fatalf("应解析为 2 组（OR 组 + standalone）, got %d: %#v", len(got), got)
	}
	or, standalone := -1, -1
	for i, grp := range got {
		if len(grp) == 2 {
			or = i
		} else if _, ok := grp["common"]; ok {
			standalone = i
		}
	}
	if or < 0 || standalone < 0 {
		t.Fatalf("应为 {t1,t2} OR 组 + {common} standalone, got %#v", got)
	}
	// branch_targets 为空时原样返回（graph.py:193-194）
	g2 := NewPregelGraph()
	in := []map[string]struct{}{{"a": {}}}
	if out := g2.resolveBarrierGroups("w", in); len(out) != 1 {
		t.Fatal("无 branch_targets 应原样返回")
	}
}

// TestForwardReachable 对齐 graph.py:145-157 BFS 正向可达（visited 含起点自身，Python 行为）
func TestForwardReachable(t *testing.T) {
	g := NewPregelGraph()
	g.AddEdge("s", "m1")
	g.AddEdge("m1", "m2")
	g.AddEdge("m2", "sink")
	reach := g.forwardReachable("s")
	if _, ok := reach["s"]; !ok {
		t.Fatal("Python visited 首轮即 add 起点，应含 s")
	}
	for _, want := range []string{"m1", "m2", "sink"} {
		if _, ok := reach[want]; !ok {
			t.Fatalf("s 应可达 %s, got %#v", want, reach)
		}
	}
}

// TestBranchRoot_嵌套提升 对齐 graph.py:159-178
func TestBranchRoot_嵌套提升(t *testing.T) {
	g := NewPregelGraph()
	g.RegisterBranchTargets("outer", []string{"inner", "exit"})
	g.RegisterBranchTargets("inner", []string{"e1", "e2"})
	parent := g.buildBranchParent()
	if parent["inner"] != "outer" {
		t.Fatalf("inner 应提升至 outer, got %#v", parent)
	}
	if root := branchRoot("inner", parent); root != "outer" {
		t.Fatalf("branchRoot 应回溯至 outer, got %q", root)
	}
	if root := branchRoot("outer", parent); root != "outer" {
		t.Fatalf("无父分支应原地返回, got %q", root)
	}
}
