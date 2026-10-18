package vector_store

import (
	"context"
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
	deleteCount            int64
	flushCalled            bool
	flushCount             int
	insertErr              error
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
	return milvusclient.InsertResult{}, nil
}
func (f *fakeRetrievalMilvusClient) Delete(ctx context.Context, option milvusclient.DeleteOption, callOptions ...any) (milvusclient.DeleteResult, error) {
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
	return nil
}
func (f *fakeRetrievalMilvusClient) GetLoadState(ctx context.Context, option milvusclient.GetLoadStateOption, callOptions ...any) (entity.LoadState, error) {
	return entity.LoadState{State: entity.LoadStateLoaded}, nil
}
func (f *fakeRetrievalMilvusClient) ReleaseCollection(ctx context.Context, option milvusclient.ReleaseCollectionOption, callOptions ...any) error {
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
