# ESVectorStore 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 Elasticsearch 向量存储后端，对齐 Python `es_vector_store.py` 的功能。

**Architecture:** ESVectorStore 实现 BaseVectorStore 接口，使用 `go-elasticsearch/v8` 官方客户端惰性创建模式。元数据通过 ES 内部 `_meta` 文档持久化，配合 `ESVectorField` 索引配置子类型和 `DatabaseTypeES` 枚举扩展。

**Tech Stack:** Go 1.25, `github.com/elastic/go-elasticsearch/v8`, `net/http/httptest` (测试)

---

### Task 1: 添加 go-elasticsearch 依赖

**Files:**
- Modify: `go.mod`
- Modify: `go.sum`

- [ ] **Step 1: 添加依赖**

```bash
cd /home/opensource/uap-claw-go && go get github.com/elastic/go-elasticsearch/v8
```

- [ ] **Step 2: 验证依赖添加成功**

```bash
cd /home/opensource/uap-claw-go && go mod tidy && grep "elastic/go-elasticsearch" go.mod
```

Expected: 输出包含 `github.com/elastic/go-elasticsearch/v8`

- [ ] **Step 3: Commit**

```bash
git add go.mod go.sum && git commit -m "chore: 添加 go-elasticsearch/v8 依赖"
```

---

### Task 2: 扩展 vector_fields 包 — 新增 DatabaseTypeES 和 ESVectorField

**Files:**
- Modify: `internal/agentcore/store/vector_fields/base.go`
- Create: `internal/agentcore/store/vector_fields/es_fields.go`
- Create: `internal/agentcore/store/vector_fields/es_fields_test.go`
- Modify: `internal/agentcore/store/vector_fields/doc.go`

- [ ] **Step 1: 在 base.go 新增 DatabaseTypeES 枚举值**

在 `internal/agentcore/store/vector_fields/base.go` 的 `DatabaseType` 枚举 `const` 块中，在 `DatabaseTypeGauss` 之后新增：

```go
// DatabaseTypeES Elasticsearch 向量数据库
DatabaseTypeES
```

在 `databaseTypeStrings` 变量中，在 `"gauss"` 之后新增：

```go
"es",
```

- [ ] **Step 2: 创建 es_fields.go**

创建 `internal/agentcore/store/vector_fields/es_fields.go`：

```go
package vector_fields

// ──────────────────────────── 结构体 ────────────────────────────

// ESVectorField Elasticsearch 向量索引配置。
// ES 8.x 使用 dense_vector 字段的 HNSW 算法实现 k-NN 搜索。
//
// 对应 Python: es_vector_store.py 中的 k-NN 索引参数
type ESVectorField struct {
	VectorField
	// NumCandidates k-NN 搜索候选集大小（search 阶段）
	NumCandidates int `vf:"search"`
	// ExtraConstruct 构建阶段额外参数
	ExtraConstruct map[string]any `vf:"construct"`
	// ExtraSearch 搜索阶段额外参数
	ExtraSearch map[string]any `vf:"search"`
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewESVectorField 创建 Elasticsearch 向量索引配置，使用默认参数。
// fieldName 为向量字段名。
func NewESVectorField(fieldName string) *ESVectorField {
	return &ESVectorField{
		VectorField: VectorField{
			DatabaseType:    DatabaseTypeES,
			IndexType:       IndexTypeHNSW,
			VectorFieldName: fieldName,
		},
		NumCandidates: 100,
	}
}

// Validate 校验 ESVectorField 参数。
func (e *ESVectorField) Validate() error {
	if e.NumCandidates < 0 {
		return fmt.Errorf("NumCandidates 不能为负数，当前值: %d", e.NumCandidates)
	}
	return nil
}
```

注意：需要在 import 中添加 `"fmt"`。

- [ ] **Step 3: 创建 es_fields_test.go**

创建 `internal/agentcore/store/vector_fields/es_fields_test.go`：

```go
package vector_fields

import "testing"

func TestNewESVectorField(t *testing.T) {
	e := NewESVectorField("embedding")
	if e.DatabaseType != DatabaseTypeES {
		t.Errorf("DatabaseType = %v, want %v", e.DatabaseType, DatabaseTypeES)
	}
	if e.IndexType != IndexTypeHNSW {
		t.Errorf("IndexType = %v, want %v", e.IndexType, IndexTypeHNSW)
	}
	if e.VectorFieldName != "embedding" {
		t.Errorf("VectorFieldName = %v, want embedding", e.VectorFieldName)
	}
	if e.NumCandidates != 100 {
		t.Errorf("NumCandidates = %v, want 100", e.NumCandidates)
	}
}

func TestESVectorField_Validate_正常(t *testing.T) {
	e := NewESVectorField("embedding")
	if err := e.Validate(); err != nil {
		t.Errorf("Validate() error = %v, want nil", err)
	}
}

func TestESVectorField_Validate_NumCandidates无效(t *testing.T) {
	e := NewESVectorField("embedding")
	e.NumCandidates = -1
	if err := e.Validate(); err == nil {
		t.Error("NumCandidates=-1 时 Validate 应返回错误")
	}
}

func TestESVectorField_ToDict_construct(t *testing.T) {
	e := NewESVectorField("embedding")
	dict := ToDict(e, StageConstruct)
	// construct 阶段无特有字段（ExtraConstruct 为 nil），结果应为空
	if len(dict) != 0 {
		t.Errorf("construct 阶段 dict 应为空，实际 %v", dict)
	}
}

func TestESVectorField_ToDict_search(t *testing.T) {
	e := NewESVectorField("embedding")
	dict := ToDict(e, StageSearch)
	if v, ok := dict["NumCandidates"]; !ok {
		t.Error("search 阶段应包含 NumCandidates")
	} else if v != 100 {
		t.Errorf("NumCandidates = %v, want 100", v)
	}
}

func TestESVectorField_ToDict_ExtraFields(t *testing.T) {
	e := NewESVectorField("embedding")
	e.ExtraConstruct = map[string]any{"custom_param": 42}
	e.ExtraSearch = map[string]any{"search_param": "value"}

	constructDict := ToDict(e, StageConstruct)
	if v, ok := constructDict["custom_param"]; !ok {
		t.Error("construct 阶段应包含 ExtraConstruct 展开的 custom_param")
	} else if v != 42 {
		t.Errorf("custom_param = %v, want 42", v)
	}

	searchDict := ToDict(e, StageSearch)
	if v, ok := searchDict["search_param"]; !ok {
		t.Error("search 阶段应包含 ExtraSearch 展开的 search_param")
	} else if v != "value" {
		t.Errorf("search_param = %v, want value", v)
	}
}
```

- [ ] **Step 4: 运行测试**

```bash
cd /home/opensource/uap-claw-go && go test ./internal/agentcore/store/vector_fields/... -v -run "TestNewESVectorField|TestESVectorField"
```

Expected: 所有测试 PASS

- [ ] **Step 5: 更新 vector_fields/doc.go**

在文件目录树中 `gauss_fields.go` 行后新增：

```
//	└── es_fields.go      # Elasticsearch HNSW/k-NN 索引子类型
```

在核心类型/接口索引中 `GaussDiskANN` 行后新增：

```
//	ESVectorField   — Elasticsearch k-NN 索引配置（NumCandidates, ExtraConstruct, ExtraSearch）
```

更新 DatabaseType 枚举描述：

```
//	DatabaseType    — 向量数据库类型枚举（Milvus, Chroma, PG, Gauss, ES）
```

- [ ] **Step 6: Commit**

```bash
git add internal/agentcore/store/vector_fields/ && git commit -m "feat(vector_fields): 新增 DatabaseTypeES 枚举和 ESVectorField 索引配置"
```

---

### Task 3: 扩展 vector/base.go — 新增 NumCandidates Option

**Files:**
- Modify: `internal/agentcore/store/vector/base.go`

- [ ] **Step 1: 在 Options 结构体新增 NumCandidates 字段**

在 `internal/agentcore/store/vector/base.go` 的 `Options` 结构体中，在 `ShardsNum` 字段后新增：

```go
// NumCandidates ES k-NN 搜索候选集大小，0 表示使用默认值 max(topK*10, 100)
NumCandidates int
```

- [ ] **Step 2: 新增 WithNumCandidates Option 函数**

在 `WithShardsNum` 函数后新增：

```go
// WithNumCandidates 设置 ES k-NN 搜索候选集大小。
// 候选集越大搜索越精确但越慢，0 表示使用默认值 max(topK*10, 100)。
func WithNumCandidates(n int) Option {
	return func(o *Options) { o.NumCandidates = n }
}
```

- [ ] **Step 3: 运行现有测试确保无破坏**

```bash
cd /home/opensource/uap-claw-go && go test ./internal/agentcore/store/vector/... -v -count=1
```

Expected: 所有现有测试 PASS

- [ ] **Step 4: Commit**

```bash
git add internal/agentcore/store/vector/base.go && git commit -m "feat(vector): Options 新增 NumCandidates 字段和 WithNumCandidates Option"
```

---

### Task 4: 实现 ESVectorStore 核心结构体和 esClient 接口

**Files:**
- Create: `internal/agentcore/store/vector/es.go`

- [ ] **Step 1: 创建 es.go 骨架**

创建 `internal/agentcore/store/vector/es.go`，包含结构体定义、构造函数、esClient 接口、类型映射函数和 Close 方法：

