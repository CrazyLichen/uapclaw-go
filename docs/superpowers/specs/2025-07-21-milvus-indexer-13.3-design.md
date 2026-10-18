# 13.3 MilvusIndexer + MilvusVectorStore (retrieval 层) 实现设计

## 概述

实现 retrieval 层的 MilvusIndexer 和 MilvusVectorStore，对齐 Python `retrieval/indexing/indexer/milvus_indexer.py` 和 `retrieval/vector_store/milvus_store.py`。一步到位完成 13.3。

## 在 Agent 会话中的流程位置

```
用户上传文档/链接
     ↓
[13.10-16 Parser 层]  ← 解析文档为纯文本（☐ 未实现）
     ↓
[13.5-9  Chunker 层]  ← 分块（☐ 未实现）
     ↓
[13.4     ComputeChunkEmbeddings]  ← 计算 embedding（✅ 已完成）
     ↓
★ [13.3     MilvusIndexer]  ← 本步骤：写入 Milvus 向量库
     ↓
知识库检索（Retriever 调用 MilvusVectorStore.Search/HybridSearch）
     ↓
Agent 上下文注入 → ReAct 循环
```

MilvusIndexer 是文档入库的"最后一公里"——将已嵌入好的 TextChunk 写入 Milvus，支持 5 种操作（BuildIndex/UpdateIndex/DeleteIndex/IndexExists/GetIndexInfo）+ ensureCollection 自动建表 + close 关闭连接。

## Python 对齐

### 涉及的 Python 文件

| Python 文件 | Go 目标位置 |
|-------------|------------|
| `retrieval/utils/common.py` (`create_milvus_alias` + `deduplicate`) | `retrieval/utils/common.go` |
| `retrieval/vector_store/milvus_store.py` | `retrieval/vector_store/milvus.go` |
| `retrieval/indexing/indexer/milvus_indexer.py` | `retrieval/indexing/indexer/milvus.go` |

### 依赖关系

Python 中 retrieval 层的 Milvus 实现**直接使用 `pymilvus` SDK**，不依赖 foundation 层的 Milvus 封装。Go 对齐此模式：

- retrieval 层自建 `milvusClient` 接口，直接使用 Go Milvus SDK `milvusclient.Client`
- 不复用 foundation 层的 `milvusClient` 接口
- 保留 foundation 层引用的仅是**分数转换工具函数**（`ConvertL2Squared` / `ConvertCosineSimilarity` / `ConvertIPSimilarity`），对齐 Python 两者都 import `foundation/store/vector/utils`

## 涉及文件清单

| # | Go 文件 | 对齐 Python | 说明 |
|---|---------|------------|------|
| 1 | `retrieval/utils/common.go` | `retrieval/utils/common.py` | **新建**：`CreateMilvusAlias` + `Deduplicate` |
| 2 | `retrieval/utils/common_test.go` | — | 对应测试 |
| 3 | `retrieval/vector_store/milvus.go` | `retrieval/vector_store/milvus_store.py` | **新建**：retrieval 层 MilvusVectorStore |
| 4 | `retrieval/vector_store/milvus_test.go` | — | 对应测试 |
| 5 | `retrieval/indexing/indexer/milvus.go` | `retrieval/indexing/indexer/milvus_indexer.py` | **新建**：MilvusIndexer |
| 6 | `retrieval/indexing/indexer/milvus_test.go` | — | 对应测试 |

## 文件 1：retrieval/utils/common.go

### CreateMilvusAlias

对齐 Python `create_milvus_alias(alias, uri, user="", token=None)`：

- alias 非空时直接返回
- 否则基于 URI + token MD5 生成别名：`"kb-{uri}-{auth_info}"`
- token 非空时 auth_info = md5(token)，否则 auth_info = user 或 "noauth"

### Deduplicate

对齐 Python `deduplicate(data, key=lambda x: x)`：

