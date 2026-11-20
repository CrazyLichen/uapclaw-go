//go:build integration

package workflow_end_render

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/graph"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/graph/stream_actor"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/config"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/state"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/tracer"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/workflow/components/ability"
	flow "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/workflow/components/flow"
	workflow "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/workflow/workflowconfig"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// EndRenderSuite 8.17 End 组件流叶消费集成测试。
// 真实跨子系统链路：graph/stream_actor（ActorManager 队列 + StreamReader）→
// graph.Vertex（preStream 信封 + transform 策略）→ workflow/components/flow
// （End 模板渲染消费 StreamReader 叶子）→ runner/callback（组件流事件）。
type EndRenderSuite struct {
	isuite.RunnerSuite
}

// ──────────────────────────── 桩 ────────────────────────────

// fakeInner 最小 InnerSession 桩（对齐 workflow_vertex 集成同款）。
type fakeInner struct {
	cfg config.SessionConfig
	st  state.SessionState
	am  *stream_actor.ActorManager
	id  string
	// wm End 节点 postStream 写流信封必需（isEndNode 路径）
	wm *stream.StreamWriterManager
}

func (f *fakeInner) Config() config.SessionConfig { return f.cfg }
func (f *fakeInner) State() state.SessionState    { return f.st }
func (f *fakeInner) Tracer() *tracer.Tracer       { return nil }
func (f *fakeInner) StreamWriterManager() *stream.StreamWriterManager {
	if f.wm == nil {
		f.wm = stream.NewStreamWriterManager(stream.NewStreamEmitter())
	}
	return f.wm
}
func (f *fakeInner) SessionID() string                        { return f.id }
func (f *fakeInner) Checkpointer() interfaces.Checkpointer    { return nil }
func (f *fakeInner) ActorManager() *stream_actor.ActorManager { return f.am }
func (f *fakeInner) Close() error                             { return nil }
func (f *fakeInner) WorkflowID() string                       { return wfIDEndRender }

// fakeActorSess ActorManager 会话桩（对齐 workflow_vertex 集成同款）。
type fakeActorSess struct{}

func (f *fakeActorSess) GetEnvFloat(string) (float64, bool) { return -1, false }
func (f *fakeActorSess) GetWorkflowStateValue(string) any   { return nil }
func (f *fakeActorSess) UpdateAndCommitWorkflowState(m map[string]any) {
	_ = m
}

// wfIDEndRender 集成测试默认 workflowID
const wfIDEndRender = "wf-end-render"

// ──────────────────────────── 测试入口 ────────────────────────────

// TestEndRenderSuite 测试套入口
func TestEndRenderSuite(t *testing.T) {
	suite.Run(t, new(EndRenderSuite))
}

// ──────────────────────────── 被测世界 ────────────────────────────

// endWorld 真实 End 组件 + 真实 Vertex + 真实 ActorManager 的被测世界
type endWorld struct {
	v      *graph.Vertex
	am     *stream_actor.ActorManager
	parent *fakeInner
	end    *flow.End
}

// newEndWorld 构建 A --stream--> End(B) 的全真实链路（End 为 8.17 真实组件）
func (s *EndRenderSuite) newEndWorld(template string) *endWorld {
	cfg := config.NewSessionConfig(context.Background())
	parent := &fakeInner{cfg: cfg, st: state.NewInMemoryWorkflowState(), id: "s1"}
	cfgEnd, errCfg := flow.NewEndConfig(template)
	s.Require().NoError(errCfg)
	end, errEnd := flow.NewEnd(cfgEnd)
	s.Require().NoError(errEnd)
	end.SetMix()
	v := graph.NewVertex("B", end)
	// 8.15：经 NodeConfig 管路注入节点规格（真实 NodeSession.NodeConfig() 链路）
	wfc, errWfc := workflow.NewWorkflowConfig(&workflow.WorkflowCard{})
	s.Require().NoError(errWfc)
	wfc.Spec.CompConfigs["B"] = &workflow.NodeSpec{
		Abilities:       []ability.ComponentAbility{ability.ComponentAbilityTransform},
		StreamIOConfigs: &workflow.CompIOConfig{InputsSchema: map[string]any{"x": "${A.out}"}},
	}
	cfg.AddWorkflowConfig(wfIDEndRender, wfc)
	s.Require().True(v.Init(parent, nil))
	v.SetEndNode(true)
	spec := workflow.NewWorkflowSpec()
	spec.StreamEdges = map[string][]string{"A": {"B"}}
	spec.CompConfigs = map[string]*workflow.NodeSpec{
		"A": {Abilities: []ability.ComponentAbility{ability.ComponentAbilityStream}},
		"B": {Abilities: []ability.ComponentAbility{ability.ComponentAbilityTransform}},
	}
	g := stream_actor.NewStreamGraph()
	v.RegisterStreamConsumer(g)
	am, errAM := stream_actor.NewActorManager(context.Background(), spec, g, false, &fakeActorSess{})
	s.Require().NoError(errAM)
	parent.am = am
	return &endWorld{v: v, am: am, parent: parent, end: end}
}

