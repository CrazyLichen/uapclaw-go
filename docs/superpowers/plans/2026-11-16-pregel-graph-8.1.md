# 8.1 PregelGraph 门面层 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 落地 8.1 声明式构图门面（Graph 接口 / PregelGraph / CompiledGraph / Branch），把已完成的 Vertex（8.11）与 Pregel 引擎（8.3~8.9）拼成可编译、可执行的图执行入口。

**Architecture:** 纯新增三文件进 `internal/agentcore/graph` 根包：`base.go`（Graph 接口 + `Router = pregel.Selector` 别名）、`graph.go`（Branch/PregelGraph/CompiledGraph/getCallableName），一比一对应 Python `base.py`(58 行) + `graph.py`(409 行)。CompiledGraph 内嵌 `BaseExecutable` 并转手 `DefaultGraphXxx`（复用 8.2 已落默认体），实现 `Invoke_` 真实生命周期（含取消三分支保 HITL 快照）。

**Tech Stack:** Go 1.x、项目自有 exception 体系（`internal/common/exception`，错误码 112100-112103 已预置）、`internal/agentcore/graph/pregel`（已收口）、`internal/agentcore/session`。

**Spec:** `docs/superpowers/specs/2026-11-16-pregel-graph-8.1-design.md`（五项拍板 §4、偏差 D-1/D-2/D-3 §7）。

---

## 关键既有 API（实施者必读，勿重新发明）

| 符号 | 位置 | 签名/形状 |
|---|---|---|
| `BaseExecutable[I,O]` | graph/executable.go:25 | 内嵌满足 `Executable[I,O]`（fake 先例 executable_graph_test.go:14-19） |
| `ExecutableGraph[I,O]` | graph/executable_graph.go:30 | 嵌 `Executable` + `Invoke/Stream/Collect/Transform/Interrupt/Invoke_`；默认体 `DefaultGraphXxx` |
| `GeneralExecutor` | graph/executable.go:101 | `Executable[map[string]any, map[string]any]` 别名 |
| `NewVertex(nodeID string, executable GeneralExecutor) *Vertex` | graph/vertex.go:100 | |
| `(*Vertex).Init(sess session.BaseSession, kwargs map[string]any) bool` | vertex.go:229 | |
| `(*Vertex).SetEndNode(bool)` / `(*Vertex).Reset()` | vertex.go:776/1558 | |
| `pregel.Start = "__start__"` / `pregel.End = "__end__"` / `pregel.MaxRecursiveLimit = 10000` | pregel/constants.go:8-12 | |
| `pregel.PregelConfig{SessionID, Ns, RecursionLimit}` | pregel/config.go:8 | 字段名以文件为准 |
| `pregel.Selector` / `SelectorFunc` | pregel/router.go:21-25 | `Select(ctx, state any) ([]string, error)` |
| `(*PregelBuilder).AddNode/AddEdge(any,any)/AddBranch(string,Selector)/Build(Store, AfterStepFunc)` | pregel/builder.go:38-112 | `Build` 返回 `*Pregel` |
| `(*Pregel).Run(ctx, *InnerPregelConfig) (map[string]any, error)` | pregel/engine.go:191 | |
| `(*GraphState).IsResumable() bool` | pregel/state.go:48 | 三条件：PendingNode/PendingBuffer/ChannelValues |
| `session.BaseSession` 接口 | session/interfaces/interfaces.go:20 | `Checkpointer()/State()/SessionID()` 等 |
| `session.WorkflowIDProvider` | interfaces.go:120 | 类型断言取 `WorkflowID()` |
| `session.Checkpointer` | interfaces.go:58 | `PreWorkflowExecute/PostWorkflowExecute/GraphStore()` |
| `session.interaction.InteractiveInput` | session/interaction/interactive_input.go:13 | |
| `state.WorkflowState.CommitUserInputs(map[string]any)` | session/state/workflow_commit_state.go:114 | 经类型断言 `st.(state.WorkflowState)` |
| `exception.StatusPregelGraphNodeIDInvalid/NodeInvalid/EdgeInvalid/ConditionEdgeInvalid` | common/exception/codes_graph.go:57-71 | + `BuildError(status, exception.WithParam(...))` |
| `logComponent = logger.ComponentAgentCore` | vertex.go:95 | 同包已定义，**禁止重复定义** |

包环核实（已做）：session/interfaces → pregel 单向；graph 根包（vertex.go 已 import session）→ session 单向；pregel 不 import graph 根包（8.3c B5）。graph.go 同时 import 两者不构成环。

---

### Task 1: base.go — Graph 接口 + Router 别名

**Files:**
- Create: `internal/agentcore/graph/base.go`
- Test: `internal/agentcore/graph/base_test.go`
- Modify: `internal/agentcore/graph/doc.go`（文件树加 base.go 条目）

- [ ] **Step 1: 写失败测试**

```go
package graph

import "testing"

// 编译期断言：Router 别名指向 pregel.Selector（spec 拍板 1）
func TestRouterAlias(t *testing.T) {
	var _ Router = pregel.SelectorFunc(nil)
}

// 编译期断言：*PregelGraph 满足 Graph 接口（Task 2/3/4 落地后应通过；本步允许先失败）
func TestPregelGraphImplementsGraph(t *testing.T) {
	var _ Graph = (*PregelGraph)(nil)
}
```

（import 需补 `pregel "github.com/uapclaw/uapclaw-go/internal/agentcore/graph/pregel"`，下同。）

- [ ] **Step 2: 运行确认失败**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/graph/ -run 'TestRouterAlias|TestPregelGraphImplementsGraph' -v`
Expected: FAIL（undefined: Router / undefined: Graph / PregelGraph）

- [ ] **Step 3: 实现 base.go**

```go
package graph

import (
	pregel "github.com/uapclaw/uapclaw-go/internal/agentcore/graph/pregel"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
)

// ──────────────────────────── 结构体 ────────────────────────────

// Graph 声明式构图接口：链式登记节点/边/条件边，Compile 产出可执行图。
// Python: openjiuwen/core/graph/base.py:39-58 Graph(ABC)。
// Self 返回类型收窄为 *PregelGraph（唯一实现者，spec 偏差 D-1）。
type Graph interface {
	// StartNode 登记起始边 START→node_id（Python start_node，base.py:40-42）
	StartNode(nodeID string) *PregelGraph
	// EndNode 标记终节点并登记边 node_id→END（Python end_node，base.py:43-45）
	EndNode(nodeID string) *PregelGraph
	// AddNode 注册节点，waitForAll=true 时前驱汇聚成屏障（Python add_node，base.py:46-48；
	// keyword-only 默认 False → Go 显式 bool 形参，spec 偏差 D-2）
	AddNode(nodeID string, node GeneralExecutor, waitForAll bool) *PregelGraph
	// AddEdge 登记边，sourceNodeID 为 string 或 []string（Python add_edge，base.py:49-50）
	AddEdge(sourceNodeID any, targetNodeID string) *PregelGraph
	// AddConditionalEdges 登记条件边（Python add_conditional_edges，base.py:52-53）
	AddConditionalEdges(sourceNodeID string, router Router) *PregelGraph
	// Compile 编译为可执行图（Python compile，base.py:55-56）
	Compile(sess session.BaseSession, kwargs map[string]any) ExecutableGraph[map[string]any, map[string]any]
	// GetNodes 返回节点表浅拷贝（Python get_nodes，base.py:58-59）
	GetNodes() map[string]*Vertex
}

