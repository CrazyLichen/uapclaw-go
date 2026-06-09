# 4.8 MilvusVectorStore 实现设计

## 概述

实现领域4第4.8小节：基于 Milvus 的向量存储实现。使用官方 `milvus-sdk-go/v2` 作为 Milvus 客户端，实现 `BaseVectorStore` 接口的所有方法，并包含 Milvus 特有的索引子类型和距离转换工具函数。

**Python 参考**：`openjiuwen/core/foundation/store/vector/milvus_vector_store.py`

## 关键决策

| 决策项 | 选择 | 理由 |
|--------|------|------|
| Go Milvus SDK | 官方 `milvus-sdk-go/v2` | 官方维护，API 与 Python SDK 对应 |
| Schema 迁移范围 | 包含完整功能，迁移计算逻辑预留回填 | 迁移操作类型和计算函数属于 7.22/7.23 范畴 |
| Milvus 索引子类型 | 4.8 内一起实现 | create_collection 依赖索引配置 |
| 工具函数组织 | 统一放 `vector/utils.go` | 与 Python 保持一致 |
| 迁移操作类型 | 不单独建文件 | 7.22/7.23 实现时再引入 |
| 文件组织 | 方案 A：单文件核心实现 + 工具分离 | 与 Python 一一对应，便于维护对照 |
| Mock 方式 | 定义 `milvusClient` 接口抽象层 | milvus.Client 是具体类型无法直接 mock |

## 文件结构与产出清单

### 新增文件

| 文件 | 职责 | 对应 Python |
|------|------|-------------|
| `vector/utils.go` | 距离/相似度转换函数 | `vector/utils.py`（距离转换部分） |
| `vector/milvus.go` | MilvusVectorStore 结构体 + 接口实现 | `vector/milvus_vector_store.py` |
| `vector/utils_test.go` | 距离转换单元测试 | — |
| `vector/milvus_test.go` | MilvusVectorStore 单元测试（mock 客户端） | — |
| `vector/milvus_integration_test.go` | 集成测试（build tag: integration） | — |
| `vector_fields/milvus_fields.go` | Milvus 索引子类型 | `vector_fields/milvus_fields.py` |
| `vector_fields/milvus_fields_test.go` | Milvus 索引子类型测试 | — |

### 更新文件

| 文件 | 变更 |
|------|------|
| `vector/doc.go` | 文件目录树新增 utils.go、milvus.go |
| `vector_fields/doc.go` | 文件目录树新增 milvus_fields.go |
| `vector/base.go` | BaseVectorStore 接口补充 `UpdateSchema`、`UpdateCollectionMetadata`、`GetCollectionMetadata` 3 个方法 |
| `vector/base_test.go` | 补充新接口方法的测试 |

### 不在本次范围（预留回填 ⤵️）

- `vector/milvus.go` 中 `UpdateSchema` / `_executeMigration` 方法 — 仅签名 + `// TODO: ⤵️ 回填，待 7.22/7.23 实现后补全`
- `vector/utils.go` 中 schema 迁移相关函数（`compute_new_schema`、`build_transform_func_for_operations`、`_map_string_to_vector_data_type`）— 仅签名 + `// TODO: ⤵️ 回填`

## MilvusVectorStore 核心结构

### 结构体定义

```go
// MilvusVectorStore 基于 Milvus 的向量存储实现
type MilvusVectorStore struct {
    client             milvusClient           // 惰性创建的 Milvus 客户端（接口）
    milvusURI          string                 // Milvus 连接地址
    milvusToken        string                 // Milvus 认证令牌
    dbName             string                 // 数据库名（默认 "default"）
    collectionMetadata map[string]*collMeta   // 集合元数据缓存（距离度量等）
    collectionsLoaded  map[string]bool        // 已加载到内存的集合缓存
    mu                 sync.RWMutex           // 保护缓存字段的读写锁
}

// collMeta 集合元数据缓存
type collMeta struct {
    DistanceMetric DistanceMetricType  // 距离度量类型
    SchemaVersion string               // schema 版本
    // 后续 7.22/7.23 可扩展
}
```

### 构造函数

```go
// NewMilvusVectorStore 创建 MilvusVectorStore 实例
// 客户端惰性创建，初始化时不需要 Milvus 可用
func NewMilvusVectorStore(milvusURI, milvusToken, dbName string) *MilvusVectorStore
```

### 接口方法实现映射

