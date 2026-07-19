package signal

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/evolving/optimizer/llm_resilience"
	"github.com/uapclaw/uapclaw-go/internal/evolving/trajectory"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TeamSignalDetector 团队域信号检测器，从用户输入和轨迹中检测演化信号。
//
// 对应 Python: openjiuwen/agent_evolving/signal/team.py TeamSignalDetector
type TeamSignalDetector struct {
	// llm LLM 模型实例
	llm *llm.Model
	// model 模型名称
	model string
	// language 语言（"cn" 或 "en"）
	language string
	// trajectoryIssueLLMPolicy 轨迹问题检测的 LLM 策略
	trajectoryIssueLLMPolicy llm_resilience.LLMInvokePolicy
	// userIntentLLMPolicy 用户意图检测的 LLM 策略
	userIntentLLMPolicy llm_resilience.LLMInvokePolicy
}

// UserIntent 解析后的用户改进意图。
//
// 对应 Python: openjiuwen/agent_evolving/signal/team.py UserIntent(frozen dataclass)
type UserIntent struct {
	// IsImprovement 是否包含改进意图
	IsImprovement bool
	// Intent 改进意图摘要
	Intent string
}

// TrajectoryIssue 规范化的轨迹问题。
//
// 对应 Python: openjiuwen/agent_evolving/signal/team.py TrajectoryIssue(frozen dataclass)
type TrajectoryIssue struct {
	// IssueType 问题类型
	IssueType string
	// Description 问题描述
	Description string
	// AffectedRole 受影响角色
	AffectedRole string
	// Severity 严重程度（low/medium/high）
	Severity string
}

// ──────────────────────────── 枚举 ────────────────────────────

// TeamSignalType 团队域信号类型枚举。
// UserRequest 保留为向后兼容别名。
//
// 对应 Python: TeamSignalType(str, Enum)
type TeamSignalType string

const (
	// TeamSignalTypeUserIntent 用户意图信号
	TeamSignalTypeUserIntent TeamSignalType = "user_intent"
	// TeamSignalTypeUserRequest 用户请求信号（向后兼容）
	TeamSignalTypeUserRequest TeamSignalType = "user_request"
	// TeamSignalTypeTrajectoryIssue 轨迹问题信号
	TeamSignalTypeTrajectoryIssue TeamSignalType = "trajectory_issue"
)

// ──────────────────────────── 常量 ────────────────────────────

// teamTrajectoryIssuesKey 轨迹问题在信号 context 中的键名。
const teamTrajectoryIssuesKey = "trajectory_issues"

// teamSkillContentKey 技能内容在信号 context 中的键名。
const teamSkillContentKey = "skill_content"

// jsonBlockRE 匹配 JSON 代码块的正则。
var jsonBlockRE = regexp.MustCompile("(?s)```(?:json)?\\s*\\n(.*?)```")

// keyTools 协作关键工具集合。
var keyTools = map[string]bool{
	"spawn_member": true, "create_task": true, "build_team": true,
	"view_task": true, "send_message": true,
}

// teamUserRequestPromptCN 中文团队用户请求检测提示词。
//
// 对应 Python: _TEAM_USER_REQUEST_PROMPT_CN（原文复刻，不翻译）
const teamUserRequestPromptCN = "判断以下用户输入是否包含对当前团队任务或团队协作方式的改进意见。\n" +
	"如果是，提取改进意图的摘要。\n\n" +
	"团队技能描述：{team_skill_description}\n" +
	"当前角色：{roles}\n" +
	"用户输入：{user_messages}\n\n" +
	"输出 JSON: {\"is_improvement\": true/false, \"intent\": \"str\"}\n"

// teamUserRequestPromptEN 英文团队用户请求检测提示词。
//
// 对应 Python: _TEAM_USER_REQUEST_PROMPT_EN（原文复刻，不翻译）
const teamUserRequestPromptEN = "Determine if the following user input contains improvement suggestions " +
	"for the current team task or collaboration approach.\n" +
	"If yes, extract a summary of the improvement intent.\n\n" +
	"Team skill description: {team_skill_description}\n" +
	"Current roles: {roles}\n" +
	"User input: {user_messages}\n\n" +
	"Output JSON: {\"is_improvement\": true/false, \"intent\": \"str\"}\n"

