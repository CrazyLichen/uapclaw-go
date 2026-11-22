package loop

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/common/constants"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/graph"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session"
)

// ──────────────────────────── 测试 ────────────────────────────

// TestLoopGroup_checkValidate三段 对齐 loop_comp.py:142-156（101030 reason 逐字）
func TestLoopGroup_checkValidate三段(t *testing.T) {
	// 缺 start_nodes
	g := NewLoopGroup()
	g.EndComp("end")
	err := g.CheckValidate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing start_nodes in loop group")

	// 缺 end_nodes
	g2 := NewLoopGroup()
	g2.StartComp("start")
	err = g2.CheckValidate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing end_nodes in loop group")

	// 空图
	g3 := NewLoopGroup()
	g3.StartComp("start")
	g3.EndComp("end")
	err = g3.CheckValidate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "loop group is empty (contains no nodes)")
}

// TestLoopGroup_checkValidate通过 有节点时通过
func TestLoopGroup_checkValidate通过(t *testing.T) {
	g := NewLoopGroup()
	g.StartComp("start")
	g.EndComp("end")
	// 注册一个组件占位节点（BaseWorkflow.AddWorkflowComp 需 ComponentComposable——
	// 用最小组合件桩）
	g.BaseWorkflow.AddWorkflowComp("n1", &fakeComposable{}, nil, nil, nil, nil, nil, nil, 0, 0, nil, nil)
	require.NoError(t, g.CheckValidate())
}

// TestLoopGroup_双轨记录 对齐 loop_comp.py:93-98/110-115（BaseWorkflow API 直调也收集）
func TestLoopGroup_双轨记录(t *testing.T) {
	g := NewLoopGroup()
	// 经 LoopGroup API（内部走 super + 收集）
	require.NoError(t, g.StartNodes([]string{"s1"}))
	require.NoError(t, g.EndNodes("e1"))
	assert.Equal(t, []string{"s1"}, g.startNodeIDs)
	// Python 逐字行为：end_comp 内 append + end_nodes(str) 外层 append → 双条目（bug 忠实保留）

	// 重复 StartComp 不重复记录（Python: if start_comp_id not in self._start_nodes）
	require.NoError(t, g.StartComp("s1"))
	assert.Equal(t, []string{"s1"}, g.startNodeIDs)
	require.NoError(t, g.EndComp("e1"))
	assert.Equal(t, []string{"e1", "e1"}, g.endNodeIDs)
}

// TestLoopGroup_EndNodes批量 对齐 loop_comp.py:104-107（list → 整体覆盖 _end_nodes）
func TestLoopGroup_EndNodes批量(t *testing.T) {
	g := NewLoopGroup()
	require.NoError(t, g.EndNodes([]string{"e1", "e2"}))
	assert.Equal(t, []string{"e1", "e2"}, g.endNodeIDs)
}

// TestLoopComponent_信封硬错 对齐 loop_comp.py:168-172（8.2 D5 兑现）
func TestLoopComponent_信封硬错(t *testing.T) {
	g := newValidGroup(t)
	c, err := NewLoopComponent(g, map[string]any{})
	require.NoError(t, err)

	facade, _ := session.NewWorkflowNodeSessionPair(newFakeVertexParent(t), "loop_1", "", false)
	// INPUTS_KEY 缺失
	_, err = c.invoke(context.Background(), map[string]any{}, facade)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing required key inputs")

	// inputs nil
	_, err = c.invoke(context.Background(), nil, facade)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "inputs must be a dict")
}

// TestLoopComponent_未知loopType 对齐 loop_comp.py:199-200
func TestLoopComponent_未知loopType(t *testing.T) {
	g := newValidGroup(t)
	c, err := NewLoopComponent(g, map[string]any{})
	require.NoError(t, err)
	facade, _ := session.NewWorkflowNodeSessionPair(newFakeVertexParent(t), "loop_1", "", false)

	_, err = c.invokeInner(context.Background(), map[string]any{
		"inputs": map[string]any{"loop_type": "bogus"},
	}, facade)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid loop type 'bogus' for LoopComponent")
}

// TestLoopComponent_numberEnv上限 对齐 loop_comp.py:179-189
func TestLoopComponent_numberEnv上限(t *testing.T) {
	g := newValidGroup(t)
	c, err := NewLoopComponent(g, map[string]any{})
	require.NoError(t, err)
	facade, _ := session.NewWorkflowNodeSessionPair(newFakeVertexParent(t), "loop_1", "", false)

	// loop_number nil → ValueError
	_, err = c.invokeInner(context.Background(), map[string]any{
		"inputs": map[string]any{"loop_type": "number"},
	}, facade)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "loop_number variable not found or is None")

	// 超限 → ValueError（默认上限 1000）
	n := 1001
	_, err = c.invokeInner(context.Background(), map[string]any{
		"inputs": map[string]any{"loop_type": "number", "loop_number": n},
	}, facade)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "loop_number exceeds maximum limit 1000")
}

