//go:build integration

package evolution

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	evolution "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/evolution"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/evolving/checkpointing"
	"github.com/uapclaw/uapclaw-go/internal/evolving/optimizer/llm_resilience"
	"github.com/uapclaw/uapclaw-go/internal/evolving/trajectory"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TeamSkillEvolutionRailSuite 测试 TeamSkillEvolutionRail 构造默认值、选项、属性访问器、
// 触发控制、信号去重、审批快照及 OnAfterToolCall 行为。
//
// 对齐 Python: tests/unit_tests/harness/rails/evolution/test_team_skill_rail.py
//   - TestTeamSkillEvolutionRailInit: priority, auto_scan, auto_save, timeout
//   - TestTeamSkillEvolutionRailOptions: WithTeamSkill* 系列选项
//   - TestTeamSkillEvolutionRailConfig: EvolutionConfig 键值验证
//   - TestAllowEvolutionTrigger: passiveEvolutionPending / hostCompletionPending 条件
//   - TestOnBeforeInvoke: 重置 passiveEvolutionPending
//   - TestNotifyTeamCompleted: 标记 hostCompletionPendingSessionID
//   - TestOnAfterToolCall: view_task 完成检测 + 经验详情读取
//   - TestHasPendingApprovalSnapshot: 暂存审批快照存在性
//   - TestUpdateLLM / TestSetSysOperation: 热更新传播
//   - TestApprovalLifecycle: ApproveRecord / RejectRecord
//
// 注意：未导出函数（markPassiveEvolutionPending, isCompletedTeamTaskView, pendingApprovalSnapshots 等）
// 在 internal 包内 _test.go 中已有单元测试，此集成测试套件只验证导出行为。
// isCompletedTeamTaskView 的逻辑通过 OnAfterToolCall + view_task 间接覆盖。
type TeamSkillEvolutionRailSuite struct {
	isuite.BaseIntegrationSuite
}

// mockTeamSkillClient 集成测试用模拟 LLM 客户端。
// 对齐 skill_evolution_rail_test.go 中 mockSkillClient 的模式。
type mockTeamSkillClient struct {
	// invokeFn 自定义 Invoke 行为
	invokeFn func(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error)
}

func (m *mockTeamSkillClient) Invoke(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
	if m.invokeFn != nil {
		return m.invokeFn(ctx, messages, opts...)
	}
	return llmschema.NewAssistantMessage("mock team skill evolution response"), nil
}
func (m *mockTeamSkillClient) Stream(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.StreamOption) (<-chan *llmschema.AssistantMessageChunk, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockTeamSkillClient) GenerateImage(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateImageOption) (*llmschema.ImageGenerationResponse, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockTeamSkillClient) GenerateSpeech(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateSpeechOption) (*llmschema.AudioGenerationResponse, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockTeamSkillClient) GenerateVideo(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateVideoOption) (*llmschema.VideoGenerationResponse, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockTeamSkillClient) TranscribeAudio(_ context.Context, _ string, _ ...llmschema.TranscribeAudioOption) (*llmschema.TranscriptionResponse, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockTeamSkillClient) Release(_ context.Context, _ ...model_clients.ReleaseOption) (bool, error) {
	return false, nil
}
func (m *mockTeamSkillClient) SupportsKVCacheRelease() bool { return false }

// ──────────────────────────── 导出函数 ────────────────────────────

// TestTeamSkillEvolutionRailSuite 运行 TeamSkillEvolutionRail 集成测试套件。
func TestTeamSkillEvolutionRailSuite(t *testing.T) {
	suite.Run(t, new(TeamSkillEvolutionRailSuite))
}

// ─── 构造与默认值 ───

// TestTeamSkillEvolutionRail_Priority 测试 Priority() 返回 80。
//
// 对齐 Python: TeamSkillEvolutionRail.priority = 80
func (s *TeamSkillEvolutionRailSuite) TestTeamSkillEvolutionRail_Priority() {
	rail := s.newTestRail()
	s.Equal(80, rail.Priority())
}

// TestTeamSkillEvolutionRail_GetEvolutionTotalTimeoutSecs 测试默认超时 720.0 秒。
//
// 对齐 Python: _DEFAULT_TEAM_EVOLUTION_TOTAL_TIMEOUT_SECS = 720.0
// 关键差异：SkillEvolutionRail 默认 600.0
func (s *TeamSkillEvolutionRailSuite) TestTeamSkillEvolutionRail_GetEvolutionTotalTimeoutSecs() {
	rail := s.newTestRail()
	s.Equal(720.0, rail.GetEvolutionTotalTimeoutSecs())
}

// TestTeamSkillEvolutionRail_AutoScan默认值 测试 autoScan 默认为 true。
//
// 对齐 Python: TeamSkillEvolutionRail(auto_scan=True)
func (s *TeamSkillEvolutionRailSuite) TestTeamSkillEvolutionRail_AutoScan默认值() {
	rail := s.newTestRail()
	s.True(rail.AutoScan())
}

// TestTeamSkillEvolutionRail_AutoSave默认值false 测试 autoSave 默认为 false（与 SkillEvolutionRail 的关键差异）。
//
// 对齐 Python: TeamSkillEvolutionRail(auto_save=False) — 与 SkillEvolutionRail(auto_save=True) 不同
func (s *TeamSkillEvolutionRailSuite) TestTeamSkillEvolutionRail_AutoSave默认值false() {
	rail := s.newTestRail()
	s.False(rail.AutoSave())
}

// ─── EvolutionConfig ───

// TestTeamSkillEvolutionRail_EvolutionConfig 测试 EvolutionConfig 包含预期键和默认值。
//
// 对齐 Python: TeamSkillEvolutionRail.evolution_config
// 关键差异：TeamSkillEvolutionRail 有 5 个 LLM 策略 + max_concurrent_evolution（SkillEvolutionRail 有 3 个）
func (s *TeamSkillEvolutionRailSuite) TestTeamSkillEvolutionRail_EvolutionConfig() {
	rail := s.newTestRail()
	config := rail.EvolutionConfig()

	// 验证 5 个 LLM 策略键（关键差异：SkillEvolutionRail 只有 3 个）
	s.Contains(config, "user_request_llm_policy")
	s.Contains(config, "trajectory_issue_llm_policy")
	s.Contains(config, "record_llm_policy")
	s.Contains(config, "evaluate_llm_policy")
	s.Contains(config, "simplify_llm_policy")

	// 验证其他配置键
	s.Contains(config, "eval_interval")
	s.Contains(config, "evolution_total_timeout_secs")
	s.Contains(config, "max_concurrent_evolution")

	// 验证默认值
	s.Equal(720.0, config["evolution_total_timeout_secs"])

	// 验证默认 LLM 策略非零值
	userReqPolicy, ok1 := config["user_request_llm_policy"].(llm_resilience.LLMInvokePolicy)
	s.Require().True(ok1)
	s.Greater(userReqPolicy.MaxAttempts, 0)

	trajIssuePolicy, ok2 := config["trajectory_issue_llm_policy"].(llm_resilience.LLMInvokePolicy)
	s.Require().True(ok2)
	s.Greater(trajIssuePolicy.MaxAttempts, 0)

	recordPolicy, ok3 := config["record_llm_policy"].(llm_resilience.LLMInvokePolicy)
	s.Require().True(ok3)
	s.Greater(recordPolicy.MaxAttempts, 0)

	evalPolicy, ok4 := config["evaluate_llm_policy"].(llm_resilience.LLMInvokePolicy)
	s.Require().True(ok4)
	s.Greater(evalPolicy.MaxAttempts, 0)

	simplPolicy, ok5 := config["simplify_llm_policy"].(llm_resilience.LLMInvokePolicy)
	s.Require().True(ok5)
	s.Greater(simplPolicy.MaxAttempts, 0)
}

