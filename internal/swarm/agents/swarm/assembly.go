package swarm

import (
	"strings"

	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	agentteams "github.com/uapclaw/uapclaw-go/internal/agent_teams"
	obs "github.com/uapclaw/uapclaw-go/internal/agent_teams/observability"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
	"github.com/uapclaw/uapclaw-go/internal/swarm/common"
	swschema "github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// logComponent 组件级日志组件。
const logComponent = logger.ComponentAgentServer

// ──────────────────────────── 结构体 ────────────────────────────

// EnrichSeedParams EnrichTeamSpecForSwarmSeed 的入参。
// 对齐 Python enrich_team_spec_for_swarm（assembly.py:391-437）签名中本节
// 消费的子集；agent_group_name/agent_group_package ⤵️15.16e 不入参。
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
}

// ──────────────────────────── 导出函数 ────────────────────────────

// EnrichTeamSpecForSwarmSeed 就地 enrich 团队 spec 的 context 构造段：
// 构建每团队基础 SwarmBuildContext、挂活载体、携带可序列化种子。
//
// 偏差 D-15.16-c3：Python enrich_team_spec_for_swarm（assembly.py:391-570）
// 单函数跨 rails/mcp/experts 多章内容——本节只承担 context 构造子链
//（:456-541）；rails 目录注册 ⤵️15.7/15.8、_ensure_external_team_transport
// ⤵️15.16d/swarmflow、成员 spec 重写/mcp/agent_group ⤵️15.16e 及成员配置
// 源装配章节，届时并入同一 Go 函数并删除本拆分壳。
//
// 偏差 D-15.16-c5：尾日志 mcps/agent_group 两字段本节无输入，按 Python
// 无该输入时的字面值（0/None）记录，随对应章节落地改为真实输入驱动。
//
// 偏差 D-15.16-c8 收尾：接线点为 TeamManager.GetSwarmEnrichedTeamSpec
//（对齐 Python 活跃路径 get_swarm_enriched_team_spec，team_manager.py:1958；
// 旧 GetEnrichedTeamSpec/GetEnrichedTeamSpec 路径已随 c8 收尾批删除）。
func EnrichTeamSpecForSwarmSeed(spec *atschema.TeamAgentSpec, p EnrichSeedParams) error {
	// 步骤 1: 注册种子工厂（对齐 :456 register_swarm_providers 的工厂段）
	RegisterSwarmBuildContextFactory()

	// 步骤 2: _ensure_external_team_transport（:457）⤵️15.16d/swarmflow

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

	// 步骤 7: base = SwarmBuildContext(...)（:485-535）
	base := NewSwarmBuildContext(SwarmBuildContextParams{
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
		TrajectorySpanProcessor: obs.GetTrajectorySpanProcessor(),
		HeartbeatJobService:     GetHeartbeatJobService(),
	})

	// 步骤 8: 挂活载体 + 携带可序列化种子（:536/:540 ★）
	// 种子随活载体同行，跨序列化边界（spawned teammate / 分布式远端 / 冷
	// 恢复）重建的成员经注册工厂还原上下文。
	spec.BuildContext = base
	spec.BuildContextSeed = base.ToSeed()

	// 步骤 9: 尾日志（:563-570；mcps/agent_group 按 c5 记字面值）
	roles := make([]string, 0, len(spec.Agents))
	for _, role := range []string{"leader", "teammate"} {
		if _, ok := spec.Agents[role]; ok {
			roles = append(roles, role)
		}
	}
	logger.Info(logComponent).
		Str("team_name", spec.TeamName).
		Str("roles", strings.Join(roles, ",")).
		Str("session_id", p.SessionID).
		Int("mcps", 0).
		Str("agent_group", "").
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
