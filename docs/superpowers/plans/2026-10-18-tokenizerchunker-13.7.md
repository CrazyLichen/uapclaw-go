# 13.7 TokenizerChunker 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 TokenizerChunker（token 预算句级分块器），连带带出 splitter 包（SentenceSplitter + Splitter 基类，13.18 归属编号不变），注册 "tokenizer" 到 chunker registry。

**Architecture:** chunker → splitter 单向依赖。splitter 是纯算法层（收已解析的 enc/dec 函数对，sentencizer 分句 + token 预算打包）；chunker 是策略解析层（Tokenizer 接口三态解析 → 函数对传给 splitter）。设计文档：`docs/superpowers/specs/2026-10-18-tokenizerchunker-13.7-design.md`（偏差清单、决策理由全部在其中，实施时如与本计划冲突以设计文档为准）。

**Tech Stack:** `github.com/sentencizer/sentencizer v0.3.0`（新增依赖，零传递依赖）、`github.com/tiktoken-go/tokenizer v0.8.0`（已有）、`github.com/google/uuid`（已有）。

**Python 参考**（实施每个 Task 前先读对应 Python 源码）：
- `openjiuwen/core/retrieval/indexing/processor/chunker/tokenizer_chunker.py`
- `openjiuwen/core/retrieval/indexing/processor/chunker/text_splitter.py`（IndexSentenceSplitter/_resolve_tokenizer/_max_length/_resolve_chunk_size）
- `openjiuwen/core/retrieval/indexing/processor/splitter/base.py`
- `openjiuwen/core/retrieval/indexing/processor/splitter/splitter.py`

**编码规范提醒**：所有注释中文；声明顺序（结构体→枚举→常量→全局变量→导出函数→非导出函数）；每包覆盖率 ≥85%；日志用 `logger.Info/Warn(logger.ComponentAgentCore)` 结构化风格（对齐 char_chunker.go 先例）；`export GOPROXY=https://goproxy.cn,direct` 后编译。

---

### Task 1: 新增 sentencizer 依赖 + splitter 包骨架（doc.go）

**Files:**
- Modify: `go.mod` / `go.sum`
- Create: `internal/agentcore/retrieval/indexing/processor/splitter/doc.go`

- [x] **Step 1: 添加依赖**

```bash
cd /home/opensource/uapclaw-gateway
export GOPROXY=https://goproxy.cn,direct
go get github.com/sentencizer/sentencizer@v0.3.0
```

Expected: go.mod require 块出现 sentencizer v0.3.0。

- [x] **Step 2: 创建 splitter/doc.go**

```go
// Package splitter 提供句级文本分割：把整篇文档文本按句子边界切分，
// 再按 token 预算打包成块，供 chunker 层（如 TokenizerChunker）消费。
//
// 本包是纯算法层：构造时接收已解析的 encode/decode 函数对（由 chunker
// 包的 IndexSentenceSplitter 解析 Tokenizer 接口三态后传入），不做接口
// 探测。分句底层依赖 sentencizer（pysbd 启发的 Go 实现，对齐 Python pysbd）。
//
// 文件目录：
//
//	splitter/
//	├── doc.go       # 包文档
//	├── base.go      # Splitter 接口 + BaseSplitter 基类（构造校验+共享方法）
//	└── splitter.go  # SentenceSplitter（中英文检测+分句+token 预算打包）
//
// 对应 Python 代码：openjiuwen/core/retrieval/indexing/processor/splitter/
package splitter
```

- [x] **Step 3: 编译验证**

```bash
go build ./internal/agentcore/retrieval/indexing/processor/splitter/
```

Expected: 编译通过（空包+doc.go）。

- [x] **Step 4: Commit**

```bash
git add go.mod go.sum internal/agentcore/retrieval/indexing/processor/splitter/doc.go
git commit -m "feat(13.7): 新增 sentencizer 依赖 + splitter 包骨架"
```

---

### Task 2: BaseSplitter 基类（base.go）

**Files:**
- Create: `internal/agentcore/retrieval/indexing/processor/splitter/base.go`
- Test: `internal/agentcore/retrieval/indexing/processor/splitter/base_test.go`

对齐 Python `splitter/base.py`（Splitter ABC 全部 154 行）。155102 tokenizer 校验省略（设计文档偏差 6：enc 函数由调用方解析后传入，类型系统静态保证）。

- [x] **Step 1: 写失败测试（构造校验 + GetTokenCount + SplitText）**

```go
package splitter

import (
	"strings"
	"testing"
)

// fakeSplitDocSplitter Splitter 接口 fake：按固定句数返回
type fakeSplitDocSplitter struct {
	*BaseSplitter
	spans []ChunkSpan
}

func (f *fakeSplitDocSplitter) SplitDoc(doc string) ([]ChunkSpan, error) {
	return f.spans, nil
}

func TestNewBaseSplitter_合法参数(t *testing.T) {
	enc := func(s string) ([]int, error) { return make([]int, len([]rune(s))), nil }
	bs, err := NewBaseSplitter(512, 50, enc, nil)
	if err != nil {
		t.Fatalf("合法参数不应报错: %v", err)
	}
	if bs.ChunkSize != 512 || bs.ChunkOverlap != 50 {
		t.Fatalf("字段未正确保存: %+v", bs)
	}
}

func TestNewBaseSplitter_size非正数报错(t *testing.T) {
	_, err := NewBaseSplitter(0, 50, nil, nil)
	if err == nil {
		t.Fatal("chunk_size<=0 应报 155100")
	}
}

func TestNewBaseSplitter_overlap负数报错(t *testing.T) {
	_, err := NewBaseSplitter(512, -1, nil, nil)
	if err == nil {
		t.Fatal("overlap<0 应报 155101")
	}
}

func TestNewBaseSplitter_overlap大于等于size报错(t *testing.T) {
	_, err := NewBaseSplitter(50, 50, nil, nil)
	if err == nil {
		t.Fatal("overlap>=size 应报 155101")
	}
}

func TestGetTokenCount_有enc(t *testing.T) {
	enc := func(s string) ([]int, error) { return make([]int, len([]rune(s))), nil }
	bs, _ := NewBaseSplitter(512, 50, enc, nil)
	if got := bs.GetTokenCount("你好ab"); got != 4 {
		t.Fatalf("enc 计数应为 4 (rune 数), got %d", got)
	}
}

func TestGetTokenCount_无enc按字符(t *testing.T) {
	bs, _ := NewBaseSplitter(512, 50, nil, nil)
	if got := bs.GetTokenCount("你好ab"); got != 4 {
		t.Fatalf("无 enc 应按 rune 计数 4, got %d", got)
	}
}

func TestSplitText(t *testing.T) {
	enc := func(s string) ([]int, error) { return make([]int, len([]rune(s))), nil }
	bs, _ := NewBaseSplitter(512, 50, enc, nil)
	fake := &fakeSplitDocSplitter{BaseSplitter: bs, spans: []ChunkSpan{
		{Text: "第一句。", Start: 0, End: 4},
		{Text: "第二句！", Start: 4, End: 8},
	}}
	got := bs.SplitText("whatever")
	if len(got) != 2 || got[0] != "第一句。" || got[1] != "第二句！" {
		t.Fatalf("SplitText 应只取 text: %v", got)
	}
	_ = strings.TrimSpace // 占位使用
}
```

- [x] **Step 2: 运行确认失败**

```bash
go test ./internal/agentcore/retrieval/indexing/processor/splitter/ -run "TestNewBaseSplitter|TestGetTokenCount|TestSplitText" -v
```

Expected: FAIL（NewBaseSplitter/ChunkSpan 未定义）。

- [x] **Step 3: 实现 base.go**

