# ChromaVectorStore 设计文档

## 概述

本文档描述 ChromaVectorStore 的 Go 实现方案，对应实现计划领域四第 4.9 小节。

ChromaVectorStore 是 BaseVectorStore 接口的 ChromaDB 后端实现，使用 Persistent Client（本地嵌入式）模式运行，数据持久化到指定目录。Python 对应源码为 `openjiuwen/core/foundation/store/vector/chroma_vector_store.py`。

## 关键决策

| 决策项 | 选择 | 理由 |
|--------|------|------|
| Go SDK | `amikos-tech/chroma-go` v0.4.x | 社区最活跃（518 commits, 204 stars），支持 ChromaDB v1.x，API 完整 |
| 客户端模式 | Persistent Client（本地嵌入式） | 与 Python 版 `chromadb.PersistentClient` 对齐 |
| CGO 依赖 | 不依赖 CGO | SDK 使用 `ebitengine/purego`（纯 Go dlopen）加载 Rust 编译的 `libchroma_shim.so`，编译时无需 C 编译器 |
| 迁移功能 | 仅实现基础 CRUD + GetAllDocuments，迁移入口留 placeholder | 与 MilvusVectorStore 一致的策略，UpdateSchema 待 7.22/7.23 |
| field_mapping 存储 | ChromaDB metadata + 本地内存缓存 | 与 Python 完全对齐 |
| 文件组织 | 放在现有 `vector/` 包下，新增 `chroma.go` | 与 MilvusVectorStore 同包，共享接口和类型 |

## 文件结构

```
internal/agentcore/store/vector/
├── doc.go                          # 更新：添加 chroma.go 条目
├── base.go                         # 不变
├── chroma.go                       # 新增：ChromaVectorStore 实现
├── chroma_test.go                  # 新增：单元测试（fake client）
├── chroma_integration_test.go      # 新增：集成测试（//go:build integration）
├── milvus.go                       # 不变
├── utils.go                        # 不变（已有 Chroma 转换函数）
└── ...
```

## 核心结构

### ChromaVectorStore

```go
// ChromaVectorStore 基于 ChromaDB 的向量存储实现。
// 使用 Persistent Client（本地嵌入式），数据持久化到指定目录。
type ChromaVectorStore struct {
    client           chromaClient               // 接口抽象，测试注入 fake
    persistDirectory string                      // 持久化目录
    collectionCache  map[string]chromaCollection // collection 对象缓存
    fieldMappings    map[string]*fieldMapping    // 本地 field_mapping 缓存
    mu               sync.RWMutex
    createClient     func(persistDir string) (chromaClient, error) // 依赖注入
}
```

### fieldMapping

```go
// fieldMapping 用户字段名到 ChromaDB 内置字段的映射及距离度量缓存
type fieldMapping struct {
    PrimaryKey     string // 映射到 ChromaDB 的 ids
    VectorField    string // 映射到 ChromaDB 的 embeddings
    TextField      string // 映射到 ChromaDB 的 documents（可为空）
    DistanceMetric string // 距离度量（cosine/l2/ip），Search 时用于选择转换函数
}
```

### chromaClient 接口

> **注意**：以下接口签名为概念设计，实现时需根据 `amikos-tech/chroma-go` v0.4.x 的 V2 API（`pkg/api/v2`）实际方法签名精确对齐。接口的核心目的是隔离 SDK 细节以支持 fake 测试，方法集应覆盖 ChromaVectorStore 所需的全部 SDK 操作。

```go
// chromaClient ChromaDB 客户端操作接口（用于解耦和测试）
type chromaClient interface {
    GetOrCreateCollection(ctx context.Context, name string, metadata map[string]any, distanceFunc string) (chromaCollection, error)
    GetCollection(ctx context.Context, name string) (chromaCollection, error)
    DeleteCollection(ctx context.Context, name string) error
    ListCollections(ctx context.Context) ([]string, error)
    Close() error
}
```

### chromaCollection 接口

> **注意**：方法签名需根据 SDK V2 API 的 Collection 实际方法对齐。SDK 使用 Option 模式（如 `WithIDs`、`WithWhere`、`WithEmbeddings` 等），接口可能需要调整为接受 Option 参数或更灵活的参数结构。

