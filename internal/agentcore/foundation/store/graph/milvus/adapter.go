package milvus

import (
	"context"

	"github.com/milvus-io/milvus/client/v2/entity"
	milvusclient "github.com/milvus-io/milvus/client/v2/milvusclient"
)

// ──────────────────────────── 结构体 ────────────────────────────

// milvusClientGraphAdapter 适配器，包装真实 Milvus SDK 客户端。
// 将 SDK 的异步 Task 返回值转换为同步 error 返回值。
// 仅在集成测试中覆盖（需要真实 Milvus 服务器）。
type milvusClientGraphAdapter struct {
	client *milvusclient.Client
}

// ──────────────────────────── 导出函数 ────────────────────────────

// CreateCollection 创建集合
func (a *milvusClientGraphAdapter) CreateCollection(ctx context.Context, option milvusclient.CreateCollectionOption, callOptions ...interface{}) error {
	return a.client.CreateCollection(ctx, option)
}

// DropCollection 删除集合
func (a *milvusClientGraphAdapter) DropCollection(ctx context.Context, option milvusclient.DropCollectionOption, callOptions ...interface{}) error {
	return a.client.DropCollection(ctx, option)
}

// HasCollection 检查集合是否存在
func (a *milvusClientGraphAdapter) HasCollection(ctx context.Context, option milvusclient.HasCollectionOption, callOptions ...interface{}) (bool, error) {
	return a.client.HasCollection(ctx, option)
}

// DescribeCollection 获取集合描述信息
func (a *milvusClientGraphAdapter) DescribeCollection(ctx context.Context, option milvusclient.DescribeCollectionOption, callOptions ...interface{}) (*entity.Collection, error) {
	return a.client.DescribeCollection(ctx, option)
}

// Insert 插入数据
func (a *milvusClientGraphAdapter) Insert(ctx context.Context, option milvusclient.InsertOption, callOptions ...interface{}) (milvusclient.InsertResult, error) {
	return a.client.Insert(ctx, option)
}

// Upsert 更新或插入数据
func (a *milvusClientGraphAdapter) Upsert(ctx context.Context, option milvusclient.UpsertOption, callOptions ...interface{}) (milvusclient.UpsertResult, error) {
	return a.client.Upsert(ctx, option)
}

// Search 向量搜索
func (a *milvusClientGraphAdapter) Search(ctx context.Context, option milvusclient.SearchOption, callOptions ...interface{}) ([]milvusclient.ResultSet, error) {
	return a.client.Search(ctx, option)
}

// HybridSearch 混合搜索
func (a *milvusClientGraphAdapter) HybridSearch(ctx context.Context, option milvusclient.HybridSearchOption, callOptions ...interface{}) ([]milvusclient.ResultSet, error) {
	return a.client.HybridSearch(ctx, option)
}

// Query 查询数据
func (a *milvusClientGraphAdapter) Query(ctx context.Context, option milvusclient.QueryOption, callOptions ...interface{}) (milvusclient.ResultSet, error) {
	return a.client.Query(ctx, option)
}

// Delete 删除数据
func (a *milvusClientGraphAdapter) Delete(ctx context.Context, option milvusclient.DeleteOption, callOptions ...interface{}) (milvusclient.DeleteResult, error) {
	return a.client.Delete(ctx, option)
}

// ListCollections 列出所有集合
func (a *milvusClientGraphAdapter) ListCollections(ctx context.Context, option milvusclient.ListCollectionOption, callOptions ...interface{}) ([]string, error) {
	return a.client.ListCollections(ctx, option)
}

// LoadCollection 加载集合到内存
func (a *milvusClientGraphAdapter) LoadCollection(ctx context.Context, option milvusclient.LoadCollectionOption, callOptions ...interface{}) error {
	task, err := a.client.LoadCollection(ctx, option)
	if err != nil {
		return err
	}
	return task.Await(ctx)
}

// Flush 刷写数据到存储
func (a *milvusClientGraphAdapter) Flush(ctx context.Context, option milvusclient.FlushOption, callOptions ...interface{}) error {
	task, err := a.client.Flush(ctx, option)
	if err != nil {
		return err
	}
	if task != nil {
		return task.Await(ctx)
	}
	return nil
}

// CreateIndex 创建索引
func (a *milvusClientGraphAdapter) CreateIndex(ctx context.Context, option milvusclient.CreateIndexOption, callOptions ...interface{}) error {
	task, err := a.client.CreateIndex(ctx, option)
	if err != nil {
		return err
	}
	if task != nil {
		return task.Await(ctx)
	}
	return nil
}

// DropDatabase 删除数据库
func (a *milvusClientGraphAdapter) DropDatabase(ctx context.Context, option milvusclient.DropDatabaseOption, callOptions ...interface{}) error {
	return a.client.DropDatabase(ctx, option)
}

// Compact 压缩集合数据
func (a *milvusClientGraphAdapter) Compact(ctx context.Context, option milvusclient.CompactOption, callOptions ...interface{}) (int64, error) {
	return a.client.Compact(ctx, option)
}

// Close 关闭客户端连接
func (a *milvusClientGraphAdapter) Close(ctx context.Context) error {
	return a.client.Close(ctx)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// defaultCreateGraphClient 默认客户端创建函数。
// 仅在集成测试中覆盖。
func defaultCreateGraphClient(ctx context.Context, uri, token, dbName string) (milvusClient, error) {
	c, err := milvusclient.New(ctx, &milvusclient.ClientConfig{
		Address: uri,
		APIKey:  token,
		DBName:  dbName,
	})
	if err != nil {
		return nil, err
	}
	return &milvusClientGraphAdapter{client: c}, nil
}