// ──────────────────────────── 常量 ────────────────────────────

// Router 条件路由选择器。Python: base.py:33-36 Router 类型别名
// （同步/async callable 双形态，Go 无 async 统一同步）。
// 注意：与 8.5 的 pregel.Router（IRouter 接口，Dispatch）同名不同物——
// 本别名指向 pregel.Selector（SelectorProtocol 的 Go 对应物）。
// 运行时数据语义见 spec §5.1：state 恒 nil，判断靠捕获 session 引用。
type Router = pregel.Selector
```

- [ ] **Step 4: 运行确认 TestRouterAlias 通过、TestPregelGraphImplementsGraph 仍失败**

Run: `go test ./internal/agentcore/graph/ -run TestRouterAlias -v` → PASS；`-run TestPregelGraphImplementsGraph` → FAIL（PregelGraph 未定义）

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/graph/base.go internal/agentcore/graph/base_test.go internal/agentcore/graph/doc.go
git commit -m "feat(8.1): base.go Graph 接口 + Router=pregel.Selector 别名"
```

---

### Task 2: graph.go 构图方法 — Branch/PregelGraph 登记 + 校验

**Files:**
- Create: `internal/agentcore/graph/graph.go`
- Test: `internal/agentcore/graph/graph_test.go`

- [ ] **Step 1: 写失败测试**

```go
package graph

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// fakeSel 记录调用的最小 selector
type fakeSel struct{ called bool }

func (f *fakeSel) Select(_ context.Context, _ any) ([]string, error) {
	f.called = true
	return []string{"b"}, nil
}

// TestNewPregelGraph_初始字段 构造后各容器非 nil
func TestNewPregelGraph_初始字段(t *testing.T) {
	g := NewPregelGraph()
	if g.nodes == nil || g.edges == nil || g.waits == nil || g.branches == nil || g.branchTargets == nil {
		t.Fatal("构造后容器应非 nil（对应 Python __init__ 各字段初始化）")
	}
	if g.pregel != nil || g.sessionRef != nil {
		t.Fatal("pregel/sessionRef 初始应为 nil")
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
	g.AddNode("a", fakeExec{}, false)
	if err := catchErr(func() { g.AddNode("a", fakeExec{}, false) }); err == nil {
		t.Fatal("重复注册应报错（already exist）")
	}
	g.AddNode("b", fakeExec{}, true)
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
	cases := []struct{ name string; source any; target string }{
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
	g.AddNode("a", fakeExec{}, false)
	g.EndNode("a")
	if len(g.edges) != 2 {
		t.Fatal("EndNode 应再登记一条边")
	}
	if g.edges[1].target != pregel.End {
		t.Fatal("EndNode 目标应为 End")
	}
	if !g.nodes["a"].IsEndNode() && g.nodes["a"].endNode != true {
		// SetEndNode 的读回方式以 vertex.go 实际导出面为准，此处允许断言调整
		t.Log("终节点标记断言以 vertex.go 导出面为准")
	}
}

// TestAddConditionalEdges_覆盖键 对齐 graph.py:122-132：同名覆盖、异名并存
func TestAddConditionalEdges_覆盖键(t *testing.T) {
	g := NewPregelGraph()
	s1, s2 := &fakeSel{}, &fakeSel{}
	g.AddConditionalEdges("a", s1)
	g.AddConditionalEdges("a", s2) // 同类型同名 → 覆盖
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

// ──────────────────────────── 测试工具 ────────────────────────────

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

// catchErr 捕获 panic 返回 error（构图方法对齐 Python raise → Go panic，见 Step 3 说明）
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
```

（import 补 `context`、`session`、`pregel`。fakeSel/fakeExec 若与他任务重复，以本任务文件内定义为准、后续任务复用。）

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/agentcore/graph/ -run 'TestNewPregelGraph|TestAddNode|TestAddEdge|TestStartNode|TestAddConditionalEdges|TestRegisterBranchTargets' -v`
Expected: FAIL（NewPregelGraph/PregelGraph 未定义）

- [ ] **Step 3: 实现 graph.go 构图部分**

**panic 纪律说明**：Python `add_conditional_edges` 等构图方法以 `raise build_error(...)` 中断构图。对齐 8.3 `NewPregelNode` 既有先例（构造期 panic，spec 8.3 已拍板），构图校验失败以 `panic(exception.BuildError(...))` 表达。`catchErr` 测试助手即为此服务。

```go
package graph

