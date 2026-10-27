# 13.21 HybridRetriever Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 HybridRetriever（向量+稀疏 RRF 融合检索的薄层路由器），并把 13.19 的 RetrieveOptions.Extra map 平铺为类型化字段 Alpha *float64。

**Architecture:** retriever 包既有布局内新增 hybrid_retriever.go（全字面复刻 Python retrieve/rsr 两份三分支树）+ base.go 修订（Extra→Alpha 平铺）+ 桩改造（HybridSearch 可编程化）。RRF 融合不在本类——在 store 层（13.2/13.3 已交付），本类只做 mode 路由 + alpha 透传 + 归一化。

**Tech Stack:** Go / golang.org/x/sync/errgroup / testify / 项目 exception.BuildError 错误码体系（155200/155201/155202）

**Spec:** `docs/superpowers/specs/2026-10-28-hybridretriever-13.21-design.md`（含 7 决策 + 8 偏差 + 8 回填项，实施中随时对照）

**Python 对齐基准：**
- 源：`/home/opensource/agent-core/openjiuwen/core/retrieval/retriever/hybrid_retriever.py`（222 行）
- 测试：`/home/opensource/agent-core/tests/unit_tests/core/retrieval/retriever/test_hybrid_retriever.py`（8 用例）

**关键纪律（全任务适用）：**
1. 注释全中文；文件内声明按 结构体→枚举→常量→全局变量→导出函数→非导出函数 排列（go-code-conventions）
2. 错误文案逐字符对齐 Python（spec §6 文案表）
3. **155201 前置校验必须先于 mode 分支**（Python L64-68 在 L70 之前——执行顺序不可调换）
4. Python 零 logger → Go 零日志
5. git 提交显式 add 具体文件，**禁 -am**（工作区可能有他任务改动）
6. 编译前 `pgrep -f 'go (build|test)'` 检查残留进程；`export GOPROXY=https://goproxy.cn,direct`
7. 测试命令统一：`cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/retrieval/retriever/ -run <TestName> -v -count=1`（包级全量在 Task 7）

---

### Task 1: base.go 平铺修订（Extra → Alpha）

**Files:**
- Modify: `internal/agentcore/retrieval/retriever/base.go:46-79`（结构体+WithRetrieveExtra 段）
- Modify: `internal/agentcore/retrieval/retriever/base_test.go:16-36`（3 个测试改写）

- [ ] **Step 1.1: 改写 base_test.go 的 3 个测试（先失败）**

将 base_test.go 的这三个测试（TestNewRetrieveOptions_空参数 / TestWithRetrieveExtra_单键 / TestWithRetrieveExtra_多选项）整体替换为：

```go
// TestNewRetrieveOptions_空参数 空参数构建零值选项（Alpha nil=未设）
func TestNewRetrieveOptions_空参数(t *testing.T) {
	o := NewRetrieveOptions()
	assert.Nil(t, o.Alpha)
}

// TestWithRetrieveAlpha_设置 13.21 平铺字段：设置后非 nil、值正确
func TestWithRetrieveAlpha_设置(t *testing.T) {
	o := NewRetrieveOptions(WithRetrieveAlpha(0.8))
	require.NotNil(t, o.Alpha)
	assert.Equal(t, 0.8, *o.Alpha)
}

// TestWithRetrieveAlpha_显式零 显式 0 是合法纯稀疏语义，与未设（nil）区分
func TestWithRetrieveAlpha_显式零(t *testing.T) {
	o := NewRetrieveOptions(WithRetrieveAlpha(0))
	require.NotNil(t, o.Alpha)
	assert.Equal(t, 0.0, *o.Alpha)
}
```

注意：base_test.go 头部 import 需加 `"github.com/stretchr/testify/require"`（现有仅 assert）。

- [ ] **Step 1.2: 运行测试确认编译失败**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/retrieval/retriever/ -run 'TestNewRetrieveOptions|TestWithRetrieveAlpha' -v -count=1`
Expected: 编译失败 `undefined: WithRetrieveAlpha`（TDD 红灯）

- [ ] **Step 1.3: 修订 base.go**

将 base.go 的 RetrieveOptions 段（L46-79：结构体 + WithRetrieveExtra + NewRetrieveOptions）替换为：

```go
// ──────────────────────────── 结构体 ────────────────────────────

// RetrieveOptions 检索选项（Python **kwargs 的闭合世界对应物）。
//
// 13.21 修订：原 Extra map[string]any 平铺为类型化字段（编译期类型安全，
// 2026-10-28 拍板）。原"Extra 收纳 **kwargs"设计见 13.19 spec §4.1，
// 修订记录归档两 spec，原约定不删除。
type RetrieveOptions struct {
	// Alpha hybrid 检索权重（0=纯稀疏，1=纯向量，0.5=平衡）。
	// nil = 未设置 → HybridRetriever 回落构造默认；非 nil = 调用时覆写。
	// 指针类型区分"未设"与"显式 0"（alpha=0 是合法纯稀疏语义，不能与
	// 零值混淆）。仅 HybridRetriever 消费；Vector/Sparse 不读
	// （对齐 Python 死参数）。
	Alpha *float64
}

// RetrieveOption RetrieveOptions 的函数选项。
type RetrieveOption func(*RetrieveOptions)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewRetrieveOptions 从可变参数构建 RetrieveOptions。
func NewRetrieveOptions(opts ...RetrieveOption) RetrieveOptions {
	o := RetrieveOptions{}
	for _, opt := range opts {
		opt(&o)
	}
	return o
}

// WithRetrieveAlpha 设置 hybrid 检索权重的 RetrieveOption。
func WithRetrieveAlpha(v float64) RetrieveOption {
	return func(o *RetrieveOptions) { o.Alpha = &v }
}
```

同时 base.go 结构体区块注释上方的分隔注释位置不变（原 L46 分隔注释是"──── 结构体 ────"区块头，RetrieveOptions 本就在该区块；删除的是旧注释行"// RetrieveOptions 检索选项（Python **kwargs 收纳容器）"等三行）。WithRetrieveExtra 函数整体删除。

- [ ] **Step 1.4: 运行测试确认通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/retrieval/retriever/ -run 'TestNewRetrieveOptions|TestWithRetrieveAlpha' -v -count=1`
Expected: 3 例 PASS

