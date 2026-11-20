//go:build integration

package workflow_sub_workflow

import (
	"context"
	"fmt"
	"testing"
	"time"

	graphexe "github.com/uapclaw/uapclaw-go/internal/agentcore/graph/executable"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/workflow/components/ability"

	sessionpkg "github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/workflow"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/workflow/components"
	flow "github.com/uapclaw/uapclaw-go/internal/agentcore/workflow/components/flow"
)

// echoComp 回显组件：把收到的 inputs 原样放 answer 键（父→子传递链定位用）
type echoComp struct {
	components.WorkflowComponent
}

// NewEchoComp 构造回显组件
func NewEchoComp() *echoComp {
	c := &echoComp{}
	c.InvokeFn = func(ctx context.Context, inputs map[string]any,
		sess *sessionpkg.NodeSessionFacade) (map[string]any, error) {
		return map[string]any{"answer": fmt.Sprint(inputs)}, nil
	}
	return c
}

// TestProbeEcho_子内回显 子工作流 start→echo→end，观察 start.a1 是否有值
func TestProbeEcho_子内回显(t *testing.T) {
	child := workflow.NewWorkflow(nil)
	child.SetStartComp("start", flow.NewStart(), map[string]any{"a1": "${input}"}, nil, nil)
	child.AddWorkflowComp("echo", NewEchoComp(), nil, map[string]any{"name": "${start.a1}"}, nil, nil, nil, nil, 0, -1.0, nil, nil)
	end, _ := flow.NewEndConfig("child:{{answer}}")
	e, _ := flow.NewEnd(end)
	child.SetEndComp("end", e, map[string]any{"answer": "${echo.answer}"}, nil, nil, nil, "batch", nil)
	child.AddConnection([]string{"start"}, "echo")
	child.AddConnection([]string{"echo"}, "end")

	sess := sessionpkg.NewSession(sessionpkg.WithEnvs(map[string]any{}))
	out, err := child.Invoke(context.Background(), map[string]any{"input": "u9"}, sess, nil, nil)
	fmt.Printf("ECHO-CHILD out=%+v err=%v\n", out, err)
}

// TestProbeEcho_两层回显 父 start→sub→end，观察父侧 sub 节点收到的信封
func TestProbeEcho_两层回显(t *testing.T) {
	child := workflow.NewWorkflow(nil)
	child.SetStartComp("start", flow.NewStart(), map[string]any{"a1": "${input}"}, nil, nil)
	child.AddWorkflowComp("echo", NewEchoComp(), nil, map[string]any{"name": "${start.a1}"}, nil, nil, nil, nil, 0, -1.0, nil, nil)
	end, _ := flow.NewEndConfig("child:{{answer}}")
	e, _ := flow.NewEnd(end)
	child.SetEndComp("end", e, map[string]any{"answer": "${echo.answer}"}, nil, nil, nil, "batch", nil)
	child.AddConnection([]string{"start"}, "echo")
	child.AddConnection([]string{"echo"}, "end")

	parent := workflow.NewWorkflow(nil)
	sub, err := flow.NewSubWorkflowComponent(child)
	if err != nil {
		t.Fatal(err)
	}
	parent.SetStartComp("start", flow.NewStart(), map[string]any{"b": "${name}"}, nil, nil)
	wb := false
	parent.AddWorkflowComp("sub", sub, &wb, map[string]any{"input": "${start.b}"}, nil, nil, nil, nil, 0, -1.0, nil, nil)
	pend, _ := flow.NewEndConfig("{{response}}")
	pe, _ := flow.NewEnd(pend)
	parent.SetEndComp("end", pe, map[string]any{"response": "${sub.response}"}, nil, nil, nil, "batch", nil)
	parent.AddConnection([]string{"start"}, "sub")
	parent.AddConnection([]string{"sub"}, "end")

	sess := sessionpkg.NewSession(sessionpkg.WithEnvs(map[string]any{}))
	out, err := parent.Invoke(context.Background(), map[string]any{"name": "u9"}, sess, nil, nil)
	fmt.Printf("ECHO-PARENT out=%+v err=%v\n", out, err)
}

// TestProbeEcho_父侧单层 父 start→echo（不经 sub），验证 ${start.b} 解析
func TestProbeEcho_父侧单层(t *testing.T) {
	parent := workflow.NewWorkflow(nil)
	parent.SetStartComp("start", flow.NewStart(), map[string]any{"b": "${name}"}, nil, nil)
	parent.AddWorkflowComp("echo", NewEchoComp(), nil, map[string]any{"input": "${start.b}"}, nil, nil, nil, nil, 0, -1.0, nil, nil)
	pend, _ := flow.NewEndConfig("{{response}}")
	pe, _ := flow.NewEnd(pend)
	parent.SetEndComp("end", pe, map[string]any{"response": "${echo.answer}"}, nil, nil, nil, "batch", nil)
	parent.AddConnection([]string{"start"}, "echo")
	parent.AddConnection([]string{"echo"}, "end")

	sess := sessionpkg.NewSession(sessionpkg.WithEnvs(map[string]any{}))
	out, err := parent.Invoke(context.Background(), map[string]any{"name": "u9"}, sess, nil, nil)
	fmt.Printf("ECHO-PARENT-SINGLE out=%+v err=%v\n", out, err)
}

// graphInvokerEcho 伪装 GraphInvoker 的回显组件
type graphInvokerEcho struct{ *echoComp }

// GraphInvoker 恒 true（触发 Vertex isSubgraph 信封路径）
func (g *graphInvokerEcho) GraphInvoker() bool { return true }

