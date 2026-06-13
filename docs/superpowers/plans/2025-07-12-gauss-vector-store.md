# GaussVectorStore 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 GaussVectorStore — 基于 pgx/v5 + pgxpool 的 GaussDB 向量存储，完成 BaseVectorStore 接口的 12 个方法。

**Architecture:** 在 `internal/agentcore/store/vector/gauss.go` 中实现 GaussVectorStore 结构体，通过抽象 dbClient 接口解耦 pgxpool 依赖，使用 pgx.Identifier.Sanitize() 做标识符转义、参数化查询防 SQL 注入。在 `internal/agentcore/store/vector_fields/gauss_fields.go` 中新增 GaussDiskANN 索引子类型。UpdateSchema stub 等 7.22/7.23 回填。

**Tech Stack:** pgx/v5, pgxpool, vector_fields vf 标签反射, exception 包错误码

---

## 文件结构

| 操作 | 文件 | 职责 |
|------|------|------|
| 修改 | `go.mod` / `go.sum` | 新增 `github.com/jackc/pgx/v5` 依赖 |
| 修改 | `internal/agentcore/store/vector_fields/base.go` | 新增 `DatabaseTypeGauss`、`IndexTypeDiskANN` 枚举值 |
| 创建 | `internal/agentcore/store/vector_fields/gauss_fields.go` | GaussDiskANN 索引子类型 |
| 创建 | `internal/agentcore/store/vector_fields/gauss_fields_test.go` | GaussDiskANN 单元测试 |
| 修改 | `internal/agentcore/store/vector_fields/doc.go` | 更新文件目录和核心类型索引 |
| 创建 | `internal/agentcore/store/vector/gauss.go` | GaussVectorStore 主体实现 |
| 创建 | `internal/agentcore/store/vector/gauss_test.go` | GaussVectorStore 单元测试 |
| 创建 | `internal/agentcore/store/vector/gauss_integration_test.go` | GaussVectorStore 集成测试 |
| 修改 | `internal/agentcore/store/vector/doc.go` | 更新文件目录和核心类型索引 |
| 修改 | `IMPLEMENTATION_PLAN.md` | 更新 4.10 状态 |

---

### Task 1: 添加 pgx/v5 依赖

**Files:**
- Modify: `go.mod`
- Modify: `go.sum`

- [ ] **Step 1: 添加 pgx/v5 依赖**

```bash
cd /home/opensource/uap-claw-go && go get github.com/jackc/pgx/v5
```

- [ ] **Step 2: 验证依赖添加成功**

```bash
cd /home/opensource/uap-claw-go && go mod tidy && grep "jackc/pgx/v5" go.mod
```

Expected: 输出包含 `github.com/jackc/pgx/v5` 行

- [ ] **Step 3: Commit**

```bash
git add go.mod go.sum && git commit -m "chore: 添加 pgx/v5 依赖（GaussVectorStore 使用）"
```

---

### Task 2: 新增 DatabaseTypeGauss 和 IndexTypeDiskANN 枚举值

**Files:**
- Modify: `internal/agentcore/store/vector_fields/base.go:37-44` (DatabaseType 枚举)
- Modify: `internal/agentcore/store/vector_fields/base.go:49-62` (IndexType 枚举)
- Modify: `internal/agentcore/store/vector_fields/base.go:77-89` (枚举字符串数组)

- [ ] **Step 1: 在 DatabaseType 枚举中新增 DatabaseTypeGauss**

在 `base.go` 的 `DatabaseType` const 块中，`DatabaseTypePG` 之后添加：

```go
// DatabaseTypeGauss GaussDB 向量数据库
DatabaseTypeGauss
```

- [ ] **Step 2: 在 IndexType 枚举中新增 IndexTypeDiskANN**

在 `base.go` 的 `IndexType` const 块中，`IndexTypeSCANN` 之后添加：

```go
// IndexTypeDiskANN DiskANN 索引（GaussDB 专用）
IndexTypeDiskANN
```

- [ ] **Step 3: 更新枚举字符串数组**

在 `databaseTypeStrings` 中 `"pg"` 之后添加：

```go
"gauss",
```

在 `indexTypeStrings` 中 `"scann"` 之后添加：

```go
"diskann",
```

- [ ] **Step 4: 运行已有测试确认无破坏**

```bash
cd /home/opensource/uap-claw-go && go test ./internal/agentcore/store/vector_fields/... -v
```

Expected: 所有测试 PASS

- [ ] **Step 5: Commit**

```bash
git add internal/agentcore/store/vector_fields/base.go && git commit -m "feat(vector_fields): 新增 DatabaseTypeGauss 和 IndexTypeDiskANN 枚举值"
```

---

### Task 3: 实现 GaussDiskANN 索引子类型

**Files:**
- Create: `internal/agentcore/store/vector_fields/gauss_fields.go`
- Create: `internal/agentcore/store/vector_fields/gauss_fields_test.go`

- [ ] **Step 1: 编写 GaussDiskANN 测试**

创建 `internal/agentcore/store/vector_fields/gauss_fields_test.go`：

```go
package vector_fields

import "testing"

func TestNewGaussDiskANN(t *testing.T) {
	g := NewGaussDiskANN("embedding")
	if g.DatabaseType != DatabaseTypeGauss {
		t.Errorf("DatabaseType = %v, want %v", g.DatabaseType, DatabaseTypeGauss)
	}
	if g.IndexType != IndexTypeDiskANN {
		t.Errorf("IndexType = %v, want %v", g.IndexType, IndexTypeDiskANN)
	}
	if g.VectorFieldName != "embedding" {
		t.Errorf("VectorFieldName = %v, want embedding", g.VectorFieldName)
	}
	if !g.EnablePQ {
		t.Error("EnablePQ 应为 true")
	}
	if g.PGNseg != 128 {
		t.Errorf("PGNseg = %v, want 128", g.PGNseg)
	}
	if g.PGNclus != 16 {
		t.Errorf("PGNclus = %v, want 16", g.PGNclus)
	}
	if g.NumParallels != 32 {
		t.Errorf("NumParallels = %v, want 32", g.NumParallels)
	}
	if g.QuantizationType != "lvq" {
		t.Errorf("QuantizationType = %v, want lvq", g.QuantizationType)
	}
	if g.SubgraphCount != 1 {
		t.Errorf("SubgraphCount = %v, want 1", g.SubgraphCount)
	}
}

func TestGaussDiskANN_Validate_正常(t *testing.T) {
	g := NewGaussDiskANN("embedding")
	if err := g.Validate(); err != nil {
		t.Errorf("Validate() error = %v, want nil", err)
	}
}

func TestGaussDiskANN_Validate_PGNseg无效(t *testing.T) {
	g := NewGaussDiskANN("embedding")
	g.PGNseg = 0
	if err := g.Validate(); err == nil {
		t.Error("PGNseg=0 时 Validate 应返回错误")
	}
}

func TestGaussDiskANN_Validate_PGNclus无效(t *testing.T) {
	g := NewGaussDiskANN("embedding")
	g.PGNclus = 0
	if err := g.Validate(); err == nil {
		t.Error("PGNclus=0 时 Validate 应返回错误")
	}
}

func TestGaussDiskANN_Validate_NumParallels无效(t *testing.T) {
	g := NewGaussDiskANN("embedding")
	g.NumParallels = 0
	if err := g.Validate(); err == nil {
		t.Error("NumParallels=0 时 Validate 应返回错误")
	}
}

func TestGaussDiskANN_Validate_QuantizationType无效(t *testing.T) {
	g := NewGaussDiskANN("embedding")
	g.QuantizationType = "invalid"
	if err := g.Validate(); err == nil {
		t.Error("QuantizationType='invalid' 时 Validate 应返回错误")
	}
}

func TestGaussDiskANN_Validate_SubgraphCount无效(t *testing.T) {
	g := NewGaussDiskANN("embedding")
	g.SubgraphCount = 0
	if err := g.Validate(); err == nil {
		t.Error("SubgraphCount=0 时 Validate 应返回错误")
	}
}

func TestGaussDiskANN_ToDict_construct(t *testing.T) {
	g := NewGaussDiskANN("embedding")
	dict := ToDict(g, StageConstruct)
	if _, ok := dict["EnablePQ"]; !ok {
		t.Error("construct 阶段应包含 EnablePQ")
	}
	if _, ok := dict["PGNseg"]; !ok {
		t.Error("construct 阶段应包含 PGNseg")
	}
	if _, ok := dict["QuantizationType"]; !ok {
		t.Error("construct 阶段应包含 QuantizationType")
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

```bash
cd /home/opensource/uap-claw-go && go test ./internal/agentcore/store/vector_fields/... -run TestNewGaussDiskANN -v
```

Expected: 编译失败（NewGaussDiskANN 未定义）

- [ ] **Step 3: 实现 GaussDiskANN**

创建 `internal/agentcore/store/vector_fields/gauss_fields.go`：

```go
package vector_fields

import "fmt"

// ──────────────────────────── 结构体 ────────────────────────────

