//go:build integration

package harness_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// interactionSupervisorSuite 交互监督器集成测试。
//
// 覆盖：单租约抢占、会话流经转发器投递、断线清场重连、以及**完整一轮**的
// 生产路径（Start → Attach → Send → RunOneRound → 边界帧收尾）。
// 真实 LLM 由 MockLLM 替代，被测子系统（harness / task_loop / session）不 mock。
type interactionSupervisorSuite struct {
	// AgentSuite 提供 Ctx / MockLLM / NewDeepAgentForTest / NewTestSession
	isuite.AgentSuite
}

// newPlainAgent 构造不接模型的 DeepAgent 并启动交互监督器（租约类用例）
func (s *interactionSupervisorSuite) newPlainAgent() (*harness.DeepAgent, *session.Session) {
	s.T().Helper()
	card := agentschema.NewAgentCard(agentschema.WithAgentName("it-interaction"))
	d := harness.NewDeepAgent(card)
	sess := session.NewSession()
	s.Require().NoError(d.Start(context.Background(), sess))
	s.T().Cleanup(func() { _ = d.Stop(context.Background()) })
	return d, sess
}

// TestLeaseForwardAndReconnect 单租约 + 转发 + 重连
// 对齐 spec IT1/IT3
func (s *interactionSupervisorSuite) TestLeaseForwardAndReconnect() {
	ctx := context.Background()
	d, sess := s.newPlainAgent()

	first, attachErr := d.AttachOutput(ctx)
	s.Require().NoError(attachErr)
	s.Require().NotNil(first, "首个宿主应拿到租约")
	secondTry, secondErr := d.AttachOutput(ctx)
	s.Require().NoError(secondErr)
	s.Nil(secondTry, "第二个宿主不得拿到租约")
	s.True(d.HasOutputStream())

	// 会话流 → 转发器 → 租约队列
	s.Require().NotNil(sess.StreamEmitter())
	s.Require().NoError(sess.StreamEmitter().Emit(ctx,
		&stream.OutputSchema{Type: "chunk", Index: 0}))
	item, ok := d.NextOutput(ctx, first.Lease())
	s.Require().True(ok)
	s.Equal("chunk", item.(*stream.OutputSchema).Type)

	// 断线清场
	first.Close(ctx, true)
	s.False(d.HasOutputStream())

	// 重连后仍可收新帧
	second, secondAttachErr := d.AttachOutput(ctx)
	s.Require().NoError(secondAttachErr)
	s.Require().NotNil(second)
	s.Require().NoError(sess.StreamEmitter().Emit(ctx,
		&stream.OutputSchema{Type: "after-reconnect"}))
	item, ok = d.NextOutput(ctx, second.Lease())
	s.Require().True(ok)
	s.Equal("after-reconnect", item.(*stream.OutputSchema).Type)
}

// TestDetachDiscardsPending 断线丢弃待办，新宿主流无残留
// 对齐 spec IT2
func (s *interactionSupervisorSuite) TestDetachDiscardsPending() {
	ctx := context.Background()
	d, _ := s.newPlainAgent()

	a, aErr := d.AttachOutput(ctx)
	s.Require().NoError(aErr)
	s.Require().NotNil(a)
	s.Require().NoError(d.SendInput(ctx, hschema.SendInputRequest{
		RequestID: "r1", Inputs: map[string]any{"query": "残留"},
	}))
	s.True(d.EventManager().HasPendingWork(), "输入应先进待办队列（本轮尚未被 supervisor 消费）")

	a.Close(ctx, true)
	s.False(d.EventManager().HasPendingWork(), "断线必须丢弃待办")

	b, bErr := d.AttachOutput(ctx)
	s.Require().NoError(bErr)
	s.Require().NotNil(b)
	readCtx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()
	_, ok := d.NextOutput(readCtx, b.Lease())
	s.False(ok, "新租约不得读到上一任的待办输出")
}

// TestRoundEndToEnd 完整一轮：Start → Attach → Send → RunOneRound → 流结束
//
// 这是唯一真正执行 RunOneRound / executeRound 真分支 / supervisor work 分支的用例：
// 其余单测都用 roundOverride 跳过真实轮次。
func (s *interactionSupervisorSuite) TestRoundEndToEnd() {
	ctx := s.Ctx
	s.MockLLM.SetResponses(mockllm.CreateTextResponse("你好，我是助手。"))

	agent, err := s.NewDeepAgentForTest(ctx, hconfig.CreateDeepAgentParams{})
	s.Require().NoError(err, "创建 DeepAgent 失败")

	sess := s.NewTestSession("interaction-e2e")
	s.Require().NoError(sess.PreRun(ctx), "Session PreRun 失败")

	s.Require().NoError(agent.Start(ctx, sess), "Start 失败")
	s.T().Cleanup(func() { _ = agent.Stop(context.Background()) })

	out, outErr := agent.AttachOutput(ctx)
	s.Require().NoError(outErr)
	s.Require().NotNil(out, "应拿到唯一输出租约")

	s.Require().NoError(agent.SendInput(ctx, hschema.SendInputRequest{
		RequestID: "req-e2e", Inputs: map[string]any{"query": "你好"},
	}))

	readCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	frames := 0
	for {
		_, ok := agent.NextOutput(readCtx, out.Lease())
		if !ok {
			break
		}
		frames++
		if frames > 500 {
			s.FailNow("输出帧数异常：超过 500 帧仍未结束")
		}
	}

	s.Greater(frames, 0, "一轮执行必须至少产出一帧，否则监督器未真正跑轮")
	s.False(agent.HasOutputStream(), "轮次收尾后租约应已由边界帧/哨兵释放")
	s.Equal(hschema.InteractionPhaseIdle, agent.Phase(), "轮次结束后应回到 idle")
}

// TestInteractionSupervisorSuite 套件入口
func TestInteractionSupervisorSuite(t *testing.T) {
	suite.Run(t, new(interactionSupervisorSuite))
}