```go
package splitter

import (
	"context"
	"strconv"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/reranker"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// Splitter 文本分割器抽象接口。
//
// 对齐 Python Splitter ABC 的 __call__ 抽象方法（Go 侧改名 SplitDoc
// 避免与接口自身语义混淆）。
//
// Python: openjiuwen/core/retrieval/indexing/processor/splitter/base.py (Splitter.__call__)
type Splitter interface {
	// SplitDoc 分割文档文本，返回分句片段列表（文本 + 原文坐标）。
	SplitDoc(doc string) ([]ChunkSpan, error)
}

// ChunkSpan 分句片段（文本 + 原文字节坐标）。
//
// 对齐 Python __call__ 返回的 List[Tuple[str, int, int]]。
type ChunkSpan struct {
	// Text 句子文本
	Text string
	// Start 原文起始字节位置
	Start int
	// End 原文结束字节位置
	End int
}

// BaseSplitter 分割器基类适配器。
//
// 对齐 Python Splitter 基类的可复用部分：构造校验（155100/155101）+
// tokenizer 函数保存 + GetNodesFromDocuments/SplitText/GetTokenCount。
// 具体 Splitter 嵌入本结构体，覆写 SplitDoc。
//
// Python: splitter/base.py (Splitter)
type BaseSplitter struct {
	// ChunkSize 分块大小（token 数）
	ChunkSize int
	// ChunkOverlap 相邻块重叠（token 数）
	ChunkOverlap int
	// tokenizerEnc encode 函数（text → token id 列表），可为 nil
	tokenizerEnc func(string) ([]int, error)
	// tokenizerDec decode 函数（token id 列表 → text），可为 nil；
	// 为 nil 时长句不做子切分（对齐 Python dec=None）
	tokenizerDec func([]int) (string, error)
	// self 完整 Splitter 引用（子类构造时经 SetSelf 注入），
	// 使 GetNodesFromDocuments/SplitText 路由到子类覆写的 SplitDoc
	self Splitter
}

// ──────────────────────────── 枚举 ────────────────────────────
// ──────────────────────────── 常量 ────────────────────────────

const (
	// logComponent 日志组件标识（对齐 chunker 包先例）
	logComponent = logger.ComponentAgentCore
)

// ──────────────────────────── 全局变量 ────────────────────────────
// ──────────────────────────── 导出函数 ────────────────────────────

// NewBaseSplitter 构造并校验参数。
//
// 校验规则一比一对齐 Python Splitter.__init__：
//   - chunkSize <= 0 → 155100 RETRIEVAL_INDEXING_CHUNK_SIZE_INVALID
//   - chunkOverlap < 0 或 >= chunkSize → 155101 RETRIEVAL_INDEXING_CHUNK_OVERLAP_INVALID
//
// Python 的 155102 tokenizer 校验（无 encode 且不可 callable）在 Go 侧
// 不可达省略：enc/dec 是函数类型，静态保证（设计文档偏差 6）。
//
// Python: splitter/base.py Splitter.__init__
func NewBaseSplitter(chunkSize, chunkOverlap int, enc func(string) ([]int, error), dec func([]int) (string, error)) (*BaseSplitter, error) {
	if chunkSize <= 0 {
		return nil, exception.BuildError(
			exception.StatusRetrievalIndexingChunkSizeInvalid,
			exception.WithParam("error_msg", "chunk_size must be greater than 0, current value: "+strconv.Itoa(chunkSize)),
		)
	}
	if chunkOverlap < 0 {
		return nil, exception.BuildError(
			exception.StatusRetrievalIndexingChunkOverlapInvalid,
			exception.WithParam("error_msg", "chunk_overlap must be greater than or equal to 0, current value: "+strconv.Itoa(chunkOverlap)),
		)
	}
	if chunkOverlap >= chunkSize {
		return nil, exception.BuildError(
			exception.StatusRetrievalIndexingChunkOverlapInvalid,
			exception.WithParam("error_msg", "chunk_overlap ("+strconv.Itoa(chunkOverlap)+") must be less than chunk_size ("+strconv.Itoa(chunkSize)+")"),
		)
	}
	return &BaseSplitter{
		ChunkSize:    chunkSize,
		ChunkOverlap: chunkOverlap,
		tokenizerEnc: enc,
		tokenizerDec: dec,
	}, nil
}

// GetNodesFromDocuments 对文档列表分块，产出 TextChunk 列表。
//
// 空文档（nil 或 Text 为空）记 Warn 跳过；每个 span 经
// NewTextChunkFromDocument 生成 TextChunk；尾部汇总日志。
//
// Python: splitter/base.py Splitter.get_nodes_from_documents
func (b *BaseSplitter) GetNodesFromDocuments(docs []reranker.Document) ([]common.TextChunk, error) {
	_ = context.TODO() // 对齐 Python 同步签名
	returnedNodes := make([]common.TextChunk, 0)
	for i := range docs {
		doc := &docs[i]
		if doc.Text == "" {
			logger.Warn(logComponent).
				Str("doc_id", doc.ID).
				Msg("Skipping empty document")
			continue
		}
		chunkTuples, err := b.splitDocOf(doc.Text)
		if err != nil {
			return nil, err
		}
		for _, ct := range chunkTuples {
			node := common.NewTextChunkFromDocument(doc, ct.Text, "")
			returnedNodes = append(returnedNodes, *node)
		}
	}
	logger.Info(logComponent).
		Int("chunk_count", len(returnedNodes)).
		Int("doc_count", len(docs)).
		Msg("Generated text chunks from documents")
	return returnedNodes, nil
}

// SplitText 分割文本，仅返回文本列表（不含坐标）。
//
// Python: splitter/base.py Splitter.split_text
func (b *BaseSplitter) SplitText(text string) ([]string, error) {
	chunks, err := b.splitDocOf(text)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(chunks))
	for _, c := range chunks {
		out = append(out, c.Text)
	}
	return out, nil
}

// GetTokenCount 计算文本 token 数。
//
// enc 未设置时按 rune 字符计数（对齐 Python len(text)）。
//
// Python: splitter/base.py Splitter._get_token_count
func (b *BaseSplitter) GetTokenCount(text string) int {
	if b.tokenizerEnc != nil {
		tokens, err := b.tokenizerEnc(text)
		if err == nil {
			return len(tokens)
		}
		// 编码失败按 Python 的 len(str(tokens)) 语义不现实，此处按字符数降级
	}
	return len([]rune(text))
}

// SetSelf 注入完整 Splitter 引用（子类构造函数调用），
// 使 GetNodesFromDocuments/SplitText 路由到子类覆写的 SplitDoc。
func (b *BaseSplitter) SetSelf(s Splitter) {
	b.self = s
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// splitDocOf 路由到子类覆写的 SplitDoc；未注入时返回错误。
func (b *BaseSplitter) splitDocOf(text string) ([]ChunkSpan, error) {
	if b.self != nil {
		return b.self.SplitDoc(text)
	}
	return nil, fmt.Errorf("BaseSplitter used standalone: call SetSelf before GetNodesFromDocuments")
}
```

**注意**：上面 `splitDocOf` 引用了 `fmt`，import 块需补 `"fmt"`。BaseSplitter 的 `self` 字段与 `SetSelf` 方法已并入上方结构体定义（对齐 chunker.BaseChunker 的 SetSelf 注入模式，13.5 定稿约定）。

- [x] **Step 4: 运行测试通过**

```bash
go test ./internal/agentcore/retrieval/indexing/processor/splitter/ -v
```

Expected: 全 PASS。

- [x] **Step 5: Commit**

```bash
git add internal/agentcore/retrieval/indexing/processor/splitter/
git commit -m "feat(13.7): splitter 包 BaseSplitter 基类（构造校验+GetNodesFromDocuments+SplitText+GetTokenCount）"
```

---

### Task 3: detectChinese 中英检测

**Files:**
- Modify: `internal/agentcore/retrieval/indexing/processor/splitter/splitter.go`（创建）
- Test: `internal/agentcore/retrieval/indexing/processor/splitter/splitter_test.go`（创建）

对齐 Python `_detect_chinese`。numpy 向量化与 5000 万字符分块省略（偏差 7），语义早退保留。

- [x] **Step 1: 写失败测试（表驱动）**

```go
package splitter

import "testing"

func TestDetectChinese(t *testing.T) {
	tests := []struct {
		name string
		text string
		want string
	}{
		{"纯中文", "这是中文测试句子。", "zh"},
		{"纯英文", "This is English text.", "en"},
		{"空文本", "", "en"},
		{"中文刚好过10%阈值", strings.Repeat("a", 8) + "中中", "zh"},           // 2/10=20% > 10%
		{"中文低于10%阈值", strings.Repeat("a", 20) + "中", "en"},              // 1/21 ≈ 4.8% < 10%
		{"标点启发fallback", "真的吗？太好了！Really？Great！", "zh"},            // CJK 0 但 ？> ? 且 ！> !
		{"中英混排过半", "Hello 世界 Hello 世界 你好你好。", "zh"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := detectChinese(tt.text, 0.1); got != tt.want {
				t.Fatalf("detectChinese(%q) = %q, want %q", tt.text, got, tt.want)
			}
		})
	}
}
```

import 补 `"strings"`。

- [x] **Step 2: 运行确认失败**

```bash
go test ./internal/agentcore/retrieval/indexing/processor/splitter/ -run TestDetectChinese -v
```

Expected: FAIL（detectChinese 未定义）。

- [x] **Step 3: 实现 splitter.go 的 detectChinese 部分**

```go
package splitter

// ──────────────────────────── 结构体 ────────────────────────────
// ──────────────────────────── 枚举 ────────────────────────────
// ──────────────────────────── 常量 ────────────────────────────
// ──────────────────────────── 全局变量 ────────────────────────────
// ──────────────────────────── 导出函数 ────────────────────────────
// ──────────────────────────── 非导出函数 ────────────────────────────

// detectChinese 检测文本主要为中文还是英文（基于字符分布）。
//
// 中文字符（CJK 统一表意 0x4E00-0x9FFF）占比 >= threshold 判 zh；
// 未达标时用标点启发（？> ? 且 ！> ! 判 zh）。计满阈值即早退。
//
// Python 的 numpy UTF-32 向量化与 5000 万字符分块统计在 Go 侧省略
// （numpy 特有内存优化，rune 单循环足够快；语义早退保留）。
//
// Python: splitter/splitter.py SentenceSplitter._detect_chinese
func detectChinese(text string, threshold float64) string {
	if text == "" {
		return "en"
	}
	runes := []rune(text)
	total := len(runes)
	thresholdVal := int(threshold * float64(total))
	chineseCount := 0
	for _, r := range runes {
		if r >= 0x4E00 && r <= 0x9FFF {
			chineseCount++
			// 语义早退：已计满阈值（对齐 Python 分块路径的早退）
			if chineseCount >= thresholdVal {
				return "zh"
			}
		}
	}
	isChinese := chineseCount >= thresholdVal
	if !isChinese {
		usingChinesePuncs := strings.Count(text, "？") > strings.Count(text, "?") &&
			strings.Count(text, "！") > strings.Count(text, "!")
		isChinese = usingChinesePuncs
	}
	if isChinese {
		return "zh"
	}
	return "en"
}
```

