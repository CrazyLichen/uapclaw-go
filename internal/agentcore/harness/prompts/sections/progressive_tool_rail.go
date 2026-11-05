package sections

import (
	"sort"
	"strconv"
	"strings"

	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// maxDeferredToolSummaryChars 目录摘要最大字符数
	maxDeferredToolSummaryChars = 120
	// deferredToolSummaryEllipsis 摘要截断后缀
	deferredToolSummaryEllipsis = "…"

	// progressiveToolRulesHeaderCN 规则节标题（中文）
	progressiveToolRulesHeaderCN = "# 渐进式工具使用规则\n"
	// progressiveToolRulesHeaderEN 规则节标题（英文）
	progressiveToolRulesHeaderEN = "# Progressive Tool Usage Rules\n"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// progressiveToolRulesBodyCN 规则正文（中文，严格对齐上游 progressive_tool_rail.py）
var progressiveToolRulesBodyCN = "当前启用了渐进式工具加载。工具分为两类：\n" +
	"- direct 工具：可以直接调用。\n" +
	"- deferred 工具：已注册且可用，但需要先通过 `tool_search` 搜索；" +
	"搜索结果会提供完整 schema，但结果工具不会加入顶层 `tools`；" +
	"下一轮必须通过固定的 `tool_call` 执行。\n\n" +
	"当前 session 启动时的 deferred 工具初始目录列在本节末尾。" +
	"后续新增、修改和删除的工具不会改写这条稳定系统提示词，而是通过新的 system attachment 提供；" +
	"如果多个 attachment 都涉及工具目录，应按顺序应用，并以最新内容为准。\n" +
	"## 动态工具状态优先级\n" +
	"- 当前 session 启动时的 deferred 工具目录只是初始快照，不代表永久可用。\n" +
	"- `deferred 工具目录更新` attachment 表示本次请求的最新工具状态，" +
	"即使它在消息序列中出现在当前 user 消息之后，也必须在回答前应用。\n" +
	"- 最新目录覆盖初始目录、历史消息、历史 tool_search 结果、历史 toolResult，" +
	"以及 task_tool 或子代理描述中的工具名称。\n" +
	"- 工具被标记为删除后，当前不可用；不得声称仍支持，不得复用旧结果，" +
	"也不得通过 `tool_call`、`task_tool` 或子代理间接调用。\n" +
	"- 如果工具仍在当前注册表中且没有变化，可以继续复用历史结果；" +
	"如果工具描述或 schema 已修改，应重新搜索获取最新结果。\n\n" +
	"## tool_search 使用规则\n" +
	"1. 先判断用户真正想完成的操作。\n" +
	"2. 如果 direct 工具明确支持该操作，可以直接调用。\n" +
	"3. 如果没有匹配的 direct 工具，必须调用 `tool_search`，不能猜测 deferred 工具名称。\n" +
	"4. 搜索时使用用户意图和所需能力描述作为 `query`。\n" +
	"5. 搜索结果包含工具的完整 `parameters` schema；结果工具不会加入顶层 `tools`，" +
	"下一轮必须调用固定的 `tool_call`，并把搜索结果中的准确工具名称放入 `name`，" +
	"把符合 schema 的参数放入 `args`。不要直接调用搜索结果工具名称。\n" +
	"6. 不要使用 `bash` 或其他 direct 工具去执行搜索到的工具。\n" +
	"7. 同一用户意图不要重复调用相同或相近的搜索；若返回结果不适用，不要猜测工具名称，" +
	"必要时调用 `ask_user`，否则说明当前没有合适工具。\n"

// progressiveToolRulesBodyEN 规则正文（英文，严格对齐上游 progressive_tool_rail.py）
var progressiveToolRulesBodyEN = "Progressive tool loading is enabled. Tools are divided into two categories:\n" +
	"- Direct tools: can be called directly.\n" +
	"- Deferred tools: registered and available, but must first be found through `tool_search`. " +
	"The search result provides the complete schema, but result tools are not added to the top-level `tools`; " +
	"execute them through the fixed `tool_call` tool in the next turn.\n\n" +
	"The initial deferred-tool directory for this session is listed at the end of this stable section. " +
	"Later additions, updates, and removals do not rewrite this system prompt; they are delivered as new system " +
	"attachments. Apply directory updates in order and use the latest content. \n" +
	"## Dynamic Tool State Priority\n" +
	"- The deferred-tool directory at session startup is only an initial snapshot; " +
	"it does not mean those tools remain available forever.\n" +
	"- A `deferred-tool directory update` attachment is the latest tool state for this request. " +
	"Even if it appears after the current user message in the serialized message sequence, " +
	"apply it before answering.\n" +
	"- The latest directory overrides the initial directory, historical messages, " +
	"historical tool_search results, historical toolResults, and tool names described " +
	"by task_tool or subagents.\n" +
	"- Once a tool is marked as removed, it is unavailable. Do not claim that it is " +
	"supported, reuse its old result, or invoke it through `tool_call`, `task_tool`, " +
	"or a subagent.\n" +
	"- A historical result may be reused only while the tool remains registered and " +
	"unchanged; if its description or schema changed, search again.\n\n" +
	"## tool_search usage rules\n" +
	"1. First identify the actual operation the user wants to complete.\n" +
	"2. If a direct tool clearly supports the operation, call it directly.\n" +
	"3. If no direct tool matches, you must call `tool_search`; do not guess deferred tool names.\n" +
	"4. Use the user's intent and required capability as the `query`.\n" +
	"5. `tool_search` uses BM25 to match tool names, descriptions, and parameter information.\n" +
	"6. Search results contain the complete `parameters` schema. Result tools are not " +
	"added to the top-level `tools`; in the next turn call the fixed `tool_call` " +
	"with the exact result name in `name` and schema-compatible arguments in `args`. " +
	"Do not call the result tool by its own name.\n" +
	"7. Do not use `bash` or another direct tool to execute a search result.\n" +
	"8. Do not repeat the same or a similar search for the same user intent. " +
	"If the results are not suitable, " +
	"do not guess a tool name; call `ask_user` when necessary, otherwise explain " +
	"that no suitable tool is available.\n"

// ──────────────────────────── 导出函数 ────────────────────────────

// BuildProgressiveToolRulesSection 构建渐进式工具规则节（Priority 75）。
//
// 稳定节：规则正文 + 本 session 的初始 deferred 目录；后续变化走附件。
//
// Python: build_multilingual_progressive_tool_rules_section
func BuildProgressiveToolRulesSection(deferredToolDescriptions map[string]string) saprompt.PromptSection {
	return saprompt.PromptSection{
		Name: SectionProgressiveToolRules,
		Content: map[string]string{
			"cn": buildProgressiveToolRulesPrompt("cn", deferredToolDescriptions),
			"en": buildProgressiveToolRulesPrompt("en", deferredToolDescriptions),
		},
		Priority: 75,
	}
}

// RenderDeferredToolCatalogSnapshot 渲染全量 deferred 目录附件。
//
// Python: render_deferred_tool_catalog_snapshot
func RenderDeferredToolCatalogSnapshot(toolDescriptions map[string]string, language string, version int) string {
	lang := normalizeSectionLanguage(language)
	descriptionText := renderDeferredToolDescriptions(toolDescriptions, lang)
	if lang == "en" {
		return "## Deferred tools available through tool_search\n\n" +
			"Catalog version: " + itoa(version) + "\n\n" + descriptionText
	}
	return "## 当前可通过 tool_search 搜索的 deferred 工具\n\n" +
		"目录版本：" + itoa(version) + "\n\n" + descriptionText
}

// RenderDeferredToolCatalogDelta 渲染增量 deferred 目录附件。
//
// Python: render_deferred_tool_catalog_delta
func RenderDeferredToolCatalogDelta(
	added map[string]string,
	updated map[string]string,
	removed []string,
	language string,
	version int,
) string {
	lang := normalizeSectionLanguage(language)
	sortedRemoved := append([]string(nil), removed...)
	sort.Strings(sortedRemoved)

	if lang == "en" {
		sections := make([]string, 0, 3)
		if len(added) > 0 {
			sections = append(sections,
				"### Added (searchable through tool_search)\n\n"+renderDeferredToolDescriptions(added, lang))
		}
		if len(updated) > 0 {
			sections = append(sections,
				"### Updated (old results invalid; search again)\n\n"+renderDeferredToolDescriptions(updated, lang))
		}
		if len(sortedRemoved) > 0 {
			sections = append(sections, "### Removed (unavailable now)\n\n"+renderRemovedTools(sortedRemoved))
		}
		body := "No changes."
		if len(sections) > 0 {
			body = strings.Join(sections, "\n\n")
		}
		return "## Deferred-tool directory update (effective immediately)\n\n" +
			"Catalog version: " + itoa(version) + "\n\n" +
			"Only changed tools are listed; omitted tools are unchanged. This update supersedes the " +
			"old directory and historical search results.\n\n" + body
	}

	sections := make([]string, 0, 3)
	if len(added) > 0 {
		sections = append(sections,
			"### 新增（可通过 tool_search 搜索）\n\n"+renderDeferredToolDescriptions(added, lang))
	}
	if len(updated) > 0 {
		sections = append(sections,
			"### 修改（旧结果失效，需重新搜索）\n\n"+renderDeferredToolDescriptions(updated, lang))
	}
	if len(sortedRemoved) > 0 {
		sections = append(sections, "### 删除（当前不可用）\n\n"+renderRemovedTools(sortedRemoved))
	}
	body := "本次没有变化。"
	if len(sections) > 0 {
		body = strings.Join(sections, "\n\n")
	}
	return "## deferred 工具目录更新（立即生效）\n\n" +
		"目录版本：" + itoa(version) + "\n\n" +
		"仅列出本次变化；未列出的工具保持不变。本更新覆盖旧目录和历史搜索结果。\n\n" + body
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// buildProgressiveToolRulesPrompt 拼接规则正文与初始目录。
//
// Python: build_progressive_tool_rules_prompt
func buildProgressiveToolRulesPrompt(language string, deferredToolDescriptions map[string]string) string {
	lang := normalizeSectionLanguage(language)
	prompt := progressiveToolRulesHeaderCN + progressiveToolRulesBodyCN
	if lang == "en" {
		prompt = progressiveToolRulesHeaderEN + progressiveToolRulesBodyEN
		prompt += "## Deferred tools available through tool_search (initial session directory)\n\n" +
			renderDeferredToolDescriptions(deferredToolDescriptions, lang)
		return prompt
	}
	prompt += "## 当前可通过 tool_search 搜索的 deferred 工具（session 初始目录）\n\n" +
		renderDeferredToolDescriptions(deferredToolDescriptions, lang)
	return prompt
}

// renderDeferredToolDescriptions 渲染目录摘要列表（按名字排序）。
//
// Python: _render_deferred_tool_descriptions
func renderDeferredToolDescriptions(toolDescriptions map[string]string, language string) string {
	if len(toolDescriptions) == 0 {
		if language == "en" {
			return "(No deferred tools are currently available through tool_search.)"
		}
		return "（当前没有可通过 tool_search 搜索的 deferred 工具。）"
	}
	names := make([]string, 0, len(toolDescriptions))
	for name := range toolDescriptions {
		names = append(names, name)
	}
	sort.Strings(names)

	lines := make([]string, 0, len(names))
	for _, name := range names {
		if summary := compactToolDescription(toolDescriptions[name]); summary != "" {
			lines = append(lines, "- **"+name+"**: "+summary)
			continue
		}
		lines = append(lines, "- **"+name+"**")
	}
	return strings.Join(lines, "\n")
}

// renderRemovedTools 渲染删除段（仅名称）。
func renderRemovedTools(removed []string) string {
	lines := make([]string, 0, len(removed))
	for _, name := range removed {
		lines = append(lines, "- **"+name+"**")
	}
	return strings.Join(lines, "\n")
}

// compactToolDescription 把描述压成一句（最长 120 字符，优先截到句末标点）。
//
// Python: _compact_tool_description
func compactToolDescription(description string) string {
	compact := strings.Join(strings.Fields(description), " ")
	runes := []rune(compact)
	if len(runes) <= maxDeferredToolSummaryChars {
		return compact
	}
	for index, char := range runes[:maxDeferredToolSummaryChars] {
		if index > 0 && strings.ContainsRune("。.！!？?", char) {
			return string(runes[:index+1])
		}
	}
	return string(runes[:maxDeferredToolSummaryChars-1]) + deferredToolSummaryEllipsis
}

// normalizeSectionLanguage 归一化语言标识（非 en 一律 cn）。
func normalizeSectionLanguage(language string) string {
	if language == "en" {
		return "en"
	}
	return "cn"
}

// itoa 整数转字符串。
func itoa(value int) string {
	return strconv.Itoa(value)
}
