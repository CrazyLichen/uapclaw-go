package visualization

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestToMermaid_基础图 两节点静态边（格式逐字对齐 mermaid-py：
// frontmatter + flowchart TB + 节点 + 连线；start/end → round-edge）
func TestToMermaid_基础图(t *testing.T) {
	d := NewDrawable()
	d.addNode("start", &fakeBranchSource{})
	d.addNode("end", &fakeBranchSource{})
	require.NoError(t, d.SetStartNode("start"))
	require.NoError(t, d.SetEndNode("end"))
	d.addEdge("start", "end", false, false, nil)
	s, err := d.ToMermaid("", false, false)
	require.NoError(t, err)
	assert.Equal(t,
		"---\ntitle: \n---\nflowchart TB\n\tnode_1(\"start\")\n\tnode_2(\"end\")\n\tnode_1 --> node_2\n",
		s)
}

// TestToMermaid_标题 title 进入 frontmatter
func TestToMermaid_标题(t *testing.T) {
	d := NewDrawable()
	d.addNode("a", &fakeBranchSource{})
	s, err := d.ToMermaid("T1", false, false)
	require.NoError(t, err)
	assert.Contains(t, s, "---\ntitle: T1\n---")
}

// TestToMermaid_条件边 dotted 形状 + 标签（drawable.py:325/332-333）
func TestToMermaid_条件边(t *testing.T) {
	d := NewDrawable()
	d.addNode("a", &fakeBranchSource{})
	d.addNode("x", &fakeBranchSource{})
	d.addEdge("a", "x", true, false, &fakeTargetsSource{targets: []string{"x"}, datas: []string{"hit"}})
	s, err := d.ToMermaid("", false, false)
	require.NoError(t, err)
	assert.Contains(t, s, "node_1 -.->|\"hit\"| node_2")
}

// TestToMermaid_流式动画 thick + id@properties（drawable.py:327-331 + _ExtendLink）
func TestToMermaid_流式动画(t *testing.T) {
	d := NewDrawable()
	d.addNode("a", &fakeBranchSource{})
	d.addNode("b", &fakeBranchSource{})
	d.addEdge("a", "b", false, true, nil)
	s, err := d.ToMermaid("", false, true)
	require.NoError(t, err)
	assert.Contains(t, s, "node_1 link_1@==> node_2\nlink_1@{animate: true}")
}

// TestToMermaid_流式无动画 streaming 无 animation 时无 id@（drawable.py:329 守卫）
func TestToMermaid_流式无动画(t *testing.T) {
	d := NewDrawable()
	d.addNode("a", &fakeBranchSource{})
	d.addNode("b", &fakeBranchSource{})
	d.addEdge("a", "b", false, true, nil)
	s, err := d.ToMermaid("", false, false)
	require.NoError(t, err)
	assert.Contains(t, s, "node_1 ==> node_2")
}

// TestToMermaid_子图展开 子图嵌套结构（drawable.py:276-315）
func TestToMermaid_子图展开(t *testing.T) {
	sub := &DrawableGraph{
		Nodes: map[string]NodeLike{
			"s": &DrawableNode{ID: "s"}, "e": &DrawableNode{ID: "e"},
		},
		Edges:      []*DrawableEdge{{Source: "s", Target: "e"}},
		StartNodes: []NodeLike{&DrawableNode{ID: "s"}},
		EndNodes:   []NodeLike{&DrawableNode{ID: "e"}},
	}
	d := NewDrawable()
	d.addNode("loop1", &fakeLoopSource{sub: sub})
	s, err := d.ToMermaid("", true, false)
	require.NoError(t, err)
	// Python 真实 ID 序：子图内部节点先分配（drawable.py:283-292 递归先于 :309 头节点），
	// 子图序按 ID 兜底排序 e(node_1)、s(node_2)，头节点 node_3
	assert.Contains(t, s, "subgraph node_3 [\"loop1\"]")
	assert.Contains(t, s, "\tdirection TB")
	assert.Contains(t, s, "end")
}

