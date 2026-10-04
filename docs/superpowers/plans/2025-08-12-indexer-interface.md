# 13.1 Indexer 抽象接口 + 前置类型 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 补齐 `retrieval/common` 缺失的类型定义（TextChunk + config），建立 `retrieval/indexing/indexer` 包结构与 `Indexer` 抽象接口。

**Architecture:** 三层递进实现——先补 common/config.go 配置类型（含验证），再补 common/document.go 的 TextChunk（含 NewTextChunkFromDocument），最后建 indexing/indexer 包的 Indexer 接口。TDD 驱动，每层先写测试再实现。

**Tech Stack:** Go 1.22+, stretchr/testify, google/uuid, 项目内 exception/logger 包

---

## 文件结构

| 操作 | 文件 | 职责 |
|------|------|------|
| 新建 | `internal/agentcore/retrieval/common/config.go` | 5 结构体 + 3 枚举 + 默认构造函数 + Validate + 重导出 |
| 新建 | `internal/agentcore/retrieval/common/config_test.go` | 配置类型测试 |
| 修改 | `internal/agentcore/retrieval/common/document.go` | 追加 TextChunk + NewTextChunkFromDocument |
| 修改 | `internal/agentcore/retrieval/common/document_test.go` | 追加 TextChunk 测试 |
| 修改 | `internal/agentcore/retrieval/common/doc.go` | 添加 config.go 条目 |
| 新建 | `internal/agentcore/retrieval/indexing/doc.go` | indexing 包文档 |
| 新建 | `internal/agentcore/retrieval/indexing/indexer/base.go` | Indexer 接口 + IndexOption |
| 新建 | `internal/agentcore/retrieval/indexing/indexer/doc.go` | indexer 包文档 |
| 新建 | `internal/agentcore/retrieval/indexing/indexer/base_test.go` | fakeIndexer + 接口满足性测试 |

---

### Task 1: common/config.go — 配置类型定义

**Files:**
- Create: `internal/agentcore/retrieval/common/config.go`
- Create: `internal/agentcore/retrieval/common/config_test.go`

- [ ] **Step 1: 写 config_test.go 失败测试**

