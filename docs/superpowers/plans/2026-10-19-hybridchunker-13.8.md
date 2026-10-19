# HybridChunker（13.8）Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 HybridChunker 混合分块器（no_split_when 谓词单块保留 + inner chunker 委托），并回填 13.5 留下的 hybridFactory 占位。

**Architecture:** 嵌入 BaseChunker 与兄弟 chunker 同构；inner 尺寸经 DimsProvider 接口断言提取（等价 Python getattr 反射），失败 fallback 512/50；完全覆写 ChunkDocuments、局部覆写 Process（Python 动态分发的 Go 手工等价物）；hybridFactory inner 缺省分支构造并保留默认 CharChunker。

**Tech Stack:** Go 1.x、testify/assert、google/uuid。零新增外部依赖。

**设计文档:** `docs/superpowers/specs/2026-10-19-hybridchunker-13.8-design.md`

**Python 参考源码:** `/home/opensource/agent-core/openjiuwen/core/retrieval/indexing/processor/chunker/hybrid_chunker.py`（89 行全文已核对）+ 同目录 `__init__.py` 的 `_hybrid_factory`（L29-59）

**编码规范提醒（全程适用）:**
- 所有注释中文；文件内声明按 结构体→枚举→常量→全局变量→导出函数→非导出函数 排列，分隔注释 `// ────…` 
- 每步编译前 `pgrep -f 'go (build|test)'` 检查残留进程，有则 kill；同一时间只跑一个 go 进程
- 测试命令统一在项目根 `/home/opensource/uapclaw-gateway` 执行
- 覆盖率目标：chunker 包 ≥85%（现状 92.4%，不得拉低）

---

### Task 1: base.go 追加 DimsProvider 接口与 ChunkDims 方法

**Files:**
- Modify: `internal/agentcore/retrieval/indexing/processor/chunker/base.go`（结构体区块 Chunker 接口之后、BaseChunker 之前插入接口；导出函数区块末尾追加方法）
- Test: `internal/agentcore/retrieval/indexing/processor/chunker/base_test.go`（文件末尾追加）

- [ ] **Step 1.1: 写失败测试**

在 `base_test.go` 文件末尾追加：

```go
// TestDimsProvider_接口满足性 嵌入 BaseChunker 的实现自动满足 DimsProvider
// （方法提升），对齐 Python getattr(inner, "chunk_size", 512) 能取到属性
func TestDimsProvider_接口满足性(t *testing.T) {
	fc := newFakeChunker(t, 2048, 100, nil)
	p, ok := fc.(DimsProvider)
	assert.True(t, ok, "嵌入 BaseChunker 的实现必须满足 DimsProvider")
	size, overlap := p.ChunkDims()
	assert.Equal(t, 2048, size)
	assert.Equal(t, 100, overlap)
}

// TestChunkDims_返回构造值 纯 BaseChunker 直接调用也满足（防御路径）
func TestChunkDims_返回构造值(t *testing.T) {
	bc, err := NewBaseChunker(300, 30)
	assert.NoError(t, err)
	assert.Equal(t, 300, bc.ChunkSize)
	assert.Equal(t, 30, bc.ChunkOverlap)
	size, overlap := bc.ChunkDims()
	assert.Equal(t, 300, size)
	assert.Equal(t, 30, overlap)
}
```

- [ ] **Step 1.2: 跑测试确认失败**

```bash
pgrep -f 'go (build|test)' || true
go test ./internal/agentcore/retrieval/indexing/processor/chunker/ -run 'TestDimsProvider|TestChunkDims' -v 2>&1 | tail -20
```

预期：编译失败，报 `undefined: DimsProvider`（ChunkDims 方法尚未定义时第一个测试也编译失败）。

- [ ] **Step 1.3: 写实现**

`base.go` 结构体区块中，`Chunker` 接口定义结束后、`BaseChunker` 定义之前插入：

```go
// DimsProvider 可选尺寸提供者。
//
// 嵌入 BaseChunker 的分块器自动满足本接口（方法提升），未嵌入的纯接口
// 实现不满足——对齐 Python getattr(inner, "chunk_size", 512) 取不到属性
// 时走默认值的语义（hybrid 构造时断言探测，13.8）。
type DimsProvider interface {
	// ChunkDims 返回 (chunkSize, chunkOverlap)。
	ChunkDims() (int, int)
}
```

导出函数区块末尾（`Length` 方法之后、`──────── 非导出函数` 分隔注释之前）追加：

```go
// ChunkDims 返回 (ChunkSize, ChunkOverlap)，使嵌入方满足 DimsProvider。
//
// Python: base.py Chunker 的 chunk_size/chunk_overlap 属性（getattr 读取点）
func (b *BaseChunker) ChunkDims() (int, int) {
	return b.ChunkSize, b.ChunkOverlap
}
```

- [ ] **Step 1.4: 跑测试确认通过**

```bash
go test ./internal/agentcore/retrieval/indexing/processor/chunker/ -run 'TestDimsProvider|TestChunkDims' -v 2>&1 | tail -10
```

预期：2 个测试 PASS。

- [ ] **Step 1.5: 全包回归**

```bash
go test ./internal/agentcore/retrieval/indexing/processor/chunker/ 2>&1 | tail -3
```

预期：全部 PASS（纯增量，13.5/13.6/13.7 既有测试不受影响）。

- [ ] **Step 1.6: 提交**

```bash
git add internal/agentcore/retrieval/indexing/processor/chunker/base.go internal/agentcore/retrieval/indexing/processor/chunker/base_test.go
git commit -m "feat(chunker): DimsProvider 可选尺寸接口 + BaseChunker.ChunkDims（13.8 前置）"
```

