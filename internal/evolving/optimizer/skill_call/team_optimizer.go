package skill_call

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
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
	"github.com/uapclaw/uapclaw-go/internal/evolving/trajectory"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TeamSkillExperienceOptimizer 团队技能经验优化器。
//
// 对应 Python: TeamSkillExperienceOptimizer
type TeamSkillExperienceOptimizer struct {
	SkillExperienceOptimizerBase
	// debugDir 调试输出目录（可选）
	debugDir string
	// recordLLMPolicy 团队记录生成 LLM 调用策略
	recordLLMPolicy llm_resilience.LLMInvokePolicy
	// evolutionStore 演进存储接口（可选，用于加载技能内容和已有演进）
	evolutionStore EvolutionStore
}

// EvolutionStore 接口 — 用于 loadSkillContent / loadExistingEvolutionsSummary。
// 对齐 Python: EvolutionStore；签名对齐 checkpointing.EvolutionStore 结构体方法。
type EvolutionStore interface {
	// ReadSkillContent 读取技能内容
	ReadSkillContent(ctx context.Context, skillName string) (string, error)
	// LoadFullEvolutionLog 加载完整演进日志
	LoadFullEvolutionLog(ctx context.Context, skillName string) *checkpointing.EvolutionLog
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// TeamSkillOptimizer 团队技能优化器类型别名，对齐 Python: TeamSkillOptimizer = TeamSkillExperienceOptimizer
type TeamSkillOptimizer = TeamSkillExperienceOptimizer

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTeamSkillExperienceOptimizer 创建 TeamSkillExperienceOptimizer 实例。
//
// 对齐 Python:
//
//	TeamSkillExperienceOptimizer(llm, model, language, debug_dir, record_llm_policy, evolution_store)
func NewTeamSkillExperienceOptimizer(llmModel *llm.Model, model string, language string, debugDir string, recordLLMPolicy llm_resilience.LLMInvokePolicy, evolutionStore EvolutionStore) *TeamSkillExperienceOptimizer {
	return &TeamSkillExperienceOptimizer{
		SkillExperienceOptimizerBase: SkillExperienceOptimizerBase{
			llm:            llmModel,
			model:          model,
			language:       language,
			onlineContexts: map[string]*experience.EvolutionContext{},
		},
		debugDir:        debugDir,
		recordLLMPolicy: recordLLMPolicy,
		evolutionStore:  evolutionStore,
	}
}

// Bind 由 SkillExperienceOptimizerBase.Bind 继承，无需重复定义。
// 基类已实现 online_contexts 提取 + BaseOptimizerMixin.Bind 调用。

// Backward 反向传播：使用 Trajectory 和信号。
//
// 对齐 Python: TeamSkillExperienceOptimizer._backward(signals)
func (o *TeamSkillExperienceOptimizer) Backward(ctx context.Context, signals []*signal.EvolutionSignal) error {
	o.ValidateParameters()
	selected := o.SelectSignals(signals)
	o.SetSelectedSignals(selected)

	trajectories := o.GetTrajectories()
	defaultTrajectory := &trajectory.Trajectory{
		ExecutionID: "team-skill-evolution",
		SessionID:   "team-skill-evolution",
		Source:      "online",
	}
	if len(trajectories) > 0 {
		defaultTrajectory = trajectories[len(trajectories)-1]
	}

	for opID, op := range o.BaseOptimizerMixin.Operators() {
		skillName := removeSkillPrefix(opID)
		var skillSignals []*signal.EvolutionSignal
		for _, s := range selected {
			if (s.SkillName != nil && *s.SkillName == skillName) || s.SkillName == nil {
				skillSignals = append(skillSignals, s)
			}
		}
		if len(skillSignals) == 0 {
			continue
		}

		evoCtx, err := o.buildEvolutionContext(skillName, op, skillSignals, defaultTrajectory)
		if err != nil {
			logger.Error(logComponent).
				Str("method", "Backward").
				Str("skill_name", skillName).
				Err(err).
				Msg("[TeamSkillOptimizer] 上下文构建失败")
			continue
		}

		generated, err := o.GenerateRecords(ctx, evoCtx)
		if err != nil {
			logger.Error(logComponent).
				Str("method", "Backward").
				Str("skill_name", skillName).
				Err(err).
				Msg("[TeamSkillOptimizer] GenerateRecords 失败")
			continue
		}
		if len(generated) == 0 {
			logger.Info(logComponent).
				Str("skill_name", skillName).
				Msg("[TeamSkillOptimizer] no records generated for skill")
			continue
		}

		param := o.BaseOptimizerMixin.Parameters()[opID]
		existingAny := param.GetGradient(schema.ExperiencesTarget)
		var existing []checkpointing.EvolutionRecord
		if existingAny != nil {
			if eList, ok := existingAny.([]checkpointing.EvolutionRecord); ok {
				existing = eList
			}
		}
		param.SetGradient(schema.ExperiencesTarget, append(existing, generated...))

		logger.Info(logComponent).
			Str("skill_name", skillName).
			Int("record_count", len(generated)).
			Msg("[TeamSkillOptimizer] generated record(s) for skill")
	}
	return nil
}

// Step 生成更新映射。
//
// 对齐 Python: TeamSkillExperienceOptimizer._step()
func (o *TeamSkillExperienceOptimizer) Step() map[schema.UpdateKey]any {
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

// SelectSignals 选择此优化器可消费的信号。
func (o *TeamSkillExperienceOptimizer) SelectSignals(signals []*signal.EvolutionSignal) []*signal.EvolutionSignal {
	result := make([]*signal.EvolutionSignal, len(signals))
	copy(result, signals)
	return result
}

// GenerateRecords 双路径生成演进记录。
//
// 对齐 Python: TeamSkillExperienceOptimizer.generate_records(ctx)
func (o *TeamSkillExperienceOptimizer) GenerateRecords(ctx context.Context, evoCtx *experience.EvolutionContext) ([]checkpointing.EvolutionRecord, error) {
	if len(evoCtx.Signals) == 0 {
		return nil, nil
	}

	traj := evoCtx.Trajectory
	if traj == nil {
		traj = &trajectory.Trajectory{
			ExecutionID: "team-skill-evolution",
			SessionID:   "team-skill-evolution",
			Source:      "online",
		}
	}

	// 对齐 Python: if any(not hasattr(step, "kind") for step in getattr(trajectory, "steps", [])):
	// 双路径：逐信号 patch
	hasKindOnAll := true
	for _, step := range traj.Steps {
		if step.Kind == "" {
			hasKindOnAll = false
			break
		}
	}
	if !hasKindOnAll || len(traj.Steps) == 0 {
		var generated []checkpointing.EvolutionRecord
		for _, sig := range evoCtx.Signals {
			var record *checkpointing.EvolutionRecord
			var err error
			if sig.SignalType == "user_intent" {
				record, err = o.GenerateUserPatch(ctx, traj, evoCtx.SkillName, orString(sig.Excerpt, evoCtx.UserQuery))
			} else {
				skillContent := orString(signal.GetTeamSignalSkillContent(&sig), evoCtx.SkillContent)
				trajectoryIssues := signal.GetTeamTrajectoryIssues(&sig)
				record, err = o.GenerateTrajectoryPatch(ctx, traj, evoCtx.SkillName, skillContent, trajectoryIssues)
			}
			if err != nil {
				logger.Warn(logComponent).
					Str("signal_type", sig.SignalType).
					Err(err).
					Msg("[TeamSkillOptimizer] per-signal patch failed")
				continue
			}
			if record != nil {
				generated = append(generated, *record)
			}
		}
		return generated, nil
	}

	// 聚合 LLM 流路径
	trajectorySummary := signal.BuildTeamTrajectorySummary(traj)
	promptTemplate := TeamExperienceGeneratePrompt[o.language]
	if promptTemplate == "" {
		promptTemplate = TeamExperienceGeneratePromptEN
	}

	signalsJSON, _ := json.Marshal(signalValuesToDicts(evoCtx.Signals))
	currentSkillContent := summarizeSkillContentTeam(evoCtx.SkillContent)
	descSummary := summarizeExistingEvolutions(evoCtx.ExistingDescRecords, o.language)
	bodySummary := summarizeExistingEvolutions(evoCtx.ExistingBodyRecords, o.language)
	scriptSummary := summarizeExistingEvolutions(evoCtx.ExistingScriptRecords, o.language)

	prompt := formatTemplate(promptTemplate,
		"skill_content", orString(currentSkillContent, langDefault("无", "None", o.language)),
		"trajectory_summary", orString(trajectorySummary, langDefault("无轨迹摘要", "No trajectory summary", o.language)),
		"signals_json", string(signalsJSON),
		"existing_desc_summary", descSummary,
		"existing_body_summary", bodySummary,
		"existing_script_summary", scriptSummary,
		"user_query", orString(evoCtx.UserQuery, langDefault("无", "None", o.language)),
	)

	retryPrompt := formatTemplate(promptTemplate,
		"skill_content", orString(summarizeSkillContentTeamWithMax(evoCtx.SkillContent, 2500), langDefault("无", "None", o.language)),
		"trajectory_summary", orString(truncateString(trajectorySummary, PatchRetryTrajectoryChars), langDefault("无轨迹摘要", "No trajectory summary", o.language)),
		"signals_json", string(signalValuesToJSONCompact(evoCtx.Signals)),
		"existing_desc_summary", shortenExistingEvolutionsSummary(descSummary, 2),
		"existing_body_summary", shortenExistingEvolutionsSummary(bodySummary, 2),
		"existing_script_summary", shortenExistingEvolutionsSummary(scriptSummary, 1),
		"user_query", truncateOrDefault(evoCtx.UserQuery, UserIntentRetryChars, "无", "None"),
	)

	logger.Info(logComponent).
		Str("skill_name", evoCtx.SkillName).
		Msg("[TeamSkillOptimizer] calling aggregated LLM flow")

	drafts, err := o.generateDraftsWithRetries(ctx, prompt, retryPrompt)
	if err != nil {
		logger.Error(logComponent).
			Str("skill_name", evoCtx.SkillName).
			Err(err).
			Msg("[TeamSkillOptimizer] aggregated LLM call failed")
		return nil, err
	}

	sources := make(map[string]bool)
	for _, sig := range evoCtx.Signals {
		sources[sig.SignalType] = true
	}
	source := "team_skill_mixed"
	if len(sources) == 1 {
		for k := range sources {
			source = k
		}
	}
	initialScore := TeamInitialScoreBySignal[source]
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
				Msg("[TeamSkillOptimizer] aggregated flow skipped record")
			continue
		}
		if strings.TrimSpace(patch.Content) == "" {
			logger.Info(logComponent).
				Msg("[TeamSkillOptimizer] aggregated flow returned empty content, skipping")
			continue
		}
		isScript := patch.Target == signal.EvolutionTargetScript
		if isScript && len(scriptRecords) >= 1 {
			continue
		}
		if !isScript && len(textRecords) >= 2 {
			continue
		}
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
			Msg("[TeamSkillOptimizer] aggregated record")
	}
	return append(textRecords, scriptRecords...), nil
}

