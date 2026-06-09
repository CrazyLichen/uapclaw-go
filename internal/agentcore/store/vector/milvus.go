package vector

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/milvus-io/milvus-sdk-go/v2/client"
	"github.com/milvus-io/milvus-sdk-go/v2/entity"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/store/vector_fields"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// defaultDistanceMetric 默认距离度量方式
	defaultDistanceMetric = "COSINE"
	// defaultBatchSize 默认批量插入大小
	defaultBatchSize = 128
)

// ──────────────────────────── 全局变量 ────────────────────────────

// logComponent 日志组件，agentcore 下统一使用 ComponentAgentCore
var logComponent = logger.ComponentAgentCore

// ──────────────────────────── 结构体 ────────────────────────────

// collMeta 集合元数据缓存
type collMeta struct {
	// DistanceMetric 距离度量类型
	DistanceMetric string
	// VectorField 向量字段名
	VectorField string
	// VectorDim 向量维度
	VectorDim int
	// SchemaVersion schema 版本
	SchemaVersion string
}

// MilvusVectorStore 基于 Milvus 的向量存储实现。
//
// 实现 BaseVectorStore 接口，使用 milvus-sdk-go/v2 作为客户端。
// 客户端惰性创建，初始化时不需要 Milvus 可用。
//
// 对应 Python: vector/milvus_vector_store.py (MilvusVectorStore)
type MilvusVectorStore struct {
	client             milvusClient
	milvusURI          string
	milvusToken        string
	dbName             string
	collectionMetadata map[string]*collMeta
	collectionsLoaded  map[string]bool
	mu                 sync.RWMutex
	createClient       func(ctx context.Context, uri, token, dbName string) (milvusClient, error)
}

// milvusClient Milvus 客户端操作接口（用于解耦和测试）。
// 生产代码使用真实 client.Client，测试代码注入 fakeMilvusClient。
type milvusClient interface {
	CreateCollection(ctx context.Context, schema *entity.Schema, shardsNum int32, opts ...client.CreateCollectionOption) error
	DropCollection(ctx context.Context, collName string, opts ...client.DropCollectionOption) error
	HasCollection(ctx context.Context, collName string) (bool, error)
	DescribeCollection(ctx context.Context, collName string) (*entity.Collection, error)
	Insert(ctx context.Context, collName string, partitionName string, columns ...entity.Column) (entity.Column, error)
	Search(ctx context.Context, collName string, partitions []string, expr string, outputFields []string, vectors []entity.Vector, vectorField string, metricType entity.MetricType, topK int, sp entity.SearchParam, opts ...client.SearchQueryOptionFunc) ([]client.SearchResult, error)
	Delete(ctx context.Context, collName string, partitionName string, expr string) error
	ListCollections(ctx context.Context, opts ...client.ListCollectionOption) ([]*entity.Collection, error)
	LoadCollection(ctx context.Context, collName string, async bool, opts ...client.LoadCollectionOption) error
	AlterCollection(ctx context.Context, collName string, attrs ...entity.CollectionAttribute) error
	Flush(ctx context.Context, collName string, async bool, opts ...client.FlushOption) error
	CreateIndex(ctx context.Context, collName string, fieldName string, idx entity.Index, async bool, opts ...client.IndexOption) error
	DescribeIndex(ctx context.Context, collName string, fieldName string, opts ...client.IndexOption) ([]entity.Index, error)
	Close() error
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewMilvusVectorStore 创建 MilvusVectorStore 实例。
// 客户端惰性创建，初始化时不需要 Milvus 可用。
//
// 对应 Python: MilvusVectorStore.__init__(milvus_uri, milvus_token, database_name)
func NewMilvusVectorStore(milvusURI, milvusToken, dbName string) *MilvusVectorStore {
	if dbName == "" {
		dbName = "default"
	}
	return &MilvusVectorStore{
		milvusURI:          milvusURI,
		milvusToken:        milvusToken,
		dbName:             dbName,
		collectionMetadata: make(map[string]*collMeta),
		collectionsLoaded:  make(map[string]bool),
		createClient:       defaultCreateClient,
	}
}

// Close 关闭 Milvus 客户端连接。
//
// 对应 Python: MilvusVectorStore.close()
func (s *MilvusVectorStore) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.client != nil {
		_ = s.client.Close()
		s.client = nil
		logger.Info(logComponent).Str("action", "close").Msg("Milvus 客户端连接已关闭")
	}
}

