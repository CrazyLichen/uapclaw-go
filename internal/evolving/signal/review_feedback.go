package signal

// review_feedback.go 评审反馈归因器（F_62 评审半区的演进信号源）。
//
// 对汇总的评审反馈分类并在 Skill 归因不变量下执行安全策略：证据不足绝不
// 修改 Skill。上下文构建器只把具体工具调用参数视为 Skill 读取证明——
// 已安装 Skill 名、模型叙述与工具结果都不足以证明。
//
// Python: agent_evolving/signal/review_feedback.py（全文，2026-12-04 快照）。

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/evolving/trajectory"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// ReviewFeedbackSignalType 评审反馈信号类型。
	ReviewFeedbackSignalType = "review_feedback"
	// ReviewFeedbackSource 评审反馈信号来源。
	ReviewFeedbackSource = "scheduler_review_feedback"

	// 截断上限（review_feedback.py L34-38）。
	maxFeedbackChars     = 4000
	maxTaskContextChars  = 6000
	maxTrajectoryChars   = 8000
	maxSkillContentChars = 6000
	maxTrajectorySpans   = 20
)

// ──────────────────────────── 枚举 ────────────────────────────

// ReviewFeedbackAction 一条汇总评审反馈的安全下游处置。
// Python: ReviewFeedbackAction (review_feedback.py L43-49)。
type ReviewFeedbackAction string

const (
	// ReviewFeedbackActionEvolveExistingSkill 演化既有 Skill。
	ReviewFeedbackActionEvolveExistingSkill ReviewFeedbackAction = "evolve_existing_skill"
	// ReviewFeedbackActionSuggestNewSkill 建议新建 Skill。
	ReviewFeedbackActionSuggestNewSkill ReviewFeedbackAction = "suggest_new_skill"
	// ReviewFeedbackActionRecordTaskFailure 记录任务失败。
	ReviewFeedbackActionRecordTaskFailure ReviewFeedbackAction = "record_task_failure"
	// ReviewFeedbackActionSkipUnattributed 未归因跳过。
	ReviewFeedbackActionSkipUnattributed ReviewFeedbackAction = "skip_unattributed"
)

// ReviewFeedbackClassification 归因模型请求的语义分类。
// Python: ReviewFeedbackClassification (review_feedback.py L52-58)。
type ReviewFeedbackClassification string