// teamTrajectoryIssuePromptCN 中文团队轨迹问题检测提示词。
//
// 对应 Python: _TEAM_TRAJECTORY_ISSUE_PROMPT_CN（原文复刻，不翻译）
const teamTrajectoryIssuePromptCN = "分析以下执行轨迹，判断团队技能是否存在不足需要演进。\n\n" +
	"当前团队技能：\n{skill_content}\n\n" +
	"执行轨迹摘要：\n{trajectory_summary}\n\n" +
	"请从以下维度分析：\n" +
	"- 角色配合是否恰当（是否有角色间协作断裂、数据未传递）\n" +
	"- 约束是否被违反（超时、产出格式不合规）\n" +
	"- 流程是否低效（重复调用、多余步骤）\n" +
	"- 角色能力是否不足（某角色多次失败或产出质量不达标）\n\n" +
	"如果存在不足，输出 JSON 数组：\n" +
	"[{\"issue_type\": str, \"description\": str, \"affected_role\": str, \"severity\": \"low\"|\"medium\"|\"high\"}]\n" +
	"如果没有问题，输出空数组 []。"

// teamTrajectoryIssuePromptEN 英文团队轨迹问题检测提示词。
//
// 对应 Python: _TEAM_TRAJECTORY_ISSUE_PROMPT_EN（原文复刻，不翻译）
const teamTrajectoryIssuePromptEN = "Analyze the following execution trajectory and determine whether the team skill has deficiencies.\n\n" +
	"Current team skill:\n{skill_content}\n\n" +
	"Trajectory summary:\n{trajectory_summary}\n\n" +
	"Analyze from these dimensions:\n" +
	"- Role coordination (collaboration breaks, data not passed)\n" +
	"- Constraint violations (timeout, output format issues)\n" +
	"- Workflow inefficiency (redundant calls, extra steps)\n" +
	"- Role capability gaps (repeated failures, poor output quality)\n\n" +
	"If issues exist, output a JSON array:\n" +
	"[{\"issue_type\": str, \"description\": str, \"affected_role\": str, \"severity\": \"low\"|\"medium\"|\"high\"}]\n" +
	"If no issues, output empty array [].\n"

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTeamSignalDetector 创建 TeamSignalDetector 实例。
// 必须至少传入一个 LLMInvokePolicy，否则 panic。
//
// 对应 Python: TeamSignalDetector(llm, model, language, llm_policy, ...)
func NewTeamSignalDetector(
	llmModel *llm.Model,
	model string,
	language string,
	trajectoryIssueLLMPolicy *llm_resilience.LLMInvokePolicy,
	userIntentLLMPolicy *llm_resilience.LLMInvokePolicy,
) *TeamSignalDetector {
	var policy llm_resilience.LLMInvokePolicy
	if trajectoryIssueLLMPolicy != nil {
		policy = *trajectoryIssueLLMPolicy
	} else if userIntentLLMPolicy != nil {
		policy = *userIntentLLMPolicy
	}
	if policy.MaxAttempts == 0 && policy.TotalBudgetSecs == 0 {
		panic("TeamSignalDetector requires at least one LLM policy")
	}

	trajectoryPolicy := policy
	if trajectoryIssueLLMPolicy != nil {
		trajectoryPolicy = *trajectoryIssueLLMPolicy
	}
	userIntentPolicy := policy
	if userIntentLLMPolicy != nil {
		userIntentPolicy = *userIntentLLMPolicy
	}

	return &TeamSignalDetector{
		llm:                      llmModel,
		model:                    model,
		language:                 language,
		trajectoryIssueLLMPolicy: trajectoryPolicy,
		userIntentLLMPolicy:      userIntentPolicy,
	}
}

