package vector_store

import (
	"context"
	"fmt"
	"testing"

	"github.com/milvus-io/milvus/client/v2/entity"
	milvusclient "github.com/milvus-io/milvus/client/v2/milvusclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/vector_fields"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
)

// fakeRetrievalMilvusClient retrieval 层测试用 fake client
type fakeRetrievalMilvusClient struct {
	collections            map[string]bool
	hasCollectionDefault   bool
	insertedRows           int
	insertCallCount        int
	deleteCount            int64
	flushCalled            bool
	flushCount             int
	loadCount              int
	releaseCount           int
	closeCount             int
	insertErr              error
	deleteErr              error
	queryErr               error
	searchResults          []milvusclient.ResultSet
	hybridResults          []milvusclient.ResultSet
	searchErr              error
	hybridErr              error
	describeIndexErr       error
	describeCollectionResp *entity.Collection
}

func newFakeRetrievalMilvusClient() *fakeRetrievalMilvusClient {
	return &fakeRetrievalMilvusClient{
		collections:          map[string]bool{},
		hasCollectionDefault: true, // 对齐 Python 默认集合存在的场景（ensureLoaded 需要）
	}
}

func (f *fakeRetrievalMilvusClient) CreateCollection(ctx context.Context, option milvusclient.CreateCollectionOption, callOptions ...any) error {
	return nil
}
func (f *fakeRetrievalMilvusClient) DropCollection(ctx context.Context, option milvusclient.DropCollectionOption, callOptions ...any) error {
	return nil
}
func (f *fakeRetrievalMilvusClient) HasCollection(ctx context.Context, option milvusclient.HasCollectionOption, callOptions ...any) (bool, error) {
	name := option.Request().GetCollectionName()
	if has, ok := f.collections[name]; ok {
		return has, nil
	}
	return f.hasCollectionDefault, nil
}
func (f *fakeRetrievalMilvusClient) DescribeCollection(ctx context.Context, option milvusclient.DescribeCollectionOption, callOptions ...any) (*entity.Collection, error) {
	if f.describeCollectionResp != nil {
		return f.describeCollectionResp, nil
	}
	return &entity.Collection{}, nil
}
func (f *fakeRetrievalMilvusClient) ListCollections(ctx context.Context, option milvusclient.ListCollectionOption, callOptions ...any) ([]string, error) {
	return []string{}, nil
}
func (f *fakeRetrievalMilvusClient) GetCollectionStats(ctx context.Context, option milvusclient.GetCollectionOption, callOptions ...any) (map[string]string, error) {
	return map[string]string{"row_count": "10"}, nil
}
func (f *fakeRetrievalMilvusClient) Insert(ctx context.Context, option milvusclient.InsertOption, callOptions ...any) (milvusclient.InsertResult, error) {
	if f.insertErr != nil {
		return milvusclient.InsertResult{}, f.insertErr
	}
	f.insertCallCount++
	return milvusclient.InsertResult{}, nil
}
func (f *fakeRetrievalMilvusClient) Delete(ctx context.Context, option milvusclient.DeleteOption, callOptions ...any) (milvusclient.DeleteResult, error) {
	if f.deleteErr != nil {
		return milvusclient.DeleteResult{DeleteCount: 0}, f.deleteErr
	}
	return milvusclient.DeleteResult{DeleteCount: f.deleteCount}, nil
}
func (f *fakeRetrievalMilvusClient) Flush(ctx context.Context, option milvusclient.FlushOption, callOptions ...any) error {
	f.flushCalled = true
	f.flushCount++
	return nil
}
func (f *fakeRetrievalMilvusClient) Search(ctx context.Context, option milvusclient.SearchOption, callOptions ...any) ([]milvusclient.ResultSet, error) {
	if f.searchErr != nil {
		return nil, f.searchErr
	}
	return f.searchResults, nil
}
func (f *fakeRetrievalMilvusClient) HybridSearch(ctx context.Context, option milvusclient.HybridSearchOption, callOptions ...any) ([]milvusclient.ResultSet, error) {
	if f.hybridErr != nil {
		return nil, f.hybridErr
	}
	return f.hybridResults, nil
}
func (f *fakeRetrievalMilvusClient) Query(ctx context.Context, option milvusclient.QueryOption, callOptions ...any) (milvusclient.ResultSet, error) {
	return milvusclient.ResultSet{}, f.queryErr
}
func (f *fakeRetrievalMilvusClient) DescribeIndex(ctx context.Context, option milvusclient.DescribeIndexOption, callOptions ...any) (milvusclient.IndexDescription, error) {
	if f.describeIndexErr != nil {
		return milvusclient.IndexDescription{}, f.describeIndexErr
	}
	return milvusclient.IndexDescription{}, nil
}
func (f *fakeRetrievalMilvusClient) LoadCollection(ctx context.Context, option milvusclient.LoadCollectionOption, callOptions ...any) error {
	f.loadCount++
	return nil
}
func (f *fakeRetrievalMilvusClient) GetLoadState(ctx context.Context, option milvusclient.GetLoadStateOption, callOptions ...any) (entity.LoadState, error) {
	return entity.LoadState{State: entity.LoadStateLoaded}, nil
}
func (f *fakeRetrievalMilvusClient) ReleaseCollection(ctx context.Context, option milvusclient.ReleaseCollectionOption, callOptions ...any) error {
	f.releaseCount++
	return nil
}
func (f *fakeRetrievalMilvusClient) ListDatabase(ctx context.Context, option milvusclient.ListDatabaseOption, callOptions ...any) ([]string, error) {
	return []string{"default"}, nil
}
func (f *fakeRetrievalMilvusClient) CreateDatabase(ctx context.Context, option milvusclient.CreateDatabaseOption, callOptions ...any) error {
	return nil
}
func (f *fakeRetrievalMilvusClient) UseDatabase(ctx context.Context, option milvusclient.UseDatabaseOption, callOptions ...any) error {
	return nil
}
func (f *fakeRetrievalMilvusClient) Close(ctx context.Context) error {
	f.closeCount++
	return nil
}

