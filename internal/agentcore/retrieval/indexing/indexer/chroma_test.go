package indexer

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/vector_fields"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
)

// TestNewChromaIndexer_默认参数 测试默认参数创建
func TestNewChromaIndexer_默认参数(t *testing.T) {
	dir := t.TempDir()
	config := common.VectorStoreConfig{
		StoreProvider:  common.StoreTypeChroma,
		CollectionName: "test-collection",
		DistanceMetric: common.DistanceMetricCosine,
	}

	indexer, err := NewChromaIndexer(config, dir)

	require.NoError(t, err)
	require.NotNil(t, indexer)
	assert.Equal(t, "cosine", indexer.DistanceMetric())
	assert.Equal(t, "content", indexer.textField)
	assert.Equal(t, "document_id", indexer.docIDField)
	assert.Equal(t, "sparse_vector", indexer.sparseVectorField)
	assert.Equal(t, "metadata", indexer.metadataField)
}

// TestNewChromaIndexer_chromaPath为空 测试空路径
func TestNewChromaIndexer_chromaPath为空(t *testing.T) {
	config := common.VectorStoreConfig{
		StoreProvider:  common.StoreTypeChroma,
		CollectionName: "test-collection",
	}

	_, err := NewChromaIndexer(config, "")
	assert.Error(t, err)
}

// TestNewChromaIndexer_chromaPath为空格 测试纯空格路径
func TestNewChromaIndexer_chromaPath为空格(t *testing.T) {
	config := common.VectorStoreConfig{
		StoreProvider:  common.StoreTypeChroma,
		CollectionName: "test-collection",
	}

	_, err := NewChromaIndexer(config, "   ")
	assert.Error(t, err)
}

// TestNewChromaIndexer_距离度量归一化 测试 dot→ip, euclidean→l2
func TestNewChromaIndexer_距离度量归一化(t *testing.T) {
	tests := []struct {
		input    common.DistanceMetricKind
		expected string
	}{
		{common.DistanceMetricCosine, "cosine"},
		{common.DistanceMetricEuclidean, "l2"},
		{common.DistanceMetricDot, "ip"},
	}

	for _, tt := range tests {
		t.Run(string(tt.input), func(t *testing.T) {
			dir := t.TempDir()
			config := common.VectorStoreConfig{
				StoreProvider:  common.StoreTypeChroma,
				CollectionName: "test-collection",
				DistanceMetric: tt.input,
			}

			indexer, err := NewChromaIndexer(config, dir)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, indexer.DistanceMetric())
		})
	}
}

// TestNewChromaIndexer_选项设置 测试 option 模式
func TestNewChromaIndexer_选项设置(t *testing.T) {
	dir := t.TempDir()
	config := common.VectorStoreConfig{
		StoreProvider:  common.StoreTypeChroma,
		CollectionName: "test-collection",
		DistanceMetric: common.DistanceMetricCosine,
	}

	indexer, err := NewChromaIndexer(config, dir,
		WithChromaTextField("my_text"),
		WithChromaDocIDField("my_doc_id"),
		WithChromaMetadataField("my_meta"),
		WithChromaSparseVectorField("my_sparse"),
	)

	require.NoError(t, err)
	assert.Equal(t, "my_text", indexer.textField)
	assert.Equal(t, "my_doc_id", indexer.docIDField)
	assert.Equal(t, "my_meta", indexer.metadataField)
	assert.Equal(t, "my_sparse", indexer.sparseVectorField)
}

// TestNewChromaIndexer_ConstructConfig 测试构建配置含 space
func TestNewChromaIndexer_ConstructConfig(t *testing.T) {
	dir := t.TempDir()
	config := common.VectorStoreConfig{
		StoreProvider:  common.StoreTypeChroma,
		CollectionName: "test-collection",
		DistanceMetric: common.DistanceMetricCosine,
	}

	indexer, err := NewChromaIndexer(config, dir)
	require.NoError(t, err)
	assert.Equal(t, "cosine", indexer.constructConfig["space"])
}

