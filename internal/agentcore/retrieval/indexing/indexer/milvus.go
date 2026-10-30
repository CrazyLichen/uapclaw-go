package indexer

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/milvus-io/milvus/client/v2/entity"
	"github.com/milvus-io/milvus/client/v2/index"
	milvusclient "github.com/milvus-io/milvus/client/v2/milvusclient"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/embedding"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/vector_fields"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/utils"
	vector_store "github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/vector_store"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// MilvusIndexer Milvus 文档索引管理器。
//
// 实现 Indexer 接口，负责将 TextChunk 写入 Milvus 向量库：
// ensureCollection（含 BM25 Function/dense 索引/INVERTED 标量索引）、
// BuildIndex（查重→嵌入→写入）、UpdateIndex（先删后建）、
// DeleteIndex（filter 删除）、IndexExists、GetIndexInfo。
//
// Python: retrieval/indexing/indexer/milvus_indexer.py (MilvusIndexer)
type MilvusIndexer struct {
	// milvusURI Milvus 服务地址
	milvusURI string
	// milvusToken Milvus 认证 token
	milvusToken string
	// textField 文本字段名，默认 "content"
	textField string
	// vectorField 向量字段配置
	vectorField vector_fields.MilvusVectorFieldConfig
	// sparseVectorField 稀疏向量字段名，默认 "sparse_vector"
	sparseVectorField string
	// metadataField 元数据字段名，默认 "metadata"
	metadataField string
	// docIDField 文档 ID 字段名，默认 "document_id"
	docIDField string
	// databaseName 数据库名称
	databaseName string
	// constructConfig 构建阶段配置（AUTO 时仅含 metric_type）。
	// S-02: dense 索引建库唯一真源——buildDenseIndex 消费此字典透传给服务器，
	// 与 CheckVectorField 的 CheckConfigsMatching 校验期望同源。
	constructConfig map[string]any
	// searchConfig 搜索阶段配置
	searchConfig map[string]any
	// distanceMetric 归一化距离度量：IP/L2/COSINE
	distanceMetric string
	// docIndexCallback 嵌入进度回调
	docIndexCallback common.DocIndexCallback
	// milvusAlias 连接别名
	milvusAlias string
	// client Milvus 客户端（构造时创建，全方法共享）
	client vector_store.MilvusClient
	// createClientFunc 客户端创建函数（依赖注入）
	createClientFunc func(ctx context.Context, databaseName, uri, token string) (vector_store.MilvusClient, error)
}

// MilvusIndexerOption MilvusIndexer 的可选参数函数。
type MilvusIndexerOption func(*MilvusIndexer)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// milvusBM25FunctionName BM25 函数名，对齐 Python "text_bm25_emb"
	milvusBM25FunctionName = "text_bm25_emb"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// 编译期接口断言：MilvusIndexer 实现 Indexer 接口
