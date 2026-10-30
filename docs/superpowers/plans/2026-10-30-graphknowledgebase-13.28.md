# 13.28 GraphKnowledgeBase 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现领域十三"查"侧图增强知识库 GraphKnowledgeBase：双索引写入（chunk + triple）、图路径检索（GraphRetriever 懒创建 + index_type 注入 + agentic 包装）、非图 fallback 复用 SimpleKB，收口检索查询管线。

**Architecture:** knowledgebase 包新增 `graph_kb.go`（子类，值嵌入 13.26 基座）与 `kb_helpers.go`（把 13.27 的四个私有函数提为包级复用）。前置 Task 0 把 `Close()` 统一为 `Close() error`，消除 13.26 的双变体鸭子探测。三元组 chunk 的 `metadata["doc_id"/"chunk_id"]` 是 13.23 graph_fetch 读侧的契约，写入侧必须逐键对齐。

**Tech Stack:** Go 1.x / testify / 标准库 `encoding/json`、`sync`、`github.com/google/uuid`

**设计文档:** `docs/superpowers/specs/2026-10-30-graphknowledgebase-13.28-design.md`

**Python 对齐源:**
- 实现：`/home/opensource/agent-core/openjiuwen/core/retrieval/graph_knowledge_base.py`（371 行）
- 测试：`/home/opensource/agent-core/tests/unit_tests/core/retrieval/test_graph_knowledge_base.py`（12 例）

**编译/测试纪律（全计划适用）:**
- 单测：`cd /home/opensource/uapclaw-gateway && CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/agentcore/retrieval/<包>/... -count=1`
- 全量回归：`cd /home/opensource/uapclaw-gateway && CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./... -count=1`
- 编译前 `pgrep -f 'go (build|test)'` 检查残留；有多个先 kill（规则 5）
- 提交用显式 `git add <文件>`，**禁止 `-am`**（工作区可能有其它任务的改动，见 feedback-git-am-stowaway-commits）
- 源码声明顺序：结构体 → 枚举 → 常量 → 全局变量 → 导出函数 → 非导出函数；接口归结构体区块且排在前（go-code-conventions 规范 2）
- 注释中文；日志 Msg 英文（feedback-logs-english）
- 错误构造：`exception.BuildError(exception.StatusXxx, exception.WithMsg("..."))`

---

## 关键既有符号速查（实施前必读）

```
// knowledgebase 包
type KnowledgeBase interface {
    AddDocuments(ctx context.Context, documents []reranker.Document, opts ...KBOption) ([]string, error)
    Retrieve(ctx context.Context, query string, config *common.RetrievalConfig, opts ...KBOption) ([]common.RetrievalResult, error)
    DeleteDocuments(ctx context.Context, docIDs []string, opts ...KBOption) (bool, error)
    UpdateDocuments(ctx context.Context, documents []reranker.Document, opts ...KBOption) ([]string, error)
    GetStatistics(ctx context.Context) (map[string]any, error)
    ParseFiles/ParseURLs (模板) / DeleteCollection (模板) / Close() error (Task 0 后) / Config() common.KnowledgeBaseConfig
}
func NewBaseKnowledgeBase(config common.KnowledgeBaseConfig, opts ...KnowledgeBaseOption) (*BaseKnowledgeBase, error)
func (b *BaseKnowledgeBase) maybeClose(obj any)      // 非导出，Task 0 后单变体 + Warn 吞
// 基座非导出字段（同包直访）：strictValidation / config / vectorStore / embedModel /
//   parser / chunker / extractor / indexManager / llmClient
// 选项：WithVectorStore / WithEmbedModel / WithParser / WithChunker / WithExtractor /
//       WithIndexManager / WithLLMClient / WithStrictValidationFalse
// KBOptions：NewKBOptions(opts...) → {FileName string; Extra map[string]any}；WithKBExtra(k,v)

// retriever 包
func NewGraphRetriever(opts ...GraphRetrieverOption) *GraphRetriever
func WithGraphChunkRetriever(r Retriever) GraphRetrieverOption
func WithGraphTripleRetriever(r Retriever) GraphRetrieverOption
func WithGraphVectorStore(s vector_store.VectorStore) GraphRetrieverOption
func WithGraphEmbedModel(m embedding.BaseEmbedding) GraphRetrieverOption
func WithGraphChunkCollection(name string) GraphRetrieverOption
func WithGraphTripleCollection(name string) GraphRetrieverOption
type IndexTypeCarrier interface { IndexType() string; SetIndexType(string) }  // *GraphRetriever 实现
func WithRetrieveFilters(map[string]any) RetrieveOption
func WithRetrieveGraphExpansion(bool) RetrieveOption
func NewAgenticRetriever(r Retriever, llm model_clients.BaseModelClient, opts ...AgenticRetrieverOption) (*AgenticRetriever, error)

// common 包
type KnowledgeBaseConfig struct { KBID string; IndexType IndexTypeKind; UseGraph bool; ChunkSize int; ChunkOverlap int; UseCaptionForImages bool }
type RetrievalConfig struct { TopK int; ScoreThreshold *float64; UseGraph *bool; Agentic bool; GraphExpansion bool; Filters map[string]any }
type IndexConfig struct { IndexName string; IndexType IndexTypeKind; UseCaptionForImages bool }
type TextChunk struct { ID string; Text string; DocID string; Metadata map[string]any; Embedding []float64 }
type Triple struct { Subject, Predicate, Object string; Metadata map[string]any }
type RetrievalResult struct { Text string; Score float64; Metadata map[string]any; DocID string; ChunkID string }

// indexer / extractor 包
func WithIndexExtra(key string, value any) IndexOption
Indexer.BuildIndex(ctx, chunks []common.TextChunk, config common.IndexConfig, embedModel embedding.BaseEmbedding, opts ...IndexOption) (bool, error)
Indexer.DeleteIndex(ctx, docID string, indexName string) (bool, error)
Indexer.GetIndexInfo(ctx, indexName string) (map[string]any, error)
Extractor.Extract(ctx, chunks []common.TextChunk, opts ...ExtractOption) ([]common.Triple, error)

// 错误码（155505/155506 本节首次消费）
StatusRetrievalKbChunkerNotFound(155501) / StatusRetrievalKbIndexManagerNotFound(155502) /
StatusRetrievalKbVectorStoreNotFound(155503) / StatusRetrievalKbIndexBuildExecutionError(155504) /
StatusRetrievalKbChunkIndexBuildExecutionError(155505) / StatusRetrievalKbTripleIndexBuildExecutionError(155506)
```

**既有测试桩可复用（同包，`knowledgebase` 包测试文件内）：**
`fakeAttrStore` / `fakeAttrIndexer` / `makeCompatiblePair()` / `makeCompatibleRecordingIndexer()` / `recordingIndexer`（只记最后一次 BuildIndex）/ `countingChunker` / `recordingRetriever`（记录 calls/gotTopK/gotMode/gotOpts/closed）/ `statsIndexer` / `stubKBLlm`。

**已知偏差速查（写码时勿"顺手修正"）：**
1. chunk 索引 `UseCaptionForImages` 取配置值；**triple 索引硬编码 `true`**
2. triple chunk 的 `metadata` 先展开 triple.metadata 再覆写 `triple`/`chunk_index`/`chunk_id` 三键
3. `metadata["triple"]` 用 `json.Marshal`（紧凑无空格），**不是** Python `json.dumps` 的 `", "` 形态（§8-2 用户拍板）
4. `doc_id`/`chunk_id` 非 string 时收窄空串（pydantic 会抛，Go 收窄，§8-3）
5. fallback 构造 SimpleKB **不传 extractor**、**不传 strict_validation**
6. `Close` 重复关闭字面复刻（graph 已关 chunk/triple，KB 再关一遍，§6.1）
7. 图路径非 agentic 时 `KBOptions.Extra` 整体丢弃（只有 `AgenticRetriever(**kwargs)` 收）

---

### Task 0: Close 签名统一为 `Close() error`（前置，独立 commit）

**Files:**
- Modify: `internal/agentcore/retrieval/retriever/base.go:33`
- Modify: `internal/agentcore/retrieval/vector_store/base.go:55`
- Modify: `internal/agentcore/retrieval/knowledgebase/base.go:45,52-58,318-337`
- Modify: `internal/agentcore/retrieval/retriever/{vector_retriever.go:166,sparse_retriever.go:115,hybrid_retriever.go:265,graph_retriever_impl.go:413,agentic_retriever.go:290}`
- Modify: `internal/agentcore/retrieval/vector_store/{chroma.go:734,milvus.go:669}`
- Modify: `internal/agentcore/retrieval/indexing/indexer/{chroma.go:189,milvus.go:324}`
- Modify（测试桩）: `retriever/{base_test.go:156,graph_retriever_impl_test.go:56,agentic_retriever_test.go:132,graph_beam_search_test.go:100}`、`knowledgebase/{base_test.go:55,simple_kb_test.go:190,219}`

- [ ] **Step 0.1: 改三个接口与基座模板方法**

`retriever/base.go` L33 接口方法：

```go
	// Close 关闭检索器并释放底层资源。
	Close() error
```

`vector_store/base.go` L55：

```go
	// Close 关闭存储并释放底层资源。
	Close() error
```

`knowledgebase/base.go` L45 接口方法：

```go
	// Close 关闭并释放资源（模板）
	Close() error
```

同文件 L52-58 编译期断言内的 `Close()` 改 `Close() error`：

```go
var _ interface {
	ParseFiles(ctx context.Context, filePaths []string, opts ...KBOption) ([]reranker.Document, error)
	ParseURLs(ctx context.Context, urls []string, opts ...KBOption) ([]reranker.Document, error)
	DeleteCollection(ctx context.Context, collection string) error
	Close() error
	Config() common.KnowledgeBaseConfig
} = (*BaseKnowledgeBase)(nil)
```

同文件 L311-337 的 `Close` / `maybeClose` 换成：