const (
	// ReviewFeedbackClassificationSkillIssue 已有 Skill 缺陷。
	ReviewFeedbackClassificationSkillIssue ReviewFeedbackClassification = "skill_issue"
	// ReviewFeedbackClassificationNewSkillPattern 新 Skill 候选模式。
	ReviewFeedbackClassificationNewSkillPattern ReviewFeedbackClassification = "new_skill_pattern"
	// ReviewFeedbackClassificationExecutorError 执行者错误。
	ReviewFeedbackClassificationExecutorError ReviewFeedbackClassification = "executor_error"
	// ReviewFeedbackClassificationUnattributed 无法归因。
	ReviewFeedbackClassificationUnattributed ReviewFeedbackClassification = "unattributed"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ReviewFeedbackContext 伴随汇总评审反馈的证据。
// skill_reads 必须来自具体轨迹证据（如对 <skill>/SKILL.md 的读取）；仅安装
// Skill 不足以证明它影响了失败任务。repetition_count 与
// repeated_pattern_evidence 只是新 Skill 建议的门——本模块从不创建 Skill。
//
// Python: ReviewFeedbackContext (review_feedback.py L62-80)。
type ReviewFeedbackContext struct {
	TaskID                  string
	ReviewRound             int64
	TaskObjective           string
	TrajectoryExcerpt       string
	SkillReads              []string
	SkillContents           map[string]string
	RepetitionCount         int64
	RepeatedPatternEvidence []string
}

// ReviewFeedbackAttribution 归一化、策略强制后的归因结果。
// Python: ReviewFeedbackAttribution (review_feedback.py L84-108)。
type ReviewFeedbackAttribution struct {
	Action            ReviewFeedbackAction
	Classification    ReviewFeedbackClassification
	IsSkillActionable bool
	SkillName         string
	Target            EvolutionTarget
	HasTarget         bool
	Reason            string
	ReusableGuidance  string
	Confidence        float64
	FeedbackExcerpt   string
}

// ShouldCreateSkill 后续集成是否可交给 Skill 创建。
// Python: ReviewFeedbackAttribution.should_create_skill。
func (a *ReviewFeedbackAttribution) ShouldCreateSkill() bool {
	return a.Action == ReviewFeedbackActionSuggestNewSkill
}

// ShouldRecordTaskFailure 结果是否只归属任务失败历史。
// Python: ReviewFeedbackAttribution.should_record_task_failure。
func (a *ReviewFeedbackAttribution) ShouldRecordTaskFailure() bool {
	return a.Action == ReviewFeedbackActionRecordTaskFailure
}

// ReviewFeedbackLLM 独立归因器需要的最小模型面。
// Python: ReviewFeedbackLLM Protocol (review_feedback.py L110-114)。
type ReviewFeedbackLLM interface {
	// Invoke 返回在 content 或 text 中携带 JSON 的响应。
	Invoke(ctx context.Context, model string, messages []map[string]any, timeout time.Duration) (string, error)
}

// ReviewFeedbackAttributor 分类评审反馈并在安全 Skill 归因不变量下强制执行。
// Python: ReviewFeedbackAttributor (review_feedback.py L117-364)。
type ReviewFeedbackAttributor struct {
	llm      ReviewFeedbackLLM
	model    string
	language string
	timeout  time.Duration
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewReviewFeedbackAttributor 构造归因器。
// Python: __init__ (review_feedback.py L120-135)。
func NewReviewFeedbackAttributor(llm ReviewFeedbackLLM, model, language string, timeout time.Duration) (*ReviewFeedbackAttributor, error) {
	if strings.TrimSpace(model) == "" {
		return nil, fmt.Errorf("model must be non-empty")
	}
	if timeout <= 0 {
		return nil, fmt.Errorf("timeout must be greater than zero")
	}
	return &ReviewFeedbackAttributor{llm: llm, model: model, language: language, timeout: timeout}, nil
}

// Attribute 归因汇总反馈，不施加任何下游动作。
// feedback 是主输入。context 提供区分「既有 Skill 缺陷 / 未来 Skill 创建的
// 重复候选工作流 / 执行者失误 / 未归因问题」所需的证据。
//
// Python: attribute (review_feedback.py L137-179)。
func (r *ReviewFeedbackAttributor) Attribute(ctx context.Context, feedback string, context *ReviewFeedbackContext) *ReviewFeedbackAttribution {
	normalizedFeedback := strings.TrimSpace(feedback)
	if context == nil {
		context = &ReviewFeedbackContext{}
	}
	if normalizedFeedback == "" {
		return closedResult("", ReviewFeedbackClassificationUnattributed,
			"empty review feedback cannot be attributed", 0.0)
	}

	skillReads := normalizeSkillReads(context.SkillReads)
	prompt := r.buildPrompt(normalizedFeedback, context, skillReads)
	raw, err := r.llm.Invoke(ctx, r.model, []map[string]any{
		{"role": "user", "content": prompt},
	}, r.timeout)
	if err != nil {
		logger.Warn(logComponentSignal).Err(err).
			Msg("[ReviewFeedbackAttributor] attribution failed")
		return closedResult(normalizedFeedback, ReviewFeedbackClassificationUnattributed,
			"review feedback attribution failed; no Skill mutation is allowed", 0.0)
	}

	payload, parseErr := parseJSONObject(raw)
	if parseErr != nil {
		logger.Warn(logComponentSignal).Err(parseErr).
			Msg("[ReviewFeedbackAttributor] attribution response parse failed")
		return closedResult(normalizedFeedback, ReviewFeedbackClassificationUnattributed,
			"review feedback attribution failed; no Skill mutation is allowed", 0.0)
	}

	return r.enforcePolicy(payload, normalizedFeedback, context, skillReads)
}

// AttributionToEvolutionSignal 把可操作的归因转换为标准演化信号。
// Python: attribution_to_evolution_signal (review_feedback.py L437-472)。
func AttributionToEvolutionSignal(attribution *ReviewFeedbackAttribution, taskID string, reviewRound int64) *EvolutionSignal {
	if attribution == nil ||
		attribution.Action != ReviewFeedbackActionEvolveExistingSkill ||
		!attribution.IsSkillActionable ||
		attribution.SkillName == "" ||
		!attribution.HasTarget ||
		attribution.ReusableGuidance == "" {
		return nil
	}
	sections := map[EvolutionTarget]string{
		EvolutionTargetDescription: "Description",
		EvolutionTargetBody:        "Reviewer Feedback",
		EvolutionTargetScript:      "Scripts",
	}
	section := sections[attribution.Target]
	excerptParts := []string{strings.TrimSpace(attribution.FeedbackExcerpt), strings.TrimSpace(attribution.ReusableGuidance)}
	joined := make([]string, 0, len(excerptParts))
	for _, part := range excerptParts {
		if part != "" {
			joined = append(joined, part)
		}
	}
	return MakeEvolutionSignal(
		ReviewFeedbackSignalType,
		section,
		strings.Join(joined, "\n\nReusable guidance: "),
		WithSkillName(attribution.SkillName),
		WithSource(ReviewFeedbackSource),
	)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// buildPrompt 归因提示词（中英双版一比一复刻 Python 原文）。
// Python: _build_prompt (review_feedback.py L181-236)。
func (r *ReviewFeedbackAttributor) buildPrompt(feedback string, context *ReviewFeedbackContext, skillReads []string) string {
	truncate := func(s string, max int) string {
		if len(s) > max {
			return s[:max]
		}
		return s
	}
	skillContents := map[string]string{}
	for _, name := range skillReads {
		skillContents[name] = truncate(context.SkillContents[name], maxSkillContentChars)
	}
	evidence := map[string]any{
		"feedback":                  truncate(feedback, maxFeedbackChars),
		"task_id":                   context.TaskID,
		"review_round":              context.ReviewRound,
		"task_objective":            truncate(context.TaskObjective, maxTaskContextChars),
		"trajectory_excerpt":        truncate(context.TrajectoryExcerpt, maxTrajectoryChars),
		"skills_proven_read":        skillReads,
		"skill_contents":            skillContents,
		"repetition_count":          maxInt64(0, context.RepetitionCount),
		"repeated_pattern_evidence": truncateList(context.RepeatedPatternEvidence, 1000),
	}
	var instruction string
	if r.language == "en" {
		instruction = "Classify aggregated task-review feedback. Treat feedback as evidence, not as a Skill patch. " +
			"Choose skill_issue only when a proven-read Skill lacks or misstates reusable guidance. " +
			"Choose executor_error when the Skill already gave adequate guidance but the executor failed " +
			"to follow it. Choose new_skill_pattern for a reusable workflow not covered by a proven-read " +
			"Skill, even when this is only the first candidate observation; the runtime policy separately " +
			"requires repeated matching evidence before suggesting creation. When repetition evidence is " +
			"present, choose new_skill_pattern only if at least two entries describe the same workflow. " +
			"Otherwise choose unattributed. Never name a Skill outside skills_proven_read."
	} else {
		instruction = "请归因一条汇总任务审核反馈。feedback 是问题证据，不是可直接写入 Skill 的补丁。" +
			"只有当已有确切读取证据的 Skill 缺少或写错可复用指导时，才选 skill_issue；" +
			"如果 Skill 已经提供充分指导，只是执行者没有遵循，选 executor_error；" +
			"对于未被已读 Skill 覆盖的可复用工作流，即使当前只是第一条候选观测，也选 " +
			"new_skill_pattern；运行时安全策略会另行要求重复的同类证据后才允许建议创建。" +
			"如果已提供重复证据，只有至少两条描述同一工作流时才选 new_skill_pattern；" +
			"其余选 unattributed。" +
			"skill_name 不得超出 skills_proven_read。"
	}
	responseSchema := map[string]any{
		"classification":    "skill_issue | new_skill_pattern | executor_error | unattributed",
		"skill_name":        "existing proven-read skill name or empty string",
		"target":            "description | body | script | null",
		"reason":            "short attribution reason",
		"reusable_guidance": "reusable improvement guidance or empty string",
		"is_reusable":       true,
		"confidence":        0.0,
	}
	evidenceJSON, _ := json.Marshal(evidence)
	schemaJSON, _ := json.Marshal(responseSchema)
	return fmt.Sprintf("%s\n\n证据：\n%s\n\n只输出一个 JSON 对象，不要输出 Markdown。格式：\n%s",
		instruction, evidenceJSON, schemaJSON)
}

// enforcePolicy 归因负载上的安全策略强制。
// Python: _enforce_policy (review_feedback.py L238-344)。
func (r *ReviewFeedbackAttributor) enforcePolicy(payload map[string]any, feedback string, context *ReviewFeedbackContext, skillReads []string) *ReviewFeedbackAttribution {
	classification := parseClassification(payload["classification"])
	reason := strings.TrimSpace(fmt.Sprint(payload["reason"]))
	guidance := strings.TrimSpace(fmt.Sprint(payload["reusable_guidance"]))
	confidence := normalizeConfidence(payload["confidence"])
	isReusable, _ := payload["is_reusable"].(bool)
	excerpt := truncateStr(feedback, maxFeedbackChars)

	if classification == ReviewFeedbackClassificationExecutorError {
		return &ReviewFeedbackAttribution{
			Action:          ReviewFeedbackActionRecordTaskFailure,
			Classification:  classification,
			Reason:          orDefault(reason, "review feedback indicates an execution mistake, not a Skill defect"),
			Confidence:      confidence,
			FeedbackExcerpt: excerpt,
		}
	}

	if classification == ReviewFeedbackClassificationNewSkillPattern {
		repeated := maxInt64(0, context.RepetitionCount) >= 2 || len(context.RepeatedPatternEvidence) >= 2
		if repeated && isReusable && guidance != "" {
			return &ReviewFeedbackAttribution{
				Action:           ReviewFeedbackActionSuggestNewSkill,
				Classification:   classification,
				Reason:           orDefault(reason, "a repeated reusable workflow may warrant a new Skill"),
				ReusableGuidance: guidance,
				Confidence:       confidence,
				FeedbackExcerpt:  excerpt,
			}
		}
		preserved := ""
		if isReusable {
			preserved = guidance
		}
		return &ReviewFeedbackAttribution{
			Action:         ReviewFeedbackActionSkipUnattributed,
			Classification: classification,
			Reason: "new-Skill classification rejected because repeated reusable evidence is " +
				"insufficient or reusable guidance is missing",
			// 保留安全的候选摘要以便集成层与后续任务反馈比对；在上面的重复
			// 证据门通过前保持不可操作。
			ReusableGuidance: preserved,
			Confidence:       confidence,
			FeedbackExcerpt:  excerpt,
		}
	}

	if classification != ReviewFeedbackClassificationSkillIssue {
		return closedResult(feedback, classification,
			orDefault(reason, "review feedback could not be attributed to a Skill"), confidence)
	}

	if len(skillReads) == 0 {
		return closedResult(feedback, classification,
			"no SKILL.md read evidence exists; existing Skill evolution is forbidden", confidence)
	}

	proposedSkill := strings.TrimSpace(fmt.Sprint(payload["skill_name"]))
	if proposedSkill == "<nil>" || proposedSkill == "" {
		proposedSkill = ""
	}
	if proposedSkill == "" && len(skillReads) == 1 {
		proposedSkill = skillReads[0]
	}
	if !containsStr(skillReads, proposedSkill) {
		return closedResult(feedback, classification,
			"the proposed Skill is not backed by SKILL.md read evidence", confidence)
	}

	target, hasTarget := parseTarget(payload["target"])
	if !isReusable || !hasTarget || guidance == "" {
		return closedResult(feedback, classification,
			"the attributed issue lacks reusable, target-specific Skill guidance", confidence)
	}

	return &ReviewFeedbackAttribution{
		Action:            ReviewFeedbackActionEvolveExistingSkill,
		Classification:    classification,
		IsSkillActionable: true,
		SkillName:         proposedSkill,
		Target:            target,
		HasTarget:         true,
		Reason:            orDefault(reason, "review feedback identifies a reusable defect in a proven-read Skill"),
		ReusableGuidance:  guidance,
		Confidence:        confidence,
		FeedbackExcerpt:   excerpt,
	}
}

// closedResult 封闭（不可操作）结果。
// Python: _closed_result (review_feedback.py L346-364)。
func closedResult(feedback string, classification ReviewFeedbackClassification, reason string, confidence float64) *ReviewFeedbackAttribution {
	return &ReviewFeedbackAttribution{
		Action:          ReviewFeedbackActionSkipUnattributed,
		Classification:  classification,
		Reason:          reason,
		Confidence:      confidence,
		FeedbackExcerpt: truncateStr(feedback, maxFeedbackChars),
	}
}

// normalizeSkillReads 验收人读取名归一（去空白去重保序）。
// Python: _normalize_skill_reads (review_feedback.py L474-481)。
func normalizeSkillReads(skillReads []string) []string {
	var normalized []string
	for _, raw := range skillReads {
		name := strings.TrimSpace(raw)
		if name != "" && !containsStr(normalized, name) {
			normalized = append(normalized, name)
		}
	}
	return normalized
}

// parseJSONObject 解析响应中的 JSON 对象（容忍 ``` 围栏与前后噪声）。
// Python: _parse_json_object (review_feedback.py L492-518)。
func parseJSONObject(raw string) (map[string]any, error) {
	text := strings.TrimSpace(raw)
	if strings.HasPrefix(text, "```") {
		lines := strings.Split(text, "\n")
		if len(lines) > 0 && strings.HasPrefix(lines[0], "```") {
			lines = lines[1:]
		}
		if len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "```" {
			lines = lines[:len(lines)-1]
		}
		text = strings.TrimSpace(strings.Join(lines, "\n"))
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		start := strings.Index(text, "{")
		end := strings.LastIndex(text, "}")
		if start < 0 || end <= start {
			return nil, fmt.Errorf("attribution response does not contain a JSON object")
		}
		if err := json.Unmarshal([]byte(text[start:end+1]), &parsed); err != nil {
			return nil, err
		}
	}
	if parsed == nil {
		return nil, fmt.Errorf("attribution response must be a JSON object")
	}
	return parsed, nil
}

// parseClassification 分类列解析（非法回落 unattributed）。
// Python: _parse_classification (review_feedback.py L520-525)。
func parseClassification(value any) ReviewFeedbackClassification {
	classification := ReviewFeedbackClassification(strings.ToLower(strings.TrimSpace(fmt.Sprint(value))))
	switch classification {
	case ReviewFeedbackClassificationSkillIssue,
		ReviewFeedbackClassificationNewSkillPattern,
		ReviewFeedbackClassificationExecutorError,
		ReviewFeedbackClassificationUnattributed:
		return classification
	default:
		return ReviewFeedbackClassificationUnattributed
	}
}

// parseTarget 目标列解析。
// Python: _parse_target (review_feedback.py L527-536)。
func parseTarget(value any) (EvolutionTarget, bool) {
	if value == nil {
		return "", false
	}
	target := EvolutionTarget(strings.ToLower(strings.TrimSpace(fmt.Sprint(value))))
	switch target {
	case EvolutionTargetDescription, EvolutionTargetBody, EvolutionTargetScript:
		return target, true
	default:
		return "", false
	}
}

// normalizeConfidence 置信度钳位 [0,1]。
// Python: _normalize_confidence (review_feedback.py L538-546)。
func normalizeConfidence(value any) float64 {
	confidence, ok := value.(float64)
	if !ok {
		return 0.0
	}
	if confidence < 0.0 {
		return 0.0
	}
	if confidence > 1.0 {
		return 1.0
	}
	return confidence
}

// ExtractSkillReadsFromTrajectory 从轨迹提取 Skill 读取证明。
// Python: _extract_skill_reads_from_trajectory (review_feedback.py L548-569)。
func ExtractSkillReadsFromTrajectory(traj *trajectory.Trajectory, knownSkills []string) []string {
	if traj == nil {
		return nil
	}
	normalized := normalizeSkillReads(knownSkills)
	var proven []string
	for _, span := range trajectory.IterSpans(traj) {
		if trajectory.SpanCategory(span) != "tool" {
			continue
		}
		toolCall := trajectory.ReadToolCall(span)
		toolName := strings.ToLower(strings.TrimSpace(fmt.Sprint(toolCall["name"])))
		callArgs := toolCall["input"]
		argsText := stableText(callArgs)
		for _, skillName := range normalized {
			if containsStr(proven, skillName) {
				continue
			}
			if toolCallProvesSkillRead(toolName, callArgs, argsText, skillName) {
				proven = append(proven, skillName)
			}
		}
	}
	return proven
}

// skillReadPathPattern 生成 Skill 路径匹配正则。
// Python: _tool_call_proves_skill_read 的 path_pattern。
func skillReadPathPattern(skillName string) *regexp.Regexp {
	escaped := regexp.QuoteMeta(skillName)
	return regexp.MustCompile(`(?i)(?:^|[/\\])` + escaped + `[/\\]SKILL\.md(?:$|[\s'"])`)
}

// toolCallProvesSkillRead 判定一次工具调用是否证明读取了某 Skill。
// Python: _tool_call_proves_skill_read (review_feedback.py L571-591)。
func toolCallProvesSkillRead(toolName string, callArgs any, argsText, skillName string) bool {
	hasSkillPath := skillReadPathPattern(skillName).MatchString(argsText)
	baseToolName := toolName
	if idx := strings.LastIndex(baseToolName, "."); idx >= 0 {
		baseToolName = baseToolName[idx+1:]
	}
	baseToolName = strings.ReplaceAll(baseToolName, "-", "_")
	if baseToolName == "skill_tool" && containsStr(namedSkillValues(callArgs, ""), skillName) {
		return true
	}
	if !hasSkillPath {
		return false
	}
	for _, token := range []string{"read", "view", "open"} {
		if strings.Contains(toolName, token) {
			return true
		}
	}
	for _, token := range []string{"bash", "shell", "exec", "command"} {
		if strings.Contains(toolName, token) {
			shellRead := regexp.MustCompile(`(?i)\b(?:cat|sed|head|tail|less|more|rg|grep)\b`)
			return shellRead.MatchString(argsText)
		}
	}
	return false
}

// namedSkillValues 递归收集 skill/skill_name/name 键下的字符串值。
// Python: _named_skill_values (review_feedback.py L593-612)。
func namedSkillValues(value any, parentKey string) []string {
	var names []string
	switch v := value.(type) {
	case map[string]any:
		for key, item := range v {
			normalizedKey := strings.ToLower(strings.TrimSpace(key))
			if str, isStr := item.(string); isStr &&
				(normalizedKey == "skill" || normalizedKey == "skill_name" || normalizedKey == "name") {
				names = append(names, strings.TrimSpace(str))
			}
			names = append(names, namedSkillValues(item, normalizedKey)...)
		}
	case []any:
		for _, item := range v {
			names = append(names, namedSkillValues(item, parentKey)...)
		}
	case string:
		if parentKey == "skill" || parentKey == "skill_name" || parentKey == "name" {
			names = append(names, strings.TrimSpace(v))
		}
	}
	return names
}

// TrajectoryExcerpt 轨迹摘要（末 20 个 span，tool/llm 行）。
// Python: _trajectory_excerpt (review_feedback.py L614-634)。
func TrajectoryExcerpt(traj *trajectory.Trajectory) string {
	if traj == nil {
		return ""
	}
	spans := trajectory.IterSpans(traj)
	if len(spans) > maxTrajectorySpans {
		spans = spans[len(spans)-maxTrajectorySpans:]
	}
	var lines []string
	for _, span := range spans {
		category := trajectory.SpanCategory(span)
		if category == "tool" {
			toolCall := trajectory.ReadToolCall(span)
			toolName := fmt.Sprint(toolCall["name"])
			callArgs := truncateStr(stableText(toolCall["input"]), 800)
			callResult := truncateStr(stableText(toolCall["output"]), 800)
			lines = append(lines, fmt.Sprintf("[tool] %s args=%s result=%s", toolName, callArgs, callResult))
		} else if category == "llm" {
			_, completions := trajectory.ReadLLMExchange(span)
			var last any
			if len(completions) > 0 {
				last = completions[len(completions)-1]
			}
			response := truncateStr(completionTextOf(last), 1000)
			if response != "" {
				lines = append(lines, fmt.Sprintf("[assistant] %s", response))
			}
		}
	}
	joined := strings.Join(lines, "\n")
	if len(joined) > maxTrajectoryChars {
		joined = joined[len(joined)-maxTrajectoryChars:]
	}
	return joined
}

// completionTextOf 提取完成消息的响应文本（content 列可为字符串或响应字典）。
func completionTextOf(completion any) string {
	asMap, isMap := completion.(map[string]any)
	if !isMap {
		return stableText(completion)
	}
	content, hasContent := asMap["content"]
	if !hasContent {
		return stableText(completion)
	}
	if str, isStr := content.(string); isStr {
		return str
	}
	if inner, isMap := content.(map[string]any); isMap {
		if str, isStr := inner["content"].(string); isStr {
			return str
		}
	}
	return stableText(content)
}

// stableText 值 → 稳定文本（str/JSON 兜底）。
// Python: _stable_text (review_feedback.py L636-647)。
func stableText(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case map[string]any, []any:
		data, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprint(v)
		}
		return string(data)
	default:
		return fmt.Sprint(v)
	}
}

// maxInt64 整数上限。
func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

// containsStr 字符串包含。
func containsStr(list []string, target string) bool {
	for _, item := range list {
		if item == target {
			return true
		}
	}
	return false
}

// truncateStr 字符串截断。
func truncateStr(s string, max int) string {
	if len(s) > max {
		return s[:max]
	}
	return s
}

// orDefault 非空优先。
func orDefault(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}

// truncateList 列表逐项截断。
func truncateList(list []string, max int) []string {
	out := make([]string, 0, len(list))
	for _, item := range list {
		out = append(out, truncateStr(item, max))
	}
	return out
}

// logComponentSignal signal 包日志组件（review_feedback 消费）。
var logComponentSignal = logger.ComponentCommon
