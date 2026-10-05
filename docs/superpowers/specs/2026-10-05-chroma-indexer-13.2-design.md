# 13.2 ChromaIndexer + 前置依赖 实现设计

## 概述

实现领域 13.2 ChromaIndexer，包括其全部前置依赖：ChromaVectorField、DocIndexCallback、ComputeChunkEmbeddings 签名回填、retrieval 层 ChromaVectorStore。所有实现严格对齐 Python 源码。

## 执行子步骤

| 子步骤 | 内容 | 产出 |
|--------|------|------|
| 13.2-1 | ChromaVectorField struct + VectorField 基类 | `foundation/store/vector_fields/` 新包 |
| 13.2-2 | DocIndexCallback 接口 + NoOp + Logging 实现 | `retrieval/common/callbacks.go` |
| 13.2-3 | 回填 ComputeChunkEmbeddings 签名 | 加 `docIndexCallback` 可选参数，改为 option 模式 |
| 13.2-4 | Retrieval 层 ChromaVectorStore | `retrieval/vector_store/` 新包 + SearchResult/RetrievalResult/RRFFusion |
| 13.2-5 | ChromaIndexer | `retrieval/indexing/indexer/chroma.go`，实现 Indexer 接口 |

---

## 13.2-1：ChromaVectorField + VectorField 基类

### 包位置

对齐 Python `foundation/store/vector_fields/`，Go 放在 `internal/agentcore/foundation/store/vector_fields/`。

### 文件结构

```
internal/agentcore/foundation/store/vector_fields/
├── doc.go           # 包文档
├── base.go          # VectorField 基类 + Stage 常量 + shouldKeep + ToDict
├── chroma.go        # ChromaVectorField struct
├── base_test.go     # VectorField 测试
└── chroma_test.go   # ChromaVectorField 测试
```

### base.go — VectorField 基类

对齐 Python `vector_fields/base.py`。

```go
// VectorField 向量字段索引配置基类。
//
// 提供跨数据库后端的向量字段索引统一接口，支持构建/搜索阶段过滤。
// 通过 struct tag `stage:"construct"` / `stage:"search"` / `stage:"-"` 标记字段阶段，
// ToDict(stage) 通过反射读取 tag 过滤字段，对齐 Python should_keep(finfo, stage)。
//
// Python: foundation/store/vector_fields/base.py (VectorField)
type VectorField struct {
    // VectorFieldName 向量字段名，默认 "embedding"
    // 对齐 Python vector_field
    VectorFieldName string `json:"vector_field" stage:"-"`
    // DatabaseType 数据库类型，"chroma" | "milvus" | "pg"
    // 对齐 Python database_type
    DatabaseType string `json:"database_type" stage:"-"`
    // IndexType 索引类型，"auto" | "hnsw" | "flat" | "ivf" | "scann"
    // 对齐 Python index_type
    IndexType string `json:"index_type" stage:"-"`
}
```

**Stage tag 规则**：

| Python 标记 | Go tag | 作用 |
|------------|--------|------|
| `IS_CONSTRUCT = {"json_schema_extra": {"stage": "construct"}}` | `stage:"construct"` | 构建阶段字段 |
| `IS_SEARCH = {"json_schema_extra": {"stage": "search"}}` | `stage:"search"` | 搜索阶段字段 |
| 无 stage（内部字段：database_type/index_type/vector_field/variant） | `stage:"-"` | 始终被 ToDict 移除 |
| 基类无 stage 标记的字段 | 不加 stage tag | 始终包含 |

**ToDict 方法**：

```go
// ToDict 按阶段过滤字段并返回字典。
// 对齐 Python VectorField.to_dict(stage)。
// 通过反射遍历 struct 字段，读取 stage tag 过滤：
//   - stage:"construct" → 仅 construct 阶段返回
//   - stage:"search" → 仅 search 阶段返回
//   - stage:"-" → 始终移除（内部字段）
//   - 无 tag → 始终包含
// ExtraSearch/ExtraConstruct 等 map 字段被展开合并到返回字典中（键值对合并，原键移除）。
func ToDict(v any, stage string) map[string]any
```

**ShouldKeep 辅助函数**：

```go
// ShouldKeep 判断字段是否应在指定阶段保留。
// 对齐 Python VectorField.should_keep(finfo, stage)。
// 通过反射读取 struct tag `stage` 值判断。
func ShouldKeep(field reflect.StructField, stage string) bool
```

**导出函数**：

```go
// NewVectorField 创建默认 VectorField。
func NewVectorField(vectorFieldName, databaseType, indexType string) VectorField
```

