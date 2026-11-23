package rails

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"reflect"

	cschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/controller"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/controller/modules"
	llm "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm"
	llmschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
	cb "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/runner/callback"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/agents"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/interfaces"
	saprompt "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/prompts"
	agentschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/schema"
	hinterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/rails"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/rails/skills"
	hschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/schema"
)

// ──────────────────────────── 测试替身 ────────────────────────────

// fakeRailsAgent 实现 BaseAgent 的最小替身（可挂真实 SystemPromptBuilder）。
type fakeRailsAgent struct {
	builder *saprompt.SystemPromptBuilder
}

func (f *fakeRailsAgent) Configure(_ context.Context, _ agentinterfaces.AgentConfig) error {
	return nil
}
func (f *fakeRailsAgent) Invoke(_ context.Context, _ map[string]any, _ ...agentinterfaces.AgentOption) (map[string]any, error) {
	return nil, nil
}
func (f *fakeRailsAgent) Stream(_ context.Context, _ map[string]any, _ ...agentinterfaces.AgentOption) (<-chan stream.Schema, error) {
	return nil, nil
}
func (f *fakeRailsAgent) Card() *agentschema.AgentCard {
	return &agentschema.AgentCard{BaseCard: *cschema.NewBaseCard()}
}
func (f *fakeRailsAgent) Config() agentinterfaces.AgentConfig                     { return nil }
func (f *fakeRailsAgent) AbilityManager() agentinterfaces.AbilityManagerInterface { return nil }
func (f *fakeRailsAgent) CallbackManager() *agentinterfaces.AgentCallbackManager  { return nil }
func (f *fakeRailsAgent) SystemPromptBuilder() saprompt.SystemPromptBuilderInterface {
	return f.builder
}
func (f *fakeRailsAgent) RegisterCallback(_ context.Context, _ agentinterfaces.AgentCallbackEvent, _ cb.PerAgentCallbackFunc, _ ...cb.CallbackOption) error {
	return nil
}
func (f *fakeRailsAgent) RegisterRail(_ context.Context, _ agentinterfaces.AgentRail, _ ...cb.CallbackOption) error {
	return nil
}
func (f *fakeRailsAgent) UnregisterRail(_ context.Context, _ agentinterfaces.AgentRail) error {
	return nil
}

// 编译时验证
var _ agentinterfaces.BaseAgent = (*fakeRailsAgent)(nil)

// newRailsTestAgent 创建带构建器的测试 agent。
func newRailsTestAgent() (*fakeRailsAgent, *saprompt.SystemPromptBuilder) {
	builder := saprompt.NewSystemPromptBuilder()
	return &fakeRailsAgent{builder: builder}, builder
}

// newModelCallCBC 创建模型调用回调上下文。
func newModelCallCBC(agent agentinterfaces.BaseAgent) *agentinterfaces.AgentCallbackContext {
	return agentinterfaces.NewAgentCallbackContext(agent, &agentinterfaces.ModelCallInputs{}, nil)
}

// fakeDeepAgentWithRails 实现 DeepAgentInterface 的最小替身（FindRailsByType 用）。
type fakeDeepAgentWithRails struct {
	*fakeRailsAgent
	hinterfaces.UnimplementedDeepAgentInteraction
	railsList []agentinterfaces.AgentRail
}

