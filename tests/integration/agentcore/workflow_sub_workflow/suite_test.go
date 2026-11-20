//go:build integration

// Package workflow_sub_workflow 8.14b SubWorkflowComponent 全链集成测试。
// 真实跨子系统链路：workflow（Invoke/Stream 门面 + subInvoke/subStream）→
// graph（CompiledGraph/Pregel 两层调度 + Vertex graph_invoker 语义）→
// session/stream（ActorManager emitter 队列）→ components/flow（Start/End/SubWorkflow）。
// 对齐 Python: workflow_comp.py 嵌套场景（两层 Workflow 嵌套真实输入输出）。
package workflow_sub_workflow

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
	sessionpkg "github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/workflow"
	flow "github.com/uapclaw/uapclaw-go/internal/agentcore/workflow/components/flow"
)

// SubWorkflowSuite 8.14b 子工作流嵌套全链集成。
type SubWorkflowSuite struct {
	suite.Suite
}

func TestSubWorkflowSuite(t *testing.T) {
	suite.Run(t, new(SubWorkflowSuite))
}

// newEnd 构造 End 组件（模板收敛）。
func (s *SubWorkflowSuite) newEnd(tmpl string) *flow.End {
	end, err := flow.NewEndConfig(tmpl)
	s.Require().NoError(err)
	e, err := flow.NewEnd(end)
	s.Require().NoError(err)
	return e
}

// buildChild 组子工作流：Start → End(batch "child:{{name}}")。
func (s *SubWorkflowSuite) buildChild() *workflow.Workflow {
	w := workflow.NewWorkflow(nil)
	w.SetStartComp("start", flow.NewStart(), nil, nil, nil)
	w.SetEndComp("end", s.newEnd("child:{{name}}"), nil, nil, nil, nil, "batch", nil)
	w.AddConnection([]string{"start"}, "end")
	return w
}

// buildParentBatch 组父工作流（批式）：Start → sub → End(batch "{{response}}")。
// 父 End 模板取 sub 节点输出（= 子工作流 End 渲染结果 {"response": "child:..."}）。
func (s *SubWorkflowSuite) buildParentBatch(child *workflow.Workflow, cacheStream bool) *workflow.Workflow {
	w := workflow.NewWorkflow(nil)
	sub, err := flow.NewSubWorkflowComponent(child, flow.WithCacheStream(cacheStream))
	s.Require().NoError(err)
	w.SetStartComp("start", flow.NewStart(), nil, nil, nil)
	w.AddWorkflowComp("sub", sub, nil, nil, nil, nil, nil, nil, 0, -1.0, nil, nil)
	w.SetEndComp("end", s.newEnd("{{response}}"), nil, nil, nil, nil, "batch", nil)
	w.AddConnection([]string{"start"}, "sub")
	w.AddConnection([]string{"sub"}, "end")
	return w
}

// TestInvoke_两层真实嵌套输入输出贯通 批式：父 invoke 输入经信封 INPUTS_KEY
// 落入子工作流 → 子 End 模板渲染 → sub 节点输出回传 → 父 End 引用渲染。
func (s *SubWorkflowSuite) TestInvoke_两层真实嵌套输入输出贯通() {
	parent := s.buildParentBatch(s.buildChild(), false)
	sess := sessionpkg.NewSession(sessionpkg.WithEnvs(map[string]any{}))
	out, err := parent.Invoke(context.Background(),
		map[string]any{"name": "u1"}, sess, nil, nil)
	s.Require().NoError(err)
	s.Require().NotNil(out)
	s.Equal(workflow.WorkflowExecutionStateCompleted, out.State)
	// 真实输出断言：子工作流渲染产物 "child:u1" 贯穿到父 End
	s.Contains(fmt.Sprint(out.Result), "child:u1")
}

// TestStream_两层帧序列贯通 流式：子工作流帧经 sub_workflow_stream /
// 转发层逐帧到达父侧输出。
func (s *SubWorkflowSuite) TestStream_两层帧序列贯通() {
	child := workflow.NewWorkflow(nil)
	child.SetStartComp("start", flow.NewStart(), nil, nil, nil)
	child.SetEndComp("end", s.newEnd("hello {{name}}"), nil, nil, nil, nil, "streaming", nil)
	child.AddConnection([]string{"start"}, "end")

	w := workflow.NewWorkflow(nil)
	sub, err := flow.NewSubWorkflowComponent(child)
	s.Require().NoError(err)
	w.SetStartComp("start", flow.NewStart(), nil, nil, nil)
	w.AddWorkflowComp("sub", sub, nil, nil, nil, nil, nil, nil, 0, -1.0, nil, nil)
	w.SetEndComp("end", s.newEnd("{{response}}"), nil, nil, nil, nil, "streaming", nil)
	w.AddConnection([]string{"start"}, "sub")
	w.AddConnection([]string{"sub"}, "end")

	sess := sessionpkg.NewSession(sessionpkg.WithEnvs(map[string]any{}))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ch, err := w.Stream(ctx, map[string]any{"name": "u3"}, sess, nil, nil, nil)
	s.Require().NoError(err)
	var count int
	sawChildPayload := false
	for chunk := range ch {
		count++
		if fmt.Sprint(chunk) != "" && contains(fmt.Sprint(chunk), "hello") {
			sawChildPayload = true
		}
	}
	s.GreaterOrEqual(count, 1)
	s.True(sawChildPayload, "帧序列应含子工作流渲染内容 hello（真实输入贯通）")
}

// TestCallback_触发序列断言（8.14 回填②）：真实链路断言组件事件触发顺序。
func (s *SubWorkflowSuite) TestCallback_触发序列断言() {
	parent := s.buildParentBatch(s.buildChild(), false)
	sess := sessionpkg.NewSession(sessionpkg.WithEnvs(map[string]any{}))

	fw := callback.GetCallbackFramework()
	var seq []callback.WorkflowEventType
	var mu sync.Mutex
	collect := func(ev callback.WorkflowEventType) {
		fw.OnWorkflow(ev, func(ctx context.Context, data *callback.WorkflowEventData) any {
			mu.Lock()
			defer mu.Unlock()
			seq = append(seq, data.Event)
			return nil
		})
	}
	collect(callback.ComponentBatchInput)
	collect(callback.ComponentBatchOutput)

	_, err := parent.Invoke(context.Background(),
		map[string]any{"name": "u1"}, sess, nil, nil)
	s.Require().NoError(err)

	mu.Lock()
	defer mu.Unlock()
	// 真实链路：组件事件成对触发（sub 节点 + 两层 End，各产生 Input→Output 对）
	s.GreaterOrEqual(len(seq), 2)
	s.Equal(callback.ComponentBatchInput, seq[0])
	s.Equal(callback.ComponentBatchOutput, seq[len(seq)-1])
}

// contains 子串判断（不引 strings 依赖保持 suite 轻）。
func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}
