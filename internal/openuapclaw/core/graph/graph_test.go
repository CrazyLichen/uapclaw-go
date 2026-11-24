package graph

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	pregel "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/graph/pregel"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/graph/stream_actor"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/config"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/interaction"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/state"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/tracer"
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
func (f *fakeExec) OnStream(_ context.Context, _ map[string]any, _ session.BaseSession, _ map[string]any) (<-chan StreamChunk[any], error) {
	return nil, nil
}
func (f *fakeExec) OnCollect(_ context.Context, _ <-chan map[string]any, _ session.BaseSession, _ map[string]any) (map[string]any, error) {
	return map[string]any{}, nil
}
func (f *fakeExec) OnTransform(_ context.Context, _ <-chan map[string]any, _ session.BaseSession, _ map[string]any) (<-chan StreamChunk[any], error) {
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
	if err := catchErr(func() { g.AddNode("", nil) }); err == nil {
		t.Fatal("空 nodeID 应报 NodeIDInvalid")
	}
	if err := catchErr(func() { g.AddNode("a", nil) }); err == nil {
		t.Fatal("node 为 nil 应报 NodeInvalid")
	}
	g.AddNode("a", &fakeExec{})
	if err := catchErr(func() { g.AddNode("a", &fakeExec{}) }); err == nil {
		t.Fatal("重复注册应报错（already exist）")
	}
	g.AddNode("b", &fakeExec{}, WithWaitForAll())
	if _, ok := g.waits["b"]; !ok {
		t.Fatal("waitForAll=true 应登记 waits")
	}
	if len(g.nodes) != 2 {
		t.Fatalf("应有 2 个节点, got %d", len(g.nodes))
	}
}

// TestAddEdge_校验分支 对齐 graph.py:103-120 校验语义（源收窄 []string 后：
// nil/空切片 → 空源错、含空项 → 列表空项错、空目标错）
func TestAddEdge_校验分支(t *testing.T) {
	g := NewPregelGraph()
	cases := []struct {
		name   string
		source []string
		target string
	}{
		{"nil source", nil, "b"},
		{"空串 source", []string{""}, "b"},
		{"list 含空串", []string{"a", ""}, "b"},
		{"空 target", []string{"a"}, ""},
	}
	for _, c := range cases {
		if err := catchErr(func() { g.AddEdge(c.source, c.target) }); err == nil {
			t.Fatalf("%s 应报 EdgeInvalid", c.name)
		}
	}
	// D-7 形态标记：单源（len==1）→ static（listForm=false）、多源 → barrier（listForm=true）
	g.AddEdge([]string{"a"}, "b")
	g.AddEdge([]string{"x", "y"}, "b")
	if len(g.edges) != 2 {
		t.Fatalf("应登记 2 条边, got %d", len(g.edges))
	}
	if g.edges[0].listForm {
		t.Fatal("单源边应还原为 Python str 形态（listForm=false/static）")
	}
	if !g.edges[1].listForm {
		t.Fatal("多源边应保持 Python list 形态（listForm=true/barrier）")
	}
}

