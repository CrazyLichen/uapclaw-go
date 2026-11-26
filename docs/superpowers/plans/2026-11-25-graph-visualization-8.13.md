# 8.13 Graph Visualization 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 一比一对齐 Python `openjiuwen/core/graph/visualization/`（419 行），落地 Go 可视化件并回收 D-d/D-19-1/D-20-2 三处回填缝。

**Architecture:** 新包 `internal/openuapclaw/core/graph/visualization/`（纯数据 + Drawable 构建器 + mermaid 文本拼接 + mermaid.ink HTTP 渲染）；环破除按 D-13-5——visualization 只依赖 common/exception，flow/loop/llm 反向 import visualization 实现四个能力接口，workflow import visualization 构造 Drawable 与 Draw()。

**Tech Stack:** Go stdlib（net/http、encoding/base64、regexp）；测试用 httptest + testify。

**Spec:** docs/superpowers/specs/2026-11-25-graph-visualization-8.13-design.md（拍板 ①-⑤，偏差 D-13-1..5）

---

## 文件结构总览

| 文件 | 动作 | 职责 |
|---|---|---|
| `internal/openuapclaw/core/graph/visualization/drawable_node.go` | 新建 | DrawableNode 数据结构 |
| `internal/openuapclaw/core/graph/visualization/drawable_edge.go` | 新建 | DrawableEdge + DrawableBranchRouter |
| `internal/openuapclaw/core/graph/visualization/drawable_graph.go` | 新建 | DrawableGraph |
| `internal/openuapclaw/core/graph/visualization/drawable_subgraph_node.go` | 新建 | DrawableSubgraphNode |
| `internal/openuapclaw/core/graph/visualization/drawable.go` | 新建 | Drawable + mermaidNode/mermaidLink 拼接 + mermaid.ink 渲染 |
| `internal/openuapclaw/core/graph/visualization/doc.go` | 新建 | 包文档 |
| `internal/common/exception/codes_graph.go` | 修改 | 补 112043 |
| `internal/openuapclaw/core/workflow/components/flow/branch_router.go` | 修改 | D-19-1 三处回填 |
| `internal/openuapclaw/core/workflow/components/flow/branch_comp.go` | 修改 | DrawableRouter 接口实现 |
| `internal/openuapclaw/core/workflow/components/llm/intent_detection_comp.go` | 修改 | DrawableRouter 接口实现 |
| `internal/openuapclaw/core/workflow/components/flow/loop/loop_comp.go` | 修改 | D-20-2 回填 + LoopDrawableSubgraph 实现 |
| `internal/openuapclaw/core/workflow/components/flow/sub_workflow_comp.go` | 修改 | SubWorkflowDrawableSubgraph 实现 |
| `internal/openuapclaw/core/workflow/base_workflow.go` | 修改 | D-d 回填（drawable 具体类型化 + 钩子五段 + Drawable() 访问器） |
| `internal/openuapclaw/core/workflow/workflow.go` | 修改 | Draw() 真实现 + Internal() 访问器 + D-14-7 回收 |

每个 Go 源文件配套 `_test.go` 同步新建/追加。所有 commit 一律 **path-limited**（`git commit <paths> -m ...`），commit 前 `git status --porcelain` 核对无他流 staged 改动。

---

### Task 1: 错误码 112043

**Files:**
- Modify: `internal/common/exception/codes_graph.go`
- Test: `internal/common/exception/codes_graph_test.go`（追加）

- [ ] **Step 1: 追加失败测试**

在 `codes_graph_test.go` 追加：

```go
// TestStatusDrawableGraphToMermaidInvalid 错误码值对齐 Python codes.py:409
func TestStatusDrawableGraphToMermaidInvalid(t *testing.T) {
	assert.Equal(t, 112043, int(exception.StatusDrawableGraphToMermaidInvalid.Code()))
}
```

（按既有 codes_graph_test.go 的断言风格对齐；若既有测试断言的是 Name()/Message()，照同款补全三断言。）

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/common/exception/ -run TestStatusDrawableGraphToMermaidInvalid`
Expected: 编译失败 `StatusDrawableGraphToMermaidInvalid undefined`

- [ ] **Step 3: 实现**

`codes_graph.go` 在 112022 之后追加（Python codes.py:409 一比一）：

```go
// StatusDrawableGraphToMermaidInvalid drawable_graph to_mermaid 参数无效
StatusDrawableGraphToMermaidInvalid = NewStatusCode(
	"DRAWABLE_GRAPH_TO_MERMAID_INVALID", 112043,
	"drawable_graph to_mermaid error, reason={reason}")
```

- [ ] **Step 4: 运行测试通过**

Run: `go test ./internal/common/exception/ -run TestStatusDrawableGraph`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git status --porcelain   # 确认无他流 staged
git commit internal/common/exception/codes_graph.go internal/common/exception/codes_graph_test.go -m "feat(8.13): 补错误码 112043 DRAWABLE_GRAPH_TO_MERMAID_INVALID（Python codes.py:409 对齐）"
```

---

### Task 2: 四个数据结构文件（drawable_node/edge/graph/subgraph_node + doc.go）

**Files:**
- Create: `internal/openuapclaw/core/graph/visualization/drawable_node.go`
- Create: `internal/openuapclaw/core/graph/visualization/drawable_edge.go`
- Create: `internal/openuapclaw/core/graph/visualization/drawable_graph.go`
- Create: `internal/openuapclaw/core/graph/visualization/drawable_subgraph_node.go`
- Create: `internal/openuapclaw/core/graph/visualization/doc.go`
- Test: `internal/openuapclaw/core/graph/visualization/drawable_edge_test.go`

- [ ] **Step 1: 写失败测试（drawable_edge_test.go，断言结构体零值形状与 D-13-2 string 槽）**

```go
package visualization

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestDrawableEdge零值 对齐 drawable_edge.py dataclass 默认值
func TestDrawableEdge零值(t *testing.T) {
	e := &DrawableEdge{Source: "a", Target: "b"}
	assert.Equal(t, "a", e.Source)
	assert.Equal(t, "b", e.Target)
	assert.Equal(t, "", e.Data) // Python Optional[Stringifiable]=None → Go 空串（D-13-2）
	assert.False(t, e.Conditional)
	assert.False(t, e.Streaming)
}

// TestDrawableBranchRouter 分支路由 drawable 形状（drawable_edge.py:23-26）
func TestDrawableBranchRouter(t *testing.T) {
	br := &DrawableBranchRouter{Targets: []string{"x", "y"}, Datas: []string{"d1", "d2"}}
	assert.Equal(t, []string{"x", "y"}, br.Targets)
	assert.Equal(t, []string{"d1", "d2"}, br.Datas)
}

// TestDrawableGraph构造 对齐 drawable_graph.py dataclass
func TestDrawableGraph构造(t *testing.T) {
	g := &DrawableGraph{Nodes: map[string]*DrawableNode{}, Edges: []*DrawableEdge{}}
	assert.Empty(t, g.Nodes)
	assert.Empty(t, g.Edges)
	assert.Nil(t, g.BreakNodes) // Python Optional → Go nil
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/openuapclaw/core/graph/visualization/`
Expected: 编译失败（类型未定义）

- [ ] **Step 3: 实现四个文件**

`drawable_node.go`（≙ drawable_node.py）：

```go
// Package visualization 提供工作流图的可视化镜像与 Mermaid 导出。
//
// Python 真相源：openjiuwen/core/graph/visualization/（六文件一比一映射见 doc.go）。
// 环破除偏差 D-13-5：本包只依赖 common/exception；flow/loop/llm 反向 import
// 本包实现能力接口（LoopSubgraphSource/SubgraphSource/BranchRouterSource/
// DrawableBranchTargetsSource），workflow import 本包构造 Drawable 与 Draw()。
package visualization

// DrawableNode 可绘制节点（Python drawable_node.py:8-11 一比一；
// dataclass → Go 结构体直译，偏差 D-13-2：metadata dict → map[string]any）。
type DrawableNode struct {
	// ID 节点标识（Python id）
	ID string
	// Name 名称（Python name，Optional → 空串）
	Name string
	// Metadata 元数据（Python metadata，Optional → nil）
	Metadata map[string]any
}
```

`drawable_edge.go`（≙ drawable_edge.py）：

```go
package visualization

// DrawableEdge 可绘制边（Python drawable_edge.py:15-20 一比一；
// 偏差 D-13-2：data Stringifiable 协议 → string，调用方实传均为字符串标签）。
type DrawableEdge struct {
	// Source 源节点（Python source）
	Source string
	// Target 目标节点（Python target）
	Target string
	// Data 条件边标签（Python data，Optional → 空串，D-13-2）
	Data string
	// Conditional 是否条件边（Python conditional）
	Conditional bool
	// Streaming 是否流式边（Python streaming）
	Streaming bool
}

// DrawableBranchRouter 分支路由 drawable 投影（Python drawable_edge.py:24-26 一比一）
type DrawableBranchRouter struct {
	// Targets 分支目标列表（Python targets）
	Targets []string
	// Datas 分支标签列表（Python datas）
	Datas []string
}
```

`drawable_graph.go`（≙ drawable_graph.py）：

