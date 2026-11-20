// Package providers 提供 swarm 平台能力 provider（config 声明的 rails/tools/subagents
// 的构造函数），经 harness provider 注册表把 DeepAgentSpec 声明物化为活实例。
//
// 每个 provider 是 `factory(params, context) -> rail|list|nil`，由构建期以
// 每成员 SwarmBuildContext 调用；返回 nil/空列表表示「该成员跳过此 rail」
// （config 门控）。provider 优先于同名类注册。
//
// 对应 Python 代码：jiuwenswarm/agents/swarm/providers/
// 注册入口：registry.go RegisterSwarmProviders（对齐 registry.py register_swarm_providers）
//
// 文件目录：
//
//	providers/
//	├── doc.go              # 包文档
//	├── member_rails.go    # 15 个成员 rail 工厂（member_rails.py）
//	├── code_rails.go      # code profile 12 个 rail 工厂（code_rails.py）
//	├── evolution_rails.go # 技能演进三 rail 子类（evolution_rails.py）
//	├── code_subagents.go  # 四个子代理工厂（code_subagents.py）
//	├── runtime_tools.go   # 组织/cron/send_file/session_messaging 工具（runtime_tools.py）
//	├── tools.go           # ~20 个工具工厂 + 可见性过滤（tools.py）
//	├── skills.go          # 成员技能可见性（skills.py）
//	├── design_rails.go    # designer 三 rail 壳（design_rails.py，基座缝 F4）
//	├── designer_tools.go  # designer 工具壳（designer_tools.py，基座缝 F4）
//	└── builtin_rails.go  # response_prompt/stream_event 等包装（builtin_rails.py）
//
// 循环依赖拆解（偏差 D-15.16f-view）：Python providers 直接 import 父包的
// SwarmBuildContext；Go 侧父包 registry.go 需调用本包注册函数（规则 9 目录
// 1:1），为避免 providers↔swarm 成环，本包定义 SwarmContextView 窄接口，
// SwarmBuildContext 以访问器方法隐式实现（Python 无对应方法，属性直读）。
package providers

