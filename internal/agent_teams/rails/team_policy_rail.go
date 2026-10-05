package rails

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/prompts"
	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools"
	harnessrails "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TeamPolicyRail 将团队策略作为有序 PromptSection 注入到系统提示词构建器。
// Python: TeamPolicyRail(DeepAgentRail) (rails/team_policy_rail.py)
//
// Section 分为两类：
//   - 静态：角色/工作流/生命周期/人设/额外。在构造时从参数构建，
//     每次 BeforeModelCall 重新添加到 builder（低开销 dict 插入）。
//   - 动态：team_info / team_members。由 MtimeSectionCache 支持，
//     在每次 LLM 调用前探测 DB 的 updated_at 变化后再全量拉取重建。
//
// 当 team_backend 为 nil 时（如只关心静态内容的单元测试），
// 动态缓存被完全跳过，Rail 表现与之前纯静态实现一致。
type TeamPolicyRail struct {
	harnessrails.DeepAgentRail
	// role 团队角色
	role atschema.TeamRole
	// persona 人设
	persona string
	// teammateMode 队友模式
	teammateMode string
	// lifecycle 生命周期
	lifecycle string
	// teamMode 团队模式
	teamMode string
	// basePrompt 基础提示词
	basePrompt string
	// language 语言
	language string
	// memberName 成员名
	memberName string
	// teamBackend 团队后端
	teamBackend *tools.TeamBackend
	// teamWorkspaceMount 工作空间挂载路径
	teamWorkspaceMount string
	// teamWorkspacePath 工作空间绝对路径
	teamWorkspacePath string
	// exposeHumanAgentsToTeammates 是否暴露人类成员给队友
	exposeHumanAgentsToTeammates bool
	// systemPromptBuilder 系统提示词构建器（Init 时缓存）
	systemPromptBuilder saprompt.SystemPromptBuilderInterface
	// staticSections 静态 PromptSection 列表（构造时构建）
	staticSections []saprompt.PromptSection
	// infoCache 团队信息动态缓存
	infoCache *prompts.MtimeSectionCache
	// membersCache 成员关系动态缓存
	membersCache *prompts.MtimeSectionCache
}

// TeamPolicyRailOption TeamPolicyRail 构造选项。
type TeamPolicyRailOption func(*TeamPolicyRail)

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// teamPolicyRailPriority TeamPolicyRail 优先级，对齐 Python priority=12
const teamPolicyRailPriority = 12

// ──────────────────────────── 全局变量 ────────────────────────────

// dynamicSectionNames 动态 Section 名称列表（对应 Python _DYNAMIC_SECTION_NAMES）
var dynamicSectionNames = []string{
	string(prompts.SectionInfo),
	string(prompts.SectionMembers),
}

// 编译时验证：确保 TeamPolicyRail 满足 DeepAgentRailProvider 接口
var _ harnessrails.DeepAgentRailProvider = (*TeamPolicyRail)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTeamPolicyRail 创建 TeamPolicyRail 实例。
// Python: TeamPolicyRail.__init__(role, persona, ...)
func NewTeamPolicyRail(opts ...TeamPolicyRailOption) *TeamPolicyRail {
	r := &TeamPolicyRail{
		DeepAgentRail: *harnessrails.NewDeepAgentRail(),
	}
	r.WithPriority(teamPolicyRailPriority)
	for _, opt := range opts {
		opt(r)
	}

	// Python: human_names = sorted(team_backend.human_agent_names()) if team_backend else []
	var humanNames []string
	if r.teamBackend != nil {
		humanNames = r.teamBackend.HumanAgentNames()
	}

	// Python: self._static_sections = self._build_static_sections(...)
	r.staticSections = r.buildStaticSections(humanNames)

	// Python: 动态 section 缓存
	if r.teamBackend != nil {
		r.infoCache = prompts.NewMtimeSectionCache(
			r.teamBackend.GetTeamUpdatedAt,
			r.fetchAndBuildInfoSection,
		)
		r.membersCache = prompts.NewMtimeSectionCache(
			r.teamBackend.GetMembersMaxUpdatedAt,
			r.fetchAndBuildMembersSection,
		)
	}

	return r
}

