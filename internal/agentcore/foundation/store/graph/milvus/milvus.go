package milvus

import (
	"context"
	"fmt"
	"sync"

	milvusclient "github.com/milvus-io/milvus/client/v2/milvusclient"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/embedding"
	graph "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/graph"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// MilvusGraphStore Milvus 图存储实现。
//
// 通过嵌入 graphWriter 和 graphSearcher 拆分读写/搜索职责，
// 实现 graph.BaseGraphStore 接口。
//
// 对应 Python: MilvusGraphStore
type MilvusGraphStore struct {
	*graphWriter
	*graphSearcher

	// config 图存储配置
	config *graph.GraphConfig
	// client Milvus 客户端（懒初始化）
	client milvusClient
	// createClient 客户端创建函数（可替换用于测试）
	createClient func(ctx context.Context, uri, token, dbName string) (milvusClient, error)
	// mu 读写锁
	mu sync.RWMutex
	// initialized 是否已初始化
	initialized bool
}

// ──────────────────────────── 全局变量 ────────────────────────────

// 编译时检查接口实现
var _ graph.BaseGraphStore = (*MilvusGraphStore)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewMilvusGraphStore 创建 Milvus 图存储实例。
func NewMilvusGraphStore(config *graph.GraphConfig) *MilvusGraphStore {
	s := &MilvusGraphStore{
		config:       config,
		createClient: defaultCreateGraphClient,
	}
	return s
}

// Config 获取图存储配置
func (s *MilvusGraphStore) Config() *graph.GraphConfig {
	return s.config
}

// Rebuild 重建所有集合和索引。
// 对齐 Python: 先尝试 LoadCollection，加载成功则直接返回；加载失败则删数据库再重建。
//
// 注意（T-20）：此操作先删除旧数据再重建，如果重建失败旧数据已丢失。
// Milvus 不支持集合原子重命名，无法实现"备份旧集合 → 建新集合 → 删备份"的事务性回滚。
// 重建前会记录即将删除的集合信息，以便手动恢复参考。
func (s *MilvusGraphStore) Rebuild(ctx context.Context) error {
	client, err := s.getClient(ctx)
	if err != nil {
		return err
	}

	// 对齐 Python: 先尝试 load_collection，如果成功则不需要 rebuild
	loadOK := true
	for _, coll := range []string{CollectionEntity, CollectionRelation, CollectionEpisode} {
		if err := client.LoadCollection(ctx, milvusclient.NewLoadCollectionOption(coll)); err != nil {
			loadOK = false
			break
		}
	}
	if loadOK {
		logger.Info(logComponent).Msg("集合加载成功，跳过重建")
		return nil
	}

	// 记录即将删除的集合信息，便于手动恢复参考（对齐 T-20 修复）
	for _, coll := range []string{CollectionEntity, CollectionRelation, CollectionEpisode} {
		has, err := client.HasCollection(ctx, milvusclient.NewHasCollectionOption(coll))
		if err == nil && has {
			logger.Warn(logComponent).Str("collection", coll).Msg("即将删除集合，重建失败时旧数据将丢失")
		}
	}

	// 加载失败，删数据库再重建，对齐 Python: drop database
	if err := client.DropDatabase(ctx, milvusclient.NewDropDatabaseOption(s.config.Name)); err != nil {
		logger.Warn(logComponent).Err(err).Str("db_name", s.config.Name).Msg("删除数据库失败，回退到删除集合")
		// 回退到删除集合
		for _, coll := range []string{CollectionEntity, CollectionRelation, CollectionEpisode} {
			has, err := client.HasCollection(ctx, milvusclient.NewHasCollectionOption(coll))
			if err != nil {
				return fmt.Errorf("检查集合 %s 失败: %w", coll, err)
			}
			if has {
				if err := client.DropCollection(ctx, milvusclient.NewDropCollectionOption(coll)); err != nil {
					return fmt.Errorf("删除集合 %s 失败: %w", coll, err)
				}
			}
		}
	}

	// 重新创建集合
	if err := EnsureCollections(ctx, client, s.config.StorageConfig, s.config.IndexConfig, s.config.EmbedDim); err != nil {
		logger.Error(logComponent).Err(err).Msg("重建集合失败，旧数据已丢失，请手动恢复")
		return fmt.Errorf("重建集合失败: %w", err)
	}

	logger.Info(logComponent).Msg("成功重建图存储集合")
	return nil
}

// Refresh 刷新数据（flush + 可选 compact）。
// 对齐 Python: flush + 可选 compact。
func (s *MilvusGraphStore) Refresh(ctx context.Context, opts ...graph.Option) error {
	client, err := s.getClient(ctx)
	if err != nil {
		return err
	}

	for _, coll := range []string{CollectionEntity, CollectionRelation, CollectionEpisode} {
		if err := client.Flush(ctx, milvusclient.NewFlushOption(coll)); err != nil {
			logger.Warn(logComponent).Err(err).Str("collection", coll).Msg("Flush 失败")
		}
	}

	// 对齐 Python: 可选 compact 操作
	if s.config.EnableCompact {
		for _, coll := range []string{CollectionEntity, CollectionRelation, CollectionEpisode} {
			if _, err := client.Compact(ctx, milvusclient.NewCompactOption(coll)); err != nil {
				logger.Warn(logComponent).Err(err).Str("collection", coll).Msg("Compact 失败")
			}
		}
	}

	return nil
}

// Close 关闭存储连接。
func (s *MilvusGraphStore) Close() error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.client != nil {
		if err := s.client.Close(context.Background()); err != nil {
			logger.Error(logComponent).Err(err).Msg("关闭 Milvus 连接失败")
			return err
		}
		s.client = nil
	}

	logger.Info(logComponent).Msg("成功关闭图存储连接")
	return nil
}

