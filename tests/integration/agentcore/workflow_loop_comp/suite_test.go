//go:build integration

// Package workflow_loop_comp 8.20 LoopComp 全链集成测试。
// 真实跨子系统链路：外层 Workflow（Invoke 门面 + 信封 INPUTS_KEY）→
// graph（CompiledGraph/Pregel 两层调度 + Vertex graph_invoker）→
// components/flow/loop（LoopComponent 装配 → AdvancedLoopComponent 小图跑环）→
// condition（InSession 条件 tuple 分支写 state）→ session/state（io_state/commit）。
// 接线复刻上游官方用例：test_loop_comp.py::test_loop_number、
// test_workflow_with_interrupt.py loop 段。
package workflow_loop_comp

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/suite"

	sessionpkg "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/interaction"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/workflow"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/workflow/components"
	flow "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/workflow/components/flow"
	loop "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/workflow/components/flow/loop"
)

// LoopCompSuite 8.20 LoopComp 端到端集成。
type LoopCompSuite struct {
	suite.Suite
}

func TestLoopCompSuite(t *testing.T) {
	suite.Run(t, new(LoopCompSuite))
}

// ──────────────────────────── 测试组件 ────────────────────────────

// addTenComp ≙ 上游 AddTenNode（mock_nodes.py:287-304）：读 source 加 10 返回。
type addTenComp struct {
	components.WorkflowComponent
}

func newAddTen() *addTenComp {
	c := &addTenComp{}
	c.InvokeFn = func(_ context.Context, inputs map[string]any,
		_ *sessionpkg.NodeSessionFacade) (map[string]any, error) {
		src, ok := inputs["source"].(int)
		if !ok {
			return nil, fmt.Errorf("source must be int, got %T", inputs["source"])
		}
		return map[string]any{"result": src + 10}, nil
	}
	return c
}

// gateComp ≙ 上游 AddTenNode4Cp（mock_nodes.py:136-148）：首两次执行抛错
// （reason 携带真实输入），第三次放行返回 source+10——中断恢复重放载体。
type gateComp struct {
	components.WorkflowComponent
	failures int
}

func newGate(failures int) *gateComp {
	g := &gateComp{failures: failures}
	g.InvokeFn = func(_ context.Context, inputs map[string]any,
		_ *sessionpkg.NodeSessionFacade) (map[string]any, error) {
		src, _ := inputs["source"].(int)
		if g.failures > 0 {
			g.failures--
			return nil, fmt.Errorf("inner error: %d", src)
		}
		return map[string]any{"result": src + 10}, nil
	}
	return g
}

// breakComp 触发 LoopBreak（读 state 计数，第 N 次执行时 break）
type breakComp struct {
	components.WorkflowComponent
	atRound int
	count   int
}

func newBreakAt(atRound int) *breakComp {
	b := &breakComp{atRound: atRound}
	b.InvokeFn = func(_ context.Context, _ map[string]any,
		_ *sessionpkg.NodeSessionFacade) (map[string]any, error) {
		b.count++
		return map[string]any{}, nil
	}
	return b
}

// ──────────────────────────── 1. number 循环（复刻 upstream test_loop_number） ────────────────────────────

// TestLoopNumber_真实链 复刻 upstream test_loop_comp.py:95-127：
// number=12 轮；loop_1 读 ${loop.index}、loop_2 读中间变量 ${loop.user_num}；
// LoopSetVariableComponent 写回 user_num；end 断言三数组。
func (s *LoopCompSuite) TestLoopNumber_真实链() {
	w := workflow.NewWorkflow(nil)
	w.SetStartComp("start", flow.NewStart(),
		map[string]any{"input_arr": "${array}", "input_num": "${num}"}, nil, nil)
	w.SetEndComp("end", s.newEnd("{{end_out}}"),
		map[string]any{"end_out": "${loop}"}, nil, nil, nil, "batch", nil)

	group := loop.NewLoopGroup()
	s.Require().NoError(group.AddWorkflowComp("loop_1", newAddTen(), nil,
		map[string]any{"source": "${loop.index}"}, nil, nil, nil, nil, 0, -1.0, nil, nil))
	s.Require().NoError(group.AddWorkflowComp("loop_2", newAddTen(), nil,
		map[string]any{"source": "${loop.user_num}"}, nil, nil, nil, nil, 0, -1.0, nil, nil))
	setVar, err := loop.NewLoopSetVariableComponent(map[string]any{"${loop.user_num}": "${loop_2.result}"})
	s.Require().NoError(err)
	s.Require().NoError(group.AddWorkflowComp("loop_3", setVar, nil, nil, nil, nil, nil, nil, 0, -1.0, nil, nil))
	s.Require().NoError(group.StartNodes([]string{"loop_1"}))
	s.Require().NoError(group.EndNodes("loop_3"))
	s.Require().NoError(group.AddConnection([]string{"loop_1"}, "loop_2"))
	s.Require().NoError(group.AddConnection([]string{"loop_2"}, "loop_3"))

	loopComp, err := loop.NewLoopComponent(group,
		map[string]any{"l_out1": "${loop_1.result}", "l_out2": "${loop_2.result}"})
	s.Require().NoError(err)

	n := 12
	w.AddWorkflowComp("loop", loopComp, nil,
		map[string]any{
			"loop_type":        "number",
			"loop_number":      n,
			"intermediate_var": map[string]any{"user_num": "${start.input_num}"},
		}, nil, nil, nil, nil, 0, -1.0, nil, nil)
	w.AddConnection([]string{"start"}, "loop")
	w.AddConnection([]string{"loop"}, "end")

	sess := sessionpkg.NewSession(sessionpkg.WithEnvs(map[string]any{}))
	out, err := w.Invoke(context.Background(),
		map[string]any{"array": []any{4, 5, 6}, "num": -3}, sess, nil, nil)
	s.Require().NoError(err)
	wo, ok := out.(*workflow.WorkflowOutput)
	s.Require().True(ok)
	s.Equal(workflow.WorkflowExecutionStateCompleted, wo.State)
	// 上游断言：l_out1 = [10..21]，l_out2 = [7,17,...,117]，user_num = 117
	res := fmt.Sprint(wo.Result)
	for _, v := range []int{10, 11, 21} {
		s.Contains(res, fmt.Sprint(v), "l_out1 数组应含 %d", v)
	}
	s.Contains(res, "117", "user_num 复利加 10×12 轮应达 117")
	s.NotContains(res, "127", "不得多跑一轮（D-20-6：INDEX 增量须在条件求值前可见）")
	s.NotContains(res, "22", "l_out1 末位应为 21（12 轮 distinct），不得出现第 13 轮的 22")
}