// newTestMilvusStore 构建不注入 client 的测试 store（惰性客户端路径）
func newTestMilvusStore(t *testing.T) *MilvusVectorStore {
	t.Helper()
	config := common.VectorStoreConfig{
		StoreProvider:  common.StoreTypeMilvus,
		CollectionName: "test-collection",
		DatabaseName:   "default",
		DistanceMetric: common.DistanceMetricCosine,
	}
	vf := vector_fields.NewMilvusAUTO("embedding")
	s, err := NewMilvusVectorStore(config, "http://localhost:19530", "",
		"content", vf, "sparse_vector", "metadata", "document_id")
	require.NoError(t, err)
	require.NotNil(t, s)
	return s
}

// newTestStoreWithFake 构建注入 fake client 的 store
func newTestStoreWithFake(t *testing.T, fake *fakeRetrievalMilvusClient) *MilvusVectorStore {
	t.Helper()
	config := common.VectorStoreConfig{
		StoreProvider:  common.StoreTypeMilvus,
		CollectionName: "test-collection",
		DistanceMetric: common.DistanceMetricCosine,
	}
	s, err := NewMilvusVectorStore(config, "http://localhost:19530", "",
		"content", vector_fields.NewMilvusAUTO("embedding"), "sparse_vector", "metadata", "document_id",
		WithMilvusClientOption(fake))
	require.NoError(t, err)
	return s
}

// TestNewMilvusVectorStore_默认参数 对齐 Python MilvusVectorStore.__init__
func TestNewMilvusVectorStore_默认参数(t *testing.T) {
	s := newTestMilvusStore(t)
	assert.Equal(t, "test-collection", s.collectionName)
	assert.Equal(t, "COSINE", s.distanceMetric) // 对齐 Python: .upper()
	assert.Equal(t, "content", s.textField)
	assert.Equal(t, "embedding", s.vectorField.VectorFieldName())
	assert.Equal(t, "sparse_vector", s.sparseVectorField)
	assert.Equal(t, "metadata", s.metadataField)
	assert.Equal(t, "document_id", s.docIDField)
}

// TestNewMilvusVectorStore_距离度量归一化 对齐 Python: replace("dot","ip").replace("euclidean","l2").upper()
func TestNewMilvusVectorStore_距离度量归一化(t *testing.T) {
	tests := []struct {
		input    common.DistanceMetricKind
		expected string
	}{
		{common.DistanceMetricCosine, "COSINE"},
		{common.DistanceMetricEuclidean, "L2"},
		{common.DistanceMetricDot, "IP"},
	}
	for _, tt := range tests {
		t.Run(string(tt.input), func(t *testing.T) {
			config := common.VectorStoreConfig{
				StoreProvider:  common.StoreTypeMilvus,
				CollectionName: "test-collection",
				DistanceMetric: tt.input,
			}
			s, err := NewMilvusVectorStore(config, "http://localhost:19530", "",
				"content", vector_fields.NewMilvusAUTO("embedding"), "sparse_vector", "metadata", "document_id")
			require.NoError(t, err)
			assert.Equal(t, tt.expected, s.distanceMetric)
		})
	}
}

