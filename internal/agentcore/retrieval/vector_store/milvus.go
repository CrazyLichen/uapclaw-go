package vector_store

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"sync"

	"github.com/milvus-io/milvus/client/v2/column"
	"github.com/milvus-io/milvus/client/v2/entity"
	milvusclient "github.com/milvus-io/milvus/client/v2/milvusclient"
	"google.golang.org/grpc"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/query"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/vector"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/vector_fields"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/utils"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 接口 ────────────────────────────

// MilvusClient retrieval 层 Milvus 客户端接口。
//
// 对齐 Python 直接使用 pymilvus.MilvusClient 的方法子集，
// 与 foundation 层的 milvusClient 接口独立（Python 两层也各自独立用 SDK）。
// 通过依赖注入支持单元测试 mock。
type MilvusClient interface {
	// CreateCollection 创建集合
	CreateCollection(ctx context.Context, option milvusclient.CreateCollectionOption, callOptions ...grpc.CallOption) error
	// DropCollection 删除集合
	DropCollection(ctx context.Context, option milvusclient.DropCollectionOption, callOptions ...grpc.CallOption) error
	// HasCollection 检查集合是否存在
	HasCollection(ctx context.Context, option milvusclient.HasCollectionOption, callOptions ...grpc.CallOption) (bool, error)
	// DescribeCollection 获取集合描述
	DescribeCollection(ctx context.Context, option milvusclient.DescribeCollectionOption, callOptions ...grpc.CallOption) (*entity.Collection, error)
	// GetCollectionStats 获取集合统计信息（row_count）
	GetCollectionStats(ctx context.Context, option milvusclient.GetCollectionOption, callOptions ...grpc.CallOption) (map[string]string, error)
	// Insert 插入数据
	Insert(ctx context.Context, option milvusclient.InsertOption, callOptions ...grpc.CallOption) (milvusclient.InsertResult, error)
	// Delete 删除数据
	Delete(ctx context.Context, option milvusclient.DeleteOption, callOptions ...grpc.CallOption) (milvusclient.DeleteResult, error)
	// Flush 刷写数据
	Flush(ctx context.Context, option milvusclient.FlushOption, callOptions ...grpc.CallOption) error
	// Search 向量搜索
	Search(ctx context.Context, option milvusclient.SearchOption, callOptions ...grpc.CallOption) ([]milvusclient.ResultSet, error)
	// HybridSearch 混合搜索
	HybridSearch(ctx context.Context, option milvusclient.HybridSearchOption, callOptions ...grpc.CallOption) ([]milvusclient.ResultSet, error)
	// Query 按表达式查询
	Query(ctx context.Context, option milvusclient.QueryOption, callOptions ...grpc.CallOption) (milvusclient.ResultSet, error)
	// DescribeIndex 获取索引描述
	DescribeIndex(ctx context.Context, option milvusclient.DescribeIndexOption, callOptions ...grpc.CallOption) (milvusclient.IndexDescription, error)
	// LoadCollection 加载集合
	LoadCollection(ctx context.Context, option milvusclient.LoadCollectionOption, callOptions ...grpc.CallOption) error
	// GetLoadState 获取加载状态
	GetLoadState(ctx context.Context, option milvusclient.GetLoadStateOption, callOptions ...grpc.CallOption) (entity.LoadState, error)
	// ReleaseCollection 释放集合
	ReleaseCollection(ctx context.Context, option milvusclient.ReleaseCollectionOption, callOptions ...grpc.CallOption) error
	// ListDatabase 列出数据库（供 DefaultMilvusCreateClient 建库前检查）
	ListDatabase(ctx context.Context, option milvusclient.ListDatabaseOption, callOptions ...grpc.CallOption) ([]string, error)
	// CreateDatabase 创建数据库（供 DefaultMilvusCreateClient 自动建库）
	CreateDatabase(ctx context.Context, option milvusclient.CreateDatabaseOption, callOptions ...grpc.CallOption) error
	// Close 关闭连接
	Close(ctx context.Context) error
}

// ──────────────────────────── 结构体 ────────────────────────────

// milvusClientAdapter 适配器，将 milvusclient.Client 适配到 MilvusClient 接口。
// 异步 Task 方法（LoadCollection/Flush）通过 task.Await 转为同步。
// 仅在集成测试中覆盖（需要真实 Milvus 服务）。
type milvusClientAdapter struct {
	client *milvusclient.Client
}

// MilvusVectorStore retrieval 层 Milvus 向量存储实现。
//
// 支持向量搜索、BM25 稀疏搜索和混合搜索（原生 HybridSearch + RRF）。
// 对齐 Python retrieval/vector_store/milvus_store.py (MilvusVectorStore)。
type MilvusVectorStore struct {
	// config 向量存储配置
	config common.VectorStoreConfig
	// collectionName 集合名称
	collectionName string
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
	// distanceMetric 归一化距离度量：IP/L2/COSINE
	distanceMetric string
	// constructConfig 构建阶段配置
	constructConfig map[string]any
	// searchConfig 搜索阶段配置
	searchConfig map[string]any
	// client Milvus 客户端（惰性创建）
	client MilvusClient
	// createClientFunc 客户端创建函数（依赖注入）
	createClientFunc func(ctx context.Context, databaseName, uri, token string) (MilvusClient, error)
	// collectionLoaded 集合是否已加载
	collectionLoaded bool
	// mu 读写锁
	mu sync.RWMutex
}

// MilvusStoreOption MilvusVectorStore 的可选参数函数。
type MilvusStoreOption func(*MilvusVectorStore)

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// defaultMilvusBatchSize 默认批大小，对齐 Python batch_size=128
	defaultMilvusBatchSize = 128
)

// ──────────────────────────── 全局变量 ────────────────────────────

// S-47: milvusConstructKeyMap 已删除——vf tag name= 根源修复后
// constructConfig 键名（M/efConstruction/nlist/with_raw_data）与
// Milvus 服务器参数键天然一致，无需对比前映射。

