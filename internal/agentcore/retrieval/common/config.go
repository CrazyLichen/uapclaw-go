package common

import (
	"fmt"
	"regexp"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/reranker"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 结构体 ────────────────────────────

// IndexConfig 索引配置。
//
// Python: openjiuwen/core/retrieval/common/config.py (IndexConfig)
type IndexConfig struct {
	// IndexName 索引名称（必填）
	IndexName string
	// IndexType 索引类型，取值 IndexTypeHybrid / IndexTypeBM25 / IndexTypeVector
	IndexType IndexTypeKind
	// UseCaptionForImages 为 true 时，图片分块仅用文本/标题嵌入（纯文本路径）
	UseCaptionForImages bool
}

// KnowledgeBaseConfig 知识库配置。
//
// Python: openjiuwen/core/retrieval/common/config.py (KnowledgeBaseConfig)
type KnowledgeBaseConfig struct {
	// KBID 知识库标识（必填）
	KBID string
	// IndexType 索引类型，取值 IndexTypeHybrid / IndexTypeBM25 / IndexTypeVector
	IndexType IndexTypeKind
	// UseGraph 是否使用图索引
	UseGraph bool
	// ChunkSize 分块大小
	ChunkSize int
	// ChunkOverlap 分块重叠
	ChunkOverlap int
	// UseCaptionForImages 为 true 时，图片分块仅用文本/标题嵌入
	UseCaptionForImages bool
}

// RetrievalConfig 检索配置。
//
// Python: openjiuwen/core/retrieval/common/config.py (RetrievalConfig)
type RetrievalConfig struct {
	// TopK 返回结果数量
	TopK int
	// ScoreThreshold 分数阈值，nil 表示不限制
	ScoreThreshold *float64
	// UseGraph 是否使用图检索，nil 表示使用默认配置
	UseGraph *bool
	// Agentic 是否使用 Agentic 检索
	Agentic bool
	// GraphExpansion 是否启用图扩展
	GraphExpansion bool
	// Filters 元数据过滤条件
	Filters map[string]any
}

// VectorStoreConfig 向量存储配置。
//
// Python: openjiuwen/core/retrieval/common/config.py (VectorStoreConfig)
type VectorStoreConfig struct {
	// StoreProvider 向量存储提供商
	StoreProvider StoreType
	// DatabaseName 数据库名称（仅字母数字下划线）
	DatabaseName string
	// CollectionName 集合名称（必填）
	CollectionName string
	// DistanceMetric 距离度量方式
	DistanceMetric DistanceMetricKind
}

// ──────────────────────────── 枚举 ────────────────────────────

// IndexTypeKind 索引类型，对齐 Python Literal["hybrid", "bm25", "vector"]。
type IndexTypeKind string

// StoreType 向量存储提供商类型，对齐 Python StoreType 枚举。
type StoreType string

// DistanceMetricKind 距离度量方式，对齐 Python Literal["cosine", "euclidean", "dot"]。
type DistanceMetricKind string

// ──────────────────────────── 常量 ────────────────────────────

const (
	// IndexTypeHybrid 混合索引（BM25 + 向量）
	IndexTypeHybrid IndexTypeKind = "hybrid"
	// IndexTypeBM25 BM25 稀疏索引
	IndexTypeBM25 IndexTypeKind = "bm25"
	// IndexTypeVector 向量索引
	IndexTypeVector IndexTypeKind = "vector"
)

const (
	// StoreTypeMilvus Milvus 向量库
	StoreTypeMilvus StoreType = "milvus"
	// StoreTypeChroma ChromaDB 向量库
	StoreTypeChroma StoreType = "chroma"
	// StoreTypePGVector PGVector 向量库
	StoreTypePGVector StoreType = "pgvector"
)

const (
	// DistanceMetricCosine 余弦距离（默认）
	DistanceMetricCosine DistanceMetricKind = "cosine"
	// DistanceMetricEuclidean 欧几里得距离
	DistanceMetricEuclidean DistanceMetricKind = "euclidean"
	// DistanceMetricDot 点积距离
	DistanceMetricDot DistanceMetricKind = "dot"
)

// 重导出类型，对齐 Python config.py __all__
// 注意：EmbeddingConfig 在 retrieval/embedding 中，import 会形成循环依赖，
// 因此不在 common 包重导出，使用方应直接 import retrieval/embedding。

// RerankerConfig 重排序模型配置。
// 重导出自 foundation/store/reranker，对齐 Python config.py __all__。
type RerankerConfig = reranker.RerankerConfig

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// validIndexTypeKinds 有效的索引类型集合
	validIndexTypeKinds = map[IndexTypeKind]bool{
		IndexTypeHybrid: true,
		IndexTypeBM25:   true,
		IndexTypeVector: true,
	}
	// validStoreTypes 有效的存储类型集合
	validStoreTypes = map[StoreType]bool{
		StoreTypeMilvus:   true,
		StoreTypeChroma:   true,
		StoreTypePGVector: true,
	}
	// validDistanceMetrics 有效的距离度量集合
	validDistanceMetrics = map[DistanceMetricKind]bool{
		DistanceMetricCosine:    true,
		DistanceMetricEuclidean: true,
		DistanceMetricDot:       true,
	}
	// dbNamePattern 数据库名称正则，仅允许字母数字下划线
	dbNamePattern = regexp.MustCompile(`^[A-Za-z0-9_]*$`)
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewDefaultIndexConfig 创建默认索引配置。
//
// Python: IndexConfig(index_name=..., index_type="hybrid", use_caption_for_images=False)
func NewDefaultIndexConfig(indexName string) *IndexConfig {
	return &IndexConfig{
		IndexName:           indexName,
		IndexType:           IndexTypeHybrid,
		UseCaptionForImages: false,
	}
}

// NewDefaultKnowledgeBaseConfig 创建默认知识库配置。
//
// Python: KnowledgeBaseConfig(kb_id=..., index_type="hybrid", use_graph=False, chunk_size=512, chunk_overlap=50, use_caption_for_images=False)
func NewDefaultKnowledgeBaseConfig(kbID string) *KnowledgeBaseConfig {
	return &KnowledgeBaseConfig{
		KBID:                kbID,
		IndexType:           IndexTypeHybrid,
		UseGraph:            false,
		ChunkSize:           512,
		ChunkOverlap:        50,
		UseCaptionForImages: false,
	}
}

// NewDefaultRetrievalConfig 创建默认检索配置。
//
// Python: RetrievalConfig(top_k=5, score_threshold=None, use_graph=None, agentic=False, graph_expansion=False, filters=None)
func NewDefaultRetrievalConfig() *RetrievalConfig {
	return &RetrievalConfig{
		TopK:           5,
		ScoreThreshold: nil,
		UseGraph:       nil,
		Agentic:        false,
		GraphExpansion: false,
		Filters:        nil,
	}
}

// Validate 校验 IndexConfig 字段。
func (c *IndexConfig) Validate() error {
	if c.IndexName == "" {
		return exception.BuildError(
			exception.StatusRetrievalIndexingPathNotFound,
			exception.WithParam("error_msg", "索引名称不能为空"),
		)
	}
	return validateIndexTypeKind(c.IndexType)
}

// Validate 校验 KnowledgeBaseConfig 字段。
func (c *KnowledgeBaseConfig) Validate() error {
	if c.KBID == "" {
		return exception.BuildError(
			exception.StatusRetrievalIndexingPathNotFound,
			exception.WithParam("error_msg", "知识库标识不能为空"),
		)
	}
	return validateIndexTypeKind(c.IndexType)
}

// Validate 校验 RetrievalConfig 字段。
func (c *RetrievalConfig) Validate() error {
	return nil
}

// Validate 校验 VectorStoreConfig 字段。
func (c *VectorStoreConfig) Validate() error {
	if err := validateStoreType(c.StoreProvider); err != nil {
		return err
	}
	if c.CollectionName == "" {
		return exception.BuildError(
			exception.StatusRetrievalIndexingPathNotFound,
			exception.WithParam("error_msg", "集合名称不能为空"),
		)
	}
	if err := validateDatabaseName(c.DatabaseName); err != nil {
		return err
	}
	return validateDistanceMetricKind(c.DistanceMetric)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// validateIndexTypeKind 校验索引类型是否有效。
func validateIndexTypeKind(kind IndexTypeKind) error {
	if !validIndexTypeKinds[kind] {
		return exception.BuildError(
			exception.StatusRetrievalIndexingVectorFieldInvalid,
			exception.WithParam("error_msg", fmt.Sprintf("无效的索引类型: %s，可选值: hybrid, bm25, vector", kind)),
		)
	}
	return nil
}

// validateStoreType 校验存储类型是否有效。
func validateStoreType(st StoreType) error {
	if !validStoreTypes[st] {
		return exception.BuildError(
			exception.StatusRetrievalIndexingVectorFieldInvalid,
			exception.WithParam("error_msg", fmt.Sprintf("无效的存储类型: %s，可选值: milvus, chroma, pgvector", st)),
		)
	}
	return nil
}

// validateDistanceMetricKind 校验距离度量是否有效。
func validateDistanceMetricKind(dm DistanceMetricKind) error {
	if !validDistanceMetrics[dm] {
		return exception.BuildError(
			exception.StatusRetrievalIndexingVectorFieldInvalid,
			exception.WithParam("error_msg", fmt.Sprintf("无效的距离度量: %s，可选值: cosine, euclidean, dot", dm)),
		)
	}
	return nil
}

// validateDatabaseName 校验数据库名称是否合法（仅字母数字下划线）。
func validateDatabaseName(name string) error {
	if name != "" && !dbNamePattern.MatchString(name) {
		return exception.BuildError(
			exception.StatusRetrievalIndexingVectorFieldInvalid,
			exception.WithParam("error_msg", fmt.Sprintf("数据库名称含非法字符: %s，仅允许字母数字下划线", name)),
		)
	}
	return nil
}
