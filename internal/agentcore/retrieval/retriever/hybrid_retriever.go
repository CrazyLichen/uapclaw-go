package retriever

import (
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/embedding"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/vector_store"
)

// ──────────────────────────── 结构体 ────────────────────────────

// HybridRetriever 混合检索器实现（向量+稀疏，RRF 融合在 store 层完成）。
//
// KB 编排层的兜底检索器：SimpleKnowledgeBase 按 index_type 路由的 else
// 分支（含 "hybrid"）、GraphRetriever 动态创建的 else 分支。
// Python: openjiuwen/core/retrieval/retriever/hybrid_retriever.py
type HybridRetriever struct {
	// vectorStore 底层向量存储
	vectorStore vector_store.VectorStore
	// embedModel 嵌入模型（nil = Python None；hybrid 模式可选走纯稀疏，
	// vector 模式缺失时报 155202）
	embedModel embedding.BaseEmbedding
	// alpha hybrid 权重（0=纯稀疏，1=纯向量，0.5=平衡），构造默认 0.5
	alpha float64
}

// HybridRetrieverOption HybridRetriever 的构造选项。
type HybridRetrieverOption func(*HybridRetriever)

// ──────────────────────────── 导出函数 ────────────────────────────

// WithHybridAlpha 设置 hybrid 权重（0=纯稀疏，1=纯向量）。
func WithHybridAlpha(v float64) HybridRetrieverOption {
	return func(r *HybridRetriever) { r.alpha = v }
}

// NewHybridRetriever 创建混合检索器。
//
// 对齐 Python HybridRetriever.__init__(vector_store, embed_model=None,
// alpha=0.5, **kwargs)：默认 alpha=0.5；kwargs 构造死参数不引入
// （13.19 决策 5 先例，spec §8-2）。构造零校验（embedModel nil 合法）。
func NewHybridRetriever(vectorStore vector_store.VectorStore, embedModel embedding.BaseEmbedding, opts ...HybridRetrieverOption) *HybridRetriever {
	r := &HybridRetriever{
		vectorStore: vectorStore,
		embedModel:  embedModel,
		alpha:       0.5,
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}