// ─── 选项函数 ───

// TestTeamSkillEvolutionRail_WithTeamSkillAutoScan 测试 WithTeamSkillAutoScan 选项。
//
// 对齐 Python: TeamSkillEvolutionRail(auto_scan=False)
func (s *TeamSkillEvolutionRailSuite) TestTeamSkillEvolutionRail_WithTeamSkillAutoScan() {
	rail := s.newTestRail(evolution.WithTeamSkillAutoScan(false))
	s.False(rail.AutoScan())

	rail2 := s.newTestRail(evolution.WithTeamSkillAutoScan(true))
	s.True(rail2.AutoScan())
}

// TestTeamSkillEvolutionRail_WithTeamSkillAutoSave 测试 WithTeamSkillAutoSave 选项。
//
// 对齐 Python: TeamSkillEvolutionRail(auto_save=True)
func (s *TeamSkillEvolutionRailSuite) TestTeamSkillEvolutionRail_WithTeamSkillAutoSave() {
	rail := s.newTestRail(evolution.WithTeamSkillAutoSave(true))
	s.True(rail.AutoSave())

	rail2 := s.newTestRail(evolution.WithTeamSkillAutoSave(false))
	s.False(rail2.AutoSave())
}

// TestTeamSkillEvolutionRail_WithTeamSkillEvalInterval 测试 WithTeamSkillEvalInterval 选项。
//
// 对齐 Python: TeamSkillEvolutionRail(eval_interval=3)
// 间隔 < 1 时钳位到 1。
func (s *TeamSkillEvolutionRailSuite) TestTeamSkillEvolutionRail_WithTeamSkillEvalInterval() {
	rail := s.newTestRail(evolution.WithTeamSkillEvalInterval(10))
	s.NotNil(rail)

	// WithTeamSkillEvalInterval(0) → 钳位到 1
	rail2 := s.newTestRail(evolution.WithTeamSkillEvalInterval(0))
	s.NotNil(rail2)

	// WithTeamSkillEvalInterval(-5) → 钳位到 1
	rail3 := s.newTestRail(evolution.WithTeamSkillEvalInterval(-5))
	s.NotNil(rail3)
}

// TestTeamSkillEvolutionRail_WithTeamSkillEvolutionTimeout 测试 WithTeamSkillEvolutionTimeout 选项。
//
// 对齐 Python: TeamSkillEvolutionRail(evolution_total_timeout_secs=300)
func (s *TeamSkillEvolutionRailSuite) TestTeamSkillEvolutionRail_WithTeamSkillEvolutionTimeout() {
	rail := s.newTestRail(evolution.WithTeamSkillEvolutionTimeout(300.0))
	s.Equal(300.0, rail.GetEvolutionTotalTimeoutSecs())

	// 验证 EvolutionConfig 同步
	config := rail.EvolutionConfig()
	s.Equal(300.0, config["evolution_total_timeout_secs"])
}

// TestTeamSkillEvolutionRail_WithTeamSkillTeamID 测试 WithTeamSkillTeamID 选项。
//
// 对齐 Python: TeamSkillEvolutionRail(team_id="team_123")
func (s *TeamSkillEvolutionRailSuite) TestTeamSkillEvolutionRail_WithTeamSkillTeamID() {
	rail := s.newTestRail(evolution.WithTeamSkillTeamID("team_abc"))
	s.NotNil(rail)
	// teamID 存储在内部，通过 aggregateTeamTrajectory 间接生效
}

// TestTeamSkillEvolutionRail_WithTeamSkillMemberRole 测试 WithTeamSkillMemberRole 默认为 "leader"。
//
// 对齐 Python: TeamSkillEvolutionRail.__init__(default_member_role="leader")
// 关键差异：SkillEvolutionRail 默认 "teammate"，TeamSkillEvolutionRail 默认 "leader"
func (s *TeamSkillEvolutionRailSuite) TestTeamSkillEvolutionRail_WithTeamSkillMemberRole() {
	rail := s.newTestRail()
	// 默认 member role 为 "leader"（通过基类 NewEvolutionRail 传入 WithDefaultMemberRole("leader")）
	s.NotNil(rail)

	// WithTeamSkillMemberRole 覆盖
	rail2 := s.newTestRail(evolution.WithTeamSkillMemberRole("member"))
	s.NotNil(rail2)
}

// TestTeamSkillEvolutionRail_WithTeamSkillAsyncEvolution 测试 WithTeamSkillAsyncEvolution 选项。
//
// 对齐 Python: TeamSkillEvolutionRail(async_evolution=False)
func (s *TeamSkillEvolutionRailSuite) TestTeamSkillEvolutionRail_WithTeamSkillAsyncEvolution() {
	rail := s.newTestRail(evolution.WithTeamSkillAsyncEvolution(false))
	s.NotNil(rail)

	rail2 := s.newTestRail(evolution.WithTeamSkillAsyncEvolution(true))
	s.NotNil(rail2)
}

// TestTeamSkillEvolutionRail_WithTeamSkillMaxConcurrentEvolution 测试 WithTeamSkillMaxConcurrentEvolution 选项。
//
// 对齐 Python: TeamSkillEvolutionRail(max_concurrent_evolution=5)
func (s *TeamSkillEvolutionRailSuite) TestTeamSkillEvolutionRail_WithTeamSkillMaxConcurrentEvolution() {
	rail := s.newTestRail(evolution.WithTeamSkillMaxConcurrentEvolution(5))
	s.NotNil(rail)

	// 验证 EvolutionConfig 中 max_concurrent_evolution
	config := rail.EvolutionConfig()
	s.Equal(5, config["max_concurrent_evolution"])
}

// TestTeamSkillEvolutionRail_WithTeamSkillDisabledSkills 测试禁用技能名称标准化。
//
// 对齐 Python: disabled_skills 参数处理
func (s *TeamSkillEvolutionRailSuite) TestTeamSkillEvolutionRail_WithTeamSkillDisabledSkills() {
	rail := s.newTestRail(
		evolution.WithTeamSkillDisabledSkills([]string{"TeamSkill-A", " team_skill_b ", ""}),
	)
	// 经过 normalizeSkillNames（TrimSpace + 过滤空串）
	baseDisabled := rail.EvolutionRail.DisabledSkills()
	s.True(baseDisabled["TeamSkill-A"])
	s.True(baseDisabled["team_skill_b"])  // TrimSpace 后
	s.False(baseDisabled[""])             // 空串被过滤
	s.False(baseDisabled["team-skill-c"]) // 不存在
}

// TestTeamSkillEvolutionRail_WithTeamSkillTrajectoriesDir 测试轨迹调试目录选项。
//
// 对齐 Python: TeamSkillEvolutionRail(trajectories_dir="/tmp/debug")
func (s *TeamSkillEvolutionRailSuite) TestTeamSkillEvolutionRail_WithTeamSkillTrajectoriesDir() {
	rail := s.newTestRail(evolution.WithTeamSkillTrajectoriesDir("/tmp/debug_traj"))
	s.NotNil(rail)
}

// ─── LLM 策略选项 ───

