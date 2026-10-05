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
