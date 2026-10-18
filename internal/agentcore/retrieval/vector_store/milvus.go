package vector_store

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/milvus-io/milvus/client/v2/entity"
	milvusclient "github.com/milvus-io/milvus/client/v2/milvusclient"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/vector_fields"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 接口 ────────────────────────────

// MilvusClient retrieval 层 Milvus 客户端接口。
//
// 对齐 Python 直接使用 pymilvus.MilvusClient 的方法子集，
// 与 foundation 层的 milvusClient 接口独立（Python 两层也各自独立用 SDK）。
// 通过依赖注入支持单元测试 mock。
type MilvusClient interface {
	// CreateCollection 创建集合
	CreateCollection(ctx context.Context, option milvusclient.CreateCollectionOption, callOptions ...any) error
	// DropCollection 删除集合
	DropCollection(ctx context.Context, option milvusclient.DropCollectionOption, callOptions ...any) error
	// HasCollection 检查集合是否存在
	HasCollection(ctx context.Context, option milvusclient.HasCollectionOption, callOptions ...any) (bool, error)
	// DescribeCollection 获取集合描述
	DescribeCollection(ctx context.Context, option milvusclient.DescribeCollectionOption, callOptions ...any) (*entity.Collection, error)
	// ListCollections 列出集合名称
	ListCollections(ctx context.Context, option milvusclient.ListCollectionOption, callOptions ...any) ([]string, error)
	// GetCollectionStats 获取集合统计信息（row_count）
	GetCollectionStats(ctx context.Context, option milvusclient.GetCollectionOption, callOptions ...any) (map[string]string, error)
	// Insert 插入数据
	Insert(ctx context.Context, option milvusclient.InsertOption, callOptions ...any) (milvusclient.InsertResult, error)
	// Delete 删除数据
	Delete(ctx context.Context, option milvusclient.DeleteOption, callOptions ...any) (milvusclient.DeleteResult, error)
	// Flush 刷写数据
	Flush(ctx context.Context, option milvusclient.FlushOption, callOptions ...any) error
	// Search 向量搜索
	Search(ctx context.Context, option milvusclient.SearchOption, callOptions ...any) ([]milvusclient.ResultSet, error)
	// HybridSearch 混合搜索
	HybridSearch(ctx context.Context, option milvusclient.HybridSearchOption, callOptions ...any) ([]milvusclient.ResultSet, error)
	// Query 按表达式查询
	Query(ctx context.Context, option milvusclient.QueryOption, callOptions ...any) (milvusclient.ResultSet, error)
	// DescribeIndex 获取索引描述
	DescribeIndex(ctx context.Context, option milvusclient.DescribeIndexOption, callOptions ...any) (milvusclient.IndexDescription, error)
	// LoadCollection 加载集合
	LoadCollection(ctx context.Context, option milvusclient.LoadCollectionOption, callOptions ...any) error
	// GetLoadState 获取加载状态
	GetLoadState(ctx context.Context, option milvusclient.GetLoadStateOption, callOptions ...any) (entity.LoadState, error)
	// ReleaseCollection 释放集合
	ReleaseCollection(ctx context.Context, option milvusclient.ReleaseCollectionOption, callOptions ...any) error
	// ListDatabase 列出数据库
	ListDatabase(ctx context.Context, option milvusclient.ListDatabaseOption, callOptions ...any) ([]string, error)
	// CreateDatabase 创建数据库
	CreateDatabase(ctx context.Context, option milvusclient.CreateDatabaseOption, callOptions ...any) error
	// UseDatabase 切换数据库
	UseDatabase(ctx context.Context, option milvusclient.UseDatabaseOption, callOptions ...any) error
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

// ──────────────────────────── 导出函数 ────────────────────────────

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

// ──────────────────────────── 非导出函数 ────────────────────────────

// DefaultMilvusCreateClient 默认客户端创建函数。
//
// 对齐 Python MilvusVectorStore.create_client：
// 创建 client → database_name 非空且非 default 时确保数据库存在并切换。
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
	return &milvusClientAdapter{client: c}, nil
}

// CreateCollection 创建集合。
func (a *milvusClientAdapter) CreateCollection(ctx context.Context, option milvusclient.CreateCollectionOption, callOptions ...any) error {
	return a.client.CreateCollection(ctx, option)
}

// DropCollection 删除集合。
func (a *milvusClientAdapter) DropCollection(ctx context.Context, option milvusclient.DropCollectionOption, callOptions ...any) error {
	return a.client.DropCollection(ctx, option)
}

