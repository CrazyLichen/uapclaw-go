package embedding

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/packages/param"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/embedding"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 结构体 ────────────────────────────

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
	// dimOnce 保证维度探测只执行一次，消除 TOCTOU 竞态，对齐 T-04 修复。
	dimOnce sync.Once
	// matryoshkaDimension 是否启用 Matryoshka 维度截断
	matryoshkaDimension bool
	// extraHeaders 额外请求头，透传给 OpenAI SDK
	extraHeaders map[string]string
	// extraParams 额外请求参数，通过 option.WithJSONSet 透传给 SDK。
	// 对齐 Python **kwargs 透传机制，支持 encoding_format、user 等参数。
	extraParams map[string]any
	// httpClient 自定义 HTTP 客户端（可选）
	httpClient *http.Client
}

// OpenAIEmbeddingOption OpenAIEmbedding 可选配置。
type OpenAIEmbeddingOption func(*OpenAIEmbedding)

// ──────────────────────────── 导出函数 ────────────────────────────

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

// WithOpenAIExtraHeaders 设置额外请求头，透传给 OpenAI SDK。
// 对齐 Python OpenAIEmbedding 中的 default_headers=self._headers。
func WithOpenAIExtraHeaders(headers map[string]string) OpenAIEmbeddingOption {
	return func(o *OpenAIEmbedding) {
		if o.extraHeaders == nil {
			o.extraHeaders = make(map[string]string)
		}
		for k, v := range headers {
			o.extraHeaders[k] = v
		}
	}
}

// WithOpenAIHTTPClient 设置自定义 HTTP 客户端。
func WithOpenAIHTTPClient(client *http.Client) OpenAIEmbeddingOption {
	return func(o *OpenAIEmbedding) {
		o.httpClient = client
	}
}

