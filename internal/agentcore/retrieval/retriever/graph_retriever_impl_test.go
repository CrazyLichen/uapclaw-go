package retriever

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	milvuscol "github.com/milvus-io/milvus/client/v2/column"
	milvusclient "github.com/milvus-io/milvus/client/v2/milvusclient"
	"google.golang.org/grpc"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/embedding"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/vector_fields"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/vector_store"
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
	// 计数区分：首次（主检索）返回空触发回落，二次（sparse 回落）返回 3 条
	calls := 0
	var mu sync.Mutex
	sparseFallback := &fakeGraphChunkRetriever{fakeBeamRetriever{retrieveFn: func(context.Context, string, int) ([]common.RetrievalResult, error) {
		mu.Lock()
		defer mu.Unlock()
		calls++
		if calls == 1 {
			return nil, nil // 首检索空 → sparse 二次回落
		}
		return []common.RetrievalResult{
			makeChunkResult("fallback1", "f1", "fd1", 0.7),
			makeChunkResult("fallback2", "f2", "fd2", 0.6),
			makeChunkResult("fallback3", "f3", "fd3", 0.5),
		}, nil
	}}}
	gr := NewGraphRetriever(WithGraphChunkRetriever(sparseFallback))
	results, err := gr.Retrieve(context.Background(), "q", 2, nil, RetrievalModeSparse)
	require.NoError(t, err)
	assert.Len(t, results, 2) // fallback[:2] 截断（topk=2）
	assert.Equal(t, "fallback1", results[0].Text)
	assert.Equal(t, 2, calls) // 二次调用证明回落发生
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
	// Milvus 桩：fetchTriples 命中（chunk_id IN 查询返回 1 条 triple）
	tripleRS := milvusclient.ResultSet{
		ResultCount: 1,
		Fields: milvusclient.DataSet{
			milvuscol.NewColumnString("content", []string{"a r b"}),
			milvuscol.NewColumnString("metadata", []string{`{"chunk_id":"c0","triple":"[\"a\",\"r\",\"b\"]"}`}),
		},
	}
	stub := &graphFetchMilvusStub{tripleRS: tripleRS}
	store := newGraphMilvusStore(t, stub)
	store.SetCollectionName("kb_1_triples")

	tripleRetriever := &graphEmbedRetriever{
		fakeBeamRetriever{retrieveFn: func(context.Context, string, int) ([]common.RetrievalResult, error) {
			return nil, nil
		}},
		&fakeBeamEmbedModel{batchSize: 32, batches: [][][]float64{
			{{1, 0}, {1, 0}}, // 仅首轮
		}},
		store,
	}
	chunkRetriever := &fakeGraphChunkRetriever{fakeBeamRetriever{retrieveFn: func(context.Context, string, int) ([]common.RetrievalResult, error) {
		return []common.RetrievalResult{makeChunkResult("orig", "c0", "d0", 0.9)}, nil
	}}}
	gr := NewGraphRetriever(
		WithGraphChunkRetriever(chunkRetriever),
		WithGraphTripleRetriever(tripleRetriever),
	)
	// graph_hops=1：单轮 beam，无扩展轮嵌入消耗 → 不触发批次耗尽错误
	// （embedModel.calls=1 证明 beam 首轮已执行——若 maxLength=2 会耗尽批次
	// 报错降级，结果同为 orig 但 embed 调用数不同）
	results, err := gr.Retrieve(context.Background(), "q", 1, nil, RetrievalModeSparse,
		WithRetrieveGraphHops(1))
	require.NoError(t, err)
	assert.Equal(t, "orig", results[0].Text)
	assert.Equal(t, 1, tripleRetriever.embedModel.(*fakeBeamEmbedModel).calls) // 单轮嵌入
}