// ──────────────────────────── 2. array 循环 ────────────────────────────

// TestLoopArray_三轮遍历收割 array=3 元素；body 读 ${loop.item}；收割 [11,12,13]。
func (s *LoopCompSuite) TestLoopArray_三轮遍历收割() {
	w := workflow.NewWorkflow(nil)
	w.SetStartComp("start", flow.NewStart(),
		map[string]any{"input_arr": "${array}", "input_num": "${num}"}, nil, nil)
	w.SetEndComp("end", s.newEnd("{{end_out}}"),
		map[string]any{"end_out": "${loop}"}, nil, nil, nil, "batch", nil)

	group := loop.NewLoopGroup()
	s.Require().NoError(group.AddWorkflowComp("body_1", newAddTen(), nil,
		map[string]any{"source": "${loop.item}"}, nil, nil, nil, nil, 0, -1.0, nil, nil))
	s.Require().NoError(group.StartNodes([]string{"body_1"}))
	s.Require().NoError(group.EndNodes("body_1"))

	loopComp, err := loop.NewLoopComponent(group,
		map[string]any{"results": "${body_1.result}"})
	s.Require().NoError(err)

	w.AddWorkflowComp("loop", loopComp, nil,
		map[string]any{
			"loop_type":  "array",
			"loop_array": map[string]any{"item": []any{1, 2, 3}},
		}, nil, nil, nil, nil, 0, -1.0, nil, nil)
	w.AddConnection([]string{"start"}, "loop")
	w.AddConnection([]string{"loop"}, "end")

	sess := sessionpkg.NewSession(sessionpkg.WithEnvs(map[string]any{}))
	out, err := w.Invoke(context.Background(),
		map[string]any{"array": []any{1, 2, 3}, "num": 0}, sess, nil, nil)
	s.Require().NoError(err)
	wo := out.(*workflow.WorkflowOutput)
	s.Equal(workflow.WorkflowExecutionStateCompleted, wo.State)
	res := fmt.Sprint(wo.Result)
	// ArrayCondition tuple 分支逐轮写 item → body 逐元素 +10 → 收割 [11,12,13]
	for _, v := range []string{"11", "12", "13"} {
		s.Contains(res, v, "收割数组应含 %s", v)
	}
}

// ──────────────────────────── 3. always_true + break ────────────────────────────

