# 13.26 KnowledgeBase 抽象 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 复刻 Python `openjiuwen/core/retrieval/knowledge_base.py`——知识库抽象基类（5 抽象 + 4 模板方法 + 配置一致性防线），新建 `knowledgebase` 包 + 回填 4 类型 getter。

**Architecture:** 单包 `internal/agentcore/retrieval/knowledgebase/` 收容 KB 三兄弟（本节 base.go+validate.go，13.27/13.28 留位）；`__setattr__` 魔法复刻为"构造尾部校验 + 双 setter"（方案 B）；7 属性比对走 storageAttrProvider 消费接口 + attrEqual(DeepEqual)；Chroma 拦截走 reflect 类型名前缀（R1）。

**Tech Stack:** Go stdlib（reflect/strings/uuid — google/uuid 已有）、既有 exception/logger/common 包。

**设计文档:** `docs/superpowers/specs/2026-10-29-knowledgebase-13.26-design.md`（偏差 §8 共 9 项，实施时逐项对照）

**编译纪律:** 先 `pgrep -f 'go (build|test)'` 查残留进程，有则 kill；`export GOPROXY=https://goproxy.cn,direct`；单包测试 `go test ./internal/agentcore/retrieval/knowledgebase/ -count=1`；全量回归 `go test -tags "sqlite_fts5 test" ./... -count=1`（CGO_ENABLED=1）。

**Python 源码:** `/home/opensource/agent-core/openjiuwen/core/retrieval/knowledge_base.py`（211 行）
**Python 测试:** `/home/opensource/agent-core/tests/unit_tests/core/retrieval/test_knowledge_base.py`（8 例）+ `test_knowledge_base_validation.py`（15 例）

---

## 背景速览（零上下文工程师必读）

KnowledgeBase 是检索管线**编排层顶点**：下游 Retriever 六兄弟（13.19-13.25）已就绪，本节提供统一门面（AddDocuments/Retrieve/Delete/Update/GetStatistics），13.27 SimpleKB / 13.28 GraphKB 将作为子类嵌入本节基座。核心机制是**配置一致性防线**：读侧 vector_store 与写侧 index_manager 是分开构造的两个组件，连接同一物理集合但各持 7 项 schema 配置（database_name/distance_metric/text_field/vector_field/sparse_vector_field/metadata_field/doc_id_field），两者首次配齐时逐项比对，不一致抛 155508（防"写 db1 读 db2 → 检索静默为空"的漂移故障）。

关键事实（已核实）：
- 错误码常量已定义零消费：`StatusRetrievalKbParserNotFound`(155500)/`StatusRetrievalKbVectorStoreNotFound`(155503)/`StatusRetrievalKbDatabaseConfigInvalid`(155508)，位于 `internal/common/exception/codes_context.go`
- 4 真实类型私有字段名（getter 回填目标）：ChromaVectorStore 与 MilvusVectorStore 均为 `textField/vectorField/sparseVectorField/metadataField/docIDField/databaseName/distanceMetric`；ChromaIndexer 与 MilvusIndexer 同名。**注意**：两个 store 的 `vectorField` 类型不同——`*vector_fields.ChromaVectorField` vs `vector_fields.MilvusVectorFieldConfig`（接口），所以 `VectorField()` getter 返回 `any`
- `MilvusVectorStore` 已有私有 `ensureLoaded(ctx) error`（milvus.go L927）
- parser 既有选项：`parser.WithDocID(string)` / `parser.WithExtra(key, value)`（base.go L107/L117）
- 日志：`logger.Error(logComponent).Str(...).Err(err).Msg(...)` 形态（ComponentAgentCore）
- 错误断言先例：`errors.As(err, &be)` + `be.Code()` + `be.Message()`（hybrid_retriever_test.go L98-101）

---

### Task 1: 回填 vector_store 包 6 getter + EnsureLoaded

**Files:**
- Modify: `internal/agentcore/retrieval/vector_store/chroma.go`（ChromaVectorStore 访问器区）
- Modify: `internal/agentcore/retrieval/vector_store/milvus.go`（MilvusVectorStore 访问器区）
- Test: `internal/agentcore/retrieval/vector_store/graph_backfill_test.go`（既有回填测试文件，追加）

- [ ] **Step 1: 写失败测试（getter 断言）**

在 `graph_backfill_test.go` 追加（该文件是 13.23 回填访问器的测试文件，延续先例；若文件已有构造辅助函数直接复用，否则按下文自建）：

```go
// TestChromaVectorStore_属性访问器回填 13.26 新增 6 getter（validate_index 消费）。
func TestChromaVectorStore_属性访问器回填(t *testing.T) {
	s := &ChromaVectorStore{
		textField:         "content",
		sparseVectorField: "sparse_vector",
		metadataField:     "metadata",
		docIDField:        "document_id",
		databaseName:      "test_db",
	}
	// vectorField nil 时 VectorField() 返回 nil（any 装箱 nil 结构体指针）
	assert.Nil(t, s.VectorField())
	assert.Equal(t, "content", s.TextField())
	assert.Equal(t, "sparse_vector", s.SparseVectorField())
	assert.Equal(t, "metadata", s.MetadataField())
	assert.Equal(t, "document_id", s.DocIDField())
	assert.Equal(t, "test_db", s.DatabaseName())
}

// TestMilvusVectorStore_属性访问器回填 13.26 新增 6 getter + EnsureLoaded 转发。
func TestMilvusVectorStore_属性访问器回填(t *testing.T) {
	s := &MilvusVectorStore{
		textField:         "content",
		sparseVectorField: "sparse_vector",
		metadataField:     "metadata",
		docIDField:        "document_id",
		databaseName:      "test_db",
	}
	assert.Nil(t, s.VectorField())
	assert.Equal(t, "content", s.TextField())
	assert.Equal(t, "sparse_vector", s.SparseVectorField())
	assert.Equal(t, "metadata", s.MetadataField())
	assert.Equal(t, "document_id", s.DocIDField())
	assert.Equal(t, "test_db", s.DatabaseName())
	// EnsureLoaded：collectionLoaded=true 时短路返回 nil（不触网络）
	s.mu.Lock()
	s.collectionLoaded = true
	s.mu.Unlock()
	assert.NoError(t, s.EnsureLoaded(context.Background()))
}
```

注意：若 graph_backfill_test.go 缺 `context`/`assert` 导入则补。若既有测试用例表明 `MilvusVectorStore.mu` 字段名不同，以实际为准（已核实字段存在，L115 附近 `collectionLoaded`）。

- [ ] **Step 2: 运行确认失败**

```bash
go test ./internal/agentcore/retrieval/vector_store/ -run '属性访问器回填' -count=1 2>&1 | tail -3
```
Expected: FAIL（`s.VectorField undefined` 等编译错误）

- [ ] **Step 3: chroma.go 追加 6 getter**

在 `DistanceMetric()` 访问器（L368 附近）之后追加：

```go
// DatabaseName 返回数据库名称（13.26 validate_index 消费）。
func (s *ChromaVectorStore) DatabaseName() string { return s.databaseName }

// TextField 返回文本字段名（13.26 validate_index 消费）。
func (s *ChromaVectorStore) TextField() string { return s.textField }

// VectorField 返回向量字段配置（13.26 validate_index 消费）。
// 返回 any 装箱联合类型事实：Chroma 侧 *ChromaVectorField、Milvus 侧
// MilvusVectorFieldConfig 接口——统一签名供 KB 包 attrEqual(DeepEqual) 比对。
func (s *ChromaVectorStore) VectorField() any { return s.vectorField }

// SparseVectorField 返回稀疏向量字段名（13.26 validate_index 消费）。
func (s *ChromaVectorStore) SparseVectorField() string { return s.sparseVectorField }

// MetadataField 返回元数据字段名（13.26 validate_index 消费）。
func (s *ChromaVectorStore) MetadataField() string { return s.metadataField }

// DocIDField 返回文档 ID 字段名（13.26 validate_index 消费）。
func (s *ChromaVectorStore) DocIDField() string { return s.docIDField }
```

- [ ] **Step 4: milvus.go 追加 6 getter + EnsureLoaded**

在 `DistanceMetric()` 访问器（L271 附近）之后追加：

```go
// DatabaseName 返回数据库名称（13.26 validate_index 消费，SimpleKB add_documents 复用）。
func (s *MilvusVectorStore) DatabaseName() string { return s.databaseName }

// TextField 返回文本字段名（13.26 validate_index 消费）。
func (s *MilvusVectorStore) TextField() string { return s.textField }

// VectorField 返回向量字段配置（13.26 validate_index 消费）。
// 返回 any 装箱联合类型事实（Chroma/Milvus 两型不同源，见 ChromaVectorStore.VectorField 注释）。
func (s *MilvusVectorStore) VectorField() any { return s.vectorField }

// SparseVectorField 返回稀疏向量字段名（13.26 validate_index 消费）。
func (s *MilvusVectorStore) SparseVectorField() string { return s.sparseVectorField }

// MetadataField 返回元数据字段名（13.26 validate_index 消费）。
func (s *MilvusVectorStore) MetadataField() string { return s.metadataField }

// DocIDField 返回文档 ID 字段名（13.26 validate_index 消费）。
func (s *MilvusVectorStore) DocIDField() string { return s.docIDField }

// EnsureLoaded 确保集合已加载（13.26 KB 构造期钩子，对齐 Python _ensure_loaded）。
// 一行转发既有私有 ensureLoaded；错误传播由调用方决定。
func (s *MilvusVectorStore) EnsureLoaded(ctx context.Context) error { return s.ensureLoaded(ctx) }
```

- [ ] **Step 5: 运行确认通过**

```bash
go test ./internal/agentcore/retrieval/vector_store/ -count=1 2>&1 | tail -3
```
Expected: PASS（全包既有测试不回归）

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/retrieval/vector_store/chroma.go internal/agentcore/retrieval/vector_store/milvus.go internal/agentcore/retrieval/vector_store/graph_backfill_test.go
git commit -m "feat(kb): vector_store 回填 6 getter + EnsureLoaded（13.26 validate_index 消费面）"
```

---

### Task 2: 回填 indexer 包 6 getter

**Files:**
- Modify: `internal/agentcore/retrieval/indexing/indexer/chroma.go`（ChromaIndexer 访问器区）
- Modify: `internal/agentcore/retrieval/indexing/indexer/milvus.go`（MilvusIndexer 访问器区）
- Test: `internal/agentcore/retrieval/indexing/indexer/base_test.go`（追加）

- [ ] **Step 1: 写失败测试**

在 base_test.go 追加：

```go
// TestChromaIndexer_属性访问器回填 13.26 新增 6 getter（validate_index 消费）。
func TestChromaIndexer_属性访问器回填(t *testing.T) {
	ci := &ChromaIndexer{
		textField:         "content",
		sparseVectorField: "sparse_vector",
		metadataField:     "metadata",
		docIDField:        "document_id",
		databaseName:      "test_db",
	}
	assert.Nil(t, ci.VectorField())
	assert.Equal(t, "content", ci.TextField())
	assert.Equal(t, "sparse_vector", ci.SparseVectorField())
	assert.Equal(t, "metadata", ci.MetadataField())
	assert.Equal(t, "document_id", ci.DocIDField())
	assert.Equal(t, "test_db", ci.DatabaseName())
}

