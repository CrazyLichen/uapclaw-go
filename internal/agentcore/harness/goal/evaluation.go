// 本文件对应 Python openjiuwen/harness/goal/evaluation.py，
// 提供可配置策略的完成度评估。
//
// 评估器不写 GoalRecord：它只产出 GoalAssessment，由 GoalManager.ApplyAssessment 落状态。
package goal

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/utils"
)

// ──────────────────────────── 结构体 ────────────────────────────

// GoalEvaluator 按配置策略评估目标完成度。
// Python: GoalEvaluator (evaluation.py L74)
type GoalEvaluator struct {
	// config 停止判定配置
	config GoalStopConfig
}

// ──────────────────────────── 全局变量 ────────────────────────────

// jsonBlockPattern 匹配 ```json ... ``` 围栏块。
// Python: _JSON_BLOCK_PATTERN (evaluation.py L33)，re.DOTALL + 非贪婪
var jsonBlockPattern = regexp.MustCompile("(?s)```(?:json)?\\s*\\n?(.*?)\\n?\\s*```")

// ──────────────────────────── 导出函数 ────────────────────────────

// NewGoalEvaluator 创建评估器；config 为 nil 时使用默认配置。
// Python: GoalEvaluator(config=None) (evaluation.py L81)
func NewGoalEvaluator(config *GoalStopConfig) *GoalEvaluator {
	if config == nil {
		defaults := NewGoalStopConfig()
		return &GoalEvaluator{config: defaults}
	}
	return &GoalEvaluator{config: *config}
}

// Strategy 返回当前停止策略。
// Python: GoalEvaluator.strategy (evaluation.py L84)
func (e *GoalEvaluator) Strategy() GoalStopStrategy {
	return e.config.Strategy
}

// ShouldSpotCheck 判定 CONTINUE 是否要抽检复核。
// Python: GoalEvaluator.should_spot_check (evaluation.py L264)
func (e *GoalEvaluator) ShouldSpotCheck(record *GoalRecord) bool {
	if record == nil || e.config.VerificationInterval == nil {
		return false
	}
	interval := *e.config.VerificationInterval
	if interval <= 0 {
		return false
	}
	return record.AttemptCount > 0 && record.AttemptCount%interval == 0
}

