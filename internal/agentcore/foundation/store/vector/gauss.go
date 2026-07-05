package vector

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/vector_fields"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// dbClient 封装 pgxpool.Pool 的查询方法，用于依赖注入和测试 mock。
// *pgxpool.Pool 天然实现此接口。
type dbClient interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Close()
}

// gaussCollMeta GaussDB 集合元数据缓存
type gaussCollMeta struct {
	// DistanceMetric 距离度量类型
	DistanceMetric string
	// VectorField 向量字段名
	VectorField string
	// VectorDim 向量维度
	VectorDim int
	// SchemaVersion schema 版本
	SchemaVersion string
}

// GaussVectorStore 基于 GaussDB 的向量存储实现。
//
// 使用 pgx/v5 pgxpool 连接池与 GaussDB 通信，支持 DiskANN 向量索引。
// 客户端惰性创建，初始化时不需要数据库可用。
// 元数据仅存内存缓存，进程重启后从 information_schema 回查。
//
// 对应 Python: vector/gauss_vector_store.py (GaussVectorStore)
type GaussVectorStore struct {
	// pool 数据库连接池
	pool dbClient
	// connConfig 连接字符串
	connConfig string
	// collectionMetadata 集合元数据内存缓存
	collectionMetadata map[string]*gaussCollMeta
	// mu 读写锁，保护连接池和缓存
	mu sync.RWMutex
	// createPool 连接池创建函数，用于依赖注入和测试
	createPool func(ctx context.Context, connString string) (dbClient, error)
}

// gaussDistanceMetric GaussDB 支持的距离度量
type gaussDistanceMetric string

// ──────────────────────────── 常量 ────────────────────────────
const (
	gaussMetricCosine gaussDistanceMetric = "cosine"
	gaussMetricL2     gaussDistanceMetric = "l2"
)

const (
	// gaussDefaultDistanceMetric GaussDB 默认距离度量
	gaussDefaultDistanceMetric = "COSINE"
	// gaussDefaultBatchSize GaussDB 默认批量插入大小
	gaussDefaultBatchSize = 128
)

// ──────────────────────────── 全局变量 ────────────────────────────

// gaussLogComponent 日志组件
var gaussLogComponent = logger.ComponentAgentCore

// ──────────────────────────── 导出函数 ────────────────────────────

// NewGaussVectorStore 创建 GaussVectorStore 实例。
// 连接池惰性创建，初始化时不需要数据库可用。
// connString 格式：postgres://user:password@host:port/database
func NewGaussVectorStore(connString string) *GaussVectorStore {
	return &GaussVectorStore{
		connConfig:         connString,
		collectionMetadata: make(map[string]*gaussCollMeta),
		createPool:         defaultCreatePool,
	}
}

// Close 关闭数据库连接池。
func (s *GaussVectorStore) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pool != nil {
		s.pool.Close()
		s.pool = nil
	}
}

