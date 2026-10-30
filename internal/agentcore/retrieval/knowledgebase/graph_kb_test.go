package knowledgebase

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/embedding"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/reranker"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/indexing/indexer"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/indexing/processor/extractor"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/indexing/processor/parser"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/retriever"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 测试桩 ────────────────────────────

// graphBuildCall 一次 BuildIndex 调用快照。
type graphBuildCall struct {
	// cfg 构建配置
	cfg common.IndexConfig
	// chunks 参与构建的分块
	chunks []common.TextChunk
}

// graphIndexer 多次构建捕获 + 可编程返回值（区分 chunk / triple 两次构建）。
type graphIndexer struct {
	fakeAttrIndexer
	mu       sync.Mutex
	calls    []graphBuildCall
	buildOK  bool
	buildErr error
	deleteOK bool
	delCalls []string
	info     map[string]any
}

func (g *graphIndexer) BuildIndex(_ context.Context, chunks []common.TextChunk, config common.IndexConfig,
	_ embedding.BaseEmbedding, _ ...indexer.IndexOption) (bool, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.calls = append(g.calls, graphBuildCall{cfg: config, chunks: chunks})
	return g.buildOK, g.buildErr
}

func (g *graphIndexer) DeleteIndex(_ context.Context, docID string, _ string) (bool, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.delCalls = append(g.delCalls, docID)
	return g.deleteOK, nil
}

func (g *graphIndexer) GetIndexInfo(_ context.Context, _ string) (map[string]any, error) {
	return g.info, nil
}

// failSecondBuildIndexer 第一次构建成功、第二次返回 false（triple 索引失败路径）。
type failSecondBuildIndexer struct {
	fakeAttrIndexer
	n int
}

func (f *failSecondBuildIndexer) BuildIndex(_ context.Context, _ []common.TextChunk, _ common.IndexConfig,
	_ embedding.BaseEmbedding, _ ...indexer.IndexOption) (bool, error) {
	f.n++
	if f.n == 1 {
		return true, nil
	}
	return false, nil
}

// stubExtractor 三元组抽取桩（可编程返回）。
type stubExtractor struct {
	triples []common.Triple
	err     error
	calls   int
	gotDocs []common.TextChunk
}

func (s *stubExtractor) Extract(_ context.Context, chunks []common.TextChunk,
	_ ...extractor.ExtractOption) ([]common.Triple, error) {
	s.calls++
	s.gotDocs = chunks
	return s.triples, s.err
}

func (s *stubExtractor) Process(ctx context.Context, chunks []common.TextChunk,
	opts ...extractor.ExtractOption) ([]common.Triple, error) {
	return s.Extract(ctx, chunks, opts...)
}

// stubParser 最小 Parser 桩（仅 Parse 生效，用于验证基座模板方法继承）。
type stubParser struct {
	docs  []reranker.Document
	calls int
}

func (p *stubParser) Parse(_ context.Context, _ string, _ ...parser.ParseOption) ([]reranker.Document, error) {
	p.calls++
	return p.docs, nil
}

func (p *stubParser) LazyParse(_ context.Context, _ string, fn func(reranker.Document) error,
	_ ...parser.ParseOption) error {
	for _, d := range p.docs {
		if err := fn(d); err != nil {
			return err
		}
	}
	return nil
}

func (p *stubParser) Process(ctx context.Context, doc string,
	opts ...parser.ParseOption) ([]reranker.Document, error) {
	return p.Parse(ctx, doc, opts...)
}

func (p *stubParser) Supports(string) bool { return true }

func (p *stubParser) ParseContent(context.Context, string, *llm.Model) (string, error) {
	return "", nil
}

// graphFakeRetriever 图检索器桩（捕获 Retrieve 参数 + Close 计数）。
type graphFakeRetriever struct {
	results []common.RetrievalResult
	err     error
	calls   int
	gotTopK int
	gotMode retriever.RetrievalMode
	gotOpts retriever.RetrieveOptions
	closed  int
}

func (f *graphFakeRetriever) Retrieve(_ context.Context, _ string, topK int, _ *float64,
	mode retriever.RetrievalMode, opts ...retriever.RetrieveOption) ([]common.RetrievalResult, error) {
	f.calls++
	f.gotTopK = topK
	f.gotMode = mode
	f.gotOpts = retriever.NewRetrieveOptions(opts...)
	return f.results, f.err
}

