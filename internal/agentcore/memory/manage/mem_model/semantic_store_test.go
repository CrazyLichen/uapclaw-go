package mem_model

import (
	"context"
	"testing"

	embedding "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/embedding"
	vector "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/vector"
)

// ──────────────────────────── Fake 实现 ────────────────────────────

// fakeEmbedding 用于测试的模拟嵌入模型
type fakeEmbedding struct {
	dimension int
}

func (f *fakeEmbedding) EmbedQuery(ctx context.Context, text string, opts ...embedding.EmbedOption) ([]float64, error) {
	result := make([]float64, f.dimension)
	for i := range result {
		result[i] = 0.1
	}
	return result, nil
}

func (f *fakeEmbedding) EmbedDocuments(ctx context.Context, texts []string, opts ...embedding.EmbedOption) ([][]float64, error) {
	result := make([][]float64, len(texts))
	for i := range result {
		vec := make([]float64, f.dimension)
		for j := range vec {
			vec[j] = 0.1
		}
		result[i] = vec
	}
	return result, nil
}

func (f *fakeEmbedding) Dimension() int {
	return f.dimension
}

func (f *fakeEmbedding) DimensionWithContext(ctx context.Context) (int, error) {
	return f.dimension, nil
}

// fakeVectorStore 用于测试的模拟向量存储
type fakeVectorStore struct {
	collections map[string]bool
	docs        map[string][]map[string]any
}

func newFakeVectorStore() *fakeVectorStore {
	return &fakeVectorStore{
		collections: make(map[string]bool),
		docs:        make(map[string][]map[string]any),
	}
}

func (f *fakeVectorStore) CreateCollection(ctx context.Context, name string, schema *vector.CollectionSchema, opts ...vector.Option) error {
	f.collections[name] = true
	return nil
}

func (f *fakeVectorStore) DeleteCollection(ctx context.Context, name string, opts ...vector.Option) error {
	delete(f.collections, name)
	delete(f.docs, name)
	return nil
}

func (f *fakeVectorStore) CollectionExists(ctx context.Context, name string, opts ...vector.Option) (bool, error) {
	return f.collections[name], nil
}

func (f *fakeVectorStore) GetSchema(ctx context.Context, name string, opts ...vector.Option) (*vector.CollectionSchema, error) {
	return nil, nil
}

func (f *fakeVectorStore) AddDocs(ctx context.Context, name string, docs []map[string]any, opts ...vector.Option) error {
	f.docs[name] = append(f.docs[name], docs...)
	return nil
}

func (f *fakeVectorStore) Search(ctx context.Context, name string, queryVector []float64, vectorField string, topK int, filters map[string]any, opts ...vector.Option) ([]vector.VectorSearchResult, error) {
	return []vector.VectorSearchResult{}, nil
}

func (f *fakeVectorStore) DeleteDocsByIDs(ctx context.Context, name string, ids []string, opts ...vector.Option) error {
	return nil
}

func (f *fakeVectorStore) DeleteDocsByFilters(ctx context.Context, name string, filters map[string]any, opts ...vector.Option) error {
	return nil
}

func (f *fakeVectorStore) ListCollectionNames(ctx context.Context) ([]string, error) {
	names := make([]string, 0, len(f.collections))
	for name := range f.collections {
		names = append(names, name)
	}
	return names, nil
}

func (f *fakeVectorStore) UpdateSchema(ctx context.Context, name string, operations []any, opts ...vector.Option) error {
	return nil
}

func (f *fakeVectorStore) UpdateCollectionMetadata(ctx context.Context, name string, metadata map[string]any, opts ...vector.Option) error {
	return nil
}

func (f *fakeVectorStore) GetCollectionMetadata(ctx context.Context, name string, opts ...vector.Option) (map[string]any, error) {
	return nil, nil
}

// ──────────────────────────── SemanticStore 测试 ────────────────────────────