| Go 方法 | Python 方法 | 核心逻辑 |
|---------|------------|---------|
| `CreateCollection` | `create_collection` | 构建 Milvus schema → CreateCollection → 设置索引参数 → 加载集合 |
| `DeleteCollection` | `delete_collection` | DropCollection → 清除缓存 |
| `CollectionExists` | `collection_exists` | HasCollection |
| `GetSchema` | `get_schema` | DescribeCollection → 转换为 CollectionSchema |
| `AddDocs` | `add_docs` | 分 batch 插入，Insert |
| `Search` | `search` | ensureLoaded → Search → 距离转换 → 返回 []VectorSearchResult |
| `DeleteDocsByIDs` | `delete_docs_by_ids` | Delete by PK |
| `DeleteDocsByFilters` | `delete_docs_by_filters` | buildFilterExpr → Delete by expr |
| `ListCollectionNames` | `list_collection_names` | ListCollections |
| `UpdateSchema` | `update_schema` | ⤵️ TODO: 预留签名，待 7.22/7.23 回填 |
| `UpdateCollectionMetadata` | `update_collection_metadata` | AlterCollectionProperties → 更新缓存 |
| `GetCollectionMetadata` | `get_collection_metadata` | 从缓存获取 → 若缺失则 DescribeCollection 获取 properties |

### BaseVectorStore 接口补充

Python 基类有但 Go 接口目前缺少的 3 个方法：

```go
type BaseVectorStore interface {
    // ... 已有的 9 个方法 ...

    // UpdateSchema 执行 schema 迁移操作
    // ⤵️ 预留：实际迁移逻辑待 7.22/7.23 实现后回填
    UpdateSchema(ctx context.Context, collectionName string, operations []any, opts ...Option) error

    // UpdateCollectionMetadata 更新集合元数据
    UpdateCollectionMetadata(ctx context.Context, collectionName string, metadata map[string]any, opts ...Option) error

    // GetCollectionMetadata 获取集合元数据
    GetCollectionMetadata(ctx context.Context, collectionName string, opts ...Option) (map[string]any, error)
}
```

> 注：`UpdateSchema` 签名中 `operations` 参数当前使用 `[]any` 占位，待 7.22 定义 `MigrationOperation` 正式类型后替换。

### 内部辅助方法

| Go 方法 | Python 方法 | 说明 |
|---------|------------|------|
| `mapFieldType` | `_map_field_type` | VectorDataType → milvus.DataType |
| `mapMilvusTypeToOurType` | `_map_milvus_type_to_our_type` | 反向映射 |
| `buildFilterExpr` | `_build_filter_expr` | map[string]any 过滤条件 → Milvus 表达式字符串（仅等值） |
| `ensureLoaded` | `_ensure_loaded` | 确保集合已 LoadCollection，使用缓存避免重复加载 |
| `getClient` | `client()` | 惰性获取/创建 milvus.Client |
| `Close` | `close()` | 关闭客户端连接 |

## 距离转换函数（vector/utils.go）

| Go 函数 | Python 函数 | 公式 |
|---------|------------|------|
| `ConvertL2Squared(rawScore, maxDist float64) float64` | `convert_l2_squared` | max(0, (maxDist - raw) / maxDist)，默认 maxDist=4.0 |
| `ConvertCosineSimilarity(rawScore float64) float64` | `convert_cosine_similarity` | (raw + 1) / 2，[-1,1] → [0,1] |
| `ConvertCosineDistance(rawScore float64) float64` | `convert_cosine_distance` | (2 - raw) / 2，[0,2] → [0,1] |
| `ConvertIPSimilarity(rawScore float64) float64` | `convert_ip_similarity` | clamp((raw + 1) / 2, 0, 1)，Milvus 用 |
| `ConvertIPDistance(rawScore float64) float64` | `convert_ip_distance` | clamp((2 - raw) / 2, 0, 1)，Chroma 用 |

- 5 个函数均为纯函数，无状态依赖
- `Search` 方法内部根据 `DistanceMetric` 选择对应转换函数对原始 score 归一化

## Milvus 索引子类型（vector_fields/milvus_fields.go）

### 类型层次

```
VectorField (基类，已有)
├── MilvusAUTO        // 自动选择索引
├── MilvusFLAT        // 暴力搜索
├── MilvusHNSW        // HNSW 索引
│   ├── MilvusHNSW_SQ   // SQ 量化
│   ├── MilvusHNSW_PQ   // PQ 量化
│   └── MilvusHNSW_PRQ  // PRQ 量化
├── MilvusIVF         // IVF 基类（非导出）
│   ├── MilvusIVFFLAT   // IVF + FLAT
│   ├── MilvusIVFSQ8    // IVF + SQ8
│   ├── MilvusIVFPQ     // IVF + PQ
│   └── MilvusIVFRABITQ // IVF + RABITQ
└── MilvusSCANN       // SCANN 索引（继承 IVF）
```

