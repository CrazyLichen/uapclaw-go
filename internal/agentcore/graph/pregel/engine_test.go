package pregel

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestRecursionError_Error 文案复刻 Python engine.py:139-141
func TestRecursionError_Error(t *testing.T) {
	e := &RecursionError{Limit: 25, Step: 26}
	assert.Equal(t, "Recursion limit of 25 reached at step 26", e.Error())
	// spec D3：engine.py:85 按类型名分类上抛，Go 以 errors.As 类型断言还原契约
	var target *RecursionError
	assert.True(t, errors.As(error(e), &target))
	assert.Equal(t, 25, target.Limit)
}

// ──────────────────────────── 测试桩与构造 helper ────────────────────────────

// fakeSaver 可控返回的检查点存储桩（pregel.Store 实现）
type fakeSaver struct {
	state  *GraphState
	getErr error
	saved  *GraphState
}

func (f *fakeSaver) Get(_ context.Context, _, _ string) (*GraphState, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	return f.state, nil
}

func (f *fakeSaver) Save(_ context.Context, _, _ string, s *GraphState) error {
	f.saved = s
	return nil
}

func (f *fakeSaver) Delete(_ context.Context, _, _ string) error { return nil }

// handGraph 手工构造 START→a→END 图（Task 8 Build 回填前的构造方式）
func handGraph() *Pregel {
	nodes := map[string]*PregelNode{}
	nodes[Start] = NewPregelNode(Start, NoopNodeFunc)
	a := NewPregelNode("a", NoopNodeFunc)
	a.addRouter(NewStaticRouter(End))
	nodes["a"] = a
	nodes[End] = NewPregelNode(End, NoopNodeFunc)
	channels := []Channel{NewTriggerChannel(Start), NewTriggerChannel("a"), NewTriggerChannel(End)}
	return NewPregel(nodes, channels, Start, nil, nil)
}

// ──────────────────────────── PregelLoop.Init ────────────────────────────

// TestPregelLoop_Init_首跑 触发 START 消息并 flush（engine.py:59-62）
func TestPregelLoop_Init_首跑(t *testing.T) {
	pg := handGraph()
	l := NewPregelLoop(pg, &InnerPregelConfig{PregelConfig: PregelConfig{RecursionLimit: 25}})
	assert.NoError(t, l.Init(context.Background()))
	assert.NotNil(t, l.Executor)
	assert.Equal(t, 25, l.MaxStep)
	// START 通道 flush 后就绪
	assert.Equal(t, []string{Start}, l.Manager.GetReadyNodes())
}

// TestPregelLoop_Init_恢复 state 可恢复则逐项回填（engine.py:45-58）
func TestPregelLoop_Init_恢复(t *testing.T) {
	sv := &fakeSaver{state: &GraphState{
		Step: 3,
		ChannelValues: map[string][]ChannelSnapshot{
			"a": {{Messages: []Message{NewTriggerMessage("x", "a")}}},
		},
		NodeVersion:   map[string]int{"a": 2},
		PendingBuffer: []Message{NewTriggerMessage("a", "b")},
		PendingNode:   map[string]PendingNode{"z": {NodeName: "z", Status: TaskStatusError}},
	}}
	pg := handGraph()
	l := NewPregelLoop(pg, &InnerPregelConfig{PregelConfig: PregelConfig{SessionID: "s", Ns: "n", RecursionLimit: 25}})
	l.Saver = sv
	assert.NoError(t, l.Init(context.Background()))
	assert.Equal(t, 3, l.Step)
	assert.Equal(t, 3+25, l.MaxStep)
	assert.Equal(t, map[string]int{"a": 2}, l.NodeVersion)
	assert.Equal(t, map[string]PendingNode{"z": {NodeName: "z", Status: TaskStatusError}}, l.retryPendingNodes)
	// 通道恢复即就绪（pending_buffer 仅暂存未 flush，对齐 engine.py:54-55）
	assert.Equal(t, []string{"a"}, l.Manager.GetReadyNodes())
}

// TestPregelLoop_Init_不满足恢复条件 无 session/ns/saver 不读存储（engine.py:43）
func TestPregelLoop_Init_不满足恢复条件(t *testing.T) {
	sv := &fakeSaver{state: &GraphState{Step: 9}}
	pg := handGraph()
	l := NewPregelLoop(pg, &InnerPregelConfig{PregelConfig: PregelConfig{RecursionLimit: 25}})
	l.Saver = sv // 有 saver 但无 sessionID/ns
	assert.NoError(t, l.Init(context.Background()))
	assert.Nil(t, sv.saved)
	// Python: state=None → 非 resume → 触发 START
	assert.Equal(t, []string{Start}, l.Manager.GetReadyNodes())
}

// TestIsResume nil state 与空 state 均不恢复（engine.py:94-96 + store is_resumable）
func TestIsResume(t *testing.T) {
	assert.False(t, isResume(nil))
	assert.False(t, isResume(&GraphState{}))
	assert.True(t, isResume(&GraphState{PendingBuffer: []Message{NewTriggerMessage("a", "b")}}))
}