// CreateCollection 创建向量集合。
// 从 schema 构建建表 SQL，在向量字段上创建 DiskANN 索引。
//
// 对应 Python: GaussVectorStore.create_collection()
func (s *GaussVectorStore) CreateCollection(ctx context.Context, collectionName string, schema *CollectionSchema, opts ...Option) error {
	o := newOptions(opts...)

	// 校验 Schema
	if err := gaussValidateSchema(schema); err != nil {
		return err
	}

	// 检查集合是否已存在
	exists, err := s.CollectionExists(ctx, collectionName)
	if err != nil {
		return fmt.Errorf("CreateCollection: %w", err)
	}
	if exists {
		logger.Info(gaussLogComponent).
			Str("collection_name", collectionName).
			Msg("集合已存在，跳过创建")
		return nil
	}

	// 获取数据库连接池
	pool, err := s.getClient(ctx)
	if err != nil {
		return fmt.Errorf("CreateCollection: %w", err)
	}

	// 获取距离度量和向量字段
	distanceMetric := o.DistanceMetric
	if distanceMetric == "" {
		distanceMetric = gaussDefaultDistanceMetric
	}
	pgMetric := mapDistanceMetricToPG(distanceMetric)

	vectorFields := schema.GetVectorFields()
	if len(vectorFields) == 0 {
		return exception.BuildError(exception.StatusStoreVectorSchemaInvalid,
			exception.WithParam("error_msg", "Schema 缺少向量字段"),
		)
	}
	vf := vectorFields[0]

	// 解析 DiskANN 索引配置
	diskann := s.resolveDiskANNConfig(vf.Name, o)

	// 构建 CREATE TABLE SQL
	createSQL, err := gaussBuildCreateTableSQL(collectionName, schema)
	if err != nil {
		return fmt.Errorf("CreateCollection: %w", err)
	}

	// 执行建表
	if _, err := pool.Exec(ctx, createSQL); err != nil {
		logger.Error(gaussLogComponent).Err(err).
			Str("collection_name", collectionName).
			Str("event_type", "LLM_CALL_ERROR").
			Msg("创建集合失败")
		return fmt.Errorf("CreateCollection: %w", err)
	}

	// 构建 CREATE INDEX SQL
	indexName := fmt.Sprintf("idx_%s_%s", collectionName, vf.Name)
	indexSQL := gaussBuildCreateIndexSQL(indexName, collectionName, vf.Name, pgMetric, diskann)

	if _, err := pool.Exec(ctx, indexSQL); err != nil {
		logger.Error(gaussLogComponent).Err(err).
			Str("collection_name", collectionName).
			Str("event_type", "LLM_CALL_ERROR").
			Msg("创建 DiskANN 索引失败")
		return fmt.Errorf("CreateCollection: %w", err)
	}

	// 缓存元数据
	s.mu.Lock()
	s.collectionMetadata[collectionName] = &gaussCollMeta{
		DistanceMetric: strings.ToUpper(distanceMetric),
		VectorField:    vf.Name,
		VectorDim:      vf.Dim,
		SchemaVersion:  "1",
	}
	s.mu.Unlock()

	logger.Info(gaussLogComponent).
		Str("collection_name", collectionName).
		Str("vector_field", vf.Name).
		Str("distance_metric", distanceMetric).
		Msg("创建集合成功")

	return nil
}

// DeleteCollection 删除向量集合。
//
// 对应 Python: GaussVectorStore.delete_collection()
func (s *GaussVectorStore) DeleteCollection(ctx context.Context, collectionName string, opts ...Option) error {
	pool, err := s.getClient(ctx)
	if err != nil {
		return fmt.Errorf("DeleteCollection: %w", err)
	}

	tableName := pgx.Identifier{collectionName}.Sanitize()
	sql := fmt.Sprintf("DROP TABLE IF EXISTS %s", tableName)

	if _, err := pool.Exec(ctx, sql); err != nil {
		logger.Error(gaussLogComponent).Err(err).
			Str("collection_name", collectionName).
			Str("event_type", "LLM_CALL_ERROR").
			Msg("删除集合失败")
		return fmt.Errorf("DeleteCollection: %w", err)
	}

	// 清除缓存
	s.mu.Lock()
	delete(s.collectionMetadata, collectionName)
	s.mu.Unlock()

	logger.Info(gaussLogComponent).
		Str("collection_name", collectionName).
		Msg("删除集合成功")

	return nil
}

// CollectionExists 检查集合是否存在。
//
// 对应 Python: GaussVectorStore.collection_exists()
func (s *GaussVectorStore) CollectionExists(ctx context.Context, collectionName string, opts ...Option) (bool, error) {
	pool, err := s.getClient(ctx)
	if err != nil {
		return false, fmt.Errorf("CollectionExists: %w", err)
	}

	var exists bool
	err = pool.QueryRow(ctx,
		"SELECT EXISTS(SELECT 1 FROM information_schema.tables WHERE table_schema = 'public' AND table_name = $1)",
		collectionName,
	).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("CollectionExists: %w", err)
	}
	return exists, nil
}

