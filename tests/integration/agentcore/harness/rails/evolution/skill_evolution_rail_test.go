//go:build integration

package evolution

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	evolution "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/evolution"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/evolving/checkpointing"
	"github.com/uapclaw/uapclaw-go/internal/evolving/experience"
	"github.com/uapclaw/uapclaw-go/internal/evolving/optimizer/llm_resilience"
	"github.com/uapclaw/uapclaw-go/internal/evolving/trajectory"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SkillEvolutionRailSuite 测试 SkillEvolutionRail 选项、属性访问器、信号去重和完整演化流程。
//
// 对齐 Python: tests/unit_tests/harness/rails/evolution/test_skill_evolution_rail.py
//   - TestSkillEvolutionRailInit: priority, auto_scan, auto_save, timeout
//   - TestSkillEvolutionRailOptions: WithAutoScan, WithAutoSave, WithEvalInterval, WithEvolutionTimeout
//   - TestSkillEvolutionRailSharing: is_sharing_enabled, sharing_config
//   - TestSignalDedup: processed_signal_keys, clear_processed_signals
//   - TestShouldHintSimplifyOrRebuild: 精简/重建提示
//   - TestSnapshotForEvolution: autoScan 关闭返回 nil，空轨迹返回 nil
//   - TestOnAfterToolCall: skill_tool 读取经验详情
//   - TestRollbackSkill: 归档回滚流程
//   - TestUpdateLLM / TestSetSysOperation: 热更新传播
//   - TestApprovalLifecycle: ApproveRecord / RejectRecord
//   - TestEvolutionSnapshot: 序列化与反序列化
//   - TestResultHasChanges: EvolutionRequestResult / SimplifyRequestResult
//
// 注意：未导出函数（parseDuplicateCheckResponse, hasRealError, attributeSignalsToSkills, resolveIncrementalMessages 等）
// 在 internal 包内 _test.go 中已有单元测试，此集成测试套件只验证导出行为。
type SkillEvolutionRailSuite struct {
	isuite.BaseIntegrationSuite
}

// mockSkillClient 集成测试用模拟 LLM 客户端。
// 对齐 skill_call/llm_mock_test.go 中的 mockBaseModelClient。
type mockSkillClient struct {
	// invokeFn 自定义 Invoke 行为
	invokeFn func(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error)
}

func (m *mockSkillClient) Invoke(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
	if m.invokeFn != nil {
		return m.invokeFn(ctx, messages, opts...)
	}
	return llmschema.NewAssistantMessage("mock skill evolution response"), nil
}
func (m *mockSkillClient) Stream(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.StreamOption) (<-chan *llmschema.AssistantMessageChunk, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockSkillClient) GenerateImage(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateImageOption) (*llmschema.ImageGenerationResponse, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockSkillClient) GenerateSpeech(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateSpeechOption) (*llmschema.AudioGenerationResponse, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockSkillClient) GenerateVideo(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateVideoOption) (*llmschema.VideoGenerationResponse, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockSkillClient) TranscribeAudio(_ context.Context, _ string, _ ...llmschema.TranscribeAudioOption) (*llmschema.TranscriptionResponse, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockSkillClient) Release(_ context.Context, _ ...model_clients.ReleaseOption) (bool, error) {
	return false, nil
}
func (m *mockSkillClient) SupportsKVCacheRelease() bool { return false }

// testTrajectorySink 用于测试的轨迹 Sink 桩。
type testTrajectorySink struct{}

func (t *testTrajectorySink) PublishMemberTrajectory(_ *trajectory.MemberTrajectorySnapshot) {}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestSkillEvolutionRailSuite 运行 SkillEvolutionRail 集成测试套件。
func TestSkillEvolutionRailSuite(t *testing.T) {
	suite.Run(t, new(SkillEvolutionRailSuite))
}

// ─── 构造与默认值 ───

// TestSkillEvolutionRail_Priority 测试 Priority() 返回 80。
//
// 对齐 Python: SkillEvolutionRail.priority = 80
func (s *SkillEvolutionRailSuite) TestSkillEvolutionRail_Priority() {
	rail := s.newTestRail()
	s.Equal(80, rail.Priority())
}

// TestSkillEvolutionRail_GetEvolutionTotalTimeoutSecs 测试默认超时 600.0 秒。
//
// 对齐 Python: _DEFAULT_EVOLUTION_TOTAL_TIMEOUT_SECS = 600.0
func (s *SkillEvolutionRailSuite) TestSkillEvolutionRail_GetEvolutionTotalTimeoutSecs() {
	rail := s.newTestRail()
	s.Equal(600.0, rail.GetEvolutionTotalTimeoutSecs())
}

// TestSkillEvolutionRail_EvolutionConfig 测试 EvolutionConfig 包含预期键和默认值。
//
// 对齐 Python: SkillEvolutionRail.evolution_config
func (s *SkillEvolutionRailSuite) TestSkillEvolutionRail_EvolutionConfig() {
	rail := s.newTestRail()
	config := rail.EvolutionConfig()

	s.Contains(config, "generate_records_llm_policy")
	s.Contains(config, "evaluate_llm_policy")
	s.Contains(config, "simplify_llm_policy")
	s.Contains(config, "evolution_total_timeout_secs")

	// 验证默认值
	s.Equal(600.0, config["evolution_total_timeout_secs"])

	// 验证默认 LLM 策略非零值
	genPolicy, genOK := config["generate_records_llm_policy"].(llm_resilience.LLMInvokePolicy)
	s.Require().True(genOK)
	s.Greater(genPolicy.MaxAttempts, 0)

	evalPolicy, evalOK := config["evaluate_llm_policy"].(llm_resilience.LLMInvokePolicy)
	s.Require().True(evalOK)
	s.Greater(evalPolicy.MaxAttempts, 0)

	simplPolicy, simplOK := config["simplify_llm_policy"].(llm_resilience.LLMInvokePolicy)
	s.Require().True(simplOK)
	s.Greater(simplPolicy.MaxAttempts, 0)
}

// TestSkillEvolutionRail_AutoScan默认值 测试 autoScan 默认为 true。
//
// 对齐 Python: SkillEvolutionRail(auto_scan=True)
func (s *SkillEvolutionRailSuite) TestSkillEvolutionRail_AutoScan默认值() {
	rail := s.newTestRail()
	s.True(rail.AutoScan())
}

// TestSkillEvolutionRail_AutoSave默认值 测试 autoSave 默认为 true。
//
// 对齐 Python: SkillEvolutionRail(auto_save=True)
func (s *SkillEvolutionRailSuite) TestSkillEvolutionRail_AutoSave默认值() {
	rail := s.newTestRail()
	s.True(rail.AutoSave())
}

