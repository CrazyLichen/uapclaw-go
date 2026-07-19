package subagents

import (
	llm "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	hprompts "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// ──────────────────────────── 常量 ────────────────────────────

// PlanAgentFactoryName plan 子代理工厂名称
// 对齐 Python: PLAN_AGENT_FACTORY_NAME (隐含于 agent_card.name="plan_agent")
const PlanAgentFactoryName = "plan_agent"

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// defaultPlanAgentSystemPrompt 默认系统提示词
	// 对齐 Python: PLAN_AGENT_SYSTEM_PROMPT_CN / PLAN_AGENT_SYSTEM_PROMPT_EN
	// 提示词一比一复刻 Python 原文，不做自行翻译
	defaultPlanAgentSystemPrompt = map[string]string{
		"cn": "你是架构设计与规划专家，基于提供的代码探索背景和用户需求，设计清晰、可执行的实现方案。" +
			"\n\n=== 关键约束：只读模式，禁止任何文件修改 ===" +
			"\n这是纯规划任务。你严格禁止执行以下行为：" +
			"\n- 创建文件（如 Write、touch 或任何形式的新建文件）" +
			"\n- 修改文件（任何编辑操作）" +
			"\n- 删除文件（如 rm）" +
			"\n- 移动/复制文件（如 mv、cp）" +
			"\n- 在任意目录（含 /tmp）创建临时文件" +
			"\n- 使用重定向或管道将内容写入文件（>, >>, |）" +
			"\n- 执行任何会改变系统状态的命令" +
			"\n\n你的职责仅限：探索代码库并设计可执行计划。" +
			"\n\n## 工作流程：" +
			"\n1) 理解需求：聚焦用户目标与约束。" +
			"\n2) 充分探索：识别现有架构、相似实现、关键调用链与约定。" +
			"\n3) 方案设计：给出实现路径，并说明关键取舍。适当遵循已有范式。" +
			"\n4) 细化计划：拆分步骤、依赖关系、执行顺序与潜在风险。" +
			"\n\n如需使用 bash，仅允许只读命令（例如 ls、git status、git log、git diff、find、grep、cat、head、tail）。" +
			"\n严禁使用 bash 执行：mkdir、touch、rm、cp、mv、git add、git commit、npm install、pip install，或任何创建/修改文件的命令。" +
			"\n\n输出要求：在回答末尾必须给出\"Critical Files for Implementation\"，列出 3-5 个最关键文件路径。",
		"en": "You are a software architect and planning specialist. " +
			"Your role is to design a clear, actionable implementation approach " +
			"based on the provided code exploration context and user requirements." +
			"\n\n=== CRITICAL: READ-ONLY MODE - NO FILE MODIFICATIONS ===" +
			"\nThis is a read-only planning task. You are STRICTLY PROHIBITED from:" +
			"\n- Creating new files (no Write, touch, or file creation of any kind)" +
			"\n- Modifying existing files (no edit operations)" +
			"\n- Deleting files (no rm or deletion)" +
			"\n- Moving or copying files (no mv or cp)" +
			"\n- Creating temporary files anywhere, including /tmp" +
			"\n- Using redirect operators or pipes to write to files (>, >>, |)" +
			"\n- Running any command that changes system state" +
			"\n\nYour role is EXCLUSIVELY to explore the codebase and design implementation plans." +
			"\n\n## Your Process:" +
			"\n1) Understand requirements: focus on user goals and constraints." +
			"\n2) Explore thoroughly: identify architecture, conventions, reference implementations, and code paths." +
			"\n3) Design solution: propose implementation approach with architectural trade-offs. " +
			"Follow existing patterns where appropriate." +
			"\n4) Detail the plan: provide steps, sequencing, dependencies, and potential challenges." +
			"\n\nIf using bash, " +
			"use it ONLY for read-only operations (e.g., ls, git status, git log, git diff, find, grep, cat, head, tail)." +
			"\nNEVER use bash for: mkdir, touch, rm, cp, mv, git add, git commit, npm install, pip install, " +
			"or any file creation/modification." +
			"\n\nRequired output: end with a section titled \"Critical Files for Implementation\" and " +
			"list 3-5 most critical file paths.",
	}
	// defaultPlanAgentDescription 默认描述
	// 对齐 Python: PLAN_AGENT_DESC
	defaultPlanAgentDescription = map[string]string{
		"cn": "架构设计专家。基于代码探索结果设计实现方案，生成详细的实现计划。",
		"en": "Architecture design specialist. Designs implementation approaches based on " +
			"code exploration results and produces detailed implementation plans.",
	}
)

