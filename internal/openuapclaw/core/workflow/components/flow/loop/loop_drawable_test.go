package loop

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/graph/visualization"
)

// TestLoopComponentLoopDrawableSubgraph 循环体子图（Python drawable.py:59-60
// loop_group.drawable.get_graph()；8.13 D-13-5 接口实现）
func TestLoopComponentLoopDrawableSubgraph(t *testing.T) {
	t.Setenv("WORKFLOW_DRAWABLE", "true")
	group := NewLoopGroup()
	require.NoError(t, group.AddWorkflowComp("body_1", newCounting(), nil,
		nil, nil, nil, nil, nil, 0, -1.0, nil, nil))
	require.NoError(t, group.StartNodes([]string{"body_1"}))
	require.NoError(t, group.EndNodes([]string{"body_1"}))
	loopComp, err := NewLoopComponent(group, nil)
	require.NoError(t, err)

	var src visualization.LoopSubgraphSource = loopComp
	sub := src.LoopDrawableSubgraph()
	require.NotNil(t, sub)
	assert.Same(t, group.Drawable().GetGraph(), sub)
	assert.Contains(t, sub.Nodes, "body_1")
}

// TestAdvancedLoopComponentLoopDrawableSubgraph AdvancedLoop 取 body 子图
// （Python drawable.py:60 `component.body.drawable.get_graph()`）
func TestAdvancedLoopComponentLoopDrawableSubgraph(t *testing.T) {
	t.Setenv("WORKFLOW_DRAWABLE", "true")
	group := NewLoopGroup()
	require.NoError(t, group.AddWorkflowComp("body_1", newCounting(), nil,
		nil, nil, nil, nil, nil, 0, -1.0, nil, nil))
	alc, err := NewAdvancedLoopComponent(group, "loop.index < 3", nil, nil)
	require.NoError(t, err)

	var src visualization.LoopSubgraphSource = alc
	sub := src.LoopDrawableSubgraph()
	require.NotNil(t, sub)
	assert.Contains(t, sub.Nodes, "body_1")
}

// TestLoopGroupSetBreakNode D-20-2 回填：env 开启时 LoopBreakComponent 登记 break 节点
// （loop_comp.py:83-84）
func TestLoopGroupSetBreakNode(t *testing.T) {
	t.Setenv("WORKFLOW_DRAWABLE", "true")
	group := NewLoopGroup()
	require.NoError(t, group.AddWorkflowComp("body_1", newCounting(), nil,
		nil, nil, nil, nil, nil, 0, -1.0, nil, nil))
	require.NoError(t, group.AddWorkflowComp("brk", NewLoopBreakComponent(), nil,
		nil, nil, nil, nil, nil, 0, -1.0, nil, nil))
	require.NotNil(t, group.Drawable())
	require.Len(t, group.Drawable().GetGraph().BreakNodes, 1)
	assert.Equal(t, "brk", group.Drawable().GetGraph().BreakNodes[0].NodeID())
}

// TestLoopGroupSetBreakNode_env关闭 env 关闭时零登记（行为与现状等价回归锁）
func TestLoopGroupSetBreakNode_env关闭(t *testing.T) {
	t.Setenv("WORKFLOW_DRAWABLE", "")
	group := NewLoopGroup()
	require.NoError(t, group.AddWorkflowComp("body_1", newCounting(), nil,
		nil, nil, nil, nil, nil, 0, -1.0, nil, nil))
	require.NoError(t, group.AddWorkflowComp("brk", NewLoopBreakComponent(), nil,
		nil, nil, nil, nil, nil, 0, -1.0, nil, nil))
	assert.Nil(t, group.Drawable())
}