---

### Task 2: base.go 提取 mergeChunkMetadata 私有函数

**Files:**
- Modify: `internal/agentcore/retrieval/indexing/processor/chunker/base.go`（ChunkDocuments 内联合并逻辑 L114-120 提取为非导出函数）
- Test: 无新增测试（行为不变的重构，由既有 TestBaseChunker_ChunkDocuments默认实现 回归覆盖）

- [ ] **Step 2.1: 提取函数**

`base.go` 的 `ChunkDocuments` 中，将：

```go
		for i, text := range texts {
			uid := uuid.NewString()
			meta := make(map[string]any, len(doc.Metadata)+3)
			for k, v := range doc.Metadata {
				meta[k] = v
			}
			meta["chunk_index"] = i
			meta["total_chunks"] = len(texts)
			meta["chunk_id"] = uid
			chunks = append(chunks, common.TextChunk{
```

替换为：

```go
		for i, text := range texts {
			uid := uuid.NewString()
			meta := mergeChunkMetadata(doc.Metadata, i, len(texts), uid)
			chunks = append(chunks, common.TextChunk{
```

非导出函数区块末尾（`chunkTextOf` 之后）追加：

```go
// mergeChunkMetadata 合并溯源元数据：复制 doc.Metadata 后叠加
// chunk_index/total_chunks/chunk_id 三键。
//
// Python: base.py chunk_documents 与 hybrid_chunker.py chunk_documents
// 两处同型的 {**doc.metadata, ...} 字典展开
func mergeChunkMetadata(docMeta map[string]any, chunkIndex, totalChunks int, chunkID string) map[string]any {
	meta := make(map[string]any, len(docMeta)+3)
	for k, v := range docMeta {
		meta[k] = v
	}
	meta["chunk_index"] = chunkIndex
	meta["total_chunks"] = totalChunks
	meta["chunk_id"] = chunkID
	return meta
}
```

- [ ] **Step 2.2: 全包回归（行为不变验证）**

```bash
go test ./internal/agentcore/retrieval/indexing/processor/chunker/ 2>&1 | tail -3
```

预期：全部 PASS（重构不改行为，metadata 断言由既有测试逐键核对）。

- [ ] **Step 2.3: 提交**

```bash
git add internal/agentcore/retrieval/indexing/processor/chunker/base.go
git commit -m "refactor(chunker): 提取 mergeChunkMetadata 供 base/hybrid 共用"
```

---

### Task 3: HybridChunker 实体（构造 + 三方法）

**Files:**
- Create: `internal/agentcore/retrieval/indexing/processor/chunker/hybrid_chunker.go`
- Test: Create: `internal/agentcore/retrieval/indexing/processor/chunker/hybrid_chunker_test.go`

- [ ] **Step 3.1: 写失败测试**

创建 `hybrid_chunker_test.go`：

