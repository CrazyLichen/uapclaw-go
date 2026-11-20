package providers

import (
	hp "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/common/hooks"
	coderails "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/code/rails"
	swarmrails "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/rails"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// code_rails.go —— code profile 成员 rail 工厂。
//
// Python: jiuwenswarm/agents/swarm/providers/code_rails.py（596 行）。
// 常量值（code_rails.py:47-61）已由 registry.go 常量层承载；本文件只声明
// 包内引用别名。基座缺口（AgentMdPromptRail/WorkAgentModeRail/
// CodeAgentModeRail/PlanApprovalInterruptRail/独立 create_coding_memory_rail/
// interrupt_helpers 权限轨构建段）⤴️15.20。

// ──────────────────────────── 常量 ────────────────────────────

const (
	constCodeRuntimePrompt   = "swarm.code_runtime_prompt"
	constCodeProjectMemory   = "swarm.code_project_memory"
	constCodeAgentMd         = "swarm.code_agent_md"
	constPermissionInterrupt = "swarm.permission_interrupt"
	constCodeCodingMemory    = "swarm.code_coding_memory"
	constCodeAgentMode       = "swarm.code_agent_mode"
	constTeamPlanApproval    = "swarm.team_plan_approval"
	constStructuredAskUser   = "swarm.structured_ask_user"
	constCodeTaskPlanning    = "swarm.code_task_planning"
	constCodeAgentRail       = "swarm.code_agent_rail"
	constUserHooks           = "swarm.user_hooks"

	// CodingMemoryExtrasKey ctx.extras 键：主 agent 的 CodingMemoryRail
	// 发布给 code_agent 子代理复用同一实例。
	// Python: CODING_MEMORY_EXTRAS_KEY (code_rails.py:61)
	CodingMemoryExtrasKey = "_coding_memory_rail"
)

// ──────────────────────────── 非导出函数 ────────────────────────────

// teamPlanExitNotificationEN 团队计划退出通知（英文）。
// Python: _TEAM_PLAN_EXIT_NOTIFICATION_EN (code_rails.py:63-68)
const teamPlanExitNotificationEN = "<system-reminder>\n" +
	"The user approved the team plan. Continue as the Team Leader inside the\n" +
	"team runtime. Do not implement directly as a single agent. Organize the team,\n" +
	"delegate the approved work, and coordinate the members through completion.\n" +
	"</system-reminder>"

// teamPlanExitNotificationCN 团队计划退出通知（中文）。
// Python: _TEAM_PLAN_EXIT_NOTIFICATION_CN (code_rails.py:70-75)
const teamPlanExitNotificationCN = "<system-reminder>\n" +
	"用户已批准团队计划。请继续作为 Team Leader 工作，不要由 Leader 独自完成全部任务。\n" +
	"立即按已批准的计划组织团队协作、分派工作并推动成员完成交付。\n" +
	"</system-reminder>"

// isTeamPlanLeader 是否 normal/code Team Plan leader。
// Python: _is_team_plan_leader (code_rails.py:78-80)
func isTeamPlanLeader(view SwarmContextView) bool {
	return schema.IsTeamPlanMode(view.ViewMode()) && view.ViewRole() == "leader"
}

// CodeRuntimeLanguage 解析 code 成员运行时提示语言。
//
// code 模式英文独占，唯 team.plan.code leader 用配置偏好语言
// （镜像旧 force_english_runtime_prompt / team_plan_runtime_language 处理）。
// Python: code_runtime_language (code_rails.py:83-99)
func CodeRuntimeLanguage(view SwarmContextView) string {
	if isTeamPlanLeader(view) {
		return hp.ResolveLanguage(preferredLanguage(view))
	}
	return "en"
}

// StructuredAskUserLanguage 解析 team/code profile 的 StructuredAskUserRail 语言。
// Python: structured_ask_user_language (code_rails.py:102-108)
func StructuredAskUserLanguage(view SwarmContextView) string {
	if view.ViewRole() == "leader" &&
		(schema.IsTeamMode(view.ViewMode()) || schema.IsTeamPlanMode(view.ViewMode())) {
		return hp.ResolveLanguage(preferredLanguage(view))
	}
	return CodeRuntimeLanguage(view)
}

// preferredLanguage 读 config.preferred_language（缺省 zh，对齐 Python get("preferred_language", "zh")）。
func preferredLanguage(view SwarmContextView) string {
	cfg := view.ViewConfig()
	if cfg == nil {
		return "zh"
	}
	if v, ok := cfg["preferred_language"].(string); ok && v != "" {
		return v
	}
	return "zh"
}

// codeProjectDir 从构建上下文解析 code 项目目录。
// Python: _project_dir (code_rails.py:111-117)
func codeProjectDir(view SwarmContextView) string {
	return orEmpty(view.ViewProjectDir(), view.ViewMemberWorkspaceRoot(), "./")
}

// codeAgentWorkspaceDir 解析 CodeAgentRail 工作区目录（工作区根或项目目录）。
// Python: _code_agent_workspace_dir (code_rails.py:122-125)
func codeAgentWorkspaceDir(view SwarmContextView) string {
	return orEmpty(view.ViewMemberWorkspaceRoot(), codeProjectDir(view))
}

// buildCodeRuntimePrompt 构建 code RuntimePromptRail（英文或 plan leader 配置语言）。
//
// code 成员需要与常规 team profile 同款的按请求运行时绑定，否则 mode 回落
// "unknown"（set_session_id 基座缺 ⤴️15.20）。构造失败 warn + nil。
// Python: build_code_runtime_prompt (code_rails.py:162-180)
func buildCodeRuntimePrompt(params map[string]any, view SwarmContextView) (any, error) {
	_ = params
	rail := swarmrails.NewRuntimePromptRail(CodeRuntimeLanguage(view), stringFieldOrEmpty(view.ViewChannel(), "default"))
	rail.SetMode(view.ViewMode())
	// rail.set_session_id(ctx.session_id)——基座缺 ⤴️15.20
	rail.SetRuntimePaths(view.ViewProjectDir(), view.ViewProjectDir())
	return rail, nil
}

// buildCodeAgentMd 构建 code 成员项目指令附件 rail（agent.md / claude.md，
// 自会话工作目录向上三级内最近命中）。
// 基座 AgentMdPromptRail Go 未实施 ⤴️15.20。构造失败 warn + nil。
// Python: build_code_agent_md (code_rails.py:216-231)
func buildCodeAgentMd(params map[string]any, view SwarmContextView) (any, error) {
	_ = params
	_ = view.ViewProjectDir()
	// Python: AgentMdPromptRail(); rail.set_runtime_paths(cwd=inp.project_dir)
	return nil, nil
}

// buildCodeProjectMemory 构建 ProjectMemoryRail（自动加载 JIUWENSWARM.md / CLAUDE.md
// 及附加配置目录）。构造失败 warn + nil。
// Python: build_code_project_memory (code_rails.py:268-283)
func buildCodeProjectMemory(params map[string]any, view SwarmContextView) (any, error) {
	additional := stringSliceParam(params, "additional_directories")
	rail := swarmrails.NewProjectMemoryRail(
		orEmpty(view.ViewProjectDir(), view.ViewMemberWorkspaceRoot(), "./"),
		CodeRuntimeLanguage(view),
		0,
		additional,
	)
	return rail, nil
}

// buildPermissionInterrupt 权限中断 rail（config.permissions.enabled 门控）。
//
// 无 A4P 时跳过 cron：调度器给每个 cron 请求打 request_metadata["cron"]
// 标记；channel_id == "__cron__" 兜底覆盖 SDK 未把 request_metadata 传播进
// SwarmBuildContext 的单 agent 会话。Team Plan leader 用
// _TeamPlanPermissionInterruptRail 委派包装（exit_plan_mode 放行给计划审批）。
// 基座 build_permission_rail/apply_permission_trusted_dirs
// （interrupt_helpers.py）与 is_a4p_preauthorization_active Go 未实施
// ⤴️15.20——门控逻辑全量保留，构建段缺失时返回 nil。
// Python: build_permission_interrupt (code_rails.py:336-378)
func buildPermissionInterrupt(params map[string]any, view SwarmContextView) (any, error) {
	_ = params
	requestMeta := view.ViewRequestMetadata()
	isCron := mapKeyTruthy(requestMeta, "cron") || view.ViewChannelID() == "__cron__"
	if isCron && !isA4PPreauthorizationActive(view.ViewConfig()) {
		// Python: 无 A4P 时跳过 cron
		return nil, nil
	}
	// Python: build_permission_rail(config={...}, llm=None, model_name=...,
	//   session_id=..., channel_id=...) + apply_permission_trusted_dirs + 计划
	// leader 委派包装——基座 ⤴️15.20。
	_ = isTeamPlanLeader(view)
	return nil, nil
}

// isA4PPreauthorizationActive A4P 预授权激活判定。
// 基座（common/a4p/runtime.py）Go 未实施 ⤴️15.20；缺省 false 对齐
// 「无 A4P」分支。
func isA4PPreauthorizationActive(config map[string]any) bool {
	_ = config
	return false
}

// buildCodeCodingMemory 构建 CodingMemoryRail 并发布到 ctx.extras 供
// code_agent 子代理复用同一实例。
// 基座独立函数 create_coding_memory_rail Go 未实施（现仅 CodeAdapter 私有
// 方法 buildCodingMemoryRail）⤴️15.20。构造失败 warn + nil。
// Python: build_code_coding_memory (code_rails.py:415-444)
func buildCodeCodingMemory(params map[string]any, view SwarmContextView) (any, error) {
	_ = params
	_ = view
	// Python: create_coding_memory_rail(project_dir=..., agent_workspace_dir=...,
	//   config={"embed": ...}) + ctx.extras[CODING_MEMORY_EXTRAS_KEY] = rail
	// ——基座 ⤴️15.20（extras 发布随回收点一并补）。
	return nil, nil
}

// buildCodeAgentMode profile 感知的计划模式 rail（code 成员与 Team Plan leader）。
//
// 分支（Python :489-520）：team.plan（非 code profile）leader →
// WorkAgentModeRail（语言偏好 + 退出通知）；code profile → CodeAgentModeRail
// （允许工具集/系统注/进入指令/退出通知）。基座 WorkAgentModeRail/
// CodeAgentModeRail 及 _CODE_PLAN_ALLOWED_TOOLS/_PLAN_MODE_SYSTEM_NOTE/
// _code_enter_plan_instructions Go 未实施 ⤴️15.20。构造失败 warn + nil。
// Python: build_code_agent_mode (code_rails.py:485-520)
func buildCodeAgentMode(params map[string]any, view SwarmContextView) (any, error) {
	_ = params
	if schema.IsTeamPlanMode(view.ViewMode()) && !schema.IsCodeProfileMode(view.ViewMode()) && view.ViewRole() == "leader" {
		// Python: WorkAgentModeRail(language=..., exit_plan_notification=...)
		// ——基座 ⤴️15.20
		return nil, nil
	}
	if !schema.IsCodeProfileMode(view.ViewMode()) {
		return nil, nil
	}
	// Python: CodeAgentModeRail(allowed_tools=..., plan_mode_system_note=...,
	//   enter_plan_instructions=..., exit_plan_notification=...)——基座 ⤴️15.20
	_ = isTeamPlanLeader(view)
	return nil, nil
}

// buildTeamPlanApproval Team Plan leader 的 exit_plan_mode 审批中断。
// 基座 PlanApprovalInterruptRail Go 未实施 ⤴️15.20。构造失败 warn + nil。
// Python: build_team_plan_approval (code_rails.py:525-537)
func buildTeamPlanApproval(params map[string]any, view SwarmContextView) (any, error) {
	_ = params
	if !isTeamPlanLeader(view) {
		return nil, nil
	}
	return nil, nil
}

// buildStructuredAskUser code 运行语言的 StructuredAskUserRail。
//
// request_metadata.supports_user_interaction 为 False 时跳过。
// Python: build_structured_ask_user (code_rails.py:561-577)
func buildStructuredAskUser(params map[string]any, view SwarmContextView) (any, error) {
	if v, ok := view.ViewRequestMetadata()["supports_user_interaction"]; ok && v == false {
		return nil, nil
	}
	// Python: StructuredAskUserRail(language=inp.language)——基座签名核对：
	// Go StructuredAskUserRail 已有（common/rails/structured_ask_user_rail.go），
	// 构造形参以基座实际为准（语言注入段随基座构造）。
	rail := swarmrails.NewStructuredAskUserRail(StructuredAskUserLanguage(view))
	return rail, nil
}

// buildCodeTaskPlanning code 专属任务规划 rail（CC 对齐 todo 工具）。
// Python: build_code_task_planning (code_rails.py:582-589)
func buildCodeTaskPlanning(params map[string]any, view SwarmContextView) (any, error) {
	_ = params
	_ = view
	// Python: CodeTaskPlanningRail()——基座 Go 已有（code/rails/code_task_planning_rail.go）
	return coderails.NewCodeTaskPlanningRail(CodeRuntimeLanguage(view)), nil
}

// buildCodeAgentRail CodeAgentRail（管理 /agents 自定义 agents，工作区为根）。
// Go 基座 NewCodeAgentRail 需要 configLister 形参（Python 无——基座签名
// 偏差登记 D-15.16f-codeagent-lister；无注册源时 nil lister 走基座缺省）。
// Python: build_code_agent_rail (code_rails.py:608-620)
func buildCodeAgentRail(params map[string]any, view SwarmContextView) (any, error) {
	_ = params
	workspaceDir := codeAgentWorkspaceDir(view)
	// 基座 NewCodeAgentRail 在 server/adapter 包（adapter→team→swarm 成环），
	// 经 codeAgentRailCtor 注册段注入（D-15.16f-view 配套缝）。
	if codeAgentRailCtor == nil {
		return nil, nil
	}
	return codeAgentRailCtor(workspaceDir), nil
}

// buildUserHooks 从 config.hooks 构建用户钩子 rail（无事件配置时 None）。
// Python: build_user_hooks (code_rails.py:648-665)
func buildUserHooks(params map[string]any, view SwarmContextView) (any, error) {
	hooksSection, _ := params["hooks_section"].(map[string]any)
	if hooksSection == nil {
		hooksSection = map[string]any{}
	}
	hooksConfig := hooks.LoadHooksConfig(map[string]any{"hooks": hooksSection})
	if hooksConfig == nil || len(hooksConfig.Events) == 0 {
		return nil, nil
	}
	// Python: UserHookRail(hooks_config, subagent_type=ctx.member_name or "swarm_member")；
	// Go 基座 NewUserHookRail 无 subagent_type 形参（偏差 D-15.16f-userhook-subagent
	// ⤴️15.20 补形参）。
	// 基座 NewUserHookRail 在 server/hooks 包（同环），注册段注入。
	if userHookRailCtor == nil {
		return nil, nil
	}
	return userHookRailCtor(*hooksConfig), nil
}

// stringSliceParam 读字符串切片参数（兼容 []any）。
func stringSliceParam(params map[string]any, key string) []string {
	switch typed := params[key].(type) {
	case []string:
		return typed
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// codeAgentRailCtor CodeAgentRail 构造（server/adapter 基座，注册段注入）。
	// 返回类型收窄为 AgentRail（2026-12-04 裁决，同 member_rails 注入缝）。
	codeAgentRailCtor func(workspaceDir string) agentinterfaces.AgentRail
	// userHookRailCtor 用户钩子 rail 构造（server/hooks 基座，注册段注入）。
	userHookRailCtor func(config hooks.HooksConfig) agentinterfaces.AgentRail
)

// SetCodeAgentRailCtor 注入 CodeAgentRail 构造。
func SetCodeAgentRailCtor(fn func(workspaceDir string) agentinterfaces.AgentRail) {
	codeAgentRailCtor = fn
}

// SetUserHookRailCtor 注入用户钩子 rail 构造。
func SetUserHookRailCtor(fn func(config hooks.HooksConfig) agentinterfaces.AgentRail) {
	userHookRailCtor = fn
}

// RegisterCodeRailProviders 注册 code_rails 全部 provider。
// 清单顺序按 code_rails.py @harness_element 声明序。
// Python: code_rails.py 全部 @harness_element(kind=RAIL) 段。
func RegisterCodeRailProviders() {
	hschema.RegisterRailProvider(constCodeRuntimePrompt, railProviderAdapter(buildCodeRuntimePrompt))
	hschema.RegisterRailProvider(constCodeAgentMd, railProviderAdapter(buildCodeAgentMd))
	hschema.RegisterRailProvider(constCodeProjectMemory, railProviderAdapter(buildCodeProjectMemory))
	hschema.RegisterRailProvider(constPermissionInterrupt, railProviderAdapter(buildPermissionInterrupt))
	hschema.RegisterRailProvider(constCodeCodingMemory, railProviderAdapter(buildCodeCodingMemory))
	hschema.RegisterRailProvider(constCodeAgentMode, railProviderAdapter(buildCodeAgentMode))
	hschema.RegisterRailProvider(constTeamPlanApproval, railProviderAdapter(buildTeamPlanApproval))
	hschema.RegisterRailProvider(constStructuredAskUser, railProviderAdapter(buildStructuredAskUser))
	hschema.RegisterRailProvider(constCodeTaskPlanning, railProviderAdapter(buildCodeTaskPlanning))
	hschema.RegisterRailProvider(constCodeAgentRail, railProviderAdapter(buildCodeAgentRail))
	hschema.RegisterRailProvider(constUserHooks, railProviderAdapter(buildUserHooks))
}