// GetSchema 获取集合的 Schema。
//
// 对应 Python: GaussVectorStore.get_schema()
func (s *GaussVectorStore) GetSchema(ctx context.Context, collectionName string, opts ...Option) (*CollectionSchema, error) {
	pool, err := s.getClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("GetSchema: %w", err)
	}

	rows, err := pool.Query(ctx,
		"SELECT column_name, data_type, character_maximum_length, numeric_precision FROM information_schema.columns WHERE table_schema = 'public' AND table_name = $1 ORDER BY ordinal_position",
		collectionName,
	)
	if err != nil {
		logger.Error(gaussLogComponent).Err(err).
			Str("collection_name", collectionName).
			Msg("获取 Schema 失败")
		return nil, fmt.Errorf("GetSchema: %w", err)
	}
	defer rows.Close()

	schema, err := NewCollectionSchema()
	if err != nil {
		return nil, err
	}

	for rows.Next() {
		var colName, dataType string
		var charMaxLen *int
		var numPrec *int
		if err := rows.Scan(&colName, &dataType, &charMaxLen, &numPrec); err != nil {
			return nil, fmt.Errorf("GetSchema: %w", err)
		}

		dt := mapPGTypeToOurType(dataType)
		fieldOpts := []FieldOption{}
		if charMaxLen != nil && dt == VectorDataTypeVarchar {
			fieldOpts = append(fieldOpts, WithMaxLength(*charMaxLen))
		}
		if dt == VectorDataTypeFloatVector {
			// 尝试从 numeric_precision 或缓存获取 dim
			dim := 0
			if numPrec != nil {
				dim = *numPrec
			}
			if dim > 0 {
				fieldOpts = append(fieldOpts, WithDim(dim))
			} else {
				// 从缓存获取 dim
				s.mu.RLock()
				if meta, ok := s.collectionMetadata[collectionName]; ok {
					dim = meta.VectorDim
				}
				s.mu.RUnlock()
				if dim > 0 {
					fieldOpts = append(fieldOpts, WithDim(dim))
				}
			}
		}

		f, err := NewFieldSchema(colName, dt, fieldOpts...)
		if err != nil {
			return nil, fmt.Errorf("GetSchema: %w", err)
		}
		if _, err := schema.AddField(f); err != nil {
			return nil, fmt.Errorf("GetSchema: %w", err)
		}
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("GetSchema: %w", err)
	}

	return schema, nil
}

// AddDocs 添加文档到集合。
// 按 BatchSize 分批参数化插入。
//
// 对应 Python: GaussVectorStore.add_docs()
func (s *GaussVectorStore) AddDocs(ctx context.Context, collectionName string, docs []map[string]any, opts ...Option) error {
	if len(docs) == 0 {
		return nil
	}

	o := newOptions(opts...)
	batchSize := o.BatchSize
	if batchSize <= 0 {
		batchSize = gaussDefaultBatchSize
	}

	pool, err := s.getClient(ctx)
	if err != nil {
		return fmt.Errorf("AddDocs: %w", err)
	}

	tableName := pgx.Identifier{collectionName}.Sanitize()

	for i := 0; i < len(docs); i += batchSize {
		end := i + batchSize
		if end > len(docs) {
			end = len(docs)
		}
		batch := docs[i:end]

		if err := gaussInsertBatch(ctx, pool, tableName, batch); err != nil {
			logger.Error(gaussLogComponent).Err(err).
				Str("collection_name", collectionName).
				Int("doc_count", len(docs)).
				Int("batch_size", batchSize).
				Str("event_type", "LLM_CALL_ERROR").
				Msg("插入文档失败")
			return fmt.Errorf("AddDocs: %w", err)
		}
	}

	logger.Info(gaussLogComponent).
		Str("collection_name", collectionName).
		Int("doc_count", len(docs)).
		Int("batch_size", batchSize).
		Msg("插入文档成功")

	return nil
}