func (f *fakeDeepAgentWithRails) ReactAgent() *agents.ReActAgent { return nil }
func (f *fakeDeepAgentWithRails) LoopCoordinator() hinterfaces.LoopCoordinatorInterface {
	return nil
}
func (f *fakeDeepAgentWithRails) LoopController() controller.ControllerInterface { return nil }
func (f *fakeDeepAgentWithRails) EventHandler() modules.EventHandler             { return nil }
func (f *fakeDeepAgentWithRails) LoadState(_ sessioninterfaces.SessionFacade) *hschema.DeepAgentState {
	return &hschema.DeepAgentState{}
}
func (f *fakeDeepAgentWithRails) DeepConfig() *hschema.DeepAgentConfig { return nil }
func (f *fakeDeepAgentWithRails) IsInvokeActive() bool                 { return false }
func (f *fakeDeepAgentWithRails) IsAutoInvokeScheduled() bool          { return false }
func (f *fakeDeepAgentWithRails) SetAutoInvokeScheduled(bool)          {}
func (f *fakeDeepAgentWithRails) ScheduleAutoInvokeOnSpawnDone(context.Context, string, float64) error {
	return nil
}
func (f *fakeDeepAgentWithRails) CreateSubagent(context.Context, string, string) (hinterfaces.DeepAgentInterface, error) {
	return nil, nil
}
func (f *fakeDeepAgentWithRails) SwitchMode(sessioninterfaces.SessionFacade, string)       {}
func (f *fakeDeepAgentWithRails) RestoreModeAfterPlanExit(sessioninterfaces.SessionFacade) {}
func (f *fakeDeepAgentWithRails) GetPlanFilePath(sessioninterfaces.SessionFacade) string   { return "" }
func (f *fakeDeepAgentWithRails) SaveState(sessioninterfaces.SessionFacade, *hschema.DeepAgentState) {
}
func (f *fakeDeepAgentWithRails) FindRailsByType(railTypes ...reflect.Type) []agentinterfaces.AgentRail {
	var out []agentinterfaces.AgentRail
	for _, rail := range f.railsList {
		for _, t := range railTypes {
			if reflect.TypeOf(rail) == t {
				out = append(out, rail)
			}
		}
	}
	return out
}
func (f *fakeDeepAgentWithRails) AddRail(_ agentinterfaces.AgentRail) {}
func (f *fakeDeepAgentWithRails) InnerInvokeOverride() func(context.Context, map[string]any, ...agentinterfaces.AgentOption) (map[string]any, error) {
	return nil
}
func (f *fakeDeepAgentWithRails) UapswarmProjectDir() string       { return "" }
func (f *fakeDeepAgentWithRails) SetUapswarmProjectDir(string)     {}
func (f *fakeDeepAgentWithRails) UapswarmCodeProjectDir() string   { return "" }
func (f *fakeDeepAgentWithRails) SetUapswarmCodeProjectDir(string) {}
func (f *fakeDeepAgentWithRails) UapswarmAdapterMode() string      { return "" }
func (f *fakeDeepAgentWithRails) SetUapswarmAdapterMode(string)    {}
func (f *fakeDeepAgentWithRails) CancelRound(_ context.Context, _ string) (bool, error) {
	return false, nil
}
func (f *fakeDeepAgentWithRails) SendInput(_ context.Context, _ hschema.SendInputRequest) error {
	return nil
}
func (f *fakeDeepAgentWithRails) DetachOutput(_ context.Context, _ string, _ bool) {}

var _ hinterfaces.DeepAgentInterface = (*fakeDeepAgentWithRails)(nil)

// ──────────────────────────── storage_policy ────────────────────────────

// TestTeamSkillStoragePolicy_注入与移除 对齐 Python 全流程：init 捕 builder →
// before_model_call 注入双语节 → uninit 移除。
func TestTeamSkillStoragePolicy_注入与移除(t *testing.T) {
	agent, builder := newRailsTestAgent()
	rail := NewTeamSkillStoragePolicyRail("/global/skills", "/team/ws", "/team/ws/skills-visibility.json")
	require.NoError(t, rail.Init(context.Background(), agent))

	require.NoError(t, rail.BeforeModelCall(context.Background(), newModelCallCBC(agent)))
	sections := sectionsOf(builder)
	require.Len(t, sections, 1)
	assert.Equal(t, "team_skill_storage_policy", sections[0].Name)
	assert.Equal(t, 39, sections[0].Priority)
	cn := sections[0].Content["cn"]
	assert.Contains(t, cn, "# Team Skill 存储规则")
	assert.Contains(t, cn, "- 全局公共技能目录：`/global/skills`")
	assert.Contains(t, cn, "- 新 skill 的入口文件必须是：`/global/skills/<skill-name>/SKILL.md`")
	assert.Contains(t, cn, "- team 共享工作区：`/team/ws`")
	assert.Contains(t, cn, "- team skill 可见性声明文件：`/team/ws/skills-visibility.json`")
	assert.Contains(t, cn, "skills-visibility.json")
	en := sections[0].Content["en"]
	assert.Contains(t, en, "# Team Skill Storage Policy")
	assert.Contains(t, en, "- Team shared workspace: `/team/ws`")

	// builder 缺省时不注入（Python: system_prompt_builder is None → return）
	emptyRail := NewTeamSkillStoragePolicyRail("/g", "", "")
	require.NoError(t, emptyRail.BeforeModelCall(context.Background(), newModelCallCBC(&fakeRailsAgent{})))

	require.NoError(t, rail.Uninit(agent))
	assert.Empty(t, sectionsOf(builder))
	assert.Nil(t, rail.systemPromptBuilder)
}

// ──────────────────────────── library_reload ────────────────────────────

