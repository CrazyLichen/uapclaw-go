package vector

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	chromav2 "github.com/amikos-tech/chroma-go/pkg/api/v2"
	"github.com/amikos-tech/chroma-go/pkg/embeddings"
)

// ──────────────────────────── 结构体 ────────────────────────────

// fakeChromaCollection 用于测试的 ChromaDB 集合模拟
type fakeChromaCollection struct {
	name     string
	metadata chromav2.CollectionMetadata
	docs     map[string]map[string]any // id → {document, metadata, embedding}
}

func newFakeChromaCollection(name string, meta chromav2.CollectionMetadata) *fakeChromaCollection {
	return &fakeChromaCollection{
		name:     name,
		metadata: meta,
		docs:     make(map[string]map[string]any),
	}
}

func (f *fakeChromaCollection) Name() string                                       { return f.name }
func (f *fakeChromaCollection) ID() string                                         { return f.name }
func (f *fakeChromaCollection) Tenant() chromav2.Tenant                            { return chromav2.NewDefaultTenant() }
func (f *fakeChromaCollection) Database() chromav2.Database                        { return chromav2.NewDefaultDatabase() }
func (f *fakeChromaCollection) Metadata() chromav2.CollectionMetadata              { return f.metadata }
func (f *fakeChromaCollection) Dimension() int                                     { return 128 }
func (f *fakeChromaCollection) Configuration() chromav2.CollectionConfiguration    { return nil }
func (f *fakeChromaCollection) Schema() *chromav2.Schema                           { return nil }
func (f *fakeChromaCollection) Count(ctx context.Context) (int, error)             { return len(f.docs), nil }
func (f *fakeChromaCollection) ModifyName(ctx context.Context, newName string) error { return nil }
func (f *fakeChromaCollection) Close() error                                       { return nil }
func (f *fakeChromaCollection) Fork(ctx context.Context, newName string) (chromav2.Collection, error) {
	return nil, fmt.Errorf("not implemented")
}
func (f *fakeChromaCollection) ForkCount(ctx context.Context) (int, error)   { return 0, nil }
func (f *fakeChromaCollection) IndexingStatus(ctx context.Context) (*chromav2.IndexingStatus, error) {
	return nil, nil
}
func (f *fakeChromaCollection) ModifyConfiguration(ctx context.Context, newConfig *chromav2.UpdateCollectionConfiguration) error {
	return nil
}

func (f *fakeChromaCollection) ModifyMetadata(ctx context.Context, newMeta chromav2.CollectionMetadata) error {
	f.metadata = newMeta
	return nil
}

func (f *fakeChromaCollection) Add(ctx context.Context, opts ...chromav2.AddOption) error {
	// 使用 SDK 的 CollectionAddOp 解析选项
	op := &chromav2.CollectionAddOp{}
	for _, opt := range opts {
		_ = opt.ApplyToAdd(op)
	}
	ids := op.Ids
	embs := op.Embeddings
	docs := op.Documents
	metas := op.Metadatas

	for i, id := range ids {
		doc := map[string]any{}
		if i < len(docs) {
			doc["document"] = docs[i].ContentString()
		}
		if i < len(metas) {
			doc["metadata"] = metas[i]
		}
		if i < len(embs) {
			doc["embedding"] = embs[i]
		}
		f.docs[string(id)] = doc
	}
	return nil
}

func (f *fakeChromaCollection) Upsert(ctx context.Context, opts ...chromav2.AddOption) error {
	return f.Add(ctx, opts...)
}

func (f *fakeChromaCollection) Update(ctx context.Context, opts ...chromav2.UpdateOption) error {
	return nil
}

func (f *fakeChromaCollection) Delete(ctx context.Context, opts ...chromav2.DeleteOption) error {
	op := &chromav2.CollectionDeleteOp{}
	for _, opt := range opts {
		_ = opt.ApplyToDelete(op)
	}
	if len(op.Ids) > 0 {
		for _, id := range op.Ids {
			delete(f.docs, string(id))
		}
	}
	return nil
}

func (f *fakeChromaCollection) Get(ctx context.Context, opts ...chromav2.GetOption) (chromav2.GetResult, error) {
	ids := make(chromav2.DocumentIDs, 0)
	docs := make(chromav2.Documents, 0)
	metas := make(chromav2.DocumentMetadatas, 0)
	embs := make(embeddings.Embeddings, 0)

	for id, docData := range f.docs {
		ids = append(ids, chromav2.DocumentID(id))

		if text, ok := docData["document"]; ok {
			if s, ok := text.(string); ok {
				docs = append(docs, chromav2.NewTextDocument(s))
			}
		} else {
			docs = append(docs, nil)
		}

		if meta, ok := docData["metadata"]; ok {
			if dm, ok := meta.(chromav2.DocumentMetadata); ok {
				metas = append(metas, dm)
			} else {
				metas = append(metas, nil)
			}
		} else {
			metas = append(metas, nil)
		}

		if emb, ok := docData["embedding"]; ok {
			if e, ok := emb.(embeddings.Embedding); ok {
				embs = append(embs, e)
			}
		}
	}

	return &chromav2.GetResultImpl{
		Ids:        ids,
		Documents:  docs,
		Metadatas:  metas,
		Embeddings: embs,
	}, nil
}

func (f *fakeChromaCollection) Query(ctx context.Context, opts ...chromav2.QueryOption) (chromav2.QueryResult, error) {
	// 返回空查询结果
	return &chromav2.QueryResultImpl{
		IDLists:        []chromav2.DocumentIDs{},
		DocumentsLists: []chromav2.Documents{},
		MetadatasLists: []chromav2.DocumentMetadatas{},
		DistancesLists: []embeddings.Distances{},
	}, nil
}

func (f *fakeChromaCollection) Search(ctx context.Context, opts ...chromav2.SearchCollectionOption) (chromav2.SearchResult, error) {
	return nil, fmt.Errorf("not implemented")
}

// fakeChromaClient 用于测试的 ChromaDB 客户端模拟
type fakeChromaClient struct {
	collections map[string]*fakeChromaCollection
	closeErr    error
}

func newFakeChromaClient() *fakeChromaClient {
	return &fakeChromaClient{
		collections: make(map[string]*fakeChromaCollection),
	}
}

