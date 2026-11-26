package team

import (
	"context"
	runtimeinheritance "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/team/runtimeinheritance"
	"reflect"
	"testing"

	configutil "github.com/uapclaw/uapclaw-go/internal/common/config"
	cschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
	ceinterface "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/interface"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/controller"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/controller/modules"
	llmschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/tool"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/agents"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/interfaces"
	saprompt "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/prompts"
	agentschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/schema"
	hinterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/interfaces"
	hschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/schema"
)

// TestToolWhitelist_39项 逐项对齐 Python TOOL_WHITELIST（16.4b 补录 skill_index 后 44 项）。
func TestToolWhitelist_39项(t *testing.T) {
	want := []string{
		"free_search", "fetch_webpage", "paid_search", "vision", "audio",
		"image_ocr", "visual_question_answering", "generate_image",
		"audio_transcription", "audio_question_answering", "audio_metadata",
		"video_understanding", "search_skill", "install_skill", "uninstall_skill",
		"user_todos", "get_user_location", "create_note", "search_notes", "modify_note",
		"create_calendar_event", "search_calendar_event", "search_contact",
		"search_photo_gallery", "upload_photo", "search_file", "upload_file",
		"call_phone", "send_message", "search_message", "create_alarm", "search_alarms",
		"modify_alarm", "delete_alarm", "xiaoyi_collection", "image_reading",
		"xiaoyi_gui_agent", "web_free_search", "web_fetch_webpage", "web_paid_search",
		"skill_toolkit", "enter_worktree", "exit_worktree", "acp_chat",
		// 16.4b 补录：Python TOOL_WHITELIST :127 "skill_index"
		"skill_index",
	}
	if len(toolWhitelist) != len(want) {
		t.Fatalf("toolWhitelist size = %d, want %d", len(toolWhitelist), len(want))
	}
	for _, name := range want {
		if !toolWhitelist[name] {
			t.Errorf("toolWhitelist missing %q", name)
		}
	}
}

// TestRailWhitelist_13项 对齐 Python RAIL_WHITELIST（Go 实际类型名适配）。
// T-03: FileSystemRail 死键已删除（Go 无对应反射类型名永不匹配，Python 侧同为死键）。
func TestRailWhitelist_13项(t *testing.T) {
	// 13 项：Python 名 SecurityRail → Go 实际类型 SafetyPromptRail（类型别名反射名）。
	want := []string{
		"RuntimePromptRail", "ResponsePromptRail", "JiuClawStreamEventRail",
		"TaskPlanningRail", "SafetyPromptRail", "HeartbeatRail", "AvatarPromptRail",
		"SysOperationRail", "TeamSkillEvolutionRail",
		"TeamSkillCreateRail", "SkillEvolutionRail", "TeamWorkspaceReportPathRail",
		"ContextProcessorRail",
	}
	if len(railWhitelist) != len(want) {
		t.Fatalf("railWhitelist size = %d, want %d", len(railWhitelist), len(want))
	}
	for _, name := range want {
		if !railWhitelist[name] {
			t.Errorf("railWhitelist missing %q", name)
		}
	}
}

// TestSkillEvolutionEnabled_单一开关 react.evolution.skill_evolution
// （common/config.py:335-337，strictly True 才启用）。
func TestSkillEvolutionEnabled_单一开关(t *testing.T) {
	if configutil.SkillEvolutionEnabled(nil) {
		t.Fatalf("nil config default false")
	}
	if configutil.SkillEvolutionEnabled(map[string]any{}) {
		t.Fatalf("empty config default false")
	}
	cfg := map[string]any{"react": map[string]any{"evolution": map[string]any{"skill_evolution": true}}}
	if !configutil.SkillEvolutionEnabled(cfg) {
		t.Fatalf("react.evolution.skill_evolution=true should enable")
	}
	if configutil.SkillEvolutionEnabled(map[string]any{
		"react": map[string]any{"evolution": map[string]any{"skill_evolution": "true"}},
	}) {
		t.Fatalf("string true should not enable (Python is True)")
	}
}

// TestGetContextEngineEnabled_默认true。
func TestGetContextEngineEnabled_默认true(t *testing.T) {
	if !runtimeinheritance.GetContextEngineEnabled(nil) {
		t.Fatalf("nil config default true")
	}
	if !runtimeinheritance.GetContextEngineEnabled(map[string]any{}) {
		t.Fatalf("empty config default true")
	}
	cfg := map[string]any{"react": map[string]any{"context_engine_config": map[string]any{"enabled": false}}}
	if runtimeinheritance.GetContextEngineEnabled(cfg) {
		t.Fatalf("explicit false should disable")
	}
}