// TestMilvusIndexer_属性访问器回填 13.26 新增 6 getter。
func TestMilvusIndexer_属性访问器回填(t *testing.T) {
	mi := &MilvusIndexer{
		textField:         "content",
		sparseVectorField: "sparse_vector",
		metadataField:     "metadata",
		docIDField:        "document_id",
		databaseName:      "test_db",
	}
	assert.Nil(t, mi.VectorField())
	assert.Equal(t, "content", mi.TextField())
	assert.Equal(t, "sparse_vector", mi.SparseVectorField())
	assert.Equal(t, "metadata", mi.MetadataField())
	assert.Equal(t, "document_id", mi.DocIDField())
	assert.Equal(t, "test_db", mi.DatabaseName())
}
```

- [ ] **Step 2: 运行确认失败**

```bash
go test ./internal/agentcore/retrieval/indexing/indexer/ -run '属性访问器回填' -count=1 2>&1 | tail -3
```
Expected: FAIL（`ci.VectorField undefined` 编译错误）

- [ ] **Step 3: chroma.go（indexer 包）追加 6 getter**

在 `DistanceMetric()`（L161）之后追加（代码与 Task 1 Step 3 同款，接收者改 `ci *ChromaIndexer`，字段同源）：

```go
// DatabaseName 返回数据库名称（13.26 validate_index 消费）。
func (ci *ChromaIndexer) DatabaseName() string { return ci.databaseName }

// TextField 返回文本字段名（13.26 validate_index 消费）。
func (ci *ChromaIndexer) TextField() string { return ci.textField }

// VectorField 返回向量字段配置（13.26 validate_index 消费，any 装箱联合类型）。
func (ci *ChromaIndexer) VectorField() any { return ci.vectorField }

// SparseVectorField 返回稀疏向量字段名（13.26 validate_index 消费）。
func (ci *ChromaIndexer) SparseVectorField() string { return ci.sparseVectorField }

// MetadataField 返回元数据字段名（13.26 validate_index 消费）。
func (ci *ChromaIndexer) MetadataField() string { return ci.metadataField }

// DocIDField 返回文档 ID 字段名（13.26 validate_index 消费）。
func (ci *ChromaIndexer) DocIDField() string { return ci.docIDField }
```

- [ ] **Step 4: milvus.go（indexer 包）追加 6 getter**

在 `DistanceMetric()`（L210）之后追加（接收者 `mi *MilvusIndexer`，同款 6 方法）：

```go
// DatabaseName 返回数据库名称（13.26 validate_index 消费）。
func (mi *MilvusIndexer) DatabaseName() string { return mi.databaseName }

// TextField 返回文本字段名（13.26 validate_index 消费）。
func (mi *MilvusIndexer) TextField() string { return mi.textField }

// VectorField 返回向量字段配置（13.26 validate_index 消费，any 装箱联合类型）。
func (mi *MilvusIndexer) VectorField() any { return mi.vectorField }

// SparseVectorField 返回稀疏向量字段名（13.26 validate_index 消费）。
func (mi *MilvusIndexer) SparseVectorField() string { return mi.sparseVectorField }

// MetadataField 返回元数据字段名（13.26 validate_index 消费）。
func (mi *MilvusIndexer) MetadataField() string { return mi.metadataField }

// DocIDField 返回文档 ID 字段名（13.26 validate_index 消费）。
func (mi *MilvusIndexer) DocIDField() string { return mi.docIDField }
```

- [ ] **Step 5: 运行确认通过**

```bash
go test ./internal/agentcore/retrieval/indexing/indexer/ -count=1 2>&1 | tail -3
```
Expected: PASS

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/retrieval/indexing/indexer/chroma.go internal/agentcore/retrieval/indexing/indexer/milvus.go internal/agentcore/retrieval/indexing/indexer/base_test.go
git commit -m "feat(kb): indexer 回填 6 getter（13.26 validate_index 消费面）"
```

---

### Task 3: knowledgebase 包骨架（doc.go + KBOptions + 接口 + 基座 + 构造器）

**Files:**
- Create: `internal/agentcore/retrieval/knowledgebase/doc.go`
- Create: `internal/agentcore/retrieval/knowledgebase/base.go`
- Test: `internal/agentcore/retrieval/knowledgebase/base_test.go`

- [ ] **Step 1: 创建 doc.go**

```go
// Package knowledgebase 提供知识库编排层抽象与实现。
//
// KnowledgeBase 是检索管线的顶层门面：统一文档写入（解析→分块→嵌入→
// 入库）与查询读取（多策略检索）的出入口。BaseKnowledgeBase 提供共享
// 基座（组件字段 + 配置一致性防线 + 4 模板方法），SimpleKnowledgeBase
// （13.27）与 GraphKnowledgeBase（13.28）作为子类嵌入实现 5 抽象方法。
//
// 核心机制——配置一致性防线：读侧 vector_store 与写侧 index_manager
// 分开构造、各持 7 项 schema 配置，两者首次配齐（构造尾部或二次 setter）
// 时逐项比对，不一致抛 155508（防"写 db1 读 db2 → 检索静默为空"漂移）。
//
// 文件目录：
//
//	knowledgebase/
//	├── doc.go           # 包文档
//	├── base.go          # KnowledgeBase 接口 + BaseKnowledgeBase + 构造/setter/模板方法（13.26）
//	├── validate.go      # ValidateIndex 三段式 + attrEqual + isChromaType（13.26）
//	├── simple.go        # SimpleKnowledgeBase + 多 KB 聚合（13.27 ⤵️ 待实施）
//	└── graph.go         # GraphKnowledgeBase（13.28 ⤵️ 待实施）
//
// 对应 Python 代码：
//
//	openjiuwen/core/retrieval/knowledge_base.py
//	openjiuwen/core/retrieval/simple_knowledge_base.py（13.27）
//	openjiuwen/core/retrieval/graph_knowledge_base.py（13.28）
//
// 核心类型/接口索引：
//
//	KnowledgeBase    — 知识库抽象接口（5 抽象 + 4 模板 + Config）
//	BaseKnowledgeBase — 子类共享基座（嵌入）
package knowledgebase
```

- [ ] **Step 2: 创建 base.go（接口 + 基座 + 构造器 + 选项 + 访问器）**

**注意**：本步骤先不实现模板方法与 setter（后续任务 TDD 补），但文件按规范 1 分节排列完整骨架，模板方法先返回零值占位会导致编译失败——所以本步骤**同时写入**：接口、KBOptions、基座、构造器、选项、访问器；模板方法 ParseFiles/ParseURLs/DeleteCollection/Close/setter 留空到 Task 4/5/6（**本步骤接口先不含模板方法，Task 4/5/6 逐个加入**——避免占位符违例）。

实际上为保持每步可编译可测试，本步骤 base.go 只含：

```go
package knowledgebase

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/embedding"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/reranker"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/indexing/indexer"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/indexing/processor/chunker"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/indexing/processor/extractor"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/indexing/processor/parser"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/vector_store"
)

// ──────────────────────────── 结构体 ────────────────────────────

// BaseKnowledgeBase 子类嵌入的共享基座（13.10 BaseParser 先例）。
//
// 持有知识库的 7 大组件与配置；模板方法（ParseFiles/ParseURLs/
// DeleteCollection/Close）与配置一致性防线由基座提供，5 抽象方法
// 由 SimpleKnowledgeBase（13.27）/GraphKnowledgeBase（13.28）实现。
//
// Python: knowledge_base.py (KnowledgeBase 的 __init__ 部分)
type BaseKnowledgeBase struct {
	// strictValidation 严格校验开关（Python strict_validation，默认 true）
	strictValidation bool
	// config 知识库配置
	config common.KnowledgeBaseConfig
	// vectorStore 读侧组件（检索）
	vectorStore vector_store.VectorStore
	// embedModel 嵌入模型
	embedModel embedding.BaseEmbedding
	// parser 文档解析器
	parser parser.Parser
	// chunker 文本分块器
	chunker chunker.Chunker
	// extractor 信息抽取器（如三元组）
	extractor extractor.Extractor
	// indexManager 写侧组件（建索引）
	indexManager indexer.Indexer
	// llmClient LLM 客户端（agentic 检索等）
	llmClient model_clients.BaseModelClient
}

// KBOptions 知识库方法选项（Python **kwargs 收纳）。
type KBOptions struct {
	// FileName ParseFiles 的 file_name 参数（空串=未设回落路径末段）
	FileName string
	// Extra 其余额外参数：ParseURLs 全量透传 parser；抽象方法子类消费
	Extra map[string]any
}

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────
```

编译期接口断言与构造器在 Step 3 测试驱动下补。

- [ ] **Step 3: 写失败测试（构造器默认值 + 全组件 + 单组件不触发校验）**

base_test.go 需要先建桩（Python MagicMock setattr 的方法版同构）。**桩只在本任务实现构造器所需最小面**（VectorStore/Indexer 接口 + 7 getter），模板方法测试所需的能力在 Task 4/5 扩充：

