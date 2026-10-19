package team

import (
	"context"
	"path/filepath"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TeamWorkspaceReportPathRail 报告真实 team workspace 产物路径的提示 rail。
// 对齐 Python: TeamWorkspaceReportPathRail (rails/team_workspace_report_path_rail.py)
type TeamWorkspaceReportPathRail struct {
	rails.DeepAgentRail
	// systemPromptBuilder 提示词构建器引用（Init 时缓存，可为 nil）
	systemPromptBuilder saprompt.SystemPromptBuilderInterface
	// rootDir 工作区根目录（绝对路径）
	rootDir string
	// teamID 团队标识（可为空）
	teamID string
	// language 语言标识
	language string
}

// 编译时验证 AgentRail 接口实现
var _ agentinterfaces.AgentRail = (*TeamWorkspaceReportPathRail)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTeamWorkspaceReportPathRail 创建 TeamWorkspaceReportPathRail 实例。
// 对齐 Python: TeamWorkspaceReportPathRail(*, root_dir, team_id=None, language="cn")
func NewTeamWorkspaceReportPathRail(rootDir string, teamID string, language string) *TeamWorkspaceReportPathRail {
	r := &TeamWorkspaceReportPathRail{
		rootDir:  filepath.Clean(rootDir),
		teamID:   teamID,
		language: language,
	}
	r.WithPriority(5)
	return r
}

// Init 缓存 agent 的 SystemPromptBuilder（可能为 nil）。
// 对齐 Python: init(self, agent)
func (r *TeamWorkspaceReportPathRail) Init(ctx context.Context, agent agentinterfaces.BaseAgent) error {
	if agent == nil {
		return nil
	}
	r.systemPromptBuilder = agent.SystemPromptBuilder()
	return nil
}

// Uninit 移除注入的 section。
// 对齐 Python: uninit(self, agent)
func (r *TeamWorkspaceReportPathRail) Uninit(agent agentinterfaces.BaseAgent) error {
	if r.systemPromptBuilder != nil {
		r.systemPromptBuilder.RemoveSection("team_workspace_report_paths")
	}
	r.systemPromptBuilder = nil
	return nil
}

// BeforeModelCall 注入 team workspace 路径指引 section。
// 对齐 Python: before_model_call(self, ctx)
func (r *TeamWorkspaceReportPathRail) BeforeModelCall(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	if r.systemPromptBuilder == nil {
		return nil
	}

	mount := ".team/"
	if r.teamID != "" {
		mount = ".team/" + r.teamID + "/"
	}
	sample := filepath.Join(r.rootDir, "prd-review-memo.md")
	content := "# Team Workspace Artifact Paths\n\n" +
		"- Team workspace absolute root: `" + r.rootDir + "`\n" +
		"- Internal mount path: `" + mount + "`\n" +
		"- Use the internal mount path only for tool read/write operations.\n" +
		"- When telling the user where an artifact was saved, report the real absolute filesystem path under " +
		"the team workspace absolute root, not the `.team/...` mount path.\n" +
		"- If a generated artifact path contains `.team/<team>/team-workspace/`, remove that mount prefix and " +
		"join the remaining file name under the team workspace absolute root before reporting it.\n" +
		"- Example saved location to report: `" + sample + "`\n"

	r.systemPromptBuilder.AddSection(saprompt.NewPromptSection(
		"team_workspace_report_paths",
		map[string]string{"cn": content, "en": content},
		67,
	))
	return nil
}