var _ Indexer = (*MilvusIndexer)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewMilvusIndexer 创建 MilvusIndexer 实例。
//
// 对齐 Python MilvusIndexer.__init__：
//   - vectorField 字符串自动转 MilvusAUTO（Go 用构造函数显式转换，默认值即 MilvusAUTO）
//   - 距离度量归一化 dot→ip / euclidean→l2 / 大写
//   - constructConfig（AUTO → {} + metric_type；否则 ToConstructDict + metric_type）
//   - searchConfig 初始化
//   - milvusAlias 生成
//   - client 创建
func NewMilvusIndexer(
	config common.VectorStoreConfig,
	milvusURI string,
	milvusToken string,
	opts ...MilvusIndexerOption,
) (*MilvusIndexer, error) {
	idx := &MilvusIndexer{
		milvusURI:         milvusURI,
		milvusToken:       milvusToken,
		textField:         "content",
		vectorField:       vector_fields.NewMilvusAUTO("embedding"),
		sparseVectorField: "sparse_vector",
		metadataField:     "metadata",
		docIDField:        "document_id",
		databaseName:      config.DatabaseName,
		createClientFunc:  vector_store.DefaultMilvusCreateClient,
	}

	for _, opt := range opts {
		opt(idx)
	}

	// 对齐 Python: if isinstance(vector_field, str) → MilvusAUTO（默认值已处理）

	// 对齐 Python: self._distance_metric = ...replace("dot","ip").replace("euclidean","l2").upper()
	idx.distanceMetric = strings.ToUpper(
		strings.ReplaceAll(
			strings.ReplaceAll(string(config.DistanceMetric), "dot", "ip"),
			"euclidean", "l2",
		),
	)

	// 对齐 Python: if self.vector_field.index_type == "auto": {} else to_dict("construct")
	if idx.vectorField.IndexType() == vector_fields.IndexTypeAUTO {
		idx.constructConfig = map[string]any{}
	} else {
		idx.constructConfig = idx.vectorField.ToConstructDict()
	}
	// 对齐 Python: self._construct_config["metric_type"] = self._distance_metric
	idx.constructConfig["metric_type"] = idx.distanceMetric

	// 对齐 Python: self._search_config = self.vector_field.to_dict(stage="search")
	idx.searchConfig = idx.vectorField.ToSearchDict()

	// 对齐 Python: self.milvus_alias = create_milvus_alias(milvus_alias, uri=..., token=...)
	if idx.milvusAlias == "" {
		idx.milvusAlias = utils.CreateMilvusAlias("", milvusURI, milvusToken)
	}

	// 对齐 Python: self._client = MilvusVectorStore.create_client(...)
	if idx.client == nil {
		client, err := idx.createClientFunc(context.Background(), idx.databaseName, idx.milvusURI, idx.milvusToken)
		if err != nil {
			return nil, exception.BuildError(
				exception.StatusRetrievalVectorStoreProviderInvalid,
				exception.WithParam("error_msg", fmt.Sprintf("创建 Milvus 客户端失败: %s", err)),
				exception.WithCause(err),
			)
		}
		idx.client = client
	}

	return idx, nil
}

// WithMilvusIndexerClient 注入 Milvus 客户端（测试用，跳过默认创建）。
func WithMilvusIndexerClient(c vector_store.MilvusClient) MilvusIndexerOption {
	return func(idx *MilvusIndexer) { idx.client = c }
}

// WithMilvusIndexerCreateClient 注入客户端创建函数。
func WithMilvusIndexerCreateClient(f func(ctx context.Context, databaseName, uri, token string) (vector_store.MilvusClient, error)) MilvusIndexerOption {
	return func(idx *MilvusIndexer) { idx.createClientFunc = f }
}

// WithMilvusIndexerTextField 设置文本字段名。
func WithMilvusIndexerTextField(field string) MilvusIndexerOption {
	return func(idx *MilvusIndexer) { idx.textField = field }
}

// WithMilvusIndexerVectorField 设置向量字段配置。
func WithMilvusIndexerVectorField(vf vector_fields.MilvusVectorFieldConfig) MilvusIndexerOption {
	return func(idx *MilvusIndexer) { idx.vectorField = vf }
}

// WithMilvusIndexerSparseVectorField 设置稀疏向量字段名。
func WithMilvusIndexerSparseVectorField(field string) MilvusIndexerOption {
	return func(idx *MilvusIndexer) { idx.sparseVectorField = field }
}

// WithMilvusIndexerMetadataField 设置元数据字段名。
func WithMilvusIndexerMetadataField(field string) MilvusIndexerOption {
	return func(idx *MilvusIndexer) { idx.metadataField = field }
}

// WithMilvusIndexerDocIDField 设置文档 ID 字段名。
func WithMilvusIndexerDocIDField(field string) MilvusIndexerOption {
	return func(idx *MilvusIndexer) { idx.docIDField = field }
}

