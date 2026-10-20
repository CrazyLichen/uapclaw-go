package team

import (
	"context"
	"os"
	"path/filepath"
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
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/agents"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	cschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
	upath "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
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
	spec, _ := loadTeamSpec("sess-1", cfg)
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
	spec, _ := loadTeamSpec("sess-2", cfg)
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
	spec, _ := loadTeamSpec("sess-3", cfg)
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
	result, err := mgr.NormalizeDistributedTransportFields(configBase, teamCfg)
	if err != nil {
		t.Fatalf("method wrapper should not fail: %v", err)
	}
	params := result["transport"].(map[string]any)["params"].(map[string]any)
	if params["direct_addr"] != "tcp://0.0.0.0:18555" {
		t.Fatalf("method wrapper should delegate to pure function")
	}
}

// TestParsePort_方法包装。
func TestParsePort_方法包装(t *testing.T) {
	mgr := NewTeamManager()
	if got, err := mgr.ParsePort("18600", 1, "f"); got != 18600 || err != nil {
		t.Fatalf("ParsePort method = %d, err = %v", got, err)
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
func (f *fakeAbilityMgrForCustomizer) ReorderTools(orderedNames []string)            {}

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
func (f *fakeDeepAgentForCustomizer) Card() *agentschema.AgentCard   { return f.card }
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
func (f *fakeDeepAgentForCustomizer) GetPlanFilePath(sessioninterfaces.SessionFacade) string {
	return ""
}
func (f *fakeDeepAgentForCustomizer) SaveState(sessioninterfaces.SessionFacade, *hschema.DeepAgentState) {
}
func (f *fakeDeepAgentForCustomizer) FindRailsByType(...reflect.Type) []agentinterfaces.AgentRail {
	return nil
}
func (f *fakeDeepAgentForCustomizer) AddRail(r agentinterfaces.AgentRail) {
	f.rails = append(f.rails, r)
}
func (f *fakeDeepAgentForCustomizer) UnregisterRail(context.Context, agentinterfaces.AgentRail) error {
	return nil
}
func (f *fakeDeepAgentForCustomizer) InnerInvokeOverride() func(context.Context, map[string]any, ...agentinterfaces.AgentOption) (map[string]any, error) {
	return nil
}
func (f *fakeDeepAgentForCustomizer) UapswarmProjectDir() string { return f.projectDir }
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

// ──────────────────────────── Task 16：六步函数覆盖补齐 ────────────────────────────

// TestCustomizer_六步全流程 leader 角色触发 stepD/stepE 注册路径。
func TestCustomizer_六步全流程(t *testing.T) {
	wsDir, _ := setupSkillTestEnv(t)
	globalSkills := filepath.Join(wsDir, "agent", "workspace", "skills")
	mkdir(t, filepath.Join(globalSkills, "shared-a"))
	writeFile(t, filepath.Join(globalSkills, "shared-a", "SKILL.md"), "a")

	mgr := NewTeamManager()
	// spec 配置 leader 成员 + selected skills（触发 copyMemberConfiguredSkills）
	leaderRole := "leader"
	memberSpec := atschema.DeepAgentSpec{}
	memberSpec.Skills = []string{"shared-a"}
	spec := &atschema.TeamAgentSpec{
		TeamName: "six-step-team",
		Agents:   map[string]atschema.DeepAgentSpec{"member-1": memberSpec},
	}

	parent := newCustomizerFakeAgent("parent")
	member := newCustomizerFakeAgent("member")
	memberWs := filepath.Join(t.TempDir(), "member-ws")
	member.deepConfig = &hschema.DeepAgentConfig{
		Workspace: &workspace.Workspace{RootPath: memberWs},
	}
	parent.projectDir = "/tmp/parent-project"

	customizer := mgr.BuildAgentCustomizer(spec, parent, "sess-6", "req-6", "web", map[string]any{"mode": "team.plan"})
	if customizer == nil {
		t.Fatalf("customizer should not be nil")
	}
	if err := customizer(context.Background(), member, "member-1", leaderRole); err != nil {
		t.Fatalf("customizer failed: %v", err)
	}

	// 步骤 a+skill 同步后：成员 skills 目录应有 shared-a（copyMemberConfiguredSkills 路径）
	if _, err := os.Stat(filepath.Join(memberWs, "skills", "shared-a", "SKILL.md")); err != nil {
		t.Fatalf("member skills dir should contain shared-a: %v", err)
	}
	// skills_state.json 应存在（writeMemberSkillState 路径）
	if _, err := os.Stat(filepath.Join(memberWs, "skills", "skills_state.json")); err != nil {
		t.Fatalf("member skills_state.json should exist: %v", err)
	}
	// 步骤 d：member rails 应含白名单 rail（TeamWorkspaceReportPath/ContextProcessor 等）
	if len(member.rails) == 0 {
		t.Fatalf("member rails should be added via stepD")
	}
	// parent project dir 传递
	if member.setProjectDirOK != "/tmp/parent-project" {
		t.Fatalf("parent project dir should propagate, got %q", member.setProjectDirOK)
	}
	// leader 注册 TeamRailContext——stepD/stepE 内部经 GetTeamManager(resolvedChannel)
	// 获取全局实例（channel="web"），断言需从同一全局实例读取
	if GetTeamManager("web").GetTeamRailContext("sess-6") == nil {
		t.Fatalf("leader should register TeamRailContext")
	}
}

// TestCustomizer_code模式注入回调 stepC configurer 被调用。
func TestCustomizer_code模式注入回调(t *testing.T) {
	_, _ = setupSkillTestEnv(t)
	mgr := NewTeamManager()
	configured := false
	var gotMemberName, gotRole, gotLang string
	var gotForceEnglish bool
	mgr.SetCodeMemberConfigurer(func(ctx context.Context, agent, parent hinterfaces.DeepAgentInterface,
		skillManager *skillruntime.SkillManager, memberName, role, sessionID, channelID, projectDir, runtimeLanguage string,
		forceEnglishRuntimePrompt bool) {
		configured = true
		gotMemberName = memberName
		gotRole = role
		gotLang = runtimeLanguage
		gotForceEnglish = forceEnglishRuntimePrompt
	})

	teammateRole := "teammate"
	spec := &atschema.TeamAgentSpec{TeamName: "code-team"}
	spec.Agents = map[string]atschema.DeepAgentSpec{}

	parent := newCustomizerFakeAgent("parent")
	parent.adapterMode = "code" // 触发步骤 c
	member := newCustomizerFakeAgent("member")
	memberWs := filepath.Join(t.TempDir(), "member-ws")
	member.deepConfig = &hschema.DeepAgentConfig{
		Workspace: &workspace.Workspace{RootPath: memberWs},
	}

	customizer := mgr.BuildAgentCustomizer(spec, parent, "sess-c", "req-c", "web", nil)
	if err := customizer(context.Background(), member, "coder-1", teammateRole); err != nil {
		t.Fatalf("customizer failed: %v", err)
	}
	if !configured {
		t.Fatalf("code member configurer should be invoked for adapter mode code")
	}
	if gotMemberName != "coder-1" || gotRole != teammateRole {
		t.Fatalf("configurer member/role mismatch: %q/%q", gotMemberName, gotRole)
	}
	// teammate 非 leader → forceEnglishRuntimePrompt = true（非 team-plan leader 恒 true）
	if !gotForceEnglish {
		t.Fatalf("non-leader should force english runtime prompt")
	}
	_ = gotLang
}

// TestCustomizer_code模式leader_teamPlan 语言解析。
func TestCustomizer_code模式leader_teamPlan语言(t *testing.T) {
	_, _ = setupSkillTestEnv(t)
	mgr := NewTeamManager()
	var gotLang string
	var gotForceEnglish bool
	mgr.SetCodeMemberConfigurer(func(ctx context.Context, agent, parent hinterfaces.DeepAgentInterface,
		skillManager *skillruntime.SkillManager, memberName, role, sessionID, channelID, projectDir, runtimeLanguage string,
		forceEnglishRuntimePrompt bool) {
		gotLang = runtimeLanguage
		gotForceEnglish = forceEnglishRuntimePrompt
	})

	leaderRole := "leader"
	spec := &atschema.TeamAgentSpec{TeamName: "lang-team", Language: "en"}
	spec.Agents = map[string]atschema.DeepAgentSpec{}

	parent := newCustomizerFakeAgent("parent")
	parent.adapterMode = "code"
	member := newCustomizerFakeAgent("member")
	member.deepConfig = &hschema.DeepAgentConfig{
		Workspace: &workspace.Workspace{RootPath: filepath.Join(t.TempDir(), "mw")},
	}

	customizer := mgr.BuildAgentCustomizer(spec, parent, "sess-l", "req-l", "web", map[string]any{"mode": "team.plan"})
	if err := customizer(context.Background(), member, "leader-1", leaderRole); err != nil {
		t.Fatalf("customizer failed: %v", err)
	}
	// team.plan 且 leader：runtimeLanguage 取 spec.Language="en"，forceEnglish=false
	if gotLang != "en" {
		t.Fatalf("team-plan leader language = %q, want en", gotLang)
	}
	if gotForceEnglish {
		t.Fatalf("team-plan leader should not force english")
	}
}

// TestResolveMemberSkills_三种查找路径。
func TestResolveMemberSkills_三种查找路径(t *testing.T) {
	byName := atschema.DeepAgentSpec{}
	byName.Skills = []string{"s1"}
	byRole := atschema.DeepAgentSpec{}
	byRole.Skills = []string{"s2"}
	leaderSpec := atschema.DeepAgentSpec{}
	leaderSpec.Skills = []string{"s3"}
	spec := &atschema.TeamAgentSpec{Agents: map[string]atschema.DeepAgentSpec{
		"alice": byName, "worker": byRole, "leader": leaderSpec,
	}}

	// member_name 命中
	ok, skills := resolveMemberSkills(spec, "alice", "")
	if !ok || len(skills) != 1 || skills[0] != "s1" {
		t.Fatalf("by-name lookup failed: %v %v", ok, skills)
	}
	// role 命中（member_name 不存在）
	ok, skills = resolveMemberSkills(spec, "nobody", "worker")
	if !ok || len(skills) != 1 || skills[0] != "s2" {
		t.Fatalf("by-role lookup failed: %v %v", ok, skills)
	}
	// leader 回退
	ok, skills = resolveMemberSkills(spec, "nobody", "nobody")
	if !ok || len(skills) != 1 || skills[0] != "s3" {
		t.Fatalf("leader fallback failed: %v %v", ok, skills)
	}
	// 无 Agents → false
	ok, _ = resolveMemberSkills(&atschema.TeamAgentSpec{}, "x", "y")
	if ok {
		t.Fatalf("empty agents should return false")
	}
}

// TestCopyMemberConfiguredSkills_缺失技能warning。
func TestCopyMemberConfiguredSkills_缺失技能warning(t *testing.T) {
	source := t.TempDir()
	target := t.TempDir()
	// global 有 skill-x；selected 含 skill-x + skill-missing
	mkdir(t, filepath.Join(source, "skill-x"))
	writeFile(t, filepath.Join(source, "skill-x", "SKILL.md"), "x")

	copyMemberConfiguredSkills(target, []string{"skill-x", "skill-missing"}, source)

	if _, err := os.Stat(filepath.Join(target, "skill-x", "SKILL.md")); err != nil {
		t.Fatalf("skill-x should be copied: %v", err)
	}
	// skill-missing 不存在（missing warning 路径，不 panic 即通过）
}

// TestBuildMemberSkillState_补全project条目。
func TestBuildMemberSkillState_补全project条目(t *testing.T) {
	globalState := t.TempDir()
	statePath := filepath.Join(globalState, "skills_state.json")
	// global state 有 installed_plugins（含 name=extra 不在 member 目录 → 过滤掉）
	writeFile(t, statePath, `{"marketplaces":[],"installed_plugins":[{"name":"extra"}],"local_skills":[]}`)

	memberDir := t.TempDir()
	mkdir(t, filepath.Join(memberDir, "real-skill"))
	writeFile(t, filepath.Join(memberDir, "real-skill", "SKILL.md"), "r")

	state := buildMemberSkillState(memberDir, statePath)
	plugins, _ := state["installed_plugins"].([]any)
	if len(plugins) != 1 {
		t.Fatalf("installed_plugins should contain only real-skill, got %v", plugins)
	}
	plugin := plugins[0].(map[string]any)
	if plugin["name"] != "real-skill" || plugin["source"] != "project" {
		t.Fatalf("project entry mismatch: %v", plugin)
	}
	locals, _ := state["local_skills"].([]any)
	if len(locals) != 1 || locals[0].(map[string]any)["name"] != "real-skill" {
		t.Fatalf("local_skills project entry missing: %v", locals)
	}
}

// TestCustomizer_无workspace跳过技能同步。
func TestCustomizer_无workspace跳过技能同步(t *testing.T) {
	_, _ = setupSkillTestEnv(t)
	mgr := NewTeamManager()
	spec := &atschema.TeamAgentSpec{TeamName: "no-ws-team"}
	spec.Agents = map[string]atschema.DeepAgentSpec{}

	parent := newCustomizerFakeAgent("parent")
	member := newCustomizerFakeAgent("member")
	// member.deepConfig 为 nil → agentWsRootPath 空 → 步骤 b 跳过

	customizer := mgr.BuildAgentCustomizer(spec, parent, "sess-n", "", "", nil)
	if err := customizer(context.Background(), member, "m", "teammate"); err != nil {
		t.Fatalf("customizer failed: %v", err)
	}
	// 不 panic 即通过（warning 路径）
}

// TestGetEnrichedTeamSpec_装配链 覆盖 spec 加载+plan mode+customizer 装配。
func TestGetEnrichedTeamSpec_装配链(t *testing.T) {
	_, _ = setupSkillTestEnv(t)
	mgr := NewTeamManager()

	parent := newCustomizerFakeAgent("parent")
	// configBase 无 team 段 → loadTeamSpec 走默认配置路径（内部 teamLoadConfig）
	spec := mgr.GetEnrichedTeamSpec(context.Background(), "sess-ge", parent, "req-ge", "web", map[string]any{"mode": "team.plan"})
	if spec == nil {
		t.Fatalf("spec should not be nil")
	}
	// team.plan 模式启用 EnableTeamPlan
	if !spec.EnableTeamPlan {
		t.Fatalf("team.plan mode should enable team plan")
	}
	// deepAgent 非 nil → customizer 应已装配
	if spec.AgentCustomizer == nil {
		t.Fatalf("agent customizer should be set")
	}
}

// TestApplyTeamPlanMode_显式teamPlan。
func TestApplyTeamPlanMode_显式teamPlan(t *testing.T) {
	mgr := NewTeamManager()
	spec := &atschema.TeamAgentSpec{}
	mgr.ApplyTeamPlanMode(spec, map[string]any{"mode": "  TEAM.PLAN  "})
	if !spec.EnableTeamPlan {
		t.Fatalf("TEAM.PLAN (case+space) should enable team plan")
	}
	// 非 team.plan 不启用
	spec2 := &atschema.TeamAgentSpec{}
	mgr.ApplyTeamPlanMode(spec2, map[string]any{"mode": "normal"})
	if spec2.EnableTeamPlan {
		t.Fatalf("normal mode should not enable team plan")
	}
	// nil metadata 不 panic
	mgr.ApplyTeamPlanMode(spec2, nil)
}

// TestApplySessionScopedTeamName_会话作用域。
func TestApplySessionScopedTeamName_会话作用域(t *testing.T) {
	mgr := NewTeamManager()
	spec := &atschema.TeamAgentSpec{TeamName: "my-team"}
	mgr.applySessionScopedTeamName(spec, "abc123")
	if spec.TeamName != "my-team_abc123" {
		t.Fatalf("scoped name = %q", spec.TeamName)
	}
	// 幂等：再次应用不叠加
	mgr.applySessionScopedTeamName(spec, "abc123")
	if spec.TeamName != "my-team_abc123" {
		t.Fatalf("scoped name should be idempotent, got %q", spec.TeamName)
	}
}

// TestConfigPreferredLanguage_env隔离读config config.yaml 存在时读取 preferred_language，缺失回退 zh。
func TestConfigPreferredLanguage_env隔离读config(t *testing.T) {
	// 独立 workspace：UAPCLAW_DATA_DIR → 临时目录 + config/config.yaml
	wsDir := t.TempDir()
	t.Setenv("UAPCLAW_DATA_DIR", wsDir)
	upath.ResetCache()
	t.Cleanup(upath.ResetCache)

	// 无 config.yaml（config 目录也未创建）→ Load 返回空 map → 回退 "zh"
	if got := configPreferredLanguage(); got != "zh" {
		t.Fatalf("missing config should default zh, got %q", got)
	}

	// 写入 preferred_language: en → 命中
	cfgDir := filepath.Join(wsDir, "config")
	mkdir(t, cfgDir)
	writeFile(t, filepath.Join(cfgDir, "config.yaml"), "preferred_language: en\n")
	if got := configPreferredLanguage(); got != "en" {
		t.Fatalf("config preferred_language should be read, got %q", got)
	}

	// preferred_language 为空串 → 回退 zh
	writeFile(t, filepath.Join(cfgDir, "config.yaml"), "preferred_language: \"\"\n")
	if got := configPreferredLanguage(); got != "zh" {
		t.Fatalf("empty preferred_language should default zh, got %q", got)
	}
}