```go
// Close 关闭知识库并释放资源（模板方法）。
//
// 逐组件关闭（vectorStore → indexManager 顺序字面）；组件 Close 返回错误时
// Warn 吞（对齐 Python _maybe_await 的 sync 分支 try/except warn）。
//
// Python: knowledge_base.py close
func (b *BaseKnowledgeBase) Close() error {
	b.maybeClose(b.vectorStore)
	b.maybeClose(b.indexManager)
	return nil
}

// maybeClose 关闭单个组件（统一 error 变体，Task 0 单一化）。
func (b *BaseKnowledgeBase) maybeClose(obj any) {
	if obj == nil {
		return
	}
	c, ok := obj.(interface{ Close() error })
	if !ok {
		return
	}
	if err := c.Close(); err != nil {
		logger.Warn(logComponent).Err(err).Msg("Failed to close object")
	}
}
```

- [ ] **Step 0.2: 改 5 个 retriever 实现**

`vector_retriever.go` / `sparse_retriever.go` / `hybrid_retriever.go` 三处（结构相同，仅接收者名不同）：

```go
// Close 关闭检索器（转关底层向量存储）。
func (r *VectorRetriever) Close() error {
	if r.vectorStore != nil {
		_ = r.vectorStore.Close()
	}
	return nil
}
```

`graph_retriever_impl.go` L413-420（委托型，返回首个错误）：

```go
// Close 关闭检索器（关闭固定 chunk / triple 检索器）。
func (g *GraphRetriever) Close() error {
	var firstErr error
	if g.chunkRetriever != nil {
		if err := g.chunkRetriever.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if g.tripleRetriever != nil {
		if err := g.tripleRetriever.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
```

`agentic_retriever.go` L290-292：

```go
// Close 关闭检索器（转关内层检索器）。
func (a *AgenticRetriever) Close() error {
	if a.retriever == nil {
		return nil
	}
	return a.retriever.Close()
}
```

- [ ] **Step 0.3: 改 4 个叶子实现（vector_store 2 + indexer 2）**

`vector_store/chroma.go`：

```go
// Close 关闭向量存储客户端。
func (s *ChromaVectorStore) Close() error {
	if s.client != nil {
		_ = s.client.Close()
	}
	return nil
}
```

`vector_store/milvus.go`：

```go
// Close 关闭向量存储（不释放集合）。
func (s *MilvusVectorStore) Close() error {
	s.CloseWithRelease(false)
	return nil
}
```

`indexing/indexer/chroma.go`（保留内部 Warn，返回 nil）：

```go
// Close 关闭索引管理器客户端。
//
// 内部已 Warn 记录关闭失败（对齐 Python logger.warning），故返回 nil。
func (ci *ChromaIndexer) Close() error {
	if ci.client != nil {
		if err := ci.client.Close(); err != nil {
			logger.Warn(logComponent).
				Str("event_type", "INDEXING_WARN").
				Err(err).
				Msg("failed to close ChromaDB client")
		}
	}
	return nil
}
```

`indexing/indexer/milvus.go`：

```go
// Close 关闭索引管理器客户端（内部 Warn 已记录，返回 nil）。
func (mi *MilvusIndexer) Close() error {
	if mi.client == nil {
		return nil
	}
	if err := mi.client.Close(context.Background()); err != nil {
		logger.Warn(logComponent).Err(err).Msg("failed to close Milvus client")
	}
	return nil
}
```

> **必须改这两个 Indexer**：`indexer.Indexer` 接口没有 Close，`maybeClose(indexManager)` 全靠动态类型断言；若漏改，统一后断言落空 → 索引管理器不再被关闭（真实回归，spec §13.3）。

- [ ] **Step 0.4: 改 7 处测试桩**

```go
// retriever/base_test.go (fakeVectorStore)
func (f *fakeVectorStore) Close() error { return nil }

// retriever/graph_retriever_impl_test.go (closeCountRetriever)
func (c *closeCountRetriever) Close() error { c.closed++; return nil }

// retriever/agentic_retriever_test.go (agenticFakeRetriever)
func (f *agenticFakeRetriever) Close() error { return nil }

// retriever/graph_beam_search_test.go (fakeBeamRetriever)
func (f *fakeBeamRetriever) Close() error { return nil }

// knowledgebase/base_test.go (fakeAttrStore)
func (f *fakeAttrStore) Close() error { return nil }

// knowledgebase/simple_kb_test.go (plainVectorStore)
func (p *plainVectorStore) Close() error { return nil }

// knowledgebase/simple_kb_test.go (recordingRetriever)
func (r *recordingRetriever) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed++
	return nil
}
```

`knowledgebase/base_test.go` 的 `fakeErrCloserIndexer.Close() error` 已是目标形态，**不动**。

- [ ] **Step 0.5: 编译 + 找漏网桩**

Run: `cd /home/opensource/uapclaw-gateway && go build ./... && go vet ./internal/agentcore/retrieval/...`
Expected: 编译错误逐个指出未同步的类型；按报错补齐（多半是其它测试文件里的 `Close() {}` 桩），直到通过。

- [ ] **Step 0.6: 跑三个包单测（既有断言一字不改）**

Run: `cd /home/opensource/uapclaw-gateway && CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/agentcore/retrieval/retriever/... ./internal/agentcore/retrieval/vector_store/... ./internal/agentcore/retrieval/indexing/... ./internal/agentcore/retrieval/knowledgebase/... -count=1`
Expected: 全 PASS。任何断言需要修改即说明不是"行为零变化"，停下来查。

> `knowledgebase/base_test.go` 的 `TestClose_void变体关闭` 名字里的 "void" 已过时，可顺带改名为 `TestClose_组件关闭`（**只改名，断言不动**）；`TestClose_错误变体吞并` 保持不变——它就是"组件错误被 Warn 吞"的既有承载者。

- [ ] **Step 0.7: 提交**

```bash
cd /home/opensource/uapclaw-gateway
git add internal/agentcore/retrieval/retriever/base.go internal/agentcore/retrieval/vector_store/base.go \
        internal/agentcore/retrieval/knowledgebase/base.go \
        internal/agentcore/retrieval/retriever/vector_retriever.go internal/agentcore/retrieval/retriever/sparse_retriever.go \
        internal/agentcore/retrieval/retriever/hybrid_retriever.go internal/agentcore/retrieval/retriever/graph_retriever_impl.go \
        internal/agentcore/retrieval/retriever/agentic_retriever.go \
        internal/agentcore/retrieval/vector_store/chroma.go internal/agentcore/retrieval/vector_store/milvus.go \
        internal/agentcore/retrieval/indexing/indexer/chroma.go internal/agentcore/retrieval/indexing/indexer/milvus.go \
        internal/agentcore/retrieval/retriever/base_test.go internal/agentcore/retrieval/retriever/graph_retriever_impl_test.go \
        internal/agentcore/retrieval/retriever/agentic_retriever_test.go internal/agentcore/retrieval/retriever/graph_beam_search_test.go \
        internal/agentcore/retrieval/knowledgebase/base_test.go internal/agentcore/retrieval/knowledgebase/simple_kb_test.go
git commit -m "refactor(retrieval): Close 签名统一为 Close() error（13.28 Task 0，行为零变化）"
```

---

### Task 1: kb_helpers.go 抽取四个包级函数

**Files:**
- Create: `internal/agentcore/retrieval/knowledgebase/kb_helpers.go`
- Modify: `internal/agentcore/retrieval/knowledgebase/simple_kb.go`（删 4 个私有函数/方法，改调用点）

- [ ] **Step 1.1: 建立基线（改动前先跑一遍）**

Run: `cd /home/opensource/uapclaw-gateway && CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/agentcore/retrieval/knowledgebase/... -count=1`
Expected: 全 PASS（这就是 Task 1 的回归网，改完后必须仍然全 PASS 且断言不改）。

- [ ] **Step 1.2: 新建 kb_helpers.go**

```go
package knowledgebase

import (
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/retriever"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/vector_store"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// defaultRetrievalConfig 默认检索配置（对齐 Python RetrievalConfig() 默认
// top_k=5，其余字段为零值/nil）。
func defaultRetrievalConfig() *common.RetrievalConfig {
	return &common.RetrievalConfig{TopK: 5}
}

// resolveRetrievalMode 由 index_type 推导检索模式。
//
// Python: mode = "hybrid"; index_type=="vector"→"vector"；
// index_type=="bm25"→"sparse"；其余保持 "hybrid"。
func resolveRetrievalMode(indexType common.IndexTypeKind) retriever.RetrievalMode {
	switch indexType {
	case common.IndexTypeVector:
		return retriever.RetrievalModeVector
	case common.IndexTypeBM25:
		return retriever.RetrievalModeSparse
	default:
		return retriever.RetrievalModeHybrid
	}
}

// vectorStoreDatabaseName 防御式取 vector_store 的 database_name。
//
// Python: getattr(getattr(self.vector_store, "config", None), "database_name", "")
// 且非 str 归零。Go 用接口断言表达"属性存在性"，未实现即回落空串。
func vectorStoreDatabaseName(vs vector_store.VectorStore) string {
	if vs == nil {
		return ""
	}
	if p, ok := vs.(interface{ DatabaseName() string }); ok {
		return p.DatabaseName()
	}
	return ""
}

// wrapAgenticRetriever 用 AgenticRetriever 包装底层 retriever。
//
// 对齐 Python `AgenticRetriever(retriever=..., llm_client=..., **kwargs)`：
// kwargs 只认 max_iter，经 KBOptions.Extra["max_iter"] 映射；其余键 Python
// 会在构造前抛 TypeError 让整条链路失败，Go 同样报 155510（不静默忽略）。
// max_iter 类型不符时 Python isinstance 判假 → Warn 回落默认 2。
func wrapAgenticRetriever(llm model_clients.BaseModelClient, r retriever.Retriever,
	opts ...KBOption) (retriever.Retriever, error) {
	kbOpts := NewKBOptions(opts...)
	var agenticOpts []retriever.AgenticRetrieverOption
	for key, v := range kbOpts.Extra {
		switch key {
		case "max_iter":
			n, ok := v.(int)
			if !ok {
				logger.Warn(logComponent).
					Str("max_iter_type", fmt.Sprintf("%T", v)).
					Msg("max_iter is not an int, falling back to default")
				continue
			}
			agenticOpts = append(agenticOpts, retriever.WithAgenticMaxIter(n))
		default:
			return nil, exception.BuildError(
				exception.StatusAgenticKwargsUnexpected,
				exception.WithMsg(fmt.Sprintf(
					"AgenticRetriever() got an unexpected keyword argument '%s'", key)),
			)
		}
	}
	return retriever.NewAgenticRetriever(r, llm, agenticOpts...)
}
```