// TestNewChromaIndexer_自定义VectorField 测试自定义向量字段配置
func TestNewChromaIndexer_自定义VectorField(t *testing.T) {
	dir := t.TempDir()
	config := common.VectorStoreConfig{
		StoreProvider:  common.StoreTypeChroma,
		CollectionName: "test-collection",
		DistanceMetric: common.DistanceMetricEuclidean,
	}

	vf := vector_fields.NewChromaVectorFieldFromName("custom_vector")
	indexer, err := NewChromaIndexer(config, dir, WithChromaVectorField(vf))
	require.NoError(t, err)
	assert.Equal(t, "custom_vector", indexer.vectorField.VectorFieldName)
	assert.Equal(t, "l2", indexer.DistanceMetric())
	assert.Equal(t, "l2", indexer.constructConfig["space"])
}

// TestNewChromaIndexer_SearchConfig 测试搜索配置
func TestNewChromaIndexer_SearchConfig(t *testing.T) {
	dir := t.TempDir()
	config := common.VectorStoreConfig{
		StoreProvider:  common.StoreTypeChroma,
		CollectionName: "test-collection",
		DistanceMetric: common.DistanceMetricCosine,
	}

	indexer, err := NewChromaIndexer(config, dir)
	require.NoError(t, err)
	// searchConfig 不应包含 space（对齐 Python: to_dict(stage="search") 不含 space）
	_, hasSpace := indexer.searchConfig["space"]
	assert.False(t, hasSpace, "searchConfig 不应包含 space 键")
}

// TestNewChromaIndexer_DocIndexCallback 测试回调设置
func TestNewChromaIndexer_DocIndexCallback(t *testing.T) {
	dir := t.TempDir()
	config := common.VectorStoreConfig{
		StoreProvider:  common.StoreTypeChroma,
		CollectionName: "test-collection",
		DistanceMetric: common.DistanceMetricCosine,
	}

	cb := common.NewLoggingDocIndexCallback(100)
	indexer, err := NewChromaIndexer(config, dir, WithChromaDocIndexCallback(cb))
	require.NoError(t, err)
	assert.NotNil(t, indexer.docIndexCallback)
}

// TestChromaIndexer_实现Indexer接口 编译期校验
func TestChromaIndexer_实现Indexer接口(t *testing.T) {
	// ChromaIndexer 必须实现 Indexer 接口
	var _ Indexer = (*ChromaIndexer)(nil)
}

// TestChromaIndexer_持久client复用 测试构造时创建的 client 被所有操作复用
//
// M-44: 对齐 Python __init__ 中创建一次 self._client 的语义。
// 每次操作后 client 指针不变，证明没有重建 PersistentClient。
func TestChromaIndexer_持久client复用(t *testing.T) {
	dir := t.TempDir()
	config := common.VectorStoreConfig{
		StoreProvider:  common.StoreTypeChroma,
		CollectionName: "test-collection",
		DistanceMetric: common.DistanceMetricCosine,
	}

	indexer, err := NewChromaIndexer(config, dir)
	require.NoError(t, err)
	defer indexer.Close()

	clientBefore := indexer.Client()
	require.NotNil(t, clientBefore, "构造后应持有持久 client")

	// 多次操作后 client 指针不变
	_, _ = indexer.IndexExists(t.Context(), "nonexistent-index")
	assert.Equal(t, clientBefore, indexer.Client(), "IndexExists 不应重建 client")

	_, _ = indexer.DeleteIndex(t.Context(), "doc-1", "nonexistent-index")
	assert.Equal(t, clientBefore, indexer.Client(), "DeleteIndex 不应重建 client")

	_, _ = indexer.GetIndexInfo(t.Context(), "nonexistent-index")
	assert.Equal(t, clientBefore, indexer.Client(), "GetIndexInfo 不应重建 client")
}