// Search 向量相似度搜索。
// 使用 GaussDB 的 <-> 距离操作符 + ORDER BY + LIMIT。
//
// 对应 Python: GaussVectorStore.search()
func (s *GaussVectorStore) Search(ctx context.Context, collectionName string, queryVector []float64, vectorField string, topK int, filters map[string]any, opts ...Option) ([]VectorSearchResult, error) {
	if topK <= 0 {
		topK = 5
	}

	pool, err := s.getClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("Search: %w", err)
	}

	// 获取元数据
	meta := s.getOrInitCollMeta(collectionName, vectorField)

	distanceMetric := gaussDefaultDistanceMetric
	if meta != nil && meta.DistanceMetric != "" {
		distanceMetric = meta.DistanceMetric
	}

	// 构建查询 SQL
	tableName := pgx.Identifier{collectionName}.Sanitize()
	vectorCol := pgx.Identifier{vectorField}.Sanitize()

	// 格式化查询向量为 floatvector 字面量
	vecStr := gaussFormatVector(queryVector)

	distanceCol := fmt.Sprintf("%s <-> '%s'::floatvector AS distance", vectorCol, vecStr)

	var whereClause string
	var args []any
	if len(filters) > 0 {
		whereClause, args = gaussBuildFilterClause(filters)
		whereClause = " WHERE " + whereClause
	}

	sql := fmt.Sprintf("SELECT *, %s FROM %s%s ORDER BY distance LIMIT %d",
		distanceCol, tableName, whereClause, topK)

	rows, err := pool.Query(ctx, sql, args...)
	if err != nil {
		logger.Error(gaussLogComponent).Err(err).
			Str("collection_name", collectionName).
			Str("event_type", "LLM_CALL_ERROR").
			Msg("搜索失败")
		return nil, fmt.Errorf("Search: %w", err)
	}
	defer rows.Close()

	// 获取列描述
	fieldDescriptions := rows.FieldDescriptions()

	var results []VectorSearchResult
	for rows.Next() {
		// 动态扫描所有列
		values := make([]any, len(fieldDescriptions))
		valuePtrs := make([]any, len(fieldDescriptions))
		for i := range values {
			valuePtrs[i] = &values[i]
		}
		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, fmt.Errorf("Search: %w", err)
		}

		fields := make(map[string]any)
		var rawDistance float64
		for i, fd := range fieldDescriptions {
			colName := fd.Name
			if colName == "distance" {
				// pgx 扫描 float 类型的 distance 列
				switch v := values[i].(type) {
				case float64:
					rawDistance = v
				case float32:
					rawDistance = float64(v)
				}
			} else {
				fields[colName] = gaussConvertValue(values[i])
			}
		}

		score := gaussNormalizeScore(rawDistance, distanceMetric)
		results = append(results, VectorSearchResult{
			Score:  score,
			Fields: fields,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("Search: %w", err)
	}

	resultCount := len(results)
	logger.Info(gaussLogComponent).
		Str("collection_name", collectionName).
		Int("top_k", topK).
		Str("distance_metric", distanceMetric).
		Int("result_count", resultCount).
		Msg("搜索完成")

	return results, nil
}

// DeleteDocsByIDs 按 ID 删除文档。
// 使用 = ANY($1) 参数化删除。
//
// 对应 Python: GaussVectorStore.delete_docs_by_ids()
func (s *GaussVectorStore) DeleteDocsByIDs(ctx context.Context, collectionName string, ids []string, opts ...Option) error {
	if len(ids) == 0 {
		return nil
	}

	pool, err := s.getClient(ctx)
	if err != nil {
		return fmt.Errorf("DeleteDocsByIDs: %w", err)
	}

	tableName := pgx.Identifier{collectionName}.Sanitize()
	pkCol := pgx.Identifier{"id"}.Sanitize()

	sql := fmt.Sprintf("DELETE FROM %s WHERE %s = ANY($1)", tableName, pkCol)
	if _, err := pool.Exec(ctx, sql, ids); err != nil {
		logger.Error(gaussLogComponent).Err(err).
			Str("collection_name", collectionName).
			Str("event_type", "LLM_CALL_ERROR").
			Msg("按 ID 删除文档失败")
		return fmt.Errorf("DeleteDocsByIDs: %w", err)
	}

	logger.Info(gaussLogComponent).
		Str("collection_name", collectionName).
		Int("id_count", len(ids)).
		Msg("按 ID 删除文档成功")

	return nil
}