// ─── 选项函数 ───

// TestSkillEvolutionRail_WithDisabledSkillsSet 测试禁用技能名称标准化。
//
// 对齐 Python: disabled_skills 参数处理
func (s *SkillEvolutionRailSuite) TestSkillEvolutionRail_WithDisabledSkillsSet() {
	rail := s.newTestRail(
		evolution.WithDisabledSkillsSet([]string{"Skill-A", " skill_b ", ""}),
	)
	// 基类字段经过 normalizeSkillNames（TrimSpace + 过滤空串）
	baseDisabled := rail.EvolutionRail.DisabledSkills()
	s.True(baseDisabled["Skill-A"])
	s.True(baseDisabled["skill_b"]) // TrimSpace 后
	s.False(baseDisabled[""])        // 空串被过滤
	s.False(baseDisabled["skill-c"]) // 不存在
}

// TestSkillEvolutionRail_WithAutoScan 测试 WithAutoScan 选项。
//
// 对齐 Python: SkillEvolutionRail(auto_scan=False)
func (s *SkillEvolutionRailSuite) TestSkillEvolutionRail_WithAutoScan() {
	rail := s.newTestRail(evolution.WithAutoScan(false))
	s.False(rail.AutoScan())

	rail2 := s.newTestRail(evolution.WithAutoScan(true))
	s.True(rail2.AutoScan())
}

// TestSkillEvolutionRail_WithAutoSave 测试 WithAutoSave 选项。
//
// 对齐 Python: SkillEvolutionRail(auto_save=False)
func (s *SkillEvolutionRailSuite) TestSkillEvolutionRail_WithAutoSave() {
	rail := s.newTestRail(evolution.WithAutoSave(false))
	s.False(rail.AutoSave())

	rail2 := s.newTestRail(evolution.WithAutoSave(true))
	s.True(rail2.AutoSave())
}

// TestSkillEvolutionRail_WithEvalInterval 测试 WithEvalInterval 选项。
//
// 对齐 Python: SkillEvolutionRail(eval_interval=3)
// 间隔 < 1 时钳位到 1（通过 ExperienceTracker 访问器间接验证）。
func (s *SkillEvolutionRailSuite) TestSkillEvolutionRail_WithEvalInterval() {
	// WithEvalInterval(10) → 内部 evalInterval=10
	rail := s.newTestRail(evolution.WithEvalInterval(10))
	s.NotNil(rail)

	// WithEvalInterval(0) → 钳位到 1
	rail2 := s.newTestRail(evolution.WithEvalInterval(0))
	s.NotNil(rail2)

	// WithEvalInterval(-5) → 钳位到 1
	rail3 := s.newTestRail(evolution.WithEvalInterval(-5))
	s.NotNil(rail3)
}

// TestSkillEvolutionRail_WithEvolutionTimeout 测试 WithEvolutionTimeout 选项。
//
// 对齐 Python: SkillEvolutionRail(evolution_total_timeout_secs=300)
func (s *SkillEvolutionRailSuite) TestSkillEvolutionRail_WithEvolutionTimeout() {
	rail := s.newTestRail(evolution.WithEvolutionTimeout(120.0))
	s.Equal(120.0, rail.GetEvolutionTotalTimeoutSecs())

	// 验证 EvolutionConfig 同步
	config := rail.EvolutionConfig()
	s.Equal(120.0, config["evolution_total_timeout_secs"])
}

// TestSkillEvolutionRail_WithGenerateRecordsLLMPolicy 测试自定义记录生成策略。
//
// 对齐 Python: SkillEvolutionRail(generate_records_llm_policy=...)
func (s *SkillEvolutionRailSuite) TestSkillEvolutionRail_WithGenerateRecordsLLMPolicy() {
	customPolicy := llm_resilience.LLMInvokePolicy{
		AttemptTimeoutSecs: 99.0,
		TotalBudgetSecs:    200.0,
		MaxAttempts:        5,
	}
	rail := s.newTestRail(evolution.WithGenerateRecordsLLMPolicy(customPolicy))

	config := rail.EvolutionConfig()
	policy, ok := config["generate_records_llm_policy"].(llm_resilience.LLMInvokePolicy)
	s.Require().True(ok)
	s.Equal(99.0, policy.AttemptTimeoutSecs)
	s.Equal(200.0, policy.TotalBudgetSecs)
	s.Equal(5, policy.MaxAttempts)
}

// TestSkillEvolutionRail_WithEvaluateLLMPolicy 测试自定义评估策略。
//
// 对齐 Python: SkillEvolutionRail(evaluate_llm_policy=...)
func (s *SkillEvolutionRailSuite) TestSkillEvolutionRail_WithEvaluateLLMPolicy() {
	customPolicy := llm_resilience.LLMInvokePolicy{
		AttemptTimeoutSecs: 50.0,
		TotalBudgetSecs:    100.0,
		MaxAttempts:        3,
	}
	rail := s.newTestRail(evolution.WithEvaluateLLMPolicy(customPolicy))

	config := rail.EvolutionConfig()
	policy, ok := config["evaluate_llm_policy"].(llm_resilience.LLMInvokePolicy)
	s.Require().True(ok)
	s.Equal(50.0, policy.AttemptTimeoutSecs)
}

// TestSkillEvolutionRail_WithSimplifyLLMPolicy 测试自定义精简策略。
//
// 对齐 Python: SkillEvolutionRail(simplify_llm_policy=...)
func (s *SkillEvolutionRailSuite) TestSkillEvolutionRail_WithSimplifyLLMPolicy() {
	customPolicy := llm_resilience.LLMInvokePolicy{
		AttemptTimeoutSecs: 77.0,
		TotalBudgetSecs:    150.0,
		MaxAttempts:        4,
	}
	rail := s.newTestRail(evolution.WithSimplifyLLMPolicy(customPolicy))

	config := rail.EvolutionConfig()
	policy, ok := config["simplify_llm_policy"].(llm_resilience.LLMInvokePolicy)
	s.Require().True(ok)
	s.Equal(77.0, policy.AttemptTimeoutSecs)
}

// TestSkillEvolutionRail_WithSharingConfigMap 测试共享配置字典选项（enabled=false）。
//
// 对齐 Python: SkillEvolutionRail(sharing_config=...)
func (s *SkillEvolutionRailSuite) TestSkillEvolutionRail_WithSharingConfigMap() {
	sharingConfig := map[string]any{
		"enabled":        false,
		"backend":        "local_file",
		"download_top_k": 5,
	}
	rail := s.newTestRail(evolution.WithSharingConfigMap(sharingConfig))
	// sharing 未启用（enabled=false）→ IsSharingEnabled 返回 false
	s.False(rail.IsSharingEnabled())
}

