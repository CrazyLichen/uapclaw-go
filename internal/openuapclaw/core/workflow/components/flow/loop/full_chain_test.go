package loop

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	iface "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/interface"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/graph"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/state"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/workflow/components"
)

// newLoopFacadeWithCp 构造真实 WorkflowSession 链的 loop 节点门面
// （真实 WorkflowSession 状态/检查点——Compile 需要 GraphStore；状态语义与
// 生产一致，规避 fakeInMemory 状态视图 Update/Get 不可见的语义差）
func newLoopFacadeWithCp(t *testing.T, nodeID string) *session.NodeSessionFacade {
	t.Helper()
	ws := session.NewWorkflowSessionFacade("outer", "sid-fullchain", nil)
	facade, _ := session.NewWorkflowNodeSessionPair(ws.Inner(), nodeID, "loop", false)
	return facade
}

// ──────────────────────────── 全链单测（LoopComponent → LoopGroup → 小图） ────────────────────────────

// countingComp 计数循环体组件（InvokeFn 记录 source 序列）
type countingComp struct {
	components.WorkflowComponent
	sources []int
}

func newCounting() *countingComp {
	c := &countingComp{}
	c.InvokeFn = func(_ context.Context, inputs map[string]any,
		_ *session.NodeSessionFacade, _ iface.ModelContext) (map[string]any, error) {
		n, _ := inputs["source"].(int)
		c.sources = append(c.sources, n)
		return map[string]any{"result": n + 10}, nil
	}
	return c
}

// TestLoopComponent_全链break中断 LoopBreakComponent 置 BROKEN → 第 2 轮跳出
func TestLoopComponent_全链break中断(t *testing.T) {
	group := NewLoopGroup()
	body := newCounting()
	require.NoError(t, group.AddWorkflowComp("body_1", body, nil,
		map[string]any{"source": "${loop.index}"}, nil, nil, nil, nil, 0, -1.0, nil, nil))
	require.NoError(t, group.AddWorkflowComp("brk", NewLoopBreakComponent(), nil,
		nil, nil, nil, nil, nil, 0, -1.0, nil, nil))
	require.NoError(t, group.StartNodes([]string{"body_1"}))
	require.NoError(t, group.EndNodes("brk"))
	require.NoError(t, group.AddConnection([]string{"body_1"}, "brk"))

	loopComp, err := NewLoopComponent(group, map[string]any{"results": "${body_1.result}"})
	require.NoError(t, err)

	facade := newLoopFacadeWithCp(t, "loop")

	_, err = loopComp.invokeInner(context.Background(),
		map[string]any{
			"inputs": map[string]any{"loop_type": "number", "loop_number": 5},
		}, facade)
	require.NoError(t, err)
	assert.Equal(t, []int{0}, body.sources, "第 1 轮末 brk 置 BROKEN → 第 2 轮跳出")
}

// TestLoopComponent_invoke包装器成功路径 number 循环经 invoke 十步（异常分类外圈）
func TestLoopComponent_invoke包装器成功路径(t *testing.T) {
	group := NewLoopGroup()
	body := newCounting()
	require.NoError(t, group.AddWorkflowComp("body_1", body, nil,
		map[string]any{"source": "${loop.index}"}, nil, nil, nil, nil, 0, -1.0, nil, nil))
	require.NoError(t, group.StartNodes([]string{"body_1"}))
	require.NoError(t, group.EndNodes("body_1"))

	loopComp, err := NewLoopComponent(group, map[string]any{"results": "${body_1.result}"})
	require.NoError(t, err)
	facade := newLoopFacadeWithCp(t, "loop")

	result, err := loopComp.invoke(context.Background(),
		map[string]any{"inputs": map[string]any{"loop_type": "number", "loop_number": 2}}, facade, nil)
	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, []int{0, 1}, body.sources)
}

// TestLoopComponent_全链expressionFalse 表达式恒假 → 不执行循环体直接收割
func TestLoopComponent_全链expressionFalse(t *testing.T) {
	group := NewLoopGroup()
	body := newCounting()
	require.NoError(t, group.AddWorkflowComp("body_1", body, nil,
		map[string]any{"source": "${loop.index}"}, nil, nil, nil, nil, 0, -1.0, nil, nil))
	require.NoError(t, group.StartNodes([]string{"body_1"}))
	require.NoError(t, group.EndNodes("body_1"))

	loopComp, err := NewLoopComponent(group, map[string]any{"results": "${body_1.result}"})
	require.NoError(t, err)
	facade := newLoopFacadeWithCp(t, "loop")

	_, err = loopComp.invoke(context.Background(),
		map[string]any{"inputs": map[string]any{"loop_type": "expression", "bool_expression": "False"}}, facade, nil)
	require.NoError(t, err)
	assert.Empty(t, body.sources, "表达式恒假 → 循环体零执行")
}

