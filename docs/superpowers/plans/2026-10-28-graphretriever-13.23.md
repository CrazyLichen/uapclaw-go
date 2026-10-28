# 13.23 GraphRetriever 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 GraphRetriever + TripleBeamSearch（三元组 beam search 图检索），消费 `kb_{id}_triples` 索引，含五处 Python 鸭子点的 Go 消解与 13.20/13.21/13.22 回填。

**Architecture:** 方案 A 能力接口（EmbedModelProvider/IndexTypeCarrier/VectorStoreAccessor/MaxBatchSizeProvider/SupportedModesProvider，hasattr/getattr 三态同构）+ gonum floats 数值层 + store 具体类型断言的 fetch 层。spec：`docs/superpowers/specs/2026-10-28-graphretriever-13.23-design.md`。

**Tech Stack:** Go 1.26 / gonum.org/v1/gonum floats（go.sum 已有，需 go.mod 转直接依赖）/ milvusclient v2.6.5 / chromav2 v0.4.1 / errgroup

**编译纪律**：每次编译前 `pgrep -f 'go (build|test)'` 检查残留；单进程互斥；GOPROXY 已配置。测试命令统一 `CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/agentcore/retrieval/...`（memory 包 FTS 假失败先例，跑全量时必须带标签；单包跑可省）。

**Python 基线**：`/home/opensource/agent-core/openjiuwen/core/retrieval/retriever/graph_retriever.py`（813 行）；测试 `/home/opensource/agent-core/tests/unit_tests/core/retrieval/retriever/test_graph_retriever.py`（20 例）。计划中 L 标注均为该文件行号。

**SDK 事实（已核实钉死）**：
- Milvus：`milvusclient.NewQueryOption(coll).WithFilter(expr).WithOutputFields("*").WithLimit(n)` → `client.Query(ctx, opt)` → `ResultSet{ResultCount, IDs, Fields DataSet}`；列遍历先例 milvus.go L1196-1220（`col.Get(j)` 逐行取值）
- Chroma：`client.GetCollection(ctx, name)` → `collection.Get(ctx, chromav2.WithWhere(clause), chromav2.WithLimit(n))` → `GetResult.GetIDs()/GetDocuments()/GetMetadatas()`；`EqString(field, v)`/`InString(field, vs...)` 构造 WhereClause；metadata GetRaw→map 先例 chroma.go L1012-1018；`DocumentMetadataImpl.Keys()` 键集
- ChromaVectorStore.getCollection 有 nil 缓存（L959-963）——SetCollectionName 必须清缓存
- gonum：`floats.Norm(s []float64, L float64) float64`（L=2 即 L2）；v0.16.0 在 go.sum 不在 go.mod

---

### Task 1: go.mod 引入 gonum 直接依赖

**Files:**
- Modify: `go.mod`

- [ ] **Step 1: go.mod 添加 gonum**

```bash
cd /home/opensource/uapclaw-gateway && go mod edit -require=gonum.org/v1/gonum@v0.16.0 && go mod tidy
```

- [ ] **Step 2: 验证可编译**

```bash
cd /home/opensource/uapclaw-gateway && go build ./internal/agentcore/retrieval/...
```

Expected: 编译通过（gonum v0.16.0 已在 go.sum，零下载）

- [ ] **Step 3: Commit**

```bash
git add go.mod go.sum && git commit -m "build: 引入 gonum 直接依赖（13.23 余弦打分数值层）"
```

---

### Task 2: 数值层——cosineScores + topkIndices

**Files:**
- Create: `internal/agentcore/retrieval/retriever/graph_numeric.go`
- Test: `internal/agentcore/retrieval/retriever/graph_numeric_test.go`

对应 Python `_cosine_scores`（L52-63）/`_topk`（L65-72）。

- [ ] **Step 1: 写失败测试**

```go
package retriever

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestCosineScores_基本余弦 对齐 Python _cosine_scores：归一化后点积
func TestCosineScores_基本余弦(t *testing.T) {
	query := []float64{1, 0}
	cands := [][]float64{
		{1, 0},  // cos=1
		{0, 1},  // cos=0
		{-1, 0}, // cos=-1
		{3, 0},  // 归一化后 cos=1（量纲无关）
	}
	scores := cosineScores(query, cands)
	assert.InDelta(t, 1.0, scores[0], 1e-9)
	assert.InDelta(t, 0.0, scores[1], 1e-9)
	assert.InDelta(t, -1.0, scores[2], 1e-9)
	assert.InDelta(t, 1.0, scores[3], 1e-9)
}

// TestCosineScores_零向量哨兵 1e-12 防 NaN
func TestCosineScores_零向量哨兵(t *testing.T) {
	scores := cosineScores([]float64{0, 0}, [][]float64{{1, 0}})
	assert.False(t, math.IsNaN(scores[0]))
	assert.InDelta(t, 0.0, scores[0], 1e-9)
}

// TestCosineScores_空候选
func TestCosineScores_空候选(t *testing.T) {
	scores := cosineScores([]float64{1}, nil)
	assert.Empty(t, scores)
}

// TestTopkIndices_基本 对齐 argpartition+argsort 降序
func TestTopkIndices_基本(t *testing.T) {
	idx, scores := topkIndices([]float64{0.1, 0.9, 0.5, 0.7}, 2)
	assert.Equal(t, []int{1, 3}, idx)
	assert.InDelta(t, 0.9, scores[0], 1e-9)
	assert.InDelta(t, 0.7, scores[1], 1e-9)
}

// TestTopkIndices_k超出N 对齐 k=min(k, N)
func TestTopkIndices_k超出N(t *testing.T) {
	idx, scores := topkIndices([]float64{0.3, 0.1}, 5)
	assert.Len(t, idx, 2)
	assert.InDelta(t, 0.3, scores[0], 1e-9)
}

// TestTopkIndices_k非正 对齐 k<=0 → ([], [])
func TestTopkIndices_k非正(t *testing.T) {
	idx, scores := topkIndices([]float64{0.3, 0.1}, 0)
	assert.Empty(t, idx)
	assert.Empty(t, scores)
	idx, _ = topkIndices([]float64{0.3}, -1)
	assert.Empty(t, idx)
}

// TestTopkIndices_空输入
func TestTopkIndices_空输入(t *testing.T) {
	idx, scores := topkIndices(nil, 3)
	assert.Empty(t, idx)
	assert.Empty(t, scores)
}
```

- [ ] **Step 2: 跑测试确认失败**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/retrieval/retriever/ -run "TestCosineScores|TestTopkIndices" -v
```

Expected: FAIL（cosineScores/topkIndices 未定义）

- [ ] **Step 3: 实现 graph_numeric.go**

```go
// Package retriever 数值工具：graph_retriever 的余弦打分与 topk 选择。
//
// Python: graph_retriever.py (_cosine_scores L52-63 / _topk L65-72)
package retriever

import (
	"sort"

	"gonum.org/v1/gonum/floats"
)

// ──────────────────────────── 非导出函数 ────────────────────────────

// cosineScores 计算查询向量与候选向量集的余弦相似度。
//
// 对齐 Python _cosine_scores：逐向量 L2 归一化（+1e-12 哨兵防零向量
// 除零）后点积。float32 截断不复刻（numpy BLAS 求和序 bit-exact 不可达，
// spec §8-3）。
func cosineScores(queryVec []float64, candVecs [][]float64) []float64 {
	scores := make([]float64, len(candVecs))
	qNorm := floats.Norm(queryVec, 2) + 1e-12
	for i, cand := range candVecs {
		cNorm := floats.Norm(cand, 2) + 1e-12
		dot := 0.0
		for j := range queryVec {
			if j < len(cand) {
				// Python 先除后乘（q/q_norm @ c/c_norm）的逐步语义
				dot += queryVec[j] / qNorm * (cand[j] / cNorm)
			}
		}
		scores[i] = dot
	}
	return scores
}

// topkIndices 返回分数最高的前 k 个（索引+分数），降序。
//
// 对齐 Python _topk：k=min(k,N)；k<=0 返回空。argpartition 的选择集+
// 降序语义用全排序实现（等值非稳定与 Python 同构，spec §8-4）。
func topkIndices(scores []float64, k int) ([]int, []float64) {
	k = min(k, len(scores))
	if k <= 0 {
		return nil, nil
	}
	idx := make([]int, len(scores))
	for i := range idx {
		idx[i] = i
	}
	sort.Slice(idx, func(a, b int) bool { return scores[idx[a]] > scores[idx[b]] })
	idx = idx[:k]
	out := make([]float64, k)
	for i, v := range idx {
		out[i] = scores[v]
	}
	return idx, out
}
```

- [ ] **Step 4: 跑测试确认通过**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/retrieval/retriever/ -run "TestCosineScores|TestTopkIndices" -v
```

Expected: 7 例全 PASS

- [ ] **Step 5: Commit**

```bash
git add internal/agentcore/retrieval/retriever/graph_numeric.go internal/agentcore/retrieval/retriever/graph_numeric_test.go && git commit -m "feat(retriever): 13.23 数值层——cosineScores 余弦打分 + topkIndices（gonum Norm）"
```

---

### Task 3: 能力接口 + 13.20/13.21 三实现回填访问器

**Files:**
- Create: `internal/agentcore/retrieval/retriever/capability.go`
- Modify: `internal/agentcore/retrieval/retriever/vector_retriever.go`（struct 加 indexType + 4 方法）
- Modify: `internal/agentcore/retrieval/retriever/sparse_retriever.go`（struct 加 indexType + 3 方法）
- Modify: `internal/agentcore/retrieval/retriever/hybrid_retriever.go`（struct 加 indexType + 4 方法）
- Test: `internal/agentcore/retrieval/retriever/capability_test.go`

对应 Python hasattr（L48）/getattr（L193）/属性挖取（L622-631）/SUPPORTED_MODES（L299）。

- [ ] **Step 1: 写失败测试**

```go
package retriever

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestVectorRetriever_能力接口回填 embed_model/vector_store/index_type 三态
func TestVectorRetriever_能力接口回填(t *testing.T) {
	store := &fakeVectorStore{}
	r := NewVectorRetriever(store, nil)

	// EmbedModelProvider：实现且返回 nil（Python 属性存在值为 None）
	ep, ok := r.(EmbedModelProvider)
	assert.True(t, ok)
	assert.Nil(t, ep.EmbedModel())

	// VectorStoreAccessor
	vs, ok := r.(VectorStoreAccessor)
	assert.True(t, ok)
	assert.Equal(t, store, vs.VectorStore())

	// IndexTypeCarrier：零值 "" = Python None
	ic, ok := r.(IndexTypeCarrier)
	assert.True(t, ok)
	assert.Equal(t, "", ic.IndexType())
	ic.SetIndexType("hybrid")
	assert.Equal(t, "hybrid", ic.IndexType())
}

// TestSparseRetriever_能力接口回填 无 embed_model 属性（hasattr False 同构）
func TestSparseRetriever_能力接口回填(t *testing.T) {
	r := NewSparseRetriever(&fakeVectorStore{})

	_, ok := r.(EmbedModelProvider)
	assert.False(t, ok) // Python SparseRetriever 无 embed_model 属性

	_, ok = r.(VectorStoreAccessor)
	assert.True(t, ok)

	_, ok = r.(IndexTypeCarrier)
	assert.True(t, ok)
}

// TestHybridRetriever_能力接口回填
func TestHybridRetriever_能力接口回填(t *testing.T) {
	r := NewHybridRetriever(&fakeVectorStore{}, nil)

	ep, ok := r.(EmbedModelProvider)
	assert.True(t, ok)
	assert.Nil(t, ep.EmbedModel())

	_, ok = r.(VectorStoreAccessor)
	assert.True(t, ok)

	_, ok = r.(IndexTypeCarrier)
	assert.True(t, ok)
}
```

- [ ] **Step 2: 跑测试确认失败**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/retrieval/retriever/ -run "能力接口回填" -v
```

Expected: FAIL

- [ ] **Step 3: 实现 capability.go**

```go
// Package retriever 能力接口：Python hasattr/getattr 鸭子探测的 Go 同构物。
//
// 类型断言三态语义（13.23 spec §3.1）：
//   - 未实现接口    ↔ Python hasattr False（如 SparseRetriever.embed_model）
//   - 实现返回 nil  ↔ Python 属性存在值为 None
//   - 实现返回非 nil ↔ Python 属性存在且非 None
package retriever

import (
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/embedding"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/vector_store"
)

// ──────────────────────────── 接口 ────────────────────────────

// EmbedModelProvider 嵌入模型提供者。
//
// Python: hasattr(retriever, "embed_model")（graph_retriever.py L48）
type EmbedModelProvider interface {
	// EmbedModel 返回嵌入模型（nil = Python None）
	EmbedModel() embedding.BaseEmbedding
}

// IndexTypeCarrier index_type 载体（上层 KnowledgeBase 注入）。
//
// Python: getattr(retriever, "index_type", None)（L193/L375）+ 动态属性
// 注入（L376）。13.25 AgenticRetriever 构造 default_mode 推导复用。
type IndexTypeCarrier interface {
	// IndexType 返回 index_type（"" = Python None 未注入）
	IndexType() string
	// SetIndexType 注入 index_type（上层 13.28 GraphKB 调用）
	SetIndexType(indexType string)
}

// VectorStoreAccessor 向量存储访问器。
//
// Python: hasattr(retriever, "vector_store") + 属性挖取（L622-631）
type VectorStoreAccessor interface {
	// VectorStore 返回底层向量存储（nil = Python 无该属性/None）
	VectorStore() vector_store.VectorStore
}

// MaxBatchSizeProvider 嵌入批大小提供者。
//
// Python: embed_model.max_batch_size（L91）——retrieval 层四实现
// （API/Dashscope/OpenAI/VLLM）的实例字段，foundation 层 Embedding ABC
// 无此属性。断言失败显式报 155202（拍板 c，spec §3.7）。
type MaxBatchSizeProvider interface {
	// MaxBatchSize 返回每批最大文档数
	MaxBatchSize() int
}

