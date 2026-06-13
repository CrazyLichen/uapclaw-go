# 4.26 Graph Store 实现设计

## 概述

实现领域4第4.26小节：图存储（Graph Store）。图存储用于知识图谱的实体/关系/片段管理，基于 Milvus 向量数据库实现混合语义搜索（dense embedding + BM25 sparse）+ BFS 图扩展 + 可选 reranking。

**Python 参考**：`openjiuwen/core/foundation/store/graph/`

## 关键决策

| 决策项 | 选择 | 理由 |
|--------|------|------|
| 实现范围 | 一次性完整对标 Python | 与 Python 行为一致，避免后续补丁式开发 |
| 后端策略 | 仅 Milvus 后端 | 与 Python 一致；图存储核心是语义检索，Milvus 同时提供向量索引和 BM25 |
| 搜索功能 | 一次性完整实现（混合搜索 + BFS + reranking） | 搜索是图存储的核心价值，分期交付意义不大 |
| 文件组织 | 子目录结构，milvus/ 单独子包 | 与 Python 的 graph/milvus/ 对应，后端实现隔离清晰 |
| 图对象模型 | 独立 graph_object.go 文件 | 三个图对象字段多、有序列化方法，独立文件更易维护 |
| 接口风格 | 方案 C：单一 BaseGraphStore 接口 + 内部 Embed 拆分 | 对外与项目风格一致（vector/kv/db 均单一接口），内部通过 graphWriter + graphSearcher 拆分职责 |
| QueryExpr | 最小化接口定义，4.28 完善后替换 | 4.28 Query Builder 尚未实现，先定义接口让 Graph Store 可编译 |
| Relations/LHS/RHS 类型 | 纯 `[]string`/`string`（仅 UUID） | Python 混合对象+UUID 是动态类型便利，Go 强类型下存入 Milvus 时都序列化为 UUID 字符串，无需存对象 |
| ContentBM25 类型 | `map[uint32]float32` | Milvus 稀疏向量用 {dim_id: value} 表示，Go 用 map 更自然 |
| 时区偏移类型 | `int8` | 偏移范围 -96~+96（15分钟粒度），int8 足够 |
| BM25 稀疏向量 | 使用新 SDK 的 BM25 Function（与 Python 一致） | 统一迁移到新 Milvus Go SDK `github.com/milvus-io/milvus/client/v2`，该 SDK 支持 `entity.FunctionTypeBM25`，可自动从文本生成 BM25 稀疏向量，与 Python 行为完全一致 |
| Milvus SDK | 统一迁移到 `github.com/milvus-io/milvus/client/v2` | 旧 SDK `milvus-sdk-go/v2@v2.4.2` 已归档且不支持 BM25 Function；新 SDK 活跃维护、支持 BM25、API 更现代；Graph Store + Vector Store 一并迁移，避免两个 SDK 共存的依赖冲突 |

## 文件结构与产出清单

### 新增文件

