// GraphKnowledgeBase 图增强知识库实现：写侧双索引（chunk + triple），
// 读侧图检索（GraphRetriever）。文件职责见 knowledgebase/doc.go。
package knowledgebase

import (
	"context"
	"encoding/json"
	"strings"
	"sync"

	"github.com/google/uuid"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/reranker"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/indexing/indexer"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/retriever"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// GraphKnowledgeBase 图增强知识库（13.28）。
//
// 在 SimpleKnowledgeBase 的双索引能力之上加图检索：写侧 use_graph 时抽取
// 三元组建 kb_{id}_triples 索引，读侧懒创建 GraphRetriever 走 beam search
// 图检索。对外仍是 KnowledgeBase 接口，上层按 use_graph 二选一。
//
// Python: openjiuwen/core/retrieval/graph_knowledge_base.py (GraphKnowledgeBase)
type GraphKnowledgeBase struct {
	// BaseKnowledgeBase 共享基座（值嵌入，13.27 先例）
	BaseKnowledgeBase
	// chunkRetriever 固定 chunk 检索器（nil = 由 GraphRetriever 动态创建）
	chunkRetriever retriever.Retriever
	// tripleRetriever 固定三元组检索器（nil = 动态创建）
	tripleRetriever retriever.Retriever
	// graphRetriever 图检索器（nil = 待懒创建）
	graphRetriever retriever.Retriever
	// graphMu 懒创建并发保护；读路径同样持锁（13.27 §13-1 先例）
	graphMu sync.Mutex
}

// （编译期断言 `var _ KnowledgeBase = (*GraphKnowledgeBase)(nil)` 在五抽象
// 方法齐全后加入；当前仅 AddDocuments，提前断言会编译失败。）

// ──────────────────────────── 导出函数 ────────────────────────────

// NewGraphKnowledgeBase 构造图增强知识库。
//
// chunk/triple 检索器为显式参数（Python 子类特有字段的直译），其余组件经
// KnowledgeBaseOption 透传基座，构造期配置一致性校验随之继承。
//
// Python: GraphKnowledgeBase.__init__
func NewGraphKnowledgeBase(config common.KnowledgeBaseConfig,
	chunkRetriever, tripleRetriever retriever.Retriever,
	opts ...KnowledgeBaseOption) (*GraphKnowledgeBase, error) {
	base, err := NewBaseKnowledgeBase(config, opts...)
	if err != nil {
		return nil, err
	}
	return &GraphKnowledgeBase{
		BaseKnowledgeBase: *base,
		chunkRetriever:    chunkRetriever,
		tripleRetriever:   tripleRetriever,
	}, nil
}

// SetGraphRetriever 注入图检索器（Python kb.graph_retriever = r 的显式对应物）。
//
// 传 nil 表示复位：下次 Retrieve 重新懒创建（Python 赋 None 同义）。
func (g *GraphKnowledgeBase) SetGraphRetriever(r retriever.Retriever) {
	g.graphMu.Lock()
	defer g.graphMu.Unlock()
	g.graphRetriever = r
}

// AddDocuments 添加文档（写 chunk 索引；use_graph 时另写 triple 索引）。
//
// Python: graph_knowledge_base.py add_documents
func (g *GraphKnowledgeBase) AddDocuments(ctx context.Context, documents []reranker.Document,
	opts ...KBOption) ([]string, error) {
	_ = opts
	if g.chunker == nil {
		return nil, exception.BuildError(
			exception.StatusRetrievalKbChunkerNotFound,
			exception.WithMsg("chunker is required for add_documents"))
	}
	if g.indexManager == nil {
		return nil, exception.BuildError(
			exception.StatusRetrievalKbIndexManagerNotFound,
			exception.WithMsg("index_manager is required for add_documents"))
	}
	if g.strictValidation && g.vectorStore != nil {
		if err := g.vectorStore.CheckVectorField(); err != nil {
			return nil, err
		}
	}

	for i := range documents {
		if strings.TrimSpace(documents[i].ID) == "" {
			documents[i].ID = uuid.NewString()
		}
	}

	chunks, err := g.chunker.ChunkDocuments(ctx, documents)
	if err != nil {
		return nil, err
	}
	logger.Info(logComponent).
		Int("document_count", len(documents)).
		Int("chunk_count", len(chunks)).
		Msg("Chunked documents into chunks")

	databaseName := vectorStoreDatabaseName(g.vectorStore)

	chunkConfig := common.IndexConfig{
		IndexName:           "kb_" + g.config.KBID + "_chunks",
		IndexType:           g.config.IndexType,
		UseCaptionForImages: g.config.UseCaptionForImages,
	}
	success, err := g.indexManager.BuildIndex(ctx, chunks, chunkConfig, g.embedModel,
		indexer.WithIndexExtra("database_name", databaseName))
	if err != nil {
		return nil, err
	}
	if !success {
		return nil, exception.BuildError(
			exception.StatusRetrievalKbChunkIndexBuildExecutionError,
			exception.WithMsg("Failed to build chunk index"))
	}

	if g.config.UseGraph && g.extractor != nil {
		logger.Info(logComponent).Msg("Extracting triples for graph index...")
		triples, err := g.extractor.Extract(ctx, chunks)
		if err != nil {
			return nil, err
		}
		if len(triples) > 0 {
			logger.Info(logComponent).Int("triple_count", len(triples)).Msg("Extracted triples")

			// 注意：use_caption_for_images 硬编码 true（与 chunk 索引不对称，
			// Python 真实行为：三元组是纯文本，恒走 text-only 嵌入路径）
			tripleConfig := common.IndexConfig{
				IndexName:           "kb_" + g.config.KBID + "_triples",
				IndexType:           g.config.IndexType,
				UseCaptionForImages: true,
			}
			tripleChunks := triplesToTextChunks(triples)
			success, err := g.indexManager.BuildIndex(ctx, tripleChunks, tripleConfig, g.embedModel,
				indexer.WithIndexExtra("database_name", databaseName))
			if err != nil {
				return nil, err
			}
			if !success {
				return nil, exception.BuildError(
					exception.StatusRetrievalKbTripleIndexBuildExecutionError,
					exception.WithMsg("Failed to build triple index"))
			}
			logger.Info(logComponent).Int("triple_chunk_count", len(tripleChunks)).
				Msg("Built triple index with triples")
		}
	}

	docIDs := make([]string, len(documents))
	for i := range documents {
		docIDs[i] = documents[i].ID
	}
	logger.Info(logComponent).Int("document_count", len(docIDs)).
		Msg("Successfully added documents to knowledge base")
	return docIDs, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// triplesToTextChunks 三元组转 TextChunk（图索引的写入形态）。
//
// metadata 先展开 triple.metadata 再覆写 triple / chunk_index / chunk_id：
// 后两键是 13.23 graph_fetch 读侧回链原始分块（metadata["doc_id"] /
// metadata["chunk_id"]）的契约，缺一个键图扩展就断链。
// triple 用 json.Marshal（紧凑）而非 Python json.dumps 的 ", " 形态，
// 语义等价、字节不同（spec §8-2）。
//
// Python: add_documents L144-158
func triplesToTextChunks(triples []common.Triple) []common.TextChunk {
	chunks := make([]common.TextChunk, 0, len(triples))
	for i, t := range triples {
		meta := make(map[string]any, len(t.Metadata)+3)
		for k, v := range t.Metadata {
			meta[k] = v
		}
		tripleJSON, _ := json.Marshal([]string{t.Subject, t.Predicate, t.Object})
		meta["triple"] = string(tripleJSON)
		meta["chunk_index"] = i
		meta["chunk_id"] = metaString(t.Metadata, "chunk_id")

		chunks = append(chunks, common.TextChunk{
			ID:       uuid.NewString(),
			Text:     t.Subject + " " + t.Predicate + " " + t.Object,
			DocID:    metaString(t.Metadata, "doc_id"),
			Metadata: meta,
		})
	}
	return chunks
}

// metaString 从元数据取字符串值，缺失或非 string 收窄空串。
//
// Python 侧 pydantic 对非 str 会抛 ValidationError；Go 收窄（偏差 §8-3，
// 13.25 §8-7 先例）。
func metaString(meta map[string]any, key string) string {
	if meta == nil {
		return ""
	}
	if s, ok := meta[key].(string); ok {
		return s
	}
	return ""
}
