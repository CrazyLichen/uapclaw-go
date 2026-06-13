package milvus

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/milvus-io/milvus/client/v2/column"
	"github.com/milvus-io/milvus/client/v2/entity"
	milvusclient "github.com/milvus-io/milvus/client/v2/milvusclient"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/embedding"
	graph "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/graph"
)

// ──────────────────────────── 结构体 ────────────────────────────

// fakeGraphStoreClient 用于 MilvusGraphStore 测试的完整 Milvus 客户端模拟
type fakeGraphStoreClient struct {
	mu            sync.RWMutex
	collections   map[string]bool
	insertCount   int
	upsertCount   int
	deleteCount   int
	queryResult   milvusclient.ResultSet
	queryErr      error
	searchResults []milvusclient.ResultSet
	searchErr     error
	closeErr      error
	loadErr       error
	flushErr      error
	createCollErr error
	dropCollErr   error
	hasCollErr    error
}

func newFakeGraphStoreClient() *fakeGraphStoreClient {
	return &fakeGraphStoreClient{
		collections: make(map[string]bool),
	}
}

func (f *fakeGraphStoreClient) CreateCollection(ctx context.Context, option milvusclient.CreateCollectionOption, callOptions ...interface{}) error {
	if f.createCollErr != nil {
		return f.createCollErr
	}
	name := option.Request().GetCollectionName()
	f.mu.Lock()
	f.collections[name] = true
	f.mu.Unlock()
	return nil
}

func (f *fakeGraphStoreClient) DropCollection(ctx context.Context, option milvusclient.DropCollectionOption, callOptions ...interface{}) error {
	if f.dropCollErr != nil {
		return f.dropCollErr
	}
	name := option.Request().GetCollectionName()
	f.mu.Lock()
	delete(f.collections, name)
	f.mu.Unlock()
	return nil
}

func (f *fakeGraphStoreClient) HasCollection(ctx context.Context, option milvusclient.HasCollectionOption, callOptions ...interface{}) (bool, error) {
	if f.hasCollErr != nil {
		return false, f.hasCollErr
	}
	name := option.Request().GetCollectionName()
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.collections[name], nil
}

func (f *fakeGraphStoreClient) DescribeCollection(ctx context.Context, option milvusclient.DescribeCollectionOption, callOptions ...interface{}) (*entity.Collection, error) {
	return nil, fmt.Errorf("not implemented")
}

func (f *fakeGraphStoreClient) Insert(ctx context.Context, option milvusclient.InsertOption, callOptions ...interface{}) (milvusclient.InsertResult, error) {
	f.mu.Lock()
	f.insertCount++
	f.mu.Unlock()
	return milvusclient.InsertResult{}, nil
}

func (f *fakeGraphStoreClient) Upsert(ctx context.Context, option milvusclient.UpsertOption, callOptions ...interface{}) (milvusclient.UpsertResult, error) {
	f.mu.Lock()
	f.upsertCount++
	f.mu.Unlock()
	return milvusclient.UpsertResult{}, nil
}

func (f *fakeGraphStoreClient) Search(ctx context.Context, option milvusclient.SearchOption, callOptions ...interface{}) ([]milvusclient.ResultSet, error) {
	return f.searchResults, f.searchErr
}

func (f *fakeGraphStoreClient) HybridSearch(ctx context.Context, option milvusclient.HybridSearchOption, callOptions ...interface{}) ([]milvusclient.ResultSet, error) {
	return f.searchResults, f.searchErr
}

func (f *fakeGraphStoreClient) Query(ctx context.Context, option milvusclient.QueryOption, callOptions ...interface{}) (milvusclient.ResultSet, error) {
	return f.queryResult, f.queryErr
}

func (f *fakeGraphStoreClient) Delete(ctx context.Context, option milvusclient.DeleteOption, callOptions ...interface{}) (milvusclient.DeleteResult, error) {
	f.mu.Lock()
	f.deleteCount++
	f.mu.Unlock()
	return milvusclient.DeleteResult{}, nil
}

