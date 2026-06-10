# ChromaVectorStore 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 ChromaVectorStore，即 BaseVectorStore 接口的 ChromaDB 后端，使用 Persistent Client 本地嵌入式模式。

**Architecture:** 在现有 `vector/` 包下新增 `chroma.go`，定义 `chromaClient`/`chromaCollection` 接口隔离 `amikos-tech/chroma-go` SDK V2 API，通过 `createClient` 函数注入实现可测试性。field_mapping 存入 ChromaDB collection metadata + 本地内存缓存。Search 结果根据 distance_metric 调用 `utils.go` 中已有的 Chroma 转换函数归一化分数。

**Tech Stack:** Go 1.25, `amikos-tech/chroma-go` v0.4.x (V2 API `pkg/api/v2`), `ebitengine/purego` (无 CGO)

**Design Spec:** `docs/superpowers/specs/2026-06-11-chroma-vector-store-design.md`

**Python Reference:** `openjiuwen/core/foundation/store/vector/chroma_vector_store.py`

---

## File Structure

| File | Action | Responsibility |
|------|--------|---------------|
| `go.mod` | Modify | 添加 `amikos-tech/chroma-go` 依赖 |
| `internal/agentcore/store/vector/chroma.go` | Create | ChromaVectorStore 完整实现 + chromaClient/chromaCollection 接口 + fieldMapping + 结果类型 |
| `internal/agentcore/store/vector/chroma_test.go` | Create | 单元测试：fakeChromaClient + fakeChromaCollection，覆盖所有方法和错误路径 |
| `internal/agentcore/store/vector/chroma_integration_test.go` | Create | 集成测试：`//go:build integration`，使用真实 PersistentClient |
| `internal/agentcore/store/vector/doc.go` | Modify | 更新文件目录和核心类型索引 |

---

### Task 1: 添加 chroma-go SDK 依赖

**Files:**
- Modify: `go.mod`
- Modify: `go.sum`

- [ ] **Step 1: 添加 chroma-go 依赖**

Run:
```bash
cd /home/opensource/uap-claw-go && go get github.com/amikos-tech/chroma-go@latest
```

- [ ] **Step 2: 验证依赖添加成功**

Run:
```bash
cd /home/opensource/uap-claw-go && go mod tidy && grep "amikos-tech/chroma-go" go.mod
```
Expected: 输出包含 `github.com/amikos-tech/chroma-go v0.4.x`

- [ ] **Step 3: 提交**

```bash
git add go.mod go.sum
git commit -m "chore: 添加 amikos-tech/chroma-go SDK 依赖"
```

---

### Task 2: 创建 chroma.go — 接口定义 + 结构体 + 构造函数

**Files:**
- Create: `internal/agentcore/store/vector/chroma.go`

这是核心文件的骨架，包含所有类型定义、接口、构造函数和 Close 方法。后续 Task 逐个添加 BaseVectorStore 方法实现。

- [ ] **Step 1: 编写 chroma.go 骨架代码**

创建文件 `internal/agentcore/store/vector/chroma.go`，包含：

```go
package vector

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	chromav2 "github.com/amikos-tech/chroma-go/pkg/api/v2"
	"github.com/amikos-tech/chroma-go/pkg/embeddings"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// chromaClient ChromaDB 客户端操作接口（用于解耦和测试）。
// 生产代码使用 chromav2.Client，测试代码注入 fakeChromaClient。
type chromaClient interface {
	GetOrCreateCollection(ctx context.Context, name string, opts ...chromav2.CreateCollectionOption) (chromav2.Collection, error)
	GetCollection(ctx context.Context, name string, opts ...chromav2.GetCollectionOption) (chromav2.Collection, error)
	DeleteCollection(ctx context.Context, name string, opts ...chromav2.DeleteCollectionOption) error
	ListCollections(ctx context.Context, opts ...chromav2.ListCollectionsOption) ([]chromav2.Collection, error)
	Close() error
}

// chromaCollection ChromaDB 集合操作接口（用于解耦和测试）。
// 生产代码使用 chromav2.Collection，测试代码注入 fakeChromaCollection。
type chromaCollection interface {
	Add(ctx context.Context, opts ...chromav2.CollectionAddOption) error
	Query(ctx context.Context, opts ...chromav2.CollectionQueryOption) (chromav2.QueryResult, error)
	Get(ctx context.Context, opts ...chromav2.CollectionGetOption) (chromav2.GetResult, error)
	Delete(ctx context.Context, opts ...chromav2.CollectionDeleteOption) error
	ModifyMetadata(ctx context.Context, newMetadata chromav2.CollectionMetadata) error
	Count(ctx context.Context) (int, error)
	Metadata() chromav2.CollectionMetadata
	Name() string
}

// fieldMapping 用户字段名到 ChromaDB 内置字段的映射及距离度量缓存。
//
// 对应 Python: ChromaVectorStore._field_mapping
type fieldMapping struct {
	// PrimaryKey 映射到 ChromaDB 的 ids
	PrimaryKey string
	// VectorField 映射到 ChromaDB 的 embeddings
	VectorField string
	// TextField 映射到 ChromaDB 的 documents（可为空）
	TextField string
	// DistanceMetric 距离度量（cosine/l2/ip），Search 时用于选择转换函数
	DistanceMetric string
}

// ChromaVectorStore 基于 ChromaDB 的向量存储实现。
// 使用 Persistent Client（本地嵌入式），数据持久化到指定目录。
//
// 实现 BaseVectorStore 接口，使用 amikos-tech/chroma-go 作为客户端。
// 客户端惰性创建，初始化时不需要 ChromaDB 可用。
//
// 对应 Python: vector/chroma_vector_store.py (ChromaVectorStore)
type ChromaVectorStore struct {
	// client ChromaDB 客户端实例
	client chromaClient
	// persistDirectory 持久化目录
	persistDirectory string
	// collectionCache collection 对象缓存
	collectionCache map[string]chromav2.Collection
	// fieldMappings 本地 field_mapping 缓存
	fieldMappings map[string]*fieldMapping
	// mu 读写锁，保护客户端和缓存
	mu sync.RWMutex
	// createClient 客户端创建函数，用于依赖注入和测试
	createClient func(persistDir string) (chromaClient, error)
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// defaultChromaBatchSize 默认批量插入大小
	defaultChromaBatchSize = 128
	// defaultChromaDistanceMetric 默认距离度量方式
	defaultChromaDistanceMetric = "cosine"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewChromaVectorStore 创建 ChromaVectorStore 实例。
// persistDirectory 为持久化目录，为空时使用内存模式。
// 客户端惰性创建，初始化时不需要 ChromaDB 可用。
//
// 对应 Python: ChromaVectorStore.__init__(persist_directory)
func NewChromaVectorStore(persistDirectory string) *ChromaVectorStore {
	return &ChromaVectorStore{
		persistDirectory: persistDirectory,
		collectionCache:  make(map[string]chromav2.Collection),
		fieldMappings:    make(map[string]*fieldMapping),
		createClient:     defaultCreateChromaClient,
	}
}

// Close 关闭 ChromaDB 客户端连接。
//
// 对应 Python: ChromaVectorStore.close()
func (s *ChromaVectorStore) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.client != nil {
		_ = s.client.Close()
		s.client = nil
		logger.Info(logComponent).Str("action", "close").Msg("ChromaDB 客户端连接已关闭")
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// defaultCreateChromaClient 默认的客户端创建函数，使用 chroma-go PersistentClient。
func defaultCreateChromaClient(persistDir string) (chromaClient, error) {
	opts := []chromav2.PersistentClientOption{
		chromav2.WithPersistentLibraryAutoDownload(true),
	}
	if persistDir != "" {
		opts = append(opts, chromav2.WithPersistentPath(persistDir))
	}
	client, err := chromav2.NewPersistentClient(opts...)
	if err != nil {
		return nil, err
	}
	// chromav2.Client 接口兼容我们的 chromaClient 接口
	// 通过 chromaClientAdapter 适配
	return &chromaClientAdapter{Client: client}, nil
}

// chromaClientAdapter 适配 chromav2.Client 到 chromaClient 接口。
type chromaClientAdapter struct {
	chromav2.Client
}

// ensure getFieldMapping helper will be added in next task
```