// TestGetDefaultModelName_读取modelsDefault。
func TestGetDefaultModelName_读取modelsDefault(t *testing.T) {
	cfg := map[string]any{"models": map[string]any{
		"default": map[string]any{"model_client_config": map[string]any{"model_name": "qwen-max"}},
	}}
	if got := GetDefaultModelName(cfg); got != "qwen-max" {
		t.Fatalf("got %q, want qwen-max", got)
	}
	if got := GetDefaultModelName(map[string]any{}); got != "gpt-4" {
		t.Fatalf("default fallback = %q, want gpt-4", got)
	}
}

// TestResolveModelConfig_defaults列表isDefault优先。
func TestResolveModelConfig_defaults列表isDefault优先(t *testing.T) {
	cfg := map[string]any{"models": map[string]any{
		"defaults": []any{
			map[string]any{"model_client_config": map[string]any{"model_name": "first"}},
			map[string]any{"is_default": true, "model_client_config": map[string]any{"model_name": "second", "api_key": "k"}},
		},
	}}
	mcc, mco, name := runtimeinheritance.ResolveModelConfig(cfg)
	if name != "second" {
		t.Fatalf("is_default entry should win, got %q", name)
	}
	if mcc["api_key"] != "k" {
		t.Fatalf("mcc mismatch: %v", mcc)
	}
	_ = mco
}

// TestResolveModelConfig_无isDefault取第一个。
func TestResolveModelConfig_无isDefault取第一个(t *testing.T) {
	cfg := map[string]any{"models": map[string]any{
		"defaults": []any{
			map[string]any{"model_client_config": map[string]any{"model_name": "first"}},
			map[string]any{"model_client_config": map[string]any{"model_name": "second"}},
		},
	}}
	_, _, name := runtimeinheritance.ResolveModelConfig(cfg)
	if name != "first" {
		t.Fatalf("first entry fallback = %q, want first", name)
	}
}

// TestResolveModelConfig_旧格式default与react回退。
func TestResolveModelConfig_旧格式default与react回退(t *testing.T) {
	cfg := map[string]any{
		"models": map[string]any{"default": map[string]any{
			"model_client_config": map[string]any{"model_name": "from-default"},
		}},
		"react": map[string]any{"model_name": "from-react"},
	}
	_, _, name := runtimeinheritance.ResolveModelConfig(cfg)
	if name != "from-default" {
		t.Fatalf("models.default should win over react, got %q", name)
	}

	cfg2 := map[string]any{
		"react": map[string]any{
			"model_name":          "from-react",
			"model_client_config": map[string]any{"api_key": "rk"},
			"model_config_obj":    map[string]any{"temperature": 0.5},
		},
	}
	mcc, mco, name2 := runtimeinheritance.ResolveModelConfig(cfg2)
	if name2 != "from-react" {
		t.Fatalf("react fallback = %q", name2)
	}
	if mcc["api_key"] != "rk" || mco["temperature"] != 0.5 {
		t.Fatalf("react fallback mcc/mco mismatch: %v %v", mcc, mco)
	}
}

// TestResolveModelConfig_最终回退gpt4。
func TestResolveModelConfig_最终回退gpt4(t *testing.T) {
	_, _, name := runtimeinheritance.ResolveModelConfig(map[string]any{})
	if name != "gpt-4" {
		t.Fatalf("final fallback = %q, want gpt-4", name)
	}
}

// TestBuildEvolutionLLM_合法配置 temperature 默认 0.95 + 显式覆盖。
func TestBuildEvolutionLLM_合法配置(t *testing.T) {
	cfg := map[string]any{"models": map[string]any{
		"default": map[string]any{
			"model_client_config": map[string]any{
				"model_name": "test-model", "api_key": "k", "api_base": "https://x", "verify_ssl": false,
				"client_provider": "OpenAI",
			},
			"model_config_obj": map[string]any{"temperature": 0.5},
		},
	}}
	model, name := BuildEvolutionLLM(cfg)
	if model == nil {
		t.Fatalf("model should be created")
	}
	if name != "test-model" {
		t.Fatalf("name = %q", name)
	}
	if model.ModelConfig.Temperature == nil || *model.ModelConfig.Temperature != 0.5 {
		t.Fatalf("temperature = %v, want 0.5", model.ModelConfig.Temperature)
	}
}