// TestNewMilvusVectorStore_constructConfig 对齐 Python: auto 时 _construct_config={} 否则 to_dict(stage="construct")
func TestNewMilvusVectorStore_constructConfig(t *testing.T) {
	s := newTestMilvusStore(t) // AUTO → 空 construct config + metric_type
	assert.Equal(t, "COSINE", s.constructConfig["metric_type"])
	assert.Len(t, s.constructConfig, 1) // auto 索引无其他参数

	// HNSW → construct 参数存在
	config := common.VectorStoreConfig{
		StoreProvider:  common.StoreTypeMilvus,
		CollectionName: "test-collection",
		DistanceMetric: common.DistanceMetricCosine,
	}
	hnsw := vector_fields.NewMilvusHNSW("embedding", 30, 360, 2.0)
	s2, err := NewMilvusVectorStore(config, "http://localhost:19530", "",
		"content", hnsw, "sparse_vector", "metadata", "document_id")
	require.NoError(t, err)
	assert.Equal(t, 30, s2.constructConfig["M"])
	assert.Equal(t, 360, s2.constructConfig["EfConstruction"])
	assert.Equal(t, "COSINE", s2.constructConfig["metric_type"])
}

// TestNewMilvusVectorStore_HNSW搜索配置 对齐 Python: _search_config = to_dict(stage="search")
func TestNewMilvusVectorStore_HNSW搜索配置(t *testing.T) {
	config := common.VectorStoreConfig{
		StoreProvider:  common.StoreTypeMilvus,
		CollectionName: "test-collection",
		DistanceMetric: common.DistanceMetricCosine,
	}
	hnsw := vector_fields.NewMilvusHNSW("embedding", 30, 360, 2.0)
	s, err := NewMilvusVectorStore(config, "http://localhost:19530", "",
		"content", hnsw, "sparse_vector", "metadata", "document_id")
	require.NoError(t, err)
	assert.Equal(t, 2.0, s.searchConfig["EfSearchFactor"])
}

// TestNewMilvusVectorStore_vectorField为nil 对齐 Python: raise VECTOR_FIELD_INVALID
func TestNewMilvusVectorStore_vectorField为nil(t *testing.T) {
	config := common.VectorStoreConfig{
		StoreProvider:  common.StoreTypeMilvus,
		CollectionName: "test-collection",
	}
	_, err := NewMilvusVectorStore(config, "http://localhost:19530", "",
		"content", nil, "sparse_vector", "metadata", "document_id")
	assert.Error(t, err)
}

// TestNewMilvusVectorStore_URI为空 对齐 Python: 连接失败（Go 显式校验）
func TestNewMilvusVectorStore_URI为空(t *testing.T) {
	config := common.VectorStoreConfig{
		StoreProvider:  common.StoreTypeMilvus,
		CollectionName: "test-collection",
	}
	_, err := NewMilvusVectorStore(config, "", "",
		"content", vector_fields.NewMilvusAUTO("embedding"), "sparse_vector", "metadata", "document_id")
	assert.Error(t, err)
}

// TestNewMilvusVectorStore_惰性客户端 对齐 Python：构造时创建，Go 改为惰性便于测试
func TestNewMilvusVectorStore_惰性客户端(t *testing.T) {
	s := newTestMilvusStore(t)
	assert.Nil(t, s.client) // 未注入时为 nil，通过 getClient 惰性创建
}

// TestNewMilvusVectorStore_注入客户端 对齐 Python: client property 返回底层客户端
func TestNewMilvusVectorStore_注入客户端(t *testing.T) {
	fake := newFakeRetrievalMilvusClient()
	s := newTestStoreWithFake(t, fake)
	c, err := s.Client(context.Background())
	require.NoError(t, err)
	assert.Same(t, fake, c)
}