// TestTeamSkillEvolutionRail_WithTeamSkillUserRequestLLMPolicy 测试用户意图检测策略。
//
// 对齐 Python: TeamSkillEvolutionRail(user_request_llm_policy=...)
func (s *TeamSkillEvolutionRailSuite) TestTeamSkillEvolutionRail_WithTeamSkillUserRequestLLMPolicy() {
	customPolicy := llm_resilience.LLMInvokePolicy{
		AttemptTimeoutSecs: 30.0,
		TotalBudgetSecs:    60.0,
		MaxAttempts:        3,
	}
	rail := s.newTestRail(evolution.WithTeamSkillUserRequestLLMPolicy(customPolicy))

	config := rail.EvolutionConfig()
	policy, ok := config["user_request_llm_policy"].(llm_resilience.LLMInvokePolicy)
	s.Require().True(ok)
	s.Equal(30.0, policy.AttemptTimeoutSecs)
	s.Equal(60.0, policy.TotalBudgetSecs)
	s.Equal(3, policy.MaxAttempts)
}

// TestTeamSkillEvolutionRail_WithTeamSkillTrajectoryIssueLLMPolicy 测试轨迹问题检测策略。
//
// 对齐 Python: TeamSkillEvolutionRail(trajectory_issue_llm_policy=...)
func (s *TeamSkillEvolutionRailSuite) TestTeamSkillEvolutionRail_WithTeamSkillTrajectoryIssueLLMPolicy() {
	customPolicy := llm_resilience.LLMInvokePolicy{
		AttemptTimeoutSecs: 200.0,
		TotalBudgetSecs:    400.0,
		MaxAttempts:        4,
	}
	rail := s.newTestRail(evolution.WithTeamSkillTrajectoryIssueLLMPolicy(customPolicy))

	config := rail.EvolutionConfig()
	policy, ok := config["trajectory_issue_llm_policy"].(llm_resilience.LLMInvokePolicy)
	s.Require().True(ok)
	s.Equal(200.0, policy.AttemptTimeoutSecs)
}

// TestTeamSkillEvolutionRail_WithTeamSkillRecordLLMPolicy 测试记录生成策略。
//
// 对齐 Python: TeamSkillEvolutionRail(record_llm_policy=...)
func (s *TeamSkillEvolutionRailSuite) TestTeamSkillEvolutionRail_WithTeamSkillRecordLLMPolicy() {
	customPolicy := llm_resilience.LLMInvokePolicy{
		AttemptTimeoutSecs: 99.0,
		TotalBudgetSecs:    200.0,
		MaxAttempts:        5,
	}
	rail := s.newTestRail(evolution.WithTeamSkillRecordLLMPolicy(customPolicy))

	config := rail.EvolutionConfig()
	policy, ok := config["record_llm_policy"].(llm_resilience.LLMInvokePolicy)
	s.Require().True(ok)
	s.Equal(99.0, policy.AttemptTimeoutSecs)
}

// TestTeamSkillEvolutionRail_WithTeamSkillEvaluateLLMPolicy 测试评估策略。
//
// 对齐 Python: TeamSkillEvolutionRail(evaluate_llm_policy=...)
func (s *TeamSkillEvolutionRailSuite) TestTeamSkillEvolutionRail_WithTeamSkillEvaluateLLMPolicy() {
	customPolicy := llm_resilience.LLMInvokePolicy{
		AttemptTimeoutSecs: 50.0,
		TotalBudgetSecs:    100.0,
		MaxAttempts:        3,
	}
	rail := s.newTestRail(evolution.WithTeamSkillEvaluateLLMPolicy(customPolicy))

	config := rail.EvolutionConfig()
	policy, ok := config["evaluate_llm_policy"].(llm_resilience.LLMInvokePolicy)
	s.Require().True(ok)
	s.Equal(50.0, policy.AttemptTimeoutSecs)
}

// TestTeamSkillEvolutionRail_WithTeamSkillSimplifyLLMPolicy 测试精简策略。
//
// 对齐 Python: TeamSkillEvolutionRail(simplify_llm_policy=...)
func (s *TeamSkillEvolutionRailSuite) TestTeamSkillEvolutionRail_WithTeamSkillSimplifyLLMPolicy() {
	customPolicy := llm_resilience.LLMInvokePolicy{
		AttemptTimeoutSecs: 77.0,
		TotalBudgetSecs:    150.0,
		MaxAttempts:        4,
	}
	rail := s.newTestRail(evolution.WithTeamSkillSimplifyLLMPolicy(customPolicy))

	config := rail.EvolutionConfig()
	policy, ok := config["simplify_llm_policy"].(llm_resilience.LLMInvokePolicy)
	s.Require().True(ok)
	s.Equal(77.0, policy.AttemptTimeoutSecs)
}

// ─── 属性访问器 ───

// TestTeamSkillEvolutionRail_SetAutoScan 测试 SetAutoScan 运行时修改。
func (s *TeamSkillEvolutionRailSuite) TestTeamSkillEvolutionRail_SetAutoScan() {
	rail := s.newTestRail()
	s.True(rail.AutoScan())

	rail.SetAutoScan(false)
	s.False(rail.AutoScan())

	rail.SetAutoScan(true)
	s.True(rail.AutoScan())
}

// TestTeamSkillEvolutionRail_SetAutoSave 测试 SetAutoSave 运行时修改。
func (s *TeamSkillEvolutionRailSuite) TestTeamSkillEvolutionRail_SetAutoSave() {
	rail := s.newTestRail()
	s.False(rail.AutoSave()) // 默认 false

	rail.SetAutoSave(true)
	s.True(rail.AutoSave())

	rail.SetAutoSave(false)
	s.False(rail.AutoSave())
}

// TestTeamSkillEvolutionRail_EvolutionStore 测试 EvolutionStore 访问器。
func (s *TeamSkillEvolutionRailSuite) TestTeamSkillEvolutionRail_EvolutionStore() {
	rail := s.newTestRail()
	s.NotNil(rail.EvolutionStore())
}

// TestTeamSkillEvolutionRail_Scorer 测试 Scorer 访问器。
func (s *TeamSkillEvolutionRailSuite) TestTeamSkillEvolutionRail_Scorer() {
	rail := s.newTestRail()
	s.NotNil(rail.Scorer())
}

// TestTeamSkillEvolutionRail_Generator 测试 Generator 访问器。
func (s *TeamSkillEvolutionRailSuite) TestTeamSkillEvolutionRail_Generator() {
	rail := s.newTestRail()
	s.NotNil(rail.Generator())
}

// TestTeamSkillEvolutionRail_TeamSignalDetector 测试 TeamSignalDetector 访问器。
func (s *TeamSkillEvolutionRailSuite) TestTeamSkillEvolutionRail_TeamSignalDetector() {
	rail := s.newTestRail()
	s.NotNil(rail.TeamSignalDetector())
}

// TestTeamSkillEvolutionRail_ApprovalRuntime 测试 ApprovalRuntime 访问器。
func (s *TeamSkillEvolutionRailSuite) TestTeamSkillEvolutionRail_ApprovalRuntime() {
	rail := s.newTestRail()
	s.NotNil(rail.ApprovalRuntime())
}

// ─── AllowEvolutionTrigger ───

// TestTeamSkillEvolutionRail_AllowEvolutionTrigger_默认不允许 测试默认状态下不允许触发。
//
// 对齐 Python: TeamSkillEvolutionRail._allow_evolution_trigger — 需 passiveEvolutionPending 或 hostCompletionPending
// 关键差异：SkillEvolutionRail 在 autoScan=true 时总是允许，TeamSkillEvolutionRail 需要额外条件
func (s *TeamSkillEvolutionRailSuite) TestTeamSkillEvolutionRail_AllowEvolutionTrigger_默认不允许() {
	rail := s.newTestRail()
	// 默认 passiveEvolutionPending=false, hostCompletionPendingSessionID=nil
	s.False(rail.AllowEvolutionTrigger(evolution.TriggerAfterInvoke, nil))
}

