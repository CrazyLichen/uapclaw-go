package utils

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"strings"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// RetryConfig 重试配置。
//
// 对齐 Python: sync_request_with_retry / async_request_with_retry 的参数
type RetryConfig struct {
	// MaxRetries 最大重试次数，默认 3
	MaxRetries int
	// RetryWait 重试等待基数，默认 100ms
	RetryWait time.Duration
	// Task 任务类型，决定错误码前缀，默认 TaskReranker
	Task TaskName
}

// TaskName 任务类型，决定错误码前缀。
//
// 对齐 Python: Literal["Reranker", "Embedding"]
type TaskName string

// ──────────────────────────── 常量 ────────────────────────────
const (
	// TaskReranker 重排序任务
	TaskReranker TaskName = "Reranker"
	// TaskEmbedding 嵌入任务
	TaskEmbedding TaskName = "Embedding"
)

const (
	// defaultMaxRetries 默认最大重试次数
	defaultMaxRetries = 3
	// defaultRetryWait 默认重试等待基数
	defaultRetryWait = 100 * time.Millisecond
)

// ──────────────────────────── 全局变量 ────────────────────────────
var (
	// logComponent 日志组件常量
	logComponent = logger.ComponentAgentCore
	// censorshipKeywords 审查内容检测关键词，对齐 Python
	censorshipKeywords = []string{"safety", "violation", "policy", "inspection", "appropriate"}
)

// ──────────────────────────── 导出函数 ────────────────────────────

// RequestWithRetry 发送带重试的 HTTP POST 请求。
//
// 对齐 Python: async_request_with_retry。
// httpClient 由调用方创建和管理，cfg 控制重试行为。
// 返回响应的 JSON 解析结果 map[string]any。
func RequestWithRetry(
	ctx context.Context,
	httpClient *http.Client,
	url string,
	jsonBody map[string]any,
	headers map[string]string,
	cfg RetryConfig,
) (map[string]any, error) {
	return doRequestWithRetry(ctx, httpClient, url, jsonBody, headers, cfg)
}

// RequestWithRetrySync 发送带重试的同步 HTTP POST 请求。
//
// 对齐 Python: sync_request_with_retry。
// 参数和返回值与 RequestWithRetry 一致。
func RequestWithRetrySync(
	ctx context.Context,
	httpClient *http.Client,
	url string,
	jsonBody map[string]any,
	headers map[string]string,
	cfg RetryConfig,
) (map[string]any, error) {
	return doRequestWithRetry(ctx, httpClient, url, jsonBody, headers, cfg)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// doRequestWithRetry 执行带重试的 HTTP POST 请求。
// 对齐 Python: sync_request_with_retry / async_request_with_retry 的核心逻辑。
// Go 中同步和异步调用统一使用此函数（Go 的 goroutine 调度由调用方控制）。
func doRequestWithRetry(
	ctx context.Context,
	httpClient *http.Client,
	url string,
	jsonBody map[string]any,
	headers map[string]string,
	cfg RetryConfig,
) (map[string]any, error) {
	maxRetries := cfg.MaxRetries
	if maxRetries <= 0 {
		maxRetries = defaultMaxRetries
	}
	retryWait := cfg.RetryWait
	if retryWait <= 0 {
		retryWait = defaultRetryWait
	}
	task := cfg.Task
	if task == "" {
		task = TaskReranker
	}

	shouldRetry := false
	respStr := "No request sent"
	var response *http.Response
	var lastError error

	for backoff := 1; backoff <= maxRetries; backoff++ {
		// 重试退避：线性退避 + 抖动，对齐 Python random.random() * retry_wait * backoff
		if shouldRetry {
			waitDuration := time.Duration(rand.Float64()*float64(retryWait)) * time.Duration(backoff)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(waitDuration):
			}
			shouldRetry = false
		}

		// 序列化请求体
		body, err := json.Marshal(jsonBody)
		if err != nil {
			return nil, exception.BuildError(
				requestCallFailedStatus(task),
				exception.WithParam("error_msg", fmt.Sprintf("序列化请求失败: %s", err)),
			)
		}

		// 创建 HTTP 请求
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			return nil, exception.BuildError(
				requestCallFailedStatus(task),
				exception.WithParam("error_msg", fmt.Sprintf("创建请求失败: %s", err)),
			)
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}

		// 发送请求
		resp, err := httpClient.Do(req)
		if err != nil {
			respStr = err.Error()
			lastError = err
			shouldRetry = true
			continue
		}

		response = resp
		// 解析响应
		respBody, err := readResponseBody(response)
		if err != nil {
			respStr = err.Error()
			shouldRetry = true
			continue
		}
		respStr = string(respBody)

		// 按状态码处理
		result, handled := handleResponseByStatus(response, respBody, task)
		if handled {
			return result, nil
		}

		// 需要重试的状态码
		if isRetryableStatus(response.StatusCode) {
			shouldRetry = true
			continue
		}

		// 其他状态码不重试
		break
	}

	// 超过最大重试次数
	return nil, raiseErrors(task, maxRetries, respStr, response, lastError)
}

