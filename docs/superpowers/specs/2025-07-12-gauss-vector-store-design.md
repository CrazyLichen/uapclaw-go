# GaussVectorStore 设计文档

> 日期：2025-07-12
> 对应实现计划：领域四 4.10 — GaussVectorStore GaussDB 向量实现
> Python 参考：`openjiuwen/core/foundation/store/vector/gauss_vector_store.py`

## 1. 概述

GaussVectorStore 是 BaseVectorStore 接口的 GaussDB 向量存储实现，基于 pgx/v5 驱动连接 GaussDB，使用 DiskANN 向量索引提供向量搜索能力。

## 2. 决策汇总

| 项 | 决策 | 理由 |
|----|------|------|
| 驱动 | pgx/v5 | Go 生态最成熟的高性能 PG 驱动，原生异步、连接池内置、活跃维护 |
| 并发模型 | pgxpool 连接池 | 天然支持并发安全，与 BaseVectorStore 的 ctx 传参完美配合 |
| 元数据 | 纯内存缓存（对齐 Python） | 与 Python 版行为一致，重启后从 information_schema 回查 |
| SQL 安全 | 参数化查询 + pgx Identifier.Sanitize() | DML 用 $1/$2 参数化，DDL 表名列名用标识符转义 |
| 索引配置 | 新增 GaussDiskANN 索引子类型 | 与 Milvus 索引子类型组织方式一致，上层可通过 WithVectorField() 传入 |
| 过滤 | 等值过滤 + 类型感知 + 参数化 | 对齐 Python 等值范围，Go 端根据值类型生成正确 SQL |
| UpdateSchema | stub 返回 ErrNotImplemented | 等 7.22/7.23 统一实现 Schema 迁移工具函数后回填 |
| 距离转换 | 复用 ConvertCosineDistance / ConvertL2Squared | GaussDB COSINE 距离语义与 ChromaDB 一致，直接复用 |
| 测试 | 抽象 dbClient 接口 + fakeDBClient | 与 Milvus 的 milvusClient 接口 + fakeMilvusClient 模式一致 |
| 代码组织 | 单文件 gauss.go | 与 Milvus/Chroma 单文件组织方式一致，代码量约 400-500 行可控 |
| 日志 | ComponentCommon，对齐 Python store_logger | store 属于基础设施层 |
| 错误 | 包级 sentinel error + fmt.Errorf 包装 | 保留原始错误链，便于排障 |

## 3. 结构体与构造函数

### 3.1 GaussVectorStore

```go
// GaussVectorStore 基于 GaussDB 的向量存储实现
type GaussVectorStore struct {
    pool               dbClient           // 抽象接口，非直接 *pgxpool.Pool
    connConfig         string             // 连接字符串，惰性建池用
    collectionMetadata map[string]*collMeta  // 集合元数据内存缓存
    mu                 sync.RWMutex
    createPool         func(ctx context.Context, connString string) (dbClient, error) // 可注入，测试用
}

// collMeta 集合元数据缓存
type collMeta struct {
    DistanceMetric string
    VectorField    string
    VectorDim      int
    SchemaVersion  string
}
```

### 3.2 构造函数

```go
// NewGaussVectorStore 创建 GaussVectorStore 实例
// 连接池惰性创建，首次调用 getClient() 时才建池
func NewGaussVectorStore(connString string) *GaussVectorStore
```

- `connConfig` 存连接字符串
- `createPool` 默认为 `defaultCreatePool`（调用 `pgxpool.New`），测试可注入 fake
- `getClient()` 双重检查锁惰性建池

### 3.3 dbClient 抽象接口

```go
// dbClient 封装 pgxpool.Pool 的查询方法
type dbClient interface {
    Exec(ctx context.Context, sql string, args ...any) (pgCommandTag, error)
    Query(ctx context.Context, sql string, args ...any) (pgRows, error)
    QueryRow(ctx context.Context, sql string, args ...any) pgRow
    Close()
}

type pgCommandTag interface {
    RowsAffected() int64
}

type pgRows interface {
    Next() bool
    Scan(dest ...any) error
    Close()
}

type pgRow interface {
    Scan(dest ...any) error
}
```

`*pgxpool.Pool` 天然实现 `dbClient`，无需手写适配器。

## 4. 类型映射

### 4.1 正向映射（VectorDataType → PG 类型）