// CreateCollection 创建集合。
// 如果集合已存在则跳过创建。schema 定义字段结构，opts 可指定 DistanceMetric 和 VectorField 索引配置。
//
// 对应 Python: MilvusVectorStore.create_collection(collection_name, schema, **kwargs)
func (s *MilvusVectorStore) CreateCollection(ctx context.Context, collectionName string, schema *CollectionSchema, opts ...Option) error {
	o := newOptions(opts...)
	c, err := s.getClient(ctx)
	if err != nil {
		return err
	}

	// 检查集合是否已存在
	has, err := c.HasCollection(ctx, collectionName)
	if err != nil {
		return err
	}
	if has {
		logger.Info(logComponent).Str("collection_name", collectionName).Msg("集合已存在，跳过创建")
		return nil
	}

	distanceMetric := o.DistanceMetric
	if distanceMetric == "" {
		distanceMetric = defaultDistanceMetric
	}
	distanceMetric = strings.ToUpper(distanceMetric)

	// 构建 Milvus schema
	milvusSchema := entity.NewSchema().WithName(collectionName).WithDescription(schema.Description)
	if schema.EnableDynamicField {
		milvusSchema = milvusSchema.WithDynamicFieldEnabled(true)
	}

	var vectorFieldName string
	var vectorDim int

	for _, field := range schema.Fields() {
		milvusType, err := mapFieldType(field.DType)
		if err != nil {
			return err
		}

		milvusField := entity.NewField().WithName(field.Name).WithDataType(milvusType)
		if field.IsPrimary {
			milvusField = milvusField.WithIsPrimaryKey(true)
		}
		if field.AutoID {
			milvusField = milvusField.WithIsAutoID(true)
		}

		if milvusType == entity.FieldTypeFloatVector {
			vectorFieldName = field.Name
			vectorDim = field.Dim
			if vectorDim == 0 {
				return exception.BuildError(exception.StatusStoreVectorSchemaInvalid,
					exception.WithParam("error_msg", fmt.Sprintf("dim of vector field is missing, field=%s", field.Name)),
				)
			}
			milvusField = milvusField.WithDim(int64(vectorDim))
		} else if milvusType == entity.FieldTypeVarChar {
			maxLen := field.MaxLength
			if maxLen == 0 {
				maxLen = defaultMaxLength
			}
			milvusField = milvusField.WithMaxLength(int64(maxLen))
		}

		milvusSchema = milvusSchema.WithField(milvusField)
	}

	if vectorFieldName == "" {
		return exception.BuildError(exception.StatusStoreVectorSchemaInvalid,
			exception.WithParam("error_msg", "schema must contain at least one FLOAT_VECTOR field"),
		)
	}

	// 创建集合
	if err := c.CreateCollection(ctx, milvusSchema, 1); err != nil {
		logger.Error(logComponent).Err(err).Str("collection_name", collectionName).Msg("创建集合失败")
		return err
	}

	// 创建向量索引
	idx, err := s.buildIndexParams(vectorFieldName, distanceMetric, o)
	if err != nil {
		return err
	}
	if err := c.CreateIndex(ctx, collectionName, vectorFieldName, idx, false); err != nil {
		logger.Error(logComponent).Err(err).Str("collection_name", collectionName).Msg("创建索引失败")
		return err
	}

	// 加载集合
	if err := c.LoadCollection(ctx, collectionName, false); err != nil {
		logger.Error(logComponent).Err(err).Str("collection_name", collectionName).Msg("加载集合失败")
		return err
	}

	// 缓存集合元数据
	s.mu.Lock()
	s.collectionMetadata[collectionName] = &collMeta{
		DistanceMetric: distanceMetric,
		VectorField:    vectorFieldName,
		VectorDim:      vectorDim,
	}
	s.collectionsLoaded[collectionName] = true
	s.mu.Unlock()

	logger.Info(logComponent).Str("collection_name", collectionName).
		Int("field_count", len(schema.Fields())).
		Msg("成功创建集合并加载")
	return nil
}

// DeleteCollection 删除集合。
//
// 对应 Python: MilvusVectorStore.delete_collection(collection_name)
func (s *MilvusVectorStore) DeleteCollection(ctx context.Context, collectionName string, opts ...Option) error {
	c, err := s.getClient(ctx)
	if err != nil {
		return err
	}

	has, err := c.HasCollection(ctx, collectionName)
	if err != nil {
		return err
	}
	if !has {
		logger.Warn(logComponent).Str("collection_name", collectionName).Msg("集合不存在，跳过删除")
		return nil
	}

	if err := c.DropCollection(ctx, collectionName); err != nil {
		logger.Error(logComponent).Err(err).Str("collection_name", collectionName).Msg("删除集合失败")
		return err
	}

	// 清除缓存
	s.mu.Lock()
	delete(s.collectionMetadata, collectionName)
	delete(s.collectionsLoaded, collectionName)
	s.mu.Unlock()

	logger.Info(logComponent).Str("collection_name", collectionName).Msg("成功删除集合")
	return nil
}

