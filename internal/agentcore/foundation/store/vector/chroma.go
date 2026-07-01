package vector

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	chromav2 "github.com/amikos-tech/chroma-go/pkg/api/v2"
	"github.com/amikos-tech/chroma-go/pkg/embeddings"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// chromaWhereFilter 适配器，将 WhereClause 包装为 WhereFilter 接口。
// ChromaDB SDK v2 的 WhereClause 和 WhereFilter 是不同的接口，
// WithWhere 需要 WhereFilter，而 EqString/And 等返回 WhereClause，
// 因此需要适配器桥接两者。
type chromaWhereFilter struct {
	clause chromav2.WhereClause
}

// chromaFieldMapping Chroma 集合的字段映射缓存。
//
// ChromaDB 不支持传统数据库的字段 Schema，通过 DocumentMetadata 存储字段映射信息。
// 本结构缓存主键字段名、向量字段名、距离度量和文本字段映射，
// 用于在 AddDocs/Search/GetAllDocuments 等操作中进行字段名与 ChromaDB 内部格式之间的转换。
type chromaFieldMapping struct {
	// PKField 主键字段名
	PKField string
	// VectorField 向量字段名
	VectorField string
	// DistanceMetric 距离度量方式（COSINE/L2/IP）
	DistanceMetric string
	// TextFieldMapping 文本字段映射：用户字段名 → ChromaDB 文档文本
	// 用户字段名（如 "content"）映射为 ChromaDB 的 document 字段
	TextFieldMapping map[string]bool
}

// ChromaVectorStore 基于 ChromaDB 的向量存储实现。
//
// 实现 BaseVectorStore 接口，使用 chroma-go v2 SDK 作为客户端。
// 客户端惰性创建，初始化时不需要 ChromaDB 可用。
// Schema 和字段映射信息通过 ChromaDB 的 CollectionMetadata 存储，
// 支持跨进程持久化和恢复。
//
// 对应 Python: vector/chroma_vector_store.py (ChromaVectorStore)
type ChromaVectorStore struct {
	// client ChromaDB 客户端实例
	client chromav2.Client
	// persistPath 持久化存储路径
	persistPath string
	// collectionCache 集合对象缓存
	collectionCache map[string]chromav2.Collection
	// fieldMappingCache 字段映射缓存
	fieldMappingCache map[string]*chromaFieldMapping
	// mu 读写锁，保护客户端和缓存
	mu sync.RWMutex
	// createClient 客户端创建函数，用于依赖注入和测试
	createClient func(persistPath string) (chromav2.Client, error)
}

// ──────────────────────────── 常量 ────────────────────────────
const (
	// chromaMetadataKeySchema CollectionMetadata 中存储 Schema JSON 的键
	chromaMetadataKeySchema = "schema"
	// chromaMetadataKeyFieldMapping CollectionMetadata 中存储字段映射 JSON 的键
	chromaMetadataKeyFieldMapping = "field_mapping"
	// chromaMetadataKeyDistanceMetric CollectionMetadata 中存储距离度量的键
	chromaMetadataKeyDistanceMetric = "distance_metric"
	// chromaMetadataKeyVectorField CollectionMetadata 中存储向量字段名的键
	chromaMetadataKeyVectorField = "vector_field"
	// chromaDefaultBatchSize ChromaDB 默认批量插入大小
	chromaDefaultBatchSize = 100
)

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

// NewChromaVectorStore 创建 ChromaVectorStore 实例。
// 客户端惰性创建，初始化时不需要 ChromaDB 可用。
//
// 对应 Python: ChromaVectorStore.__init__(persist_path)
func NewChromaVectorStore(persistPath string) *ChromaVectorStore {
	return &ChromaVectorStore{
		persistPath:       persistPath,
		collectionCache:   make(map[string]chromav2.Collection),
		fieldMappingCache: make(map[string]*chromaFieldMapping),
		createClient:      defaultChromaCreateClient,
	}
}

// Close 关闭 ChromaDB 客户端连接。
//
// 对应 Python: ChromaVectorStore.close()
func (s *ChromaVectorStore) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.client != nil {
		_ = s.client.Close()
		s.client = nil
		logger.Info(logComponent).Str("action", "close").Msg("ChromaDB 客户端连接已关闭")
	}
}