// GenerateUserPatch 生成用户意图 patch。
//
// 对齐 Python: TeamSkillExperienceOptimizer.generate_user_patch(trajectory, skill_name, user_intent)
func (o *TeamSkillExperienceOptimizer) GenerateUserPatch(ctx context.Context, traj *trajectory.Trajectory, skillName string, userIntent string) (*checkpointing.EvolutionRecord, error) {
	description := "team-skill"
	rolesSummary := "N/A"
	workflowSummary := "N/A"

	summary := signal.BuildTeamTrajectorySummary(traj)
	if strings.Contains(summary, "spawn_member") {
		roleMentions := regexp.MustCompile(`role[_-]?([a-z]+)`).FindAllStringSubmatch(summary, -1)
		if len(roleMentions) > 0 {
			var roles []string
			seen := make(map[string]bool)
			for _, m := range roleMentions {
				if len(m) > 1 && !seen[m[1]] {
					roles = append(roles, m[1])
					seen[m[1]] = true
				}
			}
			if len(roles) > 5 {
				roles = roles[:5]
			}
			rolesSummary = strings.Join(roles, ", ")
		}
	}
	if strings.Contains(strings.ToLower(summary), "workflow") || strings.Contains(strings.ToLower(summary), "mermaid") {
		workflowSummary = "Present in trajectory"
	}

	skillContent := summarizeSkillContentTeamFallback(skillName, o.language)
	existingEvolutions := langDefault("无已有演进经验", "No existing evolution records", o.language)

	promptTemplate := UserPatchPrompt[o.language]
	if promptTemplate == "" {
		promptTemplate = UserPatchPromptEN
	}

	prompt := formatTemplate(promptTemplate,
		"skill_name", skillName,
		"description", description,
		"roles_summary", rolesSummary,
		"workflow_summary", workflowSummary,
		"skill_content", skillContent,
		"existing_evolutions", existingEvolutions,
		"user_intent", userIntent,
	)

	retryPrompt := formatTemplate(promptTemplate,
		"skill_name", skillName,
		"description", description,
		"roles_summary", truncateString(rolesSummary, SummaryRetryChars),
		"workflow_summary", truncateString(workflowSummary, SummaryRetryChars),
		"skill_content", summarizeSkillContentTeamWithMax(skillContent, 2500),
		"existing_evolutions", shortenExistingEvolutionsSummary(existingEvolutions, 2),
		"user_intent", truncateString(userIntent, UserIntentRetryChars),
	)

	t0 := time.Now()
	raw, err := o.callLLM(ctx, prompt, retryPrompt, &o.recordLLMPolicy, func(text string) bool {
		return signal.ParseTeamModelJSON(text) != nil
	})
	if err != nil {
		elapsed := time.Since(t0).Seconds()
		logger.Warn(logComponent).
			Float64("elapsed_sec", elapsed).
			Err(err).
			Msg("[TeamSkillOptimizer] user_patch: LLM generation failed")
		return nil, err
	}

	parsed, lastError := parsePatchResponse(raw)
	if parsed == nil {
		return nil, fmt.Errorf("TeamSkillExperienceOptimizer response could not be parsed: %s", lastError)
	}

	elapsed := time.Since(t0).Seconds()
	needPatch, _ := parsed["need_patch"].(bool)
	action := getStrFromAny(parsed["action"], "")
	if !needPatch || action == "skip" {
		reason := getStrFromAny(parsed["reason"], "N/A")
		logger.Info(logComponent).
			Str("reason", reason).
			Msg("[TeamSkillOptimizer] user_patch: no patch needed")
		return nil, nil
	}

	section := getStrFromAny(parsed["section"], "Instructions")
	content := getStrFromAny(parsed["content"], "")
	if strings.TrimSpace(content) == "" {
		return nil, fmt.Errorf("TeamSkill user patch response contained empty content")
	}

	logger.Info(logComponent).
		Str("section", section).
		Int("content_len", len(content)).
		Float64("elapsed_sec", elapsed).
		Msg("[TeamSkillOptimizer] user_patch")

	summaryPtr := NormalizeSummary(parsed["summary"])
	return checkpointing.MakeEvolutionRecord(
		"team_skill_user_patch",
		fmt.Sprintf("User intent: %s", truncateString(userIntent, 200)),
		checkpointing.EvolutionPatch{
			Section: section,
			Action:  "append",
			Content: content,
			Target:  signal.EvolutionTargetBody,
		},
		0.6,
		nil,
		summaryPtr,
	), nil
}

