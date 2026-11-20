package providers

import (
	buildctx "github.com/uapclaw/uapclaw-go/internal/swarm/agents/swarm/buildctx"
	hschema "github.com/uapclaw/uapclaw-go/internal/openjiuwen/harness/schema"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/openjiuwen/core/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	skillruntime "github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime/skill"
)

// skills.go —— 成员技能可见性 provider 与成员技能工具箱 rail。
//
// Python: jiuwenswarm/agents/swarm/providers/skills.py（233 行）。
// Provider 名常量：registry.go 常量层承载（swarm.member_skill_toolkit）。

// ──────────────────────────── 常量 ────────────────────────────

const constMemberSkillToolkit = "swarm.member_skill_toolkit"

// ──────────────────────────── 导出函数 ────────────────────────────

// BuildMemberSkillVisibilityProvider 构建成员的活 Skill 可见性 provider。
//
// 返回对象在每次调用时重读成员与团队元数据文件，其他进程的授权变更无需
// 重建即可生效。这是能接 visibility provider 的 Skill rail 应拿到的形态；
// 只能接名字集合的调用方走 ComposeMemberSkillVisibility。
//
// 成员工作区与 (team, member) 身份都不在时返回 false（调用方回落到仅
// 进程级 disabled 清单）。
//
// Deviation D-15.16f-visprovider：Python build_skill_visibility_provider
// （FileSkillVisibilityProvider，common 工具域）Go 未实施 ⤴️15.20——
// 本函数保留完整门控与路径解析，provider 段由回收点接线。
// Python: build_member_skill_visibility_provider (skills.py:60-93)
func BuildMemberSkillVisibilityProvider(view *buildctx.SwarmBuildContext) bool {
	memberPath := view.ResolveMemberSkillVisibilityPath()
	teamPath := view.TeamSkillVisibilityPath
	memberName := trimSpaceOrEmpty(view.MemberName)
	if memberPath == "" {
		return false
	}
	// Python: build_skill_visibility_provider(member_path=..., member_id=...,
	//   team_path=..., team_id=..., global_disabled_loader=...)——基座 ⤴️15.20
	_ = teamPath
	_ = memberName
	return false
}

// ComposeMemberSkillVisibility 一次性解析成员 (enabled, disabled) 名字集合。
//
// 快照形态；enabled 空集意为「继承全库」，绝不意为「全拒」；disabled 恒
// 压过 enabled。两条规则与 SkillUseRail 对自身 enabled/disabled 的处理一致。
// 无可解析成员身份时仅进程级 disabled 清单生效。
// Python: compose_member_skill_visibility (skills.py:96-125)
func ComposeMemberSkillVisibility(view *buildctx.SwarmBuildContext) (map[string]struct{}, map[string]struct{}) {
	if !BuildMemberSkillVisibilityProvider(view) {
		// Python: return set(), set(_load_global_disabled_skills())
		disabled := make(map[string]struct{})
		for _, name := range LoadGlobalDisabledSkills() {
			disabled[name] = struct{}{}
		}
		return map[string]struct{}{}, disabled
	}
	// provider 段 ⤴️15.20
	return map[string]struct{}{}, map[string]struct{}{}
}

// LoadGlobalDisabledSkills 进程级执行禁用 Skill 名单。
// Python: _load_global_disabled_skills (skills.py:128-135)
func LoadGlobalDisabledSkills() []string {
	return skillruntime.LoadExecutionDisabledSkills()
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// buildMemberSkillToolkit 当前成员的成员级技能工具箱 rail。
//
// 绑定共享 agent 工作区（安装落在全成员共读的唯一库）。不写可见性元数据：
// 成员的 skills-visibility.json 只有 team Skill rail 一个播种者。不注入
// on_skill_library_changed 钩子：空 allow-list 已继承全库，显式 allow-list
// 必须有意授予。成员无可用工区时跳过（nil）。
// Python: build_member_skill_toolkit (skills.py:176-231)
func buildMemberSkillToolkit(params map[string]any, view *buildctx.SwarmBuildContext) (any, error) {
	_ = params
	rootPath := memberWorkspaceRoot(view)
	if rootPath == "" {
		return nil, nil
	}
	visibilityPath := view.ResolveMemberSkillVisibilityPath()
	agentDir := agentWorkspaceDir()
	// Python: SkillManager(workspace_dir=str(agent_workspace_dir))（失败 warn）
	// Python: member_skill_manager = SkillManager(workspace_dir=...)（失败 warn）
	_ = skillruntime.NewSkillManager(agentDir)
	logger.Info(logger.ComponentAgentServer).
		Str("library", orEmpty(view.GlobalSkillsDir, agentpathsGlobalSkillsDir())).
		Str("visibility", visibilityPath).
		Msg("[swarm.member_skill_toolkit] MemberSkillToolkitRail built")
	// Python: MemberSkillToolkitRail(workspace_dir=..., manager=...)
	// ——基座 Go 已有（team/member_skill_toolkit_rail.go，构造仅 workspaceDir；
	// manager 形参 ⤴️15.20 对齐双形参）。
	// 基座 MemberSkillToolkitRail 在 team 包（team→swarm 成环），注册段注入
	// （D-15.16f-view 配套缝）；manager 形参 ⤴️15.20 对齐双形参。
	if memberSkillToolkitRailCtor == nil {
		return nil, nil
	}
	return memberSkillToolkitRailCtor(agentDir), nil
}

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// memberSkillToolkitRailCtor 成员技能工具箱 rail 构造（team 基座，注册段注入）。
	memberSkillToolkitRailCtor func(workspaceDir string) agentinterfaces.AgentRail
)

// SetMemberSkillToolkitRailCtor 注入成员技能工具箱 rail 构造。
func SetMemberSkillToolkitRailCtor(fn func(workspaceDir string) agentinterfaces.AgentRail) {
	memberSkillToolkitRailCtor = fn
}

// ──────────────────────────── 导出函数（注册） ────────────────────────────

// RegisterSkillProviders 注册 skills.py 的 rail provider。
// Python: skills.py @harness_element 段（:171-176）。
func RegisterSkillProviders() {
	hschema.RegisterRailProvider(constMemberSkillToolkit, railProviderAdapter(buildMemberSkillToolkit))
}
