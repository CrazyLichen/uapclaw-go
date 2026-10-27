package retriever

import (
	"context"
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/embedding"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/vector_store"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
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

// Retrieve 混合检索（vector/sparse/hybrid 三模式路由 + 归一化）。
//
// 对齐 Python HybridRetriever.retrieve（L41-130）：
//   - alpha 解析：opts.Alpha 非 nil 覆写，否则回落构造字段
//   - 155201 前置校验先于 mode 分支（顺序不可调换——hybrid+threshold
//     报 155201 而非 155200）
//   - hybrid 分支 embed 可选（nil 走 store 层纯稀疏路径）
//   - vector 分支空结果 BM25 兜底（同 VectorRetriever）
//   - 阈值过滤仅 vector 模式生效
//   - 归一化 chunk_id 取 result.ID（同 Sparse，异于 Vector 的 metadata，
//     Python 三作者不一致字面复刻）
//
// 注意：Python 不向 store 传 filters（字面 None 复刻，kwargs 有也不透传）。
func (r *HybridRetriever) Retrieve(ctx context.Context, query string, topK int, scoreThreshold *float64, mode RetrievalMode, opts ...RetrieveOption) ([]common.RetrievalResult, error) {
	options := NewRetrieveOptions(opts...)
	alpha := r.alpha
	if options.Alpha != nil {
		alpha = *options.Alpha
	}

	// 155201 前置校验（Python L64-68，先于 mode 分支）
	if scoreThreshold != nil && mode != RetrievalModeVector {
		return nil, exception.BuildError(
			exception.StatusRetrievalRetrieverScoreThresholdInvalid,
			exception.WithParam("error_msg", "score_threshold is only supported when mode='vector'"),
		)
	}

	// mode 零值回落子类默认（spec §8-1：Python 签名默认 "hybrid"）
	if mode == "" {
		mode = RetrievalModeHybrid
	}

	var searchResults []common.SearchResult
	var err error
	switch mode {
	case RetrievalModeHybrid:
		// 混合检索：embed 可选，nil 向量走 store 层纯稀疏路径
		var queryVector []float64
		if r.embedModel != nil {
			queryVector, err = r.embedModel.EmbedQuery(ctx, query)
			if err != nil {
				return nil, err // Python await 直接冒泡，不包装
			}
		}
		searchResults, err = r.vectorStore.HybridSearch(ctx, query, queryVector, topK, alpha, nil)
		if err != nil {
			return nil, err
		}
	case RetrievalModeVector:
		// 纯向量检索：embed 必需
		if r.embedModel == nil {
			return nil, exception.BuildError(
				exception.StatusRetrievalRetrieverEmbedModelNotFound,
				exception.WithParam("error_msg", "embed_model is required for vector search"),
			)
		}
		queryVector, embedErr := r.embedModel.EmbedQuery(ctx, query)
		if embedErr != nil {
			return nil, embedErr
		}
		searchResults, err = r.vectorStore.Search(ctx, queryVector, topK, nil)
		if err != nil {
			return nil, err
		}
		// 向量检索空结果时兜底 BM25（对齐 Python fallback to sparse）
		if len(searchResults) == 0 {
			searchResults, err = r.vectorStore.SparseSearch(ctx, query, topK, nil)
			if err != nil {
				return nil, err
			}
		}
	case RetrievalModeSparse:
		// 纯稀疏检索（BM25）
		searchResults, err = r.vectorStore.SparseSearch(ctx, query, topK, nil)
		if err != nil {
			return nil, err
		}
	default:
		return nil, exception.BuildError(
			exception.StatusRetrievalRetrieverModeNotSupport,
			exception.WithParam("error_msg", fmt.Sprintf("Unsupported mode: %s", mode)),
		)
	}

	// 归一化循环（阈值过滤仅 vector 模式；Python result.score is not None
	// 的 nil 检查在 Go 无对应物，spec §8-4 延续）
	retrievalResults := make([]common.RetrievalResult, 0, len(searchResults))
	for _, result := range searchResults {
		if mode == RetrievalModeVector && scoreThreshold != nil && result.Score < *scoreThreshold {
			continue
		}
		retrievalResults = append(retrievalResults, common.RetrievalResult{
			Text:     result.Text,
			Score:    result.Score,
			Metadata: result.Metadata,
			DocID:    metadataString(result.Metadata, "doc_id"),
			ChunkID:  result.ID, // Hybrid 从主键取（Python 三作者不一致，spec 决策 7 延续）
		})
	}
	return retrievalResults, nil
}
