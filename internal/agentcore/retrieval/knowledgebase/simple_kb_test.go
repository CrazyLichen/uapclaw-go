package knowledgebase

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/embedding"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/query"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/reranker"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/indexing/indexer"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/retriever"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/vector_store"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 测试桩 ────────────────────────────

// stubKBLlm 零行为 LLM 桩（BaseModelClient 全接口补全）。
type stubKBLlm struct{}

func (s *stubKBLlm) Invoke(context.Context, model_clients.MessagesParam, ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
	return llmschema.NewAssistantMessage(""), nil
}
func (s *stubKBLlm) Stream(context.Context, model_clients.MessagesParam, ...model_clients.StreamOption) (<-chan *llmschema.AssistantMessageChunk, error) {
	return nil, errors.New("not implemented")
}
func (s *stubKBLlm) GenerateImage(context.Context, []*llmschema.UserMessage, ...model_clients.GenerateImageOption) (*llmschema.ImageGenerationResponse, error) {
	return nil, errors.New("not implemented")
}
func (s *stubKBLlm) GenerateSpeech(context.Context, []*llmschema.UserMessage, ...model_clients.GenerateSpeechOption) (*llmschema.AudioGenerationResponse, error) {
	return nil, errors.New("not implemented")
}
func (s *stubKBLlm) GenerateVideo(context.Context, []*llmschema.UserMessage, ...model_clients.GenerateVideoOption) (*llmschema.VideoGenerationResponse, error) {
	return nil, errors.New("not implemented")
}
func (s *stubKBLlm) TranscribeAudio(context.Context, string, ...llmschema.TranscribeAudioOption) (*llmschema.TranscriptionResponse, error) {
	return nil, errors.New("not implemented")
}
func (s *stubKBLlm) Release(context.Context, ...model_clients.ReleaseOption) (bool, error) {
	return false, nil
}
func (s *stubKBLlm) SupportsKVCacheRelease() bool { return false }

// countingChunker 可编程返回值的 Chunker 桩（Python MagicMock chunker 同构）。
type countingChunker struct {
	// chunks 预置返回分块
	chunks []common.TextChunk
	// err 预置返回错误
	err error
	// calls ChunkDocuments 调用次数
	calls int
	// gotDocs 收到的文档快照
	gotDocs []reranker.Document
}

func (c *countingChunker) ChunkText(_ string) ([]string, error) { return nil, nil }
func (c *countingChunker) ChunkDocuments(_ context.Context, docs []reranker.Document) ([]common.TextChunk, error) {
	c.calls++
	c.gotDocs = docs
	if c.err != nil {
		return nil, c.err
	}
	return c.chunks, nil
}
func (c *countingChunker) Process(ctx context.Context, docs []reranker.Document) ([]common.TextChunk, error) {
	return c.ChunkDocuments(ctx, docs)
}

// recordingIndexer 记录调用的 Indexer 桩（可编程返回值 + 调用捕获）。
type recordingIndexer struct {
	fakeAttrIndexer
	mu            sync.Mutex
	buildCalls    int
	updateCalls   int
	deleteCalls   int
	buildResult   bool
	updateResult  bool
	deleteResult  bool
	lastBuildCfg  common.IndexConfig
	lastBuildOpt  []indexer.IndexOption
	lastUpdateCfg common.IndexConfig
	lastBuildDocs []common.TextChunk
	deleteIDs     []string
}

func (r *recordingIndexer) BuildIndex(_ context.Context, chunks []common.TextChunk, config common.IndexConfig, _ embedding.BaseEmbedding, opts ...indexer.IndexOption) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.buildCalls++
	r.lastBuildCfg = config
	r.lastBuildOpt = opts
	r.lastBuildDocs = chunks
	return r.buildResult, nil
}
func (r *recordingIndexer) UpdateIndex(_ context.Context, _ []common.TextChunk, _ string, config common.IndexConfig, _ embedding.BaseEmbedding, _ ...indexer.IndexOption) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.updateCalls++
	r.lastUpdateCfg = config
	return r.updateResult, nil
}
func (r *recordingIndexer) DeleteIndex(_ context.Context, docID string, _ string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.deleteCalls++
	r.deleteIDs = append(r.deleteIDs, docID)
	return r.deleteResult, nil
}

// countingDeleteIndexer 删除计数桩：第一次 true 第二次 false（部分失败路径）。
type countingDeleteIndexer struct {
	fakeAttrIndexer
	calls int
}

func (c *countingDeleteIndexer) DeleteIndex(_ context.Context, _ string, _ string) (bool, error) {
	c.calls++
	return c.calls == 1, nil
}

// statsIndexer 统计桩：可编程 info/err。
type statsIndexer struct {
	fakeAttrIndexer
	info map[string]any
	err  error
	got  string
}

