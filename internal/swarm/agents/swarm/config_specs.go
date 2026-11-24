// Python 源：jiuwenswarm/agents/swarm/config_specs.py（1241 行 1:1）
//          + jiuwenswarm/agents/swarm/assembly.py:119-137（_with_project_cwd）
//
// swarm 团队成员的「config 来源能力装配」重写版：成员的 rails、tools 与
// sub-agents 全部以 RailSpec / BuiltinToolSpec / SubAgentSpec 声明（引用
// swarm.* provider，常量经 registry.go），从 config 源与成员角色派生。
// 刻意不依赖任何 DeepAgent 实例——只依赖 config map 与 provider 注册名。
//
// build_member_deep_agent_spec 把这些声明折到基础 DeepAgentSpec 上，由
// openjiuwen 从合并后的 spec 构建成员。两个 profile：
//   - team：聊天团队 profile（common rails + base tools）
//   - code.team / team.plan.code：code profile（swarm.code_* rails、code
//     子代理、code system prompt），同样纯声明式
// build_reviewer_deep_agent_spec 把装配好的 teammate spec 收窄成一次性
// reviewer 的基座（TeamAgentSpec.reviewer_agent）。

package swarm

import (
	"os"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/config"
	cschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/agent/scheduling"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/rails"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/tool/mcp/types"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/kv_cache"
	agentschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/manifest"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/prompts"
	skillrails "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/rails/skills"
	hschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/schema"
	codeprompt "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/code/prompt"
	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/tools"
	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/team/runtimeinheritance"
	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/swarm/providers"
	swarmcommon "github.com/uapclaw/uapclaw-go/internal/swarm/common"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
// codePromptBuilder 无参 code system prompt（build_code_system_prompt）。
// Python: jiuwenswarm/agents/harness/code/prompt/code_prompt_builder.py
)

// codeModes 路由到 code adapter、取 code 成员 profile 的模式。
// Python: _CODE_MODES (config_specs.py:81-88)
var codeModes = map[string]bool{
	"code.team":        true,
	"team.plan.code":   true, // Python: TEAM_PLAN_CODE_MODE (mode_matrix.py:66)
	"team.code.normal": true, // Python: NEW_TEAM_CODE_NORMAL (mode_matrix.py:39)
	"team.code.plan":   true, // Python: NEW_TEAM_CODE_PLAN (mode_matrix.py:40)
}

// commonRailNames 两角色共有的 rails，按挂载顺序。每项是 registry 的 swarm.*
// provider 名（不硬编码字符串）。
// Python: _COMMON_RAIL_NAMES (config_specs.py:101-123)
var commonRailNames = []string{
	UserHooks,
	RuntimePrompt,
	TeamSkillStoragePolicy,
	// 单一技能库写入后重载成员的 Skill 视图；取代旧的 link-refresh rail：
	// 不再有 per-team link 视图。
	TeamSkillLibraryReload,
	SwarmResponsePrompt,
	manifest.SysOperation,
	SwarmStreamEvent,
	manifest.TaskPlanning,
	manifest.Security,
	ModelAnomalyDetection,
	SwarmHeartbeat,
	SwarmAvatarPrompt,
	SwarmMultimodalImage,
	TeamWorkspaceReportPath,
	ContextProcessor,
	PersonalContext,
	PluginRails,
	SkillRetrievalPrompt,
	SymphonyOrchestrationPrompt,
}

// commonToolNames 两角色共有的 tools。每个元素按 config 自门控，因此全部声明；
// 未配置的元素产出零工具。技能管理工具（search/install/uninstall_skill）由
// MEMBER_SKILL_TOOLKIT rail（下方追加）注册并持有，这里再声明 SKILL_TOOLKIT
// 只会双重注册同名工具。
// Python: _COMMON_TOOL_NAMES (config_specs.py:127-148)
var commonToolNames = []string{
	manifest.WebSearch,
	manifest.WebFetch,
	manifest.WebPaidSearch,
	manifest.Vision,
	manifest.Audio,
	SwarmSkillRetrieval,
	SwarmSymphonyToolkit,
	SwarmVideo,
	SwarmImageGen,
	SwarmVideoGen,
	SwarmVisualGen,
	SwarmXiaoyiPhone,
	CronTools,
	SendFileTool,
}

// codeRailNames 无参 code-profile rails（common rails 的 code 变体 + code 专属
// rails）。带 params 的 code_confirm_interrupt 与 member_skill_toolkit 单独追加
// （后者紧挨它互补的 Skill rail）。
// Python: _CODE_RAIL_NAMES (config_specs.py:154-179)
var codeRailNames = []string{
	CodeRuntimePrompt,
	SwarmResponsePrompt,
	SwarmStreamEvent,
	SwarmMultimodalImage,
	manifest.Security,
	ModelAnomalyDetection,
	SwarmHeartbeat,
	manifest.LSP,
	CodeProjectMemory,
	CodeAgentMD,
	PermissionInterrupt,
	manifest.SysOperation,
	CodeCodingMemory,
	CodeAgentMode,
	StructuredAskUser,
	ContextProcessor,
	PersonalContext,
	CodeTaskPlanning,
	CodeAgentRail,
	UserHooks,
	// Skill rail 单独追加：team 持有的 core.team.skill_use，与聊天 profile 共享。
	SkillRetrievalPrompt,
	SymphonyOrchestrationPrompt,
}

// codeSharedRailNames 与 team profile 共享、追加到 code profile 的 rails。
// Python: _CODE_SHARED_RAIL_NAMES (config_specs.py:182-185)
var codeSharedRailNames = []string{
	TeamWorkspaceReportPath,
	PluginRails,
}

// codeToolNames code 成员 tools：common 工具集 + code 专属 acp_chat。
// Python: _CODE_TOOL_NAMES (config_specs.py:188-206)
var codeToolNames = []string{
	manifest.WebSearch,
	manifest.WebFetch,
	manifest.WebPaidSearch,
	manifest.Vision,
	manifest.Audio,
	SwarmSkillRetrieval,
	SwarmSymphonyToolkit,
	SwarmVideo,
	SwarmImageGen,
	SwarmVideoGen,
	SwarmVisualGen,
	SwarmXiaoyiPhone,
	CodeExtraTools,
	CronTools,
	SendFileTool,
}

// reviewerTeamRailNames scheduled-dispatch reviewer 从 teammate 集合保留的
// rails（按 profile）。reviewer 只检查交付物并投票：保留提示词框架、只读文件
// 与 shell（SYS_OPERATION 由 BuildReviewerAgentSpec 切只读）、事件流、安全与
// 上下文处理。规划、调度、技能管理/演进、权限与 plugin rails 排除。
// Python: _REVIEWER_TEAM_RAIL_NAMES (config_specs.py:214-223)
var reviewerTeamRailNames = []string{
	RuntimePrompt,
	SwarmResponsePrompt,
	manifest.SysOperation,
	SwarmStreamEvent,
	manifest.Security,
	ModelAnomalyDetection,
	SwarmMultimodalImage,
	ContextProcessor,
}