// TestNewSemanticStore 测试创建 SemanticStore
func TestNewSemanticStore(t *testing.T) {
	vs := newFakeVectorStore()
	em := &fakeEmbedding{dimension: 4}
	store := NewSemanticStore(vs, em)
	if store == nil {
		t.Fatal("NewSemanticStore() 返回 nil")
	}
	if store.embeddingModel == nil {
		t.Error("embeddingModel 不应为 nil")
	}
}

// TestNewSemanticStore_无Embedding 测试无 embedding 模型创建
func TestNewSemanticStore_无Embedding(t *testing.T) {
	vs := newFakeVectorStore()
	store := NewSemanticStore(vs, nil)
	if store == nil {
		t.Fatal("NewSemanticStore() 返回 nil")
	}
	if store.embeddingModel != nil {
		t.Error("embeddingModel 应为 nil")
	}
}

// TestSemanticStore_InitializeEmbeddingModel 测试延迟初始化
func TestSemanticStore_InitializeEmbeddingModel(t *testing.T) {
	vs := newFakeVectorStore()
	store := NewSemanticStore(vs, nil)
	if store.embeddingModel != nil {
		t.Error("初始 embeddingModel 应为 nil")
	}
	em := &fakeEmbedding{dimension: 4}
	store.InitializeEmbeddingModel(em)
	if store.embeddingModel == nil {
		t.Error("InitializeEmbeddingModel 后 embeddingModel 不应为 nil")
	}
}

// TestSemanticStore_AddDocs 测试添加文档
func TestSemanticStore_AddDocs(t *testing.T) {
	vs := newFakeVectorStore()
	em := &fakeEmbedding{dimension: 4}
	store := NewSemanticStore(vs, em)
	ctx := context.Background()

	docs := []DocTuple{
		{ID: "doc1", Text: "hello world"},
		{ID: "doc2", Text: "goodbye world"},
	}
	ok, err := store.AddDocs(ctx, docs, "test_collection", "scope1")
	if err != nil {
		t.Fatalf("AddDocs() error = %v", err)
	}
	if !ok {
		t.Error("AddDocs() 应返回 true")
	}
}

// TestSemanticStore_AddDocs_无Embedding 测试 embeddingModel 为 nil 返回 false
func TestSemanticStore_AddDocs_无Embedding(t *testing.T) {
	vs := newFakeVectorStore()
	store := NewSemanticStore(vs, nil)
	ctx := context.Background()

	docs := []DocTuple{{ID: "doc1", Text: "hello"}}
	ok, err := store.AddDocs(ctx, docs, "test_collection", "scope1")
	if err != nil {
		t.Fatalf("AddDocs() error = %v", err)
	}
	if ok {
		t.Error("AddDocs() embeddingModel 为 nil 时应返回 false")
	}
}

// TestSemanticStore_AddDocs_空docs 测试空文档列表
func TestSemanticStore_AddDocs_空docs(t *testing.T) {
	vs := newFakeVectorStore()
	em := &fakeEmbedding{dimension: 4}
	store := NewSemanticStore(vs, em)
	ctx := context.Background()

	ok, err := store.AddDocs(ctx, []DocTuple{}, "test_collection", "scope1")
	if err != nil {
		t.Fatalf("AddDocs() error = %v", err)
	}
	if !ok {
		t.Error("AddDocs() 空 docs 应返回 true")
	}
}

// TestSemanticStore_AddDocs_创建集合 测试创建集合被调用
func TestSemanticStore_AddDocs_创建集合(t *testing.T) {
	vs := newFakeVectorStore()
	em := &fakeEmbedding{dimension: 4}
	store := NewSemanticStore(vs, em)
	ctx := context.Background()

	docs := []DocTuple{{ID: "doc1", Text: "hello"}}
	_, _ = store.AddDocs(ctx, docs, "new_collection", "scope1")

	if !vs.collections["new_collection"] {
		t.Error("AddDocs 应创建集合")
	}
}

