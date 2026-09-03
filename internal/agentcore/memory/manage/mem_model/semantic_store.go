package mem_model

import (
	"context"

	embedding "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/embedding"
	vector "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/vector"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/common"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/migration"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// DocTuple 文档元组 (id, text)，对齐 Python 的 List[Tuple[str, str]]。
type DocTuple struct {
	// ID 文档唯一标识
	ID string
	// Text 文档原始文本
	Text string
}

// SearchResult 语义搜索结果，对齐 Python 的 Tuple[str, float]。
type SearchResult struct {
	// ID 匹配文档标识
	ID string
	// Score 相似度分数
	Score float64
}

// SemanticStore 语义向量存储，使用嵌入模型和向量存储实现语义搜索。
// 对齐 Python: openjiuwen/core/memory/manage/mem_model/semantic_store.py (SemanticStore)
type SemanticStore struct {
	// embeddingModel 嵌入模型（可选，可通过 InitializeEmbeddingModel 延迟设置）
	embeddingModel embedding.BaseEmbedding
	// vectorStore 向量存储后端
	vectorStore vector.BaseVectorStore
	// createdCollections 已创建的集合缓存，避免重复检查
	createdCollections map[string]struct{}
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSemanticStore 创建语义向量存储。embeddingModel 可为 nil。
// 对齐 Python: SemanticStore.__init__
func NewSemanticStore(vectorStore vector.BaseVectorStore, embeddingModel embedding.BaseEmbedding) *SemanticStore {
	return &SemanticStore{
		embeddingModel:     embeddingModel,
		vectorStore:        vectorStore,
		createdCollections: make(map[string]struct{}),
	}
}

// InitializeEmbeddingModel 初始化或更新嵌入模型。
// 对齐 Python: SemanticStore.initialize_embedding_model
func (s *SemanticStore) InitializeEmbeddingModel(embeddingModel embedding.BaseEmbedding) {
	s.embeddingModel = embeddingModel
}

// AddDocs 将文档添加到指定集合（先嵌入再存储向量）。
// 对齐 Python: SemanticStore.add_docs
func (s *SemanticStore) AddDocs(ctx context.Context, docs []DocTuple, tableName string, scopeID string) (bool, error) {
	if s.embeddingModel == nil {
		logger.Error(logComponent).
			Str("event_type", "MEMORY_STORE").
			Str("scope_id", scopeID).
			Str("collection_name", tableName).
			Msg("Embedding model not initialized, please call InitializeEmbeddingModel first.")
		return false, nil
	}

	// 分离 ID 和文本
	memoryIDs := make([]string, len(docs))
	texts := make([]string, len(docs))
	for i, doc := range docs {
		memoryIDs[i] = doc.ID
		texts[i] = doc.Text
	}

	// 生成嵌入
	embeddings, err := s.embeddingModel.EmbedDocuments(ctx, texts)
	if err != nil {
		logger.Error(logComponent).
			Str("event_type", "MEMORY_STORE").
			Str("scope_id", scopeID).
			Str("collection_name", tableName).
			Err(err).
			Msg("Failed to add documents to semantic store.")
		return false, nil
	}

	if len(memoryIDs) != len(embeddings) {
		logger.Error(logComponent).
			Str("event_type", "MEMORY_STORE").
			Str("scope_id", scopeID).
			Str("collection_name", tableName).
			Msg("memory_ids and embeddings must have same length")
		return false, nil
	}

	// 创建集合（如果不存在）
	if len(embeddings) > 0 {
		embeddingDim := len(embeddings[0])
		if err := s.createCollectionIfNotExists(ctx, tableName, embeddingDim); err != nil {
			logger.Error(logComponent).
				Str("event_type", "MEMORY_STORE").
				Str("scope_id", scopeID).
				Str("collection_name", tableName).
				Err(err).
				Msg("Failed to create collection.")
			return false, nil
		}
	}

	// 准备向量存储数据
	data := make([]map[string]any, len(memoryIDs))
	for i, docID := range memoryIDs {
		data[i] = map[string]any{
			"id":        docID,
			"embedding": embeddings[i],
		}
	}

	// 添加到向量存储
	if err := s.vectorStore.AddDocs(ctx, tableName, data); err != nil {
		logger.Error(logComponent).
			Str("event_type", "MEMORY_STORE").
			Str("scope_id", scopeID).
			Str("collection_name", tableName).
			Err(err).
			Msg("Failed to add documents to semantic store.")
		return false, nil
	}

	return true, nil
}

// DeleteDocs 从指定集合删除文档。
// 对齐 Python: SemanticStore.delete_docs
func (s *SemanticStore) DeleteDocs(ctx context.Context, ids []string, tableName string) error {
	exists, err := s.vectorStore.CollectionExists(ctx, tableName)
	if err != nil {
		logger.Error(logComponent).
			Str("event_type", "MEMORY_DELETE").
			Str("collection_name", tableName).
			Err(err).
			Msg("Failed to delete documents from semantic store.")
		return nil
	}
	if !exists {
		logger.Debug(logComponent).
			Str("event_type", "MEMORY_DELETE").
			Str("collection_name", tableName).
			Msg("Collection does not exist, nothing to delete")
		return nil
	}

	if err := s.vectorStore.DeleteDocsByIDs(ctx, tableName, ids); err != nil {
		logger.Error(logComponent).
			Str("event_type", "MEMORY_DELETE").
			Str("collection_name", tableName).
			Err(err).
			Msg("Failed to delete documents from semantic store.")
		return err
	}
	return nil
}

// Search 搜索与查询最相似的文档。返回 (id, score) 列表。
// 对齐 Python: SemanticStore.search
func (s *SemanticStore) Search(ctx context.Context, query, tableName string, scopeID string, topK int) ([]SearchResult, error) {
	if s.embeddingModel == nil {
		logger.Error(logComponent).
			Str("event_type", "MEMORY_RETRIEVE").
			Str("query", query).
			Str("collection_name", tableName).
			Msg("Embedding model not initialized, please call InitializeEmbeddingModel first.")
		return []SearchResult{}, nil
	}

	// 生成查询嵌入
	queryEmbedding, err := s.embeddingModel.EmbedQuery(ctx, query)
	if err != nil {
		logger.Error(logComponent).
			Str("event_type", "MEMORY_RETRIEVE").
			Str("query", query).
			Str("collection_name", tableName).
			Err(err).
			Msg("Failed to embed query.")
		return []SearchResult{}, nil
	}
	if len(queryEmbedding) == 0 {
		logger.Error(logComponent).
			Str("event_type", "MEMORY_RETRIEVE").
			Str("query", query).
			Str("collection_name", tableName).
			Msg("Failed to embed query.")
		return []SearchResult{}, nil
	}

	// 检查集合是否存在
	exists, err := s.vectorStore.CollectionExists(ctx, tableName)
	if err != nil || !exists {
		return []SearchResult{}, nil
	}

	// 向量搜索
	results, err := s.vectorStore.Search(ctx, tableName, queryEmbedding, "embedding", topK, nil)
	if err != nil {
		logger.Error(logComponent).
			Str("event_type", "MEMORY_RETRIEVE").
			Str("query", query).
			Str("collection_name", tableName).
			Err(err).
			Msg("Failed to embed query.")
		return []SearchResult{}, nil
	}

	// 转换结果格式
	searchResults := make([]SearchResult, 0, len(results))
	for _, result := range results {
		id, _ := result.Fields["id"].(string)
		searchResults = append(searchResults, SearchResult{
			ID:    id,
			Score: result.Score,
		})
	}
	return searchResults, nil
}

// DeleteTable 删除整个集合。
// 对齐 Python: SemanticStore.delete_table
func (s *SemanticStore) DeleteTable(ctx context.Context, tableName string) error {
	err := s.vectorStore.DeleteCollection(ctx, tableName)
	// 从缓存中移除
	delete(s.createdCollections, tableName)
	if err != nil {
		logger.Error(logComponent).
			Str("event_type", "MEMORY_DELETE").
			Str("collection_name", tableName).
			Err(err).
			Msg("Failed to delete table from semantic store.")
		return err
	}
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// createCollectionIfNotExists 若集合不存在则创建。
// 对齐 Python: SemanticStore._create_collection_if_not_exists
func (s *SemanticStore) createCollectionIfNotExists(ctx context.Context, collectionName string, embeddingDim int) error {
	// 检查内存缓存
	if _, ok := s.createdCollections[collectionName]; ok {
		return nil
	}

	// 检查集合是否已存在
	exists, err := s.vectorStore.CollectionExists(ctx, collectionName)
	if err != nil {
		return err
	}
	if exists {
		s.createdCollections[collectionName] = struct{}{}
		return nil
	}

	// 创建 schema
	idField, err := vector.NewFieldSchema("id", vector.VectorDataTypeVarchar,
		vector.WithPrimary(),
		vector.WithMaxLength(256),
	)
	if err != nil {
		return err
	}
	embeddingField, err := vector.NewFieldSchema("embedding", vector.VectorDataTypeFloatVector,
		vector.WithDim(embeddingDim),
	)
	if err != nil {
		return err
	}

	schema, err := vector.NewCollectionSchemaFromFields([]*vector.FieldSchema{idField, embeddingField},
		vector.WithCollectionDescription("Semantic memory collection"),
	)
	if err != nil {
		return err
	}

	if err := s.vectorStore.CreateCollection(ctx, collectionName, schema); err != nil {
		return err
	}

	// 写入 schema_version metadata
	memType := common.ParseMemtypeFromIdxName(collectionName)
	entityKey := "vector_" + memType
	latestSchemaVersion := migration.VectorRegistry.GetCurrentVersion(entityKey)
	if err := s.vectorStore.UpdateCollectionMetadata(ctx, collectionName, map[string]any{
		"schema_version": latestSchemaVersion,
	}); err != nil {
		return err
	}

	s.createdCollections[collectionName] = struct{}{}
	logger.Debug(logComponent).
		Str("event_type", "MEMORY_STORE").
		Str("collection_name", collectionName).
		Int("embedding_dim", embeddingDim).
		Msg("Created collection with embedding dimension")

	return nil
}
