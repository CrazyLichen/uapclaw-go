package persistence

import (
	"context"
	"testing"

	"github.com/milvus-io/milvus/client/v2/column"
	"github.com/milvus-io/milvus/client/v2/entity"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	milvusclient "github.com/milvus-io/milvus/client/v2/milvusclient"
)

// ──────────────────────────── 结构体 ────────────────────────────

// fakeMilvusClient 用于单元测试的模拟 Milvus 客户端。
type fakeMilvusClient struct {
	// collections 模拟的集合数据：collection → namespace → id → map[string]any
	collections map[string]map[string]map[string]map[string]any
	// hasCollectionResult HasCollection 返回值
	hasCollectionResult bool
	// hasCollectionErr HasCollection 错误
	hasCollectionErr error
	// closed 是否已关闭
	closed bool
}

// ──────────────────────────── 导出函数 ────────────────────────────

func newFakeMilvusClient() *fakeMilvusClient {
	return &fakeMilvusClient{
		collections: make(map[string]map[string]map[string]map[string]any),
	}
}

// ──────────────────────────── milvusClient 接口实现 ────────────────────────────

func (f *fakeMilvusClient) CreateCollection(ctx context.Context, option milvusclient.CreateCollectionOption, callOptions ...any) error {
	return nil
}

func (f *fakeMilvusClient) HasCollection(ctx context.Context, option milvusclient.HasCollectionOption, callOptions ...any) (bool, error) {
	if f.hasCollectionErr != nil {
		return false, f.hasCollectionErr
	}
	return f.hasCollectionResult, nil
}

func (f *fakeMilvusClient) DescribeCollection(ctx context.Context, option milvusclient.DescribeCollectionOption, callOptions ...any) (*entity.Collection, error) {
	return nil, nil
}

func (f *fakeMilvusClient) Insert(ctx context.Context, option milvusclient.InsertOption, callOptions ...any) (milvusclient.InsertResult, error) {
	return milvusclient.InsertResult{}, nil
}

func (f *fakeMilvusClient) Search(ctx context.Context, option milvusclient.SearchOption, callOptions ...any) ([]milvusclient.ResultSet, error) {
	return nil, nil
}

func (f *fakeMilvusClient) Query(ctx context.Context, option milvusclient.QueryOption, callOptions ...any) (milvusclient.ResultSet, error) {
	return milvusclient.ResultSet{}, nil
}

func (f *fakeMilvusClient) Delete(ctx context.Context, option milvusclient.DeleteOption, callOptions ...any) (milvusclient.DeleteResult, error) {
	return milvusclient.DeleteResult{}, nil
}

func (f *fakeMilvusClient) LoadCollection(ctx context.Context, option milvusclient.LoadCollectionOption, callOptions ...any) error {
	return nil
}

func (f *fakeMilvusClient) Flush(ctx context.Context, option milvusclient.FlushOption, callOptions ...any) error {
	return nil
}

func (f *fakeMilvusClient) CreateIndex(ctx context.Context, option milvusclient.CreateIndexOption, callOptions ...any) error {
	return nil
}

func (f *fakeMilvusClient) Close(ctx context.Context) error {
	f.closed = true
	return nil
}

// ──────────────────────────── 测试函数 ────────────────────────────

func TestNewMilvusConnectorImpl_默认值(t *testing.T) {
	m := NewMilvusConnectorImpl()
	assert.Equal(t, milvusDefaultCollectionName, m.collectionName)
	assert.Equal(t, milvusDefaultAlias, m.alias)
	assert.Equal(t, milvusDefaultMetricType, m.metricType)
	assert.Equal(t, 0, m.dim)
}

func TestNewMilvusConnectorImpl_自定义选项(t *testing.T) {
	m := NewMilvusConnectorImpl(
		WithConnectorHost("my-host"),
		WithConnectorPort(19530),
		WithConnectorCollection("test_coll"),
		WithConnectorDim(768),
		WithConnectorMetricType("L2"),
	)
	assert.Equal(t, "my-host", m.host)
	assert.Equal(t, 19530, m.port)
	assert.Equal(t, "test_coll", m.collectionName)
	assert.Equal(t, 768, m.dim)
	assert.Equal(t, "L2", m.metricType)
}

