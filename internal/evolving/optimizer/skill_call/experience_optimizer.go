package skill_call

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/operator"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/evolving/checkpointing"
	"github.com/uapclaw/uapclaw-go/internal/evolving/experience"
	"github.com/uapclaw/uapclaw-go/internal/evolving/optimizer/llm_resilience"
	"github.com/uapclaw/uapclaw-go/internal/evolving/schema"
	"github.com/uapclaw/uapclaw-go/internal/evolving/signal"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SkillExperienceOptimizer 个体技能经验优化器。
//
// 信号从 SkillEvolutionRail 到达，通过优化器的中性信号选择契约消费。
// Backward 阶段按 skill_name 分组选中的信号，生成 EvolutionRecord(s)。
//
// 对应 Python: SkillExperienceOptimizer
type SkillExperienceOptimizer struct {
	SkillExperienceOptimizerBase
	// generateRecordsLLMPolicy 个体记录生成 LLM 调用策略
	generateRecordsLLMPolicy llm_resilience.LLMInvokePolicy
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSkillExperienceOptimizer 创建 SkillExperienceOptimizer 实例。
//
// 对齐 Python:
//
//	创建 SkillExperienceOptimizer（参数: llm, model, language, generate_records_llm_policy）
func NewSkillExperienceOptimizer(llmModel *llm.Model, model string, language string, policy llm_resilience.LLMInvokePolicy) *SkillExperienceOptimizer {
	return &SkillExperienceOptimizer{
		SkillExperienceOptimizerBase: SkillExperienceOptimizerBase{
			llm:            llmModel,
			model:          model,
			language:       language,
			onlineContexts: map[string]*experience.EvolutionContext{},
		},
		generateRecordsLLMPolicy: policy,
	}
}

// Bind 由 SkillExperienceOptimizerBase.Bind 继承，无需重复定义。
// 基类已实现 online_contexts 提取 + BaseOptimizerMixin.Bind 调用。

// AddTrajectory 缓存 Trajectory 供 backward 阶段查询。
func (o *SkillExperienceOptimizer) AddTrajectory(traj *signal.EvolutionSignal) {
	// SkillExperienceOptimizer 不使用 trajectory
}

// Backward 反向传播：从信号计算梯度。
//
// 对齐 Python: SkillExperienceOptimizer._backward(signals)
//
//		for op_id, op in self._operators.items():
//		    skill_name = op_id.removeprefix("skill_experience_")
//		    skill_signals = [s for s in self._selected_signals if s.skill_name == skill_name or not s.skill_name]
//	   如果没有 skill_signals 则跳过
//		    ctx = self._build_evolution_context(skill_name, op, skill_signals)
//		    records = await self.generate_records(ctx)
//		    if not records:
//	       logger.info: 无记录生成（skill=%s）
//		        continue
//		    existing = param.get_gradient(EXPERIENCES_TARGET) or []
//		    param.set_gradient(EXPERIENCES_TARGET, existing + records)
func (o *SkillExperienceOptimizer) Backward(ctx context.Context, signals []*signal.EvolutionSignal) error {
	o.ValidateParameters()
	selected := o.SelectSignals(signals)
	o.SetSelectedSignals(selected)

	for opID, op := range o.BaseOptimizerMixin.Operators() {
		skillName := removeSkillPrefix(opID)
		// 对齐 Python: skill_signals = [s for s in self._selected_signals if s.skill_name == skill_name or not s.skill_name]
		var skillSignals []*signal.EvolutionSignal
		for _, s := range selected {
			if (s.SkillName != nil && *s.SkillName == skillName) || s.SkillName == nil {
				skillSignals = append(skillSignals, s)
			}
		}
		if len(skillSignals) == 0 {
			continue
		}

		evoCtx, err := o.buildEvolutionContext(skillName, op, skillSignals)
		if err != nil {
			logger.Error(logComponent).
				Str("method", "Backward").
				Str("skill_name", skillName).
				Err(err).
				Msg("[SkillExperienceOptimizer] 上下文构建失败，跳过")
			continue
		}

		records, err := o.GenerateRecords(ctx, evoCtx)
		if err != nil {
			logger.Error(logComponent).
				Str("method", "Backward").
				Str("skill_name", skillName).
				Err(err).
				Msg("[SkillExperienceOptimizer] GenerateRecords 失败")
			continue
		}
		if len(records) == 0 {
			logger.Info(logComponent).
				Str("skill_name", skillName).
				Msg("[SkillExperienceOptimizer] no records generated for skill")
			continue
		}

		// 对齐 Python: existing = param.get_gradient(EXPERIENCES_TARGET) or []
		//	param.set_gradient(EXPERIENCES_TARGET, existing + records)
		param := o.BaseOptimizerMixin.Parameters()[opID]
		existingAny := param.GetGradient(schema.ExperiencesTarget)
		var existing []checkpointing.EvolutionRecord
		if existingAny != nil {
			if eList, ok := existingAny.([]checkpointing.EvolutionRecord); ok {
				existing = eList
			}
		}
		param.SetGradient(schema.ExperiencesTarget, append(existing, records...))

		logger.Info(logComponent).
			Str("skill_name", skillName).
			Int("record_count", len(records)).
			Msg("[SkillExperienceOptimizer] generated record(s) for skill")
	}
	return nil
}

// Step 生成更新映射，由 Trainer.apply_updates 统一应用。
//
// 对齐 Python: SkillExperienceOptimizer._step()
//
//	updates = {}
//	for op_id, param in self._parameters.items():
//	    records = param.get_gradient(EXPERIENCES_TARGET) or []
//	    if records: updates[(op_id, EXPERIENCES_TARGET)] = records
//	return updates
func (o *SkillExperienceOptimizer) Step() map[schema.UpdateKey]any {
	o.ValidateParameters()
	updates := make(map[schema.UpdateKey]any)
	for opID, param := range o.BaseOptimizerMixin.Parameters() {
		recordsAny := param.GetGradient(schema.ExperiencesTarget)
		if recordsAny != nil {
			if records, ok := recordsAny.([]checkpointing.EvolutionRecord); ok && len(records) > 0 {
				updates[schema.UpdateKey{opID, schema.ExperiencesTarget}] = records
			}
		}
	}
	o.ClearTrajectories()
	return updates
}

// SelectSignals 选择此优化器可消费的信号。默认保留全部信号。
func (o *SkillExperienceOptimizer) SelectSignals(signals []*signal.EvolutionSignal) []*signal.EvolutionSignal {
	result := make([]*signal.EvolutionSignal, len(signals))
	copy(result, signals)
	return result
}

// GenerateRecords 生成并解析 LLM 输出的演进记录。
//
// 对齐 Python: SkillExperienceOptimizer.generate_records(ctx)
func (o *SkillExperienceOptimizer) GenerateRecords(ctx context.Context, evoCtx *experience.EvolutionContext) ([]checkpointing.EvolutionRecord, error) {
	if len(evoCtx.Signals) == 0 {
		return nil, nil
	}

	conversationSnippet := buildConversationSnippet(evoCtx.Messages, 30, 300, o.language)
	signalsJSON, _ := json.Marshal(signalValuesToDicts(evoCtx.Signals))
	descSummary := buildExistingSummary(evoCtx.ExistingDescRecords, "description")
	bodySummary := buildExistingSummary(evoCtx.ExistingBodyRecords, "body")
	skillContent := summarizeSkillContent(evoCtx.SkillContent)

	defaultExistingSummary := "无已有记录"
	if o.language == "en" {
		defaultExistingSummary = "No existing records"
	}

	prompt := formatTemplate(SkillExperienceGeneratePrompt[o.language],
		"skill_content", skillContent,
		"signals_json", string(signalsJSON),
		"conversation_snippet", strings.TrimSpace(conversationSnippet),
		"existing_desc_summary", orDefault(descSummary, defaultExistingSummary),
		"existing_body_summary", orDefault(bodySummary, defaultExistingSummary),
		"user_query", orDefault(evoCtx.UserQuery, "无", "None"),
	)

	retryPrompt := formatTemplate(SkillExperienceGeneratePrompt[o.language],
		"skill_content", summarizeSkillContentWithMax(evoCtx.SkillContent, 2500),
		"signals_json", string(signalValuesToJSONCompact(evoCtx.Signals)),
		"conversation_snippet", strings.TrimSpace(buildConversationSnippet(evoCtx.Messages, 10, 100, o.language)),
		"existing_desc_summary", orDefault(limitSummaryLines(descSummary, 2), defaultExistingSummary),
		"existing_body_summary", orDefault(limitSummaryLines(bodySummary, 2), defaultExistingSummary),
		"user_query", truncateOrDefault(evoCtx.UserQuery, 500, "无", "None"),
	)

	logger.Info(logComponent).
		Str("skill_name", evoCtx.SkillName).
		Msg("[SkillExperienceOptimizer] calling LLM")

	drafts, err := o.generateDraftsWithRetries(ctx, prompt, retryPrompt)
	if err != nil {
		logger.Error(logComponent).
			Str("skill_name", evoCtx.SkillName).
			Err(err).
			Msg("[SkillExperienceOptimizer] LLM call failed")
		return nil, err
	}

	source := evoCtx.Signals[0].SignalType
	mergedContext := buildContextFromValues(evoCtx.Signals)
	var textRecords []checkpointing.EvolutionRecord
	var scriptRecords []checkpointing.EvolutionRecord

	for _, draft := range drafts {
		patch := draft.Patch
		if patch.Action == "skip" {
			reason := "unknown"
			if patch.SkipReason != nil {
				reason = *patch.SkipReason
			}
			logger.Info(logComponent).
				Str("reason", reason).
				Msg("[SkillExperienceOptimizer] LLM decided to skip")
			continue
		}
		if strings.TrimSpace(patch.Content) == "" {
			logger.Info(logComponent).
				Msg("[SkillExperienceOptimizer] LLM returned empty content, skipping")
			continue
		}
		isScript := patch.Target == signal.EvolutionTargetScript
		if isScript && len(scriptRecords) >= 1 {
			continue
		}
		if !isScript && len(textRecords) >= 2 {
			continue
		}
		initialScore := InitialScoreBySignal[source]
		record := checkpointing.MakeEvolutionRecord(
			source,
			mergedContext,
			patch,
			initialScore,
			nil,
			draft.Summary,
		)
		if isScript {
			scriptRecords = append(scriptRecords, *record)
		} else {
			textRecords = append(textRecords, *record)
		}
		logger.Info(logComponent).
			Str("record_id", record.ID).
			Str("section", patch.Section).
			Str("target", string(patch.Target)).
			Str("merge_target", ptrToStr(patch.MergeTarget)).
			Msg("[SkillExperienceOptimizer] generated record")
	}
	return append(textRecords, scriptRecords...), nil
}

// RetryParseDrafts 重试解析：截断→重新生成 / 格式错误→JSON_FIX / attempt≥3→JSON_FIX_STRICT。
//
// 对齐 Python: SkillExperienceOptimizer.retry_parse_drafts(broken_raw, original_prompt, attempt_number, parse_error)
func (o *SkillExperienceOptimizer) RetryParseDrafts(ctx context.Context, brokenRaw string, originalPrompt string, attemptNumber int, parseError string) ([]ParsedExperienceDraft, string, error) {
	truncated := LooksTruncated(brokenRaw)
	var retryPrompt string

	if truncated {
		if attemptNumber >= 3 {
			logger.Warn(logComponent).
				Msg("[SkillExperienceOptimizer] output still truncated on attempt 3, giving up")
			return nil, brokenRaw, fmt.Errorf("第 3 次尝试输出仍被截断")
		}
		logger.Warn(logComponent).
			Msg("[SkillExperienceOptimizer] output appears truncated, retrying full regeneration")
		retryPrompt = originalPrompt
	} else if attemptNumber >= 3 {
		logger.Warn(logComponent).
			Int("attempt", attemptNumber).
			Msg("[SkillExperienceOptimizer] JSON malformed, using strict fix prompt")
		errorDetail := parseError
		if errorDetail == "" {
			errorDetail = "无法解析为合法 JSON"
		}
		retryPrompt = formatTemplate(JSONFixPromptStrict,
			"parse_error", errorDetail,
			"broken_preview", truncateString(brokenRaw, 500),
		)
	} else {
		logger.Warn(logComponent).
			Str("preview", truncateString(brokenRaw, 200)).
			Msg("[SkillExperienceOptimizer] JSON malformed, requesting fix")
		errorDetail := parseError
		if errorDetail == "" {
			errorDetail = "JSON 解析失败"
		}
		retryPrompt = formatTemplate(JSONFixPrompt,
			"parse_error", errorDetail,
			"broken_output", brokenRaw,
		)
	}

	// 对齐 Python: response = await self._llm.invoke(model=self._model, messages=..., temperature=0.1, timeout=...)
	response, err := llm_resilience.InvokeTextWithRetry(
		ctx,
		o.llm,
		o.model,
		retryPrompt,
		llm_resilience.LLMInvokePolicy{
			AttemptTimeoutSecs: float64(RetryParseTimeoutSecs),
			TotalBudgetSecs:    float64(RetryParseTimeoutSecs) * 2,
			MaxAttempts:        1,
		},
		llm_resilience.WithTemperature(0.1),
	)
	if err != nil {
		logger.Error(logComponent).
			Err(err).
			Msg("[SkillExperienceOptimizer] retry LLM call failed")
		return nil, "", err
	}

	drafts, lastError := ParseExperienceDraftsWithError(response, ExtractJSONWithError)
	if drafts == nil {
		strategy := "regeneration"
		if !truncated {
			if attemptNumber >= 3 {
				strategy = "strict_fix"
			} else {
				strategy = "fix"
			}
		}
		logger.Warn(logComponent).
			Str("strategy", strategy).
			Msg("[SkillExperienceOptimizer] retry also failed, giving up")
		return nil, response, fmt.Errorf("重试 (%s) 失败: %s", strategy, lastError)
	}

	logger.Info(logComponent).
		Int("patch_count", len(drafts)).
		Msg("[SkillExperienceOptimizer] retry succeeded")
	return drafts, response, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// buildEvolutionContext 从 onlineContexts 查找 EvolutionContext，不存在时抛异常。
//
// 对齐 Python: SkillExperienceOptimizer._build_evolution_context(skill_name, operator, skill_signals)
//
//	online_ctx = self._online_contexts.get(skill_name)
//	if online_ctx is not None: return online_ctx
//	raise build_error(...)
func (o *SkillExperienceOptimizer) buildEvolutionContext(skillName string, op operator.Operator, skillSignals []*signal.EvolutionSignal) (*experience.EvolutionContext, error) {
	onlineCtx := o.onlineContexts[skillName]
	if onlineCtx != nil {
		return onlineCtx, nil
	}
	return nil, exception.BuildError(
		exception.NewStatusCode("TOOLCHAIN_AGENT_PARAM_ERROR", 170000, ""),
		exception.WithMsg(fmt.Sprintf(
			"online_contexts missing entry for skill %s; SkillExperienceOptimizer requires EvolutionContext",
			skillName,
		)),
	)
}

// generateDraftsWithRetries 调用 LLM + 解析草稿 + 重试循环（最多3次）。
//
// 对齐 Python: SkillExperienceOptimizer._generate_drafts_with_retries(prompt, retry_prompt)
func (o *SkillExperienceOptimizer) generateDraftsWithRetries(ctx context.Context, prompt string, retryPrompt string) ([]ParsedExperienceDraft, error) {
	raw, promptUsed, err := llm_resilience.InvokeTextWithRetryAndPrompt(
		ctx,
		o.llm,
		o.model,
		prompt,
		o.generateRecordsLLMPolicy,
		llm_resilience.WithRetryPrompt(retryPrompt),
	)
	if err != nil {
		return nil, err
	}

	drafts, lastError := ParseExperienceDraftsWithError(raw, ExtractJSONWithError)
	if drafts != nil {
		return drafts, nil
	}

	lastRaw := raw
	for attempt := 2; attempt <= 3; attempt++ {
		logger.Warn(logComponent).
			Int("attempt", attempt).
			Msg("[SkillExperienceOptimizer] parse failed, repair attempt")

		repaired, retryRaw, retryErr := o.RetryParseDrafts(
			ctx,
			lastRaw,
			promptUsed,
			attempt,
			lastError,
		)
		if retryErr == nil && repaired != nil {
			return repaired, nil
		}
		if retryRaw != "" {
			lastRaw = retryRaw
			_, lastError = ParseExperienceDraftsWithError(retryRaw, ExtractJSONWithError)
		}
	}

	return nil, fmt.Errorf("SkillExperienceOptimizer 响应无法解析")
}

// buildConversationSnippet 构建紧凑对话片段用于 LLM 提示词上下文。
//
// 对齐 Python: _build_conversation_snippet(messages, max_messages, content_preview_chars, language)
func buildConversationSnippet(messages []map[string]any, maxMessages int, contentPreviewChars int, language string) string {
	if len(messages) == 0 {
		return ""
	}

	recent := messages
	if len(messages) > maxMessages {
		recent = messages[len(messages)-maxMessages:]
	}

	var lines []string
	for i, msg := range recent {
		role := getStrFromMap(msg, "role", "unknown")
		text := extractTextFromMessage(msg)
		if strings.TrimSpace(text) == "" {
			if language == "cn" {
				text = "(无文本)"
			} else {
				text = "(No text)"
			}
		}

		budget := contentPreviewChars
		if i >= len(recent)-5 {
			budget = contentPreviewChars * 2
		}

		if len(text) > budget {
			origLen := len(text)
			text = text[:budget]
			if language == "cn" {
				text += fmt.Sprintf("\n... [已截断，原始长度 %d 字符]", origLen)
			} else {
				text += fmt.Sprintf("\n... [truncated, original %d chars]", origLen)
			}
		}

		// 对齐 Python: tool_calls handling
		toolCalls, hasToolCalls := msg["tool_calls"]
		if role == "assistant" && hasToolCalls {
			var names []string
			if tcList, ok := toolCalls.([]any); ok {
				for _, tc := range tcList {
					if tcDict, ok := tc.(map[string]any); ok {
						names = append(names, getStrFromMap(tcDict, "name", ""))
					}
				}
			}
			prefix := fmt.Sprintf("[assistant] (tool_calls: %s)\n  ", strings.Join(names, ", "))
			lines = append(lines, prefix+text)
		} else {
			lines = append(lines, fmt.Sprintf("[%s] %s", role, text))
		}
	}
	return strings.Join(lines, "\n")
}

// extractTextFromMessage 从消息中提取文本内容。
//
// 对齐 Python: _extract_text(message)
func extractTextFromMessage(msg map[string]any) string {
	content := msg["content"]
	if content == nil {
		return ""
	}
	if s, ok := content.(string); ok {
		return s
	}
	if list, ok := content.([]any); ok {
		var parts []string
		for _, block := range list {
			if dict, ok := block.(map[string]any); ok {
				parts = append(parts, getStrFromMap(dict, "text", ""))
			} else if s, ok := block.(string); ok {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, "\n")
	}
	return fmt.Sprintf("%v", content)
}

// summarizeSkillContent 将大型 SKILL.md 内容压缩为 LLM 提示词可用的摘要。
//
// 对齐 Python: _summarize_skill_content(raw, max_chars)
func summarizeSkillContent(raw string) string {
	return summarizeSkillContentWithMax(raw, SkillContentMaxChars)
}

// summarizeSkillContentWithMax 同 summarizeSkillContent 但可指定截断上限。
func summarizeSkillContentWithMax(raw string, maxChars int) string {
	if len(raw) <= maxChars {
		return raw
	}
	sections := splitIntoSections(raw)
	if len(sections) == 0 {
		return raw[:maxChars] + fmt.Sprintf("\n... [已截断，原始共 %d 字符]", len(raw))
	}
	parts := []string{sections[0]}
	if len(sections) > 1 {
		parts = append(parts, "\n[以下章节仅保留标题与开头摘要，完整内容已省略]\n")
		for _, section := range sections[1:] {
			parts = append(parts, previewSection(section))
		}
	}
	summary := strings.Join(parts, "\n")
	if len(summary) > maxChars {
		summary = summary[:maxChars] + fmt.Sprintf("\n... [已截断，原始 SKILL.md 共 %d 字符]", len(raw))
	}
	return summary
}

// splitIntoSections 按 Markdown 章节标题拆分文本。
//
// 对齐 Python: _split_into_sections(text)
func splitIntoSections(text string) []string {
	lines := strings.Split(text, "\n")
	var sections []string
	var current []string

	for _, line := range lines {
		if headingRE.MatchString(line) && len(current) > 0 {
			sections = append(sections, strings.Join(current, "\n"))
			current = nil
		}
		current = append(current, line)
	}
	if len(current) > 0 {
		sections = append(sections, strings.Join(current, "\n"))
	}
	return sections
}

// previewSection 返回章节标题 + 前 previewChars 字符正文。
//
// 对齐 Python: _preview_section(section, preview_chars)
func previewSection(section string) string {
	lines := strings.Split(section, "\n")
	heading := lines[0]
	body := strings.TrimSpace(strings.Join(lines[1:], "\n"))
	if body == "" {
		return heading
	}
	if len(body) <= SectionPreviewChars {
		return section
	}
	return heading + "\n" + body[:SectionPreviewChars] + "..."
}

// buildExistingSummary 从已有记录构建摘要字符串。
//
// 对齐 Python: _build_existing_summary(records, label)
func buildExistingSummary(records []checkpointing.EvolutionRecord, label string) string {
	if len(records) == 0 {
		return ""
	}
	var lines []string
	for _, record := range records {
		prefix := ""
		if label != "" {
			prefix = fmt.Sprintf("[%s] ", label)
		}
		lines = append(lines, fmt.Sprintf("- %s[%s] [%s] %s", prefix, record.ID, record.Change.Section, record.Change.Content))
	}
	return strings.Join(lines, "\n")
}

// limitSummaryLines 限制摘要行数。
//
// 对齐 Python: _limit_summary_lines(summary, max_lines)
func limitSummaryLines(summary string, maxLines int) string {
	if summary == "" || maxLines <= 0 {
		return ""
	}
	lines := strings.Split(summary, "\n")
	if len(lines) <= maxLines {
		return summary
	}
	return strings.Join(lines[:maxLines], "\n")
}

// buildContext 从信号构建简洁上下文字符串。
//
// 对齐 Python: _build_context(signals, max_chars)
func buildContext(signals []*signal.EvolutionSignal) string {
	if len(signals) == 0 {
		return ""
	}
	perSignal := max(80, ContextMaxChars/len(signals))
	var parts []string
	for _, sig := range signals {
		excerpt := strings.TrimSpace(sig.Excerpt)
		if len(excerpt) > perSignal {
			excerpt = excerpt[:perSignal] + "..."
		}
		parts = append(parts, fmt.Sprintf("[%s] %s", sig.SignalType, excerpt))
	}
	return strings.Join(parts, " | ")
}

// buildContextFromValues 从值类型信号构建简洁上下文字符串（EvolutionContext.Signals 是 []signal.EvolutionSignal）。
func buildContextFromValues(signals []signal.EvolutionSignal) string {
	if len(signals) == 0 {
		return ""
	}
	perSignal := max(80, ContextMaxChars/len(signals))
	var parts []string
	for _, sig := range signals {
		excerpt := strings.TrimSpace(sig.Excerpt)
		if len(excerpt) > perSignal {
			excerpt = excerpt[:perSignal] + "..."
		}
		parts = append(parts, fmt.Sprintf("[%s] %s", sig.SignalType, excerpt))
	}
	return strings.Join(parts, " | ")
}

// formatTemplate 用 strings.ReplaceAll 替换模板占位符。
func formatTemplate(template string, pairs ...string) string {
	result := template
	for i := 0; i < len(pairs); i += 2 {
		result = strings.ReplaceAll(result, "{"+pairs[i]+"}", pairs[i+1])
	}
	return result
}

// signalListToDicts 将信号列表转为 []map[string]any（对齐 Python signal.to_dict()）。
func signalListToDicts(signals []*signal.EvolutionSignal) []map[string]any {
	var dicts []map[string]any
	for _, s := range signals {
		dicts = append(dicts, s.ToDict())
	}
	return dicts
}

// signalValuesToDicts 将值类型信号列表转为 []map[string]any。
func signalValuesToDicts(signals []signal.EvolutionSignal) []map[string]any {
	var dicts []map[string]any
	for i := range signals {
		dicts = append(dicts, signals[i].ToDict())
	}
	return dicts
}

// signalListToJSONCompact 将信号列表转为紧凑 JSON（不带缩进，对齐 retry_prompt 中的用法）。
func signalListToJSONCompact(signals []*signal.EvolutionSignal) []byte {
	dicts := signalListToDicts(signals)
	data, _ := json.Marshal(dicts)
	return data
}

// signalValuesToJSONCompact 将值类型信号列表转为紧凑 JSON。
func signalValuesToJSONCompact(signals []signal.EvolutionSignal) []byte {
	dicts := signalValuesToDicts(signals)
	data, _ := json.Marshal(dicts)
	return data
}

// orDefault 返回 value 如果非空，否则返回 defaults 中的第一个匹配语言的值。
func orDefault(value string, defaults ...string) string {
	if value != "" {
		return value
	}
	if len(defaults) > 0 {
		return defaults[0]
	}
	return ""
}

// truncateOrDefault 截断字符串或返回语言默认值。
func truncateOrDefault(s string, maxLen int, cnDefault string, enDefault string) string {
	if s == "" {
		if true { // 由调用方决定语言
			return cnDefault
		}
		return enDefault
	}
	if len(s) > maxLen {
		return s[:maxLen]
	}
	return s
}

// truncateString 截断字符串到指定长度。
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}

// ptrToStr 安全地将 *string 转为字符串。
func ptrToStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// getStrFromMap 从 map[string]any 中安全获取字符串。
func getStrFromMap(data map[string]any, key string, defaultVal string) string {
	if v, ok := data[key]; ok && v != nil {
		if s, ok := v.(string); ok {
			return s
		}
		return fmt.Sprintf("%v", v)
	}
	return defaultVal
}

// max 返回两个整数中的较大值。
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// timeNow 返回当前 UTC 时间字符串。
func timeNow() string {
	return time.Now().UTC().Format(time.RFC3339)
}
