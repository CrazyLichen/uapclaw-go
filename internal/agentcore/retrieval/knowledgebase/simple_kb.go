package knowledgebase

import (
	"context"
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

// SimpleKnowledgeBase 简单知识库（13.27）。
//
// 实现 KnowledgeBase 的 5 个抽象方法；4 个模板方法（ParseFiles/ParseURLs/
// DeleteCollection/Close）与配置一致性防线全部继承 BaseKnowledgeBase。
// retriever 为 nil 时首次 Retrieve 按 index_type 懒创建（并发安全）。
//
// Python: openjiuwen/core/retrieval/simple_knowledge_base.py (SimpleKnowledgeBase)
type SimpleKnowledgeBase struct {
	// BaseKnowledgeBase 共享基座（13.26；值嵌入，13.8 HybridChunker 先例）
	BaseKnowledgeBase
	// retriever 检索器（nil = 待懒创建；构造期可由调用方显式注入）
	retriever retriever.Retriever
	// retrieverMu 懒创建并发保护（同一 KB 并发 Retrieve 只建一次）
	retrieverMu sync.Mutex
}

// 编译期断言：SimpleKnowledgeBase 完整实现 KnowledgeBase 接口
// （兑现 13.26 base.go 预留的断言缺口）。
var _ KnowledgeBase = (*SimpleKnowledgeBase)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSimpleKnowledgeBase 构造简单知识库。
//
// r 为显式参数（Python 签名不对称的直译：基类不收 retriever，子类特有）；
// 其余组件经 KnowledgeBaseOption 透传基座，构造期的配置一致性校验
// （postAssignValidation）随之继承。
//
// Python: SimpleKnowledgeBase.__init__
func NewSimpleKnowledgeBase(config common.KnowledgeBaseConfig,
	r retriever.Retriever, opts ...KnowledgeBaseOption) (*SimpleKnowledgeBase, error) {
	base, err := NewBaseKnowledgeBase(config, opts...)
	if err != nil {
		return nil, err
	}
	return &SimpleKnowledgeBase{BaseKnowledgeBase: *base, retriever: r}, nil
}

// AddDocuments 添加文档到知识库，返回文档 ID 列表。
//
// 空白 id（含纯空格）才补 uuid → 分块 → 建索引。IndexConfig **带**
// UseCaptionForImages（与 UpdateDocuments 不对称，Python 真实行为）。
// database_name 经 WithIndexExtra 传入（对齐 Python build_index 关键字参数）。
//
// Python: add_documents
func (s *SimpleKnowledgeBase) AddDocuments(ctx context.Context, documents []reranker.Document, opts ...KBOption) ([]string, error) {
	_ = opts
	if s.chunker == nil {
		return nil, exception.BuildError(
			exception.StatusRetrievalKbChunkerNotFound,
			exception.WithMsg("chunker is required for add_documents"),
		)
	}
	if s.indexManager == nil {
		return nil, exception.BuildError(
			exception.StatusRetrievalKbIndexManagerNotFound,
			exception.WithMsg("index_manager is required for add_documents"),
		)
	}
	if s.strictValidation && s.vectorStore != nil {
		if err := s.vectorStore.CheckVectorField(); err != nil {
			return nil, err
		}
	}

	for i := range documents {
		if strings.TrimSpace(documents[i].ID) == "" {
			documents[i].ID = uuid.NewString()
		}
	}

	chunks, err := s.chunker.ChunkDocuments(ctx, documents)
	if err != nil {
		return nil, err
	}
	logger.Info(logComponent).
		Int("document_count", len(documents)).
		Int("chunk_count", len(chunks)).
		Msg("Chunked documents into chunks")

	indexConfig := common.IndexConfig{
		IndexName:           "kb_" + s.config.KBID + "_chunks",
		IndexType:           s.config.IndexType,
		UseCaptionForImages: s.config.UseCaptionForImages,
	}
	success, err := s.indexManager.BuildIndex(ctx, chunks, indexConfig, s.embedModel,
		indexer.WithIndexExtra("database_name", s.vectorStoreDatabaseName()))
	if err != nil {
		return nil, err
	}
	if !success {
		return nil, exception.BuildError(
			exception.StatusRetrievalKbIndexBuildExecutionError,
			exception.WithMsg("Failed to build index"),
		)
	}

	docIDs := make([]string, len(documents))
	for i := range documents {
		docIDs[i] = documents[i].ID
	}
	logger.Info(logComponent).Int("document_count", len(docIDs)).
		Msg("Successfully added documents to knowledge base")
	return docIDs, nil
}

// Retrieve 检索相关文档。
//
// retriever 为 nil 时按 index_type 懒创建（vector→VectorRetriever /
// bm25→SparseRetriever / 其他→HybridRetriever），加锁双检保证只建一次；
// config.Agentic 为真时外包 AgenticRetriever。filters 写入 RetrieveOptions
// 但四个 retriever 实现均不消费（Python 死参数字面复刻）。
//
// Python: retrieve
func (s *SimpleKnowledgeBase) Retrieve(ctx context.Context, query string, config *common.RetrievalConfig, opts ...KBOption) ([]common.RetrievalResult, error) {
	r, err := s.ensureRetriever()
	if err != nil {
		return nil, err
	}

	// 对齐 Python `config or RetrievalConfig()`：nil 回落默认 top_k=5
	retrievalConfig := config
	if retrievalConfig == nil {
		retrievalConfig = defaultRetrievalConfig()
	}

	mode := resolveRetrievalMode(s.config.IndexType)

	// 对齐 Python retrieval_config.agentic 分支
	if retrievalConfig.Agentic {
		r, err = s.wrapAgentic(ctx, r, opts...)
		if err != nil {
			return nil, err
		}
	}

	return r.Retrieve(ctx, query, retrievalConfig.TopK, retrievalConfig.ScoreThreshold, mode,
		retriever.WithRetrieveFilters(retrievalConfig.Filters))
}

// DeleteDocuments 删除文档。
//
// 逐 doc_id 删除，循环不中断；任一返回 false → 总结果为 false。
//
// Python: delete_documents
func (s *SimpleKnowledgeBase) DeleteDocuments(ctx context.Context, docIDs []string, opts ...KBOption) (bool, error) {
	_ = opts
	if s.indexManager == nil {
		return false, exception.BuildError(
			exception.StatusRetrievalKbIndexManagerNotFound,
			exception.WithMsg("index_manager is required for delete_documents"),
		)
	}
	if s.strictValidation && s.vectorStore != nil {
		if err := s.vectorStore.CheckVectorField(); err != nil {
			return false, err
		}
	}

	indexName := "kb_" + s.config.KBID + "_chunks"
	success := true
	for _, docID := range docIDs {
		result, err := s.indexManager.DeleteIndex(ctx, docID, indexName)
		if err != nil {
			return false, err
		}
		if !result {
			success = false
		}
	}
	return success, nil
}

// UpdateDocuments 更新文档，返回更新成功的文档 ID 列表。
//
// 与 AddDocuments 不对称（Python 真实行为）：不做 uuid 填充；
// IndexConfig **不带** UseCaptionForImages。按 DocID 过滤 chunks 后
// 逐文档 UpdateIndex，成功才收集 doc_id。
//
// Python: update_documents
func (s *SimpleKnowledgeBase) UpdateDocuments(ctx context.Context, documents []reranker.Document, opts ...KBOption) ([]string, error) {
	_ = opts
	if s.chunker == nil {
		return nil, exception.BuildError(
			exception.StatusRetrievalKbChunkerNotFound,
			exception.WithMsg("chunker is required for update_documents"),
		)
	}
	if s.indexManager == nil {
		return nil, exception.BuildError(
			exception.StatusRetrievalKbIndexManagerNotFound,
			exception.WithMsg("index_manager is required for update_documents"),
		)
	}
	if s.strictValidation && s.vectorStore != nil {
		if err := s.vectorStore.CheckVectorField(); err != nil {
			return nil, err
		}
	}

	chunks, err := s.chunker.ChunkDocuments(ctx, documents)
	if err != nil {
		return nil, err
	}

	// 注意：不带 UseCaptionForImages（与 add_documents 的不对称是 Python 真实行为）
	indexConfig := common.IndexConfig{
		IndexName: "kb_" + s.config.KBID + "_chunks",
		IndexType: s.config.IndexType,
	}

	var docIDs []string
	for i := range documents {
		var docChunks []common.TextChunk
		for _, c := range chunks {
			if c.DocID == documents[i].ID {
				docChunks = append(docChunks, c)
			}
		}
		if len(docChunks) == 0 {
			continue
		}
		success, err := s.indexManager.UpdateIndex(ctx, docChunks, documents[i].ID, indexConfig, s.embedModel)
		if err != nil {
			return nil, err
		}
		if success {
			docIDs = append(docIDs, documents[i].ID)
		}
	}
	return docIDs, nil
}

// GetStatistics 获取知识库统计信息。
//
// index_manager 缺失时返回 2 键短路（kb_id + index_exists=false）；
// 存在时 GetIndexInfo 的错误直接冒泡（Python 无 try 包裹）。
//
// Python: get_statistics
func (s *SimpleKnowledgeBase) GetStatistics(ctx context.Context) (map[string]any, error) {
	indexName := "kb_" + s.config.KBID + "_chunks"

	if s.indexManager == nil {
		return map[string]any{
			"kb_id":        s.config.KBID,
			"index_exists": false,
		}, nil
	}

	indexInfo, err := s.indexManager.GetIndexInfo(ctx, indexName)
	if err != nil {
		return nil, err
	}

	return map[string]any{
		"kb_id":            s.config.KBID,
		"index_type":       string(s.config.IndexType),
		"index_info":       indexInfo,
		"has_parser":       s.parser != nil,
		"has_chunker":      s.chunker != nil,
		"has_extractor":    s.extractor != nil,
		"has_embed_model":  s.embedModel != nil,
		"has_vector_store": s.vectorStore != nil,
	}, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// defaultRetrievalConfig 默认检索配置（对齐 Python RetrievalConfig() 默认
// top_k=5，其余字段为零值/ nil）。
func defaultRetrievalConfig() *common.RetrievalConfig {
	return &common.RetrievalConfig{TopK: 5}
}

// resolveRetrievalMode 由 index_type 推导检索模式。
//
// Python: mode = "hybrid"; index_type=="vector"→"vector"；
// index_type=="bm25"→"sparse"；其余保持 "hybrid"。
func resolveRetrievalMode(indexType common.IndexTypeKind) retriever.RetrievalMode {
	switch indexType {
	case common.IndexTypeVector:
		return retriever.RetrievalModeVector
	case common.IndexTypeBM25:
		return retriever.RetrievalModeSparse
	default:
		return retriever.RetrievalModeHybrid
	}
}

// ensureRetriever 返回可用 retriever，nil 时加锁双检懒创建。
//
// Python 侧在 retrieve 内按 index_type 三分支延迟 import 并构造；Go 为
// 包级 import，分支逻辑保持一致。
func (s *SimpleKnowledgeBase) ensureRetriever() (retriever.Retriever, error) {
	if s.retriever != nil {
		return s.retriever, nil
	}

	s.retrieverMu.Lock()
	defer s.retrieverMu.Unlock()
	if s.retriever != nil {
		return s.retriever, nil
	}

	if s.vectorStore == nil {
		return nil, exception.BuildError(
			exception.StatusRetrievalKbVectorStoreNotFound,
			exception.WithMsg("vector_store or retriever is required for retrieve"),
		)
	}

	// 对齐 Python：vector→VectorRetriever / bm25→SparseRetriever / 其他→HybridRetriever
	switch s.config.IndexType {
	case common.IndexTypeVector:
		s.retriever = retriever.NewVectorRetriever(s.vectorStore, s.embedModel)
	case common.IndexTypeBM25:
		s.retriever = retriever.NewSparseRetriever(s.vectorStore)
	default:
		s.retriever = retriever.NewHybridRetriever(s.vectorStore, s.embedModel)
	}
	return s.retriever, nil
}

// wrapAgentic 用 AgenticRetriever 包装底层 retriever。
//
// 对齐 Python `AgenticRetriever(retriever=..., llm_client=..., **kwargs)`：
// kwargs 只认 max_iter，经 KBOptions.Extra["max_iter"] 映射；其余键在
// Python 会 TypeError，Go 静默忽略（行为分叉已归档 spec §8-6）。
func (s *SimpleKnowledgeBase) wrapAgentic(ctx context.Context, r retriever.Retriever, opts ...KBOption) (retriever.Retriever, error) {
	_ = ctx
	kbOpts := NewKBOptions(opts...)
	var agenticOpts []retriever.AgenticRetrieverOption
	if v, ok := kbOpts.Extra["max_iter"]; ok {
		if n, ok := v.(int); ok {
			agenticOpts = append(agenticOpts, retriever.WithAgenticMaxIter(n))
		}
	}
	agentic, err := retriever.NewAgenticRetriever(r, s.llmClient, agenticOpts...)
	if err != nil {
		return nil, err
	}
	return agentic, nil
}

// vectorStoreDatabaseName 防御式取 vector_store 的 database_name。
//
// Python: getattr(getattr(self.vector_store, "config", None), "database_name", "")
// 且非 str 归零。Go 用接口断言表达"属性存在性"，未实现即回落空串；
// 非 str 归零由静态类型天然满足（spec §8-5）。
func (s *SimpleKnowledgeBase) vectorStoreDatabaseName() string {
	if s.vectorStore == nil {
		return ""
	}
	if p, ok := s.vectorStore.(interface{ DatabaseName() string }); ok {
		return p.DatabaseName()
	}
	return ""
}