```go
package knowledgebase

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/query"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
)

// ──────────────────────────── 测试桩 ────────────────────────────

// fakeAttrStore 属性可配置的 VectorStore 桩（Python MagicMock setattr 同构）。
// 校验/模板方法路径不触达真实存储语义，接口方法返回零值。
type fakeAttrStore struct {
	databaseName      string
	distanceMetric    string
	textField         string
	vectorField       any
	sparseVectorField string
	metadataField     string
	docIDField        string
}

func (f *fakeAttrStore) CheckVectorField() error { return nil }
func (f *fakeAttrStore) Add(ctx context.Context, data []map[string]any, opts ...StoreOption) error {
	return nil
}
func (f *fakeAttrStore) Search(ctx context.Context, queryVector []float64, topK int, filters query.QueryExpr, opts ...StoreOption) ([]common.SearchResult, error) {
	return nil, nil
}
func (f *fakeAttrStore) SparseSearch(ctx context.Context, queryText string, topK int, filters query.QueryExpr, opts ...StoreOption) ([]common.SearchResult, error) {
	return nil, nil
}
func (f *fakeAttrStore) HybridSearch(ctx context.Context, queryText string, queryVector []float64, topK int, alpha float64, filters query.QueryExpr, opts ...StoreOption) ([]common.SearchResult, error) {
	return nil, nil
}
func (f *fakeAttrStore) Delete(ctx context.Context, ids []string, filterExpr query.QueryExpr) (bool, error) {
	return false, nil
}
func (f *fakeAttrStore) TableExists(ctx context.Context, tableName string) (bool, error) {
	return false, nil
}
func (f *fakeAttrStore) DeleteTable(ctx context.Context, tableName string) error { return nil }
func (f *fakeAttrStore) Close()                                                    {}

// storageAttrProvider 7 getter（13.26 校验消费接口）
func (f *fakeAttrStore) DatabaseName() string      { return f.databaseName }
func (f *fakeAttrStore) DistanceMetric() string    { return f.distanceMetric }
func (f *fakeAttrStore) TextField() string         { return f.textField }
func (f *fakeAttrStore) VectorField() any          { return f.vectorField }
func (f *fakeAttrStore) SparseVectorField() string { return f.sparseVectorField }
func (f *fakeAttrStore) MetadataField() string     { return f.metadataField }
func (f *fakeAttrStore) DocIDField() string        { return f.docIDField }

// fakeAttrIndexer 属性可配置的 Indexer 桩。
type fakeAttrIndexer struct {
	databaseName      string
	distanceMetric    string
	textField         string
	vectorField       any
	sparseVectorField string
	metadataField     string
	docIDField        string
}

func (f *fakeAttrIndexer) BuildIndex(ctx context.Context, chunks []common.TextChunk, config common.IndexConfig, embedModel embedding.BaseEmbedding, opts ...indexer.IndexOption) (bool, error) {
	return true, nil
}
func (f *fakeAttrIndexer) UpdateIndex(ctx context.Context, chunks []common.TextChunk, docID string, config common.IndexConfig, embedModel embedding.BaseEmbedding, opts ...indexer.IndexOption) (bool, error) {
	return true, nil
}
func (f *fakeAttrIndexer) DeleteIndex(ctx context.Context, docID string, indexName string) (bool, error) {
	return true, nil
}
func (f *fakeAttrIndexer) IndexExists(ctx context.Context, indexName string) (bool, error) {
	return true, nil
}
func (f *fakeAttrIndexer) GetIndexInfo(ctx context.Context, indexName string) (map[string]any, error) {
	return nil, nil
}

func (f *fakeAttrIndexer) DatabaseName() string      { return f.databaseName }
func (f *fakeAttrIndexer) DistanceMetric() string    { return f.distanceMetric }
func (f *fakeAttrIndexer) TextField() string         { return f.textField }
func (f *fakeAttrIndexer) VectorField() any          { return f.vectorField }
func (f *fakeAttrIndexer) SparseVectorField() string { return f.sparseVectorField }
func (f *fakeAttrIndexer) MetadataField() string     { return f.metadataField }
func (f *fakeAttrIndexer) DocIDField() string        { return f.docIDField }

// makeCompatiblePair 构造全属性匹配的桩对（Python _create_compatible_mocks 同构）。
func makeCompatiblePair() (*fakeAttrStore, *fakeAttrIndexer) {
	vs := &fakeAttrStore{
		databaseName: "test_db", distanceMetric: "cosine", textField: "content",
		vectorField: any("test_vector_field"), sparseVectorField: "sparse_vector",
		metadataField: "metadata", docIDField: "document_id",
	}
	im := &fakeAttrIndexer{
		databaseName: "test_db", distanceMetric: "cosine", textField: "content",
		vectorField: any("test_vector_field"), sparseVectorField: "sparse_vector",
		metadataField: "metadata", docIDField: "document_id",
	}
	return vs, im
}

// ──────────────────────────── 构造器测试 ────────────────────────────

// TestNewBaseKnowledgeBase_默认构造 对齐 test_init（全组件 nil，无校验触发）。
func TestNewBaseKnowledgeBase_默认构造(t *testing.T) {
	cfg := common.KnowledgeBaseConfig{KBID: "test_kb"}
	b, err := NewBaseKnowledgeBase(cfg)
	assert.NoError(t, err)
	assert.Equal(t, cfg, b.config)
	assert.Nil(t, b.vectorStore)
	assert.Nil(t, b.embedModel)
	assert.Nil(t, b.parser)
	assert.Nil(t, b.chunker)
	assert.Nil(t, b.extractor)
	assert.Nil(t, b.indexManager)
	assert.Nil(t, b.llmClient)
	assert.True(t, b.strictValidation) // Python 默认 true
}

// TestNewBaseKnowledgeBase_全组件构造 对齐 test_init_with_components。
func TestNewBaseKnowledgeBase_全组件构造(t *testing.T) {
	vs, im := makeCompatiblePair()
	b, err := NewBaseKnowledgeBase(common.KnowledgeBaseConfig{KBID: "test_kb"},
		WithVectorStore(vs), WithIndexManager(im))
	assert.NoError(t, err)
	assert.Equal(t, vs, b.vectorStore)
	assert.Equal(t, im, b.indexManager)
}

// TestNewBaseKnowledgeBase_单组件不触发校验 对齐 Python 单组件赋值时刻另一侧 None。
func TestNewBaseKnowledgeBase_单组件不触发校验(t *testing.T) {
	// 只有 vs（属性故意不匹配也不校验——im 缺失）
	vs, _ := makeCompatiblePair()
	im2 := &fakeAttrIndexer{databaseName: "another_db"}
	_ = im2 // 不传入
	b, err := NewBaseKnowledgeBase(common.KnowledgeBaseConfig{KBID: "test_kb"}, WithVectorStore(vs))
	assert.NoError(t, err)
	assert.NotNil(t, b.vectorStore)
	// 只有 im
	b2, err := NewBaseKnowledgeBase(common.KnowledgeBaseConfig{KBID: "test_kb"}, WithIndexManager(im2))
	assert.NoError(t, err)
	assert.NotNil(t, b2.indexManager)
}
```

- [ ] **Step 4: 运行确认失败**

```bash
go test ./internal/agentcore/retrieval/knowledgebase/ -count=1 2>&1 | tail -5
```
Expected: FAIL（`NewBaseKnowledgeBase undefined` / `WithVectorStore undefined` 编译错误）

- [ ] **Step 5: 补全 base.go 构造器 + 选项 + 访问器**

在 base.go 追加（规范 2 顺序：选项类型归全局区块、构造器归导出函数区块）：

```go
// KnowledgeBaseOption BaseKnowledgeBase 构造选项。
type KnowledgeBaseOption func(*BaseKnowledgeBase)

// NewBaseKnowledgeBase 构造知识库基座。
//
// 构造尾部：vectorStore 与 indexManager 双非空时触发 postAssignValidation
// （对齐 Python __init__ 赋值序：index_manager 赋值时刻两者配齐触发）。
func NewBaseKnowledgeBase(config common.KnowledgeBaseConfig, opts ...KnowledgeBaseOption) (*BaseKnowledgeBase, error) {
	b := &BaseKnowledgeBase{
		strictValidation: true, // Python 默认 true
		config:           config,
	}
	for _, opt := range opts {
		opt(b)
	}
	if b.vectorStore != nil && b.indexManager != nil {
		if err := b.postAssignValidation(context.Background()); err != nil {
			return nil, err
		}
	}
	return b, nil
}

// WithVectorStore 设置读侧组件。
func WithVectorStore(vs vector_store.VectorStore) KnowledgeBaseOption {
	return func(b *BaseKnowledgeBase) { b.vectorStore = vs }
}

// WithEmbedModel 设置嵌入模型。
func WithEmbedModel(em embedding.BaseEmbedding) KnowledgeBaseOption {
	return func(b *BaseKnowledgeBase) { b.embedModel = em }
}

// WithParser 设置解析器。
func WithParser(p parser.Parser) KnowledgeBaseOption {
	return func(b *BaseKnowledgeBase) { b.parser = p }
}

// WithChunker 设置分块器。
func WithChunker(c chunker.Chunker) KnowledgeBaseOption {
	return func(b *BaseKnowledgeBase) { b.chunker = c }
}

// WithExtractor 设置抽取器。
func WithExtractor(e extractor.Extractor) KnowledgeBaseOption {
	return func(b *BaseKnowledgeBase) { b.extractor = e }
}

// WithIndexManager 设置写侧组件。
func WithIndexManager(im indexer.Indexer) KnowledgeBaseOption {
	return func(b *BaseKnowledgeBase) { b.indexManager = im }
}

// WithLLMClient 设置 LLM 客户端。
func WithLLMClient(c model_clients.BaseModelClient) KnowledgeBaseOption {
	return func(b *BaseKnowledgeBase) { b.llmClient = c }
}

// WithStrictValidationFalse 关闭严格校验（Python strict_validation=False）。
func WithStrictValidationFalse() KnowledgeBaseOption {
	return func(b *BaseKnowledgeBase) { b.strictValidation = false }
}

// Config 返回知识库配置（Python kb.config 属性镜像）。
func (b *BaseKnowledgeBase) Config() common.KnowledgeBaseConfig { return b.config }

// StrictValidation 返回严格校验开关。
func (b *BaseKnowledgeBase) StrictValidation() bool { return b.strictValidation }

// VectorStore 返回读侧组件。
func (b *BaseKnowledgeBase) VectorStore() vector_store.VectorStore { return b.vectorStore }

// IndexManager 返回写侧组件。
func (b *BaseKnowledgeBase) IndexManager() indexer.Indexer { return b.indexManager }
```

`postAssignValidation` 本步骤先建**最小桩**让编译通过（Task 4 实现）：

```go
// postAssignValidation 赋值期三段式校验（Task 4 实现，本处占位返回 nil）。
// ⚠️ Task 4 将替换此实现。
func (b *BaseKnowledgeBase) postAssignValidation(ctx context.Context) error { return nil }
```

- [ ] **Step 6: 运行确认通过**

```bash
go test ./internal/agentcore/retrieval/knowledgebase/ -count=1 2>&1 | tail -3
```
Expected: PASS（3 例）

- [ ] **Step 7: 提交**

```bash
git add internal/agentcore/retrieval/knowledgebase/
git commit -m "feat(kb): knowledgebase 包骨架——基座/构造器/8选项/访问器（13.26）"
```

---

### Task 4: 校验流水线 validate.go（三段式核心）

**Files:**
- Create: `internal/agentcore/retrieval/knowledgebase/validate.go`
- Test: `internal/agentcore/retrieval/knowledgebase/validate_test.go`
- Modify: `internal/agentcore/retrieval/knowledgebase/base.go`（替换 postAssignValidation 占位）

- [ ] **Step 1: 写失败测试（Python validation 15 例中的核心 6 例先行）**

validate_test.go：