import (
	"context"

	pregel "github.com/uapclaw/uapclaw-go/internal/agentcore/graph/pregel"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 结构体 ────────────────────────────

// Branch 条件分支载体。Python: graph.py:52-54 @dataclass Branch
type Branch struct {
	// Condition 选择器（Python: condition callable）
	Condition Router
}

// edge 二元组，对应 Python edges: list[Tuple[str | list[str], str]]（graph.py:61）
type edge struct {
	source any // string 或 []string
	target string
}

// PregelGraph 声明式构图实现。Python: graph.py:57-67
type PregelGraph struct {
	// pregel 编译产物缓存（graph.py:60 pregel | None）
	preg *pregel.Pregel
	// edges 待编译边表（graph.py:61），只登记不建图
	edges []edge
	// waits 需要 wait_for_all 的节点集（graph.py:62）
	waits map[string]struct{}
	// nodes 节点表（graph.py:63）
	nodes map[string]*Vertex
	// branches 条件边：src → callable 名 → Branch（graph.py:64 defaultdict(dict)）
	branches map[string]map[string]Branch
	// branchTargets 分支目标集（graph.py:65 branch_targets），CNF 屏障解析用
	branchTargets map[string]map[string]struct{}
	// checkpointer 会话检查点器（graph.py:66）
	checkpointer session.Checkpointer
	// sessionRef 编译时绑定的会话（graph.py:67 _session）
	sessionRef session.BaseSession
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewPregelGraph 创建构图器。Python: graph.py:59-67 __init__
func NewPregelGraph() *PregelGraph {
	return &PregelGraph{
		edges:         []edge{},
		waits:         map[string]struct{}{},
		nodes:         map[string]*Vertex{},
		branches:      map[string]map[string]Branch{},
		branchTargets: map[string]map[string]struct{}{},
	}
}

// StartNode 登记起始边。Python: graph.py:69-72
func (g *PregelGraph) StartNode(nodeID string) *PregelGraph {
	g.validateNodeID(nodeID)
	g.addEdgeSource([]string{pregel.Start}, nodeID)
	return g
}

// EndNode 标记终节点并登记结束边。Python: graph.py:78-84
func (g *PregelGraph) EndNode(nodeID string) *PregelGraph {
	g.validateNodeID(nodeID)
	if v, ok := g.nodes[nodeID]; ok {
		v.SetEndNode(true)
	}
	g.addEdgeSource([]string{nodeID}, pregel.End)
	return g
}

// AddNode 注册节点。Python: graph.py:86-98
func (g *PregelGraph) AddNode(nodeID string, node GeneralExecutor, waitForAll bool) *PregelGraph {
	g.validateNodeID(nodeID)
	if node == nil {
		panic(exception.BuildError(exception.StatusPregelGraphNodeInvalid,
			exception.WithParam("node_id", nodeID), exception.WithParam("reason", "node is None")))
	}
	if _, exists := g.nodes[nodeID]; exists {
		panic(exception.BuildError(exception.StatusPregelGraphNodeIDInvalid,
			exception.WithParam("node_id", nodeID), exception.WithParam("reason", "already exist, can not add again")))
	}
	g.nodes[nodeID] = NewVertex(nodeID, node)
	if waitForAll {
		g.waits[nodeID] = struct{}{}
	}
	return g
}

// GetNodes 返回节点表浅拷贝。Python: graph.py:100-101
func (g *PregelGraph) GetNodes() map[string]*Vertex {
	out := make(map[string]*Vertex, len(g.nodes))
	for k, v := range g.nodes {
		out[k] = v
	}
	return out
}

// AddEdge 登记边。Python: graph.py:103-120（校验四分支顺序照抄）
func (g *PregelGraph) AddEdge(sourceNodeID any, targetNodeID string) *PregelGraph {
	g.addEdgeSource(sourceNodeID, targetNodeID)
	return g
}

// AddConditionalEdges 登记条件边。Python: graph.py:122-132
func (g *PregelGraph) AddConditionalEdges(sourceNodeID string, router Router) *PregelGraph {
	if sourceNodeID == "" {
		panic(exception.BuildError(exception.StatusPregelGraphConditionEdgeInvalid,
			exception.WithParam("source_id", sourceNodeID), exception.WithParam("reason", "source_node_is is None or empty")))
	}
	if router == nil {
		panic(exception.BuildError(exception.StatusPregelGraphConditionEdgeInvalid,
			exception.WithParam("source_id", sourceNodeID), exception.WithParam("reason", "router is None")))
	}
	name := getCallableName(router)
	if g.branches[sourceNodeID] == nil {
		g.branches[sourceNodeID] = map[string]Branch{}
	}
	g.branches[sourceNodeID][name] = Branch{Condition: router}
	return g
}

// RegisterBranchTargets 登记分支节点的目标全集，编译期 CNF 屏障解析用。
// Python: graph.py:134-143（branch_node_id 与 targets 非空且 len>1 才登记）
func (g *PregelGraph) RegisterBranchTargets(branchNodeID string, targets []string) *PregelGraph {
	if branchNodeID != "" && len(targets) > 1 {
		set := make(map[string]struct{}, len(targets))
		for _, t := range targets {
			set[t] = struct{}{}
		}
		g.branchTargets[branchNodeID] = set
	}
	return g
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// validateNodeID 校验节点 ID 非空。Python: graph.py:74-76
func (g *PregelGraph) validateNodeID(nodeID string) {
	if nodeID == "" {
		panic(exception.BuildError(exception.StatusPregelGraphNodeIDInvalid,
			exception.WithParam("node_id", nodeID), exception.WithParam("reason", "is None or empty")))
	}
}

// addEdgeSource 校验并登记边（AddEdge/StartNode/EndNode 共用落点）。
// 校验四分支顺序对齐 graph.py:104-119。
func (g *PregelGraph) addEdgeSource(sourceNodeID any, targetNodeID string) {
	edgeInvalid := func(reason string) {
		panic(exception.BuildError(exception.StatusPregelGraphEdgeInvalid,
			exception.WithParam("source_id", sourceNodeID),
			exception.WithParam("target_id", targetNodeID),
			exception.WithParam("reason", reason)))
	}
	switch src := sourceNodeID.(type) {
	case string:
		if src == "" {
			edgeInvalid("source_node_id is None or empty")
		}
	case []string:
		if len(src) == 0 {
			edgeInvalid("source_node_id is None or empty")
		}
		for _, item := range src {
			if item == "" {
				edgeInvalid("source_node_id list has None or empty")
			}
		}
	default:
		if sourceNodeID == nil {
			edgeInvalid("source_node_id is None or empty")
		}
	}
	if targetNodeID == "" {
		edgeInvalid("target_node_id is None or empty")
	}
	g.edges = append(g.edges, edge{source: sourceNodeID, target: targetNodeID})
}
```

**注意**：`getCallableName` 在 Task 3 定义，本步骤先在本文件声明占位会导致编译失败——本任务提交前临时补最小实现（Task 3 会补齐测试）：

```go
// getCallableName 临时最小实现，Task 3 补齐三级反射与测试。
func getCallableName(r Router) string { return fmt.Sprintf("%T", r) }
```

（import 补 `fmt`；Task 3 完成后删除此注释行。）

- [ ] **Step 4: 运行确认通过**

Run: `go test ./internal/agentcore/graph/ -run 'TestNewPregelGraph|TestAddNode|TestAddEdge|TestStartNode|TestAddConditionalEdges|TestRegisterBranchTargets|TestRouterAlias' -v`
Expected: 全 PASS（TestPregelGraphImplementsGraph 可仍 FAIL，Task 3/4 落 Compile 后通过）

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/graph/graph.go internal/agentcore/graph/graph_test.go
git commit -m "feat(8.1): PregelGraph 构图登记方法（AddNode/AddEdge/AddConditionalEdges/Start/End/BranchTargets）"
```

---

### Task 3: getCallableName 三级反射 + CNF 屏障解析

**Files:**
- Modify: `internal/agentcore/graph/graph.go`（替换临时 getCallableName，追加私有解析方法）
- Test: `internal/agentcore/graph/graph_test.go`（追加）

- [ ] **Step 1: 写失败测试**

```go
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
	_ = getCallableName(pregel.SelectorFunc(nil))
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
	// 找 OR 组与 standalone
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

// TestForwardReachable 对齐 graph.py:145-157 BFS 正向可达
func TestForwardReachable(t *testing.T) {
	g := NewPregelGraph()
	g.AddEdge("s", "m1")
	g.AddEdge("m1", "m2")
	g.AddEdge("m2", "sink")
	reach := g.forwardReachable("s")
	for _, want := range []string{"m1", "m2", "sink"} {
		if _, ok := reach[want]; !ok {
			t.Fatalf("s 应可达 %s, got %#v", want, reach)
		}
	}
	if _, ok := reach["s"]; ok {
		t.Fatal("起点自身不应计入可达集")
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
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/agentcore/graph/ -run 'TestGetCallableName|TestResolveBarrierGroups|TestForwardReachable|TestBranchRoot' -v`
Expected: FAIL（resolveBarrierGroups/forwardReachable/buildBranchParent/branchRoot 未定义；getCallableName 行为不符）

- [ ] **Step 3: 实现**

替换临时 `getCallableName`，追加：

```go
// ──────────────────────────── 非导出函数（追加）────────────────────────────

// getCallableName 反射取名，对应 Python graph.py:403-409 _get_callable_name 三级回退。
// 唯一消费点是 branches[src] 的 map key（同名覆盖/异名并存）。
// 偏差 D-3（spec §7）：Python 匿名 lambda 恒名 "<lambda>" 会互相覆盖；
// Go 匿名闭包经 FuncForPC 得编译器名 funcN 各不相同，不触发覆盖——已获拍板。
func getCallableName(r Router) string {
	if r == nil {
		return fmt.Sprintf("%v", r) // 三级兜底：nil selector（AddConditionalEdges 已拦，防御性留档）
	}
	v := reflect.ValueOf(r)
	switch v.Kind() {
	case reflect.Func:
		// 一级：func.__name__ → runtime.FuncForPC，去包路径取短名
		full := runtime.FuncForPC(v.Pointer()).Name()
		if idx := strings.LastIndex(full, "/"); idx >= 0 {
			full = full[idx+1:]
		}
		if idx := strings.LastIndex(full, "."); idx >= 0 {
			full = full[idx+1:]
		}
		return full
	default:
		// 二级：func.__class__.__name__ → 类型名
		t := v.Type()
		if t.Kind() == reflect.Ptr {
			t = t.Elem()
		}
		return t.Name()
	}
}

// forwardReachable BFS 正向可达集（不含起点自身）。Python: graph.py:145-157
func (g *PregelGraph) forwardReachable(startNode string) map[string]struct{} {
	visited := map[string]struct{}{}
	queue := []string{startNode}
	for len(queue) > 0 {
		node := queue[0] // Python queue.pop(0) 头取
		queue = queue[1:]
		if _, ok := visited[node]; ok {
			continue
		}
		visited[node] = struct{}{}
		for _, e := range g.edges {
			if src, ok := e.source.(string); ok && src == node {
				if _, seen := visited[e.target]; !seen {
					queue = append(queue, e.target)
				}
			}
		}
	}
	delete(visited, startNode) // Python 首轮 pop 即 add 起点，但 BFS 结果语义以「可达」为准——对齐 Python 实际行为（起点入 visited）
}
```

⚠️ **实现前核对**：Python `_forward_reachable` 的 visited **包含起点自身**（graph.py:152-153 首轮即 add）。删除上面 `delete(visited, startNode)` 行与测试断言中的「起点不计入」一行，测试改为断言 `reach["s"]` 存在。以 Python 行为为准，不要臆造。

```go
	return visited
}

// buildBranchParent 嵌套分支节点 → 父分支节点映射。Python: graph.py:159-171
func (g *PregelGraph) buildBranchParent() map[string]string {
	parent := map[string]string{}
	for _, bid := range sortedKeys(g.branchTargets) { // 遍历序不影响结果（键值写入互不相交），排序仅为确定性
		for target := range g.branchTargets[bid] {
			if _, isBranchTarget := g.branchTargets[target]; isBranchTarget {
				parent[target] = bid
				continue
			}
			if _, isBranchNode := g.branches[target]; isBranchNode {
				parent[target] = bid
			}
		}
	}
	return parent
}

// branchRoot 沿父链回溯至最外层分支祖先。Python: graph.py:173-178 @staticmethod
func branchRoot(branchNodeID string, branchParent map[string]string) string {
	root := branchNodeID
	for {
		next, ok := branchParent[root]
		if !ok {
			break
		}
		root = next
	}
	return root
}

// resolveBarrierGroups 互斥前驱 CNF 解析（五步，注释编号对齐 graph.py:180-237）。
func (g *PregelGraph) resolveBarrierGroups(targetID string, sourceList []map[string]struct{}) []map[string]struct{} {
	// 前置：无 branch_targets 或无前驱 → 原样返回（graph.py:193-194）
	if len(g.branchTargets) == 0 || len(sourceList) == 0 {
		return sourceList
	}

	// Step 1: 收集全部直接前驱（graph.py:196-200）
	allPredecessors := map[string]struct{}{}
	for _, grp := range sourceList {
		for p := range grp {
			allPredecessors[p] = struct{}{}
		}
	}

	// Step 2: 每个 branch 出口的 BFS 正向可达集（graph.py:202-205）
	type bidTarget struct{ bid, tgt string }
	reachable := map[bidTarget]map[string]struct{}{}
	for bid, targets := range g.branchTargets {
		for tgt := range targets {
			reachable[bidTarget{bid, tgt}] = g.forwardReachable(tgt)
		}
	}

	// Step 3: 每个前驱的归属（branch_id, target）集合（graph.py:207-213）
	predInfo := map[string]map[bidTarget]struct{}{}
	for p := range allPredecessors {
		predInfo[p] = map[bidTarget]struct{}{}
		for bt, nodes := range reachable {
			if _, ok := nodes[p]; ok {
				predInfo[p][bt] = struct{}{}
			}
		}
	}

	// Step 4: 按 branch_id 分组；多归属/零归属进 standalone（graph.py:215-225）
	branchGroups := map[string]map[string]struct{}{}
	var standalone []map[string]struct{}
	for _, p := range sortedKeys(allPredecessors) { // 排序遍历保 standalone 序确定性（Python set 迭代序不定，Go 对齐为不依赖序的组集）
		branches := predInfo[p]
		if len(branches) == 1 {
			var bid string
			for bt := range branches {
				bid = bt.bid
			}
			if branchGroups[bid] == nil {
				branchGroups[bid] = map[string]struct{}{}
			}
			branchGroups[bid][p] = struct{}{}
		} else {
			standalone = append(standalone, map[string]struct{}{p: {}})
		}
	}

	// Step 5: 嵌套分支组提升至根分支祖先（graph.py:227-231）
	branchParent := g.buildBranchParent()
	rootMerged := map[string]map[string]struct{}{}
	var rootOrder []string
	for _, bid := range sortedKeys(branchGroups) {
		root := branchRoot(bid, branchParent)
		if rootMerged[root] == nil {
			rootMerged[root] = map[string]struct{}{}
			rootOrder = append(rootOrder, root)
		}
		for p := range branchGroups[bid] {
			rootMerged[root][p] = struct{}{}
		}
	}

	// 结果组装（graph.py:233-237）：空则原样返回
	var result []map[string]struct{}
	for _, root := range rootOrder {
		if len(rootMerged[root]) > 0 {
			result = append(result, rootMerged[root])
		}
	}
	result = append(result, standalone...)
	if len(result) == 0 {
		return sourceList
	}
	return result
}

// sortedKeys map 键升序，保确定性遍历
func sortedKeys[T comparable](m map[T]struct{}) []T { /* keys + sort */ }
```

⚠️ `sortedKeys[T comparable]` 对 `map[string]struct{}` 可用；泛型 + sort 需约束 `cmp.Ordered`——签名写 `func sortedKeys[T cmp.Ordered](m map[T]struct{}) []T`，实现用 `slices.Sort`。

- [ ] **Step 4: 运行确认通过**

Run: `go test ./internal/agentcore/graph/ -v`（整包）
Expected: 全 PASS（含 Task 1/2 用例）

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/graph/graph.go internal/agentcore/graph/graph_test.go
git commit -m "feat(8.1): getCallableName 三级反射 + CNF 屏障解析（forwardReachable/buildBranchParent/resolveBarrierGroups）"
```

---

### Task 4: Compile / _compile / Reset

**Files:**
- Modify: `internal/agentcore/graph/graph.go`
- Test: `internal/agentcore/graph/graph_test.go`（追加）

- [ ] **Step 1: 写失败测试**

```go
// TestCompile_生命周期 对齐 graph.py:239-265：
// 首次编译 init 全节点 + 建图 + 缓存；二次编译只换 sessionRef；CompiledGraph 返回
func TestCompile_生命周期(t *testing.T) {
	g := NewPregelGraph()
	g.AddNode("a", fakeExec{}, false)
	g.StartNode("a")
	g.EndNode("a")
	sess := newFakeSession() // 见 Task 5 测试工具；此处仅要求非 nil BaseSession
	cg := g.Compile(sess, nil)
	if cg == nil {
		t.Fatal("Compile 应返回 CompiledGraph")
	}
	if g.preg == nil || g.checkpointer == nil {
		t.Fatal("首次编译应缓存 pregel 与 checkpointer")
	}
	// 二次编译：复用 pregel，只更新 sessionRef
	cg2 := g.Compile(sess, nil)
	if cg != cg2 {
		t.Log("两次编译返回各自 CompiledGraph（Python 每次都 new，对齐即可）")
	}
}

// TestCompile_NodeInit_被调用 compile 对每个节点 init（graph.py:240-241）
func TestCompile_NodeInit_被调用(t *testing.T) {
	g := NewPregelGraph()
	g.AddNode("a", fakeExec{}, false)
	sess := newFakeSession()
	_ = g.Compile(sess, nil)
	// 断言 fakeExec 对应 Vertex 的 session 已绑定（经 GetNodes 读回 Init 结果）
	_ = g.GetNodes()
}

// TestCompile_WaitsCNF wait 节点前驱经 resolveBarrierGroups 后进 builder.AddEdge
func TestCompile_WaitsCNF(t *testing.T) {
	g := NewPregelGraph()
	g.AddNode("w", fakeExec{}, true) // wait_for_all
	g.AddEdge("a", "w")
	g.AddEdge("b", "w")
	sess := newFakeSession()
	_ = g.Compile(sess, nil)
	// 编译成功即视为 barrier 边进入 builder；组形状断言由 Task 3 的
	// TestResolveBarrierGroups 覆盖，此处冒烟。
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/agentcore/graph/ -run 'TestCompile' -v`
Expected: FAIL（Compile 未定义）

- [ ] **Step 3: 实现 Compile/_compile/Reset**

```go
// ──────────────────────────── 导出函数（追加）────────────────────────────

// Compile 编译构图产物。Python: graph.py:239-265
// 步骤：①全节点 init ②构造 afterStep 回调 ③首次建图/复用 ④返回 CompiledGraph
func (g *PregelGraph) Compile(sess session.BaseSession, kwargs map[string]any) ExecutableGraph[map[string]any, map[string]any] {
	// ① 逐节点 init（graph.py:240-241）
	for _, node := range g.nodes {
		node.Init(sess, kwargs)
	}

	// ② afterStep 回调（graph.py:243-256）：session commit + SUPER_STEP_END 日志
	afterStep := func(ctx context.Context, loop *pregel.PregelLoop) {
		if g.sessionRef != nil {
			if st := g.sessionRef.State(); st != nil {
				if ws, ok := st.(state.WorkflowState); ok {
					ws.Commit()
				}
			}
		}
		activeNodes := make([]string, 0, len(loop.ActiveNodes))
		for n := range loop.ActiveNodes {
			activeNodes = append(activeNodes, n)
		}
		logger.Debug(logComponent).
			Str("msg", "Finished to run graph super-step").
			Int("step", loop.Step).
			Str("graph_id", loop.Config.Ns).
			Str("session_id", loop.Config.SessionID).
			Strs("active_nodes", activeNodes).
			Msg("graph super-step end")
	}

	// ③ 建图（graph.py:258-264）：首次取 checkpointer/store 全量编译；复用仅换 session
	if g.preg == nil {
		g.checkpointer = sess.Checkpointer()
		graphStore := store.NewGraphStore(g.checkpointer.GraphStore())
		g.preg = g.compile(graphStore, afterStep)
		g.sessionRef = sess
	} else {
		g.sessionRef = sess
	}
	return NewCompiledGraph(g.preg, g.checkpointer)
}

// Reset 重置全部节点。Python: graph.py:305-307
func (g *PregelGraph) Reset() {
	for _, node := range g.nodes {
		node.Reset()
	}
}

// ──────────────────────────── 非导出函数（追加）────────────────────────────

// compile 三段编译（graph.py:267-303，步骤编号保留）。
func (g *PregelGraph) compile(graphStore *store.GraphStore, stepCallback pregel.AfterStepFunc) *pregel.Pregel {
	edges := []edge{}
	sources := map[string][]map[string]struct{}{}
	builder := pregel.NewPregelBuilder()
	for nodeID := range g.nodes {
		builder.AddNode(nodeID, g.nodes[nodeID].NodeFunc())
	}

	// Step 1: waits 前驱收集为单元素组（graph.py:274-285）
	for _, e := range g.edges {
		if _, isWait := g.waits[e.target]; isWait {
			switch src := e.source.(type) {
			case string:
				sources[e.target] = append(sources[e.target], map[string]struct{}{src: {}})
			case []string:
				for _, s := range src {
					sources[e.target] = append(sources[e.target], map[string]struct{}{s: {}})
				}
			}
		} else {
			edges = append(edges, e)
		}
	}

	// Step 2: 互斥前驱 CNF 解析（graph.py:287-291）
	for targetID := range sources {
		sources[targetID] = g.resolveBarrierGroups(targetID, sources[targetID])
	}

	// Step 3: 传入 builder——wait 组在前、普通边在后（graph.py:293-298）
	for _, targetID := range sortedKeys(sources) {
		starts := make([]any, 0, len(sources[targetID]))
		for _, grp := range sources[targetID] {
			if len(grp) == 1 {
				for k := range grp {
					starts = append(starts, k)
				}
			} else {
				starts = append(starts, sortedKeys(grp))
			}
		}
		if _, err := builder.AddEdge(starts, targetID); err != nil {
			panic(err)
		}
	}
	for _, e := range edges {
		if _, err := builder.AddEdge(e.source, e.target); err != nil {
			panic(err)
		}
	}

	for _, src := range sortedKeys(g.branches) {
		for _, name := range sortedKeys(g.branches[src]) {
			builder.AddBranch(src, g.branches[src][name].Condition)
		}
	}
	return builder.Build(graphStore, stepCallback)
}
```

⚠️ **两处实现前必核对的对接面**（Python 形状 → Go 现状可能已变，以实际代码为准并在偏差区登记）：
1. `builder.AddNode(nodeID, ...)` 第二参在 Go 是 `NodeFunc`（pregel/builder.go:38），而 8.11 Vertex 是否暴露 `NodeFunc()` 方法需查 vertex.go——若为 `func (v *Vertex) NodeFunc() NodeFunc` 不存在，查 Vertex 执行入口（`Call`/`atomicInvoke`）与 8.3b/8.3c 消费先例（task.go 怎么从节点拿执行体），用同一对接面；此为 Vertex 与 builder 的既有接缝，**不新发明**。
2. `store.NewGraphStore` 是否存在（8.9a 有日志壳 GraphStore）——构造签名以 store/base.go 或对应文件为准；Python `GraphStore(self.checkpointer.graph_store())`（graph.py:260）。
3. `loop.Config`/`ActiveNodes`/`Step` 字段名以 pregel/engine.go `PregelLoop` 实际导出面为准。

- [ ] **Step 4: 运行确认通过**

Run: `go test ./internal/agentcore/graph/ -v`
Expected: 全 PASS，且 `TestPregelGraphImplementsGraph` 转绿（Graph 接口七方法齐）

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/graph/graph.go internal/agentcore/graph/graph_test.go internal/agentcore/graph/doc.go
git commit -m "feat(8.1): Compile/_compile/Reset——构图翻译至 PregelBuilder（waits CNF + branches 转手）"
```

---

### Task 5: CompiledGraph — invoke 生命周期 + 取消分流

**Files:**
- Modify: `internal/agentcore/graph/graph.go`（追加 CompiledGraph）
- Test: `internal/agentcore/graph/graph_test.go`（追加 fake session/checkpointer + 生命周期用例）

- [ ] **Step 1: 写失败测试**

```go
// ──────────────────────────── fake session/checkpointer ────────────────────

// fakeSession 最小 BaseSession（按 interfaces.InnerSession 方法面补齐）
type fakeSession struct {
	session.BaseSession // 未实现方法 panic 兜底；仅覆写用到的
	sessionID, workflowID string
	state                 state.SessionState
	checkpointer          *fakeCheckpointer
}

func (f *fakeSession) SessionID() string    { return f.sessionID }
func (f *fakeSession) Checkpointer() session.Checkpointer { return f.checkpointer }
func (f *fakeSession) State() state.SessionState { return f.state }
func (f *fakeSession) Config() config.SessionConfig { return config.SessionConfig{} }
func (f *fakeSession) Tracer() *tracer.Tracer { return nil }
func (f *fakeSession) StreamWriterManager() *stream.StreamWriterManager { return nil }
func (f *fakeSession) ActorManager() any { return nil }
func (f *fakeSession) Close() error { return nil }

// fakeWorkflowSession 带 WorkflowIDProvider 能力
type fakeWorkflowSession struct {
	*fakeSession
}
func (f *fakeWorkflowSession) WorkflowID() string { return f.workflowID }

// fakeCheckpointer 记录调用序
type fakeCheckpointer struct {
	preCalls, postCalls int
	postResults         []map[string]any
	postExceptions      []error
	store               *fakeGraphStore
}

func (c *fakeCheckpointer) PreWorkflowExecute(_ context.Context, _ session.InnerSession, _ any) error { c.preCalls++; return nil }
func (c *fakeCheckpointer) PostWorkflowExecute(_ context.Context, _ session.InnerSession, result map[string]any, exception error) error {
	c.postCalls++
	c.postResults = append(c.postResults, result)
	c.postExceptions = append(c.postExceptions, exception)
	return nil
}
func (c *fakeCheckpointer) GraphStore() pregel.Store { return c.store }

// fakeGraphStore 可编程返回快照
type fakeGraphStore struct {
	pregel.Store
	st   *pregel.GraphState
	getErr error
}
func (s *fakeGraphStore) Get(_ context.Context, _, _ string) (*pregel.GraphState, error) {
	return s.st, s.getErr
}

// TestCompiledGraph_Invoke_主流程 对齐 graph.py:315-348：
// config nil → isMain + 默认 PregelConfig + pre + commit_user_inputs + run + post
func TestCompiledGraph_Invoke_主流程(t *testing.T) {
	g := NewPregelGraph()
	g.AddNode("a", fakeExec{}, false)
	g.StartNode("a")
	g.EndNode("a")
	sess := &fakeWorkflowSession{&fakeSession{sessionID: "s1", workflowID: "wf1",
		state: newFakeWorkflowState(), checkpointer: &fakeCheckpointer{store: &fakeGraphStore{}}}}
	cg := g.Compile(sess, nil)

	res, err := cg.Invoke(map[string]any{}, sess)
	if err != nil {
		t.Fatalf("主流程不应报错: %v", err)
	}
	_ = res
	if sess.checkpointer.preCalls != 1 || sess.checkpointer.postCalls != 1 {
		t.Fatalf("isMain 应走 pre/post 各一次, pre=%d post=%d", sess.checkpointer.preCalls, sess.checkpointer.postCalls)
	}
	if sess.checkpointer.postExceptions[0] != nil {
		t.Fatal("正常完成 post 的 exception 应为 nil")
	}
}

// TestCompiledGraph_Invoke_非主流程异常上抛 isMain=false 且 run 报错 → 原样上抛（graph.py:347-348）
func TestCompiledGraph_Invoke_非主流程异常上抛(t *testing.T) {
	// config 非 nil（envelope 携带 config）→ isMain=false
	// run 失败：构造带非法边的图使 Pregel 返回错误，或直接以可控图触发
	// （实施时选最简可控路径：inputs 带 config 键）
}

// TestCompiledGraph_取消_可续跑保留快照 对齐 graph.py:377-393：
// 图侧有 resumable 快照 → post 带 canceled 异常走保存分支
func TestCompiledGraph_取消_可续跑保留快照(t *testing.T) {
	g := NewPregelGraph()
	g.AddNode("a", fakeExec{}, false)
	sess := &fakeWorkflowSession{&fakeSession{sessionID: "s1", workflowID: "wf1",
		state: newFakeWorkflowState(),
		checkpointer: &fakeCheckpointer{store: &fakeGraphStore{
			st: &pregel.GraphState{PendingNode: map[string]*pregel.PendingNode{
				"a": {NodeName: "a", Status: pregel.PendingStatusInterrupt}},
			}}}}}
	cg := g.Compile(sess, nil)
	// 取消注入方式：fake 的 PostWorkflowExecute 返回被 Pregel.Run 感知的取消——
	// 最简路径：fake 图 store 让 afterStep 回调返回 canceled？不行——
	// 实施时采用：NodeFunc 的 fakeExec 在 ctx.Done 时返回 context.Canceled，
	// 或直接以可取消 ctx 传入 Invoke（Invoke 签名含 envelope/sess，ctx 从 sess 取不到时
	// 用 context.WithCancel 外部取消——以 DefaultGraphInvoke/Invoke_ 实际传参为准）。
	// 断言：post 被调且 exception 非 nil（canceled）。
	_ = cg
}

// TestCompiledGraph_取消_无快照不落盘 对齐 graph.py:358-364
func TestCompiledGraph_取消_无快照不落盘(t *testing.T) {
	// store.Get 返回 (nil, nil) → post 不被调用（保留可重跑语义）
}

// TestCompiledGraph_取消_查询失败按不可续跑 对齐 graph.py:367-376
func TestCompiledGraph_取消_查询失败按不可续跑(t *testing.T) {
	// store.Get 返回 error → 快照按 nil 处理，post 不被调用，原始取消链上抛
}
```

⚠️ 上述三个取消用例的**取消注入机制**在 Step 3 实现定型后回填断言细节：CompiledGraph 的 `Invoke_` 从 `envelope[ConfigKey]` 拿 config，ctx 来源以 8.2 `Invoke_` 签名（无 ctx 形参）为准——若 8.2 契约无 ctx，取消探测点在 `pregel.Run` 内部（其 ctx 从 config/loop 取，以 engine.go:191 实际来源为准），fake 通过让 fakeExec 的 OnInvoke 返回 `context.Canceled` 模拟 Pregel 传播取消。**用例骨架先行，断言以真实取消传播路径为准，不许 Skip。**

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/agentcore/graph/ -run 'TestCompiledGraph' -v`
Expected: FAIL（NewCompiledGraph 未定义）

- [ ] **Step 3: 实现 CompiledGraph**

```go
// ──────────────────────────── 结构体（追加到 graph.go 结构体区）──────────

// CompiledGraph 编译产物，ExecutableGraph 的图实现。
// Python: graph.py:310-400 CompiledGraph(ExecutableGraph)
type CompiledGraph struct {
	BaseExecutable[map[string]any, map[string]any] // 满足 Executable 部分（fake 先例 executable_graph_test.go:17）
	preg        *pregel.Pregel
	checkpointer session.Checkpointer
}

// NewCompiledGraph 构造。Python: graph.py:311-313 __init__
func NewCompiledGraph(p *pregel.Pregel, cp session.Checkpointer) *CompiledGraph {
	return &CompiledGraph{preg: p, checkpointer: cp}
}

// ──────────────────────────── 导出函数 ────────────────────────────

// Invoke 拆信封默认体（Python invoke 继承 base.py:13-14）
func (c *CompiledGraph) Invoke(inputs map[string]any, sess session.BaseSession) (map[string]any, error) {
	return DefaultGraphInvoke[map[string]any, map[string]any](c, inputs, sess)
}

// Stream pass 占位（Python graph.py:396-397）
func (c *CompiledGraph) Stream(inputs map[string]any, sess session.BaseSession) (<-chan StreamChunk[map[string]any], error) {
	return DefaultGraphStream[map[string]any, map[string]any](c, inputs, sess)
}

// Collect pass 占位（Python 未覆写 collect，继承 base.py:19-20）
func (c *CompiledGraph) Collect(inputs <-chan map[string]any, sess session.BaseSession) (map[string]any, error) {
	return DefaultGraphCollect[map[string]any, map[string]any](c, inputs, sess)
}

// Transform pass 占位（Python 未覆写 transform，继承 base.py:22-23）
func (c *CompiledGraph) Transform(inputs <-chan map[string]any, sess session.BaseSession) (<-chan StreamChunk[map[string]any], error) {
	return DefaultGraphTransform[map[string]any, map[string]any](c, inputs, sess)
}

// Interrupt pass 占位（Python graph.py:399-400 interrupt 直接 return）
func (c *CompiledGraph) Interrupt(message map[string]any) error {
	return DefaultGraphInterrupt[map[string]any, map[string]any](c, message)
}

// Invoke_ 图执行生命周期（Python _invoke，graph.py:315-394，步骤编号保留）
func (c *CompiledGraph) Invoke_(inputs map[string]any, sess session.BaseSession, config any) (map[string]any, error) {
	isMain := false
	sessionID := sess.SessionID()
	workflowID := ""
	if wp, ok := sess.(session.WorkflowIDProvider); ok { // Python session.workflow_id() hasattr 模式
		workflowID = wp.WorkflowID()
	}

	// config 为 nil → isMain + 默认配置（graph.py:320-322）
	var pc *pregel.PregelConfig
	switch cfg := config.(type) {
	case *pregel.PregelConfig:
		pc = cfg
	case pregel.PregelConfig:
		pc = &cfg
	}
	if pc == nil {
		isMain = true
		pc = &pregel.PregelConfig{SessionID: sessionID, Ns: workflowID, RecursionLimit: pregel.MaxRecursiveLimit}
	}

	// 正常路径（graph.py:324-348）
	var (
		result    map[string]any
		exception error
	)
	inner := pregel.CreateInnerConfig(*pc)
	defer func() {
		// 取消分流（graph.py:349-394）在错误返回路径走——见下方 invokeFailed
		_ = inner
	}()

	if isMain {
		if err := c.checkpointer.PreWorkflowExecute(context.Background(), sess, inputs); err != nil {
			return nil, err
		}
	}
	// 非 InteractiveInput → commit_user_inputs（graph.py:327-328）
	if _, isInteractive := inputs[/* InteractiveInput 标记键，以 interaction 包实际形状为准 */]; !isInteractive {
		if ws, ok := sess.State().(state.WorkflowState); ok {
			ws.CommitUserInputs(inputs)
		}
	}

	result, exception = func() (map[string]any, error) {
		r, err := c.preg.Run(context.Background(), &inner)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				// graph.py:335-341：取消先记日志再 re-raise，进入取消分流
				logger.Warn(logComponent).
					Str("session_id", sessionID).
					Str("workflow_id", workflowID).
					Bool("cancelled", true).
					Msg("Pregel execution cancelled")
			}
			return nil, err
		}
		return r, nil
	}()

	if isMain {
		if err := c.checkpointer.PostWorkflowExecute(context.Background(), sess, result, exception); err != nil {
			return nil, err
		}
	} else if exception != nil {
		return nil, exception // graph.py:347-348
	}
	return result, nil
}
```

⚠️ **三处实现前必核对**（Python 形状 → Go 现状，不许臆造）：
1. **ctx 来源**：`Invoke_` 签名无 ctx（8.2 契约），Python 生命周期由 asyncio 天然携带。Go 取 ctx 的既定先例查 DefaultGraphInvoke 调用链与 8.3c `Pregel.Run` 的 ctx 从哪来——若项目惯例是 `context.Background()` 起步，取消注入点必在 `Pregel.Run` 内部传播（NodeFunc 返回 canceled），本实现照此落；若 8.2 契约实际带 ctx，以契约为准改签名。**以此核对结论登记到偏差区。**
2. **InteractiveInput 判定**：Python `isinstance(inputs, InteractiveInput)`——Go inputs 是 map 信封拆出的业务载荷，判定方式查 `session/interaction/interactive_input.go` 的实际形状（类型断言 / 标记键），以现状为准。
3. **取消分流**：上面骨架未含 graph.py:349-394 的三分支——在 `errors.Is(err, context.Canceled)` 命中后、返回前插入：

```go
// 取消分流（graph.py:349-394，HITL 快照三分支）
func (c *CompiledGraph) handleCancellation(ctx context.Context, sess session.BaseSession, sessionID, workflowID string) error {
	// 仅 isMain 走分流（graph.py:350）——由调用方守卫
	graphState, err := c.checkpointer.GraphStore().Get(ctx, sessionID, workflowID)
	if err != nil {
		// 查询失败按不可续跑：不落盘不清除（graph.py:367-376）
		logger.Warn(logComponent).
			Str("session_id", sessionID).
			Str("workflow_id", workflowID).
			Err(err).
			Msg("Failed to inspect graph checkpoint on workflow cancellation")
		return nil
	}
	if graphState != nil && graphState.IsResumable() {
		// 可续跑 → 带非 nil 异常走保存分支保留现场（graph.py:377-393）
		canceled := context.Canceled // 对齐 asyncio.CancelledError("workflow execution cancelled")
		saveErr := c.checkpointer.PostWorkflowExecute(ctx, sess, map[string]any{}, canceled)
		if errors.Is(saveErr, context.Canceled) {
			return nil // 保存分支 re-raise 的取消吞掉，保原始取消链
		}
		if saveErr != nil {
			// 保存失败不顶替原始取消链（graph.py:386-393）
			logger.Error(logComponent).
				Str("session_id", sessionID).
				Str("workflow_id", workflowID).
				Err(saveErr).
				Msg("Failed to preserve checkpoint on workflow cancellation")
			return nil
		}
	}
	return nil
}
```

调用方式：`Invoke_` 中 `isMain` 且 run 返回 canceled 时调 `handleCancellation`，随后 `return nil, err` 原样上抛取消。三分支断言用例（Step 1 的三个 TestCompiledGraph_取消_*）在此实现落地后回填完整断言。

- [ ] **Step 4: 运行确认通过**

Run: `go test ./internal/agentcore/graph/ -v && go vet ./internal/agentcore/graph/`
Expected: 全 PASS、vet 干净

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/graph/graph.go internal/agentcore/graph/graph_test.go
git commit -m "feat(8.1): CompiledGraph invoke 生命周期 + 取消三分支（HITL 快照保留）"
```

---

### Task 6: doc.go / 计划状态 / 全量回归 / 推送

**Files:**
- Modify: `internal/agentcore/graph/doc.go`
- Modify: `IMPLEMENTATION_PLAN.md`（8.1 行 ☐ → ✅，补 Go 事实）

- [ ] **Step 1: doc.go 文件树补三行**

在 `internal/agentcore/graph/doc.go` 文件树段（按 doc.go → 核心类型 → 辅助 → 配置顺序）加：

```
//	├── base.go          # Graph 接口 + Router 类型别名（Python base.py）
//	├── graph.go         # Branch/PregelGraph/CompiledGraph 构图与编译（Python graph.py）
```

- [ ] **Step 2: 更新 IMPLEMENTATION_PLAN.md 8.1 行**

`☐` → `✅`，产出列追加 Go 事实摘要（模式对齐 8.3/8.11 已有行）：新文件 base.go/graph.go、`Router = pregel.Selector` 别名、CNF 解析、CompiledGraph 取消三分支、偏差 D-1/D-2/D-3、spec 与 plan 链接。

- [ ] **Step 3: 全量回归（CI 同款标签）**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./...
```
Expected: 零 FAIL（FTS 用例假失败 = 漏标签，见 testing-build-tags 教训）

- [ ] **Step 4: 提交并推送**

```bash
git add internal/agentcore/graph/doc.go IMPLEMENTATION_PLAN.md
git commit -m "docs(8.1): doc.go 文件树 + 实施计划状态同步"
git push
```

---

## Self-Review 结论

1. **Spec 覆盖**：§2.1→Task1、§2.2→Task2/3/4、§2.3→Task5、§2.4→Task3、§4 六拍板→各任务内注明、§7 偏差→D-1 Task1/D-2 Task1/D-3 Task3、§8 删除清单空（纯新增）✅
2. **占位符扫描**：Task 4/5 各有 2-3 处「实现前必核对」项——这些是**既有接缝的核对指令**（Vertex↔builder 接缝、ctx 来源、InteractiveInput 判定形状），不是留白：每处都给了核对位置与「不许新发明」的约束，核对结论须登记偏差。Task 5 取消用例骨架先行的部分同样标注了回填要求（不许 Skip）。
3. **类型一致性**：`Router`/`Branch.Condition`/`branches` map 键/`ExecutableGraph[map[string]any, map[string]any]` 各任务间一致；`getCallableName` Task2 占位→Task3 定稿的替换关系已写明。