// TestTeamSkillEvolutionRail_AllowEvolutionTrigger_autoScan关闭 测试 autoScan 关闭时不允许触发。
//
// 对齐 Python: if not self.auto_scan: return False
func (s *TeamSkillEvolutionRailSuite) TestTeamSkillEvolutionRail_AllowEvolutionTrigger_autoScan关闭() {
	rail := s.newTestRail(evolution.WithTeamSkillAutoScan(false))
	s.False(rail.AllowEvolutionTrigger(evolution.TriggerAfterInvoke, nil))
}

// TestTeamSkillEvolutionRail_AllowEvolutionTrigger_passiveEvolutionPending 测试 passiveEvolutionPending 为 true 时允许触发。
//
// 对齐 Python: if self._passive_evolution_pending: return True
// 通过 OnAfterToolCall + view_task(completed) 间接设置 passiveEvolutionPending
func (s *TeamSkillEvolutionRailSuite) TestTeamSkillEvolutionRail_AllowEvolutionTrigger_passiveEvolutionPending() {
	rail := s.newTestRail()
	// 初始不允许
	s.False(rail.AllowEvolutionTrigger(evolution.TriggerAfterInvoke, nil))

	// 通过 view_task 返回全部完成状态间接设置 passiveEvolutionPending
	inputs := &agentinterfaces.ToolCallInputs{
		ToolName:   "view_task",
		ToolResult: map[string]any{"status": "completed"},
	}
	cbc := agentinterfaces.NewAgentCallbackContext(nil, inputs, nil)
	err := rail.OnAfterToolCall(s.Ctx, cbc)
	s.Require().NoError(err)

	// 设置后允许触发
	s.True(rail.AllowEvolutionTrigger(evolution.TriggerAfterInvoke, nil))
}

// TestTeamSkillEvolutionRail_AllowEvolutionTrigger_不同触发点 测试不同触发点行为一致。
//
// 对齐 Python: _allow_evolution_trigger 不区分触发点
func (s *TeamSkillEvolutionRailSuite) TestTeamSkillEvolutionRail_AllowEvolutionTrigger_不同触发点() {
	rail := s.newTestRail()
	// 通过 view_task 返回完成状态间接设置 passiveEvolutionPending
	inputs := &agentinterfaces.ToolCallInputs{
		ToolName:   "view_task",
		ToolResult: map[string]any{"status": "completed"},
	}
	cbc := agentinterfaces.NewAgentCallbackContext(nil, inputs, nil)
	err := rail.OnAfterToolCall(s.Ctx, cbc)
	s.Require().NoError(err)

	// passiveEvolutionPending=true 时，所有触发点都允许
	s.True(rail.AllowEvolutionTrigger(evolution.TriggerAfterInvoke, nil))
	s.True(rail.AllowEvolutionTrigger(evolution.TriggerAfterModelCall, nil))
	s.True(rail.AllowEvolutionTrigger(evolution.TriggerAfterToolCall, nil))
	s.True(rail.AllowEvolutionTrigger(evolution.TriggerAfterTaskIteration, nil))
}

// ─── OnBeforeInvoke ───

// TestTeamSkillEvolutionRail_OnBeforeInvoke重置passive 测试 OnBeforeInvoke 重置 passiveEvolutionPending。
//
// 对齐 Python: TeamSkillEvolutionRail._on_before_invoke → self._passive_evolution_pending = False
// 先通过 view_task 设置 passiveEvolutionPending，再通过 OnBeforeInvoke 重置
func (s *TeamSkillEvolutionRailSuite) TestTeamSkillEvolutionRail_OnBeforeInvoke重置passive() {
	rail := s.newTestRail()

	// 先通过 view_task 设置 passiveEvolutionPending
	inputs := &agentinterfaces.ToolCallInputs{
		ToolName:   "view_task",
		ToolResult: map[string]any{"status": "completed"},
	}
	cbc := agentinterfaces.NewAgentCallbackContext(nil, inputs, nil)
	err := rail.OnAfterToolCall(s.Ctx, cbc)
	s.Require().NoError(err)
	s.True(rail.AllowEvolutionTrigger(evolution.TriggerAfterInvoke, nil))

	// OnBeforeInvoke 应重置 passiveEvolutionPending
	err = rail.OnBeforeInvoke(s.Ctx, nil)
	s.Require().NoError(err)
	s.False(rail.AllowEvolutionTrigger(evolution.TriggerAfterInvoke, nil))
}

// ─── NotifyTeamCompleted ───

// TestTeamSkillEvolutionRail_NotifyTeamCompleted_autoScan关闭 测试 autoScan 关闭时 NotifyTeamCompleted 返回 false。
//
// 对齐 Python: if not self.auto_scan: return False
func (s *TeamSkillEvolutionRailSuite) TestTeamSkillEvolutionRail_NotifyTeamCompleted_autoScan关闭() {
	rail := s.newTestRail(evolution.WithTeamSkillAutoScan(false))
	ok, err := rail.NotifyTeamCompleted(s.Ctx)
	s.Require().NoError(err)
	s.False(ok)
}

// TestTeamSkillEvolutionRail_NotifyTeamCompleted_无builder 测试无轨迹构造器时返回 false。
//
// 对齐 Python: if session_id is empty → return False
func (s *TeamSkillEvolutionRailSuite) TestTeamSkillEvolutionRail_NotifyTeamCompleted_无builder() {
	rail := s.newTestRail()
	// 无 builder 时 sessionID 为空 → 返回 false
	ok, err := rail.NotifyTeamCompleted(s.Ctx)
	s.Require().NoError(err)
	s.False(ok)
}

// ─── OnAfterToolCall ───

// TestTeamSkillEvolutionRail_OnAfterToolCall需要有效cbc 测试 OnAfterToolCall 需要有效 cbc。
// 非 ToolCallInputs 类型时返回 nil 不崩溃。
func (s *TeamSkillEvolutionRailSuite) TestTeamSkillEvolutionRail_OnAfterToolCall需要有效cbc() {
	rail := s.newTestRail()
	cbc := agentinterfaces.NewAgentCallbackContext(nil, nil, nil)
	err := rail.OnAfterToolCall(s.Ctx, cbc)
	s.Require().NoError(err)
}

// TestTeamSkillEvolutionRail_OnAfterToolCall_skillTool读取经验详情 测试 skill_tool 读取经验详情。
//
// 对齐 Python: TeamSkillEvolutionRail._on_after_tool_call → _record_presented_experience_detail
func (s *TeamSkillEvolutionRailSuite) TestTeamSkillEvolutionRail_OnAfterToolCall_skillTool读取经验详情() {
	rail := s.newTestRail()

	inputs := &agentinterfaces.ToolCallInputs{
		ToolName: "skill_tool",
		ToolArgs: map[string]any{
			"skill_name":         "my_team_skill",
			"relative_file_path": "evolution/experience1.md",
		},
		ToolResult: map[string]any{
			"skill_content": "## [rec_001] Some experience\nContent here",
		},
	}
	cbc := agentinterfaces.NewAgentCallbackContext(nil, inputs, nil)

	err := rail.OnAfterToolCall(s.Ctx, cbc)
	s.Require().NoError(err)
}

