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

// TestDrawableBranchRouter 分支路由 drawable 形状（drawable_edge.py:24-26）
func TestDrawableBranchRouter(t *testing.T) {
	br := &DrawableBranchRouter{Targets: []string{"x", "y"}, Datas: []string{"d1", "d2"}}
	assert.Equal(t, []string{"x", "y"}, br.Targets)
	assert.Equal(t, []string{"d1", "d2"}, br.Datas)
}

// TestDrawableGraph构造 对齐 drawable_graph.py dataclass
func TestDrawableGraph构造(t *testing.T) {
	g := &DrawableGraph{Nodes: map[string]NodeLike{}, Edges: []*DrawableEdge{}}
	assert.Empty(t, g.Nodes)
	assert.Empty(t, g.Edges)
	assert.Nil(t, g.BreakNodes) // Python Optional → Go nil
}

// TestDrawableSubgraphNode嵌入 继承 → Go 嵌入（drawable_subgraph_node.py）
func TestDrawableSubgraphNode嵌入(t *testing.T) {
	n := &DrawableSubgraphNode{DrawableNode: DrawableNode{ID: "loop1"}}
	assert.Equal(t, "loop1", n.ID) // 提升字段
	assert.Nil(t, n.Subgraph)
}