// ParseTeamModelJSON 健壮的 JSON 解析器，从团队技能 LLM 输出中解析 dict/list JSON。
// 支持代码块提取、平衡括号提取、格式修复（去除注释/尾逗号）。
//
// 对应 Python: parse_team_model_json(raw)
func ParseTeamModelJSON(raw string) any {
	if raw == "" {
		return nil
	}

	var candidates []string

	match := jsonBlockRE.FindStringSubmatch(raw)
	if len(match) >= 2 {
		candidates = append(candidates, strings.TrimSpace(match[1]))
	}
	candidates = append(candidates, strings.TrimSpace(raw))
	candidates = append(candidates, fixJSONText(raw))

	balancedObject := extractBalancedJSON(raw, '{', '}')
	if balancedObject != "" {
		candidates = append(candidates, balancedObject)
		candidates = append(candidates, fixJSONText(balancedObject))
	}

	balancedArray := extractBalancedJSON(raw, '[', ']')
	if balancedArray != "" {
		candidates = append(candidates, balancedArray)
		candidates = append(candidates, fixJSONText(balancedArray))
	}

	seen := map[string]bool{}
	for _, candidate := range candidates {
		if candidate == "" || seen[candidate] {
			continue
		}
		seen[candidate] = true
		var data any
		if err := json.Unmarshal([]byte(candidate), &data); err == nil {
			switch data.(type) {
			case map[string]any, []any:
				return data
			}
		}
	}

	head := raw
	if len(head) > 600 {
		head = head[:600]
	}
	head = strings.ReplaceAll(head, "\n", "\\n")
	logger.Warn(logComponent).
		Str("method", "ParseTeamModelJSON").
		Int("raw_len", len(raw)).
		Str("head", head).
		Msg("[TeamSignal] JSON parse failed")

	return nil
}

// BuildTeamTrajectorySummary 将轨迹步骤摘要为文本，对关键协作工具保留更多细节。
//
// 对应 Python: build_team_trajectory_summary(trajectory)
func BuildTeamTrajectorySummary(traj *trajectory.Trajectory) string {
	toolBudget := 20000
	llmBudget := 10000
	var toolLines []string
	var llmLines []string
	llmCount := 0
	toolCount := 0

	for _, step := range traj.Steps {
		if step.Kind == trajectory.StepKindTool && step.Detail != nil {
			toolDetail, ok := step.Detail.(*trajectory.ToolCallDetail)
			if !ok {
				continue
			}
			toolCount++
			toolName := toolDetail.ToolName
			isKey := keyTools[toolName]
			argsLimit := 150
			if isKey {
				argsLimit = 500
			}
			resultLimit := 200
			if isKey {
				resultLimit = 500
			}
			args := truncateString(fmt.Sprintf("%v", toolDetail.CallArgs), argsLimit)
			result := truncateString(fmt.Sprintf("%v", toolDetail.CallResult), resultLimit)
			toolLines = append(toolLines, fmt.Sprintf("[Tool:%s] args=%s result=%s", toolName, args, result))
		} else if step.Kind == trajectory.StepKindLLM && step.Detail != nil {
			llmDetail, ok := step.Detail.(*trajectory.LLMCallDetail)
			if !ok {
				continue
			}
			llmCount++
			if llmDetail.Response != nil {
				llmLines = append(llmLines, fmt.Sprintf("[LLM] %s", truncateString(fmt.Sprintf("%v", llmDetail.Response), 300)))
			}
		}
	}

	toolSection := strings.Join(toolLines, "\n")
	if len(toolSection) > toolBudget {
		toolSection = toolSection[:toolBudget] + "\n... (tool section truncated)"
	}

	llmSection := strings.Join(llmLines, "\n")
	if len(llmSection) > llmBudget {
		llmSection = llmSection[:llmBudget] + "\n... (LLM section truncated)"
	}

	summary := fmt.Sprintf("### Tool Calls (%d)\n%s\n\n### LLM Responses (%d)\n%s", toolCount, toolSection, llmCount, llmSection)
	logger.Info(logComponent).
		Str("method", "BuildTeamTrajectorySummary").
		Int("llm_count", llmCount).
		Int("tool_count", toolCount).
		Int("tool_section_len", len(toolSection)).
		Int("llm_section_len", len(llmSection)).
		Int("total_len", len(summary)).
		Msg("[TeamSignal] trajectory summary")

	return summary
}