| VectorDataType | PG 类型 | 备注 |
|---------------|---------|------|
| VectorDataTypeVarchar | VARCHAR(N) | 需拼接 MaxLength，默认 65535 |
| VectorDataTypeFloatVector | FLOATVECTOR(N) | 需拼接 Dim |
| VectorDataTypeInt64 | BIGINT | |
| VectorDataTypeInt32 | INTEGER | |
| VectorDataTypeInt16 | SMALLINT | |
| VectorDataTypeInt8 | SMALLINT | PG 无 INT8 |
| VectorDataTypeFloat | REAL | |
| VectorDataTypeDouble | DOUBLE PRECISION | |
| VectorDataTypeBool | BOOLEAN | |
| VectorDataTypeJSON | JSONB | |
| VectorDataTypeArray | — | 不支持，返回 ErrUnsupportedType |

### 4.2 反向映射（PG 类型 → VectorDataType）

| PG 类型 | VectorDataType |
|---------|---------------|
| varchar, character varying | VectorDataTypeVarchar |
| floatvector | VectorDataTypeFloatVector |
| bigint, int8 | VectorDataTypeInt64 |
| integer, int, int4 | VectorDataTypeInt32 |
| smallint, int2 | VectorDataTypeInt16 |
| real, float4 | VectorDataTypeFloat |
| double precision, float8 | VectorDataTypeDouble |
| boolean, bool | VectorDataTypeBool |
| jsonb, json | VectorDataTypeJSON |
| 其他 | VectorDataTypeVarchar（fallback） |

反向映射从 `information_schema.columns` 读取 `data_type` 和 `character_maximum_length`/`numeric_precision` 还原完整 FieldSchema。

### 4.3 距离度量映射

| 输入 | PG 操作符类别 | 说明 |
|------|-------------|------|
| COSINE（默认） | cosine | |
| L2 | l2 | |

### 4.4 距离 → 相似度转换

```go
func gaussNormalizeScore(rawScore float64, metric string) float64 {
    switch strings.ToUpper(metric) {
    case "COSINE":
        // GaussDB <-> 返回余弦距离 [0,2]，与 ChromaDB 语义一致
        return ConvertCosineDistance(rawScore)
    case "L2":
        return ConvertL2Squared(rawScore)
    default:
        return ConvertCosineDistance(rawScore)
    }
}
```

## 5. 核心方法实现

### 5.1 CreateCollection

1. 校验 Schema（必须有主键字段和向量字段）
2. 从 Option 取 DistanceMetric（默认 COSINE）和 VectorField（GaussDiskANN 索引配置）
3. 用 `pgx.Identifier{collectionName}.Sanitize()` 转义表名
4. 构建 `CREATE TABLE` SQL：每个字段根据 `mapFieldTypeToPG` 生成列定义
5. 执行 `CREATE TABLE`
6. 构建 `CREATE INDEX ... USING GSDISKANN` SQL
7. 执行 `CREATE INDEX`
8. 缓存 collMeta
9. 记录 Info 日志

```sql
-- 示例 DDL
CREATE TABLE "collection_name" (
    "id" VARCHAR(256) PRIMARY KEY,
    "content" VARCHAR(65535),
    "embedding" FLOATVECTOR(1024)
);

CREATE INDEX "idx_collection_name_embedding" ON "collection_name"
    USING GSDISKANN ("embedding" cosine)
    WITH (enable_pq = true, pg_nseg = 128, pg_nclus = 16,
          num_parallels = 32, quantization_type = 'lvq', subgraph_count = 1);
```

### 5.2 Search

1. 从缓存取 collMeta
2. 构建带 `<->` 距离操作符的 SELECT
3. 可选 WHERE 子句（等值过滤，参数化）
4. ORDER BY distance LIMIT topK
5. 用 `gaussNormalizeScore` 转换距离为相似度
6. 映射行数据到 `[]VectorSearchResult`

```sql
SELECT *, "embedding" <-> $1::floatvector AS distance
FROM "collection_name"
WHERE "status" = $2
ORDER BY distance
LIMIT 10;
```

### 5.3 AddDocs

1. 按 BatchSize（默认 128）分批
2. 每批构建参数化 INSERT
3. 向量字段值格式化为 `[1.0,2.0,3.0]::floatvector`

### 5.4 DeleteDocsByIDs

```sql
DELETE FROM "collection" WHERE "id" = ANY($1)
```

用 `= ANY($1)` 传入数组参数，比 Python 版 IN 拼接更安全。

### 5.5 DeleteDocsByFilters

```sql
DELETE FROM "collection" WHERE "key" = $1 AND "key2" = $2
```

### 5.6 CollectionExists

