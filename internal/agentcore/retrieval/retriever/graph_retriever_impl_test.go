package retriever

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
)

// ──────────────────────────── GraphRetriever 专属桩 ────────────────────────────

// fakeGraphChunkRetriever chunk 检索桩（Retriever 四方法，复用 fakeBeamRetriever）
type fakeGraphChunkRetriever struct {
	fakeBeamRetriever
}

// makeChunkResult 构造 chunk 结果
func makeChunkResult(text, chunkID, docID string, score float64) common.RetrievalResult {
	return common.RetrievalResult{
		Text: text, Score: score,
		Metadata: map[string]any{"chunk_id": chunkID, "doc_id": docID},
		ChunkID:  chunkID, DocID: docID,
	}
}

// fakeGraphStore 带 CollectionName 记录的 fakeVectorStore 扩展
type fakeGraphStore struct {
	fakeVectorStore
	lastName string
}

func (f *fakeGraphStore) CollectionName() string        { return f.lastName }
func (f *fakeGraphStore) SetCollectionName(name string) { f.lastName = name }

// closeCountRetriever Close 计数桩
type closeCountRetriever struct {
	fakeBeamRetriever
	closed int
}

func (c *closeCountRetriever) Close() { c.closed++ }

// ──────────────────────────── 用例 ────────────────────────────

// TestGraphRetriever_构造（init_with_retrievers + init_with_vector_store）
func TestGraphRetriever_构造(t *testing.T) {
	cr := &fakeGraphChunkRetriever{}
	gr := NewGraphRetriever(WithGraphChunkRetriever(cr))
	assert.Equal(t, cr, gr.chunkRetriever)

	store := &fakeGraphStore{}
	emb := &fakeBeamEmbedModel{}
	gr2 := NewGraphRetriever(WithGraphVectorStore(store), WithGraphEmbedModel(emb),
		WithGraphChunkCollection("chunks"), WithGraphTripleCollection("triples"))
	assert.Equal(t, store, gr2.vectorStore)
	assert.Equal(t, emb, gr2.embedModel)
	assert.Equal(t, "chunks", gr2.chunkCollection)
	assert.Equal(t, "triples", gr2.tripleCollection)
	assert.Equal(t, "", gr2.indexType)
}

// TestGraphRetriever_retrieve阈值非法模式 155201
func TestGraphRetriever_retrieve阈值非法模式(t *testing.T) {
	gr := NewGraphRetriever(WithGraphChunkRetriever(&fakeGraphChunkRetriever{}))
	threshold := 0.8
	_, err := gr.Retrieve(context.Background(), "test query", 5, &threshold, RetrievalModeSparse)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "score_threshold is only supported")
}

// TestGraphRetriever_模式校验 index_type 未知 155203 / 不兼容 155204
func TestGraphRetriever_模式校验(t *testing.T) {
	gr := NewGraphRetriever()
	gr.SetIndexType("weird")
	_, err := gr.GetRetrieverForMode(RetrievalModeHybrid, true)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Unsupported index_type=weird")

	gr.SetIndexType("vector")
	_, err = gr.GetRetrieverForMode(RetrievalModeSparse, true)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "incompatible with index_type=vector")
	assert.Contains(t, err.Error(), "allowed modes: [vector]") // sorted allowed 清单

	// bm25 只允许 sparse：vector 模式被拒，清单 [sparse]
	gr.SetIndexType("bm25")
	_, err = gr.GetRetrieverForMode(RetrievalModeVector, true)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "incompatible with index_type=bm25")
	assert.Contains(t, err.Error(), "allowed modes: [sparse]")

	// index_type ""（None）免检
	gr2 := NewGraphRetriever()
	_, err = gr2.GetRetrieverForMode(RetrievalModeHybrid, true)
	assert.Error(t, err) // 走到 155206（无 vector_store）——免检通过的证据
	assert.Contains(t, err.Error(), "vector_store is required")
}

// TestGraphRetriever_固定retriever不支持 155205
func TestGraphRetriever_固定retriever不支持(t *testing.T) {
	sr := NewSparseRetriever(&fakeVectorStore{})
	gr := NewGraphRetriever(WithGraphChunkRetriever(sr))
	_, err := gr.GetRetrieverForMode(RetrievalModeVector, true)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "does not support mode=vector")
	assert.Contains(t, err.Error(), "SparseRetriever")
}