- [ ] **Step 1.5: 确认包内既有测试零回归**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/retrieval/retriever/ -count=1`
Expected: 全部 PASS（Vector/Sparse 的 `_ ...RetrieveOption` 签名不受影响——收而不用）

- [ ] **Step 1.6: Commit**

```bash
git add internal/agentcore/retrieval/retriever/base.go internal/agentcore/retrieval/retriever/base_test.go
git commit -m "refactor(retriever): 13.21 RetrieveOptions 平铺为类型化字段 Alpha

- Extra map[string]any → Alpha *float64（编译期类型安全，指针区分未设/显式 0）
- WithRetrieveExtra 删除（生产零调用）→ WithRetrieveAlpha 新增
- 13.19 spec §4.1 修订兑现，Vector/Sparse 签名零改动"
```

---

### Task 2: 桩改造（fakeVectorStore.HybridSearch 可编程化）

**Files:**
- Modify: `internal/agentcore/retrieval/retriever/base_test.go:53-66`（struct 字段）与 `:91-93`（HybridSearch 方法）

- [ ] **Step 2.1: 增量字段 + HybridSearch 实现**

fakeVectorStore struct 增量字段（插在 `lastQueryText string` 之后、`mu sync.Mutex` 之前）：

```go
type fakeVectorStore struct {
	searchResult   []common.SearchResult
	sparseResult   []common.SearchResult
	searchErr      error
	sparseErr      error
	searchErrAfter int
	searchCalls    int
	sparseCalls    int
	closeCalls     int
	lastTopK       int
	lastQueryVec   []float64
	lastQueryText  string
	// ── 13.21 HybridSearch 桩（可编程化）──
	hybridResult  []common.SearchResult
	hybridErr     error
	hybridCalls   int
	lastAlpha     float64   // alpha 覆写断言用（Python call_args 对应物）
	lastHybridVec []float64 // embed nil 纯稀疏路径断言用（区别于 search 的 lastQueryVec）
	mu            sync.Mutex
}
```

HybridSearch 方法替换为零实现版（保持 mu 保护，BatchRetrieve 并发打桩先例）：

```go
func (f *fakeVectorStore) HybridSearch(_ context.Context, queryText string, queryVector []float64, _ int, alpha float64, _ query.QueryExpr, _ ...vector_store.StoreOption) ([]common.SearchResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.hybridCalls++
	f.lastQueryText = queryText
	f.lastHybridVec = queryVector
	f.lastAlpha = alpha
	return f.hybridResult, f.hybridErr
}
```

注意：lastHybridVec 独立字段不与 search 的 lastQueryVec 复用（hybrid 分支 embed nil 时 lastQueryVec 不被触碰，两路径断言互不干扰）。

- [ ] **Step 2.2: 编译+全量测试确认零回归**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/retrieval/retriever/ -count=1`
Expected: 全部 PASS（既有用例不触达 HybridSearch 或触达旧零实现语义——13.20 的 Vector/Sparse 用例不调 HybridSearch，无影响）

- [ ] **Step 2.3: Commit**

```bash
git add internal/agentcore/retrieval/retriever/base_test.go
git commit -m "test(retriever): 13.21 fakeVectorStore.HybridSearch 可编程化桩

- hybridResult/hybridErr/lastAlpha/lastHybridVec/hybridCalls 捕获字段（mu 保护）
- Python call_args 断言的 Go 对应物"
```

---

### Task 3: HybridRetriever 结构体 + 构造 + 编译期断言

**Files:**
- Create: `internal/agentcore/retrieval/retriever/hybrid_retriever.go`
- Modify: `internal/agentcore/retrieval/retriever/base.go:81-87`（全局变量区断言追加）

- [ ] **Step 3.1: 创建 hybrid_retriever.go（结构体+构造+空方法占位）**

```go
package retriever

import (
	"context"

	"golang.org/x/sync/errgroup"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/embedding"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/vector_store"
)

// ──────────────────────────── 结构体 ────────────────────────────

// HybridRetriever 混合检索器实现（向量+稀疏，RRF 融合在 store 层完成）。
//
// KB 编排层的兜底检索器：SimpleKnowledgeBase 按 index_type 路由的 else
// 分支（含 "hybrid"）、GraphRetriever 动态创建的 else 分支。
// Python: openjiuwen/core/retrieval/retriever/hybrid_retriever.py
type HybridRetriever struct {
	// vectorStore 底层向量存储
	vectorStore vector_store.VectorStore
	// embedModel 嵌入模型（nil = Python None；hybrid 模式可选走纯稀疏，
	// vector 模式缺失时报 155202）
	embedModel embedding.BaseEmbedding
	// alpha hybrid 权重（0=纯稀疏，1=纯向量，0.5=平衡），构造默认 0.5
	alpha float64
}

// HybridRetrieverOption HybridRetriever 的构造选项。
type HybridRetrieverOption func(*HybridRetriever)

// ──────────────────────────── 导出函数 ────────────────────────────

// WithHybridAlpha 设置 hybrid 权重（0=纯稀疏，1=纯向量）。
func WithHybridAlpha(v float64) HybridRetrieverOption {
	return func(r *HybridRetriever) { r.alpha = v }
}

// NewHybridRetriever 创建混合检索器。
//
// 对齐 Python HybridRetriever.__init__(vector_store, embed_model=None,
// alpha=0.5, **kwargs)：默认 alpha=0.5；kwargs 构造死参数不引入
// （13.19 决策 5 先例，spec §8-2）。构造零校验（embedModel nil 合法）。
func NewHybridRetriever(vectorStore vector_store.VectorStore, embedModel embedding.BaseEmbedding, opts ...HybridRetrieverOption) *HybridRetriever {
	r := &HybridRetriever{
		vectorStore: vectorStore,
		embedModel:  embedModel,
		alpha:       0.5,
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}
```