// GenerateTrajectoryPatch 生成轨迹 patch。
//
// 对齐 Python: TeamSkillExperienceOptimizer.generate_trajectory_patch(...)
func (o *TeamSkillExperienceOptimizer) GenerateTrajectoryPatch(ctx context.Context, traj *trajectory.Trajectory, skillName string, currentSkillContent string, trajectoryIssues []map[string]string) (*checkpointing.EvolutionRecord, error) {
	summary := signal.BuildTeamTrajectorySummary(traj)
	issuesText := "N/A"
	if len(trajectoryIssues) > 0 {
		data, _ := json.Marshal(trajectoryIssues)
		issuesText = string(data)
	}

	existingEvolutions := langDefault("无已有演进经验", "No existing evolution records", o.language)

	logger.Info(logComponent).
		Str("skill_name", skillName).
		Int("summary_len", len(summary)).
		Int("content_len", len(currentSkillContent)).
		Int("issues_len", len(issuesText)).
		Msg("[TeamSkillOptimizer] trajectory_patch")

	promptTemplate := TrajectoryPatchPrompt[o.language]
	if promptTemplate == "" {
		promptTemplate = TrajectoryPatchPromptEN
	}

	prompt := formatTemplate(promptTemplate,
		"skill_content", truncateString(currentSkillContent, 15000),
		"existing_evolutions", existingEvolutions,
		"trajectory_summary", summary,
		"trajectory_issues", truncateString(issuesText, 5000),
	)

	retryPrompt := formatTemplate(promptTemplate,
		"skill_content", truncateString(currentSkillContent, PatchRetrySkillContentChars),
		"existing_evolutions", shortenExistingEvolutionsSummary(existingEvolutions, 2),
		"trajectory_summary", truncateString(summary, PatchRetryTrajectoryChars),
		"trajectory_issues", truncateString(issuesText, TrajectoryIssuesRetryChars),
	)

	t0 := time.Now()
	raw, err := o.callLLM(ctx, prompt, retryPrompt, &o.recordLLMPolicy, func(text string) bool {
		return signal.ParseTeamModelJSON(text) != nil
	})
	if err != nil {
		elapsed := time.Since(t0).Seconds()
		logger.Warn(logComponent).
			Float64("elapsed_sec", elapsed).
			Err(err).
			Msg("[TeamSkillOptimizer] trajectory_patch: LLM generation failed")
		return nil, err
	}

	parsed, lastError := parsePatchResponse(raw)
	if parsed == nil {
		return nil, fmt.Errorf("TeamSkillExperienceOptimizer response could not be parsed: %s", lastError)
	}

	elapsed := time.Since(t0).Seconds()
	needPatch, _ := parsed["need_patch"].(bool)
	if !needPatch {
		reason := getStrFromAny(parsed["reason"], "N/A")
		logger.Info(logComponent).
			Str("reason", reason).
			Msg("[TeamSkillOptimizer] trajectory_patch: no patch needed")
		return nil, nil
	}

	section := getStrFromAny(parsed["section"], "Workflow")
	content := getStrFromAny(parsed["content"], "")
	if strings.TrimSpace(content) == "" {
		return nil, fmt.Errorf("TeamSkill trajectory patch response contained empty content")
	}

	logger.Info(logComponent).
		Str("section", section).
		Int("content_len", len(content)).
		Float64("elapsed_sec", elapsed).
		Msg("[TeamSkillOptimizer] trajectory_patch")

	summaryPtr := NormalizeSummary(parsed["summary"])
	return checkpointing.MakeEvolutionRecord(
		"team_skill_trajectory_patch",
		fmt.Sprintf("Trajectory issues: %s", truncateString(issuesText, 200)),
		checkpointing.EvolutionPatch{
			Section: section,
			Action:  "append",
			Content: content,
			Target:  signal.EvolutionTargetBody,
		},
		0.6,
		nil,
		summaryPtr,
	), nil
}