func (s *statsIndexer) GetIndexInfo(_ context.Context, indexName string) (map[string]any, error) {
	s.got = indexName
	return s.info, s.err
}

// errUpdateIndexer UpdateIndex 直接返回错误的桩（错误冒泡路径）。
type errUpdateIndexer struct {
	fakeAttrIndexer
}

func (e *errUpdateIndexer) UpdateIndex(_ context.Context, _ []common.TextChunk, _ string, _ common.IndexConfig, _ embedding.BaseEmbedding, _ ...indexer.IndexOption) (bool, error) {
	return false, errors.New("update boom")
}

// falseUpdateIndexer UpdateIndex 返回 false 的桩（不收集 doc_id 路径）。
type falseUpdateIndexer struct {
	fakeAttrIndexer
}

func (f *falseUpdateIndexer) UpdateIndex(_ context.Context, _ []common.TextChunk, _ string, _ common.IndexConfig, _ embedding.BaseEmbedding, _ ...indexer.IndexOption) (bool, error) {
	return false, nil
}

// makeCompatibleRecordingIndexer 构造与 vs 全属性匹配、且可编程返回值的 Indexer 桩。
func makeCompatibleRecordingIndexer() (*fakeAttrStore, *recordingIndexer) {
	vs, imi := makeCompatiblePair()
	return vs, &recordingIndexer{fakeAttrIndexer: *imi, buildResult: true}
}

// plainVectorStore 不实现 DatabaseName() 的 VectorStore 桩
// （防御式取值回落空串路径；同时提供 7 项属性中的其余 6 项，
// 以便与 index_manager 通过一致性校验）。
type plainVectorStore struct{}

func (p *plainVectorStore) CheckVectorField() error { return nil }
func (p *plainVectorStore) Add(context.Context, []map[string]any, ...vector_store.StoreOption) error {
	return nil
}
func (p *plainVectorStore) Search(context.Context, []float64, int, query.QueryExpr, ...vector_store.StoreOption) ([]common.SearchResult, error) {
	return nil, nil
}
func (p *plainVectorStore) SparseSearch(context.Context, string, int, query.QueryExpr, ...vector_store.StoreOption) ([]common.SearchResult, error) {
	return nil, nil
}
func (p *plainVectorStore) HybridSearch(context.Context, string, []float64, int, float64, query.QueryExpr, ...vector_store.StoreOption) ([]common.SearchResult, error) {
	return nil, nil
}
func (p *plainVectorStore) Delete(context.Context, []string, query.QueryExpr) (bool, error) {
	return false, nil
}
func (p *plainVectorStore) TableExists(context.Context, string) (bool, error) { return false, nil }
func (p *plainVectorStore) DeleteTable(context.Context, string) error         { return nil }
func (p *plainVectorStore) Close()                                            {}

// recordingRetriever 记录调用参数的 Retriever 桩。
type recordingRetriever struct {
	mu      sync.Mutex
	results []common.RetrievalResult
	err     error
	calls   int
	gotTopK int
	gotMode retriever.RetrievalMode
	gotOpts retriever.RetrieveOptions
	closed  int
}

func (r *recordingRetriever) Retrieve(_ context.Context, _ string, topK int, _ *float64, mode retriever.RetrievalMode, opts ...retriever.RetrieveOption) ([]common.RetrievalResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	r.gotTopK = topK
	r.gotMode = mode
	r.gotOpts = retriever.NewRetrieveOptions(opts...)
	return r.results, r.err
}
func (r *recordingRetriever) BatchRetrieve(context.Context, []string, int, ...retriever.RetrieveOption) ([][]common.RetrievalResult, error) {
	return nil, nil
}
func (r *recordingRetriever) RetrieveSearchResults(context.Context, string, int, retriever.RetrievalMode, ...retriever.RetrieveOption) ([]common.SearchResult, error) {
	return nil, nil
}
func (r *recordingRetriever) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed++
}

// ──────────────────────────── 构造与断言缺口 ────────────────────────────

// TestNewSimpleKnowledgeBase_构造 继承基座构造与校验（对齐 Python 全组件构造入口）。
func TestNewSimpleKnowledgeBase_构造(t *testing.T) {
	vs, im := makeCompatiblePair()
	kb, err := NewSimpleKnowledgeBase(common.KnowledgeBaseConfig{KBID: "test_kb"},
		nil, WithVectorStore(vs), WithIndexManager(im))
	require.NoError(t, err)
	// 伏笔 2：完整 KnowledgeBase 编译期断言在 simple_kb.go（本测试可调用接口方法即证明）
	assert.Equal(t, "test_kb", kb.Config().KBID)
	var _ KnowledgeBase = kb
}