// TestBuildEvolutionLLM_默认temperature095。
func TestBuildEvolutionLLM_默认temperature095(t *testing.T) {
	cfg := map[string]any{"models": map[string]any{
		"default": map[string]any{
			"model_client_config": map[string]any{
				"model_name": "m", "api_key": "k", "api_base": "https://x", "verify_ssl": false,
				"client_provider": "OpenAI",
			},
		},
	}}
	model, _ := BuildEvolutionLLM(cfg)
	if model == nil {
		t.Fatalf("model should be created")
	}
	if model.ModelConfig.Temperature == nil || *model.ModelConfig.Temperature != 0.95 {
		t.Fatalf("default temperature = %v, want 0.95", model.ModelConfig.Temperature)
	}
}

// TestBuildEvolutionLLM_缺apiKey返回nil。
func TestBuildEvolutionLLM_缺apiKey返回nil(t *testing.T) {
	cfg := map[string]any{"models": map[string]any{
		"default": map[string]any{
			"model_client_config": map[string]any{
				"model_name": "m", "api_base": "https://x", "client_provider": "OpenAI",
			},
		},
	}}
	model, name := BuildEvolutionLLM(cfg)
	if model != nil {
		t.Fatalf("invalid config should return nil model")
	}
	if name != "m" {
		t.Fatalf("model_name should still be returned, got %q", name)
	}
}

// ──────────────────────────── Task 8 测试辅助 ────────────────────────────

// fakeAbilityMgrForFilter 测试用 AbilityManager。
type fakeAbilityMgrForFilter struct {
	abilities []cschema.Ability
}

// AddAbility 空实现（接口扩张后补齐，规则 10）。
func (f *fakeAbilityMgrForFilter) AddAbility(card *tool.ToolCard, _ tool.Tool) agentschema.AddAbilityResult {
	if card == nil {
		return agentschema.AddAbilityResult{Added: false, Reason: "nil_card"}
	}
	return agentschema.AddAbilityResult{Name: card.Name, Added: true}
}

func (f *fakeAbilityMgrForFilter) Add(a cschema.Ability) agentschema.AddAbilityResult {
	f.abilities = append(f.abilities, a)
	return agentschema.AddAbilityResult{Added: true}
}
func (f *fakeAbilityMgrForFilter) AddMany(a []cschema.Ability) []agentschema.AddAbilityResult {
	return nil
}
func (f *fakeAbilityMgrForFilter) Remove(name string) cschema.Ability {
	for i, a := range f.abilities {
		if a.AbilityName() == name {
			removed := f.abilities[i]
			f.abilities = append(f.abilities[:i], f.abilities[i+1:]...)
			return removed
		}
	}
	return nil
}
func (f *fakeAbilityMgrForFilter) RemoveMany(names []string) []cschema.Ability {
	var removed []cschema.Ability
	for _, n := range names {
		if r := f.Remove(n); r != nil {
			removed = append(removed, r)
		}
	}
	return removed
}
func (f *fakeAbilityMgrForFilter) Get(name string) cschema.Ability {
	for _, a := range f.abilities {
		if a.AbilityName() == name {
			return a
		}
	}
	return nil
}
func (f *fakeAbilityMgrForFilter) List() []cschema.Ability { return f.abilities }
func (f *fakeAbilityMgrForFilter) ListToolInfo(ctx context.Context, names []string, mcpServerName ...string) ([]cschema.ToolInfoInterface, error) {
	return nil, nil
}
func (f *fakeAbilityMgrForFilter) Execute(
	ctx context.Context,
	cbc *agentinterfaces.AgentCallbackContext,
	toolCalls []*llmschema.ToolCall,
	sess sessioninterfaces.SessionFacade,
	tag string,
	parallelToolCalls bool,
) []agentschema.ExecuteResult {
	return nil
}
func (f *fakeAbilityMgrForFilter) SetContextEngine(ce ceinterface.ContextEngine) {}
func (f *fakeAbilityMgrForFilter) ReorderTools(orderedNames []string)            {}

// fakeDeepAgentForInheritance 最小 DeepAgentInterface（仅 AbilityManager 路径真实，其余空桩）。
type fakeDeepAgentForInheritance struct {
	// hinterfaces.UnimplementedDeepAgentInteraction 交互方法空实现（9.87）
	hinterfaces.UnimplementedDeepAgentInteraction
	abilityMgr *fakeAbilityMgrForFilter
}