| 文件 | 职责 | 对应 Python |
|------|------|-------------|
| `graph/doc.go` | 包文档 | `graph/__init__.py` |
| `graph/base.go` | BaseGraphStore 接口 + Option + 常量 + 工厂 + QueryExpr 最小定义 | `graph/base_graph_store.py` + `graph/base.py` + `graph/constants.py` |
| `graph/graph_object.go` | BaseGraphObject / NamedGraphObject / Entity / Relation / Episode / EmbedTask | `graph/graph_object.py` |
| `graph/config.go` | GraphConfig / GraphStoreStorageConfig / GraphStoreIndexConfig / BM25Config | `graph/config.py` + `graph/database_config.py` |
| `graph/ranking.go` | BaseRankConfig / WeightedRankConfig / RRFRankConfig / RankerRegistry | `graph/result_ranking.py` |
| `graph/utils.go` | UUID生成 / 时间戳转换 / 批处理 / 格式化 | `graph/utils.py` |
| `graph/base_test.go` | 工厂注册/创建测试 | — |
| `graph/graph_object_test.go` | 图对象创建、序列化、嵌入任务测试 | — |
| `graph/config_test.go` | 配置默认值、校验测试 | — |
| `graph/ranking_test.go` | 排序策略测试 | — |
| `graph/utils_test.go` | 工具函数测试 | — |
| `milvus/doc.go` | Milvus 后端子包文档 | `graph/milvus/__init__.py` |
| `milvus/milvus.go` | MilvusGraphStore 主结构体 + 接口实现委托 + lazy init | `graph/milvus/milvus_support.py`（构造 + 生命周期部分） |
| `milvus/milvus_writer.go` | graphWriter（addEntity/addRelation/addEpisode/delete） | `graph/milvus/milvus_support.py`（_add_data 部分） |
| `milvus/milvus_searcher.go` | graphSearcher（hybrid_search/BFS/rerank） | `graph/milvus/milvus_support.py`（搜索部分） |
| `milvus/schema.go` | generateSchemaAndIndex（三集合 schema + BM25 Function + 索引构建） | `graph/milvus/generate_milvus_schema.py` |
| `milvus/milvus_test.go` | 主结构体 + 生命周期测试 | — |
| `milvus/milvus_writer_test.go` | 写入逻辑测试 | — |
| `milvus/milvus_searcher_test.go` | 搜索逻辑测试 | — |
| `milvus/schema_test.go` | Schema 构建测试 | — |
| `milvus/milvus_integration_test.go` | 集成测试（build tag: integration） | — |

### 修改文件（SDK 迁移）

| 文件 | 变更 |
|------|------|
| `go.mod` | 替换 `milvus-sdk-go/v2@v2.4.2` → `milvus-io/milvus/client/v2` |
| `vector/milvus.go` | 重写 milvusClient 接口、SDK 类型映射、构造函数（新 SDK Builder API） |
| `vector/milvus_test.go` | 更新所有 fake client 接口实现 |

### 更新文件

| 文件 | 变更 |
|------|------|
| `IMPLEMENTATION_PLAN.md` | 步骤 4.26 状态 ☐ → ✅ |

## 详细设计

### 1. 图对象模型（graph_object.go）

#### 继承体系（Go 组合替代 Python 继承）

```
BaseGraphObject
├── NamedGraphObject（嵌入 BaseGraphObject + Name + NameEmbedding）
│   ├── Entity（嵌入 NamedGraphObject + Relations + Episodes + Attributes）
│   └── Relation（嵌入 NamedGraphObject + LHS/RHS + ValidSince/Until + OffsetSince/Until）
└── Episode（嵌入 BaseGraphObject + ValidSince + Entities）
```

#### BaseGraphObject

```go
type BaseGraphObject struct {
    UUID            string         `json:"uuid"`
    CreatedAt       int64          `json:"created_at"`
    UserID          string         `json:"user_id"`
    ObjType         string         `json:"obj_type"`
    Language        string         `json:"language"`
    Metadata        map[string]any `json:"metadata,omitempty"`
    Content         string         `json:"content"`
    ContentEmbedding []float64     `json:"content_embedding,omitempty"`
    ContentBM25     map[uint32]float32 `json:"content_bm25,omitempty"`
}
```

方法：
- `EmbedTasks() []EmbedTask`：返回 `[{self, "content_embedding", Content}]`
- `ToMap() map[string]any`：序列化为 Milvus 插入格式

#### NamedGraphObject

```go
type NamedGraphObject struct {
    BaseGraphObject
    Name           string   `json:"name"`
    NameEmbedding  []float64 `json:"name_embedding,omitempty"`
}
```

方法：
- `EmbedTasks() []EmbedTask`：返回 content_embedding + name_embedding 两个任务

#### Entity

```go
type Entity struct {
    NamedGraphObject
    Relations  []string      `json:"relations"`
    Episodes   []string      `json:"episodes"`
    Attributes map[string]any `json:"attributes,omitempty"`
}
```

方法：
- `EmbedTasks() []EmbedTask`：覆写，返回 content + name 两个嵌入任务
- `ToMap() map[string]any`：Relations/Episodes 去重排序后写入

#### Relation