func (f *fakeChromaClient) PreFlight(ctx context.Context) error                     { return nil }
func (f *fakeChromaClient) Heartbeat(ctx context.Context) error                     { return nil }
func (f *fakeChromaClient) GetVersion(ctx context.Context) (string, error)          { return "0.4.0", nil }
func (f *fakeChromaClient) GetIdentity(ctx context.Context) (chromav2.Identity, error) {
	return chromav2.Identity{}, nil
}
func (f *fakeChromaClient) GetTenant(ctx context.Context, tenant chromav2.Tenant) (chromav2.Tenant, error) {
	return chromav2.NewDefaultTenant(), nil
}
func (f *fakeChromaClient) UseTenant(ctx context.Context, tenant chromav2.Tenant) error { return nil }
func (f *fakeChromaClient) UseDatabase(ctx context.Context, db chromav2.Database) error { return nil }
func (f *fakeChromaClient) CreateTenant(ctx context.Context, tenant chromav2.Tenant) (chromav2.Tenant, error) {
	return chromav2.NewDefaultTenant(), nil
}
func (f *fakeChromaClient) ListDatabases(ctx context.Context, tenant chromav2.Tenant) ([]chromav2.Database, error) {
	return nil, nil
}
func (f *fakeChromaClient) GetDatabase(ctx context.Context, db chromav2.Database) (chromav2.Database, error) {
	return chromav2.NewDefaultDatabase(), nil
}
func (f *fakeChromaClient) CreateDatabase(ctx context.Context, db chromav2.Database) (chromav2.Database, error) {
	return chromav2.NewDefaultDatabase(), nil
}
func (f *fakeChromaClient) DeleteDatabase(ctx context.Context, db chromav2.Database) error { return nil }
func (f *fakeChromaClient) CurrentTenant() chromav2.Tenant                             { return chromav2.NewDefaultTenant() }
func (f *fakeChromaClient) CurrentDatabase() chromav2.Database                         { return chromav2.NewDefaultDatabase() }
func (f *fakeChromaClient) Reset(ctx context.Context) error                            { return nil }

func (f *fakeChromaClient) CreateCollection(ctx context.Context, name string, options ...chromav2.CreateCollectionOption) (chromav2.Collection, error) {
	op, err := chromav2.NewCreateCollectionOp(name, options...)
	if err != nil {
		return nil, err
	}
	coll := newFakeChromaCollection(name, op.Metadata)
	f.collections[name] = coll
	return coll, nil
}

func (f *fakeChromaClient) GetOrCreateCollection(ctx context.Context, name string, options ...chromav2.CreateCollectionOption) (chromav2.Collection, error) {
	if coll, ok := f.collections[name]; ok {
		return coll, nil
	}
	return f.CreateCollection(ctx, name, options...)
}

func (f *fakeChromaClient) DeleteCollection(ctx context.Context, name string, options ...chromav2.DeleteCollectionOption) error {
	delete(f.collections, name)
	return nil
}

func (f *fakeChromaClient) GetCollection(ctx context.Context, name string, opts ...chromav2.GetCollectionOption) (chromav2.Collection, error) {
	coll, ok := f.collections[name]
	if !ok {
		return nil, fmt.Errorf("collection %s not found", name)
	}
	return coll, nil
}

func (f *fakeChromaClient) CountCollections(ctx context.Context, opts ...chromav2.CountCollectionsOption) (int, error) {
	return len(f.collections), nil
}

func (f *fakeChromaClient) ListCollections(ctx context.Context, opts ...chromav2.ListCollectionsOption) ([]chromav2.Collection, error) {
	result := make([]chromav2.Collection, 0, len(f.collections))
	for _, coll := range f.collections {
		result = append(result, coll)
	}
	return result, nil
}

func (f *fakeChromaClient) Close() error {
	if f.closeErr != nil {
		return f.closeErr
	}
	return nil
}

// fakeChromaClientWithErrors 支持模拟错误的 ChromaDB 客户端
type fakeChromaClientWithErrors struct {
	*fakeChromaClient
	getCollectionErr error
	deleteCollErr    error
	createCollErr    error
	listCollErr      error
}

func (f *fakeChromaClientWithErrors) GetCollection(ctx context.Context, name string, opts ...chromav2.GetCollectionOption) (chromav2.Collection, error) {
	if f.getCollectionErr != nil {
		return nil, f.getCollectionErr
	}
	return f.fakeChromaClient.GetCollection(ctx, name, opts...)
}

func (f *fakeChromaClientWithErrors) DeleteCollection(ctx context.Context, name string, options ...chromav2.DeleteCollectionOption) error {
	if f.deleteCollErr != nil {
		return f.deleteCollErr
	}
	return f.fakeChromaClient.DeleteCollection(ctx, name, options...)
}

func (f *fakeChromaClientWithErrors) GetOrCreateCollection(ctx context.Context, name string, options ...chromav2.CreateCollectionOption) (chromav2.Collection, error) {
	if f.createCollErr != nil {
		return nil, f.createCollErr
	}
	return f.fakeChromaClient.GetOrCreateCollection(ctx, name, options...)
}

func (f *fakeChromaClientWithErrors) ListCollections(ctx context.Context, opts ...chromav2.ListCollectionsOption) ([]chromav2.Collection, error) {
	if f.listCollErr != nil {
		return nil, f.listCollErr
	}
	return f.fakeChromaClient.ListCollections(ctx, opts...)
}

// fakeChromaCollectionWithQuery 支持自定义查询结果的 fake 集合
type fakeChromaCollectionWithQuery struct {
	*fakeChromaCollection
	queryResult chromav2.QueryResult
	queryErr    error
	addErr      error
	deleteErr   error
	getResult   chromav2.GetResult
	getErr      error
}

func (f *fakeChromaCollectionWithQuery) Query(ctx context.Context, opts ...chromav2.QueryOption) (chromav2.QueryResult, error) {
	if f.queryErr != nil {
		return nil, f.queryErr
	}
	return f.queryResult, nil
}

func (f *fakeChromaCollectionWithQuery) Add(ctx context.Context, opts ...chromav2.AddOption) error {
	if f.addErr != nil {
		return f.addErr
	}
	return f.fakeChromaCollection.Add(ctx, opts...)
}

func (f *fakeChromaCollectionWithQuery) Delete(ctx context.Context, opts ...chromav2.DeleteOption) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	return f.fakeChromaCollection.Delete(ctx, opts...)
}