// GaussDiskANN GaussDB DiskANN 向量索引配置。
// DiskANN 是 GaussDB 的磁盘近似最近邻索引，支持大规模向量检索。
//
// 对应 Python: gauss_vector_store.py 中的 GSDISKANN 索引参数
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

// ──────────────────────────── 导出函数 ────────────────────────────

// NewGaussDiskANN 创建 GaussDB DiskANN 索引配置，使用默认参数。
// fieldName 为向量字段名。
func NewGaussDiskANN(fieldName string) *GaussDiskANN {
	return &GaussDiskANN{
		VectorField: VectorField{
			DatabaseType:    DatabaseTypeGauss,
			IndexType:       IndexTypeDiskANN,
			VectorFieldName: fieldName,
		},
		EnablePQ:           true,
		PGNseg:             128,
		PGNclus:            16,
		NumParallels:       32,
		QuantizationType:   "lvq",
		SubgraphCount:      1,
	}
}

// Validate 校验 GaussDiskANN 参数。
func (g *GaussDiskANN) Validate() error {
	if g.PGNseg <= 0 {
		return fmt.Errorf("PGNseg 必须大于 0，当前值: %d", g.PGNseg)
	}
	if g.PGNclus <= 0 {
		return fmt.Errorf("PGNclus 必须大于 0，当前值: %d", g.PGNclus)
	}
	if g.NumParallels <= 0 {
		return fmt.Errorf("NumParallels 必须大于 0，当前值: %d", g.NumParallels)
	}
	if g.QuantizationType != "lvq" && g.QuantizationType != "pq" {
		return fmt.Errorf("QuantizationType 必须为 lvq 或 pq，当前值: %s", g.QuantizationType)
	}
	if g.SubgraphCount <= 0 {
		return fmt.Errorf("SubgraphCount 必须大于 0，当前值: %d", g.SubgraphCount)
	}
	return nil
}
```

- [ ] **Step 4: 运行测试确认通过**

```bash
cd /home/opensource/uap-claw-go && go test ./internal/agentcore/store/vector_fields/... -v
```

Expected: 所有测试 PASS

- [ ] **Step 5: Commit**

```bash
git add internal/agentcore/store/vector_fields/gauss_fields.go internal/agentcore/store/vector_fields/gauss_fields_test.go && git commit -m "feat(vector_fields): 实现 GaussDiskANN 索引子类型"
```

---

### Task 4: 更新 vector_fields/doc.go

**Files:**
- Modify: `internal/agentcore/store/vector_fields/doc.go`

- [ ] **Step 1: 更新文件目录和核心类型索引**

在 doc.go 文件目录的 `pg_fields.go` 行后添加：

```
//	└── gauss_fields.go    # GaussDB DiskANN 索引子类型
```

在核心类型/接口索引中添加：

```
//	GaussDiskANN    — GaussDB DiskANN 索引配置（EnablePQ, PGNseg, PGNclus, NumParallels, QuantizationType, SubgraphCount）
```

在 DatabaseType 枚举说明中更新为 `（Milvus, Chroma, PG, Gauss）`，IndexType 更新为 `（AUTO, HNSW, FLAT, IVF, SCANN, DiskANN）`。

- [ ] **Step 2: Commit**

```bash
git add internal/agentcore/store/vector_fields/doc.go && git commit -m "docs(vector_fields): 更新 doc.go 添加 GaussDiskANN"
```

---

### Task 5: 实现 GaussVectorStore 主体（gauss.go）

这是最大的 Task，按照源码规范分步实现。每步代码都给出完整内容。

**Files:**
- Create: `internal/agentcore/store/vector/gauss.go`

- [ ] **Step 5.1: 创建 gauss.go 骨架 — 结构体、接口、构造函数、错误定义**

创建 `internal/agentcore/store/vector/gauss.go`，写入以下内容：

```go
package vector

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/store/vector_fields"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// gaussCollMeta GaussDB 集合元数据缓存
type gaussCollMeta struct {
	// DistanceMetric 距离度量类型
	DistanceMetric string
	// VectorField 向量字段名
	VectorField string
	// VectorDim 向量维度
	VectorDim int
	// SchemaVersion schema 版本
	SchemaVersion string
}

// dbClient 封装 pgxpool.Pool 的查询方法，用于依赖注入和测试 mock。
// *pgxpool.Pool 天然实现此接口。
type dbClient interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Close()
}

// GaussVectorStore 基于 GaussDB 的向量存储实现。
//
// 使用 pgx/v5 pgxpool 连接池与 GaussDB 通信，支持 DiskANN 向量索引。
// 客户端惰性创建，初始化时不需要数据库可用。
// 元数据仅存内存缓存，进程重启后从 information_schema 回查。
//
// 对应 Python: vector/gauss_vector_store.py (GaussVectorStore)
type GaussVectorStore struct {
	// pool 数据库连接池
	pool dbClient
	// connConfig 连接字符串
	connConfig string
	// collectionMetadata 集合元数据内存缓存
	collectionMetadata map[string]*gaussCollMeta
	// mu 读写锁，保护连接池和缓存
	mu sync.RWMutex
	// createPool 连接池创建函数，用于依赖注入和测试
	createPool func(ctx context.Context, connString string) (dbClient, error)
}

// ──────────────────────────── 枚举 ────────────────────────────

// gaussDistanceMetric GaussDB 支持的距离度量
type gaussDistanceMetric string

const (
	gaussMetricCosine gaussDistanceMetric = "cosine"
	gaussMetricL2     gaussDistanceMetric = "l2"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// gaussDefaultDistanceMetric GaussDB 默认距离度量
	gaussDefaultDistanceMetric = "COSINE"
	// gaussDefaultBatchSize GaussDB 默认批量插入大小
	gaussDefaultBatchSize = 128
	// gaussDefaultPGNseg 默认产品量化段数
	gaussDefaultPGNseg = 128
	// gaussDefaultPGNclus 默认产品量化聚类数
	gaussDefaultPGNclus = 16
	// gaussDefaultNumParallels 默认并行度
	gaussDefaultNumParallels = 32
	// gaussDefaultQuantizationType 默认量化类型
	gaussDefaultQuantizationType = "lvq"
	// gaussDefaultSubgraphCount 默认子图数量
	gaussDefaultSubgraphCount = 1
)

// ──────────────────────────── 全局变量 ────────────────────────────

// gaussLogComponent 日志组件
var gaussLogComponent = logger.ComponentAgentCore

// ──────────────────────────── 导出函数 ────────────────────────────

// NewGaussVectorStore 创建 GaussVectorStore 实例。
// 连接池惰性创建，初始化时不需要数据库可用。
// connString 格式：postgres://user:password@host:port/database
func NewGaussVectorStore(connString string) *GaussVectorStore {
	return &GaussVectorStore{
		connConfig:         connString,
		collectionMetadata: make(map[string]*gaussCollMeta),
		createPool:         defaultCreatePool,
	}
}

// Close 关闭数据库连接池。
func (s *GaussVectorStore) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pool != nil {
		s.pool.Close()
		s.pool = nil
	}
}

// CreateCollection 创建向量集合。
// 从 schema 构建建表 SQL，在向量字段上创建 DiskANN 索引。
//
// 对应 Python: GaussVectorStore.create_collection()
func (s *GaussVectorStore) CreateCollection(ctx context.Context, collectionName string, schema *CollectionSchema, opts ...Option) error {
	o := newOptions(opts...)

	// 校验 Schema
	if err := gaussValidateSchema(schema); err != nil {
		return err
	}

	// 检查集合是否已存在
	exists, err := s.CollectionExists(ctx, collectionName)
	if err != nil {
		return fmt.Errorf("CreateCollection: %w", err)
	}
	if exists {
		logger.Info(gaussLogComponent).
			Str("collection_name", collectionName).
			Msg("集合已存在，跳过创建")
		return nil
	}

	// 获取数据库连接池
	pool, err := s.getClient(ctx)
	if err != nil {
		return fmt.Errorf("CreateCollection: %w", err)
	}

	// 获取距离度量和向量字段
	distanceMetric := o.DistanceMetric
	if distanceMetric == "" {
		distanceMetric = gaussDefaultDistanceMetric
	}
	pgMetric := mapDistanceMetricToPG(distanceMetric)

	vectorFields := schema.GetVectorFields()
	if len(vectorFields) == 0 {
		return exception.BuildError(exception.StatusStoreVectorSchemaInvalid,
			exception.WithParam("error_msg", "Schema 缺少向量字段"),
		)
	}
	vf := vectorFields[0]

	// 解析 DiskANN 索引配置
	diskann := s.resolveDiskANNConfig(vf.Name, o)

	// 构建 CREATE TABLE SQL
	createSQL, err := gaussBuildCreateTableSQL(collectionName, schema)
	if err != nil {
		return fmt.Errorf("CreateCollection: %w", err)
	}

	// 执行建表
	if _, err := pool.Exec(ctx, createSQL); err != nil {
		logger.Error(gaussLogComponent).Err(err).
			Str("collection_name", collectionName).
			Str("event_type", "LLM_CALL_ERROR").
			Msg("创建集合失败")
		return fmt.Errorf("CreateCollection: %w", err)
	}

	// 构建 CREATE INDEX SQL
	indexName := fmt.Sprintf("idx_%s_%s", collectionName, vf.Name)
	indexSQL := gaussBuildCreateIndexSQL(indexName, collectionName, vf.Name, pgMetric, diskann)

	if _, err := pool.Exec(ctx, indexSQL); err != nil {
		logger.Error(gaussLogComponent).Err(err).
			Str("collection_name", collectionName).
			Str("event_type", "LLM_CALL_ERROR").
			Msg("创建 DiskANN 索引失败")
		return fmt.Errorf("CreateCollection: %w", err)
	}

	// 缓存元数据
	s.mu.Lock()
	s.collectionMetadata[collectionName] = &gaussCollMeta{
		DistanceMetric: strings.ToUpper(distanceMetric),
		VectorField:    vf.Name,
		VectorDim:      vf.Dim,
		SchemaVersion:  "1",
	}
	s.mu.Unlock()

	logger.Info(gaussLogComponent).
		Str("collection_name", collectionName).
		Str("vector_field", vf.Name).
		Str("distance_metric", distanceMetric).
		Msg("创建集合成功")

	return nil
}