import 补 `"strings"`。

⚠️ **注意阈值语义**：Python 源码 `threshold_val = int(threshold * total_chars)` 且 numpy 路径**没有**循环内早退（早退只在 >5000 万字符的分块路径），统一路径最后 `chinese_count >= threshold_val` 判定。Go 侧把早退引入主循环是**行为增强**（同判定的更快路径，结果等价：循环内达到阈值 → 最终判定必为 zh）。如对此增强有疑虑，可去掉循环内早退改为纯计数——两者测试结果一致。

- [x] **Step 4: 运行测试通过**

```bash
go test ./internal/agentcore/retrieval/indexing/processor/splitter/ -run TestDetectChinese -v
```

Expected: 全 PASS。

- [x] **Step 5: Commit**

```bash
git add internal/agentcore/retrieval/indexing/processor/splitter/
git commit -m "feat(13.7): detectChinese 中英检测（CJK 占比+标点启发，语义早退保留）"
```

---

### Task 4: SentenceSplitter 主体（构造 + SplitDoc 打包循环 + flush）

**Files:**
- Modify: `internal/agentcore/retrieval/indexing/processor/splitter/splitter.go`
- Test: `internal/agentcore/retrieval/indexing/processor/splitter/splitter_test.go`

对齐 Python `splitter/splitter.py` 的 `SentenceSplitter.__init__`/`__call__`/`_flush`。span 恢复省略（偏差 2：sentencizer TextSpans 原生产出）。

- [x] **Step 1: 写失败测试**

```go
// splitter_test.go 追加

// oneTokPerCharEnc 1 rune = 1 token 的简化 enc，预算可手算
func oneTokPerCharEnc(s string) ([]int, error) {
	rs := []rune(s)
	ids := make([]int, len(rs))
	for i := range rs {
		ids[i] = i + 1
	}
	return ids, nil
}

func oneTokPerCharDec(ids []int) (string, error) {
	// 每个 id 映射回单 rune；仅为往返测试用（产出 rune i+1 对应字符）
	runes := make([]rune, len(ids))
	for i, id := range ids {
		runes[i] = rune('a' + (id % 26))
	}
	return string(runes), nil
}

func TestSentenceSplitter_SplitDoc_多短句打包(t *testing.T) {
	enc := oneTokPerCharEnc
	dec := oneTokPerCharDec
	bs, err := NewBaseSplitter(20, 4, enc, dec) // 预算 20 token，overlap 4
	if err != nil {
		t.Fatal(err)
	}
	ss := NewSentenceSplitter(bs, "auto")
	bs.SetSelf(ss)

	text := "一二三。四五。六七。八九。" // 每句 4 rune（4 token）
	spans, err := ss.SplitDoc(text)
	if err != nil {
		t.Fatal(err)
	}
	// 3 句 12 token <= 20 应打成一块
	if len(spans) != 1 {
		t.Fatalf("12 token 应打包成 1 块, got %d: %+v", len(spans), spans)
	}
	if spans[0].Text != text {
		t.Fatalf("块文本应为全句拼接: %q", spans[0].Text)
	}
	if spans[0].Start != 0 || spans[0].End != len(text) {
		t.Fatalf("span 应覆盖全文: %+v", spans[0])
	}
}

func TestSentenceSplitter_SplitDoc_溢出flush(t *testing.T) {
	enc := oneTokPerCharEnc
	bs, _ := NewBaseSplitter(8, 0, enc, nil) // 预算 8，无 overlap
	ss := NewSentenceSplitter(bs, "auto")
	bs.SetSelf(ss)

	text := "一二三四。五六。七八。" // 句1=5 token 超 8？不：5<=8；句2 加 3=8 边界；句3 触发 flush
	spans, err := ss.SplitDoc(text)
	if err != nil {
		t.Fatal(err)
	}
	if len(spans) < 2 {
		t.Fatalf("预算 8 应至少 2 块, got %d: %+v", len(spans), spans)
	}
	// 无 overlap：块间不应重复内容
	for i := 1; i < len(spans); i++ {
		if spans[i].Text == spans[i-1].Text && spans[i].Start == spans[i-1].Start {
			t.Fatalf("overlap=0 不应有重复块: %+v", spans[i])
		}
	}
}

func TestSentenceSplitter_SplitDoc_overlap尾缀(t *testing.T) {
	enc := oneTokPerCharEnc
	bs, _ := NewBaseSplitter(6, 4, enc, nil) // 预算 6，overlap 4
	ss := NewSentenceSplitter(bs, "auto")
	bs.SetSelf(ss)

	// 每句 3 token：句1+句2=6 打包；触发 flush 后尾缀句（3<=4）进下块开头
	text := "一二三。四五。六七。八九。"
	spans, err := ss.SplitDoc(text)
	if err != nil {
		t.Fatal(err)
	}
	if len(spans) < 2 {
		t.Fatalf("应至少 2 块, got %d: %+v", len(spans), spans)
	}
	// 第二块应以 flush 尾缀句开头（Python 语义：前缀句子复用）
	if len(spans) < 2 || !strings.Contains(spans[1].Text, "四五。") {
		t.Fatalf("overlap 尾缀应携带前块末句: %+v", spans)
	}
}

func TestSentenceSplitter_SplitDoc_空文本(t *testing.T) {
	enc := oneTokPerCharEnc
	bs, _ := NewBaseSplitter(8, 0, enc, nil)
	ss := NewSentenceSplitter(bs, "auto")
	bs.SetSelf(ss)
	for _, empty := range []string{"", "   \n  "} {
		spans, err := ss.SplitDoc(empty)
		if err != nil || len(spans) != 0 {
			t.Fatalf("空文本应返回空: %v %v", spans, err)
		}
	}
}

func TestSentenceSplitter_SplitDoc_英文lan透传(t *testing.T) {
	enc := oneTokPerCharEnc
	bs, _ := NewBaseSplitter(8, 0, enc, nil)
	ss := NewSentenceSplitter(bs, "en") // 显式 lan，不检测
	bs.SetSelf(ss)
	spans, err := ss.SplitDoc("Dr. Smith went home. He slept.") // en 模式缩写不切
	if err != nil {
		t.Fatal(err)
	}
	if len(spans) == 0 {
		t.Fatal("应至少 1 块")
	}
}
```

- [x] **Step 2: 运行确认失败**

```bash
go test ./internal/agentcore/retrieval/indexing/processor/splitter/ -run "TestSentenceSplitter" -v
```

Expected: FAIL（NewSentenceSplitter 未定义）。

- [x] **Step 3: 实现 SentenceSplitter**

splitter.go 追加（结构体区块加结构体，导出函数区块加构造，非导出区块加算法）：

