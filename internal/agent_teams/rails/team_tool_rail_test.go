package rails

import (
	"context"
	"reflect"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/messager"
	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools/database"
	cschema "github.com/uapclaw/uapclaw-go/internal/common/schema"

	ceinterface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller/modules"
	harnessinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/interfaces"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/agents"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// fakeBaseAgentTeam 测试用 BaseAgent mock
type fakeBaseAgentTeam struct {
	cbMgr   *agentinterfaces.AgentCallbackManager
	builder *saprompt.SystemPromptBuilder
	am      agentinterfaces.AbilityManagerInterface
}

// fakeAbilityManagerTeam 测试用 AbilityManager mock
type fakeAbilityManagerTeam struct {
	added   []string
	removed []string
}

func newFakeBaseAgentTeam() *fakeBaseAgentTeam {
	return &fakeBaseAgentTeam{
		cbMgr:   agentinterfaces.NewAgentCallbackManager("test-agent"),
		builder: saprompt.NewSystemPromptBuilder(),
		am:      &fakeAbilityManagerTeam{},
	}
}

func (f *fakeBaseAgentTeam) Configure(_ context.Context, _ agentinterfaces.AgentConfig) error {
	return nil
}
func (f *fakeBaseAgentTeam) Invoke(_ context.Context, _ map[string]any, _ ...agentinterfaces.AgentOption) (map[string]any, error) {
	return nil, nil
}
func (f *fakeBaseAgentTeam) Stream(_ context.Context, _ map[string]any, _ ...agentinterfaces.AgentOption) (<-chan stream.Schema, error) {
	return nil, nil
}
func (f *fakeBaseAgentTeam) Card() *agentschema.AgentCard {
	return &agentschema.AgentCard{
		BaseCard: cschema.BaseCard{ID: "test-agent", Name: "TestAgent"},
	}
}
func (f *fakeBaseAgentTeam) Config() agentinterfaces.AgentConfig                     { return nil }
func (f *fakeBaseAgentTeam) AbilityManager() agentinterfaces.AbilityManagerInterface { return f.am }
func (f *fakeBaseAgentTeam) CallbackManager() *agentinterfaces.AgentCallbackManager  { return f.cbMgr }
func (f *fakeBaseAgentTeam) SystemPromptBuilder() saprompt.SystemPromptBuilderInterface {
	if f.builder == nil {
		return nil
	}
	return f.builder
}
func (f *fakeBaseAgentTeam) RegisterCallback(_ context.Context, _ agentinterfaces.AgentCallbackEvent, _ cb.PerAgentCallbackFunc, _ ...cb.CallbackOption) error {
	return nil
}
func (f *fakeBaseAgentTeam) RegisterRail(_ context.Context, _ agentinterfaces.AgentRail, _ ...cb.CallbackOption) error {
	return nil
}
func (f *fakeBaseAgentTeam) UnregisterRail(_ context.Context, _ agentinterfaces.AgentRail) error {
	return nil
}

// DeepAgentInterface 方法（桩实现，供 TeamPlanModeRail.Init 类型断言通过）
func (f *fakeBaseAgentTeam) ReactAgent() *agents.ReActAgent                                 { return nil }
func (f *fakeBaseAgentTeam) LoopCoordinator() harnessinterfaces.LoopCoordinatorInterface   { return nil }
func (f *fakeBaseAgentTeam) LoopController() controller.ControllerInterface                { return nil }
func (f *fakeBaseAgentTeam) EventHandler() modules.EventHandler                             { return nil }
func (f *fakeBaseAgentTeam) LoadState(_ sessioninterfaces.SessionFacade) *hschema.DeepAgentState {
	return nil
}
func (f *fakeBaseAgentTeam) DeepConfig() *hschema.DeepAgentConfig { return nil }
func (f *fakeBaseAgentTeam) IsInvokeActive() bool                  { return false }
func (f *fakeBaseAgentTeam) IsAutoInvokeScheduled() bool           { return false }
func (f *fakeBaseAgentTeam) SetAutoInvokeScheduled(_ bool)         {}
func (f *fakeBaseAgentTeam) ScheduleAutoInvokeOnSpawnDone(_ context.Context, _ string, _ float64) error {
	return nil
}
func (f *fakeBaseAgentTeam) CreateSubagent(_ context.Context, _ string, _ string) (harnessinterfaces.DeepAgentInterface, error) {
	return nil, nil
}
func (f *fakeBaseAgentTeam) SwitchMode(_ sessioninterfaces.SessionFacade, _ string)        {}
func (f *fakeBaseAgentTeam) RestoreModeAfterPlanExit(_ sessioninterfaces.SessionFacade)    {}
func (f *fakeBaseAgentTeam) GetPlanFilePath(_ sessioninterfaces.SessionFacade) string       { return "" }
func (f *fakeBaseAgentTeam) SaveState(_ sessioninterfaces.SessionFacade, _ *hschema.DeepAgentState) {}
func (f *fakeBaseAgentTeam) FindRailsByType(_ ...reflect.Type) []agentinterfaces.AgentRail { return nil }

// 编译时验证
var _ agentinterfaces.BaseAgent = (*fakeBaseAgentTeam)(nil)
var _ harnessinterfaces.DeepAgentInterface = (*fakeBaseAgentTeam)(nil)

// fakeAbilityManagerTeam 方法实现
func (f *fakeAbilityManagerTeam) Add(ability cschema.Ability) agentschema.AddAbilityResult {
	f.added = append(f.added, ability.AbilityName())
	return agentschema.AddAbilityResult{}
}
func (f *fakeAbilityManagerTeam) AddMany(abilities []cschema.Ability) []agentschema.AddAbilityResult {
	for _, a := range abilities {
		f.Add(a)
	}
	return nil
}
func (f *fakeAbilityManagerTeam) Remove(name string) cschema.Ability {
	f.removed = append(f.removed, name)
	return nil
}
func (f *fakeAbilityManagerTeam) RemoveMany(names []string) []cschema.Ability { return nil }
func (f *fakeAbilityManagerTeam) Get(_ string) cschema.Ability                { return nil }
func (f *fakeAbilityManagerTeam) List() []cschema.Ability                     { return nil }
func (f *fakeAbilityManagerTeam) ListToolInfo(_ context.Context, _ []string, _ ...string) ([]cschema.ToolInfoInterface, error) {
	return nil, nil
}
func (f *fakeAbilityManagerTeam) Execute(_ context.Context, _ *agentinterfaces.AgentCallbackContext, _ []*llmschema.ToolCall, _ sessioninterfaces.SessionFacade, _ string) []agentschema.ExecuteResult {
	return nil
}
func (f *fakeAbilityManagerTeam) SetContextEngine(_ ceinterface.ContextEngine) {}
func (f *fakeAbilityManagerTeam) ReorderTools(_ []string)                      {}

// 编译时验证
var _ agentinterfaces.AbilityManagerInterface = (*fakeAbilityManagerTeam)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// newRailsTestTeamBackend 创建测试用 TeamBackend
func newRailsTestTeamBackend() *tools.TeamBackend {
	db := database.NewInMemoryTeamDatabase()
	msg := messager.NewInProcessMessager(atschema.NewMessagerTransportConfig())
	return tools.NewTeamBackend("test-team", "leader", true, db, msg)
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestNewTeamToolRail_默认值(t *testing.T) {
	r := NewTeamToolRail()
	if r == nil {
		t.Fatal("NewTeamToolRail returned nil")
	}
	if r.teamName != "default" {
		t.Fatalf("expected teamName=default, got %s", r.teamName)
	}
}

// TestNewTeamToolRail_WithOptions 测试选项构造
func TestNewTeamToolRail_WithOptions(t *testing.T) {
	r := NewTeamToolRail(
		WithRole("leader"),
		WithTeammateMode("build_mode"),
		WithLifecycle("temporary"),
		WithLanguage("cn"),
		WithTeamName("test-team"),
		WithMemberName("alice"),
	)
	if r.role != "leader" {
		t.Fatalf("expected role=leader, got %s", r.role)
	}
	if r.teamName != "test-team" {
		t.Fatalf("expected teamName=test-team, got %s", r.teamName)
	}
}

// TestTeamToolRail_Init_注册工具 测试 Init 注册工具到 AbilityManager
func TestTeamToolRail_Init_注册工具(t *testing.T) {
	agent := newFakeBaseAgentTeam()
	tb := newRailsTestTeamBackend()

	r := NewTeamToolRail(
		WithTeamBackend(tb),
		WithRole("leader"),
		WithTeammateMode("build_mode"),
		WithLifecycle("temporary"),
		WithLanguage("cn"),
	)
	err := r.Init(context.Background(), agent)
	if err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	if len(r.registeredTools) == 0 {
		t.Fatal("expected some registered tools after Init")
	}
	am := agent.am.(*fakeAbilityManagerTeam)
	if len(am.added) == 0 {
		t.Fatal("expected tools added to AbilityManager")
	}
}

// TestTeamToolRail_Init_幂等 测试 Init 幂等性
func TestTeamToolRail_Init_幂等(t *testing.T) {
	agent := newFakeBaseAgentTeam()
	tb := newRailsTestTeamBackend()

	r := NewTeamToolRail(
		WithTeamBackend(tb),
		WithRole("leader"),
		WithTeammateMode("build_mode"),
		WithLifecycle("temporary"),
		WithLanguage("cn"),
	)
	_ = r.Init(context.Background(), agent)
	firstCount := len(r.registeredTools)
	_ = r.Init(context.Background(), agent)
	if len(r.registeredTools) != firstCount {
		t.Fatal("Init should be idempotent")
	}
}

// TestTeamToolRail_Uninit 测试 Uninit 移除工具
func TestTeamToolRail_Uninit(t *testing.T) {
	agent := newFakeBaseAgentTeam()
	tb := newRailsTestTeamBackend()

	r := NewTeamToolRail(
		WithTeamBackend(tb),
		WithRole("leader"),
		WithTeammateMode("build_mode"),
		WithLifecycle("temporary"),
		WithLanguage("cn"),
	)
	_ = r.Init(context.Background(), agent)
	if r.registeredTools == nil {
		t.Fatal("expected tools after Init")
	}
	_ = r.Uninit(agent)
	if r.registeredTools != nil {
		t.Fatal("expected nil tools after Uninit")
	}
	am := agent.am.(*fakeAbilityManagerTeam)
	if len(am.removed) == 0 {
		t.Fatal("expected tools removed from AbilityManager")
	}
}

// TestTeamToolRail_QualifyIDs 测试工具 ID 后缀
func TestTeamToolRail_QualifyIDs(t *testing.T) {
	agent := newFakeBaseAgentTeam()

	r := NewTeamToolRail(
		WithRole("leader"),
		WithTeammateMode("build_mode"),
		WithLifecycle("temporary"),
		WithLanguage("cn"),
		WithQualifyIDs(true),
		WithTeamName("my-team"),
		WithMemberName("alice"),
	)
	_ = r.Init(context.Background(), agent)

	for _, tl := range r.registeredTools {
		card := tl.Card()
		if card != nil && card.ID != "" {
			hasSuffix := len(card.ID) > len(".my-team.alice")
			if !hasSuffix {
				t.Fatalf("expected qualified ID, got %s", card.ID)
			}
		}
	}
}

// TestTeamToolRail_Teammate角色 测试 Teammate 角色过滤
func TestTeamToolRail_Teammate角色(t *testing.T) {
	agent := newFakeBaseAgentTeam()

	r := NewTeamToolRail(
		WithRole("teammate"),
		WithTeammateMode("build_mode"),
		WithLifecycle("temporary"),
		WithLanguage("cn"),
	)
	_ = r.Init(context.Background(), agent)

	for _, tl := range r.registeredTools {
		card := tl.Card()
		if card != nil && card.Name == "build_team" {
			t.Fatal("teammate should not have build_team")
		}
	}
}

// TestTeamToolRail_NilAbilityManager 测试 AbilityManager 为 nil 时不 panic
func TestTeamToolRail_NilAbilityManager(t *testing.T) {
	agent := newFakeBaseAgentTeam()
	agent.am = nil

	r := NewTeamToolRail(
		WithRole("leader"),
		WithTeammateMode("build_mode"),
		WithLifecycle("temporary"),
		WithLanguage("cn"),
	)
	err := r.Init(context.Background(), agent)
	if err != nil {
		t.Fatalf("Init should not fail with nil AbilityManager: %v", err)
	}
}

// TestTeamToolRail_RegisteredTools 测试 RegisteredTools 访问器
func TestTeamToolRail_RegisteredTools(t *testing.T) {
	r := NewTeamToolRail()
	if r.RegisteredTools() != nil {
		t.Fatal("expected nil before Init")
	}
}
