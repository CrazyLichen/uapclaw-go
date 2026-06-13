package reranker

import (
	"context"

	"github.com/google/uuid"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 结构体 ────────────────────────────

// Document 文档数据模型，表示待重排序的文档。
//
// 对应 Python: openjiuwen/core/foundation/store/base_reranker.py (Document)
type Document struct {
	// ID 唯一标识，未设置时自动生成 UUID
	ID string `json:"id"`
	// Text 文档文本内容
	Text string `json:"text"`
	// Metadata 文档元数据
	Metadata map[string]any `json:"metadata"`
}

// RerankerConfig 重排序模型配置。
//
// 对应 Python: openjiuwen/core/foundation/store/base_reranker.py (RerankerConfig)
type RerankerConfig struct {
	// APIKey API 密钥
	APIKey string
	// APIBase API 地址（必填）
	APIBase string
	// ModelName 模型名称
	ModelName string
	// Timeout 请求超时时间（秒），默认 10
	Timeout float64
	// Temperature 生成温度，默认 0.95
	Temperature float64
	// TopP Top-P 采样参数，默认 0.1
	TopP float64
	// YesNoIDs "yes" 和 "no" 的 token ID，ChatReranker 必填
	YesNoIDs [2]int
	// ExtraBody 传递给 API 的额外参数
	ExtraBody map[string]any
}

// RerankOption 重排序可选参数。
type RerankOption struct {
	// InstructEnabled 是否启用指令模板，nil 表示使用默认行为（启用）
	InstructEnabled *bool
	// CustomInstruct 自定义指令文本，非空时使用此值替代默认指令
	CustomInstruct string
	// TopN 返回的最大文档数量，0 表示返回全部
	TopN int
	// ExtraParams 额外请求参数
	ExtraParams map[string]any
}

// BaseReranker 重排序模型抽象接口，定义文档相关性重排序操作。
//
// 所有重排序模型实现必须满足此接口。给定查询和一组文档，
// 返回文档到相关性分数的映射，分数越高表示越相关。
//
// 对应 Python: openjiuwen/core/foundation/store/base_reranker.py (Reranker)
type BaseReranker interface {
	// Rerank 对字符串文档列表进行异步重排序，返回文档到相关性分数的映射。
	Rerank(ctx context.Context, query string, docs []string, opts ...RerankOption) (map[string]float64, error)

	// RerankDocs 对 Document 列表进行异步重排序，返回文档 ID 到相关性分数的映射。
	RerankDocs(ctx context.Context, query string, docs []*Document, opts ...RerankOption) (map[string]float64, error)

	// RerankSync 对字符串文档列表进行同步重排序。
	RerankSync(ctx context.Context, query string, docs []string, opts ...RerankOption) (map[string]float64, error)

	// RerankDocsSync 对 Document 列表进行同步重排序。
	RerankDocsSync(ctx context.Context, query string, docs []*Document, opts ...RerankOption) (map[string]float64, error)
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// defaultRerankerTimeout 默认请求超时时间（秒）
	defaultRerankerTimeout float64 = 10
	// defaultTemperature 默认生成温度
	defaultTemperature float64 = 0.95
	// defaultTopP 默认 Top-P 采样参数
	defaultTopP float64 = 0.1
	// defaultInstruct 默认指令文本
	defaultInstruct = "Given a search query, retrieve relevant candidates that answer the query."
	// queryTemplate 查询模板，包含指令和查询
	queryTemplate = "<Instruct>: {instruct}\n<Query>: {query}\n"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewDocument 创建文档，自动生成 UUID 作为 ID。
func NewDocument(text string, metadata ...map[string]any) *Document {
	doc := &Document{
		ID:   uuid.New().String(),
		Text: text,
	}
	if len(metadata) > 0 {
		doc.Metadata = metadata[0]
	}
	return doc
}

// DocID 提取文档标识：如果输入是 *Document 返回其 ID，否则返回字符串本身。
// 用于 rerank 结果 map 的键。
func DocID(doc any) string {
	if d, ok := doc.(*Document); ok {
		return d.ID
	}
	if d, ok := doc.(string); ok {
		return d
	}
	return ""
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// ValidateConfig 校验 RerankerConfig 字段。
func ValidateConfig(config *RerankerConfig) error {
	if config.APIBase == "" {
		return exception.ValidateError(exception.StatusRetrievalRerankerInputInvalid,
			exception.WithParam("error_msg", "APIBase is required"),
		)
	}
	if config.Timeout < 0 {
		return exception.ValidateError(exception.StatusRetrievalRerankerInputInvalid,
			exception.WithParam("error_msg", "Timeout must be greater than 0"),
		)
	}
	return nil
}

// ResolveInstruct 解析 instruct 选项，返回最终的查询字符串。
// InstructEnabled = nil 或 &true + CustomInstruct = "" → 使用默认指令
// InstructEnabled = &true + CustomInstruct != "" → 使用自定义指令
// InstructEnabled = &false → 不使用指令
func ResolveInstruct(query string, opt *RerankOption) string {
	if opt == nil {
		return FormatQuery(query, defaultInstruct)
	}
	if opt.InstructEnabled != nil && !*opt.InstructEnabled {
		return query
	}
	instruct := defaultInstruct
	if opt.CustomInstruct != "" {
		instruct = opt.CustomInstruct
	}
	return FormatQuery(query, instruct)
}

// FormatQuery 使用模板格式化查询字符串。
func FormatQuery(query, instruct string) string {
	result := queryTemplate
	result = ReplacePlaceholder(result, "{instruct}", instruct)
	result = ReplacePlaceholder(result, "{query}", query)
	return result
}

// ReplacePlaceholder 替换字符串中的占位符。
func ReplacePlaceholder(s, old, new string) string {
	for i := 0; i < len(s)-len(old)+1; i++ {
		if s[i:i+len(old)] == old {
			return s[:i] + new + s[i+len(old):]
		}
	}
	return s
}