func (f *fakeChromaCollectionWithQuery) Get(ctx context.Context, opts ...chromav2.GetOption) (chromav2.GetResult, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	if f.getResult != nil {
		return f.getResult, nil
	}
	return f.fakeChromaCollection.Get(ctx, opts...)
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestNewChromaVectorStore 测试构造函数
func TestNewChromaVectorStore(t *testing.T) {
	s := NewChromaVectorStore("/tmp/chroma_test")
	if s.persistPath != "/tmp/chroma_test" {
		t.Errorf("persistPath = %v, want /tmp/chroma_test", s.persistPath)
	}
	if s.createClient == nil {
		t.Error("createClient 不应为 nil")
	}
	if s.collectionCache == nil {
		t.Error("collectionCache 不应为 nil")
	}
	if s.fieldMappingCache == nil {
		t.Error("fieldMappingCache 不应为 nil")
	}
}

// TestChromaVectorStore_Close 测试关闭客户端
func TestChromaVectorStore_Close(t *testing.T) {
	s := newChromaTestStore()
	fake := newFakeChromaClient()
	s.client = fake

	s.Close()
	if s.client != nil {
		t.Error("Close() 后 client 应为 nil")
	}
}

// TestChromaVectorStore_Close_未连接 测试未连接时关闭不 panic
func TestChromaVectorStore_Close_未连接(t *testing.T) {
	s := NewChromaVectorStore("/tmp/chroma_test")
	s.Close()
	if s.client != nil {
		t.Error("Close() 未连接时 client 应为 nil")
	}
}

// TestChromaVectorStore_CreateCollection 测试创建集合
func TestChromaVectorStore_CreateCollection(t *testing.T) {
	s := newChromaTestStore()
	ctx := context.Background()
	schema := createTestSchema()

	err := s.CreateCollection(ctx, "test_coll", schema, WithDistanceMetric("COSINE"))
	if err != nil {
		t.Fatalf("CreateCollection() error = %v", err)
	}

	// 验证集合已在缓存中
	s.mu.RLock()
	_, ok := s.collectionCache["test_coll"]
	s.mu.RUnlock()
	if !ok {
		t.Error("CreateCollection() 后集合应在缓存中")
	}

	// 验证字段映射已在缓存中
	s.mu.RLock()
	fm, ok := s.fieldMappingCache["test_coll"]
	s.mu.RUnlock()
	if !ok {
		t.Error("CreateCollection() 后字段映射应在缓存中")
	}
	if fm.PKField != "id" {
		t.Errorf("PKField = %v, want id", fm.PKField)
	}
	if fm.VectorField != "embedding" {
		t.Errorf("VectorField = %v, want embedding", fm.VectorField)
	}
	if fm.DistanceMetric != "COSINE" {
		t.Errorf("DistanceMetric = %v, want COSINE", fm.DistanceMetric)
	}
}

// TestChromaVectorStore_CreateCollection_默认距离度量 测试不指定距离度量时使用默认值
func TestChromaVectorStore_CreateCollection_默认距离度量(t *testing.T) {
	s := newChromaTestStore()
	ctx := context.Background()
	schema := createTestSchema()

	err := s.CreateCollection(ctx, "test_coll", schema)
	if err != nil {
		t.Fatalf("CreateCollection() error = %v", err)
	}

	s.mu.RLock()
	fm := s.fieldMappingCache["test_coll"]
	s.mu.RUnlock()
	if fm.DistanceMetric != defaultDistanceMetric {
		t.Errorf("DistanceMetric = %v, want %v", fm.DistanceMetric, defaultDistanceMetric)
	}
}

// TestChromaVectorStore_CreateCollection_无主键字段 测试 schema 缺少主键字段时返回错误
func TestChromaVectorStore_CreateCollection_无主键字段(t *testing.T) {
	s := newChromaTestStore()
	ctx := context.Background()

	// 没有主键字段的 schema
	vec, _ := NewFieldSchema("embedding", VectorDataTypeFloatVector, WithDim(128))
	text, _ := NewFieldSchema("text", VectorDataTypeVarchar)
	schema, _ := NewCollectionSchemaFromFields([]*FieldSchema{vec, text})

	err := s.CreateCollection(ctx, "bad_coll", schema)
	if err == nil {
		t.Error("CreateCollection() 无主键字段应返回错误")
	}
}

// TestChromaVectorStore_CreateCollection_无向量字段 测试 schema 缺少向量字段时返回错误
func TestChromaVectorStore_CreateCollection_无向量字段(t *testing.T) {
	s := newChromaTestStore()
	ctx := context.Background()

	pk, _ := NewFieldSchema("id", VectorDataTypeVarchar, WithPrimary())
	text, _ := NewFieldSchema("text", VectorDataTypeVarchar)
	schema, _ := NewCollectionSchemaFromFields([]*FieldSchema{pk, text})

	err := s.CreateCollection(ctx, "bad_coll", schema)
	if err == nil {
		t.Error("CreateCollection() 无向量字段应返回错误")
	}
}

// TestChromaVectorStore_CreateCollection_客户端创建失败 测试客户端创建失败
func TestChromaVectorStore_CreateCollection_客户端创建失败(t *testing.T) {
	s := NewChromaVectorStore("/tmp/chroma_test")
	s.createClient = func(persistPath string) (chromav2.Client, error) {
		return nil, fmt.Errorf("connection refused")
	}
	ctx := context.Background()
	schema := createTestSchema()

	err := s.CreateCollection(ctx, "test_coll", schema)
	if err == nil {
		t.Error("CreateCollection() 连接失败应返回错误")
	}
}

// TestChromaVectorStore_CreateCollection_创建失败 测试 ChromaDB 创建集合失败
func TestChromaVectorStore_CreateCollection_创建失败(t *testing.T) {
	s := newChromaTestStore()
	fakeBase := newFakeChromaClient()
	s.client = fakeBase

	errFake := &fakeChromaClientWithErrors{
		fakeChromaClient: fakeBase,
		createCollErr:    fmt.Errorf("create collection failed"),
	}
	s.client = errFake

	ctx := context.Background()
	schema := createTestSchema()
	err := s.CreateCollection(ctx, "test_coll", schema)
	if err == nil {
		t.Error("CreateCollection() 创建失败应返回错误")
	}
}

// TestChromaVectorStore_DeleteCollection 测试删除集合
func TestChromaVectorStore_DeleteCollection(t *testing.T) {
	s := newChromaTestStore()
	ctx := context.Background()
	schema := createTestSchema()

	_ = s.CreateCollection(ctx, "test_coll", schema)
	err := s.DeleteCollection(ctx, "test_coll")
	if err != nil {
		t.Fatalf("DeleteCollection() error = %v", err)
	}

	// 验证缓存已清除
	s.mu.RLock()
	_, collOK := s.collectionCache["test_coll"]
	_, fmOK := s.fieldMappingCache["test_coll"]
	s.mu.RUnlock()
	if collOK {
		t.Error("DeleteCollection() 后集合缓存应已清除")
	}
	if fmOK {
		t.Error("DeleteCollection() 后字段映射缓存应已清除")
	}
}

// TestChromaVectorStore_DeleteCollection_删除失败 测试 ChromaDB 删除集合失败
func TestChromaVectorStore_DeleteCollection_删除失败(t *testing.T) {
	s := newChromaTestStore()
	fakeBase := newFakeChromaClient()
	s.client = fakeBase
	_, _ = fakeBase.GetOrCreateCollection(context.Background(), "test_coll")

	errFake := &fakeChromaClientWithErrors{
		fakeChromaClient: fakeBase,
		deleteCollErr:    fmt.Errorf("delete collection failed"),
	}
	s.client = errFake

	ctx := context.Background()
	err := s.DeleteCollection(ctx, "test_coll")
	if err == nil {
		t.Error("DeleteCollection() 删除失败应返回错误")
	}
}

// TestChromaVectorStore_CollectionExists 测试集合是否存在
func TestChromaVectorStore_CollectionExists(t *testing.T) {
	s := newChromaTestStore()
	ctx := context.Background()
	schema := createTestSchema()
	_ = s.CreateCollection(ctx, "test_coll", schema)

	exists, err := s.CollectionExists(ctx, "test_coll")
	if err != nil {
		t.Fatalf("CollectionExists() error = %v", err)
	}
	if !exists {
		t.Error("集合应该存在")
	}

	exists, err = s.CollectionExists(ctx, "not_exist")
	if err != nil {
		t.Fatalf("CollectionExists() error = %v", err)
	}
	if exists {
		t.Error("集合不应该存在")
	}
}

// TestChromaVectorStore_CollectionExists_客户端错误 测试客户端错误时返回错误
func TestChromaVectorStore_CollectionExists_客户端错误(t *testing.T) {
	s := NewChromaVectorStore("/tmp/chroma_test")
	s.createClient = func(persistPath string) (chromav2.Client, error) {
		return nil, fmt.Errorf("connection refused")
	}
	ctx := context.Background()

	_, err := s.CollectionExists(ctx, "test_coll")
	if err == nil {
		t.Error("CollectionExists() 连接失败应返回错误")
	}
}

// TestChromaVectorStore_ListCollectionNames 测试列出集合名称
func TestChromaVectorStore_ListCollectionNames(t *testing.T) {
	s := newChromaTestStore()
	ctx := context.Background()
	schema := createTestSchema()
	_ = s.CreateCollection(ctx, "coll1", schema)
	_ = s.CreateCollection(ctx, "coll2", schema)

	names, err := s.ListCollectionNames(ctx)
	if err != nil {
		t.Fatalf("ListCollectionNames() error = %v", err)
	}
	if len(names) != 2 {
		t.Errorf("ListCollectionNames() 返回 %d 个, want 2", len(names))
	}
}

// TestChromaVectorStore_ListCollectionNames_客户端错误 测试客户端错误
func TestChromaVectorStore_ListCollectionNames_客户端错误(t *testing.T) {
	s := NewChromaVectorStore("/tmp/chroma_test")
	s.createClient = func(persistPath string) (chromav2.Client, error) {
		return nil, fmt.Errorf("connection refused")
	}
	ctx := context.Background()

	_, err := s.ListCollectionNames(ctx)
	if err == nil {
		t.Error("ListCollectionNames() 连接失败应返回错误")
	}
}

// TestChromaVectorStore_AddDocs_空文档 测试空文档列表
func TestChromaVectorStore_AddDocs_空文档(t *testing.T) {
	s := newChromaTestStore()
	ctx := context.Background()

	err := s.AddDocs(ctx, "test_coll", nil)
	if err != nil {
		t.Errorf("AddDocs(nil) error = %v, want nil", err)
	}
}

// TestChromaVectorStore_AddDocs 测试添加文档
func TestChromaVectorStore_AddDocs(t *testing.T) {
	s := newChromaTestStore()
	ctx := context.Background()
	schema := createTestSchema()
	_ = s.CreateCollection(ctx, "test_coll", schema)

	docs := []map[string]any{
		{"id": "doc1", "text": "hello", "embedding": []float32{0.1, 0.2, 0.3}},
		{"id": "doc2", "text": "world", "embedding": []float32{0.4, 0.5, 0.6}},
	}
	err := s.AddDocs(ctx, "test_coll", docs, WithBatchSize(10))
	if err != nil {
		t.Fatalf("AddDocs() error = %v", err)
	}
}

// TestChromaVectorStore_AddDocs_缺少主键 测试文档缺少主键字段
func TestChromaVectorStore_AddDocs_缺少主键(t *testing.T) {
	s := newChromaTestStore()
	ctx := context.Background()
	schema := createTestSchema()
	_ = s.CreateCollection(ctx, "test_coll", schema)

	docs := []map[string]any{
		{"text": "hello", "embedding": []float32{0.1, 0.2, 0.3}}, // 缺少 id
	}
	err := s.AddDocs(ctx, "test_coll", docs)
	if err == nil {
		t.Error("AddDocs() 缺少主键字段应返回错误")
	}
}

// TestChromaVectorStore_AddDocs_集合不存在 测试向不存在的集合添加文档
func TestChromaVectorStore_AddDocs_集合不存在(t *testing.T) {
	s := newChromaTestStore()
	ctx := context.Background()

	docs := []map[string]any{{"id": "1", "text": "hello"}}
	err := s.AddDocs(ctx, "not_exist", docs)
	if err == nil {
		t.Error("AddDocs() 集合不存在应返回错误")
	}
}

// TestChromaVectorStore_AddDocs_插入失败 测试 ChromaDB 插入失败
func TestChromaVectorStore_AddDocs_插入失败(t *testing.T) {
	s := newChromaTestStore()
	ctx := context.Background()
	schema := createTestSchema()
	_ = s.CreateCollection(ctx, "test_coll", schema)

	// 替换为会报错的 fake 集合
	fakeColl, ok := s.collectionCache["test_coll"].(*fakeChromaCollection)
	if !ok {
		t.Skip("需要 fakeChromaCollection 类型")
	}
	s.collectionCache["test_coll"] = &fakeChromaCollectionWithQuery{
		fakeChromaCollection: fakeColl,
		addErr:               fmt.Errorf("add failed"),
	}

	docs := []map[string]any{{"id": "1", "text": "hello", "embedding": []float32{0.1}}}
	err := s.AddDocs(ctx, "test_coll", docs)
	if err == nil {
		t.Error("AddDocs() 插入失败应返回错误")
	}
}

// TestChromaVectorStore_Search 测试向量搜索
func TestChromaVectorStore_Search(t *testing.T) {
	s := newChromaTestStore()
	ctx := context.Background()
	schema := createTestSchema()
	_ = s.CreateCollection(ctx, "test_coll", schema)

	results, err := s.Search(ctx, "test_coll", []float64{0.1, 0.2, 0.3}, "embedding", 5, nil)
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	// fake 返回空结果
	if results == nil {
		t.Error("Search() 不应返回 nil")
	}
}

// TestChromaVectorStore_Search_默认TopK 测试 TopK 为 0 时使用默认值
func TestChromaVectorStore_Search_默认TopK(t *testing.T) {
	s := newChromaTestStore()
	ctx := context.Background()
	schema := createTestSchema()
	_ = s.CreateCollection(ctx, "test_coll", schema)

	results, err := s.Search(ctx, "test_coll", []float64{0.1, 0.2}, "embedding", 0, nil)
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	_ = results
}

// TestChromaVectorStore_Search_集合不存在 测试搜索不存在的集合
func TestChromaVectorStore_Search_集合不存在(t *testing.T) {
	s := newChromaTestStore()
	ctx := context.Background()

	_, err := s.Search(ctx, "not_exist", []float64{0.1, 0.2}, "embedding", 5, nil)
	if err == nil {
		t.Error("Search() 集合不存在应返回错误")
	}
}

// TestChromaVectorStore_Search_搜索失败 测试 ChromaDB 搜索失败
func TestChromaVectorStore_Search_搜索失败(t *testing.T) {
	s := newChromaTestStore()
	ctx := context.Background()
	schema := createTestSchema()
	_ = s.CreateCollection(ctx, "test_coll", schema)

	// 替换为会报错的 fake 集合
	fakeColl, ok := s.collectionCache["test_coll"].(*fakeChromaCollection)
	if !ok {
		t.Skip("需要 fakeChromaCollection 类型")
	}
	s.collectionCache["test_coll"] = &fakeChromaCollectionWithQuery{
		fakeChromaCollection: fakeColl,
		queryErr:             fmt.Errorf("query failed"),
	}

	_, err := s.Search(ctx, "test_coll", []float64{0.1, 0.2}, "embedding", 5, nil)
	if err == nil {
		t.Error("Search() 搜索失败应返回错误")
	}
}

// TestChromaVectorStore_Search_有结果 测试搜索返回结果
func TestChromaVectorStore_Search_有结果(t *testing.T) {
	s := newChromaTestStore()
	ctx := context.Background()
	schema := createTestSchema()
	_ = s.CreateCollection(ctx, "test_coll", schema)

	// 构造搜索结果
	queryResult := &chromav2.QueryResultImpl{
		IDLists: []chromav2.DocumentIDs{
			{chromav2.DocumentID("doc1"), chromav2.DocumentID("doc2")},
		},
		DocumentsLists: []chromav2.Documents{
			{chromav2.NewTextDocument("hello"), chromav2.NewTextDocument("world")},
		},
		MetadatasLists: []chromav2.DocumentMetadatas{
			{nil, nil},
		},
		DistancesLists: []embeddings.Distances{
			{0.1, 0.5},
		},
	}

	// 替换为返回结果的 fake 集合
	fakeColl, ok := s.collectionCache["test_coll"].(*fakeChromaCollection)
	if !ok {
		t.Skip("需要 fakeChromaCollection 类型")
	}
	s.collectionCache["test_coll"] = &fakeChromaCollectionWithQuery{
		fakeChromaCollection: fakeColl,
		queryResult:          queryResult,
	}

	results, err := s.Search(ctx, "test_coll", []float64{0.1, 0.2}, "embedding", 5, nil)
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("Search() 返回 %d 结果, want 2", len(results))
	}
	// 验证第一个结果的 score > 0（COSINE 距离转换）
	if results[0].Score <= 0 {
		t.Errorf("Search() 分数 %v 应为正值", results[0].Score)
	}
	// 验证主键字段存在
	if _, ok := results[0].Fields["id"]; !ok {
		t.Error("Search() 结果应包含 id 字段")
	}
}