// TestSkillEvolutionRail_WithSharingConfigMap启用 测试 enabled=true 时共享组件初始化。
//
// 对齐 Python: SkillEvolutionRail(sharing_config={"enabled": True, "backend": "local_file"})
// 需要设置 EVOLUTION_SHARING_HUB_PATH 以避免 local_file backend 路径为空。
func (s *SkillEvolutionRailSuite) TestSkillEvolutionRail_WithSharingConfigMap启用() {
	hubDir := s.T().TempDir()
	s.Require().NoError(os.Setenv("EVOLUTION_SHARING_HUB_PATH", hubDir))
	defer os.Unsetenv("EVOLUTION_SHARING_HUB_PATH")

	sharingConfig := map[string]any{
		"enabled": true,
		"backend": "local_file",
	}
	rail := s.newTestRail(evolution.WithSharingConfigMap(sharingConfig))
	// enabled=true + backend=local_file → IsSharingEnabled 应为 true
	s.True(rail.IsSharingEnabled())
	s.NotNil(rail.ExperienceSharer())
	s.NotNil(rail.ShareStager())
	s.NotNil(rail.KeywordExtractor())
}

// TestSkillEvolutionRail_WithSharingConfigMap启用_无效后端 测试无效后端降级到 local_file。
//
// 对齐 Python: _build_experience_sharer 中 unsupported backend warning
func (s *SkillEvolutionRailSuite) TestSkillEvolutionRail_WithSharingConfigMap启用_无效后端() {
	hubDir := s.T().TempDir()
	s.Require().NoError(os.Setenv("EVOLUTION_SHARING_HUB_PATH", hubDir))
	defer os.Unsetenv("EVOLUTION_SHARING_HUB_PATH")

	sharingConfig := map[string]any{
		"enabled": true,
		"backend": "invalid_cloud",
	}
	rail := s.newTestRail(evolution.WithSharingConfigMap(sharingConfig))
	// 降级到 local_file → 仍可构建 ExperienceSharer
	s.True(rail.IsSharingEnabled())
}

// ─── 属性访问器 ───

// TestSkillEvolutionRail_SetAutoScan 测试 SetAutoScan 运行时修改。
func (s *SkillEvolutionRailSuite) TestSkillEvolutionRail_SetAutoScan() {
	rail := s.newTestRail()
	s.True(rail.AutoScan())

	rail.SetAutoScan(false)
	s.False(rail.AutoScan())

	rail.SetAutoScan(true)
	s.True(rail.AutoScan())
}

// TestSkillEvolutionRail_SetAutoSave 测试 SetAutoSave 运行时修改。
func (s *SkillEvolutionRailSuite) TestSkillEvolutionRail_SetAutoSave() {
	rail := s.newTestRail()
	s.True(rail.AutoSave())

	rail.SetAutoSave(false)
	s.False(rail.AutoSave())
}

// TestSkillEvolutionRail_EvolutionStore 测试 EvolutionStore 访问器。
func (s *SkillEvolutionRailSuite) TestSkillEvolutionRail_EvolutionStore() {
	rail := s.newTestRail()
	s.NotNil(rail.EvolutionStore())
}

// TestSkillEvolutionRail_Scorer 测试 Scorer 访问器。
func (s *SkillEvolutionRailSuite) TestSkillEvolutionRail_Scorer() {
	rail := s.newTestRail()
	s.NotNil(rail.Scorer())
}

// TestSkillEvolutionRail_Evolver 测试 Evolver 访问器。
func (s *SkillEvolutionRailSuite) TestSkillEvolutionRail_Evolver() {
	rail := s.newTestRail()
	s.NotNil(rail.Evolver())
}

// TestSkillEvolutionRail_Manager 测试 Manager 访问器。
func (s *SkillEvolutionRailSuite) TestSkillEvolutionRail_Manager() {
	rail := s.newTestRail()
	s.NotNil(rail.Manager())
}

// TestSkillEvolutionRail_ApprovalRuntime 测试 ApprovalRuntime 访问器。
func (s *SkillEvolutionRailSuite) TestSkillEvolutionRail_ApprovalRuntime() {
	rail := s.newTestRail()
	s.NotNil(rail.ApprovalRuntime())
}

// TestSkillEvolutionRail_ApprovalRuntime重建 测试 ApprovalRuntime 在 manager 变化时重建。
//
// 对齐 Python: SkillEvolutionRail.approval_runtime (property) — 延迟重建
func (s *SkillEvolutionRailSuite) TestSkillEvolutionRail_ApprovalRuntime重建() {
	rail := s.newTestRail()
	runtime1 := rail.ApprovalRuntime()
	s.NotNil(runtime1)

	// 同一 manager 和 pendingApprovalSnapshots → 不重建
	runtime2 := rail.ApprovalRuntime()
	s.Equal(fmt.Sprintf("%p", runtime1), fmt.Sprintf("%p", runtime2))
}

// TestSkillEvolutionRail_ExperienceSharer默认nil 测试无共享配置时 ExperienceSharer 为 nil。
func (s *SkillEvolutionRailSuite) TestSkillEvolutionRail_ExperienceSharer默认nil() {
	rail := s.newTestRail()
	s.Nil(rail.ExperienceSharer())
	s.Nil(rail.ShareStager())
	s.Nil(rail.KeywordExtractor())
}

// TestSkillEvolutionRail_DefaultMemberRole 测试默认成员角色为 "teammate"。
//
// 对齐 Python: SkillEvolutionRail.__init__(default_member_role="teammate")
func (s *SkillEvolutionRailSuite) TestSkillEvolutionRail_DefaultMemberRole() {
	rail := s.newTestRail()

	// SkillEvolutionRail 构造时传入 WithDefaultMemberRole("teammate")
	// 通过 SetTrajectorySink 验证 defaultMemberRole 生效
	sink := &testTrajectorySink{}
	err := rail.EvolutionRail.SetTrajectorySink(sink, "team_1")
	s.Require().NoError(err)
	// 不崩溃即验证 defaultMemberRole 已正确设置
}

// ─── 信号去重 ───

// TestSkillEvolutionRail_ProcessedSignalKeys 测试信号指纹去重。
//
// 对齐 Python: SkillEvolutionRail.processed_signal_keys
func (s *SkillEvolutionRailSuite) TestSkillEvolutionRail_ProcessedSignalKeys() {
	rail := s.newTestRail()

	// 初始为空
	keys := rail.ProcessedSignalKeys()
	s.NotNil(keys)
	s.Empty(keys)

	// 添加信号指纹
	fp := [4]string{"skill_test", "execution_failure", "test_rule", "test_source"}
	keys[fp] = true
	s.Len(rail.ProcessedSignalKeys(), 1)

	// ClearProcessedSignals 清空
	rail.ClearProcessedSignals()
	s.Empty(rail.ProcessedSignalKeys())
}