（四方法本 Task 先不写——Task 4/5 TDD 逐个补齐；本文件先只含结构体+构造。）

- [ ] **Step 3.2: base.go 全局变量区追加第三条断言**

base.go 的编译期断言块（L81-87）改为：

```go
// ──────────────────────────── 全局变量 ────────────────────────────

// 编译期断言：三实现满足 Retriever 接口（13.17 先例）。
var (
	_ Retriever = (*VectorRetriever)(nil)
	_ Retriever = (*SparseRetriever)(nil)
	_ Retriever = (*HybridRetriever)(nil)
)
```

注意：此步会让包编译失败（HybridRetriever 未实现四方法）——断言随 Task 4 首方法落地后转绿。若希望每 Task 独立绿，可把断言追加挪到 Task 5 完成后；本计划选择断言随 Task 3 引入、Task 4/5 完成后自然转绿（Task 3 的 commit 时点在 Task 4 之前须保证可编译，故 **断言追加实际放 Task 5 Step 5.4**——见该步。Task 3 只创建 hybrid_retriever.go 本体）。

- [ ] **Step 3.3: 编译确认（无断言版）**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/agentcore/retrieval/retriever/`
Expected: 编译通过（结构体+构造不依赖接口实现）

- [ ] **Step 3.4: Commit**

```bash
git add internal/agentcore/retrieval/retriever/hybrid_retriever.go
git commit -m "feat(retriever): 13.21 HybridRetriever 结构体+构造+alpha 选项

- NewHybridRetriever 选项模式，默认 alpha=0.5（对齐 Python 默认参数）
- WithHybridAlpha 构造覆写（13.17 WithTripleTemperature 先例）
- kwargs 构造死参数不引入（spec §8-2）"
```

---

### Task 4: Retrieve + 测试（H1-H7 + Go 增强）

**Files:**
- Modify: `internal/agentcore/retrieval/retriever/hybrid_retriever.go`（追加 Retrieve）
- Create: `internal/agentcore/retrieval/retriever/hybrid_retriever_test.go`

- [ ] **Step 4.1: 写失败测试（fixture + H1-H7 + 增强 5 项）**

hybrid_retriever_test.go 全文（首版，Batch/rsr 用例 Task 5 追加）：

```go
package retriever

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// newHybridFixture 构建装配好的 Hybrid 桩与检索器（Python mock fixture 对应物）
func newHybridFixture() (*HybridRetriever, *fakeVectorStore, *fakeEmbedModel) {
	store := &fakeVectorStore{
		hybridResult: []common.SearchResult{
			{ID: "1", Text: "Hybrid result 1", Score: 0.95, Metadata: map[string]any{"doc_id": "doc_1"}},
			{ID: "2", Text: "Hybrid result 2", Score: 0.85, Metadata: map[string]any{"doc_id": "doc_2"}},
		},
		searchResult: []common.SearchResult{
			{ID: "1", Text: "Vector result", Score: 0.9, Metadata: map[string]any{}},
		},
		sparseResult: []common.SearchResult{
			{ID: "1", Text: "Sparse result", Score: 0.8, Metadata: map[string]any{}},
		},
	}
	embed := &fakeEmbedModel{queryVec: []float64{0.1} * 384}
	r := NewHybridRetriever(store, embed)
	return r, store, embed
}
```

注意：`[]float64{0.1} * 384` 非法 Go 语法，实际写为：

```go
	embed := &fakeEmbedModel{queryVec: make([]float64, 384)}
	for i := range embed.queryVec {
		embed.queryVec[i] = 0.1
	}
```

（对齐 Python `[0.1] * 384`。）

H1-H7 + 增强用例：

```go
// TestHybridRetriever_Retrieve_hybrid模式 对齐 test_retrieve_hybrid_mode
func TestHybridRetriever_Retrieve_hybrid模式(t *testing.T) {
	r, store, embed := newHybridFixture()
	results, err := r.Retrieve(context.Background(), "test query", 5, nil, RetrievalModeHybrid)
	require.NoError(t, err)
	assert.Len(t, results, 2)
	assert.Equal(t, 1, store.hybridCalls)
	assert.Equal(t, 1, embed.embedQueryCalls)
}

// TestHybridRetriever_Retrieve_vector模式 对齐 test_retrieve_vector_mode
func TestHybridRetriever_Retrieve_vector模式(t *testing.T) {
	r, store, _ := newHybridFixture()
	results, err := r.Retrieve(context.Background(), "test query", 5, nil, RetrievalModeVector)
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, 1, store.searchCalls)
}

// TestHybridRetriever_Retrieve_sparse模式 对齐 test_retrieve_sparse_mode（无 embed 构造）
func TestHybridRetriever_Retrieve_sparse模式(t *testing.T) {
	store := &fakeVectorStore{
		sparseResult: []common.SearchResult{
			{ID: "1", Text: "Sparse result", Score: 0.8, Metadata: map[string]any{}},
		},
	}
	r := NewHybridRetriever(store, nil)
	results, err := r.Retrieve(context.Background(), "test query", 5, nil, RetrievalModeSparse)
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, 1, store.sparseCalls)
}

// TestHybridRetriever_Retrieve_alpha覆写 对齐 test_retrieve_with_custom_alpha
// （构造 0.7 + 调用 0.8 → store 收到 0.8：调用覆写优先于构造默认）
func TestHybridRetriever_Retrieve_alpha覆写(t *testing.T) {
	r, store, _ := newHybridFixture0(0.7)
	_, err := r.Retrieve(context.Background(), "test query", 5, nil, RetrievalModeHybrid, WithRetrieveAlpha(0.8))
	require.NoError(t, err)
	assert.Equal(t, 0.8, store.lastAlpha)
}

// TestHybridRetriever_Retrieve_vector模式无embed 对齐 test_retrieve_vector_mode_without_embed_model
func TestHybridRetriever_Retrieve_vector模式无embed(t *testing.T) {
	r := NewHybridRetriever(&fakeVectorStore{}, nil)
	_, err := r.Retrieve(context.Background(), "test query", 5, nil, RetrievalModeVector)
	require.Error(t, err)
	var be *exception.BaseError
	require.True(t, errors.As(err, &be))
	assert.Equal(t, 155202, be.Code())
	assert.Contains(t, be.Message(), "embed_model is required")
}