### vf 标签字段分配

| 类型 | construct 阶段字段 | search 阶段字段 |
|------|-------------------|----------------|
| `MilvusHNSW` | M, EfConstruction | EfSearchFactor |
| `MilvusHNSW_SQ/PQ/PRQ` | 同 HNSW + 量化参数 | 同 HNSW |
| `MilvusIVF` | Nlist | Nprobe |
| `MilvusIVFFLAT/SQ8/PQ/RABITQ` | 同 IVF + 量化参数 | 同 IVF |
| `MilvusSCANN` | 同 IVF + WithRawData | 同 IVF + ReorderK |
| `MilvusAUTO` / `MilvusFLAT` | 无额外字段 | 无额外字段 |

### Validate 校验

- `MilvusHNSW`：M 范围 [4, 64]，EfConstruction > 0，EfSearchFactor > 0
- `MilvusIVF`：Nlist > 0，Nprobe > 0
- `MilvusSCANN`：继承 IVF 校验 + ReorderK > 0（如果设置）

### 与 MilvusVectorStore 集成

`CreateCollection` 通过 Option 机制传入 `VectorField` 实例，`milvus.go` 内部调用 `ToDict(field, StageConstruct)` 获取索引创建参数，调用 `ToDict(field, StageSearch)` 获取搜索参数。

## 测试策略

### 单元测试（无外部依赖）

| 测试文件 | 覆盖内容 |
|---------|---------|
| `vector/utils_test.go` | 5 个距离转换函数的边界值和典型值测试 |
| `vector/milvus_test.go` | MilvusVectorStore 所有方法测试，通过 fakeMilvusClient mock |
| `vector_fields/milvus_fields_test.go` | 各索引子类型的创建、Validate、ToDict(construct/search) 测试 |
| `vector/base_test.go` | 补充 BaseVectorStore 新增 3 个接口方法的 mock 测试 |

### Milvus 客户端 Mock 方式

定义 `milvusClient` 接口抽象层：

```go
// milvusClient Milvus 客户端操作接口（用于解耦和测试）
type milvusClient interface {
    CreateCollection(ctx context.Context, opt milvus.CreateCollectionOption) error
    DropCollection(ctx context.Context, opt milvus.DropCollectionOption) error
    HasCollection(ctx context.Context, opt milvus.HasCollectionOption) (bool, error)
    DescribeCollection(ctx context.Context, opt milvus.DescribeCollectionOption) (*milvus.CollectionDescription, error)
    Insert(ctx context.Context, opt milvus.InsertOption) error
    Search(ctx context.Context, opt milvus.SearchOption) ([]milvus.SearchResult, error)
    Delete(ctx context.Context, opt milvus.DeleteOption) error
    ListCollections(ctx context.Context, opt milvus.ListCollectionsOption) ([]string, error)
    LoadCollection(ctx context.Context, opt milvus.LoadCollectionOption) error
    AlterCollectionProperties(ctx context.Context, opt milvus.AlterCollectionPropertiesOption) error
    // ... 其他需要的方法
}
```

- `MilvusVectorStore` 持有 `milvusClient` 接口字段
- 生产代码通过 `getClient()` 创建真实 `milvus.Client` 并包装为接口
- 测试代码注入 `fakeMilvusClient` 实现

### 集成测试（build tag: integration）

| 测试文件 | 覆盖内容 |
|---------|---------|
| `vector/milvus_integration_test.go` | 连接真实 Milvus 实例，完整 CRUD + Search 流程 |

- 使用 `//go:build integration` 标签隔离
- 从环境变量读取：`MILVUS_URI`、`MILVUS_TOKEN`
- `go test ./...` 默认不执行

### 不在本次测试范围

- `UpdateSchema` 的实际逻辑测试（⤵️ 待 7.22/7.23 回填后补充）
- Schema 迁移的端到端测试

## 日志同步

对照 Python `milvus_vector_store.py` 中的 `store_logger` 调用，在 Go 代码等价位置补充日志：
- 使用 `logger.ComponentCommon`（store 属于基础设施层）
- Python 级别映射：`logger.debug` → `logger.Debug`，`logger.info` → `logger.Info`，`logger.warning` → `logger.Warn`，`logger.error` → `logger.Error`
- 异常路径日志包含 `event_type=LLM_CALL_ERROR`、`method`、`model_provider` 等上下文字段