```go
package visualization

// DrawableGraph 可绘制图（Python drawable_graph.py:11-16 一比一；
// break_nodes Optional → nil 切片承载）。
type DrawableGraph struct {
	// Nodes 节点表（Python nodes dict）
	Nodes map[string]*DrawableNode
	// Edges 边列表（Python edges）
	Edges []*DrawableEdge
	// StartNodes 起始节点（Python start_nodes）
	StartNodes []*DrawableNode
	// EndNodes 结束节点（Python end_nodes）
	EndNodes []*DrawableNode
	// BreakNodes 中断节点（Python break_nodes，Optional → nil）
	BreakNodes []*DrawableNode
}
```

`drawable_subgraph_node.go`（≙ drawable_subgraph_node.py，继承 → Go 嵌入）：

```go
package visualization

// DrawableSubgraphNode 子图节点（Python drawable_subgraph_node.py:12-13 一比一；
// 继承 DrawableNode → Go 嵌入）。
type DrawableSubgraphNode struct {
	DrawableNode
	// Subgraph 子图（Python subgraph，Optional → nil）
	Subgraph *DrawableGraph
}
```

`doc.go` 按规则 1 写包文档与文件树（上面 package 注释并入 doc.go，普通文件只留 package 行）。

- [ ] **Step 4: 测试通过 + go vet**

Run: `go test ./internal/openuapclaw/core/graph/visualization/ && go vet ./internal/openuapclaw/core/graph/visualization/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git commit internal/openuapclaw/core/graph/visualization -m "feat(8.13): 四数据结构文件落地（drawable_node/edge/graph/subgraph_node 一比一，D-13-2 data string 槽）"
```

---

### Task 3: Drawable 构建器（drawable.go 上半：add_node/set_*/add_edge/get_graph）

**Files:**
- Create: `internal/openuapclaw/core/graph/visualization/drawable.go`
- Test: `internal/openuapclaw/core/graph/visualization/drawable_test.go`

- [ ] **Step 1: 定义能力接口与 Drawable 主体（含 D-13-1 getTargets 函数壳）**

`drawable.go` 追加（Task 2 建立的包内）：

```go
// ──────────────────────────── 结构体 ────────────────────────────

// LoopSubgraphSource 循环体子图源能力（偏差 D-13-5，≙ Python
// isinstance(LoopComponent/AdvancedLoopComponent) 分支；实现方 flow/loop）
type LoopSubgraphSource interface {
	// LoopDrawableSubgraph 返回循环体子图（Python loop_group/body drawable.get_graph()）
	LoopDrawableSubgraph() *DrawableGraph
}

// SubgraphSource 子工作流子图源能力（≙ Python isinstance(SubWorkflowComponent) 分支；
// 实现方 flow.SubWorkflowComponent）
type SubgraphSource interface {
	// SubWorkflowDrawableSubgraph 返回子工作流子图
	SubWorkflowDrawableSubgraph() *DrawableGraph
}

// BranchRouterSource 分支路由源能力（≙ Python isinstance(BranchComponent/
// IntentDetectionComponent) 分支；实现方 flow/llm，返回 component.router()）
type BranchRouterSource interface {
	// DrawableRouter 返回分支路由器（*flow.BranchRouter）
	DrawableRouter() any
}

// DrawableBranchTargetsSource 分支目标投影能力（≙ Python BranchRouter 的
// get_drawable_branch_router；实现方 flow.BranchRouter，D-19-1 回填件）
type DrawableBranchTargetsSource interface {
	// GetDrawableBranchTargets 返回分支目标与标签（branch_router.py:71）
	GetDrawableBranchTargets() (targets []string, datas []string)
}

// Drawable 可视化构建器（Python drawable.py:46-140 一比一）
type Drawable struct {
	// graph 图镜像（Python self._graph）
	graph *DrawableGraph
	// loopNodes 循环节点集合（Python self._loop_nodes）
	loopNodes map[string]struct{}
}

// NewDrawable 构造（Python __init__: DrawableGraph(nodes={},edges=[],start_nodes=[],
// end_nodes=[],break_nodes=[])）
func NewDrawable() *Drawable {
	return &Drawable{
		graph: &DrawableGraph{
			Nodes:      map[string]*DrawableNode{},
			Edges:      []*DrawableEdge{},
			StartNodes: []*DrawableNode{},
			EndNodes:   []*DrawableNode{},
		},
		loopNodes: map[string]struct{}{},
	}
}
```

（声明顺序遵守规则 2：结构体/接口区 → 常量区 → 导出函数区 → 非导出函数区；后续 Step 的方法按区归位。）

- [ ] **Step 2: 写 add_node/set_*/add_edge 的失败测试**

`drawable_test.go`：

```go
package visualization

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeLoopSource 循环子图源测试替身（接口实现在边界，D-13-5 接口面即真实缝）
type fakeLoopSource struct{ sub *DrawableGraph }

func (f *fakeLoopSource) LoopDrawableSubgraph() *DrawableGraph { return f.sub }

// fakeBranchSource 分支路由源测试替身
type fakeBranchSource struct{}

func (f *fakeBranchSource) DrawableRouter() any { return nil }

// TestDrawableAddNode_普通节点 add_node 默认分支（drawable.py:80-81）
func TestDrawableAddNode_普通节点(t *testing.T) {
	d := NewDrawable()
	d.addNode("n1", &fakeBranchSource{}) // 无任何能力接口 → 普通节点
	assert.Contains(t, d.graph.Nodes, "n1")
	assert.Equal(t, "n1", d.graph.Nodes["n1"].ID)
	assert.NotContains(t, d.loopNodes, "n1")
}

// TestDrawableAddNode_循环子图 循环分支：子图 + end 自动发现 + 自环边（drawable.py:58-73）
func TestDrawableAddNode_循环子图(t *testing.T) {
	sub := &DrawableGraph{
		Nodes: map[string]*DrawableNode{
			"a": {ID: "a"}, "b": {ID: "b"},
		},
		Edges: []*DrawableEdge{{Source: "a", Target: "b"}},
	}
	d := NewDrawable()
	d.addNode("loop1", &fakeLoopSource{sub: sub})
	sn, ok := d.graph.Nodes["loop1"].(*DrawableSubgraphNode)
	require.True(t, ok)
	assert.Same(t, sub, sn.Subgraph)
	assert.Contains(t, d.loopNodes, "loop1")
	// end_nodes 自动发现：出度=0 的 b
	require.Len(t, sub.EndNodes, 1)
	assert.Equal(t, "b", sub.EndNodes[0].ID)
	// 自环边
	require.Len(t, d.graph.Edges, 1)
	assert.Equal(t, "loop1", d.graph.Edges[0].Source)
	assert.Equal(t, "loop1", d.graph.Edges[0].Target)
	assert.True(t, d.graph.Edges[0].Conditional)
}

// TestDrawableAddNode_循环子图_end已设 时不重复发现（drawable.py:62 判断）
func TestDrawableAddNode_循环子图_end已设(t *testing.T) {
	sub := &DrawableGraph{
		Nodes:    map[string]*DrawableNode{"a": {ID: "a"}, "b": {ID: "b"}},
		Edges:    []*DrawableEdge{{Source: "a", Target: "b"}},
		EndNodes: []*DrawableNode{{ID: "b"}},
	}
	d := NewDrawable()
	d.addNode("loop1", &fakeLoopSource{sub: sub})
	assert.Len(t, sub.EndNodes, 1)
}

// TestDrawableSetStartNode_不存在报错（drawable.py:85-88，112020）
func TestDrawableSetStartNode_不存在报错(t *testing.T) {
	d := NewDrawable()
	err := d.SetStartNode("ghost")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not exist in the graph")
}

// TestDrawableSetBreakNode 不存在报错（drawable.py:100-102，112022）
func TestDrawableSetBreakNode_不存在报错(t *testing.T) {
	d := NewDrawable()
	assert.Error(t, d.SetBreakNode("ghost"))
}

// TestDrawableSetStartEnd 存在时登记（drawable.py:88/95）
func TestDrawableSetStartEnd(t *testing.T) {
	d := NewDrawable()
	d.addNode("s1", &fakeBranchSource{})
	d.addNode("e1", &fakeBranchSource{})
	require.NoError(t, d.SetStartNode("s1"))
	require.NoError(t, d.SetEndNode("e1"))
	assert.Equal(t, "s1", d.graph.StartNodes[0].ID)
	assert.Equal(t, "e1", d.graph.EndNodes[0].ID)
}

// TestDrawableAddEdge_静态边（drawable.py:110-113）
func TestDrawableAddEdge_静态边(t *testing.T) {
	d := NewDrawable()
	d.addEdge("a", "b", false, false, nil)
	require.Len(t, d.graph.Edges, 1)
	assert.False(t, d.graph.Edges[0].Conditional)
}

// TestDrawableAddEdge_流式边（drawable.py:104-112 streaming 透传）
func TestDrawableAddEdge_流式边(t *testing.T) {
	d := NewDrawable()
	d.addEdge("a", "b", false, true, nil)
	assert.True(t, d.graph.Edges[0].Streaming)
}

// TestDrawableAddEdge_循环源强制条件边（drawable.py:107-109）
func TestDrawableAddEdge_循环源强制条件边(t *testing.T) {
	d := NewDrawable()
	d.loopNodes["loop1"] = struct{}{}
	d.addEdge("loop1", "x", false, false, nil)
	require.Len(t, d.graph.Edges, 1)
	assert.True(t, d.graph.Edges[0].Conditional)
}

// TestDrawableAddEdge_条件边BranchRouter展开（drawable.py:115-124）
func TestDrawableAddEdge_条件边BranchRouter展开(t *testing.T) {
	d := NewDrawable()
	router := &fakeTargetsSource{targets: []string{"x", "y"}, datas: []string{"d1", "d2"}}
	d.addEdge("a", "", true, false, router)
	require.Len(t, d.graph.Edges, 2)
	assert.Equal(t, "x", d.graph.Edges[0].Target)
	assert.Equal(t, "d1", d.graph.Edges[0].Data)
	assert.Equal(t, "y", d.graph.Edges[1].Target)
	assert.Equal(t, "d2", d.graph.Edges[1].Data)
	assert.True(t, d.graph.Edges[0].Conditional)
}

// TestDrawableAddEdge_条件边datas缺省 datas 少于 targets 时不越界（drawable.py:123 三元）
func TestDrawableAddEdge_条件边datas缺省(t *testing.T) {
	d := NewDrawable()
	router := &fakeTargetsSource{targets: []string{"x"}, datas: nil}
	d.addEdge("a", "", true, false, router)
	assert.Equal(t, "", d.graph.Edges[0].Data)
}

// fakeTargetsSource 分支目标投影替身
type fakeTargetsSource struct{ targets, datas []string }

func (f *fakeTargetsSource) GetDrawableBranchTargets() ([]string, []string) {
	return f.targets, f.datas
}

// TestGetTargets_不可达形态 函数壳行为（D-13-1：非 callable → nil）
func TestGetTargets_不可达形态(t *testing.T) {
	assert.Nil(t, getTargets(42))
	assert.Nil(t, getTargets("str"))
}
```