```go
type Relation struct {
    NamedGraphObject
    ValidSince  int64 `json:"valid_since"`
    ValidUntil  int64 `json:"valid_until"`
    OffsetSince int8  `json:"offset_since"`
    OffsetUntil int8  `json:"offset_until"`
    LHS         string `json:"lhs"`
    RHS         string `json:"rhs"`
}
```

方法：
- `UpdateConnectedEntities()`：将自身 UUID 添加到 lhs/rhs 实体的 Relations 中（如果尚未存在）——此方法用于上层 GraphMemory 调用，不在 store 层内部使用
- `ToMap() map[string]any`：LHS/RHS 写为 UUID 字符串

#### Episode

```go
type Episode struct {
    BaseGraphObject
    ValidSince int64    `json:"valid_since"`
    Entities  []string `json:"entities"`
}
```

方法：
- `ToMap() map[string]any`：Entities 去重排序后写入

#### EmbedTask

```go
type EmbedTask struct {
    Object    any    // 指向图对象的指针
    FieldName string // "content_embedding" 或 "name_embedding"
    Text      string // 待嵌入文本
}
```

### 2. 配置体系（config.go）

#### GraphConfig

```go
type GraphConfig struct {
    URI               string                    `json:"uri"`
    Name              string                    `json:"name"`
    Token             string                    `json:"token"`
    Backend           string                    `json:"backend"`
    Timeout           float64                   `json:"timeout"`
    Extras            map[string]any            `json:"extras,omitempty"`
    MaxConcurrent     int                       `json:"max_concurrent"`
    EmbedDim          int                       `json:"embed_dim"`
    EmbedBatchSize    int                       `json:"embed_batch_size"`
    StorageConfig     *GraphStoreStorageConfig  `json:"db_storage_config"`
    IndexConfig       *GraphStoreIndexConfig    `json:"db_embed_config"`
    RequestMaxRetries int                       `json:"request_max_retries"`
}
```

默认值：Backend="milvus", Timeout=15.0, MaxConcurrent=10, EmbedDim=512, EmbedBatchSize=10, RequestMaxRetries=5

构造函数：`NewGraphConfig(uri string) *GraphConfig` — 带全部默认值
校验：`Validate() error` — URI 非空、Timeout>0、EmbedDim≥32、MaxConcurrent≥0、EmbedBatchSize≥1

#### GraphStoreStorageConfig

```go
type GraphStoreStorageConfig struct {
    UUID      int `json:"uuid"`       // 默认 32
    Name      int `json:"name"`       // 默认 500
    Content   int `json:"content"`    // 默认 65535
    Language  int `json:"language"`   // 默认 10
    UserID    int `json:"user_id"`    // 默认 32
    Entities  int `json:"entities"`   // 默认 4096
    Relations int `json:"relations"`  // 默认 4096
    Episodes  int `json:"episodes"`   // 默认 4096
    ObjType   int `json:"obj_type"`   // 默认 20
}
```

#### GraphStoreIndexConfig

```go
type GraphStoreIndexConfig struct {
    IndexType            vector_fields.VectorField `json:"index_type"`
    DistanceMetric       string                    `json:"distance_metric"`  // cosine/euclidean/dot
    ExtraConfigs         map[string]any            `json:"extra_configs,omitempty"`
    BM25Config           *BM25Config               `json:"bm25_config"`
    BM25AnalyzerSettings map[string]any            `json:"bm25_analyzer_settings,omitempty"`
}
```

距离度量映射：cosine→COSINE, euclidean→L2, dot→IP

#### BM25Config

```go
type BM25Config struct {
    B  float64 `json:"b"`   // 默认 0.75，范围 0~1
    K1 float64 `json:"k1"`  // 默认 1.2，≥0
}
```

### 3. 排序策略（ranking.go）

#### BaseRankConfig 接口

```go
type BaseRankConfig interface {
    Name() string
    HigherIsBetter() bool
    IsActive() [3]int           // [name_dense, content_dense, content_sparse]
    Args() ([]any, map[string]any)
}
```

#### WeightedRankConfig