// DeleteCollection 删除向量集合。
//
// 对应 Python: GaussVectorStore.delete_collection()
func (s *GaussVectorStore) DeleteCollection(ctx context.Context, collectionName string, opts ...Option) error {
	pool, err := s.getClient(ctx)
	if err != nil {
		return fmt.Errorf("DeleteCollection: %w", err)
	}

	tableName := pgx.Identifier{collectionName}.Sanitize()
	sql := fmt.Sprintf("DROP TABLE IF EXISTS %s", tableName)

	if _, err := pool.Exec(ctx, sql); err != nil {
		logger.Error(gaussLogComponent).Err(err).
			Str("collection_name", collectionName).
			Str("event_type", "LLM_CALL_ERROR").
			Msg("删除集合失败")
		return fmt.Errorf("DeleteCollection: %w", err)
	}

	// 清除缓存
	s.mu.Lock()
	delete(s.collectionMetadata, collectionName)
	s.mu.Unlock()

	logger.Info(gaussLogComponent).
		Str("collection_name", collectionName).
		Msg("删除集合成功")

	return nil
}

// CollectionExists 检查集合是否存在。
//
// 对应 Python: GaussVectorStore.collection_exists()
func (s *GaussVectorStore) CollectionExists(ctx context.Context, collectionName string, opts ...Option) (bool, error) {
	pool, err := s.getClient(ctx)
	if err != nil {
		return false, fmt.Errorf("CollectionExists: %w", err)
	}

	var exists bool
	err = pool.QueryRow(ctx,
		"SELECT EXISTS(SELECT 1 FROM information_schema.tables WHERE table_schema = 'public' AND table_name = $1)",
		collectionName,
	).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("CollectionExists: %w", err)
	}
	return exists, nil
}

// GetSchema 获取集合的 Schema。
//
// 对应 Python: GaussVectorStore.get_schema()
func (s *GaussVectorStore) GetSchema(ctx context.Context, collectionName string, opts ...Option) (*CollectionSchema, error) {
	pool, err := s.getClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("GetSchema: %w", err)
	}

	rows, err := pool.Query(ctx,
		"SELECT column_name, data_type, character_maximum_length, numeric_precision FROM information_schema.columns WHERE table_schema = 'public' AND table_name = $1 ORDER BY ordinal_position",
		collectionName,
	)
	if err != nil {
		logger.Error(gaussLogComponent).Err(err).
			Str("collection_name", collectionName).
			Msg("获取 Schema 失败")
		return nil, fmt.Errorf("GetSchema: %w", err)
	}
	defer rows.Close()

	schema, err := NewCollectionSchema()
	if err != nil {
		return nil, err
	}

	for rows.Next() {
		var colName, dataType string
		var charMaxLen *int
		var numPrec *int
		if err := rows.Scan(&colName, &dataType, &charMaxLen, &numPrec); err != nil {
			return nil, fmt.Errorf("GetSchema: %w", err)
		}

		dt := mapPGTypeToOurType(dataType)
		fieldOpts := []FieldOption{}
		if charMaxLen != nil && dt == VectorDataTypeVarchar {
			fieldOpts = append(fieldOpts, WithMaxLength(*charMaxLen))
		}
		if dt == VectorDataTypeFloatVector {
			// 尝试从 numeric_precision 或缓存获取 dim
			dim := 0
			if numPrec != nil {
				dim = *numPrec
			}
			if dim > 0 {
				fieldOpts = append(fieldOpts, WithDim(dim))
			} else {
				// 从缓存获取 dim
				s.mu.RLock()
				if meta, ok := s.collectionMetadata[collectionName]; ok {
					dim = meta.VectorDim
				}
				s.mu.RUnlock()
				if dim > 0 {
					fieldOpts = append(fieldOpts, WithDim(dim))
				}
			}
		}

		f, err := NewFieldSchema(colName, dt, fieldOpts...)
		if err != nil {
			return nil, fmt.Errorf("GetSchema: %w", err)
		}
		if _, err := schema.AddField(f); err != nil {
			return nil, fmt.Errorf("GetSchema: %w", err)
		}
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("GetSchema: %w", err)
	}

	return schema, nil
}

// AddDocs 添加文档到集合。
// 按 BatchSize 分批参数化插入。
//
// 对应 Python: GaussVectorStore.add_docs()
func (s *GaussVectorStore) AddDocs(ctx context.Context, collectionName string, docs []map[string]any, opts ...Option) error {
	if len(docs) == 0 {
		return nil
	}

	o := newOptions(opts...)
	batchSize := o.BatchSize
	if batchSize <= 0 {
		batchSize = gaussDefaultBatchSize
	}

	pool, err := s.getClient(ctx)
	if err != nil {
		return fmt.Errorf("AddDocs: %w", err)
	}

	tableName := pgx.Identifier{collectionName}.Sanitize()

	for i := 0; i < len(docs); i += batchSize {
		end := i + batchSize
		if end > len(docs) {
			end = len(docs)
		}
		batch := docs[i:end]

		if err := gaussInsertBatch(ctx, pool, tableName, collectionName, batch, s); err != nil {
			logger.Error(gaussLogComponent).Err(err).
				Str("collection_name", collectionName).
				Int("doc_count", len(docs)).
				Int("batch_size", batchSize).
				Str("event_type", "LLM_CALL_ERROR").
				Msg("插入文档失败")
			return fmt.Errorf("AddDocs: %w", err)
		}
	}

	logger.Info(gaussLogComponent).
		Str("collection_name", collectionName).
		Int("doc_count", len(docs)).
		Int("batch_size", batchSize).
		Msg("插入文档成功")

	return nil
}

// Search 向量相似度搜索。
// 使用 GaussDB 的 <-> 距离操作符 + ORDER BY + LIMIT。
//
// 对应 Python: GaussVectorStore.search()
func (s *GaussVectorStore) Search(ctx context.Context, collectionName string, queryVector []float64, vectorField string, topK int, filters map[string]any, opts ...Option) ([]VectorSearchResult, error) {
	if topK <= 0 {
		topK = 5
	}

	pool, err := s.getClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("Search: %w", err)
	}

	// 获取元数据
	meta := s.getOrInitCollMeta(ctx, collectionName, vectorField)

	distanceMetric := gaussDefaultDistanceMetric
	if meta != nil && meta.DistanceMetric != "" {
		distanceMetric = meta.DistanceMetric
	}
	pgMetric := mapDistanceMetricToPG(distanceMetric)

	// 构建查询 SQL
	tableName := pgx.Identifier{collectionName}.Sanitize()
	vectorCol := pgx.Identifier{vectorField}.Sanitize()

	// 格式化查询向量为 floatvector 字面量
	vecStr := gaussFormatVector(queryVector)

	selectCols := "*"
	distanceCol := fmt.Sprintf("%s <-> '%s'::floatvector AS distance", vectorCol, vecStr)

	var whereClause string
	var args []any
	if len(filters) > 0 {
		whereClause, args = gaussBuildFilterClause(filters)
		whereClause = " WHERE " + whereClause
	}

	sql := fmt.Sprintf("SELECT %s, %s FROM %s%s ORDER BY distance LIMIT %d",
		selectCols, distanceCol, tableName, whereClause, topK)

	rows, err := pool.Query(ctx, sql, args...)
	if err != nil {
		logger.Error(gaussLogComponent).Err(err).
			Str("collection_name", collectionName).
			Str("event_type", "LLM_CALL_ERROR").
			Msg("搜索失败")
		return nil, fmt.Errorf("Search: %w", err)
	}
	defer rows.Close()

	// 获取列描述
	fieldDescriptions := rows.FieldDescriptions()

	var results []VectorSearchResult
	for rows.Next() {
		// 动态扫描所有列
		values := make([]any, len(fieldDescriptions))
		valuePtrs := make([]any, len(fieldDescriptions))
		for i := range values {
			valuePtrs[i] = &values[i]
		}
		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, fmt.Errorf("Search: %w", err)
		}

		fields := make(map[string]any)
		var rawDistance float64
		for i, fd := range fieldDescriptions {
			colName := fd.Name
			if colName == "distance" {
				// pgx 扫描 float 类型的 distance 列
				switch v := values[i].(type) {
				case float64:
					rawDistance = v
				case float32:
					rawDistance = float64(v)
				}
			} else {
				fields[colName] = gaussConvertValue(values[i])
			}
		}

		score := gaussNormalizeScore(rawDistance, distanceMetric)
		results = append(results, VectorSearchResult{
			Score:  score,
			Fields: fields,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("Search: %w", err)
	}

	resultCount := len(results)
	logger.Info(gaussLogComponent).
		Str("collection_name", collectionName).
		Int("top_k", topK).
		Str("distance_metric", distanceMetric).
		Int("result_count", resultCount).
		Msg("搜索完成")

	return results, nil
}

