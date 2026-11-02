//go:build integration

package agentteams_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/suite"
	agentteams "github.com/uapclaw/uapclaw-go/internal/agent_teams"
	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	sessionstate "github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interrupt"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	teamharness "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/team"
	aitsuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// TeamInterruptResumeSuite 团队中断恢复契约集成套件。
type TeamInterruptResumeSuite struct {
	aitsuite.BaseIntegrationSuite
}

// TestTeamInterruptResumeSuite 运行团队中断恢复套件。
func TestTeamInterruptResumeSuite(t *testing.T) {
	suite.Run(t, new(TeamInterruptResumeSuite))
}

// newInterruptState 构造一份带一个待确认请求的中断状态。
func newInterruptState() *agentschema.ToolInterruptionState {
	return &agentschema.ToolInterruptionState{
		InterruptedTools: map[string]*agentschema.ToolInterruptEntry{
			"tc-1": {
				InterruptRequests: map[string]agentschema.InterruptRequester{
					"n-1": &agentschema.InterruptRequest{Message: "确认执行？"},
				},
			},
		},
	}
}

// TestInterruptKeyContract handler 写、会话直读、handler 复读三者的键与类型必须一致。
//
// 这是 S-01 断链的根因契约：TeamHarness 用 sessionstate.StringKey(InterruptionKey)
// 读 *ToolInterruptionState，写入方必须落在同一位置，否则线上表现是
// "用户回答了但系统说没有待处理中断"。
func (s *TeamInterruptResumeSuite) TestInterruptKeyContract() {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentID("agent-int"),
		agentschema.WithAgentName("agent-int"),
	)
	sess := session.CreateAgentSession("sess-int-1", card, nil)
	s.Require().NotNil(sess)

	handler := interrupt.NewToolInterruptHandler(nil)
	handler.Save(newInterruptState(), sess)

	// 1) handler 自己读得回来
	loaded := handler.Load(sess)
	s.Require().NotNil(loaded)
	s.Contains(loaded.InterruptedTools, "tc-1")

	// 2) 会话直读：键与类型必须与 TeamHarness 的读取方式一致
	raw, err := sess.GetState(sessionstate.StringKey(agentschema.InterruptionKey))
	s.Require().NoError(err)
	state, ok := raw.(*agentschema.ToolInterruptionState)
	s.Require().True(ok, "中断状态应以 *ToolInterruptionState 存储，实际 %T", raw)
	s.Require().NotNil(state.InterruptedTools["tc-1"])
	s.Contains(state.InterruptedTools["tc-1"].InterruptRequests, "n-1")

	// 3) Clear 之后会话里不再有中断状态
	handler.Clear(sess)
	cleared, err := sess.GetState(sessionstate.StringKey(agentschema.InterruptionKey))
	s.NoError(err)
	s.Nil(cleared)
}

// TestHarnessReadsSessionState team 侧 harness 读取同一 session 时判据成立。
//
// 走真实 DeepAgent + 真实会话：HasPendingInterrupt 无 loop/active 会话时返回 false，
// 但 Rail 转调必须落到底层 Agent（原实现是空桩，运行中挂载 Rail 静默无效）。
func (s *TeamInterruptResumeSuite) TestHarnessReadsSessionState() {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentID("agent-rail"),
		agentschema.WithAgentName("agent-rail"),
	)
	deepAgent := harness.NewDeepAgent(card)
	s.Require().NotNil(deepAgent)

	h := agentteams.NewTeamHarness(deepAgent, nil, string(atschema.TeamRoleLeader), "leader-1", false)
	s.Require().NotNil(h)

	// 无活跃/循环会话 → 判定为"无待处理中断"（不能 panic、不能误判）
	s.False(h.HasPendingInterrupt())
	s.False(h.IsPendingInterruptResumeValid(nil))

	// Rail 注册/注销必须真正转到底层 DeepAgent
	rail := agentinterfaces.NewBaseRail()
	s.Require().NoError(h.RegisterRail(context.Background(), rail))
	s.Require().NoError(h.UnregisterRail(context.Background(), rail))

	// 非 Rail 入参必须报错（原实现静默返回 nil）
	s.Error(h.RegisterRail(context.Background(), "not-a-rail"))

	// 运行中投递失败必须可识别（M-02）：无活跃会话 → 具名 error 而非 nil
	s.Error(h.Steer(context.Background(), "转向"))
	s.Error(h.FollowUp(context.Background(), "追加"))
}

// TestSteerFailsFastWithoutLoop 无任务循环时 steer/follow_up 必须快速失败。
//
// M-02：Python 靠异常传播，Go 用哨兵 error。这里从集成层锁住
// "真实 DeepAgent + 真实会话缺失" 的组合不会 panic、且返回可识别错误。
func (s *TeamInterruptResumeSuite) TestSteerFailsFastWithoutLoop() {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentID("agent-steer"),
		agentschema.WithAgentName("agent-steer"),
	)
	d := harness.NewDeepAgent(card)
	s.Require().NotNil(d)

	s.ErrorIs(d.Steer(context.Background(), "转向", nil), harness.ErrNoActiveLoop)
	s.ErrorIs(d.FollowUp(context.Background(), "追加", "", nil), harness.ErrNoActiveLoop)
}

// TestEnsureMonitorWithoutRuntime 运行时未激活时返回哨兵而不是 nil。
//
// T-10：上层原本无法区分"监控没起来"与"监控已启动"，只能靠日志猜。
func (s *TeamInterruptResumeSuite) TestEnsureMonitorWithoutRuntime() {
	tm := teamharness.NewTeamManager()
	s.Require().NotNil(tm)

	err := tm.EnsureMonitor(s.Ctx, "sess-no-runtime", "team-no-runtime", false)
	s.ErrorIs(err, teamharness.ErrNoActiveRuntime)
	s.Nil(tm.GetMonitor("sess-no-runtime"), "未启动 monitor 时不得注册 handler")
}