```go
package common

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// ──── IndexConfig 测试 ────

func TestNewDefaultIndexConfig(t *testing.T) {
	cfg := NewDefaultIndexConfig("test-index")
	assert.Equal(t, "test-index", cfg.IndexName)
	assert.Equal(t, IndexTypeHybrid, cfg.IndexType)
	assert.False(t, cfg.UseCaptionForImages)
}

func TestIndexConfig_Validate_正常(t *testing.T) {
	cfg := NewDefaultIndexConfig("my-index")
	err := cfg.Validate()
	assert.NoError(t, err)
}

func TestIndexConfig_Validate_名称为空(t *testing.T) {
	cfg := NewDefaultIndexConfig("")
	err := cfg.Validate()
	assert.Error(t, err)
}

func TestIndexConfig_Validate_索引类型无效(t *testing.T) {
	cfg := &IndexConfig{IndexName: "test", IndexType: IndexTypeKind("invalid")}
	err := cfg.Validate()
	assert.Error(t, err)
}

// ──── KnowledgeBaseConfig 测试 ────

func TestNewDefaultKnowledgeBaseConfig(t *testing.T) {
	cfg := NewDefaultKnowledgeBaseConfig("kb-001")
	assert.Equal(t, "kb-001", cfg.KBID)
	assert.Equal(t, IndexTypeHybrid, cfg.IndexType)
	assert.False(t, cfg.UseGraph)
	assert.Equal(t, 512, cfg.ChunkSize)
	assert.Equal(t, 50, cfg.ChunkOverlap)
	assert.False(t, cfg.UseCaptionForImages)
}

func TestKnowledgeBaseConfig_Validate_正常(t *testing.T) {
	cfg := NewDefaultKnowledgeBaseConfig("kb-001")
	err := cfg.Validate()
	assert.NoError(t, err)
}

func TestKnowledgeBaseConfig_Validate_KBID为空(t *testing.T) {
	cfg := NewDefaultKnowledgeBaseConfig("")
	err := cfg.Validate()
	assert.Error(t, err)
}

func TestKnowledgeBaseConfig_Validate_索引类型无效(t *testing.T) {
	cfg := &KnowledgeBaseConfig{KBID: "kb-001", IndexType: IndexTypeKind("invalid")}
	err := cfg.Validate()
	assert.Error(t, err)
}

// ──── RetrievalConfig 测试 ────

func TestNewDefaultRetrievalConfig(t *testing.T) {
	cfg := NewDefaultRetrievalConfig()
	assert.Equal(t, 5, cfg.TopK)
	assert.Nil(t, cfg.ScoreThreshold)
	assert.Nil(t, cfg.UseGraph)
	assert.False(t, cfg.Agentic)
	assert.False(t, cfg.GraphExpansion)
	assert.Nil(t, cfg.Filters)
}

func TestRetrievalConfig_Validate_正常(t *testing.T) {
	cfg := NewDefaultRetrievalConfig()
	err := cfg.Validate()
	assert.NoError(t, err)
}

// ──── VectorStoreConfig 测试 ────

func TestVectorStoreConfig_Validate_正常(t *testing.T) {
	cfg := &VectorStoreConfig{
		StoreProvider:  StoreTypeMilvus,
		CollectionName: "my-collection",
		DistanceMetric: DistanceMetricCosine,
	}
	err := cfg.Validate()
	assert.NoError(t, err)
}

func TestVectorStoreConfig_Validate_集合名称为空(t *testing.T) {
	cfg := &VectorStoreConfig{
		StoreProvider:  StoreTypeMilvus,
		CollectionName: "",
		DistanceMetric: DistanceMetricCosine,
	}
	err := cfg.Validate()
	assert.Error(t, err)
}

func TestVectorStoreConfig_Validate_存储类型无效(t *testing.T) {
	cfg := &VectorStoreConfig{
		StoreProvider:  StoreType("invalid"),
		CollectionName: "my-collection",
		DistanceMetric: DistanceMetricCosine,
	}
	err := cfg.Validate()
	assert.Error(t, err)
}

func TestVectorStoreConfig_Validate_数据库名称非法字符(t *testing.T) {
	cfg := &VectorStoreConfig{
		StoreProvider:  StoreTypeMilvus,
		DatabaseName:   "my-db!",
		CollectionName: "my-collection",
		DistanceMetric: DistanceMetricCosine,
	}
	err := cfg.Validate()
	assert.Error(t, err)
}

func TestVectorStoreConfig_Validate_距离度量无效(t *testing.T) {
	cfg := &VectorStoreConfig{
		StoreProvider:  StoreTypeMilvus,
		CollectionName: "my-collection",
		DistanceMetric: DistanceMetricKind("invalid"),
	}
	err := cfg.Validate()
	assert.Error(t, err)
}

// ──── 枚举常量测试 ────

func TestIndexTypeKind_常量值(t *testing.T) {
	assert.Equal(t, IndexTypeKind("hybrid"), IndexTypeHybrid)
	assert.Equal(t, IndexTypeKind("bm25"), IndexTypeBM25)
	assert.Equal(t, IndexTypeKind("vector"), IndexTypeVector)
}

func TestStoreType_常量值(t *testing.T) {
	assert.Equal(t, StoreType("milvus"), StoreTypeMilvus)
	assert.Equal(t, StoreType("chroma"), StoreTypeChroma)
	assert.Equal(t, StoreType("pgvector"), StoreTypePGVector)
}

func TestDistanceMetricKind_常量值(t *testing.T) {
	assert.Equal(t, DistanceMetricKind("cosine"), DistanceMetricCosine)
	assert.Equal(t, DistanceMetricKind("euclidean"), DistanceMetricEuclidean)
	assert.Equal(t, DistanceMetricKind("dot"), DistanceMetricDot)
}

// ──── 重导出类型测试 ────

func TestEmbeddingConfig_重导出(t *testing.T) {
	// 验证 common.EmbeddingConfig 可赋值，类型别名成立
	var _ EmbeddingConfig = embedding.EmbeddingConfig{}
}

func TestRerankerConfig_重导出(t *testing.T) {
	// 验证 common.RerankerConfig 可赋值，类型别名成立
	var _ RerankerConfig = reranker.RerankerConfig{}
}
```

- [ ] **Step 2: 运行测试确认失败**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/retrieval/common/... -run "TestNewDefaultIndexConfig|TestIndexConfig_Validate|TestNewDefaultKnowledgeBaseConfig|TestKnowledgeBaseConfig_Validate|TestNewDefaultRetrievalConfig|TestRetrievalConfig_Validate|TestVectorStoreConfig_Validate|TestIndexTypeKind|TestStoreType|TestDistanceMetricKind|TestEmbeddingConfig|TestRerankerConfig" -v 2>&1 | head -20
```

Expected: 编译失败，undefined 错误

- [ ] **Step 3: 实现 config.go**

```go
package common