// TestChromaVectorStore_Search_带过滤条件 测试搜索带过滤条件
func TestChromaVectorStore_Search_带过滤条件(t *testing.T) {
	s := newChromaTestStore()
	ctx := context.Background()
	schema := createTestSchema()
	_ = s.CreateCollection(ctx, "test_coll", schema)

	// 使用带查询结果的 fake
	fakeColl, ok := s.collectionCache["test_coll"].(*fakeChromaCollection)
	if !ok {
		t.Skip("需要 fakeChromaCollection 类型")
	}
	queryResult := &chromav2.QueryResultImpl{
		IDLists:        []chromav2.DocumentIDs{},
		DocumentsLists: []chromav2.Documents{},
		MetadatasLists: []chromav2.DocumentMetadatas{},
		DistancesLists: []embeddings.Distances{},
	}
	s.collectionCache["test_coll"] = &fakeChromaCollectionWithQuery{
		fakeChromaCollection: fakeColl,
		queryResult:          queryResult,
	}

	results, err := s.Search(ctx, "test_coll", []float64{0.1, 0.2}, "embedding", 5, map[string]any{"status": "active"})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	_ = results
}

// TestChromaVectorStore_DeleteDocsByIDs_空 测试空 ID 列表
func TestChromaVectorStore_DeleteDocsByIDs_空(t *testing.T) {
	s := newChromaTestStore()
	ctx := context.Background()

	err := s.DeleteDocsByIDs(ctx, "test_coll", nil)
	if err != nil {
		t.Errorf("DeleteDocsByIDs(nil) error = %v, want nil", err)
	}
}