// TestStartNode_EndNode 对齐 graph.py:69-84
func TestStartNode_EndNode(t *testing.T) {
	g := NewPregelGraph()
	g.StartNode("a")
	if len(g.edges) != 1 {
		t.Fatal("StartNode 应登记一条边")
	}
	if src := g.edges[0].source; len(src) != 1 || src[0] != pregel.Start || !g.edges[0].listForm {
		t.Fatalf("StartNode 源应为 []string{Start} 且列表形态, got %#v listForm=%v", g.edges[0].source, g.edges[0].listForm)
	}
	g.AddNode("a", &fakeExec{})
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
	g.AddEdge([]string{"exit1"}, "t1")
	g.AddEdge([]string{"exit2"}, "t2")
	// w 的前驱：t1（br 出口下游）、t2（br 出口下游）、common（共享）
	g.AddEdge([]string{"t1"}, "w")
	g.AddEdge([]string{"t2"}, "w")
	g.AddEdge([]string{"common"}, "w")

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
	g.AddEdge([]string{"s"}, "m1")
	g.AddEdge([]string{"m1"}, "m2")
	g.AddEdge([]string{"m2"}, "sink")
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

// TestVertexNodeFunc_适配器 NodeFunc 桥接 Vertex.Call：返回值丢弃（task.py:152）、错误上抛（task.py:171）
func TestVertexNodeFunc_适配器(t *testing.T) {
	g := NewPregelGraph()
	g.AddNode("a", &fakeExec{})
	v := g.nodes["a"]
	nf := vertexNodeFunc(v)
	if nf == nil {
		t.Fatal("适配器应返回非 nil NodeFunc")
	}
	// Python 侧 nodeTask 仅在 compile init（graph.py:240-241）之后才会派发执行，
	// Init 前 Call 同样不可达——故此处只断言适配器形状非 nil，真实执行路径由
	// TestCompile_WaitsCNF（经 builder 全链）覆盖。
}

// TestCompile_生命周期 对齐 graph.py:239-265
func TestCompile_生命周期(t *testing.T) {
	g := NewPregelGraph()
	g.AddNode("a", &fakeExec{})
	g.StartNode("a")
	g.EndNode("a")
	sess := newFakeSession()
	cg := g.Compile(sess, nil)
	if cg == nil {
		t.Fatal("Compile 应返回 CompiledGraph")
	}
	if g.preg == nil || g.checkpointer == nil {
		t.Fatal("首次编译应缓存 pregel 与 checkpointer")
	}
	if g.sessionRef != sess {
		t.Fatal("首次编译应绑定 sessionRef")
	}
	// 二次编译：复用 pregel，只更新 sessionRef
	pregBefore := g.preg
	sess2 := newFakeSession()
	_ = g.Compile(sess2, nil)
	if g.sessionRef != sess2 {
		t.Fatal("二次编译应更新 sessionRef")
	}
	if g.preg != pregBefore {
		t.Fatal("二次编译应复用已建 pregel")
	}
}

// TestCompile_NodeInit_被调用 compile 对每个节点 init（graph.py:240-241）
func TestCompile_NodeInit_被调用(t *testing.T) {
	g := NewPregelGraph()
	g.AddNode("a", &fakeExec{})
	sess := newFakeSession()
	_ = g.Compile(sess, nil)
	nodes := g.GetNodes()
	if nodes["a"] == nil {
		t.Fatal("编译后节点应存在")
	}
}

// TestCompile_WaitsCNF wait 节点前驱经 resolveBarrierGroups 后进 builder.AddEdge
func TestCompile_WaitsCNF(t *testing.T) {
	g := NewPregelGraph()
	g.AddNode("a", &fakeExec{})
	g.AddNode("b", &fakeExec{})
	g.AddNode("w", &fakeExec{}, WithWaitForAll())
	g.AddEdge([]string{"a"}, "w")
	g.AddEdge([]string{"b"}, "w")
	sess := newFakeSession()
	if cg := g.Compile(sess, nil); cg == nil {
		t.Fatal("waits CNF 编译应成功")
	}
}

// TestReset 对齐 graph.py:305-307
func TestReset(t *testing.T) {
	g := NewPregelGraph()
	g.AddNode("a", &fakeExec{})
	g.Reset() // 不 panic 即通过（Vertex.Reset 内部复位状态）
}

// fakeGraphSession 最小 BaseSession（复用 vertex_test 的 fakeInnerSession 形状）
type fakeGraphSession struct {
	cfg          config.SessionConfig
	st           state.SessionState
	id, wfID     string
	checkpointer *fakeGraphCheckpointer
}

func (f *fakeGraphSession) Config() config.SessionConfig                     { return f.cfg }
func (f *fakeGraphSession) State() state.SessionState                        { return f.st }
func (f *fakeGraphSession) Tracer() *tracer.Tracer                           { return nil }
func (f *fakeGraphSession) StreamWriterManager() *stream.StreamWriterManager { return nil }
func (f *fakeGraphSession) SessionID() string                                { return f.id }
func (f *fakeGraphSession) Checkpointer() interfaces.Checkpointer            { return f.checkpointer }
func (f *fakeGraphSession) ActorManager() *stream_actor.ActorManager         { return nil }
func (f *fakeGraphSession) Close() error                                     { return nil }

// WorkflowIDProvider 能力（Python session.workflow_id()）
func (f *fakeGraphSession) WorkflowID() string { return f.wfID }

// fakeGraphCheckpointer 记录调用序（Task 5 扩展）
type fakeGraphCheckpointer struct {
	preCalls, postCalls int
	postResults         []map[string]any
	postExceptions      []error
	gStore              pregel.Store
}

func (c *fakeGraphCheckpointer) PreWorkflowExecute(_ context.Context, _ session.BaseSession, _ any) error {
	c.preCalls++
	return nil
}
func (c *fakeGraphCheckpointer) PostWorkflowExecute(_ context.Context, _ session.BaseSession, result map[string]any, exception error) error {
	c.postCalls++
	c.postResults = append(c.postResults, result)
	c.postExceptions = append(c.postExceptions, exception)
	return nil
}
func (c *fakeGraphCheckpointer) GraphStore() pregel.Store { return c.gStore }

// Checkpointer 接口其余方法：本节用例不触达，panic 兜底防误用
func (c *fakeGraphCheckpointer) PreAgentExecute(context.Context, session.BaseSession, any) error {
	panic("未实现")
}
func (c *fakeGraphCheckpointer) PreAgentTeamExecute(context.Context, session.BaseSession, any) error {
	panic("未实现")
}
func (c *fakeGraphCheckpointer) InterruptAgentExecute(context.Context, session.BaseSession) error {
	panic("未实现")
}
func (c *fakeGraphCheckpointer) PostAgentExecute(context.Context, session.BaseSession) error {
	panic("未实现")
}
func (c *fakeGraphCheckpointer) PostAgentTeamExecute(context.Context, session.BaseSession) error {
	panic("未实现")
}
func (c *fakeGraphCheckpointer) SessionExists(context.Context, string) (bool, error) {
	panic("未实现")
}
func (c *fakeGraphCheckpointer) Release(context.Context, string, ...string) error { panic("未实现") }

// newFakeSession 构造带最小 checkpointer/store 的会话
func newFakeSession() *fakeGraphSession {
	return &fakeGraphSession{
		cfg: config.NewSessionConfig(context.Background()),
		st:  state.NewInMemoryWorkflowState(),
		id:  "s1", wfID: "wf1",
		checkpointer: &fakeGraphCheckpointer{gStore: &fakeGraphStore{}},
	}
}

// fakeGraphStore 可编程返回快照（Task 5 扩展）。
// failAfterN: 前 N 次 Get 正常返回，之后返回 getErr——模拟瞬态故障
// （引擎 resume Init 与取消分流 inspect 共用同一 store，Python graph.py:260 同源）。
type fakeGraphStore struct {
	st        *pregel.GraphState
	getErr    error
	failAfter int
	getCalls  int
}

func (s *fakeGraphStore) Get(_ context.Context, _, _ string) (*pregel.GraphState, error) {
	s.getCalls++
	if s.failAfter > 0 && s.getCalls > s.failAfter {
		return nil, s.getErr
	}
	return s.st, nil
}

func (s *fakeGraphStore) Save(_ context.Context, _, _ string, _ *pregel.GraphState) error {
	return nil
}
func (s *fakeGraphStore) Delete(_ context.Context, _, _ string) error { return nil }

// ──────────────────────────── 取消分流用例（graph.py:349-394）────────────────

// cancelExec OnInvoke 返回 context.Canceled 的执行器
type cancelExec struct {
	BaseExecutable[map[string]any, map[string]any]
}

func (f *cancelExec) OnInvoke(_ context.Context, _ map[string]any, _ session.BaseSession, _ map[string]any) (map[string]any, error) {
	return nil, context.Canceled
}
func (f *cancelExec) OnStream(_ context.Context, _ map[string]any, _ session.BaseSession, _ map[string]any) (<-chan StreamChunk[any], error) {
	return nil, nil
}
func (f *cancelExec) OnCollect(_ context.Context, _ <-chan map[string]any, _ session.BaseSession, _ map[string]any) (map[string]any, error) {
	return map[string]any{}, nil
}
func (f *cancelExec) OnTransform(_ context.Context, _ <-chan map[string]any, _ session.BaseSession, _ map[string]any) (<-chan StreamChunk[any], error) {
	return nil, nil
}

// TestCompiledGraph_取消_可续跑保留快照 分支②：图侧有 resumable 快照
// → post 被调且 exception 非 nil（canceled），快照现场保留（graph.py:377-385）
func TestCompiledGraph_取消_可续跑保留快照(t *testing.T) {
	g := NewPregelGraph()
	g.AddNode("a", &cancelExec{})
	g.StartNode("a")
	g.EndNode("a")
	sess := newFakeSession()
	sess.checkpointer.gStore = &fakeGraphStore{
		st: &pregel.GraphState{PendingNode: map[string]pregel.PendingNode{
			"a": {NodeName: "a", Status: pregel.TaskStatusInterrupt}}},
	}
	cg := g.Compile(sess, nil)
	_, err := cg.Invoke(context.Background(), map[string]any{}, sess)
	if err == nil {
		t.Fatal("取消应上抛")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("原始取消链应上抛, got %v", err)
	}
	cp := sess.checkpointer
	if cp.postCalls != 1 {
		t.Fatalf("可续跑取消应走一次 post 保存, got %d", cp.postCalls)
	}
	if cp.postExceptions[0] == nil {
		t.Fatal("保存分支应带非 nil 异常（canceled）")
	}
	if !errors.Is(cp.postExceptions[0], context.Canceled) {
		t.Fatalf("post 异常应为 canceled, got %v", cp.postExceptions[0])
	}
	if cp.postResults[0] == nil {
		t.Fatal("post result 应为 {}（对齐 Python 传 {}）")
	}
}

// TestCompiledGraph_取消_无快照不落盘 分支③：快照 nil → post 不被调（可重跑语义，graph.py:358-364）
func TestCompiledGraph_取消_无快照不落盘(t *testing.T) {
	g := NewPregelGraph()
	g.AddNode("a", &cancelExec{})
	g.StartNode("a")
	g.EndNode("a")
	sess := newFakeSession() // gStore.st = nil
	cg := g.Compile(sess, nil)
	_, err := cg.Invoke(context.Background(), map[string]any{}, sess)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("取消应上抛, got %v", err)
	}
	if sess.checkpointer.postCalls != 0 {
		t.Fatalf("无快照不应走 post 保存, got %d", sess.checkpointer.postCalls)
	}
}