```go
package knowledgebase

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 校验流水线测试 ────────────────────────────

// TestValidateIndex_全属性匹配 对齐 test_validation_passes_when_all_attributes_match。
func TestValidateIndex_全属性匹配(t *testing.T) {
	vs, im := makeCompatiblePair()
	b, err := NewBaseKnowledgeBase(common.KnowledgeBaseConfig{KBID: "test_kb"},
		WithVectorStore(vs), WithIndexManager(im))
	require.NoError(t, err)
	assert.NoError(t, b.ValidateIndex(context.Background()))
}

// TestValidateIndex_vs缺失跳过 对齐 test_validation_skipped_when_vector_store_is_none。
func TestValidateIndex_vs缺失跳过(t *testing.T) {
	_, im := makeCompatiblePair()
	b, err := NewBaseKnowledgeBase(common.KnowledgeBaseConfig{KBID: "test_kb"}, WithIndexManager(im))
	require.NoError(t, err)
	// validate_index 直接调用：vs nil 时 7 属性读取得 nil，与 im 值不等——
	// 但 Python 语义：构造期双非空才触发；直接调 validate_index 时
	// getattr(None, ...) 也是 None。实际 Python validate_index 在 vs=None
	// 时 vector_store_val 全 None vs im 有值 → 不匹配 → 报错！
	// 复核：Python 测试只验证"构造不炸"（跳过触发），非 validate_index 本体。
	// Go 对齐：本例只断言构造不炸 + CheckVectorField 不被调（vs nil 分支）。
	assert.NotNil(t, b)
}

// TestValidateIndex_七属性逐项不匹配 对齐 test_validation_fails_on_mismatch_*（7 例合一）。
func TestValidateIndex_七属性逐项不匹配(t *testing.T) {
	cases := []struct {
		name string
		mut  func(vs *fakeAttrStore, im *fakeAttrIndexer)
		attr string
	}{
		{"database_name", func(vs *fakeAttrStore, im *fakeAttrIndexer) { im.databaseName = "db2" }, "database_name"},
		{"distance_metric", func(vs *fakeAttrStore, im *fakeAttrIndexer) { im.distanceMetric = "euclidean" }, "distance_metric"},
		{"text_field", func(vs *fakeAttrStore, im *fakeAttrIndexer) { im.textField = "text" }, "text_field"},
		{"vector_field", func(vs *fakeAttrStore, im *fakeAttrIndexer) { im.vectorField = any("vector") }, "vector_field"},
		{"sparse_vector_field", func(vs *fakeAttrStore, im *fakeAttrIndexer) { im.sparseVectorField = "bm25_vector" }, "sparse_vector_field"},
		{"metadata_field", func(vs *fakeAttrStore, im *fakeAttrIndexer) { im.metadataField = "meta" }, "metadata_field"},
		{"doc_id_field", func(vs *fakeAttrStore, im *fakeAttrIndexer) { im.docIDField = "doc_id" }, "doc_id_field"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			vs, im := makeCompatiblePair()
			tc.mut(vs, im)
			_, err := NewBaseKnowledgeBase(common.KnowledgeBaseConfig{KBID: "test_kb"},
				WithVectorStore(vs), WithIndexManager(im))
			require.Error(t, err)
			var be *exception.BaseError
			require.True(t, errors.As(err, &be))
			assert.Equal(t, 155508, be.Code())
			assert.Contains(t, be.Message(), "incompatible "+tc.attr+" configs")
		})
	}
}

// TestValidateIndex_错误消息含类型名与值 对齐 test_validation_error_message_includes_type_names。
func TestValidateIndex_错误消息含类型名与值(t *testing.T) {
	vs, im := makeCompatiblePair()
	im.databaseName = "db2"
	_, err := NewBaseKnowledgeBase(common.KnowledgeBaseConfig{KBID: "test_kb"},
		WithVectorStore(vs), WithIndexManager(im))
	require.Error(t, err)
	msg := err.Error()
	assert.Contains(t, msg, "fakeAttrStore")
	assert.Contains(t, msg, "fakeAttrIndexer")
	assert.Contains(t, msg, "test_db")
	assert.Contains(t, msg, "db2")
}

// TestValidateIndex_双nil属性通过 对齐 test_validation_passes_when_attributes_are_none。
func TestValidateIndex_双nil属性通过(t *testing.T) {
	vs := &fakeAttrStore{} // 全零值：vectorField any 为 nil，其余空串
	im := &fakeAttrIndexer{}
	// 空串 == 空串 匹配；vectorField nil == nil 匹配
	_, err := NewBaseKnowledgeBase(common.KnowledgeBaseConfig{KBID: "test_kb"},
		WithVectorStore(vs), WithIndexManager(im))
	assert.NoError(t, err)
}

// TestValidateIndex_首个不匹配即停 对齐 test_validation_fails_on_first_mismatch（顺序敏感）。
func TestValidateIndex_首个不匹配即停(t *testing.T) {
	vs, im := makeCompatiblePair()
	vs.databaseName = "db1"
	im.databaseName = "db2"
	vs.distanceMetric = "cosine"
	im.distanceMetric = "euclidean"
	_, err := NewBaseKnowledgeBase(common.KnowledgeBaseConfig{KBID: "test_kb"},
		WithVectorStore(vs), WithIndexManager(im))
	require.Error(t, err)
	// database_name 在 distance_metric 之前——消息只含 database_name
	assert.Contains(t, err.Error(), "incompatible database_name configs")
	assert.NotContains(t, err.Error(), "incompatible distance_metric configs")
}
```

- [ ] **Step 2: 运行确认失败**

```bash
go test ./internal/agentcore/retrieval/knowledgebase/ -run TestValidateIndex -count=1 2>&1 | tail -5
```
Expected: FAIL（`b.ValidateIndex undefined` 编译错误；全属性匹配/双 nil 例因占位返回 nil 而"假通过"——七属性不匹配例驱动真实实现）

- [ ] **Step 3: 实现 validate.go**

```go
package knowledgebase

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 接口 ────────────────────────────

// storageAttrProvider 校验流水线的属性消费接口（非导出，消费侧接口惯例）。
//
// ChromaVectorStore/MilvusVectorStore/ChromaIndexer/MilvusIndexer 已回填
// 7 getter（13.26）。未实现本接口的对象其属性按 nil 处理——对齐 Python
// getattr(obj, attr, None) 缺属性语义。
type storageAttrProvider interface {
	DatabaseName() string
	DistanceMetric() string
	TextField() string
	VectorField() any
	SparseVectorField() string
	MetadataField() string
	DocIDField() string
}

// ensureLoader 可选加载钩子（对齐 Python getattr(vs, "_ensure_loaded", None)）。
// MilvusVectorStore 实现本接口；ChromaVectorStore 无此方法 → 断言失败跳过。
type ensureLoader interface {
	EnsureLoaded(ctx context.Context) error
}

// ──────────────────────────── 常量 ────────────────────────────

// validateAttrOrder 7 属性固定比对顺序（Python 字面：
// ["database_name", "distance_metric"] + data_fields）。
var validateAttrOrder = []string{
	"database_name", "distance_metric", "text_field", "vector_field",
	"sparse_vector_field", "metadata_field", "doc_id_field",
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ValidateIndex 校验 vector store 与 index manager 的 7 项配置一致性。
//
// 任一属性不等抛 155508（错误消息含属性名+双方值+类型名）；
// strict 且 vectorStore 非 nil 时执行 CheckVectorField 并传播错误。
//
// Python: knowledge_base.py validate_index
func (b *BaseKnowledgeBase) ValidateIndex(ctx context.Context) error {
	for _, attr := range validateAttrOrder {
		vsVal := storageAttr(b.vectorStore, attr)
		imVal := storageAttr(b.indexManager, attr)
		if !attrEqual(vsVal, imVal) {
			return exception.BuildError(
				exception.StatusRetrievalKbDatabaseConfigInvalid,
				exception.WithMsg(fmt.Sprintf(
					"Vector store and index manager have incompatible %s configs:\n"+
						"- Vector Store (%s) is using \"%s\"\n"+
						"- Index manager (%s) is using \"%s\"",
					attr, typeName(b.vectorStore), renderAttrVal(vsVal),
					typeName(b.indexManager), renderAttrVal(imVal),
				)),
			)
		}
	}
	if b.strictValidation && b.vectorStore != nil {
		return b.vectorStore.CheckVectorField()
	}
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// postAssignValidation 赋值期三段式校验（Python __setattr__ 的显式对应物）。
//
// ① ValidateIndex（7 属性比对 + CheckVectorField）
// ② Chroma 本地限制拦截（类型名前缀探测）
// ③ ensureLoader 钩子（Milvus 构造期 load collection）
func (b *BaseKnowledgeBase) postAssignValidation(ctx context.Context) error {
	if err := b.ValidateIndex(ctx); err != nil {
		return err
	}
	if isChromaType(b.vectorStore) || isChromaType(b.indexManager) {
		if b.strictValidation && b.config.IndexType != common.IndexTypeVector {
			return exception.BuildError(
				exception.StatusRetrievalKbDatabaseConfigInvalid,
				exception.WithMsg("Chroma database does not support sparse embedding & hybrid search in local mode yet"),
			)
		}
	}
	if el, ok := b.vectorStore.(ensureLoader); ok {
		return el.EnsureLoaded(ctx)
	}
	return nil
}

// storageAttr 按属性名读取组件属性（getattr(obj, attr, None) 同构）。
func storageAttr(obj any, attr string) any {
	p, ok := obj.(storageAttrProvider)
	if !ok {
		return nil
	}
	switch attr {
	case "database_name":
		return p.DatabaseName()
	case "distance_metric":
		return p.DistanceMetric()
	case "text_field":
		return p.TextField()
	case "vector_field":
		return p.VectorField()
	case "sparse_vector_field":
		return p.SparseVectorField()
	case "metadata_field":
		return p.MetadataField()
	case "doc_id_field":
		return p.DocIDField()
	}
	return nil
}

// attrEqual 属性相等比较（V1 拍板：any 槽位 + DeepEqual）。
//
// 双 nil → true；单 nil → false；DeepEqual 首步即类型比较——
// Chroma 配置 vs Milvus 配置跨源天然 false → 155508（混配拦截）。
func attrEqual(a, b any) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return reflect.DeepEqual(a, b)
}

// isChromaType 类型名 "chroma" 前缀探测（R1 拍板）。
//
// 对齐 Python type(...).__name__.casefold().startswith("chroma")：
// 命名约定而非行为契约，第三方 Chroma 命名类型同样被拦。
func isChromaType(obj any) bool {
	t := reflect.TypeOf(obj)
	if t == nil {
		return false
	}
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	return strings.HasPrefix(strings.ToLower(t.Name()), "chroma")
}

// typeName 反射取类型名（错误消息用）。
func typeName(obj any) string {
	if obj == nil {
		return "NoneType"
	}
	t := reflect.TypeOf(obj)
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	return t.Name()
}

// renderAttrVal 渲染属性值（对齐 Python str() 形态：None 原样，其余 %v）。
func renderAttrVal(v any) string {
	if v == nil {
		return "None"
	}
	return fmt.Sprintf("%v", v)
}
```