- [ ] **Step 3: 运行确认失败**

Run: `go test ./internal/openuapclaw/core/graph/visualization/ -run TestDrawable`
Expected: 编译失败（addNode/SetStartNode 等未定义）

- [ ] **Step 4: 实现方法（drawable.py:51-140 步骤一比一）**

```go
// ──────────────────────────── 导出函数 ────────────────────────────

// SetStartNode 登记起始节点（Python drawable.py:83-88 set_start_node；
// 112020 复刻 reason 文案）
func (d *Drawable) SetStartNode(nodeID string) error {
	node, ok := d.graph.Nodes[nodeID]
	if !ok {
		return exception.BuildError(exception.StatusDrawableGraphStartNodeInvalid,
			exception.WithParam("node_id", nodeID),
			exception.WithParam("reason", "node '"+nodeID+"' does not exist in the graph"))
	}
	d.graph.StartNodes = append(d.graph.StartNodes, node)
	return nil
}

// SetEndNode 登记结束节点（Python drawable.py:90-95，112021）
func (d *Drawable) SetEndNode(nodeID string) error {
	node, ok := d.graph.Nodes[nodeID]
	if !ok {
		return exception.BuildError(exception.StatusDrawableGraphEndNodeInvalid,
			exception.WithParam("node_id", nodeID),
			exception.WithParam("reason", "node '"+nodeID+"' does not exist in the graph"))
	}
	d.graph.EndNodes = append(d.graph.EndNodes, node)
	return nil
}

// SetBreakNode 登记中断节点（Python drawable.py:97-102，112022）
func (d *Drawable) SetBreakNode(nodeID string) error {
	node, ok := d.graph.Nodes[nodeID]
	if !ok {
		return exception.BuildError(exception.StatusDrawableGraphBreakNodeInvalid,
			exception.WithParam("node_id", nodeID),
			exception.WithParam("reason", "node '"+nodeID+"' does not exist in the graph"))
	}
	d.graph.BreakNodes = append(d.graph.BreakNodes, node)
	return nil
}

// AddEdge 加边（Python drawable.py:104-124 add_edge 一比一；
// D-13-5：isinstance(BranchRouter) → DrawableBranchTargetsSource 断言）
func (d *Drawable) AddEdge(source string, opts ...EdgeOption) {
	o := newEdgeOpt(opts...)
	d.addEdge(source, o.target, o.conditional, o.streaming, o.data)
}

// GetGraph 返回图镜像（Python drawable.py:138-140 get_graph）
func (d *Drawable) GetGraph() *DrawableGraph { return d.graph }

// ──────────────────────────── 非导出函数 ────────────────────────────

// addNode 组件登记（Python drawable.py:51-81 add_node；分派序=Python isinstance 序）
func (d *Drawable) addNode(nodeID string, component any) {
	if src, ok := component.(LoopSubgraphSource); ok {
		subgraph := src.LoopDrawableSubgraph()
		// drawable.py:61-70：end 未设时出度=0 自动发现
		if len(subgraph.EndNodes) == 0 {
			outDegrees := map[string]int{}
			for id := range subgraph.Nodes {
				outDegrees[id] = 0
			}
			for _, edge := range subgraph.Edges {
				if _, ok := outDegrees[edge.Source]; !ok {
					outDegrees[edge.Source] = 0
				}
				outDegrees[edge.Source]++
			}
			for id, deg := range outDegrees {
				if deg == 0 {
					subgraph.EndNodes = append(subgraph.EndNodes, subgraph.Nodes[id])
				}
			}
		}
		d.graph.Nodes[nodeID] = &DrawableSubgraphNode{DrawableNode: DrawableNode{ID: nodeID}, Subgraph: subgraph}
		d.loopNodes[nodeID] = struct{}{}
		d.addEdge(nodeID, nodeID, false, false, nil)
		return
	}
	if src, ok := component.(SubgraphSource); ok {
		d.graph.Nodes[nodeID] = &DrawableSubgraphNode{DrawableNode: DrawableNode{ID: nodeID},
			Subgraph: src.SubWorkflowDrawableSubgraph()}
		return
	}
	if src, ok := component.(BranchRouterSource); ok {
		d.graph.Nodes[nodeID] = &DrawableNode{ID: nodeID}
		d.addEdge(nodeID, "", true, false, src.DrawableRouter())
		return
	}
	d.graph.Nodes[nodeID] = &DrawableNode{ID: nodeID}
}

// addEdge 加边内部实现（Python drawable.py:104-124 步骤一比一）
func (d *Drawable) addEdge(source, target string, conditional, streaming bool, data any) {
	if _, ok := d.loopNodes[source]; ok {
		d.graph.Edges = append(d.graph.Edges, &DrawableEdge{Source: source, Target: target, Conditional: true})
		return
	}
	if !conditional {
		d.graph.Edges = append(d.graph.Edges, &DrawableEdge{Source: source, Target: target,
			Conditional: conditional, Streaming: streaming})
		return
	}
	var targets []string
	var datas []string
	if src, ok := data.(DrawableBranchTargetsSource); ok {
		targets, datas = src.GetDrawableBranchTargets()
	} else {
		targets = getTargets(data)
	}
	for i, tgt := range targets {
		dataStr := ""
		if datas != nil {
			dataStr = datas[i]
		}
		d.graph.Edges = append(d.graph.Edges, &DrawableEdge{Source: source, Target: tgt,
			Data: dataStr, Conditional: conditional, Streaming: streaming})
	}
}

// getTargets 返回类型注解反射的目标提取（Python drawable.py:29-43 _get_targets；
// 偏差 D-13-1：Go 无 Literal 类型注解反射且调用形态不可达，函数壳保留——
// 非 callable 一律 nil）
func getTargets(data any) []string {
	return nil
}
```

注意：`AddEdge(source, opts...)` 的变参选项形态是为对齐 Python 关键字实参（`add_edge(source=..., conditional=True, data=...)`）——若嫌重可直接用非导出 addEdge 五参版供 workflow/base_workflow.go 同包外调用困难时的替代：**拍板：导出五参版 `AddEdge(source, target string, conditional, streaming bool, data any)` 替代选项形态**（更贴 Python 形参序），上面 AddEdge 实现为：

```go
// AddEdge 加边（Python drawable.py:104-124 add_edge 形参序一比一）
func (d *Drawable) AddEdge(source, target string, conditional, streaming bool, data any) {
	d.addEdge(source, target, conditional, streaming, data)
}
```

（`EdgeOption`/`newEdgeOpt` 不落地，测试里的 `d.addNode`/`d.addEdge` 非导出方法同包测试直调。）

- [ ] **Step 5: 测试通过**

Run: `go test ./internal/openuapclaw/core/graph/visualization/ -run TestDrawable -v`
Expected: 全 PASS

- [ ] **Step 6: Commit**

```bash
git commit internal/openuapclaw/core/graph/visualization -m "feat(8.13): Drawable 构建器落地（drawable.py:29-140 一比一；D-13-5 四能力接口 + D-13-1 getTargets 函数壳）"
```

---

### Task 4: _MermaidDiagram 文本拼接（drawable.go 下半）

**Files:**
- Modify: `internal/openuapclaw/core/graph/visualization/drawable.go`
- Test: `internal/openuapclaw/core/graph/visualization/drawable_mermaid_test.go`

