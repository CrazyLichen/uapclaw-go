//go:build integration

// 25.3 零头收编：review→演化→折叠端到端集成（spec
// docs/superpowers/specs/2026-12-05-25.3-residue-design.md §3）。
//
// E-1 主链：真结算落 DB（fail→返工邮件→重做→pass→completed）→ 作者下次输入经
// TeamPolicyRail 自然 claim 折叠 verified 通知（非直调 tracker）→ 板清 →
// scheduler dispatchTeamReviewFeedback（任务级反馈回调 + 终态聚合）→ 归因器
// （skill_issue + 轨迹读取证据）→ TeamSkillEvolutionRail.EvolveFromExternalSignals
// （autoSave）→ 演化产物落盘（skill workspace evolutions.json）。
//
// 真实跨子系统：agent_teams/scheduling + tools（TM/Backend）+ database +
// rails（TeamPolicyRail 折叠）+ agentcore rails evolution + evolving signal/experience
// + evolving checkpointing 存储。mock 边界只许 LLM。
package agentteams_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/agent/coordination/scheduling"
	ateamprompts "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/prompts"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/rails"
	atschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/tools"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/tools/database"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/tracker"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/rails/evolution"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/interfaces"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/interfaces"

	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ReviewEvolutionE2ESuite 评审→演化→折叠端到端套件（独立 DB）。
type ReviewEvolutionE2ESuite struct {
	isuite.AgentSuite
	suite.Suite
	db      *database.InMemoryTeamDatabase
	tm      *tools.TeamTaskManager
	backend *schedFakeBackend
	host    *schedFakeHost
	sender  *schedFakeSender
	sched   *scheduling.TeamScheduler

	// evolution 面（E-1 共用；每用例在 beforeTest 重建）
	skillsDir    string
	rail         *evolution.TeamSkillEvolutionRail
	feedbackCalls int
	// aggregated 终态聚合回调是否被调度器执行（E-3 断言面）。
	aggregated bool
}

// strPtr 字符串指针助手（本套件私有）。
func (s *ReviewEvolutionE2ESuite) strPtr(v string) *string { return &v }

// TestReviewEvolutionE2ESuite 入口。
func TestReviewEvolutionE2ESuite(t *testing.T) {
	suite.Run(t, new(ReviewEvolutionE2ESuite))
}

// e2eMockClient 可编程响应的 mock 模型客户端（对齐 reviewFeedbackMockClient 形态；
// 两套件不共享测试件）。
type e2eMockClient struct {
	responses []string
	idx       int
}

func (m *e2eMockClient) Invoke(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
	if m.idx < len(m.responses) {
		msg := m.responses[m.idx]
		m.idx++
		return llmschema.NewAssistantMessage(msg), nil
	}
	return llmschema.NewAssistantMessage("{}"), nil
}
func (m *e2eMockClient) Stream(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.StreamOption) (<-chan *llmschema.AssistantMessageChunk, error) {
	return nil, nil
}
func (m *e2eMockClient) GenerateImage(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateImageOption) (*llmschema.ImageGenerationResponse, error) {
	return nil, nil
}
func (m *e2eMockClient) GenerateSpeech(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateSpeechOption) (*llmschema.AudioGenerationResponse, error) {
	return nil, nil
}
func (m *e2eMockClient) GenerateVideo(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateVideoOption) (*llmschema.VideoGenerationResponse, error) {
	return nil, nil
}
func (m *e2eMockClient) TranscribeAudio(_ context.Context, _ string, _ ...llmschema.TranscribeAudioOption) (*llmschema.TranscriptionResponse, error) {
	return nil, nil
}
func (m *e2eMockClient) Release(_ context.Context, _ ...model_clients.ReleaseOption) (bool, error) {
	return false, nil
}
func (m *e2eMockClient) SupportsKVCacheRelease() bool  { return false }
func (m *e2eMockClient) SupportsKVCacheAffinity() bool { return false }
func (m *e2eMockClient) BuildKVCacheAffinityInvokeKwargs(_ any, _, _ string, _ int) map[string]any {
	return map[string]any{}
}