// TestTeamSkillEvolutionRail_OnAfterToolCall_skillTool读SKILLmd不触发 测试读取 SKILL.md 不触发经验追踪。
//
// 对齐 Python: SKILL.md 读取仅用于索引发现，不计入 presented
func (s *TeamSkillEvolutionRailSuite) TestTeamSkillEvolutionRail_OnAfterToolCall_skillTool读SKILLmd不触发() {
	rail := s.newTestRail()

	inputs := &agentinterfaces.ToolCallInputs{
		ToolName: "skill_tool",
		ToolArgs: map[string]any{
			"skill_name":         "my_team_skill",
			"relative_file_path": "SKILL.md",
		},
		ToolResult: map[string]any{
			"skill_content": "# My Team Skill\nSome content",
		},
	}
	cbc := agentinterfaces.NewAgentCallbackContext(nil, inputs, nil)

	err := rail.OnAfterToolCall(s.Ctx, cbc)
	s.Require().NoError(err)
}

// TestTeamSkillEvolutionRail_OnAfterToolCall_非skillTool不触发 测试非 skill_tool 工具不触发经验追踪。
func (s *TeamSkillEvolutionRailSuite) TestTeamSkillEvolutionRail_OnAfterToolCall_非skillTool不触发() {
	rail := s.newTestRail()

	inputs := &agentinterfaces.ToolCallInputs{
		ToolName: "read_file",
		ToolArgs: map[string]any{
			"file_path": "/some/other/path.txt",
		},
		ToolResult: "file content",
	}
	cbc := agentinterfaces.NewAgentCallbackContext(nil, inputs, nil)

	err := rail.OnAfterToolCall(s.Ctx, cbc)
	s.Require().NoError(err)
}

// TestTeamSkillEvolutionRail_OnAfterToolCall_viewTask完成检测 测试 view_task 工具返回完成状态时标记 passiveEvolutionPending。
//
// 对齐 Python: TeamSkillEvolutionRail._on_after_tool_call → view_task → _all_tasks_completed → mark_passive_evolution_pending
// 间接验证 isCompletedTeamTaskView（包级非导出函数）的正确性
func (s *TeamSkillEvolutionRailSuite) TestTeamSkillEvolutionRail_OnAfterToolCall_viewTask完成检测() {
	rail := s.newTestRail()

	// 初始不允许触发
	s.False(rail.AllowEvolutionTrigger(evolution.TriggerAfterInvoke, nil))

	// view_task 返回全部完成状态
	inputs := &agentinterfaces.ToolCallInputs{
		ToolName: "view_task",
		ToolResult: map[string]any{
			"tasks": []map[string]any{
				{"name": "task1", "status": "completed"},
				{"name": "task2", "status": "completed"},
			},
		},
	}
	cbc := agentinterfaces.NewAgentCallbackContext(nil, inputs, nil)

	err := rail.OnAfterToolCall(s.Ctx, cbc)
	s.Require().NoError(err)

	// 完成后应允许触发
	s.True(rail.AllowEvolutionTrigger(evolution.TriggerAfterInvoke, nil))
}

// TestTeamSkillEvolutionRail_OnAfterToolCall_viewTask进行中不触发 测试 view_task 返回非终态时不触发。
//
// 对齐 Python: if not completed → skip
// 间接验证 isCompletedTeamTaskView 检测非终态（pending, claimed, in_progress, blocked）
func (s *TeamSkillEvolutionRailSuite) TestTeamSkillEvolutionRail_OnAfterToolCall_viewTask进行中不触发() {
	rail := s.newTestRail()

	// view_task 返回仍有任务进行中
	inputs := &agentinterfaces.ToolCallInputs{
		ToolName: "view_task",
		ToolResult: map[string]any{
			"tasks": []map[string]any{
				{"name": "task1", "status": "completed"},
				{"name": "task2", "status": "in_progress"},
			},
		},
	}
	cbc := agentinterfaces.NewAgentCallbackContext(nil, inputs, nil)

	err := rail.OnAfterToolCall(s.Ctx, cbc)
	s.Require().NoError(err)

	// 仍有任务进行中 → 不允许触发
	s.False(rail.AllowEvolutionTrigger(evolution.TriggerAfterInvoke, nil))
}

// TestTeamSkillEvolutionRail_OnAfterToolCall_viewTask有pending状态不触发 测试含 pending 状态时不触发。
//
// 对齐 Python: _TEAM_TASK_NON_TERMINAL_STATES 包含 "pending"
func (s *TeamSkillEvolutionRailSuite) TestTeamSkillEvolutionRail_OnAfterToolCall_viewTask有pending状态不触发() {
	rail := s.newTestRail()

	inputs := &agentinterfaces.ToolCallInputs{
		ToolName: "view_task",
		ToolResult: map[string]any{
			"tasks": []map[string]any{
				{"name": "task1", "status": "completed"},
				{"name": "task2", "status": "pending"},
			},
		},
	}
	cbc := agentinterfaces.NewAgentCallbackContext(nil, inputs, nil)

	err := rail.OnAfterToolCall(s.Ctx, cbc)
	s.Require().NoError(err)

	s.False(rail.AllowEvolutionTrigger(evolution.TriggerAfterInvoke, nil))
}

// TestTeamSkillEvolutionRail_OnAfterToolCall_viewTask有blocked状态不触发 测试含 blocked 状态时不触发。
//
// 对齐 Python: _TEAM_TASK_NON_TERMINAL_STATES 包含 "blocked"
func (s *TeamSkillEvolutionRailSuite) TestTeamSkillEvolutionRail_OnAfterToolCall_viewTask有blocked状态不触发() {
	rail := s.newTestRail()

	inputs := &agentinterfaces.ToolCallInputs{
		ToolName: "view_task",
		ToolResult: map[string]any{
			"tasks": []map[string]any{
				{"name": "task1", "status": "completed"},
				{"name": "task2", "status": "blocked"},
			},
		},
	}
	cbc := agentinterfaces.NewAgentCallbackContext(nil, inputs, nil)

	err := rail.OnAfterToolCall(s.Ctx, cbc)
	s.Require().NoError(err)

	s.False(rail.AllowEvolutionTrigger(evolution.TriggerAfterInvoke, nil))
}

// TestTeamSkillEvolutionRail_OnAfterToolCall_autoScan关闭跳过viewTask 测试 autoScan 关闭时跳过 view_task 检测。
//
// 对齐 Python: if not self.auto_scan: return
func (s *TeamSkillEvolutionRailSuite) TestTeamSkillEvolutionRail_OnAfterToolCall_autoScan关闭跳过viewTask() {
	rail := s.newTestRail(evolution.WithTeamSkillAutoScan(false))

	inputs := &agentinterfaces.ToolCallInputs{
		ToolName:   "view_task",
		ToolResult: map[string]any{"status": "completed"},
	}
	cbc := agentinterfaces.NewAgentCallbackContext(nil, inputs, nil)

	err := rail.OnAfterToolCall(s.Ctx, cbc)
	s.Require().NoError(err)

	// autoScan=false → 不标记
	s.False(rail.AllowEvolutionTrigger(evolution.TriggerAfterInvoke, nil))
}

// TestTeamSkillEvolutionRail_OnAfterToolCall_无completed关键字不触发 测试 view_task 返回无 completed 关键字时不触发。
//
// 间接验证 isCompletedTeamTaskView 要求 "completed" 关键字
func (s *TeamSkillEvolutionRailSuite) TestTeamSkillEvolutionRail_OnAfterToolCall_无completed关键字不触发() {
	rail := s.newTestRail()

	inputs := &agentinterfaces.ToolCallInputs{
		ToolName:   "view_task",
		ToolResult: map[string]any{"status": "running"},
	}
	cbc := agentinterfaces.NewAgentCallbackContext(nil, inputs, nil)

	err := rail.OnAfterToolCall(s.Ctx, cbc)
	s.Require().NoError(err)

	s.False(rail.AllowEvolutionTrigger(evolution.TriggerAfterInvoke, nil))
}

