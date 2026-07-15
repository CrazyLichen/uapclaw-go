// Package vector 提供向量存储的抽象接口和具体实现。
//
// 本文件包含 Milvus SDK 客户端适配器，将 milvusclient.Client 适配到
// milvusClient 接口。由于适配器方法直接委托给真实 SDK Client，
// 无法在单元测试中 mock，这些代码仅在集成测试中覆盖。
//
// 对应 Python: vector/milvus_vector_store.py (MilvusVectorStore)
package vector

import (
	"context"

	"github.com/milvus-io/milvus/client/v2/entity"
	milvusclient "github.com/milvus-io/milvus/client/v2/milvusclient"
)

// ──────────────────────────── 结构体 ────────────────────────────

// milvusClientAdapter 适配器，将 milvusclient.Client 适配到 milvusClient 接口。
//
// 新 SDK 的 Client 方法签名使用具体 Option 类型而非接口，
// 需要适配层来桥接测试和生产的接口统一。
//
// 注意：适配器方法直接委托给真实 SDK Client，无法在单元测试中覆盖，
// 需通过集成测试（go test -tags=integration）验证。
type milvusClientAdapter struct {
	client *milvusclient.Client
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────
func (a *milvusClientAdapter) CreateCollection(ctx context.Context, option milvusclient.CreateCollectionOption, callOptions ...interface{}) error {
	return a.client.CreateCollection(ctx, option)
}

func (a *milvusClientAdapter) DropCollection(ctx context.Context, option milvusclient.DropCollectionOption, callOptions ...interface{}) error {
	return a.client.DropCollection(ctx, option)
}

func (a *milvusClientAdapter) HasCollection(ctx context.Context, option milvusclient.HasCollectionOption, callOptions ...interface{}) (bool, error) {
	return a.client.HasCollection(ctx, option)
}

func (a *milvusClientAdapter) DescribeCollection(ctx context.Context, option milvusclient.DescribeCollectionOption, callOptions ...interface{}) (*entity.Collection, error) {
	return a.client.DescribeCollection(ctx, option)
}

func (a *milvusClientAdapter) Insert(ctx context.Context, option milvusclient.InsertOption, callOptions ...interface{}) (milvusclient.InsertResult, error) {
	return a.client.Insert(ctx, option)
}

func (a *milvusClientAdapter) Search(ctx context.Context, option milvusclient.SearchOption, callOptions ...interface{}) ([]milvusclient.ResultSet, error) {
	return a.client.Search(ctx, option)
}

func (a *milvusClientAdapter) Delete(ctx context.Context, option milvusclient.DeleteOption, callOptions ...interface{}) (milvusclient.DeleteResult, error) {
	return a.client.Delete(ctx, option)
}

func (a *milvusClientAdapter) ListCollections(ctx context.Context, option milvusclient.ListCollectionOption, callOptions ...interface{}) ([]string, error) {
	return a.client.ListCollections(ctx, option)
}

func (a *milvusClientAdapter) LoadCollection(ctx context.Context, option milvusclient.LoadCollectionOption, callOptions ...interface{}) error {
	task, err := a.client.LoadCollection(ctx, option)
	if err != nil {
		return err
	}
	return task.Await(ctx)
}

func (a *milvusClientAdapter) Flush(ctx context.Context, option milvusclient.FlushOption, callOptions ...interface{}) error {
	task, err := a.client.Flush(ctx, option)
	if err != nil {
		return err
	}
	if task != nil {
		return task.Await(ctx)
	}
	return nil
}

func (a *milvusClientAdapter) CreateIndex(ctx context.Context, option milvusclient.CreateIndexOption, callOptions ...interface{}) error {
	task, err := a.client.CreateIndex(ctx, option)
	if err != nil {
		return err
	}
	if task != nil {
		return task.Await(ctx)
	}
	return nil
}

func (a *milvusClientAdapter) DescribeIndex(ctx context.Context, option milvusclient.DescribeIndexOption, callOptions ...interface{}) (milvusclient.IndexDescription, error) {
	return a.client.DescribeIndex(ctx, option)
}

func (a *milvusClientAdapter) Close(ctx context.Context) error {
	return a.client.Close(ctx)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// defaultCreateClient 默认的客户端创建函数，使用新 SDK milvusclient.New。
//
// 注意：此函数依赖真实 Milvus 服务，无法在单元测试中覆盖，
// 需通过集成测试（go test -tags=integration）验证。
func defaultCreateClient(ctx context.Context, uri, token, dbName string) (milvusClient, error) {
	c, err := milvusclient.New(ctx, &milvusclient.ClientConfig{
		Address: uri,
		APIKey:  token,
		DBName:  dbName,
	})
	if err != nil {
		return nil, err
	}
	return &milvusClientAdapter{client: c}, nil
}
