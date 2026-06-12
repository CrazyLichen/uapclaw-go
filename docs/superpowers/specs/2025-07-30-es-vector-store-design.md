# ESVectorStore 设计文档

## 概述

本文档描述 Elasticsearch 向量存储（ESVectorStore）的 Go 实现设计，对应实现计划 4.11 节。

Python 源码路径：`openjiuwen/extensions/store/vector/es_vector_store.py`

ESVectorStore 使用 Elasticsearch 8.x 的 `dense_vector` 字段类型 + 原生 k-NN 搜索提供向量相似度搜索能力。每个 collection 映射为一个 ES index。

## 决策记录

| 决策点 | 选择 | 理由 |
|--------|------|------|
| 客户端注入 | 惰性创建 | 对齐 Go 项目 Milvus/Gauss 现有模式 |
| 元数据持久化 | ES 内部 `_meta` 文档 | 对齐 Python 实现，进程重启后可恢复 |
| Go ES 客户端库 | `github.com/elastic/go-elasticsearch/v8` | 官方维护，API 完整，ES 8.x 原生支持 |
| vector_fields 子类型 | 新增 `ESVectorField` + `DatabaseTypeES` | 对齐其他实现模式，方便后续扩展 |
| UpdateSchema | 先 stub | 与 Milvus/Chroma/Gauss 保持一致，待 7.22/7.23 统一实现 |
| num_candidates 传递 | 通过 Option 新增字段 | 与 BatchSize、OutputFields 等参数传递方式一致 |
| IndexType | 复用 `IndexTypeHNSW` | ES k-NN 本质就是 HNSW，语义正确 |

## 文件变更清单

| 文件 | 操作 | 说明 |
|------|------|------|
| `internal/agentcore/store/vector/es.go` | 新增 | ESVectorStore 实现 |
| `internal/agentcore/store/vector/es_test.go` | 新增 | 单元测试（httptest mock） |
| `internal/agentcore/store/vector/base.go` | 修改 | Options 新增 `NumCandidates` 字段 + `WithNumCandidates` Option |
| `internal/agentcore/store/vector/doc.go` | 修改 | 添加 es.go 条目 |
| `internal/agentcore/store/vector_fields/base.go` | 修改 | 新增 `DatabaseTypeES` 枚举值 + 字符串 |
| `internal/agentcore/store/vector_fields/es_fields.go` | 新增 | ESVectorField 定义 |
| `internal/agentcore/store/vector_fields/es_fields_test.go` | 新增 | ESVectorField 测试 |
| `internal/agentcore/store/vector_fields/doc.go` | 修改 | 添加 es_fields.go 条目 |
| `go.mod` / `go.sum` | 修改 | 添加 `github.com/elastic/go-elasticsearch/v8` 依赖 |

## 核心设计

### 1. ESVectorStore 结构体

```go
type ESVectorStore struct {
    client       esClient       // 惰性创建的 ES 客户端
    addresses    []string       // ES 节点地址列表
    username     string         // 认证用户名
    password     string         // 认证密码
    indexPrefix  string         // index 名前缀，默认 "agent_vector"
    createClient func(addresses []string, username, password string) (esClient, error) // 依赖注入，无 ctx（对齐 Milvus/Gauss）
    mu           sync.RWMutex   // 保护 client
}
```

构造函数：`NewESVectorStore(addresses, ...ESOption)` 返回 `*ESVectorStore`，惰性创建客户端。

### 2. esClient 接口（私有，用于测试 mock）

采用透传式接口，直接透传 `esapi.Request` → `esapi.Response`，避免为每个 ES API 操作定义方法：

```go
type esClient interface {
    Do(req esapi.Request) (*esapi.Response, error)
    Close()
}
```

封装 `*elasticsearch8.Client` 的 `esClientWrapper` 实现：调用 `req.Do(ctx, inner)` 转发。
测试中 `fakeESClient` 实现 `esapi.Transport` 接口（`Perform(req) (*http.Response, error)`），将请求转发到 `httptest.Server`。

### 3. 集合 ↔ Index 映射

命名格式：`{indexPrefix}__{collectionName}`，与 Python 一致。

示例：`agent_vector__my_collection`

### 4. 元数据 _meta 文档

每个 index 内存储一个专用文档，文档 ID = `__collection_metadata__`：

```json
{
  "_meta": {
    "schema": { "fields": [...], "description": "...", "enable_dynamic_field": false },
    "distance_metric": "COSINE",
    "vector_field": "embedding",
    "vector_dim": 768,
    "schema_version": 0,
    "collection_name": "my_collection"
  }
}
```