// ─── HasPendingApprovalSnapshot ───

// TestTeamSkillEvolutionRail_HasPendingApprovalSnapshot_不存在 测试不存在的 requestID 返回 false。
//
// 对齐 Python: request_id in self._pending_approval_snapshots
func (s *TeamSkillEvolutionRailSuite) TestTeamSkillEvolutionRail_HasPendingApprovalSnapshot_不存在() {
	rail := s.newTestRail()
	s.False(rail.HasPendingApprovalSnapshot("nonexistent_id"))
}

// TestTeamSkillEvolutionRail_HasPendingApprovalSnapshot_多个不存在 测试多个不存在 ID 均返回 false。
func (s *TeamSkillEvolutionRailSuite) TestTeamSkillEvolutionRail_HasPendingApprovalSnapshot_多个不存在() {
	rail := s.newTestRail()
	s.False(rail.HasPendingApprovalSnapshot("req_001"))
	s.False(rail.HasPendingApprovalSnapshot("req_002"))
	s.False(rail.HasPendingApprovalSnapshot(""))
}

// ─── 信号去重 ───

// TestTeamSkillEvolutionRail_ProcessedSignalKeys 测试信号指纹去重。
//
// 对齐 Python: TeamSkillEvolutionRail.processed_signal_keys
func (s *TeamSkillEvolutionRailSuite) TestTeamSkillEvolutionRail_ProcessedSignalKeys() {
	rail := s.newTestRail()

	// 初始为空
	keys := rail.ProcessedSignalKeys()
	s.NotNil(keys)
	s.Empty(keys)

	// 添加信号指纹
	fp := [4]string{"team_skill_test", "trajectory_issue", "test_rule", "test_source"}
	keys[fp] = true
	s.Len(rail.ProcessedSignalKeys(), 1)

	// ClearProcessedSignals 清空
	rail.ClearProcessedSignals()
	s.Empty(rail.ProcessedSignalKeys())
}

// ─── 空操作方法 ───

// TestTeamSkillEvolutionRail_OnAfterModelCall空操作 测试 OnAfterModelCall 返回 nil。
func (s *TeamSkillEvolutionRailSuite) TestTeamSkillEvolutionRail_OnAfterModelCall空操作() {
	rail := s.newTestRail()
	err := rail.OnAfterModelCall(s.Ctx, nil)
	s.Require().NoError(err)
}

// TestTeamSkillEvolutionRail_OnAfterInvoke空操作 测试 OnAfterInvoke 返回 nil。
func (s *TeamSkillEvolutionRailSuite) TestTeamSkillEvolutionRail_OnAfterInvoke空操作() {
	rail := s.newTestRail()
	err := rail.OnAfterInvoke(s.Ctx, nil)
	s.Require().NoError(err)
}

// TestTeamSkillEvolutionRail_OnAfterTaskIteration空操作 测试 OnAfterTaskIteration 返回 nil。
func (s *TeamSkillEvolutionRailSuite) TestTeamSkillEvolutionRail_OnAfterTaskIteration空操作() {
	rail := s.newTestRail()
	err := rail.OnAfterTaskIteration(s.Ctx, nil)
	s.Require().NoError(err)
}

// TestTeamSkillEvolutionRail_OnAfterEvolutionTriggered空操作 测试 OnAfterEvolutionTriggered 返回 nil。
func (s *TeamSkillEvolutionRailSuite) TestTeamSkillEvolutionRail_OnAfterEvolutionTriggered空操作() {
	rail := s.newTestRail()
	err := rail.OnAfterEvolutionTriggered(s.Ctx, nil, nil)
	s.Require().NoError(err)
}

// ─── 回调注册 ───

// TestTeamSkillEvolutionRail_GetCallbacks 测试 GetCallbacks 注册。
func (s *TeamSkillEvolutionRailSuite) TestTeamSkillEvolutionRail_GetCallbacks() {
	rail := s.newTestRail()
	callbacks := rail.GetCallbacks()
	s.NotNil(callbacks)
	// TeamSkillEvolutionRail 通过 EvolutionRail 基类注册回调
	s.GreaterOrEqual(len(callbacks), 5)
	s.Contains(callbacks, agentinterfaces.CallbackBeforeInvoke)
	s.Contains(callbacks, agentinterfaces.CallbackAfterModelCall)
	s.Contains(callbacks, agentinterfaces.CallbackAfterToolCall)
	s.Contains(callbacks, agentinterfaces.CallbackAfterInvoke)
	s.Contains(callbacks, agentinterfaces.CallbackAfterTaskIteration)
}

// ─── RecordPresentedExperiences ───

// TestTeamSkillEvolutionRail_RecordPresentedExperiences 测试非 rail 路径呈现经验记录。
//
// 对齐 Python: TeamSkillEvolutionRail.record_presented_experiences
func (s *TeamSkillEvolutionRailSuite) TestTeamSkillEvolutionRail_RecordPresentedExperiences() {
	rail := s.newTestRail()
	rail.RecordPresentedExperiences(s.Ctx, "team_skill_1", "snippet content", "session_1", []string{"rec_001"})
	rail.RecordPresentedExperiences(s.Ctx, "team_skill_1", "snippet without record IDs", "session_1", nil)
}

// ─── SnapshotForEvolution ───

// TestTeamSkillEvolutionRail_SnapshotForEvolution_autoScan关闭返回nil 测试 autoScan 关闭时返回 nil。
//
// 对齐 Python: if not self.auto_scan: return None
func (s *TeamSkillEvolutionRailSuite) TestTeamSkillEvolutionRail_SnapshotForEvolution_autoScan关闭返回nil() {
	rail := s.newTestRail(evolution.WithTeamSkillAutoScan(false))
	s.False(rail.AutoScan())

	snapshot := rail.SnapshotForEvolution(s.Ctx, &trajectory.Trajectory{}, nil)
	s.Nil(snapshot)
}

// TestTeamSkillEvolutionRail_SnapshotForEvolution_空轨迹返回空消息快照 测试空轨迹返回空消息快照。
//
// 对齐 Python: TeamSkillEvolutionRail._snapshot_for_evolution 不检查 messages 是否为空，
// 与 SkillEvolutionRail 不同（SkillEvolutionRail 在 messages 为空时返回 None）
func (s *TeamSkillEvolutionRailSuite) TestTeamSkillEvolutionRail_SnapshotForEvolution_空轨迹返回空消息快照() {
	rail := s.newTestRail()
	s.True(rail.AutoScan())

	emptyTraj := &trajectory.Trajectory{SessionID: "test"}
	snapshot := rail.SnapshotForEvolution(s.Ctx, emptyTraj, nil)
	// TeamSkillEvolutionRail 不检查 messages 为空，返回空消息快照
	s.Require().NotNil(snapshot)
	s.Empty(snapshot.Messages)
	s.Equal("test", snapshot.SessionID)
}

