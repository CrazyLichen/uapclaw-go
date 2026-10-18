# 13.6 CharChunker + CharSplitter 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 落地 chunker registry 的第一个真实分块算法 CharChunker（注册名 "char"），带出 CharSplitter 滑窗分割器，回填 hybridFactory 默认 inner 分支。

**Architecture:** 复用 13.5 交付的 Chunker 接口/BaseChunker/选项系统，新增两个同包文件（char_chunker.go + text_splitter.go），registry.go 小改（注册 + 回填）。CharChunker 嵌入 BaseChunker 仅覆写 ChunkText，每次调用内联新建 CharSplitter（对齐 Python）。CharSplitter 构造为钳制语义（不报错），滑窗按 rune 切分防中文截断。

**Tech Stack:** Go 标准库 + testify + google/uuid + 项目内 logger（zerolog 封装）。设计文档：`docs/superpowers/specs/2026-10-19-charchunker-13.6-design.md`

**对应 Python：** `openjiuwen/core/retrieval/indexing/processor/chunker/char_chunker.py`（61 行）+ `text_splitter.py` 的 TextSplitter/CharSplitter 部分（第 26-67 行；IndexSentenceSplitter 及 DEFAULT_CHUNK_SIZE/DEFAULT_SAFE_ENCODE_MAX_LENGTH 留 13.7）

---

## 环境约定（每个任务执行前检查）

- 工作目录：`/home/opensource/uapclaw-gateway`
- 编译前设置：`export GOPROXY=https://goproxy.cn,direct`
- 编译互斥：先 `pgrep -f 'go (build|test)'` 检查残留进程，有则 kill 后再执行（项目规则 5）
- 本计划全部命令只涉及单包测试，无需 `go clean -cache`

## 已定稿决策（设计文档 4 项，不可偏离）

1. `TextSplitter` 接口：`Split(doc *reranker.Document) ([]common.TextChunk, error)` 单一入参；error 为 13.7 预留，CharSplitter 恒返回 nil
2. `NewCharSplitter` 钳制不报错：chunkSize==0→200（`or` 语义）、overlap→clamp [0, size-1]、size<1→1；**overlap 的 0 是合法显式值，不做默认替换**（Go 无 None）
3. `ChunkText("")` → `[]string{}`（空切片非 nil）
4. 计划表回填：既有行一字不动，只追加/改状态符号

---

### Task 1: TextSplitter 接口 + CharSplitter（text_splitter.go）

**Files:**
- Create: `internal/agentcore/retrieval/indexing/processor/chunker/text_splitter.go`
- Test: `internal/agentcore/retrieval/indexing/processor/chunker/text_splitter_test.go`

- [x] **Step 1.1: 写失败测试**

创建 `internal/agentcore/retrieval/indexing/processor/chunker/text_splitter_test.go`：