// WithMilvusIndexerDocIndexCallback 设置嵌入进度回调。
func WithMilvusIndexerDocIndexCallback(cb common.DocIndexCallback) MilvusIndexerOption {
	return func(idx *MilvusIndexer) { idx.docIndexCallback = cb }
}

// WithMilvusIndexerAlias 设置连接别名。
func WithMilvusIndexerAlias(alias string) MilvusIndexerOption {
	return func(idx *MilvusIndexer) { idx.milvusAlias = alias }
}

// Client 返回底层 Milvus 客户端。
// 对齐 Python client property。
func (mi *MilvusIndexer) Client() vector_store.MilvusClient { return mi.client }

// DistanceMetric 返回归一化距离度量。
// 对齐 Python distance_metric property。
func (mi *MilvusIndexer) DistanceMetric() string { return mi.distanceMetric }

// DatabaseName 返回数据库名称（13.26 validate_index 消费）。
func (mi *MilvusIndexer) DatabaseName() string { return mi.databaseName }

// TextField 返回文本字段名（13.26 validate_index 消费）。
func (mi *MilvusIndexer) TextField() string { return mi.textField }

// VectorField 返回向量字段配置（13.26 validate_index 消费，any 装箱联合类型）。
func (mi *MilvusIndexer) VectorField() any { return mi.vectorField }

// SparseVectorField 返回稀疏向量字段名（13.26 validate_index 消费）。
func (mi *MilvusIndexer) SparseVectorField() string { return mi.sparseVectorField }

// MetadataField 返回元数据字段名（13.26 validate_index 消费）。
func (mi *MilvusIndexer) MetadataField() string { return mi.metadataField }

// DocIDField 返回文档 ID 字段名（13.26 validate_index 消费）。
func (mi *MilvusIndexer) DocIDField() string { return mi.docIDField }

// IndexExists 检查索引是否存在。
//
// 对齐 Python MilvusIndexer.index_exists：has_collection，异常时返回 False。
func (mi *MilvusIndexer) IndexExists(ctx context.Context, indexName string) (bool, error) {
	ok, err := mi.client.HasCollection(ctx, milvusclient.NewHasCollectionOption(indexName))
	if err != nil {
		// 对齐 Python: except Exception as e: logger.error(...); return False
		logger.Error(logComponent).
			Str("index_name", indexName).
			Err(err).
			Msg("failed to check index existence")
		return false, nil
	}
	return ok, nil
}

// DeleteIndex 删除索引。
//
// 对齐 Python MilvusIndexer.delete_index：
// filter_expr = 'document_id == "xxx"'，delete 后按 delete_count 判断。
func (mi *MilvusIndexer) DeleteIndex(ctx context.Context, docID string, indexName string) (bool, error) {
	// 对齐 Python: filter_expr = f'{self.doc_id_field} == "{doc_id}"'
	filterExpr := fmt.Sprintf("%s == %q", mi.docIDField, docID)

	result, err := mi.client.Delete(ctx, milvusclient.NewDeleteOption(indexName).WithExpr(filterExpr))
	if err != nil {
		// 对齐 Python: except MilvusException / except Exception → logger.error + return False
		logger.Error(logComponent).
			Str("event_type", "INDEXING_ERROR").
			Str("doc_id", docID).
			Str("index_name", indexName).
			Err(err).
			Msg("failed to delete index entries")
		return false, nil
	}

	// 对齐 Python: logger.info(f"Deleted {delete_count} entries for doc_id={doc_id}")
	logger.Info(logComponent).
		Str("doc_id", docID).
		Int64("delete_count", result.DeleteCount).
		Msg("deleted doc entries")

	return result.DeleteCount > 0, nil
}