// Init 缓存 Agent 的共享 prompt builder。
// Python: TeamPolicyRail.init(agent) — 调用 super().init(agent) 后设置 system_prompt_builder
// Go 侧 DeepAgentRail.Init 是 no-op（BaseRail 默认实现），因此不调用 super init
func (r *TeamPolicyRail) Init(ctx context.Context, agent agentinterfaces.BaseAgent) error {
	r.systemPromptBuilder = agent.SystemPromptBuilder()
	return nil
}

// Uninit 从共享 builder 移除所有团队 Section。
// Python: TeamPolicyRail.uninit(agent)
func (r *TeamPolicyRail) Uninit(agent agentinterfaces.BaseAgent) error {
	if r.systemPromptBuilder != nil {
		for _, section := range r.staticSections {
			r.systemPromptBuilder.RemoveSection(section.Name)
		}
		for _, name := range dynamicSectionNames {
			r.systemPromptBuilder.RemoveSection(name)
		}
	}
	r.systemPromptBuilder = nil
	return nil
}

// BeforeModelCall 在每次模型调用前注入静态 Section + 刷新动态 Section。
// Python: TeamPolicyRail.before_model_call(ctx)
func (r *TeamPolicyRail) BeforeModelCall(ctx context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	if r.systemPromptBuilder == nil {
		return nil
	}

	// Python: for section in self._static_sections: builder.add_section(section)
	for _, section := range r.staticSections {
		r.systemPromptBuilder.AddSection(section)
	}

	// Python: if self._info_cache is not None
	if r.infoCache != nil {
		infoSection := r.infoCache.Refresh(ctx)
		if infoSection != nil {
			r.systemPromptBuilder.AddSection(*infoSection)
		}
	}

	// Python: if self._members_cache is not None
	if r.membersCache != nil {
		membersSection := r.membersCache.Refresh(ctx)
		if membersSection != nil {
			r.systemPromptBuilder.AddSection(*membersSection)
		}
	}

	return nil
}

// StaticSections 返回静态 Section 列表（仅用于测试）。
func (r *TeamPolicyRail) StaticSections() []saprompt.PromptSection {
	return r.staticSections
}

// WithPolicyRole 设置角色。
func WithPolicyRole(role atschema.TeamRole) TeamPolicyRailOption {
	return func(r *TeamPolicyRail) { r.role = role }
}

// WithPolicyPersona 设置人设。
func WithPolicyPersona(persona string) TeamPolicyRailOption {
	return func(r *TeamPolicyRail) { r.persona = persona }
}

// WithPolicyMemberName 设置成员名。
func WithPolicyMemberName(name string) TeamPolicyRailOption {
	return func(r *TeamPolicyRail) { r.memberName = name }
}

// WithPolicyLifecycle 设置生命周期。
func WithPolicyLifecycle(lifecycle string) TeamPolicyRailOption {
	return func(r *TeamPolicyRail) { r.lifecycle = lifecycle }
}

// WithPolicyTeammateMode 设置队友模式。
func WithPolicyTeammateMode(mode string) TeamPolicyRailOption {
	return func(r *TeamPolicyRail) { r.teammateMode = mode }
}

// WithPolicyLanguage 设置语言。
func WithPolicyLanguage(lang string) TeamPolicyRailOption {
	return func(r *TeamPolicyRail) { r.language = lang }
}

// WithPolicyTeamMode 设置团队模式。
func WithPolicyTeamMode(mode string) TeamPolicyRailOption {
	return func(r *TeamPolicyRail) { r.teamMode = mode }
}

// WithPolicyBasePrompt 设置基础提示词。
func WithPolicyBasePrompt(prompt string) TeamPolicyRailOption {
	return func(r *TeamPolicyRail) { r.basePrompt = prompt }
}

// WithPolicyTeamWorkspaceMount 设置工作空间挂载路径。
func WithPolicyTeamWorkspaceMount(mount string) TeamPolicyRailOption {
	return func(r *TeamPolicyRail) { r.teamWorkspaceMount = mount }
}