- 泛型函数，按 key 函数去重，保留首次出现顺序

## 文件 2：retrieval/vector_store/milvus.go

### milvusClient 接口

retrieval 层本地定义，只包含本层需要的方法，对齐 Python `pymilvus.MilvusClient` 的使用子集：

```go
type milvusClient interface {
    // 集合管理
    CreateCollection(ctx context.Context, option milvusclient.CreateCollectionOption, callOptions ...any) error
    DropCollection(ctx context.Context, option milvusclient.DropCollectionOption, callOptions ...any) error
    HasCollection(ctx context.Context, option milvusclient.HasCollectionOption, callOptions ...any) (bool, error)
    DescribeCollection(ctx context.Context, option milvusclient.DescribeCollectionOption, callOptions ...any) (*entity.Collection, error)
    ListCollections(ctx context.Context, option milvusclient.ListCollectionOption, callOptions ...any) ([]*entity.Collection, error)

    // 数据操作
    Insert(ctx context.Context, option milvusclient.InsertOption, callOptions ...any) (milvusclient.InsertResult, error)
    Delete(ctx context.Context, option milvusclient.DeleteOption, callOptions ...any) (milvusclient.DeleteResult, error)
    Flush(ctx context.Context, option milvusclient.FlushOption, callOptions ...any) error

    // 搜索
    Search(ctx context.Context, option milvusclient.SearchOption, callOptions ...any) ([]milvusclient.ResultSet, error)
    HybridSearch(ctx context.Context, option milvusclient.HybridSearchOption, callOptions ...any) ([]milvusclient.ResultSet, error)

    // 查询
    Query(ctx context.Context, option milvusclient.QueryOption, callOptions ...any) (milvusclient.QueryResult, error)

    // 索引
    DescribeIndex(ctx context.Context, option milvusclient.DescribeIndexOption, callOptions ...any) ([]index.Index, error)

    // 加载
    LoadCollection(ctx context.Context, option milvusclient.LoadCollectionOption, callOptions ...any) error
    GetLoadState(ctx context.Context, option milvusclient.GetLoadStateOption, callOptions ...any) (entity.LoadState, error)

    // 统计
    GetCollectionStats(ctx context.Context, option milvusclient.GetCollectionStatsOption, callOptions ...any) (map[string]string, error)

    // 数据库
    ListDatabases(ctx context.Context, option milvusclient.ListDatabaseOption, callOptions ...any) ([]string, error)
    CreateDatabase(ctx context.Context, option milvusclient.CreateDatabaseOption, callOptions ...any) error
    UseDatabase(ctx context.Context, option milvusclient.UseDatabaseOption, callOptions ...any) error

    // 关闭
    Close(ctx context.Context) error
}
```

适配器 `milvusClientAdapter` 委托给 `*milvusclient.Client`。

### MilvusVectorStore 结构体

对齐 Python `MilvusVectorStore.__init__`：

```go
type MilvusVectorStore struct {
    config            common.VectorStoreConfig
    collectionName    string
    milvusURI         string
    milvusToken       string
    textField         string                      // 默认 "content"
    vectorField       *vector_fields.MilvusVectorField
    sparseVectorField string                      // 默认 "sparse_vector"
    metadataField     string                      // 默认 "metadata"
    docIDField        string                      // 默认 "document_id"
    databaseName      string
    distanceMetric    string                      // 归一化后：IP/L2/COSINE
    constructConfig   map[string]any
    searchConfig      map[string]any
    client            milvusClient                // 惰性创建
    createClient      func(ctx context.Context, dbName, uri, token string) (milvusClient, error)
    collectionLoaded  bool
    mu                sync.RWMutex
}
```

### 方法清单