```go
type WeightedRankConfig struct {
    NameDense      float64 `json:"name_dense"`       // 默认 0.15
    ContentDense   float64 `json:"content_dense"`    // 默认 0.60
    ContentSparse  float64 `json:"content_sparse"`   // 默认 0.25
}
```

- `HigherIsBetter() bool` → false
- `IsActive()` → 各权重 >0 则为1，否则为0
- `Args()` → 归一化权重：过滤零值后除以总和

#### RRFRankConfig

```go
type RRFRankConfig struct {
    K              int  `json:"k"`               // 默认 40
    NameDense      bool `json:"name_dense"`      // 默认 true
    ContentDense   bool `json:"content_dense"`   // 默认 true
    ContentSparse  bool `json:"content_sparse"`  // 默认 true
}
```

- `HigherIsBetter() bool` → true
- `IsActive()` → 各 bool 转 int
- `Args()` → `([K], {})`

#### RankerRegistry

```go
type RankerRegistry struct {
    mu       sync.RWMutex
    backends map[string]map[string]any
}
```

方法：`Register(backend, rankers)`, `GetRanker(backend, strategy) (any, bool)`

全局实例：`GlobalRankerRegistry *RankerRegistry`

Milvus `init()` 中注册 WeightedRanker 和 RRFRanker 构造函数。

### 4. BaseGraphStore 接口（base.go）

```go
type BaseGraphStore interface {
    // 配置与生命周期
    Config() *GraphConfig
    Rebuild(ctx context.Context) error
    Refresh(ctx context.Context, opts ...Option) error
    Close() error

    // 数据写入
    AddEntity(ctx context.Context, entities []*Entity, opts ...Option) error
    AddRelation(ctx context.Context, relations []*Relation, opts ...Option) error
    AddEpisode(ctx context.Context, episodes []*Episode, opts ...Option) error

    // 查询与删除
    Query(ctx context.Context, collection string, opts ...Option) ([]map[string]any, error)
    Delete(ctx context.Context, collection string, opts ...Option) error
    IsEmpty(ctx context.Context, collection string) (bool, error)

    // 搜索
    Search(ctx context.Context, query string, opts ...Option) (map[string][]map[string]any, error)

    // 嵌入管理
    AttachEmbedder(embedder embedding.BaseEmbedding)
}
```

#### Option 函数式选项

```go
type Options struct {
    // 写入选项
    Flush    bool
    Upsert   bool
    NoEmbed  bool

    // 查询选项
    IDs           []any
    Expr          QueryExpr
    SilenceErrors bool

    // 搜索选项
    Collection     string
    K              int
    RankerConfig   BaseRankConfig
    Reranker       reranker.BaseReranker
    BFSDepth       int
    BFSK           int
    FilterExpr     QueryExpr
    OutputFields   []string
    QueryEmbedding []float64
    Language       string
    MinScore       float64
}

type Option func(*Options)
```

选项构造函数：`WithFlush`, `WithUpsert`, `WithNoEmbed`, `WithIDs`, `WithExpr`, `WithSilenceErrors`, `WithCollection`, `WithK`, `WithRankerConfig`, `WithReranker`, `WithBFS`, `WithFilterExpr`, `WithOutputFields`, `WithQueryEmbedding`, `WithLanguage`, `WithMinScore`

#### QueryExpr 最小定义

```go
type QueryExpr interface {
    ToExpr(backend string) (string, error)
}
```

4.28 Query Builder 完善后替换此接口。

#### 集合常量

```go
const (
    EntityCollection   = "ENTITY_COLLECTION"
    RelationCollection = "RELATION_COLLECTION"
    EpisodeCollection  = "EPISODE_COLLECTION"
    AllCollections     = "all"
    DefaultWorkerNum   = 10
)
```

#### 工厂

```go
var globalFactory = &GraphStoreFactory{
    backends: make(map[string]func(*GraphConfig) (BaseGraphStore, error)),
}

type GraphStoreFactory struct {
    mu       sync.RWMutex
    backends map[string]func(*GraphConfig) (BaseGraphStore, error)
}

func RegisterBackend(name string, constructor func(*GraphConfig) (BaseGraphStore, error), force ...bool)
func NewFromConfig(config *GraphConfig, backendName ...string) (BaseGraphStore, error)
```