```go
package chunker

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/reranker"
)

// ──────────────────────────── 结构体 ────────────────────────────

// fakePlainChunker 不嵌入 BaseChunker 的纯接口实现。
//
// 用于验证 DimsProvider 断言失败分支（对齐 Python getattr 取不到
// chunk_size 属性时 fallback 512/50）。
type fakePlainChunker struct{}

// ChunkText 原样返回单块
func (f *fakePlainChunker) ChunkText(text string) ([]string, error) {
	return []string{text}, nil
}

// ChunkDocuments 单文档单块
func (f *fakePlainChunker) ChunkDocuments(ctx context.Context, docs []reranker.Document) ([]commonTextChunkAlias, error) {
	return nil, errors.New("not used")
}

// Process 等价 ChunkDocuments
func (f *fakePlainChunker) Process(ctx context.Context, docs []reranker.Document) ([]commonTextChunkAlias, error) {
	return f.ChunkDocuments(ctx, docs)
}

// fakeRecordingInner 记录委托调用的 inner，用于验证逐文档单元素委托
type fakeRecordingInner struct {
	*BaseChunker
	calledWith [][]reranker.Document // 每次委托传入的文档切片快照
}

// ChunkText 原样返回单块
func (f *fakeRecordingInner) ChunkText(text string) ([]string, error) {
	return []string{text}, nil
}

// ChunkDocuments 记录入参后走 BaseChunker 默认实现
func (f *fakeRecordingInner) ChunkDocuments(ctx context.Context, docs []reranker.Document) ([]common.TextChunk, error) {
	f.calledWith = append(f.calledWith, docs)
	return f.BaseChunker.ChunkDocuments(ctx, docs)
}

// newFakeRecordingInner 构造注入 self 的记录型 inner
func newFakeRecordingInner(t *testing.T) *fakeRecordingInner {
	t.Helper()
	bc, err := NewBaseChunker(100, 10)
	assert.NoError(t, err)
	inner := &fakeRecordingInner{BaseChunker: bc}
	bc.SetSelf(inner)
	return inner
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestNewHybridChunker_inner为nil报错 Go 侧防御（Python 由类型标注保证非空）
func TestNewHybridChunker_inner为nil报错(t *testing.T) {
	h, err := NewHybridChunker(nil)
	assert.Nil(t, h)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "inner chunker is required")
}

// TestNewHybridChunker_尺寸继承自inner 对齐 Python getattr(inner_chunker, "chunk_size", 512)
func TestNewHybridChunker_尺寸继承自inner(t *testing.T) {
	inner, err := NewCharChunker(WithChunkSize(2048), WithChunkOverlap(100))
	assert.NoError(t, err)
	h, err := NewHybridChunker(inner)
	assert.NoError(t, err)
	assert.Equal(t, 2048, h.ChunkSize)
	assert.Equal(t, 100, h.ChunkOverlap)
}

// TestNewHybridChunker_纯接口实现fallback512和50 对齐 Python getattr 取不到属性走默认值
func TestNewHybridChunker_纯接口实现fallback512和50(t *testing.T) {
	h, err := NewHybridChunker(&fakePlainChunker{})
	assert.NoError(t, err)
	assert.Equal(t, 512, h.ChunkSize)
	assert.Equal(t, 50, h.ChunkOverlap)
}

// TestNewHybridChunker_谓词缺省生效 noSplitWhen 可变参数空 → defaultNoSplit
func TestNewHybridChunker_谓词缺省生效(t *testing.T) {
	inner, _ := NewCharChunker(WithChunkSize(10), WithChunkOverlap(2))
	h, err := NewHybridChunker(inner)
	assert.NoError(t, err)
	// source_type=row 文档应单块（默认谓词生效的间接验证，直接验证见 TestDefaultNoSplit）
	docs := []reranker.Document{{
		ID:   "excel-row",
		Text: "姓名: 张三, 年龄: 25",
		Metadata: map[string]any{"source_type": "row"},
	}}
	chunks, err := h.ChunkDocuments(context.Background(), docs)
	assert.NoError(t, err)
	assert.Len(t, chunks, 1)
}

// TestDefaultNoSplit 对齐 Python _default_no_split：
// (doc.metadata or {}).get("source_type") in ("row", "column")
func TestDefaultNoSplit(t *testing.T) {
	cases := []struct {
		name     string
		metadata map[string]any
		want     bool
	}{
		{"row命中", map[string]any{"source_type": "row"}, true},
		{"column命中", map[string]any{"source_type": "column"}, true},
		{"其他类型不命中", map[string]any{"source_type": "paragraph"}, false},
		{"无source_type键", map[string]any{"k": "v"}, false},
		{"metadata为nil", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc := &reranker.Document{Metadata: tc.metadata}
			assert.Equal(t, tc.want, defaultNoSplit(doc))
		})
	}
}

// TestHybridChunker_ChunkText纯委托 对齐 Python chunk_text = self._inner.chunk_text
func TestHybridChunker_ChunkText纯委托(t *testing.T) {
	inner, err := NewCharChunker(WithChunkSize(10), WithChunkOverlap(2))
	assert.NoError(t, err)
	h, err := NewHybridChunker(inner)
	assert.NoError(t, err)

	viaHybrid, err := h.ChunkText("This is a test text")
	assert.NoError(t, err)
	viaInner, err := inner.ChunkText("This is a test text")
	assert.NoError(t, err)
	assert.Equal(t, viaInner, viaHybrid)
}

// TestHybridChunker_ChunkDocuments_谓词命中单块 对齐 Python L69-83：
// 命中且文本非空 → 1 块，text 为 strip 后值，metadata 三键规范
func TestHybridChunker_ChunkDocuments_谓词命中单块(t *testing.T) {
	inner, _ := NewCharChunker(WithChunkSize(10), WithChunkOverlap(2))
	h, _ := NewHybridChunker(inner)
	docs := []reranker.Document{{
		ID:   "excel-col",
		Text: "  Column name: 年龄. Values: 25, 30  ",
		Metadata: map[string]any{"source_type": "column", "sheet_name": "Sheet1"},
	}}
	chunks, err := h.ChunkDocuments(context.Background(), docs)
	assert.NoError(t, err)
	assert.Len(t, chunks, 1)
	c := chunks[0]
	assert.Equal(t, "Column name: 年龄. Values: 25, 30", c.Text, "单块文本必须 strip")
	assert.Equal(t, "excel-col", c.DocID)
	assert.Equal(t, "Sheet1", c.Metadata["sheet_name"], "原 metadata 必须保留")
	assert.Equal(t, 0, c.Metadata["chunk_index"])
	assert.Equal(t, 1, c.Metadata["total_chunks"])
	assert.Equal(t, c.ID, c.Metadata["chunk_id"])
	assert.NotEmpty(t, c.ID)
}

// TestHybridChunker_ChunkDocuments_谓词命中但空白文本 对齐 Python
// (doc.text or "").strip() 为空 → 走 inner（CharChunker 空文本产 0 块）
func TestHybridChunker_ChunkDocuments_谓词命中但空白文本(t *testing.T) {
	inner, _ := NewCharChunker(WithChunkSize(10), WithChunkOverlap(2))
	h, _ := NewHybridChunker(inner)
	docs := []reranker.Document{{
		ID:       "empty-row",
		Text:     "   \n\t  ",
		Metadata: map[string]any{"source_type": "row"},
	}}
	chunks, err := h.ChunkDocuments(context.Background(), docs)
	assert.NoError(t, err)
	assert.Empty(t, chunks, "空白文本走 inner 后产出 0 块")
}

// TestHybridChunker_ChunkDocuments_谓词不命中走inner 普通文档正常分块（多块+inner metadata 规范）
func TestHybridChunker_ChunkDocuments_谓词不命中走inner(t *testing.T) {
	inner, _ := NewCharChunker(WithChunkSize(10), WithChunkOverlap(2))
	h, _ := NewHybridChunker(inner)
	docs := []reranker.Document{{
		ID:       "word-doc",
		Text:     "This is a test text",
		Metadata: map[string]any{"source_type": "paragraph"},
	}}
	chunks, err := h.ChunkDocuments(context.Background(), docs)
	assert.NoError(t, err)
	assert.Len(t, chunks, 3, "19 字符 size=10 overlap=2 → 3 块")
	for i, c := range chunks {
		assert.Equal(t, "word-doc", c.DocID)
		assert.Equal(t, i, c.Metadata["chunk_index"])
		assert.Equal(t, 3, c.Metadata["total_chunks"])
		assert.Equal(t, c.ID, c.Metadata["chunk_id"])
	}
}

// TestHybridChunker_ChunkDocuments_空metadata 对齐 Python (doc.metadata or {}) 防御
func TestHybridChunker_ChunkDocuments_空metadata(t *testing.T) {
	inner, _ := NewCharChunker(WithChunkSize(10), WithChunkOverlap(2))
	h, _ := NewHybridChunker(inner)
	docs := []reranker.Document{{
		ID:       "no-meta",
		Text:     "plain text",
		Metadata: nil,
	}}
	chunks, err := h.ChunkDocuments(context.Background(), docs)
	assert.NoError(t, err)
	assert.Len(t, chunks, 1)
	assert.Equal(t, 0, chunks[0].Metadata["chunk_index"])
	assert.Equal(t, 1, chunks[0].Metadata["total_chunks"])
}

// TestHybridChunker_ChunkDocuments_混合列表 分支交错：row + 长文 + column + 空白 row
func TestHybridChunker_ChunkDocuments_混合列表(t *testing.T) {
	inner := newFakeRecordingInner(t)
	h, _ := NewHybridChunker(inner)
	docs := []reranker.Document{
		{ID: "r1", Text: "姓名: 张三", Metadata: map[string]any{"source_type": "row"}},
		{ID: "w1", Text: "This is a test text", Metadata: map[string]any{"source_type": "paragraph"}},
		{ID: "c1", Text: "Column name: 年龄. Values: 25", Metadata: map[string]any{"source_type": "column"}},
		{ID: "r2", Text: "  ", Metadata: map[string]any{"source_type": "row"}},
	}
	chunks, err := h.ChunkDocuments(context.Background(), docs)
	assert.NoError(t, err)
	// r1 单块 + w1 经 fakeRecordingInner（ChunkText 原样单块 → 1 块）+ c1 单块 + r2 空白走 inner 产 0 块
	assert.Len(t, chunks, 3)
	assert.Equal(t, "r1", chunks[0].DocID)
	assert.Equal(t, 0, chunks[0].Metadata["chunk_index"])
	assert.Equal(t, 1, chunks[0].Metadata["total_chunks"])
	assert.Equal(t, "w1", chunks[1].DocID)
	assert.Equal(t, "c1", chunks[2].DocID)

	// 逐文档单元素委托验证：w1 和 r2 两次调用，每次恰好 1 个文档
	assert.Len(t, inner.calledWith, 2)
	assert.Equal(t, []reranker.Document{docs[1]}, inner.calledWith[0])
	assert.Equal(t, []reranker.Document{docs[3]}, inner.calledWith[1])
}

// TestHybridChunker_ChunkDocuments_inner错误传播 inner 分块失败立即中断
func TestHybridChunker_ChunkDocuments_inner错误传播(t *testing.T) {
	inner := newFakeChunker(t, 100, 10, errors.New("inner boom"))
	h, err := NewHybridChunker(inner)
	assert.NoError(t, err)
	docs := []reranker.Document{
		{ID: "ok", Text: "姓名: 张三", Metadata: map[string]any{"source_type": "row"}},
		{ID: "bad", Text: "will fail"},
	}
	chunks, err := h.ChunkDocuments(context.Background(), docs)
	assert.Nil(t, chunks)
	assert.EqualError(t, err, "inner boom")
}

// TestHybridChunker_Process局部覆写 决策 3 回归：Process 入口不绕过谓词判定
// （BaseChunker.Process 静态解析到默认 ChunkDocuments，hybrid 必须局部覆写）
func TestHybridChunker_Process局部覆写(t *testing.T) {
	inner, _ := NewCharChunker(WithChunkSize(10), WithChunkOverlap(2))
	h, _ := NewHybridChunker(inner)
	docs := []reranker.Document{{
		ID:       "excel-row",
		Text:     "姓名: 张三, 年龄: 25",
		Metadata: map[string]any{"source_type": "row"},
	}}
	viaProcess, err := h.Process(context.Background(), docs)
	assert.NoError(t, err)
	assert.Len(t, viaProcess, 1, "Process 入口必须路由到覆写版 ChunkDocuments（谓词生效）")
	assert.Equal(t, "姓名: 张三, 年龄: 25", viaProcess[0].Text)
	assert.Equal(t, 1, viaProcess[0].Metadata["total_chunks"])
}

// TestHybridChunker_自定义谓词覆盖 对齐 Python no_split_when=lambda doc: ...
func TestHybridChunker_自定义谓词覆盖(t *testing.T) {
	inner, _ := NewCharChunker(WithChunkSize(10), WithChunkOverlap(2))
	// 自定义谓词：标题文档不切
	h, err := NewHybridChunker(inner, func(doc *reranker.Document) bool {
		_, ok := doc.Metadata["is_title"]
		return ok
	})
	assert.NoError(t, err)
	docs := []reranker.Document{
		{ID: "title", Text: "第一章 概述", Metadata: map[string]any{"is_title": true}},
		{ID: "excel", Text: "姓名: 张三", Metadata: map[string]any{"source_type": "row"}},
	}
	chunks, err := h.ChunkDocuments(context.Background(), docs)
	assert.NoError(t, err)
	// title 单块（自定义谓词命中）；excel 走 inner（默认谓词被覆盖不再生效）
	assert.Len(t, chunks, 2)
	assert.Equal(t, "title", chunks[0].DocID)
	assert.Equal(t, 1, chunks[0].Metadata["total_chunks"])
	assert.Equal(t, "excel", chunks[1].DocID)
}
```

