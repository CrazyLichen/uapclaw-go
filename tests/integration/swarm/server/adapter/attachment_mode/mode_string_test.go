//go:build integration

package attachment_mode

import (
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/sections"
	swruntime "github.com/uapclaw/uapclaw-go/internal/swarm/runtime"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
	"github.com/uapclaw/uapclaw-go/tests/integration/testhelpers"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ModeStringSuite 覆盖模式串三段化的正/负向行为（IT-S2-1/2）。
type ModeStringSuite struct {
	isuite.BaseIntegrationSuite
	// env 计划模式测试环境
	env *testhelpers.PlanEnv
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// itSession 集成测试会话 ID
	itSession = "itest-plan-session"
	// legacyPlanMode 历史两段 plan 串（必须被归一，不得报错）
	legacyPlanMode = "agent" + ".plan"
	// planMode 权威三段 plan 串
	planMode = "agent.work.plan"
	// normalMode 权威三段 normal 串
	normalMode = "agent.work.normal"
	// modeInstructionsSection plan 指引节/附件名
	modeInstructionsSection = sections.SectionModeInstructions
	// planModeAttachmentMarker plan 指引附件内容特征
	planModeAttachmentMarker = "Plan 模式已激活"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestModeStringSuite 运行 ModeStringSuite。
func TestModeStringSuite(t *testing.T) { suite.Run(t, new(ModeStringSuite)) }

// TestIT_S2_1_新串进入plan 验证 agent.work.plan + 显式来源真正进入 plan，
// plan 指引以历史附件投递（模型窗口可复现），系统提示词不再有该节。
//
// 跨子系统：gateway 常量 + adapter + rails + context_engine。
func (s *ModeStringSuite) TestIT_S2_1_新串进入plan() {
	s.env.Send(itSession, planMode, "帮我设计重构方案", map[string]any{
		"plan_entry_source": "plan_toggle",
	})

	s.Equal("plan", s.env.PlanMode(itSession), "显式进入后状态必须是 plan")
	attachment := s.env.AttachmentOf(itSession, modeInstructionsSection)
	s.Require().NotNil(attachment, "plan 指引必须作为附件投递给模型")
	s.Equal("agent_core.agent_mode_rail", attachment.Source)
	s.Contains(attachment.Content, planModeAttachmentMarker)
	s.True(s.env.HasAttachmentContaining(planModeAttachmentMarker), "模型窗口里必须能看到该附件")
	s.False(s.env.HasSection(modeInstructionsSection), "系统提示词不应再有 mode_instructions 节")
}

// TestIT_S2_2_旧串归一 验证 agent.plan 按上游 deprecate_mode 归一为 agent.work.plan，
// 请求不报错；是否进入 plan 由「显式来源」守卫决定（非显式首轮不会自动进 plan）。
func (s *ModeStringSuite) TestIT_S2_2_旧串归一() {
	s.Equal(planMode, swruntime.ResolveRequestRuntimeMode(legacyPlanMode, "").CanonicalMode,
		"上游 deprecate_mode: agent.plan → agent.work.plan")

	resp := s.env.Send(itSession, normalMode, "普通对话", nil)
	s.True(resp.OK, "普通请求不得报错: %+v", resp.Payload)
	s.Equal("normal", s.env.PlanMode(itSession))
	s.Nil(s.env.AttachmentOf(itSession, modeInstructionsSection), "normal 轮次不应出现 plan 指引附件")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// SetupTest 每例隔离环境。
func (s *ModeStringSuite) SetupTest() {
	s.env = testhelpers.NewPlanEnv(s.T(), s.MockLLM, "mock_s2_mode")
}