index mapping 中 `_meta` 字段设为 `{"type": "object", "enabled": false}`，确保不被索引和搜索。

读写流程：
- **写入**：`create_collection` 时通过 `esStoreMetadata` 写入 _meta 文档，同时更新内存缓存
- **读取**：`esLoadMetadata` 优先查内存缓存，缓存未命中时从 ES _meta 文档加载并写入缓存
- **更新**：`update_collection_metadata` 时先读再合并再写，同时更新内存缓存
- **删除**：`delete_collection` 时清除对应缓存条目
- **关闭**：`Close` 时清空全部缓存
- 缓存 key 为 indexName（含前缀），value 为 `map[string]any`
- **缓存**：维护内存缓存（对齐 Python `_metadata_cache`），缓存未命中时从 ES _meta 文档回查

### 5. 类型映射

#### VectorDataType → ES mapping type

| VectorDataType | ES mapping type | 备注 |
|---------------|-----------------|------|
| FLOAT_VECTOR | `dense_vector` | 含 dims, index:true, similarity |
| VARCHAR | `keyword` | |
| INT64 | `long` | |
| INT32 | `integer` | |
| INT16 | `integer` | ES 无 short 类型 |
| INT8 | `integer` | ES 无 byte 标量类型 |
| FLOAT | `float` | |
| DOUBLE | `double` | |
| BOOL | `boolean` | |
| JSON | `object` (enabled:true) | |
| ARRAY | `object` (enabled:true) | |

#### ES mapping type → VectorDataType

| ES type | VectorDataType |
|---------|---------------|
| dense_vector | FLOAT_VECTOR |
| keyword / text | VARCHAR |
| long | INT64 |
| integer / short / byte | INT32 |
| float | FLOAT |
| double | DOUBLE |
| boolean | BOOL |
| object | JSON |

> **注意**：Python 实现中 short → INT16、byte → INT8，但 Go 实现统一映射为 INT32。
> 原因：Go 侧 `esMapFieldType` 中 INT16/INT8 统一映射为 ES `integer` 类型，
> 反向映射时无法区分，因此统一回 INT32，避免双向映射不对称。

#### Distance Metric → ES similarity

| Distance Metric | ES similarity |
|----------------|---------------|
| COSINE | cosine |
| L2 | l2_norm |
| IP | dot_product |

### 6. CreateCollection 流程

1. 校验 schema 必须包含至少一个 FLOAT_VECTOR 字段
2. 检查 index 是否已存在（`IndicesExists`），已存在则跳过
3. 从 schema 构建 ES mapping（`_buildMappings`）：
   - 遍历 fields，向量字段 → dense_vector，标量字段 → 对应类型
   - 添加 `_meta` 字段（enabled:false）
   - 设置 `dynamic: strict`
4. 创建 index（`IndicesCreate`）
5. 存储 _meta 元数据文档
6. 记录日志

### 7. AddDocs 流程

1. 空文档直接返回
2. 从 _meta 文档获取 schema，提取 primary_key_field
3. 构建批量操作 body（NDJSON 格式）：
   - 有主键的文档：`{ "index": { "_index": "...", "_id": "..." } }` + 文档源
   - 无主键的文档：`{ "index": { "_index": "..." } }` + 文档源
4. 按 BatchSize 分批调用 `Bulk`（默认 500）
5. 刷新 index（`IndicesRefresh`）
6. 记录日志

### 8. Search 流程

1. 从 _meta 文档获取 distance_metric
2. 构建 ES 查询 body：
   ```json
   {
     "knn": {
       "field": "embedding",
       "query_vector": [0.1, 0.2, ...],
       "k": 5,
       "num_candidates": 50,
       "filter": { "bool": { "filter": [...] } }
     },
     "size": 5,
     "_source": { "excludes": ["_meta"] }
   }
   ```
3. num_candidates 取值：优先 Option 中的 `NumCandidates`，未设置则 `max(topK*10, 100)`
4. 过滤条件构建：
   - 单值 → `{"term": {"field": value}}`
   - 切片 → `{"terms": {"field": [v1, v2]}}`
   - 组合 → `{"bool": {"filter": [...]}}`
5. 解析搜索结果：
   - 从 `_score` 提取分数
   - **分数归一化**（Go 独有增强，Python 直接返回原始 _score）：
     - COSINE → `ConvertCosineDistance`
     - L2 → `ConvertL2Squared`
     - IP → `ConvertIPSimilarity`
   - 从 `_source` 提取字段（移除 `_meta`）
   - `_id` 回填到 `id` 字段