**注意**：上面 `fakePlainChunker` 里写的 `commonTextChunkAlias` 是占位符——实现时直接写全类型 `[]common.TextChunk`（import `common` 包），并删掉本行说明。

- [ ] **Step 3.2: 跑测试确认失败**

```bash
pgrep -f 'go (build|test)' || true
go test ./internal/agentcore/retrieval/indexing/processor/chunker/ -run 'Hybrid|DefaultNoSplit' -v 2>&1 | tail -20
```

预期：编译失败，报 `undefined: NewHybridChunker` / `undefined: defaultNoSplit`。

- [ ] **Step 3.3: 写实现**

创建 `hybrid_chunker.go`：

```go
package chunker

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/reranker"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
)

// ──────────────────────────── 结构体 ────────────────────────────

// HybridChunker 混合分块器。
//
// 命中 noSplitWhen 谓词的文档整体保留为单块（默认谓词把表格行/列
// metadata.source_type ∈ {"row","column"} 视为单块，保护 Excel 语义
// 完整性），其余文档委托 inner chunker 正常分块。"hybrid" 指分块
// 策略混合，与检索层 hybrid search 无关。
//
// 嵌入 BaseChunker 仅为复用尺寸字段与构造校验；ChunkDocuments 为
// 完全覆写（不走基类默认实现），Process 为局部覆写（决策 3）。
//
// Python: openjiuwen/core/retrieval/indexing/processor/chunker/hybrid_chunker.py (HybridChunker)
type HybridChunker struct {
	// BaseChunker 尺寸字段（继承自 inner）+ 构造校验
	*BaseChunker
	// inner 委托目标（任何 Chunker 实现）
	inner Chunker
	// noSplitWhen 单块判定谓词，nil 时 defaultNoSplit
	noSplitWhen func(*reranker.Document) bool
}

// ──────────────────────────── 枚举 ────────────────────────────
// ──────────────────────────── 常量 ────────────────────────────

const (
	// fallbackChunkSize inner 不提供尺寸时的默认分块大小
	//（对齐 Python getattr(inner_chunker, "chunk_size", 512)）
	fallbackChunkSize = 512
	// fallbackChunkOverlap inner 不提供尺寸时的默认重叠
	//（对齐 Python getattr(inner_chunker, "chunk_overlap", 50)）
	fallbackChunkOverlap = 50
)

// ──────────────────────────── 全局变量 ────────────────────────────

// 编译期断言 HybridChunker 实现 Chunker
var _ Chunker = (*HybridChunker)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewHybridChunker 构造混合分块器。
//
// 尺寸提取（对齐 Python __init__ 的 getattr 反射）：inner 满足
// DimsProvider（嵌入 BaseChunker 的实现）→ 取其真实尺寸；纯接口实现
// 断言失败 → fallback 512/50。随后过 NewBaseChunker 校验（inner 尺寸
// 已合法时恒通过，保留为构造结构对称）。
//
// noSplitWhen 可变参数空 → nil → defaultNoSplit（对齐 Python
// no_split_when or _default_no_split）。
//
// Python: hybrid_chunker.py HybridChunker.__init__
func NewHybridChunker(inner Chunker, noSplitWhen ...func(*reranker.Document) bool) (*HybridChunker, error) {
	if inner == nil {
		return nil, fmt.Errorf("inner chunker is required for HybridChunker")
	}
	size, overlap := fallbackChunkSize, fallbackChunkOverlap
	if p, ok := inner.(DimsProvider); ok {
		size, overlap = p.ChunkDims()
	}
	bc, err := NewBaseChunker(size, overlap)
	if err != nil {
		return nil, err
	}
	h := &HybridChunker{
		BaseChunker: bc,
		inner:       inner,
		noSplitWhen: defaultNoSplit,
	}
	if len(noSplitWhen) > 0 && noSplitWhen[0] != nil {
		h.noSplitWhen = noSplitWhen[0]
	}
	// 与兄弟构造器模式一致；hybrid 的 ChunkText/ChunkDocuments 均自实现，
	// self 路由实际不被消费，注入仅防御直接调用 BaseChunker 默认路径
	bc.SetSelf(h)
	return h, nil
}

// ChunkText 纯委托 inner（对齐 Python chunk_text = self._inner.chunk_text）。
//
// 不经过 BaseChunker 的 chunkTextOf/self 路由，hybrid 直接持有 inner 引用。
//
// Python: hybrid_chunker.py HybridChunker.chunk_text
func (h *HybridChunker) ChunkText(text string) ([]string, error) {
	return h.inner.ChunkText(text)
}

// ChunkDocuments 完全覆写：谓词命中且文本非空 → 整体单块；否则逐文档
// 单元素委托 inner。
//
// 两个 Python 对齐细节：
//  1. 谓词命中的前提是文本非空（(doc.text or "").strip()）——空白文档
//     也走 inner（CharChunker 空文本产 0 块）
//  2. 单块文本是 strip 后的；inner 委托路径的文本不 strip——两条路径
//     空白处理不对称，为 Python 原样行为
//
// Python: hybrid_chunker.py HybridChunker.chunk_documents
func (h *HybridChunker) ChunkDocuments(ctx context.Context, docs []reranker.Document) ([]common.TextChunk, error) {
	chunks := make([]common.TextChunk, 0)
	for _, doc := range docs {
		if h.noSplitWhen(&doc) && strings.TrimSpace(doc.Text) != "" {
			uid := uuid.NewString()
			chunks = append(chunks, common.TextChunk{
				ID:       uid,
				Text:     strings.TrimSpace(doc.Text),
				DocID:    doc.ID,
				Metadata: mergeChunkMetadata(doc.Metadata, 0, 1, uid),
			})
		} else {
			innerChunks, err := h.inner.ChunkDocuments(ctx, []reranker.Document{doc})
			if err != nil {
				return nil, err
			}
			chunks = append(chunks, innerChunks...)
		}
	}
	return chunks, nil
}

// Process 局部覆写（决策 3）：转发到覆写版 ChunkDocuments。
//
// BaseChunker.Process 静态解析到基类默认 ChunkDocuments（Go 嵌入是
// 组合非继承，感知不到本类型覆写），不覆写会绕过谓词判定——
// 表格行被错误切碎。
//
// Python: 基类 process → self.chunk_documents 动态分发到覆写版
func (h *HybridChunker) Process(ctx context.Context, docs []reranker.Document) ([]common.TextChunk, error) {
	return h.ChunkDocuments(ctx, docs)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// defaultNoSplit 表格行/列视为单块。
//
// 对齐 Python _default_no_split：source_type 为 "row" 或 "column" 时
// 命中。Go map 取值 nil 时 == 比较安全（metadata 为 nil map 或缺键
// 均返回零值 ""，不命中），等价 (doc.metadata or {}).get(...)。
//
// Python: hybrid_chunker.py _default_no_split
func defaultNoSplit(doc *reranker.Document) bool {
	return doc.Metadata["source_type"] == "row" || doc.Metadata["source_type"] == "column"
}
```

