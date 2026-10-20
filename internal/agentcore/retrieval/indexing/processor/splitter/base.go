package splitter

import (
	"fmt"
	"strconv"

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
//   - chunkSize <= 0 时返回 155100 RETRIEVAL_INDEXING_CHUNK_SIZE_INVALID
//   - chunkOverlap < 0 或 >= chunkSize 时返回 155101 RETRIEVAL_INDEXING_CHUNK_OVERLAP_INVALID
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
// 空文档（Text 为空）记 Warn 跳过；每个 span 经 NewTextChunkFromDocument
// 生成 TextChunk；尾部汇总日志。
//
// Python: splitter/base.py Splitter.get_nodes_from_documents
func (b *BaseSplitter) GetNodesFromDocuments(docs []reranker.Document) ([]common.TextChunk, error) {
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