// GetIndexInfo 获取索引信息。
//
// 对齐 Python MilvusIndexer.get_index_info：
//   - 不存在返回 {"exists": False}
//   - get_collection_stats 获取 row_count（避免 query 分页错误）
//   - describe_collection 获取字段信息
//   - 任何异常返回 {"exists": False, "error": ...}
func (mi *MilvusIndexer) GetIndexInfo(ctx context.Context, indexName string) (map[string]any, error) {
	exists, err := mi.IndexExists(ctx, indexName)
	if err != nil || !exists {
		return map[string]any{"exists": false}, nil
	}

	// 对齐 Python: stats = get_collection_stats(...)；row_count 可能是字符串
	stats, err := mi.client.GetCollectionStats(ctx, milvusclient.NewGetCollectionStatsOption(indexName))
	if err != nil {
		logger.Error(logComponent).
			Str("index_name", indexName).
			Err(err).
			Msg("failed to get collection stats")
		return map[string]any{"exists": false, "error": err.Error()}, nil
	}
	rowCount := 0
	if rc, ok := stats["row_count"]; ok {
		if n, parseErr := strconv.Atoi(rc); parseErr == nil {
			rowCount = n
		}
	}

	// 对齐 Python: collection_info = describe_collection(...)
	collectionInfo, err := mi.client.DescribeCollection(ctx, milvusclient.NewDescribeCollectionOption(indexName))
	if err != nil {
		logger.Error(logComponent).
			Str("index_name", indexName).
			Err(err).
			Msg("failed to describe collection")
		return map[string]any{"exists": false, "error": err.Error()}, nil
	}

	return map[string]any{
		"exists":          true,
		"collection_name": indexName,
		"info":            collectionInfo,
		"count":           rowCount,
	}, nil
}

// Close 关闭索引管理器。
//
// 对齐 Python MilvusIndexer.close。关闭失败已在此 Warn 记录，故返回 nil。
func (mi *MilvusIndexer) Close() error {
	if mi.client == nil {
		return nil
	}
	if err := mi.client.Close(context.Background()); err != nil {
		// 对齐 Python: logger.warning(f"Failed to close Milvus client: {e}")
		logger.Warn(logComponent).Err(err).Msg("failed to close Milvus client")
	}
	return nil
}

