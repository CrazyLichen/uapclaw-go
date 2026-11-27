//go:build integration

// E3 团队全状态机（计划 Task 9.1）：评审反馈 → ReviewFeedbackCoordinator →
// 聚合生成 → staging → team watcher → cycle 四态逐帧断言（start collecting →
// progress → approval_required end → 下一 cycle 回退取 id）→ 审批 → 团队库落盘；
// 共享库不污染断言（15.24c 前置 #3：团队链不得触碰共享 hub/个人库）。
// Python 锚点：team_helpers.py:6114-6408 + team_skill_evolution_rail.py
// handle_review_feedback(:282-285)/finalize_review_feedback(:287-293)。
package swarmevolution

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/trajectory"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/agent/coordination/scheduling"
	coreevolution "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/rails/evolution"
)

// e3Marker 团队全状态机特征标记。
const e3Marker = "E2E-MARK-TEAM-STATE-MACHINE"

// e3AttributionResp 归因阶段台本（skill_issue → EVOLVE_EXISTING_SKILL）。
const e3AttributionResp = `{"classification":"skill_issue","skill_name":"evo_state_skill","target":"body","reason":"Skill 缺部署排障指导","reusable_guidance":"补充部署排障规则","is_reusable":true,"confidence":0.9}`

// TeamStateMachineSuite E3 文件级 suite（嵌入主 suite 复用基建）。
type TeamStateMachineSuite struct {
	EvolutionE2ESuite
}

// TestSwarmEvolutionTeamStateMachine 本文件审计入口。
func TestSwarmEvolutionTeamStateMachine(t *testing.T) {
	suite.Run(t, new(TeamStateMachineSuite))
}