// TestGraphRetriever_动态创建分支 + collection 顺序（155207 先污染后校验）
func TestGraphRetriever_动态创建(t *testing.T) {
	// 无 vector_store → 155206
	gr := NewGraphRetriever()
	_, err := gr.GetRetrieverForMode(RetrievalModeHybrid, true)
	assert.Contains(t, err.Error(), "vector_store is required")

	// 有 store 无 collection → 155207（SetCollectionName 先执行——
	// Python L339-346 字面顺序复刻）
	store := &fakeGraphStore{}
	gr = NewGraphRetriever(WithGraphVectorStore(store))
	_, err = gr.GetRetrieverForMode(RetrievalModeHybrid, true)
	assert.Contains(t, err.Error(), "chunk_collection is required")
	assert.Equal(t, "", store.lastName) // SetCollectionName("") 先执行

	// triple 侧同样先污染
	_, err = gr.GetRetrieverForMode(RetrievalModeHybrid, false)
	assert.Contains(t, err.Error(), "triple_collection is required")
	assert.Equal(t, "", store.lastName)

	// vector 分支缺 embed → 155202
	gr = NewGraphRetriever(WithGraphVectorStore(store), WithGraphChunkCollection("c"))
	_, err = gr.GetRetrieverForMode(RetrievalModeVector, true)
	assert.Contains(t, err.Error(), "embed_model is required for vector mode")

	// 三分支构造成功 + index_type 注入回读
	gr = NewGraphRetriever(WithGraphVectorStore(store), WithGraphEmbedModel(&fakeBeamEmbedModel{}),
		WithGraphChunkCollection("c"), WithGraphTripleCollection("t"))
	gr.SetIndexType("hybrid")
	tr, err := gr.GetRetrieverForMode(RetrievalModeHybrid, false)
	require.NoError(t, err)
	hr, ok := tr.(*HybridRetriever)
	assert.True(t, ok)
	assert.Equal(t, "hybrid", hr.indexType) // 注入回读（同包可读）
	assert.Equal(t, "t", store.lastName)    // tripleCollection 注入
}

// TestGraphRetriever_graphExpansion空chunks（test_graph_expansion_empty_chunks）
func TestGraphRetriever_graphExpansion空chunks(t *testing.T) {
	gr := NewGraphRetriever(WithGraphChunkRetriever(&fakeGraphChunkRetriever{}))
	results, err := gr.GraphExpansion(context.Background(), "q", nil, nil, nil, RetrievalModeHybrid)
	assert.NoError(t, err)
	assert.Empty(t, results)
}

// TestGraphRetriever_Close 两个固定 retriever 的 Close 被调
func TestGraphRetriever_Close(t *testing.T) {
	cr := &closeCountRetriever{}
	tr := &closeCountRetriever{}
	gr := NewGraphRetriever(WithGraphChunkRetriever(cr), WithGraphTripleRetriever(tr))
	gr.Close()
	assert.Equal(t, 1, cr.closed)
	assert.Equal(t, 1, tr.closed)
}

// TestGraphRetriever_RetrieveSearchResults返回nil 对齐 Python 基类 None
func TestGraphRetriever_RetrieveSearchResults返回nil(t *testing.T) {
	gr := NewGraphRetriever()
	rs, err := gr.RetrieveSearchResults(context.Background(), "q", 5, RetrievalModeHybrid)
	assert.NoError(t, err)
	assert.Nil(t, rs)
}

// TestGraphRetriever_主链retrieve无triples回落 chunk 检索 → fetch 无 client
// 能力（fakeGraphStore 非 Milvus/Chroma 具体类型）→ warn 回落空 → 返回原
// chunks（无 topk 截断 nil）
func TestGraphRetriever_主链retrieve无triples回落(t *testing.T) {
	chunkRetriever := &fakeGraphChunkRetriever{fakeBeamRetriever{retrieveFn: func(context.Context, string, int) ([]common.RetrievalResult, error) {
		return []common.RetrievalResult{
			makeChunkResult("chunk1", "c1", "d1", 0.9),
			makeChunkResult("chunk2", "c2", "d2", 0.8),
		}, nil
	}}}
	store := &fakeGraphStore{}
	gr := NewGraphRetriever(
		WithGraphChunkRetriever(chunkRetriever),
		WithGraphVectorStore(store),
		WithGraphTripleCollection("t"), WithGraphChunkCollection("c"),
	)
	// triple 侧动态创建成功（sparse 不需要 embed），fetchTriples 走
	// store 具体类型断言 → fakeGraphStore 不匹配 → Unsupported → triples 空
	results, err := gr.Retrieve(context.Background(), "q", 2, nil, RetrievalModeSparse)
	require.NoError(t, err)
	require.Len(t, results, 2)
	assert.Equal(t, "chunk1", results[0].Text)
	assert.Equal(t, "chunk2", results[1].Text)
}

