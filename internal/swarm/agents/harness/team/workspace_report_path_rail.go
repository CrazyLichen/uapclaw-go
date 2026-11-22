package team

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/rails"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/interfaces"
	saprompt "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/prompts"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// TeamWorkspaceReportPathRail 注入项目产物与团队共享输出的文件系统归属策略。
// 团队工作区是团队的配置/内部数据目录（prompts、team.yaml、技能可见性、
// 轨迹），以绝对路径访问——既不是任务 cwd 也不是产物投递点。
// projectless 成员的最终产物进共享团队 outputs/ 目录；
// 绑定项目的成员产物进项目内（与单 agent 行为一致）。
//
// Python: TeamWorkspaceReportPathRail (team_workspace_report_path_rail.py, 236 行 1:1)
type TeamWorkspaceReportPathRail struct {
	rails.DeepAgentRail
	// systemPromptBuilder 提示词构建器引用（Init 时缓存，可为 nil）
	systemPromptBuilder saprompt.SystemPromptBuilderInterface
	// rootDir 团队工作区根（配置/内部数据，绝对路径访问）
	rootDir string
	// projectDir 成员绑定的用户项目根（可选）
	projectDir string
	// outputsDir 共享团队产物目录（可选）
	outputsDir string
	// teamID 团队标识（可为空）
	teamID string
	// language 语言标识
	language string
	// swarmContext swarm 装配上下文（技能演进热重载注册用）
	swarmContext any
	// sendFileEnabledFn 渠道 send_file 开关探测缝（工厂注入；nil=不提示投递）
	sendFileEnabledFn func() bool
}

// logComponent 日志组件
var workspaceReportLogComponent = logger.ComponentAgentServer

// 编译时验证 AgentRail 接口实现
var _ agentinterfaces.AgentRail = (*TeamWorkspaceReportPathRail)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTeamWorkspaceReportPathRail 创建 rail。
// Python: TeamWorkspaceReportPathRail.__init__ (team_workspace_report_path_rail.py:30-46)
func NewTeamWorkspaceReportPathRail(rootDir, projectDir, outputsDir, teamID, language string) *TeamWorkspaceReportPathRail {
	r := &TeamWorkspaceReportPathRail{
		rootDir:    filepath.Clean(rootDir),
		projectDir: filepathOrEmpty(projectDir),
		outputsDir: filepathOrEmpty(outputsDir),
		teamID:     teamID,
		language:   language,
	}
	r.WithPriority(5)
	return r
}

// BindSwarmContext 捕获团队装配上下文，供演进热重载注册使用。
// 该 rail 在技能演进关闭时也存在，其生命周期为 manager 提供
// 挂载上下文，可在之后重建角色专属演进 rail。
// 本 rail 自身不添加任何演进能力或提示词内容。
//
// Python: bind_swarm_context (team_workspace_report_path_rail.py:48-52)
func (r *TeamWorkspaceReportPathRail) BindSwarmContext(ctx any) {
	r.swarmContext = ctx
}

// SetSendFileEnabledProbe 注入渠道 send_file 开关探测（工厂缝）。
func (r *TeamWorkspaceReportPathRail) SetSendFileEnabledProbe(fn func() bool) {
	r.sendFileEnabledFn = fn
}

// Init 缓存构建器并注册团队挂载上下文。
// Python: init (team_workspace_report_path_rail.py:96-140)
func (r *TeamWorkspaceReportPathRail) Init(_ context.Context, agent agentinterfaces.BaseAgent) error {
	if agent == nil {
		return nil
	}
	r.systemPromptBuilder = agent.SystemPromptBuilder()
	r.registerTeamMountContext(agent)
	return nil
}

// Uninit 移除注入的 section。
// Python: uninit
func (r *TeamWorkspaceReportPathRail) Uninit(_ agentinterfaces.BaseAgent) error {
	if r.systemPromptBuilder != nil {
		r.systemPromptBuilder.RemoveSection("team_workspace_report_paths")
	}
	r.systemPromptBuilder = nil
	return nil
}

