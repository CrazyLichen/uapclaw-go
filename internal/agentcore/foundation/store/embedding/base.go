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
	EmbedQuery(ctx context.Context, text string) ([]float64, error)

	// EmbedDocuments 将多条文档文本批量转换为向量。
	EmbedDocuments(ctx context.Context, texts []string, opts ...EmbedOption) ([][]float64, error)

	// Dimension 返回嵌入向量的维度。
	Dimension() int
}

// EmbedOption 批量嵌入的可选参数。
//
// 对齐 Python: embed_documents(batch_size=, callback_cls=)
type EmbedOption struct {
	// BatchSize 批大小，0 表示使用默认值
	BatchSize int
	// Callback 进度回调，nil 表示不回调
	Callback Callback
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────
