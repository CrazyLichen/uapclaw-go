package knowledgebase

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/embedding"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/query"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/reranker"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/indexing/indexer"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/indexing/processor/parser"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/vector_store"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 测试桩 ────────────────────────────

// fakeAttrStore 属性可配置的 VectorStore 桩（Python MagicMock setattr 同构）。
// 校验/模板方法路径不触达真实存储语义，接口方法返回零值。
type fakeAttrStore struct {
	databaseName      string
	distanceMetric    string
	textField         string
	vectorField       any
	sparseVectorField string
	metadataField     string
	docIDField        string
}

func (f *fakeAttrStore) CheckVectorField() error { return nil }
func (f *fakeAttrStore) Add(ctx context.Context, data []map[string]any, opts ...vector_store.StoreOption) error {
	return nil
}
func (f *fakeAttrStore) Search(ctx context.Context, queryVector []float64, topK int, filters query.QueryExpr, opts ...vector_store.StoreOption) ([]common.SearchResult, error) {
	return nil, nil
}
func (f *fakeAttrStore) SparseSearch(ctx context.Context, queryText string, topK int, filters query.QueryExpr, opts ...vector_store.StoreOption) ([]common.SearchResult, error) {
	return nil, nil
}
func (f *fakeAttrStore) HybridSearch(ctx context.Context, queryText string, queryVector []float64, topK int, alpha float64, filters query.QueryExpr, opts ...vector_store.StoreOption) ([]common.SearchResult, error) {
	return nil, nil
}
func (f *fakeAttrStore) Delete(ctx context.Context, ids []string, filterExpr query.QueryExpr) (bool, error) {
	return false, nil
}
func (f *fakeAttrStore) TableExists(ctx context.Context, tableName string) (bool, error) {
	return false, nil
}
func (f *fakeAttrStore) DeleteTable(ctx context.Context, tableName string) error { return nil }
func (f *fakeAttrStore) Close()                                                    {}

// storageAttrProvider 7 getter（13.26 校验消费接口）
func (f *fakeAttrStore) DatabaseName() string      { return f.databaseName }
func (f *fakeAttrStore) DistanceMetric() string    { return f.distanceMetric }
func (f *fakeAttrStore) TextField() string         { return f.textField }
func (f *fakeAttrStore) VectorField() any          { return f.vectorField }
func (f *fakeAttrStore) SparseVectorField() string { return f.sparseVectorField }
func (f *fakeAttrStore) MetadataField() string     { return f.metadataField }
func (f *fakeAttrStore) DocIDField() string        { return f.docIDField }

// fakeAttrIndexer 属性可配置的 Indexer 桩。
type fakeAttrIndexer struct {
	databaseName      string
	distanceMetric    string
	textField         string
	vectorField       any
	sparseVectorField string
	metadataField     string
	docIDField        string
}

func (f *fakeAttrIndexer) BuildIndex(ctx context.Context, chunks []common.TextChunk, config common.IndexConfig, embedModel embedding.BaseEmbedding, opts ...indexer.IndexOption) (bool, error) {
	return true, nil
}
func (f *fakeAttrIndexer) UpdateIndex(ctx context.Context, chunks []common.TextChunk, docID string, config common.IndexConfig, embedModel embedding.BaseEmbedding, opts ...indexer.IndexOption) (bool, error) {
	return true, nil
}
func (f *fakeAttrIndexer) DeleteIndex(ctx context.Context, docID string, indexName string) (bool, error) {
	return true, nil
}
func (f *fakeAttrIndexer) IndexExists(ctx context.Context, indexName string) (bool, error) {
	return true, nil
}
func (f *fakeAttrIndexer) GetIndexInfo(ctx context.Context, indexName string) (map[string]any, error) {
	return nil, nil
}