func TestTruncate_UTF8安全截断(t *testing.T) {
	// ASCII 不超长
	assert.Equal(t, "hello", Truncate("hello", 10))
	// ASCII 超长
	assert.Equal(t, "hel", Truncate("hello", 3))
	// UTF-8 多字节字符
	text := "你好世界"
	result := Truncate(text, 6) // "你好" = 6 bytes
	assert.Equal(t, "你好", result)
	// 截断到中间字节时回退到完整字符边界
	result = Truncate(text, 7) // "你好" + 3 bytes of "世"
	assert.Equal(t, "你好", result) // 回退到完整字符
}

func TestTruncate_空字符串(t *testing.T) {
	assert.Equal(t, "", Truncate("", 10))
}

func TestIDsExpr(t *testing.T) {
	result := IDsExpr([]string{"a", "b", "c"})
	assert.Equal(t, `id in ["a", "b", "c"]`, result)
}

func TestIDsExpr_空列表(t *testing.T) {
	result := IDsExpr([]string{})
	assert.Equal(t, `id in []`, result)
}

func TestIDsExpr_单元素(t *testing.T) {
	result := IDsExpr([]string{"only"})
	assert.Equal(t, `id in ["only"]`, result)
}

func TestExtractEmbedding_float32(t *testing.T) {
	emb := extractEmbedding([]float32{1.0, 2.0, 3.0})
	assert.Equal(t, []float32{1.0, 2.0, 3.0}, emb)
}

func TestExtractEmbedding_float64(t *testing.T) {
	emb := extractEmbedding([]float64{1.0, 2.0, 3.0})
	assert.Equal(t, []float32{1.0, 2.0, 3.0}, emb)
}

func TestExtractEmbedding_nil(t *testing.T) {
	assert.Nil(t, extractEmbedding(nil))
	assert.Nil(t, extractEmbedding("not an embedding"))
}

func TestMapMetricType(t *testing.T) {
	assert.Equal(t, entity.COSINE, mapMetricType("COSINE"))
	assert.Equal(t, entity.L2, mapMetricType("L2"))
	assert.Equal(t, entity.IP, mapMetricType("IP"))
	assert.Equal(t, entity.COSINE, mapMetricType("unknown"))
}

func TestSaveToDB_空数据(t *testing.T) {
	m := NewMilvusConnectorImpl()
	m.SetClient(newFakeMilvusClient())

	err := m.SaveToDB("ns", map[string]any{})
	assert.NoError(t, err)
}

func TestSaveToDB_跳过无Embedding(t *testing.T) {
	m := NewMilvusConnectorImpl(WithConnectorDim(3))
	fake := newFakeMilvusClient()
	m.SetClient(fake)

	data := map[string]any{
		"node1": map[string]any{"id": "node1", "content": "hello", "embedding": []float32{1, 2, 3}},
		"node2": map[string]any{"id": "node2", "content": "world"}, // 无 embedding
	}

	err := m.SaveToDB("ns", data)
	assert.NoError(t, err)
}

func TestSaveToDB_无Dim信息(t *testing.T) {
	m := NewMilvusConnectorImpl() // dim=0
	fake := newFakeMilvusClient()
	m.SetClient(fake)

	data := map[string]any{
		"node1": map[string]any{"id": "node1", "content": "hello"}, // 无 embedding
	}

	err := m.SaveToDB("ns", data)
	assert.NoError(t, err) // 应跳过（无 dim 信息）
}

func TestExists_集合不存在(t *testing.T) {
	m := NewMilvusConnectorImpl()
	fake := newFakeMilvusClient()
	fake.hasCollectionResult = false
	m.SetClient(fake)

	result := m.Exists("ns")
	assert.False(t, result)
}