// TestHybridRetriever_Retrieve_阈值过滤 对齐 test_retrieve_with_score_threshold
// （vector 模式 0.95/0.85 阈值 0.85 → 全保留，>= 边界）
func TestHybridRetriever_Retrieve_阈值过滤(t *testing.T) {
	store := &fakeVectorStore{
		searchResult: []common.SearchResult{
			{ID: "1", Text: "High", Score: 0.95, Metadata: map[string]any{}},
			{ID: "2", Text: "Low", Score: 0.85, Metadata: map[string]any{}},
		},
	}
	r := NewHybridRetriever(store, &fakeEmbedModel{queryVec: []float64{0.1}})
	threshold := 0.85
	results, err := r.Retrieve(context.Background(), "test query", 5, &threshold, RetrievalModeVector)
	require.NoError(t, err)
	for _, result := range results {
		assert.GreaterOrEqual(t, result.Score, 0.85)
	}
	assert.Len(t, results, 2) // >= 边界全保留
}

// TestHybridRetriever_Retrieve_阈值非vector模式 对齐 test_retrieve_score_threshold_invalid_mode
// （hybrid+threshold → 155201，先于 mode 分支的前置校验）
func TestHybridRetriever_Retrieve_阈值非vector模式(t *testing.T) {
	r, _, _ := newHybridFixture()
	threshold := 0.5
	_, err := r.Retrieve(context.Background(), "test query", 5, &threshold, RetrievalModeHybrid)
	require.Error(t, err)
	var be *exception.BaseError
	require.True(t, errors.As(err, &be))
	assert.Equal(t, 155201, be.Code())
	assert.Contains(t, be.Message(), "score_threshold is only supported")
}

// ── Go 增强（钉 Python 盲区）──

// TestHybridRetriever_Retrieve_mode零值回落 G-1："" → hybrid（spec §8-1 钉死）
func TestHybridRetriever_Retrieve_mode零值回落(t *testing.T) {
	r, store, _ := newHybridFixture()
	results, err := r.Retrieve(context.Background(), "test query", 5, nil, "")
	require.NoError(t, err)
	assert.Len(t, results, 2)
	assert.Equal(t, 1, store.hybridCalls)
}

// TestHybridRetriever_Retrieve_embed缺省纯稀疏 G-2：hybrid 模式无 embed →
// HybridSearch 收到 nil queryVector + embed_query 零调用
func TestHybridRetriever_Retrieve_embed缺省纯稀疏(t *testing.T) {
	store := &fakeVectorStore{
		hybridResult: []common.SearchResult{
			{ID: "1", Text: "Sparse-only", Score: 0.8, Metadata: map[string]any{}},
		},
	}
	r := NewHybridRetriever(store, nil)
	results, err := r.Retrieve(context.Background(), "test query", 5, nil, RetrievalModeHybrid)
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Nil(t, store.lastHybridVec)
}

// TestHybridRetriever_Retrieve_alpha未设回落构造值 G-3：构造 0.7 不传 opts → 0.7
func TestHybridRetriever_Retrieve_alpha未设回落构造值(t *testing.T) {
	r, store, _ := newHybridFixture0(0.7)
	_, err := r.Retrieve(context.Background(), "test query", 5, nil, RetrievalModeHybrid)
	require.NoError(t, err)
	assert.Equal(t, 0.7, store.lastAlpha)
}

// TestHybridRetriever_Retrieve_模式不支持文案 G-4：逐字符断言（与 Vector/Sparse 文案不同）
func TestHybridRetriever_Retrieve_模式不支持文案(t *testing.T) {
	r, _, _ := newHybridFixture()
	_, err := r.Retrieve(context.Background(), "test query", 5, nil, RetrievalMode("foo"))
	require.Error(t, err)
	var be *exception.BaseError
	require.True(t, errors.As(err, &be))
	assert.Equal(t, 155200, be.Code())
	assert.Contains(t, be.Message(), "Unsupported mode: foo")
}

// TestHybridRetriever_Retrieve_阈值下界过滤 G-5：阈值 0.9 → 0.85 被过滤（H6 只测全保留边界）
func TestHybridRetriever_Retrieve_阈值下界过滤(t *testing.T) {
	store := &fakeVectorStore{
		searchResult: []common.SearchResult{
			{ID: "1", Text: "High", Score: 0.95, Metadata: map[string]any{}},
			{ID: "2", Text: "Low", Score: 0.85, Metadata: map[string]any{}},
		},
	}
	r := NewHybridRetriever(store, &fakeEmbedModel{queryVec: []float64{0.1}})
	threshold := 0.9
	results, err := r.Retrieve(context.Background(), "test query", 5, &threshold, RetrievalModeVector)
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, "High", results[0].Text)
}

// TestHybridRetriever_Retrieve_HybridSearch错误透传 G-6：不包装原样返回
func TestHybridRetriever_Retrieve_HybridSearch错误透传(t *testing.T) {
	sentinel := errors.New("hybrid backend down")
	store := &fakeVectorStore{hybridErr: sentinel}
	r := NewHybridRetriever(store, &fakeEmbedModel{queryVec: []float64{0.1}})
	_, err := r.Retrieve(context.Background(), "q", 5, nil, RetrievalModeHybrid)
	require.Equal(t, sentinel, err)
}

// TestHybridRetriever_Retrieve_chunkID取主键 G-7：归一化 chunk_id 取 result.ID
// （Python 三作者不一致点钉死：Hybrid 同 Sparse 取主键，异于 Vector 的 metadata）
func TestHybridRetriever_Retrieve_chunkID取主键(t *testing.T) {
	r, _, _ := newHybridFixture()
	results, err := r.Retrieve(context.Background(), "test query", 5, nil, RetrievalModeHybrid)
	require.NoError(t, err)
	assert.Equal(t, "1", results[0].ChunkID)
	assert.Equal(t, "doc_1", results[0].DocID)
}