// RegenerateBody 重写 SKILL.md body。
//
// 对齐 Python: TeamSkillExperienceOptimizer.regenerate_body(...)
func (o *TeamSkillExperienceOptimizer) RegenerateBody(ctx context.Context, skillName string, currentBody string, evolutionRecords []checkpointing.EvolutionRecord, userIntent string) (string, error) {
	var evoLines []string
	for i, r := range evolutionRecords {
		if i >= 20 {
			break
		}
		evoLines = append(evoLines, fmt.Sprintf("- [%s] %s: %s", r.ID, r.Change.Section, truncateString(r.Change.Content, 200)))
	}
	evoSummary := strings.Join(evoLines, "\n")
	if evoSummary == "" {
		evoSummary = "(no evolutions)"
	}
	intentSection := ""
	if userIntent != "" {
		intentSection = fmt.Sprintf("\n\n## 用户意图\n%s", userIntent)
	}

	prompt := fmt.Sprintf(
		"你是多角色协作 Skill 文档重写专家。请根据当前 Team Skill body 和积累的演进经验，重新编写一份更优的 body。\n\n"+
			"## 当前 Team Skill: %s\n\n"+
			"```markdown\n%s\n```\n\n"+
			"## 积累的演进经验\n%s"+
			"%s\n\n"+
			"## 要求\n"+
			"1. 保留 YAML frontmatter 不动（不要输出 frontmatter）\n"+
			"2. 将有价值的演进经验融入 body 正文\n"+
			"3. 保持 roles 子文件的引用结构\n"+
			"4. 精简冗余内容，保持结构清晰\n"+
			"5. 直接输出 Markdown body，不要加任何解释\n",
		skillName,
		truncateString(currentBody, 8000),
		evoSummary,
		intentSection,
	)

	raw, err := o.callLLM(ctx, prompt, "", nil, nil)
	if err != nil {
		return "", err
	}
	body := strings.TrimSpace(raw)
	if len(body) < 50 {
		return "", nil
	}
	return body, nil
}