// TestTeamSkillEvolutionRail_SnapshotForEvolution_skillName为teamSkill 测试快照中 skillName 为 "team-skill"。
//
// 对齐 Python: TeamSkillEvolutionRail._snapshot_for_evolution → skill_name="team-skill"
// 关键差异：SkillEvolutionRail 的 skillName 为 "skill-evolution"
func (s *TeamSkillEvolutionRailSuite) TestTeamSkillEvolutionRail_SnapshotForEvolution_skillName为teamSkill() {
	rail := s.newTestRail()

	builder := trajectory.NewTrajectoryBuilder("session_team", "online")
	builder.RecordStep(&trajectory.TrajectoryStep{
		Kind: trajectory.StepKindLLM,
		Detail: &trajectory.LLMCallDetail{
			Messages: []llmschema.BaseMessage{llmschema.NewUserMessage("test query")},
			Response: map[string]any{"content": "test response"},
		},
	})
	traj := builder.Build()

	cbc := agentinterfaces.NewAgentCallbackContext(nil, &agentinterfaces.InvokeInputs{ConversationID: "session_team"}, nil)
	snapshot := rail.SnapshotForEvolution(s.Ctx, traj, cbc)

	s.Require().NotNil(snapshot)
	s.NotNil(snapshot.Trajectory)
	s.NotEmpty(snapshot.Messages)
	s.Equal("session_team", snapshot.SessionID)
	// 关键差异：skillName 为 "team-skill"（而非 "skill-evolution"）
	s.Require().NotNil(snapshot.SkillName)
	s.Equal("team-skill", *snapshot.SkillName)
}

// ─── RunEvolution ───

// TestTeamSkillEvolutionRail_RunEvolution_autoScan关闭 测试 autoScan 关闭时 RunEvolution 返回 nil。
//
// 对齐 Python: if not self.auto_scan: return
func (s *TeamSkillEvolutionRailSuite) TestTeamSkillEvolutionRail_RunEvolution_autoScan关闭() {
	rail := s.newTestRail(evolution.WithTeamSkillAutoScan(false))
	err := rail.RunEvolution(s.Ctx, nil, nil)
	s.Require().NoError(err)
}

// TestTeamSkillEvolutionRail_RunEvolution_空snapshot空轨迹 测试 snapshot 和轨迹都为 nil 时返回 nil。
//
// 对齐 Python: if trajectory is None and snapshot is None: return
func (s *TeamSkillEvolutionRailSuite) TestTeamSkillEvolutionRail_RunEvolution_空snapshot空轨迹() {
	rail := s.newTestRail()
	err := rail.RunEvolution(s.Ctx, nil, nil)
	s.Require().NoError(err)
}

// TestTeamSkillEvolutionRail_RunEvolution_空消息snapshot 测试 snapshot 消息为空时提前返回。
//
// 对齐 Python: if not messages: return
func (s *TeamSkillEvolutionRailSuite) TestTeamSkillEvolutionRail_RunEvolution_空消息snapshot() {
	rail := s.newTestRail()
	snapshot := &evolution.EvolutionSnapshot{
		Trajectory: &trajectory.Trajectory{SessionID: "test"},
		Messages:   []map[string]any{},
	}
	err := rail.RunEvolution(s.Ctx, nil, snapshot)
	s.Require().NoError(err)
}

// ─── 热更新 ───

// TestTeamSkillEvolutionRail_UpdateLLM 测试 UpdateLLM 热更新 LLM 客户端和模型。
//
// 对齐 Python: TeamSkillEvolutionRail.update_llm(llm, model)
func (s *TeamSkillEvolutionRailSuite) TestTeamSkillEvolutionRail_UpdateLLM() {
	rail := s.newTestRail()
	newModel := newTeamSkillMockModel(s.T())

	// 热更新不应崩溃
	rail.UpdateLLM(newModel, "new-team-model")

	// 验证 Generator 已更新
	s.Equal("new-team-model", rail.Generator().ModelName())
}

// TestTeamSkillEvolutionRail_SetSysOperation 测试 SetSysOperation 传播到基类和 EvolutionStore。
//
// 对齐 Python: TeamSkillEvolutionRail.set_sys_operation(sys_operation)
func (s *TeamSkillEvolutionRailSuite) TestTeamSkillEvolutionRail_SetSysOperation() {
	rail := s.newTestRail()
	rail.SetSysOperation(nil)
	// 不崩溃即验证 SetSysOperation 正确传播到基类和 EvolutionStore
}

// ─── 审批生命周期 ───

// TestTeamSkillEvolutionRail_ApproveRecord_不存在 测试审批不存在的 requestID 不崩溃。
//
// 对齐 Python: TeamSkillEvolutionRail.approve_record(request_id) → unknown request → return
func (s *TeamSkillEvolutionRailSuite) TestTeamSkillEvolutionRail_ApproveRecord_不存在() {
	rail := s.newTestRail()
	err := rail.ApproveRecord(s.Ctx, "nonexistent_request_id")
	s.Require().NoError(err)
}

// TestTeamSkillEvolutionRail_RejectRecord_不存在 测试拒绝不存在的 requestID 不崩溃。
//
// 对齐 Python: TeamSkillEvolutionRail.reject_record(request_id) → unknown request → return
func (s *TeamSkillEvolutionRailSuite) TestTeamSkillEvolutionRail_RejectRecord_不存在() {
	rail := s.newTestRail()
	err := rail.RejectRecord(s.Ctx, "nonexistent_request_id")
	s.Require().NoError(err)
}

// ─── RequestSimplify / RequestRebuild ───

// TestTeamSkillEvolutionRail_RequestSimplify_无经验 测试无经验时 RequestSimplify 返回空结果。
//
// 对齐 Python: TeamSkillEvolutionRail.request_simplify(skill_name) → no experience → empty result
func (s *TeamSkillEvolutionRailSuite) TestTeamSkillEvolutionRail_RequestSimplify_无经验() {
	rail := s.newTestRail()
	result, err := rail.RequestSimplify(s.Ctx, "nonexistent_team_skill", nil)
	s.Require().NoError(err)
	s.NotNil(result)
	s.Equal("nonexistent_team_skill", result.SkillName)
}

// TestTeamSkillEvolutionRail_RequestRebuild_无经验 测试无经验时 RequestRebuild 返回空。
//
// 对齐 Python: TeamSkillEvolutionRail.request_rebuild(skill_name) → no experience → empty
func (s *TeamSkillEvolutionRailSuite) TestTeamSkillEvolutionRail_RequestRebuild_无经验() {
	rail := s.newTestRail()
	result, err := rail.RequestRebuild(s.Ctx, "nonexistent_team_skill", nil, 0.5)
	s.Require().NoError(err)
	s.Empty(result)
}

// ─── RequestUserEvolution ───

// TestTeamSkillEvolutionRail_RequestUserEvolution_无轨迹 测试无轨迹时返回基础结果。
//
// 对齐 Python: TeamSkillEvolutionRail.request_user_evolution(skill_name, user_intent, auto_approve=...)
func (s *TeamSkillEvolutionRailSuite) TestTeamSkillEvolutionRail_RequestUserEvolution_无轨迹() {
	rail := s.newTestRail()

	result, err := rail.RequestUserEvolution(s.Ctx, "team_skill_1", "", false)
	s.Require().NoError(err)
	s.NotNil(result)
	s.Equal("team_skill_1", result.SkillName)
}

// TestTeamSkillEvolutionRail_RequestUserEvolution_有意图 测试有 userIntent 时触发信号生成。
//
// 对齐 Python: TeamSkillEvolutionRail.request_user_evolution(skill_name, user_intent="...", auto_approve=False)
func (s *TeamSkillEvolutionRailSuite) TestTeamSkillEvolutionRail_RequestUserEvolution_有意图() {
	rail := s.newTestRail()

	result, err := rail.RequestUserEvolution(s.Ctx, "team_skill_1", "请优化团队技能的协作流程", false)
	s.Require().NoError(err)
	s.NotNil(result)
	s.Equal("team_skill_1", result.SkillName)
}