// TestTeamSkillLibraryReload_注入钩子触发 对齐 Python：write_file 触碰技能库根
// 时调用注入钩子；非写入工具/库外路径不触发。
func TestTeamSkillLibraryReload_注入钩子触发(t *testing.T) {
	skillsDir := t.TempDir()
	calls := 0
	agent, _ := newRailsTestAgent()
	rail := NewTeamSkillLibraryReloadRail(skillsDir, func() error { calls++; return nil })

	// 命中：write_file + 库内路径
	cbc := agentinterfaces.NewAgentCallbackContext(agent, &agentinterfaces.ToolCallInputs{
		ToolName: "write_file",
		ToolArgs: map[string]any{"file_path": filepath.Join(skillsDir, "a", "SKILL.md")},
	}, nil)
	require.NoError(t, rail.AfterToolCall(context.Background(), cbc))
	assert.Equal(t, 1, calls)

	// 非写入工具不触发
	cbc2 := agentinterfaces.NewAgentCallbackContext(agent, &agentinterfaces.ToolCallInputs{
		ToolName: "read_file",
		ToolArgs: map[string]any{"file_path": filepath.Join(skillsDir, "a", "SKILL.md")},
	}, nil)
	require.NoError(t, rail.AfterToolCall(context.Background(), cbc2))
	assert.Equal(t, 1, calls)

	// 库外路径不触发
	cbc3 := agentinterfaces.NewAgentCallbackContext(agent, &agentinterfaces.ToolCallInputs{
		ToolName: "edit_file",
		ToolArgs: map[string]any{"file_path": filepath.Join(t.TempDir(), "x.md")},
	}, nil)
	require.NoError(t, rail.AfterToolCall(context.Background(), cbc3))
	assert.Equal(t, 1, calls)

	// path 键回退（Python: args.get("path")）
	cbc4 := agentinterfaces.NewAgentCallbackContext(agent, &agentinterfaces.ToolCallInputs{
		ToolName: "write_file",
		ToolArgs: map[string]any{"path": skillsDir},
	}, nil)
	require.NoError(t, rail.AfterToolCall(context.Background(), cbc4))
	assert.Equal(t, 2, calls)
}

// TestTeamSkillLibraryReload_默认路径重载SkillUseRail 对齐 Python 默认钩子：
// reload_agent_skill_views 枚举 SkillUseRail 并逐个 ReloadSkills。
func TestTeamSkillLibraryReload_默认路径重载SkillUseRail(t *testing.T) {
	skillsDir := t.TempDir()
	skillRail := skills.NewSkillUseRail([]string{skillsDir})
	agent := &fakeDeepAgentWithRails{railsList: []agentinterfaces.AgentRail{skillRail}}
	rail := NewTeamSkillLibraryReloadRail(skillsDir, nil)

	cbc := agentinterfaces.NewAgentCallbackContext(agent, &agentinterfaces.ToolCallInputs{
		ToolName: "write_file",
		ToolArgs: map[string]any{"file_path": filepath.Join(skillsDir, "b", "SKILL.md")},
	}, nil)
	require.NoError(t, rail.AfterToolCall(context.Background(), cbc))
}

// TestTeamSkillLibraryReload_钩子失败告警不上抛 对齐 Python except 分支。
func TestTeamSkillLibraryReload_钩子失败告警不上抛(t *testing.T) {
	agent, _ := newRailsTestAgent()
	rail := NewTeamSkillLibraryReloadRail(t.TempDir(), func() error { return errors.New("boom") })
	cbc := agentinterfaces.NewAgentCallbackContext(agent, &agentinterfaces.ToolCallInputs{
		ToolName: "write_file",
		ToolArgs: map[string]any{"file_path": "~"},
	}, nil)
	// ~ 不解析为库内路径（用户主目录 ≠ TempDir），且钩子不被调用
	require.NoError(t, rail.AfterToolCall(context.Background(), cbc))
}

// TestIsUnderGlobalSkillsDir_相对路径与符号链接 对齐 Python resolve 语义。
func TestIsUnderGlobalSkillsDir_相对路径与符号链接(t *testing.T) {
	skillsDir := t.TempDir()
	cwd, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(skillsDir))
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	rail := NewTeamSkillLibraryReloadRail(skillsDir, nil)
	assert.True(t, rail.isUnderGlobalSkillsDir("a/SKILL.md"))

	// symlink 指向库内路径应判定命中
	link := filepath.Join(t.TempDir(), "link.md")
	require.NoError(t, os.Symlink(filepath.Join(skillsDir, "a.md"), link))
	assert.True(t, rail.isUnderGlobalSkillsDir(link))
}

// ──────────────────────────── permission_policy ────────────────────────────

