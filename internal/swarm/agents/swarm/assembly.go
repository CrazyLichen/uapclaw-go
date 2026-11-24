package swarm

import (
	"errors"

	configutil "github.com/uapclaw/uapclaw-go/internal/common/config"
	experts "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/experts"
	externalPkg "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/external"
	"os"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
	agentteams "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/observability/demand"
	atschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/schema"
	buildctx "github.com/uapclaw/uapclaw-go/internal/swarm/agents/swarm/buildctx"
	"github.com/uapclaw/uapclaw-go/internal/swarm/common"
	swschema "github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// logComponent 组件级日志组件。
const logComponent = logger.ComponentAgentServer

// ──────────────────────────── 结构体 ────────────────────────────

// EnrichSeedParams EnrichTeamSpecForSwarmSeed 的入参。
// 对齐 Python enrich_team_spec_for_swarm（assembly.py:391-437）签名全集；
// agent_group_package 由调用方预载（Python :435 同名形参）。
type EnrichSeedParams struct {
	// SessionID 当前会话 id
	SessionID string
	// Mode 请求模式（如 "team"）
	Mode string
	// ProjectDir 已解析项目目录（可空）
	ProjectDir string
	// TrustedDirs 客户端声明可信目录
	TrustedDirs []string
	// RequestID 发起请求 id
	RequestID string
	// UserID 认证请求属主
	UserID string
	// ChannelID 请求原始 channel id
	ChannelID string
	// RequestMetadata 请求元数据（携带 mode 等）
	RequestMetadata map[string]any
	// AgentGroupName 本 Team 选定的 AgentGroup 名（可空）。
	// Python: agent_group_name (assembly.py:397)
	AgentGroupName string
	// AgentGroupPackage 预载的 AgentGroup 包（可空；nil 时按名解析加载）。
	// Python: agent_group_package (assembly.py:398)
	AgentGroupPackage *experts.LoadedAgentGroupPackage
}

// externalTeamPublishURL 按渠道算外部团队事件发布 URL。
// Python: _external_team_publish_url (assembly.py:85-92)
// tui → ws://127.0.0.1:{GATEWAY_PORT:-19001}/tui；web（含空渠道）→
// ws://127.0.0.1:{WEB_PORT:-19000}{WEB_PATH:-/ws}。
func externalTeamPublishURL(channelID string) string {
	channel := strings.ToLower(strings.TrimSpace(orDefault(channelID, "web")))
	if channel == "tui" {
		port := orDefault(os.Getenv("GATEWAY_PORT"), "19001")
		return "ws://127.0.0.1:" + port + "/tui"
	}
	port := orDefault(os.Getenv("WEB_PORT"), "19000")
	path := strings.TrimSpace(os.Getenv("WEB_PATH"))
	if path == "" {
		path = "/ws"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return "ws://127.0.0.1:" + port + path
}

// ensureExternalTeamTransport 给跨进程 CLI 成员一条回到 Gateway 的实时事件路径。
// Python: _ensure_external_team_transport (assembly.py:97-112)；调用位 :425。
// spec.external_transport 已配置则幂等返回。
func ensureExternalTeamTransport(spec *atschema.TeamAgentSpec, channelID string) {
	if spec.ExternalTransport != nil {
		return
	}
	publishURL := externalTeamPublishURL(channelID)
	spec.ExternalTransport = &atschema.TransportSpec{
		Type: "hybrid",
		Params: map[string]any{
			"team_name":            spec.TeamName,
			"external_publish_url": publishURL,
		},
	}
	logger.Info(logComponent).
		Str("team_name", spec.TeamName).
		Str("channel", orDefault(channelID, "web")).
		Str("url", publishURL).
		Msg("[swarm.assembly] configured external team relay")
}

// orDefault Python `x or default` 语义：空串取默认。
func orDefault(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}

// ──────────────────────────── 导出函数 ────────────────────────────

// EnrichTeamSpecForSwarmSeed 就地 enrich 团队 spec 的 provider 化装配全链：
// 注册 swarm providers、构建每团队基础 context、按 config 源改写成员 spec、
// 装配 AgentGroup roster、挂活载体并携带可序列化种子。就地修改 spec，无返回
// spec（Python 原语义）。
//
// 偏差 D-15.16-c3 已随本批回收（T3）：成员 spec 重写/mcp_configs/reviewer/
// agent_group 应用/group_members 段全部并入本函数，拆分壳注释删除。
// rails 目录注册段（register_from_catalog）仍 ⤵️15.7/15.8（D-15.16-c3 余量）。
//
// 偏差 D-15.16-c5 已随本批回收：尾日志 mcps/agent_group 用真实输入驱动。
//
// 偏差 D-15.16-c8：接线点为 TeamManager.GetSwarmEnrichedTeamSpec
// （team_manager.py:1958）。
//
// 偏差 D-15.16-e1：_apply_design_agent_group 分支体留缝（判断与步骤位置
// 保留，designer profile 装配体不落，⤴️25.12）。
// 偏差 D-15.16-e2：group_members 段走报错路径（group_chat.py 未迁，
// ⤴️群聊章节），步骤位置与分支结构保留。
func EnrichTeamSpecForSwarmSeed(spec *atschema.TeamAgentSpec, p EnrichSeedParams) error {
	// 步骤 1: 注册种子工厂（对齐 :456 register_swarm_providers 的工厂段）
	RegisterSwarmProviders()

	// 步骤 2: _ensure_external_team_transport（:425 调用位；:457 原行号）
	// 给跨进程 CLI 成员挂 hybrid 回传通道（15.16d）。
	ensureExternalTeamTransport(spec, p.ChannelID)

	// 步骤 3: 把 openjiuwen 唯一 Skill 库指到平台拥有目录（:459-467）。
	// 不做则回落默认布局，平台与成员读两套不同的库。幂等。
	skillsLibrary := workspace.AgentSkillsDir()
	agentteams.ConfigureGlobalSkillsDir(skillsLibrary)

	// 步骤 4: team_ws_root 两级选择（:470-477）：spec.workspace.root_path
	// 优先，否则 team_home(team_name)/team-workspace 默认布局。
	teamWsRoot := ""
	if spec.Workspace != nil && spec.Workspace.RootPath != "" {
		teamWsRoot = spec.Workspace.RootPath
	} else {
		teamWsRoot = agentteams.TeamWorkspaceDir(spec.TeamName) // paths.py:168 布局
	}

	// 步骤 5: 无项目团队的共享产物目录分配（:482-511）。
	// 绑定项目的成员把产物留在项目内：不分配任务工作空间，运行时 prompt
	// rail 停在 else 分支（对齐单 agent 行为）。per-member task_work_dir
	// 在 rail 内按成员解析，不在此处。
	taskWorkspaceRoot := ""
	teamOutputsDir := ""
	if strings.TrimSpace(p.ProjectDir) == "" {
		artifactWs, err := common.GetTeamArtifactWorkspace(teamWsRoot, p.SessionID, "")
		if err != nil {
			// Python: except OSError → warning + 双 None（:500-511）
			logger.Warn(logComponent).
				Str("team_name", spec.TeamName).
				Str("session_id", p.SessionID).
				Err(err).
				Msg("[swarm.assembly] team artifact workspace allocation failed — members will keep deliverables in their own workspace")
		} else {
			taskWorkspaceRoot = artifactWs.RootDir
			teamOutputsDir = artifactWs.OutputsDir
		}
	}

	// 步骤 6: 团队 Skill 可见性文档路径（:516-519）。
	// 从实际团队工作空间根派生而非 paths.team_skill_visibility_path——
	// 被重定位的团队工作空间其元数据保持在它真正使用的根旁。默认布局下
	// 两者同路径。只解析不写入：文档唯一 seeder 是
	// TeamWorkspaceManager.initialize（团队工作空间起来时运行）；缺失文档
	// 读回"无限制"，无工作空间的团队不需要此文件。
	teamVisibilityPath := teamWsRoot + "/" + agentteams.SKILL_VISIBILITY_FILENAME
	globalSkillsDir := skillsLibrary

	// 步骤 7: base = buildctx.SwarmBuildContext(...)（:485-535）
	base := buildctx.NewSwarmBuildContext(buildctx.SwarmBuildContextParams{
		// Python: config=config（assembly.py:485，buildctx.SwarmBuildContext config 字段
		// context.py:124）——providers 工厂经 ctx.config 读开关（15.16f 执行期
		// 集成矩阵发现的 parity 缺口：此前未传，工厂拿到 nil config）。
		Config:                  legacyConfigMap(),
		SessionID:               p.SessionID,
		RequestID:               p.RequestID,
		UserID:                  p.UserID,
		ChannelID:               p.ChannelID,
		Channel:                 channelOrDefault(p.ChannelID),
		RequestMetadata:         p.RequestMetadata,
		Mode:                    p.Mode,
		ProjectDir:              p.ProjectDir,
		TrustedDirs:             p.TrustedDirs,
		DisableTeammateWorktree: disableTeammateWorktree(p.ChannelID, p.Mode),
		TeamID:                  spec.TeamName,
		TeamWsRoot:              teamWsRoot,
		TaskWorkspaceRoot:       taskWorkspaceRoot,
		TeamOutputsDir:          teamOutputsDir,
		TeamSkillVisibilityPath: teamVisibilityPath,
		GlobalSkillsDir:         globalSkillsDir,
		TrajectorySpanProcessor: demand.GetTrajectorySpanProcessor(),
		HeartbeatJobService:     buildctx.GetHeartbeatJobService(),
	})

	// 步骤 8: mcp_configs（assembly.py:537-540 区段）
	// config.yaml 的已启用 MCP servers，按团队 scope 派生稳定 id 并解析凭据
	// 占位符——只注入 native 成员（外部 CLI 成员走 panel/catalog 自己的路径）。
	mcpConfigs := common.BuildEnabledMCPServerConfigs(legacyConfigMap(), "team:"+spec.TeamName, true)

	// 步骤 9: 成员能力装配循环（assembly.py:515-523）
	// _MEMBER_ROLES = ("leader", "teammate")——只处理 spec.agents 两个内置
	// native 角色；外部 CLI 成员不经此段（rail 只进 native 成员）。
	for _, role := range memberRoles {
		if base, ok := spec.Agents[role]; ok {
			memberSpec := BuildMemberDeepAgentSpec(
				legacyConfigMap(),
				p.Mode,
				role,
				&base,
				spec.EnablePermissions,
				mcpConfigs,
			)
			memberSpec = WithProjectCwd(memberSpec, p.ProjectDir)
			spec.Agents[role] = *memberSpec
		}
	}

	// 步骤 10: reviewer 基座（assembly.py:525-526）
	if _, ok := spec.Agents["teammate"]; ok {
		teammate := spec.Agents["teammate"]
		spec.ReviewerAgent = BuildReviewerDeepAgentSpec(p.Mode, &teammate)
	}

	// 步骤 11: AgentGroup 装配（assembly.py:531-534）
	if p.AgentGroupName != "" {
		if isDesignTeamMode(p.Mode) {
			applyDesignAgentGroup(spec, p.AgentGroupName)
		} else {
			if err := applyAgentGroup(spec, p.AgentGroupName, p.AgentGroupPackage); err != nil {
				return err
			}
		}
	}

	// 步骤 12: 挂活载体 + 携带可序列化种子（:536/:540 ★）
	// 种子随活载体同行，跨序列化边界（spawned teammate / 分布式远端 / 冷
	// 恢复）重建的成员经注册工厂还原上下文。
	spec.BuildContext = base
	spec.BuildContextSeed = base.ToSeed()

	// 步骤 13: group_members 群聊段（:552-564；偏差 D-15.16-e2）
	// 步骤位置与分支结构保留；group_chat.py（assemble_group_members /
	// GROUP_MEMBER_TEMPLATE）未迁，走报错路径。
	if groupMembers, ok := (p.RequestMetadata["group_members"]).([]any); ok && groupMembers != nil {
		if p.AgentGroupName != "" {
			// Python: "a public group roster cannot also select a single AgentGroup"
			return errors.New("a public group roster cannot also select a single AgentGroup")
		}
		return errors.New("group_members roster assembly requires group_chat (D-15.16-e2, pending group chat chapter)")
	}

	// 步骤 14: 尾日志（:563-570；c5 回收：mcps/agent_group 真实输入）
	roles := make([]string, 0, len(memberRoles))
	for _, role := range memberRoles {
		if _, ok := spec.Agents[role]; ok {
			roles = append(roles, role)
		}
	}
	logger.Info(logComponent).
		Str("team_name", spec.TeamName).
		Str("roles", strings.Join(roles, ",")).
		Str("session_id", p.SessionID).
		Int("mcps", len(mcpConfigs)).
		Str("agent_group", p.AgentGroupName).
		Msg("[swarm.assembly] enriched team spec")
	return nil
}

// SeedParamsFromRequestMetadata 从请求元数据提取 enrich 入参。
// 对齐 Python team_manager.py:2421-2426 的提取语义（分布式 bootstrap 调用点
// 与 get_swarm_enriched_team_spec 同源）：mode/project_dir/agent_group_name
// 均来自 request_metadata；user_id 由调用方显式传入（认证层已解出）。
func SeedParamsFromRequestMetadata(sessionID string, requestMetadata map[string]any) EnrichSeedParams {
	p := EnrichSeedParams{
		SessionID:       sessionID,
		RequestMetadata: requestMetadata,
	}
	if p.Mode == "" {
		p.Mode = strFromAny(requestMetadata["mode"])
	}
	p.ProjectDir = strFromAny(requestMetadata["project_dir"])
	return p
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// channelOrDefault Python: channel_id or "default"（assembly.py:491）。
func channelOrDefault(channelID string) string {
	if strings.TrimSpace(channelID) == "" {
		return "default"
	}
	return channelID
}

// disableTeammateWorktree Python（assembly.py:493-497）：
// channel=="web"（strip+lower 后）且非（team 模式且 code profile 模式）时禁用。
func disableTeammateWorktree(channelID, mode string) bool {
	if strings.ToLower(strings.TrimSpace(channelID)) != "web" {
		return false
	}
	return !(swschema.IsTeamMode(mode) && swschema.IsCodeProfileMode(mode))
}

// strFromAny any → 非空字符串（缺失/类型不符返回空串）。
func strFromAny(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// ──────────────────────────── AgentGroup 装配（assembly.py:140-360） ────────────────────────────

// memberRoles enrich 装配循环的角色集。
// Python: _MEMBER_ROLES (assembly.py:76)
var memberRoles = []string{"leader", "teammate"}

// legacyConfigMap 取 config.yaml 规范快照（Python get_config() 的 dict 形态）。
// Python: get_config (assembly.py:445)——Go 侧 config 读取面（D-15.16-c2①
// 约定：config=map[string]any）。经既有 config 加载器取规范 map
// （teamLoadConfig 同款形态，runtime_inheritance.go:642）。
func legacyConfigMap() map[string]any {
	c, err := configutil.New("", configutil.WithNormalize(configutil.NormalizeConfig))
	if err != nil {
		return map[string]any{}
	}
	m, err := c.Load()
	if err != nil {
		return map[string]any{}
	}
	return m
}

// mergeTeamPrompt 追加专家 prompt 不丢既有 Team prompt。
// Python: _merge_team_prompt (assembly.py:132-136)
func mergeTeamPrompt(existing string, expertPrompt string) string {
	parts := []string{}
	if strings.TrimSpace(existing) != "" {
		parts = append(parts, strings.TrimSpace(existing))
	}
	if strings.TrimSpace(expertPrompt) != "" {
		parts = append(parts, strings.TrimSpace(expertPrompt))
	}
	return strings.Join(parts, "\n\n")
}

// isDesignTeamMode 本轮 enrich 是否服务 team.design（设计画布）团队。
// Python: _is_design_team_mode (assembly.py:275-278)
func isDesignTeamMode(mode string) bool {
	return string(swschema.CanonicalizeMode(mode)) == string(swschema.ModeTeamDesign)
}

// applyAgentGroup 从一个解析的 AgentGroup 包播种团队。
// leader 恒取组的 leader 模板。scheduled dispatch 下团队以空名册起步：组内
// 其他成员不在此装配，leader 按需 spawn 任务所需成员（core 从已安装包按需
// 加载：spawn_expert / 内联 create_task 成员 / swarmflow agent_template）。
// 否则它们成为 hybrid 预定义名册。
// Python: _apply_agent_group (assembly.py:140-246)
func applyAgentGroup(spec *atschema.TeamAgentSpec, agentGroupName string, pkg *experts.LoadedAgentGroupPackage) error {
	var packageDir string
	if pkg == nil {
		// Python: 懒加载（assembly.py:146-154）——resolve_agent_group_dir
		// → load_agent_group_package_bundle。
		dir, err := experts.ResolveAgentGroupDir(agentGroupName)
		if err != nil {
			return err
		}
		loaded, loadErr := experts.LoadAgentGroupPackageBundle(dir)
		if loadErr != nil {
			return loadErr
		}
		pkg = loaded
	}
	templates := pkg.Templates
	packageDir = pkg.PackageDir
	// metadata 键保留给既有读者；agent_group_name 是 core spawn_expert 读的
	// 孵化字段（assembly.py:157-161）。
	if spec.Metadata == nil {
		spec.Metadata = map[string]any{}
	}
	spec.Metadata["agent_group_name"] = agentGroupName
	if caps := pkg.Capabilities(); len(caps) > 0 {
		spec.Metadata["capabilities"] = caps
	}
	spec.AgentGroupName = agentGroupName

	leaderBase, hasLeader := spec.Agents["leader"]
	teammateBase, hasTeammate := spec.Agents["teammate"]
	if !hasLeader || !hasTeammate {
		// Python: leader_base/teammate_base 缺一即报错（:165-168）；Go LeaderSpec
		// 是值类型无 nil 形态。
		return errors.New("AgentGroup assembly requires both base 'leader' and 'teammate' specs")
	}
	leaderMember := experts.ComposeTemplateMember(leaderBase, templates["leader"], "cn")
	// Python: spec.leader.prompt = _merge_team_prompt(spec.leader.prompt, persona)
	// （:169-174）——leader 卡的 prompt 合并专家 persona。
	spec.Leader.Persona = mergeTeamPrompt(spec.Leader.Persona, leaderMember.Persona)
	spec.Agents["leader"] = leaderMember.Spec
	if spec.DispatchMode == "scheduled" {
		// scheduled：空名册起步，成员等 leader 按需 spawn。
		return nil
	}

	predefinedMembers := []atschema.TeamMemberSpec{}
	for agentName, template := range templates {
		if agentName == experts.LeaderMember {
			continue
		}
		teammateCopy := teammateBase
		assembled := experts.ComposeTemplateMember(teammateCopy, template, "cn")
		memberPrompt := assembled.Persona
		externalConfig, extErr := externalPkg.ExternalCliAgentSpecFromTemplate(&template)
		if extErr != nil {
			return extErr
		}
		displayName := experts.ResolveAgentGroupMemberDisplayName(packageDir, agentName, template.AgentCard.BaseCard.Name)
		desc := template.AgentCard.BaseCard.Description
		if externalConfig == nil {
			spec.Agents[agentName] = assembled.Spec
			predefinedMembers = append(predefinedMembers, atschema.TeamMemberSpec{
				MemberName:  agentName,
				DisplayName: displayName,
				Desc:        desc,
				Prompt:      memberPrompt,
				RoleType:    atschema.TeamRoleTeammate,
			})
		} else {
			spec.Agents[agentName] = teammateBase
			predefinedMembers = append(predefinedMembers, atschema.TeamMemberSpec{
				MemberName:  agentName,
				DisplayName: displayName,
				Desc:        desc,
				Prompt:      memberPrompt,
				RoleType:    atschema.TeamRoleTeammate,
				ExternalCLI: externalConfig,
			})
		}
	}
	spec.PredefinedMembers = predefinedMembers
	spec.TeamMode = "hybrid"
	return nil
}

// applyDesignAgentGroup 从一个解析的 AgentGroup 包装配 team.design 名册。
// 设计团队（team.design）只应用 leader 模板：成员模板留在包内作为 worker 基座
// 由 TeamWorkerBackend 逐节点消费——它们从不进入 spawn 名册——leader 的协作
// 工具面被 DesignerSurfaceSanitizerRail 剥除（执行由服务驱动；leader 无运行
// 控制面）。
// 偏差 D-15.16-e1：分支体留缝——保留判断与步骤位置，designer profile 装配体
// （tools/rails/mcps/subagents 清空重建 + _DESIGN_LEADER_SYSTEM_PROMPT +
// build_member_capability_specs(NEW_TEAM_DESIGN) 调用）不落，⤴️25.12
// （designer 工具/rail 真实现同点回收）。
// Python: _apply_design_agent_group (assembly.py:277-360)
func applyDesignAgentGroup(spec *atschema.TeamAgentSpec, agentGroupName string) {
	// Python: load_agent_group_package(resolve_agent_group_dir(...))（:285-292）
	// ——留缝版本不加载包，走显式未实现路径（错误信息指向 25.12）。
	panic("design profile assembly not ported (D-15.16-e1, pending 25.12 designer chapter)")
}