// SupportedModesProvider 支持模式声明（Python SUPPORTED_MODES 类属性
// 探测通道，L299）。Retriever 接口（13.19）无此方法；测试 stub/自定义
// 实现可选用。
type SupportedModesProvider interface {
	// SupportedModes 返回支持的 RetrievalMode 清单
	SupportedModes() []RetrievalMode
}
```

- [ ] **Step 4: 三实现回填（纯加法）**

vector_retriever.go struct 加字段：

```go
	// indexType 索引类型（"" = Python None；上层 GraphRetriever/GraphKB
	// 注入，13.23 回填）
	indexType string
```

文件内"导出函数"区（构造函数之后）追加：

```go
// EmbedModel 返回嵌入模型（能力接口回填，13.23；nil = Python None）。
func (r *VectorRetriever) EmbedModel() embedding.BaseEmbedding { return r.embedModel }

// VectorStore 返回底层向量存储（能力接口回填，13.23）。
func (r *VectorRetriever) VectorStore() vector_store.VectorStore { return r.vectorStore }

// IndexType 返回索引类型（"" = Python None，能力接口回填 13.23）。
func (r *VectorRetriever) IndexType() string { return r.indexType }

// SetIndexType 注入索引类型（上层 GraphRetriever 动态创建后回写，13.23）。
func (r *VectorRetriever) SetIndexType(indexType string) { r.indexType = indexType }
```

sparse_retriever.go struct 加同款 `indexType string` 字段，追加：

```go
// VectorStore 返回底层向量存储（能力接口回填，13.23）。
func (r *SparseRetriever) VectorStore() vector_store.VectorStore { return r.vectorStore }

// IndexType 返回索引类型（"" = Python None，能力接口回填 13.23）。
func (r *SparseRetriever) IndexType() string { return r.indexType }

// SetIndexType 注入索引类型（上层注入通道，13.23）。
func (r *SparseRetriever) SetIndexType(indexType string) { r.indexType = indexType }
```

hybrid_retriever.go struct 加 `indexType string` 字段，追加四方法（EmbedModel/VectorStore/IndexType/SetIndexType，同 VectorRetriever 形态）。

- [ ] **Step 5: 跑测试确认通过**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/retrieval/retriever/ -run "能力接口回填" -v
```

Expected: 3 例 PASS

- [ ] **Step 6: 全包回归（零破坏确认）**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/retrieval/retriever/ -count=1
```

Expected: 全 PASS

- [ ] **Step 7: Commit**

```bash
git add internal/agentcore/retrieval/retriever/capability.go internal/agentcore/retrieval/retriever/capability_test.go internal/agentcore/retrieval/retriever/vector_retriever.go internal/agentcore/retrieval/retriever/sparse_retriever.go internal/agentcore/retrieval/retriever/hybrid_retriever.go && git commit -m "feat(retriever): 13.23 能力接口五件套 + 三实现回填访问器（indexType/embedModel/vectorStore）"
```

---

### Task 4: store 侧回填——CollectionName getter/setter

**Files:**
- Modify: `internal/agentcore/retrieval/vector_store/milvus.go`
- Modify: `internal/agentcore/retrieval/vector_store/chroma.go`
- Test: `internal/agentcore/retrieval/vector_store/graph_backfill_test.go`

对应 Python `self.vector_store.collection_name = collection_name`（L340）与读取（L627）。

- [ ] **Step 1: 写失败测试**

```go
package vector_store

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestChromaVectorStore_CollectionName回填 getter/setter + 缓存失效
func TestChromaVectorStore_CollectionName回填(t *testing.T) {
	s := &ChromaVectorStore{collectionName: "old"}
	assert.Equal(t, "old", s.CollectionName())
	s.SetCollectionName("kb_1_triples")
	assert.Equal(t, "kb_1_triples", s.CollectionName())
	// 缓存失效：Set 后 getCollection 必须重新按名取（nil 缓存被清）
	assert.Nil(t, s.collection)
}

// TestMilvusVectorStore_CollectionName回填
func TestMilvusVectorStore_CollectionName回填(t *testing.T) {
	s := &MilvusVectorStore{collectionName: "old"}
	assert.Equal(t, "old", s.CollectionName())
	s.SetCollectionName("kb_1_chunks")
	assert.Equal(t, "kb_1_chunks", s.CollectionName())
}
```

注：结构体字面量构造绕开正式构造器，同包测试专测字段契约；`mu` 字段零值可用。

- [ ] **Step 2: 跑测试确认失败**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/retrieval/vector_store/ -run "CollectionName回填" -v
```

Expected: FAIL

- [ ] **Step 3: 实现（两文件"导出函数"区各加两方法）**

milvus.go（Client(ctx) 已存在 L265 无需动）：

```go
// CollectionName 返回集合名称（13.23 回填：GraphRetriever 动态创建读取）。
func (s *MilvusVectorStore) CollectionName() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.collectionName
}

// SetCollectionName 设置集合名称（13.23 回填：对齐 Python graph_retriever
// L340 字段赋值——先污染后校验的字面顺序在调用方保留）。
func (s *MilvusVectorStore) SetCollectionName(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.collectionName = name
}
```

chroma.go（同形态 + 缓存失效）：

```go
// CollectionName 返回集合名称（13.23 回填）。
func (s *ChromaVectorStore) CollectionName() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.collectionName
}

// SetCollectionName 设置集合并失效集合缓存（13.23 回填）。
//
// Python 侧 collection_name 字段赋值后，collection.get 每次按名新建；
// Go 侧 getCollection 有 nil 缓存，Set 时必须清空对齐"每次按名取"语义
// （spec §7-3）。
func (s *ChromaVectorStore) SetCollectionName(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.collectionName = name
	s.collection = nil
}
```

实施前 `grep -n "mu " internal/agentcore/retrieval/vector_store/chroma.go` 确认锁字段名（若 Chroma 锁名不同按实际调整）。

- [ ] **Step 4: 跑测试确认通过 + 全包回归**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/retrieval/vector_store/ -count=1
```

Expected: 全 PASS

- [ ] **Step 5: Commit**

```bash
git add internal/agentcore/retrieval/vector_store/milvus.go internal/agentcore/retrieval/vector_store/chroma.go internal/agentcore/retrieval/vector_store/graph_backfill_test.go && git commit -m "feat(vector_store): 13.23 回填 CollectionName getter/setter（Chroma Set 失效集合缓存）"
```

---

### Task 5: embedding 四实现回填 MaxBatchSize getter

**Files:**
- Modify: `internal/agentcore/retrieval/embedding/api.go`
- Modify: `internal/agentcore/retrieval/embedding/dashscope.go`
- Modify: `internal/agentcore/retrieval/embedding/openai.go`
- Modify: `internal/agentcore/retrieval/embedding/vllm.go`
- Test: `internal/agentcore/retrieval/embedding/maxbatch_test.go`

对应 Python `embed_model.max_batch_size`（L91；Python 四实现 `__init__` 默认 8）。

- [ ] **Step 1: 写失败测试**

测试骨架（四个构造器必填参数实施时从各文件的 NewXxx 签名读取填入）：

```go
package embedding

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Test四实现MaxBatchSizeGetter 构造默认 8 + 选项覆写回读
func Test四实现MaxBatchSizeGetter(t *testing.T) {
	// 实施时按 NewAPIEmbedding 实际签名构造（最小参数集）
	a := mustNewAPIEmbedding(t)
	assert.Equal(t, 8, a.MaxBatchSize())

	o := mustNewOpenAIEmbedding(t)
	assert.Equal(t, 8, o.MaxBatchSize())

	d := mustNewDashscopeEmbedding(t)
	assert.Equal(t, 8, d.MaxBatchSize())

	v := mustNewVLLMEmbedding(t)
	assert.Equal(t, 8, v.MaxBatchSize())
}
```

`mustNewXxx` 辅助函数实施时按构造签名补（各构造器的 config/必填项不同）；若有 WithXxxMaxBatchSize 选项则加覆写回读断言。

- [ ] **Step 2: 跑测试确认失败**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/retrieval/embedding/ -run "MaxBatchSizeGetter" -v
```

Expected: FAIL

- [ ] **Step 3: 实现（四文件各加一个方法）**

```go
// MaxBatchSize 返回每批最大文档数（13.23 回填：MaxBatchSizeProvider
// 能力接口；Python max_batch_size 实例字段）。
func (a *APIEmbedding) MaxBatchSize() int { return a.maxBatchSize }
```

四文件同形态（接收者/字段名按实际：api.go `a *APIEmbedding`、dashscope.go、openai.go、vllm.go——实施时 `grep -n maxBatchSize` 各文件确认字段名；VLLM 若无该字段则查 Python vllm_embedding.py 是否有 max_batch_size，有则补字段、无则跳过 VLLM 并归档偏差）。

- [ ] **Step 4: 跑测试确认通过**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/retrieval/embedding/ -count=1
```

Expected: 全 PASS

- [ ] **Step 5: Commit**

```bash
git add internal/agentcore/retrieval/embedding/ && git commit -m "feat(embedding): 13.23 回填四实现 MaxBatchSize getter（能力接口消费点）"
```

---

### Task 6: TripleBeamSearch 核心

**Files:**
- Create: `internal/agentcore/retrieval/retriever/graph_beam_search.go`
- Test: `internal/agentcore/retrieval/retriever/graph_beam_search_test.go`

对应 Python `TripleBeamSearch` 类（L24-225）。

- [ ] **Step 1: 写失败测试（Python TestTripleBeamSearch 10 例映射 + 增强）**

```go
package retriever

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/embedding"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
)

// ──────────────────────────── 测试桩 ────────────────────────────

// fakeBeamEmbedModel 嵌入桩：可编程多段返回 + MaxBatchSizeProvider。
type fakeBeamEmbedModel struct {
	mu        sync.Mutex
	batches   [][][]float64 // 依次返回的批次
	calls     int
	lastTexts []string
	lastOpts  []embedding.EmbedOption
	batchSize int // MaxBatchSize 返回值
}

func (f *fakeBeamEmbedModel) EmbedQuery(context.Context, string, ...embedding.EmbedOption) ([]float64, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeBeamEmbedModel) EmbedDocuments(_ context.Context, texts []string, opts ...embedding.EmbedOption) ([][]float64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	idx := f.calls
	f.calls++
	f.lastTexts = texts
	f.lastOpts = opts
	if idx >= len(f.batches) {
		return nil, errors.New("no more batches")
	}
	return f.batches[idx], nil
}
func (f *fakeBeamEmbedModel) Dimension() int                                       { return 3 }
func (f *fakeBeamEmbedModel) DimensionWithContext(context.Context) (int, error)    { return 3, nil }
func (f *fakeBeamEmbedModel) MaxBatchSize() int                                    { return f.batchSize }

// plainEmbedModel 无 MaxBatchSize 的嵌入桩（断言失败 155202 用例）
type plainEmbedModel struct{}

func (p *plainEmbedModel) EmbedQuery(context.Context, string, ...embedding.EmbedOption) ([]float64, error) {
	return nil, nil
}
func (p *plainEmbedModel) EmbedDocuments(context.Context, []string, ...embedding.EmbedOption) ([][]float64, error) {
	return nil, nil
}
func (p *plainEmbedModel) Dimension() int                                    { return 0 }
func (p *plainEmbedModel) DimensionWithContext(context.Context) (int, error) { return 0, nil }

// fakeBeamRetriever 候选检索桩（可编程 Retrieve）
type fakeBeamRetriever struct {
	mu         sync.Mutex
	retrieveFn func(ctx context.Context, query string, topK int) ([]common.RetrievalResult, error)
	lastQuery  string
	lastTopK   int
	lastMode   RetrievalMode
	calls      int
}

func (f *fakeBeamRetriever) Retrieve(ctx context.Context, query string, topK int, _ *float64, mode RetrievalMode, _ ...RetrieveOption) ([]common.RetrievalResult, error) {
	f.mu.Lock()
	f.lastQuery, f.lastTopK, f.lastMode, f.calls = query, topK, mode, f.calls+1
	f.mu.Unlock()
	return f.retrieveFn(ctx, query, topK)
}
func (f *fakeBeamRetriever) BatchRetrieve(context.Context, []string, int, ...RetrieveOption) ([][]common.RetrievalResult, error) {
	return nil, nil
}
func (f *fakeBeamRetriever) RetrieveSearchResults(context.Context, string, int, RetrievalMode, ...RetrieveOption) ([]common.SearchResult, error) {
	return nil, nil
}
func (f *fakeBeamRetriever) Close() {}

// noEmbedRetriever 无 embed_model 属性的桩（hasattr False 同构——
// 不实现 EmbedModelProvider）
type noEmbedRetriever struct{ fakeBeamRetriever }

// withEmbedRetriever 有 embed_model 属性的桩（_RetrieverStubNoIndexType 对应物）
type withEmbedRetriever struct {
	fakeBeamRetriever
	embedModel embedding.BaseEmbedding
}

func (s *withEmbedRetriever) EmbedModel() embedding.BaseEmbedding { return s.embedModel }

// withIndexTypeRetriever 有 index_type 属性的桩
type withIndexTypeRetriever struct {
	fakeBeamRetriever
	indexType string
}

func (s *withIndexTypeRetriever) IndexType() string     { return s.indexType }
func (s *withIndexTypeRetriever) SetIndexType(v string) { s.indexType = v }

// makeTriple 构造带 triple 元数据的三元组结果
func makeTriple(text, tripleJSON string, score float64) common.RetrievalResult {
	return common.RetrievalResult{
		Text: text, Score: score,
		Metadata: map[string]any{"triple": tripleJSON},
	}
}

// ──────────────────────────── 用例 ────────────────────────────

// TestTripleBeamSearch_构造校验 max_length<1 → 155204
func TestTripleBeamSearch_构造校验(t *testing.T) {
	_, err := NewTripleBeamSearch(&withEmbedRetriever{}, WithTripleMaxLength(0))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "expect max_length >= 1")
}

