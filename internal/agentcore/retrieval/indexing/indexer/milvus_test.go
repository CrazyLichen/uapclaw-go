package indexer

import (
	"context"
	"fmt"
	"testing"

	"github.com/milvus-io/milvus-proto/go-api/v2/schemapb"
	"github.com/milvus-io/milvus/client/v2/entity"
	milvusclient "github.com/milvus-io/milvus/client/v2/milvusclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
	vector_store "github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/vector_store"
)

// fakeIndexerMilvusClient indexer 测试用 fake Milvus 客户端。
// 实现 vector_store.MilvusClient 接口，通过字段注入错误和结果。
type fakeIndexerMilvusClient struct {
	// collections 已存在的集合名集合
	collections map[string]bool
	// deleteCount Delete 返回的删除条数
	deleteCount int64
	// statsRowCount GetCollectionStats 返回的 row_count 字符串
	statsRowCount string
	// statsErr GetCollectionStats 返回的错误
	statsErr error
	// describeCollResult DescribeCollection 返回的结果
	describeCollResult *entity.Collection
	// describeCollErr DescribeCollection 返回的错误
	describeCollErr error
	// hasCollectionErr HasCollection 返回的错误
	hasCollectionErr error
	// deleteErr Delete 返回的错误
	deleteErr error
	// flushCount Flush 调用次数
	flushCount int
	// createdCollections CreateCollection 创建的集合名
	createdCollections []string
	// createdSchemas CreateCollection 创建的 schema（按集合名记录）
	createdSchemas map[string]*entity.Schema
	// createCollErr CreateCollection 返回的错误
	createCollErr error
}

func newFakeIndexerMilvusClient() *fakeIndexerMilvusClient {
	return &fakeIndexerMilvusClient{
		collections:     map[string]bool{},
		createdSchemas:  map[string]*entity.Schema{},
	}
}

func (f *fakeIndexerMilvusClient) CreateCollection(ctx context.Context, option milvusclient.CreateCollectionOption, callOptions ...any) error {
	if f.createCollErr != nil {
		return f.createCollErr
	}
	req := option.Request()
	f.createdCollections = append(f.createdCollections, req.GetCollectionName())
	f.collections[req.GetCollectionName()] = true
	// Schema 在 Request() 中被序列化为 bytes（schemapb.CollectionSchema），反序列化还原
	if schemaBytes := req.GetSchema(); len(schemaBytes) > 0 {
		schemaProto := &schemapb.CollectionSchema{}
		if err := proto.Unmarshal(schemaBytes, schemaProto); err == nil {
			f.createdSchemas[req.GetCollectionName()] = entity.NewSchema().ReadProto(schemaProto)
		}
	}
	return nil
}
func (f *fakeIndexerMilvusClient) DropCollection(ctx context.Context, option milvusclient.DropCollectionOption, callOptions ...any) error {
	delete(f.collections, option.Request().GetCollectionName())
	return nil
}
func (f *fakeIndexerMilvusClient) HasCollection(ctx context.Context, option milvusclient.HasCollectionOption, callOptions ...any) (bool, error) {
	if f.hasCollectionErr != nil {
		return false, f.hasCollectionErr
	}
	name := option.Request().GetCollectionName()
	return f.collections[name], nil
}
func (f *fakeIndexerMilvusClient) DescribeCollection(ctx context.Context, option milvusclient.DescribeCollectionOption, callOptions ...any) (*entity.Collection, error) {
	if f.describeCollErr != nil {
		return nil, f.describeCollErr
	}
	if f.describeCollResult != nil {
		return f.describeCollResult, nil
	}
	return &entity.Collection{}, nil
}
func (f *fakeIndexerMilvusClient) ListCollections(ctx context.Context, option milvusclient.ListCollectionOption, callOptions ...any) ([]string, error) {
	names := make([]string, 0, len(f.collections))
	for name := range f.collections {
		names = append(names, name)
	}
	return names, nil
}
func (f *fakeIndexerMilvusClient) GetCollectionStats(ctx context.Context, option milvusclient.GetCollectionOption, callOptions ...any) (map[string]string, error) {
	if f.statsErr != nil {
		return nil, f.statsErr
	}
	return map[string]string{"row_count": f.statsRowCount}, nil
}
func (f *fakeIndexerMilvusClient) Insert(ctx context.Context, option milvusclient.InsertOption, callOptions ...any) (milvusclient.InsertResult, error) {
	return milvusclient.InsertResult{}, nil
}
func (f *fakeIndexerMilvusClient) Delete(ctx context.Context, option milvusclient.DeleteOption, callOptions ...any) (milvusclient.DeleteResult, error) {
	if f.deleteErr != nil {
		return milvusclient.DeleteResult{}, f.deleteErr
	}
	return milvusclient.DeleteResult{DeleteCount: f.deleteCount}, nil
}
func (f *fakeIndexerMilvusClient) Flush(ctx context.Context, option milvusclient.FlushOption, callOptions ...any) error {
	f.flushCount++
	return nil
}
func (f *fakeIndexerMilvusClient) Search(ctx context.Context, option milvusclient.SearchOption, callOptions ...any) ([]milvusclient.ResultSet, error) {
	return nil, nil
}
func (f *fakeIndexerMilvusClient) HybridSearch(ctx context.Context, option milvusclient.HybridSearchOption, callOptions ...any) ([]milvusclient.ResultSet, error) {
	return nil, nil
}
func (f *fakeIndexerMilvusClient) Query(ctx context.Context, option milvusclient.QueryOption, callOptions ...any) (milvusclient.ResultSet, error) {
	return milvusclient.ResultSet{}, nil
}
func (f *fakeIndexerMilvusClient) DescribeIndex(ctx context.Context, option milvusclient.DescribeIndexOption, callOptions ...any) (milvusclient.IndexDescription, error) {
	return milvusclient.IndexDescription{}, nil
}
func (f *fakeIndexerMilvusClient) LoadCollection(ctx context.Context, option milvusclient.LoadCollectionOption, callOptions ...any) error {
	return nil
}
func (f *fakeIndexerMilvusClient) GetLoadState(ctx context.Context, option milvusclient.GetLoadStateOption, callOptions ...any) (entity.LoadState, error) {
	return entity.LoadState{State: entity.LoadStateLoaded}, nil
}
func (f *fakeIndexerMilvusClient) ReleaseCollection(ctx context.Context, option milvusclient.ReleaseCollectionOption, callOptions ...any) error {
	return nil
}
func (f *fakeIndexerMilvusClient) ListDatabase(ctx context.Context, option milvusclient.ListDatabaseOption, callOptions ...any) ([]string, error) {
	return []string{"default"}, nil
}
func (f *fakeIndexerMilvusClient) CreateDatabase(ctx context.Context, option milvusclient.CreateDatabaseOption, callOptions ...any) error {
	return nil
}
func (f *fakeIndexerMilvusClient) UseDatabase(ctx context.Context, option milvusclient.UseDatabaseOption, callOptions ...any) error {
	return nil
}
func (f *fakeIndexerMilvusClient) Close(ctx context.Context) error {
	return nil
}