> **注意**：`chromaClientAdapter` 使用内嵌 `chromav2.Client`，由于 `chromav2.Client` 接口的方法签名与 `chromaClient` 接口不完全匹配（如 `GetCollection` 返回值签名差异），需要在 adapter 中逐方法包装。实际实现时根据 SDK 签名精确适配。

- [ ] **Step 2: 验证编译通过**

Run:
```bash
cd /home/opensource/uap-claw-go && go build ./internal/agentcore/store/vector/...
```
Expected: 编译通过（可能有未实现接口方法的错误，后续 Task 逐步修复）

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/store/vector/chroma.go
git commit -m "feat(vector): 添加 ChromaVectorStore 骨架 — 接口定义+结构体+构造函数"
```

---

### Task 3: 实现 chroma.go — getClient + getFieldMapping + 辅助函数

**Files:**
- Modify: `internal/agentcore/store/vector/chroma.go`

- [ ] **Step 1: 实现 getClient（惰性创建客户端）**

在 `chroma.go` 的非导出函数区块添加：

```go
// getClient 惰性获取或创建 ChromaDB 客户端。
func (s *ChromaVectorStore) getClient(ctx context.Context) (chromaClient, error) {
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

	c, err := s.createClient(s.persistDirectory)
	if err != nil {
		logger.Error(logComponent).Err(err).Str("persist_directory", s.persistDirectory).Msg("连接 ChromaDB 失败")
		return nil, exception.BuildError(exception.StatusStoreVectorCollectionNotFound,
			exception.WithParam("error_msg", fmt.Sprintf("failed to connect to ChromaDB: %v", err)),
		)
	}
	s.client = c
	logger.Info(logComponent).Str("persist_directory", s.persistDirectory).Msg("成功连接 ChromaDB")
	return s.client, nil
}
```

- [ ] **Step 2: 实现 getOrLoadCollection（获取或加载 collection 缓存）**

```go
// getOrLoadCollection 获取或加载 collection 对象。
func (s *ChromaVectorStore) getOrLoadCollection(ctx context.Context, collectionName string) (chromav2.Collection, error) {
	s.mu.RLock()
	if coll, ok := s.collectionCache[collectionName]; ok {
		s.mu.RUnlock()
		return coll, nil
	}
	s.mu.RUnlock()

	c, err := s.getClient(ctx)
	if err != nil {
		return nil, err
	}

	coll, err := c.GetCollection(ctx, collectionName)
	if err != nil {
		logger.Error(logComponent).Err(err).Str("collection_name", collectionName).Msg("获取集合失败")
		return nil, exception.BuildError(exception.StatusStoreVectorCollectionNotFound,
			exception.WithParam("collection_name", collectionName),
		)
	}

	s.mu.Lock()
	s.collectionCache[collectionName] = coll
	s.mu.Unlock()

	return coll, nil
}
```

- [ ] **Step 3: 实现 getFieldMapping（获取或推断 field_mapping）**

```go
// getFieldMapping 获取集合的 field_mapping，优先从缓存读取，缓存未命中从 metadata 读取或推断。
func (s *ChromaVectorStore) getFieldMapping(ctx context.Context, collectionName string) (*fieldMapping, error) {
	s.mu.RLock()
	if fm, ok := s.fieldMappings[collectionName]; ok {
		s.mu.RUnlock()
		return fm, nil
	}
	s.mu.RUnlock()

	coll, err := s.getOrLoadCollection(ctx, collectionName)
	if err != nil {
		return nil, err
	}

	// 从 collection metadata 读取
	fm, err := fieldMappingFromMetadata(coll.Metadata())
	if err == nil && fm != nil {
		s.mu.Lock()
		s.fieldMappings[collectionName] = fm
		s.mu.Unlock()
		return fm, nil
	}

	// 从 schema 推断
	schema, err := s.GetSchema(ctx, collectionName)
	if err != nil {
		return nil, err
	}
	fm = inferFieldMapping(schema)
	s.mu.Lock()
	s.fieldMappings[collectionName] = fm
	s.mu.Unlock()
	return fm, nil
}

// fieldMappingFromMetadata 从 collection metadata 解析 field_mapping。
func fieldMappingFromMetadata(meta chromav2.CollectionMetadata) (*fieldMapping, error) {
	if meta == nil {
		return nil, fmt.Errorf("metadata is nil")
	}
	// chromav2.CollectionMetadata 是 map[string]any 的包装
	fmStr, ok := meta["field_mapping"]
	if !ok {
		return nil, fmt.Errorf("field_mapping not found in metadata")
	}
	fmJSON, ok := fmStr.(string)
	if !ok {
		return nil, fmt.Errorf("field_mapping is not a string")
	}
	var fm fieldMapping
	if err := json.Unmarshal([]byte(fmJSON), &fm); err != nil {
		return nil, fmt.Errorf("failed to unmarshal field_mapping: %w", err)
	}
	return &fm, nil
}

// inferFieldMapping 从 CollectionSchema 推断 field_mapping。
func inferFieldMapping(schema *CollectionSchema) *fieldMapping {
	fm := &fieldMapping{
		DistanceMetric: defaultChromaDistanceMetric,
	}
	for _, field := range schema.Fields() {
		if field.IsPrimary {
			fm.PrimaryKey = field.Name
		}
		if field.DType == VectorDataTypeFloatVector && fm.VectorField == "" {
			fm.VectorField = field.Name
		}
		if field.DType == VectorDataTypeVarchar && !field.IsPrimary && fm.TextField == "" {
			fm.TextField = field.Name
		}
	}
	return fm
}
```

- [ ] **Step 4: 实现 buildChromaMetadata（构建 ChromaDB collection metadata）**

```go
// buildChromaMetadata 构建要存入 ChromaDB collection 的 metadata。
func buildChromaMetadata(schema *CollectionSchema, fm *fieldMapping, distanceMetric string) map[string]any {
	schemaJSON, _ := json.Marshal(schema.ToDict())
	fmJSON, _ := json.Marshal(fm)
	return map[string]any{
		"schema":          string(schemaJSON),
		"field_mapping":   string(fmJSON),
		"distance_metric": distanceMetric,
		"vector_field":    fm.VectorField,
	}
}
```

- [ ] **Step 5: 验证编译通过**

Run:
```bash
cd /home/opensource/uap-claw-go && go build ./internal/agentcore/store/vector/...
```

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/store/vector/chroma.go
git commit -m "feat(vector): 实现 ChromaVectorStore getClient + getFieldMapping + 辅助函数"
```