// 编译期接口断言：MilvusVectorStore 实现 VectorStore 接口
var _ VectorStore = (*MilvusVectorStore)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// DefaultMilvusCreateClient 默认客户端创建函数。
//
// 对齐 Python MilvusVectorStore.create_client：
// 创建 client → database_name 非空且非 default 时检查库是否存在，不存在则创建。
// 切库语义由 ClientConfig.DBName 等价覆盖（SDK 通过 grpc metadata header
// 将 currentDB 附加到每个请求，等价 Python use_database），无需显式调用。
// 仅在集成测试中覆盖（需要真实 Milvus 服务）。
func DefaultMilvusCreateClient(ctx context.Context, databaseName, uri, token string) (MilvusClient, error) {
	c, err := milvusclient.New(ctx, &milvusclient.ClientConfig{
		Address: uri,
		APIKey:  token,
		DBName:  databaseName,
	})
	if err != nil {
		return nil, err
	}
	adapter := &milvusClientAdapter{client: c}
	// 对齐 Python: if database_name and database_name != "default":
	//   if database_name not in client.list_databases(): client.create_database(database_name)
	if databaseName != "" && databaseName != "default" {
		dbs, err := adapter.ListDatabase(ctx, milvusclient.NewListDatabaseOption())
		if err != nil {
			_ = adapter.Close(ctx)
			return nil, err
		}
		exists := false
		for _, db := range dbs {
			if db == databaseName {
				exists = true
				break
			}
		}
		if !exists {
			if err := adapter.CreateDatabase(ctx, milvusclient.NewCreateDatabaseOption(databaseName)); err != nil {
				_ = adapter.Close(ctx)
				return nil, err
			}
		}
	}
	return adapter, nil
}

// NewMilvusVectorStore 创建 retrieval 层 MilvusVectorStore。
//
// 对齐 Python MilvusVectorStore.__init__。
// vectorField 为 vector_fields.MilvusVectorFieldConfig 完整配置（含 MilvusAUTO 等变体），
// 对齐 Python vector_field 参数（str 自动转 MilvusAUTO / MilvusVectorField 实例）。
// 客户端惰性创建：首次使用时才建立连接（对齐 Python 构造时创建，Go 便于测试改为惰性）。
func NewMilvusVectorStore(
	config common.VectorStoreConfig,
	milvusURI string,
	milvusToken string,
	textField string,
	vectorField vector_fields.MilvusVectorFieldConfig,
	sparseVectorField string,
	metadataField string,
	docIDField string,
	opts ...MilvusStoreOption,
) (*MilvusVectorStore, error) {
	// 对齐 Python: 连接时 URI 为空会失败（Go 显式校验提前报错）
	if strings.TrimSpace(milvusURI) == "" {
		return nil, exception.BuildError(
			exception.StatusRetrievalVectorStorePathNotFound,
			exception.WithParam("error_msg", "milvus_uri is required and cannot be empty"),
		)
	}

	// 对齐 Python: if isinstance(vector_field, str): MilvusAUTO(vector_field=...)
	//              elif isinstance(vector_field, MilvusVectorField): pass
	//              else: raise VECTOR_FIELD_INVALID
	if vectorField == nil {
		return nil, exception.BuildError(
			exception.StatusRetrievalIndexingVectorFieldInvalid,
			exception.WithParam("error_msg", "vector_field must be either a str or MilvusVectorField instance"),
		)
	}

	s := &MilvusVectorStore{
		config:            config,
		collectionName:    config.CollectionName,
		milvusURI:         milvusURI,
		milvusToken:       milvusToken,
		textField:         textField,
		sparseVectorField: sparseVectorField,
		metadataField:     metadataField,
		docIDField:        docIDField,
		databaseName:      config.DatabaseName,
		vectorField:       vectorField,
		createClientFunc:  DefaultMilvusCreateClient,
	}

	for _, opt := range opts {
		opt(s)
	}

	// 对齐 Python: self._distance_metric = config.distance_metric.replace("dot","ip").replace("euclidean","l2").upper()
	s.distanceMetric = strings.ToUpper(
		strings.ReplaceAll(
			strings.ReplaceAll(string(config.DistanceMetric), "dot", "ip"),
			"euclidean", "l2",
		),
	)

	// 对齐 Python: if self.vector_field.index_type == "auto": self._construct_config = {}
	//              else: self._construct_config = self.vector_field.to_dict(stage="construct")
	if vectorField.IndexType() == vector_fields.IndexTypeAUTO {
		s.constructConfig = map[string]any{}
	} else {
		s.constructConfig = vectorField.ToConstructDict()
	}
	// 对齐 Python: self._construct_config["metric_type"] = self._distance_metric
	s.constructConfig["metric_type"] = s.distanceMetric

	// 对齐 Python: self._search_config = self.vector_field.to_dict(stage="search")
	s.searchConfig = vectorField.ToSearchDict()

	return s, nil
}

// Client 返回底层 Milvus 客户端（惰性创建）。
// 对齐 Python client property。
func (s *MilvusVectorStore) Client(ctx context.Context) (MilvusClient, error) {
	return s.getClient(ctx)
}

// DistanceMetric 返回归一化距离度量（IP/L2/COSINE）。
// 对齐 Python distance_metric property。
func (s *MilvusVectorStore) DistanceMetric() string { return s.distanceMetric }

// Search 向量搜索。
//
// 对齐 Python MilvusVectorStore.search：
//   - _ensure_loaded
//   - 构建 filter 表达式（M-27: QueryExpr → to_expr("milvus")）
//   - client.search（anns_field + search_params 含 metric_type）
//   - 结果转 SearchResult（vector 模式分数归一化）
func (s *MilvusVectorStore) Search(ctx context.Context, queryVector []float64, topK int, filters query.QueryExpr, _ ...StoreOption) ([]common.SearchResult, error) {
	if err := s.ensureLoaded(ctx); err != nil {
		return nil, err
	}
	c, err := s.getClient(ctx)
	if err != nil {
		return nil, err
	}

	// 对齐 Python: output_fields = [text_field, metadata_field, doc_id_field, "chunk_id"]
	outputFields := []string{s.textField, s.metadataField, s.docIDField, "chunk_id"}

	// M-27: 对齐 Python filter_expr 构建（QueryExpr → to_expr("milvus")）
	filterExpr, err := buildMilvusFilterExpr(filters)
	if err != nil {
		// 对齐 Python: to_expr 异常沿调用链上抛
		logger.Error(logComponent).
			Str("event_type", "LLM_CALL_ERROR").
			Str("method", "Search").
			Str("collection_name", s.collectionName).
			Err(err).
			Msg("failed to convert filter expression to milvus")
		return nil, err
	}

	// 构建查询向量
	f32 := make([]float32, len(queryVector))
	for i, v := range queryVector {
		f32[i] = float32(v)
	}
	vectors := []entity.Vector{entity.FloatVector(f32)}

	// 对齐 Python: search_params={"metric_type": self._distance_metric, "params": self.get_search_params(top_k)}
	searchOpt := milvusclient.NewSearchOption(s.collectionName, topK, vectors).
		WithANNSField(s.vectorField.VectorFieldName()).
		WithSearchParam("metric_type", s.distanceMetric).
		WithOutputFields(outputFields...)
	for k, v := range s.getSearchParams(topK) {
		searchOpt = searchOpt.WithSearchParam(k, fmt.Sprintf("%v", v))
	}
	if filterExpr != "" {
		searchOpt = searchOpt.WithFilter(filterExpr)
	}

	resultSets, err := c.Search(ctx, searchOpt)
	if err != nil {
		logger.Error(logComponent).
			Str("event_type", "LLM_CALL_ERROR").
			Str("method", "Search").
			Str("collection_name", s.collectionName).
			Err(err).
			Msg("vector search failed")
		return nil, err
	}

	// 对齐 Python: if results and len(results) > 0: return self._milvus_result_to_search_results(results[0], mode="vector")
	if len(resultSets) > 0 {
		return s.milvusResultToSearchResults(resultSets[0], "vector"), nil
	}
	return nil, nil
}