### chroma.go — ChromaVectorField

对齐 Python `vector_fields/chroma_fields.py`。

```go
// ChromaVectorField ChromaDB HNSW 索引配置。
//
// 对齐 Python ChromaVectorField，含 HNSW 图参数和搜索参数。
// database_type 固定为 "chroma"，index_type 固定为 "hnsw"。
//
// Python: foundation/store/vector_fields/chroma_fields.py (ChromaVectorField)
type ChromaVectorField struct {
    VectorField                                                          // 嵌入基类
    // MaxNeighbors HNSW 图每个节点的最大边数。默认 16，范围 [2, 2048]
    MaxNeighbors int `json:"max_neighbors" stage:"construct"`
    // EfConstruction 构建时候选邻居数。默认 100，范围 [1, ∞)
    EfConstruction int `json:"ef_construction" stage:"construct"`
    // EfSearch 搜索时探索候选数。默认 100，范围 [1, ∞)
    // 注意：Python 中 ef_search 类型为 float，但实际使用为整数，Go 中使用 int
    EfSearch int `json:"ef_search" stage:"construct"`
    // ExtraSearch 额外搜索参数（resize_factor/num_threads/batch_size/sync_threshold）
    // TODO: 待 chroma-go SDK 支持后启用
    ExtraSearch map[string]any `json:"extra_search" stage:"search"`
}
```

**关键方法**：

```go
// ToConstructDict 返回构建阶段配置。
// 对齐 Python vector_field.to_dict(stage="construct")。
func (f *ChromaVectorField) ToConstructDict() map[string]any

// ToSearchDict 返回搜索阶段配置。
// 对齐 Python vector_field.to_dict(stage="search")。
func (f *ChromaVectorField) ToSearchDict() map[string]any

// ValidateExtraSearch 校验 ExtraSearch 字段类型。
// 对齐 Python ChromaVectorField.validate_kwargs。
// - resize_factor 必须为 int 或 float
// - num_threads/batch_size/sync_threshold 必须为 int
func (f *ChromaVectorField) ValidateExtraSearch() error
```

**构造函数**：

```go
// NewChromaVectorField 创建默认 ChromaVectorField。
// 对齐 Python ChromaVectorField() 默认值。
func NewChromaVectorField(opts ...ChromaVectorFieldOption) *ChromaVectorField

// NewChromaVectorFieldFromName 从向量字段名创建默认 ChromaVectorField。
// 对齐 Python ChromaVectorField(vector_field=name)。
// Python 中 vector_field 参数支持 str，自动包装为 ChromaVectorField。
func NewChromaVectorFieldFromName(name string) *ChromaVectorField
```

**Python 对齐差异**：

| Python | Go | 说明 |
|--------|-----|------|
| `ef_search: float = Field(default=100)` | `EfSearch int` | Python 类型为 float 但实际使用整数，Go 用 int 更准确 |
| `database_type: Literal["chroma"] = Field(init=False)` | 构造函数硬编码 `"chroma"` | Go 不需要 init=False |
| `index_type: Literal["hnsw"] = Field(init=False)` | 构造函数硬编码 `"hnsw"` | 同上 |
| `extra_search` 通过 `create_extra_search_field()` 工厂创建 | `ExtraSearch map[string]any` | Go 直接用 map |
| `validate_kwargs` 通过 Pydantic `@field_validator` 自动调用 | `ValidateExtraSearch() error` 显式调用 | Go 无 Pydantic 自动校验 |

---

## 13.2-2：DocIndexCallback

### 包位置

对齐 Python `retrieval/common/callbacks.py`，Go 放在 `internal/agentcore/retrieval/common/callbacks.go`。

### 类型定义

```go
// DocIndexCallback 文档索引进度回调接口。
//
// 用于追踪嵌入批次的处理进度。对齐 Python BaseCallback。
// 类型别名指向 foundation 层 embedding.Callback，保持编译期类型安全。
//
// Python: retrieval/common/callbacks.py (BaseCallback)
type DocIndexCallback = embedding.Callback
```

### NoOpDocIndexCallback

```go
// NoOpDocIndexCallback 空操作回调，默认使用。
//
// 对齐 Python BaseCallback 的空操作语义（仅计数器+1，无进度条）。
// 实现 embedding.Callback 接口。
type NoOpDocIndexCallback struct {
    // callCounter 调用计数器，对齐 Python BaseCallback._call_counter
    callCounter int
    // mu 线程锁，对齐 Python BaseCallback._thread_lock
    mu sync.Mutex
}

func (c *NoOpDocIndexCallback) OnBatchComplete(_ int, _ int, _ []string) {
    c.mu.Lock()
    c.callCounter++
    c.mu.Unlock()
}

// CallCounter 返回累计调用次数，对齐 Python BaseCallback.call_counter。
func (c *NoOpDocIndexCallback) CallCounter() int {
    c.mu.Lock()
    defer c.mu.Unlock()
    return c.callCounter
}
```