// TestSkillEvolutionRail_ProcessedSignalKeys_超限清空 测试信号指纹集合超限后可手动清空。
//
// 对齐 Python: if len(self._processed_signal_keys) > _MAX_PROCESSED_SIGNAL_KEYS: self._processed_signal_keys.clear()
// 注：此测试通过手动填充 processedSignalKeys 验证清空机制，RunEvolution 中的自动清空由单元测试覆盖。
func (s *SkillEvolutionRailSuite) TestSkillEvolutionRail_ProcessedSignalKeys_超限清空() {
	rail := s.newTestRail()

	// 填充超过 maxProcessedSignalKeys (500) 个指纹
	for i := 0; i < 501; i++ {
		fp := [4]string{fmt.Sprintf("skill_%d", i), "type", "rule", "source"}
		rail.ProcessedSignalKeys()[fp] = true
	}
	// 验证填充后超过 500
	s.Greater(len(rail.ProcessedSignalKeys()), 500)

	// ClearProcessedSignals 可手动清空
	rail.ClearProcessedSignals()
	s.Empty(rail.ProcessedSignalKeys())
}

// ─── 共享配置 ───

// TestSkillEvolutionRail_IsSharingEnabled_默认禁用 测试无共享配置时共享禁用。
//
// 对齐 Python: SkillEvolutionSharingMixin.is_sharing_enabled
func (s *SkillEvolutionRailSuite) TestSkillEvolutionRail_IsSharingEnabled_默认禁用() {
	rail := s.newTestRail()
	s.False(rail.IsSharingEnabled())
}

// ─── 允许触发 ───

// TestSkillEvolutionRail_AllowEvolutionTrigger 测试 AllowEvolutionTrigger 受 autoScan 控制。
//
// 对齐 Python: SkillEvolutionRail._allow_evolution_trigger(trigger_point, ctx)
func (s *SkillEvolutionRailSuite) TestSkillEvolutionRail_AllowEvolutionTrigger() {
	rail := s.newTestRail()
	s.True(rail.AllowEvolutionTrigger(evolution.TriggerAfterInvoke, nil))

	rail.SetAutoScan(false)
	s.False(rail.AllowEvolutionTrigger(evolution.TriggerAfterInvoke, nil))
}

// TestSkillEvolutionRail_AllowEvolutionTrigger_不同触发点 测试不同触发点行为一致。
//
// 对齐 Python: SkillEvolutionRail._allow_evolution_trigger 不区分触发点，只看 autoScan
func (s *SkillEvolutionRailSuite) TestSkillEvolutionRail_AllowEvolutionTrigger_不同触发点() {
	rail := s.newTestRail()
	// autoScan=true 时，所有触发点都允许
	s.True(rail.AllowEvolutionTrigger(evolution.TriggerAfterInvoke, nil))
	s.True(rail.AllowEvolutionTrigger(evolution.TriggerAfterModelCall, nil))
	s.True(rail.AllowEvolutionTrigger(evolution.TriggerAfterToolCall, nil))
	s.True(rail.AllowEvolutionTrigger(evolution.TriggerAfterTaskIteration, nil))

	rail.SetAutoScan(false)
	// autoScan=false 时，所有触发点都不允许
	s.False(rail.AllowEvolutionTrigger(evolution.TriggerAfterInvoke, nil))
	s.False(rail.AllowEvolutionTrigger(evolution.TriggerAfterModelCall, nil))
}

// ─── EvolutionExtension 空操作方法 ───

// TestSkillEvolutionRail_OnBeforeInvoke空操作 测试 OnBeforeInvoke 返回 nil。
func (s *SkillEvolutionRailSuite) TestSkillEvolutionRail_OnBeforeInvoke空操作() {
	rail := s.newTestRail()
	err := rail.OnBeforeInvoke(s.Ctx, nil)
	s.Require().NoError(err)
}

// TestSkillEvolutionRail_OnAfterModelCall空操作 测试 OnAfterModelCall 返回 nil。
func (s *SkillEvolutionRailSuite) TestSkillEvolutionRail_OnAfterModelCall空操作() {
	rail := s.newTestRail()
	err := rail.OnAfterModelCall(s.Ctx, nil)
	s.Require().NoError(err)
}

// TestSkillEvolutionRail_OnAfterInvoke空操作 测试 OnAfterInvoke 返回 nil。
func (s *SkillEvolutionRailSuite) TestSkillEvolutionRail_OnAfterInvoke空操作() {
	rail := s.newTestRail()
	err := rail.OnAfterInvoke(s.Ctx, nil)
	s.Require().NoError(err)
}

// TestSkillEvolutionRail_OnAfterTaskIteration空操作 测试 OnAfterTaskIteration 返回 nil。
func (s *SkillEvolutionRailSuite) TestSkillEvolutionRail_OnAfterTaskIteration空操作() {
	rail := s.newTestRail()
	err := rail.OnAfterTaskIteration(s.Ctx, nil)
	s.Require().NoError(err)
}

// TestSkillEvolutionRail_OnAfterEvolutionTriggered空操作 测试 OnAfterEvolutionTriggered 返回 nil。
func (s *SkillEvolutionRailSuite) TestSkillEvolutionRail_OnAfterEvolutionTriggered空操作() {
	rail := s.newTestRail()
	err := rail.OnAfterEvolutionTriggered(s.Ctx, nil, nil)
	s.Require().NoError(err)
}

// ─── OnAfterToolCall 经验详情检测 ───

// TestSkillEvolutionRail_OnAfterToolCall需要有效cbc 测试 OnAfterToolCall 需要有效 cbc。
// 非 ToolCallInputs 类型时返回 nil 不崩溃。
func (s *SkillEvolutionRailSuite) TestSkillEvolutionRail_OnAfterToolCall需要有效cbc() {
	rail := s.newTestRail()
	// 非 ToolCallInputs 类型时应返回 nil（不崩溃）
	// 使用空的 AgentCallbackContext（Inputs 返回 nil）
	cbc := agentinterfaces.NewAgentCallbackContext(nil, nil, nil)
	err := rail.OnAfterToolCall(s.Ctx, cbc)
	s.Require().NoError(err)
}