// reviewerCodeRailNames code profile 的 reviewer 保留集。
// Python: _REVIEWER_CODE_RAIL_NAMES (config_specs.py:224-235)
var reviewerCodeRailNames = []string{
	CodeRuntimePrompt,
	SwarmResponsePrompt,
	manifest.SysOperation,
	SwarmStreamEvent,
	manifest.Security,
	ModelAnomalyDetection,
	SwarmMultimodalImage,
	ContextProcessor,
	manifest.LSP,
	CodeAgentMD,
}

// reviewerSkillRailTypes reviewer 保留的 Skill rail：一个 reviewer 可以用已安装
// 的技能（例如读 PDF 交付物）；安装/卸载技能是另一条 rail，排除在外。
// Python: _REVIEWER_SKILL_RAIL_TYPES (config_specs.py:238)
var reviewerSkillRailTypes = []string{
	manifest.SkillUse,  // Python: CORE_SKILL_USE
	rails.TeamSkillUse, // Python: TEAM_SKILL_USE
}

// reviewerToolNames reviewer 保留的 tools：事实/查重校验的 web 查询 + 媒体交付
// 物的感知工具。
// Python: _REVIEWER_TOOL_NAMES (config_specs.py:241-248)
var reviewerToolNames = []string{
	manifest.WebSearch,
	manifest.WebFetch,
	manifest.WebPaidSearch,
	manifest.Vision,
	manifest.Audio,
	SwarmVideo,
}

// designRailNames design profile（team.design）能力集。design leader 是设计画布
// 之上的对话面，worker 是逐节点执行器：skill/symphony/task-planning rails 与
// 通用 web/媒体工具排除（designer graph tools 经 agent_group leader 模板进入，
// 不走 profile）。其余——runtime/response prompts、streaming、security、异常
// 检测、heartbeat、context——保留，因为 team runtime 的会话机制依赖它们。
// Python: _DESIGN_RAIL_NAMES (config_specs.py:265-274)
var designRailNames = []string{
	RuntimePrompt,
	SwarmResponsePrompt,
	SwarmStreamEvent,
	manifest.Security,
	ModelAnomalyDetection,
	SwarmMultimodalImage,
	TeamWorkspaceReportPath,
	ContextProcessor,
}

// designToolNames Python: _DESIGN_TOOL_NAMES (config_specs.py:276-279)
var designToolNames = []string{
	manifest.Vision,
	manifest.Audio,
}

// designLeaderToolNames Python: _DESIGN_LEADER_TOOL_NAMES (config_specs.py:281-285)
var designLeaderToolNames = []string{
	manifest.WebSearch,
	manifest.WebFetch,
	manifest.WebPaidSearch,
}

// ──────────────────────────── 导出函数 ────────────────────────────

// BuildMemberCapabilitySpecs 构建团队成员的 rail/tool 声明。
// 按 mode 分支：code 模式取 code profile（swarm.code_* rails），其余模式取
// 聊天团队 profile。member-skill toolkit rail 携带角色选择的技能名，角色追加
// 各自的技能演进 rails。
// Python: build_member_capability_specs (config_specs.py:826-873)
func BuildMemberCapabilitySpecs(configMap map[string]any, mode string, role string, enablePermissions bool) ([]hschema.RailSpec, []hschema.BuiltinToolSpec) {
	// Python: role == "leader" and (config.get("experimental") or {}).get("team_organization_ui_enabled", False)
	var availability *hschema.RailSpec
	if role == "leader" && organizationUIEnabled(configMap) {
		availability = &hschema.RailSpec{Type: OrganizationAvailability}
	}
	var railSpecs []hschema.RailSpec
	var toolSpecs []hschema.BuiltinToolSpec
	if isCodeMode(mode) {
		railSpecs, toolSpecs = buildCodeCapabilitySpecs(configMap, mode, role, enablePermissions)
	} else if isDesignMode(mode) {
		railSpecs, toolSpecs = buildDesignCapabilitySpecs(configMap, mode, role, enablePermissions)
	} else {
		railSpecs, toolSpecs = buildTeamCapabilitySpecs(configMap, mode, role, enablePermissions)
	}
	if availability != nil {
		railSpecs = append(railSpecs, *availability)
	}
	if organizationUIEnabled(configMap) {
		railSpecs = append(railSpecs, hschema.RailSpec{Type: OrganizationModel})
		if role == "leader" {
			toolSpecs = append(toolSpecs, hschema.BuiltinToolSpec{Type: OrganizationModelTools})
			railSpecs = append(railSpecs, hschema.RailSpec{Type: OrganizationOwnerInbox})
			toolSpecs = append(toolSpecs, hschema.BuiltinToolSpec{Type: OrganizationOwnerInboxTools})
		}
	}
	return railSpecs, toolSpecs
}

// BuildMemberSubagentSpecs 构建声明式成员子代理声明。
// status-line setup agent 在所有模式下可用（启用时）。code 模式额外包含
// explore / plan。每个子代理按 react.subagents.<name>.enabled 门控；
// status-line setup、explore、plan 缺省开（只有显式 false 才去掉），code /
// browser 需要显式 true。cua（桌面）子代理与模式无关，同样需要显式 true。
// Python: build_member_subagent_specs (config_specs.py:966-1042)
func BuildMemberSubagentSpecs(configMap map[string]any, mode string, role string) []hschema.SubAgentSpec {
	react := configSection(configMap, "react")
	subagentsCfg := configSection(react, "subagents")
	language := subagentLanguage(mode, role, configMap)

	specs := []hschema.SubAgentSpec{}
	if isSubagentDefaultEnabled(subagentsCfg["statusline-setup"]) {
		specs = append(specs, codeSubagentSpec("statusline-setup", StatuslineSetupAgent, react, language))
	}
	// cua_agent 驱动宿主桌面，不是 code-profile 关注点，运营显式开启时所有模式
	// 都提供。
	if isSubagentEnabled(subagentsCfg["cua_agent"]) {
		specs = append(specs, codeSubagentSpec("cua_agent", SwarmCuaAgent, react, language))
	}
	if !isCodeMode(mode) {
		return specs
	}
	// explore / plan 是 code profile 的核心子代理，除非配置显式关闭否则保持
	// 挂载：缺条目维持既有行为。
	for _, pair := range [][2]string{
		{"explore_agent", rails.ExploreAgent},
		{"plan_agent", rails.PlanAgent},
	} {
		if isSubagentDefaultEnabled(subagentsCfg[pair[0]]) {
			specs = append(specs, codeSubagentSpec(pair[0], pair[1], react, language))
		}
	}
	if isSubagentEnabled(subagentsCfg["code_agent"]) {
		specs = append(specs, codeSubagentSpec("code_agent", SwarmCodeAgent, react, language))
	}
	if isSubagentEnabled(subagentsCfg["browser_agent"]) {
		specs = append(specs, codeSubagentSpec("browser_agent", SwarmBrowserAgent, react, language))
	}
	return specs
}

