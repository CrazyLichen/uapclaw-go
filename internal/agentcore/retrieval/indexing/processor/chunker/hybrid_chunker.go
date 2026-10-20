package chunker

import (
	"context"
	"fmt"
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
// 完全覆写（不走基类默认实现），Process 为局部覆写（设计决策 3）。
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
// DimsProvider（嵌入 BaseChunker 的实现）时取其真实尺寸；纯接口实现
// 断言失败则回退 512/50。随后过 NewBaseChunker 校验（inner 尺寸
// 已合法时恒通过，保留为构造结构对称）。
//
// noSplitWhen 可变参数为空时为 nil → defaultNoSplit（对齐 Python
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

// Process 局部覆写（设计决策 3）：转发到覆写版 ChunkDocuments。
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