func newFakeDeepAgentForInheritance() *fakeDeepAgentForInheritance {
	whiteListed := &tool.ToolCard{}
	whiteListed.Name = "vision"
	whiteListed.ID = "v1"
	outside := &tool.ToolCard{}
	outside.Name = "my_custom_tool"
	outside.ID = "c1"
	return &fakeDeepAgentForInheritance{
		abilityMgr: &fakeAbilityMgrForFilter{abilities: []cschema.Ability{whiteListed, outside}},
	}
}

func (f *fakeDeepAgentForInheritance) AbilityManager() agentinterfaces.AbilityManagerInterface {
	return f.abilityMgr
}

// 其余 DeepAgentInterface 空桩（仅 AbilityManager 路径真实）
func (f *fakeDeepAgentForInheritance) ReactAgent() *agents.ReActAgent { return nil }
func (f *fakeDeepAgentForInheritance) Card() *agentschema.AgentCard   { return nil }
func (f *fakeDeepAgentForInheritance) SystemPromptBuilder() saprompt.SystemPromptBuilderInterface {
	return nil
}
func (f *fakeDeepAgentForInheritance) LoopCoordinator() hinterfaces.LoopCoordinatorInterface {
	return nil
}
func (f *fakeDeepAgentForInheritance) LoopController() controller.ControllerInterface {
	return nil
}
func (f *fakeDeepAgentForInheritance) EventHandler() modules.EventHandler { return nil }
func (f *fakeDeepAgentForInheritance) LoadState(sess sessioninterfaces.SessionFacade) *hschema.DeepAgentState {
	return &hschema.DeepAgentState{}
}
func (f *fakeDeepAgentForInheritance) DeepConfig() *hschema.DeepAgentConfig { return nil }
func (f *fakeDeepAgentForInheritance) IsInvokeActive() bool                 { return false }
func (f *fakeDeepAgentForInheritance) IsAutoInvokeScheduled() bool          { return false }
func (f *fakeDeepAgentForInheritance) SetAutoInvokeScheduled(bool)          {}
func (f *fakeDeepAgentForInheritance) ScheduleAutoInvokeOnSpawnDone(context.Context, string, float64) error {
	return nil
}
func (f *fakeDeepAgentForInheritance) CreateSubagent(context.Context, string, string) (hinterfaces.DeepAgentInterface, error) {
	return nil, nil
}
func (f *fakeDeepAgentForInheritance) Invoke(context.Context, map[string]any, ...agentinterfaces.AgentOption) (map[string]any, error) {
	return nil, nil
}
func (f *fakeDeepAgentForInheritance) SwitchMode(sessioninterfaces.SessionFacade, string)       {}
func (f *fakeDeepAgentForInheritance) RestoreModeAfterPlanExit(sessioninterfaces.SessionFacade) {}
func (f *fakeDeepAgentForInheritance) GetPlanFilePath(sessioninterfaces.SessionFacade) string {
	return ""
}
func (f *fakeDeepAgentForInheritance) SaveState(sessioninterfaces.SessionFacade, *hschema.DeepAgentState) {
}
func (f *fakeDeepAgentForInheritance) FindRailsByType(...reflect.Type) []agentinterfaces.AgentRail {
	return nil
}
func (f *fakeDeepAgentForInheritance) AddRail(agentinterfaces.AgentRail) {}
func (f *fakeDeepAgentForInheritance) UnregisterRail(context.Context, agentinterfaces.AgentRail) error {
	return nil
}
func (f *fakeDeepAgentForInheritance) InnerInvokeOverride() func(context.Context, map[string]any, ...agentinterfaces.AgentOption) (map[string]any, error) {
	return nil
}
func (f *fakeDeepAgentForInheritance) UapswarmProjectDir() string       { return "" }
func (f *fakeDeepAgentForInheritance) SetUapswarmProjectDir(string)     {}
func (f *fakeDeepAgentForInheritance) UapswarmCodeProjectDir() string   { return "" }
func (f *fakeDeepAgentForInheritance) SetUapswarmCodeProjectDir(string) {}
func (f *fakeDeepAgentForInheritance) UapswarmAdapterMode() string      { return "" }
func (f *fakeDeepAgentForInheritance) SetUapswarmAdapterMode(string)    {}

// 编译时验证
var _ hinterfaces.DeepAgentInterface = (*fakeDeepAgentForInheritance)(nil)