| 方法 | Python 对齐 | 说明 |
|------|------------|------|
| `NewMilvusVectorStore` | `__init__` | 构造函数，校验 vectorField，初始化 constructConfig/searchConfig/distanceMetric |
| `Client()` | `client` property | 返回底层 client |
| `DistanceMetric()` | `distance_metric` property | 返回距离度量 |
| `createClient` (包级函数) | `create_client` (static) | 创建 MilvusClient + 确保 database 存在 + use database |
| `getSearchParams` | `get_search_params` | efSearchFactor → ef = round(topK * efSearchFactor) 转换 |
| `CheckVectorField` | `check_vector_field` | 校验索引类型和配置一致性（has_collection → describe_index → CheckConfigsMatching） |
| `Add` | `add` | 批量写入（batch_size=128，每 100 条打日志，最后 flush） |
| `Search` | `search` | 向量搜索 + 分数归一化（L2/COSINE/IP 三种） |
| `SparseSearch` | `sparse_search` | BM25 稀疏搜索（传递文本给 SDK，BM25 函数自动分词） |
| `HybridSearch` | `hybrid_search` | 原生 HybridSearch（AnnSearchRequest + RRFRanker k=60） |
| `hybridSearchFallback` | `_hybrid_search_fallback` | 失败时分别搜索后 RRFFusion |
| `Delete` | `delete` | 按 ids 或 filterExpr 删除 + flush |
| `TableExists` | `table_exists` | 检查 collection 存在 |
| `DeleteTable` | `delete_table` | 删除 collection |
| `ensureLoaded` | `_ensure_loaded` | 确保 collection 已加载（has_collection → load_collection） |
| `normalizeMilvusHit` | `_normalize_milvus_hit` | 扁平化 SDK 返回的 entity 嵌套 |
| `milvusResultToSearchResults` | `_milvus_result_to_search_results` | 结果转换 + 分数归一化 + doc_id/chunk_id 元数据回填 |
| `Close` | `close` | 关闭（可选 release collection） |

### 过滤表达式构建

对齐 Python MilvusVectorStore 中 filter 构建逻辑：

- `filters` 为 `map[string]any` 时：拼接 `key == value`，字符串值用 `QueryExpr.SanitizeStr` 转义
- `filters` 为 `QueryExpr` 时：调用 `filters.ToExpr("milvus")`
- 使用 `foundation/store/query` 包的 `QueryExpr`

### 分数归一化

对齐 Python `_milvus_result_to_search_results` 中分数转换：

| mode | distanceMetric | Python | Go |
|------|---------------|--------|-----|
| vector | L2 | `convert_l2_squared(raw)` | `vector.ConvertL2Squared(raw, 0)` |
| vector | COSINE | `convert_cosine_similarity(raw)` | `vector.ConvertCosineSimilarity(raw)` |
| vector | IP | `convert_ip_similarity(raw)` | `vector.ConvertIPSimilarity(raw)` |
| sparse | - | `final_score = raw_score` | 直接使用原始分数 |
| hybrid | - | `final_score = raw_score` | 直接使用原始分数（RRF 已归一化） |

## 文件 3：retrieval/indexing/indexer/milvus.go

### MilvusIndexer 结构体

对齐 Python `MilvusIndexer.__init__`：

```go
type MilvusIndexer struct {
    milvusURI         string
    milvusToken       string
    textField         string                      // 默认 "content"
    vectorField       *vector_fields.MilvusVectorField
    sparseVectorField string                      // 默认 "sparse_vector"
    metadataField     string                      // 默认 "metadata"
    docIDField        string                      // 默认 "document_id"
    databaseName      string
    constructConfig   map[string]any
    searchConfig      map[string]any
    distanceMetric    string
    docIndexCallback  common.DocIndexCallback
    milvusAlias       string
    client            milvusClient                // 共享 client（ensureCollection 使用）
    createClient      func(ctx context.Context, dbName, uri, token string) (milvusClient, error)
}
```

### 构造函数 NewMilvusIndexer

对齐 Python `MilvusIndexer.__init__`：