**同时**替换 base.go 的占位（删除占位函数——真实现移入 validate.go）。base.go 需补 import `"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"`（已有）。validate.go 需补 common 导入（IndexTypeVector）。

- [ ] **Step 4: 运行确认通过**

```bash
go test ./internal/agentcore/retrieval/knowledgebase/ -count=1 2>&1 | tail -3
```
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/retrieval/knowledgebase/
git commit -m "feat(kb): 三段式校验流水线 ValidateIndex/postAssignValidation + attrEqual/isChromaType（13.26 核心）"
```

---

### Task 5: 双 setter（二次赋值触发）

**Files:**
- Modify: `internal/agentcore/retrieval/knowledgebase/base.go`
- Test: `internal/agentcore/retrieval/knowledgebase/validate_test.go`（追加）

- [ ] **Step 1: 写失败测试（Python 2 例 setter 触发 + Go 增强 3 例）**

validate_test.go 追加：

```go
// ──────────────────────────── setter 测试 ────────────────────────────

// TestSetVectorStore_后置触发校验 对齐 test_validation_runs_when_setting_vector_store_after_index_manager。
func TestSetVectorStore_后置触发校验(t *testing.T) {
	vs, im := makeCompatiblePair()
	b, err := NewBaseKnowledgeBase(common.KnowledgeBaseConfig{KBID: "test_kb"}, WithIndexManager(im))
	require.NoError(t, err)
	assert.NoError(t, b.SetVectorStore(vs))
	assert.Equal(t, vs, b.vectorStore)
}

// TestSetIndexManager_后置触发校验 对齐 test_validation_runs_when_setting_index_manager_after_vector_store。
func TestSetIndexManager_后置触发校验(t *testing.T) {
	vs, im := makeCompatiblePair()
	b, err := NewBaseKnowledgeBase(common.KnowledgeBaseConfig{KBID: "test_kb"}, WithVectorStore(vs))
	require.NoError(t, err)
	assert.NoError(t, b.SetIndexManager(im))
	assert.Equal(t, im, b.indexManager)
}

// TestSetVectorStore_不匹配拒绝 setter 后配置漂移场景（Go 增强）。
func TestSetVectorStore_不匹配拒绝(t *testing.T) {
	vs, im := makeCompatiblePair()
	im.databaseName = "db2"
	b, err := NewBaseKnowledgeBase(common.KnowledgeBaseConfig{KBID: "test_kb"}, WithIndexManager(im))
	require.NoError(t, err)
	err = b.SetVectorStore(vs)
	require.Error(t, err)
	var be *exception.BaseError
	require.True(t, errors.As(err, &be))
	assert.Equal(t, 155508, be.Code())
}

// TestSetVectorStore_nil无校验 SetVectorStore(nil) 不触发校验不 panic（Go 增强）。
func TestSetVectorStore_nil无校验(t *testing.T) {
	vs, im := makeCompatiblePair()
	b, err := NewBaseKnowledgeBase(common.KnowledgeBaseConfig{KBID: "test_kb"},
		WithVectorStore(vs), WithIndexManager(im))
	require.NoError(t, err)
	assert.NoError(t, b.SetVectorStore(nil))
	assert.Nil(t, b.vectorStore)
}

// TestSetIndexManager_卸载后再配齐重新校验（Go 增强：二次变更语义）。
func TestSetIndexManager_卸载后再配齐重新校验(t *testing.T) {
	vs, im := makeCompatiblePair()
	b, err := NewBaseKnowledgeBase(common.KnowledgeBaseConfig{KBID: "test_kb"},
		WithVectorStore(vs), WithIndexManager(im))
	require.NoError(t, err)
	require.NoError(t, b.SetIndexManager(nil)) // 卸载
	im2, _ := makeCompatiblePair()
	assert.NoError(t, b.SetIndexManager(im2)) // 重新配齐 → 重新校验（兼容通过）
}
```

- [ ] **Step 2: 运行确认失败**

```bash
go test ./internal/agentcore/retrieval/knowledgebase/ -run TestSet -count=1 2>&1 | tail -3
```
Expected: FAIL（`b.SetVectorStore undefined` 编译错误）

- [ ] **Step 3: base.go 追加双 setter**

```go
// SetVectorStore 构造后替换读侧组件（Python kb.vector_store = vs 显式对应物）。
//
// 赋值后双组件非空时触发 postAssignValidation（对齐 Python __setattr__）。
func (b *BaseKnowledgeBase) SetVectorStore(vs vector_store.VectorStore) error {
	b.vectorStore = vs
	if b.vectorStore != nil && b.indexManager != nil {
		return b.postAssignValidation(context.Background())
	}
	return nil
}

// SetIndexManager 构造后替换写侧组件（Python kb.index_manager = im 显式对应物）。
func (b *BaseKnowledgeBase) SetIndexManager(im indexer.Indexer) error {
	b.indexManager = im
	if b.vectorStore != nil && b.indexManager != nil {
		return b.postAssignValidation(context.Background())
	}
	return nil
}
```

- [ ] **Step 4: 运行确认通过**

```bash
go test ./internal/agentcore/retrieval/knowledgebase/ -count=1 2>&1 | tail -3
```
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/retrieval/knowledgebase/
git commit -m "feat(kb): SetVectorStore/SetIndexManager 二次赋值触发校验（13.26 方案B）"
```

---

### Task 6: KnowledgeBase 接口 + 模板方法（ParseFiles/ParseURLs/DeleteCollection/Close）

**Files:**
- Modify: `internal/agentcore/retrieval/knowledgebase/base.go`
- Test: `internal/agentcore/retrieval/knowledgebase/base_test.go`（追加桩能力 + 测试）

- [ ] **Step 1: 扩充桩 + 写失败测试**

base_test.go 追加（Parser 桩 + Close 桩 + 8 例 close/模板方法测试）：

```go
// fakeKBParser 记录调用参数的 Parser 桩（Python AsyncMock parser 同构）。
type fakeKBParser struct {
	// docs 预置返回
	docs []reranker.Document
	// err 预置返回错误
	err error
	// supports 返回值
	supports bool
	// gotDocs 收到的 doc 参数记录
	gotDocs []string
	// gotOpts 收到的 opts 快照（ParseOptions 深拷贝断言用）
	// （NewParseOptions 返回 *ParseOptions——base.go L95 已核实）
	gotOpts []*parser.ParseOptions
}

func (p *fakeKBParser) Parse(ctx context.Context, doc string, opts ...parser.ParseOption) ([]reranker.Document, error) {
	p.gotDocs = append(p.gotDocs, doc)
	p.gotOpts = append(p.gotOpts, parser.NewParseOptions(opts...))
	if p.err != nil {
		return nil, p.err
	}
	return p.docs, nil
}
func (p *fakeKBParser) LazyParse(ctx context.Context, doc string, fn func(reranker.Document) error, opts ...parser.ParseOption) error {
	return nil
}
func (p *fakeKBParser) Process(ctx context.Context, doc string, opts ...parser.ParseOption) ([]reranker.Document, error) {
	return p.Parse(ctx, doc, opts...)
}
func (p *fakeKBParser) Supports(doc string) bool { return p.supports }
func (p *fakeKBParser) ParseContent(ctx context.Context, doc string, llmClient *llm.Model) (string, error) {
	return "", nil
}

// fakeErrCloserStore Close 返回错误的 store 桩（test_close_with_exception 通道）。
type fakeErrCloserStore struct {
	fakeAttrStore
	closeErr error
	closed   bool
}

// Close 覆写为错误变体（遮蔽 fakeAttrStore 的 void Close）。
// ⚠️ 注意：maybeClose 断言 interface{ Close() error }，Go 方法集规则：
// 同时有 Close() 和 Close() error 是重复方法名编译错误！
// 改用独立类型（不嵌 fakeAttrStore）+ 完整接口实现。
```

**重要实现提示**：`Close() error` 与 `Close()` 不能共存于一个类型——`fakeErrCloserStore` 改为独立 struct 实现 `VectorStore` 接口 + `Close() error`（遮蔽接口的 void Close 需另命名类型断言，Go 不允许同类型双 Close 方法）。**方案**：错误关闭桩不实现 VectorStore 接口，Close 测试直接构造只含 vectorStore 字段失败场景时——但 BaseKnowledgeBase.vectorStore 字段类型是 `vector_store.VectorStore` 接口（接口已含 void Close()）……

**这意味着 maybeClose 的 `interface{ Close() error }` 断言对 VectorStore 永远为 false**（接口方法集已含 void Close）。修正设计（spec §4.4 注已知 VectorStore 接口有 void Close）：

maybeClose 逻辑修正为：

```go
// maybeClose 关闭组件（Python _maybe_await 同构：先试 void，后试 error 变体）。
// VectorStore/Indexer 的真实类型 Close() 均为 void——error 变体分支
// 为 test_close_with_exception 的映射通道（fake 类型专属）。
func (b *BaseKnowledgeBase) maybeClose(obj any) {
	if obj == nil {
		return
	}
	if c, ok := obj.(interface{ Close() }); ok {
		c.Close()
		return
	}
	if c, ok := obj.(interface{ Close() error }); ok {
		if err := c.Close(); err != nil {
			logger.Warn(logComponent).Err(err).Msg("Failed to close object")
		}
	}
}
```

由于接口嵌入，VectorStore 桩（fakeAttrStore）走 void 分支。**error 变体桩需要既满足 VectorStore 接口又有 error Close**——Go 不允许。所以 error 变体测试改走 **indexManager 槽**：Indexer 接口**不含** Close 方法！fakeErrCloserIndexer 实现 Indexer 5 方法 + Close() error：

```go
// fakeErrCloserIndexer Indexer 接口不含 Close——可自由加 Close() error 变体
// （test_close_with_exception 的 Go 映射通道）。
type fakeErrCloserIndexer struct {
	fakeAttrIndexer
	closeErr error
	closed   int
}

// Close 错误变体（Indexer 接口无 Close，无遮蔽冲突）。
func (f *fakeErrCloserIndexer) Close() error {
	f.closed++
	return f.closeErr
}
```

测试：

