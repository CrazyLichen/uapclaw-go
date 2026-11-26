package swarm

import (
	"sync"

	configutil "github.com/uapclaw/uapclaw-go/internal/common/config"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/observability/demand"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/rails"
	atschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/schema"
	hschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/schema"
	buildctx "github.com/uapclaw/uapclaw-go/internal/swarm/agents/swarm/buildctx"
	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/swarm/providers"
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// registeredMu 保护 registered。
	registeredMu sync.Mutex
	// registered 幂等注册标记。
	// Python: _REGISTERED (registry.py:158)
	registered bool
)

// ──────────────────────────── 导出函数 ────────────────────────────

// RegisterSwarmProviders 幂等注册全部 swarm 平台 provider 与种子工厂。
// 三步顺序不可换（Python: register_swarm_providers registry.py:180-200）：
//  1. EnsureHarnessElementsRegistered（对齐 ensure_harness_elements_registered）
//  2. 显式注册 swarm.* rail/tool/subagent provider——偏差 F6：Python 靠
//     @harness_element + manifest catalog + register_from_catalog 自动同步，
//     Go 无 catalog 机制（rails/registration.go 偏差 D-9 延伸），改为显式
//     调用清单，顺序按 registry.py:48-58 import 段排列；遗漏由
//     TestRegisterSwarmProviders_幂等且全集注册 的常量层对拍兜住
//  3. RegisterBuildContextFactory（种子工厂，覆盖注册语义由 core 保证）
//
// D-15.16-c3 已随本节回收（原独立 RegisterSwarmBuildContextFactory 并入，
// 规则 8）。同时注入 providers 包的 team/adapter 基座构造函数
// （D-15.16f-view 配套缝：providers↔team 成环，经父包无环注入）。
func RegisterSwarmProviders() {
	registeredMu.Lock()
	defer registeredMu.Unlock()
	if registered {
		return
	}
	// 步骤 1
	rails.EnsureHarnessElementsRegistered()
	// 步骤 2：rail（member_rails → code_rails → builtin → design → skills）
	providers.RegisterMemberRailProviders()
	providers.RegisterCodeRailProviders()
	providers.RegisterBuiltinRailProviders()
	providers.RegisterDesignProviders()
	providers.RegisterSkillProviders()
	providers.RegisterEvolutionRailProviders()
	// 步骤 2：tool（tools → runtime_tools → designer）
	providers.RegisterToolProviders()
	providers.RegisterRuntimeToolProviders()
	// 步骤 2：subagent（code_subagents）
	providers.RegisterSubagentProviders()
	// 步骤 3
	atschema.RegisterBuildContextFactory(buildSwarmContextFromSeed)
	// evolution rail 的 TeamManager 注册缝（D-15.24c-3 父包无环注入）
	registered = true
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// buildSwarmContextFromSeed 从可序列化种子重建 buildctx.SwarmBuildContext。
// 非序列化句柄从接收进程获取：config 来自本进程 config.yaml（与 team 包
// teamLoadConfig 同源，等价 Python get_config()），轨迹处理器来自进程级
// demand 单例。
// 注册到 core 侧后 from_spawn_payload / recover_from_session 反序列化后
// 经此恢复 provider 构建上下文。
// Python: _build_swarm_context_from_seed (registry.py:160-173)
func buildSwarmContextFromSeed(seed map[string]any) (hschema.BuildContextInterface, error) {
	cfg, err := swarmLoadConfig()
	if err != nil {
		return nil, err
	}
	return buildctx.SwarmBuildContextFromSeed(seed, cfg, demand.GetTrajectorySpanProcessor()), nil
}

// swarmLoadConfig 加载默认 config.yaml。
// 对齐 Python: get_config()（含 _normalize_config 后处理）。
func swarmLoadConfig() (map[string]any, error) {
	c, err := configutil.New("", configutil.WithNormalize(configutil.NormalizeConfig))
	if err != nil {
		return nil, err
	}
	return c.Load()
}

// ──────────────────────────── 常量 ────────────────────────────

// 以下为 swarm capability provider 注册名常量（config_specs 按 symbol 引用，
// 不硬编码字符串）。Python 源：jiuwenswarm/agents/swarm/registry.py 常量
// re-export 段（:85-150）+ providers/{member_rails,code_rails,code_subagents,
// builtin_rails,tools,runtime_tools,skills,evolution_rails,design_rails}.py
// 的字面值段（行号逐条标注）。openjiuwen 通用元素的值来自 harness/manifest
// 的 builtin_elements 与 agent_teams/rails/elements 的 subagent_elements。

const (
	// SwarmSkillToolkit Python: providers/tools.py:117 SKILL_TOOLKIT
	SwarmSkillToolkit = "swarm.skill_toolkit"
	// SwarmSkillRetrieval Python: providers/tools.py:118
	SwarmSkillRetrieval = "swarm.skill_retrieval"
	// SwarmUserTodos Python: providers/tools.py:119
	SwarmUserTodos = "swarm.user_todos"
	// SwarmVideo Python: providers/tools.py:120
	SwarmVideo = "swarm.video"
	// SwarmImageGen Python: providers/tools.py:121
	SwarmImageGen = "swarm.image_gen"
	// SwarmVideoGen Python: providers/tools.py:122
	SwarmVideoGen = "swarm.video_gen"
	// SwarmVisualGen Python: providers/tools.py:123
	SwarmVisualGen = "swarm.visual_gen"
	// SwarmXiaoyiPhone Python: providers/tools.py:124
	SwarmXiaoyiPhone = "swarm.xiaoyi_phone"
	// SwarmSymphonyToolkit Python: providers/tools.py:125
	SwarmSymphonyToolkit = "swarm.symphony_toolkit"
	// SwarmA4PIntentAuthorization Python: providers/tools.py:126 区段
	SwarmA4PIntentAuthorization = "swarm.a4p_intent_authorization"
	// DesignerGraphTools Python: registry.py:106（registry 内联字面值）
	DesignerGraphTools = "swarm.designer_graph_tools"
	// DesignerAuthoring Python: providers/design_rails.py:44
	DesignerAuthoring = "swarm.designer_authoring"
	// DesignerExecution Python: providers/design_rails.py:45
	DesignerExecution = "swarm.designer_execution"
	// DesignerSurfaceSanitizer Python: providers/design_rails.py:46
	DesignerSurfaceSanitizer = "swarm.designer_surface_sanitizer"
	// CronTools Python: providers/runtime_tools.py:47
	CronTools = "swarm.cron_tools"
	// SendFileTool Python: providers/runtime_tools.py:48
	SendFileTool = "swarm.send_file"
	// SessionMessagingTools Python: providers/runtime_tools.py:49
	SessionMessagingTools = "swarm.session_messaging_tools"
	// OrganizationModelTools Python: providers/runtime_tools.py:50
	OrganizationModelTools = "swarm.organization_model_tools"
	// OrganizationOwnerInboxTools Python: providers/runtime_tools.py:51
	OrganizationOwnerInboxTools = "swarm.organization_owner_inbox_tools"
	// MemberSkillToolkit Python: providers/skills.py:57
	MemberSkillToolkit = "swarm.member_skill_toolkit"

	// PersonalContext Python: providers/member_rails.py:71
	PersonalContext = "swarm.personal_context"
	// RuntimePrompt Python: providers/member_rails.py:72
	RuntimePrompt = "swarm.runtime_prompt"
	// TeamSkillStoragePolicy Python: providers/member_rails.py:73
	TeamSkillStoragePolicy = "swarm.team_skill_storage_policy"
	// TeamSkillLibraryReload Python: providers/member_rails.py:78
	TeamSkillLibraryReload = "swarm.team_skill_library_reload"
	// TeamWorkspaceReportPath Python: providers/member_rails.py:79
	TeamWorkspaceReportPath = "swarm.team_workspace_report_path"
	// OrganizationAvailability Python: providers/member_rails.py:80
	OrganizationAvailability = "swarm.organization_availability"
	// OrganizationModel Python: providers/member_rails.py:81
	OrganizationModel = "swarm.organization_model"
	// OrganizationOwnerInbox Python: providers/member_rails.py:82
	OrganizationOwnerInbox = "swarm.organization_owner_inbox"
	// ContextProcessor Python: providers/member_rails.py:110
	ContextProcessor = "swarm.context_processor"
	// ModelAnomalyDetection Python: providers/member_rails.py:111
	ModelAnomalyDetection = "swarm.model_anomaly_detection"
	// PluginRails Python: providers/member_rails.py:112
	PluginRails = "swarm.plugin_rails"
	// SkillRetrievalPrompt Python: providers/member_rails.py:113
	SkillRetrievalPrompt = "swarm.skill_retrieval_prompt"
	// SymphonyOrchestrationPrompt Python: providers/member_rails.py:114
	SymphonyOrchestrationPrompt = "swarm.symphony_orchestration_prompt"
	// SwarmHeartbeat Python: providers/member_rails.py:115
	SwarmHeartbeat = "swarm.heartbeat"
	// SessionMessagingRoute Python: providers/member_rails.py:116
	SessionMessagingRoute = "swarm.session_messaging_route"
	// TeamPermissionPolicy Python: providers/member_rails.py:700
	TeamPermissionPolicy = "swarm.team_permission_policy"
	// TeamPermission Python: providers/member_rails.py:748
	TeamPermission = "swarm.team_permission"

	// TeamSkillEvolution Python: providers/evolution_rails.py:51
	TeamSkillEvolution = "swarm.team_skill_evolution"
	// TeamSkillCreate Python: providers/evolution_rails.py:52
	TeamSkillCreate = "swarm.team_skill_create"
	// MemberSkillEvolution Python: providers/evolution_rails.py:53
	MemberSkillEvolution = "swarm.member_skill_evolution"
	// EvolutionInterrupt Python: providers/evolution_rails.py:54
	EvolutionInterrupt = "swarm.evolution_interrupt"
	// SymphonyGraphEvolution Python: providers/evolution_rails.py:55
	SymphonyGraphEvolution = "swarm.symphony_graph_evolution"

	// CodeExtraTools Python: providers/tools.py:127
	CodeExtraTools = "swarm.code_extra_tools"
	// CodeRuntimePrompt Python: providers/code_rails.py:47
	CodeRuntimePrompt = "swarm.code_runtime_prompt"
	// CodeProjectMemory Python: providers/code_rails.py:48
	CodeProjectMemory = "swarm.code_project_memory"
	// CodeAgentMD Python: providers/code_rails.py:49
	CodeAgentMD = "swarm.code_agent_md"
	// PermissionInterrupt Python: providers/code_rails.py:50
	PermissionInterrupt = "swarm.permission_interrupt"
	// CodeCodingMemory Python: providers/code_rails.py:51
	CodeCodingMemory = "swarm.code_coding_memory"
	// CodeAgentMode Python: providers/code_rails.py:52
	CodeAgentMode = "swarm.code_agent_mode"
	// TeamPlanApproval Python: providers/code_rails.py:53
	TeamPlanApproval = "swarm.team_plan_approval"
	// StructuredAskUser Python: providers/code_rails.py:54
	StructuredAskUser = "swarm.structured_ask_user"
	// CodeTaskPlanning Python: providers/code_rails.py:55
	CodeTaskPlanning = "swarm.code_task_planning"
	// CodeAgentRail Python: providers/code_rails.py:56
	CodeAgentRail = "swarm.code_agent_rail"
	// UserHooks Python: providers/code_rails.py:57
	UserHooks = "swarm.user_hooks"
	// SwarmCodeAgent Python: providers/code_subagents.py:67
	SwarmCodeAgent = "swarm.code_agent"
	// SwarmBrowserAgent Python: providers/code_subagents.py:68
	SwarmBrowserAgent = "swarm.browser_agent"
	// SwarmCuaAgent Python: providers/code_subagents.py:69
	SwarmCuaAgent = "swarm.cua_agent"
	// StatuslineSetupAgent Python: providers/code_subagents.py:70
	StatuslineSetupAgent = "swarm.statusline_setup_agent"
	// DefaultStatuslineSetupMaxIterations Python: server/runtime/agent_adapter/statusline_setup_agent.py:17（经 registry.py:133-135 re-export）
	DefaultStatuslineSetupMaxIterations = 15

	// SwarmResponsePrompt Python: providers/builtin_rails.py:40
	SwarmResponsePrompt = "swarm.response_prompt"
	// SwarmStreamEvent Python: providers/builtin_rails.py:41
	SwarmStreamEvent = "swarm.stream_event"
	// SwarmAvatarPrompt Python: providers/builtin_rails.py:42
	SwarmAvatarPrompt = "swarm.avatar_prompt"
	// SwarmMultimodalImage Python: providers/builtin_rails.py:43
	SwarmMultimodalImage = "swarm.multimodal_image"
)

// SwarmRailNamesForTest 返回常量层全部 swarm.* rail 注册名（测试全集对拍用；
// 非测试代码不得消费）。
func SwarmRailNamesForTest() map[string]bool {
	return map[string]bool{
		PersonalContext:             true,
		RuntimePrompt:               true,
		TeamSkillStoragePolicy:      true,
		TeamSkillLibraryReload:      true,
		TeamWorkspaceReportPath:     true,
		OrganizationAvailability:    true,
		OrganizationModel:           true,
		OrganizationOwnerInbox:      true,
		ContextProcessor:            true,
		ModelAnomalyDetection:       true,
		PluginRails:                 true,
		SkillRetrievalPrompt:        true,
		SymphonyOrchestrationPrompt: true,
		SwarmHeartbeat:              true,
		SessionMessagingRoute:       true,
		TeamPermissionPolicy:        true,
		TeamPermission:              true,
		TeamSkillEvolution:          true,
		TeamSkillCreate:             true,
		MemberSkillEvolution:        true,
		DesignerAuthoring:           true,
		DesignerExecution:           true,
		DesignerSurfaceSanitizer:    true,
		MemberSkillToolkit:          true,
	}
}
