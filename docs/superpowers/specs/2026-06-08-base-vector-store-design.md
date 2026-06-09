# BaseVectorStore 接口设计

## 概述

领域四（存储层）4.6 小节的实现设计：Go 版 BaseVectorStore 接口及配套数据模型（VectorDataType / FieldSchema / CollectionSchema / VectorSearchResult），对照 Python 源码 `openjiuwen/core/foundation/store/base_vector_store.py` 进行迁移。

## 设计决策

| 决策项 | 选择 | 理由 |
|--------|------|------|
| 接口风格 | 同步（ctx + 阻塞调用） | 对齐已有 BaseKVStore 风格；Go 的 goroutine 天然并发，调用者按需 `go` 即可，比 Python asyncio 更强 |
| 文件组织 | 单文件 `base.go` | 接口 + Schema + 枚举全放 base.go，类似 Python 单文件组织，简单直接 |
| Schema 校验 | 构造函数校验 | `NewFieldSchema()` / `NewCollectionSchema()` 内校验，不合法返回 error；比延迟 Validate 更严格 |
| CollectionSchema 可变性 | 指针可变 | `AddField()` 修改 `*CollectionSchema` 并返回自身，对齐 Python 原地修改 + 返回 self 的行为 |
| 可选参数 | `opts ...Option` 函数选项模式 | 替代 Python `**kwargs`，灵活可扩展，对齐 Go 惯例 |
| schema 参数类型 | `CollectionSchema`（强类型） | Python 接受 `Union[CollectionSchema, Dict]`，Go 用强类型；调用者需要时自行从 map 构造 |
| 迁移相关方法 | 延后 | `UpdateSchema` / `UpdateCollectionMetadata` / `GetCollectionMetadata` 依赖迁移系统（BaseOperation），4.6 不实现 |
| Python async 与 Go sync 的性能 | 等价或更优 | Python 的 await 是单线程协作式并发；Go 同步接口 + goroutine = 真正并行，I/O 用非阻塞 syscall |

## 文件结构

```
internal/agentcore/store/vector/
├── doc.go        # 包文档
└── base.go       # VectorDataType + FieldSchema + CollectionSchema + VectorSearchResult + BaseVectorStore + Option
```

包路径：`internal/agentcore/store/vector`

对应 Python 代码：`openjiuwen/core/foundation/store/base_vector_store.py`

## 类型定义

### VectorDataType

```go
// VectorDataType 向量存储支持的字段数据类型
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
```

Python 枚举值为字符串（"VARCHAR"、"FLOAT_VECTOR" 等），Go 用 iota + `String()` 方法。`String()` 返回与 Python 枚举值相同的字符串，便于序列化和跨语言交互。

### FieldSchema

```go
// FieldSchema 集合中单个字段的 Schema 定义
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
    // Dim FLOAT_VECTOR 字段的向量维度
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

// NewFieldSchema 创建并校验 FieldSchema
// 校验规则：
//   - Dim 必须大于 0（FLOAT_VECTOR 类型必须提供 Dim）
//   - DType 为 FloatVector 时 Dim 不能为 0
func NewFieldSchema(name string, dtype VectorDataType, opts ...FieldOption) (*FieldSchema, error)

// ToDict 将字段 Schema 转为字典格式（只包含非零值字段）
func (f *FieldSchema) ToDict() map[string]any

// FieldFromDict 从字典创建 FieldSchema
func FieldFromDict(data map[string]any) (*FieldSchema, error)
```

**与 Python 的差异**：
- Python 用 Pydantic `field_validator` 校验 dim；Go 在 `NewFieldSchema()` 中手动校验
- Python 的 `Optional[int]` 字段默认 None；Go 用零值（0 / "" / nil）+ 布尔语义区分"未设置"
- Python 的 `max_length` 默认 65535；Go 的 `MaxLength` 默认 0，`ToDict()` 时 0 → 65535（对齐 Python 序列化行为）

### CollectionSchema

