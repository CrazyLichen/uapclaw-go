package prompts

import (
	"strings"

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

// ──────────────────────────── 非导出函数 ────────────────────────────

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
