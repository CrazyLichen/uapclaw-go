//go:build integration

package attachment_mode

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/suite"

	swruntime "github.com/uapclaw/uapclaw-go/internal/swarm/runtime"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
	"github.com/uapclaw/uapclaw-go/tests/integration/testhelpers"
)

// ──────────────────────────── 结构体 ────────────────────────────

// PlanStateSuite 覆盖 plan 状态机的端到端行为（IT-S2-3~7、IT-S2-13）。
type PlanStateSuite struct {
	isuite.BaseIntegrationSuite
	// env 计划模式测试环境
	env *testhelpers.PlanEnv
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestPlanStateSuite 运行 PlanStateSuite。
func TestPlanStateSuite(t *testing.T) { suite.Run(t, new(PlanStateSuite)) }

// TestIT_S2_3_显式进入改写query 验证 plan_toggle 进入并把原 query 前置约束提醒。
//
// 跨子系统：runtime + adapter + session。
func (s *PlanStateSuite) TestIT_S2_3_显式进入改写query() {
	s.env.Send(itSession, planMode, "帮我重构权限模块", map[string]any{
		"plan_entry_source": "plan_toggle",
	})

	s.Equal("plan", s.env.PlanMode(itSession))
	// 模型收到的本轮用户输入（JSON 信封内的 content）被前置约束提醒，原 query 原样保留在尾部
	rewritten := ""
	for _, msg := range s.env.LastMessages() {
		content := userEnvelopeContent(msg.GetContent().Text())
		if strings.Contains(content, "帮我重构权限模块") {
			rewritten = content
		}
	}
	s.Require().NotEmpty(rewritten, "模型窗口里必须能看到原 query")
	s.Contains(rewritten, "<system-reminder>")
	s.Contains(rewritten, "Plan mode is active")
	s.True(strings.HasSuffix(rewritten, "帮我重构权限模块"), "原 query 必须原样保留在提醒之后")
}

// TestIT_S2_4_退出记忆阻断 验证 check_post_process_exit 记的退出记忆阻断非显式进入。
//
// 跨子系统：runtime + adapter。
func (s *PlanStateSuite) TestIT_S2_4_退出记忆阻断() {
	s.env.SeedState(itSession, "normal", "")
	events := s.env.MarkExitedByPostProcess(itSession, planMode)
	s.Require().Len(events, 1, "工具退出必须产生 plan_mode_exited 事件")
	s.Equal(normalMode, events[0].Mode)

	s.env.Send(itSession, planMode, "继续", nil)

	s.Equal("normal", s.env.PlanMode(itSession), "退出记忆必须阻断自动进入")
	s.True(s.env.HasEvent(itSession, swruntime.PlanModeExitedEvent), "阻断必须下发退出事件")
	s.Nil(s.env.AttachmentOf(itSession, modeInstructionsSection), "阻断轮不应出现 plan 指引附件")
}

// TestIT_S2_5_planSlug残留阻断 验证残留计划文件阻断并清空 slug。
//
// 跨子系统：runtime + session + adapter。
func (s *PlanStateSuite) TestIT_S2_5_planSlug残留阻断() {
	s.env.SeedState(itSession, "normal", "old-plan")
	s.Require().Equal("old-plan", s.env.PlanSlug(itSession))

	s.env.Send(itSession, planMode, "继续", nil)

	s.Equal("normal", s.env.PlanMode(itSession))
	s.Empty(s.env.PlanSlug(itSession), "阻断后必须清空 slug")
	s.True(s.env.HasEvent(itSession, swruntime.PlanModeExitedEvent))
}

// TestIT_S2_6_退出后不被拉回 验证工具退出（exit_plan_mode）后紧接着的 plan 请求被阻断，
// 第二轮恢复正常请求。
//
// 跨子系统：runtime + adapter + context_engine。
func (s *PlanStateSuite) TestIT_S2_6_退出后不被拉回() {
	// ① 显式进入 plan
	s.env.Send(itSession, planMode, "计划一下", map[string]any{"plan_entry_source": "plan_toggle"})
	s.Equal("plan", s.env.PlanMode(itSession))

	// ② 模拟 exit_plan_mode 工具把状态切回 normal，随后 check_post_process_exit 记退出记忆
	s.env.SwitchMode(itSession, "normal")
	s.Require().NotEmpty(s.env.MarkExitedByPostProcess(itSession, planMode))

	// ③ 客户端仍发 plan（无显式来源）→ 退出记忆阻断，状态保持 normal
	s.env.Send(itSession, planMode, "继续", nil)
	s.Equal("normal", s.env.PlanMode(itSession), "退出后不得被拉回 plan")
}

// TestIT_S2_7_跳过矩阵 验证 team 请求 / 非 chat 方法 / 中断续答都不写状态。
//
// 跨子系统：runtime + adapter。
func (s *PlanStateSuite) TestIT_S2_7_跳过矩阵() {
	// team 模式：不参与 plan 状态同步
	s.env.Send(itSession, "team.code.normal", "团队任务", map[string]any{"plan_entry_source": "plan_toggle"})
	s.NotEqual("plan", s.env.PlanMode(itSession), "team 请求不得写 plan 状态")

	// 非 chat 方法
	s.env.SendNonChat(itSession, "session.list", planMode)
	s.NotEqual("plan", s.env.PlanMode(itSession), "非 chat 方法不得写 plan 状态")

	// 中断续答
	s.env.Send(itSession, planMode, "续答", map[string]any{"resume": true})
	s.NotEqual("plan", s.env.PlanMode(itSession), "中断续答不得写 plan 状态")
}

// TestIT_S2_13_跨会话排队占位透传 验证未实现的跨会话排队显式返回 false（不假装已排队）。
func (s *PlanStateSuite) TestIT_S2_13_跨会话排队占位透传() {
	s.env.Send(itSession, planMode, "计划", map[string]any{"plan_entry_source": "plan_toggle"})

	controller := s.env.PlanController()
	s.Require().NotNil(controller)
	s.False(controller.SessionMessageRequiresQueue(itSession), "占位实现必须透传 false")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// userEnvelopeContent 从 BuildUserPrompt 的 JSON 信封里取出 content；非信封原样返回。
func userEnvelopeContent(text string) string {
	start := strings.Index(text, "{")
	if start < 0 {
		return text
	}
	var envelope map[string]any
	if err := json.Unmarshal([]byte(text[start:]), &envelope); err != nil {
		return text
	}
	content, _ := envelope["content"].(string)
	if content == "" {
		return text
	}
	return content
}

// SetupTest 每例隔离环境。
func (s *PlanStateSuite) SetupTest() {
	s.env = testhelpers.NewPlanEnv(s.T(), s.MockLLM, "mock_s2_state")
}
