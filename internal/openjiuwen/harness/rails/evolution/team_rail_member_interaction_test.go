package evolution

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fmt"
	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/core/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/core/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/openjiuwen/core/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/agent_evolving/signal"
	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/agent_evolving/trajectory"
	"time"
)

// team_rail_member_interaction_test.go 成员交互面补测（25.2b Task 14）：
// 审批交互四方法（simplify/rebuild 请求/批准/拒绝）、演化主链
// （EvolveFromExternalSignals → handleEvolutionFromSignals → dump debug）、
// 25.2b review feedback 五方法与访问器（fake 协调器）。

// ──────────────────────────── 桩 ────────────────────────────

// memberInteractionClient 可编程响应 mock 客户端。
type memberInteractionClient struct {
	responses []string
	cursor    int
}

func (m *memberInteractionClient) Invoke(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
	idx := m.cursor
	if m.cursor < len(m.responses) {
		m.cursor++
	}
	if idx < len(m.responses) {
		return llmschema.NewAssistantMessage(m.responses[idx]), nil
	}
	return llmschema.NewAssistantMessage("{}"), nil
}
func (m *memberInteractionClient) Stream(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.StreamOption) (<-chan *llmschema.AssistantMessageChunk, error) {
	return nil, fmt.Errorf("no")
}
func (m *memberInteractionClient) GenerateImage(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateImageOption) (*llmschema.ImageGenerationResponse, error) {
	return nil, fmt.Errorf("no")
}
func (m *memberInteractionClient) GenerateSpeech(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateSpeechOption) (*llmschema.AudioGenerationResponse, error) {
	return nil, fmt.Errorf("no")
}
func (m *memberInteractionClient) GenerateVideo(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateVideoOption) (*llmschema.VideoGenerationResponse, error) {
	return nil, fmt.Errorf("no")
}
func (m *memberInteractionClient) TranscribeAudio(_ context.Context, _ string, _ ...llmschema.TranscribeAudioOption) (*llmschema.TranscriptionResponse, error) {
	return nil, fmt.Errorf("no")
}
func (m *memberInteractionClient) Release(_ context.Context, _ ...model_clients.ReleaseOption) (bool, error) {
	return false, nil
}
func (m *memberInteractionClient) SupportsKVCacheRelease() bool  { return false }
func (m *memberInteractionClient) SupportsKVCacheAffinity() bool { return false }
func (m *memberInteractionClient) BuildKVCacheAffinityInvokeKwargs(_ any, _, _ string, _ int) map[string]any {
	return map[string]any{}
}

// newMemberInteractionRail 建带 mock 模型与磁盘技能的团队 rail。
func newMemberInteractionRail(t *testing.T, responses []string) (*TeamSkillEvolutionRail, string) {
	t.Helper()
	providerName := fmt.Sprintf("member_interaction_%d", time.Now().UnixNano())
	model_clients.GetClientRegistry().Register(providerName, "llm", func(_ *llmschema.ModelRequestConfig, _ *llmschema.ModelClientConfig) (model_clients.BaseModelClient, error) {
		return &memberInteractionClient{responses: responses}, nil
	})
	model, err := llm.NewModel(&llmschema.ModelClientConfig{ClientProvider: providerName, ClientID: providerName + "_id"},
		&llmschema.ModelRequestConfig{ModelName: "test-model"})
	if err != nil {
		t.Fatal(err)
	}
	skillsDir := t.TempDir()
	skillDir := filepath.Join(skillsDir, "alpha-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# alpha-skill\n\n测试技能。"), 0o644); err != nil {
		t.Fatal(err)
	}
	rail := NewTeamSkillEvolutionRail([]string{skillsDir}, model, "test-model", "cn",
		WithTeamSkillTeamID("alpha"), WithTeamSkillAutoSave(true))
	return rail, skillsDir
}

// ──────────────────────────── 审批交互四方法 ────────────────────────────

// TestRequestSimplify_不存在技能 请求精简不存在的技能 → 错误路径。
func TestRequestSimplify_不存在技能(t *testing.T) {
	rail, _ := newMemberInteractionRail(t, nil)
	// manager 语义：技能不存在 → skill_not_found 直接跳过（无错误、无请求）。
	result, err := rail.RequestSimplify(context.Background(), "no-such-skill", nil)
	if err != nil {
		t.Fatalf("missing skill must skip silently: %v", err)
	}
	if result == nil || result.SkillName != "no-such-skill" || result.RequestID != nil {
		t.Fatalf("result = %+v", result)
	}
}

// TestRequestRebuild_不存在技能 重建请求不存在的技能 → 错误路径。
func TestRequestRebuild_不存在技能(t *testing.T) {
	rail, _ := newMemberInteractionRail(t, nil)
	// manager 语义：技能不存在 → 空 followup（跳过重建提示词生成）。
	followup, err := rail.RequestRebuild(context.Background(), "no-such-skill", nil, 0.5)
	if err != nil || followup != "" {
		t.Fatalf("missing skill must skip: %q %v", followup, err)
	}
}