// TestSemanticStore_DeleteDocs 测试删除文档
func TestSemanticStore_DeleteDocs(t *testing.T) {
	vs := newFakeVectorStore()
	vs.collections["test_collection"] = true
	em := &fakeEmbedding{dimension: 4}
	store := NewSemanticStore(vs, em)
	ctx := context.Background()

	err := store.DeleteDocs(ctx, []string{"doc1"}, "test_collection")
	if err != nil {
		t.Fatalf("DeleteDocs() error = %v", err)
	}
}

// TestSemanticStore_DeleteDocs_集合不存在 测试集合不存在时正常返回
func TestSemanticStore_DeleteDocs_集合不存在(t *testing.T) {
	vs := newFakeVectorStore()
	em := &fakeEmbedding{dimension: 4}
	store := NewSemanticStore(vs, em)
	ctx := context.Background()

	err := store.DeleteDocs(ctx, []string{"doc1"}, "nonexist")
	if err != nil {
		t.Fatalf("DeleteDocs() 集合不存在时不应报错, error = %v", err)
	}
}

// TestSemanticStore_Search 测试搜索
func TestSemanticStore_Search(t *testing.T) {
	vs := newFakeVectorStore()
	vs.collections["test_collection"] = true
	em := &fakeEmbedding{dimension: 4}
	store := NewSemanticStore(vs, em)
	ctx := context.Background()

	results, err := store.Search(ctx, "hello", "test_collection", "scope1", 5)
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	// fakeVectorStore.Search 返回空结果
	if results == nil {
		t.Error("Search() 不应返回 nil")
	}
}

// TestSemanticStore_Search_无Embedding 测试 embeddingModel 为 nil 返回空
func TestSemanticStore_Search_无Embedding(t *testing.T) {
	vs := newFakeVectorStore()
	vs.collections["test_collection"] = true
	store := NewSemanticStore(vs, nil)
	ctx := context.Background()

	results, err := store.Search(ctx, "hello", "test_collection", "scope1", 5)
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(results) != 0 {
		t.Error("Search() embeddingModel 为 nil 时应返回空结果")
	}
}

// TestSemanticStore_Search_集合不存在 测试集合不存在返回空
func TestSemanticStore_Search_集合不存在(t *testing.T) {
	vs := newFakeVectorStore()
	em := &fakeEmbedding{dimension: 4}
	store := NewSemanticStore(vs, em)
	ctx := context.Background()

	results, err := store.Search(ctx, "hello", "nonexist", "scope1", 5)
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(results) != 0 {
		t.Error("Search() 集合不存在时应返回空结果")
	}
}

// TestSemanticStore_DeleteTable 测试删除集合
func TestSemanticStore_DeleteTable(t *testing.T) {
	vs := newFakeVectorStore()
	vs.collections["test_collection"] = true
	em := &fakeEmbedding{dimension: 4}
	store := NewSemanticStore(vs, em)
	ctx := context.Background()

	err := store.DeleteTable(ctx, "test_collection")
	if err != nil {
		t.Fatalf("DeleteTable() error = %v", err)
	}
	if vs.collections["test_collection"] {
		t.Error("DeleteTable 后集合应不存在")
	}
}

// TestSemanticStore_DeleteTable_缓存清理 测试删除后缓存清理
func TestSemanticStore_DeleteTable_缓存清理(t *testing.T) {
	vs := newFakeVectorStore()
	em := &fakeEmbedding{dimension: 4}
	store := NewSemanticStore(vs, em)
	ctx := context.Background()

	// 先添加文档以创建缓存
	docs := []DocTuple{{ID: "doc1", Text: "hello"}}
	_, _ = store.AddDocs(ctx, docs, "cache_test", "scope1")
	if _, ok := store.createdCollections["cache_test"]; !ok {
		t.Error("AddDocs 后应在缓存中")
	}

	// 删除集合
	_ = store.DeleteTable(ctx, "cache_test")
	if _, ok := store.createdCollections["cache_test"]; ok {
		t.Error("DeleteTable 后应从缓存中移除")
	}
}
