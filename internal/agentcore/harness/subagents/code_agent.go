package subagents

import (
	llm "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	hprompts "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// CodeAgentFactoryName code 子代理工厂名称
// 对齐 Python: CODE_AGENT_FACTORY_NAME
const CodeAgentFactoryName = "code_agent"

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// defaultCodeAgentSystemPrompt 默认系统提示词
	// 对齐 Python: DEFAULT_CODE_AGENT_SYSTEM_PROMPT
	// 提示词一比一复刻 Python 原文，不做自行翻译
	defaultCodeAgentSystemPrompt = map[string]string{
		"cn": "你是一个 AI 编程助手，规则：能用工具就用工具（读/写/编辑/grep/list/bash/code），不要猜文件内容；变更要小、可回滚；" +
			"先澄清数据结构与接口，再动代码；输出给出测试/验证步骤。",
		"en": "You are an AI Coding Agent. " +
			"Rules: Use tools whenever possible (read/write/edit/grep/list/bash/code), don't guess file contents;" +
			"make small, reversible changes; clarify data structures and interfaces before modifying code; " +
			"provide testing/verification steps in your output.",
	}
	// defaultCodeAgentDescription 默认描述
	// 对齐 Python: DEFAULT_CODE_AGENT_DESCRIPTION
	// 描述一比一复刻 Python 原文，不做自行翻译
	defaultCodeAgentDescription = map[string]string{
		"cn": "资深软件工程师与代码代理。擅长把任务落到可运行的代码与可验证的结果。",
		"en": "You are a senior software engineer and coding agent, " +
			"excel at translating tasks into runnable code and verifiable results.",
	}
)

// ──────────────────────────── 导出函数 ────────────────────────────

// BuildCodeAgentConfig 构建 code 子代理配置（延迟实例化）。
// 对齐 Python: build_code_agent_config(model, card=..., system_prompt=..., tools=..., ...)
//
// 参数通过 SubagentCreateParams 传入，对齐 Python 的具名参数风格。
// adapter 层负责从 map[string]any 解析出 SubagentCreateParams。
func BuildCodeAgentConfig(model *llm.Model, params *hschema.SubagentCreateParams) *hschema.SubAgentConfig {
	language := hprompts.ResolveLanguage(params.Language)

	cfg := hschema.NewSubAgentConfig()

	// AgentCard：用户未提供时使用默认
	// 对齐 Python: card or AgentCard(name="code_agent", description=DEFAULT_CODE_AGENT_DESCRIPTION.get(...))
	cfg.AgentCard = params.Card
	if cfg.AgentCard == nil {
		desc := defaultCodeAgentDescription[language]
		if desc == "" {
			desc = defaultCodeAgentDescription["cn"]
		}
		cfg.AgentCard = agentschema.NewAgentCard(
			agentschema.WithAgentName(CodeAgentFactoryName),
			agentschema.WithAgentDescription(desc),
		)
	}

	// SystemPrompt：用户未提供时使用默认
	// 对齐 Python: system_prompt or DEFAULT_CODE_AGENT_SYSTEM_PROMPT.get(...)
	cfg.SystemPrompt = params.SystemPrompt
	if cfg.SystemPrompt == "" {
		prompt := defaultCodeAgentSystemPrompt[language]
		if prompt == "" {
			prompt = defaultCodeAgentSystemPrompt["cn"]
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

	cfg.FactoryName = CodeAgentFactoryName

	// ⤵️ 9.19-23 CodingMemoryRail 回填：当 EmbeddingConfig 可用时注入
	cfg.FactoryKwargs = nil

	cfg.EnablePlanMode = params.EnablePlanMode

	// RestrictToWorkDir：CodeAgent 默认 false（不限制工作目录，需要读写整个代码库）
	// 对齐 Python: create_code_agent 中不显式传 restrict_to_work_dir，
	// 但 code_agent 语义上需要访问整个代码库，因此默认 false
	// params 为 *bool 指针，nil 表示未设置（使用 CodeAgent 默认 false），非 nil 则使用用户显式指定的值
	if params.RestrictToWorkDir != nil {
		cfg.RestrictToWorkDir = *params.RestrictToWorkDir
	} else {
		cfg.RestrictToWorkDir = false
	}

	return cfg
}

// DefaultCodeAgentSystemPrompt 返回指定语言的默认系统提示词。
// 对齐 Python: DEFAULT_CODE_AGENT_SYSTEM_PROMPT.get(resolved_language, ...)
func DefaultCodeAgentSystemPrompt(language string) string {
	if s, ok := defaultCodeAgentSystemPrompt[language]; ok && s != "" {
		return s
	}
	return defaultCodeAgentSystemPrompt["cn"]
}

// DefaultCodeAgentDescription 返回指定语言的默认描述。
// 对齐 Python: DEFAULT_CODE_AGENT_DESCRIPTION.get(resolved_language, ...)
func DefaultCodeAgentDescription(language string) string {
	if s, ok := defaultCodeAgentDescription[language]; ok && s != "" {
		return s
	}
	return defaultCodeAgentDescription["cn"]
}