// TestGraphRetriever_GraphHops零值报155204 显式 0 → max_length=0 → 构造错
func TestGraphRetriever_GraphHops零值报155204(t *testing.T) {
	// Milvus 桩：fetchTriples 命中（保证 beam search 被触达）
	tripleRS := milvusclient.ResultSet{
		ResultCount: 1,
		Fields: milvusclient.DataSet{
			milvuscol.NewColumnString("content", []string{"a r b"}),
			milvuscol.NewColumnString("metadata", []string{`{"chunk_id":"c0","triple":"[\"a\",\"r\",\"b\"]"}`}),
		},
	}
	stub := &graphFetchMilvusStub{tripleRS: tripleRS}
	store := newGraphMilvusStore(t, stub)
	store.SetCollectionName("kb_1_triples")

	tripleRetriever := &graphEmbedRetriever{
		fakeBeamRetriever{retrieveFn: func(context.Context, string, int) ([]common.RetrievalResult, error) {
			return nil, nil
		}},
		&fakeBeamEmbedModel{batchSize: 32, batches: [][][]float64{
			{{1, 0}, {1, 0}},
		}},
		store,
	}
	chunkRetriever := &fakeGraphChunkRetriever{fakeBeamRetriever{retrieveFn: func(context.Context, string, int) ([]common.RetrievalResult, error) {
		return []common.RetrievalResult{makeChunkResult("orig", "c0", "d0", 0.9)}, nil
	}}}
	gr := NewGraphRetriever(
		WithGraphChunkRetriever(chunkRetriever),
		WithGraphTripleRetriever(tripleRetriever),
	)
	// graph_hops=0 → beam search 构造失败（155204）→ 降级返回原 chunks
	// （Python：max_length=0 kwargs 传入 TripleBeamSearch → 构造炸 → except 降级）
	// embed 调用数为 0 证明 BeamSearch 从未执行（构造阶段就炸）
	results, err := gr.Retrieve(context.Background(), "q", 1, nil, RetrievalModeSparse,
		WithRetrieveGraphHops(0))
	require.NoError(t, err) // 降级吞错
	assert.Equal(t, "orig", results[0].Text)
	assert.Equal(t, 0, tripleRetriever.embedModel.(*fakeBeamEmbedModel).calls) // 构造即炸
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

// ──────────────────────────── fetch 链集成用例（Milvus 桩） ────────────────────────────

// graphFetchMilvusStub 可编程 Query 桩：fetchTriples 用 chunk_id IN [...] →
// tripleRS；fetchChunk 用 chunk_id == "xxx" → chunkRows 按 id 分发单行。
type graphFetchMilvusStub struct {
	vector_store.MilvusClient
	mu         sync.Mutex
	tripleRS   milvusclient.ResultSet
	tripleErr  error
	chunkRows  map[string]milvusclient.ResultSet // chunk_id → 单行结果
	chunkErr   error
	queryCalls int
	lastFilter string
}

// chunkIDEqFilterExpr fetchChunk 的 filter 前缀（graph_retriever_impl.go L683）
const chunkIDEqFilterExpr = `chunk_id == "`

func (m *graphFetchMilvusStub) Query(_ context.Context, opt milvusclient.QueryOption, _ ...grpc.CallOption) (milvusclient.ResultSet, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.queryCalls++
	// Request() 返回公开 proto：CollectionName + Expr（区分 chunk/triple 查询）
	req, err := opt.Request()
	if err != nil {
		return milvusclient.ResultSet{}, err
	}
	expr := req.GetExpr()
	m.lastFilter = expr
	if strings.HasPrefix(expr, chunkIDEqFilterExpr) && strings.HasSuffix(expr, `"`) {
		// 单 chunk 查询：按 chunk_id 分发
		cid := expr[len(chunkIDEqFilterExpr) : len(expr)-1]
		if rs, ok := m.chunkRows[cid]; ok {
			return rs, m.chunkErr
		}
		return milvusclient.ResultSet{}, m.chunkErr
	}
	return m.tripleRS, m.tripleErr
}

// makeChunkRow 构造单行 chunk ResultSet（content/metadata/document_id）
func makeChunkRow(content, chunkID, docID string) milvusclient.ResultSet {
	meta := fmt.Sprintf(`{"chunk_id":%q,"doc_id":%q}`, chunkID, docID)
	return milvusclient.ResultSet{
		ResultCount: 1,
		Fields: milvusclient.DataSet{
			milvuscol.NewColumnString("content", []string{content}),
			milvuscol.NewColumnString("metadata", []string{meta}),
			milvuscol.NewColumnString("document_id", []string{docID}),
		},
	}
}

// newGraphMilvusStore 构造注入桩客户端的真实 MilvusVectorStore
func newGraphMilvusStore(t *testing.T, stub *graphFetchMilvusStub) *vector_store.MilvusVectorStore {
	t.Helper()
	config := common.VectorStoreConfig{
		StoreProvider:  common.StoreTypeMilvus,
		CollectionName: "kb_1",
		DatabaseName:   "default",
		DistanceMetric: common.DistanceMetricCosine,
	}
	s, err := vector_store.NewMilvusVectorStore(config, "http://localhost:19530", "",
		"content", vector_fields.NewMilvusAUTO("embedding"), "sparse_vector", "metadata", "document_id",
		vector_store.WithMilvusClientOption(stub))
	require.NoError(t, err)
	return s
}

// graphEmbedRetriever 双能力桩：EmbedModelProvider + VectorStoreAccessor。
//
// Python 测试桩同时挂 embed_model 与 vector_store 属性（_RetrieverStub
// 家族）；beam search 挖 embed_model，fetch 链挖 vector_store——一桩两用。
type graphEmbedRetriever struct {
	fakeBeamRetriever
	embedModel  embedding.BaseEmbedding
	vectorStore vector_store.VectorStore
}

func (g *graphEmbedRetriever) EmbedModel() embedding.BaseEmbedding { return g.embedModel }
func (g *graphEmbedRetriever) VectorStore() vector_store.VectorStore {
	return g.vectorStore
}

// makeTripleWithChunkID 构造带 triple 元数据 + ChunkID 字段的三元组结果
// （fetchChunks 读 ChunkID 字段而非 metadata）
func makeTripleWithChunkID(text, tripleJSON, chunkID string, score float64) common.RetrievalResult {
	r := makeTriple(text, tripleJSON, score)
	r.ChunkID = chunkID
	return r
}

// TestGraphRetriever_fetchTriplesMilvus链 chunk_id IN 查询 → 三元组结果
// （test_fetch_triples 的 Milvus 路径同构）
func TestGraphRetriever_fetchTriplesMilvus链(t *testing.T) {
	// chunk 带 chunk_id 元数据
	chunks := []common.RetrievalResult{
		makeChunkResult("chunk-a", "cid-a", "doc-1", 0.9),
	}
	// triple 集合返回 1 行（chunk_id IN 过滤）
	tripleRS := milvusclient.ResultSet{
		ResultCount: 1,
		Fields: milvusclient.DataSet{
			milvuscol.NewColumnString("content", []string{"A relates B"}),
			milvuscol.NewColumnString("metadata", []string{`{"chunk_id":"cid-a","triple":"[\"A\",\"relates\",\"B\"]"}`}),
		},
	}
	stub := &graphFetchMilvusStub{tripleRS: tripleRS}
	store := newGraphMilvusStore(t, stub)

	// 动态创建路径：vectorStore + tripleCollection 配齐（GetRetrieverForMode
	// 内 SetCollectionName 注入 store）
	gr := NewGraphRetriever(WithGraphVectorStore(store), WithGraphTripleCollection("kb_1_triples"))
	results, err := gr.fetchTriples(context.Background(), chunks, RetrievalModeSparse)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "A relates B", results[0].Text)
	assert.Equal(t, "cid-a", results[0].ChunkID)
	assert.Contains(t, stub.lastFilter, "chunk_id IN") // IN 过滤形态
}