// TestChromaVectorStore_DeleteDocsByIDs 测试按 ID 删除文档
func TestChromaVectorStore_DeleteDocsByIDs(t *testing.T) {
	s := newChromaTestStore()
	ctx := context.Background()
	schema := createTestSchema()
	_ = s.CreateCollection(ctx, "test_coll", schema)

	err := s.DeleteDocsByIDs(ctx, "test_coll", []string{"id1", "id2"})
	if err != nil {
		t.Fatalf("DeleteDocsByIDs() error = %v", err)
	}
}

// TestChromaVectorStore_DeleteDocsByIDs_删除失败 测试删除失败
func TestChromaVectorStore_DeleteDocsByIDs_删除失败(t *testing.T) {
	s := newChromaTestStore()
	ctx := context.Background()
	schema := createTestSchema()
	_ = s.CreateCollection(ctx, "test_coll", schema)

	fakeColl, ok := s.collectionCache["test_coll"].(*fakeChromaCollection)
	if !ok {
		t.Skip("需要 fakeChromaCollection 类型")
	}
	s.collectionCache["test_coll"] = &fakeChromaCollectionWithQuery{
		fakeChromaCollection: fakeColl,
		deleteErr:            fmt.Errorf("delete failed"),
	}

	err := s.DeleteDocsByIDs(ctx, "test_coll", []string{"id1"})
	if err == nil {
		t.Error("DeleteDocsByIDs() 删除失败应返回错误")
	}
}

// TestChromaVectorStore_DeleteDocsByFilters_空 测试空过滤条件
func TestChromaVectorStore_DeleteDocsByFilters_空(t *testing.T) {
	s := newChromaTestStore()
	ctx := context.Background()

	err := s.DeleteDocsByFilters(ctx, "test_coll", nil)
	if err != nil {
		t.Errorf("DeleteDocsByFilters(nil) error = %v, want nil", err)
	}

	err = s.DeleteDocsByFilters(ctx, "test_coll", map[string]any{})
	if err != nil {
		t.Errorf("DeleteDocsByFilters(empty) error = %v, want nil", err)
	}
}