// TestChromaIndexer_IndexExists_真实语义 测试不存在的索引返回 false
//
// M-44: 原实现 NewChromaVectorStore 内部 GetOrCreateCollection 永不失败，
// 导致 IndexExists 恒返回 true（S-63/S-64/S-15 历史问题）。
// 现改为持久 client.GetCollection 真实语义：不存在 → false。
func TestChromaIndexer_IndexExists_真实语义(t *testing.T) {
	dir := t.TempDir()
	config := common.VectorStoreConfig{
		StoreProvider:  common.StoreTypeChroma,
		CollectionName: "test-collection",
		DistanceMetric: common.DistanceMetricCosine,
	}

	indexer, err := NewChromaIndexer(config, dir)
	require.NoError(t, err)
	defer indexer.Close()

	// 不存在的索引 → false（原实现恒 true 的 bug 已修）
	exists, err := indexer.IndexExists(t.Context(), "definitely-not-exists")
	require.NoError(t, err)
	assert.False(t, exists, "不存在的索引应返回 false")

	// DeleteIndex 对不存在的集合：GetCollection 失败 → 对齐 Python 返回 false
	deleted, err := indexer.DeleteIndex(t.Context(), "doc-1", "definitely-not-exists")
	require.NoError(t, err)
	assert.False(t, deleted, "不存在的集合删除应返回 false")

	// GetIndexInfo 对不存在的索引 → exists=false
	info, err := indexer.GetIndexInfo(t.Context(), "definitely-not-exists")
	require.NoError(t, err)
	assert.Equal(t, false, info["exists"], "不存在的索引 info 应为 exists=false")
}

// TestChromaIndexer_IndexExists_BuildIndex后为True 测试真实建库后存在性翻转
//
// 使用 BM25 类型跳过嵌入（无需 embedModel），验证 BuildIndex 后
// IndexExists 从 false 变 true——证明存在性检查真实生效。
func TestChromaIndexer_IndexExists_BuildIndex后为True(t *testing.T) {
	dir := t.TempDir()
	config := common.VectorStoreConfig{
		StoreProvider:  common.StoreTypeChroma,
		CollectionName: "test-collection",
		DistanceMetric: common.DistanceMetricCosine,
	}

	indexer, err := NewChromaIndexer(config, dir)
	require.NoError(t, err)
	defer indexer.Close()

	indexName := "real-index-test"
	// BM25 类型跳过嵌入计算，但预填 Embedding 保证 addBatch 不跳过该节点
	// （对齐 Python: add 中 node.get(vector_field, []) 为空时 warning + skip）
	chunks := []common.TextChunk{
		{ID: "chunk-1", Text: "hello world", DocID: "doc-1", Embedding: []float64{0.1, 0.2, 0.3}},
	}
	indexConfig := common.IndexConfig{
		IndexName: indexName,
		IndexType: common.IndexTypeBM25,
	}

	// 建库前不存在
	exists, err := indexer.IndexExists(t.Context(), indexName)
	require.NoError(t, err)
	assert.False(t, exists)

	// BM25 类型跳过嵌入，直接写入
	ok, err := indexer.BuildIndex(t.Context(), chunks, indexConfig, nil)
	require.NoError(t, err)
	assert.True(t, ok)

	// 建库后存在——真实语义验证
	exists, err = indexer.IndexExists(t.Context(), indexName)
	require.NoError(t, err)
	assert.True(t, exists, "BuildIndex 后 IndexExists 应返回 true")

	// GetIndexInfo 返回真实元信息
	info, err := indexer.GetIndexInfo(t.Context(), indexName)
	require.NoError(t, err)
	assert.Equal(t, true, info["exists"])
	assert.Equal(t, indexName, info["collection_name"])
	assert.Equal(t, 1, info["count"], "应写入 1 条记录")
}

// TestChromaIndexer_Close 测试关闭持久客户端
//
// M-44: 对齐 Python ChromaIndexer.close()。
func TestChromaIndexer_Close(t *testing.T) {
	dir := t.TempDir()
	config := common.VectorStoreConfig{
		StoreProvider:  common.StoreTypeChroma,
		CollectionName: "test-collection",
		DistanceMetric: common.DistanceMetricCosine,
	}

	indexer, err := NewChromaIndexer(config, dir)
	require.NoError(t, err)
	require.NotNil(t, indexer.Client())

	// Close 不应 panic；对已关闭 client 的操作错误被各方法正确处理
	indexer.Close()
	indexer.Close() // 幂等性验证
}