```go
package chunker

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/reranker"
)

// TestNewCharSplitter_显式默认值 对齐 Python test_init_with_defaults
// （Go 无无参构造，显式传默认 200/40）
func TestNewCharSplitter_显式默认值(t *testing.T) {
	s := NewCharSplitter(200, 40)
	assert.Equal(t, 200, s.ChunkSize)
	assert.Equal(t, 40, s.ChunkOverlap)
}

// TestNewCharSplitter_自定义值 对齐 Python test_init_with_custom_values
func TestNewCharSplitter_自定义值(t *testing.T) {
	s := NewCharSplitter(512, 50)
	assert.Equal(t, 512, s.ChunkSize)
	assert.Equal(t, 50, s.ChunkOverlap)
}

// TestNewCharSplitter_size零值取默认200 对齐 Python `chunk_size or DEFAULT`（0→200）
func TestNewCharSplitter_size零值取默认200(t *testing.T) {
	s := NewCharSplitter(0, 40)
	assert.Equal(t, 200, s.ChunkSize)
}

// TestNewCharSplitter_overlap零是合法显式值 Go 无 None：0 不做默认替换（与 Python chunk_overlap=0 一致）
func TestNewCharSplitter_overlap零是合法显式值(t *testing.T) {
	s := NewCharSplitter(0, 0)
	assert.Equal(t, 200, s.ChunkSize)
	assert.Equal(t, 0, s.ChunkOverlap)
}

// TestNewCharSplitter_overlap越界钳制 对齐 Python test_init_overlap_adjusted
func TestNewCharSplitter_overlap越界钳制(t *testing.T) {
	s := NewCharSplitter(100, 150)
	assert.Equal(t, 100, s.ChunkSize)
	assert.Equal(t, 99, s.ChunkOverlap) // clamp(0, 150, 100-1)
}

// TestNewCharSplitter_overlap负值钳制 对齐 Python test_init_overlap_negative
func TestNewCharSplitter_overlap负值钳制(t *testing.T) {
	s := NewCharSplitter(100, -10)
	assert.Equal(t, 100, s.ChunkSize)
	assert.Equal(t, 0, s.ChunkOverlap)
}

// TestNewCharSplitter_size负值抬到1 Python 真值语义：-5 非 0 不走默认，max(1,-5)=1
func TestNewCharSplitter_size负值抬到1(t *testing.T) {
	s := NewCharSplitter(-5, 50)
	assert.Equal(t, 1, s.ChunkSize)
	assert.Equal(t, 0, s.ChunkOverlap) // clamp 后 overlap=size-1=0
}

// TestNewCharSplitter_size最小值 对齐 Python test_init_chunk_size_minimum（0→200≥1）
func TestNewCharSplitter_size最小值(t *testing.T) {
	s := NewCharSplitter(0, 40)
	assert.GreaterOrEqual(t, s.ChunkSize, 1)
}

// TestCharSplitter_Split_短文本单块 对齐 Python test_split_short_text
func TestCharSplitter_Split_短文本单块(t *testing.T) {
	s := NewCharSplitter(100, 10)
	doc := &reranker.Document{ID: "doc_1", Text: "Short text"}
	chunks, err := s.Split(doc)
	assert.NoError(t, err)
	assert.Len(t, chunks, 1)
	assert.Equal(t, "Short text", chunks[0].Text)
	assert.Equal(t, "doc_1", chunks[0].DocID)
	assert.NotEmpty(t, chunks[0].ID)
}

// TestCharSplitter_Split_长文本多块 对齐 Python test_split_long_text
// 65 字符，size=10 overlap=2 → step=8 → 9 块，末块 [64:65]="s"
func TestCharSplitter_Split_长文本多块(t *testing.T) {
	s := NewCharSplitter(10, 2)
	text := "This is a longer text that needs to be split into multiple chunks"
	doc := &reranker.Document{ID: "doc_1", Text: text}
	chunks, err := s.Split(doc)
	assert.NoError(t, err)
	assert.Len(t, chunks, 9)
	for _, chunk := range chunks {
		assert.Equal(t, "doc_1", chunk.DocID)
	}
	assert.Equal(t, "This is a ", chunks[0].Text)
	assert.Equal(t, "s", chunks[8].Text)
}

// TestCharSplitter_Split_重叠校验 对齐 Python test_split_with_overlap
// 33 字符，size=10 overlap=3 → step=7 → 5 块；相邻块首尾 3 字符重叠
func TestCharSplitter_Split_重叠校验(t *testing.T) {
	s := NewCharSplitter(10, 3)
	doc := &reranker.Document{ID: "doc_1", Text: "This is a test text for splitting"}
	chunks, err := s.Split(doc)
	assert.NoError(t, err)
	assert.Len(t, chunks, 5)
	// 块 i 的末 3 字符 == 块 i+1 的首 3 字符（overlap=3）
	for i := 0; i+1 < len(chunks); i++ {
		tail := chunks[i].Text[len(chunks[i].Text)-3:]
		head := chunks[i+1].Text[:3]
		assert.Equal(t, tail, head)
	}
}

// TestCharSplitter_Split_保留metadata 对齐 Python test_split_preserves_metadata
func TestCharSplitter_Split_保留metadata(t *testing.T) {
	s := NewCharSplitter(10, 2)
	doc := &reranker.Document{
		ID:       "doc_1",
		Text:     "This is a test",
		Metadata: map[string]any{"source": "test", "author": "test_author"},
	}
	chunks, err := s.Split(doc)
	assert.NoError(t, err)
	assert.NotEmpty(t, chunks)
	for _, chunk := range chunks {
		assert.Equal(t, "test", chunk.Metadata["source"])
		assert.Equal(t, "test_author", chunk.Metadata["author"])
	}
}

// TestCharSplitter_Split_中文rune切分 Go 专项：多字节字符不被截断
func TestCharSplitter_Split_中文rune切分(t *testing.T) {
	s := NewCharSplitter(3, 1)
	doc := &reranker.Document{ID: "doc_1", Text: "你好世界再见"}
	chunks, err := s.Split(doc)
	assert.NoError(t, err)
	assert.Len(t, chunks, 3)
	assert.Equal(t, "你好世", chunks[0].Text)
	assert.Equal(t, "世界再", chunks[1].Text)
	assert.Equal(t, "再见", chunks[2].Text)
}

// TestCharSplitter_Split_空文本 空文档产出空列表（对齐 Python while start < len(text)）
func TestCharSplitter_Split_空文本(t *testing.T) {
	s := NewCharSplitter(10, 2)
	chunks, err := s.Split(&reranker.Document{ID: "doc_1"})
	assert.NoError(t, err)
	assert.Empty(t, chunks)
}

// TestCharSplitter_Split_nil文档防御 Go 防御：nil 不 panic，返回空
func TestCharSplitter_Split_nil文档防御(t *testing.T) {
	s := NewCharSplitter(10, 2)
	chunks, err := s.Split(nil)
	assert.NoError(t, err)
	assert.Empty(t, chunks)
}

// TestCharSplitter_Split_nilMetadata转空map 对齐 Python `doc.metadata or {}`
func TestCharSplitter_Split_nilMetadata转空map(t *testing.T) {
	s := NewCharSplitter(10, 2)
	doc := &reranker.Document{ID: "doc_1", Text: "This is a test"}
	chunks, err := s.Split(doc)
	assert.NoError(t, err)
	assert.NotEmpty(t, chunks)
	for _, chunk := range chunks {
		assert.NotNil(t, chunk.Metadata)
		assert.Empty(t, chunk.Metadata)
	}
}
```