// BuildMemberDeepAgentSpec 把成员能力声明折到 baseSpec 上。
// 在基础 spec 的既有条目之后追加 rails/tools（code 模式还有子代理 + code 系统
// 提示词），返回新 DeepAgentSpec（输入不被修改）。leader 永不带子代理：其
// subagent 列表钉为空，agent-core 就不挂 SubagentRail，leader 没有 task_tool
// 面——委派走团队原生 runner 工具。
// Python: build_member_deep_agent_spec (config_specs.py:1045-1182)
func BuildMemberDeepAgentSpec(configMap map[string]any, mode string, role string, baseSpec *hschema.DeepAgentSpec, enablePermissions bool, mcpConfigs []*types.McpServerConfig) *hschema.DeepAgentSpec {
	railSpecs, toolSpecs := BuildMemberCapabilitySpecs(configMap, mode, role, enablePermissions)

	mergedRails := append([]hschema.RailSpec{}, baseSpec.Rails...)
	mergedRails = append(mergedRails, railSpecs...)
	mergedTools := append([]hschema.BuiltinToolSpec{}, baseSpec.BuiltinTools...)
	mergedTools = append(mergedTools, toolSpecs...)
	mergedMcps := mergeMcpConfigs(baseSpec.Mcps, mcpConfigs)

	retrievalEnabled := retrievalEnabled(configMap)
	mergedRails = collapseSkillUseRails(mergedRails, retrievalEnabled)

	// Python update dict：Go 侧直接改副本。
	merged := *baseSpec
	merged.Rails = mergedRails
	merged.BuiltinTools = mergedTools
	merged.Mcps = mergedMcps
	kvCfg := kvCacheAffinityConfig(configMap)
	merged.KvCacheAffinityConfig = &kvCfg

	if config.ProgressiveToolEnabled(configMap) {
		// 与单 agent adapter 读的同一顶层 progressive_tool_enabled 开关：成员像
		// 单 agent 一样挂 harness ProgressiveToolRail（tool_search + 延迟工具）。
		// 团队协调工具保持 DIRECT——TeamToolRail 声明其曝光——只有长尾按需发现。
		// 仅在启用时写入，禁用的开关让基础 spec 字节级不变（YAML 基础 spec 可能
		// 自带值）。
		merged.ProgressiveTool = &hschema.ProgressiveToolSpec{Enabled: true}
	}
	if role == "leader" {
		// leader 用团队任务板（create_task / view_task / update_task）。强制关
		// agent-core 的 enable_task_planning 自动注入路径，YAML 基础 spec 无法
		// 经 resolve_deep_agent_parts 重挂 harness todo rails。
		merged.EnableTaskPlanning = false
		// leader 经团队原生 runner 工具（build_team / spawn_teammate）派发，不
		// 携带子代理：agent-core 只对非空 subagent 列表挂 SubagentRail——连着
		// task_tool 面——所以钉空列表即使 YAML 基础 spec 配置了也不上。
		merged.Subagents = []hschema.SubAgentSpec{}
	}
	if !isCodeMode(mode) {
		merged.EnableSkillDiscovery = !retrievalEnabled
	}

	// teammate 在这里构建成员子代理集；上面的 leader 分支已钉空，leader 跳过。
	var subagentSpecs []hschema.SubAgentSpec
	if role != "leader" {
		subagentSpecs = BuildMemberSubagentSpecs(configMap, mode, role)
	}

	// team 模式下 base_spec 含硬编码 server_id="playwright_official_stdio" 的
	// browser_agent。全部成员共享单个 @playwright/mcp 子进程 → 单 Chrome 窗口。
	// 换成 SWARM_BROWSER_AGENT（按成员构建唯一 server_id：session_id + role），
	// 每个成员拿到隔离的 Chrome。code 模式下 BuildMemberSubagentSpecs 已返回
	// SWARM_BROWSER_AGENT，此分支只在非 code 模式运行。
	// Python: config_specs.py:1127-1147
	var teamBrowserSpec *hschema.SubAgentSpec
	if role != "leader" && !isCodeMode(mode) {
		react := configSection(configMap, "react")
		subagentsCfg := configSection(react, "subagents")
		if isSubagentEnabled(subagentsCfg["browser_agent"]) {
			language := subagentLanguage(mode, role, configMap)
			spec := codeSubagentSpec("browser_agent", SwarmBrowserAgent, react, language)
			teamBrowserSpec = &spec
		}
	}

	if len(subagentSpecs) > 0 || teamBrowserSpec != nil {
		mergedSubagents := append([]hschema.SubAgentSpec{}, baseSpec.Subagents...)
		// 我们贡献自己的 explore_agent 时丢弃基础 spec 的：code 模式总是在
		// BuildMemberSubagentSpecs 里声明一个，同名两个 spec 只会在
		// create_subagent 里互相遮蔽。SubAgentSpec 没有 subagent_type 字段，
		// name / factory_name 检查才是实际匹配。
		// Python: config_specs.py:1150-1159
		hasExplore := false
		for _, spec := range subagentSpecs {
			if isExploreSubagent(spec) {
				hasExplore = true
				break
			}
		}
		if hasExplore {
			filtered := make([]hschema.SubAgentSpec, 0, len(mergedSubagents))
			for _, spec := range mergedSubagents {
				if !isExploreSubagent(spec) {
					filtered = append(filtered, spec)
				}
			}
			mergedSubagents = filtered
		}
		// 移除基础 spec 的 browser_agent，防止共享的 playwright_official_stdio
		// 条目与我们的隔离条目共存。
		// Python: config_specs.py:1161-1169
		removeBrowser := teamBrowserSpec != nil
		if !removeBrowser {
			for _, spec := range subagentSpecs {
				if subagentTypeOf(spec) == "browser_agent" {
					removeBrowser = true
					break
				}
			}
		}
		if removeBrowser {
			filtered := make([]hschema.SubAgentSpec, 0, len(mergedSubagents))
			for _, spec := range mergedSubagents {
				if subagentTypeOf(spec) != "browser_agent" {
					filtered = append(filtered, spec)
				}
			}
			mergedSubagents = filtered
		}
		if teamBrowserSpec != nil {
			mergedSubagents = append(mergedSubagents, *teamBrowserSpec)
		}
		mergedSubagents = append(mergedSubagents, subagentSpecs...)
		merged.Subagents = mergedSubagents
	}

	if isCodeMode(mode) {
		// Python: from ...code_prompt_builder import build_code_system_prompt
		merged.SystemPrompt = codeprompt.BuildCodeSystemPrompt()
	}

	return &merged
}