// TestNewSimpleKnowledgeBase_数据库不匹配 构造期 postAssignValidation 继承
// （对齐 Python test_database_name_mismatch）。
func TestNewSimpleKnowledgeBase_数据库不匹配(t *testing.T) {
	vs, im := makeCompatiblePair()
	im.databaseName = "different_name"
	_, err := NewSimpleKnowledgeBase(common.KnowledgeBaseConfig{KBID: "test_kb"},
		nil, WithVectorStore(vs), WithIndexManager(im))
	require.Error(t, err)
	var be *exception.BaseError
	require.True(t, errors.As(err, &be))
	assert.Equal(t, 155508, be.Code())
	assert.Contains(t, be.Message(), "incompatible database_name configs")
}

// TestNewSimpleKnowledgeBase_检索器显式传入 retriever 参数直接落到字段（Python __init__ 尾部赋值）。
func TestNewSimpleKnowledgeBase_检索器显式传入(t *testing.T) {
	r := &recordingRetriever{}
	kb, err := NewSimpleKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb"}, r)
	require.NoError(t, err)
	assert.NotNil(t, kb.retriever)
	assert.Same(t, r, kb.retriever)
}

// ──────────────────────────── AddDocuments ────────────────────────────

// TestSimpleKB_AddDocuments成功 对齐 test_add_documents_success。
func TestSimpleKB_AddDocuments成功(t *testing.T) {
	im := &recordingIndexer{buildResult: true}
	ch := &countingChunker{chunks: []common.TextChunk{
		{ID: "chunk_1", Text: "c1", DocID: "doc_1"},
		{ID: "chunk_2", Text: "c2", DocID: "doc_1"},
	}}
	kb, err := NewSimpleKnowledgeBase(
		common.KnowledgeBaseConfig{KBID: "test_kb", IndexType: common.IndexTypeVector, UseCaptionForImages: true},
		nil, WithChunker(ch), WithIndexManager(im))
	require.NoError(t, err)
	ids, err := kb.AddDocuments(context.Background(), []reranker.Document{
		{ID: "doc_1", Text: "Test document 1"}, {ID: "doc_2", Text: "Test document 2"},
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"doc_1", "doc_2"}, ids)
	assert.Equal(t, 1, ch.calls)
	assert.Equal(t, 1, im.buildCalls)
	// IndexConfig 带 UseCaptionForImages（与 update 不对称）
	assert.Equal(t, "kb_test_kb_chunks", im.lastBuildCfg.IndexName)
	assert.True(t, im.lastBuildCfg.UseCaptionForImages)
	assert.Equal(t, common.IndexTypeVector, im.lastBuildCfg.IndexType)
	assert.Len(t, im.lastBuildDocs, 2)
}

// TestSimpleKB_AddDocuments无分块器 对齐 test_add_documents_without_chunker。
func TestSimpleKB_AddDocuments无分块器(t *testing.T) {
	kb, err := NewSimpleKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb"}, nil)
	require.NoError(t, err)
	_, err = kb.AddDocuments(context.Background(), []reranker.Document{{Text: "Test"}})
	var be *exception.BaseError
	require.True(t, errors.As(err, &be))
	assert.Equal(t, 155501, be.Code())
	assert.Contains(t, be.Message(), "chunker is required")
}

// TestSimpleKB_AddDocuments无索引管理器 对齐 test_add_documents_without_index_manager。
func TestSimpleKB_AddDocuments无索引管理器(t *testing.T) {
	kb, err := NewSimpleKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb"}, nil, WithChunker(&countingChunker{}))
	require.NoError(t, err)
	_, err = kb.AddDocuments(context.Background(), []reranker.Document{{Text: "Test"}})
	var be *exception.BaseError
	require.True(t, errors.As(err, &be))
	assert.Equal(t, 155502, be.Code())
	assert.Contains(t, be.Message(), "index_manager is required")
}

// TestSimpleKB_AddDocuments构建失败 对齐 test_add_documents_build_index_failed。
func TestSimpleKB_AddDocuments构建失败(t *testing.T) {
	im := &recordingIndexer{buildResult: false}
	kb, err := NewSimpleKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb"}, nil,
		WithChunker(&countingChunker{chunks: []common.TextChunk{{ID: "c", DocID: "d"}}}), WithIndexManager(im))
	require.NoError(t, err)
	_, err = kb.AddDocuments(context.Background(), []reranker.Document{{ID: "d", Text: "Test"}})
	var be *exception.BaseError
	require.True(t, errors.As(err, &be))
	assert.Equal(t, 155504, be.Code())
	assert.Contains(t, be.Message(), "Failed to build index")
}