// CreateCollection 创建集合。
// 校验 schema（必须有主键字段和向量字段），构建字段映射，
// 将 schema 和字段映射序列化后存入 ChromaDB 的 CollectionMetadata。
//
// 对应 Python: ChromaVectorStore.create_collection(collection_name, schema, **kwargs)
func (s *ChromaVectorStore) CreateCollection(ctx context.Context, collectionName string, schema *CollectionSchema, opts ...Option) error {
	o := newOptions(opts...)
	c, err := s.getClient()
	if err != nil {
		return err
	}

	// 校验 schema：必须有主键字段
	pkField := schema.GetPrimaryKeyField()
	if pkField == nil {
		return exception.BuildError(exception.StatusStoreVectorSchemaInvalid,
			exception.WithParam("error_msg", "schema must contain a primary key field"),
		)
	}

	// 校验 schema：必须有向量字段
	vectorFields := schema.GetVectorFields()
	if len(vectorFields) == 0 {
		return exception.BuildError(exception.StatusStoreVectorSchemaInvalid,
			exception.WithParam("error_msg", "schema must contain at least one FLOAT_VECTOR field"),
		)
	}

	// 确定向量字段名和距离度量
	vectorField := vectorFields[0]
	distanceMetric := o.DistanceMetric
	if distanceMetric == "" {
		distanceMetric = defaultDistanceMetric
	}
	distanceMetric = strings.ToUpper(distanceMetric)

	// 构建字段映射
	textFieldMapping := make(map[string]bool)
	for _, field := range schema.Fields() {
		if field.DType == VectorDataTypeVarchar && !field.IsPrimary {
			textFieldMapping[field.Name] = true
		}
	}

	fieldMapping := &chromaFieldMapping{
		PKField:          pkField.Name,
		VectorField:      vectorField.Name,
		DistanceMetric:   distanceMetric,
		TextFieldMapping: textFieldMapping,
	}

	// 构建 CollectionMetadata，将 schema 和 fieldMapping 序列化为 JSON 字符串存储
	schemaJSON, err := json.Marshal(schema.ToDict())
	if err != nil {
		return exception.BuildError(exception.StatusStoreVectorSchemaInvalid,
			exception.WithParam("error_msg", fmt.Sprintf("failed to marshal schema: %v", err)),
		)
	}
	fmJSON, err := json.Marshal(fieldMapping)
	if err != nil {
		return exception.BuildError(exception.StatusStoreVectorSchemaInvalid,
			exception.WithParam("error_msg", fmt.Sprintf("failed to marshal field mapping: %v", err)),
		)
	}

	metadataMap := map[string]interface{}{
		chromaMetadataKeySchema:         string(schemaJSON),
		chromaMetadataKeyFieldMapping:   string(fmJSON),
		chromaMetadataKeyDistanceMetric: distanceMetric,
		chromaMetadataKeyVectorField:    vectorField.Name,
	}
	collectionMetadata := chromav2.NewMetadataFromMap(metadataMap)

	// 映射距离度量为 ChromaDB SDK 的 DistanceMetric
	chromaMetric := mapToChromaDistanceMetric(distanceMetric)

	// 创建或获取集合
	collection, err := c.GetOrCreateCollection(ctx, collectionName,
		chromav2.WithCollectionMetadataCreate(collectionMetadata),
		chromav2.WithHNSWSpaceCreate(chromaMetric),
	)
	if err != nil {
		logger.Error(logComponent).Err(err).Str("collection_name", collectionName).Msg("创建 ChromaDB 集合失败")
		return exception.BuildError(exception.StatusStoreVectorSchemaInvalid,
			exception.WithParam("error_msg", fmt.Sprintf("failed to create collection: %v", err)),
		)
	}

	// 缓存集合和字段映射
	s.mu.Lock()
	s.collectionCache[collectionName] = collection
	s.fieldMappingCache[collectionName] = fieldMapping
	s.mu.Unlock()

	logger.Info(logComponent).Str("collection_name", collectionName).
		Str("distance_metric", distanceMetric).
		Str("vector_field", vectorField.Name).
		Msg("成功创建 ChromaDB 集合")
	return nil
}