// AddEntity 添加实体。
func (s *MilvusGraphStore) AddEntity(ctx context.Context, entities []*graph.Entity, opts ...graph.Option) error {
	if err := s.ensureInit(ctx); err != nil {
		return err
	}
	return s.addEntity(ctx, entities, opts...)
}

// AddRelation 添加关系。
func (s *MilvusGraphStore) AddRelation(ctx context.Context, relations []*graph.Relation, opts ...graph.Option) error {
	if err := s.ensureInit(ctx); err != nil {
		return err
	}
	return s.addRelation(ctx, relations, opts...)
}

// AddEpisode 添加片段。
func (s *MilvusGraphStore) AddEpisode(ctx context.Context, episodes []*graph.Episode, opts ...graph.Option) error {
	if err := s.ensureInit(ctx); err != nil {
		return err
	}
	return s.addEpisode(ctx, episodes, opts...)
}

// Query 按ID或过滤表达式查询数据。
// 对齐 Python: IDs 和 Expr 都为空且无 limit 时报错。
func (s *MilvusGraphStore) Query(ctx context.Context, collection string, opts ...graph.Option) ([]map[string]any, error) {
	if err := s.ensureInit(ctx); err != nil {
		return nil, err
	}

	o := applyGraphOptions(opts...)
	expr := ""
	if len(o.IDs) > 0 {
		ids := make([]string, 0, len(o.IDs))
		for _, id := range o.IDs {
			ids = append(ids, fmt.Sprintf("%v", id))
		}
		expr = buildIDFilterExpr(ids)
	} else if o.Expr != nil {
		exprVal, exprErr := o.Expr.ToExpr("milvus")
		if exprErr != nil {
			return nil, fmt.Errorf("构建查询表达式失败: %w", exprErr)
		}
		strExpr, ok := exprVal.(string)
		if !ok {
			return nil, fmt.Errorf("milvus 后端应返回 string 类型的表达式")
		}
		expr = strExpr
	}

	// 对齐 Python: expr 和 ids 都为 None 且没有 limit 时报错
	if expr == "" && o.K == 0 {
		return nil, fmt.Errorf("查询必须提供 IDs 或过滤表达式")
	}

	outputFields := o.OutputFields
	queryOpt := milvusclient.NewQueryOption(collection)
	if expr != "" {
		queryOpt = queryOpt.WithFilter(expr)
	}
	if len(outputFields) > 0 {
		queryOpt = queryOpt.WithOutputFields(outputFields...)
	}

	resultSet, err := s.client.Query(ctx, queryOpt)
	if err != nil {
		return nil, fmt.Errorf("查询集合 %s 失败: %w", collection, err)
	}

	return resultSetToMaps(resultSet), nil
}

// Delete 按ID或过滤表达式删除数据。
func (s *MilvusGraphStore) Delete(ctx context.Context, collection string, opts ...graph.Option) error {
	if err := s.ensureInit(ctx); err != nil {
		return err
	}
	return s.delete(ctx, collection, opts...)
}