// SetupSuite 建团队行 + 全套管理器（scheduled dispatch 团队）。
func (s *ReviewEvolutionE2ESuite) SetupSuite() {
	ctx := context.Background()
	db := database.NewInMemoryTeamDatabase()
	s.db = db
	s.Require().True(db.Team().CreateTeam(ctx, "alpha", "Alpha", "leader1", "", "", "scheduled", false))
	for _, member := range []string{"leader1", "agent1", "reviewer1"} {
		role := "teammate"
		if member == "leader1" {
			role = "leader"
		}
		s.Require().True(db.Member().CreateMember(ctx, member, "alpha", member, "{}", "ready", role, "", "", "build_mode", "", ""))
	}
	s.Require().NoError(db.Initialize(ctx))
	s.tm = tools.NewTeamTaskManager(db, "alpha", "leader1", nil, nil, s.T().TempDir(), "", "leader1")
	s.backend = &schedFakeBackend{shutdown: map[string]bool{}, verificationEnabled: true}
	s.sender = &schedFakeSender{rows: map[string][]*database.TeamMessageBase{}}
	s.host = &schedFakeHost{}
	s.sched = scheduling.NewTeamScheduler(s.host, s.tm, s.backend, s.sender, 2, 3600, "alpha", "cn")
}

// beforeE2ETest 每用例复位板面 + 重建演化面（rail/mock 模型/调度器回调接线）。
func (s *ReviewEvolutionE2ESuite) beforeE2ETest(sessionID string, responses ...string) {
	s.backend.verificationEnabled = true
	ctxReset := context.Background()
	for _, status := range []string{"pending", "blocked", "planning", "in_progress", "in_review"} {
		rows, err := s.tm.ListTasks(ctxReset, status)
		if err != nil {
			continue
		}
		for _, row := range rows {
			_, _ = s.db.Task().UpdateTaskStatus(ctxReset, row.TaskID, "in_progress")
			_, _ = s.db.Task().UpdateTaskStatus(ctxReset, row.TaskID, "completed")
		}
	}
	s.sender = &schedFakeSender{rows: map[string][]*database.TeamMessageBase{}}
	s.host = &schedFakeHost{}
	s.sched = scheduling.NewTeamScheduler(s.host, s.tm, s.backend, s.sender, 2, 3600, "alpha", "cn")

	// 演化面：skillsDir 预置 data-processor 技能 + 捕获一次读取 SKILL.md 的
	// 工具 span（轨迹读取证据，复用 chain 套件形态）。
	s.skillsDir = s.T().TempDir()
	skillDir := filepath.Join(s.skillsDir, "data-processor")
	s.Require().NoError(os.MkdirAll(skillDir, 0o755))
	s.Require().NoError(os.WriteFile(filepath.Join(skillDir, "SKILL.md"),
		[]byte("# data-processor\n\n数据处理技能。"), 0o644))

	mockClient := &e2eMockClient{responses: responses}
	providerName := fmt.Sprintf("mock_e2e_%s_%d", sessionID, time.Now().UnixNano())
	model_clients.GetClientRegistry().Register(providerName, "llm", func(_ *llmschema.ModelRequestConfig, _ *llmschema.ModelClientConfig) (model_clients.BaseModelClient, error) {
		return mockClient, nil
	})
	model, err := llm.NewModel(&llmschema.ModelClientConfig{
		ClientProvider: providerName, ClientID: providerName + "_id",
	}, &llmschema.ModelRequestConfig{ModelName: "test-model"})
	s.Require().NoError(err)

	s.rail = evolution.NewTeamSkillEvolutionRail([]string{s.skillsDir}, model, "test-model", "cn",
		evolution.WithTeamSkillTeamID("alpha"),
		evolution.WithTeamSkillAutoSave(true))
	cbc := &agentinterfaces.AgentCallbackContext{}
	cbc.SetInputs(&agentinterfaces.InvokeInputs{ConversationID: sessionID})
	s.Require().NoError(s.rail.BeforeInvoke(context.Background(), cbc))
	tcbc := &agentinterfaces.AgentCallbackContext{}
	tcbc.SetInputs(&agentinterfaces.ToolCallInputs{
		ToolName: "read_file",
		ToolArgs: map[string]any{"path": filepath.Join(skillDir, "SKILL.md")},
	})
	s.Require().NoError(s.rail.AfterToolCall(context.Background(), tcbc))

	// 调度器接线：任务级反馈回调（rail.HandleReviewFeedback）+ 终态聚合
	// （rail.FinalizeReviewFeedback）——Python scheduler._dispatch_review_feedback
	// 与 _dispatch_team_review_feedback 的装配面。
	coordinator := scheduling.NewReviewFeedbackEvolutionCoordinator(sessionID, "alpha",
		func() scheduling.TeamEvolutionRailLike { return s.rail }, true, 0.7)
	s.rail.SetupReviewFeedbackEvolution(sessionID, "alpha", 0.7, coordinator)
	s.feedbackCalls = 0
	s.sched.SetReviewFeedbackHandler(sessionID, func(ctx context.Context, payload map[string]any) error {
		s.feedbackCalls++
		return s.rail.HandleReviewFeedback(ctx, payload)
	})
	s.aggregated = false
	s.sched.SetReviewFeedbackFinalizer(func(ctx context.Context) (bool, error) {
		s.aggregated = true
		return s.rail.FinalizeReviewFeedback(ctx, nil)
	})
}

