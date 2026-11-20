// llm_as_judge.go 基于参考答案的评估 agent（自早前行为评分工作流还原）。
// Python: openjiuwen/rsi/harness_rsi/evaluator/judger/llm_as_judge.py（349 行全量对齐）。

package judger

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	harnessrsi "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi/harness_rsi"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi/harness_rsi/config"
	dataloader "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi/harness_rsi/data_loader"
)

// ──────────────────────────── 常量 ────────────────────────────

const logComponent = logger.ComponentAgentCore

// ──────────────────────────── 结构体 ────────────────────────────

// unusableJudgeAssessmentError 模型有响应，但其评估无法产出可信分数。
// Python: llm_as_judge.py _UnusableJudgeAssessmentError(RetryableModelOutputError)。
type unusableJudgeAssessmentError struct {
	// failureKind 失败类别（truncated / repair_invalid / repair_unavailable / unavailable / invalid_schema）
	failureKind string
	// msg 错误描述
	msg string
}

// Error 实现 error 接口。
func (e *unusableJudgeAssessmentError) Error() string { return e.msg }

// LlmAsJudgeJudger 显式选择的模型判分器；官方与精确匹配判分器保持不变。
// Python: llm_as_judge.py:80 LlmAsJudgeJudger(EvaluationJudger)。
type LlmAsJudgeJudger struct {
	// cfg 评估器配置（Python self._config）
	cfg *config.EvaluatorConfig
}

var _ EvaluationJudger = (*LlmAsJudgeJudger)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewLlmAsJudgeJudger 构造 LLM 裁判。
// Python: llm_as_judge.py:83 __init__。
func NewLlmAsJudgeJudger(cfg *config.EvaluatorConfig) (*LlmAsJudgeJudger, error) {
	j := &LlmAsJudgeJudger{cfg: cfg}
	// Python: if not (config.judge_model_config_ref or config.model_config_ref): raise
	if cfg.JudgeModelConfigRef == "" && cfg.ModelConfigRef == "" {
		return nil, errors.New("llm_as_judge requires judge_model_config_ref or model_config_ref")
	}
	// Python: finite_number(config.judge_success_score, minimum=0, maximum=1, name="judge_success_score")
	if _, err := FiniteNumber(cfg.JudgeSuccessScore, 0, 1, "judge_success_score"); err != nil {
		return nil, err
	}
	// Python: for name in ("judge_agent_max_iterations", "judge_timeout_sec"):
	// bool 或非正整数拒绝（bool-as-int Go 类型层不可能，登记类型层偏差）。
	for _, name := range []struct {
		value int
		label string
	}{
		{cfg.JudgeAgentMaxIterations, "judge_agent_max_iterations"},
		{cfg.JudgeTimeoutSec, "judge_timeout_sec"},
	} {
		if name.value <= 0 {
			return nil, fmt.Errorf("%s must be a positive integer", name.label)
		}
	}
	// Python: judge_max_retries 非整数拒绝（类型层不可能）+ 0..5 范围。
	if cfg.JudgeMaxRetries < 0 || cfg.JudgeMaxRetries > 5 {
		return nil, errors.New("judge_max_retries must be between 0 and 5")
	}
	return j, nil
}

// MethodName 方法标识（Python 类属性 method = "llm_as_judge"）。
func (j *LlmAsJudgeJudger) MethodName() string { return "llm_as_judge" }

// ValidateCase 判分前校验评分契约。
// Python: llm_as_judge.py:103 validate_case。
func (j *LlmAsJudgeJudger) ValidateCase(caseMapping map[string]any) error {
	if _, _, err := ScoringContract(caseMapping); err != nil {
		return NewEvaluationInfrastructureError(err.Error())
	}
	return nil
}