### 5. Milvus 后端实现

#### 5.1 schema.go — 集合 Schema 与索引构建

三个集合的 Schema 定义：

**通用字段**（所有集合）：
- uuid (VARCHAR, PK)
- created_at (INT64)
- user_id (VARCHAR)
- obj_type (VARCHAR, whitespace analyzer)
- language (VARCHAR)
- metadata (JSON)
- content (VARCHAR, BM25 analyzer) + content_embedding (FLOAT_VECTOR) + content_bm25 (SPARSE_FLOAT_VECTOR, BM25 Function 自动生成)

**Entity 特有字段**：
- name (VARCHAR, ICU analyzer) + name_embedding (FLOAT_VECTOR)
- attributes (JSON)
- relations (ARRAY<VARCHAR>)
- episodes (ARRAY<VARCHAR>)

**Relation 特有字段**：
- name (VARCHAR) + lhs (VARCHAR) + rhs (VARCHAR)
- valid_since (INT64) + valid_until (INT64)
- offset_since (INT8) + offset_until (INT8)

**Episode 特有字段**：
- valid_since (INT64)
- entities (ARRAY<VARCHAR>)

索引定义：
- Entity: name_embedding索引 + content_embedding索引 + content_bm25索引
- Relation/Episode: content_embedding索引 + content_bm25索引

#### 5.2 milvus_writer.go — 写入逻辑

```go
type graphWriter struct {
    client     milvusClient
    storageCfg *GraphStoreStorageConfig
    embedder   embedding.BaseEmbedding
    embedDim   int
    batchSize  int
    sem        chan struct{}
}
```

写入流程：
1. 遍历图对象，调用 `EmbedTasks()` 收集嵌入任务
2. 如果 `noEmbed=false` 且 `embedder!=nil`，调用 `fetchAndEmbed()` 批量嵌入
3. 调用 `truncateFields()` 按 storageConfig 截断超长字段
4. 序列化为 `[]map[string]any`，调用 `client.Upsert()` 或 `client.Insert()`
5. 如果 `flush=true`，调用 `client.Flush()`
6. 插入失败时回退到逐条插入

#### 5.3 milvus_searcher.go — 搜索逻辑

```go
type graphSearcher struct {
    client   milvusClient
    embedder embedding.BaseEmbedding
    indexCfg *GraphStoreIndexConfig
    registry *RankerRegistry
    metric   string // COSINE/L2/IP
}
```

三种搜索模式：

**模式1：collection="all"（searchAll）**
1. 并发搜索 Entity/Relation/Episode（各调用 rawHybridSearch）
2. 合并结果
3. combinedRerank（用关系内容丰富实体后 rerank）

**模式2：单集合 + BFS（searchSingle with BFS）**
1. 第1轮：rawHybridSearch(skipRanking=true) → 得到初始 UUID 集合
2. 循环 bfsDepth 次：
   - expandEntities/expandRelations → 扩展 UUID 集合
   - rawHybridSearch(skipRanking=true, filter=扩展后UUIDs) → 合并 UUID
3. 最终：rawHybridSearch(skipRanking=false, filter=全部UUID) → rankResults

**模式3：单集合无 BFS（searchSingle without BFS）**
1. rawHybridSearch(skipRanking=false) → rankResults

3通道混合搜索细节（rawHybridSearch）：
1. queryEmbedding → 获取查询向量
2. buildSearchRequests：
   - name_embedding + query_vector → AnnSearchRequest
   - content_embedding + query_vector → AnnSearchRequest
   - content_bm25 + query_text → AnnSearchRequest(BM25搜索)
3. getRankerAndRequests：
   - Entity: 3通道全活跃
   - Relation/Episode: name_dense 强制为0，仅2通道
4. client.HybridSearch()（SDK 方法：`Reranker` + `AnnRequest`）→ 解析返回结果