// DeleteDocsByFilters 按标量字段过滤条件删除文档。
//
// 对应 Python: GaussVectorStore.delete_docs_by_filters()
func (s *GaussVectorStore) DeleteDocsByFilters(ctx context.Context, collectionName string, filters map[string]any, opts ...Option) error {
	if len(filters) == 0 {
		return nil
	}

	pool, err := s.getClient(ctx)
	if err != nil {
		return fmt.Errorf("DeleteDocsByFilters: %w", err)
	}

	tableName := pgx.Identifier{collectionName}.Sanitize()
	whereClause, args := gaussBuildFilterClause(filters)

	sql := fmt.Sprintf("DELETE FROM %s WHERE %s", tableName, whereClause)
	if _, err := pool.Exec(ctx, sql, args...); err != nil {
		logger.Error(gaussLogComponent).Err(err).
			Str("collection_name", collectionName).
			Str("event_type", "LLM_CALL_ERROR").
			Msg("按过滤条件删除文档失败")
		return fmt.Errorf("DeleteDocsByFilters: %w", err)
	}

	logger.Info(gaussLogComponent).
		Str("collection_name", collectionName).
		Int("filter_count", len(filters)).
		Msg("按过滤条件删除文档成功")

	return nil
}

// ListCollectionNames 列出所有集合名称。
//
// 对应 Python: GaussVectorStore.list_collection_names()
func (s *GaussVectorStore) ListCollectionNames(ctx context.Context) ([]string, error) {
	pool, err := s.getClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("ListCollectionNames: %w", err)
	}

	rows, err := pool.Query(ctx,
		"SELECT table_name FROM information_schema.tables WHERE table_schema = 'public' AND table_type = 'BASE TABLE'",
	)
	if err != nil {
		return nil, fmt.Errorf("ListCollectionNames: %w", err)
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("ListCollectionNames: %w", err)
		}
		names = append(names, name)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ListCollectionNames: %w", err)
	}

	return names, nil
}

// UpdateSchema 执行 Schema 迁移操作。
// ⤵️ 预留：实际迁移逻辑待 7.22/7.23 实现后回填。
//
// 对应 Python: GaussVectorStore.update_schema()
func (s *GaussVectorStore) UpdateSchema(ctx context.Context, collectionName string, operations []any, opts ...Option) error {
	return exception.BuildError(exception.StatusStoreVectorSchemaInvalid,
		exception.WithParam("error_msg", "UpdateSchema 未实现，待 7.22/7.23 回填"),
	)
}

// UpdateCollectionMetadata 更新集合元数据。
//
// 对应 Python: GaussVectorStore.update_collection_metadata()
func (s *GaussVectorStore) UpdateCollectionMetadata(ctx context.Context, collectionName string, metadata map[string]any, opts ...Option) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	meta, ok := s.collectionMetadata[collectionName]
	if !ok {
		meta = &gaussCollMeta{}
		s.collectionMetadata[collectionName] = meta
	}

	if v, ok := metadata["distance_metric"].(string); ok {
		meta.DistanceMetric = v
	}
	if v, ok := metadata["vector_field"].(string); ok {
		meta.VectorField = v
	}
	if v, ok := metadata["vector_dim"].(int); ok {
		meta.VectorDim = v
	}
	if v, ok := metadata["schema_version"].(string); ok {
		meta.SchemaVersion = v
	}

	return nil
}

