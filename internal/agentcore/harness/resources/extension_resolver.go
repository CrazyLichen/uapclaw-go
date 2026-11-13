package resources

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
)

// 本文件对应 Python openjiuwen/harness/resources/extension_resolver.py 的
// persona 渲染部分：把模板的 prompt_sections 渲染成一段纯文本系统提示词。
//
// 用途：无法挂载 PromptSection 对象的 harness provider（Claude Code、Codex、
// DSH）需要把 persona 当纯文本交给外部 CLI。
//
// ⏵ 15.15a：本文件只实现渲染路径。resolve_agent_template_parts /
// resolve_plugin_parts（ExtensionParts 装配、plugin 能力覆盖、资源引用解析）
// 属 15.15a。

// ──────────────────────────── 全局变量 ────────────────────────────

// templatePlaceholderPattern 模板占位符：{{ name }}，name 为标识符。
// Python: extension_resolver.py:38 _TEMPLATE_PATTERN
var templatePlaceholderPattern = regexp.MustCompile(`\{\{\s*([a-zA-Z_][a-zA-Z0-9_]*)\s*\}\}`)

// ──────────────────────────── 导出函数 ────────────────────────────

// RenderAgentTemplateSystemPrompt 把模板的 persona 段落渲染成一段系统提示词。
// Python: extension_resolver.py:241-256 render_agent_template_system_prompt
//
// 段落按 Priority 升序排序，用空行连接，空段落跳过。
func RenderAgentTemplateSystemPrompt(spec hschema.AgentTemplateSpec, language string) string {
	sections := make([]hschema.PromptSectionSpec, len(spec.PromptSections))
	copy(sections, spec.PromptSections)
	// Python: sorted(spec.prompt_sections, key=lambda item: item.priority)
	// 稳定排序保证同 priority 时保持声明顺序（与 Python sorted 一致）
	sort.SliceStable(sections, func(i, j int) bool {
		return sections[i].Priority < sections[j].Priority
	})

	rendered := make([]string, 0, len(sections))
	for _, section := range sections {
		text := renderPromptSectionText(section, language)
		if text != "" {
			rendered = append(rendered, text)
		}
	}
	return strings.Join(rendered, "\n\n")
}

// RenderPromptContent 渲染单段本地化提示词正文。
// Python: extension_resolver.py:259-268 render_prompt_content
//
// 语言选择四级回退（language → en → cn → 任一），再替换 {{ name }} 占位符；
// 未在 params 中的占位符原样保留。
func RenderPromptContent(content map[string]string, language string, params map[string]any) string {
	return renderTemplate(selectContent(content, language), params)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// renderPromptSectionText 渲染单段 persona 正文。
// Python: extension_resolver.py:236-238 _render_prompt_section_text
//
// render_params 会先与 BuildContext 派生的值合并（language / workspace），
// 用户显式给的键覆盖派生值（Python: values.update(dict(render_params or {}))）。
func renderPromptSectionText(section hschema.PromptSectionSpec, language string) string {
	params := map[string]any{
		"language":  defaultLanguage(language),
		"workspace": "",
	}
	for key, value := range section.RenderParams {
		params[key] = value
	}
	return renderTemplate(selectContent(section.Content, language), params)
}

// selectContent 按语言选正文。
// Python: extension_resolver.py:275-285 _select_content
//
// 四级回退：language → en → cn → 任一（map 遍历无序，Go 侧取字典序最小的键
// 以保证输出确定；Python 用 next(iter(...)) 取插入序第一个，两者都只在
// 「三种语言都没有」时才有差异，属极端兜底）。
func selectContent(content map[string]string, language string) string {
	if len(content) == 0 {
		return ""
	}
	if text, ok := content[language]; ok {
		return text
	}
	if text, ok := content["en"]; ok {
		return text
	}
	if text, ok := content["cn"]; ok {
		return text
	}
	keys := make([]string, 0, len(content))
	for key := range content {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return content[keys[0]]
}

// renderTemplate 替换 {{ name }} 占位符；未命中时保留原文。
// Python: extension_resolver.py:296-303 _render_template
func renderTemplate(text string, params map[string]any) string {
	if text == "" {
		return ""
	}
	return templatePlaceholderPattern.ReplaceAllStringFunc(text, func(match string) string {
		sub := templatePlaceholderPattern.FindStringSubmatch(match)
		if len(sub) < 2 {
			return match
		}
		value, ok := params[sub[1]]
		if !ok {
			return match
		}
		if str, isString := value.(string); isString {
			return str
		}
		// Python: str(params[key]) —— 非字符串值走字符串化
		return fmt.Sprint(value)
	})
}

// defaultLanguage 空语言回退 cn。
// Python: extension_resolver.py:289 `ctx.language or "cn"`
func defaultLanguage(language string) string {
	if language == "" {
		return "cn"
	}
	return language
}
