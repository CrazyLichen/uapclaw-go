package experience

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/evolving/checkpointing"
	"github.com/uapclaw/uapclaw-go/internal/evolving/optimizer/llm_resilience"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ExperienceScorer LLM 驱动的经验评分和整理器。
//
// 对应 Python: ExperienceScorer
type ExperienceScorer struct {
	llm            *llm.Model
	model          string
	language       string
	evaluatePolicy llm_resilience.LLMInvokePolicy
	simplifyPolicy llm_resilience.LLMInvokePolicy
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// WE 效果权重
	WE = 0.5
	// WU 利用权重
	WU = 0.3
	// WF 新鲜度权重
	WF = 0.2
	// FreshnessHalfLifeDays 新鲜度衰减半衰期（天）
	FreshnessHalfLifeDays = 90
	// StaleVersionPenalty 过期版本惩罚系数
	StaleVersionPenalty = 0.7
	// bt 反引号字符常量，用于拼接提示词中的 markdown 代码块标记
	bt = "\x60"
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// EvaluateLLMPolicy 评估 LLM 调用策略默认值
	// 对应 Python: EVALUATE_LLM_POLICY
	EvaluateLLMPolicy = llm_resilience.LLMInvokePolicy{
		AttemptTimeoutSecs: 60, TotalBudgetSecs: 120, MaxAttempts: 2,
		BackoffBaseSecs: 1.0, RetryEmptyResponse: true,
	}
	// SimplifyLLMPolicy 整理 LLM 调用策略默认值
	// 对应 Python: SIMPLIFY_LLM_POLICY
	SimplifyLLMPolicy = llm_resilience.LLMInvokePolicy{
		AttemptTimeoutSecs: 150, TotalBudgetSecs: 300, MaxAttempts: 2,
		BackoffBaseSecs: 1.0, RetryEmptyResponse: true,
	}
	// codeBlockOpen markdown 代码块开始标记（```json）
	codeBlockOpen = bt + bt + bt + "json"
	// codeBlockClose markdown 代码块结束标记（```）
	codeBlockClose = bt + bt + bt
)

// 双语提示词（一比一复刻 Python 原文）
// Python 的 {{ 在 .format() 中表示 literal {，Go raw string 中不需要转义
// Go raw string 不能包含反引号字符，提示词中的 ```json 代码块标记用 bt 常量拼接

// ExperienceEvalPromptCN 中文经验评估提示词
// 对应 Python: EXPERIENCE_EVAL_PROMPT_CN
// 注意：Python 中 {{ 在 .format() 表示 literal {，Go 中直接用 {；
// Python 中 ```json 的反引号在 Go raw string 中不可表示，用常量拼接
var ExperienceEvalPromptCN = `你是一个经验评估专家。根据对话片段，评估之前展示给 Agent 的经验是否被有效使用。

## 展示给 Agent 的经验
{presented_experiences}

## 对话片段（展示经验之后的部分）
{conversation_snippet}

## 评估任务
对于每条展示的经验，判断：
1. 该经验是否被 Agent 理解和采纳（内容被用于指导后续行为）
2. 该经验是否产生了积极效果（帮助解决了问题或改进了输出）
3. 该经验是否产生了消极效果（导致错误或误导）

## 输出格式
输出 JSON 数组，每条经验一个对象：
` + codeBlockOpen + `
[
  {
    "record_id": "经验ID",
    "used": true/false,
    "positive": true/false,
    "negative": true/false,
    "reason": "简短说明"
  }
]
` + codeBlockClose + `

只输出 JSON，不要其他内容。`

// ExperienceEvalPromptEN 英文经验评估提示词
// 对应 Python: EXPERIENCE_EVAL_PROMPT_EN
var ExperienceEvalPromptEN = `You are an experience evaluation expert. Based on the conversation snippet, evaluate whether the previously presented experiences were effectively used by the Agent.

## Experiences Presented to Agent
{presented_experiences}

## Conversation Snippet (after presenting experiences)
{conversation_snippet}

## Evaluation Task
For each presented experience, determine:
1. Whether the experience was understood and adopted by the Agent (content used to guide subsequent behavior)
2. Whether the experience produced positive effects (helped solve problems or improved output)
3. Whether the experience produced negative effects (caused errors or mislead)

## Output Format
Output a JSON array, one object per experience:
` + codeBlockOpen + `
[
  {
    "record_id": "experience ID",
    "used": true/false,
    "positive": true/false,
    "negative": true/false,
    "reason": "brief explanation"
  }
]
` + codeBlockClose + `

Output only JSON, no other content.`