func (f *fakeAttrIndexer) DatabaseName() string      { return f.databaseName }
func (f *fakeAttrIndexer) DistanceMetric() string    { return f.distanceMetric }
func (f *fakeAttrIndexer) TextField() string         { return f.textField }
func (f *fakeAttrIndexer) VectorField() any          { return f.vectorField }
func (f *fakeAttrIndexer) SparseVectorField() string { return f.sparseVectorField }
func (f *fakeAttrIndexer) MetadataField() string     { return f.metadataField }
func (f *fakeAttrIndexer) DocIDField() string        { return f.docIDField }

// makeCompatiblePair 构造全属性匹配的桩对（Python _create_compatible_mocks 同构）。
func makeCompatiblePair() (*fakeAttrStore, *fakeAttrIndexer) {
	vs := &fakeAttrStore{
		databaseName: "test_db", distanceMetric: "cosine", textField: "content",
		vectorField: any("test_vector_field"), sparseVectorField: "sparse_vector",
		metadataField: "metadata", docIDField: "document_id",
	}
	im := &fakeAttrIndexer{
		databaseName: "test_db", distanceMetric: "cosine", textField: "content",
		vectorField: any("test_vector_field"), sparseVectorField: "sparse_vector",
		metadataField: "metadata", docIDField: "document_id",
	}
	return vs, im
}

// ──────────────────────────── 构造器测试 ────────────────────────────

// TestNewBaseKnowledgeBase_默认构造 对齐 test_init（全组件 nil，无校验触发）。
func TestNewBaseKnowledgeBase_默认构造(t *testing.T) {
	cfg := common.KnowledgeBaseConfig{KBID: "test_kb"}
	b, err := NewBaseKnowledgeBase(cfg)
	assert.NoError(t, err)
	assert.Equal(t, cfg, b.config)
	assert.Nil(t, b.vectorStore)
	assert.Nil(t, b.embedModel)
	assert.Nil(t, b.parser)
	assert.Nil(t, b.chunker)
	assert.Nil(t, b.extractor)
	assert.Nil(t, b.indexManager)
	assert.Nil(t, b.llmClient)
	assert.True(t, b.strictValidation) // Python 默认 true
}

// TestNewBaseKnowledgeBase_全组件构造 对齐 test_init_with_components。
func TestNewBaseKnowledgeBase_全组件构造(t *testing.T) {
	vs, im := makeCompatiblePair()
	b, err := NewBaseKnowledgeBase(common.KnowledgeBaseConfig{KBID: "test_kb"},
		WithVectorStore(vs), WithIndexManager(im))
	assert.NoError(t, err)
	assert.Equal(t, vs, b.vectorStore)
	assert.Equal(t, im, b.indexManager)
}

// TestNewBaseKnowledgeBase_单组件不触发校验 对齐 Python 单组件赋值时刻另一侧 None。
func TestNewBaseKnowledgeBase_单组件不触发校验(t *testing.T) {
	// 只有 vs（属性故意不匹配也不校验——im 缺失）
	vs, _ := makeCompatiblePair()
	im2 := &fakeAttrIndexer{databaseName: "another_db"}
	_ = im2 // 不传入
	b, err := NewBaseKnowledgeBase(common.KnowledgeBaseConfig{KBID: "test_kb"}, WithVectorStore(vs))
	assert.NoError(t, err)
	assert.NotNil(t, b.vectorStore)
	// 只有 im
	b2, err := NewBaseKnowledgeBase(common.KnowledgeBaseConfig{KBID: "test_kb"}, WithIndexManager(im2))
	assert.NoError(t, err)
	assert.NotNil(t, b2.indexManager)
}

// ──────────────────────────── 模板方法桩 ────────────────────────────

// fakeKBParser 记录调用参数的 Parser 桩（Python AsyncMock parser 同构）。
type fakeKBParser struct {
	// docs 预置返回
	docs []reranker.Document
	// err 预置返回错误
	err error
	// supports 返回值
	supports bool
	// gotDocs 收到的 doc 参数记录
	gotDocs []string
	// gotOpts 收到的 opts 快照（NewParseOptions 返回 *ParseOptions——base.go L95 已核实）
	gotOpts []*parser.ParseOptions
}