Python 输出格式事实（mermaid-py 0.9.0 源码逐字对齐）：
- `Graph._build_script`：`"---\ntitle: {title}\n---"`（config 恒 None 不拼）
- `FlowChart._build_script`：`"\nflowchart TB"`（orientation 恒默认 TB）+ 每个 node `"\n\t{node}"` + 每个 link `"\n\t{link}"` + 结尾 `"\n"`
- `Node.__str__` 单节点：`id_ + shape.start + '"{content}"' + shape.end`；normal=`[ ]`、round-edge=`( )`；id_ 经 `text_to_snake_case`（`re.sub(r"[^a-zA-Z0-9_\.-]","_",t).lower()`）
- `Node.__str__` 子图：`"subgraph {id_} [\"{content}\"]"` + `"\n\tdirection TB"`（drawable 传 TOP_TO_BOTTOM）+ 每个子节点 `"\n" + "\t" + str(node)`（**只缩进首行**，嵌套子图后续行不加深）+ `"\nend"`
- `Link.__str__`：`origin.id_ + " " + head_left + shape + head_right + '|"{message}"|' + " " + end.id_`；head_left none=`""`、head_right arrow=`">"`；shape normal=`"--"`、dotted=`"-.-"`、thick=`"=="`；无 message 时不拼 `|""|`
- `_ExtendLink.__str__` 在 Link 基础上：`id_` 非空时 link 串中 `head_left` 前插 `{id_}@`；properties 非空时结尾拼 `"\n" + {id_}@ + "{k: v, ...}"`（多属性逗号+空格分隔，dict 插入序）

- [ ] **Step 1: 写黄金串失败测试**

`drawable_mermaid_test.go`：

```go
package visualization

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestToMermaid_基础图 简单两节点静态边（格式逐字对齐 mermaid-py）
func TestToMermaid_基础图(t *testing.T) {
	d := NewDrawable()
	d.addNode("start", &fakeBranchSource{})
	d.addNode("end", &fakeBranchSource{})
	require.NoError(t, d.SetStartNode("start"))
	require.NoError(t, d.SetEndNode("end"))
	d.addEdge("start", "end", false, false, nil)
	s, err := d.ToMermaid("", false, false)
	require.NoError(t, err)
	assert.Equal(t, "---\ntitle: \n---\nflowchart TB\n\tnode_1[\"start\"]\n\tnode_2[\"end\"]\n\tnode_1 --> node_2\n", s)
}

// TestToMermaid_标题 title 进入 frontmatter
func TestToMermaid_标题(t *testing.T) {
	d := NewDrawable()
	d.addNode("a", &fakeBranchSource{})
	s, err := d.ToMermaid("T1", false, false)
	require.NoError(t, err)
	assert.Contains(t, s, "---\ntitle: T1\n---")
}

// TestToMermaid_条件边 dotted + 标签
func TestToMermaid_条件边(t *testing.T) {
	d := NewDrawable()
	d.addNode("a", &fakeBranchSource{})
	d.addNode("x", &fakeBranchSource{})
	d.addEdge("a", "x", true, false, &fakeTargetsSource{targets: []string{"x"}, datas: []string{"hit"}})
	s, err := d.ToMermaid("", false, false)
	require.NoError(t, err)
	assert.Contains(t, s, "node_2 -.->|\"hit\"| node_1") // 注意：a 是 node_1，x 是 node_2
}

// TestToMermaid_流式动画 thick + id@properties
func TestToMermaid_流式动画(t *testing.T) {
	d := NewDrawable()
	d.addNode("a", &fakeBranchSource{})
	d.addNode("b", &fakeBranchSource{})
	d.addEdge("a", "b", false, true, nil)
	s, err := d.ToMermaid("", false, true)
	require.NoError(t, err)
	assert.Contains(t, s, "node_1 link_1@==> node_2\nlink_1@{animate: true}")
}

// TestToMermaid_子图展开 bool 层级1 子图嵌套结构
func TestToMermaid_子图展开(t *testing.T) {
	sub := &DrawableGraph{
		Nodes:     map[string]*DrawableNode{"s": {ID: "s"}, "e": {ID: "e"}},
		Edges:     []*DrawableEdge{{Source: "s", Target: "e"}},
		StartNodes: []*DrawableNode{{ID: "s"}},
		EndNodes:  []*DrawableNode{{ID: "e"}},
	}
	d := NewDrawable()
	d.addNode("loop1", &fakeLoopSource{sub: sub})
	s, err := d.ToMermaid("", true, false)
	require.NoError(t, err)
	assert.Contains(t, s, "subgraph node_1 [\"loop1\"]")
	assert.Contains(t, s, "\tdirection TB")
	assert.Contains(t, s, "end")
}

// TestToMermaid_子图不展开 expand=false 时子图节点画普通框
func TestToMermaid_子图不展开(t *testing.T) {
	sub := &DrawableGraph{Nodes: map[string]*DrawableNode{"s": {ID: "s"}}}
	d := NewDrawable()
	d.addNode("loop1", &fakeLoopSource{sub: sub})
	s, err := d.ToMermaid("", false, false)
	require.NoError(t, err)
	assert.NotContains(t, s, "subgraph ")
}

// TestToMermaid_int层级 expand=1 展开一层、内层子图不再展开
func TestToMermaid_int层级(t *testing.T) {
	inner := &DrawableGraph{Nodes: map[string]*DrawableNode{"i": {ID: "i"}}}
	outer := &DrawableGraph{
		Nodes: map[string]*DrawableNode{"mid": {ID: "mid"}},
	}
	// mid 是外子图里的子图节点
	outer.Nodes["mid"] = &DrawableSubgraphNode{DrawableNode: DrawableNode{ID: "mid"}, Subgraph: inner}
	d := NewDrawable()
	d.addNode("loop1", &fakeLoopSource{sub: outer})
	s1, err := d.ToMermaid("", 1, false)
	require.NoError(t, err)
	assert.Contains(t, s1, "subgraph node_1 [\"loop1\"]")   // 第一层展开
	assert.NotContains(t, s1, "subgraph node_")             // 第二层不再展开（只出现 node_1 一个 subgraph 头）
}

// TestToMermaid_参数校验三分支（drawable.py:237-244，112043）
func TestToMermaid_参数校验(t *testing.T) {
	d := NewDrawable()
	_, err := d.ToMermaid("", 1.5, false) // expand 非 bool/int
	assert.Error(t, err)
	_, err = d.ToMermaid("", -1, false)
	assert.Error(t, err)
	_, err = d.ToMermaid("", true, "yes") // animation 非 bool
	assert.Error(t, err)
}

// TestToMermaid_int零值 expand=0 不展开（drawable.py:252 <=0 短路）
func TestToMermaid_int零值(t *testing.T) {
	sub := &DrawableGraph{Nodes: map[string]*DrawableNode{"s": {ID: "s"}}}
	d := NewDrawable()
	d.addNode("loop1", &fakeLoopSource{sub: sub})
	s, err := d.ToMermaid("", 0, false)
	require.NoError(t, err)
	assert.NotContains(t, s, "subgraph ")
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/openuapclaw/core/graph/visualization/ -run TestToMermaid`
Expected: 编译失败（ToMermaid 未定义）

- [ ] **Step 3: 实现 mermaid 拼接（drawable.py:143-356 一比一）**

drawable.go 追加：

```go
// ──────────────────────────── mermaid 节点/连线（_MermaidDiagram 内部件） ────────────────────────────

// snakeCase Node id_ 归一（mermaid-py utils.py:34-50 text_to_snake_case 一比一）
var snakeCaseRe = regexp.MustCompile(`[^a-zA-Z0-9_\.-]`)

func snakeCase(text string) string {
	return strings.ToLower(snakeCaseRe.ReplaceAllString(text, "_"))
}

// nodeShape 形状对（mermaid-py node.py NODE_SHAPES 消费面：normal/round-edge）
// + 子图头（Node.__str__ subgraph 分支）。Python MermaidNode 兼容子图两种形态，
// 以 isSubgraph 区分（D-13 复刻面=drawable.py 消费面：href/styles 零使用不落）。
type mermaidNode struct {
	id        string // 已 snakeCase
	content   string
	normal    bool // true=普通框 [ ]，false=round-edge ( )
	subNodes  []*mermaidNode
	isSub     bool
	direction string // 子图方向，恒 "TB"（drawable.py:310 TOP_TO_BOTTOM）
}

// String 单节点形态（Node.__str__ else 分支：id_+start+"content"+end）
func (m *mermaidNode) stringNormal() string {
	start, end := "[", "]"
	if !m.normal {
		start, end = "(", ")"
	}
	return m.id + start + "\"" + m.content + "\"" + end
}

// String 子图形态（Node.__str__ subgraph 分支逐字复刻，含只缩进首行语义）
func (m *mermaidNode) stringSubgraph() string {
	lines := []string{
		"subgraph " + m.id + " [\"" + m.content + "\"]",
		"\tdirection " + m.direction,
	}
	for _, sn := range m.subNodes {
		lines = append(lines, "\t"+sn.String())
	}
	return strings.Join(lines, "\n") + "\nend"
}

// String 总入口（Node.__str__）
func (m *mermaidNode) String() string {
	if m.isSub {
		return m.stringSubgraph()
	}
	return m.stringNormal()
}

// mermaidLink 连线（Link + _ExtendLink 合体；headLeft 恒 ""、headRight 恒 ">"
// —— drawable.py 全部消费走默认值，复刻面即消费面）
type mermaidLink struct {
	origin *mermaidNode
	end    *mermaidNode
	shape  string // "--" / "-.-" / "=="
	message string
	idTag  string // animate 时 "link_N"，空=无（_ExtendLink id_）
	properties []string // animate 时 ["animate: true"]
}

// String _ExtendLink.__str__ 一比一（drawable.py:210-228）
func (l *mermaidLink) String() string {
	tag := ""
	if l.idTag != "" {
		tag = l.idTag + "@"
	}
	propertiesStr := ""
	if len(l.properties) > 0 {
		propertiesStr = "\n" + tag + "{" + strings.Join(l.properties, ", ") + "}"
	}
	msg := ""
	if l.message != "" {
		msg = "|\"" + l.message + "\"|"
	}
	return l.origin.id + " " + tag + l.shape + ">" + msg + " " + l.end.id + propertiesStr
}
```