- [x] **Step 1.2: 运行测试验证失败**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/retrieval/indexing/processor/chunker/ -run 'TestNewCharSplitter|TestCharSplitter' -v
```

预期：**编译失败**，`undefined: NewCharSplitter` / `undefined: TextSplitter`（红阶段）。

- [x] **Step 1.3: 实现 text_splitter.go**

创建 `internal/agentcore/retrieval/indexing/processor/chunker/text_splitter.go`：

```go
package chunker

import (
	"github.com/google/uuid"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/reranker"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TextSplitter 文本分割器抽象接口。
//
// 对齐 Python TextSplitter ABC（仅 split 一个抽象方法）。
// Python split 入参为 TextChunk | Document 联合类型，Go 侧收窄为
// *reranker.Document（生产调用方 CharChunker/TokenizerChunker 只传
// Document；13.7 IndexSentenceSplitter 的 TextChunk→Document 转换留在
// 实现内部）。error 返回为 13.7 预留，CharSplitter 恒返回 nil。
//
// Python: openjiuwen/core/retrieval/indexing/processor/chunker/text_splitter.py (TextSplitter)
type TextSplitter interface {
	// Split 将文档分割为文本块列表。
	Split(doc *reranker.Document) ([]common.TextChunk, error)
}

// CharSplitter 基于字符长度的分割器，不依赖 tokenizer。
//
// 构造为钳制语义（不报错）：chunkSize==0→默认 200、overlap→clamp
// [0, size-1]、size<1→1。与 BaseChunker 的"越界报错"相反是有意为之：
// 真实路径 CharChunker 已过基类校验，此处钳制是防御层（对齐 Python
// CharSplitter 原样行为）。
//
// Python: text_splitter.py (CharSplitter)
type CharSplitter struct {
	// ChunkSize 分块大小（字符数），构造后恒 ≥ 1
	ChunkSize int
	// ChunkOverlap 相邻块重叠（字符数），构造后恒 ∈ [0, ChunkSize-1]
	ChunkOverlap int
}

// ──────────────────────────── 枚举 ────────────────────────────
// ──────────────────────────── 常量 ────────────────────────────

const (
	// defaultCharChunkSize CharSplitter 默认分块大小
	//（对齐 Python DEFAULT_CHAR_CHUNK_SIZE，注意与 CharChunker 的 512 是两套默认值）
	defaultCharChunkSize = 200
	// defaultCharChunkOverlap CharSplitter 默认重叠（对齐 Python DEFAULT_CHAR_CHUNK_OVERLAP）。
	// Go 无 None 语义：overlap 的 0 是合法显式值不做默认替换，需要默认行为时调用方显式传 40
	defaultCharChunkOverlap = 40
)

// ──────────────────────────── 全局变量 ────────────────────────────

// 编译期断言 CharSplitter 实现 TextSplitter
var _ TextSplitter = (*CharSplitter)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewCharSplitter 构造并钳制参数（不报错，对齐 Python CharSplitter.__init__）。
//
// 钳制规则（执行顺序对齐 Python 源码）：
//  1. chunkSize==0 → 200（对齐 `chunk_size or DEFAULT` 的 0 值语义；负数保持原值）
//  2. overlap → clamp [0, size-1]（size 为步骤 1 后的值）
//  3. size < 1 → 1
//
// Python: text_splitter.py CharSplitter.__init__
func NewCharSplitter(chunkSize, chunkOverlap int) *CharSplitter {
	size := chunkSize
	if size == 0 {
		size = defaultCharChunkSize
	}
	overlap := chunkOverlap
	if overlap > size-1 {
		overlap = size - 1
	}
	if overlap < 0 {
		overlap = 0
	}
	if size < 1 {
		size = 1
	}
	return &CharSplitter{ChunkSize: size, ChunkOverlap: overlap}
}

// Split 按滑窗切分文档。
//
// step = size-overlap（size≤overlap 时退化 step=size——钳制后不可达，
// 保留对齐 Python 源码）；每块生成 uuid，携带 doc_id 与 metadata
//（共享引用，对齐 Python）；rune 切片保证多字节字符（中文）不被截断。
//
// Python: text_splitter.py CharSplitter.split
func (s *CharSplitter) Split(doc *reranker.Document) ([]common.TextChunk, error) {
	if doc == nil {
		return []common.TextChunk{}, nil
	}
	runes := []rune(doc.Text)
	meta := doc.Metadata
	if meta == nil {
		meta = make(map[string]any)
	}
	step := s.ChunkSize - s.ChunkOverlap
	if s.ChunkSize <= s.ChunkOverlap {
		step = s.ChunkSize
	}
	res := make([]common.TextChunk, 0)
	for start := 0; start < len(runes); start += step {
		end := start + s.ChunkSize
		if end > len(runes) {
			end = len(runes)
		}
		res = append(res, common.TextChunk{
			ID:       uuid.NewString(),
			Text:     string(runes[start:end]),
			DocID:    doc.ID,
			Metadata: meta,
		})
	}
	return res, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────
```

- [x] **Step 1.4: 运行测试验证通过**

```bash
go test ./internal/agentcore/retrieval/indexing/processor/chunker/ -run 'TestNewCharSplitter|TestCharSplitter' -v
```

预期：全部 PASS（16 个测试）。若 `TestCharSplitter_Split_长文本多块` 断言块数不符，先复查文本长度（65 字符）与 step 计算，**不要改测试期望值凑绿**。

- [x] **Step 1.5: 提交**

```bash
git add internal/agentcore/retrieval/indexing/processor/chunker/text_splitter.go internal/agentcore/retrieval/indexing/processor/chunker/text_splitter_test.go
git commit -m "feat(chunker): 13.6 TextSplitter 接口 + CharSplitter 滑窗分割（带出自 13.9，rune 切片防中文截断）"
```

---

### Task 2: CharChunker（char_chunker.go）

**Files:**
- Create: `internal/agentcore/retrieval/indexing/processor/chunker/char_chunker.go`
- Test: `internal/agentcore/retrieval/indexing/processor/chunker/char_chunker_test.go`

注意：本任务测试**不含** `GetChunker("char")` 相关（注册在 Task 3）。

- [x] **Step 2.1: 写失败测试**

创建 `internal/agentcore/retrieval/indexing/processor/chunker/char_chunker_test.go`：

```go
package chunker

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/reranker"
)

// TestNewCharChunker_默认值512和50 对齐 Python test_init_with_defaults
func TestNewCharChunker_默认值512和50(t *testing.T) {
	c, err := NewCharChunker()
	assert.NoError(t, err)
	assert.Equal(t, 512, c.ChunkSize)
	assert.Equal(t, 50, c.ChunkOverlap)
}

// TestNewCharChunker_自定义值 对齐 Python test_init_with_custom_values
func TestNewCharChunker_自定义值(t *testing.T) {
	c, err := NewCharChunker(WithChunkSize(256), WithChunkOverlap(25))
	assert.NoError(t, err)
	assert.Equal(t, 256, c.ChunkSize)
	assert.Equal(t, 25, c.ChunkOverlap)
}

// TestNewCharChunker_构造校验穿透 参数经 NewBaseChunker 校验（155100/155101）
func TestNewCharChunker_构造校验穿透(t *testing.T) {
	_, err := NewCharChunker(WithChunkSize(0))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "155100")

	_, err = NewCharChunker(WithChunkOverlap(-1))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "155101")

	_, err = NewCharChunker(WithChunkSize(10), WithChunkOverlap(10))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "155101")
}

