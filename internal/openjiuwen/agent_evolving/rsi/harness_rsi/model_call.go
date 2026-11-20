package harnessrsi

import (
	"context"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// logComponent 组件级日志常量（agentcore 域）
const logComponent = logger.ComponentAgentCore

// ──────────────────────────── 常量 ────────────────────────────

// DefaultModelCallMaxRetries 首次尝试后的最大重试次数（20 次重试 = 21 次尝试）
const DefaultModelCallMaxRetries = 20

// DefaultModelCallRetryDelaySeconds 初始重试延迟秒数
const DefaultModelCallRetryDelaySeconds = 1.0

// DefaultModelCallMaxRetryDelaySeconds 重试延迟上限秒数
const DefaultModelCallMaxRetryDelaySeconds = 10.0

// timeoutMarkers 超时类瞬态标记（对齐 _TIMEOUT_MARKERS）
var timeoutMarkers = []string{
	"timeout", "timed out", "time out", "read timed out", "deadline",
}

// transientServiceMarkers 瞬态服务标记（对齐 _TRANSIENT_SERVICE_MARKERS）
var transientServiceMarkers = []string{
	"bad gateway", "service unavailable", "gateway timeout", "internal server error",
	"connection error", "connection reset", "connection refused", "server disconnected",
	"remote protocol error", "temporarily unavailable", "too many requests", "rate limit",
}

// transientConnectorBackendMarkers 连接器后端瞬态标记：
// 部分 OpenAI 兼容网关把连接器启动失败误报为 HTTP 401，标记刻意收窄
// 以保证真实无效凭据立即失败（对齐 _TRANSIENT_CONNECTOR_BACKEND_MARKERS）。
var transientConnectorBackendMarkers = []string{
	"database system is not yet accepting connections",
	"consistent recovery state has not been yet reached",
	"database system is starting up",
	"database system is in recovery mode",
}

// nonRetryableServiceMarkers 非瞬态服务标记（对齐 _NON_RETRYABLE_SERVICE_MARKERS）
var nonRetryableServiceMarkers = []string{
	"budget has been exceeded", "budget_exceeded", "insufficient_quota",
	"invalid api key", "authentication failed", "permission denied",
}

// transientStatusRe 瞬态状态码正则（对齐 _TRANSIENT_STATUS_RE）
var transientStatusRe = regexp.MustCompile(`(?:error\s+code|status(?:\s+code)?|http)\D{0,16}(408|409|429|5\d\d)\b`)

// transientHTMLStatusRe HTML title/h1 5xx 正则（对齐 _TRANSIENT_HTML_STATUS_RE）
var transientHTMLStatusRe = regexp.MustCompile(`<(?:title|h1)>\s*5\d\d\b`)

// ──────────────────────────── 全局变量 ────────────────────────────

// ErrRetryableModelOutput 模型返回可重试但不可用的输出（对齐 RetryableModelOutputError）
var ErrRetryableModelOutput = errors.New("retryable model output error")

// sleepFn 延迟函数（测试注入用；对齐偏差 D-7：Python 直接 asyncio.sleep）
var sleepFn = func(seconds float64) { time.Sleep(time.Duration(seconds * float64(time.Second))) }

// ──────────────────────────── 导出函数 ────────────────────────────

// RunModelCallWithRetries 运行模型调用，对瞬态失败与不可用输出重试。
// maxRetries 指首次尝试之后的重试次数：20 次重试最多 21 次尝试。
// 最后一个可重试错误原样抛出、无 fallback，下游阶段绝不消费已知坏文本。
func RunModelCallWithRetries(ctx context.Context, call func() (string, error), operationName string,
	maxRetries int, initialRetryDelaySeconds, maxRetryDelaySeconds float64) (string, error) {
	attempts := maxRetries
	if attempts < 1 {
		attempts = 1
	}
	attempts = attempts + 1
	for attempt := 1; attempt <= attempts; attempt++ {
		raw, err := call()
		if err == nil && strings.TrimSpace(raw) == "" {
			err = fmt.Errorf("%s model output is empty", operationName)
			err = fmt.Errorf("%w: %w", ErrRetryableModelOutput, err)
		}
		if err == nil {
			repaired := RepairTextMojibake(raw)
			if repaired != raw {
				unrepairedErr := fmt.Errorf("%w: %s model output contains mojibake", ErrRetryableModelOutput, operationName)
				if HasUnrepairedMojibake(repaired) {
					return "", unrepairedErr
				}
				if attempt >= attempts {
					return repaired, nil
				}
				err = unrepairedErr
			} else if HasUnrepairedMojibake(raw) {
				err = fmt.Errorf("%w: %s model output contains mojibake", ErrRetryableModelOutput, operationName)
			}
		}
		if err == nil {
			return raw, nil
		}
		if !IsRetryableModelCallFailure(err) {
			return "", err
		}
		if attempt >= attempts {
			return "", err
		}
		delay := retryDelaySeconds(err, attempt, initialRetryDelaySeconds, maxRetryDelaySeconds)
		// 对齐 Python logger.warning：attempt/attempts/delay/error_excerpt 结构化字段
		logModelCallRetry(operationName, attempt, attempts, delay, errorExcerpt(err))
		if delay > 0 {
			sleepFn(delay)
		}
	}
	// Python 的兜底 raise：call 未设置时理论不可达
	return "", fmt.Errorf("%s model call failed without result", operationName)
}

// IsRetryableModelCallFailure 返回错误是否代表瞬态模型服务失败（对齐 is_retryable_model_call_failure）
func IsRetryableModelCallFailure(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ErrRetryableModelOutput) {
		return true
	}
	message := strings.ToLower(errorText(err))
	for _, marker := range transientConnectorBackendMarkers {
		if strings.Contains(message, marker) {
			return true
		}
	}
	for _, marker := range nonRetryableServiceMarkers {
		if strings.Contains(message, marker) {
			return false
		}
	}
	for _, marker := range timeoutMarkers {
		if strings.Contains(message, marker) {
			return true
		}
	}
	for _, marker := range transientServiceMarkers {
		if strings.Contains(message, marker) {
			return true
		}
	}
	if transientStatusRe.MatchString(message) {
		return true
	}
	return transientHTMLStatusRe.MatchString(message)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// retryDelaySeconds 计算重试延迟：可重试输出错误立即重试（0 秒），其余指数退避封顶
// （对齐 _retry_delay_seconds）
func retryDelaySeconds(err error, attempt int, initialDelaySeconds, maxDelaySeconds float64) float64 {
	if errors.Is(err, ErrRetryableModelOutput) {
		return 0
	}
	initial := math.Max(0, initialDelaySeconds)
	maximum := math.Max(initial, maxDelaySeconds)
	exp := attempt - 1
	if exp < 0 {
		exp = 0
	}
	return math.Min(maximum, initial*math.Pow(2, float64(exp)))
}

// errorExcerpt 错误摘要（空白折叠，限 300 字符；对齐 _error_excerpt）
func errorExcerpt(err error, limit ...int) string {
	maxLen := 300
	if len(limit) > 0 {
		maxLen = limit[0]
	}
	text := strings.Join(strings.Fields(errorText(err)), " ")
	if len(text) <= maxLen {
		return text
	}
	return text[:maxLen] + "..."
}

// errorText 取错误的完整文本（包装链拼接）
func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// logModelCallRetry 重试告警日志（Msg 英文，字段对齐 Python f-string）
func logModelCallRetry(operationName string, attempt, attempts int, delay float64, excerpt string) {
	logger.Warn(logComponent).
		Str("operation_name", operationName).
		Int("attempt", attempt).
		Int("attempts", attempts).
		Float64("delay_seconds", delay).
		Str("error_excerpt", excerpt).
		Msg("model call attempt failed transiently; retrying")
}