// TestChromaVectorStore_DeleteDocsByFilters 测试按过滤条件删除文档
func TestChromaVectorStore_DeleteDocsByFilters(t *testing.T) {
	s := newChromaTestStore()
	ctx := context.Background()
	schema := createTestSchema()
	_ = s.CreateCollection(ctx, "test_coll", schema)

	err := s.DeleteDocsByFilters(ctx, "test_coll", map[string]any{"status": "deleted"})
	if err != nil {
		t.Fatalf("DeleteDocsByFilters() error = %v", err)
	}
}

// TestChromaVectorStore_DeleteDocsByFilters_删除失败 测试删除失败
func TestChromaVectorStore_DeleteDocsByFilters_删除失败(t *testing.T) {
	s := newChromaTestStore()
	ctx := context.Background()
	schema := createTestSchema()
	_ = s.CreateCollection(ctx, "test_coll", schema)

	fakeColl, ok := s.collectionCache["test_coll"].(*fakeChromaCollection)
	if !ok {
		t.Skip("需要 fakeChromaCollection 类型")
	}
	s.collectionCache["test_coll"] = &fakeChromaCollectionWithQuery{
		fakeChromaCollection: fakeColl,
		deleteErr:            fmt.Errorf("delete failed"),
	}

	err := s.DeleteDocsByFilters(ctx, "test_coll", map[string]any{"status": "deleted"})
	if err == nil {
		t.Error("DeleteDocsByFilters() 删除失败应返回错误")
	}
}

// TestChromaVectorStore_GetSchema 测试获取 Schema
func TestChromaVectorStore_GetSchema(t *testing.T) {
	s := newChromaTestStore()
	ctx := context.Background()
	schema := createTestSchema()
	_ = s.CreateCollection(ctx, "test_coll", schema)

	gotSchema, err := s.GetSchema(ctx, "test_coll")
	if err != nil {
		t.Fatalf("GetSchema() error = %v", err)
	}
	if gotSchema == nil {
		t.Fatal("GetSchema() 不应返回 nil")
	}
}

// TestChromaVectorStore_GetSchema_集合不存在 测试获取不存在的集合 Schema
func TestChromaVectorStore_GetSchema_集合不存在(t *testing.T) {
	s := newChromaTestStore()
	ctx := context.Background()

	_, err := s.GetSchema(ctx, "not_exist")
	if err == nil {
		t.Error("GetSchema() 不存在的集合应返回错误")
	}
}

// TestChromaVectorStore_UpdateSchema_预留 测试 UpdateSchema 预留方法
func TestChromaVectorStore_UpdateSchema_预留(t *testing.T) {
	s := newChromaTestStore()
	ctx := context.Background()

	err := s.UpdateSchema(ctx, "test_coll", []any{})
	if err == nil {
		t.Error("UpdateSchema() 预留方法应返回错误")
	}
}

// TestChromaVectorStore_GetCollectionMetadata 测试获取集合元数据
func TestChromaVectorStore_GetCollectionMetadata(t *testing.T) {
	s := newChromaTestStore()
	ctx := context.Background()
	schema := createTestSchema()
	_ = s.CreateCollection(ctx, "test_coll", schema)

	meta, err := s.GetCollectionMetadata(ctx, "test_coll")
	if err != nil {
		t.Fatalf("GetCollectionMetadata() error = %v", err)
	}
	if meta == nil {
		t.Fatal("GetCollectionMetadata() 不应返回 nil")
	}
}

// TestChromaVectorStore_GetCollectionMetadata_集合不存在 测试集合不存在
func TestChromaVectorStore_GetCollectionMetadata_集合不存在(t *testing.T) {
	s := newChromaTestStore()
	ctx := context.Background()

	_, err := s.GetCollectionMetadata(ctx, "not_exist")
	if err == nil {
		t.Error("GetCollectionMetadata() 集合不存在应返回错误")
	}
}

// TestChromaVectorStore_UpdateCollectionMetadata_空元数据 测试空元数据
func TestChromaVectorStore_UpdateCollectionMetadata_空元数据(t *testing.T) {
	s := newChromaTestStore()
	ctx := context.Background()

	err := s.UpdateCollectionMetadata(ctx, "test_coll", map[string]any{})
	if err != nil {
		t.Errorf("UpdateCollectionMetadata(empty) error = %v, want nil", err)
	}
}

// TestChromaVectorStore_UpdateCollectionMetadata 测试更新集合元数据
func TestChromaVectorStore_UpdateCollectionMetadata(t *testing.T) {
	s := newChromaTestStore()
	ctx := context.Background()
	schema := createTestSchema()
	_ = s.CreateCollection(ctx, "test_coll", schema)

	err := s.UpdateCollectionMetadata(ctx, "test_coll", map[string]any{"custom_key": "custom_value"})
	if err != nil {
		t.Fatalf("UpdateCollectionMetadata() error = %v", err)
	}

	// 验证字段映射缓存中的 distance_metric 更新
	s.mu.RLock()
	fm, ok := s.fieldMappingCache["test_coll"]
	s.mu.RUnlock()
	if ok && fm.DistanceMetric != "COSINE" {
		t.Errorf("DistanceMetric = %v, want COSINE", fm.DistanceMetric)
	}
}

// TestChromaVectorStore_UpdateCollectionMetadata_更新距离度量 测试更新距离度量
func TestChromaVectorStore_UpdateCollectionMetadata_更新距离度量(t *testing.T) {
	s := newChromaTestStore()
	ctx := context.Background()
	schema := createTestSchema()
	_ = s.CreateCollection(ctx, "test_coll", schema, WithDistanceMetric("COSINE"))

	err := s.UpdateCollectionMetadata(ctx, "test_coll", map[string]any{
		chromaMetadataKeyDistanceMetric: "L2",
	})
	if err != nil {
		t.Fatalf("UpdateCollectionMetadata() error = %v", err)
	}

	// 验证字段映射缓存中的 distance_metric 已更新
	s.mu.RLock()
	fm := s.fieldMappingCache["test_coll"]
	s.mu.RUnlock()
	if fm.DistanceMetric != "L2" {
		t.Errorf("DistanceMetric = %v, want L2", fm.DistanceMetric)
	}
}

// TestChromaVectorStore_UpdateCollectionMetadata_集合不存在 测试集合不存在
func TestChromaVectorStore_UpdateCollectionMetadata_集合不存在(t *testing.T) {
	s := newChromaTestStore()
	ctx := context.Background()

	err := s.UpdateCollectionMetadata(ctx, "not_exist", map[string]any{"key": "val"})
	if err == nil {
		t.Error("UpdateCollectionMetadata() 集合不存在应返回错误")
	}
}