// MakeTeamUserIntentSignal 构建团队用户意图信号。
//
// 对应 Python: make_team_user_intent_signal(skill_name, user_intent)
func MakeTeamUserIntentSignal(skillName, userIntent string) *EvolutionSignal {
	return MakeEvolutionSignal(
		string(TeamSignalTypeUserIntent),
		"Instructions",
		userIntent,
		WithSkillName(skillName),
		WithSource("explicit_request"),
	)
}

// MakeTeamTrajectorySignal 构建团队轨迹问题信号。
//
// 对应 Python: make_team_trajectory_signal(skill_name, skill_content, trajectory_issues)
func MakeTeamTrajectorySignal(skillName, skillContent string, trajectoryIssues []map[string]string) *EvolutionSignal {
	return MakeEvolutionSignal(
		string(TeamSignalTypeTrajectoryIssue),
		"",
		"Detected team skill trajectory issues requiring evolution.",
		WithSkillName(skillName),
		WithSource("passive_trajectory"),
		WithContext(map[string]any{
			teamTrajectoryIssuesKey: trajectoryIssues,
			teamSkillContentKey:    skillContent,
		}),
	)
}

// GetTeamTrajectoryIssues 从信号中读取轨迹问题列表。
//
// 对应 Python: get_team_trajectory_issues(signal)
func GetTeamTrajectoryIssues(sig *EvolutionSignal) []map[string]string {
	ctx := sig.Context
	if ctx == nil {
		return nil
	}
	issues, ok := ctx[teamTrajectoryIssuesKey]
	if !ok {
		return nil
	}
	slice, ok := issues.([]map[string]string)
	if !ok {
		return nil
	}
	return slice
}

// GetTeamSignalSkillContent 从信号中读取关联的团队技能内容。
//
// 对应 Python: get_team_signal_skill_content(signal)
func GetTeamSignalSkillContent(sig *EvolutionSignal) string {
	ctx := sig.Context
	if ctx == nil {
		return ""
	}
	v, ok := ctx[teamSkillContentKey]
	if !ok || v == nil {
		return ""
	}
	return fmt.Sprintf("%v", v)
}

// DetectUserIntent 检测用户消息是否包含团队技能改进意图。
//
// 对应 Python: TeamSignalDetector.detect_user_intent(messages, team_skill_content)
func (d *TeamSignalDetector) DetectUserIntent(
	ctx context.Context,
	messages []map[string]any,
	teamSkillContent string,
) (*UserIntent, error) {
	var userMsgs []string
	for _, m := range messages {
		role := fmt.Sprintf("%v", m["role"])
		if role == "user" {
			userMsgs = append(userMsgs, fmt.Sprintf("%v", m["content"]))
		}
	}
	if len(userMsgs) > 10 {
		userMsgs = userMsgs[len(userMsgs)-10:]
	}
	if len(userMsgs) == 0 {
		return nil, nil
	}

	userText := strings.Join(userMsgs, "\n")
	promptTemplate := teamUserRequestPromptCN
	if d.language != "cn" {
		promptTemplate = teamUserRequestPromptEN
	}
	skillDesc := ""
	if teamSkillContent != "" {
		if len(teamSkillContent) > 1000 {
			skillDesc = teamSkillContent[:1000]
		} else {
			skillDesc = teamSkillContent
		}
	}
	prompt := strings.ReplaceAll(promptTemplate, "{team_skill_description}", skillDesc)
	prompt = strings.ReplaceAll(prompt, "{roles}", extractRolesSummary(teamSkillContent))
	userTextTruncated := userText
	if len(userTextTruncated) > 2000 {
		userTextTruncated = userTextTruncated[:2000]
	}
	prompt = strings.ReplaceAll(prompt, "{user_messages}", userTextTruncated)

	raw, err := llm_resilience.InvokeTextWithRetry(
		ctx, d.llm, d.model, prompt, d.userIntentLLMPolicy,
		llm_resilience.WithIsResultUsable(func(text string) bool {
			return ParseTeamModelJSON(text) != nil
		}),
	)
	if err != nil {
		logger.Warn(logComponent).
			Str("method", "DetectUserIntent").
			Str("error", err.Error()).
			Msg("[TeamSignalDetector] detect_user_intent failed")
		return nil, err
	}

	parsed := ParseTeamModelJSON(raw)
	m, ok := parsed.(map[string]any)
	if !ok {
		return nil, nil
	}
	isImprovement := false
	switch v := m["is_improvement"].(type) {
	case bool:
		isImprovement = v
	default:
		isImprovement = fmt.Sprintf("%v", v) == "true"
	}
	if isImprovement {
		intent := fmt.Sprintf("%v", m["intent"])
		if intent == "<nil>" {
			intent = ""
		}
		return &UserIntent{IsImprovement: true, Intent: intent}, nil
	}
	return nil, nil
}

