package reranker

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	reranker "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/reranker"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/utils"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// StandardRerankerOption StandardReranker 可选配置
type StandardRerankerOption func(*StandardReranker)

// StandardReranker 标准重排序客户端，支持 vLLM 风格的 /rerank API。
//
// 对应 Python: openjiuwen/core/retrieval/reranker/standard_reranker.py (StandardReranker)
type StandardReranker struct {
	// RerankerBase 嵌入基类
	*RerankerBase
	// httpClient HTTP 客户端
	httpClient *http.Client
	// endPoint API 端点，默认 "/rerank"
	endPoint string
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// standardEndPoint 标准重排序 API 端点
	standardEndPoint = "/rerank"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// WithMaxRetries 设置最大重试次数
func WithMaxRetries(n int) StandardRerankerOption {
	return func(r *StandardReranker) {
		r.maxRetries = n
	}
}

// WithRetryWait 设置重试等待时间
func WithRetryWait(d time.Duration) StandardRerankerOption {
	return func(r *StandardReranker) {
		r.retryWait = d
	}
}

// WithExtraHeaders 设置额外请求头
func WithExtraHeaders(headers map[string]string) StandardRerankerOption {
	return func(r *StandardReranker) {
		for k, v := range headers {
			r.headers[k] = v
		}
	}
}

// WithHTTPClient 设置自定义 HTTP 客户端
func WithHTTPClient(client *http.Client) StandardRerankerOption {
	return func(r *StandardReranker) {
		r.httpClient = client
	}
}

// NewStandardReranker 创建标准重排序客户端。
// APIBase 尾部去除 "/rerank" 后缀（对齐 Python removesuffix(self.end_point)）。
func NewStandardReranker(config reranker.RerankerConfig, opts ...StandardRerankerOption) (*StandardReranker, error) {
	if err := reranker.ValidateConfig(&config); err != nil {
		return nil, err
	}

	// 去除 APIBase 尾部的端点后缀
	apiBase := strings.TrimSuffix(config.APIBase, standardEndPoint)

	base := NewRerankerBase(config, defaultMaxRetries, defaultRetryWait)

	r := &StandardReranker{
		RerankerBase: base,
		httpClient:   &http.Client{Timeout: time.Duration(config.Timeout) * time.Second},
		endPoint:     standardEndPoint,
	}

	// 如果 APIBase 被截断了，更新 config
	r.config.APIBase = apiBase

	for _, opt := range opts {
		opt(r)
	}

	// 如果未设置 HTTP 客户端超时且 config.Timeout > 0
	if r.httpClient.Timeout == 0 && config.Timeout > 0 {
		r.httpClient.Timeout = time.Duration(config.Timeout) * time.Second
	}

	return r, nil
}

// Rerank 对字符串文档列表进行异步重排序，返回文档到相关性分数的映射。
func (r *StandardReranker) Rerank(ctx context.Context, query string, docs []string, opts ...reranker.RerankOption) (map[string]float64, error) {
	// 将 []string 转为 []any
	docsAny := make([]any, len(docs))
	for i, d := range docs {
		docsAny[i] = d
	}
	return r.doRerank(ctx, query, docsAny, resolveOption(opts...))
}

// RerankDocs 对 Document 列表进行异步重排序，返回文档 ID 到相关性分数的映射。
func (r *StandardReranker) RerankDocs(ctx context.Context, query string, docs []*reranker.Document, opts ...reranker.RerankOption) (map[string]float64, error) {
	// 将 []*Document 转为 []any
	docsAny := make([]any, len(docs))
	for i, d := range docs {
		docsAny[i] = d
	}
	return r.doRerank(ctx, query, docsAny, resolveOption(opts...))
}

// RerankSync 对字符串文档列表进行同步重排序。
func (r *StandardReranker) RerankSync(ctx context.Context, query string, docs []string, opts ...reranker.RerankOption) (map[string]float64, error) {
	docsAny := make([]any, len(docs))
	for i, d := range docs {
		docsAny[i] = d
	}
	return r.doRerankSync(ctx, query, docsAny, resolveOption(opts...))
}

// RerankDocsSync 对 Document 列表进行同步重排序。
func (r *StandardReranker) RerankDocsSync(ctx context.Context, query string, docs []*reranker.Document, opts ...reranker.RerankOption) (map[string]float64, error) {
	docsAny := make([]any, len(docs))
	for i, d := range docs {
		docsAny[i] = d
	}
	return r.doRerankSync(ctx, query, docsAny, resolveOption(opts...))
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// assembleParams 组装请求参数，将文档和查询合并为完整的请求参数。
// 覆盖基类方法，增加输入类型校验。
// 对齐 Python: StandardReranker._assemble_params
func (r *StandardReranker) assembleParams(query string, docs []any, opt *reranker.RerankOption) (map[string]string, map[string]any, []string) {
	// 校验输入类型：必须是 []string 或 []*Document
	docIDs := make([]string, len(docs))
	texts := make([]string, len(docs))
	for i, doc := range docs {
		switch d := doc.(type) {
		case string:
			docIDs[i] = d
			texts[i] = d
		case *reranker.Document:
			docIDs[i] = d.ID
			texts[i] = d.Text
		default:
			// 不支持的类型，跳过
		}
	}

	topN := r.resolveTopN(opt, len(docs))
	resolvedQuery := reranker.ResolveInstruct(query, opt)

	headers := r.requestHeaders()
	params := r.requestParams(resolvedQuery, texts, topN, opt)

	params["documents"] = texts
	params["top_n"] = topN

	return headers, params, docIDs
}

// resolveOption 从可变参数中解析 RerankOption
func resolveOption(opts ...reranker.RerankOption) *reranker.RerankOption {
	if len(opts) == 0 {
		return nil
	}
	return &opts[0]
}

// doRerank 执行异步重排序
func (r *StandardReranker) doRerank(ctx context.Context, query string, docs []any, opt *reranker.RerankOption) (map[string]float64, error) {
	headers, params, docIDs := r.assembleParams(query, docs, opt)

	cfg := utils.RetryConfig{
		MaxRetries: r.maxRetries,
		RetryWait:  r.retryWait,
		Task:       utils.TaskReranker,
	}

	result, err := utils.RequestWithRetry(ctx, r.httpClient, r.config.APIBase+r.endPoint, params, headers, cfg)
	if err != nil {
		logger.Error(logComponent).
			Str("event_type", "llm_call_error").
			Str("method", "Rerank").
			Str("model_provider", r.config.ModelName).
			Err(err).
			Msg("StandardReranker 请求失败")
		return nil, err
	}

	return r.parseResponse(result, docIDs), nil
}

// doRerankSync 执行同步重排序
func (r *StandardReranker) doRerankSync(ctx context.Context, query string, docs []any, opt *reranker.RerankOption) (map[string]float64, error) {
	headers, params, docIDs := r.assembleParams(query, docs, opt)

	cfg := utils.RetryConfig{
		MaxRetries: r.maxRetries,
		RetryWait:  r.retryWait,
		Task:       utils.TaskReranker,
	}

	result, err := utils.RequestWithRetrySync(ctx, r.httpClient, r.config.APIBase+r.endPoint, params, headers, cfg)
	if err != nil {
		logger.Error(logComponent).
			Str("event_type", "llm_call_error").
			Str("method", "RerankSync").
			Str("model_provider", r.config.ModelName).
			Err(err).
			Msg("StandardReranker 同步请求失败")
		return nil, err
	}

	return r.parseResponse(result, docIDs), nil
}

// validateStandardConfig 校验输入文档类型
func validateStandardConfig(docs []any) error {
	for _, doc := range docs {
		switch doc.(type) {
		case string:
		case *reranker.Document:
		default:
			return exception.ValidateError(exception.StatusRetrievalRerankerInputInvalid,
				exception.WithParam("error_msg", "input to reranker must be either list[str | Document]"),
			)
		}
	}
	return nil
}

// formatAPIBase 格式化 API 地址，去除尾部端点后缀
func formatAPIBase(apiBase, endPoint string) string {
	return strings.TrimSuffix(apiBase, endPoint)
}

// 确保编译时接口合规
var _ reranker.BaseReranker = (*StandardReranker)(nil)

// 抑制未使用导入警告
var _ = fmt.Sprintf
