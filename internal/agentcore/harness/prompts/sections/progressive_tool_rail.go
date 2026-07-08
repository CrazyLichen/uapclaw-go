package sections

import (
	"fmt"
	"strings"

	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	progressiveToolNavigationHeaderCN = "## 工具导航\n" +
		"以下条目用于帮助你理解当前 session 下的工具生态。\n" +
		"请注意：这里展示的是\"工具地图\"，不是\"全部可立即调用的工具清单\"。\n" +
		"只有在当前 session 中显式调用 `load_tools` 后，目标工具才会进入可调用状态。\n"

	progressiveToolNavigationHeaderEN = "## Tool Navigation\n" +
		"The entries below help you understand the tool ecosystem available " +
		"in the current session.\n" +
		"Treat this section as a tool map, not as a full list of immediately " +
		"callable tools.\n" +
		"A tool becomes callable only after `load_tools` has been explicitly " +
		"called for it in the current session.\n"

	progressiveToolNavigationEmptyCN = "- （当前无可展示的导航条目）"
	progressiveToolNavigationEmptyEN = "- (no navigation entries available)"

	progressiveToolRulesHeaderCN = "## 渐进式工具使用规则\n"
	progressiveToolRulesHeaderEN = "## Progressive Tool Usage Rules\n"

	progressiveToolRulesBodyCN = "你正在一个渐进式工具环境中工作。\n" +
		"请严格遵循以下规则：\n" +
		"1. 当你不确定该使用哪个工具时，先调用 `search_tools` 查找候选工具。\n" +
		"2. 如需查看更多细节，可直接提高 `search_tools` 的 `detail_level`" +
		"（2=参数摘要，3=完整参数）。\n" +
		"3. 在导航区或搜索结果中看到某个工具，并不意味着它已经可调用。\n" +
		"4. 真实工具只有在当前 session 中显式调用 `load_tools` 后才可调用。\n" +
		"5. 一旦你已经通过 `search_tools` 找到要使用的目标工具，下一步应立即调用 " +
		"`load_tools`，而不是继续只用文字描述计划。\n" +
		"6. 在所需工具尚未加载前，不要声称你将要检查文件、读取目录、解析文档、" +
		"生成表格或执行任何依赖这些工具的动作；应先加载工具，再执行。\n" +
		"7. 如果任务涉及文件检查、PDF 处理、XLSX 生成、目录浏览或数据处理，" +
		"你应尽快从搜索结果中选择合适工具并调用 `load_tools`，随后立刻使用真实工具执行。\n" +
		"8. 不要停留在\"下一步我将……\"这类自然语言计划上；若已有足够信息选择工具，" +
		"就直接进入 `load_tools` 和真实工具调用。\n" +
		"9. 工作顺序应尽量保持为：先导航，再搜索，必要时看更详细结果，再加载，最后执行。\n"

	progressiveToolRulesBodyEN = "You are operating in a progressive tool environment.\n" +
		"Follow these rules strictly:\n" +
		"1. If you are unsure which tool to use, call `search_tools` first.\n" +
		"2. If you need more detail, increase `search_tools.detail_level` directly " +
		"(2=parameter summary, 3=full parameters).\n" +
		"3. Seeing a tool in navigation or search results does NOT make it callable.\n" +
		"4. A real tool becomes callable only after `load_tools` has been " +
		"explicitly called for it in the current session.\n" +
		"5. Once `search_tools` has identified the tools you want, the next step " +
		"should be to call `load_tools` immediately, rather than continuing with " +
		"natural-language planning only.\n" +
		"6. Do not claim that you will inspect files, browse directories, parse " +
		"documents, generate spreadsheets, or perform any other tool-dependent " +
		"action before the required tools have been loaded.\n" +
		"7. If the task involves file inspection, PDF processing, XLSX generation, " +
		"directory browsing, or data processing, select suitable tools from search " +
		"results, call `load_tools`, and then use the real tools right away.\n" +
		"8. Do not stop at statements like 'next I will ...'. If you already have " +
		"enough information to choose tools, move directly to `load_tools` and then " +
		"to real tool execution.\n" +
		"9. Prefer this sequence: navigate first, search second, inspect richer " +
		"results when needed, load third, execute last.\n"
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	progressiveToolRulesContentCN = progressiveToolRulesHeaderCN + progressiveToolRulesBodyCN
	progressiveToolRulesContentEN = progressiveToolRulesHeaderEN + progressiveToolRulesBodyEN
)

// ──────────────────────────── 导出函数 ────────────────────────────

// BuildNavigationSection 构建工具导航节（Priority 70）
func BuildNavigationSection(entries []string, lang string) saprompt.PromptSection {
	var header string
	if lang == "en" {
		header = progressiveToolNavigationHeaderEN
	} else {
		header = progressiveToolNavigationHeaderCN
	}

	// 过滤空条目
	items := make([]string, 0, len(entries))
	for _, e := range entries {
		if e != "" {
			items = append(items, e)
		}
	}

	var content string
	if len(items) == 0 {
		var emptyText string
		if lang == "en" {
			emptyText = progressiveToolNavigationEmptyEN
		} else {
			emptyText = progressiveToolNavigationEmptyCN
		}
		content = header + "\n" + emptyText
	} else {
		content = header + "\n" + strings.Join(items, "\n")
	}

	return saprompt.PromptSection{
		Name:     SectionToolNavigation,
		Content:  map[string]string{lang: content},
		Priority: 70,
	}
}

// BuildProgressiveToolRulesSection 构建渐进式工具规则节（Priority 75）
func BuildProgressiveToolRulesSection(lang string) saprompt.PromptSection {
	var content string
	if lang == "en" {
		content = progressiveToolRulesContentEN
	} else {
		content = progressiveToolRulesContentCN
	}

	return saprompt.PromptSection{
		Name:     SectionProgressiveToolRules,
		Content:  map[string]string{lang: content},
		Priority: 75,
	}
}

// BuildNavigationEntry 构建单个导航条目
func BuildNavigationEntry(name string, group string, status string, summary string, lang string) string {
	if lang == "en" {
		return fmt.Sprintf("- %s [%s, %s]: %s", name, group, status, summary)
	}
	return fmt.Sprintf("- %s [%s, %s]：%s", name, group, status, summary)
}