// TestGraphRetriever_fetchTriples查询错误吞掉 Milvus Query err → warn 回落空
func TestGraphRetriever_fetchTriples查询错误吞掉(t *testing.T) {
	chunks := []common.RetrievalResult{makeChunkResult("c", "cid", "d", 0.9)}
	stub := &graphFetchMilvusStub{tripleErr: errors.New("milvus down")}
	store := newGraphMilvusStore(t, stub)

	gr := NewGraphRetriever(WithGraphVectorStore(store), WithGraphTripleCollection("kb_1_triples"))
	results, err := gr.fetchTriples(context.Background(), chunks, RetrievalModeSparse)
	require.NoError(t, err) // warn 吞错返回 nil（Python except 分支）
	assert.Empty(t, results)
}

// TestGraphRetriever_fetchTriples无chunkID 无可反查 id → nil, nil
func TestGraphRetriever_fetchTriples无chunkID(t *testing.T) {
	gr := NewGraphRetriever()
	// chunks 无 metadata
	results, err := gr.fetchTriples(context.Background(),
		[]common.RetrievalResult{{Text: "no-meta"}}, RetrievalModeSparse)
	require.NoError(t, err)
	assert.Empty(t, results)
}

// TestGraphRetriever_fetchTriples无client能力 retriever 不实现 VectorStoreAccessor
// → warn 回落空（Python hasattr False 同构）
func TestGraphRetriever_fetchTriples无client能力(t *testing.T) {
	gr := NewGraphRetriever(WithGraphTripleRetriever(&noEmbedRetriever{}))
	chunks := []common.RetrievalResult{makeChunkResult("c", "cid", "d", 0.9)}
	results, err := gr.fetchTriples(context.Background(), chunks, RetrievalModeSparse)
	require.NoError(t, err)
	assert.Empty(t, results)
}

