package vector_store

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/milvus-io/milvus/client/v2/column"
	"github.com/milvus-io/milvus/client/v2/entity"
	milvusclient "github.com/milvus-io/milvus/client/v2/milvusclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/vector"
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

// buildTestResultSet 构建测试用搜索结果集
func buildTestResultSet(fieldValues map[string][]any, scores []float32) milvusclient.ResultSet {
	// 通过真实 column 构造 Fields
	var fields milvusclient.DataSet
	for name, values := range fieldValues {
		if len(values) == 0 {
			continue
		}
		switch values[0].(type) {
		case string:
			strs := make([]string, len(values))
			for i, v := range values {
				strs[i], _ = v.(string)
			}
			fields = append(fields, column.NewColumnVarChar(name, strs))
		case map[string]any:
			jsonVals := make([][]byte, len(values))
			for i, v := range values {
				bs, _ := json.Marshal(v)
				jsonVals[i] = bs
			}
			fields = append(fields, column.NewColumnJSONBytes(name, jsonVals))
		}
	}
	return milvusclient.ResultSet{
		ResultCount: len(scores),
		Fields:      fields,
		Scores:      scores,
	}
}

// TestMilvusVectorStore_Search 对齐 Python search：向量搜索+分数归一化
func TestMilvusVectorStore_Search(t *testing.T) {
	fake := newFakeRetrievalMilvusClient()
	fake.searchResults = []milvusclient.ResultSet{buildTestResultSet(
		map[string][]any{
			"content":     {"hello"},
			"metadata":    {map[string]any{"k": "v"}},
			"document_id": {"d1"},
			"chunk_id":    {"c1"},
		},
		[]float32{0.9},
	)}
	s := newTestStoreWithFake(t, fake)

	results, err := s.Search(context.Background(), []float64{0.1, 0.2}, 5, nil)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "hello", results[0].Text)
	assert.Equal(t, "d1", results[0].Metadata["doc_id"]) // 对齐 Python: doc_id 回填到 metadata
}

// TestMilvusVectorStore_Search_带过滤 对齐 Python: filters 构建 filter expr
func TestMilvusVectorStore_Search_带过滤(t *testing.T) {
	fake := newFakeRetrievalMilvusClient()
	fake.searchResults = []milvusclient.ResultSet{buildTestResultSet(
		map[string][]any{"content": {"hello"}}, []float32{0.9})}
	s := newTestStoreWithFake(t, fake)

	results, err := s.Search(context.Background(), []float64{0.1}, 5, map[string]any{"document_id": "d1"})
	require.NoError(t, err)
	assert.Len(t, results, 1)
}

// TestMilvusVectorStore_Search_空结果 对齐 Python: results 为空返回 []
func TestMilvusVectorStore_Search_空结果(t *testing.T) {
	fake := newFakeRetrievalMilvusClient()
	s := newTestStoreWithFake(t, fake)

	results, err := s.Search(context.Background(), []float64{0.1}, 5, nil)
	require.NoError(t, err)
	assert.Empty(t, results)
}

// TestMilvusVectorStore_Search_L2分数 对齐 Python: convert_l2_squared
func TestMilvusVectorStore_Search_L2分数(t *testing.T) {
	config := common.VectorStoreConfig{
		StoreProvider:  common.StoreTypeMilvus,
		CollectionName: "test-collection",
		DistanceMetric: common.DistanceMetricEuclidean,
	}
	fake := newFakeRetrievalMilvusClient()
	fake.searchResults = []milvusclient.ResultSet{buildTestResultSet(
		map[string][]any{"content": {"hello"}}, []float32{0.5})}
	s, err := NewMilvusVectorStore(config, "http://localhost:19530", "",
		"content", vector_fields.NewMilvusAUTO("embedding"), "sparse_vector", "metadata", "document_id",
		WithMilvusClientOption(fake))
	require.NoError(t, err)

	results, err := s.Search(context.Background(), []float64{0.1}, 5, nil)
	require.NoError(t, err)
	require.Len(t, results, 1)
	// L2: convert_l2_squared(0.5)（foundation utils 默认 maxDist）
	expected := vector.ConvertL2Squared(0.5, 4.0)
	assert.InDelta(t, expected, results[0].Score, 1e-6)
	assert.InDelta(t, 0.5, results[0].Metadata["raw_score"].(float64), 1e-6) // 对齐 Python: raw_score 写入 metadata
}