func (p *fakeKBParser) Parse(ctx context.Context, doc string, opts ...parser.ParseOption) ([]reranker.Document, error) {
	p.gotDocs = append(p.gotDocs, doc)
	p.gotOpts = append(p.gotOpts, parser.NewParseOptions(opts...))
	if p.err != nil {
		return nil, p.err
	}
	return p.docs, nil
}
func (p *fakeKBParser) LazyParse(ctx context.Context, doc string, fn func(reranker.Document) error, opts ...parser.ParseOption) error {
	return nil
}
func (p *fakeKBParser) Process(ctx context.Context, doc string, opts ...parser.ParseOption) ([]reranker.Document, error) {
	return p.Parse(ctx, doc, opts...)
}
func (p *fakeKBParser) Supports(doc string) bool { return p.supports }
func (p *fakeKBParser) ParseContent(ctx context.Context, doc string, llmClient *llm.Model) (string, error) {
	return "", nil
}

// fakeErrCloserIndexer Indexer 接口不含 Close——可自由加 Close() error 变体
// （test_close_with_exception 的 Go 映射通道）。
type fakeErrCloserIndexer struct {
	fakeAttrIndexer
	closeErr error
	closed   int
}

// Close 错误变体（Indexer 接口无 Close，无遮蔽冲突）。
func (f *fakeErrCloserIndexer) Close() error {
	f.closed++
	return f.closeErr
}

// ──────────────────────────── 模板方法测试 ────────────────────────────

// TestParseFiles_成功 对齐 test_parse_files_success（Python simple 测试覆盖，
// KB 基类 Go 增强补全——Python 基类测试未测模板方法）。
func TestParseFiles_成功(t *testing.T) {
	p := &fakeKBParser{
		docs: []reranker.Document{{ID: "doc_1", Text: "text1"}},
	}
	b, err := NewBaseKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb"}, WithParser(p))
	require.NoError(t, err)
	docs, err := b.ParseFiles(context.Background(), []string{"/a/b/file.txt"})
	require.NoError(t, err)
	assert.Len(t, docs, 1)
	assert.Equal(t, "/a/b/file.txt", p.gotDocs[0])
	// file_name 回落：未传 FileName → "/" split 末段
	assert.Equal(t, map[string]any{"file_name": "file.txt"}, p.gotOpts[0].Extra)
}

// TestParseFiles_无parser 对齐 test_parse_files_without_parser（155500）。
func TestParseFiles_无parser(t *testing.T) {
	b, err := NewBaseKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb"})
	require.NoError(t, err)
	_, err = b.ParseFiles(context.Background(), []string{"f.txt"})
	require.Error(t, err)
	var be *exception.BaseError
	require.True(t, errors.As(err, &be))
	assert.Equal(t, 155500, be.Code())
	assert.Contains(t, be.Message(), "parser is required for parse_files")
}

// TestParseFiles_单文件失败继续 对齐 test_parse_files_with_exception（软失败 continue）。
func TestParseFiles_单文件失败继续(t *testing.T) {
	p := &fakeKBParser{err: errors.New("boom"), docs: []reranker.Document{{ID: "d1"}}}
	b, err := NewBaseKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb"}, WithParser(p))
	require.NoError(t, err)
	docs, err := b.ParseFiles(context.Background(), []string{"a.txt", "b.txt"})
	require.NoError(t, err) // 异常被吞
	assert.Empty(t, docs)
	assert.Len(t, p.gotDocs, 2) // 两个文件都尝试了
}

// TestParseFiles_FileName选项 KBOptions.FileName 透传（Go 增强）。
func TestParseFiles_FileName选项(t *testing.T) {
	p := &fakeKBParser{docs: []reranker.Document{{ID: "d1"}}}
	b, _ := NewBaseKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb"}, WithParser(p))
	_, _ = b.ParseFiles(context.Background(), []string{"/a/b/file.txt"}, WithKBFileName("custom.md"))
	assert.Equal(t, map[string]any{"file_name": "custom.md"}, p.gotOpts[0].Extra)
}