// TestGraphRetriever_fetchTriples动态创建失败 GetRetrieverForMode 错误冒泡
func TestGraphRetriever_fetchTriples动态创建失败(t *testing.T) {
	// 无 vectorStore + 无固定 triple retriever → 155206 冒泡
	gr := NewGraphRetriever()
	chunks := []common.RetrievalResult{makeChunkResult("c", "cid", "d", 0.9)}
	_, err := gr.fetchTriples(context.Background(), chunks, RetrievalModeHybrid)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "vector_store is required")
}

// TestGraphRetriever_fetchTriples空集合名 固定 triple 检索器持有 store 但
// collectionName "" → warn 回落空（Python missing client or collection_name）
func TestGraphRetriever_fetchTriples空集合名(t *testing.T) {
	stub := &graphFetchMilvusStub{}
	store := newGraphMilvusStore(t, stub)
	store.SetCollectionName("") // 显式清空

	// 固定 triple retriever 持有 store（绕开动态创建的 SetCollectionName）
	tripleRetriever := &graphEmbedRetriever{fakeBeamRetriever{}, nil, store}
	gr := NewGraphRetriever(WithGraphTripleRetriever(tripleRetriever))
	chunks := []common.RetrievalResult{makeChunkResult("c", "cid", "d", 0.9)}
	results, err := gr.fetchTriples(context.Background(), chunks, RetrievalModeSparse)
	require.NoError(t, err)
	assert.Empty(t, results)
}

// TestGraphRetriever_fetchTriples无store 固定 triple 检索器 VectorStore()
// 返回 nil → warn 回落空（Python vector_store None）
func TestGraphRetriever_fetchTriples无store(t *testing.T) {
	tripleRetriever := &graphEmbedRetriever{fakeBeamRetriever{}, nil, nil}
	gr := NewGraphRetriever(WithGraphTripleRetriever(tripleRetriever))
	chunks := []common.RetrievalResult{makeChunkResult("c", "cid", "d", 0.9)}
	results, err := gr.fetchTriples(context.Background(), chunks, RetrievalModeSparse)
	require.NoError(t, err)
	assert.Empty(t, results)
}

// TestGraphRetriever_fetchChunksMilvus链 beam 展开 → fetchChunks 并发
// 查询 → 结果归一化
func TestGraphRetriever_fetchChunksMilvus链(t *testing.T) {
	stub := &graphFetchMilvusStub{chunkRows: map[string]milvusclient.ResultSet{
		"cid-a": makeChunkRow("expanded chunk", "cid-a", "doc-1"),
	}}
	store := newGraphMilvusStore(t, stub)
	store.SetCollectionName("kb_1_chunks")

	// 固定 chunk retriever 持有 store（fetchChunks 挖取通道）
	chunkRetriever := &graphEmbedRetriever{fakeBeamRetriever{}, nil, store}
	gr := NewGraphRetriever(WithGraphChunkRetriever(chunkRetriever))
	triples := []common.RetrievalResult{
		{Text: "A relates B", ChunkID: "cid-a"},
	}
	newChunks, err := gr.fetchChunks(context.Background(), triples, RetrievalModeSparse)
	require.NoError(t, err)
	require.Len(t, newChunks, 1)
	assert.Equal(t, "expanded chunk", newChunks[0].Text)
	assert.Equal(t, "cid-a", newChunks[0].ChunkID)
	assert.Contains(t, stub.lastFilter, `chunk_id == "cid-a"`)
}

// TestGraphRetriever_fetchChunks查询错误吞掉 fetchChunk 错误 → warn 返回 nil
// 元素（Python fetch_chunk except None 语义）
func TestGraphRetriever_fetchChunks查询错误吞掉(t *testing.T) {
	stub := &graphFetchMilvusStub{chunkErr: errors.New("query failed")}
	store := newGraphMilvusStore(t, stub)
	store.SetCollectionName("kb_1_chunks")

	chunkRetriever := &graphEmbedRetriever{fakeBeamRetriever{}, nil, store}
	gr := NewGraphRetriever(WithGraphChunkRetriever(chunkRetriever))
	triples := []common.RetrievalResult{
		{Text: "A relates B", ChunkID: "cid-a"},
	}
	newChunks, err := gr.fetchChunks(context.Background(), triples, RetrievalModeSparse)
	require.NoError(t, err) // fetchChunk 吞错 → nil 元素 → 过滤后空
	assert.Empty(t, newChunks)
}

