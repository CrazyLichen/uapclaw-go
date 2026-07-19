package subagents

import (
	llm "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	hprompts "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	bm "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/tools/browser_move"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// BrowserAgentFactoryName browser 子代理工厂名称
// 对齐 Python: BROWSER_AGENT_FACTORY_NAME
const BrowserAgentFactoryName = "browser_agent"

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// defaultBrowserAgentSystemPrompt 默认系统提示词（中/英双语）
	// 对齐 Python: DEFAULT_BROWSER_AGENT_SYSTEM_PROMPT
	// 提示词逐字符复制 Python 原文
	defaultBrowserAgentSystemPrompt = map[string]string{
		"cn": "你是浏览器自动化代理，负责直接执行网页任务。" +
			"请在当前代理层面规划和决策，并使用 Playwright 浏览器工具以及已批准的运行时辅助工具" +
			"完成导航、点击、输入、选择、检查和信息提取。" +
			"操作应保持目标明确，避免不必要的页面快照。" +
			"在使用大范围页面快照、全页面扫描或通用 DOM 抓取前，优先选择与任务匹配的最小紧凑探测工具。" +
			"需要按钮、链接、输入框、表单、导航控件、登录控件、分页控件、菜单或其他可见交互元素时，" +
			"优先使用 browser_probe_interactives；如果只是查找页面级控件，max_items 通常保持在 20-30 左右，" +
			"除非任务明确需要更大的元素清单。" +
			"在商品页、市场页、搜索结果页、目录页、文章列表页，或任何包含重复可见卡片/列表项的页面上，" +
			"应先调用 browser_probe_cards，再进行大范围提取。" +
			"使用 browser_probe_cards 识别商品卡片、结果卡片、图书卡片、文章卡片、列表行、" +
			"标题/价格/评分/评论数/库存字段、主链接、可见按钮、边界框、selector_hint 和重复结构特征。" +
			"对于商品、列表或条目数据任务，优先使用 browser_probe_cards；只有在还需要卡片外的页面级导航、" +
			"筛选器、表单或控件时，再调用 browser_probe_interactives。" +
			"相关时优先使用紧凑探测返回的 selector_hint。" +
			"仅在紧凑探测不足、需要无障碍结构，或 Playwright MCP 操作需要精确元素引用时使用 browser_snapshot。" +
			"仅在已经知道精确 selector/计算逻辑，或紧凑探测和 browser_snapshot 都不足时，" +
			"使用 browser_run_code_unsafe 或 browser_run_code。" +
			"除非所有紧凑方式都失败，不要使用 browser_run_code_unsafe 或 browser_run_code 转储整个 document body。" +
			"browser_custom_action 只用于基础浏览器工具难以表达的确定性辅助动作。" +
			"不要假设存在嵌套 browser worker 或 browser_run_task 包装器。" +
			"避免重复动作，保持会话连续性；只有当网页上的具体证据证明任务已完成时，才声明完成。" +
			"请如实、简洁地汇报结果。",
		"en": "You are a browser automation agent responsible for executing web tasks directly. " +
			"Plan and decide at this agent level, then use Playwright browser tools and approved runtime " +
			"helper tools to navigate, click, type, select, inspect, and extract information. " +
			"Keep actions targeted and avoid unnecessary page snapshots. " +
			"Before broad page snapshots, full-body scans, or generic DOM scraping, choose the smallest " +
			"compact probe that matches the task. " +
			"Use browser_probe_interactives for buttons, links, inputs, forms, navigation controls, " +
			"login controls, pagination controls, menus, and other visible interactive elements. " +
			"When using browser_probe_interactives only for page-level controls, prefer max_items around " +
			"20-30 unless the task explicitly requires a larger inventory. " +
			"On product pages, marketplace pages, search-result pages, catalog pages, article-list pages, " +
			"or any page with repeated visible cards/listings, call browser_probe_cards before broad " +
			"extraction. " +
			"Use browser_probe_cards to identify compact repeated structures such as product cards, result " +
			"cards, book cards, article cards, listing rows, title/price/rating/review/availability fields, " +
			"primary links, visible buttons, bounding boxes, selector hints, and recurring structure " +
			"signatures. " +
			"For product/listing/item-data tasks, prefer browser_probe_cards first; call " +
			"browser_probe_interactives only if you also need page-level navigation, filters, forms, or " +
			"controls outside the cards. " +
			"Prefer selector_hint values from compact probes when they are relevant. " +
			"Use browser_snapshot only when compact probes are insufficient, when accessibility structure " +
			"is needed, or when exact element references are required by a Playwright MCP action. " +
			"Use browser_run_code_unsafe or browser_run_code only when you already know the exact " +
			"selector/computation, or when the compact probes and browser_snapshot are insufficient. " +
			"Do not use browser_run_code_unsafe or browser_run_code to dump the entire document body unless " +
			"all compact approaches fail. " +
			"Use browser_custom_action only for deterministic helper actions that are awkward to express with " +
			"the primitive browser tools. " +
			"Do not assume a nested browser worker or browser_run_task wrapper exists. " +
			"Avoid redundant actions, preserve session continuity, and only claim completion when the " +
			"requested browser outcome is actually evidenced.",
	}
	// defaultBrowserAgentDescription 默认描述（中/英双语）
	// 对齐 Python: DEFAULT_BROWSER_AGENT_DESCRIPTION
	// 描述逐字符复制 Python 原文
	defaultBrowserAgentDescription = map[string]string{
		"cn": "专用浏览器子代理，直接使用 Playwright MCP 工具执行网页任务。",
		"en": "Dedicated browser subagent that directly controls the browser with Playwright MCP tools.",
	}
)