// DeleteCollection 删除集合。
//
// 对应 Python: ChromaVectorStore.delete_collection(collection_name)
func (s *ChromaVectorStore) DeleteCollection(ctx context.Context, collectionName string, opts ...Option) error {
	c, err := s.getClient()
	if err != nil {
		return err
	}

	if err := c.DeleteCollection(ctx, collectionName); err != nil {
		logger.Error(logComponent).Err(err).Str("collection_name", collectionName).Msg("删除 ChromaDB 集合失败")
		return err
	}

	// 清除缓存
	s.mu.Lock()
	delete(s.collectionCache, collectionName)
	delete(s.fieldMappingCache, collectionName)
	s.mu.Unlock()

	logger.Info(logComponent).Str("collection_name", collectionName).Msg("成功删除 ChromaDB 集合")
	return nil
}

// CollectionExists 检查集合是否存在。
//
// 对应 Python: ChromaVectorStore.collection_exists(collection_name)
func (s *ChromaVectorStore) CollectionExists(ctx context.Context, collectionName string, opts ...Option) (bool, error) {
	c, err := s.getClient()
	if err != nil {
		return false, err
	}

	collections, err := c.ListCollections(ctx)
	if err != nil {
		return false, err
	}

	for _, coll := range collections {
		if coll.Name() == collectionName {
			return true, nil
		}
	}
	return false, nil
}

// GetSchema 获取集合的 Schema。
// 优先从 CollectionMetadata 的 "schema" 字段反序列化，
// 其次从 "fields" 字段读取，最后从 fieldMapping 推断默认 schema。
//
// 对应 Python: ChromaVectorStore.get_schema(collection_name)
func (s *ChromaVectorStore) GetSchema(ctx context.Context, collectionName string, opts ...Option) (*CollectionSchema, error) {
	collection, err := s.getCollection(ctx, collectionName)
	if err != nil {
		return nil, err
	}

	meta := collection.Metadata()
	if meta == nil {
		return s.inferDefaultSchema(collectionName)
	}

	// 优先从 schema 字段读取
	if schemaStr, ok := meta.GetString(chromaMetadataKeySchema); ok && schemaStr != "" {
		var schemaDict map[string]any
		if err := json.Unmarshal([]byte(schemaStr), &schemaDict); err == nil {
			schema, err := CollectionFromDict(schemaDict)
			if err == nil && schema != nil {
				return schema, nil
			}
		}
	}

	// Fallback: 从 fieldMapping 推断默认 schema
	return s.inferDefaultSchema(collectionName)
}

// AddDocs 添加文档到集合。
// 从 fieldMapping 提取 ids/embeddings/documents/metadatas，
// 分批调用 ChromaDB 的 collection.Add。
//
// 对应 Python: ChromaVectorStore.add_docs(collection_name, docs, **kwargs)
func (s *ChromaVectorStore) AddDocs(ctx context.Context, collectionName string, docs []map[string]any, opts ...Option) error {
	if len(docs) == 0 {
		return nil
	}

	o := newOptions(opts...)
	batchSize := o.BatchSize
	if batchSize <= 0 {
		batchSize = chromaDefaultBatchSize
	}

	collection, err := s.getCollection(ctx, collectionName)
	if err != nil {
		return err
	}

	fm, err := s.getFieldMapping(ctx, collectionName)
	if err != nil {
		return err
	}

	total := len(docs)

	for i := 0; i < total; i += batchSize {
		end := i + batchSize
		if end > total {
			end = total
		}
		batch := docs[i:end]

		// 从文档列表提取 ids、embeddings、texts、metadatas
		ids := make([]chromav2.DocumentID, 0, len(batch))
		var allEmbeddings []embeddings.Embedding
		texts := make([]string, 0, len(batch))
		metas := make([]chromav2.DocumentMetadata, 0, len(batch))

		for _, doc := range batch {
			// 提取主键
			pkValue, ok := doc[fm.PKField]
			if !ok {
				return exception.BuildError(exception.StatusStoreVectorDocInvalid,
					exception.WithParam("error_msg", fmt.Sprintf("document missing primary key field: %s", fm.PKField)),
				)
			}
			ids = append(ids, chromav2.DocumentID(fmt.Sprintf("%v", pkValue)))

			// 提取向量
			if emb, ok := doc[fm.VectorField]; ok {
				embSlice := toFloat32Slice(emb)
				if embSlice != nil {
					allEmbeddings = append(allEmbeddings, embSlice)
				}
			}

			// 提取文本字段，拼接为 document
			var textParts []string
			for textField := range fm.TextFieldMapping {
				if val, ok := doc[textField]; ok {
					textParts = append(textParts, fmt.Sprintf("%v", val))
				}
			}
			texts = append(texts, strings.Join(textParts, " "))

			// 提取 metadata（除主键、向量、文本字段外的所有字段）
			metaMap := make(map[string]interface{})
			for k, v := range doc {
				if k == fm.PKField || k == fm.VectorField || fm.TextFieldMapping[k] {
					continue
				}
				// 仅存储基本类型
				switch v.(type) {
				case string, int, int64, int32, float64, float32, bool:
					metaMap[k] = v
				}
			}
			docMeta, err := chromav2.NewDocumentMetadataFromMap(metaMap)
			if err != nil {
				logger.Warn(logComponent).Err(err).Str("collection_name", collectionName).
					Msg("转换文档 metadata 失败，使用空 metadata")
				docMeta = chromav2.NewDocumentMetadata()
			}
			metas = append(metas, docMeta)
		}

		// 构建 Add 选项
		addOpts := []chromav2.AddOption{
			chromav2.WithIDs(ids...),
		}
		if len(allEmbeddings) > 0 {
			addOpts = append(addOpts, chromav2.WithEmbeddings(allEmbeddings...))
		}
		if len(texts) > 0 {
			addOpts = append(addOpts, chromav2.WithTexts(texts...))
		}
		if len(metas) > 0 {
			addOpts = append(addOpts, chromav2.WithMetadatas(metas...))
		}

		if err := collection.Add(ctx, addOpts...); err != nil {
			logger.Error(logComponent).Err(err).Str("collection_name", collectionName).
				Int("batch_start", i).Int("batch_size", len(batch)).Msg("插入文档批次失败")
			return exception.BuildError(exception.StatusStoreVectorDocInvalid,
				exception.WithParam("error_msg", fmt.Sprintf("failed to add docs: %v", err)),
			)
		}
	}

	logger.Info(logComponent).Str("collection_name", collectionName).
		Int("total", total).Msg("成功添加文档到 ChromaDB 集合")
	return nil
}