```go
// ──────────────────────────── 模板方法测试 ────────────────────────────

// TestParseFiles_成功 对齐 test_parse_files_success（Python simple 测试
// 覆盖，KB 基类 Go 增强补全——Python 基类测试未测模板方法）。
func TestParseFiles_成功(t *testing.T) {
	p := &fakeKBParser{
		docs: []reranker.Document{{ID: "doc_1", Text: "text1"}},
	}
	b, err := NewBaseKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb"}, WithParser(p))
	require.NoError(t, err)
	docs, err := b.ParseFiles(context.Background(), []string{"/a/b/file.txt"})
	require.NoError(t, err)
	assert.Len(t, docs, 1)
	assert.Equal(t, "/a/b/file.txt", p.gotDocs[0])
	// file_name 回落：未传 FileName → "/" split 末段
	assert.Equal(t, map[string]any{"file_name": "file.txt"}, p.gotOpts[0].Extra)
}

// TestParseFiles_无parser 对齐 test_parse_files_without_parser（155500）。
func TestParseFiles_无parser(t *testing.T) {
	b, err := NewBaseKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb"})
	require.NoError(t, err)
	_, err = b.ParseFiles(context.Background(), []string{"f.txt"})
	require.Error(t, err)
	var be *exception.BaseError
	require.True(t, errors.As(err, &be))
	assert.Equal(t, 155500, be.Code())
	assert.Contains(t, be.Message(), "parser is required for parse_files")
}

// TestParseFiles_单文件失败继续 对齐 test_parse_files_with_exception（软失败 continue）。
func TestParseFiles_单文件失败继续(t *testing.T) {
	p := &fakeKBParser{err: errors.New("boom"), docs: []reranker.Document{{ID: "d1"}}}
	b, err := NewBaseKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb"}, WithParser(p))
	require.NoError(t, err)
	docs, err := b.ParseFiles(context.Background(), []string{"a.txt", "b.txt"})
	require.NoError(t, err) // 异常被吞
	assert.Empty(t, docs)
	assert.Len(t, p.gotDocs, 2) // 两个文件都尝试了
}

// TestParseFiles_FileName选项 KBOptions.FileName 透传（Go 增强）。
func TestParseFiles_FileName选项(t *testing.T) {
	p := &fakeKBParser{docs: []reranker.Document{{ID: "d1"}}}
	b, _ := NewBaseKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb"}, WithParser(p))
	_, _ = b.ParseFiles(context.Background(), []string{"/a/b/file.txt"}, WithKBFileName("custom.md"))
	assert.Equal(t, map[string]any{"file_name": "custom.md"}, p.gotOpts[0].Extra)
}

// TestParseURLs_成功 对齐 test_parse_files_success 的 URL 版。
func TestParseURLs_成功(t *testing.T) {
	p := &fakeKBParser{supports: true, docs: []reranker.Document{{ID: "d1"}}}
	b, _ := NewBaseKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb"}, WithParser(p))
	docs, err := b.ParseURLs(context.Background(), []string{"https://x.com/a"}, WithKBExtra("k1", "v1"))
	require.NoError(t, err)
	assert.Len(t, docs, 1)
	// Extra 全量透传
	assert.Equal(t, map[string]any{"k1": "v1"}, p.gotOpts[0].Extra)
}

// TestParseURLs_不支持跳过 对齐 supports False → Warn + continue。
func TestParseURLs_不支持跳过(t *testing.T) {
	p := &fakeKBParser{supports: false}
	b, _ := NewBaseKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb"}, WithParser(p))
	docs, err := b.ParseURLs(context.Background(), []string{"https://x.com/a"})
	require.NoError(t, err)
	assert.Empty(t, docs)
	assert.Empty(t, p.gotDocs) // 未尝试 parse
}

// TestParseURLs_无parser 155500 error_msg 不同（parse_urls 变体）。
func TestParseURLs_无parser(t *testing.T) {
	b, _ := NewBaseKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb"})
	_, err := b.ParseURLs(context.Background(), []string{"https://x.com"})
	require.Error(t, err)
	var be *exception.BaseError
	require.True(t, errors.As(err, &be))
	assert.Equal(t, 155500, be.Code())
	assert.Contains(t, be.Message(), "parser is required for parse_urls")
}

// TestDeleteCollection_无store 155503。
func TestDeleteCollection_无store(t *testing.T) {
	b, _ := NewBaseKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb"})
	err := b.DeleteCollection(context.Background(), "coll")
	require.Error(t, err)
	var be *exception.BaseError
	require.True(t, errors.As(err, &be))
	assert.Equal(t, 155503, be.Code())
	assert.Contains(t, be.Message(), "vector_store is required for delete_collection")
}

// TestDeleteCollection_转发 对齐 delete_table 转发。
func TestDeleteCollection_转发(t *testing.T) {
	vs, im := makeCompatiblePair()
	_ = im
	b, _ := NewBaseKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb"}, WithVectorStore(vs))
	assert.NoError(t, b.DeleteCollection(context.Background(), "coll"))
}

// TestClose_void变体关闭 对齐 test_close_with_sync_close。
func TestClose_void变体关闭(t *testing.T) {
	vs, im := makeCompatiblePair()
	b, _ := NewBaseKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb"},
		WithVectorStore(vs), WithIndexManager(im))
	b.Close() // 不 panic 即通过（void 桩无状态可断言，fakeAttrStore.Close 空实现）
}

// TestClose_双nil组件 对齐 test_close_with_none_components。
func TestClose_双nil组件(t *testing.T) {
	b, _ := NewBaseKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb"})
	b.Close() // 不 panic
}

// TestClose_错误变体吞并对齐 test_close_with_exception（Warn 吞错误）。
func TestClose_错误变体吞并(t *testing.T) {
	vs, _ := makeCompatiblePair()
	ec := &fakeErrCloserIndexer{closeErr: errors.New("close boom")}
	b, _ := NewBaseKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb"},
		WithVectorStore(vs), WithIndexManager(ec))
	b.Close() // 错误被 Warn 吞，不 panic
	assert.Equal(t, 1, ec.closed)
}
```

- [ ] **Step 2: 运行确认失败**

```bash
go test ./internal/agentcore/retrieval/knowledgebase/ -count=1 2>&1 | tail -5
```
Expected: FAIL（`b.ParseFiles undefined` / `WithKBFileName undefined` 等编译错误）

- [ ] **Step 3: base.go 补全接口 + 模板方法 + KBOption 函数**

```go
// KnowledgeBase 知识库抽象接口（Python ABC：5 抽象 + 4 模板 + Config 访问器）。
//
// 5 抽象方法由 SimpleKnowledgeBase（13.27）/GraphKnowledgeBase（13.28）
// 实现；4 模板方法由 BaseKnowledgeBase 提供默认实现。
type KnowledgeBase interface {
	// AddDocuments 添加文档到知识库（抽象）
	AddDocuments(ctx context.Context, documents []reranker.Document, opts ...KBOption) ([]string, error)
	// Retrieve 检索相关文档（抽象）。config nil = 用默认值。
	Retrieve(ctx context.Context, query string, config *common.RetrievalConfig, opts ...KBOption) ([]common.RetrievalResult, error)
	// DeleteDocuments 删除文档（抽象）
	DeleteDocuments(ctx context.Context, docIDs []string, opts ...KBOption) (bool, error)
	// UpdateDocuments 更新文档（抽象）
	UpdateDocuments(ctx context.Context, documents []reranker.Document, opts ...KBOption) ([]string, error)
	// GetStatistics 获取知识库统计（抽象）
	GetStatistics(ctx context.Context) (map[string]any, error)
	// ParseFiles 解析文件列表为 Document（模板）
	ParseFiles(ctx context.Context, filePaths []string, opts ...KBOption) ([]reranker.Document, error)
	// ParseURLs 解析 URL 列表为 Document（模板）
	ParseURLs(ctx context.Context, urls []string, opts ...KBOption) ([]reranker.Document, error)
	// DeleteCollection 删除集合（模板）
	DeleteCollection(ctx context.Context, collection string) error
	// Close 关闭并释放资源（模板）
	Close()
	// Config 返回知识库配置
	Config() common.KnowledgeBaseConfig
}

// 编译期断言：BaseKnowledgeBase 满足模板方法与 Config 部分。
// （5 抽象方法未实现——完整 KnowledgeBase 断言由 13.27 SimpleKnowledgeBase 承接）
var _ interface {
	ParseFiles(ctx context.Context, filePaths []string, opts ...KBOption) ([]reranker.Document, error)
	ParseURLs(ctx context.Context, urls []string, opts ...KBOption) ([]reranker.Document, error)
	DeleteCollection(ctx context.Context, collection string) error
	Close()
	Config() common.KnowledgeBaseConfig
} = (*BaseKnowledgeBase)(nil)

// KBOption 知识库方法选项。
type KBOption func(*KBOptions)

// NewKBOptions 从可变参数构建 KBOptions。
func NewKBOptions(opts ...KBOption) KBOptions {
	o := KBOptions{}
	for _, opt := range opts {
		opt(&o)
	}
	return o
}

// WithKBFileName 设置 file_name（ParseFiles 专用）。
func WithKBFileName(name string) KBOption {
	return func(o *KBOptions) { o.FileName = name }
}

// WithKBExtra 设置额外参数键值（ParseURLs 透传/抽象方法子类消费）。
func WithKBExtra(key string, value any) KBOption {
	return func(o *KBOptions) {
		if o.Extra == nil {
			o.Extra = make(map[string]any)
		}
		o.Extra[key] = value
	}
}

// ParseFiles 解析文件列表为 Document 列表（模板方法）。
//
// 单文件解析失败 Error 日志后继续（Python 字面）。
// 只透传 file_name（Python parse 只收 file_name/file_id——与 ParseURLs 不对称）。
//
// Python: knowledge_base.py parse_files
func (b *BaseKnowledgeBase) ParseFiles(ctx context.Context, filePaths []string, opts ...KBOption) ([]reranker.Document, error) {
	if b.parser == nil {
		return nil, exception.BuildError(
			exception.StatusRetrievalKbParserNotFound,
			exception.WithMsg("parser is required for parse_files"),
		)
	}
	kbOpts := NewKBOptions(opts...)
	var allDocuments []reranker.Document
	for _, filePath := range filePaths {
		fileName := kbOpts.FileName
		if fileName == "" {
			// Python 字面：kwargs.get("file_name", file_path.split("/")[-1])
			parts := strings.Split(filePath, "/")
			fileName = parts[len(parts)-1]
		}
		fileID := uuid.NewString()
		documents, err := b.parser.Parse(ctx, filePath,
			parser.WithDocID(fileID), parser.WithExtra("file_name", fileName))
		if err != nil {
			logger.Error(logComponent).Str("file_path", filePath).Err(err).Msg("Failed to parse file")
			continue
		}
		allDocuments = append(allDocuments, documents...)
	}
	return allDocuments, nil
}

// ParseURLs 解析 URL 列表为 Document 列表（模板方法）。
//
// supports 不过 → Warn 跳过；单 URL 失败 Error 继续；
// KBOptions.Extra 全量透传 parser（Python **kwargs 直通）。
//
// Python: knowledge_base.py parse_urls
func (b *BaseKnowledgeBase) ParseURLs(ctx context.Context, urls []string, opts ...KBOption) ([]reranker.Document, error) {
	if b.parser == nil {
		return nil, exception.BuildError(
			exception.StatusRetrievalKbParserNotFound,
			exception.WithMsg("parser is required for parse_urls"),
		)
	}
	kbOpts := NewKBOptions(opts...)
	var allDocuments []reranker.Document
	for _, url := range urls {
		if !b.parser.Supports(url) {
			logger.Warn(logComponent).Str("url", url).Msg("Parser does not support URL, skipping")
			continue
		}
		docID := uuid.NewString()
		parseOpts := []parser.ParseOption{parser.WithDocID(docID)}
		for k, v := range kbOpts.Extra {
			parseOpts = append(parseOpts, parser.WithExtra(k, v))
		}
		documents, err := b.parser.Parse(ctx, url, parseOpts...)
		if err != nil {
			logger.Error(logComponent).Str("url", url).Err(err).Msg("Failed to parse URL")
			continue
		}
		allDocuments = append(allDocuments, documents...)
	}
	return allDocuments, nil
}

// DeleteCollection 删除当前数据库中的一个集合（模板方法）。
//
// Python: knowledge_base.py delete_collection
func (b *BaseKnowledgeBase) DeleteCollection(ctx context.Context, collection string) error {
	if b.vectorStore == nil {
		return exception.BuildError(
			exception.StatusRetrievalKbVectorStoreNotFound,
			exception.WithMsg("vector_store is required for delete_collection"),
		)
	}
	return b.vectorStore.DeleteTable(ctx, collection)
}

// Close 关闭知识库并释放资源（模板方法）。
//
// 逐组件关闭（vectorStore → indexManager 顺序字面）；
// void Close 直调；error Close 变体失败 Warn 吞（Python
// _maybe_await 异常 Warn 同构；async 形态编译期消解，偏差 §8-3）。
//
// Python: knowledge_base.py close
func (b *BaseKnowledgeBase) Close() {
	b.maybeClose(b.vectorStore)
	b.maybeClose(b.indexManager)
}

// maybeClose 关闭单个组件（双变体可选接口）。
func (b *BaseKnowledgeBase) maybeClose(obj any) {
	if obj == nil {
		return
	}
	if c, ok := obj.(interface{ Close() }); ok {
		c.Close()
		return
	}
	if c, ok := obj.(interface{ Close() error }); ok {
		if err := c.Close(); err != nil {
			logger.Warn(logComponent).Err(err).Msg("Failed to close object")
		}
	}
}
```