func (f *graphFakeRetriever) BatchRetrieve(context.Context, []string, int,
	...retriever.RetrieveOption) ([][]common.RetrievalResult, error) {
	return nil, nil
}

func (f *graphFakeRetriever) RetrieveSearchResults(context.Context, string, int,
	retriever.RetrievalMode, ...retriever.RetrieveOption) ([]common.SearchResult, error) {
	return nil, nil
}

func (f *graphFakeRetriever) Close() error { f.closed++; return nil }

// ──────────────────────────── 构造 ────────────────────────────

// TestNewGraphKnowledgeBase_构造 基座构造与校验继承 + 三检索器字段初始为 nil。
func TestNewGraphKnowledgeBase_构造(t *testing.T) {
	vs, im := makeCompatiblePair()
	kb, err := NewGraphKnowledgeBase(common.KnowledgeBaseConfig{KBID: "test_kb", UseGraph: true},
		nil, nil, WithVectorStore(vs), WithIndexManager(im))
	require.NoError(t, err)
	assert.Equal(t, "test_kb", kb.Config().KBID)
	assert.Nil(t, kb.graphRetriever)
	assert.Nil(t, kb.chunkRetriever)
	assert.Nil(t, kb.tripleRetriever)
}

// ──────────────────────────── AddDocuments ────────────────────────────

// TestGraphKB_AddDocuments_图索引 对齐 test_add_documents_with_graph：
// 建 chunk + triple 两个索引，抽一次三元组，triple 索引 use_caption 硬编码 true。
func TestGraphKB_AddDocuments_图索引(t *testing.T) {
	im := &graphIndexer{buildOK: true}
	ch := &countingChunker{chunks: []common.TextChunk{
		{ID: "chunk_1", Text: "Test chunk 1", DocID: "doc_1"},
	}}
	ex := &stubExtractor{triples: []common.Triple{{
		Subject: "Alice", Predicate: "knows", Object: "Bob",
		Metadata: map[string]any{"doc_id": "doc_1", "chunk_id": "chunk_1"},
	}}}
	kb, err := NewGraphKnowledgeBase(
		common.KnowledgeBaseConfig{KBID: "test_kb", IndexType: common.IndexTypeVector,
			UseGraph: true, UseCaptionForImages: false},
		nil, nil, WithChunker(ch), WithExtractor(ex), WithIndexManager(im))
	require.NoError(t, err)

	ids, err := kb.AddDocuments(context.Background(), []reranker.Document{{ID: "doc_1", Text: "Test document"}})
	require.NoError(t, err)
	assert.Equal(t, []string{"doc_1"}, ids)
	assert.Equal(t, 1, ex.calls)
	require.Len(t, im.calls, 2)

	// chunk 索引：名称 / index_type / use_caption 取配置
	assert.Equal(t, "kb_test_kb_chunks", im.calls[0].cfg.IndexName)
	assert.Equal(t, common.IndexTypeVector, im.calls[0].cfg.IndexType)
	assert.False(t, im.calls[0].cfg.UseCaptionForImages)

	// triple 索引：名称 / index_type / use_caption 硬编码 true（不对称照复刻）
	assert.Equal(t, "kb_test_kb_triples", im.calls[1].cfg.IndexName)
	assert.Equal(t, common.IndexTypeVector, im.calls[1].cfg.IndexType)
	assert.True(t, im.calls[1].cfg.UseCaptionForImages)

	// triple chunk 字段逐项
	require.Len(t, im.calls[1].chunks, 1)
	tc := im.calls[1].chunks[0]
	assert.NotEmpty(t, tc.ID)
	assert.Equal(t, "Alice knows Bob", tc.Text)
	assert.Equal(t, "doc_1", tc.DocID)
	assert.Equal(t, 0, tc.Metadata["chunk_index"])
	assert.Equal(t, "chunk_1", tc.Metadata["chunk_id"])
	assert.Equal(t, "doc_1", tc.Metadata["doc_id"])
	tripleStr, ok := tc.Metadata["triple"].(string)
	require.True(t, ok)
	var parsed []string
	require.NoError(t, json.Unmarshal([]byte(tripleStr), &parsed))
	assert.Equal(t, []string{"Alice", "knows", "Bob"}, parsed)
}