### LoggingDocIndexCallback

```go
// LoggingDocIndexCallback 日志进度回调。
//
// 对齐 Python TqdmCallback 的进度追踪语义。
// 每 100 批或最后一批打 Info 日志，对齐 Python ChromaVectorStore.add 中
// `if processed % 100 == 0: logger.info("Written %d/%d records")` 的逻辑。
type LoggingDocIndexCallback struct {
    // total 总批次数
    total int
    // processed 已处理批次数
    processed int
    // mu 线程锁
    mu sync.Mutex
}

func NewLoggingDocIndexCallback(total int) *LoggingDocIndexCallback

func (c *LoggingDocIndexCallback) OnBatchComplete(startIdx, endIdx int, batch []string) {
    c.mu.Lock()
    defer c.mu.Unlock()
    c.processed++
    if c.processed%100 == 0 || c.processed >= c.total {
        logger.Info(logComponent).
            Int("processed", endIdx).
            Int("total", c.total).
            Msg("嵌入进度")
    }
}
```

### Python 对齐差异

| Python | Go | 说明 |
|--------|-----|------|
| `doc_index_callback: type[BaseCallback]`（传类型/工厂） | `docIndexCallback DocIndexCallback`（传实例） | Go 没有"传类型"惯用法，改传接口实例 |
| `BaseCallback(seq)` 构造时传序列长度 | `OnBatchComplete` 不需要 seq | Python 需要序列长度初始化进度条，Go callback 不负责初始化 |
| `TqdmCallback` 进度条 | `LoggingDocIndexCallback` 日志 | Go CLI 无 Tqdm，用日志替代 |

### 变更影响

| 文件 | 变更 |
|------|------|
| `retrieval/common/callbacks.go` | **新建**：`DocIndexCallback` 别名 + `NoOpDocIndexCallback` + `LoggingDocIndexCallback` |

**foundation 层 `embedding.Callback`、`EmbedOptions.Callback`、`WithCallback`、`BaseEmbedding` 接口均不变。**

---

## 13.2-3：回填 ComputeChunkEmbeddings 签名

### 当前签名

```go
func ComputeChunkEmbeddings(
    ctx context.Context,
    chunks []common.TextChunk,
    embedModel storeEmbedding.BaseEmbedding,
    useCaptionForImages bool,
    opts ...storeEmbedding.EmbedOption,
) error
```

### 回填后签名

```go
// ChunkEmbedOptions ComputeChunkEmbeddings 的选项结构。
type ChunkEmbedOptions struct {
    // UseCaptionForImages 为 true 时图片分块仅用文本/标题嵌入
    UseCaptionForImages bool
    // DocIndexCallback 嵌入进度回调，对齐 Python doc_index_callback
    // nil 表示不回调（对齐 Python doc_index_callback is None）
    DocIndexCallback common.DocIndexCallback
    // EmbedOpts 透传给 EmbedDocuments 的额外选项
    EmbedOpts []storeEmbedding.EmbedOption
}

// ChunkEmbedOption ComputeChunkEmbeddings 的可选参数函数。
type ChunkEmbedOption func(*ChunkEmbedOptions)

// 导出函数
func WithUseCaptionForImages(v bool) ChunkEmbedOption
func WithDocIndexCallback(cb common.DocIndexCallback) ChunkEmbedOption
func WithEmbedOpts(opts ...storeEmbedding.EmbedOption) ChunkEmbedOption

func ComputeChunkEmbeddings(
    ctx context.Context,
    chunks []common.TextChunk,
    embedModel storeEmbedding.BaseEmbedding,
    opts ...ChunkEmbedOption,
) error
```

### 内部改动

1. `useCaptionForImages bool` → 从 `ChunkEmbedOptions.UseCaptionForImages` 读取
2. `EmbedDocuments(ctx, texts, opts...)` → `EmbedDocuments(ctx, texts, append(embedOpts, storeEmbedding.WithCallback(docIndexCallback))...)`
3. 已有调用点（11 个测试，0 个生产代码）适配：
   - `ComputeChunkEmbeddings(ctx, chunks, fake, false)` → `ComputeChunkEmbeddings(ctx, chunks, fake, WithUseCaptionForImages(false))`

### Python 对齐

