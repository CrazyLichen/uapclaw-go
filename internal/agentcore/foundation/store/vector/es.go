package vector

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	elasticsearch8 "github.com/elastic/go-elasticsearch/v8"
	"github.com/elastic/go-elasticsearch/v8/esapi"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/vector_fields"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// esClient ES 客户端抽象接口，用于解耦和测试 mock。
type esClient interface {
	Do(ctx context.Context, req esapi.Request) (*esapi.Response, error)
	Close()
}

// esClientWrapper 封装 *elasticsearch8.Client，实现 esClient 接口。
type esClientWrapper struct {
	inner *elasticsearch8.Client
}

// ESVectorStore 基于 Elasticsearch 的向量存储实现。
//
// 使用 ES 8.x 的 dense_vector 字段和 k-NN 搜索实现向量存储。
// 客户端惰性创建，初始化时不需要 ES 可用。
// 元数据通过 ES _meta 文档持久化，同时维护内存缓存加速读取，
// 缓存未命中或进程重启时从 ES _meta 文档回查。
//
// 对应 Python: vector/es_vector_store.py (ESVectorStore)
type ESVectorStore struct {
	// client ES 客户端实例
	client esClient
	// addresses ES 节点地址列表
	addresses []string
	// username ES 认证用户名
	username string
	// password ES 认证密码
	password string
	// indexPrefix ES 索引名前缀，索引名格式为 {indexPrefix}__{collectionName}
	indexPrefix string
	// metadataCache 集合元数据内存缓存，key 为 indexName
	// 优先查缓存，缓存未命中时从 ES _meta 文档加载
	metadataCache map[string]map[string]any
	// mu 读写锁，保护客户端创建和元数据缓存
	mu sync.RWMutex
	// createClient 客户端创建函数，用于依赖注入和测试
	createClient func(addresses []string, username, password string) (esClient, error)
}

// ──────────────────────────── 枚举 ────────────────────────────

// ESOption ESVectorStore 构造选项
type ESOption func(*ESVectorStore)

