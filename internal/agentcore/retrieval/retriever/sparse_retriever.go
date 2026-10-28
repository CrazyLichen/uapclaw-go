package retriever

import (
	"context"
	"fmt"

	"golang.org/x/sync/errgroup"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/vector_store"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SparseRetriever 稀疏检索器实现（BM25）。
//
// Python: openjiuwen/core/retrieval/retriever/sparse_retriever.py
type SparseRetriever struct {
	// vectorStore 底层向量存储（需支持稀疏搜索）
	vectorStore vector_store.VectorStore
	// indexType 索引类型（"" = Python None；上层注入，13.23 回填）。
	// 无 embedModel 字段——Python SparseRetriever 无 embed_model 属性，
	// hasattr False 同构（EmbedModelProvider 断言失败）
	indexType string
}

// 编译期断言：SparseRetriever 满足 Retriever 接口。
var _ Retriever = (*SparseRetriever)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSparseRetriever 创建稀疏检索器。
func NewSparseRetriever(vectorStore vector_store.VectorStore) *SparseRetriever {
	return &SparseRetriever{
		vectorStore: vectorStore,
	}
}

// Retrieve 稀疏检索（BM25）。
//
// 对齐 Python SparseRetriever.retrieve：仅 mode 校验——
// 无 threshold 校验（收而不用，静默忽略，spec §8-5）、
// 无 embedModel 校验（BM25 不需要嵌入）。
// 归一化 chunk_id 取 result.ID（与 Vector 的 metadata 来源不同，
// Python 三作者不一致，字面复刻 spec 决策 7）。
func (r *SparseRetriever) Retrieve(ctx context.Context, query string, topK int, _ *float64, mode RetrievalMode, _ ...RetrieveOption) ([]common.RetrievalResult, error) {
	// mode 零值回落子类默认（spec §8-1：Python 签名默认 "sparse"）
	if mode == "" {
		mode = RetrievalModeSparse
	}
	if mode != RetrievalModeSparse {
		return nil, exception.BuildError(
			exception.StatusRetrievalRetrieverModeNotSupport,
			exception.WithParam("error_msg", fmt.Sprintf("SparseRetriever only supports 'sparse' mode, got %s", mode)),
		)
	}

	searchResults, err := r.vectorStore.SparseSearch(ctx, query, topK, nil)
	if err != nil {
		return nil, err
	}

	retrievalResults := make([]common.RetrievalResult, 0, len(searchResults))
	for _, result := range searchResults {
		retrievalResults = append(retrievalResults, common.RetrievalResult{
			Text:     result.Text,
			Score:    result.Score,
			Metadata: result.Metadata,
			DocID:    metadataString(result.Metadata, "doc_id"),
			ChunkID:  result.ID, // Sparse 从主键取（spec 决策 7）
		})
	}
	return retrievalResults, nil
}

// BatchRetrieve 批量并发检索（裸 errgroup，同 VectorRetriever 语义）。
func (r *SparseRetriever) BatchRetrieve(ctx context.Context, queries []string, topK int, opts ...RetrieveOption) ([][]common.RetrievalResult, error) {
	results := make([][]common.RetrievalResult, len(queries))
	var g errgroup.Group
	for i, q := range queries {
		g.Go(func() error {
			res, err := r.Retrieve(ctx, q, topK, nil, "", opts...)
			if err != nil {
				return err
			}
			results[i] = res
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	return results, nil
}

// RetrieveSearchResults 稀疏检索（原始 SearchResult）。
//
// 对齐 Python SparseRetriever.retrieve_search_results：
// 有 mode 校验（与 Vector 的无校验不对称，Python 两作者差异，字面复刻 spec §8-8）。
func (r *SparseRetriever) RetrieveSearchResults(ctx context.Context, query string, topK int, mode RetrievalMode, _ ...RetrieveOption) ([]common.SearchResult, error) {
	if mode == "" {
		mode = RetrievalModeSparse
	}
	if mode != RetrievalModeSparse {
		return nil, exception.BuildError(
			exception.StatusRetrievalRetrieverModeNotSupport,
			exception.WithParam("error_msg", fmt.Sprintf("SparseRetriever only supports 'sparse' mode, got %s", mode)),
		)
	}
	return r.vectorStore.SparseSearch(ctx, query, topK, nil)
}

// Close 关闭检索器（同 VectorRetriever 语义，鸭子探测消解 spec §8-7）。
func (r *SparseRetriever) Close() {
	if r.vectorStore != nil {
		r.vectorStore.Close()
	}
}

// VectorStore 返回底层向量存储（能力接口回填，13.23）。
func (r *SparseRetriever) VectorStore() vector_store.VectorStore { return r.vectorStore }

// IndexType 返回索引类型（"" = Python None，能力接口回填 13.23）。
func (r *SparseRetriever) IndexType() string { return r.indexType }

// SetIndexType 注入索引类型（上层注入通道，13.23）。
func (r *SparseRetriever) SetIndexType(indexType string) { r.indexType = indexType }