// TestHybridRetriever_Retrieve_默认alpha05 G-8：无构造无调用 → 0.5（Python 默认参数）
func TestHybridRetriever_Retrieve_默认alpha05(t *testing.T) {
	r, store, _ := newHybridFixture()
	_, err := r.Retrieve(context.Background(), "test query", 5, nil, RetrievalModeHybrid)
	require.NoError(t, err)
	assert.Equal(t, 0.5, store.lastAlpha)
}
```

fixture 辅助（alpha 构造值可变）加在 newHybridFixture 之后：

```go
// newHybridFixture0 指定构造 alpha 的 Hybrid 桩（alpha 覆写/回落用例）
func newHybridFixture0(alpha float64) (*HybridRetriever, *fakeVectorStore, *fakeEmbedModel) {
	store := &fakeVectorStore{
		hybridResult: []common.SearchResult{
			{ID: "1", Text: "Hybrid result 1", Score: 0.95, Metadata: map[string]any{"doc_id": "doc_1"}},
		},
	}
	embed := &fakeEmbedModel{queryVec: []float64{0.1}}
	r := NewHybridRetriever(store, embed, WithHybridAlpha(alpha))
	return r, store, embed
}
```

- [ ] **Step 4.2: 运行确认编译失败（红灯）**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/retrieval/retriever/ -run 'TestHybridRetriever_Retrieve' -v -count=1`
Expected: 编译失败 `r.Retrieve undefined`（HybridRetriever 无 Retrieve 方法）

- [ ] **Step 4.3: 实现 Retrieve（追加到 hybrid_retriever.go 导出函数区）**

```go
// Retrieve 混合检索（vector/sparse/hybrid 三模式路由 + 归一化）。
//
// 对齐 Python HybridRetriever.retrieve（L41-130）：
//   - alpha 解析：opts.Alpha 非 nil 覆写，否则回落构造字段
//   - 155201 前置校验先于 mode 分支（顺序不可调换——hybrid+threshold
//     报 155201 而非 155200）
//   - hybrid 分支 embed 可选（nil 走 store 层纯稀疏路径）
//   - vector 分支空结果 BM25 兜底（同 VectorRetriever）
//   - 阈值过滤仅 vector 模式生效
//   - 归一化 chunk_id 取 result.ID（同 Sparse，异于 Vector 的 metadata，
//     Python 三作者不一致字面复刻）
//
// 注意：Python 不向 store 传 filters（字面 None 复刻，kwargs 有也不透传）。
func (r *HybridRetriever) Retrieve(ctx context.Context, query string, topK int, scoreThreshold *float64, mode RetrievalMode, opts ...RetrieveOption) ([]common.RetrievalResult, error) {
	options := NewRetrieveOptions(opts...)
	alpha := r.alpha
	if options.Alpha != nil {
		alpha = *options.Alpha
	}

	// 155201 前置校验（Python L64-68，先于 mode 分支）
	if scoreThreshold != nil && mode != RetrievalModeVector {
		return nil, exception.BuildError(
			exception.StatusRetrievalRetrieverScoreThresholdInvalid,
			exception.WithParam("error_msg", "score_threshold is only supported when mode='vector'"),
		)
	}

	// mode 零值回落子类默认（spec §8-1：Python 签名默认 "hybrid"）
	if mode == "" {
		mode = RetrievalModeHybrid
	}

	var searchResults []common.SearchResult
	var err error
	switch mode {
	case RetrievalModeHybrid:
		// 混合检索：embed 可选，nil 向量走 store 层纯稀疏路径
		var queryVector []float64
		if r.embedModel != nil {
			queryVector, err = r.embedModel.EmbedQuery(ctx, query)
			if err != nil {
				return nil, err // Python await 直接冒泡，不包装
			}
		}
		searchResults, err = r.vectorStore.HybridSearch(ctx, query, queryVector, topK, alpha, nil)
		if err != nil {
			return nil, err
		}
	case RetrievalModeVector:
		// 纯向量检索：embed 必需
		if r.embedModel == nil {
			return nil, exception.BuildError(
				exception.StatusRetrievalRetrieverEmbedModelNotFound,
				exception.WithParam("error_msg", "embed_model is required for vector search"),
			)
		}
		queryVector, embedErr := r.embedModel.EmbedQuery(ctx, query)
		if embedErr != nil {
			return nil, embedErr
		}
		searchResults, err = r.vectorStore.Search(ctx, queryVector, topK, nil)
		if err != nil {
			return nil, err
		}
		// 向量检索空结果时兜底 BM25（对齐 Python fallback to sparse）
		if len(searchResults) == 0 {
			searchResults, err = r.vectorStore.SparseSearch(ctx, query, topK, nil)
			if err != nil {
				return nil, err
			}
		}
	case RetrievalModeSparse:
		// 纯稀疏检索（BM25）
		searchResults, err = r.vectorStore.SparseSearch(ctx, query, topK, nil)
		if err != nil {
			return nil, err
		}
	default:
		return nil, exception.BuildError(
			exception.StatusRetrievalRetrieverModeNotSupport,
			exception.WithParam("error_msg", fmt.Sprintf("Unsupported mode: %s", mode)),
		)
	}

	// 归一化循环（阈值过滤仅 vector 模式；Python result.score is not None
	// 的 nil 检查在 Go 无对应物，spec §8-4 延续）
	retrievalResults := make([]common.RetrievalResult, 0, len(searchResults))
	for _, result := range searchResults {
		if mode == RetrievalModeVector && scoreThreshold != nil && result.Score < *scoreThreshold {
			continue
		}
		retrievalResults = append(retrievalResults, common.RetrievalResult{
			Text:     result.Text,
			Score:    result.Score,
			Metadata: result.Metadata,
			DocID:    metadataString(result.Metadata, "doc_id"),
			ChunkID:  result.ID, // Hybrid 从主键取（Python 三作者不一致，spec 决策 7 延续）
		})
	}
	return retrievalResults, nil
}
```