- [ ] **Step 1.3: simple_kb.go 删除旧实现、改调用点**

删除：`defaultRetrievalConfig`、`resolveRetrievalMode`、`vectorStoreDatabaseName` 三个函数（原 L289-308、L383-396）与 `wrapAgentic` 方法（原 L344-381）。

调用点替换：

```go
// Retrieve 内（原 s.wrapAgentic(ctx, r, opts...)）
	if retrievalConfig.Agentic {
		r, err = wrapAgenticRetriever(s.llmClient, r, opts...)
		if err != nil {
			return nil, err
		}
	}

// AddDocuments 内（原 s.vectorStoreDatabaseName()）
	success, err := s.indexManager.BuildIndex(ctx, chunks, indexConfig, s.embedModel,
		indexer.WithIndexExtra("database_name", vectorStoreDatabaseName(s.vectorStore)))
```

同时删掉因此不再使用的 import（`fmt`、`logger` 若仅被删掉的代码使用；以 `goimports`/编译报错为准）。

- [ ] **Step 1.4: 跑单测确认行为零变化**

Run: `cd /home/opensource/uapclaw-gateway && CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/agentcore/retrieval/knowledgebase/... -count=1`
Expected: 全 PASS，且**没有任何断言被改动**（`git diff --stat` 只应显示 simple_kb.go 与新增 kb_helpers.go）。

- [ ] **Step 1.5: 提交**

```bash
cd /home/opensource/uapclaw-gateway
git add internal/agentcore/retrieval/knowledgebase/kb_helpers.go internal/agentcore/retrieval/knowledgebase/simple_kb.go
git commit -m "refactor(knowledgebase): 四函数下沉 kb_helpers.go 供 GraphKB 复用（13.28 Task 1）"
```

---

### Task 2: GraphKnowledgeBase 类型 + AddDocuments 双索引写入

**Files:**
- Create: `internal/agentcore/retrieval/knowledgebase/graph_kb.go`
- Test: `internal/agentcore/retrieval/knowledgebase/graph_kb_test.go`

- [ ] **Step 2.1: 写失败测试（构造 + AddDocuments 共 8 例）**

```go
package knowledgebase

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/embedding"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/reranker"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/indexing/indexer"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/indexing/processor/extractor"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/indexing/processor/parser"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/retriever"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 测试桩 ────────────────────────────

// graphBuildCall 一次 BuildIndex 调用快照。
type graphBuildCall struct {
	cfg    common.IndexConfig
	chunks []common.TextChunk
}

// graphIndexer 多次构建捕获 + 可编程返回值（区分 chunk / triple 两次构建）。
type graphIndexer struct {
	fakeAttrIndexer
	mu       sync.Mutex
	calls    []graphBuildCall
	buildOK  bool
	buildErr error
	deleteOK bool
	delCalls []string
	info     map[string]any
}

func (g *graphIndexer) BuildIndex(_ context.Context, chunks []common.TextChunk, config common.IndexConfig, _ embedding.BaseEmbedding, _ ...indexer.IndexOption) (bool, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.calls = append(g.calls, graphBuildCall{cfg: config, chunks: chunks})
	return g.buildOK, g.buildErr
}
func (g *graphIndexer) DeleteIndex(_ context.Context, docID string, _ string) (bool, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.delCalls = append(g.delCalls, docID)
	return g.deleteOK, nil
}
func (g *graphIndexer) GetIndexInfo(_ context.Context, _ string) (map[string]any, error) {
	return g.info, nil
}

// stubExtractor 三元组抽取桩（可编程返回）。
type stubExtractor struct {
	triples []common.Triple
	err     error
	calls   int
	gotDocs []common.TextChunk
}

func (s *stubExtractor) Extract(_ context.Context, chunks []common.TextChunk, _ ...extractor.ExtractOption) ([]common.Triple, error) {
	s.calls++
	s.gotDocs = chunks
	return s.triples, s.err
}
func (s *stubExtractor) Process(ctx context.Context, chunks []common.TextChunk, opts ...extractor.ExtractOption) ([]common.Triple, error) {
	return s.Extract(ctx, chunks, opts...)
}

// graphFakeRetriever 图检索器桩（捕获 Retrieve 参数 + Close 计数）。
type graphFakeRetriever struct {
	results []common.RetrievalResult
	err     error
	calls   int
	gotTopK int
	gotMode retriever.RetrievalMode
	gotOpts retriever.RetrieveOptions
	closed  int
}

func (f *graphFakeRetriever) Retrieve(_ context.Context, _ string, topK int, _ *float64,
	mode retriever.RetrievalMode, opts ...retriever.RetrieveOption) ([]common.RetrievalResult, error) {
	f.calls++
	f.gotTopK = topK
	f.gotMode = mode
	f.gotOpts = retriever.NewRetrieveOptions(opts...)
	return f.results, f.err
}
func (f *graphFakeRetriever) BatchRetrieve(context.Context, []string, int, ...retriever.RetrieveOption) ([][]common.RetrievalResult, error) {
	return nil, nil
}
func (f *graphFakeRetriever) RetrieveSearchResults(context.Context, string, int, retriever.RetrievalMode, ...retriever.RetrieveOption) ([]common.SearchResult, error) {
	return nil, nil
}
func (f *graphFakeRetriever) Close() error { f.closed++; return nil }

// ──────────────────────────── 构造 ────────────────────────────

// TestNewGraphKnowledgeBase_构造 基座构造与校验继承 + 编译期断言可用。
func TestNewGraphKnowledgeBase_构造(t *testing.T) {
	vs, im := makeCompatiblePair()
	kb, err := NewGraphKnowledgeBase(common.KnowledgeBaseConfig{KBID: "test_kb", UseGraph: true},
		nil, nil, WithVectorStore(vs), WithIndexManager(im))
	require.NoError(t, err)
	assert.Equal(t, "test_kb", kb.Config().KBID)
	assert.Nil(t, kb.graphRetriever)
	assert.Nil(t, kb.chunkRetriever)
	assert.Nil(t, kb.tripleRetriever)
}

// ──────────────────────────── AddDocuments ────────────────────────────

// TestGraphKB_AddDocuments_图索引 对齐 test_add_documents_with_graph：
// 建 chunk + triple 两个索引，抽一次三元组，triple 索引 use_caption 硬编码 true。
func TestGraphKB_AddDocuments_图索引(t *testing.T) {
	im := &graphIndexer{buildOK: true}
	ch := &countingChunker{chunks: []common.TextChunk{
		{ID: "chunk_1", Text: "Test chunk 1", DocID: "doc_1"},
	}}
	ex := &stubExtractor{triples: []common.Triple{{
		Subject: "Alice", Predicate: "knows", Object: "Bob",
		Metadata: map[string]any{"doc_id": "doc_1", "chunk_id": "chunk_1"},
	}}}
	kb, err := NewGraphKnowledgeBase(
		common.KnowledgeBaseConfig{KBID: "test_kb", IndexType: common.IndexTypeVector,
			UseGraph: true, UseCaptionForImages: false},
		nil, nil, WithChunker(ch), WithExtractor(ex), WithIndexManager(im))
	require.NoError(t, err)

	ids, err := kb.AddDocuments(context.Background(), []reranker.Document{{ID: "doc_1", Text: "Test document"}})
	require.NoError(t, err)
	assert.Equal(t, []string{"doc_1"}, ids)
	assert.Equal(t, 1, ex.calls)
	require.Len(t, im.calls, 2)

	// chunk 索引：名称 / index_type / use_caption 取配置
	assert.Equal(t, "kb_test_kb_chunks", im.calls[0].cfg.IndexName)
	assert.Equal(t, common.IndexTypeVector, im.calls[0].cfg.IndexType)
	assert.False(t, im.calls[0].cfg.UseCaptionForImages)

	// triple 索引：名称 / index_type / use_caption 硬编码 true（不对称）
	assert.Equal(t, "kb_test_kb_triples", im.calls[1].cfg.IndexName)
	assert.Equal(t, common.IndexTypeVector, im.calls[1].cfg.IndexType)
	assert.True(t, im.calls[1].cfg.UseCaptionForImages)

	// triple chunk 字段逐项
	require.Len(t, im.calls[1].chunks, 1)
	tc := im.calls[1].chunks[0]
	assert.NotEmpty(t, tc.ID)
	assert.Equal(t, "Alice knows Bob", tc.Text)
	assert.Equal(t, "doc_1", tc.DocID)
	assert.Equal(t, 0, tc.Metadata["chunk_index"])
	assert.Equal(t, "chunk_1", tc.Metadata["chunk_id"])
	assert.Equal(t, "doc_1", tc.Metadata["doc_id"])
	tripleStr, ok := tc.Metadata["triple"].(string)
	require.True(t, ok)
	var parsed []string
	require.NoError(t, json.Unmarshal([]byte(tripleStr), &parsed))
	assert.Equal(t, []string{"Alice", "knows", "Bob"}, parsed)
}

// TestGraphKB_AddDocuments_非图 对齐 test_add_documents_without_graph：仅建 chunk 索引。
func TestGraphKB_AddDocuments_非图(t *testing.T) {
	im := &graphIndexer{buildOK: true}
	ch := &countingChunker{chunks: []common.TextChunk{{ID: "c1", Text: "c1", DocID: "doc_1"}}}
	ex := &stubExtractor{}
	kb, err := NewGraphKnowledgeBase(common.KnowledgeBaseConfig{KBID: "test_kb", UseGraph: false},
		nil, nil, WithChunker(ch), WithExtractor(ex), WithIndexManager(im))
	require.NoError(t, err)
	_, err = kb.AddDocuments(context.Background(), []reranker.Document{{ID: "doc_1", Text: "d"}})
	require.NoError(t, err)
	assert.Len(t, im.calls, 1)
	assert.Equal(t, 0, ex.calls)
}

// TestGraphKB_AddDocuments_空三元组 抽取结果为空时不建 triple 索引。
func TestGraphKB_AddDocuments_空三元组(t *testing.T) {
	im := &graphIndexer{buildOK: true}
	ch := &countingChunker{chunks: []common.TextChunk{{ID: "c1", Text: "c1", DocID: "doc_1"}}}
	kb, err := NewGraphKnowledgeBase(common.KnowledgeBaseConfig{KBID: "test_kb", UseGraph: true},
		nil, nil, WithChunker(ch), WithExtractor(&stubExtractor{}), WithIndexManager(im))
	require.NoError(t, err)
	_, err = kb.AddDocuments(context.Background(), []reranker.Document{{ID: "doc_1", Text: "d"}})
	require.NoError(t, err)
	assert.Len(t, im.calls, 1)
}

// TestGraphKB_AddDocuments_chunk索引失败 chunk 构建返回 false → 155505。
func TestGraphKB_AddDocuments_chunk索引失败(t *testing.T) {
	im := &graphIndexer{buildOK: false}
	kb, err := NewGraphKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb", UseGraph: true},
		nil, nil, WithChunker(&countingChunker{}), WithIndexManager(im))
	require.NoError(t, err)
	_, err = kb.AddDocuments(context.Background(), []reranker.Document{{ID: "d", Text: "d"}})
	var be *exception.BaseError
	require.True(t, errors.As(err, &be))
	assert.Equal(t, 155505, be.Code())
	assert.Contains(t, be.Message(), "Failed to build chunk index")
}

// TestGraphKB_AddDocuments_triple索引失败 triple 构建返回 false → 155506。
func TestGraphKB_AddDocuments_triple索引失败(t *testing.T) {
	im := &failSecondBuildIndexer{graphIndexer: graphIndexer{buildOK: true}}
	ch := &countingChunker{chunks: []common.TextChunk{{ID: "c1", Text: "c1", DocID: "d"}}}
	ex := &stubExtractor{triples: []common.Triple{{Subject: "a", Predicate: "b", Object: "c"}}}
	kb, err := NewGraphKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb", UseGraph: true},
		nil, nil, WithChunker(ch), WithExtractor(ex), WithIndexManager(im))
	require.NoError(t, err)
	_, err = kb.AddDocuments(context.Background(), []reranker.Document{{ID: "d", Text: "d"}})
	var be *exception.BaseError
	require.True(t, errors.As(err, &be))
	assert.Equal(t, 155506, be.Code())
	assert.Contains(t, be.Message(), "Failed to build triple index")
}

// failSecondBuildIndexer 第一次构建成功、第二次返回 false。
type failSecondBuildIndexer struct {
	graphIndexer
	n int
}

func (f *failSecondBuildIndexer) BuildIndex(ctx context.Context, chunks []common.TextChunk,
	cfg common.IndexConfig, em embedding.BaseEmbedding, opts ...indexer.IndexOption) (bool, error) {
	f.n++
	if f.n == 1 {
		return true, nil
	}
	return false, nil
}

// TestGraphKB_AddDocuments_缺组件 155501 / 155502。
func TestGraphKB_AddDocuments_缺组件(t *testing.T) {
	kb, err := NewGraphKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb"}, nil, nil)
	require.NoError(t, err)
	_, err = kb.AddDocuments(context.Background(), []reranker.Document{{Text: "d"}})
	var be *exception.BaseError
	require.True(t, errors.As(err, &be))
	assert.Equal(t, 155501, be.Code())

	kb2, err := NewGraphKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb"}, nil, nil,
		WithChunker(&countingChunker{}))
	require.NoError(t, err)
	_, err = kb2.AddDocuments(context.Background(), []reranker.Document{{Text: "d"}})
	require.True(t, errors.As(err, &be))
	assert.Equal(t, 155502, be.Code())
}

// TestGraphKB_ParseFiles 基座模板方法继承（对齐 test_parse_files_success）。
func TestGraphKB_ParseFiles(t *testing.T) {
	p := &stubParser{docs: []reranker.Document{{ID: "doc_1", Text: "Test document 1"}}}
	kb, err := NewGraphKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb"}, nil, nil, WithParser(p))
	require.NoError(t, err)
	docs, err := kb.ParseFiles(context.Background(), []string{"test1.txt"})
	require.NoError(t, err)
	assert.Len(t, docs, 1)
	assert.Equal(t, 1, p.calls)
}
```

