package embedding

import "context"

// ──────────────────────────── 结构体 ────────────────────────────

// Callback 嵌入进度回调接口。
//
// 对齐 Python: BaseCallback
type Callback interface {
	// OnBatchComplete 一批嵌入完成时回调。
	OnBatchComplete(startIdx, endIdx int, batch []string)
}

// BaseEmbedding 向量嵌入模型的抽象接口。
//
// 提供文本到向量的转换能力，供记忆索引等组件进行语义搜索。
// 具体实现见 internal/agentcore/retrieval/embedding/ 包。
//
// 对应 Python: openjiuwen/core/foundation/store/base_embedding.py (Embedding)
type BaseEmbedding interface {
	// EmbedQuery 将单条查询文本转换为向量。
	// 对齐 Python: embed_query(self, text, **kwargs)，opts 透传额外参数。
	EmbedQuery(ctx context.Context, text string, opts ...EmbedOption) ([]float64, error)

	// EmbedDocuments 将多条文档文本批量转换为向量。
	EmbedDocuments(ctx context.Context, texts []string, opts ...EmbedOption) ([][]float64, error)

	// Dimension 返回嵌入向量的维度。
	Dimension() int

	// DimensionWithContext 返回嵌入向量的维度，支持 context 取消。
	// 对齐 T-04 修复：Dimension() 使用 context.Background() 可能意外阻塞，
	// 此方法允许调用方传入 context 控制超时和取消。
	// 默认实现调用 Dimension()，需要 context 控制的实现应覆盖此方法。
	DimensionWithContext(ctx context.Context) (int, error)
}

// EmbedOptions 批量嵌入的内部选项结构。
type EmbedOptions struct {
	// BatchSize 批大小，0 表示使用默认值
	BatchSize int
	// Callback 进度回调，nil 表示不回调
	Callback Callback
}

// ──────────────────────────── 枚举 ────────────────────────────

// EmbedOption 函数选项模式，用于配置 EmbedOptions。
//
// 对齐 Python: embed_documents(batch_size=, callback_cls=)
type EmbedOption func(*EmbedOptions)

// ──────────────────────────── 导出函数 ────────────────────────────

// WithBatchSize 设置批大小的 EmbedOption。
func WithBatchSize(n int) EmbedOption {
	return func(o *EmbedOptions) {
		o.BatchSize = n
	}
}

// WithCallback 设置进度回调的 EmbedOption。
func WithCallback(cb Callback) EmbedOption {
	return func(o *EmbedOptions) {
		o.Callback = cb
	}
}

// NewEmbedOptions 从可变参数构建 EmbedOptions。
func NewEmbedOptions(opts ...EmbedOption) EmbedOptions {
	o := EmbedOptions{}
	for _, opt := range opts {
		opt(&o)
	}
	return o
}