base.go 头部补 import：`"strings"`、`"github.com/google/uuid"`、`exception`、`logger`、常量 `logComponent = logger.ComponentAgentCore`。

**注意 maybeClose 断言顺序**：void 分支在前。fakeErrCloserIndexer 只有 `Close() error`（无 void Close），走第二分支。而 fakeAttrStore 实现 VectorStore 接口（含 void Close）走第一分支。**顺序保证 error 变体不会被 void 断言截获吗**——会！若一个类型只有 Close() error，`obj.(interface{ Close() })` 断言失败（方法签名不匹配），落入第二分支。正确。

**但 BaseKnowledgeBase 基座问题**：`maybeClose(b.vectorStore)` 中 b.vectorStore 静态类型是 VectorStore 接口（方法集已含 void Close）——`obj` 装箱后动态类型决定断言结果。fakeAttrStore 的方法集含 void Close（实现接口必需）→ 走 void 分支 ✓。MilvusVectorStore 有 void Close ✓。fakeErrCloserIndexer 走 indexManager 槽（Indexer 接口无 Close）→ 装箱动态类型只有 error Close → 第二分支 ✓。

- [ ] **Step 4: 运行确认通过**

```bash
go test ./internal/agentcore/retrieval/knowledgebase/ -count=1 2>&1 | tail -3
```
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/retrieval/knowledgebase/
git commit -m "feat(kb): KnowledgeBase 接口 + 4 模板方法（ParseFiles/ParseURLs/DeleteCollection/Close）（13.26）"
```

---

### Task 7: 增强测试补全（Chroma 拦截 / ensureLoader / 语义固化）

**Files:**
- Test: `internal/agentcore/retrieval/knowledgebase/validate_test.go`（追加）

- [ ] **Step 1: 写增强测试（spec §9.3 的 1/2/3/4/7 项）**

validate_test.go 追加：

```go
// ──────────────────────────── 增强测试（Python 未覆盖） ────────────────────────────

// chromaFakeStore 类型名 chroma 前缀（R1 反射探测目标）。不实现 storageAttrProvider——
// 7 属性全 nil 与 fakeAttrIndexer 空值桩匹配通过属性比对，专测 chroma 拦截。
type chromaFakeStore struct {
	fakeAttrStore
}

// fakeEnsureLoaderStore 实现 ensureLoader 的桩（调用计数）。
type fakeEnsureLoaderStore struct {
	fakeAttrStore
	ensureLoadedCalls int
	ensureErr         error
}

func (f *fakeEnsureLoaderStore) EnsureLoaded(ctx context.Context) error {
	f.ensureLoadedCalls++
	return f.ensureErr
}

// TestChroma拦截_非vector索引 对齐 Python chroma 命名约定拦截（Go 增强，
// Python 测试未覆盖此路径）。
func TestChroma拦截_非vector索引(t *testing.T) {
	vs := &chromaFakeStore{}
	im := &fakeAttrIndexer{} // 全空值与 vs 空 storageAttr 匹配
	_, err := NewBaseKnowledgeBase(
		common.KnowledgeBaseConfig{KBID: "kb", IndexType: common.IndexTypeHybrid},
		WithVectorStore(vs), WithIndexManager(im))
	require.Error(t, err)
	var be *exception.BaseError
	require.True(t, errors.As(err, &be))
	assert.Equal(t, 155508, be.Code())
	assert.Contains(t, be.Message(), "Chroma database does not support sparse embedding & hybrid search in local mode yet")
}

// TestChroma拦截_vector索引放行 chroma 命名但 index_type=vector → 放行。
func TestChroma拦截_vector索引放行(t *testing.T) {
	vs := &chromaFakeStore{}
	im := &fakeAttrIndexer{}
	_, err := NewBaseKnowledgeBase(
		common.KnowledgeBaseConfig{KBID: "kb", IndexType: common.IndexTypeVector},
		WithVectorStore(vs), WithIndexManager(im))
	assert.NoError(t, err)
}

// TestChroma拦截_非strict放行 strict=false 时不拦（对齐 Python strict 门控）。
func TestChroma拦截_非strict放行(t *testing.T) {
	vs := &chromaFakeStore{}
	im := &fakeAttrIndexer{}
	_, err := NewBaseKnowledgeBase(
		common.KnowledgeBaseConfig{KBID: "kb", IndexType: common.IndexTypeHybrid},
		WithVectorStore(vs), WithIndexManager(im), WithStrictValidationFalse())
	assert.NoError(t, err)
}

// TestStrict不影响七属性比对 重要语义固化：strict=false 时 7 属性比对仍执行
// （Python strict 只影响 check_vector_field 与 chroma 拦截，Go 增强）。
func TestStrict不影响七属性比对(t *testing.T) {
	vs, im := makeCompatiblePair()
	im.databaseName = "db2"
	_, err := NewBaseKnowledgeBase(
		common.KnowledgeBaseConfig{KBID: "kb", IndexType: common.IndexTypeVector},
		WithVectorStore(vs), WithIndexManager(im), WithStrictValidationFalse())
	require.Error(t, err)
	var be *exception.BaseError
	require.True(t, errors.As(err, &be))
	assert.Equal(t, 155508, be.Code())
}

// TestEnsureLoader_钩子调用 实现者被调、未实现者跳过（Go 增强）。
func TestEnsureLoader_钩子调用(t *testing.T) {
	el := &fakeEnsureLoaderStore{}
	el.databaseName = "test_db"
	el.distanceMetric = "cosine"
	el.textField = "content"
	el.vectorField = any("test_vector_field")
	el.sparseVectorField = "sparse_vector"
	el.metadataField = "metadata"
	el.docIDField = "document_id"
	im := &fakeAttrIndexer{
		databaseName: "test_db", distanceMetric: "cosine", textField: "content",
		vectorField: any("test_vector_field"), sparseVectorField: "sparse_vector",
		metadataField: "metadata", docIDField: "document_id",
	}
	_, err := NewBaseKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb"},
		WithVectorStore(el), WithIndexManager(im))
	require.NoError(t, err)
	assert.Equal(t, 1, el.ensureLoadedCalls)
}

// TestEnsureLoader_错误传播 EnsureLoaded 失败炸构造（对齐 Python 异常传播）。
func TestEnsureLoader_错误传播(t *testing.T) {
	el := &fakeEnsureLoaderStore{ensureErr: errors.New("load failed")}
	el.databaseName = "test_db"
	el.distanceMetric = "cosine"
	el.textField = "content"
	el.vectorField = any("test_vector_field")
	el.sparseVectorField = "sparse_vector"
	el.metadataField = "metadata"
	el.docIDField = "document_id"
	im := &fakeAttrIndexer{
		databaseName: "test_db", distanceMetric: "cosine", textField: "content",
		vectorField: any("test_vector_field"), sparseVectorField: "sparse_vector",
		metadataField: "metadata", docIDField: "document_id",
	}
	_, err := NewBaseKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb"},
		WithVectorStore(el), WithIndexManager(im))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "load failed")
}

// TestEnsureLoader_校验失败不触加载 三段式顺序：①失败时 ③不被调（Go 增强）。
func TestEnsureLoader_校验失败不触加载(t *testing.T) {
	el := &fakeEnsureLoaderStore{}
	// 属性不匹配
	im := &fakeAttrIndexer{databaseName: "other"}
	_, err := NewBaseKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb"},
		WithVectorStore(el), WithIndexManager(im))
	require.Error(t, err)
	assert.Equal(t, 0, el.ensureLoadedCalls)
}

// TestAttrEqual_直测 比较器四态（Go 增强）。
func TestAttrEqual_直测(t *testing.T) {
	assert.True(t, attrEqual(nil, nil))
	assert.False(t, attrEqual(nil, "x"))
	assert.False(t, attrEqual("x", nil))
	assert.True(t, attrEqual("a", "a"))
	assert.False(t, attrEqual("a", "b"))
	// 跨类型对象（Chroma 配置 vs Milvus 配置语义）：不同 struct 类型
	assert.False(t, attrEqual(struct{ A int }{1}, struct{ A int }{1}))
	// 同类型同值
	assert.True(t, attrEqual(struct{ A int }{1}, struct{ A int }{1}))
}
```

- [ ] **Step 2: 运行确认通过（本任务纯测试增补，预期直接 PASS——若有失败暴露实现 bug 回改 Task 4/6）**

```bash
go test ./internal/agentcore/retrieval/knowledgebase/ -count=1 2>&1 | tail -3
```
Expected: PASS

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/retrieval/knowledgebase/
git commit -m "test(kb): Chroma 拦截/ensureLoader/strict 语义/attrEqual 增强（13.26 Python 未覆盖面）"
```