```go
// CollectionSchema 向量集合的 Schema 定义
type CollectionSchema struct {
    // Fields 字段定义列表
    fields []*FieldSchema
    // Description 集合描述
    Description string
    // EnableDynamicField 是否启用动态字段
    EnableDynamicField bool
}

// NewCollectionSchema 创建并校验 CollectionSchema
// 校验规则：
//   - 最多只能有一个主键字段
func NewCollectionSchema(opts ...CollectionOption) (*CollectionSchema, error)

// AddField 添加字段到 Schema（原地修改，返回自身以支持链式调用）
// 校验规则：
//   - 字段名不能重复
//   - 不能添加第二个主键字段
func (s *CollectionSchema) AddField(field *FieldSchema) (*CollectionSchema, error)

// RemoveField 按名称移除字段（原地修改，返回自身以支持链式调用）
func (s *CollectionSchema) RemoveField(fieldName string) *CollectionSchema

// GetField 按名称获取字段
func (s *CollectionSchema) GetField(fieldName string) *FieldSchema

// HasField 检查字段是否存在
func (s *CollectionSchema) HasField(fieldName string) bool

// GetPrimaryKeyField 获取主键字段，不存在返回 nil
func (s *CollectionSchema) GetPrimaryKeyField() *FieldSchema

// GetVectorFields 获取所有 FLOAT_VECTOR 类型的字段
func (s *CollectionSchema) GetVectorFields() []*FieldSchema

// ToDict 将 Schema 转为字典格式（序列化用）
func (s *CollectionSchema) ToDict() map[string]any

// CollectionFromDict 从字典创建 CollectionSchema
func CollectionFromDict(data map[string]any) (*CollectionSchema, error)

// NewCollectionSchemaFromFields 从字段列表创建 Schema
func NewCollectionSchemaFromFields(fields []*FieldSchema, opts ...CollectionOption) (*CollectionSchema, error)
```

**与 Python 的差异**：
- Python `add_field()` 接受 `Union[FieldSchema, Dict]`；Go 只接受 `*FieldSchema`，调用者需先构造
- Python 原地修改 + 返回 self；Go 原地修改 `*CollectionSchema` + 返回 `*CollectionSchema`（等价行为）
- Python `fields` 是 `List[FieldSchema]`；Go `fields` 是未导出切片，通过方法访问（封装性更好）

### VectorSearchResult

```go
// VectorSearchResult 向量搜索结果
type VectorSearchResult struct {
    // Score 相关度分数（越高越相关）
    Score float64
    // Fields 匹配文档的所有字段值（包括 id, text, metadata 等）
    Fields map[string]any
}
```

### Option 函数选项模式

```go
// Option 向量存储操作的通用可选参数
type Option func(*Options)

// Options 向量存储操作的可选参数集合
type Options struct {
    // DistanceMetric 距离度量方式（如 "COSINE"、"L2"、"IP"）
    DistanceMetric string
    // BatchSize 批量操作的批次大小
    BatchSize int
    // MetricType 搜索时的距离度量类型
    MetricType string
    // OutputFields 搜索结果中需要返回的字段列表
    OutputFields []string
}

// WithDistanceMetric 设置距离度量方式
func WithDistanceMetric(metric string) Option { ... }

// WithBatchSize 设置批量操作的批次大小
func WithBatchSize(size int) Option { ... }

// WithMetricType 设置搜索时的距离度量类型
func WithMetricType(metricType string) Option { ... }

// WithOutputFields 设置搜索结果中需要返回的字段
func WithOutputFields(fields ...string) Option { ... }
```

### FieldOption / CollectionOption