// TestLoopComponent_全链alwaysTrueBreak always_true + brk 第 1 轮末中断
func TestLoopComponent_全链alwaysTrueBreak(t *testing.T) {
	group := NewLoopGroup()
	body := newCounting()
	require.NoError(t, group.AddWorkflowComp("body_1", body, nil,
		map[string]any{"source": "${loop.index}"}, nil, nil, nil, nil, 0, -1.0, nil, nil))
	require.NoError(t, group.AddWorkflowComp("brk", NewLoopBreakComponent(), nil,
		nil, nil, nil, nil, nil, 0, -1.0, nil, nil))
	require.NoError(t, group.StartNodes([]string{"body_1"}))
	require.NoError(t, group.EndNodes("brk"))
	require.NoError(t, group.AddConnection([]string{"body_1"}, "brk"))

	loopComp, err := NewLoopComponent(group, map[string]any{"results": "${body_1.result}"})
	require.NoError(t, err)
	facade := newLoopFacadeWithCp(t, "loop")

	_, err = loopComp.invoke(context.Background(),
		map[string]any{"inputs": map[string]any{"loop_type": "always_true", "loop_number": 0}}, facade, nil)
	require.NoError(t, err)
	assert.Equal(t, []int{0}, body.sources, "第 1 轮末 brk 置 BROKEN → 跳出")
}

// TestLoopComponent_访问器与装配面 GraphInvoker/ToExecutable/AddComponent 动态分派 +
// LoopGroup 访问器 + Body 访问器 + nodeStateOf 断言
func TestLoopComponent_访问器与装配面(t *testing.T) {
	group := NewLoopGroup()
	body := newCounting()
	require.NoError(t, group.AddWorkflowComp("body_1", body, nil, nil, nil, nil, nil, nil, 0, -1.0, nil, nil))
	require.NoError(t, group.StartNodes([]string{"body_1"}))
	require.NoError(t, group.EndNodes("body_1"))
	loopComp, err := NewLoopComponent(group, map[string]any{})
	require.NoError(t, err)

	// 动态分派三钩子（8.14b D-14b 同缝的回归锚）
	assert.True(t, loopComp.GraphInvoker())
	var ge graph.GeneralExecutor = loopComp.ToExecutable()
	assert.True(t, ge.GraphInvoker(), "ToExecutable 必须保留外层动态类型")
	// AddComponent 注册自身
	g := graph.NewPregelGraph()
	require.NoError(t, loopComp.AddComponent(g, "loop_x", false))
	assert.Contains(t, g.GetNodes(), "loop_x")
	// LoopGroup 访问器
	assert.Same(t, group, loopComp.LoopGroup())
	// Body 访问器
	alc := newTestALC(t, newFakeCond(true, nil), nil)
	assert.NotNil(t, alc.Body())
	// nodeStateOf：nil 安全与断言失败
	assert.Nil(t, nodeStateOf(nil))
	assert.Nil(t, nodeStateOf(&fakeNodeSession{st: &nonCommitLoopState{}}))
}

// TestLoopGroup_cleanupFinishedStreamNodes 对齐 loop_comp.py:125-129（清理+写回提交）
func TestLoopGroup_cleanupFinishedStreamNodes(t *testing.T) {
	sess, st := newLoopSession(map[state.StateKey]any{}, nil)
	// 预置 workflow_state：finished_stream_nodes = [loop 内 2 个 + 外部 1 个]
	// UpdateAndCommitWorkflowState 经 fakeLoopState 的 WorkflowState 嵌入面 → 覆写以记录
	var committed map[string]any
	st.workflowCommit = func(data map[string]any) { committed = data }
	st.wfStateValues = map[string]any{"finished_stream_nodes": []any{"body_1", "brk", "outside"}}
	st.SetWorkflowCommitStateForTest()

	g := NewLoopGroup()
	g.nodeIDs = []string{"body_1", "brk"}
	g.cleanupFinishedStreamNodes(sess)

	require.NotNil(t, committed)
	fin := committed["finished_stream_nodes"].([]any)
	assert.Equal(t, []any{"outside"}, fin, "仅外部节点保留")
}

// TestAdvancedLoopComponent_快照与断言助手 ioStateSnapshot 两分支 + provider 缺失回退
func TestAdvancedLoopComponent_快照与断言助手(t *testing.T) {
	a := newTestALC(t, newFakeCond(true, nil), nil)

	// ioStateSnapshot：无 parent_id → 顶层快照直读（含 node_id 删除分支）
	sess, st := newLoopSession(map[state.StateKey]any{}, nil)
	st.ioSnapshot = map[string]any{"loop_1": map[string]any{"x": 1}, "other": 2}
	sess.(*fakeLoopSession).noParentID = true
	a.nodeID = "loop_1"
	snap := a.ioStateSnapshot(st, sess)
	assert.Equal(t, map[string]any{"other": 2}, snap, "本节点残留应剔除")

	// ioStateSnapshot：io 键缺失 → nil
	st.ioSnapshot = nil
	assert.Nil(t, a.ioStateSnapshot(st, sess))

	// executableIDOf/parentSessionOf：provider 缺失 → ""
	assert.Equal(t, "", executableIDOf(&bareLoopSession{}))
	assert.Nil(t, parentSessionOf(&bareLoopSession{}))
}

// bareLoopSession 仅实现 BaseSession（无 ExecutableID/ParentID/Parent provider）
type bareLoopSession struct {
	session.BaseSession
}
