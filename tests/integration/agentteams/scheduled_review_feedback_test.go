//go:build integration

// 25.2b 通道 B 全链集成（spec §7 Task 13 Step 3）。
// HandleReviewFeedback → 归因（真实 rail LLM 面 + mock 客户端）→ 入账 →
// Finalize → EvolveFromExternalSignals → 演进存储落盘 / 降级三条。
// 真实跨子系统：scheduling 协调器 + agentcore rails evolution + evolving signal
// + evolving checkpointing 存储。

package agentteams_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/agent/coordination/scheduling"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/rails/evolution"
)

// ReviewFeedbackChainSuite 通道 B 全链套件。
type ReviewFeedbackChainSuite struct {
	suite.Suite
	skillsDir string
}

// TestReviewFeedbackChainSuite 入口。
func TestReviewFeedbackChainSuite(t *testing.T) {
	suite.Run(t, new(ReviewFeedbackChainSuite))
}

// newChainMockModel 可编程响应的 mock 模型（按调用序出队响应）。
func (s *ReviewFeedbackChainSuite) newChainMockModel(responses []string) *llm.Model {
	var cursor atomic.Int64
	mockClient := &reviewFeedbackMockClient{
		invokeFn: func(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
			idx := int(cursor.Add(1)) - 1
			if idx < len(responses) {
				return llmschema.NewAssistantMessage(responses[idx]), nil
			}
			return llmschema.NewAssistantMessage("{}"), nil
		},
	}
	providerName := fmt.Sprintf("mock_review_feedback_%d", time.Now().UnixNano())
	model_clients.GetClientRegistry().Register(providerName, "llm", func(_ *llmschema.ModelRequestConfig, _ *llmschema.ModelClientConfig) (model_clients.BaseModelClient, error) {
		return mockClient, nil
	})
	model, err := llm.NewModel(&llmschema.ModelClientConfig{
		ClientProvider: providerName, ClientID: providerName + "_id",
	}, &llmschema.ModelRequestConfig{ModelName: "test-model"})
	s.Require().NoError(err)
	return model
}

// newChainRail 建 rail（skillsDir 内预置 data-processor 技能）+ 捕获一次
// SKILL.md 读取 span（证据面）。
func (s *ReviewFeedbackChainSuite) newChainRail(model *llm.Model) *evolution.TeamSkillEvolutionRail {
	s.skillsDir = s.T().TempDir()
	skillDir := filepath.Join(s.skillsDir, "data-processor")
	s.Require().NoError(os.MkdirAll(skillDir, 0o755))
	s.Require().NoError(os.WriteFile(filepath.Join(skillDir, "SKILL.md"),
		[]byte("# data-processor\n\n数据处理技能。"), 0o644))

	// teamID 选项使捕获落 team scope（RailGetTrajectory 的读取键）。
	rail := evolution.NewTeamSkillEvolutionRail([]string{s.skillsDir}, model, "test-model", "cn",
		evolution.WithTeamSkillTeamID("alpha"))
	// 捕获一次读取 SKILL.md 的工具 span（轨迹读取证据）。
	cbc := &agentinterfaces.AgentCallbackContext{}
	cbc.SetInputs(&agentinterfaces.InvokeInputs{ConversationID: "sess-chain"})
	s.Require().NoError(rail.BeforeInvoke(context.Background(), cbc))
	tcbc := &agentinterfaces.AgentCallbackContext{}
	tcbc.SetInputs(&agentinterfaces.ToolCallInputs{
		ToolName: "read_file",
		ToolArgs: map[string]any{"path": filepath.Join(skillDir, "SKILL.md")},
	})
	s.Require().NoError(rail.AfterToolCall(context.Background(), tcbc))
	return rail
}

// reviewPayload 评审反馈负载（带任务目标上下文）。
func reviewPayload(taskID, feedback string) map[string]any {
	return map[string]any{
		"task_id": taskID, "review_round": int64(1), "feedback": feedback,
		"assignee": "agent1", "task_title": "实现导出", "task_content": "导出功能实现",
	}
}

