package milvus

import (
	"context"
	"fmt"

	"github.com/milvus-io/milvus/client/v2/entity"
	"github.com/milvus-io/milvus/client/v2/index"
	milvusclient "github.com/milvus-io/milvus/client/v2/milvusclient"

	graph "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/graph"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// CollectionEntity 实体集合名称
	CollectionEntity = "entity"
	// CollectionRelation 关系集合名称
	CollectionRelation = "relation"
	// CollectionEpisode 片段集合名称
	CollectionEpisode = "episode"

	// 默认索引参数
	defaultM              = 32
	defaultEfConstruction = 256
	defaultEfSearch       = 64

	// logComponent 日志组件
	logComponent = logger.ComponentAgentCore
)

// ──────────────────────────── 导出函数 ────────────────────────────

// EnsureCollections 确保三个图集合（Entity/Relation/Episode）存在。
// 如果集合不存在则创建，已存在则跳过。
// 创建集合时会同时构建索引和加载集合。
//
// 对应 Python: generate_milvus_schema.py (ensure_collections)
func EnsureCollections(ctx context.Context, client milvusClient, storageCfg *graph.GraphStoreStorageConfig, indexCfg *graph.GraphStoreIndexConfig, embedDim int) error {
	collections := []struct {
		name      string
		buildFunc func(*graph.GraphStoreStorageConfig, int) (*entity.Schema, error)
	}{
		{CollectionEntity, buildEntitySchema},
		{CollectionRelation, buildRelationSchema},
		{CollectionEpisode, buildEpisodeSchema},
	}

	for _, coll := range collections {
		has, err := client.HasCollection(ctx, milvusclient.NewHasCollectionOption(coll.name))
		if err != nil {
			return fmt.Errorf("检查集合 %s 是否存在失败: %w", coll.name, err)
		}
		if has {
			logger.Info(logComponent).Str("collection", coll.name).Msg("集合已存在，跳过创建")
			continue
		}

		schema, err := coll.buildFunc(storageCfg, embedDim)
		if err != nil {
			return fmt.Errorf("构建集合 %s Schema 失败: %w", coll.name, err)
		}

		// 构建索引选项
		indexOpts, err := buildIndexOptions(indexCfg, coll.name, schema)
		if err != nil {
			return fmt.Errorf("构建集合 %s 索引选项失败: %w", coll.name, err)
		}

		createOpt := milvusclient.NewCreateCollectionOption(coll.name, schema)
		if len(indexOpts) > 0 {
			createOpt = createOpt.WithIndexOptions(indexOpts...)
		}

		if err := client.CreateCollection(ctx, createOpt); err != nil {
			return fmt.Errorf("创建集合 %s 失败: %w", coll.name, err)
		}

		logger.Info(logComponent).Str("collection", coll.name).Msg("成功创建集合")
	}

	return nil
}

// buildEntitySchema 构建 Entity 集合的 Schema。
//
// Entity 集合包含通用字段 + name/name_embedding + attributes + relations + episodes
func buildEntitySchema(storageCfg *graph.GraphStoreStorageConfig, embedDim int) (*entity.Schema, error) {
	schema := entity.NewSchema().WithName(CollectionEntity).
		WithDescription("知识图谱实体集合").
		WithDynamicFieldEnabled(true)

	addCommonFields(schema, storageCfg, embedDim)

	// Entity 特有字段
	schema = schema.WithField(
		entity.NewField().WithName("name").
			WithDataType(entity.FieldTypeVarChar).
			WithMaxLength(int64(storageCfg.Name)),
	)
	schema = schema.WithField(
		entity.NewField().WithName("name_embedding").
			WithDataType(entity.FieldTypeFloatVector).
			WithDim(int64(embedDim)),
	)
	schema = schema.WithField(
		entity.NewField().WithName("attributes").
			WithDataType(entity.FieldTypeJSON),
	)
	schema = schema.WithField(
		entity.NewField().WithName("relations").
			WithDataType(entity.FieldTypeArray).
			WithElementType(entity.FieldTypeVarChar).
			WithMaxLength(int64(storageCfg.UUID)).
			WithMaxCapacity(int64(storageCfg.Relations)),
	)
	schema = schema.WithField(
		entity.NewField().WithName("episodes").
			WithDataType(entity.FieldTypeArray).
			WithElementType(entity.FieldTypeVarChar).
			WithMaxLength(int64(storageCfg.UUID)).
			WithMaxCapacity(int64(storageCfg.Episodes)),
	)

	// 添加 BM25 Function
	schema = schema.WithFunction(
		entity.NewFunction().WithName("content_bm25_fn").
			WithType(entity.FunctionTypeBM25).
			WithInputFields("content").
			WithOutputFields("content_bm25"),
	)

	return schema, nil
}

