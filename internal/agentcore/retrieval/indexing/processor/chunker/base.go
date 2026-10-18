package chunker

import (
	"context"
	"fmt"
	"strconv"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/reranker"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 结构体 ────────────────────────────

// Chunker 文本分块抽象接口。
//
// 三方法对齐 Python Chunker：chunk_text（子类必覆写）、
// chunk_documents（基类默认实现，子类可选覆写）、process（Processor 契约入口）。
//
// 知识库入库时调用 ChunkDocuments；ChunkText 是子类唯一必须实现的分块算法。
//
// Python: openjiuwen/core/retrieval/indexing/processor/chunker/base.py (Chunker)
type Chunker interface {
	// ChunkText 对单段文本分块，返回分块文本列表。空文本返回空切片。
	ChunkText(text string) ([]string, error)
	// ChunkDocuments 对文档列表分块，产出带溯源元数据的 TextChunk。
	ChunkDocuments(ctx context.Context, docs []reranker.Document) ([]common.TextChunk, error)
	// Process 处理文档（Processor 契约入口），等价于 ChunkDocuments。
	Process(ctx context.Context, docs []reranker.Document) ([]common.TextChunk, error)
}

// BaseChunker 分块器默认适配器。
//
// 对齐 Python Chunker 基类的可复用部分：chunk_size/chunk_overlap 构造校验与
// length_function。具体分块器嵌入本结构体，仅覆写 ChunkText，自动获得
// ChunkDocuments/Process 默认实现。
//
// Python: openjiuwen/core/retrieval/indexing/processor/chunker/base.py (Chunker)
type BaseChunker struct {
	// ChunkSize 分块大小，必须大于 0
	ChunkSize int
	// ChunkOverlap 相邻块重叠，取值 [0, ChunkSize)
	ChunkOverlap int
	// lengthFunction 长度计算函数，nil 时默认按字符计数（utf8.RuneCountInString）
	lengthFunction func(string) int
	// self 完整 Chunker 引用（子类构造时经 SetSelf 注入），
	// 使默认 ChunkDocuments 能路由到子类覆写的 ChunkText
	self Chunker
}

// ──────────────────────────── 枚举 ────────────────────────────
// ──────────────────────────── 常量 ────────────────────────────
// ──────────────────────────── 全局变量 ────────────────────────────
// ──────────────────────────── 导出函数 ────────────────────────────

// NewBaseChunker 构造 BaseChunker 并执行参数校验。
//
// 校验规则（一比一对齐 Python Chunker.__init__）：
//   - chunkSize <= 0 → 155100 RETRIEVAL_INDEXING_CHUNK_SIZE_INVALID
//   - chunkOverlap < 0 → 155101 RETRIEVAL_INDEXING_CHUNK_OVERLAP_INVALID
//   - chunkOverlap >= chunkSize → 155101 RETRIEVAL_INDEXING_CHUNK_OVERLAP_INVALID
//
// lengthFn 可选，默认按字符计数。
//
// Python: base.py Chunker.__init__
func NewBaseChunker(chunkSize, chunkOverlap int, lengthFn ...func(string) int) (*BaseChunker, error) {
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
			exception.WithParam("error_msg", "chunk_overlap must be less than chunk_size"),
		)
	}
	bc := &BaseChunker{
		ChunkSize:    chunkSize,
		ChunkOverlap: chunkOverlap,
	}
	if len(lengthFn) > 0 && lengthFn[0] != nil {
		bc.lengthFunction = lengthFn[0]
	}
	return bc, nil
}

// ChunkDocuments 分块文档列表（默认实现，可被嵌入方继承）。
//
// 对每个文档调用子类覆写的 ChunkText，为每块生成 uuid 并合并溯源元数据：
// {**doc.Metadata, "chunk_index": i, "total_chunks": len(texts), "chunk_id": uid}。
// 任何文档分块失败即中断返回。
//
// Python: base.py Chunker.chunk_documents
func (b *BaseChunker) ChunkDocuments(ctx context.Context, docs []reranker.Document) ([]common.TextChunk, error) {
	_ = ctx // 对齐 Python 同步签名，当前无 ctx 消费
	chunks := make([]common.TextChunk, 0)
	for _, doc := range docs {
		texts, err := b.chunkTextOf(doc.Text)
		if err != nil {
			return nil, err
		}
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
				ID:       uid,
				Text:     text,
				DocID:    doc.ID,
				Metadata: meta,
			})
		}
	}
	return chunks, nil
}

// Process 处理文档（Processor 契约入口），等价于 ChunkDocuments 的 ctx 透传包装。
//
// Python: base.py Chunker.process
func (b *BaseChunker) Process(ctx context.Context, docs []reranker.Document) ([]common.TextChunk, error) {
	return b.ChunkDocuments(ctx, docs)
}

// SetSelf 注入完整 Chunker 引用（子类构造函数调用），
// 使默认 ChunkDocuments 路由到子类覆写的 ChunkText。
func (b *BaseChunker) SetSelf(c Chunker) {
	b.self = c
}

// Length 计算文本长度。lengthFunction 未设置时按字符计数（utf8.RuneCountInString，
// 对齐 Python len 对 str 的语义）。
func (b *BaseChunker) Length(text string) int {
	if b.lengthFunction != nil {
		return b.lengthFunction(text)
	}
	return utf8.RuneCountInString(text)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// chunkTextOf 路由到子类覆写的 ChunkText；未注入 self 时返回错误
//（BaseChunker 单独不构成完整 Chunker，无法猜测分块算法）。
// Python 基类 chunk_text 返回 None（无人消费），Go 侧该防御路径返回显式错误。
func (b *BaseChunker) chunkTextOf(text string) ([]string, error) {
	if b.self != nil {
		return b.self.ChunkText(text)
	}
	return nil, fmt.Errorf("BaseChunker used standalone: call SetSelf before ChunkDocuments")
}
