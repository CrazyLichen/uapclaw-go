package vector

import (
	"context"
	"fmt"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// FieldSchema 集合中单个字段的 Schema 定义。
//
// 类似 Milvus FieldSchema，支持各种数据类型和字段属性。
// 通过 NewFieldSchema 构造，构造时自动校验字段合法性。
//
// 对应 Python: openjiuwen/core/foundation/store/base_vector_store.py (FieldSchema)
type FieldSchema struct {
	// Name 字段名
	Name string
	// DType 字段数据类型
	DType VectorDataType
	// IsPrimary 是否为主键字段
	IsPrimary bool
	// AutoID 是否自动生成 ID
	AutoID bool
	// MaxLength VARCHAR 字段最大长度，0 表示使用默认值 65535
	MaxLength int
	// Dim FLOAT_VECTOR 字段的向量维度，0 表示未设置
	Dim int
	// ElementType ARRAY 字段的元素类型，0 表示未设置
	ElementType VectorDataType
	// MaxCapacity ARRAY 字段的最大容量，0 表示未设置
	MaxCapacity int
	// Description 字段描述
	Description string
	// DefaultValue 字段默认值
	DefaultValue any
}

// CollectionSchema 向量集合的 Schema 定义。
//
// 类似 Milvus CollectionSchema，支持动态字段。
// fields 为未导出切片，通过方法访问和修改，保证校验逻辑不被绕过。
//
// 对应 Python: openjiuwen/core/foundation/store/base_vector_store.py (CollectionSchema)
type CollectionSchema struct {
	// fields 字段定义列表（未导出，通过方法访问）
	fields []*FieldSchema
	// Description 集合描述
	Description string
	// EnableDynamicField 是否启用动态字段
	EnableDynamicField bool
}

// VectorSearchResult 向量搜索结果。
//
// 对应 Python: openjiuwen/core/foundation/store/base_vector_store.py (VectorSearchResult)
type VectorSearchResult struct {
	// Score 相关度分数（越高越相关）
	Score float64
	// Fields 匹配文档的所有字段值（包括 id, text, metadata 等）
	Fields map[string]any
}

// BaseVectorStore 向量存储后端的抽象接口。
//
// 所有向量存储后端（Chroma、Milvus、Gauss 等）必须实现此接口。
// 方法全部为同步风格，调用者可按需通过 goroutine 实现并发。
//
// 对应 Python: openjiuwen/core/foundation/store/base_vector_store.py (BaseVectorStore)
type BaseVectorStore interface {
	// CreateCollection 创建集合，schema 定义字段结构。
	//
	// 对应 Python: BaseVectorStore.create_collection(collection_name, schema, **kwargs)
	CreateCollection(ctx context.Context, collectionName string, schema *CollectionSchema, opts ...Option) error

	// DeleteCollection 删除集合。
	//
	// 对应 Python: BaseVectorStore.delete_collection(collection_name, **kwargs)
	DeleteCollection(ctx context.Context, collectionName string, opts ...Option) error

	// CollectionExists 检查集合是否存在。
	//
	// 对应 Python: BaseVectorStore.collection_exists(collection_name, **kwargs)
	CollectionExists(ctx context.Context, collectionName string, opts ...Option) (bool, error)

	// GetSchema 获取集合的 Schema。
	//
	// 对应 Python: BaseVectorStore.get_schema(collection_name, **kwargs)
	GetSchema(ctx context.Context, collectionName string, opts ...Option) (*CollectionSchema, error)

	// AddDocs 添加文档到集合。
	// 每个文档是包含 id/embedding/text/metadata 等字段的 map。
	//
	// 对应 Python: BaseVectorStore.add_docs(collection_name, docs, **kwargs)
	AddDocs(ctx context.Context, collectionName string, docs []map[string]any, opts ...Option) error

	// Search 向量相似度搜索。
	// queryVector: 查询向量
	// vectorField: 搜索的向量字段名（如 "embedding"）
	// topK: 返回结果数量，0 使用默认值 5
	// filters: 标量字段过滤条件，nil 表示无过滤
	//
	// 对应 Python: BaseVectorStore.search(collection_name, query_vector, vector_field, top_k=5, filters=None, **kwargs)
	Search(ctx context.Context, collectionName string, queryVector []float64, vectorField string, topK int, filters map[string]any, opts ...Option) ([]VectorSearchResult, error)

	// DeleteDocsByIDs 按 ID 删除文档。
	//
	// 对应 Python: BaseVectorStore.delete_docs_by_ids(collection_name, ids, **kwargs)
	DeleteDocsByIDs(ctx context.Context, collectionName string, ids []string, opts ...Option) error

	// DeleteDocsByFilters 按标量字段过滤条件删除文档。
	//
	// 对应 Python: BaseVectorStore.delete_docs_by_filters(collection_name, filters, **kwargs)
	DeleteDocsByFilters(ctx context.Context, collectionName string, filters map[string]any, opts ...Option) error

	// ListCollectionNames 列出所有集合名称。
	//
	// 对应 Python: BaseVectorStore.list_collection_names()
	ListCollectionNames(ctx context.Context) ([]string, error)

	// UpdateSchema 执行 schema 迁移操作。
	// ⤵️ 预留：实际迁移逻辑待 7.22/7.23 实现后回填。
	//
	// 对应 Python: BaseVectorStore.update_schema(collection_name, operations)
	UpdateSchema(ctx context.Context, collectionName string, operations []any, opts ...Option) error

	// UpdateCollectionMetadata 更新集合元数据。
	//
	// 对应 Python: BaseVectorStore.update_collection_metadata(collection_name, metadata)
	UpdateCollectionMetadata(ctx context.Context, collectionName string, metadata map[string]any, opts ...Option) error

	// GetCollectionMetadata 获取集合元数据。
	//
	// 对应 Python: BaseVectorStore.get_collection_metadata(collection_name)
	GetCollectionMetadata(ctx context.Context, collectionName string, opts ...Option) (map[string]any, error)
}

// Options 向量存储操作的可选参数集合
type Options struct {
	// DistanceMetric 距离度量方式（如 "COSINE"、"L2"、"IP"）
	DistanceMetric string
	// BatchSize 批量操作的批次大小
	BatchSize int
	// OutputFields 搜索结果中需要返回的字段列表
	OutputFields []string
	// VectorField 向量索引配置，用于 CreateCollection 时指定索引参数
	VectorField any
	// ShardsNum 创建集合时的分片数，0 表示使用服务端默认值，对齐 Python: create_collection 的 shards_num
	ShardsNum int32
	// NumCandidates ES k-NN 搜索候选集大小，0 表示使用默认值 max(topK*10, 100)
	NumCandidates int
}

// ──────────────────────────── 枚举 ────────────────────────────

// VectorDataType 向量存储支持的字段数据类型。
//
// 对应 Python: openjiuwen/core/foundation/store/base_vector_store.py (VectorDataType)
type VectorDataType int

const (
	// VectorDataTypeVarchar 变长字符串
	VectorDataTypeVarchar VectorDataType = iota
	// VectorDataTypeFloatVector 浮点向量
	VectorDataTypeFloatVector
	// VectorDataTypeInt64 64位整数
	VectorDataTypeInt64
	// VectorDataTypeInt32 32位整数
	VectorDataTypeInt32
	// VectorDataTypeInt16 16位整数
	VectorDataTypeInt16
	// VectorDataTypeInt8 8位整数
	VectorDataTypeInt8
	// VectorDataTypeFloat 浮点数
	VectorDataTypeFloat
	// VectorDataTypeDouble 双精度浮点数
	VectorDataTypeDouble
	// VectorDataTypeBool 布尔值
	VectorDataTypeBool
	// VectorDataTypeJSON JSON 对象
	VectorDataTypeJSON
	// VectorDataTypeArray 数组
	VectorDataTypeArray
)

// FieldOption FieldSchema 构造选项
type FieldOption func(*FieldSchema)

// CollectionOption CollectionSchema 构造选项
type CollectionOption func(*CollectionSchema)

// Option 向量存储操作的通用可选参数
type Option func(*Options)

// ──────────────────────────── 常量 ────────────────────────────

// defaultMaxLength VARCHAR 字段默认最大长度，对齐 Python FieldSchema.max_length 默认值
const defaultMaxLength = 65535

// ──────────────────────────── 全局变量 ────────────────────────────

// vectorDataTypeStrings VectorDataType 枚举值对应的字符串表示，与 Python VectorDataType 枚举值保持一致。
var vectorDataTypeStrings = [...]string{
	"VARCHAR",
	"FLOAT_VECTOR",
	"INT64",
	"INT32",
	"INT16",
	"INT8",
	"FLOAT",
	"DOUBLE",
	"BOOL",
	"JSON",
	"ARRAY",
}

// ──────────────────────────── 导出函数 ────────────────────────────

// String 返回 VectorDataType 的字符串表示，与 Python 枚举值一致。
func (dt VectorDataType) String() string {
	if dt >= 0 && int(dt) < len(vectorDataTypeStrings) {
		return vectorDataTypeStrings[dt]
	}
	return fmt.Sprintf("UNKNOWN(%d)", dt)
}

// WithPrimary 设置为主键字段
func WithPrimary() FieldOption {
	return func(f *FieldSchema) { f.IsPrimary = true }
}

// WithAutoID 设置自动生成 ID
func WithAutoID() FieldOption {
	return func(f *FieldSchema) { f.AutoID = true }
}

// WithMaxLength 设置 VARCHAR 字段最大长度
func WithMaxLength(maxLen int) FieldOption {
	return func(f *FieldSchema) { f.MaxLength = maxLen }
}

// WithDim 设置向量维度
func WithDim(dim int) FieldOption {
	return func(f *FieldSchema) { f.Dim = dim }
}

// WithElementType 设置 ARRAY 元素类型
func WithElementType(dt VectorDataType) FieldOption {
	return func(f *FieldSchema) { f.ElementType = dt }
}

// WithMaxCapacity 设置 ARRAY 最大容量
func WithMaxCapacity(cap int) FieldOption {
	return func(f *FieldSchema) { f.MaxCapacity = cap }
}

// WithFieldDescription 设置字段描述
func WithFieldDescription(desc string) FieldOption {
	return func(f *FieldSchema) { f.Description = desc }
}

// WithDefaultValue 设置字段默认值
func WithDefaultValue(val any) FieldOption {
	return func(f *FieldSchema) { f.DefaultValue = val }
}

// WithCollectionDescription 设置集合描述
func WithCollectionDescription(desc string) CollectionOption {
	return func(s *CollectionSchema) { s.Description = desc }
}

// WithEnableDynamicField 启用动态字段
func WithEnableDynamicField() CollectionOption {
	return func(s *CollectionSchema) { s.EnableDynamicField = true }
}

// WithDistanceMetric 设置距离度量方式
func WithDistanceMetric(metric string) Option {
	return func(o *Options) { o.DistanceMetric = metric }
}

// WithBatchSize 设置批量操作的批次大小
func WithBatchSize(size int) Option {
	return func(o *Options) { o.BatchSize = size }
}

// WithOutputFields 设置搜索结果中需要返回的字段
func WithOutputFields(fields ...string) Option {
	return func(o *Options) { o.OutputFields = fields }
}

// WithVectorField 设置向量索引配置
func WithVectorField(vf any) Option {
	return func(o *Options) { o.VectorField = vf }
}

// WithShardsNum 设置创建集合时的分片数，默认 1。
// 大数据量场景可增加分片数提升写入吞吐。
func WithShardsNum(n int32) Option {
	return func(o *Options) { o.ShardsNum = n }
}

// WithNumCandidates 设置 ES k-NN 搜索候选集大小。
// 候选集越大搜索越精确但越慢，0 表示使用默认值 max(topK*10, 100)。
func WithNumCandidates(n int) Option {
	return func(o *Options) { o.NumCandidates = n }
}

// NewFieldSchema 创建并校验 FieldSchema。
//
// 校验规则：
//   - 名字不能为空
//   - DType 为 FloatVector 时 Dim 必须大于 0
//   - Dim 小于 0 时返回错误
//
// 对应 Python: FieldSchema(name=..., dtype=..., ...) 的 Pydantic 校验
func NewFieldSchema(name string, dtype VectorDataType, opts ...FieldOption) (*FieldSchema, error) {
	if name == "" {
		return nil, exception.BuildError(exception.StatusStoreVectorSchemaInvalid,
			exception.WithParam("error_msg", "field name is empty"),
		)
	}
	f := &FieldSchema{
		Name:  name,
		DType: dtype,
	}
	for _, opt := range opts {
		opt(f)
	}
	// 校验 dim：dim 小于 0 非法
	if f.Dim < 0 {
		return nil, exception.BuildError(exception.StatusStoreVectorSchemaInvalid,
			exception.WithParam("error_msg", fmt.Sprintf("dim of vector field is invalid, field=%s, dim=%d", f.Name, f.Dim)),
		)
	}
	// FLOAT_VECTOR 必须提供 dim
	if f.DType == VectorDataTypeFloatVector && f.Dim == 0 {
		return nil, exception.BuildError(exception.StatusStoreVectorSchemaInvalid,
			exception.WithParam("error_msg", fmt.Sprintf("dim of vector field is missing, field=%s, dim=%d", f.Name, f.Dim)),
		)
	}
	return f, nil
}

// ToDict 将字段 Schema 转为字典格式。
//
// 只包含非零值字段。VARCHAR 类型的 MaxLength 未显式设置时输出默认值 65535（对齐 Python 序列化行为）。
// 字段 type 使用 Python 兼容的字符串值（如 "VARCHAR"、"FLOAT_VECTOR"）。
//
// 对应 Python: FieldSchema.to_dict()
func (f *FieldSchema) ToDict() map[string]any {
	result := map[string]any{
		"name": f.Name,
		"type": f.DType.String(),
	}
	if f.IsPrimary {
		result["is_primary"] = true
	}
	if f.AutoID {
		result["auto_id"] = true
	}
	// MaxLength: VARCHAR 类型未设置时输出默认值 65535（对齐 Python max_length 默认值）
	if f.DType == VectorDataTypeVarchar {
		ml := f.MaxLength
		if ml == 0 {
			ml = defaultMaxLength
		}
		result["max_length"] = ml
	} else if f.MaxLength > 0 {
		result["max_length"] = f.MaxLength
	}
	if f.Dim > 0 {
		result["dim"] = f.Dim
	}
	if int(f.ElementType) != 0 {
		result["element_type"] = f.ElementType.String()
	}
	if f.MaxCapacity > 0 {
		result["max_capacity"] = f.MaxCapacity
	}
	if f.Description != "" {
		result["description"] = f.Description
	}
	if f.DefaultValue != nil {
		result["default_value"] = f.DefaultValue
	}
	return result
}

// FieldFromDict 从字典创建 FieldSchema。
//
// 字典中字段类型键名支持 "type" 或 "dtype"（兼容 Python 两种写法）。
// 枚举值不区分大小写，如 "varchar" 和 "VARCHAR" 均可。
//
// 对应 Python: FieldSchema.from_dict()
func FieldFromDict(data map[string]any) (*FieldSchema, error) {
	name, _ := data["name"].(string)
	if name == "" {
		return nil, exception.BuildError(exception.StatusStoreVectorSchemaInvalid,
			exception.WithParam("error_msg", "field name is missing in dict"),
		)
	}
	// 兼容 "type" 和 "dtype" 两种键名
	dtypeStr := ""
	if v, ok := data["type"]; ok {
		dtypeStr, _ = v.(string)
	} else if v, ok := data["dtype"]; ok {
		dtypeStr, _ = v.(string)
	}
	dtype := vectorDataTypeFromString(dtypeStr)

	opts := make([]FieldOption, 0)
	if v, ok := data["is_primary"].(bool); ok && v {
		opts = append(opts, WithPrimary())
	}
	if v, ok := data["auto_id"].(bool); ok && v {
		opts = append(opts, WithAutoID())
	}
	if v, ok := data["max_length"]; ok {
		switch n := v.(type) {
		case int:
			opts = append(opts, WithMaxLength(n))
		case float64:
			opts = append(opts, WithMaxLength(int(n)))
		}
	}
	if v, ok := data["dim"]; ok {
		switch n := v.(type) {
		case int:
			opts = append(opts, WithDim(n))
		case float64:
			opts = append(opts, WithDim(int(n)))
		}
	}
	if v, ok := data["element_type"]; ok {
		if s, ok := v.(string); ok {
			opts = append(opts, WithElementType(vectorDataTypeFromString(s)))
		}
	}
	if v, ok := data["max_capacity"]; ok {
		switch n := v.(type) {
		case int:
			opts = append(opts, WithMaxCapacity(n))
		case float64:
			opts = append(opts, WithMaxCapacity(int(n)))
		}
	}
	if v, ok := data["description"].(string); ok && v != "" {
		opts = append(opts, WithFieldDescription(v))
	}
	if v, ok := data["default_value"]; ok {
		opts = append(opts, WithDefaultValue(v))
	}

	return NewFieldSchema(name, dtype, opts...)
}

// NewCollectionSchema 创建并校验 CollectionSchema。
//
// 校验规则：最多只能有一个主键字段。
//
// 对应 Python: CollectionSchema(fields=[], ...)
func NewCollectionSchema(opts ...CollectionOption) (*CollectionSchema, error) {
	s := &CollectionSchema{}
	for _, opt := range opts {
		opt(s)
	}
	return s, nil
}

// NewCollectionSchemaFromFields 从字段列表创建 Schema。
//
// 逐个添加字段并校验，任一字段冲突则返回错误。
//
// 对应 Python: CollectionSchema.from_fields(fields, **kwargs)
func NewCollectionSchemaFromFields(fields []*FieldSchema, opts ...CollectionOption) (*CollectionSchema, error) {
	schema, err := NewCollectionSchema(opts...)
	if err != nil {
		return nil, err
	}
	for _, f := range fields {
		if _, err := schema.AddField(f); err != nil {
			return nil, err
		}
	}
	return schema, nil
}

// AddField 添加字段到 Schema（原地修改，返回自身以支持链式调用）。
//
// 校验规则：
//   - 字段名不能重复
//   - 不能添加第二个主键字段
//
// 对应 Python: CollectionSchema.add_field(field)
func (s *CollectionSchema) AddField(field *FieldSchema) (*CollectionSchema, error) {
	// 检查重名
	for _, f := range s.fields {
		if f.Name == field.Name {
			return nil, exception.BuildError(exception.StatusStoreVectorSchemaInvalid,
				exception.WithParam("error_msg", fmt.Sprintf("field name already exists, field=%s", field.Name)),
			)
		}
	}
	// 检查主键冲突
	if field.IsPrimary {
		for _, f := range s.fields {
			if f.IsPrimary {
				return nil, exception.BuildError(exception.StatusStoreVectorSchemaInvalid,
					exception.WithParam("error_msg", fmt.Sprintf(
						"collection can have at most one primary key field, primary_field=%s, field=%s",
						f.Name, field.Name)),
				)
			}
		}
	}
	s.fields = append(s.fields, field)
	return s, nil
}

// RemoveField 按名称移除字段（原地修改，返回自身以支持链式调用）。
//
// 对应 Python: CollectionSchema.remove_field(field_name)
func (s *CollectionSchema) RemoveField(fieldName string) *CollectionSchema {
	filtered := make([]*FieldSchema, 0, len(s.fields))
	for _, f := range s.fields {
		if f.Name != fieldName {
			filtered = append(filtered, f)
		}
	}
	s.fields = filtered
	return s
}

// GetField 按名称获取字段，不存在返回 nil。
//
// 对应 Python: CollectionSchema.get_field(field_name)
func (s *CollectionSchema) GetField(fieldName string) *FieldSchema {
	for _, f := range s.fields {
		if f.Name == fieldName {
			return f
		}
	}
	return nil
}

// HasField 检查字段是否存在。
//
// 对应 Python: CollectionSchema.has_field(field_name)
func (s *CollectionSchema) HasField(fieldName string) bool {
	return s.GetField(fieldName) != nil
}

// GetPrimaryKeyField 获取主键字段，不存在返回 nil。
//
// 对应 Python: CollectionSchema.get_primary_key_field()
func (s *CollectionSchema) GetPrimaryKeyField() *FieldSchema {
	for _, f := range s.fields {
		if f.IsPrimary {
			return f
		}
	}
	return nil
}

// GetVectorFields 获取所有 FLOAT_VECTOR 类型的字段。
//
// 对应 Python: CollectionSchema.get_vector_fields()
func (s *CollectionSchema) GetVectorFields() []*FieldSchema {
	var result []*FieldSchema
	for _, f := range s.fields {
		if f.DType == VectorDataTypeFloatVector {
			result = append(result, f)
		}
	}
	return result
}

// Fields 返回字段列表的副本（防止外部直接修改内部切片）。
func (s *CollectionSchema) Fields() []*FieldSchema {
	result := make([]*FieldSchema, len(s.fields))
	copy(result, s.fields)
	return result
}

// ToDict 将 Schema 转为字典格式（序列化用）。
//
// 对应 Python: CollectionSchema.to_dict()
func (s *CollectionSchema) ToDict() map[string]any {
	fields := make([]map[string]any, len(s.fields))
	for i, f := range s.fields {
		fields[i] = f.ToDict()
	}
	return map[string]any{
		"fields":               fields,
		"description":          s.Description,
		"enable_dynamic_field": s.EnableDynamicField,
	}
}

// CollectionFromDict 从字典创建 CollectionSchema。
//
// 对应 Python: CollectionSchema.from_dict(data)
func CollectionFromDict(data map[string]any) (*CollectionSchema, error) {
	opts := make([]CollectionOption, 0)
	if v, ok := data["description"].(string); ok {
		opts = append(opts, WithCollectionDescription(v))
	}
	if v, ok := data["enable_dynamic_field"].(bool); ok && v {
		opts = append(opts, WithEnableDynamicField())
	}

	schema, err := NewCollectionSchema(opts...)
	if err != nil {
		return nil, err
	}

	fieldsRaw, ok := data["fields"]
	if !ok {
		return schema, nil
	}

	// 支持两种格式：[]map[string]any（ToDict 生成）或 []any（JSON 反序列化）
	switch fields := fieldsRaw.(type) {
	case []map[string]any:
		for _, fd := range fields {
			f, err := FieldFromDict(fd)
			if err != nil {
				return nil, err
			}
			if _, err := schema.AddField(f); err != nil {
				return nil, err
			}
		}
	case []any:
		for _, item := range fields {
			fd, ok := item.(map[string]any)
			if !ok {
				continue
			}
			f, err := FieldFromDict(fd)
			if err != nil {
				return nil, err
			}
			if _, err := schema.AddField(f); err != nil {
				return nil, err
			}
		}
	}

	return schema, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// newOptions 从选项列表构造 Options
func newOptions(opts ...Option) Options {
	var o Options
	for _, opt := range opts {
		opt(&o)
	}
	return o
}

// vectorDataTypeFromString 从字符串解析 VectorDataType，不区分大小写。
func vectorDataTypeFromString(s string) VectorDataType {
	upper := strings.ToUpper(s)
	for i, v := range vectorDataTypeStrings {
		if v == upper {
			return VectorDataType(i)
		}
	}
	// 未知类型回退 VARCHAR，与 Python from_dict 的 fallback 一致
	// 但 Python 的 VectorDataType("UNKNOWN") 会抛 ValueError，这里加警告日志
	logger.Warn(logComponent).
		Str("type", s).
		Str("fallback", "VARCHAR").
		Msg("未知的向量字段类型，回退为 VARCHAR")
	return VectorDataTypeVarchar
}