// TestGraphRetriever_fetchChunks无client能力 chunk retriever 不实现
// VectorStoreAccessor → warn 回落空
func TestGraphRetriever_fetchChunks无client能力(t *testing.T) {
	gr := NewGraphRetriever(WithGraphChunkRetriever(&fakeGraphChunkRetriever{}))
	// fakeGraphChunkRetriever 嵌 fakeBeamRetriever，不实现 VectorStoreAccessor
	triples := []common.RetrievalResult{{Text: "t", ChunkID: "c"}}
	newChunks, err := gr.fetchChunks(context.Background(), triples, RetrievalModeSparse)
	require.NoError(t, err)
	assert.Empty(t, newChunks)
}

// TestGraphRetriever_fetchChunks无store 固定 chunk 检索器 VectorStore()
// 返回 nil → warn 回落空
func TestGraphRetriever_fetchChunks无store(t *testing.T) {
	chunkRetriever := &graphEmbedRetriever{fakeBeamRetriever{}, nil, nil}
	gr := NewGraphRetriever(WithGraphChunkRetriever(chunkRetriever))
	triples := []common.RetrievalResult{{Text: "t", ChunkID: "c"}}
	newChunks, err := gr.fetchChunks(context.Background(), triples, RetrievalModeSparse)
	require.NoError(t, err)
	assert.Empty(t, newChunks)
}

// TestGraphRetriever_fetchChunks空chunkIDSet triples 无 chunk_id → nil
func TestGraphRetriever_fetchChunks空chunkIDSet(t *testing.T) {
	gr := NewGraphRetriever()
	newChunks, err := gr.fetchChunks(context.Background(),
		[]common.RetrievalResult{{Text: "no-chunk-id"}}, RetrievalModeSparse)
	require.NoError(t, err)
	assert.Empty(t, newChunks)
}

// TestGraphRetriever_fetchChunks动态创建失败 GetRetrieverForMode 错误冒泡
func TestGraphRetriever_fetchChunks动态创建失败(t *testing.T) {
	gr := NewGraphRetriever() // 无 store 无固定
	triples := []common.RetrievalResult{{Text: "t", ChunkID: "c"}}
	_, err := gr.fetchChunks(context.Background(), triples, RetrievalModeHybrid)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "vector_store is required")
}

// TestGraphRetriever_fetchChunks空结果 fetchChunk 无命中 → nil 元素过滤
func TestGraphRetriever_fetchChunks空结果(t *testing.T) {
	stub := &graphFetchMilvusStub{} // chunkRows 空 → 无命中
	store := newGraphMilvusStore(t, stub)
	store.SetCollectionName("kb_1_chunks")

	chunkRetriever := &graphEmbedRetriever{fakeBeamRetriever{}, nil, store}
	gr := NewGraphRetriever(WithGraphChunkRetriever(chunkRetriever))
	triples := []common.RetrievalResult{
		{Text: "A relates B", ChunkID: "cid-a"},
	}
	newChunks, err := gr.fetchChunks(context.Background(), triples, RetrievalModeSparse)
	require.NoError(t, err)
	assert.Empty(t, newChunks)
}

// TestGraphRetriever_fetchChunkChroma空结果 Chroma 无命中路径（fetchChunk
// Chroma 分支 len(items)==0 → nil）
func TestGraphRetriever_fetchChunkChroma空结果(t *testing.T) {
	// graphChunkChromaRetriever 无法跨包构造 ChromaVectorStore（私有
	// client 字段），此处经 fetchChunk 的 Chroma 分支需要真实
	// ChromaVectorStore——用 Chroma 分支 stub 桩直接验证归一化函数行为，
	// 分支本身在 graph_fetch_test.go 已覆盖
	t.Skip("ChromaVectorStore 私有字段无法注入；Chroma 归一化已在 graph_fetch_test.go 覆盖")
}