// TestCharChunker_ChunkText_真实分块 Python mock 版用例改为真实行为断言：
// size=10 overlap=2，"This is a test text"(19 字符) → step=8 → 3 块
func TestCharChunker_ChunkText_真实分块(t *testing.T) {
	c, err := NewCharChunker(WithChunkSize(10), WithChunkOverlap(2))
	assert.NoError(t, err)
	chunks, err := c.ChunkText("This is a test text")
	assert.NoError(t, err)
	assert.Equal(t, []string{"This is a ", "a test tex", "ext"}, chunks)
}

// TestCharChunker_ChunkText_空文本 对齐 Python test_chunk_text_empty + test_chunk_text_none
//（Go 无法表达 None，两用例并入空串）
func TestCharChunker_ChunkText_空文本(t *testing.T) {
	c, err := NewCharChunker()
	assert.NoError(t, err)
	chunks, err := c.ChunkText("")
	assert.NoError(t, err)
	assert.Empty(t, chunks)
}

// TestCharChunker_ChunkText_中文rune不截断 Go 专项：多字节字符按 rune 切分
func TestCharChunker_ChunkText_中文rune不截断(t *testing.T) {
	c, err := NewCharChunker(WithChunkSize(3), WithChunkOverlap(1))
	assert.NoError(t, err)
	chunks, err := c.ChunkText("你好世界再见")
	assert.NoError(t, err)
	assert.Equal(t, []string{"你好世", "世界再", "再见"}, chunks)
}