```go
package vector

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"

	elasticsearch8 "github.com/elastic/go-elasticsearch/v8"
	"github.com/elastic/go-elasticsearch/v8/esapi"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/store/vector_fields"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// esBulkError 批量操作错误项
type esBulkError struct {
	// Index 文档所在 index
	Index string
	// ID 文档 ID
	ID string
	// Error 错误信息
	Error string
}

// ESVectorStore 基于 Elasticsearch 的向量存储实现。
//
// 使用 Elasticsearch 8.x 的 dense_vector 字段类型 + 原生 k-NN 搜索提供向量相似度搜索能力。
// 每个集合映射为一个 ES index，元数据通过 index 内部 _meta 文档持久化。
// 客户端惰性创建，初始化时不需要 ES 可用。
//
// 对应 Python: vector/es_vector_store.py (ElasticsearchVectorStore)
type ESVectorStore struct {
	// client ES 客户端实例
	client esClient
	// addresses ES 节点地址列表
	addresses []string
	// username 认证用户名
	username string
	// password 认证密码
	password string
	// indexPrefix index 名前缀
	indexPrefix string
	// createClient 客户端创建函数，用于依赖注入和测试
	createClient func(ctx context.Context, addresses []string, username, password string) (esClient, error)
	// mu 读写锁，保护客户端
	mu sync.RWMutex
}

// esClient 封装 elasticsearch.Client 的操作方法，用于依赖注入和测试 mock。
// *esClientWrapper 天然实现此接口。
type esClient interface {
	// Do 执行 ES API 请求，返回 *http.Response
	Do(req esapi.Request) (*http.Response, error)
	// Close 关闭客户端连接
	Close()
}

// esClientWrapper 封装 *elasticsearch8.Client，实现 esClient 接口。
type esClientWrapper struct {
	inner *elasticsearch8.Client
}

// Do 实现 esClient 接口
func (w *esClientWrapper) Do(req esapi.Request) (*http.Response, error) {
	return req.Do(context.Background(), w.inner)
}

// Close 实现 esClient 接口（elasticsearch8.Client 无 Close 方法，空操作）
func (w *esClientWrapper) Close() {}

// ──────────────────────────── 枚举 ────────────────────────────

// esSimilarity ES 相似度类型
type esSimilarity string

const (
	esSimilarityCosine    esSimilarity = "cosine"
	esSimilarityL2Norm    esSimilarity = "l2_norm"
	esSimilarityDotProduct esSimilarity = "dot_product"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// esDefaultIndexPrefix ES index 默认前缀
	esDefaultIndexPrefix = "agent_vector"
	// esDefaultBatchSize ES 默认批量操作大小
	esDefaultBatchSize = 500
	// esMetadataDocID 元数据文档 ID
	esMetadataDocID = "__collection_metadata__"
	// esDefaultDistanceMetric ES 默认距离度量
	esDefaultDistanceMetric = "COSINE"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// esLogComponent 日志组件
var esLogComponent = logger.ComponentAgentCore

// esSimilarityMap 距离度量到 ES similarity 的映射，对齐 Python _ES_SIMILARITY_MAP
var esSimilarityMap = map[string]esSimilarity{
	"COSINE": esSimilarityCosine,
	"L2":     esSimilarityL2Norm,
	"IP":     esSimilarityDotProduct,
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewESVectorStore 创建 ESVectorStore 实例。
// 客户端惰性创建，初始化时不需要 ES 可用。
// addresses 为 ES 节点地址列表（如 []string{"http://localhost:9200"}）。
func NewESVectorStore(addresses []string, username, password string, opts ...ESOption) *ESVectorStore {
	s := &ESVectorStore{
		addresses:   addresses,
		username:    username,
		password:    password,
		indexPrefix: esDefaultIndexPrefix,
		createClient: defaultCreateESClient,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// ESOption ESVectorStore 构造选项
type ESOption func(*ESVectorStore)

// WithESIndexPrefix 设置 index 名前缀
func WithESIndexPrefix(prefix string) ESOption {
	return func(s *ESVectorStore) { s.indexPrefix = prefix }
}

// Close 关闭 ES 客户端连接。
func (s *ESVectorStore) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.client != nil {
		s.client.Close()
		s.client = nil
	}
	logger.Info(esLogComponent).
		Msg("ES 客户端连接已关闭")
}

// CreateCollection 创建向量集合。
// 从 schema 构建 ES index mapping，在 index 内部存储 _meta 元数据文档。
//
// 对应 Python: ElasticsearchVectorStore.create_collection()
func (s *ESVectorStore) CreateCollection(ctx context.Context, collectionName string, schema *CollectionSchema, opts ...Option) error {
	o := newOptions(opts...)

	// 校验 Schema
	if len(schema.GetVectorFields()) == 0 {
		return exception.BuildError(exception.StatusStoreVectorSchemaInvalid,
			exception.WithParam("error_msg", "Schema 必须包含至少一个 FLOAT_VECTOR 字段"),
		)
	}

	// 获取客户端
	client, err := s.getClient(ctx)
	if err != nil {
		return fmt.Errorf("CreateCollection: %w", err)
	}

	indexName := esIndexName(s.indexPrefix, collectionName)

	// 检查 index 是否已存在
	exists, err := esIndicesExists(ctx, client, indexName)
	if err != nil {
		logger.Debug(esLogComponent).Err(err).
			Str("collection_name", collectionName).
			Msg("检查 index 是否存在失败")
	}
	if exists {
		logger.Info(esLogComponent).
			Str("collection_name", collectionName).
			Msg("集合 index 已存在，跳过创建")
		return nil
	}

	// 获取距离度量和向量字段
	distanceMetric := o.DistanceMetric
	if distanceMetric == "" {
		distanceMetric = esDefaultDistanceMetric
	}
	vectorFields := schema.GetVectorFields()
	vf := vectorFields[0]
	vectorDim := vf.Dim

	// 解析 ES 索引配置
	esVF := s.resolveESVectorField(vf.Name, o)

	// 构建 mapping
	mappings := esBuildMappings(schema, distanceMetric)

	// 创建 index
	if err := esIndicesCreate(ctx, client, indexName, mappings); err != nil {
		errStr := err.Error()
		if strings.Contains(errStr, "resource_already_exists_exception") {
			logger.Info(esLogComponent).
				Str("collection_name", collectionName).
				Msg("集合 index 已存在（竞态），跳过创建")
			return nil
		}
		logger.Error(esLogComponent).Err(err).
			Str("collection_name", collectionName).
			Str("event_type", "LLM_CALL_ERROR").
			Msg("创建集合 index 失败")
		return fmt.Errorf("CreateCollection: %w", err)
	}

	// 存储 _meta 元数据文档
	metadata := map[string]any{
		"schema":           schema.ToDict(),
		"distance_metric":  strings.ToUpper(distanceMetric),
		"vector_field":     vf.Name,
		"vector_dim":       vectorDim,
		"schema_version":   0,
		"collection_name":  collectionName,
	}
	if esVF != nil {
		metadata["es_index_config"] = vector_fields.ToDict(esVF, vector_fields.StageSearch)
	}

	if err := esStoreMetadata(ctx, client, indexName, metadata); err != nil {
		logger.Warn(esLogComponent).Err(err).
			Str("collection_name", collectionName).
			Msg("存储集合元数据失败")
	}

	logger.Info(esLogComponent).
		Str("collection_name", collectionName).
		Int("field_count", len(schema.Fields())).
		Str("distance_metric", distanceMetric).
		Msg("创建集合成功")

	return nil
}

// DeleteCollection 删除向量集合。
//
// 对应 Python: ElasticsearchVectorStore.delete_collection()
func (s *ESVectorStore) DeleteCollection(ctx context.Context, collectionName string, opts ...Option) error {
	client, err := s.getClient(ctx)
	if err != nil {
		return fmt.Errorf("DeleteCollection: %w", err)
	}

	indexName := esIndexName(s.indexPrefix, collectionName)

	// 检查是否存在
	exists, err := esIndicesExists(ctx, client, indexName)
	if err != nil {
		logger.Warn(esLogComponent).Err(err).
			Str("collection_name", collectionName).
			Msg("检查 index 存在性失败")
	}
	if !exists {
		logger.Warn(esLogComponent).
			Str("collection_name", collectionName).
			Msg("集合不存在")
		return nil
	}

	if err := esIndicesDelete(ctx, client, indexName); err != nil {
		logger.Error(esLogComponent).Err(err).
			Str("collection_name", collectionName).
			Str("event_type", "LLM_CALL_ERROR").
			Msg("删除集合失败")
		return fmt.Errorf("DeleteCollection: %w", err)
	}

	logger.Info(esLogComponent).
		Str("collection_name", collectionName).
		Msg("删除集合成功")

	return nil
}

// CollectionExists 检查集合是否存在。
//
// 对应 Python: ElasticsearchVectorStore.collection_exists()
func (s *ESVectorStore) CollectionExists(ctx context.Context, collectionName string, opts ...Option) (bool, error) {
	client, err := s.getClient(ctx)
	if err != nil {
		return false, fmt.Errorf("CollectionExists: %w", err)
	}

	indexName := esIndexName(s.indexPrefix, collectionName)
	exists, err := esIndicesExists(ctx, client, indexName)
	if err != nil {
		return false, nil
	}
	return exists, nil
}

// GetSchema 获取集合的 Schema。
// 优先从 _meta 文档读取，fallback 到 ES mapping 反射。
//
// 对应 Python: ElasticsearchVectorStore.get_schema()
func (s *ESVectorStore) GetSchema(ctx context.Context, collectionName string, opts ...Option) (*CollectionSchema, error) {
	client, err := s.getClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("GetSchema: %w", err)
	}

	indexName := esIndexName(s.indexPrefix, collectionName)

	// 优先从 _meta 文档获取
	meta, err := esLoadMetadata(ctx, client, indexName)
	if err != nil {
		logger.Warn(esLogComponent).Err(err).
			Str("collection_name", collectionName).
			Msg("从 _meta 文档加载元数据失败")
	}

	if schemaDict, ok := meta["schema"]; ok {
		dict, ok := schemaDict.(map[string]any)
		if ok {
			schema, err := CollectionFromDict(dict)
			if err == nil {
				return schema, nil
			}
		}
	}

	// Fallback：从 ES mapping 反射构建
	mappings, err := esIndicesGetMapping(ctx, client, indexName)
	if err != nil {
		logger.Error(esLogComponent).Err(err).
			Str("collection_name", collectionName).
			Msg("获取 ES mapping 失败")
		return nil, exception.BuildError(exception.StatusStoreVectorCollectionNotFound,
			exception.WithParam("collection_name", collectionName),
			exception.WithParam("error_msg", err.Error()),
		)
	}

	schema, err := esBuildSchemaFromMapping(mappings, indexName, opts...)
	if err != nil {
		return nil, exception.BuildError(exception.StatusStoreVectorCollectionNotFound,
			exception.WithParam("collection_name", collectionName),
			exception.WithParam("error_msg", err.Error()),
		)
	}

	return schema, nil
}

// AddDocs 添加文档到集合。
// 按 BatchSize 分批调用 ES Bulk API。
//
// 对应 Python: ElasticsearchVectorStore.add_docs()
func (s *ESVectorStore) AddDocs(ctx context.Context, collectionName string, docs []map[string]any, opts ...Option) error {
	if len(docs) == 0 {
		return nil
	}

	o := newOptions(opts...)
	batchSize := o.BatchSize
	if batchSize <= 0 {
		batchSize = esDefaultBatchSize
	}

	client, err := s.getClient(ctx)
	if err != nil {
		return fmt.Errorf("AddDocs: %w", err)
	}

	indexName := esIndexName(s.indexPrefix, collectionName)

	// 从 _meta 获取主键字段名
	pkField := ""
	meta, err := esLoadMetadata(ctx, client, indexName)
	if err != nil {
		logger.Warn(esLogComponent).Err(err).
			Str("collection_name", collectionName).
			Msg("加载元数据获取主键字段名失败")
	} else {
		pkField = esGetPrimaryKeyField(meta)
	}

	// 构建批量操作 body
	var bulkErrors []esBulkError
	for i := 0; i < len(docs); i += batchSize {
		end := i + batchSize
		if end > len(docs) {
			end = len(docs)
		}
		batch := docs[i:end]

		body := esBuildBulkRequestBody(indexName, batch, pkField)
		errors, err := esBulk(ctx, client, body)
		if err != nil {
			logger.Error(esLogComponent).Err(err).
				Str("collection_name", collectionName).
				Int("batch_start", i).
				Int("batch_size", len(batch)).
				Str("event_type", "LLM_CALL_ERROR").
				Msg("批量插入失败")
			return fmt.Errorf("AddDocs: %w", err)
		}
		bulkErrors = append(bulkErrors, errors...)
	}

	if len(bulkErrors) > 0 {
		logger.Warn(esLogComponent).
			Int("error_count", len(bulkErrors)).
			Str("collection_name", collectionName).
			Msg("批量插入存在部分错误")
	}

	// 刷新 index
	if err := esIndicesRefresh(ctx, client, indexName); err != nil {
		logger.Debug(esLogComponent).Err(err).
			Str("collection_name", collectionName).
			Msg("刷新 index 失败")
	}

	logger.Info(esLogComponent).
		Str("collection_name", collectionName).
		Int("doc_count", len(docs)).
		Int("batch_size", batchSize).
		Msg("插入文档成功")

	return nil
}

// Search 向量相似度搜索。
// 使用 ES 8.x 原生 k-NN 查询子句。
//
// 对应 Python: ElasticsearchVectorStore.search()
func (s *ESVectorStore) Search(ctx context.Context, collectionName string, queryVector []float64, vectorField string, topK int, filters map[string]any, opts ...Option) ([]VectorSearchResult, error) {
	if topK <= 0 {
		topK = 5
	}

	o := newOptions(opts...)

	client, err := s.getClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("Search: %w", err)
	}

	indexName := esIndexName(s.indexPrefix, collectionName)

	// 从 _meta 获取距离度量
	distanceMetric := esDefaultDistanceMetric
	meta, err := esLoadMetadata(ctx, client, indexName)
	if err != nil {
		logger.Warn(esLogComponent).Err(err).
			Str("collection_name", collectionName).
			Msg("加载元数据获取距离度量失败")
	} else {
		if dm, ok := meta["distance_metric"].(string); ok && dm != "" {
			distanceMetric = dm
		}
	}

	// 计算 num_candidates
	numCandidates := o.NumCandidates
	if numCandidates <= 0 {
		numCandidates = topK * 10
		if numCandidates < 100 {
			numCandidates = 100
		}
	}

	// 构建 k-NN 查询
	knnClause := map[string]any{
		"field":          vectorField,
		"query_vector":   queryVector,
		"k":              topK,
		"num_candidates": numCandidates,
	}

	// 构建过滤条件
	if len(filters) > 0 {
		filterClause := esBuildFilterClause(filters)
		knnClause["filter"] = filterClause
	}

	body := map[string]any{
		"knn":  knnClause,
		"size": topK,
	}

	// 设置 _source 排除 _meta
	if len(o.OutputFields) > 0 {
		body["_source"] = map[string]any{
			"includes": o.OutputFields,
			"excludes": []string{"_meta"},
		}
	} else {
		body["_source"] = map[string]any{
			"excludes": []string{"_meta"},
		}
	}

	// 执行搜索
	resp, err := esSearch(ctx, client, indexName, body)
	if err != nil {
		logger.Error(esLogComponent).Err(err).
			Str("collection_name", collectionName).
			Str("event_type", "LLM_CALL_ERROR").
			Msg("向量搜索失败")
		return nil, fmt.Errorf("Search: %w", err)
	}

	// 解析结果
	hits, ok := resp["hits"].(map[string]any)
	if !ok {
		return nil, nil
	}
	hitList, ok := hits["hits"].([]any)
	if !ok {
		return nil, nil
	}

	var results []VectorSearchResult
	for _, hitAny := range hitList {
		hit, ok := hitAny.(map[string]any)
		if !ok {
			continue
		}

		score, _ := hit["_score"].(float64)
		source, _ := hit["_source"].(map[string]any)
		if source == nil {
			source = make(map[string]any)
		}

		// 移除 _meta 字段
		delete(source, "_meta")

		// 回填 _id 到 id 字段
		if _, hasID := source["id"]; !hasID {
			if id, ok := hit["_id"].(string); ok {
				source["id"] = id
			}
		}

		// 归一化分数
		normalizedScore := esNormalizeScore(score, distanceMetric)

		results = append(results, VectorSearchResult{
			Score:  normalizedScore,
			Fields: source,
		})
	}

	logger.Info(esLogComponent).
		Str("collection_name", collectionName).
		Int("top_k", topK).
		Str("distance_metric", distanceMetric).
		Int("result_count", len(results)).
		Msg("搜索完成")

	return results, nil
}

// DeleteDocsByIDs 按 ID 删除文档。
// 使用 ES Bulk API 批量删除。
//
// 对应 Python: ElasticsearchVectorStore.delete_docs_by_ids()
func (s *ESVectorStore) DeleteDocsByIDs(ctx context.Context, collectionName string, ids []string, opts ...Option) error {
	if len(ids) == 0 {
		return nil
	}

	o := newOptions(opts...)
	batchSize := o.BatchSize
	if batchSize <= 0 {
		batchSize = esDefaultBatchSize
	}

	client, err := s.getClient(ctx)
	if err != nil {
		return fmt.Errorf("DeleteDocsByIDs: %w", err)
	}

	indexName := esIndexName(s.indexPrefix, collectionName)

	for i := 0; i < len(ids); i += batchSize {
		end := i + batchSize
		if end > len(ids) {
			end = len(ids)
		}
		batch := ids[i:end]

		body := esBuildBulkDeleteBody(indexName, batch)
		_, err := esBulk(ctx, client, body)
		if err != nil {
			logger.Error(esLogComponent).Err(err).
				Str("collection_name", collectionName).
				Int("batch_start", i).
				Str("event_type", "LLM_CALL_ERROR").
				Msg("批量删除文档失败")
			return fmt.Errorf("DeleteDocsByIDs: %w", err)
		}
	}

	logger.Info(esLogComponent).
		Str("collection_name", collectionName).
		Int("id_count", len(ids)).
		Msg("按 ID 删除文档成功")

	return nil
}

// DeleteDocsByFilters 按标量字段过滤条件删除文档。
// 使用 ES delete_by_query API。
//
// 对应 Python: ElasticsearchVectorStore.delete_docs_by_filters()
func (s *ESVectorStore) DeleteDocsByFilters(ctx context.Context, collectionName string, filters map[string]any, opts ...Option) error {
	if len(filters) == 0 {
		return nil
	}

	client, err := s.getClient(ctx)
	if err != nil {
		return fmt.Errorf("DeleteDocsByFilters: %w", err)
	}

	indexName := esIndexName(s.indexPrefix, collectionName)

	query := map[string]any{
		"bool": map[string]any{
			"filter": esBuildFilterTerms(filters),
		},
	}

	deleted, err := esDeleteByQuery(ctx, client, indexName, query)
	if err != nil {
		logger.Error(esLogComponent).Err(err).
			Str("collection_name", collectionName).
			Str("event_type", "LLM_CALL_ERROR").
			Msg("按过滤条件删除文档失败")
		return fmt.Errorf("DeleteDocsByFilters: %w", err)
	}

	logger.Info(esLogComponent).
		Str("collection_name", collectionName).
		Int("deleted_count", deleted).
		Msg("按过滤条件删除文档成功")

	return nil
}

// ListCollectionNames 列出所有集合名称。
//
// 对应 Python: ElasticsearchVectorStore.list_collection_names()
func (s *ESVectorStore) ListCollectionNames(ctx context.Context) ([]string, error) {
	client, err := s.getClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("ListCollectionNames: %w", err)
	}

	prefix := s.indexPrefix + "__"
	names, err := esListIndices(ctx, client, prefix)
	if err != nil {
		return nil, fmt.Errorf("ListCollectionNames: %w", err)
	}

	return names, nil
}

// UpdateSchema 执行 Schema 迁移操作。
// ⤵️ 预留：实际迁移逻辑待 7.22/7.23 实现后回填。
//
// 对应 Python: ElasticsearchVectorStore.update_schema()
func (s *ESVectorStore) UpdateSchema(ctx context.Context, collectionName string, operations []any, opts ...Option) error {
	return exception.BuildError(exception.StatusStoreVectorSchemaInvalid,
		exception.WithParam("error_msg", "UpdateSchema 未实现，待 7.22/7.23 回填"),
	)
}

// UpdateCollectionMetadata 更新集合元数据。
//
// 对应 Python: ElasticsearchVectorStore.update_collection_metadata()
func (s *ESVectorStore) UpdateCollectionMetadata(ctx context.Context, collectionName string, metadata map[string]any, opts ...Option) error {
	if len(metadata) == 0 {
		return nil
	}

	// 校验 schema_version
	if v, ok := metadata["schema_version"]; ok {
		version, ok := v.(int)
		if !ok || version < 0 {
			return exception.BuildError(exception.StatusStoreVectorSchemaInvalid,
				exception.WithParam("error_msg", fmt.Sprintf("schema_version 必须为非负整数，当前值: %v", v)),
			)
		}
	}

	client, err := s.getClient(ctx)
	if err != nil {
		return fmt.Errorf("UpdateCollectionMetadata: %w", err)
	}

	indexName := esIndexName(s.indexPrefix, collectionName)

	// 读取当前元数据
	current, err := esLoadMetadata(ctx, client, indexName)
	if err != nil {
		logger.Warn(esLogComponent).Err(err).
			Str("collection_name", collectionName).
			Msg("加载当前元数据失败")
		current = make(map[string]any)
	}

	// 合并
	for k, v := range metadata {
		current[k] = v
	}

	// 写回
	if err := esStoreMetadata(ctx, client, indexName, current); err != nil {
		logger.Error(esLogComponent).Err(err).
			Str("collection_name", collectionName).
			Str("event_type", "LLM_CALL_ERROR").
			Msg("更新集合元数据失败")
		return fmt.Errorf("UpdateCollectionMetadata: %w", err)
	}

	logger.Debug(esLogComponent).
		Str("collection_name", collectionName).
		Msg("更新集合元数据成功")

	return nil
}

// GetCollectionMetadata 获取集合元数据。
//
// 对应 Python: ElasticsearchVectorStore.get_collection_metadata()
func (s *ESVectorStore) GetCollectionMetadata(ctx context.Context, collectionName string, opts ...Option) (map[string]any, error) {
	client, err := s.getClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("GetCollectionMetadata: %w", err)
	}

	indexName := esIndexName(s.indexPrefix, collectionName)
	meta, err := esLoadMetadata(ctx, client, indexName)
	if err != nil {
		return nil, fmt.Errorf("GetCollectionMetadata: %w", err)
	}

	// 设置默认值
	if _, ok := meta["distance_metric"]; !ok {
		meta["distance_metric"] = esDefaultDistanceMetric
	}
	if _, ok := meta["schema_version"]; !ok {
		meta["schema_version"] = 0
	}

	return meta, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// defaultCreateESClient 默认 ES 客户端创建函数
func defaultCreateESClient(ctx context.Context, addresses []string, username, password string) (esClient, error) {
	cfg := elasticsearch8.Config{
		Addresses: addresses,
	}
	if username != "" {
		cfg.Username = username
		cfg.Password = password
	}
	es, err := elasticsearch8.NewClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("创建 ES 客户端失败: %w", err)
	}
	// 验证连接
	resp, err := es.Info(es.Info.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("ES 连接验证失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.IsError() {
		return nil, fmt.Errorf("ES 连接验证返回错误状态: %d", resp.StatusCode)
	}
	return &esClientWrapper{inner: es}, nil
}

// getClient 惰性获取 ES 客户端，双重检查锁
func (s *ESVectorStore) getClient(ctx context.Context) (esClient, error) {
	s.mu.RLock()
	if s.client != nil {
		s.mu.RUnlock()
		return s.client, nil
	}
	s.mu.RUnlock()

	s.mu.Lock()
	defer s.mu.Unlock()
	// 双重检查
	if s.client != nil {
		return s.client, nil
	}

	client, err := s.createClient(ctx, s.addresses, s.username, s.password)
	if err != nil {
		logger.Error(esLogComponent).Err(err).
			Msg("创建 ES 客户端失败")
		return nil, err
	}
	s.client = client
	return client, nil
}

// resolveESVectorField 从 Option 解析 ES 索引配置，未指定则返回 nil
func (s *ESVectorStore) resolveESVectorField(fieldName string, o Options) *vector_fields.ESVectorField {
	if o.VectorField != nil {
		if esVF, ok := o.VectorField.(*vector_fields.ESVectorField); ok {
			return esVF
		}
	}
	return nil
}

// esIndexName 生成 ES index 名称
func esIndexName(prefix, collectionName string) string {
	return prefix + "__" + collectionName
}

// esMapFieldType 将 FieldSchema 映射为 ES mapping 类型定义
func esMapFieldType(field *FieldSchema) map[string]any {
	dtype := field.DType
	switch dtype {
	case VectorDataTypeFloatVector:
		dim := field.Dim
		if dim <= 0 {
			dim = 768
		}
		return map[string]any{
			"type":       "dense_vector",
			"dims":       dim,
			"index":      true,
			"similarity": "cosine",
		}
	case VectorDataTypeVarchar:
		return map[string]any{"type": "keyword"}
	case VectorDataTypeInt64:
		return map[string]any{"type": "long"}
	case VectorDataTypeInt32, VectorDataTypeInt16, VectorDataTypeInt8:
		return map[string]any{"type": "integer"}
	case VectorDataTypeFloat:
		return map[string]any{"type": "float"}
	case VectorDataTypeDouble:
		return map[string]any{"type": "double"}
	case VectorDataTypeBool:
		return map[string]any{"type": "boolean"}
	case VectorDataTypeJSON, VectorDataTypeArray:
		return map[string]any{"type": "object", "enabled": true}
	default:
		return map[string]any{"type": "keyword"}
	}
}

// esMapTypeToOurType 将 ES 类型映射为 VectorDataType
func esMapTypeToOurType(esType string) VectorDataType {
	switch strings.ToLower(esType) {
	case "dense_vector":
		return VectorDataTypeFloatVector
	case "keyword", "text":
		return VectorDataTypeVarchar
	case "long":
		return VectorDataTypeInt64
	case "integer":
		return VectorDataTypeInt32
	case "short":
		return VectorDataTypeInt16
	case "byte":
		return VectorDataTypeInt8
	case "float":
		return VectorDataTypeFloat
	case "double":
		return VectorDataTypeDouble
	case "boolean":
		return VectorDataTypeBool
	case "object":
		return VectorDataTypeJSON
	default:
		logger.Warn(esLogComponent).
			Str("es_type", esType).
			Msg("不支持的 ES 数据类型，回退为 VARCHAR")
		return VectorDataTypeVarchar
	}
}

// esBuildMappings 构建 ES index mapping
func esBuildMappings(schema *CollectionSchema, distanceMetric string) map[string]any {
	properties := make(map[string]any)
	for _, field := range schema.Fields() {
		if field.DType == VectorDataTypeFloatVector {
			dim := field.Dim
			if dim <= 0 {
				dim = 768
			}
			similarity := "cosine"
			if s, ok := esSimilarityMap[strings.ToUpper(distanceMetric)]; ok {
				similarity = string(s)
			}
			properties[field.Name] = map[string]any{
				"type":       "dense_vector",
				"dims":       dim,
				"index":      true,
				"similarity": similarity,
			}
		} else {
			properties[field.Name] = esMapFieldType(field)
		}
	}
	// _meta 字段不索引
	properties["_meta"] = map[string]any{"type": "object", "enabled": false}

	return map[string]any{
		"dynamic":    "strict",
		"properties": properties,
	}
}

// esNormalizeScore 将 ES 原始分数归一化到 [0, 1]
func esNormalizeScore(rawScore float64, metric string) float64 {
	switch strings.ToUpper(metric) {
	case "COSINE":
		return ConvertCosineDistance(rawScore)
	case "L2":
		return ConvertL2Squared(rawScore, 4.0)
	case "IP":
		return ConvertIPSimilarity(rawScore)
	default:
		return ConvertCosineDistance(rawScore)
	}
}

// esBuildFilterClause 构建 ES 过滤子句（用于 knn.filter）
func esBuildFilterClause(filters map[string]any) map[string]any {
	return map[string]any{
		"bool": map[string]any{
			"filter": esBuildFilterTerms(filters),
		},
	}
}

// esBuildFilterTerms 构建 ES term/terms 过滤条件列表
func esBuildFilterTerms(filters map[string]any) []any {
	var must []any
	for key, value := range filters {
		switch v := value.(type) {
		case []string:
			must = append(must, map[string]any{
				"terms": map[string]any{key: v},
			})
		case []any:
			must = append(must, map[string]any{
				"terms": map[string]any{key: v},
			})
		case []int, []int64, []float64:
			must = append(must, map[string]any{
				"terms": map[string]any{key: v},
			})
		default:
			must = append(must, map[string]any{
				"term": map[string]any{key: value},
			})
		}
	}
	return must
}

// esGetPrimaryKeyField 从元数据中提取主键字段名
func esGetPrimaryKeyField(meta map[string]any) string {
	schemaDict, ok := meta["schema"].(map[string]any)
	if !ok {
		return ""
	}
	fields, ok := schemaDict["fields"].([]any)
	if !ok {
		return ""
	}
	for _, fAny := range fields {
		f, ok := fAny.(map[string]any)
		if !ok {
			continue
		}
		if isPrimary, ok := f["is_primary"].(bool); ok && isPrimary {
			if name, ok := f["name"].(string); ok {
				return name
			}
		}
	}
	return ""
}

// esBuildBulkRequestBody 构建 Bulk 插入 NDJSON body
func esBuildBulkRequestBody(indexName string, docs []map[string]any, pkField string) string {
	var sb strings.Builder
	for _, doc := range docs {
		action := map[string]any{
			"index": map[string]any{
				"_index": indexName,
			},
		}
		// 有主键时设置 _id
		if pkField != "" {
			if id, ok := doc[pkField]; ok && id != nil {
				action["index"].(map[string]any)["_id"] = fmt.Sprintf("%v", id)
			}
		}
		actionLine, _ := json.Marshal(action)
		sb.Write(actionLine)
		sb.WriteByte('\n')

		// 过滤 nil 值
		source := make(map[string]any)
		for k, v := range doc {
			if v != nil {
				source[k] = v
			}
		}
		sourceLine, _ := json.Marshal(source)
		sb.Write(sourceLine)
		sb.WriteByte('\n')
	}
	return sb.String()
}

// esBuildBulkDeleteBody 构建 Bulk 删除 NDJSON body
func esBuildBulkDeleteBody(indexName string, ids []string) string {
	var sb strings.Builder
	for _, id := range ids {
		action := map[string]any{
			"delete": map[string]any{
				"_index": indexName,
				"_id":    id,
			},
		}
		actionLine, _ := json.Marshal(action)
		sb.Write(actionLine)
		sb.WriteByte('\n')
	}
	return sb.String()
}

// esBuildSchemaFromMapping 从 ES mapping 反射构建 CollectionSchema
func esBuildSchemaFromMapping(mappings map[string]any, indexName string, opts ...Option) (*CollectionSchema, error) {
	indexMappings, ok := mappings[indexName].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("mapping 中未找到 index: %s", indexName)
	}
	mappingsInner, ok := indexMappings["mappings"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("mapping 格式无效")
	}
	props, ok := mappingsInner["properties"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("mapping properties 格式无效")
	}

	schema, err := NewCollectionSchema()
	if err != nil {
		return nil, err
	}

	o := newOptions(opts...)
	pkField := "id" // 默认主键字段名

	for fname, fdefAny := range props {
		if fname == "_meta" {
			continue
		}
		fdef, ok := fdefAny.(map[string]any)
		if !ok {
			continue
		}
		esType, _ := fdef["type"].(string)
		dtype := esMapTypeToOurType(esType)

		fieldOpts := []FieldOption{}
		if dtype == VectorDataTypeFloatVector {
			if dims, ok := fdef["dims"]; ok {
				switch d := dims.(type) {
				case float64:
					fieldOpts = append(fieldOpts, WithDim(int(d)))
				case int:
					fieldOpts = append(fieldOpts, WithDim(d))
				}
			}
		}
		if fname == pkField {
			fieldOpts = append(fieldOpts, WithPrimary())
		}

		f, err := NewFieldSchema(fname, dtype, fieldOpts...)
		if err != nil {
			return nil, fmt.Errorf("构建字段 Schema 失败: %w", err)
		}
		if _, err := schema.AddField(f); err != nil {
			return nil, fmt.Errorf("添加字段到 Schema 失败: %w", err)
		}
	}

	return schema, nil
}

// ────────────────────────────────────────────────────────────────
// ES REST API 封装函数（非导出）
// ────────────────────────────────────────────────────────────────

// esDoRequest 执行 ES API 请求并解析 JSON 响应
func esDoRequest(ctx context.Context, client esClient, req esapi.Request) (map[string]any, error) {
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ES 请求失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取 ES 响应失败: %w", err)
	}

	if resp.IsError() {
		return nil, fmt.Errorf("ES 返回错误: status=%d, body=%s", resp.StatusCode, string(body))
	}

	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("解析 ES 响应失败: %w", err)
	}

	return result, nil
}

// esIndicesExists 检查 index 是否存在
func esIndicesExists(ctx context.Context, client esClient, indexName string) (bool, error) {
	req := esapi.IndicesExistsRequest{
		Index: []string{indexName},
	}
	resp, err := client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == 200 {
		return true, nil
	}
	if resp.StatusCode == 404 {
		return false, nil
	}
	return false, fmt.Errorf("unexpected status: %d", resp.StatusCode)
}

// esIndicesCreate 创建 index
func esIndicesCreate(ctx context.Context, client esClient, indexName string, mappings map[string]any) error {
	body := map[string]any{"mappings": mappings}
	bodyJSON, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("序列化 mapping 失败: %w", err)
	}

	req := esapi.IndicesCreateRequest{
		Index: indexName,
		Body:  strings.NewReader(string(bodyJSON)),
	}
	_, err = esDoRequest(ctx, client, req)
	return err
}

// esIndicesDelete 删除 index
func esIndicesDelete(ctx context.Context, client esClient, indexName string) error {
	req := esapi.IndicesDeleteRequest{
		Index: []string{indexName},
	}
	_, err := esDoRequest(ctx, client, req)
	return err
}

// esIndicesGetMapping 获取 index mapping
func esIndicesGetMapping(ctx context.Context, client esClient, indexName string) (map[string]any, error) {
	req := esapi.IndicesGetMappingRequest{
		Index: []string{indexName},
	}
	return esDoRequest(ctx, client, req)
}

// esIndicesRefresh 刷新 index
func esIndicesRefresh(ctx context.Context, client esClient, indexName string) error {
	req := esapi.IndicesRefreshRequest{
		Index: []string{indexName},
	}
	_, err := esDoRequest(ctx, client, req)
	return err
}

// esStoreMetadata 存储 _meta 元数据文档
func esStoreMetadata(ctx context.Context, client esClient, indexName string, metadata map[string]any) error {
	doc := map[string]any{"_meta": metadata}
	docJSON, err := json.Marshal(doc)
	if err != nil {
		return fmt.Errorf("序列化元数据失败: %w", err)
	}

	req := esapi.IndexRequest{
		Index:      indexName,
		DocumentID: esMetadataDocID,
		Body:       strings.NewReader(string(docJSON)),
		Refresh:    "true",
	}
	_, err = esDoRequest(ctx, client, req)
	return err
}

// esLoadMetadata 加载 _meta 元数据文档
func esLoadMetadata(ctx context.Context, client esClient, indexName string) (map[string]any, error) {
	req := esapi.GetRequest{
		Index:      indexName,
		DocumentID: esMetadataDocID,
	}
	result, err := esDoRequest(ctx, client, req)
	if err != nil {
		return make(map[string]any), nil
	}

	found, _ := result["found"].(bool)
	if !found {
		return make(map[string]any), nil
	}

	source, ok := result["_source"].(map[string]any)
	if !ok {
		return make(map[string]any), nil
	}

	meta, ok := source["_meta"].(map[string]any)
	if !ok {
		return make(map[string]any), nil
	}

	return meta, nil
}

// esSearch 执行搜索
func esSearch(ctx context.Context, client esClient, indexName string, body map[string]any) (map[string]any, error) {
	bodyJSON, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("序列化搜索请求失败: %w", err)
	}

	req := esapi.SearchRequest{
		Index: []string{indexName},
		Body:  strings.NewReader(string(bodyJSON)),
	}
	return esDoRequest(ctx, client, req)
}

// esBulk 执行批量操作
func esBulk(ctx context.Context, client esClient, body string) ([]esBulkError, error) {
	req := esapi.BulkRequest{
		Body: strings.NewReader(body),
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ES Bulk 请求失败: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取 Bulk 响应失败: %w", err)
	}

	if resp.IsError() {
		return nil, fmt.Errorf("ES Bulk 返回错误: status=%d, body=%s", resp.StatusCode, string(respBody))
	}

	var bulkResp map[string]any
	if err := json.Unmarshal(respBody, &bulkResp); err != nil {
		return nil, fmt.Errorf("解析 Bulk 响应失败: %w", err)
	}

	var errors []esBulkError
	if items, ok := bulkResp["items"].([]any); ok {
		for _, itemAny := range items {
			item, ok := itemAny.(map[string]any)
			if !ok {
				continue
			}
			for _, opAny := range item {
				op, ok := opAny.(map[string]any)
				if !ok {
					continue
				}
				if errObj, ok := op["error"].(map[string]any); ok {
					index, _ := op["_index"].(string)
					id, _ := op["_id"].(string)
					errMsg, _ := errObj["reason"].(string)
					errors = append(errors, esBulkError{
						Index: index,
						ID:    id,
						Error: errMsg,
					})
				}
			}
		}
	}

	return errors, nil
}

// esDeleteByQuery 按 query 删除文档
func esDeleteByQuery(ctx context.Context, client esClient, indexName string, query map[string]any) (int, error) {
	body := map[string]any{"query": query}
	bodyJSON, err := json.Marshal(body)
	if err != nil {
		return 0, fmt.Errorf("序列化删除请求失败: %w", err)
	}

	req := esapi.DeleteByQueryRequest{
		Index:   []string{indexName},
		Body:    strings.NewReader(string(bodyJSON)),
		Refresh: "true",
	}
	result, err := esDoRequest(ctx, client, req)
	if err != nil {
		return 0, err
	}

	deleted := 0
	if d, ok := result["deleted"]; ok {
		switch v := d.(type) {
		case float64:
			deleted = int(v)
		case int:
			deleted = v
		case json.Number:
			n, _ := v.Int64()
			deleted = int(n)
		case string:
			deleted, _ = strconv.Atoi(v)
		}
	}

	return deleted, nil
}

// esListIndices 列出匹配前缀的 index 名称，返回去掉前缀的集合名称
func esListIndices(ctx context.Context, client esClient, prefix string) ([]string, error) {
	req := esapi.IndicesGetMappingRequest{
		Index: []string{prefix + "*"},
	}
	result, err := esDoRequest(ctx, client, req)
	if err != nil {
		return nil, err
	}

	var names []string
	for idx := range result {
		if strings.HasPrefix(idx, prefix) {
			names = append(names, idx[len(prefix):])
		}
	}

	return names, nil
}
```