// TestGraphKB_AddDocuments_非图 对齐 test_add_documents_without_graph：仅建 chunk 索引。
func TestGraphKB_AddDocuments_非图(t *testing.T) {
	im := &graphIndexer{buildOK: true}
	ch := &countingChunker{chunks: []common.TextChunk{{ID: "c1", Text: "c1", DocID: "doc_1"}}}
	ex := &stubExtractor{}
	kb, err := NewGraphKnowledgeBase(common.KnowledgeBaseConfig{KBID: "test_kb", UseGraph: false},
		nil, nil, WithChunker(ch), WithExtractor(ex), WithIndexManager(im))
	require.NoError(t, err)
	_, err = kb.AddDocuments(context.Background(), []reranker.Document{{ID: "doc_1", Text: "d"}})
	require.NoError(t, err)
	assert.Len(t, im.calls, 1)
	assert.Equal(t, 0, ex.calls)
}

// TestGraphKB_AddDocuments_空三元组 抽取结果为空时不建 triple 索引。
func TestGraphKB_AddDocuments_空三元组(t *testing.T) {
	im := &graphIndexer{buildOK: true}
	ch := &countingChunker{chunks: []common.TextChunk{{ID: "c1", Text: "c1", DocID: "doc_1"}}}
	kb, err := NewGraphKnowledgeBase(common.KnowledgeBaseConfig{KBID: "test_kb", UseGraph: true},
		nil, nil, WithChunker(ch), WithExtractor(&stubExtractor{}), WithIndexManager(im))
	require.NoError(t, err)
	_, err = kb.AddDocuments(context.Background(), []reranker.Document{{ID: "doc_1", Text: "d"}})
	require.NoError(t, err)
	assert.Len(t, im.calls, 1)
}

// TestGraphKB_AddDocuments_chunk索引失败 chunk 构建返回 false → 155505。
func TestGraphKB_AddDocuments_chunk索引失败(t *testing.T) {
	im := &graphIndexer{buildOK: false}
	kb, err := NewGraphKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb", UseGraph: true},
		nil, nil, WithChunker(&countingChunker{}), WithIndexManager(im))
	require.NoError(t, err)
	_, err = kb.AddDocuments(context.Background(), []reranker.Document{{ID: "d", Text: "d"}})
	var be *exception.BaseError
	require.True(t, errors.As(err, &be))
	assert.Equal(t, 155505, be.Code())
	assert.Contains(t, be.Message(), "Failed to build chunk index")
}

// TestGraphKB_AddDocuments_triple索引失败 triple 构建返回 false → 155506。
func TestGraphKB_AddDocuments_triple索引失败(t *testing.T) {
	im := &failSecondBuildIndexer{}
	ch := &countingChunker{chunks: []common.TextChunk{{ID: "c1", Text: "c1", DocID: "d"}}}
	ex := &stubExtractor{triples: []common.Triple{{Subject: "a", Predicate: "b", Object: "c"}}}
	kb, err := NewGraphKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb", UseGraph: true},
		nil, nil, WithChunker(ch), WithExtractor(ex), WithIndexManager(im))
	require.NoError(t, err)
	_, err = kb.AddDocuments(context.Background(), []reranker.Document{{ID: "d", Text: "d"}})
	var be *exception.BaseError
	require.True(t, errors.As(err, &be))
	assert.Equal(t, 155506, be.Code())
	assert.Contains(t, be.Message(), "Failed to build triple index")
}

// TestGraphKB_AddDocuments_缺组件 155501 chunker 缺 / 155502 index_manager 缺。
func TestGraphKB_AddDocuments_缺组件(t *testing.T) {
	kb, err := NewGraphKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb"}, nil, nil)
	require.NoError(t, err)
	_, err = kb.AddDocuments(context.Background(), []reranker.Document{{Text: "d"}})
	var be *exception.BaseError
	require.True(t, errors.As(err, &be))
	assert.Equal(t, 155501, be.Code())

	kb2, err := NewGraphKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb"}, nil, nil,
		WithChunker(&countingChunker{}))
	require.NoError(t, err)
	_, err = kb2.AddDocuments(context.Background(), []reranker.Document{{Text: "d"}})
	require.True(t, errors.As(err, &be))
	assert.Equal(t, 155502, be.Code())
}

