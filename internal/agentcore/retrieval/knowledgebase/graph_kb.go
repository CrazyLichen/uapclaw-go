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

// 编译期断言：GraphKnowledgeBase 完整实现 KnowledgeBase 接口。
var _ KnowledgeBase = (*GraphKnowledgeBase)(nil)

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

// Retrieve 检索相关文档（use_graph 时走图检索，否则回落 SimpleKB）。
//
// use_graph 三层判定：retrieval_config.use_graph 为 true → 图；为 nil 时随
// config.use_graph；为 false 显式压过配置 → 回落。
//
// Python: graph_knowledge_base.py retrieve
func (g *GraphKnowledgeBase) Retrieve(ctx context.Context, query string,
	config *common.RetrievalConfig, opts ...KBOption) ([]common.RetrievalResult, error) {
	retrievalConfig := config
	if retrievalConfig == nil {
		retrievalConfig = defaultRetrievalConfig()
	}

	useGraph := (retrievalConfig.UseGraph != nil && *retrievalConfig.UseGraph) ||
		(retrievalConfig.UseGraph == nil && g.config.UseGraph)
	if !useGraph {
		return g.retrieveViaSimpleKB(ctx, query, config, opts...)
	}

	gret, err := g.ensureGraphRetriever()
	if err != nil {
		return nil, err
	}
	mode := resolveRetrievalMode(g.config.IndexType)

	r := gret
	if retrievalConfig.Agentic {
		r, err = wrapAgenticRetriever(g.llmClient, gret, opts...)
		if err != nil {
			return nil, err
		}
	}

	return r.Retrieve(ctx, query, retrievalConfig.TopK, retrievalConfig.ScoreThreshold, mode,
		retriever.WithRetrieveFilters(retrievalConfig.Filters),
		retriever.WithRetrieveGraphExpansion(retrievalConfig.GraphExpansion))
}

// DeleteDocuments 删除文档（chunk 索引必删；use_graph 时另删 triple 索引）。
//
// triple 删除返回值 false 只 Warn 不影响总结果（Python 真实行为）；
// DeleteIndex 返回 error 仍冒泡（Python 无 try/except）。
//
// Python: graph_knowledge_base.py delete_documents
func (g *GraphKnowledgeBase) DeleteDocuments(ctx context.Context, docIDs []string,
	opts ...KBOption) (bool, error) {
	_ = opts
	if g.indexManager == nil {
		return false, exception.BuildError(
			exception.StatusRetrievalKbIndexManagerNotFound,
			exception.WithMsg("index_manager is required for delete_documents"))
	}
	if g.strictValidation && g.vectorStore != nil {
		if err := g.vectorStore.CheckVectorField(); err != nil {
			return false, err
		}
	}

	chunkIndexName := "kb_" + g.config.KBID + "_chunks"
	tripleIndexName := "kb_" + g.config.KBID + "_triples"

	success := true
	for _, docID := range docIDs {
		result, err := g.indexManager.DeleteIndex(ctx, docID, chunkIndexName)
		if err != nil {
			return false, err
		}
		if !result {
			success = false
		}
	}

	if g.config.UseGraph {
		for _, docID := range docIDs {
			result, err := g.indexManager.DeleteIndex(ctx, docID, tripleIndexName)
			if err != nil {
				return false, err
			}
			if !result {
				// triple 删除失败不影响总结果（Python 仅 warning）
				logger.Warn(logComponent).Str("doc_id", docID).Msg("Failed to delete triples")
			}
		}
	}
	return success, nil
}

// UpdateDocuments 更新文档（先删后加）。
//
// Python: graph_knowledge_base.py update_documents
func (g *GraphKnowledgeBase) UpdateDocuments(ctx context.Context, documents []reranker.Document,
	opts ...KBOption) ([]string, error) {
	if g.strictValidation && g.vectorStore != nil {
		if err := g.vectorStore.CheckVectorField(); err != nil {
			return nil, err
		}
	}
	docIDs := make([]string, len(documents))
	for i := range documents {
		docIDs[i] = documents[i].ID
	}
	if _, err := g.DeleteDocuments(ctx, docIDs); err != nil {
		return nil, err
	}
	return g.AddDocuments(ctx, documents, opts...)
}