- [ ] **Step 2: 验证编译**

```bash
cd /home/opensource/uap-claw-go && go build ./internal/agentcore/store/vector/...
```

Expected: 编译通过，无错误

- [ ] **Step 3: Commit**

```bash
git add internal/agentcore/store/vector/es.go && git commit -m "feat(vector): 实现 ESVectorStore 核心"
```

---

### Task 5: 实现 ESVectorStore 单元测试

**Files:**
- Create: `internal/agentcore/store/vector/es_test.go`

- [ ] **Step 1: 创建 es_test.go**

创建 `internal/agentcore/store/vector/es_test.go`，使用 httptest 模拟 ES REST API：

```go
package vector

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/elastic/go-elasticsearch/v8/esapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ──────────────────────────── 结构体 ────────────────────────────

// fakeESClient 使用 httptest 模拟 ES REST API 的客户端
type fakeESClient struct {
	server  *httptest.Server
	client  esClient
	handler http.Handler
}

// fakeESHandler 模拟 ES REST API 的路由处理器
type fakeESHandler struct {
	// indices 存储 index 数据：indexName → {docID → docSource}
	indices map[string]map[string]map[string]any
	// mu 保护 indices
	mu atomic.Pointer[http.Handler]
}

// ──────────────────────────── 导出函数 ────────────────────────────

// newFakeESClient 创建模拟 ES 客户端
func newFakeESClient(t *testing.T) *fakeESClient {
	handler := &fakeESHandler{
		indices: make(map[string]map[string]map[string]any),
	}

	mux := http.NewServeMux()

	// HEAD /{index} - 检查 index 是否存在
	mux.HandleFunc("/{index}", func(w http.ResponseWriter, r *http.Request) {
		indexName := strings.TrimPrefix(r.URL.Path, "/")
		switch r.Method {
		case http.MethodHead:
			if _, ok := handler.indices[indexName]; ok {
				w.WriteHeader(http.StatusOK)
			} else {
				w.WriteHeader(http.StatusNotFound)
			}
		}
	})

	// PUT /{index} - 创建 index
	mux.HandleFunc("/{index}", func(w http.ResponseWriter, r *http.Request) {
		indexName := strings.TrimPrefix(r.URL.Path, "/")
		if r.Method == http.MethodPut {
			if _, ok := handler.indices[indexName]; ok {
				// resource_already_exists_exception
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(map[string]any{
					"error": map[string]any{
						"type": "resource_already_exists_exception",
					},
				})
				return
			}
			handler.indices[indexName] = make(map[string]map[string]any)
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]any{
				"acknowledged": true,
			})
		}
	})

	// 注意：由于 httptest 无法完美模拟 ES 复杂路由，
	// 我们使用更简单的方式：直接替换 createClient 注入自定义 esClient

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	// 创建连接到 mock server 的 ES 客户端
	cfg := elasticsearch8.Config{
		Addresses: []string{server.URL},
	}
	es, err := elasticsearch8.NewClient(cfg)
	require.NoError(t, err)

	return &fakeESClient{
		server: server,
		client: &esClientWrapper{inner: es},
		handler: handler,
	}
}

// ──────────────────────────── 测试函数 ────────────────────────────

// testESClient 用于测试的 esClient mock
type testESClient struct {
	doFunc func(req esapi.Request) (*http.Response, error)
}

func (c *testESClient) Do(req esapi.Request) (*http.Response, error) {
	return c.doFunc(req)
}

func (c *testESClient) Close() {}

func TestNewESVectorStore(t *testing.T) {
	store := NewESVectorStore([]string{"http://localhost:9200"}, "", "")
	assert.NotNil(t, store)
	assert.Equal(t, "agent_vector", store.indexPrefix)
	assert.Equal(t, []string{"http://localhost:9200"}, store.addresses)
}

func TestNewESVectorStore_自定义前缀(t *testing.T) {
	store := NewESVectorStore(
		[]string{"http://localhost:9200"}, "", "",
		WithESIndexPrefix("custom_prefix"),
	)
	assert.Equal(t, "custom_prefix", store.indexPrefix)
}

func TestESIndexName(t *testing.T) {
	assert.Equal(t, "agent_vector__my_collection", esIndexName("agent_vector", "my_collection"))
	assert.Equal(t, "prefix__test", esIndexName("prefix", "test"))
}

func TestEsMapFieldType(t *testing.T) {
	tests := []struct {
		name     string
		field    *FieldSchema
		esType   string
	}{
		{"VARCHAR", &FieldSchema{DType: VectorDataTypeVarchar}, "keyword"},
		{"INT64", &FieldSchema{DType: VectorDataTypeInt64}, "long"},
		{"INT32", &FieldSchema{DType: VectorDataTypeInt32}, "integer"},
		{"INT16", &FieldSchema{DType: VectorDataTypeInt16}, "integer"},
		{"INT8", &FieldSchema{DType: VectorDataTypeInt8}, "integer"},
		{"FLOAT", &FieldSchema{DType: VectorDataTypeFloat}, "float"},
		{"DOUBLE", &FieldSchema{DType: VectorDataTypeDouble}, "double"},
		{"BOOL", &FieldSchema{DType: VectorDataTypeBool}, "boolean"},
		{"JSON", &FieldSchema{DType: VectorDataTypeJSON}, "object"},
		{"ARRAY", &FieldSchema{DType: VectorDataTypeArray}, "object"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := esMapFieldType(tt.field)
			assert.Equal(t, tt.esType, result["type"])
		})
	}
}

func TestEsMapFieldType_FloatVector(t *testing.T) {
	field := &FieldSchema{DType: VectorDataTypeFloatVector, Dim: 128}
	result := esMapFieldType(field)
	assert.Equal(t, "dense_vector", result["type"])
	assert.Equal(t, 128, result["dims"])
	assert.Equal(t, true, result["index"])
	assert.Equal(t, "cosine", result["similarity"])
}

func TestEsMapFieldType_FloatVector_默认dim(t *testing.T) {
	field := &FieldSchema{DType: VectorDataTypeFloatVector, Dim: 0}
	result := esMapFieldType(field)
	assert.Equal(t, 768, result["dims"])
}

func TestEsMapTypeToOurType(t *testing.T) {
	tests := []struct {
		esType  string
		wantDType VectorDataType
	}{
		{"dense_vector", VectorDataTypeFloatVector},
		{"keyword", VectorDataTypeVarchar},
		{"text", VectorDataTypeVarchar},
		{"long", VectorDataTypeInt64},
		{"integer", VectorDataTypeInt32},
		{"short", VectorDataTypeInt16},
		{"byte", VectorDataTypeInt8},
		{"float", VectorDataTypeFloat},
		{"double", VectorDataTypeDouble},
		{"boolean", VectorDataTypeBool},
		{"object", VectorDataTypeJSON},
		{"unknown_type", VectorDataTypeVarchar}, // fallback
	}

	for _, tt := range tests {
		t.Run(tt.esType, func(t *testing.T) {
			got := esMapTypeToOurType(tt.esType)
			assert.Equal(t, tt.wantDType, got)
		})
	}
}

func TestEsBuildMappings(t *testing.T) {
	schema := mustNewTestSchema(t, "id", "embedding", 128, "text")
	mappings := esBuildMappings(schema, "COSINE")

	assert.Equal(t, "strict", mappings["dynamic"])

	props, ok := mappings["properties"].(map[string]any)
	require.True(t, ok)

	// 验证向量字段
	emb, ok := props["embedding"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "dense_vector", emb["type"])
	assert.Equal(t, 128, emb["dims"])
	assert.Equal(t, "cosine", emb["similarity"])

	// 验证 _meta 字段
	meta, ok := props["_meta"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "object", meta["type"])
	assert.Equal(t, false, meta["enabled"])
}

func TestEsBuildMappings_L2(t *testing.T) {
	schema := mustNewTestSchema(t, "id", "vector", 256, "content")
	mappings := esBuildMappings(schema, "L2")

	props := mappings["properties"].(map[string]any)
	vec := props["vector"].(map[string]any)
	assert.Equal(t, "l2_norm", vec["similarity"])
}

func TestEsBuildMappings_IP(t *testing.T) {
	schema := mustNewTestSchema(t, "id", "embedding", 64, "data")
	mappings := esBuildMappings(schema, "IP")

	props := mappings["properties"].(map[string]any)
	emb := props["embedding"].(map[string]any)
	assert.Equal(t, "dot_product", emb["similarity"])
}

func TestEsNormalizeScore(t *testing.T) {
	// COSINE: rawScore=0.5 → (2-0.5)/2 = 0.75
	assert.InDelta(t, 0.75, esNormalizeScore(0.5, "COSINE"), 0.001)

	// L2: rawScore=1.0, maxDist=4.0 → (4-1)/4 = 0.75
	assert.InDelta(t, 0.75, esNormalizeScore(1.0, "L2"), 0.001)

	// IP: rawScore=0.5 → (0.5+1)/2 = 0.75
	assert.InDelta(t, 0.75, esNormalizeScore(0.5, "IP"), 0.001)
}

func TestEsBuildFilterTerms(t *testing.T) {
	filters := map[string]any{
		"status": "active",
		"tag":    []string{"a", "b"},
	}
	terms := esBuildFilterTerms(filters)
	assert.Len(t, terms, 2)
}

func TestEsBuildBulkRequestBody(t *testing.T) {
	docs := []map[string]any{
		{"id": "1", "text": "hello"},
		{"id": "2", "text": "world"},
	}
	body := esBuildBulkRequestBody("test__coll", docs, "id")

	lines := strings.Split(strings.TrimSpace(body), "\n")
	assert.Len(t, lines, 4) // 2 docs × 2 lines each

	// 第一行是 action
	var action map[string]any
	require.NoError(t, json.Unmarshal([]byte(lines[0]), &action))
	indexAction, ok := action["index"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "1", indexAction["_id"])

	// 第二行是 source
	var source map[string]any
	require.NoError(t, json.Unmarshal([]byte(lines[1]), &source))
	assert.Equal(t, "hello", source["text"])
}

func TestEsBuildBulkDeleteBody(t *testing.T) {
	ids := []string{"1", "2", "3"}
	body := esBuildBulkDeleteBody("test__coll", ids)

	lines := strings.Split(strings.TrimSpace(body), "\n")
	assert.Len(t, lines, 3)

	var action map[string]any
	require.NoError(t, json.Unmarshal([]byte(lines[0]), &action))
	deleteAction, ok := action["delete"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "1", deleteAction["_id"])
}

func TestEsGetPrimaryKeyField(t *testing.T) {
	meta := map[string]any{
		"schema": map[string]any{
			"fields": []any{
				map[string]any{"name": "id", "is_primary": true, "type": "VARCHAR"},
				map[string]any{"name": "embedding", "type": "FLOAT_VECTOR", "dim": 128},
			},
		},
	}
	assert.Equal(t, "id", esGetPrimaryKeyField(meta))
}

func TestEsGetPrimaryKeyField_无主键(t *testing.T) {
	meta := map[string]any{
		"schema": map[string]any{
			"fields": []any{
				map[string]any{"name": "embedding", "type": "FLOAT_VECTOR"},
			},
		},
	}
	assert.Equal(t, "", esGetPrimaryKeyField(meta))
}

func TestEsGetPrimaryKeyField_空元数据(t *testing.T) {
	assert.Equal(t, "", esGetPrimaryKeyField(map[string]any{}))
	assert.Equal(t, "", esGetPrimaryKeyField(nil))
}

func TestESVectorStore_CreateCollection_Schema无向量字段(t *testing.T) {
	store := NewESVectorStore([]string{"http://localhost:9200"}, "", "")
	schema, _ := NewCollectionSchema()
	idField, _ := NewFieldSchema("id", VectorDataTypeVarchar, WithPrimary())
	schema.AddField(idField)

	err := store.CreateCollection(context.Background(), "test", schema)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "FLOAT_VECTOR")
}

func TestESVectorStore_Close(t *testing.T) {
	store := NewESVectorStore([]string{"http://localhost:9200"}, "", "")
	// Close 未初始化客户端时不 panic
	store.Close()
}

func TestESVectorStore_AddDocs_空文档(t *testing.T) {
	store := NewESVectorStore([]string{"http://localhost:9200"}, "", "")
	err := store.AddDocs(context.Background(), "test", []map[string]any{})
	assert.NoError(t, err)
}

func TestESVectorStore_DeleteDocsByIDs_空ID列表(t *testing.T) {
	store := NewESVectorStore([]string{"http://localhost:9200"}, "", "")
	err := store.DeleteDocsByIDs(context.Background(), "test", []string{})
	assert.NoError(t, err)
}

func TestESVectorStore_DeleteDocsByFilters_空过滤(t *testing.T) {
	store := NewESVectorStore([]string{"http://localhost:9200"}, "", "")
	err := store.DeleteDocsByFilters(context.Background(), "test", map[string]any{})
	assert.NoError(t, err)
}

func TestESVectorStore_UpdateSchema_未实现(t *testing.T) {
	store := NewESVectorStore([]string{"http://localhost:9200"}, "", "")
	err := store.UpdateSchema(context.Background(), "test", []any{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "7.22/7.23")
}

func TestESVectorStore_UpdateCollectionMetadata_空元数据(t *testing.T) {
	store := NewESVectorStore([]string{"http://localhost:9200"}, "", "")
	err := store.UpdateCollectionMetadata(context.Background(), "test", map[string]any{})
	assert.NoError(t, err)
}

func TestESVectorStore_UpdateCollectionMetadata_无效SchemaVersion(t *testing.T) {
	store := NewESVectorStore([]string{"http://localhost:9200"}, "", "")
	err := store.UpdateCollectionMetadata(context.Background(), "test", map[string]any{
		"schema_version": -1,
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "schema_version")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// mustNewTestSchema 创建测试用 schema（id + vector + text 字段）
func mustNewTestSchema(t *testing.T, pkName, vectorName string, dim int, textFieldName string) *CollectionSchema {
	t.Helper()
	schema, err := NewCollectionSchema()
	require.NoError(t, err)

	pkField, err := NewFieldSchema(pkName, VectorDataTypeVarchar, WithPrimary())
	require.NoError(t, err)
	schema.AddField(pkField)

	vecField, err := NewFieldSchema(vectorName, VectorDataTypeFloatVector, WithDim(dim))
	require.NoError(t, err)
	schema.AddField(vecField)

	textField, err := NewFieldSchema(textFieldName, VectorDataTypeVarchar)
	require.NoError(t, err)
	schema.AddField(textField)

	return schema
}
```