// TestGraphKB_ParseFiles 基座模板方法继承（对齐 test_parse_files_success）。
func TestGraphKB_ParseFiles(t *testing.T) {
	p := &stubParser{docs: []reranker.Document{{ID: "doc_1", Text: "Test document 1"}}}
	kb, err := NewGraphKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb"}, nil, nil, WithParser(p))
	require.NoError(t, err)
	docs, err := kb.ParseFiles(context.Background(), []string{"test1.txt"})
	require.NoError(t, err)
	assert.Len(t, docs, 1)
	assert.Equal(t, 1, p.calls)
}

// ──────────────────────────── Retrieve ────────────────────────────

// TestGraphKB_Retrieve_图路径 图路径分派到注入的检索器，参数透传
// （对齐 test_retrieve_with_graph：patch GraphRetriever 后断言被调用）。
func TestGraphKB_Retrieve_图路径(t *testing.T) {
	vs, _ := makeCompatiblePair()
	fake := &graphFakeRetriever{results: []common.RetrievalResult{{Text: "Test result", Score: 0.95}}}
	kb, err := NewGraphKnowledgeBase(common.KnowledgeBaseConfig{KBID: "test_kb", UseGraph: true},
		nil, nil, WithVectorStore(vs))
	require.NoError(t, err)
	kb.SetGraphRetriever(fake)

	useGraph := true
	results, err := kb.Retrieve(context.Background(), "test query",
		&common.RetrievalConfig{TopK: 5, UseGraph: &useGraph})
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, 1, fake.calls)
	assert.Equal(t, 5, fake.gotTopK)
	assert.Equal(t, retriever.RetrievalModeHybrid, fake.gotMode) // 零值 index_type → hybrid
	// graph_expansion 显式传递（false 但指针非 nil，对齐 Python 传 bool）
	require.NotNil(t, fake.gotOpts.GraphExpansion)
	assert.False(t, *fake.gotOpts.GraphExpansion)
}

// TestGraphKB_Retrieve_懒创建注入indexType 不注入时真实构造 GraphRetriever
// 并注入 index_type（SetIndexType 首个生产调用）。
func TestGraphKB_Retrieve_懒创建注入indexType(t *testing.T) {
	vs, _ := makeCompatiblePair()
	kb, err := NewGraphKnowledgeBase(
		common.KnowledgeBaseConfig{KBID: "test_kb", IndexType: common.IndexTypeBM25, UseGraph: true},
		nil, nil, WithVectorStore(vs))
	require.NoError(t, err)

	got, err := kb.ensureGraphRetriever()
	require.NoError(t, err)
	gret, ok := got.(*retriever.GraphRetriever)
	require.True(t, ok)
	assert.Equal(t, "bm25", gret.IndexType())

	again, err := kb.ensureGraphRetriever()
	require.NoError(t, err)
	assert.Same(t, got, again)
}

// TestGraphKB_Retrieve_并发只建一次 并发 ensureGraphRetriever 返回同一实例（-race）。
func TestGraphKB_Retrieve_并发只建一次(t *testing.T) {
	vs, _ := makeCompatiblePair()
	kb, err := NewGraphKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb", UseGraph: true},
		nil, nil, WithVectorStore(vs))
	require.NoError(t, err)

	const n = 8
	var wg sync.WaitGroup
	got := make([]retriever.Retriever, n)
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			got[idx], errs[idx] = kb.ensureGraphRetriever()
		}(i)
	}
	wg.Wait()
	for i := 0; i < n; i++ {
		require.NoError(t, errs[i])
	}
	for i := 1; i < n; i++ {
		assert.Same(t, got[0], got[i])
	}
}

// TestGraphKB_Retrieve_无VectorStore 图路径建检索器时缺 store → 155503。
func TestGraphKB_Retrieve_无VectorStore(t *testing.T) {
	kb, err := NewGraphKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb", UseGraph: true}, nil, nil)
	require.NoError(t, err)
	_, err = kb.Retrieve(context.Background(), "q", &common.RetrievalConfig{TopK: 5})
	var be *exception.BaseError
	require.True(t, errors.As(err, &be))
	assert.Equal(t, 155503, be.Code())
	assert.Contains(t, be.Message(), "vector_store is required for graph retrieval")
}