`Drawable` 方法区追加：

```go
// nodeLinkIDGenerator 自增 ID（_NodeIdGenerator/_LinkIdGenerator 合体，
// 前缀区分；drawable.py:164-182）
type idGenerator struct {
	prefix string
	n      int
}

func (g *idGenerator) next() string {
	g.n++
	return g.prefix + "_" + strconv.Itoa(g.n)
}

// ToMermaid 转 Mermaid 文本（Python drawable.py:234-263 to_mermaid 一比一；
// expandSubgraph any 承载 bool|int，D-13-3）
func (d *Drawable) ToMermaid(title string, expandSubgraph any, enableAnimation bool) (string, error) {
	// drawable.py:237-244 参数校验三分支（112043）
	if _, ok := expandSubgraph.(bool); !ok {
		if n, ok := expandSubgraph.(int); !ok || n < 0 {
			return "", exception.BuildError(exception.StatusDrawableGraphToMermaidInvalid,
				exception.WithParam("reason", "'expand_subgraph' type is not bool"))
		}
	}
	if _, ok := enableAnimation.(bool); !ok {
		return "", exception.BuildError(exception.StatusDrawableGraphToMermaidInvalid,
			exception.WithParam("reason", "'enable_animation' type is not bool"))
	}
	exp, _ := expandSubgraph.(bool)
	if b, ok := expandSubgraph.(int); ok {
		_ = b
	}

	nodeIDs := &idGenerator{prefix: "node"}
	linkIDs := &idGenerator{prefix: "link"}
	subMermaidNodes := map[string]*mermaidNode{}
	mermaidNodes := map[string]*mermaidNode{}
	for id, node := range d.graph.Nodes {
		if sg, isSG := node.(*DrawableSubgraphNode); isSG && truthyExpand(expandSubgraph) {
			// drawable.py:249-253：层数递减（bool 恒传 / int<=0 恒传）
			subMermaidNodes[id] = genMermaidNode(nextExpand(expandSubgraph), sg, enableAnimation, nodeIDs, linkIDs)
		} else {
			normal := !d.containsNode(append(d.graph.StartNodes, d.graph.EndNodes...), node)
			mermaidNodes[id] = &mermaidNode{id: snakeCase(nodeIDs.next()), content: node.ID, normal: normal}
		}
	}
	links := d.genMermaidLinks(mermaidNodes, subMermaidNodes, enableAnimation, linkIDs)
	// FlowChart._build_script 逐字复刻
	var sb strings.Builder
	sb.WriteString("---\ntitle: " + title + "\n---")
	sb.WriteString("\nflowchart TB")
	for _, n := range orderedNodes(mermaidNodes, subMermaidNodes) {
		sb.WriteString("\n\t" + n.String())
	}
	for _, l := range links {
		sb.WriteString("\n\t" + l.String())
	}
	sb.WriteString("\n")
	return sb.String(), nil
}
```

配套非导出函数：

```go
// truthyExpand Python truth(expand_subgraph) 等价：bool 取值，int>0 真
func truthyExpand(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case int:
		return x > 0
	}
	return false
}

// nextExpand drawable.py:251-253 层数递减：bool 恒传 / int<=0 恒传 / int 递减 1
func nextExpand(v any) any {
	if n, ok := v.(int); ok {
		if n > 0 {
			return n - 1
		}
		return n
	}
	return v
}

// containsNode 节点身份判断（Python `node in list` dataclass eq → Go 指针等同值：
// start/end 登记的即 Nodes 表同指针，用 ID 兜底覆盖测试注入值场景）
func (d *Drawable) containsNode(list []*DrawableNode, n *DrawableNode) bool {
	for _, x := range list {
		if x == n || x.ID == n.ID {
			return true
		}
	}
	return false
}

// orderedNodes 节点输出序（Python dict 插入序：普通节点先、子图节点后，
// drawable.py:261 nodes 列表序；Go map 无序 → 维护插入序切片承载。
// Drawable.graph 需同步加非导出 nodeOrder []string 记录 addNode 顺序——
// 在 Task 3 的 addNode 中 append）
func orderedNodes(mermaidNodes map[string]*mermaidNode, subMermaidNodes map[string]*mermaidNode) []*mermaidNode {
	out := []*mermaidNode{}
	for _, id := range d.nodeOrder {
		if n, ok := mermaidNodes[id]; ok {
			out = append(out, n)
		} else if n, ok := subMermaidNodes[id]; ok {
			out = append(out, n.node) // 子图头节点
		}
	}
	return out
}
```

⚠️ 实现注意（实施者必读）：
1. `subMermaidNodes` 值需同时携带**子图头 mermaidNode**（`Node(id_=gen, content=node.id, sub_nodes=..., direction=TB)`）与内部 links/start/end/break 节点引用（`_SubGraphNode` dataclass drawable.py:184-190）——Go 以 `subgraphMermaidNode` 结构体承载：
```go
type subgraphMermaidNode struct {
	node            *mermaidNode        // 子图头（drawable.py:309-310）
	links           []*mermaidLink      // 子图内部 links（drawable.py:311）
	startNodes      []*mermaidNode      // drawable.py:304
	endNodes        []*mermaidNode      // drawable.py:305
	breakNodes      []*mermaidNode      // drawable.py:306
}
```
`orderedNodes` 中 `n.node` 即取该结构体头。
2. `genMermaidNode`（drawable.py:276-315 一比一）：递归构造子图头 + `get_node_by_id`（drawable.py:296-302）三分支——未命中复刻为返回 `&mermaidNode{id: "invalid node id"}`（Python 返回裸字符串属潜在 AttributeError 死分支，Go 以此承载，plan 登记为实现注记不另立偏差编号）。
3. `genMermaidLinks`（drawable.py:317-356 一比一）：四象限展开（source/target 命中 mermaidNodes/subMermaidNodes 四组合）+ loop 子图 links 追加（drawable.py:354-355）；shape 选择：conditional→`"-.-"`、streaming→`"=="`（+animation 时 idTag/properties）、默认`"--"`；message=conditional&&data 时取 Data。
4. `ToMermaidPNG/ToMermaidSVG` 在 Task 5 实现，本任务先以 `// Task 5 落地` 占位注释标注（不占覆盖率豁免——两方法随 Task 5 同 commit 进包）。

- [ ] **Step 4: 测试通过**

Run: `go test ./internal/openuapclaw/core/graph/visualization/ -run TestToMermaid -v`
Expected: 全 PASS（黄金串逐字断言）

- [ ] **Step 5: 覆盖率检查**

Run: `go test -cover ./internal/openuapclaw/core/graph/visualization/`
Expected: ≥85%（PNG/SVG 未实现会拉低——若 <85% 属预期中间态，Task 5 后复核）

- [ ] **Step 6: Commit**

```bash
git commit internal/openuapclaw/core/graph/visualization -m "feat(8.13): _MermaidDiagram 文本拼接落地（drawable.py:143-356 一比一，mermaid-py 输出格式逐字对齐）"
```

---

### Task 5: png/svg 渲染（mermaid.ink HTTP）

**Files:**
- Modify: `internal/openuapclaw/core/graph/visualization/drawable.go`
- Test: `internal/openuapclaw/core/graph/visualization/drawable_render_test.go`

Python 事实（mermaid-py 0.9.0 `__main__.py:165-225`）：
- 编码：`base64.urlsafe_b64encode(script.encode("utf-8")).decode("ascii")`
- img URL：`{server}/img/{b64}?format=png`（width/height/scale 恒 None 不拼，仅 format）；svg URL：`{server}/svg/{b64}?`（**query 为空仍带尾部 `?`**）
- server：`os.getenv("MERMAID_INK_SERVER", "https://mermaid.ink")`
- 响应非 2xx 报错含 status 与响应体（MermaidError 文案复刻要点：`HTTP {code} Error` + Details 前 200 字符）
- `to_mermaid_svg` 调 `to_mermaid` 时 **enable_animation 恒 True**（drawable.py:274）

- [ ] **Step 1: 写失败测试（httptest 起本地 mock 服务）**

`drawable_render_test.go`：