import (
	"os"
	"path/filepath"

	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	atrails "github.com/uapclaw/uapclaw-go/internal/agent_teams/rails"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	swarmrails "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/rails"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SwarmContextView providers 工厂所需的平台构建上下文视图。
//
// *swarm.SwarmBuildContext 隐式实现（父包加访问器方法，偏差 D-15.16f-view）；
// ctx 不是 SwarmBuildContext 时工厂按「上下文不可用」走各自 nil 门控。
type SwarmContextView interface {
	hschema.BuildContextInterface

	// ViewRole 成员角色（leader/teammate）。Python: context.role
	ViewRole() string
	// ViewTeamID 团队名。Python: context.team_id
	ViewTeamID() string
	// ViewSessionID 会话 id。Python: context.session_id
	ViewSessionID() string
	// ViewChannelID 渠道 id。Python: context.channel_id
	ViewChannelID() string
	// ViewChannel 渠道键。Python: context.channel
	ViewChannel() string
	// ViewMode 请求模式。Python: context.mode
	ViewMode() string
	// ViewUserID 请求属主。Python: context.user_id
	ViewUserID() string
	// ViewMemberCardID 成员卡片 id。Python: context.member_card_id
	ViewMemberCardID() string
	// ViewMemberName 成员名。Python: context.member_name
	ViewMemberName() string
	// ViewTeamSkillVisibilityPath 团队技能可见性文件路径。
	// Python: context.team_skill_visibility_path
	ViewTeamSkillVisibilityPath() string
	// ViewRequestMetadata 请求元数据。Python: context.request_metadata
	ViewRequestMetadata() map[string]any
	// ViewHeartbeatJobService 心跳作业服务句柄（any 别名，D-15.16-c2③）。
	// Python: context.heartbeat_job_service
	ViewHeartbeatJobService() any
	// ViewConfig config.yaml 映射。Python: context.config
	ViewConfig() map[string]any
	// ViewProjectDir 用户项目目录。Python: context.project_dir
	ViewProjectDir() string
	// ViewTrustedDirs 客户端声明的受信目录。Python: context.trusted_dirs
	ViewTrustedDirs() []string
	// ViewGlobalSkillsDir 全局 Skill 库目录。Python: context.global_skills_dir
	ViewGlobalSkillsDir() string
	// ViewTeamWsRoot 团队共享工作区根。Python: context.team_ws_root
	ViewTeamWsRoot() string
	// ViewTeamOutputsDir 项目less 团队交付物目录。Python: context.team_outputs_dir
	ViewTeamOutputsDir() string
	// ViewTaskWorkspaceRoot 共享交付物根。Python: context.task_workspace_root
	ViewTaskWorkspaceRoot() string
	// ViewLanguage 成员语言码。Python: context.language
	ViewLanguage() string
	// ViewMemberWorkspaceRoot 成员工作区根（workspace.root_path）。
	// Python: _workspace_root(ctx)（member_rails.py:214-218）
	ViewMemberWorkspaceRoot() string
	// ViewResolveMemberWorkDir 解析成员工作目录。
	// Python: context.resolve_member_work_dir()
	ViewResolveMemberWorkDir() (string, error)
	// ViewResolveMemberSkillVisibilityPath 解析成员技能可见性文件路径。
	// Python: context.resolve_member_skill_visibility_path()
	ViewResolveMemberSkillVisibilityPath() string
}

// SwarmContextOf 从构建上下文取出平台视图；非平台上下文返回 nil。
func SwarmContextOf(ctx hschema.BuildContextInterface) SwarmContextView {
	if view, ok := ctx.(SwarmContextView); ok {
		return view
	}
	return nil
}

// railProviderAdapter 把具体工厂适配成 hschema.RailProvider 签名，
// 并在上下文非平台形态时返回 nil（跳过该 rail）。
func railProviderAdapter(
	build func(params map[string]any, view SwarmContextView) (any, error),
) hschema.RailProvider {
	return func(params map[string]any, ctx hschema.BuildContextInterface) (any, error) {
		view := SwarmContextOf(ctx)
		if view == nil {
			return nil, nil
		}
		return build(params, view)
	}
}

// ──────────────────────────── 常量 ────────────────────────────

// 以下注册名常量与 registry.go 常量层同源（Python member_rails.py:71-116）。
// 为避免双真源，这里只声明包内引用别名；导出常量以 registry.go 为准。
const (
	constPersonalContext       = "swarm.personal_context"
	constRuntimePrompt         = "swarm.runtime_prompt"
	constContextProcessor      = "swarm.context_processor"
	constModelAnomalyDetection = "swarm.model_anomaly_detection"
	constPluginRails           = "swarm.plugin_rails"
	constSkillRetrievalPrompt  = "swarm.skill_retrieval_prompt"
	constSymphonyOrchestration = "swarm.symphony_orchestration_prompt"
	constHeartbeat             = "swarm.heartbeat"
	constSessionMessagingRoute = "swarm.session_messaging_route"
)

// ──────────────────────────── 非导出函数 ────────────────────────────

// buildOrganizationOwnerInboxRail Owner 有序持久结果注入（leader-only）。
// 基座 OrganizationOwnerInboxRail Go 未实施 ⤴️15.20（成员 rail 基座缺口批）。
// Python: _build_organization_owner_inbox_rail (member_rails.py:87-93)
func buildOrganizationOwnerInboxRail(params map[string]any, view SwarmContextView) (any, error) {
	_ = params
	if view.ViewRole() != "leader" {
		return nil, nil
	}
	// Python: return OrganizationOwnerInboxRail(context.team_id, context.session_id)
	return nil, nil
}

// buildOrganizationModelRail 组织专家 Team 持久模型选择应用。
// 基座 OrganizationModelRail Go 未实施 ⤴️15.20。
// Python: _build_organization_model_rail (member_rails.py:97-103)
func buildOrganizationModelRail(params map[string]any, view SwarmContextView) (any, error) {
	_ = params
	_ = view.ViewTeamID()
	_ = view.ViewSessionID()
	_ = view.ViewRole()
	return nil, nil
}

// buildOrganizationAvailabilityRail 组织 leader 的 AgentGroup 可用性暴露。
// 基座 OrganizationAvailabilityRail Go 未实施 ⤴️15.20。
// Python: _build_organization_availability_rail (member_rails.py:108-112)
func buildOrganizationAvailabilityRail(params map[string]any, view SwarmContextView) (any, error) {
	_ = params
	return nil, nil
}

// buildSessionMessagingRouteRail 仅 Team leader 挂载 Session messaging 路由。
// 分支体（SessionMessagingRouteRail）⤴️25.6 群聊（缝 F2）。
// Python: _build_session_messaging_route_rail (member_rails.py:127-137)
func buildSessionMessagingRouteRail(params map[string]any, view SwarmContextView) (any, error) {
	_ = params
	if view.ViewRole() != "leader" {
		return nil, nil
	}
	// Python: return SessionMessagingRouteRail()——群聊域基座未实施
	return nil, nil
}

// buildHeartbeatRail 为 Team 成员挂载 Job Heartbeat Rail。
//
// 服务由 AgentServer 进程持有；provider 只把成员工具绑定到不可变 Team
// 会话身份，不创建调度器、不使用废弃的 RunKind.HEARTBEAT rail。
// cron 执行会话不挂心跳工具（三个判定信号与 swarm.cron_tools 对齐）；
// 无心跳支持投递路径的渠道（小艺等，见 heartbeat_policy）同样不挂。
//
// Go 基座：HeartbeatRail 已有（agentcore/harness/rails/heartbeat.go），
// 但其构造为 NewHeartbeatRail()（service/context 绑定段未实现）；
// is_heartbeat_supported / is_cron_execution_session 助手 ⤴️15.20。
// Python: _build_heartbeat_rail (member_rails.py:143-196)
func buildHeartbeatRail(params map[string]any, view SwarmContextView) (any, error) {
	_ = params
	service := view.ViewHeartbeatJobService()
	sessionID := trimSpaceOrEmpty(view.ViewSessionID())
	if service == nil || sessionID == "" {
		return nil, nil
	}
	metadata := copyMapOrEmpty(view.ViewRequestMetadata())
	// cron 执行会话不挂心跳工具（心跳任务绑定创建它的会话，从 cron 运行里
	// 再派生心跳任务与「禁止 cron 派生 cron」同理）。
	if mapKeyTruthy(metadata, "cron") ||
		trimSpaceOrEmpty(view.ViewChannelID()) == "__cron__" ||
		isCronExecutionSession(sessionID) {
		logHeartbeatSkip("cron execution session", sessionID)
		return nil, nil
	}
	// 小艺等渠道没有可用的主动投递路径（heartbeat_policy），不挂心跳工具，
	// 否则成员会回答「心跳已创建」而结果永远送不到手机。
	if !isHeartbeatSupported(view.ViewChannelID()) {
		logHeartbeatSkip("channel without heartbeat support", sessionID)
		return nil, nil
	}
	// Python: HeartbeatRail(service=service, context=tool_context)——
	// Go HeartbeatRail 构造期 service/context 绑定段 ⤴️15.20（对齐 :183-196
	// 的 SimpleNamespace tool_context 形状），本批仅保留门控逻辑。
	return nil, nil
}

// buildPersonalContextRail 个人上下文 rail（disabled 时也挂载，让活成员
// 观察到后续开关变化）。
// 基座 PersonalContextRail（harness/rails/personal_context.py 360 行）Go
// 未实施 ⤴️15.20。
// Python: _build_personal_context_rail (member_rails.py:201-212)
func buildPersonalContextRail(params map[string]any, view SwarmContextView) (any, error) {
	_ = params
	_ = view
	// Python: PersonalContextRail(Path.home() / ".jiuwenswarm" / ".personal_context")，
	// 构造失败 warn 后返回 None（optional rail）。
	home, err := os.UserHomeDir()
	if err != nil {
		logPersonalContextSkip(err)
		return nil, nil
	}
	_ = filepath.Join(home, ".jiuwenswarm", ".personal_context")
	return nil, nil
}

// buildSkillRetrievalPromptRail Skill 目录检索提示 rail（开关门控）。
// 基座 SkillRetrievalPromptRail（skill_retrieval_prompt_rail.py 582 行）与
// skill_retrieval_toolkit 消费段已有承接章节 9.11b（前置件清单含同名文件）
// ⤴️9.11b；is_skill_retrieval_enabled 助手随 tools.go（Task 5）落地。
// Python: _build_skill_retrieval_prompt_rail (member_rails.py:246-272)
func buildSkillRetrievalPromptRail(params map[string]any, view SwarmContextView) (any, error) {
	_ = params
	if !isSkillRetrievalEnabled(view.ViewConfig()) {
		return nil, nil
	}
	// Python: toolkit = skill_retrieval_toolkit_for_context(context);
	// SkillRetrievalPromptRail(toolkit=..., session_scope=..., config_base=...)
	// 基座 ⤴️9.11b。
	return nil, nil
}

// buildSymphonyOrchestrationRail leader-only Symphony 编排提示 rail。
// 基座 SymphonyOrchestrationRail（symphony/orchestration_rail.py 1149 行）
// 归领域十六 ⤴️领域十六。
// Python: _build_symphony_orchestration_rail (member_rails.py:277-284)
func buildSymphonyOrchestrationRail(params map[string]any, view SwarmContextView) (any, error) {
	_ = params
	if view.ViewRole() != "leader" {
		return nil, nil
	}
	return nil, nil
}

// buildRuntimePromptRail 成员运行时提示 rail（绑定成员语言与渠道）。
//
// 分支语义（Python :338-379）：有项目或无 task 路径 → else 前分支
// （cwd=project_dir 或成员工作区根）；无项目 + task_workspace_root 与
// team_outputs_dir 齐 → projectless 分支（cwd=resolve_member_work_dir()），
// work_dir 为 None 属内部不变量违反，显式报错。
//
// Go 基座 RuntimePromptRail（common/rails/runtime_prompt_rail.go 847 行）
// 缺 set_session_id 与 set_runtime_paths 扩展形参（task_workspace_root/
// task_work_dir/task_outputs_dir）⤴️15.20（runtime_prompt rail 全量对齐）；
// 本工厂保留完整分支结构与路径解析，已支持形参全部传入。
// Python: _build_runtime_prompt_rail (member_rails.py:321-384)
func buildRuntimePromptRail(params map[string]any, view SwarmContextView) (any, error) {
	_ = params
	// RuntimePromptInput.resolve（:275-298 的 context_field 全集）
	language := stringFieldOrEmpty(view.ViewLanguage(), "cn")
	channel := stringFieldOrEmpty(view.ViewChannel(), "default")
	projectDir := view.ViewProjectDir()
	memberWorkspaceRoot := view.ViewMemberWorkspaceRoot()
	taskWorkspaceRoot := view.ViewTaskWorkspaceRoot()
	teamOutputsDir := view.ViewTeamOutputsDir()
	trustedDirs := view.ViewTrustedDirs()

	rail := swarmrails.NewRuntimePromptRail(language, channel)
	// 团队成员用自己的 RuntimePromptRail 实例：绑定发起请求，运行态从
	// 活会话读取而非进程级 default 状态文件。
	// Python: rail.set_mode(context.mode); rail.set_session_id(context.session_id)
	rail.SetMode(view.ViewMode())
	// rail.set_session_id —— 基座缺 ⤴️15.20

	// Python :346-379 分支：
	if projectDir != "" || teamOutputsDir == "" || taskWorkspaceRoot == "" {
		// else 前分支：有项目或无 task 路径
		// Python: rail.set_runtime_paths(cwd=..., project_dir=..., workspace_dir=...)
		rail.SetRuntimePaths(orEmpty(projectDir, memberWorkspaceRoot), projectDir)
	} else {
		workDir, err := view.ViewResolveMemberWorkDir()
		if err != nil {
			return nil, err
		}
		if workDir == "" {
			// Python: RuntimeError（显式 raise 而非 assert，:365-369）
			return nil, errRuntimePromptInvariant
		}
		// Python: set_runtime_paths(cwd=work_dir, project_dir=..., workspace_dir=...,
		//   task_workspace_root=..., task_work_dir=..., task_outputs_dir=...)
		// 扩展形参 ⤴️15.20；本批先传已支持形参。
		rail.SetRuntimePaths(workDir, projectDir)
	}
	// 没有这一步成员永远渲染不出 trusted_dirs 策略段。
	rail.SetTrustedDirs(trustedDirs)
	return rail, nil
}

// buildTeamSkillStoragePolicyRail 团队 Skill 存储策略 rail（Skill 库已知时）。
// 基座 TeamSkillStoragePolicyRail（team_skill_storage_policy_rail.py 140 行）
// Go 未实施 ⤴️15.20。
// Python: _build_team_skill_storage_policy_rail (member_rails.py:439-452)
func buildTeamSkillStoragePolicyRail(params map[string]any, view SwarmContextView) (any, error) {
	_ = params
	if view.ViewGlobalSkillsDir() == "" {
		return nil, nil
	}
	// Python: TeamSkillStoragePolicyRail(global_skills_dir=..., team_workspace_root=...,
	//   team_skill_visibility_file=...)——基座 ⤴️15.20
	return nil, nil
}

// buildTeamSkillLibraryReloadRail Skill 库写入后重载成员 Skill 视图。
// 基座 TeamSkillLibraryReloadRail（team_skill_library_reload_rail.py 147 行）
// Go 未实施 ⤴️15.20。
// Python: _build_team_skill_library_reload_rail (member_rails.py:483-495)
func buildTeamSkillLibraryReloadRail(params map[string]any, view SwarmContextView) (any, error) {
	_ = params
	if view.ViewGlobalSkillsDir() == "" {
		return nil, nil
	}
	return nil, nil
}

// buildTeamWorkspaceReportPathRail 团队工作区报告路径重写 rail。
// 基座 TeamWorkspaceReportPathRail 已有（team/workspace_report_path_rail.go）。
// Python: _build_team_workspace_report_path_rail (member_rails.py:549-568)
func buildTeamWorkspaceReportPathRail(params map[string]any, view SwarmContextView) (any, error) {
	if view.ViewTeamWsRoot() == "" {
		return nil, nil
	}
	// Python 构造：root_dir/project_dir/outputs_dir/team_id/language（:556-563）
	// Go 基座构造签名（rootDir, teamID, language）；project_dir/outputs_dir
	// 注入段 ⤴️15.20（基座扩展形参）。
	// 基座 NewTeamWorkspaceReportPathRail 在 team 包（同环），注册段注入。
	if workspaceReportPathCtor == nil {
		return nil, nil
	}
	rail := workspaceReportPathCtor(view.ViewTeamWsRoot(), view.ViewTeamID(), stringFieldOrEmpty(view.ViewLanguage(), "cn"))
	// Python: rail.bind_swarm_context(context)——基座缺 ⤴️15.20
	_ = rail
	return rail, nil
}

// buildContextProcessorRail 上下文压缩 rail（config 开关门控）。
// 基座 BuildContextProcessorRail 已有（team/runtime_inheritance.go:315，
// 即 Python team_runtime_inheritance._build_context_processor_rail）。
// Python: _build_context_processor (member_rails.py:597-611)
func buildContextProcessorRail(params map[string]any, view SwarmContextView) (any, error) {
	// ContextProcessorInput.resolve（:583-592：param_field 两个）
	enabled := boolParamOr(params, "context_engine_enabled", true)
	if !enabled {
		return nil, nil
	}
	ctxCfg, _ := params["context_engine_config"].(map[string]any)
	// 基座 BuildContextProcessorRail 在 team 包（team→swarm 成环），经
	// SetContextProcessorRailBuilder 注册段注入（偏差 D-15.16f-view 的配套缝）。
	if contextProcessorRailBuilder == nil {
		return nil, nil
	}
	rail := contextProcessorRailBuilder(map[string]any{"context_engine_config": ctxCfg})
	if rail == nil {
		return nil, nil
	}
	return rail, nil
}

// buildModelAnomalyDetectionRail 模型异常检测 rail（execution_guard 门控）。
// 基座 BuildModelAnomalyDetectionRail 已有（team/runtime_inheritance.go:258，
// 注释即对齐 Python member_rails.py:613）。
// Python: _build_model_anomaly_detection (member_rails.py:632-660)
func buildModelAnomalyDetectionRail(params map[string]any, view SwarmContextView) (any, error) {
	_ = view
	railCfg, _ := params["rail_config"].(map[string]any)
	if railCfg == nil {
		railCfg = map[string]any{}
	}
	if enabled, _ := railCfg["enabled"].(bool); !enabled {
		return nil, nil
	}
	// Python: ModelAnomalyDetectionRail(**rail_cfg 派生参数)；构造失败 warn + nil。
	// Go 基座从 config.yaml 全量读（BuildModelAnomalyDetectionRail(config)），
	// 与 Python 从 params.rail_config 读的入口差异登记为偏差
	// D-15.16f-mad-entry（上游 rail_config 由 config_specs 烘焙进 params，
	// Go 侧 runtime_inheritance 基座直读 config——数据同源）。
	return buildModelAnomalyDetectionFromParams(railCfg), nil
}

// buildPluginRails 用户注册的扩展 rail：每个已注册 rail 扩展一个新实例。
// 基座 rail_manager（common/plugins/rail_manager.py）Go 未实施 ⤴️15.20
// （扩展包管理 D-e12 同域）。
// Python: _build_plugin_rails (member_rails.py:671-698)
func buildPluginRails(params map[string]any, view SwarmContextView) (any, error) {
	_ = params
	_ = view
	// Python: get_rail_manager().get_registered_rail_names() 逐个
	// load_rail_instance_without_enabled_check，失败 warn 跳过。
	return []agentinterfaces.AgentRail{}, nil
}

// buildTeamPermissionPolicyRail leader 的权限策略提示 rail。
// 基座 TeamPermissionPolicyRail（team_permission_policy_rail.py 75 行）
// Go 未实施 ⤴️15.20。
// Python: _build_team_permission_policy_rail (member_rails.py:783-797)
func buildTeamPermissionPolicyRail(params map[string]any, view SwarmContextView) (any, error) {
	permissionsConfig, _ := params["permissions_config"].(map[string]any)
	if permissionsConfig == nil {
		permissionsConfig = map[string]any{}
	}
	if enabled, _ := permissionsConfig["enabled"].(bool); !enabled {
		return nil, nil
	}
	// Python: TeamPermissionPolicyRail(permissions_config=..., language=...)
	// ——基座 ⤴️15.20
	_ = stringFieldOrEmpty(view.ViewLanguage(), "cn")
	return nil, nil
}

// buildTeamPermissionRail 团队权限护栏（backend + messager + 权限开启门控）。
//
// 窄 provider：permissions_config 从 RailSpec.params 读（config_specs 烘焙），
// 运行时句柄从 BuildContext.extras 读（AgentConfigurator 注入）；权限本体
// （PermissionEngine/TeamPermissionRail/TeamApprovalOrchestrator）在
// openjiuwen。Go 侧 TeamPermissionRail 已在 agent_teams/rails 声明；
// TeamApprovalOrchestrator / narrow_permissions 助手 ⤴️15.20。
// Python: _build_team_permission_rail (member_rails.py:813-819)
func buildTeamPermissionRail(params map[string]any, view SwarmContextView) (any, error) {
	backend := atrails.GetTeamBackend(view)
	messager := atrails.GetMessager(view)
	if backend == nil || messager == nil {
		return nil, nil
	}
	permissionsConfig, _ := params["permissions_config"].(map[string]any)
	if permissionsConfig == nil {
		permissionsConfig = map[string]any{}
	}
	if enabled, _ := permissionsConfig["enabled"].(bool); !enabled {
		return nil, nil
	}
	// Python :818-819（ narrowed_config = narrow_permissions(...) ）、
	// TeamMessageManager + TeamApprovalOrchestrator + ToolPermissionHost 组装段
	// ——orchestrator/narrowing 助手 ⤴️15.20（TeamMessageManager 已有）。
	return nil, nil
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// contextProcessorRailBuilder 上下文压缩 rail 构造（team 包基座，注册段注入）。
	contextProcessorRailBuilder func(config map[string]any) any
	// workspaceReportPathCtor 团队工作区报告路径 rail 构造（同上）。
	workspaceReportPathCtor func(rootDir, teamID, language string) any
)

// SetContextProcessorRailBuilder 注入上下文压缩 rail 构造（父包注册段调用；
// 偏差 D-15.16f-view 配套缝——team 基座经父包注入规避 providers↔team 成环）。
func SetContextProcessorRailBuilder(fn func(config map[string]any) any) {
	contextProcessorRailBuilder = fn
}

// SetWorkspaceReportPathCtor 注入团队工作区报告路径 rail 构造。
func SetWorkspaceReportPathCtor(fn func(rootDir, teamID, language string) any) {
	workspaceReportPathCtor = fn
}

// RegisterMemberRailProviders 注册 member_rails 全部 provider。
// 清单顺序按 member_rails.py 声明序（registry.py:50 import 序）。
// Python: member_rails.py 全部 @harness_element(kind=RAIL) 段。
func RegisterMemberRailProviders() {
	// Python 声明序：organization 三件 → session_messaging_route → heartbeat
	// → personal_context → skill_retrieval_prompt → symphony_orchestration
	// → runtime_prompt → team_skill_storage_policy → team_skill_library_reload
	// → team_workspace_report_path → context_processor → model_anomaly_detection
	// → plugin_rails → team_permission_policy → team_permission
	hschema.RegisterRailProvider("swarm.organization_owner_inbox",
		railProviderAdapter(buildOrganizationOwnerInboxRail))
	hschema.RegisterRailProvider("swarm.organization_model",
		railProviderAdapter(buildOrganizationModelRail))
	hschema.RegisterRailProvider("swarm.organization_availability",
		railProviderAdapter(buildOrganizationAvailabilityRail))
	hschema.RegisterRailProvider(constSessionMessagingRoute,
		railProviderAdapter(buildSessionMessagingRouteRail))
	hschema.RegisterRailProvider(constHeartbeat,
		railProviderAdapter(buildHeartbeatRail))
	hschema.RegisterRailProvider(constPersonalContext,
		railProviderAdapter(buildPersonalContextRail))
	hschema.RegisterRailProvider(constSkillRetrievalPrompt,
		railProviderAdapter(buildSkillRetrievalPromptRail))
	hschema.RegisterRailProvider(constSymphonyOrchestration,
		railProviderAdapter(buildSymphonyOrchestrationRail))
	hschema.RegisterRailProvider(constRuntimePrompt,
		railProviderAdapter(buildRuntimePromptRail))
	hschema.RegisterRailProvider("swarm.team_skill_storage_policy",
		railProviderAdapter(buildTeamSkillStoragePolicyRail))
	hschema.RegisterRailProvider("swarm.team_skill_library_reload",
		railProviderAdapter(buildTeamSkillLibraryReloadRail))
	hschema.RegisterRailProvider("swarm.team_workspace_report_path",
		railProviderAdapter(buildTeamWorkspaceReportPathRail))
	hschema.RegisterRailProvider(constContextProcessor,
		railProviderAdapter(buildContextProcessorRail))
	hschema.RegisterRailProvider(constModelAnomalyDetection,
		railProviderAdapter(buildModelAnomalyDetectionRail))
	hschema.RegisterRailProvider(constPluginRails,
		railProviderAdapter(buildPluginRails))
	hschema.RegisterRailProvider("swarm.team_permission_policy",
		railProviderAdapter(buildTeamPermissionPolicyRail))
	hschema.RegisterRailProvider("swarm.team_permission",
		railProviderAdapter(buildTeamPermissionRail))
}

// ──────────────────────────── 非导出函数（追加段） ────────────────────────────

// buildModelAnomalyDetectionFromParams 从 params.rail_config 构造模型异常
// 检测 rail（对齐 Python :641-660 的逐参数派生；构造失败 warn + nil）。
// 基座 rails.NewModelAnomalyDetectionRail（agentcore/harness/rails）。
// Python: _build_model_anomaly_detection 的构造段 (member_rails.py:641-660)
func buildModelAnomalyDetectionFromParams(railCfg map[string]any) any {
	opts := []rails.ModelAnomalyOption{
		rails.WithMaxRetries(mapIntOr(railCfg, "max_retries", 2)),
		rails.WithRepeatMinPatternChars(mapIntOr(railCfg, "repeat_min_pattern_chars", 2)),
		rails.WithRepeatMaxPatternChars(mapIntOr(railCfg, "repeat_max_pattern_chars", 64)),
		rails.WithRepeatMinCount(mapIntOr(railCfg, "repeat_min_count", 6)),
		rails.WithRepeatMinTotalChars(mapIntOr(railCfg, "repeat_min_total_chars", 160)),
		rails.WithRepeatWindowChars(mapIntOr(railCfg, "repeat_window_chars", 1024)),
		rails.WithSingleCharRepeatCount(mapIntOr(railCfg, "single_char_repeat_count", 100)),
	}
	if compact, ok := railCfg["tool_loop_compact"].(map[string]any); ok && compact != nil {
		compactEnabled, _ := compact["enabled"].(bool)
		opts = append(opts, rails.WithToolLoopCompact(rails.ToolLoopCompactConfig{
			Enabled:              compactEnabled,
			ConsecutiveThreshold: mapIntOr(compact, "consecutive_threshold", 4),
			BailoutThreshold:     mapIntOr(compact, "bailout_threshold", 3),
		}))
	}
	rail, err := rails.NewModelAnomalyDetectionRail(opts...)
	if err != nil {
		logger.Warn(logger.ComponentAgentServer).
			Err(err).
			Msg("[SwarmRails] ModelAnomalyDetectionRail create failed")
		return nil
	}
	return rail
}

// mapIntOr 读取 map 整数字段（兼容 int 与 JSON float64）。
func mapIntOr(m map[string]any, key string, fallback int) int {
	switch typed := m[key].(type) {
	case int:
		return typed
	case float64:
		return int(typed)
	default:
		return fallback
	}
}