// TestChromaVectorStore_GetAllDocuments 测试获取所有文档
func TestChromaVectorStore_GetAllDocuments(t *testing.T) {
	s := newChromaTestStore()
	ctx := context.Background()
	schema := createTestSchema()
	_ = s.CreateCollection(ctx, "test_coll", schema)

	docs, err := s.GetAllDocuments(ctx, "test_coll")
	if err != nil {
		t.Fatalf("GetAllDocuments() error = %v", err)
	}
	// fake 集合中没有文档，应返回空列表
	if docs == nil {
		t.Error("GetAllDocuments() 不应返回 nil")
	}
}

// TestChromaVectorStore_GetAllDocuments_集合不存在 测试集合不存在
func TestChromaVectorStore_GetAllDocuments_集合不存在(t *testing.T) {
	s := newChromaTestStore()
	ctx := context.Background()

	_, err := s.GetAllDocuments(ctx, "not_exist")
	if err == nil {
		t.Error("GetAllDocuments() 集合不存在应返回错误")
	}
}

// TestChromaVectorStore_GetAllDocuments_有文档 测试获取有文档的集合
func TestChromaVectorStore_GetAllDocuments_有文档(t *testing.T) {
	s := newChromaTestStore()
	ctx := context.Background()
	schema := createTestSchema()
	_ = s.CreateCollection(ctx, "test_coll", schema)

	// 先添加文档
	docs := []map[string]any{
		{"id": "doc1", "text": "hello", "embedding": []float32{0.1, 0.2}},
		{"id": "doc2", "text": "world", "embedding": []float32{0.3, 0.4}},
	}
	_ = s.AddDocs(ctx, "test_coll", docs)

	// 获取所有文档
	allDocs, err := s.GetAllDocuments(ctx, "test_coll")
	if err != nil {
		t.Fatalf("GetAllDocuments() error = %v", err)
	}
	if len(allDocs) != 2 {
		t.Errorf("GetAllDocuments() 返回 %d 文档, want 2", len(allDocs))
	}
}

// TestChromaVectorStore_GetClient_惰性创建 测试惰性创建客户端
func TestChromaVectorStore_GetClient_惰性创建(t *testing.T) {
	s := NewChromaVectorStore("/tmp/chroma_test")
	fake := newFakeChromaClient()
	s.createClient = func(persistPath string) (chromav2.Client, error) {
		return fake, nil
	}

	c, err := s.getClient()
	if err != nil {
		t.Fatalf("getClient() error = %v", err)
	}
	if c == nil {
		t.Error("getClient() 不应返回 nil")
	}
	if s.client == nil {
		t.Error("getClient() 后 s.client 不应为 nil")
	}
}

// TestChromaVectorStore_GetClient_缓存命中 测试客户端缓存命中
func TestChromaVectorStore_GetClient_缓存命中(t *testing.T) {
	s := newChromaTestStore()

	// 第二次调用应返回缓存的客户端
	c, err := s.getClient()
	if err != nil {
		t.Fatalf("getClient() error = %v", err)
	}
	if c == nil {
		t.Error("getClient() 缓存命中不应返回 nil")
	}
}

// TestChromaVectorStore_GetClient_连接失败 测试连接失败
func TestChromaVectorStore_GetClient_连接失败(t *testing.T) {
	s := NewChromaVectorStore("/tmp/chroma_test")
	s.createClient = func(persistPath string) (chromav2.Client, error) {
		return nil, fmt.Errorf("connection refused")
	}

	_, err := s.getClient()
	if err == nil {
		t.Error("getClient() 连接失败应返回错误")
	}
}

// ──────────────────────────── 辅助函数测试 ────────────────────────────