import (
	"fmt"
	"regexp"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/embedding"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/reranker"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 结构体 ────────────────────────────

// IndexConfig 索引配置。
//
// Python: openjiuwen/core/retrieval/common/config.py (IndexConfig)
type IndexConfig struct {
	// IndexName 索引名称（必填）
	IndexName string
	// IndexType 索引类型，取值 IndexTypeHybrid / IndexTypeBM25 / IndexTypeVector
	IndexType IndexTypeKind
	// UseCaptionForImages 为 true 时，图片分块仅用文本/标题嵌入（纯文本路径）
	UseCaptionForImages bool
}

// KnowledgeBaseConfig 知识库配置。
//
// Python: openjiuwen/core/retrieval/common/config.py (KnowledgeBaseConfig)
type KnowledgeBaseConfig struct {
	// KBID 知识库标识（必填）
	KBID string
	// IndexType 索引类型，取值 IndexTypeHybrid / IndexTypeBM25 / IndexTypeVector
	IndexType IndexTypeKind
	// UseGraph 是否使用图索引
	UseGraph bool
	// ChunkSize 分块大小
	ChunkSize int
	// ChunkOverlap 分块重叠
	ChunkOverlap int
	// UseCaptionForImages 为 true 时，图片分块仅用文本/标题嵌入
	UseCaptionForImages bool
}

// RetrievalConfig 检索配置。
//
// Python: openjiuwen/core/retrieval/common/config.py (RetrievalConfig)
type RetrievalConfig struct {
	// TopK 返回结果数量
	TopK int
	// ScoreThreshold 分数阈值，nil 表示不限制
	ScoreThreshold *float64
	// UseGraph 是否使用图检索，nil 表示使用默认配置
	UseGraph *bool
	// Agentic 是否使用 Agentic 检索
	Agentic bool
	// GraphExpansion 是否启用图扩展
	GraphExpansion bool
	// Filters 元数据过滤条件
	Filters map[string]any
}

// VectorStoreConfig 向量存储配置。
//
// Python: openjiuwen/core/retrieval/common/config.py (VectorStoreConfig)
type VectorStoreConfig struct {
	// StoreProvider 向量存储提供商
	StoreProvider StoreType
	// DatabaseName 数据库名称（仅字母数字下划线）
	DatabaseName string
	// CollectionName 集合名称（必填）
	CollectionName string
	// DistanceMetric 距离度量方式
	DistanceMetric DistanceMetricKind
}

// ──────────────────────────── 枚举 ────────────────────────────

// IndexTypeKind 索引类型，对齐 Python Literal["hybrid", "bm25", "vector"]。
type IndexTypeKind string

// StoreType 向量存储提供商类型，对齐 Python StoreType 枚举。
type StoreType string

// DistanceMetricKind 距离度量方式，对齐 Python Literal["cosine", "euclidean", "dot"]。
type DistanceMetricKind string

// ──────────────────────────── 常量 ────────────────────────────

const (
	// IndexTypeHybrid 混合索引（BM25 + 向量）
	IndexTypeHybrid IndexTypeKind = "hybrid"
	// IndexTypeBM25 BM25 稀疏索引
	IndexTypeBM25 IndexTypeKind = "bm25"
	// IndexTypeVector 向量索引
	IndexTypeVector IndexTypeKind = "vector"
)

const (
	// StoreTypeMilvus Milvus 向量库
	StoreTypeMilvus StoreType = "milvus"
	// StoreTypeChroma ChromaDB 向量库
	StoreTypeChroma StoreType = "chroma"
	// StoreTypePGVector PGVector 向量库
	StoreTypePGVector StoreType = "pgvector"
)

const (
	// DistanceMetricCosine 余弦距离（默认）
	DistanceMetricCosine DistanceMetricKind = "cosine"
	// DistanceMetricEuclidean 欧几里得距离
	DistanceMetricEuclidean DistanceMetricKind = "euclidean"
	// DistanceMetricDot 点积距离
	DistanceMetricDot DistanceMetricKind = "dot"
)

// 重导出类型，对齐 Python config.py __all__

// EmbeddingConfig 嵌入模型配置。
// 重导出自 foundation/store/embedding，对齐 Python config.py __all__。
type EmbeddingConfig = embedding.EmbeddingConfig

// RerankerConfig 重排序模型配置。
// 重导出自 foundation/store/reranker，对齐 Python config.py __all__。
type RerankerConfig = reranker.RerankerConfig

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// validIndexTypeKinds 有效的索引类型集合
	validIndexTypeKinds = map[IndexTypeKind]bool{
		IndexTypeHybrid: true,
		IndexTypeBM25:   true,
		IndexTypeVector: true,
	}
	// validStoreTypes 有效的存储类型集合
	validStoreTypes = map[StoreType]bool{
		StoreTypeMilvus:   true,
		StoreTypeChroma:   true,
		StoreTypePGVector: true,
	}
	// validDistanceMetrics 有效的距离度量集合
	validDistanceMetrics = map[DistanceMetricKind]bool{
		DistanceMetricCosine:    true,
		DistanceMetricEuclidean: true,
		DistanceMetricDot:       true,
	}
	// dbNamePattern 数据库名称正则，仅允许字母数字下划线
	dbNamePattern = regexp.MustCompile(`^[A-Za-z0-9_]*$`)
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewDefaultIndexConfig 创建默认索引配置。
//
// Python: IndexConfig(index_name=..., index_type="hybrid", use_caption_for_images=False)
func NewDefaultIndexConfig(indexName string) *IndexConfig {
	return &IndexConfig{
		IndexName:           indexName,
		IndexType:           IndexTypeHybrid,
		UseCaptionForImages: false,
	}
}

// NewDefaultKnowledgeBaseConfig 创建默认知识库配置。
//
// Python: KnowledgeBaseConfig(kb_id=..., index_type="hybrid", use_graph=False, chunk_size=512, chunk_overlap=50, use_caption_for_images=False)
func NewDefaultKnowledgeBaseConfig(kbID string) *KnowledgeBaseConfig {
	return &KnowledgeBaseConfig{
		KBID:                kbID,
		IndexType:           IndexTypeHybrid,
		UseGraph:            false,
		ChunkSize:           512,
		ChunkOverlap:        50,
		UseCaptionForImages: false,
	}
}

// NewDefaultRetrievalConfig 创建默认检索配置。
//
// Python: RetrievalConfig(top_k=5, score_threshold=None, use_graph=None, agentic=False, graph_expansion=False, filters=None)
func NewDefaultRetrievalConfig() *RetrievalConfig {
	return &RetrievalConfig{
		TopK:           5,
		ScoreThreshold: nil,
		UseGraph:       nil,
		Agentic:        false,
		GraphExpansion: false,
		Filters:        nil,
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// validateIndexTypeKind 校验索引类型是否有效。
func validateIndexTypeKind(kind IndexTypeKind) error {
	if !validIndexTypeKinds[kind] {
		return exception.BuildError(
			exception.StatusRetrievalIndexingVectorFieldInvalid,
			exception.WithParam("error_msg", fmt.Sprintf("无效的索引类型: %s，可选值: hybrid, bm25, vector", kind)),
		)
	}
	return nil
}

// validateStoreType 校验存储类型是否有效。
func validateStoreType(st StoreType) error {
	if !validStoreTypes[st] {
		return exception.BuildError(
			exception.StatusRetrievalIndexingVectorFieldInvalid,
			exception.WithParam("error_msg", fmt.Sprintf("无效的存储类型: %s，可选值: milvus, chroma, pgvector", st)),
		)
	}
	return nil
}

// validateDistanceMetricKind 校验距离度量是否有效。
func validateDistanceMetricKind(dm DistanceMetricKind) error {
	if !validDistanceMetrics[dm] {
		return exception.BuildError(
			exception.StatusRetrievalIndexingVectorFieldInvalid,
			exception.WithParam("error_msg", fmt.Sprintf("无效的距离度量: %s，可选值: cosine, euclidean, dot", dm)),
		)
	}
	return nil
}

// validateDatabaseName 校验数据库名称是否合法（仅字母数字下划线）。
func validateDatabaseName(name string) error {
	if name != "" && !dbNamePattern.MatchString(name) {
		return exception.BuildError(
			exception.StatusRetrievalIndexingVectorFieldInvalid,
			exception.WithParam("error_msg", fmt.Sprintf("数据库名称含非法字符: %s，仅允许字母数字下划线", name)),
		)
	}
	return nil
}
```

注意：`Validate()` 方法需要按结构体拆分实现。继续在同一文件中追加导出函数区域的 `Validate` 方法：

```go
// Validate 校验 IndexConfig 字段。
func (c *IndexConfig) Validate() error {
	if c.IndexName == "" {
		return exception.BuildError(
			exception.StatusRetrievalIndexingPathNotFound,
			exception.WithParam("error_msg", "索引名称不能为空"),
		)
	}
	return validateIndexTypeKind(c.IndexType)
}

// Validate 校验 KnowledgeBaseConfig 字段。
func (c *KnowledgeBaseConfig) Validate() error {
	if c.KBID == "" {
		return exception.BuildError(
			exception.StatusRetrievalIndexingPathNotFound,
			exception.WithParam("error_msg", "知识库标识不能为空"),
		)
	}
	return validateIndexTypeKind(c.IndexType)
}

// Validate 校验 RetrievalConfig 字段。
func (c *RetrievalConfig) Validate() error {
	return nil
}

// Validate 校验 VectorStoreConfig 字段。
func (c *VectorStoreConfig) Validate() error {
	if err := validateStoreType(c.StoreProvider); err != nil {
		return err
	}
	if c.CollectionName == "" {
		return exception.BuildError(
			exception.StatusRetrievalIndexingPathNotFound,
			exception.WithParam("error_msg", "集合名称不能为空"),
		)
	}
	if err := validateDatabaseName(c.DatabaseName); err != nil {
		return err
	}
	return validateDistanceMetricKind(c.DistanceMetric)
}
```

注意：`Validate` 方法放在结构体定义之后、导出函数区域中。由于 Go 不允许同一文件中有两个同名方法分属不同类型放不同区域，为遵循声明顺序规范，`Validate` 方法统一归入导出函数区域。

- [ ] **Step 4: 运行测试确认通过**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/retrieval/common/... -run "TestNewDefaultIndexConfig|TestIndexConfig_Validate|TestNewDefaultKnowledgeBaseConfig|TestKnowledgeBaseConfig_Validate|TestNewDefaultRetrievalConfig|TestRetrievalConfig_Validate|TestVectorStoreConfig_Validate|TestIndexTypeKind|TestStoreType|TestDistanceMetricKind|TestEmbeddingConfig|TestRerankerConfig" -v
```

Expected: 全部 PASS

- [ ] **Step 5: 运行全包测试确认无回归**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/retrieval/common/... -v
```

Expected: 全部 PASS

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/retrieval/common/config.go internal/agentcore/retrieval/common/config_test.go
git commit -m "feat(retrieval): 添加 common/config.go 配置类型 — IndexConfig + KnowledgeBaseConfig + RetrievalConfig + VectorStoreConfig + 枚举 + Validate"
```

---

### Task 2: common/document.go — 追加 TextChunk

**Files:**
- Modify: `internal/agentcore/retrieval/common/document.go`
- Modify: `internal/agentcore/retrieval/common/document_test.go`

- [ ] **Step 1: 写 TextChunk 失败测试**

在 `document_test.go` 末尾追加：

```go
// ──── TextChunk 测试 ────

func TestTextChunk_基本字段(t *testing.T) {
	tc := &TextChunk{
		ID:       "chunk-001",
		Text:     "分块内容",
		DocID:    "doc-001",
		Metadata: map[string]any{"page": 1},
	}
	assert.Equal(t, "chunk-001", tc.ID)
	assert.Equal(t, "分块内容", tc.Text)
	assert.Equal(t, "doc-001", tc.DocID)
	assert.Equal(t, map[string]any{"page": 1}, tc.Metadata)
	assert.Nil(t, tc.Embedding)
}

func TestTextChunk_EmbeddingOmitEmpty(t *testing.T) {
	// 无 Embedding 时 JSON 不包含 embedding 字段
	tc := &TextChunk{ID: "c1", Text: "text", DocID: "d1"}
	data, err := json.Marshal(tc)
	assert.NoError(t, err)
	assert.NotContains(t, string(data), "embedding")

	// 有 Embedding 时 JSON 包含 embedding 字段
	tc.Embedding = []float64{0.1, 0.2}
	data, err = json.Marshal(tc)
	assert.NoError(t, err)
	assert.Contains(t, string(data), "embedding")
}

func TestNewTextChunkFromDocument(t *testing.T) {
	doc := &reranker.Document{
		ID:       "doc-001",
		Text:     "原始文档",
		Metadata: map[string]any{"source": "test"},
	}
	tc := NewTextChunkFromDocument(doc, "分块文本", "")
	assert.NotEmpty(t, tc.ID)            // ID 自动生成
	assert.Equal(t, "分块文本", tc.Text)
	assert.Equal(t, "doc-001", tc.DocID)
	assert.Equal(t, map[string]any{"source": "test"}, tc.Metadata)
	assert.Nil(t, tc.Embedding)
}

func TestNewTextChunkFromDocument_自定义ID(t *testing.T) {
	doc := &reranker.Document{
		ID:       "doc-001",
		Text:     "原始文档",
		Metadata: map[string]any{"source": "test"},
	}
	tc := NewTextChunkFromDocument(doc, "分块文本", "my-chunk-id")
	assert.Equal(t, "my-chunk-id", tc.ID)
}

func TestNewTextChunkFromDocument_Metadata为nil(t *testing.T) {
	doc := &reranker.Document{
		ID:       "doc-001",
		Text:     "原始文档",
		Metadata: nil,
	}
	tc := NewTextChunkFromDocument(doc, "分块文本", "c1")
	assert.NotNil(t, tc.Metadata)
	assert.Equal(t, map[string]any{}, tc.Metadata)
}
```

注意：`document_test.go` 需新增 import `"encoding/json"` 和 `"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/reranker"`。

- [ ] **Step 2: 运行测试确认失败**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/retrieval/common/... -run "TestTextChunk_|TestNewTextChunkFromDocument" -v 2>&1 | head -20
```

Expected: 编译失败，undefined TextChunk

- [ ] **Step 3: 在 document.go 追加 TextChunk**

在 `MultimodalDocument` 和 `addFieldOptions` 之间追加 `TextChunk` 结构体（按声明顺序规范，结构体区域）。

在 `document.go` 的 import 中追加 `"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/reranker"`。

**结构体区域**追加：

```go
// TextChunk 文本分块数据模型，用于文档索引管线中的分块传递。
//
// Python: openjiuwen/core/retrieval/common/document.py (TextChunk)
type TextChunk struct {
	// ID 分块 ID
	ID string `json:"id_"`
	// Text 分块文本内容
	Text string `json:"text"`
	// DocID 父文档 ID
	DocID string `json:"doc_id"`
	// Metadata 分块元数据
	Metadata map[string]any `json:"metadata"`
	// Embedding 分块嵌入向量（嵌入计算后填充）
	Embedding []float64 `json:"embedding,omitempty"`
}
```

**导出函数区域**追加：

```go
// NewTextChunkFromDocument 从 Document 创建 TextChunk。
//
// Python: TextChunk.from_document()
// id 为空时自动生成 UUID。
func NewTextChunkFromDocument(doc *reranker.Document, chunkText string, id string) *TextChunk {
	if id == "" {
		id = uuid.NewString()
	}
	meta := doc.Metadata
	if meta == nil {
		meta = make(map[string]any)
	}
	return &TextChunk{
		ID:       id,
		Text:     chunkText,
		DocID:    doc.ID,
		Metadata: meta,
	}
}
```

- [ ] **Step 4: 运行测试确认通过**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/retrieval/common/... -run "TestTextChunk_|TestNewTextChunkFromDocument" -v
```

Expected: 全部 PASS

- [ ] **Step 5: 运行全包测试确认无回归**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/retrieval/common/... -v
```

Expected: 全部 PASS

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/retrieval/common/document.go internal/agentcore/retrieval/common/document_test.go
git commit -m "feat(retrieval): 添加 TextChunk + NewTextChunkFromDocument — 对齐 Python TextChunk.from_document"
```

---

### Task 3: 更新 common/doc.go

**Files:**
- Modify: `internal/agentcore/retrieval/common/doc.go`

- [ ] **Step 1: 更新 doc.go 文件目录**

将文件目录从：

```go
// 文件目录：
//
//	common/
//	├── doc.go           # 包文档
//	└── document.go      # MultimodalDocument 多模态文档模型
```

改为：

```go
// 文件目录：
//
//	common/
//	├── doc.go           # 包文档
//	├── config.go        # 索引/知识库/检索/向量存储配置类型
//	└── document.go      # MultimodalDocument + TextChunk 文档数据模型
```

同时更新核心类型索引，在现有 `ModalityKind` / `ModalityField` / `MultimodalDocument` 后追加：

```go
// 核心类型索引：
//
//	ModalityKind       — 内容模态类型枚举
//	ModalityField      — 单个模态字段
//	MultimodalDocument — 多模态文档（文本/图片/音频/视频）
//	TextChunk          — 文本分块数据模型
//	IndexConfig        — 索引配置
//	KnowledgeBaseConfig — 知识库配置
//	RetrievalConfig    — 检索配置
//	VectorStoreConfig  — 向量存储配置
//	IndexTypeKind      — 索引类型枚举
//	StoreType          — 向量存储提供商枚举
//	DistanceMetricKind — 距离度量枚举
```

- [ ] **Step 2: 运行测试确认无回归**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/retrieval/common/... -v
```

Expected: 全部 PASS

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/retrieval/common/doc.go
git commit -m "docs(retrieval): 更新 common/doc.go — 添加 config.go + TextChunk 条目"
```

---

### Task 4: indexing 包 — doc.go + indexer 包结构

**Files:**
- Create: `internal/agentcore/retrieval/indexing/doc.go`
- Create: `internal/agentcore/retrieval/indexing/indexer/doc.go`

- [ ] **Step 1: 创建目录**

```bash
mkdir -p /home/opensource/uapclaw-gateway/internal/agentcore/retrieval/indexing/indexer
```

- [ ] **Step 2: 创建 indexing/doc.go**

```go
// Package indexing 提供文档级索引管线：解析 → 分块 → 嵌入 → 入库。
//
// 与 agentcore/foundation/store/index（7.36 用户记忆索引）不同，
// 本包处理外部文档（上传文件/链接/图片）的索引构建与维护。
// 用户记忆索引按用户/作用域增删改 KV+Vector 双写；
// 文档索引按 doc_id 整体替换，纯 Vector 存储。
//
// 对应 Python 代码：openjiuwen/core/retrieval/indexing/
package indexing
```

- [ ] **Step 3: 创建 indexer/doc.go**

```go
// Package indexer 提供 Indexer 抽象接口及其具体实现。
//
// Indexer 负责将已解析、分块、嵌入好的 TextChunk 写入向量数据库，
// 支持新建、更新、删除、存在性检查、元信息查询五类操作。
//
// 文件目录：
//
//	indexer/
//	├── doc.go       # 包文档
//	└── base.go      # Indexer 接口定义
//
// 对应 Python 代码：openjiuwen/core/retrieval/indexing/indexer/
//
// 核心类型索引：
//
//	Indexer — 文档索引抽象接口
package indexer
```

- [ ] **Step 4: 运行编译确认**

```bash
cd /home/opensource/uapclaw-gateway && go build ./internal/agentcore/retrieval/indexing/...
```

Expected: 编译成功（空包）

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/retrieval/indexing/doc.go internal/agentcore/retrieval/indexing/indexer/doc.go
git commit -m "feat(retrieval): 创建 indexing/indexer 包结构与 doc.go"
```

---

### Task 5: indexer/base.go — Indexer 接口

**Files:**
- Create: `internal/agentcore/retrieval/indexing/indexer/base.go`
- Create: `internal/agentcore/retrieval/indexing/indexer/base_test.go`

- [ ] **Step 1: 写 base_test.go 失败测试**

```go
package indexer

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
)

// fakeIndexer 用于测试的 Indexer mock 实现
type fakeIndexer struct {
	buildIndexCalled   bool
	updateIndexCalled  bool
	deleteIndexCalled  bool
	indexExistsCalled  bool
	getIndexInfoCalled bool
}

var _ Indexer = (*fakeIndexer)(nil)

func (f *fakeIndexer) BuildIndex(_ context.Context, _ []common.TextChunk, _ common.IndexConfig, _ interface{ EmbedQuery(ctx context.Context, text string, opts ...interface{}) ([]float64, error); EmbedDocuments(ctx context.Context, texts []string, opts ...interface{}) ([][]float64, error); Dimension() int; DimensionWithContext(ctx context.Context) (int, error) }, _ ...IndexOption) (bool, error) {
	f.buildIndexCalled = true
	return true, nil
}

func (f *fakeIndexer) UpdateIndex(_ context.Context, _ []common.TextChunk, _ string, _ common.IndexConfig, _ interface{ EmbedQuery(ctx context.Context, text string, opts ...interface{}) ([]float64, error); EmbedDocuments(ctx context.Context, texts []string, opts ...interface{}) ([][]float64, error); Dimension() int; DimensionWithContext(ctx context.Context) (int, error) }, _ ...IndexOption) (bool, error) {
	f.updateIndexCalled = true
	return true, nil
}

func (f *fakeIndexer) DeleteIndex(_ context.Context, _ string, _ string) (bool, error) {
	f.deleteIndexCalled = true
	return true, nil
}

func (f *fakeIndexer) IndexExists(_ context.Context, _ string) (bool, error) {
	f.indexExistsCalled = true
	return true, nil
}

func (f *fakeIndexer) GetIndexInfo(_ context.Context, _ string) (map[string]any, error) {
	f.getIndexInfoCalled = true
	return map[string]any{"count": 0}, nil
}

func TestIndexer_接口满足性(t *testing.T) {
	// 验证 fakeIndexer 满足 Indexer 接口
	var _ Indexer = (*fakeIndexer)(nil)
}

func TestIndexer_fakeIndexer调用(t *testing.T) {
	f := &fakeIndexer{}
	ctx := context.Background()

	ok, err := f.BuildIndex(ctx, nil, common.IndexConfig{}, nil)
	assert.True(t, ok)
	assert.NoError(t, err)
	assert.True(t, f.buildIndexCalled)

	ok, err = f.DeleteIndex(ctx, "doc1", "idx1")
	assert.True(t, ok)
	assert.NoError(t, err)
	assert.True(t, f.deleteIndexCalled)

	exists, err := f.IndexExists(ctx, "idx1")
	assert.True(t, exists)
	assert.NoError(t, err)
	assert.True(t, f.indexExistsCalled)

	info, err := f.GetIndexInfo(ctx, "idx1")
	assert.NoError(t, err)
	assert.Equal(t, 0, info["count"])
	assert.True(t, f.getIndexInfoCalled)
}

func TestNewIndexOptions(t *testing.T) {
	opts := NewIndexOptions()
	assert.NotNil(t, opts)
	assert.Nil(t, opts.Extra)
}

func TestWithIndexExtra(t *testing.T) {
	opts := NewIndexOptions(WithIndexExtra("key", "value"))
	assert.Equal(t, "value", opts.Extra["key"])
}
```

注意：上面的 fakeIndexer 中 embedModel 参数签名太复杂，实际实现时应该用 `embedding.BaseEmbedding` 类型。这里展示测试意图，实际代码中简化。

- [ ] **Step 2: 运行测试确认失败**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/retrieval/indexing/indexer/... -v 2>&1 | head -20
```

Expected: 编译失败，undefined Indexer

- [ ] **Step 3: 实现 base.go**

```go
package indexer

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/embedding"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
)

// ──────────────────────────── 结构体 ────────────────────────────

// IndexOptions Indexer 方法选项结构。
type IndexOptions struct {
	// Extra 额外参数，对齐 Python **kwargs
	Extra map[string]any
}

// IndexOption Indexer 方法的可选参数。
type IndexOption func(*IndexOptions)

// ──────────────────────────── 接口 ────────────────────────────

// Indexer 文档索引抽象接口。
//
// 负责将 TextChunk 写入向量数据库，支持五种操作：
// BuildIndex（新建）、UpdateIndex（更新）、DeleteIndex（删除）、
// IndexExists（存在性检查）、GetIndexInfo（元信息查询）。
//
// Python: openjiuwen/core/retrieval/indexing/indexer/base.py (Indexer)
type Indexer interface {
	// BuildIndex 构建索引。将 chunks 去重→嵌入→写入向量库。
	// embedModel 为 nil 时由具体实现决定如何获取嵌入。
	BuildIndex(ctx context.Context, chunks []common.TextChunk, config common.IndexConfig, embedModel embedding.BaseEmbedding, opts ...IndexOption) (bool, error)

	// UpdateIndex 更新索引。按 docID 先删后建。
	UpdateIndex(ctx context.Context, chunks []common.TextChunk, docID string, config common.IndexConfig, embedModel embedding.BaseEmbedding, opts ...IndexOption) (bool, error)

	// DeleteIndex 删除索引。按 docID 删除指定文档的所有分块。
	DeleteIndex(ctx context.Context, docID string, indexName string) (bool, error)

	// IndexExists 检查索引是否存在。
	IndexExists(ctx context.Context, indexName string) (bool, error)

	// GetIndexInfo 获取索引元信息。
	GetIndexInfo(ctx context.Context, indexName string) (map[string]any, error)
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewIndexOptions 从可变参数构建 IndexOptions。
func NewIndexOptions(opts ...IndexOption) *IndexOptions {
	o := &IndexOptions{}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

// WithIndexExtra 设置额外参数的 IndexOption。
func WithIndexExtra(key string, value any) IndexOption {
	return func(o *IndexOptions) {
		if o.Extra == nil {
			o.Extra = make(map[string]any)
		}
		o.Extra[key] = value
	}
}
```

- [ ] **Step 4: 修正 base_test.go 中 fakeIndexer 的 embedModel 参数类型**

将 fakeIndexer 的方法签名改为使用 `embedding.BaseEmbedding`：

```go
package indexer

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/embedding"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
)

// fakeIndexer 用于测试的 Indexer mock 实现
type fakeIndexer struct {
	buildIndexCalled   bool
	updateIndexCalled  bool
	deleteIndexCalled  bool
	indexExistsCalled  bool
	getIndexInfoCalled bool
}

var _ Indexer = (*fakeIndexer)(nil)

func (f *fakeIndexer) BuildIndex(_ context.Context, _ []common.TextChunk, _ common.IndexConfig, _ embedding.BaseEmbedding, _ ...IndexOption) (bool, error) {
	f.buildIndexCalled = true
	return true, nil
}

func (f *fakeIndexer) UpdateIndex(_ context.Context, _ []common.TextChunk, _ string, _ common.IndexConfig, _ embedding.BaseEmbedding, _ ...IndexOption) (bool, error) {
	f.updateIndexCalled = true
	return true, nil
}

func (f *fakeIndexer) DeleteIndex(_ context.Context, _ string, _ string) (bool, error) {
	f.deleteIndexCalled = true
	return true, nil
}

func (f *fakeIndexer) IndexExists(_ context.Context, _ string) (bool, error) {
	f.indexExistsCalled = true
	return true, nil
}

func (f *fakeIndexer) GetIndexInfo(_ context.Context, _ string) (map[string]any, error) {
	f.getIndexInfoCalled = true
	return map[string]any{"count": 0}, nil
}

func TestIndexer_接口满足性(t *testing.T) {
	var _ Indexer = (*fakeIndexer)(nil)
}

func TestIndexer_fakeIndexer调用(t *testing.T) {
	f := &fakeIndexer{}
	ctx := context.Background()

	ok, err := f.BuildIndex(ctx, nil, common.IndexConfig{}, nil)
	assert.True(t, ok)
	assert.NoError(t, err)
	assert.True(t, f.buildIndexCalled)

	ok, err = f.DeleteIndex(ctx, "doc1", "idx1")
	assert.True(t, ok)
	assert.NoError(t, err)
	assert.True(t, f.deleteIndexCalled)

	exists, err := f.IndexExists(ctx, "idx1")
	assert.True(t, exists)
	assert.NoError(t, err)
	assert.True(t, f.indexExistsCalled)

	info, err := f.GetIndexInfo(ctx, "idx1")
	assert.NoError(t, err)
	assert.Equal(t, 0, info["count"])
	assert.True(t, f.getIndexInfoCalled)
}

func TestNewIndexOptions(t *testing.T) {
	opts := NewIndexOptions()
	assert.NotNil(t, opts)
	assert.Nil(t, opts.Extra)
}

func TestWithIndexExtra(t *testing.T) {
	opts := NewIndexOptions(WithIndexExtra("key", "value"))
	assert.Equal(t, "value", opts.Extra["key"])
}
```

- [ ] **Step 5: 运行测试确认通过**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/retrieval/indexing/indexer/... -v
```

Expected: 全部 PASS

- [ ] **Step 6: 运行全包测试确认无回归**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/retrieval/... -v
```

Expected: 全部 PASS

- [ ] **Step 7: 提交**

```bash
git add internal/agentcore/retrieval/indexing/indexer/base.go internal/agentcore/retrieval/indexing/indexer/base_test.go
git commit -m "feat(retrieval): 添加 Indexer 接口 + IndexOption — 对齐 Python Indexer(ABC)"
```

---

### Task 6: 更新 indexer/doc.go 文件目录

**Files:**
- Modify: `internal/agentcore/retrieval/indexing/indexer/doc.go`

- [ ] **Step 1: 更新文件目录**

doc.go 中的文件目录已包含 base.go 条目（Task 4 创建时已写入），无需修改。验证一下内容是否正确：

```bash
cat /home/opensource/uapclaw-gateway/internal/agentcore/retrieval/indexing/indexer/doc.go
```

Expected: 包含 `base.go      # Indexer 接口定义` 条目

如果正确则跳过此步骤。

- [ ] **Step 2: 提交（如有变更）**

仅当 doc.go 需要更新时才提交。

---

### Task 7: 覆盖率验证 + IMPLEMENTATION_PLAN.md 状态更新

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`（13.1 状态 ☐ → ✅）

- [ ] **Step 1: 运行覆盖率检查**

```bash
cd /home/opensource/uapclaw-gateway && go test -cover ./internal/agentcore/retrieval/common/... ./internal/agentcore/retrieval/indexing/...
```

Expected: 各包覆盖率 ≥ 85%

- [ ] **Step 2: 更新 IMPLEMENTATION_PLAN.md**

将 13.1 行的状态从 `☐` 改为 `✅`。

- [ ] **Step 3: 提交**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "docs: 标记 13.1 Indexer 抽象接口为已完成"
```