// ──────────────────────────── 用例 ────────────────────────────

// TestEnd_StreamReader叶子全链 上游 STREAM 帧 → ActorManager 队列 → 首帧拉起
// Vertex.stream_call → preStream 信封（dict-of-StreamReader）→ End.OnTransform
// → 模板逐段渲染消费 StreamReader → COMPONENT_STREAM_OUTPUT 逐帧事件。
// 回归点：8.17 审查发现的流叶形态缺陷（End 未识别引擎真实 StreamReader 叶子）。
func (s *EndRenderSuite) TestEnd_StreamReader叶子全链() {
	ctx := context.Background()
	w := s.newEndWorld("Hi {{x}}")
	// COMPONENT_STREAM_OUTPUT 逐帧事件监听（runner/callback 子系统）
	type frameEvt struct{ payload any }
	frameCh := make(chan frameEvt, 8)
	var fired int32
	fn := func(ctx context.Context, data *callback.WorkflowEventData) any {
		if data != nil && data.Result != nil && atomic.AddInt32(&fired, 1) > 0 {
			frameCh <- frameEvt{payload: data.Result}
		}
		return nil
	}
	callback.GetCallbackFramework().OnWorkflow(callback.ComponentStreamOutput, fn)
	defer callback.GetCallbackFramework().OffWorkflow(callback.ComponentStreamOutput, fn)
	// 流入侧：真实首帧 spawn（actor.send → stream_call → preStream → 信封）
	s.Require().NoError(w.am.Produce(ctx, "A", map[string]any{"out": "alice"},
		ability.ComponentAbilityStream, true))
	s.Require().NoError(w.am.Produce(ctx, "A", "END_A",
		ability.ComponentAbilityStream, false))
	// 等待两帧信封事件（静态段 "Hi " + 变量段 "alice"）
	var got []string
	deadline := time.After(30 * time.Second)
	for len(got) < 2 {
		select {
		case e := <-frameCh:
			if os, ok := e.payload.(map[string]any); ok {
				if f, ok2 := os["frame"]; ok2 {
					got = append(got, fmt.Sprintf("%v", f))
				}
			}
		case <-deadline:
			s.FailNow("30s 内未收齐两帧信封事件")
		}
	}
	joined := got[0] + got[1]
	s.Contains(joined, "Hi", "静态段应先出帧")
	s.Contains(joined, "alice", "StreamReader 叶子帧应经模板渲染后出帧")
}

// TestEnd_StreamReader惰性消费 上游帧先入队、模板静态段先行的次序验证：
// 流叶帧在模板位置到达前不被消费，上屏顺序严格跟模板一致。
func (s *EndRenderSuite) TestEnd_StreamReader惰性消费() {
	ctx := context.Background()
	// 模板静态段在前：先行帧 "PREFIX" 应先于流叶帧出现
	w := s.newEndWorld("PREFIX {{x}}")
	var order []string
	orderCh := make(chan string, 8)
	fn := func(ctx context.Context, data *callback.WorkflowEventData) any {
		if data == nil || data.Result == nil {
			return nil
		}
		if os, ok := data.Result.(map[string]any); ok {
			if f, ok2 := os["frame"]; ok2 {
				orderCh <- fmt.Sprintf("%v", f)
			}
		}
		return nil
	}
	callback.GetCallbackFramework().OnWorkflow(callback.ComponentStreamOutput, fn)
	defer callback.GetCallbackFramework().OffWorkflow(callback.ComponentStreamOutput, fn)
	s.Require().NoError(w.am.Produce(ctx, "A", map[string]any{"out": "leaf"},
		ability.ComponentAbilityStream, true))
	s.Require().NoError(w.am.Produce(ctx, "A", "END_A",
		ability.ComponentAbilityStream, false))
	deadline := time.After(30 * time.Second)
	for len(order) < 2 {
		select {
		case o := <-orderCh:
			order = append(order, o)
		case <-deadline:
			s.FailNow("30s 内未收齐两帧")
		}
	}
	s.Contains(order[0], "PREFIX", "静态段必须先于流叶帧（位置驱动惰性消费）")
	s.Contains(order[1], "leaf", "流叶帧随后")
}