import 块需补 `"fmt"`。

- [ ] **Step 4.4: 运行测试确认通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/retrieval/retriever/ -run 'TestHybridRetriever_Retrieve' -v -count=1`
Expected: 14 例 PASS（H1-H7 + G1-G8 中 Retrieve 部分）

- [ ] **Step 4.5: Commit**

```bash
git add internal/agentcore/retrieval/retriever/hybrid_retriever.go internal/agentcore/retrieval/retriever/hybrid_retriever_test.go
git commit -m "feat(retriever): 13.21 HybridRetriever.Retrieve 三模式路由+归一化

- 155201 前置校验首次消费（先于 mode 分支，顺序对齐 Python L64-68）
- alpha 调用覆写（opts.Alpha）优先于构造默认
- hybrid 分支 embed 可选走 store 纯稀疏；vector 分支 BM25 兜底
- chunk_id 取 result.ID（三作者不一致钉死）
- Python 8 用例中 7 例 1:1 + Go 增强 8 例"
```

---

### Task 5: BatchRetrieve + RetrieveSearchResults + Close + 断言

**Files:**
- Modify: `internal/agentcore/retrieval/retriever/hybrid_retriever.go`（追加三方法）
- Modify: `internal/agentcore/retrieval/retriever/base.go`（断言第三条）
- Modify: `internal/agentcore/retrieval/retriever/hybrid_retriever_test.go`（追加 H8 + rsr/Close 用例）

- [ ] **Step 5.1: 追加失败测试（H8 + rsr 三分支 + 不对称 + Close）**

```go
// TestHybridRetriever_BatchRetrieve 对齐 test_batch_retrieve
func TestHybridRetriever_BatchRetrieve(t *testing.T) {
	r, _, _ := newHybridFixture()
	results, err := r.BatchRetrieve(context.Background(), []string{"query 1", "query 2"}, 5)
	require.NoError(t, err)
	assert.Len(t, results, 2)
	assert.Len(t, results[0], 2)
	assert.Len(t, results[1], 2)
}

// TestHybridRetriever_BatchRetrieve_alpha透传 batch 的 alpha 经 opts 透传（对齐 **kwargs 展开调用）
func TestHybridRetriever_BatchRetrieve_alpha透传(t *testing.T) {
	r, store, _ := newHybridFixture0(0.3)
	_, err := r.BatchRetrieve(context.Background(), []string{"q1", "q2"}, 5, WithRetrieveAlpha(0.9))
	require.NoError(t, err)
	assert.Equal(t, 0.9, store.lastAlpha) // 批内每次调用都收到覆写值
}

// TestHybridRetriever_RetrieveSearchResults_hybrid模式 G-6：直接返回 SearchResult 不归一化
func TestHybridRetriever_RetrieveSearchResults_hybrid模式(t *testing.T) {
	r, store, _ := newHybridFixture()
	results, err := r.RetrieveSearchResults(context.Background(), "test query", 5, RetrievalModeHybrid)
	require.NoError(t, err)
	assert.Len(t, results, 2)
	assert.Equal(t, "1", results[0].ID) // 原始 ID，非归一化
	assert.Equal(t, 1, store.hybridCalls)
}

// TestHybridRetriever_RetrieveSearchResults_vector模式 G-6
func TestHybridRetriever_RetrieveSearchResults_vector模式(t *testing.T) {
	r, store, _ := newHybridFixture()
	results, err := r.RetrieveSearchResults(context.Background(), "test query", 5, RetrievalModeVector)
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, 1, store.searchCalls)
}

// TestHybridRetriever_RetrieveSearchResults_sparse模式 G-6
func TestHybridRetriever_RetrieveSearchResults_sparse模式(t *testing.T) {
	store := &fakeVectorStore{
		sparseResult: []common.SearchResult{
			{ID: "1", Text: "Sparse result", Score: 0.8, Metadata: map[string]any{}},
		},
	}
	r := NewHybridRetriever(store, nil)
	results, err := r.RetrieveSearchResults(context.Background(), "test query", 5, RetrievalModeSparse)
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, 1, store.sparseCalls)
}

// TestHybridRetriever_RetrieveSearchResults_无阈值校验 G-7：hybrid+threshold
// 调 rsr 不报 155201（Python retrieve 有前置校验、rsr 没有——同文件不对称，字面复刻）
func TestHybridRetriever_RetrieveSearchResults_无阈值校验(t *testing.T) {
	r, _, _ := newHybridFixture()
	threshold := 0.5
	results, err := r.RetrieveSearchResults(context.Background(), "test query", 5, RetrievalModeHybrid, WithRetrieveAlpha(0.5))
	require.NoError(t, err)
	_ = threshold
	_ = results
	// rsr 签名无 scoreThreshold 参数（对齐 Python rsr 签名本来就不收）——
	// 本用例真实断言点：rsr 传 alpha 不触发任何阈值校验路径
}

// TestHybridRetriever_Close_关闭底层 G-9
func TestHybridRetriever_Close_关闭底层(t *testing.T) {
	r, store, _ := newHybridFixture()
	r.Close()
	assert.Equal(t, 1, store.closeCalls)
}

// TestHybridRetriever_Close_nilStore不panic G-9 边界
func TestHybridRetriever_Close_nilStore不panic(t *testing.T) {
	r := NewHybridRetriever(nil, nil)
	assert.NotPanics(t, r.Close)
}
```

- [ ] **Step 5.2: 运行确认编译失败**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/retrieval/retriever/ -run 'TestHybridRetriever' -v -count=1`
Expected: 编译失败 `r.BatchRetrieve undefined` / `r.RetrieveSearchResults undefined` / `r.Close undefined`

- [ ] **Step 5.3: 实现三方法（追加到 hybrid_retriever.go）**

