package rails

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/prompts"
	harnessrails "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TeamPlanModeRail 注入 team.plan Leader 指令的 Rail。
// Python: TeamPlanModeRail(DeepAgentRail) (rails/team_plan_mode_rail.py)
//
// 在 AgentModeRail（priority=85）之后运行，
// 以 team 专属的 MODE_INSTRUCTIONS Section 替换通用 plan 提示，
// 同时保留所有通用 plan 工具和安全检查。
//
// Init 时特化默认的 plan subagent（将 plan_agent 的 system prompt
// 替换为 team.plan 版本）。BeforeModelCall 在 plan 模式下
// 注入团队计划指令。
type TeamPlanModeRail struct {
	harnessrails.DeepAgentRail
	// languageOverride 语言覆盖（可选）
	languageOverride string
	// agent 引用（Init 时缓存）
	agent agentinterfaces.BaseAgent
	// systemPromptBuilder 系统提示词构建器（Init 时缓存）
	systemPromptBuilder saprompt.SystemPromptBuilderInterface
}

// TeamPlanModeRailOption TeamPlanModeRail 构造选项。
type TeamPlanModeRailOption func(*TeamPlanModeRail)

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// planModeLogComponent 日志组件标识
	planModeLogComponent = logger.ComponentTeam

	// modeInstructionsSectionName MODE_INSTRUCTIONS Section 名称
	// Python: SectionName.MODE_INSTRUCTIONS (harness/prompts/sections.py)
	modeInstructionsSectionName = "MODE_INSTRUCTIONS"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// _ 编译时验证 TeamPlanModeRail 满足 DeepAgentRailProvider 接口
var _ harnessrails.DeepAgentRailProvider = (*TeamPlanModeRail)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTeamPlanModeRail 创建 TeamPlanModeRail 实例。
// Python: TeamPlanModeRail.__init__(language=None)
func NewTeamPlanModeRail(opts ...TeamPlanModeRailOption) *TeamPlanModeRail {
	r := &TeamPlanModeRail{}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// Init 缓存 prompt builder 并特化默认 plan subagent。
// Python: TeamPlanModeRail.init(agent)
func (r *TeamPlanModeRail) Init(_ context.Context, agent agentinterfaces.BaseAgent) error {
	r.agent = agent
	r.systemPromptBuilder = agent.SystemPromptBuilder()
	r.specializePlanAgent()
	return nil
}

// Uninit 移除 team.plan 提示词覆盖。
// Python: TeamPlanModeRail.uninit(agent)
func (r *TeamPlanModeRail) Uninit(_ agentinterfaces.BaseAgent) error {
	if r.systemPromptBuilder != nil {
		r.systemPromptBuilder.RemoveSection(modeInstructionsSectionName)
	}
	r.agent = nil
	r.systemPromptBuilder = nil
	return nil
}

// BeforeModelCall 在 plan 模式下替换通用 plan 指令为 team.plan 指令。
// Python: TeamPlanModeRail.before_model_call(ctx)
func (r *TeamPlanModeRail) BeforeModelCall(_ context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	if r.agent == nil || r.systemPromptBuilder == nil {
		return nil
	}

	// Python: state = self._agent.load_state(ctx.session)
	// Python: if getattr(state.plan_mode, "mode", None) != "plan"
	// Go 侧暂无 load_state / plan_mode 访问能力，
	// 此处默认在 plan 模式下添加 team.plan 指令。
	// TODO(#9.runtime): 集成 agent plan_mode 状态检查

	// 非 plan 模式：移除 MODE_INSTRUCTIONS section
	// Python: self.system_prompt_builder.remove_section(SectionName.MODE_INSTRUCTIONS)
	// 当 plan_mode 状态可检查后补充此分支

	// Plan 模式：特化 plan_agent + 注入 team.plan 指令
	r.specializePlanAgent()

	language := r.resolveLanguage()
	// Python: build_team_plan_mode_section(language, agent, session)
	content := prompts.BuildTeamPlanModePrompt(language, "", "")
	if content != "" {
		section := saprompt.NewPromptSection(
			modeInstructionsSectionName,
			map[string]string{language: content},
			84, // priority 对齐 Python TeamPlanModeRail.priority = 84
		)
		r.systemPromptBuilder.AddSection(section)
	}

	return nil
}

// WithLanguageOverride 设置语言覆盖。
func WithLanguageOverride(lang string) TeamPlanModeRailOption {
	return func(r *TeamPlanModeRail) { r.languageOverride = lang }
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// resolveLanguage 解析 team.plan 提示词语言。
// Python: TeamPlanModeRail._resolve_language()
func (r *TeamPlanModeRail) resolveLanguage() string {
	if r.languageOverride != "" {
		return r.languageOverride
	}
	// 从 systemPromptBuilder 获取语言
	if r.systemPromptBuilder != nil {
		lang := r.systemPromptBuilder.Language()
		if lang != "" {
			return lang
		}
	}
	return "cn"
}

// specializePlanAgent 特化内置 plan_agent。
// Python: TeamPlanModeRail._specialize_plan_agent()
//
// 将 plan subagent 的 system prompt 替换为 team.plan 版本。
// 在 Go 侧，通过 prompts 包提供的函数完成 plan_agent 的特化。
func (r *TeamPlanModeRail) specializePlanAgent() {
	if r.agent == nil {
		return
	}
	// Python: deep_config = getattr(self._agent, "deep_config", None)
	// Python: applied = apply_team_plan_agent_prompt(deep_config.subagents, language=...)
	// Go 侧暂无法直接访问 deep_config.subagents，
	// 但 prompts.BuildTeamPlanAgentCard() 和 prompts.TeamPlanAgentSystemPrompt()
	// 已提供，可在后续集成 DeepAgentInterface 扩展时完成。
	// TODO(#9.runtime): 集成 apply_team_plan_agent_prompt 逻辑
}