// buildRelationSchema 构建 Relation 集合的 Schema。
//
// Relation 集合包含通用字段 + name + lhs/rhs + valid_since/valid_until + offset_since/offset_until
func buildRelationSchema(storageCfg *graph.GraphStoreStorageConfig, embedDim int) (*entity.Schema, error) {
	schema := entity.NewSchema().WithName(CollectionRelation).
		WithDescription("知识图谱关系集合").
		WithDynamicFieldEnabled(true)

	addCommonFields(schema, storageCfg, embedDim)

	// Relation 特有字段
	schema = schema.WithField(
		entity.NewField().WithName("name").
			WithDataType(entity.FieldTypeVarChar).
			WithMaxLength(int64(storageCfg.Name)),
	)
	schema = schema.WithField(
		entity.NewField().WithName("lhs").
			WithDataType(entity.FieldTypeVarChar).
			WithMaxLength(int64(storageCfg.UUID)),
	)
	schema = schema.WithField(
		entity.NewField().WithName("rhs").
			WithDataType(entity.FieldTypeVarChar).
			WithMaxLength(int64(storageCfg.UUID)),
	)
	schema = schema.WithField(
		entity.NewField().WithName("valid_since").
			WithDataType(entity.FieldTypeInt64),
	)
	schema = schema.WithField(
		entity.NewField().WithName("valid_until").
			WithDataType(entity.FieldTypeInt64),
	)
	schema = schema.WithField(
		entity.NewField().WithName("offset_since").
			WithDataType(entity.FieldTypeInt32),
	)
	schema = schema.WithField(
		entity.NewField().WithName("offset_until").
			WithDataType(entity.FieldTypeInt32),
	)

	// 添加 BM25 Function
	schema = schema.WithFunction(
		entity.NewFunction().WithName("content_bm25_fn").
			WithType(entity.FunctionTypeBM25).
			WithInputFields("content").
			WithOutputFields("content_bm25"),
	)

	return schema, nil
}

// buildEpisodeSchema 构建 Episode 集合的 Schema。
//
// Episode 集合包含通用字段 + valid_since + entities
func buildEpisodeSchema(storageCfg *graph.GraphStoreStorageConfig, embedDim int) (*entity.Schema, error) {
	schema := entity.NewSchema().WithName(CollectionEpisode).
		WithDescription("知识图谱片段集合").
		WithDynamicFieldEnabled(true)

	addCommonFields(schema, storageCfg, embedDim)

	// Episode 特有字段
	schema = schema.WithField(
		entity.NewField().WithName("valid_since").
			WithDataType(entity.FieldTypeInt64),
	)
	schema = schema.WithField(
		entity.NewField().WithName("entities").
			WithDataType(entity.FieldTypeArray).
			WithElementType(entity.FieldTypeVarChar).
			WithMaxLength(int64(storageCfg.UUID)).
			WithMaxCapacity(int64(storageCfg.Entities)),
	)

	// 添加 BM25 Function
	schema = schema.WithFunction(
		entity.NewFunction().WithName("content_bm25_fn").
			WithType(entity.FunctionTypeBM25).
			WithInputFields("content").
			WithOutputFields("content_bm25"),
	)

	return schema, nil
}