// GetStatistics 获取知识库统计信息（11 键，triple_index_info 恒存在）。
//
// Python: graph_knowledge_base.py get_statistics
func (g *GraphKnowledgeBase) GetStatistics(ctx context.Context) (map[string]any, error) {
	chunkIndexName := "kb_" + g.config.KBID + "_chunks"
	tripleIndexName := "kb_" + g.config.KBID + "_triples"

	if g.indexManager == nil {
		return map[string]any{
			"kb_id":        g.config.KBID,
			"index_exists": false,
		}, nil
	}

	chunkInfo, err := g.indexManager.GetIndexInfo(ctx, chunkIndexName)
	if err != nil {
		return nil, err
	}
	var tripleInfo map[string]any
	if g.config.UseGraph {
		tripleInfo, err = g.indexManager.GetIndexInfo(ctx, tripleIndexName)
		if err != nil {
			return nil, err
		}
	}

	return map[string]any{
		"kb_id":               g.config.KBID,
		"index_type":          string(g.config.IndexType),
		"use_graph":           g.config.UseGraph,
		"chunk_index_info":    chunkInfo,
		"triple_index_info":   tripleInfo,
		"has_parser":          g.parser != nil,
		"has_chunker":         g.chunker != nil,
		"has_extractor":       g.extractor != nil,
		"has_embed_model":     g.embedModel != nil,
		"has_vector_store":    g.vectorStore != nil,
		"has_graph_retriever": g.hasGraphRetriever(),
	}, nil
}

// Close 关闭知识库（基座组件 + 三个检索器，顺序字面）。
//
// 注意：GraphRetriever.Close 内部已关闭固定 chunk/triple 检索器，这里再关
// 一遍是 Python 真实行为（重复关闭照复刻，spec §6.1）；主链路两字段恒 nil，
// 仅显式注入固定检索器时才会重复关闭底层 store。
//
// Python: graph_knowledge_base.py close
func (g *GraphKnowledgeBase) Close() error {
	_ = g.BaseKnowledgeBase.Close()

	g.graphMu.Lock()
	graph, chunk, triple := g.graphRetriever, g.chunkRetriever, g.tripleRetriever
	g.graphMu.Unlock()

	g.maybeClose(graph)
	g.maybeClose(chunk)
	g.maybeClose(triple)
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// hasGraphRetriever 图检索器是否已创建（持锁读，避免与懒创建竞态）。
func (g *GraphKnowledgeBase) hasGraphRetriever() bool {
	g.graphMu.Lock()
	defer g.graphMu.Unlock()
	return g.graphRetriever != nil
}

// ensureGraphRetriever 取图检索器，nil 时持锁懒创建。
//
// 六个 WithGraph* 选项与 SetIndexType 注入此前零生产调用，13.28 首次接线。
// 读路径同样持锁（Go 内存模型下锁外读属数据竞争，13.27 §13-1 先例）。
func (g *GraphKnowledgeBase) ensureGraphRetriever() (retriever.Retriever, error) {
	g.graphMu.Lock()
	defer g.graphMu.Unlock()
	if g.graphRetriever != nil {
		return g.graphRetriever, nil
	}
	if g.vectorStore == nil {
		return nil, exception.BuildError(
			exception.StatusRetrievalKbVectorStoreNotFound,
			exception.WithMsg("vector_store is required for graph retrieval"))
	}

	gret := retriever.NewGraphRetriever(
		retriever.WithGraphChunkRetriever(g.chunkRetriever),
		retriever.WithGraphTripleRetriever(g.tripleRetriever),
		retriever.WithGraphVectorStore(g.vectorStore),
		retriever.WithGraphEmbedModel(g.embedModel),
		retriever.WithGraphChunkCollection("kb_"+g.config.KBID+"_chunks"),
		retriever.WithGraphTripleCollection("kb_"+g.config.KBID+"_triples"),
	)
	// 注入 index_type：GraphRetriever 的 mode 校验与 AgenticRetriever 的
	// defaultMode 推导都读它。
	if carrier, ok := any(gret).(retriever.IndexTypeCarrier); ok {
		carrier.SetIndexType(string(g.config.IndexType))
	}
	g.graphRetriever = gret
	return gret, nil
}

// retrieveViaSimpleKB 回落路径：现构造 SimpleKB 委托（Python 每次新建、不缓存）。
//
// 两处保真：不传 extractor（GraphKB 独有字段）；不传 strict_validation
// （Python 不传 → SimpleKB 恒为 true）。传原始 config（可能为 nil），由
// SimpleKB 自己做 nil→默认回落。
func (g *GraphKnowledgeBase) retrieveViaSimpleKB(ctx context.Context, query string,
	config *common.RetrievalConfig, opts ...KBOption) ([]common.RetrievalResult, error) {
	base, err := NewSimpleKnowledgeBase(g.config, nil,
		WithVectorStore(g.vectorStore),
		WithEmbedModel(g.embedModel),
		WithParser(g.parser),
		WithChunker(g.chunker),
		WithIndexManager(g.indexManager),
		WithLLMClient(g.llmClient),
	)
	if err != nil {
		return nil, err
	}
	return base.Retrieve(ctx, query, config, opts...)
}

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