1. 校验 vectorField 参数（必须为 string 或 MilvusVectorField，string 自动转 MilvusAUTO）
2. 校验 docIndexCallback（必须为 DocIndexCallback 类型）
3. 距离度量映射：`dot→ip`, `euclidean→l2`, `.upper()`
4. constructConfig / searchConfig 初始化（与 MilvusVectorStore 相同逻辑）
5. milvusAlias = `CreateMilvusAlias(alias, uri, token)`
6. 创建 client = `createClient(ctx, databaseName, milvusURI, milvusToken)`

### 方法清单

| 方法 | Python 对齐 | 说明 |
|------|------------|------|
| `NewMilvusIndexer` | `__init__` | 构造 + 校验 + client 创建 |
| `Client()` | `client` property | 返回底层 client |
| `DistanceMetric()` | `distance_metric` property | 返回距离度量 |
| `BuildIndex` | `build_index` | ensureCollection → 去重 doc_id → 嵌入 → 转换 TextChunk → 写入 |
| `UpdateIndex` | `update_index` | deleteIndex → flush → buildIndex |
| `DeleteIndex` | `delete_index` | 按 doc_id 过滤删除（filter_expr = `document_id == "xxx"`） |
| `IndexExists` | `index_exists` | 检查 collection 存在 |
| `GetIndexInfo` | `get_index_info` | 优先 getCollectionStats 获取 row_count + describeCollection 获取字段信息 |
| `ensureCollection` | `_ensure_collection` | 自动建表（完整 schema + index） |
| `Close` | `close` | 关闭 client |

### BuildIndex 流程（对齐 Python build_index）

1. `ensureCollection(collectionName, config, embedModel)` — 确保 collection 存在
2. 去重 doc_id — `client.Query(filter="document_id in [id1,id2,...]")`，重复则抛 `RETRIEVAL_INDEXING_ADD_DOC_RUNTIME_ERROR`
3. 嵌入 — vector/hybrid 类型调用 `ComputeChunkEmbeddings`，bm25 类型跳过
4. 转换 TextChunk → Milvus 字段 — `chunk_id` / `document_id` / `content` / `metadata` / 可选 `embedding`
5. 写入 — 创建 `MilvusVectorStore` 实例，调用 `Add(ctx, data)`
6. 日志 — `logger.Info("成功构建索引", index_name, chunk_count)`

### UpdateIndex 流程（对齐 Python update_index）

1. `deleteIndex(ctx, docID, config.IndexName)`
2. `client.Flush(collectionName)` — Milvus 最终一致性，必须 flush
3. `buildIndex(ctx, chunks, config, embedModel, opts...)`

### ensureCollection 流程（对齐 Python _ensure_collection）

这是最复杂的方法，完整对齐 Python 的 schema 构建：

1. 如果 collection 已存在，直接返回
2. 构建 schema（auto_id=True, enable_dynamic_field=False）：
   - `document_id` (VARCHAR 256) + INVERTED 索引
   - `pk` (INT64, is_primary=True, auto_id=True)
   - `chunk_id` (VARCHAR 256) + INVERTED 索引
   - `content` (VARCHAR 65535) — BM25/hybrid 时 enable_analyzer=true, analyzer_params={"tokenizer": "jieba"}
   - BM25/hybrid 时：`sparse_vector` (SPARSE_FLOAT_VECTOR) + `FunctionType.BM25` 函数 + SPARSE_INVERTED_INDEX(metric=BM25)
   - vector/hybrid 时：`embedding` (FLOAT_VECTOR, dim) + 索引（由 MilvusVectorField 决定 index_type + constructConfig）
   - `metadata` (JSON)
3. dimension 推断优先级：embedModel.Dimension → embedModel.EmbedQuery("X") 取 len → 都失败则报错
4. `client.CreateCollection(collectionName, schema, indexParams)`

### 与 Chroma 实现的差异点