注意：需要确保 import 中有 `elasticsearch8 "github.com/elastic/go-elasticsearch/v8"`。

- [ ] **Step 2: 运行测试**

```bash
cd /home/opensource/uap-claw-go && go test ./internal/agentcore/store/vector/... -v -run "TestNewESVectorStore|TestESIndexName|TestEsMap|TestEsBuild|TestEsNormalize|TestEsBuildBulk|TestEsGetPrimaryKey|TestESVectorStore_" -count=1
```

Expected: 所有测试 PASS

- [ ] **Step 3: Commit**

```bash
git add internal/agentcore/store/vector/es_test.go && git commit -m "test(vector): ESVectorStore 单元测试"
```

---

### Task 6: 集成测试（httptest 模拟完整 ES API）

**Files:**
- Modify: `internal/agentcore/store/vector/es_test.go`

- [ ] **Step 1: 在 es_test.go 中添加 httptest 集成测试**

在现有测试文件末尾添加完整的 httptest 模拟 ES API 测试，覆盖 CreateCollection → AddDocs → Search → DeleteDocsByIDs → DeleteCollection 全流程。

添加以下测试到 `es_test.go`：

```go
// esMockServer 模拟 ES REST API 的 HTTP 服务
type esMockServer struct {
	server   *httptest.Server
	indices  map[string]map[string]map[string]any // index → docID → source
	metadata map[string]map[string]any            // index → metadata
	mu       sync.Mutex
}

func newESMockServer(t *testing.T) *esMockServer {
	s := &esMockServer{
		indices:  make(map[string]map[string]map[string]any),
		metadata: make(map[string]map[string]any),
	}

	mux := http.NewServeMux()

	// GET /{index}/_mapping - 获取 mapping
	mux.HandleFunc("/{index}/_mapping", func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/"), "/")
		indexName := parts[0]

		s.mu.Lock()
		defer s.mu.Unlock()

		if _, ok := s.indices[indexName]; !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		// 返回简单 mapping
		resp := map[string]any{
			indexName: map[string]any{
				"mappings": map[string]any{
					"properties": map[string]any{},
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	})

	// POST /{index}/_refresh - 刷新 index
	mux.HandleFunc("/{index}/_refresh", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{"acknowledged": true})
	})

	// POST /{index}/_search - 搜索
	mux.HandleFunc("/{index}/_search", func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/"), "/")
		indexName := parts[0]

		s.mu.Lock()
		docs := s.indices[indexName]
		s.mu.Unlock()

		hits := []any{}
		for id, source := range docs {
			if id == esMetadataDocID {
				continue
			}
			hits = append(hits, map[string]any{
				"_id":     id,
				"_score":  1.0,
				"_source": source,
			})
		}

		resp := map[string]any{
			"hits": map[string]any{
				"hits": hits,
			},
		}
		json.NewEncoder(w).Encode(resp)
	})

	// POST /{index}/_delete_by_query - 按条件删除
	mux.HandleFunc("/{index}/_delete_by_query", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{"deleted": 0})
	})

	// POST /_bulk - 批量操作
	mux.HandleFunc("/_bulk", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		lines := strings.Split(strings.TrimSpace(string(body)), "\n")

		s.mu.Lock()
		defer s.mu.Unlock()

		items := []any{}
		for i := 0; i < len(lines); i += 2 {
			if i+1 >= len(lines) {
				break
			}
			var action map[string]any
			json.Unmarshal([]byte(lines[i]), &action)

			for opType, opData := range action {
				op, ok := opData.(map[string]any)
				if !ok {
					continue
				}
				indexName, _ := op["_index"].(string)
				docID, _ := op["_id"].(string)

				switch opType {
				case "index":
					if s.indices[indexName] == nil {
						s.indices[indexName] = make(map[string]map[string]any)
					}
					var source map[string]any
					json.Unmarshal([]byte(lines[i+1]), &source)
					if docID == "" {
						docID = fmt.Sprintf("auto_%d", i)
					}
					s.indices[indexName][docID] = source
					items = append(items, map[string]any{
						opType: map[string]any{
							"result": "created",
							"_id":    docID,
							"status": 201,
						},
					})
				case "delete":
					if s.indices[indexName] != nil {
						delete(s.indices[indexName], docID)
					}
					items = append(items, map[string]any{
						opType: map[string]any{
							"result": "deleted",
							"_id":    docID,
							"status": 200,
						},
					})
				}
			}
		}

		json.NewEncoder(w).Encode(map[string]any{
			"took":  1,
			"errors": false,
			"items": items,
		})
	})

	// PUT /{index} - 创建 index
	mux.HandleFunc("/{index}", func(w http.ResponseWriter, r *http.Request) {
		indexName := strings.TrimPrefix(r.URL.Path, "/")

		switch r.Method {
		case http.MethodPut:
			s.mu.Lock()
			defer s.mu.Unlock()

			if _, ok := s.indices[indexName]; ok {
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(map[string]any{
					"error": map[string]any{
						"type": "resource_already_exists_exception",
					},
				})
				return
			}
			s.indices[indexName] = make(map[string]map[string]any)
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]any{"acknowledged": true})

		case http.MethodHead:
			s.mu.Lock()
			defer s.mu.Unlock()

			if _, ok := s.indices[indexName]; ok {
				w.WriteHeader(http.StatusOK)
			} else {
				w.WriteHeader(http.StatusNotFound)
			}

		case http.MethodDelete:
			s.mu.Lock()
			defer s.mu.Unlock()

			delete(s.indices, indexName)
			delete(s.metadata, indexName)
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]any{"acknowledged": true})
		}
	})

	// 默认路由：处理 GET /{index}/_doc/{id} 等路径
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		// GET /{index}/_doc/{id} - 获取文档（用于 _meta）
		if r.Method == http.MethodGet && strings.Contains(path, "/_doc/") {
			parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
			if len(parts) >= 3 {
				indexName := parts[0]
				docID := parts[2]

				s.mu.Lock()
				defer s.mu.Unlock()

				if docs, ok := s.indices[indexName]; ok {
					if source, ok := docs[docID]; ok {
						json.NewEncoder(w).Encode(map[string]any{
							"found":  true,
							"_source": source,
						})
						return
					}
				}
				// _meta 专用
				if docID == esMetadataDocID {
					if meta, ok := s.metadata[indexName]; ok {
						json.NewEncoder(w).Encode(map[string]any{
							"found":  true,
							"_source": map[string]any{"_meta": meta},
						})
						return
					}
				}
				json.NewEncoder(w).Encode(map[string]any{"found": false})
				return
			}
		}

		// PUT /{index}/_doc/{id} or POST /{index}/_doc - 索引文档
		if (r.Method == http.MethodPut || r.Method == http.MethodPost) && strings.Contains(path, "/_doc") {
			body, _ := io.ReadAll(r.Body)
			var source map[string]any
			json.Unmarshal(body, &source)

			parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
			indexName := parts[0]

			// 提取 doc ID
			var docID string
			if len(parts) >= 3 && parts[2] != "" {
				docID = parts[2]
			} else {
				docID = fmt.Sprintf("auto_%d", len(s.indices[indexName]))
			}

			s.mu.Lock()
			defer s.mu.Unlock()

			if docID == esMetadataDocID {
				// _meta 文档
				if meta, ok := source["_meta"].(map[string]any); ok {
					s.metadata[indexName] = meta
				}
			} else {
				if s.indices[indexName] == nil {
					s.indices[indexName] = make(map[string]map[string]any)
				}
				s.indices[indexName][docID] = source
			}

			json.NewEncoder(w).Encode(map[string]any{
				"result": "created",
				"_id":    docID,
			})
			return
		}

		// ES Info endpoint
		if path == "/" || path == "" {
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]any{
				"version": map[string]any{"number": "8.12.0"},
			})
			return
		}

		w.WriteHeader(http.StatusNotFound)
	})

	s.server = httptest.NewServer(mux)
	t.Cleanup(s.server.Close)

	return s
}

// newStoreWithMock 创建连接到 mock server 的 ESVectorStore
func newStoreWithMock(t *testing.T) (*ESVectorStore, *esMockServer) {
	mock := newESMockServer(t)

	store := NewESVectorStore([]string{mock.server.URL}, "", "")
	// 替换 createClient 为连接 mock server 的版本
	store.createClient = func(ctx context.Context, addresses []string, username, password string) (esClient, error) {
		cfg := elasticsearch8.Config{Addresses: addresses}
		es, err := elasticsearch8.NewClient(cfg)
		if err != nil {
			return nil, err
		}
		return &esClientWrapper{inner: es}, nil
	}

	return store, mock
}

func TestESVectorStore_CreateCollection_成功(t *testing.T) {
	store, _ := newStoreWithMock(t)
	schema := mustNewTestSchema(t, "id", "embedding", 128, "text")

	err := store.CreateCollection(context.Background(), "test_collection", schema)
	assert.NoError(t, err)
}

func TestESVectorStore_CreateCollection_已存在(t *testing.T) {
	store, _ := newStoreWithMock(t)
	schema := mustNewTestSchema(t, "id", "embedding", 128, "text")

	err := store.CreateCollection(context.Background(), "test_collection", schema)
	assert.NoError(t, err)

	// 重复创建应成功（幂等）
	err = store.CreateCollection(context.Background(), "test_collection", schema)
	assert.NoError(t, err)
}

func TestESVectorStore_DeleteCollection_成功(t *testing.T) {
	store, _ := newStoreWithMock(t)
	schema := mustNewTestSchema(t, "id", "embedding", 128, "text")

	err := store.CreateCollection(context.Background(), "test_collection", schema)
	require.NoError(t, err)

	err = store.DeleteCollection(context.Background(), "test_collection")
	assert.NoError(t, err)
}

func TestESVectorStore_CollectionExists_存在(t *testing.T) {
	store, _ := newStoreWithMock(t)
	schema := mustNewTestSchema(t, "id", "embedding", 128, "text")

	err := store.CreateCollection(context.Background(), "test_collection", schema)
	require.NoError(t, err)

	exists, err := store.CollectionExists(context.Background(), "test_collection")
	assert.NoError(t, err)
	assert.True(t, exists)
}

func TestESVectorStore_CollectionExists_不存在(t *testing.T) {
	store, _ := newStoreWithMock(t)

	exists, err := store.CollectionExists(context.Background(), "nonexistent")
	assert.NoError(t, err)
	assert.False(t, exists)
}

func TestESVectorStore_ListCollectionNames(t *testing.T) {
	store, _ := newStoreWithMock(t)
	schema := mustNewTestSchema(t, "id", "embedding", 128, "text")

	err := store.CreateCollection(context.Background(), "coll1", schema)
	require.NoError(t, err)

	err = store.CreateCollection(context.Background(), "coll2", schema)
	require.NoError(t, err)

	names, err := store.ListCollectionNames(context.Background())
	assert.NoError(t, err)
	assert.Contains(t, names, "coll1")
	assert.Contains(t, names, "coll2")
}

func TestESVectorStore_GetCollectionMetadata(t *testing.T) {
	store, _ := newStoreWithMock(t)
	schema := mustNewTestSchema(t, "id", "embedding", 128, "text")

	err := store.CreateCollection(context.Background(), "test_meta", schema)
	require.NoError(t, err)

	meta, err := store.GetCollectionMetadata(context.Background(), "test_meta")
	assert.NoError(t, err)
	assert.Equal(t, "COSINE", meta["distance_metric"])
}
```