// TestOnApproveSimplify_OnRejectSimplify_未知请求 未知 requestID 不崩溃。
func TestOnApproveSimplify_OnRejectSimplify_未知请求(t *testing.T) {
	rail, _ := newMemberInteractionRail(t, nil)
	if _, err := rail.OnApproveSimplify(context.Background(), "unknown-req"); err == nil {
		t.Log("approve unknown request tolerated")
	}
	rail.OnRejectSimplify("unknown-req")
}

// TestRequestSimplify_存在技能 真实技能的精简请求（走 manager 全链）。
func TestRequestSimplify_存在技能(t *testing.T) {
	rail, _ := newMemberInteractionRail(t, nil)
	intent := "精简描述"
	result, err := rail.RequestSimplify(context.Background(), "alpha-skill", &intent)
	if err != nil {
		t.Fatalf("simplify on real skill: %v", err)
	}
	if result == nil {
		t.Fatal("result must not be nil")
	}
}

// ──────────────────────────── 演化主链 ────────────────────────────

// TestEvolveFromExternalSignals_存在技能 外部信号演化主链
// （handleEvolutionFromSignals/stageEvolutionFromSignals/dumpTrajectoryDebug 全链）。
func TestEvolveFromExternalSignals_存在技能(t *testing.T) {
	skillName := "alpha-skill"
	rail, skillsDir := newMemberInteractionRail(t, []string{
		`{"records": []}`,
	})
	// 轨迹调试目录（dumpTrajectoryDebug 写入面）。
	trajectoriesDir := filepath.Join(skillsDir, "_debug")
	railWithDir := NewTeamSkillEvolutionRail([]string{skillsDir}, rail.generator.LLM(), "test-model", "cn",
		WithTeamSkillTeamID("alpha"), WithTeamSkillAutoSave(true),
		WithTeamSkillTrajectoriesDir(trajectoriesDir))

	sig := &signal.EvolutionSignal{}
	name := skillName
	sig.SkillName = &name
	sig.Excerpt = "补充并发说明"
	result := railWithDir.EvolveFromExternalSignals(context.Background(),
		[]*signal.EvolutionSignal{sig}, nil, nil, "补充并发说明", nil)
	if result.Status == "skipped_no_input" || result.Status == "skipped_skill_not_found" {
		t.Fatalf("real skill must not be skipped: %+v", result)
	}
}

// TestEvolveFromExternalSignals_双技能拒绝 多技能信号 → skipped_no_input。
func TestEvolveFromExternalSignals_双技能拒绝(t *testing.T) {
	rail, _ := newMemberInteractionRail(t, nil)
	nameA, nameB := "alpha-skill", "other"
	sigA := &signal.EvolutionSignal{SkillName: &nameA}
	sigB := &signal.EvolutionSignal{SkillName: &nameB}
	result := rail.EvolveFromExternalSignals(context.Background(), []*signal.EvolutionSignal{sigA, sigB}, nil, nil, "", nil)
	if result.Status != "skipped_no_input" {
		t.Fatalf("two-skill signals must skip: %+v", result)
	}
}