同时需要 `stubParser` 桩（包内尚无，放进 `graph_kb_test.go`），五方法签名字面取自 `parser/base.go` L24-42：

```go
// stubParser 最小 Parser 桩（仅 Parse 生效，用于验证基座模板方法继承）。
type stubParser struct {
	docs  []reranker.Document
	calls int
}

func (p *stubParser) Parse(_ context.Context, _ string, _ ...parser.ParseOption) ([]reranker.Document, error) {
	p.calls++
	return p.docs, nil
}

func (p *stubParser) LazyParse(_ context.Context, _ string, fn func(reranker.Document) error,
	_ ...parser.ParseOption) error {
	for _, d := range p.docs {
		if err := fn(d); err != nil {
			return err
		}
	}
	return nil
}

func (p *stubParser) Process(ctx context.Context, doc string, opts ...parser.ParseOption) ([]reranker.Document, error) {
	return p.Parse(ctx, doc, opts...)
}

func (p *stubParser) Supports(string) bool { return true }

func (p *stubParser) ParseContent(context.Context, string, *llm.Model) (string, error) {
	return "", nil
}
```

`sync` 用于并发用例（Task 3）；`llm` 仅 `ParseContent` 签名需要。

- [ ] **Step 2.2: 跑测试确认失败**

Run: `cd /home/opensource/uapclaw-gateway && CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/agentcore/retrieval/knowledgebase/... -run TestGraphKB -count=1`
Expected: 编译失败 `undefined: NewGraphKnowledgeBase`。

- [ ] **Step 2.3: 写 graph_kb.go（类型 + AddDocuments + 三元组转换）**