---

### Task 4: 实现 chroma.go — CreateCollection + DeleteCollection + CollectionExists

**Files:**
- Modify: `internal/agentcore/store/vector/chroma.go`

- [ ] **Step 1: 实现 CreateCollection**

在导出函数区块添加：

```go
// CreateCollection 创建集合。
// 如果集合已存在则跳过创建。schema 定义字段结构，opts 可指定 DistanceMetric。
//
// 对应 Python: ChromaVectorStore.create_collection(collection_name, schema, **kwargs)
func (s *ChromaVectorStore) CreateCollection(ctx context.Context, collectionName string, schema *CollectionSchema, opts ...Option) error {
	o := newOptions(opts...)
	c, err := s.getClient(ctx)
	if err != nil {
		return err
	}

	// 校验 schema：必须有主键字段
	primaryField := schema.GetPrimaryKeyField()
	if primaryField == nil {
		return exception.BuildError(exception.StatusStoreVectorSchemaInvalid,
			exception.WithParam("error_msg", "schema must contain a primary key field (is_primary=True)"),
		)
	}

	// 校验 schema：必须有 FLOAT_VECTOR 字段
	vectorFields := schema.GetVectorFields()
	if len(vectorFields) == 0 {
		return exception.BuildError(exception.StatusStoreVectorSchemaInvalid,
			exception.WithParam("error_msg", "schema must contain at least one FLOAT_VECTOR field"),
		)
	}

	distanceMetric := o.DistanceMetric
	if distanceMetric == "" {
		distanceMetric = defaultChromaDistanceMetric
	}
	// ChromaDB 使用小写距离度量
	chromaMetric := normalizeChromaMetric(distanceMetric)

	// 构建 field_mapping
	fm := &fieldMapping{
		PrimaryKey:     primaryField.Name,
		VectorField:    vectorFields[0].Name,
		TextField:      "", // 从 schema 中找第一个非主键 VARCHAR 字段
		DistanceMetric: chromaMetric,
	}
	for _, f := range schema.Fields() {
		if f.DType == VectorDataTypeVarchar && !f.IsPrimary {
			fm.TextField = f.Name
			break
		}
	}

	// 构建 metadata
	metadataMap := buildChromaMetadata(schema, fm, chromaMetric)
	metadata, err := chromav2.NewMetadataFromMap(metadataMap)
	if err != nil {
		logger.Error(logComponent).Err(err).Str("collection_name", collectionName).Msg("构建 ChromaDB metadata 失败")
		return err
	}

	// 构建距离度量
	spaceFunc, err := mapChromaDistanceMetric(chromaMetric)
	if err != nil {
		return err
	}

	// 创建集合
	coll, err := c.GetOrCreateCollection(ctx, collectionName,
		chromav2.WithCollectionMetadataCreate(metadata),
		chromav2.WithHNSWSpaceCreate(spaceFunc),
	)
	if err != nil {
		logger.Error(logComponent).Err(err).Str("collection_name", collectionName).Msg("创建集合失败")
		return err
	}

	// 缓存 collection 和 field_mapping
	s.mu.Lock()
	s.collectionCache[collectionName] = coll
	s.fieldMappings[collectionName] = fm
	s.mu.Unlock()

	logger.Info(logComponent).Str("collection_name", collectionName).
		Int("field_count", len(schema.Fields())).
		Msg("成功创建集合")
	return nil
}
```

- [ ] **Step 2: 实现 DeleteCollection**

```go
// DeleteCollection 删除集合。
//
// 对应 Python: ChromaVectorStore.delete_collection(collection_name)
func (s *ChromaVectorStore) DeleteCollection(ctx context.Context, collectionName string, opts ...Option) error {
	c, err := s.getClient(ctx)
	if err != nil {
		return err
	}

	if err := c.DeleteCollection(ctx, collectionName); err != nil {
		logger.Error(logComponent).Err(err).Str("collection_name", collectionName).Msg("删除集合失败")
		return err
	}

	// 清除缓存
	s.mu.Lock()
	delete(s.collectionCache, collectionName)
	delete(s.fieldMappings, collectionName)
	s.mu.Unlock()

	logger.Info(logComponent).Str("collection_name", collectionName).Msg("成功删除集合")
	return nil
}
```

- [ ] **Step 3: 实现 CollectionExists**

```go
// CollectionExists 检查集合是否存在。
//
// 对应 Python: ChromaVectorStore.collection_exists(collection_name)
func (s *ChromaVectorStore) CollectionExists(ctx context.Context, collectionName string, opts ...Option) (bool, error) {
	c, err := s.getClient(ctx)
	if err != nil {
		return false, err
	}
	_, err = c.GetCollection(ctx, collectionName)
	if err != nil {
		return false, nil
	}
	return true, nil
}
```

- [ ] **Step 4: 实现辅助函数 normalizeChromaMetric + mapChromaDistanceMetric**

```go
// normalizeChromaMetric 将距离度量标准化为 ChromaDB 格式。
func normalizeChromaMetric(metric string) string {
	m := strings.ToLower(strings.TrimSpace(metric))
	// dot -> ip, euclidean -> l2
	switch m {
	case "dot":
		return "ip"
	case "euclidean":
		return "l2"
	default:
		return m
	}
}

// mapChromaDistanceMetric 将字符串距离度量映射为 SDK 的 DistanceMetric 类型。
func mapChromaDistanceMetric(metric string) (embeddings.DistanceMetric, error) {
	switch metric {
	case "cosine":
		return embeddings.Cosine, nil
	case "l2":
		return embeddings.L2, nil
	case "ip":
		return embeddings.IP, nil
	default:
		return embeddings.Cosine, nil
	}
}
```

- [ ] **Step 5: 验证编译通过**