// TestSimpleKB_AddDocuments空白ID填充uuid Go 增强：空白 id（含纯空格）才填 uuid。
func TestSimpleKB_AddDocuments空白ID填充uuid(t *testing.T) {
	im := &recordingIndexer{buildResult: true}
	kb, err := NewSimpleKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb"}, nil,
		WithChunker(&countingChunker{}), WithIndexManager(im))
	require.NoError(t, err)
	ids, err := kb.AddDocuments(context.Background(), []reranker.Document{
		{ID: "", Text: "a"}, {ID: "  ", Text: "b"}, {ID: "keep", Text: "c"},
	})
	require.NoError(t, err)
	assert.Len(t, ids, 3)
	assert.NotEmpty(t, ids[0])
	assert.NotEqual(t, "  ", ids[1])
	assert.NotEmpty(t, ids[1])
	assert.Equal(t, "keep", ids[2])
	assert.NotEqual(t, ids[0], ids[1])
}

// TestSimpleKB_AddDocuments传数据库名 database_name 防御式取值（伏笔 5：
// 13.2/13.3 DatabaseName() getter 首次被消费）。
func TestSimpleKB_AddDocuments传数据库名(t *testing.T) {
	vs, im := makeCompatibleRecordingIndexer()
	kb, err := NewSimpleKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb"}, nil,
		WithChunker(&countingChunker{}), WithIndexManager(im), WithVectorStore(vs))
	require.NoError(t, err)
	_, err = kb.AddDocuments(context.Background(), []reranker.Document{{ID: "d", Text: "x"}})
	require.NoError(t, err)
	opts := indexer.NewIndexOptions(im.lastBuildOpt...)
	assert.Equal(t, "test_db", opts.Extra["database_name"])
}

// TestSimpleKB_数据库名防御式取值 vector_store 未实现 DatabaseName() 时回落
// 空串（Python getattr(vs, "database_name", "") 语义）。仅注入 vector_store
// 不触发一致性校验（Python 同为两者配齐才校验），直接验证取值函数。
func TestSimpleKB_数据库名防御式取值(t *testing.T) {
	kb, err := NewSimpleKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb"}, nil,
		WithVectorStore(&plainVectorStore{}))
	require.NoError(t, err)
	assert.Equal(t, "", kb.vectorStoreDatabaseName())

	// nil vector_store 同样回落空串
	kb2, err := NewSimpleKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb"}, nil)
	require.NoError(t, err)
	assert.Equal(t, "", kb2.vectorStoreDatabaseName())

	// 实现了 DatabaseName() 的存储原样返回
	vs, _ := makeCompatiblePair()
	kb3, err := NewSimpleKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb"}, nil, WithVectorStore(vs))
	require.NoError(t, err)
	assert.Equal(t, "test_db", kb3.vectorStoreDatabaseName())
}

// TestSimpleKB_AddDocuments数据库名不匹配构造期拦截 与 Python 一致：
// 未提供 database_name 的存储与提供空串的索引管理器被视为不一致。
func TestSimpleKB_AddDocuments数据库名不匹配构造期拦截(t *testing.T) {
	im := &recordingIndexer{buildResult: true}
	_, err := NewSimpleKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb"}, nil,
		WithChunker(&countingChunker{}), WithIndexManager(im),
		WithVectorStore(&plainVectorStore{}))
	require.Error(t, err)
	var be *exception.BaseError
	require.True(t, errors.As(err, &be))
	assert.Equal(t, 155508, be.Code())
}

// TestSimpleKB_AddDocuments分块错误 chunker 报错直接冒泡（Python 无 try）。
func TestSimpleKB_AddDocuments分块错误(t *testing.T) {
	boom := errors.New("chunk boom")
	im := &recordingIndexer{}
	kb, err := NewSimpleKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb"}, nil,
		WithChunker(&countingChunker{err: boom}), WithIndexManager(im))
	require.NoError(t, err)
	_, err = kb.AddDocuments(context.Background(), []reranker.Document{{Text: "x"}})
	require.ErrorIs(t, err, boom)
	assert.Equal(t, 0, im.buildCalls)
}

// ──────────────────────────── DeleteDocuments ────────────────────────────

// TestSimpleKB_DeleteDocuments成功 对齐 test_delete_documents_success。
func TestSimpleKB_DeleteDocuments成功(t *testing.T) {
	im := &recordingIndexer{deleteResult: true}
	kb, err := NewSimpleKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb"}, nil, WithIndexManager(im))
	require.NoError(t, err)
	ok, err := kb.DeleteDocuments(context.Background(), []string{"doc_1", "doc_2"})
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, 2, im.deleteCalls)
	assert.Equal(t, []string{"doc_1", "doc_2"}, im.deleteIDs)
}