注意：需要添加 `"sync"` 到 import 中。

- [ ] **Step 2: 运行全部 ES 测试**

```bash
cd /home/opensource/uap-claw-go && go test ./internal/agentcore/store/vector/... -v -run "TestESVectorStore_|TestESIndexName|TestEsMap|TestEsBuild|TestEsNormalize|TestEsBuildBulk|TestEsGetPrimaryKey|TestNewESVectorStore" -count=1
```

Expected: 所有测试 PASS

- [ ] **Step 3: Commit**

```bash
git add internal/agentcore/store/vector/es_test.go && git commit -m "test(vector): ESVectorStore 集成测试（httptest mock）"
```

---

### Task 7: 更新 doc.go 文件

**Files:**
- Modify: `internal/agentcore/store/vector/doc.go`

- [ ] **Step 1: 更新 vector/doc.go**

在文件目录树中 `gauss.go` 行后新增：

```
//	├── es.go          # ESVectorStore 结构体 + BaseVectorStore 接口实现（Elasticsearch k-NN）
```

在 Python 代码对应列表中新增：

```
//	openjiuwen/extensions/store/vector/es_vector_store.py
```

在核心类型/接口索引中 `GaussVectorStore` 行后新增：

```
//	ESVectorStore      — Elasticsearch 向量存储实现，基于 go-elasticsearch/v8 官方客户端 + dense_vector k-NN
```