```go
// chromaCollection ChromaDB 集合操作接口
type chromaCollection interface {
    Add(ctx context.Context, ids []string, embeddings [][]float32, documents []string, metadatas []map[string]any) error
    Query(ctx context.Context, queryEmbeddings [][]float32, nResults int32, where map[string]any, include []string) (chromaQueryResult, error)
    Get(ctx context.Context, ids []string, where map[string]any, include []string) (chromaGetResult, error)
    Delete(ctx context.Context, ids []string, where map[string]any) error
    Modify(ctx context.Context, name string, metadata map[string]any) error
    Count(ctx context.Context) (int32, error)
    GetMetadata() map[string]any
}
```

### chromaQueryResult / chromaGetResult

```go
// chromaQueryResult ChromaDB 查询结果
type chromaQueryResult struct {
    IDs        [][]string
    Documents  [][]string
    Metadatas  [][]map[string]any
    Distances  [][]float32
}

// chromaGetResult ChromaDB Get 结果
type chromaGetResult struct {
    IDs        []string
    Documents  []string
    Metadatas  []map[string]any
    Embeddings [][]float32
}
```

## BaseVectorStore 接口方法实现映射

| BaseVectorStore 方法 | ChromaDB 实现策略 | 与 Python 对齐要点 |
|---|---|---|
| CreateCollection | `client.GetOrCreateCollection`，构建 field_mapping 存入 metadata，配置 HNSW distance | Python 存 `schema`+`field_mapping`+`distance_metric` 到 metadata |
| DeleteCollection | `client.DeleteCollection`，清除 collectionCache 和 fieldMappings 缓存 | 一致 |
| CollectionExists | 尝试 `client.GetCollection`，成功返回 true，异常返回 false | 一致 |
| GetSchema | 从 collection metadata 中读取 `schema` JSON 反序列化；fallback 从 field_mapping 构建默认 schema | Python 有 schema→fields→field_mapping→default 四级 fallback |
| AddDocs | 按 field_mapping 提取 ids/embeddings/documents/metadatas，分批 `collection.Add` | Python 默认 batch_size=128 |
| Search | `collection.Query`，根据 distance_metric 调用对应的转换函数，映射回用户字段名 | Python 从 metadata 取 metric 决定转换方式 |
| DeleteDocsByIDs | `collection.Delete(ids=ids)` | 一致 |
| DeleteDocsByFilters | `collection.Delete(where=filters)` | 一致（ChromaDB where 过滤） |
| ListCollectionNames | `client.ListCollections` 提取名称列表 | 一致 |
| UpdateSchema | 返回 `not implemented` 错误（placeholder，待 7.22/7.23） | 与 MilvusVectorStore 一致的策略 |
| UpdateCollectionMetadata | `collection.Modify` 更新 metadata，同时更新本地 fieldMappings 缓存 | Python 用 `collection.modify(metadata=...)` |
| GetCollectionMetadata | 从 collection metadata 读取 `distance_metric` 等，补充默认值 | Python 默认 distance_metric=cosine, schema_version=0 |

### 新增导出方法：GetAllDocuments

Python 版有 `get_all_documents` 方法用于迁移场景。`BaseVectorStore` 接口目前不包含此方法，因此作为 `ChromaVectorStore` 的独立导出方法实现：

```go
// GetAllDocuments 获取集合中的所有文档（含向量），用于迁移场景。
func (s *ChromaVectorStore) GetAllDocuments(ctx context.Context, collectionName string) ([]map[string]any, error)
```

实现方式：`collection.Get(include=[documents, metadatas, embeddings])`，按 field_mapping 映射回用户字段名。

## field_mapping 缓存与持久化策略

### 存储

field_mapping JSON 序列化后存入 ChromaDB collection 的 metadata（key = `"field_mapping"`），同时内存维护 `map[string]*fieldMapping` 缓存。

### 读取优先级

1. 内存缓存命中 → 直接返回
2. 缓存未命中 → 从 collection metadata 读取 JSON 反序列化，填入缓存
3. metadata 中无 field_mapping → 从 schema 推断（主键→ids，第一个 FLOAT_VECTOR→embeddings，第一个非主键 VARCHAR→documents），distance_metric 默认为 cosine

### 生命周期

- `CreateCollection` 时构建并写入 metadata + 缓存
- `DeleteCollection` 时清除缓存
- `GetSchema`/`AddDocs`/`Search` 等操作时按需加载缓存
- `UpdateCollectionMetadata` 时同步更新缓存

### ChromaDB metadata 中存储的完整字段

| Key | 内容 | 说明 |
|-----|------|------|
| `schema` | CollectionSchema JSON | 完整 schema 定义 |
| `field_mapping` | fieldMapping JSON | 字段映射关系 |
| `distance_metric` | string | 距离度量（cosine/l2/ip） |
| `vector_field` | string | 向量字段名 |