// IsEmpty 检查集合是否为空。
func (s *MilvusGraphStore) IsEmpty(ctx context.Context, collection string) (bool, error) {
	if err := s.ensureInit(ctx); err != nil {
		return false, err
	}

	resultSet, err := s.client.Query(ctx, milvusclient.NewQueryOption(collection).WithFilter("uuid != ''").WithOutputFields("uuid").WithLimit(1))
	if err != nil {
		return false, fmt.Errorf("检查集合 %s 是否为空失败: %w", collection, err)
	}

	return resultSet.ResultCount == 0, nil
}

// Search 混合搜索。
func (s *MilvusGraphStore) Search(ctx context.Context, query string, opts ...graph.Option) (map[string][]map[string]any, error) {
	if err := s.ensureInit(ctx); err != nil {
		return nil, err
	}
	return s.search(ctx, query, opts...)
}

// AttachEmbedder 绑定嵌入模型。
// 对齐 Python: 校验 embed_dim 与 embedder.dimension 是否一致，不一致则返回错误。
func (s *MilvusGraphStore) AttachEmbedder(embedder embedding.BaseEmbedding) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 校验嵌入维度，对齐 Python: if config.embed_dim != embedder.dimension
	if s.config.EmbedDim != embedder.Dimension() {
		return fmt.Errorf("嵌入维度不匹配: 配置 EmbedDim=%d, embedder.Dimension=%d", s.config.EmbedDim, embedder.Dimension())
	}

	if s.graphWriter != nil {
		s.graphWriter.embedder = embedder
	}
	if s.graphSearcher != nil {
		s.graphSearcher.embedder = embedder
	}

	logger.Info(logComponent).Int("embed_dim", s.config.EmbedDim).Msg("已绑定嵌入模型")
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// getClient 懒初始化 Milvus 客户端（double-check locking）
func (s *MilvusGraphStore) getClient(ctx context.Context) (milvusClient, error) {
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

	c, err := s.createClient(ctx, s.config.URI, s.config.Token, s.config.Name)
	if err != nil {
		logger.Error(logComponent).Err(err).Str("uri", s.config.URI).Msg("连接 Milvus 失败")
		return nil, fmt.Errorf("连接 Milvus 失败: %w", err)
	}
	s.client = c
	logger.Info(logComponent).Str("uri", s.config.URI).Msg("成功连接 Milvus")
	return s.client, nil
}

// ensureInit 确保图存储已初始化（创建集合 + 构建 writer/searcher）
func (s *MilvusGraphStore) ensureInit(ctx context.Context) error {
	s.mu.RLock()
	if s.initialized {
		s.mu.RUnlock()
		return nil
	}
	s.mu.RUnlock()

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.initialized {
		return nil
	}

	// 在已持有写锁时直接创建客户端（避免与 getClient 死锁）
	client := s.client
	if client == nil {
		c, err := s.createClient(ctx, s.config.URI, s.config.Token, s.config.Name)
		if err != nil {
			logger.Error(logComponent).Err(err).Str("uri", s.config.URI).Msg("连接 Milvus 失败")
			return fmt.Errorf("连接 Milvus 失败: %w", err)
		}
		s.client = c
		client = c
		logger.Info(logComponent).Str("uri", s.config.URI).Msg("成功连接 Milvus")
	}

	// 确保集合存在
	storageCfg := s.config.StorageConfig
	if storageCfg == nil {
		storageCfg = graph.NewDefaultStorageConfig()
	}
	indexCfg := s.config.IndexConfig
	if indexCfg == nil {
		indexCfg = graph.NewDefaultIndexConfig()
	}

	if err := EnsureCollections(ctx, client, storageCfg, indexCfg, s.config.EmbedDim); err != nil {
		return fmt.Errorf("初始化集合失败: %w", err)
	}

	// 构建 writer 和 searcher

	metric := "cosine"
	if indexCfg != nil && indexCfg.DistanceMetric != "" {
		metric = indexCfg.DistanceMetric
	}

	s.graphWriter = newGraphWriter(client, storageCfg, nil, s.config.EmbedDim, s.config.EmbedBatchSize, s.config.MaxConcurrent)
	s.graphSearcher = newGraphSearcher(client, nil, indexCfg, graph.GlobalRankerRegistry, metric)
	s.initialized = true

	logger.Info(logComponent).Msg("Milvus 图存储初始化完成")
	return nil
}

// init 注册 milvus 后端到全局工厂
func init() {
	if err := graph.RegisterBackend("milvus", func(cfg *graph.GraphConfig) (graph.BaseGraphStore, error) {
		return NewMilvusGraphStore(cfg), nil
	}); err != nil {
		logger.Error(logComponent).Err(err).Msg("注册 milvus 图存储后端失败")
	}
}
