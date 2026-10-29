package knowledgebase

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/embedding"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/query"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/indexing/indexer"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/vector_store"
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