```go
// FieldOption FieldSchema 构造选项
type FieldOption func(*FieldSchema)

// WithPrimary 设置为主键字段
func WithPrimary() FieldOption { ... }

// WithAutoID 设置自动生成 ID
func WithAutoID() FieldOption { ... }

// WithMaxLength 设置 VARCHAR 字段最大长度
func WithMaxLength(maxLen int) FieldOption { ... }

// WithDim 设置向量维度
func WithDim(dim int) FieldOption { ... }

// WithElementType 设置 ARRAY 元素类型
func WithElementType(dt VectorDataType) FieldOption { ... }

// WithMaxCapacity 设置 ARRAY 最大容量
func WithMaxCapacity(cap int) FieldOption { ... }

// WithFieldDescription 设置字段描述
func WithFieldDescription(desc string) FieldOption { ... }

// WithDefaultValue 设置字段默认值
func WithDefaultValue(val any) FieldOption { ... }

// CollectionOption CollectionSchema 构造选项
type CollectionOption func(*CollectionSchema)

// WithCollectionDescription 设置集合描述
func WithCollectionDescription(desc string) CollectionOption { ... }

// WithEnableDynamicField 启用动态字段
func WithEnableDynamicField() CollectionOption { ... }
```

## 接口定义

### BaseVectorStore 接口

```go
// BaseVectorStore 向量存储后端的抽象接口
//
// 所有向量存储后端（Chroma、Milvus、Gauss 等）必须实现此接口。
// 方法全部为同步风格，调用者可按需通过 goroutine 实现并发。
type BaseVectorStore interface {
    // CreateCollection 创建集合，schema 定义字段结构
    CreateCollection(ctx context.Context, collectionName string, schema *CollectionSchema, opts ...Option) error

    // DeleteCollection 删除集合
    DeleteCollection(ctx context.Context, collectionName string, opts ...Option) error

    // CollectionExists 检查集合是否存在
    CollectionExists(ctx context.Context, collectionName string, opts ...Option) (bool, error)

    // GetSchema 获取集合的 Schema
    GetSchema(ctx context.Context, collectionName string, opts ...Option) (*CollectionSchema, error)

    // AddDocs 添加文档到集合
    // 每个文档是包含 id/embedding/text/metadata 等字段的 map
    AddDocs(ctx context.Context, collectionName string, docs []map[string]any, opts ...Option) error

    // Search 向量相似度搜索
    // queryVector: 查询向量
    // vectorField: 搜索的向量字段名（如 "embedding"）
    // topK: 返回结果数量，0 使用默认值 5
    // filters: 标量字段过滤条件，nil 表示无过滤
    Search(ctx context.Context, collectionName string, queryVector []float64, vectorField string, topK int, filters map[string]any, opts ...Option) ([]VectorSearchResult, error)

    // DeleteDocsByIDs 按 ID 删除文档
    DeleteDocsByIDs(ctx context.Context, collectionName string, ids []string, opts ...Option) error

    // DeleteDocsByFilters 按标量字段过滤条件删除文档
    DeleteDocsByFilters(ctx context.Context, collectionName string, filters map[string]any, opts ...Option) error

    // ListCollectionNames 列出所有集合名称
    ListCollectionNames(ctx context.Context) ([]string, error)
}
```

## Python → Go 方法映射

| Python 方法 | Go 方法 | 差异说明 |
|-------------|---------|---------|
| `async create_collection(name, schema, **kwargs)` | `CreateCollection(ctx, name, schema *CollectionSchema, opts ...Option) error` | schema 用指针类型；`**kwargs` → `opts` |
| `async delete_collection(name, **kwargs)` | `DeleteCollection(ctx, name, opts ...Option) error` | — |
| `async collection_exists(name, **kwargs) -> bool` | `CollectionExists(ctx, name, opts ...Option) (bool, error)` | — |
| `async get_schema(name, **kwargs) -> CollectionSchema` | `GetSchema(ctx, name, opts ...Option) (*CollectionSchema, error)` | 返回指针，允许 nil |
| `async add_docs(name, docs, **kwargs)` | `AddDocs(ctx, name, docs, opts ...Option) error` | docs 用 `[]map[string]any` 对齐 Python `List[Dict]` |
| `async search(name, query_vector, vector_field, top_k=5, filters=None, **kwargs)` | `Search(ctx, name, queryVector, vectorField, topK, filters, opts ...Option) ([]VectorSearchResult, error)` | topK 零值=默认5；filters nil=无过滤 |
| `async delete_docs_by_ids(name, ids, **kwargs)` | `DeleteDocsByIDs(ctx, name, ids, opts ...Option) error` | — |
| `async delete_docs_by_filters(name, filters, **kwargs)` | `DeleteDocsByFilters(ctx, name, filters, opts ...Option) error` | — |
| `async list_collection_names() -> List[str]` | `ListCollectionNames(ctx) ([]string, error)` | — |
| `async update_schema(name, operations)` | 延后 | 依赖迁移系统 BaseOperation |
| `async update_collection_metadata(name, metadata)` | 延后 | 依赖迁移系统 |
| `async get_collection_metadata(name)` | 延后 | 依赖迁移系统 |

