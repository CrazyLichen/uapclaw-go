package reranker

import (
	"fmt"
	"time"

	reranker "github.com/uapclaw/uapclaw-go/internal/agentcore/store/reranker"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// RerankerBase 重排序模型的默认实现基类。
//
// 嵌入此结构体后，实现类只需实现核心的 Rerank/RerankDocs 等方法即可满足 BaseReranker 接口。
// 默认提供 requestHeaders / requestParams / parseResponse / assembleParams 等通用方法，
// 子类可按需覆盖。
//
// 对应 Python: Reranker ABC 中的 _request_headers / _request_params / _parse_response
type RerankerBase struct {
	// config 重排序模型配置
	config reranker.RerankerConfig
	// headers 默认请求头
	headers map[string]string
	// maxRetries 最大重试次数
	maxRetries int
	// retryWait 重试等待时间
	retryWait time.Duration
}

// ──────────────────────────── 枚 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// defaultMaxRetries 默认最大重试次数
	defaultMaxRetries = 3
	// defaultRetryWait 默认重试等待时间
	defaultRetryWait = 100 * time.Millisecond
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// logComponent 日志组件常量
	logComponent = logger.ComponentAgentCore
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewRerankerBase 创建重排序基类实例。
func NewRerankerBase(config reranker.RerankerConfig, maxRetries int, retryWait time.Duration) *RerankerBase {
	return &RerankerBase{
		config:     config,
		headers:    buildDefaultHeaders(config.APIKey),
		maxRetries: maxRetries,
		retryWait:  retryWait,
	}
}

// NewRerankerBaseWithDefaults 使用默认值创建重排序基类实例。
func NewRerankerBaseWithDefaults(config reranker.RerankerConfig) *RerankerBase {
	return NewRerankerBase(config, defaultMaxRetries, defaultRetryWait)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// buildDefaultHeaders 构建默认请求头。
func buildDefaultHeaders(apiKey string) map[string]string {
	headers := map[string]string{
		"Content-Type": "application/json",
	}
	if apiKey != "" {
		headers["Authorization"] = fmt.Sprintf("Bearer %s", apiKey)
	}
	return headers
}

// Config 返回重排序模型配置。
func (b *RerankerBase) Config() reranker.RerankerConfig {
	return b.config
}

// MaxRetries 返回最大重试次数。
func (b *RerankerBase) MaxRetries() int {
	return b.maxRetries
}

// RetryWait 返回重试等待时间。
func (b *RerankerBase) RetryWait() time.Duration {
	return b.retryWait
}

// requestHeaders 返回默认请求头，子类可覆盖。
func (b *RerankerBase) requestHeaders() map[string]string {
	return b.headers
}

// requestParams 构建请求参数（StandardReranker 风格）。
// 子类应覆盖此方法以适配不同 API 格式（如 DashScope、ChatReranker）。
func (b *RerankerBase) requestParams(query string, documents []string, topN int, opt *reranker.RerankOption) map[string]any {
	params := map[string]any{
		"model":            b.config.ModelName,
		"return_documents": false,
		"query":            query,
		"documents":        documents,
		"top_n":            topN,
	}
	// 合并 ExtraBody
	for k, v := range b.config.ExtraBody {
		params[k] = v
	}
	// 合并 ExtraParams
	if opt != nil && opt.ExtraParams != nil {
		for k, v := range opt.ExtraParams {
			params[k] = v
		}
	}
	return params
}

// parseResponse 解析 API 响应为文档-分数映射。
// 默认实现 StandardReranker 风格：从 results[index].relevance_score 提取分数。
// 子类可覆盖以适配不同响应格式。
func (b *RerankerBase) parseResponse(responseData map[string]any, docIDs []string) map[string]float64 {
	result := make(map[string]float64, len(docIDs))
	// 初始化所有文档分数为 0
	for _, id := range docIDs {
		result[id] = 0.0
	}

	// 尝试从 "output" 或根级别获取 "results"
	var results []any
	if output, ok := responseData["output"]; ok {
		if outputMap, ok := output.(map[string]any); ok {
			results, _ = outputMap["results"].([]any)
		}
	}
	if results == nil {
		results, _ = responseData["results"].([]any)
	}

	for _, item := range results {
		rankResult, ok := item.(map[string]any)
		if !ok {
			continue
		}
		index, _ := rankResult["index"].(float64)
		score, _ := rankResult["relevance_score"].(float64)
		idx := int(index)
		if idx >= 0 && idx < len(docIDs) {
			result[docIDs[idx]] = score
		}
	}

	return result
}

// extractDocIDs 从文档列表提取 ID 列表。
// 字符串直接作为 ID，*Document 使用其 ID 字段。
func (b *RerankerBase) extractDocIDs(docs []any) []string {
	ids := make([]string, len(docs))
	for i, doc := range docs {
		ids[i] = reranker.DocID(doc)
	}
	return ids
}

// extractTexts 从文档列表提取文本列表。
// 字符串直接使用，*Document 使用其 Text 字段。
func (b *RerankerBase) extractTexts(docs []any) []string {
	texts := make([]string, len(docs))
	for i, doc := range docs {
		if d, ok := doc.(*reranker.Document); ok {
			texts[i] = d.Text
		} else if s, ok := doc.(string); ok {
			texts[i] = s
		}
	}
	return texts
}

// resolveTopN 解析 TopN 选项，0 或未设置时使用文档总数。
func (b *RerankerBase) resolveTopN(opt *reranker.RerankOption, docCount int) int {
	if opt != nil && opt.TopN > 0 {
		return opt.TopN
	}
	return docCount
}

// assembleParams 组装请求参数，将文档和查询合并为完整的请求参数。
// 返回请求头、请求参数和文档 ID 列表。
func (b *RerankerBase) assembleParams(query string, docs []any, opt *reranker.RerankOption) (map[string]string, map[string]any, []string) {
	docIDs := b.extractDocIDs(docs)
	texts := b.extractTexts(docs)
	topN := b.resolveTopN(opt, len(docs))
	resolvedQuery := reranker.ResolveInstruct(query, opt)

	headers := b.requestHeaders()
	params := b.requestParams(resolvedQuery, texts, topN, opt)

	// 确保参数中有 documents 和 top_n
	params["documents"] = texts
	params["top_n"] = topN

	return headers, params, docIDs
}
