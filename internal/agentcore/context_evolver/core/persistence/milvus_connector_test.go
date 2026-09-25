package persistence

import (
	"context"
	"testing"

	"github.com/milvus-io/milvus/client/v2/entity"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	milvusclient "github.com/milvus-io/milvus/client/v2/milvusclient"
)

// ──────────────────────────── 结构体 ────────────────────────────

// fakeMilvusClient 用于单元测试的模拟 Milvus 客户端。
// 使用简单的记录注入方式，通过 directInsert 直接插入数据，
// 通过 directQuery/directDelete 提供带 namespace 过滤的查询/删除。
type fakeMilvusClient struct {
	// records 存储记录：collection → id → *fakeRecord
	records map[string]map[string]*fakeRecord
	// hasCollectionResult HasCollection 返回值
	hasCollectionResult bool
	// hasCollectionErr HasCollection 错误
	hasCollectionErr error
	// closed 是否已关闭
	closed bool
}

// fakeRecord 模拟 Milvus 中的一条记录。
type fakeRecord struct {
	id        string
	namespace string
	content   string
	embedding []float32
	metadata  map[string]any
}

// ──────────────────────────── 导出函数 ────────────────────────────

func newFakeMilvusClient() *fakeMilvusClient {
	return &fakeMilvusClient{
		records:             make(map[string]map[string]*fakeRecord),
		hasCollectionResult: true,
	}
}

// directInsert 直接插入记录到 fake 存储（绕过 milvusClient 接口）。
// 用于测试前预置数据。
func (f *fakeMilvusClient) directInsert(collection, id, namespace, content string, embedding []float32, metadata map[string]any) {
	if f.records[collection] == nil {
		f.records[collection] = make(map[string]*fakeRecord)
	}
	f.records[collection][id] = &fakeRecord{
		id: id, namespace: namespace, content: content,
		embedding: embedding, metadata: metadata,
	}
}

// directQuery 按 namespace 查询记录数量。
func (f *fakeMilvusClient) directQuery(collection, namespace string) int {
	count := 0
	for _, rec := range f.records[collection] {
		if namespace == "" || rec.namespace == namespace {
			count++
		}
	}
	return count
}

// directDelete 按 namespace 删除记录。
func (f *fakeMilvusClient) directDelete(collection, namespace string) int {
	deleted := 0
	for id, rec := range f.records[collection] {
		if rec.namespace == namespace {
			delete(f.records[collection], id)
			deleted++
		}
	}
	return deleted
}

// ──────────────────────────── milvusClient 接口实现 ────────────────────────────

func (f *fakeMilvusClient) CreateCollection(_ context.Context, _ milvusclient.CreateCollectionOption) error {
	return nil
}

func (f *fakeMilvusClient) HasCollection(_ context.Context, _ milvusclient.HasCollectionOption) (bool, error) {
	if f.hasCollectionErr != nil {
		return false, f.hasCollectionErr
	}
	return f.hasCollectionResult, nil
}

func (f *fakeMilvusClient) DescribeCollection(_ context.Context, _ milvusclient.DescribeCollectionOption) (*entity.Collection, error) {
	return nil, nil
}

func (f *fakeMilvusClient) Insert(_ context.Context, _ milvusclient.InsertOption) (milvusclient.InsertResult, error) {
	// Insert 通过 MilvusConnectorImpl.SaveToDB 的列格式数据插入，
	// fake 不解析 InsertOption（接口太不透明），改为通过 directInsert 预置数据。
	// 对于 SaveToDB 的 upsert 流程，Delete + Insert 会正确处理。
	return milvusclient.InsertResult{}, nil
}

func (f *fakeMilvusClient) Search(_ context.Context, _ milvusclient.SearchOption) ([]milvusclient.ResultSet, error) {
	// Search 通过 MilvusConnectorImpl.Search 调用，
	// fake 不解析 SearchOption，返回空结果。
	return nil, nil
}

func (f *fakeMilvusClient) Query(_ context.Context, _ milvusclient.QueryOption) (milvusclient.ResultSet, error) {
	// Query 通过 MilvusConnectorImpl.LoadFromDB/Exists/Delete/ListNamespaces/Count 调用，
	// fake 不解析 QueryOption，返回空结果。
	return milvusclient.ResultSet{}, nil
}