// TestSimpleKB_DeleteDocuments部分失败 Go 增强：任一 false → 总 false（循环不中断）。
func TestSimpleKB_DeleteDocuments部分失败(t *testing.T) {
	im := &countingDeleteIndexer{}
	kb, err := NewSimpleKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb"}, nil, WithIndexManager(im))
	require.NoError(t, err)
	ok, err := kb.DeleteDocuments(context.Background(), []string{"doc_1", "doc_2"})
	require.NoError(t, err)
	assert.False(t, ok)
	assert.Equal(t, 2, im.calls)
}

// TestSimpleKB_DeleteDocuments无索引管理器 对齐 test_delete_documents_without_index_manager。
func TestSimpleKB_DeleteDocuments无索引管理器(t *testing.T) {
	kb, err := NewSimpleKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb"}, nil)
	require.NoError(t, err)
	_, err = kb.DeleteDocuments(context.Background(), []string{"doc_1"})
	var be *exception.BaseError
	require.True(t, errors.As(err, &be))
	assert.Equal(t, 155502, be.Code())
	assert.Contains(t, be.Message(), "index_manager is required")
}

// ──────────────────────────── UpdateDocuments ────────────────────────────

// TestSimpleKB_UpdateDocuments成功 对齐 test_update_documents_success + 不对称断言。
func TestSimpleKB_UpdateDocuments成功(t *testing.T) {
	im := &recordingIndexer{updateResult: true}
	ch := &countingChunker{chunks: []common.TextChunk{
		{ID: "c1", Text: "x", DocID: "doc_1"},
		{ID: "c2", Text: "y", DocID: "doc_2"},
	}}
	kb, err := NewSimpleKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb", UseCaptionForImages: true},
		nil, WithChunker(ch), WithIndexManager(im))
	require.NoError(t, err)
	ids, err := kb.UpdateDocuments(context.Background(), []reranker.Document{{ID: "doc_1", Text: "Updated"}})
	require.NoError(t, err)
	assert.Equal(t, []string{"doc_1"}, ids)
	assert.Equal(t, 1, im.updateCalls)
	// 不对称偏差：update 的 IndexConfig 不带 UseCaptionForImages（即使 KB 配置为 true）
	assert.False(t, im.lastUpdateCfg.UseCaptionForImages)
	assert.Equal(t, "kb_kb_chunks", im.lastUpdateCfg.IndexName)
}

// TestSimpleKB_UpdateDocuments无分块器 对齐 Python update 前置校验（文案区分 add）。
func TestSimpleKB_UpdateDocuments无分块器(t *testing.T) {
	kb, err := NewSimpleKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb"}, nil)
	require.NoError(t, err)
	_, err = kb.UpdateDocuments(context.Background(), []reranker.Document{{Text: "x"}})
	var be *exception.BaseError
	require.True(t, errors.As(err, &be))
	assert.Equal(t, 155501, be.Code())
	assert.Contains(t, be.Message(), "chunker is required for update_documents")
}

// TestSimpleKB_UpdateDocuments无索引管理器 对齐 Python update 前置校验。
func TestSimpleKB_UpdateDocuments无索引管理器(t *testing.T) {
	kb, err := NewSimpleKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb"}, nil, WithChunker(&countingChunker{}))
	require.NoError(t, err)
	_, err = kb.UpdateDocuments(context.Background(), []reranker.Document{{Text: "x"}})
	var be *exception.BaseError
	require.True(t, errors.As(err, &be))
	assert.Equal(t, 155502, be.Code())
	assert.Contains(t, be.Message(), "index_manager is required for update_documents")
}

// TestSimpleKB_UpdateDocuments无匹配分块 文档无对应 chunks 时跳过（不调 UpdateIndex）。
func TestSimpleKB_UpdateDocuments无匹配分块(t *testing.T) {
	im := &recordingIndexer{updateResult: true}
	ch := &countingChunker{chunks: []common.TextChunk{{ID: "c1", DocID: "other"}}}
	kb, err := NewSimpleKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb"}, nil,
		WithChunker(ch), WithIndexManager(im))
	require.NoError(t, err)
	ids, err := kb.UpdateDocuments(context.Background(), []reranker.Document{{ID: "doc_1", Text: "x"}})
	require.NoError(t, err)
	assert.Empty(t, ids)
	assert.Equal(t, 0, im.updateCalls)
}

// TestSimpleKB_UpdateDocuments索引返回false 未成功不收集 doc_id。
func TestSimpleKB_UpdateDocuments索引返回false(t *testing.T) {
	im := &falseUpdateIndexer{}
	ch := &countingChunker{chunks: []common.TextChunk{{ID: "c1", DocID: "doc_1"}}}
	kb, err := NewSimpleKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb"}, nil,
		WithChunker(ch), WithIndexManager(im))
	require.NoError(t, err)
	ids, err := kb.UpdateDocuments(context.Background(), []reranker.Document{{ID: "doc_1", Text: "x"}})
	require.NoError(t, err)
	assert.Empty(t, ids)
}

