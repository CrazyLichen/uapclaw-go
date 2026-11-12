//go:build integration

package adapter_test

import (
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	swruntime "github.com/uapclaw/uapclaw-go/internal/swarm/runtime"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/adapter"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// PlanControllerStateSuite 验证 PlanModeController 经 PlanStateAccessor
// 读写真实 DeepAdapter 状态的跨层链路。
// S3：控制器归 AgentRuntime 持有，adapter 只提供状态存储（PlanStateAccessor）。
//
// 从 internal/swarm/server/adapter/deep_adapter_config_test.go 迁来：该用例
// 跨 runtime（PlanModeController）+ adapter（PlanStateAccessor）两层，而
// server/adapter 已被 runtime 反向依赖（P-7 断环），同包测试无法再 import runtime。
type PlanControllerStateSuite struct {
	isuite.BaseIntegrationSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestPlanControllerStateSuite 运行本套件。
func TestPlanControllerStateSuite(t *testing.T) {
	suite.Run(t, new(PlanControllerStateSuite))
}

// TestEnsureState 显式进入 plan 后状态落到承接该会话的 Session 上。
func (s *PlanControllerStateSuite) TestEnsureState() {
	d := adapter.NewDeepAdapter()
	controller := swruntime.NewPlanModeController()

	// 空 sessionID / 非 plan 请求：不应写状态（EnsureState 内部守卫）
	_, err := controller.EnsureState(s.Ctx, swruntime.PlanRequest{}, d.PlanStateAccessor())
	s.Require().NoError(err, "EnsureState 空请求")

	// 完整链路：真实 DeepAgent + 真实 session，显式进入 plan 后应写入 plan 状态
	agent := harness.NewDeepAgent(agentschema.NewAgentCard())
	d.SetInstanceForTest(agent)
	_, err = controller.EnsureState(s.Ctx, swruntime.PlanRequest{
		SessionID:   "sess-plan-state",
		Mode:        "agent.work.plan",
		ReqMethod:   "chat.send",
		EntrySource: swruntime.PlanEntrySourcePlanToggle,
	}, d.PlanStateAccessor())
	s.Require().NoError(err, "EnsureState")

	// plan 状态必须落在「即将承接该会话的那个 Session」上：EnsureState 会通过
	// ensure_live_session_instance 建出会话级 child 并绑定会话，因此优先按
	// live instance 的绑定会话读；没有 live 时回落到新建会话（根路径）。
	readAgent := agent
	loaded := session.CreateAgentSession("sess-plan-state", agent.Card(), nil)
	if live := d.GetLiveSessionInstance("sess-plan-state"); live != nil {
		if bound := liveAgentSession(live, "sess-plan-state"); bound != nil {
			readAgent, loaded = live, bound
		}
	}
	s.Require().NoError(loaded.PreRun(s.Ctx), "PreRun")
	s.Equal("plan", readAgent.LoadState(loaded).PlanMode.Mode, "进入 plan 后状态应为 plan")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// liveAgentSession 取 live 实例上绑定该会话的那个 Session。
// 复刻 adapter 包内 resolveLiveAgentSession（非导出，外部测试包不可见）：
// 依次看 InteractionSession / LoopSession，取 sessionID 匹配者。
func liveAgentSession(inst *harness.DeepAgent, sessionID string) *session.Session {
	if inst == nil {
		return nil
	}
	for _, sess := range []*session.Session{inst.InteractionSession(), inst.LoopSession()} {
		if sess == nil {
			continue
		}
		if sess.GetSessionID() == sessionID {
			return sess
		}
	}
	return nil
}
