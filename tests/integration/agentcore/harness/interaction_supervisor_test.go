//go:build integration

package harness_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// interactionSupervisorSuite 交互监督器集成测试。
//
// 覆盖不依赖 LLM 的四条链路：单租约抢占、会话流经转发器投递、断线清场、重连。
// 真实一轮问答由 real_llm 套件覆盖（登记为 9.89 回填测试项）。
type interactionSupervisorSuite struct {
	// suite testify 套件基类
	suite.Suite
	// agent 被测 DeepAgent
	agent *harness.DeepAgent
	// sess 绑定的会话
	sess *session.Session
}

// SetupTest 启动交互监督器
func (s *interactionSupervisorSuite) SetupTest() {
	card := agentschema.NewAgentCard(agentschema.WithAgentName("it-interaction"))
	s.agent = harness.NewDeepAgent(card)
	s.sess = session.NewSession()
	s.Require().NoError(s.agent.Start(context.Background(), s.sess))
}

// TearDownTest 停止交互监督器
func (s *interactionSupervisorSuite) TearDownTest() {
	_ = s.agent.Stop(context.Background())
}

// TestLeaseForwardAndReconnect 单租约 + 转发 + 重连
// 对齐 spec IT1/IT3
func (s *interactionSupervisorSuite) TestLeaseForwardAndReconnect() {
	ctx := context.Background()

	first := s.agent.AttachOutput(ctx)
	s.Require().NotNil(first, "首个宿主应拿到租约")
	s.Nil(s.agent.AttachOutput(ctx), "第二个宿主不得拿到租约")
	s.True(s.agent.HasOutputStream())

	// 会话流 → 转发器 → 租约队列
	s.Require().NotNil(s.sess.StreamEmitter())
	s.Require().NoError(s.sess.StreamEmitter().Emit(ctx,
		&stream.OutputSchema{Type: "chunk", Index: 0}))
	item, ok := s.agent.NextOutput(ctx, first.Lease())
	s.Require().True(ok)
	s.Equal("chunk", item.(*stream.OutputSchema).Type)

	// 断线清场
	first.Close(ctx, true)
	s.False(s.agent.HasOutputStream())

	// 重连后仍可收新帧
	second := s.agent.AttachOutput(ctx)
	s.Require().NotNil(second)
	s.Require().NoError(s.sess.StreamEmitter().Emit(ctx,
		&stream.OutputSchema{Type: "after-reconnect"}))
	item, ok = s.agent.NextOutput(ctx, second.Lease())
	s.Require().True(ok)
	s.Equal("after-reconnect", item.(*stream.OutputSchema).Type)
}

// TestDetachDiscardsPending 断线丢弃待办，新宿主流无残留
// 对齐 spec IT2
func (s *interactionSupervisorSuite) TestDetachDiscardsPending() {
	ctx := context.Background()

	a := s.agent.AttachOutput(ctx)
	s.Require().NotNil(a)
	s.Require().NoError(s.agent.SendInput(ctx, hschema.SendInputRequest{
		RequestID: "r1", Inputs: map[string]any{"query": "残留"},
	}))
	s.True(s.agent.EventManager().HasPendingWork(), "输入应先入待办队列")

	a.Close(ctx, true)
	s.False(s.agent.EventManager().HasPendingWork(), "断线必须丢弃待办")

	b := s.agent.AttachOutput(ctx)
	s.Require().NotNil(b)
	readCtx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()
	_, ok := s.agent.NextOutput(readCtx, b.Lease())
	s.False(ok, "新租约不得读到上一任的待办输出")
}

// TestInteractionSupervisorSuite 套件入口
func TestInteractionSupervisorSuite(t *testing.T) {
	suite.Run(t, new(interactionSupervisorSuite))
}
