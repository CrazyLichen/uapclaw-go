package reranker

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	reranker "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/reranker"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/utils"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// DashScopeReranker 阿里云 DashScope 重排序客户端。
//
// 继承 RerankerBase，使用 DashScope 专用的 /services/rerank/text-rerank/text-rerank API，
// 支持纯文本和多模态文档输入。instruct 作为 parameters.instruct 字段传递，
// 而非像 StandardReranker 那样拼入 query 模板。
//
// 对应 Python: openjiuwen/core/retrieval/reranker/dashscope_reranker.py
type DashScopeReranker struct {
	// RerankerBase 嵌入基类
	*RerankerBase
	// httpClient HTTP 客户端
	httpClient *http.Client
	// endPoint API 端点
	endPoint string
}

// ──────────────────────────── 枚举 ────────────────────────────

// DashScopeRerankerOption DashScopeReranker 可选配置。
type DashScopeRerankerOption func(*DashScopeReranker)

// ──────────────────────────── 常量 ────────────────────────────
const (
	// dashScopeEndPoint DashScope 重排序 API 端点
	dashScopeEndPoint = "/services/rerank/text-rerank/text-rerank"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// 确保编译时接口合规
var _ reranker.BaseReranker = (*DashScopeReranker)(nil)

// 抑制未使用导入警告
var _ = fmt.Sprintf

// ──────────────────────────── 导出函数 ────────────────────────────

// WithDashScopeMaxRetries 设置最大重试次数。
func WithDashScopeMaxRetries(n int) DashScopeRerankerOption {
	return func(r *DashScopeReranker) { r.maxRetries = n }
}

// WithDashScopeRetryWait 设置重试等待时间。
func WithDashScopeRetryWait(d time.Duration) DashScopeRerankerOption {
	return func(r *DashScopeReranker) { r.retryWait = d }
}

// WithDashScopeHTTPClient 设置自定义 HTTP 客户端。
func WithDashScopeHTTPClient(client *http.Client) DashScopeRerankerOption {
	return func(r *DashScopeReranker) { r.httpClient = client }
}

// WithDashScopeExtraHeaders 设置额外请求头。
func WithDashScopeExtraHeaders(headers map[string]string) DashScopeRerankerOption {
	return func(r *DashScopeReranker) {
		for k, v := range headers {
			r.headers[k] = v
		}
	}
}

// NewDashScopeReranker 创建 DashScope 重排序客户端。
//
// APIBase 尾部去除 DashScope 端点后缀（对齐 Python removesuffix）。
func NewDashScopeReranker(config reranker.RerankerConfig, opts ...DashScopeRerankerOption) (*DashScopeReranker, error) {
	if err := reranker.ValidateConfig(&config); err != nil {
		return nil, err
	}

	// 去除 APIBase 尾部斜杠和端点后缀（对齐 Python removesuffix("/") + removesuffix(self.end_point)）
	apiBase := strings.TrimSuffix(config.APIBase, "/")
	apiBase = strings.TrimSuffix(apiBase, dashScopeEndPoint)

	base := NewRerankerBase(config, defaultMaxRetries, defaultRetryWait)

	r := &DashScopeReranker{
		RerankerBase: base,
		httpClient:   &http.Client{Timeout: time.Duration(config.Timeout) * time.Second},
		endPoint:     dashScopeEndPoint,
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
func (r *DashScopeReranker) Rerank(ctx context.Context, query string, docs []string, opts ...reranker.RerankOption) (map[string]float64, error) {
	docsAny := make([]any, len(docs))
	for i, d := range docs {
		docsAny[i] = d
	}
	return r.doRerank(ctx, query, docsAny, resolveOption(opts...))
}

// RerankDocs 对 Document 列表进行异步重排序，返回文档 ID 到相关性分数的映射。
func (r *DashScopeReranker) RerankDocs(ctx context.Context, query string, docs []*reranker.Document, opts ...reranker.RerankOption) (map[string]float64, error) {
	docsAny := make([]any, len(docs))
	for i, d := range docs {
		docsAny[i] = d
	}
	return r.doRerank(ctx, query, docsAny, resolveOption(opts...))
}

// RerankSync 对字符串文档列表进行同步重排序。
func (r *DashScopeReranker) RerankSync(ctx context.Context, query string, docs []string, opts ...reranker.RerankOption) (map[string]float64, error) {
	docsAny := make([]any, len(docs))
	for i, d := range docs {
		docsAny[i] = d
	}
	return r.doRerankSync(ctx, query, docsAny, resolveOption(opts...))
}

// RerankDocsSync 对 Document 列表进行同步重排序。
func (r *DashScopeReranker) RerankDocsSync(ctx context.Context, query string, docs []*reranker.Document, opts ...reranker.RerankOption) (map[string]float64, error) {
	docsAny := make([]any, len(docs))
	for i, d := range docs {
		docsAny[i] = d
	}
	return r.doRerankSync(ctx, query, docsAny, resolveOption(opts...))
}

// RerankMultimodal 对多模态文档列表进行异步重排序。
func (r *DashScopeReranker) RerankMultimodal(ctx context.Context, query string, docs []*common.MultimodalDocument, opts ...reranker.RerankOption) (map[string]float64, error) {
	docsAny := make([]any, len(docs))
	for i, d := range docs {
		docsAny[i] = d
	}
	return r.doRerank(ctx, query, docsAny, resolveOption(opts...))
}

// RerankMultimodalSync 对多模态文档列表进行同步重排序。
func (r *DashScopeReranker) RerankMultimodalSync(ctx context.Context, query string, docs []*common.MultimodalDocument, opts ...reranker.RerankOption) (map[string]float64, error) {
	docsAny := make([]any, len(docs))
	for i, d := range docs {
		docsAny[i] = d
	}
	return r.doRerankSync(ctx, query, docsAny, resolveOption(opts...))
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// doRerank 执行异步重排序。
func (r *DashScopeReranker) doRerank(ctx context.Context, query string, docs []any, opt *reranker.RerankOption) (map[string]float64, error) {
	headers, params, docIDs, err := r.assembleParams(query, docs, opt)
	if err != nil {
		return nil, err
	}

	cfg := utils.RetryConfig{
		MaxRetries: r.maxRetries,
		RetryWait:  r.retryWait,
		Task:       utils.TaskReranker,
	}

	result, err := utils.RequestWithRetry(ctx, r.httpClient, r.config.APIBase+r.endPoint, params, headers, cfg)
	if err != nil {
		logger.Error(logComponent).
			Str("event_type", "llm_call_error").
			Str("method", "DashScopeRerank").
			Str("model_provider", r.config.ModelName).
			Err(err).
			Msg("DashScopeReranker 请求失败")
		return nil, err
	}

	return r.parseResponse(result, docIDs), nil
}

// doRerankSync 执行同步重排序。
func (r *DashScopeReranker) doRerankSync(ctx context.Context, query string, docs []any, opt *reranker.RerankOption) (map[string]float64, error) {
	headers, params, docIDs, err := r.assembleParams(query, docs, opt)
	if err != nil {
		return nil, err
	}

	cfg := utils.RetryConfig{
		MaxRetries: r.maxRetries,
		RetryWait:  r.retryWait,
		Task:       utils.TaskReranker,
	}

	result, err := utils.RequestWithRetrySync(ctx, r.httpClient, r.config.APIBase+r.endPoint, params, headers, cfg)
	if err != nil {
		logger.Error(logComponent).
			Str("event_type", "llm_call_error").
			Str("method", "DashScopeRerankSync").
			Str("model_provider", r.config.ModelName).
			Err(err).
			Msg("DashScopeReranker 同步请求失败")
		return nil, err
	}

	return r.parseResponse(result, docIDs), nil
}

// assembleParams 组装请求参数，将文档和查询合并为 DashScope 格式的请求参数。
// 覆盖基类方法，增加多模态文档支持和类型校验。
// 对齐 Python: DashscopeReranker._assemble_params
func (r *DashScopeReranker) assembleParams(query string, docs []any, opt *reranker.RerankOption) (map[string]string, map[string]any, []string, error) {
	docIDs := make([]string, len(docs))
	texts := make([]string, 0, len(docs))
	multimodalInputs := make([]map[string]any, 0, len(docs))
	hasMultimodal := false

	// 处理多模态查询
	var resolvedQuery any = query
	if opt != nil && opt.MultimodalQuery != nil {
		if md, ok := opt.MultimodalQuery.(*common.MultimodalDocument); ok {
			dsInput, dsErr := md.DashscopeInput()
			if dsErr != nil {
				return nil, nil, nil, dsErr
			}
			resolvedQuery = dsInput
		}
	}

	for i, doc := range docs {
		switch d := doc.(type) {
		case string:
			docIDs[i] = d
			texts = append(texts, d)
		case *reranker.Document:
			docIDs[i] = d.ID
			texts = append(texts, d.Text)
		case *common.MultimodalDocument:
			// 对齐 G-25 修复：AddField(ModalityText,...) 不再更新 d.Text，
			// 需要从 Content() 中提取文本作为 docID
			docID := ""
			for _, item := range d.Content() {
				if t, ok := item["type"].(string); ok && t == "text" {
					if txt, ok := item["text"].(string); ok && txt != "" {
						docID = txt
						break
					}
				}
			}
			if docID == "" {
				docID = fmt.Sprintf("multimodal_%d", i)
			}
			docIDs[i] = docID
			dsInput, dsErr := d.DashscopeInput()
			if dsErr != nil {
				return nil, nil, nil, dsErr
			}
			multimodalInputs = append(multimodalInputs, dsInput)
			hasMultimodal = true
		default:
			return nil, nil, nil, exception.ValidateError(
				exception.StatusRetrievalRerankerInputInvalid,
				exception.WithParam("error_msg", "input to reranker must be list[str | Document | MultimodalDocument]"),
			)
		}
	}

	topN := r.resolveTopN(opt, len(docs))
	headers := r.requestHeaders()

	// 构建 documents 字段
	var documents any
	if hasMultimodal {
		// 多模态模式：纯文本包装为 {"text": d}，多模态使用 DashscopeInput 结果
		docList := make([]map[string]any, 0, len(docs))
		multimodalIdx := 0
		for _, doc := range docs {
			switch d := doc.(type) {
			case string:
				docList = append(docList, map[string]any{"text": d})
			case *reranker.Document:
				docList = append(docList, map[string]any{"text": d.Text})
			case *common.MultimodalDocument:
				docList = append(docList, multimodalInputs[multimodalIdx])
				multimodalIdx++
			}
		}
		documents = docList
	} else {
		documents = texts
	}

	params := r.requestParams(resolvedQuery, documents, topN, opt)

	return headers, params, docIDs, nil
}

// requestParams 构造 DashScope 专用请求参数。
// 覆盖基类方法，使用 DashScope 的 {model, input, parameters} 格式。
// query 参数支持 string 和 map[string]any（多模态查询），对齐 Python。
// 对齐 Python: DashscopeReranker._request_params
func (r *DashScopeReranker) requestParams(query any, documents any, topN int, opt *reranker.RerankOption) map[string]any {
	parameters := map[string]any{
		"return_documents": false,
		"top_n":            topN,
	}

	// instruct 处理：仅当 CustomInstruct 非空时设置 parameters.instruct
	if opt != nil && opt.CustomInstruct != "" {
		parameters["instruct"] = opt.CustomInstruct
	}

	params := map[string]any{
		"model": r.config.ModelName,
		"input": map[string]any{
			"query":     query,
			"documents": documents,
		},
		"parameters": parameters,
	}

	// 合并 ExtraBody 到 parameters 内（对齐 Python kwargs 合并到 parameters）
	for k, v := range r.config.ExtraBody {
		parameters[k] = v
	}

	// 合并 ExtraParams 到 parameters 内
	if opt != nil && opt.ExtraParams != nil {
		for k, v := range opt.ExtraParams {
			parameters[k] = v
		}
	}

	return params
}
