package reme

import "fmt"

// ──────────────────────────── 结构体 ────────────────────────────

// ReMeRetrievePrompts ReMe 检索管线的提示词配置。
// 对齐 Python ReMeRetrievePrompts(BaseModel)。
type ReMeRetrievePrompts struct {
	// RerankPrompt 重排序提示词
	RerankPrompt string
	// RewritePrompt 改写提示词
	RewritePrompt string
}

// ──────────────────────────── 常量 ────────────────────────────

// 提示词模板 — 一比一复刻 Python 原文，不做自行翻译

// memoryRerankPromptTpl 记忆重排序提示词模板
// 对齐 Python MEMORY_RERANK_PROMPT
const memoryRerankPromptTpl = `You are an expert AI analyst tasked with reranking retrieved experiences based on their relevance to a specific query.

Your task is to analyze the candidates and rank them by relevance, considering:
● DIRECT RELEVANCE: How directly applicable the experience is to the current query
● SITUATION SIMILARITY: How similar the experience context is to the current situation
● ACTIONABILITY: How actionable and specific the experience is
● QUALITY: The overall quality and clarity of the experience

# Current Query
{query}

# Candidate Experiences (Total: {num_candidates})
{candidates}

OUTPUT FORMAT:
Provide a ranked list of candidate indices (0-based) from most relevant to least relevant:
` + "```json" + `
{{
"ranked_indices": [2, 0, 4, 1, 3],
"reasoning": "Brief explanation of ranking rationale"
}}
` + "```" + `

Note: Include ALL candidate indices in the ranking, even if some are less relevant.`

// memoryRewritePromptTpl 记忆改写提示词模板
// 对齐 Python MEMORY_REWRITE_PROMPT
const memoryRewritePromptTpl = `You are an expert AI assistant tasked with rewriting and reorganizing context content to make it more relevant and actionable for the current task.

Your task is to take the original context (containing multiple experiences) and rewrite it as a cohesive, task-specific guidance that directly addresses the current situation.

REWRITING GUIDELINES:
● RELEVANCE FOCUS: Emphasize the most relevant aspects of each experience. Prioritize the most relevant experiences. Use clear, direct language.
● ACTIONABLE INSIGHTS: Extract specific, actionable guidance. Make the context immediately actionable
● COHERENT NARRATIVE: Create a flowing narrative rather than disconnected tips
● SITUATIONAL AWARENESS: Adapt the guidance to the current situation

# Current Task/Query
{current_query}

# Original Context Content (Multiple Experiences)
{original_context}

OUTPUT FORMAT:
Provide the rewritten context:
` + "```json" + `
{{
"rewritten_context": "A cohesive, task-specific context message that reorganizes and adapts the original experiences for the current task. This should be written as a unified guidance rather than separate experience items.",
}}
` + "```" + `

Guidelines:
- Rewrite as a unified, flowing guidance
- Adapt terminology and examples to match the current task domain
- Consolidate overlapping insights into coherent recommendations
- Prioritize experiences most relevant to the current situation
- Make the guidance feel custom-written for this specific task`

// ──────────────────────────── 全局变量 ────────────────────────────

// ReMeRetrieveDefaultPrompts 默认提示词实例。
// 对齐 Python ReMeRetrievePrompts() 无参构造的默认值。
var ReMeRetrieveDefaultPrompts = ReMeRetrievePrompts{
	RerankPrompt:  memoryRerankPromptTpl,
	RewritePrompt: memoryRewritePromptTpl,
}

// ──────────────────────────── 导出函数 ────────────────────────────

// FormatRerankPrompt 格式化重排序提示词。
// 对齐 Python RerankMemoryOp 中 prompt.format(query=..., num_candidates=..., candidates=...)
func FormatRerankPrompt(prompt, query string, numCandidates int, candidates string) string {
	result := prompt
	result = replacePlaceholders(result, "query", query)
	result = replacePlaceholders(result, "num_candidates", fmt.Sprintf("%d", numCandidates))
	result = replacePlaceholders(result, "candidates", candidates)
	return result
}

// FormatRewritePrompt 格式化改写提示词。
// 对齐 Python RewriteMemoryOp 中 prompt.format(current_query=..., original_context=...)
func FormatRewritePrompt(prompt, currentQuery, originalContext string) string {
	result := prompt
	result = replacePlaceholders(result, "current_query", currentQuery)
	result = replacePlaceholders(result, "original_context", originalContext)
	return result
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// replacePlaceholders 替换模板中的 {key} 占位符
func replacePlaceholders(tpl, key, value string) string {
	// 简单替换，不使用 regexp 避免误匹配 JSON 中的 {{ }}
	// 只替换 {key} 形式的占位符，保留 {{ 和 }}
	old := "{" + key + "}"
	result := ""
	remaining := tpl
	for {
		idx := indexOf(remaining, old)
		if idx < 0 {
			result += remaining
			break
		}
		result += remaining[:idx] + value
		remaining = remaining[idx+len(old):]
	}
	return result
}

// indexOf 返回子串在字符串中首次出现的位置，未找到返回 -1
func indexOf(s, sub string) int {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