**注意**：`fmt.Errorf` 需在 import 块加 `"fmt"`（上面构造函数用到）。测试文件里 `fakePlainChunker` 的返回类型直接写 `[]common.TextChunk` 并 import `"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"`。

- [ ] **Step 3.4: 跑测试确认通过**

```bash
go test ./internal/agentcore/retrieval/indexing/processor/chunker/ -run 'Hybrid|DefaultNoSplit' -v 2>&1 | tail -30
```

预期：13 个测试全 PASS（含子测试）。

- [ ] **Step 3.5: 全包回归**

```bash
go test ./internal/agentcore/retrieval/indexing/processor/chunker/ 2>&1 | tail -3
```

预期：全部 PASS。

- [ ] **Step 3.6: 提交**

```bash
git add internal/agentcore/retrieval/indexing/processor/chunker/hybrid_chunker.go internal/agentcore/retrieval/indexing/processor/chunker/hybrid_chunker_test.go
git commit -m "feat(chunker): HybridChunker 混合分块器（13.8）"
```

---

### Task 4: hybridFactory 回填（占位替换）

**Files:**
- Modify: `internal/agentcore/retrieval/indexing/processor/chunker/registry.go`（hybridFactory L209-227 重写）
- Test: Modify: `internal/agentcore/retrieval/indexing/processor/chunker/registry_test.go`（4 个占位测试改写）；Test: `internal/agentcore/retrieval/indexing/processor/chunker/hybrid_chunker_test.go`（追加工厂集成测试）