// handleResponseByStatus 按状态码处理 HTTP 响应。
// 返回 (result, handled)：handled=true 时表示请求成功或失败已确定，不再重试。
// 对齐 Python: _handle_response_by_status
func handleResponseByStatus(resp *http.Response, body []byte, task TaskName) (map[string]any, bool) {
	switch resp.StatusCode {
	case http.StatusOK:
		// 200：成功，解析 JSON 返回
		if len(body) == 0 {
			return map[string]any{}, true
		}
		var result map[string]any
		if err := json.Unmarshal(body, &result); err != nil {
			return nil, false
		}
		return result, true

	case http.StatusBadRequest:
		// 400：客户端错误，不重试
		logger.Error(logComponent).
			Str("event_type", "api_request_error").
			Str("task", string(task)).
			Str("response", string(body)).
			Msg("API 请求错误")

		// 检测审查内容关键词，对齐 Python
		var respJSON map[string]any
		if err := json.Unmarshal(body, &respJSON); err == nil {
			errObj, _ := respJSON["error"].(map[string]any)
			if errObj == nil {
				errObj = respJSON
			}
			errorCode := strings.ToLower(
				fmt.Sprintf("%v%v%v",
					errObj["code"], errObj["message"], errObj["content"]))
			for _, kw := range censorshipKeywords {
				if strings.Contains(errorCode, kw) {
					logger.Warn(logComponent).
						Str("event_type", "censored_content").
						Str("task", string(task)).
						Msg("请求可能包含被审查的内容")
					break
				}
			}
		}
		return nil, false

	default:
		return nil, false
	}
}

// isRetryableStatus 判断 HTTP 状态码是否可重试。
// 对齐 Python: 429/500/503 触发重试
func isRetryableStatus(statusCode int) bool {
	return statusCode == http.StatusTooManyRequests ||
		statusCode == http.StatusInternalServerError ||
		statusCode == http.StatusServiceUnavailable
}

// readResponseBody 读取 HTTP 响应体。
func readResponseBody(resp *http.Response) ([]byte, error) {
	defer func() { _ = resp.Body.Close() }()
	var buf bytes.Buffer
	_, err := buf.ReadFrom(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应体失败: %w", err)
	}
	return buf.Bytes(), nil
}

// raiseErrors 超过最大重试次数后构建错误。
// 对齐 Python: _raise_errors
func raiseErrors(task TaskName, maxRetries int, respStr string, resp *http.Response, lastError error) error {
	logger.Error(logComponent).
		Str("event_type", "api_request_exhausted").
		Str("task", string(task)).
		Int("max_retries", maxRetries).
		Str("response", respStr).
		Msg("API 请求重试耗尽")

	if resp != nil {
		// 有 HTTP 响应 → RequestCallFailed
		return exception.BuildError(
			requestCallFailedStatus(task),
			exception.WithParam("error_msg", fmt.Sprintf("Failed to get %s after %d attempts: HTTP %d", task, maxRetries, resp.StatusCode)),
			exception.WithCause(lastError),
		)
	}
	// 无 HTTP 响应 → UnreachableCallFailed
	return exception.BuildError(
		unreachableCallFailedStatus(task),
		exception.WithParam("error_msg", fmt.Sprintf("Failed to get %s after %d attempts", task, maxRetries)),
		exception.WithCause(lastError),
	)
}

// requestCallFailedStatus 根据 TaskName 返回对应的请求调用失败错误码。
func requestCallFailedStatus(task TaskName) exception.StatusCode {
	switch task {
	case TaskEmbedding:
		return exception.StatusRetrievalEmbeddingRequestCallFailed
	default:
		return exception.StatusRetrievalRerankerRequestCallFailed
	}
}

// unreachableCallFailedStatus 根据 TaskName 返回对应的不可达调用失败错误码。
func unreachableCallFailedStatus(task TaskName) exception.StatusCode {
	switch task {
	case TaskEmbedding:
		return exception.StatusRetrievalEmbeddingUnreachableCallFailed
	default:
		return exception.StatusRetrievalRerankerUnreachableCallFailed
	}
}