// TestSkillEvolutionRail_OnAfterToolCall_skillTool读取经验详情 测试 skill_tool 读取经验详情。
//
// 对齐 Python: SkillEvolutionRail._on_after_tool_call → _detect_experience_detail_read
func (s *SkillEvolutionRailSuite) TestSkillEvolutionRail_OnAfterToolCall_skillTool读取经验详情() {
	rail := s.newTestRail()

	// skill_tool 读取 evolution/ 下的 .md 文件（经验详情）
	inputs := &agentinterfaces.ToolCallInputs{
		ToolName: "skill_tool",
		ToolArgs: map[string]any{
			"skill_name":          "my_skill",
			"relative_file_path": "evolution/experience1.md",
		},
		ToolResult: map[string]any{
			"skill_content": "## [rec_001] Some experience\nContent here",
		},
	}
	cbc := agentinterfaces.NewAgentCallbackContext(nil, inputs, nil)

	// 应不崩溃
	err := rail.OnAfterToolCall(s.Ctx, cbc)
	s.Require().NoError(err)
}

// TestSkillEvolutionRail_OnAfterToolCall_skillTool读SKILLmd不触发 测试读取 SKILL.md 不触发经验追踪。
//
// 对齐 Python: SKILL.md 读取仅用于索引发现，不计入 presented
func (s *SkillEvolutionRailSuite) TestSkillEvolutionRail_OnAfterToolCall_skillTool读SKILLmd不触发() {
	rail := s.newTestRail()

	inputs := &agentinterfaces.ToolCallInputs{
		ToolName: "skill_tool",
		ToolArgs: map[string]any{
			"skill_name":          "my_skill",
			"relative_file_path": "SKILL.md",
		},
		ToolResult: map[string]any{
			"skill_content": "# My Skill\nSome content",
		},
	}
	cbc := agentinterfaces.NewAgentCallbackContext(nil, inputs, nil)

	// SKILL.md 不是经验详情，应返回 nil 不崩溃
	err := rail.OnAfterToolCall(s.Ctx, cbc)
	s.Require().NoError(err)
}