// GetCollectionMetadata 获取集合元数据。
// 优先缓存，未命中则从 information_schema 回查。
//
// 对应 Python: GaussVectorStore.get_collection_metadata()
func (s *GaussVectorStore) GetCollectionMetadata(ctx context.Context, collectionName string, opts ...Option) (map[string]any, error) {
	s.mu.RLock()
	meta, ok := s.collectionMetadata[collectionName]
	s.mu.RUnlock()

	if ok {
		return map[string]any{
			"distance_metric": meta.DistanceMetric,
			"vector_field":    meta.VectorField,
			"vector_dim":      meta.VectorDim,
			"schema_version":  meta.SchemaVersion,
		}, nil
	}

	// 从数据库回查
	pool, err := s.getClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("GetCollectionMetadata: %w", err)
	}

	var colName, dataType string
	err = pool.QueryRow(ctx,
		"SELECT column_name, data_type FROM information_schema.columns WHERE table_schema = 'public' AND table_name = $1 AND data_type = 'floatvector' LIMIT 1",
		collectionName,
	).Scan(&colName, &dataType)
	if err != nil {
		logger.Error(gaussLogComponent).Err(err).
			Str("collection_name", collectionName).
			Msg("获取集合元数据失败")
		return nil, fmt.Errorf("GetCollectionMetadata: %w", err)
	}

	// 更新缓存
	newMeta := &gaussCollMeta{
		VectorField: colName,
	}
	s.mu.Lock()
	s.collectionMetadata[collectionName] = newMeta
	s.mu.Unlock()

	return map[string]any{
		"vector_field": newMeta.VectorField,
	}, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// defaultCreatePool 默认连接池创建函数
func defaultCreatePool(ctx context.Context, connString string) (dbClient, error) {
	config, err := pgxpool.ParseConfig(connString)
	if err != nil {
		return nil, fmt.Errorf("解析连接字符串失败: %w", err)
	}
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("创建连接池失败: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("连接池 Ping 失败: %w", err)
	}
	return pool, nil
}

// getClient 惰性获取数据库连接池，双重检查锁
func (s *GaussVectorStore) getClient(ctx context.Context) (dbClient, error) {
	s.mu.RLock()
	if s.pool != nil {
		s.mu.RUnlock()
		return s.pool, nil
	}
	s.mu.RUnlock()

	s.mu.Lock()
	defer s.mu.Unlock()
	// 双重检查
	if s.pool != nil {
		return s.pool, nil
	}

	pool, err := s.createPool(ctx, s.connConfig)
	if err != nil {
		logger.Error(gaussLogComponent).Err(err).
			Msg("创建连接池失败")
		return nil, err
	}
	s.pool = pool
	return pool, nil
}

// getOrInitCollMeta 获取或初始化集合元数据
func (s *GaussVectorStore) getOrInitCollMeta(collectionName string, vectorField string) *gaussCollMeta {
	s.mu.RLock()
	meta, ok := s.collectionMetadata[collectionName]
	s.mu.RUnlock()
	if ok {
		return meta
	}

	// 初始化默认元数据
	s.mu.Lock()
	defer s.mu.Unlock()
	// 双重检查
	if meta, ok := s.collectionMetadata[collectionName]; ok {
		return meta
	}

	meta = &gaussCollMeta{
		DistanceMetric: gaussDefaultDistanceMetric,
		VectorField:    vectorField,
	}
	s.collectionMetadata[collectionName] = meta
	return meta
}

// resolveDiskANNConfig 从 Option 解析 DiskANN 配置，未指定则使用默认值
func (s *GaussVectorStore) resolveDiskANNConfig(fieldName string, o Options) *vector_fields.GaussDiskANN {
	if o.VectorField != nil {
		if diskann, ok := o.VectorField.(*vector_fields.GaussDiskANN); ok {
			return diskann
		}
	}
	return vector_fields.NewGaussDiskANN(fieldName)
}

// gaussValidateSchema 校验 Schema 是否满足 GaussDB 向量存储要求
func gaussValidateSchema(schema *CollectionSchema) error {
	if schema.GetPrimaryKeyField() == nil {
		return exception.BuildError(exception.StatusStoreVectorSchemaInvalid,
			exception.WithParam("error_msg", "Schema 缺少主键字段"),
		)
	}
	if len(schema.GetVectorFields()) == 0 {
		return exception.BuildError(exception.StatusStoreVectorSchemaInvalid,
			exception.WithParam("error_msg", "Schema 缺少向量字段"),
		)
	}
	return nil
}