// ExperienceEvalPrompt 双语经验评估提示词映射
// 对应 Python: EXPERIENCE_EVAL_PROMPT
var ExperienceEvalPrompt = map[string]string{"cn": ExperienceEvalPromptCN, "en": ExperienceEvalPromptEN}

// SimplifyPromptCN 中文经验整理提示词
// 对应 Python: SIMPLIFY_PROMPT_CN
var SimplifyPromptCN = `你是一个经验库维护专家。根据当前经验的评分和使用情况，生成整理建议。

## Skill 名称
{skill_name}

## Skill 摘要
{skill_summary}

## 当前经验列表（按分数排序）
{scored_experiences}

## 整理操作类型
- DELETE: 删除低质量或过时的经验
- MERGE: 合并多条相似经验为一条
- REFINE: 优化单条经验的内容
- KEEP: 保留不变

## 规则
1. 删除分数低于 0.4 且使用率为 0 的经验
2. 合并内容高度相似的经验（保留分数最高的作为 primary）
3. 优化内容模糊或格式不规范的经验
4. 保留高质量、高使用率的经验

## 输出格式
输出 JSON 数组：
` + codeBlockOpen + `
[
  {
    "action": "DELETE | MERGE | REFINE | KEEP",
    "record_id": "目标经验ID",
    "reason": "操作原因",
    "merge_remove_ids": ["要合并删除的经验ID列表（仅 MERGE 时）"],
    "new_content": "新内容（仅 REFINE 或 MERGE 时）"
  }
]
` + codeBlockClose + `

只输出 JSON，不要其他内容。`

// SimplifyPromptEN 英文经验整理提示词
// 对应 Python: SIMPLIFY_PROMPT_EN
var SimplifyPromptEN = `You are an experience library maintenance expert. Based on current experience scores and usage, generate organization suggestions.

## Skill Name
{skill_name}

## Skill Summary
{skill_summary}

## Current Experience List (sorted by score)
{scored_experiences}

## Maintenance Actions
- DELETE: Remove low-quality or outdated experiences
- MERGE: Combine multiple similar experiences into one
- REFINE: Optimize content of a single experience
- KEEP: Keep unchanged

## Rules
1. Delete experiences with score below 0.4 and zero utilization
2. Merge highly similar experiences (keep the highest-scored as primary)
3. Refine experiences with vague or poorly formatted content
4. Keep high-quality, high-utilization experiences

## Output Format
Output a JSON array:
` + codeBlockOpen + `
[
  {
    "action": "DELETE | MERGE | REFINE | KEEP",
    "record_id": "target experience ID",
    "reason": "reason for action",
    "merge_remove_ids": ["list of experience IDs to merge and remove (MERGE only)"],
    "new_content": "new content (REFINE or MERGE only)"
  }
]
` + codeBlockClose + `

Output only JSON, no other content.`

// SimplifyPrompt 双语经验整理提示词映射
// 对应 Python: SIMPLIFY_PROMPT
var SimplifyPrompt = map[string]string{"cn": SimplifyPromptCN, "en": SimplifyPromptEN}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewExperienceScorer 创建 ExperienceScorer。
//
// llmModel 不能为 nil（对应 Python 中 Model 为必填参数，非 Optional），
// 若传入 nil 则返回错误。
// 对应 Python: ExperienceScorer.__init__()
func NewExperienceScorer(
	llmModel *llm.Model,
	model string,
	language string,
	evaluatePolicy *llm_resilience.LLMInvokePolicy,
	simplifyPolicy *llm_resilience.LLMInvokePolicy,
) (*ExperienceScorer, error) {
	if llmModel == nil {
		return nil, fmt.Errorf("ExperienceScorer: llmModel 不能为 nil")
	}
	ep := EvaluateLLMPolicy
	if evaluatePolicy != nil {
		ep = *evaluatePolicy
	}
	sp := SimplifyLLMPolicy
	if simplifyPolicy != nil {
		sp = *simplifyPolicy
	}
	return &ExperienceScorer{
		llm:            llmModel,
		model:          model,
		language:       language,
		evaluatePolicy: ep,
		simplifyPolicy: sp,
	}, nil
}