```go
// BatchRetrieve 批量并发检索。
//
// 对齐 Python 裸 asyncio.gather：全启动、首错传播、其余不取消
// （裸 errgroup 不派生 ctx 不 cancel，13.20 两兄弟同构）。
// alpha 经 opts 随 batch 透传（对齐 Python **kwargs 展开调用）。
func (r *HybridRetriever) BatchRetrieve(ctx context.Context, queries []string, topK int, opts ...RetrieveOption) ([][]common.RetrievalResult, error) {
	results := make([][]common.RetrievalResult, len(queries))
	var g errgroup.Group
	for i, q := range queries {
		g.Go(func() error {
			// Python batch_retrieve 不传 mode/score_threshold → 子类默认/None
			res, err := r.Retrieve(ctx, q, topK, nil, "", opts...)
			if err != nil {
				return err
			}
			results[i] = res
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	return results, nil
}

// RetrieveSearchResults 检索原始 SearchResult（不归一化、不阈值过滤）。
//
// 对齐 Python retrieve_search_results（L146-209）：三分支树与 Retrieve
// 完全相同，差异两处——无 155201 threshold 前置校验（Python rsr 没有
// 这段，同文件不对称字面复刻）、无归一化循环（直接返回）。
func (r *HybridRetriever) RetrieveSearchResults(ctx context.Context, query string, topK int, mode RetrievalMode, opts ...RetrieveOption) ([]common.SearchResult, error) {
	options := NewRetrieveOptions(opts...)
	alpha := r.alpha
	if options.Alpha != nil {
		alpha = *options.Alpha
	}

	// mode 零值回落子类默认（spec §8-1）
	if mode == "" {
		mode = RetrievalModeHybrid
	}

	switch mode {
	case RetrievalModeHybrid:
		var queryVector []float64
		var err error
		if r.embedModel != nil {
			queryVector, err = r.embedModel.EmbedQuery(ctx, query)
			if err != nil {
				return nil, err
			}
		}
		return r.vectorStore.HybridSearch(ctx, query, queryVector, topK, alpha, nil)
	case RetrievalModeVector:
		if r.embedModel == nil {
			return nil, exception.BuildError(
				exception.StatusRetrievalRetrieverEmbedModelNotFound,
				exception.WithParam("error_msg", "embed_model is required for vector search"),
			)
		}
		queryVector, err := r.embedModel.EmbedQuery(ctx, query)
		if err != nil {
			return nil, err
		}
		searchResults, err := r.vectorStore.Search(ctx, queryVector, topK, nil)
		if err != nil {
			return nil, err
		}
		if len(searchResults) == 0 {
			searchResults, err = r.vectorStore.SparseSearch(ctx, query, topK, nil)
			if err != nil {
				return nil, err
			}
		}
		return searchResults, nil
	case RetrievalModeSparse:
		return r.vectorStore.SparseSearch(ctx, query, topK, nil)
	default:
		return nil, exception.BuildError(
			exception.StatusRetrievalRetrieverModeNotSupport,
			exception.WithParam("error_msg", fmt.Sprintf("Unsupported mode: %s", mode)),
		)
	}
}

// Close 关闭检索器。
//
// 对齐 Python close()：if self.vector_store 判空后关底层。
// 鸭子探测消解为接口静态 Close() 直调（spec §8-7，同 13.20 两兄弟）。
func (r *HybridRetriever) Close() {
	if r.vectorStore != nil {
		r.vectorStore.Close()
	}
}
```

- [ ] **Step 5.4: base.go 追加断言第三条（此刻转绿）**

```go
// 编译期断言：三实现满足 Retriever 接口（13.17 先例）。
var (
	_ Retriever = (*VectorRetriever)(nil)
	_ Retriever = (*SparseRetriever)(nil)
	_ Retriever = (*HybridRetriever)(nil)
)
```

- [ ] **Step 5.5: 运行全部 Hybrid 测试通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/retrieval/retriever/ -run 'TestHybridRetriever' -v -count=1`
Expected: 22 例 PASS（Task 4 的 14 + 本 Task 8）

- [ ] **Step 5.6: Commit**

```bash
git add internal/agentcore/retrieval/retriever/hybrid_retriever.go internal/agentcore/retrieval/retriever/hybrid_retriever_test.go internal/agentcore/retrieval/retriever/base.go
git commit -m "feat(retriever): 13.21 HybridRetriever Batch/rsr/Close + 接口断言

- BatchRetrieve 裸 errgroup + alpha 经 opts 透传
- rsr 三分支字面复刻第二份（无 threshold 校验——Python 同文件不对称字面复刻）
- Close nil 防御 + 接口静态直调（鸭子探测消解）
- 编译期断言第三条，Python 8 用例全兑现"
```

---

### Task 6: doc.go + 回填清单（防丢失 8 项）

**Files:**
- Modify: `internal/agentcore/retrieval/retriever/doc.go`
- Modify: `internal/agentcore/retrieval/retriever/vector_retriever.go:57-59`（注释兑现）
- Modify: `IMPLEMENTATION_PLAN.md`（13.21 行 + 13.19 修订注记）
- Modify: `docs/superpowers/specs/2026-10-27-retriever-13.19-13.20-design.md`（修订记录段）

- [ ] **Step 6.1: doc.go 增补**

文件目录加一行（sparse_retriever.go 之后）：

```
//	retriever/
//	├── doc.go                   # 包文档
//	├── base.go                  # Retriever 接口 + RetrievalMode + RetrieveOptions（13.21 平铺 Alpha）
//	├── vector_retriever.go      # VectorRetriever 向量检索器
//	├── sparse_retriever.go      # SparseRetriever 稀疏检索器（BM25）
//	└── hybrid_retriever.go      # HybridRetriever 混合检索器（向量+稀疏）
```

Python 参考路径段追加一行：`openjiuwen/core/retrieval/retriever/hybrid_retriever.py`；核心类型索引追加：`HybridRetriever — 混合检索器实现（alpha 权重路由）`。

- [ ] **Step 6.2: 13.20 注释兑现（回填点②）**

vector_retriever.go L57-59：

```go
	// Python L65-69 的 155201（threshold+mode≠vector）为死分支（前置 mode 校验
	// 先抛 155200），跳过归档 spec §8-2；已由 13.21 HybridRetriever 真实消费