```go
package knowledgebase

import (
	"context"
	"encoding/json"
	"strings"
	"sync"

	"github.com/google/uuid"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/reranker"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/indexing/indexer"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/retriever"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// GraphKnowledgeBase 图增强知识库（13.28）。
//
// 在 SimpleKnowledgeBase 的双索引能力之上加图检索：写侧 use_graph 时抽取
// 三元组建 kb_{id}_triples 索引，读侧懒创建 GraphRetriever 走 beam search
// 图检索。对外仍是 KnowledgeBase 接口，上层按 use_graph 二选一。
//
// Python: openjiuwen/core/retrieval/graph_knowledge_base.py (GraphKnowledgeBase)
type GraphKnowledgeBase struct {
	// BaseKnowledgeBase 共享基座（值嵌入，13.27 先例）
	BaseKnowledgeBase
	// chunkRetriever 固定 chunk 检索器（nil = 由 GraphRetriever 动态创建）
	chunkRetriever retriever.Retriever
	// tripleRetriever 固定三元组检索器（nil = 动态创建）
	tripleRetriever retriever.Retriever
	// graphRetriever 图检索器（nil = 待懒创建）
	graphRetriever retriever.Retriever
	// graphMu 懒创建并发保护；读路径同样持锁（13.27 §13-1 先例）
	graphMu sync.Mutex
}

// （编译期断言 `var _ KnowledgeBase = (*GraphKnowledgeBase)(nil)` 在 Task 4
// 五方法齐全后加入；Task 2 阶段接口未满足，提前断言会编译失败。）

// ──────────────────────────── 导出函数 ────────────────────────────

// NewGraphKnowledgeBase 构造图增强知识库。
//
// chunk/triple 检索器为显式参数（Python 子类特有字段的直译），其余组件经
// KnowledgeBaseOption 透传基座，构造期配置一致性校验随之继承。
//
// Python: GraphKnowledgeBase.__init__
func NewGraphKnowledgeBase(config common.KnowledgeBaseConfig,
	chunkRetriever, tripleRetriever retriever.Retriever,
	opts ...KnowledgeBaseOption) (*GraphKnowledgeBase, error) {
	base, err := NewBaseKnowledgeBase(config, opts...)
	if err != nil {
		return nil, err
	}
	return &GraphKnowledgeBase{
		BaseKnowledgeBase: *base,
		chunkRetriever:    chunkRetriever,
		tripleRetriever:   tripleRetriever,
	}, nil
}

// SetGraphRetriever 注入图检索器（Python kb.graph_retriever = r 的显式对应物）。
//
// 传 nil 表示复位：下次 Retrieve 重新懒创建（Python 赋 None 同义）。
func (g *GraphKnowledgeBase) SetGraphRetriever(r retriever.Retriever) {
	g.graphMu.Lock()
	defer g.graphMu.Unlock()
	g.graphRetriever = r
}

// AddDocuments 添加文档（写 chunk 索引；use_graph 时另写 triple 索引）。
//
// Python: graph_knowledge_base.py add_documents
func (g *GraphKnowledgeBase) AddDocuments(ctx context.Context, documents []reranker.Document,
	opts ...KBOption) ([]string, error) {
	_ = opts
	if g.chunker == nil {
		return nil, exception.BuildError(
			exception.StatusRetrievalKbChunkerNotFound,
			exception.WithMsg("chunker is required for add_documents"))
	}
	if g.indexManager == nil {
		return nil, exception.BuildError(
			exception.StatusRetrievalKbIndexManagerNotFound,
			exception.WithMsg("index_manager is required for add_documents"))
	}
	if g.strictValidation && g.vectorStore != nil {
		if err := g.vectorStore.CheckVectorField(); err != nil {
			return nil, err
		}
	}

	for i := range documents {
		if strings.TrimSpace(documents[i].ID) == "" {
			documents[i].ID = uuid.NewString()
		}
	}

	chunks, err := g.chunker.ChunkDocuments(ctx, documents)
	if err != nil {
		return nil, err
	}
	logger.Info(logComponent).
		Int("document_count", len(documents)).
		Int("chunk_count", len(chunks)).
		Msg("Chunked documents into chunks")

	databaseName := vectorStoreDatabaseName(g.vectorStore)

	chunkConfig := common.IndexConfig{
		IndexName:           "kb_" + g.config.KBID + "_chunks",
		IndexType:           g.config.IndexType,
		UseCaptionForImages: g.config.UseCaptionForImages,
	}
	success, err := g.indexManager.BuildIndex(ctx, chunks, chunkConfig, g.embedModel,
		indexer.WithIndexExtra("database_name", databaseName))
	if err != nil {
		return nil, err
	}
	if !success {
		return nil, exception.BuildError(
			exception.StatusRetrievalKbChunkIndexBuildExecutionError,
			exception.WithMsg("Failed to build chunk index"))
	}

	if g.config.UseGraph && g.extractor != nil {
		logger.Info(logComponent).Msg("Extracting triples for graph index...")
		triples, err := g.extractor.Extract(ctx, chunks)
		if err != nil {
			return nil, err
		}
		if len(triples) > 0 {
			logger.Info(logComponent).Int("triple_count", len(triples)).Msg("Extracted triples")

			// 注意：use_caption_for_images 硬编码 true（与 chunk 索引不对称，Python 真实行为）
			tripleConfig := common.IndexConfig{
				IndexName:           "kb_" + g.config.KBID + "_triples",
				IndexType:           g.config.IndexType,
				UseCaptionForImages: true,
			}
			tripleChunks := triplesToTextChunks(triples)
			success, err := g.indexManager.BuildIndex(ctx, tripleChunks, tripleConfig, g.embedModel,
				indexer.WithIndexExtra("database_name", databaseName))
			if err != nil {
				return nil, err
			}
			if !success {
				return nil, exception.BuildError(
					exception.StatusRetrievalKbTripleIndexBuildExecutionError,
					exception.WithMsg("Failed to build triple index"))
			}
			logger.Info(logComponent).Int("triple_chunk_count", len(tripleChunks)).
				Msg("Built triple index with triples")
		}
	}

	docIDs := make([]string, len(documents))
	for i := range documents {
		docIDs[i] = documents[i].ID
	}
	logger.Info(logComponent).Int("document_count", len(docIDs)).
		Msg("Successfully added documents to knowledge base")
	return docIDs, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// triplesToTextChunks 三元组转 TextChunk（图索引的写入形态）。
//
// metadata 先展开 triple.metadata 再覆写 triple / chunk_index / chunk_id：
// 后两键是 13.23 graph_fetch 读侧回链 chunk 的契约（缺则图扩展断链）。
// triple 用 json.Marshal（紧凑）而非 Python json.dumps 的 ", " 形态，语义等价。
//
// Python: add_documents L144-158
func triplesToTextChunks(triples []common.Triple) []common.TextChunk {
	chunks := make([]common.TextChunk, 0, len(triples))
	for i, t := range triples {
		meta := make(map[string]any, len(t.Metadata)+3)
		for k, v := range t.Metadata {
			meta[k] = v
		}
		tripleJSON, _ := json.Marshal([]string{t.Subject, t.Predicate, t.Object})
		meta["triple"] = string(tripleJSON)
		meta["chunk_index"] = i
		meta["chunk_id"] = metaString(t.Metadata, "chunk_id")

		chunks = append(chunks, common.TextChunk{
			ID:       uuid.NewString(),
			Text:     t.Subject + " " + t.Predicate + " " + t.Object,
			DocID:    metaString(t.Metadata, "doc_id"),
			Metadata: meta,
		})
	}
	return chunks
}

// metaString 从元数据取字符串值，缺失或非 string 收窄空串。
//
// Python 侧 pydantic 对非 str 会抛 ValidationError；Go 收窄（偏差 §8-3，
// 13.25 §8-7 先例）。
func metaString(meta map[string]any, key string) string {
	if meta == nil {
		return ""
	}
	if s, ok := meta[key].(string); ok {
		return s
	}
	return ""
}
```

- [ ] **Step 2.4: 跑测试确认通过**

Run: `cd /home/opensource/uapclaw-gateway && CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/agentcore/retrieval/knowledgebase/... -run TestGraphKB -count=1`
Expected: 全 PASS（含 `TestGraphKB_AddDocuments_*` 与 `TestGraphKB_ParseFiles`）。

- [ ] **Step 2.5: 提交**

```bash
cd /home/opensource/uapclaw-gateway
git add internal/agentcore/retrieval/knowledgebase/graph_kb.go internal/agentcore/retrieval/knowledgebase/graph_kb_test.go
git commit -m "feat(knowledgebase): 13.28 GraphKB 类型 + 双索引写入（三元组转 TextChunk 含三键元数据）"
```

---

### Task 3: Retrieve（三层判定 + 懒创建 + index_type 注入 + agentic + fallback）

**Files:**
- Modify: `internal/agentcore/retrieval/knowledgebase/graph_kb.go`
- Test: `internal/agentcore/retrieval/knowledgebase/graph_kb_test.go`

- [ ] **Step 3.1: 写失败测试（8 例）**

```go
// TestGraphKB_Retrieve_图路径 图路径分派到注入的检索器，参数透传（对齐 test_retrieve_with_graph）。
func TestGraphKB_Retrieve_图路径(t *testing.T) {
	vs, _ := makeCompatiblePair()
	fake := &graphFakeRetriever{results: []common.RetrievalResult{{Text: "Test result", Score: 0.95}}}
	kb, err := NewGraphKnowledgeBase(common.KnowledgeBaseConfig{KBID: "test_kb", UseGraph: true},
		nil, nil, WithVectorStore(vs))
	require.NoError(t, err)
	kb.SetGraphRetriever(fake)

	useGraph := true
	results, err := kb.Retrieve(context.Background(), "test query",
		&common.RetrievalConfig{TopK: 5, UseGraph: &useGraph})
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, 1, fake.calls)
	assert.Equal(t, 5, fake.gotTopK)
	assert.Equal(t, retriever.RetrievalModeHybrid, fake.gotMode) // 零值 index_type → hybrid
	// graph_expansion 显式传递（false 但非 nil）
	require.NotNil(t, fake.gotOpts.GraphExpansion)
	assert.False(t, *fake.gotOpts.GraphExpansion)
}

// TestGraphKB_Retrieve_懒创建注入indexType 不注入时真实构造 GraphRetriever 并注入 index_type。
func TestGraphKB_Retrieve_懒创建注入indexType(t *testing.T) {
	vs, _ := makeCompatiblePair()
	kb, err := NewGraphKnowledgeBase(
		common.KnowledgeBaseConfig{KBID: "test_kb", IndexType: common.IndexTypeBM25, UseGraph: true},
		nil, nil, WithVectorStore(vs))
	require.NoError(t, err)

	got, err := kb.ensureGraphRetriever()
	require.NoError(t, err)
	gret, ok := got.(*retriever.GraphRetriever)
	require.True(t, ok)
	assert.Equal(t, "bm25", gret.IndexType())
	// 单实例：再次获取返回同一对象
	again, err := kb.ensureGraphRetriever()
	require.NoError(t, err)
	assert.Same(t, got, again)
}

// TestGraphKB_Retrieve_并发只建一次 并发 ensureGraphRetriever 返回同一实例（-race）。
func TestGraphKB_Retrieve_并发只建一次(t *testing.T) {
	vs, _ := makeCompatiblePair()
	kb, err := NewGraphKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb", UseGraph: true},
		nil, nil, WithVectorStore(vs))
	require.NoError(t, err)

	const n = 8
	var wg sync.WaitGroup
	got := make([]retriever.Retriever, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			r, err := kb.ensureGraphRetriever()
			assert.NoError(t, err)
			got[idx] = r
		}(i)
	}
	wg.Wait()
	for i := 1; i < n; i++ {
		assert.Same(t, got[0], got[i])
	}
}

// TestGraphKB_Retrieve_无VectorStore 图路径建检索器时缺 store → 155503。
func TestGraphKB_Retrieve_无VectorStore(t *testing.T) {
	kb, err := NewGraphKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb", UseGraph: true}, nil, nil)
	require.NoError(t, err)
	_, err = kb.Retrieve(context.Background(), "q", &common.RetrievalConfig{TopK: 5})
	var be *exception.BaseError
	require.True(t, errors.As(err, &be))
	assert.Equal(t, 155503, be.Code())
	assert.Contains(t, be.Message(), "vector_store is required for graph retrieval")
}

// TestGraphKB_Retrieve_回落SimpleKB use_graph 全 false → 回落 SimpleKB（对齐
// test_retrieve_without_graph_falls_back_to_simple）。bm25 索引避免依赖 embed_model。
func TestGraphKB_Retrieve_回落SimpleKB(t *testing.T) {
	kb, err := NewGraphKnowledgeBase(
		common.KnowledgeBaseConfig{KBID: "kb", IndexType: common.IndexTypeBM25, UseGraph: false},
		nil, nil, WithVectorStore(&fakeAttrStore{}), WithLLMClient(&stubKBLlm{}))
	require.NoError(t, err)

	useGraph := false
	results, err := kb.Retrieve(context.Background(), "q",
		&common.RetrievalConfig{TopK: 5, UseGraph: &useGraph})
	require.NoError(t, err)
	assert.Empty(t, results)
	assert.Nil(t, kb.graphRetriever) // 未走图路径
}

// TestGraphKB_Retrieve_agentic无LLM Agentic=true 且 llmClient 为 nil →
// NewAgenticRetriever 报 155209（证明 agentic 分支已接线）。
func TestGraphKB_Retrieve_agentic无LLM(t *testing.T) {
	vs, _ := makeCompatiblePair()
	kb, err := NewGraphKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb", UseGraph: true},
		nil, nil, WithVectorStore(vs))
	require.NoError(t, err)
	kb.SetGraphRetriever(&graphFakeRetriever{})

	_, err = kb.Retrieve(context.Background(), "q", &common.RetrievalConfig{TopK: 5, Agentic: true})
	var be *exception.BaseError
	require.True(t, errors.As(err, &be))
	assert.Equal(t, 155209, be.Code())
}

// TestGraphKB_Retrieve_agentic未知kwargs Agentic=true + 未知 kwargs → 155510。
func TestGraphKB_Retrieve_agentic未知kwargs(t *testing.T) {
	vs, _ := makeCompatiblePair()
	kb, err := NewGraphKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb", UseGraph: true},
		nil, nil, WithVectorStore(vs), WithLLMClient(&stubKBLlm{}))
	require.NoError(t, err)
	kb.SetGraphRetriever(&graphFakeRetriever{})

	_, err = kb.Retrieve(context.Background(), "q",
		&common.RetrievalConfig{TopK: 5, Agentic: true}, WithKBExtra("bogus", 1))
	var be *exception.BaseError
	require.True(t, errors.As(err, &be))
	assert.Equal(t, 155510, be.Code())
}

// TestGraphKB_SetGraphRetriever_复位 传 nil 复位后重新懒创建。
func TestGraphKB_SetGraphRetriever_复位(t *testing.T) {
	vs, _ := makeCompatiblePair()
	kb, err := NewGraphKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb", UseGraph: true},
		nil, nil, WithVectorStore(vs))
	require.NoError(t, err)
	first, err := kb.ensureGraphRetriever()
	require.NoError(t, err)
	kb.SetGraphRetriever(nil)
	second, err := kb.ensureGraphRetriever()
	require.NoError(t, err)
	assert.NotSame(t, first, second)
}
```