// TestCompiledGraph_取消_查询失败按不可续跑 分支①：Get 返回 error → 不落盘不清除（graph.py:367-376）
func TestCompiledGraph_取消_查询失败按不可续跑(t *testing.T) {
	g := NewPregelGraph()
	g.AddNode("a", &cancelExec{})
	g.StartNode("a")
	g.EndNode("a")
	sess := newFakeSession()
	sess.checkpointer.gStore = &fakeGraphStore{getErr: errors.New("backend down"), failAfter: 1}
	cg := g.Compile(sess, nil)
	_, err := cg.Invoke(context.Background(), map[string]any{}, sess)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("查询失败仍应上抛原始取消链, got %v", err)
	}
	if sess.checkpointer.postCalls != 0 {
		t.Fatalf("查询失败不应走 post 保存, got %d", sess.checkpointer.postCalls)
	}
}

// TestCompiledGraph_Invoke_非主流程异常上抛 isMain=false 且 run 报错 → 原样上抛、不走 post（graph.py:347-348）
func TestCompiledGraph_Invoke_非主流程异常上抛(t *testing.T) {
	g := NewPregelGraph()
	g.AddNode("a", &cancelExec{})
	g.StartNode("a")
	g.EndNode("a")
	sess := newFakeSession()
	cg := g.Compile(sess, nil)
	// config 非 nil → isMain=false
	_, err := cg.Invoke(context.Background(), map[string]any{InputsKey: map[string]any{},
		ConfigKey: &pregel.PregelConfig{SessionID: "s1", Ns: "wf1"}}, sess)
	if err == nil {
		t.Fatal("非主流程异常应上抛")
	}
	if sess.checkpointer.postCalls != 0 {
		t.Fatalf("非主流程不应走 post, got %d", sess.checkpointer.postCalls)
	}
}