// TestGraphRetriever_空chunks_sparse回落 mode=sparse 空 chunks 二次检索
func TestGraphRetriever_空chunks_sparse回落(t *testing.T) {
	sparseFallback := &fakeGraphChunkRetriever{fakeBeamRetriever{retrieveFn: func(context.Context, string, int) ([]common.RetrievalResult, error) {
		return []common.RetrievalResult{
			makeChunkResult("fallback1", "f1", "fd1", 0.7),
			makeChunkResult("fallback2", "f2", "fd2", 0.6),
			makeChunkResult("fallback3", "f3", "fd3", 0.5),
		}, nil
	}}}
	gr := NewGraphRetriever(WithGraphChunkRetriever(sparseFallback))
	// 首检索返回空（retrieveFn 同桩）→ sparse 二次回落
	results, err := gr.Retrieve(context.Background(), "q", 2, nil, RetrievalModeSparse)
	require.NoError(t, err)
	assert.Len(t, results, 2) // fallback[:2] 截断（topk=2）
	assert.Equal(t, "fallback1", results[0].Text)
}

// TestGraphRetriever_空chunks_sparse回落默认5 topk=nil → topk or 5
func TestGraphRetriever_空chunks_sparse回落默认5(t *testing.T) {
	// GraphExpansion 直接调：topk nil → fallbackTopK=5
	fallback := &fakeGraphChunkRetriever{fakeBeamRetriever{retrieveFn: func(context.Context, string, int) ([]common.RetrievalResult, error) {
		return []common.RetrievalResult{
			makeChunkResult("f1", "c1", "d", 0.7),
			makeChunkResult("f2", "c2", "d", 0.6),
		}, nil
	}}}
	gr := NewGraphRetriever(WithGraphChunkRetriever(fallback))
	results, err := gr.GraphExpansion(context.Background(), "q", nil, nil, nil, RetrievalModeSparse)
	require.NoError(t, err)
	assert.Len(t, results, 2) // 无 topk → 5 兜底，不截断
}

// TestGraphRetriever_空chunks非sparse返回空
func TestGraphRetriever_空chunks非sparse返回空(t *testing.T) {
	gr := NewGraphRetriever(WithGraphChunkRetriever(&fakeGraphChunkRetriever{}))
	results, err := gr.GraphExpansion(context.Background(), "q", nil, nil, nil, RetrievalModeHybrid)
	assert.NoError(t, err)
	assert.Empty(t, results)
}

// TestGraphRetriever_graphExpansion显式triples跳过fetch triples 非 nil
// → 不走 fetchTriples → beam search → 扩展
func TestGraphRetriever_graphExpansion显式triples跳过fetch(t *testing.T) {
	// triple 检索桩：无候选 → beams 保原 → fetchChunks 走
	// fakeGraphStore Unsupported → newChunks 空 → fused = chunks
	tripleRetriever := &withEmbedRetriever{fakeBeamRetriever{retrieveFn: func(context.Context, string, int) ([]common.RetrievalResult, error) {
		return nil, nil
	}}, &fakeBeamEmbedModel{batchSize: 32, batches: [][][]float64{
		{{1, 0}, {1, 0}}, // 1 triple + query（首轮）
		{{1, 0}},         // 扩展轮：保原路径 1 条
	}}}
	chunkRetriever := &fakeGraphChunkRetriever{fakeBeamRetriever{retrieveFn: func(context.Context, string, int) ([]common.RetrievalResult, error) {
		return []common.RetrievalResult{makeChunkResult("orig", "c0", "d0", 0.9)}, nil
	}}}
	store := &fakeGraphStore{}
	gr := NewGraphRetriever(
		WithGraphChunkRetriever(chunkRetriever),
		WithGraphTripleRetriever(tripleRetriever),
		WithGraphVectorStore(store),
		WithGraphChunkCollection("c"), WithGraphTripleCollection("t"),
	)
	chunks := []common.RetrievalResult{makeChunkResult("orig", "c0", "d0", 0.9)}
	triples := []common.RetrievalResult{makeTriple("a r b", `["a","r","b"]`, 0.9)}
	topk := 3
	results, err := gr.GraphExpansion(context.Background(), "q", chunks, triples, &topk, RetrievalModeSparse)
	require.NoError(t, err)
	// beam 扩展成功但 fetchChunks Unsupported → newChunks 空 → fused=chunks
	require.Len(t, results, 1)
	assert.Equal(t, "orig", results[0].Text)
}