// BuildIndex 构建索引。ensureCollection → 查重 → 嵌入 → 转换 → 写入。
//
// 对齐 Python MilvusIndexer.build_index。
// 5 步流程：
//  1. _ensure_collection 确保集合存在（自动建表）
//  2. query 检查 doc_id 重复（重复抛 ADD_DOC_RUNTIME_ERROR）
//  3. vector/hybrid 类型计算 embedding
//  4. 转换 TextChunk → Milvus 字段（chunk_id/doc_id/content/metadata/embedding）
//  5. MilvusVectorStore.add 写入
func (mi *MilvusIndexer) BuildIndex(
	ctx context.Context,
	chunks []common.TextChunk,
	config common.IndexConfig,
	embedModel embedding.BaseEmbedding,
	opts ...IndexOption,
) (bool, error) {
	indexOpts := NewIndexOptions(opts...)
	collectionName := config.IndexName

	// ─── 步骤 1：确保集合存在 ───
	// 对齐 Python: await self._ensure_collection(collection_name=..., config=..., embed_model=...)
	if err := mi.ensureCollection(ctx, collectionName, config, embedModel); err != nil {
		return false, err
	}

	// ─── 步骤 2：查重 doc_id ───
	// 对齐 Python: all_doc_ids = '["' + '","'.join({chunk.doc_id for chunk in chunks}) + '"]'
	docIDSet := make(map[string]struct{})
	for _, chunk := range chunks {
		if chunk.DocID != "" {
			docIDSet[chunk.DocID] = struct{}{}
		}
	}
	if len(docIDSet) > 0 {
		quoted := make([]string, 0, len(docIDSet))
		for docID := range docIDSet {
			quoted = append(quoted, fmt.Sprintf("%q", docID))
		}
		filterExpr := fmt.Sprintf("%s in [%s]", mi.docIDField, strings.Join(quoted, ","))
		// 对齐 Python: results = self._client.query(collection_name, filter=..., output_fields=[doc_id_field])
		resultSet, err := mi.client.Query(ctx, milvusclient.NewQueryOption(collectionName).
			WithFilter(filterExpr).
			WithOutputFields(mi.docIDField))
		if err != nil {
			return false, exception.BuildError(
				exception.StatusRetrievalIndexingAddDocRuntimeError,
				exception.WithParam("error_msg", err.Error()),
				exception.WithCause(err),
			)
		}
		// 对齐 Python: duplicate_doc_ids = sorted({result.get(doc_id_field) for result in results} - {None, ""})
		dupSet := make(map[string]struct{})
		if col := resultSet.GetColumn(mi.docIDField); col != nil {
			for j := 0; j < resultSet.ResultCount; j++ {
				if v, getErr := col.GetAsString(j); getErr == nil && v != "" {
					dupSet[v] = struct{}{}
				}
			}
		}
		if len(dupSet) > 0 {
			duplicateDocIDs := make([]string, 0, len(dupSet))
			for docID := range dupSet {
				duplicateDocIDs = append(duplicateDocIDs, docID)
			}
			sort.Strings(duplicateDocIDs)
			return false, exception.BuildError(
				exception.StatusRetrievalIndexingAddDocRuntimeError,
				exception.WithParam("error_msg",
					fmt.Sprintf("some documents with same doc_id already exist, if they are the same documents, "+
						"please consider updating instead of adding. duplicate_doc_ids=%v", duplicateDocIDs)),
			)
		}
	}

	// ─── 步骤 3：嵌入 ───
	// 对齐 Python: if config.index_type in ("vector", "hybrid"):
	if config.IndexType == common.IndexTypeVector || config.IndexType == common.IndexTypeHybrid {
		if embedModel == nil {
			// 对齐 Python: EMBED_MODEL_NOT_FOUND
			return false, exception.BuildError(
				exception.StatusRetrievalIndexingEmbedModelNotFound,
				exception.WithParam("error_msg", "embed_model is required for vector/hybrid index type"),
			)
		}
		embedOpts := []ChunkEmbedOption{
			WithUseCaptionForImages(config.UseCaptionForImages),
		}
		if mi.docIndexCallback != nil {
			embedOpts = append(embedOpts, WithDocIndexCallback(mi.docIndexCallback))
		}
		// 对齐 Python: await compute_chunk_embeddings(chunks, embed_model, doc_index_callback=..., use_caption_for_images=...)
		if err := ComputeChunkEmbeddings(ctx, chunks, embedModel, embedOpts...); err != nil {
			return false, err
		}
	}

	// ─── 步骤 4：转换 TextChunk → Milvus 字段 ───
	// 对齐 Python: 避免写入 schema 未定义的 id_ 字段
	databaseName, ok := indexOpts.Extra["database_name"].(string)
	if !ok {
		// 防御性日志：Python kwargs.pop 传错类型时同样静默退化，此处补 warn 便于排查
		logger.Warn(logComponent).Str("method", "BuildIndex").
			Str("database_name_type", fmt.Sprintf("%T", indexOpts.Extra["database_name"])).
			Msg("Extra[database_name] is not a string, fallback to empty")
	}
	data := make([]map[string]any, 0, len(chunks))
	for _, chunk := range chunks {
		meta := chunk.Metadata
		if meta == nil {
			meta = make(map[string]any)
		}
		// 对齐 Python: chunk_id = meta.get("chunk_id", chunk.id_)
		chunkID := chunk.ID
		if cid, ok := meta["chunk_id"].(string); ok && cid != "" {
			chunkID = cid
		}
		item := map[string]any{
			"chunk_id":       chunkID,
			mi.docIDField:    chunk.DocID,
			mi.textField:     chunk.Text,
			mi.metadataField: meta,
		}
		if chunk.Embedding != nil {
			item[mi.vectorField.VectorFieldName()] = chunk.Embedding
		}
		data = append(data, item)
	}

	// ─── 步骤 5：写入 ───
	// 对齐 Python: vector_store = MilvusVectorStore(config=..., milvus_uri=..., milvus_token=..., milvus_alias=vs_alias)
	vectorStoreConfig := common.VectorStoreConfig{
		StoreProvider:  common.StoreTypeMilvus,
		CollectionName: collectionName,
		DatabaseName:   databaseName,
	}
	// 对齐 Python: vs_alias = f"{self.milvus_alias}_{collection_name}" if self.milvus_alias else None
	// （alias 仅用于 Python pymilvus 连接复用；Go 复用 indexer 持有的 client，语义等价）
	vectorStore, err := vector_store.NewMilvusVectorStore(vectorStoreConfig, mi.milvusURI, mi.milvusToken,
		mi.textField, mi.vectorField, mi.sparseVectorField, mi.metadataField, mi.docIDField,
		vector_store.WithMilvusClientOption(mi.client))
	if err != nil {
		return false, err
	}

	// 对齐 Python: await vector_store.add(data=data)
	if err := vectorStore.Add(ctx, data); err != nil {
		return false, err
	}

	// 对齐 Python: logger.info(f"Successfully built index {collection_name} with {len(chunks)} chunks")
	logger.Info(logComponent).
		Str("index_name", collectionName).
		Int("chunk_count", len(chunks)).
		Msg("successfully built index")

	return true, nil
}