// DeleteDocsByIDs 按 ID 删除文档。
// 使用 = ANY($1) 参数化删除。
//
// 对应 Python: GaussVectorStore.delete_docs_by_ids()
func (s *GaussVectorStore) DeleteDocsByIDs(ctx context.Context, collectionName string, ids []string, opts ...Option) error {
	if len(ids) == 0 {
		return nil
	}

	pool, err := s.getClient(ctx)
	if err != nil {
		return fmt.Errorf("DeleteDocsByIDs: %w", err)
	}

	tableName := pgx.Identifier{collectionName}.Sanitize()

	// 获取主键字段名
	pkField, err := s.getPrimaryKeyField(ctx, collectionName)
	if err != nil {
		return fmt.Errorf("DeleteDocsByIDs: %w", err)
	}
	pkCol := pgx.Identifier{pkField}.Sanitize()

	sql := fmt.Sprintf("DELETE FROM %s WHERE %s = ANY($1)", tableName, pkCol)
	if _, err := pool.Exec(ctx, sql, ids); err != nil {
		logger.Error(gaussLogComponent).Err(err).
			Str("collection_name", collectionName).
			Str("event_type", "LLM_CALL_ERROR").
			Msg("按 ID 删除文档失败")
		return fmt.Errorf("DeleteDocsByIDs: %w", err)
	}

	logger.Info(gaussLogComponent).
		Str("collection_name", collectionName).
		Int("id_count", len(ids)).
		Msg("按 ID 删除文档成功")

	return nil
}

// DeleteDocsByFilters 按标量字段过滤条件删除文档。
//
// 对应 Python: GaussVectorStore.delete_docs_by_filters()
func (s *GaussVectorStore) DeleteDocsByFilters(ctx context.Context, collectionName string, filters map[string]any, opts ...Option) error {
	if len(filters) == 0 {
		return nil
	}

	pool, err := s.getClient(ctx)
	if err != nil {
		return fmt.Errorf("DeleteDocsByFilters: %w", err)
	}

	tableName := pgx.Identifier{collectionName}.Sanitize()
	whereClause, args := gaussBuildFilterClause(filters)

	sql := fmt.Sprintf("DELETE FROM %s WHERE %s", tableName, whereClause)
	if _, err := pool.Exec(ctx, sql, args...); err != nil {
		logger.Error(gaussLogComponent).Err(err).
			Str("collection_name", collectionName).
			Str("event_type", "LLM_CALL_ERROR").
			Msg("按过滤条件删除文档失败")
		return fmt.Errorf("DeleteDocsByFilters: %w", err)
	}

	logger.Info(gaussLogComponent).
		Str("collection_name", collectionName).
		Int("filter_count", len(filters)).
		Msg("按过滤条件删除文档成功")

	return nil
}

// ListCollectionNames 列出所有集合名称。
//
// 对应 Python: GaussVectorStore.list_collection_names()
func (s *GaussVectorStore) ListCollectionNames(ctx context.Context) ([]string, error) {
	pool, err := s.getClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("ListCollectionNames: %w", err)
	}

	rows, err := pool.Query(ctx,
		"SELECT table_name FROM information_schema.tables WHERE table_schema = 'public' AND table_type = 'BASE TABLE'",
	)
	if err != nil {
		return nil, fmt.Errorf("ListCollectionNames: %w", err)
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("ListCollectionNames: %w", err)
		}
		names = append(names, name)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ListCollectionNames: %w", err)
	}

	return names, nil
}

// UpdateSchema 执行 Schema 迁移操作。
// ⤵️ 预留：实际迁移逻辑待 7.22/7.23 实现后回填。
//
// 对应 Python: GaussVectorStore.update_schema()
func (s *GaussVectorStore) UpdateSchema(ctx context.Context, collectionName string, operations []any, opts ...Option) error {
	return exception.BuildError(exception.StatusStoreVectorSchemaInvalid,
		exception.WithParam("error_msg", "UpdateSchema 未实现，待 7.22/7.23 回填"),
	)
}

// UpdateCollectionMetadata 更新集合元数据。
//
// 对应 Python: GaussVectorStore.update_collection_metadata()
func (s *GaussVectorStore) UpdateCollectionMetadata(ctx context.Context, collectionName string, metadata map[string]any, opts ...Option) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	meta, ok := s.collectionMetadata[collectionName]
	if !ok {
		meta = &gaussCollMeta{}
		s.collectionMetadata[collectionName] = meta
	}

	if v, ok := metadata["distance_metric"].(string); ok {
		meta.DistanceMetric = v
	}
	if v, ok := metadata["vector_field"].(string); ok {
		meta.VectorField = v
	}
	if v, ok := metadata["vector_dim"].(int); ok {
		meta.VectorDim = v
	}
	if v, ok := metadata["schema_version"].(string); ok {
		meta.SchemaVersion = v
	}

	return nil
}

// GetCollectionMetadata 获取集合元数据。
// 优先缓存，未命中则从 information_schema 回查。
//
// 对应 Python: GaussVectorStore.get_collection_metadata()
func (s *GaussVectorStore) GetCollectionMetadata(ctx context.Context, collectionName string, opts ...Option) (map[string]any, error) {
	s.mu.RLock()
	meta, ok := s.collectionMetadata[collectionName]
	s.mu.RUnlock()

	if ok {
		return map[string]any{
			"distance_metric": meta.DistanceMetric,
			"vector_field":    meta.VectorField,
			"vector_dim":      meta.VectorDim,
			"schema_version":  meta.SchemaVersion,
		}, nil
	}

	// 从数据库回查
	pool, err := s.getClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("GetCollectionMetadata: %w", err)
	}

	var colName, dataType string
	err = pool.QueryRow(ctx,
		"SELECT column_name, data_type FROM information_schema.columns WHERE table_schema = 'public' AND table_name = $1 AND data_type = 'floatvector' LIMIT 1",
		collectionName,
	).Scan(&colName, &dataType)
	if err != nil {
		logger.Error(gaussLogComponent).Err(err).
			Str("collection_name", collectionName).
			Msg("获取集合元数据失败")
		return nil, fmt.Errorf("GetCollectionMetadata: %w", err)
	}

	// 更新缓存
	newMeta := &gaussCollMeta{
		VectorField: colName,
	}
	s.mu.Lock()
	s.collectionMetadata[collectionName] = newMeta
	s.mu.Unlock()

	return map[string]any{
		"vector_field": newMeta.VectorField,
	}, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// defaultCreatePool 默认连接池创建函数
func defaultCreatePool(ctx context.Context, connString string) (dbClient, error) {
	config, err := pgxpool.ParseConfig(connString)
	if err != nil {
		return nil, fmt.Errorf("解析连接字符串失败: %w", err)
	}
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("创建连接池失败: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("连接池 Ping 失败: %w", err)
	}
	return pool, nil
}

// getClient 惰性获取数据库连接池，双重检查锁
func (s *GaussVectorStore) getClient(ctx context.Context) (dbClient, error) {
	s.mu.RLock()
	if s.pool != nil {
		s.mu.RUnlock()
		return s.pool, nil
	}
	s.mu.RUnlock()

	s.mu.Lock()
	defer s.mu.Unlock()
	// 双重检查
	if s.pool != nil {
		return s.pool, nil
	}

	pool, err := s.createPool(ctx, s.connConfig)
	if err != nil {
		logger.Error(gaussLogComponent).Err(err).
			Msg("创建连接池失败")
		return nil, err
	}
	s.pool = pool
	return pool, nil
}

