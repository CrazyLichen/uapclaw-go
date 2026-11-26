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
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/signal"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/trajectory"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/stream"
	coreevolution "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/rails/evolution"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/rails/evolution/review"
	evolutionlogic "github.com/uapclaw/uapclaw-go/internal/swarm/server/adapter/evolution/logic"
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

// ──────────────────────────── E8：watcher 超时 hidden end + 重入 ────────────────────────────

// TestE8Watcher超时HiddenEnd与重入 超时 → hidden end 推送 → rail cleanup →
// 再次 staging 成功（Python resolve_evolution_event_timeout_sec + watcher
// timeout 分支：pushEvolutionStatus(end, hidden) + cleanupEvolutionRail）。
func diagKeysE8(m map[string]any) []string {
	out := []string{}
	for k := range m {
		out = append(out, k)
	}
	return out
}

// TestE8Watcher超时HiddenEnd与重入 仅 progress 事件后静默 → idle 超时 →
// hidden end 推送 → 重启 watcher 重入成功。
// ⤴️ 语义澄清（2026-12-26 会话7，Python team_helpers.py:6355-6363 对照）：
// 审批推送后 activeCycleRequestID 即清空（Go :1190 与 Python :6363 一致），
// 审批挂起"不超时"是上游设计——hidden timeout 只对"有活跃 progress cycle
// 但事件静默"生效。本用例以 EmitHostEvent 直发 progress 事件构造该场景。
func (s *EvolutionE2ESuite) TestE8Watcher超时HiddenEnd与重入() {
	// 根因已定位并修复（2026-12-26 会话7）：①用例前提错误——审批推送后
	// activeCycleRequestID 即清空（Go :1190 与 Python team_helpers.py:6363 一致），
	// 审批挂起"不超时"是上游设计语义；②原断言 stage/status 字段搞反
	// （hidden end 帧形状 = status:"end" + stage:"hidden"）。已改为仅 progress
	// 事件静默 → idle 超时 → hidden end 的真实可触发场景。
	d := s.newAdapter()
	s.adapter = d
	s.makeTeamSkill("evo_timeout_skill")

	teamModel := s.newMockModel()
	teamRail := coreevolution.NewTeamSkillEvolutionRail(
		[]string{s.skillsDir}, teamModel, "test-model", "cn",
		coreevolution.WithTeamSkillAutoSave(false),
		coreevolution.WithTeamSkillSignalTrigger(true),
		coreevolution.WithTeamSkillReviewTrigger(true),
		coreevolution.WithTeamSkillEvolutionTimeout(2.0), // idle 超时 2s+grace 5s=7s
	)

	// ① 直发一条可见 progress 事件（event_kind=progress + generating 可见态）
	progressEvent := stream.OutputSchema{
		Type: "chat.evolution_progress",
		Payload: map[string]any{
			"event_type": "chat.evolution_status",
			"stage":      "generating",
			"message":    "正在生成演化记录",
			"request_id": "team_evolve_e8",
			"_evolution_meta": map[string]any{
				"event_kind": "progress",
			},
		},
	}
	teamRail.EmitHostEvent(&progressEvent)

	// ② 启动 watcher：drain → cycle start（从 visible progress 取 id）→ progress 帧
	watcherCtx, cancel := contextWithCancel(s.ctx)
	defer cancel()
	d.StartTeamEvolutionWatcherForTest(watcherCtx, "ch-e2e", "web_sess-e2e", teamRail)

	// ③ idle 超时（2s+grace5s+轮询1s）→ hidden end 帧（含 timed out）
	deadline := time.Now().Add(20 * time.Second)
	sawHiddenEnd := false
	for time.Now().Before(deadline) {
		for _, payload := range s.pushPayloads("chat.evolution_status") {
			// 帧形状：status="end" + stage="hidden"（TeamEvolutionEndUpdate/hidden 语义）
			status, _ := payload["status"].(string)
			stage, _ := payload["stage"].(string)
			msg, _ := payload["message"].(string)
			if status == "end" && stage == "hidden" && strings.Contains(msg, "timed out") {
				sawHiddenEnd = true
			}
		}
		if sawHiddenEnd {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	s.Assert().True(sawHiddenEnd, "watcher idle 超时应推送 hidden end 帧（含 timed out），帧数=%d", len(s.pushFrames))

	// ④ 重入：重启 watcher + 新 progress 事件 → 新 cycle start 帧（cleanup 后可重入）
	d.StartTeamEvolutionWatcherForTest(watcherCtx, "ch-e2e", "web_sess-e2e", teamRail)
	reEntry := stream.OutputSchema{
		Type: "chat.evolution_progress",
		Payload: map[string]any{
			"event_type": "chat.evolution_status",
			"stage":      "generating",
			"message":    "重入后的第二次分析",
			"request_id": "team_evolve_e8_reentry",
			"_evolution_meta": map[string]any{
				"event_kind": "progress",
			},
		},
	}
	teamRail.EmitHostEvent(&reEntry)

	reDeadline := time.Now().Add(10 * time.Second)
	sawReentry := false
	for time.Now().Before(reDeadline) {
		for _, payload := range s.pushPayloads("chat.evolution_status") {
			if rid, _ := payload["request_id"].(string); rid == "team_evolve_e8_reentry" {
				sawReentry = true
			}
		}
		if sawReentry {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	s.Assert().True(sawReentry, "cleanup 后 watcher 重入应处理新事件")
}

// ──────────────────────────── E12：团队轨迹聚合路径完整性 ────────────────────────────

// e12Marker 团队轨迹完整性特征标记（成员发言 → 轨迹摘要 → 生成输入 → 提案 → 落盘）。
const e12Marker = "E2E-MARK-TRAJECTORY-SAVEGUARD"

// e12GenerateResp 聚合路径台本（records 数组，内容回显标记）。
const e12GenerateResp = `[
	{"action": "append", "section": "Troubleshooting", "target": "body", "content": "` + e12Marker + ` 部署失败聚合排查", "summary": "聚合路径排障经验"}
]`

// TestE12团队轨迹完整性标记回读 六环节标记回读：成员发言（含标记）→ 轨迹 span →
// 轨迹摘要进入生成输入（mock 捕获的 prompt 含标记）→ 提案（records 含标记）→
// 审批 → 团队库落盘（含标记）。
// Python 锚点：TeamSkillEvolutionRail 聚合路径（span 模型）+ BuildTeamTrajectorySummary。
func (s *EvolutionE2ESuite) TestE12团队轨迹完整性标记回读() {
	// ⤴️ 断点登记（2026-12-26）：独跑 PASS（轨迹标记经聚合路径六环节全程回读），
	// 套件序必红（聚合生成 0 次 LLM 调用）——与 E8 同源的全局污染待专项定位。
	// 根因已定位并修复（2026-12-26 会话6）：非全局污染——①GetInvokeCall 为
	// 0-indexed（原断言从 1 起漏首次调用）；②MessagesParam 私有字段需经
	// Messages()+GetContent().Text() 提取（%v/json.Marshal 均不可见）。
	d := s.newAdapter()
	s.adapter = d
	s.makeTeamSkill("evo_traj_skill")

	teamModel := s.newMockModel()
	teamRail := coreevolution.NewTeamSkillEvolutionRail(
		[]string{s.skillsDir}, teamModel, "test-model", "cn",
		coreevolution.WithTeamSkillAutoSave(false),
		coreevolution.WithTeamSkillSignalTrigger(true),
		coreevolution.WithTeamSkillReviewTrigger(true),
	)
	// 轨迹：成员发言含标记（span 模型，IterSpans 非空 → 聚合路径）
	traj, terr := trajectory.NewTestTrajectory("sess-e12", nil, []trajectory.TestStep{{
		Kind: "llm",
		Messages: []map[string]any{
			{"role": "user", "content": "部署失败了，" + e12Marker + "：先检查端口占用再重启服务"},
			{"role": "assistant", "content": "已按标记流程排查端口占用"},
		},
	}})
	s.Require().NoError(terr)
	s.Require().NotEmpty(trajectory.IterSpans(traj), "轨迹应有工作 span")

	s.mockScript(e12GenerateResp)
	// [DIAG-E12]
	stagedE12, err := teamRail.HandleEvolutionFromSignalsWithResult(
		s.ctx, "evo_traj_skill", traj,
		[]*signal.EvolutionSignal{signal.MakeEvolutionSignal(
			schema.UserIntentSignal, "Instructions", "部署排障（"+e12Marker+"）",
			signal.WithSkillName("evo_traj_skill"), signal.WithSource("explicit_request"))},
		false, "部署排障",
		[]map[string]any{{"role": "user", "content": "部署排障"}}, true)
	s.Require().NoError(err)
	if stagedE12 == nil {
		s.T().Logf("[DIAG-E12] staged=nil; drain=%d", len(teamRail.DrainPendingApprovalEvents(false, nil)))
	} else {
		s.T().Logf("[DIAG-E12] staged req=%s payload=%d", stagedE12.RequestID, len(stagedE12.PendingChange.Payload))
	}

	// ① 轨迹 → 生成输入：mock 捕获的 prompt 应含标记（轨迹摘要未丢失）。
	// 调用次数随路径浮动（生成+评分等），扫描 0..5 个调用取并集断言。
	// 注意 GetInvokeCall 为 0-indexed（mock_client.go:385）。
	promptAll := ""
	sawAnyCall := false
	for i := 0; i <= 5; i++ {
		if call := s.mockLLM.GetInvokeCall(i); call != nil {
			sawAnyCall = true
			// MessagesParam 私有字段——经导出访问器提取消息文本
			for _, bm := range call.Messages.Messages() {
				promptAll += bm.GetContent().Text() + "\n"
			}
		}
	}
	s.Require().True(sawAnyCall, "聚合路径应发起 LLM 调用")
	s.T().Logf("[DIAG-E12] prompt head=%q", func() string {
		if len(promptAll) > 400 {
			return promptAll[:400]
		}
		return promptAll
	}())
	s.Assert().Contains(promptAll, e12Marker, "生成输入应含轨迹标记（环节①②不丢失）")

	// ② 提案 → 审批 → 落盘：drain 审批事件取 request_id → ApproveRecord → 落盘回读
	events := teamRail.DrainPendingApprovalEvents(false, nil)
	approvalRequestID := ""
	for _, evt := range events {
		m := evolutionlogic.EventPayloadDict(evt.ToMap())
		if evolutionlogic.EventType(m) == "chat.ask_user_question" {
			if rid, _ := m["request_id"].(string); rid != "" {
				approvalRequestID = rid
			}
		}
	}
	s.Require().NotEmpty(approvalRequestID, "聚合路径应产出审批事件")
	s.Require().NoError(teamRail.ApproveRecord(s.ctx, approvalRequestID))

	evoLog, err := teamRail.EvolutionStore().LoadFullEvolutionLog(s.ctx, "evo_traj_skill")
	s.Require().NoError(err)
	s.Require().NotEmpty(evoLog.Entries)
	contents := ""
	for _, entry := range evoLog.Entries {
		contents += entry.Change.Content
	}
	s.Assert().Contains(contents, e12Marker, "落盘产物应含轨迹标记（六环节全程无丢失）")
}