Run:
```bash
cd /home/opensource/uap-claw-go && go build ./internal/agentcore/store/vector/...
```

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/store/vector/chroma.go
git commit -m "feat(vector): 实现 ChromaVectorStore CreateCollection + DeleteCollection + CollectionExists"
```

---

### Task 5: 实现 chroma.go — GetSchema + GetCollectionMetadata + UpdateCollectionMetadata + ListCollectionNames

**Files:**
- Modify: `internal/agentcore/store/vector/chroma.go`

- [ ] **Step 1: 实现 GetSchema**

```go
// GetSchema 获取集合的 Schema。
// 优先从 ChromaDB collection metadata 读取，fallback 到从 field_mapping 推断。
//
// 对应 Python: ChromaVectorStore.get_schema(collection_name)
func (s *ChromaVectorStore) GetSchema(ctx context.Context, collectionName string, opts ...Option) (*CollectionSchema, error) {
	coll, err := s.getOrLoadCollection(ctx, collectionName)
	if err != nil {
		return nil, err
	}

	meta := coll.Metadata()
	if meta != nil {
		// 尝试从 metadata 的 "schema" 字段反序列化
		if schemaStr, ok := meta["schema"]; ok {
			if schemaJSON, ok := schemaStr.(string); ok {
				var schemaDict map[string]any
				if err := json.Unmarshal([]byte(schemaJSON), &schemaDict); err == nil {
					return CollectionFromDict(schemaDict)
				}
			}
		}
		// 尝试从 "fields" 字段反序列化（Python 版兼容）
		if fieldsStr, ok := meta["fields"]; ok {
			if fieldsJSON, ok := fieldsStr.(string); ok {
				var schemaDict map[string]any
				if err := json.Unmarshal([]byte(fieldsJSON), &schemaDict); err == nil {
					return CollectionFromDict(schemaDict)
				}
			}
		}
	}

	// Fallback：从 field_mapping 构建默认 schema
	fm, err := s.getFieldMapping(ctx, collectionName)
	if err != nil {
		return nil, err
	}
	return buildDefaultSchema(collectionName, fm), nil
}

// buildDefaultSchema 从 fieldMapping 构建默认 schema。
func buildDefaultSchema(collectionName string, fm *fieldMapping) *CollectionSchema {
	schema, _ := NewCollectionSchema(
		WithCollectionDescription(fmt.Sprintf("Collection '%s'", collectionName)),
	)
	schema.EnableDynamicField = true

	pkField, _ := NewFieldSchema(fm.PrimaryKey, VectorDataTypeVarchar, WithMaxLength(256), WithPrimary())
	schema.AddField(pkField)

	vecField, _ := NewFieldSchema(fm.VectorField, VectorDataTypeFloatVector)
	schema.AddField(vecField)

	if fm.TextField != "" {
		textField, _ := NewFieldSchema(fm.TextField, VectorDataTypeVarchar, WithMaxLength(65535))
		schema.AddField(textField)
	}

	return schema
}
```

- [ ] **Step 2: 实现 GetCollectionMetadata**

```go
// GetCollectionMetadata 获取集合元数据。
//
// 对应 Python: ChromaVectorStore.get_collection_metadata(collection_name)
func (s *ChromaVectorStore) GetCollectionMetadata(ctx context.Context, collectionName string, opts ...Option) (map[string]any, error) {
	coll, err := s.getOrLoadCollection(ctx, collectionName)
	if err != nil {
		return nil, err
	}

	meta := coll.Metadata()
	result := make(map[string]any)

	// 从 metadata 中提取
	if meta != nil {
		for k, v := range meta {
			result[k] = v
		}
	}

	// 确保默认值
	if _, ok := result["distance_metric"]; !ok {
		result["distance_metric"] = defaultChromaDistanceMetric
	}
	if _, ok := result["schema_version"]; !ok {
		result["schema_version"] = 0
	}

	return result, nil
}
```

- [ ] **Step 3: 实现 UpdateCollectionMetadata**

```go
// UpdateCollectionMetadata 更新集合元数据。
//
// 对应 Python: ChromaVectorStore.update_collection_metadata(collection_name, metadata)
func (s *ChromaVectorStore) UpdateCollectionMetadata(ctx context.Context, collectionName string, metadata map[string]any, opts ...Option) error {
	if len(metadata) == 0 {
		return nil
	}

	// 校验 schema_version
	if v, ok := metadata["schema_version"]; ok {
		version, ok := v.(int)
		if !ok || version < 0 {
			return exception.BuildError(exception.StatusStoreVectorSchemaInvalid,
				exception.WithParam("error_msg", fmt.Sprintf("schema_version must be a non-negative integer, got %v", v)),
			)
		}
	}

	coll, err := s.getOrLoadCollection(ctx, collectionName)
	if err != nil {
		return err
	}

	// 合并现有 metadata
	currentMeta := coll.Metadata()
	mergedMap := make(map[string]any)
	if currentMeta != nil {
		for k, v := range currentMeta {
			mergedMap[k] = v
		}
	}
	for k, v := range metadata {
		mergedMap[k] = v
	}

	newMeta, err := chromav2.NewMetadataFromMap(mergedMap)
	if err != nil {
		logger.Error(logComponent).Err(err).Str("collection_name", collectionName).Msg("构建更新 metadata 失败")
		return err
	}

	if err := coll.ModifyMetadata(ctx, newMeta); err != nil {
		logger.Error(logComponent).Err(err).Str("collection_name", collectionName).Msg("更新集合 metadata 失败")
		return err
	}

	// 同步更新本地 field_mapping 缓存中的 DistanceMetric
	if dm, ok := metadata["distance_metric"]; ok {
		if dmStr, ok := dm.(string); ok {
			s.mu.Lock()
			if fm, ok := s.fieldMappings[collectionName]; ok {
				fm.DistanceMetric = dmStr
			}
			s.mu.Unlock()
		}
	}

	logger.Info(logComponent).Str("collection_name", collectionName).
		Interface("metadata", metadata).Msg("成功更新集合 metadata")
	return nil
}
```

- [ ] **Step 4: 实现 ListCollectionNames**

```go
// ListCollectionNames 列出所有集合名称。
//
// 对应 Python: ChromaVectorStore.list_collection_names()
func (s *ChromaVectorStore) ListCollectionNames(ctx context.Context) ([]string, error) {
	c, err := s.getClient(ctx)
	if err != nil {
		return nil, err
	}
	colls, err := c.ListCollections(ctx)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(colls))
	for _, coll := range colls {
		names = append(names, coll.Name())
	}
	return names, nil
}
```

- [ ] **Step 5: 验证编译通过**

Run:
```bash
cd /home/opensource/uap-claw-go && go build ./internal/agentcore/store/vector/...
```

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/store/vector/chroma.go
git commit -m "feat(vector): 实现 ChromaVectorStore GetSchema + metadata 方法 + ListCollectionNames"
```

---

### Task 6: 实现 chroma.go — AddDocs + Search + DeleteDocsByIDs + DeleteDocsByFilters

**Files:**
- Modify: `internal/agentcore/store/vector/chroma.go`

- [ ] **Step 1: 实现 AddDocs**