**两处测试设计说明（spec §9.2-5 / §9.2-7 的可观测边界）：**

- `AgenticRetriever.defaultMode`（bm25→sparse 联动）是 `retriever` 包**未导出字段**，`knowledgebase` 包测试读不到，故本节只验证"`GraphRetriever.IndexType()=="bm25"`"（注入生效的因），`defaultMode` 推导由 13.25 单测覆盖，不在此重复。
- fallback 的"不传 extractor / 恒 strict"同样不可直接观测（构造出的 SimpleKB 是局部变量）。改为断言"回落路径不创建图检索器"（`kb.graphRetriever == nil`）+ 代码审查比对构造参数（`retrieveViaSimpleKB` 的选项列表是唯一事实源）。两条的可观测边界已写入 spec §9.4，本计划不再重复断言。

- [ ] **Step 3.2: 跑测试确认失败**

Run: `cd /home/opensource/uapclaw-gateway && CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/agentcore/retrieval/knowledgebase/... -run 'TestGraphKB_Retrieve|TestGraphKB_SetGraphRetriever' -count=1`
Expected: 编译失败 `kb.Retrieve undefined` / `kb.ensureGraphRetriever undefined`。

- [ ] **Step 3.3: 实现 Retrieve + ensureGraphRetriever + fallback**

在 `graph_kb.go` 的 **导出函数** 区（紧跟 `AddDocuments` 之后）加：

```go
// Retrieve 检索相关文档（use_graph 时走图检索，否则回落 SimpleKB）。
//
// Python: graph_knowledge_base.py retrieve
func (g *GraphKnowledgeBase) Retrieve(ctx context.Context, query string,
	config *common.RetrievalConfig, opts ...KBOption) ([]common.RetrievalResult, error) {
	retrievalConfig := config
	if retrievalConfig == nil {
		retrievalConfig = defaultRetrievalConfig()
	}

	useGraph := (retrievalConfig.UseGraph != nil && *retrievalConfig.UseGraph) ||
		(retrievalConfig.UseGraph == nil && g.config.UseGraph)
	if !useGraph {
		return g.retrieveViaSimpleKB(ctx, query, config, opts...)
	}

	gret, err := g.ensureGraphRetriever()
	if err != nil {
		return nil, err
	}
	mode := resolveRetrievalMode(g.config.IndexType)

	r := gret
	if retrievalConfig.Agentic {
		r, err = wrapAgenticRetriever(g.llmClient, gret, opts...)
		if err != nil {
			return nil, err
		}
	}

	return r.Retrieve(ctx, query, retrievalConfig.TopK, retrievalConfig.ScoreThreshold, mode,
		retriever.WithRetrieveFilters(retrievalConfig.Filters),
		retriever.WithRetrieveGraphExpansion(retrievalConfig.GraphExpansion))
}
```

**非导出函数** 区加：

```go
// ensureGraphRetriever 取图检索器，nil 时持锁懒创建。
//
// 六个 WithGraph* 选项与 SetIndexType 注入此前零生产调用，本节首次接线。
// 读路径同样持锁（Go 内存模型下锁外读属数据竞争，13.27 §13-1 先例）。
func (g *GraphKnowledgeBase) ensureGraphRetriever() (retriever.Retriever, error) {
	g.graphMu.Lock()
	defer g.graphMu.Unlock()
	if g.graphRetriever != nil {
		return g.graphRetriever, nil
	}
	if g.vectorStore == nil {
		return nil, exception.BuildError(
			exception.StatusRetrievalKbVectorStoreNotFound,
			exception.WithMsg("vector_store is required for graph retrieval"))
	}

	gret := retriever.NewGraphRetriever(
		retriever.WithGraphChunkRetriever(g.chunkRetriever),
		retriever.WithGraphTripleRetriever(g.tripleRetriever),
		retriever.WithGraphVectorStore(g.vectorStore),
		retriever.WithGraphEmbedModel(g.embedModel),
		retriever.WithGraphChunkCollection("kb_"+g.config.KBID+"_chunks"),
		retriever.WithGraphTripleCollection("kb_"+g.config.KBID+"_triples"),
	)
	// 注入 index_type（GraphRetriever 的 mode 校验与 AgenticRetriever 的
	// defaultMode 推导都读它）
	if carrier, ok := any(gret).(retriever.IndexTypeCarrier); ok {
		carrier.SetIndexType(string(g.config.IndexType))
	}
	g.graphRetriever = gret
	return gret, nil
}

// retrieveViaSimpleKB 回落路径：现构造 SimpleKB 委托（Python 每次新建、不缓存）。
//
// 两处保真：不传 extractor（GraphKB 独有）；不传 strict_validation
// （Python 不传 → SimpleKB 恒为 true）。传原始 config（可能为 nil），由
// SimpleKB 自己做 nil→默认回落。
func (g *GraphKnowledgeBase) retrieveViaSimpleKB(ctx context.Context, query string,
	config *common.RetrievalConfig, opts ...KBOption) ([]common.RetrievalResult, error) {
	base, err := NewSimpleKnowledgeBase(g.config, nil,
		WithVectorStore(g.vectorStore),
		WithEmbedModel(g.embedModel),
		WithParser(g.parser),
		WithChunker(g.chunker),
		WithIndexManager(g.indexManager),
		WithLLMClient(g.llmClient),
	)
	if err != nil {
		return nil, err
	}
	return base.Retrieve(ctx, query, config, opts...)
}
```

- [ ] **Step 3.4: 跑测试确认通过**

Run: `cd /home/opensource/uapclaw-gateway && CGO_ENABLED=1 go test -tags "sqlite_fts5 test" -race ./internal/agentcore/retrieval/knowledgebase/... -run 'TestGraphKB' -count=1`
Expected: 全 PASS，无 DATA RACE。

- [ ] **Step 3.5: 提交**

```bash
cd /home/opensource/uapclaw-gateway
git add internal/agentcore/retrieval/knowledgebase/graph_kb.go internal/agentcore/retrieval/knowledgebase/graph_kb_test.go
git commit -m "feat(knowledgebase): 13.28 GraphKB Retrieve 图路径懒创建 + index_type 注入 + agentic 包装 + SimpleKB 回落"
```

---

### Task 4: Delete / Update / GetStatistics / Close

**Files:**
- Modify: `internal/agentcore/retrieval/knowledgebase/graph_kb.go`
- Test: `internal/agentcore/retrieval/knowledgebase/graph_kb_test.go`

- [ ] **Step 4.1: 写失败测试（8 例）**