// WithOpenAIExtraParams 设置额外请求参数，通过 option.WithJSONSet 透传给 SDK。
// 对齐 Python **kwargs 透传机制，支持 encoding_format、user 等参数。
func WithOpenAIExtraParams(params map[string]any) OpenAIEmbeddingOption {
	return func(o *OpenAIEmbedding) {
		if o.extraParams == nil {
			o.extraParams = make(map[string]any)
		}
		for k, v := range params {
			o.extraParams[k] = v
		}
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

	// 先应用 Option，确保 extraHeaders / httpClient 等字段在构造 SDK 客户端之前就位
	for _, opt := range opts {
		opt(o)
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
	// 透传 extra_headers 给 SDK，对齐 Python default_headers=self._headers
	for k, v := range o.extraHeaders {
		clientOpts = append(clientOpts, option.WithHeader(k, v))
	}
	// 透传自定义 HTTP 客户端
	if o.httpClient != nil {
		clientOpts = append(clientOpts, option.WithHTTPClient(o.httpClient))
	}

	o.client = openai.NewClient(clientOpts...)

	return o
}

// EmbedQuery 将单条查询文本转换为向量。
func (o *OpenAIEmbedding) EmbedQuery(ctx context.Context, text string, opts ...embedding.EmbedOption) ([]float64, error) {
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
func (o *OpenAIEmbedding) EmbedMultimodal(ctx context.Context, doc *common.MultimodalDocument, opts ...MultimodalOption) ([]float64, error) {
	if doc == nil {
		return nil, exception.BuildError(
			exception.StatusRetrievalEmbeddingInputInvalid,
			exception.WithParam("error_msg", "多模态文档为 nil"),
		)
	}

	// OpenAI 多模态嵌入：将 content 中的文本作为 input 传入
	// 目前 OpenAI embedding 不直接支持多模态 content，这里以文本回退
	// 后续如果 OpenAI 支持多模态嵌入 API，可以改为传 doc.Content()
	// 对齐 G-25 修复：AddField(ModalityText,...) 不再更新 doc.Text，
	// 因此需要从 Content() 中提取文本字段作为回退。
	text := doc.Text
	if text == "" {
		// 从 Content() 中提取文本字段
		for _, item := range doc.Content() {
			if t, ok := item["type"].(string); ok && t == "text" {
				if txt, ok := item["text"].(string); ok && txt != "" {
					text = txt
					break
				}
			}
		}
	}
	if text == "" {
		return nil, exception.BuildError(
			exception.StatusRetrievalEmbeddingInputInvalid,
			exception.WithParam("error_msg", "多模态文档的文本回退字段为空"),
		)
	}

	return o.EmbedQuery(ctx, text)
}

// Dimension 返回嵌入向量维度。
// 使用 sync.Once 保证探测只执行一次，消除 TOCTOU 竞态，对齐 T-04 修复。
// 注意：此方法使用 context.Background() 创建 30s 超时，无法受调用方取消控制。
// 推荐使用 DimensionWithContext 以获得 context 控制。
func (o *OpenAIEmbedding) Dimension() int {
	o.dimOnce.Do(func() {
		if o.dimension > 0 {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		vec, err := o.EmbedQuery(ctx, "test")
		if err != nil {
			logger.Warn(logComponent).
				Str("model_provider", "openai").
				Str("model_name", o.config.ModelName).
				Err(err).
				Msg("探测嵌入向量维度失败")
			return
		}

		if !o.matryoshkaDimension {
			o.dimension = len(vec)
		}
		logger.Debug(logComponent).
			Int("dimension", len(vec)).
			Bool("matryoshka", o.matryoshkaDimension).
			Msg("探测到嵌入向量维度")
	})
	return o.dimension
}

// DimensionWithContext 返回嵌入向量维度，支持 context 取消。
// 对齐 T-04 修复：替代 Dimension() 的 context.Background() 阻塞问题。
// 当 ctx 被取消时返回 ctx.Err()。
func (o *OpenAIEmbedding) DimensionWithContext(ctx context.Context) (int, error) {
	o.dimOnce.Do(func() {
		if o.dimension > 0 {
			return
		}
		probeCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()

		vec, err := o.EmbedQuery(probeCtx, "test")
		if err != nil {
			logger.Warn(logComponent).
				Str("model_provider", "openai").
				Str("model_name", o.config.ModelName).
				Err(err).
				Msg("探测嵌入向量维度失败")
			return
		}

		if !o.matryoshkaDimension {
			o.dimension = len(vec)
		}
		logger.Debug(logComponent).
			Int("dimension", len(vec)).
			Bool("matryoshka", o.matryoshkaDimension).
			Msg("探测到嵌入向量维度")
	})
	if o.dimension == 0 {
		return 0, fmt.Errorf("探测嵌入向量维度失败")
	}
	return o.dimension, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

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

		// 构建额外参数选项，对齐 Python **kwargs 透传
		var extraOpts []option.RequestOption
		for k, v := range o.extraParams {
			extraOpts = append(extraOpts, option.WithJSONSet(k, v))
		}

		resp, err := o.client.Embeddings.New(ctx, params, extraOpts...)
		if err != nil {
			logger.Warn(logComponent).
				Str("event_type", "embedding_request_failed").
				Str("model_provider", "openai").
				Str("model_name", o.config.ModelName).
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
		if !o.matryoshkaDimension {
			if o.dimension == 0 && len(embeddings) > 0 && len(embeddings[0]) > 0 {
				o.dimension = len(embeddings[0])
				logger.Debug(logComponent).
					Int("dimension", o.dimension).
					Msg("探测到嵌入向量维度")
			}
		}

		return embeddings, nil
	})
}

// parseOpenAIResponse 解析 OpenAI SDK 响应对象。
// 支持 base64 编码的 embedding 数据，对齐 Python encoding_format=base64。
// 当 SDK 的 Embedding 字段为空时，尝试从 JSON 原始数据解码 base64 字符串。
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
			// 优先使用 SDK 已解码的 []float64
			if len(item.Embedding) > 0 {
				embeddings[item.Index] = item.Embedding
			} else {
				// SDK 未解码时（encoding_format=base64），尝试从 RawJSON 解码
				vec, err := decodeBase64FromRawJSON(item.RawJSON())
				if err != nil {
					return nil, exception.BuildError(
						exception.StatusRetrievalEmbeddingResponseInvalid,
						exception.WithParam("error_msg", fmt.Sprintf("base64 解码嵌入数据失败(index=%d): %s", item.Index, err)),
					)
				}
				embeddings[item.Index] = vec
			}
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

// decodeBase64FromRawJSON 从 JSON 原始数据中提取 base64 编码的嵌入向量并解码。
// 对齐 Python: encoding_format=base64 时 API 返回 embedding 为 base64 字符串。
func decodeBase64FromRawJSON(rawJSON string) ([]float64, error) {
	var raw struct {
		Embedding json.RawMessage `json:"embedding"`
	}
	if err := json.Unmarshal([]byte(rawJSON), &raw); err != nil {
		return nil, fmt.Errorf("解析原始 JSON 失败: %w", err)
	}
	if len(raw.Embedding) == 0 {
		return nil, fmt.Errorf("embedding 字段为空")
	}
	// 尝试解析为 base64 字符串
	var b64Str string
	if err := json.Unmarshal(raw.Embedding, &b64Str); err != nil {
		return nil, fmt.Errorf("embedding 不是 base64 字符串: %w", err)
	}
	vec, err := ParseBase64Embedding(b64Str)
	if err != nil {
		return nil, fmt.Errorf("base64 解码失败: %w", err)
	}
	return vec, nil
}