```go
// AddDocs 添加文档到集合。支持批量插入，通过 BatchSize 控制批次大小。
//
// 对应 Python: ChromaVectorStore.add_docs(collection_name, docs, **kwargs)
func (s *ChromaVectorStore) AddDocs(ctx context.Context, collectionName string, docs []map[string]any, opts ...Option) error {
	if len(docs) == 0 {
		return nil
	}

	o := newOptions(opts...)
	batchSize := o.BatchSize
	if batchSize <= 0 {
		batchSize = defaultChromaBatchSize
	}

	coll, err := s.getOrLoadCollection(ctx, collectionName)
	if err != nil {
		return err
	}

	fm, err := s.getFieldMapping(ctx, collectionName)
	if err != nil {
		return err
	}

	total := len(docs)

	for i := 0; i < total; i += batchSize {
		end := i + batchSize
		if end > total {
			end = total
		}
		batch := docs[i:end]

		// 提取 ids, embeddings, documents, metadatas
		ids := make([]string, 0, len(batch))
		embList := make([]embeddings.Embedding, 0, len(batch))
		docList := make([]string, 0, len(batch))
		metaList := make([]map[string]any, 0, len(batch))

		for _, doc := range batch {
			// 提取主键
			docID, ok := doc[fm.PrimaryKey]
			if !ok {
				return exception.BuildError(exception.StatusStoreVectorDocInvalid,
					exception.WithParam("error_msg", fmt.Sprintf("document must have primary field '%s'", fm.PrimaryKey)),
				)
			}
			ids = append(ids, fmt.Sprintf("%v", docID))

			// 提取向量
			embRaw, ok := doc[fm.VectorField]
			if !ok {
				return exception.BuildError(exception.StatusStoreVectorDocInvalid,
					exception.WithParam("error_msg", fmt.Sprintf("document must have vector field '%s'", fm.VectorField)),
				)
			}
			emb := toFloat32Slice(embRaw)
			embList = append(embList, emb)

			// 提取文本
			text := ""
			if fm.TextField != "" {
				if t, ok := doc[fm.TextField]; ok {
					text = fmt.Sprintf("%v", t)
				}
			}
			docList = append(docList, text)

			// 构建 metadata（排除主键、向量字段、文本字段）
			meta := make(map[string]any)
			for k, v := range doc {
				if k == fm.PrimaryKey || k == fm.VectorField || k == fm.TextField {
					continue
				}
				switch v := v.(type) {
				case string, int, float64, bool:
					meta[k] = v
				case []any, map[string]any:
					jsonBytes, _ := json.Marshal(v)
					meta[k] = string(jsonBytes)
				default:
					meta[k] = fmt.Sprintf("%v", v)
				}
			}
			metaList = append(metaList, meta)
		}

		// 调用 ChromaDB Add
		addOpts := []chromav2.CollectionAddOption{
			chromav2.WithIDs(ids...),
			chromav2.WithEmbeddings(embList...),
			chromav2.WithTexts(docList...),
		}
		if len(metaList) > 0 {
			addOpts = append(addOpts, chromav2.WithMetadatas(metaList...))
		}

		if err := coll.Add(ctx, addOpts...); err != nil {
			logger.Error(logComponent).Err(err).Str("collection_name", collectionName).
				Int("batch_start", i).Int("batch_size", len(batch)).Msg("插入文档批次失败")
			return err
		}

		logger.Info(logComponent).Str("collection_name", collectionName).
			Int("processed", end).Int("total", total).Msg("添加文档批次进度")
	}

	logger.Info(logComponent).Str("collection_name", collectionName).
		Int("data_num", total).Msg("成功添加文档到集合")
	return nil
}
```

- [ ] **Step 2: 实现 toFloat32Slice 辅助函数**

```go
// toFloat32Slice 将 []float64 或 []any 转为 embeddings.Embedding ([]float32)。
func toFloat32Slice(v any) embeddings.Embedding {
	switch arr := v.(type) {
	case []float64:
		result := make([]float32, len(arr))
		for i, f := range arr {
			result[i] = float32(f)
		}
		return result
	case []any:
		result := make([]float32, len(arr))
		for i, f := range arr {
			if f64, ok := f.(float64); ok {
				result[i] = float32(f64)
			}
		}
		return result
	default:
		return nil
	}
}
```

- [ ] **Step 3: 实现 Search**

```go
// Search 向量相似度搜索。
//
// 对应 Python: ChromaVectorStore.search(collection_name, query_vector, vector_field, top_k, filters, **kwargs)
func (s *ChromaVectorStore) Search(ctx context.Context, collectionName string, queryVector []float64, vectorField string, topK int, filters map[string]any, opts ...Option) ([]VectorSearchResult, error) {
	if topK <= 0 {
		topK = 5
	}

	coll, err := s.getOrLoadCollection(ctx, collectionName)
	if err != nil {
		return nil, err
	}

	fm, err := s.getFieldMapping(ctx, collectionName)
	if err != nil {
		return nil, err
	}

	// 转换查询向量为 SDK 格式
	queryEmb := make([]float32, len(queryVector))
	for i, v := range queryVector {
		queryEmb[i] = float32(v)
	}

	// 构建 Query 选项
	queryOpts := []chromav2.CollectionQueryOption{
		chromav2.WithQueryEmbeddings(embeddings.Embedding(queryEmb)),
		chromav2.WithNResults(topK),
		chromav2.WithInclude(chromav2.IncludeDocuments, chromav2.IncludeMetadatas, chromav2.IncludeDistances),
	}

	// 添加 where 过滤
	if len(filters) > 0 {
		whereFilter := buildChromaWhereFilter(filters)
		queryOpts = append(queryOpts, chromav2.WithWhere(whereFilter))
	}

	results, err := coll.Query(ctx, queryOpts...)
	if err != nil {
		logger.Error(logComponent).Err(err).Str("collection_name", collectionName).Msg("向量搜索失败")
		return nil, err
	}

	// 转换结果
	searchResults := make([]VectorSearchResult, 0)
	if results != nil && results.CountGroups() > 0 {
		idGroups := results.GetIDGroups()
		docGroups := results.GetDocumentsGroups()
		metaGroups := results.GetMetadatasGroups()
		distGroups := results.GetDistancesGroups()

		if len(idGroups) > 0 {
			for idx, idGroup := range idGroups[0] {
				// 计算归一化分数
				var score float64
				if len(distGroups) > 0 && len(distGroups[0]) > idx {
					rawDist := float64(distGroups[0][idx])
					score = normalizeChromaScore(rawDist, fm.DistanceMetric)
				}

				// 映射回用户字段名
				fields := make(map[string]any)
				fields[fm.PrimaryKey] = string(idGroup)

				if fm.TextField != "" && len(docGroups) > 0 && len(docGroups[0]) > idx {
					fields[fm.TextField] = string(docGroups[0][idx])
				}

				if len(metaGroups) > 0 && len(metaGroups[0]) > idx {
					meta := metaGroups[0][idx]
					for k, v := range meta {
						// 尝试解析 JSON 字符串
						if str, ok := v.(string); ok {
							var parsed any
							if err := json.Unmarshal([]byte(str), &parsed); err == nil {
								fields[k] = parsed
							} else {
								fields[k] = v
							}
						} else {
							fields[k] = v
						}
					}
				}

				searchResults = append(searchResults, VectorSearchResult{
					Score:  score,
					Fields: fields,
				})
			}
		}
	}

	logger.Info(logComponent).Str("collection_name", collectionName).
		Int("result_count", len(searchResults)).Msg("向量搜索完成")
	return searchResults, nil
}
```

- [ ] **Step 4: 实现 normalizeChromaScore + buildChromaWhereFilter**