// TestGraphRetriever_beam失败降级 embed 桩批次耗尽 → BeamSearch err →
// warn 降级返回原 chunks[:topk]（Python graph_expansion except 降级终点）
func TestGraphRetriever_beam失败降级(t *testing.T) {
	tripleRetriever := &withEmbedRetriever{fakeBeamRetriever{retrieveFn: func(context.Context, string, int) ([]common.RetrievalResult, error) {
		return nil, nil
	}}, &fakeBeamEmbedModel{batchSize: 32, batches: [][][]float64{
		{{1, 0}, {1, 0}}, // 首轮 OK
		// 扩展轮无批次 → "no more batches" 错误 → BeamSearch err → 降级
	}}}
	gr := NewGraphRetriever(WithGraphTripleRetriever(tripleRetriever))
	chunks := []common.RetrievalResult{
		makeChunkResult("c1", "ch1", "d1", 0.9),
		makeChunkResult("c2", "ch2", "d2", 0.8),
	}
	triples := []common.RetrievalResult{makeTriple("a r b", `["a","r","b"]`, 0.9)}
	topk := 1
	results, err := gr.GraphExpansion(context.Background(), "q", chunks, triples, &topk, RetrievalModeSparse)
	require.NoError(t, err)
	require.Len(t, results, 1) // 降级返回原 chunks[:1]
	assert.Equal(t, "c1", results[0].Text)
}

// TestGraphRetriever_GraphHops透传 retrieve 的 graph_hops → beam maxLength
func TestGraphRetriever_GraphHops透传(t *testing.T) {
	// graph_hops=1 → maxLength=1 → 不进扩展轮 → 只需 1 批嵌入
	tripleRetriever := &withEmbedRetriever{fakeBeamRetriever{retrieveFn: func(context.Context, string, int) ([]common.RetrievalResult, error) {
		return nil, nil
	}}, &fakeBeamEmbedModel{batchSize: 32, batches: [][][]float64{
		{{1, 0}, {1, 0}}, // 仅首轮
	}}}
	chunkRetriever := &fakeGraphChunkRetriever{fakeBeamRetriever{retrieveFn: func(context.Context, string, int) ([]common.RetrievalResult, error) {
		return []common.RetrievalResult{makeChunkResult("orig", "c0", "d0", 0.9)}, nil
	}}}
	gr := NewGraphRetriever(
		WithGraphChunkRetriever(chunkRetriever),
		WithGraphTripleRetriever(tripleRetriever),
	)
	// graph_hops=1：单轮 beam，无扩展轮嵌入消耗 → 不触发批次耗尽错误
	results, err := gr.Retrieve(context.Background(), "q", 1, nil, RetrievalModeSparse,
		WithRetrieveGraphHops(1))
	require.NoError(t, err)
	assert.Equal(t, "orig", results[0].Text)
}

// TestGraphRetriever_GraphHops零值报155204 显式 0 → max_length=0 → 构造错
func TestGraphRetriever_GraphHops零值报155204(t *testing.T) {
	tripleRetriever := &withEmbedRetriever{fakeBeamRetriever{retrieveFn: func(context.Context, string, int) ([]common.RetrievalResult, error) {
		return nil, nil
	}}, &fakeBeamEmbedModel{batchSize: 32, batches: [][][]float64{
		{{1, 0}, {1, 0}},
	}}}
	chunkRetriever := &fakeGraphChunkRetriever{fakeBeamRetriever{retrieveFn: func(context.Context, string, int) ([]common.RetrievalResult, error) {
		return []common.RetrievalResult{makeChunkResult("orig", "c0", "d0", 0.9)}, nil
	}}}
	gr := NewGraphRetriever(
		WithGraphChunkRetriever(chunkRetriever),
		WithGraphTripleRetriever(tripleRetriever),
	)
	// graph_hops=0 → beam search 构造失败（155204）→ 降级返回原 chunks
	// （Python：max_length=0 kwargs 传入 TripleBeamSearch → 构造炸 → except 降级）
	results, err := gr.Retrieve(context.Background(), "q", 1, nil, RetrievalModeSparse,
		WithRetrieveGraphHops(0))
	require.NoError(t, err) // 降级吞错
	assert.Equal(t, "orig", results[0].Text)
}