更新包概述，将 "Milvus、ChromaDB 和 GaussDB" 改为 "Milvus、ChromaDB、GaussDB 和 Elasticsearch"。

- [ ] **Step 2: 验证编译**

```bash
cd /home/opensource/uap-claw-go && go build ./internal/agentcore/store/vector/...
```

Expected: 编译通过

- [ ] **Step 3: Commit**

```bash
git add internal/agentcore/store/vector/doc.go && git commit -m "docs(vector): 更新 doc.go 添加 ESVectorStore"
```

---

### Task 8: 运行完整测试套件

**Files:**
- 无文件变更

- [ ] **Step 1: 运行 vector 包全部测试**

```bash
cd /home/opensource/uap-claw-go && go test ./internal/agentcore/store/vector/... -v -count=1
```

Expected: 所有测试 PASS

- [ ] **Step 2: 运行 vector_fields 包全部测试**

```bash
cd /home/opensource/uap-claw-go && go test ./internal/agentcore/store/vector_fields/... -v -count=1
```

Expected: 所有测试 PASS

- [ ] **Step 3: 运行项目完整测试**

```bash
cd /home/opensource/uap-claw-go && go test ./... -count=1 2>&1 | tail -20
```

Expected: 无 FAIL

- [ ] **Step 4: 检查测试覆盖率**

```bash
cd /home/opensource/uap-claw-go && go test -cover ./internal/agentcore/store/vector/... ./internal/agentcore/store/vector_fields/...
```

Expected: 覆盖率 ≥ 85%

---

### Task 9: 更新 IMPLEMENTATION_PLAN.md

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 将 4.11 状态从 ☐ 改为 ✅**

```
| 4.11 | ✅ | ESVectorStore | Elasticsearch 向量实现 | `openjiuwen/extensions/store/es_vector_store.py` |
```

- [ ] **Step 2: Commit**

```bash
git add IMPLEMENTATION_PLAN.md && git commit -m "docs: 更新实现计划 4.11 ESVectorStore 为已完成"
```