// createE2ETask 建一条带验收人（verifier reviewer1）的任务并交给作者完成。
func (s *ReviewEvolutionE2ESuite) createE2ETask(taskID string) {
	ctx := context.Background()
	s.Require().True(s.db.Task().CreateTask(ctx, &database.TeamTaskBase{
		TaskID: taskID, TeamName: "alpha", Title: "端到端任务 " + taskID, Content: "导出功能实现",
		Status: "in_progress", Assignee: s.strPtr("agent1"),
		Reviewer: s.strPtr(`[{"type":"verifier","reviewer_id":"reviewer1"}]`),
	}))
	authorTM := tools.NewTeamTaskManager(s.db, "alpha", "agent1", nil, nil, s.T().TempDir(), "", "leader1")
	result, err := authorTM.Complete(ctx, taskID)
	s.Require().NoError(err)
	s.Require().True(result.OK, "complete must pass the review gate")
	row, getErr := s.db.Task().GetTask(ctx, taskID)
	s.Require().NoError(getErr)
	s.Require().NotNil(row)
	s.Require().Equal("in_review", row.Status, "带验收人的任务完成时进 IN_REVIEW（评审门）")
}

// injectVote 注入一个以 VerifyTask 工具路径投票的 runner（模拟临时 reviewer
// harness 的 verify_task 调用——MockLLM 编排在工具层等效）。
func (s *ReviewEvolutionE2ESuite) injectVote(decision, feedback string) {
	s.sched.SetReviewerRunner(func(_ context.Context, reviewer string, task *database.TeamTaskBase) bool {
		reviewerTM := tools.NewTeamTaskManager(s.db, "alpha", reviewer, nil, nil, s.T().TempDir(), "", "leader1")
		result, err := reviewerTM.VerifyTask(context.Background(), task.TaskID, decision, feedback)
		if err != nil || !result.OK {
			return false
		}
		return true
	})
}

// scanTwice 两轮扫描拾取异步投票并结算（真实时序：下一唤醒）。
func (s *ReviewEvolutionE2ESuite) scanTwice() {
	s.sched.Activate(context.Background())
	time.Sleep(400 * time.Millisecond)
	s.sched.Activate(context.Background())
}