```go
// normalizeChromaScore 根据 distance_metric 将 ChromaDB 原始距离转换为归一化相似度 [0, 1]。
func normalizeChromaScore(rawDist float64, metric string) float64 {
	switch metric {
	case "cosine":
		return ConvertCosineDistance(rawDist)
	case "l2":
		return ConvertL2Squared(rawDist, 4.0)
	case "ip":
		return ConvertIPDistance(rawDist)
	default:
		return ConvertCosineDistance(rawDist)
	}
}

// buildChromaWhereFilter 从过滤条件字典构建 ChromaDB WhereFilter。
func buildChromaWhereFilter(filters map[string]any) chromav2.WhereFilter {
	// ChromaDB SDK 使用 EqString/EqInt 等 helper 构建 where 过滤
	// 简单实现：仅支持等值过滤
	var conditions []chromav2.WhereFilter
	for key, value := range filters {
		switch v := value.(type) {
		case string:
			conditions = append(conditions, chromav2.EqString(key, v))
		case int:
			conditions = append(conditions, chromav2.EqInt(key, v))
		case float64:
			conditions = append(conditions, chromav2.EqFloat(key, v))
		}
	}
	if len(conditions) == 1 {
		return conditions[0]
	}
	if len(conditions) > 1 {
		return chromav2.And(conditions...)
	}
	return nil
}
```

- [ ] **Step 5: 实现 DeleteDocsByIDs**

```go
// DeleteDocsByIDs 按 ID 删除文档。
//
// 对应 Python: ChromaVectorStore.delete_docs_by_ids(collection_name, ids)
func (s *ChromaVectorStore) DeleteDocsByIDs(ctx context.Context, collectionName string, ids []string, opts ...Option) error {
	if len(ids) == 0 {
		logger.Warn(logComponent).Str("collection_name", collectionName).Msg("未提供删除 ID")
		return nil
	}

	coll, err := s.getOrLoadCollection(ctx, collectionName)
	if err != nil {
		return err
	}

	if err := coll.Delete(ctx, chromav2.WithIDs(ids...)); err != nil {
		logger.Error(logComponent).Err(err).Str("collection_name", collectionName).
			Int("id_count", len(ids)).Msg("按 ID 删除文档失败")
		return err
	}

	logger.Info(logComponent).Str("collection_name", collectionName).
		Int("id_count", len(ids)).Msg("成功按 ID 删除文档")
	return nil
}
```

- [ ] **Step 6: 实现 DeleteDocsByFilters**

```go
// DeleteDocsByFilters 按标量字段过滤条件删除文档。
//
// 对应 Python: ChromaVectorStore.delete_docs_by_filters(collection_name, filters)
func (s *ChromaVectorStore) DeleteDocsByFilters(ctx context.Context, collectionName string, filters map[string]any, opts ...Option) error {
	if len(filters) == 0 {
		logger.Warn(logComponent).Str("collection_name", collectionName).Msg("未提供过滤条件")
		return nil
	}

	coll, err := s.getOrLoadCollection(ctx, collectionName)
	if err != nil {
		return err
	}

	whereFilter := buildChromaWhereFilter(filters)
	if err := coll.Delete(ctx, chromav2.WithWhere(whereFilter)); err != nil {
		logger.Error(logComponent).Err(err).Str("collection_name", collectionName).Msg("按过滤条件删除文档失败")
		return err
	}

	logger.Info(logComponent).Str("collection_name", collectionName).Msg("成功按过滤条件删除文档")
	return nil
}
```

- [ ] **Step 7: 验证编译通过**

Run:
```bash
cd /home/opensource/uap-claw-go && go build ./internal/agentcore/store/vector/...
```

- [ ] **Step 8: 提交**

```bash
git add internal/agentcore/store/vector/chroma.go
git commit -m "feat(vector): 实现 ChromaVectorStore AddDocs + Search + Delete 方法"
```

---

### Task 7: 实现 chroma.go — UpdateSchema (placeholder) + GetAllDocuments

**Files:**
- Modify: `internal/agentcore/store/vector/chroma.go`

- [ ] **Step 1: 实现 UpdateSchema (placeholder)**

```go
// UpdateSchema 执行 schema 迁移操作。
// ⤵️ 预留：实际迁移逻辑待 7.22/7.23 实现后回填。
//
// 对应 Python: ChromaVectorStore.update_schema(collection_name, operations)
func (s *ChromaVectorStore) UpdateSchema(ctx context.Context, collectionName string, operations []any, opts ...Option) error {
	// TODO: ⤵️ 回填，待 7.22/7.23 实现后补全
	logger.Warn(logComponent).Str("collection_name", collectionName).Msg("UpdateSchema 尚未实现，待 7.22/7.23 回填")
	return exception.BuildError(exception.StatusStoreVectorSchemaInvalid,
		exception.WithParam("error_msg", "UpdateSchema is not yet implemented, pending 7.22/7.23"),
	)
}
```

- [ ] **Step 2: 实现 GetAllDocuments**

```go
// GetAllDocuments 获取集合中的所有文档（含向量），用于迁移场景。
//
// 对应 Python: ChromaVectorStore.get_all_documents(collection_name)
func (s *ChromaVectorStore) GetAllDocuments(ctx context.Context, collectionName string) ([]map[string]any, error) {
	coll, err := s.getOrLoadCollection(ctx, collectionName)
	if err != nil {
		return nil, err
	}

	fm, err := s.getFieldMapping(ctx, collectionName)
	if err != nil {
		return nil, err
	}

	results, err := coll.Get(ctx,
		chromav2.WithInclude(chromav2.IncludeDocuments, chromav2.IncludeMetadatas, chromav2.IncludeEmbeddings),
	)
	if err != nil {
		logger.Error(logComponent).Err(err).Str("collection_name", collectionName).Msg("获取全部文档失败")
		return nil, err
	}

	documents := make([]map[string]any, 0)
	if results == nil {
		return documents, nil
	}

	ids := results.GetIDs()
	docs := results.GetDocuments()
	metas := results.GetMetadatas()
	embs := results.GetEmbeddings()

	for i := 0; i < results.Count(); i++ {
		doc := make(map[string]any)

		// 主键
		if i < len(ids) {
			doc[fm.PrimaryKey] = string(ids[i])
		}

		// 文本字段
		if fm.TextField != "" && i < len(docs) {
			doc[fm.TextField] = string(docs[i])
		}

		// 向量字段
		if i < len(embs) {
			doc[fm.VectorField] = embs[i]
		}

		// metadata
		if i < len(metas) {
			for k, v := range metas[i] {
				doc[k] = v
			}
		}

		documents = append(documents, doc)
	}

	logger.Info(logComponent).Str("collection_name", collectionName).
		Int("data_num", len(documents)).Msg("获取全部文档完成")
	return documents, nil
}
```

- [ ] **Step 3: 验证编译通过**