// SparseSearch 稀疏搜索（BM25）。
//
// 对齐 Python MilvusVectorStore.sparse_search：
// 传递查询文本（BM25 Function 自动分词），失败时 warning + 返回空。
// M-27: filters 对齐 Python dict | QueryExpr，nil 表示无过滤。
func (s *MilvusVectorStore) SparseSearch(ctx context.Context, queryText string, topK int, filters query.QueryExpr, _ ...StoreOption) ([]common.SearchResult, error) {
	if err := s.ensureLoaded(ctx); err != nil {
		return nil, err
	}
	c, err := s.getClient(ctx)
	if err != nil {
		return nil, err
	}

	// 对齐 Python: output_fields = [text_field, metadata_field, doc_id_field]
	outputFields := []string{s.textField, s.metadataField, s.docIDField}
	// M-27: 对齐 Python filter_expr 构建（QueryExpr → to_expr("milvus")）
	filterExpr, err := buildMilvusFilterExpr(filters)
	if err != nil {
		// 对齐 Python: to_expr 异常沿调用链上抛
		logger.Error(logComponent).
			Str("event_type", "LLM_CALL_ERROR").
			Str("method", "SparseSearch").
			Str("collection_name", s.collectionName).
			Err(err).
			Msg("failed to convert filter expression to milvus")
		return nil, err
	}

	// 对齐 Python: data=[query_text] 直接传文本，BM25 function 处理分词
	vectors := []entity.Vector{entity.Text(queryText)}
	searchOpt := milvusclient.NewSearchOption(s.collectionName, topK, vectors).
		WithANNSField(s.sparseVectorField).
		WithSearchParam("metric_type", "BM25").
		WithOutputFields(outputFields...)
	if filterExpr != "" {
		searchOpt = searchOpt.WithFilter(filterExpr)
	}

	resultSets, err := c.Search(ctx, searchOpt)
	if err != nil {
		// 对齐 Python: except Exception as e: logger.warning(f"BM25 text search failed: {e}"); return []
		// T-22: 对齐 Python 删除自加的截断，直接输出完整 query_text
		logger.Warn(logComponent).
			Str("method", "SparseSearch").
			Str("query_text", queryText).
			Err(err).
			Msg("BM25 text search failed")
		return nil, nil
	}

	// 对齐 Python: if results and len(results) > 0: ... mode="sparse"
	if len(resultSets) > 0 {
		return s.milvusResultToSearchResults(resultSets[0], "sparse"), nil
	}
	return nil, nil
}

// HybridSearch 混合搜索（原生 HybridSearch + RRF）。
//
// 对齐 Python MilvusVectorStore.hybrid_search：
//   - query_vector 非 nil 时构建 dense AnnRequest
//   - 始终构建 sparse BM25 AnnRequest
//   - 原生 hybrid_search + RRFRanker(k=60)
//   - 失败时降级到分别搜索后 RRF 融合
// M-27: filters 对齐 Python dict | QueryExpr，nil 表示无过滤。
func (s *MilvusVectorStore) HybridSearch(ctx context.Context, queryText string, queryVector []float64, topK int, alpha float64, filters query.QueryExpr, _ ...StoreOption) ([]common.SearchResult, error) {
	if err := s.ensureLoaded(ctx); err != nil {
		return nil, err
	}
	c, err := s.getClient(ctx)
	if err != nil {
		return nil, err
	}

	// 对齐 Python: output_fields = [text_field, metadata_field, doc_id_field]
	outputFields := []string{s.textField, s.metadataField, s.docIDField}
	// M-27: 对齐 Python filter_expr 构建（QueryExpr → to_expr("milvus")）
	filterExpr, err := buildMilvusFilterExpr(filters)
	if err != nil {
		// 对齐 Python: to_expr 异常沿调用链上抛
		logger.Error(logComponent).
			Str("event_type", "LLM_CALL_ERROR").
			Str("method", "HybridSearch").
			Str("collection_name", s.collectionName).
			Err(err).
			Msg("failed to convert filter expression to milvus")
		return nil, err
	}

	var annRequests []*milvusclient.AnnRequest

	// 对齐 Python: if query_vector is not None: dense_req = AnnSearchRequest(...)
	if queryVector != nil {
		f32 := make([]float32, len(queryVector))
		for i, v := range queryVector {
			f32[i] = float32(v)
		}
		denseReq := milvusclient.NewAnnRequest(s.vectorField.VectorFieldName(), topK, entity.FloatVector(f32)).
			WithSearchParam("metric_type", s.distanceMetric)
		for k, v := range s.getSearchParams(topK) {
			denseReq = denseReq.WithSearchParam(k, fmt.Sprintf("%v", v))
		}
		if filterExpr != "" {
			denseReq = denseReq.WithFilter(filterExpr)
		}
		annRequests = append(annRequests, denseReq)
	}

	// 对齐 Python: sparse_req = AnnSearchRequest(data=[query_text], anns_field=sparse_vector_field, param={"metric_type": "BM25"}, ...)
	sparseReq := milvusclient.NewAnnRequest(s.sparseVectorField, topK, entity.Text(queryText)).
		WithSearchParam("metric_type", "BM25")
	if filterExpr != "" {
		sparseReq = sparseReq.WithFilter(filterExpr)
	}
	annRequests = append(annRequests, sparseReq)

	// 对齐 Python: if not search_requests: return []
	if len(annRequests) == 0 {
		return nil, nil
	}

	// 对齐 Python: ranker=RRFRanker(k=60)
	reranker := milvusclient.NewRRFReranker().WithK(60)
	searchOpt := milvusclient.NewHybridSearchOption(s.collectionName, topK, annRequests...).
		WithOutputFields(outputFields...).
		WithReranker(reranker)

	resultSets, err := c.HybridSearch(ctx, searchOpt)
	if err != nil {
		// 对齐 Python: logger.warning(f"Hybrid search failed, falling back to separate searches: {e}")
		logger.Warn(logComponent).
			Str("method", "HybridSearch").
			Str("collection_name", s.collectionName).
			Err(err).
			Msg("Hybrid search failed, falling back to separate searches")
		return s.hybridSearchFallback(ctx, queryText, queryVector, topK, filters)
	}

	// 对齐 Python: if results and len(results) > 0: ... mode="hybrid"
	if len(resultSets) > 0 {
		return s.milvusResultToSearchResults(resultSets[0], "hybrid"), nil
	}
	return nil, nil
}

