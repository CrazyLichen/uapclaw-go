package embedding

import "context"

// ──────────────────────────── 结构体 ────────────────────────────

// BaseEmbedding 向量嵌入模型的抽象接口。
//
// 提供文本到向量的转换能力，供记忆索引等组件进行语义搜索。
// ⤵️ 预留：4.19-4.22 补充具体实现（OpenAI/DashScope/VLLM/API）
//
// 对应 Python: openjiuwen/core/foundation/store/base_embedding.py (Embedding)
type BaseEmbedding interface {
	// EmbedQuery 将单条查询文本转换为向量。
	EmbedQuery(ctx context.Context, text string) ([]float64, error)

	// EmbedDocuments 将多条文档文本批量转换为向量。
	EmbedDocuments(ctx context.Context, texts []string) ([][]float64, error)

	// Dimension 返回嵌入向量的维度。
	Dimension() int
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────