// TestMilvusVectorStore_Search_COSINE分数 对齐 Python: convert_cosine_similarity
func TestMilvusVectorStore_Search_COSINE分数(t *testing.T) {
	fake := newFakeRetrievalMilvusClient()
	fake.searchResults = []milvusclient.ResultSet{buildTestResultSet(
		map[string][]any{"content": {"hello"}}, []float32{0.8})}
	s := newTestStoreWithFake(t, fake) // COSINE

	results, err := s.Search(context.Background(), []float64{0.1}, 5, nil)
	require.NoError(t, err)
	require.Len(t, results, 1)
	expected := vector.ConvertCosineSimilarity(0.8)
	assert.InDelta(t, expected, results[0].Score, 1e-6)
	assert.InDelta(t, expected, results[0].Metadata["raw_score_scaled"].(float64), 1e-6)
}

// TestMilvusVectorStore_Search_IP分数 对齐 Python: convert_ip_similarity
func TestMilvusVectorStore_Search_IP分数(t *testing.T) {
	config := common.VectorStoreConfig{
		StoreProvider:  common.StoreTypeMilvus,
		CollectionName: "test-collection",
		DistanceMetric: common.DistanceMetricDot,
	}
	fake := newFakeRetrievalMilvusClient()
	fake.searchResults = []milvusclient.ResultSet{buildTestResultSet(
		map[string][]any{"content": {"hello"}}, []float32{1.5})}
	s, err := NewMilvusVectorStore(config, "http://localhost:19530", "",
		"content", vector_fields.NewMilvusAUTO("embedding"), "sparse_vector", "metadata", "document_id",
		WithMilvusClientOption(fake))
	require.NoError(t, err)

	results, err := s.Search(context.Background(), []float64{0.1}, 5, nil)
	require.NoError(t, err)
	expected := vector.ConvertIPSimilarity(1.5)
	assert.InDelta(t, expected, results[0].Score, 1e-6)
}

// TestMilvusVectorStore_SparseSearch 对齐 Python sparse_search（BM25）
func TestMilvusVectorStore_SparseSearch(t *testing.T) {
	fake := newFakeRetrievalMilvusClient()
	fake.searchResults = []milvusclient.ResultSet{buildTestResultSet(
		map[string][]any{"content": {"hello"}, "document_id": {"d1"}},
		[]float32{2.5})}
	s := newTestStoreWithFake(t, fake)

	results, err := s.SparseSearch(context.Background(), "query text", 5, nil)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.InDelta(t, 2.5, results[0].Score, 1e-6) // sparse: final_score = raw_score
}

// TestMilvusVectorStore_SparseSearch_失败返回空 对齐 Python: except → warning + return []
func TestMilvusVectorStore_SparseSearch_失败返回空(t *testing.T) {
	fake := newFakeRetrievalMilvusClient()
	fake.searchErr = fmt.Errorf("bm25 failed")
	s := newTestStoreWithFake(t, fake)

	results, err := s.SparseSearch(context.Background(), "query", 5, nil)
	require.NoError(t, err) // 对齐 Python: 失败不抛错
	assert.Empty(t, results)
}

// TestMilvusVectorStore_HybridSearch 对齐 Python hybrid_search：原生 HybridSearch + RRF
func TestMilvusVectorStore_HybridSearch(t *testing.T) {
	fake := newFakeRetrievalMilvusClient()
	fake.hybridResults = []milvusclient.ResultSet{buildTestResultSet(
		map[string][]any{"content": {"hello"}, "document_id": {"d1"}},
		[]float32{0.8})}
	s := newTestStoreWithFake(t, fake)

	results, err := s.HybridSearch(context.Background(), "query", []float64{0.1, 0.2}, 5, 0.5, nil)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "hello", results[0].Text)
	assert.InDelta(t, 0.8, results[0].Score, 1e-6) // hybrid: final_score = raw_score
}

// TestMilvusVectorStore_HybridSearch_降级 对齐 Python: 失败 fallback 到分别搜索
func TestMilvusVectorStore_HybridSearch_降级(t *testing.T) {
	fake := newFakeRetrievalMilvusClient()
	fake.hybridErr = fmt.Errorf("hybrid failed")
	fake.searchResults = []milvusclient.ResultSet{buildTestResultSet(
		map[string][]any{"content": {"dense result"}}, []float32{0.9})}
	s := newTestStoreWithFake(t, fake)

	results, err := s.HybridSearch(context.Background(), "query", []float64{0.1, 0.2}, 5, 0.5, nil)
	require.NoError(t, err)
	assert.NotEmpty(t, results) // fallback 生效：至少有 dense 结果
}

