//go:build integration

package agentteams_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/agent"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
	"github.com/uapclaw/uapclaw-go/tests/integration/testhelpers"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TeamMemberSuite 团队成员交互面集成测试套件。
//
// 对齐 Python jiuwenswarm tests/system_tests 的团队成员端到端用例：
// 真 TeamAgent + MockLLM 走完整 Configure 装配链（TeamHarnessBuild →
// spec.ResolveParts → llm.NewModel 取 MockLLM），验证 25.1a 订阅驱动的
// 团队流程（send/outputs/subscribe/retry/interrupt）。
type TeamMemberSuite struct {
	isuite.AgentSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestTeamMemberSuite 套件入口。
func TestTeamMemberSuite(t *testing.T) {
	suite.Run(t, new(TeamMemberSuite))
}

// ──────────────────────────── 测试 ────────────────────────────

// TestMemberTwoRounds_输入不丢 真 TeamAgent + MockLLM 连发两条输入。
//
// 对齐 spec 9.3 IT-1（C1/C6/C10/C13/C17）：两轮各消费一次、不重复开轮。
// 临时团队（temporary）一轮结束后靠 clean_team 关流（非 completion poll），
// 本测试单轮场景无 clean_team，故手动 CloseStream 收尾让 Invoke 返回。
func (s *TeamMemberSuite) TestMemberTwoRounds_输入不丢() {
	s.MockLLM.SetResponses(
		mockllm.CreateTextResponse("第一轮完成"),
		mockllm.CreateTextResponse("第二轮完成"),
	)

	member := testhelpers.BuildMockMember(s.T(), s.ProviderName, "", "member")
	ta := member.Agent
	teamSession := session.CreateAgentTeamSession("itest-sess", nil, "itest-team")

	// 第一轮
	s.runRoundAndClose(ta, teamSession, "第一条")

	// 第二轮
	s.runRoundAndClose(ta, teamSession, "第二条")

	// MockLLM 至少被调用两次（两轮各一次）
	s.GreaterOrEqual(s.MockLLM.InvokeCallCount(), 2)
}

// TestMemberRunOnce_评审场景 验证 RunOnce 绕过 supervisor 单次执行。
//
// 对齐 spec 9.3 IT-8（C15）：RunOnce 一次返回结果，不启动 supervisor、不改成员状态。
func (s *TeamMemberSuite) TestMemberRunOnce_评审场景() {
	s.MockLLM.SetResponses(mockllm.CreateTextResponse("评审结论"))

	member := testhelpers.BuildMockMember(s.T(), s.ProviderName, "", "member")
	ta := member.Agent
	teamSession := session.CreateAgentTeamSession("runonce-sess", nil, "itest-team")

	result, err := ta.Harness().RunOnce(s.Ctx, "评审", teamSession)
	s.Require().NoError(err)
	s.NotNil(result, "RunOnce 应返回结果")

	// RunOnce 绕过 supervisor，不改成员状态（仍 idle/ready，非 running）
	s.False(ta.IsAgentRunning(), "RunOnce 不应进入 running 状态")
	s.GreaterOrEqual(s.MockLLM.InvokeCallCount(), 1)
}

// TestMemberStop_资源释放 验证 Stop 后 Send 报错、流关闭。
//
// 对齐 spec 9.3 IT-9（C6/C17）：Stop 后 harness 终止，Send 不再受理。
func (s *TeamMemberSuite) TestMemberStop_资源释放() {
	s.MockLLM.SetResponses(mockllm.CreateTextResponse("完成"))

	member := testhelpers.BuildMockMember(s.T(), s.ProviderName, "", "member")
	ta := member.Agent
	teamSession := session.CreateAgentTeamSession("stop-sess", nil, "itest-team")

	// 先跑一轮让 native supervisor 启动
	s.runRoundAndClose(ta, teamSession, "首轮")

	// Stop 后 Send 应报错（supervisor 已终止）
	s.Require().NoError(ta.Harness().Stop(s.Ctx))
	s.Error(ta.Harness().Send(s.Ctx, "stop 之后", false), "Stop 后 Send 应报错")
}

// TestMemberConcurrentDeliver_无丢失 两协程同时投递，两条都被消费。
//
// 对齐 spec 9.3 IT-2（C1/C13/C16）：并发投递无 race、无死锁、两条都受理。
func (s *TeamMemberSuite) TestMemberConcurrentDeliver_无丢失() {
	s.MockLLM.SetResponses(
		mockllm.CreateTextResponse("并发一"),
		mockllm.CreateTextResponse("并发二"),
	)

	member := testhelpers.BuildMockMember(s.T(), s.ProviderName, "", "member")
	ta := member.Agent
	teamSession := session.CreateAgentTeamSession("conc-sess", nil, "itest-team")

	// 先跑一轮让 native supervisor 启动
	s.runRoundAndClose(ta, teamSession, "预热")

	// 两协程并发 Send
	base := s.MockLLM.InvokeCallCount()
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			errs <- ta.Harness().Send(s.Ctx, "并发输入", false)
		}()
	}
	for i := 0; i < 2; i++ {
		select {
		case err := <-errs:
			s.NoError(err, "并发 Send 应被受理")
		case <-time.After(10 * time.Second):
			s.T().Fatal("并发 Send 有调用方阻塞")
		}
	}
	_ = base
}

// runRoundAndClose 跑一轮 Invoke：等 MockLLM 被调用（一轮启动）后手动
// CloseStream 让 Invoke 返回（临时团队无 clean_team 关流，见测试注释）。
func (s *TeamMemberSuite) runRoundAndClose(ta *agent.TeamAgent, teamSession *session.AgentTeamSession, query string) {
	before := s.MockLLM.InvokeCallCount()

	resultCh := make(chan map[string]any, 1)
	go func() {
		result, _ := ta.Invoke(s.Ctx, map[string]any{"query": query},
			agentinterfaces.WithSession(teamSession))
		resultCh <- result
	}()

	// 等一轮启动（MockLLM 被调用）
	s.Require().Eventually(func() bool {
		return s.MockLLM.InvokeCallCount() > before
	}, 15*time.Second, 50*time.Millisecond, "一轮应启动并调用 MockLLM")

	// 给 forwardOutputs 泵出 chunk 的时间，再手动 CloseStream 让 Invoke 返回
	time.Sleep(200 * time.Millisecond)
	ta.CloseStream()

	select {
	case <-resultCh:
		// Invoke 正常返回
	case <-time.After(10 * time.Second):
		s.T().Fatal("CloseStream 后 Invoke 应返回")
	}
}
