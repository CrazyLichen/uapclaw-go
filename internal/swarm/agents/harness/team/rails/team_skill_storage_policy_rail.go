package rails

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/interfaces"
	saprompt "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/prompts"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/rails"
)

// TeamSkillStoragePolicyRail 告知团队成员技能产出的存放位置。
//
// 策略文本对团队每个成员完全一致，且常驻系统提示词，
// 因此整个团队共享一个可缓存的 prompt 前缀。文本只点名团队级路径；
// 成员自身工作区是按成员分配的，由团队侧 rail 作为成员身份的一部分交付，
// 并附带「不得在其中新建技能」的规则。
//
// Python: TeamSkillStoragePolicyRail (team_skill_storage_policy_rail.py:18-138)
type TeamSkillStoragePolicyRail struct {
	rails.DeepAgentRail
	// systemPromptBuilder 成员持有的系统提示词构建器（init 时捕获）
	systemPromptBuilder saprompt.SystemPromptBuilderInterface
	// globalSkillsDir 全局公共技能目录（每个 agent 读取的唯一物理技能库）
	globalSkillsDir string
	// teamWorkspaceRoot 团队共享工作区根（不存放任何 Skill 源）
	teamWorkspaceRoot string
	// teamSkillVisibilityFile 团队 skills-visibility.json 路径（只声明可见性，绝不是 Skill 源）
	teamSkillVisibilityFile string
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// teamSkillStoragePolicyRailPriority rail 挂载优先级
	// Python: TeamSkillStoragePolicyRail.priority = 5
	teamSkillStoragePolicyRailPriority = 5

	// teamSkillStoragePolicySection 注入节名
	// Python: TeamSkillStoragePolicyRail.SECTION_NAME = "team_skill_storage_policy"
	teamSkillStoragePolicySection = "team_skill_storage_policy"

	// teamSkillStoragePolicySectionPriority 注入节优先级
	// Python: TeamSkillStoragePolicyRail.SECTION_PRIORITY = 39
	teamSkillStoragePolicySectionPriority = 39
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTeamSkillStoragePolicyRail 将 rail 绑定到策略点名的团队级路径。
//
// Python: TeamSkillStoragePolicyRail.__init__ (team_skill_storage_policy_rail.py:32-52)
func NewTeamSkillStoragePolicyRail(globalSkillsDir, teamWorkspaceRoot, teamSkillVisibilityFile string) *TeamSkillStoragePolicyRail {
	r := &TeamSkillStoragePolicyRail{
		DeepAgentRail:           *rails.NewDeepAgentRail(),
		globalSkillsDir:         globalSkillsDir,
		teamWorkspaceRoot:       teamWorkspaceRoot,
		teamSkillVisibilityFile: teamSkillVisibilityFile,
	}
	r.WithPriority(teamSkillStoragePolicyRailPriority)
	return r
}

// Init 捕获成员持有的系统提示词构建器。
//
// Python: TeamSkillStoragePolicyRail.init (team_skill_storage_policy_rail.py:54-56)
func (r *TeamSkillStoragePolicyRail) Init(_ context.Context, agent agentinterfaces.BaseAgent) error {
	r.systemPromptBuilder = agent.SystemPromptBuilder()
	return nil
}

// Uninit 移除注入的策略节。
//
// Python: TeamSkillStoragePolicyRail.uninit (team_skill_storage_policy_rail.py:58-63)
func (r *TeamSkillStoragePolicyRail) Uninit(_ agentinterfaces.BaseAgent) error {
	if r.systemPromptBuilder != nil {
		r.systemPromptBuilder.RemoveSection(teamSkillStoragePolicySection)
	}
	r.systemPromptBuilder = nil
	return nil
}

// BeforeModelCall 每次模型调用前注入存储策略。
//
// Python: TeamSkillStoragePolicyRail.before_model_call (team_skill_storage_policy_rail.py:65-80)
func (r *TeamSkillStoragePolicyRail) BeforeModelCall(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	if r.systemPromptBuilder == nil {
		return nil
	}

	r.systemPromptBuilder.AddSection(saprompt.PromptSection{
		Name: teamSkillStoragePolicySection,
		Content: map[string]string{
			"cn": r.buildCNContent(),
			"en": r.buildENContent(),
		},
		Priority: teamSkillStoragePolicySectionPriority,
	})
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// buildCNContent 构建中文提示词节。
//
// Python: TeamSkillStoragePolicyRail._build_cn_content (team_skill_storage_policy_rail.py:82-97)
func (r *TeamSkillStoragePolicyRail) buildCNContent() string {
	visibilityOnlyPaths := r.formatVisibilityOnlyPaths("cn")
	return "# Team Skill 存储规则\n\n" +
		"在 team 模式中，凡是创建、转换、修改或优化 Skill / Swarm Skill / Team Skill，" +
		"最终 skill 源目录必须写入全局公共技能目录。\n\n" +
		"- 全局公共技能目录：`" + r.globalSkillsDir + "`\n" +
		"- 新 skill 的入口文件必须是：`" + r.globalSkillsDir + "/<skill-name>/SKILL.md`\n" +
		"- `assets/`、`scripts/`、`references/`、模板、示例、验证脚本等配套资源，" +
		"必须放在同一个 `<skill-name>/` 目录下。\n" +
		"- 成员 workspace 和 team workspace 下只有 `" + agent_teams.SKILL_VISIBILITY_FILENAME + "` " +
		"这一个可见性声明文件，绝不能在其中创建 skill 实体；它们不存放任何 skill 源文件。\n" +
		visibilityOnlyPaths +
		"- 所有 skill 实体唯一存放于上面的全局公共技能目录，也不要写到当前目录或临时目录。"
}

// buildENContent 构建英文提示词节。
//
// Python: TeamSkillStoragePolicyRail._build_en_content (team_skill_storage_policy_rail.py:99-117)
func (r *TeamSkillStoragePolicyRail) buildENContent() string {
	visibilityOnlyPaths := r.formatVisibilityOnlyPaths("en")
	return "# Team Skill Storage Policy\n\n" +
		"In team mode, whenever you create, convert, modify, or optimize a Skill, " +
		"Swarm Skill, or Team Skill, the final skill source directory must be " +
		"written under the global shared skills directory.\n\n" +
		"- Global shared skills directory: `" + r.globalSkillsDir + "`\n" +
		"- A new skill entry file must be: `" + r.globalSkillsDir + "/<skill-name>/SKILL.md`\n" +
		"- Put related `assets/`, `scripts/`, `references/`, templates, examples, " +
		"and validation scripts under the same `<skill-name>/` directory.\n" +
		"- A member workspace and the team workspace hold a single " +
		"`" + agent_teams.SKILL_VISIBILITY_FILENAME + "` visibility declaration and nothing else " +
		"about skills; never create a skill entity inside them.\n" +
		visibilityOnlyPaths +
		"- Every skill entity lives in the global shared skills directory above, " +
		"and never in the current directory or a temporary directory."
}

// formatVisibilityOnlyPaths 格式化「只声明可见性、不存放任何内容」的团队级路径。
//
// 只有团队级路径出现在这里：它们对每个成员完全一致，
// 以保持 prompt 前缀可共享。成员自身工作区是按成员分配的，
// 由团队 rail 作为其身份的一部分告知。
//
// Python: TeamSkillStoragePolicyRail._format_visibility_only_paths (team_skill_storage_policy_rail.py:119-138)
func (r *TeamSkillStoragePolicyRail) formatVisibilityOnlyPaths(language string) string {
	separator := "："
	if language != "cn" {
		separator = ": "
	}
	var lines string
	if r.teamWorkspaceRoot != "" {
		label := "team 共享工作区"
		if language != "cn" {
			label = "Team shared workspace"
		}
		lines += "- " + label + separator + "`" + r.teamWorkspaceRoot + "`\n"
	}
	if r.teamSkillVisibilityFile != "" {
		label := "team skill 可见性声明文件"
		if language != "cn" {
			label = "Team skill visibility declaration file"
		}
		lines += "- " + label + separator + "`" + r.teamSkillVisibilityFile + "`\n"
	}
	return lines
}
