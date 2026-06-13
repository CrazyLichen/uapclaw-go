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

// DashscopeEmbeddingOption DashscopeEmbedding 可选配置。
type DashscopeEmbeddingOption func(*DashscopeEmbedding)

// DashscopeEmbedding 阿里云 DashScope 向量嵌入客户端。
//
// 使用 HTTP 直接调用 DashScope 多模态嵌入 API，
// 支持文本+图片+视频多模态嵌入。
//
// 对应 Python: openjiuwen/core/retrieval/embedding/dashscope_embedding.py
type DashscopeEmbedding struct {
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
	// dimension 缓存的向量维度（0 表示未探测）
	dimension int
	// matryoshkaDimension 是否启用 Matryoshka 维度截断
	matryoshkaDimension bool
	// httpClient HTTP 客户端
	httpClient *http.Client
}

// dashscopeResponse DashScope API 响应结构
type dashscopeResponse struct {
	StatusCode int    `json:"status_code"`
	Code       string `json:"code"`
	Message    string `json:"message"`
	Output     struct {
		Embeddings []struct {
			Embedding []float64 `json:"embedding"`
			Index     int       `json:"index"`
		} `json:"embeddings"`
	} `json:"output"`
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// dashscopeDefaultBaseURL DashScope API 默认地址
	dashscopeDefaultBaseURL = "https://dashscope.aliyuncs.com/api/v1/services/embeddings/text-embedding/text-embedding"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// WithDashscopeTimeout 设置请求超时。
func WithDashscopeTimeout(d time.Duration) DashscopeEmbeddingOption {
	return func(ds *DashscopeEmbedding) { ds.timeout = d }
}

// WithDashscopeMaxRetries 设置最大重试次数。
func WithDashscopeMaxRetries(n int) DashscopeEmbeddingOption {
	return func(ds *DashscopeEmbedding) { ds.maxRetries = n }
}

// WithDashscopeMaxBatchSize 设置每批最大文档数。
func WithDashscopeMaxBatchSize(n int) DashscopeEmbeddingOption {
	return func(ds *DashscopeEmbedding) { ds.maxBatchSize = n }
}

// WithDashscopeMaxConcurrent 设置最大并发数。
func WithDashscopeMaxConcurrent(n int) DashscopeEmbeddingOption {
	return func(ds *DashscopeEmbedding) { ds.limiter = make(chan struct{}, n) }
}

// WithDashscopeDimension 设置 Matryoshka 维度截断。
func WithDashscopeDimension(dim int) DashscopeEmbeddingOption {
	return func(ds *DashscopeEmbedding) {
		ds.dimension = dim
		ds.matryoshkaDimension = true
	}
}

// WithDashscopeHTTPClient 设置自定义 HTTP 客户端。
func WithDashscopeHTTPClient(client *http.Client) DashscopeEmbeddingOption {
	return func(ds *DashscopeEmbedding) { ds.httpClient = client }
}

// NewDashscopeEmbedding 创建 DashScope 向量嵌入客户端。
func NewDashscopeEmbedding(config EmbeddingConfig, opts ...DashscopeEmbeddingOption) *DashscopeEmbedding {
	ds := &DashscopeEmbedding{
		config:       config,
		timeout:      defaultTimeout,
		maxRetries:   defaultMaxRetries,
		maxBatchSize: defaultMaxBatchSize,
		limiter:      make(chan struct{}, defaultMaxConcurrent),
	}

	for _, opt := range opts {
		opt(ds)
	}

	// HTTP 客户端
	if ds.httpClient == nil {
		ds.httpClient = NewEmbeddingHTTPClient(config.BaseURL)
	}

	return ds
}

// EmbedQuery 将单条查询文本转换为向量。
func (ds *DashscopeEmbedding) EmbedQuery(ctx context.Context, text string) ([]float64, error) {
	if strings.TrimSpace(text) == "" {
		return nil, exception.BuildError(
			exception.StatusRetrievalEmbeddingInputInvalid,
			exception.WithParam("error_msg", "嵌入查询文本为空"),
		)
	}

	embeddings, err := ds.callAPI(ctx, []map[string]any{{"text": text}})
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
func (ds *DashscopeEmbedding) EmbedDocuments(ctx context.Context, texts []string, opts ...embedding.EmbedOption) ([][]float64, error) {
	validated, err := ValidateEmbedDocs(texts)
	if err != nil {
		return nil, err
	}

	batchSize, cb := ApplyEmbedOptions(opts, ds.maxBatchSize)
	batchSize = ResolveBatchSize(batchSize, ds.maxBatchSize)
	batches := BatchTexts(validated, batchSize)

	tasks := make([]EmbeddingTask, len(batches))
	for i, batch := range batches {
		batch := batch
		i := i
		tasks[i] = func() ([][]float64, error) {
			// 转换为 DashScope 输入格式
			input := make([]map[string]any, len(batch))
			for j, text := range batch {
				input[j] = map[string]any{"text": text}
			}

			result, err := ds.callAPI(ctx, input)
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

	return ExecuteWithConcurrency(ctx, tasks, ds.limiter)
}

// EmbedMultimodal 将多模态文档转换为向量。
func (ds *DashscopeEmbedding) EmbedMultimodal(ctx context.Context, doc *MultimodalDocument, opts ...MultimodalOption) ([]float64, error) {
	if doc == nil {
		return nil, exception.BuildError(
			exception.StatusRetrievalEmbeddingInputInvalid,
			exception.WithParam("error_msg", "多模态文档为 nil"),
		)
	}

	input := []map[string]any{doc.DashscopeInput()}
	embeddings, err := ds.callAPI(ctx, input)
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

// Dimension 返回嵌入向量维度。
func (ds *DashscopeEmbedding) Dimension() int {
	if ds.dimension > 0 {
		return ds.dimension
	}

	vec, err := ds.EmbedQuery(context.Background(), "test")
	if err != nil {
		return 0
	}
	if !ds.matryoshkaDimension {
		ds.dimension = len(vec)
	}
	logger.Debug(logComponent).
		Int("dimension", len(vec)).
		Bool("matryoshka", ds.matryoshkaDimension).
		Msg("探测到嵌入向量维度")
	return len(vec)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// callAPI 调用 DashScope Embeddings API。
func (ds *DashscopeEmbedding) callAPI(ctx context.Context, input []map[string]any) ([][]float64, error) {
	return RetryWithBackoff(ctx, ds.maxRetries, func(attempt int) ([][]float64, error) {
		payload := map[string]any{
			"model": ds.config.ModelName,
			"input": input,
		}

		// Matryoshka 维度截断
		if ds.matryoshkaDimension && ds.dimension > 0 {
			payload["dimension"] = ds.dimension
		}

		body, err := json.Marshal(payload)
		if err != nil {
			return nil, exception.BuildError(
				exception.StatusRetrievalEmbeddingRequestCallFailed,
				exception.WithParam("error_msg", fmt.Sprintf("序列化请求失败: %s", err)),
			)
		}

		apiURL := ds.config.BaseURL
		if apiURL == "" {
			apiURL = dashscopeDefaultBaseURL
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(body))
		if err != nil {
			return nil, exception.BuildError(
				exception.StatusRetrievalEmbeddingRequestCallFailed,
				exception.WithParam("error_msg", fmt.Sprintf("创建请求失败: %s", err)),
			)
		}

		req.Header.Set("Content-Type", "application/json")
		if ds.config.APIKey != "" {
			req.Header.Set("Authorization", "Bearer "+ds.config.APIKey)
		}

		resp, err := ds.httpClient.Do(req)
		if err != nil {
			logger.Warn(logComponent).
				Str("event_type", "embedding_request_failed").
				Str("model_provider", "dashscope").
				Int("attempt", attempt+1).
				Int("max_retries", ds.maxRetries).
				Err(err).
				Msg("DashScope 嵌入请求失败")
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
			return nil, exception.BuildError(
				exception.StatusRetrievalEmbeddingRequestCallFailed,
				exception.WithParam("error_msg", fmt.Sprintf("HTTP 状态码 %d: %s", resp.StatusCode, string(respBody))),
			)
		}

		embeddings, err := ds.handleDashscopeAPIResp(respBody, attempt)
		if err != nil {
			return nil, err
		}

		// 自动探测 dimension
		if !ds.matryoshkaDimension && ds.dimension == 0 && len(embeddings) > 0 && len(embeddings[0]) > 0 {
			ds.dimension = len(embeddings[0])
			logger.Debug(logComponent).
				Int("dimension", ds.dimension).
				Msg("探测到嵌入向量维度")
		}

		return embeddings, nil
	})
}

// handleDashscopeAPIResp 解析 DashScope API 响应。
func (ds *DashscopeEmbedding) handleDashscopeAPIResp(body []byte, attempt int) ([][]float64, error) {
	var resp dashscopeResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, exception.BuildError(
			exception.StatusRetrievalEmbeddingResponseInvalid,
			exception.WithParam("error_msg", fmt.Sprintf("解析响应 JSON 失败: %s", err)),
		)
	}

	if resp.StatusCode != 200 {
		if attempt >= ds.maxRetries-1 {
			errMsg := fmt.Sprintf("DashScope 请求失败。HTTP 状态: %d, 错误码: %s, 错误消息: %s",
				resp.StatusCode, resp.Code, resp.Message)
			logger.Warn(logComponent).
				Str("event_type", "embedding_request_failed").
				Int("attempt", attempt+1).
				Int("max_retries", ds.maxRetries).
				Str("error_msg", errMsg).
				Msg("DashScope 嵌入请求失败")
			return nil, exception.BuildError(
				exception.StatusRetrievalEmbeddingRequestCallFailed,
				exception.WithParam("error_msg", errMsg),
			)
		}
		return nil, nil // 返回 nil 让 RetryWithBackoff 重试
	}

	if len(resp.Output.Embeddings) == 0 {
		return nil, exception.BuildError(
			exception.StatusRetrievalEmbeddingResponseInvalid,
			exception.WithParam("error_msg", fmt.Sprintf("响应中无嵌入数据: %s", string(body))),
		)
	}

	// 按 index 排序
	embeddings := make([][]float64, len(resp.Output.Embeddings))
	for _, item := range resp.Output.Embeddings {
		if item.Index < len(embeddings) {
			embeddings[item.Index] = item.Embedding
		}
	}

	return embeddings, nil
}
