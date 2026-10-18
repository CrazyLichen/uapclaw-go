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