// TestGraphRetriever_topk语义 topk nil 不截断
func TestGraphRetriever_topk语义(t *testing.T) {
	gr := NewGraphRetriever(WithGraphChunkRetriever(&fakeGraphChunkRetriever{}))
	// 空 chunks + 非 sparse → 返回空（topk nil 路径不炸）
	results, err := gr.GraphExpansion(context.Background(), "q", nil, nil, nil, RetrievalModeHybrid)
	assert.NoError(t, err)
	assert.Empty(t, results)

	// 无 triples：topk nil → 返回全部原 chunks
	chunkRetriever := &fakeGraphChunkRetriever{fakeBeamRetriever{retrieveFn: func(context.Context, string, int) ([]common.RetrievalResult, error) {
		return []common.RetrievalResult{
			makeChunkResult("c1", "ch1", "d1", 0.9),
			makeChunkResult("c2", "ch2", "d2", 0.8),
		}, nil
	}}}
	store := &fakeGraphStore{}
	gr = NewGraphRetriever(
		WithGraphChunkRetriever(chunkRetriever),
		WithGraphVectorStore(store),
		WithGraphTripleCollection("t"),
	)
	results, err = gr.GraphExpansion(context.Background(), "q",
		[]common.RetrievalResult{makeChunkResult("c1", "ch1", "d1", 0.9), makeChunkResult("c2", "ch2", "d2", 0.8)},
		nil, nil, RetrievalModeSparse)
	require.NoError(t, err)
	assert.Len(t, results, 2) // topk nil 不截断
}

// TestGraphRetriever_BatchRetrieve 批量检索透传（裸 errgroup）
func TestGraphRetriever_BatchRetrieve(t *testing.T) {
	chunkRetriever := &fakeGraphChunkRetriever{fakeBeamRetriever{retrieveFn: func(_ context.Context, query string, _ int) ([]common.RetrievalResult, error) {
		return []common.RetrievalResult{makeChunkResult(query, "c1", "d1", 0.9)}, nil
	}}}
	gr := NewGraphRetriever(WithGraphChunkRetriever(chunkRetriever))
	results, err := gr.BatchRetrieve(context.Background(), []string{"q1", "q2"}, 3)
	require.NoError(t, err)
	require.Len(t, results, 2)
	assert.Equal(t, "q1", results[0][0].Text)
	assert.Equal(t, "q2", results[1][0].Text)
}

// TestGraphRetriever_mode零值回落 Python 签名默认 hybrid（spec §8-1 同构）
func TestGraphRetriever_mode零值回落(t *testing.T) {
	chunkRetriever := &fakeGraphChunkRetriever{fakeBeamRetriever{retrieveFn: func(context.Context, string, int) ([]common.RetrievalResult, error) {
		return []common.RetrievalResult{makeChunkResult("c1", "ch1", "d1", 0.9)}, nil
	}}}
	// 检验 mode 断言透传：零值回落 hybrid 后，固定 SparseRetriever 不支持
	// hybrid → 155205（证明回落生效；若未回落 sparse 会通过）
	sr := NewSparseRetriever(&fakeVectorStore{})
	gr := NewGraphRetriever(WithGraphChunkRetriever(sr))
	_, err := gr.Retrieve(context.Background(), "q", 1, nil, "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "does not support mode=hybrid")

	// GraphExpansion 同款零值回落
	gr2 := NewGraphRetriever(WithGraphChunkRetriever(chunkRetriever))
	results, err := gr2.GraphExpansion(context.Background(), "q", nil, nil, nil, "")
	assert.NoError(t, err) // 非 sparse 空 chunks → 空
	assert.Empty(t, results)
}