// getOrInitCollMeta 获取或初始化集合元数据
func (s *GaussVectorStore) getOrInitCollMeta(ctx context.Context, collectionName string, vectorField string) *gaussCollMeta {
	s.mu.RLock()
	meta, ok := s.collectionMetadata[collectionName]
	s.mu.RUnlock()
	if ok {
		return meta
	}

	// 初始化默认元数据
	s.mu.Lock()
	defer s.mu.Unlock()
	// 双重检查
	if meta, ok := s.collectionMetadata[collectionName]; ok {
		return meta
	}

	meta = &gaussCollMeta{
		DistanceMetric: gaussDefaultDistanceMetric,
		VectorField:    vectorField,
	}
	s.collectionMetadata[collectionName] = meta
	return meta
}

// resolveDiskANNConfig 从 Option 解析 DiskANN 配置，未指定则使用默认值
func (s *GaussVectorStore) resolveDiskANNConfig(fieldName string, o Options) *vector_fields.GaussDiskANN {
	if o.VectorField != nil {
		if diskann, ok := o.VectorField.(*vector_fields.GaussDiskANN); ok {
			return diskann
		}
	}
	return vector_fields.NewGaussDiskANN(fieldName)
}

// getPrimaryKeyField 获取集合的主键字段名
func (s *GaussVectorStore) getPrimaryKeyField(ctx context.Context, collectionName string) (string, error) {
	// GaussDB 不存储主键约束名映射到我们的 FieldSchema.IsPrimary，
	// 默认使用 "id" 作为主键字段名（与 Python 版行为一致）
	return "id", nil
}

// gaussValidateSchema 校验 Schema 是否满足 GaussDB 向量存储要求
func gaussValidateSchema(schema *CollectionSchema) error {
	if schema.GetPrimaryKeyField() == nil {
		return exception.BuildError(exception.StatusStoreVectorSchemaInvalid,
			exception.WithParam("error_msg", "Schema 缺少主键字段"),
		)
	}
	if len(schema.GetVectorFields()) == 0 {
		return exception.BuildError(exception.StatusStoreVectorSchemaInvalid,
			exception.WithParam("error_msg", "Schema 缺少向量字段"),
		)
	}
	return nil
}

// mapFieldTypeToPG 将 VectorDataType 映射为 GaussDB/PostgreSQL 类型字符串
func mapFieldTypeToPG(dt VectorDataType) (string, error) {
	switch dt {
	case VectorDataTypeVarchar:
		return "VARCHAR", nil
	case VectorDataTypeFloatVector:
		return "FLOATVECTOR", nil
	case VectorDataTypeInt64:
		return "BIGINT", nil
	case VectorDataTypeInt32:
		return "INTEGER", nil
	case VectorDataTypeInt16:
		return "SMALLINT", nil
	case VectorDataTypeInt8:
		return "SMALLINT", nil
	case VectorDataTypeFloat:
		return "REAL", nil
	case VectorDataTypeDouble:
		return "DOUBLE PRECISION", nil
	case VectorDataTypeBool:
		return "BOOLEAN", nil
	case VectorDataTypeJSON:
		return "JSONB", nil
	case VectorDataTypeArray:
		return "", fmt.Errorf("GaussDB 向量存储不支持 ARRAY 类型")
	default:
		return "", fmt.Errorf("不支持的类型: %d", dt)
	}
}

// mapPGTypeToOurType 将 GaussDB/PostgreSQL 类型映射为 VectorDataType
func mapPGTypeToOurType(pgType string) VectorDataType {
	switch strings.ToLower(pgType) {
	case "varchar", "character varying":
		return VectorDataTypeVarchar
	case "floatvector":
		return VectorDataTypeFloatVector
	case "bigint", "int8":
		return VectorDataTypeInt64
	case "integer", "int", "int4":
		return VectorDataTypeInt32
	case "smallint", "int2":
		return VectorDataTypeInt16
	case "real", "float4":
		return VectorDataTypeFloat
	case "double precision", "float8":
		return VectorDataTypeDouble
	case "boolean", "bool":
		return VectorDataTypeBool
	case "jsonb", "json":
		return VectorDataTypeJSON
	default:
		return VectorDataTypeVarchar
	}
}

// mapDistanceMetricToPG 将距离度量映射为 GaussDB 索引操作符类别
func mapDistanceMetricToPG(metric string) gaussDistanceMetric {
	switch strings.ToUpper(metric) {
	case "L2":
		return gaussMetricL2
	case "COSINE":
		return gaussMetricCosine
	default:
		return gaussMetricCosine
	}
}

// gaussNormalizeScore 将 GaussDB 原始距离转换为归一化相似度 [0,1]
// COSINE: GaussDB <-> 返回余弦距离 [0,2]，等价于 ChromaDB 语义，用 ConvertCosineDistance
// L2: 用 ConvertL2Squared
func gaussNormalizeScore(rawScore float64, metric string) float64 {
	switch strings.ToUpper(metric) {
	case "COSINE":
		return ConvertCosineDistance(rawScore)
	case "L2":
		return ConvertL2Squared(rawScore, 4.0)
	default:
		return ConvertCosineDistance(rawScore)
	}
}

// gaussBuildCreateTableSQL 构建 CREATE TABLE SQL
func gaussBuildCreateTableSQL(collectionName string, schema *CollectionSchema) (string, error) {
	tableName := pgx.Identifier{collectionName}.Sanitize()

	var colDefs []string
	for _, f := range schema.Fields() {
		pgType, err := mapFieldTypeToPG(f.DType)
		if err != nil {
			return "", err
		}

		// 拼接类型参数
		switch f.DType {
		case VectorDataTypeVarchar:
			maxLen := f.MaxLength
			if maxLen <= 0 {
				maxLen = defaultMaxLength
			}
			pgType = fmt.Sprintf("VARCHAR(%d)", maxLen)
		case VectorDataTypeFloatVector:
			pgType = fmt.Sprintf("FLOATVECTOR(%d)", f.Dim)
		}

		colName := pgx.Identifier{f.Name}.Sanitize()
		colDef := fmt.Sprintf("%s %s", colName, pgType)

		if f.IsPrimary {
			colDef += " PRIMARY KEY"
		}
		if f.DefaultValue != nil {
			colDef += fmt.Sprintf(" DEFAULT '%v'", f.DefaultValue)
		}

		colDefs = append(colDefs, colDef)
	}

	return fmt.Sprintf("CREATE TABLE %s (%s)", tableName, strings.Join(colDefs, ", ")), nil
}

// gaussBuildCreateIndexSQL 构建 CREATE INDEX ... USING GSDISKANN SQL
func gaussBuildCreateIndexSQL(indexName, collectionName, vectorField string, metric gaussDistanceMetric, diskann *vector_fields.GaussDiskANN) string {
	idxName := pgx.Identifier{indexName}.Sanitize()
	tableName := pgx.Identifier{collectionName}.Sanitize()
	vectorCol := pgx.Identifier{vectorField}.Sanitize()

	return fmt.Sprintf(
		"CREATE INDEX %s ON %s USING GSDISKANN (%s %s) WITH (enable_pq = %t, pg_nseg = %d, pg_nclus = %d, num_parallels = %d, quantization_type = '%s', subgraph_count = %d)",
		idxName, tableName, vectorCol, string(metric),
		diskann.EnablePQ, diskann.PGNseg, diskann.PGNclus,
		diskann.NumParallels, diskann.QuantizationType, diskann.SubgraphCount,
	)
}

// gaussFormatVector 将 float64 切片格式化为 floatvector 字面量 [1.0,2.0,3.0]
func gaussFormatVector(vec []float64) string {
	parts := make([]string, len(vec))
	for i, v := range vec {
		parts[i] = fmt.Sprintf("%g", v)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

// gaussBuildFilterClause 构建参数化 WHERE 子句（等值过滤，类型感知）
// 返回 WHERE 子句（不含 WHERE 关键字）和参数列表
func gaussBuildFilterClause(filters map[string]any) (string, []any) {
	var conditions []string
	var args []any
	paramIdx := 1

	for key, val := range filters {
		colName := pgx.Identifier{key}.Sanitize()
		conditions = append(conditions, fmt.Sprintf("%s = $%d", colName, paramIdx))
		args = append(args, val)
		paramIdx++
	}

	return strings.Join(conditions, " AND "), args
}

// gaussInsertBatch 执行一批文档的参数化 INSERT
func gaussInsertBatch(ctx context.Context, pool dbClient, tableName, collectionName string, docs []map[string]any, store *GaussVectorStore) error {
	if len(docs) == 0 {
		return nil
	}

	// 从第一个文档获取列名（假设所有文档字段一致）
	var colNames []string
	for k := range docs[0] {
		colNames = append(colNames, k)
	}

	// 构建列名列表
	sanitizedCols := make([]string, len(colNames))
	for i, col := range colNames {
		sanitizedCols[i] = pgx.Identifier{col}.Sanitize()
	}

	// 构建每行的占位符
	rowPlaceholder := make([]string, len(colNames))
	for i := range colNames {
		rowPlaceholder[i] = fmt.Sprintf("$%d", i+1)
	}

	// 逐行插入（简化实现，后续可优化为多行 VALUES）
	for _, doc := range docs {
		values := make([]any, len(colNames))
		for i, col := range colNames {
			val := doc[col]
			// float64 切片转为 floatvector 字面量
			if vec, ok := val.([]float64); ok {
				val = gaussFormatVector(vec)
			}
			values[i] = val
		}

		sql := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
			tableName,
			strings.Join(sanitizedCols, ", "),
			strings.Join(rowPlaceholder, ", "),
		)

		if _, err := pool.Exec(ctx, sql, values...); err != nil {
			return err
		}
	}

	return nil
}

// gaussConvertValue 转换 pgx 返回的值为 Go 原生类型
func gaussConvertValue(v any) any {
	switch val := v.(type) {
	case []byte:
		return string(val)
	case float32:
		return float64(val)
	case int16:
		return int64(val)
	case int32:
		return int64(val)
	case int64:
		return val
	default:
		return val
	}
}
```

- [ ] **Step 5.2: 验证编译通过**

```bash
cd /home/opensource/uap-claw-go && go build ./internal/agentcore/store/vector/...
```

Expected: 编译成功，无错误

- [ ] **Step 5.3: Commit**

```bash
git add internal/agentcore/store/vector/gauss.go && git commit -m "feat(vector): 实现 GaussVectorStore 主体（12 个 BaseVectorStore 方法）"
```

---

### Task 6: 实现 GaussVectorStore 单元测试

**Files:**
- Create: `internal/agentcore/store/vector/gauss_test.go`

- [ ] **Step 6.1: 编写 fakeDBClient 和单元测试**

创建 `internal/agentcore/store/vector/gauss_test.go`：

```go
package vector

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/store/vector_fields"
)

