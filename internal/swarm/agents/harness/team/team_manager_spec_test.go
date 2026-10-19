package team

import (
	"context"
	"reflect"
	"testing"

	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	ceinterface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller/modules"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	hinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/interfaces"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/agents"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	cschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
	skillruntime "github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime/skill"
)

// TestLoadTeamSpec_分布式normalize 缺依赖时回退 local（Go 恒无缺依赖，仅验证 normalize 路径）。
func TestLoadTeamSpec_分布式normalize(t *testing.T) {
	cfg := map[string]any{
		"team": map[string]any{
			"runtime":   map[string]any{"mode": "distributed", "role": "leader"},
			"transport": map[string]any{"type": "pyzmq"},
		},
		"modes":  map[string]any{"team": map[string]any{}},
		"models": map[string]any{},
	}
	spec := loadTeamSpec("sess-1", cfg)
	if spec == nil {
		t.Fatalf("spec should not be nil")
	}
}

// TestLoadTeamSpec_modelPool注入 多默认模型时填充 spec.ModelPool。
func TestLoadTeamSpec_modelPool注入(t *testing.T) {
	cfg := map[string]any{
		"team":  map[string]any{},
		"modes": map[string]any{"team": map[string]any{}},
		"models": map[string]any{
			"defaults": []any{
				map[string]any{
					"model_client_config": map[string]any{
						"model_name": "m1", "api_key": "k1", "api_base": "https://1",
						"client_provider": "OpenAI", "timeout": 60,
					},
					"model_config_obj": map[string]any{"temperature": 0.9},
				},
				map[string]any{
					"model_client_config": map[string]any{
						"model_name": "m2", "api_key": "k2", "api_base": "https://2",
						"client_provider": "OpenAI",
					},
				},
			},
		},
	}
	spec := loadTeamSpec("sess-2", cfg)
	if spec == nil {
		t.Fatalf("spec should not be nil")
	}
	if len(spec.ModelPool) != 2 {
		t.Fatalf("ModelPool = %d, want 2", len(spec.ModelPool))
	}
	if spec.ModelPoolStrategy != "by_model_name" {
		t.Fatalf("ModelPoolStrategy = %q", spec.ModelPoolStrategy)
	}
	first := spec.ModelPool[0]
	if first.ModelName != "m1" || first.APIKey != "k1" || first.APIBaseURL != "https://1" || first.APIProvider != "OpenAI" {
		t.Fatalf("first pool entry mismatch: %+v", first)
	}
	clientMeta, _ := first.Metadata["client"].(map[string]any)
	if clientMeta == nil || clientMeta["timeout"] != 60.0 {
		t.Fatalf("metadata.client timeout missing: %v", first.Metadata)
	}
	if _, hasModelName := clientMeta["model_name"]; hasModelName {
		t.Fatalf("metadata.client should exclude model_name")
	}
	requestMeta, _ := first.Metadata["request"].(map[string]any)
	if requestMeta == nil || requestMeta["temperature"] != 0.9 {
		t.Fatalf("metadata.request temperature missing: %v", first.Metadata)
	}
}

// TestLoadTeamSpec_单模型不注入pool。
func TestLoadTeamSpec_单模型不注入pool(t *testing.T) {
	cfg := map[string]any{
		"team":  map[string]any{},
		"modes": map[string]any{"team": map[string]any{}},
		"models": map[string]any{
			"default": map[string]any{
				"model_client_config": map[string]any{"model_name": "only"},
			},
		},
	}
	spec := loadTeamSpec("sess-3", cfg)
	if spec == nil {
		t.Fatalf("spec should not be nil")
	}
	if len(spec.ModelPool) != 0 {
		t.Fatalf("single model should not populate pool, got %d", len(spec.ModelPool))
	}
}

// TestNormalizeDistributedTransportFields_方法包装 委托纯函数。
func TestNormalizeDistributedTransportFields_方法包装(t *testing.T) {
	mgr := NewTeamManager()
	configBase := map[string]any{"team": map[string]any{
		"runtime":   map[string]any{"mode": "distributed", "role": "leader"},
		"transport": map[string]any{"type": "pyzmq"},
	}}
	teamCfg := map[string]any{"transport": map[string]any{"type": "pyzmq", "params": map[string]any{}}}
	result := mgr.NormalizeDistributedTransportFields(configBase, teamCfg)
	params := result["transport"].(map[string]any)["params"].(map[string]any)
	if params["direct_addr"] != "tcp://0.0.0.0:18555" {
		t.Fatalf("method wrapper should delegate to pure function")
	}
}

// TestParsePort_方法包装。
func TestParsePort_方法包装(t *testing.T) {
	mgr := NewTeamManager()
	if got := mgr.ParsePort("18600", 1, "f"); got != 18600 {
		t.Fatalf("ParsePort method = %d", got)
	}
}