```go
// TestGraphKB_DeleteDocuments_图 对齐 test_delete_documents_with_graph：
// chunk + triple 各删一次，返回 true。
func TestGraphKB_DeleteDocuments_图(t *testing.T) {
	im := &graphIndexer{deleteOK: true}
	kb, err := NewGraphKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb", UseGraph: true}, nil, nil,
		WithIndexManager(im))
	require.NoError(t, err)
	ok, err := kb.DeleteDocuments(context.Background(), []string{"doc_1"})
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Len(t, im.delCalls, 2) // chunk + triple 各一次
}

// TestGraphKB_DeleteDocuments_非图 对齐 test_delete_documents_without_graph：仅一次。
func TestGraphKB_DeleteDocuments_非图(t *testing.T) {
	im := &graphIndexer{deleteOK: true}
	kb, err := NewGraphKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb", UseGraph: false}, nil, nil,
		WithIndexManager(im))
	require.NoError(t, err)
	ok, err := kb.DeleteDocuments(context.Background(), []string{"doc_1"})
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Len(t, im.delCalls, 1)
}

// TestGraphKB_DeleteDocuments_triple失败仍返回true triple 删除返回 false 只 Warn。
func TestGraphKB_DeleteDocuments_triple失败仍返回true(t *testing.T) {
	im := &secondDeleteFalseIndexer{graphIndexer: graphIndexer{deleteOK: true}}
	kb, err := NewGraphKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb", UseGraph: true}, nil, nil,
		WithIndexManager(im))
	require.NoError(t, err)
	ok, err := kb.DeleteDocuments(context.Background(), []string{"doc_1"})
	require.NoError(t, err)
	assert.True(t, ok) // triple 失败不影响总结果
}

// secondDeleteFalseIndexer 首次 true，后续 false（chunk 成功 / triple 失败）。
type secondDeleteFalseIndexer struct {
	graphIndexer
	n int
}

func (s *secondDeleteFalseIndexer) DeleteIndex(_ context.Context, docID string, _ string) (bool, error) {
	s.n++
	if s.n == 1 {
		return true, nil
	}
	return false, nil
}

// TestGraphKB_DeleteDocuments_缺索引管理器 155502。
func TestGraphKB_DeleteDocuments_缺索引管理器(t *testing.T) {
	kb, err := NewGraphKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb"}, nil, nil)
	require.NoError(t, err)
	_, err = kb.DeleteDocuments(context.Background(), []string{"doc_1"})
	var be *exception.BaseError
	require.True(t, errors.As(err, &be))
	assert.Equal(t, 155502, be.Code())
}

// TestGraphKB_UpdateDocuments 先删后加（对齐 test_update_documents）。
func TestGraphKB_UpdateDocuments(t *testing.T) {
	im := &graphIndexer{buildOK: true, deleteOK: true}
	ch := &countingChunker{chunks: []common.TextChunk{{ID: "c1", Text: "c1", DocID: "doc_1"}}}
	kb, err := NewGraphKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb", UseGraph: false}, nil, nil,
		WithChunker(ch), WithIndexManager(im))
	require.NoError(t, err)
	ids, err := kb.UpdateDocuments(context.Background(),
		[]reranker.Document{{ID: "doc_1", Text: "Updated document"}})
	require.NoError(t, err)
	assert.Equal(t, []string{"doc_1"}, ids)
	assert.NotEmpty(t, im.delCalls)
	assert.NotEmpty(t, im.calls)
}

// TestGraphKB_GetStatistics_图 对齐 test_get_statistics_with_graph：11 键齐全。
func TestGraphKB_GetStatistics_图(t *testing.T) {
	im := &graphIndexer{info: map[string]any{"count": 10}}
	kb, err := NewGraphKnowledgeBase(common.KnowledgeBaseConfig{KBID: "test_kb", UseGraph: true},
		nil, nil, WithIndexManager(im))
	require.NoError(t, err)
	stats, err := kb.GetStatistics(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "test_kb", stats["kb_id"])
	assert.Equal(t, true, stats["use_graph"])
	assert.Contains(t, stats, "chunk_index_info")
	assert.Contains(t, stats, "triple_index_info")
	assert.Contains(t, stats, "has_graph_retriever")
	assert.Len(t, stats, 11)
}

// TestGraphKB_GetStatistics_无IndexManager 对齐 test_get_statistics_without_index_manager。
func TestGraphKB_GetStatistics_无IndexManager(t *testing.T) {
	kb, err := NewGraphKnowledgeBase(common.KnowledgeBaseConfig{KBID: "test_kb"}, nil, nil)
	require.NoError(t, err)
	stats, err := kb.GetStatistics(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "test_kb", stats["kb_id"])
	assert.Equal(t, false, stats["index_exists"])
}

// TestGraphKB_Close 对齐 test_close：graph / chunk / triple 三检索器均被关闭。
func TestGraphKB_Close(t *testing.T) {
	graph := &graphFakeRetriever{}
	chunk := &graphFakeRetriever{}
	triple := &graphFakeRetriever{}
	kb, err := NewGraphKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb"},
		chunk, triple)
	require.NoError(t, err)
	kb.SetGraphRetriever(graph)

	require.NoError(t, kb.Close())
	assert.Equal(t, 1, graph.closed)
	assert.Equal(t, 1, chunk.closed)
	assert.Equal(t, 1, triple.closed)
}
```

- [ ] **Step 4.2: 跑测试确认失败**

Run: `cd /home/opensource/uapclaw-gateway && CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/agentcore/retrieval/knowledgebase/... -run 'TestGraphKB_Delete|TestGraphKB_Update|TestGraphKB_GetStatistics|TestGraphKB_Close' -count=1`
Expected: 编译失败 `kb.DeleteDocuments undefined`（方法未实现，接口断言也会报错）。

- [ ] **Step 4.3: 实现四方法（导出函数区，置于 Retrieve 之后）**

```go
// DeleteDocuments 删除文档（chunk 索引必删；use_graph 时另删 triple 索引）。
//
// triple 删除返回值 false 只 Warn 不影响总结果（Python 真实行为）；
// DeleteIndex 返回 error 仍冒泡（Python 无 try/except）。
//
// Python: graph_knowledge_base.py delete_documents
func (g *GraphKnowledgeBase) DeleteDocuments(ctx context.Context, docIDs []string,
	opts ...KBOption) (bool, error) {
	_ = opts
	if g.indexManager == nil {
		return false, exception.BuildError(
			exception.StatusRetrievalKbIndexManagerNotFound,
			exception.WithMsg("index_manager is required for delete_documents"))
	}
	if g.strictValidation && g.vectorStore != nil {
		if err := g.vectorStore.CheckVectorField(); err != nil {
			return false, err
		}
	}

	chunkIndexName := "kb_" + g.config.KBID + "_chunks"
	tripleIndexName := "kb_" + g.config.KBID + "_triples"

	success := true
	for _, docID := range docIDs {
		result, err := g.indexManager.DeleteIndex(ctx, docID, chunkIndexName)
		if err != nil {
			return false, err
		}
		if !result {
			success = false
		}
	}

	if g.config.UseGraph {
		for _, docID := range docIDs {
			result, err := g.indexManager.DeleteIndex(ctx, docID, tripleIndexName)
			if err != nil {
				return false, err
			}
			if !result {
				// triple 删除失败不影响总结果
				logger.Warn(logComponent).Str("doc_id", docID).Msg("Failed to delete triples")
			}
		}
	}
	return success, nil
}

// UpdateDocuments 更新文档（先删后加）。
//
// Python: graph_knowledge_base.py update_documents
func (g *GraphKnowledgeBase) UpdateDocuments(ctx context.Context, documents []reranker.Document,
	opts ...KBOption) ([]string, error) {
	if g.strictValidation && g.vectorStore != nil {
		if err := g.vectorStore.CheckVectorField(); err != nil {
			return nil, err
		}
	}
	docIDs := make([]string, len(documents))
	for i := range documents {
		docIDs[i] = documents[i].ID
	}
	if _, err := g.DeleteDocuments(ctx, docIDs); err != nil {
		return nil, err
	}
	return g.AddDocuments(ctx, documents, opts...)
}

// GetStatistics 获取知识库统计信息（11 键，triple_index_info 恒存在）。
//
// Python: graph_knowledge_base.py get_statistics
func (g *GraphKnowledgeBase) GetStatistics(ctx context.Context) (map[string]any, error) {
	chunkIndexName := "kb_" + g.config.KBID + "_chunks"
	tripleIndexName := "kb_" + g.config.KBID + "_triples"

	if g.indexManager == nil {
		return map[string]any{
			"kb_id":        g.config.KBID,
			"index_exists": false,
		}, nil
	}

	chunkInfo, err := g.indexManager.GetIndexInfo(ctx, chunkIndexName)
	if err != nil {
		return nil, err
	}
	var tripleInfo map[string]any
	if g.config.UseGraph {
		tripleInfo, err = g.indexManager.GetIndexInfo(ctx, tripleIndexName)
		if err != nil {
			return nil, err
		}
	}

	return map[string]any{
		"kb_id":              g.config.KBID,
		"index_type":         string(g.config.IndexType),
		"use_graph":          g.config.UseGraph,
		"chunk_index_info":   chunkInfo,
		"triple_index_info":  tripleInfo,
		"has_parser":         g.parser != nil,
		"has_chunker":        g.chunker != nil,
		"has_extractor":      g.extractor != nil,
		"has_embed_model":    g.embedModel != nil,
		"has_vector_store":   g.vectorStore != nil,
		"has_graph_retriever": g.hasGraphRetriever(),
	}, nil
}

// Close 关闭知识库（基座组件 + 三个检索器，顺序字面）。
//
// 注意：GraphRetriever.Close 内部已关闭固定 chunk/triple 检索器，这里再关
// 一遍是 Python 真实行为（重复关闭照复刻，spec §6.1）。
//
// Python: graph_knowledge_base.py close
func (g *GraphKnowledgeBase) Close() error {
	_ = g.BaseKnowledgeBase.Close()

	g.graphMu.Lock()
	graph, chunk, triple := g.graphRetriever, g.chunkRetriever, g.tripleRetriever
	g.graphMu.Unlock()

	g.maybeClose(graph)
	g.maybeClose(chunk)
	g.maybeClose(triple)
	return nil
}
```

**非导出函数** 区补：

```go
// hasGraphRetriever 图检索器是否已创建（持锁读，避免与懒创建竞态）。
func (g *GraphKnowledgeBase) hasGraphRetriever() bool {
	g.graphMu.Lock()
	defer g.graphMu.Unlock()
	return g.graphRetriever != nil
}
```

五方法齐全后，在 `graph_kb.go` 结构体区块末尾（`GraphKnowledgeBase` 定义之后）补编译期断言：

```go
// 编译期断言：GraphKnowledgeBase 完整实现 KnowledgeBase 接口
// （Task 2 阶段缺五方法无法断言，本任务补齐）。
var _ KnowledgeBase = (*GraphKnowledgeBase)(nil)
```

- [ ] **Step 4.4: 跑测试确认通过**

Run: `cd /home/opensource/uapclaw-gateway && CGO_ENABLED=1 go test -tags "sqlite_fts5 test" -race ./internal/agentcore/retrieval/knowledgebase/... -count=1`
Expected: 全 PASS（13.27 既有 + 13.28 新增）。