// TestCharChunker_ChunkDocuments_全链 SetSelf 注入后默认实现路由到覆写的 ChunkText
// 断言确定性字段（含 uuid 的产出不可整 struct 比较——13.5 审查规则）
func TestCharChunker_ChunkDocuments_全链(t *testing.T) {
	c, err := NewCharChunker(WithChunkSize(10), WithChunkOverlap(2))
	assert.NoError(t, err)
	docs := []reranker.Document{{
		ID:       "doc_1",
		Text:     "This is a test text",
		Metadata: map[string]any{"k": "v"},
	}}
	chunks, err := c.ChunkDocuments(context.Background(), docs)
	assert.NoError(t, err)
	assert.Len(t, chunks, 3)
	for i, chunk := range chunks {
		assert.Equal(t, "doc_1", chunk.DocID)
		assert.Equal(t, "v", chunk.Metadata["k"])
		assert.Equal(t, i, chunk.Metadata["chunk_index"])
		assert.Equal(t, 3, chunk.Metadata["total_chunks"])
		assert.Equal(t, chunk.ID, chunk.Metadata["chunk_id"])
	}
	assert.Equal(t, "This is a ", chunks[0].Text)
	assert.Equal(t, "a test tex", chunks[1].Text)
	assert.Equal(t, "ext", chunks[2].Text)
}

// TestCharChunker_Process_等价ChunkDocuments 对齐 Python process = chunk_documents。
// 两次调用各自生成 uuid，按确定性字段断言等价（不可整 struct 比较）
func TestCharChunker_Process_等价ChunkDocuments(t *testing.T) {
	c, err := NewCharChunker(WithChunkSize(10), WithChunkOverlap(2))
	assert.NoError(t, err)
	docs := []reranker.Document{{ID: "doc_1", Text: "This is a test text"}}

	viaProcess, err := c.Process(context.Background(), docs)
	assert.NoError(t, err)
	viaDocs, err := c.ChunkDocuments(context.Background(), docs)
	assert.NoError(t, err)

	assert.Len(t, viaProcess, 3)
	assert.Len(t, viaDocs, 3)
	for i := range viaDocs {
		assert.Equal(t, viaDocs[i].Text, viaProcess[i].Text)
		assert.Equal(t, viaDocs[i].DocID, viaProcess[i].DocID)
		assert.Equal(t, viaDocs[i].Metadata["chunk_index"], viaProcess[i].Metadata["chunk_index"])
		assert.Equal(t, viaDocs[i].Metadata["total_chunks"], viaProcess[i].Metadata["total_chunks"])
	}
}
```

- [x] **Step 2.2: 运行测试验证失败**

```bash
go test ./internal/agentcore/retrieval/indexing/processor/chunker/ -run 'TestNewCharChunker|TestCharChunker' -v
```

预期：**编译失败**，`undefined: NewCharChunker`。

- [x] **Step 2.3: 实现 char_chunker.go**

创建 `internal/agentcore/retrieval/indexing/processor/chunker/char_chunker.go`：

```go
package chunker