// Judge 对单个用例回答打分。
// Python: llm_as_judge.py:112 judge。
func (j *LlmAsJudgeJudger) Judge(ctx context.Context, caseMapping map[string]any,
	executionResult CaseExecutionView, outputDir string,
	baselineContext BaselineContextView) (*JudgeResult, error) {
	if err := j.ValidateCase(caseMapping); err != nil {
		return nil, err
	}
	// Python: if baseline_context is not None and not self._config.judge_baseline_comparison:
	if baselineContext != nil && !j.cfg.JudgeBaselineComparison {
		return nil, NewEvaluationInfrastructureError("Baseline comparison is disabled for this evaluator")
	}
	normalized, err := dataloader.NormalizeGradingCase(caseMapping)
	if err != nil {
		return nil, NewEvaluationInfrastructureError(err.Error())
	}
	// Python: if execution_result.execution_status != "passed": return self._execution_failure_result(...)
	if executionResult.ExecutionStatusView() != "passed" {
		return j.executionFailureResult(executionResult.ErrorView()), nil
	}
	if outputDir == "" {
		return nil, NewEvaluationInfrastructureError("llm_as_judge requires a case output directory")
	}
	caseDir, err := filepath.Abs(outputDir)
	if err != nil {
		return nil, NewEvaluationInfrastructureError(err.Error())
	}
	// Python: judge_dir = case_dir / "judge" / f"evaluation_{uuid.uuid4().hex[:12]}"
	judgeDir := filepath.Join(caseDir, "judge", fmt.Sprintf("evaluation_%s", uuid.NewString()[:12]))
	workspace := filepath.Join(judgeDir, "evidence")
	behaviors, forbidden, err := ScoringContract(normalized)
	if err != nil {
		return nil, NewEvaluationInfrastructureError(err.Error())
	}
	// Python: reference.get("penalty_mode", "ceiling")
	penaltyMode := "ceiling"
	if reference, ok := normalized["reference"].(map[string]any); ok {
		if mode, ok := reference["penalty_mode"].(string); ok && mode != "" {
			penaltyMode = mode
		}
	}
	if err := PrepareJudgeWorkspace(PrepareJudgeWorkspaceParams{
		Case: normalized, Response: executionResult.ResponseView(),
		CaseDir: caseDir, Workspace: workspace,
		Behaviors: behaviors, Forbidden: forbidden,
		BaselineContext: baselineContext, ScoreMode: j.cfg.ScoreMode,
	}); err != nil {
		return nil, NewEvaluationInfrastructureError(err.Error())
	}
	result, err := j.evaluate(ctx, workspace, judgeDir, behaviors, forbidden, penaltyMode)
	if err != nil {
		// Python: except EvaluationInfrastructureError → error.json 落盘后原样抛出；
		// 其余异常 → 包装后抛出。Go 统一留档 error.json；infra 错误保留类型。
		_ = WriteJudgeJSON(filepath.Join(judgeDir, "error.json"), map[string]any{
			"error_type": errorTypeName(err), "message": err.Error(),
		})
		return nil, err
	}
	if baselineContext != nil {
		if _, err := baselineContext.Validate(normalized); err != nil {
			return nil, NewEvaluationInfrastructureError(err.Error())
		}
		result.Metadata["baseline_context_fingerprint"] = baselineContext.Fingerprint()
	}
	return result, nil
}