// CollectionExists 检查集合是否存在。
//
// 对应 Python: MilvusVectorStore.collection_exists(collection_name)
func (s *MilvusVectorStore) CollectionExists(ctx context.Context, collectionName string, opts ...Option) (bool, error) {
	c, err := s.getClient(ctx)
	if err != nil {
		return false, err
	}
	return c.HasCollection(ctx, collectionName)
}

// GetSchema 获取集合的 Schema。
//
// 对应 Python: MilvusVectorStore.get_schema(collection_name)
func (s *MilvusVectorStore) GetSchema(ctx context.Context, collectionName string, opts ...Option) (*CollectionSchema, error) {
	c, err := s.getClient(ctx)
	if err != nil {
		return nil, err
	}

	has, err := c.HasCollection(ctx, collectionName)
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, exception.BuildError(exception.StatusStoreVectorCollectionNotFound,
			exception.WithParam("collection_name", collectionName),
		)
	}

	collInfo, err := c.DescribeCollection(ctx, collectionName)
	if err != nil {
		logger.Error(logComponent).Err(err).Str("collection_name", collectionName).Msg("获取集合 Schema 失败")
		return nil, err
	}

	schema, err := NewCollectionSchema(
		WithCollectionDescription(collInfo.Schema.Description),
	)
	if err != nil {
		return nil, err
	}
	if collInfo.Schema.EnableDynamicField {
		schema.EnableDynamicField = true
	}

	for _, milvusField := range collInfo.Schema.Fields {
		ourType := mapMilvusTypeToOurType(milvusField.DataType)
		fieldOpts := []FieldOption{}
		if milvusField.PrimaryKey {
			fieldOpts = append(fieldOpts, WithPrimary())
		}
		if milvusField.AutoID {
			fieldOpts = append(fieldOpts, WithAutoID())
		}
		if dimStr, ok := milvusField.TypeParams["dim"]; ok {
			if d, err := strconv.Atoi(dimStr); err == nil && d > 0 {
				fieldOpts = append(fieldOpts, WithDim(d))
			}
		}
		if maxLenStr, ok := milvusField.TypeParams["max_length"]; ok {
			if ml, err := strconv.Atoi(maxLenStr); err == nil {
				fieldOpts = append(fieldOpts, WithMaxLength(ml))
			}
		}

		f, err := NewFieldSchema(milvusField.Name, ourType, fieldOpts...)
		if err != nil {
			return nil, err
		}
		if _, err := schema.AddField(f); err != nil {
			return nil, err
		}
	}

	return schema, nil
}

// AddDocs 添加文档到集合。支持批量插入，通过 BatchSize 控制批次大小。
//
// 对应 Python: MilvusVectorStore.add_docs(collection_name, docs, **kwargs)
func (s *MilvusVectorStore) AddDocs(ctx context.Context, collectionName string, docs []map[string]any, opts ...Option) error {
	if len(docs) == 0 {
		return nil
	}

	o := newOptions(opts...)
	batchSize := o.BatchSize
	if batchSize <= 0 {
		batchSize = defaultBatchSize
	}

	if err := s.ensureLoaded(ctx, collectionName); err != nil {
		return err
	}

	c, err := s.getClient(ctx)
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

		// 将 map 转为 entity.Column 列表
		columns, err := s.docsToColumns(batch)
		if err != nil {
			return err
		}

		_, err = c.Insert(ctx, collectionName, "", columns...)
		if err != nil {
			logger.Error(logComponent).Err(err).Str("collection_name", collectionName).
				Int("batch_start", i).Int("batch_size", len(batch)).Msg("插入文档批次失败")
			return err
		}
	}

	// 刷新确保持久化
	if err := c.Flush(ctx, collectionName, false); err != nil {
		logger.Warn(logComponent).Err(err).Str("collection_name", collectionName).Msg("Flush 失败")
	}

	logger.Info(logComponent).Str("collection_name", collectionName).
		Int("total", total).Msg("成功添加文档到集合")
	return nil
}

