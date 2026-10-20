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
	// BaseChunker 默认适配器（ChunkSize/ChunkOverlap + 默认 ChunkDocuments/Process）
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
// 构造链（对齐 Python __init__ 的 super().__init__ + splitter 构造）：
// NewBaseChunker（155100/155101 校验）→ NewIndexSentenceSplitter
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
// 对齐 Python：空文本→[]；Document(text, metadata={}) 包装 →
// splitter.split → 收集 node.text；完成日志 chunk_count。
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