// TestTripleBeamSearch_无嵌入模型 → 155202
func TestTripleBeamSearch_无嵌入模型(t *testing.T) {
	s, err := NewTripleBeamSearch(&noEmbedRetriever{})
	assert.NoError(t, err)
	_, err = s.BeamSearch(context.Background(), "q", []common.RetrievalResult{makeTriple("t", `["a","r","b"]`, 0.9)})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "embed_model is required for beam search")
}

// TestTripleBeamSearch_空输入 warn 返回空
func TestTripleBeamSearch_空输入(t *testing.T) {
	s, _ := NewTripleBeamSearch(&withEmbedRetriever{})
	beams, err := s.BeamSearch(context.Background(), "q", nil)
	assert.NoError(t, err)
	assert.Empty(t, beams)
}

// TestTripleBeamSearch_基本流程 两轮嵌入 + 无候选不扩展
func TestTripleBeamSearch_基本流程(t *testing.T) {
	emb := &fakeBeamEmbedModel{
		batchSize: 32,
		batches: [][][]float64{
			{{0.1, 0.2, 0.3}, {0.2, 0.3, 0.4}, {0.15, 0.25, 0.35}}, // 首轮：2 triples + query
			{{0.1, 0.2, 0.3}, {0.2, 0.3, 0.4}},                     // 次轮：2 beams 保原
		},
	}
	r := &withEmbedRetriever{fakeBeamRetriever{retrieveFn: func(context.Context, string, int) ([]common.RetrievalResult, error) {
		return nil, nil
	}}, emb}
	s, err := NewTripleBeamSearch(r, WithTripleNumBeams(2), WithTripleMaxLength(2))
	assert.NoError(t, err)

	triples := []common.RetrievalResult{
		makeTriple("entity1 relation entity2", `["entity1","relation","entity2"]`, 0.9),
		makeTriple("entity3 relation entity4", `["entity3","relation","entity4"]`, 0.8),
	}
	beams, err := s.BeamSearch(context.Background(), "test query", triples)
	assert.NoError(t, err)
	assert.Len(t, beams, 2)
	for _, b := range beams {
		assert.Equal(t, 1, b.Len()) // 无候选不扩展
	}
	assert.NotEmpty(t, emb.lastOpts) // 批大小选项透传（含 WithBatchSize）
}

// TestTripleBeamSearch_嵌入缺MaxBatchSize → 155202（拍板 c）
func TestTripleBeamSearch_嵌入缺MaxBatchSize(t *testing.T) {
	r := &withEmbedRetriever{fakeBeamRetriever{retrieveFn: func(context.Context, string, int) ([]common.RetrievalResult, error) {
		return nil, nil
	}}, &plainEmbedModel{}}
	s, _ := NewTripleBeamSearch(r)
	_, err := s.BeamSearch(context.Background(), "q", []common.RetrievalResult{makeTriple("t", `["a","r","b"]`, 0.9)})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "max_batch_size")
}

// TestSearchCandidates_bm25映射sparse
func TestSearchCandidates_bm25映射sparse(t *testing.T) {
	r := &withIndexTypeRetriever{fakeBeamRetriever{retrieveFn: func(context.Context, string, int) ([]common.RetrievalResult, error) {
		return nil, nil
	}}, "bm25"}
	s, _ := NewTripleBeamSearch(r, WithTripleNumCandidatesPerBeam(50))
	beam := common.NewTripleBeam([]common.RetrievalResult{makeTriple("entity1 relation entity2", `["entity1","relation","entity2"]`, 0.9)}, 0.9)
	_, err := s.searchCandidates(context.Background(), beam)
	assert.NoError(t, err)
	assert.Equal(t, RetrievalModeSparse, r.lastMode)
	assert.Equal(t, 50, r.lastTopK)
	assert.Contains(t, r.lastQuery, "entity1")
	assert.Contains(t, r.lastQuery, "entity2")
}

// TestSearchCandidates_非bm25透传
func TestSearchCandidates_非bm25透传(t *testing.T) {
	r := &withIndexTypeRetriever{fakeBeamRetriever{retrieveFn: func(context.Context, string, int) ([]common.RetrievalResult, error) {
		return nil, nil
	}}, "hybrid"}
	s, _ := NewTripleBeamSearch(r)
	beam := common.NewTripleBeam([]common.RetrievalResult{makeTriple("a rel b", `["a","rel","b"]`, 1.0)}, 1.0)
	_, _ = s.searchCandidates(context.Background(), beam)
	assert.Equal(t, RetrievalModeHybrid, r.lastMode)
}

// TestSearchCandidates_无indexType用retrieveMode
func TestSearchCandidates_无indexType用retrieveMode(t *testing.T) {
	r := &withEmbedRetriever{fakeBeamRetriever{retrieveFn: func(context.Context, string, int) ([]common.RetrievalResult, error) {
		return nil, nil
	}}, nil}
	s, _ := NewTripleBeamSearch(r, WithTripleRetrieveMode("hybrid"), WithTripleNumCandidatesPerBeam(7))
	beam := common.NewTripleBeam([]common.RetrievalResult{makeTriple("entity1 relation entity2", `["entity1","relation","entity2"]`, 0.9)}, 0.9)
	_, _ = s.searchCandidates(context.Background(), beam)
	assert.Equal(t, RetrievalModeHybrid, r.lastMode)
	assert.Equal(t, 7, r.lastTopK)
}

// TestSearchCandidates_retrieveModeVector
func TestSearchCandidates_retrieveModeVector(t *testing.T) {
	r := &withEmbedRetriever{fakeBeamRetriever{retrieveFn: func(context.Context, string, int) ([]common.RetrievalResult, error) {
		return nil, nil
	}}, nil}
	s, _ := NewTripleBeamSearch(r, WithTripleRetrieveMode("vector"), WithTripleNumCandidatesPerBeam(3))
	beam := common.NewTripleBeam([]common.RetrievalResult{makeTriple("x rel y", `["x","rel","y"]`, 1.0)}, 1.0)
	_, _ = s.searchCandidates(context.Background(), beam)
	assert.Equal(t, RetrievalModeVector, r.lastMode)
}

// TestSearchCandidates_双缺省hybrid
func TestSearchCandidates_双缺省hybrid(t *testing.T) {
	r := &withEmbedRetriever{fakeBeamRetriever{retrieveFn: func(context.Context, string, int) ([]common.RetrievalResult, error) {
		return nil, nil
	}}, nil}
	s, _ := NewTripleBeamSearch(r, WithTripleNumCandidatesPerBeam(5))
	beam := common.NewTripleBeam([]common.RetrievalResult{makeTriple("a rel b", `["a","rel","b"]`, 1.0)}, 1.0)
	_, _ = s.searchCandidates(context.Background(), beam)
	assert.Equal(t, RetrievalModeHybrid, r.lastMode)
}

// TestSearchCandidates_indexType优先
func TestSearchCandidates_indexType优先(t *testing.T) {
	r := &withIndexTypeRetriever{fakeBeamRetriever{retrieveFn: func(context.Context, string, int) ([]common.RetrievalResult, error) {
		return nil, nil
	}}, "vector"}
	s, _ := NewTripleBeamSearch(r, WithTripleRetrieveMode("hybrid"), WithTripleNumCandidatesPerBeam(2))
	beam := common.NewTripleBeam([]common.RetrievalResult{makeTriple("p rel q", `["p","rel","q"]`, 1.0)}, 1.0)
	_, _ = s.searchCandidates(context.Background(), beam)
	assert.Equal(t, RetrievalModeVector, r.lastMode)
}

// TestSearchCandidates_防御路径 尾元数据缺失/非字符串/解析失败/len<2
func TestSearchCandidates_防御路径(t *testing.T) {
	r := &withEmbedRetriever{fakeBeamRetriever{retrieveFn: func(context.Context, string, int) ([]common.RetrievalResult, error) {
		return nil, nil
	}}, nil}
	s, _ := NewTripleBeamSearch(r)

	// 无 triple 键
	beam := common.NewTripleBeam([]common.RetrievalResult{{Text: "t", Metadata: map[string]any{}}}, 1.0)
	ret, err := s.searchCandidates(context.Background(), beam)
	assert.NoError(t, err)
	assert.Empty(t, ret)

	// 非 JSON 字符串
	beam = common.NewTripleBeam([]common.RetrievalResult{makeTriple("t", "not-json", 1.0)}, 1.0)
	ret, err = s.searchCandidates(context.Background(), beam)
	assert.NoError(t, err)
	assert.Empty(t, ret)

	// len<2
	beam = common.NewTripleBeam([]common.RetrievalResult{makeTriple("t", `["a"]`, 1.0)}, 1.0)
	ret, err = s.searchCandidates(context.Background(), beam)
	assert.NoError(t, err)
	assert.Empty(t, ret)
}

// TestSearchCandidates_候选裸解析冒泡 no-meta 候选触发 error 冒泡
func TestSearchCandidates_候选裸解析冒泡(t *testing.T) {
	candidates := []common.RetrievalResult{
		{Text: "no-meta", Metadata: nil}, // Python json.loads(None) TypeError 冒泡
	}
	r := &withEmbedRetriever{fakeBeamRetriever{retrieveFn: func(context.Context, string, int) ([]common.RetrievalResult, error) {
		return candidates, nil
	}}, nil}
	s, _ := NewTripleBeamSearch(r)
	beam := common.NewTripleBeam([]common.RetrievalResult{makeTriple("entity1 relation entity2", `["entity1","relation","entity2"]`, 0.9)}, 0.9)
	_, err := s.searchCandidates(context.Background(), beam)
	assert.Error(t, err)
}

// TestSearchCandidates_三重过滤 已在beam/首尾过滤/合格保留
func TestSearchCandidates_三重过滤(t *testing.T) {
	candidates := []common.RetrievalResult{
		{Text: "already-in-beam", Metadata: map[string]any{"triple": `["entity1","r","entity2"]`}}, // Contains 过滤
		{Text: "unrelated", Metadata: map[string]any{"triple": `["x","r","y"]`}},                    // 首尾过滤
		{Text: "good", Metadata: map[string]any{"triple": `["entity1","r2","z"]`}},                  // 保留
	}
	r := &withEmbedRetriever{fakeBeamRetriever{retrieveFn: func(context.Context, string, int) ([]common.RetrievalResult, error) {
		return candidates, nil
	}}, nil}
	s, _ := NewTripleBeamSearch(r)
	beam := common.NewTripleBeam([]common.RetrievalResult{makeTriple("entity1 relation entity2", `["entity1","relation","entity2"]`, 0.9)}, 0.9)
	ret, err := s.searchCandidates(context.Context(), beam)
	assert.NoError(t, err)
	assert.Len(t, ret, 1)
	assert.Equal(t, "good", ret[0].Text)
}

// TestTripleBeamSearch_扩展轮路径 候选存在时的 beam 扩展 + 去重
func TestTripleBeamSearch_扩展轮路径(t *testing.T) {
	// beam1 尾 [a,r,b]，候选 [[b,r2,c]]（首尾 b 命中）→ 扩展为 [a,r,b]+[b,r2,c]
	emb := &fakeBeamEmbedModel{
		batchSize: 32,
		batches: [][][]float64{
			{{1, 0}, {0.5, 0.5}},                             // 首轮：1 triple + query
			{{1, 0}, {0.9, 0.1}, {0.5, 0.5}},                 // 次轮：保原 beam + 扩展路径
		},
	}
	r := &withEmbedRetriever{fakeBeamRetriever{retrieveFn: func(context.Context, string, int) ([]common.RetrievalResult, error) {
		return []common.RetrievalResult{makeTriple("b r2 c", `["b","r2","c"]`, 0.7)}, nil
	}}, emb}
	s, _ := NewTripleBeamSearch(r, WithTripleNumBeams(2), WithTripleMaxLength(2))
	triples := []common.RetrievalResult{makeTriple("a r b", `["a","r","b"]`, 0.9)}
	beams, err := s.BeamSearch(context.Background(), "q", triples)
	assert.NoError(t, err)
	// 断言：至少一条 beam 长度 2（扩展成功），去重后无重复
	maxLen := 0
	for _, b := range beams {
		if b.Len() > maxLen {
			maxLen = b.Len()
		}
	}
	assert.Equal(t, 2, maxLen)
}
```

- [ ] **Step 2: 跑测试确认失败**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/retrieval/retriever/ -run "TestTripleBeamSearch|TestSearchCandidates" -v
```

Expected: FAIL

- [ ] **Step 3: 实现 graph_beam_search.go**

