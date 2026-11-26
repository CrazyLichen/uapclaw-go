package flow

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/graph/visualization"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/workflow"
)

// TestSubWorkflowDrawableSubgraph 子图取图（Python drawable.py:76
// `component.sub_workflow._internal.drawable.get_graph()`；8.13 D-13-5 接口实现）
func TestSubWorkflowDrawableSubgraph(t *testing.T) {
	t.Setenv("WORKFLOW_DRAWABLE", "true")
	sub := workflow.NewWorkflow(nil)
	require.NoError(t, sub.Internal().AddWorkflowComp("inner", NewStart(), boolPtr(false),
		nil, nil, nil, nil, nil, 0, -1.0, nil, nil))
	c, err := NewSubWorkflowComponent(sub)
	require.NoError(t, err)

	var src visualization.SubgraphSource = c
	g := src.SubWorkflowDrawableSubgraph()
	require.NotNil(t, g)
	assert.Contains(t, g.Nodes, "inner")
}

// TestSubWorkflowDrawableSubgraph_env关闭 env 关闭时子图 nil（drawable None 透传）
func TestSubWorkflowDrawableSubgraph_env关闭(t *testing.T) {
	t.Setenv("WORKFLOW_DRAWABLE", "")
	sub := workflow.NewWorkflow(nil)
	c, err := NewSubWorkflowComponent(sub)
	require.NoError(t, err)
	assert.Nil(t, c.SubWorkflowDrawableSubgraph(), "env 关闭应返回 nil")
}

func boolPtr(b bool) *bool { return &b }