// Add 添加向量数据。
//
// 对齐 Python MilvusVectorStore.add：
//   - _ensure_loaded 确保集合已加载
//   - 按 batch_size（默认128）分批 insert
//   - 每 100 条打进度日志
//   - 最后 flush
func (s *MilvusVectorStore) Add(ctx context.Context, data []map[string]any, opts ...StoreOption) error {
	if err := s.ensureLoaded(ctx); err != nil {
		return err
	}
	storeOpts := NewStoreOptions(opts...)
	batchSize := storeOpts.BatchSize
	// 对齐 Python: if batch_size is None or batch_size <= 0: batch_size = 128
	if batchSize <= 0 {
		batchSize = defaultMilvusBatchSize
	}

	c, err := s.getClient(ctx)
	if err != nil {
		return err
	}

	processed := 0
	total := len(data)
	var cache []map[string]any
	for _, doc := range data {
		cache = append(cache, doc)
		if len(cache) >= batchSize {
			nodes := cache[:batchSize]
			cache = cache[batchSize:]
			if err := s.insertBatch(ctx, c, nodes); err != nil {
				return err
			}
			processed += len(nodes)
			// 对齐 Python: if processed % 100 == 0: logger.info("Written %d/%d records to %s", ...)
			if processed%100 == 0 {
				logger.Info(logComponent).
					Int("processed", processed).
					Int("total", total).
					Str("collection_name", s.collectionName).
					Msg("written records progress")
			}
		}
	}
	if len(cache) > 0 {
		if err := s.insertBatch(ctx, c, cache); err != nil {
			return err
		}
		processed += len(cache)
	}
	// 对齐 Python: logger.info("Writing completed, total %d/%d records to %s", ...)
	logger.Info(logComponent).
		Int("processed", processed).
		Int("total", total).
		Str("collection_name", s.collectionName).
		Msg("Writing completed")

	// 对齐 Python: self._client.flush(self.collection_name)（无 try/except，异常上抛）
	// M-03: flush 失败必须返回 error——数据已 insert 但未持久化可搜时，调用方
	// （BuildIndex）应沿既有错误链路感知失败，而非静默报成功
	if err := c.Flush(ctx, milvusclient.NewFlushOption(s.collectionName)); err != nil {
		logger.Error(logComponent).
			Str("method", "Add").
			Str("collection_name", s.collectionName).
			Err(err).
			Msg("flush failed")
		return err
	}
	return nil
}

// Delete 删除向量。
//
// 对齐 Python MilvusVectorStore.delete：按 ids 或 filter_expr 删除 + flush。
// 异常时 logger.error 并返回 False（不抛错，对齐 Python except 分支）。
// M-27: filterExpr 对齐 Python str | QueryExpr——QueryExpr 走 to_expr("milvus")，
// 转换失败对齐 Python try/except 吞错返回 False。
func (s *MilvusVectorStore) Delete(ctx context.Context, ids []string, filterExpr query.QueryExpr) (bool, error) {
	if err := s.ensureLoaded(ctx); err != nil {
		return false, err
	}
	c, err := s.getClient(ctx)
	if err != nil {
		return false, err
	}

	deleteOpt := milvusclient.NewDeleteOption(s.collectionName)
	// 对齐 Python: ids 非空时按主键删除（pymilvus ids 参数语义，Go 用 in 表达式等价）
	if len(ids) > 0 {
		quoted := make([]string, 0, len(ids))
		for _, id := range ids {
			quoted = append(quoted, fmt.Sprintf("%q", id))
		}
		deleteOpt = deleteOpt.WithExpr(fmt.Sprintf("pk in [%s]", strings.Join(quoted, ",")))
	} else if filterExpr != nil {
		// M-27: 对齐 Python filter_expr（QueryExpr）→ to_expr("milvus") 等值过滤
		expr, err := buildMilvusFilterExpr(filterExpr)
		if err != nil {
			// 对齐 Python: except Exception as e: logger.error(...); return False
			logger.Error(logComponent).
				Str("event_type", "LLM_CALL_ERROR").
				Str("method", "Delete").
				Str("collection_name", s.collectionName).
				Err(err).
				Msg("failed to convert filter expression to milvus")
			return false, nil
		}
		if expr == "" {
			return false, nil
		}
		deleteOpt = deleteOpt.WithExpr(expr)
	} else {
		// 对齐 Python: ids 和 filter 都为空时 delete 无条件 → 直接返回 False
		return false, nil
	}

	result, err := c.Delete(ctx, deleteOpt)
	if err != nil {
		// 对齐 Python: except Exception as e: logger.error(f"Failed to delete vectors: {e}"); return False
		logger.Error(logComponent).
			Str("event_type", "LLM_CALL_ERROR").
			Str("method", "Delete").
			Str("collection_name", s.collectionName).
			Err(err).
			Msg("failed to delete vectors")
		return false, nil
	}
	if err := c.Flush(ctx, milvusclient.NewFlushOption(s.collectionName)); err != nil {
		logger.Warn(logComponent).Err(err).Str("collection_name", s.collectionName).Msg("flush failed")
	}
	// 对齐 Python: result.get("delete_count", 0) > 0
	return result.DeleteCount > 0, nil
}

// TableExists 检查集合是否存在。
// 对齐 Python MilvusVectorStore.table_exists。
func (s *MilvusVectorStore) TableExists(ctx context.Context, tableName string) (bool, error) {
	c, err := s.getClient(ctx)
	if err != nil {
		return false, err
	}
	return c.HasCollection(ctx, milvusclient.NewHasCollectionOption(tableName))
}

// DeleteTable 删除集合。
// 对齐 Python MilvusVectorStore.delete_table。
func (s *MilvusVectorStore) DeleteTable(ctx context.Context, tableName string) error {
	c, err := s.getClient(ctx)
	if err != nil {
		return err
	}
	return c.DropCollection(ctx, milvusclient.NewDropCollectionOption(tableName))
}

// Close 关闭存储（不释放集合）。
// 实现 VectorStore 接口。
func (s *MilvusVectorStore) Close() {
	s.CloseWithRelease(false)
}