## Python → Go 类型映射

| Python 类型 | Go 类型 | 差异说明 |
|------------|---------|---------|
| `VectorDataType(str, Enum)` | `VectorDataType int` (iota) | Go 枚举用 int + String()，序列化时输出 Python 兼容字符串 |
| `FieldSchema(BaseModel)` | `FieldSchema struct` + `NewFieldSchema()` | Pydantic → 手动构造函数校验 |
| `CollectionSchema(BaseModel)` | `CollectionSchema struct` + `NewCollectionSchema()` | Pydantic → 手动构造函数校验 |
| `VectorSearchResult(BaseModel)` | `VectorSearchResult struct` | — |
| `BaseVectorStore(ABC)` | `BaseVectorStore interface` | ABC → Go interface |
| `Union[CollectionSchema, Dict]` | `CollectionSchema` | Go 只用强类型，调用者自行从 map 构造 |
| `Optional[int]` | `int` (零值) | 0 = 未设置 / 默认值 |
| `Optional[Dict]` | `map[string]any` (nil) | nil = 未设置 |
| `List[Dict[str, Any]]` | `[]map[string]any` | — |
| `List[float]` | `[]float64` | — |
| `**kwargs` | `opts ...Option` | 函数选项模式替代 |

## 使用示例

### 创建集合

```go
schema, err := NewCollectionSchema(
    WithCollectionDescription("语义记忆集合"),
    WithEnableDynamicField(),
)
if err != nil {
    return err
}

idField, err := NewFieldSchema("id", VectorDataTypeVarchar, WithPrimary(), WithMaxLength(256))
if err != nil {
    return err
}
schema.AddField(idField)

embedField, err := NewFieldSchema("embedding", VectorDataTypeFloatVector, WithDim(768))
if err != nil {
    return err
}
schema.AddField(embedField)

err = store.CreateCollection(ctx, "uid_user1_gid_scope1_mtype_user_profile", schema)
```

### 添加文档

```go
docs := []map[string]any{
    {
        "id":        "mem_abc123",
        "embedding": []float64{0.023, -0.015, 0.042, /* ... 768 维 */},
    },
}
err := store.AddDocs(ctx, "uid_user1_gid_scope1_mtype_user_profile", docs)
```

### 搜索

```go
results, err := store.Search(
    ctx,
    "uid_user1_gid_scope1_mtype_user_profile",
    queryVector,  // []float64
    "embedding",
    10,           // topK
    nil,          // 无过滤
    WithMetricType("COSINE"),
    WithOutputFields("id"),
)
for _, r := range results {
    fmt.Printf("Score: %.4f, ID: %v\n", r.Score, r.Fields["id"])
}
```

## 范围说明

**4.6 范围内**：
- VectorDataType 枚举 + String() 方法
- FieldSchema 结构体 + NewFieldSchema() 构造校验
- CollectionSchema 结构体 + NewCollectionSchema() 构造校验 + AddField/RemoveField/GetField 等方法
- VectorSearchResult 结构体
- Option / FieldOption / CollectionOption 函数选项
- BaseVectorStore 接口（9 个核心方法）
- doc.go 包文档
- base.go 单元测试

**不在 4.6 范围内**（后续步骤）：
- 4.7 VectorFields 索引配置（MilvusHNSW / ChromaVectorField 等）
- 4.8 MilvusVectorStore 实现
- 4.9 ChromaVectorStore 实现
- 4.10 GaussVectorStore 实现
- 4.11 ESVectorStore 实现
- UpdateSchema / UpdateCollectionMetadata / GetCollectionMetadata（迁移系统实现时添加）