// BuildReviewerDeepAgentSpec 把装配好的 teammate spec 收窄成
// scheduled-dispatch reviewer spec。从完整装配的 teammate spec 出发（rail
// params、项目 cwd、MCP servers 原样携带），再在框架裁剪之上只保留 reviewer
// 白名单（只读文件工具、无 todo planning、无子代理）。
// Python: build_reviewer_deep_agent_spec (config_specs.py:1185-1205)
func BuildReviewerDeepAgentSpec(mode string, teammateSpec *hschema.DeepAgentSpec) *hschema.DeepAgentSpec {
	railNames := reviewerTeamRailNames
	if isCodeMode(mode) {
		railNames = reviewerCodeRailNames
	}
	railTypes := append(append([]string{}, railNames...), reviewerSkillRailTypes...)
	return scheduling.BuildReviewerAgentSpec(teammateSpec, railTypes, reviewerToolNames)
}

// WithProjectCwd 把成员的 cwd / project root 指向请求的项目目录。
// 只移动工作目录：成员保留自己的工作区（记忆、Skill 可见性元数据）。worktree
// 隔离开启时 AgentConfigurator 会再次用成员 worktree 覆盖 cwd——所以这里无
// 条件设置。
// Python: _with_project_cwd (assembly.py:119-130)
func WithProjectCwd(memberSpec *hschema.DeepAgentSpec, projectDir string) *hschema.DeepAgentSpec {
	projectRoot := strings.TrimSpace(projectDir)
	if projectRoot == "" {
		return memberSpec
	}
	copied := *memberSpec
	// Python model_copy(update={"cwd": ..., "project_root": ...})——spec 值拷贝
	// 后覆写两个字段。
	copied.Cwd = projectRoot
	copied.ProjectRoot = projectRoot
	return &copied
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// isCodeMode mode 是否路由到 code 成员 profile。
// Python: _is_code_mode (config_specs.py:253-255)
func isCodeMode(mode string) bool {
	return codeModes[schema.CanonicalizeMode(mode)]
}

// isDesignMode mode 是否路由到 design 成员 profile。
// Python: _is_design_mode (config_specs.py:288-290)
func isDesignMode(mode string) bool {
	return schema.CanonicalizeMode(mode) == "team.design"
}

// organizationUIEnabled Python: (config.get("experimental") or {}).get("team_organization_ui_enabled", False)
func organizationUIEnabled(configMap map[string]any) bool {
	return configSection(configMap, "experimental")["team_organization_ui_enabled"] == true
}

// kvCacheAffinityConfig 组装 KVC 亲和配置。
// Python: _kv_cache_affinity_config (config_specs.py:91-96)
func kvCacheAffinityConfig(configMap map[string]any) kv_cache.KVCacheAffinityConfig {
	return swarmcommon.BuildKvCacheAffinityConfig(
		configMap,
		swarmcommon.GetKvCacheControlModelProvider(configMap),
		swarmcommon.GetKvCacheControlModelClientConfig(configMap),
	)
}

// resolveMemberSkills 从 config 源解析 *role* 的种子白名单。
// 读 config.agents.<role>.skills，返回清洗后的非空名。
// 这**不再是运行时权威**。该值只是成员 skills-visibility.json 的种子，由
// core.team.skill_use rail 以 bootstrap_allow 携带、在文件尚不存在时由唯一
// seeder（create_team_skill_use_rail）写入一次；之后由文档决定成员看到什么，
// 修改这个 config 键对已有文件的成员无效。值按 *role*，文档按成员 *instance*：
// rail 在 setup 期解析成员身份，无需其他地方把 role 展开成 instance。
// Python: _resolve_member_skills (config_specs.py:356-389)
func resolveMemberSkills(configMap map[string]any, role string) []string {
	agents, ok := configMap["agents"].(map[string]any)
	if !ok {
		return nil
	}
	member, ok := agents[role].(map[string]any)
	if !ok {
		return nil
	}
	skills, ok := member["skills"].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(skills))
	for _, skill := range skills {
		name := strings.TrimSpace(stringifySkill(skill))
		if name != "" {
			out = append(out, name)
		}
	}
	return out
}

// configSection 取 config[key] 的 dict（缺失或类型错得空 map）。
// Python: _config_section (config_specs.py:400-403)
func configSection(configMap map[string]any, key string) map[string]any {
	if configMap == nil {
		return map[string]any{}
	}
	if sectionMap, ok := configMap[key].(map[string]any); ok {
		return sectionMap
	}
	return map[string]any{}
}

// evolutionModelConfig 解析可序列化的演进模型配置（LLM 稍后构建）。
// Python: _evolution_model_config (config_specs.py:406-415)
func evolutionModelConfig(configMap map[string]any) map[string]any {
	mcc, mco, modelName := runtimeinheritance.ResolveModelConfig(configMap)
	return map[string]any{
		"model_client_config": mcc,
		"model_config_obj":    mco,
		"model_name":          modelName,
	}
}

// skillMode 从 react.skill_mode 解析校验过的技能使用模式。
// Python: _skill_mode (config_specs.py:418-425)
func skillMode(configMap map[string]any) string {
	if retrievalEnabled(configMap) {
		return skillrails.SkillModeAutoList
	}
	react := configSection(configMap, "react")
	raw := react["skill_mode"]
	valid := map[string]bool{skillrails.SkillModeAutoList: true, skillrails.SkillModeAll: true}
	if s, ok := raw.(string); ok && valid[s] {
		return s
	}
	return skillrails.SkillModeAll
}

// retrievalEnabled 此 config 是否启用 agentic 技能检索。
// env SYMPHONY_SKILL_RETRIEVAL_ENABLED 优先；否则读 symphony.skill_retrieval.enabled。
// Python: _retrieval_enabled (config_specs.py:428-438)
func retrievalEnabled(configMap map[string]any) bool {
	if envValue := os.Getenv("SYMPHONY_SKILL_RETRIEVAL_ENABLED"); envValue != "" && strings.TrimSpace(envValue) != "" {
		switch strings.ToLower(strings.TrimSpace(envValue)) {
		case "1", "true", "yes", "on", "enabled":
			return true
		}
		return false
	}
	symphony := configSection(configMap, "symphony")
	if retrieval, ok := symphony["skill_retrieval"].(map[string]any); ok {
		enabled, _ := retrieval["enabled"].(bool)
		return enabled
	}
	return false
}