// 编译期断言：fake 实现 vector_store.MilvusClient 接口
var _ vector_store.MilvusClient = (*fakeIndexerMilvusClient)(nil)

// newTestMilvusIndexer 构建 MilvusIndexer 测试实例
func newTestMilvusIndexer(t *testing.T, fake *fakeIndexerMilvusClient) *MilvusIndexer {
	t.Helper()
	config := common.VectorStoreConfig{
		StoreProvider:  common.StoreTypeMilvus,
		CollectionName: "test-collection",
		DatabaseName:   "default",
		DistanceMetric: common.DistanceMetricCosine,
	}
	idx, err := NewMilvusIndexer(config, "http://localhost:19530", "",
		WithMilvusIndexerClient(fake))
	require.NoError(t, err)
	return idx
}

// TestNewMilvusIndexer_默认参数 对齐 Python MilvusIndexer.__init__
func TestNewMilvusIndexer_默认参数(t *testing.T) {
	fake := newFakeIndexerMilvusClient()
	idx := newTestMilvusIndexer(t, fake)
	assert.Equal(t, "COSINE", idx.distanceMetric)
	assert.Equal(t, "content", idx.textField)
	assert.Equal(t, "embedding", idx.vectorField.VectorFieldName())
	assert.Equal(t, "sparse_vector", idx.sparseVectorField)
	assert.Equal(t, "metadata", idx.metadataField)
	assert.Equal(t, "document_id", idx.docIDField)
}

// TestNewMilvusIndexer_别名生成 对齐 Python: create_milvus_alias(None, uri, token)
func TestNewMilvusIndexer_别名生成(t *testing.T) {
	fake := newFakeIndexerMilvusClient()
	config := common.VectorStoreConfig{
		StoreProvider:  common.StoreTypeMilvus,
		CollectionName: "test",
		DistanceMetric: common.DistanceMetricCosine,
	}
	idx, err := NewMilvusIndexer(config, "http://localhost:19530", "token123",
		WithMilvusIndexerClient(fake))
	require.NoError(t, err)
	assert.Contains(t, idx.milvusAlias, "kb-http://localhost:19530-")
}