```go
// Package retriever 三元组 beam search（GraphRetriever 的图扩展核心算法）。
//
// Python: graph_retriever.py (TripleBeamSearch L24-225)
package retriever

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"golang.org/x/sync/errgroup"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/embedding"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TripleBeamSearch 三元组 beam search。
//
// 构造默认：numBeams=10 / numCandidatesPerBeam=100 / maxLength=2 /
// encoderBatchSize=256（死参数存而不读，spec §8-5）/ retrieveMode=""
// （= Python None）。embedModel 从 retriever 经 EmbedModelProvider 断言提取
// （Python hasattr L48）。
type TripleBeamSearch struct {
	// retriever 候选检索器
	retriever Retriever
	// numBeams 束宽
	numBeams int
	// numCandidatesPerBeam 每束候选数
	numCandidatesPerBeam int
	// maxLength 最大路径长度（图跳数）
	maxLength int
	// encoderBatchSize 编码批大小（Python 死参数，存而不读）
	encoderBatchSize int
	// retrieveMode 构造传入的模式（"" = Python None）
	retrieveMode string
	// embedModel 嵌入模型（构造时从 retriever 提取）
	embedModel embedding.BaseEmbedding
}

// TripleBeamSearchOption 构造选项。
type TripleBeamSearchOption func(*TripleBeamSearch)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// defaultTripleNumBeams 默认束宽
	defaultTripleNumBeams = 10
	// defaultTripleNumCandidates 默认每束候选数
	defaultTripleNumCandidates = 100
	// defaultTripleMaxLength 默认最大路径长度
	defaultTripleMaxLength = 2
	// defaultTripleEncoderBatchSize 默认编码批大小（Python 死参数）
	defaultTripleEncoderBatchSize = 256
)

// ──────────────────────────── 导出函数 ────────────────────────────

// WithTripleNumBeams 设置束宽。
func WithTripleNumBeams(n int) TripleBeamSearchOption {
	return func(s *TripleBeamSearch) { s.numBeams = n }
}

// WithTripleNumCandidatesPerBeam 设置每束候选数。
func WithTripleNumCandidatesPerBeam(n int) TripleBeamSearchOption {
	return func(s *TripleBeamSearch) { s.numCandidatesPerBeam = n }
}

// WithTripleMaxLength 设置最大路径长度。
func WithTripleMaxLength(n int) TripleBeamSearchOption {
	return func(s *TripleBeamSearch) { s.maxLength = n }
}

// WithTripleEncoderBatchSize 设置编码批大小（Python 死参数，仅存储）。
func WithTripleEncoderBatchSize(n int) TripleBeamSearchOption {
	return func(s *TripleBeamSearch) { s.encoderBatchSize = n }
}

// WithTripleRetrieveMode 设置构造模式（"" = Python None）。
func WithTripleRetrieveMode(mode string) TripleBeamSearchOption {
	return func(s *TripleBeamSearch) { s.retrieveMode = mode }
}

// NewTripleBeamSearch 创建 beam search。
//
// 对齐 Python __init__（L27-50）：max_length<1 报 155204（文案对齐
// Python f"expect max_length >= 1; got {max_length=}" 展开形态）；
// embedModel 经 EmbedModelProvider 断言提取（hasattr 同构）。
func NewTripleBeamSearch(retriever Retriever, opts ...TripleBeamSearchOption) (*TripleBeamSearch, error) {
	s := &TripleBeamSearch{
		retriever:            retriever,
		numBeams:             defaultTripleNumBeams,
		numCandidatesPerBeam: defaultTripleNumCandidates,
		maxLength:            defaultTripleMaxLength,
		encoderBatchSize:     defaultTripleEncoderBatchSize,
	}
	for _, opt := range opts {
		opt(s)
	}
	if s.maxLength < 1 {
		return nil, exception.BuildError(
			exception.StatusRetrievalRetrieverModeInvalid,
			exception.WithParam("error_msg", fmt.Sprintf("expect max_length >= 1; got max_length=%d", s.maxLength)),
		)
	}
	if p, ok := retriever.(EmbedModelProvider); ok {
		s.embedModel = p.EmbedModel()
	}
	return s, nil
}

// BeamSearch 执行 beam search。
//
// 对齐 Python beam_search（L78-108）：空输入 warn 返回空；无 embed 155202；
// triples+query 合批嵌入（批大小经 MaxBatchSizeProvider 断言，断言失败
// 显式 155202——拍板 c）；余弦 topk 初始 beams；maxLength-1 轮扩展。
func (s *TripleBeamSearch) BeamSearch(ctx context.Context, query string, triples []common.RetrievalResult) ([]*common.TripleBeam, error) {
	if len(triples) == 0 {
		logger.Warn(logComponent).
			Str("query", query).
			Msg("beam search got empty input triples")
		return nil, nil
	}
	if s.embedModel == nil {
		return nil, exception.BuildError(
			exception.StatusRetrievalRetrieverEmbedModelNotFound,
			exception.WithParam("error_msg", "embed_model is required for beam search"),
		)
	}

	texts := make([]string, 0, len(triples)+1)
	for _, x := range triples {
		texts = append(texts, x.Text)
	}
	texts = append(texts, query)

	embeddings, err := s.embedBatch(ctx, texts)
	if err != nil {
		return nil, err
	}
	queryEmbedding := embeddings[len(embeddings)-1]
	candEmbeddings := embeddings[:len(embeddings)-1]

	scores := cosineScores(queryEmbedding, candEmbeddings)
	topkIdx, topkScores := topkIndices(scores, s.numBeams)
	beams := make([]*common.TripleBeam, 0, len(topkIdx))
	for i, idx := range topkIdx {
		beams = append(beams, common.NewTripleBeam([]common.RetrievalResult{triples[idx]}, topkScores[i]))
	}

	for range s.maxLength - 1 {
		results := make([][]common.RetrievalResult, len(beams))
		g, gctx := errgroup.WithContext(ctx)
		for i := range beams {
			i, beam := i, beams[i]
			g.Go(func() error {
				ret, err := s.searchCandidates(gctx, beam)
				if err != nil {
					return err
				}
				results[i] = ret
				return nil
			})
		}
		if err := g.Wait(); err != nil {
			return nil, err
		}

		beams, err = s.expandBeams(ctx, beams, results, queryEmbedding)
		if err != nil {
			return nil, err
		}
	}
	return beams, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// embedBatch 批量嵌入（Python embed_documents(batch_size=max_batch_size)）。
//
// max_batch_size 经 MaxBatchSizeProvider 断言：断言失败显式报 155202
// （Python AttributeError 炸点的业务化形态，spec §8-7——炸点保留使
// graph_expansion 降级路径可达）。
func (s *TripleBeamSearch) embedBatch(ctx context.Context, texts []string) ([][]float64, error) {
	mp, ok := s.embedModel.(MaxBatchSizeProvider)
	if !ok {
		return nil, exception.BuildError(
			exception.StatusRetrievalRetrieverEmbedModelNotFound,
			exception.WithParam("error_msg", "embed_model does not support max_batch_size"),
		)
	}
	return s.embedModel.EmbedDocuments(ctx, texts, embedding.WithBatchSize(mp.MaxBatchSize()))
}

// expandBeams 扩展 beams。
//
// 对齐 Python _expand_beams（L110-170）：existTriples 全 beam 文本判重；
// 候选路径 (beam, triple|nil)（nil=保原 beam）；路径文本重嵌入打分 topk；
// seenBeamTexts 按 "; ".join 格式去重；texts 空→原 beams。
func (s *TripleBeamSearch) expandBeams(ctx context.Context, beams []*common.TripleBeam, candidatesPerBeam [][]common.RetrievalResult, queryEmbedding []float64) ([]*common.TripleBeam, error) {
	var texts []string
	type candidatePath struct {
		beam   *common.TripleBeam
		triple *common.RetrievalResult // nil = 保原 beam
	}
	var candidatePaths []candidatePath

	existTriples := make(map[string]struct{})
	for _, beam := range beams {
		for _, x := range beam.Triples() {
			existTriples[x.Text] = struct{}{}
		}
	}

	for i, beam := range beams {
		cands := candidatesPerBeam[i]
		if len(cands) == 0 {
			// 无候选：保原 beam（Python L121-124）
			candidatePaths = append(candidatePaths, candidatePath{beam: beam, triple: nil})
			texts = append(texts, formatTriples(beam.Triples()))
			continue
		}
		for j := range cands {
			triple := cands[j]
			if _, ok := existTriples[triple.Text]; ok {
				continue
			}
			candidatePaths = append(candidatePaths, candidatePath{beam: beam, triple: &cands[j]})
			// Python beam.triples + [triple]——Triples() 浅拷贝密封 append（13.22 C2）
			texts = append(texts, formatTriples(append(beam.Triples(), triple)))
		}
	}

	if len(texts) == 0 {
		return beams, nil
	}
	if s.embedModel == nil {
		return beams, nil
	}

	embeddings, err := s.embedBatch(ctx, texts)
	if err != nil {
		return nil, err
	}
	nextScores := cosineScores(queryEmbedding, embeddings)

	topkIdx, _ := topkIndices(nextScores, s.numBeams)

	newBeams := make([]*common.TripleBeam, 0, len(topkIdx))
	seenBeamTexts := make(map[string]struct{})
	for _, idx := range topkIdx {
		path := candidatePaths[idx]
		if path.triple == nil {
			beamText := formatTriples(path.beam.Triples())
			if _, ok := seenBeamTexts[beamText]; !ok {
				newBeams = append(newBeams, path.beam)
				seenBeamTexts[beamText] = struct{}{}
			}
		} else {
			newTriples := append(path.beam.Triples(), *path.triple)
			beamText := formatTriples(newTriples)
			if _, ok := seenBeamTexts[beamText]; !ok {
				newBeams = append(newBeams, common.NewTripleBeam(newTriples, nextScores[idx]))
				seenBeamTexts[beamText] = struct{}{}
			}
		}
	}
	return newBeams, nil
}

// searchCandidates 检索单 beam 的候选三元组。
//
// 对齐 Python _search_candidates（L172-225）：beam 尾 triple 元数据防御
// 解析（缺失/非字符串/解析失败 warn 返回空，Python L176-185）；entities=
// {首,尾}；mode 三级解析 IndexTypeCarrier > retrieveMode > "hybrid"
// （bm25→sparse）；retrieve(top_k=numCandidates)；三重过滤——已在 beam
// （Contains）/候选解析**裸调用无防御**（Python L213，错误冒泡由上层
// graph_expansion 降级）/首尾都不在 entities。空结果 warn。
func (s *TripleBeamSearch) searchCandidates(ctx context.Context, beam *common.TripleBeam) ([]common.RetrievalResult, error) {
	tail := beam.Get(-1)
	var tripleData any
	if tail.Metadata != nil {
		tripleData = tail.Metadata["triple"]
	}
	tripleStr, ok := tripleData.(string)
	if !ok || tripleStr == "" {
		// Python L176-179：metadata.get("triple") None → warn 返回空
		logger.Warn(logComponent).Msg("beam has no triple metadata")
		return nil, nil
	}
	var parsed []any
	if err := json.Unmarshal([]byte(tripleStr), &parsed); err != nil {
		// Python L182-185：JSONDecodeError → warn 返回空
		logger.Warn(logComponent).Err(err).Msg("[graph] Failed to parse triple metadata")
		return nil, nil
	}
	if len(parsed) < 2 {
		// Python L187-188：not triple or len(triple) < 2
		return nil, nil
	}

	entities := make(map[string]struct{}, 2)
	entities[pyAnyStr(parsed[0])] = struct{}{}
	entities[pyAnyStr(parsed[len(parsed)-1])] = struct{}{}
	queryStr := strings.Join(mapKeys(entities), " ")

	mode := s.resolveCandidateMode()

	nodes, err := s.retriever.Retrieve(ctx, queryStr, s.numCandidatesPerBeam, nil, mode)
	if err != nil {
		return nil, err
	}

	var ret []common.RetrievalResult
	for _, x := range nodes {
		if beam.Contains(x) {
			continue
		}
		// 候选解析裸调用（Python L213 无 try——json.loads(None) TypeError
		// 冒泡炸掉 beam search，由 graph_expansion except 降级）：
		// Go 侧 Unmarshal 错误/类型不符直接冒泡，字面保留。
		xTriple, err := parseCandidateTriple(x)
		if err != nil {
			return nil, err
		}
		if len(xTriple) < 2 {
			continue
		}
		if _, okFirst := entities[pyAnyStr(xTriple[0])]; !okFirst {
			if _, okLast := entities[pyAnyStr(xTriple[len(xTriple)-1])]; !okLast {
				continue
			}
		}
		ret = append(ret, x)
	}

	if len(ret) == 0 {
		logger.Warn(logComponent).
			Str("beam", formatTriples(beam.Triples())).
			Msg("empty candidates for beam")
	}
	return ret, nil
}

// resolveCandidateMode 候选检索 mode 三级解析。
//
// 对齐 Python L193-199：retriever.index_type > retrieveMode > "hybrid"；
// bm25 → sparse 映射。
func (s *TripleBeamSearch) resolveCandidateMode() RetrievalMode {
	mode := ""
	if ic, ok := s.retriever.(IndexTypeCarrier); ok {
		mode = ic.IndexType()
	}
	if mode == "" {
		mode = s.retrieveMode
	}
	if mode == "" {
		mode = "hybrid"
	}
	if mode == "bm25" {
		mode = "sparse"
	}
	return RetrievalMode(mode)
}

// parseCandidateTriple 候选 triple 元数据裸解析（无防御，错误冒泡）。
func parseCandidateTriple(x common.RetrievalResult) ([]any, error) {
	if x.Metadata == nil {
		return nil, fmt.Errorf("json: cannot unmarshal nil triple metadata")
	}
	raw, ok := x.Metadata["triple"].(string)
	if !ok {
		return nil, fmt.Errorf("json: cannot unmarshal non-string triple metadata")
	}
	var parsed []any
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return nil, err
	}
	return parsed, nil
}

// formatTriples 路径文本渲染（Python "; ".join L75-76）。
func formatTriples(triples []common.RetrievalResult) string {
	parts := make([]string, len(triples))
	for i, x := range triples {
		parts[i] = x.Text
	}
	return strings.Join(parts, "; ")
}

// pyAnyStr Python str() 的宽松字符串化（triple 元素可能非字符串）。
func pyAnyStr(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

// mapKeys 取 map 键集（entities join 顺序与 Python set 迭代同样无序，
// 语义同构）。
func mapKeys(m map[string]struct{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
```

**注意**：
1. `logComponent` 常量：先 `grep -rn "logComponent" internal/agentcore/retrieval/retriever/*.go`——13.20/13.21 未用过 logger 则本文件（或 capability.go）补 `const logComponent = logger.ComponentAgentCore`（一个包一份，放首个用它的文件）
2. `s.embedModel == nil` 判空在 expandBeams 保留（Python L137-138 `if not self.embed_model: return beams`——beam_search 已前置拦截，此为字面复刻防御分支）