func (f *fakeGraphStoreClient) ListCollections(ctx context.Context, option milvusclient.ListCollectionOption, callOptions ...interface{}) ([]string, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	names := make([]string, 0, len(f.collections))
	for name := range f.collections {
		names = append(names, name)
	}
	return names, nil
}

func (f *fakeGraphStoreClient) LoadCollection(ctx context.Context, option milvusclient.LoadCollectionOption, callOptions ...interface{}) error {
	return f.loadErr
}

func (f *fakeGraphStoreClient) Flush(ctx context.Context, option milvusclient.FlushOption, callOptions ...interface{}) error {
	return f.flushErr
}

func (f *fakeGraphStoreClient) CreateIndex(ctx context.Context, option milvusclient.CreateIndexOption, callOptions ...interface{}) error {
	return nil
}

func (f *fakeGraphStoreClient) Close(ctx context.Context) error {
	return f.closeErr
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestNewMilvusGraphStore(t *testing.T) {
	cfg := graph.NewGraphConfig("localhost:19530")
	cfg.Backend = "milvus"
	s := NewMilvusGraphStore(cfg)
	if s == nil {
		t.Fatal("NewMilvusGraphStore() 返回 nil")
	}
	if s.Config() != cfg {
		t.Error("Config() 应返回传入配置")
	}
}

func TestMilvusGraphStore_Config(t *testing.T) {
	cfg := graph.NewGraphConfig("localhost:19530")
	s := NewMilvusGraphStore(cfg)
	if s.Config() != cfg {
		t.Error("Config() 应返回传入配置")
	}
}

func TestMilvusGraphStore_EnsureInit(t *testing.T) {
	fake := newFakeGraphStoreClient()
	cfg := graph.NewGraphConfig("localhost:19530")
	cfg.EmbedDim = 3
	s := NewMilvusGraphStore(cfg)
	s.createClient = func(ctx context.Context, uri, token, dbName string) (milvusClient, error) {
		return fake, nil
	}

	ctx := context.Background()
	err := s.ensureInit(ctx)
	if err != nil {
		t.Fatalf("ensureInit() error = %v", err)
	}
	if !s.initialized {
		t.Error("ensureInit 后 initialized 应为 true")
	}
	if s.graphWriter == nil {
		t.Error("ensureInit 后 graphWriter 不应为 nil")
	}
	if s.graphSearcher == nil {
		t.Error("ensureInit 后 graphSearcher 不应为 nil")
	}
}

func TestMilvusGraphStore_EnsureInit_创建客户端失败(t *testing.T) {
	cfg := graph.NewGraphConfig("invalid-uri")
	s := NewMilvusGraphStore(cfg)
	s.createClient = func(ctx context.Context, uri, token, dbName string) (milvusClient, error) {
		return nil, fmt.Errorf("connection refused")
	}

	ctx := context.Background()
	err := s.ensureInit(ctx)
	if err == nil {
		t.Error("ensureInit 创建客户端失败应返回错误")
	}
}

func TestMilvusGraphStore_EnsureInit_重复调用(t *testing.T) {
	fake := newFakeGraphStoreClient()
	cfg := graph.NewGraphConfig("localhost:19530")
	cfg.EmbedDim = 3
	s := NewMilvusGraphStore(cfg)
	s.createClient = func(ctx context.Context, uri, token, dbName string) (milvusClient, error) {
		return fake, nil
	}

	ctx := context.Background()
	// 第一次
	err := s.ensureInit(ctx)
	if err != nil {
		t.Fatalf("ensureInit() error = %v", err)
	}
	// 第二次（应快速返回）
	err = s.ensureInit(ctx)
	if err != nil {
		t.Fatalf("重复 ensureInit() error = %v", err)
	}
}

func TestMilvusGraphStore_GetClient(t *testing.T) {
	fake := newFakeGraphStoreClient()
	cfg := graph.NewGraphConfig("localhost:19530")
	s := NewMilvusGraphStore(cfg)
	s.createClient = func(ctx context.Context, uri, token, dbName string) (milvusClient, error) {
		return fake, nil
	}

	ctx := context.Background()
	client, err := s.getClient(ctx)
	if err != nil {
		t.Fatalf("getClient() error = %v", err)
	}
	if client == nil {
		t.Error("getClient() 应返回非 nil 客户端")
	}
}

func TestMilvusGraphStore_GetClient_失败(t *testing.T) {
	cfg := graph.NewGraphConfig("invalid-uri")
	s := NewMilvusGraphStore(cfg)
	s.createClient = func(ctx context.Context, uri, token, dbName string) (milvusClient, error) {
		return nil, fmt.Errorf("connection refused")
	}

	ctx := context.Background()
	_, err := s.getClient(ctx)
	if err == nil {
		t.Error("getClient 失败应返回错误")
	}
}

func TestMilvusGraphStore_AddEntity(t *testing.T) {
	fake := newFakeGraphStoreClient()
	cfg := graph.NewGraphConfig("localhost:19530")
	cfg.EmbedDim = 3
	s := NewMilvusGraphStore(cfg)
	s.createClient = func(ctx context.Context, uri, token, dbName string) (milvusClient, error) {
		return fake, nil
	}

	ctx := context.Background()
	e := graph.NewEntity()
	e.Content = "测试实体"
	e.Name = "测试名称"

	err := s.AddEntity(ctx, []*graph.Entity{e}, graph.WithNoEmbed(true))
	if err != nil {
		t.Fatalf("AddEntity() error = %v", err)
	}
}

func TestMilvusGraphStore_AddRelation(t *testing.T) {
	fake := newFakeGraphStoreClient()
	cfg := graph.NewGraphConfig("localhost:19530")
	cfg.EmbedDim = 3
	s := NewMilvusGraphStore(cfg)
	s.createClient = func(ctx context.Context, uri, token, dbName string) (milvusClient, error) {
		return fake, nil
	}

	ctx := context.Background()
	r := graph.NewRelation()
	r.Content = "测试关系"
	r.LHS = "e1"
	r.RHS = "e2"

	err := s.AddRelation(ctx, []*graph.Relation{r}, graph.WithNoEmbed(true))
	if err != nil {
		t.Fatalf("AddRelation() error = %v", err)
	}
}

func TestMilvusGraphStore_AddEpisode(t *testing.T) {
	fake := newFakeGraphStoreClient()
	cfg := graph.NewGraphConfig("localhost:19530")
	cfg.EmbedDim = 3
	s := NewMilvusGraphStore(cfg)
	s.createClient = func(ctx context.Context, uri, token, dbName string) (milvusClient, error) {
		return fake, nil
	}

	ctx := context.Background()
	p := graph.NewEpisode()
	p.Content = "测试片段"

	err := s.AddEpisode(ctx, []*graph.Episode{p}, graph.WithNoEmbed(true))
	if err != nil {
		t.Fatalf("AddEpisode() error = %v", err)
	}
}

func TestMilvusGraphStore_Delete(t *testing.T) {
	fake := newFakeGraphStoreClient()
	cfg := graph.NewGraphConfig("localhost:19530")
	cfg.EmbedDim = 3
	s := NewMilvusGraphStore(cfg)
	s.createClient = func(ctx context.Context, uri, token, dbName string) (milvusClient, error) {
		return fake, nil
	}

	ctx := context.Background()
	err := s.Delete(ctx, CollectionEntity, graph.WithIDs("id1"))
	if err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
}

func TestMilvusGraphStore_Query(t *testing.T) {
	fake := newFakeGraphStoreClient()
	cfg := graph.NewGraphConfig("localhost:19530")
	cfg.EmbedDim = 3
	s := NewMilvusGraphStore(cfg)
	s.createClient = func(ctx context.Context, uri, token, dbName string) (milvusClient, error) {
		return fake, nil
	}

	ctx := context.Background()
	results, err := s.Query(ctx, CollectionEntity, graph.WithIDs("abc"))
	if err != nil {
		t.Fatalf("Query() error = %v", err)
	}
	// fakeGraphStoreClient 返回空 ResultSet，结果应为 nil
	if results != nil {
		t.Logf("Query 返回结果: %v", results)
	}
}

func TestMilvusGraphStore_Query_空选项(t *testing.T) {
	fake := newFakeGraphStoreClient()
	cfg := graph.NewGraphConfig("localhost:19530")
	cfg.EmbedDim = 3
	s := NewMilvusGraphStore(cfg)
	s.createClient = func(ctx context.Context, uri, token, dbName string) (milvusClient, error) {
		return fake, nil
	}

	ctx := context.Background()
	results, err := s.Query(ctx, CollectionEntity)
	if err != nil {
		t.Fatalf("Query() error = %v", err)
	}
	_ = results
}

func TestMilvusGraphStore_IsEmpty(t *testing.T) {
	fake := newFakeGraphStoreClient()
	cfg := graph.NewGraphConfig("localhost:19530")
	cfg.EmbedDim = 3
	s := NewMilvusGraphStore(cfg)
	s.createClient = func(ctx context.Context, uri, token, dbName string) (milvusClient, error) {
		return fake, nil
	}

	ctx := context.Background()
	empty, err := s.IsEmpty(ctx, CollectionEntity)
	if err != nil {
		t.Fatalf("IsEmpty() error = %v", err)
	}
	// fakeGraphStoreClient 返回空 ResultSet，ResultCount=0 → IsEmpty=true
	if !empty {
		t.Error("空集合 IsEmpty 应返回 true")
	}
}

func TestMilvusGraphStore_IsEmpty_有数据(t *testing.T) {
	fake := newFakeGraphStoreClient()
	// 构造有数据的 ResultSet
	uuidCol := column.NewColumnVarChar("uuid", []string{"abc"})
	fake.queryResult = milvusclient.ResultSet{
		ResultCount: 1,
		Fields:      []column.Column{uuidCol},
	}

	cfg := graph.NewGraphConfig("localhost:19530")
	cfg.EmbedDim = 3
	s := NewMilvusGraphStore(cfg)
	s.createClient = func(ctx context.Context, uri, token, dbName string) (milvusClient, error) {
		return fake, nil
	}

	ctx := context.Background()
	empty, err := s.IsEmpty(ctx, CollectionEntity)
	if err != nil {
		t.Fatalf("IsEmpty() error = %v", err)
	}
	if empty {
		t.Error("有数据的集合 IsEmpty 应返回 false")
	}
}

func TestMilvusGraphStore_Search(t *testing.T) {
	fake := newFakeGraphStoreClient()
	// 构造搜索结果
	uuidCol := column.NewColumnVarChar("uuid", []string{"id1"})
	contentCol := column.NewColumnVarChar("content", []string{"hello"})
	fake.searchResults = []milvusclient.ResultSet{
		{
			ResultCount: 1,
			Fields:      []column.Column{uuidCol, contentCol},
		},
	}

	cfg := graph.NewGraphConfig("localhost:19530")
	cfg.EmbedDim = 3
	s := NewMilvusGraphStore(cfg)
	s.createClient = func(ctx context.Context, uri, token, dbName string) (milvusClient, error) {
		return fake, nil
	}

	ctx := context.Background()
	results, err := s.Search(ctx, "test query", graph.WithCollection(CollectionEntity), graph.WithQueryEmbedding([]float64{0.1, 0.2, 0.3}))
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if results == nil {
		t.Error("Search 应返回非 nil 结果")
	}
}

func TestMilvusGraphStore_AttachEmbedder(t *testing.T) {
	fake := newFakeGraphStoreClient()
	cfg := graph.NewGraphConfig("localhost:19530")
	cfg.EmbedDim = 3
	s := NewMilvusGraphStore(cfg)
	s.createClient = func(ctx context.Context, uri, token, dbName string) (milvusClient, error) {
		return fake, nil
	}

	// 先初始化
	ctx := context.Background()
	_ = s.ensureInit(ctx)

	emb := &fakeEmbedder{}
	s.AttachEmbedder(emb)

	if s.graphWriter.embedder != emb {
		t.Error("graphWriter.embedder 应被设置")
	}
	if s.graphSearcher.embedder != emb {
		t.Error("graphSearcher.embedder 应被设置")
	}
}

func TestMilvusGraphStore_AttachEmbedder_未初始化(t *testing.T) {
	cfg := graph.NewGraphConfig("localhost:19530")
	s := NewMilvusGraphStore(cfg)

	emb := &fakeEmbedder{}
	// 未初始化时 AttachEmbedder 不应 panic
	s.AttachEmbedder(emb)
}

func TestMilvusGraphStore_Close(t *testing.T) {
	fake := newFakeGraphStoreClient()
	cfg := graph.NewGraphConfig("localhost:19530")
	cfg.EmbedDim = 3
	s := NewMilvusGraphStore(cfg)
	s.createClient = func(ctx context.Context, uri, token, dbName string) (milvusClient, error) {
		return fake, nil
	}

	// 先初始化客户端
	ctx := context.Background()
	_, _ = s.getClient(ctx)

	err := s.Close()
	if err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestMilvusGraphStore_Close_无客户端(t *testing.T) {
	cfg := graph.NewGraphConfig("localhost:19530")
	s := NewMilvusGraphStore(cfg)

	err := s.Close()
	if err != nil {
		t.Fatalf("Close() 无客户端应返回 nil, error = %v", err)
	}
}

func TestMilvusGraphStore_Close_失败(t *testing.T) {
	fake := newFakeGraphStoreClient()
	fake.closeErr = fmt.Errorf("close error")
	cfg := graph.NewGraphConfig("localhost:19530")
	s := NewMilvusGraphStore(cfg)
	s.createClient = func(ctx context.Context, uri, token, dbName string) (milvusClient, error) {
		return fake, nil
	}

	ctx := context.Background()
	_, _ = s.getClient(ctx)

	err := s.Close()
	if err == nil {
		t.Error("Close 失败应返回错误")
	}
}

func TestMilvusGraphStore_Refresh(t *testing.T) {
	fake := newFakeGraphStoreClient()
	cfg := graph.NewGraphConfig("localhost:19530")
	cfg.EmbedDim = 3
	s := NewMilvusGraphStore(cfg)
	s.createClient = func(ctx context.Context, uri, token, dbName string) (milvusClient, error) {
		return fake, nil
	}

	ctx := context.Background()
	err := s.Refresh(ctx)
	if err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
}

func TestMilvusGraphStore_Refresh_Flush失败(t *testing.T) {
	fake := newFakeGraphStoreClient()
	fake.flushErr = fmt.Errorf("flush error")
	cfg := graph.NewGraphConfig("localhost:19530")
	cfg.EmbedDim = 3
	s := NewMilvusGraphStore(cfg)
	s.createClient = func(ctx context.Context, uri, token, dbName string) (milvusClient, error) {
		return fake, nil
	}

	ctx := context.Background()
	// Refresh 内部 Flush 失败只记录警告，不返回错误
	err := s.Refresh(ctx)
	if err != nil {
		t.Fatalf("Refresh() Flush 失败应只记录警告, error = %v", err)
	}
}

func TestMilvusGraphStore_Rebuild(t *testing.T) {
	fake := newFakeGraphStoreClient()
	cfg := graph.NewGraphConfig("localhost:19530")
	cfg.EmbedDim = 3
	s := NewMilvusGraphStore(cfg)
	s.createClient = func(ctx context.Context, uri, token, dbName string) (milvusClient, error) {
		return fake, nil
	}

	ctx := context.Background()
	err := s.Rebuild(ctx)
	if err != nil {
		t.Fatalf("Rebuild() error = %v", err)
	}
}

func TestMilvusGraphStore_Rebuild_集合已存在(t *testing.T) {
	fake := newFakeGraphStoreClient()
	fake.collections[CollectionEntity] = true
	fake.collections[CollectionRelation] = true
	fake.collections[CollectionEpisode] = true
	cfg := graph.NewGraphConfig("localhost:19530")
	cfg.EmbedDim = 3
	s := NewMilvusGraphStore(cfg)
	s.createClient = func(ctx context.Context, uri, token, dbName string) (milvusClient, error) {
		return fake, nil
	}

	ctx := context.Background()
	err := s.Rebuild(ctx)
	if err != nil {
		t.Fatalf("Rebuild() error = %v", err)
	}
}

func TestMilvusGraphStore_Rebuild_HasCollection失败(t *testing.T) {
	fake := newFakeGraphStoreClient()
	fake.hasCollErr = fmt.Errorf("has collection error")
	cfg := graph.NewGraphConfig("localhost:19530")
	cfg.EmbedDim = 3
	s := NewMilvusGraphStore(cfg)
	s.createClient = func(ctx context.Context, uri, token, dbName string) (milvusClient, error) {
		return fake, nil
	}

	ctx := context.Background()
	err := s.Rebuild(ctx)
	if err == nil {
		t.Error("Rebuild HasCollection 失败应返回错误")
	}
}

func TestMilvusGraphStore_Rebuild_DropCollection失败(t *testing.T) {
	fake := newFakeGraphStoreClient()
	fake.collections[CollectionEntity] = true
	fake.dropCollErr = fmt.Errorf("drop error")
	cfg := graph.NewGraphConfig("localhost:19530")
	cfg.EmbedDim = 3
	s := NewMilvusGraphStore(cfg)
	s.createClient = func(ctx context.Context, uri, token, dbName string) (milvusClient, error) {
		return fake, nil
	}

	ctx := context.Background()
	err := s.Rebuild(ctx)
	if err == nil {
		t.Error("Rebuild DropCollection 失败应返回错误")
	}
}

func TestMilvusGraphStore_Rebuild_CreateCollection失败(t *testing.T) {
	fake := newFakeGraphStoreClient()
	fake.createCollErr = fmt.Errorf("create error")
	cfg := graph.NewGraphConfig("localhost:19530")
	cfg.EmbedDim = 3
	s := NewMilvusGraphStore(cfg)
	s.createClient = func(ctx context.Context, uri, token, dbName string) (milvusClient, error) {
		return fake, nil
	}

	ctx := context.Background()
	err := s.Rebuild(ctx)
	if err == nil {
		t.Error("Rebuild CreateCollection 失败应返回错误")
	}
}

// TestMilvusGraphStore_EnsureInit_StorageConfigNil 验证 StorageConfig 为 nil 时使用默认值
func TestMilvusGraphStore_EnsureInit_StorageConfigNil(t *testing.T) {
	fake := newFakeGraphStoreClient()
	cfg := graph.NewGraphConfig("localhost:19530")
	cfg.EmbedDim = 3
	cfg.StorageConfig = nil
	cfg.IndexConfig = nil
	s := NewMilvusGraphStore(cfg)
	s.createClient = func(ctx context.Context, uri, token, dbName string) (milvusClient, error) {
		return fake, nil
	}

	ctx := context.Background()
	err := s.ensureInit(ctx)
	if err != nil {
		t.Fatalf("ensureInit() StorageConfig nil error = %v", err)
	}
	if s.graphWriter == nil {
		t.Error("ensureInit 后 graphWriter 不应为 nil")
	}
}

// TestMilvusGraphStore_Query_带Expr 测试带过滤表达式的查询
func TestMilvusGraphStore_Query_带Expr(t *testing.T) {
	fake := newFakeGraphStoreClient()
	cfg := graph.NewGraphConfig("localhost:19530")
	cfg.EmbedDim = 3
	s := NewMilvusGraphStore(cfg)
	s.createClient = func(ctx context.Context, uri, token, dbName string) (milvusClient, error) {
		return fake, nil
	}

	ctx := context.Background()
	// 使用 WithIDs 替代 WithExpr（Query 使用 o.IDs 或 o.Expr）
	results, err := s.Query(ctx, CollectionEntity, graph.WithIDs("id1", "id2"))
	if err != nil {
		t.Fatalf("Query() error = %v", err)
	}
	_ = results
}

// TestMilvusGraphStore_Query_带OutputFields 测试带输出字段的查询
func TestMilvusGraphStore_Query_带OutputFields(t *testing.T) {
	fake := newFakeGraphStoreClient()
	cfg := graph.NewGraphConfig("localhost:19530")
	cfg.EmbedDim = 3
	s := NewMilvusGraphStore(cfg)
	s.createClient = func(ctx context.Context, uri, token, dbName string) (milvusClient, error) {
		return fake, nil
	}

	ctx := context.Background()
	results, err := s.Query(ctx, CollectionEntity, graph.WithOutputFields("uuid", "content"))
	if err != nil {
		t.Fatalf("Query() error = %v", err)
	}
	_ = results
}

// TestMilvusGraphStore_IsEmpty_Query失败 测试 Query 失败场景
func TestMilvusGraphStore_IsEmpty_Query失败(t *testing.T) {
	fake := newFakeGraphStoreClient()
	fake.queryErr = fmt.Errorf("query error")
	cfg := graph.NewGraphConfig("localhost:19530")
	cfg.EmbedDim = 3
	s := NewMilvusGraphStore(cfg)
	s.createClient = func(ctx context.Context, uri, token, dbName string) (milvusClient, error) {
		return fake, nil
	}

	ctx := context.Background()
	_, err := s.IsEmpty(ctx, CollectionEntity)
	if err == nil {
		t.Error("IsEmpty Query 失败应返回错误")
	}
}

// TestMilvusGraphStore_AddEntity_未初始化 测试未初始化时的错误
func TestMilvusGraphStore_AddEntity_未初始化(t *testing.T) {
	cfg := graph.NewGraphConfig("invalid-uri")
	s := NewMilvusGraphStore(cfg)
	s.createClient = func(ctx context.Context, uri, token, dbName string) (milvusClient, error) {
		return nil, fmt.Errorf("connection refused")
	}

	ctx := context.Background()
	e := graph.NewEntity()
	err := s.AddEntity(ctx, []*graph.Entity{e})
	if err == nil {
		t.Error("AddEntity 未初始化应返回错误")
	}
}

// TestMilvusGraphStore_Search_搜索全部集合
func TestMilvusGraphStore_Search_搜索全部集合(t *testing.T) {
	fake := newFakeGraphStoreClient()
	uuidCol := column.NewColumnVarChar("uuid", []string{"id1"})
	contentCol := column.NewColumnVarChar("content", []string{"hello"})
	fake.searchResults = []milvusclient.ResultSet{
		{
			ResultCount: 1,
			Fields:      []column.Column{uuidCol, contentCol},
		},
	}

	cfg := graph.NewGraphConfig("localhost:19530")
	cfg.EmbedDim = 3
	s := NewMilvusGraphStore(cfg)
	s.createClient = func(ctx context.Context, uri, token, dbName string) (milvusClient, error) {
		return fake, nil
	}

	ctx := context.Background()
	results, err := s.Search(ctx, "test", graph.WithQueryEmbedding([]float64{0.1, 0.2, 0.3}))
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(results) != 3 {
		t.Errorf("搜索全部集合应返回3个集合结果，实际 %d", len(results))
	}
}

// TestResultSetToMaps 测试 ResultSet 转 map
func TestResultSetToMaps(t *testing.T) {
	uuidCol := column.NewColumnVarChar("uuid", []string{"abc", "def"})
	contentCol := column.NewColumnVarChar("content", []string{"hello", "world"})

	rs := milvusclient.ResultSet{
		ResultCount: 2,
		Fields:      []column.Column{uuidCol, contentCol},
	}

	results := resultSetToMaps(rs)
	if len(results) != 2 {
		t.Fatalf("resultSetToMaps 应返回2行，实际 %d", len(results))
	}
	if results[0]["uuid"] != "abc" {
		t.Errorf("row0 uuid = %v, want abc", results[0]["uuid"])
	}
	if results[1]["content"] != "world" {
		t.Errorf("row1 content = %v, want world", results[1]["content"])
	}
}

// TestResultSetToMaps_空 测试空 ResultSet
func TestResultSetToMaps_空(t *testing.T) {
	rs := milvusclient.ResultSet{
		ResultCount: 0,
		Fields:      nil,
	}
	results := resultSetToMaps(rs)
	if results != nil {
		t.Errorf("空 ResultSet 应返回 nil，实际 %v", results)
	}
}

// TestParseResultSets 测试多 ResultSet 解析
func TestParseResultSets(t *testing.T) {
	uuidCol := column.NewColumnVarChar("uuid", []string{"abc"})
	contentCol := column.NewColumnVarChar("content", []string{"hello"})

	resultSets := []milvusclient.ResultSet{
		{
			ResultCount: 1,
			Fields:      []column.Column{uuidCol, contentCol},
		},
	}

	results := parseResultSets(resultSets)
	if len(results) != 1 {
		t.Fatalf("parseResultSets 应返回1行，实际 %d", len(results))
	}
	if results[0]["uuid"] != "abc" {
		t.Errorf("uuid = %v, want abc", results[0]["uuid"])
	}
}

// TestParseResultSets_空 测试空 ResultSet 列表
func TestParseResultSets_空(t *testing.T) {
	results := parseResultSets(nil)
	if results != nil {
		t.Errorf("空 ResultSet 列表应返回 nil，实际 %v", results)
	}
}

// TestParseResultSets_ResultCountZero 测试 ResultCount 为 0 的 ResultSet
func TestParseResultSets_ResultCountZero(t *testing.T) {
	resultSets := []milvusclient.ResultSet{
		{ResultCount: 0, Fields: nil},
	}
	results := parseResultSets(resultSets)
	if len(results) != 0 {
		t.Errorf("ResultCount=0 应返回空切片，实际 %d", len(results))
	}
}

// TestMilvusGraphStore_AttachEmbedder_初始化后 测试初始化后 AttachEmbedder
func TestMilvusGraphStore_AttachEmbedder_初始化后(t *testing.T) {
	fake := newFakeGraphStoreClient()
	cfg := graph.NewGraphConfig("localhost:19530")
	cfg.EmbedDim = 3
	s := NewMilvusGraphStore(cfg)
	s.createClient = func(ctx context.Context, uri, token, dbName string) (milvusClient, error) {
		return fake, nil
	}

	ctx := context.Background()
	_ = s.ensureInit(ctx)

	emb := &fakeEmbedder{}
	s.AttachEmbedder(emb)

	// 验证 writer 和 searcher 都获得了 embedder
	if s.graphWriter.embedder == nil {
		t.Error("graphWriter.embedder 不应为 nil")
	}
	if s.graphSearcher.embedder == nil {
		t.Error("graphSearcher.embedder 不应为 nil")
	}
}

// fakeGraphSearchEmbedder 用于搜索测试的 embedder
type fakeGraphSearchEmbedder struct {
	emb []float64
	err error
}

func (f *fakeGraphSearchEmbedder) EmbedQuery(ctx context.Context, text string) ([]float64, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.emb != nil {
		return f.emb, nil
	}
	return []float64{0.1, 0.2, 0.3}, nil
}

func (f *fakeGraphSearchEmbedder) EmbedDocuments(ctx context.Context, texts []string, opts ...embedding.EmbedOption) ([][]float64, error) {
	if f.err != nil {
		return nil, f.err
	}
	result := make([][]float64, len(texts))
	for i := range texts {
		if f.emb != nil {
			result[i] = f.emb
		} else {
			result[i] = []float64{0.1, 0.2, 0.3}
		}
	}
	return result, nil
}

func (f *fakeGraphSearchEmbedder) Dimension() int {
	if f.emb != nil {
		return len(f.emb)
	}
	return 3
}

// TestMilvusGraphStore_Rebuild_获取客户端失败
func TestMilvusGraphStore_Rebuild_获取客户端失败(t *testing.T) {
	cfg := graph.NewGraphConfig("invalid-uri")
	s := NewMilvusGraphStore(cfg)
	s.createClient = func(ctx context.Context, uri, token, dbName string) (milvusClient, error) {
		return nil, fmt.Errorf("connection refused")
	}

	ctx := context.Background()
	err := s.Rebuild(ctx)
	if err == nil {
		t.Error("Rebuild 获取客户端失败应返回错误")
	}
}

// TestMilvusGraphStore_Refresh_获取客户端失败
func TestMilvusGraphStore_Refresh_获取客户端失败(t *testing.T) {
	cfg := graph.NewGraphConfig("invalid-uri")
	s := NewMilvusGraphStore(cfg)
	s.createClient = func(ctx context.Context, uri, token, dbName string) (milvusClient, error) {
		return nil, fmt.Errorf("connection refused")
	}

	ctx := context.Background()
	err := s.Refresh(ctx)
	if err == nil {
		t.Error("Refresh 获取客户端失败应返回错误")
	}
}
