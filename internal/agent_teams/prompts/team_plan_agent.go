package prompts

import (
	"strings"

	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// TeamPlanAgentDesc 团队规划 Agent 的中英文描述。
// Python: TEAM_PLAN_AGENT_DESC (openjiuwen/agent_teams/prompts/team_plan_agent.py)
var TeamPlanAgentDesc = map[string]string{
	"cn": "团队规划专家。基于目标、约束和上下文设计团队执行方案、分工、依赖和验收计划。",
	"en": "Team planning specialist. Designs team execution strategy, role split, " +
		"dependencies, and acceptance plans from goals, constraints, and context.",
}

// teamPlanAgentSystemPromptCN 中文 plan_agent 系统提示词（从模板加载，包级初始化）。
// Python: TEAM_PLAN_AGENT_SYSTEM_PROMPT_CN (openjiuwen/agent_teams/prompts/team_plan_agent.py)
var teamPlanAgentSystemPromptCN string

// teamPlanAgentSystemPromptEN 英文 plan_agent 系统提示词（从模板加载，包级初始化）。
// Python: TEAM_PLAN_AGENT_SYSTEM_PROMPT_EN (openjiuwen/agent_teams/prompts/team_plan_agent.py)
var teamPlanAgentSystemPromptEN string

// DefaultTeamPlanAgentSystemPrompt plan_agent 中英文系统提示词映射。
// Python: DEFAULT_TEAM_PLAN_AGENT_SYSTEM_PROMPT (openjiuwen/agent_teams/prompts/team_plan_agent.py)
var DefaultTeamPlanAgentSystemPrompt map[string]string

// ──────────────────────────── 导出函数 ────────────────────────────

// TeamPlanAgentSystemPrompt 返回指定语言的 plan_agent 系统提示词。
// Python: _team_plan_agent_prompt(language) (openjiuwen/agent_teams/prompts/team_plan_agent.py)
func TeamPlanAgentSystemPrompt(language string) string {
	if language == "en" {
		return teamPlanAgentSystemPromptEN
	}
	return teamPlanAgentSystemPromptCN
}

// TeamPlanAgentDescription 返回指定语言的 plan_agent 描述。
// Python: _team_plan_agent_description(language) (openjiuwen/agent_teams/prompts/team_plan_agent.py)
func TeamPlanAgentDescription(language string) string {
	if desc, ok := TeamPlanAgentDesc[language]; ok {
		return desc
	}
	return TeamPlanAgentDesc["cn"]
}

// BuildTeamPlanAgentCard 构建 plan_agent 的 AgentCard。
// Python: build_team_plan_agent_card(language) (openjiuwen/agent_teams/prompts/team_plan_agent.py)
func BuildTeamPlanAgentCard(language string) *agentschema.AgentCard {
	desc := TeamPlanAgentDescription(language)
	return agentschema.NewAgentCard(
		agentschema.WithAgentName("plan_agent"),
		agentschema.WithAgentDescription(desc),
	)
}

// ApplyTeamPlanAgentPrompt 特化内置 plan_agent，将其系统提示词替换为 team.plan 版本。
// Python: apply_team_plan_agent_prompt(subagents, language=...) (openjiuwen/agent_teams/prompts/team_plan_agent.py)
//
// 仅替换使用默认 plan 提示词的 plan_agent，用户自定义的 plan_agent 提示词保持不变。
// 返回 true 表示已替换，false 表示未找到或用户自定义。
func ApplyTeamPlanAgentPrompt(subagents []hschema.SubagentSpec, language string) bool {
	if len(subagents) == 0 {
		return false
	}

	// Python: builtin_prompts = set(DEFAULT_PLAN_AGENT_SYSTEM_PROMPT.values())
	resolvedLang := resolveLanguage(language)
	builtinPromptSet := make(map[string]struct{})
	for _, v := range DefaultTeamPlanAgentSystemPrompt {
		builtinPromptSet[v] = struct{}{}
	}

	for _, spec := range subagents {
		// 只处理 *SubAgentConfig 类型
		cfg, ok := spec.(*hschema.SubAgentConfig)
		if !ok {
			continue
		}
		if cfg.AgentCard == nil || cfg.AgentCard.Name != "plan_agent" {
			continue
		}
		// Python: if spec.system_prompt not in builtin_prompts: return False
		if _, ok := builtinPromptSet[cfg.SystemPrompt]; !ok {
			return false
		}
		// Python: spec.system_prompt = _team_plan_agent_prompt(resolved_language)
		cfg.SystemPrompt = TeamPlanAgentSystemPrompt(resolvedLang)
		// Python: spec.agent_card = spec.agent_card.model_copy(update={"description": ...})
		cfg.AgentCard = agentschema.NewAgentCard(
			agentschema.WithAgentName("plan_agent"),
			agentschema.WithAgentDescription(TeamPlanAgentDescription(resolvedLang)),
		)
		return true
	}
	return false
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// resolveLanguage 解析语言参数，空字符串回退到 "cn"。
// Python: resolve_language(language) (openjiuwen/agent_teams/prompts/__init__.py)
func resolveLanguage(language string) string {
	if language == "en" {
		return "en"
	}
	return "cn"
}

// init 初始化 plan_agent 的中英文系统提示词（从模板加载）
func init() {
	tplCN := LoadTemplate("team_plan_agent", "cn")
	if tplCN != nil {
		teamPlanAgentSystemPromptCN = strings.TrimSpace(tplCN.Content())
	}
	tplEN := LoadTemplate("team_plan_agent", "en")
	if tplEN != nil {
		teamPlanAgentSystemPromptEN = strings.TrimSpace(tplEN.Content())
	}
	DefaultTeamPlanAgentSystemPrompt = map[string]string{
		"cn": teamPlanAgentSystemPromptCN,
		"en": teamPlanAgentSystemPromptEN,
	}
}