// TestFilterInheritableAbilityCards_白名单过滤。
func TestFilterInheritableAbilityCards_白名单过滤(t *testing.T) {
	agent := newFakeDeepAgentForInheritance()
	inherited := FilterInheritableAbilityCards(agent)
	names := make([]string, 0)
	for _, card := range inherited {
		names = append(names, card.Name)
	}
	// fake 中 vision（白名单内）+ my_custom_tool（白名单外）
	if len(inherited) != 1 || inherited[0].Name != "vision" {
		t.Fatalf("filtered = %v, want [vision]", names)
	}
}

// TestBuildContextProcessorRail_四段配置 按序构造 ProcessorSpec。
func TestBuildContextProcessorRail_四段配置(t *testing.T) {
	cfg := map[string]any{"react": map[string]any{"context_engine_config": map[string]any{
		"message_summary_offloader_config": map[string]any{"threshold": 10},
		"dialogue_compressor_config":       map[string]any{"max_tokens": 100},
	}}}
	rail := BuildContextProcessorRail(cfg)
	if rail == nil {
		t.Fatalf("rail should not be nil")
	}
	// 内部 userProcessors 无法直接断言，通过日志路径覆盖；此处验证不 panic 且返回实例
}

// TestBuildContextProcessorRail_空配置 preset 实例。
func TestBuildContextProcessorRail_空配置(t *testing.T) {
	rail := BuildContextProcessorRail(nil)
	if rail == nil {
		t.Fatalf("rail should not be nil for nil config")
	}
}

// TestBuildSkillEvolutionRail_失败返回nil。
func TestBuildSkillEvolutionRail_失败返回nil(t *testing.T) {
	// 缺 api_key 的 config → BuildEvolutionLLM 失败 → 返回 nil
	rail := BuildSkillEvolutionRail("/tmp/x", map[string]any{})
	if rail != nil {
		t.Fatalf("invalid config should return nil rail")
	}
}

// TestBuildSkillEvolutionRail_合法配置 不绑 sink（teamID 空）。
func TestBuildSkillEvolutionRail_合法配置(t *testing.T) {
	cfg := map[string]any{
		"react": map[string]any{"evolution": map[string]any{"skill_evolution": true}},
		"models": map[string]any{
			"default": map[string]any{
				"model_client_config": map[string]any{
					"model_name": "m", "api_key": "k", "api_base": "https://x",
					"client_provider": "OpenAI", "verify_ssl": false,
				},
			},
		},
	}
	rail := BuildSkillEvolutionRail("/tmp/skills", cfg)
	if rail == nil {
		t.Fatalf("valid config should build rail")
	}
}

// TestBuildMemberRails_默认路径 8 个基础 rail 全部构造成功。
func TestBuildMemberRails_默认路径(t *testing.T) {
	// 无 team_ws_root/skills_dir/config：仅 8 个基础 rail + ContextProcessorRail（默认 enabled）
	rails := BuildMemberRails(nil, nil, nil)
	// 8 基础 + 1 ContextProcessor（runtimeinheritance.GetContextEngineEnabled(nil)=true）
	if len(rails) != 9 {
		names := railTypeNames(rails)
		t.Fatalf("rails count = %d, want 9, got %v", len(rails), names)
	}
}

// TestBuildMemberRails_带workspace_leader 全量 rail。
func TestBuildMemberRails_带workspace_leader(t *testing.T) {
	// leader + team_ws_root + skills_dir：8 基础 + WorkspaceReportPath + ContextProcessor
	root := "/tmp/team-ws"
	skills := "/tmp/team-ws/skills"
	teamID := "t1"
	memberRole := "leader"
	rails := BuildMemberRails(
		&MemberInfo{AgentName: "leader-agent", Role: &memberRole},
		&RuntimeInfo{Channel: "web", Language: "cn"},
		&TeamWorkspaceInfo{
			RootDir:   &root,
			SkillsDir: &skills,
			TeamID:    &teamID,
			Config:    map[string]any{},
		},
	)
	names := railTypeNames(rails)
	// TeamSkillEvolutionRail 依赖 BuildEvolutionLLM（config 无 models 段 → gpt-4 回退
	// 无 api_key → nil → 该 rail 跳过）；skill_create 默认 false → TeamSkillCreateRail 跳过
	hasWorkspace := false
	hasContext := false
	for _, n := range names {
		if n == "TeamWorkspaceReportPathRail" {
			hasWorkspace = true
		}
		if n == "ContextProcessorRail" {
			hasContext = true
		}
	}
	if !hasWorkspace {
		t.Fatalf("TeamWorkspaceReportPathRail missing: %v", names)
	}
	if !hasContext {
		t.Fatalf("ContextProcessorRail missing: %v", names)
	}
}