// TestTeamPermissionPolicy_注入与空配置跳过 对齐 Python before_model_call 全流程。
func TestTeamPermissionPolicy_注入与空配置跳过(t *testing.T) {
	agent, builder := newRailsTestAgent()
	rail := NewTeamPermissionPolicyRail(map[string]any{
		"tools":    map[string]any{"bash": "allow"},
		"defaults": map[string]any{"*": "ask"},
	}, "cn")
	require.NoError(t, rail.Init(context.Background(), agent))

	require.NoError(t, rail.BeforeModelCall(context.Background(), newModelCallCBC(agent)))
	sections := sectionsOf(builder)
	require.Len(t, sections, 1)
	assert.Equal(t, "team_permission_policy", sections[0].Name)
	assert.Equal(t, 39, sections[0].Priority)
	assert.Contains(t, sections[0].Content["cn"], "# Teammate 权限规则")
	assert.Contains(t, sections[0].Content["cn"], "- bash: allow")

	// 空配置不注入（Python: permissions_text 为空 → return）
	agentE, builderE := newRailsTestAgent()
	emptyRail := NewTeamPermissionPolicyRail(map[string]any{}, "cn")
	require.NoError(t, emptyRail.Init(context.Background(), agentE))
	require.NoError(t, emptyRail.BeforeModelCall(context.Background(), newModelCallCBC(agentE)))
	assert.Empty(t, sectionsOf(builderE))

	// en 标题
	agentEn, builderEn := newRailsTestAgent()
	enRail := NewTeamPermissionPolicyRail(map[string]any{"tools": map[string]any{"bash": "allow"}}, "en")
	require.NoError(t, enRail.Init(context.Background(), agentEn))
	require.NoError(t, enRail.BeforeModelCall(context.Background(), newModelCallCBC(agentEn)))
	assert.Len(t, sectionsOf(builderEn), 1)

	require.NoError(t, rail.Uninit(agent))
	assert.Empty(t, sectionsOf(builder))
}

// ──────────────────────────── organization_model ────────────────────────────

// fakeOrgModelBackend 实现 OrganizationModelBackend 的替身。
type fakeOrgModelBackend struct {
	setting    TeamModelSetting
	hasSetting bool
	resolved   bool
	applied    []any
}

func (f *fakeOrgModelBackend) LoadTeamModel(teamID, sessionID string) (TeamModelSetting, bool) {
	return f.setting, f.hasSetting
}

func (f *fakeOrgModelBackend) ResolveTeamModel(setting TeamModelSetting) (*llm.Model, string, bool) {
	if !f.resolved {
		return nil, "", false
	}
	return &llm.Model{ModelConfig: &llmschema.ModelRequestConfig{ModelName: "resolved-model"}}, "payload-1", true
}

// fakeLLMAgent 支持读写 LLM 的测试 agent（对齐 Python set_llm/get_llm）。
type fakeLLMAgent struct {
	*fakeRailsAgent
	hinterfaces.UnimplementedDeepAgentInteraction
	model *llm.Model
}

func (f *fakeLLMAgent) SetLLM(model *llm.Model) { f.model = model }
func (f *fakeLLMAgent) GetLLM() (*llm.Model, error) {
	if f.model == nil {
		return nil, errors.New("no model")
	}
	return f.model, nil
}

// TestOrganizationModel_Init注入策略节 对齐 Python init：leader 注入双语策略节。
func TestOrganizationModel_Init注入策略节(t *testing.T) {
	agent, builder := newRailsTestAgent()
	rail := NewOrganizationModelRail("team-1", "sess-1", "leader", nil)
	require.NoError(t, rail.Init(context.Background(), agent))
	require.Len(t, sectionsOf(builder), 1)
	assert.Equal(t, "organization_team_model_policy", sectionsOf(builder)[0].Name)
	assert.Equal(t, 5, sectionsOf(builder)[0].Priority)
	assert.Contains(t, sectionsOf(builder)[0].Content["cn"], "org_set_team_model")

	// 非 leader 不注入
	agent2, builder2 := newRailsTestAgent()
	rail2 := NewOrganizationModelRail("team-1", "sess-1", "member", nil)
	require.NoError(t, rail2.Init(context.Background(), agent2))
	assert.Empty(t, sectionsOf(builder2))

	// uninit 移除
	require.NoError(t, rail.Uninit(agent))
	assert.Empty(t, sectionsOf(builder))
}