// Search 向量相似度搜索。
// 调用 ChromaDB 的 collection.Query，根据距离度量转换分数，
// 遍历 QueryResult 映射回用户字段名。
//
// 对应 Python: ChromaVectorStore.search(collection_name, query_vector, vector_field, top_k, filters, **kwargs)
func (s *ChromaVectorStore) Search(ctx context.Context, collectionName string, queryVector []float64, vectorField string, topK int, filters map[string]any, opts ...Option) ([]VectorSearchResult, error) {
	if topK <= 0 {
		topK = 5
	}

	collection, err := s.getCollection(ctx, collectionName)
	if err != nil {
		return nil, err
	}

	fm, err := s.getFieldMapping(ctx, collectionName)
	if err != nil {
		return nil, err
	}

	// 转换查询向量为 Float32Embedding
	querySlice := make([]float32, len(queryVector))
	for i, v := range queryVector {
		querySlice[i] = float32(v)
	}
	queryEmb := &embeddings.Float32Embedding{ArrayOfFloat32: &querySlice}

	// 构建 Query 选项
	queryOpts := []chromav2.QueryOption{
		chromav2.WithQueryEmbeddings(queryEmb),
		chromav2.WithNResults(topK),
		chromav2.WithInclude(chromav2.IncludeDocuments, chromav2.IncludeMetadatas, chromav2.IncludeDistances),
	}

	// 构建过滤条件
	if len(filters) > 0 {
		whereClause := buildChromaWhereFilter(filters)
		if whereClause != nil {
			queryOpts = append(queryOpts, chromav2.WithWhere(whereClause))
		}
	}

	result, err := collection.Query(ctx, queryOpts...)
	if err != nil {
		logger.Error(logComponent).Err(err).Str("collection_name", collectionName).Msg("ChromaDB 向量搜索失败")
		return nil, err
	}

	// 获取距离转换函数
	scoreConverter := getScoreConverter(fm.DistanceMetric)

	// 转换结果
	searchResults := make([]VectorSearchResult, 0)
	idGroups := result.GetIDGroups()
	if len(idGroups) == 0 {
		return searchResults, nil
	}

	// 使用第一个查询组的结果（单查询向量）
	docGroups := result.GetDocumentsGroups()
	metaGroups := result.GetMetadatasGroups()
	distGroups := result.GetDistancesGroups()

	for g, ids := range idGroups {
		var docs chromav2.Documents
		if g < len(docGroups) {
			docs = docGroups[g]
		}
		var metas chromav2.DocumentMetadatas
		if g < len(metaGroups) {
			metas = metaGroups[g]
		}
		var distances embeddings.Distances
		if g < len(distGroups) {
			distances = distGroups[g]
		}

		for i, id := range ids {
			score := float64(0)
			if i < len(distances) {
				score = scoreConverter(float64(distances[i]))
			}

			fields := make(map[string]any)
			fields[fm.PKField] = string(id)

			// 从 document 还原文本字段
			if i < len(docs) && docs[i] != nil {
				text := docs[i].ContentString()
				if text != "" && len(fm.TextFieldMapping) > 0 {
					// 如果只有一个文本字段，直接映射
					if len(fm.TextFieldMapping) == 1 {
						for textField := range fm.TextFieldMapping {
							fields[textField] = text
						}
					} else {
						// 多个文本字段时，存入 _document 字段
						fields["_document"] = text
					}
				}
			}

			// 从 metadata 还原其他字段
			if i < len(metas) && metas[i] != nil {
				meta := metas[i]
				for _, key := range metaKeys(meta) {
					if val, ok := meta.GetRaw(key); ok {
						mv, isMv := val.(chromav2.MetadataValue)
						if isMv {
							if rawVal, ok := mv.GetRaw(); ok {
								fields[key] = rawVal
							}
						} else {
							fields[key] = val
						}
					}
				}
			}

			searchResults = append(searchResults, VectorSearchResult{
				Score:  score,
				Fields: fields,
			})
		}
	}

	logger.Info(logComponent).Str("collection_name", collectionName).
		Int("result_count", len(searchResults)).Msg("ChromaDB 向量搜索完成")
	return searchResults, nil
}