// DetectTrajectorySignals 分析团队轨迹，返回标准被动演化信号。
//
// 对应 Python: TeamSignalDetector.detect_trajectory_signals(trajectory, skill_name, skill_content)
func (d *TeamSignalDetector) DetectTrajectorySignals(
	ctx context.Context,
	traj *trajectory.Trajectory,
	skillName, skillContent string,
) ([]*EvolutionSignal, error) {
	issues, err := d.DetectTrajectoryIssues(ctx, traj, skillContent)
	if err != nil {
		return nil, err
	}
	if len(issues) == 0 {
		return nil, nil
	}
	return []*EvolutionSignal{
		MakeTeamTrajectorySignal(skillName, skillContent, issues),
	}, nil
}

// DetectTrajectoryIssues 返回规范化的 medium/high 严重度轨迹问题。
//
// 对应 Python: TeamSignalDetector.detect_trajectory_issues(trajectory, skill_content)
func (d *TeamSignalDetector) DetectTrajectoryIssues(
	ctx context.Context,
	traj *trajectory.Trajectory,
	skillContent string,
) ([]map[string]string, error) {
	trajectorySummary := BuildTeamTrajectorySummary(traj)
	promptTemplate := teamTrajectoryIssuePromptCN
	if d.language != "cn" {
		promptTemplate = teamTrajectoryIssuePromptEN
	}
	skillContentTruncated := skillContent
	if len(skillContentTruncated) > 10000 {
		skillContentTruncated = skillContentTruncated[:10000]
	}
	prompt := strings.ReplaceAll(promptTemplate, "{skill_content}", skillContentTruncated)
	prompt = strings.ReplaceAll(prompt, "{trajectory_summary}", trajectorySummary)

	raw, err := llm_resilience.InvokeTextWithRetry(
		ctx, d.llm, d.model, prompt, d.trajectoryIssueLLMPolicy,
		llm_resilience.WithIsResultUsable(func(text string) bool {
			parsed := ParseTeamModelJSON(text)
			_, ok := parsed.([]any)
			return ok
		}),
	)
	if err != nil {
		logger.Warn(logComponent).
			Str("method", "DetectTrajectoryIssues").
			Str("error", err.Error()).
			Msg("[TeamSignalDetector] detect_trajectory_issues failed")
		return nil, err
	}

	parsed := ParseTeamModelJSON(raw)
	if parsed == nil {
		return nil, nil
	}
	list, ok := parsed.([]any)
	if !ok {
		return nil, nil
	}

	var issues []map[string]string
	for _, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		normalized := normalizeIssue(m)
		if normalized == nil {
			continue
		}
		if normalized["severity"] == "medium" || normalized["severity"] == "high" {
			issues = append(issues, normalized)
		}
	}
	return issues, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// fixJSONText 对常见 LLM JSON 格式问题做轻量修复。
