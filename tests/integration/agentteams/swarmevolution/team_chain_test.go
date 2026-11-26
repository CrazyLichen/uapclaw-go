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

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/signal"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm"
	coreevolution "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/rails/evolution"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/rails/evolution/review"
)

// e2Marker 团队链落盘特征标记。
const e2Marker = "E2E-MARK-TEAM-端口排查"

// e2GenerateResp 团队提案生成台本（user_intent 信号 → GenerateUserPatch 契约：
// {"need_patch":true,"action":...,"section":...,"content":...}，非 records 数组）。
const e2GenerateResp = `{"need_patch": true, "action": "append", "section": "Troubleshooting", "content": "` + e2Marker + ` 团队部署排障", "summary": "团队部署排障经验"}`

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

// TestE2TeamLeader被动链 真实链：signal staging（emitRecordApprovalEvent 进
// pending 缓冲）→ 团队 watcher drain（1s 轮询）→ 推送审批卡 + approval_required
// end → host 从审批帧取 request_id → rail.ApproveRecord → 落盘。
// （新版 request_user_evolution 为 followup 壳——见 skill_evolution_rail.py:1066-1100
// ——生成+审批事件由 staging 半区承接，RequestUserEvolution 旧式生成路径已删。）
func (s *EvolutionE2ESuite) TestE2TeamLeader被动链() {
	d := s.newAdapter()
	s.adapter = d
	s.makeTeamSkill("evo_team_skill")

	// 团队 rail 复用独立 MockLLM（生成台本注入其队列）
	teamModel := s.newMockModel()
	teamRail := s.newTeamRail(teamModel)
	s.mockScriptN(e2GenerateResp, 4) // 团队生成可能多次调用 LLM，队列给足

	// ① staging：signal 编排（user_intent 信号）→ emitRecordApprovalEvent 进缓冲
	// Python: _handle_evolution_from_signals（requires_approval=true，emit_host_events=true）
	intentSignal := signal.MakeEvolutionSignal(
		schema.UserIntentSignal, "Instructions",
		"部署排障经验（"+e2Marker+"）",
		signal.WithSkillName("evo_team_skill"),
		signal.WithSource("explicit_request"),
	)
	staged, err := teamRail.HandleEvolutionFromSignalsWithResult(
		s.ctx, "evo_team_skill", nil, []*signal.EvolutionSignal{intentSignal},
		false, "部署排障经验（"+e2Marker+"）",
		[]map[string]any{{"role": "user", "content": "部署排障经验"}}, true)
	s.Require().NoError(err)
	if staged != nil {
		s.T().Logf("staged request_id=%s payload=%d", staged.RequestID, len(staged.PendingChange.Payload))
	} else {
		s.T().Logf("staged=nil（requiresApproval 时 Finalize 恒 nil，事件应在缓冲）")
	}
	// 注意：DrainPendingApprovalEvents 是破坏性的（drain 后清缓冲），
	// 不得在此探针——事件必须留给 watcher drain

	// ② 启动团队 watcher（1s 轮询 drain）	// ② 启动团队 watcher（1s 轮询 drain）
	watcherCtx, cancel := contextWithCancel(s.ctx)
	defer cancel()
	d.StartTeamEvolutionWatcherForTest(watcherCtx, "ch-e2e", "web_sess-e2e", teamRail)

	// ③ 轮询推送帧：审批卡（含 request_id）+ approval_required end（E3 cycle 帧）
	deadline := time.Now().Add(15 * time.Second)
	var sawApproval, sawEnd bool
	approvalRequestID := ""
	for time.Now().Before(deadline) {
		for _, payload := range s.pushPayloads("chat.evolution_status") {
			if stage, _ := payload["stage"].(string); stage == "approval_required" {
				sawEnd = true
			}
		}
		for _, frame := range s.pushFrames {
			payload, ok := frame["payload"].(map[string]any)
			if !ok {
				continue
			}
			if t, _ := payload["type"].(string); t != "chat.ask_user_question" {
				continue
			}
			sawApproval = true
			if approvalRequestID == "" {
				if rid, _ := payload["request_id"].(string); rid != "" {
					approvalRequestID = rid
				}
			}
		}
		if sawApproval && sawEnd && approvalRequestID != "" {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	s.Assert().True(sawApproval, "watcher 应推送审批卡，帧数=%d", len(s.pushFrames))
	s.Assert().True(sawEnd, "watcher 应推送 approval_required end")
	s.Require().NotEmpty(approvalRequestID, "审批卡应带 request_id")

	// ④ leader 批准（补答链的 rail 半区：approve_record）
	err = teamRail.ApproveRecord(s.ctx, approvalRequestID)
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

func keysOf(m map[string]any) []string {
	out := []string{}
	for k := range m {
		out = append(out, k)
	}
	return out
}

func mapOrEmpty(m map[string]any) any { return "" }