```python
# Python compute_chunk_embeddings 签名
async def compute_chunk_embeddings(
    chunks, embed_model, *,
    doc_index_callback=None,
    use_caption_for_images=False,
) -> None:
```

Go 的 `ChunkEmbedOptions` 完整对齐 Python 的 `doc_index_callback` + `use_caption_for_images` 参数。

---

## 13.2-4：Retrieval 层 ChromaVectorStore

### 新建类型

#### SearchResult / RetrievalResult

对齐 Python `retrieval/common/retrieval_result.py`，放在 `retrieval/common/result.go`。

```go
// SearchResult 搜索结果数据模型。
// Python: retrieval/common/retrieval_result.py (SearchResult)
type SearchResult struct {
    ID       string         `json:"id"`
    Text     string         `json:"text"`
    Score    float64        `json:"score"`
    Metadata map[string]any `json:"metadata"`
}

// RetrievalResult 检索结果数据模型。
// Python: retrieval/common/retrieval_result.py (RetrievalResult)
type RetrievalResult struct {
    Text     string         `json:"text"`
    Score    float64        `json:"score"`
    Metadata map[string]any `json:"metadata"`
    DocID    string         `json:"doc_id,omitempty"`
    ChunkID  string         `json:"chunk_id,omitempty"`
}
```

#### RRFFusion

对齐 Python `retrieval/utils/fusion.py`，放在 `retrieval/utils/fusion.go`。

```go
// RRFFusion 倒数秩融合（Reciprocal Rank Fusion）。
// 将多路检索结果融合为单一排序结果。
// Python: retrieval/utils/fusion.py (rrf_fusion)
func RRFFusion(resultsList [][]RetrievalResult, k int) []RetrievalResult
```

Python 实现要点（Go 对齐）：
- 用 `text` 作为唯一键做去重
- RRF 分数 = Σ 1/(k + rank)
- 保留首次出现的 metadata
- 按分数降序排列

#### VectorStore 接口

对齐 Python `retrieval/vector_store/base.py`，放在 `retrieval/vector_store/base.go`。

```go
// VectorStore 检索层向量存储抽象接口。
//
// 提供向量搜索、稀疏搜索、混合搜索等检索能力。
// 与 foundation 层 BaseVectorStore（CRUD+迁移）不同，本接口面向检索场景。
//
// Python: retrieval/vector_store/base.py (VectorStore)
type VectorStore interface {
    // CreateClient 创建向量数据库客户端，对齐 Python create_client
    CreateClient(databaseName, pathOrURI string, token string, opts ...StoreOption) (any, error)

    // CheckVectorField 校验向量字段配置是否和实际数据库一致
    // 对齐 Python check_vector_field / _check_configs_matching
    CheckVectorField() error

    // Add 添加向量数据，对齐 Python add
    Add(ctx context.Context, data []map[string]any, opts ...StoreOption) error

    // Search 向量搜索，对齐 Python search
    Search(ctx context.Context, queryVector []float64, topK int, filters any, opts ...StoreOption) ([]common.SearchResult, error)

    // SparseSearch 稀疏搜索（文本匹配），对齐 Python sparse_search
    SparseSearch(ctx context.Context, queryText string, topK int, filters any, opts ...StoreOption) ([]common.SearchResult, error)

    // HybridSearch 混合搜索（向量+文本 RRF 融合），对齐 Python hybrid_search
    HybridSearch(ctx context.Context, queryText string, queryVector []float64, topK int, alpha float64, filters any, opts ...StoreOption) ([]common.SearchResult, error)

    // Delete 删除向量，对齐 Python delete
    Delete(ctx context.Context, ids []string, filterExpr any) (bool, error)

    // TableExists 检查集合是否存在，对齐 Python table_exists
    TableExists(ctx context.Context, tableName string) (bool, error)

    // DeleteTable 删除集合，对齐 Python delete_table
    DeleteTable(ctx context.Context, tableName string) error

    // Close 关闭存储
    Close()
}
```

`filters` 参数类型为 `any`，对齐 Python 的 `dict | QueryExpr`。Go 运行时判断是 `map[string]any` 还是 `query.QueryExpr`。

#### StoreOption

```go
// StoreOption 向量存储操作的函数选项。
type StoreOption func(*StoreOptions)

// StoreOptions 向量存储操作选项。
type StoreOptions struct {
    // BatchSize 批大小，0 表示使用默认值
    BatchSize int
}
```

### ChromaVectorStore（retrieval 层）

对齐 Python `retrieval/vector_store/chroma_store.py`，放在 `retrieval/vector_store/chroma.go`。