// CloseWithRelease 关闭存储，可选释放集合。
//
// 对齐 Python MilvusVectorStore.close(release_collection=False)：
// release 时检查集合存在 + load state == Loaded 后 release，然后关闭 client。
func (s *MilvusVectorStore) CloseWithRelease(releaseCollection bool) {
	c, err := s.getClient(context.Background())
	if err != nil || c == nil {
		return
	}
	ctx := context.Background()
	if releaseCollection {
		// 对齐 Python: if release_collection and has_collection(...):
		//   if get_load_state(...).state == Loaded: release_collection(...)
		if has, hasErr := c.HasCollection(ctx, milvusclient.NewHasCollectionOption(s.collectionName)); hasErr == nil && has {
			if state, stateErr := c.GetLoadState(ctx, milvusclient.NewGetLoadStateOption(s.collectionName)); stateErr == nil && state.State == entity.LoadStateLoaded {
				if err := c.ReleaseCollection(ctx, milvusclient.NewReleaseCollectionOption(s.collectionName)); err != nil {
					// 对齐 Python: logger.warning(f"Failed to release Milvus collection: {e}")
					logger.Warn(logComponent).Err(err).Str("collection_name", s.collectionName).Msg("failed to release collection")
				}
				s.mu.Lock()
				s.collectionLoaded = false
				s.mu.Unlock()
			}
		}
	}
	if err := c.Close(ctx); err != nil {
		// 对齐 Python: logger.warning(f"Failed to close Milvus client: {e}")
		logger.Warn(logComponent).Err(err).Msg("failed to close Milvus client")
	}
}

// WithMilvusClientOption 注入 MilvusClient 实现。
func WithMilvusClientOption(c MilvusClient) MilvusStoreOption {
	return func(s *MilvusVectorStore) {
		s.client = c
		s.createClientFunc = func(ctx context.Context, _, _, _ string) (MilvusClient, error) {
			return c, nil
		}
	}
}

// WithMilvusCreateClient 注入客户端创建函数。
func WithMilvusCreateClient(f func(ctx context.Context, databaseName, uri, token string) (MilvusClient, error)) MilvusStoreOption {
	return func(s *MilvusVectorStore) { s.createClientFunc = f }
}