// CalcEffectiveness 计算 E（Effectiveness）评分，使用贝叶斯平滑 Beta(1,1)。
//
// stats 为 nil → 返回 0.5；total == 0 → 返回 0.5；
// 否则 (TimesPositive + 1) / (total + 2)。
// 对应 Python: calc_effectiveness()
func CalcEffectiveness(stats *checkpointing.UsageStats) float64 {
	if stats == nil {
		return 0.5
	}
	total := stats.TimesPositive + stats.TimesNegative
	if total == 0 {
		return 0.5
	}
	return float64(stats.TimesPositive+1) / float64(total+2)
}

// CalcUtilization 计算 U（Utilization）评分。
//
// stats 为 nil → 返回 0.5；TimesPresented == 0 → 返回 0.5；
// 否则 TimesUsed / TimesPresented。
// 对应 Python: calc_utilization()
func CalcUtilization(stats *checkpointing.UsageStats) float64 {
	if stats == nil {
		return 0.5
	}
	if stats.TimesPresented == 0 {
		return 0.5
	}
	return float64(stats.TimesUsed) / float64(stats.TimesPresented)
}

// CalcFreshness 计算 F（Freshness）评分，指数衰减 + 版本惩罚。
//
// timestamp 空 → 返回 0.5；解析失败 → 返回 0.5；
// 衰减 = 0.5 * 2^(-daysOld/90)，新鲜度 = 0.5 + 衰减；
// 版本不匹配 → *= 0.7；clamp 到 [0,1]。
// 对应 Python: calc_freshness()
func CalcFreshness(record *checkpointing.EvolutionRecord, currentSkillVersion *string) float64 {
	if record.Timestamp == "" {
		return 0.5
	}

	recordTime, err := parseTimestamp(record.Timestamp)
	if err != nil {
		return 0.5
	}

	now := time.Now().UTC()
	daysOld := int(now.Sub(recordTime).Hours() / 24)

	// 对齐 Python: decay_factor = 0.5 * 2^(-days_old / half_life)
	decayFactor := 0.5 * math.Pow(2, -float64(daysOld)/FreshnessHalfLifeDays)
	freshness := 0.5 + decayFactor // 范围: 0.5 ~ 1.0

	// 对齐 Python: version staleness penalty
	if currentSkillVersion != nil && *currentSkillVersion != "" &&
		record.SkillVersion != nil && *record.SkillVersion != "" {
		if *record.SkillVersion != *currentSkillVersion {
			freshness *= StaleVersionPenalty
		}
	}

	return math.Max(0.0, math.Min(1.0, freshness))
}

// CalcScore 计算综合评分 WE*e + WU*u + WF*f。
//
// stats 为 nil → 初始化零值；然后分别计算 e/u/f。
// 对应 Python: calc_score()
func CalcScore(record *checkpointing.EvolutionRecord, currentSkillVersion *string) float64 {
	stats := record.UsageStats
	if stats == nil {
		stats = &checkpointing.UsageStats{}
	}

	e := CalcEffectiveness(stats)
	u := CalcUtilization(stats)
	f := CalcFreshness(record, currentSkillVersion)

	return WE*e + WU*u + WF*f
}

// UpdateScore 更新记录的 usage_stats 和重新计算评分。
//
// 如果 record.UsageStats == nil → 初始化零值；
// 按 evalResult["used/positive/negative"] 更新 TimesUsed/TimesPositive/TimesNegative（bool 类型断言）；
// 设置 LastEvaluatedAt = time.Now().UTC().Format(time.RFC3339Nano)；
// 对应 Python: record.Score = CalcScore(record, currentSkillVersion)
// 对应 Python: update_score()
func UpdateScore(
	record *checkpointing.EvolutionRecord,
	evalResult map[string]any,
	currentSkillVersion *string,
) float64 {
	if record.UsageStats == nil {
		record.UsageStats = &checkpointing.UsageStats{}
	}

	stats := record.UsageStats

	if getBoolFromEvalResult(evalResult, "used") {
		stats.TimesUsed++
	}
	if getBoolFromEvalResult(evalResult, "positive") {
		stats.TimesPositive++
	}
	if getBoolFromEvalResult(evalResult, "negative") {
		stats.TimesNegative++
	}

	nowStr := time.Now().UTC().Format(time.RFC3339Nano)
	stats.LastEvaluatedAt = &nowStr

	record.Score = CalcScore(record, currentSkillVersion)
	return record.Score
}