```go
package visualization

import (
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestToMermaidPNG_真实链 httptest 承载 mermaid.ink 端点（规则 3.3）
func TestToMermaidPNG_真实链(t *testing.T) {
	var gotPath string
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		_, _ = io.WriteString(w, "PNGBYTES")
	}))
	defer srv.Close()
	t.Setenv("MERMAID_INK_SERVER", srv.URL)

	d := NewDrawable()
	d.addNode("a", &fakeBranchSource{})
	script, err := d.ToMermaid("", false, false)
	require.NoError(t, err)
	png, err := d.ToMermaidPNG("", false)
	require.NoError(t, err)
	assert.Equal(t, "PNGBYTES", string(png))
	assert.True(t, strings.HasPrefix(gotPath, "/img/"))
	wantB64 := base64.URLEncoding.EncodeToString([]byte(script))
	assert.Equal(t, "/img/"+wantB64, gotPath)
	assert.Equal(t, "format=png", gotQuery)
}

// TestToMermaidSVG_尾部问号 query 空仍带 ?（_make_request_to_mermaid svg_url 拼接）
func TestToMermaidSVG_尾部问号(t *testing.T) {
	var gotReqURI string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotReqURI = r.URL.RequestURI()
		_, _ = io.WriteString(w, "<svg/>")
	}))
	defer srv.Close()
	t.Setenv("MERMAID_INK_SERVER", srv.URL)

	d := NewDrawable()
	d.addNode("a", &fakeBranchSource{})
	svg, err := d.ToMermaidSVG("", false)
	require.NoError(t, err)
	assert.Equal(t, "<svg/>", string(svg))
	// svg query 为空：URI 以 ? 结尾
	assert.True(t, strings.HasSuffix(gotReqURI, "?"))
}

// TestToMermaidPNG_服务错误 非2xx → error 含 status（MermaidError 复刻）
func TestToMermaidPNG_服务错误(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()
	t.Setenv("MERMAID_INK_SERVER", srv.URL)
	d := NewDrawable()
	d.addNode("a", &fakeBranchSource{})
	_, err := d.ToMermaidPNG("", false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "502")
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/openuapclaw/core/graph/visualization/ -run TestToMermaidPNG -v`
Expected: 编译失败（ToMermaidPNG 未定义）

- [ ] **Step 3: 实现（drawable.py:130-136 + mermaid-py 渲染链一比一）**

drawable.go 追加：

```go
// ──────────────────────────── 常量 ────────────────────────────

// DefaultMermaidInkServer mermaid.ink 默认服务（mermaid-py __main__.py:194-196）
const DefaultMermaidInkServer = "https://mermaid.ink"

// MermaidInkServerEnv 服务地址 env 键（Python MERMAID_INK_SERVER 同名）
const MermaidInkServerEnv = "MERMAID_INK_SERVER"

// ──────────────────────────── 导出函数 ────────────────────────────

// ToMermaidPNG 转 Mermaid 并渲染为 png（Python drawable.py:130-132 to_mermaid_png）
func (d *Drawable) ToMermaidPNG(title string, expandSubgraph any) ([]byte, error) {
	script, err := d.ToMermaid(title, expandSubgraph, false)
	if err != nil {
		return nil, err
	}
	return mermaidRender(script, "img", "format=png")
}

// ToMermaidSVG 转 Mermaid 并渲染为 svg（Python drawable.py:134-136 to_mermaid_svg；
// enable_animation 恒 True，drawable.py:274）
func (d *Drawable) ToMermaidSVG(title string, expandSubgraph any) ([]byte, error) {
	script, err := d.ToMermaid(title, expandSubgraph, true)
	if err != nil {
		return nil, err
	}
	return mermaidRender(script, "svg", "")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// mermaidRender mermaid.ink 渲染（mermaid-py __main__.py:165-225 一比一：
// urlsafe base64 → GET {server}/{endpoint}/{b64}?{query}）
func mermaidRender(script, endpoint, query string) ([]byte, error) {
	server := os.Getenv(MermaidInkServerEnv)
	if server == "" {
		server = DefaultMermaidInkServer
	}
	b64 := base64.URLEncoding.EncodeToString([]byte(script))
	url := server + "/" + endpoint + "/" + b64 + "?" + query
	resp, err := http.Get(url)
	if err != nil {
		return nil, exception.BuildError(exception.StatusDrawableGraphToMermaidInvalid,
			exception.WithParam("reason", err.Error()))
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, exception.BuildError(exception.StatusDrawableGraphToMermaidInvalid,
			exception.WithParam("reason", err.Error()))
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, exception.BuildError(exception.StatusDrawableGraphToMermaidInvalid,
			exception.WithParam("reason",
				fmt.Sprintf("HTTP %d Error: %s", resp.StatusCode, truncateDetails(body))))
	}
	return body, nil
}

// truncateDetails MermaidError Details 段（前 200 字符）
func truncateDetails(body []byte) string {
	s := string(body)
	if len(s) > 200 {
		return s[:200] + "..."
	}
	return s
}
```

（import 区同步补 `os`/`io`/`net/http`/`encoding/base64`/`fmt`/`regexp`/`strconv`/`strings`。）

- [ ] **Step 4: 测试通过 + 覆盖率复核**

Run: `go test -cover ./internal/openuapclaw/core/graph/visualization/ && go vet ./internal/openuapclaw/core/graph/visualization/`
Expected: PASS 且覆盖率 ≥85%

- [ ] **Step 5: Commit**

```bash
git commit internal/openuapclaw/core/graph/visualization -m "feat(8.13): png/svg 渲染落地（mermaid.ink HTTP 一比一：urlsafe b64/尾部问号/错误复刻）"
```

---

### Task 6: D-19-1 回填（BranchRouter drawable 三处）

**Files:**
- Modify: `internal/openuapclaw/core/workflow/components/flow/branch_router.go`
- Test: `internal/openuapclaw/core/workflow/components/flow/branch_router_test.go`（追加）

- [ ] **Step 1: 写失败测试**

`branch_router_test.go` 追加：

```go
// TestBranchRouterGetDrawableBranchTargets D-19-1 回填①：drawable 非空时返回投影
// （branch_router.py:71 get_drawable_branch_router）
func TestBranchRouterGetDrawableBranchTargets(t *testing.T) {
	r := flow.NewBranchRouter(false)
	require.NoError(t, r.AddBranch("a > 1", []string{"x"}, "b1"))
	require.NoError(t, r.AddBranch("b > 1", []string{"y"}, "b2"))
	// env 开启：构造期创建 drawable（NewBranchRouter 内读 env，测试直接置位）
	// 实现细节见 Step 3：非导出 drawable 字段 + 环境开关在构造函数
	_ = r
}
```

⚠️ 实施者注意：先读 Python `branch_router.py:41-112` 全文再定测试形状——drawable 的创建在 `__init__`（env 判定），`add_branch` 填充在每分支 append 时（branch_router.py:58-64）。测试按如下形状写（env 开/关双例）：

```go
// env 开启：drawable 创建 + add_branch 填充
func TestBranchRouterDrawable_env开启(t *testing.T) {
	t.Setenv("WORKFLOW_DRAWABLE", "true")
	r := flow.NewBranchRouter(false)
	require.NoError(t, r.AddBranch("a > 1", []string{"x"}, "b1"))
	require.NoError(t, r.AddBranch("b > 1", []string{"y"}, "b2"))
	targets, datas := r.GetDrawableBranchTargets()
	assert.Equal(t, []string{"x", "y"}, targets)
	assert.Equal(t, []string{"b1", "b2"}, datas) // datas=branch_id（核对 Python 58-64 实传值后修正）
}

// env 关闭：drawable nil，GetDrawableBranchTargets 返回 nil,nil（Python None 分支）
func TestBranchRouterDrawable_env关闭(t *testing.T) {
	t.Setenv("WORKFLOW_DRAWABLE", "")
	r := flow.NewBranchRouter(false)
	require.NoError(t, r.AddBranch("a > 1", []string{"x"}, "b1"))
	targets, datas := r.GetDrawableBranchTargets()
	assert.Nil(t, targets)
	assert.Nil(t, datas)
}
```