//
// 对应 Python: _fix_json_text(text)
func fixJSONText(text string) string {
	// S1007: 使用 raw string 避免双重转义，但正则含反引号时仍用解释字符串
	text = regexp.MustCompile("(?m)^```(?:json)?\\s*").ReplaceAllString(strings.TrimSpace(text), "")
	text = regexp.MustCompile("(?m)^```\\s*$").ReplaceAllString(text, "")
	text = regexp.MustCompile(`//[^\n]*`).ReplaceAllString(text, "")
	text = regexp.MustCompile(`,\s*([}\]])`).ReplaceAllString(text, "$1")
	return strings.TrimSpace(text)
}

// extractBalancedJSON 提取第一个平衡的 JSON 子串。
//
// 对应 Python: _extract_balanced_json(text, opener, closer)
func extractBalancedJSON(text string, opener, closer rune) string {
	start := strings.IndexRune(text, opener)
	if start == -1 {
		return ""
	}
	depth := 0
	inString := false
	escape := false
	for i := start; i < len(text); i++ {
		ch := rune(text[i])
		if inString {
			if escape {
				escape = false
			} else if ch == '\\' {
				escape = true
			} else if ch == '"' {
				inString = false
			}
			continue
		}
		if ch == '"' {
			inString = true
			continue
		}
		switch ch {
		case opener:
			depth++
		case closer:
			depth--
			if depth == 0 {
				return text[start : i+1]
			}
		}
	}
	return ""
}

// extractRolesSummary 从团队技能内容中提取紧凑的角色摘要。
//
// 对应 Python: _extract_roles_summary(team_skill_content)
func extractRolesSummary(teamSkillContent string) string {
	if teamSkillContent == "" {
		return ""
	}

	lines := strings.Split(teamSkillContent, "\n")
	var roleLines []string
	inRoles := false
	for _, line := range lines {
		stripped := strings.TrimSpace(line)
		lowered := strings.ToLower(stripped)
		if strings.HasPrefix(lowered, "roles:") {
			idx := strings.Index(stripped, ":")
			if idx >= 0 {
				value := strings.TrimSpace(stripped[idx+1:])
				if value != "" {
					roleLines = append(roleLines, value)
				}
			}
			inRoles = true
			continue
		}
		if inRoles {
			if stripped == "" {
				continue
			}
			if strings.HasPrefix(stripped, "-") || (len(line) > 0 && (line[0] == ' ' || line[0] == '\t')) {
				roleLines = append(roleLines, stripped)
				continue
			}
			break
		}
	}

	if len(roleLines) == 0 {
		for _, line := range lines {
			stripped := strings.TrimSpace(line)
			lowered := strings.ToLower(stripped)
			if strings.HasPrefix(lowered, "role:") || strings.HasPrefix(lowered, "角色：") || strings.HasPrefix(lowered, "角色:") {
				roleLines = append(roleLines, stripped)
			}
			if len(roleLines) >= 5 {
				break
			}
		}
	}

	result := strings.Join(roleLines, "\n")
	if len(result) > 500 {
		result = result[:500]
	}
	return result
}

// normalizeIssue 规范化轨迹问题项。
//
// 对应 Python: TeamSignalDetector._normalize_issue(item)
func normalizeIssue(item map[string]any) map[string]string {
	severity := "medium"
	if v, exists := item["severity"]; exists && v != nil {
		s := fmt.Sprintf("%v", v)
		if s == "low" || s == "medium" || s == "high" {
			severity = s
		}
	}
	return map[string]string{
		"issue_type":    stringOrDefault(item, "issue_type", "unknown"),
		"description":   stringOrDefault(item, "description", ""),
		"affected_role": stringOrDefault(item, "affected_role", ""),
		"severity":      severity,
	}
}

// stringOrDefault 从 map 中安全获取字符串值。
func stringOrDefault(m map[string]any, key, defaultVal string) string {
	v, ok := m[key]
	if !ok || v == nil {
		return defaultVal
	}
	s := fmt.Sprintf("%v", v)
	if s == "<nil>" {
		return defaultVal
	}
	return s
}
