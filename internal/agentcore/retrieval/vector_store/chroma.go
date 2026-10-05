package vector_store

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	chromav2 "github.com/amikos-tech/chroma-go/pkg/api/v2"
	"github.com/amikos-tech/chroma-go/pkg/embeddings"
	"github.com/google/uuid"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/vector"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/vector_fields"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/utils"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// chromaWhereFilter 适配器，将 WhereClause 包装为 WhereFilter 接口。
type chromaWhereFilter struct {
	clause chromav2.WhereClause
}

// ChromaVectorStore retrieval 层 ChromaDB 向量存储实现。
//
// 支持向量搜索、稀疏搜索（文本匹配）和混合搜索（RRF 融合）。
// 对齐 Python retrieval/vector_store/chroma_store.py (ChromaVectorStore)。
type ChromaVectorStore struct {
	// config 向量存储配置
	config common.VectorStoreConfig
	// collectionName 集合名称
	collectionName string
	// chromaPath ChromaDB 持久化路径
	chromaPath string
	// textField 文本字段名
	textField string
	// vectorField 向量字段配置
	vectorField *vector_fields.ChromaVectorField
	// sparseVectorField 稀疏向量字段名
	sparseVectorField string
	// metadataField 元数据字段名
	metadataField string
	// docIDField 文档 ID 字段名
	docIDField string
	// databaseName 数据库名称
	databaseName string
	// distanceMetric 归一化距离度量：cosine/l2/ip
	distanceMetric string
	// constructConfig 构建阶段配置
	constructConfig map[string]any
	// searchConfig 搜索阶段配置
	searchConfig map[string]any
	// client ChromaDB 客户端
	client chromav2.Client
	// collection ChromaDB 集合
	collection chromav2.Collection
	// mu 读写锁
	mu sync.RWMutex
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// defaultChromaBatchSize 默认批大小，对齐 Python batch_size=128
	defaultChromaBatchSize = 128
	// logComponent 日志组件
	logComponent = logger.ComponentAgentCore
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// String 实现 WhereFilter 接口
func (f *chromaWhereFilter) String() string {
	if f.clause != nil {
		return f.clause.String()
	}
	return ""
}

// Validate 实现 WhereFilter 接口
func (f *chromaWhereFilter) Validate() error {
	if f.clause != nil {
		return f.clause.Validate()
	}
	return nil
}

// MarshalJSON 实现 WhereFilter 接口
func (f *chromaWhereFilter) MarshalJSON() ([]byte, error) {
	if f.clause != nil {
		return f.clause.MarshalJSON()
	}
	return []byte("null"), nil
}

// UnmarshalJSON 实现 WhereFilter 接口
func (f *chromaWhereFilter) UnmarshalJSON(b []byte) error {
	if f.clause != nil {
		return f.clause.UnmarshalJSON(b)
	}
	return nil
}

// NewChromaVectorStore 创建 retrieval 层 ChromaVectorStore。
//
// 对齐 Python ChromaVectorStore.__init__。
// vectorField 参数为 *vector_fields.ChromaVectorField 完整配置，
// 对齐 Python vector_field: ChromaVectorField。
func NewChromaVectorStore(
	config common.VectorStoreConfig,
	chromaPath string,
	textField string,
	vectorField *vector_fields.ChromaVectorField,
	sparseVectorField string,
	metadataField string,
	docIDField string,
	_ ...StoreOption,
) (*ChromaVectorStore, error) {
	// 对齐 Python: if not chroma_path or not chroma_path.strip():
	//   raise build_error(RETRIEVAL_VECTOR_STORE_PATH_NOT_FOUND)
	if strings.TrimSpace(chromaPath) == "" {
		return nil, exception.BuildError(
			exception.StatusRetrievalVectorStorePathNotFound,
			exception.WithParam("error_msg", "chroma_path is required and cannot be empty"),
		)
	}

	if vectorField == nil {
		return nil, exception.BuildError(
			exception.StatusRetrievalIndexingVectorFieldInvalid,
			exception.WithParam("error_msg", "vector_field must not be nil"),
		)
	}

	s := &ChromaVectorStore{
		config:            config,
		collectionName:    config.CollectionName,
		chromaPath:        chromaPath,
		textField:         textField,
		sparseVectorField: sparseVectorField,
		metadataField:     metadataField,
		docIDField:        docIDField,
		databaseName:      config.DatabaseName,
		vectorField:       vectorField,
	}

	// 对齐 Python: self._distance_metric = config.distance_metric.replace("dot", "ip").replace("euclidean", "l2")
	s.distanceMetric = strings.ReplaceAll(
		strings.ReplaceAll(string(config.DistanceMetric), "dot", "ip"),
		"euclidean", "l2",
	)

	// 对齐 Python: self._construct_config = self.vector_field.to_dict(stage="construct")
	s.constructConfig = s.vectorField.ToConstructDict()
	s.constructConfig["space"] = s.distanceMetric

	// 对齐 Python: self._search_config = self.vector_field.to_dict(stage="search")
	s.searchConfig = s.vectorField.ToSearchDict()

	// 创建 ChromaDB 客户端
	clientOpts := []chromav2.PersistentClientOption{
		chromav2.WithPersistentPath(chromaPath),
		chromav2.WithPersistentLibraryAutoDownload(true),
	}
	client, err := chromav2.NewPersistentClient(clientOpts...)
	if err != nil {
		return nil, exception.BuildError(
			exception.StatusRetrievalVectorStoreProviderInvalid,
			exception.WithParam("error_msg", fmt.Sprintf("创建 ChromaDB 客户端失败: %s", err)),
			exception.WithCause(err),
		)
	}
	s.client = client

	// 对齐 Python: self._collection = self._client.get_or_create_collection(
	//   name=self.collection_name, configuration={"hnsw": self._construct_config | self._search_config})
	collection, err := s.client.GetOrCreateCollection(context.Background(), s.collectionName,
		chromav2.WithHNSWSpaceCreate(mapToChromaDistanceMetric(s.distanceMetric)),
	)
	if err != nil {
		return nil, exception.BuildError(
			exception.StatusRetrievalVectorStoreProviderInvalid,
			exception.WithParam("error_msg", fmt.Sprintf("创建/获取 ChromaDB 集合失败: %s", err)),
			exception.WithCause(err),
		)
	}
	s.collection = collection

	return s, nil
}

// Client 返回 ChromaDB 客户端
func (s *ChromaVectorStore) Client() chromav2.Client { return s.client }

// Collection 返回 ChromaDB 集合
func (s *ChromaVectorStore) Collection() chromav2.Collection { return s.collection }

// DistanceMetric 返回距离度量字符串
func (s *ChromaVectorStore) DistanceMetric() string { return s.distanceMetric }

// ConstructConfig 返回构建阶段配置
func (s *ChromaVectorStore) ConstructConfig() map[string]any { return s.constructConfig }

// SearchConfig 返回搜索阶段配置
func (s *ChromaVectorStore) SearchConfig() map[string]any { return s.searchConfig }

// CreateClient 创建 ChromaDB 客户端。
// 对齐 Python ChromaVectorStore.create_client。
// CheckVectorField 校验向量字段配置是否一致。
func (s *ChromaVectorStore) CheckVectorField() error {
	return CheckConfigsMatching(s.constructConfig, map[string]any{})
}

// Add 添加向量数据。
//
// 对齐 Python ChromaVectorStore.add + _add_batch。
func (s *ChromaVectorStore) Add(ctx context.Context, data []map[string]any, opts ...StoreOption) error {
	storeOpts := NewStoreOptions(opts...)
	batchSize := storeOpts.BatchSize
	if batchSize <= 0 {
		batchSize = defaultChromaBatchSize
	}

	processed := 0
	total := len(data)
	var cache []map[string]any

	for _, doc := range data {
		cache = append(cache, doc)
		if len(cache) >= batchSize {
			nodes := cache[:batchSize]
			cache = cache[batchSize:]
			if err := s.addBatch(ctx, nodes); err != nil {
				return err
			}
			processed += len(nodes)
			// 对齐 Python: if processed % 100 == 0: logger.info("Written %d/%d records to %s", ...)
			if processed%100 == 0 {
				logger.Info(logComponent).
					Int("processed", processed).
					Int("total", total).
					Str("collection_name", s.collectionName).
					Msg("写入进度")
			}
		}
	}

	if len(cache) > 0 {
		if err := s.addBatch(ctx, cache); err != nil {
			return err
		}
		processed += len(cache)
	}

	// 对齐 Python: logger.info("Writing completed, total %d/%d records to %s", ...)
	logger.Info(logComponent).
		Int("processed", processed).
		Int("total", total).
		Str("collection_name", s.collectionName).
		Msg("写入完成")

	return nil
}

// Search 向量搜索。
// 对齐 Python ChromaVectorStore.search。
func (s *ChromaVectorStore) Search(ctx context.Context, queryVector []float64, topK int, filters map[string]any, _ ...StoreOption) ([]common.SearchResult, error) {
	collection, err := s.getCollection(ctx)
	if err != nil {
		return nil, err
	}

	queryEmb := embeddings.NewEmbeddingFromFloat64(queryVector)
	queryOpts := []chromav2.QueryOption{
		chromav2.WithQueryEmbeddings(queryEmb),
		chromav2.WithNResults(topK),
		chromav2.WithInclude(chromav2.IncludeDocuments, chromav2.IncludeMetadatas, chromav2.IncludeDistances),
	}
	if wf := BuildChromaWhereFilter(filters); wf != nil {
		queryOpts = append(queryOpts, chromav2.WithWhere(wf))
	}

	result, err := collection.Query(ctx, queryOpts...)
	if err != nil {
		logger.Error(logComponent).
			Str("event_type", "SEARCH_ERROR").
			Str("method", "Search").
			Err(err).
			Msg("向量搜索失败")
		return nil, err
	}

	return s.chromaResultToSearchResults(result, "vector"), nil
}

// SparseSearch 稀疏搜索（文本匹配）。
// 对齐 Python ChromaVectorStore.sparse_search。
func (s *ChromaVectorStore) SparseSearch(ctx context.Context, queryText string, topK int, filters map[string]any, _ ...StoreOption) ([]common.SearchResult, error) {
	collection, err := s.getCollection(ctx)
	if err != nil {
		return nil, err
	}

	queryOpts := []chromav2.QueryOption{
		chromav2.WithQueryTexts(queryText),
		chromav2.WithNResults(topK),
		chromav2.WithInclude(chromav2.IncludeDocuments, chromav2.IncludeMetadatas, chromav2.IncludeDistances),
	}
	if wf := BuildChromaWhereFilter(filters); wf != nil {
		queryOpts = append(queryOpts, chromav2.WithWhere(wf))
	}

	result, err := collection.Query(ctx, queryOpts...)
	if err != nil {
		// 对齐 Python: except: logger.warning(...); return []
		logger.Warn(logComponent).
			Str("method", "SparseSearch").
			Str("query_text", truncateText(queryText, 100)).
			Err(err).
			Msg("文本搜索失败")
		return nil, nil
	}

	return s.chromaResultToSearchResults(result, "sparse"), nil
}

// HybridSearch 混合搜索（向量+文本 RRF 融合）。
// 对齐 Python ChromaVectorStore.hybrid_search。
func (s *ChromaVectorStore) HybridSearch(ctx context.Context, queryText string, queryVector []float64, topK int, alpha float64, filters map[string]any, _ ...StoreOption) ([]common.SearchResult, error) {
	type searchResult struct {
		results []common.SearchResult
		err     error
		mode    string
	}

	resultCh := make(chan searchResult, 2)

	// 并发执行向量搜索和文本搜索
	go func() {
		if queryVector != nil {
			results, err := s.Search(ctx, queryVector, topK*2, filters)
			resultCh <- searchResult{results: results, err: err, mode: "vector"}
		} else {
			resultCh <- searchResult{mode: "vector"}
		}
	}()

	go func() {
		results, err := s.SparseSearch(ctx, queryText, topK*2, filters)
		resultCh <- searchResult{results: results, err: err, mode: "text"}
	}()

	var resultsList [][]common.RetrievalResult
	idMapping := make(map[string]string)

	for i := 0; i < 2; i++ {
		sr := <-resultCh
		if sr.err != nil {
			logger.Warn(logComponent).Str("mode", sr.mode).Err(sr.err).Msg("hybrid search 部分路径失败")
			continue
		}
		if len(sr.results) == 0 {
			continue
		}

		var retrievalResults []common.RetrievalResult
		for _, searchRes := range sr.results {
			idMapping[searchRes.Text] = searchRes.ID
			metadata := make(map[string]any)
			for k, v := range searchRes.Metadata {
				metadata[k] = v
			}
			metadata["id"] = searchRes.ID

			docID, _ := searchRes.Metadata[s.docIDField].(string)
			chunkID, _ := searchRes.Metadata["chunk_id"].(string)
			retrievalResults = append(retrievalResults, common.RetrievalResult{
				Text:     searchRes.Text,
				Score:    searchRes.Score,
				Metadata: metadata,
				DocID:    docID,
				ChunkID:  chunkID,
			})
		}
		resultsList = append(resultsList, retrievalResults)
	}

	if len(resultsList) == 0 {
		return nil, nil
	}

	fusedRetrievalResults := utils.RRFFusion(resultsList, 60)

	var fusedResults []common.SearchResult
	for i, rr := range fusedRetrievalResults {
		if i >= topK {
			break
		}
		resultID, _ := rr.Metadata["id"].(string)
		if resultID == "" {
			resultID = idMapping[rr.Text]
		}
		metadata := make(map[string]any)
		for k, v := range rr.Metadata {
			if k != "id" {
				metadata[k] = v
			}
		}
		fusedResults = append(fusedResults, common.SearchResult{
			ID:       resultID,
			Text:     rr.Text,
			Score:    rr.Score,
			Metadata: metadata,
		})
	}

	return fusedResults, nil
}

// Delete 删除向量。
// 对齐 Python ChromaVectorStore.delete。
func (s *ChromaVectorStore) Delete(ctx context.Context, ids []string, filterExpr map[string]any) (bool, error) {
	collection, err := s.getCollection(ctx)
	if err != nil {
		return false, err
	}

	if len(ids) > 0 {
		chromaIDs := make([]chromav2.DocumentID, len(ids))
		for i, id := range ids {
			chromaIDs[i] = chromav2.DocumentID(id)
		}
		if err := collection.Delete(ctx, chromav2.WithIDs(chromaIDs...)); err != nil {
			logger.Error(logComponent).Str("event_type", "DELETE_ERROR").Err(err).Msg("删除向量失败")
			return false, err
		}
		return true, nil
	}

	if filterExpr != nil {
		if wf := BuildChromaWhereFilter(filterExpr); wf != nil {
			if err := collection.Delete(ctx, chromav2.WithWhere(wf)); err != nil {
				logger.Error(logComponent).Str("event_type", "DELETE_ERROR").Err(err).Msg("删除向量失败")
				return false, err
			}
			return true, nil
		}
		// 对齐 Python: logger.warning("ChromaDB does not support string filter expressions.")
		logger.Warn(logComponent).Msg("ChromaDB does not support string filter expressions.")
		return false, nil
	}

	return false, nil
}

// TableExists 检查集合是否存在。
func (s *ChromaVectorStore) TableExists(ctx context.Context, tableName string) (bool, error) {
	collections, err := s.client.ListCollections(ctx)
	if err != nil {
		return false, err
	}
	for _, c := range collections {
		if c.Name() == tableName {
			return true, nil
		}
	}
	return false, nil
}

// DeleteTable 删除集合。
func (s *ChromaVectorStore) DeleteTable(ctx context.Context, tableName string) error {
	return s.client.DeleteCollection(ctx, tableName)
}

// Close 关闭存储。
func (s *ChromaVectorStore) Close() {
	if s.client != nil {
		_ = s.client.Close()
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// addBatch 批量添加数据到 ChromaDB。
// 对齐 Python _add_batch。
func (s *ChromaVectorStore) addBatch(ctx context.Context, nodes []map[string]any) error {
	var ids []chromav2.DocumentID
	var allEmbeddings []embeddings.Embedding
	var texts []string
	var metas []chromav2.DocumentMetadata

	for _, node := range nodes {
		// 对齐 Python: embedding = node.get(self.vector_field.vector_field, [])
		embedding, _ := node[s.vectorField.VectorFieldName].([]float64)
		if len(embedding) == 0 {
			// 对齐 Python: logger.warning(f"Node has no embedding, skipping: ...")
			nodeID, _ := node["id"].(string)
			logger.Warn(logComponent).Str("node_id", nodeID).Msg("节点无嵌入向量，跳过")
			continue
		}

		// 对齐 Python: node_id = str(node.get("id", node.get("pk", ""))); if not node_id: uuid.uuid4()
		nodeID, _ := node["id"].(string)
		if nodeID == "" {
			nodeID, _ = node["pk"].(string)
		}
		if nodeID == "" {
			nodeID = uuid.NewString()
		}
		ids = append(ids, chromav2.DocumentID(nodeID))
		allEmbeddings = append(allEmbeddings, embeddings.NewEmbeddingFromFloat64(embedding))

		// 对齐 Python: text = node.get(self.text_field, "")
		text, _ := node[s.textField].(string)
		texts = append(texts, text)

		// 对齐 Python: metadata = {}; if self.metadata_field in node: ...
		metaMap := make(map[string]any)
		if rawMeta, ok := node[s.metadataField]; ok {
			switch m := rawMeta.(type) {
			case map[string]any:
				for k, v := range m {
					metaMap[k] = v
				}
			case string:
				var parsed map[string]any
				if err := json.Unmarshal([]byte(m), &parsed); err == nil {
					for k, v := range parsed {
						metaMap[k] = v
					}
				}
			}
		}

		// 对齐 Python: if self.doc_id_field in node: metadata[self.doc_id_field] = str(node[self.doc_id_field])
		if docID, ok := node[s.docIDField]; ok {
			metaMap[s.docIDField] = fmt.Sprintf("%v", docID)
		}
		if chunkID, ok := node["chunk_id"]; ok {
			metaMap["chunk_id"] = fmt.Sprintf("%v", chunkID)
		}
		if sparseVec, ok := node[s.sparseVectorField]; ok {
			jsonBytes, err := json.Marshal(sparseVec)
			if err == nil {
				metaMap[s.sparseVectorField] = string(jsonBytes)
			}
		}

		docMeta, err := chromav2.NewDocumentMetadataFromMap(metaMap)
		if err != nil {
			docMeta = chromav2.NewDocumentMetadata()
		}
		metas = append(metas, docMeta)
	}

	if len(ids) == 0 {
		return nil
	}

	collection, err := s.getCollection(ctx)
	if err != nil {
		return err
	}

	addOpts := []chromav2.AddOption{
		chromav2.WithIDs(ids...),
		chromav2.WithEmbeddings(allEmbeddings...),
		chromav2.WithTexts(texts...),
		chromav2.WithMetadatas(metas...),
	}

	if err := collection.Add(ctx, addOpts...); err != nil {
		return exception.BuildError(
			exception.StatusRetrievalVectorStoreProviderInvalid,
			exception.WithParam("error_msg", fmt.Sprintf("批量添加数据失败: %s", err)),
			exception.WithCause(err),
		)
	}
	return nil
}

// getCollection 获取最新集合引用。
func (s *ChromaVectorStore) getCollection(ctx context.Context) (chromav2.Collection, error) {
	s.mu.RLock()
	if s.collection != nil {
		s.mu.RUnlock()
		return s.collection, nil
	}
	s.mu.RUnlock()

	s.mu.Lock()
	defer s.mu.Unlock()

	collection, err := s.client.GetCollection(ctx, s.collectionName)
	if err != nil {
		return nil, exception.BuildError(
			exception.StatusRetrievalVectorStoreProviderInvalid,
			exception.WithParam("error_msg", fmt.Sprintf("获取 ChromaDB 集合失败: %s", err)),
			exception.WithCause(err),
		)
	}
	s.collection = collection
	return collection, nil
}

// chromaResultToSearchResults 将 ChromaDB 搜索结果转为 SearchResult 列表。
// 对齐 Python ChromaVectorStore._chroma_result_to_search_results。
func (s *ChromaVectorStore) chromaResultToSearchResults(result chromav2.QueryResult, mode string) []common.SearchResult {
	if result == nil {
		return nil
	}

	idGroups := result.GetIDGroups()
	if len(idGroups) == 0 {
		return nil
	}

	docGroups := result.GetDocumentsGroups()
	metaGroups := result.GetMetadatasGroups()
	distGroups := result.GetDistancesGroups()

	var searchResults []common.SearchResult

	// 使用第一个查询组的结果（单查询向量）
	ids := idGroups[0]
	for idx, resultID := range ids {
		text := ""
		if len(docGroups) > 0 && idx < len(docGroups[0]) && docGroups[0][idx] != nil {
			text = docGroups[0][idx].ContentString()
		}

		metadata := make(map[string]any)
		if len(metaGroups) > 0 && idx < len(metaGroups[0]) && metaGroups[0][idx] != nil {
			meta := metaGroups[0][idx]
			for _, key := range chromaMetaKeys(meta) {
				if val, ok := meta.GetRaw(key); ok {
					if mv, isMv := val.(chromav2.MetadataValue); isMv {
						if rawVal, ok := mv.GetRaw(); ok {
							metadata[key] = rawVal
						}
					} else {
						metadata[key] = val
					}
				}
			}
		}

		// 对齐 Python: if "doc_id" not in metadata: metadata["doc_id"] = metadata.pop(self.doc_id_field, None)
		if _, ok := metadata["doc_id"]; !ok {
			if docID, ok := metadata[s.docIDField]; ok {
				metadata["doc_id"] = docID
				delete(metadata, s.docIDField)
			}
		}

		var rawScoreVal *float64
		if len(distGroups) > 0 && idx < len(distGroups[0]) {
			v := float64(distGroups[0][idx])
			rawScoreVal = &v
		}

		var finalScore float64
		var rawScoreScaled *float64

		switch mode {
		case "vector":
			if rawScoreVal != nil {
				switch s.distanceMetric {
				case "l2":
					scaled := vector.ConvertL2Squared(*rawScoreVal, 0)
					rawScoreScaled = &scaled
					finalScore = scaled
				case "cosine":
					scaled := vector.ConvertCosineDistance(*rawScoreVal)
					rawScoreScaled = &scaled
					finalScore = scaled
				default: // ip
					scaled := vector.ConvertIPDistance(*rawScoreVal)
					rawScoreScaled = &scaled
					finalScore = scaled
				}
			}
		case "sparse":
			if rawScoreVal != nil {
				if *rawScoreVal <= 1.0 {
					finalScore = 1.0 - *rawScoreVal
				} else {
					finalScore = *rawScoreVal
				}
			} else {
				finalScore = 0.5
			}
		}

		if rawScoreVal != nil {
			metadata["raw_score"] = *rawScoreVal
		}
		if rawScoreScaled != nil {
			metadata["raw_score_scaled"] = *rawScoreScaled
		}

		searchResults = append(searchResults, common.SearchResult{
			ID:       string(resultID),
			Text:     text,
			Score:    finalScore,
			Metadata: metadata,
		})
	}

	return searchResults
}

// BuildChromaWhereFilter 构建 ChromaDB where 过滤器。
// 返回 chromav2.WhereFilter 接口，可跨包使用（如 indexer 包调用）。
// filters 为 nil 或空 map 时返回 nil。
func BuildChromaWhereFilter(filters map[string]any) chromav2.WhereFilter {
	if filters == nil {
		return nil
	}

	f := filters

	var clauses []chromav2.WhereClause
	for key, value := range f {
		switch v := value.(type) {
		case string:
			clauses = append(clauses, chromav2.EqString(key, v))
		case int:
			clauses = append(clauses, chromav2.EqInt(key, v))
		case int64:
			clauses = append(clauses, chromav2.EqInt(key, int(v)))
		case float32:
			clauses = append(clauses, chromav2.EqFloat(key, v))
		case float64:
			clauses = append(clauses, chromav2.EqFloat(key, float32(v)))
		case bool:
			clauses = append(clauses, chromav2.EqBool(key, v))
		default:
			clauses = append(clauses, chromav2.EqString(key, fmt.Sprintf("%v", v)))
		}
	}

	if len(clauses) == 0 {
		return nil
	}
	if len(clauses) == 1 {
		return &chromaWhereFilter{clause: clauses[0]}
	}
	return &chromaWhereFilter{clause: chromav2.And(clauses...)}
}

// mapToChromaDistanceMetric 将距离度量字符串转为 ChromaDB 距离函数。
func mapToChromaDistanceMetric(metric string) embeddings.DistanceMetric {
	switch strings.ToUpper(metric) {
	case "L2":
		return embeddings.L2
	case "IP":
		return embeddings.IP
	case "COSINE":
		return embeddings.COSINE
	default:
		return embeddings.COSINE
	}
}

// truncateText 截断文本用于日志输出
func truncateText(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// chromaMetaKeys 获取 DocumentMetadata 的所有键。
func chromaMetaKeys(meta chromav2.DocumentMetadata) []string {
	if impl, ok := meta.(*chromav2.DocumentMetadataImpl); ok {
		return impl.Keys()
	}
	return nil
}
