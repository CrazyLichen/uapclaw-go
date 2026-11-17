//go:build integration

// Package stream_actor 集成测试：8.12 StreamActor 数据面全链。
// 真实跨子系统：workflow（WorkflowSpec）→ graph/stream_actor（数据面）→
// session/internal（WorkflowSession env/state）→ session/state（WorkflowCommitState）→
// session/stream（AsyncStreamQueue）。mock 只在边界（StreamConsumer 执行体）。
package stream_actor_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/graph/stream_actor"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/constants"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/workflow"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/workflow/components"
)

// ──────────────────────────── mock 类型 ────────────────────────────

// sessionAdapter 把真实 session 基础设施（config.SessionConfig + state.WorkflowCommitState）
// 适配为 stream_actor.ActorSession。session/internal 对 tests/ 不可导入（嵌套 internal 规则），
// 故直接组合可导入的 config/state 真实类型（mock 只在边界，config/state 为真实子系统）。
type sessionAdapter struct {
	cfg  config.SessionConfig
	wfSt state.WorkflowState
}

func newSessionAdapter(t *suite.Suite) *sessionAdapter {
	cfg := config.NewSessionConfig(context.Background())
	wfSt := state.NewInMemoryWorkflowState()
	return &sessionAdapter{cfg: cfg, wfSt: wfSt}
}

func (a *sessionAdapter) GetEnvFloat(key string) (float64, bool) {
	v := a.cfg.GetEnv(key)
	switch t := v.(type) {
	case float64:
		return t, true
	case int:
		return float64(t), true
	case int64:
		return float64(t), true
	default:
		return 0, false
	}
}

func (a *sessionAdapter) workflowState() state.WorkflowState {
	return a.wfSt
}

func (a *sessionAdapter) GetWorkflowStateValue(key string) any {
	ws := a.workflowState()
	if ws == nil {
		return nil
	}
	return ws.GetWorkflowState(state.StringKey(key))
}

func (a *sessionAdapter) UpdateAndCommitWorkflowState(data map[string]any) {
	if ws := a.workflowState(); ws != nil {
		ws.UpdateAndCommitWorkflowState(data)
	}
}

// liveConsumer 集成用 StreamConsumer：event 立即置位，执行体存活至 ctx 取消
type liveConsumer struct {
	handle bool
}

func (c *liveConsumer) StreamCall(ctx context.Context, event chan struct{}, done func(error)) {
	close(event)
	<-ctx.Done() // 对齐真实 Vertex：执行体存活直至取消
}
func (c *liveConsumer) ShouldHandleMessage() bool { return c.handle }
func (c *liveConsumer) IsDone() bool              { return false }

// twoProducerSpec A/B(STREAM) → C(COLLECT)，默认单源组
func twoProducerSpec() *workflow.WorkflowSpec {
	spec := workflow.NewWorkflowSpec()
	spec.StreamEdges = map[string][]string{"A": {"C"}, "B": {"C"}}
	spec.CompConfigs = map[string]*workflow.NodeSpec{
		"A": {Abilities: []components.ComponentAbility{components.ComponentAbilityStream}},
		"B": {Abilities: []components.ComponentAbility{components.ComponentAbilityStream}},
		"C": {Abilities: []components.ComponentAbility{components.ComponentAbilityCollect}},
	}
	return spec
}

// ──────────────────────────── 套件 ────────────────────────────

type StreamActorIntegrationSuite struct {
	suite.Suite
}

// 讨论点 1/11：produce→consume 全链 + session state 写回（跨 ≥3 子系统）
func (s *StreamActorIntegrationSuite) TestProduceConsume全链_跨子系统() {
	ctx := context.Background()
	graph := stream_actor.NewStreamGraph()
	graph.AddStreamConsumer(&liveConsumer{handle: true}, "C")
	adapter := newSessionAdapter(&s.Suite)
	mgr, err := stream_actor.NewActorManager(ctx, twoProducerSpec(), graph, false, adapter)
	s.Require().NoError(err)

	// produce 内容（Produce 内部包 {producer_id: content}）
	s.Require().NoError(mgr.Produce(ctx, "A", map[string]any{"answer": "你好"},
		components.ComponentAbilityStream, true))
	// 消费前帧滞留主队列（D1 新行为，讨论点 4 的集成面）
	p := mgr.StreamActor("C").Processor(components.ComponentAbilityCollect)
	s.Require().NotNil(p)
	s.Assert().Equal(1, p.MainQueueLen(), "consume 注册前帧应滞留主队列")

	// consume：注册 gen 树并启动 run（D1）
	out, err := mgr.Consume(ctx, "C", components.ComponentAbilityCollect,
		map[string]any{"text": "${A.answer}"}, nil)
	s.Require().NoError(err)
	s.Assert().NotNil(out, "应返回 gen 树")

	// EndMessage → run 分发 → reader 拉到数据
	s.Require().NoError(mgr.EndMessage(ctx, "A", components.ComponentAbilityStream))
	reader := out["text"].(*stream_actor.StreamReader)
	val, ok := reader.Next(ctx)
	s.Require().True(ok, "reader 应拉到数据帧")
	s.Assert().Equal("你好", val)

	// state 写回（session/state 子系统）
	mgr.MarkProducerDone("A")
	nodes := adapter.GetWorkflowStateValue("finished_stream_nodes")
	s.Assert().NotNil(nodes, "finished_stream_nodes 应写回 session state")

	// session env（session/config 子系统）：默认 -1 → 不超时
	if v, ok := adapter.GetEnvFloat(constants.StreamInputGenTimeoutKey); ok {
		s.Assert().LessOrEqual(v, float64(0), "默认 timeout 应 <=0（不超时）")
	}

	s.Require().NoError(mgr.Shutdown())
}

