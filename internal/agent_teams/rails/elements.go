package rails

import (
	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/team_workspace"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// TeamRailSpecInput 构造一个成员所需团队 rail 的全部输入。
//
// 来源三处：spec（dispatch_mode/lifecycle/...）、runtime ctx（role/member_name/...）、
// configurator 本地（team_name/workspace 路径/...）。用结构体收齐，
// 由 AgentConfigurator 填。
//
// Python: agent_configurator.py:723-800 里 team_rail_specs 的构造输入
type TeamRailSpecInput struct {
	// Role 团队角色值
	Role string
	// MemberName 成员名
	MemberName string
	// Language 解析后的语言
	Language string
	// TeammateMode 队友模式
	TeammateMode string
	// DispatchMode 调度模式
	DispatchMode string
	// BootstrapMode 建队模式
	BootstrapMode string
	// Lifecycle 团队生命周期
	Lifecycle string
	// TeamMode 团队模式
	TeamMode string
	// TeamName 团队名
	TeamName string
	// LeaderMemberName Leader 成员名
	LeaderMemberName string
	// Prompt 成员私有工作约定
	Prompt string
	// DisplayName 成员可读标签
	DisplayName string
	// BasePrompt 调用方给的基础提示词
	BasePrompt string
	// MemberWorkspacePath 成员私有产物目录
	MemberWorkspacePath string
	// TeamWorkspacePath 团队工作空间绝对路径
	TeamWorkspacePath string
	// TeamOutputsDir 最终产物目录
	TeamOutputsDir string
	// ForkSource fork 来源成员名
	ForkSource string
	// ExcludeTools 要排除的工具名
	ExcludeTools []string
	// TeamPermissionsEnabled 团队权限开关（开启时用 TeamPermissionRail 替代审批轨）
	TeamPermissionsEnabled bool
	// ExposeHumanAgents 是否暴露人类成员给队友
	ExposeHumanAgents bool
	// SteerBatchSize 一次模型调用吸收的 steering 条数
	SteerBatchSize int
	// SwarmflowEnabled leader 是否持有 swarmflow 工具
	SwarmflowEnabled bool
	// BoundGroupName 绑定的 AgentGroup 名
	BoundGroupName string
	// ApprovalTools 需审批的工具名
	ApprovalTools []string
	// HasWorkspaceManager 是否有工作空间管理器（决定要不要出 workspace rail）
	HasWorkspaceManager bool
	// IsTeamPlanLeader 是否 team.plan Leader（决定要不要出 plan_mode rail）
	IsTeamPlanLeader bool
	// IsCoordinatedTeammate 是否受协调的队友（审批轨门卫之一）
	IsCoordinatedTeammate bool
	// HasTeamBackend 是否有团队后端（tool rail 的门卫）
	HasTeamBackend bool
	// HasMessager 是否有消息总线（审批轨门卫之一）
	HasMessager bool
}

// ──────────────────────────── 常量 ────────────────────────────

// 8 个团队 rail 的注册名，逐字对齐上游 elements.py:64-71。
//
// 后三个（reliability/skill_use/observability）的 Go 实现尚未落地，
// 但常量与占位工厂现在就注册（决策 C8）：type 字符串是持久化契约
// （checkpoint 里的 RailSpec.type），先占位可让后续实现只替换工厂体，
// 不牵动调用方与已持久化的 spec。
const (
	// TeamTool 团队协调工具轨
	TeamTool = "core.team.tool"
	// TeamPolicy 团队策略 / 提示词轨
	TeamPolicy = "core.team.policy"
	// TeamWorkspace 团队工作空间轨
	TeamWorkspace = "core.team.workspace"
	// TeamToolApproval 工具审批轨
	TeamToolApproval = "core.team.tool_approval"
	// TeamPlanMode 团队计划模式轨
	TeamPlanMode = "core.team.plan_mode"
	// TeamReliability 可靠性轨（Go 实现未落地，见占位工厂）
	TeamReliability = "core.team.reliability"
	// TeamSkillUse 团队技能轨（Go 实现未落地，见占位工厂）
	TeamSkillUse = "core.team.skill_use"
	// TeamObservability 团队可观测性轨（Go 实现未落地，见占位工厂）
	TeamObservability = "core.team.observability"
	// teamWorkspaceMountPath 团队工作空间在成员工作区里的挂载路径
	// Python: agent_configurator.py:943 的 "/.team/workspace"
	teamWorkspaceMountPath = "/.team/workspace"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// RegisterTeamRailProviders 注册全部 8 个团队 rail provider。
//
// 5 个真工厂 + 3 个占位工厂。占位注册而非省略：type 字符串是持久化契约，
// 先占位可让后续实现只替换工厂体；且能把"未实现"与"未知 type"在日志上区分开
// （后者专门用于诊断旧 checkpoint 引用了已删除的 rail）。
func RegisterTeamRailProviders() {
	hschema.RegisterRailProvider(TeamTool, buildTeamToolRail)
	hschema.RegisterRailProvider(TeamPolicy, buildTeamPolicyRail)
	hschema.RegisterRailProvider(TeamWorkspace, buildTeamWorkspaceRail)
	hschema.RegisterRailProvider(TeamToolApproval, buildTeamToolApprovalRail)
	hschema.RegisterRailProvider(TeamPlanMode, buildTeamPlanModeRail)
	// 三个占位：warn + 返回 (nil, nil)，与「未知 type」同语义
	hschema.RegisterRailProvider(TeamReliability, buildTeamReliabilityRail)
	hschema.RegisterRailProvider(TeamSkillUse, buildTeamSkillUseRail)
	hschema.RegisterRailProvider(TeamObservability, buildTeamObservabilityRail)
}

// TeamRailSpecs 按成员身份产出团队 rail 的声明列表（顺序即挂载顺序）。
//
// 顺序有语义：TeamToolRail 必须在 TeamPolicyRail 之前挂载并急切初始化，
// 以便 LLM 看到的能力快照稳定。
//
// Python: agent_configurator.py:723-800 的 team_rail_specs 构造
func TeamRailSpecs(in TeamRailSpecInput) []hschema.RailSpec {
	specs := make([]hschema.RailSpec, 0, 5)

	// 1. team.tool（门卫：有团队后端）
	// Python: if team_backend is not None（provider 侧也判，双保险）
	if in.HasTeamBackend {
		specs = append(specs, hschema.RailSpec{
			Type: TeamTool,
			Params: map[string]any{
				"role":                     in.Role,
				"member_name":              in.MemberName,
				"language":                 in.Language,
				"teammate_mode":            in.TeammateMode,
				"dispatch_mode":            in.DispatchMode,
				"bootstrap_mode":           in.BootstrapMode,
				"lifecycle":                in.Lifecycle,
				"team_mode":                in.TeamMode,
				"team_name":                in.TeamName,
				"exclude_tools":            in.ExcludeTools,
				"team_permissions_enabled": in.TeamPermissionsEnabled,
				"swarmflow_enabled":        in.SwarmflowEnabled,
			},
		})
	}

	// 2. team.policy（无条件）
	specs = append(specs, hschema.RailSpec{
		Type: TeamPolicy,
		Params: map[string]any{
			"role":                             in.Role,
			"member_name":                      in.MemberName,
			"language":                         in.Language,
			"display_name":                     in.DisplayName,
			"member_prompt":                    in.Prompt,
			"member_workspace_path":            in.MemberWorkspacePath,
			"team_outputs_dir":                 in.TeamOutputsDir,
			"fork_source":                      in.ForkSource,
			"lifecycle":                        in.Lifecycle,
			"teammate_mode":                    in.TeammateMode,
			"team_mode":                        in.TeamMode,
			"dispatch_mode":                    in.DispatchMode,
			"bootstrap_mode":                   in.BootstrapMode,
			"base_prompt":                      in.BasePrompt,
			"team_workspace_path":              in.TeamWorkspacePath,
			"expose_human_agents_to_teammates": in.ExposeHumanAgents,
			"steer_batch_size":                 in.SteerBatchSize,
			"swarmflow_enabled":                in.SwarmflowEnabled,
			"bound_group_name":                 in.BoundGroupName,
		},
	})

	// 3. team.workspace（有管理器才出）
	if in.HasWorkspaceManager {
		specs = append(specs, hschema.RailSpec{Type: TeamWorkspace})
	}

	// 4. team.tool_approval（受协调队友 + 有审批工具 + 后端与消息总线齐备 + 非团队权限模式）
	isCoordinated := in.IsCoordinatedTeammate && len(in.ApprovalTools) > 0 &&
		in.HasTeamBackend && in.HasMessager
	if isCoordinated && !in.TeamPermissionsEnabled {
		specs = append(specs, hschema.RailSpec{
			Type: TeamToolApproval,
			Params: map[string]any{
				"team_name":          in.TeamName,
				"leader_member_name": in.LeaderMemberName,
				"tool_names":         in.ApprovalTools,
			},
		})
	}

	// 5. team.plan_mode（team.plan Leader 才出）
	if in.IsTeamPlanLeader {
		specs = append(specs, hschema.RailSpec{Type: TeamPlanMode})
	}

	return specs
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// buildTeamToolRail 构造团队协调工具轨。
//
// 门卫：无团队后端时返回 nil（对齐上游 `if backend is None: return None`）。
//
// Python: elements.py:99-145 build_team_tool_rail
func buildTeamToolRail(params map[string]any, ctx hschema.BuildContextInterface) (any, error) {
	backend := GetTeamBackend(ctx)
	if backend == nil {
		return nil, nil
	}

	// exclude_tools 从 []string 转成 map 形态（TeamToolRail 用 set 语义）
	rawExclude := hschema.ParamStringSlice(params, "exclude_tools")
	var excludeTools map[string]struct{}
	if len(rawExclude) > 0 {
		excludeTools = make(map[string]struct{}, len(rawExclude))
		for _, name := range rawExclude {
			excludeTools[name] = struct{}{}
		}
	}

	opts := []TeamToolRailOption{
		WithTeamBackend(backend),
		WithRole(hschema.ParamString(params, "role", "leader")),
		WithMemberName(hschema.ParamString(params, "member_name", "")),
		WithLanguage(hschema.ParamString(params, "language", "cn")),
		WithTeammateMode(hschema.ParamString(params, "teammate_mode", "build_mode")),
		WithToolDispatchMode(hschema.ParamString(params, "dispatch_mode", "autonomous")),
		WithToolBootstrapMode(hschema.ParamString(params, "bootstrap_mode", "leader")),
		WithLifecycle(hschema.ParamString(params, "lifecycle", "temporary")),
		WithToolTeamMode(hschema.ParamString(params, "team_mode", "default")),
		WithTeamName(hschema.ParamString(params, "team_name", "default")),
		WithExcludeTools(excludeTools),
		WithToolSwarmflowEnabled(hschema.ParamBool(params, "swarmflow_enabled", false)),
		WithWorkspaceManager(GetTeamWorkspaceManager(ctx)),
		WithWorktreeManager(GetWorktreeManager(ctx)),
	}
	// 模型分配器：teammate spawn 时按 model_name 领取端点。
	// 旧的手工挂载路径由 AgentConfigurator 直接注入 ModelAllocator.Allocate；
	// 改为 spec 声明驱动后必须从 build context 的句柄取，否则 spawn 拿不到模型。
	// Python: build_team_tool_rail 从 build_context 取 model_allocator
	if alloc := GetModelAllocator(ctx); alloc != nil {
		opts = append(opts, WithModelConfigAllocator(alloc.Allocate))
	}
	return NewTeamToolRail(opts...), nil
}

// buildTeamPolicyRail 构造团队策略轨（无条件挂载）。
//
// Python: elements.py:184-256 build_team_policy_rail
func buildTeamPolicyRail(params map[string]any, ctx hschema.BuildContextInterface) (any, error) {
	role := atschema.TeamRole(hschema.ParamString(params, "role", hschema.ContextString(ctx, "Role", "leader")))
	memberName := hschema.ParamString(params, "member_name", hschema.ContextString(ctx, "MemberName", ""))
	language := hschema.ContextString(ctx, "Language", "cn")

	opts := []TeamPolicyRailOption{
		WithPolicyRole(role),
		WithPolicyMemberName(memberName),
		WithPolicyLanguage(language),
		WithPolicyDisplayName(hschema.ParamString(params, "display_name", "")),
		WithPolicyMemberPrompt(hschema.ParamString(params, "member_prompt", "")),
		WithPolicyMemberWorkspacePath(hschema.ParamString(params, "member_workspace_path", "")),
		WithPolicyTeamOutputsDir(hschema.ParamString(params, "team_outputs_dir", "")),
		WithPolicyForkSource(hschema.ParamString(params, "fork_source", "")),
		WithPolicyLifecycle(hschema.ParamString(params, "lifecycle", "temporary")),
		WithPolicyTeammateMode(hschema.ParamString(params, "teammate_mode", "build_mode")),
		WithPolicyTeamMode(hschema.ParamString(params, "team_mode", "default")),
		WithPolicyDispatchMode(hschema.ParamString(params, "dispatch_mode", "autonomous")),
		WithPolicyBootstrapMode(hschema.ParamString(params, "bootstrap_mode", "leader")),
		WithPolicyBasePrompt(hschema.ParamString(params, "base_prompt", "")),
		WithPolicyTeamWorkspacePath(hschema.ParamString(params, "team_workspace_path", "")),
		WithPolicyExposeHumanAgents(hschema.ParamBool(params, "expose_human_agents_to_teammates", false)),
		WithPolicySwarmflowEnabled(hschema.ParamBool(params, "swarmflow_enabled", false)),
		WithPolicyBoundGroupName(hschema.ParamString(params, "bound_group_name", "")),
		WithPolicyTeamBackend(GetTeamBackend(ctx)),
		WithPolicyTaskEventTracker(GetTaskEventTracker(ctx)),
	}
	// steer_batch_size：上游默认 2（team_policy_rail.py:147）
	size, err := hschema.ParamInt(params, "steer_batch_size", defaultSteerBatchSize)
	if err != nil {
		return nil, err
	}
	opts = append(opts, WithPolicySteerBatchSize(size))

	// 团队工作空间挂载：有 workspace manager 时才注入两个路径参数
	// Python: agent_configurator.py:940-945 的 opts.append 分支
	if mgr := GetTeamWorkspaceManager(ctx); mgr != nil {
		opts = append(opts,
			WithPolicyTeamWorkspaceMount(teamWorkspaceMountPath),
			WithPolicyTeamWorkspacePath(mgr.WorkspacePath()),
		)
	}

	rail, err := NewTeamPolicyRail(opts...)
	if err != nil {
		return nil, err
	}
	return rail, nil
}

// buildTeamWorkspaceRail 构造团队工作空间轨。
//
// 门卫：无 workspace manager 时返回 nil。
//
// Python: elements.py:258-292 build_team_workspace_rail
func buildTeamWorkspaceRail(params map[string]any, ctx hschema.BuildContextInterface) (any, error) {
	mgr := GetTeamWorkspaceManager(ctx)
	if mgr == nil {
		return nil, nil
	}
	memberName := hschema.ParamString(params, "member_name", hschema.ContextString(ctx, "MemberName", ""))
	return team_workspace.NewTeamWorkspaceRail(mgr, memberName), nil
}

// buildTeamToolApprovalRail 构造工具审批轨。
//
// 门卫：无团队后端、无消息总线、或无审批工具时返回 nil。
//
// Python: elements.py:293-328 build_team_tool_approval_rail
func buildTeamToolApprovalRail(params map[string]any, ctx hschema.BuildContextInterface) (any, error) {
	backend := GetTeamBackend(ctx)
	msg := GetMessager(ctx)
	if backend == nil || msg == nil {
		return nil, nil
	}
	toolNames := hschema.ParamStringSlice(params, "tool_names")
	if len(toolNames) == 0 {
		return nil, nil
	}
	return NewTeamToolApprovalRail(
		hschema.ParamString(params, "team_name", ""),
		hschema.ParamString(params, "member_name", hschema.ContextString(ctx, "MemberName", "")),
		backend.DB(),
		msg,
		hschema.ParamString(params, "leader_member_name", ""),
		toolNames,
	), nil
}

// buildTeamPlanModeRail 构造团队计划模式轨。
//
// Python: elements.py:329-363 build_team_plan_mode_rail
func buildTeamPlanModeRail(params map[string]any, ctx hschema.BuildContextInterface) (any, error) {
	lang := hschema.ParamString(params, "language", hschema.ContextString(ctx, "Language", "cn"))
	return NewTeamPlanModeRail(WithLanguageOverride(lang)), nil
}

// buildTeamReliabilityRail 占位工厂：可靠性轨的 Go 实现未落地。
//
// ⤵️ 上游实现：agent_teams/rails/elements.py:365-433 → reliability/rail.py
// 依赖 agent_teams/reliability/{factory,monitor,detectors,remediation,anomaly}.py，
// 整域未迁。落地时把本函数体替换为真正的构造即可，签名与注册点都不变。
func buildTeamReliabilityRail(params map[string]any, ctx hschema.BuildContextInterface) (any, error) {
	logger.Warn(logComponent).
		Str("rail_type", TeamReliability).
		Msg("team reliability rail provider is not implemented; skipping")
	return nil, nil
}

// buildTeamSkillUseRail 占位工厂：团队技能轨的 Go 实现未落地。
//
// ⤵️ 归属 25.10（团队技能）。
func buildTeamSkillUseRail(params map[string]any, ctx hschema.BuildContextInterface) (any, error) {
	logger.Warn(logComponent).
		Str("rail_type", TeamSkillUse).
		Msg("team skill_use rail provider is not implemented; skipping")
	return nil, nil
}

// buildTeamObservabilityRail 占位工厂：团队可观测性轨的 Go 实现未落地。
//
// ⤵️ 归属领域二十二（trajectory v2）。
func buildTeamObservabilityRail(params map[string]any, ctx hschema.BuildContextInterface) (any, error) {
	logger.Warn(logComponent).
		Str("rail_type", TeamObservability).
		Msg("team observability rail provider is not implemented; skipping")
	return nil, nil
}