// TestMilvusVectorStore_HybridSearch_无向量 对齐 Python: query_vector 为 None 时只用 sparse
func TestMilvusVectorStore_HybridSearch_无向量(t *testing.T) {
	fake := newFakeRetrievalMilvusClient()
	fake.hybridResults = []milvusclient.ResultSet{buildTestResultSet(
		map[string][]any{"content": {"sparse result"}}, []float32{0.7})}
	s := newTestStoreWithFake(t, fake)

	results, err := s.HybridSearch(context.Background(), "query", nil, 5, 0.5, nil)
	require.NoError(t, err)
	assert.NotEmpty(t, results)
}

// TestMilvusVectorStore_HybridSearch_降级两路都失败 对齐 Python: rrf_fusion 空输入
func TestMilvusVectorStore_HybridSearch_降级两路都失败(t *testing.T) {
	fake := newFakeRetrievalMilvusClient()
	fake.hybridErr = fmt.Errorf("hybrid failed")
	fake.searchErr = fmt.Errorf("search failed") // sparse 也失败 → 两路都空
	s := newTestStoreWithFake(t, fake)

	results, err := s.HybridSearch(context.Background(), "query", []float64{0.1}, 5, 0.5, nil)
	require.NoError(t, err)
	assert.Empty(t, results)
}

// TestMilvusVectorStore_milvusResultToSearchResults 对齐 Python _milvus_result_to_search_results
func TestMilvusVectorStore_milvusResultToSearchResults(t *testing.T) {
	s := newTestMilvusStore(t)
	rs := buildTestResultSet(
		map[string][]any{
			"content":     {"text1", "text2"},
			"metadata":    {map[string]any{"k": "v1"}, map[string]any{"k": "v2"}},
			"document_id": {"d1", "d2"},
			"chunk_id":    {"c1", "c2"},
		},
		[]float32{0.9, 0.8},
	)
	results := s.milvusResultToSearchResults(rs, "vector")
	require.Len(t, results, 2)
	assert.Equal(t, "d1", results[0].Metadata["doc_id"])
	assert.Equal(t, "c1", results[0].Metadata["chunk_id"]) // 对齐 Python: chunk_id 写入 metadata
	assert.InDelta(t, 0.9, results[0].Metadata["raw_score"].(float64), 1e-6)
}

// TestMilvusVectorStore_milvusResultToSearchResults_metadata字符串 对齐 Python: metadata 为 str 时 json.loads
func TestMilvusVectorStore_milvusResultToSearchResults_metadata字符串(t *testing.T) {
	s := newTestMilvusStore(t)
	// metadata 列用 JSON bytes 模拟（Get 返回的可能被序列化为字符串）
	rs := milvusclient.ResultSet{
		ResultCount: 1,
		Fields: milvusclient.DataSet{
			column.NewColumnVarChar("content", []string{"text"}),
			column.NewColumnVarChar("metadata", []string{`{"k":"v"}`}), // 字符串形态的 metadata
			column.NewColumnVarChar("document_id", []string{"d1"}),
		},
		Scores: []float32{0.9},
	}
	results := s.milvusResultToSearchResults(rs, "sparse")
	require.Len(t, results, 1)
	assert.Equal(t, "v", results[0].Metadata["k"])
}

// TestMilvusVectorStore_getSearchParams 对齐 Python get_search_params：efSearchFactor → ef
func TestMilvusVectorStore_getSearchParams(t *testing.T) {
	config := common.VectorStoreConfig{
		StoreProvider:  common.StoreTypeMilvus,
		CollectionName: "test-collection",
		DistanceMetric: common.DistanceMetricCosine,
	}
	hnsw := vector_fields.NewMilvusHNSW("embedding", 30, 360, 2.0)
	s, err := NewMilvusVectorStore(config, "http://localhost:19530", "",
		"content", hnsw, "sparse_vector", "metadata", "document_id")
	require.NoError(t, err)

	params := s.getSearchParams(10)
	// 对齐 Python: ef = round(top_k * efSearchFactor) = round(10 * 2.0) = 20
	assert.Equal(t, float64(20), params["ef"])
	assert.NotContains(t, params, "EfSearchFactor") // pop 掉
}
