# 领域 13.1 Indexer 抽象接口 + 前置类型设计

## 概述

实现领域十三（检索文档索引管理）的第一步：补齐 `retrieval/common` 缺失的类型定义，并建立 `retrieval/indexing/indexer` 包结构与 `Indexer` 抽象接口。

### 扩展后的 13.1 范围

原 13.1 仅含 `Indexer` 接口，但 Go 侧前置类型全部缺失，扩展为 3 件交付物：

| 交付物 | 文件 | Python 对齐 |
|--------|------|------------|
| TextChunk + 配置类型 | `retrieval/common/document.go`（追加）+ `retrieval/common/config.go`（新建） | `common/document.py` + `common/config.py` |
| Indexer 接口 + 包结构 | `retrieval/indexing/indexer/base.go` + `doc.go` | `indexing/indexer/base.py` |

---

## 1. `retrieval/common/config.go` — 新建

对齐 Python `openjiuwen/core/retrieval/common/config.py`（82 行），包含 5 结构体 + 3 枚举 + 默认构造函数。

### 1.1 结构体

```go
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
```

### 1.2 枚举（字符串常量）

对齐 Python `Literal[...]` 语义，JSON 序列化零转换。

```go
// IndexTypeKind 索引类型，对齐 Python Literal["hybrid", "bm25", "vector"]。
type IndexTypeKind string

// StoreType 向量存储提供商类型，对齐 Python StoreType 枚举。
type StoreType string

// DistanceMetricKind 距离度量方式，对齐 Python Literal["cosine", "euclidean", "dot"]。
type DistanceMetricKind string

const (
    IndexTypeHybrid IndexTypeKind = "hybrid"
    IndexTypeBM25   IndexTypeKind = "bm25"
    IndexTypeVector IndexTypeKind = "vector"
)

const (
    StoreTypeMilvus   StoreType = "milvus"
    StoreTypeChroma   StoreType = "chroma"
    StoreTypePGVector StoreType = "pgvector"
)

const (
    DistanceMetricCosine    DistanceMetricKind = "cosine"
    DistanceMetricEuclidean DistanceMetricKind = "euclidean"
    DistanceMetricDot       DistanceMetricKind = "dot"
)
```

### 1.3 默认构造函数

```go
// NewDefaultIndexConfig 创建默认索引配置。
func NewDefaultIndexConfig(indexName string) *IndexConfig

// NewDefaultKnowledgeBaseConfig 创建默认知识库配置。
func NewDefaultKnowledgeBaseConfig(kbID string) *KnowledgeBaseConfig

// NewDefaultRetrievalConfig 创建默认检索配置。
func NewDefaultRetrievalConfig() *RetrievalConfig
```

### 1.4 验证方法

各结构体提供 `Validate() error` 方法，使用已有 `exception.StatusRetrievalIndexing*` 错误码：

| 验证场景 | 错误码 |
|----------|--------|
| `IndexConfig.IndexName` 为空 | `StatusRetrievalIndexingPathNotFound` |
| `KnowledgeBaseConfig.KBID` 为空 | `StatusRetrievalIndexingPathNotFound` |
| `IndexTypeKind` 不是 hybrid/bm25/vector 之一 | `StatusRetrievalIndexingVectorFieldInvalid` |
| `VectorStoreConfig.CollectionName` 为空 | `StatusRetrievalIndexingPathNotFound` |
| `VectorStoreConfig.DatabaseName` 含非法字符 | `StatusRetrievalIndexingVectorFieldInvalid` |
| `StoreType` 不是 milvus/chroma/pgvector 之一 | `StatusRetrievalIndexingVectorFieldInvalid` |
| `DistanceMetricKind` 不是 cosine/euclidean/dot 之一 | `StatusRetrievalIndexingVectorFieldInvalid` |

### 1.5 EmbeddingConfig / RerankerConfig 重导出

对齐 Python `config.py` 的 `__all__`，通过 import 重导出：

```go
// EmbeddingConfig 嵌入模型配置。
// 重导出自 foundation/store/embedding，对齐 Python config.py __all__。
type EmbeddingConfig = embedding.EmbeddingConfig

// RerankerConfig 重排序模型配置。
// 重导出自 foundation/store/reranker，对齐 Python config.py __all__。
type RerankerConfig = reranker.RerankerConfig
```

---

## 2. `retrieval/common/document.go` — 追加 TextChunk

在现有 `MultimodalDocument` 下方追加 `TextChunk` 结构体，对齐 Python `TextChunk(BaseModel)`。

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

### 2.1 FromDocument 包级函数

