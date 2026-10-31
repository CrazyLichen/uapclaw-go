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
	// indexType 索引类型（"" = Python None；上层 GraphRetriever 注入，
	// 13.23 回填）
	indexType string
}

// HybridRetrieverOption HybridRetriever 的构造选项。
type HybridRetrieverOption func(*HybridRetriever)

// 编译期断言：HybridRetriever 满足 Retriever 接口。
var _ Retriever = (*HybridRetriever)(nil)

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

// BatchRetrieve 批量并发检索。
//
// 对齐 Python HybridRetriever.batch_retrieve（L132-144）裸 asyncio.gather：
// 全启动、其余不取消跑完结果丢弃、错误负载与传播序一致（裸 errgroup
// 不派生 ctx 不 cancel，13.20 两兄弟同构）。审查修复 M-11（表述修正）：
// Wait 阻塞等全部完成后返回时间首个错误，Python 首异常立即传播——
// 返回时机差异为声明容差。alpha 经 opts 随 batch 透传（对齐 Python
// **kwargs 展开调用 self.retrieve(query, top_k=top_k, **kwargs)）。
func (r *HybridRetriever) BatchRetrieve(ctx context.Context, queries []string, topK int, opts ...RetrieveOption) ([][]common.RetrievalResult, error) {
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

// RetrieveSearchResults 检索原始 SearchResult（不归一化、不阈值过滤）。
//
// 对齐 Python retrieve_search_results（L146-209）：三分支树与 Retrieve
// 完全相同，差异两处——无 155201 threshold 前置校验（Python rsr 没有
// 这段，同文件不对称字面复刻 spec §8-5）、无归一化循环（直接返回）。
func (r *HybridRetriever) RetrieveSearchResults(ctx context.Context, query string, topK int, mode RetrievalMode, opts ...RetrieveOption) ([]common.SearchResult, error) {
	options := NewRetrieveOptions(opts...)
	alpha := r.alpha
	if options.Alpha != nil {
		alpha = *options.Alpha
	}

	// mode 零值回落子类默认（spec §8-1：Python 签名默认 "hybrid"）
	if mode == "" {
		mode = RetrievalModeHybrid
	}

	switch mode {
	case RetrievalModeHybrid:
		// 混合检索：embed 可选，nil 向量走 store 层纯稀疏路径
		var queryVector []float64
		var err error
		if r.embedModel != nil {
			queryVector, err = r.embedModel.EmbedQuery(ctx, query)
			if err != nil {
				return nil, err // Python await 直接冒泡，不包装
			}
		}
		return r.vectorStore.HybridSearch(ctx, query, queryVector, topK, alpha, nil)
	case RetrievalModeVector:
		// 纯向量检索：embed 必需
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
		// 向量检索空结果时兜底 BM25（对齐 Python fallback to sparse）
		if len(searchResults) == 0 {
			searchResults, err = r.vectorStore.SparseSearch(ctx, query, topK, nil)
			if err != nil {
				return nil, err
			}
		}
		return searchResults, nil
	case RetrievalModeSparse:
		// 纯稀疏检索（BM25）
		return r.vectorStore.SparseSearch(ctx, query, topK, nil)
	default:
		return nil, exception.BuildError(
			exception.StatusRetrievalRetrieverModeNotSupport,
			exception.WithParam("error_msg", fmt.Sprintf("Unsupported mode: %s", mode)),
		)
	}
}

// Close 关闭检索器。
//
// 对齐 Python close()（L211-221）：if self.vector_store 判空后关底层。
// Python 的 getattr+iscoroutinefunction 鸭子探测消解为接口静态
// Close() 直调（spec §8-7，同 13.20 两兄弟）。
func (r *HybridRetriever) Close() error {
	if r.vectorStore == nil {
		return nil
	}
	// 底层 store 的错误必须上抛：Close 声明了 error，吞掉会让上层错误聚合
	// （如 GraphRetriever.firstErr）永远不可达。
	return r.vectorStore.Close()
}

// EmbedModel 返回嵌入模型（能力接口回填，13.23；nil = Python None）。
func (r *HybridRetriever) EmbedModel() embedding.BaseEmbedding { return r.embedModel }

// VectorStore 返回底层向量存储（能力接口回填，13.23）。
func (r *HybridRetriever) VectorStore() vector_store.VectorStore { return r.vectorStore }

// IndexType 返回索引类型（"" = Python None，能力接口回填 13.23）。
func (r *HybridRetriever) IndexType() string { return r.indexType }

// SetIndexType 注入索引类型（上层 GraphRetriever 动态创建后回写，13.23）。
func (r *HybridRetriever) SetIndexType(indexType string) { r.indexType = indexType }