- [ ] **Step 4.1: 写失败测试（改写占位断言）**

`registry_test.go` 中，将以下 4 个测试整体替换：

```go
// TestHybridFactory_inner缺省时13_8前占位报错 ⤴️ 13.6 已回填默认 CharChunker 构造校验
func TestHybridFactory_inner缺省时13_8前占位报错(t *testing.T) {
	// 无 inner → 默认 CharChunker(512/50) 构造成功 → 落到 13.8 占位
	_, err := GetChunker("hybrid")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not implemented until 13.8")
}

// TestHybridFactory_inner注入时13_8前占位报错 ⤴️ 13.8 落地 HybridChunker
func TestHybridFactory_inner注入时13_8前占位报错(t *testing.T) {
	inner, err := newFakeEntryChunkerFake()
	assert.NoError(t, err)
	_, err = GetChunker("hybrid", WithInnerChunker(inner))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not implemented until 13.8")
}
```

与：

```go
// TestHybridFactory_inner显式nil走缺省分支 WithInnerChunker(nil) 等价未设置，
// 走默认 CharChunker（inner 类型由 Chunker 接口静态保证，无需运行时断言）
func TestHybridFactory_inner显式nil走缺省分支(t *testing.T) {
	_, err := GetChunker("hybrid", WithInnerChunker(nil))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not implemented until 13.8")
}
```

替换为：

```go
// TestHybridFactory_inner缺省构造默认CharChunker ⤴️ 13.8 回填：
// 构造并保留（决策 4），尺寸穿透到 hybrid
func TestHybridFactory_inner缺省构造默认CharChunker(t *testing.T) {
	c, err := GetChunker("hybrid")
	assert.NoError(t, err)
	h, ok := c.(*HybridChunker)
	assert.True(t, ok)
	assert.Equal(t, 512, h.ChunkSize)
	assert.Equal(t, 50, h.ChunkOverlap)

	// 默认谓词生效（source_type=row 单块）
	docs := []reranker.Document{{
		ID:       "r",
		Text:     "姓名: 张三",
		Metadata: map[string]any{"source_type": "row"},
	}}
	chunks, err := h.ChunkDocuments(context.Background(), docs)
	assert.NoError(t, err)
	assert.Len(t, chunks, 1)
}

// TestHybridFactory_inner注入构造成功 ⤴️ 13.8 回填
func TestHybridFactory_inner注入构造成功(t *testing.T) {
	inner, err := newFakeEntryChunkerFake(WithChunkSize(2048), WithChunkOverlap(64))
	assert.NoError(t, err)
	c, err := GetChunker("hybrid", WithInnerChunker(inner))
	assert.NoError(t, err)
	h, ok := c.(*HybridChunker)
	assert.True(t, ok)
	assert.Equal(t, 2048, h.ChunkSize, "尺寸从 inner 断言提取")
	assert.Equal(t, 64, h.ChunkOverlap)
}

// TestHybridFactory_inner显式nil走缺省分支 WithInnerChunker(nil) 等价未设置，
// 走默认 CharChunker（inner 类型由 Chunker 接口静态保证，无需运行时断言）
func TestHybridFactory_inner显式nil走缺省分支(t *testing.T) {
	c, err := GetChunker("hybrid", WithInnerChunker(nil))
	assert.NoError(t, err)
	h, ok := c.(*HybridChunker)
	assert.True(t, ok)
	assert.Equal(t, 512, h.ChunkSize, "显式 nil 与未设置等价 → 默认 512/50")
}
```