---

### Task 8: 全部 Python 用例映射对账 + gofmt + 全量回归

**Files:**
- Test: 既有测试文件核对（无新文件）

- [ ] **Step 1: Python 23 例对账清单核对**

逐例核对（grep 测试函数名）：

```bash
grep -c "^func Test" internal/agentcore/retrieval/knowledgebase/*_test.go
```

对账表（Python → Go 测试函数）：
- test_init → TestNewBaseKnowledgeBase_默认构造 ✓
- test_init_with_components → TestNewBaseKnowledgeBase_全组件构造 ✓
- test_init_with_mismatch_metrics → TestValidateIndex_七属性逐项不匹配/distance_metric 子例 ✓
- test_init_with_mismatch_names → TestValidateIndex_七属性逐项不匹配/database_name 子例 ✓
- test_close_with_async_close → TestClose_void变体关闭（async 形态编译期消解，偏差 §8-3）✓
- test_close_with_sync_close → TestClose_void变体关闭 ✓
- test_close_with_none_components → TestClose_双nil组件 ✓
- test_close_with_exception → TestClose_错误变体吞并 ✓
- test_validation_passes_when_all_attributes_match → TestValidateIndex_全属性匹配 ✓
- test_validation_skipped_when_vector_store_is_none → TestValidateIndex_vs缺失跳过 ✓
- test_validation_skipped_when_index_manager_is_none → TestValidateIndex_vs缺失跳过 的镜像场景（Task 3 TestNewBaseKnowledgeBase_单组件不触发校验覆盖 im-only 分支）✓
- test_validation_runs_when_setting_vector_store_after_index_manager → TestSetVectorStore_后置触发校验 ✓
- test_validation_runs_when_setting_index_manager_after_vector_store → TestSetIndexManager_后置触发校验 ✓
- test_validation_fails_on_mismatch_database_name → 七属性子例 ✓
- test_validation_fails_on_mismatch_distance_metric → 七属性子例 ✓
- test_validation_fails_on_mismatch_text_field → 七属性子例 ✓
- test_validation_fails_on_mismatch_vector_field → 七属性子例 ✓
- test_validation_fails_on_mismatch_sparse_vector_field → 七属性子例 ✓
- test_validation_fails_on_mismatch_metadata_field → 七属性子例 ✓
- test_validation_fails_on_mismatch_doc_id_field → 七属性子例 ✓
- test_validation_error_message_includes_type_names → TestValidateIndex_错误消息含类型名与值 ✓
- test_validation_passes_when_attributes_are_none → TestValidateIndex_双nil属性通过 ✓
- test_validation_fails_on_first_mismatch → TestValidateIndex_首个不匹配即停 ✓

- [ ] **Step 2: gofmt**

```bash
gofmt -l internal/agentcore/retrieval/knowledgebase/ internal/agentcore/retrieval/vector_store/ internal/agentcore/retrieval/indexing/indexer/
```
Expected: 空输出（有输出则 `gofmt -w` 修正后重跑）

- [ ] **Step 3: -race + 覆盖率**

```bash
go test ./internal/agentcore/retrieval/knowledgebase/ -race -count=1 -cover 2>&1 | tail -3
go test ./internal/agentcore/retrieval/vector_store/ ./internal/agentcore/retrieval/indexing/indexer/ -count=1 -cover 2>&1 | tail -4
```
Expected: PASS，knowledgebase 覆盖率 ≥ 90%

- [ ] **Step 4: 全量回归（CI 同款构建标签）**

```bash
go test -tags "sqlite_fts5 test" ./... -count=1 2>&1 | grep -E 'FAIL|ok .*(retrieval|knowledgebase)' | head -20
```
Expected: 零 FAIL

- [ ] **Step 5: 提交（如有 gofmt 修正）**

```bash
git add -A internal/agentcore/retrieval/
git commit -m "style(kb): gofmt 对齐 + 23 例对账完成（13.26）"
```

（无修正则跳过本步）

---

### Task 9: 文档同步（doc.go 索引 + IMPLEMENTATION_PLAN.md + spec 勘误）

**Files:**
- Modify: `internal/agentcore/retrieval/vector_store/doc.go`（核心类型索引补一行）
- Modify: `internal/agentcore/retrieval/indexing/indexer/doc.go`（同上）
- Modify: `IMPLEMENTATION_PLAN.md`（13.26 行 + 领域13 顶部备注链）

- [ ] **Step 1: vector_store/doc.go 核心类型索引追加**

在"核心类型/接口索引"区块追加：

```
//	（13.26）ChromaVectorStore/MilvusVectorStore 增 6 属性 getter + EnsureLoaded
//	（DatabaseName/TextField/VectorField(any)/SparseVectorField/MetadataField/DocIDField）
//	——knowledgebase 包 validate_index 消费面
```

- [ ] **Step 2: indexer/doc.go 同款追加**

- [ ] **Step 3: IMPLEMENTATION_PLAN.md 更新**

13.26 行（L831）替换为：

```markdown
| 13.26 | ✅ | KnowledgeBase 抽象 | `knowledgebase 新包(base.go+validate.go+doc.go)：KnowledgeBase 接口[5抽象+4模板(ParseFiles只透file_name/ParseURLs Extra全透传不对称字面复刻)+Config访问器] + BaseKnowledgeBase 基座[9组件字段+构造尾部校验+SetVectorStore/SetIndexManager二次赋值setter——Python __setattr__魔法方案B同构] + postAssignValidation三段式[①ValidateIndex 7属性固定序比对+strict CheckVectorField ②isChromaType reflect类型名chroma前缀拦截 ③ensureLoader钩子(MilvusVectorStore.EnsureLoaded回填)] + storageAttrProvider消费接口[7 getter,未实现→nil对齐getattr] + attrEqual[双nil true/单nil false/DeepEqual首步类型比较——Chroma vs Milvus跨源混配天然拦截] + KBOption[FileName/Extra双语义分流] + maybeClose双变体[void直调/error Warn吞] + 错误码155500/155503/155508零消费转已消费 + 回填4类型×6getter(Chroma/Milvus×Store/Indexer,VectorField()any装箱联合类型)+MilvusVectorStore.EnsureLoaded + Python 23例1:1映射+Go增强12项(Chroma拦截路径/strict不影响7属性比对语义固化/ensureLoader计数/三段式顺序/attrEqual直测/KBOptions双语义/模板方法补全[Python基类测试未测]等) + 偏差9项归档spec§8(llmClient收紧BaseModelClient/值渲染%v/close async消解/ctx首普/触发点收窄/WithExtra逐键展开/split字面/uuid直译/storageAttrProvider整接口粒度)`。设计：docs/superpowers/specs/2026-10-29-knowledgebase-13.26-design.md | `openjiuwen/core/retrieval/knowledge_base.py` |
```

领域13 顶部备注链（L796 后）追加：

```markdown
> 2026-10-29：13.26 KnowledgeBase 抽象完成（编排层顶点：5抽象+4模板+配置一致性防线[__setattr__→构造尾部+双setter方案B] + 7属性attrEqual[DeepEqual跨源混配拦截] + Chroma类型名反射拦截 + ensureLoader钩子；错误码 155500/155503/155508 转已消费；回填 4 类型×6 getter）。下一节 13.27 SimpleKnowledgeBase。设计：docs/superpowers/specs/2026-10-29-knowledgebase-13.26-design.md
```

- [ ] **Step 4: spec 偏差表对照（如有新增偏差补充归档；实施中确认的偏差如与预归档不符则修正）**

- [ ] **Step 5: 提交**

```bash
git add IMPLEMENTATION_PLAN.md internal/agentcore/retrieval/vector_store/doc.go internal/agentcore/retrieval/indexing/indexer/doc.go docs/superpowers/specs/2026-10-29-knowledgebase-13.26-design.md
git commit -m "docs(kb): 13.26 完成——PLAN 状态同步 + doc.go 索引 + spec 勘误"
```

---

## 自审记录

1. **Spec 覆盖**：§1 包结构（Task 3 doc.go+base.go、Task 4 validate.go）✓；§2 接口/基座/构造/setter/KBOptions/访问器（Task 3/5/6）✓；§3 三段式+attrEqual+isChromaType+ensureLoader+错误消息（Task 4/7）✓；§4 四模板方法（Task 6）✓；§5 回填清单（Task 1/2/9）✓；§6 错误码三码（Task 4/6）✓；§7 日志四条（Task 6，logger 调用在 ParseFiles/ParseURLs/maybeClose 内）✓；§8 偏差 9 项（各任务注释+Task 9 对照）✓；§9 测试 23 例映射（Task 3/4/5/6/7/8 对账）✓
2. **占位符扫描**：Task 3 Step 5 的 postAssignValidation 占位是 TDD 中间态（Task 4 立即替换），非交付占位符 ✓
3. **类型一致性**：storageAttrProvider 7 方法签名在 Task 1/2 getter 与 Task 4 接口定义一致（VectorField() any）✓；fakeAttrStore/fakeAttrIndexer 桩在 Task 3 定义、Task 4/5/7 复用同名 ✓；KBOptions 字段名 FileName/Extra 与 WithKBFileName/WithKBExtra 一致 ✓

## 已知风险提示（实施时注意）

1. **Task 6 的 fakeKBParser.gotOpts 类型**：✅ 已核实 `NewParseOptions` 返回 `*ParseOptions`（parser/base.go L95），桩字段用 `[]*parser.ParseOptions`，测试断言 `p.gotOpts[0].Extra`——上方 Task 6 代码已修正，无需再查
2. **Task 1 Step 1 MilvusVectorStore 的 mu 字段**：✅ 已核实存在（milvus.go L117-118 `mu sync.RWMutex`），测试 `s.mu.Lock()` 可直接使用
3. **ensureLoader 断言时机**：postAssignValidation 中 ensureLoaded 在 Chroma 拦截**之后**——若未来 Python 行为变化需同步
4. **verifyMode 等密封类型不涉及本节**：无
5. **测试断言 `p.gotOpts[0].Extra`**：✅ 已核实 `ParseOptions.Extra` 为 `map[string]any`（parser/base.go L58-59），断言 `map[string]any{"file_name": "file.txt"}` 成立，无需再查