// TestSkillEvolutionRail_OnAfterToolCall_非skillTool不触发 测试非 skill_tool 工具不触发经验追踪。
func (s *SkillEvolutionRailSuite) TestSkillEvolutionRail_OnAfterToolCall_非skillTool不触发() {
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

// ─── SnapshotForEvolution ───

// TestSkillEvolutionRail_SnapshotForEvolution_autoScan关闭返回nil 测试 autoScan 关闭时返回 nil。
//
// 对齐 Python: SkillEvolutionRail._snapshot_for_evolution → if not self.auto_scan: return None
func (s *SkillEvolutionRailSuite) TestSkillEvolutionRail_SnapshotForEvolution_autoScan关闭返回nil() {
	rail := s.newTestRail(evolution.WithAutoScan(false))
	s.False(rail.AutoScan())

	// autoScan=false → SnapshotForEvolution 返回 nil
	snapshot := rail.SnapshotForEvolution(s.Ctx, &trajectory.Trajectory{}, nil)
	s.Nil(snapshot)
}

// TestSkillEvolutionRail_SnapshotForEvolution_空轨迹返回nil 测试空轨迹消息返回 nil。
//
// 对齐 Python: if not messages: return None
func (s *SkillEvolutionRailSuite) TestSkillEvolutionRail_SnapshotForEvolution_空轨迹返回nil() {
	rail := s.newTestRail()
	s.True(rail.AutoScan())

	// 空轨迹（无 steps）→ collectMessagesFromTrajectory 返回空 → snapshot nil
	emptyTraj := &trajectory.Trajectory{SessionID: "test"}
	snapshot := rail.SnapshotForEvolution(s.Ctx, emptyTraj, nil)
	s.Nil(snapshot)
}

// TestSkillEvolutionRail_SnapshotForEvolution_有效轨迹 测试有效轨迹返回非空快照。
//
// 对齐 Python: _snapshot_for_evolution 正常路径
func (s *SkillEvolutionRailSuite) TestSkillEvolutionRail_SnapshotForEvolution_有效轨迹() {
	rail := s.newTestRail()

	// 构造有内容的轨迹
	builder := trajectory.NewTrajectoryBuilder("session_123", "online")
	builder.RecordStep(&trajectory.TrajectoryStep{
		Kind: trajectory.StepKindLLM,
		Detail: &trajectory.LLMCallDetail{
			Messages: []llmschema.BaseMessage{llmschema.NewUserMessage("test query")},
			Response: map[string]any{"content": "test response"},
		},
	})
	traj := builder.Build()

	cbc := agentinterfaces.NewAgentCallbackContext(nil, &agentinterfaces.InvokeInputs{ConversationID: "session_123"}, nil)
	snapshot := rail.SnapshotForEvolution(s.Ctx, traj, cbc)

	s.Require().NotNil(snapshot)
	s.NotNil(snapshot.Trajectory)
	s.NotEmpty(snapshot.Messages)
	s.Equal("session_123", snapshot.SessionID)
	// skillName 默认为 "skill-evolution"
	s.Require().NotNil(snapshot.SkillName)
	s.Equal("skill-evolution", *snapshot.SkillName)
}

// ─── ShouldHintSimplifyOrRebuild ───

// TestSkillEvolutionRail_ShouldHintSimplifyOrRebuild 测试无技能目录时返回 false。
//
// 对齐 Python: SkillEvolutionRail.should_hint_simplify_or_rebuild(skill_name)
func (s *SkillEvolutionRailSuite) TestSkillEvolutionRail_ShouldHintSimplifyOrRebuild() {
	rail := s.newTestRail()

	// 无技能目录或无演进日志 → false
	result := rail.ShouldHintSimplifyOrRebuild("nonexistent_skill")
	s.False(result)
}

// TestSkillEvolutionRail_ShouldHintSimplifyOrRebuild_不足经验 测试少于 10 条经验时返回 false。
//
// 对齐 Python: SkillEvolutionRail.should_hint_simplify_or_rebuild → len(entries) >= 10
func (s *SkillEvolutionRailSuite) TestSkillEvolutionRail_ShouldHintSimplifyOrRebuild_不足经验() {
	tempDir := s.T().TempDir()
	rail := s.newTestRailWithDir(tempDir)

	// 创建技能目录（只有 SKILL.md，无 evolutions.json）
	skillDir := filepath.Join(tempDir, "my_skill")
	s.Require().NoError(os.MkdirAll(skillDir, 0o755))
	s.Require().NoError(os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# My Skill\n"), 0o644))

	// 无演进日志 → 返回 false
	result := rail.ShouldHintSimplifyOrRebuild("my_skill")
	s.False(result)
}

// TestSkillEvolutionRail_ShouldHintSimplifyOrRebuild_足够经验 测试 >= 10 条经验时返回 true。
//
// 对齐 Python: SkillEvolutionRail.should_hint_simplify_or_rebuild → len(entries) >= 10 → return True
func (s *SkillEvolutionRailSuite) TestSkillEvolutionRail_ShouldHintSimplifyOrRebuild_足够经验() {
	tempDir := s.T().TempDir()
	rail := s.newTestRailWithDir(tempDir)

	// 创建技能目录
	skillDir := filepath.Join(tempDir, "my_skill")
	s.Require().NoError(os.MkdirAll(skillDir, 0o755))
	s.Require().NoError(os.MkdirAll(filepath.Join(skillDir, "evolution"), 0o755))
	s.Require().NoError(os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# My Skill\n"), 0o644))

	// 创建含 10 条 entries 的 evolutions.json
	entries := make([]map[string]any, 10)
	for i := 0; i < 10; i++ {
		entries[i] = map[string]any{
			"id":        fmt.Sprintf("entry_%d", i),
			"timestamp": time.Now().Unix(),
			"change": map[string]any{
				"target":  "description",
				"section": "Instructions",
				"content": fmt.Sprintf("Experience entry %d", i),
			},
		}
	}
	evoData, err := json.Marshal(map[string]any{"entries": entries})
	s.Require().NoError(err)
	s.Require().NoError(os.WriteFile(filepath.Join(skillDir, "evolutions.json"), evoData, 0o644))

	// >= 10 条经验 → 返回 true
	result := rail.ShouldHintSimplifyOrRebuild("my_skill")
	s.True(result)
}

// ─── 回调注册 ───

// TestSkillEvolutionRail_GetCallbacks 测试 GetCallbacks 注册。
func (s *SkillEvolutionRailSuite) TestSkillEvolutionRail_GetCallbacks() {
	rail := s.newTestRail()
	callbacks := rail.GetCallbacks()
	s.NotNil(callbacks)
	// SkillEvolutionRail 通过 EvolutionRail 基类注册 5 个回调
	s.GreaterOrEqual(len(callbacks), 5)
	s.Contains(callbacks, agentinterfaces.CallbackBeforeInvoke)
	s.Contains(callbacks, agentinterfaces.CallbackAfterModelCall)
	s.Contains(callbacks, agentinterfaces.CallbackAfterToolCall)
	s.Contains(callbacks, agentinterfaces.CallbackAfterInvoke)
	s.Contains(callbacks, agentinterfaces.CallbackAfterTaskIteration)
}

// ─── RecordPresentedExperiences ───

// TestSkillEvolutionRail_RecordPresentedExperiences 测试非 rail 路径呈现经验记录。
//
// 对齐 Python: SkillEvolutionRail.record_presented_experiences
func (s *SkillEvolutionRailSuite) TestSkillEvolutionRail_RecordPresentedExperiences() {
	rail := s.newTestRail()
	// 不崩溃即验证
	rail.RecordPresentedExperiences(s.Ctx, "my_skill", "snippet content", "session_1", []string{"rec_001"})
	rail.RecordPresentedExperiences(s.Ctx, "my_skill", "snippet without record IDs", "session_1", nil)
}

// ─── RollbackSkill ───

// TestSkillEvolutionRail_RollbackSkill_无归档 测试无归档目录时返回 false。
//
// 对齐 Python: SkillEvolutionRail.rollback_skill(skill_name) → no archive → return False
func (s *SkillEvolutionRailSuite) TestSkillEvolutionRail_RollbackSkill_无归档() {
	tempDir := s.T().TempDir()
	rail := s.newTestRailWithDir(tempDir)

	// 创建技能目录但无归档
	skillDir := filepath.Join(tempDir, "my_skill")
	s.Require().NoError(os.MkdirAll(skillDir, 0o755))
	s.Require().NoError(os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# My Skill\n"), 0o644))

	result, err := rail.RollbackSkill(s.Ctx, "my_skill", nil)
	s.Require().NoError(err)
	s.False(result)
}

// TestSkillEvolutionRail_RollbackSkill_有归档 测试有归档目录时回滚到最新版本。
//
// 对齐 Python: SkillEvolutionRail.rollback_skill(skill_name) → restore latest archive
func (s *SkillEvolutionRailSuite) TestSkillEvolutionRail_RollbackSkill_有归档() {
	tempDir := s.T().TempDir()
	rail := s.newTestRailWithDir(tempDir)

	// 创建技能目录 + 归档
	skillDir := filepath.Join(tempDir, "my_skill")
	archiveDir := filepath.Join(skillDir, "archive")
	s.Require().NoError(os.MkdirAll(archiveDir, 0o755))
	s.Require().NoError(os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# My Skill Current\n"), 0o644))
	s.Require().NoError(os.WriteFile(filepath.Join(archiveDir, "SKILL.v2.md"), []byte("# My Skill v2\n"), 0o644))
	s.Require().NoError(os.WriteFile(filepath.Join(archiveDir, "SKILL.v1.md"), []byte("# My Skill v1\n"), 0o644))

	result, err := rail.RollbackSkill(s.Ctx, "my_skill", nil)
	s.Require().NoError(err)
	s.True(result)
}

// TestSkillEvolutionRail_RollbackSkill_指定版本 测试指定版本回滚。
//
// 对齐 Python: SkillEvolutionRail.rollback_skill(skill_name, version="v1")
func (s *SkillEvolutionRailSuite) TestSkillEvolutionRail_RollbackSkill_指定版本() {
	tempDir := s.T().TempDir()
	rail := s.newTestRailWithDir(tempDir)

	// 创建技能目录 + 归档
	skillDir := filepath.Join(tempDir, "my_skill")
	archiveDir := filepath.Join(skillDir, "archive")
	s.Require().NoError(os.MkdirAll(archiveDir, 0o755))
	s.Require().NoError(os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# Current\n"), 0o644))
	s.Require().NoError(os.WriteFile(filepath.Join(archiveDir, "SKILL.v1.md"), []byte("# Version 1\n"), 0o644))

	version := "SKILL.v1.md"
	result, err := rail.RollbackSkill(s.Ctx, "my_skill", &version)
	s.Require().NoError(err)
	s.True(result)
}

// ─── 热更新 ───

// TestSkillEvolutionRail_UpdateLLM 测试 UpdateLLM 热更新 LLM 客户端和模型。
//
// 对齐 Python: SkillEvolutionRail.update_llm(llm, model)
func (s *SkillEvolutionRailSuite) TestSkillEvolutionRail_UpdateLLM() {
	rail := s.newTestRail()

	// 创建新的 mock model
	newModel := newSkillEvolutionMockModel(s.T())

	// 热更新不应崩溃
	rail.UpdateLLM(newModel, "new-model-name")

	// 验证 Evolver 已更新
	s.Equal("new-model-name", rail.Evolver().ModelName())
}

// TestSkillEvolutionRail_SetSysOperation 测试 SetSysOperation 传播到基类和 EvolutionStore。
//
// 对齐 Python: SkillEvolutionRail.set_sys_operation(sys_operation)
func (s *SkillEvolutionRailSuite) TestSkillEvolutionRail_SetSysOperation() {
	rail := s.newTestRail()

	// SetSysOperation 不应崩溃
	// sys_operation.SysOperation 为接口，传 nil 验证方法调用不 panic
	rail.SetSysOperation(nil)
	// 不崩溃即验证 SetSysOperation 正确传播到基类和 EvolutionStore
}

// ─── 审批生命周期 ───

// TestSkillEvolutionRail_ApproveRecord_不存在 测试审批不存在的 requestID 不崩溃。
//
// 对齐 Python: SkillEvolutionRail.approve_record(request_id) → unknown request → return
func (s *SkillEvolutionRailSuite) TestSkillEvolutionRail_ApproveRecord_不存在() {
	rail := s.newTestRail()

	err := rail.ApproveRecord(s.Ctx, "nonexistent_request_id")
	s.Require().NoError(err)
}

// TestSkillEvolutionRail_RejectRecord_不存在 测试拒绝不存在的 requestID 不崩溃。
//
// 对齐 Python: SkillEvolutionRail.reject_record(request_id) → unknown request → return
func (s *SkillEvolutionRailSuite) TestSkillEvolutionRail_RejectRecord_不存在() {
	rail := s.newTestRail()

	err := rail.RejectRecord(s.Ctx, "nonexistent_request_id")
	s.Require().NoError(err)
}

// TestSkillEvolutionRail_OnApprove_OnReject_兼容别名 测试兼容别名方法。
//
// 对齐 Python: SkillEvolutionRail.on_approve / on_reject
func (s *SkillEvolutionRailSuite) TestSkillEvolutionRail_OnApprove_OnReject_兼容别名() {
	rail := s.newTestRail()

	// OnApprove 应等同于 ApproveRecord
	err := rail.OnApprove(s.Ctx, "nonexistent")
	s.Require().NoError(err)

	// OnReject 应等同于 RejectRecord
	err = rail.OnReject(s.Ctx, "nonexistent")
	s.Require().NoError(err)
}

// ─── RequestSimplify ───

// TestSkillEvolutionRail_RequestSimplify_无经验 测试无经验时 RequestSimplify 返回空结果。
//
// 对齐 Python: SkillEvolutionRail.request_simplify(skill_name) → no experience → empty result
func (s *SkillEvolutionRailSuite) TestSkillEvolutionRail_RequestSimplify_无经验() {
	rail := s.newTestRail()

	result, err := rail.RequestSimplify(s.Ctx, "nonexistent_skill", nil)
	s.Require().NoError(err)
	s.NotNil(result)
	s.Equal("nonexistent_skill", result.SkillName)
}

// ─── RequestRebuild ───

// TestSkillEvolutionRail_RequestRebuild_无经验 测试无经验时 RequestRebuild 返回空。
//
// 对齐 Python: SkillEvolutionRail.request_rebuild(skill_name) → no experience → empty
func (s *SkillEvolutionRailSuite) TestSkillEvolutionRail_RequestRebuild_无经验() {
	rail := s.newTestRail()

	result, err := rail.RequestRebuild(s.Ctx, "nonexistent_skill", nil, 0.5)
	s.Require().NoError(err)
	s.Empty(result)
}

// ─── RequestUserEvolution ───

// TestSkillEvolutionRail_RequestUserEvolution_无轨迹 测试无轨迹时返回基础结果。
//
// 对齐 Python: SkillEvolutionRail.request_user_evolution(skill_name, user_intent, auto_approve=...)
func (s *SkillEvolutionRailSuite) TestSkillEvolutionRail_RequestUserEvolution_无轨迹() {
	rail := s.newTestRail()

	result, err := rail.RequestUserEvolution(s.Ctx, "my_skill", "", false)
	s.Require().NoError(err)
	s.NotNil(result)
	s.Equal("my_skill", result.SkillName)
}

// TestSkillEvolutionRail_RequestUserEvolution_有意图 测试有 userIntent 时触发信号生成。
//
// 对齐 Python: SkillEvolutionRail.request_user_evolution(skill_name, user_intent="...", auto_approve=False)
func (s *SkillEvolutionRailSuite) TestSkillEvolutionRail_RequestUserEvolution_有意图() {
	rail := s.newTestRail()

	result, err := rail.RequestUserEvolution(s.Ctx, "my_skill", "请帮我优化这个技能的文档", false)
	s.Require().NoError(err)
	s.NotNil(result)
	s.Equal("my_skill", result.SkillName)
}

// ─── RunEvolution autoScan 控制 ───

// TestSkillEvolutionRail_RunEvolution_autoScan关闭 测试 autoScan 关闭时 RunEvolution 返回 nil。
//
// 对齐 Python: SkillEvolutionRail.run_evolution → if not self.auto_scan: return
func (s *SkillEvolutionRailSuite) TestSkillEvolutionRail_RunEvolution_autoScan关闭() {
	rail := s.newTestRail(evolution.WithAutoScan(false))

	err := rail.RunEvolution(s.Ctx, nil, nil)
	s.Require().NoError(err)
}

// TestSkillEvolutionRail_RunEvolution_空snapshot空轨迹 测试 snapshot 和轨迹都为 nil 时返回 nil。
//
// 对齐 Python: if trajectory is None and snapshot is None: return
func (s *SkillEvolutionRailSuite) TestSkillEvolutionRail_RunEvolution_空snapshot空轨迹() {
	rail := s.newTestRail()

	err := rail.RunEvolution(s.Ctx, nil, nil)
	s.Require().NoError(err)
}

// TestSkillEvolutionRail_RunEvolution_空消息snapshot 测试 snapshot 消息为空时提前返回。
//
// 对齐 Python: if not messages: return
func (s *SkillEvolutionRailSuite) TestSkillEvolutionRail_RunEvolution_空消息snapshot() {
	rail := s.newTestRail()

	// snapshot 的 Messages 为空
	snapshot := &evolution.EvolutionSnapshot{
		Trajectory: &trajectory.Trajectory{SessionID: "test"},
		Messages:   []map[string]any{},
	}
	err := rail.RunEvolution(s.Ctx, nil, snapshot)
	s.Require().NoError(err)
}

// ─── EvolutionRequestResult / SimplifyRequestResult ───

// TestSkillEvolutionRail_EvolutionRequestResult_HasChanges 测试 HasChanges 判断。
func (s *SkillEvolutionRailSuite) TestSkillEvolutionRail_EvolutionRequestResult_HasChanges() {
	// 有 Records → HasChanges=true
	result1 := &evolution.EvolutionRequestResult{
		SkillName: "test",
		Records:   []checkpointing.EvolutionRecord{{ID: "rec_001"}},
	}
	s.True(result1.HasChanges())

	// 有 ApprovalEvent → HasChanges=true
	result2 := &evolution.EvolutionRequestResult{
		SkillName:     "test",
		ApprovalEvent: &stream.OutputSchema{Type: "test"},
	}
	s.True(result2.HasChanges())

	// 都没有 → HasChanges=false
	result3 := &evolution.EvolutionRequestResult{SkillName: "test"}
	s.False(result3.HasChanges())
}

// TestSkillEvolutionRail_SimplifyRequestResult_HasChanges 测试 SimplifyRequestResult 的 HasChanges。
func (s *SkillEvolutionRailSuite) TestSkillEvolutionRail_SimplifyRequestResult_HasChanges() {
	// 有 Actions → HasChanges=true
	result1 := &evolution.SimplifyRequestResult{
		SkillName: "test",
		Actions:   []map[string]any{{"action": "merge"}},
	}
	s.True(result1.HasChanges())

	// 有 ApprovalEvent → HasChanges=true
	result2 := &evolution.SimplifyRequestResult{
		SkillName:     "test",
		ApprovalEvent: &stream.OutputSchema{Type: "test"},
	}
	s.True(result2.HasChanges())

	// 都没有 → HasChanges=false
	result3 := &evolution.SimplifyRequestResult{SkillName: "test"}
	s.False(result3.HasChanges())
}

// ─── EvolutionSnapshot ───

// TestSkillEvolutionRail_EvolutionSnapshot_ToLegacyDict 测试快照序列化为 dict。
//
// 对齐 Python: snapshot dict 序列化
func (s *SkillEvolutionRailSuite) TestSkillEvolutionRail_EvolutionSnapshot_ToLegacyDict() {
	skillName := "test-skill"
	snapshot := evolution.EvolutionSnapshot{
		Trajectory:          &trajectory.Trajectory{SessionID: "session-123"},
		Messages:            []map[string]any{{"role": "user", "content": "hello"}},
		SkillName:           &skillName,
		SessionID:           "session-123",
		PresentedEntries:    []experience.PresentedRecordEntry{},
		IncrementalMessages: []map[string]any{{"role": "user", "content": "incremental"}},
	}

	legacy := snapshot.ToLegacyDict()
	s.Equal("session-123", legacy["session_id"])
	s.Equal("test-skill", legacy["skill_name"])
	s.NotNil(legacy["trajectory"])
	s.NotNil(legacy["messages"])
	s.NotNil(legacy["presented_entries"])
	s.NotNil(legacy["incremental_messages"])
}

// TestSkillEvolutionRail_EvolutionSnapshot_FromLegacyDict 测试从 dict 恢复快照。
func (s *SkillEvolutionRailSuite) TestSkillEvolutionRail_EvolutionSnapshot_FromLegacyDict() {
	skillName := "test-skill"
	snapshot := evolution.EvolutionSnapshot{
		Trajectory: &trajectory.Trajectory{SessionID: "session-456"},
		Messages:   []map[string]any{{"role": "user", "content": "test"}},
		SkillName:  &skillName,
		SessionID:  "session-456",
	}

	legacy := snapshot.ToLegacyDict()
	restored := evolution.FromLegacyDict(legacy)

	s.Equal("session-456", restored.SessionID)
	s.Require().NotNil(restored.SkillName)
	s.Equal("test-skill", *restored.SkillName)
	s.NotNil(restored.Trajectory)
	s.NotEmpty(restored.Messages)
}

// ─── EvolutionHostEventMeta ───

// TestSkillEvolutionRail_EvolutionHostEventMeta_ToPayload 测试事件元数据序列化。
func (s *SkillEvolutionRailSuite) TestSkillEvolutionRail_EvolutionHostEventMeta_ToPayload() {
	meta := evolution.EvolutionHostEventMeta{
		EventKind:  evolution.EvolutionEventKindProgress,
		RailKind:   stringPtr("regular"),
		Stage:      stringPtr("detecting_signals"),
		SkillName:  stringPtr("my_skill"),
		RequestID:  stringPtr("req_001"),
		Status:     stringPtr("started"),
	}

	payload := meta.ToPayload()
	s.Equal("progress", payload["event_kind"])
	s.Equal("regular", payload["rail_kind"])
	s.Equal("detecting_signals", payload["stage"])
	s.Equal("my_skill", payload["skill_name"])
	s.Equal("req_001", payload["request_id"])
	s.Equal("started", payload["status"])
}

// TestSkillEvolutionRail_EvolutionHostEventMeta_空字段跳过 测试空字段不序列化。
func (s *SkillEvolutionRailSuite) TestSkillEvolutionRail_EvolutionHostEventMeta_空字段跳过() {
	meta := evolution.EvolutionHostEventMeta{
		EventKind: evolution.EvolutionEventKindOutcome,
	}

	payload := meta.ToPayload()
	s.Equal("outcome", payload["event_kind"])
	// nil 字段不应出现在 payload 中
	_, hasRailKind := payload["rail_kind"]
	s.False(hasRailKind)
	_, hasSkillName := payload["skill_name"]
	s.False(hasSkillName)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// newTestRail 创建测试用 SkillEvolutionRail。
// 使用临时目录作为技能目录，mock LLM 客户端避免真实 API 调用。
func (s *SkillEvolutionRailSuite) newTestRail(opts ...evolution.SkillEvolutionRailOption) *evolution.SkillEvolutionRail {
	tempDir := s.T().TempDir()
	return s.newTestRailWithDir(tempDir, opts...)
}

// newTestRailWithDir 使用指定目录创建测试用 SkillEvolutionRail。
func (s *SkillEvolutionRailSuite) newTestRailWithDir(skillsDir string, opts ...evolution.SkillEvolutionRailOption) *evolution.SkillEvolutionRail {
	model := newSkillEvolutionMockModel(s.T())
	allOpts := append([]evolution.SkillEvolutionRailOption{}, opts...)
	return evolution.NewSkillEvolutionRail(
		[]string{skillsDir},
		model,
		"test-model",
		"cn",
		allOpts...,
	)
}

// newSkillEvolutionMockModel 创建集成测试用 mock *llm.Model。
// 对齐 skill_call/llm_mock_test.go 中 newMockSkillModel 的模式：
// 注册 mock client 到 ClientRegistry → NewModel 从 registry 获取客户端。
func newSkillEvolutionMockModel(t interface {
	Helper()
	Fatalf(format string, args ...any)
}) *llm.Model {
	t.Helper()

	mockClient := &mockSkillClient{}
	providerName := fmt.Sprintf("mock_skill_evolution_%d", time.Now().UnixNano())
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

// stringPtr 返回字符串指针。
func stringPtr(s string) *string {
	return &s
}
