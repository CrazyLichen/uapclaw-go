//go:build integration

// Package workflow_sub_workflow 8.14b SubWorkflowComponent 全链集成测试。
// 真实跨子系统链路：workflow（Invoke/Stream 门面 + subInvoke/subStream）→
// graph（CompiledGraph/Pregel 两层调度 + Vertex graph_invoker 语义）→
// session/stream（ActorManager emitter 队列）→ components/flow（Start/End/SubWorkflow）。
// 对齐 Python: workflow_comp.py 嵌套场景（两层 Workflow 嵌套真实输入输出）。
package workflow_sub_workflow

import (
	"context"
	"github.com/google/uuid"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	sessionpkg "github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interaction"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/workflow"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/workflow/components"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/workflow/components/ability"
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

// buildChild 组子工作流（Python test_workflow.py:836-864 官方接线习惯）：
// Start(inputs_schema {a1:${input}}) → End(batch "child:{{result}}", {result:${start.a1}})。
// ${input} 引用子工作流入口载荷键 "input"（父侧 sub 节点 inputs_schema 注入）。
func (s *SubWorkflowSuite) buildChild() *workflow.Workflow {
	w := workflow.NewWorkflow(nil)
	w.SetStartComp("start", flow.NewStart(), map[string]any{"a1": "${input}"}, nil, nil)
	w.SetEndComp("end", s.newEnd("child:{{result}}"),
		map[string]any{"result": "${start.a1}"}, nil, nil, nil, "batch", nil)
	w.AddConnection([]string{"start"}, "end")
	return w
}

// buildParentBatch 组父工作流（批式，官方接线）：
// Start({b:${name}}) → sub({input:${start.b}}) → End(batch "{{response}}", {response:${sub.response}})。
func (s *SubWorkflowSuite) buildParentBatch(child *workflow.Workflow, cacheStream bool) *workflow.Workflow {
	w := workflow.NewWorkflow(nil)
	sub, err := flow.NewSubWorkflowComponent(child, flow.WithCacheStream(cacheStream))
	s.Require().NoError(err)
	w.SetStartComp("start", flow.NewStart(), map[string]any{"b": "${name}"}, nil, nil)
	wb := false
	w.AddWorkflowComp("sub", sub, &wb, map[string]any{"input": "${start.b}"}, nil, nil, nil, nil, 0, -1.0, nil, nil)
	w.SetEndComp("end", s.newEnd("{{response}}"),
		map[string]any{"response": "${sub.response}"}, nil, nil, nil, "batch", nil)
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
	wo, ok := out.(*workflow.WorkflowOutput)
	s.Require().True(ok, "顶层 invoke 应返回 *WorkflowOutput")
	s.Equal(workflow.WorkflowExecutionStateCompleted, wo.State)
	// 真实输出断言：子工作流渲染产物 "child:u1" 贯穿到父 End
	s.Contains(fmt.Sprint(wo.Result), "child:u1")
}

// TestStream_两层帧序列贯通 流式：子工作流帧经 sub_workflow_stream /
// 转发层逐帧到达父侧输出。
func (s *SubWorkflowSuite) TestStream_两层帧序列贯通() {
	child := workflow.NewWorkflow(nil)
	child.SetStartComp("start", flow.NewStart(), map[string]any{"a1": "${input}"}, nil, nil)
	child.SetEndComp("end", s.newEnd("hello {{result}}"),
		map[string]any{"result": "${start.a1}"}, nil, nil, nil, "streaming", nil)
	child.AddConnection([]string{"start"}, "end")

	w := workflow.NewWorkflow(nil)
	sub, err := flow.NewSubWorkflowComponent(child)
	s.Require().NoError(err)
	w.SetStartComp("start", flow.NewStart(), map[string]any{"b": "${name}"}, nil, nil)
	// Python test_stream_comp_workflow:871-886 官方惯例：流式生产节点显式
	// comp_ability=[STREAM] + wait_for_all=True + **流边 add_stream_connection**
	//（流帧经流边 actor 队列路由到流式消费方；缺流边则 End 流输入无源永不产出）
	wb := true
	w.AddWorkflowComp("sub", sub, &wb, map[string]any{"input": "${start.b}"}, nil, nil, nil,
		[]ability.ComponentAbility{ability.ComponentAbilityStream}, 0, -1.0, nil, nil)
	// 流式 End：流式引用走 streamInputsSchema（Python 官方 b 节点同款）
	w.SetEndComp("end", s.newEnd("{{response}}"), nil, nil,
		map[string]any{"response": "${sub.response}"}, nil, "streaming", nil)
	w.AddConnection([]string{"start"}, "sub")
	w.AddStreamConnection("sub", "end")

	sess := sessionpkg.NewSession(sessionpkg.WithEnvs(map[string]any{}))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ch, err := w.Stream(ctx, map[string]any{"name": "u3"}, sess, nil, nil, nil)
	s.Require().NoError(err)
	// 子 End 流式渲染为分帧形态（静态前缀与 ${result} 引用分帧，8.14 单层同款）：
	// 帧序拼接后含 "hello" 与 "u3" 即输入贯通（非空渲染假阳排除）
	var count int
	var allFrames strings.Builder
	for chunk := range ch {
		count++
		allFrames.WriteString(fmt.Sprint(chunk))
	}
	s.GreaterOrEqual(count, 2)
	s.Contains(allFrames.String(), "hello", "帧序列应含子 End 渲染前缀")
	s.Contains(allFrames.String(), "u3", "帧序列应含真实输入渲染值（输入键贯通）")
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

// ──────────────────────────── Task 7：cache 短路 + 中断恢复 ────────────────────────────

// interruptGateComp 测试中断源组件：对齐上游 Node4Cp 真实形态
//（test_workflow_with_interrupt.py:123-133——首次执行抛普通执行错误，
// checkpointer 落图侧快照；InteractiveInput 恢复后二次执行放行）。
// 全链路真实：错误经 Vertex→Pregel→checkpointer 快照→恢复重放，不发明机制。
type interruptGateComp struct {
	components.WorkflowComponent
	// calls 调用计数（首次报错、恢复后放行）
	calls atomic.Int32
	// lastInputs 捕获最近一次真实输入（恢复场景输入贯通断言用）
	lastInputs map[string]any
}

// NewInterruptGateComp 构造中断门组件。
func NewInterruptGateComp() *interruptGateComp {
	c := &interruptGateComp{}
	c.InvokeFn = func(ctx context.Context, inputs map[string]any,
		sess *sessionpkg.NodeSessionFacade) (map[string]any, error) {
		n := c.calls.Add(1)
		c.lastInputs = inputs
		if n == 1 {
			return nil, exception.BuildError(exception.StatusWorkflowComponentExecutionError,
				exception.WithParam("comp", "gate"),
				exception.WithParam("reason", "value < 20"))
		}
		return map[string]any{"answer": "resumed:" + fmt.Sprint(inputs["name"])}, nil
	}
	return c
}

// buildChildWithGate 子工作流（官方接线）：
// Start({a1:${input}}) → gate({name:${start.a1}}) → End(batch "child-resumed:{{answer}}", {answer:${gate.answer}})。
func (s *SubWorkflowSuite) buildChildWithGate(gate *interruptGateComp) *workflow.Workflow {
	w := workflow.NewWorkflow(nil)
	w.SetStartComp("start", flow.NewStart(), map[string]any{"a1": "${input}"}, nil, nil)
	w.AddWorkflowComp("gate", gate, nil, map[string]any{"name": "${start.a1}"}, nil, nil, nil, nil, 0, -1.0, nil, nil)
	w.SetEndComp("end", s.newEnd("child-resumed:{{answer}}"),
		map[string]any{"answer": "${gate.answer}"}, nil, nil, nil, "batch", nil)
	w.AddConnection([]string{"start"}, "gate")
	w.AddConnection([]string{"gate"}, "end")
	return w
}

// buildParentSubWith 父工作流（官方接线）：
// Start({b:${name}}) → sub({input:${start.b}}) → End(batch "{{response}}", {response:${sub.response}})。
func (s *SubWorkflowSuite) buildParentSubWith(child *workflow.Workflow, cacheStream bool) *workflow.Workflow {
	w := workflow.NewWorkflow(nil)
	sub, err := flow.NewSubWorkflowComponent(child, flow.WithCacheStream(cacheStream))
	s.Require().NoError(err)
	w.SetStartComp("start", flow.NewStart(), map[string]any{"b": "${name}"}, nil, nil)
	wb := false
	w.AddWorkflowComp("sub", sub, &wb, map[string]any{"input": "${start.b}"}, nil, nil, nil, nil, 0, -1.0, nil, nil)
	w.SetEndComp("end", s.newEnd("{{response}}"),
		map[string]any{"response": "${sub.response}"}, nil, nil, nil, "batch", nil)
	w.AddConnection([]string{"start"}, "sub")
	w.AddConnection([]string{"sub"}, "end")
	return w
}

// TestCacheStream_流后批式短路不重跑 先流式跑完（缓存积累）→ 再批式 invoke 同组件
// → 断言子工作流未重跑（child End 组件事件零触发）且返回合并缓存。
func (s *SubWorkflowSuite) TestCacheStream_流后批式短路不重跑() {
	child := workflow.NewWorkflow(nil)
	child.SetStartComp("start", flow.NewStart(), map[string]any{"a1": "${input}"}, nil, nil)
	childEnd := s.newEnd("child:{{result}}")
	child.SetEndComp("end", childEnd,
		map[string]any{"result": "${start.a1}"}, nil, nil, nil, "streaming", nil)
	child.AddConnection([]string{"start"}, "end")

	parent := s.buildParentSubWith(child, true)

	// 子 End 组件事件计数器：重跑则 ComponentBatchInput 再触发
	var childEndEvents atomic.Int32
	fw := callback.GetCallbackFramework()
	fw.OnWorkflow(callback.ComponentBatchInput, func(ctx context.Context,
		data *callback.WorkflowEventData) any {
		if data.NodeID == "end" {
			childEndEvents.Add(1)
		}
		return nil
	})

	sess := sessionpkg.NewSession(sessionpkg.WithEnvs(map[string]any{}))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	// ① 流式跑完（子工作流帧累积进缓存）
	ch, err := parent.Stream(ctx, map[string]any{"name": "u5"}, sess, nil, nil, nil)
	s.Require().NoError(err)
	streamed := 0
	for chunk := range ch {
		streamed++
		s.T().Log("frame", streamed, chunk)
	}
	s.Require().Greater(streamed, 0)
	firstRound := childEndEvents.Load()
	s.Require().Greater(firstRound, int32(0), "流式运行应触发子 End 组件事件")

	// ② 批式 invoke：vertex.call 双腿循环（INVOKE→STREAM，vertex.py:669-673）——
	// INVOKE 腿被 Quirk A 短路（cache_stream 开启即短路，含空缓存 {} 形态），
	// STREAM 腿照常重跑子图（Python 同款双腿语义，短路只护批入腿）
	out, err := parent.Invoke(context.Background(), map[string]any{"name": "u5"}, sess, nil, nil)
	s.Require().NoError(err)
	s.Require().NotNil(out)
	s.Greater(childEndEvents.Load(), firstRound, "STREAM 腿照常重跑子图（对齐 Python 双腿循环）")
	s.NotContains(fmt.Sprint(out), "child:u5",
		"Quirk A 复刻：INVOKE 腿短路返回空缓存合并结果 {}，父 End 批式渲染不含真实子输出（上游 workflow_comp.py:81-84 真实行为）")
}

// TestInterruptResume_真实输入输出 恢复全链（对齐上游 test_workflow_with_interrupt.py
// test_workflow_comp 真实形态）：首次执行 gate 抛错（真实输入贯通到出错点）→
// checkpointer 落图侧快照 → 同 session_id 以 InteractiveInput 恢复 → gate 二次
// 执行放行 → 真实输出贯通到父 End（state COMPLETED）。用户明确要求真实输入输出。
func (s *SubWorkflowSuite) TestInterruptResume_真实输入输出() {
	gate := NewInterruptGateComp()
	child := s.buildChildWithGate(gate)
	parent := s.buildParentSubWith(child, false)

	sid := uuid.New().String()
	sess := sessionpkg.NewSession(sessionpkg.WithSessionID(sid), sessionpkg.WithEnvs(map[string]any{}))
	// ① 首次运行：真实输入 {"name": "u7"} 进入，gate 首调报错
	_, err1 := parent.Invoke(context.Background(), map[string]any{"name": "u7"}, sess, nil, nil)
	s.Require().Error(err1, "首次运行应报错（上游 Node4Cp 形态）")
	s.Require().Equal(int32(1), gate.calls.Load(), "gate 只执行一次（报错点）")
	s.Require().Equal("u7", gate.lastInputs["name"], "真实输入应贯通到出错点（子工作流信封 INPUTS_KEY）")

	// ② 恢复：同 session_id 以 InteractiveInput 真实重调（上游 :181 形态）
	ii, iiErr := interaction.NewInteractiveInput()
	s.Require().NoError(iiErr)
	out2, err2 := parent.Invoke(context.Background(), ii, sess, nil, nil)
	s.Require().NoError(err2)
	s.Require().NotNil(out2)
	s.Contains(fmt.Sprint(out2), "child-resumed:resumed:u7",
		"恢复后最终输出完整（gate 放行真实输出贯通到父 End，含首调输入值）")
	s.Require().Equal(int32(2), gate.calls.Load(), "恢复时 gate 第二次执行放行")
	s.Require().Equal("u7", gate.lastInputs["name"], "恢复重放仍携带真实输入")
}

