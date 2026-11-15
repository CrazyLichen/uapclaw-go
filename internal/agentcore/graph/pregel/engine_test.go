package pregel

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
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
	nodes[Start].addRouter(NewStaticRouter("a"))
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

// innerCfgWith 经 CreateInnerConfig 归一化的内部配置（复刻 engine.py:232 调用时机）
func innerCfgWith(sessionID, ns string) *InnerPregelConfig {
	inner := CreateInnerConfig(PregelConfig{SessionID: sessionID, Ns: ns})
	return &inner
}

// ──────────────────────────── RunStep / saveStateOnError ────────────────────────────

// failingGraph 节点 a 返回普通错误（非中断）
func failingGraph() *Pregel {
	nodes := map[string]*PregelNode{}
	nodes[Start] = NewPregelNode(Start, NoopNodeFunc)
	nodes[Start].addRouter(NewStaticRouter("a"))
	nodes["a"] = NewPregelNode("a", func(_ context.Context, _ NodeInput) error {
		return errors.New("boom")
	})
	nodes[End] = NewPregelNode(End, NoopNodeFunc)
	channels := []Channel{NewTriggerChannel(Start), NewTriggerChannel("a"), NewTriggerChannel(End)}
	return NewPregel(nodes, channels, Start, nil, nil)
}

// interruptingGraph 节点 a 抛图级中断
func interruptingGraph() *Pregel {
	nodes := map[string]*PregelNode{}
	nodes[Start] = NewPregelNode(Start, NoopNodeFunc)
	nodes[Start].addRouter(NewStaticRouter("a"))
	nodes["a"] = NewPregelNode("a", func(_ context.Context, _ NodeInput) error {
		PanicGraphInterrupt(Interrupt{Value: "x"})
		return nil
	})
	nodes[End] = NewPregelNode(End, NoopNodeFunc)
	channels := []Channel{NewTriggerChannel(Start), NewTriggerChannel("a"), NewTriggerChannel(End)}
	return NewPregel(nodes, channels, Start, nil, nil)
}

// TestPregelLoop_RunStep_递归上限（engine.py:138-141）
func TestPregelLoop_RunStep_递归上限(t *testing.T) {
	pg := handGraph()
	l := NewPregelLoop(pg, &InnerPregelConfig{PregelConfig: PregelConfig{RecursionLimit: 25}})
	assert.NoError(t, l.Init(context.Background()))
	l.Step = 3
	l.MaxStep = 2
	_, err := l.RunStep(context.Background())
	var re *RecursionError
	assert.True(t, errors.As(err, &re))
	assert.Equal(t, 2, re.Limit)
}

// TestPregelLoop_RunStep_错误保存 主循环错误落盘并包 112104（engine.py:82/173-205）
func TestPregelLoop_RunStep_错误保存(t *testing.T) {
	sv := &fakeSaver{}
	pg := failingGraph()
	l := NewPregelLoop(pg, innerCfgWith("s", "n"))
	l.Saver = sv
	assert.NoError(t, l.Init(context.Background()))
	var err error
	for {
		var more bool
		more, err = l.RunStep(context.Background())
		if err != nil || !more {
			break
		}
	}
	assert.Error(t, err)
	var be *exception.BaseError
	assert.True(t, errors.As(err, &be))
	assert.Equal(t, 112104, be.Code())
	assert.NotNil(t, sv.saved)
	assert.NotEmpty(t, sv.saved.PendingNode)
}

// TestPregelLoop_RunStep_中断保存不包装 GraphInterrupt 原样上抛（engine.py:85-86）
func TestPregelLoop_RunStep_中断保存不包装(t *testing.T) {
	pg := interruptingGraph()
	l := NewPregelLoop(pg, innerCfgWith("s", "n"))
	l.Saver = &fakeSaver{}
	assert.NoError(t, l.Init(context.Background()))
	var err error
	for {
		var more bool
		more, err = l.RunStep(context.Background())
		if err != nil || !more {
			break
		}
	}
	var gi *GraphInterrupt
	assert.True(t, errors.As(err, &gi))
	assert.Equal(t, "x", gi.Interrupts[0].Value)
}

// TestPregelLoop_RunStep_图结束 无活跃节点且 buffer 空 → false（engine.py:129-131）
func TestPregelLoop_RunStep_图结束(t *testing.T) {
	pg := handGraph()
	l := NewPregelLoop(pg, &InnerPregelConfig{PregelConfig: PregelConfig{RecursionLimit: 25}})
	assert.NoError(t, l.Init(context.Background()))
	more, err := l.RunStep(context.Background()) // step0: 跑 START
	assert.NoError(t, err)
	assert.True(t, more)
	more, err = l.RunStep(context.Background()) // step1: 跑 a
	assert.NoError(t, err)
	assert.True(t, more)
	more, err = l.RunStep(context.Background()) // step2: END 就绪但被过滤，buffer 空 → 结束
	assert.NoError(t, err)
	assert.False(t, more)
}

// TestPregelLoop_RunStep_重试队列优先 retryPendingNodes 先于就绪集（engine.py:115-118）
func TestPregelLoop_RunStep_重试队列优先(t *testing.T) {
	pg := failingGraph()
	l := NewPregelLoop(pg, innerCfgWith("s", "n"))
	l.Saver = &fakeSaver{}
	assert.NoError(t, l.Init(context.Background()))
	_, _ = l.RunStep(context.Background()) // a 失败落盘
	// 从落盘状态构造恢复：pending_node = {a}
	l.retryPendingNodes = map[string]PendingNode{"a": {NodeName: "a", Status: TaskStatusError}}
	_, err := l.RunStep(context.Background())
	assert.Error(t, err)                 // 再次失败，错误路径 more=false
	assert.Empty(t, l.retryPendingNodes) // 调度即清空（engine.py:118）
}

// ──────────────────────────── Pregel.Run ────────────────────────────

// TestPregel_Run_happyPath 正常跑完返回空 map（engine.py:254-268）
func TestPregel_Run_happyPath(t *testing.T) {
	pg := handGraph()
	res, err := pg.Run(context.Background(), nil)
	assert.NoError(t, err)
	assert.Empty(t, res)
}

// TestPregel_Run_顶层中断 转换 __interrupt__（engine.py:269-284）
func TestPregel_Run_顶层中断(t *testing.T) {
	pg := interruptingGraph()
	res, err := pg.Run(context.Background(), nil)
	assert.NoError(t, err)
	v, ok := res[TaskStatusInterrupt]
	assert.True(t, ok)
	gi := v.(*GraphInterrupt)
	assert.Equal(t, "x", gi.Interrupts[0].Value)
}

// TestPregel_Run_子图中断非顶层 不转换直接上抛（engine.py:284-285）
func TestPregel_Run_子图中断非顶层(t *testing.T) {
	pg := interruptingGraph()
	inner := CreateInnerConfig(PregelConfig{})
	inner.ParentNs = "parent:ns"
	_, err := pg.Run(context.Background(), &inner)
	var gi *GraphInterrupt
	assert.True(t, errors.As(err, &gi))
}

// TestPregel_Run_afterStep 回调收到 loop 且可读写导出字段（engine.py:163-169）
func TestPregel_Run_afterStep(t *testing.T) {
	pg := handGraph()
	steps := 0
	pg.AfterStep = func(_ context.Context, loop *PregelLoop) {
		steps++
		loop.Step += 100 // D6 字段导出的消费面验证
	}
	res, err := pg.Run(context.Background(), nil)
	assert.NoError(t, err)
	assert.Empty(t, res)
	assert.Equal(t, 2, steps) // step0(START)/step1(a) 两步各回调一次
}