#### 构造函数

```go
// NewChromaVectorStore 创建 retrieval 层 ChromaVectorStore。
// 对齐 Python ChromaVectorStore.__init__。
//
// vectorField 参数支持 string（字段名）或 ChromaVectorField（完整配置），
// 对齐 Python vector_field: str | ChromaVectorField。
func NewChromaVectorStore(
    config common.VectorStoreConfig,
    chromaPath string,
    textField string,            // 默认 "content"
    vectorField any,             // string 或 *ChromaVectorField
    sparseVectorField string,    // 默认 "sparse_vector"
    metadataField string,        // 默认 "metadata"
    docIDField string,           // 默认 "document_id"
    opts ...StoreOption,
) (*ChromaVectorStore, error)
```

构造函数逻辑对齐 Python：
1. 校验 `chromaPath` 非空
2. `vectorField` 如果是 string 则包装为 `NewChromaVectorFieldFromName(name)`
3. 归一化距离度量：`dot→ip`，`euclidean→l2`
4. 构建 `constructConfig` = `vectorField.ToConstructDict()` + `{"space": distanceMetric}`
5. 构建 `searchConfig` = `vectorField.ToSearchDict()`
6. 创建 ChromaDB 客户端 `CreateClient(databaseName, chromaPath)`
7. `GetOrCreateCollection(name=collectionName, configuration={hnsw: constructConfig | searchConfig})`

#### Add 方法

对齐 Python `add` + `_add_batch`：

```go
func (s *ChromaVectorStore) Add(ctx context.Context, data []map[string]any, opts ...StoreOption) error
```

逻辑：
1. 按 `batchSize`（默认 128）分批
2. 每批从 data 提取 ids/embeddings/documents/metadatas（按字段名映射）
3. embedding 为空时 warn 但跳过该条（对齐 Python `logger.warning("Node has no embedding, skipping")`）
4. id 缺失时生成 uuid（对齐 Python `uuid.uuid4()`）
5. metadata 中添加 `docIDField`、`chunk_id`、`sparseVectorField`
6. 每 100 批打 Info 日志（对齐 Python `if processed % 100 == 0: logger.info(...)`）

#### Search 方法

对齐 Python `search`：
1. 调 ChromaDB `collection.Query(query_embeddings, n_results, where=...)`
2. `_chromaResultToSearchResults(results, mode="vector")` 转换结果

#### SparseSearch 方法

对齐 Python `sparse_search`：
1. 调 ChromaDB `collection.Query(query_texts, n_results, where=...)`
2. ChromaDB 的文本搜索基于 TF-IDF
3. 失败时 warn 并返回空列表（对齐 Python `except: return []`）

#### HybridSearch 方法

对齐 Python `hybrid_search`：
1. 并发执行 `Search(queryVector, topK*2)` 和 `SparseSearch(queryText, topK*2)`
2. 将 `[]SearchResult` 转为 `[]RetrievalResult`（保存 id 映射）
3. 调 `RRFFusion(resultsList, k=60)` 融合
4. 融合结果转回 `[]SearchResult`（恢复 id）
5. 取前 `topK` 条返回

#### Delete 方法

对齐 Python `delete`：
1. 按 IDs 删除 或 按 QueryExpr 过滤删除
2. string 过滤表达式不支持（对齐 Python `logger.warning("ChromaDB does not support string filter expressions")`）

#### 结果转换

对齐 Python `_chroma_result_to_search_results`：

```go
// chromaResultToSearchResults 将 ChromaDB 搜索结果转为 SearchResult 列表。
// 对齐 Python ChromaVectorStore._chroma_result_to_search_results。
func chromaResultToSearchResults(results, distanceMetric, mode string, docIDField, sparseVectorField string) []common.SearchResult
```

距离→分数转换（对齐 Python）：

| distanceMetric | 转换函数 | Python |
|---------------|---------|--------|
| `cosine` | `ConvertCosineDistance` | `convert_cosine_distance` |
| `l2` | `ConvertL2Squared` | `convert_l2_squared` |
| `ip` | `ConvertIPDistance` | `convert_ip_distance` |

Sparse 模式下：有距离时按值判断（≤1.0 取反，>1.0 直接用），无距离时默认 0.5。

### 文件结构

```
internal/agentcore/retrieval/
├── common/
│   ├── callbacks.go          # DocIndexCallback + NoOp + Logging（13.2-2）
│   ├── result.go             # SearchResult + RetrievalResult（新建）
│   └── result_test.go
│
├── utils/
│   ├── fusion.go             # RRFFusion（新建）
│   └── fusion_test.go
│
├── vector_store/             # 新包
│   ├── doc.go                # 包文档
│   ├── base.go               # VectorStore 接口 + CheckConfigsMatching + StoreOption
│   ├── chroma.go             # ChromaVectorStore 实现
│   ├── base_test.go
│   └── chroma_test.go
```