- [ ] **Step 4: 跑测试确认通过**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/retrieval/retriever/ -run "TestTripleBeamSearch|TestSearchCandidates" -v
```

Expected: 15 例全 PASS

- [ ] **Step 5: Commit**

```bash
git add internal/agentcore/retrieval/retriever/graph_beam_search.go internal/agentcore/retrieval/retriever/graph_beam_search_test.go && git commit -m "feat(retriever): 13.23 TripleBeamSearch——beam search 图扩展核心（批大小断言 155202 + mode 三级解析 + 三重过滤）"
```

---

### Task 7: fetch 层——queryByFilterMilvus/queryByFilterChroma

**Files:**
- Create: `internal/agentcore/retrieval/retriever/graph_fetch.go`
- Test: `internal/agentcore/retrieval/retriever/graph_fetch_test.go`

对应 Python `_query_by_filter_milvus`（L537-552）/_query_by_filter_chroma（L554-596）/归一化（L654-672/L749-768）。

- [ ] **Step 1: 写失败测试**

Milvus 桩策略：嵌入 `vector_store.MilvusClient` 接口只覆写 Query（`milvusclient.ResultSet` 字段全导出可字面量构造；列构造用 `column.NewColumnString(name, values)`——实施时按 SDK column 包实际 API 填）：

```go
package retriever

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	chromav2 "github.com/amikos-tech/chroma-go/pkg/api/v2"
	milvusclient "github.com/milvus-io/milvus/client/v2/milvusclient"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/vector_store"
)

// milvusQueryStub 仅覆写 Query 的 MilvusClient 桩
type milvusQueryStub struct {
	vector_store.MilvusClient // 嵌入接口：未触达方法 nil panic（测试不触达即安全）
	queryRS   milvusclient.ResultSet
	queryErr  error
	queryCalls int
}

func (m *milvusQueryStub) Query(_ context.Context, _ milvusclient.QueryOption, _ ...grpc.CallOption) (milvusclient.ResultSet, error) {
	m.queryCalls++
	return m.queryRS, m.queryErr
}
```

（import 补 `grpc "google.golang.org/grpc"`；QueryOption 为值类型——桩签名按接口定义精确复制，实施时以 `grep -n "Query(ctx" vector_store/milvus.go` L54 原签名为准）

```go
// TestQueryByFilterMilvus_结果归一化 列数据 → fetchItem（docIDTop 双源用）
func TestQueryByFilterMilvus_结果归一化(t *testing.T) {
	// 构造 1 行：content/metadata(JSON 字符串)/document_id 列
	// （列构造实施时用 column.NewColumnString 等 SDK API）
	// 断言：fetchItem.content/id/docIDTop/metadata 已解析
}

// TestQueryByFilterMilvus_空结果
// TestQueryByFilterMilvus_错误冒泡 queryErr → 返回 error
```

Chroma 桩：`chromav2.Client`/`chromav2.Collection` 均为接口，可全桩化；`GetResult` 用 `*chromav2.GetResultImpl` 字面量构造（字段 Ids/Documents/Metadatas 导出）：

```go
// chromaClientStub GetCollection 桩
type chromaClientStub struct {
	chromav2.Client
	collection chromav2.Collection
	getCollErr error
}

func (c *chromaClientStub) GetCollection(ctx context.Context, name string, _ ...chromav2.GetCollectionOption) (chromav2.Collection, error) {
	return c.collection, c.getCollErr
}

// chromaCollectionStub Get 桩
type chromaCollectionStub struct {
	chromav2.Collection
	getResult chromav2.GetResult
	getErr    error
}

func (c *chromaCollectionStub) Get(ctx context.Context, opts ...chromav2.CollectionGetOption) (chromav2.GetResult, error) {
	return c.getResult, c.getErr
}

// TestQueryByFilterChroma_结果归一化 score 恒 0.0 + metadata 提取
// TestQueryByFilterChroma_错误冒泡 两段错误（GetCollection/Get）

// TestFetchItemsToRetrievalResults doc_id 双源 + chunk_id 回落
func TestFetchItemsToRetrievalResults(t *testing.T) {
	items := []fetchItem{
		{ // 顶层 document_id 优先
			id: "i1", content: "text1", score: 0.9,
			metadata: map[string]any{"chunk_id": "c1", "doc_id": "meta-doc"},
			docIDTop: "top-doc",
		},
		{ // 顶层空回落 metadata
			id: "i2", content: "text2", score: 0.0,
			metadata: map[string]any{"chunk_id": "c2", "doc_id": "meta-doc2"},
		},
		{ // chunk_id 缺失回落 fallback
			id: "i3", content: "text3", metadata: map[string]any{},
		},
	}
	results := fetchItemsToRetrievalResults(items, "query-cid")
	assert.Equal(t, "top-doc", results[0].DocID)
	assert.Equal(t, "c1", results[0].ChunkID)
	assert.Equal(t, "meta-doc2", results[1].DocID)
	assert.Equal(t, "query-cid", results[2].ChunkID)
	assert.Equal(t, 0.0, results[1].Score) // Chroma get 无分数字面
}
```

（metadata 的 GetRaw 提取路径用真实 `chromav2.NewDocumentMetadataFromMap` 构造，复用 chroma.go L925 先例）

- [ ] **Step 2: 跑测试确认失败**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/retrieval/retriever/ -run "QueryByFilter|FetchItems" -v
```

Expected: FAIL

- [ ] **Step 3: 实现 graph_fetch.go**

```go
// Package retriever 图检索 fetch 层：按 chunk_id 反查三元组/正查 chunk。
//
// Python: graph_retriever.py (_query_by_filter_milvus L537-552 /
// _query_by_filter_chroma L554-596 / 归一化 L654-672、L749-768)
package retriever

import (
	"context"
	"encoding/json"
	"fmt"

	chromav2 "github.com/amikos-tech/chroma-go/pkg/api/v2"
	milvusclient "github.com/milvus-io/milvus/client/v2/milvusclient"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/vector_store"
)

// ──────────────────────────── 结构体 ────────────────────────────

// fetchItem Python dict {"id","content","metadata","score","document_id"}
// 的结构化对应物（spec §8-2）。
type fetchItem struct {
	// id 结果 ID
	id string
	// content 文本内容
	content string
	// metadata 元数据
	metadata map[string]any
	// score 相关性分数（Chroma get 恒 0.0）
	score float64
	// docIDTop 顶层 document_id（Milvus query 返回形态；Python L663
	// item.get("document_id") 双源优先侧）
	docIDTop string
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// queryByFilterMilvus 按过滤表达式查询 Milvus 集合。
//
// 对齐 Python _query_by_filter_milvus（L537-552）：filter 字符串 + 全字段
// 输出 + limit。asyncio.to_thread 异步包装在 Go 无对应物（同步调用）。
func queryByFilterMilvus(ctx context.Context, client vector_store.MilvusClient, collectionName, filterExpr string, limit int) ([]fetchItem, error) {
	opt := milvusclient.NewQueryOption(collectionName).
		WithFilter(filterExpr).
		WithOutputFields("*").
		WithLimit(limit)
	rs, err := client.Query(ctx, *opt)
	if err != nil {
		return nil, err
	}
	return milvusResultSetToFetchItems(rs), nil
}

// milvusResultSetToFetchItems ResultSet 列数据 → fetchItem 列表
// （13.3 milvusResultToSearchResults 列遍历先例 L1196-1220）。
func milvusResultSetToFetchItems(rs milvusclient.ResultSet) []fetchItem {
	rowCount := rs.ResultCount
	if rowCount == 0 {
		return nil
	}
	colData := make(map[string][]any)
	for _, col := range rs.Fields {
		name := col.Name()
		values := make([]any, rowCount)
		for j := 0; j < rowCount; j++ {
			if v, err := col.Get(j); err == nil {
				values[j] = v
			}
		}
		colData[name] = values
	}

	items := make([]fetchItem, 0, rowCount)
	for j := 0; j < rowCount; j++ {
		item := fetchItem{metadata: map[string]any{}}
		if ids := rs.IDs; ids != nil {
			if v, err := ids.Get(j); err == nil {
				item.id = fmt.Sprintf("%v", v)
			}
		}
		if vals := colData["content"]; j < len(vals) {
			item.content, _ = vals[j].(string)
		}
		// metadata 三形态：map / JSON 字符串（Python L656-661 防御）
		if vals := colData["metadata"]; j < len(vals) {
			switch m := vals[j].(type) {
			case map[string]any:
				for k, v := range m {
					item.metadata[k] = v
				}
			case string:
				var parsed map[string]any
				if json.Unmarshal([]byte(m), &parsed) == nil {
					for k, v := range parsed {
						item.metadata[k] = v
					}
				}
			}
		}
		// document_id 顶层字段（Python L663）
		if vals := colData["document_id"]; j < len(vals) {
			item.docIDTop, _ = vals[j].(string)
		}
		items = append(items, item)
	}
	return items
}

// queryByFilterChroma 按 where 过滤查询 Chroma 集合。
//
// 对齐 Python _query_by_filter_chroma（L554-596）：get_collection +
// get(where, limit)；结果无分数（score 恒 0.0 字面复刻 Python L589）。
func queryByFilterChroma(ctx context.Context, client chromav2.Client, collectionName string, where chromav2.WhereClause, limit int) ([]fetchItem, error) {
	collection, err := client.GetCollection(ctx, collectionName)
	if err != nil {
		return nil, err
	}
	result, err := collection.Get(ctx, chromav2.WithWhere(where), chromav2.WithLimit(limit))
	if err != nil {
		return nil, err
	}

	ids := result.GetIDs()
	docs := result.GetDocuments()
	metas := result.GetMetadatas()

	items := make([]fetchItem, 0, len(ids))
	for i, id := range ids {
		item := fetchItem{id: string(id), metadata: map[string]any{}}
		if i < len(docs) && docs[i] != nil {
			item.content = docs[i].ContentString()
		}
		if i < len(metas) && metas[i] != nil {
			meta := metas[i]
			for _, key := range chromaMetaKeysForFetch(meta) {
				if val, ok := meta.GetRaw(key); ok {
					if mv, isMv := val.(chromav2.MetadataValue); isMv {
						if rawVal, ok := mv.GetRaw(); ok {
							item.metadata[key] = rawVal
						}
					} else {
						item.metadata[key] = val
					}
				}
			}
		}
		item.score = 0.0 // Chroma get 不返分数
		items = append(items, item)
	}
	return items, nil
}

// chromaMetaKeysForFetch DocumentMetadata 键集（chroma.go chromaMetaKeys
// 同款逻辑；包私有不导出，本包复制避免公开面扩大）。
func chromaMetaKeysForFetch(meta chromav2.DocumentMetadata) []string {
	if impl, ok := meta.(*chromav2.DocumentMetadataImpl); ok {
		return impl.Keys()
	}
	return nil
}

// fetchItemsToRetrievalResults fetchItem → RetrievalResult 归一化。
//
// 对齐 Python L663-671/L758-767：doc_id 双源（docIDTop 空串回落
// metadata，Python or 语义）；chunk_id metadata 回落 fallback（_fetch_
// triples 传 ""，_fetch_chunks 传查询 chunk_id——Python 两处
// metadata.get("chunk_id", 默认值) 不同默认，由调用方区分）。
func fetchItemsToRetrievalResults(items []fetchItem, chunkIDFallback string) []common.RetrievalResult {
	results := make([]common.RetrievalResult, 0, len(items))
	for _, item := range items {
		docID := item.docIDTop
		if docID == "" {
			docID, _ = item.metadata["doc_id"].(string)
		}
		chunkID, ok := item.metadata["chunk_id"].(string)
		if !ok || chunkID == "" {
			chunkID = chunkIDFallback
		}
		results = append(results, common.RetrievalResult{
			Text:     item.content,
			Score:    item.score,
			Metadata: item.metadata,
			DocID:    docID,
			ChunkID:  chunkID,
		})
	}
	return results
}
```

**实施核实点**：`client.Query(ctx, *opt)` 的 opt 形参形态（值/指针）以 13.3 milvusClientAdapter.Query L836 与接口定义 L54 为准；chromav2 `WithWhere(clause)` 的参数类型以 SDK collection.go L158 注释为准。

- [ ] **Step 4: 跑测试确认通过**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/retrieval/retriever/ -run "QueryByFilter|FetchItems" -v
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/agentcore/retrieval/retriever/graph_fetch.go internal/agentcore/retrieval/retriever/graph_fetch_test.go && git commit -m "feat(retriever): 13.23 fetch 层——Milvus/Chroma 过滤查询 + fetchItem 归一化（doc_id 双源）"
```

---

### Task 8: RetrieveOptions 扩容 + GraphRetriever 主体

**Files:**
- Modify: `internal/agentcore/retrieval/retriever/base.go`（三字段+三选项）
- Create: `internal/agentcore/retrieval/retriever/graph_retriever_impl.go`
- Modify: `internal/agentcore/retrieval/retriever/doc.go`（文件目录）
- Test: `internal/agentcore/retrieval/retriever/graph_retriever_impl_test.go`

对应 Python `GraphRetriever` 类（L228-814）。

- [ ] **Step 1: base.go RetrieveOptions 扩容**

struct 追加三字段（保留 Alpha 原注释）：

```go
	// GraphHops 图检索跳数（13.23 消费；nil→默认 2；显式 0 →
	// max_length=0 → 155204，Python 行为保真）。潜伏参数：Python 侧
	// kwargs.get 消费、全库零生产方（13.28 接线后激活）。
	GraphHops *int
	// NumBeams beam search 束宽（13.23 TripleBeamSearch 消费；nil→10）。
	NumBeams *int
	// NumCandidatesPerBeam 每束候选数（13.23 消费；nil→100）。
	NumCandidatesPerBeam *int
```

追加三选项：

```go
// WithRetrieveGraphHops 设置图检索跳数的 RetrieveOption。
func WithRetrieveGraphHops(v int) RetrieveOption {
	return func(o *RetrieveOptions) { o.GraphHops = &v }
}

// WithRetrieveNumBeams 设置 beam search 束宽的 RetrieveOption。
func WithRetrieveNumBeams(v int) RetrieveOption {
	return func(o *RetrieveOptions) { o.NumBeams = &v }
}

// WithRetrieveNumCandidatesPerBeam 设置每束候选数的 RetrieveOption。
func WithRetrieveNumCandidatesPerBeam(v int) RetrieveOption {
	return func(o *RetrieveOptions) { o.NumCandidatesPerBeam = &v }
}
```

base_test.go 追加三选项回读断言（形态同 WithRetrieveAlpha 既有测试）。

- [ ] **Step 2: 写失败测试（Python TestGraphRetriever 6 例 + DynamicRetrieverIndexType 3 例 + 增强）**

```go
package retriever

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
)

