package team

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	atschema "github.com/uapclaw/uapclaw-go/internal/openjiuwen/agent_teams/schema"
	buildctx "github.com/uapclaw/uapclaw-go/internal/swarm/agents/swarm/buildctx"
	ceinterface "github.com/uapclaw/uapclaw-go/internal/openjiuwen/core/context_engine/interface"
	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/core/controller"
	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/core/controller/modules"
	llmschema "github.com/uapclaw/uapclaw-go/internal/openjiuwen/core/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/core/foundation/tool"
	hinterfaces "github.com/uapclaw/uapclaw-go/internal/openjiuwen/harness/interfaces"
	hschema "github.com/uapclaw/uapclaw-go/internal/openjiuwen/harness/schema"
	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/core/runner"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/openjiuwen/core/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/core/single_agent/agents"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/openjiuwen/core/single_agent/interfaces"
	saprompt "github.com/uapclaw/uapclaw-go/internal/openjiuwen/core/single_agent/prompts"
	agentschema "github.com/uapclaw/uapclaw-go/internal/openjiuwen/core/single_agent/schema"
	cschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
	upath "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
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

// ──────────────────────────── 11.10：RegisterMemberRuntimeTools cron 注册 ────────────────────────────

// TestRegisterMemberRuntimeTools_cron工具注册 测试 cron 工具经 CronRuntimeBridge 注册到
// 资源管理器与成员能力管理器（对齐 Python team_manager.py L484-517）。
func TestRegisterMemberRuntimeTools_cron工具注册(t *testing.T) {
	agent := newCustomizerFakeAgent("worker-1")
	agent.card = agentschema.NewAgentCard(agentschema.WithAgentID("member-a1"))
	agent.deepConfig = &hschema.DeepAgentConfig{Language: "en"}

	reqID := "req-100"
	chID := "web"
	mgr := &TeamManager{}

	mgr.RegisterMemberRuntimeTools(agent, "sess-team", &reqID, &chID, map[string]any{"k": "v"})

	// 能力管理器：cron 统一工具 + 7 个遗留兼容工具 = 8 张卡；
	// 10.6.24b 起 web 渠道三态默认启用 send_file → +1 = 9 张
	if got := len(agent.abilityMgr.abilities); got != 9 {
		t.Fatalf("registered abilities = %d, want 9 (1 unified cron + 7 legacy compat + 1 send_file)", got)
	}
	names := map[string]bool{}
	for _, a := range agent.abilityMgr.abilities {
		names[a.AbilityName()] = true
	}
	if !names["cron"] {
		t.Errorf("unified cron tool card missing, got %v", names)
	}
	for _, legacy := range []string{"cron_list_jobs", "cron_get_job", "cron_create_job", "cron_update_job", "cron_delete_job", "cron_toggle_job", "cron_preview_job"} {
		if !names[legacy] {
			t.Errorf("legacy cron tool card %q missing", legacy)
		}
	}
	if !names["send_file_to_user"] {
		t.Errorf("send_file tool card missing (web 默认三态启用), got %v", names)
	}

	// 资源管理器：8 个工具已注册（GetTool 按 card.ID 可取回）
	resourceMgr := runner.GetResourceMgr()
	for _, a := range agent.abilityMgr.abilities {
		tools, err := resourceMgr.GetTool([]string{a.AbilityID()})
		if err != nil || len(tools) == 0 {
			t.Errorf("tool %q not registered in resource manager (err=%v)", a.AbilityName(), err)
		}
	}
}