// Search 向量相似度搜索。
//
// 对应 Python: MilvusVectorStore.search(collection_name, query_vector, vector_field, top_k, filters, **kwargs)
func (s *MilvusVectorStore) Search(ctx context.Context, collectionName string, queryVector []float64, vectorField string, topK int, filters map[string]any, opts ...Option) ([]VectorSearchResult, error) {
	o := newOptions(opts...)
	if topK <= 0 {
		topK = 5
	}

	if err := s.ensureLoaded(ctx, collectionName); err != nil {
		return nil, err
	}

	c, err := s.getClient(ctx)
	if err != nil {
		return nil, err
	}

	metricType := s.getDistanceMetricType(collectionName, o)

	// 构建过滤表达式
	expr := buildFilterExpr(filters)

	// 构建查询向量
	vecFloat32 := make([]float32, len(queryVector))
	for i, v := range queryVector {
		vecFloat32[i] = float32(v)
	}
	vectors := []entity.Vector{entity.FloatVector(vecFloat32)}

	// 确定输出字段
	outputFields := o.OutputFields

	// 构建搜索参数
	sp, err := s.buildSearchParams(o)
	if err != nil {
		return nil, err
	}

	results, err := c.Search(ctx, collectionName, nil, expr, outputFields, vectors, vectorField, metricType, topK, sp)
	if err != nil {
		logger.Error(logComponent).Err(err).Str("collection_name", collectionName).Msg("向量搜索失败")
		return nil, err
	}

	// 转换结果
	searchResults := make([]VectorSearchResult, 0)
	for _, result := range results {
		if result.Err != nil {
			logger.Warn(logComponent).Err(result.Err).Msg("搜索结果包含错误")
			continue
		}
		for j := 0; j < result.ResultCount; j++ {
			score := float64(result.Scores[j])
			// 根据度量类型转换分数
			normalizedScore := normalizeScore(score, metricType)

			fields := make(map[string]any)
			for _, col := range result.Fields {
				fields[col.Name()] = col.FieldData()
			}

			searchResults = append(searchResults, VectorSearchResult{
				Score:  normalizedScore,
				Fields: fields,
			})
		}
	}

	logger.Info(logComponent).Str("collection_name", collectionName).
		Int("result_count", len(searchResults)).Msg("向量搜索完成")
	return searchResults, nil
}

// DeleteDocsByIDs 按 ID 删除文档。
//
// 对应 Python: MilvusVectorStore.delete_docs_by_ids(collection_name, ids)
func (s *MilvusVectorStore) DeleteDocsByIDs(ctx context.Context, collectionName string, ids []string, opts ...Option) error {
	if len(ids) == 0 {
		logger.Warn(logComponent).Str("collection_name", collectionName).Msg("未提供删除 ID")
		return nil
	}

	if err := s.ensureLoaded(ctx, collectionName); err != nil {
		return err
	}

	c, err := s.getClient(ctx)
	if err != nil {
		return err
	}

	// 构建 ID 过滤表达式
	expr := fmt.Sprintf("id in [%s]", joinIDs(ids))

	if err := c.Delete(ctx, collectionName, "", expr); err != nil {
		logger.Error(logComponent).Err(err).Str("collection_name", collectionName).
			Int("id_count", len(ids)).Msg("按 ID 删除文档失败")
		return err
	}

	if err := c.Flush(ctx, collectionName, false); err != nil {
		logger.Warn(logComponent).Err(err).Str("collection_name", collectionName).Msg("Flush 失败")
	}

	logger.Info(logComponent).Str("collection_name", collectionName).
		Int("id_count", len(ids)).Msg("成功按 ID 删除文档")
	return nil
}

// DeleteDocsByFilters 按标量字段过滤条件删除文档。
//
// 对应 Python: MilvusVectorStore.delete_docs_by_filters(collection_name, filters)
func (s *MilvusVectorStore) DeleteDocsByFilters(ctx context.Context, collectionName string, filters map[string]any, opts ...Option) error {
	if len(filters) == 0 {
		logger.Warn(logComponent).Str("collection_name", collectionName).Msg("未提供过滤条件")
		return nil
	}

	if err := s.ensureLoaded(ctx, collectionName); err != nil {
		return err
	}

	c, err := s.getClient(ctx)
	if err != nil {
		return err
	}

	expr := buildFilterExpr(filters)
	if expr == "" {
		return nil
	}

	if err := c.Delete(ctx, collectionName, "", expr); err != nil {
		logger.Error(logComponent).Err(err).Str("collection_name", collectionName).
			Str("filter_expr", expr).Msg("按过滤条件删除文档失败")
		return err
	}

	if err := c.Flush(ctx, collectionName, false); err != nil {
		logger.Warn(logComponent).Err(err).Str("collection_name", collectionName).Msg("Flush 失败")
	}

	logger.Info(logComponent).Str("collection_name", collectionName).
		Str("filter_expr", expr).Msg("成功按过滤条件删除文档")
	return nil
}

