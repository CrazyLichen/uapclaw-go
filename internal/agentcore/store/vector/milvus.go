package vector

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/milvus-io/milvus-sdk-go/v2/client"
	"github.com/milvus-io/milvus-sdk-go/v2/entity"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/store/vector_fields"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

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
	// PKType 主键字段类型（用于 DeleteDocsByIDs 生成正确的过滤表达式）
	PKType entity.FieldType
	// FieldNames 集合的所有字段名列表（用于 Search outputFields 自动推断）
	FieldNames []string
}

// MilvusVectorStore 基于 Milvus 的向量存储实现。
//
// 实现 BaseVectorStore 接口，使用 milvus-sdk-go/v2 作为客户端。
// 客户端惰性创建，初始化时不需要 Milvus 可用。
//
// 对应 Python: vector/milvus_vector_store.py (MilvusVectorStore)
type MilvusVectorStore struct {
	// client Milvus 客户端实例
	client milvusClient
	// milvusURI Milvus 服务地址
	milvusURI string
	// milvusToken Milvus 认证令牌
	milvusToken string
	// dbName 数据库名称
	dbName string
	// collectionMetadata 集合元数据缓存
	collectionMetadata map[string]*collMeta
	// collectionsLoaded 集合加载状态缓存
	collectionsLoaded map[string]bool
	// mu 读写锁，保护客户端和缓存
	mu sync.RWMutex
	// createClient 客户端创建函数，用于依赖注入和测试
	createClient func(ctx context.Context, uri, token, dbName string) (milvusClient, error)
}

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
	var pkType entity.FieldType

	for _, field := range schema.Fields() {
		milvusType, err := mapFieldType(field.DType)
		if err != nil {
			return err
		}

		milvusField := entity.NewField().WithName(field.Name).WithDataType(milvusType)
		if field.IsPrimary {
			milvusField = milvusField.WithIsPrimaryKey(true)
			pkType = milvusType
		}
		if field.AutoID {
			milvusField = milvusField.WithIsAutoID(true)
		}

		switch milvusType {
		case entity.FieldTypeFloatVector:
			vectorFieldName = field.Name
			vectorDim = field.Dim
			if vectorDim == 0 {
				return exception.BuildError(exception.StatusStoreVectorSchemaInvalid,
					exception.WithParam("error_msg", fmt.Sprintf("dim of vector field is missing, field=%s", field.Name)),
				)
			}
			milvusField = milvusField.WithDim(int64(vectorDim))
		case entity.FieldTypeVarChar:
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
	// ShardsNum=0 时使用 Milvus 服务端默认值，对齐 Python: 不显式设置 shardsNum
	shardsNum := o.ShardsNum
	if err := c.CreateCollection(ctx, milvusSchema, shardsNum); err != nil {
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

	// 为标量字段创建 INVERTED 索引，对齐 Python: create_collection 中的 add_index(INVERTED)
	for _, field := range schema.Fields() {
		if field.IsPrimary {
			continue
		}
		milvusType, _ := mapFieldType(field.DType)
		switch milvusType {
		case entity.FieldTypeVarChar, entity.FieldTypeInt64, entity.FieldTypeInt32:
			invertedIdx := entity.NewGenericIndex(field.Name, "INVERTED", map[string]string{})
			if err := c.CreateIndex(ctx, collectionName, field.Name, invertedIdx, false); err != nil {
				logger.Warn(logComponent).Err(err).Str("field", field.Name).
					Msg("创建 INVERTED 索引失败，非致命错误")
			}
		}
	}

	// 加载集合
	if err := c.LoadCollection(ctx, collectionName, false); err != nil {
		logger.Error(logComponent).Err(err).Str("collection_name", collectionName).Msg("加载集合失败")
		return err
	}

	// 缓存集合元数据
	fieldNames := make([]string, 0, len(schema.Fields()))
	for _, f := range schema.Fields() {
		fieldNames = append(fieldNames, f.Name)
	}
	s.mu.Lock()
	s.collectionMetadata[collectionName] = &collMeta{
		DistanceMetric: distanceMetric,
		VectorField:    vectorFieldName,
		VectorDim:      vectorDim,
		PKType:         pkType,
		FieldNames:     fieldNames,
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
		// 对齐 Python: logger.debug(f"Added {processed}/{total} documents")
		logger.Debug(logComponent).Str("collection_name", collectionName).
			Int("added", end).Int("total", total).
			Msg("添加文档进度")
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

	// 确定输出字段：未指定时从集合元数据自动推断
	// 对齐 Python: if not search_output_fields: describe_collection 获取字段列表
	outputFields := o.OutputFields
	if len(outputFields) == 0 {
		outputFields = s.getOutputFields(collectionName)
	}

	// 构建搜索参数
	sp, err := s.buildSearchParams(o, topK)
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
				val, err := col.Get(j)
				if err != nil {
					logger.Warn(logComponent).Err(err).
						Str("field", col.Name()).Int("row", j).
						Msg("提取搜索结果字段值失败")
					continue
				}
				fields[col.Name()] = val
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

	// 根据 ID 列表和主键类型构建过滤表达式
	// INT64 主键：id in [1, 2, 3]（无引号）
	// VARCHAR 主键：id in ["a", "b", "c"]（有引号）
	// 对齐 Python: client.delete(ids=ids)，SDK 自动处理类型
	expr := buildDeleteExpr(ids, s.getPKType(collectionName))

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

	// 校验 schema_version：必须是数字类型且 >= 0
	// 对齐 Python: if not isinstance(version, int) or version < 0: raise error
	if v, ok := metadata["schema_version"]; ok {
		var version int
		switch sv := v.(type) {
		case int:
			version = sv
		case int64:
			version = int(sv)
		case float64:
			version = int(sv)
		case string:
			parsed, err := strconv.Atoi(sv)
			if err != nil {
				return exception.BuildError(exception.StatusStoreVectorSchemaInvalid,
					exception.WithParam("error_msg", fmt.Sprintf("schema_version must be non-negative int, got: %v", v)),
				)
			}
			version = parsed
		default:
			return exception.BuildError(exception.StatusStoreVectorSchemaInvalid,
				exception.WithParam("error_msg", fmt.Sprintf("schema_version must be non-negative int, got: %v", v)),
			)
		}
		if version < 0 {
			return exception.BuildError(exception.StatusStoreVectorSchemaInvalid,
				exception.WithParam("error_msg", fmt.Sprintf("schema_version must be non-negative, got: %d", version)),
			)
		}
	}

	// SDK 的 AlterCollection 使用 CollectionAttribute，仅支持预定义属性（TTL 等）
	// 自定义属性（如 schema_version）目前无法通过 SDK 写入 Milvus，仅更新本地缓存
	// 但通过 DescribeCollection.Properties 可以读取 Milvus 中已有的 schema_version
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
	// 对齐 Python: logger.debug(f"Cache miss for '{collection_name}' metadata")
	logger.Debug(logComponent).Str("collection_name", collectionName).Msg("集合元数据缓存未命中")
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

	// 提取向量字段名、主键类型和字段名列表
	var vectorFieldName string
	var vectorDim int
	var pkType entity.FieldType
	fieldNames := make([]string, 0, len(collInfo.Schema.Fields))
	for _, f := range collInfo.Schema.Fields {
		fieldNames = append(fieldNames, f.Name)
		if f.PrimaryKey {
			pkType = f.DataType
		}
		if f.DataType == entity.FieldTypeFloatVector && vectorFieldName == "" {
			vectorFieldName = f.Name
			if dimStr, ok := f.TypeParams["dim"]; ok {
				if d, err := strconv.Atoi(dimStr); err == nil {
					vectorDim = d
				}
			}
		}
	}

	// 从 Milvus collection properties 读取 schema_version
	schemaVersion := "0"
	if v, ok := collInfo.Properties["schema_version"]; ok {
		schemaVersion = v
	}

	metadata := map[string]any{
		"distance_metric": defaultDistanceMetric,
		"vector_field":    vectorFieldName,
		"vector_dim":      vectorDim,
		"schema_version":  schemaVersion,
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
		SchemaVersion:  schemaVersion,
		PKType:         pkType,
		FieldNames:     fieldNames,
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

// joinIDs 将 ID 列表拼接为 Milvus 表达式中带引号的 ID 字符串（用于 VARCHAR 主键）。
func joinIDs(ids []string) string {
	quoted := make([]string, len(ids))
	for i, id := range ids {
		quoted[i] = fmt.Sprintf(`"%s"`, id)
	}
	return strings.Join(quoted, ", ")
}

// joinIDsNoQuote 将 ID 列表拼接为 Milvus 表达式中不带引号的 ID 字符串（用于 INT64 主键）。
func joinIDsNoQuote(ids []string) string {
	return strings.Join(ids, ", ")
}

// buildDeleteExpr 根据主键类型构建删除表达式。
// INT64 主键生成 id in [1, 2, 3]，VARCHAR 主键生成 id in ["a", "b", "c"]。
// 对齐 Python: SDK PKs2Expr 自动根据主键类型选择格式。
func buildDeleteExpr(ids []string, pkType entity.FieldType) string {
	switch pkType {
	case entity.FieldTypeInt64, entity.FieldTypeInt32, entity.FieldTypeInt16, entity.FieldTypeInt8:
		return fmt.Sprintf("id in [%s]", joinIDsNoQuote(ids))
	default:
		// VARCHAR 及其他类型，默认加引号
		return fmt.Sprintf("id in [%s]", joinIDs(ids))
	}
}

// getPKType 获取集合的主键字段类型，优先从缓存获取。
func (s *MilvusVectorStore) getPKType(collectionName string) entity.FieldType {
	s.mu.RLock()
	if meta, ok := s.collectionMetadata[collectionName]; ok {
		s.mu.RUnlock()
		return meta.PKType
	}
	s.mu.RUnlock()
	// 缓存未命中时默认 VARCHAR（加引号更安全）
	return entity.FieldTypeVarChar
}

// getOutputFields 获取集合的输出字段列表，用于 Search 时自动推断 outputFields。
// 对齐 Python: describe_collection 获取字段名列表。
func (s *MilvusVectorStore) getOutputFields(collectionName string) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if meta, ok := s.collectionMetadata[collectionName]; ok {
		if len(meta.FieldNames) > 0 {
			return meta.FieldNames
		}
	}
	return nil
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
		idx, err := entity.NewIndexSCANN(metricType, vf.Nlist, vf.WithRawData)
		if err != nil {
			return nil, fmt.Errorf("创建 SCANN 索引失败: %w", err)
		}
		return idx, nil
		}
	}

	// 默认使用 AUTOINDEX
	return entity.NewGenericIndex(vectorFieldName, entity.AUTOINDEX, map[string]string{}), nil
}

// buildSearchParams 根据配置构建搜索参数。
// topK 用于 HNSW 的 ef 计算：ef = topK * EfSearchFactor，对齐 Python 行为。
func (s *MilvusVectorStore) buildSearchParams(o Options, topK int) (entity.SearchParam, error) {
	if o.VectorField != nil {
		switch vf := o.VectorField.(type) {
		case *vector_fields.MilvusAUTO:
			_ = vf
			// AUTOINDEX 使用 FLAT 搜索参数，Milvus 服务端自动选择最优参数
			return entity.NewIndexFlatSearchParam()
		case *vector_fields.MilvusFLAT:
			_ = vf
			// FLAT 索引使用暴力搜索，无需特殊参数
			return entity.NewIndexFlatSearchParam()
		case *vector_fields.MilvusHNSW:
			// ef = topK * EfSearchFactor，对齐 Python: ef = top_k * ef_search_factor
			ef := topK * int(vf.EfSearchFactor)
			if ef <= 0 {
				ef = 64
			}
			return entity.NewIndexHNSWSearchParam(ef)
		case *vector_fields.MilvusIVF:
			return entity.NewIndexIvfSQ8SearchParam(vf.Nprobe)
		case *vector_fields.MilvusSCANN:
		reorderK := vf.ReorderK
		if reorderK <= 0 {
			reorderK = 1
		}
		sp, err := entity.NewIndexSCANNSearchParam(vf.Nprobe, reorderK)
		if err != nil {
			return nil, fmt.Errorf("创建 SCANN 搜索参数失败: %w", err)
		}
		return sp, nil
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
		case bool:
			bools := make([]bool, 0, len(values))
			for _, val := range values {
				if b, ok := val.(bool); ok {
					bools = append(bools, b)
				} else {
					bools = append(bools, false)
				}
			}
			return entity.NewColumnBool(fieldName, bools)
		case float64:
			// float64 标量（注意：[]float64 是向量，已在上面处理）
			doubles := make([]float64, 0, len(values))
			for _, val := range values {
				if f, ok := val.(float64); ok {
					doubles = append(doubles, f)
				} else {
					doubles = append(doubles, 0)
				}
			}
			return entity.NewColumnDouble(fieldName, doubles)
		case float32:
			// float32 标量（注意：[]float32 是向量，已在上面处理）
			floats := make([]float32, 0, len(values))
			for _, val := range values {
				if f, ok := val.(float32); ok {
					floats = append(floats, f)
				} else {
					floats = append(floats, 0)
				}
			}
			return entity.NewColumnFloat(fieldName, floats)
		}
	}
	return nil
}