```sql
SELECT 1 FROM information_schema.tables
WHERE table_schema = 'public' AND table_name = $1
```

### 5.7 GetSchema

```sql
SELECT column_name, data_type, character_maximum_length, numeric_precision
FROM information_schema.columns
WHERE table_schema = 'public' AND table_name = $1
```

### 5.8 ListCollectionNames

```sql
SELECT table_name FROM information_schema.tables
WHERE table_schema = 'public' AND table_type = 'BASE TABLE'
```

### 5.9 GetCollectionMetadata / UpdateCollectionMetadata

- **GetCollectionMetadata**：优先缓存 → 未命中则从 `information_schema.columns` 回查 floatvector 列 → 更新缓存
- **UpdateCollectionMetadata**：更新内存缓存中的 collMeta

### 5.10 UpdateSchema

返回 `ErrNotImplemented`，等 7.22/7.23 统一实现。

### 5.11 DeleteCollection

```sql
DROP TABLE IF EXISTS "collection_name"
```

清除内存缓存中对应的 collMeta。

## 6. GaussDiskANN 索引子类型

### 6.1 结构体

在 `vector_fields` 包中新增：

```go
// GaussDiskANN GaussDB DiskANN 向量索引配置
type GaussDiskANN struct {
    VectorField

    // EnablePQ 是否启用产品量化
    EnablePQ bool `vf:"construct,keepzero"`
    // PGNseg 产品量化段数
    PGNseg int `vf:"construct,keepzero"`
    // PGNclus 产品量化聚类数
    PGNclus int `vf:"construct,keepzero"`
    // NumParallels 并行度
    NumParallels int `vf:"construct,keepzero"`
    // QuantizationType 量化类型（lvq/pq）
    QuantizationType string `vf:"construct"`
    // SubgraphCount 子图数量
    SubgraphCount int `vf:"construct,keepzero"`
}
```

### 6.2 枚举新增

- `DatabaseTypeGauss` 加入 `DatabaseType` 枚举
- `IndexTypeDiskANN` 加入 `IndexType` 枚举

### 6.3 构造函数与默认值

```go
func NewGaussDiskANN() *GaussDiskANN {
    return &GaussDiskANN{
        VectorField: VectorField{
            DatabaseType: DatabaseTypeGauss,
            IndexType:    IndexTypeDiskANN,
        },
        EnablePQ:           true,
        PGNseg:             128,
        PGNclus:            16,
        NumParallels:       32,
        QuantizationType:   "lvq",
        SubgraphCount:      1,
    }
}
```

### 6.4 Validate

```go
func (g *GaussDiskANN) Validate() error  // PGNseg/PGNclus/NumParallels/SubgraphCount > 0, QuantizationType in (lvq, pq)
```

## 7. 过滤构建

`buildGaussFilterClause` 遍历 `filters map[string]any`，根据值类型生成参数化条件：

| 值类型 | SQL 模式 | 参数传递 |
|--------|---------|---------|
| string | `"key" = $N` | 直接参数化 |
| int/int64 | `"key" = $N` | 直接参数化 |
| float64 | `"key" = $N` | 直接参数化 |
| bool | `"key" = $N` | 直接参数化 |

多条件用 `AND` 连接。返回 WHERE 子句字符串 + 参数列表。

## 8. 测试策略

### 8.1 单元测试（gauss_test.go）

使用 fakeDBClient 注入，覆盖：

| 测试场景 | 方法 |
|---------|------|
| 创建集合成功/失败 | CreateCollection |
| 集合已存在 | CreateCollection |
| 集合存在/不存在 | CollectionExists |
| 获取 Schema / 集合不存在 | GetSchema |
| 插入文档（含分批）/ 空文档 | AddDocs |
| 搜索（COSINE/L2/带过滤/无结果） | Search |
| 按 ID 删除 / 空列表 | DeleteDocsByIDs |
| 按过滤删除 | DeleteDocsByFilters |
| 删除集合成功/不存在 | DeleteCollection |
| 列出集合名 | ListCollectionNames |
| 获取/更新元数据 / 缓存命中 | GetCollectionMetadata, UpdateCollectionMetadata |
| UpdateSchema 返回 ErrNotImplemented | UpdateSchema |
| 连接池惰性创建 / 创建失败 | getClient |
| SQL 注入标识符转义 | CreateCollection |
| 类型映射正反向 | mapFieldTypeToPG, mapPGTypeToOurType |
| GaussDiskANN Validate | Validate 正常/异常 |
| 距离转换映射 | gaussNormalizeScore |