```

（仅把"155201 留 13.21 HybridRetriever 真实消费"改为"已由 13.21 HybridRetriever 真实消费"。）

- [ ] **Step 6.3: IMPLEMENTATION_PLAN.md 两处回填**

13.21 行：

```markdown
| 13.21 | ✅ | HybridRetriever | `HybridRetriever(三模式路由 retrieve/rsr 两份字面复刻 + 155201 前置校验首次消费 + alpha 构造默认0.5/调用opts覆写 + vector分支BM25兜底 + chunk_id取result.ID三作者不一致复刻 + Batch裸errgroup alpha透传 + Close鸭子探测消解) + RetrieveOptions平铺修订(Extra map→Alpha *float64 编译期类型安全,13.19原描述保留) + WithRetrieveAlpha/WithHybridAlpha选项 + fakeVectorStore.HybridSearch桩可编程化；偏差8条归档spec §8` | `openjiuwen/core/retrieval/retriever/hybrid_retriever.py` |
```

13.19 行：在"`RetrieveOptions.Extra 收纳 **kwargs(本节收而不用对齐 Python 死参数,13.21 Hybrid 消费 alpha/13.27 SimpleKB 写 filters)`"后追加"（13.21 修订：平铺为 Alpha *float64 类型化字段）"——**原描述整段保留不删**。

- [ ] **Step 6.4: 13.19 spec 追加修订记录段**

文件末尾追加：

```markdown
## 修订记录

- 2026-10-28（13.21）：RetrieveOptions.Extra map[string]any 平铺为类型化字段
  Alpha *float64（编译期类型安全；kwargs 闭合世界偏差归档 13.21 spec §8-1）。
  WithRetrieveExtra 删除（生产零调用）→ WithRetrieveAlpha 新增。本 spec §4.1
  原设计描述保留作为历史记录，以 13.21 spec 为准。
```

- [ ] **Step 6.5: 验证 spec §9 回填清单逐项核对**

对照 `docs/superpowers/specs/2026-10-28-hybridretriever-13.21-design.md` §9 的 8 项：1✅(Step 6.3) 2✅(Step 6.3) 3✅(Step 6.2) 4✅(155201 已消费随 13.21 行产出描述) 5✅(Step 6.1) 6✅(Step 6.4) 7✅(验证点行/跨域联动行不动——本步核对其未被误改) 8✅(各 Task 显式 add)。

- [ ] **Step 6.6: Commit**

```bash
git add internal/agentcore/retrieval/retriever/doc.go internal/agentcore/retrieval/retriever/vector_retriever.go IMPLEMENTATION_PLAN.md docs/superpowers/specs/2026-10-27-retriever-13.19-13.20-design.md
git commit -m "docs(retriever): 13.21 回填清单兑现（8 项防丢失）

- doc.go 文件目录+索引；13.20 注释 155201 兑现
- IMPLEMENTATION_PLAN 13.21 ✅ + 13.19 平铺修订注记（原描述保留）
- 13.19 spec 修订记录段"
```

注意：Step 6.3 编辑 IMPLEMENTATION_PLAN.md 时工作区可能存在他任务（10.6.24 拆分）的未提交改动——**只改 13.21/13.19 两行，用 Edit 精确替换，git add 时该文件整体加入会把 10.6.24 行也带上**。处理：add 前先 `git diff IMPLEMENTATION_PLAN.md` 核对，若 10.6.24 改动仍在，改用 `git add -p` 或与用户确认后合提（按 git-am 陷阱记忆：显式核对事后 stat）。

---

### Task 7: 全量回归 + 覆盖率

**Files:** 无新文件

- [ ] **Step 7.1: 包级全量测试 + 覆盖率**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/retrieval/retriever/ -cover -count=1`
Expected: 全 PASS，覆盖率 ≥90%（13.20 基线 94.7%，纯增量预期持平或略升）

- [ ] **Step 7.2: CI 同款标签全量回归**

Run: `cd /home/opensource/uapclaw-gateway && CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./... 2>&1 | tail -30`
Expected: 全 PASS（memory 包 FTS 假失败教训——必须带标签）

- [ ] **Step 7.3: 若覆盖率 <90% 补测试**

用 `go test ./internal/agentcore/retrieval/retriever/ -coverprofile=/tmp/cov.out && go tool cover -func=/tmp/cov.out | grep -v '100%'` 定位缺口，按 spec §7.3 清单补齐后重复 Step 7.1。

- [ ] **Step 7.4: 最终 Commit（如有补充）**

```bash
git add -A internal/agentcore/retrieval/retriever/
git commit -m "test(retriever): 13.21 覆盖率补齐至 ≥90%"
```

（此命令仅当 Step 7.3 产生了新改动才执行；`-A` 限定目录路径，不碰工作区其他文件。）

---

## Self-Review 记录

1. **Spec 覆盖**：§4.1/§4.2→Task 1/3；§5.1→Task 4；§5.2-5.4→Task 5；§7.1 桩→Task 2；§7.2 H1-H8→Task 4(H1-H7)+Task 5(H8)；§7.3 G1-G9→Task 4(G1-G8)+Task 5(G6/G7/G9)；§9 回填 8 项→Task 6；§10 文件清单→各 Task Files。无缺口。
2. **占位符扫描**：无 TBD/TODO；所有代码块完整可编译（Step 4.1 的 `[]float64{0.1}*384` 已显式标注为非法语法并给出正确写法）。
3. **类型一致性**：HybridRetrieverOption/WithHybridAlpha/WithRetrieveAlpha/Alpha *float64 全计划统一；fakeVectorStore 桩字段（hybridResult/hybridErr/hybridCalls/lastAlpha/lastHybridVec）Task 2 定义、Task 4/5 消费一致；`newHybridFixture0` 在 Step 4.1 定义并被 H4/G3/G5(增强) 复用。
4. **已知注意点**：Task 6 Step 6.6 的 IMPLEMENTATION_PLAN.md 共享工作区风险已在步骤内写明处理办法；Task 3 Step 3.2 的断言时点问题已显式修正为 Task 5 Step 5.4。
