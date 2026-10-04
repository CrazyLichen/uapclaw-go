package common

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/reranker"
)

// ──── IndexConfig 测试 ────

func TestNewDefaultIndexConfig(t *testing.T) {
	cfg := NewDefaultIndexConfig("test-index")
	assert.Equal(t, "test-index", cfg.IndexName)
	assert.Equal(t, IndexTypeHybrid, cfg.IndexType)
	assert.False(t, cfg.UseCaptionForImages)
}

func TestIndexConfig_Validate_正常(t *testing.T) {
	cfg := NewDefaultIndexConfig("my-index")
	err := cfg.Validate()
	assert.NoError(t, err)
}

func TestIndexConfig_Validate_名称为空(t *testing.T) {
	cfg := NewDefaultIndexConfig("")
	err := cfg.Validate()
	assert.Error(t, err)
}

func TestIndexConfig_Validate_索引类型无效(t *testing.T) {
	cfg := &IndexConfig{IndexName: "test", IndexType: IndexTypeKind("invalid")}
	err := cfg.Validate()
	assert.Error(t, err)
}

// ──── KnowledgeBaseConfig 测试 ────

func TestNewDefaultKnowledgeBaseConfig(t *testing.T) {
	cfg := NewDefaultKnowledgeBaseConfig("kb-001")
	assert.Equal(t, "kb-001", cfg.KBID)
	assert.Equal(t, IndexTypeHybrid, cfg.IndexType)
	assert.False(t, cfg.UseGraph)
	assert.Equal(t, 512, cfg.ChunkSize)
	assert.Equal(t, 50, cfg.ChunkOverlap)
	assert.False(t, cfg.UseCaptionForImages)
}

func TestKnowledgeBaseConfig_Validate_正常(t *testing.T) {
	cfg := NewDefaultKnowledgeBaseConfig("kb-001")
	err := cfg.Validate()
	assert.NoError(t, err)
}

func TestKnowledgeBaseConfig_Validate_KBID为空(t *testing.T) {
	cfg := NewDefaultKnowledgeBaseConfig("")
	err := cfg.Validate()
	assert.Error(t, err)
}

func TestKnowledgeBaseConfig_Validate_索引类型无效(t *testing.T) {
	cfg := &KnowledgeBaseConfig{KBID: "kb-001", IndexType: IndexTypeKind("invalid")}
	err := cfg.Validate()
	assert.Error(t, err)
}

// ──── RetrievalConfig 测试 ────

func TestNewDefaultRetrievalConfig(t *testing.T) {
	cfg := NewDefaultRetrievalConfig()
	assert.Equal(t, 5, cfg.TopK)
	assert.Nil(t, cfg.ScoreThreshold)
	assert.Nil(t, cfg.UseGraph)
	assert.False(t, cfg.Agentic)
	assert.False(t, cfg.GraphExpansion)
	assert.Nil(t, cfg.Filters)
}

func TestRetrievalConfig_Validate_正常(t *testing.T) {
	cfg := NewDefaultRetrievalConfig()
	err := cfg.Validate()
	assert.NoError(t, err)
}

// ──── VectorStoreConfig 测试 ────

func TestVectorStoreConfig_Validate_正常(t *testing.T) {
	cfg := &VectorStoreConfig{
		StoreProvider:  StoreTypeMilvus,
		CollectionName: "my-collection",
		DistanceMetric: DistanceMetricCosine,
	}
	err := cfg.Validate()
	assert.NoError(t, err)
}

func TestVectorStoreConfig_Validate_集合名称为空(t *testing.T) {
	cfg := &VectorStoreConfig{
		StoreProvider:  StoreTypeMilvus,
		CollectionName: "",
		DistanceMetric: DistanceMetricCosine,
	}
	err := cfg.Validate()
	assert.Error(t, err)
}

func TestVectorStoreConfig_Validate_存储类型无效(t *testing.T) {
	cfg := &VectorStoreConfig{
		StoreProvider:  StoreType("invalid"),
		CollectionName: "my-collection",
		DistanceMetric: DistanceMetricCosine,
	}
	err := cfg.Validate()
	assert.Error(t, err)
}

func TestVectorStoreConfig_Validate_数据库名称非法字符(t *testing.T) {
	cfg := &VectorStoreConfig{
		StoreProvider:  StoreTypeMilvus,
		DatabaseName:   "my-db!",
		CollectionName: "my-collection",
		DistanceMetric: DistanceMetricCosine,
	}
	err := cfg.Validate()
	assert.Error(t, err)
}

func TestVectorStoreConfig_Validate_距离度量无效(t *testing.T) {
	cfg := &VectorStoreConfig{
		StoreProvider:  StoreTypeMilvus,
		CollectionName: "my-collection",
		DistanceMetric: DistanceMetricKind("invalid"),
	}
	err := cfg.Validate()
	assert.Error(t, err)
}

// ──── 枚举常量测试 ────

func TestIndexTypeKind_常量值(t *testing.T) {
	assert.Equal(t, IndexTypeKind("hybrid"), IndexTypeHybrid)
	assert.Equal(t, IndexTypeKind("bm25"), IndexTypeBM25)
	assert.Equal(t, IndexTypeKind("vector"), IndexTypeVector)
}

func TestStoreType_常量值(t *testing.T) {
	assert.Equal(t, StoreType("milvus"), StoreTypeMilvus)
	assert.Equal(t, StoreType("chroma"), StoreTypeChroma)
	assert.Equal(t, StoreType("pgvector"), StoreTypePGVector)
}

func TestDistanceMetricKind_常量值(t *testing.T) {
	assert.Equal(t, DistanceMetricKind("cosine"), DistanceMetricCosine)
	assert.Equal(t, DistanceMetricKind("euclidean"), DistanceMetricEuclidean)
	assert.Equal(t, DistanceMetricKind("dot"), DistanceMetricDot)
}

// ──── 重导出类型测试 ────

// 注意：EmbeddingConfig 因循环依赖不在 common 包重导出，此测试仅验证 RerankerConfig。

func TestRerankerConfig_重导出(t *testing.T) {
	// 验证 common.RerankerConfig 可赋值，类型别名成立
	var _ RerankerConfig = reranker.RerankerConfig{}
}