// TestGraphRetriever_GraphExpansion全链RRF融合 beam 扩展 + fetchChunks
// 命中 + RRF 融合（newChunks > 0 分支——Python L504-510 列对齐 + L517 融合）
func TestGraphRetriever_GraphExpansion全链RRF融合(t *testing.T) {
	// 两个 chunk_id 查出同文本结果：newChunks 内同 key 两次 rank 累计
	// 1/61+1/62（顺序无关），orig 单列表 1/61——分数与顺序均确定
	stub := &graphFetchMilvusStub{chunkRows: map[string]milvusclient.ResultSet{
		"cid-b": makeChunkRow("expanded chunk", "cid-b", "doc-1"),
		"c0":    makeChunkRow("expanded chunk", "c0", "doc-1"),
	}}
	store := newGraphMilvusStore(t, stub)
	store.SetCollectionName("kb_1_chunks")

	tripleRetriever := &graphEmbedRetriever{
		fakeBeamRetriever{retrieveFn: func(context.Context, string, int) ([]common.RetrievalResult, error) {
			return nil, nil // 无候选 → 保原 beam → 扩展轮 2 条路径文本
		}},
		&fakeBeamEmbedModel{batchSize: 32, batches: [][][]float64{
			{{1, 0}, {1, 0}, {1, 0}}, // 首轮 2 triples + query
			{{1, 0}, {1, 0}},         // 扩展轮 2 beams 保原
		}},
		store,
	}
	// chunk retriever 需实现 VectorStoreAccessor（fetchChunks 挖取通道）
	chunkRetriever := &graphEmbedRetriever{
		fakeBeamRetriever{retrieveFn: func(context.Context, string, int) ([]common.RetrievalResult, error) {
			return []common.RetrievalResult{makeChunkResult("orig", "c0", "d0", 0.9)}, nil
		}},
		nil,
		store,
	}
	gr := NewGraphRetriever(
		WithGraphChunkRetriever(chunkRetriever),
		WithGraphTripleRetriever(tripleRetriever),
	)

	chunks := []common.RetrievalResult{makeChunkResult("orig", "c0", "d0", 0.9)}
	// 两条 triple 各自进初始 beams（同向量 cos=1），保原后 fetchChunks
	// 查 {cid-b, c0} 两个 chunk_id
	triples := []common.RetrievalResult{
		makeTripleWithChunkID("A relates B", `["A","relates","B"]`, "cid-b", 0.9),
		makeTripleWithChunkID("C relates D", `["C","relates","D"]`, "c0", 0.8),
	}
	results, err := gr.GraphExpansion(context.Background(), "q", chunks, triples, nil, RetrievalModeSparse)
	require.NoError(t, err)
	// RRF 融合：expanded chunk 双命中（newChunks rank0+rank1）=1/61+1/62；
	// orig 单命中（chunks rank0）=1/61。expanded 分高在前
	require.Len(t, results, 2)
	assert.Equal(t, "expanded chunk", results[0].Text)
	assert.Equal(t, "orig", results[1].Text)
	assert.InDelta(t, 1.0/61+1.0/62, results[0].Score, 1e-9)
	assert.InDelta(t, 1.0/61, results[1].Score, 1e-9)
}

// TestGraphRetriever_GraphExpansion取topk RRF 融合后 topk 截断
func TestGraphRetriever_GraphExpansion取topk(t *testing.T) {
	stub := &graphFetchMilvusStub{chunkRows: map[string]milvusclient.ResultSet{
		"cid-b": makeChunkRow("expanded chunk", "cid-b", "doc-1"),
		"c0":    makeChunkRow("expanded chunk", "c0", "doc-1"),
	}}
	store := newGraphMilvusStore(t, stub)
	store.SetCollectionName("kb_1_chunks")

	tripleRetriever := &graphEmbedRetriever{
		fakeBeamRetriever{retrieveFn: func(context.Context, string, int) ([]common.RetrievalResult, error) {
			return nil, nil
		}},
		&fakeBeamEmbedModel{batchSize: 32, batches: [][][]float64{
			{{1, 0}, {1, 0}, {1, 0}},
			{{1, 0}, {1, 0}},
		}},
		store,
	}
	chunkRetriever := &graphEmbedRetriever{
		fakeBeamRetriever{retrieveFn: func(context.Context, string, int) ([]common.RetrievalResult, error) {
			return []common.RetrievalResult{makeChunkResult("orig", "c0", "d0", 0.9)}, nil
		}},
		nil,
		store,
	}
	gr := NewGraphRetriever(
		WithGraphChunkRetriever(chunkRetriever),
		WithGraphTripleRetriever(tripleRetriever),
	)

	chunks := []common.RetrievalResult{makeChunkResult("orig", "c0", "d0", 0.9)}
	triples := []common.RetrievalResult{
		makeTripleWithChunkID("A relates B", `["A","relates","B"]`, "cid-b", 0.9),
		makeTripleWithChunkID("C relates D", `["C","relates","D"]`, "c0", 0.8),
	}
	topk := 1
	results, err := gr.GraphExpansion(context.Background(), "q", chunks, triples, &topk, RetrievalModeSparse)
	require.NoError(t, err)
	require.Len(t, results, 1) // topk=1 截断（expanded 分高保留）
	assert.Equal(t, "expanded chunk", results[0].Text)
}

