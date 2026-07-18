package llm_resilience

import (
	"context"
	"math"
	"reflect"
	"strings"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// LLMInvokePolicy 演化层 LLM 调用策略。
//
// 控制单次尝试超时、总预算、最大尝试次数和指数退避参数。
//
// 对应 Python: LLMInvokePolicy dataclass
type LLMInvokePolicy struct {
	// AttemptTimeoutSecs 单次尝试超时（秒）
	AttemptTimeoutSecs float64
	// TotalBudgetSecs 总预算（秒），≤ 0 时立即报错
	TotalBudgetSecs float64
	// MaxAttempts 最大尝试次数，默认 2
	MaxAttempts int
	// BackoffBaseSecs 退避基数（秒），默认 1.0；≤ 0 时跳过退避
	BackoffBaseSecs float64
	// RetryEmptyResponse 是否重试空响应，默认 true
	RetryEmptyResponse bool
}

// InvokeRetryOption 重试调用选项函数。
type InvokeRetryOption func(*invokeRetryConfig)

// invokeRetryConfig 重试调用的额外配置。
type invokeRetryConfig struct {
	// retryPrompt 超时后切换的短提示词
	retryPrompt string
	// temperature 温度参数
	temperature *float64
	// isResultUsable 结果可用性检查函数
	isResultUsable func(string) bool
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// logComponent llm_resilience 包日志组件常量
const logComponent = logger.ComponentAgentCore

// 错误原因常量
const (
	reasonTotalBudgetExceeded = "total_budget_exceeded"
	reasonInvokeFailed        = "invoke_failed"
	reasonEmptyResponse       = "empty_response"
	reasonUnusableResponse    = "unusable_response"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// InvokeTextWithRetry 带重试的 LLM 文本调用，返回响应文本。
//
// 包装 InvokeTextWithRetryAndPrompt，丢弃使用的 prompt。
//
// 对应 Python: invoke_text_with_retry()
func InvokeTextWithRetry(
	ctx context.Context,
	model *llm.Model,
	modelName string,
	prompt string,
	policy LLMInvokePolicy,
	opts ...InvokeRetryOption,
) (string, error) {
	raw, _, err := InvokeTextWithRetryAndPrompt(ctx, model, modelName, prompt, policy, opts...)
	return raw, err
}

// InvokeTextWithRetryAndPrompt 带重试的 LLM 文本调用，返回响应文本和使用的 prompt。
//
// 处理三种失败模式：
//  1. 调用异常：超时类异常可能触发 retry_prompt 回退，其他异常直接报错
//  2. 空响应：policy.RetryEmptyResponse 为 true 时重试
//  3. 不可用响应：isResultUsable 返回 false 时重试
//
// 总预算控制采用双重方案：
//   - 外层 context.WithTimeout 限制整体时间
//   - 每次 attempt 前手动检查 remainingBudget > 0
//
// 对齐 Python: invoke_text_with_retry_and_prompt()
func InvokeTextWithRetryAndPrompt(
	ctx context.Context,
	model *llm.Model,
	modelName string,
	prompt string,
	policy LLMInvokePolicy,
	opts ...InvokeRetryOption,
) (text string, promptUsed string, err error) {
	// 解析选项
	cfg := &invokeRetryConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	// 对齐 Python: if policy.total_budget_secs <= 0
	if policy.TotalBudgetSecs <= 0 {
		return "", "", raiseLLMResilienceError(
			exception.NewStatusCode("TOOLCHAIN_EVOLVING_TOOL_CALL_LLM_CALL_EXECUTION_ERROR", 174031, ""),
			reasonTotalBudgetExceeded,
			0,
			nil,
			"",
			nil,
		)
	}

	startedAt := time.Now()
	attemptsStarted := 0
	var lastError error
	lastResponse := ""
	useRetryPrompt := false
	maxAttempts := policy.MaxAttempts
	if maxAttempts < 1 {
		maxAttempts = 1
	}

	// 对齐 Python: async with asyncio.timeout(policy.total_budget_secs)
	budgetCtx, cancel := context.WithTimeout(ctx, time.Duration(policy.TotalBudgetSecs*float64(time.Second)))
	defer cancel()

	defer func() {
		// 对齐 Python: except TimeoutError
		if r := recover(); r != nil {
			// 不期望 panic，但做防御性处理
			err = raiseLLMResilienceError(
				exception.NewStatusCode("TOOLCHAIN_EVOLVING_TOOL_CALL_LLM_CALL_EXECUTION_ERROR", 174031, ""),
				reasonTotalBudgetExceeded,
				attemptsStarted,
				lastError,
				lastResponse,
				nil,
			)
		}
	}()

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		attemptsStarted = attempt

		// 检查 context 是否已超时
		if budgetCtx.Err() != nil {
			logger.Warn(logComponent).
				Str("method", "InvokeTextWithRetryAndPrompt").
				Str("model", modelName).
				Int("attempts_started", attemptsStarted).
				Int("prompt_chars", len(prompt)).
				Float64("total_budget", policy.TotalBudgetSecs).
				Float64("elapsed", time.Since(startedAt).Seconds()).
				Str("last_error", errorString(lastError)).
				Msg("[llm_resilience] LLM total budget exceeded")
			return "", "", raiseLLMResilienceError(
				exception.NewStatusCode("TOOLCHAIN_EVOLVING_TOOL_CALL_LLM_CALL_EXECUTION_ERROR", 174031, ""),
				reasonTotalBudgetExceeded,
				attemptsStarted,
				lastError,
				lastResponse,
				budgetCtx.Err(),
			)
		}

		// 对齐 Python: remaining_budget = policy.total_budget_secs - elapsed
		elapsed := time.Since(startedAt).Seconds()
		remainingBudget := policy.TotalBudgetSecs - elapsed
		if remainingBudget <= 0 {
			logger.Warn(logComponent).
				Str("method", "InvokeTextWithRetryAndPrompt").
				Str("model", modelName).
				Int("attempts_started", attemptsStarted).
				Int("prompt_chars", len(prompt)).
				Float64("total_budget", policy.TotalBudgetSecs).
				Float64("elapsed", elapsed).
				Str("last_error", errorString(lastError)).
				Msg("[llm_resilience] LLM total budget exceeded")
			return "", "", raiseLLMResilienceError(
				exception.NewStatusCode("TOOLCHAIN_EVOLVING_TOOL_CALL_LLM_CALL_EXECUTION_ERROR", 174031, ""),
				reasonTotalBudgetExceeded,
				attempt-1,
				lastError,
				lastResponse,
				nil,
			)
		}

		// 对齐 Python: timeout_secs = min(policy.attempt_timeout_secs, remaining_budget)
		timeoutSecs := math.Min(policy.AttemptTimeoutSecs, remainingBudget)

		// 对齐 Python: current_prompt = retry_prompt if use_retry_prompt and retry_prompt is not None else prompt
		currentPrompt := prompt
		if useRetryPrompt && cfg.retryPrompt != "" {
			currentPrompt = cfg.retryPrompt
		}

		// 构建 LLM 调用参数
		// 对齐 Python: messages=[{"role": "user", "content": current_prompt}]
		messages := model_clients.NewMessagesParam(llmschema.NewUserMessage(currentPrompt))
		invokeOpts := []model_clients.InvokeOption{
			model_clients.WithInvokeModel(modelName),
			model_clients.WithInvokeTimeout(timeoutSecs),
		}
		if cfg.temperature != nil {
			invokeOpts = append(invokeOpts, model_clients.WithInvokeTemperature(*cfg.temperature))
		}

		// 对齐 Python: response = await llm.invoke(...)
		var response *llmschema.AssistantMessage
		response, err = model.Invoke(budgetCtx, messages, invokeOpts...)
		if err != nil {
			lastError = err
			logger.Warn(logComponent).
				Str("method", "InvokeTextWithRetryAndPrompt").
				Str("model", modelName).
				Int("attempt", attempt).
				Int("max_attempts", maxAttempts).
				Int("prompt_chars", len(currentPrompt)).
				Float64("timeout", timeoutSecs).
				Bool("timeout_like", isTimeoutLike(err)).
				Str("error", err.Error()).
				Msg("[llm_resilience] LLM attempt failed")

			// 对齐 Python: if retry_prompt is not None and attempt < policy.max_attempts and _is_timeout_like(exc)
			if cfg.retryPrompt != "" && attempt < maxAttempts && isTimeoutLike(err) {
				useRetryPrompt = true
				logger.Info(logComponent).
					Str("method", "InvokeTextWithRetryAndPrompt").
					Int("attempt", attempt).
					Int("max_attempts", maxAttempts).
					Msg("[llm_resilience] attempt timed out; retrying with shorter prompt")
				if sleepErr := sleepBeforeRetry(budgetCtx, policy, startedAt, attempt); sleepErr != nil {
					return "", "", raiseLLMResilienceError(
						exception.NewStatusCode("TOOLCHAIN_EVOLVING_TOOL_CALL_LLM_CALL_EXECUTION_ERROR", 174031, ""),
						reasonTotalBudgetExceeded,
						attempt,
						lastError,
						lastResponse,
						sleepErr,
					)
				}
				continue
			}
			return "", "", raiseLLMResilienceError(
				exception.NewStatusCode("TOOLCHAIN_EVOLVING_TOOL_CALL_LLM_CALL_EXECUTION_ERROR", 174031, ""),
				reasonInvokeFailed,
				attempt,
				err,
				lastResponse,
				err,
			)
		}

		// 对齐 Python: raw = _response_to_text(response)
		raw := responseToTextFromAssistantMessage(response)
		lastResponse = raw

		// 对齐 Python: if policy.retry_empty_response and not raw.strip()
		if policy.RetryEmptyResponse && strings.TrimSpace(raw) == "" {
			if attempt >= maxAttempts {
				return "", "", raiseLLMResilienceError(
					exception.NewStatusCode("TOOLCHAIN_EVOLVING_TOOL_CALL_OUTPUT_PARSE_ERROR", 174034, ""),
					reasonEmptyResponse,
					attempt,
					lastError,
					raw,
					nil,
				)
			}
			logger.Warn(logComponent).
				Str("method", "InvokeTextWithRetryAndPrompt").
				Str("model", modelName).
				Int("attempt", attempt).
				Int("max_attempts", maxAttempts).
				Msg("[llm_resilience] empty LLM response; retrying")
			if sleepErr := sleepBeforeRetry(budgetCtx, policy, startedAt, attempt); sleepErr != nil {
				return "", "", raiseLLMResilienceError(
					exception.NewStatusCode("TOOLCHAIN_EVOLVING_TOOL_CALL_LLM_CALL_EXECUTION_ERROR", 174031, ""),
					reasonTotalBudgetExceeded,
					attempt,
					lastError,
					lastResponse,
					sleepErr,
				)
			}
			continue
		}

		// 对齐 Python: if is_result_usable is not None
		if cfg.isResultUsable != nil {
			usable := false
			func() {
				defer func() {
					if r := recover(); r != nil {
						usable = false
					}
				}()
				usable = cfg.isResultUsable(raw)
			}()
			if !usable {
				if attempt >= maxAttempts {
					return "", "", raiseLLMResilienceError(
						exception.NewStatusCode("TOOLCHAIN_EVOLVING_TOOL_CALL_OUTPUT_PARSE_ERROR", 174034, ""),
						reasonUnusableResponse,
						attempt,
						lastError,
						raw,
						nil,
					)
				}
				logger.Warn(logComponent).
					Str("method", "InvokeTextWithRetryAndPrompt").
					Str("model", modelName).
					Int("attempt", attempt).
					Int("max_attempts", maxAttempts).
					Int("response_chars", len(raw)).
					Msg("[llm_resilience] unusable LLM response; retrying")
				if sleepErr := sleepBeforeRetry(budgetCtx, policy, startedAt, attempt); sleepErr != nil {
					return "", "", raiseLLMResilienceError(
						exception.NewStatusCode("TOOLCHAIN_EVOLVING_TOOL_CALL_LLM_CALL_EXECUTION_ERROR", 174031, ""),
						reasonTotalBudgetExceeded,
						attempt,
						lastError,
						lastResponse,
						sleepErr,
					)
				}
				continue
			}
		}

		// 对齐 Python: return raw, current_prompt
		return raw, currentPrompt, nil
	}

	// 对齐 Python: 兜底 _raise_llm_resilience_error (所有 attempts 耗尽但未返回)
	return "", "", raiseLLMResilienceError(
		exception.NewStatusCode("TOOLCHAIN_EVOLVING_TOOL_CALL_OUTPUT_PARSE_ERROR", 174034, ""),
		reasonUnusableResponse,
		maxAttempts,
		lastError,
		lastResponse,
		nil,
	)
}

// WithRetryPrompt 设置超时后切换的短提示词。
//
// 对应 Python: invoke_text_with_retry_and_prompt(retry_prompt=...)
func WithRetryPrompt(prompt string) InvokeRetryOption {
	return func(cfg *invokeRetryConfig) { cfg.retryPrompt = prompt }
}

// WithTemperature 设置温度参数。
//
// 对应 Python: invoke_text_with_retry_and_prompt(temperature=...)
func WithTemperature(t float64) InvokeRetryOption {
	return func(cfg *invokeRetryConfig) { cfg.temperature = &t }
}

// WithIsResultUsable 设置结果可用性检查函数。
//
// 对应 Python: invoke_text_with_retry_and_prompt(is_result_usable=...)
func WithIsResultUsable(fn func(string) bool) InvokeRetryOption {
	return func(cfg *invokeRetryConfig) { cfg.isResultUsable = fn }
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// isTimeoutLike 判断错误是否为超时类型。
//
// 检测规则（对齐 Python _is_timeout_like）：
//  1. context.DeadlineExceeded（上下文超时）
//  2. 错误类型名包含 "timeout"
//  3. 错误消息包含 "timeout" 或 "timed out"
//
// 对应 Python: _is_timeout_like(exc)
func isTimeoutLike(err error) bool {
	if err == nil {
		return false
	}
	// 对齐 Python: isinstance(exc, asyncio.TimeoutError)
	if err == context.DeadlineExceeded {
		return true
	}
	// 对齐 Python: "timeout" in type(exc).__name__.lower()
	typeName := reflect.TypeOf(err).String()
	if strings.Contains(strings.ToLower(typeName), "timeout") {
		return true
	}
	// 对齐 Python: "timeout" in message or "timed out" in message
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "timeout") || strings.Contains(msg, "timed out")
}

// sleepBeforeRetry 指数退避等待。
//
// 对齐 Python: _sleep_before_retry()
//   - 退避时间 = backoff_base * 2^(attempt-1)
//   - 取 min(backoff, remaining_budget)
//   - 如果 backoff_base <= 0 或 remaining_budget <= 0，跳过
func sleepBeforeRetry(ctx context.Context, policy LLMInvokePolicy, startedAt time.Time, attempt int) error {
	if policy.BackoffBaseSecs <= 0 {
		return nil
	}

	remainingBudget := policy.TotalBudgetSecs - time.Since(startedAt).Seconds()
	if remainingBudget <= 0 {
		return nil
	}

	// 对齐 Python: backoff_secs = policy.backoff_base_secs * (2 ** max(attempt - 1, 0))
	exp := attempt - 1
	if exp < 0 {
		exp = 0
	}
	backoffSecs := policy.BackoffBaseSecs * math.Pow(2, float64(exp))
	sleepSecs := math.Min(backoffSecs, remainingBudget)

	// 对齐 Python: await asyncio.sleep(min(backoff_secs, remaining_budget))
	timer := time.NewTimer(time.Duration(sleepSecs * float64(time.Second)))
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// raiseLLMResilienceError 构建 LLM 弹性错误并返回。
//
// 对齐 Python: _raise_llm_resilience_error()
// 使用 exception.NewBaseError + WithMsg + WithDetails + WithCause 构建错误。
func raiseLLMResilienceError(
	status exception.StatusCode,
	reason string,
	attempts int,
	lastError error,
	lastResponse string,
	cause error,
) error {
	// 对齐 Python: cause=cause or last_error
	effectiveCause := cause
	if effectiveCause == nil {
		effectiveCause = lastError
	}

	details := map[string]any{
		"reason":        reason,
		"attempts":      attempts,
		"last_response": lastResponse,
		"last_error":    errorString(lastError),
	}

	return exception.NewBaseError(
		status,
		exception.WithMsg(reason),
		exception.WithDetails(details),
		exception.WithCause(effectiveCause),
	)
}

// responseToTextFromAssistantMessage 从 AssistantMessage 提取文本内容。
//
// 对齐 Python: _response_to_text(response)
// Python 版本检查 hasattr(response, "content")，Go 版本直接使用 GetContent。
func responseToTextFromAssistantMessage(response *llmschema.AssistantMessage) string {
	if response == nil {
		return ""
	}
	content := response.GetContent()
	// MessageContent 是 struct，String() 返回空字符串表示无内容
	return content.String()
}

// errorString 安全地获取 error 的字符串表示。
func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