6. `_source` 处理（Go 独有增强）：
   - **始终**设置 `_source.excludes: ["_meta"]`，即使无 OutputFields（Python 仅在有 output_fields 时设置）
   - 有 OutputFields 时额外设置 `_source.includes`

> **与 Python 的差异说明**：Python search 返回 ES 原始 _score（COSINE 时为距离值，非相似度），
> Go 做了归一化转换，返回 [0,1] 范围的相似度分数，与其他向量存储后端行为一致。
> Python 仅在有 output_fields 时设 `_source.excludes`，Go 始终排除 `_meta`，防止元数据泄漏。

### 9. DeleteDocsByIDs 流程

1. 构建 NDJSON 批量删除操作
2. 按 BatchSize 分批调用 `Bulk`（默认 500，refresh=true）
3. 记录日志

### 10. DeleteDocsByFilters 流程

1. 构建过滤条件（与 Search 过滤逻辑复用）
2. 调用 `DeleteByQuery`（refresh=true）
3. 记录日志

### 11. ListCollectionNames 流程

1. 通过 ES `_cat/indices` API 列出匹配 `{prefix}__*` 的 index
   - **注意**：Python 使用 `indices.get`，Go 使用更轻量的 `_cat/indices`，返回格式不同但功能等价
2. 去掉前缀返回 collection 名称列表

### 12. GetSchema 流程

1. 优先从 _meta 文档读取 schema 字段，`CollectionFromDict` 反序列化
2. _meta 无 schema 时，fallback 到 `IndicesGetMapping` 反射构建：
   - 遍历 properties（跳过 `_meta`）
   - `mapESTypeToOurType` 转换类型
   - dense_vector 字段从 `dims` 提取维度
   - 通过 Option 或默认值推断主键字段

### 13. ESVectorField

```go
type ESVectorField struct {
    vector_fields.VectorField
    // NumCandidates k-NN 搜索候选集大小（search 阶段）
    NumCandidates int `vf:"search"`
    // ExtraConstruct 构建阶段额外参数
    ExtraConstruct map[string]any `vf:"construct"`
    // ExtraSearch 搜索阶段额外参数
    ExtraSearch map[string]any `vf:"search"`
}
```

构造函数：`NewESVectorField(fieldName string) *ESVectorField`

设置：`DatabaseType=DatabaseTypeES`, `IndexType=IndexTypeHNSW`

### 14. Options 扩展

在 `base.go` 的 `Options` 结构体新增：

```go
// NumCandidates ES k-NN 搜索候选集大小，0 表示使用默认值 max(topK*10, 100)
NumCandidates int
```

新增 Option 函数：

```go
func WithNumCandidates(n int) Option {
    return func(o *Options) { o.NumCandidates = n }
}
```

### 15. DatabaseType 枚举扩展

在 `vector_fields/base.go` 新增：

```go
// DatabaseTypeES Elasticsearch 向量数据库
DatabaseTypeES
```

`databaseTypeStrings` 新增 `"es"` 条目。

### 16. UpdateSchema

Stub 实现，返回"未实现"错误，与 Milvus/Chroma/Gauss 对齐：

```go
func (s *ESVectorStore) UpdateSchema(ctx context.Context, collectionName string, operations []any, opts ...Option) error {
    return exception.BuildError(exception.StatusStoreVectorSchemaInvalid,
        exception.WithParam("error_msg", "UpdateSchema 未实现，待 7.22/7.23 回填"),
    )
}
```

## 测试策略

### 单元测试（默认 `go test ./...` 执行）

- 使用 `net/http/httptest` 起本地 HTTP 服务模拟 ES REST API
- 通过 `esClient` 接口 + 替换 `createClient` 函数注入 mock
- 覆盖所有 `BaseVectorStore` 接口方法
- 测试文件：`internal/agentcore/store/vector/es_test.go`

### 集成测试（需真实 ES 实例）

- 使用 `//go:build integration` 标签隔离
- 运行方式：`go test -tags=integration ./internal/agentcore/store/vector/...`
- 从环境变量读取 ES 连接地址

### 测试函数命名