// callLLM 调用 LLM，有 policy 时走 InvokeTextWithRetry，无 policy 时直接 invoke。
//
// 对齐 Python: TeamSkillExperienceOptimizer._call_llm(prompt, retry_prompt, policy, is_result_usable)
func (o *TeamSkillExperienceOptimizer) callLLM(ctx context.Context, prompt string, retryPrompt string, policy *llm_resilience.LLMInvokePolicy, isResultUsable func(string) bool) (string, error) {
	logger.Info(logComponent).
		Str("model", o.model).
		Int("prompt_len", len(prompt)).
		Msg("[TeamSkillOptimizer] LLM call start")

	t0 := time.Now()
	var result string
	var err error

	if policy == nil {
		// 对齐 Python: 无 policy → 走 InvokeTextWithRetry 单次尝试
		singlePolicy := llm_resilience.LLMInvokePolicy{
			MaxAttempts:        1,
			AttemptTimeoutSecs: 120,
			TotalBudgetSecs:    120,
		}
		result, err = llm_resilience.InvokeTextWithRetry(ctx, o.llm, o.model, prompt, singlePolicy)
		if err != nil {
			logger.Error(logComponent).Err(err).Msg("[TeamSkillOptimizer] LLM call failed")
			return "", err
		}
	} else {
		opts := []llm_resilience.InvokeRetryOption{}
		if retryPrompt != "" {
			opts = append(opts, llm_resilience.WithRetryPrompt(retryPrompt))
		}
		if isResultUsable != nil {
			opts = append(opts, llm_resilience.WithIsResultUsable(isResultUsable))
		}
		result, err = llm_resilience.InvokeTextWithRetry(ctx, o.llm, o.model, prompt, *policy, opts...)
		if err != nil {
			logger.Error(logComponent).Err(err).Msg("[TeamSkillOptimizer] LLM call failed")
			return "", err
		}
	}

	elapsed := time.Since(t0).Seconds()
	logger.Info(logComponent).
		Float64("elapsed_sec", elapsed).
		Int("response_len", len(result)).
		Msg("[TeamSkillOptimizer] LLM call done")

	return result, nil
}