Run:
```bash
cd /home/opensource/uap-claw-go && go build ./internal/agentcore/store/vector/...
```

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/store/vector/chroma.go
git commit -m "feat(vector): 实现 ChromaVectorStore UpdateSchema(placeholder) + GetAllDocuments"
```

---

### Task 8: 完善 chromaClientAdapter 适配器

**Files:**
- Modify: `internal/agentcore/store/vector/chroma.go`

SDK V2 的 `Client` 接口方法签名与我们的 `chromaClient` 接口可能不完全匹配，需要精确适配。这一步需要根据编译错误调整。

- [ ] **Step 1: 编译查看错误**

Run:
```bash
cd /home/opensource/uap-claw-go && go build ./internal/agentcore/store/vector/... 2>&1
```

- [ ] **Step 2: 根据 SDK V2 API 精确适配 chromaClientAdapter**

逐方法包装 `chromav2.Client` 到 `chromaClient` 接口。关键差异：
- SDK `ListCollections` 返回 `[]chromav2.Collection`，我们的接口也返回此类型，可直接透传
- SDK `GetCollection` 需要 `...chromav2.GetCollectionOption` 参数
- SDK `DeleteCollection` 需要 `...chromav2.DeleteCollectionOption` 参数

根据实际编译错误逐一修复，确保 adapter 完整实现 `chromaClient` 接口。

- [ ] **Step 3: 验证编译通过**

Run:
```bash
cd /home/opensource/uap-claw-go && go build ./internal/agentcore/store/vector/...
```

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/store/vector/chroma.go
git commit -m "fix(vector): 完善 ChromaVectorStore chromaClientAdapter 适配器"
```

---

### Task 9: 更新 doc.go

**Files:**
- Modify: `internal/agentcore/store/vector/doc.go`

- [ ] **Step 1: 更新 doc.go 文件目录和核心类型索引**

在文件目录中添加 `chroma.go` 条目，在核心类型索引中添加 `ChromaVectorStore`，在对应 Python 代码中添加 chroma 路径。更新包功能概述，增加 ChromaDB 相关描述。

- [ ] **Step 2: 验证编译通过**

Run:
```bash
cd /home/opensource/uap-claw-go && go build ./internal/agentcore/store/vector/...
```

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/store/vector/doc.go
git commit -m "docs(vector): 更新 doc.go 添加 ChromaVectorStore"
```

---

### Task 10: 创建 chroma_test.go — fake 实现 + 基础测试

**Files:**
- Create: `internal/agentcore/store/vector/chroma_test.go`

- [ ] **Step 1: 编写 fakeChromaClient + fakeChromaCollection**

```go
package vector

import (
	"context"
	"sync"

	chromav2 "github.com/amikos-tech/chroma-go/pkg/api/v2"
	"github.com/amikos-tech/chroma-go/pkg/embeddings"
)

// ──────────────────────────── 结构体 ────────────────────────────

// fakeChromaClient 用于测试的 ChromaDB 客户端模拟
type fakeChromaClient struct {
	collections map[string]*fakeChromaCollection
	mu          sync.RWMutex
}

// fakeChromaCollection 用于测试的 ChromaDB 集合模拟
type fakeChromaCollection struct {
	name     string
	metadata chromav2.CollectionMetadata
	docs     []fakeDoc
}

// fakeDoc 模拟文档
type fakeDoc struct {
	id         string
	embedding  embeddings.Embedding
	document   string
	metadata   map[string]any
}

func newFakeChromaClient() *fakeChromaClient {
	return &fakeChromaClient{
		collections: make(map[string]*fakeChromaCollection),
	}
}

// 实现 chromaClient 接口的所有方法...
// GetOrCreateCollection, GetCollection, DeleteCollection, ListCollections, Close
```

> 完整实现需根据 `chromaClient` 接口最终签名逐方法编写。参考 `milvus_test.go` 中 `fakeMilvusClient` 的模式。

- [ ] **Step 2: 编写 NewChromaVectorStore 和 Close 测试**

```go
// TestNewChromaVectorStore 测试创建 ChromaVectorStore 实例
func TestNewChromaVectorStore(t *testing.T) {
	s := NewChromaVectorStore("/tmp/test-chroma")
	if s == nil {
		t.Fatal("NewChromaVectorStore 返回 nil")
	}
	if s.persistDirectory != "/tmp/test-chroma" {
		t.Errorf("persistDirectory = %q, want /tmp/test-chroma", s.persistDirectory)
	}
}

// TestNewChromaVectorStore_空目录 测试空持久化目录
func TestNewChromaVectorStore_空目录(t *testing.T) {
	s := NewChromaVectorStore("")
	if s == nil {
		t.Fatal("NewChromaVectorStore 返回 nil")
	}
	if s.persistDirectory != "" {
		t.Errorf("persistDirectory = %q, want empty", s.persistDirectory)
	}
}
```

- [ ] **Step 3: 运行测试验证通过**

Run:
```bash
cd /home/opensource/uap-claw-go && go test -run "TestNewChromaVectorStore" ./internal/agentcore/store/vector/... -v
```

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/store/vector/chroma_test.go
git commit -m "test(vector): 添加 ChromaVectorStore fake 实现 + 基础测试"
```

---

### Task 11: 补充 chroma_test.go — CreateCollection + DeleteCollection + CollectionExists 测试

**Files:**
- Modify: `internal/agentcore/store/vector/chroma_test.go`

- [ ] **Step 1: 编写 CreateCollection 测试**

覆盖：
- 正常创建（含 schema 校验：主键 + 向量字段）
- 无主键字段报错
- 无向量字段报错
- 集合已存在（跳过创建）
- 自定义 distance_metric

- [ ] **Step 2: 编写 DeleteCollection 测试**

覆盖：
- 正常删除
- 删除后缓存清除

- [ ] **Step 3: 编写 CollectionExists 测试**

覆盖：
- 存在返回 true
- 不存在返回 false

- [ ] **Step 4: 运行测试验证通过**

Run:
```bash
cd /home/opensource/uap-claw-go && go test -run "TestChroma.*Collection" ./internal/agentcore/store/vector/... -v
```

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/store/vector/chroma_test.go
git commit -m "test(vector): 添加 ChromaVectorStore CreateCollection/DeleteCollection/CollectionExists 测试"
```

---

### Task 12: 补充 chroma_test.go — AddDocs + Search + Delete 测试

**Files:**
- Modify: `internal/agentcore/store/vector/chroma_test.go`

- [ ] **Step 1: 编写 AddDocs 测试**

覆盖：
- 正常添加文档
- 空文档列表不报错
- 缺少主键字段报错
- 缺少向量字段报错
- 批量插入（超过 batch_size 分批）
- metadata 中 JSON 字段序列化

- [ ] **Step 2: 编写 Search 测试**

覆盖：
- 正常搜索返回结果
- 空结果集
- 分数归一化（cosine/l2/ip 三种度量）
- field_mapping 映射回用户字段名
- metadata 中 JSON 字符串反序列化
- with filters

- [ ] **Step 3: 编写 DeleteDocsByIDs 测试**

覆盖：
- 正常删除
- 空 ID 列表不报错

- [ ] **Step 4: 编写 DeleteDocsByFilters 测试**

覆盖：
- 正常按过滤条件删除
- 空 filters 不报错

- [ ] **Step 5: 运行测试验证通过**

Run:
```bash
cd /home/opensource/uap-claw-go && go test -run "TestChroma" ./internal/agentcore/store/vector/... -v
```

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/store/vector/chroma_test.go
git commit -m "test(vector): 添加 ChromaVectorStore AddDocs/Search/Delete 测试"
```