```go
// ──────────────────────────── 结构体 ────────────────────────────

// SentenceSplitter 句级分割器：sentencizer 分句 + token 预算打包。
//
// 非并发安全（seg 每文档覆盖，对齐 Python self.seg），按文档使用。
//
// Python: splitter/splitter.py (SentenceSplitter)
type SentenceSplitter struct {
	// BaseSplitter 基类（ChunkSize/ChunkOverlap/enc/dec + 共享方法）
	*BaseSplitter
	// defaultLan 语言码："auto" → ""（每文档 detectChinese），其他原样透传 sentencizer
	defaultLan string
	// seg sentencizer 分句器，每文档重建（对齐 Python self.seg = Segmenter(...)）
	seg *sentencizer.Segmenter
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSentenceSplitter 构造句级分割器。
//
// 对齐 Python SentenceSplitter.__init__：
//   - lan == "auto" → defaultLan 置空（__call__ 时逐文档检测），否则原样保留
//   - dec 为 nil 时长句整句保留（不子切分）
//
// Python: splitter/splitter.py SentenceSplitter.__init__
func NewSentenceSplitter(bs *BaseSplitter, lan string) *SentenceSplitter {
	defaultLan := lan
	if lan == "auto" {
		defaultLan = ""
	}
	ss := &SentenceSplitter{BaseSplitter: bs, defaultLan: defaultLan}
	bs.SetSelf(ss)
	return ss
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// sentenceSpan 内部四元组：句子 + 坐标 + token 长度（tokenLen 不外泄到 ChunkSpan）
type sentenceSpan struct {
	text     string
	start    int
	end      int
	tokenLen int
}

// SplitDoc 分割文档：分句 → token 预算打包 → (text, start, end) 列表。
//
// 严格对齐 Python SentenceSplitter.__call__：
//   - 空文本 → []
//   - lan 路由（defaultLan 或 detectChinese）
//   - sentencizer 每文档重建（默认无 Clean()，对齐 pysbd clean=False）
//   - 单句超预算：flush 后 dec 有则子切分窗口、无则整句保留
//   - 短句累加：超预算 flush，当前句留作下块开头（overlap 修复语义）
//   - 尾部 flush
//
// Python: splitter/splitter.py SentenceSplitter.__call__
func (s *SentenceSplitter) SplitDoc(doc string) ([]ChunkSpan, error) {
	if strings.TrimSpace(doc) == "" {
		return []ChunkSpan{}, nil
	}

	lan := s.defaultLan
	if lan == "" {
		lan = detectChinese(doc, 0.1)
	}
	s.seg = sentencizer.NewSegmenter(lan)

	sentences := s.sentencesWithSpans(doc)
	chunks := make([]ChunkSpan, 0)
	curSents := make([]sentenceSpan, 0)

	for _, sent := range sentences {
		if sent.len() > s.ChunkSize {
			// 单句超预算：flush 缓冲，然后子切分或整句保留
			chunks, curSents = s.flush(chunks, curSents)
			if s.tokenizerDec != nil {
				subChunks, err := s.splitLongSegment(sent.text, sent.start, sent.end)
				if err != nil {
					return nil, err
				}
				chunks = append(chunks, subChunks...)
			} else {
				chunks = append(chunks, ChunkSpan{Text: sent.text, Start: sent.start, End: sent.end})
			}
			continue
		}

		curTokenCount := 0
		for _, cs := range curSents {
			curTokenCount += cs.tokenLen
		}
		if curTokenCount+sent.tokenLen <= s.ChunkSize {
			curSents = append(curSents, sent)
		} else {
			chunks, curSents = s.flush(chunks, curSents)
			// 当前句留作下块开头（对齐 Python overlap fix 注释）
			curSents = append(curSents, sent)
		}
	}

	chunks, _ = s.flush(chunks, curSents)
	logger.Info(logComponent).
		Int("chunk_count", len(chunks)).
		Msg("Computed the following sentence-level chunks")
	return chunks, nil
}

// sentencesWithSpans 分句并附加 token 长度。
//
// sentencizer TextSpans 原生产出 (start, end, sentence)——Python 的
// find-span 恢复 + used_spans 去重 + 失败 warning 不可达省略（偏差 2）。
// 空白句子过滤 + enc 编码求 tokenLen；enc 失败返回错误（决策 2b 上抛）。
//
// Python: splitter/splitter.py SentenceSplitter._sentences_with_spans
func (s *SentenceSplitter) sentencesWithSpans(text string) ([]sentenceSpan, error) {
	spans := s.seg.TextSpans(text)
	out := make([]sentenceSpan, 0, len(spans))
	for _, sp := range spans {
		if strings.TrimSpace(sp.Sentence) == "" {
			continue
		}
		tokens, err := s.tokenizerEnc(sp.Sentence)
		if err != nil {
			return nil, err
		}
		out = append(out, sentenceSpan{
			text:     sp.Sentence,
			start:    sp.Start,
			end:      sp.End,
			tokenLen: len(tokens),
		})
	}
	return out, nil
}

// flush 输出当前缓冲为一块，并计算 overlap 尾缀作为下块开头。
//
// 对齐 Python _flush：无分隔符拼接；span 取首尾句坐标；overlap>0 且
// 多于一句时，倒序累加 token 和 <= overlap 的句子前缀、正序放回。
//
// Python: splitter/splitter.py SentenceSplitter._flush
func (s *SentenceSplitter) flush(chunks []ChunkSpan, curSents []sentenceSpan) ([]ChunkSpan, []sentenceSpan) {
	if len(curSents) == 0 {
		return chunks, nil
	}

	var sb strings.Builder
	for _, cs := range curSents {
		sb.WriteString(cs.text)
	}
	chunks = append(chunks, ChunkSpan{
		Text:  sb.String(),
		Start: curSents[0].start,
		End:   curSents[len(curSents)-1].end,
	})

	nextCurSents := make([]sentenceSpan, 0)
	if s.ChunkOverlap > 0 && len(curSents) > 1 {
		overlapTokens := 0
		overlapSents := make([]sentenceSpan, 0)
		for i := len(curSents) - 1; i >= 0; i-- {
			cs := curSents[i]
			if overlapTokens+cs.tokenLen <= s.ChunkOverlap {
				overlapSents = append(overlapSents, cs)
				overlapTokens += cs.tokenLen
			} else {
				break
			}
		}
		// 倒序收集，正序放回
		for i, j := 0, len(overlapSents)-1; i < j; i, j = i+1, j-1 {
			overlapSents[i], overlapSents[j] = overlapSents[j], overlapSents[i]
		}
		nextCurSents = overlapSents
	}
	return chunks, nextCurSents
}

// len tokenLen 访问辅助
func (ss sentenceSpan) len() int { return ss.tokenLen }
```

import 块加 `"github.com/sentencizer/sentencizer"`。

⚠️ **两处代码内修正（实施时直接采用修正版，不要再引入歧义）**：
1. `sentencesWithSpans` 返回 `([]sentenceSpan, error)`，SplitDoc 内调用处写为 `sentences, err := s.sentencesWithSpans(doc); if err != nil { return nil, err }`
2. 删除 `len()` 辅助方法，循环内统一用 `sent.tokenLen` 字段直取

- [x] **Step 4: 运行测试通过**

```bash
go test ./internal/agentcore/retrieval/indexing/processor/splitter/ -v
```

Expected: 全 PASS（含 Task 3 的 detectChinese 用例）。

- [x] **Step 5: Commit**

```bash
git add internal/agentcore/retrieval/indexing/processor/splitter/
git commit -m "feat(13.7): SentenceSplitter 主体（分句+token预算打包+flush overlap尾缀）"
```

---

### Task 5: splitLongSegment 长句子切分

**Files:**
- Modify: `internal/agentcore/retrieval/indexing/processor/splitter/splitter.go`
- Test: `internal/agentcore/retrieval/indexing/processor/splitter/splitter_test.go`

对齐 Python `_split_long_segment`。

- [x] **Step 1: 写失败测试**

```go
// splitter_test.go 追加

// idEchoDec 把 id 列表解码回可见文本（id → 单字符），保证子窗口文本非空
func subWindowDec(ids []int) (string, error) {
	runes := make([]rune, len(ids))
	for i, id := range ids {
		runes[i] = rune('A' + (id % 26))
	}
	return string(runes), nil
}

func TestSentenceSplitter_splitLongSegment_滑窗子切分(t *testing.T) {
	enc := oneTokPerCharEnc
	dec := subWindowDec
	bs, _ := NewBaseSplitter(5, 2, enc, dec) // 预算5，overlap2 → step=3
	ss := NewSentenceSplitter(bs, "auto")
	bs.SetSelf(ss)

	// 构造超长单句（无句号直到结尾）
	text := "一二三四五六七八九。"
	spans, err := ss.SplitDoc(text)
	if err != nil {
		t.Fatal(err)
	}
	// 9 token > 5：应滑窗子切分，ceil((9-5)/3)+1 = 3 块
	if len(spans) != 3 {
		t.Fatalf("9 token 预算 5 step 3 应 3 块, got %d: %+v", len(spans), spans)
	}
	// 子窗口 span 沿用父句坐标
	for _, sp := range spans {
		if sp.Start != 0 || sp.End != len("一二三四五六七八九") {
			t.Fatalf("子窗口 span 应沿用父句坐标: %+v", sp)
		}
	}
}

func TestSentenceSplitter_splitLongSegment_dec为nil整句保留(t *testing.T) {
	enc := oneTokPerCharEnc
	bs, _ := NewBaseSplitter(5, 2, enc, nil) // dec=nil
	ss := NewSentenceSplitter(bs, "auto")
	bs.SetSelf(ss)

	text := "一二三四五六七八九。"
	spans, err := ss.SplitDoc(text)
	if err != nil {
		t.Fatal(err)
	}
	if len(spans) != 1 || spans[0].Text != "一二三四五六七八九。" {
		t.Fatalf("dec=nil 长句应整句保留: %+v", spans)
	}
}

func TestSentenceSplitter_splitLongSegment_decode失败跳过窗口(t *testing.T) {
	enc := oneTokPerCharEnc
	failDec := func(ids []int) (string, error) { return "", fmt.Errorf("decode boom") }
	bs, _ := NewBaseSplitter(5, 2, enc, failDec)
	ss := NewSentenceSplitter(bs, "auto")
	bs.SetSelf(ss)

	spans, err := ss.SplitDoc("一二三四五六七八九。")
	if err != nil {
		t.Fatalf("decode 失败应跳过不报错: %v", err)
	}
	// 全部窗口 decode 失败 → 整句 fallback（Python: out 为空时返回原句）
	if len(spans) != 1 {
		t.Fatalf("全失败应整句 fallback: %+v", spans)
	}
}

func TestSentenceSplitter_splitLongSegment_step退化1(t *testing.T) {
	enc := oneTokPerCharEnc
	dec := subWindowDec
	bs, _ := NewBaseSplitter(5, 5, enc, dec) // overlap=size → step=max(1,0)=1
	ss := NewSentenceSplitter(bs, "auto")
	bs.SetSelf(ss)

	spans, err := ss.SplitDoc("一二三四五六七八九。")
	if err != nil {
		t.Fatal(err)
	}
	// step=1：9 个起点，每个窗口非空 → 9 块
	if len(spans) != 9 {
		t.Fatalf("step=1 应 9 块, got %d: %+v", len(spans), spans)
	}
}
```

import 补 `"fmt"`（若尚无）。

- [x] **Step 2: 运行确认失败**