Python `TextChunk.from_document` 是类方法，Go 用包级函数实现（因为 `reranker.Document` 是外部类型，无法附加方法）。

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

### 2.2 循环依赖验证

```
common/document.go → import reranker
reranker/base.go   → import (context, uuid, exception)  ← 不 import common
```

**无循环依赖，安全。**

---

## 3. `retrieval/indexing/indexer/base.go` — 新建

### 3.1 Indexer 接口

纯接口，5 方法，对齐 Python `Indexer(ABC)`。无默认实现。

```go
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
```

### 3.2 IndexOption 函数选项

Python 的 `**kwargs` 用 Go 函数选项模式替代。

```go
// IndexOption Indexer 方法的可选参数。
type IndexOption func(*IndexOptions)

// IndexOptions Indexer 方法选项结构。
type IndexOptions struct {
    // Extra 额外参数，对齐 Python **kwargs
    Extra map[string]any
}
```

### 3.3 依赖方向

```
retrieval/common               ← 不 import 任何 agentcore 子包
    ↑
retrieval/indexing/indexer     ← import common + foundation/store/embedding
```

**无循环依赖风险。**

---

## 4. 包结构与 doc.go 更新

### 4.1 目录结构

```
internal/agentcore/retrieval/
├── common/
│   ├── doc.go              # 更新：添加 config.go 条目
│   ├── document.go         # 更新：追加 TextChunk + NewTextChunkFromDocument
│   ├── document_test.go    # 更新：追加 TextChunk 测试
│   ├── config.go           # 新建：5 结构体 + 3 枚举 + 默认构造函数 + Validate
│   └── config_test.go      # 新建：配置验证测试
│
├── indexing/                # 新建整个子包
│   ├── doc.go              # 包文档
│   └── indexer/
│       ├── doc.go           # 子包文档
│       ├── base.go          # Indexer 接口 + IndexOption
│       └── base_test.go     # fakeIndexer mock + 接口满足性测试
│
├── embedding/              # 不变
├── reranker/               # 不变
└── utils/                  # 不变
```

### 4.2 `common/doc.go` 更新

```go
// 文件目录：
//
//	common/
//	├── doc.go           # 包文档
//	├── config.go        # 索引/知识库/检索/向量存储配置类型
//	└── document.go      # MultimodalDocument + TextChunk 文档数据模型
```

### 4.3 `indexing/doc.go`

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

### 4.4 `indexing/indexer/doc.go`

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

---

## 5. 测试策略

无 build tag 隔离——全部纯内存测试，无外部依赖。

| 文件 | 测试内容 | 覆盖要点 |
|------|----------|----------|
| `config_test.go` | 5 结构体构造 + 默认值 + Validate | IndexName 为空报错、IndexType 校验、DatabaseName 正则、默认值正确、重导出类型可赋值 |
| `document_test.go`（追加） | TextChunk 构造 + NewTextChunkFromDocument + JSON 序列化 | ID 自动生成、Embedding omitempty、Metadata 空 map 处理、FromDocument 字段映射 |
| `indexer/base_test.go` | fakeIndexer mock + 接口满足性 | 实现全部 5 方法，var _ Indexer = (*fakeIndexer)(nil) 编译通过 |

覆盖率目标 ≥ 85%。

---

## 6. 关键设计决策记录

| 决策 | 选择 | 理由 |
|------|------|------|
| TextChunk 放置位置 | 同文件 `document.go` | 和 Python 对齐，TextChunk 与 MultimodalDocument 同属文档数据模型 |
| IndexTypeKind 类型 | 字符串常量 | 最贴近 Python Literal 语义，JSON 序列化零转换 |
| Indexer 接口风格 | 纯接口，无默认实现 | 最直接对齐 Python ABC，ComputeChunkEmbeddings 已独立为 13.4 |
| Python **kwargs | `...IndexOption` 函数选项 | Go 惯用模式，后续 13.2/13.3 可定义具体 Option |
| Python Optional[Embedding] | 直接传 `BaseEmbedding` | Go nil 接口有陷阱，由具体实现决定 embedModel 为 nil 时的行为 |
| TextChunk.from_document | 包级函数 `NewTextChunkFromDocument` | reranker.Document 是外部类型，无法附加方法；包级函数避免循环依赖 |
| config.py 全部补齐 | 一次性搬 IndexConfig + KnowledgeBaseConfig + RetrievalConfig + StoreType + VectorStoreConfig | 82 行不复杂，后续 13.2-13.4 直接依赖 |
| EmbeddingConfig/RerankerConfig | 类型别名重导出 | 对齐 Python __all__，实际定义在 foundation/store |