// WithPolicyTeamWorkspacePath 设置工作空间绝对路径。
func WithPolicyTeamWorkspacePath(path string) TeamPolicyRailOption {
	return func(r *TeamPolicyRail) { r.teamWorkspacePath = path }
}

// WithPolicyTeamBackend 设置团队后端。
func WithPolicyTeamBackend(tb *tools.TeamBackend) TeamPolicyRailOption {
	return func(r *TeamPolicyRail) { r.teamBackend = tb }
}

// WithPolicyExposeHumanAgents 设置是否暴露人类成员。
func WithPolicyExposeHumanAgents(v bool) TeamPolicyRailOption {
	return func(r *TeamPolicyRail) { r.exposeHumanAgentsToTeammates = v }
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// fetchAndBuildInfoSection 从 DB 重新加载团队元数据并重建 info Section。
// Python: TeamPolicyRail._fetch_and_build_info_section()
func (r *TeamPolicyRail) fetchAndBuildInfoSection(ctx context.Context) *saprompt.PromptSection {
	var teamInfo *prompts.TeamInfo
	if r.teamBackend != nil {
		info, err := r.teamBackend.GetTeamInfo(ctx)
		if err == nil && info != nil {
			teamInfo = &prompts.TeamInfo{
				TeamName:    info.TeamName,
				DisplayName: info.DisplayName,
				Description: info.Desc,
			}
		}
	}
	return prompts.BuildTeamInfoSection(
		teamInfo,
		r.teamWorkspaceMount,
		r.teamWorkspacePath,
		r.language,
	)
}

// fetchAndBuildMembersSection 从 DB 重新加载成员名册并重建 members Section。
// Python: TeamPolicyRail._fetch_and_build_members_section()
func (r *TeamPolicyRail) fetchAndBuildMembersSection(ctx context.Context) *saprompt.PromptSection {
	var members []prompts.TeamMember
	if r.teamBackend != nil {
		dbMembers, err := r.teamBackend.ListMembers(ctx)
		if err == nil && len(dbMembers) > 0 {
			members = make([]prompts.TeamMember, 0, len(dbMembers))
			for _, m := range dbMembers {
				members = append(members, prompts.TeamMember{
					MemberName:  m.MemberName,
					DisplayName: m.DisplayName,
					Description: m.Desc,
				})
			}
		}
	}
	return prompts.BuildTeamMembersSection(
		members,
		r.memberName,
		r.language,
	)
}

// buildStaticSections 构造不变的静态 Section 列表。
// Python: TeamPolicyRail._build_static_sections(...)
func (r *TeamPolicyRail) buildStaticSections(humanNames []string) []saprompt.PromptSection {
	var sections []saprompt.PromptSection

	// Role Section（总是存在）
	if s := prompts.BuildTeamRoleSection(r.role, r.memberName, r.teammateMode, r.language); s != nil {
		sections = append(sections, *s)
	}
	// HITT Section（仅当有人类成员时非 nil）
	if s := prompts.BuildTeamHITTSection(r.role, humanNames, r.language, r.memberName, r.exposeHumanAgentsToTeammates); s != nil {
		sections = append(sections, *s)
	}
	// Workflow Section（仅 Leader）
	if s := prompts.BuildTeamWorkflowSection(r.role, r.teamMode, r.language); s != nil {
		sections = append(sections, *s)
	}
	// Lifecycle Section（仅 Leader）
	if s := prompts.BuildTeamLifecycleSection(r.role, r.lifecycle, r.language); s != nil {
		sections = append(sections, *s)
	}
	// Persona Section（仅当 persona 非空）
	if s := prompts.BuildTeamPersonaSection(r.persona, r.language); s != nil {
		sections = append(sections, *s)
	}
	// Extra Section（仅当 basePrompt 非空）
	if s := prompts.BuildTeamExtraSection(r.basePrompt, r.language); s != nil {
		sections = append(sections, *s)
	}
	return sections
}