```bash
go test ./internal/agentcore/retrieval/indexing/processor/splitter/ -run splitLongSegment -v
```

Expected: FAIL 或 panic（splitLongSegment 未定义/未接线）。

- [x] **Step 3: 实现 splitLongSegment**

splitter.go 非导出区块追加：

```go
// splitLongSegment 把一个长句切成重叠的 token 窗口，保证内容不丢失。
//
// 对齐 Python _split_long_segment：整句 encode 一次 → step=max(1, size-overlap)
// 滑窗 → 每窗口 decode（失败或空白跳过，对齐 safe_decode→""→continue）→
// span 沿用父句坐标；全部窗口为空时整句 fallback。
//
// Python: splitter/splitter.py SentenceSplitter._split_long_segment
func (s *SentenceSplitter) splitLongSegment(sentText string, sentStart, sentEnd int) ([]ChunkSpan, error) {
	ids, err := s.tokenizerEnc(sentText)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return []ChunkSpan{{Text: sentText, Start: sentStart, End: sentEnd}}, nil
	}

	step := s.ChunkSize - s.ChunkOverlap
	if step < 1 {
		step = 1
	}
	out := make([]ChunkSpan, 0)
	nIDs := len(ids)
	for windowStart := 0; windowStart < nIDs; windowStart += step {
		windowEnd := windowStart + s.ChunkSize
		if windowEnd > nIDs {
			windowEnd = nIDs
		}
		window := ids[windowStart:windowEnd]
		if len(window) == 0 {
			break
		}
		text, err := s.tokenizerDec(window)
		if err != nil || strings.TrimSpace(text) == "" {
			continue
		}
		out = append(out, ChunkSpan{Text: text, Start: sentStart, End: sentEnd})
	}
	if len(out) == 0 {
		return []ChunkSpan{{Text: sentText, Start: sentStart, End: sentEnd}}, nil
	}
	return out, nil
}
```

- [x] **Step 4: 运行测试通过**

```bash
go test ./internal/agentcore/retrieval/indexing/processor/splitter/ -v
```

Expected: 全 PASS。

⚠️ **窗口数手算核对（实施前先跑 Python 对照）**：
- 滑窗用例（step=3, budget=5, 9 token）：起点 0,3,6 → 窗口 [0:5],[3:8],[6:9] → 3 块 ✓
- `step退化1` 用例（step=1, budget=5, 9 token）：`range(0,9,1)` 产生 9 个起点，每个窗口 ids[start:start+5] 均非空（最后 [8:9] 单 token）→ **期望应为 9 块**（计划中的 5 是笔误，以 `for windowStart := 0; windowStart < nIDs; windowStart += step` 循环语义为准）

```go
// step退化1 用例的期望修正：
	if len(spans) != 9 {
		t.Fatalf("step=1 应 9 块, got %d: %+v", len(spans), spans)
	}
```

- [x] **Step 5: Commit**

```bash
git add internal/agentcore/retrieval/indexing/processor/splitter/
git commit -m "feat(13.7): splitLongSegment 长句滑窗子切分（decode失败跳过+整句fallback+step退化1）"
```

---

### Task 6: chunker 包 Tokenizer 接口三态 + TiktokenTokenizer 适配器

**Files:**
- Modify: `internal/agentcore/retrieval/indexing/processor/chunker/text_splitter.go`
- Test: `internal/agentcore/retrieval/indexing/processor/chunker/text_splitter_test.go`

对齐 Python `_resolve_tokenizer`/`_max_length`。偏差 1（max_token_value 不模拟）、偏差 4（失败链保留）、决策 2b（encode 上抛）。

- [x] **Step 1: 写失败测试**

```go
// text_splitter_test.go 追加

// fakeTokEncOnly 只实现 Encode（对齐 Python tokenize-only 分支②）
type fakeTokEncOnly struct{}

func (f *fakeTokEncOnly) Encode(text string) ([]int, error) {
	return make([]int, len([]rune(text))), nil
}

// fakeTokFull 实现 Encode+Decode+MaxLength（对齐分支①+HF max_length）
type fakeTokFull struct{ maxLen int }

func (f *fakeTokFull) Encode(text string) ([]int, error) {
	return make([]int, len([]rune(text))), nil
}
func (f *fakeTokFull) Decode(ids []int) (string, error) {
	return strings.Repeat("a", len(ids)), nil
}
func (f *fakeTokFull) MaxLength() int { return f.maxLen }

// fakeTokEncFail Encode 恒失败（验证上抛）
type fakeTokEncFail struct{}

func (f *fakeTokEncFail) Encode(text string) ([]int, error) {
	return nil, fmt.Errorf("encode boom")
}
func (f *fakeTokEncFail) Decode(ids []int) (string, error) {
	return "", fmt.Errorf("decode boom")
}

func TestResolveTokenizers_完整实现(t *testing.T) {
	ft := &fakeTokFull{maxLen: 1024}
	enc, dec, maxLen := resolveTokenizers(ft)
	if maxLen != 1024 {
		t.Fatalf("maxLen 应为 1024, got %d", maxLen)
	}
	if enc == nil || dec == nil {
		t.Fatal("完整实现 enc/dec 都应有")
	}
	if got := len(mustEnc(t, enc, "你好")); got != 2 {
		t.Fatalf("enc 应按 rune 计数, got %d", got)
	}
}

func TestResolveTokenizers_空白文本返回空(t *testing.T) {
	ft := &fakeTokFull{}
	enc, _, _ := resolveTokenizers(ft)
	ids, err := enc("   \n ")
	if err != nil || len(ids) != 0 {
		t.Fatalf("空白文本应返回空切片: %v %v", ids, err)
	}
}

func TestResolveTokenizers_仅Encode(t *testing.T) {
	enc, dec, maxLen := resolveTokenizers(&fakeTokEncOnly{})
	if enc == nil {
		t.Fatal("enc 应有")
	}
	if dec != nil {
		t.Fatal("encode-only 实现 dec 应为 nil")
	}
	if maxLen != 0 {
		t.Fatalf("无 MaxLengthProvider maxLen 应为 0, got %d", maxLen)
	}
}

func TestResolveTokenizers_nil走tiktoken(t *testing.T) {
	enc, dec, maxLen := resolveTokenizers(nil)
	if enc == nil || dec == nil {
		t.Fatal("nil 应 fallback tiktoken（enc+dec）")
	}
	if maxLen != 0 {
		t.Fatalf("偏差1：maxLen 应为 0（max_token_value 不模拟）, got %d", maxLen)
	}
	// 真实 cl100k_base 编解码往返
	text := "你好世界 hello"
	ids, err := enc(text)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) == 0 {
		t.Fatal("真实 tiktoken 编码应有结果")
	}
	back, err := dec(ids)
	if err != nil {
		t.Fatal(err)
	}
	if back != text {
		t.Fatalf("tiktoken 往返不一致: %q != %q", back, text)
	}
}

func TestResolveTokenizers_encode失败上抛(t *testing.T) {
	enc, _, _ := resolveTokenizers(&fakeTokEncFail{})
	_, err := enc("anything")
	if err == nil {
		t.Fatal("encode 失败应上抛（决策 2b）")
	}
}

func mustEnc(t *testing.T, enc func(string) ([]int, error), text string) []int {
	t.Helper()
	ids, err := enc(text)
	if err != nil {
		t.Fatal(err)
	}
	return ids
}

func TestResolveChunkSize(t *testing.T) {
	tests := []struct {
		name      string
		chunkSize int
		maxLen    int
		want      int
	}{
		{"未设+有max", 0, 1024, 1024},
		{"都设取min", 512, 256, 256},
		{"chunkSize大max小", 100, 200, 100},
		{"都无默认200", 0, 0, 200},
		{"仅显式", 300, 0, 300},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolveChunkSize(tt.chunkSize, tt.maxLen); got != tt.want {
				t.Fatalf("resolveChunkSize(%d, %d) = %d, want %d", tt.chunkSize, tt.maxLen, got, tt.want)
			}
		})
	}
}
```

- [x] **Step 2: 运行确认失败**

```bash
go test ./internal/agentcore/retrieval/indexing/processor/chunker/ -run "TestResolve" -v
```

Expected: FAIL（resolveTokenizers/resolveChunkSize 未定义）。

- [x] **Step 3: 实现接口三态 + 适配器 + resolve 函数**

text_splitter.go 追加。结构体区块追加：

```go
// Tokenizer token 编码器最小接口（Encode 必选能力）。
//
// 对齐 Python _resolve_tokenizer 分支②③（tokenize-only / callable）——
// Go 接口本身就是 callable 语义，两类分支天然合并。
type Tokenizer interface {
	// Encode 编码文本为 token id 列表。失败上抛（决策 2b）。
	Encode(text string) ([]int, error)
}

// DecodeTokenizer 带 Decode 的完整 token 化器。
//
// 对齐 Python 分支①（HF encode+decode）：实现者使 SentenceSplitter
// 获得长句子切分能力（子窗口需 decode 回文本）。
type DecodeTokenizer interface {
	Tokenizer
	// Decode 解码 token id 列表为文本。
	Decode(ids []int) (string, error)
}

// MaxLengthProvider 可选长度上限提供者。
//
// 对齐 Python _max_length 的 4 属性 hasattr 探测（model_max_length/
// max_len_single_sentence/max_position_embeddings/max_seq_length），
// 简化为单方法——由实现者决定返回哪个语义的值（0 视为未提供）。
type MaxLengthProvider interface {
	// MaxLength 返回模型最大序列长度；0 表示未提供。
	MaxLength() int
}

// TiktokenTokenizer tiktoken-go Codec 适配器。
//
// 对齐 Python 分支④（nil → lazy import tiktoken cl100k_base）。
// 不实现 MaxLengthProvider：Python 把词表大小 max_token_value 当
// max_length 是无意识行为，Go Codec 无该属性（设计文档偏差 1）。
type TiktokenTokenizer struct {
	// codec tiktoken 编解码器
	codec tokenizer.Codec
}
```