// ──────────────────────────── 结构体 ────────────────────────────

// fakeDBClient 用于测试的数据库客户端模拟
type fakeDBClient struct {
	execFn      func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	queryFn     func(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	queryRowFn  func(ctx context.Context, sql string, args ...any) pgx.Row
	closeCalled atomic.Bool
}

// fakeRows 用于测试的行结果模拟
type fakeRows struct {
	rows   [][]any
	idx    int
	closed bool
}

// fakeRow 用于测试的单行结果模拟
type fakeRow struct {
	vals []any
	err  error
}

// ──────────────────────────── 导出函数 ────────────────────────────

func newFakeDBClient() *fakeDBClient {
	return &fakeDBClient{
		execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.NewCommandTag("OK"), nil
		},
		queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &fakeRows{}, nil
		},
		queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &fakeRow{}
		},
	}
}

func (f *fakeDBClient) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	return f.execFn(ctx, sql, args...)
}

func (f *fakeDBClient) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return f.queryFn(ctx, sql, args...)
}

func (f *fakeDBClient) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return f.queryRowFn(ctx, sql, args...)
}

func (f *fakeDBClient) Close() {
	f.closeCalled.Store(true)
}

// fakeRows 实现 pgx.Rows 接口

func (r *fakeRows) Close()                         { r.closed = true }
func (r *fakeRows) Err() error                     { return nil }
func (r *fakeRows) CommandTag() pgconn.CommandTag  { return pgconn.NewCommandTag("") }
func (r *fakeRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (r *fakeRows) Next() bool {
	if r.idx < len(r.rows) {
		r.idx++
		return true
	}
	return false
}
func (r *fakeRows) Scan(dest ...any) error {
	if r.idx == 0 || r.idx > len(r.rows) {
		return fmt.Errorf("no more rows")
	}
	row := r.rows[r.idx-1]
	for i, d := range dest {
		if i < len(row) {
			// 使用 pgx 的 Scan 赋值方式
			switch dp := d.(type) {
			case *string:
				*dp = fmt.Sprintf("%v", row[i])
			case *bool:
				*dp = row[i].(bool)
			case *int:
				*dp = row[i].(int)
			case *float64:
				*dp = row[i].(float64)
			default:
				return fmt.Errorf("unsupported scan type: %T", d)
			}
		}
	}
	return nil
}
func (r *fakeRows) Values() ([]any, error) { return nil, nil }
func (r *fakeRows) RawValues() [][]byte     { return nil }

// fakeRow 实现 pgx.Row 接口

func (r *fakeRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(r.vals) == 0 {
		return nil
	}
	for i, d := range dest {
		if i < len(r.vals) {
			switch dp := d.(type) {
			case *string:
				*dp = fmt.Sprintf("%v", r.vals[i])
			case *bool:
				*dp = r.vals[i].(bool)
			case *int:
				*dp = r.vals[i].(int)
			case *float64:
				*dp = r.vals[i].(float64)
			default:
				return fmt.Errorf("unsupported scan type: %T", d)
			}
		}
	}
	return nil
}

// newTestGaussStore 创建带 fake 客户端的 GaussVectorStore
func newTestGaussStore() *GaussVectorStore {
	s := NewGaussVectorStore("postgres://test:test@localhost:5432/testdb")
	fake := newFakeDBClient()
	s.pool = fake
	s.createPool = func(ctx context.Context, connString string) (dbClient, error) {
		return fake, nil
	}
	return s
}

func createGaussTestSchema() *CollectionSchema {
	pk, _ := NewFieldSchema("id", VectorDataTypeVarchar, WithPrimary())
	vec, _ := NewFieldSchema("embedding", VectorDataTypeFloatVector, WithDim(128))
	text, _ := NewFieldSchema("text", VectorDataTypeVarchar)
	schema, _ := NewCollectionSchemaFromFields([]*FieldSchema{pk, vec, text})
	return schema
}

// ─── 构造函数测试 ───

func TestNewGaussVectorStore(t *testing.T) {
	s := NewGaussVectorStore("postgres://localhost:5432/test")
	if s.connConfig != "postgres://localhost:5432/test" {
		t.Errorf("connConfig = %v, want postgres://localhost:5432/test", s.connConfig)
	}
	if s.collectionMetadata == nil {
		t.Error("collectionMetadata 不应为 nil")
	}
	if s.createPool == nil {
		t.Error("createPool 不应为 nil")
	}
}

// ─── CreateCollection 测试 ───

func TestGaussVectorStore_CreateCollection(t *testing.T) {
	s := newTestGaussStore()
	schema := createGaussTestSchema()
	ctx := context.Background()

	err := s.CreateCollection(ctx, "test_coll", schema, WithDistanceMetric("COSINE"))
	if err != nil {
		t.Fatalf("CreateCollection() error = %v", err)
	}

	// 验证元数据缓存
	s.mu.RLock()
	meta, ok := s.collectionMetadata["test_coll"]
	s.mu.RUnlock()
	if !ok {
		t.Fatal("集合元数据应已缓存")
	}
	if meta.DistanceMetric != "COSINE" {
		t.Errorf("DistanceMetric = %v, want COSINE", meta.DistanceMetric)
	}
	if meta.VectorField != "embedding" {
		t.Errorf("VectorField = %v, want embedding", meta.VectorField)
	}
}

func TestGaussVectorStore_CreateCollection_缺少主键(t *testing.T) {
	s := newTestGaussStore()
	vec, _ := NewFieldSchema("embedding", VectorDataTypeFloatVector, WithDim(128))
	schema, _ := NewCollectionSchemaFromFields([]*FieldSchema{vec})
	ctx := context.Background()

	err := s.CreateCollection(ctx, "test_coll", schema)
	if err == nil {
		t.Error("缺少主键时应返回错误")
	}
}

func TestGaussVectorStore_CreateCollection_缺少向量字段(t *testing.T) {
	s := newTestGaussStore()
	pk, _ := NewFieldSchema("id", VectorDataTypeVarchar, WithPrimary())
	schema, _ := NewCollectionSchemaFromFields([]*FieldSchema{pk})
	ctx := context.Background()

	err := s.CreateCollection(ctx, "test_coll", schema)
	if err == nil {
		t.Error("缺少向量字段时应返回错误")
	}
}

func TestGaussVectorStore_CreateCollection_已存在(t *testing.T) {
	s := newTestGaussStore()
	schema := createGaussTestSchema()
	ctx := context.Background()

	// 模拟集合已存在
	fake := s.pool.(*fakeDBClient)
	fake.queryRowFn = func(ctx context.Context, sql string, args ...any) pgx.Row {
		return &fakeRow{vals: []any{true}, err: nil}
	}

	err := s.CreateCollection(ctx, "test_coll", schema)
	if err != nil {
		t.Fatalf("集合已存在时应返回 nil, error = %v", err)
	}
}

// ─── DeleteCollection 测试 ───

func TestGaussVectorStore_DeleteCollection(t *testing.T) {
	s := newTestGaussStore()
	ctx := context.Background()

	// 先缓存元数据
	s.mu.Lock()
	s.collectionMetadata["test_coll"] = &gaussCollMeta{
		DistanceMetric: "COSINE",
		VectorField:    "embedding",
	}
	s.mu.Unlock()

	err := s.DeleteCollection(ctx, "test_coll")
	if err != nil {
		t.Fatalf("DeleteCollection() error = %v", err)
	}

	// 验证缓存已清除
	s.mu.RLock()
	_, ok := s.collectionMetadata["test_coll"]
	s.mu.RUnlock()
	if ok {
		t.Error("删除后缓存应已清除")
	}
}

// ─── CollectionExists 测试 ───

func TestGaussVectorStore_CollectionExists_存在(t *testing.T) {
	s := newTestGaussStore()
	fake := s.pool.(*fakeDBClient)
	fake.queryRowFn = func(ctx context.Context, sql string, args ...any) pgx.Row {
		return &fakeRow{vals: []any{true}, err: nil}
	}
	ctx := context.Background()

	exists, err := s.CollectionExists(ctx, "test_coll")
	if err != nil {
		t.Fatalf("CollectionExists() error = %v", err)
	}
	if !exists {
		t.Error("集合应存在")
	}
}

func TestGaussVectorStore_CollectionExists_不存在(t *testing.T) {
	s := newTestGaussStore()
	fake := s.pool.(*fakeDBClient)
	fake.queryRowFn = func(ctx context.Context, sql string, args ...any) pgx.Row {
		return &fakeRow{vals: []any{false}, err: nil}
	}
	ctx := context.Background()

	exists, err := s.CollectionExists(ctx, "test_coll")
	if err != nil {
		t.Fatalf("CollectionExists() error = %v", err)
	}
	if exists {
		t.Error("集合不应存在")
	}
}

// ─── ListCollectionNames 测试 ───

func TestGaussVectorStore_ListCollectionNames(t *testing.T) {
	s := newTestGaussStore()
	fake := s.pool.(*fakeDBClient)
	fake.queryFn = func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
		return &fakeRows{
			rows: [][]any{{"coll1"}, {"coll2"}},
		}, nil
	}
	ctx := context.Background()

	names, err := s.ListCollectionNames(ctx)
	if err != nil {
		t.Fatalf("ListCollectionNames() error = %v", err)
	}
	if len(names) != 2 {
		t.Errorf("len(names) = %v, want 2", len(names))
	}
}