// ──────────────────────────── 导出函数 ────────────────────────────

// BuildBrowserAgentConfig 构建 browser 子代理配置（延迟实例化）。
// 对齐 Python: build_browser_agent_config(model, card=..., system_prompt=..., ...)
//
// 参数通过 SubagentCreateParams 传入，对齐 Python 的具名参数风格。
// adapter 层负责从 map[string]any 解析出 SubagentCreateParams。
func BuildBrowserAgentConfig(model *llm.Model, params *hschema.SubagentCreateParams) *hschema.SubAgentConfig {
	language := hprompts.ResolveLanguage(params.Language)

	cfg := hschema.NewSubAgentConfig()

	// AgentCard：用户未提供时使用默认
	cfg.AgentCard = params.Card
	if cfg.AgentCard == nil {
		desc := defaultBrowserAgentDescription[language]
		if desc == "" {
			desc = defaultBrowserAgentDescription["cn"]
		}
		cfg.AgentCard = agentschema.NewAgentCard(
			agentschema.WithAgentName(BrowserAgentFactoryName),
			agentschema.WithAgentDescription(desc),
		)
	}

	// SystemPrompt：用户未提供时使用默认
	cfg.SystemPrompt = params.SystemPrompt
	if cfg.SystemPrompt == "" {
		prompt := defaultBrowserAgentSystemPrompt[language]
		if prompt == "" {
			prompt = defaultBrowserAgentSystemPrompt["cn"]
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

	cfg.FactoryName = BrowserAgentFactoryName

	// FactoryKwargs：包含 settings key，指向 ResolveRuntimeSettings 返回值
	// 对齐 Python: factory_kwargs={"settings": resolved_settings}
	resolvedSettings := bm.ResolveRuntimeSettings(model, nil)
	cfg.FactoryKwargs = map[string]any{"settings": resolvedSettings}

	cfg.EnablePlanMode = params.EnablePlanMode
	// RestrictToWorkDir：params 为 *bool 指针，nil 表示未设置（保持 NewSubAgentConfig 默认 true），
	// 非 nil 则使用用户显式指定的值
	if params.RestrictToWorkDir != nil {
		cfg.RestrictToWorkDir = *params.RestrictToWorkDir
	}

	return cfg
}

// DefaultBrowserAgentSystemPrompt 返回指定语言的默认系统提示词。
// 对齐 Python: DEFAULT_BROWSER_AGENT_SYSTEM_PROMPT.get(resolved_language, ...)
func DefaultBrowserAgentSystemPrompt(language string) string {
	if s, ok := defaultBrowserAgentSystemPrompt[language]; ok && s != "" {
		return s
	}
	return defaultBrowserAgentSystemPrompt["cn"]
}

// DefaultBrowserAgentDescription 返回指定语言的默认描述。
// 对齐 Python: DEFAULT_BROWSER_AGENT_DESCRIPTION.get(resolved_language, ...)
func DefaultBrowserAgentDescription(language string) string {
	if s, ok := defaultBrowserAgentDescription[language]; ok && s != "" {
		return s
	}
	return defaultBrowserAgentDescription["cn"]
}