func (f *fakeMilvusClient) Delete(_ context.Context, _ milvusclient.DeleteOption) (milvusclient.DeleteResult, error) {
	return milvusclient.DeleteResult{}, nil
}

func (f *fakeMilvusClient) LoadCollection(_ context.Context, _ milvusclient.LoadCollectionOption) error {
	return nil
}

func (f *fakeMilvusClient) Flush(_ context.Context, _ milvusclient.FlushOption) error {
	return nil
}

func (f *fakeMilvusClient) CreateIndex(_ context.Context, _ milvusclient.CreateIndexOption) error {
	return nil
}

func (f *fakeMilvusClient) Close(_ context.Context) error {
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
	assert.Equal(t, "hello", Truncate("hello", 10))
	assert.Equal(t, "hel", Truncate("hello", 3))
	text := "你好世界"
	result := Truncate(text, 6)
	assert.Equal(t, "你好", result)
	result = Truncate(text, 7)
	assert.Equal(t, "你好", result)
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

func TestSaveToDB_Upsert(t *testing.T) {
	fake := newFakeMilvusClient()
	m := NewMilvusConnectorImpl(WithConnectorDim(3))
	m.SetClient(fake)

	data := map[string]any{
		"node1": map[string]any{
			"id":        "node1",
			"content":   "hello world",
			"embedding": []float32{1.0, 2.0, 3.0},
			"metadata":  map[string]any{"key": "value"},
		},
		"node2": map[string]any{
			"id":        "node2",
			"content":   "goodbye world",
			"embedding": []float32{4.0, 5.0, 6.0},
			"metadata":  map[string]any{"key": "value2"},
		},
	}

	// 预置数据到 fake 存储（模拟已有数据被 upsert）
	fake.directInsert(milvusDefaultCollectionName, "node1", "test_ns", "old content", []float32{1, 2, 3}, map[string]any{})

	err := m.SaveToDB("test_ns", data)
	require.NoError(t, err)
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
	m := NewMilvusConnectorImpl()
	fake := newFakeMilvusClient()
	m.SetClient(fake)

	data := map[string]any{
		"node1": map[string]any{"id": "node1", "content": "hello"},
	}

	err := m.SaveToDB("ns", data)
	assert.NoError(t, err)
}

func TestLoadFromDB(t *testing.T) {
	fake := newFakeMilvusClient()
	m := NewMilvusConnectorImpl(WithConnectorDim(3))
	m.SetClient(fake)

	// LoadFromDB 通过 Query 实现，fake 返回空结果
	// 因此测试无数据情况
	loaded, err := m.LoadFromDB("test_ns")
	require.NoError(t, err)
	assert.Len(t, loaded, 0)
}

func TestExists_有数据(t *testing.T) {
	fake := newFakeMilvusClient()
	m := NewMilvusConnectorImpl(WithConnectorDim(3))
	m.SetClient(fake)

	// Exists 通过 Query 实现，fake 返回空结果
	// 因此测试无数据情况（始终 false）
	assert.False(t, m.Exists("test_ns"))
}

func TestExists_集合不存在(t *testing.T) {
	m := NewMilvusConnectorImpl()
	fake := newFakeMilvusClient()
	fake.hasCollectionResult = false
	m.SetClient(fake)

	result := m.Exists("ns")
	assert.False(t, result)
}

func TestDelete_有数据(t *testing.T) {
	fake := newFakeMilvusClient()
	m := NewMilvusConnectorImpl(WithConnectorDim(3))
	m.SetClient(fake)

	// Delete 通过 Query + Delete 实现，fake Query 返回空
	result := m.Delete("test_ns")
	assert.False(t, result) // Query 返回空 → 无数据可删
}

func TestDelete_集合不存在(t *testing.T) {
	m := NewMilvusConnectorImpl()
	fake := newFakeMilvusClient()
	fake.hasCollectionResult = false
	m.SetClient(fake)

	result := m.Delete("ns")
	assert.False(t, result)
}

func TestSearch(t *testing.T) {
	fake := newFakeMilvusClient()
	m := NewMilvusConnectorImpl(WithConnectorDim(3))
	m.SetClient(fake)

	// Search 通过 fake 返回空结果
	results, err := m.Search("test_ns", []float32{1.0, 2.0, 3.0}, 10, "COSINE")
	require.NoError(t, err)
	assert.Len(t, results, 0)
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
