package embedding

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/embedding"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
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
	// dimMu 保护 dimension 字段的互斥锁
	dimMu sync.Mutex
	// matryoshkaDimension 是否启用 Matryoshka 维度截断
	matryoshkaDimension bool
	// extraHeaders 额外请求头，合并到 HTTP 请求中
	// 对齐 Python DashScopeEmbedding 构造函数的 extra_headers 参数
	extraHeaders map[string]string
	// httpClient HTTP 客户端
	httpClient *http.Client
}

// dashscopeResponse DashScope API 响应结构
type dashscopeResponse struct {
	StatusCode int    `json:"status_code"`
	Code       string `json:"code"`
	Message    string `json:"message"`
	RequestID  string `json:"request_id"`
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

// WithDashscopeExtraHeaders 设置额外请求头，合并到 HTTP 请求中。
// 对齐 Python DashScopeEmbedding 构造函数的 extra_headers 参数。
func WithDashscopeExtraHeaders(headers map[string]string) DashscopeEmbeddingOption {
	return func(ds *DashscopeEmbedding) {
		if ds.extraHeaders == nil {
			ds.extraHeaders = make(map[string]string)
		}
		for k, v := range headers {
			ds.extraHeaders[k] = v
		}
	}
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

// dashscopeInputMode DashScope API 输入模式
type dashscopeInputMode int

const (
	// dashscopeInputTexts 纯文本模式，input 使用 {"texts": [...]} 格式
	dashscopeInputTexts dashscopeInputMode = iota
	// dashscopeInputMultimodal 多模态模式，input 使用 [{...}] 格式
	dashscopeInputMultimodal
)

// EmbedQuery 将单条查询文本转换为向量。
func (ds *DashscopeEmbedding) EmbedQuery(ctx context.Context, text string, opts ...embedding.EmbedOption) ([]float64, error) {
	if strings.TrimSpace(text) == "" {
		return nil, exception.BuildError(
			exception.StatusRetrievalEmbeddingInputInvalid,
			exception.WithParam("error_msg", "嵌入查询文本为空"),
		)
	}

	embeddings, err := ds.callAPI(ctx, []string{text}, dashscopeInputTexts)
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
			result, err := ds.callAPI(ctx, batch, dashscopeInputTexts)
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
func (ds *DashscopeEmbedding) EmbedMultimodal(ctx context.Context, doc *common.MultimodalDocument, opts ...MultimodalOption) ([]float64, error) {
	if doc == nil {
		return nil, exception.BuildError(
			exception.StatusRetrievalEmbeddingInputInvalid,
			exception.WithParam("error_msg", "多模态文档为 nil"),
		)
	}

	dsInput, err := doc.DashscopeInput()
	if err != nil {
		return nil, err
	}
	input := []map[string]any{dsInput}
	embeddings, err := ds.callAPI(ctx, input, dashscopeInputMultimodal)
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
	ds.dimMu.Lock()
	if ds.dimension > 0 {
		dim := ds.dimension
		ds.dimMu.Unlock()
		return dim
	}
	ds.dimMu.Unlock()

	vec, err := ds.EmbedQuery(context.Background(), "test")
	if err != nil {
		return 0
	}
	if !ds.matryoshkaDimension {
		ds.dimMu.Lock()
		ds.dimension = len(vec)
		ds.dimMu.Unlock()
	}
	logger.Debug(logComponent).
		Int("dimension", len(vec)).
		Bool("matryoshka", ds.matryoshkaDimension).
		Msg("探测到嵌入向量维度")
	return len(vec)
}

// DimensionWithContext 返回嵌入向量维度，支持 context 取消。
// 对齐 T-04 修复：替代 Dimension() 的 context.Background() 阻塞问题。
func (ds *DashscopeEmbedding) DimensionWithContext(ctx context.Context) (int, error) {
	ds.dimMu.Lock()
	if ds.dimension > 0 {
		dim := ds.dimension
		ds.dimMu.Unlock()
		return dim, nil
	}
	ds.dimMu.Unlock()

	vec, err := ds.EmbedQuery(ctx, "test")
	if err != nil {
		return 0, fmt.Errorf("探测嵌入向量维度失败: %w", err)
	}
	dim := len(vec)
	if !ds.matryoshkaDimension {
		ds.dimMu.Lock()
		ds.dimension = dim
		ds.dimMu.Unlock()
	}
	logger.Debug(logComponent).
		Int("dimension", dim).
		Bool("matryoshka", ds.matryoshkaDimension).
		Msg("探测到嵌入向量维度")
	return dim, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// callAPI 调用 DashScope Embeddings API。
// textInput 用于纯文本模式，multimodalInput 用于多模态模式。
// mode 决定 input 字段的格式：纯文本用 {"texts": [...]}，多模态用 [{...}]。
func (ds *DashscopeEmbedding) callAPI(ctx context.Context, input interface{}, mode dashscopeInputMode) ([][]float64, error) {
	return RetryWithBackoff(ctx, ds.maxRetries, func(attempt int) ([][]float64, error) {
		// 根据 mode 构造 input 字段
		var payloadInput interface{}
		switch mode {
		case dashscopeInputTexts:
			// 纯文本模式：input 使用 {"texts": [...]} 格式
			texts, ok := input.([]string)
			if !ok {
				return nil, exception.BuildError(
					exception.StatusRetrievalEmbeddingInputInvalid,
					exception.WithParam("error_msg", "纯文本模式需要 []string 输入"),
				)
			}
			payloadInput = map[string]any{"texts": texts}
		case dashscopeInputMultimodal:
			// 多模态模式：input 使用 [{...}] 格式
			payloadInput = input
		default:
			return nil, exception.BuildError(
				exception.StatusRetrievalEmbeddingInputInvalid,
				exception.WithParam("error_msg", fmt.Sprintf("不支持的输入模式: %d", mode)),
			)
		}

		payload := map[string]any{
			"model": ds.config.ModelName,
			"input": payloadInput,
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
		// 合并 extra_headers，对齐 Python extra_headers 参数
		for k, v := range ds.extraHeaders {
			req.Header.Set(k, v)
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
		defer func() { _ = resp.Body.Close() }()

		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, exception.BuildError(
				exception.StatusRetrievalEmbeddingResponseInvalid,
				exception.WithParam("error_msg", fmt.Sprintf("读取响应失败: %s", err)),
			)
		}

		// 对齐 Python: 所有 HTTP 错误（含 5xx）都可重试
		if resp.StatusCode != http.StatusOK {
			// 4xx 客户端错误（不含 429）不可重试
			if resp.StatusCode >= 400 && resp.StatusCode < 500 && resp.StatusCode != http.StatusTooManyRequests {
				return nil, exception.ValidateError(
					exception.StatusRetrievalEmbeddingRequestCallFailed,
					exception.WithParam("error_msg", fmt.Sprintf("HTTP 客户端错误 %d: %s", resp.StatusCode, string(respBody))),
				)
			}
			// 5xx 服务端错误和 429 限流错误可重试，使用 Execution 类别确保可重试
			apiErr := exception.BuildError(
				exception.StatusRetrievalEmbeddingRequestCallFailed,
				exception.WithParam("error_msg", fmt.Sprintf("HTTP 状态码 %d: %s", resp.StatusCode, string(respBody))),
			)
			apiErr.SetCategory(exception.ErrorCategoryExecution)
			return nil, apiErr
		}

		embeddings, err := ds.handleDashscopeAPIResp(respBody, attempt)
		if err != nil {
			return nil, err
		}

		// 自动探测 dimension
		if !ds.matryoshkaDimension {
			ds.dimMu.Lock()
			if ds.dimension == 0 && len(embeddings) > 0 && len(embeddings[0]) > 0 {
				ds.dimension = len(embeddings[0])
				logger.Debug(logComponent).
					Int("dimension", ds.dimension).
					Msg("探测到嵌入向量维度")
			}
			ds.dimMu.Unlock()
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
		errMsg := fmt.Sprintf("DashScope 请求失败。HTTP 状态: %d, 错误码: %s, 错误消息: %s",
			resp.StatusCode, resp.Code, resp.Message)
		logger.Warn(logComponent).
			Str("event_type", "embedding_request_failed").
			Str("model_provider", "dashscope").
			Str("request_id", resp.RequestID).
			Int("attempt", attempt+1).
			Int("max_retries", ds.maxRetries).
			Str("error_msg", errMsg).
			Msg("DashScope 嵌入请求失败")
		// 返回可重试错误，让 RetryWithBackoff 正确重试
		// 对齐 Python: 所有 DashScope API 错误都可重试
		err := exception.BuildError(
			exception.StatusRetrievalEmbeddingRequestCallFailed,
			exception.WithParam("error_msg", errMsg),
		)
		err.SetCategory(exception.ErrorCategoryExecution)
		return nil, err
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