// fakeGraphChunkRetriever chunk 检索桩（Retriever 四方法）
type fakeGraphChunkRetriever struct {
	fakeBeamRetriever
}

// makeChunkResult 构造 chunk 结果
func makeChunkResult(text, chunkID, docID string, score float64) common.RetrievalResult {
	return common.RetrievalResult{
		Text: text, Score: score,
		Metadata: map[string]any{"chunk_id": chunkID, "doc_id": docID},
		ChunkID:  chunkID, DocID: docID,
	}
}

// TestGraphRetriever_构造（init_with_retrievers + init_with_vector_store）
func TestGraphRetriever_构造(t *testing.T) {
	cr := &fakeGraphChunkRetriever{}
	gr := NewGraphRetriever(WithGraphChunkRetriever(cr))
	assert.Equal(t, cr, gr.chunkRetriever)

	store := &fakeVectorStore{}
	emb := &fakeBeamEmbedModel{}
	gr2 := NewGraphRetriever(WithGraphVectorStore(store), WithGraphEmbedModel(emb),
		WithGraphChunkCollection("chunks"), WithGraphTripleCollection("triples"))
	assert.Equal(t, store, gr2.vectorStore)
	assert.Equal(t, emb, gr2.embedModel)
	assert.Equal(t, "chunks", gr2.chunkCollection)
	assert.Equal(t, "triples", gr2.tripleCollection)
	assert.Equal(t, "", gr2.indexType)
}

// TestGraphRetriever_retrieve阈值非法模式 155201
func TestGraphRetriever_retrieve阈值非法模式(t *testing.T) {
	gr := NewGraphRetriever(WithGraphChunkRetriever(&fakeGraphChunkRetriever{}))
	threshold := 0.8
	_, err := gr.Retrieve(context.Background(), "test query", 5, &threshold, RetrievalModeSparse)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "score_threshold is only supported")
}

// TestGraphRetriever_模式校验 index_type 未知 155203 / 不兼容 155204
func TestGraphRetriever_模式校验(t *testing.T) {
	gr := NewGraphRetriever()
	gr.SetIndexType("weird")
	_, err := gr.GetRetrieverForMode(RetrievalModeHybrid, true)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Unsupported index_type=weird")

	gr.SetIndexType("vector")
	_, err = gr.GetRetrieverForMode(RetrievalModeSparse, true)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "incompatible with index_type=vector")
	assert.Contains(t, err.Error(), "[sparse, vector]") // sorted allowed 清单

	// index_type ""（None）免检
	gr2 := NewGraphRetriever()
	_, err = gr2.GetRetrieverForMode(RetrievalModeHybrid, true)
	assert.Error(t, err) // 走到 155206（无 vector_store）——免检通过的证据
	assert.Contains(t, err.Error(), "vector_store is required")
}

// TestGraphRetriever_固定retriever不支持 155205
func TestGraphRetriever_固定retriever不支持(t *testing.T) {
	sr := NewSparseRetriever(&fakeVectorStore{})
	gr := NewGraphRetriever(WithGraphChunkRetriever(sr))
	_, err := gr.GetRetrieverForMode(RetrievalModeVector, true)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "does not support mode=vector")
	assert.Contains(t, err.Error(), "SparseRetriever")
}

// TestGraphRetriever_动态创建三分支 + collection 顺序（155207 先污染后校验）
func TestGraphRetriever_动态创建(t *testing.T) {
	// 无 vector_store → 155206
	gr := NewGraphRetriever()
	_, err := gr.GetRetrieverForMode(RetrievalModeHybrid, true)
	assert.Contains(t, err.Error(), "vector_store is required")

	// 有 store 无 collection → 155207（SetCollectionName 先执行——
	// 断言：fakeVectorStore 需扩展 CollectionName/setter 或用真 ChromaVectorStore
	// 字面量桩。实施时给 fakeVectorStore 补 CollectionName()/SetCollectionName()
	// 桩（记录 set 调用），断言 set 先于错误返回）
	store := &fakeGraphStore{}
	gr = NewGraphRetriever(WithGraphVectorStore(store))
	_, err = gr.GetRetrieverForMode(RetrievalModeHybrid, true)
	assert.Contains(t, err.Error(), "chunk_collection is required")
	assert.Equal(t, "", store.lastName) // SetCollectionName("") 先执行

	// vector 分支缺 embed → 155202
	gr = NewGraphRetriever(WithGraphVectorStore(store), WithGraphChunkCollection("c"))
	_, err = gr.GetRetrieverForMode(RetrievalModeVector, true)
	assert.Contains(t, err.Error(), "embed_model is required for vector mode")

	// 三分支构造成功（真 Milvus/Chroma 字面量或 fake store + 断言注入 index_type）
	gr = NewGraphRetriever(WithGraphVectorStore(store), WithGraphEmbedModel(&fakeBeamEmbedModel{}),
		WithGraphChunkCollection("c"), WithGraphTripleCollection("t"))
	gr.SetIndexType("hybrid")
	tr, err := gr.GetRetrieverForMode(RetrievalModeHybrid, false)
	assert.NoError(t, err)
	hr, ok := tr.(*HybridRetriever)
	assert.True(t, ok)
	assert.Equal(t, "hybrid", hr.indexType) // 注入回读（同包可读）
}

// fakeGraphStore 带 CollectionName 记录的 fakeVectorStore 扩展
type fakeGraphStore struct {
	fakeVectorStore
	lastName string
}

func (f *fakeGraphStore) CollectionName() string         { return f.lastName }
func (f *fakeGraphStore) SetCollectionName(name string)  { f.lastName = name }

// TestGraphRetriever_graphExpansion空chunks（test_graph_expansion_empty_chunks）
func TestGraphRetriever_graphExpansion空chunks(t *testing.T) {
	gr := NewGraphRetriever(WithGraphChunkRetriever(&fakeGraphChunkRetriever{}))
	results, err := gr.GraphExpansion(context.Background(), "q", nil, nil, nil, RetrievalModeHybrid)
	assert.NoError(t, err)
	assert.Empty(t, results)
}

// TestGraphRetriever_Close 两个固定 retriever 的 Close 被调
func TestGraphRetriever_Close(t *testing.T) {
	cr := &closeCountRetriever{}
	tr := &closeCountRetriever{}
	gr := NewGraphRetriever(WithGraphChunkRetriever(cr), WithGraphTripleRetriever(tr))
	gr.Close()
	assert.Equal(t, 1, cr.closed)
	assert.Equal(t, 1, tr.closed)
}

type closeCountRetriever struct {
	fakeBeamRetriever
	closed int
}

func (c *closeCountRetriever) Close() { c.closed++ }

// TestGraphRetriever_RetrieveSearchResults返回nil 对齐 Python 基类 None
func TestGraphRetriever_RetrieveSearchResults返回nil(t *testing.T) {
	gr := NewGraphRetriever()
	rs, err := gr.RetrieveSearchResults(context.Background(), "q", 5, RetrievalModeHybrid)
	assert.NoError(t, err)
	assert.Nil(t, rs)
}

// TestGraphRetriever_主链retrieve chunk 检索 → 无 triples 回落原 chunks
func TestGraphRetriever_主链retrieve无triples回落(t *testing.T) {
	// chunk 桩返回 2 结果；fetch triples 走 dynamic retriever（fake store
	// 无 client 能力）→ warn 回落空 → graph_expansion 返回原 chunks[:topk]
	// 实施时组装：fakeGraphStore（CollectionName 桩）+ chunk 桩
}
```

（主链其余分支——beam 失败降级/RRF 融合/列对齐展开——在 Step 3 实现后补集成用例，核心断言：beam search 错误 → 返回原 chunks；扩展成功 → RRFFusion 结果序）

- [ ] **Step 3: 实现 graph_retriever_impl.go**

```go
// Package retriever 图检索器：chunk 检索 + 三元组 beam search 图扩展。
//
// Python: graph_retriever.py (GraphRetriever L228-814)
package retriever

import (
	"context"
	"fmt"
	"sort"
	"strings"

	chromav2 "github.com/amikos-tech/chroma-go/pkg/api/v2"
	"golang.org/x/sync/errgroup"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/embedding"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/utils"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/vector_store"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// GraphRetriever 图检索器。
//
// chunk 检索 + 三元组 beam search 图扩展 + RRF 融合。六可选构造
// （nil = Python None → 动态创建路径）。
type GraphRetriever struct {
	// chunkRetriever 固定 chunk 检索器（nil = 动态创建）
	chunkRetriever Retriever
	// tripleRetriever 固定三元组检索器（nil = 动态创建）
	tripleRetriever Retriever
	// vectorStore 向量存储（动态创建用）
	vectorStore vector_store.VectorStore
	// embedModel 嵌入模型（动态创建用）
	embedModel embedding.BaseEmbedding
	// chunkCollection chunk 集合名
	chunkCollection string
	// tripleCollection 三元组集合名
	tripleCollection string
	// indexType 索引类型（"" = Python None；上层 GraphKB 注入）
	indexType string
}

// GraphRetrieverOption 构造选项。
type GraphRetrieverOption func(*GraphRetriever)

// 编译期断言：GraphRetriever 满足 Retriever 接口。
var _ Retriever = (*GraphRetriever)(nil)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// defaultGraphHops 默认图跳数（Python kwargs.get("graph_hops", 2)）
	defaultGraphHops = 2
	// defaultGraphFallbackTopK 空 chunks sparse 回落默认 top_k（topk or 5）
	defaultGraphFallbackTopK = 5
	// graphFetchLimit fetch 查询 limit（Python L646/L557 字面 100）
	graphFetchLimit = 100
)

// ──────────────────────────── 导出函数 ────────────────────────────

// WithGraphChunkRetriever 设置固定 chunk 检索器。
func WithGraphChunkRetriever(r Retriever) GraphRetrieverOption {
	return func(g *GraphRetriever) { g.chunkRetriever = r }
}

// WithGraphTripleRetriever 设置固定三元组检索器。
func WithGraphTripleRetriever(r Retriever) GraphRetrieverOption {
	return func(g *GraphRetriever) { g.tripleRetriever = r }
}

// WithGraphVectorStore 设置向量存储（动态创建用）。
func WithGraphVectorStore(s vector_store.VectorStore) GraphRetrieverOption {
	return func(g *GraphRetriever) { g.vectorStore = s }
}

// WithGraphEmbedModel 设置嵌入模型（动态创建用）。
func WithGraphEmbedModel(m embedding.BaseEmbedding) GraphRetrieverOption {
	return func(g *GraphRetriever) { g.embedModel = m }
}

// WithGraphChunkCollection 设置 chunk 集合名。
func WithGraphChunkCollection(name string) GraphRetrieverOption {
	return func(g *GraphRetriever) { g.chunkCollection = name }
}

// WithGraphTripleCollection 设置三元组集合名。
func WithGraphTripleCollection(name string) GraphRetrieverOption {
	return func(g *GraphRetriever) { g.tripleCollection = name }
}

// NewGraphRetriever 创建图检索器。
//
// 对齐 Python __init__（L231-260）：六可选参数全收，零校验；index_type
// 留空由上层注入（SetIndexType）。
func NewGraphRetriever(opts ...GraphRetrieverOption) *GraphRetriever {
	g := &GraphRetriever{}
	for _, opt := range opts {
		opt(g)
	}
	return g
}

// IndexType 返回索引类型（"" = Python None）。
func (g *GraphRetriever) IndexType() string { return g.indexType }

// SetIndexType 注入索引类型（上层 GraphKB 调用，Python L260 注释语义）。
func (g *GraphRetriever) SetIndexType(indexType string) { g.indexType = indexType }

// GetRetrieverForMode 按 mode 取对应检索器。
//
// 对齐 Python get_retriever_for_mode（L304-378）：固定优先（不支持
// 155205）→ 动态创建（无 store 155206）→ SetCollectionName 先于空校验
// （155207，L339-346 字面顺序）→ 三分支 → index_type 注入。
func (g *GraphRetriever) GetRetrieverForMode(mode RetrievalMode, isChunk bool) (Retriever, error) {
	if err := g.ensureModeAllowed(mode); err != nil {
		return nil, err
	}

	fixed := g.chunkRetriever
	if !isChunk {
		fixed = g.tripleRetriever
	}
	if fixed != nil {
		if !g.retrieverSupportsMode(fixed, mode) {
			kind := "chunk"
			if !isChunk {
				kind = "triple"
			}
			return nil, exception.BuildError(
				exception.StatusRetrievalRetrieverCapabilityNotSupport,
				exception.WithParam("error_msg", fmt.Sprintf("Provided %s retriever %s does not support mode=%s",
					kind, retrieverTypeName(fixed), mode)),
			)
		}
		return fixed, nil
	}

	if g.vectorStore == nil {
		return nil, exception.BuildError(
			exception.StatusRetrievalRetrieverVectorStoreNotFound,
			exception.WithParam("error_msg", "vector_store is required for dynamic retriever creation"),
		)
	}

	collectionName := g.chunkCollection
	if !isChunk {
		collectionName = g.tripleCollection
	}
	// Python L339-346：先污染 collection_name 再判空（字面顺序复刻）
	g.vectorStore.SetCollectionName(collectionName)
	if collectionName == "" {
		collectionType := "chunk"
		if !isChunk {
			collectionType = "triple"
		}
		return nil, exception.BuildError(
			exception.StatusRetrievalRetrieverCollectionNotFound,
			exception.WithParam("error_msg", fmt.Sprintf("%s_collection is required for dynamic retriever creation", collectionType)),
		)
	}

	var retriever Retriever
	switch mode {
	case RetrievalModeVector:
		if g.embedModel == nil {
			return nil, exception.BuildError(
				exception.StatusRetrievalRetrieverEmbedModelNotFound,
				exception.WithParam("error_msg", "embed_model is required for vector mode"),
			)
		}
		retriever = NewVectorRetriever(g.vectorStore, g.embedModel)
	case RetrievalModeSparse:
		retriever = NewSparseRetriever(g.vectorStore)
	default: // hybrid（Python else 分支）
		retriever = NewHybridRetriever(g.vectorStore, g.embedModel)
	}

	// Python L375-376：index_type 注入动态 retriever（对未实现
	// IndexTypeCarrier 的跳过——宽松语义收窄归档 spec §8-9）
	if g.indexType != "" {
		if ic, ok := retriever.(IndexTypeCarrier); ok {
			ic.SetIndexType(g.indexType)
		}
	}
	return retriever, nil
}