// Evaluate 评估展示经验是否被有效使用。
//
// 空 records → 返回空 slice, nil；
// 对应 Python: formatted = formatPresentedExperiences(presentedRecords)
// 选择语言模板，替换 {presented_experiences} 和 {conversation_snippet}（snippet 限制 4000 字符）；
// 调用 llm_resilience.InvokeTextWithRetry + WithIsResultUsable(parseLLMJSON != nil)；
// err != nil → logger.Error 并返回空 slice, nil；
// parseLLMJSON nil → logger.Warn 并返回空 slice, nil；
// 返回 results, nil。
// 对应 Python: ExperienceScorer.evaluate()
func (s *ExperienceScorer) Evaluate(
	ctx context.Context,
	conversationSnippet string,
	presentedRecords []checkpointing.EvolutionRecord,
) ([]map[string]any, error) {
	if len(presentedRecords) == 0 {
		return nil, nil
	}

	formatted := formatPresentedExperiences(presentedRecords)

	// 对齐 Python: 选择语言模板，替换占位符
	template, ok := ExperienceEvalPrompt[s.language]
	if !ok {
		template = ExperienceEvalPrompt["cn"]
	}
	prompt := strings.ReplaceAll(template, "{presented_experiences}", formatted)
	prompt = strings.ReplaceAll(prompt, "{conversation_snippet}", truncateString(conversationSnippet, 4000))

	// 对齐 Python: invoke_text_with_retry + is_result_usable
	raw, err := llm_resilience.InvokeTextWithRetry(
		ctx, s.llm, s.model, prompt, s.evaluatePolicy,
		llm_resilience.WithIsResultUsable(func(text string) bool {
			return parseLLMJSON(text) != nil
		}),
	)
	if err != nil {
		logger.Error(logComponent).
			Str("method", "ExperienceScorer.Evaluate").
			Err(err).
			Msg("[ExperienceScorer] evaluate LLM 调用失败")
		return nil, nil
	}

	results := parseLLMJSON(raw)
	if results == nil {
		logger.Warn(logComponent).
			Str("method", "ExperienceScorer.Evaluate").
			Msg("[ExperienceScorer] evaluate: 无法解析 LLM 响应")
		return nil, nil
	}

	return results, nil
}