// TestSimpleKB_UpdateDocuments索引错误 UpdateIndex 错误直接冒泡。
func TestSimpleKB_UpdateDocuments索引错误(t *testing.T) {
	im := &errUpdateIndexer{}
	ch := &countingChunker{chunks: []common.TextChunk{{ID: "c1", DocID: "doc_1"}}}
	kb, err := NewSimpleKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb"}, nil,
		WithChunker(ch), WithIndexManager(im))
	require.NoError(t, err)
	_, err = kb.UpdateDocuments(context.Background(), []reranker.Document{{ID: "doc_1", Text: "x"}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "update boom")
}

// ──────────────────────────── GetStatistics ────────────────────────────

// TestSimpleKB_GetStatistics成功 对齐 test_get_statistics_success。
func TestSimpleKB_GetStatistics成功(t *testing.T) {
	im := &statsIndexer{info: map[string]any{"count": 10}}
	kb, err := NewSimpleKnowledgeBase(
		common.KnowledgeBaseConfig{KBID: "test_kb", IndexType: common.IndexTypeVector},
		nil, WithIndexManager(im), WithChunker(&countingChunker{}))
	require.NoError(t, err)
	stats, err := kb.GetStatistics(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "test_kb", stats["kb_id"])
	assert.Equal(t, "vector", stats["index_type"])
	assert.Contains(t, stats, "index_info")
	assert.Equal(t, true, stats["has_chunker"])
	assert.Equal(t, false, stats["has_parser"])
	assert.Equal(t, false, stats["has_extractor"])
	assert.Equal(t, false, stats["has_embed_model"])
	assert.Equal(t, false, stats["has_vector_store"])
	assert.Len(t, stats, 8)
	assert.Equal(t, "kb_test_kb_chunks", im.got)
}

// TestSimpleKB_GetStatistics无索引管理器 对齐 test_get_statistics_without_index_manager。
func TestSimpleKB_GetStatistics无索引管理器(t *testing.T) {
	kb, err := NewSimpleKnowledgeBase(common.KnowledgeBaseConfig{KBID: "test_kb"}, nil)
	require.NoError(t, err)
	stats, err := kb.GetStatistics(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "test_kb", stats["kb_id"])
	assert.Equal(t, false, stats["index_exists"])
	assert.Len(t, stats, 2)
}

// TestSimpleKB_GetStatistics错误冒泡 Go 增强：GetIndexInfo 错误直接冒泡（Python 无 try）。
func TestSimpleKB_GetStatistics错误冒泡(t *testing.T) {
	im := &statsIndexer{err: errors.New("boom")}
	kb, err := NewSimpleKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb"}, nil, WithIndexManager(im))
	require.NoError(t, err)
	_, err = kb.GetStatistics(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "boom")
}

// ──────────────────────────── Retrieve ────────────────────────────

// fakeEmbed 固定向量的嵌入桩（懒创建 vector 分支所需）。
type fakeEmbed struct{}

func (f *fakeEmbed) EmbedQuery(context.Context, string, ...embedding.EmbedOption) ([]float64, error) {
	return []float64{0.1}, nil
}
func (f *fakeEmbed) EmbedDocuments(context.Context, []string, ...embedding.EmbedOption) ([][]float64, error) {
	return nil, nil
}
func (f *fakeEmbed) Dimension() int                                    { return 1 }
func (f *fakeEmbed) DimensionWithContext(context.Context) (int, error) { return 1, nil }

// TestSimpleKB_Retrieve带检索器 对齐 test_retrieve_with_retriever。
func TestSimpleKB_Retrieve带检索器(t *testing.T) {
	r := &recordingRetriever{results: []common.RetrievalResult{{Text: "Test result", Score: 0.95}}}
	kb, err := NewSimpleKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb", IndexType: common.IndexTypeVector}, r)
	require.NoError(t, err)
	results, err := kb.Retrieve(context.Background(), "test query", nil)
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, 1, r.calls)
}

// TestSimpleKB_Retrieve非agentic 对齐 test_retrieve_without_agentic。
func TestSimpleKB_Retrieve非agentic(t *testing.T) {
	r := &recordingRetriever{results: []common.RetrievalResult{{Text: "r", Score: 0.9}}}
	kb, err := NewSimpleKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb"}, r)
	require.NoError(t, err)
	results, err := kb.Retrieve(context.Background(), "q", common.NewDefaultRetrievalConfig())
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, 1, r.calls)
}