// TestGraphKB_Retrieve_回落SimpleKB use_graph 全 false → 回落 SimpleKB
// （对齐 test_retrieve_without_graph_falls_back_to_simple）。bm25 索引避开
// embed_model 依赖。
func TestGraphKB_Retrieve_回落SimpleKB(t *testing.T) {
	kb, err := NewGraphKnowledgeBase(
		common.KnowledgeBaseConfig{KBID: "kb", IndexType: common.IndexTypeBM25, UseGraph: false},
		nil, nil, WithVectorStore(&fakeAttrStore{}), WithLLMClient(&stubKBLlm{}))
	require.NoError(t, err)

	useGraph := false
	results, err := kb.Retrieve(context.Background(), "q",
		&common.RetrievalConfig{TopK: 5, UseGraph: &useGraph})
	require.NoError(t, err)
	assert.Empty(t, results)
	assert.Nil(t, kb.graphRetriever) // 未走图路径
}

// TestGraphKB_Retrieve_agentic无LLM Agentic=true 且 llmClient 为 nil →
// NewAgenticRetriever 报 155209（证明 agentic 分支已接线，对齐
// test_retrieve_with_agentic 的构造参数断言）。
func TestGraphKB_Retrieve_agentic无LLM(t *testing.T) {
	vs, _ := makeCompatiblePair()
	kb, err := NewGraphKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb", UseGraph: true},
		nil, nil, WithVectorStore(vs))
	require.NoError(t, err)
	kb.SetGraphRetriever(&graphFakeRetriever{})

	_, err = kb.Retrieve(context.Background(), "q", &common.RetrievalConfig{TopK: 5, Agentic: true})
	var be *exception.BaseError
	require.True(t, errors.As(err, &be))
	assert.Equal(t, 155209, be.Code())
}

// TestGraphKB_Retrieve_agentic未知kwargs Agentic=true + 未知 kwargs → 155510
// （13.27 §13.5 反转后的统一规则，GraphKB 复用）。
func TestGraphKB_Retrieve_agentic未知kwargs(t *testing.T) {
	vs, _ := makeCompatiblePair()
	kb, err := NewGraphKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb", UseGraph: true},
		nil, nil, WithVectorStore(vs), WithLLMClient(&stubKBLlm{}))
	require.NoError(t, err)
	kb.SetGraphRetriever(&graphFakeRetriever{})

	_, err = kb.Retrieve(context.Background(), "q",
		&common.RetrievalConfig{TopK: 5, Agentic: true}, WithKBExtra("bogus", 1))
	var be *exception.BaseError
	require.True(t, errors.As(err, &be))
	assert.Equal(t, 155510, be.Code())
}

// TestGraphKB_SetGraphRetriever_复位 传 nil 复位后重新懒创建。
func TestGraphKB_SetGraphRetriever_复位(t *testing.T) {
	vs, _ := makeCompatiblePair()
	kb, err := NewGraphKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb", UseGraph: true},
		nil, nil, WithVectorStore(vs))
	require.NoError(t, err)
	first, err := kb.ensureGraphRetriever()
	require.NoError(t, err)
	kb.SetGraphRetriever(nil)
	second, err := kb.ensureGraphRetriever()
	require.NoError(t, err)
	assert.NotSame(t, first, second)
}

// ──────────────────────────── Delete / Update / Statistics / Close ────────────────────────────

// secondDeleteFalseIndexer 首次删除 true、后续 false（chunk 成功 / triple 失败）。
type secondDeleteFalseIndexer struct {
	fakeAttrIndexer
	n int
}

func (s *secondDeleteFalseIndexer) DeleteIndex(_ context.Context, _ string, _ string) (bool, error) {
	s.n++
	if s.n == 1 {
		return true, nil
	}
	return false, nil
}

// TestGraphKB_DeleteDocuments_图 对齐 test_delete_documents_with_graph：
// chunk + triple 各删一次，返回 true。
func TestGraphKB_DeleteDocuments_图(t *testing.T) {
	im := &graphIndexer{deleteOK: true}
	kb, err := NewGraphKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb", UseGraph: true}, nil, nil,
		WithIndexManager(im))
	require.NoError(t, err)
	ok, err := kb.DeleteDocuments(context.Background(), []string{"doc_1"})
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Len(t, im.delCalls, 2) // chunk + triple 各一次
}

