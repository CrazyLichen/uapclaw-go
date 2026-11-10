//go:build integration

package harness_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	teamharness "github.com/uapclaw/uapclaw-go/internal/agent_teams/harness"
	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	harnessprotocol "github.com/uapclaw/uapclaw-go/internal/agentcore/harness_protocol"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// MidRoundAnnounceSuite F-129 中途唤醒 + 门控删除集成测试。
//
// 跨度（规则 7）：agent_teams/tracker + agent_teams/harness + agentcore/session。
type MidRoundAnnounceSuite struct {
	isuite.AgentSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestMidRoundAnnounceSuite 套件入口。
func TestMidRoundAnnounceSuite(t *testing.T) {
	suite.Run(t, new(MidRoundAnnounceSuite))
}

// TestI06_AnnounceTaskRevisions_无会话静默 未启动时 announce 静默跳过（不 panic）。
func (s *MidRoundAnnounceSuite) TestI06_AnnounceTaskRevisions_无会话静默() {
	spec := atschema.NewDeepAgentSpec()
	h := teamharness.TeamHarnessBuild(spec, "teammate", "alice", false, nil, nil)
	s.Require().NotNil(h)

	s.Nil(h.CurrentSession(), "未启动时无当前会话")
	s.NoError(h.AnnounceTaskRevisions(s.Ctx), "无会话时应静默跳过")
}

// TestI06b_AnnounceTaskRevisions_无tracker静默 tracker 为 nil 时静默。
func (s *MidRoundAnnounceSuite) TestI06b_AnnounceTaskRevisions_无tracker静默() {
	spec := atschema.NewDeepAgentSpec()
	h := teamharness.TeamHarnessBuild(spec, "teammate", "alice", false, nil, nil)
	s.Require().NotNil(h)

	s.NoError(h.AnnounceTaskRevisions(s.Ctx))
}

// TestI07_首轮不再等门控 Teammate 启动后邮箱轮询立即入队、轮次正常结束。
//
// 上游 kernel.py:709 的断言：不再有 FirstIterationGate 等待。本用例通过
// 「启动后能立刻投递并开轮」间接验证——若还有门控等待，在 rail 初始化
// 延迟的场景下轮次不会开始。
func (s *MidRoundAnnounceSuite) TestI07_首轮不再等门控() {
	spec, err := s.NewSpecWithMockLLM(2)
	s.Require().NoError(err)
	spec.Language = "cn"

	h := teamharness.TeamHarnessBuild(spec, "teammate", "alice", false, nil, nil)
	s.Require().NotNil(h)
	s.Require().NotNil(h.Native())

	sess := session.CreateAgentSession("", h.Native().DeepAgent().Card(), nil)
	s.Require().NoError(h.Start(s.Ctx, sess))
	defer func() { _ = h.Stop(s.Ctx) }()

	// 启动即 IDLE（不是 RUNNING），第一条 Send 应立刻开轮
	s.Equal(harnessprotocol.HarnessStateIdle, h.State())

	s.NoError(h.Send(s.Ctx, "开始工作", false))

	// 关键断言：轮次必须真的开起来（旧门控会在这里永久阻塞）。
	// 不断言"最终回到 IDLE"——那取决于 MockLLM 响应队列是否够用，
	// 属测试夹具细节，不是本用例要验证的门控语义。
	s.Eventually(func() bool {
		return h.State() == harnessprotocol.HarnessStateRunning
	}, 15*time.Second, 20*time.Millisecond, "Send 后轮次应立即开始（无门控阻塞）")
}