// ListCollectionNames 列出所有集合名称。
//
// 对应 Python: MilvusVectorStore.list_collection_names()
func (s *MilvusVectorStore) ListCollectionNames(ctx context.Context) ([]string, error) {
	c, err := s.getClient(ctx)
	if err != nil {
		return nil, err
	}
	colls, err := c.ListCollections(ctx)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(colls))
	for _, coll := range colls {
		names = append(names, coll.Name)
	}
	return names, nil
}

// UpdateSchema 执行 schema 迁移操作。
// ⤵️ 预留：实际迁移逻辑待 7.22/7.23 实现后回填。
//
// 对应 Python: MilvusVectorStore.update_schema(collection_name, operations)
func (s *MilvusVectorStore) UpdateSchema(ctx context.Context, collectionName string, operations []any, opts ...Option) error {
	// TODO: ⤵️ 回填，待 7.22/7.23 实现后补全
	logger.Warn(logComponent).Str("collection_name", collectionName).Msg("UpdateSchema 尚未实现，待 7.22/7.23 回填")
	return exception.BuildError(exception.StatusStoreVectorSchemaInvalid,
		exception.WithParam("error_msg", "UpdateSchema is not yet implemented, pending 7.22/7.23"),
	)
}

// UpdateCollectionMetadata 更新集合元数据。
// 同时更新 Milvus 集合属性和本地缓存。
//
// 对应 Python: MilvusVectorStore.update_collection_metadata(collection_name, metadata)
func (s *MilvusVectorStore) UpdateCollectionMetadata(ctx context.Context, collectionName string, metadata map[string]any, opts ...Option) error {
	if len(metadata) == 0 {
		return nil
	}

	c, err := s.getClient(ctx)
	if err != nil {
		return err
	}

	// 检查集合是否存在
	has, err := c.HasCollection(ctx, collectionName)
	if err != nil {
		return err
	}
	if !has {
		return exception.BuildError(exception.StatusStoreVectorCollectionNotFound,
			exception.WithParam("collection_name", collectionName),
		)
	}

	// SDK 的 AlterCollection 使用 CollectionAttribute，仅支持预定义属性（TTL 等）
	// 自定义属性（如 schema_version）目前无法通过 SDK 设置，仅更新本地缓存
	logger.Info(logComponent).Str("collection_name", collectionName).
		Interface("metadata", metadata).Msg("更新集合元数据（仅本地缓存）")

	// 更新本地缓存
	s.mu.Lock()
	if meta, ok := s.collectionMetadata[collectionName]; ok {
		if v, ok := metadata["schema_version"]; ok {
			meta.SchemaVersion = fmt.Sprintf("%v", v)
		}
		if v, ok := metadata["distance_metric"]; ok {
			if str, ok := v.(string); ok {
				meta.DistanceMetric = str
			}
		}
	}
	s.mu.Unlock()

	return nil
}