常量区块追加：

```go
const (
	// defaultChunkSize SentenceSplitter 默认分块大小（对齐 Python DEFAULT_CHUNK_SIZE）
	defaultChunkSize = 200
	// defaultSafeEncodeMaxLength HF encode truncation 上限的文档锚点。
	//
	// Python: DEFAULT_SAFE_ENCODE_MAX_LENGTH = 65536（HF encode(max_length=...)
	// 的截断保护）。Go 接口无 truncation 参数概念，此常量仅作文档锚点保留，
	// 无运行时消费者（设计文档决策 2c）。
	defaultSafeEncodeMaxLength = 65536
)
```

导出函数区块追加：

```go
// NewTiktokenTokenizer 构造 cl100k_base 适配器。
//
// Python: _resolve_tokenizer 分支④ tiktoken.get_encoding("cl100k_base")
func NewTiktokenTokenizer() (*TiktokenTokenizer, error) {
	codec, err := tokenizer.Get(tokenizer.Cl100kBase)
	if err != nil {
		return nil, err
	}
	return &TiktokenTokenizer{codec: codec}, nil
}

// Encode 编码文本（[]uint→[]int 转换 + 薄转发）。
func (t *TiktokenTokenizer) Encode(text string) ([]int, error) {
	ids, _, err := t.codec.Encode(text)
	if err != nil {
		return nil, err
	}
	out := make([]int, len(ids))
	for i, id := range ids {
		out[i] = int(id)
	}
	return out, nil
}

// Decode 解码 token id 列表（[]int→[]uint 转换 + 薄转发）。
func (t *TiktokenTokenizer) Decode(ids []int) (string, error) {
	uints := make([]uint, len(ids))
	for i, id := range ids {
		uints[i] = uint(id)
	}
	return t.codec.Decode(uints)
}
```

非导出函数区块追加：

```go
// resolveTokenizers 解析 Tokenizer 接口三态为函数对。
//
// 执行顺序严格对齐 Python _resolve_tokenizer：
//  1. MaxLengthProvider 断言（对齐 _max_length hasattr 探测）
//  2. DecodeTokenizer 断言（对齐 encode+decode 分支①）
//  3. 仅 Tokenizer（对齐 tokenize-only/callable 分支②③，类型系统合并）
//  4. nil → TiktokenTokenizer fallback；失败 Warn + 空格分词 proxy（偏差 4 保留）
//
// Python: text_splitter.py IndexSentenceSplitter._resolve_tokenizer
func resolveTokenizers(t Tokenizer) (func(string) ([]int, error), func([]int) (string, error), int) {
	maxLen := 0
	if t != nil {
		if ml, ok := t.(MaxLengthProvider); ok {
			maxLen = ml.MaxLength()
		}
	}

	if t != nil {
		if dt, ok := t.(DecodeTokenizer); ok {
			return wrapSafeEncode(dt.Encode), dt.Decode, maxLen
		}
		return wrapSafeEncode(t.Encode), nil, maxLen
	}

	// 分支④：nil → tiktoken cl100k_base
	tk, err := NewTiktokenTokenizer()
	if err != nil {
		logger.Warn(logComponent).
			Err(err).
			Msg("Failed to load tiktoken fallback tokenizer")
		// 空格分词 proxy（对齐 Python text.split()，dec=nil）
		return func(text string) ([]int, error) {
			words := strings.Fields(text)
			ids := make([]int, len(words))
			for i := range words {
				ids[i] = i
			}
			return ids, nil
		}, nil, 0
	}
	return tk.Encode, tk.Decode, 0
}

// wrapSafeEncode 包装 Encode：空白文本返回空切片。
//
// 对齐 Python safe_encode 的 `if not (text and text.strip()): return []`；
// 其余直调（失败上抛，决策 2b——Go 无 truncation 参数，裸重试场景不存在）。
func wrapSafeEncode(enc func(string) ([]int, error)) func(string) ([]int, error) {
	return func(text string) ([]int, error) {
		if strings.TrimSpace(text) == "" {
			return []int{}, nil
		}
		return enc(text)
	}
}

// resolveChunkSize 决定最终 chunk_size。
//
// 对齐 Python _resolve_chunk_size：未设(0)+有max→max；都设→min；
// 否则 chunkSize（0 则 DEFAULT_CHUNK_SIZE）。Go 无 None，0 表示未设。
//
// Python: text_splitter.py IndexSentenceSplitter._resolve_chunk_size
func resolveChunkSize(chunkSize, maxTokenLength int) int {
	if chunkSize == 0 && maxTokenLength > 0 {
		return maxTokenLength
	}
	if chunkSize > 0 && maxTokenLength > 0 {
		if chunkSize < maxTokenLength {
			return chunkSize
		}
		return maxTokenLength
	}
	if chunkSize > 0 {
		return chunkSize
	}
	return defaultChunkSize
}
```

import 块追加 `"strings"`、`"github.com/tiktoken-go/tokenizer"`、`"github.com/uapclaw/uapclaw-go/internal/common/logger"`（logger 若 chunker 包已有则复用 char_chunker.go 的 logComponent 常量——**注意**：logComponent 定义在 char_chunker.go，本文件直接引用）。

- [x] **Step 4: 运行测试通过**

```bash
go test ./internal/agentcore/retrieval/indexing/processor/chunker/ -v
```

Expected: 全 PASS（含既有 13.5/13.6 测试）。

- [x] **Step 5: Commit**

```bash
git add internal/agentcore/retrieval/indexing/processor/chunker/
git commit -m "feat(13.7): Tokenizer 接口三态 + TiktokenTokenizer 适配器 + resolveTokenizers/resolveChunkSize"
```

---

### Task 7: IndexSentenceSplitter + TokenizerChunker

**Files:**
- Modify: `internal/agentcore/retrieval/indexing/processor/chunker/text_splitter.go`（IndexSentenceSplitter）
- Create: `internal/agentcore/retrieval/indexing/processor/chunker/tokenizer_chunker.go`
- Test: `internal/agentcore/retrieval/indexing/processor/chunker/tokenizer_chunker_test.go`

- [x] **Step 1: 写失败测试**

```go
package chunker

import (
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/reranker"
)

// tokByRune rune 计数 tokenizer（Encode+Decode 全实现）
type tokByRune struct{}

func (t *tokByRune) Encode(text string) ([]int, error) {
	rs := []rune(text)
	ids := make([]int, len(rs))
	for i := range rs {
		ids[i] = i + 1
	}
	return ids, nil
}
func (t *tokByRune) Decode(ids []int) (string, error) {
	runes := make([]rune, len(ids))
	for i, id := range ids {
		runes[i] = rune('a' + (id % 26))
	}
	return string(runes), nil
}

func TestNewTokenizerChunker_构造校验(t *testing.T) {
	if _, err := NewTokenizerChunker(0, 50, nil, "auto", nil); err == nil {
		t.Fatal("chunk_size<=0 应过基类校验 155100")
	}
	if _, err := NewTokenizerChunker(512, 600, nil, "auto", nil); err == nil {
		t.Fatal("overlap>=size 应过基类校验 155101")
	}
	c, err := NewTokenizerChunker(20, 4, &tokByRune{}, "auto", nil)
	if err != nil {
		t.Fatalf("合法参数不应报错: %v", err)
	}
	if c.ChunkSize != 20 || c.ChunkOverlap != 4 {
		t.Fatalf("基类字段未保存: %+v", c.BaseChunker)
	}
}

func TestTokenizerChunker_ChunkText_空文本(t *testing.T) {
	c, _ := NewTokenizerChunker(20, 4, &tokByRune{}, "auto", nil)
	got, err := c.ChunkText("")
	if err != nil || len(got) != 0 {
		t.Fatalf("空文本应返回空: %v %v", got, err)
	}
}

func TestTokenizerChunker_ChunkText_分块(t *testing.T) {
	c, _ := NewTokenizerChunker(12, 0, &tokByRune{}, "auto", nil) // 12 token 预算
	got, err := c.ChunkText("一二三。四五。六七。八九。十。") // 每句4token，共20
	if err != nil {
		t.Fatal(err)
	}
	if len(got) < 2 {
		t.Fatalf("20 token 预算 12 应至少 2 块: %v", got)
	}
	joined := strings.Join(got, "")
	if !strings.Contains(joined, "一二三。") {
		t.Fatalf("块内容应含首句: %v", got)
	}
}

func TestTokenizerChunker_ChunkDocuments_带元数据(t *testing.T) {
	c, _ := NewTokenizerChunker(12, 0, &tokByRune{}, "auto", nil)
	docs := []reranker.Document{{ID: "doc1", Text: "一二三。四五。六七。", Metadata: map[string]any{"src": "test"}}}
	chunks, err := c.ChunkDocuments(nil, docs)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) == 0 {
		t.Fatal("应有块产出")
	}
	for _, ch := range chunks {
		if ch.DocID != "doc1" {
			t.Fatalf("DocID 应为 doc1: %s", ch.DocID)
		}
		if ch.Metadata["src"] != "test" {
			t.Fatalf("元数据应透传: %v", ch.Metadata)
		}
		if _, ok := ch.Metadata["chunk_index"]; !ok {
			t.Fatal("应有 chunk_index 元数据")
		}
	}
}

func TestIndexSentenceSplitter_splitterConfig归一化不报错(t *testing.T) {
	// splitter_config 非 map（nil 或任意值）归一化后忽略——不应 panic/报错（偏差 3a）
	_, err := NewIndexSentenceSplitter(&tokByRune{}, 20, 4, nil, "auto")
	if err != nil {
		t.Fatalf("splitterConfig=nil 应归一化不报错: %v", err)
	}
}

func TestIndexSentenceSplitter_overlap为0取五分之一(t *testing.T) {
	// Python `chunk_overlap or chunk_size // 5`：overlap==0 → chunkSize/5
	sp, err := NewIndexSentenceSplitter(&tokByRune{}, 20, 0, nil, "auto")
	if err != nil {
		t.Fatal(err)
	}
	if sp.splitter.ChunkOverlap != 4 {
		t.Fatalf("overlap=0 应取 chunkSize/5=4, got %d", sp.splitter.ChunkOverlap)
	}
}