⚠️ `datas` 实传值必须先核对 Python `branch_router.py:55-64`：drawable 填充段在 `__init__` 还是 `add_branch`、datas 是否为 branch_id、`WORKFLOW_DRAWABLE` 判定用 `os.getenv(...) == "true"` 还是其他。**以 Python 源码为准修正测试断言后再进 Step 2**，不许想当然。

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/openuapclaw/core/workflow/components/flow/ -run TestBranchRouterDrawable`
Expected: 编译失败（GetDrawableBranchTargets 未定义）

- [ ] **Step 3: 实现三处回填**

branch_router.go：
1. 构造期 env 判定（branch_router.py 对应行）：`NewBranchRouter` 内 env=="true" 时创建 `drawable *visualization.Drawable`（非导出字段）
2. `AddBranch` 填充段（branch_router.py:58-64）：drawable 非空时 append（targets/datas 按 Python 实传值）
3. `GetDrawableBranchTargets() (targets, datas []string)`：drawable nil → nil,nil；否则返回投影（实现 `visualization.DrawableBranchTargetsSource` 接口，加编译期断言 `var _ visualization.DrawableBranchTargetsSource = (*BranchRouter)(nil)`）

同步删除 D-19-1 注释块（branch_router.go:60-61、75-76 的偏差注释改为指向 spec 的一句"已回填"或不留）。

- [ ] **Step 4: 测试通过**

Run: `go test ./internal/openuapclaw/core/workflow/components/flow/ -run TestBranchRouter -v`
Expected: 全 PASS（含既有用例零回归）

- [ ] **Step 5: Commit**

```bash
git commit internal/openuapclaw/core/workflow/components/flow/branch_router.go internal/openuapclaw/core/workflow/components/flow/branch_router_test.go -m "feat(8.13): D-19-1 回填——BranchRouter drawable 三处（env 开关/DrawableBranchRouter/add_branch 填充）"
```

---

### Task 7: 接口实现（flow/loop/llm 三包 + BranchComponent/IntentDetection）

**Files:**
- Modify: `internal/openuapclaw/core/workflow/components/flow/branch_comp.go`
- Modify: `internal/openuapclaw/core/workflow/components/flow/sub_workflow_comp.go`
- Modify: `internal/openuapclaw/core/workflow/components/flow/loop/loop_comp.go`
- Modify: `internal/openuapclaw/core/workflow/components/llm/intent_detection_comp.go`
- Test: 各包 `_test.go` 追加

- [ ] **Step 1: 写失败测试（各包一例，断言接口满足与返回值）**

`branch_comp_test.go` 追加：

```go
// TestBranchComponentDrawableRouter D-13-5 接口实现（≙ Python isinstance 分支
// 的 component.router() 取数）
func TestBranchComponentDrawableRouter(t *testing.T) {
	c := flow.NewBranchComponent(...) // 按既有测试的构造惯例补全参数
	var src visualization.BranchRouterSource = c
	assert.NotNil(t, src.DrawableRouter())
}
```

`sub_workflow_comp_test.go` 追加（构造用例沿用文件内既有 SubWorkflowComponent 构造惯例）：

```go
// TestSubWorkflowDrawableSubgraph 子图取图（Python sub_workflow._internal.drawable.get_graph()）
func TestSubWorkflowDrawableSubgraph(t *testing.T) {
	// env 开启使内嵌 workflow drawable 非空
	t.Setenv("WORKFLOW_DRAWABLE", "true")
	// 按既有测试惯例构造 *workflow.Workflow 与组件；组一个 start→end
	// 断言 c.SubWorkflowDrawableSubgraph() 返回含注册节点的 *DrawableGraph
}
```

`loop_comp_test.go` 追加：

```go
// TestLoopComponentLoopDrawableSubgraph 循环体子图（Python loop_group.drawable.get_graph()）
func TestLoopComponentLoopDrawableSubgraph(t *testing.T) {
	t.Setenv("WORKFLOW_DRAWABLE", "true")
	// 按 loop_comp_test.go 既有 LoopGroup 构造惯例；断言返回 *DrawableGraph
	// 且与 loopGroup.Drawable().GetGraph() 同指针
}
```

`intent_detection_comp_test.go` 追加 `DrawableRouter` 断言（同 branch_comp 形状）。

- [ ] **Step 2: 运行确认失败**

Run: `go build ./internal/openuapclaw/core/workflow/components/...`
Expected: 编译失败（方法未定义）

- [ ] **Step 3: 实现**

1. `branch_comp.go`：`func (c *BranchComponent) DrawableRouter() any { return c.router }`
2. `intent_detection_comp.go`：`func (c *IntentDetectionComponent) DrawableRouter() any { return c.router }`
3. `sub_workflow_comp.go`：`func (c *SubWorkflowComponent) SubWorkflowDrawableSubgraph() *visualization.DrawableGraph { return c.subWorkflow.Internal().Drawable().GetGraph() }`（依赖 Task 8 的 `Workflow.Internal()`/`BaseWorkflow.Drawable()` 访问器——**若 Task 8 未先行，本步先落访问器最小组**：见 Task 8 Step 3 ①）
4. `loop_comp.go`：
   - `func (c *LoopComponent) LoopDrawableSubgraph() *visualization.DrawableGraph { return c.loopGroup.Drawable().GetGraph() }`
   - `func (a *AdvancedLoopComponent) LoopDrawableSubgraph() *visualization.DrawableGraph { if lg, ok := a.body.(*LoopGroup); ok { return lg.Drawable().GetGraph() } return nil }`
   - D-20-2 回填：`AddWorkflowComp` 中（loop_comp.go:710 附近）按 Python loop_comp.py:83-84 补：
   ```go
   // Python loop_comp.py:83-84：drawable 非空且组件为 LoopBreakComponent 时登记
   if g.Drawable() != nil {
       if _, ok := workflowComp.(*LoopBreakComponent); ok {
           _ = g.Drawable().SetBreakNode(compID)
       }
   }
   ```
   并删除 :682/:710-711 的 D-20-2 注释块。

   注意访问器名 `Drawable()`/`Internal()` 归 Task 8 落地；Task 7 若先做会编译不过——**执行顺序：Task 8 的 Step 3 ①（两个访问器最小组）必须先于本步**，或在 Task 7 内一并落访问器并把 Task 8 剩余内容独立提交。计划按「Task 8 ① 访问器先落、Task 7 后行」执行；subagent 派发时以依赖为准调换。

- [ ] **Step 4: 测试通过**

Run: `go test ./internal/openuapclaw/core/workflow/components/... `
Expected: 全 PASS 零回归

- [ ] **Step 5: Commit**

```bash
git commit internal/openuapclaw/core/workflow/components -m "feat(8.13): D-13-5 接口实现四包落地 + D-20-2 set_break_node 回填"
```

---

### Task 8: D-d 回填（BaseWorkflow drawable 接线 + 组图钩子五段 + Draw 真实现）

**Files:**
- Modify: `internal/openuapclaw/core/workflow/base_workflow.go`
- Modify: `internal/openuapclaw/core/workflow/workflow.go`
- Test: `base_workflow_test.go` / `workflow_test.go` 追加

- [ ] **Step 1: 写失败测试**

`base_workflow_test.go` 追加：

```go
// TestBaseWorkflowDrawable_env开启 构造期创建（Python _workflow.py:157-160）
func TestBaseWorkflowDrawable_env开启(t *testing.T) {
	t.Setenv("WORKFLOW_DRAWABLE", "true")
	bw := workflow.NewBaseWorkflow(nil, nil)
	assert.NotNil(t, bw.Drawable())
}

// TestBaseWorkflowDrawable_env关闭 恒 nil（Python None 分支）
func TestBaseWorkflowDrawable_env关闭(t *testing.T) {
	t.Setenv("WORKFLOW_DRAWABLE", "")
	bw := workflow.NewBaseWorkflow(nil, nil)
	assert.Nil(t, bw.Drawable())
}
```

组图钩子五段（_workflow.py:200-294）——先核对 Python 原文五段的精确位置与形参再写测试：`add_workflow_comp`（drawable.add_node）、`start_comp`（set_start_node）、`end_comp`（set_end_node）、`add_edge` 静态/流式（`add_edge(src, dst)` / `add_edge(src, dst, False, True)`）、条件边（`add_edge(source=..., conditional=True, data=router)`）。测试形状（env 开启组 start→branch→end 图后断言 GetGraph 内容）：

```go
// TestBaseWorkflow组图Drawable镜像 env 开启时组图同步镜像（_workflow.py:200-294）
func TestBaseWorkflow组图Drawable镜像(t *testing.T) {
	t.Setenv("WORKFLOW_DRAWABLE", "true")
	// 按既有 base_workflow_test.go 组图惯例构造 BaseWorkflow，
	// AddWorkflowComp(start/branch/end) → StartComp → EndComp → AddEdge/条件边
	// 断言：bw.Drawable().GetGraph() 节点数/边数/StartNodes/EndNodes 与组图一致
}
```

`workflow_test.go` 追加：

```go
// TestDraw_env关闭_空分支 Python _drawable is None 分支保留（workflow.py:471-499）
func TestDraw_env关闭_空分支(t *testing.T) {
	t.Setenv("WORKFLOW_DRAWABLE", "")
	w := /* 按既有惯例构造 Workflow */
	assert.Equal(t, "", w.Draw("t", "mermaid", false, false))
	assert.Equal(t, []byte(nil), w.Draw("t", "png", false, false))
}

// TestDraw_env开启_真实现 env 开启走 to_mermaid/png/svg 三分支
func TestDraw_env开启_真实现(t *testing.T) {
	t.Setenv("WORKFLOW_DRAWABLE", "true")
	srv := httptest.NewServer(...) // mock mermaid.ink，同 visualization 测试
	defer srv.Close()
	// w 组图后：
	//   Draw(...,"mermaid",...) 断言含 "flowchart TB"
	//   Draw(...,"png",...) 断言返回 mock 服务响应体
	//   Draw(...,"svg",...) 同上
}
```

⚠️ 实施者注意：测试中 Workflow 构造必须按 `workflow_test.go` 既有惯例（真链构造，规则：真实链路测试纪律）；`Draw` 的 `expandSubgraph` 形参改 `any` 后既有 `workflow_test.go` 中 `TestDraw_未启用返回空`（workflow_test.go:83）与 `TestWorkflowDrawable常量` 需同步修签名，不修编译不过。

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/openuapclaw/core/workflow/ -run 'TestDraw|TestBaseWorkflow' `
Expected: 编译失败（Drawable()/Internal() 未定义、Draw 签名未改）

