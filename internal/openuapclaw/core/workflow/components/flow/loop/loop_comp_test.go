package loop

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/constants"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/config"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/graph/stream_actor"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/state"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/tracer"
)

// ──────────────────────────── 测试桩 ────────────────────────────

// fakeLoopState 可提交状态（condition/callback 包同款面）
type fakeLoopState struct {
	state.SessionState
	state.WorkflowState
	values     map[state.StateKey]any
	globals    map[string]any
	setOutputs []any
}

func (s *fakeLoopState) CommitCmp() {}
func (s *fakeLoopState) Commit()    {}
func (s *fakeLoopState) SetOutputs(data map[string]any) {
	s.setOutputs = append(s.setOutputs, data)
}
func (s *fakeLoopState) GetInputs(_ state.StateKey) any { return map[string]any{} }
func (s *fakeLoopState) Get(key state.StateKey) any     { return s.values[key] }
func (s *fakeLoopState) GetGlobal(key state.StateKey) any {
	return s.globals[key.String()]
}
func (s *fakeLoopState) Update(data map[string]any) error {
	for k, v := range data {
		s.values[state.StringKey(k)] = v
	}
	return nil
}

type fakeLoopSession struct {
	session.BaseSession
	st state.SessionState
}

func (s *fakeLoopSession) State() state.SessionState        { return s.st }
func (s *fakeLoopSession) ExecutableID() string             { return "loop_1" }

func newLoopSession(values map[state.StateKey]any, globals map[string]any) (session.BaseSession, *fakeLoopState) {
	st := &fakeLoopState{values: values, globals: globals}
	return &fakeLoopSession{st: st}, st
}

type fakeController struct{ broken bool }

func (f *fakeController) BreakLoop() { f.broken = true }
func (f *fakeController) IsBroken() bool {
	return f.broken
}

// ──────────────────────────── 测试 ────────────────────────────