// TestReviewFeedbackChain_skill_issue全链 skill_issue 归因（真实读取证据）→
// 入账 → Finalize → EvolveFromExternalSignals。
func (s *ReviewFeedbackChainSuite) TestReviewFeedbackChain_skill_issue全链() {
	attributionJSON := `{"classification":"skill_issue","skill_name":"data-processor","target":"body","reason":"Skill 缺并发指导","reusable_guidance":"补充并发安全说明","is_reusable":true,"confidence":0.9}`
	rail := s.newChainRail(s.newChainMockModel([]string{attributionJSON}))
	coordinator := scheduling.NewReviewFeedbackEvolutionCoordinator("sess-chain", "alpha",
		func() scheduling.TeamEvolutionRailLike { return rail }, true, 0.7)
	rail.SetupReviewFeedbackEvolution("sess-chain", "alpha", 0.7, coordinator)

	s.Require().True(rail.ReviewFeedbackEvolutionEnabled(), "rail must accept scheduler feedback")

	// HandleReviewFeedback：证据齐 → EVOLVE_EXISTING_SKILL → 观察保留。
	s.Require().NoError(rail.HandleReviewFeedback(context.Background(), reviewPayload("task-b1", "并发不安全")))

	// Finalize：聚合 → evolve_team_groups → EvolveFromExternalSignals。
	attempted, err := rail.FinalizeReviewFeedback(context.Background(), nil)
	s.Require().NoError(err)
	s.Require().True(attempted, "retained observation must attempt evolution")

	// 二次 Finalize：cursor 已推进，无新观察 → 不重复。
	attempted2, err := rail.FinalizeReviewFeedback(context.Background(), nil)
	s.Require().NoError(err)
	s.Require().False(attempted2, "cursor advanced; no repeated aggregation")
}

// TestReviewFeedbackChain_降级无证据 skill_issue 但无读取证据 → 封闭（降级①）。
func (s *ReviewFeedbackChainSuite) TestReviewFeedbackChain_降级无证据() {
	attributionJSON := `{"classification":"skill_issue","skill_name":"data-processor","target":"body","reusable_guidance":"x","is_reusable":true,"confidence":0.95}`
	rail := s.newChainRail(s.newChainMockModel([]string{attributionJSON}))
	coordinator := scheduling.NewReviewFeedbackEvolutionCoordinator("sess-noev", "alpha",
		func() scheduling.TeamEvolutionRailLike { return rail }, true, 0.7)
	rail.SetupReviewFeedbackEvolution("sess-noev", "alpha", 0.7, coordinator)

	s.Require().NoError(rail.HandleReviewFeedback(context.Background(), reviewPayload("task-d1", "并发不安全")))
	// 无轨迹读取证据（会话窗口无 SKILL.md 读取）→ 封闭，无观察入账。
	attempted, err := rail.FinalizeReviewFeedback(context.Background(), nil)
	s.Require().NoError(err)
	s.Require().False(attempted, "closed attribution must not attempt evolution")
}

// TestReviewFeedbackChain_降级未归因 unattributed → SKIP（降级②）。
func (s *ReviewFeedbackChainSuite) TestReviewFeedbackChain_降级未归因() {
	attributionJSON := `{"classification":"unattributed","reason":"无法归因"}`
	rail := s.newChainRail(s.newChainMockModel([]string{attributionJSON}))
	coordinator := scheduling.NewReviewFeedbackEvolutionCoordinator("sess-unattr", "alpha",
		func() scheduling.TeamEvolutionRailLike { return rail }, true, 0.7)
	rail.SetupReviewFeedbackEvolution("sess-unattr", "alpha", 0.7, coordinator)

	s.Require().NoError(rail.HandleReviewFeedback(context.Background(), reviewPayload("task-d2", "随机失败")))
	attempted, err := rail.FinalizeReviewFeedback(context.Background(), nil)
	s.Require().NoError(err)
	s.Require().False(attempted, "unattributed must not attempt evolution")
}

