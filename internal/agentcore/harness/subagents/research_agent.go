package subagents

import (
	llm "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	hprompts "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// ──────────────────────────── 常量 ────────────────────────────

// ResearchAgentFactoryName research 子代理工厂名称
// 对齐 Python: RESEARCH_AGENT_FACTORY_NAME
const ResearchAgentFactoryName = "research_agent"

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// defaultResearchAgentSystemPrompt 默认系统提示词
	// 对齐 Python: DEFAULT_RESEARCH_AGENT_SYSTEM_PROMPT
	defaultResearchAgentSystemPrompt = map[string]string{
		"cn": "你是研究助理，负责围绕用户输入的主题开展调研，仅需返回最终研究结果。",
		"en": "You are a research assistant responsible for conducting research around the topic provided by the user.Only return the final research results.",
	}
	// defaultResearchAgentDescription 默认描述
	// 对齐 Python: DEFAULT_RESEARCH_AGENT_DESCRIPTION
	defaultResearchAgentDescription = map[string]string{
		"cn": "专注于研究调查任务，当用户想要调查某问题时，可使用该代理执行研究工作。每次只给这位研究员一个主题。",
		"en": "Focuses on research and investigation tasks. \nWhen users want to investigate a specific issue, this agent can be used to execute research work. \nProvide only one topic to this researcher at a time.",
	}
)

// ──────────────────────────── 导出函数 ────────────────────────────

// BuildResearchAgentConfig 构建 research 子代理配置（延迟实例化）。
// 对齐 Python: build_research_agent_config(model, language=..., workspace=..., ...)
//
// 参数通过 SubagentCreateParams 传入，对齐 Python 的具名参数风格。
// adapter 层负责从 map[string]any 解析出 SubagentCreateParams。
func BuildResearchAgentConfig(model *llm.Model, params *hschema.SubagentCreateParams) *hschema.SubAgentConfig {
	language := hprompts.ResolveLanguage(params.Language)

	cfg := hschema.NewSubAgentConfig()

	// AgentCard：用户未提供时使用默认
	cfg.AgentCard = params.Card
	if cfg.AgentCard == nil {
		desc := defaultResearchAgentDescription[language]
		if desc == "" {
			desc = defaultResearchAgentDescription["cn"]
		}
		cfg.AgentCard = agentschema.NewAgentCard(
			agentschema.WithAgentName(ResearchAgentFactoryName),
			agentschema.WithAgentDescription(desc),
		)
	}

	// SystemPrompt：用户未提供时使用默认
	cfg.SystemPrompt = params.SystemPrompt
	if cfg.SystemPrompt == "" {
		prompt := defaultResearchAgentSystemPrompt[language]
		if prompt == "" {
			prompt = defaultResearchAgentSystemPrompt["cn"]
		}
		cfg.SystemPrompt = prompt
	}

	cfg.Tools = params.Tools
	cfg.ToolInstances = params.ToolInstances
	cfg.Mcps = params.Mcps
	cfg.Model = model
	cfg.Rails = params.Rails
	cfg.Skills = params.Skills
	cfg.Backend = params.Backend
	cfg.Workspace = params.Workspace
	cfg.SysOperation = params.SysOperation
	cfg.Language = language
	cfg.PromptMode = params.PromptMode
	cfg.EnableTaskLoop = params.EnableTaskLoop

	// MaxIterations：用户未提供（0）时默认 15
	// 对齐 Python: max_iterations=15
	cfg.MaxIterations = params.MaxIterations
	if cfg.MaxIterations == 0 {
		cfg.MaxIterations = 15
	}

	cfg.FactoryName = ResearchAgentFactoryName
	cfg.FactoryKwargs = nil
	cfg.EnablePlanMode = params.EnablePlanMode
	// RestrictToWorkDir：params 为 *bool 指针，nil 表示未设置（保持 NewSubAgentConfig 默认 true），
	// 非 nil 则使用用户显式指定的值
	// 对齐 Python: build_research_agent_config 不传 restrict_to_work_dir，使用 SubAgentConfig 默认值
	if params.RestrictToWorkDir != nil {
		cfg.RestrictToWorkDir = *params.RestrictToWorkDir
	}

	return cfg
}

// DefaultResearchAgentSystemPrompt 返回指定语言的默认系统提示词。
// 对齐 Python: DEFAULT_RESEARCH_AGENT_SYSTEM_PROMPT.get(resolved_language, ...)
func DefaultResearchAgentSystemPrompt(language string) string {
	if s, ok := defaultResearchAgentSystemPrompt[language]; ok && s != "" {
		return s
	}
	return defaultResearchAgentSystemPrompt["cn"]
}

// DefaultResearchAgentDescription 返回指定语言的默认描述。
// 对齐 Python: DEFAULT_RESEARCH_AGENT_DESCRIPTION.get(resolved_language, ...)
func DefaultResearchAgentDescription(language string) string {
	if s, ok := defaultResearchAgentDescription[language]; ok && s != "" {
		return s
	}
	return defaultResearchAgentDescription["cn"]
}
