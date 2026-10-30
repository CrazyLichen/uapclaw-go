package indexer

import (
	"context"
	"fmt"
	"sort"
	"strings"

	chromav2 "github.com/amikos-tech/chroma-go/pkg/api/v2"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/embedding"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/vector_fields"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
	vector_store "github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/vector_store"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ChromaIndexer ChromaDB 文档索引管理器。
//
// 实现 Indexer 接口，负责将 TextChunk 写入 ChromaDB 向量库，
// 支持五种操作：BuildIndex（新建）、UpdateIndex（更新）、
// DeleteIndex（删除）、IndexExists（存在性检查）、GetIndexInfo（元信息查询）。
//
// Python: retrieval/indexing/indexer/chroma_indexer.py (ChromaIndexer)
type ChromaIndexer struct {
	// chromaPath ChromaDB 持久化路径
	chromaPath string
	// textField 文本字段名，默认 "content"
	textField string
	// vectorField 向量字段配置
	vectorField *vector_fields.ChromaVectorField
	// sparseVectorField 稀疏向量字段名，默认 "sparse_vector"
	sparseVectorField string
	// metadataField 元数据字段名，默认 "metadata"
	metadataField string
	// docIDField 文档 ID 字段名，默认 "document_id"
	docIDField string
	// distanceMetric 归一化距离度量：cosine/l2/ip
	distanceMetric string
	// constructConfig 构建阶段配置
	constructConfig map[string]any
	// searchConfig 搜索阶段配置
	searchConfig map[string]any
	// docIndexCallback 嵌入进度回调
	docIndexCallback common.DocIndexCallback
	// databaseName 数据库名称
	// M-44: 对齐 Python self.database_name = config.database_name
	databaseName string
	// client ChromaDB 持久客户端，构造时创建一次，所有操作复用
	// M-44: 对齐 Python __init__ 中 self._client = ChromaVectorStore.create_client(...)
	client chromav2.Client
}

// ChromaIndexerOption ChromaIndexer 的可选参数函数。
type ChromaIndexerOption func(*ChromaIndexer)

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewChromaIndexer 创建 ChromaIndexer 实例。
//
// 对齐 Python ChromaIndexer.__init__。
func NewChromaIndexer(
	config common.VectorStoreConfig,
	chromaPath string,
	opts ...ChromaIndexerOption,
) (*ChromaIndexer, error) {
	// 对齐 Python: if not chroma_path or not chroma_path.strip():
	//   raise build_error(RETRIEVAL_INDEXING_PATH_NOT_FOUND)
	if strings.TrimSpace(chromaPath) == "" {
		return nil, exception.BuildError(
			exception.StatusRetrievalIndexingPathNotFound,
			exception.WithParam("error_msg", "chroma_path is required and cannot be empty"),
		)
	}

	ci := &ChromaIndexer{
		chromaPath:        chromaPath,
		textField:         "content",
		sparseVectorField: "sparse_vector",
		metadataField:     "metadata",
		docIDField:        "document_id",
		vectorField:       vector_fields.NewDefaultChromaVectorField(),
	}

	for _, opt := range opts {
		opt(ci)
	}

	// 对齐 Python: self._distance_metric = config.distance_metric.replace("dot", "ip").replace("euclidean", "l2")
	ci.distanceMetric = strings.ReplaceAll(
		strings.ReplaceAll(string(config.DistanceMetric), "dot", "ip"),
		"euclidean", "l2",
	)

	// 对齐 Python: self._construct_config = self.vector_field.to_dict(stage="construct")
	ci.constructConfig = ci.vectorField.ToConstructDict()
	ci.constructConfig["space"] = ci.distanceMetric

	// 对齐 Python: self._search_config = self.vector_field.to_dict(stage="search")
	ci.searchConfig = ci.vectorField.ToSearchDict()

	// M-44: 对齐 Python self.database_name = config.database_name
	ci.databaseName = config.DatabaseName

	// M-44: 对齐 Python __init__ L90-93:
	//
	//	Python: self._client = ChromaVectorStore.create_client(
	//	Python:   database_name=self.database_name, path_or_uri=chroma_path)
	//
	// 构造时创建一次持久 client，后续 DeleteIndex/IndexExists/GetIndexInfo 复用，
	// 不再每次操作新建 PersistentClient（资源浪费 + 历史上的 IndexExists 恒真问题）。
	client, err := vector_store.CreateChromaClient(ci.databaseName, chromaPath)
	if err != nil {
		return nil, err
	}
	ci.client = client

	return ci, nil
}

// WithChromaTextField 设置文本字段名。
func WithChromaTextField(field string) ChromaIndexerOption {
	return func(ci *ChromaIndexer) { ci.textField = field }
}

// WithChromaVectorField 设置向量字段配置。
func WithChromaVectorField(vf *vector_fields.ChromaVectorField) ChromaIndexerOption {
	return func(ci *ChromaIndexer) { ci.vectorField = vf }
}

// WithChromaSparseVectorField 设置稀疏向量字段名。
func WithChromaSparseVectorField(field string) ChromaIndexerOption {
	return func(ci *ChromaIndexer) { ci.sparseVectorField = field }
}

// WithChromaMetadataField 设置元数据字段名。
func WithChromaMetadataField(field string) ChromaIndexerOption {
	return func(ci *ChromaIndexer) { ci.metadataField = field }
}

// WithChromaDocIDField 设置文档 ID 字段名。
func WithChromaDocIDField(field string) ChromaIndexerOption {
	return func(ci *ChromaIndexer) { ci.docIDField = field }
}

// WithChromaDocIndexCallback 设置嵌入进度回调。
func WithChromaDocIndexCallback(cb common.DocIndexCallback) ChromaIndexerOption {
	return func(ci *ChromaIndexer) { ci.docIndexCallback = cb }
}

// DistanceMetric 返回归一化距离度量。
func (ci *ChromaIndexer) DistanceMetric() string { return ci.distanceMetric }

// DatabaseName 返回数据库名称（13.26 validate_index 消费）。
func (ci *ChromaIndexer) DatabaseName() string { return ci.databaseName }

// TextField 返回文本字段名（13.26 validate_index 消费）。
func (ci *ChromaIndexer) TextField() string { return ci.textField }

// VectorField 返回向量字段配置（13.26 validate_index 消费，any 装箱联合类型）。
func (ci *ChromaIndexer) VectorField() any { return ci.vectorField }

// SparseVectorField 返回稀疏向量字段名（13.26 validate_index 消费）。
func (ci *ChromaIndexer) SparseVectorField() string { return ci.sparseVectorField }

// MetadataField 返回元数据字段名（13.26 validate_index 消费）。
func (ci *ChromaIndexer) MetadataField() string { return ci.metadataField }

// DocIDField 返回文档 ID 字段名（13.26 validate_index 消费）。
func (ci *ChromaIndexer) DocIDField() string { return ci.docIDField }

// Client 返回 ChromaDB 持久客户端。
//
// M-44: 对齐 Python @property client -> self._client。
func (ci *ChromaIndexer) Client() chromav2.Client { return ci.client }

// Close 关闭索引管理器，释放持久客户端。
//
// M-44: 对齐 Python ChromaIndexer.close()。关闭失败已在此 Warn 记录
// （对齐 Python logger.warning），故返回 nil。
func (ci *ChromaIndexer) Close() error {
	if ci.client != nil {
		if err := ci.client.Close(); err != nil {
			logger.Warn(logComponent).
				Str("event_type", "INDEXING_WARN").
				Err(err).
				Msg("failed to close ChromaDB client")
		}
	}
	return nil
}

// BuildIndex 构建索引。将 chunks 去重→嵌入→写入向量库。
//
// 对齐 Python ChromaIndexer.build_index。
// 4 步流程：
//  1. 查重 doc_id（collection.get 检查已有文档）
//  2. 嵌入（vector/hybrid 类型需要 embedModel）
//  3. 转换 TextChunk → ChromaDB 字段
//  4. 写入（ChromaVectorStore.add）
func (ci *ChromaIndexer) BuildIndex(
	ctx context.Context,
	chunks []common.TextChunk,
	config common.IndexConfig,
	embedModel embedding.BaseEmbedding,
	opts ...IndexOption,
) (built bool, err error) {
	// M-28: 对齐 Python build_index 的外层异常包装——
	//   except BaseError: raise                        → BuildError 原样透传
	//   except Exception as e: logger.error + 包装上抛  → 非 BuildError 统一包装
	//   ADD_DOC_RUNTIME_ERROR + Error 日志（含 cause 链）。
	defer func() {
		if err == nil {
			return
		}
		if _, ok := err.(*exception.BaseError); ok {
			return
		}
		logger.Error(logComponent).
			Str("event_type", "INDEXING_ERROR").
			Str("index_name", config.IndexName).
			Int("chunk_count", len(chunks)).
			Err(err).
			Msg("failed to build index")
		err = exception.BuildError(
			exception.StatusRetrievalIndexingAddDocRuntimeError,
			exception.WithParam("error_msg", err.Error()),
			exception.WithCause(err),
		)
	}()

	collectionName := config.IndexName
	indexOpts := NewIndexOptions(opts...)
	databaseName, _ := indexOpts.Extra["database_name"].(string)

	// 对齐 Python: vector_store_config = VectorStoreConfig(...)
	vectorStoreConfig := common.VectorStoreConfig{
		StoreProvider:  common.StoreTypeChroma,
		CollectionName: collectionName,
		DatabaseName:   databaseName,
	}

	// T-26: 对齐偏差说明——Python build_index 传 vector_field=self.vector_field.vector_field
	// （仅名字，建集合用默认 HNSW 16/100/100，indexer 自定义 HNSW 参数不生效）；
	// Go 传完整 *ChromaVectorField，自定义 HNSW 参数在 build 路径生效。
	// 此为有意保留的修正性偏差：参数生效比 Python 行为更合理，不改回对齐。
	vectorStore, err := vector_store.NewChromaVectorStore(
		vectorStoreConfig, ci.chromaPath,
		ci.textField, ci.vectorField,
		ci.sparseVectorField, ci.metadataField, ci.docIDField,
	)
	if err != nil {
		return false, err
	}
	// M-45: 补资源释放（对齐 DeleteIndex/GetIndexInfo 的 Close 模式）。
	// Python build_index 每次也新建 vector_store，此行为保留；
	// Go 端 PersistentClient 持有底层资源，必须显式释放。
	defer vectorStore.Close()

	collection := vectorStore.Collection()

	// ─── 步骤 1：查重 doc_id ───
	// 对齐 Python: all_doc_ids = sorted({chunk.doc_id for chunk in chunks})
	allDocIDs := make(map[string]bool)
	for _, chunk := range chunks {
		if chunk.DocID != "" {
			allDocIDs[chunk.DocID] = true
		}
	}

	var duplicateDocIDs []string
	for docID := range allDocIDs {
		if docID == "" {
			continue
		}
		// 对齐 Python: collection.get(where={self.doc_id_field: doc_id}).get("ids")
		// M-27: dict 直传 where 语义，走 WrapChromaWhereMap 包装
		whereFilter := vector_store.WrapChromaWhereMap(map[string]any{ci.docIDField: docID})
		if whereFilter != nil {
			getResult, err := collection.Get(ctx, chromav2.WithWhere(whereFilter))
			// S-29: 查重保护不可静默失效——Python 端 collection.get 异常会沿
			// build_index 外层 except 包装上抛；Go 原实现吞错放行会让重复 doc_id
			// 绕过保护写入，破坏 Add 不变式，改为报错中断。
			if err != nil {
				return false, exception.BuildError(
					exception.StatusRetrievalIndexingAddDocRuntimeError,
					exception.WithParam("error_msg", fmt.Sprintf(
						"查重阶段 collection.Get 失败（doc_id=%s）: %v", docID, err)),
				)
			}
			if getResult != nil {
				ids := getResult.GetIDs()
				if len(ids) > 0 {
					duplicateDocIDs = append(duplicateDocIDs, docID)
				}
			}
		}
	}

	// T-22: map 遍历顺序不确定，排序对齐 Python sorted() 输出确定性。
	sort.Strings(duplicateDocIDs)

	// 对齐 Python: if duplicate_doc_ids: raise build_error(...)
	if len(duplicateDocIDs) > 0 {
		return false, exception.BuildError(
			exception.StatusRetrievalIndexingAddDocRuntimeError,
			exception.WithParam("error_msg",
				fmt.Sprintf("some documents with same doc_id already exist, if they are the same documents, "+
					"please consider updating instead of adding. duplicate_doc_ids=%v", duplicateDocIDs)),
		)
	}

	// ─── 步骤 2：嵌入 ───
	// 对齐 Python: if config.index_type in ("vector", "hybrid"):
	if config.IndexType == common.IndexTypeVector || config.IndexType == common.IndexTypeHybrid {
		if embedModel == nil {
			return false, exception.BuildError(
				exception.StatusRetrievalIndexingEmbedModelNotFound,
				exception.WithParam("error_msg", "embed_model is required for vector/hybrid index type"),
			)
		}

		embedOpts := []ChunkEmbedOption{
			WithUseCaptionForImages(config.UseCaptionForImages),
		}
		if ci.docIndexCallback != nil {
			embedOpts = append(embedOpts, WithDocIndexCallback(ci.docIndexCallback))
		}

		if err := ComputeChunkEmbeddings(ctx, chunks, embedModel, embedOpts...); err != nil {
			return false, err
		}
	}

	// ─── 步骤 3：转换 TextChunk → ChromaDB 字段 ───
	data := make([]map[string]any, len(chunks))
	for i, chunk := range chunks {
		meta := chunk.Metadata
		if meta == nil {
			meta = make(map[string]any)
		}
		item := map[string]any{
			"id":             chunk.ID,
			ci.docIDField:    chunk.DocID,
			ci.textField:     chunk.Text,
			ci.metadataField: meta,
		}
		if chunk.Embedding != nil {
			item[ci.vectorField.VectorFieldName] = chunk.Embedding
		}
		data[i] = item
	}

	// ─── 步骤 4：写入 ───
	// 对齐 Python: await vector_store.add(data=data)
	if err := vectorStore.Add(ctx, data); err != nil {
		return false, err
	}

	// 对齐 Python: logger.info(f"Successfully built index {collection_name} with {len(chunks)} chunks")
	logger.Info(logComponent).
		Str("index_name", collectionName).
		Int("chunk_count", len(chunks)).
		Msg("index built successfully")

	return true, nil
}

// UpdateIndex 更新索引。先删后建。
//
// 对齐 Python ChromaIndexer.update_index（chroma_indexer.py L198-215）。
// S-11: 对齐 Python 整体 try/except 吞错语义——delete 或 build 任一失败均
// 记录 Error 日志后返回 (false, nil)，不让异常传播阻断调用方
// （Python: except Exception → logger.error → return False）。
func (ci *ChromaIndexer) UpdateIndex(
	ctx context.Context,
	chunks []common.TextChunk,
	docID string,
	config common.IndexConfig,
	embedModel embedding.BaseEmbedding,
	opts ...IndexOption,
) (bool, error) {
	ok, err := func() (ok bool, buildErr error) {
		defer func() {
			if buildErr != nil {
				// 对齐 Python: except Exception as e: logger.error(f"Failed to update index: {e}")
				logger.Error(logComponent).
					Str("event_type", "INDEXING_ERROR").
					Str("doc_id", docID).
					Str("index_name", config.IndexName).
					Err(buildErr).
					Msg("Failed to update index")
				buildErr = nil
				ok = false
			}
		}()
		// 对齐 Python: await self.delete_index(doc_id, config.index_name)
		//
		// M-26: 对齐 Python 吞错语义——Python update_index 对 delete_index 失败
		// 的处理是记录日志后继续 rebuild（底层 delete_index 自身已吞掉多数异常
		// 返回 False），不会让旧数据删除失败阻断新数据写入。
		if _, delErr := ci.DeleteIndex(ctx, docID, config.IndexName); delErr != nil {
			logger.Warn(logComponent).
				Str("event_type", "INDEXING_WARN").
				Str("doc_id", docID).
				Str("index_name", config.IndexName).
				Err(delErr).
				Msg("update index: failed to delete old data, continuing with rebuild")
		}

		// 对齐 Python: return await self.build_index(chunks, config, embed_model, **kwargs)
		return ci.BuildIndex(ctx, chunks, config, embedModel, opts...)
	}()
	return ok, err
}

// DeleteIndex 删除索引。ChromaDB 不支持复杂过滤删除，需先查后删。
//
// 对齐 Python ChromaIndexer.delete_index。
// M-44: 复用构造时创建的持久 client（对齐 Python self._client.get_collection）。
func (ci *ChromaIndexer) DeleteIndex(ctx context.Context, docID string, indexName string) (bool, error) {
	// 对齐 Python: collection = await asyncio.to_thread(self._client.get_collection, name=index_name)
	collection, err := ci.client.GetCollection(ctx, indexName)
	if err != nil {
		// 对齐 Python: except Exception as e: logger.error(...); return False
		logger.Error(logComponent).
			Str("event_type", "INDEXING_ERROR").
			Str("doc_id", docID).
			Str("index_name", indexName).
			Err(err).
			Msg("failed to delete index entries")
		return false, nil
	}

	// 对齐 Python: results = collection.get(where={self.doc_id_field: doc_id})
	// M-27: dict 直传 where 语义，走 WrapChromaWhereMap 包装
	whereFilter := vector_store.WrapChromaWhereMap(map[string]any{ci.docIDField: docID})
	if whereFilter == nil {
		return false, nil
	}

	getResult, err := collection.Get(ctx, chromav2.WithWhere(whereFilter))
	if err != nil {
		// M-26: 对齐 Python delete_index 整体 try/except——查询失败也走
		// except 分支 return False（吞错不抛），上层 update_index 才能继续 rebuild。
		logger.Error(logComponent).
			Str("event_type", "INDEXING_ERROR").
			Str("doc_id", docID).
			Err(err).
			Msg("delete index: query failed")
		return false, nil
	}

	ids := getResult.GetIDs()
	// 对齐 Python: if not results or not results.get("ids") or len(results["ids"]) == 0:
	if len(ids) == 0 {
		logger.Info(logComponent).
			Str("doc_id", docID).
			Msg("no matching document entries found")
		return false, nil
	}

	// 对齐 Python: ids_to_delete = results["ids"]; collection.delete(ids=ids_to_delete)
	chromaIDs := make([]chromav2.DocumentID, len(ids))
	copy(chromaIDs, ids)

	if err := collection.Delete(ctx, chromav2.WithIDs(chromaIDs...)); err != nil {
		// M-26: 对齐 Python delete_index 整体 try/except——删除失败也走
		// except 分支 return False（吞错不抛）。
		logger.Error(logComponent).
			Str("event_type", "INDEXING_ERROR").
			Str("doc_id", docID).
			Err(err).
			Msg("failed to delete index entries")
		return false, nil
	}

	// 对齐 Python: logger.info(f"Deleted {delete_count} entries for doc_id={doc_id}")
	deleteCount := len(chromaIDs)
	logger.Info(logComponent).
		Str("doc_id", docID).
		Int("delete_count", deleteCount).
		Msg("document entries deleted successfully")

	return deleteCount > 0, nil
}

// IndexExists 检查索引是否存在。
//
// 对齐 Python ChromaIndexer.index_exists。
// M-44: 复用持久 client + 真实 get_collection 语义。
// 原实现 NewChromaVectorStore 内部 GetOrCreateCollection 永不失败，
// 导致恒返回 true（S-63/S-64/S-15 历史问题），本修复一并解决。
func (ci *ChromaIndexer) IndexExists(ctx context.Context, indexName string) (bool, error) {
	// 对齐 Python: try: get_collection(name=index_name); return True; except Exception: return False
	_, err := ci.client.GetCollection(ctx, indexName)
	if err != nil {
		return false, nil
	}
	return true, nil
}

// GetIndexInfo 获取索引元信息。
//
// 对齐 Python ChromaIndexer.get_index_info。
// M-44: 复用构造时创建的持久 client（对齐 Python self._client.get_collection）。
func (ci *ChromaIndexer) GetIndexInfo(ctx context.Context, indexName string) (map[string]any, error) {
	exists, err := ci.IndexExists(ctx, indexName)
	if err != nil {
		logger.Error(logComponent).
			Str("event_type", "INDEXING_ERROR").
			Str("index_name", indexName).
			Err(err).
			Msg("failed to get index info")
		return map[string]any{"exists": false, "error": err.Error()}, nil
	}

	if !exists {
		return map[string]any{"exists": false}, nil
	}

	// 对齐 Python: collection = await asyncio.to_thread(self._client.get_collection, name=index_name)
	collection, err := ci.client.GetCollection(ctx, indexName)
	if err != nil {
		return map[string]any{"exists": false, "error": err.Error()}, nil
	}
	count, err := collection.Count(ctx)
	if err != nil {
		// M-31: 对齐 Python get_index_info 的整体 try/except——count 异常
		// 走 except 分支 return {"exists": False, "error": str(e)}，
		// 不静默置 0（否则无法区分空集合与查询失败）。
		logger.Error(logComponent).
			Str("event_type", "INDEXING_ERROR").
			Str("index_name", indexName).
			Err(err).
			Msg("failed to get index info: count failed")
		return map[string]any{"exists": false, "error": err.Error()}, nil
	}

	metadata := make(map[string]any)
	if collMeta := collection.Metadata(); collMeta != nil {
		for _, key := range collMeta.Keys() {
			if val, ok := collMeta.GetRaw(key); ok {
				metadata[key] = val
			}
		}
	}

	return map[string]any{
		"exists":          true,
		"collection_name": indexName,
		"count":           count,
		"metadata":        metadata,
	}, nil
}
