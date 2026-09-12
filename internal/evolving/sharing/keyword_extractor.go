package sharing

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/evolving/checkpointing"
	"github.com/uapclaw/uapclaw-go/internal/evolving/optimizer/llm_resilience"
)

// ──────────────────────────── 结构体 ────────────────────────────

// KeywordExtractor 关键词提取器，桥接优化器输出和共享检索路径。
//
// 两条职责：
//  1. 上传路径 – 从优化器输出（EvolutionPatch 或原始 dict）中解析
//     keywords/summary，无需额外网络调用。
//  2. 下载路径 – 对对话摘录（用户查询、工具执行结果等）执行一次小型
//     LLM 调用来获取 QueryKeywords，用于跨用户经验检索。
//     LLM 失败时返回空关键词，确保不阻塞调用方。
//
// Python: openjiuwen/agent_evolving/sharing/keyword_extractor.py KeywordExtractor
type KeywordExtractor struct {
	// llm LLM 模型实例（可选，nil 时跳过 LLM 调用）
	llm *llm.Model
	// model 模型名称
	model string
	// language 提示词语言，"cn" 或 "en"，默认 "cn"
	language string
	// policy LLM 调用策略
	policy llm_resilience.LLMInvokePolicy
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// logComponent 日志组件常量
const logComponent = logger.ComponentAgentCore

// QUERY_KEYWORDS_LLM_POLICY 关键词提取 LLM 调用策略。
//
// Python: QUERY_KEYWORDS_LLM_POLICY = LLMInvokePolicy(
//
//	attempt_timeout_secs=1500,
//	total_budget_secs=4000,
//	max_attempts=5,
//
// )
var QUERY_KEYWORDS_LLM_POLICY = llm_resilience.LLMInvokePolicy{
	AttemptTimeoutSecs: 1500,
	TotalBudgetSecs:    4000,
	MaxAttempts:        5,
}

// queryKeywordsPromptCN 中文检索关键词提取提示词。
//
// 一比一复刻 Python: _QUERY_KEYWORDS_PROMPT_CN
const queryKeywordsPromptCN = `你是一个检索关键词抽取器。下面是从对话中提取的关键信息片段，包含用户查询、工具执行结果（特别是出错的）等，请提取用于"跨用户经验检索"的关键词。

## 输入
{excerpt}

## 当前 Skill 提示（可能为空）
{skill_hint}

## 输出要求
- 关键词 10-20 个，覆盖问题的核心概念，避免主语/口头语
- 优先输出英文标识符 / 报错关键字；同时给出对应中文术语，提升召回
- 同时给出 <=40 字的查询意图描述
- 严格输出以下 JSON，不要任何其它内容（包括 Markdown 代码块）：

{{
  "keywords": ["..."],
  "intent": "..."
}}`

// queryKeywordsPromptEN 英文检索关键词提取提示词。
//
// 一比一复刻 Python: _QUERY_KEYWORDS_PROMPT_EN
const queryKeywordsPromptEN = `You are a retrieval keyword extractor. The text below is an excerpt from a conversation,
containing user queries, tool execution results (especially failed ones), etc.
Extract keywords useful for *cross-user experience retrieval*.

## Input
{excerpt}

## Current skill hint (may be empty)
{skill_hint}

## Output requirements
- 10-15 keywords covering the core concepts; avoid pronouns / fillers
- Prefer English code identifiers / error keywords; you may add the matching Chinese term to widen recall
- Plus an intent string of <= 40 characters
- Output ONLY this JSON (no Markdown, no explanation):

{{
  "keywords": ["..."],
  "intent": "..."
}}`

// prompts 语言→提示词模板映射。
//
// Python: _PROMPTS = {"cn": ..., "en": ...}
var prompts = map[string]string{
	"cn": queryKeywordsPromptCN,
	"en": queryKeywordsPromptEN,
}

// ──────────────────────────── 全局变量 ────────────────────────────

// extractQueryJSONRe 提取 JSON 对象的正则表达式。
var extractQueryJSONRe = regexp.MustCompile(`\{[\s\S]*\}`)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewKeywordExtractor 创建 KeywordExtractor 实例。
//
// language 仅接受 "cn" 或 "en"，其他值默认为 "cn"。
// policy 为零值时使用 QUERY_KEYWORDS_LLM_POLICY。
//
// Python: KeywordExtractor.__init__()
func NewKeywordExtractor(llm *llm.Model, model string, language string, policy llm_resilience.LLMInvokePolicy) *KeywordExtractor {
	lang := language
	if _, ok := prompts[lang]; !ok {
		lang = "cn"
	}
	p := policy
	if p.MaxAttempts == 0 {
		p = QUERY_KEYWORDS_LLM_POLICY
	}
	return &KeywordExtractor{
		llm:      llm,
		model:    model,
		language: lang,
		policy:   p,
	}
}

// ParseFromOptimizerOutput 从优化器输出中读取 keywords 和 summary。
//
// 接受 EvolutionPatch（已解析）或 map[string]any（LLM 原始 JSON dict），
// 提取 keywords 列表和 summary 字符串。
//
// Python: KeywordExtractor.parse_from_optimizer_output()
func ParseFromOptimizerOutput(rawPatch any) ([]string, string) {
	keywords := []string{}
	summary := ""

	switch v := rawPatch.(type) {
	case checkpointing.EvolutionPatch:
		if v.Keywords != nil {
			for _, k := range v.Keywords {
				s := strings.TrimSpace(k)
				if s != "" {
					keywords = append(keywords, s)
				}
			}
		}
		if v.Summary != nil {
			summary = strings.TrimSpace(*v.Summary)
		}
	case *checkpointing.EvolutionPatch:
		if v != nil {
			if v.Keywords != nil {
				for _, k := range v.Keywords {
					s := strings.TrimSpace(k)
					if s != "" {
						keywords = append(keywords, s)
					}
				}
			}
			if v.Summary != nil {
				summary = strings.TrimSpace(*v.Summary)
			}
		}
	case map[string]any:
		kws, _ := v["keywords"].([]any)
		if kws == nil {
			kws = []any{}
		}
		for _, k := range kws {
			s := strings.TrimSpace(fmt.Sprintf("%v", k))
			if s != "" {
				keywords = append(keywords, s)
			}
		}
		rawSummary, ok := v["summary"].(string)
		if ok {
			summary = strings.TrimSpace(rawSummary)
		}
	}

	return keywords, summary
}

// ExtractQueryKeywords 从对话摘录中提取检索关键词。
//
// 摘录通常包含用户查询、工具执行结果（特别是失败的）等关键信息。
// 优先使用小型 LLM 调用；失败时返回空关键词，确保不阻塞调用方。
//
// Python: KeywordExtractor.extract_query_keywords()
func (e *KeywordExtractor) ExtractQueryKeywords(ctx context.Context, feedbackExcerpt string, skillHint ...string) QueryKeywords {
	excerpt := strings.TrimSpace(feedbackExcerpt)
	if excerpt == "" {
		return QueryKeywords{Keywords: []string{}, Intent: "", RawExcerpt: ""}
	}

	if e.llm == nil || e.model == "" {
		logger.Debug(logComponent).
			Str("method", "KeywordExtractor.ExtractQueryKeywords").
			Msg("[KeywordExtractor] no LLM bound, skipping query keyword extraction")
		return QueryKeywords{
			Keywords:   []string{},
			Intent:     truncateString(excerpt, 40),
			RawExcerpt: excerpt,
		}
	}

	logger.Info(logComponent).
		Str("method", "KeywordExtractor.ExtractQueryKeywords").
		Str("excerpt", excerpt).
		Msg("[KeywordExtractor] query before keyword extraction")

	hint := ""
	if len(skillHint) > 0 {
		hint = strings.TrimSpace(skillHint[0])
	}
	if hint == "" {
		if e.language == "cn" {
			hint = "无"
		} else {
			hint = "None"
		}
	}

	tmpl, ok := prompts[e.language]
	if !ok {
		tmpl = queryKeywordsPromptCN
	}
	prompt := strings.ReplaceAll(tmpl, "{excerpt}", excerpt)
	prompt = strings.ReplaceAll(prompt, "{skill_hint}", hint)

	raw, err := llm_resilience.InvokeTextWithRetry(
		ctx, e.llm, e.model, prompt, e.policy,
		llm_resilience.WithTemperature(0.2),
	)
	if err != nil {
		logger.Warn(logComponent).
			Str("method", "KeywordExtractor.ExtractQueryKeywords").
			Err(err).
			Msg("[KeywordExtractor] LLM call failed")
		return QueryKeywords{
			Keywords:   []string{},
			Intent:     truncateString(excerpt, 40),
			RawExcerpt: excerpt,
		}
	}

	data := extractQueryJSON(raw)
	if data == nil {
		logger.Warn(logComponent).
			Str("method", "KeywordExtractor.ExtractQueryKeywords").
			Msg("[KeywordExtractor] LLM JSON parse failed")
		return QueryKeywords{
			Keywords:   []string{},
			Intent:     truncateString(excerpt, 40),
			RawExcerpt: excerpt,
		}
	}

	rawKeywords, _ := data["keywords"].([]any)
	if rawKeywords == nil {
		rawKeywords = []any{}
	}
	keywords := []string{}
	for _, item := range rawKeywords {
		s := strings.TrimSpace(fmt.Sprintf("%v", item))
		if s != "" {
			keywords = append(keywords, s)
		}
	}
	// 最多保留 20 个关键词
	if len(keywords) > 20 {
		keywords = keywords[:20]
	}

	intentVal, _ := data["intent"]
	intent := strings.TrimSpace(fmt.Sprintf("%v", intentVal))
	intentRunes := []rune(intent)
	if len(intentRunes) > 80 {
		intent = string(intentRunes[:80])
	}

	return QueryKeywords{
		Keywords:   keywords,
		Intent:     intent,
		RawExcerpt: excerpt,
	}
}

// UpdateLLM 后置绑定 LLM 模型实例和名称。
//
// Python: KeywordExtractor.update_llm()
func (e *KeywordExtractor) UpdateLLM(llm *llm.Model, model string) {
	e.llm = llm
	e.model = model
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// extractQueryJSON 从 LLM 原始响应中提取 JSON 对象。
//
// 先尝试 json.Unmarshal；失败则用正则匹配 {…} 后再解析。
//
// Python: _extract_query_json()
func extractQueryJSON(raw string) map[string]any {
	if raw == "" {
		return nil
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(raw), &result); err == nil {
		return result
	}
	matched := extractQueryJSONRe.FindString(raw)
	if matched == "" {
		return nil
	}
	if err := json.Unmarshal([]byte(matched), &result); err == nil {
		return result
	}
	return nil
}

// truncateString 截断字符串到指定字符数（按 rune 计数，对齐 Python s[:40]）。
func truncateString(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen])
}