---

### Task 13: 补充 chroma_test.go — GetSchema + Metadata + GetAllDocuments 测试

**Files:**
- Modify: `internal/agentcore/store/vector/chroma_test.go`

- [ ] **Step 1: 编写 GetSchema 测试**

覆盖：
- 从 metadata 读取 schema
- schema 字段不存在时 fallback 到 fields
- field_mapping 推断默认 schema

- [ ] **Step 2: 编写 GetCollectionMetadata 测试**

覆盖：
- 正常获取 metadata
- 默认 distance_metric 和 schema_version
- 集合不存在报错

- [ ] **Step 3: 编写 UpdateCollectionMetadata 测试**

覆盖：
- 正常更新
- schema_version 校验（负数报错）
- 更新 distance_metric 同步缓存

- [ ] **Step 4: 编写 GetAllDocuments 测试**

覆盖：
- 正常获取所有文档
- 空集合返回空列表
- field_mapping 映射正确

- [ ] **Step 5: 编写 UpdateSchema 测试**

覆盖：
- 返回 not implemented 错误

- [ ] **Step 6: 运行全部单元测试**

Run:
```bash
cd /home/opensource/uap-claw-go && go test ./internal/agentcore/store/vector/... -v -cover
```

- [ ] **Step 7: 提交**

```bash
git add internal/agentcore/store/vector/chroma_test.go
git commit -m "test(vector): 添加 ChromaVectorStore GetSchema/Metadata/GetAllDocuments 测试"
```

---

### Task 14: 创建 chroma_integration_test.go

**Files:**
- Create: `internal/agentcore/store/vector/chroma_integration_test.go`

- [ ] **Step 1: 编写集成测试**

```go
//go:build integration

package vector

import (
	"context"
	"os"
	"testing"
)

// TestChromaVectorStore_真实调用 测试 ChromaVectorStore 真实调用
// 运行方式: go test -tags=integration ./internal/agentcore/store/vector/...
func TestChromaVectorStore_真实调用(t *testing.T) {
	persistDir := os.Getenv("CHROMA_PERSIST_DIR")
	if persistDir == "" {
		persistDir = t.TempDir()
	}

	store := NewChromaVectorStore(persistDir)
	defer store.Close()

	ctx := context.Background()

	// 构建测试 schema
	schema, _ := NewCollectionSchema(
		WithCollectionDescription("test collection"),
	)
	pkField, _ := NewFieldSchema("id", VectorDataTypeVarchar, WithMaxLength(256), WithPrimary())
	vecField, _ := NewFieldSchema("embedding", VectorDataTypeFloatVector, WithDim(4))
	textField, _ := NewFieldSchema("text", VectorDataTypeVarchar, WithMaxLength(65535))
	schema.AddField(pkField)
	schema.AddField(vecField)
	schema.AddField(textField)

	// CreateCollection
	if err := store.CreateCollection(ctx, "test_coll", schema); err != nil {
		t.Fatalf("CreateCollection 失败: %v", err)
	}

	// CollectionExists
	exists, err := store.CollectionExists(ctx, "test_coll")
	if err != nil {
		t.Fatalf("CollectionExists 失败: %v", err)
	}
	if !exists {
		t.Fatal("集合应该存在")
	}

	// AddDocs
	docs := []map[string]any{
		{"id": "1", "embedding": []float64{0.1, 0.2, 0.3, 0.4}, "text": "hello"},
		{"id": "2", "embedding": []float64{0.5, 0.6, 0.7, 0.8}, "text": "world"},
	}
	if err := store.AddDocs(ctx, "test_coll", docs); err != nil {
		t.Fatalf("AddDocs 失败: %v", err)
	}

	// Search
	results, err := store.Search(ctx, "test_coll", []float64{0.1, 0.2, 0.3, 0.4}, "embedding", 2, nil)
	if err != nil {
		t.Fatalf("Search 失败: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("搜索结果不应为空")
	}

	// DeleteCollection
	if err := store.DeleteCollection(ctx, "test_coll"); err != nil {
		t.Fatalf("DeleteCollection 失败: %v", err)
	}
}
```

- [ ] **Step 2: 验证编译通过**

Run:
```bash
cd /home/opensource/uap-claw-go && go build -tags=integration ./internal/agentcore/store/vector/...
```

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/store/vector/chroma_integration_test.go
git commit -m "test(vector): 添加 ChromaVectorStore 集成测试"
```

---

### Task 15: 最终验证 + 覆盖率检查

**Files:**
- May modify: any files that need fixes

- [ ] **Step 1: 运行全部单元测试**

Run:
```bash
cd /home/opensource/uap-claw-go && go test ./internal/agentcore/store/vector/... -v -cover
```

- [ ] **Step 2: 检查覆盖率是否达标（≥ 85%）**

Run:
```bash
cd /home/opensource/uap-claw-go && go test -coverprofile=coverage.out ./internal/agentcore/store/vector/... && go tool cover -func=coverage.out | grep chroma
```

如果覆盖率不足，补充测试用例。

- [ ] **Step 3: 验证整个项目编译通过**

Run:
```bash
cd /home/opensource/uap-claw-go && go build ./...
```

- [ ] **Step 4: 更新 IMPLEMENTATION_PLAN.md**

将步骤 4.9 的 `☐` 改为 `✅`。

- [ ] **Step 5: 最终提交**

```bash
git add -A
git commit -m "feat(vector): 完成 ChromaVectorStore 实现（4.9 ✅）"
```

---

## Self-Review Checklist

**1. Spec coverage:**
- ✅ chromaClient/chromaCollection 接口 — Task 2
- ✅ ChromaVectorStore 结构体 — Task 2
- ✅ fieldMapping + DistanceMetric — Task 2, 3
- ✅ CreateCollection — Task 4
- ✅ DeleteCollection — Task 4
- ✅ CollectionExists — Task 4
- ✅ GetSchema — Task 5
- ✅ GetCollectionMetadata — Task 5
- ✅ UpdateCollectionMetadata — Task 5
- ✅ ListCollectionNames — Task 5
- ✅ AddDocs — Task 6
- ✅ Search — Task 6
- ✅ DeleteDocsByIDs — Task 6
- ✅ DeleteDocsByFilters — Task 6
- ✅ UpdateSchema (placeholder) — Task 7
- ✅ GetAllDocuments — Task 7
- ✅ 日志对齐 — 每个方法内含
- ✅ 错误码 — 每个方法内含
- ✅ 测试 ≥ 85% — Task 15
- ✅ doc.go 更新 — Task 9
- ✅ 集成测试 — Task 14

**2. Placeholder scan:** No TBD/TODO except the intentional `// TODO: ⤵️` in UpdateSchema placeholder.

**3. Type consistency:** All types and method names are consistent across tasks. `fieldMapping` includes `DistanceMetric` field. `chromaClient`/`chromaCollection` interfaces use SDK V2 types.