// TestRegisterMemberRuntimeTools_作用域覆盖 验证 team 场景 tool_scope 覆盖为 team_member_{agentID}。
// Python: SimpleNamespace(tool_scope=f"team_member_{agent_id or 'unknown'}")。
// 卡 Name 恒为工具名（"cron" 等），作用域体现在卡 ID：BuildToolCard 生成
// f"{toolID}_{agentID}"，toolID = "cron_" + scope → ID 前缀 cron_team_member_{agentID}。
func TestRegisterMemberRuntimeTools_作用域覆盖(t *testing.T) {
	agent := newCustomizerFakeAgent("worker-2")
	agent.card = agentschema.NewAgentCard(agentschema.WithAgentID("member-b2"))

	reqID := "req-101"
	chID := "feishu"
	mgr := &TeamManager{}
	mgr.RegisterMemberRuntimeTools(agent, "sess-scope", &reqID, &chID, nil)

	// 作用域覆盖后：统一工具卡 ID 前缀为 cron_team_member_member-b2_member-b2
	// （scope=team_member_member-b2，BuildToolCard 再拼 _agentID），替代默认 feishu_sess-scope
	unified := agent.abilityMgr.Get("cron")
	if unified == nil {
		t.Fatalf("unified cron card missing")
	}
	if prefix := "cron_team_member_member-b2_member-b2"; !strings.HasPrefix(unified.AbilityID(), prefix) {
		t.Errorf("unified card ID = %q, want prefix %q (tool_scope override)", unified.AbilityID(), prefix)
	}
	// agentID 兜底：Go NewAgentCard 默认构造自动生成 uuid（Python AgentCard 无默认 id
	// 时走 "unknown"），scope = team_member_{uuid}，卡 ID 前缀 cron_team_member_{uuid}
	agent2 := newCustomizerFakeAgent("worker-3")
	mgr.RegisterMemberRuntimeTools(agent2, "sess-scope2", nil, nil, nil)
	unified2 := agent2.abilityMgr.Get("cron")
	if unified2 == nil {
		t.Fatalf("unified cron card (fallback) missing")
	}
	// NewAgentCard 自动 uuid → scope 用该 uuid（非 unknown，Go 构造语义差异）
	if prefix := "cron_team_member_" + agent2.card.ID + "_"; !strings.HasPrefix(unified2.AbilityID(), prefix) {
		t.Errorf("fallback card ID = %q, want prefix %q (auto uuid card)", unified2.AbilityID(), prefix)
	}
	// card 为 nil 时才走 Python 语义的 unknown 兜底
	agent3 := newCustomizerFakeAgent("worker-4")
	agent3.card = nil
	mgr.RegisterMemberRuntimeTools(agent3, "sess-scope3", nil, nil, nil)
	unified3 := agent3.abilityMgr.Get("cron")
	if unified3 == nil {
		t.Fatalf("unified cron card (nil card) missing")
	}
	if prefix := "cron_team_member_unknown_"; !strings.HasPrefix(unified3.AbilityID(), prefix) {
		t.Errorf("nil card fallback ID = %q, want prefix %q", unified3.AbilityID(), prefix)
	}
}

// TestRegisterMemberRuntimeTools_幂等注册 验证重复注册不重复添加 resource manager 条目。
// Python: if not Runner.resource_mgr.get_tool(cron_tool.card.id): add_tool。
func TestRegisterMemberRuntimeTools_幂等注册(t *testing.T) {
	agent := newCustomizerFakeAgent("worker-4")
	agent.card = agentschema.NewAgentCard(agentschema.WithAgentID("member-c3"))

	reqID := "req-102"
	chID := "web"
	mgr := &TeamManager{}
	mgr.RegisterMemberRuntimeTools(agent, "sess-idem", &reqID, &chID, nil)
	mgr.RegisterMemberRuntimeTools(agent, "sess-idem", &reqID, &chID, nil)

	// abilityMgr 为 fake（Add 不查重），resourceMgr 走 GetTool 查重：
	// 第二轮 GetTool 命中已注册 → 不再 AddTool → ability 数量翻倍但 resource 不重复。
	// 10.6.24b：web 渠道三态默认启用 send_file——但 SendFile 分支先做前缀移除
	// 再注册（fake Remove 真实移除），故 send_file 卡数稳定为 1（不随轮次翻倍）
	if got := len(agent.abilityMgr.abilities); got != 17 {
		t.Fatalf("abilities after double registration = %d, want 17 (cron 翻倍 + send_file 前缀移除后稳定 1)", got)
	}
	resourceMgr := runner.GetResourceMgr()
	tools, err := resourceMgr.GetTool([]string{agent.abilityMgr.abilities[0].AbilityID()})
	if err != nil {
		t.Fatalf("GetTool error: %v", err)
	}
	if got := len(tools); got != 1 {
		t.Fatalf("resource manager tool entries = %d, want 1 (GetTool 查重生效)", got)
	}
}