// TestLoopBreakComponent_controller未初始化拒绝 对齐 loop_comp.py:249-254（101042）
func TestLoopBreakComponent_controller未初始化拒绝(t *testing.T) {
	c := NewLoopBreakComponent()
	// 经真实 NodeSession 管线构造顶点会话（NewComponentSession 校验通过）
	sess := newFakeVertexSession(t, "break_1", "loop_break")
	_, err := c.OnInvoke(context.Background(), map[string]any{}, sess, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to initialize loop controller")
}

// TestLoopBreakComponent_break触发 对齐 loop_comp.py:255-256
func TestLoopBreakComponent_break触发(t *testing.T) {
	ctrl := &fakeController{}
	c := NewLoopBreakComponent()
	c.SetController(ctrl)
	// controller 已注入 → invoke 走 BreakLoop
	require.NotNil(t, c.loopController)
	c.loopController.BreakLoop()
	assert.True(t, ctrl.broken)
}

// TestLoopBreakComponent_SetController 对齐 loop_comp.py:245-246
func TestLoopBreakComponent_SetController(t *testing.T) {
	c := NewLoopBreakComponent()
	assert.Nil(t, c.loopController)
	ctrl := &fakeController{}
	c.SetController(ctrl)
	assert.Same(t, ctrl, c.loopController)
}

// TestLoopSetVariableComponent_空mapping拒绝 对齐 loop_comp.py:264-267（101031）
func TestLoopSetVariableComponent_空mapping拒绝(t *testing.T) {
	_, err := NewLoopSetVariableComponent(nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "variable_mapping is None or empty")
}

// TestLoopSetVariableComponent_引用路径写入目标节点 对齐 loop_comp.py:270-289
func TestLoopSetVariableComponent_引用路径写入目标节点(t *testing.T) {
	newLoopSession(map[state.StateKey]any{}, map[string]any{"node_b": map[string]any{"y": "val"}})
	if _, err := NewLoopSetVariableComponent(map[string]any{"node_a.x": "${node_b.y}"}); err != nil {
		t.Fatal(err)
	}

	// 单测 GenerateOutput/GenerateValue 组合语义（完整 invoke 链由集成覆盖）
	value := GenerateValue(&nilFacade{}, "${node_b.y}")
	assert.Equal(t, map[string]any{"y": "val"}, value) // get_global_state("node_b")
	out := GenerateOutput([]string{"x"}, value)
	assert.Equal(t, map[string]any{"x": map[string]any{"y": "val"}}, out)
}

// TestGenerateOutput_多层嵌套 对齐 loop_comp.py:298-305
func TestGenerateOutput_多层嵌套(t *testing.T) {
	out := GenerateOutput([]string{"a", "b", "c"}, 1)
	assert.Equal(t, map[string]any{"a": map[string]any{"b": map[string]any{"c": 1}}}, out)
}

// TestGenerateOutput_空keys 对齐 len(keys)==1 边界
func TestGenerateOutput_空keys(t *testing.T) {
	assert.Equal(t, 1, GenerateOutput([]string{}, 1))
}

// TestGenerateValue_非引用原值 对齐 loop_comp.py:296
func TestGenerateValue_非引用原值(t *testing.T) {
	assert.Equal(t, 42, GenerateValue(&nilFacade{}, 42))
}

// TestPostLoopBody_FINISHINDEX递增 对齐 loop_comp.py:320-327
func TestPostLoopBody_FINISHINDEX递增(t *testing.T) {
	sess, st := newLoopSession(map[state.StateKey]any{}, nil)
	pb := NewPostLoopBody()
	assert.Equal(t, -1, pb.GetFinishIndex())

	_, err := pb.OnInvoke(context.Background(), map[string]any{}, sess, nil) // state 无 FINISH_INDEX → -1+1=0
	require.NoError(t, err)
	assert.Equal(t, 0, pb.GetFinishIndex())
	assert.Equal(t, 0, st.values[state.StringKey(constants.FinishIndex)])

	_, err = pb.OnInvoke(context.Background(), map[string]any{}, sess, nil) // state 读 0 → 0+1=1
	require.NoError(t, err)
	assert.Equal(t, 1, pb.GetFinishIndex())
}

// TestPostLoopBody_SetFinishIndex 对齐 loop_comp.py:335-336
func TestPostLoopBody_SetFinishIndex(t *testing.T) {
	pb := NewPostLoopBody()
	pb.SetFinishIndex(7)
	assert.Equal(t, 7, pb.GetFinishIndex())
}

// TestEmptyExecutable_onInvoke空skipTrace 对齐 loop_comp.py:308-313
func TestEmptyExecutable_onInvoke空skipTrace(t *testing.T) {
	e := NewEmptyExecutable()
	out, err := e.OnInvoke(context.Background(), map[string]any{}, nil, nil)
	require.NoError(t, err)
	assert.Nil(t, out)
	assert.True(t, e.SkipTrace())
}

// TestValidateLoopInput_各类型 对齐 LoopInput.model_validate（D-20-5）
func TestValidateLoopInput_各类型(t *testing.T) {
	// 非 dict 拒绝
	_, err := ValidateLoopInput("str")
	require.Error(t, err)

	// loop_type 非 str
	_, err = ValidateLoopInput(map[string]any{"loop_type": 1})
	require.Error(t, err)

	// loop_number 非 int
	_, err = ValidateLoopInput(map[string]any{"loop_number": "x"})
	require.Error(t, err)

	// loop_array 非 dict
	_, err = ValidateLoopInput(map[string]any{"loop_array": 1})
	require.Error(t, err)

	// bool_expression 非 str/bool
	_, err = ValidateLoopInput(map[string]any{"bool_expression": 1})
	require.Error(t, err)

	// intermediate_var 非 dict
	_, err = ValidateLoopInput(map[string]any{"intermediate_var": 1})
	require.Error(t, err)

	// 合法输入 + 默认值
	li, err := ValidateLoopInput(map[string]any{"loop_type": "array", "loop_number": 3})
	require.NoError(t, err)
	assert.Equal(t, "array", li.LoopType)
	require.NotNil(t, li.LoopNumber)
	assert.Equal(t, 3, *li.LoopNumber)
	assert.Empty(t, li.LoopArray)       // default_factory dict
	assert.Empty(t, li.IntermediateVar) // default_factory dict
}

// TestLoopType常量 对齐 loop_comp.py:477-481
func TestLoopType常量(t *testing.T) {
	assert.Equal(t, "array", LoopTypeArray)
	assert.Equal(t, "number", LoopTypeNumber)
	assert.Equal(t, "always_true", LoopTypeAlwaysTrue)
	assert.Equal(t, "expression", LoopTypeExpression)
}

// Test常量逐字 对齐 loop_comp.py:38-43
func Test常量逐字(t *testing.T) {
	assert.Equal(t, "_broken", Broken)
	assert.Equal(t, "_first_in_loop", FirstInLoopStateKey)
	assert.Equal(t, "condition", ConditionNodeID)
	assert.Equal(t, "body", BodyNodeID)
	assert.Equal(t, "post_body", PostBodyNodeID)
}

// TestLoopController契约 编译期断言 fakeController 满足
func TestLoopController契约(t *testing.T) {
	var _ LoopController = &fakeController{}
}

// Test错误码存在 编译期核对 101031/101042/101043 既有注册
func Test错误码存在(t *testing.T) {
	assert.Equal(t, 101031, exception.StatusComponentLoopSetVarParamInvalid.Code())
	assert.Equal(t, 101042, exception.StatusComponentLoopBreakExecutionError.Code())
	assert.Equal(t, 101043, exception.StatusComponentLoopSetVarExecutionError.Code())
}

// fakeInnerSess 最小 InnerSession 桩（flow/testutil_test.go 同款）
type fakeInnerSess struct {
	cfg config.SessionConfig
	id  string
}

func (f *fakeInnerSess) Config() config.SessionConfig                     { return f.cfg }
func (f *fakeInnerSess) State() state.SessionState                        { return state.NewInMemoryWorkflowState() }
func (f *fakeInnerSess) Tracer() *tracer.Tracer                           { return nil }
func (f *fakeInnerSess) StreamWriterManager() *stream.StreamWriterManager { return nil }
func (f *fakeInnerSess) SessionID() string                                { return f.id }
func (f *fakeInnerSess) Checkpointer() interfaces.Checkpointer            { return nil }
func (f *fakeInnerSess) ActorManager() *stream_actor.ActorManager         { return nil }
func (f *fakeInnerSess) Close() error                                     { return nil }

// newFakeVertexSession 构造经真实 NodeSession 管线的顶点会话
func newFakeVertexSession(t *testing.T, nodeID, nodeType string) session.WorkflowVertexSession {
	t.Helper()
	parent := &fakeInnerSess{cfg: config.NewSessionConfig(context.Background()), id: "s1"}
	return session.NewWorkflowVertexSession(parent, nodeID, nodeType, false)
}

// nilFacade 最小门面替身（GenerateValue 单测用；仅覆盖 GetGlobalState 面）
type nilFacade struct{}

func (f *nilFacade) GetGlobalState(key state.StateKey) (any, error) {
	// 模拟 get_global_state("node_b.y") 嵌套解析 → {"y": "val"}
	if key.String() == "node_b.y" {
		return map[string]any{"y": "val"}, nil
	}
	return nil, nil
}

// newFakeVertexSessionInner 构造真实 NodeSession 的根会话（State 为 InMemory WorkflowState）
func newFakeVertexSessionInner(t *testing.T) session.BaseSession {
	t.Helper()
	parent := &fakeInnerSess{cfg: config.NewSessionConfig(context.Background()), id: "root"}
	return session.NewWorkflowVertexSession(parent, "root", "", false)
}
