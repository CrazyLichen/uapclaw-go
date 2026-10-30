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