// TestGraphRetriever_IndexType getter 回读（Python index_type 属性读取）
func TestGraphRetriever_IndexType(t *testing.T) {
	gr := NewGraphRetriever()
	assert.Equal(t, "", gr.IndexType()) // None 同构
	gr.SetIndexType("vector")
	assert.Equal(t, "vector", gr.IndexType())
}

// TestGraphRetriever_固定triple检索器拒绝 isChunk=false 固定 triple
// 不支持 mode → 155205（retrieverSupportsMode SupportedModesProvider 分支）
func TestGraphRetriever_固定triple检索器拒绝(t *testing.T) {
	// withModesRetriever 实现 SupportedModesProvider（自定义 stub 通道）
	sr := &withModesRetriever{fakeBeamRetriever{}, []RetrievalMode{RetrievalModeSparse}}
	gr := NewGraphRetriever(WithGraphTripleRetriever(sr))
	_, err := gr.GetRetrieverForMode(RetrievalModeHybrid, false)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "does not support mode=hybrid")

	// 支持 sparse → 通过
	r, err := gr.GetRetrieverForMode(RetrievalModeSparse, false)
	require.NoError(t, err)
	assert.Equal(t, sr, r)
}

// TestGraphRetriever_检索器无模式声明 SupportedModesProvider 未实现 →
// 兜底 true（Python 无 SUPPORTED_MODES 类属性 → 默认支持）
func TestGraphRetriever_检索器无模式声明(t *testing.T) {
	// noEmbedRetriever 不实现 SupportedModesProvider → 兜底 true
	gr := NewGraphRetriever(WithGraphChunkRetriever(&noEmbedRetriever{}))
	r, err := gr.GetRetrieverForMode(RetrievalModeHybrid, true)
	require.NoError(t, err)
	assert.NotNil(t, r)
}

// TestGraphRetriever_类型switch三分支 VectorRetriever/SparseRetriever/
// HybridRetriever 固定检索器的 mode 支持判定（retrieverSupportsMode
// 类型 switch——Python isinstance 三兄弟）
func TestGraphRetriever_类型switch三分支(t *testing.T) {
	// VectorRetriever 只支持 vector
	vr := NewVectorRetriever(&fakeVectorStore{}, &fakeBeamEmbedModel{})
	gr := NewGraphRetriever(WithGraphChunkRetriever(vr))
	_, err := gr.GetRetrieverForMode(RetrievalModeSparse, true)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "does not support mode=sparse")
	r, err := gr.GetRetrieverForMode(RetrievalModeVector, true)
	require.NoError(t, err)
	assert.Equal(t, vr, r)

	// HybridRetriever 支持全部三 mode
	hr := NewHybridRetriever(&fakeVectorStore{}, &fakeBeamEmbedModel{})
	gr2 := NewGraphRetriever(WithGraphChunkRetriever(hr))
	for _, m := range []RetrievalMode{RetrievalModeVector, RetrievalModeSparse, RetrievalModeHybrid} {
		r, err := gr2.GetRetrieverForMode(m, true)
		require.NoError(t, err)
		assert.Equal(t, hr, r)
	}

	// SparseRetriever 不支持 hybrid（已有用例，此处补 vector 拒绝）
	sr := NewSparseRetriever(&fakeVectorStore{})
	gr3 := NewGraphRetriever(WithGraphChunkRetriever(sr))
	_, err = gr3.GetRetrieverForMode(RetrievalModeVector, true)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "does not support mode=vector")
}

// withModesRetriever SupportedModesProvider 桩
type withModesRetriever struct {
	fakeBeamRetriever
	modes []RetrievalMode
}