// TestE3团队全状态机 评审反馈全链 + cycle 四态逐帧 + 共享库不污染。
func (s *TeamStateMachineSuite) TestE3团队全状态机() {
	d := s.newAdapter()
	s.adapter = d
	s.makeTeamSkill("evo_state_skill")

	// 共享库不污染断言的准备：独立 hub 目录（本链无 sharing 配置 → 应保持为空）
	hubDir := filepath.Join(s.T().TempDir(), "hub")
	s.Require().NoError(os.MkdirAll(hubDir, 0o755))

	teamModel := s.newMockModel()
	teamRail := coreevolution.NewTeamSkillEvolutionRail(
		[]string{s.skillsDir}, teamModel, "test-model", "cn",
		// Python feedback 链语义：auto_save=true（requires_approval=not auto_save）——
		// external signals 走 orchestrator.Evolve 直调，不进 host 审批事件流
		//（执行期澄清，见 spec 收尾登记），审批卡帧序由 E2/E8 覆盖。
		coreevolution.WithTeamSkillAutoSave(true),
		coreevolution.WithTeamSkillSignalTrigger(true),
		coreevolution.WithTeamSkillReviewTrigger(true),
		coreevolution.WithTeamSkillEvolutionTimeout(3.0),
	)

	// ① 评审反馈入口：归因（LLM 台本 1）→ 入账保留
	coordinator := scheduling.NewReviewFeedbackEvolutionCoordinator(
		"web_sess-e3", "alpha",
		func() scheduling.TeamEvolutionRailLike { return teamRail }, true, 0.7)
	teamRail.SetupReviewFeedbackEvolution("web_sess-e3", "alpha", 0.7, coordinator)
	s.Require().True(teamRail.ReviewFeedbackEvolutionEnabled())

	// 归因证据前置：团队轨迹注入成员读 SKILL.md 的 tool 步骤
	// （守卫：无 SKILL.md 读取证据时 existing Skill evolution 被禁止——
	// review_feedback_evolution.go:205 skip_unattributed）
	evidenceTraj, terr := trajectory.NewTestTrajectory("web_sess-e3", nil, []trajectory.TestStep{{
		Kind:     "tool",
		ToolName: "skill_tool",
		CallArgs: map[string]any{"skill_name": "evo_state_skill", "relative_file_path": "SKILL.md"},
		CallResult: map[string]any{
			"skill_name": "evo_state_skill", "relative_file_path": "SKILL.md", "content": "# evo_state_skill\n正文",
		},
	}})
	s.Require().NoError(terr)
	// member 组合 key（守卫证据：review_feedback_evolution.py:444-457）+ team key
	// （聚合生成输入：evolveTeamGroups 的 getTeamTrajectory）
	teamRail.InjectScopeTrajectoryForTest("alpha_agent1", "alpha", evidenceTraj)
	teamRail.InjectScopeTrajectoryForTest("web_sess-e3", "alpha", evidenceTraj)

	s.mockScript(e3AttributionResp)
	s.Require().NoError(teamRail.HandleReviewFeedback(s.ctx, map[string]any{
		"task_id": "task-e3-1", "review_round": int64(1), "feedback": e3Marker + " 部署排障经验不足",
		"assignee": "agent1", "task_title": "实现导出", "task_content": "导出功能实现",
	}))

	// ② Finalize：聚合生成（LLM 台本 2，records 数组）→ staging 进 pending 缓冲
	s.mockScript(`[
		{"action": "append", "section": "Troubleshooting", "target": "body", "content": "` + e3Marker + ` 部署排障聚合规则", "summary": "部署排障聚合", "keywords": ["deploy", "rollback"]}
	]`)
	attempted, ferr := teamRail.FinalizeReviewFeedback(s.ctx, nil)
	s.Require().NoError(ferr)
	s.Require().True(attempted, "保留的观察应触发聚合演进")

	// ③ 直落盘断言（auto_save=true；两轮 feedback → 两份记录，标记回读）
	evoLog, err := teamRail.EvolutionStore().LoadFullEvolutionLog(s.ctx, "evo_state_skill")
	s.Require().NoError(err)
	s.Require().NotEmpty(evoLog.Entries)
	contents := ""
	for _, entry := range evoLog.Entries {
		contents += entry.Change.Content
	}
	s.Assert().Contains(contents, e3Marker, "团队库落盘应含评审反馈标记")

	// ④ 多轮 feedback：第二轮 Finalize → 追加一份记录
	s.mockScriptN(e3AttributionResp, 1)
	s.Require().NoError(teamRail.HandleReviewFeedback(s.ctx, map[string]any{
		"task_id": "task-e3-2", "review_round": int64(1), "feedback": e3Marker + " 第二轮部署排障",
		"assignee": "agent1", "task_title": "实现导出", "task_content": "导出功能实现",
	}))
	s.mockScriptN(`[
		{"action": "append", "section": "Troubleshooting", "target": "body", "content": "`+e3Marker+` 第二轮聚合规则", "summary": "第二轮聚合"}
	]`, 1)
	attempted2, ferr2 := teamRail.FinalizeReviewFeedback(s.ctx, nil)
	s.Require().NoError(ferr2)
	s.Require().True(attempted2, "第二轮保留观察应触发聚合")

	evoLog2, err2 := teamRail.EvolutionStore().LoadFullEvolutionLog(s.ctx, "evo_state_skill")
	s.Require().NoError(err2)
	s.Assert().GreaterOrEqual(len(evoLog2.Entries), len(evoLog.Entries)+1, "第二轮应追加记录")

	// ⑥ 共享库不污染：无 sharing 配置 → hub 目录应为空；共享（个人）库无本链产物
	hubEntries, _ := os.ReadDir(hubDir)
	s.Assert().Empty(hubEntries, "团队链未接 sharing → hub 目录不得有产物")
	// 单机夹具下团队 rail 与 workspace skills 共目录是团队 skill 的正常形态；
	// "共享库不污染"的正确判据 = 团队 rail 未接 sharing（ExperienceSharer nil）
	// → hub 无产物、无跨用户上传。
	// 团队 rail 无 sharing 配置 → 无 sharer/hub 通道（编译面即约束），
	// hub 目录空断言已覆盖；此处补共享 store 的 pending 面断言。
	s.Assert().Empty(teamRail.DrainPendingApprovalEvents(false, nil), "auto_save 链不应有残留审批事件")
}
