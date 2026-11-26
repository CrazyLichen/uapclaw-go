//go:build integration

// 8.14b 调查期探针（probe_echo_test.go/probe_test.go，2026-11-22 按规则 7+8 清退）
// 的观察场景转正：保留三条有覆盖价值的链路，改为 suite 用例 + 真实断言。
// 链路跨子系统：workflow.Invoke/Stream → graph 两层调度 → session → components/flow。
package workflow_sub_workflow

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
	iface "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/interface"
	graphexe "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/graph/executable"
	sessionpkg "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/workflow"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/workflow/components"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/workflow/components/ability"
	flow "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/workflow/components/flow"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/workflow/workflowbase"
)

// buildEchoChild 构造子工作流：start(a1=${input}) → echo → end(child:{{answer}})。
func buildEchoChild() *workflow.Workflow {
	child := workflow.NewWorkflow(nil)
	child.SetStartComp("start", flow.NewStart(), map[string]any{"a1": "${input}"}, nil, nil)
	child.AddWorkflowComp("echo", NewEchoComp(), nil, map[string]any{"name": "${start.a1}"}, nil, nil, nil, nil, 0, -1.0, nil, nil)
	end, err := flow.NewEndConfig("child:{{answer}}")
	if err != nil {
		panic(err)
	}
	e, err := flow.NewEnd(end)
	if err != nil {
		panic(err)
	}
	child.SetEndComp("end", e, map[string]any{"answer": "${echo.answer}"}, nil, nil, nil, "batch", nil)
	child.AddConnection([]string{"start"}, "echo")
	child.AddConnection([]string{"echo"}, "end")
	return child
}

// TestInvokeEchoTwoLayers 父 start→sub→end：输入值贯通两层真实嵌套，
// 端到端断言子工作流输出经信封回到父侧 response。
type ProbeBackfillSuite struct {
	suite.Suite
}

func TestProbeBackfillSuite(t *testing.T) {
	suite.Run(t, new(ProbeBackfillSuite))
}

func (s *ProbeBackfillSuite) TestInvokeEchoTwoLayers() {
	parent := workflow.NewWorkflow(nil)
	parent.SetStartComp("start", flow.NewStart(), map[string]any{"b": "${name}"}, nil, nil)
	sub, err := flow.NewSubWorkflowComponent(buildEchoChild())
	s.Require().NoError(err)
	wb := false
	parent.AddWorkflowComp("sub", sub, &wb, map[string]any{"input": "${start.b}"}, nil, nil, nil, nil, 0, -1.0, nil, nil)
	pend, err := flow.NewEndConfig("{{response}}")
	s.Require().NoError(err)
	pe, err := flow.NewEnd(pend)
	s.Require().NoError(err)
	parent.SetEndComp("end", pe, map[string]any{"response": "${sub.response}"}, nil, nil, nil, "batch", nil)
	parent.AddConnection([]string{"start"}, "sub")
	parent.AddConnection([]string{"sub"}, "end")

	sess := sessionpkg.NewSession(sessionpkg.WithEnvs(map[string]any{}))
	out, err := parent.Invoke(context.Background(), map[string]any{"name": "u9"}, sess, nil, nil)
	s.Require().NoError(err)
	s.Require().NotNil(out)
	wo, ok := out.(*workflowbase.WorkflowOutput)
	s.Require().True(ok, "顶层 invoke 应返回 *WorkflowOutput")
	s.Require().Equal(workflowbase.WorkflowExecutionStateCompleted, wo.State)
	response := fmt.Sprint(wo.Result)
	// 子工作流 echo 收到 name=u9，end 模板收敛为 child:...，父侧原样透传。
	s.Require().True(strings.Contains(response, "child:"), "response 应含子工作流 end 模板前缀，实际 %q", response)
	s.Require().True(strings.Contains(response, "u9"), "输入值 u9 应贯通两层嵌套，实际 %q", response)
}