// UpdateIndex 更新索引。先删后 flush 后重建。
//
// 对齐 Python MilvusIndexer.update_index：
// delete_index → flush（Milvus 最终一致性，使删除可见）→ build_index。
// 异常时 logger.error 后返回 False（对齐 Python except 分支）。
func (mi *MilvusIndexer) UpdateIndex(
	ctx context.Context,
	chunks []common.TextChunk,
	docID string,
	config common.IndexConfig,
	embedModel embedding.BaseEmbedding,
	opts ...IndexOption,
) (bool, error) {
	// 对齐 Python: await self.delete_index(doc_id, config.index_name)
	if _, err := mi.DeleteIndex(ctx, docID, config.IndexName); err != nil {
		logger.Error(logComponent).
			Str("event_type", "INDEXING_ERROR").
			Str("doc_id", docID).
			Str("index_name", config.IndexName).
			Err(err).
			Msg("failed to update index")
		return false, err
	}

	// 对齐 Python: self._client.flush(collection_name=config.index_name)
	if err := mi.client.Flush(ctx, milvusclient.NewFlushOption(config.IndexName)); err != nil {
		logger.Error(logComponent).
			Str("event_type", "INDEXING_ERROR").
			Str("doc_id", docID).
			Str("index_name", config.IndexName).
			Err(err).
			Msg("failed to flush after update index")
		return false, err
	}

	// 对齐 Python: return await self.build_index(chunks, config, embed_model, **kwargs)
	return mi.BuildIndex(ctx, chunks, config, embedModel, opts...)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// ensureCollection 确保集合存在，不存在则创建。
//
// 对齐 Python MilvusIndexer._ensure_collection：
//   - 已存在直接返回
//   - schema：document_id(VARCHAR 256+INVERTED) + pk(INT64 PK auto_id) + chunk_id(VARCHAR 256+INVERTED)
//   - content(VARCHAR 65535, BM25 时 enable_analyzer+jieba)
//   - BM25/hybrid：sparse_vector(SPARSE_FLOAT_VECTOR) + BM25 Function + SPARSE_INVERTED_INDEX
//   - vector/hybrid：embedding(FLOAT_VECTOR dim) + dense 索引
//   - metadata(JSON)
//   - dimension 推断：embedModel.Dimension → EmbedQuery("X") → 报错
func (mi *MilvusIndexer) ensureCollection(ctx context.Context, collectionName string, config common.IndexConfig, embedModel embedding.BaseEmbedding) error {
	exists, err := mi.IndexExists(ctx, collectionName)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}

	// 对齐 Python: schema = self._client.create_schema(auto_id=True, enable_dynamic_field=False)
	schema := entity.NewSchema().WithAutoID(true).WithDynamicFieldEnabled(false)

	var indexOpts []milvusclient.CreateIndexOption

	// 对齐 Python: document_id 字段 + INVERTED 索引（加速按文档删除/过滤）
	schema = schema.WithField(
		entity.NewField().WithName(mi.docIDField).
			WithDataType(entity.FieldTypeVarChar).
			WithMaxLength(256),
	)
	indexOpts = append(indexOpts, milvusclient.NewCreateIndexOption(collectionName, mi.docIDField, index.NewInvertedIndex()))

	// 对齐 Python: pk 主键字段（INT64, is_primary=True, auto_id=True）
	schema = schema.WithField(
		entity.NewField().WithName("pk").
			WithDataType(entity.FieldTypeInt64).
			WithIsPrimaryKey(true).
			WithIsAutoID(true),
	)

	// 对齐 Python: chunk_id 字段 + INVERTED 索引
	schema = schema.WithField(
		entity.NewField().WithName("chunk_id").
			WithDataType(entity.FieldTypeVarChar).
			WithMaxLength(256),
	)
	indexOpts = append(indexOpts, milvusclient.NewCreateIndexOption(collectionName, "chunk_id", index.NewInvertedIndex()))

	// 对齐 Python: enable_bm25 = config.index_type in ("bm25", "hybrid")
	enableBM25 := config.IndexType == common.IndexTypeBM25 || config.IndexType == common.IndexTypeHybrid

	// 对齐 Python: content 字段（BM25 时 enable_analyzer + jieba 分词器）
	contentField := entity.NewField().WithName(mi.textField).
		WithDataType(entity.FieldTypeVarChar).
		WithMaxLength(65535)
	if enableBM25 {
		contentField = contentField.WithEnableAnalyzer(true).
			WithAnalyzerParams(map[string]any{"tokenizer": "jieba"})
	}
	schema = schema.WithField(contentField)

	// 对齐 Python: BM25 稀疏向量字段 + Function + SPARSE_INVERTED_INDEX(metric_type=BM25)
	if enableBM25 {
		schema = schema.WithField(
			entity.NewField().WithName(mi.sparseVectorField).
				WithDataType(entity.FieldTypeSparseVector),
		)
		schema = schema.WithFunction(
			entity.NewFunction().WithName(milvusBM25FunctionName).
				WithType(entity.FunctionTypeBM25).
				WithInputFields(mi.textField).
				WithOutputFields(mi.sparseVectorField),
		)
		// 对齐 Python: index_type="SPARSE_INVERTED_INDEX", metric_type="BM25"
		indexOpts = append(indexOpts, milvusclient.NewCreateIndexOption(collectionName, mi.sparseVectorField, index.NewSparseInvertedIndex(entity.BM25, 0)))
	}

	// 对齐 Python: vector/hybrid 时 embedding 字段 + dense 索引
	if config.IndexType == common.IndexTypeVector || config.IndexType == common.IndexTypeHybrid {
		// 对齐 Python: dimension 推断三级防御
		dimension := 0
		if embedModel != nil {
			dimension = embedModel.Dimension()
		}
		if dimension == 0 && embedModel != nil {
			// 对齐 Python: test_embedding = await embed_model.embed_query("X")
			if testEmb, embErr := embedModel.EmbedQuery(ctx, "X"); embErr == nil {
				dimension = len(testEmb)
				// 对齐 Python: logger.debug(f"Got dimension through API call: {dimension}")
				logger.Debug(logComponent).Int("dimension", dimension).Msg("got dimension through API call")
			} else {
				// 对齐 Python: logger.warning(f"Unable to get dimension through API call: {e}")
				logger.Warn(logComponent).Err(embErr).Msg("unable to get dimension through API call")
			}
		}
		if dimension == 0 {
			return exception.BuildError(
				exception.StatusRetrievalIndexingDimensionNotFound,
				exception.WithParam("error_msg", "dimension is required for vector/hybrid index type"),
			)
		}

		schema = schema.WithField(
			entity.NewField().WithName(mi.vectorField.VectorFieldName()).
				WithDataType(entity.FieldTypeFloatVector).
				WithDim(int64(dimension)),
		)

		// 对齐 Python: index_type = "AUTOINDEX" if auto else index_type.upper() (+variant)
		denseIndex, err := mi.buildDenseIndex()
		if err != nil {
			return err
		}
		indexOpts = append(indexOpts, milvusclient.NewCreateIndexOption(collectionName, mi.vectorField.VectorFieldName(), denseIndex))
	}

	// 对齐 Python: metadata 字段（JSON）
	schema = schema.WithField(
		entity.NewField().WithName(mi.metadataField).
			WithDataType(entity.FieldTypeJSON),
	)

	// 对齐 Python: create_collection(collection_name, schema, index_params)
	createOpt := milvusclient.NewCreateCollectionOption(collectionName, schema).
		WithIndexOptions(indexOpts...)
	if err := mi.client.CreateCollection(ctx, createOpt); err != nil {
		logger.Error(logComponent).
			Str("collection_name", collectionName).
			Err(err).
			Msg("failed to create collection")
		return err
	}

	// 对齐 Python: logger.info(f"Created collection: {collection_name}")
	logger.Info(logComponent).
		Str("collection_name", collectionName).
		Msg("created collection")
	return nil
}