```
TestNewESVectorStore                     — 构造函数
TestESVectorStore_CreateCollection       — 创建集合
TestESVectorStore_CreateCollection_已存在  — 集合已存在场景
TestESVectorStore_DeleteCollection       — 删除集合
TestESVectorStore_CollectionExists       — 检查集合存在
TestESVectorStore_GetSchema              — 获取 Schema
TestESVectorStore_GetSchema_从Meta恢复     — 从 _meta 文档恢复 Schema
TestESVectorStore_AddDocs                — 添加文档
TestESVectorStore_AddDocs_空文档           — 空文档场景
TestESVectorStore_Search                 — 向量搜索
TestESVectorStore_Search_带过滤           — 带过滤条件搜索
TestESVectorStore_DeleteDocsByIDs        — 按 ID 删除
TestESVectorStore_DeleteDocsByFilters    — 按过滤删除
TestESVectorStore_ListCollectionNames    — 列出集合
TestESVectorStore_UpdateCollectionMetadata — 更新元数据
TestESVectorStore_GetCollectionMetadata  — 获取元数据
TestESVectorStore_真实调用                 — +build integration
```

## 日志规范

对齐项目日志规则（规则 3）：

- 包级定义 `const logComponent = logger.ComponentAgentCore`
- 所有 Python `store_logger` 调用在 Go 等价位置使用 `logger.Info/Warn/Error(logComponent)`
- 异常路径日志包含 `event_type=LLM_CALL_ERROR`、`method`、`collection_name` 等上下文字段
- Python 日志中的 f-string 变量以结构化字段等价体现

## Python → Go 方法映射

| Python 方法 | Go 方法 |
|------------|---------|
| `__init__(es, index_prefix)` | `NewESVectorStore(addresses, ...ESOption)` |
| `close()` | `Close()` |
| `_index_name(collection_name)` | `esIndexName(collectionName)` (非导出) |
| `_map_es_type(field)` | `esMapFieldType(field)` (非导出) |
| `_map_es_type_to_our_type(es_type)` | `esMapTypeToOurType(esType)` (非导出) |
| `_build_mappings(schema, distance_metric)` | `esBuildMappings(schema, metric)` (非导出) |
| `_store_metadata(index_name, metadata)` | `esStoreMetadata(ctx, indexName, metadata)` (非导出) |
| `_load_metadata(index_name)` | `esLoadMetadata(ctx, indexName)` (非导出) |
| `create_collection(...)` | `CreateCollection(...)` |
| `delete_collection(...)` | `DeleteCollection(...)` |
| `collection_exists(...)` | `CollectionExists(...)` |
| `get_schema(...)` | `GetSchema(...)` |
| `add_docs(...)` | `AddDocs(...)` |
| `search(...)` | `Search(...)` |
| `delete_docs_by_ids(...)` | `DeleteDocsByIDs(...)` |
| `delete_docs_by_filters(...)` | `DeleteDocsByFilters(...)` |
| `list_collection_names()` | `ListCollectionNames(...)` |
| `get_collection_metadata(...)` | `GetCollectionMetadata(...)` |
| `update_collection_metadata(...)` | `UpdateCollectionMetadata(...)` |
| `update_schema(...)` | `UpdateSchema(...)` (stub) |
| `_get_primary_key_field(schema_dict)` | `esGetPrimaryKeyField(schemaDict)` (非导出) |

## 与 Python 实现的已知偏差

| # | 偏差点 | Python 行为 | Go 行为 | 原因 |
|---|--------|------------|---------|------|
| 1 | esClient 接口 | 直接使用 `AsyncElasticsearch` | 透传式 `Do(esapi.Request)` | 更简洁，与 go-elasticsearch/v8 的 esapi.Request 体系天然匹配 |
| 2 | createClient 签名 | — | 无 `ctx` 参数 | 对齐 Milvus/Gauss 项目模式 |
| 3 | Search 分数归一化 | 直接返回 ES 原始 _score | ConvertCosineDistance/L2Squared/IPSimilarity | Go 统一返回 [0,1] 相似度，与其他后端一致 |
| 4 | Search _source.excludes | 仅 output_fields 时设置 | 始终排除 `_meta` | 防止元数据泄漏，更安全 |
| 5 | DeleteCollection | 先 exists 再 delete | 同（已修复，对齐 Python） | — |
| 6 | ListCollectionNames | `indices.get` | `indices.get`（已修复，对齐 Python） | — |
| 7 | add_docs bulk | `async_bulk` 辅助函数 | 手动构建 NDJSON | Go 无 `async_bulk` 等价物 |
| 8 | _metadata_cache | 有内存缓存 | 有内存缓存（已修复，对齐 Python） | 缓存未命中或重启时从 ES _meta 文档回查 |
| 9 | short/byte 反向映射 | short→INT16, byte→INT8 | 统一→INT32 | 避免双向映射不对称 |
| 10 | esBuildMappings 使用 vectorFieldConfig | 无此功能 | construct 参数写入 mapping | 支持 HNSW 参数（m、ef_construction） |