`TestHybridFactory_多余参数报错` 保持不变（断言目标未变）。

**注意**：改写后 registry_test.go 需补 import：`"context"` 与 `"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/reranker"`（原文件只 import 了 testing/assert）。

再在 `hybrid_chunker_test.go` 末尾追加工厂集成测试：

```go
// TestGetChunker_hybrid全参路径 工厂全参数集成：尺寸穿透 + 谓词透传
func TestGetChunker_hybrid全参路径(t *testing.T) {
	inner, _ := NewCharChunker(WithChunkSize(64), WithChunkOverlap(8))
	c, err := GetChunker("hybrid", WithInnerChunker(inner), WithNoSplitWhen(func(doc *reranker.Document) bool {
		return doc.Metadata["keep"] == true
	}))
	assert.NoError(t, err)
	h, ok := c.(*HybridChunker)
	assert.True(t, ok)
	assert.Equal(t, 64, h.ChunkSize)
	assert.Equal(t, 8, h.ChunkOverlap)

	docs := []reranker.Document{
		{ID: "keep-doc", Text: "  keep whole  ", Metadata: map[string]any{"keep": true}},
		{ID: "split-doc", Text: "normal paragraph"},
	}
	chunks, err := h.ChunkDocuments(context.Background(), docs)
	assert.NoError(t, err)
	// keep-doc 自定义谓词命中单块（strip）；split-doc 走 inner(size=64) 1 块
	assert.Len(t, chunks, 2)
	assert.Equal(t, "keep whole", chunks[0].Text)
	assert.Equal(t, "keep-doc", chunks[0].DocID)
}

// TestGetChunker_hybrid尺寸穿透 无 inner 时 chunk_size/chunk_overlap 决定默认 CharChunker
func TestGetChunker_hybrid尺寸穿透(t *testing.T) {
	c, err := GetChunker("hybrid", WithChunkSize(300), WithChunkOverlap(30))
	assert.NoError(t, err)
	h, ok := c.(*HybridChunker)
	assert.True(t, ok)
	assert.Equal(t, 300, h.ChunkSize, "尺寸穿透到默认 CharChunker 再断言提取回 hybrid")
	assert.Equal(t, 30, h.ChunkOverlap)
}
```

- [ ] **Step 4.2: 跑测试确认失败**

```bash
go test ./internal/agentcore/retrieval/indexing/processor/chunker/ -run 'HybridFactory|GetChunker_hybrid' -v 2>&1 | tail -20
```

预期：新改写的测试 FAIL（`GetChunker("hybrid")` 仍返回占位错误 `not implemented until 13.8`，断言 NoError 失败）。

- [ ] **Step 4.3: 重写 hybridFactory**

`registry.go` 中将 hybridFactory 整体（含注释）替换为：

```go
// hybridFactory hybrid 分块器工厂。
//
// 对齐 Python _hybrid_factory：
//   - inner 缺省：由 chunk_size/chunk_overlap 构造默认 CharChunker（13.8
//     起构造并保留，决策 4），参数经其构造校验（155100/155101），Extra
//     随 opts 透传给 CharChunker 后吞掉（对齐 Python **params 前向兼容）
//   - inner 已给：多余 kwargs 报错（TypeError 语义）；inner 类型由 Chunker
//     接口静态保证（Go 无运行时 isinstance 需求）
//
// Python: chunker/__init__.py _hybrid_factory
func hybridFactory(opts ...ChunkerOption) (Chunker, error) {
	o := NewChunkerOptions(opts...)
	var inner Chunker
	if o.InnerChunker == nil {
		c, err := NewCharChunker(opts...)
		if err != nil {
			return nil, err
		}
		inner = c
	} else if len(o.Extra) > 0 {
		keys := make([]string, 0, len(o.Extra))
		for k := range o.Extra {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		return nil, fmt.Errorf("Unknown kwargs for 'hybrid' chunker: %s", strings.Join(keys, ", "))
	} else {
		inner = o.InnerChunker
	}
	return NewHybridChunker(inner, o.NoSplitWhen)
}
```

- [ ] **Step 4.4: 跑测试确认通过**

```bash
go test ./internal/agentcore/retrieval/indexing/processor/chunker/ -run 'HybridFactory|GetChunker_hybrid' -v 2>&1 | tail -15
```

预期：6 个工厂相关测试全 PASS。

- [ ] **Step 4.5: 全包回归 + 覆盖率**

```bash
go test -cover ./internal/agentcore/retrieval/indexing/processor/chunker/ 2>&1 | tail -3
```

预期：全部 PASS，覆盖率 ≥92%（不得低于现状 92.4% 减去 2pp 的浮动；若 <85% 必须补测试）。

- [ ] **Step 4.6: 提交**