- [ ] **Step 4.5: 提交**

```bash
cd /home/opensource/uapclaw-gateway
git add internal/agentcore/retrieval/knowledgebase/graph_kb.go internal/agentcore/retrieval/knowledgebase/graph_kb_test.go
git commit -m "feat(knowledgebase): 13.28 GraphKB 删改查统计 + Close（重复关闭字面复刻）"
```

---

### Task 5: 多图 KB 聚合薄委托

**Files:**
- Modify: `internal/agentcore/retrieval/knowledgebase/graph_kb.go`（文件末尾追加）
- Test: `internal/agentcore/retrieval/knowledgebase/graph_kb_test.go`

- [ ] **Step 5.1: 写失败测试**

```go
// TestRetrieveMultiGraphKB_委托 与 13.27 文本版同结果（Python 纯委托）。
func TestRetrieveMultiGraphKB_委托(t *testing.T) {
	fake := &graphFakeRetriever{results: []common.RetrievalResult{{Text: "A", Score: 0.9}}}
	kb, err := NewGraphKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb1", UseGraph: true},
		nil, nil, WithVectorStore(&fakeAttrStore{}))
	require.NoError(t, err)
	kb.SetGraphRetriever(fake)

	got := RetrieveMultiGraphKB(context.Background(), []KnowledgeBase{kb}, "q", nil, nil)
	assert.Equal(t, []string{"A"}, got)
}

// TestRetrieveMultiGraphKBWithSource_委托 带来源版同结果。
func TestRetrieveMultiGraphKBWithSource_委托(t *testing.T) {
	fake := &graphFakeRetriever{results: []common.RetrievalResult{{Text: "A", Score: 0.9}}}
	kb, err := NewGraphKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb1", UseGraph: true},
		nil, nil, WithVectorStore(&fakeAttrStore{}))
	require.NoError(t, err)
	kb.SetGraphRetriever(fake)

	got, err := RetrieveMultiGraphKBWithSource(context.Background(), []KnowledgeBase{kb}, "q", nil, nil)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "A", got[0].Text)
	assert.Equal(t, []string{"kb1"}, got[0].KbIDs)
}
```

- [ ] **Step 5.2: 跑测试确认失败**

Run: `cd /home/opensource/uapclaw-gateway && CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/agentcore/retrieval/knowledgebase/... -run 'TestRetrieveMultiGraphKB' -count=1`
Expected: 编译失败 `undefined: RetrieveMultiGraphKB`。

- [ ] **Step 5.3: 在 graph_kb.go 导出函数区末尾追加**

```go
// RetrieveMultiGraphKB 跨图知识库检索，返回去重排序后的文本列表。
//
// 纯委托 13.27 聚合函数（Python L353-360 同样两行）。
//
// Python: graph_knowledge_base.py retrieve_multi_graph_kb
func RetrieveMultiGraphKB(ctx context.Context, kbs []KnowledgeBase,
	query string, config *common.RetrievalConfig, topK *int) []string {
	return RetrieveMultiKB(ctx, kbs, query, config, topK)
}

// RetrieveMultiGraphKBWithSource 跨图知识库检索，返回带来源信息的结果。
//
// Python: graph_knowledge_base.py retrieve_multi_graph_kb_with_source
func RetrieveMultiGraphKBWithSource(ctx context.Context, kbs []KnowledgeBase,
	query string, config *common.RetrievalConfig, topK *int) ([]common.MultiKBRetrievalResult, error) {
	return RetrieveMultiKBWithSource(ctx, kbs, query, config, topK)
}
```

- [ ] **Step 5.4: 跑测试确认通过**

Run: `cd /home/opensource/uapclaw-gateway && CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/agentcore/retrieval/knowledgebase/... -count=1 -cover`
Expected: 全 PASS；`coverage` ≥ 96%（13.27 收尾基线）。

- [ ] **Step 5.5: 提交**

```bash
cd /home/opensource/uapclaw-gateway
git add internal/agentcore/retrieval/knowledgebase/graph_kb.go internal/agentcore/retrieval/knowledgebase/graph_kb_test.go
git commit -m "feat(knowledgebase): 13.28 多图 KB 聚合两函数薄委托"
```

---

### Task 6: doc.go、注释纠偏、计划表状态

**Files:**
- Modify: `internal/agentcore/retrieval/knowledgebase/doc.go`
- Modify: `internal/agentcore/retrieval/retriever/base.go:66`（GraphHops 注释纠偏）
- Modify: `internal/agentcore/retrieval/retriever/capability.go:44`
- Modify: `IMPLEMENTATION_PLAN.md`（13.28 行 / L838 验证点 / L840 跨域联动 / 7.36 行）

- [ ] **Step 6.1: knowledgebase/doc.go 更新**

文件目录段替换为：

```
//	knowledgebase/
//	├── doc.go           # 包文档
//	├── base.go          # KnowledgeBase 接口 + BaseKnowledgeBase + 构造/setter/模板方法（13.26）
//	├── validate.go      # ValidateIndex 三段式 + attrEqual + isChromaType（13.26）
//	├── kb_helpers.go    # 包级复用：默认检索配置/mode 推导/database_name 取值/agentic 包装（13.28）
//	├── simple_kb.go     # SimpleKnowledgeBase 完整实现（13.27，5 抽象方法 + 懒创建 + agentic 包装）
//	├── multi_kb.go      # 跨知识库聚合检索两函数（13.27）
//	└── graph_kb.go      # GraphKnowledgeBase 图增强实现（13.28，双索引写入 + 图路径检索）
```

包概述追加一段（替换原"13.28 graph.go 落地时补充目录条目"那句）：

```
// GraphKnowledgeBase（13.28）是图增强实现：写侧 use_graph 时抽取三元组、
// 建 kb_{id}_triples 索引；读侧懒创建 GraphRetriever 走 beam search 图检索，
// index_type 注入后由检索器自行校验 mode。三元组 chunk 的 metadata 承载
// doc_id/chunk_id，供 13.23 graph_fetch 回链原始分块。
```

"核心类型/接口索引"补一行：

```
//	GraphKnowledgeBase — 图增强知识库实现（双索引 + 图检索）
```

- [ ] **Step 6.2: 注释纠偏（三条）**

`retriever/base.go` 的 `GraphHops` 字段注释：

```go
	// GraphHops 图检索跳数（13.23 消费；nil→默认 2；显式 0 →
	// max_length=0 → 155204，Python 行为保真）。死参数：Python 侧
	// kwargs.get 消费、全库零生产方（13.28 GraphKB 亦不传 graph_hops）。
```

`retriever/capability.go` 的 `IndexTypeCarrier` 注释：

```go
	// SetIndexType 注入 index_type（13.28 GraphKB 已接线）
```

`retriever/base.go` 的 `GraphExpansion` 字段注释中"13.28 GraphKB 传 ... 默认 False"保留（已成立），仅把将来时改完成时：

```go
	// 指针类型：13.28 GraphKB 传 RetrievalConfig.graph_expansion（默认 False），
```

- [ ] **Step 6.3: IMPLEMENTATION_PLAN.md 状态同步**

1. 13.28 行：`| 13.28 | ☐ | GraphKnowledgeBase |` → `| 13.28 | ✅ | GraphKnowledgeBase |`，内容列补实现摘要（双索引写入 + triple 元数据三键 + 图路径懒创建 + index_type 注入 + agentic + fallback + 多图聚合委托 + Close 重复关闭复刻；覆盖率值填实际数）。
2. 领域十三验证点 L838：`☐ 检索查询管线闭环（...）（13.19-13.28 完成后达成）` → `✅ 检索查询管线闭环（...）`。
3. L840 跨域联动：注明"13.26-13.28 已完成，8.25 可直接消费 `retrieve_multi_kb_with_source` / `RetrieveMultiGraphKBWithSource`"。
4. 7.36 行的 ⤵️ 缺口说明末尾补一句"领域十三已覆盖完整写入+读取管线"。
5. 领域十三开头追加一行小节说明：

```
> 2026-10-30：13.28 GraphKnowledgeBase 完成（图增强 KB：use_graph 时双索引写入 + GraphRetriever 懒创建与 index_type 注入 + agentic 包装 + SimpleKB 回落；错误码 155505/155506 零消费转已消费；Close 签名统一为 Close() error）。领域十三收口。设计：docs/superpowers/specs/2026-10-30-graphknowledgebase-13.28-design.md
```

- [ ] **Step 6.4: 全量回归 + race**

Run:
```bash
cd /home/opensource/uapclaw-gateway
pgrep -f 'go (build|test)' || true
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/agentcore/retrieval/... -count=1 -race
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./... -count=1
```
Expected: 两个命令全 PASS，零 FAIL。（规则 5：若发现多个 go 进程先 kill 再跑。）

- [ ] **Step 6.5: 提交**

```bash
cd /home/opensource/uapclaw-gateway
git add internal/agentcore/retrieval/knowledgebase/doc.go internal/agentcore/retrieval/retriever/base.go \
        internal/agentcore/retrieval/retriever/capability.go IMPLEMENTATION_PLAN.md
git commit -m "docs(knowledgebase): 13.28 doc.go/注释纠偏/计划表收口（领域十三闭环）"
```

---

## 完成判定

- [ ] Task 0-6 全部提交
- [ ] `knowledgebase` 包覆盖率 ≥ 13.27 基线（96.2%），`-race` 干净
- [ ] 全量回归 `CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./...` 零 FAIL
- [ ] Python 12 例全部有对应 Go 用例（`TestGraphKB_ParseFiles` / `_AddDocuments_图索引` / `_非图` / `_Retrieve_图路径` / `_agentic无LLM` / `_回落SimpleKB` / `_DeleteDocuments_图` / `_非图` / `_UpdateDocuments` / `_GetStatistics_图` / `_无IndexManager` / `_Close`）
- [ ] 错误码 155505 / 155506 从零消费转已消费（`grep -rn StatusRetrievalKbChunkIndexBuildExecutionError internal --include=*.go` 有命中）