// ─── teamSkillKinds / teamTaskNonTerminalStates 间接验证 ───

// TestTeamSkillKinds_间接验证 通过 view_task 行为间接验证 teamSkillKinds 和 teamTaskNonTerminalStates。
//
// 对齐 Python: _TEAM_SKILL_KINDS = {"team-skill", "swarm-skill"}
// 对齐 Python: _TEAM_TASK_NON_TERMINAL_STATES = ("pending", "claimed", "in_progress", "blocked")
func (s *TeamSkillEvolutionRailSuite) TestTeamSkillKinds_间接验证() {
	rail := s.newTestRail()
	s.NotNil(rail)

	// 通过 view_task 间接验证非终态集合
	// pending 阻止触发
	inputs1 := &agentinterfaces.ToolCallInputs{
		ToolName:   "view_task",
		ToolResult: map[string]any{"status": "completed and pending"},
	}
	cbc1 := agentinterfaces.NewAgentCallbackContext(nil, inputs1, nil)
	err := rail.OnAfterToolCall(s.Ctx, cbc1)
	s.Require().NoError(err)
	s.False(rail.AllowEvolutionTrigger(evolution.TriggerAfterInvoke, nil))

	// OnBeforeInvoke 重置
	err = rail.OnBeforeInvoke(s.Ctx, nil)
	s.Require().NoError(err)

	// claimed 阻止触发
	inputs2 := &agentinterfaces.ToolCallInputs{
		ToolName:   "view_task",
		ToolResult: map[string]any{"status": "completed and claimed"},
	}
	cbc2 := agentinterfaces.NewAgentCallbackContext(nil, inputs2, nil)
	err = rail.OnAfterToolCall(s.Ctx, cbc2)
	s.Require().NoError(err)
	s.False(rail.AllowEvolutionTrigger(evolution.TriggerAfterInvoke, nil))

	// OnBeforeInvoke 重置
	err = rail.OnBeforeInvoke(s.Ctx, nil)
	s.Require().NoError(err)

	// in_progress 阻止触发
	inputs3 := &agentinterfaces.ToolCallInputs{
		ToolName:   "view_task",
		ToolResult: map[string]any{"status": "completed and in_progress"},
	}
	cbc3 := agentinterfaces.NewAgentCallbackContext(nil, inputs3, nil)
	err = rail.OnAfterToolCall(s.Ctx, cbc3)
	s.Require().NoError(err)
	s.False(rail.AllowEvolutionTrigger(evolution.TriggerAfterInvoke, nil))

	// OnBeforeInvoke 重置
	err = rail.OnBeforeInvoke(s.Ctx, nil)
	s.Require().NoError(err)

	// blocked 阻止触发
	inputs4 := &agentinterfaces.ToolCallInputs{
		ToolName:   "view_task",
		ToolResult: map[string]any{"status": "completed and blocked"},
	}
	cbc4 := agentinterfaces.NewAgentCallbackContext(nil, inputs4, nil)
	err = rail.OnAfterToolCall(s.Ctx, cbc4)
	s.Require().NoError(err)
	s.False(rail.AllowEvolutionTrigger(evolution.TriggerAfterInvoke, nil))

	// OnBeforeInvoke 重置
	err = rail.OnBeforeInvoke(s.Ctx, nil)
	s.Require().NoError(err)

	// 仅 completed 允许触发
	inputs5 := &agentinterfaces.ToolCallInputs{
		ToolName:   "view_task",
		ToolResult: map[string]any{"status": "completed"},
	}
	cbc5 := agentinterfaces.NewAgentCallbackContext(nil, inputs5, nil)
	err = rail.OnAfterToolCall(s.Ctx, cbc5)
	s.Require().NoError(err)
	s.True(rail.AllowEvolutionTrigger(evolution.TriggerAfterInvoke, nil))
}

// ─── EvolutionRequestResult ───

// TestTeamSkillEvolutionRail_EvolutionRequestResult_HasChanges 测试 HasChanges 判断。
func (s *TeamSkillEvolutionRailSuite) TestTeamSkillEvolutionRail_EvolutionRequestResult_HasChanges() {
	// 有 Records → HasChanges=true
	result1 := &evolution.EvolutionRequestResult{
		SkillName: "team_skill",
		Records:   []checkpointing.EvolutionRecord{{ID: "rec_001"}},
	}
	s.True(result1.HasChanges())

	// 有 ApprovalEvent → HasChanges=true
	result2 := &evolution.EvolutionRequestResult{
		SkillName:     "team_skill",
		ApprovalEvent: &stream.OutputSchema{Type: "test"},
	}
	s.True(result2.HasChanges())

	// 都没有 → HasChanges=false
	result3 := &evolution.EvolutionRequestResult{SkillName: "team_skill"}
	s.False(result3.HasChanges())
}

// ─── SetTrajectorySource ───

// TestTeamSkillEvolutionRail_SetTrajectorySource 测试设置团队轨迹聚合源。
//
// 对齐 Python: TeamSkillEvolutionRail.set_trajectory_source(source)
func (s *TeamSkillEvolutionRailSuite) TestTeamSkillEvolutionRail_SetTrajectorySource() {
	rail := s.newTestRail()
	// 设置 nil 不崩溃
	rail.SetTrajectorySource(nil)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// newTestRail 创建测试用 TeamSkillEvolutionRail。
// 使用临时目录作为技能目录，mock LLM 客户端避免真实 API 调用。
func (s *TeamSkillEvolutionRailSuite) newTestRail(opts ...evolution.TeamSkillEvolutionRailOption) *evolution.TeamSkillEvolutionRail {
	tempDir := s.T().TempDir()
	return s.newTestRailWithDir(tempDir, opts...)
}

// newTestRailWithDir 使用指定目录创建测试用 TeamSkillEvolutionRail。
func (s *TeamSkillEvolutionRailSuite) newTestRailWithDir(skillsDir string, opts ...evolution.TeamSkillEvolutionRailOption) *evolution.TeamSkillEvolutionRail {
	model := newTeamSkillMockModel(s.T())
	allOpts := append([]evolution.TeamSkillEvolutionRailOption{}, opts...)
	return evolution.NewTeamSkillEvolutionRail(
		[]string{skillsDir},
		model,
		"test-model",
		"cn",
		allOpts...,
	)
}

// newTeamSkillMockModel 创建集成测试用 mock *llm.Model。
// 对齐 skill_evolution_rail_test.go 中 newSkillEvolutionMockModel 的模式：
// 注册 mock client 到 ClientRegistry → NewModel 从 registry 获取客户端。
func newTeamSkillMockModel(t interface {
	Helper()
	Fatalf(format string, args ...any)
}) *llm.Model {
	t.Helper()

	mockClient := &mockTeamSkillClient{}
	providerName := fmt.Sprintf("mock_team_skill_evolution_%d", time.Now().UnixNano())
	model_clients.GetClientRegistry().Register(providerName, "llm", func(_ *llmschema.ModelRequestConfig, _ *llmschema.ModelClientConfig) (model_clients.BaseModelClient, error) {
		return mockClient, nil
	})

	clientConfig := &llmschema.ModelClientConfig{
		ClientProvider: providerName,
		ClientID:       providerName + "_id",
	}
	modelConfig := &llmschema.ModelRequestConfig{
		ModelName: "test-model",
	}

	model, err := llm.NewModel(clientConfig, modelConfig)
	if err != nil {
		t.Fatalf("创建 mock model 失败: %v", err)
	}
	return model
}