// mapFieldTypeToPG 将 VectorDataType 映射为 GaussDB/PostgreSQL 类型字符串
func mapFieldTypeToPG(dt VectorDataType) (string, error) {
	switch dt {
	case VectorDataTypeVarchar:
		return "VARCHAR", nil
	case VectorDataTypeFloatVector:
		return "FLOATVECTOR", nil
	case VectorDataTypeInt64:
		return "BIGINT", nil
	case VectorDataTypeInt32:
		return "INTEGER", nil
	case VectorDataTypeInt16:
		return "SMALLINT", nil
	case VectorDataTypeInt8:
		return "SMALLINT", nil
	case VectorDataTypeFloat:
		return "REAL", nil
	case VectorDataTypeDouble:
		return "DOUBLE PRECISION", nil
	case VectorDataTypeBool:
		return "BOOLEAN", nil
	case VectorDataTypeJSON:
		return "JSONB", nil
	case VectorDataTypeArray:
		return "", fmt.Errorf("GaussDB 向量存储不支持 ARRAY 类型")
	default:
		return "", fmt.Errorf("不支持的类型: %d", dt)
	}
}

// mapPGTypeToOurType 将 GaussDB/PostgreSQL 类型映射为 VectorDataType
func mapPGTypeToOurType(pgType string) VectorDataType {
	switch strings.ToLower(pgType) {
	case "varchar", "character varying":
		return VectorDataTypeVarchar
	case "floatvector":
		return VectorDataTypeFloatVector
	case "bigint", "int8":
		return VectorDataTypeInt64
	case "integer", "int", "int4":
		return VectorDataTypeInt32
	case "smallint", "int2":
		return VectorDataTypeInt16
	case "real", "float4":
		return VectorDataTypeFloat
	case "double precision", "float8":
		return VectorDataTypeDouble
	case "boolean", "bool":
		return VectorDataTypeBool
	case "jsonb", "json":
		return VectorDataTypeJSON
	default:
		return VectorDataTypeVarchar
	}
}

// mapDistanceMetricToPG 将距离度量映射为 GaussDB 索引操作符类别
func mapDistanceMetricToPG(metric string) gaussDistanceMetric {
	switch strings.ToUpper(metric) {
	case "L2":
		return gaussMetricL2
	case "COSINE":
		return gaussMetricCosine
	default:
		return gaussMetricCosine
	}
}

// gaussNormalizeScore 将 GaussDB 原始距离转换为归一化相似度 [0,1]
// COSINE: GaussDB <-> 返回余弦距离 [0,2]，等价于 ChromaDB 语义，用 ConvertCosineDistance
// L2: 用 ConvertL2Squared
func gaussNormalizeScore(rawScore float64, metric string) float64 {
	switch strings.ToUpper(metric) {
	case "COSINE":
		return ConvertCosineDistance(rawScore)
	case "L2":
		return ConvertL2Squared(rawScore, 4.0)
	default:
		return ConvertCosineDistance(rawScore)
	}
}

// gaussBuildCreateTableSQL 构建 CREATE TABLE SQL
func gaussBuildCreateTableSQL(collectionName string, schema *CollectionSchema) (string, error) {
	tableName := pgx.Identifier{collectionName}.Sanitize()

	var colDefs []string
	for _, f := range schema.Fields() {
		pgType, err := mapFieldTypeToPG(f.DType)
		if err != nil {
			return "", err
		}

		// 拼接类型参数
		switch f.DType {
		case VectorDataTypeVarchar:
			maxLen := f.MaxLength
			if maxLen <= 0 {
				maxLen = defaultMaxLength
			}
			pgType = fmt.Sprintf("VARCHAR(%d)", maxLen)
		case VectorDataTypeFloatVector:
			pgType = fmt.Sprintf("FLOATVECTOR(%d)", f.Dim)
		}

		colName := pgx.Identifier{f.Name}.Sanitize()
		colDef := fmt.Sprintf("%s %s", colName, pgType)

		if f.IsPrimary {
			colDef += " PRIMARY KEY"
		}
		if f.DefaultValue != nil {
			colDef += fmt.Sprintf(" DEFAULT '%v'", f.DefaultValue)
		}

		colDefs = append(colDefs, colDef)
	}

	return fmt.Sprintf("CREATE TABLE %s (%s)", tableName, strings.Join(colDefs, ", ")), nil
}