// HasCollection 检查集合是否存在。
func (a *milvusClientAdapter) HasCollection(ctx context.Context, option milvusclient.HasCollectionOption, callOptions ...any) (bool, error) {
	return a.client.HasCollection(ctx, option)
}

// DescribeCollection 获取集合描述。
func (a *milvusClientAdapter) DescribeCollection(ctx context.Context, option milvusclient.DescribeCollectionOption, callOptions ...any) (*entity.Collection, error) {
	return a.client.DescribeCollection(ctx, option)
}

// ListCollections 列出集合名称。
func (a *milvusClientAdapter) ListCollections(ctx context.Context, option milvusclient.ListCollectionOption, callOptions ...any) ([]string, error) {
	return a.client.ListCollections(ctx, option)
}

// GetCollectionStats 获取集合统计信息。
func (a *milvusClientAdapter) GetCollectionStats(ctx context.Context, option milvusclient.GetCollectionOption, callOptions ...any) (map[string]string, error) {
	return a.client.GetCollectionStats(ctx, option)
}

// Insert 插入数据。
func (a *milvusClientAdapter) Insert(ctx context.Context, option milvusclient.InsertOption, callOptions ...any) (milvusclient.InsertResult, error) {
	return a.client.Insert(ctx, option)
}

// Delete 删除数据。
func (a *milvusClientAdapter) Delete(ctx context.Context, option milvusclient.DeleteOption, callOptions ...any) (milvusclient.DeleteResult, error) {
	return a.client.Delete(ctx, option)
}

// Flush 刷写数据（异步 Task 转同步）。
func (a *milvusClientAdapter) Flush(ctx context.Context, option milvusclient.FlushOption, callOptions ...any) error {
	task, err := a.client.Flush(ctx, option)
	if err != nil {
		return err
	}
	if task != nil {
		return task.Await(ctx)
	}
	return nil
}

// Search 向量搜索。
func (a *milvusClientAdapter) Search(ctx context.Context, option milvusclient.SearchOption, callOptions ...any) ([]milvusclient.ResultSet, error) {
	return a.client.Search(ctx, option)
}

// HybridSearch 混合搜索。
func (a *milvusClientAdapter) HybridSearch(ctx context.Context, option milvusclient.HybridSearchOption, callOptions ...any) ([]milvusclient.ResultSet, error) {
	return a.client.HybridSearch(ctx, option)
}

// Query 按表达式查询。
func (a *milvusClientAdapter) Query(ctx context.Context, option milvusclient.QueryOption, callOptions ...any) (milvusclient.ResultSet, error) {
	return a.client.Query(ctx, option)
}

// DescribeIndex 获取索引描述。
func (a *milvusClientAdapter) DescribeIndex(ctx context.Context, option milvusclient.DescribeIndexOption, callOptions ...any) (milvusclient.IndexDescription, error) {
	return a.client.DescribeIndex(ctx, option)
}

// LoadCollection 加载集合（异步 Task 转同步）。
func (a *milvusClientAdapter) LoadCollection(ctx context.Context, option milvusclient.LoadCollectionOption, callOptions ...any) error {
	task, err := a.client.LoadCollection(ctx, option)
	if err != nil {
		return err
	}
	return task.Await(ctx)
}

// GetLoadState 获取加载状态。
func (a *milvusClientAdapter) GetLoadState(ctx context.Context, option milvusclient.GetLoadStateOption, callOptions ...any) (entity.LoadState, error) {
	return a.client.GetLoadState(ctx, option)
}

// ReleaseCollection 释放集合。
func (a *milvusClientAdapter) ReleaseCollection(ctx context.Context, option milvusclient.ReleaseCollectionOption, callOptions ...any) error {
	return a.client.ReleaseCollection(ctx, option)
}

// ListDatabase 列出数据库。
func (a *milvusClientAdapter) ListDatabase(ctx context.Context, option milvusclient.ListDatabaseOption, callOptions ...any) ([]string, error) {
	return a.client.ListDatabase(ctx, option)
}

// CreateDatabase 创建数据库。
func (a *milvusClientAdapter) CreateDatabase(ctx context.Context, option milvusclient.CreateDatabaseOption, callOptions ...any) error {
	return a.client.CreateDatabase(ctx, option)
}

// UseDatabase 切换数据库。
func (a *milvusClientAdapter) UseDatabase(ctx context.Context, option milvusclient.UseDatabaseOption, callOptions ...any) error {
	return a.client.UseDatabase(ctx, option)
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