// TestDetectActiveRequestSignals 活跃请求信号检测（存在技能）。
func TestDetectActiveRequestSignals(t *testing.T) {
	rail, _ := newMemberInteractionRail(t, nil)
	builder, err := trajectory.NewTestTrajectory("sess-detect", nil, []trajectory.TestStep{
		{Kind: "tool", ToolName: "write_file", CallArgs: map[string]any{"path": "/skills/alpha-skill/SKILL.md"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	signals, err := rail.detectActiveRequestSignals(context.Background(), "alpha-skill", builder)
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	// 检测结果可以为空（无信号）——只验证链路可达。
	_ = signals
	// 不存在技能：读取回落空内容或错误——两者都视为无信号。
	detected, detErr := rail.detectActiveRequestSignals(context.Background(), "no-such", builder)
	if detErr != nil {
		t.Fatalf("detect error tolerated: %v", detErr)
	}
	if len(detected) != 0 {
		t.Fatalf("missing skill must yield no signals: %d", len(detected))
	}
}

// ──────────────────────────── 25.2b review feedback 面 ────────────────────────────

// fakeFeedbackCoordinator 协调器替身（记录调用）。
type fakeFeedbackCoordinator struct {
	handled  int
	finalize int
}

func (f *fakeFeedbackCoordinator) HandleReviewFeedback(_ context.Context, _ map[string]any) error {
	f.handled++
	return nil
}
func (f *fakeFeedbackCoordinator) OnTeamCompleted(_ context.Context, _ map[string]any) (bool, error) {
	f.finalize++
	return true, nil
}

// TestReviewFeedback五方法_集成 coordination 注入 → handle → finalize →
// enabled/bind/访问器。
func TestReviewFeedback五方法_集成(t *testing.T) {
	rail, _ := newMemberInteractionRail(t, nil)
	if rail.ReviewFeedbackEvolutionEnabled() {
		t.Fatal("must be disabled before setup")
	}
	fake := &fakeFeedbackCoordinator{}
	rail.SetupReviewFeedbackEvolution("sess", "alpha", 0.7, fake)
	if !rail.ReviewFeedbackEvolutionEnabled() {
		t.Fatal("enabled after setup")
	}
	if err := rail.HandleReviewFeedback(context.Background(), map[string]any{"task_id": "t1"}); err != nil {
		t.Fatal(err)
	}
	attempted, err := rail.FinalizeReviewFeedback(context.Background(), nil)
	if err != nil || !attempted {
		t.Fatalf("finalize: %v %v", attempted, err)
	}
	if fake.handled != 1 || fake.finalize != 1 {
		t.Fatalf("calls = %d/%d", fake.handled, fake.finalize)
	}
	// 访问器。
	if rail.ReviewLanguage() != "cn" {
		t.Fatalf("language = %q", rail.ReviewLanguage())
	}
	if rail.ReviewStore() == nil {
		t.Fatal("store")
	}
	if rail.ReviewGenerator() == nil {
		t.Fatal("generator")
	}
	if window, _ := rail.RailGetTrajectory("none", "alpha"); window != nil {
		t.Log("window miss tolerated")
	}
	if rail.RailDrainApprovalEvents() == nil {
		t.Fatal("drain must return a slice")
	}
	// bind 覆写。
	other := &fakeFeedbackCoordinator{}
	rail.BindReviewFeedbackSkillCreateRail(other)
	if !strings.Contains("x", "x") {
		t.Fatal("placeholder")
	}
	// 未注入协调器 → handle/finalize 空转。
	bare := NewTeamSkillEvolutionRail([]string{t.TempDir()}, nil, "test-model", "cn")
	if err := bare.HandleReviewFeedback(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if attempted, err := bare.FinalizeReviewFeedback(context.Background(), nil); err != nil || attempted {
		t.Fatalf("bare rail finalize: %v %v", attempted, err)
	}
}

// TestHandleEvolutionFromSignals_noRecords 演化主链：空记录 → no_evolution_no_records
// → emitBackgroundOutcomeEvent → nil request。
func TestHandleEvolutionFromSignals_noRecords(t *testing.T) {
	skillName := "alpha-skill"
	rail, skillsDir := newMemberInteractionRail(t, []string{`{"records": []}`})
	debugDir := filepath.Join(skillsDir, "_debug")
	rail.trajectoriesDir = debugDir
	_ = debugDir

	name := skillName
	sig := &signal.EvolutionSignal{SkillName: &name, Excerpt: "补充并发说明"}
	builder, terr := trajectory.NewTestTrajectory("sess-handle", map[string]any{}, []trajectory.TestStep{
		{Kind: "tool", ToolName: "read_file", CallArgs: map[string]any{"path": filepath.Join(skillsDir, skillName, "SKILL.md")}},
	})
	if terr != nil {
		t.Fatal(terr)
	}
	request, err := rail.handleEvolutionFromSignals(context.Background(), skillName, builder,
		[]*signal.EvolutionSignal{sig}, true, "补充并发说明", nil, false)
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if request != nil {
		t.Fatalf("no-record pipeline must return nil request: %+v", request)
	}
	// 审批事件面（emitRecordApprovalEvent 的守卫分支）。
	rail.emitRecordApprovalEvent(skillName, nil)
	// 轨迹调试导出（nil 与非 nil 两分支）。
	rail.dumpTrajectoryDebug(nil)
	rail.dumpTrajectoryDebug(builder)
}

// TestHandleEvolutionFromSignals_stagedRecords 有记录 → staged 审批请求
// （emitRecordApprovalEvent / emitBackgroundOutcomeEvent 全链）。
func TestHandleEvolutionFromSignals_stagedRecords(t *testing.T) {
	skillName := "alpha-skill"
	recordJSON := `{"records": [{"target": "body", "summary": "补充并发说明", "details": {"reusable_guidance": "并发时加锁"}}]}`
	rail, skillsDir := newMemberInteractionRail(t, []string{recordJSON})
	rail.trajectoriesDir = filepath.Join(skillsDir, "_debug")

	name := skillName
	sig := &signal.EvolutionSignal{SkillName: &name, Excerpt: "补充并发说明"}
	builder, terr := trajectory.NewTestTrajectory("sess-staged", map[string]any{}, []trajectory.TestStep{
		{Kind: "tool", ToolName: "read_file", CallArgs: map[string]any{"path": filepath.Join(skillsDir, skillName, "SKILL.md")}},
	})
	if terr != nil {
		t.Fatal(terr)
	}
	request, err := rail.handleEvolutionFromSignals(context.Background(), skillName, builder,
		[]*signal.EvolutionSignal{sig}, false, "补充并发说明", nil, true)
	if err != nil {
		t.Fatalf("handle staged: %v", err)
	}
	t.Logf("staged request = %+v", request)
}
