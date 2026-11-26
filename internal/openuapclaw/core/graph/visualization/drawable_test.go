package visualization

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeLoopSource 循环子图源测试替身（D-13-5 接口面即真实缝，实现在边界）
type fakeLoopSource struct{ sub *DrawableGraph }

func (f *fakeLoopSource) LoopDrawableSubgraph() *DrawableGraph { return f.sub }

// fakeBranchSource 分支路由源测试替身
type fakeBranchSource struct{}

func (f *fakeBranchSource) DrawableRouter() any { return nil }

// fakeTargetsSource 分支目标投影替身
type fakeTargetsSource struct{ targets, datas []string }

func (f *fakeTargetsSource) GetDrawableBranchTargets() ([]string, []string) {
	return f.targets, f.datas
}

// TestDrawableAddNode_普通节点 add_node 默认分支（drawable.py:80-81）
func TestDrawableAddNode_普通节点(t *testing.T) {
	d := NewDrawable()
	d.addNode("n1", &fakeBranchSource{})
	assert.Contains(t, d.graph.Nodes, "n1")
	assert.Equal(t, "n1", d.graph.Nodes["n1"].NodeID())
	assert.NotContains(t, d.loopNodes, "n1")
}

// TestDrawableAddNode_循环子图 循环分支：子图 + end 自动发现 + 自环边（drawable.py:58-73）
func TestDrawableAddNode_循环子图(t *testing.T) {
	sub := &DrawableGraph{
		Nodes: map[string]NodeLike{
			"a": &DrawableNode{ID: "a"}, "b": &DrawableNode{ID: "b"},
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
	assert.Equal(t, "b", sub.EndNodes[0].NodeID())
	// 自环边
	require.Len(t, d.graph.Edges, 1)
	assert.Equal(t, "loop1", d.graph.Edges[0].Source)
	assert.Equal(t, "loop1", d.graph.Edges[0].Target)
	assert.True(t, d.graph.Edges[0].Conditional)
}

// TestDrawableAddNode_循环子图_end已设 时不重复发现（drawable.py:62 判断）
func TestDrawableAddNode_循环子图_end已设(t *testing.T) {
	sub := &DrawableGraph{
		Nodes:    map[string]NodeLike{"a": &DrawableNode{ID: "a"}, "b": &DrawableNode{ID: "b"}},
		Edges:    []*DrawableEdge{{Source: "a", Target: "b"}},
		EndNodes: []NodeLike{&DrawableNode{ID: "b"}},
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

// TestDrawableSetEndNode_不存在报错（drawable.py:92-95，112021）
func TestDrawableSetEndNode_不存在报错(t *testing.T) {
	d := NewDrawable()
	assert.Error(t, d.SetEndNode("ghost"))
}

// TestDrawableSetBreakNode_不存在报错（drawable.py:100-102，112022）
func TestDrawableSetBreakNode_不存在报错(t *testing.T) {
	d := NewDrawable()
	assert.Error(t, d.SetBreakNode("ghost"))
}

// TestDrawableSetStartEnd 存在时登记（drawable.py:88/95/102）
func TestDrawableSetStartEnd(t *testing.T) {
	d := NewDrawable()
	d.addNode("s1", &fakeBranchSource{})
	d.addNode("e1", &fakeBranchSource{})
	d.addNode("bk1", &fakeBranchSource{})
	require.NoError(t, d.SetStartNode("s1"))
	require.NoError(t, d.SetEndNode("e1"))
	require.NoError(t, d.SetBreakNode("bk1"))
	assert.Equal(t, "s1", d.graph.StartNodes[0].NodeID())
	assert.Equal(t, "e1", d.graph.EndNodes[0].NodeID())
	assert.Equal(t, "bk1", d.graph.BreakNodes[0].NodeID())
}

// TestDrawableAddEdge_静态边（drawable.py:110-113）
func TestDrawableAddEdge_静态边(t *testing.T) {
	d := NewDrawable()
	d.AddEdge("a", "b", false, false, nil)
	require.Len(t, d.graph.Edges, 1)
	assert.False(t, d.graph.Edges[0].Conditional)
	assert.False(t, d.graph.Edges[0].Streaming)
}

// TestDrawableAddEdge_流式边（drawable.py:104-112 streaming 透传）
func TestDrawableAddEdge_流式边(t *testing.T) {
	d := NewDrawable()
	d.AddEdge("a", "b", false, true, nil)
	assert.True(t, d.graph.Edges[0].Streaming)
}

// TestDrawableAddEdge_循环源强制条件边（drawable.py:107-109）
func TestDrawableAddEdge_循环源强制条件边(t *testing.T) {
	d := NewDrawable()
	d.loopNodes["loop1"] = struct{}{}
	d.AddEdge("loop1", "x", false, false, nil)
	require.Len(t, d.graph.Edges, 1)
	assert.True(t, d.graph.Edges[0].Conditional)
}

// TestDrawableAddEdge_条件边BranchRouter展开（drawable.py:115-124）
func TestDrawableAddEdge_条件边BranchRouter展开(t *testing.T) {
	d := NewDrawable()
	router := &fakeTargetsSource{targets: []string{"x", "y"}, datas: []string{"d1", "d2"}}
	d.AddEdge("a", "", true, false, router)
	require.Len(t, d.graph.Edges, 2)
	assert.Equal(t, "x", d.graph.Edges[0].Target)
	assert.Equal(t, "d1", d.graph.Edges[0].Data)
	assert.Equal(t, "y", d.graph.Edges[1].Target)
	assert.Equal(t, "d2", d.graph.Edges[1].Data)
	assert.True(t, d.graph.Edges[0].Conditional)
}

// TestDrawableAddEdge_条件边datas缺省 datas nil 时不取值（drawable.py:123 三元）
func TestDrawableAddEdge_条件边datas缺省(t *testing.T) {
	d := NewDrawable()
	router := &fakeTargetsSource{targets: []string{"x"}, datas: nil}
	d.AddEdge("a", "", true, false, router)
	require.Len(t, d.graph.Edges, 1)
	assert.Equal(t, "", d.graph.Edges[0].Data)
}

// TestGetTargets_不可达形态 函数壳行为（D-13-1：非 callable → nil）
func TestGetTargets_不可达形态(t *testing.T) {
	assert.Nil(t, getTargets(42))
	assert.Nil(t, getTargets("str"))
	assert.Nil(t, getTargets(nil))
}

// TestGetGraph 镜像访问（drawable.py:138-140）
func TestGetGraph(t *testing.T) {
	d := NewDrawable()
	assert.Same(t, d.graph, d.GetGraph())
}

// TestDrawableAddNode_重复compID R-2 回归：dict 覆盖语义（Python comp_configs/
// add_node dict 覆盖，重复注册不重复输出、保持原插入序）
func TestDrawableAddNode_重复compID(t *testing.T) {
	d := NewDrawable()
	d.addNode("n1", &fakeBranchSource{})
	d.addNode("n2", &fakeBranchSource{})
	d.addNode("n1", &fakeBranchSource{}) // 重复注册
	require.Len(t, d.nodeOrder, 2)
	require.Len(t, d.graph.SubNodeOrder, 2)
	assert.Equal(t, []string{"n1", "n2"}, d.nodeOrder)
	s, err := d.ToMermaid("", false, false)
	require.NoError(t, err)
	assert.Equal(t, 1, strings.Count(s, "node_1[")) // 输出仅一次
}

// TestDrawableAddNode_循环子图end顺序 R-1 回归：end 自动发现按插入序
// （Python out_degrees dict 按 subgraph.nodes 插入序迭代）
func TestDrawableAddNode_循环子图end顺序(t *testing.T) {
	sub := &DrawableGraph{
		Nodes:        map[string]NodeLike{"z": &DrawableNode{ID: "z"}, "a": &DrawableNode{ID: "a"}, "m": &DrawableNode{ID: "m"}},
		SubNodeOrder: []string{"z", "a", "m"}, // 注册序（map 序不可依赖）
		Edges:        []*DrawableEdge{{Source: "z", Target: "m"}},
	}
	d := NewDrawable()
	d.addNode("loop1", &fakeLoopSource{sub: sub})
	// 出度=0：a、m（z 有出边 z→m）；按插入序 a 在 m 前
	require.Len(t, sub.EndNodes, 2)
	assert.Equal(t, "a", sub.EndNodes[0].NodeID())
	assert.Equal(t, "m", sub.EndNodes[1].NodeID())
}
