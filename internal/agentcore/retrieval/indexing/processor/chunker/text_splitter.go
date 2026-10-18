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

var (
	// 编译期断言 CharSplitter 实现 TextSplitter
	_ TextSplitter = (*CharSplitter)(nil)
	// 编译期引用 defaultCharChunkOverlap：Go 无 None 语义该默认值不参与运行时
	// 构造（overlap 0 为合法显式值），保留常量作为 Python 对齐锚点供文档引用
	_ = defaultCharChunkOverlap
)

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