// TestNewMilvusVectorStore_DistanceMetric 对齐 Python distance_metric property
func TestNewMilvusVectorStore_DistanceMetric(t *testing.T) {
	s := newTestMilvusStore(t)
	assert.Equal(t, "COSINE", s.DistanceMetric())
}

// TestMilvusVectorStore_Add 对齐 Python add：批量写入+每100条日志+flush
func TestMilvusVectorStore_Add(t *testing.T) {
	fake := newFakeRetrievalMilvusClient()
	s := newTestStoreWithFake(t, fake)

	// 数据中含向量字段
	data := []map[string]any{
		{"chunk_id": "c1", "document_id": "d1", "content": "text1", "metadata": map[string]any{"k": "v"}, "embedding": []float64{0.1, 0.2}},
		{"chunk_id": "c2", "document_id": "d1", "content": "text2", "metadata": map[string]any{}, "embedding": []float64{0.3, 0.4}},
	}
	err := s.Add(context.Background(), data)
	require.NoError(t, err)
	assert.True(t, fake.flushCalled) // 对齐 Python: 最后 flush
}

// TestMilvusVectorStore_Add_空数据 对齐 Python: 空数据不 insert 但仍 flush
func TestMilvusVectorStore_Add_空数据(t *testing.T) {
	fake := newFakeRetrievalMilvusClient()
	s := newTestStoreWithFake(t, fake)

	err := s.Add(context.Background(), nil)
	require.NoError(t, err)
	assert.True(t, fake.flushCalled)
}

// TestMilvusVectorStore_Add_自定义批大小 对齐 Python: batch_size 可配置
func TestMilvusVectorStore_Add_自定义批大小(t *testing.T) {
	fake := newFakeRetrievalMilvusClient()
	s := newTestStoreWithFake(t, fake)

	data := make([]map[string]any, 10)
	for i := range data {
		data[i] = map[string]any{"chunk_id": fmt.Sprintf("c%d", i), "embedding": []float64{0.1}}
	}
	err := s.Add(context.Background(), data, WithStoreBatchSize(3))
	require.NoError(t, err)
	assert.Equal(t, 4, fake.insertCallCount) // ceil(10/3) = 4 批
}

// TestMilvusVectorStore_Add_插入失败 对齐 Python: insert 抛错向上传播
func TestMilvusVectorStore_Add_插入失败(t *testing.T) {
	fake := newFakeRetrievalMilvusClient()
	fake.insertErr = fmt.Errorf("insert failed")
	s := newTestStoreWithFake(t, fake)

	err := s.Add(context.Background(), []map[string]any{{"chunk_id": "c1"}})
	assert.Error(t, err)
}

// TestMilvusVectorStore_Delete 对齐 Python delete：按 filterExpr 删除+flush
func TestMilvusVectorStore_Delete(t *testing.T) {
	fake := newFakeRetrievalMilvusClient()
	fake.deleteCount = 3
	s := newTestStoreWithFake(t, fake)

	ok, err := s.Delete(context.Background(), nil, map[string]any{"document_id": "d1"})
	require.NoError(t, err)
	assert.True(t, ok) // delete_count > 0
	assert.True(t, fake.flushCalled)
}

// TestMilvusVectorStore_Delete_按ids 对齐 Python: ids 非空时按主键删除
func TestMilvusVectorStore_Delete_按ids(t *testing.T) {
	fake := newFakeRetrievalMilvusClient()
	fake.deleteCount = 1
	s := newTestStoreWithFake(t, fake)

	ok, err := s.Delete(context.Background(), []string{"id1", "id2"}, nil)
	require.NoError(t, err)
	assert.True(t, ok)
}

// TestMilvusVectorStore_Delete_零删除 对齐 Python: delete_count == 0 → False
func TestMilvusVectorStore_Delete_零删除(t *testing.T) {
	fake := newFakeRetrievalMilvusClient()
	fake.deleteCount = 0
	s := newTestStoreWithFake(t, fake)

	ok, err := s.Delete(context.Background(), nil, map[string]any{"document_id": "d1"})
	require.NoError(t, err)
	assert.False(t, ok)
}

// TestMilvusVectorStore_Delete_无条件 对齐 Python: ids 和 filter 都为空时直接返回
func TestMilvusVectorStore_Delete_无条件(t *testing.T) {
	fake := newFakeRetrievalMilvusClient()
	s := newTestStoreWithFake(t, fake)

	ok, err := s.Delete(context.Background(), nil, nil)
	require.NoError(t, err)
	assert.False(t, ok)
}