// buildIndexOptions 根据集合类型构建索引选项。
//
// Entity 集合：name_embedding + content_embedding + content_bm25 三个索引
// Relation/Episode 集合：content_embedding + content_bm25 两个索引
func buildIndexOptions(indexCfg *graph.GraphStoreIndexConfig, collection string, schema *entity.Schema) ([]milvusclient.CreateIndexOption, error) {
	metricType := mapDistanceMetric(indexCfg.DistanceMetric)
	var opts []milvusclient.CreateIndexOption

	// content_embedding 索引（所有集合都有）
	contentEmbIdx := index.NewHNSWIndex(metricType, defaultM, defaultEfConstruction)
	opts = append(opts, milvusclient.NewCreateIndexOption(collection, "content_embedding", contentEmbIdx))

	// name_embedding 索引（仅 Entity 集合）
	if collection == CollectionEntity {
		nameEmbIdx := index.NewHNSWIndex(metricType, defaultM, defaultEfConstruction)
		opts = append(opts, milvusclient.NewCreateIndexOption(collection, "name_embedding", nameEmbIdx))
	}

	// content_bm25 稀疏向量索引（所有集合都有）
	// BM25 稀疏向量使用 SPARSE_INVERTED_INDEX，dropRatio=0.2 与 Python 一致
	sparseIdx := index.NewSparseInvertedIndex(entity.BM25, 0.2)
	opts = append(opts, milvusclient.NewCreateIndexOption(collection, "content_bm25", sparseIdx))

	// 标量字段索引（inverted index）
	scalarFields := []string{"uuid", "created_at", "user_id", "obj_type"}
	if collection == CollectionRelation {
		scalarFields = append(scalarFields, "lhs", "rhs", "valid_since", "valid_until")
	} else if collection == CollectionEpisode {
		scalarFields = append(scalarFields, "valid_since")
	}
	for _, field := range scalarFields {
		invertedIdx := index.NewInvertedIndex()
		opts = append(opts, milvusclient.NewCreateIndexOption(collection, field, invertedIdx))
	}

	return opts, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// addCommonFields 向 Schema 添加三个集合共有的通用字段。
//
// 通用字段：uuid(PK), created_at, user_id, obj_type, language, metadata, content,
// content_embedding, content_bm25
func addCommonFields(schema *entity.Schema, storageCfg *graph.GraphStoreStorageConfig, embedDim int) {
	// uuid — 主键
	schema = schema.WithField(
		entity.NewField().WithName("uuid").
			WithDataType(entity.FieldTypeVarChar).
			WithIsPrimaryKey(true).
			WithMaxLength(int64(storageCfg.UUID)),
	)
	// created_at
	schema = schema.WithField(
		entity.NewField().WithName("created_at").
			WithDataType(entity.FieldTypeInt64),
	)
	// user_id
	schema = schema.WithField(
		entity.NewField().WithName("user_id").
			WithDataType(entity.FieldTypeVarChar).
			WithMaxLength(int64(storageCfg.UserID)),
	)
	// obj_type
	schema = schema.WithField(
		entity.NewField().WithName("obj_type").
			WithDataType(entity.FieldTypeVarChar).
			WithMaxLength(int64(storageCfg.ObjType)),
	)
	// language
	schema = schema.WithField(
		entity.NewField().WithName("language").
			WithDataType(entity.FieldTypeVarChar).
			WithMaxLength(int64(storageCfg.Language)),
	)
	// metadata
	schema = schema.WithField(
		entity.NewField().WithName("metadata").
			WithDataType(entity.FieldTypeJSON),
	)
	// content
	schema = schema.WithField(
		entity.NewField().WithName("content").
			WithDataType(entity.FieldTypeVarChar).
			WithMaxLength(int64(storageCfg.Content)),
	)
	// content_embedding — dense 向量
	schema = schema.WithField(
		entity.NewField().WithName("content_embedding").
			WithDataType(entity.FieldTypeFloatVector).
			WithDim(int64(embedDim)),
	)
	// content_bm25 — sparse 向量（由 BM25 Function 自动生成）
	schema = schema.WithField(
		entity.NewField().WithName("content_bm25").
			WithDataType(entity.FieldTypeSparseVector),
	)
}

// mapDistanceMetric 将字符串距离度量映射为 entity.MetricType。
func mapDistanceMetric(metric string) entity.MetricType {
	switch metric {
	case "cosine", "COSINE":
		return entity.COSINE
	case "euclidean", "l2", "L2":
		return entity.L2
	case "dot", "ip", "IP":
		return entity.IP
	default:
		return entity.COSINE
	}
}