// Simplify 生成经验库整理建议。
//
// 空 records → logger.Info 并返回空 slice, nil；
// 对应 Python: formatted = formatScoredExperiences(records)
// 选择语言模板，替换 {skill_name}/{skill_summary}(限1000)/{scored_experiences}；
// userIntent != nil → prompt += "\n\n**用户意图**: " + *userIntent；
// 记录 simplify 开始日志（skill_name, records_count, prompt_chars, attempt_timeout, total_budget, max_attempts）；
// 调用 llm_resilience.InvokeTextWithRetry + WithIsResultUsable；
// err != nil → logger.Error（elapsed, skill, records, prompt_chars, error）并返回空 slice, nil；
// 记录 simplify 完成日志（elapsed, skill, response_chars）；
// parseLLMJSON nil → logger.Warn 并返回空 slice, nil；
// 对应 Python: logger.Info(skill, actions_count)
// 返回 actions, nil。
// 对应 Python: ExperienceScorer.simplify()
func (s *ExperienceScorer) Simplify(
	ctx context.Context,
	skillName string,
	skillSummary string,
	records []checkpointing.EvolutionRecord,
	userIntent *string,
) ([]map[string]any, error) {
	if len(records) == 0 {
		logger.Info(logComponent).
			Str("skill_name", skillName).
			Msg("[ExperienceScorer] simplify 跳过: 无记录")
		return nil, nil
	}

	formatted := formatScoredExperiences(records)

	// 对齐 Python: 选择语言模板，替换占位符
	template, ok := SimplifyPrompt[s.language]
	if !ok {
		template = SimplifyPrompt["cn"]
	}
	prompt := strings.ReplaceAll(template, "{skill_name}", skillName)
	prompt = strings.ReplaceAll(prompt, "{skill_summary}", truncateString(skillSummary, 1000))
	prompt = strings.ReplaceAll(prompt, "{scored_experiences}", formatted)

	// 对齐 Python: if user_intent: prompt += f"\n\n**用户意图**: {user_intent}"
	if userIntent != nil && *userIntent != "" {
		prompt += fmt.Sprintf("\n\n**用户意图**: %s", *userIntent)
	}

	// 对齐 Python: simplify 开始日志（skill_name, records_count, prompt_chars, attempt_timeout, total_budget, max_attempts）
	logger.Info(logComponent).
		Str("skill_name", skillName).
		Int("records_count", len(records)).
		Int("prompt_chars", len(prompt)).
		Float64("attempt_timeout", s.simplifyPolicy.AttemptTimeoutSecs).
		Float64("total_budget", s.simplifyPolicy.TotalBudgetSecs).
		Int("max_attempts", s.simplifyPolicy.MaxAttempts).
		Msg("[ExperienceScorer] simplify LLM 调用开始")

	startedAt := time.Now()

	raw, err := llm_resilience.InvokeTextWithRetry(
		ctx, s.llm, s.model, prompt, s.simplifyPolicy,
		llm_resilience.WithIsResultUsable(func(text string) bool {
			return parseLLMJSON(text) != nil
		}),
	)
	if err != nil {
		elapsed := time.Since(startedAt).Seconds()
		logger.Error(logComponent).
			Float64("elapsed", elapsed).
			Str("skill", skillName).
			Int("records", len(records)).
			Int("prompt_chars", len(prompt)).
			Err(err).
			Msg("[ExperienceScorer] simplify LLM 调用失败")
		return nil, nil
	}

	elapsed := time.Since(startedAt).Seconds()
	logger.Info(logComponent).
		Float64("elapsed", elapsed).
		Str("skill", skillName).
		Int("response_chars", len(raw)).
		Msg("[ExperienceScorer] simplify LLM 调用完成")

	actions := parseLLMJSON(raw)
	if actions == nil {
		logger.Warn(logComponent).
			Str("skill", skillName).
			Int("response_chars", len(raw)).
			Msg("[ExperienceScorer] simplify: 无法解析 LLM 响应")
		return nil, nil
	}

	logger.Info(logComponent).
		Str("skill", skillName).
		Int("actions_count", len(actions)).
		Msg("[ExperienceScorer] simplify 解析完成")

	return actions, nil
}

