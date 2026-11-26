//go:build integration

// Package workflow_drawable 8.13 Graph Visualization 全链集成测试。
// 真实跨子系统链路：workflow（门面 + 组图 drawable 钩子五段 + Draw 三分支）→
// components/flow+loop（BranchRouter 分支投影 + LoopGroup 子图 + break 登记，
// D-19-1/D-20-2 回填件）→ visualization（Drawable 镜像 + Mermaid 拼接 + 渲染）。
// 对齐 Python: openjiuwen/core/graph/visualization/ + _workflow.py drawable 段。
package workflow_drawable

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/graph/visualization"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/workflow"
	conditionpkg "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/workflow/components/condition"
	flow "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/workflow/components/flow"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/workflow/components/flow/loop"
)

// DrawableSuite 8.13 可视化全链集成。
type DrawableSuite struct {
	suite.Suite
}

func TestDrawableSuite(t *testing.T) {
	suite.Run(t, new(DrawableSuite))
}

// buildWorkflow env 开启下组真实工作流：start → branch(双出口) → end_a/end_b，
// 外加 loop 子图节点（loop 内 2 组件 + break）。
func (s *DrawableSuite) buildWorkflow() *workflow.Workflow {
	w := workflow.NewWorkflow(nil)
	b := flow.NewBranchComponent()
	s.Require().NoError(b.AddBranch("${amount} > 100", []string{"end_a"}, "high"))
	s.Require().NoError(b.AddBranch(conditionpkg.NewAlwaysTrue(), []string{"end_b"}, "low"))

	w.SetStartComp("start", flow.NewStart(), nil, nil, nil)
	w.AddWorkflowComp("branch", b, nil, nil, nil, nil, nil, nil, 0, -1.0, nil, nil)

	// loop 子图（LoopGroup 组图走 BaseWorkflow drawable 镜像 + D-20-2 break 登记）
	group := loop.NewLoopGroup()
	body := loop.NewLoopBreakComponent() // 占位组件即可（组图面）
	s.Require().NoError(group.AddWorkflowComp("l_body", body, nil,
		nil, nil, nil, nil, nil, 0, -1.0, nil, nil))
	s.Require().NoError(group.AddWorkflowComp("l_brk", loop.NewLoopBreakComponent(), nil,
		nil, nil, nil, nil, nil, 0, -1.0, nil, nil))
	s.Require().NoError(group.StartNodes([]string{"l_body"}))
	s.Require().NoError(group.EndNodes([]string{"l_brk"}))
	lc, err := loop.NewLoopComponent(group, nil)
	s.Require().NoError(err)
	w.AddWorkflowComp("loop1", lc, nil, nil, nil, nil, nil, nil, 0, -1.0, nil, nil)

	newEnd := func(tmpl string) *flow.End {
		cfg, err := flow.NewEndConfig(tmpl)
		s.Require().NoError(err)
		e, err := flow.NewEnd(cfg)
		s.Require().NoError(err)
		return e
	}
	w.SetEndComp("end_b", newEnd("low:{{amount}}"), nil, nil, nil, nil, "batch", nil)
	w.SetEndComp("end_a", newEnd("high:{{amount}}"), nil, nil, nil, nil, "batch", nil)
	s.Require().NoError(w.Internal().AddConnection([]string{"start"}, "branch"))
	s.Require().NoError(w.Internal().AddConnection([]string{"branch"}, "end_a"))
	s.Require().NoError(w.Internal().AddConnection([]string{"branch"}, "end_b"))
	return w
}

// TestDrawable端到端 env 开启：组图 → Draw mermaid 含条件边/子图/自环；
// expand 层级控制子图展开。
func (s *DrawableSuite) TestDrawable端到端() {
	t := s.T()
	t.Setenv("WORKFLOW_DRAWABLE", "true")
	w := s.buildWorkflow()

	got, err := w.Draw("集成", "mermaid", true, false)
	require.NoError(t, err)
	svg := got.(string)
	assert.Contains(t, svg, "flowchart TB")
	assert.Contains(t, svg, "title: 集成")
	// 条件边（BranchRouter drawable 投影 → dotted + 标签；
	// branch_router.py:60-61：str 条件覆盖 branch_id 作标签）
	assert.Contains(t, svg, "-.->|\"${amount} > 100\"|")
	assert.Contains(t, svg, "-.->|\"low\"|")
	// loop 子图展开（expand=true）
	assert.Contains(t, svg, "subgraph node_")
	assert.Contains(t, svg, "direction TB")

	// 层级控制：expand=0 不展开子图
	got, err = w.Draw("集成", "mermaid", 0, false)
	require.NoError(t, err)
	assert.NotContains(t, got.(string), "subgraph node_")
}

// TestDrawable关闭与环境等价 env 关闭：Draw 返回空（现状语义回归锁）。
func (s *DrawableSuite) TestDrawable关闭与环境等价() {
	t := s.T()
	t.Setenv("WORKFLOW_DRAWABLE", "")
	w := s.buildWorkflow()
	got, err := w.Draw("", "mermaid", false, false)
	require.NoError(t, err)
	assert.Equal(t, "", got)
	gotPng, err := w.Draw("", "png", false, false)
	require.NoError(t, err)
	assert.Len(t, gotPng.([]byte), 0)
}

// TestDrawablePNG渲染链 png 分支走 httptest mock mermaid.ink 端点（网络边界 mock）。
func (s *DrawableSuite) TestDrawablePNG渲染链() {
	t := s.T()
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = io.WriteString(w, "IMG")
	}))
	defer srv.Close()
	t.Setenv("MERMAID_INK_SERVER", srv.URL)
	t.Setenv("WORKFLOW_DRAWABLE", "true")
	w := s.buildWorkflow()

	got, err := w.Draw("", "png", false, false)
	require.NoError(t, err)
	assert.Equal(t, "IMG", string(got.([]byte)))
	assert.True(t, strings.HasPrefix(gotPath, "/img/"))
}

// 编译期锚点：visualization 包常量消费（保活 import 面）
var _ = visualization.MermaidInkServerEnv