// TestMapToChromaDistanceMetric 测试距离度量映射
func TestMapToChromaDistanceMetric(t *testing.T) {
	tests := []struct {
		name   string
		metric string
		want   embeddings.DistanceMetric
	}{
		{"L2", "L2", embeddings.L2},
		{"IP", "IP", embeddings.IP},
		{"COSINE", "COSINE", embeddings.COSINE},
		{"默认", "", embeddings.COSINE},
		{"小写", "cosine", embeddings.COSINE},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mapToChromaDistanceMetric(tt.metric); got != tt.want {
				t.Errorf("mapToChromaDistanceMetric() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestGetScoreConverter 测试分数转换函数获取
func TestGetScoreConverter(t *testing.T) {
	// COSINE
	converter := getScoreConverter("COSINE")
	if converter == nil {
		t.Error("getScoreConverter(COSINE) 不应返回 nil")
	}

	// L2
	converter = getScoreConverter("L2")
	if converter == nil {
		t.Error("getScoreConverter(L2) 不应返回 nil")
	}

	// IP
	converter = getScoreConverter("IP")
	if converter == nil {
		t.Error("getScoreConverter(IP) 不应返回 nil")
	}

	// 默认
	converter = getScoreConverter("UNKNOWN")
	if converter == nil {
		t.Error("getScoreConverter(UNKNOWN) 不应返回 nil")
	}
}

// TestBuildChromaWhereFilter 测试构建 ChromaDB WhereFilter
func TestBuildChromaWhereFilter(t *testing.T) {
	// 空过滤器
	if got := buildChromaWhereFilter(nil); got != nil {
		t.Error("buildChromaWhereFilter(nil) 应返回 nil")
	}
	if got := buildChromaWhereFilter(map[string]any{}); got != nil {
		t.Error("buildChromaWhereFilter(empty) 应返回 nil")
	}

	// 单条件 - 字符串
	filter := buildChromaWhereFilter(map[string]any{"name": "test"})
	if filter == nil {
		t.Error("buildChromaWhereFilter(string) 不应返回 nil")
	}

	// 单条件 - 整数
	filter = buildChromaWhereFilter(map[string]any{"age": 30})
	if filter == nil {
		t.Error("buildChromaWhereFilter(int) 不应返回 nil")
	}

	// 单条件 - 浮点数
	filter = buildChromaWhereFilter(map[string]any{"score": float64(0.95)})
	if filter == nil {
		t.Error("buildChromaWhereFilter(float64) 不应返回 nil")
	}

	// 单条件 - 布尔
	filter = buildChromaWhereFilter(map[string]any{"active": true})
	if filter == nil {
		t.Error("buildChromaWhereFilter(bool) 不应返回 nil")
	}

	// 多条件 - 应使用 And
	filter = buildChromaWhereFilter(map[string]any{"name": "test", "age": 30})
	if filter == nil {
		t.Error("buildChromaWhereFilter(multi) 不应返回 nil")
	}
}

// TestBuildChromaWhereFilter_不支持的类型 测试不支持的过滤值类型
func TestBuildChromaWhereFilter_不支持的类型(t *testing.T) {
	// 不支持的类型应返回 nil
	filter := buildChromaWhereFilter(map[string]any{"data": []string{"a", "b"}})
	if filter != nil {
		t.Error("buildChromaWhereFilter(unsupported type) 应返回 nil")
	}
}

// TestChromaWhereFilter 测试 chromaWhereFilter 适配器
func TestChromaWhereFilter(t *testing.T) {
	// nil clause
	f := &chromaWhereFilter{clause: nil}
	if f.String() != "" {
		t.Errorf("String() with nil clause = %v, want empty", f.String())
	}
	if f.Validate() != nil {
		t.Errorf("Validate() with nil clause should return nil, got %v", f.Validate())
	}
	b, err := f.MarshalJSON()
	if err != nil {
		t.Errorf("MarshalJSON() with nil clause error = %v", err)
	}
	if string(b) != "null" {
		t.Errorf("MarshalJSON() with nil clause = %v, want null", string(b))
	}

	// 有 clause 的情况
	clause := chromav2.EqString("key", "value")
	f = &chromaWhereFilter{clause: clause}
	// String() 的具体格式由 SDK 决定，只需验证不 panic
	_ = f.String()
	if f.Validate() != nil {
		t.Errorf("Validate() with clause error = %v", f.Validate())
	}
	b, err = f.MarshalJSON()
	if err != nil {
		t.Errorf("MarshalJSON() with clause error = %v", err)
	}
	// 有 clause 时 MarshalJSON 不应返回 null（除非 clause 本身序列化为 null）
	if string(b) == "null" {
		t.Log("MarshalJSON() with clause 返回 null，SDK 行为可能有变化")
	}
}

// TestToFloat32Slice 测试向量类型转换
func TestToFloat32Slice(t *testing.T) {
	// []float32
	emb := toFloat32Slice([]float32{0.1, 0.2, 0.3})
	if emb == nil {
		t.Error("toFloat32Slice([]float32) 不应返回 nil")
	}

	// []float64
	emb = toFloat32Slice([]float64{0.1, 0.2, 0.3})
	if emb == nil {
		t.Error("toFloat32Slice([]float64) 不应返回 nil")
	}

	// []any
	emb = toFloat32Slice([]any{float64(0.1), float64(0.2)})
	if emb == nil {
		t.Error("toFloat32Slice([]any) 不应返回 nil")
	}

	// 空 []any
	emb = toFloat32Slice([]any{})
	if emb != nil {
		t.Error("toFloat32Slice(empty []any) 应返回 nil")
	}

	// 不支持的类型
	emb = toFloat32Slice("not a vector")
	if emb != nil {
		t.Error("toFloat32Slice(string) 应返回 nil")
	}
}

// TestMetaKeys 测试 metaKeys 函数
func TestMetaKeys(t *testing.T) {
	// 使用 DocumentMetadataImpl
	meta := chromav2.NewMetadataFromMap(map[string]interface{}{
		"key1": "value1",
		"key2": 42,
	})
	keys := metaKeys(meta)
	// NewMetadataFromMap 返回的类型可能不是 *DocumentMetadataImpl
	// 这里主要验证函数不会 panic
	_ = keys
}

// TestChromaFieldMappingJSON 测试字段映射的 JSON 序列化/反序列化
func TestChromaFieldMappingJSON(t *testing.T) {
	fm := &chromaFieldMapping{
		PKField:          "id",
		VectorField:      "embedding",
		DistanceMetric:   "COSINE",
		TextFieldMapping: map[string]bool{"text": true, "content": true},
	}

	data, err := json.Marshal(fm)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	var fm2 chromaFieldMapping
	err = json.Unmarshal(data, &fm2)
	if err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if fm2.PKField != "id" {
		t.Errorf("PKField = %v, want id", fm2.PKField)
	}
	if fm2.VectorField != "embedding" {
		t.Errorf("VectorField = %v, want embedding", fm2.VectorField)
	}
	if fm2.DistanceMetric != "COSINE" {
		t.Errorf("DistanceMetric = %v, want COSINE", fm2.DistanceMetric)
	}
	if len(fm2.TextFieldMapping) != 2 {
		t.Errorf("TextFieldMapping len = %v, want 2", len(fm2.TextFieldMapping))
	}
}

// TestBuildChromaWhereFilter_int64 测试 int64 类型过滤
func TestBuildChromaWhereFilter_int64(t *testing.T) {
	filter := buildChromaWhereFilter(map[string]any{"count": int64(100)})
	if filter == nil {
		t.Error("buildChromaWhereFilter(int64) 不应返回 nil")
	}
}

// TestBuildChromaWhereFilter_float32 测试 float32 类型过滤
func TestBuildChromaWhereFilter_float32(t *testing.T) {
	filter := buildChromaWhereFilter(map[string]any{"score": float32(0.95)})
	if filter == nil {
		t.Error("buildChromaWhereFilter(float32) 不应返回 nil")
	}
}

// TestChromaVectorStore_AddDocs_float64向量 测试 float64 类型向量
func TestChromaVectorStore_AddDocs_float64向量(t *testing.T) {
	s := newChromaTestStore()
	ctx := context.Background()
	schema := createTestSchema()
	_ = s.CreateCollection(ctx, "test_coll", schema)

	docs := []map[string]any{
		{"id": "doc1", "text": "hello", "embedding": []float64{0.1, 0.2, 0.3}},
	}
	err := s.AddDocs(ctx, "test_coll", docs)
	if err != nil {
		t.Fatalf("AddDocs() with float64 vectors error = %v", err)
	}
}

// TestChromaVectorStore_AddDocs_批量大小 测试自定义批量大小
func TestChromaVectorStore_AddDocs_批量大小(t *testing.T) {
	s := newChromaTestStore()
	ctx := context.Background()
	schema := createTestSchema()
	_ = s.CreateCollection(ctx, "test_coll", schema)

	// 5 个文档，批量大小 2
	docs := []map[string]any{
		{"id": "1", "text": "a", "embedding": []float32{0.1}},
		{"id": "2", "text": "b", "embedding": []float32{0.2}},
		{"id": "3", "text": "c", "embedding": []float32{0.3}},
		{"id": "4", "text": "d", "embedding": []float32{0.4}},
		{"id": "5", "text": "e", "embedding": []float32{0.5}},
	}
	err := s.AddDocs(ctx, "test_coll", docs, WithBatchSize(2))
	if err != nil {
		t.Fatalf("AddDocs() with batch size 2 error = %v", err)
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// newChromaTestStore 创建带 fake 客户端的 ChromaVectorStore
func newChromaTestStore() *ChromaVectorStore {
	s := NewChromaVectorStore("/tmp/chroma_test")
	fake := newFakeChromaClient()
	s.client = fake
	s.createClient = func(persistPath string) (chromav2.Client, error) {
		return fake, nil
	}
	return s
}