func TestIndexSentenceSplitter_maxLength参与chunkSize(t *testing.T) {
	// fakeTokFull 在 text_splitter_test.go 定义
	sp, err := NewIndexSentenceSplitter(&fakeTokFull{maxLen: 15}, 100, 0, nil, "auto")
	if err != nil {
		t.Fatal(err)
	}
	if sp.splitter.ChunkSize != 15 {
		t.Fatalf("chunkSize 应被 maxLen 钳到 15, got %d", sp.splitter.ChunkSize)
	}
}
```

- [x] **Step 2: 运行确认失败**

```bash
go test ./internal/agentcore/retrieval/indexing/processor/chunker/ -run "TestTokenizerChunker|TestIndexSentenceSplitter" -v
```

Expected: FAIL（NewTokenizerChunker/NewIndexSentenceSplitter 未定义）。

- [x] **Step 3: 实现 IndexSentenceSplitter（text_splitter.go 追加）**

结构体区块追加：

```go
// IndexSentenceSplitter 句子级分割器，SentenceSplitter 的包装层。
//
// 负责策略解析：Tokenizer 接口三态 → enc/dec 函数对 → 构造 splitter.SentenceSplitter。
//
// Python: text_splitter.py (IndexSentenceSplitter)
type IndexSentenceSplitter struct {
	// splitter 内部句级分割器
	splitter *splitter.SentenceSplitter
}
```

导出函数区块追加：

```go
// NewIndexSentenceSplitter 构造句子级分割器。
//
// 执行顺序严格对齐 Python IndexSentenceSplitter.__init__：
//  1. splitterConfig 非 map 归一化 {"paragraph_separator": "\n"} 后丢弃
//     （偏差 3a：Python 死参数，归一化结果无消费者，签名保留）
//  2. resolveTokenizers 解析接口三态
//  3. resolveChunkSize 决定最终 chunkSize
//  4. 构造 SentenceSplitter（overlap==0 → chunkSize/5，Python or 语义）
//
// Python: text_splitter.py IndexSentenceSplitter.__init__
func NewIndexSentenceSplitter(tokenizer Tokenizer, chunkSize, chunkOverlap int, splitterConfig map[string]any, language string) (*IndexSentenceSplitter, error) {
	if splitterConfig == nil {
		splitterConfig = map[string]any{"paragraph_separator": "\n"}
	}
	_ = splitterConfig // 归一化后无消费者（对齐 Python 死参数）

	enc, dec, maxLen := resolveTokenizers(tokenizer)
	finalChunkSize := resolveChunkSize(chunkSize, maxLen)

	overlap := chunkOverlap
	if overlap == 0 {
		overlap = finalChunkSize / 5 // Python `chunk_overlap or chunk_size // 5`
	}

	bs, err := splitter.NewBaseSplitter(finalChunkSize, overlap, enc, dec)
	if err != nil {
		return nil, err
	}
	ss := splitter.NewSentenceSplitter(bs, language)
	return &IndexSentenceSplitter{splitter: ss}, nil
}

// Split 实现 TextSplitter 接口：委托 GetNodesFromDocuments。
//
// Python 的 TextChunk|Document 联合入参在 Go 侧收窄为 *Document
// （13.6 决策），isinstance 转换分支消失。
//
// Python: text_splitter.py IndexSentenceSplitter.split
func (s *IndexSentenceSplitter) Split(doc *reranker.Document) ([]common.TextChunk, error) {
	return s.splitter.GetNodesFromDocuments([]reranker.Document{*doc})
}
```

编译期断言（全局变量区块追加）：

```go
// 编译期断言 IndexSentenceSplitter 实现 TextSplitter
var _ TextSplitter = (*IndexSentenceSplitter)(nil)
```

import 追加 `"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/indexing/processor/splitter"`。

- [x] **Step 4: 实现 TokenizerChunker（tokenizer_chunker.go 新文件）**

```go
package chunker

