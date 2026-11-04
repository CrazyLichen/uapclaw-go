//go:build integration

package attachment_mode

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/sections"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/subagent"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
	"github.com/uapclaw/uapclaw-go/tests/integration/testhelpers"
)

// ──────────────────────────── 结构体 ────────────────────────────

// RailLifecycleSuite 覆盖 rail 装配幂等与 todo 体系（IT-S2-8/9/10/12）。
type RailLifecycleSuite struct {
	isuite.BaseIntegrationSuite
	// env 计划模式测试环境
	env *testhelpers.PlanEnv
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestRailLifecycleSuite 运行 RailLifecycleSuite。
func TestRailLifecycleSuite(t *testing.T) { suite.Run(t, new(RailLifecycleSuite)) }

// TestIT_S2_8_rail幂等 验证多次 plan/normal 切换不重复挂 rail（plan 专属能力恒在）。
//
// 跨子系统：adapter + rails。
func (s *RailLifecycleSuite) TestIT_S2_8_rail幂等() {
	taskPlanning := reflect.TypeOf(&rails.TaskPlanningRail{})
	subagentRail := reflect.TypeOf(&subagent.SubagentRail{})

	for i := 0; i < 3; i++ {
		s.env.Send(itSession, planMode, "计划", map[string]any{"plan_entry_source": "plan_toggle"})
		s.Equal(1, s.env.RailCount(taskPlanning), "第 %d 次 plan 后 TaskPlanningRail 应恒为 1", i+1)
		s.Equal(1, s.env.RailCount(subagentRail), "第 %d 次 plan 后 SubagentRail 应恒为 1", i+1)
	}

	s.env.Send(itSession, normalMode, "退出", nil)
	s.Equal(1, s.env.RailCount(taskPlanning), "normal 期间 TaskPlanningRail 仍常驻（上游合并后不过载）")
	s.Equal(1, s.env.RailCount(subagentRail), "normal 期间 SubagentRail 仍常驻")

	s.env.Send(itSession, planMode, "再进", map[string]any{"plan_entry_source": "plan_toggle"})
	s.Equal(1, s.env.RailCount(taskPlanning))
}

// TestIT_S2_9_plan指引走附件 验证 plan 指引进历史附件，退出后清空附件。
//
// 跨子系统：rails(agent_mode) + context_engine。
func (s *RailLifecycleSuite) TestIT_S2_9_plan指引走附件() {
	s.env.Send(itSession, planMode, "计划", map[string]any{"plan_entry_source": "plan_toggle"})

	attachment := s.env.AttachmentOf(itSession, modeInstructionsSection)
	s.Require().NotNil(attachment, "进入 plan 应产生 mode_instructions 附件")
	s.Equal("agent_core.agent_mode_rail", attachment.Source)
	s.Equal("runtime", string(attachment.Kind))
	s.False(s.env.HasSection(modeInstructionsSection), "system prompt 不应注入该节")

	s.env.Send(itSession, normalMode, "退出", nil)
	s.Nil(s.env.AttachmentOf(itSession, modeInstructionsSection), "退出后应清除 mode_instructions 附件")
}

// TestIT_S2_10_todo默认不注入 验证默认不写 todo 提示词节，但工具仍可见。
//
// 跨子系统：rails(task_planning) + context_engine。
func (s *RailLifecycleSuite) TestIT_S2_10_todo默认不注入() {
	s.env.Send(itSession, normalMode, "普通对话", nil)

	s.False(s.env.HasSection(sections.SectionTodo), "system prompt 不应有 todo 节")
	s.NotContains(s.env.LastSystemText(), "# 待办事项", "模型实收 system 不得含 todo 静态节")
	for _, name := range []string{"todo_create", "todo_list", "todo_get", "todo_modify"} {
		s.True(s.env.ToolVisible(name), "工具 %s 应仍可见", name)
	}
}

// TestIT_S2_12_静态节留系统提示词 按上游语义：静态节走 add_section，不做附件迁移。
//
// 跨子系统：context_assemble + rails + context_engine。
func (s *RailLifecycleSuite) TestIT_S2_12_静态节留系统提示词() {
	s.env.Send(itSession, normalMode, "干活", nil)

	s.True(s.env.HasSection(sections.SectionTools), "工具规则节应留在系统提示词（上游 add_section）")
	s.True(s.env.HasSection(sections.SectionWorkspace), "workspace 节应留在系统提示词")
	s.Nil(s.env.AttachmentOf(itSession, sections.SectionTools), "静态节不得被误迁成附件")
	s.Nil(s.env.AttachmentOf(itSession, sections.SectionWorkspace))
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// SetupTest 每例隔离环境。
func (s *RailLifecycleSuite) SetupTest() {
	s.env = testhelpers.NewPlanEnv(s.T(), s.MockLLM, "mock_s2_rails")
}