// TestOrganizationModel_BeforeModelCall_selection节与模型应用 对齐 Python 主流程。
func TestOrganizationModel_BeforeModelCall_selection节与模型应用(t *testing.T) {
	backend := &fakeOrgModelBackend{
		setting:    TeamModelSetting{ModelName: "m1", Selection: map[string]any{"model_name": "m1"}},
		hasSetting: true,
		resolved:   true,
	}
	agent := &fakeLLMAgent{fakeRailsAgent: func() *fakeRailsAgent { a, _ := newRailsTestAgent(); return a }()}
	rail := NewOrganizationModelRail("team-1", "sess-1", "leader", backend)
	require.NoError(t, rail.Init(context.Background(), agent))

	// 第一次调用：注入 selection 节 + 应用模型
	require.NoError(t, rail.BeforeModelCall(context.Background(), newModelCallCBC(agent)))
	assert.Equal(t, "resolved-model", agent.model.ModelConfig.ModelName)
	secs := sectionsOf(agent.builder)
	require.Len(t, secs, 2)
	assert.Equal(t, "organization_team_model_selection", secs[1].Name)
	assert.Contains(t, secs[1].Content["cn"], `"persistent_team_model":"m1"`)
	assert.Contains(t, secs[1].Content["cn"], "Already set for the entire Team")

	// 第二次调用：签名与活模型未变 → 不重复应用（Python: return）
	require.NoError(t, rail.BeforeModelCall(context.Background(), newModelCallCBC(agent)))
	assert.Len(t, backend.applied, 0)

	// 无设置时：builder 清 selection 节但不注入、不应用
	backend2 := &fakeOrgModelBackend{}
	agent3 := &fakeLLMAgent{fakeRailsAgent: func() *fakeRailsAgent { a, _ := newRailsTestAgent(); return a }()}
	rail3 := NewOrganizationModelRail("team-1", "sess-1", "leader", backend2)
	require.NoError(t, rail3.Init(context.Background(), agent3))
	require.NoError(t, rail3.BeforeModelCall(context.Background(), newModelCallCBC(agent3)))
	assert.Len(t, sectionsOf(agent3.builder), 1) // 仅 policy 节
}

// TestOrganizationModel_BeforeToolCall_持久模型禁止外部CLI 对齐 Python before_tool_call。
func TestOrganizationModel_BeforeToolCall_持久模型禁止外部CLI(t *testing.T) {
	backend := &fakeOrgModelBackend{setting: TeamModelSetting{ModelName: "m1"}, hasSetting: true}
	agent, _ := newRailsTestAgent()
	rail := NewOrganizationModelRail("team-1", "sess-1", "leader", backend)

	cbc := agentinterfaces.NewAgentCallbackContext(agent, &agentinterfaces.ToolCallInputs{
		ToolName: "spawn_external_cli",
		ToolCall: llmschema.NewToolCall("call-1", "spawn_external_cli", "{}"),
	}, nil)
	require.NoError(t, rail.BeforeToolCall(context.Background(), cbc))
	assert.Equal(t, true, mustExtra(t, cbc, "_skip_tool"))
	inputs := cbc.Inputs().(*agentinterfaces.ToolCallInputs)
	toolResult, _ := inputs.ToolResult.(map[string]any)
	assert.Equal(t, false, toolResult["success"])
	assert.Contains(t, toolResult["error"], "persistent native model selection")
	assert.NotNil(t, inputs.ToolMsg)

	// 非目标工具不拦截
	cbc2 := agentinterfaces.NewAgentCallbackContext(agent, &agentinterfaces.ToolCallInputs{
		ToolName: "write_file",
	}, nil)
	require.NoError(t, rail.BeforeToolCall(context.Background(), cbc2))
	_, skip := cbc2.GetExtra("_skip_tool")
	assert.False(t, skip)

	// 无持久设置不拦截
	backend2 := &fakeOrgModelBackend{}
	rail2 := NewOrganizationModelRail("team-1", "sess-1", "leader", backend2)
	cbc3 := agentinterfaces.NewAgentCallbackContext(agent, &agentinterfaces.ToolCallInputs{
		ToolName: "spawn_external_cli",
	}, nil)
	require.NoError(t, rail2.BeforeToolCall(context.Background(), cbc3))
	_, skip = cbc3.GetExtra("_skip_tool")
	assert.False(t, skip)
}

// TestOrganizationModel_AfterToolCall_名册富化 对齐 Python after_tool_call。
func TestOrganizationModel_AfterToolCall_名册富化(t *testing.T) {
	backend := &fakeOrgModelBackend{
		setting:    TeamModelSetting{ModelName: "m1", Selection: map[string]any{"model_name": "m1"}},
		hasSetting: true,
	}
	agent, _ := newRailsTestAgent()
	rail := NewOrganizationModelRail("team-1", "sess-1", "leader", backend)

	cbc := agentinterfaces.NewAgentCallbackContext(agent, &agentinterfaces.ToolCallInputs{
		ToolName: "org_view_organization",
		ToolResult: map[string]any{
			"success": true,
			"data": map[string]any{
				"owner_team_id": "owner-1",
				"leaders": []any{
					map[string]any{"team_id": "owner-1", "execution_model_name": "stale"},
					map[string]any{"team_id": "child-1"},
				},
			},
		},
		ToolMsg: llmschema.NewToolMessage("call-1", "raw"),
	}, nil)
	require.NoError(t, rail.AfterToolCall(context.Background(), cbc))
	inputs := cbc.Inputs().(*agentinterfaces.ToolCallInputs)
	data := inputs.ToolResult.(map[string]any)["data"].(map[string]any)
	leaders := data["leaders"].([]any)
	owner := leaders[0].(map[string]any)
	assert.Equal(t, true, owner["is_owner"])
	_, hasStale := owner["execution_model_name"]
	assert.False(t, hasStale)
	child := leaders[1].(map[string]any)
	assert.Equal(t, false, child["is_owner"])
	assert.Equal(t, "m1", child["execution_model_name"])
	assert.Contains(t, data["model_selection_guidance"], "Never call org_set_team_model")
	assert.Contains(t, inputs.ToolMsg.Content.Text(), "model_selection_guidance")

	// 非 org_view_organization 不处理
	cbc2 := agentinterfaces.NewAgentCallbackContext(agent, &agentinterfaces.ToolCallInputs{
		ToolName: "write_file",
	}, nil)
	require.NoError(t, rail.AfterToolCall(context.Background(), cbc2))
}

