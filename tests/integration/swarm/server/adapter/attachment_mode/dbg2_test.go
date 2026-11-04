//go:build integration

package attachment_mode

import (
	"testing"

	"github.com/stretchr/testify/suite"
	hprompts "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
	"github.com/uapclaw/uapclaw-go/tests/integration/testhelpers"
)

type Dbg2Suite struct {
	isuite.BaseIntegrationSuite
	env *testhelpers.PlanEnv
}

func TestDbg2Suite(t *testing.T) { suite.Run(t, new(Dbg2Suite)) }

func (s *Dbg2Suite) SetupTest() {
	s.env = testhelpers.NewPlanEnv(s.T(), s.MockLLM, "dbg2_plan")
}

func (s *Dbg2Suite) TestDbgRail() {
	s.env.Send(itSession, planMode, "计划", map[string]any{"plan_entry_source": "plan_toggle"})
	s.T().Logf("after plan: state=%q attach=%v active=%v", s.env.PlanMode(itSession),
		s.env.AttachmentOf(itSession, modeInstructionsSection) != nil,
		len(s.env.PlanController().ActiveSessions()))
	s.env.Send(itSession, normalMode, "退出", nil)
	s.T().Logf("after normal: state=%q attach=%v active=%v pushes=%v", s.env.PlanMode(itSession),
		s.env.AttachmentOf(itSession, modeInstructionsSection) != nil,
		len(s.env.PlanController().ActiveSessions()), s.env.Events(itSession))
	all, _ := s.env.Inst().PromptAttachments().ListByFilter(hprompts.AttachmentFilter{})
	s.T().Logf("all attachments=%d mgr=%p inst=%p", len(all), s.env.Inst().PromptAttachments(), s.env.Inst())
	for _, a := range all {
		s.T().Logf("  att session=%q section=%q", a.SessionID, a.Section)
	}
	s.T().Logf("system=%q", firstN(s.env.LastSystemText(), 200))
}

func firstN(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