// buildDenseIndex 根据 constructConfig 构建 dense 索引（S-01/S-02 字典透传方案）。
//
// 对齐 Python _ensure_collection：
//
//	index_params.add_index(field_name=..., index_type=index_type, **self._construct_config)
//
// Python 将 index_type 与 _construct_config 参数字典整体透传给服务器，无类型化构造层。
// Go 用 SDK NewGenericIndex 等价实现——以 constructConfig 为建库唯一真源：
// 建库参数与 CheckVectorField 的 CheckConfigsMatching 校验期望天然同源。
// AUTO 分支保持 NewAutoIndex（constructConfig 仅含 metric_type，无参数可透传）。
func (mi *MilvusIndexer) buildDenseIndex() (index.Index, error) {
	metricType := mapMilvusMetricType(mi.distanceMetric)

	if vf, ok := mi.vectorField.(*vector_fields.MilvusAUTO); ok {
		_ = vf
		return index.NewAutoIndex(metricType), nil
	}

	// 字典透传：constructConfig 键（index_type 补充后）直接进 NewGenericIndex，
	// 服务器按 index_type 解释其余键（nlist/M/efConstruction/with_raw_data 等）。
	// future variant（T-13 回填后）的 extra_construct 并入 dict 即自动下发，无需新 case。
	params := make(map[string]string, len(mi.constructConfig)+1)
	for k, v := range mi.constructConfig {
		params[k] = fmt.Sprintf("%v", v)
	}
	// 对齐 Python: index_type = self.vector_field.index_type.upper()（auto 已在上分支处理）
	params[index.IndexTypeKey] = strings.ToUpper(mi.vectorField.IndexType().String())
	return index.NewGenericIndex("", params), nil
}

// mapMilvusMetricType 距离度量字符串转 SDK MetricType。
func mapMilvusMetricType(metric string) entity.MetricType {
	switch strings.ToUpper(metric) {
	case "L2":
		return entity.L2
	case "IP":
		return entity.IP
	default:
		return entity.COSINE
	}
}