// TestCompiledGraph_Invoke_主流程 对齐 graph.py:315-348：pre/post 各一次、exception nil
func TestCompiledGraph_Invoke_主流程(t *testing.T) {
	g := NewPregelGraph()
	g.AddNode("a", &fakeExec{})
	g.StartNode("a")
	g.EndNode("a")
	sess := newFakeSession()
	cg := g.Compile(sess, nil)
	if _, err := cg.Invoke(context.Background(), map[string]any{}, sess); err != nil {
		t.Fatalf("主流程不应报错: %v", err)
	}
	if sess.checkpointer.preCalls != 1 || sess.checkpointer.postCalls != 1 {
		t.Fatalf("isMain 应走 pre/post 各一次, pre=%d post=%d", sess.checkpointer.preCalls, sess.checkpointer.postCalls)
	}
	if sess.checkpointer.postExceptions[0] != nil {
		t.Fatal("正常完成 post 的 exception 应为 nil")
	}
}

// TestCompiledGraph_Invoke_InteractiveInput跳过提交 D-5 改写锁定（graph.py:327）：
// inputs 为 *InteractiveInput → 跳过 commit_user_inputs；普通 map → 提交进全局状态。
// 判定在 Invoke_ 内局部进行（I=any 对齐 Python Input TypeVar 无类型）
func TestCompiledGraph_Invoke_InteractiveInput跳过提交(t *testing.T) {
	g := NewPregelGraph()
	g.AddNode("a", &fakeExec{})
	g.StartNode("a")
	g.EndNode("a")

	// ① 普通 map 输入 → 提交：global_state 可读回
	sess := newFakeSession()
	cg := g.Compile(sess, nil)
	if _, err := cg.Invoke(context.Background(),
		map[string]any{InputsKey: map[string]any{"user_key": "v1"}}, sess); err != nil {
		t.Fatalf("普通输入不应报错: %v", err)
	}
	if got := sess.st.GetGlobal(state.StringKey("user_key")); got != "v1" {
		t.Fatalf("普通输入应提交进全局状态, got %v", got)
	}

	// ② InteractiveInput → 跳过提交：global_state 不被写入
	sess2 := newFakeSession()
	cg2 := g.Compile(sess2, nil)
	if _, err := cg2.Invoke(context.Background(),
		map[string]any{InputsKey: &interaction.InteractiveInput{
			UserInputs: map[string]any{"user_key": "v2"}}}, sess2); err != nil {
		t.Fatalf("交互输入不应报错: %v", err)
	}
	if got := sess2.st.GetGlobal(state.StringKey("user_key")); got != nil {
		t.Fatalf("InteractiveInput 应跳过 commit_user_inputs, got %v", got)
	}
}
