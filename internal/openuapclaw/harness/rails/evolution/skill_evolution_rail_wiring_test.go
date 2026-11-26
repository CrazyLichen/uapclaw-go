package evolution

import (
	"context"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/common/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/checkpointing"
	controllerpkg "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/controller"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/controller/modules"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/tool"
	cb "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/runner/callback"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/stream"
	agents "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/agents"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/interfaces"
	saprompt "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/prompts"
	agentschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/schema"
	harnessinterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/rails/evolution/review"
	harnessschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestSkillEvolutionRail_InitRegistersToolsAndReviewAgent 对齐 init（skill_evolution_rail.py:379-384）：
// init 后 ability manager 注册六主工具且 deep_config.subagents 恰有一个 evolution_reviewer；
// uninit 后工具与 review agent 均移除（:385-394）。
func TestSkillEvolutionRail_InitRegistersToolsAndReviewAgent(t *testing.T) {
	r := newConstructedRail(t)
	r.reviewRuntime = reviewRuntimeForWiringTest()
	r.evolutionStore = checkpointing.NewEvolutionStore([]string{t.TempDir()})

	agent := newFakeDeepAgentForWiring(t)
	require.NoError(t, r.Init(context.Background(), agent))
	defer func() { _ = r.Uninit(agent) }()

	// 六主工具注册
	assert.Len(t, agent.abilityMgr.added, 6)
	names := map[string]bool{}
	for _, card := range agent.abilityMgr.added {
		names[card.Name] = true
	}
	for _, want := range []string{
		"prepare_skill_evolution", "evolve_review_task", "list_skill_experiences",
		"read_skill_experiences", "evolve_skill_experiences", "simplify_skill_experiences",
	} {
		assert.True(t, names[want], "应注册工具 %s", want)
	}
	// 审查子 agent 注入且唯一
	subagents := agent.deepConfig.Subagents
	require.Len(t, subagents, 1)
	cfg, ok := subagents[0].(*harnessschema.SubAgentConfig)
	require.True(t, ok)
	assert.Equal(t, "evolution_reviewer", cfg.AgentCard.Name)

	// uninit 移除
	require.NoError(t, r.Uninit(agent))
	// Python: _unregister_runtime_tools 按工具名移除六主工具
	assert.Len(t, agent.abilityMgr.removedNames, 6)
	assert.Empty(t, agent.deepConfig.Subagents)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// reviewRuntimeForWiringTest 构造接线测试用审查运行时。
func reviewRuntimeForWiringTest() *review.EvolutionReviewRuntime {
	return review.NewEvolutionReviewRuntime(60)
}

// fakeAbilityMgrForWiring 记录 AddAbility/Remove 的能力管理器替身。
type fakeAbilityMgrForWiring struct {
	agentinterfaces.AbilityManagerInterface
	added        []*tool.ToolCard
	removedNames []string
}

// Add 记录后返回空结果。
func (f *fakeAbilityMgrForWiring) Add(ability schema.Ability) agentschema.AddAbilityResult {
	return agentschema.AddAbilityResult{}
}

// AddAbility 记录注册的卡片。
func (f *fakeAbilityMgrForWiring) AddAbility(card *tool.ToolCard, resource tool.Tool) agentschema.AddAbilityResult {
	f.added = append(f.added, card)
	return agentschema.AddAbilityResult{}
}

// Remove 记录移除名。
func (f *fakeAbilityMgrForWiring) Remove(name string) schema.Ability {
	f.removedNames = append(f.removedNames, name)
	return nil
}

// fakeDeepAgentForWiring 接线测试用最小 DeepAgentInterface。
type fakeDeepAgentForWiring struct {
	harnessinterfaces.UnimplementedDeepAgentInteraction
	abilityMgr *fakeAbilityMgrForWiring
	deepConfig *harnessschema.DeepAgentConfig
	card       *agentschema.AgentCard
}

// newFakeDeepAgentForWiring 构造接线测试替身。
func newFakeDeepAgentForWiring(t *testing.T) *fakeDeepAgentForWiring {
	t.Helper()
	card := agentschema.NewAgentCard(agentschema.WithAgentID("agent-1"))
	return &fakeDeepAgentForWiring{
		abilityMgr: &fakeAbilityMgrForWiring{},
		deepConfig: &harnessschema.DeepAgentConfig{},
		card:       card,
	}
}

// AbilityManager 返回假能力管理器。
func (f *fakeDeepAgentForWiring) AbilityManager() agentinterfaces.AbilityManagerInterface {
	return f.abilityMgr
}

// CallbackManager 空桩（BaseAgent 要求）。
func (f *fakeDeepAgentForWiring) CallbackManager() *agentinterfaces.AgentCallbackManager {
	return nil
}

// Config 空桩（BaseAgent 要求）。
func (f *fakeDeepAgentForWiring) Config() agentinterfaces.AgentConfig {
	return nil
}

// Configure 空桩（BaseAgent 要求）。
func (f *fakeDeepAgentForWiring) Configure(context.Context, agentinterfaces.AgentConfig) error {
	return nil
}

// Stream 空桩（BaseAgent 要求）。
func (f *fakeDeepAgentForWiring) Stream(context.Context, map[string]any, ...agentinterfaces.AgentOption) (<-chan stream.Schema, error) {
	return nil, nil
}

// RegisterCallback 空桩（BaseAgent 要求）。
func (f *fakeDeepAgentForWiring) RegisterCallback(context.Context, agentinterfaces.AgentCallbackEvent, cb.PerAgentCallbackFunc, ...cb.CallbackOption) error {
	return nil
}

// RegisterRail 空桩（BaseAgent 要求）。
func (f *fakeDeepAgentForWiring) RegisterRail(context.Context, agentinterfaces.AgentRail, ...cb.CallbackOption) error {
	return nil
}

// UnregisterRail 空桩（同时满足 BaseAgent 与 DeepAgentInterface 的同名方法签名）。
func (f *fakeDeepAgentForWiring) UnregisterRail(context.Context, agentinterfaces.AgentRail) error {
	return nil
}

// ReactAgent 空桩。
func (f *fakeDeepAgentForWiring) ReactAgent() *agents.ReActAgent { return nil }

// Card 返回身份卡片。
func (f *fakeDeepAgentForWiring) Card() *agentschema.AgentCard { return f.card }

// SystemPromptBuilder 空桩。
func (f *fakeDeepAgentForWiring) SystemPromptBuilder() saprompt.SystemPromptBuilderInterface {
	return nil
}

// LoopCoordinator 空桩。
func (f *fakeDeepAgentForWiring) LoopCoordinator() harnessinterfaces.LoopCoordinatorInterface {
	return nil
}

// LoopController 空桩。
func (f *fakeDeepAgentForWiring) LoopController() controllerpkg.ControllerInterface { return nil }

// EventHandler 空桩。
func (f *fakeDeepAgentForWiring) EventHandler() modules.EventHandler { return nil }

// LoadState 空桩。
func (f *fakeDeepAgentForWiring) LoadState(sess sessioninterfaces.SessionFacade) *harnessschema.DeepAgentState {
	return &harnessschema.DeepAgentState{}
}

// DeepConfig 返回注入的 deep 配置。
func (f *fakeDeepAgentForWiring) DeepConfig() *harnessschema.DeepAgentConfig { return f.deepConfig }

// IsInvokeActive 空桩。
func (f *fakeDeepAgentForWiring) IsInvokeActive() bool { return false }

// IsAutoInvokeScheduled 空桩。
func (f *fakeDeepAgentForWiring) IsAutoInvokeScheduled() bool { return false }

// SetAutoInvokeScheduled 空桩。
func (f *fakeDeepAgentForWiring) SetAutoInvokeScheduled(bool) {}

// ScheduleAutoInvokeOnSpawnDone 空桩。
func (f *fakeDeepAgentForWiring) ScheduleAutoInvokeOnSpawnDone(context.Context, string, float64) error {
	return nil
}

// CreateSubagent 空桩。
func (f *fakeDeepAgentForWiring) CreateSubagent(context.Context, string, string) (harnessinterfaces.DeepAgentInterface, error) {
	return nil, nil
}

// Invoke 空桩。
func (f *fakeDeepAgentForWiring) Invoke(context.Context, map[string]any, ...agentinterfaces.AgentOption) (map[string]any, error) {
	return nil, nil
}

// SwitchMode 空桩。
func (f *fakeDeepAgentForWiring) SwitchMode(sessioninterfaces.SessionFacade, string) {}

// RestoreModeAfterPlanExit 空桩。
func (f *fakeDeepAgentForWiring) RestoreModeAfterPlanExit(sessioninterfaces.SessionFacade) {}

// GetPlanFilePath 空桩。
func (f *fakeDeepAgentForWiring) GetPlanFilePath(sessioninterfaces.SessionFacade) string { return "" }

// SaveState 空桩。
func (f *fakeDeepAgentForWiring) SaveState(sessioninterfaces.SessionFacade, *harnessschema.DeepAgentState) {
}

// FindRailsByType 空桩。
func (f *fakeDeepAgentForWiring) FindRailsByType(...reflect.Type) []agentinterfaces.AgentRail {
	return nil
}

// AddRail 空桩。
func (f *fakeDeepAgentForWiring) AddRail(agentinterfaces.AgentRail) {}

// InnerInvokeOverride 空桩。
func (f *fakeDeepAgentForWiring) InnerInvokeOverride() func(context.Context, map[string]any, ...agentinterfaces.AgentOption) (map[string]any, error) {
	return nil
}

// UapswarmProjectDir 空桩。
func (f *fakeDeepAgentForWiring) UapswarmProjectDir() string { return "" }

// SetUapswarmProjectDir 空桩。
func (f *fakeDeepAgentForWiring) SetUapswarmProjectDir(string) {}

// UapswarmCodeProjectDir 空桩。
func (f *fakeDeepAgentForWiring) UapswarmCodeProjectDir() string { return "" }

// SetUapswarmCodeProjectDir 空桩。
func (f *fakeDeepAgentForWiring) SetUapswarmCodeProjectDir(string) {}

// UapswarmAdapterMode 空桩。
func (f *fakeDeepAgentForWiring) UapswarmAdapterMode() string { return "" }

// SetUapswarmAdapterMode 空桩。
func (f *fakeDeepAgentForWiring) SetUapswarmAdapterMode(string) {}

// 编译时验证
var _ harnessinterfaces.DeepAgentInterface = (*fakeDeepAgentForWiring)(nil)