// DeleteDocsByIDs 按 ID 删除文档。
//
// 对应 Python: ChromaVectorStore.delete_docs_by_ids(collection_name, ids)
func (s *ChromaVectorStore) DeleteDocsByIDs(ctx context.Context, collectionName string, ids []string, opts ...Option) error {
	if len(ids) == 0 {
		return nil
	}

	collection, err := s.getCollection(ctx, collectionName)
	if err != nil {
		return err
	}

	chromaIDs := make([]chromav2.DocumentID, len(ids))
	for i, id := range ids {
		chromaIDs[i] = chromav2.DocumentID(id)
	}

	if err := collection.Delete(ctx, chromav2.WithIDs(chromaIDs...)); err != nil {
		logger.Error(logComponent).Err(err).Str("collection_name", collectionName).
			Int("id_count", len(ids)).Msg("按 ID 删除文档失败")
		return err
	}

	logger.Info(logComponent).Str("collection_name", collectionName).
		Int("id_count", len(ids)).Msg("成功按 ID 删除文档")
	return nil
}

// DeleteDocsByFilters 按标量字段过滤条件删除文档。
//
// 对应 Python: ChromaVectorStore.delete_docs_by_filters(collection_name, filters)
func (s *ChromaVectorStore) DeleteDocsByFilters(ctx context.Context, collectionName string, filters map[string]any, opts ...Option) error {
	if len(filters) == 0 {
		return nil
	}

	collection, err := s.getCollection(ctx, collectionName)
	if err != nil {
		return err
	}

	whereClause := buildChromaWhereFilter(filters)
	if whereClause == nil {
		return nil
	}

	if err := collection.Delete(ctx, chromav2.WithWhere(whereClause)); err != nil {
		logger.Error(logComponent).Err(err).Str("collection_name", collectionName).
			Interface("filters", filters).Msg("按过滤条件删除文档失败")
		return err
	}

	logger.Info(logComponent).Str("collection_name", collectionName).
		Interface("filters", filters).Msg("成功按过滤条件删除文档")
	return nil
}

// ListCollectionNames 列出所有集合名称。
//
// 对应 Python: ChromaVectorStore.list_collection_names()
func (s *ChromaVectorStore) ListCollectionNames(ctx context.Context) ([]string, error) {
	c, err := s.getClient()
	if err != nil {
		return nil, err
	}

	collections, err := c.ListCollections(ctx)
	if err != nil {
		return nil, err
	}

	names := make([]string, 0, len(collections))
	for _, coll := range collections {
		names = append(names, coll.Name())
	}
	return names, nil
}