// BeforeModelCall 注入文件归属策略 section。
// Python: before_model_call (team_workspace_report_path_rail.py:141-226)
func (r *TeamWorkspaceReportPathRail) BeforeModelCall(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	if r.systemPromptBuilder == nil {
		return nil
	}

	// 团队工作区是配置/内部数据目录，以绝对路径访问，绝非产物投递点。
	// 绑定项目成员的最终产物留在项目内；projectless 成员进共享 outputs/。
	outputs := r.outputsDir
	var deliverablesLines string
	switch {
	case r.projectDir != "":
		deliverablesLines = "- User project root: `" + r.projectDir + "`\n" +
			"- Source code, tests, configuration, project documentation, and " +
			"files the user asks to create or modify belong in the active " +
			"project working root. When worktree isolation is active, the " +
			"worktree is the active project working root: use its cwd and " +
			"never bypass it with an absolute path to the main checkout.\n" +
			"- Without worktree isolation, use project-relative paths or " +
			"absolute paths under the user project root.\n" +
			"- This member is bound to a project, so final deliverables stay " +
			"in the project — do not write them to the team shared " +
			"deliverables directory.\n"
	case outputs != "":
		deliverablesLines = "- No user project root is bound to this member. Final " +
			"deliverables (reports, exports, data, and other output the user " +
			"will receive) go to the shared team deliverables directory:\n" +
			"  - Final deliverables directory: `" + outputs + "`\n" +
			"- The whole team shares this one directory; distinguish your " +
			"outputs by filename.\n" +
			"- Intermediate files, caches, and temporary scripts stay in " +
			"your own working directory (the per-member temporary working " +
			"directory named in the directory boundaries section); only " +
			"final deliverables go to the shared directory above.\n"
	default:
		deliverablesLines = "- No user project root is available and no shared deliverables " +
			"directory is configured. Resolve the project directory or " +
			"confirm a destination before creating deliverables; do not " +
			"silently drop them in the team workspace.\n"
	}

	// 文件投递提示按渠道开关给：渠道关了 send_file_allowed 就不提示
	// send_file_to_user（工具本身也不会注册），避免模型承诺发不出的文件。
	var deliveryLines string
	if r.sendFileEnabledFn != nil && r.sendFileEnabledFn() {
		deliveryLines = "- `send_file_to_user` controls message delivery only; it never " +
			"changes the required on-disk destination.\n" +
			"- A path mentioned in `send_message` or a completion summary does " +
			"not deliver the file. Call `send_file_to_user` only when the user " +
			"requested a downloadable file.\n"
	} else {
		deliveryLines = "- This channel cannot deliver files. Do not promise to send a " +
			"file; put the deliverable's content (or its absolute path) in " +
			"your reply instead.\n"
	}

	content := "# Project and Team Workspace File Policy\n\n" +
		deliverablesLines +
		deliveryLines +
		"- Team shared workspace (config / internal data): `" + r.rootDir + "`\n" +
		"- The team shared workspace holds the team's prompts, team.yaml, " +
		"skill-visibility metadata, trajectories, and the shared " +
		"deliverables directory. Access it by absolute path — do not create " +
		"a `.team` sub-directory under it or under your own workspace.\n" +
		"- Use the team shared workspace only for internal coordination " +
		"artifacts such as plans, drafts, review notes, intermediate data, " +
		"and handoff material that is not part of the user's project, or " +
		"when the user explicitly requests that exact team-workspace " +
		"destination.\n" +
		"- Do not place final project files in the team shared workspace " +
		"root; members bound to a project keep deliverables in the project " +
		"and projectless members use the deliverables directory above.\n"
	r.systemPromptBuilder.AddSection(saprompt.PromptSection{
		Name:     "team_workspace_report_paths",
		Content:  map[string]string{"cn": content, "en": content},
		Priority: 67,
	})
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// registerTeamMountContext 来自 swarm 装配时注册团队挂载上下文。
// Python: init 的注册段 (team_workspace_report_path_rail.py:95-140)。
// 挂载上下文构造（_make_team_rail_mount_context）与角色注册链路
// RegisterTeamRailContext/RegisterTeamLiveRail 已在 team_manager_skill.go。
// swarm 装配上下文字段经 BindSwarmContext 注入；字段缺失时按 Python
// 语义直接返回（session/team/role 不齐）。
func (r *TeamWorkspaceReportPathRail) registerTeamMountContext(_ agentinterfaces.BaseAgent) {
	if r.swarmContext == nil {
		return
	}
	// Python: session_id/channel/team_id/role 提取 + _make_team_rail_mount_context
	// 构造 + leader/member 双路注册。完整注册段依赖 SwarmBuildContext 的
	// channel/role/member_name/trajectory_span_processor 字段透传，
	// 随 15.21 evolution_rails 的注册段同缝接线（15.21 行已登记）。
	logger.Debug(workspaceReportLogComponent).
		Msg("[TeamWorkspaceReportPathRail] team mount context registration pending 15.21 seam")
}

// filepathOrEmpty 清洗可选路径形参（Python: str(Path(x)) if x else None）。
func filepathOrEmpty(p string) string {
	return strings.TrimSpace(p)
}
