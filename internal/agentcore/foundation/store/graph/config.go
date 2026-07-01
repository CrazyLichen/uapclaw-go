package graph

import (
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/vector_fields"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 结构体 ────────────────────────────

// GraphConfig 图存储顶层配置
type GraphConfig struct {
	// URI 数据库连接地址
	URI string `json:"uri"`
	// Name 数据库名称
	Name string `json:"name"`
	// Token 认证令牌
	Token string `json:"token"`
	// Backend 后端类型（目前仅 "milvus"）
	Backend string `json:"backend"`
	// Timeout 连接超时（秒）
	Timeout float64 `json:"timeout"`
	// Extras 额外参数
	Extras map[string]any `json:"extras,omitempty"`
	// MaxConcurrent 最大并发数
	MaxConcurrent int `json:"max_concurrent"`
	// EmbedDim 嵌入向量维度
	EmbedDim int `json:"embed_dim"`
	// EmbedBatchSize 嵌入批大小
	EmbedBatchSize int `json:"embed_batch_size"`
	// EmbeddingModel 嵌入模型名称，对齐 Python embedding_model 字段
	EmbeddingModel string `json:"embedding_model,omitempty"`
	// StorageConfig 存储限制配置
	StorageConfig *GraphStoreStorageConfig `json:"db_storage_config"`
	// IndexConfig 索引配置
	IndexConfig *GraphStoreIndexConfig `json:"db_embed_config"`
	// RequestMaxRetries 请求最大重试次数
	RequestMaxRetries int `json:"request_max_retries"`
	// EnableCompact 是否在 Refresh 时执行 Compact（默认 false）
	EnableCompact bool `json:"enable_compact"`
}

// GraphStoreStorageConfig 图存储字段长度限制配置
type GraphStoreStorageConfig struct {
	// UUID UUID最大长度
	UUID int `json:"uuid"`
	// Name 名称最大长度
	Name int `json:"name"`
	// Content 内容最大长度
	Content int `json:"content"`
	// Language 语言字段最大长度
	Language int `json:"language"`
	// UserID 用户ID最大长度
	UserID int `json:"user_id"`
	// Entities 每片段最大实体数
	Entities int `json:"entities"`
	// Relations 每实体最大关系数
	Relations int `json:"relations"`
	// Episodes 每实体最大片段数
	Episodes int `json:"episodes"`
	// ObjType 对象类型最大长度
	ObjType int `json:"obj_type"`
}

// GraphStoreIndexConfig 图存储索引配置
type GraphStoreIndexConfig struct {
	// IndexType 向量索引类型
	IndexType vector_fields.VectorField `json:"index_type"`
	// DistanceMetric 距离度量方式（cosine/euclidean/dot）
	DistanceMetric string `json:"distance_metric"`
	// ExtraConfigs 额外索引配置
	ExtraConfigs map[string]any `json:"extra_configs,omitempty"`
	// BM25Config BM25参数配置
	BM25Config *BM25Config `json:"bm25_config"`
	// BM25AnalyzerSettings BM25分析器设置
	BM25AnalyzerSettings map[string]any `json:"bm25_analyzer_settings,omitempty"`
}

// BM25Config BM25检索参数
type BM25Config struct {
	// B 文档长度归一化参数（0~1）
	B float64 `json:"b"`
	// K1 词频饱和度参数（≥0）
	K1 float64 `json:"k1"`
}

// ──────────────────────────── 常量 ────────────────────────────
const (
	// DefaultGraphBackend 默认后端
	DefaultGraphBackend = "milvus"
	// DefaultGraphTimeout 默认超时（秒）
	DefaultGraphTimeout = 15.0
	// DefaultGraphMaxConcurrent 默认最大并发
	DefaultGraphMaxConcurrent = 10
	// DefaultGraphEmbedDim 默认嵌入维度
	DefaultGraphEmbedDim = 512
	// DefaultGraphEmbedBatchSize 默认嵌入批大小
	DefaultGraphEmbedBatchSize = 10
	// DefaultGraphRequestMaxRetries 默认请求最大重试次数
	DefaultGraphRequestMaxRetries = 5

	// 存储限制默认值
	DefaultStorageUUID      = 32
	DefaultStorageName      = 500
	DefaultStorageContent   = 65535
	DefaultStorageLanguage  = 10
	DefaultStorageUserID    = 32
	DefaultStorageEntities  = 4096
	DefaultStorageRelations = 4096
	DefaultStorageEpisodes  = 4096
	DefaultStorageObjType   = 20

	// BM25 默认参数
	DefaultBM25B  = 0.75
	DefaultBM25K1 = 1.2
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewGraphConfig 创建带默认值的图存储配置
func NewGraphConfig(uri string) *GraphConfig {
	return &GraphConfig{
		URI:               uri,
		Backend:           DefaultGraphBackend,
		Timeout:           DefaultGraphTimeout,
		MaxConcurrent:     DefaultGraphMaxConcurrent,
		EmbedDim:          DefaultGraphEmbedDim,
		EmbedBatchSize:    DefaultGraphEmbedBatchSize,
		RequestMaxRetries: DefaultGraphRequestMaxRetries,
		StorageConfig:     NewDefaultStorageConfig(),
		IndexConfig:       NewDefaultIndexConfig(),
	}
}

// NewDefaultStorageConfig 创建默认存储限制配置
func NewDefaultStorageConfig() *GraphStoreStorageConfig {
	return &GraphStoreStorageConfig{
		UUID:      DefaultStorageUUID,
		Name:      DefaultStorageName,
		Content:   DefaultStorageContent,
		Language:  DefaultStorageLanguage,
		UserID:    DefaultStorageUserID,
		Entities:  DefaultStorageEntities,
		Relations: DefaultStorageRelations,
		Episodes:  DefaultStorageEpisodes,
		ObjType:   DefaultStorageObjType,
	}
}

// NewDefaultIndexConfig 创建默认索引配置
func NewDefaultIndexConfig() *GraphStoreIndexConfig {
	return &GraphStoreIndexConfig{
		DistanceMetric: "cosine",
		BM25Config: &BM25Config{
			B:  DefaultBM25B,
			K1: DefaultBM25K1,
		},
	}
}

// Validate 校验 GraphConfig 字段合法性
func (c *GraphConfig) Validate() error {
	if c.URI == "" {
		return exception.ValidateError(exception.StatusStoreGraphParamInvalid,
			exception.WithParam("error_msg", "URI 不能为空"))
	}
	if c.Timeout <= 0 {
		return exception.ValidateError(exception.StatusStoreGraphParamInvalid,
			exception.WithParam("error_msg", fmt.Sprintf("Timeout 必须大于 0，当前值: %v", c.Timeout)))
	}
	if c.EmbedDim < 32 {
		return exception.ValidateError(exception.StatusStoreGraphParamInvalid,
			exception.WithParam("error_msg", fmt.Sprintf("EmbedDim 必须 ≥ 32，当前值: %d", c.EmbedDim)))
	}
	if c.MaxConcurrent < 0 {
		return exception.ValidateError(exception.StatusStoreGraphParamInvalid,
			exception.WithParam("error_msg", fmt.Sprintf("MaxConcurrent 必须 ≥ 0，当前值: %d", c.MaxConcurrent)))
	}
	if c.EmbedBatchSize < 1 {
		return exception.ValidateError(exception.StatusStoreGraphParamInvalid,
			exception.WithParam("error_msg", fmt.Sprintf("EmbedBatchSize 必须 ≥ 1，当前值: %d", c.EmbedBatchSize)))
	}
	return nil
}