// TestInvokeEnvelopePath GraphInvoker 回显组件替代 sub（Vertex 信封路径）：
// 断言信封语义下父侧仍取到子节点 answer。
func (s *ProbeBackfillSuite) TestInvokeEnvelopePath() {
	parent := workflow.NewWorkflow(nil)
	parent.SetStartComp("start", flow.NewStart(), map[string]any{"b": "${name}"}, nil, nil)
	gi := &graphInvokerEcho{echoComp: NewEchoComp()}
	parent.AddWorkflowComp("sub", gi, nil, map[string]any{"input": "${start.b}"}, nil, nil, nil, nil, 0, -1.0, nil, nil)
	pend, err := flow.NewEndConfig("{{response}}")
	s.Require().NoError(err)
	pe, err := flow.NewEnd(pend)
	s.Require().NoError(err)
	parent.SetEndComp("end", pe, map[string]any{"response": "${sub.answer}"}, nil, nil, nil, "batch", nil)
	parent.AddConnection([]string{"start"}, "sub")
	parent.AddConnection([]string{"sub"}, "end")

	sess := sessionpkg.NewSession(sessionpkg.WithEnvs(map[string]any{}))
	out, err := parent.Invoke(context.Background(), map[string]any{"name": "u9"}, sess, nil, nil)
	s.Require().NoError(err)
	s.Require().NotNil(out)
	wo, ok := out.(*workflowbase.WorkflowOutput)
	s.Require().True(ok, "顶层 invoke 应返回 *WorkflowOutput")
	s.Require().Equal(workflowbase.WorkflowExecutionStateCompleted, wo.State)
	response := fmt.Sprint(wo.Result)
	s.Require().True(strings.Contains(response, "u9"), "信封路径下输入值应到达子节点并回传，实际 %q", response)
}

// TestStreamSingleLayerEdge 单层流边基线（无子工作流）：a(STREAM) --stream--> end(streaming)，
// 流式两帧逐帧贯通到 end。
func (s *ProbeBackfillSuite) TestStreamSingleLayerEdge() {
	w := workflow.NewWorkflow(nil)
	w.SetStartComp("start", flow.NewStart(), map[string]any{"b": "${name}"}, nil, nil)
	wb := true
	w.AddWorkflowComp("a", NewEchoStreamComp(), &wb, map[string]any{"x": "${start.b}"}, nil, nil, nil,
		[]ability.ComponentAbility{ability.ComponentAbilityStream}, 0, -1.0, nil, nil)
	w.SetEndComp("end", probeEnd("{{x}}"), nil, nil,
		map[string]any{"x": "${a.v}"}, nil, "streaming", nil)
	w.AddConnection([]string{"start"}, "a")
	w.AddStreamConnection("a", "end")

	sess := sessionpkg.NewSession(sessionpkg.WithEnvs(map[string]any{}))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ch, err := w.Stream(ctx, map[string]any{"name": "u9"}, sess, nil, nil, nil)
	s.Require().NoError(err)
	var allFrames strings.Builder
	count := 0
	for chunk := range ch {
		count++
		allFrames.WriteString(fmt.Sprint(chunk))
	}
	s.Require().GreaterOrEqual(count, 2, "流式两帧应全部到达 end")
	s.Require().Contains(allFrames.String(), "part1")
	s.Require().Contains(allFrames.String(), "part2")
}

// echoComp 回显组件：把收到的 inputs 原样放 answer 键（原探针件转正）。
type echoComp struct {
	components.WorkflowComponent
}

// NewEchoComp 构造回显组件。
func NewEchoComp() *echoComp {
	c := &echoComp{}
	c.InvokeFn = func(ctx context.Context, inputs map[string]any,
		sess *sessionpkg.NodeSessionFacade, _ iface.ModelContext) (map[string]any, error) {
		return map[string]any{"answer": fmt.Sprint(inputs)}, nil
	}
	return c
}

// graphInvokerEcho 伪装 GraphInvoker 的回显组件（触发 Vertex 信封路径）。
type graphInvokerEcho struct{ *echoComp }

// GraphInvoker 恒 true。
func (g *graphInvokerEcho) GraphInvoker() bool { return true }

// echoStreamComp 流式回显组件（两帧产出）。
type echoStreamComp struct {
	components.WorkflowComponent
}

// NewEchoStreamComp 构造流式回显组件。
func NewEchoStreamComp() *echoStreamComp {
	c := &echoStreamComp{}
	c.StreamFn = func(ctx context.Context, inputs map[string]any,
		sess *sessionpkg.NodeSessionFacade, _ iface.ModelContext) (<-chan graphexe.StreamChunk[any], error) {
		out := make(chan graphexe.StreamChunk[any])
		go func() {
			defer close(out)
			out <- graphexe.StreamChunk[any]{Val: map[string]any{"v": "part1"}}
			out <- graphexe.StreamChunk[any]{Val: map[string]any{"v": "part2"}}
		}()
		return out, nil
	}
	return c
}

// probeEnd 独立 End 构造（ProbeBackfillSuite 不依赖主套件助手）。
func probeEnd(tmpl string) *flow.End {
	end, err := flow.NewEndConfig(tmpl)
	if err != nil {
		panic(err)
	}
	e, err := flow.NewEnd(end)
	if err != nil {
		panic(err)
	}
	return e
}