---

## 13.2-5：ChromaIndexer

### 文件位置

```
internal/agentcore/retrieval/indexing/indexer/
├── chroma.go         # ChromaIndexer 实现（新建）
└── chroma_test.go    # 测试（新建）
```

### ChromaIndexer struct

```go
// ChromaIndexer ChromaDB 文档索引管理器。
//
// 实现Indexer接口，负责将TextChunk写入ChromaDB向量库，
// 支持五种操作：BuildIndex（新建）、UpdateIndex（更新）、
// DeleteIndex（删除）、IndexExists（存在性检查）、GetIndexInfo（元信息查询）。
//
// Python: retrieval/indexing/indexer/chroma_indexer.py (ChromaIndexer)
type ChromaIndexer struct {
    // chromaPath ChromaDB 持久化路径
    chromaPath string
    // textField 文本字段名，默认 "content"
    textField string
    // vectorField 向量字段配置
    vectorField vector_fields.ChromaVectorField
    // sparseVectorField 稀疏向量字段名，默认 "sparse_vector"
    sparseVectorField string
    // metadataField 元数据字段名，默认 "metadata"
    metadataField string
    // docIDField 文档ID字段名，默认 "document_id"
    docIDField string
    // distanceMetric 归一化距离度量：cosine/l2/ip
    distanceMetric string
    // constructConfig 构建阶段配置，对齐 Python _construct_config
    constructConfig map[string]any
    // searchConfig 搜索阶段配置，对齐 Python _search_config
    searchConfig map[string]any
    // docIndexCallback 嵌入进度回调
    docIndexCallback common.DocIndexCallback
    // client ChromaDB 客户端，对齐 Python self._client
    client chromav2.Client
}
```

### 构造函数

```go
// ChromaIndexerOption ChromaIndexer 的可选参数函数。
type ChromaIndexerOption func(*ChromaIndexer)

// NewChromaIndexer 创建 ChromaIndexer 实例。
// 对齐 Python ChromaIndexer.__init__。
func NewChromaIndexer(
    config common.VectorStoreConfig,
    chromaPath string,
    opts ...ChromaIndexerOption,
) (*ChromaIndexer, error)
```

构造逻辑对齐 Python：
1. 校验 `chromaPath` 非空 → 否则抛 `RETRIEVAL_INDEXING_PATH_NOT_FOUND`
2. 校验 `vectorField` 类型（str 或 ChromaVectorField）→ 否则抛 `RETRIEVAL_INDEXING_VECTOR_FIELD_INVALID`
3. 归一化距离度量：`dot→ip`，`euclidean→l2`
4. `constructConfig` = `vectorField.ToConstructDict()` + `{"space": distanceMetric}`
5. `searchConfig` = `vectorField.ToSearchDict()`
6. 校验 `docIndexCallback` 实现 `DocIndexCallback` 接口
7. 创建 ChromaDB 客户端 `CreateClient(databaseName, chromaPath)`

### BuildIndex

4 步流程，对齐 Python `build_index`：

**步骤 1：查重 doc_id**

```go
// 对齐 Python: all_doc_ids = sorted({chunk.doc_id for chunk in chunks})
// 对齐 Python: for doc_id in all_doc_ids:
//   if doc_id not in filter_values and collection.get(where={self.doc_id_field: doc_id}).get("ids"):
//     duplicate_doc_ids.append(doc_id)
// 对齐 Python: if duplicate_doc_ids: raise build_error(...)
```

使用 retrieval 层 `ChromaVectorStore` 获取 collection，按 `docIDField` 查询去重。

**步骤 2：嵌入**

```go
// 对齐 Python: if config.index_type in ("vector", "hybrid"):
//   if not embed_model: raise build_error(RETRIEVAL_INDEXING_EMBED_MODEL_NOT_FOUND)
//   await compute_chunk_embeddings(chunks, embed_model, ...)
if config.IndexType == common.IndexTypeVector || config.IndexType == common.IndexTypeHybrid {
    if embedModel == nil {
        return false, exception.BuildError(exception.StatusRetrievalIndexingEmbedModelNotFound, ...)
    }
    if err := ComputeChunkEmbeddings(ctx, chunks, embedModel,
        WithUseCaptionForImages(config.UseCaptionForImages),
        WithDocIndexCallback(ci.docIndexCallback),
    ); err != nil {
        return false, err
    }
}
```