// ──────────────────── 10.6.24b：RegisterMemberRuntimeTools SendFile 分支 ────────────────────

// TestRegisterMemberRuntimeTools_SendFile三态默认 注册分支三态：web 默认启用 / 其他渠道默认关 / 显式配置覆盖。
// Python: L524-532 send_file_enabled is None → (channel_id == "web")
func TestRegisterMemberRuntimeTools_SendFile三态默认(t *testing.T) {
	mgr := &TeamManager{}

	// web 渠道无配置 → 三态 None → 默认启用
	agentWeb := newCustomizerFakeAgent("sf-web")
	reqID, chWeb := "req-sf", "web"
	mgr.RegisterMemberRuntimeTools(agentWeb, "sess-sf", &reqID, &chWeb, nil)
	if !agentWeb.abilityMgr.has("send_file_to_user") {
		t.Error("web 渠道三态默认应注册 send_file")
	}

	// feishu 渠道无配置 → 三态 None → 默认关闭
	agentFeishu := newCustomizerFakeAgent("sf-feishu")
	chFeishu := "feishu"
	mgr.RegisterMemberRuntimeTools(agentFeishu, "sess-sf", &reqID, &chFeishu, nil)
	if agentFeishu.abilityMgr.has("send_file_to_user") {
		t.Error("feishu 渠道三态默认应跳过 send_file")
	}
}

// TestRegisterMemberRuntimeTools_SendFile缺参数 缺 requestID/channelID 跳过。
// Python: L519-522
func TestRegisterMemberRuntimeTools_SendFile缺参数(t *testing.T) {
	mgr := &TeamManager{}
	agent := newCustomizerFakeAgent("sf-nil")

	mgr.RegisterMemberRuntimeTools(agent, "sess", nil, nil, nil)
	if agent.abilityMgr.has("send_file_to_user") {
		t.Error("缺 requestID/channelID 应跳过 send_file 分支")
	}

	empty := ""
	mgr.RegisterMemberRuntimeTools(agent, "sess", &empty, &empty, nil)
	if agent.abilityMgr.has("send_file_to_user") {
		t.Error("空串 requestID/channelID 应跳过 send_file 分支")
	}
}

// TestRegisterMemberRuntimeTools_SendFile重复注册前缀移除 二次调用移除旧卡再注册。
// Python: L540-542 for existing in list(): startswith("send_file_to_user") → remove
func TestRegisterMemberRuntimeTools_SendFile重复注册前缀移除(t *testing.T) {
	mgr := &TeamManager{}
	agent := newCustomizerFakeAgent("sf-dup")
	agent.card = agentschema.NewAgentCard(agentschema.WithAgentID("member-sf"))

	reqID, chID := "req-1", "web"
	mgr.RegisterMemberRuntimeTools(agent, "sess-dup", &reqID, &chID, nil)
	first := agent.abilityMgr.abilities
	sfCount := 0
	for _, a := range first {
		if strings.HasPrefix(a.AbilityName(), "send_file_to_user") {
			sfCount++
		}
	}
	if sfCount != 1 {
		t.Fatalf("首轮 send_file 卡应 1 张, got %d", sfCount)
	}

	// 二次注册：前缀移除旧卡 → 仍 1 张（fake Add 无查重，验证移除生效）
	mgr.RegisterMemberRuntimeTools(agent, "sess-dup", &reqID, &chID, nil)
	sfCount2 := 0
	for _, a := range agent.abilityMgr.abilities {
		if strings.HasPrefix(a.AbilityName(), "send_file_to_user") {
			sfCount2++
		}
	}
	if sfCount2 != 1 {
		t.Errorf("二轮 send_file 卡应前缀移除后仍 1 张, got %d", sfCount2)
	}
}