// TestToMermaid_子图不展开 expand=false 时子图节点画普通框
func TestToMermaid_子图不展开(t *testing.T) {
	sub := &DrawableGraph{Nodes: map[string]NodeLike{"s": &DrawableNode{ID: "s"}}}
	d := NewDrawable()
	d.addNode("loop1", &fakeLoopSource{sub: sub})
	s, err := d.ToMermaid("", false, false)
	require.NoError(t, err)
	assert.NotContains(t, s, "subgraph ")
}

// TestToMermaid_int层级 expand=1 展开一层、内层子图不再展开
func TestToMermaid_int层级(t *testing.T) {
	inner := &DrawableGraph{Nodes: map[string]NodeLike{"i": &DrawableNode{ID: "i"}}}
	outer := &DrawableGraph{
		Nodes: map[string]NodeLike{
			"mid": &DrawableSubgraphNode{DrawableNode: DrawableNode{ID: "mid"}, Subgraph: inner},
		},
	}
	d := NewDrawable()
	d.addNode("loop1", &fakeLoopSource{sub: outer})
	s1, err := d.ToMermaid("", 1, false)
	require.NoError(t, err)
	assert.Contains(t, s1, "subgraph node_2 [\"loop1\"]") // 头节点序：内层 mid=node_1 先分配
	assert.NotContains(t, s1, "subgraph node_1")          // 第二层不再展开（mid 画普通框）
}

// TestToMermaid_int零值 expand=0 不展开（drawable.py:252 <=0 短路）
func TestToMermaid_int零值(t *testing.T) {
	sub := &DrawableGraph{Nodes: map[string]NodeLike{"s": &DrawableNode{ID: "s"}}}
	d := NewDrawable()
	d.addNode("loop1", &fakeLoopSource{sub: sub})
	s, err := d.ToMermaid("", 0, false)
	require.NoError(t, err)
	assert.NotContains(t, s, "subgraph ")
}

// TestToMermaid_参数校验 非法 expand/animation（drawable.py:239-244，112043）
func TestToMermaid_参数校验(t *testing.T) {
	d := NewDrawable()
	_, err := d.ToMermaid("", 1.5, false) // expand 非 bool/int
	assert.Error(t, err)
	_, err = d.ToMermaid("", -1, false)
	assert.Error(t, err)
	_, err = d.ToMermaid("", true, "yes") // animation 非 bool
	assert.Error(t, err)
	_, err = d.ToMermaid("", true, false) // 合法
	assert.NoError(t, err)
}

// TestToMermaid_子图边界连接 四象限：外节点 → 子图 start（drawable.py:337-340）
func TestToMermaid_子图边界连接(t *testing.T) {
	sub := &DrawableGraph{
		Nodes:      map[string]NodeLike{"s": &DrawableNode{ID: "s"}, "e": &DrawableNode{ID: "e"}},
		Edges:      []*DrawableEdge{{Source: "s", Target: "e"}},
		StartNodes: []NodeLike{&DrawableNode{ID: "s"}},
		EndNodes:   []NodeLike{&DrawableNode{ID: "e"}},
	}
	d := NewDrawable()
	d.addNode("loop1", &fakeLoopSource{sub: sub})
	d.addNode("out", &fakeBranchSource{})
	d.addEdge("out", "loop1", false, false, nil)
	s, err := d.ToMermaid("", true, false)
	require.NoError(t, err)
	// 外边 out(node_4)→loop1 展开为 out→子图start（子图内 s=node_2）；
	// 子图内部 s→e 为 node_2 --> node_1（e=node_1 round-edge）
	assert.Contains(t, s, "node_4 --> node_2")
	assert.Contains(t, s, "node_2 --> node_1")
	assert.NotContains(t, s, "node_4 --> node_3") // 不直接连子图头
}
