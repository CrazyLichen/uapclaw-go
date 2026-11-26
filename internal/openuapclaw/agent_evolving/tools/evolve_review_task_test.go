package tools

import (
	"context"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/checkpointing"
	controller "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/controller"
	modules "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/controller/modules"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/tool"
	kv "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/kv_cache"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/interfaces"
	state "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/state"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/agents"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/interfaces"
	saprompt "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/prompts"
	agentschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/schema"
	harness "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/interfaces"
	review "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/rails/evolution/review"
	hschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// fakeParentAgentForEvolve DeepAgentInterface 最小测试替身：
// CreateSubagent 返回本替身自身（Invoke 回放预设结果）。
type fakeParentAgentForEvolve struct {
	harness.UnimplementedDeepAgentInteraction
	invokeResult map[string]any
	invokeErr    error
	invokeCalls  int
}

// Card 返回最小卡。
func (f *fakeParentAgentForEvolve) Card() *agentschema.AgentCard {
	card := &agentschema.AgentCard{}
	card.ID = "parent_1"
	card.Name = "parent"
	return card
}

// ReactAgent 空。
func (f *fakeParentAgentForEvolve) ReactAgent() *agents.ReActAgent { return nil }

// AbilityManager 空。
func (f *fakeParentAgentForEvolve) AbilityManager() agentinterfaces.AbilityManagerInterface {
	return nil
}

// SystemPromptBuilder 空。
func (f *fakeParentAgentForEvolve) SystemPromptBuilder() saprompt.SystemPromptBuilderInterface {
	return nil
}

// UapswarmProjectDir 空。
func (f *fakeParentAgentForEvolve) UapswarmProjectDir() string { return "" }

// SetUapswarmProjectDir 空。
func (f *fakeParentAgentForEvolve) SetUapswarmProjectDir(string) {}

// UapswarmCodeProjectDir 空。
func (f *fakeParentAgentForEvolve) UapswarmCodeProjectDir() string { return "" }

// SetUapswarmCodeProjectDir 空。
func (f *fakeParentAgentForEvolve) SetUapswarmCodeProjectDir(string) {}

// UapswarmAdapterMode 空。
func (f *fakeParentAgentForEvolve) UapswarmAdapterMode() string { return "" }

// SetUapswarmAdapterMode 空。
func (f *fakeParentAgentForEvolve) SetUapswarmAdapterMode(string) {}

// LoopCoordinator 空。
func (f *fakeParentAgentForEvolve) LoopCoordinator() harness.LoopCoordinatorInterface { return nil }

// LoopController 空。
func (f *fakeParentAgentForEvolve) LoopController() controller.ControllerInterface { return nil }

// EventHandler 空。
func (f *fakeParentAgentForEvolve) EventHandler() modules.EventHandler { return nil }

// LoadState 空。
func (f *fakeParentAgentForEvolve) LoadState(interfaces.SessionFacade) *hschema.DeepAgentState {
	return nil
}

// DeepConfig 空。
func (f *fakeParentAgentForEvolve) DeepConfig() *hschema.DeepAgentConfig { return nil }

// IsInvokeActive false。
func (f *fakeParentAgentForEvolve) IsInvokeActive() bool { return false }

// IsAutoInvokeScheduled false。
func (f *fakeParentAgentForEvolve) IsAutoInvokeScheduled() bool { return false }

// SetAutoInvokeScheduled 空。
func (f *fakeParentAgentForEvolve) SetAutoInvokeScheduled(bool) {}

// ScheduleAutoInvokeOnSpawnDone 空。
func (f *fakeParentAgentForEvolve) ScheduleAutoInvokeOnSpawnDone(context.Context, string, float64) error {
	return nil
}

// CreateSubagent 返回本替身。
func (f *fakeParentAgentForEvolve) CreateSubagent(context.Context, string, string) (harness.DeepAgentInterface, error) {
	return f, nil
}

// Invoke 回放预设结果。
func (f *fakeParentAgentForEvolve) Invoke(_ context.Context, _ map[string]any, _ ...agentinterfaces.AgentOption) (map[string]any, error) {
	f.invokeCalls++
	if f.invokeErr != nil {
		return nil, f.invokeErr
	}
	return f.invokeResult, nil
}

// SwitchMode 空。
func (f *fakeParentAgentForEvolve) SwitchMode(interfaces.SessionFacade, string) {}

// RestoreModeAfterPlanExit 空。
func (f *fakeParentAgentForEvolve) RestoreModeAfterPlanExit(interfaces.SessionFacade) {}

// GetPlanFilePath 空。
func (f *fakeParentAgentForEvolve) GetPlanFilePath(interfaces.SessionFacade) string { return "" }

// SaveState 空。
func (f *fakeParentAgentForEvolve) SaveState(interfaces.SessionFacade, *hschema.DeepAgentState) {}

// FindRailsByType 空。
func (f *fakeParentAgentForEvolve) FindRailsByType(...reflect.Type) []agentinterfaces.AgentRail {
	return nil
}

// AddRail 空。
func (f *fakeParentAgentForEvolve) AddRail(agentinterfaces.AgentRail) {}

// UnregisterRail 空。
func (f *fakeParentAgentForEvolve) UnregisterRail(context.Context, agentinterfaces.AgentRail) error {
	return nil
}

// InnerInvokeOverride 空。
func (f *fakeParentAgentForEvolve) InnerInvokeOverride() func(ctx context.Context, inputs map[string]any, opts ...agentinterfaces.AgentOption) (map[string]any, error) {
	return nil
}

// 编译期验证接口实现
var _ harness.DeepAgentInterface = (*fakeParentAgentForEvolve)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestEvolveReviewTaskInvoke_成功端到端 经真实 TaskTool 拉起 fake 子代理并回填审查结论。
// Python skill.py:230-267 成功路径。
func TestEvolveReviewTaskInvoke_成功端到端(t *testing.T) {
	store := checkpointing.NewEvolutionStore([]string{t.TempDir()})
	ctx := context.Background()
	_, err := store.CreateSkill(ctx, "demo_skill", "d", "body", "")
	require.NoError(t, err)
	rt := review.NewEvolutionReviewRuntime(60)
	subject := map[string]any{"kind": "skill", "name": "demo_skill"}
	launch, err := rt.CreateScope("agent_detected_signal", subject, "sess1", "", nil)
	require.NoError(t, err)

	reviewJSON := `{"subject":{"kind":"skill","name":"demo_skill"},"outcome":"recommend_evolve","evidence_refs":[],"proposals":[{"proposal_id":"p1","experience":{"summary":"s","content":"c","target":"body","section":"Troubleshooting"},"reason":"r","evidence_refs":[]}],"summary":"done"}`
	parent := &fakeParentAgentForEvolve{invokeResult: map[string]any{"output": reviewJSON}}
	created, err := CreateEvolveReviewTaskTool(EvolveReviewTaskToolDeps{
		ParentAgent: parent, ReviewRuntime: rt, Language: "cn", AgentID: "agt1",
	})
	require.NoError(t, err)

	// 子 agent 先 submit（真实 runtime 登记结论）
	_, err = rt.RecordReviewResult(launch.EvolutionReviewRef, "sess1", map[string]any{
		"subject": subject, "outcome": "recommend_evolve", "evidence_refs": []any{},
		"proposals": []any{map[string]any{
			"proposal_id": "p1",
			"experience":  map[string]any{"summary": "s", "content": "c", "target": "body", "section": "Troubleshooting"},
			"reason":      "r", "evidence_refs": []any{},
		}},
		"summary": "done",
	})
	require.NoError(t, err)

	result, err := created[0].Invoke(ctx, map[string]any{
		"evolution_review_ref": launch.EvolutionReviewRef,
		"conversation_id":      "sess1",
	}, tool.WithToolSession(&fakeSessionFacadeForEvolve{sessionID: "sess1"}))
	require.NoError(t, err)
	assert.Equal(t, true, result["success"])
	data := result["data"].(map[string]any)
	assert.Contains(t, data["output"], "recommend_evolve")
	assert.Equal(t, 1, parent.invokeCalls)

	// 任务失败透传（Python L247-248）
	failingParent := &fakeParentAgentForEvolve{invokeErr: assert.AnError}
	created2, err := CreateEvolveReviewTaskTool(EvolveReviewTaskToolDeps{
		ParentAgent: failingParent, ReviewRuntime: rt, Language: "cn",
	})
	require.NoError(t, err)
	result2, err := created2[0].Invoke(ctx, map[string]any{
		"evolution_review_ref": launch.EvolutionReviewRef,
		"conversation_id":      "sess1",
	}, tool.WithToolSession(&fakeSessionFacadeForEvolve{sessionID: "sess1"}))
	require.NoError(t, err)
	assert.Equal(t, false, result2["success"])
	assert.Contains(t, result2["error"], "子代理")
}

// fakeSessionFacadeForEvolve 最小 SessionFacade 替身。
type fakeSessionFacadeForEvolve struct {
	sessionID string
}

func (f *fakeSessionFacadeForEvolve) GetSessionID() string { return f.sessionID }

// GetAgentID 空桩（facade 15.24c 扩面）。
func (f *fakeSessionFacadeForEvolve) GetAgentID() string { return "" }

// GetTeamID 空桩（facade 15.24c 扩面）。
func (f *fakeSessionFacadeForEvolve) GetTeamID() string                            { return "" }
func (f *fakeSessionFacadeForEvolve) UpdateState(map[string]any)                   {}
func (f *fakeSessionFacadeForEvolve) GetState(state.StateKey) (any, error)         { return nil, nil }
func (f *fakeSessionFacadeForEvolve) DumpState() map[string]any                    { return nil }
func (f *fakeSessionFacadeForEvolve) WriteStream(context.Context, any) error       { return nil }
func (f *fakeSessionFacadeForEvolve) WriteCustomStream(context.Context, any) error { return nil }
func (f *fakeSessionFacadeForEvolve) GetEnv(string, ...any) any                    { return nil }
func (f *fakeSessionFacadeForEvolve) Interact(context.Context, any) error          { return nil }
func (f *fakeSessionFacadeForEvolve) ClearSession(context.Context)                 {}
func (f *fakeSessionFacadeForEvolve) GetCacheIdentity() kv.KVCacheIdentity {
	return kv.KVCacheIdentity{}
}
func (f *fakeSessionFacadeForEvolve) GetParentSessionID() string { return "" }

var _ interfaces.SessionFacade = (*fakeSessionFacadeForEvolve)(nil)

// TestReviewToolStreamAndTimeout 测试 Stream 不支持与超时策略。
func TestReviewToolStreamAndTimeout(t *testing.T) {
	tools, _, _ := newReviewTestKit(t)
	for _, tt := range tools {
		_, err := tt.Stream(context.Background(), nil)
		assert.ErrorIs(t, err, tool.ErrStreamNotSupported)
		assert.Equal(t, 3.0, tt.GetCallTimeout(nil, 3.0))
		assert.NotNil(t, tt.Card())
	}
	// 主 agent 六工具同口径
	mainTools, _, _ := newSkillTools(t)
	for _, tt := range mainTools {
		_, err := tt.Stream(context.Background(), nil)
		assert.ErrorIs(t, err, tool.ErrStreamNotSupported)
		assert.Equal(t, 5.0, tt.GetCallTimeout(nil, 5.0))
	}
}

// TestNewShortID 测试短 id 非空。
func TestNewShortID(t *testing.T) {
	assert.Len(t, newShortID(), 8)
}