// UpdateSchema 执行 schema 迁移操作。
// ⤵️ 预留：实际迁移逻辑待 7.22/7.23 实现后回填。
//
// 对应 Python: ChromaVectorStore.update_schema(collection_name, operations)
func (s *ChromaVectorStore) UpdateSchema(ctx context.Context, collectionName string, operations []any, opts ...Option) error {
	// TODO: ⤵️ 回填，待 7.22/7.23 实现后补全
	logger.Warn(logComponent).Str("collection_name", collectionName).Msg("UpdateSchema 尚未实现，待 7.22/7.23 回填")
	return exception.BuildError(exception.StatusStoreVectorSchemaInvalid,
		exception.WithParam("error_msg", "UpdateSchema is not yet implemented, pending 7.22/7.23"),
	)
}

// UpdateCollectionMetadata 更新集合元数据。
// 同时更新 ChromaDB 集合的 CollectionMetadata 和本地缓存。
//
// 对应 Python: ChromaVectorStore.update_collection_metadata(collection_name, metadata)
func (s *ChromaVectorStore) UpdateCollectionMetadata(ctx context.Context, collectionName string, metadata map[string]any, opts ...Option) error {
	if len(metadata) == 0 {
		return nil
	}

	collection, err := s.getCollection(ctx, collectionName)
	if err != nil {
		return err
	}

	// 构建新的 CollectionMetadata
	newMetaMap := make(map[string]interface{})
	// 保留原有 metadata
	existingMeta := collection.Metadata()
	if existingMeta != nil {
		for _, key := range existingMeta.Keys() {
			if val, ok := existingMeta.GetRaw(key); ok {
				newMetaMap[key] = val
			}
		}
	}
	// 合并新 metadata
	for k, v := range metadata {
		newMetaMap[k] = v
	}

	newCollectionMeta := chromav2.NewMetadataFromMap(newMetaMap)

	if err := collection.ModifyMetadata(ctx, newCollectionMeta); err != nil {
		logger.Error(logComponent).Err(err).Str("collection_name", collectionName).Msg("更新集合元数据失败")
		return err
	}

	// 更新本地字段映射缓存中的 distance_metric
	if dm, ok := metadata[chromaMetadataKeyDistanceMetric]; ok {
		if dmStr, ok := dm.(string); ok {
			s.mu.Lock()
			if fm, ok := s.fieldMappingCache[collectionName]; ok {
				fm.DistanceMetric = dmStr
			}
			s.mu.Unlock()
		}
	}

	logger.Info(logComponent).Str("collection_name", collectionName).
		Interface("metadata", metadata).Msg("成功更新集合元数据")
	return nil
}

// GetCollectionMetadata 获取集合元数据。
//
// 对应 Python: ChromaVectorStore.get_collection_metadata(collection_name)
func (s *ChromaVectorStore) GetCollectionMetadata(ctx context.Context, collectionName string, opts ...Option) (map[string]any, error) {
	collection, err := s.getCollection(ctx, collectionName)
	if err != nil {
		return nil, err
	}

	meta := collection.Metadata()
	if meta == nil {
		return map[string]any{}, nil
	}

	result := make(map[string]any)
	for _, key := range meta.Keys() {
		if val, ok := meta.GetRaw(key); ok {
			mv, isMv := val.(chromav2.MetadataValue)
			if isMv {
				if rawVal, ok := mv.GetRaw(); ok {
					result[key] = rawVal
				}
			} else {
				result[key] = val
			}
		}
	}

	return result, nil
}

