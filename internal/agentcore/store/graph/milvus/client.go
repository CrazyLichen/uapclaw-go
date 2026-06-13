package milvus

import (
	"context"

	"github.com/milvus-io/milvus/client/v2/entity"
	milvusclient "github.com/milvus-io/milvus/client/v2/milvusclient"
)

// ──────────────────────────── 接口 ────────────────────────────

// milvusClient Milvus 客户端接口，用于测试 mock 和生产适配。
// 使用 ...interface{} 作为 callOptions 参数，避免在接口中导入 gRPC。
type milvusClient interface {
	CreateCollection(ctx context.Context, option milvusclient.CreateCollectionOption, callOptions ...interface{}) error
	DropCollection(ctx context.Context, option milvusclient.DropCollectionOption, callOptions ...interface{}) error
	HasCollection(ctx context.Context, option milvusclient.HasCollectionOption, callOptions ...interface{}) (bool, error)
	DescribeCollection(ctx context.Context, option milvusclient.DescribeCollectionOption, callOptions ...interface{}) (*entity.Collection, error)
	Insert(ctx context.Context, option milvusclient.InsertOption, callOptions ...interface{}) (milvusclient.InsertResult, error)
	Upsert(ctx context.Context, option milvusclient.UpsertOption, callOptions ...interface{}) (milvusclient.UpsertResult, error)
	Search(ctx context.Context, option milvusclient.SearchOption, callOptions ...interface{}) ([]milvusclient.ResultSet, error)
	HybridSearch(ctx context.Context, option milvusclient.HybridSearchOption, callOptions ...interface{}) ([]milvusclient.ResultSet, error)
	Query(ctx context.Context, option milvusclient.QueryOption, callOptions ...interface{}) (milvusclient.ResultSet, error)
	Delete(ctx context.Context, option milvusclient.DeleteOption, callOptions ...interface{}) (milvusclient.DeleteResult, error)
	ListCollections(ctx context.Context, option milvusclient.ListCollectionOption, callOptions ...interface{}) ([]string, error)
	LoadCollection(ctx context.Context, option milvusclient.LoadCollectionOption, callOptions ...interface{}) error
	Flush(ctx context.Context, option milvusclient.FlushOption, callOptions ...interface{}) error
	CreateIndex(ctx context.Context, option milvusclient.CreateIndexOption, callOptions ...interface{}) error
	Close(ctx context.Context) error
}
