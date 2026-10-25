package vector_store

import (
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/vector_fields"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 结构体 ────────────────────────────

// StoreCreateOptions 向量存储构造选项（收纳 Python 构造 **kwargs）。
//
// Python create_vector_store(config, **kwargs) 的 kwargs 透传各构造器；
// 其中 vector_field 为 str | ChromaVectorField / str | MilvusVectorField
// 联合类型——str 侧由 VectorFieldName 承载（工厂转强类型），定义侧由
// ChromaVectorField/MilvusVectorField 承载（非 nil 优先直传）。
// 零值字段在工厂内统一填充 Python 构造默认值。
type StoreCreateOptions struct {
	// ChromaPath ChromaDB 持久化路径（Chroma 专用，Python chroma_path）
	ChromaPath string
	// MilvusURI Milvus 连接 URI（Milvus 专用，Python milvus_uri）
	MilvusURI string
	// MilvusToken Milvus 连接令牌（Python milvus_token）
	MilvusToken string
	// TextField 文本字段名（零值时工厂填默认 "content"）
	TextField string
	// VectorFieldName 向量字段名 str 侧（零值时工厂填默认 "embedding" 并转强类型）
	VectorFieldName string
	// ChromaVectorField Chroma 向量字段定义侧（非 nil 优先，Chroma 专用）
	ChromaVectorField *vector_fields.ChromaVectorField
	// MilvusVectorField Milvus 向量字段定义侧（非 nil 优先，Milvus 专用）
	MilvusVectorField vector_fields.MilvusVectorFieldConfig
	// SparseVectorField 稀疏向量字段名（零值时工厂填默认 "sparse_vector"）
	SparseVectorField string
	// MetadataField 元数据字段名（零值时工厂填默认 "metadata"）
	MetadataField string
	// DocIDField 文档 ID 字段名（零值时工厂填默认 "document_id"）
	DocIDField string
}

// StoreCreateOption StoreCreateOptions 的函数选项。
type StoreCreateOption func(*StoreCreateOptions)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// defaultTextField Python text_field 默认值
	defaultTextField = "content"
	// defaultVectorFieldName Python vector_field str 默认值
	defaultVectorFieldName = "embedding"
	// defaultSparseVectorField Python sparse_vector_field 默认值
	defaultSparseVectorField = "sparse_vector"
	// defaultMetadataField Python metadata_field 默认值
	defaultMetadataField = "metadata"
	// defaultDocIDField Python doc_id_field 默认值
	defaultDocIDField = "document_id"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewStoreCreateOptions 从可变参数构建 StoreCreateOptions。
func NewStoreCreateOptions(opts ...StoreCreateOption) StoreCreateOptions {
	o := StoreCreateOptions{}
	for _, opt := range opts {
		opt(&o)
	}
	return o
}

// WithStoreChromaPath 设置 ChromaDB 路径。
func WithStoreChromaPath(path string) StoreCreateOption {
	return func(o *StoreCreateOptions) { o.ChromaPath = path }
}

// WithStoreMilvusURI 设置 Milvus URI。
func WithStoreMilvusURI(uri string) StoreCreateOption {
	return func(o *StoreCreateOptions) { o.MilvusURI = uri }
}

// WithStoreMilvusToken 设置 Milvus 令牌。
func WithStoreMilvusToken(token string) StoreCreateOption {
	return func(o *StoreCreateOptions) { o.MilvusToken = token }
}

// WithStoreTextField 设置文本字段名。
func WithStoreTextField(name string) StoreCreateOption {
	return func(o *StoreCreateOptions) { o.TextField = name }
}

// WithStoreVectorFieldName 设置向量字段名（str 联合分支）。
func WithStoreVectorFieldName(name string) StoreCreateOption {
	return func(o *StoreCreateOptions) { o.VectorFieldName = name }
}

// WithStoreChromaVectorField 设置 Chroma 向量字段定义（联合分支的实例侧）。
func WithStoreChromaVectorField(vf *vector_fields.ChromaVectorField) StoreCreateOption {
	return func(o *StoreCreateOptions) { o.ChromaVectorField = vf }
}

// WithStoreMilvusVectorField 设置 Milvus 向量字段定义（联合分支的实例侧）。
func WithStoreMilvusVectorField(vf vector_fields.MilvusVectorFieldConfig) StoreCreateOption {
	return func(o *StoreCreateOptions) { o.MilvusVectorField = vf }
}

// WithStoreSparseVectorField 设置稀疏向量字段名。
func WithStoreSparseVectorField(name string) StoreCreateOption {
	return func(o *StoreCreateOptions) { o.SparseVectorField = name }
}

// WithStoreMetadataField 设置元数据字段名。
func WithStoreMetadataField(name string) StoreCreateOption {
	return func(o *StoreCreateOptions) { o.MetadataField = name }
}

// WithStoreDocIDField 设置文档 ID 字段名。
func WithStoreDocIDField(name string) StoreCreateOption {
	return func(o *StoreCreateOptions) { o.DocIDField = name }
}

// CreateVectorStore 根据配置动态创建向量存储实例。
//
// Python: openjiuwen/core/retrieval/vector_store/store.py (create_vector_store)。
// milvus/chroma 分支组装既有构造器（含 str→强类型转换，对齐 Python
// isinstance 分支）；pgvector 分支返回 155402 not-implemented（⤵️ PGVectorStore
// 依赖 pgvector-go 拆后交付，届时只改本分支内部）；default 分支 155402
// 错误文案逐字符对齐 Python（含逗号+空格拼接）。
func CreateVectorStore(config common.VectorStoreConfig, opts ...StoreCreateOption) (VectorStore, error) {
	o := NewStoreCreateOptions(opts...)

	switch config.StoreProvider {
	case common.StoreTypeMilvus:
		vectorField := o.MilvusVectorField
		if vectorField == nil {
			// 对齐 Python: isinstance(vector_field, str) → MilvusAUTO(vector_field=...)
			name := o.VectorFieldName
			if name == "" {
				name = defaultVectorFieldName
			}
			vectorField = vector_fields.NewMilvusAUTO(name)
		}
		return NewMilvusVectorStore(
			config, o.MilvusURI, o.MilvusToken,
			defaultIfEmpty(o.TextField, defaultTextField), vectorField,
			defaultIfEmpty(o.SparseVectorField, defaultSparseVectorField),
			defaultIfEmpty(o.MetadataField, defaultMetadataField),
			defaultIfEmpty(o.DocIDField, defaultDocIDField),
		)

	case common.StoreTypeChroma:
		vectorField := o.ChromaVectorField
		if vectorField == nil {
			// 对齐 Python: isinstance(vector_field, str) → ChromaVectorField(vector_field=...)
			name := o.VectorFieldName
			if name == "" {
				name = defaultVectorFieldName
			}
			vectorField = vector_fields.NewChromaVectorFieldFromName(name)
		}
		return NewChromaVectorStore(
			config, o.ChromaPath,
			defaultIfEmpty(o.TextField, defaultTextField), vectorField,
			defaultIfEmpty(o.SparseVectorField, defaultSparseVectorField),
			defaultIfEmpty(o.MetadataField, defaultMetadataField),
			defaultIfEmpty(o.DocIDField, defaultDocIDField),
		)

	case common.StoreTypePGVector:
		// ⤵️ PGVectorStore（pgvector-go 新依赖）拆后交付——届时只改本分支内部
		return nil, exception.BuildError(
			exception.StatusRetrievalVectorStoreProviderInvalid,
			exception.WithParam("error_msg", "pgvector store is not implemented yet, planned for later delivery"),
		)

	default:
		// 对齐 Python: "unavailable vector store provider: {provider},"
		//   f"and available providers are: {', '.join(...)}" 两行拼接（逗号+空格）
		return nil, exception.BuildError(
			exception.StatusRetrievalVectorStoreProviderInvalid,
			exception.WithParam("error_msg", fmt.Sprintf(
				"unavailable vector store provider: %s, and available providers are: milvus, chroma, pgvector",
				config.StoreProvider)),
		)
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// defaultIfEmpty 空串回落默认值（Python 构造参数默认值语义）。
func defaultIfEmpty(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
