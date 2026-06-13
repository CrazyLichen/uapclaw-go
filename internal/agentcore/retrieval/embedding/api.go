package embedding

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/store/embedding"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// APIEmbeddingOption APIEmbedding 可选配置。
type APIEmbeddingOption func(*APIEmbedding)

// APIEmbedding 通用 HTTP 嵌入客户端。
//
// 支持 OpenAI 兼容的三种响应格式：embedding/embeddings/data[]。
// 使用 net/http 标准库，无第三方依赖。
//
// 对应 Python: openjiuwen/core/retrieval/embedding/api_embedding.py
type APIEmbedding struct {
	// config 嵌入配置
	config EmbeddingConfig
	// timeout 请求超时
	timeout time.Duration
	// maxRetries 最大重试次数
	maxRetries int
	// maxBatchSize 每批最大文档数
	maxBatchSize int
	// limiter 并发信号量
	limiter chan struct{}
	// headers 请求头
	headers map[string]string
	// extraParams 额外请求参数，合并到 API payload 中。
	// 对齐 Python **kwargs 透传机制，支持 encoding_format、dimensions、user 等参数。
	extraParams map[string]any
	// dimension 缓存的向量维度（0 表示未探测）
	dimension int
	// httpClient HTTP 客户端
	httpClient *http.Client
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// WithAPITimeout 设置请求超时。
func WithAPITimeout(d time.Duration) APIEmbeddingOption {
	return func(a *APIEmbedding) { a.timeout = d }
}

// WithAPIMaxRetries 设置最大重试次数。
func WithAPIMaxRetries(n int) APIEmbeddingOption {
	return func(a *APIEmbedding) { a.maxRetries = n }
}

// WithAPIMaxBatchSize 设置每批最大文档数。
func WithAPIMaxBatchSize(n int) APIEmbeddingOption {
	return func(a *APIEmbedding) { a.maxBatchSize = n }
}

// WithAPIMaxConcurrent 设置最大并发数。
func WithAPIMaxConcurrent(n int) APIEmbeddingOption {
	return func(a *APIEmbedding) { a.limiter = make(chan struct{}, n) }
}

// WithAPIExtraHeaders 设置额外请求头。
func WithAPIExtraHeaders(headers map[string]string) APIEmbeddingOption {
	return func(a *APIEmbedding) {
		for k, v := range headers {
			a.headers[k] = v
		}
	}
}

// WithAPIHTTPClient 设置自定义 HTTP 客户端。
func WithAPIHTTPClient(client *http.Client) APIEmbeddingOption {
	return func(a *APIEmbedding) { a.httpClient = client }
}

// WithAPIExtraParams 设置额外请求参数，合并到 API payload 中。
// 对齐 Python **kwargs 透传机制，支持 encoding_format、dimensions、user 等参数。
func WithAPIExtraParams(params map[string]any) APIEmbeddingOption {
	return func(a *APIEmbedding) {
		if a.extraParams == nil {
			a.extraParams = make(map[string]any)
		}
		for k, v := range params {
			a.extraParams[k] = v
		}
	}
}

// NewAPIEmbedding 创建通用 HTTP 嵌入客户端。
func NewAPIEmbedding(config EmbeddingConfig, opts ...APIEmbeddingOption) *APIEmbedding {
	a := &APIEmbedding{
		config:       config,
		timeout:      defaultTimeout,
		maxRetries:   defaultMaxRetries,
		maxBatchSize: defaultMaxBatchSize,
		limiter:      make(chan struct{}, defaultMaxConcurrent),
		headers:      make(map[string]string),
	}

	// 默认 headers
	a.headers["Content-Type"] = "application/json"
	if config.APIKey != "" {
		a.headers["Authorization"] = "Bearer " + config.APIKey
	}

	for _, opt := range opts {
		opt(a)
	}

	// HTTP 客户端
	if a.httpClient == nil {
		a.httpClient = NewEmbeddingHTTPClient(config.BaseURL)
	}

	return a
}

// EmbedQuery 将单条查询文本转换为向量。
func (a *APIEmbedding) EmbedQuery(ctx context.Context, text string) ([]float64, error) {
	if strings.TrimSpace(text) == "" {
		return nil, exception.BuildError(
			exception.StatusRetrievalEmbeddingInputInvalid,
			exception.WithParam("error_msg", "嵌入查询文本为空"),
		)
	}

	embeddings, err := a.getEmbeddings(ctx, text)
	if err != nil {
		return nil, err
	}
	if len(embeddings) == 0 {
		return nil, exception.BuildError(
			exception.StatusRetrievalEmbeddingResponseInvalid,
			exception.WithParam("error_msg", "嵌入响应为空"),
		)
	}
	return embeddings[0], nil
}

// EmbedDocuments 将多条文档文本批量转换为向量。
func (a *APIEmbedding) EmbedDocuments(ctx context.Context, texts []string, opts ...embedding.EmbedOption) ([][]float64, error) {
	validated, err := ValidateEmbedDocs(texts)
	if err != nil {
		return nil, err
	}

	batchSize, cb := ApplyEmbedOptions(opts, a.maxBatchSize)
	batchSize = ResolveBatchSize(batchSize, a.maxBatchSize)
	batches := BatchTexts(validated, batchSize)

	tasks := make([]EmbeddingTask, len(batches))
	for i, batch := range batches {
		batch := batch
		i := i
		tasks[i] = func() ([][]float64, error) {
			result, err := a.getEmbeddings(ctx, batch)
			if err != nil {
				return nil, err
			}
			if cb != nil {
				startIdx := i * batchSize
				endIdx := startIdx + len(batch)
				cb.OnBatchComplete(startIdx, endIdx, batch)
			}
			return result, nil
		}
	}

	return ExecuteWithConcurrency(ctx, tasks, a.limiter)
}

// Dimension 返回嵌入向量维度。
func (a *APIEmbedding) Dimension() int {
	if a.dimension > 0 {
		return a.dimension
	}
	// 发送探测请求
	vec, err := a.EmbedQuery(context.Background(), "test")
	if err != nil {
		return 0
	}
	a.dimension = len(vec)
	logger.Debug(logComponent).
		Int("dimension", a.dimension).
		Msg("探测到嵌入向量维度")
	return a.dimension
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// apiIsRetryable 判断 APIEmbedding 错误是否可重试。
// 4xx 客户端错误不可重试，其他错误（5xx、网络错误等）可重试。
func apiIsRetryable(err error) bool {
	if baseErr, ok := err.(*exception.BaseError); ok {
		// 检查是否为 4xx 客户端错误（由 getEmbeddings 中标记的 Validation 类别）
		if !baseErr.IsRecoverable() {
			return false
		}
	}
	return true
}

// getEmbeddings 发送 HTTP POST 请求获取嵌入向量。
func (a *APIEmbedding) getEmbeddings(ctx context.Context, input interface{}) ([][]float64, error) {
	return retryWithBackoffGeneric(ctx, a.maxRetries, func(attempt int) ([][]float64, error) {
		payload := map[string]interface{}{
			"model": a.config.ModelName,
			"input": input,
		}
		// 合并额外参数，对齐 Python **kwargs 透传
		for k, v := range a.extraParams {
			payload[k] = v
		}

		body, err := json.Marshal(payload)
		if err != nil {
			return nil, exception.BuildError(
				exception.StatusRetrievalEmbeddingRequestCallFailed,
				exception.WithParam("error_msg", fmt.Sprintf("序列化请求失败: %s", err)),
			)
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.config.BaseURL, bytes.NewReader(body))
		if err != nil {
			return nil, exception.BuildError(
				exception.StatusRetrievalEmbeddingRequestCallFailed,
				exception.WithParam("error_msg", fmt.Sprintf("创建请求失败: %s", err)),
			)
		}

		for k, v := range a.headers {
			req.Header.Set(k, v)
		}

		resp, err := a.httpClient.Do(req)
		if err != nil {
			logger.Warn(logComponent).
				Str("event_type", "embedding_request_failed").
				Str("model_provider", a.config.ModelName).
				Int("attempt", attempt+1).
				Int("max_retries", a.maxRetries).
				Err(err).
				Msg("嵌入 HTTP 请求失败")
			return nil, err
		}
		defer resp.Body.Close()

		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, exception.BuildError(
				exception.StatusRetrievalEmbeddingResponseInvalid,
				exception.WithParam("error_msg", fmt.Sprintf("读取响应失败: %s", err)),
			)
		}

		if resp.StatusCode != http.StatusOK {
			// 4xx 客户端错误不可重试（对齐 Python 只重试网络错误的行为）
			if resp.StatusCode >= 400 && resp.StatusCode < 500 {
				return nil, exception.ValidateError(
					exception.StatusRetrievalEmbeddingRequestCallFailed,
					exception.WithParam("error_msg", fmt.Sprintf("HTTP 客户端错误 %d: %s", resp.StatusCode, string(respBody))),
				)
			}
			// 5xx 服务端错误可重试，使用 Execution 类别确保可重试
			err := exception.BuildError(
				exception.StatusRetrievalEmbeddingRequestCallFailed,
				exception.WithParam("error_msg", fmt.Sprintf("HTTP 服务端错误 %d: %s", resp.StatusCode, string(respBody))),
			)
			// 强制设置为 Execution 类别，避免 keyword 规则将 "CALL" 匹配为 Framework
			err.SetCategory(exception.ErrorCategoryExecution)
			return nil, err
		}

		embeddings, err := ParseEmbeddingResponse(respBody)
		if err != nil {
			return nil, err
		}

		// 自动探测并缓存 dimension
		if a.dimension == 0 && len(embeddings) > 0 && len(embeddings[0]) > 0 {
			a.dimension = len(embeddings[0])
			logger.Debug(logComponent).
				Int("dimension", a.dimension).
				Msg("探测到嵌入向量维度")
		}

		return embeddings, nil
	}, apiIsRetryable)
}