func TestDelete_集合不存在(t *testing.T) {
	m := NewMilvusConnectorImpl()
	fake := newFakeMilvusClient()
	fake.hasCollectionResult = false
	m.SetClient(fake)

	result := m.Delete("ns")
	assert.False(t, result)
}

func TestDeleteNodes_空列表(t *testing.T) {
	m := NewMilvusConnectorImpl()
	fake := newFakeMilvusClient()
	m.SetClient(fake)

	result := m.DeleteNodes("ns", []string{})
	assert.True(t, result)
}

func TestClose(t *testing.T) {
	m := NewMilvusConnectorImpl()
	fake := newFakeMilvusClient()
	m.SetClient(fake)

	m.Close()
	assert.True(t, fake.closed)
}

func TestSetClient(t *testing.T) {
	m := NewMilvusConnectorImpl()
	fake := newFakeMilvusClient()
	m.SetClient(fake)

	m.mu.RLock()
	c := m.client
	m.mu.RUnlock()
	assert.NotNil(t, c)
}

func TestProbeReachable_成功(t *testing.T) {
	m := NewMilvusConnectorImpl()
	fake := newFakeMilvusClient()
	fake.hasCollectionResult = true
	m.SetClient(fake)

	result := m.ProbeReachable(context.Background())
	assert.True(t, result)
}

func TestProbeReachable_失败(t *testing.T) {
	m := NewMilvusConnectorImpl()
	fake := newFakeMilvusClient()
	fake.hasCollectionErr = context.DeadlineExceeded
	m.SetClient(fake)

	result := m.ProbeReachable(context.Background())
	assert.False(t, result)
}

func TestHost_Port_Dim_Accessors(t *testing.T) {
	m := NewMilvusConnectorImpl(
		WithConnectorHost("myhost"),
		WithConnectorPort(19530),
		WithConnectorDim(768),
		WithConnectorCollection("test"),
	)
	assert.Equal(t, "myhost", m.Host())
	assert.Equal(t, 19530, m.Port())
	assert.Equal(t, 768, m.Dim())
	assert.Equal(t, "test", m.CollectionName())
}

func TestFindColumn(t *testing.T) {
	cols := milvusclient.DataSet{
		column.NewColumnVarChar("id", []string{"a", "b"}),
		column.NewColumnVarChar("content", []string{"hello", "world"}),
	}

	idCol := findColumn(cols, "id")
	require.NotNil(t, idCol)
	assert.Equal(t, "id", idCol.Name())

	contentCol := findColumn(cols, "content")
	require.NotNil(t, contentCol)
	assert.Equal(t, "content", contentCol.Name())

	missingCol := findColumn(cols, "missing")
	assert.Nil(t, missingCol)
}

func TestGetColumnString(t *testing.T) {
	col := column.NewColumnVarChar("id", []string{"a", "b", "c"})

	assert.Equal(t, "a", getColumnString(col, 0))
	assert.Equal(t, "b", getColumnString(col, 1))
	assert.Equal(t, "", getColumnString(col, 99)) // 越界
	assert.Equal(t, "", getColumnString(nil, 0))  // nil
}

func TestGetColumnAny(t *testing.T) {
	col := column.NewColumnVarChar("id", []string{"a"})

	val := getColumnAny(col, 0)
	assert.Equal(t, "a", val)

	assert.Nil(t, getColumnAny(col, 99)) // 越界
	assert.Nil(t, getColumnAny(nil, 0))  // nil
}

func TestMapsToJSONBytes(t *testing.T) {
	maps := []map[string]any{
		{"key": "value"},
		{"num": 42},
	}

	result := mapsToJSONBytes(maps)
	assert.Len(t, result, 2)
	assert.Contains(t, string(result[0]), "key")
	assert.Contains(t, string(result[1]), "num")
}

func TestMapsToJSONBytes_空(t *testing.T) {
	result := mapsToJSONBytes([]map[string]any{})
	assert.Len(t, result, 0)
}