- [ ] **Step 3: 实现（回填四件）**

① 访问器最小组（Task 7 依赖）：

```go
// base_workflow.go
// Drawable 返回可视化件（Python _drawable 属性访问；8.13 D-d 回填）
func (w *BaseWorkflow) Drawable() *visualization.Drawable { return w.drawable }

// workflow.go
// Internal 返回内部基座（Python _internal 属性访问；SubWorkflowComponent
// drawable 取子图消费）
func (w *Workflow) Internal() *BaseWorkflow { return w.base }
```

（`w.base` 字段名以 workflow.go 实际承载为准核对——实施时先 grep Workflow struct 的 BaseWorkflow 持有字段。）

② `NewBaseWorkflow` env 接线（base_workflow.go:90-92 改写）：

```go
// Python _workflow.py:157-160：WORKFLOW_DRAWABLE=true 时创建 Drawable
if strings.EqualFold(os.Getenv("WORKFLOW_DRAWABLE"), "true") {
	drawable = visualization.NewDrawable()
}
return &BaseWorkflow{
	graph:          g,
	workflowConfig: cfg,
	workflowSpec:   cfg.Spec,
	streamActor:    stream_actor.NewStreamGraph(),
	session:        session.NewProxySession(),
	drawable:       drawable,
}
```

（字段声明 `drawable any` → `drawable *visualization.Drawable`，删 :62-63/:90-92 的 D-d 注释与 `_ = drawableEnabled` 段。）

③ 组图钩子五段（_workflow.py:200-294 一比一，位置：AddWorkflowComp 尾/StartComp/EndComp/AddEdge/条件边各自的 Python 对应行号处）：

```go
// Python _workflow.py:200-202：drawable.add_node(comp_id, workflow_comp)
if w.drawable != nil {
	w.drawable.addNode(compID, workflowComp)
}
```

StartComp/EndComp/AddEdge 各段同形（`SetStartNode(startCompID)` / `SetEndNode(endCompID)` / `AddEdge(src, dst, false, false, nil)` / 流式 `AddEdge(src, dst, false, true, nil)` / 条件边 `AddEdge(src, "", true, false, router)`）。⚠️ 条件边段的实参（router 从哪来、Python `_workflow.py:294` 的准确形态）实施时以 Python 源码为准核对，若 Go 侧 add_conditional_edges 入口与 Python 形参名不一致，以 Go 既有入口位置承载同一步骤。

④ `Draw()` 真实现（workflow.go:241-256 改写；workflow.py:471-499 一比一）：

```go
// Draw mermaid 生成（workflow.py:471-499 一比一；D-d/D-14-7 已回收）
func (w *Workflow) Draw(title, outputFormat string, expandSubgraph any, enableAnimation bool) any {
	if w.base.Drawable() == nil { // ≙ Python `if self._drawable is None`
		switch outputFormat {
		case "png", "svg":
			return []byte(nil)
		default:
			return ""
		}
	}
	switch outputFormat {
	case "png":
		b, err := w.base.Drawable().ToMermaidPNG(title, expandSubgraph)
		if err != nil {
			return []byte(nil) // ⚠️ 核对 workflow.py:496-497 的异常语义后修正
		}
		return b
	case "svg":
		b, err := w.base.Drawable().ToMermaidSVG(title, expandSubgraph)
		if err != nil {
			return []byte(nil)
		}
		return b
	default:
		s, err := w.base.Drawable().ToMermaid(title, expandSubgraph, enableAnimation)
		if err != nil {
			return ""
		}
		return s
	}
}
```

⚠️ Python `workflow.py:471-499` 的异常分支必须先读原文：若 Python 直接上抛（不吞），Go 改为返回 error 或 panic 对齐——**以原文为准，上面的吞错写法仅为占位示意，不许照抄**。

⑤ 签名变更同步：grep `\.Draw(` 全仓消费方（含集成测试）修形参类型；`workflow.go:243-244` D-14-7 注释改为「已随 8.13 回收」。

- [ ] **Step 4: 测试通过**

Run: `go test ./internal/openuapclaw/core/workflow/ && go build ./... && go vet ./...`
Expected: 全 PASS、全仓编译零错

- [ ] **Step 5: 锚点清零核对（规则 8.3）**

Run:
```bash
grep -rn "D-d：" internal/ | grep -v _test.go
grep -rn "恒 nil，env 读取骨架" internal/
grep -rn "D-19-1" internal/ | grep -v _test.go
grep -rn "D-20-2" internal/ | grep -v _test.go
```
Expected: 0 命中（spec/plan 台账除外）

- [ ] **Step 6: Commit**

```bash
git commit internal/openuapclaw/core/workflow -m "feat(8.13): D-d 回填——BaseWorkflow drawable 接线+组图钩子五段+Draw 真实现（D-14-7 expandSubgraph any 回收）"
```

---

### Task 9: 集成链 + 全量回归 + IMPLEMENTATION_PLAN 同步

**Files:**
- Create: `tests/integration/agentcore/workflow_drawable/drawable_suite_test.go`（目录名以现有集成树惯例为准，先 `ls tests/integration/` 核对）
- Modify: `IMPLEMENTATION_PLAN.md`（8.13 行 ☐→✅，8.19/8.20 的 ⤴️8.13 回填注记改为已完成，5.x 等悬挂处同步）

- [ ] **Step 1: 集成测试（真实跨子系统：workflow 组图 + components 分派 + visualization 输出）**

```go
//go:build integration

package workflow_drawable

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDrawable端到端 env 开启：组含 loop 子图 + branch 的真实工作流 → Draw 输出完整 mermaid
// 跨子系统：workflow(8.14) + components/flow+loop(8.19/8.20) + visualization(8.13)
func TestDrawable端到端(t *testing.T) {
	t.Setenv("WORKFLOW_DRAWABLE", "true")
	// 1. 组图：Start → BranchComponent(2 分支) → End；LoopGroup 含 2 组件
	// 2. w.Draw("", "mermaid", true, false)
	// 3. 断言：含 flowchart TB / subgraph 头 / -.-> 条件边 / 自环边
	// 4. Draw("", "png", false) 走 httptest mock 服务断言字节透传
}
```

⚠️ 组图代码按既有集成套件（如 workflow_vertex）构造惯例写全，断言数 ≥2（规则 7）；mock 只在 mermaid.ink 网络端点（规则 7.2 边界豁免）。

- [ ] **Step 2: 本地门槛**

```bash
go build ./... && go vet ./...
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./...
go run ./cmd/integration-audit -verify
```
Expected: 零 FAIL、审计通过

- [ ] **Step 3: 覆盖率终核**

```bash
go test -cover ./internal/openuapclaw/core/graph/visualization/
go test -cover ./internal/openuapclaw/core/workflow/...
```
Expected: visualization ≥85%；workflow 各包不低于改动前基线

- [ ] **Step 4: IMPLEMENTATION_PLAN.md 同步（规则 2）**

- 8.13 行 `☐ → ✅`，按领域八既有 ✅ 行格式补 Go 事实（包路径/覆盖率/偏差 D-13-1..5/回填 D-d/D-19-1/D-20-2 兑现）
- 8.19/8.20 行内 `⤴️8.13` 注记改为已完成
- 领域八小节末尾追加实施顺序记录（第十三次：8.13 落地，领域八全清）

- [ ] **Step 5: Commit + Push**

```bash
git status --porcelain   # 共享 index 核对
git commit tests/integration internal/openuapclaw IMPLEMENTATION_PLAN.md -m "feat(8.13): 集成端到端+全量回归+计划同步——领域八全清（D-d/D-19-1/D-20-2 回填兑现，D-13-1..5 归档）"
git pull --rebase && git push
```

---

## 删除清单（规则 8，Task 6-8 内兑现，Task 9 Step 2 锚点复核）

| 对象 | 位置 | 方式 |
|---|---|---|
| `_ = drawableEnabled` 留档段 | base_workflow.go:90-92 | 改写为真接线 |
| D-d 注释块 | base_workflow.go:62-63、workflow.go:241-242 | 删除 |
| D-19-1 注释块 | branch_router.go:60-61、75-76 | 删除 |
| D-20-2 注释块 | loop_comp.go:682、710-711 | 删除 |
| D-14-7 bool 收窄注释 | workflow.go:243-244 | 改「已回收」 |

## 自审记录

- spec 覆盖：§3 六文件→Task 2/3/4/5；§4.2 接口→Task 3（定义）+Task 7（实现）；§4.5→Task 4；§4.6→Task 5；§5 偏差 D-13-1..5→Task 3/7/8 各自落地；§6 三缝→Task 6/7/8；§7 测试→各 Task + Task 9；§8 错误码→Task 1。无缺口。
- 占位符：Task 6/8 中两处「以 Python 源码为准核对」标注是**防想当然的核对指令**（Python 原文行号已给），非 TBD——实施者按行号读原文即可定案。
- 类型一致性：`DrawableBranchTargetsSource.GetDrawableBranchTargets` 在 Task 3（定义）/Task 6（实现）一致；`LoopDrawableSubgraph/SubWorkflowDrawableSubgraph/DrawableRouter` 在 Task 3/Task 7 一致；`Internal()/Drawable()` 访问器在 Task 7/8 交叉依赖已标注执行顺序约束。