// UpdateLLM 热更新 LLM/model。
//
// llmModel 不能为 nil（与 NewExperienceScorer 一致），若传入 nil 则直接返回不更新，
// 避免 scorer 进入不可用状态。
// 对应 Python: ExperienceScorer.update_llm()
func (s *ExperienceScorer) UpdateLLM(llmModel *llm.Model, model string) {
	if llmModel == nil {
		return
	}
	s.llm = llmModel
	s.model = model
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// formatPresentedExperiences 格式化展示经验用于提示词。
//
// 遍历 records，每条 "[record.ID] record.Change.Content[:200]"，用 "\n" 连接。
// 对应 Python: ExperienceScorer._format_presented_experiences()
func formatPresentedExperiences(records []checkpointing.EvolutionRecord) string {
	lines := make([]string, 0, len(records))
	for _, record := range records {
		content := truncateString(record.Change.Content, 200)
		lines = append(lines, fmt.Sprintf("[%s] %s", record.ID, content))
	}
	return strings.Join(lines, "\n")
}

// formatScoredExperiences 格式化评分经验用于提示词。
//
// 遍历 records，stats = record.UsageStats（nil → 零值）；
// 每条 "[record.ID] score=%.2f | presented=stats.TimesPresented used=stats.TimesUsed | record.Change.Content[:150]"；
// 用 "\n" 连接。
// 对应 Python: ExperienceScorer._format_scored_experiences()
func formatScoredExperiences(records []checkpointing.EvolutionRecord) string {
	lines := make([]string, 0, len(records))
	for _, record := range records {
		stats := record.UsageStats
		if stats == nil {
			stats = &checkpointing.UsageStats{}
		}
		content := truncateString(record.Change.Content, 150)
		lines = append(lines, fmt.Sprintf("[%s] score=%.2f | presented=%d used=%d | %s",
			record.ID, record.Score,
			stats.TimesPresented, stats.TimesUsed,
			content,
		))
	}
	return strings.Join(lines, "\n")
}

// parseLLMJSON 最佳努力解析 LLM JSON 输出。
//
// 1. strip，空 → nil
// 2. re.sub 去掉 markdown code blocks：^```(?:json)?\s* 和 ```\s*$
// 3. re.sub 去掉 // 行注释：//[^\n]*
// 4. re.sub 去掉尾逗号：,\s*([}\]]) → \1
// 步骤 5: strip（去除前后空白）
// 6. json.Unmarshal → []map → 返回；单个 map → 包装为 slice；否则 nil
// 7. 失败 → regexp 提取 [\s\S]* → 再次 json.Unmarshal → list 返回
// 对应 Python: ExperienceScorer._parse_llm_json()
func parseLLMJSON(raw string) []map[string]any {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}

	// 去掉 markdown code blocks（```json ... ```）
	reCodeBlockOpen := regexp.MustCompile("^```(?:json)?\\s*")
	reCodeBlockClose := regexp.MustCompile("```\\s*$")
	raw = reCodeBlockOpen.ReplaceAllString(raw, "")
	raw = reCodeBlockClose.ReplaceAllString(raw, "")

	// 去掉 // 行注释
	reLineComment := regexp.MustCompile(`//[^\n]*`)
	raw = reLineComment.ReplaceAllString(raw, "")

	// 去掉尾逗号（,\s*([}\]]) → \1）
	reTrailingComma := regexp.MustCompile(`,\s*([}\]])`)
	raw = reTrailingComma.ReplaceAllString(raw, "$1")

	raw = strings.TrimSpace(raw)

	// 尝试 json.Unmarshal
	var data any
	if err := json.Unmarshal([]byte(raw), &data); err == nil {
		switch v := data.(type) {
		case []any:
			return convertSliceToMapSlice(v)
		case map[string]any:
			return []map[string]any{v}
		default:
			return nil
		}
	}

	// 失败时尝试 regexp 提取 [\s\S]* → 再次 json.Unmarshal
	reArray := regexp.MustCompile(`\[[\s\S]*\]`)
	match := reArray.FindString(raw)
	if match != "" {
		var extracted any
		if err := json.Unmarshal([]byte(match), &extracted); err == nil {
			if slice, ok := extracted.([]any); ok {
				return convertSliceToMapSlice(slice)
			}
		}
	}

	return nil
}

// convertSliceToMapSlice 将 []any 转为 []map[string]any。
func convertSliceToMapSlice(slice []any) []map[string]any {
	result := make([]map[string]any, 0, len(slice))
	for _, item := range slice {
		if m, ok := item.(map[string]any); ok {
			result = append(result, m)
		}
	}
	return result
}

// parseTimestamp 解析 UTC ISO 时间戳，对齐 Python 的 datetime.fromisoformat。
//
// 对齐 Python: record_time = datetime.fromisoformat(record.timestamp.replace("Z", "+00:00"))
// 对齐 Python: if record_time.tzinfo is None: record_time = record_time.replace(tzinfo=timezone.utc)
func parseTimestamp(ts string) (time.Time, error) {
	s := strings.ReplaceAll(ts, "Z", "+00:00")
	// 先尝试标准格式（含时区）
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC(), nil
		}
	}
	// 尝试无时区 ISO 格式，补 UTC（对齐 Python: fromisoformat + tzinfo=None → UTC）
	for _, layout := range []string{"2006-01-02T15:04:05.999999999", "2006-01-02T15:04:05"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("无法解析时间戳: %s", ts)
}

// truncateString 截断字符串到最大长度。
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}

// getBoolFromEvalResult 从评估结果字典中获取布尔值。
//
// Python 的 eval_result.get("used") 对 Go 的 any 类型需要安全转换。
func getBoolFromEvalResult(m map[string]any, key string) bool {
	v, ok := m[key]
	if !ok {
		return false
	}
	switch val := v.(type) {
	case bool:
		return val
	case string:
		return val == "true" || val == "True"
	default:
		return false
	}
}