**步骤 3：转换 TextChunk → ChromaDB 字段**

```go
// 对齐 Python:
// data = []
// for chunk in chunks:
//     item = {"id": chunk.id_, docIDField: chunk.doc_id, textField: chunk.text, metadataField: meta}
//     if chunk.embedding is not None: item[vectorField.vector_field] = chunk.embedding
//     data.append(item)
```

**步骤 4：写入**

```go
// 对齐 Python: await vector_store.add(data=data)
// 注意：Python 中 BuildIndex 为每次调用新建 ChromaVectorStore
vectorStore, err := NewChromaVectorStore(vectorStoreConfig, ci.chromaPath, ...)
if err != nil { return false, err }
if err := vectorStore.Add(ctx, data); err != nil { return false, err }
```

成功后打 Info 日志（对齐 Python `logger.info(f"Successfully built index {collection_name} with {len(chunks)} chunks")`）。

### UpdateIndex

对齐 Python `update_index`，先删后建：

```go
func (ci *ChromaIndexer) UpdateIndex(ctx, chunks, docID, config, embedModel, opts) (bool, error) {
    // 对齐 Python: await self.delete_index(doc_id, config.index_name)
    _, err := ci.DeleteIndex(ctx, docID, config.IndexName)
    if err != nil { return false, err }
    // 对齐 Python: return await self.build_index(chunks, config, embed_model, **kwargs)
    return ci.BuildIndex(ctx, chunks, config, embedModel, opts...)
}
```

### DeleteIndex

对齐 Python `delete_index`，ChromaDB 不支持复杂过滤删除，需先查后删：

```go
func (ci *ChromaIndexer) DeleteIndex(ctx, docID, indexName) (bool, error) {
    // 1. 获取 collection
    //    对齐 Python: collection = self._client.get_collection(name=index_name)
    collection, err := ci.client.GetCollection(ctx, indexName)
    if err != nil { return false, err }

    // 2. 查询匹配 docID 的所有记录
    //    对齐 Python: results = collection.get(where={self.doc_id_field: doc_id})
    results, err := collection.Get(ctx, chromav2.WithWhere(buildWhereFilter(ci.docIDField, docID)))

    // 3. 无匹配 → 返回 false，打 Info 日志
    //    对齐 Python: if not results or not results.get("ids") or len(results["ids"]) == 0:
    //                  logger.info(f"No entries found for doc_id={doc_id}")
    //                  return False

    // 4. 按 IDs 删除
    //    对齐 Python: ids_to_delete = results["ids"]
    //    对齐 Python: collection.delete(ids=ids_to_delete)
    collection.Delete(ctx, chromav2.WithIDs(idsToDelete...))

    // 5. 打 Info 日志
    //    对齐 Python: logger.info(f"Deleted {delete_count} entries for doc_id={doc_id}")
    //    return delete_count > 0
}
```

### IndexExists

```go
func (ci *ChromaIndexer) IndexExists(ctx, indexName) (bool, error) {
    // 对齐 Python: try: self._client.get_collection(name=index_name); return True
    //              except: return False
    _, err := ci.client.GetCollection(ctx, indexName)
    if err != nil {
        return false, nil
    }
    return true, nil
}
```

### GetIndexInfo

```go
func (ci *ChromaIndexer) GetIndexInfo(ctx, indexName) (map[string]any, error) {
    // 1. 检查存在
    exists, _ := ci.IndexExists(ctx, indexName)
    if !exists {
        return map[string]any{"exists": false}, nil
    }

    // 2. 获取 collection
    collection, _ := ci.client.GetCollection(ctx, indexName)

    // 3. 统计数量
    //    对齐 Python: count = collection.count()
    count, _ := collection.Count(ctx)

    // 4. 获取元数据
    //    对齐 Python: metadata = collection.metadata or {}
    metadata := collection.Metadata()

    return map[string]any{
        "exists":          true,
        "collection_name": indexName,
        "count":           count,
        "metadata":        metadata,
    }, nil
}
```

### Close

```go
func (ci *ChromaIndexer) Close() {
    // 对齐 Python: if self._client is not None: pass
    // ChromaDB 客户端不需要显式关闭
}
```

### 属性方法

```go
func (ci *ChromaIndexer) Client() chromav2.Client { return ci.client }
func (ci *ChromaVectorField) DistanceMetric() string { return ci.distanceMetric }
```

### 异常路径日志

对齐 Python 日志同步规则（项目规则 3）：