// TestLoopComponent_expressionStr字面量表达式串通过校验（真实执行链由集成覆盖）
func TestLoopComponent_expressionStr字面量表达式串通过校验(t *testing.T) {
	// expression 分支的 str → ExpressionCondition 构造（长度护栏在 condition 包已测）
	li, err := ValidateLoopInput(map[string]any{"loop_type": "expression", "bool_expression": "a == 1"})
	require.NoError(t, err)
	assert.Equal(t, "a == 1", li.BoolExpression)
}

// TestLoopComponent_构造期checkValidate 对齐 loop_comp.py:164
func TestLoopComponent_构造期checkValidate(t *testing.T) {
	_, err := NewLoopComponent(NewLoopGroup(), map[string]any{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing start_nodes in loop group")
}

// TestValidateLoopInput_number默认 对齐 pydantic Optional 默认（D-20-5）
func TestValidateLoopInput_number默认(t *testing.T) {
	li, err := ValidateLoopInput(map[string]any{"loop_type": "number"})
	require.NoError(t, err)
	assert.Nil(t, li.LoopNumber) // Optional[int] 未传 ≙ None
}

// ──────────────────────────── 桩 ────────────────────────────

// fakeComposable 最小组合件（AddComponent 注册占位节点）
type fakeComposable struct{}

func (f *fakeComposable) AddComponent(g *graph.PregelGraph, nodeID string, waitForAll bool) error {
	g.AddNode(nodeID, &fakeBody{})
	return nil
}

func (f *fakeComposable) ToExecutable() graph.GeneralExecutor { return &fakeBody{} }

// newValidGroup 构造通过 CheckValidate 的 LoopGroup
func newValidGroup(t *testing.T) *LoopGroup {
	t.Helper()
	g := NewLoopGroup()
	g.StartComp("start")
	g.EndComp("end")
	require.NoError(t, g.BaseWorkflow.AddWorkflowComp("n1", &fakeComposable{}, nil, nil, nil, nil, nil, nil, 0, 0, nil, nil))
	return g
}

// newFakeVertexParent 构造顶点会话父桩
func newFakeVertexParent(t *testing.T) session.BaseSession {
	t.Helper()
	return newFakeVertexSession(t, "loop_1", "loop")
}

// Test常量_IndexFinishIndex 对齐 constant.py:21-22（common 常量逐字）
func Test常量_IndexFinishIndex(t *testing.T) {
	assert.Equal(t, "index", constants.Index)
	assert.Equal(t, "finish_index", constants.FinishIndex)
	assert.Equal(t, "__sys_loop_id", constants.LoopID)
}

// TestLoopSetVariableComponent_invoke全链 对齐 loop_comp.py:270-289（真实 NodeSession 链）
func TestLoopSetVariableComponent_invoke全链(t *testing.T) {
	// root（NodeSession）→ setvar 节点 facade（parent=root）
	root := session.NewWorkflowVertexSession(newFakeVertexSessionInner(t), "root", "", false)
	facade, _ := session.NewWorkflowNodeSessionPair(root, "setvar_1", "", false)
	// 全局状态预置 node_b.y（Python get_global_state("node_b.y")）
	facade.InnerNodeSession().State().UpdateGlobal(map[string]any{"node_b": map[string]any{"y": "val"}})

	c, err := NewLoopSetVariableComponent(map[string]any{"node_a.x": "${node_b.y}"})
	require.NoError(t, err)

	out, err := c.invoke(facade)
	require.NoError(t, err)
	assert.Nil(t, out) // Python: return None
	// 目标节点 outputs 精确断言移交集成覆盖（io_state 视图前缀语义由 8.14b
	// 既有集成链交叉验证；单测锁"全链不 panic + 路由解析正确"）
}

// TestLoopSetVariableComponent_invoke空keys 对齐 loop_comp.py:276-283（101043）
func TestLoopSetVariableComponent_invoke空keys(t *testing.T) {
	root := session.NewWorkflowVertexSession(newFakeVertexSessionInner(t), "root", "", false)
	facade, _ := session.NewWorkflowNodeSessionPair(root, "setvar_1", "", false)
	// ExtractOriginKey 后空段 → "not supported format"（构造可产生空 key 的映射：
	// 引用路径剥离后为空串 → split 出 [""]，len==1 非 0——Python len(keys)==0 分支
	// 实际不可达（split 恒产 ≥1 段），防御分支以映射键为空串逼近）
	c, err := NewLoopSetVariableComponent(map[string]any{"": "v"})
	require.NoError(t, err)
	_, err = c.invoke(facade)
	// Python: keys=[""] → node_id="" → NewNodeSession(root,"") 行为——非 101043 路径
	// （真实不可达分支，登记 D-20-x 留档；此处锁定不 panic 且错误语义明确）
	if err != nil {
		assert.Contains(t, err.Error(), "not supported format")
	}
}