| 差异 | ChromaIndexer/ChromaVectorStore | MilvusIndexer/MilvusVectorStore |
|------|-------------------------------|-------------------------------|
| 客户端类型 | `chromav2.Client` (本地持久化) | `milvusClient` (远程服务) |
| collection 创建 | `GetOrCreateCollection` (自动) | `ensureCollection` (需手动建 schema+index) |
| 去重方式 | `collection.Get(where=...)` | `client.Query(filter=...)` |
| 删除方式 | 先查后删（ChromaDB 限制） | 直接 filter 删除（Milvus 原生支持） |
| flush | 不需要 | updateIndex 中需要 flush（Milvus 最终一致性） |
| close | 不需要 | 需要（关闭 Milvus 连接） |
| 混合搜索 | 并发双搜 + 应用层 RRF | 原生 HybridSearch + RRF（失败 fallback 到应用层） |
| client 创建 | 每次 BuildIndex/DeleteIndex 创建 | NewMilvusIndexer 时创建一次，共享使用 |

## 测试策略

### utils/common_test.go

- `TestCreateMilvusAlias` — alias 非空直接返回 / 无 token 时用 "noauth" / 有 token 时用 MD5
- `TestDeduplicate` — 正常去重 / 空切片 / 全部重复 / key 函数自定义

### vector_store/milvus_test.go

通过 mock `milvusClient` 接口测试所有路径：

- `TestNewMilvusVectorStore` — 正常创建 / vectorField 校验 / 空 URI
- `TestMilvusVectorStore_Add` — 批量写入 / 每 100 条日志 / flush
- `TestMilvusVectorStore_Search` — L2/COSINE/IP 分数归一化 / 过滤表达式
- `TestMilvusVectorStore_SparseSearch` — BM25 搜索 / 失败返回空
- `TestMilvusVectorStore_HybridSearch` — 原生混合 / fallback 到分别搜索
- `TestMilvusVectorStore_Delete` — 按 ids / 按 filterExpr / flush
- `TestMilvusVectorStore_CheckVectorField` — 配置一致 / 配置不一致 / collection 不存在
- `TestMilvusVectorStore_GetSearchParams` — efSearchFactor → ef 转换
- `TestMilvusVectorStore_ensureLoaded` — 已加载 / 未加载
- `TestMilvusVectorStore_Close` — 正常关闭 / release collection

### indexer/milvus_test.go

通过 mock `milvusClient` + mock `MilvusVectorStore` 测试：

- `TestNewMilvusIndexer` — 正常创建 / vectorField 类型校验 / callback 校验
- `TestMilvusIndexer_BuildIndex` — 完整流程 / doc_id 重复报错 / embedModel 缺失报错
- `TestMilvusIndexer_UpdateIndex` — delete → flush → build 完整流程
- `TestMilvusIndexer_DeleteIndex` — 正常删除 / 无匹配 / MilvusException
- `TestMilvusIndexer_IndexExists` — 存在 / 不存在 / 异常
- `TestMilvusIndexer_GetIndexInfo` — 存在时返回 row_count + info / 不存在 / row_count 解析
- `TestMilvusIndexer_ensureCollection` — 已存在直接返回 / BM25 schema / vector schema / hybrid schema / dimension 推断

## 需要更新的现有文件

| 文件 | 更新内容 |
|------|---------|
| `retrieval/utils/doc.go` | 文件目录新增 `common.go` + `common_test.go` |
| `retrieval/vector_store/doc.go` | 文件目录新增 `milvus.go` + `milvus_test.go` |
| `retrieval/indexing/indexer/doc.go` | 文件目录新增 `milvus.go` + `milvus_test.go` |
| `IMPLEMENTATION_PLAN.md` | 13.3 状态 ☐ → ✅ |

## 不需要回填的内容

经检查 IMPLEMENTATION_PLAN.md 中 13.x 领域无 ⤵️/⤴️ 回填标记。13.3 实现不遗漏之前任何回填。