### 8.2 集成测试（gauss_integration_test.go）

`//go:build integration`

需要真实 GaussDB 实例，测试 CreateCollection → AddDocs → Search → Delete 全流程。

### 8.3 fake 实现

```go
type fakeDBClient struct {
    execFn     func(ctx context.Context, sql string, args ...any) (pgCommandTag, error)
    queryFn    func(ctx context.Context, sql string, args ...any) (pgRows, error)
    queryRowFn func(ctx context.Context, sql string, args ...any) pgRow
}

type fakeCommandTag struct{ rowsAffected int64 }
type fakeRows struct { rows [][]any; idx int }
type fakeRow struct { vals []any; err error }
```

派生变体 fake 覆盖各种错误路径（execErrorFake、queryErrorFake 等）。

## 9. 日志

组件常量：`logger.ComponentCommon`

| 方法 | 日志点 | 级别 | 关键字段 |
|------|--------|------|---------|
| CreateCollection | 成功 | Info | `collection_name`, `vector_field`, `distance_metric` |
| CreateCollection | 失败 | Error | `collection_name`, `event_type=LLM_CALL_ERROR`, `.Err(err)` |
| AddDocs | 完成 | Info | `collection_name`, `doc_count`, `batch_size` |
| AddDocs | 失败 | Error | `collection_name`, `event_type=LLM_CALL_ERROR`, `.Err(err)` |
| Search | 完成 | Info | `collection_name`, `top_k`, `distance_metric`, `result_count` |
| Search | 失败 | Error | `collection_name`, `event_type=LLM_CALL_ERROR`, `.Err(err)` |
| DeleteDocsByIDs | 完成 | Info | `collection_name`, `id_count` |
| DeleteDocsByFilters | 完成 | Info | `collection_name`, `filter_count` |
| DeleteCollection | 完成 | Info | `collection_name` |
| GetSchema | 失败 | Error | `collection_name`, `.Err(err)` |
| UpdateCollectionMetadata | 失败 | Error | `collection_name`, `.Err(err)` |
| getClient | 建池失败 | Error | `.Err(err)` |

## 10. 错误定义

```go
var (
    ErrCollectionNotFound      = errors.New("集合不存在")
    ErrCollectionAlreadyExists = errors.New("集合已存在")
    ErrSchemaValidation        = errors.New("Schema 校验失败")
    ErrUnsupportedType         = errors.New("不支持的类型")
    ErrNotImplemented          = errors.New("方法未实现")
    ErrNoPrimaryKey            = errors.New("Schema 缺少主键字段")
    ErrNoVectorField           = errors.New("Schema 缺少向量字段")
)
```

所有从 pgx 返回的错误用 `fmt.Errorf("方法名: %w", err)` 包装，保留原始错误链。

## 11. 文件清单

| 文件 | 位置 | 职责 |
|------|------|------|
| gauss.go | `internal/agentcore/store/vector/` | GaussVectorStore 主体 + dbClient 接口 + fake + 类型映射 + 过滤构建 |
| gauss_test.go | `internal/agentcore/store/vector/` | 单元测试 |
| gauss_integration_test.go | `internal/agentcore/store/vector/` | 集成测试 |
| gauss_fields.go | `internal/agentcore/store/vector_fields/` | GaussDiskANN 索引子类型 |
| gauss_fields_test.go | `internal/agentcore/store/vector_fields/` | GaussDiskANN 单元测试 |
| base.go（修改） | `internal/agentcore/store/vector_fields/` | 新增 DatabaseTypeGauss、IndexTypeDiskANN 枚举值 |

## 12. 与 Python 版差异对照

| 维度 | Python | Go |
|------|--------|-----|
| 驱动 | psycopg2（同步） | pgx/v5 pgxpool（异步连接池） |
| SQL 安全 | 直接拼接，有注入风险 | 参数化 + 标识符转义 |
| 连接管理 | 懒加载单连接 + autocommit | pgxpool 连接池 + ctx 超时控制 |
| 元数据 | 纯内存 | 纯内存（对齐） |
| 索引参数 | 硬编码 | GaussDiskANN 结构体 + vf 标签 |
| 过滤 | 字符串拼接 | 类型感知 + 参数化 |
| DeleteDocsByIDs | IN (...) 拼接 | = ANY($1) 参数化 |
| UpdateSchema | 临时表迁移 | stub（等 7.22/7.23） |
| 异步 | 假 async（方法声明 async，内部同步） | 真异步（pgxpool 原生异步） |
