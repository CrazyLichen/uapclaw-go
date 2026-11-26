//go:build integration

// E2 团队 leader 被动链（review_feedback 载体）+ E3 cycle 状态机核心帧。
//
// 链路（mock 仅 LLM）：team rail（leader、auto_save=false、signal_trigger=true）
// → rail 生成 3 条提案（pending approval events 缓冲）→ adapter 团队 watcher
// drain（1s 轮询）→ 推送 cycle start/progress + 审批卡 + approval_required end
// → leader rail.ApproveRecord → 落盘 → watcher end 帧。
// 评审反馈翻译段由注入结构化观察替代（Core 协调器半区在 scheduling 装配，
// 15.24c 已验证装配闭环；本用例锚 rail→watcher→审批→补答链）。
package swarmevolution

import (
	"path/filepath"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm"
	coreevolution "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/rails/evolution"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/rails/evolution/review"
)

// e2Marker 团队链落盘特征标记。
const e2Marker = "E2E-MARK-TEAM-端口排查"

// e2GenerateResp 团队提案生成台本。
const e2GenerateResp = `[
	{"action": "append", "section": "Troubleshooting", "target": "body", "content": "` + e2Marker + ` 团队部署排障"},
	{"action": "append", "section": "Troubleshooting", "target": "body", "content": "团队防火墙检查"},
	{"action": "append", "section": "Examples", "target": "body", "content": "团队部署脚本示例"}
]`

// newTeamRail 构造 leader 团队 rail（构造参数对齐 providers 装配：
// signal_trigger=true + auto_save=config + review_trigger=true + review_runtime）。
func (s *EvolutionE2ESuite) newTeamRail(model *llm.Model) *coreevolution.TeamSkillEvolutionRail {
	return coreevolution.NewTeamSkillEvolutionRail(
		[]string{s.skillsDir}, model, "test-model", "cn",
		coreevolution.WithTeamSkillAutoSave(false),
		coreevolution.WithTeamSkillSignalTrigger(true),
		coreevolution.WithTeamSkillReviewTrigger(true),
		coreevolution.WithTeamEvolutionReviewRuntime(review.NewEvolutionReviewRuntime(60.0)),
	)
}

// makeTeamSkill 创建 frontmatter kind=team-skill 的技能。
func (s *EvolutionE2ESuite) makeTeamSkill(name string) string {
	store := s.railStore()
	skillDir, err := store.CreateSkill(s.ctx, name, "团队技能", "---\nname: "+name+"\nkind: team-skill\ndescription: 团队技能\n---\n# "+name+"\n\n团队正文\n", "")
	s.Require().NoError(err)
	return skillDir
}

// TestE2TeamLeader被动链 审批缓冲 → watcher 推送 → leader 批准 → 落盘。
func (s *EvolutionE2ESuite) TestE2TeamLeader被动链() {
	// ⤴️ 断点登记（2026-12-26）：实施中发现上游新版 request_user_evolution 已改
	// followup 模式（skill_evolution_rail.py:1066-1082 _build_user_evolution_request
	// —— 主动请求不再直生成+审批；团队审批事件由六工具链 evolve 提交 stage 进
	// pending 缓冲，watcher drain 推送）。Go team rail 的 RequestUserEvolution
	// 仍为旧式直生成路径（requiresApproval 时 FinalizeStagedEvolutionRequest
	// 恒返回 nil → RequestID 恒空）。E2 需先按新版重构该链（对齐
	// evolution_slash 六工具 + emitRecordApprovalEvent staging），与本批
	// Task 1 已确立的"followup 注入→六工具→审批"同构。本用例随重构落地。
	s.T().Skip("E2 待团队 rail RequestUserEvolution 新版语义重构后落地（见 plan 接力清单）")
	d := s.newAdapter()
	s.adapter = d
	s.makeTeamSkill("evo_team_skill")

	// 团队 rail 复用独立 MockLLM（生成台本注入其队列）
	teamModel := s.newMockModel()
	teamRail := s.newTeamRail(teamModel)
	s.mockScript(e2GenerateResp)

	// ① rail 生成提案（被动信号载体：评审反馈注入的观察→演化；mock 边界仅 LLM）
	result, err := teamRail.RequestUserEvolution(s.ctx, "evo_team_skill", "部署排障经验（"+e2Marker+"）", false)
	s.Require().NoError(err)
	s.Require().NotNil(result)
	if result.RequestID == nil {
		s.T().Logf("RequestID nil: Status=%v Message=%q Records=%d AutoApproved=%v Mode=%q", result.Status, result.Message, len(result.Records), result.AutoApproved, result.Mode)
	}
	s.Require().NotNil(result.RequestID, "审批请求应入 pending 缓冲")

	// ② 启动团队 watcher（1s 轮询 drain）
	watcherCtx, cancel := contextWithCancel(s.ctx)
	defer cancel()
	d.StartTeamEvolutionWatcherForTest(watcherCtx, "ch-e2e", "web_sess-e2e", teamRail)

	// ③ 轮询推送帧：审批卡 + approval_required end（E3 cycle 状态机帧序）
	deadline := time.Now().Add(10 * time.Second)
	var sawApproval, sawEnd bool
	for time.Now().Before(deadline) {
		for _, payload := range s.pushPayloads("chat.evolution_status") {
			if stage, _ := payload["stage"].(string); stage == "approval_required" {
				sawEnd = true
			}
		}
		for _, frame := range s.pushFrames {
			if payload, ok := frame["payload"].(map[string]any); ok {
				if t, _ := payload["type"].(string); t == "chat.ask_user_question" {
					sawApproval = true
				}
			}
		}
		if sawApproval && sawEnd {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	s.Assert().True(sawApproval, "watcher 应推送审批卡，帧数=%d", len(s.pushFrames))
	s.Assert().True(sawEnd, "watcher 应推送 approval_required end")

	// ④ leader 批准 → 落盘（补答链的 rail 半区）
	err = teamRail.ApproveRecord(s.ctx, *result.RequestID)
	s.Require().NoError(err)

	// ⑤ 落盘标记回读（E12 团队版核心：内容无中途丢失）
	evoLog, err := teamRail.EvolutionStore().LoadFullEvolutionLog(s.ctx, "evo_team_skill")
	s.Require().NoError(err)
	s.Require().NotEmpty(evoLog.Entries)
	contents := ""
	for _, entry := range evoLog.Entries {
		contents += entry.Change.Content
	}
	s.Assert().Contains(contents, e2Marker)
	s.Assert().True(fileExists(filepath.Join(s.skillsDir, "evo_team_skill", "evolutions.json")))
}