// ──────────────────────────── 团队测试通用 DeepAgent/AbilityManager fake ────────────────────────────
// （原 Task13 customizer 测试桩，customizer 链删除后由 skill rail 测试族继续使用）

// ──────────────────────────── Task 13：BuildAgentCustomizer 六步回填 ────────────────────────────

// fakeAbilityMgrForCustomizer customizer 测试用 AbilityManager（真实 Add/Remove/List 语义）。
type fakeAbilityMgrForCustomizer struct {
	abilities []cschema.Ability
}

// AddAbility 空实现（接口扩张后补齐，规则 10）。
func (f *fakeAbilityMgrForCustomizer) AddAbility(card *tool.ToolCard, _ tool.Tool) agentschema.AddAbilityResult {
	if card == nil {
		return agentschema.AddAbilityResult{Added: false, Reason: "nil_card"}
	}
	return agentschema.AddAbilityResult{Name: card.Name, Added: true}
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
	tag string, parallelToolCalls bool) []agentschema.ExecuteResult {
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
	// hinterfaces.UnimplementedDeepAgentInteraction 交互方法空实现（9.87）
	hinterfaces.UnimplementedDeepAgentInteraction
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

// ──────────────────────────── c8 收尾：GetSwarmEnrichedTeamSpec ────────────────────────────

// newSwarmSpecTestManager 独立数据目录 + 清理，供 swarm 路径单测使用。
func newSwarmSpecTestManager(t *testing.T) (*TeamManager, string) {
	t.Helper()
	wsDir := t.TempDir()
	t.Setenv("UAPCLAW_DATA_DIR", wsDir)
	upath.ResetCache()
	t.Cleanup(upath.ResetCache)
	return NewTeamManager(), wsDir
}

// TestGetSwarmEnrichedTeamSpec_dispatch段 覆盖 :2053-2059——非 scheduled 关闭验证/互信/子任务。
func TestGetSwarmEnrichedTeamSpec_dispatch段(t *testing.T) {
	mgr, _ := newSwarmSpecTestManager(t)
	spec, err := mgr.GetSwarmEnrichedTeamSpec(context.Background(), "sess-dispatch",
		"team", "", nil, "req-1", "", "web",
		map[string]any{"mode": "team"}, "", "autonomous", "")
	if err != nil {
		t.Fatalf("enrich failed: %v", err)
	}
	if spec.DispatchMode != "autonomous" {
		t.Fatalf("dispatch_mode should be set, got %q", spec.DispatchMode)
	}
	if spec.EnableTaskVerification || spec.EnableMemberMessaging || spec.MaxSubtaskDepth != 0 {
		t.Fatalf("autonomous dispatch should disable verification/messaging/depth: %+v", spec)
	}
}

// TestGetSwarmEnrichedTeamSpec_dispatchScheduled 覆盖 scheduled 不动三开关。
func TestGetSwarmEnrichedTeamSpec_dispatchScheduled(t *testing.T) {
	mgr, _ := newSwarmSpecTestManager(t)
	spec, err := mgr.GetSwarmEnrichedTeamSpec(context.Background(), "sched-1",
		"team", "", nil, "req-2", "", "web",
		map[string]any{"mode": "team"}, "", "scheduled", "")
	if err != nil {
		t.Fatalf("enrich failed: %v", err)
	}
	// 对齐 Python :2053-2059：scheduled 分支不写三开关——与空 dispatch 的
	// 基线 run 对比，两轮除 DispatchMode 外逐字段一致。
	base, err := mgr.GetSwarmEnrichedTeamSpec(context.Background(), "sched-base",
		"team", "", nil, "req-2b", "", "web",
		map[string]any{"mode": "team"}, "", "", "")
	if err != nil {
		t.Fatalf("base enrich failed: %v", err)
	}
	if spec.EnableTaskVerification != base.EnableTaskVerification ||
		spec.EnableMemberMessaging != base.EnableMemberMessaging ||
		spec.MaxSubtaskDepth != base.MaxSubtaskDepth {
		t.Fatalf("scheduled dispatch must not touch the three switches: got %+v vs base %+v",
			spec, base)
	}
}

// TestGetSwarmEnrichedTeamSpec_bootstrap组合校验 覆盖 :2071-2076——framework 必须搭配 scheduled。
func TestGetSwarmEnrichedTeamSpec_bootstrap组合校验(t *testing.T) {
	mgr, _ := newSwarmSpecTestManager(t)
	_, err := mgr.GetSwarmEnrichedTeamSpec(context.Background(), "bs-1",
		"team", "", nil, "req-3", "", "web",
		map[string]any{"mode": "team"}, "", "autonomous", "framework")
	if err == nil || !strings.Contains(err.Error(), "framework") {
		t.Fatalf("framework+autonomous should be rejected at request boundary, err=%v", err)
	}
	spec, err := mgr.GetSwarmEnrichedTeamSpec(context.Background(), "bs-2",
		"team", "", nil, "req-4", "", "web",
		map[string]any{"mode": "team"}, "", "scheduled", "framework")
	if err != nil {
		t.Fatalf("framework+scheduled should pass: %v", err)
	}
	if spec.BootstrapMode != "framework" {
		t.Fatalf("bootstrap_mode should be set, got %q", spec.BootstrapMode)
	}
}

// TestGetSwarmEnrichedTeamSpec_enrich接线 覆盖 :2023-2033——seed 生产挂链（D-15.16-c8）。
func TestGetSwarmEnrichedTeamSpec_enrich接线(t *testing.T) {
	mgr, _ := newSwarmSpecTestManager(t)
	spec, err := mgr.GetSwarmEnrichedTeamSpec(context.Background(), "sess-enrich",
		"team", "/tmp/nonexistent-proj", nil, "req-5", "", "web",
		map[string]any{"mode": "team"}, "", "", "")
	if err != nil {
		t.Fatalf("enrich failed: %v", err)
	}
	if spec.BuildContext == nil {
		t.Fatal("build_context should be attached by enrich")
	}
	if len(spec.BuildContextSeed) == 0 {
		t.Fatal("build_context_seed should be generated (assembly.py:540)")
	}
	if spec.BuildContextSeed["project_dir"] != "/tmp/nonexistent-proj" {
		t.Fatalf("project_dir should travel in seed, got %v", spec.BuildContextSeed["project_dir"])
	}
	if bc, ok := spec.BuildContext.(*buildctx.SwarmBuildContext); !ok || bc.TeamID == "" {
		t.Fatal("build_context should be platform subclass with session-scoped team_id")
	}
}

// TestGetSwarmEnrichedTeamSpec_演进开关记账 覆盖 :2013——config 快照写入 per-session 槽位。
func TestGetSwarmEnrichedTeamSpec_演进开关记账(t *testing.T) {
	mgr, wsDir := newSwarmSpecTestManager(t)
	// 写入 config：react.evolution.skill_evolution = true（config.py:335-337 语义）
	cfgDir := filepath.Join(wsDir, "config")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "config.yaml"),
		[]byte("react:\n  evolution:\n    skill_evolution: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.GetSwarmEnrichedTeamSpec(context.Background(), "sess-evo",
		"team", "", nil, "req-6", "", "web",
		map[string]any{"mode": "team"}, "", "", ""); err != nil {
		t.Fatalf("enrich failed: %v", err)
	}
	mgr.mu.Lock()
	defer mgr.mu.Unlock()
	if !mgr.teamEvolutionEnabled["sess-evo"] {
		t.Fatal("skill_evolution=true config should be snapshotted per session")
	}
}

// TeardownTools 测试替身空实现（对齐 AbilityManagerInterface 扩张）。
func (f *fakeAbilityMgrForCustomizer) TeardownTools() {}