// ──────────────────────────── organization_availability ────────────────────────────

// fakeOrgAvailBackend 实现 OrganizationAvailabilityBackend 的替身。
type fakeOrgAvailBackend struct {
	status   map[string]any
	entryOK  bool
	metadata map[string]any
	leaders  []string
	// selfTeam 仅对该 team 返回运行时条目（对齐 Python pool.get 语义：其他团无条目）
	selfTeam string
}

func (f *fakeOrgAvailBackend) TeamPackageAvailability(teamID, agentGroupName string) map[string]any {
	out := map[string]any{}
	for k, v := range f.status {
		out[k] = v
	}
	return out
}

func (f *fakeOrgAvailBackend) RuntimeEntry(teamID string) (string, map[string]any, bool) {
	if teamID != f.selfTeam {
		return "", nil, false
	}
	return "sess-1", f.metadata, f.entryOK
}

func (f *fakeOrgAvailBackend) OrganizationLeaders() []string { return f.leaders }

// TestOrganizationAvailability_BeforeToolCall_不可用拦截 对齐 Python before_tool_call。
func TestOrganizationAvailability_BeforeToolCall_不可用拦截(t *testing.T) {
	backend := &fakeOrgAvailBackend{status: map[string]any{"available": false}}
	agent, _ := newRailsTestAgent()
	rail := NewOrganizationAvailabilityRail("team-1", "sess-1", backend)

	cbc := agentinterfaces.NewAgentCallbackContext(agent, &agentinterfaces.ToolCallInputs{
		ToolName: "org_delegate_task",
		ToolArgs: map[string]any{"to_team_id": "child-1"},
		ToolCall: llmschema.NewToolCall("call-1", "org_delegate_task", "{}"),
	}, nil)
	require.NoError(t, rail.BeforeToolCall(context.Background(), cbc))
	inputs := cbc.Inputs().(*agentinterfaces.ToolCallInputs)
	toolResult, _ := inputs.ToolResult.(map[string]any)
	assert.Equal(t, false, toolResult["success"])
	assert.Equal(t, true, mustExtra(t, cbc, "_skip_tool"))

	// available=true 不拦截
	backend.status = map[string]any{"available": true}
	cbc2 := agentinterfaces.NewAgentCallbackContext(agent, &agentinterfaces.ToolCallInputs{
		ToolName: "org_delegate_task",
		ToolArgs: map[string]any{"to_team_id": "child-1"},
	}, nil)
	require.NoError(t, rail.BeforeToolCall(context.Background(), cbc2))
	_, skip := cbc2.GetExtra("_skip_tool")
	assert.False(t, skip)

	// 非 target_fields 工具不处理
	cbc3 := agentinterfaces.NewAgentCallbackContext(agent, &agentinterfaces.ToolCallInputs{
		ToolName: "write_file",
		ToolArgs: map[string]any{"to_team_id": "child-1"},
	}, nil)
	require.NoError(t, rail.BeforeToolCall(context.Background(), cbc3))
}

// TestOrganizationAvailability_BeforeModelCall_快照注入 对齐 Python before_model_call。
func TestOrganizationAvailability_BeforeModelCall_快照注入(t *testing.T) {
	backend := &fakeOrgAvailBackend{
		status:   map[string]any{"available": false, "agent_group_name": "grp-1"},
		entryOK:  true,
		leaders:  []string{"child-1"},
		selfTeam: "team-1",
	}
	agent, builder := newRailsTestAgent()
	rail := NewOrganizationAvailabilityRail("team-1", "sess-1", backend)
	require.NoError(t, rail.Init(context.Background(), agent))

	require.NoError(t, rail.BeforeModelCall(context.Background(), newModelCallCBC(agent)))
	secs := sectionsOf(builder)
	require.Len(t, secs, 1)
	assert.Equal(t, "organization_package_availability", secs[0].Name)
	assert.Contains(t, secs[0].Content["en"], `"team_id":"child-1"`)
	assert.Contains(t, secs[0].Content["en"], "Current AgentGroup availability")

	// 无条目不注入
	backend.entryOK = false
	require.NoError(t, rail.BeforeModelCall(context.Background(), newModelCallCBC(agent)))
	assert.Empty(t, sectionsOf(builder))

	// 后端缺省：移除节后直接返回
	rail2 := NewOrganizationAvailabilityRail("team-1", "sess-1", nil)
	require.NoError(t, rail2.Init(context.Background(), agent))
	require.NoError(t, rail2.BeforeModelCall(context.Background(), newModelCallCBC(agent)))
	assert.Empty(t, sectionsOf(builder))
}