// teamSkillUseRailSpec 为任一 profile 的成员声明团队 Skill rail。
// 这里只声明曝光模式与种子白名单。rail 读按成员与团队 skills-visibility.json
// 收窄的单一物理 Skill 库；声明路径以每次 spawn 后才铸造的成员身份为键，
// complete_declared_team_skill_rails 在成员 setup 期连同库根一起填充。
// 在这里声明而不是留给团队自动挂载，正是 react.skill_mode / agentic retrieval
// 得到遵守的原因：自动挂载无 config 可读，会缺省 ALL 模式。
// Python: _team_skill_use_rail_spec (config_specs.py:441-469)
func teamSkillUseRailSpec(configMap map[string]any, role string) hschema.RailSpec {
	return hschema.RailSpec{
		Type: rails.TeamSkillUse,
		Params: map[string]any{
			"skill_mode":      skillMode(configMap),
			"bootstrap_allow": resolveMemberSkills(configMap, role),
		},
	}
}

// collapseSkillUseRails 只保留第一条声明的 Skill rail。
// 成员绝不能挂两条 Skill rails：它们扫同一个库、注册同样的 skill/list_skill
// 工具，代价是每次构建多一条提示词段 + 每工具一条重复能力 warning。基础 spec
// 的 rails 在前，所以自带 Skill rail 的 blueprint 优先于本模块追加的同款——
// 与 openjiuwen agent_teams.skill.rail_spec 在团队侧的优先级一致。
// agentic retrieval 开启时，幸存者额外钉 auto-list：模型经检索工具发现技能，
// 而不是整库注入提示词。
// Python: _collapse_skill_use_rails (config_specs.py:472-511)
func collapseSkillUseRails(railList []hschema.RailSpec, retrievalOn bool) []hschema.RailSpec {
	skillRailTypes := map[string]bool{
		manifest.SkillUse:  true,
		rails.TeamSkillUse: true,
		"skill_use":        true,
		"SkillUseRail":     true,
	}
	hasSkillRail := false
	collapsed := make([]hschema.RailSpec, 0, len(railList))
	for _, rail := range railList {
		if skillRailTypes[rail.Type] {
			if hasSkillRail {
				continue
			}
			hasSkillRail = true
			if retrievalOn {
				params := map[string]any{}
				for k, v := range rail.Params {
					params[k] = v
				}
				params["skill_mode"] = skillrails.SkillModeAutoList
				params["include_tools"] = false
				rail = hschema.RailSpec{Type: rail.Type, Params: params}
			}
			collapsed = append(collapsed, rail)
			continue
		}
		collapsed = append(collapsed, rail)
	}
	return collapsed
}