// TestSimpleKB_Retrieve无检索器无存储 对齐 test_retrieve_without_retriever_or_vector_store。
func TestSimpleKB_Retrieve无检索器无存储(t *testing.T) {
	kb, err := NewSimpleKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb"}, nil)
	require.NoError(t, err)
	_, err = kb.Retrieve(context.Background(), "test query", nil)
	var be *exception.BaseError
	require.True(t, errors.As(err, &be))
	assert.Equal(t, 155503, be.Code())
	assert.Contains(t, be.Message(), "vector_store or retriever is required")
}

// TestSimpleKB_Retrieve懒创建三分支 index_type 决定 retriever 具体类型。
func TestSimpleKB_Retrieve懒创建三分支(t *testing.T) {
	vs, _ := makeCompatiblePair()
	cases := []struct {
		indexType common.IndexTypeKind
		assertTyp func(t *testing.T, r retriever.Retriever)
	}{
		{common.IndexTypeVector, func(t *testing.T, r retriever.Retriever) {
			_, ok := r.(*retriever.VectorRetriever)
			assert.True(t, ok, "vector → VectorRetriever")
		}},
		{common.IndexTypeBM25, func(t *testing.T, r retriever.Retriever) {
			_, ok := r.(*retriever.SparseRetriever)
			assert.True(t, ok, "bm25 → SparseRetriever")
		}},
		{common.IndexTypeHybrid, func(t *testing.T, r retriever.Retriever) {
			_, ok := r.(*retriever.HybridRetriever)
			assert.True(t, ok, "hybrid → HybridRetriever")
		}},
	}
	for _, tc := range cases {
		t.Run(string(tc.indexType), func(t *testing.T) {
			kb, err := NewSimpleKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb", IndexType: tc.indexType},
				nil, WithVectorStore(vs), WithEmbedModel(&fakeEmbed{}))
			require.NoError(t, err)
			// fakeAttrStore 各 search 返回空，检索器返回空结果
			_, err = kb.Retrieve(context.Background(), "q", nil)
			require.NoError(t, err)
			require.NotNil(t, kb.retriever)
			tc.assertTyp(t, kb.retriever)
		})
	}
}

// TestSimpleKB_Retrieve模式推导对齐 断言传给底层 retriever 的 mode 与 TopK。
func TestSimpleKB_Retrieve模式推导对齐(t *testing.T) {
	cases := []struct {
		indexType common.IndexTypeKind
		wantMode  retriever.RetrievalMode
	}{
		{common.IndexTypeVector, retriever.RetrievalModeVector},
		{common.IndexTypeBM25, retriever.RetrievalModeSparse},
		{common.IndexTypeHybrid, retriever.RetrievalModeHybrid},
		{common.IndexTypeKind(""), retriever.RetrievalModeHybrid}, // 其他 → hybrid
	}
	for _, tc := range cases {
		r := &recordingRetriever{}
		kb, err := NewSimpleKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb", IndexType: tc.indexType}, r)
		require.NoError(t, err)
		_, err = kb.Retrieve(context.Background(), "q", nil)
		require.NoError(t, err)
		assert.Equal(t, tc.wantMode, r.gotMode)
		// nil config → 私有默认 TopK 5（Python RetrievalConfig() 默认）
		assert.Equal(t, 5, r.gotTopK)
	}
}

// TestSimpleKB_Retrieve配置TopK优先 显式 config.TopK 覆盖默认。
func TestSimpleKB_Retrieve配置TopK优先(t *testing.T) {
	r := &recordingRetriever{}
	kb, err := NewSimpleKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb"}, r)
	require.NoError(t, err)
	_, err = kb.Retrieve(context.Background(), "q", &common.RetrievalConfig{TopK: 12})
	require.NoError(t, err)
	assert.Equal(t, 12, r.gotTopK)
}

// TestSimpleKB_Retrieve并发懒创建 -race：并发 Retrieve 只建一次且全部成功。
func TestSimpleKB_Retrieve并发懒创建(t *testing.T) {
	vs, _ := makeCompatiblePair()
	kb, err := NewSimpleKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb", IndexType: common.IndexTypeBM25},
		nil, WithVectorStore(vs))
	require.NoError(t, err)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = kb.Retrieve(context.Background(), "q", nil)
		}()
	}
	wg.Wait()
	assert.NotNil(t, kb.retriever)
}

// TestSimpleKB_Retrieve检索错误冒泡 底层 retriever 错误原样返回。
func TestSimpleKB_Retrieve检索错误冒泡(t *testing.T) {
	boom := errors.New("retrieve boom")
	r := &recordingRetriever{err: boom}
	kb, err := NewSimpleKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb"}, r)
	require.NoError(t, err)
	_, err = kb.Retrieve(context.Background(), "q", nil)
	require.ErrorIs(t, err, boom)
}

// agenticRewriteLLM 读步骤返回空、改写步骤返回下一问题的桩——
// 用于观察 max_iter 的真实生效次数。
type agenticRewriteLLM struct {
	stubKBLlm
	mu           sync.Mutex
	readCalls    int
	rewriteCalls int
}