// RetryParseDrafts 重试解析：截断→重新生成 / 格式错误→TEAM_JSON_FIX / attempt≥3→TEAM_JSON_FIX_STRICT。
//
// 对齐 Python: TeamSkillExperienceOptimizer.retry_parse_drafts(...)
func (o *TeamSkillExperienceOptimizer) RetryParseDrafts(ctx context.Context, brokenRaw string, originalPrompt string, attemptNumber int, parseError string) ([]ParsedExperienceDraft, string, error) {
	truncated := LooksTruncated(brokenRaw)
	var retryPrompt string

	if truncated {
		if attemptNumber >= 3 {
			logger.Warn(logComponent).
				Msg("[TeamSkillOptimizer] output still truncated on attempt 3, giving up")
			return nil, brokenRaw, fmt.Errorf("output truncated on attempt 3")
		}
		logger.Warn(logComponent).
			Msg("[TeamSkillOptimizer] output appears truncated, retrying full regeneration")
		retryPrompt = originalPrompt
	} else if attemptNumber >= 3 {
		logger.Warn(logComponent).
			Int("attempt", attemptNumber).
			Msg("[TeamSkillOptimizer] JSON malformed, using strict fix prompt")
		errorDetail := parseError
		if errorDetail == "" {
			errorDetail = "无法解析为合法 JSON"
		}
		retryPrompt = formatTemplate(TeamJSONFixPromptStrict,
			"parse_error", errorDetail,
			"broken_preview", truncateString(brokenRaw, 500),
		)
	} else {
		logger.Warn(logComponent).
			Str("preview", truncateString(brokenRaw, 200)).
			Msg("[TeamSkillOptimizer] JSON malformed, requesting fix")
		errorDetail := parseError
		if errorDetail == "" {
			errorDetail = "JSON 解析失败"
		}
		retryPrompt = formatTemplate(TeamJSONFixPrompt,
			"parse_error", errorDetail,
			"broken_output", brokenRaw,
		)
	}

	response, err := llm_resilience.InvokeTextWithRetry(
		ctx,
		o.llm,
		o.model,
		retryPrompt,
		llm_resilience.LLMInvokePolicy{
			AttemptTimeoutSecs: float64(TeamRetryParseTimeoutSecs),
			TotalBudgetSecs:    float64(TeamRetryParseTimeoutSecs) * 2,
			MaxAttempts:        1,
		},
		llm_resilience.WithTemperature(0.1),
	)
	if err != nil {
		logger.Error(logComponent).Err(err).Msg("[TeamSkillOptimizer] retry LLM call failed")
		return nil, "", err
	}

	drafts, _ := ParseExperienceDraftsWithError(response, teamExtractJSONWithError)
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
			Msg("[TeamSkillOptimizer] retry also failed, giving up")
		return nil, response, fmt.Errorf("retry (%s) failed", strategy)
	}

	logger.Info(logComponent).
		Int("patch_count", len(drafts)).
		Msg("[TeamSkillOptimizer] retry succeeded")
	return drafts, response, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// buildEvolutionContext 从 onlineContexts 查找，trajectory 为 nil 时填充 default_trajectory。