**BM25 实现方式**：使用新 SDK 的 `entity.FunctionTypeBM25`，在创建集合时定义 BM25 Function 自动将 content 文本转为稀疏向量。写入时只需提供 content 文本，Milvus 服务端自动计算 BM25 稀疏向量并填入 content_bm25 字段。搜索时 BM25 通道直接使用文本查询，无需客户端侧计算。

#### 5.4 milvus.go — 主结构体

```go
type MilvusGraphStore struct {
    *graphWriter
    *graphSearcher

    config       *GraphConfig
    client       milvusClient
    createClient func(ctx context.Context, uri, token, dbName string) (milvusClient, error)
    mu           sync.RWMutex
    initialized  bool
}
```

- 编译时检查：`var _ graph.BaseGraphStore = (*MilvusGraphStore)(nil)`
- lazy init：首次调用时创建 client + EnsureCollections
- double-check locking 保护并发初始化
- 接口方法委托给嵌入的 graphWriter/graphSearcher

#### 5.5 milvusClient 私有接口

```go
type milvusClient interface {
    CreateCollection(ctx context.Context, option CreateCollectionOption, callOptions ...grpc.CallOption) error
    DropCollection(ctx context.Context, option DropCollectionOption, callOptions ...grpc.CallOption) error
    HasCollection(ctx context.Context, option HasCollectionOption, callOptions ...grpc.CallOption) (bool, error)
    DescribeCollection(ctx context.Context, option DescribeCollectionOption, callOptions ...grpc.CallOption) (*entity.Collection, error)
    Insert(ctx context.Context, option InsertOption, callOptions ...grpc.CallOption) (ResultSet, error)
    Upsert(ctx context.Context, option UpsertOption, callOptions ...grpc.CallOption) (ResultSet, error)
    Delete(ctx context.Context, option DeleteOption, callOptions ...grpc.CallOption) error
    Flush(ctx context.Context, option FlushOption, callOptions ...grpc.CallOption) error
    Query(ctx context.Context, option QueryOption, callOptions ...grpc.CallOption) (ResultSet, error)
    HybridSearch(ctx context.Context, option HybridSearchOption, callOptions ...grpc.CallOption) ([]ResultSet, error)
    CreateIndex(ctx context.Context, option CreateIndexOption, callOptions ...grpc.CallOption) error
    DropIndex(ctx context.Context, option DropIndexOption, callOptions ...grpc.CallOption) error
    DescribeIndex(ctx context.Context, option DescribeIndexOption, callOptions ...grpc.CallOption) ([]entity.Index, error)
    LoadCollection(ctx context.Context, option LoadCollectionOption, callOptions ...grpc.CallOption) error
    GetCollectionStats(ctx context.Context, option GetCollectionStatsOption, callOptions ...grpc.CallOption) (map[string]string, error)
    Close() error
}
```

**注意**：以上接口方法签名基于新 SDK `github.com/milvus-io/milvus/client/v2/milvusclient` 的 Builder 模式。
每个方法接收一个 Option 对象而非位置参数，与旧 SDK 的 `opts ...OptionFunc` 模式不同。
具体 Option 类型（`CreateCollectionOption`、`HybridSearchOption` 等）由新 SDK 的 `milvusclient` 包提供。

### 6. 工具函数（utils.go）

| 函数 | 签名 | 对应 Python |
|------|------|-------------|
| GetUUID | `() string` | `get_uuid` |
| GetCurrentUTCTimestamp | `() int64` | `get_current_utc_timestamp` |
| EnsureUniqueUUIDs | `(ctx, store, ids, collection, skip) ([]string, error)` | `ensure_unique_uuids` |
| Batched | `[T any](items []T, n int) [][]T` | `batched`（泛型替代） |
| FormatTimestamp | `(t int64, tz, layout) string` | `format_timestamp` |
| FormatTimestampISO | `(t int64, tz) string` | `format_timestamp_iso` |
| ISO2Timestamp | `(isoStr string) (int64, int8, error)` | `iso2timestamp` |
| LoadStoredTimeFromDB | `(timestamp int64, offset int8) (*time.Time, error)` | `load_stored_time_from_db` |

去除项：
- `with_metadata`：asyncio task 追踪，Go 不需要
- `format_list_of_messages`：上层逻辑，不在 store 层

