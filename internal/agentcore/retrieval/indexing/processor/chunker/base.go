package chunker

import (
	"context"
	"strconv"

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

// ──────────────────────────── 非导出函数 ────────────────────────────
