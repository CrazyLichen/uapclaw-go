//go:build integration

package web_test

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/goal"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/task_loop"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	sessionpkg "github.com/uapclaw/uapclaw-go/internal/swarm/server/session"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// GoalReconnectSuite G3：刷新/切会话/旧租约失效后的补发与隔离（E2A 入口侧）。
type GoalReconnectSuite struct {
	goalContractSuite
}

// GoalReattachSuite G3 R1：ACTIVE goal 在重挂流后的补排（agentcore 域侧）。
// 用 AgentSuite 派生以拿到 NewTestSession / Ctx。
type GoalReattachSuite struct {
	isuite.AgentSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestGoalReconnectSuite E2A 侧套件入口。
func TestGoalReconnectSuite(t *testing.T) {
	suite.Run(t, new(GoalReconnectSuite))
}

// TestGoalReattachSuite 域侧套件入口。
func TestGoalReattachSuite(t *testing.T) {
	suite.Run(t, new(GoalReattachSuite))
}

// SetupTest 每用例启动独立进程内 AgentServer 并隔离数据目录。
func (s *GoalReconnectSuite) SetupTest() {
	s.env = newGoalContractEnv(s.T())
	isolateGoalDataDir(s.T())
}

// TearDownTest 刷盘并还原环境，避免污染后续用例。
func (s *GoalReconnectSuite) TearDownTest() {
	sessionpkg.FlushMetadataQueue(5.0)
	sessionpkg.FlushHistoryQueue(5.0)
	sessionpkg.ClearAllSessionMetadataCache()
	pathutil.ResetCache()
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// newReattachManager 真 store / 真 EventManager / 真回调，不 mock 被测子系统。
// Python: GoalManager(store=SessionGoalStore(session), event_manager=EventManager, ...)
// Go 先例: tests/integration/agentcore/harness/goal_lifecycle_test.go:34-50
func newReattachManager(sess *session.Session, em *task_loop.EventManager) *goal.GoalManager {
	return goal.NewGoalManager(goal.GoalManagerConfig{
		Store:             goal.NewSessionGoalStore(sess),
		EventManager:      em,
		ControlLock:       &sync.Mutex{},
		HasOutputStream:   func() bool { return true },
		CancelActiveRound: func(context.Context, hschema.RoundWorkKind, string, string) {},
		EmitEvent:         func(hschema.InteractionEvent) {},
		NotifyWork:        func() {},
		Language:          "cn",
	})
}

// ──────────────────────────── 测试（E2A 侧） ────────────────────────────

// TestR2_Get幂等 刷新页面等价：get 幂等且不写历史，连发两次状态一致。
// Python: agent_ws_server.py:3717-3718「只读查询，不得兜底新建 session metadata」
// 前端: hooks/useWebSocket.ts:6190 超过 1 分钟无更新主动 performGoalGet 兜底
func (s *GoalReconnectSuite) TestR2_Get幂等() {
	sid := "web-goal-r2"
	for i := 0; i < 2; i++ {
		rid := sendGoal(s.env, sid, map[string]any{"action": "get"}, false)
		resp := s.env.AwaitUnary(rid)
		s.Require().NotNil(resp)
		s.True(resp.OK)
		s.Equal("No goal in this session.", resp.Payload["message"])
	}
	records, err := sessionpkg.ReadHistoryRecords(sid)
	s.Require().NoError(err)
	s.Empty(records, "两次 get 都不得写 user 轮历史")
}

// TestR4_会话隔离 A 的 set 不影响 B 的 get。
// Python: deep_agent.py:3941-3947（绑定后换会话直接 RuntimeError）
// · GoalManager.get_store(session_id) 拒绝跨会话
func (s *GoalReconnectSuite) TestR4_会话隔离() {
	sidA := "web-goal-r4-a"
	sidB := "web-goal-r4-b"
	ridSet := sendGoal(s.env, sidA, map[string]any{
		"action": "set", "objective": "A 的目标", "overwrite_confirmed": true,
	}, false)
	s.env.AwaitUnary(ridSet)
	ridGet := sendGoal(s.env, sidB, map[string]any{"action": "get"}, true)
	frames := goalFrames(s.env.AwaitStreamEnd(ridGet))
	s.Require().NotEmpty(frames)
	for _, c := range frames {
		if eventTypeOf(c) != eventGoalSnapshot {
			continue
		}
		if g, ok := c.Payload["goal"].(map[string]any); ok && g != nil {
			s.NotEqual("A 的目标", g["objective"], "B 会话不得看到 A 的目标")
		}
	}
}

// TestR3_旧Token不摘新流 旧租约 token 不得摘掉当前租约。
// Go: internal/agentcore/harness/schema/interaction.go:443-456 Detach
// Python: openjiuwen/harness/schema/interaction.py:267-276 OutputLeaseManager.detach
// 前端: hooks/useWebSocket.ts:1954 刷新后靠新租约 + goal.updated 自愈 GoalBar
func (s *GoalReconnectSuite) TestR3_旧Token不摘新流() {
	m := hschema.NewOutputLeaseManager()
	lease1 := m.Attach()
	s.Require().NotNil(lease1, "首次挂流应成功")
	s.True(m.HasConsumer(), "挂流后应有消费者")

	s.False(m.Detach("stale-token", true), "旧 token 不得摘掉当前租约")
	s.True(m.HasConsumer(), "旧 token detach 后新流仍在")

	s.True(m.Detach(lease1.Token, true), "正确 token 应成功摘流")
	s.False(m.HasConsumer(), "摘流后无消费者")

	// 摘掉后新租约可以挂上，且旧 token 仍不得影响它
	lease2 := m.Attach()
	s.Require().NotNil(lease2, "摘流后应能重新挂流")
	s.NotEqual(lease1.Token, lease2.Token, "新租约必须是新票号")
	s.False(m.Detach(lease1.Token, true), "旧票号不得影响新租约")
	s.True(m.HasConsumer(), "新流仍存活")
}

// ──────────────────────────── 测试（域侧 R1） ────────────────────────────

// TestR1_活跃Goal在重挂流后补排Work ACTIVE 记录必须能补排一份 goal work，
// 且同一 goal_id+revision 不重复入队。
// Python: deep_agent.py:4048-4070 attach_output 在锁内
//
//	ensure_active_goal_work_locked() + _notify_work()
//
// Go: internal/agentcore/harness/deep_agent.go:1693-1698 AttachOutput 等价实现
func (s *GoalReattachSuite) TestR1_活跃Goal在重挂流后补排Work() {
	ctx := s.Ctx
	sess := s.NewTestSession("web-goal-r1")
	s.Require().NoError(sess.PreRun(ctx))

	em := task_loop.NewEventManager()
	mgr := newReattachManager(sess, em)
	rec, err := mgr.Set(ctx, "重挂后继续", goal.SetOptions{})
	s.Require().NoError(err)
	s.Require().Equal(goal.GoalStatusActive, rec.Status)

	// Set 在 HasOutputStream=true 时已按 Python manager.py:160-162 排过首轮 work，
	// 因此此时重复补排应被 goal_id+revision 去重挡掉（这是正确行为）。
	ok, err := mgr.EnsureActiveGoalWorkLocked()
	s.Require().NoError(err)
	s.False(ok, "set 已排过首轮时不得重复入队")

	// 模拟监督器取走并跑完这一轮（用户随后刷新页面 / 切走会话）。
	// Python: EventManager.next_work() → mark_started → mark_finished
	work := em.NextWork()
	s.Require().NotNil(work, "set 后应有可取的 goal work")
	s.Equal(hschema.RoundWorkKindGoal, work.Kind)
	em.MarkStarted(work)
	em.MarkFinished(work)

	// 重挂流：attach_output 必须能把 ACTIVE 目标补排回来，
	// 否则前端 GoalBar 刷新后要等下一次 goal.updated 才自愈（useWebSocket.ts:1954）。
	ok, err = mgr.EnsureActiveGoalWorkLocked()
	s.Require().NoError(err)
	s.True(ok, "轮次消费完后重挂流应补排一份新 goal work")
	ok2, err := mgr.EnsureActiveGoalWorkLocked()
	s.Require().NoError(err)
	s.False(ok2, "补排后同一 goal_id+revision 不得重复入队")
}