// TestOrganizationAvailability_团队可用性身份缓存 对齐 Python _team_availability。
func TestOrganizationAvailability_团队可用性身份缓存(t *testing.T) {
	backend := &fakeOrgAvailBackend{
		status:   map[string]any{"agent_group_name": "grp-1"}, // 无 available 键
		entryOK:  true,
		metadata: map[string]any{"agent_group_name": "grp-meta"},
	}
	backend.selfTeam = "team-1"
	rail := NewOrganizationAvailabilityRail("team-1", "sess-1", backend)

	// 已知专家身份 + 无 available 键 → 不可用（AGENT_GROUP_IDENTITY_UNKNOWN）
	status := rail.teamAvailability("child-1", "")
	assert.Equal(t, false, status["available"])
	assert.Equal(t, "AGENT_GROUP_IDENTITY_UNKNOWN", status["unavailable_reason"])

	// 未知身份但 status 携带 agent_group_name → 同样记录身份并判 UNKNOWN（对齐 Python：
	// resolved_group 取 status 的 agent_group_name 或入参 group，非空即记录）
	status2 := rail.teamAvailability("unknown-team", "")
	assert.Equal(t, false, status2["available"])

	// status 无 available 且无 agent_group_name、身份缓存无记录 → 透传（不构成卸载证据）
	backend.status = map[string]any{}
	status3 := rail.teamAvailability("brand-new", "")
	_, hasAvailable := status3["available"]
	assert.False(t, hasAvailable)

	// metadata 的 expert_team 标记保留身份
	backend.metadata = map[string]any{"expert_team": true, "agent_group_name": ""}
	backend.status = map[string]any{"agent_group_name": "grp-x"}
	status4 := rail.teamAvailability("expert-1", "")
	assert.Equal(t, "AGENT_GROUP_IDENTITY_UNKNOWN", status4["unavailable_reason"])
}

// TestOrganizationAvailability_AfterToolCall_名册富化 对齐 Python after_tool_call。
func TestOrganizationAvailability_AfterToolCall_名册富化(t *testing.T) {
	backend := &fakeOrgAvailBackend{status: map[string]any{"available": false}}
	agent, _ := newRailsTestAgent()
	rail := NewOrganizationAvailabilityRail("team-1", "sess-1", backend)

	cbc := agentinterfaces.NewAgentCallbackContext(agent, &agentinterfaces.ToolCallInputs{
		ToolName: "org_view_organization",
		ToolResult: map[string]any{
			"success": true,
			"data": map[string]any{
				"leaders": []any{
					map[string]any{"team_id": "child-1", "agent_group_name": "grp-1"},
				},
			},
		},
		ToolMsg: llmschema.NewToolMessage("call-1", "raw"),
	}, nil)
	require.NoError(t, rail.AfterToolCall(context.Background(), cbc))
	inputs := cbc.Inputs().(*agentinterfaces.ToolCallInputs)
	data := inputs.ToolResult.(map[string]any)["data"].(map[string]any)
	leader := data["leaders"].([]any)[0].(map[string]any)
	assert.Equal(t, false, leader["available"])
	assert.Contains(t, data["next_action"], "Membership is historical")
	assert.Contains(t, inputs.ToolMsg.Content.Text(), "next_action")
}

// ──────────────────────────── 助手 ────────────────────────────

// sectionsOf 读取构建器当前全部节（GetSection 语义聚合）。
func sectionsOf(builder saprompt.SystemPromptBuilderInterface) []saprompt.PromptSection {
	var out []saprompt.PromptSection
	for _, name := range []string{
		"team_skill_storage_policy", "team_permission_policy",
		"organization_team_model_policy", "organization_team_model_selection",
		"organization_package_availability",
	} {
		if sec := builder.GetSection(name); sec != nil {
			out = append(out, *sec)
		}
	}
	return out
}

// mustExtra 读取 extra 键并断言存在。
func mustExtra(t *testing.T, cbc *agentinterfaces.AgentCallbackContext, key string) any {
	t.Helper()
	v, ok := cbc.GetExtra(key)
	require.True(t, ok, "extra key %q 应存在", key)
	return v
}

// 防御：未使用导入检查
var _ = rails.NewDeepAgentRail
var _ = os.Getwd