// ─── UpdateSchema 测试 ───

func TestGaussVectorStore_UpdateSchema_未实现(t *testing.T) {
	s := newTestGaussStore()
	ctx := context.Background()

	err := s.UpdateSchema(ctx, "test_coll", nil)
	if err == nil {
		t.Error("UpdateSchema 应返回 ErrNotImplemented")
	}
}

// ─── UpdateCollectionMetadata 测试 ───

func TestGaussVectorStore_UpdateCollectionMetadata(t *testing.T) {
	s := newTestGaussStore()
	ctx := context.Background()

	err := s.UpdateCollectionMetadata(ctx, "test_coll", map[string]any{
		"distance_metric": "L2",
		"vector_field":    "embedding",
		"vector_dim":      256,
		"schema_version":  "2",
	})
	if err != nil {
		t.Fatalf("UpdateCollectionMetadata() error = %v", err)
	}

	s.mu.RLock()
	meta := s.collectionMetadata["test_coll"]
	s.mu.RUnlock()
	if meta.DistanceMetric != "L2" {
		t.Errorf("DistanceMetric = %v, want L2", meta.DistanceMetric)
	}
	if meta.VectorDim != 256 {
		t.Errorf("VectorDim = %v, want 256", meta.VectorDim)
	}
}

// ─── GetCollectionMetadata 测试 ───

func TestGaussVectorStore_GetCollectionMetadata_缓存命中(t *testing.T) {
	s := newTestGaussStore()
	s.mu.Lock()
	s.collectionMetadata["test_coll"] = &gaussCollMeta{
		DistanceMetric: "COSINE",
		VectorField:    "embedding",
		VectorDim:      128,
		SchemaVersion:  "1",
	}
	s.mu.Unlock()
	ctx := context.Background()

	meta, err := s.GetCollectionMetadata(ctx, "test_coll")
	if err != nil {
		t.Fatalf("GetCollectionMetadata() error = %v", err)
	}
	if meta["distance_metric"] != "COSINE" {
		t.Errorf("distance_metric = %v, want COSINE", meta["distance_metric"])
	}
}

// ─── AddDocs 测试 ───

func TestGaussVectorStore_AddDocs_空文档(t *testing.T) {
	s := newTestGaussStore()
	ctx := context.Background()

	err := s.AddDocs(ctx, "test_coll", nil)
	if err != nil {
		t.Errorf("AddDocs(nil) error = %v, want nil", err)
	}
}

func TestGaussVectorStore_AddDocs(t *testing.T) {
	s := newTestGaussStore()
	ctx := context.Background()

	docs := []map[string]any{
		{"id": "doc1", "text": "hello", "embedding": []float64{0.1, 0.2, 0.3}},
	}

	err := s.AddDocs(ctx, "test_coll", docs)
	if err != nil {
		t.Fatalf("AddDocs() error = %v", err)
	}
}

// ─── DeleteDocsByIDs 测试 ───

func TestGaussVectorStore_DeleteDocsByIDs_空列表(t *testing.T) {
	s := newTestGaussStore()
	ctx := context.Background()

	err := s.DeleteDocsByIDs(ctx, "test_coll", nil)
	if err != nil {
		t.Errorf("DeleteDocsByIDs(nil) error = %v, want nil", err)
	}
}

func TestGaussVectorStore_DeleteDocsByIDs(t *testing.T) {
	s := newTestGaussStore()
	ctx := context.Background()

	err := s.DeleteDocsByIDs(ctx, "test_coll", []string{"id1", "id2"})
	if err != nil {
		t.Fatalf("DeleteDocsByIDs() error = %v", err)
	}
}

// ─── DeleteDocsByFilters 测试 ───

func TestGaussVectorStore_DeleteDocsByFilters_空过滤(t *testing.T) {
	s := newTestGaussStore()
	ctx := context.Background()

	err := s.DeleteDocsByFilters(ctx, "test_coll", nil)
	if err != nil {
		t.Errorf("DeleteDocsByFilters(nil) error = %v, want nil", err)
	}
}

func TestGaussVectorStore_DeleteDocsByFilters(t *testing.T) {
	s := newTestGaussStore()
	ctx := context.Background()

	err := s.DeleteDocsByFilters(ctx, "test_coll", map[string]any{"status": "active"})
	if err != nil {
		t.Fatalf("DeleteDocsByFilters() error = %v", err)
	}
}

// ─── Close 测试 ───

func TestGaussVectorStore_Close(t *testing.T) {
	s := newTestGaussStore()
	s.Close()
	if s.pool != nil {
		t.Error("Close() 后 pool 应为 nil")
	}
}

// ─── 类型映射测试 ───

func TestMapFieldTypeToPG(t *testing.T) {
	tests := []struct {
		dt     VectorDataType
		want   string
		hasErr bool
	}{
		{VectorDataTypeVarchar, "VARCHAR", false},
		{VectorDataTypeFloatVector, "FLOATVECTOR", false},
		{VectorDataTypeInt64, "BIGINT", false},
		{VectorDataTypeInt32, "INTEGER", false},
		{VectorDataTypeInt16, "SMALLINT", false},
		{VectorDataTypeInt8, "SMALLINT", false},
		{VectorDataTypeFloat, "REAL", false},
		{VectorDataTypeDouble, "DOUBLE PRECISION", false},
		{VectorDataTypeBool, "BOOLEAN", false},
		{VectorDataTypeJSON, "JSONB", false},
		{VectorDataTypeArray, "", true},
	}
	for _, tt := range tests {
		got, err := mapFieldTypeToPG(tt.dt)
		if tt.hasErr && err == nil {
			t.Errorf("mapFieldTypeToPG(%v) 应返回错误", tt.dt)
		}
		if !tt.hasErr && got != tt.want {
			t.Errorf("mapFieldTypeToPG(%v) = %v, want %v", tt.dt, got, tt.want)
		}
	}
}

func TestMapPGTypeToOurType(t *testing.T) {
	tests := []struct {
		pgType string
		want   VectorDataType
	}{
		{"varchar", VectorDataTypeVarchar},
		{"character varying", VectorDataTypeVarchar},
		{"floatvector", VectorDataTypeFloatVector},
		{"bigint", VectorDataTypeInt64},
		{"int8", VectorDataTypeInt64},
		{"integer", VectorDataTypeInt32},
		{"int4", VectorDataTypeInt32},
		{"smallint", VectorDataTypeInt16},
		{"int2", VectorDataTypeInt16},
		{"real", VectorDataTypeFloat},
		{"float4", VectorDataTypeFloat},
		{"double precision", VectorDataTypeDouble},
		{"float8", VectorDataTypeDouble},
		{"boolean", VectorDataTypeBool},
		{"bool", VectorDataTypeBool},
		{"jsonb", VectorDataTypeJSON},
		{"json", VectorDataTypeJSON},
		{"unknown_type", VectorDataTypeVarchar},
	}
	for _, tt := range tests {
		got := mapPGTypeToOurType(tt.pgType)
		if got != tt.want {
			t.Errorf("mapPGTypeToOurType(%q) = %v, want %v", tt.pgType, got, tt.want)
		}
	}
}