import (
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/reranker"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TokenizerChunker token 预算分块器。
//
// 嵌入 BaseChunker 获得默认 ChunkDocuments/Process；ChunkText 委托
// IndexSentenceSplitter 做句级切分 + token 预算打包。
//
// Python: openjiuwen/core/retrieval/indexing/processor/chunker/tokenizer_chunker.py (TokenizerChunker)
type TokenizerChunker struct {
	// BaseChunker 默认适配器
	*BaseChunker
	// splitter 句子级分割器
	splitter *IndexSentenceSplitter
}

// ──────────────────────────── 枚举 ────────────────────────────
// ──────────────────────────── 常量 ────────────────────────────
// ──────────────────────────── 全局变量 ────────────────────────────

// 编译期断言 TokenizerChunker 实现 Chunker
var _ Chunker = (*TokenizerChunker)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTokenizerChunker 构造 TokenizerChunker。
//
// 构造链：NewBaseChunker（155100/155101 校验）→ NewIndexSentenceSplitter
// （tokenizer 三态解析 + chunkSize 决策）→ SetSelf 注入。
//
// Python: tokenizer_chunker.py TokenizerChunker.__init__
func NewTokenizerChunker(chunkSize, chunkOverlap int, tokenizer Tokenizer, language string, splitterConfig map[string]any) (*TokenizerChunker, error) {
	bc, err := NewBaseChunker(chunkSize, chunkOverlap)
	if err != nil {
		return nil, err
	}
	sp, err := NewIndexSentenceSplitter(tokenizer, chunkSize, chunkOverlap, splitterConfig, language)
	if err != nil {
		return nil, err
	}
	c := &TokenizerChunker{BaseChunker: bc, splitter: sp}
	bc.SetSelf(c)
	return c, nil
}

// ChunkText token 预算分块。
//
// 对齐 Python：空文本→[]；Document(text, metadata={}) → split →
// 收集 node.text；完成日志 chunk_count。
//
// Python: tokenizer_chunker.py TokenizerChunker.chunk_text
func (c *TokenizerChunker) ChunkText(text string) ([]string, error) {
	if text == "" {
		return []string{}, nil
	}
	doc := &reranker.Document{Text: text, Metadata: map[string]any{}}
	textNodes, err := c.splitter.Split(doc)
	if err != nil {
		return nil, err
	}
	chunks := make([]string, 0, len(textNodes))
	for _, node := range textNodes {
		chunks = append(chunks, node.Text)
	}
	// 对齐 Python: logger.info("Token chunking completed: generated %d chunks", len(chunks))
	logger.Info(logComponent).
		Int("chunk_count", len(chunks)).
		Msg("Token chunking completed")
	return chunks, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────
```

- [x] **Step 5: 更新 chunker/doc.go 文件目录**

```go
// 文件目录：
//
//	chunker/
//	├── doc.go               # 包文档
//	├── base.go              # Chunker 接口 + BaseChunker 默认适配器 + 构造校验
//	├── registry.go          # 分块器注册表 + GetChunker/RegisterChunker + hybridFactory
//	├── text_splitter.go     # TextSplitter 接口 + CharSplitter + Tokenizer 三态接口
//	│                        #   + TiktokenTokenizer + IndexSentenceSplitter（13.7）
//	├── char_chunker.go      # CharChunker 字符级分块器（"char" 注册项）
//	└── tokenizer_chunker.go # TokenizerChunker token 预算分块器（"tokenizer" 注册项）
```

- [x] **Step 6: 运行测试通过**

```bash
go test ./internal/agentcore/retrieval/indexing/processor/chunker/ -v
```

Expected: 全 PASS。

- [x] **Step 7: Commit**

```bash
git add internal/agentcore/retrieval/indexing/processor/chunker/
git commit -m "feat(13.7): IndexSentenceSplitter + TokenizerChunker（构造链+ChunkText+doc.go 更新）"
```

---

### Task 8: registry 注册 "tokenizer"（主动增强）

**Files:**
- Modify: `internal/agentcore/retrieval/indexing/processor/chunker/registry.go`
- Test: `internal/agentcore/retrieval/indexing/processor/chunker/registry_test.go`

- [x] **Step 1: 写失败测试**

```go
// registry_test.go 追加

func TestGetChunker_tokenizer注册(t *testing.T) {
	// 主动增强：Python 不注册 TokenizerChunker，Go 侧注册（设计文档偏差 5）
	c, err := GetChunker("tokenizer",
		WithChunkSize(20), WithChunkOverlap(4),
		WithExtra("tokenizer", &tokByRune{}),
		WithExtra("language", "auto"),
	)
	if err != nil {
		t.Fatalf("tokenizer 应可获取: %v", err)
	}
	got, err := c.ChunkText("一二三。四五。六七。八九。")
	if err != nil || len(got) == 0 {
		t.Fatalf("分块应有产出: %v %v", got, err)
	}
}

func TestGetChunker_tokenizer_extra类型错误(t *testing.T) {
	_, err := GetChunker("tokenizer",
		WithChunkSize(20), WithChunkOverlap(4),
		WithExtra("tokenizer", "not-a-tokenizer"),
	)
	if err == nil {
		t.Fatal("tokenizer Extra 类型错误应报错")
	}
}

func TestGetChunker_tokenizer_extra缺省走nil(t *testing.T) {
	// 不传 tokenizer → nil → tiktoken fallback，应能正常分块
	c, err := GetChunker("tokenizer", WithChunkSize(512), WithChunkOverlap(50))
	if err != nil {
		t.Fatalf("缺省 tokenizer 应走 nil fallback: %v", err)
	}
	if _, err := c.ChunkText("这是测试文本。"); err != nil {
		t.Fatalf("tiktoken fallback 分块应成功: %v", err)
	}
}
```

- [x] **Step 2: 运行确认失败**

```bash
go test ./internal/agentcore/retrieval/indexing/processor/chunker/ -run "TestGetChunker_tokenizer" -v
```

Expected: FAIL（tokenizer 未注册，GetChunker 报 Unknown chunker）。

- [x] **Step 3: 实现 registry 注册**

registry.go 的 `init()` 追加一行：

```go
func init() {
	// 对齐 Python register_chunker("char", CharChunker)
	_ = RegisterChunker("char", func(opts ...ChunkerOption) (Chunker, error) {
		return NewCharChunker(opts...)
	}, false)
	_ = RegisterChunker("hybrid", hybridFactory, false)
	// 主动增强：Python 不注册 TokenizerChunker（SDK 直接实例化），
	// Go 侧注册使 GetChunker("tokenizer") 可用（设计文档偏差 5）
	_ = RegisterChunker("tokenizer", tokenizerFactory, false)
}
```

非导出函数区块追加工厂：

```go
// tokenizerFactory "tokenizer" 注册项工厂（主动增强，Python 无此注册）。
//
// Extra 约定：tokenizer（Tokenizer 接口，缺省 nil→tiktoken fallback）、
// language（string，缺省 "auto"）、splitter_config（map[string]any，缺省 nil）。
func tokenizerFactory(opts ...ChunkerOption) (Chunker, error) {
	o := NewChunkerOptions(opts...)
	tokenizer := Tokenizer(nil)
	if v, ok := o.Extra["tokenizer"]; ok && v != nil {
		tk, ok := v.(Tokenizer)
		if !ok {
			return nil, fmt.Errorf("extra 'tokenizer' must implement Tokenizer interface")
		}
		tokenizer = tk
	}
	language := "auto"
	if v, ok := o.Extra["language"]; ok && v != nil {
		lang, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("extra 'language' must be string")
		}
		language = lang
	}
	var splitterConfig map[string]any
	if v, ok := o.Extra["splitter_config"]; ok && v != nil {
		sc, ok := v.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("extra 'splitter_config' must be map[string]any")
		}
		splitterConfig = sc
	}
	return NewTokenizerChunker(o.ChunkSize, o.ChunkOverlap, tokenizer, language, splitterConfig)
}
```

- [x] **Step 4: 运行测试通过**

```bash
go test ./internal/agentcore/retrieval/indexing/processor/chunker/ -v
```

Expected: 全 PASS。⚠️ 同时检查既有 registry_test.go 中 `GetChunker` 未知名测试的错误信息断言——注册名列表现在含 "char, hybrid, tokenizer"，如有硬编码断言需同步更新。

- [x] **Step 5: Commit**

```bash
git add internal/agentcore/retrieval/indexing/processor/chunker/
git commit -m "feat(13.7): registry 注册 tokenizer 工厂（主动增强，Extra 三键约定）"
```

---

### Task 9: 集成验证 + 覆盖率 + 计划表回填

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`（13.7/13.18/13.9 行）
- Modify: `internal/agentcore/retrieval/indexing/processor/splitter/doc.go`（如文件目录有变）

- [x] **Step 1: 全包编译 + 测试**

```bash
go build ./...
go test ./internal/agentcore/retrieval/... -v 2>&1 | tail -30
```

Expected: 编译零错误、retrieval 域全测试 PASS。

- [x] **Step 2: 覆盖率检查（≥85%）**

```bash
go test -cover ./internal/agentcore/retrieval/indexing/processor/splitter/ ./internal/agentcore/retrieval/indexing/processor/chunker/
```

Expected: 两包覆盖率 ≥85%。不足则补测试（重点：flush 边界、sentencesWithSpans 空白过滤、resolveTokenizers 分支）。

- [x] **Step 3: 计划表回填（遵守"回填不丢失"约定——13.1-13.6 行一字不动）**

13.7 行改为：

```markdown
| 13.7 | ✅ | TokenizerChunker | `TokenizerChunker(嵌入 BaseChunker 仅覆写 ChunkText：空文本→[]+Document 包装+Split 委托+完成日志 chunk_count) + IndexSentenceSplitter(splitterConfig 死参数归一化保留+resolveTokenizers 接口三态解析：DecodeTokenizer/Tokenizer/MaxLengthProvider 断言+nil→TiktokenTokenizer cl100k_base fallback+失败链 Warn+空格分词 proxy+resolveChunkSize：未设→maxLen/都设→min/默认200+overlap==0→chunkSize/5) + Tokenizer 接口三态(Tokenizer{Encode}/DecodeTokenizer{+Decode}/MaxLengthProvider{MaxLength}) + TiktokenTokenizer 适配器([]uint↔[]int 薄转发) + registry 注册 "tokenizer"(主动增强：Extra 三键 tokenizer/language/splitter_config)`。⚠️ 已带出 splitter 包全部（13.18 无剩余）。设计：`docs/superpowers/specs/2026-10-18-tokenizerchunker-13.7-design.md` | `openjiuwen/core/retrieval/indexing/processor/chunker/tokenizer_chunker.py + text_splitter.py(IndexSentenceSplitter)` |
```

13.18 行改为（保留 Splitter 分组行不动，仅改数据行）：

```markdown
| 13.18 | ✅ | Splitter | `全部由 13.7 前置带出（SentenceSplitter：detectChinese CJK 占比+标点启发+语义早退 / SplitDoc 分句+token 预算打包 / splitLongSegment 长句滑窗子切分 decode 失败跳过+整句 fallback / flush overlap 尾缀倒序累加 + BaseSplitter：构造校验 155100/155101+GetNodesFromDocuments+SplitText+GetTokenCount + sentencizer v0.3.0 替代 pysbd）`。本步骤无剩余内容 | `openjiuwen/core/retrieval/indexing/processor/splitter/splitter.py + base.py` |
```

13.9 行微调描述（仅 ⚠️ 部分）：

```markdown
| 13.9 | ☐ | 分块工具 | chunking 函数 + text_preprocessor + text_splitter。⚠️ 13.6 已带出 CharSplitter + TextSplitter 接口；13.7 已带出 IndexSentenceSplitter + splitter 包全部；本步骤仅剩 chunking.py TextChunker + text_preprocessor.py | `openjiuwen/core/retrieval/indexing/processor/chunker/` |
```

- [x] **Step 4: memory 更新提示**

提醒主会话更新 memory：`tokenizerchunker-13.7-design-decisions.md` 状态从"spec 待实施"改为"已实施完成 + 覆盖率数字 + commit 范围"。

- [x] **Step 5: Commit**

```bash
git add IMPLEMENTATION_PLAN.md internal/agentcore/retrieval/indexing/processor/splitter/doc.go
git commit -m "docs(13.7): 计划表回填（13.7/13.18 ✅、13.9 描述微调）"
```

---

## 任务依赖关系

```
Task 1 (依赖+骨架)
  → Task 2 (BaseSplitter)
    → Task 3 (detectChinese)
      → Task 4 (SentenceSplitter 主体)
        → Task 5 (splitLongSegment)
          → Task 6 (Tokenizer 三态，可与 Task 2-5 并行)
          → Task 7 (IndexSentenceSplitter + TokenizerChunker)（依赖 5+6）
            → Task 8 (registry)
              → Task 9 (集成验证+回填)
```

Task 6 与 Task 2-5 无依赖可并行；Task 7 是汇合点。