// TestNewMilvusIndexer_构造配置 对齐 Python: auto → {} 否则 to_dict(construct)
func TestNewMilvusIndexer_构造配置(t *testing.T) {
	fake := newFakeIndexerMilvusClient()
	config := common.VectorStoreConfig{
		StoreProvider:  common.StoreTypeMilvus,
		CollectionName: "test",
		DistanceMetric: common.DistanceMetricCosine,
	}
	idx, err := NewMilvusIndexer(config, "http://localhost:19530", "",
		WithMilvusIndexerClient(fake))
	require.NoError(t, err)
	// AUTO → 空 construct_config，仅 metric_type
	assert.Equal(t, map[string]any{"metric_type": "COSINE"}, idx.constructConfig)
}

// TestMilvusIndexer_IndexExists 对齐 Python index_exists
func TestMilvusIndexer_IndexExists(t *testing.T) {
	fake := newFakeIndexerMilvusClient()
	fake.collections["some_index"] = true
	idx := newTestMilvusIndexer(t, fake)

	ok, err := idx.IndexExists(context.Background(), "some_index")
	require.NoError(t, err)
	assert.True(t, ok)
}

// TestMilvusIndexer_IndexExists_不存在 对齐 Python: has_collection=False
func TestMilvusIndexer_IndexExists_不存在(t *testing.T) {
	fake := newFakeIndexerMilvusClient()
	idx := newTestMilvusIndexer(t, fake)

	ok, err := idx.IndexExists(context.Background(), "some_index")
	require.NoError(t, err)
	assert.False(t, ok)
}

// TestMilvusIndexer_IndexExists_异常 对齐 Python: except → return False
func TestMilvusIndexer_IndexExists_异常(t *testing.T) {
	fake := newFakeIndexerMilvusClient()
	fake.hasCollectionErr = fmt.Errorf("conn error")
	idx := newTestMilvusIndexer(t, fake)

	ok, err := idx.IndexExists(context.Background(), "some_index")
	require.NoError(t, err) // 对齐 Python: 异常返回 False 不抛错
	assert.False(t, ok)
}

// TestMilvusIndexer_DeleteIndex 对齐 Python delete_index：filter 删除
func TestMilvusIndexer_DeleteIndex(t *testing.T) {
	fake := newFakeIndexerMilvusClient()
	fake.deleteCount = 5
	idx := newTestMilvusIndexer(t, fake)

	ok, err := idx.DeleteIndex(context.Background(), "doc1", "index1")
	require.NoError(t, err)
	assert.True(t, ok) // delete_count > 0
}

// TestMilvusIndexer_DeleteIndex_零删除 对齐 Python: delete_count == 0 → False
func TestMilvusIndexer_DeleteIndex_零删除(t *testing.T) {
	fake := newFakeIndexerMilvusClient()
	fake.deleteCount = 0
	idx := newTestMilvusIndexer(t, fake)

	ok, err := idx.DeleteIndex(context.Background(), "doc1", "index1")
	require.NoError(t, err)
	assert.False(t, ok)
}

// TestMilvusIndexer_DeleteIndex_异常 对齐 Python: MilvusException → return False
func TestMilvusIndexer_DeleteIndex_异常(t *testing.T) {
	fake := newFakeIndexerMilvusClient()
	fake.deleteErr = fmt.Errorf("delete failed")
	idx := newTestMilvusIndexer(t, fake)

	ok, err := idx.DeleteIndex(context.Background(), "doc1", "index1")
	require.NoError(t, err) // 对齐 Python: 异常返回 False 不抛错
	assert.False(t, ok)
}

// TestMilvusIndexer_GetIndexInfo_存在 对齐 Python get_index_info：stats row_count + describe
func TestMilvusIndexer_GetIndexInfo_存在(t *testing.T) {
	fake := newFakeIndexerMilvusClient()
	fake.collections["index1"] = true
	fake.statsRowCount = "42"
	idx := newTestMilvusIndexer(t, fake)

	info, err := idx.GetIndexInfo(context.Background(), "index1")
	require.NoError(t, err)
	assert.Equal(t, true, info["exists"])
	assert.Equal(t, "index1", info["collection_name"])
	assert.Equal(t, 42, info["count"])
	assert.NotNil(t, info["info"]) // describe_collection 结果
}

// TestMilvusIndexer_GetIndexInfo_不存在 对齐 Python: return {"exists": False}
func TestMilvusIndexer_GetIndexInfo_不存在(t *testing.T) {
	fake := newFakeIndexerMilvusClient()
	idx := newTestMilvusIndexer(t, fake)

	info, err := idx.GetIndexInfo(context.Background(), "index1")
	require.NoError(t, err)
	assert.Equal(t, false, info["exists"])
}