// TestParseURLs_成功 对齐 test_parse_files_success 的 URL 版。
func TestParseURLs_成功(t *testing.T) {
	p := &fakeKBParser{supports: true, docs: []reranker.Document{{ID: "d1"}}}
	b, _ := NewBaseKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb"}, WithParser(p))
	docs, err := b.ParseURLs(context.Background(), []string{"https://x.com/a"}, WithKBExtra("k1", "v1"))
	require.NoError(t, err)
	assert.Len(t, docs, 1)
	// Extra 全量透传
	assert.Equal(t, map[string]any{"k1": "v1"}, p.gotOpts[0].Extra)
}

// TestParseURLs_不支持跳过 对齐 supports False → Warn + continue。
func TestParseURLs_不支持跳过(t *testing.T) {
	p := &fakeKBParser{supports: false}
	b, _ := NewBaseKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb"}, WithParser(p))
	docs, err := b.ParseURLs(context.Background(), []string{"https://x.com/a"})
	require.NoError(t, err)
	assert.Empty(t, docs)
	assert.Empty(t, p.gotDocs) // 未尝试 parse
}

// TestParseURLs_无parser 155500 error_msg 不同（parse_urls 变体）。
func TestParseURLs_无parser(t *testing.T) {
	b, _ := NewBaseKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb"})
	_, err := b.ParseURLs(context.Background(), []string{"https://x.com"})
	require.Error(t, err)
	var be *exception.BaseError
	require.True(t, errors.As(err, &be))
	assert.Equal(t, 155500, be.Code())
	assert.Contains(t, be.Message(), "parser is required for parse_urls")
}

// TestDeleteCollection_无store 155503。
func TestDeleteCollection_无store(t *testing.T) {
	b, _ := NewBaseKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb"})
	err := b.DeleteCollection(context.Background(), "coll")
	require.Error(t, err)
	var be *exception.BaseError
	require.True(t, errors.As(err, &be))
	assert.Equal(t, 155503, be.Code())
	assert.Contains(t, be.Message(), "vector_store is required for delete_collection")
}

// TestDeleteCollection_转发 对齐 delete_table 转发。
func TestDeleteCollection_转发(t *testing.T) {
	vs, _ := makeCompatiblePair()
	b, _ := NewBaseKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb"}, WithVectorStore(vs))
	assert.NoError(t, b.DeleteCollection(context.Background(), "coll"))
}

// TestClose_void变体关闭 对齐 test_close_with_sync_close。
func TestClose_void变体关闭(t *testing.T) {
	vs, im := makeCompatiblePair()
	b, _ := NewBaseKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb"},
		WithVectorStore(vs), WithIndexManager(im))
	b.Close() // 不 panic 即通过（void 桩无状态可断言，fakeAttrStore.Close 空实现）
}

// TestClose_双nil组件 对齐 test_close_with_none_components。
func TestClose_双nil组件(t *testing.T) {
	b, _ := NewBaseKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb"})
	b.Close() // 不 panic
}

// TestClose_错误变体吞并 对齐 test_close_with_exception（Warn 吞错误）。
func TestClose_错误变体吞并(t *testing.T) {
	vs, _ := makeCompatiblePair()
	ec := &fakeErrCloserIndexer{closeErr: errors.New("close boom")}
	// 构造尾部 7 属性比对需配齐（vs 全属性，ec 零值会 155508 → b 为 nil）
	ec.databaseName = "test_db"
	ec.distanceMetric = "cosine"
	ec.textField = "content"
	ec.vectorField = any("test_vector_field")
	ec.sparseVectorField = "sparse_vector"
	ec.metadataField = "metadata"
	ec.docIDField = "document_id"
	b, err := NewBaseKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb"},
		WithVectorStore(vs), WithIndexManager(ec))
	require.NoError(t, err)
	b.Close() // 错误被 Warn 吞，不 panic
	assert.Equal(t, 1, ec.closed)
}