## 距离转换与分数归一化

Python 版根据 collection metadata 中存储的 `distance_metric` 选择转换函数。`utils.go` 中三个 Chroma 专用函数已经实现：

| distance_metric | ChromaDB 返回 | Go 转换函数 | 公式 |
|---|---|---|---|
| `cosine` | 余弦距离 [0, 2] | `ConvertCosineDistance` | `(2 - raw) / 2` |
| `l2` | L2 平方距离 | `ConvertL2Squared` | `(maxDist - raw) / maxDist` |
| `ip` | 内积距离 [0, 2] | `ConvertIPDistance` | `clamp((2 - raw) / 2, 0, 1)` |

Search 时从缓存的元数据获取 distance_metric，选择对应转换函数。

## 构造函数

```go
// NewChromaVectorStore 创建 ChromaVectorStore 实例。
// persistDirectory 为持久化目录，为空时使用内存模式。
func NewChromaVectorStore(persistDirectory string) *ChromaVectorStore
```

Persistent Client 配置：
- `WithPersistentPath(persistDirectory)` 设置持久化目录
- `WithPersistentLibraryAutoDownload(true)` 允许自动下载 libchroma_shim.so
- 默认使用 Embedded 模式（`PersistentRuntimeModeEmbedded`）
- 不配置 embedding function（向量由外部模型生成，预计算后传入）

## 日志与错误处理

### 日志

对齐 Python 版每个 `store_logger` 调用点，使用 `logger.Info(logComponent)` 等（`logComponent = logger.ComponentAgentCore`），按项目日志规则补充结构化字段。

关键日志点：

| 操作 | 级别 | 字段 |
|------|------|------|
| CreateCollection 成功 | Info | `collection_name`, `field_count` |
| DeleteCollection 成功 | Info | `collection_name` |
| DeleteCollection 失败 | Error | `collection_name`, err |
| AddDocs 批次进度 | Info | `collection_name`, `processed`, `total` |
| AddDocs 完成 | Info | `collection_name`, `data_num` |
| Search 完成 | Info | `collection_name`, `result_count` |
| DeleteDocsByIDs 完成 | Info | `collection_name`, `id_count` |
| DeleteDocsByFilters 完成 | Info | `collection_name` |
| GetAllDocuments 完成 | Info | `collection_name`, `data_num` |
| SDK 调用失败 | Error | `collection_name`, `event_type=LLM_CALL_ERROR`, err |

### 错误码

| 场景 | 错误码 |
|------|--------|
| 集合不存在 | `StatusStoreVectorCollectionNotFound` |
| Schema 校验失败（无主键/无向量字段） | `StatusStoreVectorSchemaInvalid` |
| 文档校验失败（缺主键/缺向量字段） | `StatusStoreVectorDocInvalid` |
| UpdateSchema 未实现 | `StatusStoreVectorSchemaInvalid` |

## 测试策略

### 单元测试（chroma_test.go）

使用 `fakeChromaClient` + `fakeChromaCollection` 实现 `chromaClient`/`chromaCollection` 接口，覆盖所有方法和错误路径。

fake 设计参考 `milvus_test.go` 的 `fakeMilvusClient` 模式：
- 基础 fake：成功路径
- 专用变体：`fakeChromaClientWithQueryError`、`fakeChromaClientWithAddError`、`fakeChromaClientWithDeleteError` 等

覆盖率目标：≥ 85%（与项目编码规范一致）。

### 集成测试（chroma_integration_test.go）

- `//go:build integration` 标签隔离
- 使用真实 PersistentClient，自动下载 `libchroma_shim.so`
- 从环境变量 `CHROMA_PERSIST_DIR` 读取持久化目录（默认 `t.TempDir()`）
- 运行方式：`go test -tags=integration ./internal/agentcore/store/vector/...`

## 源码声明排列

遵循项目编码规范 2，文件内部声明顺序：

1. 结构体（`chromaClient`、`chromaCollection`、`chromaQueryResult`、`chromaGetResult`、`fieldMapping`、`ChromaVectorStore`）
2. 枚举（无）
3. 常量（`defaultChromaBatchSize`、`defaultChromaDistanceMetric`）
4. 全局变量（`logComponent` 复用）
5. 导出函数（`NewChromaVectorStore`、所有 BaseVectorStore 方法、`GetAllDocuments`、`Close`）
6. 非导出函数（`getFieldMapping`、`buildChromaMetadata`、`docsToAddParams`、`queryResultToSearchResults`、`normalizeChromaScore`）
