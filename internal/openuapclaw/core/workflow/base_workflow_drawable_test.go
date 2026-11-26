package workflow

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/graph/visualization"
)

// TestBaseWorkflowDrawable_env开启 构造期创建（Python _workflow.py:157-160）
func TestBaseWorkflowDrawable_env开启(t *testing.T) {
	t.Setenv("WORKFLOW_DRAWABLE", "true")
	bw := NewBaseWorkflow(nil, nil)
	assert.NotNil(t, bw.Drawable())
}

// TestBaseWorkflowDrawable_env关闭 恒 nil（Python `self._drawable = None` 分支）
func TestBaseWorkflowDrawable_env关闭(t *testing.T) {
	t.Setenv("WORKFLOW_DRAWABLE", "")
	bw := NewBaseWorkflow(nil, nil)
	assert.Nil(t, bw.Drawable())
}

// TestBaseWorkflow组图Drawable镜像 env 开启时组图同步镜像（_workflow.py:200-294
// 五段钩子：add_node/set_start_node/set_end_node/add_edge）
func TestBaseWorkflow组图Drawable镜像(t *testing.T) {
	t.Setenv("WORKFLOW_DRAWABLE", "true")
	bw := NewBaseWorkflow(nil, nil)
	require.NoError(t, bw.AddWorkflowComp("s", newFakeStartComp(), boolPtr(false),
		nil, nil, nil, nil, nil, 0, -1.0, nil, nil))
	require.NoError(t, bw.AddWorkflowComp("e", newFakeStartComp(), boolPtr(false),
		nil, nil, nil, nil, nil, 0, -1.0, nil, nil))
	require.NoError(t, bw.StartComp("s"))
	require.NoError(t, bw.EndComp("e"))
	require.NoError(t, bw.AddConnection([]string{"s"}, "e"))

	g := bw.Drawable().GetGraph()
	require.Len(t, g.SubNodeOrder, 2)
	assert.Equal(t, "s", g.StartNodes[0].NodeID())
	assert.Equal(t, "e", g.EndNodes[0].NodeID())
	require.Len(t, g.Edges, 1)
	assert.Equal(t, "s", g.Edges[0].Source)
	assert.Equal(t, "e", g.Edges[0].Target)
	assert.False(t, g.Edges[0].Streaming)

	// to_mermaid 全链（_workflow.py:321-324）
	s, err := bw.Drawable().ToMermaid("T", false, false)
	require.NoError(t, err)
	assert.Contains(t, s, "flowchart TB")
	assert.Contains(t, s, "node_1(\"s\")")
}

// TestBaseWorkflow组图Drawable镜像_env关闭 env 关闭时零镜像（行为与现状等价回归锁）
func TestBaseWorkflow组图Drawable镜像_env关闭(t *testing.T) {
	t.Setenv("WORKFLOW_DRAWABLE", "")
	bw := NewBaseWorkflow(nil, nil)
	require.NoError(t, bw.AddWorkflowComp("s", newFakeStartComp(), boolPtr(false),
		nil, nil, nil, nil, nil, 0, -1.0, nil, nil))
	require.NoError(t, bw.StartComp("s"))
	require.NoError(t, bw.EndComp("s"))
	assert.Nil(t, bw.Drawable())
}

// TestBaseWorkflow流式边Drawable镜像 流式边 streaming=true（_workflow.py:255-256）
func TestBaseWorkflow流式边Drawable镜像(t *testing.T) {
	t.Setenv("WORKFLOW_DRAWABLE", "true")
	bw := NewBaseWorkflow(nil, nil)
	require.NoError(t, bw.AddWorkflowComp("s", newFakeStartComp(), boolPtr(false),
		nil, nil, nil, nil, nil, 0, -1.0, nil, nil))
	require.NoError(t, bw.AddWorkflowComp("e", newFakeStartComp(), boolPtr(false),
		nil, nil, nil, nil, nil, 0, -1.0, nil, nil))
	require.NoError(t, bw.AddStreamConnection("s", "e"))
	require.Len(t, bw.Drawable().GetGraph().Edges, 1)
	assert.True(t, bw.Drawable().GetGraph().Edges[0].Streaming)
}

// TestWorkflowDraw_env开启_真实现 Draw 三分支走 Drawable 真链（workflow.py:471-499）
func TestWorkflowDraw_env开启_真实现(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "RENDERED")
	}))
	defer srv.Close()
	t.Setenv("MERMAID_INK_SERVER", srv.URL)
	t.Setenv("WORKFLOW_DRAWABLE", "true")

	w := NewWorkflow(nil)
	require.NoError(t, w.internal.AddWorkflowComp("s", newFakeStartComp(), boolPtr(false),
		nil, nil, nil, nil, nil, 0, -1.0, nil, nil))
	require.NoError(t, w.internal.StartComp("s"))
	require.NoError(t, w.internal.EndComp("s"))

	// mermaid 分支
	got, err := w.Draw("T", "mermaid", false, false)
	require.NoError(t, err)
	assert.Contains(t, got.(string), "flowchart TB")
	// png 分支（mock 服务响应体透传）
	got, err = w.Draw("T", "png", false, false)
	require.NoError(t, err)
	assert.Equal(t, "RENDERED", string(got.([]byte)))
	// svg 分支
	got, err = w.Draw("T", "svg", false, false)
	require.NoError(t, err)
	assert.Equal(t, "RENDERED", string(got.([]byte)))
}

// TestWorkflowDraw_层级展开 expandSubgraph int 层级语义（D-14-7 回收）
func TestWorkflowDraw_层级展开(t *testing.T) {
	t.Setenv("WORKFLOW_DRAWABLE", "true")
	w := NewWorkflow(nil)
	require.NoError(t, w.internal.AddWorkflowComp("s", newFakeStartComp(), boolPtr(false),
		nil, nil, nil, nil, nil, 0, -1.0, nil, nil))
	got, err := w.Draw("T", "mermaid", 1, false)
	require.NoError(t, err)
	assert.Contains(t, got.(string), "flowchart TB")
}

// TestWorkflowInternal访问器 Internal() 返回内部基座（8.13 回填件）
func TestWorkflowInternal访问器(t *testing.T) {
	w := NewWorkflow(nil)
	assert.NotNil(t, w.Internal())
	assert.Same(t, w.internal, w.Internal())
}

// 编译期引用保活（visualization 常量消费面）
var _ = visualization.MermaidInkServerEnv