// TestMilvusVectorStore_Delete_异常 对齐 Python: except → logger.error + return False
func TestMilvusVectorStore_Delete_异常(t *testing.T) {
	fake := newFakeRetrievalMilvusClient()
	fake.deleteErr = fmt.Errorf("delete failed")
	s := newTestStoreWithFake(t, fake)

	ok, err := s.Delete(context.Background(), nil, map[string]any{"document_id": "d1"})
	// 对齐 Python: 异常时不抛错，返回 False（Go err 返回原始错误信息用 error 通道，此处对齐 Python 语义返回 nil error + false）
	require.NoError(t, err)
	assert.False(t, ok)
}

// TestMilvusVectorStore_TableExists 对齐 Python table_exists
func TestMilvusVectorStore_TableExists(t *testing.T) {
	fake := newFakeRetrievalMilvusClient()
	s := newTestStoreWithFake(t, fake)

	ok, err := s.TableExists(context.Background(), "some_table")
	require.NoError(t, err)
	assert.True(t, ok) // fake hasCollectionDefault 返回 true
}

// TestMilvusVectorStore_DeleteTable 对齐 Python delete_table
func TestMilvusVectorStore_DeleteTable(t *testing.T) {
	fake := newFakeRetrievalMilvusClient()
	s := newTestStoreWithFake(t, fake)

	err := s.DeleteTable(context.Background(), "some_table")
	assert.NoError(t, err)
}

// TestMilvusVectorStore_ensureLoaded 对齐 Python _ensure_loaded：首次加载后缓存
func TestMilvusVectorStore_ensureLoaded(t *testing.T) {
	fake := newFakeRetrievalMilvusClient()
	s := newTestStoreWithFake(t, fake)

	err := s.ensureLoaded(context.Background())
	require.NoError(t, err)
	assert.True(t, s.collectionLoaded)
	// 二次调用不重复 load（fake loadCount 不增长）
	err = s.ensureLoaded(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, fake.loadCount)
}

// TestMilvusVectorStore_ensureLoaded_集合不存在 对齐 Python: has_collection 为 False 时跳过
func TestMilvusVectorStore_ensureLoaded_集合不存在(t *testing.T) {
	fake := newFakeRetrievalMilvusClient()
	fake.hasCollectionDefault = false
	s := newTestStoreWithFake(t, fake)

	err := s.ensureLoaded(context.Background())
	require.NoError(t, err)
	assert.False(t, s.collectionLoaded)
	assert.Equal(t, 0, fake.loadCount)
}

// TestMilvusVectorStore_Close 对齐 Python close(release_collection=False)
func TestMilvusVectorStore_Close(t *testing.T) {
	fake := newFakeRetrievalMilvusClient()
	s := newTestStoreWithFake(t, fake)

	s.Close() // 不释放集合
	assert.Equal(t, 0, fake.releaseCount)
	assert.Equal(t, 1, fake.closeCount)
}

// TestMilvusVectorStore_CloseWithRelease 对齐 Python close(release_collection=True)
func TestMilvusVectorStore_CloseWithRelease(t *testing.T) {
	fake := newFakeRetrievalMilvusClient()
	s := newTestStoreWithFake(t, fake)

	s.CloseWithRelease(true)
	assert.Equal(t, 1, fake.releaseCount)
	assert.Equal(t, 1, fake.closeCount)
	assert.False(t, s.collectionLoaded)
}

// TestBuildMilvusFilterExpr 对齐 Python search 中的 filter 构建（key 排序保证确定性）
func TestBuildMilvusFilterExpr(t *testing.T) {
	assert.Equal(t, "", buildMilvusFilterExpr(nil))
	assert.Equal(t, `document_id == "d1"`, buildMilvusFilterExpr(map[string]any{"document_id": "d1"}))
	// 字符串值带引号时转义
	assert.Equal(t, `name == "a\"b"`, buildMilvusFilterExpr(map[string]any{"name": `a"b`}))
	// 非字符串直接拼接
	assert.Equal(t, "count == 5", buildMilvusFilterExpr(map[string]any{"count": 5}))
	// 多条件按 key 排序连接
	assert.Equal(t, `a == "1" && b == 2`, buildMilvusFilterExpr(map[string]any{"b": 2, "a": "1"}))
}