// GetCollectionMetadata 获取集合元数据。
// 优先从缓存获取，缓存未命中则从 Milvus 获取。
//
// 对应 Python: MilvusVectorStore.get_collection_metadata(collection_name)
func (s *MilvusVectorStore) GetCollectionMetadata(ctx context.Context, collectionName string, opts ...Option) (map[string]any, error) {
	s.mu.RLock()
	if meta, ok := s.collectionMetadata[collectionName]; ok {
		s.mu.RUnlock()
		result := map[string]any{
			"distance_metric": meta.DistanceMetric,
			"vector_field":    meta.VectorField,
			"vector_dim":      meta.VectorDim,
			"schema_version":  meta.SchemaVersion,
		}
		return result, nil
	}
	s.mu.RUnlock()

	// 缓存未命中，从 Milvus 获取
	c, err := s.getClient(ctx)
	if err != nil {
		return nil, err
	}

	collInfo, err := c.DescribeCollection(ctx, collectionName)
	if err != nil {
		logger.Warn(logComponent).Err(err).Str("collection_name", collectionName).
			Msg("获取集合描述失败，回退默认值")
		return map[string]any{
			"distance_metric": defaultDistanceMetric,
			"schema_version":  "0",
		}, nil
	}

	// 提取向量字段名
	var vectorFieldName string
	var vectorDim int
	for _, f := range collInfo.Schema.Fields {
		if f.DataType == entity.FieldTypeFloatVector {
			vectorFieldName = f.Name
			if dimStr, ok := f.TypeParams["dim"]; ok {
				if d, err := strconv.Atoi(dimStr); err == nil {
					vectorDim = d
				}
			}
			break
		}
	}

	metadata := map[string]any{
		"distance_metric": defaultDistanceMetric,
		"vector_field":    vectorFieldName,
		"vector_dim":      vectorDim,
		"schema_version":  "0",
	}

	// 尝试获取索引信息以确定度量类型
	if vectorFieldName != "" {
		indexes, err := c.DescribeIndex(ctx, collectionName, vectorFieldName)
		if err == nil && len(indexes) > 0 {
			params := indexes[0].Params()
			if mt, ok := params["metric_type"]; ok {
				metadata["distance_metric"] = mt
			}
		}
	}

	// 更新缓存
	s.mu.Lock()
	dm := defaultDistanceMetric
	if v, ok := metadata["distance_metric"].(string); ok {
		dm = v
	}
	s.collectionMetadata[collectionName] = &collMeta{
		DistanceMetric: dm,
		VectorField:    vectorFieldName,
		VectorDim:      vectorDim,
		SchemaVersion:  "0",
	}
	s.mu.Unlock()

	return metadata, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// getClient 惰性获取或创建 Milvus 客户端。
func (s *MilvusVectorStore) getClient(ctx context.Context) (milvusClient, error) {
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

	c, err := s.createClient(ctx, s.milvusURI, s.milvusToken, s.dbName)
	if err != nil {
		logger.Error(logComponent).Err(err).Str("milvus_uri", s.milvusURI).Msg("连接 Milvus 失败")
		return nil, exception.BuildError(exception.StatusStoreVectorCollectionNotFound,
			exception.WithParam("error_msg", fmt.Sprintf("failed to connect to Milvus: %v", err)),
		)
	}
	s.client = c
	logger.Info(logComponent).Str("milvus_uri", s.milvusURI).Msg("成功连接 Milvus")
	return s.client, nil
}

// defaultCreateClient 默认的客户端创建函数，使用 milvus-sdk-go。
func defaultCreateClient(ctx context.Context, uri, token, dbName string) (milvusClient, error) {
	return client.NewClient(ctx, client.Config{
		Address: uri,
		APIKey:  token,
		DBName:  dbName,
	})
}

// ensureLoaded 确保集合已加载到内存，使用缓存避免重复加载。
//
// 对应 Python: MilvusVectorStore._ensure_loaded(collection)
func (s *MilvusVectorStore) ensureLoaded(ctx context.Context, collectionName string) error {
	s.mu.RLock()
	loaded := s.collectionsLoaded[collectionName]
	s.mu.RUnlock()
	if loaded {
		return nil
	}

	c, err := s.getClient(ctx)
	if err != nil {
		return err
	}

	has, err := c.HasCollection(ctx, collectionName)
	if err != nil {
		return err
	}
	if !has {
		return nil
	}

	logger.Info(logComponent).Str("collection_name", collectionName).Msg("正在加载集合")
	if err := c.LoadCollection(ctx, collectionName, false); err != nil {
		logger.Error(logComponent).Err(err).Str("collection_name", collectionName).Msg("加载集合失败")
		return err
	}

	s.mu.Lock()
	s.collectionsLoaded[collectionName] = true
	s.mu.Unlock()

	logger.Info(logComponent).Str("collection_name", collectionName).Msg("集合加载完成")
	return nil
}

// buildFilterExpr 从过滤条件字典构建 Milvus 过滤表达式（仅支持等值过滤）。
//
// 对应 Python: MilvusVectorStore._build_filter_expr(filters)
func buildFilterExpr(filters map[string]any) string {
	if len(filters) == 0 {
		return ""
	}
	parts := make([]string, 0, len(filters))
	for key, value := range filters {
		switch v := value.(type) {
		case string:
			parts = append(parts, fmt.Sprintf(`%s == "%s"`, key, v))
		default:
			parts = append(parts, fmt.Sprintf("%s == %v", key, v))
		}
	}
	return strings.Join(parts, " && ")
}

// joinIDs 将 ID 列表拼接为 Milvus 表达式中的 ID 字符串。
func joinIDs(ids []string) string {
	quoted := make([]string, len(ids))
	for i, id := range ids {
		quoted[i] = fmt.Sprintf(`"%s"`, id)
	}
	return strings.Join(quoted, ", ")
}

// mapFieldType 将 VectorDataType 映射为 Milvus DataType。
//
// 对应 Python: MilvusVectorStore._map_field_type(field_type)
func mapFieldType(dt VectorDataType) (entity.FieldType, error) {
	mapping := map[VectorDataType]entity.FieldType{
		VectorDataTypeVarchar:     entity.FieldTypeVarChar,
		VectorDataTypeFloatVector: entity.FieldTypeFloatVector,
		VectorDataTypeInt64:       entity.FieldTypeInt64,
		VectorDataTypeInt32:       entity.FieldTypeInt32,
		VectorDataTypeFloat:       entity.FieldTypeFloat,
		VectorDataTypeDouble:      entity.FieldTypeDouble,
		VectorDataTypeBool:        entity.FieldTypeBool,
		VectorDataTypeJSON:        entity.FieldTypeJSON,
	}
	milvusType, ok := mapping[dt]
	if !ok {
		return 0, exception.BuildError(exception.StatusStoreVectorSchemaInvalid,
			exception.WithParam("error_msg", fmt.Sprintf("unsupported field type: %v", dt)),
		)
	}
	return milvusType, nil
}

// mapMilvusTypeToOurType 将 Milvus DataType 映射回 VectorDataType。
//
// 对应 Python: MilvusVectorStore._map_milvus_type_to_our_type(milvus_type)
func mapMilvusTypeToOurType(milvusType entity.FieldType) VectorDataType {
	mapping := map[entity.FieldType]VectorDataType{
		entity.FieldTypeVarChar:     VectorDataTypeVarchar,
		entity.FieldTypeFloatVector: VectorDataTypeFloatVector,
		entity.FieldTypeInt64:       VectorDataTypeInt64,
		entity.FieldTypeInt32:       VectorDataTypeInt32,
		entity.FieldTypeFloat:       VectorDataTypeFloat,
		entity.FieldTypeDouble:      VectorDataTypeDouble,
		entity.FieldTypeBool:        VectorDataTypeBool,
		entity.FieldTypeJSON:        VectorDataTypeJSON,
	}
	ourType, ok := mapping[milvusType]
	if !ok {
		logger.Warn(logComponent).Str("milvus_type", milvusType.String()).Msg("不支持的 Milvus 类型，回退为 VARCHAR")
		return VectorDataTypeVarchar
	}
	return ourType
}

// getDistanceMetricType 从 Options 和缓存中获取距离度量类型。
func (s *MilvusVectorStore) getDistanceMetricType(collectionName string, opts Options) entity.MetricType {
	metricStr := opts.DistanceMetric
	if metricStr == "" {
		s.mu.RLock()
		if meta, ok := s.collectionMetadata[collectionName]; ok && meta.DistanceMetric != "" {
			metricStr = meta.DistanceMetric
		}
		s.mu.RUnlock()
	}
	if metricStr == "" {
		metricStr = defaultDistanceMetric
	}
	return mapMetricType(metricStr)
}

// mapMetricType 将字符串度量类型映射为 entity.MetricType。
func mapMetricType(metric string) entity.MetricType {
	switch strings.ToUpper(metric) {
	case "L2":
		return entity.L2
	case "IP":
		return entity.IP
	case "COSINE":
		return entity.COSINE
	default:
		return entity.COSINE
	}
}

// normalizeScore 根据度量类型将原始分数转换为归一化相似度 [0, 1]。
func normalizeScore(rawScore float64, metricType entity.MetricType) float64 {
	switch metricType {
	case entity.COSINE:
		return ConvertCosineSimilarity(rawScore)
	case entity.L2:
		return ConvertL2Squared(rawScore, 4.0)
	case entity.IP:
		return ConvertIPSimilarity(rawScore)
	default:
		return ConvertCosineSimilarity(rawScore)
	}
}

// buildIndexParams 根据 VectorField 配置构建索引参数。
func (s *MilvusVectorStore) buildIndexParams(vectorFieldName, distanceMetric string, o Options) (entity.Index, error) {
	metricType := mapMetricType(distanceMetric)

	// 如果有 VectorField 配置，使用其参数
	if o.VectorField != nil {
		switch vf := o.VectorField.(type) {
		case *vector_fields.MilvusAUTO:
			_ = vf
			return entity.NewGenericIndex(vectorFieldName, entity.AUTOINDEX, map[string]string{}), nil
		case *vector_fields.MilvusFLAT:
			return entity.NewIndexFlat(metricType)
		case *vector_fields.MilvusHNSW:
			return entity.NewIndexHNSW(metricType, vf.M, vf.EfConstruction)
		case *vector_fields.MilvusIVF:
			return entity.NewIndexIvfFlat(metricType, vf.Nlist)
		case *vector_fields.MilvusSCANN:
			return entity.NewIndexIvfFlat(metricType, vf.Nlist)
		}
	}

	// 默认使用 AUTOINDEX
	return entity.NewGenericIndex(vectorFieldName, entity.AUTOINDEX, map[string]string{}), nil
}

// buildSearchParams 根据配置构建搜索参数。
func (s *MilvusVectorStore) buildSearchParams(o Options) (entity.SearchParam, error) {
	if o.VectorField != nil {
		switch vf := o.VectorField.(type) {
		case *vector_fields.MilvusHNSW:
			ef := int(vf.EfSearchFactor)
			if ef <= 0 {
				ef = 64
			}
			return entity.NewIndexHNSWSearchParam(ef)
		case *vector_fields.MilvusIVF:
			return entity.NewIndexIvfSQ8SearchParam(vf.Nprobe)
		case *vector_fields.MilvusSCANN:
			return entity.NewIndexIvfSQ8SearchParam(vf.Nprobe)
		}
	}
	// 默认搜索参数
	return entity.NewIndexFlatSearchParam()
}

// docsToColumns 将文档列表转换为 Milvus 列格式。
func (s *MilvusVectorStore) docsToColumns(docs []map[string]any) ([]entity.Column, error) {
	if len(docs) == 0 {
		return nil, nil
	}

	// 收集所有字段名
	fieldSet := make(map[string]bool)
	for _, doc := range docs {
		for k := range doc {
			fieldSet[k] = true
		}
	}

	// 为每个字段收集值
	fieldValues := make(map[string][]any)
	for name := range fieldSet {
		fieldValues[name] = make([]any, 0, len(docs))
	}
	for _, doc := range docs {
		for name := range fieldSet {
			val, ok := doc[name]
			if ok {
				fieldValues[name] = append(fieldValues[name], val)
			} else {
				fieldValues[name] = append(fieldValues[name], nil)
			}
		}
	}

	// 转换为 entity.Column
	result := make([]entity.Column, 0, len(fieldValues))
	for fieldName, values := range fieldValues {
		col := inferColumn(fieldName, values)
		if col != nil {
			result = append(result, col)
		}
	}
	return result, nil
}

// inferColumn 从值推断列类型并创建 entity.Column。
func inferColumn(fieldName string, values []any) entity.Column {
	if len(values) == 0 {
		return nil
	}

	// 检查第一个非 nil 值的类型
	for _, v := range values {
		if v == nil {
			continue
		}
		switch v.(type) {
		case []float64:
			// 向量字段
			vecs := make([][]float32, 0, len(values))
			dim := 0
			for _, val := range values {
				if f64, ok := val.([]float64); ok {
					dim = len(f64)
					f32 := make([]float32, len(f64))
					for i, f := range f64 {
						f32[i] = float32(f)
					}
					vecs = append(vecs, f32)
				} else if val == nil && dim > 0 {
					vecs = append(vecs, make([]float32, dim))
				}
			}
			if dim > 0 && len(vecs) > 0 {
				return entity.NewColumnFloatVector(fieldName, dim, vecs)
			}
		case []float32:
			// 向量字段（float32）
			vecs := make([][]float32, 0, len(values))
			dim := 0
			for _, val := range values {
				if f32, ok := val.([]float32); ok {
					dim = len(f32)
					vecs = append(vecs, f32)
				} else if val == nil && dim > 0 {
					vecs = append(vecs, make([]float32, dim))
				}
			}
			if dim > 0 && len(vecs) > 0 {
				return entity.NewColumnFloatVector(fieldName, dim, vecs)
			}
		case string:
			strs := make([]string, 0, len(values))
			for _, val := range values {
				if s, ok := val.(string); ok {
					strs = append(strs, s)
				} else {
					strs = append(strs, "")
				}
			}
			return entity.NewColumnVarChar(fieldName, strs)
		case int64:
			ints := make([]int64, 0, len(values))
			for _, val := range values {
				if i, ok := val.(int64); ok {
					ints = append(ints, i)
				} else {
					ints = append(ints, 0)
				}
			}
			return entity.NewColumnInt64(fieldName, ints)
		case int:
			ints := make([]int64, 0, len(values))
			for _, val := range values {
				if i, ok := val.(int); ok {
					ints = append(ints, int64(i))
				} else {
					ints = append(ints, 0)
				}
			}
			return entity.NewColumnInt64(fieldName, ints)
		}
	}
	return nil
}