// TestBuildMemberRails_非leader挂SkillEvolution。
func TestBuildMemberRails_非leader挂SkillEvolution(t *testing.T) {
	// BuildSkillEvolutionRail 依赖合法 LLM config，否则跳过——此处仅验证不 panic
	skills := "/tmp/team-ws/skills"
	memberRole := "teammate"
	rails := BuildMemberRails(
		&MemberInfo{AgentName: "m1", Role: &memberRole},
		&RuntimeInfo{Channel: "web"},
		&TeamWorkspaceInfo{SkillsDir: &skills, Config: map[string]any{}},
	)
	if len(rails) == 0 {
		t.Fatalf("should have base rails")
	}
}

// TestBuildMemberRails_合法LLM时leader挂TeamSkillEvolution。
func TestBuildMemberRails_合法LLM时leader挂TeamSkillEvolution(t *testing.T) {
	cfg := map[string]any{
		"react": map[string]any{"evolution": map[string]any{"skill_evolution": true}},
		"models": map[string]any{
			"default": map[string]any{
				"model_client_config": map[string]any{
					"model_name": "m", "api_key": "k", "api_base": "https://x",
					"client_provider": "OpenAI", "verify_ssl": false,
				},
			},
		},
	}
	root := t.TempDir()
	skills := root + "/skills"
	teamID := "t1"
	memberRole := "leader"
	rails := BuildMemberRails(
		&MemberInfo{AgentName: "leader-agent", Role: &memberRole},
		&RuntimeInfo{Channel: "web", Language: "cn"},
		&TeamWorkspaceInfo{RootDir: &root, SkillsDir: &skills, TeamID: &teamID, Config: cfg},
	)
	names := railTypeNames(rails)
	found := false
	for _, n := range names {
		if n == "TeamSkillEvolutionRail" {
			found = true
		}
	}
	if !found {
		t.Fatalf("TeamSkillEvolutionRail missing with valid LLM config: %v", names)
	}
}

// railTypeNames 提取 rail 类型名列表。
func railTypeNames(rails []agentinterfaces.AgentRail) []string {
	names := make([]string, 0, len(rails))
	for _, r := range rails {
		names = append(names, reflect.TypeOf(r).Elem().Name())
	}
	return names
}

// TestBuildModelAnomalyDetectionRail_团队成员 未开启返回 nil，开启后构造成功。
func TestBuildModelAnomalyDetectionRail_团队成员(t *testing.T) {
	on := map[string]any{"execution_guard": map[string]any{
		"model_anomaly_detection_rail": map[string]any{"enabled": true},
	}}
	rail := BuildModelAnomalyDetectionRail(on)
	if rail == nil {
		t.Fatal("enabled=true 时应构建")
	}
	if rail.Priority() != 70 {
		t.Fatalf("priority 期望 70，实际 %d", rail.Priority())
	}

	if rail := BuildModelAnomalyDetectionRail(map[string]any{}); rail != nil {
		t.Fatal("缺省时不应构建")
	}
	if rail := BuildModelAnomalyDetectionRail(nil); rail != nil {
		t.Fatal("config 为 nil 时不应构建")
	}

	// tool_loop_compact 分支 + YAML/JSON 两类数值形态（int / int64 / float64）
	withCompact := map[string]any{"execution_guard": map[string]any{
		"model_anomaly_detection_rail": map[string]any{
			"enabled":             true,
			"max_retries":         int64(1),
			"repeat_window_chars": 2048.0,
			"tool_loop_compact": map[string]any{
				"enabled":               true,
				"consecutive_threshold": 5,
				"bailout_threshold":     2,
			},
		},
	}}
	if rail := BuildModelAnomalyDetectionRail(withCompact); rail == nil {
		t.Fatal("含 tool_loop_compact 配置时应构建")
	}

	// 参数非法（max_retries 为负）→ 构造失败返回 nil，不阻断团队装配
	bad := map[string]any{"execution_guard": map[string]any{
		"model_anomaly_detection_rail": map[string]any{"enabled": true, "max_retries": -1},
	}}
	if rail := BuildModelAnomalyDetectionRail(bad); rail != nil {
		t.Fatal("参数非法时应返回 nil")
	}
}