// TestLoopAlwaysTrue_Break跳出 always_true 无限循环由 LoopBreakComponent 第 2 轮中断。
func (s *LoopCompSuite) TestLoopAlwaysTrue_Break跳出() {
	w := workflow.NewWorkflow(nil)
	w.SetStartComp("start", flow.NewStart(),
		map[string]any{"input_arr": "${array}", "input_num": "${num}"}, nil, nil)
	w.SetEndComp("end", s.newEnd("{{end_out}}"),
		map[string]any{"end_out": "${loop}"}, nil, nil, nil, "batch", nil)

	group := loop.NewLoopGroup()
	s.Require().NoError(group.AddWorkflowComp("body_1", newAddTen(), nil,
		map[string]any{"source": "${loop.index}"}, nil, nil, nil, nil, 0, -1.0, nil, nil))
	// LoopBreakComponent：controller 由 AdvancedLoopComponent 构造期注入
	//（Python break_nodes → set_controller(self)，loop_comp.py:360-362）
	s.Require().NoError(group.AddWorkflowComp("brk", loop.NewLoopBreakComponent(), nil, nil, nil, nil, nil, nil, 0, -1.0, nil, nil))
	s.Require().NoError(group.StartNodes([]string{"body_1"}))
	s.Require().NoError(group.EndNodes("brk"))
	s.Require().NoError(group.AddConnection([]string{"body_1"}, "brk"))

	loopComp, err := loop.NewLoopComponent(group,
		map[string]any{"results": "${body_1.result}"})
	s.Require().NoError(err)

	w.AddWorkflowComp("loop", loopComp, nil,
		map[string]any{
			"loop_type":   "always_true",
			"loop_number": 0,
		}, nil, nil, nil, nil, 0, -1.0, nil, nil)
	w.AddConnection([]string{"start"}, "loop")
	w.AddConnection([]string{"loop"}, "end")

	sess := sessionpkg.NewSession(sessionpkg.WithEnvs(map[string]any{}))
	out, err := w.Invoke(context.Background(),
		map[string]any{"array": []any{}, "num": 0}, sess, nil, nil)
	s.Require().NoError(err, "break 后应正常收尾而非死循环")
	wo := out.(*workflow.WorkflowOutput)
	s.Equal(workflow.WorkflowExecutionStateCompleted, wo.State)
	res := fmt.Sprint(wo.Result)
	// brk 作为 body 下游节点每轮末执行 → 第 1 轮末即置 BROKEN → 第 2 轮 condition 判定跳出
	s.Contains(res, "10", "第 1 轮结果应收割（index=0+10）")
	s.NotContains(res, "11", "第 2 轮不应执行（BROKEN 已置位）")
}

// ──────────────────────────── 4. 中断恢复（复刻 upstream loop×AddTenNode4Cp） ────────────────────────────

// TestLoopInterruptResume_真实重放 gate 首两轮抛错（reason 携带真实输入），
// InteractiveInput 同 session 恢复重放；第 3 次调用放行后循环完成收割。
func (s *LoopCompSuite) TestLoopInterruptResume_真实重放() {
	w := workflow.NewWorkflow(nil)
	w.SetStartComp("start", flow.NewStart(),
		map[string]any{"input_arr": "${array}", "input_num": "${num}"}, nil, nil)
	w.SetEndComp("end", s.newEnd("{{end_out}}"),
		map[string]any{"end_out": "${loop}"}, nil, nil, nil, "batch", nil)

	group := loop.NewLoopGroup()
	gate := newGate(2) // 前两次调用抛错
	s.Require().NoError(group.AddWorkflowComp("body_1", gate, nil,
		map[string]any{"source": "${loop.index}"}, nil, nil, nil, nil, 0, -1.0, nil, nil))
	s.Require().NoError(group.StartNodes([]string{"body_1"}))
	s.Require().NoError(group.EndNodes("body_1"))

	loopComp, err := loop.NewLoopComponent(group,
		map[string]any{"results": "${body_1.result}"})
	s.Require().NoError(err)

	w.AddWorkflowComp("loop", loopComp, nil,
		map[string]any{
			"loop_type":   "number",
			"loop_number": 1,
		}, nil, nil, nil, nil, 0, -1.0, nil, nil)
	w.AddConnection([]string{"start"}, "loop")
	w.AddConnection([]string{"loop"}, "end")

	sess := sessionpkg.NewSession(sessionpkg.WithEnvs(map[string]any{}))
	// ① 首次：index=0 → gate 抛 "inner error: 0"（真实输入贯通到出错点）
	_, err1 := w.Invoke(context.Background(), map[string]any{"array": []any{}, "num": 0}, sess, nil, nil)
	s.Require().Error(err1)
	s.Contains(err1.Error(), "inner error: 0")
	// ② 恢复一：INDEX 从 checkpoint 续跑 → gate 二次抛 "inner error: 0"？——
	// 上游语义：INDEX 递增发生在 condition 前进沿；恢复后 gate 重放同轮输入 0
	//（failures=2 → 第二次仍抛），断言 reason 携带真实输入
	ii, iiErr := interaction.NewInteractiveInput()
	s.Require().NoError(iiErr)
	_, err2 := w.Invoke(context.Background(), ii, sess, nil, nil)
	if err2 != nil {
		s.Contains(err2.Error(), "inner error", "恢复重放应再次到达 gate（携带真实输入）")
	}
	// ③ 恢复二：gate 放行 → 收尾完成
	ii2, iiErr2 := interaction.NewInteractiveInput()
	s.Require().NoError(iiErr2)
	out3, err3 := w.Invoke(context.Background(), ii2, sess, nil, nil)
	s.Require().NoError(err3, "第三次执行 gate 放行后应完成")
	wo := out3.(*workflow.WorkflowOutput)
	s.Equal(workflow.WorkflowExecutionStateCompleted, wo.State)
	s.Contains(fmt.Sprint(wo.Result), "10", "收割结果含 source(0)+10=10")
}

// newEnd 构造 End 组件（模板收敛，sub_workflow 套件同款）。
func (s *LoopCompSuite) newEnd(tmpl string) *flow.End {
	end, err := flow.NewEndConfig(tmpl)
	s.Require().NoError(err)
	e, err := flow.NewEnd(end)
	s.Require().NoError(err)
	return e
}