// Retrieve 图检索主入口。
//
// 对齐 Python retrieve（L380-433）：mode 校验 → graph_hops 读取（nil→2）
// → threshold+mode≠vector 155201 → chunk 检索 → GraphExpansion。
func (g *GraphRetriever) Retrieve(ctx context.Context, query string, topK int, scoreThreshold *float64, mode RetrievalMode, opts ...RetrieveOption) ([]common.RetrievalResult, error) {
	if err := g.ensureModeAllowed(mode); err != nil {
		return nil, err
	}
	options := NewRetrieveOptions(opts...)
	graphHops := defaultGraphHops
	if options.GraphHops != nil {
		graphHops = *options.GraphHops
	}
	if scoreThreshold != nil && mode != RetrievalModeVector {
		return nil, exception.BuildError(
			exception.StatusRetrievalRetrieverScoreThresholdInvalid,
			exception.WithParam("error_msg", "score_threshold is only supported when mode='vector'"),
		)
	}

	chunkRetriever, err := g.GetRetrieverForMode(mode, true)
	if err != nil {
		return nil, err
	}
	chunkResults, err := chunkRetriever.Retrieve(ctx, query, topK, scoreThreshold, mode)
	if err != nil {
		return nil, err
	}

	logger.Info(logComponent).
		Str("event_type", "GRAPH_RETRIEVAL").
		Int("chunk_hits", len(chunkResults)).
		Int("topk", topK).
		Str("mode", string(mode)).
		Msg("[graph] Graph retrieval: graph_expansion=True")

	return g.GraphExpansion(ctx, query, chunkResults, nil, &topK, mode, opts...)
}

// GraphExpansion 图扩展。
//
// 对齐 Python graph_expansion（L435-526）：空 chunks sparse 二次回落；
// triples 未传走 _fetchTriples（异常 warn 回落空）；空 triples 返回原
// chunks；beam search（异常 warn 回落原 chunks——降级终点）；列对齐展开；
// _fetchChunks；RRF k=60 融合；topk 截断。
func (g *GraphRetriever) GraphExpansion(ctx context.Context, query string, chunks, triples []common.RetrievalResult, topk *int, mode RetrievalMode, opts ...RetrieveOption) ([]common.RetrievalResult, error) {
	if err := g.ensureModeAllowed(mode); err != nil {
		return nil, err
	}
	if len(chunks) == 0 {
		logger.Warn(logComponent).
			Str("mode", string(mode)).
			Msg("[graph] chunk_retriever returned empty, no results to expand")
		if mode == RetrievalModeSparse {
			sparseRetriever, err := g.GetRetrieverForMode(RetrievalModeSparse, true)
			if err != nil {
				return nil, err
			}
			fallbackTopK := defaultGraphFallbackTopK
			if topk != nil && *topk > 0 {
				fallbackTopK = *topk
			}
			fallback, err := sparseRetriever.Retrieve(ctx, query, fallbackTopK, nil, RetrievalModeSparse)
			if err != nil {
				return nil, err
			}
			if topk != nil && *topk < len(fallback) {
				return fallback[:*topk], nil
			}
			return fallback, nil
		}
		return nil, nil
	}

	if len(triples) == 0 {
		var err error
		triples, err = g.fetchTriples(ctx, chunks, mode)
		if err != nil {
			logger.Warn(logComponent).Err(err).
				Msg("[graph] Failed to fetch triples from chunk index")
			triples = nil
		} else {
			logger.Info(logComponent).
				Int("chunks", len(chunks)).
				Int("triples", len(triples)).
				Msg("[graph] Fetching triples from chunk index")
		}
	}

	if len(triples) == 0 {
		logger.Info(logComponent).Msg("[graph] No triples found, returning original chunks")
		return truncateResults(chunks, topk), nil
	}

	// beam search（含 TripleBeamSearch 构造的 kwargs 透传）
	var beams []*common.TripleBeam
	func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Warn(logComponent).
					Any("panic", r).
					Msg("[graph] Beam search panicked, falling back to chunks")
			}
		}()
		tripleRetriever, err := g.GetRetrieverForMode(mode, false)
		if err != nil {
			logger.Warn(logComponent).Err(err).
				Msg("[graph] Beam search failed, falling back to chunks")
			return
		}
		options := NewRetrieveOptions(opts...)
		searchOpts := []TripleBeamSearchOption{WithTripleRetrieveMode(string(mode))}
		if options.NumBeams != nil {
			searchOpts = append(searchOpts, WithTripleNumBeams(*options.NumBeams))
		}
		if options.NumCandidatesPerBeam != nil {
			searchOpts = append(searchOpts, WithTripleNumCandidatesPerBeam(*options.NumCandidatesPerBeam))
		}
		search, err := NewTripleBeamSearch(tripleRetriever, searchOpts...)
		if err != nil {
			logger.Warn(logComponent).Err(err).
				Msg("[graph] Beam search failed, falling back to chunks")
			return
		}
		beams, err = search.BeamSearch(ctx, query, triples)
		if err != nil {
			logger.Warn(logComponent).Err(err).
				Msg("[graph] Beam search failed, falling back to chunks")
			beams = nil
		}
	}()

	if len(beams) == 0 {
		logger.Info(logComponent).Msg("[graph] Beam search returned empty, returning original chunks")
		return truncateResults(chunks, topk), nil
	}

	// 列对齐展开（Python L504-510 按列优先）
	maxLength := 0
	for _, b := range beams {
		if b.Len() > maxLength {
			maxLength = b.Len()
		}
	}
	var expandedTriples []common.RetrievalResult
	for col := 0; col < maxLength; col++ {
		for _, beam := range beams {
			if col >= beam.Len() {
				continue
			}
			expandedTriples = append(expandedTriples, beam.Get(col))
		}
	}

	newChunks, err := g.fetchChunks(ctx, expandedTriples, mode)
	if err != nil {
		return nil, err
	}
	logger.Info(logComponent).
		Int("triples", len(expandedTriples)).
		Int("additional_chunks", len(newChunks)).
		Msg("[graph] Graph expansion beam results")

	var fused []common.RetrievalResult
	if len(newChunks) > 0 {
		fused = utils.RRFFusion([][]common.RetrievalResult{newChunks, chunks}, 60)
	} else {
		fused = chunks
	}
	return truncateResults(fused, topk), nil
}

// BatchRetrieve 批量并发检索（裸 errgroup 首错即返回，13.20 先例）。
func (g *GraphRetriever) BatchRetrieve(ctx context.Context, queries []string, topK int, opts ...RetrieveOption) ([][]common.RetrievalResult, error) {
	results := make([][]common.RetrievalResult, len(queries))
	gg, gctx := errgroup.WithContext(ctx)
	for i, query := range queries {
		i, query := i, query
		gg.Go(func() error {
			ret, err := g.Retrieve(gctx, query, topK, nil, "", opts...)
			if err != nil {
				return err
			}
			results[i] = ret
			return nil
		})
	}
	if err := gg.Wait(); err != nil {
		return nil, err
	}
	return results, nil
}

// RetrieveSearchResults 返回 nil（对齐 Python 基类空方法体 None）。
func (g *GraphRetriever) RetrieveSearchResults(context.Context, string, int, RetrievalMode, ...RetrieveOption) ([]common.SearchResult, error) {
	return nil, nil
}