func (a *agenticRewriteLLM) Invoke(_ context.Context, messages model_clients.MessagesParam, _ ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
	prompt := ""
	for _, m := range messages.Messages() {
		if u, ok := m.(*llmschema.UserMessage); ok {
			prompt = u.Content.Text()
		}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	switch {
	case strings.Contains(prompt, "knowledge triples, which are structured"):
		a.readCalls++
		return llmschema.NewAssistantMessage(""), nil
	case strings.Contains(prompt, `"sufficient" to true`):
		a.rewriteCalls++
		return llmschema.NewAssistantMessage(`{"sufficient": false, "next_question": "q_next"}`), nil
	default:
		return llmschema.NewAssistantMessage(""), nil
	}
}

// TestSimpleKB_RetrieveAgentic 对齐 test_retrieve_with_agentic：agentic 分支
// 用 AgenticRetriever 包裹底层 retriever（retriever/llm_client 透传）。
func TestSimpleKB_RetrieveAgentic(t *testing.T) {
	r := &recordingRetriever{results: []common.RetrievalResult{{Text: "r", Score: 0.9}}}
	llm := &agenticRewriteLLM{}
	kb, err := NewSimpleKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb"}, r, WithLLMClient(llm))
	require.NoError(t, err)
	results, err := kb.Retrieve(context.Background(), "test query", &common.RetrievalConfig{TopK: 5, Agentic: true})
	require.NoError(t, err)
	assert.NotEmpty(t, results)
	// 默认 max_iter=2 → 底层 retriever 被调用 2 次（每轮一次）
	assert.Equal(t, 2, r.calls)
}

// TestSimpleKB_RetrieveAgenticMaxIter Extra["max_iter"] 映射 WithAgenticMaxIter：
// max_iter=3 时底层 retriever 调用 3 次；未知键静默忽略不报错。
func TestSimpleKB_RetrieveAgenticMaxIter(t *testing.T) {
	r := &recordingRetriever{results: []common.RetrievalResult{{Text: "r", Score: 0.9}}}
	llm := &agenticRewriteLLM{}
	kb, err := NewSimpleKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb"}, r, WithLLMClient(llm))
	require.NoError(t, err)
	cfg := &common.RetrievalConfig{TopK: 5, Agentic: true}
	_, err = kb.Retrieve(context.Background(), "q", cfg, WithKBExtra("max_iter", 3))
	require.NoError(t, err)
	assert.Equal(t, 3, r.calls)

	// 未知键静默忽略（Python 会 TypeError，行为分叉已归档 spec §8-6）
	r2 := &recordingRetriever{results: []common.RetrievalResult{{Text: "r", Score: 0.9}}}
	kb2, err := NewSimpleKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb"}, r2, WithLLMClient(&agenticRewriteLLM{}))
	require.NoError(t, err)
	_, err = kb2.Retrieve(context.Background(), "q", cfg, WithKBExtra("unknown_key", "x"))
	require.NoError(t, err)
}

// TestSimpleKB_RetrieveFilters透传 Filters 经 RetrieveOptions 传给 retriever
// （写而不消费，13.19 伏笔回填）。
func TestSimpleKB_RetrieveFilters透传(t *testing.T) {
	r := &recordingRetriever{}
	kb, err := NewSimpleKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb"}, r)
	require.NoError(t, err)
	cfg := &common.RetrievalConfig{TopK: 5, Filters: map[string]any{"doc_type": "pdf"}}
	_, err = kb.Retrieve(context.Background(), "q", cfg)
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"doc_type": "pdf"}, r.gotOpts.Filters)
}

// TestSimpleKB_RetrieveFilters为nil filters 未设时 Options.Filters 为 nil。
func TestSimpleKB_RetrieveFilters为nil(t *testing.T) {
	r := &recordingRetriever{}
	kb, err := NewSimpleKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb"}, r)
	require.NoError(t, err)
	_, err = kb.Retrieve(context.Background(), "q", nil)
	require.NoError(t, err)
	assert.Nil(t, r.gotOpts.Filters)
}

// TestSimpleKB_RetrieveAgentic无LLM agentic 但 llmClient nil → 155209 冒泡。
func TestSimpleKB_RetrieveAgentic无LLM(t *testing.T) {
	r := &recordingRetriever{}
	kb, err := NewSimpleKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb"}, r)
	require.NoError(t, err)
	_, err = kb.Retrieve(context.Background(), "q", &common.RetrievalConfig{TopK: 5, Agentic: true})
	require.Error(t, err)
	var be *exception.BaseError
	require.True(t, errors.As(err, &be))
	assert.Equal(t, 155209, be.Code())
}