// ──────────────────────────── Task 13：BuildAgentCustomizer 六步回填 ────────────────────────────

// fakeAbilityMgrForCustomizer customizer 测试用 AbilityManager（真实 Add/Remove/List 语义）。
type fakeAbilityMgrForCustomizer struct {
	abilities []cschema.Ability
}

func (f *fakeAbilityMgrForCustomizer) Add(a cschema.Ability) agentschema.AddAbilityResult {
	f.abilities = append(f.abilities, a)
	return agentschema.AddAbilityResult{Added: true}
}
func (f *fakeAbilityMgrForCustomizer) AddMany(a []cschema.Ability) []agentschema.AddAbilityResult {
	return nil
}
func (f *fakeAbilityMgrForCustomizer) Remove(name string) cschema.Ability {
	for i, a := range f.abilities {
		if a.AbilityName() == name {
			removed := f.abilities[i]
			f.abilities = append(f.abilities[:i], f.abilities[i+1:]...)
			return removed
		}
	}
	return nil
}
func (f *fakeAbilityMgrForCustomizer) RemoveMany(names []string) []cschema.Ability {
	var removed []cschema.Ability
	for _, n := range names {
		if r := f.Remove(n); r != nil {
			removed = append(removed, r)
		}
	}
	return removed
}
func (f *fakeAbilityMgrForCustomizer) Get(name string) cschema.Ability {
	for _, a := range f.abilities {
		if a.AbilityName() == name {
			return a
		}
	}
	return nil
}
func (f *fakeAbilityMgrForCustomizer) List() []cschema.Ability { return f.abilities }
func (f *fakeAbilityMgrForCustomizer) ListToolInfo(ctx context.Context, names []string, mcpServerName ...string) ([]cschema.ToolInfoInterface, error) {
	return nil, nil
}
func (f *fakeAbilityMgrForCustomizer) Execute(
	ctx context.Context,
	cbc *agentinterfaces.AgentCallbackContext,
	toolCalls []*llmschema.ToolCall,
	sess sessioninterfaces.SessionFacade,
	tag string,
) []agentschema.ExecuteResult {
	return nil
}
func (f *fakeAbilityMgrForCustomizer) SetContextEngine(ce ceinterface.ContextEngine) {}
func (f *fakeAbilityMgrForCustomizer) ReorderTools(orderedNames []string)             {}

// has 检查能力名是否存在（测试辅助）。
func (f *fakeAbilityMgrForCustomizer) has(name string) bool {
	return f.Get(name) != nil
}

// fakeDeepAgentForCustomizer customizer 测试用 DeepAgentInterface fake。
// 记录 AddRail 的 rail 列表 + 可配置 DeepConfig.Workspace / Card / AdapterMode。
type fakeDeepAgentForCustomizer struct {
	abilityMgr      *fakeAbilityMgrForCustomizer
	rails           []agentinterfaces.AgentRail
	deepConfig      *hschema.DeepAgentConfig
	card            *agentschema.AgentCard
	adapterMode     string
	projectDir      string
	setProjectDirOK string // 记录 SetUapswarmProjectDir 最后一次入参
}

func newCustomizerFakeAgent(name string) *fakeDeepAgentForCustomizer {
	return &fakeDeepAgentForCustomizer{
		abilityMgr: &fakeAbilityMgrForCustomizer{},
		card:       agentschema.NewAgentCard(),
	}
}