// gaussBuildCreateIndexSQL 构建 CREATE INDEX ... USING GSDISKANN SQL
func gaussBuildCreateIndexSQL(indexName, collectionName, vectorField string, metric gaussDistanceMetric, diskann *vector_fields.GaussDiskANN) string {
	idxName := pgx.Identifier{indexName}.Sanitize()
	tableName := pgx.Identifier{collectionName}.Sanitize()
	vectorCol := pgx.Identifier{vectorField}.Sanitize()

	return fmt.Sprintf(
		"CREATE INDEX %s ON %s USING GSDISKANN (%s %s) WITH (enable_pq = %t, pg_nseg = %d, pg_nclus = %d, num_parallels = %d, quantization_type = '%s', subgraph_count = %d)",
		idxName, tableName, vectorCol, string(metric),
		diskann.EnablePQ, diskann.PGNseg, diskann.PGNclus,
		diskann.NumParallels, diskann.QuantizationType, diskann.SubgraphCount,
	)
}

// gaussFormatVector 将 float64 切片格式化为 floatvector 字面量 [1.0,2.0,3.0]
func gaussFormatVector(vec []float64) string {
	parts := make([]string, len(vec))
	for i, v := range vec {
		parts[i] = fmt.Sprintf("%g", v)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

// gaussBuildFilterClause 构建参数化 WHERE 子句（等值过滤，类型感知）
// 返回 WHERE 子句（不含 WHERE 关键字）和参数列表
func gaussBuildFilterClause(filters map[string]any) (string, []any) {
	var conditions []string
	var args []any
	paramIdx := 1

	for key, val := range filters {
		colName := pgx.Identifier{key}.Sanitize()
		conditions = append(conditions, fmt.Sprintf("%s = $%d", colName, paramIdx))
		args = append(args, val)
		paramIdx++
	}

	return strings.Join(conditions, " AND "), args
}

// gaussInsertBatch 执行一批文档的参数化 INSERT
func gaussInsertBatch(ctx context.Context, pool dbClient, tableName string, docs []map[string]any) error {
	if len(docs) == 0 {
		return nil
	}

	// 从第一个文档获取列名（假设所有文档字段一致）
	var colNames []string
	for k := range docs[0] {
		colNames = append(colNames, k)
	}

	// 构建列名列表
	sanitizedCols := make([]string, len(colNames))
	for i, col := range colNames {
		sanitizedCols[i] = pgx.Identifier{col}.Sanitize()
	}

	// 构建每行的占位符
	rowPlaceholder := make([]string, len(colNames))
	for i := range colNames {
		rowPlaceholder[i] = fmt.Sprintf("$%d", i+1)
	}

	// 逐行插入
	for _, doc := range docs {
		values := make([]any, len(colNames))
		for i, col := range colNames {
			val := doc[col]
			// float64 切片转为 floatvector 字面量
			if vec, ok := val.([]float64); ok {
				val = gaussFormatVector(vec)
			}
			values[i] = val
		}

		sql := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
			tableName,
			strings.Join(sanitizedCols, ", "),
			strings.Join(rowPlaceholder, ", "),
		)

		if _, err := pool.Exec(ctx, sql, values...); err != nil {
			return err
		}
	}

	return nil
}

// gaussConvertValue 转换 pgx 返回的值为 Go 原生类型
func gaussConvertValue(v any) any {
	switch val := v.(type) {
	case []byte:
		return string(val)
	case float32:
		return float64(val)
	case int16:
		return int64(val)
	case int32:
		return int64(val)
	case int64:
		return val
	default:
		return val
	}
}