| 方法 | Python 日志 | Go 日志 |
|------|------------|---------|
| BuildIndex 成功 | `logger.info(f"Successfully built index {collection_name} with {len(chunks)} chunks")` | `logger.Info(logComponent).Str("index_name", ...).Int("chunk_count", ...).Msg("成功构建索引")` |
| BuildIndex 异常 | `logger.error(f"Failed to build index: {e}")` | `logger.Error(logComponent).Str("event_type", "INDEXING_ERROR").Err(err).Msg("构建索引失败")` |
| UpdateIndex 异常 | `logger.error(f"Failed to update index: {e}")` | `logger.Error(logComponent).Str("event_type", "INDEXING_ERROR").Err(err).Msg("更新索引失败")` |
| DeleteIndex 无匹配 | `logger.info(f"No entries found for doc_id={doc_id}")` | `logger.Info(logComponent).Str("doc_id", ...).Msg("未找到匹配的文档条目")` |
| DeleteIndex 成功 | `logger.info(f"Deleted {delete_count} entries for doc_id={doc_id}")` | `logger.Info(logComponent).Str("doc_id", ...).Int("delete_count", ...).Msg("成功删除文档条目")` |
| DeleteIndex 异常 | `logger.error(f"Failed to delete index entries: {e}")` | `logger.Error(logComponent).Str("event_type", "INDEXING_ERROR").Err(err).Msg("删除索引条目失败")` |
| GetIndexInfo 异常 | `logger.error(f"Failed to get index info: {e}")` | `logger.Error(logComponent).Str("event_type", "INDEXING_ERROR").Err(err).Msg("获取索引信息失败")` |

---

## 回填点汇总

| 回填点 | 来源 | 目标 | 说明 |
|--------|------|------|------|
| `ChromaVectorField` | 13.2-1 | 13.3 MilvusIndexer | MilvusVectorField 同理需新建 |
| `DocIndexCallback` | 13.2-2 | 13.3 MilvusIndexer | MilvusIndexer 构造也需 callback |
| `ComputeChunkEmbeddings` 签名 | 13.2-3 | 已有代码 | 改 option 模式，已有调用点适配 |
| `SearchResult` / `RetrievalResult` | 13.2-4 | 后续 Retriever | 检索章节使用 |
| `RRFFusion` | 13.2-4 | MilvusVectorStore | HybridSearch 复用 |
| `VectorStore` 接口 | 13.2-4 | 13.3 MilvusIndexer | MilvusVectorStore 实现此接口 |
| `ChromaVectorStore`（retrieval 层）| 13.2-4 | 后续章节 | Retriever 使用 hybrid_search |

---

## 变更影响总览

### 新建文件

| 文件 | 说明 |
|------|------|
| `foundation/store/vector_fields/doc.go` | 包文档 |
| `foundation/store/vector_fields/base.go` | VectorField 基类 + ToDict + ShouldKeep |
| `foundation/store/vector_fields/chroma.go` | ChromaVectorField |
| `foundation/store/vector_fields/base_test.go` | 测试 |
| `foundation/store/vector_fields/chroma_test.go` | 测试 |
| `retrieval/common/callbacks.go` | DocIndexCallback 别名 + NoOp + Logging |
| `retrieval/common/callbacks_test.go` | 测试 |
| `retrieval/common/result.go` | SearchResult + RetrievalResult |
| `retrieval/common/result_test.go` | 测试 |
| `retrieval/utils/fusion.go` | RRFFusion |
| `retrieval/utils/fusion_test.go` | 测试 |
| `retrieval/vector_store/doc.go` | 包文档 |
| `retrieval/vector_store/base.go` | VectorStore 接口 + StoreOption + CheckConfigsMatching |
| `retrieval/vector_store/chroma.go` | ChromaVectorStore |
| `retrieval/vector_store/base_test.go` | 测试 |
| `retrieval/vector_store/chroma_test.go` | 测试 |
| `retrieval/indexing/indexer/chroma.go` | ChromaIndexer |
| `retrieval/indexing/indexer/chroma_test.go` | 测试 |

### 修改文件

| 文件 | 变更 |
|------|------|
| `retrieval/indexing/indexer/embed_chunks.go` | 签名改为 option 模式，加 DocIndexCallback |
| `retrieval/indexing/indexer/embed_chunks_test.go` | 适配新签名 |
| `retrieval/common/doc.go` | 更新文件目录 |
| `retrieval/utils/doc.go` | 更新文件目录 |
| `retrieval/indexing/indexer/doc.go` | 更新文件目录 |
| `foundation/store/vector/doc.go` | 更新文件目录（添加 vector_fields 引用） |