// TestProbeEcho_信封路径 用 GraphInvoker 回显组件替代 sub，观察信封与 batchInputs
func TestProbeEcho_信封路径(t *testing.T) {
	parent := workflow.NewWorkflow(nil)
	parent.SetStartComp("start", flow.NewStart(), map[string]any{"b": "${name}"}, nil, nil)
	gi := &graphInvokerEcho{echoComp: NewEchoComp()}
	parent.AddWorkflowComp("sub", gi, nil, map[string]any{"input": "${start.b}"}, nil, nil, nil, nil, 0, -1.0, nil, nil)
	pend, _ := flow.NewEndConfig("{{response}}")
	pe, _ := flow.NewEnd(pend)
	parent.SetEndComp("end", pe, map[string]any{"response": "${sub.answer}"}, nil, nil, nil, "batch", nil)
	parent.AddConnection([]string{"start"}, "sub")
	parent.AddConnection([]string{"sub"}, "end")

	sess := sessionpkg.NewSession(sessionpkg.WithEnvs(map[string]any{}))
	out, err := parent.Invoke(context.Background(), map[string]any{"name": "u9"}, sess, nil, nil)
	fmt.Printf("ECHO-ENVELOPE out=%+v err=%v\n", out, err)
}

// TestProbeEcho_两层Start直引 子 end 直接引 ${start.a1}（对齐 PROBE4 形态），
// 并打印 start 输出（经 echo 二跳转发以观察）
func TestProbeEcho_两层Start直引(t *testing.T) {
	child := workflow.NewWorkflow(nil)
	child.SetStartComp("start", flow.NewStart(), map[string]any{"a1": "${input}"}, nil, nil)
	child.AddWorkflowComp("echo", NewEchoComp(), nil, map[string]any{"name": "${start.a1}"}, nil, nil, nil, nil, 0, -1.0, nil, nil)
	end, _ := flow.NewEndConfig("child:{{answer}}")
	e, _ := flow.NewEnd(end)
	child.SetEndComp("end", e, map[string]any{"answer": "${start.a1}"}, nil, nil, nil, "batch", nil)
	child.AddConnection([]string{"start"}, "echo")
	child.AddConnection([]string{"echo"}, "end")

	parent := workflow.NewWorkflow(nil)
	sub, err := flow.NewSubWorkflowComponent(child)
	if err != nil {
		t.Fatal(err)
	}
	parent.SetStartComp("start", flow.NewStart(), map[string]any{"b": "${name}"}, nil, nil)
	wb := false
	parent.AddWorkflowComp("sub", sub, &wb, map[string]any{"input": "${start.b}"}, nil, nil, nil, nil, 0, -1.0, nil, nil)
	pend, _ := flow.NewEndConfig("{{response}}")
	pe, _ := flow.NewEnd(pend)
	parent.SetEndComp("end", pe, map[string]any{"response": "${sub.response}"}, nil, nil, nil, "batch", nil)
	parent.AddConnection([]string{"start"}, "sub")
	parent.AddConnection([]string{"sub"}, "end")

	sess := sessionpkg.NewSession(sessionpkg.WithEnvs(map[string]any{}))
	out, err := parent.Invoke(context.Background(), map[string]any{"name": "u9"}, sess, nil, nil)
	fmt.Printf("ECHO-STARTREF out=%+v err=%v\n", out, err)
}

// echoStreamComp 流式回显组件（两帧产出）
type echoStreamComp struct {
	components.WorkflowComponent
}

// NewEchoStreamComp 构造
func NewEchoStreamComp() *echoStreamComp {
	c := &echoStreamComp{}
	c.StreamFn = func(ctx context.Context, inputs map[string]any,
		sess *sessionpkg.NodeSessionFacade) (<-chan graphexe.StreamChunk[map[string]any], error) {
		out := make(chan graphexe.StreamChunk[map[string]any])
		go func() {
			defer close(out)
			out <- graphexe.StreamChunk[map[string]any]{Val: map[string]any{"v": "part1"}}
			out <- graphexe.StreamChunk[map[string]any]{Val: map[string]any{"v": "part2"}}
		}()
		return out, nil
	}
	return c
}

// TestProbeEcho_单层流边 无子工作流的单层流边基线：a(STREAM) --stream--> end(streaming)
func TestProbeEcho_单层流边(t *testing.T) {
	w := workflow.NewWorkflow(nil)
	w.SetStartComp("start", flow.NewStart(), map[string]any{"b": "${name}"}, nil, nil)
	wb := true
	w.AddWorkflowComp("a", NewEchoStreamComp(), &wb, map[string]any{"x": "${start.b}"}, nil, nil, nil,
		[]ability.ComponentAbility{ability.ComponentAbilityStream}, 0, -1.0, nil, nil)
	w.SetEndComp("end", s_probeEnd(t, "{{x}}"), nil, nil,
		map[string]any{"x": "${a.v}"}, nil, "streaming", nil)
	w.AddConnection([]string{"start"}, "a")
	w.AddStreamConnection("a", "end")

	sess := sessionpkg.NewSession(sessionpkg.WithEnvs(map[string]any{}))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ch, err := w.Stream(ctx, map[string]any{"name": "u9"}, sess, nil, nil, nil)
	if err != nil {
		t.Fatalf("stream 启动失败: %v", err)
	}
	n := 0
	for chunk := range ch {
		n++
		fmt.Println("SINGLE-STREAM frame", n, chunk)
	}
	fmt.Println("SINGLE-STREAM done frames=", n)
}

func s_probeEnd(t *testing.T, tmpl string) *flow.End {
	end, err := flow.NewEndConfig(tmpl)
	if err != nil {
		t.Fatal(err)
	}
	e, err := flow.NewEnd(end)
	if err != nil {
		t.Fatal(err)
	}
	return e
}