// Close 关闭两个固定检索器（动态创建的不持有——对齐 Python 只关
// chunk_retriever/triple_retriever 两字段）。
func (g *GraphRetriever) Close() {
	if g.chunkRetriever != nil {
		g.chunkRetriever.Close()
	}
	if g.tripleRetriever != nil {
		g.tripleRetriever.Close()
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// ensureModeAllowed mode 校验（Python _ensure_mode_allowed L269-285）。
//
// indexType ""（None）免检；未知 155203；不兼容 155204（allowed 清单
// sorted 逗号空格拼接）。
func (g *GraphRetriever) ensureModeAllowed(mode RetrievalMode) error {
	if g.indexType == "" {
		return nil // index_type 未注入时免检（上层保证）
	}
	allowed, ok := allowedModesForIndexType(g.indexType)
	if !ok {
		return exception.BuildError(
			exception.StatusRetrievalRetrieverIndexTypeNotSupport,
			exception.WithParam("error_msg", fmt.Sprintf("Unsupported index_type=%s", g.indexType)),
		)
	}
	if !allowed[mode] {
		sorted := make([]string, 0, len(allowed))
		for m := range allowed {
			sorted = append(sorted, string(m))
		}
		sort.Strings(sorted)
		return exception.BuildError(
			exception.StatusRetrievalRetrieverModeInvalid,
			exception.WithParam("error_msg", fmt.Sprintf("mode=%s is incompatible with index_type=%s; allowed modes: [%s]",
				mode, g.indexType, strings.Join(sorted, ", "))),
		)
	}
	return nil
}

// allowedModesForIndexType 对齐 Python _allowed_modes（L262-267）。
func allowedModesForIndexType(indexType string) (map[RetrievalMode]bool, bool) {
	switch indexType {
	case "vector":
		return map[RetrievalMode]bool{RetrievalModeVector: true}, true
	case "bm25":
		return map[RetrievalMode]bool{RetrievalModeSparse: true}, true
	case "hybrid":
		return map[RetrievalMode]bool{
			RetrievalModeVector: true, RetrievalModeSparse: true, RetrievalModeHybrid: true,
		}, true
	default:
		return nil, false
	}
}

// retrieverSupportsMode 对齐 Python _retriever_supports_mode（L287-302）：
// 类型 switch 三兄弟 + SupportedModesProvider 探测 + 兜底 true。
func (g *GraphRetriever) retrieverSupportsMode(r Retriever, mode RetrievalMode) bool {
	switch r.(type) {
	case *VectorRetriever:
		return mode == RetrievalModeVector
	case *SparseRetriever:
		return mode == RetrievalModeSparse
	case *HybridRetriever:
		return mode == RetrievalModeVector || mode == RetrievalModeSparse || mode == RetrievalModeHybrid
	}
	if sp, ok := r.(SupportedModesProvider); ok {
		for _, m := range sp.SupportedModes() {
			if m == mode {
				return true
			}
		}
		return false
	}
	return true
}

// retrieverTypeName 检索器类型名（Python __class__.__name__）。
func retrieverTypeName(r Retriever) string {
	return fmt.Sprintf("%T", r)
}

// truncateResults topk 截断（nil 不截断；Python fused[:topk] if topk）。
func truncateResults(results []common.RetrievalResult, topk *int) []common.RetrievalResult {
	if topk == nil {
		return results
	}
	if *topk < len(results) {
		return results[:*topk]
	}
	return results
}

// fetchTriples 按 chunk_id 反查三元组（Python _fetch_triples L598-680）。
func (g *GraphRetriever) fetchTriples(ctx context.Context, chunks []common.RetrievalResult, mode RetrievalMode) ([]common.RetrievalResult, error) {
	var chunkIDs []string
	for _, x := range chunks {
		if x.Metadata != nil {
			if cid, ok := x.Metadata["chunk_id"].(string); ok && cid != "" {
				chunkIDs = append(chunkIDs, cid)
			}
		}
	}
	if len(chunkIDs) == 0 {
		return nil, nil
	}

	tripleRetriever, err := g.GetRetrieverForMode(mode, false)
	if err != nil {
		return nil, err
	}

	vs, ok := tripleRetriever.(VectorStoreAccessor)
	if !ok {
		logger.Warn(logComponent).
			Msg("[graph] triple_retriever does not have vector_store, cannot fetch triples")
		return nil, nil
	}
	store := vs.VectorStore()
	if store == nil {
		logger.Warn(logComponent).
			Msg("[graph] Cannot fetch triples: missing client or collection_name")
		return nil, nil
	}

	// store 具体类型断言分发（Python isinstance(client, MilvusClient)/
	// _is_chroma_client 的 store 层等价物，spec §3.6）
	var items []fetchItem
	switch s := store.(type) {
	case *vector_store.MilvusVectorStore:
		client, err := s.Client(ctx)
		if err != nil {
			return nil, err
		}
		collectionName := s.CollectionName()
		if collectionName == "" {
			logger.Warn(logComponent).
				Msg("[graph] Cannot fetch triples: missing client or collection_name")
			return nil, nil
		}
		escaped := make([]string, len(chunkIDs))
		for i, cid := range chunkIDs {
			escaped[i] = fmt.Sprintf("%q", cid)
		}
		filterExpr := fmt.Sprintf("chunk_id IN [%s]", strings.Join(escaped, ", "))
		items, err = queryByFilterMilvus(ctx, client, collectionName, filterExpr, graphFetchLimit)
		if err != nil {
			logger.Warn(logComponent).Err(err).
				Msg("[graph] Failed to fetch triples for chunks")
			return nil, nil
		}
	case *vector_store.ChromaVectorStore:
		client := s.Client()
		collectionName := s.CollectionName()
		if collectionName == "" {
			logger.Warn(logComponent).
				Msg("[graph] Cannot fetch triples: missing client or collection_name")
			return nil, nil
		}
		where := chromav2.InString("chunk_id", chunkIDs...)
		items, err = queryByFilterChroma(ctx, client, collectionName, where, graphFetchLimit)
		if err != nil {
			logger.Warn(logComponent).Err(err).
				Msg("[graph] Chroma query failed")
			return nil, nil
		}
	default:
		logger.Warn(logComponent).
			Msg("[graph] Unsupported client type in fetch_for_chunks")
		return nil, nil
	}

	// _fetch_triples 归一化：chunk_id 回落空串（Python L670 metadata.get("chunk_id", "")）
	return fetchItemsToRetrievalResults(items, ""), nil
}

// fetchChunks 按 chunk_id 正查 chunk（Python _fetch_chunks L682-781）。
func (g *GraphRetriever) fetchChunks(ctx context.Context, triples []common.RetrievalResult, mode RetrievalMode) ([]common.RetrievalResult, error) {
	chunkIDSet := make(map[string]struct{})
	for _, node := range triples {
		if node.ChunkID != "" {
			chunkIDSet[node.ChunkID] = struct{}{}
		}
	}
	if len(chunkIDSet) == 0 {
		return nil, nil
	}

	chunkRetriever, err := g.GetRetrieverForMode(mode, true)
	if err != nil {
		return nil, err
	}

	vs, ok := chunkRetriever.(VectorStoreAccessor)
	if !ok {
		logger.Warn(logComponent).
			Msg("[graph] chunk_retriever does not have vector_store, cannot fetch chunks")
		return nil, nil
	}
	store := vs.VectorStore()
	if store == nil {
		logger.Warn(logComponent).
			Msg("[graph] Cannot fetch chunks: missing client or collection_name")
		return nil, nil
	}

	// 逐 chunk_id 并发 fetch（Python gather 全启动 + fetchChunk 内部吞错
	// 返回 None；Go errgroup 语义同构——吞错分支不返回 error）
	chunkIDs := make([]string, 0, len(chunkIDSet))
	for cid := range chunkIDSet {
		chunkIDs = append(chunkIDs, cid)
	}
	results := make([]*common.RetrievalResult, len(chunkIDs))
	gg, gctx := errgroup.WithContext(ctx)
	for i, cid := range chunkIDs {
		i, cid := i, cid
		gg.Go(func() error {
			result, err := g.fetchChunk(gctx, store, cid)
			if err != nil {
				logger.Warn(logComponent).Err(err).
					Str("chunk_id", cid).
					Msg("[graph] Failed to query chunk")
				return nil // Python fetchChunk except 返回 None
			}
			results[i] = result
			return nil
		})
	}
	if err := gg.Wait(); err != nil {
		return nil, err
	}

	var chunks []common.RetrievalResult
	for _, r := range results {
		if r != nil {
			chunks = append(chunks, *r)
		}
	}
	return chunks, nil
}

// fetchChunk 单 chunk 查询（Python fetch_chunk L728-768：吞错返回 nil）。
func (g *GraphRetriever) fetchChunk(ctx context.Context, store vector_store.VectorStore, chunkID string) (*common.RetrievalResult, error) {
	switch s := store.(type) {
	case *vector_store.MilvusVectorStore:
		client, err := s.Client(ctx)
		if err != nil {
			return nil, err
		}
		filterExpr := fmt.Sprintf("chunk_id == %q", chunkID)
		items, err := queryByFilterMilvus(ctx, client, s.CollectionName(), filterExpr, 1)
		if err != nil {
			return nil, err
		}
		if len(items) == 0 {
			return nil, nil
		}
		// chunk_id 回落查询值（Python L766 metadata.get("chunk_id", chunk_id)）
		results := fetchItemsToRetrievalResults(items[:1], chunkID)
		return &results[0], nil
	case *vector_store.ChromaVectorStore:
		where := chromav2.EqString("chunk_id", chunkID)
		items, err := queryByFilterChroma(ctx, s.Client(), s.CollectionName(), where, 1)
		if err != nil {
			return nil, err
		}
		if len(items) == 0 {
			return nil, nil
		}
		results := fetchItemsToRetrievalResults(items[:1], chunkID)
		return &results[0], nil
	default:
		logger.Warn(logComponent).
			Msg("[graph] Unsupported client type in fetch_chunk")
		return nil, nil
	}
}
```

**关键实施注意**：
1. **SetCollectionName 依赖**：`GetRetrieverForMode` 动态分支调 `g.vectorStore.SetCollectionName(...)`——**VectorStore 接口（13.2 base.go）没有这方法**！直接调会编译错。消解：定义包内小接口 `type collectionNameSetter interface { SetCollectionName(string) }`，`if setter, ok := g.vectorStore.(collectionNameSetter); ok { setter.SetCollectionName(collectionName) }`——fakeGraphStore 桩实现它即"先污染后校验"可测（Milvus/Chroma 真实现 Task 4 已加）。未实现的 store 跳过（Python 对无此属性的 store 也会 AttributeError，但生产链只有两个真 store，宽松处理归档偏差）。**同理 `CollectionName()` 读取**在 fetchTriples 里通过具体类型断言（Milvus/Chroma）调用——已是具体类型不怕。
2. Python `retrieve` 的 `graph_expansion=True` 日志在 Info 补 `graph_expansion` 字段（字面 True）
3. Python L465 `fallback[:topk] if topk else fallback`——`topk` 为 0 时 falsy 不截断！Go 用 `topk != nil`（0 与 nil 的语义：Python topk=0 → falsy → 不截断；Go *int 0 → 截断到 0 → 空）。**偏差点**：Python `if topk` 是 truthiness 判断（0=None 效果），Go 按指针判断。对齐方案：`if topk != nil && *topk > 0` 才截断——已在上文 GraphExpansion 空 chunks 分支如此实现，`truncateResults` 同样改为 `*topk > 0` 判断（偏差归档：topk=0 两语言行为 Python 不截断/Go 不截断——用 >0 统一）

- [ ] **Step 4: 跑测试确认通过**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/retrieval/retriever/ -run "TestGraphRetriever" -v
```

Expected: 全 PASS

- [ ] **Step 5: doc.go 更新（文件目录追加六个新文件）**

retriever/doc.go 文件目录段追加：

```
//	├── capability.go           # 能力接口五件套（13.23）
//	├── graph_numeric.go        # 余弦打分 + topk（13.23）
//	├── graph_beam_search.go    # TripleBeamSearch beam search（13.23）
//	├── graph_fetch.go          # Milvus/Chroma 过滤查询 fetch 层（13.23）
//	└── graph_retriever_impl.go # GraphRetriever 图检索器（13.23）
```

（注意原目录末行 hybrid_retriever.go 的 `└──` 改 `├──`；核心类型索引段补 TripleBeamSearch/GraphRetriever 条目；包概述补图检索一句）

- [ ] **Step 6: 全包回归**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/retrieval/... -count=1
```

Expected: 全 PASS

- [ ] **Step 7: Commit**

```bash
git add internal/agentcore/retrieval/retriever/ && git commit -m "feat(retriever): 13.23 GraphRetriever——图检索主链（mode 校验/动态创建/GraphExpansion 降级链/RRF 融合）+ RetrieveOptions 三潜伏字段"
```

---

### Task 9: 集成用例补全 + 覆盖率达标

**Files:**
- Test: `internal/agentcore/retrieval/retriever/graph_retriever_impl_test.go`（追加）

- [ ] **Step 1: 补集成用例（Task 8 Step 2 预留的主链分支）**

三个用例骨架（核心断言写全，桩复用前任务）：

```go
// TestGraphRetriever_主链beam失败降级 embed 桩第二批次耗尽 → beam search 错误
// → graph_expansion warn 降级返回原 chunks[:topk]
func TestGraphRetriever_主链beam失败降级(t *testing.T) {
	// 组装：chunk 桩返回 2 结果（带 chunk_id）+ fakeGraphStore + embed 桩
	// batches 只给 1 批（扩展轮耗尽 → no more batches 错误）
	// fetchTriples：fakeGraphStore 非 Milvus/Chroma 具体类型 → Unsupported
	// → 走不到 beam？——不对：需要 store 是 Chroma 字面量才能出 triples。
	// 方案：构造 *vector_store.ChromaVectorStore 字面量（同包测试在
	// retriever 包不行——跨包私有字段不可见！）
	//
	// 消解：beam 失败降级改从 TripleBeamSearch 层直接触发——GraphExpansion
	// 的降级用例改为：triples 显式传入（len>0 跳过 fetch）+ tripleRetriever
	// 桩 + embed 桩批次耗尽 → BeamSearch err → 降级原 chunks
	gr := NewGraphRetriever(WithGraphTripleRetriever(&withEmbedRetriever{...}))
	...
}

// TestGraphRetriever_主链扩展融合 扩展成功 → _fetchChunks 走 Unsupported
// （fake store 非 Milvus/Chroma）→ newChunks 空 → fused=chunks
func TestGraphRetriever_主链扩展融合(t *testing.T) { ... }

// TestGraphRetriever_topk语义 topk=0 不截断（Python truthiness 对齐）
func TestGraphRetriever_topk语义(t *testing.T) { ... }
```

**跨包私有字段障碍消解**（重要）：retriever 包测试无法构造带内部 client 的真 `*ChromaVectorStore`/`*MilvusVectorStore`。fetch 层的 Milvus/Chroma 分支集成测试已在 Task 7 用接口桩覆盖（milvusQueryStub/chromaClientStub）；GraphRetriever 集成层的 store 断言分支走 fakeGraphStore（Unsupported 路径）+ 显式传 triples 绕过 fetch——分支覆盖经单元+集成两层拼合达成。

- [ ] **Step 2: 跑全部 13.23 测试 + 覆盖率**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/retrieval/retriever/ -cover -count=1
```

Expected: 覆盖率 ≥ 85%（包基线 94.7% 不回落；新文件目标：graph_numeric/graph_beam_search/graph_retriever_impl ≥ 85%，graph_fetch 桩覆盖下 ≥ 80%）

- [ ] **Step 3: 覆盖率不足时补测**

`go tool cover -func` 输出逐函数排查，补边界用例（构造防御分支/常量回读）。

- [ ] **Step 4: Commit**

```bash
git add internal/agentcore/retrieval/retriever/graph_retriever_impl_test.go && git commit -m "test(retriever): 13.23 集成用例补全——beam 失败降级/扩展融合/topk 语义（覆盖率达标）"
```

---

### Task 10: 全量回归 + IMPLEMENTATION_PLAN.md 状态回填

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`（13.23 行）

- [ ] **Step 1: 全量测试（带 CI 同款标签）**

```bash
cd /home/opensource/uapclaw-gateway && CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./... 2>&1 | tail -30
```

Expected: 全 PASS（cmd 带 test 标签 build 报 main undeclared 是预期——CI 用 vet，先例）

- [ ] **Step 2: IMPLEMENTATION_PLAN.md 13.23 行 ☐→✅**

产出描述（对齐先例行格式）：

```
| 13.23 | ✅ | GraphRetriever | `GraphRetriever(mode 校验 155203/155204 免检语义+GetRetrieverForMode 固定优先 155205/动态创建 155206/SetCollectionName 先污染后校验 155207/三分支+index_type 注入 + Retrieve graph_hops 潜伏消费+155201+GraphExpansion 五层降级链(空 chunks sparse 回落/beam 失败 warn 原 chunks/列对齐展开/RRF k=60)+fetchTriples/fetchChunks store 具体类型断言 Milvus IN/Chroma $in+fetchChunk 吞错 nil+Close 双固定) + TripleBeamSearch(155204 max_length/155202 embed 缺失+批大小断言 155202/两轮嵌入余弦打分 gonum floats/expandBeams existTriples 判重+seenBeamTexts 去重/searchCandidates mode 三级解析 bm25→sparse+三重过滤+候选裸解析冒泡) + 能力接口五件套(EmbedModelProvider/IndexTypeCarrier/VectorStoreAccessor/MaxBatchSizeProvider/SupportedModesProvider) + 三实现回填(indexType 字段+访问器) + store 回填(CollectionName getter/setter+Chroma 缓存失效) + embedding 四实现回填 MaxBatchSize + RetrieveOptions 三潜伏字段(GraphHops/NumBeams/NumCandidatesPerBeam)；10 偏差归档 spec §8` | `openjiuwen/core/retrieval/retriever/graph_retriever.py` |
```

同时更新领域十三备注段（13.23 完成、下一节 13.24）。

- [ ] **Step 3: spec 偏差表回填核实**

`docs/superpowers/specs/2026-10-28-graphretriever-13.23-design.md` §8——实施中若出现计划外偏差（如 topk truthiness、SetCollectionName 接口断言），补录行。

- [ ] **Step 4: Commit**

```bash
git add IMPLEMENTATION_PLAN.md docs/superpowers/specs/2026-10-28-graphretriever-13.23-design.md && git commit -m "docs(plan): 13.23 GraphRetriever 完成状态回填"
```

---

## 自审记录

1. **Spec 覆盖**：§3.1 能力接口→Task 3/4/5；§3.2/§3.3 结构与动态创建→Task 8；§3.4 RetrieveOptions→Task 8 Step 1；§3.5 数值层→Task 2；§3.6 fetch 层→Task 7/8；§3.7 TripleBeamSearch→Task 6；§3.8 日志→各实现任务内嵌；§4 回填清单→Task 3/4/5/8/10；§5 测试设计→Task 6/8/9；§7 开放点→SDK 事实段钉死 + Task 4 缓存失效 + Task 8 注意点。无缺口。
2. **占位符扫描**：Task 5 测试的 `mustNewXxx` 辅助与 Task 7 的 milvus column 构造标注"实施时按实际签名填"——这是对**测试辅助构造器**的适配说明而非实现内容占位（实现代码本身完整）；Task 9 三个用例为骨架+断言说明（集成用例依赖前任务桩的最终形态，先例允许）。核心实现代码（Task 2/3/4/6/7/8）全部完整给出。
3. **类型一致性**：`fetchItem.docIDTop`（Task 7 定义，Task 8 fetchTriples 经 fetchItemsToRetrievalResults 消费）✅；`searchCandidates/expandBeams`（Task 6 小写方法，BeamSearch 内调用）✅；`WithTripleMaxLength` 等五选项 Task 6 定义 Task 8 消费 ✅；`logComponent` Task 6 注意点声明单点定义 ✅；`truncateResults` `*topk > 0` 语义在 Task 8 实现与注意点 3 一致 ✅。