// Assess 按配置策略产出评估结论。
//
// transcriptResponse 为 nil 表示监理未被调用；空串表示调用了但返回空。
// Python: GoalEvaluator.assess (evaluation.py L88)
func (e *GoalEvaluator) Assess(
	record *GoalRecord,
	agentReport *GoalAssessment,
	transcriptResponse *string,
) GoalAssessment {
	var assessment GoalAssessment
	switch e.config.Strategy {
	case GoalStopStrategyAgentReport:
		assessment = assessAgentReport(agentReport)
	case GoalStopStrategyTranscript:
		assessment = assessTranscript(transcriptResponse)
	default:
		assessment = e.assessHybrid(record, agentReport, transcriptResponse)
	}
	return e.applyHardLimits(record, assessment)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// parseAssessmentJSON 解析 assessor 原始输出，失败时尝试围栏块。
// Python: _parse_assessment_json (evaluation.py L39)
func parseAssessmentJSON(text string) *GoalAssessment {
	trimmed := strings.TrimSpace(text)

	var data any
	if err := json.Unmarshal([]byte(trimmed), &data); err != nil {
		if match := jsonBlockPattern.FindStringSubmatch(trimmed); match != nil {
			if blockErr := json.Unmarshal([]byte(strings.TrimSpace(match[1])), &data); blockErr != nil {
				data = nil
			}
		}
	}

	payload, ok := data.(map[string]any)
	if !ok {
		return nil
	}

	statusText, ok := payload["status"].(string)
	if !ok || statusText == "" {
		return nil
	}
	if _, exists := payload["evidence"]; !exists || !pyTruthy(payload["evidence"]) {
		return nil
	}

	status := GoalAssessmentStatus(statusText)
	switch status {
	case GoalAssessmentStatusContinue,
		GoalAssessmentStatusComplete,
		GoalAssessmentStatusBlocked:
	default:
		return nil
	}

	return &GoalAssessment{
		Status:          status,
		Evidence:        utils.PyStr(payload["evidence"]),
		RemainingWork:   pyOptionalStr(payload["remaining_work"]),
		NextInstruction: pyOptionalStr(payload["next_instruction"]),
	}
}

// fallbackContinue 返回保守的 continue 结论。
// Python: _FALLBACK_CONTINUE (evaluation.py L26)，Go 每次新建（无模块级可变共享）
func fallbackContinue() GoalAssessment {
	remaining := "Continue making verifiable progress toward the objective."
	instruction := "Continue advancing the objective with verifiable evidence."
	return GoalAssessment{
		Status:          GoalAssessmentStatusContinue,
		Evidence:        "assessment_error: no reliable assessment available, treating as continue.",
		RemainingWork:   &remaining,
		NextInstruction: &instruction,
	}
}

// effectiveLimit 复刻 Python 的 `config.x or record.x`（0 视为未设置）。
func effectiveLimit(configValue, recordValue *int) *int {
	if configValue != nil && *configValue != 0 {
		return configValue
	}
	if recordValue != nil && *recordValue != 0 {
		return recordValue
	}
	return nil
}

// assessAgentReport 直接信任主模型自报。
// Python: GoalEvaluator._assess_agent_report (evaluation.py L123)
func assessAgentReport(agentReport *GoalAssessment) GoalAssessment {
	if agentReport == nil {
		remaining := "Submit a valid goal report for this attempt."
		instruction := "Continue the goal attempt and submit a structured report before finishing."
		return GoalAssessment{
			Status:          GoalAssessmentStatusContinue,
			Evidence:        "no_report: no submit_goal_report was accepted for this attempt.",
			RemainingWork:   &remaining,
			NextInstruction: &instruction,
		}
	}
	return normalizeReport(agentReport)
}

// assessTranscript 解析独立监理模型的 JSON 输出。
// Python: GoalEvaluator._assess_transcript (evaluation.py L138)
func assessTranscript(transcriptResponse *string) GoalAssessment {
	if transcriptResponse == nil {
		logger.Info(logger.ComponentAgentCore).
			Str("event_type", "GOAL_ASSESS_TRANSCRIPT_MISSING").
			Msg("transcript strategy: no response, fallback continue")
		return fallbackContinue()
	}
	parsed := parseAssessmentJSON(*transcriptResponse)
	if parsed == nil {
		logger.Warn(logger.ComponentAgentCore).
			Str("event_type", "GOAL_ASSESS_TRANSCRIPT_PARSE_FAILED").
			Msg("transcript strategy: failed to parse response, fallback continue")
		return fallbackContinue()
	}
	return *parsed
}

// assessHybrid 默认策略：信任 continue，终态必须复核。
// Python: GoalEvaluator._assess_hybrid (evaluation.py L155)
func (e *GoalEvaluator) assessHybrid(
	record *GoalRecord,
	agentReport *GoalAssessment,
	transcriptResponse *string,
) GoalAssessment {
	if agentReport == nil {
		logger.Info(logger.ComponentAgentCore).
			Str("event_type", "GOAL_ASSESS_HYBRID_NO_REPORT").
			Msg("hybrid: no agent report, using transcript")
		return assessTranscript(transcriptResponse)
	}

	normalized := normalizeReport(agentReport)

	if normalized.Status == GoalAssessmentStatusContinue {
		if e.ShouldSpotCheck(record) {
			logger.Info(logger.ComponentAgentCore).
				Str("event_type", "GOAL_ASSESS_HYBRID_SPOT_CHECK").
				Int("attempt_count", record.AttemptCount).
				Msg("hybrid: spot-check triggered")
			return verifyWithTranscript(normalized, transcriptResponse, true)
		}
		return normalized
	}

	if transcriptResponse == nil {
		logger.Info(logger.ComponentAgentCore).
			Str("event_type", "GOAL_ASSESS_HYBRID_TRANSCRIPT_NOT_INVOKED").
			Str("agent_status", string(normalized.Status)).
			Msg("hybrid: transcript not invoked, falling back to continue")
	} else {
		logger.Info(logger.ComponentAgentCore).
			Str("event_type", "GOAL_ASSESS_HYBRID_VERIFYING").
			Str("agent_status", string(normalized.Status)).
			Msg("hybrid: verifying via transcript")
	}
	return verifyWithTranscript(normalized, transcriptResponse, false)
}

// verifyWithTranscript 用监理结论复核自报；冲突时监理赢。
// Python: GoalEvaluator._verify_with_transcript (evaluation.py L210)
func verifyWithTranscript(
	agentAssessment GoalAssessment,
	transcriptResponse *string,
	trustOnFailure bool,
) GoalAssessment {
	if transcriptResponse == nil {
		if trustOnFailure {
			return agentAssessment
		}
		logger.Info(logger.ComponentAgentCore).
			Str("event_type", "GOAL_ASSESS_TRANSCRIPT_UNAVAILABLE").
			Msg("transcript unavailable, fallback continue")
		evidence := fmt.Sprintf(
			"transcript_unavailable: agent reported %s but transcript assessor was not invoked for verification.",
			agentAssessment.Status,
		)
		return GoalAssessment{
			Status:          GoalAssessmentStatusContinue,
			Evidence:        evidence,
			RemainingWork:   agentAssessment.RemainingWork,
			NextInstruction: agentAssessment.NextInstruction,
		}
	}

	transcriptResult := parseAssessmentJSON(*transcriptResponse)
	if transcriptResult == nil {
		if trustOnFailure {
			return agentAssessment
		}
		logger.Warn(logger.ComponentAgentCore).
			Str("event_type", "GOAL_ASSESS_TRANSCRIPT_PARSE_FAILED").
			Msg("transcript parse failed, fallback continue")
		evidence := fmt.Sprintf(
			"assessment_error: agent reported %s but transcript assessor response could not be parsed.",
			agentAssessment.Status,
		)
		return GoalAssessment{
			Status:          GoalAssessmentStatusContinue,
			Evidence:        evidence,
			RemainingWork:   agentAssessment.RemainingWork,
			NextInstruction: agentAssessment.NextInstruction,
		}
	}

	if transcriptResult.Status != agentAssessment.Status {
		logger.Info(logger.ComponentAgentCore).
			Str("event_type", "GOAL_ASSESS_CONFLICT").
			Str("agent_status", string(agentAssessment.Status)).
			Str("transcript_status", string(transcriptResult.Status)).
			Msg("conflict between agent report and transcript, using transcript")
	}
	return *transcriptResult
}

// normalizeReport 归一化自报状态；非法值降级为 continue。
// Python: GoalEvaluator._normalize_report (evaluation.py L271)
func normalizeReport(report *GoalAssessment) GoalAssessment {
	switch report.Status {
	case GoalAssessmentStatusContinue,
		GoalAssessmentStatusComplete,
		GoalAssessmentStatusBlocked:
		return *report
	default:
		logger.Warn(logger.ComponentAgentCore).
			Str("event_type", "GOAL_ASSESS_INVALID_REPORT_STATUS").
			Str("report_status", string(report.Status)).
			Msg("invalid report status, normalizing to continue")
		return GoalAssessment{
			Status:          GoalAssessmentStatusContinue,
			Evidence:        fmt.Sprintf("normalized_invalid_status: original was %s", report.Status),
			RemainingWork:   report.RemainingWork,
			NextInstruction: report.NextInstruction,
		}
	}
}

// applyHardLimits 施加硬上限；只有 CONTINUE 会被改判。
// Python: GoalEvaluator._apply_hard_limits (evaluation.py L289)
func (e *GoalEvaluator) applyHardLimits(record *GoalRecord, assessment GoalAssessment) GoalAssessment {
	if assessment.Status != GoalAssessmentStatusContinue {
		return assessment
	}
	effectiveMax := effectiveLimit(e.config.MaxAttempts, record.MaxAttempts)
	effectiveBudget := effectiveLimit(e.config.TokenBudget, record.TokenBudget)
	if effectiveMax != nil && record.AttemptCount >= *effectiveMax {
		logger.Info(logger.ComponentAgentCore).
			Str("event_type", "GOAL_ASSESS_MAX_ATTEMPTS_EXHAUSTED").
			Int("attempt_count", record.AttemptCount).
			Int("max_attempts", *effectiveMax).
			Msg("max_attempts exhausted")
		return GoalAssessment{
			Status:          GoalAssessmentStatusBlocked,
			Evidence:        fmt.Sprintf("max_attempts_exhausted: %d/%d", record.AttemptCount, *effectiveMax),
			RemainingWork:   assessment.RemainingWork,
			NextInstruction: assessment.NextInstruction,
		}
	}
	if effectiveBudget != nil && record.TokenUsage.TotalTokens >= *effectiveBudget {
		logger.Info(logger.ComponentAgentCore).
			Str("event_type", "GOAL_ASSESS_TOKEN_BUDGET_EXHAUSTED").
			Int("total_tokens", record.TokenUsage.TotalTokens).
			Int("token_budget", *effectiveBudget).
			Msg("token budget exhausted")
		return GoalAssessment{
			Status: GoalAssessmentStatusBlocked,
			Evidence: fmt.Sprintf(
				"token_budget_exhausted: %d/%d", record.TokenUsage.TotalTokens, *effectiveBudget,
			),
			RemainingWork:   assessment.RemainingWork,
			NextInstruction: assessment.NextInstruction,
		}
	}
	return assessment
}