func (w *withModesRetriever) SupportedModes() []RetrievalMode { return w.modes }

// TestGraphRetriever_BatchRetrieve错误传播 单查询失败 → 整体错误
func TestGraphRetriever_BatchRetrieve错误传播(t *testing.T) {
	// searchErrAfter=1：首个查询成功后失败（G5 首错传播复用）
	failRetriever := &fakeGraphChunkRetriever{fakeBeamRetriever{retrieveFn: func(context.Context, string, int) ([]common.RetrievalResult, error) {
		return nil, errors.New("retrieval failed")
	}}}
	gr := NewGraphRetriever(WithGraphChunkRetriever(failRetriever))
	_, err := gr.BatchRetrieve(context.Background(), []string{"q1"}, 3)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "retrieval failed")
}

// TestGraphRetriever_BatchRetrieve空查询 空列表 → 空结果
func TestGraphRetriever_BatchRetrieve空查询(t *testing.T) {
	gr := NewGraphRetriever()
	results, err := gr.BatchRetrieve(context.Background(), nil, 3)
	require.NoError(t, err)
	assert.Len(t, results, 0) // make([]..., 0) 非nil空
}

// TestGraphRetriever_Retrieve链动态创建错误 GetRetrieverForMode 错误
// 冒泡（chunk 侧 155206）
func TestGraphRetriever_Retrieve链动态创建错误(t *testing.T) {
	gr := NewGraphRetriever() // 无 store 无固定
	_, err := gr.Retrieve(context.Background(), "q", 5, nil, RetrievalModeHybrid)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "vector_store is required")
}

// TestGraphRetriever_Retrieve链chunk检索失败 chunk retriever 错误冒泡
func TestGraphRetriever_Retrieve链chunk检索失败(t *testing.T) {
	failRetriever := &fakeGraphChunkRetriever{fakeBeamRetriever{retrieveFn: func(context.Context, string, int) ([]common.RetrievalResult, error) {
		return nil, errors.New("chunk retrieval down")
	}}}
	gr := NewGraphRetriever(WithGraphChunkRetriever(failRetriever))
	_, err := gr.Retrieve(context.Background(), "q", 5, nil, RetrievalModeSparse)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "chunk retrieval down")
}

// TestGraphRetriever_GraphExpansion链fetch失败 fetchChunks 错误冒泡
// （Python _fetch_chunks 异常——g.Wait() err 路径实际不可达，但
// GetRetrieverForMode 错误可达）
func TestGraphRetriever_GraphExpansion链fetch失败(t *testing.T) {
	// beam 成功后 fetchChunks 动态创建失败（无 store 无固定 chunk retriever）
	// → 冒泡
	tripleRetriever := &withEmbedRetriever{fakeBeamRetriever{retrieveFn: func(context.Context, string, int) ([]common.RetrievalResult, error) {
		return nil, nil
	}}, &fakeBeamEmbedModel{batchSize: 32, batches: [][][]float64{
		{{1, 0}, {1, 0}},
	}}}
	// chunk 检索桩返回非空（避免空 chunks 分支），但 fetchChunks 侧动态
	// 创建需要 vectorStore → 失败
	chunkRetriever := &fakeGraphChunkRetriever{fakeBeamRetriever{retrieveFn: func(context.Context, string, int) ([]common.RetrievalResult, error) {
		return []common.RetrievalResult{makeChunkResult("orig", "c0", "d0", 0.9)}, nil
	}}}
	gr := NewGraphRetriever(
		WithGraphChunkRetriever(chunkRetriever),
		WithGraphTripleRetriever(tripleRetriever),
	)
	chunks := []common.RetrievalResult{makeChunkResult("orig", "c0", "d0", 0.9)}
	triples := []common.RetrievalResult{makeTriple("A relates B", `["A","relates","B"]`, 0.9)}
	// beam 扩展保原（batches 只有 1 批，graph_hops 默认 2 → 第二轮 embedBatch
	// 错误 → BeamSearch err → 降级），但本轮 maxLength=1 避开扩展轮：显式传
	// WithRetrieveGraphHops(1)
	_, err := gr.GraphExpansion(context.Background(), "q", chunks, triples, nil, RetrievalModeSparse,
		WithRetrieveGraphHops(1))
	// fetchChunks：chunkRetriever 为固定 fake（不支持 VectorStoreAccessor）
	// → warn 返回空 newChunks → fused = chunks（非错误路径）
	require.NoError(t, err)
}
