package embedding

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/packages/param"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/store/embedding"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// OpenAIEmbeddingOption OpenAIEmbedding 可选配置。
type OpenAIEmbeddingOption func(*OpenAIEmbedding)

// OpenAIEmbedding OpenAI 向量嵌入客户端。
//
// 使用 openai-go SDK，支持 Matryoshka 维度截断和多模态嵌入。
//
// 对应 Python: openjiuwen/core/retrieval/embedding/openai_embedding.py
type OpenAIEmbedding struct {
	// config 嵌入配置
	config EmbeddingConfig
	// client OpenAI SDK 客户端
	client openai.Client
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
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// WithOpenAITimeout 设置请求超时。
func WithOpenAITimeout(d time.Duration) OpenAIEmbeddingOption {
	return func(o *OpenAIEmbedding) { o.timeout = d }
}

// WithOpenAIMaxRetries 设置最大重试次数。
func WithOpenAIMaxRetries(n int) OpenAIEmbeddingOption {
	return func(o *OpenAIEmbedding) { o.maxRetries = n }
}

// WithOpenAIMaxBatchSize 设置每批最大文档数。
func WithOpenAIMaxBatchSize(n int) OpenAIEmbeddingOption {
	return func(o *OpenAIEmbedding) { o.maxBatchSize = n }
}

// WithOpenAIMaxConcurrent 设置最大并发数。
func WithOpenAIMaxConcurrent(n int) OpenAIEmbeddingOption {
	return func(o *OpenAIEmbedding) { o.limiter = make(chan struct{}, n) }
}

// WithOpenAIDimension 设置 Matryoshka 维度截断。
func WithOpenAIDimension(dim int) OpenAIEmbeddingOption {
	return func(o *OpenAIEmbedding) {
		o.dimension = dim
		o.matryoshkaDimension = true
	}
}

// WithOpenAIHTTPClient 设置自定义 HTTP 客户端。
func WithOpenAIHTTPClient(client *http.Client) OpenAIEmbeddingOption {
	return func(o *OpenAIEmbedding) {
		opts := []option.RequestOption{
			option.WithHTTPClient(client),
		}
		cl := openai.NewClient(opts...)
		o.client = cl
	}
}

// NewOpenAIEmbedding 创建 OpenAI 向量嵌入客户端。
func NewOpenAIEmbedding(config EmbeddingConfig, opts ...OpenAIEmbeddingOption) *OpenAIEmbedding {
	o := &OpenAIEmbedding{
		config:       config,
		timeout:      defaultTimeout,
		maxRetries:   defaultMaxRetries,
		maxBatchSize: defaultMaxBatchSize,
		limiter:      make(chan struct{}, defaultMaxConcurrent),
	}

	// 处理 API URL：移除末尾 / 和 /embeddings
	apiURL := strings.TrimSuffix(config.BaseURL, "/")
	apiURL = strings.TrimSuffix(apiURL, "/embeddings")

	// 构造 SDK 客户端选项
	clientOpts := []option.RequestOption{
		option.WithBaseURL(apiURL),
		option.WithMaxRetries(o.maxRetries),
	}
	if config.APIKey != "" {
		clientOpts = append(clientOpts, option.WithAPIKey(config.APIKey))
	}

	o.client = openai.NewClient(clientOpts...)
	for _, opt := range opts {
		opt(o)
	}

	return o
}

// EmbedQuery 将单条查询文本转换为向量。
func (o *OpenAIEmbedding) EmbedQuery(ctx context.Context, text string) ([]float64, error) {
	if strings.TrimSpace(text) == "" {
		return nil, exception.BuildError(
			exception.StatusRetrievalEmbeddingInputInvalid,
			exception.WithParam("error_msg", "嵌入查询文本为空"),
		)
	}

	embeddings, err := o.callAPI(ctx, []string{text})
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
func (o *OpenAIEmbedding) EmbedDocuments(ctx context.Context, texts []string, opts ...embedding.EmbedOption) ([][]float64, error) {
	validated, err := ValidateEmbedDocs(texts)
	if err != nil {
		return nil, err
	}

	batchSize, cb := ApplyEmbedOptions(opts, o.maxBatchSize)
	batchSize = ResolveBatchSize(batchSize, o.maxBatchSize)
	batches := BatchTexts(validated, batchSize)

	tasks := make([]EmbeddingTask, len(batches))
	for i, batch := range batches {
		batch := batch
		i := i
		tasks[i] = func() ([][]float64, error) {
			result, err := o.callAPI(ctx, batch)
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

	return ExecuteWithConcurrency(ctx, tasks, o.limiter)
}

// EmbedMultimodal 将多模态文档转换为向量。
func (o *OpenAIEmbedding) EmbedMultimodal(ctx context.Context, doc *MultimodalDocument, opts ...MultimodalOption) ([]float64, error) {
	if doc == nil {
		return nil, exception.BuildError(
			exception.StatusRetrievalEmbeddingInputInvalid,
			exception.WithParam("error_msg", "多模态文档为 nil"),
		)
	}

	// OpenAI 多模态嵌入：将 content 作为 input 传入
	// 目前 OpenAI embedding 不直接支持多模态 content，这里以文本回退
	// 后续如果 OpenAI 支持多模态嵌入 API，可以改为传 doc.Content()
	text := doc.Text
	if text == "" {
		return nil, exception.BuildError(
			exception.StatusRetrievalEmbeddingInputInvalid,
			exception.WithParam("error_msg", "多模态文档的文本回退字段为空"),
		)
	}

	return o.EmbedQuery(ctx, text)
}

// Dimension 返回嵌入向量维度。
func (o *OpenAIEmbedding) Dimension() int {
	if o.dimension > 0 {
		return o.dimension
	}

	vec, err := o.EmbedQuery(context.Background(), "test")
	if err != nil {
		return 0
	}
	if !o.matryoshkaDimension {
		o.dimension = len(vec)
	}
	logger.Debug(logComponent).
		Int("dimension", len(vec)).
		Bool("matryoshka", o.matryoshkaDimension).
		Msg("探测到嵌入向量维度")
	return len(vec)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// callAPI 调用 OpenAI Embeddings API。
func (o *OpenAIEmbedding) callAPI(ctx context.Context, texts []string) ([][]float64, error) {
	return RetryWithBackoff(ctx, o.maxRetries, func(attempt int) ([][]float64, error) {
		params := openai.EmbeddingNewParams{
			Model: o.config.ModelName,
			Input: openai.EmbeddingNewParamsInputUnion{
				OfArrayOfStrings: texts,
			},
		}

		// Matryoshka 维度截断
		if o.matryoshkaDimension && o.dimension > 0 {
			params.Dimensions = param.Opt[int64]{Value: int64(o.dimension)}
		}

		resp, err := o.client.Embeddings.New(ctx, params)
		if err != nil {
			logger.Warn(logComponent).
				Str("event_type", "embedding_request_failed").
				Str("model_provider", "openai").
				Int("attempt", attempt+1).
				Int("max_retries", o.maxRetries).
				Err(err).
				Msg("OpenAI 嵌入请求失败")
			return nil, err
		}

		// 按 index 排序
		embeddings := make([][]float64, len(resp.Data))
		for _, item := range resp.Data {
			if int(item.Index) < len(embeddings) {
				embeddings[item.Index] = item.Embedding
			}
		}

		// 自动探测 dimension
		if !o.matryoshkaDimension && o.dimension == 0 && len(embeddings) > 0 && len(embeddings[0]) > 0 {
			o.dimension = len(embeddings[0])
			logger.Debug(logComponent).
				Int("dimension", o.dimension).
				Msg("探测到嵌入向量维度")
		}

		return embeddings, nil
	})
}

// parseOpenAIResponse 解析 OpenAI SDK 响应对象。
// 当前 SDK 直接返回 []Embedding，embedding 已是 []float64，无需 base64 解析。
// 此函数保留用于日志和验证。
func parseOpenAIResponse(data []openai.Embedding) ([][]float64, error) {
	if len(data) == 0 {
		return nil, exception.BuildError(
			exception.StatusRetrievalEmbeddingResponseInvalid,
			exception.WithParam("error_msg", "响应中无嵌入数据"),
		)
	}

	embeddings := make([][]float64, len(data))
	for _, item := range data {
		if int(item.Index) < len(embeddings) {
			embeddings[item.Index] = item.Embedding
		}
	}

	// 检查是否有有效数据
	validCount := 0
	for _, emb := range embeddings {
		if len(emb) > 0 {
			validCount++
		}
	}
	if validCount == 0 {
		return nil, exception.BuildError(
			exception.StatusRetrievalEmbeddingResponseInvalid,
			exception.WithParam("error_msg", fmt.Sprintf("响应中 %d 条数据无有效嵌入向量", len(data))),
		)
	}

	return embeddings, nil
}