// scanUntil 有界轮询扫描直到条件成立（投票异步落账的时序安全形态；
// 固定 sleep 在负载下会漏拾取 round2 票）。
func (s *ReviewEvolutionE2ESuite) scanUntil(cond func() bool) {
	for i := 0; i < 20; i++ {
		s.sched.Activate(context.Background())
		if cond() {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// policyRailFor 构造绑定真实 tracker 的作者侧 TeamPolicyRail（折叠路径载体）。
func (s *ReviewEvolutionE2ESuite) policyRailFor() *rails.TeamPolicyRail {
	tr := tracker.BuildTaskEventTracker("scheduled", s.newE2EBackend("agent1"), "agent1")
	s.Require().NotNil(tr)
	rail, err := rails.NewTeamPolicyRail(
		rails.WithPolicyRole(atschema.TeamRoleTeammate),
		rails.WithPolicyMemberName("agent1"),
		rails.WithPolicyLanguage("cn"),
		rails.WithPolicyTaskEventTracker(tr),
	)
	s.Require().NoError(err)
	return rail
}

// newE2EBackend 按成员建 TeamBackend（对齐 task_notices 套件形态）。
func (s *ReviewEvolutionE2ESuite) newE2EBackend(member string) *tools.TeamBackend {
	return tools.NewTeamBackend("alpha", member, member == "leader1", s.db, nil,
		tools.WithLeaderMemberName("leader1"),
		tools.WithDispatchMode("scheduled"),
	)
}

// foldViaRail 以真实输入路径触发折叠（TeamPolicyRail.OnUserMessage →
// foldTaskOutcomes → tracker.Claim/Persist），返回折叠后的输入 parts。
// 这是作者侧「下次输入自然获知」的生产路径，非直调 tracker。
func (s *ReviewEvolutionE2ESuite) foldViaRail(rail *rails.TeamPolicyRail, sess sessioninterfaces.SessionFacade, parts []string) []string {
	cbc := agentinterfaces.NewAgentCallbackContext(nil, &agentinterfaces.UserMessageInputs{Parts: parts}, sess)
	s.Require().NoError(rail.OnUserMessage(context.Background(), cbc))
	return cbc.Inputs().(*agentinterfaces.UserMessageInputs).Parts
}

// TestE2E_E1_主链 fail→返工邮件→重做→pass→自然折叠→板清→演化落盘。
func (s *ReviewEvolutionE2ESuite) TestE2E_E1_主链() {
	const sessionID = "sess-e1"
	const taskID = "e1"
	s.beforeE2ETest(sessionID,
		// 响应 1：评审反馈归因（HandleReviewFeedback 的归因 LLM）。
		`{"classification":"skill_issue","skill_name":"data-processor","target":"body","reason":"Skill 缺并发指导","reusable_guidance":"补充并发安全说明","is_reusable":true,"confidence":0.9}`,
		// 响应 2：经验记录草案（EvolveFromExternalSignals → optimizer 的记录生成 LLM）。
		`{"action":"append","section":"Troubleshooting","target":"body","content":"补充并发安全说明：评审反馈指出并发处理不安全，须加锁保护共享状态。"}`)

	// ① 作者完成 → 评审门 → round1 fail → 返工邮件（信箱直投，WAKES 路径）。
	s.createE2ETask(taskID)
	s.injectVote("fail", "请补充并发测试")
	s.scanTwice()
	row, _ := s.db.Task().GetTask(context.Background(), taskID)
	s.Require().NotNil(row)
	s.Require().Equal("in_progress", row.Status, "fail 结算退回返工")
	mail := s.sender.rows["agent1"]
	s.Require().NotEmpty(mail, "author must receive rework mail")
	s.Require().Equal("scheduler_rework", e2eTemplateOf(mail, "scheduler_rework"), "F_63 rework mail")

	// ② 作者按返工重做 → 再过评审门（round2）→ pass → 结算 completed + leader digest。
	authorTM := tools.NewTeamTaskManager(s.db, "alpha", "agent1", nil, nil, s.T().TempDir(), "", "leader1")
	result, err := authorTM.Complete(context.Background(), taskID)
	s.Require().NoError(err)
	s.Require().True(result.OK, "rework completion re-enters review gate (round 2)")
	mailBeforePass := len(s.sender.rows["agent1"])
	s.injectVote("pass", "")
	s.scanUntil(func() bool {
		row, _ := s.db.Task().GetTask(context.Background(), taskID)
		return row != nil && row.Status == "completed"
	})
	row, _ = s.db.Task().GetTask(context.Background(), taskID)
	s.Require().NotNil(row)
	s.Require().Equal("completed", row.Status, "pass 结算完成任务")
	s.Require().Len(s.sender.rows["agent1"], mailBeforePass, "F_119: pass 结算不新增作者信箱投递（round1 的 rework 邮件保留）")
	s.Require().NotEmpty(s.host.delivered, "leader must receive the terminal digest")

	// ③ 作者下次输入：TeamPolicyRail 自然 claim 折叠 verified 通知（非直调 tracker）。
	policyRail := s.policyRailFor()
	bobSession := s.NewTestSession(sessionID + "-author")
	folded := s.foldViaRail(policyRail, bobSession, []string{"继续收尾"})
	s.Require().NotEmpty(folded)
	found := false
	for _, part := range folded {
		if strings.Contains(part, taskID) {
			found = true
		}
	}
	s.Require().True(found, "折叠文本须点名任务 "+taskID)

	// 基线推进：二次输入不再折叠。
	folded2 := s.foldViaRail(policyRail, bobSession, []string{"再确认"})
	for _, part := range folded2 {
		s.Require().False(strings.Contains(part, "<team-event"), "已告知条目不得二次折叠")
	}

	// ④ 板清（任务 completed）→ 下一轮扫描触发终态聚合：任务级反馈回调
	// （fail 轮的 payload）→ rail.HandleReviewFeedback → FinalizeReviewFeedback
	// → 归因（mock LLM 返回 skill_issue + 读取证据）→ EvolveFromExternalSignals
	// （autoSave）→ 产物落盘。
	s.sched.Activate(context.Background())
	evolutionsPath := filepath.Join(s.skillsDir, "data-processor", "evolutions.json")
	s.Require().FileExists(evolutionsPath, "autoSave 演化产物必须落盘（evolutions.json）")
	data, readErr := os.ReadFile(evolutionsPath)
	s.Require().NoError(readErr)
	s.Require().Contains(string(data), "data-processor", "演进日志须指向目标技能")
}

// TestE2E_E2_pass结算_折叠基线推进 F_119 pass 侧：不唤醒、自然折叠、基线推进。
func (s *ReviewEvolutionE2ESuite) TestE2E_E2_pass结算_折叠基线推进() {
	const sessionID = "sess-e2"
	const taskID = "e2"
	s.beforeE2ETest(sessionID,
		`{"classification":"unattributed","reason":"无法归因"}`)

	s.createE2ETask(taskID)
	s.injectVote("pass", "")
	s.scanTwice()
	row, _ := s.db.Task().GetTask(context.Background(), taskID)
	s.Require().NotNil(row)
	s.Require().Equal("completed", row.Status)
	s.Require().Empty(s.sender.rows["agent1"], "pass 结算不唤醒作者（无信箱投递）")

	policyRail := s.policyRailFor()
	bobSession := s.NewTestSession(sessionID + "-author")
	folded := s.foldViaRail(policyRail, bobSession, []string{"下一步"})
	found := false
	for _, part := range folded {
		if strings.Contains(part, taskID) {
			found = true
		}
	}
	s.Require().True(found, "首次输入须折叠 verified 通知")
	// 二次输入：persist 已推进基线 → 无通知。
	folded2 := s.foldViaRail(policyRail, bobSession, []string{"收尾确认"})
	for _, part := range folded2 {
		s.Require().False(strings.Contains(part, "<team-event"), "基线推进后不得重复折叠")
	}
}

// TestE2E_E3_升级链不阻断演化 轮上限耗尽升级 leader 后板清演化仍触发。
func (s *ReviewEvolutionE2ESuite) TestE2E_E3_升级链不阻断演化() {
	const sessionID = "sess-e3"
	const taskID = "e3"
	s.beforeE2ETest(sessionID,
		// 响应 1：round1 fail 归因 → skill_issue 观察入账。
		`{"classification":"skill_issue","skill_name":"data-processor","target":"body","reason":"Skill 缺错误处理","reusable_guidance":"补充错误处理说明","is_reusable":true,"confidence":0.9}`,
		// 响应 2：round2 fail（超限升级）归因 → 不可归因（跳过）。
		`{"classification":"unattributed","reason":"无法归因"}`,
		// 响应 3：经验记录草案（终态聚合的 optimizer 记录生成）。
		`{"action":"append","section":"Troubleshooting","target":"body","content":"补充错误处理说明：评审多轮指出错误处理缺失，须显式检查并降级。"}`)

	// round1 fail（max_review_rounds=2）→ 返工 → round2 再 fail → 超限升级。
	s.createE2ETask(taskID)
	s.injectVote("fail", "第一轮缺陷")
	s.scanTwice()
	authorTM := tools.NewTeamTaskManager(s.db, "alpha", "agent1", nil, nil, s.T().TempDir(), "", "leader1")
	result, err := authorTM.Complete(context.Background(), taskID)
	s.Require().NoError(err)
	s.Require().True(result.OK, "rework completion re-enters review gate (round 2)")
	s.injectVote("fail", "仍有缺陷")
	s.scanUntil(func() bool { return len(s.host.delivered) > 0 })

	row, _ := s.db.Task().GetTask(context.Background(), taskID)
	s.Require().NotNil(row)
	s.Require().Equal("in_review", row.Status, "round ceiling must not settle")
	s.Require().NotEmpty(s.host.delivered, "leader must receive escalation")
	s.Require().NotEmpty(s.sender.rows["agent1"], "assignee must receive rework summary")
	summaryCount := 0
	for _, m := range s.sender.rows["agent1"] {
		if e2eTemplateOf([]*database.TeamMessageBase{m}, "scheduler_rework_summary") != "" {
			summaryCount++
		}
	}
	s.Require().Equal(1, summaryCount, "rework_summary 一次")

	// 升级后任务终态化（leader 处置：直接置 completed）→ 板清 → 演化仍触发。
	// leader 处置：升级后任务由 leader 直接置终态（completed），完成事件走
	// transport 总线 digest（scheduler.OnEvent——生产链路同形），板清后
	// dispatchTeamReviewFeedback 触发（Python digestTaskDone L1177 同点）。
	s.Require().True(s.db.Task().UpdateTaskStatus(context.Background(), taskID, "completed"))
	s.sched.OnEvent(context.Background(), CoordinationEventOf("task_completed", map[string]any{"task_id": taskID, "task_count": float64(1)}))
	s.Require().True(s.aggregated, "升级路径不阻断终态反馈聚合（finalizer 必须被调度器执行）")
}

// ──────────────────────────── 助手 ────────────────────────────

// e2eTemplateOf 返回信箱行中首个匹配模板的 meta.template（缺失返回空串）。
// 套件私有助手（与 task_notices 套件的方法形态互不共享）。
func e2eTemplateOf(rows []*database.TeamMessageBase, template string) string {
	for _, row := range rows {
		if row.Meta == nil {
			continue
		}
		meta := ateamprompts.ParseMeta(row.Meta)
		if tpl, _ := meta["template"].(string); tpl == template {
			return tpl
		}
	}
	return ""
}