//
// 对齐 Python: TeamSkillExperienceOptimizer._build_evolution_context(skill_name, operator, skill_signals, default_trajectory)
func (o *TeamSkillExperienceOptimizer) buildEvolutionContext(skillName string, op operator.Operator, skillSignals []*signal.EvolutionSignal, defaultTrajectory *trajectory.Trajectory) (*experience.EvolutionContext, error) {
	onlineCtx := o.onlineContexts[skillName]
	if onlineCtx != nil {
		if onlineCtx.Trajectory == nil {
			// 对齐 Python: trajectory 为 nil 时填充 default_trajectory 返回副本
			return &experience.EvolutionContext{
				SkillName:             onlineCtx.SkillName,
				Signals:               onlineCtx.Signals,
				Messages:              onlineCtx.Messages,
				UserQuery:             onlineCtx.UserQuery,
				SkillContent:          onlineCtx.SkillContent,
				ExistingDescRecords:   onlineCtx.ExistingDescRecords,
				ExistingBodyRecords:   onlineCtx.ExistingBodyRecords,
				ExistingScriptRecords: onlineCtx.ExistingScriptRecords,
				Trajectory:            defaultTrajectory,
				Metadata:              onlineCtx.Metadata,
			}, nil
		}
		return onlineCtx, nil
	}

	return nil, exception.BuildError(
		exception.NewStatusCode("TOOLCHAIN_AGENT_PARAM_ERROR", 170000, ""),
		exception.WithMsg(fmt.Sprintf(
			"online_contexts missing entry for skill %s; TeamSkillExperienceOptimizer requires EvolutionContext",
			skillName,
		)),
	)
}

// generateDraftsWithRetries 调用 LLM + 解析草稿 + 重试循环。
//
// 对齐 Python: TeamSkillExperienceOptimizer._generate_drafts_with_retries(prompt, retry_prompt)
func (o *TeamSkillExperienceOptimizer) generateDraftsWithRetries(ctx context.Context, prompt string, retryPrompt string) ([]ParsedExperienceDraft, error) {
	raw, promptUsed, err := llm_resilience.InvokeTextWithRetryAndPrompt(
		ctx,
		o.llm,
		o.model,
		prompt,
		o.recordLLMPolicy,
		llm_resilience.WithRetryPrompt(retryPrompt),
	)
	if err != nil {
		return nil, err
	}

	drafts, lastError := ParseExperienceDraftsWithError(raw, teamExtractJSONWithError)
	if drafts != nil {
		return drafts, nil
	}

	lastRaw := raw
	for attempt := 2; attempt <= 3; attempt++ {
		logger.Warn(logComponent).
			Int("attempt", attempt).
			Msg("[TeamSkillOptimizer] aggregated parse failed, repair attempt")

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
			_, lastError = ParseExperienceDraftsWithError(retryRaw, teamExtractJSONWithError)
		}
	}

	return nil, fmt.Errorf("TeamSkillExperienceOptimizer aggregated response could not be parsed")
}

// parsePatchResponse 解析 patch 响应为 dict。
//
// 对齐 Python: _parse_patch_response(raw)
func parsePatchResponse(raw string) (map[string]any, string) {
	parsed := signal.ParseTeamModelJSON(raw)
	if parsed == nil {
		return nil, "response is not a JSON object"
	}
	if dict, ok := parsed.(map[string]any); ok {
		return dict, ""
	}
	return nil, "response is not a JSON object"
}

// teamExtractJSONWithError 团队专用的 JSON 提取 + 错误返回。
//
// 对齐 Python: TeamSkillExperienceOptimizer._extract_json_with_error(raw)
func teamExtractJSONWithError(raw string) (any, string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, "empty response"
	}

	// Step 1: 直接解析
	result := tryParse(raw)
	if result != nil {
		return result, ""
	}

	// Step 2: 正则提取 [ ... ] 或 { ... }
	lastError := "unknown"
	for _, pattern := range []string{"\\[[\\s\\S]*\\]", "\\{[\\s\\S]*\\}"} {
		re := regexp.MustCompile(pattern)
		matched := re.FindString(raw)
		if matched != "" {
			result = tryParse(matched)
			if result != nil {
				return result, ""
			}
			var err error
			result, err = tryParseWithError(matched)
			if err != nil {
				lastError = err.Error()
			}
			if result != nil {
				return result, ""
			}
		}
	}
	return nil, lastError
}

