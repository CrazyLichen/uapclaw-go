//go:build integration

package workflow_sub_workflow

import (
	"context"
	"fmt"
	"testing"
	"time"

	sessionpkg "github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/workflow"
	flow "github.com/uapclaw/uapclaw-go/internal/agentcore/workflow/components/flow"
)

// Probe：按 Python test_workflow.py:836-864 官方嵌套装配习惯（全节点显式 inputs_schema ${ref} 引用）
func TestProbe_批式子_批式父(t *testing.T) {
	// 子工作流：start(inputs_schema {a1:${input}}) → end({{result}}, {result:${start.a1}})
	child := workflow.NewWorkflow(nil)
	child.SetStartComp("start", flow.NewStart(), map[string]any{"a1": "${input}"}, nil, nil)
	end, err := flow.NewEndConfig("child:{{result}}")
	if err != nil {
		t.Fatal(err)
	}
	e, err := flow.NewEnd(end)
	if err != nil {
		t.Fatal(err)
	}
	child.SetEndComp("end", e, map[string]any{"result": "${start.a1}"}, nil, nil, nil, "batch", nil)
	child.AddConnection([]string{"start"}, "end")

	// 父工作流：start({b:${input}}) → sub({input:${start.b}}) → end({{response}}, {response:${sub.response}})
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
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := parent.Invoke(ctx, map[string]any{"name": "u9"}, sess, nil, nil)
	fmt.Printf("PROBE4 out=%+v err=%v\n", out, err)
	if err != nil {
		t.Fatalf("批式嵌套应成功: %v", err)
	}
	if !contains(fmt.Sprint(out), "child:u9") {
		t.Fatalf("期望输出含 child:u9, got %v", out)
	}
}

// TestProbe_子工作流直调 schema 引用链单层验证
func TestProbe_子工作流直调(t *testing.T) {
	child := workflow.NewWorkflow(nil)
	child.SetStartComp("start", flow.NewStart(), map[string]any{"a1": "${input}"}, nil, nil)
	end, err := flow.NewEndConfig("child:{{result}}")
	if err != nil {
		t.Fatal(err)
	}
	e, err := flow.NewEnd(end)
	if err != nil {
		t.Fatal(err)
	}
	child.SetEndComp("end", e, map[string]any{"result": "${start.a1}"}, nil, nil, nil, "batch", nil)
	child.AddConnection([]string{"start"}, "end")
	sess := sessionpkg.NewSession(sessionpkg.WithEnvs(map[string]any{}))
	out, err := child.Invoke(context.Background(), map[string]any{"input": "u9"}, sess, nil, nil)
	fmt.Printf("PROBE3 child out=%+v err=%v\n", out, err)
}