// TestMilvusIndexer_Close 对齐 Python close
func TestMilvusIndexer_Close(t *testing.T) {
	fake := newFakeIndexerMilvusClient()
	idx := newTestMilvusIndexer(t, fake)
	assert.NotPanics(t, func() { idx.Close() })
}

// TestMilvusIndexer_ensureCollection_已存在 对齐 Python: index_exists → return
func TestMilvusIndexer_ensureCollection_已存在(t *testing.T) {
	fake := newFakeIndexerMilvusClient()
	fake.collections["existing_coll"] = true
	idx := newTestMilvusIndexer(t, fake)

	err := idx.ensureCollection(context.Background(), "existing_coll",
		common.IndexConfig{IndexName: "existing_coll", IndexType: common.IndexTypeVector}, nil)
	require.NoError(t, err)
	assert.Empty(t, fake.createdCollections) // 未创建新集合
}

// TestMilvusIndexer_ensureCollection_BM25 对齐 Python: bm25 类型含 analyzer+sparse+BM25函数
func TestMilvusIndexer_ensureCollection_BM25(t *testing.T) {
	fake := newFakeIndexerMilvusClient()
	idx := newTestMilvusIndexer(t, fake)

	err := idx.ensureCollection(context.Background(), "bm25_coll",
		common.IndexConfig{IndexName: "bm25_coll", IndexType: common.IndexTypeBM25}, nil)
	require.NoError(t, err)

	schema := fake.createdSchemas["bm25_coll"]
	require.NotNil(t, schema)
	// 断言字段
	fieldNames := map[string]bool{}
	for _, f := range schema.Fields {
		fieldNames[f.Name] = true
	}
	assert.True(t, fieldNames["document_id"])
	assert.True(t, fieldNames["pk"])
	assert.True(t, fieldNames["chunk_id"])
	assert.True(t, fieldNames["content"])
	assert.True(t, fieldNames["sparse_vector"])
	assert.True(t, fieldNames["metadata"])
	assert.False(t, fieldNames["embedding"]) // bm25 不含 dense 向量

	// 断言 BM25 Function
	require.Len(t, schema.Functions, 1)
	assert.Equal(t, "text_bm25_emb", schema.Functions[0].Name)
	assert.Equal(t, entity.FunctionTypeBM25, schema.Functions[0].Type)
	assert.Equal(t, []string{"content"}, schema.Functions[0].InputFieldNames)
	assert.Equal(t, []string{"sparse_vector"}, schema.Functions[0].OutputFieldNames)

	// 断言 content 字段启用 analyzer（jieba）
	for _, f := range schema.Fields {
		if f.Name == "content" {
			assert.Equal(t, "true", f.TypeParams["enable_analyzer"])
			assert.Contains(t, f.TypeParams["analyzer_params"], "jieba")
		}
	}
}

// TestMilvusIndexer_ensureCollection_Vector 对齐 Python: vector 类型含 dense 向量
func TestMilvusIndexer_ensureCollection_Vector(t *testing.T) {
	fake := newFakeIndexerMilvusClient()
	idx := newTestMilvusIndexer(t, fake)

	err := idx.ensureCollection(context.Background(), "vec_coll",
		common.IndexConfig{IndexName: "vec_coll", IndexType: common.IndexTypeVector}, nil)
	require.Error(t, err) // 无 embedModel 时 dimension 无法推断 → 报错
	assert.Empty(t, fake.createdCollections) // 创建失败
}

// TestMilvusIndexer_ensureCollection_Hybrid 对齐 Python: hybrid 类型同时含 sparse + dense
func TestMilvusIndexer_ensureCollection_Hybrid(t *testing.T) {
	fake := newFakeIndexerMilvusClient()
	idx := newTestMilvusIndexer(t, fake)

	err := idx.ensureCollection(context.Background(), "hybrid_coll",
		common.IndexConfig{IndexName: "hybrid_coll", IndexType: common.IndexTypeHybrid}, nil)
	require.Error(t, err) // 同样需要 embedModel 提供 dimension
}

// TestMilvusIndexer_ensureCollection_创建失败 对齐 Python: create_collection 异常
func TestMilvusIndexer_ensureCollection_创建失败(t *testing.T) {
	fake := newFakeIndexerMilvusClient()
	fake.createCollErr = fmt.Errorf("create failed")
	idx := newTestMilvusIndexer(t, fake)

	err := idx.ensureCollection(context.Background(), "bm25_coll",
		common.IndexConfig{IndexName: "bm25_coll", IndexType: common.IndexTypeBM25}, nil)
	require.Error(t, err)
}
