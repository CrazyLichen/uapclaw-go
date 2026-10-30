package retriever

import (
	"context"
	"fmt"

	"golang.org/x/sync/errgroup"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/embedding"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/vector_store"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 结构体 ────────────────────────────

// VectorRetriever 向量检索器实现。
//
// Python: openjiuwen/core/retrieval/retriever/vector_retriever.py
type VectorRetriever struct {
	// vectorStore 底层向量存储
	vectorStore vector_store.VectorStore
	// embedModel 嵌入模型（nil = Python None；向量检索必需，
	// 缺失时 retrieve 报 155202）
	embedModel embedding.BaseEmbedding
	// indexType 索引类型（"" = Python None；上层 GraphRetriever/GraphKB
	// 注入，13.23 回填）
	indexType string
}

// 编译期断言：VectorRetriever 满足 Retriever 接口。
var _ Retriever = (*VectorRetriever)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewVectorRetriever 创建向量检索器。
//
// 对齐 Python VectorRetriever.__init__：构造零校验，
// embedModel 为 nil 的 155202 在 Retrieve 时触发。
func NewVectorRetriever(vectorStore vector_store.VectorStore, embedModel embedding.BaseEmbedding) *VectorRetriever {
	return &VectorRetriever{
		vectorStore: vectorStore,
		embedModel:  embedModel,
	}
}

// Retrieve 向量检索。
//
// 对齐 Python VectorRetriever.retrieve：mode 校验 → embed → search →
// 空结果 BM25 兜底 → 归一化（chunk_id 取 metadata["chunk_id"]）。
// 注意：Python 此处不向 store.search 传 filters（kwargs 有 filters 也不透传，字面复刻）。
func (r *VectorRetriever) Retrieve(ctx context.Context, query string, topK int, scoreThreshold *float64, mode RetrievalMode, _ ...RetrieveOption) ([]common.RetrievalResult, error) {
	// mode 零值回落子类默认（spec §8-1：Python 签名默认 "vector"）
	if mode == "" {
		mode = RetrievalModeVector
	}
	if mode != RetrievalModeVector {
		return nil, exception.BuildError(
			exception.StatusRetrievalRetrieverModeNotSupport,
			exception.WithParam("error_msg", fmt.Sprintf("VectorRetriever only supports 'vector' mode, got %s", mode)),
		)
	}
	// Python L65-69 的 155201（threshold+mode≠vector）为死分支（前置 mode 校验
	// 先抛 155200），跳过归档 spec §8-2；已由 13.21 HybridRetriever 真实消费

	if r.embedModel == nil {
		return nil, exception.BuildError(
			exception.StatusRetrievalRetrieverEmbedModelNotFound,
			exception.WithParam("error_msg", "embed_model is required for vector search"),
		)
	}

	queryVector, err := r.embedModel.EmbedQuery(ctx, query)
	if err != nil {
		return nil, err // Python await 直接冒泡，不包装
	}
	searchResults, err := r.vectorStore.Search(ctx, queryVector, topK, nil)
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

	retrievalResults := make([]common.RetrievalResult, 0, len(searchResults))
	for _, result := range searchResults {
		// 阈值过滤（Python result.score is not None 的 nil 检查在 Go 无对应物，spec §8-4）
		if scoreThreshold != nil && result.Score < *scoreThreshold {
			continue
		}
		retrievalResults = append(retrievalResults, common.RetrievalResult{
			Text:     result.Text,
			Score:    result.Score,
			Metadata: result.Metadata,
			DocID:    metadataString(result.Metadata, "doc_id"),
			ChunkID:  metadataString(result.Metadata, "chunk_id"), // Vector 从 metadata 取（spec 决策 7）
		})
	}
	return retrievalResults, nil
}

// BatchRetrieve 批量并发检索。
//
// 对齐 Python 裸 asyncio.gather：全启动、其余 goroutine 不取消跑完结果
// 丢弃、错误负载与传播序一致。裸 errgroup（不派生 ctx、不 cancel）。
// 审查修复 M-11（表述修正）：errgroup.Wait 阻塞等全部 goroutine 完成
// 后返回时间首个错误——Python gather 首异常立即向 awaiter 传播，返回
// 时机差异为声明容差（底层 store 挂时调用方延迟 N×timeout，语义等价）。
func (r *VectorRetriever) BatchRetrieve(ctx context.Context, queries []string, topK int, opts ...RetrieveOption) ([][]common.RetrievalResult, error) {
	results := make([][]common.RetrievalResult, len(queries))
	var g errgroup.Group
	for i, q := range queries {
		g.Go(func() error {
			// Python batch_retrieve 不传 mode/score_threshold → 子类默认/None
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

// RetrieveSearchResults 向量检索（原始 SearchResult，不归一化）。
//
// 对齐 Python VectorRetriever.retrieve_search_results：仅 embedModel 校验，
// 无 mode 校验（Python 两作者不对称，字面复刻 spec §8-8）。
func (r *VectorRetriever) RetrieveSearchResults(ctx context.Context, query string, topK int, _ RetrievalMode, _ ...RetrieveOption) ([]common.SearchResult, error) {
	if r.embedModel == nil {
		return nil, exception.BuildError(
			exception.StatusRetrievalRetrieverEmbedModelNotFound,
			exception.WithParam("error_msg", "embed_model is required for vector search"),
		)
	}
	queryVector, err := r.embedModel.EmbedQuery(ctx, query)
	if err != nil {
		return nil, err
	}
	searchResults, err := r.vectorStore.Search(ctx, queryVector, topK, nil)
	if err != nil {
		return nil, err
	}
	if len(searchResults) == 0 {
		searchResults, err = r.vectorStore.SparseSearch(ctx, query, topK, nil)
		if err != nil {
			return nil, err
		}
	}
	return searchResults, nil
}

// Close 关闭检索器。
//
// 对齐 Python close()：if self.vector_store 判空后关底层。
// Python 的 getattr+iscoroutinefunction 鸭子探测在 Go 消解为
// 接口静态 Close() 直调（spec §8-7 语言特性级等价物）。
func (r *VectorRetriever) Close() error {
	if r.vectorStore != nil {
		_ = r.vectorStore.Close()
	}
	return nil
}

// EmbedModel 返回嵌入模型（能力接口回填，13.23；nil = Python None）。
func (r *VectorRetriever) EmbedModel() embedding.BaseEmbedding { return r.embedModel }

// VectorStore 返回底层向量存储（能力接口回填，13.23）。
func (r *VectorRetriever) VectorStore() vector_store.VectorStore { return r.vectorStore }

// IndexType 返回索引类型（"" = Python None，能力接口回填 13.23）。
func (r *VectorRetriever) IndexType() string { return r.indexType }

// SetIndexType 注入索引类型（上层 GraphRetriever 动态创建后回写，13.23）。
func (r *VectorRetriever) SetIndexType(indexType string) { r.indexType = indexType }

// ──────────────────────────── 非导出函数 ────────────────────────────

// metadataString 从 metadata 取字符串值，断言失败或缺失返回空串。
//
// 对齐 Python metadata.get(key) + pydantic lax str 强转：正常链路
// （写入侧 13.5 mergeChunkMetadata 恒写 uuid 字符串）不受影响；
// non-str 值 Python 强转 str(123)="123" vs Go 空串，归档 spec §8-3。
func metadataString(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}