// GetAllDocuments 获取集合中的所有文档。
// 调用 collection.Get 并按 fieldMapping 映射回用户字段。
//
// 对应 Python: ChromaVectorStore.get_all_documents(collection_name)
func (s *ChromaVectorStore) GetAllDocuments(ctx context.Context, collectionName string) ([]map[string]any, error) {
	collection, err := s.getCollection(ctx, collectionName)
	if err != nil {
		return nil, err
	}

	fm, err := s.getFieldMapping(ctx, collectionName)
	if err != nil {
		return nil, err
	}

	getResult, err := collection.Get(ctx,
		chromav2.WithInclude(chromav2.IncludeDocuments, chromav2.IncludeMetadatas, chromav2.IncludeEmbeddings),
	)
	if err != nil {
		logger.Error(logComponent).Err(err).Str("collection_name", collectionName).Msg("获取所有文档失败")
		return nil, err
	}

	ids := getResult.GetIDs()
	docs := getResult.GetDocuments()
	metas := getResult.GetMetadatas()
	embList := getResult.GetEmbeddings()

	result := make([]map[string]any, 0, len(ids))

	for i, id := range ids {
		doc := make(map[string]any)
		doc[fm.PKField] = string(id)

		// 还原文本字段
		if i < len(docs) && docs[i] != nil {
			text := docs[i].ContentString()
			if text != "" && len(fm.TextFieldMapping) > 0 {
				if len(fm.TextFieldMapping) == 1 {
					for textField := range fm.TextFieldMapping {
						doc[textField] = text
					}
				} else {
					doc["_document"] = text
				}
			}
		}

		// 还原 metadata 字段
		if i < len(metas) && metas[i] != nil {
			meta := metas[i]
			for _, key := range metaKeys(meta) {
				if val, ok := meta.GetRaw(key); ok {
					mv, isMv := val.(chromav2.MetadataValue)
					if isMv {
						if rawVal, ok := mv.GetRaw(); ok {
							doc[key] = rawVal
						}
					} else {
						doc[key] = val
					}
				}
			}
		}

		// 还原向量字段
		if i < len(embList) && embList[i] != nil {
			doc[fm.VectorField] = embList[i].ContentAsFloat32()
		}

		result = append(result, doc)
	}

	return result, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// defaultChromaCreateClient 默认的客户端创建函数，使用 chroma-go PersistentClient。
func defaultChromaCreateClient(persistPath string) (chromav2.Client, error) {
	opts := []chromav2.PersistentClientOption{
		chromav2.WithPersistentPath(persistPath),
		chromav2.WithPersistentLibraryAutoDownload(true),
	}
	return chromav2.NewPersistentClient(opts...)
}

// getClient 惰性获取或创建 ChromaDB 客户端。
func (s *ChromaVectorStore) getClient() (chromav2.Client, error) {
	s.mu.RLock()
	if s.client != nil {
		s.mu.RUnlock()
		return s.client, nil
	}
	s.mu.RUnlock()

	s.mu.Lock()
	defer s.mu.Unlock()
	// 双重检查
	if s.client != nil {
		return s.client, nil
	}

	c, err := s.createClient(s.persistPath)
	if err != nil {
		logger.Error(logComponent).Err(err).Str("persist_path", s.persistPath).Msg("连接 ChromaDB 失败")
		return nil, exception.BuildError(exception.StatusStoreVectorCollectionNotFound,
			exception.WithParam("error_msg", fmt.Sprintf("failed to connect to ChromaDB: %v", err)),
		)
	}
	s.client = c
	logger.Info(logComponent).Str("persist_path", s.persistPath).Msg("成功连接 ChromaDB")
	return s.client, nil
}

// getCollection 获取集合对象，优先从缓存获取，缓存未命中则从 ChromaDB 获取。
func (s *ChromaVectorStore) getCollection(ctx context.Context, collectionName string) (chromav2.Collection, error) {
	s.mu.RLock()
	if coll, ok := s.collectionCache[collectionName]; ok {
		s.mu.RUnlock()
		return coll, nil
	}
	s.mu.RUnlock()

	c, err := s.getClient()
	if err != nil {
		return nil, err
	}

	collection, err := c.GetCollection(ctx, collectionName)
	if err != nil {
		logger.Error(logComponent).Err(err).Str("collection_name", collectionName).Msg("获取 ChromaDB 集合失败")
		return nil, exception.BuildError(exception.StatusStoreVectorCollectionNotFound,
			exception.WithParam("collection_name", collectionName),
		)
	}

	s.mu.Lock()
	s.collectionCache[collectionName] = collection
	s.mu.Unlock()

	return collection, nil
}

// getFieldMapping 获取字段映射，优先从缓存获取，缓存未命中则从 CollectionMetadata 恢复。
func (s *ChromaVectorStore) getFieldMapping(ctx context.Context, collectionName string) (*chromaFieldMapping, error) {
	s.mu.RLock()
	if fm, ok := s.fieldMappingCache[collectionName]; ok {
		s.mu.RUnlock()
		return fm, nil
	}
	s.mu.RUnlock()

	collection, err := s.getCollection(ctx, collectionName)
	if err != nil {
		return nil, err
	}

	meta := collection.Metadata()
	if meta == nil {
		return nil, exception.BuildError(exception.StatusStoreVectorSchemaInvalid,
			exception.WithParam("error_msg", fmt.Sprintf("collection %s has no metadata", collectionName)),
		)
	}

	// 从 CollectionMetadata 恢复字段映射
	fmStr, ok := meta.GetString(chromaMetadataKeyFieldMapping)
	if !ok || fmStr == "" {
		return nil, exception.BuildError(exception.StatusStoreVectorSchemaInvalid,
			exception.WithParam("error_msg", fmt.Sprintf("collection %s has no field_mapping metadata", collectionName)),
		)
	}

	var fm chromaFieldMapping
	if err := json.Unmarshal([]byte(fmStr), &fm); err != nil {
		return nil, exception.BuildError(exception.StatusStoreVectorSchemaInvalid,
			exception.WithParam("error_msg", fmt.Sprintf("failed to unmarshal field mapping: %v", err)),
		)
	}

	s.mu.Lock()
	s.fieldMappingCache[collectionName] = &fm
	s.mu.Unlock()

	return &fm, nil
}

// inferDefaultSchema 从字段映射推断默认 Schema。
func (s *ChromaVectorStore) inferDefaultSchema(collectionName string) (*CollectionSchema, error) {
	s.mu.RLock()
	fm, ok := s.fieldMappingCache[collectionName]
	s.mu.RUnlock()
	if !ok || fm == nil {
		return nil, exception.BuildError(exception.StatusStoreVectorCollectionNotFound,
			exception.WithParam("collection_name", collectionName),
		)
	}

	schema, err := NewCollectionSchema()
	if err != nil {
		return nil, err
	}

	pkField, err := NewFieldSchema(fm.PKField, VectorDataTypeVarchar, WithPrimary())
	if err != nil {
		return nil, err
	}
	if _, err := schema.AddField(pkField); err != nil {
		return nil, err
	}

	vecField, err := NewFieldSchema(fm.VectorField, VectorDataTypeFloatVector, WithDim(0))
	if err != nil {
		return nil, err
	}
	if _, err := schema.AddField(vecField); err != nil {
		return nil, err
	}

	return schema, nil
}

// mapToChromaDistanceMetric 将字符串距离度量映射为 ChromaDB SDK 的 DistanceMetric。
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

// getScoreConverter 根据距离度量返回分数转换函数。
func getScoreConverter(distanceMetric string) func(float64) float64 {
	switch strings.ToUpper(distanceMetric) {
	case "COSINE":
		return ConvertCosineDistance
	case "L2":
		return func(raw float64) float64 { return ConvertL2Squared(raw, 4.0) }
	case "IP":
		return ConvertIPDistance
	default:
		return ConvertCosineDistance
	}
}

// buildChromaWhereFilter 从过滤条件字典构建 ChromaDB WhereFilter。
func buildChromaWhereFilter(filters map[string]any) chromav2.WhereFilter {
	if len(filters) == 0 {
		return nil
	}

	var clauses []chromav2.WhereClause
	for key, value := range filters {
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

// metaKeys 从 DocumentMetadata 获取所有键。
func metaKeys(meta chromav2.DocumentMetadata) []string {
	if impl, ok := meta.(*chromav2.DocumentMetadataImpl); ok {
		return impl.Keys()
	}
	// fallback：尝试通过常见键名访问
	return nil
}

// toFloat32Slice 将任意类型的向量转换为 embeddings.Embedding 接口。
func toFloat32Slice(v any) embeddings.Embedding {
	switch val := v.(type) {
	case []float32:
		cp := make([]float32, len(val))
		copy(cp, val)
		return &embeddings.Float32Embedding{ArrayOfFloat32: &cp}
	case []float64:
		result := make([]float32, len(val))
		for i, f := range val {
			result[i] = float32(f)
		}
		return &embeddings.Float32Embedding{ArrayOfFloat32: &result}
	case []any:
		result := make([]float32, 0, len(val))
		for _, item := range val {
			switch f := item.(type) {
			case float64:
				result = append(result, float32(f))
			case float32:
				result = append(result, f)
			case int:
				result = append(result, float32(f))
			}
		}
		if len(result) > 0 {
			return &embeddings.Float32Embedding{ArrayOfFloat32: &result}
		}
	}
	return nil
}