// additionalDirectories 从 config + env 解析 project-memory 附加目录。
// Python: _additional_directories (config_specs.py:514-524)
func additionalDirectories(configMap map[string]any) []string {
	react := configSection(configMap, "react")
	projectMemory := configSection(react, "project_memory")
	raw, present := projectMemory["additional_directories"]
	if !present || raw == nil {
		raw = os.Getenv("UAPCLAW_ADDITIONAL_DIRECTORIES")
	}
	switch v := raw.(type) {
	case string:
		out := []string{}
		for _, item := range strings.Split(v, string(os.PathListSeparator)) {
			if strings.TrimSpace(item) != "" {
				out = append(out, strings.TrimSpace(item))
			}
		}
		return out
	case []any:
		out := []string{}
		for _, item := range v {
			if s := strings.TrimSpace(stringifySkill(item)); s != "" {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

// permissionModelName 从 config 解析权限 rail 的模型名。
// Python: _permission_model_name (config_specs.py:527-534)
func permissionModelName(configMap map[string]any) string {
	models := configSection(configMap, "models")
	defaultModel := configSection(models, "default")
	mcc := configSection(defaultModel, "model_client_config")
	if name := mcc["model_name"]; name != nil {
		if s, ok := name.(string); ok && s != "" {
			return s
		}
	}
	return "gpt-4"
}

// acpEnabled 是否配置了 ACP agents（gates acp_chat 工具）。
// Python: _acp_enabled (config_specs.py:537-540)
func acpEnabled(configMap map[string]any) bool {
	acpCfg, ok := configMap["acp_agents"].(map[string]any)
	return ok && len(acpCfg) > 0
}

// contextProcessorParams 上下文压缩 rail 的属性 params。
// context_engine_config 在 config.yaml 里位于 react 下；在顶层读会得 {}，
// rail 拿到空配置、挂不上压缩器。
// Python: _context_processor_params (config_specs.py:543-554)
func contextProcessorParams(configMap map[string]any) map[string]any {
	react := configSection(configMap, "react")
	return map[string]any{
		"context_engine_enabled": runtimeinheritance.GetContextEngineEnabled(configMap),
		"context_engine_config":  configSection(react, "context_engine_config"),
	}
}

// modelAnomalyDetectionParams 模型异常/工具循环压缩 rail 的属性 params。
// Python: _model_anomaly_detection_params (config_specs.py:557-562)
func modelAnomalyDetectionParams(configMap map[string]any) map[string]any {
	guard := configSection(configMap, "execution_guard")
	return map[string]any{
		"rail_config": configSection(guard, "model_anomaly_detection_rail"),
	}
}

// permissionParams 权限中断 rail 的属性 params。
// Python: _permission_params (config_specs.py:565-570)
func permissionParams(configMap map[string]any) map[string]any {
	return map[string]any{
		"permissions_config": configSection(configMap, "permissions"),
		"model_name":         permissionModelName(configMap),
	}
}

// teamEvolutionRailParams leader 团队技能演进 rail 的属性 params。
// Python: _team_evolution_rail_params (config_specs.py:573-578)
func teamEvolutionRailParams(configMap map[string]any) map[string]any {
	return map[string]any{
		"evolution_model_config": evolutionModelConfig(configMap),
		"auto_save":              config.EvolutionAutoSaveEnabled(configMap),
	}
}

// memberEvolutionRailParams 成员技能演进 rail 的属性 params。
// Python: _member_evolution_rail_params (config_specs.py:581-585)
func memberEvolutionRailParams(configMap map[string]any) map[string]any {
	return map[string]any{
		"evolution_model_config": evolutionModelConfig(configMap),
	}
}

// railParamBuilders 按 provider 名的属性 params 构建器；无参元素为空。
// Python: _RAIL_PARAM_BUILDERS (config_specs.py:589-607)
var railParamBuilders = map[string]func(map[string]any) map[string]any{
	ContextProcessor:      contextProcessorParams,
	ModelAnomalyDetection: modelAnomalyDetectionParams,
	CodeProjectMemory: func(c map[string]any) map[string]any {
		return map[string]any{"additional_directories": additionalDirectories(c)}
	},
	PermissionInterrupt: permissionParams,
	TeamPermission: func(c map[string]any) map[string]any {
		return map[string]any{"permissions_config": configSection(c, "permissions")}
	},
	TeamPermissionPolicy: func(c map[string]any) map[string]any {
		return map[string]any{"permissions_config": configSection(c, "permissions")}
	},
	CodeCodingMemory: func(c map[string]any) map[string]any {
		return map[string]any{"embed_config": configSection(c, "embed")}
	},
	UserHooks: func(c map[string]any) map[string]any {
		return map[string]any{"hooks_section": configSection(c, "hooks")}
	},
	// Python: registry.CODE_WORKTREE: lambda c: {"enabled": True}
	// CODE_WORKTREE = core.worktree（openjiuwen WORKTREE，registry.py:149）
	"core.worktree": func(c map[string]any) map[string]any {
		return map[string]any{"enabled": true}
	},
}

// toolParamBuilders 按工具 provider 名的属性 params 构建器。
// Python: _TOOL_PARAM_BUILDERS (config_specs.py:623-628)
var toolParamBuilders = map[string]func(map[string]any) map[string]any{
	SendFileTool: func(c map[string]any) map[string]any {
		return map[string]any{"channels_config": configSection(c, "channels")}
	},
	CodeExtraTools: func(c map[string]any) map[string]any {
		return map[string]any{"acp_enabled": acpEnabled(c)}
	},
	manifest.Vision: func(c map[string]any) map[string]any {
		// Python: _vision_tool_params (config_specs.py:610-612)
		return map[string]any{"vision_model_config": providers.VisionModelConfigParams(c)}
	},
	manifest.Audio: func(c map[string]any) map[string]any {
		// Python: _audio_tool_params (config_specs.py:615-620)
		return map[string]any{
			"dedicated":          providers.AudioDedicatedConfigured(c),
			"audio_model_config": providers.AudioModelConfigParams(c),
		}
	},
}

// railParams 返回烘进 rail *name* 的属性 params（无参为空）。
// Python: _rail_params (config_specs.py:631-634)
func railParams(name string, configMap map[string]any) map[string]any {
	if builder, ok := railParamBuilders[name]; ok {
		return builder(configMap)
	}
	return nil
}

// toolParams 返回烘进 tool *name* 的属性 params（无参为空）。
// Python: _tool_params (config_specs.py:637-640)
func toolParams(name string, configMap map[string]any) map[string]any {
	if builder, ok := toolParamBuilders[name]; ok {
		return builder(configMap)
	}
	return nil
}

// teamCommonRailNames 共享的聊天团队 rails；leader 去掉 harness todo planning。
// Python: _team_common_rail_names (config_specs.py:643-649)
func teamCommonRailNames(role string) []string {
	if role == "leader" {
		out := make([]string, 0, len(commonRailNames))
		for _, name := range commonRailNames {
			if name != manifest.TaskPlanning {
				out = append(out, name)
			}
		}
		return out
	}
	return commonRailNames
}

// codeBaseRailNames code-profile rails 减权限中断；leader 去掉 code todo planning。
// PERMISSION_INTERRUPT 对全部团队成员排除：它依赖无头成员无法提供的前端用户
// 响应，leader 的中断路径在团队上下文里也不可靠。
// Python: _code_base_rail_names (config_specs.py:652-664)
func codeBaseRailNames(role string) []string {
	names := make([]string, 0, len(codeRailNames))
	for _, name := range codeRailNames {
		if name != PermissionInterrupt {
			names = append(names, name)
		}
	}
	if role == "leader" {
		out := make([]string, 0, len(names))
		for _, name := range names {
			if name != CodeTaskPlanning {
				out = append(out, name)
			}
		}
		return out
	}
	return names
}

// roleEvolutionRails 返回角色专属的技能演进 rails（两 profile 共用）。
// Python: _role_evolution_rails (config_specs.py:667-695)
func roleEvolutionRails(configMap map[string]any, role string) []hschema.RailSpec {
	railList := []hschema.RailSpec{}
	if role == "leader" && config.SymphonyEvolutionEnabled(configMap) {
		railList = append(railList, hschema.RailSpec{Type: SymphonyGraphEvolution, Params: map[string]any{}})
	}
	if !config.SkillEvolutionEnabled(configMap) {
		return railList
	}
	if role == "leader" {
		railList = append(railList,
			hschema.RailSpec{Type: TeamSkillEvolution, Params: teamEvolutionRailParams(configMap)},
			// 无 params：Skill 库与团队可见性元数据路径是按团队解析的运行时值，
			// 来自 build context。spec 构建期不知道团队名，两条路径都烘不进来。
			hschema.RailSpec{Type: TeamSkillCreate, Params: map[string]any{}},
		)
		return railList
	}
	return append(railList, hschema.RailSpec{Type: MemberSkillEvolution, Params: memberEvolutionRailParams(configMap)})
}

// buildTeamCapabilitySpecs 构建聊天团队 profile 的成员 rail/tool 声明。
// Python: _build_team_capability_specs (config_specs.py:698-749)
func buildTeamCapabilitySpecs(configMap map[string]any, mode string, role string, enablePermissions bool) ([]hschema.RailSpec, []hschema.BuiltinToolSpec) {
	names := teamCommonRailNames(role)
	railSpecs := make([]hschema.RailSpec, 0, len(names)+6)
	for _, name := range names {
		railSpecs = append(railSpecs, hschema.RailSpec{Type: name, Params: railParams(name, configMap)})
	}
	if role == "leader" {
		railSpecs = append(railSpecs, hschema.RailSpec{Type: StructuredAskUser})
		railSpecs = append(railSpecs, hschema.RailSpec{Type: SessionMessagingRoute})
		if schema.IsTeamPlanMode(mode) {
			railSpecs = append(railSpecs,
				hschema.RailSpec{Type: CodeAgentMode},
				hschema.RailSpec{Type: TeamPlanApproval},
			)
		}
	}
	railSpecs = append(railSpecs, teamSkillUseRailSpec(configMap, role))
	railSpecs = append(railSpecs, hschema.RailSpec{Type: MemberSkillToolkit})
	if enablePermissions && role == "teammate" {
		railSpecs = append(railSpecs, hschema.RailSpec{Type: TeamPermission, Params: railParams(TeamPermission, configMap)})
	}
	if enablePermissions && role == "leader" {
		railSpecs = append(railSpecs, hschema.RailSpec{Type: TeamPermissionPolicy, Params: railParams(TeamPermissionPolicy, configMap)})
	}
	railSpecs = append(railSpecs, roleEvolutionRails(configMap, role)...)

	toolSpecs := make([]hschema.BuiltinToolSpec, 0, len(commonToolNames)+2)
	for _, name := range commonToolNames {
		toolSpecs = append(toolSpecs, hschema.BuiltinToolSpec{Type: name, Params: toolParams(name, configMap)})
	}
	if role == "leader" {
		toolSpecs = append(toolSpecs,
			hschema.BuiltinToolSpec{Type: SwarmA4PIntentAuthorization},
			hschema.BuiltinToolSpec{Type: SessionMessagingTools},
		)
	}
	return railSpecs, toolSpecs
}

// buildCodeCapabilitySpecs 构建 code profile（code.team / team.plan.code）的成员声明。
// PermissionInterruptRail（swarm.permission_interrupt）在无头团队成员上无法
// 解决 ASK 中断：面向用户的确认路径需要团队成员没有的前端连接。
// enable_permissions=true 时团队权限 rails 取代它——teammate 用
// TeamPermissionRail（leader 中介 ASK 解析），leader 用
// TeamPermissionPolicyRail（提示词段注入）。enable_permissions=false 时权限
// 中断 rail 整个移除：它会让 teammate 在任何 ASK 级工具调用上死锁。
// Python: _build_code_capability_specs (config_specs.py:752-823)
func buildCodeCapabilitySpecs(configMap map[string]any, mode string, role string, enablePermissions bool) ([]hschema.RailSpec, []hschema.BuiltinToolSpec) {
	isTeamPlanLeader := schema.IsTeamPlanMode(mode) && role == "leader"

	names := codeBaseRailNames(role)
	railSpecs := make([]hschema.RailSpec, 0, len(names)+6)
	for _, name := range names {
		railSpecs = append(railSpecs, hschema.RailSpec{Type: name, Params: railParams(name, configMap)})
	}
	if isTeamPlanLeader {
		railSpecs = append(railSpecs, hschema.RailSpec{Type: TeamPlanApproval})
	}
	if role == "leader" {
		railSpecs = append(railSpecs, hschema.RailSpec{Type: SessionMessagingRoute})
	}
	if enablePermissions && role == "teammate" {
		railSpecs = append(railSpecs, hschema.RailSpec{Type: TeamPermission, Params: railParams(TeamPermission, configMap)})
	}
	if enablePermissions && role == "leader" {
		railSpecs = append(railSpecs, hschema.RailSpec{Type: TeamPermissionPolicy, Params: railParams(TeamPermissionPolicy, configMap)})
	}
	// plan-only 审批闸属于 leader。code Team Plan teammate 其余必须与
	// code.team teammate 装配完全一致。
	if !isTeamPlanLeader {
		railSpecs = append(railSpecs, hschema.RailSpec{
			Type:   manifest.ConfirmInterrupt, // Python: registry.CODE_CONFIRM_INTERRUPT = _OJ_CONFIRM_INTERRUPT
			Params: map[string]any{"tool_names": []string{"switch_mode", "exit_plan_mode"}},
		})
	}
	railSpecs = append(railSpecs, teamSkillUseRailSpec(configMap, role))
	railSpecs = append(railSpecs, hschema.RailSpec{Type: MemberSkillToolkit})
	for _, name := range codeSharedRailNames {
		railSpecs = append(railSpecs, hschema.RailSpec{Type: name, Params: railParams(name, configMap)})
	}
	railSpecs = append(railSpecs, roleEvolutionRails(configMap, role)...)

	toolSpecs := make([]hschema.BuiltinToolSpec, 0, len(codeToolNames)+2)
	for _, name := range codeToolNames {
		toolSpecs = append(toolSpecs, hschema.BuiltinToolSpec{Type: name, Params: toolParams(name, configMap)})
	}
	if role == "leader" {
		toolSpecs = append(toolSpecs,
			hschema.BuiltinToolSpec{Type: SwarmA4PIntentAuthorization},
			hschema.BuiltinToolSpec{Type: SessionMessagingTools},
		)
	}
	return railSpecs, toolSpecs
}

// buildDesignCapabilitySpecs 构建 design profile（team.design）的成员声明。
// Python: _build_design_capability_specs (config_specs.py:293-353)
func buildDesignCapabilitySpecs(configMap map[string]any, mode string, role string, enablePermissions bool) ([]hschema.RailSpec, []hschema.BuiltinToolSpec) {
	railNameList := designRailNames
	if role != "leader" {
		railNameList = append(append([]string{}, designRailNames...), manifest.SysOperation)
	}
	railSpecs := make([]hschema.RailSpec, 0, len(railNameList)+5)
	for _, name := range railNameList {
		railSpecs = append(railSpecs, hschema.RailSpec{Type: name, Params: railParams(name, configMap)})
	}
	if role == "leader" {
		railSpecs = append(railSpecs, hschema.RailSpec{Type: StructuredAskUser})
		// leader authoring rail：designer_graph_autorun 工具——全新创作需求走
		// 服务端导演链组装执行图（分析→简报→审核→分镜→审核→建图→校验）。
		// Python: config_specs.py:310-314
		railSpecs = append(railSpecs, hschema.RailSpec{Type: DesignerAuthoring})
		// leader execution rail：swarmflow 工具（画布编译/会话内触发、单节点
		// 重跑、级联重跑）+ 其提示词段。图编辑（designer_graph_tools）与执行是
		// 两条 rail 上的两个工具。
		// Python: config_specs.py:315-319
		railSpecs = append(railSpecs, hschema.RailSpec{Type: DesignerExecution})
		// surface sanitizer：减去 agent-core 无条件挂载的能力面（workspace_meta、
		// org_* 控制工具、群聊）——单仓移除而不是打补丁 agent-core。低 rail
		// 优先级 => 在挂载 rail 之后初始化，看到完整装配面。
		// Python: config_specs.py:320-325
		railSpecs = append(railSpecs, hschema.RailSpec{Type: DesignerSurfaceSanitizer})
		if enablePermissions {
			railSpecs = append(railSpecs, hschema.RailSpec{Type: TeamPermissionPolicy, Params: railParams(TeamPermissionPolicy, configMap)})
		}
	}
	if enablePermissions && role == "teammate" {
		railSpecs = append(railSpecs, hschema.RailSpec{Type: TeamPermission, Params: railParams(TeamPermission, configMap)})
	}

	toolNameList := designToolNames
	if role == "leader" {
		// 画布工具（graph_get / graph_patch / node_run / read_upstream / 模型
		// 调用——选定的 designer 工具集）经注册 provider 挂载，provider 解析
		// 会话的图绑定。不带 send_file：节点输出经画布节点契约
		// （DESIGNER_RUN_UPDATED / graph output_refs）到前端，不走聊天文件投递。
		// Python: config_specs.py:341-348
		toolNameList = append(append([]string{}, designToolNames...), designLeaderToolNames...)
		toolNameList = append(toolNameList, DesignerGraphTools)
	}
	toolSpecs := make([]hschema.BuiltinToolSpec, 0, len(toolNameList))
	for _, name := range toolNameList {
		toolSpecs = append(toolSpecs, hschema.BuiltinToolSpec{Type: name, Params: toolParams(name, configMap)})
	}
	return railSpecs, toolSpecs
}

// isSubagentEnabled react.subagents.<name> 条目是否启用。
// Python: _is_subagent_enabled (config_specs.py:876-878)
func isSubagentEnabled(subCfg any) bool {
	m, ok := subCfg.(map[string]any)
	return ok && m["enabled"] == true
}

// isSubagentDefaultEnabled 子代理未显式禁用即为真。
// Python: _is_subagent_default_enabled (config_specs.py:881-883)
func isSubagentDefaultEnabled(subCfg any) bool {
	m, ok := subCfg.(map[string]any)
	if !ok {
		return true
	}
	enabled, present := m["enabled"]
	if !present {
		return true
	}
	return enabled != false
}

// subagentLanguage 解析 code 子代理运行语言（与 code_runtime_language 同款）。
// code 模式只有 team.plan.code leader 用配置的偏好语言，其余英文。
// 烘进 factory_kwargs，让通用 openjiuwen 子代理 provider 不沾 swarm 模式/角色
// 策略。
// Python: _subagent_language (config_specs.py:886-895)
func subagentLanguage(mode string, role string, configMap map[string]any) string {
	if schema.IsTeamPlanMode(mode) && role == "leader" {
		return prompts.ResolveLanguage(stringOr(configMap["preferred_language"], "zh"))
	}
	return "en"
}

// codeSubagentSpec 为 code 子代理 provider 构建声明式 SubAgentSpec。
// agent_card / system_prompt 是占位：SubAgentSpec.build 短路到注册的 provider
// （按 factory_name），provider 返回完整 SubAgentConfig。
// Python: _code_subagent_spec (config_specs.py:898-942)
func codeSubagentSpec(name string, factoryName string, reactCfg map[string]any, language string) hschema.SubAgentSpec {
	subagentsCfg := configSection(reactCfg, "subagents")
	subCfg := subagentsCfg[name]
	var maxIterations any
	switch name {
	case "browser_agent":
		maxIterations = tools.DefaultBrowserAgentMaxIterations
	case "cua_agent":
		maxIterations = tools.DefaultCuaAgentMaxIterations
	case "statusline-setup":
		maxIterations = DefaultStatuslineSetupMaxIterations
	default:
		maxIterations = reactCfg["max_iterations"]
	}
	if m, ok := subCfg.(map[string]any); ok && m["max_iterations"] != nil {
		maxIterations = m["max_iterations"]
	}
	if maxIterations != nil {
		if n, ok := toInt(maxIterations); ok {
			maxIterations = n
		}
	}
	card := agentschema.AgentCard{BaseCard: cschema.BaseCard{Name: name}}
	if name == "statusline-setup" {
		card.BaseCard.ID = "jiuwenswarm.statusline-setup"
	}
	factoryKwargs := map[string]any{
		"max_iterations": maxIterations,
		"language":       language,
	}
	if name == "cua_agent" {
		// provider 参数名（见 CuaAgentInput）去掉 agent-core 前缀。
		// Python: for key, value in resolve_cua_factory_options(sub_cfg).items()
		for key, value := range tools.ResolveCuaFactoryOptions(subCfg) {
			factoryKwargs[trimPrefix(key, "cua_")] = value
		}
	}
	return hschema.SubAgentSpec{
		AgentCard:     &card,
		SystemPrompt:  "",
		FactoryName:   factoryName,
		FactoryKwargs: factoryKwargs,
	}
}

// isExploreSubagent spec 是否声明 explore 子代理。上游团队 spec 到达时形状
// 不止一种，三个身份载体都查：subagent_type（部分平台 spec 对象有）、
// factory_name、agent card 名。
// Python: _is_explore_subagent (config_specs.py:945-963)
func isExploreSubagent(spec hschema.SubAgentSpec) bool {
	return spec.FactoryName == rails.ExploreAgent ||
		(spec.AgentCard != nil && spec.AgentCard.BaseCard.Name == "explore_agent")
}

// subagentTypeOf 提取子代理的 subagent_type 身份（Python getattr(spec,
// "subagent_type", None)——Go SubAgentSpec 无该字段，经 factory_name /
// card.name 承载；browser_agent 判断对齐 config_specs.py:1163 的消费语义）。
func subagentTypeOf(spec hschema.SubAgentSpec) string {
	if spec.AgentCard != nil && spec.AgentCard.BaseCard.Name == "browser_agent" {
		return "browser_agent"
	}
	if spec.FactoryName == SwarmBrowserAgent {
		return "browser_agent"
	}
	return ""
}

// mergeMcpConfigs 合并基础与 config 来源的 MCP servers：按 server_id 与
// server_name 双键去重，config 侧深拷贝后追加。
// Python: _merge_mcp_configs (config_specs.py:1208-1234)
func mergeMcpConfigs(baseMcps []*types.McpServerConfig, configMcps []*types.McpServerConfig) []*types.McpServerConfig {
	merged := append([]*types.McpServerConfig{}, baseMcps...)
	if len(configMcps) == 0 {
		if len(merged) == 0 {
			return nil
		}
		return merged
	}
	existingIDs := map[string]bool{}
	existingNames := map[string]bool{}
	for _, cfg := range merged {
		if id := strings.TrimSpace(cfg.ServerID); id != "" {
			existingIDs[id] = true
		}
		if name := strings.TrimSpace(cfg.ServerName); name != "" {
			existingNames[name] = true
		}
	}
	for _, cfg := range configMcps {
		serverID := strings.TrimSpace(cfg.ServerID)
		serverName := strings.TrimSpace(cfg.ServerName)
		if (serverID != "" && existingIDs[serverID]) || (serverName != "" && existingNames[serverName]) {
			continue
		}
		copied := *cfg // Python model_copy(deep=True)——McpServerConfig 字段全值语义
		merged = append(merged, &copied)
		if serverID != "" {
			existingIDs[serverID] = true
		}
		if serverName != "" {
			existingNames[serverName] = true
		}
	}
	if len(merged) == 0 {
		return nil
	}
	return merged
}

// stringifySkill 任意技能名的字符串化（Python str(skill)）。
func stringifySkill(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// stringOr 首个非空字符串（Python (config or {}).get("preferred_language", "zh") 的缺省语义）。
func stringOr(v any, defaultVal string) string {
	if s, ok := v.(string); ok && s != "" {
		return s
	}
	return defaultVal
}

// toInt 宽松整数转换（Python int(x)）。
func toInt(v any) (int, bool) {
	switch t := v.(type) {
	case int:
		return t, true
	case int64:
		return int(t), true
	case float64:
		return int(t), true
	}
	return 0, false
}

// trimPrefix strings.TrimPrefix 别名（Python str.removeprefix）。
func trimPrefix(s, prefix string) string {
	return strings.TrimPrefix(s, prefix)
}