func (f *fakeDeepAgentForCustomizer) AbilityManager() agentinterfaces.AbilityManagerInterface {
	return f.abilityMgr
}
func (f *fakeDeepAgentForCustomizer) ReactAgent() *agents.ReActAgent { return nil }
func (f *fakeDeepAgentForCustomizer) Card() *agentschema.AgentCard    { return f.card }
func (f *fakeDeepAgentForCustomizer) SystemPromptBuilder() saprompt.SystemPromptBuilderInterface {
	return nil
}
func (f *fakeDeepAgentForCustomizer) LoopCoordinator() hinterfaces.LoopCoordinatorInterface {
	return nil
}
func (f *fakeDeepAgentForCustomizer) LoopController() controller.ControllerInterface {
	return nil
}
func (f *fakeDeepAgentForCustomizer) EventHandler() modules.EventHandler { return nil }
func (f *fakeDeepAgentForCustomizer) LoadState(sess sessioninterfaces.SessionFacade) *hschema.DeepAgentState {
	return &hschema.DeepAgentState{}
}
func (f *fakeDeepAgentForCustomizer) DeepConfig() *hschema.DeepAgentConfig { return f.deepConfig }
func (f *fakeDeepAgentForCustomizer) IsInvokeActive() bool                 { return false }
func (f *fakeDeepAgentForCustomizer) IsAutoInvokeScheduled() bool          { return false }
func (f *fakeDeepAgentForCustomizer) SetAutoInvokeScheduled(bool)          {}
func (f *fakeDeepAgentForCustomizer) ScheduleAutoInvokeOnSpawnDone(context.Context, string, float64) error {
	return nil
}
func (f *fakeDeepAgentForCustomizer) CreateSubagent(context.Context, string, string) (hinterfaces.DeepAgentInterface, error) {
	return nil, nil
}
func (f *fakeDeepAgentForCustomizer) Invoke(context.Context, map[string]any, ...agentinterfaces.AgentOption) (map[string]any, error) {
	return nil, nil
}
func (f *fakeDeepAgentForCustomizer) SwitchMode(sessioninterfaces.SessionFacade, string)       {}
func (f *fakeDeepAgentForCustomizer) RestoreModeAfterPlanExit(sessioninterfaces.SessionFacade) {}
func (f *fakeDeepAgentForCustomizer) GetPlanFilePath(sessioninterfaces.SessionFacade) string   { return "" }
func (f *fakeDeepAgentForCustomizer) SaveState(sessioninterfaces.SessionFacade, *hschema.DeepAgentState) {}
func (f *fakeDeepAgentForCustomizer) FindRailsByType(...reflect.Type) []agentinterfaces.AgentRail {
	return nil
}
func (f *fakeDeepAgentForCustomizer) AddRail(r agentinterfaces.AgentRail) { f.rails = append(f.rails, r) }
func (f *fakeDeepAgentForCustomizer) UnregisterRail(context.Context, agentinterfaces.AgentRail) error {
	return nil
}
func (f *fakeDeepAgentForCustomizer) InnerInvokeOverride() func(context.Context, map[string]any, ...agentinterfaces.AgentOption) (map[string]any, error) {
	return nil
}
func (f *fakeDeepAgentForCustomizer) UapswarmProjectDir() string   { return f.projectDir }
func (f *fakeDeepAgentForCustomizer) SetUapswarmProjectDir(d string) {
	f.setProjectDirOK = d
	f.projectDir = d
}
func (f *fakeDeepAgentForCustomizer) UapswarmCodeProjectDir() string     { return "" }
func (f *fakeDeepAgentForCustomizer) SetUapswarmCodeProjectDir(string)   {}
func (f *fakeDeepAgentForCustomizer) UapswarmAdapterMode() string        { return f.adapterMode }
func (f *fakeDeepAgentForCustomizer) SetUapswarmAdapterMode(mode string) { f.adapterMode = mode }

// 编译时验证
var _ hinterfaces.DeepAgentInterface = (*fakeDeepAgentForCustomizer)(nil)

// TestSetCodeMemberConfigurer_注入与调用。
func TestSetCodeMemberConfigurer_注入与调用(t *testing.T) {
	mgr := NewTeamManager()
	mgr.SetCodeMemberConfigurer(func(ctx context.Context, agent, parent hinterfaces.DeepAgentInterface,
		skillManager *skillruntime.SkillManager, memberName, role, sessionID, channelID, projectDir, runtimeLanguage string,
		forceEnglishRuntimePrompt bool) {
	})
	if mgr.codeMemberConfigurer == nil {
		t.Fatalf("configurer should be stored")
	}
}

// TestCustomizer_能力卡继承 白名单工具去重添加。
func TestCustomizer_能力卡继承(t *testing.T) {
	mgr := NewTeamManager()
	spec := &atschema.TeamAgentSpec{TeamName: "t"}
	spec.Agents = map[string]atschema.DeepAgentSpec{}

	parent := newCustomizerFakeAgent("parent")
	visionCard := &tool.ToolCard{}
	visionCard.Name = "vision"
	visionCard.ID = "pv1"
	parent.abilityMgr.abilities = append(parent.abilityMgr.abilities, visionCard)

	member := newCustomizerFakeAgent("member")
	memberWs := t.TempDir()
	member.deepConfig = &hschema.DeepAgentConfig{
		Workspace: &workspace.Workspace{RootPath: memberWs},
	}

	customizer := mgr.BuildAgentCustomizer(spec, parent, "sess", "req-1", "web", nil)
	if customizer == nil {
		t.Fatalf("customizer should not be nil")
	}
	if err := customizer(context.Background(), member, "member-1", "teammate"); err != nil {
		t.Fatalf("customizer failed: %v", err)
	}
	// vision 在白名单 → 应被继承
	if !member.abilityMgr.has("vision") {
		t.Fatalf("vision should be inherited")
	}
	// 隔离 UAPCLAW_DATA_DIR 防止技能同步写真实 ~/.uapclaw
	// （customizer 步骤 b 会向 memberWs/skills 写文件，team_ws 指向 TeamHome 也可能写入）
}