```bash
git add internal/agentcore/retrieval/indexing/processor/chunker/registry.go internal/agentcore/retrieval/indexing/processor/chunker/registry_test.go internal/agentcore/retrieval/indexing/processor/chunker/hybrid_chunker_test.go
git commit -m "feat(chunker): hybridFactory 回填 HybridChunker 完整体（13.8，⤴️ 13.5 闭环）"
```

---

### Task 5: doc.go 目录更新 + IMPLEMENTATION_PLAN.md 状态同步

**Files:**
- Modify: `internal/agentcore/retrieval/indexing/processor/chunker/doc.go`（文件目录补条目）
- Modify: `IMPLEMENTATION_PLAN.md`（13.8 行状态与产出；13.5 行闭环说明）

- [ ] **Step 5.1: 更新 doc.go**

文件目录部分，在 `char_chunker.go` 条目之后插入：

```
//	├── hybrid_chunker.go     # HybridChunker 混合分块器（"hybrid" 注册项，13.8）
```

即目录段变为：

```
//	chunker/
//	├── doc.go               # 包文档
//	├── base.go              # Chunker 接口 + DimsProvider + BaseChunker 默认适配器 + 构造校验
//	├── registry.go          # 分块器注册表 + GetChunker/RegisterChunker + hybridFactory
//	├── text_splitter.go     # TextSplitter 接口 + CharSplitter + Tokenizer 三态接口
//	│                        #   + TiktokenTokenizer + IndexSentenceSplitter（13.7）
//	├── char_chunker.go      # CharChunker 字符级分块器（"char" 注册项）
//	├── hybrid_chunker.go    # HybridChunker 混合分块器（"hybrid" 注册项，13.8）
//	└── tokenizer_chunker.go # TokenizerChunker token 预算分块器（"tokenizer" 注册项）
```

（base.go 条目注释追加 `DimsProvider`；排序惯例：doc.go → 核心类型 → 按交付顺序。）

- [ ] **Step 5.2: 更新 IMPLEMENTATION_PLAN.md**

13.8 行：

```markdown
| 13.8 | ✅ | HybridChunker | `HybridChunker(嵌入 BaseChunker / NewHybridChunker：inner nil 防御+DimsProvider 断言提取 inner 尺寸 fallback 512/50+155100/155101 校验+谓词可变参数空→defaultNoSplit / ChunkText 纯委托 / ChunkDocuments 完全覆写 4 分支：谓词命中+非空白→strip 单块+metadata 三键，否则逐文档单元素委托 inner，空白文本走 inner 产 0 块 / Process 局部覆写转发) + base.go 增量(DimsProvider 接口+ChunkDims+mergeChunkMetadata 提取) + hybridFactory 回填(inner 缺省构造并保留默认 CharChunker，Extra 透传吞掉 / 多余 kwargs 报错)`。设计：`docs/superpowers/specs/2026-10-19-hybridchunker-13.8-design.md` | `openjiuwen/core/retrieval/indexing/processor/chunker/hybrid_chunker.py` |
```

13.5 行末尾追加（现有文本不动，在反引号闭合后追加一句）：

```markdown
；13.8 已回填 hybridFactory 完整体 ⤴️
```

**注意**：13.5 行的产出栏是一整个反引号包裹的长串，⤴️ 追加在反引号内末尾（对齐既有 ⤵️/⤴️ 标记惯例——查 13.5 行现状确认插入位置在行尾反引号内）。

- [ ] **Step 5.3: 提交**

```bash
git add internal/agentcore/retrieval/indexing/processor/chunker/doc.go IMPLEMENTATION_PLAN.md
git commit -m "docs(chunker): 13.8 完成——doc.go 目录与计划状态同步"
```

---

### Task 6: 最终验证

- [ ] **Step 6.1: 全包测试 + 覆盖率**

```bash
pgrep -f 'go (build|test)' || true
go test -cover ./internal/agentcore/retrieval/indexing/processor/chunker/... 2>&1 | tail -3
```

预期：PASS，覆盖率 ≥85%（目标 92%+）。

- [ ] **Step 6.2: 全仓编译验证（确认无破坏）**

```bash
go build ./... 2>&1 | tail -5
```

预期：无输出（编译成功）。

- [ ] **Step 6.3: 规范自检**

```bash
gofmt -l internal/agentcore/retrieval/indexing/processor/chunker/ && echo "格式 OK"
```

预期：输出 `格式 OK`（gofmt -l 无未格式化文件）。

- [ ] **Step 6.4: 检查提交历史完整**

```bash
git log --oneline -6
```

预期：5 个新 commit（Task 1/2/3/4/5 各一个）。

---

## Self-Review 记录

- **Spec 覆盖**：设计文档第 4 节（DimsProvider/ChunkDims）→ Task 1；5.5（mergeChunkMetadata）→ Task 2；5.1-5.4（实体）→ Task 3；第 6 节（工厂）→ Task 4；第 3 节（doc.go/计划文档）→ Task 5；第 10 节验证点 → Task 6 + 各任务内嵌回归。无缺口。
- **占位符扫描**：Task 3 测试代码里 `commonTextChunkAlias` 是有意的实施提示（附了"直接写全类型"的明确指示），其余无 TBD/TODO。
- **类型一致性**：`NewHybridChunker(inner Chunker, noSplitWhen ...func(*reranker.Document) bool)` 签名在 Task 3 定义、Task 4 工厂调用一致；`mergeChunkMetadata(docMeta map[string]any, chunkIndex, totalChunks int, chunkID string)` 在 Task 2 定义、Task 3 消费一致；`defaultNoSplit(doc *reranker.Document) bool` 在 Task 3 定义、测试直接调用一致。
