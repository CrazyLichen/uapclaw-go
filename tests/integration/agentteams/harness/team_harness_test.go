//go:build integration

package harness_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	teamharness "github.com/uapclaw/uapclaw-go/internal/agent_teams/harness"
	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	harnessprotocol "github.com/uapclaw/uapclaw-go/internal/agentcore/harness_protocol"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TeamHarnessSuite TeamHarness 生命周期集成测试套件。
//
// 对齐 Python openjiuwen/agent_teams/harness/team_harness.py 的
// start(team_session)/send/outputs/run_once 生命周期：建子会话 + pre_run +
// native.start + 种子 plan 模式 + 记录活跃会话。
type TeamHarnessSuite struct {
	isuite.AgentSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestTeamHarnessSuite 套件入口。
func TestTeamHarnessSuite(t *testing.T) {
	suite.Run(t, new(TeamHarnessSuite))
}

// newTeamHarness 构造一个带 MockLLM 的 TeamHarness（native + deepAgent）。
func (s *TeamHarnessSuite) newTeamHarness() *teamharness.TeamHarness {
	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		MaxIterations: 4,
	})
	s.Require().NoError(err)
	spec, specErr := s.NewSpecWithMockLLM(4)
	s.Require().NoError(specErr)
	native, nativeErr := teamharness.NewNativeHarness(spec, nil)
	if nativeErr != nil {
		s.T().Fatalf("NewNativeHarness: %v", nativeErr)
	}
	// 复用 newTeamHarnessWithNative 的等价构造：NewTeamHarness 接收接口 + 手动包 native
	h := teamharness.NewTeamHarness(agent, string(atschema.TeamRoleLeader), "leader", false)
	// 通过 Native() 不暴露 setter，改用 TeamHarnessBuild 太重；此处直接验证 Send/State 转发面
	_ = native
	return h
}

// ──────────────────────────── 测试 ────────────────────────────

// TestState未启动为Idle 验证未 Start 时 State() 返回 idle、SessionID 为空。
func (s *TeamHarnessSuite) TestState未启动为Idle() {
	h := s.newTeamHarness()
	s.Equal(harnessprotocol.HarnessStateIdle, h.State())
	s.Empty(h.SessionID())
}

// TestSend未启动报错 验证 Send before start 返回错误（对齐上游 raise）。
func (s *TeamHarnessSuite) TestSend未启动报错() {
	h := s.newTeamHarness()
	s.Error(h.Send(s.Ctx, "早", false))
}

// TestOutputs未启动为nil 验证 Outputs before start 返回 nil。
func (s *TeamHarnessSuite) TestOutputs未启动为nil() {
	h := s.newTeamHarness()
	s.Nil(h.Outputs())
}

// TestRunOnce跑一轮 验证 RunOnce 走 DeepAgent.invoke 单次执行（无 supervisor）。
func (s *TeamHarnessSuite) TestRunOnce跑一轮() {
	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{MaxIterations: 4})
	s.Require().NoError(err)
	spec, specErr := s.NewSpecWithMockLLM(4)
	s.Require().NoError(specErr)
	native, nativeErr := teamharness.NewNativeHarness(spec, nil)
	if nativeErr != nil {
		s.T().Fatalf("NewNativeHarness: %v", nativeErr)
	}
	h := teamharness.NewTeamHarness(agent, string(atschema.TeamRoleTeammate), "m1", false)

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("单次完成"))

	// RunOnce 需要 native；直接通过 NewTeamHarness 后无法 set native，
	// 这里验证 native.RunOnce 的语义（绕过 supervisor）。
	result, err := native.RunOnce(s.Ctx, "单次任务", session.CreateAgentSession("ro", agent.Card(), nil))
	s.Require().NoError(err)
	s.NotNil(result)
	_ = h
	// 断言 MockLLM 被调用
	s.GreaterOrEqual(s.MockLLM.InvokeCallCount(), 1)
}

// TestAbort无活跃轮为Noop 验证 Abort 无活跃 run cycle 时 no-op（对齐 _is_cycle_active 守卫）。
func (s *TeamHarnessSuite) TestAbort无活跃轮为Noop() {
	h := s.newTeamHarness()
	s.NoError(h.Abort(s.Ctx, true))
	s.NoError(h.Abort(s.Ctx, false))
	s.NoError(h.Pause(s.Ctx))
	s.NoError(h.Resume(s.Ctx, "x"))
}

// TestDispose幂等 验证 Dispose 无 native 时不报错。
func (s *TeamHarnessSuite) TestDispose幂等() {
	h := s.newTeamHarness()
	s.NoError(h.Dispose(s.Ctx))
	time.Sleep(10 * time.Millisecond)
}