// summarizeSkillContentTeam 团队优化器技能内容截断。
//
// 对齐 Python: TeamSkillExperienceOptimizer._summarize_skill_content(raw, max_chars)
func summarizeSkillContentTeam(raw string) string {
	return summarizeSkillContentTeamWithMax(raw, TeamSkillContentMaxChars)
}

// summarizeSkillContentTeamWithMax 团队优化器技能内容截断（可指定上限）。
func summarizeSkillContentTeamWithMax(raw string, maxChars int) string {
	if raw == "" {
		return ""
	}
	if len(raw) <= maxChars {
		return raw
	}
	return raw[:maxChars] + fmt.Sprintf("\n... [truncated, original %d chars]", len(raw))
}

// shortenExistingEvolutionsSummary 缩短已有演进摘要。
//
// 对齐 Python: TeamSkillExperienceOptimizer._shorten_existing_evolutions_summary(summary, max_records)
func shortenExistingEvolutionsSummary(summary string, maxRecords int) string {
	if summary == "" {
		return summary
	}
	lines := strings.Split(summary, "\n")
	var kept []string
	recordCount := 0
	for _, line := range lines {
		if strings.HasPrefix(line, "- [") {
			recordCount++
			if recordCount > maxRecords {
				break
			}
		}
		kept = append(kept, line)
	}
	result := strings.TrimSpace(strings.Join(kept, "\n"))
	if result == "" {
		return summary
	}
	return result
}

// summarizeExistingEvolutions 从已有记录构建演进摘要。
//
// 对齐 Python: TeamSkillExperienceOptimizer._summarize_existing_evolutions(records, language, max_records, preview_chars)
func summarizeExistingEvolutions(records []checkpointing.EvolutionRecord, language string) string {
	var activeRecords []checkpointing.EvolutionRecord
	for _, record := range records {
		if record.Change.SkipReason == nil {
			activeRecords = append(activeRecords, record)
		}
	}
	if len(activeRecords) == 0 {
		return langDefault("无已有演进经验", "No existing evolution records", language)
	}

	header := langDefault("已有演进经验：", "Existing evolution records:", language)
	var lines []string
	lines = append(lines, header)

	maxRecords := TeamEvolutionMaxRecords
	start := len(activeRecords) - maxRecords
	if start < 0 {
		start = 0
	}
	for _, record := range activeRecords[start:] {
		content := record.Change.Content
		content = regexp.MustCompile(`\s+`).ReplaceAllString(content, " ")
		content = strings.TrimSpace(content)
		if len(content) > TeamEvolutionPreviewChars {
			content = content[:TeamEvolutionPreviewChars] + "..."
		}
		lines = append(lines, fmt.Sprintf("- [%s] [%s] %s", record.ID, record.Change.Section, content))
	}
	return strings.Join(lines, "\n")
}

// dumpRaw 调试输出原始响应到 debugDir。
//
// 对齐 Python: TeamSkillExperienceOptimizer._dump_raw(tag, raw)
func (o *TeamSkillExperienceOptimizer) dumpRaw(tag string, raw string) {
	if o.debugDir == "" || raw == "" {
		return
	}
	logger.Info(logComponent).Str("tag", tag).Msg("[TeamSkillOptimizer] dump raw (not implemented in Go: file I/O in debug dir)")
}

// langDefault 根据语言返回默认值。
func langDefault(cnDefault string, enDefault string, language string) string {
	if language == "en" {
		return enDefault
	}
	return cnDefault
}

// orString 返回 value 如果非空，否则返回 fallback。
func orString(value string, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}

// getStrFromAny 从 any 类型安全提取字符串。
func getStrFromAny(v any, defaultVal string) string {
	if v == nil {
		return defaultVal
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

// summarizeSkillContentTeamFallback 返回 N/A 或 无（根据语言）的占位内容。
func summarizeSkillContentTeamFallback(skillName string, language string) string {
	if language == "en" {
		return "N/A"
	}
	return "无"
}