// TestGraphKB_DeleteDocuments_非图 对齐 test_delete_documents_without_graph：仅一次。
func TestGraphKB_DeleteDocuments_非图(t *testing.T) {
	im := &graphIndexer{deleteOK: true}
	kb, err := NewGraphKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb", UseGraph: false}, nil, nil,
		WithIndexManager(im))
	require.NoError(t, err)
	ok, err := kb.DeleteDocuments(context.Background(), []string{"doc_1"})
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Len(t, im.delCalls, 1)
}

// TestGraphKB_DeleteDocuments_triple失败仍返回true triple 删除返回 false 只 Warn，
// 不影响总结果（Python 真实行为）。
func TestGraphKB_DeleteDocuments_triple失败仍返回true(t *testing.T) {
	im := &secondDeleteFalseIndexer{}
	kb, err := NewGraphKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb", UseGraph: true}, nil, nil,
		WithIndexManager(im))
	require.NoError(t, err)
	ok, err := kb.DeleteDocuments(context.Background(), []string{"doc_1"})
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, 2, im.n)
}

// TestGraphKB_DeleteDocuments_缺索引管理器 155502。
func TestGraphKB_DeleteDocuments_缺索引管理器(t *testing.T) {
	kb, err := NewGraphKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb"}, nil, nil)
	require.NoError(t, err)
	_, err = kb.DeleteDocuments(context.Background(), []string{"doc_1"})
	var be *exception.BaseError
	require.True(t, errors.As(err, &be))
	assert.Equal(t, 155502, be.Code())
}

// TestGraphKB_UpdateDocuments 先删后加（对齐 test_update_documents：
// delete_index 调用 ≥1 且 build_index 调用 ≥1）。
func TestGraphKB_UpdateDocuments(t *testing.T) {
	im := &graphIndexer{buildOK: true, deleteOK: true}
	ch := &countingChunker{chunks: []common.TextChunk{{ID: "c1", Text: "c1", DocID: "doc_1"}}}
	kb, err := NewGraphKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb", UseGraph: false}, nil, nil,
		WithChunker(ch), WithIndexManager(im))
	require.NoError(t, err)
	ids, err := kb.UpdateDocuments(context.Background(),
		[]reranker.Document{{ID: "doc_1", Text: "Updated document"}})
	require.NoError(t, err)
	assert.Equal(t, []string{"doc_1"}, ids)
	assert.NotEmpty(t, im.delCalls)
	assert.NotEmpty(t, im.calls)
}

// TestGraphKB_GetStatistics_图 对齐 test_get_statistics_with_graph：11 键齐全，
// triple_index_info 恒存在。
func TestGraphKB_GetStatistics_图(t *testing.T) {
	im := &graphIndexer{info: map[string]any{"count": 10}}
	kb, err := NewGraphKnowledgeBase(common.KnowledgeBaseConfig{KBID: "test_kb", UseGraph: true},
		nil, nil, WithIndexManager(im))
	require.NoError(t, err)
	stats, err := kb.GetStatistics(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "test_kb", stats["kb_id"])
	assert.Equal(t, true, stats["use_graph"])
	assert.Contains(t, stats, "chunk_index_info")
	assert.Contains(t, stats, "triple_index_info")
	assert.Contains(t, stats, "has_graph_retriever")
	assert.Len(t, stats, 11)
}

// TestGraphKB_GetStatistics_无IndexManager 对齐 test_get_statistics_without_index_manager。
func TestGraphKB_GetStatistics_无IndexManager(t *testing.T) {
	kb, err := NewGraphKnowledgeBase(common.KnowledgeBaseConfig{KBID: "test_kb"}, nil, nil)
	require.NoError(t, err)
	stats, err := kb.GetStatistics(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "test_kb", stats["kb_id"])
	assert.Equal(t, false, stats["index_exists"])
}

// TestGraphKB_Close 对齐 test_close：graph / chunk / triple 三检索器均被关闭。
func TestGraphKB_Close(t *testing.T) {
	graph := &graphFakeRetriever{}
	chunk := &graphFakeRetriever{}
	triple := &graphFakeRetriever{}
	kb, err := NewGraphKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb"}, chunk, triple)
	require.NoError(t, err)
	kb.SetGraphRetriever(graph)

	require.NoError(t, kb.Close())
	assert.Equal(t, 1, graph.closed)
	assert.Equal(t, 1, chunk.closed)
	assert.Equal(t, 1, triple.closed)
}