### 7. 测试策略

#### 层次1：graph/ 包单元测试（无需 Milvus）

| 测试文件 | 覆盖内容 |
|---------|---------|
| `graph_object_test.go` | 图对象创建、EmbedTasks()、ToMap() 序列化、UUID 去重排序、字段截断 |
| `config_test.go` | NewGraphConfig() 默认值、Validate() 校验逻辑 |
| `ranking_test.go` | WeightedRankConfig/RRFRankConfig 的 IsActive()/Args() 归一化、注册表 Register/Get |
| `base_test.go` | GraphStoreFactory 注册/创建、重复注册、未找到后端 |
| `utils_test.go` | UUID 格式、时间戳转换、批处理、时区偏移 |

#### 层次2：milvus/ 包单元测试（fake milvusClient）

| 测试文件 | 覆盖内容 |
|---------|---------|
| `milvus_test.go` | NewMilvusGraphStore、lazy init、Close、AttachEmbedder、Rebuild |
| `milvus_writer_test.go` | addEntity/addRelation/addEpisode（含嵌入+截断）、delete、批量插入失败回退 |
| `milvus_searcher_test.go` | searchAll、searchSingle（有/无BFS）、rawHybridSearch、expandEntities/expandRelations、rankResults、combinedRerank |
| `schema_test.go` | buildEntitySchema/buildRelationSchema/buildEpisodeSchema、索引参数构建 |

fake 组件：
- `fakeMilvusClient`：实现 milvusClient 接口，内存存储
- `fakeMilvusClientWithSearch`：嵌入 fakeMilvusClient，覆盖 HybridSearch
- `fakeEmbedder`：实现 embedding.BaseEmbedding
- `fakeReranker`：实现 reranker.BaseReranker

#### 层次3：集成测试（build tag: integration）

```go
//go:build integration
```

仅覆盖基本 CRUD + 搜索的端到端流程。

### 8. 依赖关系

graph 包依赖：
- `internal/agentcore/store/embedding` — BaseEmbedding 接口
- `internal/agentcore/store/reranker` — BaseReranker 接口
- `internal/agentcore/store/vector_fields` — VectorField 索引配置
- `internal/common/exception` — 错误码（186003-186009 已定义）
- `internal/common/logger` — 日志组件
- `github.com/milvus-io/milvus/client/v2` — 新 Milvus Go SDK（替代已归档的 milvus-sdk-go/v2）

### 8.1 SDK 迁移（前置任务）

Graph Store 实现前，需先完成 Milvus SDK 迁移（4.8 MilvusVectorStore 从旧 SDK 迁移到新 SDK）：

**迁移范围**：
- `go.mod`：替换 `milvus-sdk-go/v2@v2.4.2` → `milvus-io/milvus/client/v2`
- `vector/milvus.go`（1267 行）：重写 milvusClient 接口 + 所有 SDK 类型映射 + 构造函数
- `vector/milvus_test.go`（1929 行）：更新所有 fake client 的接口实现

**关键 API 变化**：
- 旧：`client.NewClient(ctx, client.Config{...})` → 新：`milvusclient.New(ctx, milvusclient.Config{...})`
- 旧：位置参数 + `opts ...OptionFunc` → 新：Option Builder 链式调用
- 旧：`client.ANNSearchRequest` → 新：`milvusclient.AnnRequest`
- 旧：`client.Reranker` → 新：`milvusclient.Reranker`
- 旧：`client.SearchResult` → 新：`milvusclient.ResultSet`
- 新增：`entity.Function` + `entity.FunctionTypeBM25` 支持

### 9. 日志同步

对照 Python 中的 logger 调用，在 Go 等价位置使用 `logger.Info(logComponent)`/`logger.Error(logComponent)` 记录日志，组件使用 `logger.ComponentAgentCore`。

关键日志点：
- 集合创建/重建
- 嵌入向量获取（批量大小、耗时）
- 混合搜索（查询文本、集合、K值、通道权重）
- BFS 扩展（深度、扩展前后的 UUID 数量）
- 插入失败回退
- reranking 执行