// TestReviewFeedbackChain_降级置信度不足 EVOLVE 归因但 confidence < 0.7 → 不入账
// （降级③）。
func (s *ReviewFeedbackChainSuite) TestReviewFeedbackChain_降级置信度不足() {
	attributionJSON := `{"classification":"skill_issue","skill_name":"data-processor","target":"body","reusable_guidance":"补充并发说明","is_reusable":true,"confidence":0.5}`
	rail := s.newChainRail(s.newChainMockModel([]string{attributionJSON}))
	coordinator := scheduling.NewReviewFeedbackEvolutionCoordinator("sess-lowconf", "alpha",
		func() scheduling.TeamEvolutionRailLike { return rail }, true, 0.7)
	rail.SetupReviewFeedbackEvolution("sess-lowconf", "alpha", 0.7, coordinator)

	s.Require().NoError(rail.HandleReviewFeedback(context.Background(), reviewPayload("task-d3", "并发不安全")))
	attempted, err := rail.FinalizeReviewFeedback(context.Background(), nil)
	s.Require().NoError(err)
	s.Require().False(attempted, "below-threshold attribution must not attempt evolution")
}

// TestReviewFeedbackChain_new_skill_pattern凑证 new_skill_pattern 重复证据 →
// SUGGEST_NEW_SKILL 观察（创建路由 D-25.2b-14 桩：观察入账断言）。
func (s *ReviewFeedbackChainSuite) TestReviewFeedbackChain_new_skill_pattern凑证() {
	attributionJSON := `{"classification":"new_skill_pattern","reusable_guidance":"先建索引再导出","is_reusable":true,"confidence":0.9}`
	rail := s.newChainRail(s.newChainMockModel([]string{attributionJSON, attributionJSON}))
	coordinator := scheduling.NewReviewFeedbackEvolutionCoordinator("sess-pattern", "alpha",
		func() scheduling.TeamEvolutionRailLike { return rail }, true, 0.7)
	rail.SetupReviewFeedbackEvolution("sess-pattern", "alpha", 0.7, coordinator)

	// 第一条：候选观测入账（无重复证据 → SKIP 但保留安全候选）。
	s.Require().NoError(rail.HandleReviewFeedback(context.Background(), reviewPayload("task-p1", "缺导出流程")))
	// 第二条（不同任务、同工作流）：重复证据凑齐 → SUGGEST_NEW_SKILL。
	payload2 := reviewPayload("task-p2", "导出也缺流程")
	s.Require().NoError(rail.HandleReviewFeedback(context.Background(), payload2))
	_ = payload2

	// Finalize：pattern 观察路由（创建 rail 缺位 → 日志桩，attempted=false 但
	// 不报错——D-25.2b-14 回收点）。
	_, err := rail.FinalizeReviewFeedback(context.Background(), nil)
	s.Require().NoError(err)
}

// ──────────────────────────── mock 客户端 ────────────────────────────

// reviewFeedbackMockClient 通道 B 的可编程 mock 模型客户端（对齐 rails 集成
// 套件的 mockTeamSkillClient 形态；两套件不共享测试件）。
type reviewFeedbackMockClient struct {
	invokeFn func(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error)
}

func (m *reviewFeedbackMockClient) Invoke(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
	if m.invokeFn != nil {
		return m.invokeFn(ctx, messages, opts...)
	}
	return llmschema.NewAssistantMessage("mock review feedback response"), nil
}
func (m *reviewFeedbackMockClient) Stream(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.StreamOption) (<-chan *llmschema.AssistantMessageChunk, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *reviewFeedbackMockClient) GenerateImage(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateImageOption) (*llmschema.ImageGenerationResponse, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *reviewFeedbackMockClient) GenerateSpeech(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateSpeechOption) (*llmschema.AudioGenerationResponse, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *reviewFeedbackMockClient) GenerateVideo(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateVideoOption) (*llmschema.VideoGenerationResponse, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *reviewFeedbackMockClient) TranscribeAudio(_ context.Context, _ string, _ ...llmschema.TranscribeAudioOption) (*llmschema.TranscriptionResponse, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *reviewFeedbackMockClient) Release(_ context.Context, _ ...model_clients.ReleaseOption) (bool, error) {
	return false, nil
}
func (m *reviewFeedbackMockClient) SupportsKVCacheRelease() bool  { return false }
func (m *reviewFeedbackMockClient) SupportsKVCacheAffinity() bool { return false }
func (m *reviewFeedbackMockClient) BuildKVCacheAffinityInvokeKwargs(_ any, _, _ string, _ int) map[string]any {
	return map[string]any{}
}