import (
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/reranker"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// CharChunker 字符级定长分块器。
//
// 嵌入 BaseChunker 获得默认 ChunkDocuments/Process；仅覆写 ChunkText：
// 每次调用内联新建 CharSplitter（对齐 Python，splitter 无状态等价）。
//
// Python: openjiuwen/core/retrieval/indexing/processor/chunker/char_chunker.py (CharChunker)
type CharChunker struct {
	// BaseChunker 默认适配器（ChunkSize/ChunkOverlap + 默认 ChunkDocuments/Process）
	*BaseChunker
}

// ──────────────────────────── 枚举 ────────────────────────────
// ──────────────────────────── 常量 ────────────────────────────

const (
	// logComponent 日志组件标识（对齐 indexer 层先例 base.go 的 logComponent；
	// base.go 为 13.5 交付不动，故落在本文件）
	logComponent = logger.ComponentAgentCore
)

// ──────────────────────────── 全局变量 ────────────────────────────

// 编译期断言 CharChunker 实现 Chunker
var _ Chunker = (*CharChunker)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewCharChunker 构造 CharChunker（复用 13.5 选项系统，默认 512/50）。
//
// 构造链：NewChunkerOptions → NewBaseChunker（校验 155100/155101）→
// SetSelf 注入。Extra 被忽略（对齐 Python **kwargs 透传后无消费即吞掉）。
//
// Python: char_chunker.py CharChunker.__init__
func NewCharChunker(opts ...ChunkerOption) (*CharChunker, error) {
	o := NewChunkerOptions(opts...)
	bc, err := NewBaseChunker(o.ChunkSize, o.ChunkOverlap)
	if err != nil {
		return nil, err
	}
	c := &CharChunker{BaseChunker: bc}
	bc.SetSelf(c)
	return c, nil
}

// ChunkText 字符级分块。空文本返回空切片。
//
// 对齐 Python：if not text → []；每次内联新建
// CharSplitter(chunk_size, chunk_overlap)；构造 Document(text, metadata={})
// → split → 收集 node.text；完成后记录 chunk_count 日志。
//
// Python: char_chunker.py CharChunker.chunk_text
func (c *CharChunker) ChunkText(text string) ([]string, error) {
	if text == "" {
		return []string{}, nil
	}
	splitter := NewCharSplitter(c.ChunkSize, c.ChunkOverlap)
	doc := &reranker.Document{Text: text, Metadata: map[string]any{}}
	textNodes, err := splitter.Split(doc)
	if err != nil {
		return nil, err
	}
	chunks := make([]string, 0, len(textNodes))
	for _, node := range textNodes {
		chunks = append(chunks, node.Text)
	}
	// 对齐 Python: logger.info("Character chunking completed: generated %d chunks", len(chunks))
	logger.Info(logComponent).
		Int("chunk_count", len(chunks)).
		Msg("character chunking completed")
	return chunks, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────
```

- [x] **Step 2.4: 运行测试验证通过**

```bash
go test ./internal/agentcore/retrieval/indexing/processor/chunker/ -run 'TestNewCharChunker|TestCharChunker' -v
```

预期：全部 PASS（8 个测试）。

- [x] **Step 2.5: 提交**

```bash
git add internal/agentcore/retrieval/indexing/processor/chunker/char_chunker.go internal/agentcore/retrieval/indexing/processor/chunker/char_chunker_test.go
git commit -m "feat(chunker): 13.6 CharChunker 字符级分块器（嵌入 BaseChunker 仅覆写 ChunkText）"
```

---

### Task 3: registry 注册 "char" + hybridFactory 回填

**Files:**
- Modify: `internal/agentcore/retrieval/indexing/processor/chunker/registry.go`（init 函数 154-157 行附近 + hybridFactory 159-185 行）
- Modify: `internal/agentcore/retrieval/indexing/processor/chunker/registry_test.go`（两处 13.6 占位断言改写 + 未知名断言增强）
- Modify: `internal/agentcore/retrieval/indexing/processor/chunker/char_chunker_test.go`（追加 GetChunker 相关测试）

- [x] **Step 3.1: 改写 registry_test.go 两处 13.6 占位断言**

在 `internal/agentcore/retrieval/indexing/processor/chunker/registry_test.go` 中：

**替换一**（原 78-83 行）：

```go
// TestHybridFactory_inner缺省时13_6前占位报错 ⤴️ 13.6 落地默认 CharChunker
func TestHybridFactory_inner缺省时13_6前占位报错(t *testing.T) {
	_, err := GetChunker("hybrid") // 无 inner → 需默认 CharChunker（13.6 前占位）
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not implemented until 13.6")
}
```

改为：

```go
// TestHybridFactory_inner缺省时13_8前占位报错 ⤴️ 13.6 已回填默认 CharChunker 构造校验
func TestHybridFactory_inner缺省时13_8前占位报错(t *testing.T) {
	// 无 inner → 默认 CharChunker(512/50) 构造成功 → 落到 13.8 占位
	_, err := GetChunker("hybrid")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not implemented until 13.8")
}
```

**替换二**（原 103-108 行）：

```go
// TestHybridFactory_inner非Chunker报错 对齐 Python TypeError（nil 在 Go 侧即"非实例"）
func TestHybridFactory_inner非Chunker报错(t *testing.T) {
	_, err := GetChunker("hybrid", WithInnerChunker(nil))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not implemented until 13.6")
}
```

改为：

```go
// TestHybridFactory_inner显式nil走缺省分支 WithInnerChunker(nil) 等价未设置，
// 走默认 CharChunker（inner 类型由 Chunker 接口静态保证，无需运行时断言）
func TestHybridFactory_inner显式nil走缺省分支(t *testing.T) {
	_, err := GetChunker("hybrid", WithInnerChunker(nil))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not implemented until 13.8")
}
```

**增强**（原 62 行 `assert.Contains(t, err.Error(), "hybrid")` 后追加一行）：

```go
	assert.Contains(t, err.Error(), "char") // 13.6 后内置 char 也已注册
```

- [x] **Step 3.2: 追加 GetChunker("char") 失败测试到 char_chunker_test.go**

在 `char_chunker_test.go` 文件末尾追加：

```go
// TestGetChunker_char按名获取 对齐 Python register_chunker("char", CharChunker)
func TestGetChunker_char按名获取(t *testing.T) {
	c, err := GetChunker("char")
	assert.NoError(t, err)
	cc, ok := c.(*CharChunker)
	assert.True(t, ok)
	assert.Equal(t, 512, cc.ChunkSize)
	assert.Equal(t, 50, cc.ChunkOverlap)

	c2, err := GetChunker("char", WithChunkSize(64), WithChunkOverlap(8))
	assert.NoError(t, err)
	cc2, ok := c2.(*CharChunker)
	assert.True(t, ok)
	assert.Equal(t, 64, cc2.ChunkSize)
	assert.Equal(t, 8, cc2.ChunkOverlap)
}

// TestGetChunker_char忽略Extra 对齐 Python **kwargs 透传后吞掉
func TestGetChunker_char忽略Extra(t *testing.T) {
	c, err := GetChunker("char", WithExtra("foo", "bar"))
	assert.NoError(t, err)
	assert.NotNil(t, c)
}

// TestHybridFactory_inner缺省走CharChunker参数校验穿透 ⤴️ 13.6 回填验证：
// 非法参数经默认 CharChunker 构造校验报 155100/155101
func TestHybridFactory_inner缺省走CharChunker参数校验穿透(t *testing.T) {
	_, err := GetChunker("hybrid", WithChunkSize(0))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "155100")

	_, err = GetChunker("hybrid", WithChunkSize(10), WithChunkOverlap(10))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "155101")
}
```

- [x] **Step 3.3: 运行测试验证失败**

```bash
go test ./internal/agentcore/retrieval/indexing/processor/chunker/ -run 'TestHybridFactory|TestGetChunker' -v
```

预期：`TestGetChunker_char按名获取` FAIL（`Unknown chunker: char`）；改写后的 hybrid 用例 FAIL（仍报 `not implemented until 13.6`）。

- [x] **Step 3.4: 修改 registry.go**

**改动一**：init 函数（原 153-157 行）替换为：

```go
// init 内置注册。对齐 Python __init__.py 尾部的 register_chunker 调用。
func init() {
	// 对齐 Python register_chunker("char", CharChunker)：类作工厂，kwargs 透传构造
	_ = RegisterChunker("char", func(opts ...ChunkerOption) (Chunker, error) {
		return NewCharChunker(opts...)
	}, false)
	_ = RegisterChunker("hybrid", hybridFactory, false)
}
```

**改动二**：hybridFactory 函数（原 159-185 行，含 doc 注释）替换为：

```go
// hybridFactory hybrid 分块器工厂。
//
// 对齐 Python _hybrid_factory：
//   - inner 缺省：由 chunk_size/chunk_overlap 构造默认 CharChunker（13.6 已回填），
//     参数经其构造校验（155100/155101），多余 Extra 随选项透传给 CharChunker 后
//     吞掉（对齐 Python **params 前向兼容）
//   - inner 已给：多余 kwargs 报错（TypeError 语义）；inner 类型由 Chunker
//     接口静态保证（Go 无运行时 isinstance 需求）
//   - HybridChunker 实体在 13.8 回填 ⤴️，当前返回占位错误
//
// Python: chunker/__init__.py _hybrid_factory
func hybridFactory(opts ...ChunkerOption) (Chunker, error) {
	o := NewChunkerOptions(opts...)
	if o.InnerChunker == nil {
		// ⤴️ 13.6 回填：inner 缺省时默认 inner 为 CharChunker，先过其构造校验
		if _, err := NewCharChunker(opts...); err != nil {
			return nil, err
		}
	} else if len(o.Extra) > 0 {
		keys := make([]string, 0, len(o.Extra))
		for k := range o.Extra {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		return nil, fmt.Errorf("Unknown kwargs for 'hybrid' chunker: %s", strings.Join(keys, ", "))
	}
	// ⤴️ 13.8：替换为 HybridChunker(inner, noSplitWhen) 构造
	_ = o.NoSplitWhen
	return nil, fmt.Errorf("HybridChunker not implemented until 13.8")
}
```

- [x] **Step 3.5: 运行全包测试验证通过**

```bash
go test ./internal/agentcore/retrieval/indexing/processor/chunker/ -v
```

预期：全部 PASS（含 13.5 既有测试 + 本计划新增测试；`TestHybridFactory_多余参数报错`、`TestHybridFactory_inner注入时13_8前占位报错` 行为不变仍 PASS）。

- [x] **Step 3.6: 提交**

```bash
git add internal/agentcore/retrieval/indexing/processor/chunker/registry.go internal/agentcore/retrieval/indexing/processor/chunker/registry_test.go internal/agentcore/retrieval/indexing/processor/chunker/char_chunker_test.go
git commit -m "feat(chunker): 13.6 注册 char 到 registry + hybridFactory 默认 inner 回填（⤴️ 解除 13.5 两处占位）"
```

---

### Task 4: doc.go 文件目录 + 计划表状态回填 + 覆盖率确认

**Files:**
- Modify: `internal/agentcore/retrieval/indexing/processor/chunker/doc.go`（文件目录段）
- Modify: `IMPLEMENTATION_PLAN.md`（13.6 行状态符号）
- Modify: `docs/superpowers/plans/2026-10-19-charchunker-13.6.md`（勾选本计划 checkbox）

- [x] **Step 4.1: 更新 doc.go 文件目录**

在 `internal/agentcore/retrieval/indexing/processor/chunker/doc.go` 中，将文件目录段：

```go
//	chunker/
//	├── doc.go        # 包文档
//	├── base.go       # Chunker 接口 + BaseChunker 默认适配器 + 构造校验
//	└── registry.go   # 分块器注册表 + GetChunker/RegisterChunker + hybridFactory
```

替换为：

```go
//	chunker/
//	├── doc.go            # 包文档
//	├── base.go           # Chunker 接口 + BaseChunker 默认适配器 + 构造校验
//	├── registry.go       # 分块器注册表 + GetChunker/RegisterChunker + hybridFactory
//	├── text_splitter.go  # TextSplitter 接口 + CharSplitter 滑窗分割（IndexSentenceSplitter 13.7）
//	└── char_chunker.go   # CharChunker 字符级分块器（"char" 注册项）
```

（doc.go 使用 tab 缩进对齐；新增行注释末尾标注 13.7 归属，呼应 13.9 收尾）

- [x] **Step 4.2: 回填 IMPLEMENTATION_PLAN.md 13.6 状态**

`IMPLEMENTATION_PLAN.md` 13.6 行（约 792 行），**仅改状态符号 `☐` → `✅`，行内其余内容一字不动**：

```
| 13.6 | ✅ | CharChunker | `CharChunker(嵌入 BaseChunker 仅覆写 ChunkText：...（原文保持）...` |
```

同时将 13.7 行状态 `☐` → `🔄`（下一章 13.7 TokenizerChunker 即将开始；若本次会话不继续 13.7 则跳过此步）。

- [x] **Step 4.3: 覆盖率确认**

```bash
go test ./internal/agentcore/retrieval/indexing/processor/chunker/ -cover
```

预期：`ok` 且 coverage **≥ 85%**（规范 3 硬性要求）。若不达标，用 `go test ./internal/agentcore/retrieval/indexing/processor/chunker/ -coverprofile=/tmp/c.out && go tool cover -func=/tmp/c.out` 定位未覆盖分支补测试（优先补：NewCharSplitter 钳制边界、Split 空/nil 分支）。

- [x] **Step 4.4: 全仓编译安全检查**

```bash
go build ./...
```

预期：无输出（编译通过）。确认无未用 import 等问题殃及其他包。

- [x] **Step 4.5: 勾选本计划 checkbox 并提交**

将本计划文档中所有 `- [ ]` 改为 `- [x]`，然后：

```bash
git add internal/agentcore/retrieval/indexing/processor/chunker/doc.go IMPLEMENTATION_PLAN.md docs/superpowers/plans/2026-10-19-charchunker-13.6.md
git commit -m "docs(chunker): 13.6 doc.go 文件目录 + 计划表状态回填 + 计划 checkbox 勾选"
```

---

## 自审记录（writing-plans Self-Review）

1. **Spec 覆盖**：设计文档决策 1/2/3 → Task 1；决策 4（计划表回填）→ Task 4.2；hybridFactory 回填 → Task 3.4；日志 → Task 2.3；rune 切片 → Task 1.3 + 专项测试；doc.go → Task 4.1。设计"不做的事"（IndexSentenceSplitter/tiktoken/HybridChunker 本体/DEFAULT_CHUNK_SIZE）均未混入。✅
2. **占位符扫描**：所有代码块完整可编译，无 TBD/TODO/"适当处理"。✅
3. **类型一致性**：`NewCharSplitter(chunkSize, chunkOverlap int) *CharSplitter`、`Split(doc *reranker.Document) ([]common.TextChunk, error)`、`NewCharChunker(opts ...ChunkerOption) (*CharChunker, error)` 在 Task 1/2/3 间一致；测试引用的 `WithChunkSize/WithChunkOverlap/WithExtra/WithInnerChunker/GetChunker` 均为 13.5 已交付符号。✅