// FailureResult 执行失败的规范化 0 分结果。
// Python: llm_as_judge.py:151 _execution_failure_result。
func (j *LlmAsJudgeJudger) FailureResult(errText string) *JudgeResult {
	return j.executionFailureResult(errText)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// executionFailureResult 把求解器崩溃记为连续 harness 失败（传输错误除外）。
// Python: llm_as_judge.py:151 _execution_failure_result。
func (j *LlmAsJudgeJudger) executionFailureResult(errText string) *JudgeResult {
	reason := errText
	if reason == "" {
		reason = "case execution failed"
	}
	metadata := map[string]any{}
	// Python: if self._config.score_mode == "continuous" and not transient_transport_error(reason):
	if j.cfg.ScoreMode == "continuous" && callTransientTransportError(errText) == "" {
		metadata["score_mode"] = "continuous"
	}
	return &JudgeResult{
		Method: j.MethodName(), Score: 0.0, Passed: false, Reason: reason, Metadata: metadata,
	}
}

// evaluate 评估主循环。
// Python: llm_as_judge.py:175 _evaluate。
func (j *LlmAsJudgeJudger) evaluate(ctx context.Context, workspace, judgeDir string,
	behaviors, forbidden []map[string]any, penaltyMode string) (*JudgeResult, error) {
	resolvedWorkspace, err := filepath.Abs(workspace)
	if err != nil {
		return nil, NewEvaluationInfrastructureError(err.Error())
	}
	// Python: prompt = f"Evidence workspace: {...}. Read this exact request file: {...}. ..."
	prompt := fmt.Sprintf(
		"Evidence workspace: %s. Read this exact request file: %s. "+
			"Do not assume /workspace or /request.json. Read relevant evidence files, "+
			"then return the complete evaluation JSON.",
		resolvedWorkspace, filepath.Join(resolvedWorkspace, "request.json"),
	)
	// Python: output_contract = "\nFINAL OUTPUT CONTRACT (applies after reading evidence):\n..." + json.dumps(ids)
	behaviorIDs := make([]string, 0, len(behaviors))
	for _, item := range behaviors {
		behaviorIDs = append(behaviorIDs, item["id"].(string))
	}
	forbiddenIDs := make([]string, 0, len(forbidden))
	for _, item := range forbidden {
		forbiddenIDs = append(forbiddenIDs, item["id"].(string))
	}
	idPayload, err := json.Marshal(map[string]any{
		"behavior_ids": behaviorIDs, "forbidden_ids": forbiddenIDs,
	})
	if err != nil {
		return nil, NewEvaluationInfrastructureError(err.Error())
	}
	outputContract := "\nFINAL OUTPUT CONTRACT (applies after reading evidence):\n" +
		"Return exactly one JSON object using the system schema, without surrounding text " +
		"or Markdown. Put explanations and evidence inside its fields. " +
		"Score each supplied ID exactly once; use [] when no forbidden IDs are supplied.\n" +
		string(idPayload) +
		"\nDo not invent evidence or change a supported score to satisfy the format. " +
		"For a genuine evaluator limitation only, return " +
		`{"status":"unavailable","reason":"specific limitation"}. ` +
		"Missing work is not evaluator unavailability. "

	invokeAgent := func() (string, error) {
		ctxTimeout, cancel := context.WithTimeout(ctx, time.Duration(j.cfg.JudgeTimeoutSec)*time.Second)
		defer cancel()
		raw, agentErr := RunJudgeAgent(ctxTimeout, j.cfg, workspace, prompt+outputContract,
			filepath.Join(judgeDir, "tool_events.jsonl"))
		if agentErr != nil {
			return "", agentErr
		}
		if ctxTimeout.Err() != nil && errors.Is(ctxTimeout.Err(), context.DeadlineExceeded) {
			return "", fmt.Errorf("Judge attempt timed out after %ds", j.cfg.JudgeTimeoutSec)
		}
		return raw, nil
	}
	invokeCloseout := func() (string, error) {
		ctxTimeout, cancel := context.WithTimeout(ctx, time.Duration(j.cfg.JudgeTimeoutSec)*time.Second)
		defer cancel()
		raw, closeoutErr := RunJudgeCloseout(ctxTimeout, j.cfg, workspace)
		if closeoutErr != nil {
			return "", closeoutErr
		}
		if ctxTimeout.Err() != nil && errors.Is(ctxTimeout.Err(), context.DeadlineExceeded) {
			return "", fmt.Errorf("Judge closeout timed out after %ds", j.cfg.JudgeTimeoutSec)
		}
		return raw, nil
	}

	recovery := "none"
	// Python: max_assessments = max(0, self._config.judge_max_retries) + 1
	maxAssessments := j.cfg.JudgeMaxRetries + 1
	var score float64
	var normalized map[string]any
	var requirements map[string]any
	assessmentAttempts := 0
	for assessmentAttempts = 1; assessmentAttempts <= maxAssessments; assessmentAttempts++ {
		// Python: with collect_judge_model_calls() as diagnostics:
		diagCtx, calls, done := CollectJudgeModelCalls(ctx)
		var raw string
		var invokeErr error
		if assessmentAttempts == 1 {
			raw, invokeErr = harnessrsi.RunModelCallWithRetries(diagCtx, invokeAgent, "llm evaluator",
				j.cfg.JudgeMaxRetries, 1.0, 8.0)
			if invokeErr != nil && errors.Is(invokeErr, ErrJudgeIterationLimit) {
				recovery = "complete_frozen_evidence"
				raw, invokeErr = harnessrsi.RunModelCallWithRetries(diagCtx, invokeCloseout, "llm evaluator closeout",
					j.cfg.JudgeMaxRetries, 1.0, 8.0)
			}
		} else {
			recovery = "complete_frozen_evidence"
			raw, invokeErr = invokeCloseout()
		}
		if invokeErr == nil {
			if err := WriteJudgeJSON(filepath.Join(judgeDir,
				fmt.Sprintf("response_%d.json", assessmentAttempts)),
				map[string]any{"raw_output": raw, "recovery": recovery}); err != nil {
				done()
				return nil, NewEvaluationInfrastructureError(err.Error())
			}
			if lastFinishReasonTruncated(calls.Items) {
				done()
				return nil, j.recordUnusableAssessment(judgeDir, assessmentAttempts,
					&unusableJudgeAssessmentError{failureKind: "truncated",
						msg: "Judge output reached its output token limit"},
					diagCtx, recovery)
			}
			parsed, parseErr := j.parseWithFormatRepair(ctx, judgeDir, raw, assessmentAttempts, calls.Items)
			if parseErr != nil {
				done()
				return nil, parseErr
			}
			failureKind := ""
			if parsed["repair_unavailable"] == true {
				failureKind = "repair_unavailable"
			} else if parsed["status"] == "unavailable" {
				failureKind = "unavailable"
			}
			if failureKind != "" {
				done()
				if unwrapErr := j.recordUnusableAssessment(judgeDir, assessmentAttempts,
					&unusableJudgeAssessmentError{failureKind: failureKind,
						msg: "Judge requires a complete evidence-based assessment"},
					diagCtx, recovery); unwrapErr != nil {
					return nil, unwrapErr
				}
				continue
			}
			if status, _ := parsed["status"].(string); status != "" && status != "completed" {
				done()
				if unwrapErr := j.recordUnusableAssessment(judgeDir, assessmentAttempts,
					&unusableJudgeAssessmentError{failureKind: "invalid_schema",
						msg: "invalid judge status"}, diagCtx, recovery); unwrapErr != nil {
					return nil, unwrapErr
				}
				continue
			}
			var scoreErr error
			score, normalized, requirements, scoreErr = ScoreJudgeOutput(parsed, behaviors, forbidden, penaltyMode)
			if scoreErr != nil {
				done()
				if unwrapErr := j.recordUnusableAssessment(judgeDir, assessmentAttempts,
					&unusableJudgeAssessmentError{failureKind: "invalid_schema", msg: scoreErr.Error()},
					diagCtx, recovery); unwrapErr != nil {
					return nil, unwrapErr
				}
				continue
			}
			diagnosticsSnapshot := diagnosticsJSON(calls.Items, assessmentAttempts, recovery)
			done()
			// Python finally: diagnostics_N.json 落盘（成功路径同样落盘）。
			if err := WriteJudgeJSON(filepath.Join(judgeDir,
				fmt.Sprintf("diagnostics_%d.json", assessmentAttempts)), diagnosticsSnapshot); err != nil {
				return nil, NewEvaluationInfrastructureError(err.Error())
			}
			break
		}
		// Python: except (ValueError, TypeError) → invalid_schema 记录；不可恢复错误直接抛出。
		var unusable *unusableJudgeAssessmentError
		if !errors.As(invokeErr, &unusable) {
			done()
			return nil, NewEvaluationInfrastructureError(invokeErr.Error())
		}
		if unwrapErr := j.recordUnusableAssessment(judgeDir, assessmentAttempts,
			unusable, diagCtx, recovery); unwrapErr != nil {
			done()
			return nil, unwrapErr
		}
		if assessmentAttempts >= maxAssessments {
			done()
			return nil, NewEvaluationInfrastructureError(fmt.Sprintf(
				"Unusable LLM evaluation after %d attempts (%s): %s; inspect %s",
				assessmentAttempts, unusable.failureKind, unusable.Error(), judgeDir))
		}
		diagnosticsSnapshot := diagnosticsJSON(calls.Items, assessmentAttempts, recovery)
		done()
		if err := WriteJudgeJSON(filepath.Join(judgeDir,
			fmt.Sprintf("diagnostics_%d.json", assessmentAttempts)), diagnosticsSnapshot); err != nil {
			return nil, NewEvaluationInfrastructureError(err.Error())
		}
	}
	// Python: request.json 的 baseline 段 → normalized["baseline_comparison"]（数值差由调用方计算）。
	requestPath := filepath.Join(workspace, "request.json")
	request := map[string]any{}
	if raw, readErr := os.ReadFile(requestPath); readErr == nil {
		_ = json.Unmarshal(raw, &request)
	}
	if baseline, hasBaseline := request["baseline"].(map[string]any); hasBaseline && baseline != nil {
		grading, _ := baseline["grading"].(map[string]any)
		assessment, _ := grading["assessment"].(map[string]any)
		assessmentBehaviors, _ := assessment["behaviors"].([]any)
		baselineBehaviors := map[string]any{}
		for _, item := range assessmentBehaviors {
			if m, ok := item.(map[string]any); ok {
				if id, ok := m["id"].(string); ok {
					baselineBehaviors[id] = m
				}
			}
		}
		normalizedBehaviors, _ := normalized["behaviors"].([]any)
		criteria := make([]any, 0, len(normalizedBehaviors))
		for _, item := range normalizedBehaviors {
			m, _ := item.(map[string]any)
			if m == nil {
				continue
			}
			id, _ := m["id"].(string)
			criteria = append(criteria, map[string]any{
				"id":                 id,
				"candidate_score":    m["score"],
				"baseline":           baselineBehaviors[id],
				"candidate_reason":   m["reason"],
				"candidate_evidence": m["evidence"],
			})
		}
		baselineScore := toFloat(baseline["score"])
		normalized["baseline_comparison"] = map[string]any{
			"case_id":             baseline["case_id"],
			"baseline_score":      baseline["score"],
			"context_fingerprint": baseline["context_fingerprint"],
			"score_delta":         score - baselineScore,
			"criteria":            criteria,
		}
	}
	if err := WriteJudgeJSON(filepath.Join(judgeDir, "assessment.json"), normalized); err != nil {
		return nil, NewEvaluationInfrastructureError(err.Error())
	}
	passed := score >= j.cfg.JudgeSuccessScore
	finalScore := score
	if j.cfg.ScoreMode != "continuous" {
		if passed {
			finalScore = 1.0
		} else {
			finalScore = 0.0
		}
	}
	// Python: float(passed) → 1.0/0.0。
	attempt := 1
	if recovery != "none" {
		attempt = 2
	}
	return &JudgeResult{
		Method: j.MethodName(),
		Score:  finalScore,
		Passed: passed,
		Reason: normalized["overall_reason"].(string),
		Metadata: map[string]any{
			"parsed":               normalized,
			"dimensions":           normalized["dimensions"],
			"requirement_results":  requirements,
			"judge_dir":            judgeDir,
			"pass_threshold":       j.cfg.JudgeSuccessScore,
			"score_mode":           j.cfg.ScoreMode,
			"optimization_signals": callOptimizationSignalsContract(score, nil, "llm_as_judge.assessment.overall_score"),
			"attempt":              attempt,
			"assessment_attempts":  assessmentAttempts,
			"recovery":             recovery,
		},
	}, nil
}

// parseWithFormatRepair 解析裁判输出并在格式非法时调用修复链。
// Python: llm_as_judge.py:56 _parse_with_format_repair。
func (j *LlmAsJudgeJudger) parseWithFormatRepair(ctx context.Context, judgeDir string,
	output string, attempt int, modelCalls []map[string]any) (map[string]any, error) {
	parsed, parseErr := ParseJudgeOutput(output)
	if parseErr == nil {
		return parsed, nil
	}
	ctxTimeout, cancel := context.WithTimeout(ctx, time.Duration(j.cfg.JudgeTimeoutSec)*time.Second)
	defer cancel()
	repaired, repairErr := RepairJudgeJSON(ctxTimeout, j.cfg, output, parseErr.Error())
	if repairErr != nil {
		return nil, NewEvaluationInfrastructureError(repairErr.Error())
	}
	if err := WriteJudgeJSON(filepath.Join(judgeDir,
		fmt.Sprintf("format_repair_%d.json", attempt)), map[string]any{"raw_output": repaired}); err != nil {
		return nil, NewEvaluationInfrastructureError(err.Error())
	}
	if lastFinishReasonTruncated(modelCalls) {
		return nil, &unusableJudgeAssessmentError{
			failureKind: "truncated", msg: "Judge format repair was truncated",
		}
	}
	parsed, err := ParseJudgeOutput(repaired)
	if err != nil {
		return nil, &unusableJudgeAssessmentError{
			failureKind: "repair_invalid", msg: "Invalid judge format repair: " + err.Error(),
		}
	}
	if parsed["repair_unavailable"] == true {
		return nil, &unusableJudgeAssessmentError{
			failureKind: "repair_unavailable",
			msg:         "Judge format repair unavailable: a complete verdict requires regrading",
		}
	}
	return parsed, nil
}

// recordUnusableAssessment 登记一次不可信评估并判断是否还有重试机会。
// Python: llm_as_judge.py:160 _record_unusable_assessment + except 分支的 max 判断。
func (j *LlmAsJudgeJudger) recordUnusableAssessment(judgeDir string, assessmentAttempts int,
	exc *unusableJudgeAssessmentError, diagCtx context.Context, recovery string) error {
	if err := WriteJudgeJSON(filepath.Join(judgeDir,
		fmt.Sprintf("validation_error_%d.json", assessmentAttempts)), map[string]any{
		"error_type":      "UnusableJudgeAssessmentError",
		"message":         exc.Error(),
		"failure_kind":    exc.failureKind,
		"raw_output_path": fmt.Sprintf("response_%d.json", assessmentAttempts),
	}); err != nil {
		return NewEvaluationInfrastructureError(err.Error())
	}
	return nil
}

// lastFinishReasonTruncated 最后一次模型调用 finish_reason 是否为截断。
// Python: diagnostics[-1].get("finish_reason") in {"length", "max_tokens"}。
func lastFinishReasonTruncated(calls []map[string]any) bool {
	if len(calls) == 0 {
		return false
	}
	reason, _ := calls[len(calls)-1]["finish_reason"].(string)
	return reason == "length" || reason == "max_tokens"
}

// diagnosticsJSON 单次评估尝试的诊断快照。
// Python: finally 块 {"assessment_attempt", "recovery", "model_calls"}。
func diagnosticsJSON(modelCalls []map[string]any, assessmentAttempts int, recovery string) map[string]any {
	if modelCalls == nil {
		modelCalls = []map[string]any{}
	}
	return map[string]any{
		"assessment_attempt": assessmentAttempts,
		"recovery":           recovery,
		"model_calls":        modelCalls,
	}
}

// errorTypeName 错误类型名（Python type(exc).__name__ 等价）。
func errorTypeName(err error) string {
	if err == nil {
		return ""
	}
	return fmt.Sprintf("%T", err)
}

// toFloat 数值安全转换。
func toFloat(value any) float64 {
	switch v := value.(type) {
	case float64:
		return v
	case int:
		return float64(v)
	case json.Number:
		f, _ := v.Float64()
		return f
	default:
		return 0
	}
}
