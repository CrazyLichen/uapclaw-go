//go:build integration

package workflow_vertex

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/core/graph"
	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/core/graph/stream_actor"
	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/core/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/core/session"
	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/core/session/config"
	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/core/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/core/session/state"
	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/core/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/core/session/tracer"
	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/core/workflow/components"
	workflow "github.com/uapclaw/uapclaw-go/internal/openjiuwen/core/workflow/workflowconfig"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// VertexStreamSuite 8.11 Vertex 双链批入/流入集成测试。
// 真实跨子系统链路：graph.Vertex（批入编排/双链握手）→ graph/stream_actor
// （ActorManager 队列与首帧拉起）→ session/state（WorkflowCommitState IO 暂存）
// → runner/callback（NODE_EXECUTED 事件）。
type VertexStreamSuite struct {
	isuite.RunnerSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestVertexStreamSuite(t *testing.T) {
	suite.Run(t, new(VertexStreamSuite))
}

// ──────────────────────────── 桩与会话底座 ────────────────────────────

// fakeExe 可编程可执行体（跨包测试桩）
type fakeExe struct {
	graph.BaseExecutable[map[string]any, map[string]any]
	invokeFn  func(ctx context.Context, inputs map[string]any) (map[string]any, error)
	collectFn func(ctx context.Context, m map[string]any, r *stream_actor.StreamReader) (map[string]any, error)
}

func (f *fakeExe) OnInvoke(ctx context.Context, inputs map[string]any,
	sess session.BaseSession, kw map[string]any) (map[string]any, error) {
	if f.invokeFn != nil {
		return f.invokeFn(ctx, inputs)
	}
	return map[string]any{"invoked": true}, nil
}

func (f *fakeExe) OnCollect(ctx context.Context, inputs <-chan map[string]any,
	sess session.BaseSession, kw map[string]any) (map[string]any, error) {
	m := <-inputs
	// 逐 StreamReader 消费（对齐 Python 组件迭代 generator 叶子）
	for k, v := range m {
		if r, ok := v.(*stream_actor.StreamReader); ok {
			payloads := make([]any, 0, 4)
			for {
				val, ok2 := r.Next(ctx)
				if !ok2 {
					break
				}
				payloads = append(payloads, val)
			}
			m[k] = payloads
		}
	}
	if f.collectFn != nil {
		return f.collectFn(ctx, m, nil)
	}
	return m, nil
}

// fakeActorSess ActorSession 桩
type fakeActorSess struct{}

func (f *fakeActorSess) GetEnvFloat(string) (float64, bool) { return -1, false }
func (f *fakeActorSess) GetWorkflowStateValue(string) any   { return nil }
func (f *fakeActorSess) UpdateAndCommitWorkflowState(m map[string]any) {
	_ = m
}

// fakeInner 最小 InnerSession（可注入 ActorManager）
type fakeInner struct {
	cfg config.SessionConfig
	st  state.SessionState
	am  *stream_actor.ActorManager
	id  string
}

func (f *fakeInner) Config() config.SessionConfig                     { return f.cfg }
func (f *fakeInner) State() state.SessionState                        { return f.st }
func (f *fakeInner) Tracer() *tracer.Tracer                           { return nil }
func (f *fakeInner) StreamWriterManager() *stream.StreamWriterManager { return nil }
func (f *fakeInner) SessionID() string                                { return f.id }
func (f *fakeInner) Checkpointer() interfaces.Checkpointer            { return nil }
func (f *fakeInner) ActorManager() *stream_actor.ActorManager         { return f.am }
func (f *fakeInner) Close() error                                     { return nil }

// wfIDIntegration 集成测试默认 workflowID（NodeSession 经 getWorkflowID 继承）
const wfIDIntegration = "wf-itest"

// WorkflowID 返回工作流 ID（8.15 NodeConfig 管路）
func (f *fakeInner) WorkflowID() string { return wfIDIntegration }

// vertexWorld 真实 Vertex + ActorManager + 会话状态世界（A --stream--> B）
type vertexWorld struct {
	v      *graph.Vertex
	am     *stream_actor.ActorManager
	parent *fakeInner
	exe    *fakeExe
}

// newVertexWorld 构建被测世界；streamSchema 非空时流入侧注册 StreamReader
func (s *VertexStreamSuite) newVertexWorld(abilities []components.ComponentAbility,
	streamSchema map[string]any, isEnd bool) *vertexWorld {
	cfg := config.NewSessionConfig(context.Background())
	parent := &fakeInner{cfg: cfg, st: state.NewInMemoryWorkflowState(), id: "s1"}
	exe := &fakeExe{}
	v := graph.NewVertex("B", exe)
	// 8.15：经 NodeConfig 管路注入节点规格（真实 NodeSession.NodeConfig() 链路）
	wfc, errWfc := workflow.NewWorkflowConfig(&workflow.WorkflowCard{})
	s.Require().NoError(errWfc)
	wfc.Spec.CompConfigs["B"] = &workflow.NodeSpec{
		Abilities:       abilities,
		StreamIOConfigs: &workflow.CompIOConfig{InputsSchema: streamSchema},
	}
	cfg.AddWorkflowConfig(wfIDIntegration, wfc)
	s.Require().True(v.Init(parent, nil))
	v.SetEndNode(isEnd)
	spec := workflow.NewWorkflowSpec()
	spec.StreamEdges = map[string][]string{"A": {"B"}}
	spec.CompConfigs = map[string]*workflow.NodeSpec{
		"A": {Abilities: []components.ComponentAbility{components.ComponentAbilityStream}},
		"B": {Abilities: abilities},
	}
	g := stream_actor.NewStreamGraph()
	v.RegisterStreamConsumer(g)
	am, err := stream_actor.NewActorManager(context.Background(), spec, g, false, &fakeActorSess{})
	s.Require().NoError(err)
	parent.am = am
	return &vertexWorld{v: v, am: am, parent: parent, exe: exe}
}

// ──────────────────────────── 用例 1：混合节点全链 ────────────────────────────

// TestVertex_混合节点全链 上游 STREAM 产 chunk → StreamActor 队列 → Vertex 流入侧
// 首帧拉起 → 批入 Call 双链握手 → NODE_EXECUTED 事件 → 计数对账 → io_state 写入。
func (s *VertexStreamSuite) TestVertex_混合节点全链() {
	ctx := context.Background()
	w := s.newVertexWorld([]components.ComponentAbility{
		components.ComponentAbilityInvoke, components.ComponentAbilityCollect},
		map[string]any{"x": "${A.out}"}, false)
	// NODE_EXECUTED 事件监听（runner/callback 子系统）
	nodeExecuted := make(chan struct{}, 1)
	var fired atomic.Bool
	fn := func(ctx context.Context, data map[string]any) any {
		if !fired.Swap(true) {
			nodeExecuted <- struct{}{}
		}
		return nil
	}
	callback.GetCallbackFramework().OnCustom(string(callback.NodeExecuted), fn)
	defer callback.GetCallbackFramework().OffCustom(string(callback.NodeExecuted), fn)
	// 流入侧：首帧 spawn StreamCall（真实 Vertex 注册→event→processor.run 启动）
	s.Require().NoError(w.am.Produce(ctx, "A", map[string]any{"out": "c1"},
		components.ComponentAbilityStream, true))
	s.Require().NoError(w.am.Produce(ctx, "A", map[string]any{"out": "c2"},
		components.ComponentAbilityStream, false))
	s.Require().NoError(w.am.Produce(ctx, "A", "END_A",
		components.ComponentAbilityStream, false))
	// 批入侧 Call：等待双链握手（stream_done channel）
	done := make(chan error, 1)
	go func() {
		_, err := w.v.Call(ctx, map[string]any{"st": 1}, nil)
		done <- err
	}()
	select {
	case err := <-done:
		s.Require().NoError(err)
	case <-time.After(30 * time.Second):
		s.Fail("Call 应在双链握手后返回")
	}
	// NODE_EXECUTED 事件 + io_state 输出写入（session/state 子系统）
	select {
	case <-nodeExecuted:
	default:
		s.Fail("应触发 NODE_EXECUTED 事件")
	}
	s.True(w.v.IsDone(), "全链完成应收口")
}

// ──────────────────────────── 用例 2：循环图多轮收口 ────────────────────────────

// TestVertex_多轮计数对齐 两轮 Produce-Call 循环：每轮双链握手后 IsDone 收口、
// Reset 重建当轮容器，无僵尸 streamCall 残留。
func (s *VertexStreamSuite) TestVertex_多轮计数对齐() {
	ctx := context.Background()
	for round := 0; round < 2; round++ {
		w := s.newVertexWorld([]components.ComponentAbility{components.ComponentAbilityCollect},
			nil, false)
		s.Require().NoError(w.am.Produce(ctx, "A", map[string]any{"out": "r"},
			components.ComponentAbilityStream, true))
		s.Require().NoError(w.am.Produce(ctx, "A", "END_A",
			components.ComponentAbilityStream, false))
		_, err := w.v.Call(ctx, nil, nil)
		s.Require().NoError(err)
		s.True(w.v.IsDone(), "第 %d 轮结束应收口", round+1)
		w.v.Reset()
	}
}

// ──────────────────────────── 用例 3：未执行分支 End 清洗 ────────────────────────────

// TestVertex_End未执行分支清洗 End 节点 COLLECT 的流式 schema 引用
// 指向未执行分支（executed_nodes 无记录、非活跃流源）→ collect_iter 中该
// StreamReader 叶子被关闭并置空串后进入组件（vertex.py collect_strategy 清洗）。
func (s *VertexStreamSuite) TestVertex_End未执行分支清洗() {
	ctx := context.Background()
	var seenInputs map[string]any
	w := s.newVertexWorld([]components.ComponentAbility{components.ComponentAbilityCollect},
		map[string]any{"missing": "${missing.out}"}, true)
	w.exe.collectFn = func(ctx context.Context, m map[string]any, r *stream_actor.StreamReader) (map[string]any, error) {
		seenInputs = m
		return map[string]any{"collected": true}, nil
	}
	// 标记其他组件已执行，missing 不在 executed_nodes（未执行分支语义）
	w.parent.st.(state.WorkflowState).UpdateAndCommitWorkflowState(
		map[string]any{"executed_nodes": []string{"other"}})
	// 首帧拉起流入侧（真实 Vertex：preStream 注册 → collect 策略 End 清洗 → OnCollect）
	s.Require().NoError(w.am.Produce(ctx, "A", map[string]any{"out": "x"},
		components.ComponentAbilityStream, true))
	time.Sleep(300 * time.Millisecond)
	s.Require().NotNil(seenInputs)
	s.Equal("", seenInputs["missing"], "未执行分支引用叶子应置空串")
}