// ─── 距离转换测试 ───

func TestGaussNormalizeScore_COSINE(t *testing.T) {
	// GaussDB COSINE 距离 0 → 相似度 1.0
	score := gaussNormalizeScore(0, "COSINE")
	if score != 1.0 {
		t.Errorf("gaussNormalizeScore(0, COSINE) = %v, want 1.0", score)
	}
	// GaussDB COSINE 距离 2 → 相似度 0.0
	score = gaussNormalizeScore(2, "COSINE")
	if score != 0.0 {
		t.Errorf("gaussNormalizeScore(2, COSINE) = %v, want 0.0", score)
	}
}

func TestGaussNormalizeScore_L2(t *testing.T) {
	score := gaussNormalizeScore(0, "L2")
	if score != 1.0 {
		t.Errorf("gaussNormalizeScore(0, L2) = %v, want 1.0", score)
	}
}

// ─── SQL 构建测试 ───

func TestGaussBuildCreateTableSQL(t *testing.T) {
	schema := createGaussTestSchema()
	sql, err := gaussBuildCreateTableSQL("test_coll", schema)
	if err != nil {
		t.Fatalf("gaussBuildCreateTableSQL() error = %v", err)
	}
	if !strings.Contains(sql, "CREATE TABLE") {
		t.Errorf("SQL 应包含 CREATE TABLE, got: %s", sql)
	}
	if !strings.Contains(sql, `"id" VARCHAR`) {
		t.Errorf("SQL 应包含 id VARCHAR 列, got: %s", sql)
	}
	if !strings.Contains(sql, `"embedding" FLOATVECTOR(128)`) {
		t.Errorf("SQL 应包含 embedding FLOATVECTOR(128) 列, got: %s", sql)
	}
	if !strings.Contains(sql, "PRIMARY KEY") {
		t.Errorf("SQL 应包含 PRIMARY KEY, got: %s", sql)
	}
}

func TestGaussBuildCreateIndexSQL(t *testing.T) {
	diskann := vector_fields.NewGaussDiskANN("embedding")
	sql := gaussBuildCreateIndexSQL("idx_test_embedding", "test_coll", "embedding", gaussMetricCosine, diskann)
	if !strings.Contains(sql, "USING GSDISKANN") {
		t.Errorf("SQL 应包含 USING GSDISKANN, got: %s", sql)
	}
	if !strings.Contains(sql, "cosine") {
		t.Errorf("SQL 应包含 cosine, got: %s", sql)
	}
	if !strings.Contains(sql, "enable_pq = true") {
		t.Errorf("SQL 应包含 enable_pq = true, got: %s", sql)
	}
}

func TestGaussBuildFilterClause(t *testing.T) {
	clause, args := gaussBuildFilterClause(map[string]any{
		"status": "active",
		"count":  10,
	})
	if !strings.Contains(clause, `"status" = $`) {
		t.Errorf("子句应包含 status 过滤, got: %s", clause)
	}
	if !strings.Contains(clause, `"count" = $`) {
		t.Errorf("子句应包含 count 过滤, got: %s", clause)
	}
	if len(args) != 2 {
		t.Errorf("参数数量 = %v, want 2", len(args))
	}
}

func TestGaussFormatVector(t *testing.T) {
	result := gaussFormatVector([]float64{1.0, 2.5, 3.0})
	if !strings.Contains(result, "[") || !strings.Contains(result, "]") {
		t.Errorf("格式化结果应包含方括号, got: %s", result)
	}
}

// ─── 标识符转义测试 ───

func TestGaussBuildCreateTableSQL_标识符转义(t *testing.T) {
	pk, _ := NewFieldSchema("id", VectorDataTypeVarchar, WithPrimary())
	vec, _ := NewFieldSchema("embedding", VectorDataTypeFloatVector, WithDim(64))
	schema, _ := NewCollectionSchemaFromFields([]*FieldSchema{pk, vec})

	sql, err := gaussBuildCreateTableSQL("my-collection", schema)
	if err != nil {
		t.Fatalf("gaussBuildCreateTableSQL() error = %v", err)
	}
	// pgx Identifier.Sanitize 会将表名加双引号
	if !strings.Contains(sql, `"my-collection"`) {
		t.Errorf("SQL 应包含转义表名, got: %s", sql)
	}
}
```

- [ ] **Step 6.2: 运行测试**

```bash
cd /home/opensource/uap-claw-go && go test ./internal/agentcore/store/vector/... -run TestGauss -v
```

Expected: 所有 Gauss 相关测试 PASS

- [ ] **Step 6.3: Commit**

```bash
git add internal/agentcore/store/vector/gauss_test.go && git commit -m "test(vector): 添加 GaussVectorStore 单元测试"
```

---

### Task 7: 创建 GaussVectorStore 集成测试占位

**Files:**
- Create: `internal/agentcore/store/vector/gauss_integration_test.go`

- [ ] **Step 7.1: 创建集成测试文件**

创建 `internal/agentcore/store/vector/gauss_integration_test.go`：

```go
//go:build integration

package vector

import (
	"context"
	"os"
	"testing"
)

// TestGaussVectorStore_集成测试 GaussVectorStore 与真实 GaussDB 的集成测试
// 运行方式: go test -tags=integration ./internal/agentcore/store/vector/...
func TestGaussVectorStore_集成测试(t *testing.T) {
	connString := os.Getenv("GAUSS_DB_CONN_STRING")
	if connString == "" {
		t.Skip("未设置 GAUSS_DB_CONN_STRING 环境变量，跳过集成测试")
	}

	s := NewGaussVectorStore(connString)
	defer s.Close()
	ctx := context.Background()

	// 创建集合
	schema := createGaussTestSchema()
	err := s.CreateCollection(ctx, "integration_test_coll", schema, WithDistanceMetric("COSINE"))
	if err != nil {
		t.Fatalf("CreateCollection() error = %v", err)
	}

	// 插入文档
	docs := []map[string]any{
		{"id": "doc1", "text": "hello world", "embedding": make([]float64, 128)},
	}
	err = s.AddDocs(ctx, "integration_test_coll", docs)
	if err != nil {
		t.Fatalf("AddDocs() error = %v", err)
	}

	// 搜索
	results, err := s.Search(ctx, "integration_test_coll", make([]float64, 128), "embedding", 5, nil)
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	t.Logf("搜索结果数量: %d", len(results))

	// 清理
	err = s.DeleteCollection(ctx, "integration_test_coll")
	if err != nil {
		t.Fatalf("DeleteCollection() error = %v", err)
	}
}
```

- [ ] **Step 7.2: 验证集成测试不影响默认测试**

```bash
cd /home/opensource/uap-claw-go && go test ./internal/agentcore/store/vector/... -v
```

Expected: 集成测试不被执行（无 `integration` build tag），其他测试 PASS

- [ ] **Step 7.3: Commit**

```bash
git add internal/agentcore/store/vector/gauss_integration_test.go && git commit -m "test(vector): 添加 GaussVectorStore 集成测试占位"
```

---

### Task 8: 更新 vector/doc.go 和 IMPLEMENTATION_PLAN.md

**Files:**
- Modify: `internal/agentcore/store/vector/doc.go`
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 8.1: 更新 vector/doc.go**

在文件目录中添加 gauss.go：

```
//	├── gauss.go       # GaussVectorStore 结构体 + BaseVectorStore 接口实现（GaussDB DiskANN）
```

在核心类型/接口索引中添加：

```
//	GaussVectorStore  — GaussDB 向量存储实现，基于 pgx/v5 pgxpool 连接池 + DiskANN 索引
```

更新包功能概述，将"Gauss"加入支持的向量存储列表。

- [ ] **Step 8.2: 更新 IMPLEMENTATION_PLAN.md**

将第 308 行的 4.10 状态从 `☐` 改为 `✅`：

```
| 4.10 | ✅ | GaussVectorStore | GaussDB 向量实现 | `openjiuwen/extensions/store/gauss_vector_store.py` |
```

- [ ] **Step 8.3: Commit**

```bash
git add internal/agentcore/store/vector/doc.go IMPLEMENTATION_PLAN.md && git commit -m "docs: 更新 doc.go 和实现计划（4.10 GaussVectorStore 已完成）"
```

---

### Task 9: 最终验证

- [ ] **Step 9.1: 运行全部测试确认无破坏**

```bash
cd /home/opensource/uap-claw-go && go test ./internal/agentcore/store/... -v
```

Expected: 所有测试 PASS

- [ ] **Step 9.2: 运行覆盖率检查**

```bash
cd /home/opensource/uap-claw-go && go test -cover ./internal/agentcore/store/vector/... ./internal/agentcore/store/vector_fields/...
```

Expected: vector 包覆盖率 ≥ 85%（含 GaussVectorStore 代码）

- [ ] **Step 9.3: 运行 go vet 静态检查**

```bash
cd /home/opensource/uap-claw-go && go vet ./internal/agentcore/store/...
```

Expected: 无警告