// TestTeamRailsUninit_移除注入节 补齐各 rail Uninit 路径。
func TestTeamRailsUninit_移除注入节(t *testing.T) {
	agent, builder := newRailsTestAgent()
	avail := NewOrganizationAvailabilityRail("team-1", "sess-1", nil)
	require.NoError(t, avail.Init(context.Background(), agent))
	require.NoError(t, avail.Uninit(agent))
	assert.Nil(t, avail.builder)
	assert.Empty(t, avail.agentGroups)
	assert.Empty(t, sectionsOf(builder))
}

// TestTeamSkillLibraryReload_非DeepAgent告警 对齐 Python 枚举失败告警分支。
func TestTeamSkillLibraryReload_非DeepAgent告警(t *testing.T) {
	agent, _ := newRailsTestAgent() // BaseAgent 而非 DeepAgent
	assert.Equal(t, 0, ReloadAgentSkillViews(context.Background(), agent))
}

// TestExtractFilePath_空参数 对齐 Python args None → ""。
func TestExtractFilePath_空参数(t *testing.T) {
	assert.Equal(t, "", extractFilePath(nil))
	assert.Equal(t, "", extractFilePath(map[string]any{"file_path": 123}))
}

// TestExpandUser_波浪号 对齐 Python expanduser。
func TestExpandUser_波浪号(t *testing.T) {
	home, err := os.UserHomeDir()
	require.NoError(t, err)
	assert.Equal(t, home, expandUser("~"))
	assert.Equal(t, filepath.Join(home, "x"), expandUser("~/x"))
	assert.Equal(t, "/plain/path", expandUser("/plain/path"))
}

// TestOrganizationModel_Resolve失败与非SetLLMAgent 对齐 Python 异常路径。
func TestOrganizationModel_Resolve失败与非SetLLMAgent(t *testing.T) {
	// ResolveTeamModel 失败（ok=false）→ 不应用、不报错
	backend := &fakeOrgModelBackend{setting: TeamModelSetting{ModelName: "m1"}, hasSetting: true, resolved: false}
	agent, _ := newRailsTestAgent()
	rail := NewOrganizationModelRail("team-1", "sess-1", "leader", backend)
	require.NoError(t, rail.Init(context.Background(), agent))
	require.NoError(t, rail.BeforeModelCall(context.Background(), newModelCallCBC(agent)))
	assert.Nil(t, rail.model)

	// 非 SetLLM agent → 报错（Python: raise ValueError）
	backend2 := &fakeOrgModelBackend{hasSetting: true, resolved: true}
	agent2, _ := newRailsTestAgent()
	rail2 := NewOrganizationModelRail("team-1", "sess-1", "leader", backend2)
	require.NoError(t, rail2.Init(context.Background(), agent2))
	require.Error(t, rail2.BeforeModelCall(context.Background(), newModelCallCBC(agent2)))

	// 非 ToolCallInputs 输入 → 各钩子直接返回
	require.NoError(t, rail2.BeforeToolCall(context.Background(), newModelCallCBC(agent2)))
	require.NoError(t, rail2.AfterToolCall(context.Background(), newModelCallCBC(agent2)))
}

// TestOrganizationAvailability_AfterToolCall异常路径 对齐 Python except 分支。
func TestOrganizationAvailability_AfterToolCall异常路径(t *testing.T) {
	backend := &fakeOrgAvailBackend{status: map[string]any{"available": false}, selfTeam: "team-1"}
	agent, _ := newRailsTestAgent()
	rail := NewOrganizationAvailabilityRail("team-1", "sess-1", backend)
	require.NoError(t, rail.Init(context.Background(), agent))

	// 非 map 结果 → 直接返回
	cbc := agentinterfaces.NewAgentCallbackContext(agent, &agentinterfaces.ToolCallInputs{
		ToolName: "org_view_organization", ToolResult: "raw",
	}, nil)
	require.NoError(t, rail.AfterToolCall(context.Background(), cbc))

	// 序列化失败路径：data 含不可序列化值（channel）→ 告警返回
	cbc2 := agentinterfaces.NewAgentCallbackContext(agent, &agentinterfaces.ToolCallInputs{
		ToolName: "org_view_organization",
		ToolResult: map[string]any{
			"success": true,
			"data":    map[string]any{"leaders": []any{make(chan int)}},
		},
		ToolMsg: llmschema.NewToolMessage("call-1", "raw"),
	}, nil)
	require.NoError(t, rail.AfterToolCall(context.Background(), cbc2))

	// 工具执行异常 → 不处理
	cbc3 := agentinterfaces.NewAgentCallbackContext(agent, &agentinterfaces.ToolCallInputs{
		ToolName: "org_view_organization",
	}, nil)
	cbc3.SetException(assert.AnError)
	require.NoError(t, rail.AfterToolCall(context.Background(), cbc3))
}
