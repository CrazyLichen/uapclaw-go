package vector_store

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/vector_fields"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// newMilvusConfig 工厂测试用 Milvus 配置
func newMilvusConfig() common.VectorStoreConfig {
	return common.VectorStoreConfig{
		StoreProvider:  common.StoreTypeMilvus,
		CollectionName: "test-collection",
		DatabaseName:   "default",
		DistanceMetric: common.DistanceMetricCosine,
	}
}

// TestCreateVectorStore_milvus分支 F1：全零值 options → 默认参数组装
func TestCreateVectorStore_milvus分支(t *testing.T) {
	vs, err := CreateVectorStore(newMilvusConfig(), WithStoreMilvusURI("http://localhost:19530"))
	require.NoError(t, err)
	require.NotNil(t, vs)
	s, ok := vs.(*MilvusVectorStore)
	require.True(t, ok)
	assert.Equal(t, "content", s.textField)                        // Python 默认 text_field="content"
	assert.Equal(t, "sparse_vector", s.sparseVectorField)
	assert.Equal(t, "metadata", s.metadataField)
	assert.Equal(t, "document_id", s.docIDField)
	assert.Equal(t, "embedding", s.vectorField.VectorFieldName()) // str 联合分支 → AUTO("embedding")
}

// TestCreateVectorStore_chroma分支 F2：默认参数 + TempDir 真实构造
func TestCreateVectorStore_chroma分支(t *testing.T) {
	config := common.VectorStoreConfig{
		StoreProvider:  common.StoreTypeChroma,
		CollectionName: "test-collection",
		DistanceMetric: common.DistanceMetricCosine,
	}
	vs, err := CreateVectorStore(config, WithStoreChromaPath(t.TempDir()))
	require.NoError(t, err)
	require.NotNil(t, vs)
	s, ok := vs.(*ChromaVectorStore)
	require.True(t, ok)
	assert.Equal(t, "content", s.textField)
	assert.Equal(t, "embedding", s.vectorField.VectorFieldName)
}

// TestCreateVectorStore_chroma分支路径校验 F2b：chromaPath 空 → 155400 透传
func TestCreateVectorStore_chroma分支路径校验(t *testing.T) {
	config := common.VectorStoreConfig{
		StoreProvider:  common.StoreTypeChroma,
		CollectionName: "test-collection",
		DistanceMetric: common.DistanceMetricCosine,
	}
	_, err := CreateVectorStore(config, WithStoreChromaPath(""))
	require.Error(t, err)
	var be *exception.BaseError
	require.True(t, errors.As(err, &be))
	assert.Equal(t, 155400, be.Code())
}

// TestCreateVectorStore_pgvector分支 F3：155402 + not implemented 文案（⤵️ PGVectorStore 拆后交付）
func TestCreateVectorStore_pgvector分支(t *testing.T) {
	config := common.VectorStoreConfig{
		StoreProvider:  common.StoreTypePGVector,
		CollectionName: "test-collection",
		DistanceMetric: common.DistanceMetricCosine,
	}
	_, err := CreateVectorStore(config)
	require.Error(t, err)
	var be *exception.BaseError
	require.True(t, errors.As(err, &be))
	assert.Equal(t, 155402, be.Code())
	assert.Contains(t, be.Message(), "not implemented yet")
}

// TestCreateVectorStore_default分支 F4：155402 + provider 清单文案逐字符
func TestCreateVectorStore_default分支(t *testing.T) {
	config := common.VectorStoreConfig{
		StoreProvider:  common.StoreType("opensearch"),
		CollectionName: "test-collection",
		DistanceMetric: common.DistanceMetricCosine,
	}
	_, err := CreateVectorStore(config)
	require.Error(t, err)
	var be *exception.BaseError
	require.True(t, errors.As(err, &be))
	assert.Equal(t, 155402, be.Code())
	// Python 两行 f-string 拼接渲染结果（逗号+空格）
	assert.True(t, strings.Contains(be.Message(), "unavailable vector store provider: opensearch, and available providers are: milvus, chroma, pgvector"))
}

// TestCreateVectorStore_显式字段定义 F5：强类型直传不转 AUTO
func TestCreateVectorStore_显式字段定义(t *testing.T) {
	vf := vector_fields.NewMilvusHNSW("custom_vec", 16, 200, 1.0)
	vs, err := CreateVectorStore(newMilvusConfig(),
		WithStoreMilvusURI("http://localhost:19530"),
		WithStoreMilvusVectorField(vf))
	require.NoError(t, err)
	s, ok := vs.(*MilvusVectorStore)
	require.True(t, ok)
	assert.Equal(t, "custom_vec", s.vectorField.VectorFieldName())
}

// TestCreateVectorStore_milvusURI为空 F6：155400 构造校验透传
func TestCreateVectorStore_milvusURI为空(t *testing.T) {
	_, err := CreateVectorStore(newMilvusConfig())
	require.Error(t, err)
	var be *exception.BaseError
	require.True(t, errors.As(err, &be))
	assert.Equal(t, 155400, be.Code())
}