// ──────────────────────────── 导出函数 ────────────────────────────

// BuildPlanAgentConfig 构建 plan 子代理配置（延迟实例化）。
// 对齐 Python: build_plan_agent_config(card=..., system_prompt=..., tools=..., ...)
//
// 参数通过 SubagentCreateParams 传入，对齐 Python 的具名参数风格。
// adapter 层负责从 map[string]any 解析出 SubagentCreateParams。
func BuildPlanAgentConfig(model *llm.Model, params *hschema.SubagentCreateParams) *hschema.SubAgentConfig {
	language := hprompts.ResolveLanguage(params.Language)

	cfg := hschema.NewSubAgentConfig()

	// AgentCard：用户未提供时使用默认
	// 对齐 Python: card or AgentCard(name="plan_agent", description=PLAN_AGENT_DESC.get(...))
	cfg.AgentCard = params.Card
	if cfg.AgentCard == nil {
		desc := defaultPlanAgentDescription[language]
		if desc == "" {
			desc = defaultPlanAgentDescription["cn"]
		}
		cfg.AgentCard = agentschema.NewAgentCard(
			agentschema.WithAgentName(PlanAgentFactoryName),
			agentschema.WithAgentDescription(desc),
		)
	}

	// SystemPrompt：用户未提供时使用默认
	// 对齐 Python: system_prompt or (PLAN_AGENT_SYSTEM_PROMPT_CN if cn else PLAN_AGENT_SYSTEM_PROMPT_EN)
	cfg.SystemPrompt = params.SystemPrompt
	if cfg.SystemPrompt == "" {
		prompt := defaultPlanAgentSystemPrompt[language]
		if prompt == "" {
			prompt = defaultPlanAgentSystemPrompt["cn"]
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

	// MaxIterations：用户未提供（0）时默认 25
	// 对齐 Python: max_iterations=25
	cfg.MaxIterations = params.MaxIterations
	if cfg.MaxIterations == 0 {
		cfg.MaxIterations = 25
	}

	// 不设 FactoryName（对齐 Python：plan_agent 不设 factory_name，走通用路径）
	cfg.FactoryName = ""
	cfg.FactoryKwargs = nil
	cfg.EnablePlanMode = params.EnablePlanMode

	// RestrictToWorkDir：PlanAgent 默认 false（区别于 ResearchAgent 的 true）
	// 对齐 Python: restrict_to_work_dir=False
	// params 为 *bool 指针，nil 表示未设置（使用 PlanAgent 默认 false），非 nil 则使用用户显式指定的值
	if params.RestrictToWorkDir != nil {
		cfg.RestrictToWorkDir = *params.RestrictToWorkDir
	} else {
		cfg.RestrictToWorkDir = false
	}

	return cfg
}

// DefaultPlanAgentSystemPrompt 返回指定语言的默认系统提示词。
// 对齐 Python: DEFAULT_PLAN_AGENT_SYSTEM_PROMPT.get(resolved_language, ...)
func DefaultPlanAgentSystemPrompt(language string) string {
	if s, ok := defaultPlanAgentSystemPrompt[language]; ok && s != "" {
		return s
	}
	return defaultPlanAgentSystemPrompt["cn"]
}

// DefaultPlanAgentDescription 返回指定语言的默认描述。
// 对齐 Python: PLAN_AGENT_DESC.get(resolved_language, ...)
func DefaultPlanAgentDescription(language string) string {
	if s, ok := defaultPlanAgentDescription[language]; ok && s != "" {
		return s
	}
	return defaultPlanAgentDescription["cn"]
}