// CheckVectorField 校验向量字段配置是否与实际数据库一致。
//
// 对齐 Python MilvusVectorStore.check_vector_field：
//   - 集合不存在时直接返回
//   - describe_index 为空时：列出集合中的向量字段并抛 KB_DATABASE_CONFIG_INVALID
//   - index_type 非 auto 时校验前缀匹配
//   - 最后 _check_configs_matching 校验构造参数
func (s *MilvusVectorStore) CheckVectorField() error {
	c, err := s.getClient(context.Background())
	if err != nil {
		return err
	}
	// 对齐 Python: if not self._client.has_collection(self.collection_name): return
	has, err := c.HasCollection(context.Background(), milvusclient.NewHasCollectionOption(s.collectionName))
	if err != nil || !has {
		return err
	}

	// 对齐 Python: actual = self._client.describe_index(collection_name, field_name) or {}
	indexType := s.vectorField.IndexType()
	fieldName := s.vectorField.VectorFieldName()
	actual, idxErr := c.DescribeIndex(context.Background(), milvusclient.NewDescribeIndexOption(s.collectionName, fieldName))
	// SDK 出错时 actual.Index（内嵌接口）为 nil，必须先判空再取 Params，避免空指针
	var actualParams map[string]string
	if idxErr == nil && actual.Index != nil {
		actualParams = actual.Params()
	}
	if idxErr != nil || actual.Index == nil || len(actualParams) == 0 {
		// 对齐 Python: 索引不存在时列出向量字段并报错
		coll, collErr := c.DescribeCollection(context.Background(), milvusclient.NewDescribeCollectionOption(s.collectionName))
		var vFieldsList string
		if collErr == nil && coll != nil && coll.Schema != nil {
			for _, v := range coll.Schema.Fields {
				if v.DataType == entity.FieldTypeFloatVector {
					vFieldsList += fmt.Sprintf("- [%d] %s: %v\n", v.ID, v.Name, v.TypeParams)
				}
			}
		}
		return exception.BuildError(
			exception.StatusRetrievalKbDatabaseConfigInvalid,
			exception.WithParam("error_msg",
				fmt.Sprintf("MilvusVectorStore has vector_field at %s while actual database has "+
					"vector field(s) at:\n%sYou may want to call delete_collection method on "+
					"collection %q", fieldName, vFieldsList, s.collectionName)),
		)
	}

	// 对齐 Python: if index_type != "auto": 校验 returned_type 前缀匹配
	if indexType != vector_fields.IndexTypeAUTO {
		returnedType := actualParams["index_type"]
		if returnedType == "" {
			returnedType = string(actual.IndexType())
		}
		// 对齐 Python: actual.get("index_type", "unknown") 的最终回退
		if returnedType == "" {
			returnedType = "unknown"
		}
		// TODO(#通用): Python 还有 variant 后缀检查 returned_type.endswith(variant)，
		// Go vector_fields 尚无 variant 字段，待补充后回填。
		if !strings.HasPrefix(returnedType, strings.ToUpper(indexType.String())) {
			return exception.BuildError(
				exception.StatusRetrievalKbDatabaseConfigInvalid,
				exception.WithParam("error_msg",
					fmt.Sprintf("MilvusVectorStore has index_type of %s while actual database has "+
						"index_type of %s, do not change index_type after Knowledge Base is constructed.",
						indexType.String(), returnedType)),
			)
		}
	}

	// 对齐 Python: self._check_configs_matching(self._construct_config, actual)
	// S-47: vf tag name= 修复后 constructConfig 键名与 Milvus 服务器键天然一致，
	// 删除原 milvusConstructKeyMap 归一化循环，直接对比。
	actualParamsAny := make(map[string]any, len(actualParams))
	for k, v := range actualParams {
		actualParamsAny[k] = v
	}
	return CheckConfigsMatching(s.constructConfig, actualParamsAny)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// CreateCollection 创建集合。
func (a *milvusClientAdapter) CreateCollection(ctx context.Context, option milvusclient.CreateCollectionOption, callOptions ...grpc.CallOption) error {
	return a.client.CreateCollection(ctx, option, callOptions...)
}

// DropCollection 删除集合。
func (a *milvusClientAdapter) DropCollection(ctx context.Context, option milvusclient.DropCollectionOption, callOptions ...grpc.CallOption) error {
	return a.client.DropCollection(ctx, option, callOptions...)
}

// HasCollection 检查集合是否存在。
func (a *milvusClientAdapter) HasCollection(ctx context.Context, option milvusclient.HasCollectionOption, callOptions ...grpc.CallOption) (bool, error) {
	return a.client.HasCollection(ctx, option, callOptions...)
}

// DescribeCollection 获取集合描述。
func (a *milvusClientAdapter) DescribeCollection(ctx context.Context, option milvusclient.DescribeCollectionOption, callOptions ...grpc.CallOption) (*entity.Collection, error) {
	return a.client.DescribeCollection(ctx, option, callOptions...)
}

// GetCollectionStats 获取集合统计信息。
func (a *milvusClientAdapter) GetCollectionStats(ctx context.Context, option milvusclient.GetCollectionOption, callOptions ...grpc.CallOption) (map[string]string, error) {
	return a.client.GetCollectionStats(ctx, option)
}

// Insert 插入数据。
func (a *milvusClientAdapter) Insert(ctx context.Context, option milvusclient.InsertOption, callOptions ...grpc.CallOption) (milvusclient.InsertResult, error) {
	return a.client.Insert(ctx, option, callOptions...)
}

// Delete 删除数据。
func (a *milvusClientAdapter) Delete(ctx context.Context, option milvusclient.DeleteOption, callOptions ...grpc.CallOption) (milvusclient.DeleteResult, error) {
	return a.client.Delete(ctx, option, callOptions...)
}

// Flush 刷写数据（异步 Task 转同步）。
func (a *milvusClientAdapter) Flush(ctx context.Context, option milvusclient.FlushOption, callOptions ...grpc.CallOption) error {
	task, err := a.client.Flush(ctx, option, callOptions...)
	if err != nil {
		return err
	}
	if task != nil {
		return task.Await(ctx)
	}
	return nil
}

// Search 向量搜索。
func (a *milvusClientAdapter) Search(ctx context.Context, option milvusclient.SearchOption, callOptions ...grpc.CallOption) ([]milvusclient.ResultSet, error) {
	return a.client.Search(ctx, option, callOptions...)
}

// HybridSearch 混合搜索。
func (a *milvusClientAdapter) HybridSearch(ctx context.Context, option milvusclient.HybridSearchOption, callOptions ...grpc.CallOption) ([]milvusclient.ResultSet, error) {
	return a.client.HybridSearch(ctx, option, callOptions...)
}

// Query 按表达式查询。
func (a *milvusClientAdapter) Query(ctx context.Context, option milvusclient.QueryOption, callOptions ...grpc.CallOption) (milvusclient.ResultSet, error) {
	return a.client.Query(ctx, option, callOptions...)
}

// DescribeIndex 获取索引描述。
func (a *milvusClientAdapter) DescribeIndex(ctx context.Context, option milvusclient.DescribeIndexOption, callOptions ...grpc.CallOption) (milvusclient.IndexDescription, error) {
	return a.client.DescribeIndex(ctx, option, callOptions...)
}

// LoadCollection 加载集合（异步 Task 转同步）。
func (a *milvusClientAdapter) LoadCollection(ctx context.Context, option milvusclient.LoadCollectionOption, callOptions ...grpc.CallOption) error {
	task, err := a.client.LoadCollection(ctx, option, callOptions...)
	if err != nil {
		return err
	}
	return task.Await(ctx)
}

// GetLoadState 获取加载状态。
func (a *milvusClientAdapter) GetLoadState(ctx context.Context, option milvusclient.GetLoadStateOption, callOptions ...grpc.CallOption) (entity.LoadState, error) {
	return a.client.GetLoadState(ctx, option, callOptions...)
}

// ReleaseCollection 释放集合。
func (a *milvusClientAdapter) ReleaseCollection(ctx context.Context, option milvusclient.ReleaseCollectionOption, callOptions ...grpc.CallOption) error {
	return a.client.ReleaseCollection(ctx, option, callOptions...)
}

// ListDatabase 列出数据库。
func (a *milvusClientAdapter) ListDatabase(ctx context.Context, option milvusclient.ListDatabaseOption, callOptions ...grpc.CallOption) ([]string, error) {
	return a.client.ListDatabase(ctx, option, callOptions...)
}

// CreateDatabase 创建数据库。
func (a *milvusClientAdapter) CreateDatabase(ctx context.Context, option milvusclient.CreateDatabaseOption, callOptions ...grpc.CallOption) error {
	return a.client.CreateDatabase(ctx, option, callOptions...)
}

// Close 关闭连接。
func (a *milvusClientAdapter) Close(ctx context.Context) error {
	return a.client.Close(ctx)
}

// getClient 惰性获取 Milvus 客户端。
func (s *MilvusVectorStore) getClient(ctx context.Context) (MilvusClient, error) {
	s.mu.RLock()
	if s.client != nil {
		s.mu.RUnlock()
		return s.client, nil
	}
	s.mu.RUnlock()

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.client != nil {
		return s.client, nil
	}
	if s.createClientFunc == nil {
		return nil, fmt.Errorf("milvus client 创建函数未设置")
	}
	client, err := s.createClientFunc(ctx, s.databaseName, s.milvusURI, s.milvusToken)
	if err != nil {
		return nil, exception.BuildError(
			exception.StatusRetrievalVectorStoreProviderInvalid,
			exception.WithParam("error_msg", fmt.Sprintf("创建 Milvus 客户端失败: %s", err)),
			exception.WithCause(err),
		)
	}
	s.client = client
	return client, nil
}

// ensureLoaded 确保集合已加载。
//
// 对齐 Python MilvusVectorStore._ensure_loaded：
// 首次调用时 has_collection → load_collection，之后缓存跳过。
func (s *MilvusVectorStore) ensureLoaded(ctx context.Context) error {
	s.mu.RLock()
	loaded := s.collectionLoaded
	s.mu.RUnlock()
	if loaded {
		return nil
	}

	c, err := s.getClient(ctx)
	if err != nil {
		return err
	}

	// 对齐 Python: if self.client.has_collection(self.collection_name):
	has, err := c.HasCollection(ctx, milvusclient.NewHasCollectionOption(s.collectionName))
	if err != nil {
		return err
	}
	if !has {
		return nil
	}

	// 对齐 Python: logger.info("Retrieval Milvus Store: loading collection %s", ...)
	logger.Info(logComponent).Str("collection_name", s.collectionName).Msg("loading collection")
	if err := c.LoadCollection(ctx, milvusclient.NewLoadCollectionOption(s.collectionName)); err != nil {
		return err
	}
	// 对齐 Python: logger.info("Retrieval Milvus Store: %s collection loaded", ...)
	logger.Info(logComponent).Str("collection_name", s.collectionName).Msg("collection loaded")

	s.mu.Lock()
	s.collectionLoaded = true
	s.mu.Unlock()
	return nil
}

// insertBatch 将一批 map 数据转为 Milvus 列并插入。
//
// 对齐 Python asyncio.to_thread(self._client.insert, collection_name=..., data=nodes)。
// 列类型推断：[]float64 → FloatVector、string → VarChar、map[string]any → JSON。
func (s *MilvusVectorStore) insertBatch(ctx context.Context, c MilvusClient, nodes []map[string]any) error {
	if len(nodes) == 0 {
		return nil
	}
	columns, err := dataToColumns(nodes)
	if err != nil {
		return err
	}
	insertOpt := milvusclient.NewColumnBasedInsertOption(s.collectionName).WithColumns(columns...)
	if _, err := c.Insert(ctx, insertOpt); err != nil {
		// 对齐 Python build_index: insert 失败抛 ADD_DOC_RUNTIME_ERROR（由调用方包装）
		return exception.BuildError(
			exception.StatusRetrievalIndexingAddDocRuntimeError,
			exception.WithParam("error_msg", fmt.Sprintf("批量插入数据失败: %s", err)),
			exception.WithCause(err),
		)
	}
	return nil
}

// dataToColumns 将 map 数据列表转为 Milvus 列。
// 对齐 Python client.insert(data=[dict...]) 的 SDK 内部列转换。
func dataToColumns(nodes []map[string]any) ([]column.Column, error) {
	// 收集所有字段名（保持首次出现顺序）
	fieldSet := make(map[string]bool)
	var fieldOrder []string
	for _, doc := range nodes {
		for k := range doc {
			if !fieldSet[k] {
				fieldSet[k] = true
				fieldOrder = append(fieldOrder, k)
			}
		}
	}

	var columns []column.Column
	for _, fieldName := range fieldOrder {
		// 找第一个非 nil 值判断类型
		var firstVal any
		for _, doc := range nodes {
			if v, ok := doc[fieldName]; ok && v != nil {
				firstVal = v
				break
			}
		}
		if firstVal == nil {
			continue
		}

		switch v := firstVal.(type) {
		case []float64:
			vecs := make([][]float32, 0, len(nodes))
			dim := len(v)
			for _, doc := range nodes {
				if f64, ok := doc[fieldName].([]float64); ok && len(f64) > 0 {
					f32 := make([]float32, len(f64))
					for i, f := range f64 {
						f32[i] = float32(f)
					}
					vecs = append(vecs, f32)
				} else {
					vecs = append(vecs, make([]float32, dim))
				}
			}
			if len(vecs) > 0 && dim > 0 {
				columns = append(columns, column.NewColumnFloatVector(fieldName, dim, vecs))
			}
		case string:
			strs := make([]string, 0, len(nodes))
			for _, doc := range nodes {
				if str, ok := doc[fieldName].(string); ok {
					strs = append(strs, str)
				} else {
					strs = append(strs, "")
				}
			}
			columns = append(columns, column.NewColumnVarChar(fieldName, strs))
		case map[string]any:
			// metadata 字段 → JSON 列
			jsonBytes := make([][]byte, 0, len(nodes))
			for _, doc := range nodes {
				if m, ok := doc[fieldName].(map[string]any); ok {
					bs, err := json.Marshal(m)
					if err != nil {
						return nil, fmt.Errorf("序列化字段 %s 失败: %w", fieldName, err)
					}
					jsonBytes = append(jsonBytes, bs)
				} else {
					jsonBytes = append(jsonBytes, []byte("{}"))
				}
			}
			columns = append(columns, column.NewColumnJSONBytes(fieldName, jsonBytes))
		}
	}
	return columns, nil
}

// buildMilvusFilterExpr M-27: 将 QueryExpr 转换为 Milvus 过滤表达式字符串。
//
// 对齐 Python MilvusVectorStore.search 的 filter 构建逻辑：
//   - QueryExpr 走 filters.to_expr("milvus") 输出过滤字符串
//   - dict 等值语义由调用方经 query.EqMap 折叠（And(Eq...) 链，
//     milvusComparisonFilter 输出 "field == value"，
//     milvusLogicalFilter and 输出 "(left) and (right)"，
//     组合结果与 Python dict 分支 "a == 1 && b == 2" 等价）
// filters 为 nil 或空表达式时返回 ""（无过滤）。
func buildMilvusFilterExpr(filters query.QueryExpr) (string, error) {
	if filters == nil {
		return "", nil
	}

	raw, err := filters.ToExpr("milvus")
	if err != nil {
		// 对齐 Python: to_expr 异常沿调用链上抛
		return "", err
	}

	expr, ok := raw.(string)
	if !ok {
		return "", fmt.Errorf("milvus 过滤表达式转换结果非字符串: %T", raw)
	}
	return expr, nil
}

// hybridSearchFallback 降级混合搜索：分别执行向量搜索和 BM25 搜索后 RRF 融合。
//
// 对齐 Python MilvusVectorStore._hybrid_search_fallback：
// 并发两路搜索（vector + text）→ rrf_fusion([results_vector, results_text], k=60) → 截断 top_k。
// M-27: filters 对齐 Python dict | QueryExpr（fallback 内部两路搜索均传 nil，
// 对齐 Python _hybrid_search_fallback 显式传 None）。
func (s *MilvusVectorStore) hybridSearchFallback(ctx context.Context, queryText string, queryVector []float64, topK int, filters query.QueryExpr) ([]common.SearchResult, error) {
	// 对齐 Python: asyncio.create_task 并发两路搜索
	type searchOut struct {
		results []common.SearchResult
		err     error
	}
	outCh := make(chan searchOut, 2)

	go func() {
		if queryVector != nil {
			// 对齐 Python: self.search(query_vector, top_k, None)
			results, err := s.Search(ctx, queryVector, topK, nil)
			outCh <- searchOut{results: results, err: err}
		} else {
			// 对齐 Python: asyncio.sleep(0, result=[]) → 空结果
			outCh <- searchOut{}
		}
	}()
	go func() {
		// 对齐 Python: self.sparse_search(query_text, top_k, None)
		results, err := s.SparseSearch(ctx, queryText, topK, nil)
		outCh <- searchOut{results: results, err: err}
	}()

	var resultsList [][]common.RetrievalResult
	// idMapping text → ID 映射，供融合后回填（Go RRFFusion 入参无 ID 字段的必要补偿，
	// 模式对齐 chroma.go hybridSearch 与 Python chroma_store.hybrid_search 的 id_mapping）
	idMapping := make(map[string]string)

	for i := 0; i < 2; i++ {
		out := <-outCh
		if out.err != nil {
			// M-01: 对齐 Python await 语义——fallback 内单路异常使整个降级搜索失败上抛
			// （Python await task_vector 直接传播异常，无静默单路降级）
			logger.Warn(logComponent).
				Str("method", "HybridSearchFallback").
				Str("collection_name", s.collectionName).
				Err(out.err).
				Msg("hybrid fallback search path failed")
			return nil, out.err
		}
		if len(out.results) == 0 {
			continue
		}
		var retrievalResults []common.RetrievalResult
		for _, sr := range out.results {
			// M-01: 首次保留，对齐 Python fusion.py L41-42 if key not in result_dict
			if _, exists := idMapping[sr.Text]; !exists {
				idMapping[sr.Text] = sr.ID
			}
			// 拷贝 metadata 后写入 id（避免污染原始 map，供融合后恢复）
			metadata := make(map[string]any, len(sr.Metadata)+1)
			for k, v := range sr.Metadata {
				metadata[k] = v
			}
			metadata["id"] = sr.ID
			docID, _ := sr.Metadata["doc_id"].(string)
			chunkID, _ := sr.Metadata["chunk_id"].(string)
			retrievalResults = append(retrievalResults, common.RetrievalResult{
				Text:     sr.Text,
				Score:    sr.Score,
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

	// 对齐 Python: fused_results = rrf_fusion([results_vector, results_text], k=60)
	fusedRetrievalResults := utils.RRFFusion(resultsList, 60)

	// 对齐 Python: return fused_results[:top_k]
	var fusedResults []common.SearchResult
	for i, rr := range fusedRetrievalResults {
		if i >= topK {
			break
		}
		// 融合后从 metadata（或 idMapping 兜底）恢复 ID
		resultID, _ := rr.Metadata["id"].(string)
		if resultID == "" {
			resultID = idMapping[rr.Text]
		}
		// 移除临时写入的 id 字段，避免污染下游 metadata
		fusedMetadata := make(map[string]any, len(rr.Metadata))
		for k, v := range rr.Metadata {
			if k != "id" {
				fusedMetadata[k] = v
			}
		}
		fusedResults = append(fusedResults, common.SearchResult{
			ID:       resultID,
			Text:     rr.Text,
			Score:    rr.Score,
			Metadata: fusedMetadata,
		})
	}
	return fusedResults, nil
}

// milvusResultToSearchResults 将 Milvus 搜索结果转为 SearchResult 列表。
//
// 对齐 Python MilvusVectorStore._milvus_result_to_search_results：
//   - 提取 text/metadata/doc_id/chunk_id
//   - doc_id 回填到 metadata（避免重复）
//   - chunk_id 写入 metadata（供上层关联）
//   - vector 模式按距离度量归一化分数；sparse/hybrid 直接用原始分数
//   - raw_score / raw_score_scaled 写入 metadata
func (s *MilvusVectorStore) milvusResultToSearchResults(rs milvusclient.ResultSet, mode string) []common.SearchResult {
	rowCount := rs.ResultCount
	if rowCount == 0 {
		return nil
	}

	// 收集列数据
	colData := make(map[string][]any)
	for _, col := range rs.Fields {
		name := col.Name()
		values := make([]any, rowCount)
		for j := 0; j < rowCount; j++ {
			v, err := col.Get(j)
			if err != nil {
				continue
			}
			values[j] = v
		}
		colData[name] = values
	}

	var searchResults []common.SearchResult
	for j := 0; j < rowCount; j++ {
		// 对齐 Python: result_id = str(item.get("id", item.get("pk", "")))
		resultID := ""
		if ids := rs.IDs; ids != nil {
			if v, err := ids.Get(j); err == nil {
				resultID = fmt.Sprintf("%v", v)
			}
		}

		// 对齐 Python: text = item.get(self.text_field, "")
		text := ""
		if vals := colData[s.textField]; j < len(vals) {
			if str, ok := vals[j].(string); ok {
				text = str
			}
		}

		// 对齐 Python: metadata = item.get(self.metadata_field, {})；str 时 json.loads
		metadata := make(map[string]any)
		if vals := colData[s.metadataField]; j < len(vals) {
			switch m := vals[j].(type) {
			case map[string]any:
				for k, v := range m {
					metadata[k] = v
				}
			case string:
				var parsed map[string]any
				if err := json.Unmarshal([]byte(m), &parsed); err == nil {
					for k, v := range parsed {
						metadata[k] = v
					}
				}
			case []byte:
				var parsed map[string]any
				if err := json.Unmarshal(m, &parsed); err == nil {
					for k, v := range parsed {
						metadata[k] = v
					}
				}
			}
		}

		// 对齐 Python: if "doc_id" not in metadata: metadata["doc_id"] = item.pop(doc_id_field, None)
		if _, ok := metadata["doc_id"]; !ok {
			if vals := colData[s.docIDField]; j < len(vals) {
				if str, ok := vals[j].(string); ok {
					metadata["doc_id"] = str
				}
			}
		}

		// 对齐 Python: if item.get("chunk_id") is not None: metadata.setdefault("chunk_id", ...)
		if vals := colData["chunk_id"]; j < len(vals) {
			if str, ok := vals[j].(string); ok && str != "" {
				metadata["chunk_id"] = str
			}
		}

		// 对齐 Python: raw_score = item.get("score", item.get("distance", None))
		var rawScoreVal *float64
		if j < len(rs.Scores) {
			v := float64(rs.Scores[j])
			rawScoreVal = &v
		}

		var finalScore float64
		var rawScoreScaled *float64

		if mode == "vector" && rawScoreVal != nil {
			// 对齐 Python: L2/COSINE/IP 三种归一化
			switch s.distanceMetric {
			case "L2":
				scaled := vector.ConvertL2Squared(*rawScoreVal, 4.0)
				rawScoreScaled = &scaled
				finalScore = scaled
			case "COSINE":
				scaled := vector.ConvertCosineSimilarity(*rawScoreVal)
				rawScoreScaled = &scaled
				finalScore = scaled
			default: // IP
				scaled := vector.ConvertIPSimilarity(*rawScoreVal)
				rawScoreScaled = &scaled
				finalScore = scaled
			}
		} else if rawScoreVal != nil {
			// 对齐 Python: sparse/hybrid: final_score = raw_score
			finalScore = *rawScoreVal
		}

		// 对齐 Python: metadata.setdefault("raw_score", raw_score_val)
		if rawScoreVal != nil {
			metadata["raw_score"] = *rawScoreVal
		}
		if rawScoreScaled != nil {
			metadata["raw_score_scaled"] = *rawScoreScaled
		}

		searchResults = append(searchResults, common.SearchResult{
			ID:       resultID,
			Text:     text,
			Score:    finalScore,
			Metadata: metadata,
		})
	}
	return searchResults
}

// getSearchParams 获取搜索参数。
//
// 对齐 Python MilvusVectorStore.get_search_params：
// efSearchFactor 存在时转为 ef = round(top_k * efSearchFactor)。
// S-47: vf tag name= 修复后 searchConfig 键名为 efSearchFactor（与 Python 一致）。
func (s *MilvusVectorStore) getSearchParams(topK int) map[string]any {
	// 对齐 Python: search_params = self._search_config（复制，不修改原配置）
	searchParams := make(map[string]any)
	for k, v := range s.searchConfig {
		searchParams[k] = v
	}
	// 对齐 Python: if "efSearchFactor" in search_params: ef = round(top_k * pop("efSearchFactor"))
	if factor, ok := searchParams["efSearchFactor"]; ok {
		delete(searchParams, "efSearchFactor")
		if f, ok := factor.(float64); ok {
			searchParams["ef"] = float64(math.Round(float64(topK) * f))
		}
	}
	return searchParams
}