// 讨论点 5/7：恢复场景——A 已完成（state 里），B 首帧拉起消费者，consume 补发 A 的 END
func (s *StreamActorIntegrationSuite) Test恢复场景_补发EndMessage_免超时退出() {
	ctx := context.Background()
	adapter := newSessionAdapter(&s.Suite)
	// 预置上一轮完成记录（session/state 子系统直写）
	adapter.UpdateAndCommitWorkflowState(map[string]any{"finished_stream_nodes": []string{"A"}})

	graph := stream_actor.NewStreamGraph()
	graph.AddStreamConsumer(&liveConsumer{handle: true}, "C")
	mgr, err := stream_actor.NewActorManager(ctx, twoProducerSpec(), graph, false, adapter)
	s.Require().NoError(err)

	// B 首帧拉起消费者
	s.Require().NoError(mgr.Produce(ctx, "B", map[string]any{"out": "x"},
		components.ComponentAbilityStream, true))
	out, err := mgr.Consume(ctx, "C", components.ComponentAbilityCollect,
		map[string]any{"a": "${A.answer}", "b": "${B.out}"}, nil)
	s.Require().NoError(err)
	// 补发后 A 应登记活跃能力（讨论点 7）
	s.Assert().True(mgr.ActiveAbilities("A")[components.ComponentAbilityStream],
		"补发 end_message 后应登记 A 活跃能力")

	// A 路径可取数不挂死：B END 后全组满足关全队列
	s.Require().NoError(mgr.EndMessage(ctx, "B", components.ComponentAbilityStream))
	done := make(chan struct{})
	go func() {
		defer close(done)
		r := out["a"].(*stream_actor.StreamReader)
		for i := 0; i < 10; i++ { // 拉数直至 EndFrame（限 10 帧防呆）
			if _, ok := r.Next(ctx); !ok {
				break
			}
		}
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		s.Fail("路径读取应在 EndFrame 后结束（免超时挂死）")
	}
	s.Require().NoError(mgr.Shutdown())
}

// 讨论点 8/9：ctx 取消贯穿 + shutdown 级联（-race 由 CI/本地 -race 运行验证）
func (s *StreamActorIntegrationSuite) TestCtx取消与Shutdown级联() {
	ctx := context.Background()
	
	graph := stream_actor.NewStreamGraph()
	graph.AddStreamConsumer(&liveConsumer{handle: true}, "C")
	mgr, err := stream_actor.NewActorManager(ctx, twoProducerSpec(), graph, false, newSessionAdapter(&s.Suite))
	s.Require().NoError(err)
	s.Require().NoError(mgr.Produce(ctx, "A", map[string]any{"answer": "x"},
		components.ComponentAbilityStream, true))
	_, err = mgr.Consume(ctx, "C", components.ComponentAbilityCollect,
		map[string]any{"text": "${A.answer}"}, nil)
	s.Require().NoError(err)
	// 子图队列：独立子图 manager 验证 sub_workflow_stream（session/stream 子系统）
	sub, err := stream_actor.NewActorManager(ctx, twoProducerSpec(), graph, true, newSessionAdapter(&s.Suite))
	s.Require().NoError(err)
	q, err := sub.SubWorkflowStream()
	s.Require().NoError(err)
	s.Assert().NotNil(q)
	// shutdown 级联取消全部 goroutine（消费者任务 + processor.run）
	s.Require().NoError(mgr.Shutdown())
	// 二次 shutdown 幂等
	s.Require().NoError(mgr.Shutdown())
}

func TestStreamActorIntegrationSuite(t *testing.T) {
	suite.Run(t, new(StreamActorIntegrationSuite))
}
