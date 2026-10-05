package vector_store

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/vector_fields"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
)

// TestChromaVectorField_String类型 测试 string 类型的 vectorField 自动包装
func TestChromaVectorField_String类型(t *testing.T) {
	config := common.VectorStoreConfig{
		StoreProvider:  common.StoreTypeChroma,
		CollectionName: "test-collection",
		DistanceMetric: common.DistanceMetricCosine,
	}
	dir := t.TempDir()

	// 使用 string 类型创建
	vs, err := NewChromaVectorStore(config, dir,
		"content", "embedding", "sparse_vector", "metadata", "document_id",
	)

	require.NoError(t, err)
	require.NotNil(t, vs)
	assert.Equal(t, "cosine", vs.DistanceMetric())
	assert.Equal(t, "embedding", vs.vectorField.VectorFieldName)
}

// TestChromaVectorField_ChromaVectorField类型 测试 ChromaVectorField 类型
func TestChromaVectorField_ChromaVectorField类型(t *testing.T) {
	config := common.VectorStoreConfig{
		StoreProvider:  common.StoreTypeChroma,
		CollectionName: "test-collection",
		DistanceMetric: common.DistanceMetricCosine,
	}
	dir := t.TempDir()
	vf := vector_fields.NewDefaultChromaVectorField()

	vs, err := NewChromaVectorStore(config, dir,
		"content", vf, "sparse_vector", "metadata", "document_id",
	)

	require.NoError(t, err)
	require.NotNil(t, vs)
	assert.Equal(t, "cosine", vs.DistanceMetric())
}

// TestChromaVectorStore_chromaPath为空 测试空路径
func TestChromaVectorStore_chromaPath为空(t *testing.T) {
	config := common.VectorStoreConfig{
		StoreProvider:  common.StoreTypeChroma,
		CollectionName: "test-collection",
		DistanceMetric: common.DistanceMetricCosine,
	}

	_, err := NewChromaVectorStore(config, "",
		"content", "embedding", "sparse_vector", "metadata", "document_id",
	)

	assert.Error(t, err)
}

// TestChromaVectorStore_vectorField无效类型 测试无效类型
func TestChromaVectorStore_vectorField无效类型(t *testing.T) {
	config := common.VectorStoreConfig{
		StoreProvider:  common.StoreTypeChroma,
		CollectionName: "test-collection",
		DistanceMetric: common.DistanceMetricCosine,
	}
	dir := t.TempDir()

	_, err := NewChromaVectorStore(config, dir,
		"content", 123, "sparse_vector", "metadata", "document_id",
	)

	assert.Error(t, err)
}

// TestChromaVectorStore_距离度量归一化 测试 dot→ip, euclidean→l2
func TestChromaVectorStore_距离度量归一化(t *testing.T) {
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
			config := common.VectorStoreConfig{
				StoreProvider:  common.StoreTypeChroma,
				CollectionName: "test-collection",
				DistanceMetric: tt.input,
			}
			dir := t.TempDir()

			vs, err := NewChromaVectorStore(config, dir,
				"content", "embedding", "sparse_vector", "metadata", "document_id",
			)

			require.NoError(t, err)
			assert.Equal(t, tt.expected, vs.DistanceMetric())
		})
	}
}

// TestChromaVectorStore_ConstructConfig 测试构建配置含 space
func TestChromaVectorStore_ConstructConfig(t *testing.T) {
	config := common.VectorStoreConfig{
		StoreProvider:  common.StoreTypeChroma,
		CollectionName: "test-collection",
		DistanceMetric: common.DistanceMetricCosine,
	}
	dir := t.TempDir()

	vs, err := NewChromaVectorStore(config, dir,
		"content", "embedding", "sparse_vector", "metadata", "document_id",
	)

	require.NoError(t, err)
	assert.Equal(t, "cosine", vs.ConstructConfig()["space"])
}

// TestBuildChromaWhereFilter_nil 测试 nil 过滤器
func TestBuildChromaWhereFilter_nil(t *testing.T) {
	result := BuildChromaWhereFilter(nil)
	assert.Nil(t, result)
}

// TestBuildChromaWhereFilter_dict 测试 dict 过滤器
func TestBuildChromaWhereFilter_dict(t *testing.T) {
	filters := map[string]any{
		"document_id": "doc-1",
		"category":    "tech",
	}
	result := BuildChromaWhereFilter(filters)
	assert.NotNil(t, result)
}

// TestBuildChromaWhereFilter_emptyDict 测试空 dict
func TestBuildChromaWhereFilter_emptyDict(t *testing.T) {
	filters := map[string]any{}
	result := BuildChromaWhereFilter(filters)
	assert.Nil(t, result)
}

// TestBuildChromaWhereFilter_nonDict 测试非 dict 类型
func TestBuildChromaWhereFilter_nonDict(t *testing.T) {
	result := BuildChromaWhereFilter("some_string")
	assert.Nil(t, result)
}

// TestMapToChromaDistanceMetric 测试距离度量映射
func TestMapToChromaDistanceMetric(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"cosine", "cosine"},
		{"l2", "l2"},
		{"ip", "ip"},
		{"COSINE", "cosine"},
		{"unknown", "cosine"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := mapToChromaDistanceMetric(tt.input)
			assert.Equal(t, tt.expected, string(result))
		})
	}
}

// TestTruncateText 测试文本截断
func TestTruncateText(t *testing.T) {
	assert.Equal(t, "hello", truncateText("hello", 10))
	assert.Equal(t, "hello00000"+"...", truncateText("hello00000world", 10))
}