// ──────────────────────────── 常量 ────────────────────────────
const (
	// esDefaultBatchSize ES 默认批量插入大小
	esDefaultBatchSize = 500
	// esDefaultDistanceMetric ES 默认距离度量
	esDefaultDistanceMetric = "COSINE"
	// esDefaultIndexPrefix ES 默认索引名前缀
	esDefaultIndexPrefix = "agent_vector"
	// esMetaDocumentID ES _meta 文档 ID，用于持久化集合元数据
	esMetaDocumentID = "__collection_metadata__"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// esLogComponent ES 日志组件
var esLogComponent = logger.ComponentAgentCore

// ──────────────────────────── 导出函数 ────────────────────────────

// WithESIndexPrefix 设置 ES 索引名前缀
func WithESIndexPrefix(prefix string) ESOption {
	return func(s *ESVectorStore) { s.indexPrefix = prefix }
}

// NewESVectorStore 创建 ESVectorStore 实例。
// 客户端惰性创建，初始化时不需要 ES 可用。
func NewESVectorStore(addresses []string, username, password string, opts ...ESOption) *ESVectorStore {
	s := &ESVectorStore{
		addresses:     addresses,
		username:      username,
		password:      password,
		indexPrefix:   esDefaultIndexPrefix,
		metadataCache: make(map[string]map[string]any),
		createClient: func(addrs []string, user, pass string) (esClient, error) {
			cfg := elasticsearch8.Config{
				Addresses: addrs,
				Username:  user,
				Password:  pass,
			}
			c, err := elasticsearch8.NewClient(cfg)
			if err != nil {
				return nil, fmt.Errorf("创建 ES 客户端失败: %w", err)
			}
			return &esClientWrapper{inner: c}, nil
		},
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Close 关闭 ES 客户端连接。
func (s *ESVectorStore) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.client != nil {
		s.client.Close()
		s.client = nil
	}
	// 清空元数据缓存（对齐 Python 实现）
	s.metadataCache = make(map[string]map[string]any)
	logger.Info(esLogComponent).Str("action", "close").Msg("ES 客户端连接已关闭")
}

// CreateCollection 创建集合（ES 索引）。
// 从 schema 构建 ES mapping，包含 dynamic:strict 和向量字段的 dense_vector 配置。
//
// 对应 Python: ESVectorStore.create_collection()
func (s *ESVectorStore) CreateCollection(ctx context.Context, collectionName string, schema *CollectionSchema, opts ...Option) error {
	o := newOptions(opts...)

	// 校验 Schema 必须包含至少一个 FLOAT_VECTOR 字段
	if len(schema.GetVectorFields()) == 0 {
		return exception.BuildError(exception.StatusStoreVectorSchemaInvalid,
			exception.WithParam("error_msg", "Schema 必须包含至少一个 FLOAT_VECTOR 字段"),
		)
	}

	c, err := s.getClient()
	if err != nil {
		return err
	}

	indexName := s.esIndexName(collectionName)

	// 检查索引是否已存在
	exists, err := s.esIndicesExists(ctx, c, indexName)
	if err != nil {
		return fmt.Errorf("CreateCollection: %w", err)
	}
	if exists {
		logger.Info(esLogComponent).
			Str("collection_name", collectionName).
			Str("index_name", indexName).
			Msg("集合已存在，跳过创建")
		return nil
	}

	distanceMetric := o.DistanceMetric
	if distanceMetric == "" {
		distanceMetric = esDefaultDistanceMetric
	}
	distanceMetric = strings.ToUpper(distanceMetric)

	// 解析向量字段配置
	vectorFieldConfig := s.resolveESVectorField(o)

	// 构建 mapping
	mapping := esBuildMappings(schema, distanceMetric, vectorFieldConfig)

	createBody := map[string]any{"mappings": mapping}
	mappingBytes, err := json.Marshal(createBody)
	if err != nil {
		return fmt.Errorf("CreateCollection: 序列化 mapping 失败: %w", err)
	}

	// 创建索引
	req := esapi.IndicesCreateRequest{
		Index: indexName,
		Body:  bytes.NewReader(mappingBytes),
	}
	resp, err := c.Do(ctx, &req)
	if err != nil {
		logger.Error(esLogComponent).Err(err).
			Str("collection_name", collectionName).
			Str("event_type", "STORE_CREATE_ERROR").
			Msg("创建集合失败")
		return fmt.Errorf("CreateCollection: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.IsError() {
		body, _ := io.ReadAll(resp.Body)
		// 并发创建时可能因索引已存在返回 resource_already_exists_exception，静默返回（对齐 Python 实现）
		if strings.Contains(string(body), "resource_already_exists_exception") {
			logger.Warn(esLogComponent).
				Str("collection_name", collectionName).
				Str("status", resp.Status()).
				Msg("集合已存在（并发创建），跳过")
			return nil
		}
		logger.Error(esLogComponent).
			Str("collection_name", collectionName).
			Str("event_type", "STORE_CREATE_ERROR").
			Str("status", resp.Status()).
			Str("response", string(body)).
			Msg("创建集合失败")
		return exception.BuildError(exception.StatusStoreVectorSchemaInvalid,
			exception.WithParam("error_msg", fmt.Sprintf("ES 创建索引失败: %s", string(body))),
		)
	}

	// 获取向量字段信息
	vectorFields := schema.GetVectorFields()
	vf := vectorFields[0]
	vectorDim := vf.Dim

	// 持久化集合元数据
	metadata := map[string]any{
		"schema":          schema.ToDict(),
		"distance_metric": distanceMetric,
		"vector_field":    vf.Name,
		"vector_dim":      vectorDim,
		"schema_version":  0,
		"collection_name": collectionName,
	}
	if vectorFieldConfig != nil {
		metadata["es_index_config"] = vectorFieldConfig
	}
	if err := esStoreMetadata(ctx, c, indexName, metadata); err != nil {
		logger.Warn(esLogComponent).Err(err).
			Str("collection_name", collectionName).
			Msg("存储集合元数据失败，非致命错误")
	}
	// 写入缓存（对齐 Python 实现）
	s.mu.Lock()
	s.metadataCache[indexName] = metadata
	s.mu.Unlock()

	logger.Info(esLogComponent).
		Str("collection_name", collectionName).
		Str("index_name", indexName).
		Str("distance_metric", distanceMetric).
		Msg("创建集合成功")

	return nil
}

// DeleteCollection 删除集合（ES 索引）。
// 先检查索引是否存在，不存在时记录 Warn 日志并返回 nil（对齐 Python 实现）。
//
// 对应 Python: ESVectorStore.delete_collection()
func (s *ESVectorStore) DeleteCollection(ctx context.Context, collectionName string, opts ...Option) error {
	c, err := s.getClient()
	if err != nil {
		return err
	}

	indexName := s.esIndexName(collectionName)

	// 先检查索引是否存在（对齐 Python 实现）
	exists, err := s.esIndicesExists(ctx, c, indexName)
	if err != nil {
		return fmt.Errorf("DeleteCollection: %w", err)
	}
	if !exists {
		logger.Warn(esLogComponent).
			Str("collection_name", collectionName).
			Msg("集合不存在，跳过删除")
		return nil
	}

	req := esapi.IndicesDeleteRequest{Index: []string{indexName}}
	resp, err := c.Do(ctx, &req)
	if err != nil {
		logger.Error(esLogComponent).Err(err).
			Str("collection_name", collectionName).
			Str("event_type", "STORE_DELETE_ERROR").
			Msg("删除集合失败")
		return fmt.Errorf("DeleteCollection: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.IsError() {
		body, _ := io.ReadAll(resp.Body)
		logger.Error(esLogComponent).
			Str("collection_name", collectionName).
			Str("event_type", "STORE_DELETE_ERROR").
			Str("status", resp.Status()).
			Str("response", string(body)).
			Msg("删除集合失败")
		return exception.BuildError(exception.StatusStoreVectorSchemaInvalid,
			exception.WithParam("error_msg", fmt.Sprintf("ES 删除索引失败: %s", string(body))),
		)
	}

	// 清除缓存（对齐 Python 实现）
	s.mu.Lock()
	delete(s.metadataCache, indexName)
	s.mu.Unlock()

	logger.Info(esLogComponent).
		Str("collection_name", collectionName).
		Str("index_name", indexName).
		Msg("删除集合成功")

	return nil
}

// CollectionExists 检查集合是否存在。
//
// 对应 Python: ESVectorStore.collection_exists()
func (s *ESVectorStore) CollectionExists(ctx context.Context, collectionName string, opts ...Option) (bool, error) {
	c, err := s.getClient()
	if err != nil {
		return false, err
	}

	indexName := s.esIndexName(collectionName)
	return s.esIndicesExists(ctx, c, indexName)
}

// GetSchema 获取集合的 Schema。
// 先尝试从 _meta 文档的 schema 字段获取，失败则从 ES mapping 反射构建。
//
// 对应 Python: ESVectorStore.get_schema()
func (s *ESVectorStore) GetSchema(ctx context.Context, collectionName string, opts ...Option) (*CollectionSchema, error) {
	c, err := s.getClient()
	if err != nil {
		return nil, err
	}

	indexName := s.esIndexName(collectionName)

	// 优先从 _meta 文档获取 schema
	metadata, err := s.esLoadMetadata(ctx, c, indexName)
	if err == nil {
		if schemaDict, ok := metadata["schema"].(map[string]any); ok {
			schema, err := CollectionFromDict(schemaDict)
			if err == nil && schema != nil {
				return schema, nil
			}
		}
	}

	// 回退：从 ES mapping 反射构建 schema
	return s.esBuildSchemaFromMapping(ctx, c, indexName)
}

// AddDocs 添加文档到集合。
// 按 BatchSize 构建 NDJSON bulk 请求分批插入。
//
// 对应 Python: ESVectorStore.add_docs()
func (s *ESVectorStore) AddDocs(ctx context.Context, collectionName string, docs []map[string]any, opts ...Option) error {
	if len(docs) == 0 {
		return nil
	}

	o := newOptions(opts...)
	batchSize := o.BatchSize
	if batchSize <= 0 {
		batchSize = esDefaultBatchSize
	}

	c, err := s.getClient()
	if err != nil {
		return err
	}

	indexName := s.esIndexName(collectionName)

	pkField := ""
	// 尝试从 _meta 获取主键字段名
	metadata, metaErr := s.esLoadMetadata(ctx, c, indexName)
	if metaErr == nil {
		if schemaDict, ok := metadata["schema"].(map[string]any); ok {
			pkField = esGetPrimaryKeyField(schemaDict)
		}
	}

	for i := 0; i < len(docs); i += batchSize {
		end := i + batchSize
		if end > len(docs) {
			end = len(docs)
		}
		batch := docs[i:end]

		body := esBuildBulkRequestBody(batch, indexName, pkField)
		if body == nil {
			continue
		}

		req := esapi.BulkRequest{Body: body}
		respBody, err := esDoRequest(ctx, c, &req)
		if err != nil {
			logger.Error(esLogComponent).Err(err).
				Str("collection_name", collectionName).
				Int("doc_count", len(docs)).
				Int("batch_size", batchSize).
				Str("event_type", "STORE_WRITE_ERROR").
				Msg("插入文档失败")
			return fmt.Errorf("AddDocs: %w", err)
		}

		// 检查 bulk 响应中的错误
		if errs, ok := respBody["errors"].(bool); ok && errs {
			logger.Warn(esLogComponent).
				Str("collection_name", collectionName).
				Interface("response", respBody).
				Msg("批量插入部分文档失败")
		}
	}

	// 刷新索引使文档可搜索
	if err := s.esIndicesRefresh(ctx, c, indexName); err != nil {
		logger.Warn(esLogComponent).Err(err).
			Str("collection_name", collectionName).
			Msg("刷新索引失败，非致命错误")
	}

	logger.Info(esLogComponent).
		Str("collection_name", collectionName).
		Int("doc_count", len(docs)).
		Int("batch_size", batchSize).
		Msg("插入文档成功")

	return nil
}

// Search 向量相似度搜索。
// 使用 ES k-NN 搜索，构建 knn 查询子句。
//
// 对应 Python: ESVectorStore.search()
func (s *ESVectorStore) Search(ctx context.Context, collectionName string, queryVector []float64, vectorField string, topK int, filters map[string]any, opts ...Option) ([]VectorSearchResult, error) {
	o := newOptions(opts...)
	if topK <= 0 {
		topK = 5
	}

	c, err := s.getClient()
	if err != nil {
		return nil, err
	}

	indexName := s.esIndexName(collectionName)

	// 优先从 _meta 元数据获取距离度量（对齐 Python 实现）
	distanceMetric := ""
	metadata, metaErr := s.esLoadMetadata(ctx, c, indexName)
	if metaErr == nil {
		if dm, ok := metadata["distance_metric"].(string); ok && dm != "" {
			distanceMetric = dm
		}
	}
	if distanceMetric == "" {
		distanceMetric = o.DistanceMetric
	}
	if distanceMetric == "" {
		distanceMetric = esDefaultDistanceMetric
	}
	distanceMetric = strings.ToUpper(distanceMetric)

	numCandidates := o.NumCandidates
	if numCandidates <= 0 {
		numCandidates = topK * 10
		if numCandidates < 100 {
			numCandidates = 100
		}
	}

	// 构建 k-NN 搜索请求
	knnClause := map[string]any{
		"field":          vectorField,
		"query_vector":   queryVector,
		"k":              topK,
		"num_candidates": numCandidates,
	}

	// 添加过滤条件到 knn.filter（对齐 Python 实现）
	if len(filters) > 0 {
		filterClause := esBuildFilterClause(filters)
		knnClause["filter"] = map[string]any{
			"bool": map[string]any{
				"filter": filterClause,
			},
		}
	}

	searchBody := map[string]any{
		"knn":  knnClause,
		"size": topK,
	}

	// 添加输出字段，排除 _meta
	// 注意：Go 使用 _source.includes 限定返回字段，使 output_fields 参数真正生效。
	// Python 的 output_fields 参数未实际生效——即使传了 output_fields，Python 也只设
	// excludes=["_meta"] 而不设 includes，导致始终返回所有字段（排除 _meta）。
	// Go 的白名单语义更合理，此处保持 Go 行为。
	if len(o.OutputFields) > 0 {
		searchBody["_source"] = map[string]any{
			"includes": o.OutputFields,
			"excludes": []string{"_meta"},
		}
	} else {
		searchBody["_source"] = map[string]any{
			"excludes": []string{"_meta"},
		}
	}

	searchBytes, err := json.Marshal(searchBody)
	if err != nil {
		return nil, fmt.Errorf("Search: 序列化搜索请求失败: %w", err)
	}

	req := esapi.SearchRequest{
		Index: []string{indexName},
		Body:  bytes.NewReader(searchBytes),
	}

	respBody, err := esDoRequest(ctx, c, &req)
	if err != nil {
		logger.Error(esLogComponent).Err(err).
			Str("collection_name", collectionName).
			Str("event_type", "STORE_SEARCH_ERROR").
			Msg("搜索失败")
		return nil, fmt.Errorf("Search: %w", err)
	}

	// 解析搜索结果
	hits, _ := respBody["hits"].(map[string]any)
	hitList, _ := hits["hits"].([]any)

	results := make([]VectorSearchResult, 0, len(hitList))
	for _, hitAny := range hitList {
		hit, ok := hitAny.(map[string]any)
		if !ok {
			continue
		}

		// 提取分数
		// ES k-NN 的 _score 已经是 [0,1] 归一化的相似度分数，直接使用原始值。
		// COSINE: _score = (1 + cosine) / 2 ∈ (0,1]
		// L2: _score = 1 / (1 + dist²) ∈ (0,1]
		// DOT_PRODUCT: _score = (1 + dp) / 2 ∈ [0,1]
		// 注意：不可再做 esNormalizeScore 转换，否则双重归一化且公式不匹配。
		score, _ := hit["_score"].(float64)

		// 提取字段
		fields := make(map[string]any)
		if source, ok := hit["_source"].(map[string]any); ok {
			for k, v := range source {
				if k == "_meta" {
					continue
				}
				fields[k] = v
			}
		}

		// 回填 _id 到 id 字段（对齐 Python 实现）
		if _, hasID := fields["id"]; !hasID {
			if id, ok := hit["_id"].(string); ok {
				fields["id"] = id
			}
		}

		results = append(results, VectorSearchResult{
			Score:  score,
			Fields: fields,
		})
	}

	logger.Info(esLogComponent).
		Str("collection_name", collectionName).
		Int("top_k", topK).
		Str("distance_metric", distanceMetric).
		Int("result_count", len(results)).
		Msg("搜索完成")

	return results, nil
}

// DeleteDocsByIDs 按 ID 删除文档。
// 构建 NDJSON bulk delete 请求，按 batch_size 分批删除。
//
// 对应 Python: ESVectorStore.delete_docs_by_ids()
func (s *ESVectorStore) DeleteDocsByIDs(ctx context.Context, collectionName string, ids []string, opts ...Option) error {
	if len(ids) == 0 {
		return nil
	}

	c, err := s.getClient()
	if err != nil {
		return err
	}

	o := newOptions(opts...)
	batchSize := o.BatchSize
	if batchSize <= 0 {
		batchSize = esDefaultBatchSize
	}

	indexName := s.esIndexName(collectionName)

	// 分批删除，对齐 Python 按 batch_size 分批的行为
	for i := 0; i < len(ids); i += batchSize {
		end := i + batchSize
		if end > len(ids) {
			end = len(ids)
		}
		batch := ids[i:end]

		body := esBuildBulkDeleteBody(batch, indexName)
		if body == nil {
			continue
		}

		req := esapi.BulkRequest{Body: body}
		respBody, err := esDoRequest(ctx, c, &req)
		if err != nil {
			logger.Error(esLogComponent).Err(err).
				Str("collection_name", collectionName).
				Int("batch_start", i).
				Int("batch_size", len(batch)).
				Str("event_type", "STORE_DELETE_ERROR").
				Msg("按 ID 删除文档批次失败")
			return fmt.Errorf("DeleteDocsByIDs batch %d-%d: %w", i, end, err)
		}

		if errs, ok := respBody["errors"].(bool); ok && errs {
			logger.Warn(esLogComponent).
				Str("collection_name", collectionName).
				Int("batch_start", i).
				Interface("response", respBody).
				Msg("批量删除部分文档失败")
		}
	}

	// 刷新索引
	if err := s.esIndicesRefresh(ctx, c, indexName); err != nil {
		logger.Warn(esLogComponent).Err(err).
			Str("collection_name", collectionName).
			Msg("刷新索引失败，非致命错误")
	}

	logger.Info(esLogComponent).
		Str("collection_name", collectionName).
		Int("id_count", len(ids)).
		Int("batch_size", batchSize).
		Msg("按 ID 删除文档成功")

	return nil
}

// DeleteDocsByFilters 按标量字段过滤条件删除文档。
// 使用 ES delete_by_query API。
//
// 对应 Python: ESVectorStore.delete_docs_by_filters()
func (s *ESVectorStore) DeleteDocsByFilters(ctx context.Context, collectionName string, filters map[string]any, opts ...Option) error {
	if len(filters) == 0 {
		return nil
	}

	c, err := s.getClient()
	if err != nil {
		return err
	}

	indexName := s.esIndexName(collectionName)

	filterClause := esBuildFilterClause(filters)
	queryBody := map[string]any{
		"query": map[string]any{
			"bool": map[string]any{
				"filter": filterClause,
			},
		},
	}

	queryBytes, err := json.Marshal(queryBody)
	if err != nil {
		return fmt.Errorf("DeleteDocsByFilters: 序列化查询失败: %w", err)
	}

	req := esapi.DeleteByQueryRequest{
		Index:   []string{indexName},
		Body:    bytes.NewReader(queryBytes),
		Refresh: esapi.BoolPtr(true),
	}

	_, err = esDoRequest(ctx, c, &req)
	if err != nil {
		logger.Error(esLogComponent).Err(err).
			Str("collection_name", collectionName).
			Str("event_type", "STORE_DELETE_ERROR").
			Msg("按过滤条件删除文档失败")
		return fmt.Errorf("DeleteDocsByFilters: %w", err)
	}

	logger.Info(esLogComponent).
		Str("collection_name", collectionName).
		Int("filter_count", len(filters)).
		Msg("按过滤条件删除文档成功")

	return nil
}

// ListCollectionNames 列出所有集合名称。
// 通过 ES indices.get API 获取匹配 indexPrefix 的索引列表。
//
// 对应 Python: ESVectorStore.list_collection_names()
//
// 注意：Python 异常时返回空列表 []，Go 返回 error。Go 更严格但行为不同，
// 此处保持 Go 返回 error 的方式，调用方必须处理异常情况。
func (s *ESVectorStore) ListCollectionNames(ctx context.Context) ([]string, error) {
	c, err := s.getClient()
	if err != nil {
		return nil, err
	}

	names, err := esListIndices(ctx, c, s.indexPrefix)
	if err != nil {
		return nil, fmt.Errorf("ListCollectionNames: %w", err)
	}
	return names, nil
}

// UpdateSchema 执行 Schema 迁移操作。
// ⤵️ 预留：实际迁移逻辑待 7.22/7.23 实现后回填。
//
// 对应 Python: ESVectorStore.update_schema()
func (s *ESVectorStore) UpdateSchema(ctx context.Context, collectionName string, operations []any, opts ...Option) error {
	return exception.BuildError(exception.StatusStoreVectorSchemaInvalid,
		exception.WithParam("error_msg", "UpdateSchema 未实现，待 7.22/7.23 回填"),
	)
}

// UpdateCollectionMetadata 更新集合元数据。
// 将元数据持久化到 ES _meta 文档。
//
// 对应 Python: ESVectorStore.update_collection_metadata()
func (s *ESVectorStore) UpdateCollectionMetadata(ctx context.Context, collectionName string, metadata map[string]any, opts ...Option) error {
	if len(metadata) == 0 {
		return nil
	}

	// 校验 schema_version（对齐 Python 实现）
	if v, ok := metadata["schema_version"]; ok {
		version, ok := v.(int)
		if !ok || version < 0 {
			return exception.BuildError(exception.StatusStoreVectorSchemaInvalid,
				exception.WithParam("error_msg", fmt.Sprintf("schema_version 必须为非负整数，当前值: %v", v)),
			)
		}
	}

	c, err := s.getClient()
	if err != nil {
		return err
	}

	indexName := s.esIndexName(collectionName)

	// 合并已有元数据
	existing, err := s.esLoadMetadata(ctx, c, indexName)
	if err != nil {
		existing = make(map[string]any)
	}
	for k, v := range metadata {
		existing[k] = v
	}

	if err := esStoreMetadata(ctx, c, indexName, existing); err != nil {
		logger.Error(esLogComponent).Err(err).
			Str("collection_name", collectionName).
			Str("event_type", "STORE_UPDATE_ERROR").
			Msg("更新集合元数据失败")
		return fmt.Errorf("UpdateCollectionMetadata: %w", err)
	}

	// 更新缓存（对齐 Python 实现）
	s.mu.Lock()
	s.metadataCache[indexName] = existing
	s.mu.Unlock()

	logger.Info(esLogComponent).
		Str("collection_name", collectionName).
		Msg("更新集合元数据成功")

	return nil
}

// GetCollectionMetadata 获取集合元数据。
// 从 ES _meta 文档加载。
//
// 对应 Python: ESVectorStore.get_collection_metadata()
func (s *ESVectorStore) GetCollectionMetadata(ctx context.Context, collectionName string, opts ...Option) (map[string]any, error) {
	c, err := s.getClient()
	if err != nil {
		return nil, err
	}

	indexName := s.esIndexName(collectionName)

	metadata, err := s.esLoadMetadata(ctx, c, indexName)
	if err != nil {
		return nil, fmt.Errorf("GetCollectionMetadata: %w", err)
	}

	// 设置默认值（对齐 Python 实现）
	if _, ok := metadata["distance_metric"]; !ok {
		metadata["distance_metric"] = esDefaultDistanceMetric
	}
	if _, ok := metadata["schema_version"]; !ok {
		metadata["schema_version"] = 0
	}

	return metadata, nil
}

// Do 实现 esClient 接口，将请求转发到内部客户端。
// 使用传入的 ctx 而非 context.Background()，确保请求级超时和取消信号生效。
func (w *esClientWrapper) Do(ctx context.Context, req esapi.Request) (*esapi.Response, error) {
	return req.Do(ctx, w.inner)
}

// Close 实现 esClient 接口，关闭内部客户端。
func (w *esClientWrapper) Close() {
	// elasticsearch8.Client 没有显式 Close 方法，依赖 GC 回收
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// getClient 惰性获取或创建 ES 客户端，双重检查锁。
func (s *ESVectorStore) getClient() (esClient, error) {
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

	c, err := s.createClient(s.addresses, s.username, s.password)
	if err != nil {
		logger.Error(esLogComponent).Err(err).
			Strs("addresses", s.addresses).
			Msg("创建 ES 客户端失败")
		return nil, exception.BuildError(exception.StatusStoreVectorSchemaInvalid,
			exception.WithParam("error_msg", fmt.Sprintf("创建 ES 客户端失败: %v", err)),
		)
	}
	s.client = c
	logger.Info(esLogComponent).
		Strs("addresses", s.addresses).
		Msg("成功连接 ES")
	return s.client, nil
}

// esIndexName 将集合名称转换为 ES 索引名。
func (s *ESVectorStore) esIndexName(collectionName string) string {
	return s.indexPrefix + "__" + collectionName
}

// resolveESVectorField 从 Options 解析 ES 向量字段配置。
func (s *ESVectorStore) resolveESVectorField(o Options) map[string]any {
	if o.VectorField != nil {
		if vf, ok := o.VectorField.(*vector_fields.ESVectorField); ok {
			result := map[string]any{
				"field_name": vf.VectorFieldName,
			}
			if vf.NumCandidates > 0 {
				result["num_candidates"] = vf.NumCandidates
			}
			if vf.ExtraConstruct != nil {
				for k, v := range vf.ExtraConstruct {
					result[k] = v
				}
			}
			if vf.ExtraSearch != nil {
				for k, v := range vf.ExtraSearch {
					result[k] = v
				}
			}
			return result
		}
	}
	return nil
}

// esMapFieldType 将 FieldSchema 映射为 ES mapping 字段类型。
func esMapFieldType(field *FieldSchema) map[string]any {
	switch field.DType {
	case VectorDataTypeFloatVector:
		dim := field.Dim
		if dim <= 0 {
			dim = 768
		}
		return map[string]any{
			"type":       "dense_vector",
			"dims":       dim,
			"index":      true,
			"similarity": "cosine", // 默认，会在 esBuildMappings 中被覆盖
		}
	case VectorDataTypeVarchar:
		return map[string]any{
			"type": "keyword",
		}
	case VectorDataTypeInt64:
		return map[string]any{
			"type": "long",
		}
	case VectorDataTypeInt32, VectorDataTypeInt16, VectorDataTypeInt8:
		return map[string]any{
			"type": "integer",
		}
	case VectorDataTypeFloat:
		return map[string]any{
			"type": "float",
		}
	case VectorDataTypeDouble:
		return map[string]any{
			"type": "double",
		}
	case VectorDataTypeBool:
		return map[string]any{
			"type": "boolean",
		}
	case VectorDataTypeJSON, VectorDataTypeArray:
		return map[string]any{
			"type":    "object",
			"enabled": true,
		}
	default:
		return map[string]any{
			"type": "keyword",
		}
	}
}

// esMapTypeToOurType 将 ES 字段类型映射回 VectorDataType。
func esMapTypeToOurType(esType string) VectorDataType {
	switch strings.ToLower(esType) {
	case "dense_vector":
		return VectorDataTypeFloatVector
	case "keyword", "text":
		return VectorDataTypeVarchar
	case "long":
		return VectorDataTypeInt64
	case "integer":
		return VectorDataTypeInt32
	case "short":
		return VectorDataTypeInt16
	case "byte":
		return VectorDataTypeInt8
	case "float":
		return VectorDataTypeFloat
	case "double":
		return VectorDataTypeDouble
	case "boolean":
		return VectorDataTypeBool
	case "object":
		return VectorDataTypeJSON
	default:
		return VectorDataTypeVarchar
	}
}

// esMapDistanceMetricToSimilarity 将距离度量映射为 ES similarity 类型。
func esMapDistanceMetricToSimilarity(metric string) string {
	switch strings.ToUpper(metric) {
	case "COSINE":
		return "cosine"
	case "L2":
		return "l2_norm"
	case "IP":
		return "dot_product"
	default:
		return "cosine"
	}
}

// esBuildMappings 构建 ES 索引的完整 mapping 定义。
// vectorFieldConfig 包含从 ESVectorField 解析的额外索引参数（如 m、ef_construction），
// 这些参数会应用到向量字段的 dense_vector mapping 中。
func esBuildMappings(schema *CollectionSchema, distanceMetric string, vectorFieldConfig map[string]any) map[string]any {
	similarity := esMapDistanceMetricToSimilarity(distanceMetric)

	properties := make(map[string]any)
	for _, field := range schema.Fields() {
		fieldMapping := esMapFieldType(field)
		// 设置向量字段的 similarity
		if field.DType == VectorDataTypeFloatVector {
			fieldMapping["similarity"] = similarity
			// 将 ESVectorField 的 construct 阶段参数写入 mapping
			// 支持 HNSW 参数：m、ef_construction 等
			for k, v := range vectorFieldConfig {
				switch k {
				case "field_name", "num_candidates":
					// 跳过非 mapping 参数
					continue
				default:
					fieldMapping[k] = v
				}
			}
		}
		properties[field.Name] = fieldMapping
	}

	// _meta 字段设为 enabled:false，不被索引但可存储
	properties["_meta"] = map[string]any{
		"type":    "object",
		"enabled": false,
	}

	return map[string]any{
		"dynamic":    "strict",
		"properties": properties,
	}
}

// esNormalizeScore 将 ES 原始分数转换为归一化相似度 [0, 1]。
func esNormalizeScore(rawScore float64, metric string) float64 {
	switch strings.ToUpper(metric) {
	case "COSINE":
		return ConvertCosineDistance(rawScore)
	case "L2":
		return ConvertL2Squared(rawScore, 4.0)
	case "IP":
		return ConvertIPSimilarity(rawScore)
	default:
		return ConvertCosineDistance(rawScore)
	}
}

// esBuildBulkRequestBody 构建批量插入的 NDJSON 请求体。
func esBuildBulkRequestBody(docs []map[string]any, indexName, pkField string) io.Reader {
	var buf bytes.Buffer
	for _, doc := range docs {
		// 提取文档 ID
		docID := ""
		if pkField != "" {
			if id, ok := doc[pkField]; ok {
				docID = fmt.Sprintf("%v", id)
			}
		}

		// 构建动作行
		action := map[string]any{
			"index": map[string]any{
				"_index": indexName,
			},
		}
		if docID != "" {
			action["index"].(map[string]any)["_id"] = docID
		}
		actionLine, _ := json.Marshal(action)
		buf.Write(actionLine)
		buf.WriteByte('\n')

		// 构建文档行，过滤 nil 值（对齐 Python 实现）
		filteredDoc := make(map[string]any)
		for k, v := range doc {
			if v != nil {
				filteredDoc[k] = v
			}
		}
		docLine, _ := json.Marshal(filteredDoc)
		buf.Write(docLine)
		buf.WriteByte('\n')
	}

	if buf.Len() == 0 {
		return nil
	}
	return bytes.NewReader(buf.Bytes())
}

// esBuildBulkDeleteBody 构建批量删除的 NDJSON 请求体。
func esBuildBulkDeleteBody(ids []string, indexName string) io.Reader {
	var buf bytes.Buffer
	for _, id := range ids {
		action := map[string]any{
			"delete": map[string]any{
				"_index": indexName,
				"_id":    id,
			},
		}
		actionLine, _ := json.Marshal(action)
		buf.Write(actionLine)
		buf.WriteByte('\n')
	}

	if buf.Len() == 0 {
		return nil
	}
	return bytes.NewReader(buf.Bytes())
}

// esBuildFilterClause 从过滤条件字典构建 ES bool.filter 子句。
// 单值 → term 查询，切片 → terms 查询，多条件组合 → bool.filter。
func esBuildFilterClause(filters map[string]any) []any {
	var clauses []any
	for key, val := range filters {
		switch v := val.(type) {
		case []any:
			clauses = append(clauses, esBuildFilterTerms(key, v))
		case []string:
			items := make([]any, len(v))
			for i, s := range v {
				items[i] = s
			}
			clauses = append(clauses, esBuildFilterTerms(key, items))
		default:
			clauses = append(clauses, map[string]any{
				"term": map[string]any{key: val},
			})
		}
	}
	return clauses
}

// esBuildFilterTerms 构建 ES terms 过滤子句。
func esBuildFilterTerms(field string, values []any) map[string]any {
	return map[string]any{
		"terms": map[string]any{field: values},
	}
}

// esGetPrimaryKeyField 从 metadata schema 字典中提取主键字段名。
func esGetPrimaryKeyField(schemaDict map[string]any) string {
	fields, ok := schemaDict["fields"].([]any)
	if !ok {
		return ""
	}
	for _, f := range fields {
		fd, ok := f.(map[string]any)
		if !ok {
			continue
		}
		if isPrimary, ok := fd["is_primary"].(bool); ok && isPrimary {
			if name, ok := fd["name"].(string); ok {
				return name
			}
		}
	}
	return ""
}

// esStoreMetadata 将集合元数据持久化到 ES _meta 文档。
// 文档体格式为 {"_meta": metadata}，与 Python 实现一致。
func esStoreMetadata(ctx context.Context, c esClient, indexName string, metadata map[string]any) error {
	doc := map[string]any{"_meta": metadata}
	docBytes, err := json.Marshal(doc)
	if err != nil {
		return fmt.Errorf("序列化元数据失败: %w", err)
	}

	req := esapi.IndexRequest{
		Index:      indexName,
		DocumentID: esMetaDocumentID,
		Body:       bytes.NewReader(docBytes),
		Refresh:    "true",
	}

	resp, err := c.Do(ctx, &req)
	if err != nil {
		return fmt.Errorf("存储元数据失败: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.IsError() {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("存储元数据失败，ES 响应: %s", string(body))
	}

	return nil
}

// esLoadMetadata 从缓存或 ES _meta 文档加载集合元数据。
// 优先查缓存（对齐 Python _metadata_cache），缓存未命中时从 ES 读取并写入缓存。
func (s *ESVectorStore) esLoadMetadata(ctx context.Context, c esClient, indexName string) (map[string]any, error) {
	// 优先查缓存
	s.mu.RLock()
	if meta, ok := s.metadataCache[indexName]; ok {
		s.mu.RUnlock()
		return meta, nil
	}
	s.mu.RUnlock()

	// 缓存未命中，从 ES _meta 文档加载
	req := esapi.GetRequest{
		Index:      indexName,
		DocumentID: esMetaDocumentID,
	}

	respBody, err := esDoRequest(ctx, c, &req)
	if err != nil {
		return nil, fmt.Errorf("加载元数据失败: %w", err)
	}

	// 提取 _source._meta
	source, ok := respBody["_source"].(map[string]any)
	if !ok {
		return make(map[string]any), nil
	}

	meta, ok := source["_meta"].(map[string]any)
	if !ok {
		return make(map[string]any), nil
	}

	// 写入缓存
	s.mu.Lock()
	s.metadataCache[indexName] = meta
	s.mu.Unlock()

	return meta, nil
}

// esDoRequest 执行 ES 请求并解析响应体。
func esDoRequest(ctx context.Context, c esClient, req esapi.Request) (map[string]any, error) {
	resp, err := c.Do(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("ES 请求失败: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取 ES 响应体失败: %w", err)
	}

	if resp.IsError() {
		return nil, fmt.Errorf("ES 请求返回错误: status=%s, body=%s", resp.Status(), string(body))
	}

	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("解析 ES 响应体失败: %w", err)
	}

	return result, nil
}

// esIndicesExists 检查 ES 索引是否存在。
func (s *ESVectorStore) esIndicesExists(ctx context.Context, c esClient, indexName string) (bool, error) {
	req := esapi.IndicesExistsRequest{Index: []string{indexName}}
	resp, err := c.Do(ctx, &req)
	if err != nil {
		return false, fmt.Errorf("检查索引是否存在失败: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusOK:
		return true, nil
	case http.StatusNotFound:
		return false, nil
	default:
		return false, fmt.Errorf("检查索引是否存在返回异常状态: %d", resp.StatusCode)
	}
}

// esIndicesRefresh 刷新 ES 索引。
func (s *ESVectorStore) esIndicesRefresh(ctx context.Context, c esClient, indexName string) error {
	req := esapi.IndicesRefreshRequest{Index: []string{indexName}}
	resp, err := c.Do(ctx, &req)
	if err != nil {
		return fmt.Errorf("刷新索引失败: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.IsError() {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("刷新索引失败: %s", string(body))
	}

	return nil
}

// esBuildSchemaFromMapping 从 ES mapping 反射构建 CollectionSchema。
func (s *ESVectorStore) esBuildSchemaFromMapping(ctx context.Context, c esClient, indexName string) (*CollectionSchema, error) {
	req := esapi.IndicesGetMappingRequest{Index: []string{indexName}}
	respBody, err := esDoRequest(ctx, c, &req)
	if err != nil {
		logger.Error(esLogComponent).Err(err).
			Str("index_name", indexName).
			Str("event_type", "STORE_SEARCH_ERROR").
			Msg("获取 ES mapping 失败")
		return nil, fmt.Errorf("esBuildSchemaFromMapping: %w", err)
	}

	// 解析 mapping
	indexData, ok := respBody[indexName].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("esBuildSchemaFromMapping: 索引 %s 的 mapping 未找到", indexName)
	}

	mappingsData, ok := indexData["mappings"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("esBuildSchemaFromMapping: 索引 %s 的 mappings 字段缺失", indexName)
	}

	properties, ok := mappingsData["properties"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("esBuildSchemaFromMapping: 索引 %s 的 properties 字段缺失", indexName)
	}

	schema, err := NewCollectionSchema(
		WithCollectionDescription(fmt.Sprintf("Collection '%s'", indexName)),
	)
	if err != nil {
		return nil, err
	}

	// Python 通过 kwargs.get("primary_key_field", "id") 参数标记主键字段，
	// Go 的 esBuildSchemaFromMapping 不接受 primary_key_field 参数，
	// 默认将名为 "id" 的字段标记为主键（与 Python 默认值一致）。
	const defaultPrimaryKeyField = "id"

	for fieldName, fieldDefAny := range properties {
		// 跳过 _meta 内部字段
		if fieldName == "_meta" {
			continue
		}

		fieldDef, ok := fieldDefAny.(map[string]any)
		if !ok {
			continue
		}

		esType, _ := fieldDef["type"].(string)
		ourType := esMapTypeToOurType(esType)

		fieldOpts := []FieldOption{}
		if ourType == VectorDataTypeFloatVector {
			if dims, ok := fieldDef["dims"]; ok {
				switch d := dims.(type) {
				case float64:
					fieldOpts = append(fieldOpts, WithDim(int(d)))
				case int:
					fieldOpts = append(fieldOpts, WithDim(d))
				}
			}
		}

		f, err := NewFieldSchema(fieldName, ourType, fieldOpts...)
		if err != nil {
			logger.Warn(esLogComponent).Err(err).
				Str("field_name", fieldName).
				Str("es_type", esType).
				Msg("从 mapping 构建字段失败，跳过")
			continue
		}
		// 标记主键字段（Python 通过 primary_key_field 参数标记，Go 默认 "id"）
		if fieldName == defaultPrimaryKeyField {
			f.IsPrimary = true
		}
		if _, err := schema.AddField(f); err != nil {
			logger.Warn(esLogComponent).Err(err).
				Str("field_name", fieldName).
				Msg("添加字段到 schema 失败，跳过")
			continue
		}
	}

	return schema, nil
}

// esListIndices 通过 ES indices.get API 列出匹配前缀的索引，并提取集合名称。
// 对齐 Python 实现使用 indices.get 而非 _cat/indices。
func esListIndices(ctx context.Context, c esClient, indexPrefix string) ([]string, error) {
	req := esapi.IndicesGetRequest{
		Index: []string{indexPrefix + "__*"},
	}

	resp, err := c.Do(ctx, &req)
	if err != nil {
		return nil, fmt.Errorf("列出索引失败: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取索引列表响应失败: %w", err)
	}

	if resp.IsError() {
		return nil, fmt.Errorf("列出索引失败: status=%s, body=%s", resp.Status(), string(body))
	}

	// indices.get 返回格式: { "index_name": { ... }, ... }
	var indices map[string]any
	if err := json.Unmarshal(body, &indices); err != nil {
		return nil, fmt.Errorf("解析索引列表响应失败: %w", err)
	}

	prefix := indexPrefix + "__"
	var names []string
	for idx := range indices {
		if strings.HasPrefix(idx, prefix) {
			collectionName := strings.TrimPrefix(idx, prefix)
			names = append(names, collectionName)
		}
	}

	return names, nil
}
